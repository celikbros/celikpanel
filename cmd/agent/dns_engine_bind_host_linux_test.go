//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

const testBINDGID = uint32(23456)

func acceptTestAPTBindDurability(uint32) error { return nil }

func TestResolveBINDGroupGIDIsExactAndDeadlineBound(t *testing.T) {
	got, err := resolveBINDGroupGIDWithRunner(
		context.Background(), "/usr/bin/getent",
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "/usr/bin/getent" ||
				!reflect.DeepEqual(args, []string{"group", "bind"}) {
				t.Fatalf("getent command = %s %#v", name, args)
			}
			return []byte("bind:x:109:\n"), nil
		},
	)
	if err != nil || got != 109 {
		t.Fatalf("gid=%d err=%v", got, err)
	}
	for _, output := range []string{
		"bind:x:0109:\n", "bind:x:109:root\n", "bind:*:109:\n",
		"bind:x:109:\nextra:x:110:\n", " bind:x:109:\n",
		"bind:x:2147483648:\n", "bind:x:4294967295:\n",
	} {
		if _, err := resolveBINDGroupGIDWithRunner(
			context.Background(), "/usr/bin/getent",
			func(context.Context, string, ...string) ([]byte, error) {
				return []byte(output), nil
			},
		); err == nil {
			t.Fatalf("unsafe BIND group record accepted: %q", output)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = resolveBINDGroupGIDWithRunner(
		ctx, "/usr/bin/getent",
		func(commandCtx context.Context, _ string, _ ...string) ([]byte, error) {
			<-commandCtx.Done()
			return nil, commandCtx.Err()
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		time.Since(started) > time.Second {
		t.Fatalf("BIND group proof ignored caller deadline: %v", err)
	}
}

type testBINDExitError int

func (testBINDExitError) Error() string   { return "forced exit status" }
func (e testBINDExitError) ExitCode() int { return int(e) }

func exactTestAPTBindStatOverrideOps(
	lists ...struct {
		output []byte
		err    error
	},
) (aptBINDStatOverrideOps, *int) {
	listIndex := 0
	addCalls := 0
	return aptBINDStatOverrideOps{
		owner: func() ([]byte, error) {
			return []byte(aptBINDExactPackageOwnerLine), nil
		},
		list: func() ([]byte, error) {
			if listIndex >= len(lists) {
				return nil, errors.New("unexpected statoverride list call")
			}
			result := lists[listIndex]
			listIndex++
			return result.output, result.err
		},
		add: func() ([]byte, error) {
			addCalls++
			return nil, nil
		},
	}, &addCalls
}

func TestAPTBindStatOverrideCommandContract(t *testing.T) {
	environment := aptBINDStatOverrideCommandEnvironment()
	wantEnvironment := []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LC_ALL=C",
	}
	if !reflect.DeepEqual(environment, wantEnvironment) {
		t.Fatalf("statoverride environment = %#v, want %#v", environment, wantEnvironment)
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "DPKG_") {
			t.Fatalf("unsafe inherited dpkg control = %q", entry)
		}
	}
	type invocation struct {
		name string
		args []string
	}
	var calls []invocation
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ops, err := aptBINDStatOverrideOperations(
		ctx,
		"/usr/sbin/dpkg-statoverride",
		"/usr/bin/dpkg-query",
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			if deadline, ok := commandCtx.Deadline(); !ok || time.Until(deadline) <= 0 {
				t.Fatal("dpkg durability command lacks a live deadline")
			}
			calls = append(calls, invocation{name: name, args: append([]string(nil), args...)})
			return nil, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = ops.owner()
	_, _ = ops.list()
	_, _ = ops.add()
	want := []invocation{
		{name: "/usr/bin/dpkg-query", args: []string{"-S", "--", "/var/cache/bind"}},
		{name: "/usr/sbin/dpkg-statoverride", args: []string{"--list", "/var/cache/bind"}},
		{name: "/usr/sbin/dpkg-statoverride", args: []string{"--no-force-statoverride-add", "--add", "root", "bind", "1775", "/var/cache/bind"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("dpkg durability calls = %#v, want %#v", calls, want)
	}
	for _, call := range calls {
		for _, argument := range call.args {
			if argument == "--force" || argument == "--update" ||
				argument == "--force-statoverride-add" {
				t.Fatalf("unsafe dpkg argument in %#v", call)
			}
		}
	}
}

func TestClassifyExactAPTBindStatOverride(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		err    error
		state  aptBINDStatOverrideListState
		ok     bool
	}{
		{name: "exact", output: aptBINDExactStatOverrideLine, state: aptBINDStatOverrideExact, ok: true},
		{name: "absent", err: testBINDExitError(1), state: aptBINDStatOverrideAbsent, ok: true},
		{name: "empty-success"},
		{name: "empty-exit-two", err: testBINDExitError(2)},
		{name: "output-exit-one", output: aptBINDExactStatOverrideLine, err: testBINDExitError(1)},
		{name: "missing-newline", output: strings.TrimSuffix(aptBINDExactStatOverrideLine, "\n")},
		{name: "wrong-owner", output: "daemon bind 1775 /var/cache/bind\n"},
		{name: "wrong-group", output: "root root 1775 /var/cache/bind\n"},
		{name: "wrong-mode", output: "root bind 0775 /var/cache/bind\n"},
		{name: "wrong-path", output: "root bind 1775 /tmp/bind\n"},
		{name: "multiple", output: aptBINDExactStatOverrideLine + "root bind 1775 /tmp/bind\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, err := classifyExactAPTBindStatOverride([]byte(test.output), test.err)
			if (err == nil) != test.ok {
				t.Fatalf("classify error = %v, want ok=%v", err, test.ok)
			}
			if err == nil && state != test.state {
				t.Fatalf("state = %v, want %v", state, test.state)
			}
		})
	}
}

func TestVerifyOrCreateExactAPTBindStatOverrideLifecycle(t *testing.T) {
	exactList := struct {
		output []byte
		err    error
	}{output: []byte(aptBINDExactStatOverrideLine)}
	absentList := struct {
		output []byte
		err    error
	}{err: testBINDExitError(1)}

	for _, mode := range []uint32{
		aptBINDStockCacheParentMode,
		aptBINDCacheParentMode,
	} {
		t.Run("exact-idempotent-"+strconv.FormatUint(uint64(mode), 8), func(t *testing.T) {
			ops, addCalls := exactTestAPTBindStatOverrideOps(exactList)
			if err := verifyOrCreateExactAPTBindStatOverride(true, mode, ops); err != nil {
				t.Fatalf("exact override rejected: %v", err)
			}
			if *addCalls != 0 {
				t.Fatalf("exact override re-added %d times", *addCalls)
			}
		})
		t.Run("absent-create-"+strconv.FormatUint(uint64(mode), 8), func(t *testing.T) {
			ops, addCalls := exactTestAPTBindStatOverrideOps(absentList, exactList)
			if err := verifyOrCreateExactAPTBindStatOverride(true, mode, ops); err != nil {
				t.Fatalf("absent override was not created: %v", err)
			}
			if *addCalls != 1 {
				t.Fatalf("add calls = %d, want 1", *addCalls)
			}
		})
	}

	t.Run("verify-only-absent", func(t *testing.T) {
		ops, addCalls := exactTestAPTBindStatOverrideOps(absentList)
		if err := verifyOrCreateExactAPTBindStatOverride(
			false, aptBINDCacheParentMode, ops,
		); err == nil {
			t.Fatal("verify-only accepted an absent override")
		}
		if *addCalls != 0 {
			t.Fatal("verify-only attempted to add an override")
		}
	})
	t.Run("conflicting", func(t *testing.T) {
		conflict := exactList
		conflict.output = []byte("root root 1775 /var/cache/bind\n")
		ops, addCalls := exactTestAPTBindStatOverrideOps(conflict)
		if err := verifyOrCreateExactAPTBindStatOverride(
			true, aptBINDCacheParentMode, ops,
		); err == nil {
			t.Fatal("conflicting override was accepted")
		}
		if *addCalls != 0 {
			t.Fatal("conflicting override was overwritten")
		}
	})
	t.Run("package-owner-mismatch", func(t *testing.T) {
		ops, addCalls := exactTestAPTBindStatOverrideOps(exactList)
		ops.owner = func() ([]byte, error) {
			return []byte("local-admin: /var/cache/bind\n"), nil
		}
		if err := verifyOrCreateExactAPTBindStatOverride(
			true, aptBINDCacheParentMode, ops,
		); err == nil {
			t.Fatal("non-bind9 package ownership was accepted")
		}
		if *addCalls != 0 {
			t.Fatal("override was added to a non-bind9 path")
		}
	})
	t.Run("package-owner-command-failure", func(t *testing.T) {
		ops, _ := exactTestAPTBindStatOverrideOps(exactList)
		ops.owner = func() ([]byte, error) { return nil, errors.New("query failed") }
		if err := verifyOrCreateExactAPTBindStatOverride(
			true, aptBINDCacheParentMode, ops,
		); err == nil {
			t.Fatal("unverified package ownership was accepted")
		}
	})
	t.Run("add-failure-exact-readback", func(t *testing.T) {
		ops, _ := exactTestAPTBindStatOverrideOps(absentList, exactList)
		ops.add = func() ([]byte, error) { return nil, errors.New("uncertain add") }
		if err := verifyOrCreateExactAPTBindStatOverride(
			true, aptBINDStockCacheParentMode, ops,
		); err != nil {
			t.Fatalf("exact committed readback did not reconcile add failure: %v", err)
		}
	})
	t.Run("add-failure-absent-readback", func(t *testing.T) {
		ops, _ := exactTestAPTBindStatOverrideOps(absentList, absentList)
		ops.add = func() ([]byte, error) { return nil, errors.New("failed add") }
		if err := verifyOrCreateExactAPTBindStatOverride(
			true, aptBINDStockCacheParentMode, ops,
		); err == nil {
			t.Fatal("failed absent add was accepted")
		}
	})
	t.Run("unexpected-add-output", func(t *testing.T) {
		ops, _ := exactTestAPTBindStatOverrideOps(absentList, exactList)
		ops.add = func() ([]byte, error) { return []byte("warning\n"), nil }
		if err := verifyOrCreateExactAPTBindStatOverride(
			true, aptBINDStockCacheParentMode, ops,
		); err == nil {
			t.Fatal("unexpected add output was accepted")
		}
	})
	t.Run("readback-conflict", func(t *testing.T) {
		conflict := exactList
		conflict.output = []byte("root root 1775 /var/cache/bind\n")
		ops, _ := exactTestAPTBindStatOverrideOps(absentList, conflict)
		if err := verifyOrCreateExactAPTBindStatOverride(
			true, aptBINDStockCacheParentMode, ops,
		); err == nil {
			t.Fatal("conflicting add readback was accepted")
		}
	})
	t.Run("unsupported-parent-mode", func(t *testing.T) {
		ownerCalls := 0
		ops := aptBINDStatOverrideOps{
			owner: func() ([]byte, error) {
				ownerCalls++
				return []byte(aptBINDExactPackageOwnerLine), nil
			},
			list: func() ([]byte, error) { return []byte(aptBINDExactStatOverrideLine), nil },
			add:  func() ([]byte, error) { return nil, nil },
		}
		if err := verifyOrCreateExactAPTBindStatOverride(true, 0o0755, ops); err == nil {
			t.Fatal("unsupported parent mode was accepted")
		}
		if ownerCalls != 0 {
			t.Fatal("durability commands ran for unsafe parent metadata")
		}
	})
}

func newAPTBindRootFixture(t *testing.T) (string, int) {
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
	cache := filepath.Join(varDirectory, "cache")
	if err := os.Mkdir(cache, os.FileMode(bindManagedRootMode)); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, cache, 0, 0, bindManagedRootMode)
	bind := filepath.Join(cache, "bind")
	if err := os.Mkdir(bind, os.FileMode(aptBINDCacheParentMode)); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, bind, 0, int(testBINDGID), aptBINDCacheParentMode)
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return root, fd
}

