//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func requireRootPDNSConfigTest(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("exact PowerDNS owner tests require root")
	}
}

func makePDNSConfigTestRoot(t *testing.T) string {
	t.Helper()
	requireRootPDNSConfigTest(t)
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "etc"),
		filepath.Join(root, "etc", "powerdns"),
		filepath.Join(root, "etc", "powerdns", "pdns.d"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func openPDNSConfigTestRoot(t *testing.T, root string) int {
	t.Helper()
	fd, err := unix.Open(
		root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	return fd
}

func openPDNSConfigTestParent(
	t *testing.T,
	root string,
	components ...string,
) *pdnsConfigParentHandle {
	t.Helper()
	handle, err := openPDNSConfigParentAt(
		openPDNSConfigTestRoot(t, root), components,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handle.close)
	return handle
}

func writePDNSConfigTestFile(
	t *testing.T,
	path string,
	data string,
	mode os.FileMode,
	gid int,
) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, gid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxPDNSConfigParentRejectsUnsafeDirectoryAndSymlink(t *testing.T) {
	t.Run("world-writable-parent", func(t *testing.T) {
		root := makePDNSConfigTestRoot(t)
		unsafeParent := filepath.Join(root, "etc", "powerdns")
		if err := os.Chmod(unsafeParent, 0o777); err != nil {
			t.Fatal(err)
		}
		handle, err := openPDNSConfigParentAt(
			openPDNSConfigTestRoot(t, root), []string{"etc", "powerdns"},
		)
		if handle != nil {
			handle.close()
		}
		if err == nil || !strings.Contains(err.Error(), "root:root 0755") {
			t.Fatalf("unsafe parent accepted: %v", err)
		}
	})

	t.Run("symlinked-parent", func(t *testing.T) {
		requireRootPDNSConfigTest(t)
		root := t.TempDir()
		realParent := filepath.Join(root, "real")
		if err := os.MkdirAll(filepath.Join(realParent, "powerdns"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", filepath.Join(root, "etc")); err != nil {
			t.Fatal(err)
		}
		handle, err := openPDNSConfigParentAt(
			openPDNSConfigTestRoot(t, root), []string{"etc", "powerdns"},
		)
		if handle != nil {
			handle.close()
		}
		if err == nil {
			t.Fatal("symlinked parent accepted")
		}
	})

	t.Run("unsafe-component", func(t *testing.T) {
		root := makePDNSConfigTestRoot(t)
		handle, err := openPDNSConfigParentAt(
			openPDNSConfigTestRoot(t, root), []string{"etc", "..", "etc"},
		)
		if handle != nil {
			handle.close()
		}
		if err == nil {
			t.Fatal("unsafe parent component accepted")
		}
	})
}

func TestLinuxPDNSConfigCaptureAcceptsExactInstalledOwners(t *testing.T) {
	for _, test := range []struct {
		name string
		gid  int
	}{
		{name: "Ubuntu-root-pdns", gid: 109},
		{name: "root-root-compatible", gid: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := makePDNSConfigTestRoot(t)
			mainPath := filepath.Join(root, "etc", "powerdns", "pdns.conf")
			writePDNSConfigTestFile(
				t, mainPath, "# stock PowerDNS configuration\n", 0o640, test.gid,
			)
			handle := openPDNSConfigTestParent(t, root, "etc", "powerdns")
			observation, err := capturePDNSConfigObservationAtParent(
				handle, dnsMainConf, false,
				pdnsConfigOwnerPolicy{pdnsGID: 109}, nil,
			)
			if err != nil {
				t.Fatalf("installed owner rejected: %v", err)
			}
			if observation.Snapshot.Mode != 0o640 ||
				observation.Snapshot.UID != 0 ||
				observation.Snapshot.GID != uint32(test.gid) {
				t.Fatalf("installed metadata changed: %#v", observation.Snapshot)
			}
		})
	}
}

func TestLinuxPDNSConfigCaptureRejectsForeignGIDSymlinkAndTOCTOU(t *testing.T) {
	t.Run("foreign-gid", func(t *testing.T) {
		root := makePDNSConfigTestRoot(t)
		mainPath := filepath.Join(root, "etc", "powerdns", "pdns.conf")
		writePDNSConfigTestFile(t, mainPath, "# stock\n", 0o640, 110)
		handle := openPDNSConfigTestParent(t, root, "etc", "powerdns")
		_, err := capturePDNSConfigObservationAtParent(
			handle, dnsMainConf, false,
			pdnsConfigOwnerPolicy{pdnsGID: 109}, nil,
		)
		if err == nil || !strings.Contains(err.Error(), "neither root nor") {
			t.Fatalf("foreign config group accepted: %v", err)
		}
	})

	t.Run("symlinked-file", func(t *testing.T) {
		root := makePDNSConfigTestRoot(t)
		target := filepath.Join(root, "target.conf")
		writePDNSConfigTestFile(t, target, "# target\n", 0o640, 109)
		mainPath := filepath.Join(root, "etc", "powerdns", "pdns.conf")
		if err := os.Symlink(target, mainPath); err != nil {
			t.Fatal(err)
		}
		handle := openPDNSConfigTestParent(t, root, "etc", "powerdns")
		_, err := capturePDNSConfigObservationAtParent(
			handle, dnsMainConf, false,
			pdnsConfigOwnerPolicy{pdnsGID: 109}, nil,
		)
		if err == nil {
			t.Fatal("symlinked config accepted")
		}
	})

	t.Run("file-changed-after-first-fstat", func(t *testing.T) {
		root := makePDNSConfigTestRoot(t)
		mainPath := filepath.Join(root, "etc", "powerdns", "pdns.conf")
		writePDNSConfigTestFile(t, mainPath, "# before\n", 0o640, 109)
		handle := openPDNSConfigTestParent(t, root, "etc", "powerdns")
		_, err := capturePDNSConfigObservationAtParent(
			handle, dnsMainConf, false,
			pdnsConfigOwnerPolicy{pdnsGID: 109},
			func() {
				writePDNSConfigTestFile(
					t, mainPath, "# changed after first descriptor stat\n", 0o640, 109,
				)
			},
		)
		if err == nil || !strings.Contains(err.Error(), "changed while") {
			t.Fatalf("in-place TOCTOU accepted: %v", err)
		}
	})

	t.Run("descriptor-replaced-after-first-fstat", func(t *testing.T) {
		root := makePDNSConfigTestRoot(t)
		mainPath := filepath.Join(root, "etc", "powerdns", "pdns.conf")
		replacement := filepath.Join(root, "etc", "powerdns", "replacement")
		writePDNSConfigTestFile(t, mainPath, "# before\n", 0o640, 109)
		writePDNSConfigTestFile(t, replacement, "# replacement\n", 0o640, 109)
		handle := openPDNSConfigTestParent(t, root, "etc", "powerdns")
		_, err := capturePDNSConfigObservationAtParent(
			handle, dnsMainConf, false,
			pdnsConfigOwnerPolicy{pdnsGID: 109},
			func() {
				if renameErr := os.Rename(replacement, mainPath); renameErr != nil {
					t.Fatal(renameErr)
				}
			},
		)
		if err == nil || !strings.Contains(err.Error(), "changed while") {
			t.Fatalf("path replacement TOCTOU accepted: %v", err)
		}
	})
}
