//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

const aptBINDVendorUnitFixture = certifiedAPTBINDVendorUnit
const aptBINDVendorEnvironmentFixture = certifiedAPTBINDVendorEnvironment
const pacmanBINDVendorUnitFixture = certifiedPacmanBINDVendorUnit

func TestCertifiedAPTBINDVendorArtifactsMatchReviewedPackages(t *testing.T) {
	for _, artifact := range []struct {
		name       string
		data       string
		size       int
		wantSHA256 string
	}{
		{
			name: "named.service", data: certifiedAPTBINDVendorUnit, size: 376,
			wantSHA256: "ed631f7bfee5e9175e2d98511315cb877d1f91bbc118b79c74332c1008dfd4dd",
		},
		{
			name: "/etc/default/named", data: certifiedAPTBINDVendorEnvironment, size: 86,
			wantSHA256: "b825c0739a949b3dff55d2587d94df934a3a16384d5a9d0b1a2e0e969b8fca42",
		},
	} {
		if len(artifact.data) != artifact.size {
			t.Fatalf("%s size = %d, want %d", artifact.name, len(artifact.data), artifact.size)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(artifact.data))); got != artifact.wantSHA256 {
			t.Fatalf("%s SHA-256 = %s, want %s", artifact.name, got, artifact.wantSHA256)
		}
	}
}

func TestRealSystemdAPTAliasIsMaterializedOnlyByEnable(t *testing.T) {
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		t.Skipf("systemctl is unavailable: %v", err)
	}
	root := t.TempDir()
	for _, relative := range []string{
		"usr", "usr/lib", "usr/lib/systemd", "usr/lib/systemd/system",
		"etc", "etc/systemd", "etc/systemd/system",
	} {
		if err := os.Mkdir(
			filepath.Join(root, filepath.FromSlash(relative)), 0o755,
		); err != nil {
			t.Fatal(err)
		}
	}
	unit := filepath.Join(
		root, "usr", "lib", "systemd", "system", "named.service",
	)
	if err := os.WriteFile(unit, []byte(certifiedAPTBINDVendorUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(
		root, "etc", "systemd", "system", "bind9.service",
	)
	if _, err := os.Lstat(alias); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("APT alias unexpectedly existed before enable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx, systemctl, "--root="+root, "enable", "named.service",
	)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("hermetic systemctl enable failed: %v: %s", err, output)
	}
	target, err := os.Readlink(alias)
	if err != nil {
		t.Fatalf("APT alias was not materialized by enable: %v", err)
	}
	if target != "/usr/lib/systemd/system/named.service" {
		t.Fatalf("APT alias target=%q", target)
	}
	wants := filepath.Join(
		root, "etc", "systemd", "system",
		"multi-user.target.wants", "named.service",
	)
	wantsTarget, err := os.Readlink(wants)
	if err != nil || wantsTarget != "/usr/lib/systemd/system/named.service" {
		t.Fatalf("named enablement target=%q err=%v", wantsTarget, err)
	}
}

func TestAPTBINDVendorPackageOwnershipRequiresExactPackagePaths(t *testing.T) {
	want := map[string]string{
		"/usr/lib/systemd/system/named.service": "bind9: /usr/lib/systemd/system/named.service\n",
		"/etc/default/named":                    "bind9: /etc/default/named\n",
	}
	lookup := func(_ context.Context, path string) ([]byte, error) {
		return []byte(want[path]), nil
	}
	if err := verifyExactAPTBINDVendorPackageOwnership(
		context.Background(), lookup,
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/usr/lib/systemd/system/named.service", "/etc/default/named",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if err := verifyExactAPTBINDVendorPackageOwnership(
				context.Background(),
				func(_ context.Context, candidate string) ([]byte, error) {
					if candidate == path {
						return []byte("evil: " + candidate + "\n"), nil
					}
					return []byte(want[candidate]), nil
				},
			); err == nil {
				t.Fatal("wrong BIND vendor package owner was accepted")
			}
		})
	}
}

type bindVendorRootFixture struct {
	root        string
	rootFD      int
	unitPath    string
	environment string
}

