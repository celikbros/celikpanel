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

func mailBootstrapChild(root, target, scenario string, lock *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailBootstrapLoadedChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_MAIL_BOOTSTRAP_ROOT="+root, "CP_MAIL_BOOTSTRAP_TARGET="+target, "CP_MAIL_BOOTSTRAP_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	return c
}
func TestMailBootstrapLoadedTransition(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, kind := range []string{"absent", "legacy"} {
		for _, scenario := range []string{"normal", "partial-cache", "enabled-before", "active-before", "override-before", "missing-property", "unknown-before", "reload-failed", "unknown-after", "owner-after", "owner-after-completed", "rollback-enabled", "missing-file-receipt", "bad-intent"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				root, lock := mailRenewalTestRoot(t)
				_, target, _ := mailCaptureFixture(t, root, kind)
				if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
				if out, err := mailBootstrapChild(root, target, scenario, lock).CombinedOutput(); err != nil {
					t.Fatalf("%v %s", err, out)
				}
			})
		}
	}
}
func TestMailBootstrapLoadedChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_BOOTSTRAP_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, scenario := os.Getenv("CP_MAIL_BOOTSTRAP_TARGET"), os.Getenv("CP_MAIL_BOOTSTRAP_CASE")
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	captured, err := os.ReadFile(filepath.Join(paths.journals, mailCaptureTestOperation+".json"))
	if os.IsNotExist(err) {
		captured, err = captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}, 9, paths, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	var capture mailCaptureRecord
	if err = decodePromotion(captured, &capture); err != nil {
		t.Fatal(err)
	}
	sha := Digest(captured)
	planRaw, err := prepareMailFilesAt(mailCaptureTestOperation, sha, 9, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	var plan mailFilesRecord
	if err = decodePromotion(planRaw, &plan); err != nil {
		t.Fatal(err)
	}
	rollbackOnly := scenario == "resume-rollback"
	if !rollbackOnly {
		if err = applyMailFilesAt(mailCaptureTestOperation, sha, "forward", 9, paths, nil); err != nil {
			t.Fatal(err)
		}
	}
	cache := filepath.Join(root, "bootstrap-loaded-cache")
	if _, e := os.Stat(cache); os.IsNotExist(e) {
		capturePut(t, cache, []byte("absent"), 0600)
	}
	reloads := 0
	unknown, failReload, afterUnknown, enabled, active, override, missing := false, false, false, false, false, false, false
	commands := mailLoadedCommands{
		observe: func(_ context.Context, unit string) ([]byte, error) {
			if unknown || afterUnknown && reloads > 0 {
				return nil, errors.New("native observation unavailable")
			}
			saved, e := os.ReadFile(cache)
			if e != nil {
				return nil, e
			}
			loaded := string(saved) == "loaded" || string(saved) == "partial" && unit == mailrenewalkit.ServiceName
			state, fragment, enable := "not-found", "", ""
			if loaded {
				state, fragment, enable = "loaded", "/etc/systemd/system/"+unit, "static"
				if unit == mailrenewalkit.TimerName {
					enable = "disabled"
				}
			}
			activity := "inactive"
			dropins := ""
			if enabled && unit == mailrenewalkit.TimerName {
				enable = "enabled"
			}
			if active && unit == mailrenewalkit.TimerName {
				activity = "active"
			}
			if override {
				dropins = "/etc/systemd/system/owner.conf"
			}
			pending := "no"
			_, e = os.Stat(filepath.Join(paths.units, unit))
			if (e == nil) != loaded {
				pending = "yes"
			}
			raw := "LoadState=" + state + "\nFragmentPath=" + fragment + "\nDropInPaths=" + dropins + "\nNeedDaemonReload=" + pending + "\nActiveState=" + activity + "\nUnitFileState=" + enable + "\n"
			if missing {
				raw = strings.Replace(raw, "DropInPaths=\n", "", 1)
			}
			return []byte(raw), nil
		},
		reload: func(context.Context) error {
			reloads++
			if failReload {
				return errors.New("reload unverified")
			}
			_, e := os.Stat(filepath.Join(paths.units, mailrenewalkit.ServiceName))
			value := "absent"
			if e == nil {
				value = "loaded"
			} else if !os.IsNotExist(e) {
				return e
			}
			capturePut(t, cache, []byte(value), 0600)
			if scenario == "owner-after" {
				enabled = true
			}
			return nil
		},
	}
	checkpoint := func(point string) {
		if scenario == "cut-"+point {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	intent := filepath.Join(paths.journals, mailCaptureTestOperation+".loaded-forward-intent.json")
	receipt := filepath.Join(paths.journals, mailCaptureTestOperation+".loaded-forward.json")
	switch scenario {
	case "partial-cache":
		capturePut(t, cache, []byte("partial"), 0600)
	case "enabled-before":
		enabled = true
	case "active-before":
		active = true
	case "override-before":
		override = true
	case "missing-property":
		missing = true
	case "unknown-before":
		unknown = true
	case "reload-failed":
		failReload = true
	case "unknown-after":
		afterUnknown = true
	case "missing-file-receipt":
		if e := os.Remove(filepath.Join(paths.journals, mailCaptureTestOperation+".files-forward.json")); e != nil {
			t.Fatal(e)
		}
	case "bad-intent":
		capturePut(t, intent, []byte("owner intent"), 0600)
	}
	if !rollbackOnly {
		err = reloadMailFilesAt(context.Background(), mailCaptureTestOperation, sha, "forward", 9, paths, commands, checkpoint)
		switch scenario {
		case "enabled-before", "active-before", "override-before", "missing-property", "unknown-before", "missing-file-receipt", "bad-intent":
			if err == nil || reloads != 0 {
				t.Fatal("unverified native state adopted", err, reloads)
			}
			if _, e := os.Stat(receipt); !os.IsNotExist(e) {
				t.Fatal("false completion", e)
			}
			assertMailNativeSide(t, paths, capture, plan, true)
			return
		case "owner-after":
			if err == nil || reloads != 1 || !enabled {
				t.Fatal("owner edit lost", err, reloads)
			}
			if _, e := os.Stat(receipt); !os.IsNotExist(e) {
				t.Fatal("false completion", e)
			}
			return
		case "reload-failed", "unknown-after":
			if err == nil || reloads != 1 {
				t.Fatal("unverified reload completed", err, reloads)
			}
			first, e := os.Stat(intent)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(receipt); !os.IsNotExist(e) {
				t.Fatal("false completion", e)
			}
			failReload, afterUnknown = false, false
			err = reloadMailFilesAt(context.Background(), mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil)
			after, e := os.Stat(intent)
			if e != nil || !os.SameFile(first, after) || reloads != 2 {
				t.Fatal("intent reset", e, reloads)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		count := reloads
		if scenario == "owner-after-completed" {
			enabled = true
		}
		err = reloadMailFilesAt(context.Background(), mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil)
		if scenario == "owner-after-completed" {
			if err == nil || reloads != count || !enabled {
				t.Fatal("historical receipt hid owner edit", err)
			}
			return
		}
		if err != nil || reloads != count {
			t.Fatal("completed reload repeated", err, reloads)
		}
		raw, e := os.ReadFile(receipt)
		if e != nil {
			t.Fatal(e)
		}
		var record mailLoadedRecord
		if e = decodePromotion(raw, &record); e != nil {
			t.Fatal(e)
		}
		if record.Schema != mailBootstrapLoadedSchema || record.Timer != (mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}) {
			t.Fatal("loading claimed activation", record)
		}
		if err = applyMailFilesAt(mailCaptureTestOperation, sha, "rollback", 9, paths, nil); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "rollback-enabled" {
		enabled = true
	}
	beforeReloads := reloads
	err = reloadMailFilesAt(context.Background(), mailCaptureTestOperation, sha, "rollback", 9, paths, commands, checkpoint)
	if scenario == "rollback-enabled" {
		if err == nil || reloads != beforeReloads || !enabled {
			t.Fatal("rollback changed owner enablement", err, reloads)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	assertMailNativeSide(t, paths, capture, plan, false)
	raw, e := os.ReadFile(cache)
	if e != nil || string(raw) != "absent" {
		t.Fatal("native absence unverified", e)
	}
	// Rollback intent is monotonic even though a historical forward receipt exists.
	if err = reloadMailFilesAt(context.Background(), mailCaptureTestOperation, sha, "forward", 9, paths, commands, nil); err == nil {
		t.Fatal("forward admitted after compensation")
	}
}
func TestMailBootstrapLoadedProcessKill(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, kind := range []string{"absent", "legacy"} {
		for _, direction := range []string{"forward", "rollback"} {
			for _, point := range []string{"intent_durable", "intent_published", "intent_parent_durable", "reloaded", "receipt_durable", "receipt_published", "receipt_parent_durable"} {
				t.Run(kind+"/"+direction+"/"+point, func(t *testing.T) {
					root, lock := mailRenewalTestRoot(t)
					_, target, _ := mailCaptureFixture(t, root, kind)
					if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
						t.Fatal(e)
					}
					child := mailBootstrapChild(root, target, "cut-loaded_"+direction+"_"+point, lock)
					out, e := child.CombinedOutput()
					if e == nil {
						t.Fatal("kill absent", string(out))
					}
					status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
					if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
						t.Fatalf("wrong cut %v %s", e, out)
					}
					if out, e = mailBootstrapChild(root, target, "resume-"+direction, lock).CombinedOutput(); e != nil {
						t.Fatalf("resume %v %s", e, out)
					}
				})
			}
		}
	}
}
