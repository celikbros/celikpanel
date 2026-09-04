//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const frankfurtBINDConfigGID = uint32(109)

func newFrankfurtBINDConfigFixture(
	t *testing.T,
) (bindHostLayout, bindConfigOwnerPolicy, bindConfigMutation) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("Frankfurt root:bind ownership tests require root")
	}
	directory := t.TempDir()
	mustChownMode(t, directory, 0, int(frankfurtBINDConfigGID), 0o2755)
	layout := bindHostLayout{
		GenerationRoot: filepath.Join(directory, "generations"),
		OptionsConfig:  filepath.Join(directory, "named.conf.options"),
		AnchorConfig:   filepath.Join(directory, "named.conf.local"),
	}
	contents := map[string][]byte{
		layout.OptionsConfig: []byte("options { };\n"),
		layout.AnchorConfig:  []byte("// operator local config\n"),
	}
	for path, content := range contents {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		mustChownMode(t, path, 0, int(frankfurtBINDConfigGID), 0o644)
	}
	mutation, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, "", bindOptionsExclusive,
		func(path string, mode os.FileMode, allowAbsent bool) (dnsFileSnapshot, error) {
			if allowAbsent {
				return dnsFileSnapshot{}, errors.New("unexpected absent BIND config")
			}
			return captureBINDConfigSnapshot(path, mode)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	mutation.ownerAware = true
	policy := bindConfigOwnerPolicy{
		paths: bindConfigPaths(layout), apt: true, bindGID: frankfurtBINDConfigGID,
	}
	return layout, policy, mutation
}

func TestBINDConfigParentPolicyAcceptsStockAPTLayouts(t *testing.T) {
	layout, policy, _ := newFrankfurtBINDConfigFixture(t)
	parent := filepath.Dir(layout.AnchorConfig)
	if _, err := captureBINDConfigSnapshotSet(policy); err != nil {
		t.Fatalf("root:bind 2755 BIND parent rejected: %v", err)
	}
	mustChownMode(t, parent, 0, 0, 0o755)
	if _, err := captureBINDConfigSnapshotSet(policy); err != nil {
		t.Fatalf("legacy root:root 0755 BIND parent rejected: %v", err)
	}
	mustChownMode(t, parent, 0, int(frankfurtBINDConfigGID), 0o2750)
	if _, err := captureBINDConfigSnapshotSet(policy); err != nil {
		t.Fatalf("hardened root:bind 2750 BIND parent rejected: %v", err)
	}
}

