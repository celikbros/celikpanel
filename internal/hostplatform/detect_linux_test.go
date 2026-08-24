//go:build linux

package hostplatform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFixedExecutableContractsMatchBootstrap(t *testing.T) {
	want := map[string]fixedExecutableContract{
		"apt-get":      {path: "/usr/bin/apt-get"},
		"apt-cache":    {path: "/usr/bin/apt-cache"},
		"dpkg-query":   {path: "/usr/bin/dpkg-query"},
		"pacman":       {path: "/usr/bin/pacman"},
		"dnf":          {path: "/usr/bin/dnf", allowedSymlinkTarget: "/usr/bin/dnf-3"},
		"rpm":          {path: "/usr/bin/rpm"},
		"systemctl":    {path: "/usr/bin/systemctl"},
		"timeout":      {path: "/usr/bin/timeout"},
		"restorecon":   {path: "/usr/sbin/restorecon"},
		"matchpathcon": {path: "/usr/sbin/matchpathcon"},
		"getenforce":   {path: "/usr/sbin/getenforce"},
	}
	if len(fixedExecutableContracts) != len(want) {
		t.Fatalf("fixed contracts = %#v, want exactly %#v", fixedExecutableContracts, want)
	}
	for role, expected := range want {
		if actual, ok := fixedExecutableContracts[role]; !ok || actual != expected {
			t.Fatalf("fixed contract %q = %#v, %v; want %#v", role, actual, ok, expected)
		}
	}
}

func TestSystemdReadinessTimeoutMatchesLifecycleScripts(t *testing.T) {
	if systemdReadinessTimeout != 3*time.Second {
		t.Fatalf("systemd readiness timeout = %s, want 3s", systemdReadinessTimeout)
	}
}

func TestValidateSystemdReadinessResultMatchesLifecycleStatusPairs(t *testing.T) {
	for _, valid := range []struct {
		output string
		status int
	}{
		{output: "running\n", status: 0},
		{output: "degraded\n", status: 0},
		{output: "degraded\n", status: 1},
		{output: "running\n\n", status: 0},
	} {
		if err := validateSystemdReadinessResult([]byte(valid.output), valid.status); err != nil {
			t.Fatalf("valid readiness output=%q status=%d rejected: %v", valid.output, valid.status, err)
		}
	}
	for _, invalid := range []struct {
		output string
		status int
	}{
		{output: "running\n", status: 1},
		{output: "running\n", status: 2},
		{output: "degraded\n", status: 2},
		{output: "starting\n", status: 0},
		{output: " running\n", status: 0},
		{output: "running\r\n", status: 0},
		{output: "running\nwarning\n", status: 0},
	} {
		if err := validateSystemdReadinessResult([]byte(invalid.output), invalid.status); err == nil {
			t.Fatalf("invalid readiness output=%q status=%d accepted", invalid.output, invalid.status)
		}
	}
}

func TestValidateGetenforceOutputRequiresExactEnforcingState(t *testing.T) {
	for _, valid := range [][]byte{[]byte("Enforcing"), []byte("Enforcing\n"), []byte("Enforcing\n\n")} {
		if err := validateGetenforceOutput(valid); err != nil {
			t.Fatalf("valid getenforce output %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range [][]byte{
		[]byte("Permissive\n"),
		[]byte("Disabled\n"),
		[]byte(" Enforcing\n"),
		[]byte("Enforcing \n"),
		[]byte("Enforcing\r\n"),
	} {
		if err := validateGetenforceOutput(invalid); err == nil {
			t.Fatalf("invalid getenforce output %q accepted", invalid)
		}
	}
}

func TestValidateFixedExecutableResolutionSymlinkContract(t *testing.T) {
	tests := []struct {
		name      string
		role      string
		path      string
		contract  fixedExecutableContract
		symbolic  bool
		canonical string
		wantError string
	}{
		{
			name: "apt direct", role: "apt-get", path: "/usr/bin/apt-get",
			contract: fixedExecutableContracts["apt-get"], canonical: "/usr/bin/apt-get",
		},
		{
			name: "dnf direct", role: "dnf", path: "/usr/bin/dnf",
			contract: fixedExecutableContracts["dnf"], canonical: "/usr/bin/dnf",
		},
		{
			name: "dnf pinned alternative", role: "dnf", path: "/usr/bin/dnf",
			contract: fixedExecutableContracts["dnf"], symbolic: true, canonical: "/usr/bin/dnf-3",
		},
		{
			name: "dnf arbitrary target", role: "dnf", path: "/usr/bin/dnf",
			contract: fixedExecutableContracts["dnf"], symbolic: true, canonical: "/opt/vendor/dnf",
			wantError: "want /usr/bin/dnf-3",
		},
		{
			name: "apt symlink", role: "apt-get", path: "/usr/bin/apt-get",
			contract: fixedExecutableContracts["apt-get"], symbolic: true, canonical: "/usr/bin/apt-get.real",
			wantError: "must not be symbolic",
		},
		{
			name: "arbitrary caller path", role: "pacman", path: "/usr/local/bin/pacman",
			contract: fixedExecutableContracts["pacman"], canonical: "/usr/local/bin/pacman",
			wantError: "must use fixed executable path /usr/bin/pacman",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateFixedExecutableResolution(
				test.role, test.path, test.contract, test.symbolic, test.canonical,
			)
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestVerifySELinuxInactiveAtMatchesLifecycleContract(t *testing.T) {
	root := t.TempDir()
	absent := filepath.Join(root, "absent")
	if err := verifySELinuxInactiveAt(absent); err != nil {
		t.Fatalf("inactive SELinux host rejected: %v", err)
	}

	tests := []struct {
		name      string
		content   []byte
		mode      os.FileMode
		wantError string
	}{
		{name: "enforcing", content: []byte("1\n"), mode: 0o600, wantError: "active in enforcing"},
		{name: "permissive", content: []byte("0\n"), mode: 0o600, wantError: "active in permissive"},
		{name: "malformed", content: []byte("Enforcing\n"), mode: 0o600, wantError: "malformed"},
		{name: "multiline", content: []byte("1\n0\n"), mode: 0o600, wantError: "exactly one line"},
		{name: "unterminated", content: []byte("1"), mode: 0o600, wantError: "newline-terminated"},
		{name: "NUL-bearing", content: []byte{'1', 0, '\n'}, mode: 0o600, wantError: "NUL byte"},
		{name: "unreadable-mode", content: []byte("1\n"), mode: 0o000, wantError: "no readable permission bit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name)
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			err := verifySELinuxInactiveAt(path)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want substring %q", err, test.wantError)
			}
		})
	}

	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := verifySELinuxInactiveAt(link); err == nil || !strings.Contains(err.Error(), "must not be symbolic") {
		t.Fatalf("symbolic SELinux state error = %v", err)
	}

	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := verifySELinuxInactiveAt(directory); err == nil || !strings.Contains(err.Error(), "unavailable or unreadable") {
		t.Fatalf("directory SELinux state error = %v", err)
	}
}