func mustChownMode(t *testing.T, path string, uid, gid int, mode uint32) {
	t.Helper()
	if err := os.Chown(path, uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func statOwnershipMode(t *testing.T, path string) (uint32, uint32, uint32) {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		t.Fatal(err)
	}
	return stat.Uid, stat.Gid, stat.Mode & bindDirectoryModeMask
}

func TestEnsureAPTBindGenerationRootCreatesExactManagedChild(t *testing.T) {
	root, rootFD := newAPTBindRootFixture(t)
	if err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, true, acceptTestAPTBindDurability, nil,
	); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "var", "cache", "bind", "celikpanel")
	uid, gid, mode := statOwnershipMode(t, child)
	if uid != 0 || gid != 0 || mode != bindManagedRootMode {
		t.Fatalf("managed child = %d:%d/%04o, want 0:0/0755", uid, gid, mode)
	}
	bind := filepath.Join(root, "var", "cache", "bind")
	uid, gid, mode = statOwnershipMode(t, bind)
	if uid != 0 || gid != testBINDGID || mode != aptBINDCacheParentMode {
		t.Fatalf("external BIND parent changed = %d:%d/%04o", uid, gid, mode)
	}
	if err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, false, acceptTestAPTBindDurability, nil,
	); err != nil {
		t.Fatalf("exact existing managed child rejected: %v", err)
	}
}

