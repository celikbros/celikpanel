package hostplatform

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func readyProbe(binaries ...string) Probe {
	available := make(map[string]bool, len(binaries))
	for _, binary := range binaries {
		available[binary] = true
	}
	// timeout is a mandatory platform primitive in every ordinary fixture.
	// The missing-timeout test overrides this one entry explicitly.
	available["timeout"] = true
	return Probe{
		ExecutablePresent: func(binary string) (bool, error) {
			return available[binary], nil
		},
		LookPath: func(binary string) (string, error) {
			if !available[binary] {
				return "", fmt.Errorf("%s not found", binary)
			}
			if binary == "restorecon" || binary == "matchpathcon" || binary == "getenforce" {
				return "/usr/sbin/" + binary, nil
			}
			return "/usr/bin/" + binary, nil
		},
		ValidateExecutable:  func(string, string) error { return nil },
		SystemdReady:        func(string) error { return nil },
		SecurityPolicyState: func() (SecurityPolicyState, error) { return SecurityPolicyInactive, nil },
		DNFSecurityReady:    func(string) error { return nil },
		Architecture:        func() (string, error) { return "x86_64", nil },
	}
}

func dnfReadyProbe(binaries ...string) Probe {
	tools := append([]string(nil), binaries...)
	tools = append(tools, "restorecon", "matchpathcon", "getenforce")
	probe := readyProbe(tools...)
	probe.SecurityPolicyState = func() (SecurityPolicyState, error) {
		return SecurityPolicyEnforcing, nil
	}
	return probe
}

func TestDetectWithSupportedDistributionFixtures(t *testing.T) {
	tests := []struct {
		name, release string
		family        DistroFamily
		manager       PackageManager
		binaries      []string
	}{
		{"debian", "ID=debian\nVERSION_ID=13\nVERSION_CODENAME=trixie\n", DistroFamilyDebian, PackageManagerAPT, []string{"apt-get", "apt-cache", "dpkg-query", "systemctl"}},
		{"ubuntu", "ID=ubuntu\nID_LIKE=debian\nVERSION_ID=24.04\nVERSION_CODENAME=noble\n", DistroFamilyDebian, PackageManagerAPT, []string{"apt-get", "apt-cache", "dpkg-query", "systemctl"}},
		{"rhel", "ID=rhel\nID_LIKE=\"fedora\"\nVERSION_ID=10.0\n", DistroFamilyRHEL, PackageManagerDNF, []string{"dnf", "rpm", "systemctl"}},
		{"alma", "ID=almalinux\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=10.0\n", DistroFamilyRHEL, PackageManagerDNF, []string{"dnf", "rpm", "systemctl"}},
		{"rocky", "ID=rocky\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=9.6\n", DistroFamilyRHEL, PackageManagerDNF, []string{"dnf", "rpm", "systemctl"}},
		{"centos", "ID=centos\nID_LIKE=\"rhel fedora\"\nVERSION_ID=10\n", DistroFamilyRHEL, PackageManagerDNF, []string{"dnf", "rpm", "systemctl"}},
		{"fedora", "ID=fedora\nVERSION_ID=42\n", DistroFamilyRHEL, PackageManagerDNF, []string{"dnf", "rpm", "systemctl"}},
		{"cloudlinux", "ID=cloudlinux\nID_LIKE=\"rhel centos fedora\"\nVERSION_ID=9.6\n", DistroFamilyRHEL, PackageManagerDNF, []string{"dnf", "rpm", "systemctl"}},
		{"arch", "ID=arch\nVERSION_ID=rolling\n", DistroFamilyArch, PackageManagerPacman, []string{"pacman", "systemctl"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := readyProbe(tt.binaries...)
			if tt.manager == PackageManagerDNF {
				probe = dnfReadyProbe(tt.binaries...)
			}
			profile, err := DetectWith([]byte(tt.release), probe)
			if err != nil {
				t.Fatal(err)
			}
			if profile.DistroFamily != tt.family || profile.PackageManager != tt.manager || profile.ServiceManager != ServiceManagerSystemd {
				t.Fatalf("profile = %+v, want family=%s manager=%s systemd", profile, tt.family, tt.manager)
			}
			if profile.ID == "" || profile.Arch != "amd64" {
				t.Fatalf("profile identity = %+v", profile)
			}
			if profile.Executables["timeout"] != "/usr/bin/timeout" {
				t.Fatalf("profile timeout executable = %q", profile.Executables["timeout"])
			}
		})
	}
}

