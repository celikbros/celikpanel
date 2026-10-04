//go:build linux

package recoveryruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

func mailLoadedChild(root, target, scenario string, lock *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailLoadedChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_MAIL_LOADED_ROOT="+root, "CP_MAIL_LOADED_TARGET="+target, "CP_MAIL_LOADED_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	return c
}
func TestMailLoadedTransitionPreservesOwnerSchedule(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"normal", "intent-owner-change", "busy-after-completion", "late-receipt-owner-change", "busy", "masked", "owner-preference", "observe-failed", "query-missing-field", "reload-failed-retry", "after-reload-unknown-retry", "after-reload-owner-edit", "after-reload-owner-schedule", "missing-file-receipt", "bad-intent", "bad-receipt", "cancelled", "owner-change-after-completion"} {
		t.Run(scenario, func(t *testing.T) {
			root, lock := mailRenewalTestRoot(t)
			kind := "independent"
			if scenario == "bootstrap" {
				kind = "absent"
			}
			_, target, _ := mailCaptureFixture(t, root, kind)
			if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			if out, err := mailLoadedChild(root, target, scenario, lock).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		})
	}
}
func TestMailLoadedChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_LOADED_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, scenario := os.Getenv("CP_MAIL_LOADED_TARGET"), os.Getenv("CP_MAIL_LOADED_CASE")
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	timer := mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}
	if scenario == "bootstrap" {
		timer = mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}
	}
	capturePath := filepath.Join(paths.journals, mailCaptureTestOperation+".json")
	captured, err := os.ReadFile(capturePath)
	if os.IsNotExist(err) {
		captured, err = captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, timer, 9, paths, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	captureSHA := Digest(captured)
	var capture mailCaptureRecord
	if err = decodePromotion(captured, &capture); err != nil {
		t.Fatal(err)
	}
	planRaw, err := prepareMailFilesAt(mailCaptureTestOperation, captureSHA, 9, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	var plan mailFilesRecord
	if err = decodePromotion(planRaw, &plan); err != nil {
		t.Fatal(err)
	}
	rollbackOnly := scenario == "resume-rollback"
	if !rollbackOnly {
		if err = applyMailFilesAt(mailCaptureTestOperation, captureSHA, "forward", 9, paths, nil); err != nil {
			t.Fatal(err)
		}
	}
	cache := filepath.Join(root, "fake-loaded-generation")
	if _, err = os.Stat(cache); os.IsNotExist(err) {
		capturePut(t, cache, []byte(capture.Contract.Previous), 0600)
	}
	observedTimer := timer
	serviceActive := "inactive"
	readFailure, missingField := false, false
	reloads := 0
	failReload := false
	unknownAfterReload := false
	nativeGeneration := func() string {
		raw, e := os.ReadFile(paths.hook)
		if e != nil {
			t.Fatal(e)
		}
		generation, e := mailrenewalkit.HookGeneration(raw)
		if e != nil {
			t.Fatal(e)
		}
		return generation
	}
	commands := mailLoadedCommands{
		observe: func(ctx context.Context, unit string) ([]byte, error) {
			if readFailure || unknownAfterReload && reloads > 0 {
				return nil, errors.New("fixture observation unavailable")
			}
			loaded, e := os.ReadFile(cache)
			if e != nil {
				return nil, e
			}
			reload := "no"
			if string(loaded) != nativeGeneration() {
				reload = "yes"
			}
			activity, enabled := serviceActive, "static"
			if unit == mailrenewalkit.TimerName {
				activity, enabled = observedTimer.Activity, observedTimer.Enablement
			}
			raw := "LoadState=loaded\nFragmentPath=/etc/systemd/system/" + unit + "\nDropInPaths=\nNeedDaemonReload=" + reload + "\nActiveState=" + activity + "\nUnitFileState=" + enabled + "\n"
			if missingField {
				raw = strings.Replace(raw, "DropInPaths=\n", "", 1)
			}
			return []byte(raw), nil
		},
		reload: func(context.Context) error {
			reloads++
			if failReload {
				return errors.New("fixture native failure")
			}
			capturePut(t, cache, []byte(nativeGeneration()), 0600)
			switch scenario {
			case "after-reload-owner-edit":
				capturePut(t, paths.hook, []byte("owner hook\n"), 0755)
			case "after-reload-owner-schedule":
				observedTimer.Enablement = "enabled"
			}
			return nil
		},
	}
	checkpoint := func(phase string) {
		if scenario == "intent-owner-change" && phase == "loaded_forward_intent_durable" {
			observedTimer.Enablement = "enabled"
		}
		if scenario == "late-receipt-owner-change" && phase == "loaded_forward_receipt_durable" {
			observedTimer.Enablement = "enabled"
		}
		if scenario == "cut-"+phase {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	intent := filepath.Join(paths.journals, mailCaptureTestOperation+".loaded-forward-intent.json")
	receipt := filepath.Join(paths.journals, mailCaptureTestOperation+".loaded-forward.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	switch scenario {
	case "busy":
		serviceActive = "active"
	case "masked":
		observedTimer.Enablement = "masked"
	case "owner-preference":
		observedTimer.Enablement = "enabled"
	case "observe-failed":
		readFailure = true
	case "query-missing-field":
		missingField = true
	case "reload-failed-retry":
		failReload = true
	case "after-reload-unknown-retry":
		unknownAfterReload = true
	case "missing-file-receipt":
		if err = os.Remove(filepath.Join(paths.journals, mailCaptureTestOperation+".files-forward.json")); err != nil {
			t.Fatal(err)
		}
	case "bad-intent":
		capturePut(t, intent, []byte("owner intent"), 0600)
	case "bad-receipt":
		capturePut(t, receipt, []byte("owner receipt"), 0600)
	case "cancelled":
		cancel()
	}
	if !rollbackOnly {
		err = reloadMailFilesAt(ctx, mailCaptureTestOperation, captureSHA, "forward", 9, paths, commands, checkpoint)
		switch scenario {
		case "bootstrap":
			if err == nil || reloads != 0 {
				t.Fatal("bootstrap unexpectedly activated", err, reloads)
			}
			if err = applyMailFilesAt(mailCaptureTestOperation, captureSHA, "rollback", 9, paths, nil); err != nil {
				t.Fatal(err)
			}
			assertMailNativeSide(t, paths, capture, plan, false)
			return
		case "intent-owner-change":
			if err == nil || reloads != 0 || observedTimer.Enablement != "enabled" {
				t.Fatal("owner timer preference changed", err)
			}
			if _, e := os.Stat(receipt); !os.IsNotExist(e) {
				t.Fatal("unconfirmed receipt", e)
			}
			return
		case "busy", "masked", "owner-preference", "observe-failed", "query-missing-field", "missing-file-receipt", "bad-intent", "bad-receipt", "cancelled":
			if err == nil || reloads != 0 {
				t.Fatal("unverified schedule mutated", err, reloads)
			}
			if scenario == "busy" && !errors.Is(err, mailrenewalkit.ErrScheduleBusy) {
				t.Fatal("busy hidden", err)
			}
			if scenario != "bad-intent" {
				if _, e := os.Stat(intent); !os.IsNotExist(e) {
					t.Fatal("intent written for rejected observation", e)
				}
			}
			assertMailNativeSide(t, paths, capture, plan, true)
			return
		case "late-receipt-owner-change", "after-reload-owner-edit", "after-reload-owner-schedule":
			if err == nil || reloads != 1 {
				t.Fatal("owner change not detected", err, reloads)
			}
			if _, e := os.Stat(receipt); !os.IsNotExist(e) {
				t.Fatal("unconfirmed reload receipt published", e)
			}
			if scenario == "after-reload-owner-edit" {
				raw, _ := os.ReadFile(paths.hook)
				if string(raw) != "owner hook\n" {
					t.Fatal("owner hook overwritten")
				}
			} else if observedTimer.Enablement != "enabled" {
				t.Fatal("owner timer preference overwritten")
			}
			return
		case "reload-failed-retry", "after-reload-unknown-retry":
			if err == nil || reloads != 1 {
				t.Fatal("unknown reload treated as complete", err, reloads)
			}
			first, e := os.Stat(intent)
			if e != nil {
				t.Fatal("intent lost", e)
			}
			if _, e = os.Stat(receipt); !os.IsNotExist(e) {
				t.Fatal("unconfirmed terminal receipt", e)
			}
			failReload = false
			unknownAfterReload = false
			err = reloadMailFilesAt(ctx, mailCaptureTestOperation, captureSHA, "forward", 9, paths, commands, nil)
			after, e := os.Stat(intent)
			if e != nil || !os.SameFile(first, after) || reloads != 2 {
				t.Fatal("retry changed intent", e, reloads)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		firstReloads := reloads
		if scenario == "owner-change-after-completion" || scenario == "busy-after-completion" {
			if scenario == "busy-after-completion" {
				serviceActive = "active"
			} else {
				observedTimer.Enablement = "enabled"
			}
			if e := reloadMailFilesAt(ctx, mailCaptureTestOperation, captureSHA, "forward", 9, paths, commands, nil); e == nil || reloads != firstReloads {
				t.Fatal("historical completion overwrote current owner preference", e)
			}
			if _, e := os.Stat(receipt); e != nil {
				t.Fatal("known receipt removed", e)
			}
			return
		}
		if err = reloadMailFilesAt(ctx, mailCaptureTestOperation, captureSHA, "forward", 9, paths, commands, nil); err != nil || reloads != firstReloads {
			t.Fatal("completed reload repeated", err, reloads)
		}
		if observedTimer != timer {
			t.Fatal("owner schedule changed")
		}
		if err = applyMailFilesAt(mailCaptureTestOperation, captureSHA, "rollback", 9, paths, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = reloadMailFilesAt(ctx, mailCaptureTestOperation, captureSHA, "rollback", 9, paths, commands, checkpoint); err != nil {
		t.Fatal(err)
	}
	assertMailNativeSide(t, paths, capture, plan, false)
	if observedTimer != timer {
		t.Fatal("rollback changed owner schedule")
	}
	if raw, e := os.ReadFile(cache); e != nil || string(raw) != capture.Contract.Previous {
		t.Fatal("old loaded generation not restored", e)
	}
}
func TestMailLoadedResumesAfterProcessKill(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, direction := range []string{"forward", "rollback"} {
		for _, point := range []string{"intent_durable", "intent_published", "intent_parent_durable", "reloaded", "receipt_durable", "receipt_published", "receipt_parent_durable"} {
			t.Run(direction+"/"+point, func(t *testing.T) {
				root, lock := mailRenewalTestRoot(t)
				_, target, _ := mailCaptureFixture(t, root, "independent")
				if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
				child := mailLoadedChild(root, target, "cut-loaded_"+direction+"_"+point, lock)
				out, err := child.CombinedOutput()
				if err == nil {
					t.Fatal("cut missing", string(out))
				}
				status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatalf("wrong cut %v %s", err, out)
				}
				if out, err = mailLoadedChild(root, target, "resume-"+direction, lock).CombinedOutput(); err != nil {
					t.Fatalf("resume %v %s", err, out)
				}
			})
		}
	}
}
