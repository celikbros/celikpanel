//go:build linux

package dnsenginerecovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

func pdnsConfigRemoveFixtureV4(t *testing.T) (int, string, dnsengineartifact.FileSnapshot) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned native config fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"etc", "etc/powerdns", "etc/powerdns/pdns.d"} {
		if err := os.Mkdir(filepath.Join(root, part), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "etc/powerdns/pdns.d/celikpanel.conf")
	data := []byte("managed-after\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	snapshot := dnsengineartifact.FileSnapshot{Path: "/etc/powerdns/pdns.d/celikpanel.conf", Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 0, Data: data, SHA256: dnsengineartifact.DigestBytes(data)}
	return fd, path, snapshot
}

func TestPDNSTargetConfigV4ExactRemovalAndOwnerEditRefusal(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		fd, path, snapshot := pdnsConfigRemoveFixtureV4(t)
		if err := removeExactPDNSTargetConfigAtV4(context.Background(), fd, snapshot, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("after-created file remains: %v", err)
		}
	})
	t.Run("late-owner-edit", func(t *testing.T) {
		fd, path, snapshot := pdnsConfigRemoveFixtureV4(t)
		err := removeExactPDNSTargetConfigAtV4(context.Background(), fd, snapshot, func() {
			if e := os.WriteFile(path, []byte("owner-edit!!!\n"), 0o644); e != nil {
				t.Fatal(e)
			}
		})
		if err == nil {
			t.Fatal("late owner edit was removed")
		}
		if data, e := os.ReadFile(path); e != nil || string(data) != "owner-edit!!!\n" {
			t.Fatalf("owner edit not preserved: %q %v", data, e)
		}
	})
	t.Run("hardlink", func(t *testing.T) {
		fd, path, snapshot := pdnsConfigRemoveFixtureV4(t)
		if err := os.Link(path, path+".foreign"); err != nil {
			t.Fatal(err)
		}
		if err := removeExactPDNSTargetConfigAtV4(context.Background(), fd, snapshot, nil); err == nil {
			t.Fatal("hardlinked file removed")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	})
}