func TestEnsureAPTBindGenerationRootDoesNotCreateDuringVerification(t *testing.T) {
	root, rootFD := newAPTBindRootFixture(t)
	err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, false, acceptTestAPTBindDurability, nil,
	)
	if err == nil {
		t.Fatal("absent managed child was accepted during verification")
	}
	if _, statErr := os.Lstat(filepath.Join(root, "var", "cache", "bind", "celikpanel")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("verification created managed child: %v", statErr)
	}
}

func TestHostBINDGenerationRootRejectsAbandonedUnreleasedPath(t *testing.T) {
	err := accessHostBINDGenerationRoot(context.Background(), bindHostLayout{
		GenerationRoot: abandonedAPTBindGenerationRoot,
	}, true)
	if !errors.Is(err, errBINDAbandonedGenerationRoot) {
		t.Fatalf("abandoned root error = %v", err)
	}
}

func TestEnsureAPTBindGenerationRootRejectsUnsafeMetadataWithoutRepair(t *testing.T) {
	tests := []struct {
		name   string
		target func(string) string
		mutate func(*testing.T, string)
	}{
		{name: "root-wrong-uid", target: func(root string) string { return root }, mutate: func(t *testing.T, path string) {
			uid, gid, mode := statOwnershipMode(t, path)
			mustChownMode(t, path, int(uid)+1200, int(gid), mode)
		}},
		{name: "var-wrong-gid", target: func(root string) string { return filepath.Join(root, "var") }, mutate: func(t *testing.T, path string) {
			uid, _, mode := statOwnershipMode(t, path)
			mustChownMode(t, path, int(uid), 1201, mode)
		}},
		{name: "var-group-write", target: func(root string) string { return filepath.Join(root, "var") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o0775); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "cache-wrong-uid", target: func(root string) string { return filepath.Join(root, "var", "cache") }, mutate: func(t *testing.T, path string) {
			_, gid, mode := statOwnershipMode(t, path)
			mustChownMode(t, path, 1202, int(gid), mode)
		}},
		{name: "cache-other-write", target: func(root string) string { return filepath.Join(root, "var", "cache") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o0757); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "bind-wrong-uid", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind") }, mutate: func(t *testing.T, path string) {
			mustChownMode(t, path, 1203, int(testBINDGID), aptBINDCacheParentMode)
		}},
		{name: "bind-wrong-gid", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind") }, mutate: func(t *testing.T, path string) { mustChownMode(t, path, 0, int(testBINDGID)+1, aptBINDCacheParentMode) }},
		{name: "bind-missing-sticky", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o0755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "bind-other-write", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o1777); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "bind-setuid", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o5775); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "bind-setgid", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o3775); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "child-wrong-uid", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind", "celikpanel") }, mutate: func(t *testing.T, path string) { mustChownMode(t, path, 1204, 0, bindManagedRootMode) }},
		{name: "child-wrong-gid", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind", "celikpanel") }, mutate: func(t *testing.T, path string) { mustChownMode(t, path, 0, 1205, bindManagedRootMode) }},
		{name: "child-group-write", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind", "celikpanel") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o0775); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "child-setuid", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind", "celikpanel") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o4755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "child-setgid", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind", "celikpanel") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o2755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "child-sticky", target: func(root string) string { return filepath.Join(root, "var", "cache", "bind", "celikpanel") }, mutate: func(t *testing.T, path string) {
			if err := unix.Chmod(path, 0o1755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, rootFD := newAPTBindRootFixture(t)
			child := filepath.Join(root, "var", "cache", "bind", "celikpanel")
			if err := os.Mkdir(child, os.FileMode(bindManagedRootMode)); err != nil {
				t.Fatal(err)
			}
			mustChownMode(t, child, 0, 0, bindManagedRootMode)
			target := test.target(root)
			test.mutate(t, target)
			beforeUID, beforeGID, beforeMode := statOwnershipMode(t, target)
			if err := ensureAPTBindGenerationRootAt(
				rootFD, testBINDGID, true, acceptTestAPTBindDurability, nil,
			); err == nil {
				t.Fatal("unsafe BIND directory metadata was accepted")
			}
			afterUID, afterGID, afterMode := statOwnershipMode(t, target)
			if beforeUID != afterUID || beforeGID != afterGID || beforeMode != afterMode {
				t.Fatalf("unsafe existing path was repaired: before=%d:%d/%04o after=%d:%d/%04o", beforeUID, beforeGID, beforeMode, afterUID, afterGID, afterMode)
			}
		})
	}
}

