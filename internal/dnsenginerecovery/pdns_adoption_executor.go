package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// PDNSAdoptionInverseOps are effects supplied by a privileged owner recovery
// executor while it holds the release and DNS host locks. Read must return the
// exact secured journal/ledger/state observation; ExcludeWorker must inspect
// the accepted process. ProveNative checks the unchanged owner PowerDNS source.
// A caller must bind these callbacks to fixed installed paths and native proof.
type PDNSAdoptionInverseOps struct {
	Read          func(context.Context) (SwitchEvidence, bool, error)
	ExcludeWorker func(context.Context, SwitchEvidence) error
	ProveNative   func(context.Context, dnsengineartifact.SwitchJournalV1) error
	RemoveState   func(context.Context, dnsengineartifact.SwitchJournalV1) error
	WritePhase    func(context.Context, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchJournalV1) error
	PublishFailed func(context.Context, dnsengineartifact.SwitchJournalV1) error
	RemoveJournal func(context.Context, dnsengineartifact.SwitchJournalV1) error
}

// CompletePDNSAdoptionInverse executes only an already durable rollback
// decision for an exact PowerDNS adoption. It does not decide to roll back a
// target, acquire locks, or give a caller mutation authority. Unknown results
// stop at their last durable checkpoint so the same request can be retried.
func CompletePDNSAdoptionInverse(ctx context.Context, ops PDNSAdoptionInverseOps) error {
	if ctx == nil || ops.Read == nil || ops.ExcludeWorker == nil ||
		ops.ProveNative == nil || ops.RemoveState == nil || ops.WritePhase == nil ||
		ops.PublishFailed == nil || ops.RemoveJournal == nil {
		return errors.New("PowerDNS adoption inverse requires complete owner recovery operations")
	}
	read := func() (SwitchEvidence, error) {
		if err := ctx.Err(); err != nil {
			return SwitchEvidence{}, err
		}
		evidence, present, err := ops.Read(ctx)
		if err != nil || !present {
			return SwitchEvidence{}, errors.Join(errors.New("exact PowerDNS adoption evidence is unavailable"), err)
		}
		return evidence, nil
	}
	first, err := read()
	if err != nil {
		return err
	}
	if err := validatePDNSAdoptionInverseEvidence(first); err != nil {
		return err
	}
	if err := ops.ExcludeWorker(ctx, first); err != nil {
		return fmt.Errorf("exclude accepted DNS worker: %w", err)
	}
	if err := ops.ProveNative(ctx, first.Journal); err != nil {
		return fmt.Errorf("prove owner PowerDNS before inverse: %w", err)
	}
	before, err := read()
	if err != nil || !samePDNSAdoptionInverseEvidence(first, before) {
		return errors.Join(errors.New("PowerDNS adoption evidence changed before inverse"), err)
	}
	if err := ops.ExcludeWorker(ctx, before); err != nil {
		return fmt.Errorf("recheck accepted DNS worker before inverse: %w", err)
	}
	if before.Observation.TargetReceipt == TargetReceiptExact {
		if err := ops.RemoveState(ctx, before.Journal); err != nil {
			return fmt.Errorf("remove exact adoption target receipt: %w", err)
		}
	}
	restored, err := read()
	if err != nil || !samePDNSAdoptionJournal(before.Journal, restored.Journal) ||
		restored.Observation.SourceReceipt != SourceReceiptMutualAbsence ||
		restored.Observation.TargetReceipt != TargetReceiptAbsent ||
		restored.Observation.Status != before.Observation.Status ||
		!reflect.DeepEqual(restored.AcceptedJob, before.AcceptedJob) {
		return errors.Join(errors.New("PowerDNS adoption source receipt was not restored"), err)
	}
	if err := ops.ProveNative(ctx, restored.Journal); err != nil {
		return fmt.Errorf("prove owner PowerDNS after inverse: %w", err)
	}
	if err := ops.ExcludeWorker(ctx, restored); err != nil {
		return fmt.Errorf("recheck accepted DNS worker after inverse: %w", err)
	}
	if restored.Journal.Phase == dnsengineartifact.SwitchPhaseRollingBack {
		if restored.Observation.Status == EvidenceTerminalRolledBack {
			return errors.New("terminal DNS rollback ledger precedes its rolled-back journal checkpoint")
		}
		next := restored.Journal
		next.Phase = dnsengineartifact.SwitchPhaseRolledBack
		if err := ops.WritePhase(ctx, restored.Journal, next); err != nil {
			return fmt.Errorf("publish DNS rolled-back checkpoint: %w", err)
		}
	}
	checkpoint, err := read()
	if err != nil || !samePDNSAdoptionJournalIgnoringPhase(first.Journal, checkpoint.Journal) ||
		checkpoint.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		checkpoint.Observation.SourceReceipt != SourceReceiptMutualAbsence ||
		checkpoint.Observation.Status != restored.Observation.Status ||
		!reflect.DeepEqual(checkpoint.AcceptedJob, restored.AcceptedJob) {
		return errors.Join(errors.New("PowerDNS adoption rollback checkpoint was not retained"), err)
	}
	if checkpoint.Observation.Status != EvidenceTerminalRolledBack {
		if !activeDNSInverseStatus(checkpoint.Observation.Status) {
			return errors.New("PowerDNS adoption rollback lost its exact active job before terminal verdict")
		}
		if err := ops.ExcludeWorker(ctx, checkpoint); err != nil {
			return fmt.Errorf("recheck accepted DNS worker before verdict: %w", err)
		}
		if err := ops.PublishFailed(ctx, checkpoint.Journal); err != nil {
			return fmt.Errorf("publish exact DNS rollback verdict: %w", err)
		}
	}
	terminal, err := read()
	if err != nil || !samePDNSAdoptionJournalIgnoringPhase(checkpoint.Journal, terminal.Journal) ||
		terminal.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		terminal.Observation.Status != EvidenceTerminalRolledBack ||
		terminal.Observation.SourceReceipt != SourceReceiptMutualAbsence {
		return errors.Join(errors.New("PowerDNS adoption terminal verdict could not be re-proved"), err)
	}
	if err := ops.ProveNative(ctx, terminal.Journal); err != nil {
		return fmt.Errorf("reprove owner PowerDNS before journal retirement: %w", err)
	}
	final, err := read()
	if err != nil || !samePDNSAdoptionInverseEvidence(terminal, final) {
		return errors.Join(errors.New("PowerDNS adoption evidence changed before journal retirement"), err)
	}
	if err := ops.ExcludeWorker(ctx, final); err != nil {
		return fmt.Errorf("recheck accepted DNS worker before journal retirement: %w", err)
	}
	if err := ops.RemoveJournal(ctx, final.Journal); err != nil {
		return fmt.Errorf("retire exact DNS rollback journal: %w", err)
	}
	return ctx.Err()
}

