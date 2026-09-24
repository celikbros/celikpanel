//go:build linux

package bindroot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

type VendorFilesIdentity struct {
	Unit        FileIdentity
	Environment FileIdentity
}

// VerifyAPTVendorOwner accepts only the two certified bind9-owned vendor files.
func VerifyAPTVendorOwner(path string, output []byte, commandErr error) error {
	if commandErr != nil {
		return fmt.Errorf("verify BIND vendor package ownership for %s: %w", path, commandErr)
	}
	switch path {
	case "/usr/lib/systemd/system/named.service", "/etc/default/named":
		if string(output) != "bind9: "+path+"\n" {
			return fmt.Errorf("%s is not owned by the exact bind9 package", path)
		}
		return nil
	default:
		return errors.New("unsupported APT BIND vendor file")
	}
}

func VerifyPacmanVendorOwner(path string, output []byte, commandErr error) error {
	if commandErr != nil {
		return fmt.Errorf("verify BIND vendor package ownership for %s: %w", path, commandErr)
	}
	if path != "/usr/lib/systemd/system/named.service" || string(output) != "bind\n" {
		return errors.New("BIND vendor unit is not owned by the exact bind package")
	}
	return nil
}

func proveInstalledVendorPackage(ctx context.Context, profile hostplatform.Profile) error {
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		for _, path := range []string{"/usr/lib/systemd/system/named.service", "/etc/default/named"} {
			output, err := runTrusted(ctx, []string{"/usr/bin/dpkg-query", "/usr/sbin/dpkg-query"}, "-S", "--", path)
			if err := VerifyAPTVendorOwner(path, output, err); err != nil {
				return err
			}
		}
	case hostplatform.PackageManagerPacman:
		const path = "/usr/lib/systemd/system/named.service"
		output, err := runTrusted(ctx, []string{"/usr/bin/pacman", "/usr/sbin/pacman"}, "--query", "--quiet", "--owns", "--", path)
		if err := VerifyPacmanVendorOwner(path, output, err); err != nil {
			return err
		}
	default:
		return errors.New("unsupported BIND vendor package proof")
	}
	return nil
}

// InspectInstalledVendor proves the fixed package-owned vendor unit and, on
// APT, its effective environment across two no-follow descriptor reads. It
// does not prove systemd loaded these bytes or that named serves the selected
// generation.
func InspectInstalledVendor(ctx context.Context, profile hostplatform.Profile) (VendorFilesIdentity, error) {
	if ctx == nil {
		return VendorFilesIdentity{}, errors.New("BIND vendor observation requires a context")
	}
	contract, err := CertifiedVendorContract(profile)
	if err != nil {
		return VendorFilesIdentity{}, err
	}
	proofCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return VendorFilesIdentity{}, fmt.Errorf("open BIND vendor proof root: %w", err)
	}
	defer unix.Close(rootFD)
	if err := proveInstalledVendorPackage(proofCtx, profile); err != nil {
		return VendorFilesIdentity{}, err
	}
	identity, err := inspectVendorAt(rootFD, contract, nil)
	if err != nil {
		return VendorFilesIdentity{}, err
	}
	if err := proveInstalledVendorPackage(proofCtx, profile); err != nil {
		return VendorFilesIdentity{}, err
	}
	return identity, nil
}

func inspectVendorAt(rootFD int, contract VendorContract, between func()) (VendorFilesIdentity, error) {
	first, err := vendorSnapshotAt(rootFD, contract)
	if err != nil {
		return VendorFilesIdentity{}, err
	}
	if between != nil {
		between()
	}
	second, err := vendorSnapshotAt(rootFD, contract)
	if err != nil {
		return VendorFilesIdentity{}, err
	}
	if first != second {
		return VendorFilesIdentity{}, errors.New("BIND vendor unit files changed during exact verification")
	}
	return second, nil
}

func vendorSnapshotAt(rootFD int, contract VendorContract) (VendorFilesIdentity, error) {
	unit, unitIdentity, err := ReadExactRootOwnedFileAt(rootFD, contract.UnitPath, "BIND vendor unit")
	if err != nil {
		return VendorFilesIdentity{}, err
	}
	if !bytes.Equal(unit, contract.UnitBytes) {
		return VendorFilesIdentity{}, errors.New("BIND vendor unit bytes differ from the certified package unit")
	}
	identity := VendorFilesIdentity{Unit: unitIdentity}
	if contract.EnvironmentPath == "" {
		return identity, nil
	}
	environment, environmentIdentity, err := ReadExactRootOwnedFileAt(rootFD, contract.EnvironmentPath, "BIND vendor environment")
	if err != nil {
		return VendorFilesIdentity{}, err
	}
	if !bytes.Equal(environment, contract.EnvironmentBytes) {
		return VendorFilesIdentity{}, errors.New("BIND vendor environment bytes differ from the certified safe options")
	}
	identity.Environment = environmentIdentity
	return identity, nil
}