func TestEnsureAPTBindGenerationRootUpgradesOnlyExactStockParent(t *testing.T) {
	root, rootFD := newAPTBindRootFixture(t)
	bind := filepath.Join(root, "var", "cache", "bind")
	mustChownMode(t, bind, 0, int(testBINDGID), aptBINDStockCacheParentMode)
	var durabilityModes []uint32
	proveDurability := func(mode uint32) error {
		durabilityModes = append(durabilityModes, mode)
		return nil
	}
	if err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, true, proveDurability, nil,
	); err != nil {
		t.Fatalf("exact stock parent was not hardened: %v", err)
	}
	uid, gid, mode := statOwnershipMode(t, bind)
	if uid != 0 || gid != testBINDGID || mode != aptBINDCacheParentMode {
		t.Fatalf("hardened parent = %d:%d/%04o, want 0:%d/1775", uid, gid, mode, testBINDGID)
	}
	child := filepath.Join(bind, "celikpanel")
	uid, gid, mode = statOwnershipMode(t, child)
	if uid != 0 || gid != 0 || mode != bindManagedRootMode {
		t.Fatalf("managed child = %d:%d/%04o, want 0:0/0755", uid, gid, mode)
	}
	wantModes := []uint32{aptBINDStockCacheParentMode, aptBINDCacheParentMode}
	if !reflect.DeepEqual(durabilityModes, wantModes) {
		t.Fatalf("durability modes = %#v, want %#v", durabilityModes, wantModes)
	}
}

