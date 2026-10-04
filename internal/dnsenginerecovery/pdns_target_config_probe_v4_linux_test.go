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

func TestPDNSTargetConfigPassV4MixedFilesAndOwnerEdit(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned native config fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"etc", "etc/powerdns", "etc/powerdns/pdns.d"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const gid = 12345
	main := filepath.Join(root, "etc/powerdns/pdns.conf")
	managed := filepath.Join(root, "etc/powerdns/pdns.d/celikpanel.conf")
	if err := os.WriteFile(main, []byte("before-main\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(main, 0, gid); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("after-managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := []dnsengineartifact.FileSnapshot{
		{Path: "/etc/powerdns/pdns.conf", Exists: true, Mode: 0o640, OwnerKnown: true, UID: 0, GID: gid, Data: []byte("before-main\n")},
		{Path: "/etc/powerdns/pdns.d/celikpanel-cluster.conf"},
		{Path: "/etc/powerdns/pdns.d/celikpanel.conf"},
	}
	after := []dnsengineartifact.FileSnapshot{
		{Path: before[0].Path, Exists: true, Mode: 0o640, OwnerKnown: true, UID: 0, GID: gid, Data: []byte("after-main\n")},
		{Path: before[1].Path},
		{Path: before[2].Path, Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 0, Data: []byte("after-managed\n")},
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	states, _, err := probePDNSTargetConfigPassV4(context.Background(), fd, before, after)
	if err != nil || len(states) != 3 || states[0] != PDNSTargetConfigBeforeV4 || states[1] != PDNSTargetConfigBeforeV4 || states[2] != PDNSTargetConfigAfterV4 {
		t.Fatalf("exact mixed checkpoint rejected: %v %v", states, err)
	}
	if err := os.WriteFile(managed, []byte("owner edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := probePDNSTargetConfigPassV4(context.Background(), fd, before, after); err == nil {
		t.Fatal("foreign config bytes accepted")
	}
	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", managed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := probePDNSTargetConfigPassV4(context.Background(), fd, before, after); err == nil {
		t.Fatal("symlink accepted as frozen after-image")
	}
}
