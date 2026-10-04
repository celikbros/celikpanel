//go:build linux

package mailrenewalruntime

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) (string, int, uint32, uint32) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return root, fd, uint32(os.Geteuid()), uint32(os.Getgid())
}

func TestRuntimePublishesOnceAndPreservesExistingContents(t *testing.T) {
	root, fd, uid, gid := fixture(t)
	if err := ensureAt(fd, uid, gid, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, directory)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0750 {
		t.Fatal("runtime mode differs")
	}
	if err = os.WriteFile(filepath.Join(path, "owner-socket-placeholder"), []byte("owner data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = ensureAt(fd, uid, gid, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("existing runtime replaced")
	}
	data, err := os.ReadFile(filepath.Join(path, "owner-socket-placeholder"))
	if err != nil || string(data) != "owner data" {
		t.Fatal("existing runtime content changed")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("unexpected stages remain")
	}
}

func TestRuntimeRejectsOwnerPathWithoutRepair(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "wrong-mode", "wrong-group"} {
		t.Run(kind, func(t *testing.T) {
			root, fd, uid, gid := fixture(t)
			path := filepath.Join(root, directory)
			switch kind {
			case "file":
				if err := os.WriteFile(path, []byte("owner"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("owner-target", path); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "wrong-group" {
					if os.Geteuid() != 0 {
						t.Skip("group fixture requires root")
					}
					if err := os.Chmod(path, 0750); err != nil {
						t.Fatal(err)
					}
					if err := os.Chown(path, int(uid), int(gid)+1); err != nil {
						t.Fatal(err)
					}
				}
			}
			var before, after unix.Stat_t
			if err := unix.Lstat(path, &before); err != nil {
				t.Fatal(err)
			}
			if err := ensureAt(fd, uid, gid, nil); err == nil {
				t.Fatal("owner path normalized")
			}
			if err := unix.Lstat(path, &after); err != nil || !identity(before, after) {
				t.Fatal("owner metadata changed")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 1 {
				t.Fatal("refusal left new stages")
			}
		})
	}
}

func TestRuntimePublicationRaceAndFault(t *testing.T) {
	for _, kind := range []string{"concurrent-valid", "concurrent-owner", "owner-stage-file", "owner-stage-mode", "owned-stage-error"} {
		t.Run(kind, func(t *testing.T) {
			root, fd, uid, gid := fixture(t)
			var stagePath string
			err := ensureAt(fd, uid, gid, func(stage string) error {
				stagePath = filepath.Join(root, stage)
				switch kind {
				case "concurrent-valid":
					return ensureAt(fd, uid, gid, nil)
				case "concurrent-owner":
					return os.WriteFile(filepath.Join(root, directory), []byte("owner"), 0600)
				case "owner-stage-file":
					return os.WriteFile(filepath.Join(stagePath, "owner-file"), []byte("preserve"), 0600)
				case "owner-stage-mode":
					return os.Chmod(stagePath, 0755)
				case "owned-stage-error":
					return errors.New("controlled before-publication failure")
				}
				return nil
			})
			if kind == "concurrent-valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("uncertain runtime published")
			}
			switch kind {
			case "concurrent-owner":
				data, e := os.ReadFile(filepath.Join(root, directory))
				if e != nil || string(data) != "owner" {
					t.Fatal("winner overwritten")
				}
			case "owner-stage-file":
				data, e := os.ReadFile(filepath.Join(stagePath, "owner-file"))
				if e != nil || string(data) != "preserve" {
					t.Fatal("owner stage erased")
				}
			case "owner-stage-mode":
				info, e := os.Stat(stagePath)
				if e != nil || info.Mode().Perm() != 0755 {
					t.Fatal("owner stage mode repaired or erased")
				}
			default:
				if _, e := os.Stat(stagePath); !os.IsNotExist(e) {
					t.Fatal("owned empty stage retained")
				}
			}
		})
	}
}
