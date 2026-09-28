package main

import (
	"bytes"
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// The owner BIND was already installed. Rollback may retire only the exact
// temporary adopted-present receipt this request created, after native source
// restoration. It never uninstalls packages or rewrites a foreign receipt.
func retireRunningBINDAdoptionInstallWithOps(
	journal dnsEngineSwitchJournal,
	expected dnsEngineInstallOwnershipReceipt,
	read func() ([]byte, bool, error),
	remove func([]byte) error,
) error {
	if read == nil || remove == nil ||
		journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 ||
		journal.InversePlan == nil || journal.InversePlan.SourceBIND == nil ||
		journal.Phase != dnsSwitchPhaseRollingBack || journal.SourceEngine != "" ||
		journal.TargetEngine != transport.DNSEngineBIND || journal.StateBefore.Exists ||
		expected.Engine != transport.DNSEngineBIND || expected.PackageManager != "apt" ||
		!expected.AdoptedPresent || len(expected.MissingBefore) != 0 ||
		expected.MutationRequestID != journal.MutationRequestID ||
		expected.MutationOwnerID != journal.MutationOwnerID ||
		expected.ManifestQualifier != journal.ManifestQualifier {
		return errors.New("running BIND adoption install receipt lacks exact rollback scope")
	}
	encoded, err := encodeDNSEngineInstallOwnership(expected)
	if err != nil {
		return err
	}
	if err := verifyRunningBINDAdoptionInstallWithOps(encoded, read); err != nil {
		return err
	}
	_, present, err := read()
	if err != nil {
		return err
	}
	if !present {
		return nil // Crash preceded package-ownership publication.
	}
	if err := remove(encoded); err != nil {
		return err
	}
	_, present, err = read()
	if err != nil {
		return err
	}
	if present {
		return errors.New("running BIND adoption install receipt remains after exact retirement")
	}
	return nil
}

// The pending receipt must be absent or byte-for-byte this adoption's receipt
// before rollback can make any changes to native BIND.
func verifyRunningBINDAdoptionInstallWithOps(expected []byte, read func() ([]byte, bool, error)) error {
	if len(expected) == 0 || read == nil {
		return errors.New("running BIND adoption install receipt proof is unavailable")
	}
	actual, present, err := read()
	if err != nil {
		return err
	}
	if present && !bytes.Equal(actual, expected) {
		return errors.New("running BIND adoption install receipt differs from this exact request; preserve owner evidence")
	}
	return nil
}
