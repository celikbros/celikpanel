//go:build linux

package bindroot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestVerifyAtRejectsUnsafeNativeRootAndPackageProof(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned directory fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"var", "var/cache"} {
		if err := os.Mkdir(filepath.Join(root, path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	parent := filepath.Join(root, "var/cache/bind")
	if err := os.Mkdir(parent, 0o1775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(parent, 0, 1234); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chmod(parent, 0o1775); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(parent, "celikpanel")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	proofs := 0
	proof := func() error { proofs++; return nil }
	if err := VerifyAt(fd, APT, 1234, proof); err != nil || proofs != 2 {
		t.Fatalf("exact root rejected: %v, proofs=%d", err, proofs)
	}
	if err := VerifyAt(fd, APT, 1234, func() error { return errors.New("foreign package") }); err == nil || !strings.Contains(err.Error(), "foreign package") {
		t.Fatalf("foreign package accepted: %v", err)
	}
	if err := VerifyAt(fd, APT, 1234, func() error { return unix.Chmod(parent, 0o775) }); err == nil {
		t.Fatal("BIND parent change between proof walks was accepted")
	}
	if err := unix.Chmod(parent, 0o1775); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(child, "generations")
	if err := os.Mkdir(catalog, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogIdentityAt(fd, APT); err != nil {
		t.Fatalf("real catalog rejected: %v", err)
	}
	if err := os.Remove(catalog); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(parent, catalog); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogIdentityAt(fd, APT); err == nil {
		t.Fatal("symlinked catalog accepted")
	}
	if err := os.Remove(catalog); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, child); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAt(fd, APT, 1234, proof); err == nil {
		t.Fatal("symlinked managed root accepted")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAt(fd, APT, 1234, proof); err == nil {
		t.Fatal("non-sticky BIND parent accepted")
	}
}

func TestVerifyAtPacmanRootRequiresStickyVendorParent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned directory fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	varDir := filepath.Join(root, "var")
	if err := os.Mkdir(varDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(varDir, "named")
	if err := os.Mkdir(parent, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(parent, 0, 1234); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chmod(parent, 0o1770); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(parent, "celikpanel"), 0o755); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := VerifyAt(fd, Pacman, 1234, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chmod(parent, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAt(fd, Pacman, 1234, func() error { return nil }); err == nil {
		t.Fatal("non-sticky pacman BIND parent accepted")
	}
}