func TestEnsureAPTBindGenerationRootHardensExistingWithoutCreatingChild(t *testing.T) {
	t.Run("missing-child", func(t *testing.T) {
		root, rootFD := newAPTBindRootFixture(t)
		bind := filepath.Join(root, "var", "cache", "bind")
		mustChownMode(t, bind, 0, int(testBINDGID), aptBINDStockCacheParentMode)
		err := ensureAPTBindGenerationRootAtWithMode(
			rootFD, testBINDGID, true, false,
			acceptTestAPTBindDurability, nil,
		)
		if err == nil {
			t.Fatal("missing managed child was accepted")
		}
		_, _, mode := statOwnershipMode(t, bind)
		if mode != aptBINDCacheParentMode {
			t.Fatalf("parent mode = %04o, want monotonic 1775 hardening", mode)
		}
		if _, statErr := os.Lstat(filepath.Join(bind, "celikpanel")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("existing-only hardening created a child: %v", statErr)
		}
	})
	t.Run("existing-child", func(t *testing.T) {
		root, rootFD := newAPTBindRootFixture(t)
		bind := filepath.Join(root, "var", "cache", "bind")
		child := filepath.Join(bind, "celikpanel")
		if err := os.Mkdir(child, os.FileMode(bindManagedRootMode)); err != nil {
			t.Fatal(err)
		}
		mustChownMode(t, child, 0, 0, bindManagedRootMode)
		mustChownMode(t, bind, 0, int(testBINDGID), aptBINDStockCacheParentMode)
		if err := ensureAPTBindGenerationRootAtWithMode(
			rootFD, testBINDGID, true, false,
			acceptTestAPTBindDurability, nil,
		); err != nil {
			t.Fatal(err)
		}
		uid, gid, mode := statOwnershipMode(t, child)
		if uid != 0 || gid != 0 || mode != bindManagedRootMode {
			t.Fatalf("existing child changed = %d:%d/%04o", uid, gid, mode)
		}
	})
}

func TestEnsureAPTBindGenerationRootDoesNotMutateBeforeDurabilityProof(t *testing.T) {
	root, rootFD := newAPTBindRootFixture(t)
	bind := filepath.Join(root, "var", "cache", "bind")
	mustChownMode(t, bind, 0, int(testBINDGID), aptBINDStockCacheParentMode)
	err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, true,
		func(mode uint32) error {
			if mode != aptBINDStockCacheParentMode {
				t.Fatalf("durability mode = %04o", mode)
			}
			return errors.New("override unavailable")
		},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "override unavailable") {
		t.Fatalf("durability error = %v", err)
	}
	uid, gid, mode := statOwnershipMode(t, bind)
	if uid != 0 || gid != testBINDGID || mode != aptBINDStockCacheParentMode {
		t.Fatalf("parent mutated before proof = %d:%d/%04o", uid, gid, mode)
	}
	if _, statErr := os.Lstat(filepath.Join(bind, "celikpanel")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("child created before durability proof: %v", statErr)
	}
}

func TestEnsureAPTBindGenerationRootVerificationNeverHardensOrCreates(t *testing.T) {
	root, rootFD := newAPTBindRootFixture(t)
	bind := filepath.Join(root, "var", "cache", "bind")
	mustChownMode(t, bind, 0, int(testBINDGID), aptBINDStockCacheParentMode)
	if err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, false, acceptTestAPTBindDurability, nil,
	); err == nil {
		t.Fatal("read-only verification accepted an unhardened stock parent")
	}
	uid, gid, mode := statOwnershipMode(t, bind)
	if uid != 0 || gid != testBINDGID || mode != aptBINDStockCacheParentMode {
		t.Fatalf("verification mutated parent = %d:%d/%04o", uid, gid, mode)
	}
	if _, err := os.Lstat(filepath.Join(bind, "celikpanel")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verification created managed child: %v", err)
	}
}

func TestEnsureAPTBindGenerationRootRejectsSymlinkComponents(t *testing.T) {
	t.Run("external-bind-parent", func(t *testing.T) {
		root, rootFD := newAPTBindRootFixture(t)
		bind := filepath.Join(root, "var", "cache", "bind")
		realBind := filepath.Join(root, "var", "cache", "real-bind")
		if err := os.Rename(bind, realBind); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real-bind", bind); err != nil {
			t.Fatal(err)
		}
		if err := ensureAPTBindGenerationRootAt(
			rootFD, testBINDGID, true, acceptTestAPTBindDurability, nil,
		); err == nil {
			t.Fatal("symbolic /var/cache/bind was accepted")
		}
	})
	t.Run("managed-child", func(t *testing.T) {
		root, rootFD := newAPTBindRootFixture(t)
		bind := filepath.Join(root, "var", "cache", "bind")
		realChild := filepath.Join(bind, "real-child")
		if err := os.Mkdir(realChild, os.FileMode(bindManagedRootMode)); err != nil {
			t.Fatal(err)
		}
		mustChownMode(t, realChild, 0, 0, bindManagedRootMode)
		if err := os.Symlink("real-child", filepath.Join(bind, "celikpanel")); err != nil {
			t.Fatal(err)
		}
		if err := ensureAPTBindGenerationRootAt(
			rootFD, testBINDGID, true, acceptTestAPTBindDurability, nil,
		); err == nil {
			t.Fatal("symbolic managed BIND child was accepted")
		}
	})
}

