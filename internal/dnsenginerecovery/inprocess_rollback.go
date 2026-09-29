package dnsenginerecovery

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// InProcessRollbackDecision is the outcome of DecideInProcessRollback: what an
// Agent that has just seen its own forward step fail may do next, derived from
// the journal on disk rather than from the phase it last tried to write.
type InProcessRollbackDecision int

const (
	// InProcessRollbackDurable: the journal on disk records the rollback
	// decision (rolling-back, or rolled-back from an earlier pass). The inverse
	// may run; every one of its effects comes after this durable decision.
	InProcessRollbackDurable InProcessRollbackDecision = iota + 1
	// InProcessForward: the journal on disk records a verified target
	// (target-verified or committed), although the write that put it there may
	// have reported failure. A verified target does not enter automatic
	// rollback: no inverse effect is allowed, and the operation continues
	// forward through the same-request recovery (dnsenginerecovery.Reconcile).
	InProcessForward
	// InProcessHandOff: the rollback decision could not be made durable, or
	// the journal could not be read back as this operation's journal. No
	// inverse effect is allowed; the same-request recovery decides from the
	// journal on disk, or retains the evidence as unknown.
	InProcessHandOff
)

func (decision InProcessRollbackDecision) String() string {
	switch decision {
	case InProcessRollbackDurable:
		return "rollback-durable"
	case InProcessForward:
		return "forward"
	case InProcessHandOff:
		return "hand-off"
	}
	return "invalid"
}

// InProcessJournalOps supplies the host-locked journal access of the running
// operation. Write must be the same checkpoint writer the forward path uses.
type InProcessJournalOps struct {
	Read  func() (dnsengineartifact.SwitchJournalV1, bool, error)
	Write func(dnsengineartifact.SwitchJournalV1) error
}

// DecideInProcessRollback is the gate every in-process DNS engine rollback
// passes before its first inverse effect. expected is the operation's journal
// as the caller holds it in memory; only its phase may differ from the disk.
//
// A write that returned an error may still be durable (a failure reported
// after the rename, a directory sync error, a failed readback), so the phase
// the caller last tried to write proves nothing. The journal is read back:
//
//   - target-verified or committed: InProcessForward, nothing is written;
//   - rolling-back or rolled-back: InProcessRollbackDurable, nothing is written;
//   - intent, target-staged, source-stopped or target-started: rolling-back is
//     written. If that write fails, a second readback decides: the exact
//     rolling-back journal on disk is InProcessRollbackDurable, anything else
//     InProcessHandOff;
//   - absent, unreadable, another operation's journal, or any other phase:
//     InProcessHandOff, nothing is written.
//
// The returned journal is the durable one for InProcessRollbackDurable and
// InProcessForward, and expected otherwise. The error explains a hand-off.
func DecideInProcessRollback(expected dnsengineartifact.SwitchJournalV1, ops InProcessJournalOps) (dnsengineartifact.SwitchJournalV1, InProcessRollbackDecision, error) {
	if ops.Read == nil || ops.Write == nil {
		return expected, InProcessHandOff, errors.New("DNS switch rollback decision has no journal access; no inverse was started")
	}
	durable, err := readInProcessJournal(expected, ops.Read)
	if err != nil {
		return expected, InProcessHandOff, fmt.Errorf("DNS switch journal could not be read back before the rollback decision, so no inverse was started: %w", err)
	}
	switch durable.Phase {
	case dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted:
		return durable, InProcessForward, nil
	case dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRolledBack:
		return durable, InProcessRollbackDurable, nil
	case dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged,
		dnsengineartifact.SwitchPhaseSourceStopped, dnsengineartifact.SwitchPhaseTargetStarted:
	default:
		return expected, InProcessHandOff, fmt.Errorf("DNS switch journal phase %s has no in-process rollback decision, so no inverse was started", durable.Phase)
	}
	next := durable
	next.Phase = dnsengineartifact.SwitchPhaseRollingBack
	writeErr := ops.Write(next)
	if writeErr == nil {
		return next, InProcessRollbackDurable, nil
	}
	again, readErr := readInProcessJournal(expected, ops.Read)
	if readErr == nil && reflect.DeepEqual(again, next) {
		return next, InProcessRollbackDurable, nil
	}
	if readErr == nil {
		readErr = fmt.Errorf("the journal on disk is at phase %s", again.Phase)
	}
	return expected, InProcessHandOff, fmt.Errorf("DNS switch rollback decision could not be recorded durably, so no inverse was started: %w", errors.Join(writeErr, readErr))
}

// readInProcessJournal reads the journal and proves it is expected's operation
// with identical frozen evidence; only the phase may differ.
func readInProcessJournal(expected dnsengineartifact.SwitchJournalV1, read func() (dnsengineartifact.SwitchJournalV1, bool, error)) (dnsengineartifact.SwitchJournalV1, error) {
	durable, exists, err := read()
	if err != nil {
		return dnsengineartifact.SwitchJournalV1{}, err
	}
	if !exists {
		return dnsengineartifact.SwitchJournalV1{}, errors.New("the DNS switch journal is absent")
	}
	comparable := durable
	comparable.Phase = expected.Phase
	if !reflect.DeepEqual(comparable, expected) {
		return dnsengineartifact.SwitchJournalV1{}, errors.New("the DNS switch journal on disk is not this operation's journal")
	}
	return durable, nil
}
