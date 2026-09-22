//go:build linux

package certpublishlock

import (
	"bufio"
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) (string, int) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("native root ownership required")
	}
	dir := t.TempDir()
	fd, err := unix.Open(dir, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return dir, fd
}
func TestExclusionCancellationAndReuse(t *testing.T) {
	dir, parent := fixture(t)
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withAt(context.Background(), parent, func() error { close(held); <-release; return nil })
	}()
	<-held
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	called := false
	err := withAt(ctx, parent, func() error { called = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatalf("competing action admitted: %v %v", called, err)
	}
	st, err := os.Stat(filepath.Join(dir, Name))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("lock metadata: %v %v", st, err)
	}
}
func TestActionErrorAndCancelledContext(t *testing.T) {
	_, parent := fixture(t)
	want := errors.New("action failed")
	if err := withAt(context.Background(), parent, func() error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	calls := 0
	if err := withAt(context.Background(), parent, func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("reuse: %v %d", err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := withAt(ctx, parent, func() error { t.Fatal("cancelled action executed"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestUnsafeLockNeverNormalizedOrAdmitted(t *testing.T) {
	for _, kind := range []string{"mode400", "mode640", "owner", "symlink", "hardlink", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir, parent := fixture(t)
			name := filepath.Join(dir, Name)
			switch kind {
			case "symlink":
				if err := os.Symlink("elsewhere", name); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(name, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(name, []byte{}, 0600); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "mode400":
					if err := os.Chmod(name, 0400); err != nil {
						t.Fatal(err)
					}
				case "mode640":
					if err := os.Chmod(name, 0640); err != nil {
						t.Fatal(err)
					}
				case "owner":
					if err := os.Chown(name, 65534, 65534); err != nil {
						t.Fatal(err)
					}
				case "hardlink":
					if err := os.Link(name, filepath.Join(dir, "owner-link")); err != nil {
						t.Fatal(err)
					}
				}
			}
			var before, after unix.Stat_t
			if err := unix.Lstat(name, &before); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := withAt(ctx, parent, func() error { t.Fatal("unsafe lock admitted"); return nil }); err == nil {
				t.Fatal("accepted")
			}
			if err := unix.Lstat(name, &after); err != nil {
				t.Fatal(err)
			}
			if !sameObject(before, after) || before.Ctim != after.Ctim {
				t.Fatal("owner evidence normalized")
			}
		})
	}
}
func TestOpenedLockReplacementRefused(t *testing.T) {
	dir, parent := fixture(t)
	name := filepath.Join(dir, Name)
	fd, err := unix.Open(name, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := os.Rename(name, filepath.Join(dir, "owner-retained-lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("owner replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := withDescriptor(context.Background(), parent, fd, func() error { t.Fatal("stale inode authorized publication"); return nil }); err == nil {
		t.Fatal("replacement accepted")
	}
	raw, err := os.ReadFile(name)
	if err != nil || string(raw) != "owner replacement" {
		t.Fatal("owner replacement changed")
	}
}
func TestHistoricalPrivateGroupAccepted(t *testing.T) {
	dir, parent := fixture(t)
	name := filepath.Join(dir, Name)
	if err := os.WriteFile(name, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(name, 0, 65534); err != nil {
		t.Fatal(err)
	}
	if err := withAt(context.Background(), parent, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	if err := unix.Lstat(name, &st); err != nil {
		t.Fatal(err)
	}
	if st.Gid != 65534 {
		t.Fatal("historical group normalized")
	}
}

// The child deliberately uses the historical plain flock protocol, independent
// of this package. Killing that owner releases exclusion without deleting state.
func TestHistoricalProcessExclusionAndDeath(t *testing.T) {
	dir, parent := fixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestHistoricalLockChild$")
	child.Env = append(os.Environ(), "CP_CERT_LOCK_CHILD="+dir)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			child.Process.Kill()
			child.Wait()
		}
	}()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("child not ready: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child readiness timeout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := withAt(ctx, parent, func() error { t.Fatal("historical process exclusion bypassed"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	waited = true
	if err := withAt(context.Background(), parent, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
func TestHistoricalLockChild(t *testing.T) {
	dir := os.Getenv("CP_CERT_LOCK_CHILD")
	if dir == "" {
		t.Skip("child helper")
	}
	fd, err := unix.Open(filepath.Join(dir, Name), unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("locked\n")
	time.Sleep(10 * time.Second)
}