func activeDNSInverseStatus(status EvidenceStatus) bool {
	switch status {
	case EvidenceActive, EvidenceLeaseExpired, EvidenceWorkerRecorded,
		EvidenceOrphanedWorker, EvidenceExpiredCancellation:
		return true
	}
	return false
}

func validatePDNSAdoptionInverseEvidence(evidence SwitchEvidence) error {
	journal, observed := evidence.Journal, evidence.Observation
	if journal.Mode != transport.DNSEngineSwitchModeAdopt ||
		journal.SourceEngine != "" || journal.TargetEngine != transport.DNSEnginePowerDNS ||
		journal.StateBefore.Exists ||
		(journal.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
			journal.Phase != dnsengineartifact.SwitchPhaseRolledBack) ||
		observed.InverseKind != NativeInversePDNSAdoption ||
		observed.RequestID != journal.MutationRequestID ||
		observed.Phase != journal.Phase ||
		observed.SourceEngine != string(journal.SourceEngine) ||
		observed.TargetEngine != string(journal.TargetEngine) ||
		observed.TargetEpoch != journal.TargetEpoch ||
		observed.SourceOwnership != SourceOwnershipNotApplicable ||
		(observed.TargetReceipt != TargetReceiptExact && observed.TargetReceipt != TargetReceiptAbsent) ||
		(observed.TargetReceipt == TargetReceiptExact && observed.SourceReceipt != SourceReceiptDifferent) ||
		(observed.TargetReceipt == TargetReceiptAbsent && observed.SourceReceipt != SourceReceiptMutualAbsence) ||
		(!activeDNSInverseStatus(observed.Status) && observed.Status != EvidenceTerminalRolledBack) ||
		(observed.Status == EvidenceTerminalRolledBack &&
			(journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
				observed.TargetReceipt != TargetReceiptAbsent ||
				observed.SourceReceipt != SourceReceiptMutualAbsence)) ||
		observed.EvidenceSHA256 == "" {
		return errors.New("PowerDNS adoption inverse lacks its exact durable rollback evidence")
	}
	return nil
}

func samePDNSAdoptionJournal(before, after dnsengineartifact.SwitchJournalV1) bool {
	return reflect.DeepEqual(before, after)
}

func samePDNSAdoptionJournalIgnoringPhase(before, after dnsengineartifact.SwitchJournalV1) bool {
	after.Phase = before.Phase
	return reflect.DeepEqual(before, after)
}

func samePDNSAdoptionInverseEvidence(before, after SwitchEvidence) bool {
	return samePDNSAdoptionJournal(before.Journal, after.Journal) &&
		reflect.DeepEqual(before.Observation, after.Observation) &&
		reflect.DeepEqual(before.AcceptedJob, after.AcceptedJob)
}
