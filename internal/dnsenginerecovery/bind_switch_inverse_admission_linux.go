//go:build linux

package dnsenginerecovery

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// ValidateInactiveBINDSwitchInverseEvidence is the first admission boundary for
// the managed PowerDNS-to-BIND inverse. It consumes a secured SwitchEvidence
// read, not caller-supplied journal bytes. Its success does not authorize a
// native effect: the caller must also hold both installed locks, exclude the
// accepted worker and prove the current generation/config and native units.
// A previously running owner BIND has a different inverse and is rejected.
// The frozen journal shape is InactiveBINDSwitchInverseJournal; the checks
// below bind it to the secured ledger/state observation.
func ValidateInactiveBINDSwitchInverseEvidence(evidence SwitchEvidence) error {
	j, o := evidence.Journal, evidence.Observation
	if err := InactiveBINDSwitchInverseJournal(j); err != nil {
		return err
	}
	if o.InverseKind != NativeInverseBINDSwitch ||
		o.EvidenceSHA256 == "" ||
		o.RequestID != j.MutationRequestID ||
		o.Phase != j.Phase ||
		o.SourceEngine != string(j.SourceEngine) ||
		o.TargetEngine != string(j.TargetEngine) ||
		o.TargetGeneration != j.TargetGeneration ||
		o.TargetEpoch != j.TargetEpoch ||
		o.SourceOwnership != SourceOwnershipExact ||
		(o.TargetReceipt != TargetReceiptExact && o.SourceReceipt != SourceReceiptExact) ||
		(o.TargetReceipt == TargetReceiptExact && o.SourceReceipt != SourceReceiptDifferent) ||
		(o.SourceReceipt == SourceReceiptExact && o.TargetReceipt == TargetReceiptExact) ||
		(!activeDNSInverseStatus(o.Status) && o.Status != EvidenceTerminalRolledBack) ||
		(o.Status == EvidenceTerminalRolledBack &&
			(j.Phase != dnsengineartifact.SwitchPhaseRolledBack || o.SourceReceipt != SourceReceiptExact)) {
		return errors.New("inactive BIND switch inverse lacks exact retained rollback evidence")
	}
	return nil
}
