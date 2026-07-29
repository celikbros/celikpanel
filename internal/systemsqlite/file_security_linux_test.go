//go:build linux

package systemsqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenManagedSourcePinsLinuxFileDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.sqlite3")
	if err := os.WriteFile(path, []byte("sqlite-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := openManagedSource(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer source.close()
	if !strings.HasPrefix(source.databasePath(), "/proc/self/fd/") {
		t.Fatalf("database path is not pinned: %q", source.databasePath())
	}
	if strings.Contains(source.databasePath(), filepath.Base(path)) {
		t.Fatalf("database path reopens a basename: %q", source.databasePath())
	}
}

func TestLinuxPinnedSQLiteOpenDoesNotFollowReplacementBasename(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "managed.sqlite3")
	database := createTestSQLite(t, path, "DELETE")
	if _, err := database.Exec("INSERT INTO parent(id) VALUES (2)"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	source, err := openManagedSource(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer source.close()
	if err := os.Rename(path, filepath.Join(root, "managed-pinned.sqlite3")); err != nil {
		t.Fatal(err)
	}
	replacement := createTestSQLite(t, path, "DELETE")
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := openSQLite(context.Background(), source.databasePath(), "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	var count int
	if err := opened.QueryRow("SELECT COUNT(*) FROM parent").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("opened row count = %d, want 2", count)
	}
}

func TestLinuxManagedSourceRejectsFinalSymlinkAndHardLink(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "managed.sqlite3")
	if err := os.WriteFile(original, []byte("sqlite-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "symlink.sqlite3")
	if err := os.Symlink(original, symlink); err != nil {
		t.Fatal(err)
	}
	if file, err := openPinnedManagedFile(symlink, false); err == nil {
		file.Close()
		t.Fatal("O_NOFOLLOW open accepted a final symbolic link")
	}
	if _, err := openManagedSource(symlink, false); err == nil {
		t.Fatal("managed source accepted a final symbolic link")
	}

	hardLink := filepath.Join(root, "hardlink.sqlite3")
	if err := os.Link(original, hardLink); err != nil {
		t.Fatal(err)
	}
	if _, err := openManagedSource(original, false); err == nil || !strings.Contains(err.Error(), "hard links") {
		t.Fatalf("managed source hard-link error = %v", err)
	}
}
