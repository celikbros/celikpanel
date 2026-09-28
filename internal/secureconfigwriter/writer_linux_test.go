//go:build linux

package secureconfigwriter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

func TestWriteRequiresExactOwnedPreimage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named.conf")
	before := []byte("owner content\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	owner := &Owner{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	expected := dnsengineartifact.FileSnapshot{
		Path: path, Exists: true, Mode: 0o600, OwnerKnown: true,
		UID: owner.UID, GID: owner.GID,
		Data: before, SHA256: dnsengineartifact.DigestBytes(before),
	}
	if err := Write(path, []byte("managed\n"), 0o600, &expected, Options{RequiredOwner: owner}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "managed\n" {
		t.Fatalf("published %q: %v", got, err)
	}
	if err := Write(path, []byte("stale write\n"), 0o600, &expected, Options{RequiredOwner: owner}); err == nil {
		t.Fatal("stale preimage accepted")
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "managed\n" {
		t.Fatalf("stale write changed file to %q: %v", got, err)
	}
}

func TestWriteAbsentPreimageRefusesInterloper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named.conf")
	expected := dnsengineartifact.FileSnapshot{Path: path}
	owner := &Owner{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	err := Write(path, []byte("managed\n"), 0o600, &expected, Options{
		RequiredOwner: owner,
		BeforeFinalParentProof: func() {
			if err := os.WriteFile(path, []byte("interloper\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	})
	if !errors.Is(err, unix.EEXIST) {
		t.Fatalf("publish error = %v, want EEXIST", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "interloper\n" {
		t.Fatalf("interloper changed to %q: %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging entries = %v: %v", entries, err)
	}
}

func TestWriteRefusesSymlinkParent(t *testing.T) {
	inside, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(inside, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(link, "named.conf")
	if err := Write(path, []byte("managed\n"), 0o600, nil, Options{}); err == nil {
		t.Fatal("symlink parent accepted")
	}
	if _, err := os.Lstat(filepath.Join(outside, "named.conf")); !os.IsNotExist(err) {
		t.Fatalf("outside target created: %v", err)
	}
}
