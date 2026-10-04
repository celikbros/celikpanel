//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Item 4a: the pin is read before any effect, and its refusal names the
// required and the offered version and what the owner can do.
func TestFreshPDNSVersionPreflightNamesRequiredAndOffered(t *testing.T) {
	packages := []string{"pdns-server", "pdns-backend-sqlite3"}
	pin := freshPDNSDebian13PackageVersionV3
	ops := func(installed, candidate map[string]string) freshPDNSVersionOpsV3 {
		return freshPDNSVersionOpsV3{
			installed: func(name string) (string, error) {
				if v, ok := installed[name]; ok {
					return v, nil
				}
				return "", errors.New("not installed")
			},
			candidate: func(name string) (string, error) {
				if v, ok := candidate[name]; ok {
					return v, nil
				}
				return "", errors.New("apt-cache policy failed")
			},
		}
	}
	if err := preflightFreshPDNSPackageVersionsWithOpsV3(packages, packages,
		ops(nil, map[string]string{"pdns-server": pin, "pdns-backend-sqlite3": pin})); err != nil {
		t.Fatalf("the measured candidate was refused: %v", err)
	}
	if err := preflightFreshPDNSPackageVersionsWithOpsV3(packages, []string{"pdns-backend-sqlite3"},
		ops(map[string]string{"pdns-server": pin}, map[string]string{"pdns-backend-sqlite3": pin})); err != nil {
		t.Fatalf("an installed measured package was refused: %v", err)
	}
	for name, tc := range map[string]struct {
		missing   []string
		installed map[string]string
		candidate map[string]string
		want      []string
	}{
		"newer candidate": {packages, nil, map[string]string{"pdns-server": "4.9.18-0+deb13u1", "pdns-backend-sqlite3": pin},
			[]string{"would install pdns-server 4.9.18-0+deb13u1", "measured version " + pin, "installable for APT", "names 4.9.18-0+deb13u1 as measured", "nothing was changed"}},
		"no candidate": {packages, nil, map[string]string{"pdns-server": "", "pdns-backend-sqlite3": pin},
			[]string{"offers no installable pdns-server", pin}},
		"unreadable candidate": {packages, nil, map[string]string{"pdns-server": pin},
			[]string{"could not read which pdns-backend-sqlite3 version", "apt-cache policy pdns-backend-sqlite3"}},
		"installed other version": {nil, map[string]string{"pdns-server": "4.8.4-1", "pdns-backend-sqlite3": pin}, nil,
			[]string{"pdns-server 4.8.4-1 is already installed", "replaces it with version " + pin}},
	} {
		t.Run(name, func(t *testing.T) {
			err := preflightFreshPDNSPackageVersionsWithOpsV3(packages, tc.missing, ops(tc.installed, tc.candidate))
			sentence := operatorSentenceOf(err)
			if err == nil || sentence == "" {
				t.Fatalf("mismatch was not refused with an operator sentence: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(sentence, want) {
					t.Fatalf("refusal lacks %q: %q", want, sentence)
				}
			}
		})
	}
	// The refusal precedes every effect of the switch.
	raw, err := os.ReadFile("dns_engine_pdns_switch.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func switchToPDNSOnCertifiedProfile(")
	if start < 0 {
		t.Fatal("switchToPDNSOnCertifiedProfile is missing")
	}
	source = source[start:]
	if end := strings.Index(source[1:], "\nfunc "); end > 0 {
		source = source[:end+1]
	}
	preflight := strings.Index(source, "preflightFreshPDNSPackageVersionsV3(ctx, profile, packages, missing)")
	for _, effect := range []string{"publishDNSEngineSourceOwnership(\n", "installOwnedDNSEnginePackages(", "installFreshPDNSPackagesV3(ctx, missing)"} {
		if at := strings.Index(source, effect); preflight < 0 || at < preflight {
			t.Fatalf("%q is not after the V3 version preflight (%d, %d)", effect, at, preflight)
		}
	}
	if !validDebianVersion(pin) || validDebianVersion("4.9;rm") || validDebianVersion("") {
		t.Fatal("Debian version validation is wrong")
	}
}

// Item 4b: the complete build is published under the candidate name by an
// atomic, non-replacing rename in the same directory; the inode is kept.
func TestPublishFreshPDNSCandidateBuildV3(t *testing.T) {
	dir := t.TempDir()
	build := filepath.Join(dir, ".celikpanel-switch-"+strings.Repeat("a", 32)+".building.sqlite3")
	candidate := filepath.Join(dir, ".celikpanel-switch-"+strings.Repeat("a", 32)+".sqlite3")
	if err := os.WriteFile(build, []byte("complete database"), 0o600); err != nil {
		t.Fatal(err)
	}
	var before unix.Stat_t
	if err := unix.Stat(build, &before); err != nil {
		t.Fatal(err)
	}
	if err := publishFreshPDNSCandidateBuildV3(build, candidate); err != nil {
		t.Fatal(err)
	}
	var after unix.Stat_t
	if err := unix.Stat(candidate, &after); err != nil || after.Ino != before.Ino {
		t.Fatalf("candidate inode=%d, want %d (%v)", after.Ino, before.Ino, err)
	}
	if _, err := os.Lstat(build); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("build name still exists: %v", err)
	}
	if err := os.WriteFile(build, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishFreshPDNSCandidateBuildV3(build, candidate); err == nil {
		t.Fatal("an existing candidate was replaced")
	}
	if err := publishFreshPDNSCandidateBuildV3(build, filepath.Join(t.TempDir(), "x.sqlite3")); err == nil {
		t.Fatal("a rename across directories was attempted")
	}
}
