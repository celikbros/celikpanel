//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func openTestChallengeAnchor(t *testing.T) (string, int) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root ownership assertions require root")
	}
	root := t.TempDir()
	if err := os.Chown(root, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(
		root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return root, fd
}

func TestSecureACMEChallengeDirectoryCreatesRootOwnedConfinedTree(t *testing.T) {
	root, rootFD := openTestChallengeAnchor(t)
	relative := "celikpanel-agent/acme-http-01/subscriptions/7/domains/19/.well-known/acme-challenge"
	if err := secureEnsureACMEChallengeDirectoryAt(rootFD, relative); err != nil {
		t.Fatal(err)
	}

	current := root
	for _, component := range strings.Split(relative, "/") {
		current = filepath.Join(current, component)
		info, err := os.Stat(current)
		if err != nil {
			t.Fatal(err)
		}
		var stat unix.Stat_t
		if err := unix.Stat(current, &stat); err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm() != 0o755 {
			t.Fatalf("unsafe ACME directory %s: mode=%o uid=%d gid=%d", current, info.Mode().Perm(), stat.Uid, stat.Gid)
		}
	}
}

func TestSecureACMEChallengeDirectoryRejectsSymlinkComponent(t *testing.T) {
	root, rootFD := openTestChallengeAnchor(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "celikpanel-agent")); err != nil {
		t.Fatal(err)
	}

	err := secureEnsureACMEChallengeDirectoryAt(
		rootFD, "celikpanel-agent/acme-http-01/.well-known/acme-challenge",
	)
	if err == nil {
		t.Fatal("symlinked ACME component was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "acme-http-01")); !os.IsNotExist(statErr) {
		t.Fatalf("symlink escape wrote outside the trusted root: %v", statErr)
	}
}

func TestSecureACMEChallengeDirectoryRejectsWritableAncestorBeforeTOCTOU(t *testing.T) {
	root, rootFD := openTestChallengeAnchor(t)
	mutable := filepath.Join(root, "mutable")
	if err := os.Mkdir(mutable, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(mutable, 0o777); err != nil {
		t.Fatal(err)
	}

	err := secureEnsureACMEChallengeDirectoryAt(
		rootFD, "mutable/subscriptions/7/domains/19/.well-known/acme-challenge",
	)
	if err == nil || !strings.Contains(err.Error(), "must not be group/other writable") {
		t.Fatalf("tenant-writable ancestor must fail before path use, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(mutable, "subscriptions")); !os.IsNotExist(statErr) {
		t.Fatalf("writable ancestor was traversed before rejection: %v", statErr)
	}
}

func TestSecureACMEChallengeDirectoryUsesDescriptorAcrossPathSwap(t *testing.T) {
	root, rootFD := openTestChallengeAnchor(t)
	if err := secureEnsureACMEChallengeDirectoryAt(rootFD, "stable"); err != nil {
		t.Fatal(err)
	}
	stableFD, err := openSSLConfinedAt(
		rootFD, "stable", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(stableFD)

	outside := t.TempDir()
	if err := os.Rename(filepath.Join(root, "stable"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "stable")); err != nil {
		t.Fatal(err)
	}
	if err := secureEnsureACMEChallengeDirectoryAt(
		stableFD, "subscriptions/7/domains/19/.well-known/acme-challenge",
	); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(
		root, "moved", "subscriptions", "7", "domains", "19", ".well-known", "acme-challenge",
	)); err != nil {
		t.Fatalf("descriptor-relative creation did not stay on the opened inode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "subscriptions")); !os.IsNotExist(err) {
		t.Fatalf("path swap escaped through the replacement symlink: %v", err)
	}
}
