//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSecureConfigRegularFileLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service.conf")

	if err := secureWriteConfig(path, []byte("first\n"), 0o640); err != nil {
		t.Fatalf("create secure config: %v", err)
	}
	got, err := secureReadConfig(path)
	if err != nil {
		t.Fatalf("read secure config: %v", err)
	}
	if string(got) != "first\n" {
		t.Fatalf("read %q, want %q", got, "first\n")
	}
	if err := secureWriteConfig(path, []byte("second\n"), 0o640); err != nil {
		t.Fatalf("replace secure config: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second\n" {
		t.Fatalf("replacement left %q, want %q", got, "second\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("replacement mode = %o, want 640", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "service.conf" {
		t.Fatalf("atomic write left temporary entries: %v", entries)
	}
	if err := secureRemoveConfig(path); err != nil {
		t.Fatalf("remove secure config: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("removed path still exists: %v", err)
	}
}

func TestSecureConfigOwnerWriterAbsentPreimageDoesNotReplaceInterloper(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service.conf")
	expected := dnsFileSnapshot{Path: filepath.Clean(path)}
	interloper := []byte("interloper\n")
	var interloperInfo os.FileInfo

	err := secureWriteConfigReplacingSnapshotWithOwnerAndHook(
		path,
		[]byte("managed\n"),
		0o600,
		&expected,
		uint32(os.Geteuid()),
		uint32(os.Getegid()),
		func() {
			if writeErr := os.WriteFile(path, interloper, 0o640); writeErr != nil {
				t.Fatal(writeErr)
			}
			if chmodErr := os.Chmod(path, 0o640); chmodErr != nil {
				t.Fatal(chmodErr)
			}
			var statErr error
			interloperInfo, statErr = os.Lstat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
		},
	)
	if !errors.Is(err, unix.EEXIST) {
		t.Fatalf("absent-preimage publication error = %v, want EEXIST", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(interloper) {
		t.Fatalf("interloper content changed to %q", data)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(interloperInfo, info) || info.Mode().Perm() != 0o640 {
		t.Fatalf("interloper identity or mode changed: %+v", info)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("failed no-replace publication left staging entries: %v", entries)
	}
}

func TestApplyConfigUpdateRestoresAndReloadsPreviousConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service.conf")
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	validator := &validatorSpec{
		name:   "test validation failed",
		reload: "test-service",
		check: func() (string, error) {
			content, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			if string(content) != "old\n" && string(content) != "new\n" {
				return string(content), errors.New("unexpected content")
			}
			return "", nil
		},
	}
	reloads := 0
	reload := func(unit string) error {
		reloads++
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch reloads {
		case 1:
			if unit != "test-service" || string(content) != "new\n" {
				t.Fatalf("first reload saw unit=%q content=%q", unit, content)
			}
			return errors.New("simulated reload failure")
		case 2:
			if unit != "test-service" || string(content) != "old\n" {
				t.Fatalf("rollback reload saw unit=%q content=%q", unit, content)
			}
			return nil
		default:
			t.Fatalf("unexpected reload call %d", reloads)
			return nil
		}
	}

	err := applyConfigUpdate(path, []byte("new\n"), validator, reload)
	if err == nil || !strings.Contains(err.Error(), "previous configuration restored and reloaded") {
		t.Fatalf("reload failure result = %v", err)
	}
	if reloads != 2 {
		t.Fatalf("reload calls = %d, want 2", reloads)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old\n" {
		t.Fatalf("content after rollback = %q, want old", content)
	}
}

func TestSecureConfigRefusesIntermediateSymlinkEscape(t *testing.T) {
	inside := t.TempDir()
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "victim.conf")
	if err := os.WriteFile(outsidePath, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(inside, "managed")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	escapedPath := filepath.Join(link, "victim.conf")

	if err := rejectConfigPathSymlinks(escapedPath); err == nil ||
		!strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("preflight did not identify intermediate symlink: %v", err)
	}
	if _, err := secureReadConfig(escapedPath); err == nil {
		t.Fatal("secure read followed an intermediate symlink")
	}
	if err := secureWriteConfig(escapedPath, []byte("changed\n"), 0o600); err == nil {
		t.Fatal("secure write followed an intermediate symlink")
	}
	if err := secureRemoveConfig(escapedPath); err == nil {
		t.Fatal("secure remove followed an intermediate symlink")
	}

	got, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatalf("outside target was removed: %v", err)
	}
	if string(got) != "outside\n" {
		t.Fatalf("outside target changed to %q", got)
	}
}

func TestSecureConfigRefusesFinalSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.conf")
	if err := os.WriteFile(target, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "service.conf")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := secureReadConfig(link); err == nil {
		t.Fatal("secure read followed a final symlink")
	}
	if err := secureWriteConfig(link, []byte("changed\n"), 0o600); err == nil {
		t.Fatal("secure write followed a final symlink")
	}
	if err := secureRemoveConfig(link); err == nil {
		t.Fatal("secure remove accepted a final symlink")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "outside\n" {
		t.Fatalf("symlink target changed to %q", got)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("refused final symlink was unexpectedly removed: %v", err)
	}
}

func TestSecureConfigRefusesNonRegularTargets(t *testing.T) {
	dir := t.TempDir()
	if _, err := secureReadConfig(dir); err == nil {
		t.Fatal("secure read accepted a directory")
	}
	if err := secureWriteConfig(dir, []byte("x"), 0o600); err == nil {
		t.Fatal("secure write accepted a directory")
	}
	if err := secureRemoveConfig(dir); err == nil {
		t.Fatal("secure remove accepted a directory")
	}
}
