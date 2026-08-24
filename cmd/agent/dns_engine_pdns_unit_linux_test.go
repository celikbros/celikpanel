//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type pdnsVendorRootFixture struct {
	root, unitPath string
	rootFD         int
}

func newPDNSVendorRootFixture(t *testing.T) pdnsVendorRootFixture {
	return newPDNSVendorRootFixtureWithUnit(
		t, []byte(certifiedDebian13PDNSVendorUnit),
	)
}

func newPDNSVendorRootFixtureWithUnit(
	t *testing.T,
	unitBytes []byte,
) pdnsVendorRootFixture {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("exact PowerDNS vendor file tests require root")
	}
	root := t.TempDir()
	mustChownMode(t, root, 0, 0, bindManagedRootMode)
	for _, relative := range []string{
		"usr", "usr/lib", "usr/lib/systemd", "usr/lib/systemd/system",
	} {
		candidate := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.Mkdir(candidate, os.FileMode(bindManagedRootMode)); err != nil {
			t.Fatal(err)
		}
		mustChownMode(t, candidate, 0, 0, bindManagedRootMode)
	}
	unitPath := filepath.Join(
		root, "usr", "lib", "systemd", "system", "pdns.service",
	)
	if err := os.WriteFile(
		unitPath, unitBytes, 0o0644,
	); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, unitPath, 0, 0, 0o0644)
	rootFD, err := unix.Open(
		root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(rootFD) })
	return pdnsVendorRootFixture{
		root: root, unitPath: unitPath, rootFD: rootFD,
	}
}

func TestPDNSVendorPackageOwnershipIsExactAndDeadlineBound(t *testing.T) {
	if err := verifyExactPDNSVendorPackageOwnership(
		context.Background(),
		func(context.Context) ([]byte, error) {
			return []byte(certifiedPDNSUnitPackageOwner), nil
		},
	); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{
		"evil: /usr/lib/systemd/system/pdns.service\n",
		certifiedPDNSUnitPackageOwner + certifiedPDNSUnitPackageOwner,
		certifiedPDNSUnitPackageOwner + "warning\n",
	} {
		if err := verifyExactPDNSVendorPackageOwnership(
			context.Background(),
			func(context.Context) ([]byte, error) {
				return []byte(output), nil
			},
		); err == nil {
			t.Fatalf("ambiguous PowerDNS package owner accepted: %q", output)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := verifyExactPDNSVendorPackageOwnership(
		ctx,
		func(commandCtx context.Context) ([]byte, error) {
			<-commandCtx.Done()
			return nil, commandCtx.Err()
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		time.Since(started) > time.Second {
		t.Fatalf("PowerDNS owner proof ignored deadline: %v", err)
	}
}

func TestInspectPDNSVendorUnitAtRejectsMetadataAndTOCTOU(t *testing.T) {
	for _, test := range []struct {
		name string
		unit []byte
		size int64
	}{
		{name: "debian-package", unit: []byte(certifiedDebian13PDNSVendorUnit), size: 1579},
		{name: "ubuntu-package", unit: []byte(certifiedUbuntu2404PDNSVendorUnit), size: 1565},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPDNSVendorRootFixtureWithUnit(t, test.unit)
			identity, err := inspectPDNSVendorUnitAt(
				fixture.rootFD, testUbuntu2404PDNSProfile(), nil,
			)
			if err != nil || identity.Inode == 0 || identity.Size != test.size {
				t.Fatalf("identity=%+v err=%v", identity, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "wrong-uid", mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 1201, 0, 0o0644)
		}},
		{name: "wrong-gid", mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 1202, 0o0644)
		}},
		{name: "group-write", mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 0, 0o0664)
		}},
		{name: "setuid", mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 0, 0o4644)
		}},
		{name: "sticky", mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 0, 0o1644)
		}},
		{name: "hardlink", mutate: func(t *testing.T, path string) {
			if err := os.Link(path, path+".link"); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPDNSVendorRootFixture(t)
			test.mutate(t, fixture.unitPath)
			if _, err := inspectPDNSVendorUnitAt(
				fixture.rootFD, testDebian13PDNSProfile(), nil,
			); err == nil {
				t.Fatal("unsafe PowerDNS vendor unit metadata was accepted")
			}
		})
	}
	t.Run("toctou-replacement", func(t *testing.T) {
		fixture := newPDNSVendorRootFixture(t)
		old := fixture.unitPath + ".old"
		_, err := inspectPDNSVendorUnitAt(
			fixture.rootFD, testDebian13PDNSProfile(),
			func() {
				if renameErr := os.Rename(fixture.unitPath, old); renameErr != nil {
					t.Fatal(renameErr)
				}
				if writeErr := os.WriteFile(
					fixture.unitPath,
					[]byte(certifiedDebian13PDNSVendorUnit), 0o0644,
				); writeErr != nil {
					t.Fatal(writeErr)
				}
				mustChownMode(t, fixture.unitPath, 0, 0, 0o0644)
			},
		)
		if err == nil || !strings.Contains(err.Error(), "changed") {
			t.Fatalf("PowerDNS vendor TOCTOU error=%v", err)
		}
	})
}
