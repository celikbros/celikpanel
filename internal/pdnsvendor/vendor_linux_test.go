//go:build linux

package pdnsvendor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

func TestVendorContractRejectsAlteredBytesAndOwner(t *testing.T) {
	for _, good := range []string{CertifiedDebian13Unit, CertifiedUbuntu2404Unit} {
		if err := VerifyBytes([]byte(good)); err != nil {
			t.Fatalf("reviewed unit rejected: %v", err)
		}
		altered := []byte(good)
		altered[len(altered)-1] ^= 1
		if err := VerifyBytes(altered); err == nil {
			t.Fatal("altered vendor bytes accepted")
		}
	}
	if err := VerifyPackageOwner([]byte(UnitPackageOwner), nil); err != nil {
		t.Fatal(err)
	}
	for _, output := range [][]byte{
		[]byte("bind9: " + UnitPath + "\n"),
		[]byte(UnitPackageOwner + "extra\n"),
		nil,
	} {
		if err := VerifyPackageOwner(output, nil); err == nil {
			t.Fatalf("foreign package owner accepted: %q", output)
		}
	}
	if err := VerifyPackageOwner([]byte(UnitPackageOwner), errors.New("unavailable")); err == nil {
		t.Fatal("failed package query accepted")
	}
}

func TestInspectUnitAtRejectsTamperingAndSymlinks(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned vendor fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "usr", "lib", "systemd", "system")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parent, "pdns.service")
	if err := os.WriteFile(file, []byte(CertifiedDebian13Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := inspectUnitAt(fd); err != nil {
		t.Fatalf("reviewed native file rejected: %v", err)
	}
	if err := os.WriteFile(file, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectUnitAt(fd); err == nil {
		t.Fatal("changed native file accepted")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", file); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectUnitAt(fd); err == nil {
		t.Fatal("symlinked native file accepted")
	}
	if _, err := InspectInstalledUnit(context.Background(), hostplatform.Profile{}); err == nil {
		t.Fatal("unsupported host profile accepted")
	}
}
