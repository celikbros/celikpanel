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
	Write             func(context.Context, dnsengineartifact.SwitchJournalV1) error
	Inverse           func(context.Context, dnsengineartifact.SwitchJournalV1) error
	Remove            func(context.Context) error
}

// Reconcile preserves the original operation. Its caller proves the accepted
// ledger job and excludes a live registered worker before supplying effects.
// A verified/committed target that later disagrees cannot be rolled back by
// interpreting a failed observation as absence. Unknown results leave the
// frozen journal in place for owner review.
func Reconcile(ctx context.Context, policy dnsengineartifact.JournalPolicy, id dnsengineartifact.SwitchIdentity, ops Operations) (Outcome, error) {
	if ctx == nil || id.Validate() != nil || policy.Validate() != nil || ops.Read == nil || ops.ProveFinalized == nil || ops.VerifyTarget == nil || ops.ProveTargetAbsent == nil || ops.Write == nil || ops.Inverse == nil || ops.Remove == nil {
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
	if err = ops.VerifyTarget(ctx, journal); err == nil {
		journal.Phase = dnsengineartifact.SwitchPhaseCommitted
		if err = ops.Write(ctx, journal); err != nil {
			return OutcomeAbsent, err
		}
		return OutcomeCommitted, nil
	} else if journal.Phase == dnsengineartifact.SwitchPhaseTargetVerified || journal.Phase == dnsengineartifact.SwitchPhaseCommitted {
		return OutcomeAbsent, fmt.Errorf("verified DNS engine target no longer matches its journal: %w", err)
	} else if ctx.Err() != nil {
		return OutcomeAbsent, fmt.Errorf("DNS engine target observation interrupted: %w", errors.Join(err, ctx.Err()))
	} else if journal.Phase != dnsengineartifact.SwitchPhaseRollingBack && journal.Phase != dnsengineartifact.SwitchPhaseRolledBack {
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

// Rollback writes both checkpoints around the inverse. It never discards a
// journal if the inverse or its final checkpoint cannot be verified/published.
// Native callbacks must protect owner edits and prove the restored service.
func Rollback(ctx context.Context, journal *dnsengineartifact.SwitchJournalV1, ops Operations) error {
	if ctx == nil || journal == nil || ops.Write == nil || ops.Inverse == nil || ops.Remove == nil {
		return errors.New("invalid DNS switch recovery rollback operations")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if err := ops.Write(ctx, *journal); err != nil {
		return err
	}
	if err := ops.Inverse(ctx, *journal); err != nil {
		return err
	}
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	if err := ops.Write(ctx, *journal); err != nil {
		return err
	}
	return ops.Remove(ctx)
}
