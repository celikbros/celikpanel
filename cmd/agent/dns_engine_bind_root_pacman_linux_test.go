//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

const testNamedGID = uint32(40)

func acceptTestPacmanOwnership() error { return nil }

func newPacmanBindRootFixture(t *testing.T, parentMode uint32) (string, int) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("exact BIND ownership tests require root")
	}
	root := t.TempDir()
	mustChownMode(t, root, 0, 0, bindManagedRootMode)
	varDirectory := filepath.Join(root, "var")
	if err := os.Mkdir(varDirectory, os.FileMode(bindManagedRootMode)); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, varDirectory, 0, 0, bindManagedRootMode)
	named := filepath.Join(varDirectory, "named")
	if err := os.Mkdir(named, 0o770); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, named, 0, int(testNamedGID), parentMode)
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return root, fd
}

// R-018 on a real Arch layout. The bind package ships /var/named as
// root:named 0770; the managed root must be created beneath it as root:root
// 0755, and the vendor parent must be hardened with the sticky bit so the
// named group cannot rename or unlink the managed root.
// Gerçek Arch yerleşiminde R-018. bind paketi /var/named'i root:named 0770
// gönderir; yönetilen kök onun altında root:root 0755 yaratılmalı ve named
// grubu yönetilen kökü yeniden adlandırıp silemesin diye satıcı üst dizini
// sticky bitiyle sertleştirilmelidir.
func TestEnsurePacmanBindGenerationRootCreatesExactManagedChildAndHardensParent(t *testing.T) {
	root, rootFD := newPacmanBindRootFixture(t, pacmanBINDStockVendorParentMode)
	if err := ensurePacmanBindGenerationRootAtWithMode(
		rootFD, testNamedGID, true, true, acceptTestPacmanOwnership,
	); err != nil {
		t.Fatal(err)
	}
	uid, gid, mode := statOwnershipMode(t, filepath.Join(root, "var", "named", "celikpanel"))
	if uid != 0 || gid != 0 || mode != bindManagedRootMode {
		t.Fatalf("managed child = %d:%d/%04o, want 0:0/0755", uid, gid, mode)
	}
	uid, gid, mode = statOwnershipMode(t, filepath.Join(root, "var", "named"))
	if uid != 0 || gid != testNamedGID || mode != pacmanBINDVendorParentMode {
		t.Fatalf("vendor parent = %d:%d/%04o, want 0:%d/1770", uid, gid, mode, testNamedGID)
	}
	if err := ensurePacmanBindGenerationRootAtWithMode(
		rootFD, testNamedGID, false, false, acceptTestPacmanOwnership,
	); err != nil {
		t.Fatalf("exact existing managed child rejected: %v", err)
	}
}

func TestEnsurePacmanBindGenerationRootDoesNotCreateOrHardenDuringVerification(t *testing.T) {
	root, rootFD := newPacmanBindRootFixture(t, pacmanBINDStockVendorParentMode)
	err := ensurePacmanBindGenerationRootAtWithMode(
		rootFD, testNamedGID, false, false, acceptTestPacmanOwnership,
	)
	if err == nil {
		t.Fatal("stock parent without the sticky bit was accepted during verification")
	}
	if _, statErr := os.Lstat(filepath.Join(root, "var", "named", "celikpanel")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("verification created managed child: %v", statErr)
	}
	_, _, mode := statOwnershipMode(t, filepath.Join(root, "var", "named"))
	if mode != pacmanBINDStockVendorParentMode {
		t.Fatalf("verification changed the vendor parent to %04o", mode)
	}
}