func TestBINDConfigParentPolicyRejectsUnsafeParentBeforeWriting(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "group-writable", mutate: func(t *testing.T, parent string) {
			mustChownMode(t, parent, 0, int(frankfurtBINDConfigGID), 0o2775)
		}},
		{name: "world-writable", mutate: func(t *testing.T, parent string) {
			mustChownMode(t, parent, 0, int(frankfurtBINDConfigGID), 0o777)
		}},
		{name: "nonroot-owner", mutate: func(t *testing.T, parent string) {
			mustChownMode(t, parent, 1200, int(frankfurtBINDConfigGID), 0o2755)
		}},
		{name: "foreign-group", mutate: func(t *testing.T, parent string) {
			mustChownMode(t, parent, 0, 110, 0o2755)
		}},
		{name: "bind-cannot-traverse-root-group", mutate: func(t *testing.T, parent string) {
			mustChownMode(t, parent, 0, 0, 0o750)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layout, policy, mutation := newFrankfurtBINDConfigFixture(t)
			test.mutate(t, filepath.Dir(layout.AnchorConfig))
			if _, err := captureBINDConfigSnapshotSet(policy); err == nil {
				t.Fatal("unsafe BIND config parent was accepted during capture")
			}
			writes := 0
			err := mutation.applyOwnerAwareWithPolicyAndOps(policy, bindConfigApplyOps{
				write: func(string, []byte, os.FileMode, *dnsFileSnapshot) error {
					writes++
					return errors.New("unexpected BIND config write")
				},
			})
			if err == nil || writes != 0 {
				t.Fatalf("unsafe parent reached config writer: writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestBINDConfigParentPolicyRejectsSymlinkedParent(t *testing.T) {
	layout, policy, _ := newFrankfurtBINDConfigFixture(t)
	parent := filepath.Dir(layout.AnchorConfig)
	realParent := parent + "-real"
	if err := os.Rename(parent, realParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realParent, parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(parent)
		_ = os.Rename(realParent, parent)
	})
	if _, err := captureBINDConfigSnapshotSet(policy); err == nil {
		t.Fatal("symlinked BIND config parent was accepted")
	}
}

func TestBINDConfigParentPolicyRejectsExtendedACL(t *testing.T) {
	layout, policy, _ := newFrankfurtBINDConfigFixture(t)
	parent := filepath.Dir(layout.AnchorConfig)
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
	if err := unix.Setxattr(parent, "system.posix_acl_access", acl, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
			t.Skipf("filesystem does not support POSIX ACL xattrs: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := captureBINDConfigSnapshotSet(policy); err == nil ||
		!strings.Contains(err.Error(), "POSIX ACL") {
		t.Fatalf("extended parent ACL was not rejected: %v", err)
	}
}

func TestBINDConfigWriterRejectsParentMetadataDriftBeforeRename(t *testing.T) {
	layout, policy, mutation := newFrankfurtBINDConfigFixture(t)
	parent := filepath.Dir(layout.AnchorConfig)
	path := mutation.paths[0]
	before := mutation.snapshots[path]
	err := secureWriteConfigReplacingSnapshotWithBINDParentAndHook(
		path,
		mutation.desired[path],
		os.FileMode(before.Mode),
		&before,
		&policy,
		func() {
			mustChownMode(t, parent, 0, int(frankfurtBINDConfigGID), 0o2775)
		},
	)
	if err == nil {
		t.Fatal("unsafe parent metadata drift was accepted before rename")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(data, before.Data) {
		t.Fatalf("parent drift committed the config replacement: %q %v", data, readErr)
	}
}

func TestBINDConfigWriterRejectsReopenedParentBeforeRename(t *testing.T) {
	layout, policy, mutation := newFrankfurtBINDConfigFixture(t)
	parent := filepath.Dir(layout.AnchorConfig)
	movedParent := parent + "-held"
	path := mutation.paths[0]
	before := mutation.snapshots[path]
	err := secureWriteConfigReplacingSnapshotWithBINDParentAndHook(
		path,
		mutation.desired[path],
		os.FileMode(before.Mode),
		&before,
		&policy,
		func() {
			if renameErr := os.Rename(parent, movedParent); renameErr != nil {
				t.Fatal(renameErr)
			}
			if mkdirErr := os.Mkdir(parent, 0o755); mkdirErr != nil {
				t.Fatal(mkdirErr)
			}
			mustChownMode(t, parent, 0, 0, 0o755)
		},
	)
	t.Cleanup(func() {
		_ = os.RemoveAll(parent)
		_ = os.Rename(movedParent, parent)
	})
	if err == nil || !strings.Contains(err.Error(), "parent path changed") {
		t.Fatalf("reopened BIND config parent was not rejected: %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(movedParent, filepath.Base(path)))
	if readErr != nil || !bytes.Equal(data, before.Data) {
		t.Fatalf("reopened parent committed replacement: %q %v", data, readErr)
	}
}

func assertBINDConfigSnapshotSet(
	t *testing.T,
	policy bindConfigOwnerPolicy,
	want []dnsFileSnapshot,
) {
	t.Helper()
	if err := verifyBINDConfigSnapshotSetExact(policy, want); err != nil {
		t.Fatal(err)
	}
}

func TestFrankfurtBINDConfigCaptureJournalApplyRestoreRoundTrip(t *testing.T) {
	_, policy, mutation := newFrankfurtBINDConfigFixture(t)
	original := mutation.originalSnapshots()
	if err := policy.validateSnapshots(original); err != nil {
		t.Fatalf("Frankfurt root:bind snapshots rejected: %v", err)
	}

	journal := testBINDSwitchJournal(t)
	journal.ConfigBefore = make([]dnsFileSnapshot, len(original))
	for index, snapshot := range original {
		snapshot.Path = []string{
			"/etc/bind/named.conf.local", "/etc/bind/named.conf.options",
		}[index]
		journal.ConfigBefore[index] = snapshot
	}
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatalf("encode root:bind journal: %v", err)
	}
	decoded, err := decodeDNSEngineSwitchJournal(encoded)
	if err != nil || !reflect.DeepEqual(decoded.ConfigBefore, journal.ConfigBefore) {
		t.Fatalf("root:bind journal round trip failed: %v", err)
	}

	if err := mutation.applyOwnerAwareWithPolicy(policy); err != nil {
		t.Fatalf("apply root:bind mutation: %v", err)
	}
	assertBINDConfigSnapshotSet(t, policy, mutation.desiredSnapshots())
	if err := mutation.restoreOwnerAwareWithPolicy(policy); err != nil {
		t.Fatalf("restore root:bind mutation: %v", err)
	}
	assertBINDConfigSnapshotSet(t, policy, original)
}

func TestBINDConfigOwnerPolicyRejectsUnsafeMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, bindHostLayout)
	}{
		{name: "wrong-owner", mutate: func(t *testing.T, layout bindHostLayout) {
			mustChownMode(t, layout.AnchorConfig, 0, 110, 0o644)
		}},
		{name: "wrong-mode", mutate: func(t *testing.T, layout bindHostLayout) {
			if err := os.Chmod(layout.AnchorConfig, 0o664); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", mutate: func(t *testing.T, layout bindHostLayout) {
			target := filepath.Join(filepath.Dir(layout.AnchorConfig), "symlink-target")
			if err := os.WriteFile(target, []byte("target\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(layout.AnchorConfig); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, layout.AnchorConfig); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "hardlink", mutate: func(t *testing.T, layout bindHostLayout) {
			link := layout.AnchorConfig + ".hardlink"
			if err := os.Link(layout.AnchorConfig, link); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layout, policy, _ := newFrankfurtBINDConfigFixture(t)
			test.mutate(t, layout)
			if _, err := captureBINDConfigSnapshotSet(policy); err == nil {
				t.Fatal("unsafe BIND config metadata was accepted")
			}
		})
	}
}

func TestBINDConfigOwnerPolicyRejectsGIDDriftAndGenericRemainsStrict(t *testing.T) {
	layout, policy, mutation := newFrankfurtBINDConfigFixture(t)
	drifted := policy
	drifted.bindGID++
	if _, err := captureBINDConfigSnapshotSet(drifted); err == nil {
		t.Fatal("foreign BIND group identity was accepted")
	}
	if _, err := captureDNSFileSnapshot(layout.AnchorConfig, 0o644, false); err == nil ||
		!strings.Contains(err.Error(), "not root-owned") {
		t.Fatalf("generic root:root snapshot contract changed: %v", err)
	}
	for _, path := range mutation.paths {
		mustChownMode(t, path, 0, 0, 0o644)
	}
	if _, err := captureBINDConfigSnapshotSet(policy); err != nil {
		t.Fatalf("legacy root:root APT config was rejected: %v", err)
	}
}

func TestDNSConfigSnapshotRejectsMetadataTOCTOU(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named.conf")
	if err := os.WriteFile(path, []byte("options {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := readDNSFileForSnapshotWithHook(path, func() {
		if chmodErr := os.Chmod(path, 0o640); chmodErr != nil {
			t.Fatal(chmodErr)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "changed while it was snapshotted") {
		t.Fatalf("metadata TOCTOU was not rejected: %v", err)
	}
}

func TestBINDConfigApplyRejectsPreimageDriftBeforeWriting(t *testing.T) {
	_, policy, mutation := newFrankfurtBINDConfigFixture(t)
	original := mutation.originalSnapshots()
	driftPath := mutation.paths[1]
	if err := os.WriteFile(driftPath, []byte("foreign operator edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mutation.applyOwnerAwareWithPolicy(policy); err == nil {
		t.Fatal("BIND preimage drift was accepted")
	}
	unchanged, err := captureBINDConfigSnapshot(mutation.paths[0], 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unchanged, original[0]) {
		t.Fatal("apply wrote a peer before detecting preimage drift")
	}
}

func TestBINDConfigApplySecondWriteFailureRestoresFirstExactly(t *testing.T) {
	_, policy, mutation := newFrankfurtBINDConfigFixture(t)
	original := mutation.originalSnapshots()
	writes := 0
	write := bindConfigWriterForPolicy(policy)
	err := mutation.applyOwnerAwareWithPolicyAndOps(policy, bindConfigApplyOps{
		write: func(path string, data []byte, mode os.FileMode, before *dnsFileSnapshot) error {
			writes++
			if writes == 2 {
				return errors.New("forced second write failure")
			}
			return write(path, data, mode, before)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "forced second write failure") {
		t.Fatalf("second write failure was not returned: %v", err)
	}
	assertBINDConfigSnapshotSet(t, policy, original)
}

func TestBINDConfigApplyReconcilesPostRenameErrorAndRollsBack(t *testing.T) {
	_, policy, mutation := newFrankfurtBINDConfigFixture(t)
	original := mutation.originalSnapshots()
	writes := 0
	write := bindConfigWriterForPolicy(policy)
	err := mutation.applyOwnerAwareWithPolicyAndOps(policy, bindConfigApplyOps{
		write: func(path string, data []byte, mode os.FileMode, before *dnsFileSnapshot) error {
			writes++
			if err := write(path, data, mode, before); err != nil {
				return err
			}
			if writes == 1 {
				return errors.New("forced post-rename directory fsync ambiguity")
			}
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "post-rename directory fsync ambiguity") {
		t.Fatalf("ambiguous committed write was not reported: %v", err)
	}
	assertBINDConfigSnapshotSet(t, policy, original)
}

func TestBINDConfigApplyFinalPeerDriftRollsBackWrittenFiles(t *testing.T) {
	_, policy, mutation := newFrankfurtBINDConfigFixture(t)
	original := mutation.originalSnapshots()
	peerPath := mutation.paths[1]
	mutation.desired[peerPath] = append([]byte(nil), mutation.original[peerPath]...)
	err := mutation.applyOwnerAwareWithPolicyAndOps(policy, bindConfigApplyOps{
		write: bindConfigWriterForPolicy(policy),
		beforeFinal: func() {
			if writeErr := os.WriteFile(peerPath, []byte("late peer drift\n"), 0o644); writeErr != nil {
				t.Fatal(writeErr)
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "final readback") {
		t.Fatalf("late peer drift was not rejected: %v", err)
	}
	first, err := captureBINDConfigSnapshot(mutation.paths[0], 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, original[0]) {
		t.Fatal("written peer was not rolled back after final-set drift")
	}
	late, err := os.ReadFile(peerPath)
	if err != nil || !bytes.Equal(late, []byte("late peer drift\n")) {
		t.Fatalf("foreign peer drift was overwritten: %q %v", late, err)
	}
}

func TestBINDConfigRestoreFinalPeerDriftReturnsEvidence(t *testing.T) {
	_, policy, mutation := newFrankfurtBINDConfigFixture(t)
	if err := mutation.applyOwnerAwareWithPolicy(policy); err != nil {
		t.Fatal(err)
	}
	peerPath := mutation.paths[1]
	err := mutation.restoreOwnerAwareWithPolicyAndOps(policy, bindConfigRestoreOps{
		write: bindConfigWriterForPolicy(policy),
		beforeFinal: func() {
			if writeErr := os.WriteFile(peerPath, []byte("late restore drift\n"), 0o644); writeErr != nil {
				t.Fatal(writeErr)
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "final readback") {
		t.Fatalf("late restore drift was not returned as evidence: %v", err)
	}
	late, readErr := os.ReadFile(peerPath)
	if readErr != nil || !bytes.Equal(late, []byte("late restore drift\n")) {
		t.Fatalf("foreign restore drift was overwritten: %q %v", late, readErr)
	}
}
