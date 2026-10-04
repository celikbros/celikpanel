//go:build linux

package mailrenewalintent

import (
	"bytes"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestImmutableBeforePublicationPreservesOwnerEvidence(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root fixture")
	}
	for _, scenario := range []string{"normal", "different-before", "owner-file", "symlink", "unsafe-parent", "unknown-precondition", "owner-race", "stage-replacement", "parent-replacement"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			if e := os.Chmod(root, 0700); e != nil {
				t.Fatal(e)
			}
			v, _ := fixture(t)
			name, _ := FileName(v.RequestID)
			path := filepath.Join(root, name)
			raw, _ := Canonical(v)
			if scenario == "owner-file" {
				if e := os.WriteFile(path, []byte("owner"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "symlink" {
				if e := os.Symlink("/outside", path); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "unsafe-parent" {
				if e := os.Chmod(root, 0777); e != nil {
					t.Fatal(e)
				}
			}
			verify := func() error {
				if scenario == "unknown-precondition" {
					return errors.New("observation unknown")
				}
				return nil
			}
			checkpoint := func(point string) {
				if point != "staged" {
					return
				}
				switch scenario {
				case "owner-race":
					if e := os.WriteFile(path, []byte("owner"), 0600); e != nil {
						t.Fatal(e)
					}
				case "stage-replacement":
					entries, e := filepath.Glob(filepath.Join(root, ".mail-renewal-before-*.json"))
					if e != nil || len(entries) != 1 {
						t.Fatal("stage missing", e)
					}
					if e = os.Rename(entries[0], filepath.Join(root, "owner-kept-stage")); e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(entries[0], raw, 0600); e != nil {
						t.Fatal(e)
					}
				case "parent-replacement":
					if e := os.Rename(root, root+"-kept"); e != nil {
						t.Fatal(e)
					}
					t.Cleanup(func() { os.RemoveAll(root + "-kept") })
					if e := os.Mkdir(root, 0700); e != nil {
						t.Fatal(e)
					}
				}
			}
			e := write(path, v, uint32(os.Getgid()), verify, checkpoint)
			if scenario != "normal" && scenario != "different-before" {
				if e == nil {
					t.Fatal("unsafe publication accepted")
				}
				if scenario == "owner-file" || scenario == "owner-race" {
					got, e := os.ReadFile(path)
					if e != nil || string(got) != "owner" {
						t.Fatal("owner evidence overwritten", e)
					}
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			first, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			if scenario == "different-before" {
				v.PreviousSelectionSHA256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
				if e = Write(path, v, uint32(os.Getgid()), verify); e == nil {
					t.Fatal("before-image rebound")
				}
			} else {
				if e = Write(path, v, uint32(os.Getgid()), verify); e != nil {
					t.Fatal(e)
				}
			}
			final, e := os.Stat(path)
			got, readErr := os.ReadFile(path)
			if e != nil || readErr != nil || !os.SameFile(first, final) || !bytes.Equal(got, raw) {
				t.Fatal("immutable proof replaced", e, readErr)
			}
		})
	}
}
func TestBeforePublicationChild(t *testing.T) {
	root := os.Getenv("CP_RENEWAL_BEFORE_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	v, _ := fixture(t)
	name, _ := FileName(v.RequestID)
	gid, e := strconv.ParseUint(os.Getenv("CP_RENEWAL_BEFORE_GID"), 10, 32)
	if e != nil {
		t.Fatal(e)
	}
	e = write(filepath.Join(root, name), v, uint32(gid), func() error { return nil }, func(point string) {
		if point == os.Getenv("CP_RENEWAL_BEFORE_CUT") {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestBeforePublicationProcessKill(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root fixture")
	}
	for _, point := range []string{"staged", "published", "parent_durable"} {
		t.Run(point, func(t *testing.T) {
			root := t.TempDir()
			if e := os.Chmod(root, 0700); e != nil {
				t.Fatal(e)
			}
			child := func(cut string) *exec.Cmd {
				cmd := exec.Command(os.Args[0], "-test.run=^TestBeforePublicationChild$", "-test.v")
				cmd.Env = append(os.Environ(), "CP_RENEWAL_BEFORE_ROOT="+root, "CP_RENEWAL_BEFORE_GID="+strconv.Itoa(os.Getgid()), "CP_RENEWAL_BEFORE_CUT="+cut)
				return cmd
			}
			cmd := child(point)
			out, e := cmd.CombinedOutput()
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if e == nil || !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("cut not reached %v %s", e, out)
			}
			v, _ := fixture(t)
			name, _ := FileName(v.RequestID)
			path := filepath.Join(root, name)
			before, _ := os.Stat(path)
			if out, e = child("none").CombinedOutput(); e != nil {
				t.Fatalf("resume %v %s", e, out)
			}
			after, e := os.Stat(path)
			if e != nil || before != nil && !os.SameFile(before, after) {
				t.Fatal("published inode replaced", e)
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			got, e := Decode(raw)
			if e != nil || got != v {
				t.Fatal("wrong durable record", e)
			}
		})
	}
}

func TestBeforeReaderRejectsForeignNameAndMetadata(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root fixture")
	}
	for _, scenario := range []string{"valid", "missing", "foreign-name", "public", "linked", "symlink", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			if e := os.Chmod(dir, 0700); e != nil {
				t.Fatal(e)
			}
			value, _ := fixture(t)
			name, _ := FileName(value.RequestID)
			path := filepath.Join(dir, name)
			if scenario != "missing" {
				if e := Write(path, value, uint32(os.Getgid()), func() error { return nil }); e != nil {
					t.Fatal(e)
				}
			}
			switch scenario {
			case "foreign-name":
				next := filepath.Join(dir, "mail-renewal-before-ffffffffffffffffffffffffffffffff.json")
				if e := os.Rename(path, next); e != nil {
					t.Fatal(e)
				}
				path = next
			case "public":
				if e := os.Chmod(path, 0644); e != nil {
					t.Fatal(e)
				}
			case "linked":
				if e := os.Link(path, filepath.Join(dir, "owner-copy")); e != nil {
					t.Fatal(e)
				}
			case "symlink":
				next := filepath.Join(dir, "owner-copy")
				if e := os.Rename(path, next); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(next, path); e != nil {
					t.Fatal(e)
				}
			case "unknown":
				if e := os.WriteFile(path, []byte("{}\n"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			got, found, e := Read(path, uint32(os.Getgid()))
			if scenario == "missing" {
				if e != nil || found {
					t.Fatal("absence confused with invalid", e)
				}
				return
			}
			if scenario == "valid" {
				if e != nil || !found || got != value {
					t.Fatal("canonical before-image rejected", e)
				}
				return
			}
			if e == nil {
				t.Fatal("unverified before-image accepted")
			}
		})
	}
}
