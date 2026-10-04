//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"fmt"

	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

const mailEnrollmentSchema = "celikpanel-mail-renewal-enrollment/v1"

// This is the immutable scope accepted by the outer operation, not an inference
// of consent from a prepared kit or an existing certificate. The dispatcher must
// preserve this exact scope, the cross-invocation mutation fence and trusted code.
// No production CLI or periodic observer admits enrollment through this private API.
type mailEnrollmentScope struct {
	Schema        string `json:"schema"`
	Operation     string `json:"operation"`
	CaptureSHA256 string `json:"capture_sha256"`
	FilesSHA256   string `json:"files_sha256"`
	Target        string `json:"target"`
}
type mailEnrollmentResult struct {
	Schema      string `json:"schema"`
	ScopeSHA256 string `json:"scope_sha256"`
	Direction   string `json:"direction"`
}
type mailEnrollmentGuard struct {
	HostLock  string
	HostOwner hostmutationlock.Owner
	// Called under both inherited locks before admission and every phase/native
	// command. It must prove outer accepted intent, persistent fence and source.
	// The callback is not serialized authority and cannot be omitted on resume.
	Verify func(mailEnrollmentScope) error
}
type mailEnrollmentCommands struct {
	loaded   mailLoadedCommands
	activity mailActivityCommands
}

// mailEnrollmentHistory validates the dependency graph before choosing a phase.
// A historical receipt permits moving to a later observer, never skipping live
// resource validation. In particular enabled/active bootstrap state must not be
// sent back to the earlier disabled/inactive load verifier on reconnection.
type mailEnrollmentHistory struct {
	filesForward, filesInverse                       bool
	loadForward, loadInverse                         bool
	enablePlan, enableForward, enableInverse         bool
	parentPlan, parentReady                          bool
	activityIntent, activityForward, activityInverse bool
	enableRollback, filesRollback                    bool
	rollback, terminalForward, terminalRollback      bool
	reloadAttempts                                   bool
}

