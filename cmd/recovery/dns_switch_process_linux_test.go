//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyRunningExecutableMatchesStableProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	started, err := verifyRunningExecutable(os.Getpid(), executable)
	if err != nil || started == "" {
		t.Fatalf("stable executable was not observed: %q %v", started, err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRunningExecutable(os.Getpid(), other); err == nil {
		t.Fatal("foreign executable inode accepted")
	}
}

func TestVerifyNativeBINDExecutableRejectsUntrustedProcessInputs(t *testing.T) {
	if _, err := verifyNativeBINDExecutable(0, "/usr/sbin/named"); err == nil {
		t.Fatal("zero process accepted")
	}
	if _, err := verifyNativeBINDExecutable(uint64(os.Getpid()), "/proc/self/exe"); err == nil {
		t.Fatal("arbitrary executable path accepted")
	}
	if _, err := verifyNativeBINDExecutable(uint64(os.Getpid()), "/usr/sbin/named"); err == nil {
		t.Fatal("recovery process accepted as named")
	}
}
