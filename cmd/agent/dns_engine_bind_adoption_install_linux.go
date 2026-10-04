//go:build linux

package main

import (
	"bytes"
	"errors"

	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func retireExactRunningBINDAdoptionInstall(journal dnsEngineSwitchJournal, expected dnsEngineInstallOwnershipReceipt) error {
	path, err := dnsEngineInstallOwnershipPath(transport.DNSEngineBIND)
	if err != nil {
		return err
	}
	owner := servicemutationledger.FileOwner{UID: serviceMutationRequiredOwnerUID, GID: serviceMutationRequiredOwnerGID}
	const limit int64 = 64 << 10
	return retireRunningBINDAdoptionInstallWithOps(journal, expected,
		func() ([]byte, bool, error) { return servicemutationledger.ReadFile(path, limit, owner) },
		func(raw []byte) error { return servicemutationledger.RemoveFileExact(path, raw, limit, owner) },
	)
}

// Publish only into an absent path. The final rename is NOREPLACE, so an owner
// receipt appearing after preflight is never rebound or overwritten.
func publishNewRunningBINDAdoptionInstall(receipt dnsEngineInstallOwnershipReceipt) error {
	if receipt.Engine != transport.DNSEngineBIND || receipt.PackageManager != "apt" ||
		!receipt.AdoptedPresent || len(receipt.MissingBefore) != 0 {
		return errors.New("running BIND adoption requires an exact adopted-present APT receipt")
	}
	path, err := dnsEngineInstallOwnershipPath(receipt.Engine)
	if err != nil {
		return err
	}
	encoded, err := encodeDNSEngineInstallOwnership(receipt)
	if err != nil {
		return err
	}
	before, err := captureDNSFileSnapshotForOwner(path, 0o600, true,
		serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID)
	if err != nil {
		return err
	}
	if before.Exists {
		return errors.New("running BIND adoption install receipt appeared after preflight; preserve owner evidence")
	}
	if err := secureWriteConfigReplacingSnapshotWithOwner(path, encoded, 0o600, &before,
		serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID); err != nil {
		return err
	}
	actual, exists, err := servicemutationledger.ReadFile(path, 64<<10,
		servicemutationledger.FileOwner{UID: serviceMutationRequiredOwnerUID, GID: serviceMutationRequiredOwnerGID})
	if err != nil {
		return err
	}
	if !exists || !bytes.Equal(actual, encoded) {
		return errors.New("running BIND adoption install receipt readback mismatch")
	}
	return nil
}

func verifyExactRunningBINDAdoptionInstall(expected dnsEngineInstallOwnershipReceipt) error {
	path, err := dnsEngineInstallOwnershipPath(transport.DNSEngineBIND)
	if err != nil {
		return err
	}
	encoded, err := encodeDNSEngineInstallOwnership(expected)
	if err != nil {
		return err
	}
	return verifyRunningBINDAdoptionInstallWithOps(encoded, func() ([]byte, bool, error) {
		return servicemutationledger.ReadFile(path, 64<<10,
			servicemutationledger.FileOwner{UID: serviceMutationRequiredOwnerUID, GID: serviceMutationRequiredOwnerGID})
	})
}
