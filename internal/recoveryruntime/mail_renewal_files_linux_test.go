//go:build linux

package recoveryruntime

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

func mailFilesChild(root, target, kind, scenario string, lock *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailFilesChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_MAIL_FILES_ROOT="+root, "CP_MAIL_FILES_TARGET="+target, "CP_MAIL_FILES_KIND="+kind, "CP_MAIL_FILES_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	return c
}
func TestMailFilesForwardAndInversePreserveIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, kind := range []string{"absent", "legacy", "independent"} {
		t.Run(kind, func(t *testing.T) {
			root, lock := mailRenewalTestRoot(t)
			_, target, _ := mailCaptureFixture(t, root, kind)
			if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
				t.Fatal(e)
			}
			if out, e := mailFilesChild(root, target, kind, "normal", lock).CombinedOutput(); e != nil {
				t.Fatalf("%v %s", e, out)
			}
		})
	}
}
func TestMailFilesRefuseOwnerDrift(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"captured-native-edit", "captured-inode-replace", "staged-edit", "staged-extra", "staged-hardlink", "staged-inode-replace", "staged-dir-replace", "after-native-edit", "after-native-inode-replace", "after-source-edit", "plan-edit", "forward-receipt-edit", "rollback-intent-edit", "late-native-edit", "late-journal-move", "late-unit-dir-move", "late-active", "late-stage-edit", "unlocked-prepare", "active-prepare", "unlocked-apply", "active-apply", "partial-owner-edit"} {
		t.Run(scenario, func(t *testing.T) {
			root, lock := mailRenewalTestRoot(t)
			_, target, _ := mailCaptureFixture(t, root, "independent")
			if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
				t.Fatal(e)
			}
			if out, e := mailFilesChild(root, target, "independent", scenario, lock).CombinedOutput(); e != nil {
				t.Fatalf("%v %s", e, out)
			}
		})
	}
}
func TestMailFilesChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_FILES_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, kind, scenario := os.Getenv("CP_MAIL_FILES_TARGET"), os.Getenv("CP_MAIL_FILES_KIND"), os.Getenv("CP_MAIL_FILES_CASE")
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	timer := mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}
	if kind == "independent" {
		timer = mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}
	}
	if scenario == "protected-parent-group" {
		if e := os.Chown(filepath.Dir(paths.hook), 0, 65534); e != nil {
			t.Fatal(e)
		}
	}
	capturePath := filepath.Join(paths.journals, mailCaptureTestOperation+".json")
	captured, e := os.ReadFile(capturePath)
	if os.IsNotExist(e) {
		captured, e = captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, timer, 9, paths, nil)
	}
	if e != nil {
		t.Fatal(e)
	}
	captureSHA := Digest(captured)
	var initial mailCaptureRecord
	if e = decodePromotion(captured, &initial); e != nil {
		t.Fatal(e)
	}
	planPath := filepath.Join(paths.journals, mailCaptureTestOperation+".files.json")
	replaceSame := func(path string) {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		st, e := os.Stat(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.Rename(path, path+".owner"); e != nil {
			t.Fatal(e)
		}
		capturePut(t, path, raw, st.Mode().Perm())
	}
	changedPath := ""
	var ownerRaw []byte
	ownerEdit := func(path string, mode os.FileMode) {
		changedPath = path
		ownerRaw = []byte("explicit owner bytes\n")
		capturePut(t, path, ownerRaw, mode)
	}
	assertOwner := func() {
		if changedPath != "" {
			raw, e := os.ReadFile(changedPath)
			if e != nil || !bytes.Equal(raw, ownerRaw) {
				t.Fatal("owner bytes lost", e)
			}
		}
	}
	switch scenario {
	case "unlocked-prepare":
		if e = unix.Flock(9, unix.LOCK_UN); e != nil {
			t.Fatal(e)
		}
	case "active-prepare":
		capturePut(t, filepath.Join(paths.transaction, "active"), []byte("other operation"), 0600)
	case "captured-native-edit":
		ownerEdit(paths.hook, 0755)
	case "captured-inode-replace":
		replaceSame(paths.hook)
	}
	checkpoint := func(phase string) {
		if scenario == "cut-"+phase || scenario == "recover-cut-"+phase {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
		if phase == "forward_"+mailrenewalkit.ServiceName && scenario == "partial-owner-edit" {
			ownerEdit(paths.hook, 0755)
		}
		if phase == "plan_durable" {
			switch scenario {
			case "late-native-edit":
				ownerEdit(paths.hook, 0755)
			case "late-journal-move":
				captureMoveJournal(t, paths.journals)
			case "late-unit-dir-move":
				captureMoveJournal(t, paths.units)
			case "late-active":
				capturePut(t, filepath.Join(paths.transaction, "active"), []byte("other operation"), 0600)
			case "late-stage-edit":
				matches, _ := filepath.Glob(filepath.Join(filepath.Dir(paths.hook), ".celikpanel-mail-transition-*", mailrenewalkit.HookName))
				if len(matches) != 1 {
					t.Fatal(matches)
				}
				ownerEdit(matches[0], 0755)
			}
		}
	}
	planRaw, e := prepareMailFilesAt(mailCaptureTestOperation, captureSHA, 9, paths, checkpoint)
	if strings.HasPrefix(scenario, "captured-") || strings.HasPrefix(scenario, "late-") || strings.HasSuffix(scenario, "-prepare") {
		if e == nil {
			t.Fatal("changed capture prepared")
		}
		assertOwner()
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	var plan mailFilesRecord
	if e = decodePromotion(planRaw, &plan); e != nil {
		t.Fatal(e)
	}
	if scenario == "normal" {
		before, e := os.Stat(planPath)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := prepareMailFilesAt(mailCaptureTestOperation, captureSHA, 9, paths, nil)
		if e != nil || !bytes.Equal(raw, planRaw) {
			t.Fatal("repeat plan differs", e)
		}
		after, e := os.Stat(planPath)
		if e != nil || !os.SameFile(before, after) {
			t.Fatal("plan replaced")
		}
		// Staged hook is below a directory, never a top-level executable Certbot hook.
		entries, e := os.ReadDir(filepath.Dir(paths.hook))
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".celikpanel-mail-transition-") && !entry.IsDir() {
				t.Fatal("executable hook stage exposed")
			}
		}
	}
	stageHook := filepath.Join(filepath.Dir(paths.hook), plan.stageName(), mailrenewalkit.HookName)
	switch scenario {
	case "unlocked-apply":
		if e = unix.Flock(9, unix.LOCK_UN); e != nil {
			t.Fatal(e)
		}
	case "active-apply":
		capturePut(t, filepath.Join(paths.transaction, "active"), []byte("other operation"), 0600)
	case "staged-hardlink":
		if e = os.Link(stageHook, stageHook+".owner"); e != nil {
			t.Fatal(e)
		}
	case "staged-extra":
		ownerEdit(filepath.Join(filepath.Dir(stageHook), "owner-file"), 0600)
	case "staged-edit":
		ownerEdit(stageHook, 0755)
	case "staged-inode-replace":
		replaceSame(stageHook)
	case "staged-dir-replace":
		captureMoveJournal(t, filepath.Dir(stageHook))
	case "plan-edit":
		ownerEdit(planPath, 0600)
	case "forward-receipt-edit":
		ownerEdit(filepath.Join(paths.journals, mailCaptureTestOperation+".files-forward.json"), 0600)
	case "rollback-intent-edit":
		ownerEdit(filepath.Join(paths.journals, mailCaptureTestOperation+".files-rollback-intent.json"), 0600)
	}
	rollbackOnly := scenario == "resume-rollback" || strings.HasPrefix(scenario, "recover-cut-")
	if !rollbackOnly {
		e = applyMailFilesAt(mailCaptureTestOperation, captureSHA, "forward", 9, paths, checkpoint)
		if scenario == "partial-owner-edit" {
			if e == nil {
				t.Fatal("partial publication overwrote later owner edit")
			}
			assertOwner()
			return
		}
		if strings.HasSuffix(scenario, "-apply") || strings.HasPrefix(scenario, "staged-") || scenario == "plan-edit" || scenario == "forward-receipt-edit" || scenario == "rollback-intent-edit" {
			if e == nil {
				t.Fatal("foreign plan/stage accepted")
			}
			assertOwner()
			assertMailNativeSide(t, paths, initial, plan, false)
			return
		}
		if e != nil {
			t.Fatal(e)
		}
		assertMailNativeSide(t, paths, initial, plan, true)
		if e = applyMailFilesAt(mailCaptureTestOperation, captureSHA, "forward", 9, paths, nil); e != nil {
			t.Fatal("forward repeat", e)
		}
	}
	switch scenario {
	case "after-native-edit":
		ownerEdit(paths.hook, 0755)
	case "after-native-inode-replace":
		replaceSame(paths.hook)
	case "after-source-edit":
		ownerEdit(filepath.Join(paths.runtime, target, mailrenewalkit.BinaryName), 0755)
	}
	e = applyMailFilesAt(mailCaptureTestOperation, captureSHA, "rollback", 9, paths, checkpoint)
	if strings.HasPrefix(scenario, "after-") {
		if e == nil {
			t.Fatal("rollback overwrote owner change")
		}
		assertOwner()
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	assertMailNativeSide(t, paths, initial, plan, false)
	if e = applyMailFilesAt(mailCaptureTestOperation, captureSHA, "rollback", 9, paths, nil); e != nil {
		t.Fatal("rollback repeat", e)
	}
	if e = applyMailFilesAt(mailCaptureTestOperation, captureSHA, "forward", 9, paths, nil); e == nil {
		t.Fatal("rollback operation reversed direction")
	}
	assertMailNativeSide(t, paths, initial, plan, false)
	if raw, e := os.ReadFile(capturePath); e != nil || !bytes.Equal(raw, captured) {
		t.Fatal("capture altered", e)
	}
}
func assertMailNativeSide(t *testing.T, paths mailCapturePaths, capture mailCaptureRecord, plan mailFilesRecord, after bool) {
	t.Helper()
	for _, name := range mailNativeNames() {
		path := filepath.Join(paths.units, name)
		if name == mailrenewalkit.HookName {
			path = paths.hook
		}
		raw, e := os.ReadFile(path)
		want, ok := capture.Contract.Before[name]
		id := capture.Files[name]
		if after {
			want, ok = capture.Contract.After[name]
			if next, changed := plan.New[name]; changed {
				id = next
			}
		}
		if !ok {
			if !os.IsNotExist(e) {
				t.Fatal("unrecorded native file", path, e)
			}
			continue
		}
		if e != nil || !bytes.Equal(raw, want) {
			t.Fatal("native bytes differ", path, e)
		}
		var st unix.Stat_t
		if e = unix.Lstat(path, &st); e != nil {
			t.Fatal(e)
		}
		if st.Ino != id.Ino || uint64(st.Dev) != id.Dev || st.Mode != id.Mode || st.Uid != id.UID || st.Gid != id.GID || st.Mtim.Sec != id.MtimeSec || st.Mtim.Nsec != id.MtimeNsec {
			t.Fatal("native inode/metadata differs", path)
		}
	}
}
func TestMailFilesResumeAfterSIGKILL(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, kind := range []string{"absent", "legacy", "independent"} {
		phases := []string{"plan_durable", "plan_published", "plan_parent_durable", "forward_receipt_durable", "forward_receipt_published", "forward_receipt_parent_durable", "rollback_intent_durable", "rollback_intent_published", "rollback_intent_parent_durable", "rollback_receipt_durable", "rollback_receipt_published", "rollback_receipt_parent_durable"}
		for _, name := range mailNativeNames() {
			if kind == "independent" && name == mailrenewalkit.TimerName {
				continue
			}
			phases = append(phases, "stage_"+name, "forward_"+name, "forward_synced_"+name, "rollback_"+name, "rollback_synced_"+name)
		}
		for _, phase := range phases {
			t.Run(kind+"/"+phase, func(t *testing.T) {
				root, lock := mailRenewalTestRoot(t)
				_, target, _ := mailCaptureFixture(t, root, kind)
				if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
					t.Fatal(e)
				}
				child := mailFilesChild(root, target, kind, "cut-"+phase, lock)
				out, e := child.CombinedOutput()
				if e == nil {
					t.Fatal("cut not reached", string(out))
				}
				status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatalf("wrong interruption %v %s", e, out)
				}
				scenario := "resume-forward"
				if strings.HasPrefix(phase, "rollback_") {
					scenario = "resume-rollback"
				}
				if out, e = mailFilesChild(root, target, kind, scenario, lock).CombinedOutput(); e != nil {
					t.Fatalf("resume %v %s", e, out)
				}
			})
		}
	}
}

