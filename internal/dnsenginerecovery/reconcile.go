// Package dnsenginerecovery owns the bounded switch decision sequence. It does
// not acquire a host lock or implement native DNS effects; a selected executor
// must supply those effects under the accepted operation's lock and authority.
package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

type Outcome string

const (
	OutcomeAbsent     Outcome = "absent"
	OutcomeCommitted  Outcome = "committed"
	OutcomeRolledBack Outcome = "rolled-back"
	OutcomeFinalized  Outcome = "finalized"
)

type Operations struct {
	// Read must pin or lock the host evidence, decode through the shared policy,
	// and refuse unrecognized file metadata. Missing is not a verified success.
	Read              func(context.Context) (dnsengineartifact.SwitchJournalV1, bool, error)
	ProveFinalized    func(context.Context, dnsengineartifact.SwitchIdentity) (bool, error)
	VerifyTarget      func(context.Context, dnsengineartifact.SwitchJournalV1) error
	ProveTargetAbsent func(context.Context, dnsengineartifact.SwitchJournalV1) (bool, error)
	Write             func(context.Context, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchJournalV1) error
	Inverse           func(context.Context, dnsengineartifact.SwitchJournalV1) error
	// RepairVerifiedTarget is optional. It runs only after VerifyTarget failed
	// for a target-verified or committed journal, which can never roll back.
	// It may restore one exactly identified, missing native artifact of the
	// target the journal already verified, and reports whether it did. It must
	// refuse (return an error) instead of repairing whenever the observed state
	// could be an owner's or another operation's change; false with no error
	// means the failure is not one it repairs. After a repair the full target
	// verification runs again before the journal moves forward.
	RepairVerifiedTarget func(context.Context, dnsengineartifact.SwitchJournalV1) (bool, error)
}

