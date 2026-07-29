//go:build linux

package systemsqlite

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareSnapshotRootCreatesPrivateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "snapshots")
	if err := prepareSnapshotRoot(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("snapshot root mode = %o", got)
	}
}

func TestPrepareSnapshotRootRejectsPermissiveExistingRootWithoutChmod(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "snapshots")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareSnapshotRoot(root); err == nil {
		t.Fatal("permissive snapshot root was accepted")
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("snapshot root mode changed to %o", got)
	}
}
