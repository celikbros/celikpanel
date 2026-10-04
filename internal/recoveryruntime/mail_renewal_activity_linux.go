//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"fmt"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

const mailActivitySchema = "celikpanel-mail-renewal-activity/v1"
const mailActivityMaxAttempts = 3

type mailActivityRecord struct {
	Schema       string `json:"schema"`
	EnableSHA256 string `json:"enable_sha256"`
	Generation   string `json:"generation"`
	Direction    string `json:"direction"`
}
type mailActivityAttempt struct {
	Schema       string `json:"schema"`
	IntentSHA256 string `json:"intent_sha256"`
	Number       int    `json:"number"`
	Outcome      string `json:"outcome"`
}
type mailActivityBudgetError struct{ Operation, Direction string }

func (e *mailActivityBudgetError) Error() string {
	action := "start"
	if e.Direction == "rollback" {
		action = "stop"
	}
	return fmt.Sprintf("mail renewal timer %s reached its native command retry limit for operation %s; inspect the retained attempts and native unit error; after resolving it, the server owner can run sudo systemctl %s celikpanel-mail-renewal.timer and resume this same operation for verification", e.Direction, e.Operation, action)
}

// Fixed native capability. A caller cannot supply an arbitrary service name,
// shell command, enable/disable action or general Agent recovery dispatcher.
type mailActivityCommands struct {
	observe    func(context.Context, string) ([]byte, error)
	startTimer func(context.Context) error
	stopTimer  func(context.Context) error
}

func (c mailActivityCommands) state(ctx context.Context) (mailrenewalkit.TimerState, error) {
	units := make([]mailrenewalkit.UnitObservation, 0, 2)
	for _, unit := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		if err := ctx.Err(); err != nil {
			return mailrenewalkit.TimerState{}, err
		}
		raw, err := c.observe(ctx, unit)
		if err != nil {
			return mailrenewalkit.TimerState{}, mailrenewalkit.ErrScheduleObservation
		}
		observed, err := mailrenewalkit.ParseUnitObservation(unit, raw)
		if err != nil {
			return mailrenewalkit.TimerState{}, err
		}
		units = append(units, observed)
	}
	timer, err := mailrenewalkit.TransitionTimer(true, units[0], units[1])
	if err != nil {
		return mailrenewalkit.TimerState{}, err
	}
	if timer.Enablement != "enabled" {
		return mailrenewalkit.TimerState{}, mailrenewalkit.ErrScheduleObservation
	}
	return timer, ctx.Err()
}

