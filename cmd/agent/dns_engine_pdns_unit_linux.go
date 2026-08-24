//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

const certifiedPDNSUnitPackageOwner = "pdns-server: /usr/lib/systemd/system/pdns.service\n"

func inspectHostPDNSVendorUnit(
	ctx context.Context,
	profile hostplatform.Profile,
) (bindSecureFileIdentity, error) {
	if ctx == nil {
		return bindSecureFileIdentity{},
			errors.New("PowerDNS vendor proof requires a context")
	}
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return bindSecureFileIdentity{}, err
	}
	dpkgQuery, err := firstTrustedExecutable(
		[]string{"/usr/bin/dpkg-query", "/usr/sbin/dpkg-query"}, "dpkg-query",
	)
	if err != nil {
		return bindSecureFileIdentity{}, err
	}
	rootFD, err := unix.Open(
		"/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return bindSecureFileIdentity{}, fmt.Errorf("open PowerDNS vendor proof root: %w", err)
	}
	defer unix.Close(rootFD)
	lookup := func(lookupCtx context.Context) ([]byte, error) {
		command := serviceMutationCommand(
			lookupCtx, dpkgQuery, "-S", "--", certifiedPDNSUnitPath,
		)
		command.Env = aptBINDStatOverrideCommandEnvironment()
		return command.CombinedOutputLimited(4 << 10)
	}
	if err := verifyExactPDNSVendorPackageOwnership(ctx, lookup); err != nil {
		return bindSecureFileIdentity{}, err
	}
	identity, err := inspectPDNSVendorUnitAt(rootFD, profile, nil)
	if err != nil {
		return bindSecureFileIdentity{}, err
	}
	if err := verifyExactPDNSVendorPackageOwnership(ctx, lookup); err != nil {
		return bindSecureFileIdentity{}, err
	}
	return identity, nil
}

type pdnsVendorOwnerLookup func(context.Context) ([]byte, error)

func verifyExactPDNSVendorPackageOwnership(
	ctx context.Context,
	lookup pdnsVendorOwnerLookup,
) error {
	if ctx == nil || lookup == nil {
		return errors.New("invalid PowerDNS vendor package ownership proof")
	}
	output, err := lookup(ctx)
	if err != nil {
		return fmt.Errorf("verify PowerDNS unit package ownership: %w", err)
	}
	if string(output) != certifiedPDNSUnitPackageOwner {
		return errors.New(
			"PowerDNS unit is not owned by the exact pdns-server package",
		)
	}
	return nil
}

func inspectPDNSVendorUnitAt(
	rootFD int,
	profile hostplatform.Profile,
	afterFirstSnapshot func(),
) (bindSecureFileIdentity, error) {
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return bindSecureFileIdentity{}, err
	}
	readUnit := func() (bindSecureFileIdentity, error) {
		data, identity, err := readExactRootOwnedBINDFileAt(
			rootFD, certifiedPDNSUnitPath, "PowerDNS vendor unit",
		)
		if err != nil {
			return bindSecureFileIdentity{}, err
		}
		debian := bytes.Equal(data, []byte(certifiedDebian13PDNSVendorUnit))
		ubuntu := bytes.Equal(data, []byte(certifiedUbuntu2404PDNSVendorUnit))
		if !debian && !ubuntu {
			return bindSecureFileIdentity{},
				errors.New(
					"PowerDNS vendor unit bytes differ from the certified package unit",
				)
		}
		return identity, nil
	}
	first, err := readUnit()
	if err != nil {
		return bindSecureFileIdentity{}, err
	}
	if afterFirstSnapshot != nil {
		afterFirstSnapshot()
	}
	second, err := readUnit()
	if err != nil {
		return bindSecureFileIdentity{}, err
	}
	if second != first {
		return bindSecureFileIdentity{},
			errors.New("PowerDNS vendor unit changed during exact verification")
	}
	return second, nil
}
