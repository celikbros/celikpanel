//go:build !linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultMailTLSDirectoryFailsClosedOutsideLinux(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "mail-tls")
	_, err := prepareDefaultMailTLSDirectory(filepath.Join(directory, "default-cert.pem"), filepath.Join(directory, "default-key.pem"))
	if err == nil || !strings.Contains(err.Error(), "requires Linux openat2") {
		t.Fatalf("non-Linux directory error = %v", err)
	}
	if _, statErr := os.Lstat(directory); !os.IsNotExist(statErr) {
		t.Fatalf("non-Linux refusal mutated the target: %v", statErr)
	}
}