func readMailEnrollmentHistory(c *mailFilesContext, scope mailEnrollmentScope) (mailEnrollmentHistory, error) {
	h := mailEnrollmentHistory{}
	expected := func(suffix string, value any) (bool, error) {
		raw, found, err := c.read(scope.Operation + suffix)
		if err != nil {
			return false, err
		}
		if found {
			want, e := promotionJSON(value)
			if e != nil || !bytes.Equal(raw, want) {
				return false, fail(ReasonChanged)
			}
		}
		return found, nil
	}
	scopeRaw, _ := promotionJSON(scope)
	for _, item := range []struct {
		suffix, direction string
		found             *bool
	}{
		{".enrollment-rollback-intent.json", "rollback", &h.rollback},
		{".enrollment-forward.json", "forward", &h.terminalForward},
		{".enrollment-rollback.json", "rollback", &h.terminalRollback},
	} {
		found, err := expected(item.suffix, mailEnrollmentResult{mailEnrollmentSchema, Digest(scopeRaw), item.direction})
		if err != nil {
			return h, err
		}
		*item.found = found
	}
	for _, side := range []string{"forward", "rollback"} {
		value := mailFilesReceipt{mailFilesSchema, scope.FilesSHA256, side}
		done, err := expected(".files-"+side+".json", value)
		if err != nil {
			return h, err
		}
		if side == "forward" {
			h.filesForward = done
		} else {
			h.filesInverse = done
			h.filesRollback, err = expected(".files-rollback-intent.json", value)
			if err != nil {
				return h, err
			}
		}
		loaded := mailLoadedRecord{mailBootstrapLoadedSchema, scope.FilesSHA256, side, scope.Target, mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}}
		if side == "rollback" {
			loaded.Generation = ""
			loaded.Timer = c.capture.Contract.TimerBefore
		}
		intent, err := expected(".loaded-"+side+"-intent.json", loaded)
		if err != nil {
			return h, err
		}
		complete, err := expected(".loaded-"+side+".json", loaded)
		if err != nil {
			return h, err
		}
		if complete && !intent || intent && !done {
			return h, fail(ReasonChanged)
		}
		if side == "forward" {
			h.loadForward = complete
		} else {
			h.loadInverse = complete
		}
	}
	parentRaw, parentPresent, err := c.read(scope.Operation + ".timer-parent.json")
	if err != nil {
		return h, err
	}
	h.parentPlan = parentPresent
	var parent mailParentRecord
	if parentPresent {
		if err = decodePromotion(parentRaw, &parent); err != nil {
			return h, err
		}
		if err = parent.validate(scope.CaptureSHA256); err != nil {
			return h, err
		}
		if !h.loadForward {
			return h, fail(ReasonChanged)
		}
	}
	h.parentReady, err = expected(".timer-parent-ready.json", mailEnableReceipt{mailParentSchema, Digest(parentRaw), "retained"})
	if err != nil {
		return h, err
	}
	if h.parentReady && !h.parentPlan {
		return h, fail(ReasonChanged)
	}
	raw, found, err := c.read(scope.Operation + ".timer-enable.json")
	if err != nil {
		return h, err
	}
	h.enablePlan = found
	if found {
		var plan mailEnableRecord
		if err = decodePromotion(raw, &plan); err != nil {
			return h, err
		}
		if err = plan.validate(scope.FilesSHA256); err != nil {
			return h, err
		}
		if !h.loadForward || !h.parentReady || plan.Parent != parent.Directory {
			return h, fail(ReasonChanged)
		}
	}
	enableSHA := Digest(raw)
	for _, side := range []string{"forward", "rollback"} {
		value := mailEnableReceipt{mailEnableSchema, enableSHA, side}
		done, err := expected(".timer-enable-"+side+".json", value)
		if err != nil {
			return h, err
		}
		if side == "forward" {
			h.enableForward = done
		} else {
			h.enableInverse = done
			h.enableRollback, err = expected(".timer-enable-rollback-intent.json", value)
			if err != nil {
				return h, err
			}
		}
		activity := mailActivityRecord{mailActivitySchema, enableSHA, scope.Target, side}
		intent, err := expected(".timer-activity-"+side+"-intent.json", activity)
		if err != nil {
			return h, err
		}
		complete, err := expected(".timer-activity-"+side+".json", activity)
		if err != nil {
			return h, err
		}
		if complete && !intent || intent && !h.enableForward {
			return h, fail(ReasonChanged)
		}
		if side == "forward" {
			h.activityIntent, h.activityForward = intent, complete
		} else {
			h.activityInverse = complete
			if intent && (!h.activityIntent || !h.rollback) {
				return h, fail(ReasonChanged)
			}
		}
		attempts := 0
		intentRaw, _ := promotionJSON(activity)
		for n := 1; n <= mailActivityMaxAttempts; n++ {
			attempt, err := expected(fmt.Sprintf(".timer-activity-%s-attempt-%d.json", side, n), mailActivityAttempt{mailActivitySchema, Digest(intentRaw), n, "admitted"})
			if err != nil {
				return h, err
			}
			failure, err := expected(fmt.Sprintf(".timer-activity-%s-attempt-%d-failed.json", side, n), mailActivityAttempt{mailActivitySchema, Digest(intentRaw), n, "native_command_failed"})
			if err != nil {
				return h, err
			}
			if failure && !attempt || attempt && (!intent || n != attempts+1) {
				return h, fail(ReasonChanged)
			}
			if attempt {
				attempts = n
			}
		}
	}
	if (h.enableForward || h.enableRollback || h.enableInverse) && !h.enablePlan ||
		h.enableInverse && !h.enableRollback || h.enableRollback && !h.rollback ||
		h.enableRollback && h.activityIntent && !h.activityInverse ||
		h.filesInverse && !h.filesRollback || h.filesRollback && !h.rollback ||
		h.filesRollback && h.enablePlan && !h.enableInverse ||
		h.terminalForward && !h.activityForward || h.terminalRollback && (!h.rollback || !h.loadInverse) {
		return h, fail(ReasonChanged)
	}
	for _, phase := range []string{"load-forward", "load-rollback", "enable-forward", "enable-rollback"} {
		n, err := readMailEnrollmentReloads(c, scope, phase)
		if err != nil {
			return h, err
		}
		if n > 0 {
			h.reloadAttempts = true
			if phase == "load-forward" && !h.filesForward || phase == "load-rollback" && !h.filesInverse ||
				phase == "enable-forward" && !h.enablePlan || phase == "enable-rollback" && !h.enableRollback {
				return h, fail(ReasonChanged)
			}
		}
	}
	return h, nil
}

