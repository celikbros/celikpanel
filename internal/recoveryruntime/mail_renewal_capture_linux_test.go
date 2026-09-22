//go:build linux

package recoveryruntime

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

const mailCaptureTestOperation = "00112233445566778899aabbccddeeff"

func mailCaptureFixture(t *testing.T, root, kind string) (mailCapturePaths, string, mailrenewalkit.TimerState) {
	t.Helper()
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	for _, dir := range []string{filepath.Dir(paths.hook), paths.units, paths.runtime, paths.journals} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(root, "next")
	target := mailRenewalFixture(t, source, []byte("reviewed target helper"))
	if err := os.Rename(source, filepath.Join(paths.runtime, target)); err != nil {
		t.Fatal(err)
	}
	timer := mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}
	if kind == "legacy" {
		capturePut(t, paths.hook, mailrenewalkit.LegacyHook(), 0755)
	}
	if kind == "independent" {
		previous := mailRenewalFixture(t, source, []byte("reviewed previous helper"))
		if err := os.Rename(source, filepath.Join(paths.runtime, previous)); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{mailrenewalkit.HookName, mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
			raw, err := os.ReadFile(filepath.Join(paths.runtime, previous, name))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(paths.units, name)
			if name == mailrenewalkit.HookName {
				path = paths.hook
			}
			capturePut(t, path, raw, os.FileMode(mailFileMode(name)))
		}
		timer = mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}
	}
	return paths, target, timer
}
func capturePut(t *testing.T, path string, raw []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
func mailCaptureChild(root, target, kind, scenario string, lock *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailCaptureChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_MAIL_CAPTURE_ROOT="+root, "CP_MAIL_CAPTURE_TARGET="+target, "CP_MAIL_CAPTURE_KIND="+kind, "CP_MAIL_CAPTURE_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	return c
}
func TestMailCaptureWithInheritedLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	cases := []string{"ok", "no-lock", "active", "foreign-hook", "foreign-unit", "unsafe-hook", "hook-xattr", "journal-conflict", "late-hook-edit", "late-absent-create", "late-source-edit", "late-parent-move", "late-parent-group", "late-journal-move", "published-journal-move", "published-file-replaced", "late-lock-marker", "journal-missing", "protected-parent-group"}
	for _, kind := range []string{"absent", "legacy", "independent"} {
		for _, scenario := range cases {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				root, lock := mailRenewalTestRoot(t)
				_, target, _ := mailCaptureFixture(t, root, kind)
				if scenario != "no-lock" {
					if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
						t.Fatal(err)
					}
				}
				if out, err := mailCaptureChild(root, target, kind, scenario, lock).CombinedOutput(); err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
			})
		}
	}
}
func TestMailCaptureChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_CAPTURE_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, kind, scenario := os.Getenv("CP_MAIL_CAPTURE_TARGET"), os.Getenv("CP_MAIL_CAPTURE_KIND"), os.Getenv("CP_MAIL_CAPTURE_CASE")
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	timer := mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}
	if kind == "independent" {
		timer = mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}
	}
	final := filepath.Join(paths.journals, mailCaptureTestOperation+".json")
	switch scenario {
	case "active":
		capturePut(t, filepath.Join(paths.transaction, "active"), []byte("owner update"), 0600)
	case "foreign-hook":
		capturePut(t, paths.hook, []byte("owner hook"), 0755)
	case "foreign-unit":
		capturePut(t, filepath.Join(paths.units, mailrenewalkit.ServiceName), []byte("owner unit"), 0644)
	case "unsafe-hook":
		capturePut(t, paths.hook, mailrenewalkit.LegacyHook(), 0775)
	case "hook-xattr":
		capturePut(t, paths.hook, mailrenewalkit.LegacyHook(), 0755)
		if err := unix.Setxattr(paths.hook, "user.owner", []byte("retain"), 0); err != nil {
			t.Fatal(err)
		}
	case "journal-conflict":
		capturePut(t, final, []byte("owner journal"), 0600)
	case "journal-missing":
		if err := os.Remove(paths.journals); err != nil {
			t.Fatal(err)
		}
	case "protected-parent-group":
		if err := os.Chown(filepath.Dir(paths.hook), 0, 65534); err != nil {
			t.Fatal(err)
		}
	}
	before := captureNativeSnapshot(t, paths)
	changed := false
	checkpoint := func(phase string) {
		if scenario == "cut-"+phase {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
		if phase == "image_durable" {
			switch scenario {
			case "late-hook-edit":
				capturePut(t, paths.hook, []byte("later owner hook"), 0755)
				changed = true
			case "late-absent-create":
				capturePut(t, filepath.Join(paths.units, "celikpanel-mail-renewal.service"), []byte("later owner unit"), 0644)
				changed = true
			case "late-source-edit":
				capturePut(t, filepath.Join(paths.runtime, target, mailrenewalkit.BinaryName), []byte("owner helper"), 0755)
				changed = true
			case "late-parent-move":
				if err := os.Rename(filepath.Dir(paths.hook), filepath.Dir(paths.hook)+".owner"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Dir(paths.hook), 0700); err != nil {
					t.Fatal(err)
				}
				changed = true
			case "late-parent-group":
				if err := os.Chown(filepath.Dir(paths.hook), 0, 65534); err != nil {
					t.Fatal(err)
				}
				changed = true
			case "late-journal-move":
				captureMoveJournal(t, paths.journals)
			case "late-lock-marker":
				capturePut(t, filepath.Join(paths.transaction, "active"), []byte("owner update"), 0600)
			}
		}
		if phase == "image_parent_durable" {
			switch scenario {
			case "published-journal-move":
				captureMoveJournal(t, paths.journals)
			case "published-file-replaced":
				raw, err := os.ReadFile(final)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(final, final+".owner"); err != nil {
					t.Fatal(err)
				}
				capturePut(t, final, raw, 0600)
			}
		}
	}
	raw, err := captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, timer, 9, paths, checkpoint)
	good := scenario == "ok" || scenario == "resume" || scenario == "protected-parent-group"
	if !good {
		if err == nil {
			t.Fatal("unverified capture accepted")
		}
		if !changed && !bytes.Equal(before, captureNativeSnapshot(t, paths)) {
			t.Fatal("capture mutated native evidence")
		}
		if scenario == "journal-conflict" {
			saved, _ := os.ReadFile(final)
			if string(saved) != "owner journal" {
				t.Fatal("journal overwritten")
			}
		}
		if strings.HasPrefix(scenario, "late-") && !strings.Contains(scenario, "journal") {
			if _, e := os.Lstat(final); !os.IsNotExist(e) {
				t.Fatal("unverified before-image committed", e)
			}
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, captureNativeSnapshot(t, paths)) {
		t.Fatal("read-only capture changed native files")
	}
	next, e := readMailRenewalBundle(filepath.Join(paths.runtime, target))
	if e != nil {
		t.Fatal(e)
	}
	defer next.state.close()
	var envelope mailCaptureRecord
	if e = json.Unmarshal(raw, &envelope); e != nil {
		t.Fatal(e)
	}
	var old *flatNativeBundle
	if envelope.Contract.Previous != "" {
		old, e = readMailRenewalBundle(filepath.Join(paths.runtime, envelope.Contract.Previous))
		if e != nil {
			t.Fatal(e)
		}
		defer old.state.close()
	}
	record, e := decodeMailCapture(raw, old, next)
	if e != nil || record.Contract.TimerBefore != timer {
		t.Fatalf("decode %v", e)
	}
	for _, mutate := range []func(*mailCaptureRecord){
		func(r *mailCaptureRecord) { r.Schema = "unknown" },
		func(r *mailCaptureRecord) { r.HookParent.Ino = 0 },
		func(r *mailCaptureRecord) { r.UnitParent.Mode |= 0020 },
		func(r *mailCaptureRecord) { r.Files = nil },
		func(r *mailCaptureRecord) { r.Contract.OperationID = "invalid" },
		func(r *mailCaptureRecord) { r.Contract.After[mailrenewalkit.HookName] = []byte("owner hook") },
		func(r *mailCaptureRecord) { r.Files["unreviewed"] = promotionIdentity{} },
	} {
		var invalid mailCaptureRecord
		if e = json.Unmarshal(raw, &invalid); e != nil {
			t.Fatal(e)
		}
		mutate(&invalid)
		bad, e := promotionJSON(invalid)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = decodeMailCapture(bad, old, next); e == nil {
			t.Fatal("invalid capture decoded")
		}
	}
	for _, bad := range [][]byte{append(bytes.Clone(raw), ' '), append([]byte(`{"schema":"unknown",`), raw[1:]...), append([]byte(`{"unknown":true,`), raw[1:]...), bytes.Repeat([]byte("x"), mailrenewalkit.MaxTransitionSize+1)} {
		if _, e = decodeMailCapture(bad, old, next); e == nil {
			t.Fatal("noncanonical capture decoded")
		}
	}
	if _, e = decodeMailCapture(raw, old, nil); e == nil {
		t.Fatal("missing target kit accepted")
	}
	first, e := os.Stat(final)
	if e != nil {
		t.Fatal(e)
	}
	repeat, e := captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, timer, 9, paths, nil)
	if e != nil || !bytes.Equal(raw, repeat) {
		t.Fatalf("repeat %v", e)
	}
	again, e := os.Stat(final)
	if e != nil || !os.SameFile(first, again) {
		t.Fatal("repeat replaced evidence")
	}
	// Existing authority must never be silently rebound after an owner change,
	// even when a replacement file has exactly the previously accepted bytes.
	if kind != "absent" {
		original, e := os.ReadFile(paths.hook)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.Rename(paths.hook, paths.hook+".owner"); e != nil {
			t.Fatal(e)
		}
		capturePut(t, paths.hook, original, 0755)
		if _, e = captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, timer, 9, paths, nil); e == nil {
			t.Fatal("owner inode replacement rebound to existing capture")
		}
		saved, _ := os.ReadFile(final)
		if !bytes.Equal(saved, raw) {
			t.Fatal("before-image changed")
		}
	}
}
func captureMoveJournal(t *testing.T, path string) {
	t.Helper()
	if err := os.Rename(path, path+".owner"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
}
func captureNativeSnapshot(t *testing.T, p mailCapturePaths) []byte {
	t.Helper()
	type entry struct {
		Path string
		Stat unix.Stat_t
		Data []byte
	}
	var entries []entry
	for _, root := range []string{filepath.Dir(p.hook), p.units, p.runtime} {
		err := filepath.Walk(root, func(path string, info os.FileInfo, e error) error {
			if e != nil {
				return e
			}
			var st unix.Stat_t
			if e = unix.Lstat(path, &st); e != nil {
				return e
			}
			var raw []byte
			if info.Mode().IsRegular() {
				raw, e = os.ReadFile(path)
				if e != nil {
					return e
				}
			}
			st.Atim = unix.Timespec{}
			entries = append(entries, entry{path, st, raw})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestMailCaptureResumesAfterSIGKILL(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, kind := range []string{"absent", "legacy", "independent"} {
		for _, phase := range []string{"image_durable", "image_published", "image_parent_durable"} {
			t.Run(kind+"/"+phase, func(t *testing.T) {
				root, lock := mailRenewalTestRoot(t)
				paths, target, _ := mailCaptureFixture(t, root, kind)
				if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
				before := captureNativeSnapshot(t, paths)
				child := mailCaptureChild(root, target, kind, "cut-"+phase, lock)
				out, err := child.CombinedOutput()
				if err == nil {
					t.Fatal("cut not reached", string(out))
				}
				status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatalf("wrong interruption: %v %s", err, out)
				}
				if !bytes.Equal(before, captureNativeSnapshot(t, paths)) {
					t.Fatal("cut changed native state")
				}
				if out, err = mailCaptureChild(root, target, kind, "resume", lock).CombinedOutput(); err != nil {
					t.Fatalf("resume: %v %s", err, out)
				}
				stages, _ := filepath.Glob(filepath.Join(paths.journals, ".mail-before-image-*"))
				if phase == "image_durable" && len(stages) != 1 {
					t.Fatal("incomplete evidence removed", stages)
				}
			})
		}
	}
}
