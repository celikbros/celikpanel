//go:build linux

package dnsenginerecovery

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func renamedPDNSTargetFixtureV4(t *testing.T) (string, string, func() error) {
	t.Helper()
	root := t.TempDir()
	privateDir := filepath.Join(root, "agent-private")
	liveDir := filepath.Join(root, "powerdns")
	if err := os.Mkdir(privateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(privateDir, ".celikpanel-switch-test.sqlite3")
	live := filepath.Join(liveDir, "pdns.sqlite3")
	writePDNSTargetFixture(t, candidate)
	return candidate, live, func() error { return nil }
}

func TestRestoreRenamedPDNSTargetV4ExactAndInterruptedReplay(t *testing.T) {
	candidate, live, guard := renamedPDNSTargetFixtureV4(t)
	proof, err := CapturePDNSTargetCandidateV4(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(candidate, live); err != nil {
		t.Fatal(err)
	}
	if err := restoreRenamedPDNSTargetFileV4(proof, live, guard, func() error {
		return errors.New("cut after native rename")
	}); err == nil {
		t.Fatal("injected cut ignored")
	}
	if err := VerifyPDNSTargetCandidateV4Exact(proof); err != nil {
		t.Fatalf("rename did not land: %v", err)
	}
	if err := verifyPDNSTargetAbsentV4(live, false); err != nil {
		t.Fatal(err)
	}
	if err := restoreRenamedPDNSTargetFileV4(proof, live, guard, nil); err != nil {
		t.Fatalf("same frozen request did not converge after cut: %v", err)
	}
}

func TestRestoreRenamedPDNSTargetV4RejectsModifiedLiveState(t *testing.T) {
	for _, mutation := range []string{"bytes", "replacement", "symlink", "sidecar", "candidate-reappears", "guard", "guard-edits"} {
		t.Run(mutation, func(t *testing.T) {
			candidate, live, guard := renamedPDNSTargetFixtureV4(t)
			proof, err := CapturePDNSTargetCandidateV4(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(candidate, live); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "bytes":
				f, err := os.OpenFile(live, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteAt([]byte("owner"), 128)
				if err := errors.Join(err, f.Close()); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				other := filepath.Join(filepath.Dir(live), "other.sqlite3")
				writePDNSTargetFixture(t, other)
				if err := os.Rename(other, live); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(live); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(candidate, live); err != nil {
					t.Fatal(err)
				}
			case "sidecar":
				if err := os.WriteFile(live+"-wal", []byte("owner"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "candidate-reappears":
				writePDNSTargetFixture(t, candidate)
			case "guard":
				guard = func() error { return errors.New("PowerDNS process exists") }
			case "guard-edits":
				guard = func() error { return os.WriteFile(live+"-shm", []byte("owner"), 0o600) }
			}
			if err := restoreRenamedPDNSTargetFileV4(proof, live, guard, nil); err == nil {
				t.Fatal("modified live state or failed native guard was accepted")
			}
			if _, err := os.Lstat(live); err != nil {
				t.Fatalf("live file was moved on refusal: %v", err)
			}
		})
	}
}

func TestRestoreRenamedPDNSTargetV4RejectsUnsafeDirectories(t *testing.T) {
	for _, mutation := range []string{"private-mode", "live-mode", "live-swap"} {
		t.Run(mutation, func(t *testing.T) {
			candidate, live, guard := renamedPDNSTargetFixtureV4(t)
			proof, err := CapturePDNSTargetCandidateV4(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(candidate, live); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "private-mode":
				if err := os.Chmod(filepath.Dir(candidate), 0o755); err != nil {
					t.Fatal(err)
				}
			case "live-mode":
				if err := os.Chmod(filepath.Dir(live), 0o777); err != nil {
					t.Fatal(err)
				}
			case "live-swap":
				guard = func() error {
					old := filepath.Dir(live) + "-old"
					if err := os.Rename(filepath.Dir(live), old); err != nil {
						return err
					}
					return os.Mkdir(filepath.Dir(live), 0o755)
				}
			}
			if err := restoreRenamedPDNSTargetFileV4(proof, live, guard, nil); err == nil {
				t.Fatal("unsafe parent was accepted")
			}
		})
	}
}