// runMailEnrollmentAt composes the fixed bootstrap protocols under two real
// inherited flocks: release fd 9, then host fd 8. Existing timers are upgraded by
// a different accepted transition; this initial enrollment never adopts one.
// Explicit rollback is monotonic even when interrupted before its first inverse.
// Unknown observation preserves the same intent; it never selects rollback.
func runMailEnrollmentAt(ctx context.Context, scope mailEnrollmentScope, direction string, paths mailCapturePaths, guard mailEnrollmentGuard, commands mailEnrollmentCommands, checkpoint func(string)) error {
	if ctx == nil || guard.Verify == nil || commands.loaded.observe == nil || commands.loaded.reload == nil || commands.activity.observe == nil || commands.activity.startTimer == nil || commands.activity.stopTimer == nil ||
		scope.Schema != mailEnrollmentSchema || !validPromotionNonce(scope.Operation) || !ValidDigest(scope.CaptureSHA256) || !ValidDigest(scope.FilesSHA256) || !ValidDigest(scope.Target) || (direction != "forward" && direction != "rollback") {
		return fail(ReasonUnsupported)
	}
	var evidence *mailFilesContext
	verify := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyEnrollmentLock(paths.transaction, 9); err != nil {
			return err
		}
		if err := hostmutationlock.VerifyInherited(guard.HostLock, 8, guard.HostOwner); err != nil {
			return err
		}
		if err := guard.Verify(scope); err != nil {
			return err
		}
		if evidence != nil {
			if err := evidence.revalidate(); err != nil {
				return err
			}
			return verifyMailEnrollmentInventory(evidence, scope.Operation)
		}
		return nil
	}
	if err := verify(); err != nil {
		return err
	}
	c, err := openMailFilesContext(scope.Operation, scope.CaptureSHA256, 9, paths)
	if err != nil {
		return err
	}
	defer c.close()
	evidence = c
	if err = verify(); err != nil {
		return err
	}
	if c.capture.Contract.Previous != "" || c.capture.Contract.Target != scope.Target {
		return fail(ReasonUnsupported)
	}
	raw, found, err := c.read(scope.Operation + ".files.json")
	if err != nil {
		return err
	}
	if !found || Digest(raw) != scope.FilesSHA256 {
		return fail(ReasonChanged)
	}
	var plan mailFilesRecord
	if err = decodePromotion(raw, &plan); err != nil {
		return err
	}
	if err = plan.validate(c); err != nil {
		return err
	}
	scopeRaw, _ := promotionJSON(scope)
	accepted, hasAcceptance, err := c.read(scope.Operation + ".enrollment.json")
	if err != nil {
		return err
	}
	if hasAcceptance && !bytes.Equal(accepted, scopeRaw) {
		return fail(ReasonChanged)
	}
	history, err := readMailEnrollmentHistory(c, scope)
	if err != nil {
		return err
	}
	if !hasAcceptance {
		// The outer reservation can survive a kill before this first receipt.
		// Explicit inverse may adopt only the unchanged prepared before-image;
		// it must not require a forward mutation just to undo an unstarted job.
		if history != (mailEnrollmentHistory{}) {
			return fail(ReasonChanged)
		}
		before, e := observeMailFiles(c, &plan)
		if e != nil {
			return e
		}
		defer before.close()
		for _, after := range before.after {
			if after {
				return fail(ReasonChanged)
			}
		}
		verifyBefore := func() error {
			if e := verify(); e != nil {
				return e
			}
			if e := before.revalidate(); e != nil {
				return e
			}
			if e := commands.loaded.observeTransition(ctx, c.capture.Contract.TimerBefore, false, true, false); e != nil {
				return e
			}
			return c.revalidate()
		}
		if err = publishMailRecord(c, scope.Operation+".enrollment.json", scopeRaw, verifyBefore, checkpoint, "enrollment_acceptance"); err != nil {
			return err
		}
	}
	if direction == "forward" && history.rollback {
		return fail(ReasonChanged)
	}
	if direction == "rollback" && !history.rollback {
		intent, _ := promotionJSON(mailEnrollmentResult{mailEnrollmentSchema, Digest(scopeRaw), "rollback"})
		if err = publishMailRecord(c, scope.Operation+".enrollment-rollback-intent.json", intent, verify, checkpoint, "enrollment_rollback_intent"); err != nil {
			return err
		}
	}
	// Each phase reopens its evidence: prior pinned observations cannot be used
	// as post-mutation evidence. Each primitive checks exact native
	// inodes again. The outer scope and locks are revalidated around every call.
	step := func(action func() error) error {
		if e := verify(); e != nil {
			return e
		}
		if e := action(); e != nil {
			return e
		}
		return verify()
	}
	reload := commands.loaded.reload
	reloadPhase := ""
	commands.loaded.reload = func(ctx context.Context) error {
		if e := verify(); e != nil {
			return e
		}
		// A previous command may have completed before the process/response was
		// lost. Current native proof can acknowledge it without another reload.
		alreadyLoaded := func() error {
			switch reloadPhase {
			case "load-forward":
				return commands.loaded.observeTransition(ctx, mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}, false, true, true)
			case "load-rollback":
				return commands.loaded.observeTransition(ctx, c.capture.Contract.TimerBefore, false, true, false)
			case "enable-forward":
				return commands.loaded.observePair(ctx, mailrenewalkit.TimerState{Enablement: "enabled", Activity: "inactive"}, false)
			case "enable-rollback":
				return commands.loaded.observePair(ctx, mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}, false)
			default:
				return fail(ReasonUnsupported)
			}
		}
		if e := alreadyLoaded(); e == nil {
			return verify()
		}
		return runMailEnrollmentReload(c, scope, reloadPhase, verify, func() error { return reload(ctx) }, checkpoint)
	}
	start, stop := commands.activity.startTimer, commands.activity.stopTimer
	commands.activity.startTimer = func(ctx context.Context) error {
		if e := verify(); e != nil {
			return e
		}
		return start(ctx)
	}
	commands.activity.stopTimer = func(ctx context.Context) error {
		if e := verify(); e != nil {
			return e
		}
		return stop(ctx)
	}
	runFiles := func(side string) error {
		return step(func() error {
			return applyMailFilesAt(scope.Operation, scope.CaptureSHA256, side, 9, paths, checkpoint)
		})
	}
	runLoad := func(side string) error {
		reloadPhase = "load-" + side
		return step(func() error {
			return reloadMailFilesAt(ctx, scope.Operation, scope.CaptureSHA256, side, 9, paths, commands.loaded, checkpoint)
		})
	}
	runEnable := func(side string) error {
		reloadPhase = "enable-" + side
		return step(func() error {
			return applyMailEnableAt(ctx, scope.Operation, scope.CaptureSHA256, side, 9, paths, commands.loaded, checkpoint)
		})
	}
	runActivity := func(side string) error {
		return step(func() error {
			return applyMailActivityAt(ctx, scope.Operation, scope.CaptureSHA256, side, 9, paths, commands.activity, checkpoint)
		})
	}
	if direction == "forward" {
		if err = runFiles("forward"); err != nil {
			return err
		}
		if !history.enablePlan {
			if err = runLoad("forward"); err != nil {
				return err
			}
		}
		if !history.enablePlan {
			err = step(func() error {
				_, e := prepareMailEnableAt(ctx, scope.Operation, scope.CaptureSHA256, 9, paths, commands.loaded, checkpoint)
				return e
			})
			if err != nil {
				return err
			}
		}
		if !history.activityIntent {
			if err = runEnable("forward"); err != nil {
				return err
			}
		}
		if err = runActivity("forward"); err != nil {
			return err
		}
	} else {
		if !history.filesRollback {
			if history.activityIntent && !history.enableRollback {
				if err = runActivity("rollback"); err != nil {
					return err
				}
			}
			if history.enablePlan {
				if err = runEnable("rollback"); err != nil {
					return err
				}
			}
		}
		if err = runFiles("rollback"); err != nil {
			return err
		}
		if err = runLoad("rollback"); err != nil {
			return err
		}
	}
	return finishMailEnrollmentAt(ctx, scope, direction, paths, verify, commands.loaded, checkpoint, false)
}

