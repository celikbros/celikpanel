//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPanelCertificateActivationStateSecureRoundTrip(t *testing.T) {
	directory := testPanelCertificateActivationDirectory(t)
	uid, gid := os.Geteuid(), os.Getegid()
	state, err := newPanelCertificateActivationState("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := writePanelCertificateActivationStateAt(
		directory,
		uid,
		gid,
		state,
	); err != nil {
		t.Fatalf("write state: %v", err)
	}
	path := filepath.Join(directory, panelCertificateActivationStateName)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("unexpected state metadata type %T", info.Sys())
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 ||
		stat.Nlink != 1 || int(stat.Uid) != uid || int(stat.Gid) != gid {
		t.Fatalf("unsafe state metadata: mode=%v nlink=%d owner=%d:%d",
			info.Mode(), stat.Nlink, stat.Uid, stat.Gid)
	}
	got, exists, err := readPanelCertificateActivationStateAt(directory, uid, gid)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if !exists || got != state {
		t.Fatalf("round trip mismatch: exists=%v got=%#v want=%#v", exists, got, state)
	}
	if err := removePanelCertificateActivationStateAt(directory, uid, gid); err != nil {
		t.Fatalf("remove state: %v", err)
	}
	if _, exists, err := readPanelCertificateActivationStateAt(directory, uid, gid); err != nil || exists {
		t.Fatalf("state remains after removal: exists=%v err=%v", exists, err)
	}
}

func TestPanelCertificateActivationStateRejectsUnsafeFiles(t *testing.T) {
	uid, gid := os.Geteuid(), os.Getegid()
	state, err := newPanelCertificateActivationState("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalPanelCertificateActivationState(state)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, path string) {
				target := filepath.Join(filepath.Dir(path), "target")
				if err := os.WriteFile(target, canonical, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hardlink",
			setup: func(t *testing.T, path string) {
				if err := os.WriteFile(path, canonical, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(path, filepath.Join(filepath.Dir(path), "second-link")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "wrong mode",
			setup: func(t *testing.T, path string) {
				if err := os.WriteFile(path, canonical, 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "oversized",
			setup: func(t *testing.T, path string) {
				if err := os.WriteFile(
					path,
					[]byte(strings.Repeat("x", panelCertificateActivationStateMaxSize+1)),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "non-canonical",
			setup: func(t *testing.T, path string) {
				if err := os.WriteFile(path, append([]byte{' '}, canonical...), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := testPanelCertificateActivationDirectory(t)
			path := filepath.Join(directory, panelCertificateActivationStateName)
			test.setup(t, path)
			if _, _, err := readPanelCertificateActivationStateAt(
				directory,
				uid,
				gid,
			); err == nil {
				t.Fatal("expected unsafe state to be rejected")
			}
			if err := writePanelCertificateActivationStateAt(
				directory,
				uid,
				gid,
				state,
			); err == nil {
				t.Fatal("expected unsafe existing state replacement to be rejected")
			}
		})
	}
}

func TestPanelCertificateActivationStateRejectsUnsafeDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readPanelCertificateActivationStateAt(
		directory,
		os.Geteuid(),
		os.Getegid(),
	); err == nil {
		t.Fatal("expected non-0700 state directory to be rejected")
	}
}

func TestPanelCertificateActivationStateAtomicReplacement(t *testing.T) {
	directory := testPanelCertificateActivationDirectory(t)
	uid, gid := os.Geteuid(), os.Getegid()
	oldState, err := newPanelCertificateActivationState("old.example.test")
	if err != nil {
		t.Fatal(err)
	}
	newState, err := newPanelCertificateActivationState("new.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := writePanelCertificateActivationStateAt(
		directory,
		uid,
		gid,
		oldState,
	); err != nil {
		t.Fatal(err)
	}

	originalRenameat := panelCertificateActivationRenameat
	panelCertificateActivationRenameat = func(int, string, int, string) error {
		return syscall.EIO
	}
	t.Cleanup(func() { panelCertificateActivationRenameat = originalRenameat })
	if err := writePanelCertificateActivationStateAt(
		directory,
		uid,
		gid,
		newState,
	); !errors.Is(err, syscall.EIO) {
		t.Fatalf("expected injected rename failure, got %v", err)
	}
	got, exists, err := readPanelCertificateActivationStateAt(directory, uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || got != oldState {
		t.Fatalf("old state was not preserved: exists=%v got=%#v", exists, got)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary state files remain: %v", matches)
	}
}

func testPanelCertificateActivationDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}
