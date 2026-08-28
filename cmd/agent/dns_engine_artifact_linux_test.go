//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovePDNSSwitchArtifactRejectsUnsafeAndReplacementEntries(t *testing.T) {
	t.Run("regular", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "candidate.sqlite3")
		if err := os.WriteFile(path, []byte("exact"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := removePDNSSwitchArtifact(path); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("regular artifact remains: %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		link := filepath.Join(root, "candidate.sqlite3")
		if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := removePDNSSwitchArtifact(link); err == nil {
			t.Fatal("symlink artifact was accepted")
		}
		if raw, err := os.ReadFile(target); err != nil || string(raw) != "keep" {
			t.Fatalf("symlink target changed: %q err=%v", raw, err)
		}
	})
	t.Run("non-regular", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "candidate.sqlite3")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := removePDNSSwitchArtifact(path); err == nil {
			t.Fatal("directory artifact was accepted")
		}
	})
	t.Run("replacement-toctou", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "candidate.sqlite3")
		original := filepath.Join(root, "original-held")
		if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
		removePDNSSwitchArtifactBeforeRename = func(candidate string) error {
			if err := os.Rename(candidate, original); err != nil {
				return err
			}
			return os.WriteFile(candidate, []byte("replacement"), 0o600)
		}
		t.Cleanup(func() { removePDNSSwitchArtifactBeforeRename = nil })
		if err := removePDNSSwitchArtifact(path); err == nil {
			t.Fatal("replacement race was accepted")
		}
		if raw, err := os.ReadFile(path); err != nil || string(raw) != "replacement" {
			t.Fatalf("replacement was deleted: %q err=%v", raw, err)
		}
		if raw, err := os.ReadFile(original); err != nil || string(raw) != "original" {
			t.Fatalf("original artifact was lost: %q err=%v", raw, err)
		}
	})
}
