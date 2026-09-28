//go:build linux

package dnsenginerecovery

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// FreshPrimaryStagedProofV3 admits only the exact sealed candidate from a
// fresh paired-primary journal. It cannot classify a target-started cut:
// without a durable native observation that state remains unknown.
func FreshPrimaryStagedProofV3(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) (dnsengineartifact.PDNSTargetCandidateProofV4, error) {
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return dnsengineartifact.PDNSTargetCandidateProofV4{}, err
	}
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		journal.PDNSFreshPlan == nil || journal.PDNSFreshPlan.Candidate == nil ||
		journal.PDNSFreshPlan.Native != nil ||
		(journal.Phase != dnsengineartifact.SwitchPhaseTargetStaged &&
			journal.Phase != dnsengineartifact.SwitchPhaseTargetEnableIntent &&
			journal.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
			journal.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable) {
		return dnsengineartifact.PDNSTargetCandidateProofV4{}, errors.New("v3 fresh primary is not an exact prestart target")
	}
	return *journal.PDNSFreshPlan.Candidate, nil
}

// RestoreFreshPrimaryRenamedV3 returns only the exact unchanged candidate
// inode at the live path to its private staged name. The guard must reprove
// accepted-worker exclusion and a stopped, empty PowerDNS cgroup under locks.
func RestoreFreshPrimaryRenamedV3(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, guard func() error) error {
	proof, err := FreshPrimaryStagedProofV3(policy, journal)
	if err != nil {
		return err
	}
	return restoreRenamedPDNSTargetFileV4(proof, policy.PDNSDatabasePath, guard, nil)
}

// RemoveFreshPrimaryStagedV3 removes only the exact frozen candidate after
// the live database and every sidecar are proven absent. No foreign file is
// removed, and a missing candidate is an idempotent replay only with absence.
func RemoveFreshPrimaryStagedV3(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, guard func() error) error {
	proof, err := FreshPrimaryStagedProofV3(policy, journal)
	if err != nil {
		return err
	}
	return removeExactStagedPDNSTargetFileV4(proof, policy.PDNSDatabasePath, guard, nil)
}