func TestDetectWithAcceptsDerivativeIDsUsingUnambiguousFamilyEvidence(t *testing.T) {
	tests := []struct {
		id, idLike string
		want       PackageManager
	}{
		{id: "linuxmint", idLike: "ubuntu debian", want: PackageManagerAPT},
		{id: "amzn", idLike: "fedora", want: PackageManagerDNF},
		{id: "parrot", idLike: "debian", want: PackageManagerAPT},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			probe := readyProbe(
				"apt-get", "apt-cache", "dpkg-query",
				"dnf", "rpm", "pacman", "systemctl",
			)
			if test.want == PackageManagerDNF {
				probe = dnfReadyProbe(
					"apt-get", "apt-cache", "dpkg-query",
					"dnf", "rpm", "pacman", "systemctl",
				)
			}
			profile, err := DetectWith(
				[]byte("ID="+test.id+"\nID_LIKE=\""+test.idLike+"\"\n"),
				probe,
			)
			if err != nil {
				t.Fatal(err)
			}
			if profile.ID != test.id || profile.PackageManager != test.want {
				t.Fatalf("profile = %+v, want metadata ID %q and manager %s", profile, test.id, test.want)
			}
		})
	}
}

func TestDetectWithCanonicalizesArchitectureAliases(t *testing.T) {
	for input, want := range map[string]string{
		"amd64":   "amd64",
		"x86_64":  "amd64",
		"arm64":   "arm64",
		"aarch64": "arm64",
	} {
		probe := readyProbe("pacman", "systemctl")
		probe.Architecture = func() (string, error) { return input, nil }
		profile, err := DetectWith([]byte("ID=arch\n"), probe)
		if err != nil {
			t.Fatal(err)
		}
		if profile.Arch != want {
			t.Fatalf("architecture %q = %q, want %q", input, profile.Arch, want)
		}
	}
	for _, input := range []string{"386", "i686", "arm", "armv7l", "riscv64", "bad arch"} {
		probe := readyProbe("pacman", "systemctl")
		probe.Architecture = func() (string, error) { return input, nil }
		if _, err := DetectWith([]byte("ID=arch\n"), probe); err == nil ||
			!strings.Contains(err.Error(), "unsupported host architecture") {
			t.Fatalf("architecture %q error = %v, want unsupported rejection", input, err)
		}
	}
}

func TestDetectWithRoutesFormerlyRejectedNamesByCapability(t *testing.T) {
	tests := []struct {
		name, release string
		binaries      []string
		wantFamily    DistroFamily
		wantManager   PackageManager
	}{
		{
			name: "kali with Debian evidence", release: "ID=kali\nID_LIKE=debian\n",
			binaries:   []string{"apt-get", "apt-cache", "dpkg-query", "systemctl"},
			wantFamily: DistroFamilyDebian, wantManager: PackageManagerAPT,
		},
		{
			name: "openSUSE with unique dnf capability", release: "ID=opensuse\nID_LIKE=suse\n",
			binaries:   []string{"dnf", "rpm", "systemctl"},
			wantFamily: DistroFamilyRHEL, wantManager: PackageManagerDNF,
		},
		{
			name: "Alpine with unique pacman capability", release: "ID=alpine\n",
			binaries:   []string{"pacman", "systemctl"},
			wantFamily: DistroFamilyArch, wantManager: PackageManagerPacman,
		},
		{
			name: "NixOS with unique apt capability", release: "ID=nixos\n",
			binaries:   []string{"apt-get", "apt-cache", "dpkg-query", "systemctl"},
			wantFamily: DistroFamilyDebian, wantManager: PackageManagerAPT,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probe := readyProbe(test.binaries...)
			if test.wantManager == PackageManagerDNF {
				probe = dnfReadyProbe(test.binaries...)
			}
			profile, err := DetectWith([]byte(test.release), probe)
			if err != nil {
				t.Fatal(err)
			}
			if profile.DistroFamily != test.wantFamily || profile.PackageManager != test.wantManager {
				t.Fatalf("profile = %+v, want family=%s manager=%s", profile, test.wantFamily, test.wantManager)
			}
		})
	}
}

