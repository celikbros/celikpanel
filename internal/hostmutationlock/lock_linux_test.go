//go:build linux

package hostmutationlock

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) (string, Owner) {
	t.Helper()
	return filepath.Join(t.TempDir(), "service-mutation.lock"), Owner{uint32(os.Geteuid()), uint32(os.Getegid())}
}
func writeLock(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestMissingObservationDoesNotInitialize(t *testing.T) {
	path, owner := fixture(t)
	if err := ProbeIdle(path, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("probe created lock: %v", err)
	}
	if file, err := AcquireExisting(path, owner); !errors.Is(err, os.ErrNotExist) || file != nil {
		t.Fatalf("missing acquisition: %v %v", file, err)
	}
	if err := ProbeIdle(filepath.Join(filepath.Dir(path), "missing", "lock"), owner); err == nil {
		t.Fatal("missing parent treated as ready")
	}
}
func TestExclusionInheritedProofAndReuse(t *testing.T) {
	path, owner := fixture(t)
	writeLock(t, path)
	held, err := AcquireExisting(path, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err = ProbeIdle(path, owner); !errors.Is(err, ErrBusy) {
		t.Fatalf("held lock not busy: %v", err)
	}
	if err = VerifyInherited(path, int(held.Fd()), owner); err != nil {
		t.Fatal(err)
	}
	if err = ProbeIdle(path, owner); !errors.Is(err, ErrBusy) {
		t.Fatalf("proof released original lease: %v", err)
	}
	other, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err = VerifyInherited(path, int(other.Fd()), owner); err == nil {
		t.Fatal("unlocked open description accepted because another held the file")
	}
	if err = held.Close(); err != nil {
		t.Fatal(err)
	}
	if err = VerifyInherited(path, int(other.Fd()), owner); err == nil {
		t.Fatal("unlocked descriptor acquired as proof")
	}
	if err = ProbeIdle(path, owner); err != nil {
		t.Fatal(err)
	}
	for _, fd := range []int{-1, 0, 1, 2, 999999} {
		if VerifyInherited(path, fd, owner) == nil {
			t.Fatalf("invalid descriptor %d accepted", fd)
		}
	}
}
func TestUnsafeEvidencePreserved(t *testing.T) {
	for _, kind := range []string{"nonempty", "mode640", "specialmode", "symlink", "hardlink", "fifo", "directory", "parentmode", "parentlink", "owner", "group"} {
		t.Run(kind, func(t *testing.T) {
			path, owner := fixture(t)
			writeLock(t, path)
			switch kind {
			case "nonempty":
				if err := os.WriteFile(path, []byte("owner data"), 0600); err != nil {
					t.Fatal(err)
				}
			case "mode640":
				if err := os.Chmod(path, 0640); err != nil {
					t.Fatal(err)
				}
			case "specialmode":
				if err := os.Chmod(path, os.ModeSetuid|0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".owner"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".owner", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".owner"); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "parentmode":
				if err := os.Chmod(filepath.Dir(path), 0770); err != nil {
					t.Fatal(err)
				}
			case "parentlink":
				link := filepath.Dir(path) + "-link"
				if err := os.Symlink(filepath.Dir(path), link); err != nil {
					t.Fatal(err)
				}
				defer os.Remove(link)
				path = filepath.Join(link, filepath.Base(path))
			case "owner":
				owner.UID++
			case "group":
				owner.GID++
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			// Bound the FIFO regression even if an opener accidentally loses O_NONBLOCK.
			if kind == "fifo" {
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUnsafeFIFOHelper$")
				cmd.Env = append(os.Environ(), "CP_TEST_FIFO="+path)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("FIFO refusal blocked/failed: %v %s", err, output)
				}
			} else {
				if f, err := AcquireExisting(path, owner); err == nil {
					f.Close()
					t.Fatal("unsafe lease admitted")
				}
				if err := ProbeIdle(path, owner); err == nil {
					t.Fatal("unsafe evidence treated idle")
				}
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
				t.Fatalf("evidence changed: %v", err)
			}
		})
	}
}
func TestUnsafeFIFOHelper(t *testing.T) {
	path := os.Getenv("CP_TEST_FIFO")
	if path == "" {
		t.Skip("subprocess only")
	}
	owner := Owner{uint32(os.Geteuid()), uint32(os.Getegid())}
	if f, err := AcquireExisting(path, owner); err == nil {
		f.Close()
		t.Fatal("FIFO admitted")
	}
	if ProbeIdle(path, owner) == nil {
		t.Fatal("FIFO treated idle")
	}
}
func TestChangesDuringAcquisitionRefuseWithoutRepair(t *testing.T) {
	for _, kind := range []string{"file", "parent", "mode", "contents", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			path, owner := fixture(t)
			writeLock(t, path)
			change := func() {
				switch kind {
				case "file":
					if err := os.Rename(path, path+".owner"); err != nil {
						t.Fatal(err)
					}
					writeLock(t, path)
				case "parent":
					dir := filepath.Dir(path)
					if err := os.Rename(dir, dir+".owner"); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.RemoveAll(dir + ".owner") })
					if err := os.Mkdir(dir, 0700); err != nil {
						t.Fatal(err)
					}
					writeLock(t, path)
				case "mode":
					if err := os.Chmod(path, 0640); err != nil {
						t.Fatal(err)
					}
				case "contents":
					if err := os.WriteFile(path, []byte("owner"), 0600); err != nil {
						t.Fatal(err)
					}
				case "hardlink":
					if err := os.Link(path, path+".owner"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if f, err := acquire(path, owner, false, change); err == nil {
				f.Close()
				t.Fatal("changed lock admitted")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("owner evidence removed: %v", err)
			}
		})
	}
}
func TestNativeProcessExclusionSurvivesProofAndReleasesOnDeath(t *testing.T) {
	path, owner := fixture(t)
	writeLock(t, path)
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeProcessHelper$")
	cmd.Env = append(os.Environ(), "CP_TEST_PROCESS_LOCK="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if strings.TrimSpace(line) != "READY" {
			t.Fatalf("child not ready: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child lock timeout")
	}
	if err = ProbeIdle(path, owner); !errors.Is(err, ErrBusy) {
		t.Fatalf("independent process admitted: %v", err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if err = ProbeIdle(path, owner); err != nil {
		t.Fatalf("kernel did not release dead process lock: %v", err)
	}
}
func TestNativeProcessHelper(t *testing.T) {
	path := os.Getenv("CP_TEST_PROCESS_LOCK")
	if path == "" {
		t.Skip("subprocess only")
	}
	owner := Owner{uint32(os.Geteuid()), uint32(os.Getegid())}
	file, err := AcquireExisting(path, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = VerifyInherited(path, int(file.Fd()), owner); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("READY\n")
	time.Sleep(30 * time.Second)
}