func finishMailEnrollmentAt(ctx context.Context, scope mailEnrollmentScope, direction string, paths mailCapturePaths, verify func() error, commands mailLoadedCommands, checkpoint func(string), verifyOnly bool) error {
	if verify == nil || ctx == nil || commands.observe == nil || (direction != "forward" && direction != "rollback") {
		return fail(ReasonUnsupported)
	}
	if err := verify(); err != nil {
		return err
	}
	scopeRaw, err := promotionJSON(scope)
	if err != nil {
		return err
	}
	// Terminal acknowledgement observes the selected disk and loaded state again;
	// an old success never repairs a later owner stop, link replacement or edit.
	final, err := openMailFilesContext(scope.Operation, scope.CaptureSHA256, 9, paths)
	if err != nil {
		return err
	}
	defer final.close()
	accepted, present, err := final.read(scope.Operation + ".enrollment.json")
	if err != nil {
		return err
	}
	if !present || !bytes.Equal(accepted, scopeRaw) {
		return fail(ReasonChanged)
	}
	planRaw, present, err := final.read(scope.Operation + ".files.json")
	if err != nil {
		return err
	}
	if !present || Digest(planRaw) != scope.FilesSHA256 {
		return fail(ReasonChanged)
	}
	var plan mailFilesRecord
	if err = decodePromotion(planRaw, &plan); err != nil {
		return err
	}
	if err = plan.validate(final); err != nil {
		return err
	}
	finalHistory, err := readMailEnrollmentHistory(final, scope)
	if err != nil {
		return err
	}
	if direction == "forward" && !finalHistory.activityForward || direction == "rollback" && !finalHistory.loadInverse {
		return fail(ReasonChanged)
	}
	if verifyOnly && (direction == "forward" && !finalHistory.terminalForward || direction == "rollback" && !finalHistory.terminalRollback) {
		return fail(ReasonChanged)
	}
	observation, err := observeMailFiles(final, &plan)
	if err != nil {
		return err
	}
	defer observation.close()
	for _, after := range observation.after {
		if after != (direction == "forward") {
			return fail(ReasonChanged)
		}
	}
	// Link proof is retained even after native files have been inversely moved.
	linkVerify := func() error { return nil }
	if finalHistory.enablePlan {
		enableRaw, _, e := final.read(scope.Operation + ".timer-enable.json")
		if e != nil {
			return e
		}
		var ep mailEnableRecord
		if e = decodePromotion(enableRaw, &ep); e != nil {
			return e
		}
		link, e := observeMailEnable(paths, ep)
		if e != nil {
			return e
		}
		defer link.close()
		if link.after != (direction == "forward") {
			return fail(ReasonChanged)
		}
		linkVerify = link.verify
	}
	verifyFinal := func() error {
		if e := verify(); e != nil {
			return e
		}
		if e := observation.revalidate(); e != nil {
			return e
		}
		if e := linkVerify(); e != nil {
			return e
		}
		if direction == "forward" {
			if e := commands.observePair(ctx, final.capture.Contract.TimerAfter, false); e != nil {
				return e
			}
		} else if e := commands.observeTransition(ctx, final.capture.Contract.TimerBefore, false, true, false); e != nil {
			return e
		}
		if e := final.revalidate(); e != nil {
			return e
		}
		return verify()
	}
	if verifyOnly {
		return verifyFinal()
	}
	result, _ := promotionJSON(mailEnrollmentResult{mailEnrollmentSchema, Digest(scopeRaw), direction})
	return publishMailRecord(final, scope.Operation+".enrollment-"+direction+".json", result, verifyFinal, checkpoint, "enrollment_"+direction+"_receipt")
}
