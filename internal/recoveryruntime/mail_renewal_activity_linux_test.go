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

func mailActivityChild(root, target, scenario string, lock *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailActivityChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_MAIL_ACTIVITY_ROOT="+root, "CP_MAIL_ACTIVITY_TARGET="+target, "CP_MAIL_ACTIVITY_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	return c
}
func TestMailActivityAdmissionRecoveryAndBudget(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"normal", "preexisting-active", "busy", "unknown", "start-budget", "stop-budget", "unknown-outcome-budget", "owner-stop-after-completion", "owner-edit-after-start", "bad-intent", "bad-receipt", "bad-attempt", "changed-attempt-after-completion", "missing-enable-receipt", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root, lock := mailRenewalTestRoot(t)
			_, target, _ := mailCaptureFixture(t, root, "absent")
			if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
				t.Fatal(e)
			}
			if out, e := mailActivityChild(root, target, scenario, lock).CombinedOutput(); e != nil {
				t.Fatalf("%v %s", e, out)
			}
		})
	}
}
func TestMailActivityChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_ACTIVITY_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, scenario := os.Getenv("CP_MAIL_ACTIVITY_TARGET"), os.Getenv("CP_MAIL_ACTIVITY_CASE")
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	capture, err := os.ReadFile(filepath.Join(paths.journals, mailCaptureTestOperation+".json"))
	if os.IsNotExist(err) {
		capture, err = captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}, 9, paths, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	sha := Digest(capture)
	if _, err = prepareMailFilesAt(mailCaptureTestOperation, sha, 9, paths, nil); err != nil {
		t.Fatal(err)
	}
	if err = applyMailFilesAt(mailCaptureTestOperation, sha, "forward", 9, paths, nil); err != nil {
		t.Fatal(err)
	}
	wants := filepath.Join(paths.units, mailTimerWants)
	if err = os.Mkdir(wants, 0700); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	link := filepath.Join(wants, mailrenewalkit.TimerName)
	native := filepath.Join(root, "activity-native")
	if _, e := os.Stat(native); os.IsNotExist(e) {
		capturePut(t, native, []byte("inactive"), 0600)
	}
	starts, stops := 0, 0
	busy, unknown, failStart, failStop, unknownOutcome := false, false, false, false, false
	observe := func(_ context.Context, unit string) ([]byte, error) {
		if unknown {
			return nil, errors.New("native unknown")
		}
		value, e := os.ReadFile(native)
		if e != nil {
			return nil, e
		}
		activity, enable := "inactive", "static"
		if unit == mailrenewalkit.TimerName {
			activity = string(value)
			enable = "disabled"
			if _, e = os.Lstat(link); e == nil {
				enable = "enabled"
			}
		}
		if unit == mailrenewalkit.ServiceName && busy {
			activity = "activating"
		}
		return []byte("LoadState=loaded\nFragmentPath=/etc/systemd/system/" + unit + "\nDropInPaths=\nNeedDaemonReload=no\nActiveState=" + activity + "\nUnitFileState=" + enable + "\n"), nil
	}
	loaded := mailLoadedCommands{observe: observe, reload: func(context.Context) error { return nil }}
	if _, e := os.Stat(filepath.Join(paths.journals, mailCaptureTestOperation+".loaded-forward.json")); os.IsNotExist(e) {
		if err = reloadMailFilesAt(context.Background(), mailCaptureTestOperation, sha, "forward", 9, paths, loaded, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = prepareMailEnableAt(context.Background(), mailCaptureTestOperation, sha, 9, paths, loaded, nil); err != nil {
		t.Fatal(err)
	}
	enableReceipt := filepath.Join(paths.journals, mailCaptureTestOperation+".timer-enable-forward.json")
	if _, e := os.Stat(enableReceipt); os.IsNotExist(e) {
		if err = applyMailEnableAt(context.Background(), mailCaptureTestOperation, sha, "forward", 9, paths, loaded, nil); err != nil {
			t.Fatal(err)
		}
	}
	commands := mailActivityCommands{
		observe: observe,
		startTimer: func(context.Context) error {
			starts++
			if failStart {
				return errors.New("native command failure redacted")
			}
			if !unknownOutcome {
				capturePut(t, native, []byte("active"), 0600)
			}
			if scenario == "owner-edit-after-start" {
				if e := os.Rename(link, filepath.Join(root, "owner-link-before")); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink("/owner.timer", link); e != nil {
					t.Fatal(e)
				}
			}
			return nil
		},
		stopTimer: func(context.Context) error {
			stops++
			if failStop {
				return errors.New("native stop failure redacted")
			}
			capturePut(t, native, []byte("inactive"), 0600)
			return nil
		},
	}
	checkpoint := func(point string) {
		if scenario == "cut-"+point {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	intent := filepath.Join(paths.journals, mailCaptureTestOperation+".timer-activity-forward-intent.json")
	receipt := filepath.Join(paths.journals, mailCaptureTestOperation+".timer-activity-forward.json")
	switch scenario {
	case "preexisting-active":
		capturePut(t, native, []byte("active"), 0600)
	case "busy":
		busy = true
	case "unknown":
		unknown = true
	case "start-budget":
		failStart = true
	case "unknown-outcome-budget":
		unknownOutcome = true
	case "bad-intent":
		capturePut(t, intent, []byte("unverified intent"), 0600)
	case "bad-receipt":
		capturePut(t, receipt, []byte("unverified receipt"), 0600)
	case "bad-attempt":
		capturePut(t, filepath.Join(paths.journals, mailCaptureTestOperation+".timer-activity-forward-attempt-1.json"), []byte("unverified attempt"), 0600)
	case "missing-enable-receipt":
		if e := os.Remove(enableReceipt); e != nil {
			t.Fatal(e)
		}
	case "cancelled":
		cancel()
	}
	rollbackOnly := scenario == "resume-rollback"
	if !rollbackOnly {
		err = applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, checkpoint)
		switch scenario {
		case "preexisting-active", "busy", "unknown", "bad-intent", "bad-receipt", "bad-attempt", "missing-enable-receipt", "cancelled":
			if err == nil || starts != 0 || stops != 0 {
				t.Fatal("unknown/native owner state changed", err, starts, stops)
			}
			return
		case "owner-edit-after-start":
			if err == nil || starts != 1 {
				t.Fatal("owner replacement hidden", err, starts)
			}
			if raw, e := os.Readlink(link); e != nil || raw != "/owner.timer" {
				t.Fatal("owner path replaced", e)
			}
			if _, e := os.Stat(receipt); !os.IsNotExist(e) {
				t.Fatal("false completion", e)
			}
			return
		case "start-budget", "unknown-outcome-budget":
			if err == nil || starts != 1 {
				t.Fatal("failed native action completed", err)
			}
			for n := 2; n <= 3; n++ {
				if e := applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil); e == nil || starts != n {
					t.Fatal("wrong bounded retry", n, e, starts)
				}
			}
			first, e := os.Stat(intent)
			if e != nil {
				t.Fatal(e)
			}
			err = applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil)
			var budget *mailActivityBudgetError
			if !errors.As(err, &budget) || budget.Operation != mailCaptureTestOperation || starts != 3 || !strings.Contains(budget.Error(), "sudo systemctl start celikpanel-mail-renewal.timer") {
				t.Fatal("retry budget not actionable", err, starts)
			}
			if scenario == "start-budget" {
				for n := 1; n <= 3; n++ {
					if _, e := os.Stat(filepath.Join(paths.journals, mailCaptureTestOperation+".timer-activity-forward-attempt-"+string(rune('0'+n))+"-failed.json")); e != nil {
						t.Fatal("known command failure lost", e)
					}
				}
			}
			// Owner performs the native action after fixing its cause. Reconciliation
			// proves it without resetting attempts or issuing another start.
			failStart, unknownOutcome = false, false
			capturePut(t, native, []byte("active"), 0600)
			err = applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil)
			after, e := os.Stat(intent)
			if e != nil || !os.SameFile(first, after) || starts != 3 {
				t.Fatal("owner recovery reset intent", e, starts)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if e := applyMailEnableAt(ctx, mailCaptureTestOperation, sha, "rollback", 9, paths, loaded, nil); e == nil {
			t.Fatal("enablement inverse bypassed activity inverse")
		}
		if scenario == "changed-attempt-after-completion" {
			capturePut(t, filepath.Join(paths.journals, mailCaptureTestOperation+".timer-activity-forward-attempt-1.json"), []byte("owner edited history"), 0600)
			if e := applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil); e == nil || starts != 1 {
				t.Fatal("current activity hid conflicting durable attempts", e)
			}
			return
		}
		count := starts
		if scenario == "owner-stop-after-completion" {
			capturePut(t, native, []byte("inactive"), 0600)
		}
		err = applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil)
		if scenario == "owner-stop-after-completion" {
			if err == nil || starts != count {
				t.Fatal("owner stop overwritten", err, starts)
			}
			if e := applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "rollback", 9, paths, commands, nil); e == nil {
				t.Fatal("unrecorded owner stop adopted as inverse")
			}
			return
		}
		if err != nil || starts != count {
			t.Fatal("completed start repeated", err, starts)
		}
	}
	if scenario == "stop-budget" {
		failStop = true
	}
	err = applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "rollback", 9, paths, commands, checkpoint)
	if scenario == "stop-budget" {
		if err == nil || stops != 1 {
			t.Fatal("failed native stop completed", err)
		}
		for n := 2; n <= 3; n++ {
			if e := applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "rollback", 9, paths, commands, nil); e == nil || stops != n {
				t.Fatal("wrong stop retry", e, stops)
			}
		}
		var budget *mailActivityBudgetError
		if e := applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "rollback", 9, paths, commands, nil); !errors.As(e, &budget) || stops != 3 || !strings.Contains(budget.Error(), "sudo systemctl stop celikpanel-mail-renewal.timer") {
			t.Fatal("stop budget lost", e)
		}
		failStop = false
		capturePut(t, native, []byte("inactive"), 0600)
		err = applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "rollback", 9, paths, commands, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	if e := applyMailActivityAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil); e == nil {
		t.Fatal("forward started after inverse")
	}
	if e := applyMailEnableAt(ctx, mailCaptureTestOperation, sha, "rollback", 9, paths, loaded, nil); e != nil {
		t.Fatal("ordered enablement inverse failed", e)
	}
	if e := applyMailFilesAt(mailCaptureTestOperation, sha, "rollback", 9, paths, nil); e != nil {
		t.Fatal("ordered file inverse failed", e)
	}
}
func TestMailActivityProcessKill(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, direction := range []string{"forward", "rollback"} {
		for _, point := range []string{"intent_durable", "intent_published", "intent_parent_durable", "attempt_durable", "attempt_published", "attempt_parent_durable", "acted", "receipt_durable", "receipt_published", "receipt_parent_durable"} {
			t.Run(direction+"/"+point, func(t *testing.T) {
				root, lock := mailRenewalTestRoot(t)
				_, target, _ := mailCaptureFixture(t, root, "legacy")
				if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
					t.Fatal(e)
				}
				child := mailActivityChild(root, target, "cut-activity_"+direction+"_"+point, lock)
				out, e := child.CombinedOutput()
				if e == nil {
					t.Fatal("cut missing", string(out))
				}
				status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatalf("wrong cut %v %s", e, out)
				}
				if out, e = mailActivityChild(root, target, "resume-"+direction, lock).CombinedOutput(); e != nil {
					t.Fatalf("resume %v %s", e, out)
				}
			})
		}
	}
}