func TestEnsurePacmanBindGenerationRootRejectsUnsafeMetadataWithoutRepair(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "named-wrong-gid", mutate: func(t *testing.T, root string) {
			mustChownMode(t, filepath.Join(root, "var", "named"), 0, int(testNamedGID)+1, pacmanBINDVendorParentMode)
		}},
		{name: "named-wrong-uid", mutate: func(t *testing.T, root string) {
			mustChownMode(t, filepath.Join(root, "var", "named"), 1203, int(testNamedGID), pacmanBINDVendorParentMode)
		}},
		{name: "named-other-write", mutate: func(t *testing.T, root string) {
			if err := unix.Chmod(filepath.Join(root, "var", "named"), 0o1772); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "named-setgid", mutate: func(t *testing.T, root string) {
			if err := unix.Chmod(filepath.Join(root, "var", "named"), 0o3770); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "var-group-write", mutate: func(t *testing.T, root string) {
			if err := unix.Chmod(filepath.Join(root, "var"), 0o0775); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, rootFD := newPacmanBindRootFixture(t, pacmanBINDVendorParentMode)
			test.mutate(t, root)
			before, _, beforeMode := statOwnershipMode(t, filepath.Join(root, "var", "named"))
			if err := ensurePacmanBindGenerationRootAtWithMode(
				rootFD, testNamedGID, true, true, acceptTestPacmanOwnership,
			); err == nil {
				t.Fatal("unsafe metadata was accepted")
			}
			after, _, afterMode := statOwnershipMode(t, filepath.Join(root, "var", "named"))
			if before != after || beforeMode != afterMode {
				t.Fatalf("unsafe metadata was repaired: %d/%04o -> %d/%04o", before, beforeMode, after, afterMode)
			}
			if _, statErr := os.Lstat(filepath.Join(root, "var", "named", "celikpanel")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("managed child was created under unsafe metadata: %v", statErr)
			}
		})
	}
}

func TestEnsurePacmanBindGenerationRootRefusesWhenOwnershipProofFails(t *testing.T) {
	root, rootFD := newPacmanBindRootFixture(t, pacmanBINDStockVendorParentMode)
	err := ensurePacmanBindGenerationRootAtWithMode(
		rootFD, testNamedGID, true, true,
		func() error { return errors.New("/var/named is not the exact bind package-owned directory") },
	)
	if err == nil {
		t.Fatal("a parent that pacman does not attribute to bind was accepted")
	}
	_, _, mode := statOwnershipMode(t, filepath.Join(root, "var", "named"))
	if mode != pacmanBINDStockVendorParentMode {
		t.Fatalf("parent was hardened despite the failed ownership proof: %04o", mode)
	}
}

func TestClassifyExactPacmanBINDOwner(t *testing.T) {
	if err := classifyExactPacmanBINDOwner([]byte("/var/named/ is owned by bind 9.20.27-1\n"), nil); err != nil {
		t.Fatalf("exact owner line rejected: %v", err)
	}
	for name, output := range map[string]string{
		"other package": "/var/named/ is owned by unbound 1.0-1\n",
		"no newline":    "/var/named/ is owned by bind 9.20.27-1",
		"two lines":     "/var/named/ is owned by bind 9.20.27-1\n/var/named/ is owned by bind 9.20.27-1\n",
		"no version":    "/var/named/ is owned by bind \n",
		"unowned":       "error: No package owns /var/named\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := classifyExactPacmanBINDOwner([]byte(output), nil); err == nil {
				t.Fatalf("non-canonical owner output accepted: %q", output)
			}
		})
	}
	if err := classifyExactPacmanBINDOwner(nil, errors.New("exit status 1")); err == nil {
		t.Fatal("a failed pacman query was accepted")
	}
}

func TestResolveServiceGroupGIDIsExact(t *testing.T) {
	gid, err := resolveServiceGroupGIDWithRunner(
		context.Background(), "/usr/bin/getent", "named",
		func(context.Context, string, ...string) ([]byte, error) { return []byte("named:x:40:\n"), nil },
	)
	if err != nil || gid != 40 {
		t.Fatalf("gid=%d err=%v", gid, err)
	}
	for name, record := range map[string]string{
		"members":   "named:x:40:bind\n",
		"wrong":     "bind:x:40:\n",
		"zero":      "named:x:0:\n",
		"two lines": "named:x:40:\nnamed:x:41:\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveServiceGroupGIDWithRunner(
				context.Background(), "/usr/bin/getent", "named",
				func(context.Context, string, ...string) ([]byte, error) { return []byte(record), nil },
			); err == nil {
				t.Fatalf("unsafe group record accepted: %q", record)
			}
		})
	}
}