func TestMailFilesInterruptedRollbackKeepsDirection(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	root, lock := mailRenewalTestRoot(t)
	_, target, _ := mailCaptureFixture(t, root, "legacy")
	if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	for _, scenario := range []string{"cut-forward_" + mailrenewalkit.HookName, "recover-cut-rollback_" + mailrenewalkit.HookName} {
		child := mailFilesChild(root, target, "legacy", scenario, lock)
		out, e := child.CombinedOutput()
		if e == nil {
			t.Fatal("cut not reached", string(out))
		}
		status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("wrong cut %v %s", e, out)
		}
	}
	if out, e := mailFilesChild(root, target, "legacy", "resume-rollback", lock).CombinedOutput(); e != nil {
		t.Fatalf("second resume %v %s", e, out)
	}
}

func TestMailFilesRetainProtectedParentGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	root, lock := mailRenewalTestRoot(t)
	paths, target, _ := mailCaptureFixture(t, root, "legacy")
	if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	if out, e := mailFilesChild(root, target, "legacy", "protected-parent-group", lock).CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, out)
	}
	var st unix.Stat_t
	if e := unix.Lstat(filepath.Dir(paths.hook), &st); e != nil {
		t.Fatal(e)
	}
	if st.Gid != 65534 || st.Mode&07777 != 0700 {
		t.Fatal("native parent normalized")
	}
}
