//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecureLiveIdleTemporaryDirectoryIgnoresTMPDIR(t *testing.T) {
	untrustedRoot := t.TempDir()
	t.Setenv("TMPDIR", untrustedRoot)
	directoryPath, err := createSecureLiveIdleTemporaryDirectory()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directoryPath)
	if filepath.Dir(directoryPath) != liveIdleTemporaryRoot {
		t.Fatalf("workspace=%q, expected child of %q", directoryPath, liveIdleTemporaryRoot)
	}
	entries, err := os.ReadDir(untrustedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("untrusted TMPDIR received workspace entries: %v", entries)
	}
}
