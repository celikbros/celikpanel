//go:build linux

package mailrenewalruntime

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestEnrollmentRuntimePublishesBothLocksAndPreservesBusyIdentity(t *testing.T) {
	root, fd, uid, gid := fixture(t)
	if err := ensureRuntimeAt(fd, uid, gid, nil, true); err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(root, directory)
	for _, name := range enrollmentLockNames {
		path := filepath.Join(runtime, name)
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		if err = ensureRuntimeAt(fd, uid, gid, nil, true); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("lock replaced", err)
		}
		second, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = unix.Flock(int(second.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		second.Close()
		lock.Close()
		if !errors.Is(err, unix.EWOULDBLOCK) {
			t.Fatal("existing exclusion lost", err)
		}
	}
}
func TestEnrollmentRuntimeNeverRepairsExistingLocks(t *testing.T) {
	for _, kind := range []string{"missing", "content", "mode", "symlink", "hardlink", "fifo", "group"} {
		t.Run(kind, func(t *testing.T) {
			root, fd, uid, gid := fixture(t)
			if err := ensureRuntimeAt(fd, uid, gid, nil, true); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, directory, enrollmentLockNames[0])
			switch kind {
			case "missing":
				os.Remove(path)
			case "content":
				os.WriteFile(path, []byte("owner"), 0600)
			case "mode":
				os.Chmod(path, 0640)
			case "symlink":
				os.Remove(path)
				os.Symlink("owner", path)
			case "hardlink":
				os.Link(path, filepath.Join(root, "owner-link"))
			case "fifo":
				os.Remove(path)
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "group":
				if os.Geteuid() != 0 {
					t.Skip("root fixture")
				}
				os.Chown(path, int(uid), int(gid)+1)
			}
			var before, after unix.Stat_t
			initial := unix.Lstat(path, &before)
			if err := ensureRuntimeAt(fd, uid, gid, nil, true); err == nil {
				t.Fatal("owner state repaired")
			}
			final := unix.Lstat(path, &after)
			if initial != nil {
				if final == nil {
					t.Fatal("absent lock created in existing runtime")
				}
			} else if final != nil || !identity(before, after) || before.Size != after.Size || before.Nlink != after.Nlink {
				t.Fatal("owner state changed")
			}
		})
	}
}
func TestEnrollmentRuntimeRefusesChangedPublication(t *testing.T) {
	for _, kind := range []string{"proof-failed", "lock-edited", "extra-file", "owner-winner", "owner-empty-runtime"} {
		t.Run(kind, func(t *testing.T) {
			root, fd, uid, gid := fixture(t)
			err := ensureRuntimeAt(fd, uid, gid, func(stage string) error {
				path := filepath.Join(root, stage)
				switch kind {
				case "proof-failed":
					return errors.New("recorded authority changed")
				case "lock-edited":
					return os.WriteFile(filepath.Join(path, enrollmentLockNames[0]), []byte("owner"), 0600)
				case "extra-file":
					return os.WriteFile(filepath.Join(path, "owner"), []byte("keep"), 0600)
				case "owner-empty-runtime":
					return os.Mkdir(filepath.Join(root, directory), 0750)
				case "owner-winner":
					return os.Mkdir(filepath.Join(root, directory), 0700)
				}
				return nil
			}, true)
			if err == nil {
				t.Fatal("uncertain runtime published")
			}
			if kind != "owner-winner" && kind != "owner-empty-runtime" {
				if _, err = os.Stat(filepath.Join(root, directory)); !os.IsNotExist(err) {
					t.Fatal("partial runtime selected", err)
				}
			}
		})
	}
}