// applyMailActivityAt starts/stops only the timer covered by the complete
// bootstrap file/load/enablement chain. Native workloads are not stopped. A
// busy renewal service is a wait; unknown command outcomes retain the same
// durable intent. At most three native actions per direction are admitted.
func applyMailActivityAt(ctx context.Context, operation, captureSHA, direction string, fd int, paths mailCapturePaths, commands mailActivityCommands, checkpoint func(string)) error {
	if ctx == nil || commands.observe == nil || commands.startTimer == nil || commands.stopTimer == nil || (direction != "forward" && direction != "rollback") {
		return fail(ReasonUnsupported)
	}
	e, err := openMailEnableContext(operation, captureSHA, fd, paths)
	if err != nil {
		return err
	}
	defer e.close()
	raw, ok, err := e.c.read(operation + ".timer-enable.json")
	if err != nil {
		return err
	}
	if !ok {
		return fail(ReasonChanged)
	}
	var plan mailEnableRecord
	if err = decodePromotion(raw, &plan); err != nil {
		return err
	}
	if err = plan.validate(e.filesSHA); err != nil {
		return err
	}
	enableSHA := Digest(raw)
	if _, present, err := e.c.read(operation + ".timer-enable-rollback-intent.json"); err != nil {
		return err
	} else if present {
		return fail(ReasonChanged)
	}
	enableReceipt, present, err := e.c.read(operation + ".timer-enable-forward.json")
	if err != nil {
		return err
	}
	expectedEnable, _ := promotionJSON(mailEnableReceipt{mailEnableSchema, enableSHA, "forward"})
	if !present || !bytes.Equal(enableReceipt, expectedEnable) {
		return fail(ReasonChanged)
	}
	link, err := observeMailEnable(paths, plan)
	if err != nil {
		return err
	}
	defer link.close()
	if !link.after {
		return fail(ReasonChanged)
	}
	verify := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.verify(fd); err != nil {
			return err
		}
		return link.verify()
	}
	record := mailActivityRecord{mailActivitySchema, enableSHA, e.c.capture.Contract.Target, direction}
	expected, _ := promotionJSON(record)
	intentName := operation + ".timer-activity-" + direction + "-intent.json"
	receiptName := operation + ".timer-activity-" + direction + ".json"
	// Validate both directions before considering any action. An inverse direction
	// cannot be erased by a later forward invocation or a missing terminal record.
	hasIntent, complete, forwardComplete := false, false, false
	for _, side := range []string{"forward", "rollback"} {
		want, _ := promotionJSON(mailActivityRecord{mailActivitySchema, enableSHA, record.Generation, side})
		intent, exists, err := e.c.read(operation + ".timer-activity-" + side + "-intent.json")
		if err != nil {
			return err
		}
		if exists && !bytes.Equal(intent, want) {
			return fail(ReasonChanged)
		}
		terminal, done, err := e.c.read(operation + ".timer-activity-" + side + ".json")
		if err != nil {
			return err
		}
		if done && (!exists || !bytes.Equal(terminal, want)) {
			return fail(ReasonChanged)
		}
		if side == "rollback" && exists && direction == "forward" {
			return fail(ReasonChanged)
		}
		if side == "forward" && !exists && direction == "rollback" {
			return fail(ReasonChanged)
		}
		if side == "forward" {
			forwardComplete = done
		}
		if side == direction {
			hasIntent, complete = exists, done
		}
	}
	desired := "active"
	if direction == "rollback" {
		desired = "inactive"
	}
	attempts := 0
	for n := 1; n <= mailActivityMaxAttempts; n++ {
		name := fmt.Sprintf("%s.timer-activity-%s-attempt-%d.json", operation, direction, n)
		admitted, found, err := e.c.read(name)
		if err != nil {
			return err
		}
		want, _ := promotionJSON(mailActivityAttempt{mailActivitySchema, Digest(expected), n, "admitted"})
		if found {
			if n != attempts+1 || !bytes.Equal(admitted, want) {
				return fail(ReasonChanged)
			}
			attempts = n
		}
		failed, present, err := e.c.read(fmt.Sprintf("%s.timer-activity-%s-attempt-%d-failed.json", operation, direction, n))
		if err != nil {
			return err
		}
		wantFailure, _ := promotionJSON(mailActivityAttempt{mailActivitySchema, Digest(expected), n, "native_command_failed"})
		if present && (!found || !bytes.Equal(failed, wantFailure)) {
			return fail(ReasonChanged)
		}
	}
	if !hasIntent && attempts != 0 {
		return fail(ReasonChanged)
	}
	state, err := commands.state(ctx)
	if err != nil {
		return err
	}
	if direction == "rollback" && !hasIntent && forwardComplete && state.Activity != "active" {
		return fail(ReasonChanged)
	}
	if complete {
		if state.Activity != desired {
			return fail(ReasonChanged)
		}
		if err = unix.Fsync(int(e.c.parent.file.Fd())); err != nil {
			return fail(ReasonReadFailed)
		}
		return verify()
	}
	// An already-active timer cannot be adopted by an unrecorded initial start.
	// A recorded interrupted start may already have reached the accepted state.
	if !hasIntent && direction == "forward" && state.Activity != "inactive" {
		return fail(ReasonChanged)
	}
	if err = publishMailRecord(e.c, intentName, expected, verify, checkpoint, "activity_"+direction+"_intent"); err != nil {
		return err
	}
	state, err = commands.state(ctx)
	if err != nil {
		return err
	}
	if state.Activity != desired {
		if attempts == mailActivityMaxAttempts {
			if err = verify(); err != nil {
				return err
			}
			return &mailActivityBudgetError{operation, direction}
		}
		number := attempts + 1
		admission, _ := promotionJSON(mailActivityAttempt{mailActivitySchema, Digest(expected), number, "admitted"})
		name := fmt.Sprintf("%s.timer-activity-%s-attempt-%d.json", operation, direction, number)
		if err = publishMailRecord(e.c, name, admission, verify, checkpoint, "activity_"+direction+"_attempt"); err != nil {
			return err
		}
		// Last boundary re-observation may discover the native action completed
		// independently while durable admission was being written; do not repeat it.
		state, err = commands.state(ctx)
		if err != nil {
			return err
		}
		if state.Activity != desired {
			if err = verify(); err != nil {
				return err
			}
			action := commands.startTimer
			if direction == "rollback" {
				action = commands.stopTimer
			}
			actionErr := action(ctx)
			if checkpoint != nil {
				checkpoint("activity_" + direction + "_acted")
			}
			if actionErr != nil {
				failed, _ := promotionJSON(mailActivityAttempt{mailActivitySchema, Digest(expected), number, "native_command_failed"})
				failureName := fmt.Sprintf("%s.timer-activity-%s-attempt-%d-failed.json", operation, direction, number)
				if err = publishMailRecord(e.c, failureName, failed, verify, checkpoint, "activity_"+direction+"_failure"); err != nil {
					return err
				}
				return mailrenewalkit.ErrScheduleObservation
			}
		}
	}
	verifyFinal := func() error {
		if err := verify(); err != nil {
			return err
		}
		state, err := commands.state(ctx)
		if err != nil {
			return err
		}
		if state.Activity != desired {
			return mailrenewalkit.ErrScheduleObservation
		}
		return verify()
	}
	return publishMailRecord(e.c, receiptName, expected, verifyFinal, checkpoint, "activity_"+direction+"_receipt")
}

// Enablement compensation must follow the timer activity inverse if a start was
// admitted. Native inactivity is re-observed separately by applyMailEnableAt.
func mailActivityRollbackBarrier(c *mailFilesContext, operation, enableSHA string) error {
	forward, found, err := c.read(operation + ".timer-activity-forward-intent.json")
	if err != nil {
		return err
	}
	if !found {
		for _, suffix := range []string{"forward.json", "rollback-intent.json", "rollback.json"} {
			if _, present, err := c.read(operation + ".timer-activity-" + suffix); err != nil {
				return err
			} else if present {
				return fail(ReasonChanged)
			}
		}
		return nil
	}
	expected, _ := promotionJSON(mailActivityRecord{mailActivitySchema, enableSHA, c.capture.Contract.Target, "forward"})
	if !bytes.Equal(forward, expected) {
		return fail(ReasonChanged)
	}
	expected, _ = promotionJSON(mailActivityRecord{mailActivitySchema, enableSHA, c.capture.Contract.Target, "rollback"})
	for _, suffix := range []string{"rollback-intent.json", "rollback.json"} {
		actual, present, err := c.read(operation + ".timer-activity-" + suffix)
		if err != nil {
			return err
		}
		if !present || !bytes.Equal(actual, expected) {
			return fail(ReasonChanged)
		}
	}
	return nil
}
