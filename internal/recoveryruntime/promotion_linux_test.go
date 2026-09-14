//go:build linux

package recoveryruntime

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func promotionFixturePaths(root string) promotionPaths {
	paths := enrollmentCrashTestPaths(root)
	return promotionPaths{paths, filepath.Join(filepath.Dir(paths.selection), "recovery-promotions/v1")}
}
func replaceFixtureFile(t *testing.T, root, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), raw, 0755); err != nil {
		t.Fatal(err)
	}
	manifestRaw, err := os.ReadFile(filepath.Join(root, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Files[path] = Digest(raw)
	encoded, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestName), encoded, 0644); err != nil {
		t.Fatal(err)
	}
}
func promotionFixture(t *testing.T, root string) (promotionPaths, string, string) {
	t.Helper()
	paths := promotionFixturePaths(root)
	old := filepath.Join(root, "bundle")
	target := filepath.Join(root, "next")
	makePackagedKit(t, old)
	makePackagedKit(t, target)
	replaceFixtureFile(t, target, "update.sh", []byte("different verified target\n"))
	if err := enrollAt(old, 9, paths.enrollmentPaths); err != nil {
		t.Fatal(err)
	}
	return paths, old, target
}
func acceptPromotionCheck(string, string) error { return nil }
func fixtureIdentity(t *testing.T, path string) unix.Stat_t {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		t.Fatal(err)
	}
	return stat
}
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func unchangedPromotionFixture(t *testing.T, before map[string][]byte) {
	t.Helper()
	for path, raw := range before {
		if !bytes.Equal(raw, readFixture(t, path)) {
			t.Fatalf("changed refusal object %s", path)
		}
	}
}

func TestPromotionInheritedLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	cases := []string{"success", "same-target", "next-promotion", "bad-checker", "bad-source", "active", "no-lock", "pending-enroll", "foreign-target", "owner-selector", "owner-launcher", "corrupt-old-kit", "corrupt-target-kit", "corrupt-intent", "orphan-receipt", "unknown-journal", "unsafe-journal", "bad-stage", "history-isolated", "resume-rechecks", "active-handoff", "completion-handoff", "ambiguous-handoff", "foreign-fd", "launcher-xattr", "selector-xattr", "replay-xattr", "owner-acquire", "unknown-transaction", "retired-loss"}
	for _, scenario := range cases {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-promotion-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			os.Mkdir(filepath.Join(root, "transaction"), 0700)
			lock, err := os.OpenFile(filepath.Join(root, "transaction/transaction.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestPromotionChild$", "-test.v")
			command.Env = append(os.Environ(), "CP_PROMOTION_ROOT="+root, "CP_PROMOTION_CASE="+scenario)
			command.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
			if out, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
func TestPromotionChild(t *testing.T) {
	root := os.Getenv("CP_PROMOTION_ROOT")
	if root == "" {
		t.Skip("isolated native child only")
	}
	paths, _, target := promotionFixture(t, root)
	scenario := os.Getenv("CP_PROMOTION_CASE")
	if status, err := inspectPromotionAt(paths); err != nil || status.Phase != "none" {
		t.Fatal("initial observation", status, err)
	}
	if scenario == "owner-acquire" {
		for _, name := range []string{"bin/panel-checker", "bin/agent-checker"} {
			replaceFixtureFile(t, target, name, []byte("#!/bin/sh\nexit 0\n"))
		}
	}
	oldSelection := readFixture(t, paths.selection)
	oldLauncher := fixtureIdentity(t, paths.launcher)
	request := PromotionRequest{target, "--normal"}
	switch scenario {
	case "no-lock":
		unix.Flock(9, unix.LOCK_UN)
	case "unknown-transaction":
		os.WriteFile(filepath.Join(paths.transaction, "foreign.pending"), []byte("unknown authority"), 0600)
	case "active":
		os.WriteFile(filepath.Join(paths.transaction, "active"), []byte("existing operation"), 0600)
	case "bad-source":
		os.WriteFile(filepath.Join(target, "rollback.sh"), []byte("owner edit"), 0755)
	case "launcher-xattr":
		if err := unix.Setxattr(paths.launcher, "user.owner-setting", []byte("preserve"), 0); err != nil {
			t.Fatal(err)
		}
	case "selector-xattr":
		if err := unix.Setxattr(paths.selection, "user.owner-setting", []byte("preserve"), 0); err != nil {
			t.Fatal(err)
		}
	case "history-isolated":
		os.MkdirAll(filepath.Join(paths.promotions, "completed-"+strings.Repeat("f", 32)), 0700)
		os.WriteFile(filepath.Join(paths.promotions, "completed-"+strings.Repeat("f", 32), "intent.json"), []byte("historical malformed record"), 0600)
	}
	checker := acceptPromotionCheck
	if scenario == "bad-checker" {
		checker = func(string, string) error { return fail(ReasonUnsupported) }
	}
	oldLauncher = fixtureIdentity(t, paths.launcher)
	if scenario == "no-lock" || scenario == "active" || scenario == "bad-source" || scenario == "bad-checker" || scenario == "launcher-xattr" || scenario == "selector-xattr" || scenario == "unknown-transaction" {
		if err := promoteAt(request, 9, paths, nil, checker); err == nil {
			t.Fatal("unsafe promotion accepted")
		}
		if !bytes.Equal(oldSelection, readFixture(t, paths.selection)) || !sameFile(oldLauncher, fixtureIdentity(t, paths.launcher)) {
			t.Fatal("entry changed on refusal")
		}
		if _, err := os.Lstat(filepath.Join(paths.promotions, "current")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("intent on preflight refusal", err)
		}
		return
	}
	pendingCases := map[string]bool{"pending-enroll": true, "foreign-target": true, "owner-selector": true, "owner-launcher": true, "corrupt-old-kit": true, "corrupt-target-kit": true, "corrupt-intent": true, "orphan-receipt": true, "unknown-journal": true, "unsafe-journal": true, "bad-stage": true, "resume-rechecks": true, "active-handoff": true, "completion-handoff": true, "ambiguous-handoff": true, "foreign-fd": true, "replay-xattr": true, "owner-acquire": true}
	if pendingCases[scenario] {
		stop := errors.New("fixture interruption")
		checks := 0
		err := promoteAt(request, 9, paths, nil, func(string, string) error {
			checks++
			if checks == 2 {
				return stop
			}
			return nil
		})
		if !errors.Is(err, stop) {
			t.Fatal("fixture did not stop after durable intent", err)
		}
		proof, missing, err := readPromotion(paths)
		if err != nil || missing {
			t.Fatal(err)
		}
		record := proof.record
		proof.close()
		saved := map[string][]byte{paths.selection: readFixture(t, paths.selection), paths.launcher: readFixture(t, paths.launcher), filepath.Join(paths.promotions, "current/intent.json"): readFixture(t, filepath.Join(paths.promotions, "current/intent.json"))}
		switch scenario {
		case "pending-enroll":
			if err := enrollAt(target, 9, paths.enrollmentPaths); err == nil {
				t.Fatal("pending promotion allowed ordinary enrollment")
			}
			unchangedPromotionFixture(t, saved)
			return
		case "foreign-target":
			other := filepath.Join(root, "other")
			makePackagedKit(t, other)
			replaceFixtureFile(t, other, "update.sh", []byte("third target"))
			if err := promoteAt(PromotionRequest{other, "--normal"}, 9, paths, nil, checker); err == nil {
				t.Fatal("foreign promotion adopted")
			}
			unchangedPromotionFixture(t, saved)
			return
		case "owner-selector":
			os.WriteFile(paths.selection, []byte("owner selector"), 0600)
		case "owner-launcher":
			os.WriteFile(paths.launcher, []byte("owner executable"), 0755)
		case "corrupt-old-kit":
			os.WriteFile(filepath.Join(paths.runtimeRoot, record.Previous, "rollback.sh"), []byte("corrupt predecessor"), 0755)
		case "corrupt-target-kit":
			os.WriteFile(filepath.Join(paths.runtimeRoot, record.Target, "update.sh"), []byte("corrupt target"), 0755)
		case "corrupt-intent":
			os.WriteFile(filepath.Join(paths.promotions, "current/intent.json"), []byte("{}\n"), 0600)
		case "orphan-receipt":
			os.Remove(filepath.Join(paths.promotions, "current/intent.json"))
			os.WriteFile(filepath.Join(paths.promotions, "current/committed.json"), []byte("{}\n"), 0600)
		case "unknown-journal":
			os.WriteFile(filepath.Join(paths.promotions, "current/foreign"), []byte("unknown"), 0600)
		case "unsafe-journal":
			os.Chmod(filepath.Join(paths.promotions, "current/intent.json"), 0666)
		case "replay-xattr":
			if err := unix.Setxattr(record.launcherStage(paths), "user.owner-setting", []byte("preserve"), 0); err != nil {
				t.Fatal(err)
			}
		case "bad-stage":
			os.Remove(record.launcherStage(paths))
			os.Symlink(paths.launcher, record.launcherStage(paths))
		case "resume-rechecks":
			calls := 0
			err := resumePromotionAt(9, paths, nil, func(string, string) error { calls++; return fail(ReasonUnsupported) })
			if err == nil || calls != 1 {
				t.Fatal("resume bypassed compatibility", err, calls)
			}
			unchangedPromotionFixture(t, saved)
			return
		case "active-handoff", "completion-handoff", "ambiguous-handoff":
			if scenario == "completion-handoff" {
				// Simulate a completed pair whose terminal promotion receipt was not durable.
				proof, _, _ := readPromotion(paths)
				if err := exchangePromotion(paths, proof, 9, true); err != nil {
					t.Fatal(err)
				}
				if err := exchangePromotion(paths, proof, 9, false); err != nil {
					t.Fatal(err)
				}
				proof.close()
			}
			raw := []byte("version=1\ntoken=" + strings.Repeat("a", 64) + "\noperation=rollback\nsnapshot=20260914T000000Z-from-unknown-to-" + strings.Repeat("b", 40) + "-" + strings.Repeat("c", 32) + "\n")
			name := "active"
			if scenario == "completion-handoff" {
				name = "completion.pending"
			}
			os.WriteFile(filepath.Join(paths.transaction, name), raw, 0600)
			if scenario == "completion-handoff" {
				os.WriteFile(filepath.Join(paths.transaction, "scheduler-restore.pending"), raw, 0600)
			}
			if scenario == "ambiguous-handoff" {
				os.WriteFile(filepath.Join(paths.transaction, "completion.pending"), raw, 0600)
			}
			before := readFixture(t, paths.selection)
			err := resumePromotionOwnerLocked(paths)
			if (scenario == "ambiguous-handoff") != (err != nil) {
				t.Fatal("handoff classification", err)
			}
			if !bytes.Equal(before, readFixture(t, paths.selection)) {
				t.Fatal("handoff changed selected state")
			}
			if _, err := os.Lstat(filepath.Join(paths.promotions, "current/committed.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("handoff wrote promotion receipt")
			}
			return
		case "owner-acquire":
			if err := unix.Flock(9, unix.LOCK_UN); err != nil {
				t.Fatal(err)
			}
			if err := unix.Close(9); err != nil {
				t.Fatal(err)
			}
			for number := 3; number < 9; number++ {
				var stat unix.Stat_t
				if err := unix.Fstat(number, &stat); errors.Is(err, unix.EBADF) {
					filler, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
					if err != nil {
						t.Fatal(err)
					}
					if filler != number {
						if err := unix.Dup3(filler, number, unix.O_CLOEXEC); err != nil {
							t.Fatal(err)
						}
						unix.Close(filler)
					}
					defer unix.Close(number)
				}
			}
			if err := resumePromotionForOwnerAt(paths); err != nil {
				t.Fatal("owner could not acquire native FD9", err)
			}
			var stat unix.Stat_t
			if err := unix.Fstat(9, &stat); !errors.Is(err, unix.EBADF) {
				t.Fatal("owner helper leaked native FD9", err)
			}
			if status, err := inspectPromotionAt(paths); err != nil || status.Phase != "committed" {
				t.Fatal("owner did not finish", status, err)
			}
			return
		case "foreign-fd":
			unix.Close(9)
			foreign, err := os.OpenFile(filepath.Join(root, "foreign"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer foreign.Close()
			if foreign.Fd() != 9 {
				unix.Dup3(int(foreign.Fd()), 9, 0)
			}
			var before, after unix.Stat_t
			unix.Fstat(9, &before)
			if err := resumePromotionForOwnerAt(paths); err == nil {
				t.Fatal("foreign FD accepted")
			}
			unix.Fstat(9, &after)
			if !sameFile(before, after) {
				t.Fatal("foreign FD overwritten")
			}
			return
		}
		before := map[string][]byte{paths.selection: readFixture(t, paths.selection), paths.launcher: readFixture(t, paths.launcher)}
		if status, err := inspectPromotionAt(paths); err == nil || status.Phase != "" {
			t.Fatal("corruption produced known observation", status, err)
		}
		if _, err := promotionPendingAt(paths); err == nil {
			t.Fatal("corrupt pending read succeeded")
		}
		if err := resumePromotionAt(9, paths, nil, checker); err == nil {
			t.Fatal("corrupt pending resumed")
		}
		unchangedPromotionFixture(t, before)
		return
	}
	if err := promoteAt(request, 9, paths, nil, checker); err != nil {
		t.Fatal(err)
	}
	proof, missing, err := readPromotion(paths)
	if err != nil || missing || !proof.committed {
		t.Fatal("not committed", err)
	}
	record := proof.record
	proof.close()
	if pending, err := promotionPendingAt(paths); err != nil || pending {
		t.Fatal("committed pending", pending, err)
	}
	if oldLauncher.Dev != fixtureIdentity(t, record.launcherStage(paths)).Dev || oldLauncher.Ino != fixtureIdentity(t, record.launcherStage(paths)).Ino {
		t.Fatal("old launcher inode not retained")
	}
	if !bytes.Equal(oldSelection, readFixture(t, record.selectionStage(paths))) {
		t.Fatal("old selector not retained")
	}
	if raw := readFixture(t, paths.selection); !bytes.Contains(raw, []byte(record.Target)) {
		t.Fatal("target not selected")
	}
	if scenario == "retired-loss" {
		os.Rename(filepath.Join(paths.runtimeRoot, record.Previous), filepath.Join(root, "retired-kit-preserved"))
		os.Rename(record.launcherStage(paths), filepath.Join(root, "retired-launcher-preserved"))
		os.Rename(record.selectionStage(paths), filepath.Join(root, "retired-selection-preserved"))
		if status, err := inspectPromotionAt(paths); err != nil || status.Phase != "committed" {
			t.Fatal("retired history blocked current status", status, err)
		}
		if pending, err := promotionPendingAt(paths); err != nil || pending {
			t.Fatal("retired history blocked recovery", pending, err)
		}
		if err := resumePromotionAt(9, paths, nil, checker); err != nil {
			t.Fatal("completed receipt unnecessarily needs predecessor", err)
		}
		selected, err := resolveAt(resolveConfig{runtimeRoot: paths.runtimeRoot, selectionPath: paths.selection, anchor: "/", uid: 0, gid: 0})
		if err != nil {
			t.Fatal(err)
		}
		defer selected.Close()
		if err := selected.Revalidate(); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "same-target" {
		before := fixtureIdentity(t, paths.selection)
		if err := promoteAt(request, 9, paths, nil, checker); err != nil {
			t.Fatal(err)
		}
		if !sameFile(before, fixtureIdentity(t, paths.selection)) {
			t.Fatal("same target changed selection")
		}
	}
	if scenario == "next-promotion" {
		next := filepath.Join(root, "third")
		makePackagedKit(t, next)
		replaceFixtureFile(t, next, "rollback.sh", []byte("third payload\n"))
		if err := promoteAt(PromotionRequest{next, "--normal"}, 9, paths, nil, checker); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(paths.promotions, "completed-"+record.Nonce, "intent.json")); err != nil {
			t.Fatal("previous receipt lost", err)
		}
		for _, digest := range []string{record.Previous, record.Target} {
			if kit, err := VerifyBundle(filepath.Join(paths.runtimeRoot, digest), false); err != nil {
				t.Fatal("predecessor kit not retained", err)
			} else {
				kit.Close()
			}
		}
	}
}

func TestPromotionActualSIGKILL(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	for _, phase := range []string{"intent_published", "launcher_exchanged", "selector_exchanged", "committed"} {
		t.Run(phase, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-promotion-crash-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			os.Mkdir(filepath.Join(root, "transaction"), 0700)
			lock, err := os.OpenFile(filepath.Join(root, "transaction/transaction.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
			run := func(action string) ([]byte, error) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestPromotionCrashChild$", "-test.v")
				cmd.Env = append(os.Environ(), "CP_PROMOTION_CRASH_ROOT="+root, "CP_PROMOTION_CRASH_PHASE="+phase, "CP_PROMOTION_CRASH_ACTION="+action)
				cmd.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
				return cmd.CombinedOutput()
			}
			out, err := run("kill")
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
				t.Fatalf("not SIGKILL %v %s", err, out)
			}
			wantPhase := map[string]string{"intent_published": "prepared", "launcher_exchanged": "launcher_published", "selector_exchanged": "selection_published", "committed": "committed"}[phase]
			if status, err := inspectPromotionAt(promotionFixturePaths(root)); err != nil || status.Phase != wantPhase {
				t.Fatal("durable phase observation", status, wantPhase, err)
			}
			// Entire accepted source trees are unavailable to the resume process.
			os.Rename(filepath.Join(root, "bundle"), filepath.Join(root, "source-preserved"))
			os.Rename(filepath.Join(root, "next"), filepath.Join(root, "candidate-preserved"))
			if out, err := run("resume"); err != nil {
				t.Fatalf("resume %v %s", err, out)
			}
		})
	}
}
func TestPromotionCrashChild(t *testing.T) {
	root := os.Getenv("CP_PROMOTION_CRASH_ROOT")
	if root == "" {
		t.Skip("isolated crash child")
	}
	paths := promotionFixturePaths(root)
	if os.Getenv("CP_PROMOTION_CRASH_ACTION") == "kill" {
		_, _, target := promotionFixture(t, root)
		err := promoteAt(PromotionRequest{target, "--normal"}, 9, paths, func(phase string) {
			if phase == os.Getenv("CP_PROMOTION_CRASH_PHASE") {
				unix.Kill(os.Getpid(), unix.SIGKILL)
				select {}
			}
		}, acceptPromotionCheck)
		t.Fatal("crash did not occur", err)
	}
	if err := resumePromotionAt(9, paths, nil, acceptPromotionCheck); err != nil {
		t.Fatal(err)
	}
	proof, absent, err := readPromotion(paths)
	if err != nil || absent || !proof.committed {
		t.Fatal("resume not terminal", err)
	}
	defer proof.close()
	if a, b, err := verifyPromotionState(paths, proof); err != nil || !a || !b {
		t.Fatal("terminal proof", a, b, err)
	}
}

func TestPromotionFixedEntryRealExec(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	root, err := os.MkdirTemp("/run", "celikpanel-promotion-exec-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	os.Mkdir(filepath.Join(root, "transaction"), 0700)
	lock, err := os.OpenFile(filepath.Join(root, "transaction/transaction.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	setup := exec.Command(os.Args[0], "-test.run=^TestPromotionDispatcherSetup$", "-test.v")
	setup.Env = append(os.Environ(), "CP_PROMOTION_EXEC_ROOT="+root)
	setup.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	out, err := setup.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("setup %v %s", err, out)
	}
	paths := promotionFixturePaths(root)
	before := readFixture(t, paths.selection)
	filler, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer filler.Close()
	foreign, err := os.OpenFile(filepath.Join(root, "foreign-fd"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	for _, mode := range []string{"native", "without-lock", "foreign"} {
		cmd := exec.Command(paths.launcher, "-test.run=^TestPromotionDispatcherChild$", "-test.v")
		cmd.Env = append(os.Environ(), "CP_PROMOTION_EXEC_ROOT="+root, "CP_PROMOTION_EXEC_MODE="+mode, "CP_SECRET=not-to-inherit")
		cmd.ExtraFiles = []*os.File{filler, filler, filler, filler, filler, filler}
		if mode == "without-lock" {
			cmd.ExtraFiles = append(cmd.ExtraFiles, filler)
		}
		if mode == "native" {
			cmd.ExtraFiles = append(cmd.ExtraFiles, lock)
		}
		if mode == "foreign" {
			cmd.ExtraFiles = append(cmd.ExtraFiles, foreign)
		}
		out, err = cmd.CombinedOutput()
		if !errors.As(err, &exit) || exit.ExitCode() != 37 {
			t.Fatalf("%s real exec failed %v %s", mode, err, out)
		}
	}
	if !bytes.Equal(before, readFixture(t, paths.selection)) {
		t.Fatal("read-only dispatch promoted selection")
	}
}
func TestPromotionDispatcherSetup(t *testing.T) {
	root := os.Getenv("CP_PROMOTION_EXEC_ROOT")
	if root == "" {
		t.Skip("isolated dispatcher fixture")
	}
	paths := promotionFixturePaths(root)
	old := filepath.Join(root, "bundle")
	target := filepath.Join(root, "next")
	makePackagedKit(t, old)
	makePackagedKit(t, target)
	bash := readFixture(t, "/usr/bin/bash")
	self := readFixture(t, "/proc/self/exe")
	replaceFixtureFile(t, old, "bin/recovery", bash)
	replaceFixtureFile(t, target, "bin/recovery", self)
	if err := enrollAt(old, 9, paths.enrollmentPaths); err != nil {
		t.Fatal(err)
	}
	err := promoteAt(PromotionRequest{target, "--normal"}, 9, paths, func(phase string) {
		if phase == "launcher_exchanged" {
			unix.Kill(os.Getpid(), unix.SIGKILL)
			select {}
		}
	}, acceptPromotionCheck)
	t.Fatal("did not kill", err)
}
func TestPromotionDispatcherChild(t *testing.T) {
	root := os.Getenv("CP_PROMOTION_EXEC_ROOT")
	if root == "" {
		t.Skip("isolated dispatcher child")
	}
	paths := promotionFixturePaths(root)
	entry, err := isLauncherEntryAt(paths.launcher)
	if err != nil || !entry {
		t.Fatal("not fixed entry", entry, err)
	}
	mode := os.Getenv("CP_PROMOTION_EXEC_MODE")
	if mode == "without-lock" {
		if err := unix.Close(9); err != nil {
			t.Fatal(err)
		}
	}
	if mode != "without-lock" {
		if _, err := unix.FcntlInt(9, unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := verifiedLauncherRuntimeAt(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err := runtime.VerifyExecutingBinary(); err == nil {
		t.Fatal("new dispatcher unexpectedly old selected executable")
	}
	fdCondition := "test -e /proc/self/fd/9"
	if mode != "native" {
		fdCondition = "test ! -e /proc/self/fd/9"
	}
	if mode == "without-lock" {
		owned := false
		for _, directory := range runtime.state.directories {
			if directory.file.Fd() == 9 {
				owned = true
			}
		}
		for _, file := range runtime.state.files {
			if file.file.Fd() == 9 {
				owned = true
			}
		}
		if !owned {
			t.Fatal("fixture did not place internal proof on FD9")
		}
	}
	if err := runtime.execSelectedAt([]string{"-c", fdCondition + " && test -z \"${CP_SECRET:-}\" && exit 37; exit 42"}, paths.transaction); err != nil {
		t.Fatal(err)
	}
}
func TestPromotionCanonicalRecordRejectsExtraData(t *testing.T) {
	value := promotionReceipt{promotionReceiptSchema, strings.Repeat("a", 64)}
	raw, _ := promotionJSON(value)
	var decoded promotionReceipt
	if err := decodePromotion(raw, &decoded); err != nil || !reflect.DeepEqual(value, decoded) {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte{}, raw...), []byte("{}\n")...), bytes.Replace(raw, []byte("}\n"), []byte(",\"unknown\":true}\n"), 1), bytes.TrimSpace(raw)} {
		if decodePromotion(bad, &decoded) == nil {
			t.Fatal("noncanonical receipt accepted")
		}
	}
}

func TestPromotionCommittedEntryIgnoresRetiredLoss(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	root, err := os.MkdirTemp("/run", "celikpanel-promotion-retired-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	os.Mkdir(filepath.Join(root, "transaction"), 0700)
	lock, err := os.OpenFile(filepath.Join(root, "transaction/transaction.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	setup := exec.Command(os.Args[0], "-test.run=^TestPromotionCommittedSetup$", "-test.v")
	setup.Env = append(os.Environ(), "CP_PROMOTION_RETIRED_ROOT="+root)
	setup.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("setup %v %s", err, out)
	}
	paths := promotionFixturePaths(root)
	proof, absent, err := readPromotion(paths)
	if err != nil || absent {
		t.Fatal(err)
	}
	record := proof.record
	proof.close()
	os.Rename(filepath.Join(paths.runtimeRoot, record.Previous), filepath.Join(root, "preserved-old-kit"))
	os.Rename(record.launcherStage(paths), filepath.Join(root, "preserved-old-launcher"))
	os.Rename(record.selectionStage(paths), filepath.Join(root, "preserved-old-selector"))
	child := exec.Command(paths.launcher, "-test.run=^TestPromotionCommittedEntryChild$", "-test.v")
	child.Env = append(os.Environ(), "CP_PROMOTION_RETIRED_ROOT="+root)
	child.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("healthy selected entry depends on retired files: %v %s", err, out)
	}
}
func TestPromotionCommittedSetup(t *testing.T) {
	root := os.Getenv("CP_PROMOTION_RETIRED_ROOT")
	if root == "" {
		t.Skip("retired fixture setup")
	}
	paths := promotionFixturePaths(root)
	old := filepath.Join(root, "bundle")
	target := filepath.Join(root, "next")
	makePackagedKit(t, old)
	makePackagedKit(t, target)
	replaceFixtureFile(t, old, "bin/recovery", readFixture(t, "/usr/bin/bash"))
	replaceFixtureFile(t, target, "bin/recovery", readFixture(t, "/proc/self/exe"))
	if err := enrollAt(old, 9, paths.enrollmentPaths); err != nil {
		t.Fatal(err)
	}
	if err := promoteAt(PromotionRequest{target, "--normal"}, 9, paths, nil, acceptPromotionCheck); err != nil {
		t.Fatal(err)
	}
}
func TestPromotionCommittedEntryChild(t *testing.T) {
	root := os.Getenv("CP_PROMOTION_RETIRED_ROOT")
	if root == "" {
		t.Skip("retired fixture entry")
	}
	paths := promotionFixturePaths(root)
	runtime, err := verifiedLauncherRuntimeAt(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err := runtime.VerifyExecutingBinary(); err != nil {
		t.Fatal(err)
	}
	if pending, err := promotionPendingAt(paths); err != nil || pending {
		t.Fatal("cannot execute selected recovery", pending, err)
	}
	if status, err := inspectPromotionAt(paths); err != nil || status.Phase != "committed" {
		t.Fatal(status, err)
	}
}
