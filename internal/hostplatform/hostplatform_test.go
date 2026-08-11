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
	return Probe{
		LookPath: func(binary string) (string, error) {
			if !available[binary] {
				return "", fmt.Errorf("%s not found", binary)
			}
			return "/usr/bin/" + binary, nil
		},
		ValidateExecutable: func(string, string) error { return nil },
		SystemdReady:       func(string) error { return nil },
		Architecture:       func() (string, error) { return "x86_64", nil },
	}
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
			profile, err := DetectWith([]byte(tt.release), readyProbe(tt.binaries...))
			if err != nil {
				t.Fatal(err)
			}
			if profile.DistroFamily != tt.family || profile.PackageManager != tt.manager || profile.ServiceManager != ServiceManagerSystemd {
				t.Fatalf("profile = %+v, want family=%s manager=%s systemd", profile, tt.family, tt.manager)
			}
			if profile.ID == "" || profile.Arch != "amd64" {
				t.Fatalf("profile identity = %+v", profile)
			}
		})
	}
}

func TestDetectWithRejectsCompatibleButUnverifiedDerivativeIDs(t *testing.T) {
	for id, idLike := range map[string]string{
		"linuxmint": "ubuntu debian",
		"amzn":      "fedora",
		"parrot":    "debian",
	} {
		_, err := DetectWith([]byte("ID="+id+"\nID_LIKE=\""+idLike+"\"\n"), readyProbe("apt-get", "apt-cache", "dpkg-query", "dnf", "rpm", "systemctl"))
		if err == nil || !strings.Contains(err.Error(), "distro ID is unverified") {
			t.Fatalf("%s error = %v, want compatible-but-unverified rejection", id, err)
		}
	}
}

func TestDetectWithCanonicalizesArchitectureAliases(t *testing.T) {
	for input, want := range map[string]string{
		"x86_64":  "amd64",
		"aarch64": "arm64",
		"i686":    "386",
		"armv7l":  "arm",
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
}

func TestDetectWithRejectsUnsupportedDistributionFamilies(t *testing.T) {
	for _, id := range []string{"kali", "opensuse", "alpine", "nixos"} {
		t.Run(id, func(t *testing.T) {
			_, err := DetectWith([]byte("ID="+id+"\nID_LIKE=debian\n"), readyProbe("apt-get", "dpkg-query", "systemctl"))
			if err == nil || !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("error = %v, want explicit unsupported error", err)
			}
		})
	}
}

func TestDetectWithRejectsConflictingFamilyEvidence(t *testing.T) {
	_, err := DetectWith([]byte("ID=exampleos\nID_LIKE=\"debian rhel\"\n"), readyProbe("apt-get", "dpkg-query", "dnf", "rpm", "systemctl"))
	if err == nil || !strings.Contains(err.Error(), "conflicting family evidence") {
		t.Fatalf("error = %v, want conflicting family evidence", err)
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

	_, err = DetectWith([]byte("ID=debian\n"), readyProbe("dnf", "rpm", "systemctl"))
	if err == nil || !strings.Contains(err.Error(), "requires apt-get") {
		t.Fatalf("error = %v, want missing apt-get; foreign dnf must not be used", err)
	}
}

func TestDetectWithRejectsUntrustedExecutablePath(t *testing.T) {
	probe := readyProbe("pacman", "systemctl")
	probe.LookPath = func(binary string) (string, error) { return "/tmp/" + binary, nil }
	probe.ValidateExecutable = func(_ string, path string) error {
		return fmt.Errorf("path %s is not trusted", path)
	}
	_, err := DetectWith([]byte("ID=arch\n"), probe)
	if err == nil || !strings.Contains(err.Error(), "untrusted pacman executable") {
		t.Fatalf("error = %v, want untrusted executable rejection", err)
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
