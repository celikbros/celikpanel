//go:build linux

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenLogFileBeneathRejectsSymlinksWithoutTouchingTarget(t *testing.T) {
	root := trustedTemporaryLogRoot(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("do not clear"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.log")); err != nil {
		t.Fatal(err)
	}

	for _, write := range []bool{false, true} {
		file, err := openLogFileBeneath(root, "escape.log", write, uint32(os.Geteuid()))
		if err == nil {
			_ = file.Close()
			t.Fatalf("symlink was accepted with write=%v", write)
		}
	}
	content, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "do not clear" {
		t.Fatalf("outside target changed to %q", content)
	}
}

func TestOpenLogFileBeneathUsesVerifiedRegularDescriptor(t *testing.T) {
	root := trustedTemporaryLogRoot(t)
	path := filepath.Join(root, "site-access.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	readFile, err := openLogFileBeneath(root, "site-access.log", false, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(readFile)
	closeErr := readFile.Close()
	if readErr != nil || closeErr != nil || string(content) != "line\n" {
		t.Fatalf("read=%q, readErr=%v, closeErr=%v", content, readErr, closeErr)
	}

	writeFile, err := openLogFileBeneath(root, "site-access.log", true, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile.Truncate(0); err != nil {
		_ = writeFile.Close()
		t.Fatal(err)
	}
	if err := writeFile.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("size=%d, want 0", info.Size())
	}
}

func TestOpenLogFileBeneathRejectsUntrustedRoot(t *testing.T) {
	root := trustedTemporaryLogRoot(t)
	if err := os.WriteFile(filepath.Join(root, "site.log"), []byte("line"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o770); err != nil {
		t.Fatal(err)
	}
	file, err := openLogFileBeneath(root, "site.log", false, uint32(os.Geteuid()))
	if err == nil {
		_ = file.Close()
		t.Fatal("group-writable trusted root was accepted")
	}
	if !strings.Contains(err.Error(), "must not be group- or world-writable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func trustedTemporaryLogRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}
