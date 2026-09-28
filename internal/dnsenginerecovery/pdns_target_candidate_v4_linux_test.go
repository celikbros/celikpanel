//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writePDNSTargetFixture(t *testing.T, name string) {
	t.Helper()
	if err := os.Chmod(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	data := append([]byte("SQLite format 3\x00"), bytes.Repeat([]byte{0x42}, 4080)...)
	if err := os.WriteFile(name, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestPDNSTargetCandidateProofFollowsOnlySameRenamedInode(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, ".celikpanel-switch-test.sqlite3")
	live := filepath.Join(dir, "pdns.sqlite3")
	writePDNSTargetFixture(t, candidate)
	proof, err := CapturePDNSTargetCandidateV4(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !proof.NoSidecars || proof.Path != candidate || proof.Size != 4096 || proof.Device == 0 || proof.Inode == 0 {
		t.Fatalf("incomplete candidate proof: %+v", proof)
	}
	if err := VerifyPDNSTargetLiveV4(proof, live); err == nil {
		t.Fatal("missing live database accepted")
	}
	if err := os.Rename(candidate, live); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPDNSTargetLiveV4(proof, live); err != nil {
		t.Fatalf("exact rename refused: %v", err)
	}
	if err := os.WriteFile(candidate, []byte("foreign"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPDNSTargetLiveV4(proof, live); err == nil {
		t.Fatal("candidate reappearance accepted")
	}
}

func TestPDNSTargetCandidateProofRejectsSymlinksAndHardlinks(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "candidate.sqlite3")
	writePDNSTargetFixture(t, candidate)
	link := filepath.Join(dir, "link.sqlite3")
	if err := os.Symlink(candidate, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CapturePDNSTargetCandidateV4(link); err == nil {
		t.Fatal("symlink candidate accepted")
	}
	parent := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(dir, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := CapturePDNSTargetCandidateV4(filepath.Join(parent, "candidate.sqlite3")); err == nil {
		t.Fatal("symlink parent accepted")
	}
	second := filepath.Join(dir, "second")
	if err := os.Link(candidate, second); err != nil {
		t.Fatal(err)
	}
	if _, err := CapturePDNSTargetCandidateV4(candidate); err == nil {
		t.Fatal("hardlinked candidate accepted")
	}
}

func TestPDNSTargetCandidateProofRejectsSidecarsAndReplacement(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm", "-journal", "-foreign", ".backup"} {
		t.Run(suffix, func(t *testing.T) {
			dir := t.TempDir()
			candidate := filepath.Join(dir, "candidate.sqlite3")
			writePDNSTargetFixture(t, candidate)
			if err := os.WriteFile(candidate+suffix, []byte("owner"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := CapturePDNSTargetCandidateV4(candidate); err == nil {
				t.Fatal("candidate with sidecar accepted")
			}
		})
	}
	dir := t.TempDir()
	candidate := filepath.Join(dir, "candidate.sqlite3")
	writePDNSTargetFixture(t, candidate)
	if _, err := capturePDNSTargetCandidateV4(candidate, func() {
		other := filepath.Join(dir, "replacement")
		writePDNSTargetFixture(t, other)
		if err := os.Rename(other, candidate); err != nil {
			t.Fatal(err)
		}
	}); err == nil {
		t.Fatal("path replacement during pinned read accepted")
	}
}

func TestPDNSTargetLiveProofRejectsOwnerEditsAndSidecars(t *testing.T) {
	for _, edit := range []string{"bytes", "mode", "sidecar", "replacement"} {
		t.Run(edit, func(t *testing.T) {
			dir := t.TempDir()
			candidate := filepath.Join(dir, "candidate.sqlite3")
			live := filepath.Join(dir, "pdns.sqlite3")
			writePDNSTargetFixture(t, candidate)
			proof, err := CapturePDNSTargetCandidateV4(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(candidate, live); err != nil {
				t.Fatal(err)
			}
			switch edit {
			case "bytes":
				file, err := os.OpenFile(live, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.WriteAt([]byte("edit"), 100)
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(live, 0o600); err != nil {
					t.Fatal(err)
				}
			case "sidecar":
				if err := os.WriteFile(live+"-wal", []byte("owner"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				other := filepath.Join(dir, "replacement")
				writePDNSTargetFixture(t, other)
				if err := os.Rename(other, live); err != nil {
					t.Fatal(err)
				}
			}
			if err := VerifyPDNSTargetLiveV4(proof, live); err == nil {
				t.Fatal("changed live database accepted")
			}
		})
	}
}

func TestPDNSTargetProofProtectedCrossDirectoryRename(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "agent-private")
	liveDir := filepath.Join(root, "powerdns")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(private, ".celikpanel-switch-test.sqlite3")
	live := filepath.Join(liveDir, "pdns.sqlite3")
	writePDNSTargetFixture(t, candidate)
	proof, err := CapturePDNSTargetCandidateV4(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(candidate, live); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPDNSTargetLiveV4(proof, live); err != nil {
		t.Fatalf("exact protected cross-directory rename refused: %v", err)
	}
}

func TestPDNSTargetProofRejectsUnsafeDirectory(t *testing.T) {
	for _, kind := range []string{"candidate-world-readable", "candidate-symlink", "live-group-writable", "live-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			private := filepath.Join(root, "agent-private")
			liveDir := filepath.Join(root, "powerdns")
			if err := os.Mkdir(private, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(liveDir, 0o755); err != nil {
				t.Fatal(err)
			}
			candidate := filepath.Join(private, ".celikpanel-switch-test.sqlite3")
			live := filepath.Join(liveDir, "pdns.sqlite3")
			writePDNSTargetFixture(t, candidate)
			switch kind {
			case "candidate-world-readable":
				if err := os.Chmod(private, 0o755); err != nil {
					t.Fatal(err)
				}
				if _, err := CapturePDNSTargetCandidateV4(candidate); err == nil {
					t.Fatal("non-private candidate directory accepted")
				}
				return
			case "candidate-symlink":
				alias := filepath.Join(root, "private-alias")
				if err := os.Symlink(private, alias); err != nil {
					t.Fatal(err)
				}
				if _, err := CapturePDNSTargetCandidateV4(filepath.Join(alias, filepath.Base(candidate))); err == nil {
					t.Fatal("candidate symlink directory accepted")
				}
				return
			}
			proof, err := CapturePDNSTargetCandidateV4(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(candidate, live); err != nil {
				t.Fatal(err)
			}
			if kind == "live-group-writable" {
				if err := os.Chmod(liveDir, 0o775); err != nil {
					t.Fatal(err)
				}
			} else {
				alias := filepath.Join(root, "live-alias")
				if err := os.Symlink(liveDir, alias); err != nil {
					t.Fatal(err)
				}
				live = filepath.Join(alias, "pdns.sqlite3")
			}
			if err := VerifyPDNSTargetLiveV4(proof, live); err == nil {
				t.Fatal("unsafe live directory accepted")
			}
		})
	}
}

func TestPDNSTargetProofRejectsSwappedPrivateDirectory(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "agent-private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(private, ".celikpanel-switch-test.sqlite3")
	writePDNSTargetFixture(t, candidate)
	if _, err := capturePDNSTargetCandidateV4(candidate, func() {
		old := filepath.Join(root, "old-private")
		if err := os.Rename(private, old); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(private, 0o700); err != nil {
			t.Fatal(err)
		}
		writePDNSTargetFixture(t, candidate)
	}); err == nil {
		t.Fatal("swapped private parent accepted")
	}
}

func TestRemoveExactStagedPDNSTargetFileV4AndReplay(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "agent-private")
	liveDir := filepath.Join(root, "powerdns")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(private, ".celikpanel-switch-test.sqlite3")
	live := filepath.Join(liveDir, "pdns.sqlite3")
	writePDNSTargetFixture(t, candidate)
	proof, err := CapturePDNSTargetCandidateV4(candidate)
	if err != nil {
		t.Fatal(err)
	}
	guards := 0
	guard := func() error { guards++; return nil }
	if err := removeExactStagedPDNSTargetFileV4(proof, live, guard, func() error {
		return errors.New("injected interruption after unlink")
	}); err == nil {
		t.Fatal("injected after-unlink fault was ignored")
	}
	if _, err := os.Lstat(candidate); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate remained after unlink: %v", err)
	}
	if err := removeExactStagedPDNSTargetFileV4(proof, live, guard, nil); err != nil {
		t.Fatalf("idempotent same-journal replay failed: %v", err)
	}
	if guards != 2 {
		t.Fatalf("native guard ran %d times, want twice", guards)
	}
}

func TestRemoveExactStagedPDNSTargetFileV4RefusesOwnerChange(t *testing.T) {
	for _, kind := range []string{"bytes", "replacement", "sidecar", "live", "guard"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			private := filepath.Join(root, "agent-private")
			liveDir := filepath.Join(root, "powerdns")
			if err := os.Mkdir(private, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(liveDir, 0o755); err != nil {
				t.Fatal(err)
			}
			candidate := filepath.Join(private, ".celikpanel-switch-test.sqlite3")
			live := filepath.Join(liveDir, "pdns.sqlite3")
			writePDNSTargetFixture(t, candidate)
			proof, err := CapturePDNSTargetCandidateV4(candidate)
			if err != nil {
				t.Fatal(err)
			}
			guard := func() error {
				switch kind {
				case "bytes":
					f, err := os.OpenFile(candidate, os.O_WRONLY, 0)
					if err != nil {
						return err
					}
					_, err = f.WriteAt([]byte("edit"), 128)
					return errors.Join(err, f.Close())
				case "replacement":
					other := filepath.Join(root, "other.sqlite3")
					writePDNSTargetFixture(t, other)
					return os.Rename(other, candidate)
				case "sidecar":
					return os.WriteFile(candidate+"-wal", []byte("owner"), 0o600)
				case "live":
					writePDNSTargetFixture(t, live)
				case "guard":
					return errors.New("native proof failed")
				}
				return nil
			}
			if err := removeExactStagedPDNSTargetFileV4(proof, live, guard, nil); err == nil {
				t.Fatal("changed target or failed native guard permitted deletion")
			}
			if _, err := os.Lstat(candidate); err != nil {
				t.Fatalf("candidate was deleted on refusal: %v", err)
			}
		})
	}
}