func newBINDVendorRootFixture(
	t *testing.T,
	manager hostplatform.PackageManager,
) bindVendorRootFixture {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("exact BIND vendor file tests require root")
	}
	root := t.TempDir()
	mustChownMode(t, root, 0, 0, bindManagedRootMode)
	for _, relative := range []string{
		"usr", "usr/lib", "usr/lib/systemd", "usr/lib/systemd/system",
		"etc", "etc/default",
	} {
		candidate := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.Mkdir(candidate, os.FileMode(bindManagedRootMode)); err != nil {
			t.Fatal(err)
		}
		mustChownMode(t, candidate, 0, 0, bindManagedRootMode)
	}
	unitPath := filepath.Join(root, "usr", "lib", "systemd", "system", "named.service")
	unit := aptBINDVendorUnitFixture
	environmentPath := filepath.Join(root, "etc", "default", "named")
	if manager == hostplatform.PackageManagerPacman {
		unit = pacmanBINDVendorUnitFixture
		environmentPath = ""
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o0644); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, unitPath, 0, 0, 0o0644)
	if environmentPath != "" {
		if err := os.WriteFile(
			environmentPath, []byte(aptBINDVendorEnvironmentFixture), 0o0644,
		); err != nil {
			t.Fatal(err)
		}
		mustChownMode(t, environmentPath, 0, 0, 0o0644)
	}
	rootFD, err := unix.Open(
		root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(rootFD) })
	return bindVendorRootFixture{
		root: root, rootFD: rootFD, unitPath: unitPath, environment: environmentPath,
	}
}

func TestInspectBINDVendorFilesAtAcceptsCertifiedAPTAndPacmanUnits(t *testing.T) {
	for _, manager := range []hostplatform.PackageManager{
		hostplatform.PackageManagerAPT,
		hostplatform.PackageManagerPacman,
	} {
		t.Run(string(manager), func(t *testing.T) {
			fixture := newBINDVendorRootFixture(t, manager)
			profile := testPacmanBINDProfile()
			if manager == hostplatform.PackageManagerAPT {
				profile = testUbuntuBINDProfile()
			}
			identity, err := inspectBINDVendorFilesAt(
				fixture.rootFD, profile, nil,
			)
			if err != nil {
				t.Fatalf("certified vendor files rejected: %v", err)
			}
			if identity.Unit.Inode == 0 || identity.Unit.Size == 0 {
				t.Fatalf("unit identity = %#v", identity.Unit)
			}
			if manager == hostplatform.PackageManagerAPT &&
				(identity.Environment.Inode == 0 || identity.Environment.Size == 0) {
				t.Fatalf("environment identity = %#v", identity.Environment)
			}
		})
	}
}

func TestVerifyExactPacmanBINDVendorPackageOwnership(t *testing.T) {
	var paths []string
	err := verifyExactPacmanBINDVendorPackageOwnership(
		context.Background(),
		func(_ context.Context, path string) ([]byte, error) {
			paths = append(paths, path)
			return []byte("bind\n"), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"/usr/lib/systemd/system/named.service"}) {
		t.Fatalf("package owner paths = %#v", paths)
	}
	for _, output := range []string{"", "bind 9.20.9-1\n", "evil\n", "bind\r\n"} {
		err := verifyExactPacmanBINDVendorPackageOwnership(
			context.Background(),
			func(context.Context, string) ([]byte, error) {
				return []byte(output), nil
			},
		)
		if err == nil {
			t.Fatalf("noncanonical pacman ownership output %q accepted", output)
		}
	}
}