// Reconcile preserves the original operation. Its caller proves the accepted
// ledger job and excludes a live registered worker before supplying effects.
// A verified/committed target that later disagrees cannot be rolled back by
// interpreting a failed observation as absence. Unknown results leave the
// frozen journal in place for owner review.
func Reconcile(ctx context.Context, policy dnsengineartifact.JournalPolicy, id dnsengineartifact.SwitchIdentity, ops Operations) (Outcome, error) {
	if ctx == nil || id.Validate() != nil || policy.Validate() != nil || ops.Read == nil || ops.ProveFinalized == nil || ops.VerifyTarget == nil || ops.ProveTargetAbsent == nil || ops.Write == nil || ops.Inverse == nil {
		return OutcomeAbsent, errors.New("invalid DNS switch recovery admission")
	}
	journal, exists, err := ops.Read(ctx)
	if err != nil {
		return OutcomeAbsent, err
	}
	if !exists {
		finalized, err := ops.ProveFinalized(ctx, id)
		if err != nil {
			return OutcomeAbsent, err
		}
		if finalized {
			return OutcomeFinalized, nil
		}
		return OutcomeAbsent, nil
	}
	if err = policy.ValidateSwitchJournal(journal); err != nil {
		return OutcomeAbsent, err
	}
	if journal.MutationRequestID != id.RequestID || journal.MutationOwnerID != id.OwnerID || journal.TargetEngine != id.Target || journal.ManifestQualifier != id.Qualifier {
		return OutcomeAbsent, errors.New("DNS engine switch journal belongs to another mutation")
	}
	if journal.Schema == dnsengineartifact.SwitchJournalSchemaV3 {
		// A committed V3 request may have been interrupted between durable
		// archive publication, exact active retirement and ledger success.
		// Reprove its native target; FinalizeSwitch archives/retires under the
		// same accepted request. No V3 precommit phase gains forward or inverse authority here.
		if journal.Phase != dnsengineartifact.SwitchPhaseCommitted {
			return OutcomeAbsent, errors.New("v3 fresh PowerDNS primary requires its independent native recovery; preserve the journal and target")
		}
		if err := ops.VerifyTarget(ctx, journal); err != nil {
			return OutcomeAbsent, fmt.Errorf("committed v3 native target no longer matches its journal: %w", err)
		}
		return OutcomeCommitted, nil
	}
	if journal.Schema == dnsengineartifact.SwitchJournalSchemaV4 &&
		(journal.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent || journal.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable) {
		return OutcomeAbsent, errors.New("PowerDNS enable-intent checkpoint requires the protected V4 owner recovery path")
	}
	// A durable inverse decision is monotonic. Re-observing the target cannot
	// turn an interrupted or completed rollback into a committed switch.
	if journal.Phase == dnsengineartifact.SwitchPhaseRollingBack || journal.Phase == dnsengineartifact.SwitchPhaseRolledBack {
		if err := Rollback(ctx, &journal, ops); err != nil {
			return OutcomeAbsent, err
		}
		return OutcomeRolledBack, nil
	}
	err = ops.VerifyTarget(ctx, journal)
	if err != nil && ops.RepairVerifiedTarget != nil && ctx.Err() == nil &&
		(journal.Phase == dnsengineartifact.SwitchPhaseTargetVerified || journal.Phase == dnsengineartifact.SwitchPhaseCommitted) {
		repaired, repairErr := ops.RepairVerifiedTarget(ctx, journal)
		if repairErr != nil {
			return OutcomeAbsent, fmt.Errorf("verified DNS engine target no longer matches its journal: %w; target check: %v", repairErr, err)
		}
		if repaired {
			if err = ops.VerifyTarget(ctx, journal); err != nil {
				return OutcomeAbsent, fmt.Errorf("verified DNS engine target was repaired but still does not match its journal: %w", err)
			}
		}
	}
	if err == nil {
		next := journal
		next.Phase = dnsengineartifact.SwitchPhaseCommitted
		if err = ops.Write(ctx, journal, next); err != nil {
			return OutcomeAbsent, err
		}
		return OutcomeCommitted, nil
	} else if journal.Phase == dnsengineartifact.SwitchPhaseTargetVerified || journal.Phase == dnsengineartifact.SwitchPhaseCommitted {
		return OutcomeAbsent, fmt.Errorf("verified DNS engine target no longer matches its journal: %w", err)
	} else if ctx.Err() != nil {
		return OutcomeAbsent, fmt.Errorf("DNS engine target observation interrupted: %w", errors.Join(err, ctx.Err()))
	} else {
		// A failed target observation does not prove that rollback is safe.
		// Only the exact source preimage (or verified absence when there was
		// no source) admits a new inverse. A durable inverse intent resumes
		// through its own owner-aware native checks.
		absent, absenceErr := ops.ProveTargetAbsent(ctx, journal)
		if absenceErr != nil {
			return OutcomeAbsent, fmt.Errorf("DNS engine target absence could not be proved: %w", errors.Join(err, absenceErr))
		}
		if !absent {
			return OutcomeAbsent, fmt.Errorf("DNS engine target observation is uncertain; journal retained: %w", err)
		}
	}
	if err = Rollback(ctx, &journal, ops); err != nil {
		return OutcomeAbsent, err
	}
	return OutcomeRolledBack, nil
}

// Rollback writes the missing checkpoints around the inverse and retains the
// rolled-back journal until the outer mutation ledger is durably terminal.
// Cleanup is a separate exact-operation step after that publication.
// Native callbacks must protect owner edits and prove the restored service.
func Rollback(ctx context.Context, journal *dnsengineartifact.SwitchJournalV1, ops Operations) error {
	if ctx == nil || journal == nil || ops.Write == nil || ops.Inverse == nil {
		return errors.New("invalid DNS switch recovery rollback operations")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch journal.Phase {
	case dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseRollingBackTargetEnable:
		return errors.New("PowerDNS enable-intent checkpoint requires the protected V4 owner recovery path")
	case dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted:
		return errors.New("verified DNS switch target cannot enter automatic rollback")
	case dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRolledBack:
		// Resume the accepted inverse without regressing its durable phase.
	default:
		next := *journal
		next.Phase = dnsengineartifact.SwitchPhaseRollingBack
		if err := ops.Write(ctx, *journal, next); err != nil {
			return err
		}
		*journal = next
	}
	if err := ops.Inverse(ctx, *journal); err != nil {
		return err
	}
	if journal.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		next := *journal
		next.Phase = dnsengineartifact.SwitchPhaseRolledBack
		if err := ops.Write(ctx, *journal, next); err != nil {
			return err
		}
		*journal = next
	}
	return nil
}
