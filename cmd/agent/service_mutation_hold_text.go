package main

import "errors"

// The Agent holds every durable change on the host ("fail-closed") for more
// than one reason, and until 2026-09-30 every hold said the same thing: "after
// an ambiguous ledger write". Batch 5's DNS hold involved no ledger write at
// all. A hold now says which of its causes applies, that every change on this
// server is held, and the next step. The stable codes are unchanged:
// errors.Is(err, errServiceMutationManagerPoisoned) for every hold, and the
// status code transport.MutationHoldLedgerAmbiguous the Panel renders.
//
// Agent sunucudaki her kalıcı değişikliği birden fazla nedenle bekletir ve
// hepsi aynı sözü söylüyordu: "belirsiz bir defter yazımından sonra". Artık her
// bekletme kendi nedenini, sunucudaki tüm değişikliklerin beklediğini ve
// sonraki adımı söyler. Kararlı kodlar değişmez.

type serviceMutationHoldCause uint8

const (
	serviceMutationHoldLedgerWrite serviceMutationHoldCause = iota + 1
	serviceMutationHoldDNSSwitch
	serviceMutationHoldUnverified
)

// dnsSwitchFailClosedDecision marks a deliberate hold taken because a DNS
// engine switch's outcome could not be proved (critical admission, an abort
// that could not be proved with no journal to keep, a receipt mismatch, or a
// failed finalization). No ledger write is implied.
type dnsSwitchFailClosedDecision struct{ err error }

func (decision *dnsSwitchFailClosedDecision) Error() string { return decision.err.Error() }
func (decision *dnsSwitchFailClosedDecision) Unwrap() error { return decision.err }

func classifyServiceMutationHold(cause error) serviceMutationHoldCause {
	var dnsDecision *dnsSwitchFailClosedDecision
	switch {
	case serviceMutationWriteMayHavePublished(cause):
		return serviceMutationHoldLedgerWrite
	case errors.As(cause, &dnsDecision):
		return serviceMutationHoldDNSSwitch
	default:
		return serviceMutationHoldUnverified
	}
}

const (
	serviceMutationHoldLedgerWriteText = "service mutation manager is fail-closed: a write of the service mutation ledger may have been published and could not be verified. " +
		"Every change on this server is held; running services are not stopped. " +
		"Next step: the server owner restarts the CelikPanel Agent (systemctl restart celikpanel-agent), which reconciles the ledger at start; if the hold returns, keep the ledger and contact support"
	serviceMutationHoldDNSSwitchText = "service mutation manager is fail-closed: a DNS engine switch could not prove its outcome, so the Agent stopped accepting changes by decision; no ledger write was ambiguous. " +
		"Every change on this server is held; running services are not stopped. " +
		"Next step: the server owner runs /usr/libexec/celikpanel/recovery dns-switch-status --quiesced and follows it, then restarts the CelikPanel Agent (systemctl restart celikpanel-agent), which re-examines the same switch at start"
	serviceMutationHoldUnverifiedText = "service mutation manager is fail-closed: an operation's durable record or host outcome could not be verified, so the Agent stopped accepting changes by decision; this does not by itself mean a ledger write was ambiguous. " +
		"Every change on this server is held; running services are not stopped. " +
		"Next step: the server owner restarts the CelikPanel Agent (systemctl restart celikpanel-agent), whose startup recovery re-examines the same operation; the Agent log names the operation and cause"
)

// serviceMutationHoldError is the fail-closed identity with the words for
// one cause. It matches errServiceMutationManagerPoisoned.
type serviceMutationHoldError struct{ cause serviceMutationHoldCause }

func newServiceMutationHoldError(cause error) error {
	return &serviceMutationHoldError{cause: classifyServiceMutationHold(cause)}
}

func (hold *serviceMutationHoldError) Error() string {
	switch hold.cause {
	case serviceMutationHoldLedgerWrite:
		return serviceMutationHoldLedgerWriteText
	case serviceMutationHoldDNSSwitch:
		return serviceMutationHoldDNSSwitchText
	default:
		return serviceMutationHoldUnverifiedText
	}
}

func (hold *serviceMutationHoldError) Is(target error) bool {
	return target == errServiceMutationManagerPoisoned
}
