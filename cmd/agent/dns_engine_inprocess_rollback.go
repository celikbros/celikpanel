package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// An in-process DNS engine switch that fails decides what to do from the
// journal on disk, never from the phase it last tried to write (a write that
// reported failure may be durable). A verified target goes forward; the inverse
// runs only after a durable rolling-back decision; anything else hands the
// operation, with no inverse effect, to the same-request recovery that the
// Agent's RPC runs next and a restarted Agent would run (Reconcile).
//
// Başarısız olan bir DNS motoru geçişi ne yapacağına, en son yazmayı denediği
// aşamaya göre değil diskteki günlüğe göre karar verir (hata bildiren bir yazma
// kalıcı olabilir). Doğrulanmış hedef ileri gider; ters işlem yalnız kalıcı bir
// rolling-back kararından sonra çalışır; diğer her durumda işlem, ters etki
// olmadan aynı isteğin kurtarmasına devredilir.

// dnsSwitchInProcessHandoffError reports that the in-process failure path ran
// no inverse effect and left the operation to same-request recovery.
type dnsSwitchInProcessHandoffError struct {
	// forward is true when the journal on disk records a verified target.
	forward bool
	phase   string
	cause   error
}

func (handoff *dnsSwitchInProcessHandoffError) Error() string {
	if handoff.forward {
		return fmt.Sprintf("the DNS switch journal is durably at %s, so this verified target does not roll back; no inverse was started and same-request recovery continues forward: %v", handoff.phase, handoff.cause)
	}
	return fmt.Sprintf("no inverse was started; same-request recovery decides this DNS switch from the journal on disk: %v", handoff.cause)
}

func (handoff *dnsSwitchInProcessHandoffError) Unwrap() error { return handoff.cause }

type dnsSwitchInProcessJournalOps struct {
	read  func() (dnsEngineSwitchJournal, bool, error)
	write func(dnsEngineSwitchJournal) error
}

// decideDNSSwitchInProcessRollback applies dnsenginerecovery's gate. On a
// durable rollback decision it returns the durable journal and a nil error;
// otherwise it returns a *dnsSwitchInProcessHandoffError joined with cause, and
// the caller must not run any inverse effect.
func decideDNSSwitchInProcessRollback(
	journal dnsEngineSwitchJournal,
	ops dnsSwitchInProcessJournalOps,
	cause error,
) (dnsEngineSwitchJournal, error) {
	decided, decision, err := dnsenginerecovery.DecideInProcessRollback(
		journal, dnsenginerecovery.InProcessJournalOps{Read: ops.read, Write: ops.write},
	)
	switch decision {
	case dnsenginerecovery.InProcessRollbackDurable:
		return decided, nil
	case dnsenginerecovery.InProcessForward:
		return journal, &dnsSwitchInProcessHandoffError{
			forward: true, phase: decided.Phase, cause: cause,
		}
	default:
		return journal, &dnsSwitchInProcessHandoffError{
			cause: errors.Join(cause, err),
		}
	}
}

// decideBINDApplyFailure is the gate between a failed BIND forward step and
// binddns.Publisher.Switch, whose failed-apply path restores the generation
// pointer and calls the inverse. It returns the error to hand to the publisher
// (nil keeps the publisher from any pointer or inverse effect) and the
// hand-off error the driver returns after Switch. Only a durable rollback
// decision lets the failure reach the publisher.
func decideBINDApplyFailure(
	applyErr error,
	journal *dnsEngineSwitchJournal,
	ops dnsSwitchInProcessJournalOps,
) (toPublisher, handoff error) {
	if applyErr == nil {
		return nil, nil
	}
	if journal == nil {
		return nil, &dnsSwitchInProcessHandoffError{
			cause: errors.Join(applyErr, errors.New("BIND switch journal is unavailable")),
		}
	}
	decided, err := decideDNSSwitchInProcessRollback(*journal, ops, applyErr)
	if err != nil {
		return nil, err
	}
	*journal = decided
	return applyErr, nil
}

// runBINDSwitchWithRollbackGate is the BIND switch and running-BIND adoption
// composition of binddns.Publisher.Switch (switchFn) with the rollback gate.
// forward is the operation's forward step; rollbackAndJournal its gated
// inverse (runBINDRollbackWithJournal). The publisher calls apply once, then
// again after a pointer restore, or recoverEmpty, only for a failure that
// reached it, which decideBINDApplyFailure allows only after a durable
// rollback decision. A hand-off is returned after Switch: with the publisher
// having made no pointer or inverse change, the pointer still selects the
// target, and the same-request recovery decides.
func runBINDSwitchWithRollbackGate(
	ctx context.Context,
	switchFn func(ctx context.Context, apply, recoverEmpty func(context.Context) error) error,
	journal *dnsEngineSwitchJournal,
	ops dnsSwitchInProcessJournalOps,
	forward func(context.Context) error,
	rollbackAndJournal func(context.Context) error,
) error {
	if switchFn == nil || journal == nil || forward == nil || rollbackAndJournal == nil {
		return errors.New("BIND switch rollback gate operations are incomplete")
	}
	var handoff error
	attempt := 0
	apply := func(applyCtx context.Context) error {
		attempt++
		if attempt > 1 {
			return rollbackAndJournal(applyCtx)
		}
		toPublisher, gateHandoff := decideBINDApplyFailure(forward(applyCtx), journal, ops)
		handoff = gateHandoff
		return toPublisher
	}
	recoverEmpty := func(recoveryCtx context.Context) error {
		return rollbackAndJournal(recoveryCtx)
	}
	if err := switchFn(ctx, apply, recoverEmpty); err != nil {
		return errors.Join(err, handoff)
	}
	return handoff
}

// gatedDNSSwitchRollbackOps are one PowerDNS driver's in-process rollback:
// decide is its rollback gate, inverse restores and proves the source from the
// decided journal, write records rolled-back.
type gatedDNSSwitchRollbackOps struct {
	decide  func(dnsEngineSwitchJournal, error) (dnsEngineSwitchJournal, error)
	inverse func(dnsEngineSwitchJournal) error
	write   func(dnsEngineSwitchJournal) error
}

// runGatedDNSSwitchRollback runs the inverse only after decide returned a
// durable rollback decision; a decide error (a hand-off) is returned with no
// inverse effect. After a proved inverse the journal is moved to rolled-back
// unless it already was.
func runGatedDNSSwitchRollback(
	journal *dnsEngineSwitchJournal,
	cause error,
	ops gatedDNSSwitchRollbackOps,
) error {
	if journal == nil || ops.decide == nil || ops.inverse == nil || ops.write == nil {
		return errors.Join(cause, errors.New("DNS switch rollback operations are incomplete; no inverse was started"))
	}
	decided, handoff := ops.decide(*journal, cause)
	if handoff != nil {
		return handoff
	}
	*journal = decided
	inverseErr := ops.inverse(*journal)
	var journalErr error
	if inverseErr == nil && journal.Phase != dnsSwitchPhaseRolledBack {
		journalErr = finishDNSSwitchRollbackJournal(journal, ops.write)
	}
	return errors.Join(cause, journalErr, inverseErr)
}
