//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// BINDSwitchNativeState is a read-only classification of the exact frozen
// native preimage. A host adapter must distinguish owner edits and unknown
// intermediate results from a replayable partial inverse; neither is Restored.
type BINDSwitchNativeState uint8

const (
	BINDSwitchNativeUnknown BINDSwitchNativeState = iota
	BINDSwitchNativeNeedsRestore
	BINDSwitchNativeRestored
)

// BINDSwitchInverseOps are supplied by a fixed-path owner recovery adapter
// holding the release and host locks. AssessNative must prove the exact BIND
// generation pointer, config, unit identity and source PowerDNS preimage. Its
// NeedsRestore result is allowed only when the adapter can safely replay every
// incomplete native effect. RestoreNative must use the existing ordered BIND
// activation rollback primitive with owner-aware proofs at each effect.
// These callbacks do not give arbitrary callers host mutation authority.
type BINDSwitchInverseOps struct {
	Read          func(context.Context) (SwitchEvidence, bool, error)
	ExcludeWorker func(context.Context, SwitchEvidence) error
	AssessNative  func(context.Context, dnsengineartifact.SwitchJournalV1) (BINDSwitchNativeState, error)
	RestoreNative func(context.Context, dnsengineartifact.SwitchJournalV1) error
	WritePhase    func(context.Context, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchJournalV1) error
	PublishFailed func(context.Context, dnsengineartifact.SwitchJournalV1) error
	RemoveJournal func(context.Context, dnsengineartifact.SwitchJournalV1) error
}

// CompleteInactiveBINDSwitchInverse continues only an already-durable rollback
// decision. Native effects must be repeatable against the frozen preimage;
// rolling-back is retained until native restoration has been re-proved. The
// terminal ledger is published before the exact journal is retired.
func CompleteInactiveBINDSwitchInverse(ctx context.Context, ops BINDSwitchInverseOps) error {
	return completeBINDSwitchInverse(ctx, ops, ValidateInactiveBINDSwitchInverseEvidence, SourceReceiptExact, SourceOwnershipExact)
}

// CompleteRunningBINDAdoptionInverse uses the same durable verdict ordering but
// admits only the independently frozen, already-running owner BIND shape.
// Native callbacks must restore by reload without stopping or restarting named.
func CompleteRunningBINDAdoptionInverse(ctx context.Context, ops BINDSwitchInverseOps) error {
	return completeBINDSwitchInverse(ctx, ops, ValidateRunningBINDAdoptionInverseEvidence, SourceReceiptMutualAbsence, SourceOwnershipNotApplicable)
}

