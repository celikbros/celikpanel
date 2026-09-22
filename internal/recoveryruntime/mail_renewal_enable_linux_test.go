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

func mailEnableChild(root, target, scenario string, lock *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailEnableChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_MAIL_ENABLE_ROOT="+root, "CP_MAIL_ENABLE_TARGET="+target, "CP_MAIL_ENABLE_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	return c
}
func TestMailEnablePreservesExactAuthority(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"normal", "owner-link-before", "owner-link-after-plan", "same-target-replacement", "owner-parent", "owner-active", "native-unknown", "reload-failure", "owner-after-reload", "owner-after-completion", "owner-disable-after-completion", "bad-plan", "bad-forward-receipt", "inverse-without-intent", "owner-link-after-inverse", "missing-load-receipt", "stage-inventory", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root, lock := mailRenewalTestRoot(t)
			_, target, _ := mailCaptureFixture(t, root, "legacy")
			if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			if out, err := mailEnableChild(root, target, scenario, lock).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		})
	}
}
func TestMailEnableChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_ENABLE_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, scenario := os.Getenv("CP_MAIL_ENABLE_TARGET"), os.Getenv("CP_MAIL_ENABLE_CASE")
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
	cache := filepath.Join(root, "enable-cache")
	if _, err = os.Stat(cache); os.IsNotExist(err) {
		capturePut(t, cache, []byte("disabled"), 0600)
	}
	active, unknown, failReload := false, false, false
	reloads := 0
	enableTestPhase := false
	commands := mailLoadedCommands{
		observe: func(_ context.Context, unit string) ([]byte, error) {
			if unknown {
				return nil, errors.New("native unknown")
			}
			raw, e := os.ReadFile(cache)
			if e != nil {
				return nil, e
			}
			enable, activity, pending := "static", "inactive", "no"
			if unit == mailrenewalkit.TimerName {
				enable = string(raw)
				if active {
					activity = "active"
				}
			}
			_, e = os.Lstat(link)
			if (e == nil) != (string(raw) == "enabled") {
				pending = "yes"
			}
			return []byte("LoadState=loaded\nFragmentPath=/etc/systemd/system/" + unit + "\nDropInPaths=\nNeedDaemonReload=" + pending + "\nActiveState=" + activity + "\nUnitFileState=" + enable + "\n"), nil
		},
		reload: func(context.Context) error {
			reloads++
			if failReload {
				return errors.New("native reload unknown")
			}
			state := "disabled"
			if _, e := os.Lstat(link); e == nil {
				state = "enabled"
			} else if !os.IsNotExist(e) {
				return e
			}
			capturePut(t, cache, []byte(state), 0600)
			if scenario == "owner-after-reload" && enableTestPhase {
				active = true
			}
			return nil
		},
	}
	loadedReceipt := filepath.Join(paths.journals, mailCaptureTestOperation+".loaded-forward.json")
	if _, err = os.Stat(loadedReceipt); os.IsNotExist(err) {
		if err = reloadMailFilesAt(context.Background(), mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil); err != nil {
			t.Fatal(err)
		}
	}
	reloads = 0
	enableTestPhase = true
	if scenario == "owner-link-before" {
		if err = os.Symlink("/owner.timer", link); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "missing-load-receipt" {
		if err = os.Remove(loadedReceipt); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := func(point string) {
		if scenario == "cut-"+point {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if scenario == "cancelled" {
		cancel()
	}
	planRaw, err := prepareMailEnableAt(ctx, mailCaptureTestOperation, sha, 9, paths, commands, checkpoint)
	if scenario == "owner-link-before" || scenario == "missing-load-receipt" || scenario == "cancelled" {
		if err == nil || reloads != 0 {
			t.Fatal("unverified plan accepted", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var plan mailEnableRecord
	if err = decodePromotion(planRaw, &plan); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(wants, plan.stageName(), mailrenewalkit.TimerName)
	planPath := filepath.Join(paths.journals, mailCaptureTestOperation+".timer-enable.json")
	switch scenario {
	case "owner-link-after-plan":
		if err = os.Symlink("/owner.timer", link); err != nil {
			t.Fatal(err)
		}
	case "same-target-replacement":
		if err = os.Rename(stage, filepath.Join(root, "owner-before-link")); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(mailEnableLinkTarget, stage); err != nil {
			t.Fatal(err)
		}
	case "owner-parent":
		if err = os.Chmod(wants, 0750); err != nil {
			t.Fatal(err)
		}
	case "owner-active":
		active = true
	case "native-unknown":
		unknown = true
	case "reload-failure":
		failReload = true
	case "bad-plan":
		capturePut(t, planPath, []byte("unknown plan"), 0600)
	case "bad-forward-receipt":
		capturePut(t, filepath.Join(paths.journals, mailCaptureTestOperation+".timer-enable-forward.json"), []byte("unknown receipt"), 0600)
	case "inverse-without-intent":
		raw, _ := promotionJSON(mailEnableReceipt{mailEnableSchema, Digest(planRaw), "rollback"})
		capturePut(t, filepath.Join(paths.journals, mailCaptureTestOperation+".timer-enable-rollback.json"), raw, 0600)
	case "stage-inventory":
		capturePut(t, filepath.Join(wants, plan.stageName(), "owner.txt"), []byte("owner"), 0600)
	}
	rollbackOnly := scenario == "resume-rollback"
	if !rollbackOnly {
		err = applyMailEnableAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, checkpoint)
		switch scenario {
		case "owner-link-after-plan", "same-target-replacement", "owner-parent", "owner-active", "native-unknown", "bad-plan", "bad-forward-receipt", "inverse-without-intent", "stage-inventory":
			if err == nil || reloads != 0 {
				t.Fatal("owner state modified", err, reloads)
			}
			if scenario == "owner-link-after-plan" {
				raw, e := os.Readlink(link)
				if e != nil || raw != "/owner.timer" {
					t.Fatal("owner link replaced", e)
				}
			}
			return
		case "owner-after-reload":
			if err == nil || reloads != 1 || !active {
				t.Fatal("owner change lost", err, reloads)
			}
			if _, e := os.Stat(filepath.Join(paths.journals, mailCaptureTestOperation+".timer-enable-forward.json")); !os.IsNotExist(e) {
				t.Fatal("unverified receipt", e)
			}
			return
		case "reload-failure":
			if err == nil || reloads != 1 {
				t.Fatal("native failure hidden", err, reloads)
			}
			first, e := os.Stat(planPath)
			if e != nil {
				t.Fatal(e)
			}
			failReload = false
			err = applyMailEnableAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil)
			after, e := os.Stat(planPath)
			if e != nil || !os.SameFile(first, after) || reloads != 2 {
				t.Fatal("plan reset", e, reloads)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = applyMailFilesAt(mailCaptureTestOperation, sha, "rollback", 9, paths, nil); err == nil {
			t.Fatal("file rollback bypassed outstanding enablement")
		}
		if _, e := os.Stat(filepath.Join(paths.journals, mailCaptureTestOperation+".files-rollback-intent.json")); !os.IsNotExist(e) {
			t.Fatal("file rollback started before enablement inverse", e)
		}
		count := reloads
		if scenario == "owner-after-completion" {
			active = true
		}
		if scenario == "owner-disable-after-completion" {
			if err = os.Rename(link, stage); err != nil {
				t.Fatal(err)
			}
			capturePut(t, cache, []byte("disabled"), 0600)
		}
		err = applyMailEnableAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil)
		if scenario == "owner-after-completion" || scenario == "owner-disable-after-completion" {
			if err == nil || reloads != count {
				t.Fatal("completed proof hid current owner state", err)
			}
			return
		}
		if err != nil || reloads != count {
			t.Fatal("completed enablement repeated", err, reloads)
		}
	}
	if err = applyMailEnableAt(ctx, mailCaptureTestOperation, sha, "rollback", 9, paths, commands, checkpoint); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Lstat(link); !os.IsNotExist(e) {
		t.Fatal("native enablement not removed", e)
	}
	if raw, e := os.ReadFile(cache); e != nil || string(raw) != "disabled" {
		t.Fatal("inverse not observed", e)
	}
	if err = applyMailEnableAt(ctx, mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil); err == nil {
		t.Fatal("forward restarted after inverse")
	}
	// The exact symlink inode remains staged for diagnosis, never deleted.
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0}}
	defer state.close()
	dir, e := state.openPath(filepath.Dir(stage))
	if e != nil {
		t.Fatal(e)
	}
	id, found, e := observeMailEnableLink(dir)
	if e != nil || !found || !matchPromotionIdentity(id, plan.Link, true) {
		t.Fatal("recorded link identity lost", e)
	}
	if scenario == "owner-link-after-inverse" {
		if e = os.Symlink("/owner.timer", link); e != nil {
			t.Fatal(e)
		}
		if e = applyMailFilesAt(mailCaptureTestOperation, sha, "rollback", 9, paths, nil); e == nil {
			t.Fatal("historical inverse hid new owner link")
		}
		if raw, e := os.Readlink(link); e != nil || raw != "/owner.timer" {
			t.Fatal("owner link lost", e)
		}
		return
	}
	if err = applyMailFilesAt(mailCaptureTestOperation, sha, "rollback", 9, paths, nil); err != nil {
		t.Fatal("file inverse refused completed enablement inverse", err)
	}
	var original mailCaptureRecord
	if err = decodePromotion(capture, &original); err != nil {
		t.Fatal(err)
	}
	for _, name := range mailNativeNames() {
		path := filepath.Join(paths.units, name)
		if name == mailrenewalkit.HookName {
			path = paths.hook
		}
		raw, e := os.ReadFile(path)
		old, present := original.Contract.Before[name]
		if present {
			if e != nil || string(raw) != string(old) {
				t.Fatal("original file not restored", name, e)
			}
		} else if !os.IsNotExist(e) {
			t.Fatal("original absence not restored", name, e)
		}
	}
}
func TestMailEnableProcessKill(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	points := []string{"link_staged", "plan_durable", "plan_published", "plan_parent_durable"}
	for _, direction := range []string{"forward", "rollback"} {
		for _, point := range []string{"moved", "parent_durable", "reloaded", "receipt_durable", "receipt_published", "receipt_parent_durable"} {
			points = append(points, direction+"_"+point)
		}
	}
	for _, point := range []string{"durable", "published", "parent_durable"} {
		points = append(points, "rollback_intent_"+point)
	}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			root, lock := mailRenewalTestRoot(t)
			_, target, _ := mailCaptureFixture(t, root, "absent")
			if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
				t.Fatal(e)
			}
			child := mailEnableChild(root, target, "cut-enable_"+point, lock)
			out, e := child.CombinedOutput()
			if e == nil {
				t.Fatal("cut absent", string(out))
			}
			status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("wrong cut %v %s", e, out)
			}
			resume := "resume-forward"
			if strings.HasPrefix(point, "rollback_") {
				resume = "resume-rollback"
			}
			if out, e = mailEnableChild(root, target, resume, lock).CombinedOutput(); e != nil {
				t.Fatalf("resume %v %s", e, out)
			}
		})
	}
}