func TestEnsureAPTBindGenerationRootDetectsParentTOCTOU(t *testing.T) {
	root, rootFD := newAPTBindRootFixture(t)
	bind := filepath.Join(root, "var", "cache", "bind")
	oldBind := filepath.Join(root, "var", "cache", "bind-old")
	err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, true, acceptTestAPTBindDurability, func() {
			if renameErr := os.Rename(bind, oldBind); renameErr != nil {
				t.Fatal(renameErr)
			}
			if mkdirErr := os.Mkdir(bind, os.FileMode(aptBINDCacheParentMode)); mkdirErr != nil {
				t.Fatal(mkdirErr)
			}
			mustChownMode(t, bind, 0, int(testBINDGID), aptBINDCacheParentMode)
			child := filepath.Join(bind, "celikpanel")
			if mkdirErr := os.Mkdir(child, os.FileMode(bindManagedRootMode)); mkdirErr != nil {
				t.Fatal(mkdirErr)
			}
			mustChownMode(t, child, 0, 0, bindManagedRootMode)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed during verification") {
		t.Fatalf("parent TOCTOU error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(bind, "celikpanel")); statErr != nil {
		t.Fatalf("replacement child was incorrectly removed: %v", statErr)
	}
}

func TestEnsureAPTBindGenerationRootDetectsChildTOCTOUWithoutDeletingReplacement(t *testing.T) {
	root, rootFD := newAPTBindRootFixture(t)
	bind := filepath.Join(root, "var", "cache", "bind")
	child := filepath.Join(bind, "celikpanel")
	oldChild := filepath.Join(bind, "celikpanel-old")
	err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, true, acceptTestAPTBindDurability, func() {
			if renameErr := os.Rename(child, oldChild); renameErr != nil {
				t.Fatal(renameErr)
			}
			if mkdirErr := os.Mkdir(child, os.FileMode(bindManagedRootMode)); mkdirErr != nil {
				t.Fatal(mkdirErr)
			}
			mustChownMode(t, child, 0, 0, bindManagedRootMode)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed during verification") {
		t.Fatalf("child TOCTOU error = %v", err)
	}
	if _, statErr := os.Stat(child); statErr != nil {
		t.Fatalf("replacement child was incorrectly removed: %v", statErr)
	}
	if _, statErr := os.Stat(oldChild); statErr != nil {
		t.Fatalf("original created child unexpectedly disappeared: %v", statErr)
	}
}

func TestEnsureAPTBindGenerationRootRejectsExtendedACL(t *testing.T) {
	root, rootFD := newAPTBindRootFixture(t)
	child := filepath.Join(root, "var", "cache", "bind", "celikpanel")
	if err := os.Mkdir(child, os.FileMode(bindManagedRootMode)); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, child, 0, 0, bindManagedRootMode)
	acl := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(acl[0:4], 2)
	entries := []struct {
		tag  uint16
		perm uint16
		id   uint32
	}{
		{tag: 0x01, perm: 7, id: ^uint32(0)},
		{tag: 0x02, perm: 4, id: 1205},
		{tag: 0x04, perm: 5, id: ^uint32(0)},
		{tag: 0x10, perm: 5, id: ^uint32(0)},
		{tag: 0x20, perm: 5, id: ^uint32(0)},
	}
	for index, entry := range entries {
		offset := 4 + index*8
		binary.LittleEndian.PutUint16(acl[offset:offset+2], entry.tag)
		binary.LittleEndian.PutUint16(acl[offset+2:offset+4], entry.perm)
		binary.LittleEndian.PutUint32(acl[offset+4:offset+8], entry.id)
	}
	if err := unix.Setxattr(child, "system.posix_acl_access", acl, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
			t.Skipf("filesystem does not support POSIX ACL xattrs: %v", err)
		}
		t.Fatal(err)
	}
	err := ensureAPTBindGenerationRootAt(
		rootFD, testBINDGID, true, acceptTestAPTBindDurability, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "POSIX ACL") {
		t.Fatalf("extended ACL error = %v", err)
	}
}

func newBINDMaskFixture(t *testing.T) (string, int) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("exact BIND mask ownership tests require root")
	}
	directory := t.TempDir()
	mustChownMode(t, directory, 0, 0, bindManagedRootMode)
	for _, unit := range []string{"named.service", "bind9.service"} {
		if err := os.Symlink("/dev/null", filepath.Join(directory, unit)); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return directory, fd
}

func TestVerifyBINDPersistentMaskFilesAtAcceptsExactPair(t *testing.T) {
	_, fd := newBINDMaskFixture(t)
	if err := verifyBINDPersistentMaskFilesAt(fd); err != nil {
		t.Fatalf("exact persistent masks rejected: %v", err)
	}
}