func TestDetectWithConflictingEvidenceFallsBackToUniqueCompleteToolchain(t *testing.T) {
	profile, err := DetectWith(
		[]byte("ID=exampleos\nID_LIKE=\"debian rhel\"\n"),
		readyProbe("apt-get", "apt-cache", "dpkg-query", "systemctl"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if profile.DistroFamily != DistroFamilyDebian || profile.PackageManager != PackageManagerAPT {
		t.Fatalf("profile = %+v, want uniquely complete apt capability", profile)
	}
}

func TestDetectWithCapabilityDiscoveryRejectsAmbiguousManagers(t *testing.T) {
	for _, release := range []string{
		"ID=exampleos\n",
		"ID=exampleos\nID_LIKE=\"debian rhel\"\n",
	} {
		_, err := DetectWith(
			[]byte(release),
			readyProbe(
				"apt-get", "apt-cache", "dpkg-query",
				"dnf", "rpm", "systemctl",
			),
		)
		if err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("release %q error = %v, want ambiguous capability rejection", release, err)
		}
	}
}

func TestDetectWithUnknownFamilyRequiresACompleteTrustedToolchain(t *testing.T) {
	_, err := DetectWith(
		[]byte("ID=exampleos\n"),
		readyProbe("apt-get", "dpkg-query", "systemctl"),
	)
	if err == nil || !strings.Contains(err.Error(), "no complete trusted") {
		t.Fatalf("error = %v, want incomplete capability rejection", err)
	}
}

func TestParseOSReleaseRejectsDuplicateAndMalformedData(t *testing.T) {
	tests := []string{
		"ID=debian\nID=ubuntu\n",
		"ID =debian\n",
		" ID=debian\n",
		"ID=\"debian\" trailing\n",
		"ID=debian;echo-owned\n",
		"ID=debian\nID_LIKE=\"ubuntu\\q\"\n",
		"ID=debian\x00\n",
	}
	for _, data := range tests {
		if release, err := ParseOSRelease([]byte(data)); err == nil {
			t.Fatalf("ParseOSRelease(%q) = %+v, want error", data, release)
		}
	}
}

func TestParseOSReleaseReadsDataWithoutShellEvaluation(t *testing.T) {
	data := []byte("NAME=\"Example Linux\"\nID=example\nID_LIKE='debian ubuntu'\nVERSION_ID=\"1.2\"\nVERSION_CODENAME=stable\n")
	release, err := ParseOSRelease(data)
	if err != nil {
		t.Fatal(err)
	}
	want := OSRelease{ID: "example", IDLike: []string{"debian", "ubuntu"}, Version: "1.2", Codename: "stable"}
	if !reflect.DeepEqual(release, want) {
		t.Fatalf("release = %+v, want %+v", release, want)
	}
}

func TestDetectWithIgnoresExtraForeignPackageManager(t *testing.T) {
	profile, err := DetectWith([]byte("ID=debian\n"), readyProbe("apt-get", "apt-cache", "dpkg-query", "dnf", "rpm", "pacman", "systemctl"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.PackageManager != PackageManagerAPT {
		t.Fatalf("manager = %s, want apt selected from release family", profile.PackageManager)
	}
	for _, foreign := range []string{"dnf", "rpm", "pacman"} {
		if _, retained := profile.Executables[foreign]; retained {
			t.Fatalf("foreign executable %q leaked into selected profile: %+v", foreign, profile.Executables)
		}
	}

	_, err = DetectWith([]byte("ID=debian\n"), readyProbe("dnf", "rpm", "systemctl"))
	if err == nil || !strings.Contains(err.Error(), "requires apt-get") {
		t.Fatalf("error = %v, want missing apt-get; foreign dnf must not be used", err)
	}
}

func TestDetectWithRejectsUnsafeForeignPackageManagerBeforeRouting(t *testing.T) {
	probe := readyProbe(
		"apt-get", "apt-cache", "dpkg-query",
		"dnf", "systemctl",
	)
	probe.ValidateExecutable = func(binary, path string) error {
		if binary == "dnf" {
			return fmt.Errorf("%s is a symbolic vendor-path escape", path)
		}
		return nil
	}

	_, err := DetectWith([]byte("ID=debian\n"), probe)
	if err == nil || !strings.Contains(err.Error(), "untrusted present dnf executable") {
		t.Fatalf("error = %v, want unsafe foreign dnf rejection", err)
	}
}

func TestDetectWithRejectsUntrustedExecutablePath(t *testing.T) {
	probe := readyProbe("pacman", "systemctl")
	probe.ValidateExecutable = func(binary, path string) error {
		if binary == "pacman" {
			return fmt.Errorf("path %s is not trusted", path)
		}
		return nil
	}
	_, err := DetectWith([]byte("ID=arch\n"), probe)
	if err == nil || !strings.Contains(err.Error(), "untrusted present pacman executable") {
		t.Fatalf("error = %v, want untrusted executable rejection", err)
	}
}

func TestDetectWithFailsClosedForIncompleteProbe(t *testing.T) {
	tests := []struct {
		name   string
		remove func(*Probe)
	}{
		{"executable-presence", func(probe *Probe) { probe.ExecutablePresent = nil }},
		{"fixed-path-lookup", func(probe *Probe) { probe.LookPath = nil }},
		{"executable-validation", func(probe *Probe) { probe.ValidateExecutable = nil }},
		{"systemd-readiness", func(probe *Probe) { probe.SystemdReady = nil }},
		{"security-policy-state", func(probe *Probe) { probe.SecurityPolicyState = nil }},
		{"dnf-security-proof", func(probe *Probe) { probe.DNFSecurityReady = nil }},
		{"architecture", func(probe *Probe) { probe.Architecture = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probe := readyProbe("pacman", "systemctl")
			test.remove(&probe)
			_, err := DetectWith([]byte("ID=arch\n"), probe)
			if err == nil || !strings.Contains(err.Error(), "probe is incomplete") {
				t.Fatalf("error = %v, want incomplete-probe rejection", err)
			}
		})
	}
}

func TestDetectWithFailsClosedForMissingToolsAndSystemd(t *testing.T) {
	_, err := DetectWith([]byte("ID=arch\n"), readyProbe("systemctl"))
	if err == nil || !strings.Contains(err.Error(), "requires pacman") {
		t.Fatalf("missing tool error = %v", err)
	}

	probe := readyProbe("pacman", "systemctl")
	probe.SystemdReady = func(string) error { return errors.New("offline") }
	_, err = DetectWith([]byte("ID=arch\n"), probe)
	if err == nil || !strings.Contains(err.Error(), "systemd is not ready") {
		t.Fatalf("systemd error = %v", err)
	}
}

func TestDetectWithRequiresFixedTimeout(t *testing.T) {
	probe := readyProbe("pacman", "systemctl")
	present := probe.ExecutablePresent
	probe.ExecutablePresent = func(binary string) (bool, error) {
		if binary == "timeout" {
			return false, nil
		}
		return present(binary)
	}
	_, err := DetectWith([]byte("ID=arch\n"), probe)
	if err == nil || !strings.Contains(err.Error(), "requires timeout") {
		t.Fatalf("error = %v, want missing fixed timeout rejection", err)
	}
}

func TestDetectWithRejectsMalformedLiveSecurityPolicy(t *testing.T) {
	probe := readyProbe("pacman", "systemctl")
	probe.SecurityPolicyState = func() (SecurityPolicyState, error) {
		return "", errors.New("SELinux state is malformed")
	}
	_, err := DetectWith([]byte("ID=arch\n"), probe)
	if err == nil || !strings.Contains(err.Error(), "inspect host security policy") ||
		!strings.Contains(err.Error(), "malformed") {
		t.Fatalf("error = %v, want malformed live security-policy rejection", err)
	}
}

func TestDetectWithAppliesManagerSpecificSecurityPolicy(t *testing.T) {
	for _, test := range []struct {
		name    string
		release string
		probe   Probe
		state   SecurityPolicyState
		wantErr string
	}{
		{
			name: "apt rejects enforcing", release: "ID=debian\n",
			probe: readyProbe("apt-get", "apt-cache", "dpkg-query", "systemctl"),
			state: SecurityPolicyEnforcing, wantErr: "apt mutations require inactive SELinux",
		},
		{
			name: "apt rejects permissive", release: "ID=debian\n",
			probe: readyProbe("apt-get", "apt-cache", "dpkg-query", "systemctl"),
			state: SecurityPolicyPermissive, wantErr: "apt mutations require inactive SELinux",
		},
		{
			name: "pacman rejects enforcing", release: "ID=arch\n",
			probe: readyProbe("pacman", "systemctl"),
			state: SecurityPolicyEnforcing, wantErr: "pacman mutations require inactive SELinux",
		},
		{
			name: "dnf rejects inactive", release: "ID=rhel\n",
			probe: dnfReadyProbe("dnf", "rpm", "systemctl"),
			state: SecurityPolicyInactive, wantErr: "DNF preview requires SELinux enforcing",
		},
		{
			name: "dnf rejects permissive", release: "ID=rhel\n",
			probe: dnfReadyProbe("dnf", "rpm", "systemctl"),
			state: SecurityPolicyPermissive, wantErr: "DNF preview requires SELinux enforcing",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := test.probe
			probe.SecurityPolicyState = func() (SecurityPolicyState, error) {
				return test.state, nil
			}
			_, err := DetectWith([]byte(test.release), probe)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestDetectWithDNFPreviewRequiresAndPublishesSecurityProof(t *testing.T) {
	probe := dnfReadyProbe("dnf", "rpm", "systemctl")
	getenforceCalls := 0
	probe.DNFSecurityReady = func(path string) error {
		getenforceCalls++
		if path != "/usr/sbin/getenforce" {
			t.Fatalf("getenforce path = %q", path)
		}
		return nil
	}
	profile, err := DetectWith([]byte("ID=rhel\n"), probe)
	if err != nil {
		t.Fatal(err)
	}
	if getenforceCalls != 1 {
		t.Fatalf("getenforce proof calls = %d, want 1", getenforceCalls)
	}
	for _, binary := range []string{"restorecon", "matchpathcon", "getenforce"} {
		if profile.Executables[binary] != "/usr/sbin/"+binary {
			t.Fatalf("DNF profile executable %s = %q", binary, profile.Executables[binary])
		}
	}

	missing := dnfReadyProbe("dnf", "rpm", "systemctl")
	present := missing.ExecutablePresent
	missing.ExecutablePresent = func(binary string) (bool, error) {
		if binary == "matchpathcon" {
			return false, nil
		}
		return present(binary)
	}
	if _, err := DetectWith([]byte("ID=rhel\n"), missing); err == nil ||
		!strings.Contains(err.Error(), "requires matchpathcon") {
		t.Fatalf("missing DNF security tool error = %v", err)
	}

	failed := dnfReadyProbe("dnf", "rpm", "systemctl")
	failed.DNFSecurityReady = func(string) error { return errors.New("Permissive") }
	if _, err := DetectWith([]byte("ID=rhel\n"), failed); err == nil ||
		!strings.Contains(err.Error(), "DNF preview security-policy proof failed") {
		t.Fatalf("failed getenforce proof error = %v", err)
	}
}
