//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

type bindVendorFileContract struct {
	unitPath         string
	environmentPath  string
	unitBytes        []byte
	environmentBytes []byte
}

const certifiedAPTBINDVendorUnit = bindroot.CertifiedAPTBINDVendorUnit
const certifiedAPTBINDVendorEnvironment = bindroot.CertifiedAPTBINDVendorEnvironment
const certifiedPacmanBINDVendorUnit = bindroot.CertifiedPacmanBINDVendorUnit

func bindVendorContract(profile hostplatform.Profile) (bindVendorFileContract, error) {
	shared, err := bindroot.CertifiedVendorContract(profile)
	if err != nil {
		return bindVendorFileContract{}, err
	}
	return bindVendorFileContract{
		unitPath: shared.UnitPath, environmentPath: shared.EnvironmentPath,
		unitBytes: shared.UnitBytes, environmentBytes: shared.EnvironmentBytes,
	}, nil
}

func inspectHostBINDVendorFiles(
	ctx context.Context,
	profile hostplatform.Profile,
) (bindVendorFilesIdentity, error) {
	if ctx == nil {
		return bindVendorFilesIdentity{},
			errors.New("BIND vendor proof requires a context")
	}
	rootFD, err := unix.Open(
		"/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return bindVendorFilesIdentity{}, fmt.Errorf("open BIND vendor proof root: %w", err)
	}
	defer unix.Close(rootFD)
	var verifyPackageOwnership func(context.Context) error
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		dpkgQuery, err := firstTrustedExecutable(
			[]string{"/usr/bin/dpkg-query", "/usr/sbin/dpkg-query"}, "dpkg-query",
		)
		if err != nil {
			return bindVendorFilesIdentity{}, err
		}
		lookup := func(lookupCtx context.Context, filename string) ([]byte, error) {
			command := serviceMutationCommand(
				lookupCtx, dpkgQuery, "-S", "--", filename,
			)
			command.Env = aptBINDStatOverrideCommandEnvironment()
			return command.CombinedOutputLimited(4 << 10)
		}
		verifyPackageOwnership = func(verifyCtx context.Context) error {
			return verifyExactAPTBINDVendorPackageOwnership(verifyCtx, lookup)
		}
	case hostplatform.PackageManagerPacman:
		pacman, err := firstTrustedExecutable(
			[]string{"/usr/bin/pacman", "/usr/sbin/pacman"}, "pacman",
		)
		if err != nil {
			return bindVendorFilesIdentity{}, err
		}
		lookup := func(lookupCtx context.Context, filename string) ([]byte, error) {
			return serviceMutationCommand(
				lookupCtx, pacman, "--query", "--quiet", "--owns", "--", filename,
			).CombinedOutputLimited(4 << 10)
		}
		verifyPackageOwnership = func(verifyCtx context.Context) error {
			return verifyExactPacmanBINDVendorPackageOwnership(verifyCtx, lookup)
		}
	default:
		return bindVendorFilesIdentity{}, errors.New(
			"BIND vendor package ownership proof is unsupported on this package manager",
		)
	}
	if err := verifyPackageOwnership(ctx); err != nil {
		return bindVendorFilesIdentity{}, err
	}
	identity, err := inspectBINDVendorFilesAt(rootFD, profile, nil)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if err := verifyPackageOwnership(ctx); err != nil {
		return bindVendorFilesIdentity{}, err
	}
	return identity, nil
}

type aptBINDVendorOwnerLookup func(context.Context, string) ([]byte, error)

func verifyExactAPTBINDVendorPackageOwnership(
	ctx context.Context,
	lookup aptBINDVendorOwnerLookup,
) error {
	if ctx == nil || lookup == nil {
		return errors.New("invalid APT BIND vendor package ownership proof")
	}
	for _, path := range []string{"/usr/lib/systemd/system/named.service", "/etc/default/named"} {
		output, err := lookup(ctx, path)
		if err := bindroot.VerifyAPTVendorOwner(path, output, err); err != nil {
			return err
		}
	}
	return nil
}

func verifyExactPacmanBINDVendorPackageOwnership(
	ctx context.Context,
	lookup aptBINDVendorOwnerLookup,
) error {
	if ctx == nil || lookup == nil {
		return errors.New("invalid pacman BIND vendor package ownership proof")
	}
	const unit = "/usr/lib/systemd/system/named.service"
	output, err := lookup(ctx, unit)
	return bindroot.VerifyPacmanVendorOwner(unit, output, err)
}

// inspectBINDVendorFilesAt reads the package unit and its effective APT
// environment through no-follow descriptors, then repeats the complete read.
// The callback is a test-only TOCTOU seam executed between the two snapshots.
func inspectBINDVendorFilesAt(
	rootFD int,
	profile hostplatform.Profile,
	afterFirstSnapshot func(),
) (bindVendorFilesIdentity, error) {
	contract, err := bindVendorContract(profile)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	first, err := readBINDVendorFilesSnapshotAt(rootFD, contract)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if afterFirstSnapshot != nil {
		afterFirstSnapshot()
	}
	second, err := readBINDVendorFilesSnapshotAt(rootFD, contract)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if first != second {
		return bindVendorFilesIdentity{},
			errors.New("BIND vendor unit files changed during exact verification")
	}
	return second, nil
}

func readBINDVendorFilesSnapshotAt(
	rootFD int,
	contract bindVendorFileContract,
) (bindVendorFilesIdentity, error) {
	unit, unitIdentity, err := readExactRootOwnedBINDFileAt(
		rootFD, contract.unitPath, "BIND vendor unit",
	)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if !bytes.Equal(unit, contract.unitBytes) {
		return bindVendorFilesIdentity{},
			errors.New("BIND vendor unit bytes differ from the certified package unit")
	}
	identity := bindVendorFilesIdentity{Unit: unitIdentity}
	if contract.environmentPath == "" {
		return identity, nil
	}
	environment, environmentIdentity, err := readExactRootOwnedBINDFileAt(
		rootFD, contract.environmentPath, "BIND vendor environment",
	)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if !bytes.Equal(environment, contract.environmentBytes) {
		return bindVendorFilesIdentity{},
			errors.New("BIND vendor environment bytes differ from the certified safe options")
	}
	identity.Environment = environmentIdentity
	return identity, nil
}

func readExactRootOwnedBINDFileAt(
	rootFD int,
	absolutePath string,
	label string,
) ([]byte, bindSecureFileIdentity, error) {
	data, observed, err := bindroot.ReadExactRootOwnedFileAt(rootFD, absolutePath, label)
	if err != nil {
		return nil, bindSecureFileIdentity{}, err
	}
	return data, bindSecureFileIdentity{
		Device: observed.Device, Inode: observed.Inode,
		Size: observed.Size, Digest: observed.Digest,
	}, nil
}
