//go:build linux

package dnsenginerecovery

import (
	"errors"
	"fmt"
	"os"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// VerifyStagedPDNSTargetV4 checks the private-candidate shape admitted by the
// bounded pre-activation inverse: the exact stopped candidate still exists,
// while the initially absent live database and all sidecars remain absent.
// The caller also proves PowerDNS is inactive and excludes its worker under
// the host locks. A successful check does not authorize deleting the file.
func VerifyStagedPDNSTargetV4(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) error {
	proof, err := stagedPDNSTargetProofV4(policy, journal)
	if err != nil {
		return err
	}
	if err := verifyPDNSTargetAbsentV4(policy.PDNSDatabasePath, false); err != nil {
		return err
	}
	actual, err := CapturePDNSTargetCandidateV4(proof.Path)
	if err != nil {
		return fmt.Errorf("capture staged PowerDNS candidate: %w", err)
	}
	if actual != proof {
		return errors.New("staged PowerDNS candidate differs from frozen inode, metadata or bytes")
	}
	return verifyPDNSTargetAbsentV4(policy.PDNSDatabasePath, false)
}

// VerifyRenamedPDNSTargetV4 accepts only the exact candidate at the live
// database name under a pre-start journal checkpoint. Native process and unit
// exclusion remains the caller's responsibility.
func VerifyRenamedPDNSTargetV4(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) error {
	proof, err := stagedPDNSTargetProofV4(policy, journal)
	if err != nil {
		return err
	}
	return VerifyPDNSTargetLiveV4(proof, policy.PDNSDatabasePath)
}

// VerifyRestoredPDNSTargetV4 is the target-file portion of a rollback proof.
// Both the candidate and the live database must be absent, with no sidecars.
// Source BIND serving and config/unit restoration require separate native proof.
func VerifyRestoredPDNSTargetV4(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) error {
	proof, err := stagedPDNSTargetProofV4(policy, journal)
	if err != nil {
		return err
	}
	if err := verifyPDNSTargetAbsentV4(proof.Path, true); err != nil {
		return err
	}
	return verifyPDNSTargetAbsentV4(policy.PDNSDatabasePath, false)
}

func stagedPDNSTargetProofV4(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) (dnsengineartifact.PDNSTargetCandidateProofV4, error) {
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return dnsengineartifact.PDNSTargetCandidateProofV4{}, err
	}
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV4 || journal.PDNSTargetPlan == nil || journal.PDNSTargetPlan.Candidate == nil ||
		(journal.Phase != dnsengineartifact.SwitchPhaseRollingBack && journal.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable && journal.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return dnsengineartifact.PDNSTargetCandidateProofV4{}, errors.New("PowerDNS target does not have a bounded staged rollback proof")
	}
	return *journal.PDNSTargetPlan.Candidate, nil
}

func verifyPDNSTargetAbsentV4(path string, privateParent bool) error {
	if !validPDNSTargetPath(path) {
		return errors.New("PowerDNS database path is invalid")
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("PowerDNS database path unexpectedly exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect PowerDNS database path: %w", err)
	}
	if _, err := observePDNSTargetDirectory(path, privateParent); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("PowerDNS database path appeared during absence proof: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("recheck PowerDNS database path: %w", err)
	}
	return nil
}
