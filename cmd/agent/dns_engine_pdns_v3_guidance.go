package main

import (
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// Recovery policy for the fresh paired PowerDNS primary (V3 journal), stated
// once here and applied by the Linux executor:
//
//   - Cut before PowerDNS ever started (intent, target-staged, or
//     target-enable-intent with the unit proven stopped): the prior state is
//     "no DNS engine" (D-026 decision 2). The Agent undoes the same request by
//     itself under the first-install stopped-target, listener and exact
//     candidate proofs, returns the unit to its rollback standby (the frozen
//     preimage: the package guard's persistent mask, or loaded and disabled),
//     publishes the terminal verdict and retires the journal. The owner
//     command recover-dns-pdns-fresh-prestart remains the Agent-independent
//     path for the same state.
//   - Cut after PowerDNS started: forward only. Its SQLite database may hold
//     the daemon's own writes, so there is no after-start inverse by policy;
//     the Agent completes the same request forward or keeps it pending.
//   - A change the install did not make (configuration, database or state
//     record) is refused at that boundary. The journal and database are kept
//     and only this DNS operation is held; unrelated host changes continue.
//
// Eşli PowerDNS birincilinin ilk kurulumu için kurtarma ilkesi: PowerDNS hiç
// başlamadıysa Agent aynı işlemi kendisi geri alır; başladıysa yalnız ileri
// tamamlar, geri alma yoktur; kurulumun yapmadığı bir değişiklik bulunursa o
// sınırda durur, günlüğü ve veritabanını korur, yalnız DNS işlemini bekletir.

type freshPrimaryV3RecoveryKind uint8

const (
	freshPrimaryV3PrestartUnproven freshPrimaryV3RecoveryKind = iota + 1
	freshPrimaryV3PoststartPending
	freshPrimaryV3OwnerChange
)

// freshPrimaryV3ChangedError marks an observation that found a change this
// install did not make. It is never an authorization to repair the change.
type freshPrimaryV3ChangedError struct {
	what  string
	cause error
}

func (e *freshPrimaryV3ChangedError) Error() string {
	return fmt.Sprintf("%s differs from what this PowerDNS install wrote: %v", e.what, e.cause)
}

func (e *freshPrimaryV3ChangedError) Unwrap() error { return e.cause }

func freshPrimaryV3Changed(what string, cause error) error {
	if cause == nil {
		return nil
	}
	var existing *freshPrimaryV3ChangedError
	if errors.As(cause, &existing) {
		return cause
	}
	return &freshPrimaryV3ChangedError{what: what, cause: cause}
}

const (
	freshPrimaryV3ChangedConfig   = "the PowerDNS configuration"
	freshPrimaryV3ChangedDatabase = "the PowerDNS database"
	freshPrimaryV3ChangedState    = "CelikPanel's DNS state record"
)

// freshPrimaryV3RecoveryError carries the fixed guidance for one unfinished
// fresh-primary recovery. Its Error text keeps the technical cause for the
// Agent log; ledgerMessage is the plain text the panel shows.
type freshPrimaryV3RecoveryError struct {
	kind      freshPrimaryV3RecoveryKind
	requestID string
	// changed names what differed for freshPrimaryV3OwnerChange.
	changed string
	// running is the systemd state of pdns.service observed after the
	// failure: "running", "not running", "failed" or "" when it could not be
	// read.
	running string
	// phase is the journal phase the recovery started from.
	phase string
	cause error
}

func (e *freshPrimaryV3RecoveryError) Error() string {
	switch e.kind {
	case freshPrimaryV3PrestartUnproven:
		return fmt.Sprintf("fresh PowerDNS primary pre-start rollback could not prove the exact pre-start state; journal retained: %v", e.cause)
	case freshPrimaryV3OwnerChange:
		return fmt.Sprintf("fresh PowerDNS primary recovery refused a change it did not make (%s); journal and database retained: %v", e.changed, e.cause)
	default:
		return fmt.Sprintf("fresh PowerDNS primary started and is recovered forward only; forward completion is pending; journal and database retained: %v", e.cause)
	}
}

func (e *freshPrimaryV3RecoveryError) Unwrap() error { return e.cause }

func (e *freshPrimaryV3RecoveryError) runningText() string {
	switch e.running {
	case "running":
		return "The PowerDNS service is running on this server now (systemd); that alone does not prove it answers correctly."
	case "not running":
		return "The PowerDNS service is not running on this server now, so this server does not answer DNS."
	case "failed":
		return "The PowerDNS service is in systemd's failed state on this server now (a start was attempted and ended with an error), so this server does not answer DNS."
	default:
		return "Whether the PowerDNS service is running could not be read."
	}
}

func (e *freshPrimaryV3RecoveryError) ledgerMessage() string {
	status := "/usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id " + e.requestID
	held := "The operation's journal is kept and blocks new DNS changes; other server changes can continue."
	switch e.kind {
	case freshPrimaryV3PrestartUnproven:
		// The owner command runs the same native proofs this Agent just
		// failed, so it is named only as what the status check offers once
		// its own admission accepts the journal, never as a sure next step.
		interrupted := "The first install of PowerDNS as the paired primary was interrupted before PowerDNS started. " +
			"CelikPanel undoes such an install by itself, but it could not prove that the server is exactly as the install left it, so it changed nothing more. "
		if freshPrimaryPrestartEnablePhaseV3(e.phase) || e.running == "failed" {
			interrupted = "The first install of PowerDNS as the paired primary was interrupted while PowerDNS was being enabled. " +
				"CelikPanel undoes such an install by itself only when it can prove PowerDNS never started and the server is exactly as the install left it; it could not, so it changed nothing more. "
		}
		return interrupted +
			e.runningText() + " This server had no DNS engine before the install, so no existing DNS service was affected. " + held +
			" Next step: the server administrator runs " + status + " and resolves the cause it reports. " +
			"Only when that check names it, /usr/libexec/celikpanel/recovery recover-dns-pdns-fresh-prestart --request-id " + e.requestID +
			" restores the state before the install; that command proves again that PowerDNS never started and refuses a started or changed target. " +
			"After the reported cause is resolved, restarting the Agent retries the same automatic undo."
	case freshPrimaryV3OwnerChange:
		return "The first install of PowerDNS as the paired primary found that " + e.changed +
			" is not as this install wrote it (an administrator edit, PowerDNS behaviour CelikPanel has not measured, or a file it could not read). " +
			"CelikPanel kept that change and neither continued nor undid the install. " + e.runningText() + " " +
			"The operation's journal and the PowerDNS database are kept and block new DNS changes; other server changes can continue. " +
			"Next step: the server administrator reviews that change and runs " + status +
			". Restarting the Agent retries the same operation only when the change is back as the install wrote it; otherwise contact support with request id " + e.requestID + "."
	default:
		return "The first install of PowerDNS as the paired primary was interrupted after PowerDNS had started. " +
			"From that point CelikPanel only completes the same install; it never removes a PowerDNS that has started, because its database may already hold PowerDNS's own changes. " +
			"The Agent could not complete it yet. " + e.runningText() + " " +
			"The operation's journal and the PowerDNS database are kept and block new DNS changes; other server changes can continue. " +
			"Next step: the server administrator checks the PowerDNS service (systemctl status pdns) and runs " + status +
			"; after the reported cause is resolved, restarting the Agent continues the same install."
	}
}

// classifyFreshPrimaryV3RecoveryError wraps an unfinished recovery with its
// guidance. prestart selects the before-start class; a change the install did
// not make overrides both.
func classifyFreshPrimaryV3RecoveryError(requestID string, prestart bool, running string, err error, phase ...string) error {
	if err == nil {
		return nil
	}
	var existing *freshPrimaryV3RecoveryError
	if errors.As(err, &existing) {
		return err
	}
	result := &freshPrimaryV3RecoveryError{
		kind: freshPrimaryV3PoststartPending, requestID: requestID, running: running, cause: err,
	}
	if len(phase) == 1 {
		result.phase = phase[0]
	}
	if prestart {
		result.kind = freshPrimaryV3PrestartUnproven
	}
	var changed *freshPrimaryV3ChangedError
	if errors.As(err, &changed) {
		result.kind, result.changed = freshPrimaryV3OwnerChange, changed.what
	}
	return result
}

// freshPrimaryPrestartJournalShapeV3 is the journal part of the pre-start
// class: the owner command's own journal predicate, without a durable native
// receipt. Whether PowerDNS started is proved natively, never from the phase.
func freshPrimaryPrestartJournalShapeV3(j dnsEngineSwitchJournal) bool {
	return j.Schema == dnsengineartifact.SwitchJournalSchemaV3 && j.PDNSFreshPlan != nil &&
		j.PDNSFreshPlan.Native == nil && len(j.TargetUnitsBefore) == 1 &&
		dnsenginerecovery.FreshPrimaryPrestartJournalV3(j)
}

func freshPrimaryPrestartEnablePhaseV3(phase string) bool {
	return phase == dnsengineartifact.SwitchPhaseTargetEnableIntent ||
		phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable
}
