package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFPMSocketWithPatternsSupportsUnversionedArchSocket(t *testing.T) {
	dir := t.TempDir()
	archSocket := filepath.Join(dir, "php-fpm.sock")
	if err := os.WriteFile(archSocket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := detectFPMSocketWithPatterns([]string{
		filepath.Join(dir, "php*-missing.sock"),
		archSocket,
	})
	if got != archSocket {
		t.Fatalf("detectFPMSocketWithPatterns() = %q, want %q", got, archSocket)
	}
}

func TestDetectFPMSocketWithPatternsPicksNewestVersionedSocket(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"php8.3-fpm.sock", "php8.4-fpm.sock"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want := filepath.Join(dir, "php8.4-fpm.sock")
	if got := detectFPMSocketWithPatterns([]string{filepath.Join(dir, "php*-fpm.sock")}); got != want {
		t.Fatalf("detectFPMSocketWithPatterns() = %q, want %q", got, want)
	}
}
