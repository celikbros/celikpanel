//go:build linux

package servicemutationledger

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func archiveFixture(t *testing.T) (string, string, []byte, FileOwner) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(dir, "active.json")
	archive := filepath.Join(dir, "archive.json")
	raw := bytes.Repeat([]byte("accepted-v3-journal\n"), 20)
	if err := os.WriteFile(active, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return active, archive, raw, FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
}

func TestArchiveFileExactCutsPreserveOrReprove(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		hook                        func() archiveFaultHooks
		activeWanted, archiveWanted bool
	}{
		{"partial-write", func() archiveFaultHooks {
			return archiveFaultHooks{AfterPartialWrite: func() error { return errors.New("cut") }}
		}, true, false},
		{"after-link", func() archiveFaultHooks {
			return archiveFaultHooks{AfterLink: func() error { return errors.New("cut") }}
		}, true, true},
		{"after-archive", func() archiveFaultHooks {
			return archiveFaultHooks{AfterArchive: func() error { return errors.New("cut") }}
		}, true, true},
		{"after-retire", func() archiveFaultHooks {
			return archiveFaultHooks{AfterRetire: func() error { return errors.New("cut") }}
		}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			active, archive, raw, owner := archiveFixture(t)
			if err := archiveFileExactWithHooks(active, archive, raw, 4096, owner, tc.hook()); err == nil {
				t.Fatal("fault cut succeeded")
			}
			_, activeExists, err := ReadFile(active, 4096, owner)
			if err != nil || activeExists != tc.activeWanted {
				t.Fatalf("active exists=%v: %v", activeExists, err)
			}
			got, archived, err := ReadFile(archive, 4096, owner)
			if err != nil || archived != tc.archiveWanted || (archived && !bytes.Equal(got, raw)) {
				t.Fatalf("archive exists=%v: %v", archived, err)
			}
			if tc.activeWanted {
				if err := ArchiveFileExact(active, archive, raw, 4096, owner); err != nil {
					t.Fatalf("same-request replay: %v", err)
				}
			}
			got, archived, err = ReadFile(archive, 4096, owner)
			if err != nil || !archived || !bytes.Equal(got, raw) {
				t.Fatalf("archive replay readback: %v", err)
			}
		})
	}
}

func TestArchiveFileExactRejectsConflictAndUnsafeNames(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(string) error
	}{
		{"different", func(path string) error { return os.WriteFile(path, []byte("foreign"), 0600) }},
		{"symlink", func(path string) error { return os.Symlink("active.json", path) }},
		{"hardlink", func(path string) error { return os.Link(filepath.Join(filepath.Dir(path), "active.json"), path) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			active, archive, raw, owner := archiveFixture(t)
			if err := tc.prepare(archive); err != nil {
				t.Fatal(err)
			}
			if err := ArchiveFileExact(active, archive, raw, 4096, owner); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			got, err := os.ReadFile(active)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatalf("active evidence changed: %v", err)
			}
		})
	}
	t.Run("changed-active", func(t *testing.T) {
		active, archive, raw, owner := archiveFixture(t)
		if err := os.WriteFile(active, []byte("owner edit"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ArchiveFileExact(active, archive, raw, 4096, owner); err == nil {
			t.Fatal("changed active accepted")
		}
		if _, err := os.Lstat(archive); !os.IsNotExist(err) {
			t.Fatalf("archive appeared: %v", err)
		}
	})
}
