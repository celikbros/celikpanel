//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

const mailLoadedSchema = "celikpanel-mail-renewal-loaded/v1"
const mailBootstrapLoadedSchema = "celikpanel-mail-renewal-bootstrap-loaded/v1"

type mailLoadedRecord struct {
	Schema     string                    `json:"schema"`
	PlanSHA256 string                    `json:"plan_sha256"`
	Direction  string                    `json:"direction"`
	Generation string                    `json:"generation"`
	Timer      mailrenewalkit.TimerState `json:"timer"`
}

// Private command capability: observation of the two fixed units and native
// daemon-reload only. No start/stop/enable/disable or general command parameter.
// A future production dispatcher must supply its trusted bounded native runner.
type mailLoadedCommands struct {
	observe func(context.Context, string) ([]byte, error)
	reload  func(context.Context) error
}

func (commands mailLoadedCommands) observePair(ctx context.Context, timer mailrenewalkit.TimerState, allowReload bool) error {
	return commands.observeTransition(ctx, timer, allowReload, false, false)
}

func (commands mailLoadedCommands) observeTransition(ctx context.Context, timer mailrenewalkit.TimerState, allowReload, bootstrap, published bool) error {
	units := make([]mailrenewalkit.UnitObservation, 0, 2)
	for _, name := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := commands.observe(ctx, name)
		if err != nil {
			return mailrenewalkit.ErrScheduleObservation
		}
		unit, err := mailrenewalkit.ParseUnitObservation(name, raw)
		if err != nil {
			return err
		}
		// Pending reload is expected only after a verified, completed file phase.
		// This normalization is of an in-memory observation, never native state.
		if allowReload && unit.NeedDaemonReload == "yes" {
			unit.NeedDaemonReload = "no"
		}
		units = append(units, unit)
	}
	if bootstrap {
		if err := mailrenewalkit.VerifyBootstrapLoaded(units[0], units[1], published, allowReload); err != nil {
			return err
		}
		return ctx.Err()
	}
	observed, err := mailrenewalkit.TransitionTimer(true, units[0], units[1])
	if err != nil {
		return err
	}
	if observed != timer {
		return mailrenewalkit.ErrScheduleObservation
	}
	return ctx.Err()
}

// reloadMailFilesAt handles existing schedules and initial idle unit loading. It binds actual
// native daemon-reload to a completed, verified file transition. It never changes
// owner enablement/activity preferences, starts a workload or enables a timer.
// Bootstrap forward proves loaded but disabled/inactive; inverse proves absence.
// The caller separately holds host/renewal exclusion and accepted owner authority;
// this private primitive additionally requires the inherited release lock.
func reloadMailFilesAt(ctx context.Context, operation, captureSHA, direction string, fd int, paths mailCapturePaths, commands mailLoadedCommands, checkpoint func(string)) error {
	if ctx == nil || commands.observe == nil || commands.reload == nil || (direction != "forward" && direction != "rollback") {
		return fail(ReasonUnsupported)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := openMailFilesContext(operation, captureSHA, fd, paths)
	if err != nil {
		return err
	}
	defer c.close()
	bootstrap := c.capture.Contract.Previous == ""
	planRaw, ok, err := c.read(operation + ".files.json")
	if err != nil {
		return err
	}
	if !ok {
		return fail(ReasonChanged)
	}
	var plan mailFilesRecord
	if err = decodePromotion(planRaw, &plan); err != nil {
		return err
	}
	if err = plan.validate(c); err != nil {
		return err
	}
	// Require the exact file-phase terminal record before touching loaded state.
	// Historical forward success cannot authorize action after rollback intent.
	expectedFiles, _ := promotionJSON(mailFilesReceipt{mailFilesSchema, Digest(planRaw), direction})
	filesReceipt, ok, err := c.read(operation + ".files-" + direction + ".json")
	if err != nil {
		return err
	}
	if !ok || !bytes.Equal(filesReceipt, expectedFiles) {
		return fail(ReasonChanged)
	}
	rollbackIntent, rollback, err := c.read(operation + ".files-rollback-intent.json")
	if err != nil {
		return err
	}
	if direction == "forward" && rollback {
		return fail(ReasonChanged)
	}
	if direction == "rollback" && (!rollback || !bytes.Equal(rollbackIntent, expectedFiles)) {
		return fail(ReasonChanged)
	}
	files, err := observeMailFiles(c, &plan)
	if err != nil {
		return err
	}
	defer files.close()
	for name := range plan.New {
		if files.after[name] != (direction == "forward") {
			return fail(ReasonChanged)
		}
	}
	generation := c.capture.Contract.Target
	if direction == "rollback" {
		generation = c.capture.Contract.Previous
	}
	record := mailLoadedRecord{mailLoadedSchema, Digest(planRaw), direction, generation, c.capture.Contract.TimerBefore}
	if bootstrap {
		record.Schema = mailBootstrapLoadedSchema
		if direction == "forward" {
			record.Timer = mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}
		}
	}
	observe := func(allowReload bool) error {
		return commands.observeTransition(ctx, record.Timer, allowReload, bootstrap, direction == "forward")
	}
	raw, err := promotionJSON(record)
	if err != nil {
		return err
	}
	intentName := operation + ".loaded-" + direction + "-intent.json"
	receiptName := operation + ".loaded-" + direction + ".json"
	intent, hasIntent, err := c.read(intentName)
	if err != nil {
		return err
	}
	if hasIntent && !bytes.Equal(intent, raw) {
		return fail(ReasonChanged)
	}
	receipt, complete, err := c.read(receiptName)
	if err != nil {
		return err
	}
	if complete && (!hasIntent || !bytes.Equal(receipt, raw)) {
		return fail(ReasonChanged)
	}
	verify := func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := verifyEnrollmentLock(paths.transaction, fd); e != nil {
			return e
		}
		if e := files.revalidate(); e != nil {
			return e
		}
		return c.revalidate()
	}
	if complete {
		if err = observe(false); err != nil {
			return err
		}
		if err = unix.Fsync(int(c.parent.file.Fd())); err != nil {
			return fail(ReasonReadFailed)
		}
		return verify()
	}
	if err = observe(true); err != nil {
		return err
	}
	if err = publishMailRecord(c, intentName, raw, verify, checkpoint, "loaded_"+direction+"_intent"); err != nil {
		return err
	}
	// Re-observe after intent durability; a newly running oneshot is a wait, and
	// a changed/unknown timer is preserved rather than repaired by this operation.
	if err = observe(true); err != nil {
		return err
	}
	if err = verify(); err != nil {
		return err
	}
	if err = commands.reload(ctx); err != nil {
		var budget *mailEnrollmentReloadBudget
		if errors.As(err, &budget) {
			return budget
		}
		return mailrenewalkit.ErrScheduleObservation
	}
	if checkpoint != nil {
		checkpoint("loaded_" + direction + "_reloaded")
	}
	if err = verify(); err != nil {
		return err
	}
	if err = observe(false); err != nil {
		return err
	}
	verifyLoaded := func() error {
		if e := verify(); e != nil {
			return e
		}
		if e := observe(false); e != nil {
			return e
		}
		return verify()
	}
	return publishMailRecord(c, receiptName, raw, verifyLoaded, checkpoint, "loaded_"+direction+"_receipt")
}
