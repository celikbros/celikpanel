//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadPinnedPanelTLSFileRejectsUnsafeMetadata(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned TLS metadata contract requires root")
	}
	root := t.TempDir()
	if err := os.Chown(root, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "panel.crt")
	keyPath := filepath.Join(root, "panel.key")
	if err := os.WriteFile(path, []byte("certificate"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("private-key"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(keyPath, 0, 0); err != nil {
		t.Fatal(err)
	}
	cert, key, err := readPinnedPanelTLSFiles(path, keyPath, 1024, 1024)
	if err != nil || string(cert) != "certificate" || string(key) != "private-key" {
		t.Fatalf("safe TLS pair = %q %q, %v", cert, key, err)
	}

	t.Run("symbolic link", func(t *testing.T) {
		link := filepath.Join(root, "linked.crt")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readPinnedPanelTLSFiles(link, keyPath, 1024, 1024); !errors.Is(err, errPanelTLSMetadataUnsafe) {
			t.Fatalf("symlink error = %v", err)
		}
	})

	t.Run("hard link", func(t *testing.T) {
		hardlink := filepath.Join(root, "hard.crt")
		if err := os.Link(path, hardlink); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readPinnedPanelTLSFiles(path, keyPath, 1024, 1024); !errors.Is(err, errPanelTLSMetadataUnsafe) {
			t.Fatalf("hardlink error = %v", err)
		}
		if err := os.Remove(hardlink); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("group writable directory", func(t *testing.T) {
		if err := os.Chmod(root, 0o770); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o750) })
		if _, _, err := readPinnedPanelTLSFiles(path, keyPath, 1024, 1024); !errors.Is(err, errPanelTLSMetadataUnsafe) {
			t.Fatalf("writable directory error = %v", err)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		if err := os.Chmod(root, 0o750); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readPinnedPanelTLSFiles(path, keyPath, 4, 1024); !errors.Is(err, errPanelTLSMetadataUnsafe) {
			t.Fatalf("oversized file error = %v", err)
		}
	})

	t.Run("different directories", func(t *testing.T) {
		other := filepath.Join(t.TempDir(), "panel.key")
		if _, _, err := readPinnedPanelTLSFiles(path, other, 1024, 1024); !errors.Is(err, errPanelTLSMetadataUnsafe) {
			t.Fatalf("split directory error = %v", err)
		}
	})

	t.Run("world readable private key", func(t *testing.T) {
		if err := os.Chmod(keyPath, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(keyPath, 0o640) })
		if _, _, err := readPinnedPanelTLSFiles(path, keyPath, 1024, 1024); !errors.Is(err, errPanelTLSMetadataUnsafe) {
			t.Fatalf("world-readable key error = %v", err)
		}
	})
}