func completeBINDSwitchInverse(ctx context.Context, ops BINDSwitchInverseOps, validate func(SwitchEvidence) error, source SourceReceiptStatus, ownership SourceOwnershipStatus) error {
	if ctx == nil || ops.Read == nil || ops.ExcludeWorker == nil || ops.AssessNative == nil ||
		ops.RestoreNative == nil || ops.WritePhase == nil || ops.PublishFailed == nil || ops.RemoveJournal == nil {
		return errors.New("inactive BIND switch inverse requires complete owner recovery operations")
	}
	read := func() (SwitchEvidence, error) {
		if err := ctx.Err(); err != nil {
			return SwitchEvidence{}, err
		}
		evidence, present, err := ops.Read(ctx)
		if err != nil || !present {
			return SwitchEvidence{}, errors.Join(errors.New("exact BIND switch evidence is unavailable"), err)
		}
		if err := validate(evidence); err != nil {
			return SwitchEvidence{}, err
		}
		return evidence, nil
	}
	first, err := read()
	if err != nil {
		return err
	}
	if err := ops.ExcludeWorker(ctx, first); err != nil {
		return fmt.Errorf("exclude accepted DNS worker: %w", err)
	}
	state, err := ops.AssessNative(ctx, first.Journal)
	if err != nil || state == BINDSwitchNativeUnknown {
		return errors.Join(errors.New("BIND switch native preimage is unknown or owner-modified"), err)
	}
	before, err := read()
	if err != nil || !sameBINDSwitchInverseEvidence(first, before) {
		return errors.Join(errors.New("BIND switch evidence changed before native inverse"), err)
	}
	if err := ops.ExcludeWorker(ctx, before); err != nil {
		return fmt.Errorf("recheck accepted DNS worker before native inverse: %w", err)
	}
	if before.Observation.Status == EvidenceTerminalRolledBack && state != BINDSwitchNativeRestored {
		return errors.New("terminal BIND rollback ledger lacks restored native proof")
	}
	if state == BINDSwitchNativeNeedsRestore {
		if before.Journal.Phase != dnsengineartifact.SwitchPhaseRollingBack {
			return errors.New("rolled-back BIND checkpoint still needs native restoration")
		}
		if err := ops.RestoreNative(ctx, before.Journal); err != nil {
			return fmt.Errorf("restore exact BIND switch native preimage: %w", err)
		}
	}
	restored, err := read()
	if err != nil || !sameBINDSwitchInverseJournal(first.Journal, restored.Journal) ||
		restored.Observation.SourceReceipt != source ||
		restored.Observation.SourceOwnership != ownership ||
		restored.Observation.Status != before.Observation.Status ||
		!reflect.DeepEqual(restored.AcceptedJob, before.AcceptedJob) {
		return errors.Join(errors.New("BIND switch source receipt was not restored"), err)
	}
	if err := ops.ExcludeWorker(ctx, restored); err != nil {
		return fmt.Errorf("recheck accepted DNS worker after native inverse: %w", err)
	}
	if state, err = ops.AssessNative(ctx, restored.Journal); err != nil || state != BINDSwitchNativeRestored {
		return errors.Join(errors.New("BIND switch native restoration could not be proved"), err)
	}
	if restored.Journal.Phase == dnsengineartifact.SwitchPhaseRollingBack {
		if restored.Observation.Status == EvidenceTerminalRolledBack {
			return errors.New("terminal BIND rollback ledger precedes its rolled-back journal checkpoint")
		}
		next := restored.Journal
		next.Phase = dnsengineartifact.SwitchPhaseRolledBack
		if err := ops.WritePhase(ctx, restored.Journal, next); err != nil {
			return fmt.Errorf("publish BIND rolled-back checkpoint: %w", err)
		}
	}
	checkpoint, err := read()
	if err != nil || !sameBINDSwitchJournalIgnoringPhase(first.Journal, checkpoint.Journal) ||
		checkpoint.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		checkpoint.Observation.SourceReceipt != source ||
		checkpoint.Observation.Status != restored.Observation.Status ||
		!reflect.DeepEqual(checkpoint.AcceptedJob, restored.AcceptedJob) {
		return errors.Join(errors.New("BIND rollback checkpoint was not retained"), err)
	}
	if checkpoint.Observation.Status != EvidenceTerminalRolledBack {
		if !activeDNSInverseStatus(checkpoint.Observation.Status) {
			return errors.New("BIND rollback lost its exact active job before terminal verdict")
		}
		if err := ops.ExcludeWorker(ctx, checkpoint); err != nil {
			return fmt.Errorf("recheck accepted DNS worker before verdict: %w", err)
		}
		if err := ops.PublishFailed(ctx, checkpoint.Journal); err != nil {
			return fmt.Errorf("publish exact BIND rollback verdict: %w", err)
		}
	}
	terminal, err := read()
	if err != nil || !sameBINDSwitchJournalIgnoringPhase(checkpoint.Journal, terminal.Journal) ||
		terminal.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		terminal.Observation.Status != EvidenceTerminalRolledBack ||
		terminal.Observation.SourceReceipt != source {
		return errors.Join(errors.New("BIND rollback terminal verdict could not be re-proved"), err)
	}
	if state, err = ops.AssessNative(ctx, terminal.Journal); err != nil || state != BINDSwitchNativeRestored {
		return errors.Join(errors.New("BIND native source changed before journal retirement"), err)
	}
	final, err := read()
	if err != nil || !sameBINDSwitchInverseEvidence(terminal, final) {
		return errors.Join(errors.New("BIND rollback evidence changed before journal retirement"), err)
	}
	if err := ops.ExcludeWorker(ctx, final); err != nil {
		return fmt.Errorf("recheck accepted DNS worker before journal retirement: %w", err)
	}
	if err := ops.RemoveJournal(ctx, final.Journal); err != nil {
		return fmt.Errorf("retire exact BIND rollback journal: %w", err)
	}
	return nil
}

func sameBINDSwitchInverseJournal(before, after dnsengineartifact.SwitchJournalV1) bool {
	return reflect.DeepEqual(before, after)
}

func sameBINDSwitchJournalIgnoringPhase(before, after dnsengineartifact.SwitchJournalV1) bool {
	after.Phase = before.Phase
	return reflect.DeepEqual(before, after)
}

func sameBINDSwitchInverseEvidence(before, after SwitchEvidence) bool {
	return sameBINDSwitchInverseJournal(before.Journal, after.Journal) &&
		reflect.DeepEqual(before.Observation, after.Observation) &&
		reflect.DeepEqual(before.AcceptedJob, after.AcceptedJob)
}
