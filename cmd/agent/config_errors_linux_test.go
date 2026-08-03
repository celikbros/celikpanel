//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestFailedConfigValidationIsTypedAfterSuccessfulRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nginx.conf")
	original := []byte("events {}\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}

	err := applyConfigUpdate(path, []byte("bad directive\n"), &validatorSpec{
		name: "nginx",
		check: func() (string, error) {
			return "nginx: invalid directive\nmore detail", errors.New("exit status 1")
		},
	}, nil)
	if !errors.Is(err, errConfigValidationFail) {
		t.Fatalf("applyConfigUpdate error = %v, want validation sentinel", err)
	}
	rpcErr := configRPCError(err)
	if rpcErr == nil || rpcErr.Code != transport.ConfigErrorValidationFail {
		t.Fatalf("configRPCError = %#v, want typed validation failure", rpcErr)
	}

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != string(original) {
		t.Fatalf("content after failed validation = %q, want %q", content, original)
	}
}

func TestFailedRollbackDoesNotMasqueradeAsExpectedConfigRefusal(t *testing.T) {
	base := t.TempDir()
	managedDir := filepath.Join(base, "managed")
	if err := os.Mkdir(managedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(managedDir, "nginx.conf")
	if err := os.WriteFile(path, []byte("events {}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(base, "replacement")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}

	err := applyConfigUpdate(path, []byte("bad directive\n"), &validatorSpec{
		name: "nginx",
		check: func() (string, error) {
			if err := os.Remove(path); err != nil {
				return "", err
			}
			if err := os.Remove(managedDir); err != nil {
				return "", err
			}
			if err := os.Symlink(targetDir, managedDir); err != nil {
				return "", err
			}
			return "nginx: invalid directive", errors.New("exit status 1")
		},
	}, nil)
	if err == nil {
		t.Fatal("applyConfigUpdate succeeded despite validation and rollback failure")
	}
	if configRPCError(err) != nil {
		t.Fatalf("rollback failure was downgraded to expected RPC error: %v", err)
	}
}
