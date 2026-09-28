//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This test needs root ownership only for temporary files, never a live daemon.
func TestFreshPDNSExecutableProofV3(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned executable proof; CI runs this test with sudo")
	}
	payload := []byte("measured native executable fixture\n")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	for _, name := range []string{"regular", "wrong-content", "wrong-mode", "special-mode", "wrong-owner", "symlink", "hardlink"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pdns_server")
			if err := os.WriteFile(path, payload, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0755); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "wrong-content":
				if err := os.WriteFile(path, []byte("changed\n"), 0755); err != nil {
					t.Fatal(err)
				}
			case "wrong-mode":
				if err := os.Chmod(path, 0775); err != nil {
					t.Fatal(err)
				}
			case "special-mode":
				if err := os.Chmod(path, 0755|os.ModeSetuid); err != nil {
					t.Fatal(err)
				}
			case "wrong-owner":
				if err := os.Chown(path, 12345, 12345); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(path, path+".link"); err != nil {
					t.Fatal(err)
				}
				path += ".link"
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			}
			err := verifyFreshPDNSExecutableV3(path, digest)
			if name == "regular" {
				if err != nil {
					t.Fatalf("actual root-owned Linux file rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsafe or changed executable accepted")
			} else if name == "wrong-content" && !strings.Contains(err.Error(), "differs from measured") {
				t.Fatalf("did not reach content verification: %v", err)
			}
		})
	}
}