func TestInspectBINDVendorFilesAtRejectsUnsafeLeafMetadata(t *testing.T) {
	for _, test := range []struct {
		name   string
		target func(bindVendorRootFixture) string
		mutate func(*testing.T, string)
	}{
		{name: "unit-wrong-uid", target: func(f bindVendorRootFixture) string { return f.unitPath }, mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 1201, 0, 0o0644)
		}},
		{name: "unit-wrong-gid", target: func(f bindVendorRootFixture) string { return f.unitPath }, mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 1202, 0o0644)
		}},
		{name: "unit-group-write", target: func(f bindVendorRootFixture) string { return f.unitPath }, mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 0, 0o0664)
		}},
		{name: "unit-setuid", target: func(f bindVendorRootFixture) string { return f.unitPath }, mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 0, 0o4644)
		}},
		{name: "unit-sticky", target: func(f bindVendorRootFixture) string { return f.unitPath }, mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 0, 0o1644)
		}},
		{name: "unit-hardlink", target: func(f bindVendorRootFixture) string { return f.unitPath }, mutate: func(t *testing.T, path string) {
			if err := os.Link(path, path+".extra"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "environment-wrong-mode", target: func(f bindVendorRootFixture) string { return f.environment }, mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 0, 0, 0o0600)
		}},
		{name: "environment-hardlink", target: func(f bindVendorRootFixture) string { return f.environment }, mutate: func(t *testing.T, path string) {
			if err := os.Link(path, path+".extra"); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBINDVendorRootFixture(t, hostplatform.PackageManagerAPT)
			test.mutate(t, test.target(fixture))
			if _, err := inspectBINDVendorFilesAt(
				fixture.rootFD,
				testUbuntuBINDProfile(), nil,
			); err == nil {
				t.Fatal("unsafe vendor file metadata was accepted")
			}
		})
	}
}

func TestInspectBINDVendorFilesAtRejectsSymlinkedLeafAndParent(t *testing.T) {
	t.Run("leaf", func(t *testing.T) {
		fixture := newBINDVendorRootFixture(t, hostplatform.PackageManagerAPT)
		real := fixture.unitPath + ".real"
		if err := os.Rename(fixture.unitPath, real); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Base(real), fixture.unitPath); err != nil {
			t.Fatal(err)
		}
		if _, err := inspectBINDVendorFilesAt(
			fixture.rootFD,
			testUbuntuBINDProfile(), nil,
		); err == nil {
			t.Fatal("symlinked vendor unit was accepted")
		}
	})
	t.Run("parent", func(t *testing.T) {
		fixture := newBINDVendorRootFixture(t, hostplatform.PackageManagerAPT)
		parent := filepath.Join(fixture.root, "usr", "lib", "systemd", "system")
		real := parent + "-real"
		if err := os.Rename(parent, real); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Base(real), parent); err != nil {
			t.Fatal(err)
		}
		if _, err := inspectBINDVendorFilesAt(
			fixture.rootFD,
			testUbuntuBINDProfile(), nil,
		); err == nil {
			t.Fatal("symlinked vendor parent was accepted")
		}
	})
}

