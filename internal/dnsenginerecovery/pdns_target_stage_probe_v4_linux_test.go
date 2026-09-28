//go:build linux

package dnsenginerecovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPDNSTargetV4AbsenceProofRejectsLiveFileSymlinkAndSidecar(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pdns.sqlite3")
	if err := verifyPDNSTargetAbsentV4(path, false); err != nil {
		t.Fatalf("empty target directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("owner data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSTargetAbsentV4(path, false); err == nil {
		t.Fatal("live database accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", path); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSTargetAbsentV4(path, false); err == nil {
		t.Fatal("live database symlink accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-wal", []byte("owner WAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSTargetAbsentV4(path, false); err == nil {
		t.Fatal("WAL sidecar accepted")
	}
}

func TestPDNSTargetV4CandidateAbsenceRequiresPrivateRootDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned candidate fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil { t.Fatal(err) }
	path := filepath.Join(root, ".celikpanel-switch-0123456789abcdef0123456789abcdef.sqlite3")
	if err := verifyPDNSTargetAbsentV4(path, true); err != nil {
		t.Fatalf("private empty candidate rejected: %v", err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSTargetAbsentV4(path, true); err == nil {
		t.Fatal("nonprivate candidate parent accepted")
	}
}
