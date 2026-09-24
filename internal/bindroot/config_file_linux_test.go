//go:build linux

package bindroot

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadExactBINDConfigAtOwnerModesAndSymlinks(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned native config fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/bind"), 0o755); err != nil {
		t.Fatal(err)
	}
	aptPath := filepath.Join(root, "etc/bind/named.conf.local")
	if err := os.WriteFile(aptPath, []byte("// managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(aptPath, 0, 12345); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if raw, identity, err := ReadExactBINDConfigAt(fd, APT, 12345, "/etc/bind/named.conf.local"); err != nil || string(raw) != "// managed\n" || identity.GID != 12345 {
		t.Fatalf("APT group-owned config rejected: %q %+v %v", raw, identity, err)
	}
	if _, _, err := ReadExactBINDConfigAt(fd, APT, 12346, "/etc/bind/named.conf.local"); err == nil {
		t.Fatal("foreign APT owner accepted")
	}
	if _, _, err := ReadExactBINDConfigAt(fd, APT, 12345, "/etc/passwd"); err == nil {
		t.Fatal("arbitrary APT path accepted")
	}
	if err := os.Chmod(aptPath, 0o664); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadExactBINDConfigAt(fd, APT, 12345, "/etc/bind/named.conf.local"); err == nil {
		t.Fatal("writable APT config accepted")
	}
	if err := os.Remove(aptPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", aptPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadExactBINDConfigAt(fd, APT, 12345, "/etc/bind/named.conf.local"); err == nil {
		t.Fatal("symlinked APT config accepted")
	}
	archPath := filepath.Join(root, "etc/named.conf")
	if err := os.WriteFile(archPath, []byte("// managed\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(archPath, 0, 12345); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadExactBINDConfigAt(fd, Pacman, 12345, "/etc/named.conf"); err != nil {
		t.Fatalf("pacman config rejected: %v", err)
	}
	if err := os.Chown(archPath, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadExactBINDConfigAt(fd, Pacman, 12345, "/etc/named.conf"); err == nil {
		t.Fatal("root:root pacman config accepted")
	}
}
