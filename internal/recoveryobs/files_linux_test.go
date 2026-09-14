//go:build linux

package recoveryobs

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func observationFixture(t *testing.T) (string, string, uint32, uint32) {
	t.Helper()
	anchor := t.TempDir()
	if err := os.Chmod(anchor, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(anchor, "observations"), anchor, uint32(os.Geteuid()), uint32(os.Getegid())
}

func TestAtomicPublicationAndReader(t *testing.T) {
	root, anchor, uid, gid := observationFixture(t)
	r := testRecord()
	if got := readAt(root, r.RequestID, uid, gid, anchor); got.Observation != "unavailable" || got.TerminalProof != "none" {
		t.Fatalf("missing record: %#v", got)
	}
	if err := publishAt(root, r, uid, gid, anchor); err != nil {
		t.Fatal(err)
	}
	if got := readAt(root, r.RequestID, uid, gid, anchor); got != r.Status() {
		t.Fatalf("published read: %#v", got)
	}
	if got := readAt(root, strings.Repeat("c", 32), uid, gid, anchor); got.Observation != "unavailable" {
		t.Fatal("another request inferred from existing record")
	}
	r.Phase, r.Reason, r.TerminalProof = "succeeded", "update_verified", "update_verified"
	if err := publishAt(root, r, uid, gid, anchor); err != nil {
		t.Fatal(err)
	}
	late := testRecord()
	late.Phase, late.Reason = "failed", "update_failed"
	if err := publishAt(root, late, uid, gid, anchor); err != nil {
		t.Fatal(err)
	}
	if got := readAt(root, r.RequestID, uid, gid, anchor); got != r.Status() {
		t.Fatalf("terminal result replaced: %#v", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("staging artifacts remained: %v", entries)
	}
}

func TestUnsafeObservationNeverBecomesKnownOrOverwritten(t *testing.T) {
	for _, defect := range []string{"record-symlink", "root-symlink", "hardlink", "mode", "directory-mode", "oversize", "malformed", "owner", "fifo"} {
		t.Run(defect, func(t *testing.T) {
			root, anchor, uid, gid := observationFixture(t)
			r := testRecord()
			if err := publishAt(root, r, uid, gid, anchor); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(root, r.RequestID+".status")
			switch defect {
			case "record-symlink":
				if err := os.Rename(p, p+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p+".original", p); err != nil {
					t.Fatal(err)
				}
			case "root-symlink":
				if err := os.Rename(root, root+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(root+".original", root); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(p, p+".linked"); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(p, 0o660); err != nil {
					t.Fatal(err)
				}
			case "directory-mode":
				if err := os.Chmod(root, 0o770); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				if err := os.WriteFile(p, []byte(strings.Repeat("x", MaxRecordSize+1)), 0o640); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(p, []byte("found=false\n"), 0o640); err != nil {
					t.Fatal(err)
				}
			case "owner":
				if os.Geteuid() != 0 {
					t.Skip("owner mutation requires root fixture")
				}
				if err := os.Chown(p, 12345, int(gid)); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(p, 0o640); err != nil {
					t.Fatal(err)
				}
			}
			if got := readAt(root, r.RequestID, uid, gid, anchor); got.Observation != "unavailable" || got.TerminalProof != "none" {
				t.Fatalf("unsafe %s accepted: %#v", defect, got)
			}
			if err := publishAt(root, r, uid, gid, anchor); err == nil {
				t.Fatalf("unsafe %s overwritten", defect)
			}
		})
	}
}

func TestBusyObserverLeavesPreviousProofUnchanged(t *testing.T) {
	root, anchor, uid, gid := observationFixture(t)
	r := testRecord()
	if err := publishAt(root, r, uid, gid, anchor); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(root, ".publish.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	next := r
	next.Phase, next.Reason, next.TerminalProof = "succeeded", "update_verified", "update_verified"
	if err := publishAt(root, next, uid, gid, anchor); err == nil {
		t.Fatal("busy observer acquired another holder's lock")
	}
	if got := readAt(root, r.RequestID, uid, gid, anchor); got != r.Status() {
		t.Fatalf("busy publication changed last verified observation: %#v", got)
	}
}
