//go:build linux

package hostmutationlock

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func ownerRecoveryFixture(t *testing.T) (string, Owner) {
	t.Helper()
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("native owner recovery fixture requires root")
	}
	return filepath.Join(t.TempDir(), "celikpanel", "service-mutation.lock"), Owner{UID: 0, GID: 0}
}

func TestOwnerRecoveryCreatesMissingVolatileDirectoryAndHeldLock(t *testing.T) {
	path, owner := ownerRecoveryFixture(t)
	held, err := acquireOrCreateOwnerRecoveryAt(path, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := VerifyInherited(path, int(held.Fd()), owner); err != nil {
		t.Fatal(err)
	}
	if err := ProbeIdle(path, owner); !errors.Is(err, ErrBusy) {
		t.Fatalf("published lock not held: %v", err)
	}
	if _, err := acquireOrCreateOwnerRecoveryAt(path, owner); !errors.Is(err, ErrBusy) {
		t.Fatalf("second owner recovered the same held lock: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != 0 {
		t.Fatalf("published lock metadata: %v", info)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := acquireOrCreateOwnerRecoveryAt(path, owner)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) {
		t.Fatalf("existing lock replaced: %v", err)
	}
}

func TestOwnerRecoveryPreservesInvalidExistingAndCrashStage(t *testing.T) {
	path, owner := ownerRecoveryFixture(t)
	parent := filepath.Dir(path)
	if err := os.Mkdir(parent, 0o750); err != nil {
		t.Fatal(err)
	}
	// An abandoned private staging directory is never treated as canonical.
	orphan := filepath.Join(filepath.Dir(parent), ".celikpanel-recovery-abandoned")
	if err := os.Mkdir(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := acquireOrCreateOwnerRecoveryAt(path, owner)
	if err != nil {
		t.Fatal(err)
	}
	held.Close()
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("owner stage removed: %v", err)
	}
	if err := os.WriteFile(path, []byte("owner evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if f, err := acquireOrCreateOwnerRecoveryAt(path, owner); err == nil {
		f.Close()
		t.Fatal("invalid existing lock accepted")
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() {
		t.Fatalf("invalid owner lock changed: %v", err)
	}
}

func TestOwnerRecoveryRefusesSymlinkAndUnsafeParent(t *testing.T) {
	path, owner := ownerRecoveryFixture(t)
	parent := filepath.Dir(path)
	if err := os.Mkdir(parent, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if f, err := acquireOrCreateOwnerRecoveryAt(path, owner); err == nil {
		f.Close()
		t.Fatal("symlink lock accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o770); err != nil {
		t.Fatal(err)
	}
	if f, err := acquireOrCreateOwnerRecoveryAt(path, owner); err == nil {
		f.Close()
		t.Fatal("writable runtime directory accepted")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe parent still created lock: %v", err)
	}
}

func TestOwnerRecoveryConcurrentCreationKeepsOneCanonicalInode(t *testing.T) {
	path, owner := ownerRecoveryFixture(t)
	type result struct {
		file *os.File
		err  error
	}
	results := make([]result, 8)
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range results {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			<-start
			results[i].file, results[i].err = acquireOrCreateOwnerRecoveryAt(path, owner)
		}(i)
	}
	close(start)
	group.Wait()
	winners := 0
	for _, result := range results {
		if result.file != nil {
			winners++
			if err := VerifyInherited(path, int(result.file.Fd()), owner); err != nil {
				t.Error(err)
			}
			defer result.file.Close()
		} else if result.err == nil || (!errors.Is(result.err, ErrBusy) && !errors.Is(result.err, unix.EEXIST)) {
			t.Errorf("unexpected contender error: %v", result.err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent canonical lease winners=%d", winners)
	}
}

func TestOwnerRecoveryAdmissionUsesOnlyTrustedLocalGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("local group fixture requires root")
	}
	path := filepath.Join(t.TempDir(), "group")
	if err := os.WriteFile(path, []byte("root:x:0:\ncelikpanel:x:42:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyOwnerRecoveryAdmissionAt(Owner{UID: 0, GID: 42}, path); err != nil {
		t.Fatal(err)
	}
	if err := verifyOwnerRecoveryAdmissionAt(Owner{UID: 0, GID: 43}, path); err == nil {
		t.Fatal("wrong local group admitted")
	}
	if err := os.WriteFile(path, []byte("celikpanel:x:42:\ncelikpanel:x:43:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyOwnerRecoveryAdmissionAt(Owner{UID: 0, GID: 42}, path); err == nil {
		t.Fatal("ambiguous local group admitted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/group", path); err != nil {
		t.Fatal(err)
	}
	if err := verifyOwnerRecoveryAdmissionAt(Owner{UID: 0, GID: 42}, path); err == nil {
		t.Fatal("symlink local group admitted")
	}
}

func TestOwnerRecoveryRefusesUnsafeCanonicalEvidence(t *testing.T) {
	for _, kind := range []string{"fifo", "hardlink", "mode", "owner"} {
		t.Run(kind, func(t *testing.T) {
			path, owner := ownerRecoveryFixture(t)
			if err := os.Mkdir(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".owner"); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(path, 0o640); err != nil {
					t.Fatal(err)
				}
			case "owner":
				if err := os.Chown(path, 1, 0); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if f, err := acquireOrCreateOwnerRecoveryAt(path, owner); err == nil {
				f.Close()
				t.Fatal("unsafe canonical lock admitted")
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
				t.Fatalf("unsafe canonical lock changed: %v", err)
			}
		})
	}
}

func TestOwnerRecoveryInterruptedBeforeAndAfterPublication(t *testing.T) {
	for _, phase := range []string{"private", "published"} {
		t.Run(phase, func(t *testing.T) {
			path, owner := ownerRecoveryFixture(t)
			cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerRecoveryInterruptionHelper$")
			cmd.Env = append(os.Environ(), "CP_OWNER_RECOVERY_PHASE="+phase, "CP_OWNER_RECOVERY_PATH="+path)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			ready := make(chan string, 1)
			go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
			select {
			case line := <-ready:
				if strings.TrimSpace(line) != "READY" {
					t.Fatalf("helper not ready: %q", line)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("helper did not reach interruption checkpoint")
			}
			var before os.FileInfo
			if phase == "private" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("private stage published canonical lock: %v", err)
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(path)), ".celikpanel-recovery-interrupted")); err != nil {
					t.Fatalf("private stage missing: %v", err)
				}
			} else {
				before, err = os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := ProbeIdle(path, owner); !errors.Is(err, ErrBusy) {
					t.Fatalf("published lease not held by helper: %v", err)
				}
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			held, err := acquireOrCreateOwnerRecoveryAt(path, owner)
			if err != nil {
				t.Fatalf("interrupted retry: %v", err)
			}
			defer held.Close()
			if err := VerifyInherited(path, int(held.Fd()), owner); err != nil {
				t.Fatal(err)
			}
			if before != nil {
				after, err := os.Lstat(path)
				if err != nil || !os.SameFile(before, after) {
					t.Fatalf("published inode replaced after kill: %v", err)
				}
			}
		})
	}
}

func TestOwnerRecoveryInterruptionHelper(t *testing.T) {
	phase := os.Getenv("CP_OWNER_RECOVERY_PHASE")
	path := os.Getenv("CP_OWNER_RECOVERY_PATH")
	if phase == "" || path == "" {
		t.Skip("subprocess only")
	}
	switch phase {
	case "private":
		run := filepath.Dir(filepath.Dir(path))
		private := filepath.Join(run, ".celikpanel-recovery-interrupted")
		if err := os.Mkdir(private, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(private, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(private, 0o750); err != nil {
			t.Fatal(err)
		}
	case "published":
		held, err := acquireOrCreateOwnerRecoveryAt(path, Owner{UID: 0, GID: 0})
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close()
	default:
		t.Fatalf("unknown helper phase %q", phase)
	}
	_, _ = os.Stdout.WriteString("READY\n")
	time.Sleep(30 * time.Second)
}