func TestInspectBINDVendorFilesAtRejectsUnsafeDirectivesAndEnvironment(t *testing.T) {
	for _, test := range []struct {
		name   string
		target func(bindVendorRootFixture) string
		alter  func(string) string
	}{
		{name: "exec-config-override", target: func(f bindVendorRootFixture) string { return f.unitPath }, alter: func(value string) string {
			return strings.Replace(value, "ExecStart=/usr/sbin/named -f $OPTIONS", "ExecStart=/usr/sbin/named -f -c /tmp/evil.conf", 1)
		}},
		{name: "extra-service-directive", target: func(f bindVendorRootFixture) string { return f.unitPath }, alter: func(value string) string {
			return strings.Replace(value, "Restart=on-failure", "Restart=on-failure\nRootDirectory=/tmp/evil", 1)
		}},
		{name: "environment-config-override", target: func(f bindVendorRootFixture) string { return f.environment }, alter: func(value string) string {
			return strings.Replace(value, "OPTIONS=\"-u bind\"", "OPTIONS=\"-u bind -c /tmp/evil.conf\"", 1)
		}},
		{name: "environment-shell-expansion", target: func(f bindVendorRootFixture) string { return f.environment }, alter: func(value string) string {
			return strings.Replace(value, "OPTIONS=\"-u bind\"", "OPTIONS=\"$(/tmp/evil)\"", 1)
		}},
		{name: "environment-duplicate", target: func(f bindVendorRootFixture) string { return f.environment }, alter: func(value string) string {
			return value + "OPTIONS=\"-u bind\"\n"
		}},
		{name: "unit-leading-nbsp", target: func(f bindVendorRootFixture) string { return f.unitPath }, alter: func(value string) string {
			return strings.Replace(value, "EnvironmentFile=", "\u00a0EnvironmentFile=", 1)
		}},
		{name: "unit-leading-vt", target: func(f bindVendorRootFixture) string { return f.unitPath }, alter: func(value string) string {
			return strings.Replace(value, "ExecStart=", "\vExecStart=", 1)
		}},
		{name: "unit-trailing-ff", target: func(f bindVendorRootFixture) string { return f.unitPath }, alter: func(value string) string {
			return strings.Replace(value, "Type=notify\n", "Type=notify\f\n", 1)
		}},
		{name: "environment-leading-nbsp", target: func(f bindVendorRootFixture) string { return f.environment }, alter: func(value string) string {
			return strings.Replace(value, "OPTIONS=", "\u00a0OPTIONS=", 1)
		}},
		{name: "environment-leading-vt", target: func(f bindVendorRootFixture) string { return f.environment }, alter: func(value string) string {
			return strings.Replace(value, "RESOLVCONF=", "\vRESOLVCONF=", 1)
		}},
		{name: "environment-trailing-ff", target: func(f bindVendorRootFixture) string { return f.environment }, alter: func(value string) string {
			return strings.Replace(value, "OPTIONS=\"-u bind\"\n", "OPTIONS=\"-u bind\"\f\n", 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBINDVendorRootFixture(t, hostplatform.PackageManagerAPT)
			target := test.target(fixture)
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte(test.alter(string(data))), 0o0644); err != nil {
				t.Fatal(err)
			}
			mustChownMode(t, target, 0, 0, 0o0644)
			if _, err := inspectBINDVendorFilesAt(
				fixture.rootFD,
				testUbuntuBINDProfile(), nil,
			); err == nil {
				t.Fatal("unsafe vendor semantics were accepted")
			}
		})
	}
}

func TestInspectBINDVendorFilesAtDetectsTOCTOUReplacement(t *testing.T) {
	fixture := newBINDVendorRootFixture(t, hostplatform.PackageManagerAPT)
	old := fixture.unitPath + ".old"
	_, err := inspectBINDVendorFilesAt(
		fixture.rootFD,
		testUbuntuBINDProfile(),
		func() {
			if renameErr := os.Rename(fixture.unitPath, old); renameErr != nil {
				t.Fatal(renameErr)
			}
			if writeErr := os.WriteFile(
				fixture.unitPath, []byte(aptBINDVendorUnitFixture), 0o0644,
			); writeErr != nil {
				t.Fatal(writeErr)
			}
			mustChownMode(t, fixture.unitPath, 0, 0, 0o0644)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("vendor file TOCTOU error = %v", err)
	}
}

func TestInspectBINDVendorFilesAtRejectsExtendedACL(t *testing.T) {
	fixture := newBINDVendorRootFixture(t, hostplatform.PackageManagerAPT)
	acl := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(acl[0:4], 2)
	entries := []struct {
		tag  uint16
		perm uint16
		id   uint32
	}{
		{tag: 0x01, perm: 6, id: ^uint32(0)},
		{tag: 0x02, perm: 4, id: 1205},
		{tag: 0x04, perm: 4, id: ^uint32(0)},
		{tag: 0x10, perm: 4, id: ^uint32(0)},
		{tag: 0x20, perm: 4, id: ^uint32(0)},
	}
	for index, entry := range entries {
		offset := 4 + index*8
		binary.LittleEndian.PutUint16(acl[offset:offset+2], entry.tag)
		binary.LittleEndian.PutUint16(acl[offset+2:offset+4], entry.perm)
		binary.LittleEndian.PutUint32(acl[offset+4:offset+8], entry.id)
	}
	if err := unix.Setxattr(fixture.unitPath, "system.posix_acl_access", acl, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
			t.Skipf("filesystem does not support POSIX ACL xattrs: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := inspectBINDVendorFilesAt(
		fixture.rootFD,
		testUbuntuBINDProfile(), nil,
	); err == nil || !strings.Contains(err.Error(), "POSIX ACL") {
		t.Fatalf("extended ACL error = %v", err)
	}
}
