//go:build linux

package bindroot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

func testVendorProfile(manager hostplatform.PackageManager) hostplatform.Profile {
	profile := hostplatform.Profile{PackageManager: manager, ServiceManager: hostplatform.ServiceManagerSystemd}
	if manager == hostplatform.PackageManagerAPT {
		profile.DistroFamily = hostplatform.DistroFamilyDebian
	} else {
		profile.DistroFamily = hostplatform.DistroFamilyArch
	}
	return profile
}

func TestCertifiedVendorContractRejectsMismatchedHost(t *testing.T) {
	for _, manager := range []hostplatform.PackageManager{hostplatform.PackageManagerAPT, hostplatform.PackageManagerPacman} {
		profile := testVendorProfile(manager)
		contract, err := CertifiedVendorContract(profile)
		if err != nil || contract.UnitPath != "/usr/lib/systemd/system/named.service" || len(contract.UnitBytes) == 0 {
			t.Fatalf("valid vendor contract: %+v %v", contract, err)
		}
		if manager == hostplatform.PackageManagerAPT && contract.EnvironmentPath != "/etc/default/named" {
			t.Fatal("APT startup options omitted")
		}
		profile.ServiceManager = "openrc"
		if _, err := CertifiedVendorContract(profile); err == nil {
			t.Fatal("non-systemd vendor contract accepted")
		}
		profile = testVendorProfile(manager)
		profile.DistroFamily = hostplatform.DistroFamilyRHEL
		if _, err := CertifiedVendorContract(profile); err == nil {
			t.Fatal("mismatched package family accepted")
		}
	}
}

func TestVendorPackageOwnershipIsExact(t *testing.T) {
	for _, path := range []string{"/usr/lib/systemd/system/named.service", "/etc/default/named"} {
		if err := VerifyAPTVendorOwner(path, []byte("bind9: "+path+"\n"), nil); err != nil {
			t.Fatal(err)
		}
		if err := VerifyAPTVendorOwner(path, []byte("bind9: "+path+"\n"), errors.New("failed")); err == nil {
			t.Fatal("failed APT query accepted")
		}
		if err := VerifyAPTVendorOwner(path, []byte("other: "+path+"\n"), nil); err == nil {
			t.Fatal("different APT owner accepted")
		}
	}
	if err := VerifyAPTVendorOwner("/tmp/named.service", []byte("bind9: /tmp/named.service\n"), nil); err == nil {
		t.Fatal("unlisted APT path accepted")
	}
	const pacmanPath = "/usr/lib/systemd/system/named.service"
	if err := VerifyPacmanVendorOwner(pacmanPath, []byte("bind\n"), nil); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"bind", "bind\nother\n", "unbound\n"} {
		if err := VerifyPacmanVendorOwner(pacmanPath, []byte(output), nil); err == nil {
			t.Fatalf("noncanonical pacman output accepted: %q", output)
		}
	}
	if err := VerifyPacmanVendorOwner(pacmanPath, []byte("bind\n"), errors.New("failed")); err == nil {
		t.Fatal("failed pacman query accepted")
	}
}

func TestInspectVendorRejectsFileReplacementBetweenReads(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned vendor fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	contract, err := CertifiedVendorContract(testVendorProfile(hostplatform.PackageManagerAPT))
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"usr/lib/systemd/system", "etc/default"} {
		if err := os.MkdirAll(filepath.Join(root, relative), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []struct {
		path string
		data []byte
	}{
		{contract.UnitPath, contract.UnitBytes},
		{contract.EnvironmentPath, contract.EnvironmentBytes},
	} {
		if err := os.WriteFile(filepath.Join(root, file.path), file.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := inspectVendorAt(fd, contract, nil); err != nil {
		t.Fatalf("certified vendor files rejected: %v", err)
	}
	environment := filepath.Join(root, contract.EnvironmentPath)
	if _, err := inspectVendorAt(fd, contract, func() {
		if writeErr := os.WriteFile(environment, []byte("OPTIONS=\"-u root\"\n"), 0o644); writeErr != nil {
			t.Error(writeErr)
		}
	}); err == nil {
		t.Fatal("vendor environment replacement accepted")
	}
	if _, err := InspectInstalledVendor(context.Background(), hostplatform.Profile{}); err == nil {
		t.Fatal("unsupported installed host profile accepted")
	}
}