func TestVerifyBINDMaskParentMetadataAtRejects0700WithoutChmod(t *testing.T) {
	directory, fd := newBINDMaskFixture(t)
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	err := verifyBINDMaskParentMetadataAt(fd)
	if err == nil || !strings.Contains(err.Error(), "mode 0700, want 0755") {
		t.Fatalf("mode 0700 error=%v", err)
	}
	info, statErr := os.Stat(directory)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("preflight changed parent mode to %04o", info.Mode().Perm())
	}
}

func TestVerifyBINDPersistentMaskFilesAtRejectsUnsafeEntries(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "wrong-target", mutate: func(t *testing.T, directory string) {
			path := filepath.Join(directory, "named.service")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/run/evil", path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "regular-file", mutate: func(t *testing.T, directory string) {
			path := filepath.Join(directory, "named.service")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("/dev/null"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing", mutate: func(t *testing.T, directory string) {
			if err := os.Remove(filepath.Join(directory, "bind9.service")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong-uid", mutate: func(t *testing.T, directory string) {
			if err := unix.Lchown(filepath.Join(directory, "named.service"), 1206, 0); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong-gid", mutate: func(t *testing.T, directory string) {
			if err := unix.Lchown(filepath.Join(directory, "bind9.service"), 0, 1207); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "multiple-links", mutate: func(t *testing.T, directory string) {
			if err := os.Link(filepath.Join(directory, "named.service"), filepath.Join(directory, "extra.service")); err != nil {
				t.Skipf("filesystem cannot hard-link a symlink: %v", err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory, fd := newBINDMaskFixture(t)
			test.mutate(t, directory)
			if err := verifyBINDPersistentMaskFilesAt(fd); err == nil {
				t.Fatal("unsafe persistent BIND mask entry was accepted")
			}
		})
	}
}

func verifiedSignedUpdateBINDTree(
	t *testing.T,
	pairing *binddns.Pairing,
	catalogSerial uint32,
) (binddns.VerifiedTree, binddns.Receipt) {
	t.Helper()
	generation, err := binddns.RenderManifest(aptBINDGenerationRoot, binddns.Manifest{
		EngineEpoch: 1, Pairing: pairing, PrimaryCatalogSerial: catalogSerial,
	})
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(generation.Zones)+1)
	for _, zone := range generation.Zones {
		files["zones/"+zone.FileName] = zone.Data
	}
	if generation.Catalog != nil && generation.ReceiptValue.Pairing != nil {
		files[generation.ReceiptValue.Pairing.CatalogFile] = generation.Catalog.Data
	}
	tree, err := binddns.VerifyTree(generation.Receipt, generation.Config, files)
	if err != nil {
		t.Fatal(err)
	}
	return tree, generation.ReceiptValue
}

func signedUpdateBINDRuntimeLayout(
	t *testing.T,
	receipt binddns.Receipt,
	legacy bool,
) bindHostLayout {
	t.Helper()
	directory := t.TempDir()
	layout := bindHostLayout{
		GenerationRoot: aptBINDGenerationRoot,
		OptionsConfig:  filepath.Join(directory, "named.conf.options"),
		AnchorConfig:   filepath.Join(directory, "named.conf.local"),
	}
	options, err := managedBINDOptions("options {\n};\n", func() string {
		if receipt.Pairing != nil &&
			receipt.Pairing.Role == binddns.PairRoleSecondary && !legacy {
			return receipt.Pairing.PeerIP
		}
		return ""
	}())
	if err != nil {
		t.Fatal(err)
	}
	if legacy {
		options, err = managedBINDLegacyOptions(options)
		if err != nil {
			t.Fatal(err)
		}
	}
	anchor, err := managedBINDZoneInclude(
		"// operator local configuration\n",
		filepath.ToSlash(filepath.Join(aptBINDGenerationRoot, "current", "zones.conf")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.OptionsConfig, []byte(options), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.AnchorConfig, []byte(anchor), 0o644); err != nil {
		t.Fatal(err)
	}
	return layout
}

func modeledRootOwnedBINDConfigSnapshot(
	path string,
	mode os.FileMode,
	allowAbsent bool,
) (dnsFileSnapshot, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || mode.Perm() == 0 || mode.Perm() != mode {
		return dnsFileSnapshot{}, errors.New("invalid modeled BIND snapshot path or mode")
	}
	data, metadata, err := readDNSFileForSnapshot(path)
	if errors.Is(err, os.ErrNotExist) && allowAbsent {
		return dnsFileSnapshot{Path: path}, nil
	}
	if err != nil {
		return dnsFileSnapshot{}, err
	}
	if metadata.Mode.Perm() != mode.Perm() {
		return dnsFileSnapshot{}, errors.New("modeled BIND snapshot mode differs from the fixture contract")
	}
	if !metadata.OwnerKnown || metadata.UID != uint32(os.Geteuid()) ||
		metadata.GID != uint32(os.Getegid()) {
		return dnsFileSnapshot{}, errors.New("modeled BIND snapshot is not owned by the test process")
	}
	return dnsFileSnapshot{
		Path: path, Exists: true, Mode: uint32(metadata.Mode.Perm()),
		OwnerKnown: true, UID: 0, GID: 0,
		SHA256: digestDNSBytes(data), Data: append([]byte(nil), data...),
	}, nil
}

func TestVerifyExistingManagedBINDTreeForSignedUpdateAcceptsReleasedLayouts(t *testing.T) {
	originalOwned := dnsPairHostAddressOwned
	t.Cleanup(func() { dnsPairHostAddressOwned = originalOwned })
	dnsPairHostAddressOwned = func(address string) (bool, error) {
		return address == "72.62.38.15", nil
	}
	pair := func(role string) *binddns.Pairing {
		return &binddns.Pairing{
			Role: role, LocalIP: "72.62.38.15", LocalNS: "ns1.celikhost.com",
			PeerIP: "2.25.80.4", PeerNS: "ns2.celikhost.com",
		}
	}
	for _, test := range []struct {
		name          string
		pairing       *binddns.Pairing
		catalogSerial uint32
		legacy        bool
		directional   bool
	}{
		{name: "standalone"},
		{name: "directional-primary", pairing: pair(binddns.PairRolePrimary), catalogSerial: 7, directional: true},
		{name: "released-tupleless-secondary", pairing: pair(binddns.PairRoleSecondary), legacy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tree, receipt := verifiedSignedUpdateBINDTree(
				t, test.pairing, test.catalogSerial,
			)
			layout := signedUpdateBINDRuntimeLayout(t, receipt, test.legacy)
			state := legacyDurableDNSState(transport.DNSEngineBIND)
			state.EngineEpoch = receipt.EngineEpoch
			state.Generation = receipt.Generation
			if test.directional {
				state.PairRole = receipt.Pairing.Role
				state.PairLocalIP = receipt.Pairing.LocalIP
				state.PairPeerIP = receipt.Pairing.PeerIP
				state.PrimaryCatalogSerial = receipt.Pairing.CatalogSerial
			}
			if os.Geteuid() == 0 {
				if err := verifyExistingManagedBINDTreeForSignedUpdate(
					layout, state, tree,
				); err != nil {
					t.Fatal(err)
				}
			} else {
				err := verifyExistingManagedBINDTreeForSignedUpdate(
					layout, state, tree,
				)
				if err == nil || !strings.Contains(err.Error(), "not root-owned") {
					t.Fatalf("production ownership proof accepted a rootless fixture: %v", err)
				}
			}
			if err := verifyExistingManagedBINDTreeForSignedUpdateWithSnapshotReader(
				layout, state, tree, modeledRootOwnedBINDConfigSnapshot,
			); err != nil {
				t.Fatalf("released layout was rejected by the modeled root-owned proof: %v", err)
			}

			t.Run("generation-drift", func(t *testing.T) {
				drift := state
				drift.Generation = strings.Repeat("a", 64)
				if drift.Generation == state.Generation {
					drift.Generation = strings.Repeat("b", 64)
				}
				if err := verifyExistingManagedBINDTreeForSignedUpdateWithSnapshotReader(
					layout, drift, tree, modeledRootOwnedBINDConfigSnapshot,
				); err == nil {
					t.Fatal("generation drift was accepted")
				}
			})
			t.Run("runtime-config-drift", func(t *testing.T) {
				original, err := os.ReadFile(layout.OptionsConfig)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.WriteFile(layout.OptionsConfig, original, 0o644); err != nil {
						t.Error(err)
					}
				}()
				drifted := bytes.Replace(
					original, []byte("recursion no;"), []byte("recursion yes;"), 1,
				)
				if bytes.Equal(drifted, original) {
					t.Fatal("test fixture lacks managed recursion directive")
				}
				if err := os.WriteFile(
					layout.OptionsConfig, drifted, 0o644,
				); err != nil {
					t.Fatal(err)
				}
				if err := verifyExistingManagedBINDTreeForSignedUpdateWithSnapshotReader(
					layout, state, tree, modeledRootOwnedBINDConfigSnapshot,
				); err == nil {
					t.Fatal("runtime config drift was accepted")
				}
			})
			if receipt.Pairing != nil {
				t.Run("host-identity-drift", func(t *testing.T) {
					dnsPairHostAddressOwned = func(string) (bool, error) { return false, nil }
					defer func() {
						dnsPairHostAddressOwned = func(address string) (bool, error) {
							return address == "72.62.38.15", nil
						}
					}()
					if err := verifyExistingManagedBINDTreeForSignedUpdateWithSnapshotReader(
						layout, state, tree, modeledRootOwnedBINDConfigSnapshot,
					); err == nil {
						t.Fatal("unowned local pair address was accepted")
					}
				})
			}
		})
	}
}
