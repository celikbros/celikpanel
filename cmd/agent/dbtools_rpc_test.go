package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstInstalledDBToolRootSupportsArchLayout(t *testing.T) {
	dir := t.TempDir()
	debian := filepath.Join(dir, "usr-share-phpmyadmin")
	arch := filepath.Join(dir, "usr-share-webapps-phpMyAdmin")
	if err := os.MkdirAll(arch, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := firstInstalledDBToolRoot([]string{debian, arch}); got != arch {
		t.Fatalf("firstInstalledDBToolRoot() = %q, want %q", got, arch)
	}
}

func TestFirstInstalledDBToolRootPrefersFirstKnownLayout(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := firstInstalledDBToolRoot([]string{first, second}); got != first {
		t.Fatalf("firstInstalledDBToolRoot() = %q, want %q", got, first)
	}
}
