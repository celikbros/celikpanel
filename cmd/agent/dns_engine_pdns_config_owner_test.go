package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func useTestPDNSConfigPaths(t *testing.T) {
	t.Helper()
	oldMain, oldManaged, oldCluster := dnsMainConf, dnsManagedConf, dnsClusterConf
	root := t.TempDir()
	dnsMainConf = filepath.Join(root, "etc", "powerdns", "pdns.conf")
	dnsManagedConf = filepath.Join(
		root, "etc", "powerdns", "pdns.d", "celikpanel.conf",
	)
	dnsClusterConf = filepath.Join(
		root, "etc", "powerdns", "pdns.d", "celikpanel-cluster.conf",
	)
	t.Cleanup(func() {
		dnsMainConf, dnsManagedConf, dnsClusterConf = oldMain, oldManaged, oldCluster
	})
}

func testPDNSOwnerManifest(t *testing.T) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEnginePowerDNS, 0, 1, 0,
		transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func testPDNSConfigSnapshot(
	path string,
	data string,
	mode uint32,
	uid uint32,
	gid uint32,
) dnsFileSnapshot {
	bytes := []byte(data)
	return dnsFileSnapshot{
		Path: filepath.Clean(path), Exists: true, Mode: mode,
		OwnerKnown: true, UID: uid, GID: gid,
		SHA256: digestDNSBytes(bytes), Data: bytes,
	}
}

func testPDNSConfigObservation(
	snapshot dnsFileSnapshot,
	inode uint64,
) pdnsConfigObservation {
	snapshot.Data = append([]byte(nil), snapshot.Data...)
	observation := pdnsConfigObservation{Snapshot: snapshot}
	if snapshot.Exists {
		observation.Identity = pdnsConfigFileIdentity{
			Exists: true, Device: 7, Inode: inode,
			Mode: snapshot.Mode, UID: snapshot.UID, GID: snapshot.GID,
			Links: 1, Size: int64(len(snapshot.Data)),
			MTimeSec: int64(inode), CTimeSec: int64(inode),
		}
	}
	return observation
}

func clonePDNSConfigObservation(
	observation pdnsConfigObservation,
) pdnsConfigObservation {
	observation.Snapshot.Data = append(
		[]byte(nil), observation.Snapshot.Data...,
	)
	return observation
}

type fakePDNSConfigFS struct {
	policy                    pdnsConfigOwnerPolicy
	state                     map[string]pdnsConfigObservation
	nextInode                 uint64
	writes                    int
	beforeFirstReplacement    func(*fakePDNSConfigFS, string)
	replaceErrorAfterCommitAt string
}

func newFakePDNSConfigFS(mainGID uint32) *fakePDNSConfigFS {
	main := testPDNSConfigSnapshot(
		dnsMainConf, "# stock PowerDNS configuration\n", 0o640, 0, mainGID,
	)
	result := &fakePDNSConfigFS{
		policy:    pdnsConfigOwnerPolicy{pdnsGID: 109},
		state:     make(map[string]pdnsConfigObservation, 3),
		nextInode: 100,
	}
	result.state[filepath.Clean(dnsMainConf)] =
		testPDNSConfigObservation(main, 90)
	for _, path := range []string{dnsManagedConf, dnsClusterConf} {
		clean := filepath.Clean(path)
		result.state[clean] = pdnsConfigObservation{
			Snapshot: dnsFileSnapshot{Path: clean},
		}
	}
	return result
}

func (fake *fakePDNSConfigFS) snapshots() []dnsFileSnapshot {
	result := make([]dnsFileSnapshot, 0, len(fake.state))
	for _, path := range pdnsConfigPaths() {
		observation := fake.state[path]
		snapshot := observation.Snapshot
		snapshot.Data = append([]byte(nil), snapshot.Data...)
		result = append(result, snapshot)
	}
	return result
}

func (fake *fakePDNSConfigFS) observations() []pdnsConfigObservation {
	result := make([]pdnsConfigObservation, 0, len(fake.state))
	for _, path := range pdnsConfigPaths() {
		result = append(result, clonePDNSConfigObservation(fake.state[path]))
	}
	return result
}

func (fake *fakePDNSConfigFS) nextObservation(
	snapshot dnsFileSnapshot,
) pdnsConfigObservation {
	fake.nextInode++
	return testPDNSConfigObservation(snapshot, fake.nextInode)
}

func (fake *fakePDNSConfigFS) ops() pdnsConfigAccessOps {
	return pdnsConfigAccessOps{
		resolve: func(context.Context) (pdnsConfigOwnerPolicy, error) {
			return fake.policy, nil
		},
		capture: func(policy pdnsConfigOwnerPolicy) ([]pdnsConfigObservation, error) {
			if policy != fake.policy {
				return nil, errors.New("unexpected owner policy")
			}
			return fake.observations(), nil
		},
		replace: func(
			policy pdnsConfigOwnerPolicy,
			before pdnsConfigObservation,
			desired dnsFileSnapshot,
		) error {
			if fake.beforeFirstReplacement != nil {
				hook := fake.beforeFirstReplacement
				fake.beforeFirstReplacement = nil
				hook(fake, desired.Path)
			}
			if policy != fake.policy ||
				!reflect.DeepEqual(fake.state[desired.Path], before) {
				return errors.New("fake replacement preimage changed")
			}
			fake.writes++
			fake.state[desired.Path] = fake.nextObservation(desired)
			if desired.Path == fake.replaceErrorAfterCommitAt {
				return errors.New("ambiguous post-rename sync failure")
			}
			return nil
		},
		remove: func(
			policy pdnsConfigOwnerPolicy,
			before pdnsConfigObservation,
		) error {
			if fake.beforeFirstReplacement != nil {
				hook := fake.beforeFirstReplacement
				fake.beforeFirstReplacement = nil
				hook(fake, before.Snapshot.Path)
			}
			if policy != fake.policy ||
				!reflect.DeepEqual(fake.state[before.Snapshot.Path], before) {
				return errors.New("fake removal preimage changed")
			}
			fake.writes++
			fake.state[before.Snapshot.Path] = pdnsConfigObservation{
				Snapshot: dnsFileSnapshot{Path: before.Snapshot.Path},
			}
			return nil
		},
	}
}

func testPDNSOwnerMutation(
	t *testing.T,
	fake *fakePDNSConfigFS,
) pdnsConfigMutation {
	t.Helper()
	before := fake.snapshots()
	mutation, err := newPDNSConfigMutationFromSnapshots(
		fake.policy, before, testPDNSOwnerManifest(t),
		testManagedPowerDNSStandaloneConfig(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	mutation.originalIdentities = make(
		map[string]pdnsConfigFileIdentity, len(fake.state),
	)
	for path, observation := range fake.state {
		mutation.originalIdentities[path] = observation.Identity
	}
	return mutation
}

func TestPDNSConfigOwnerPolicyAcceptsInstalledMainFileContracts(t *testing.T) {
	useTestPDNSConfigPaths(t)
	policy := pdnsConfigOwnerPolicy{pdnsGID: 109}
	for _, test := range []struct {
		name string
		gid  uint32
	}{
		{name: "Ubuntu-root-pdns", gid: 109},
		{name: "root-root-compatible", gid: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakePDNSConfigFS(test.gid)
			if err := policy.validateSnapshots(fake.snapshots()); err != nil {
				t.Fatalf("installed main config rejected: %v", err)
			}
			mutation := testPDNSOwnerMutation(t, fake)
			desired := pdnsConfigSnapshotMap(mutation.desiredSnapshots())
			main := desired[filepath.Clean(dnsMainConf)]
			if main.Mode != 0o640 || main.UID != 0 || main.GID != test.gid {
				t.Fatalf("main metadata not preserved: %#v", main)
			}
			managed := desired[filepath.Clean(dnsManagedConf)]
			if managed.Mode != 0o644 || managed.UID != 0 || managed.GID != 0 {
				t.Fatalf("drop-in metadata is not root:root 0644: %#v", managed)
			}
		})
	}

	t.Run("foreign-group", func(t *testing.T) {
		fake := newFakePDNSConfigFS(110)
		if err := policy.validateSnapshots(fake.snapshots()); err == nil {
			t.Fatal("foreign main config group accepted")
		}
	})
	t.Run("wrong-mode", func(t *testing.T) {
		fake := newFakePDNSConfigFS(109)
		main := fake.state[filepath.Clean(dnsMainConf)]
		main.Snapshot.Mode = 0o644
		main.Identity.Mode = 0o644
		fake.state[filepath.Clean(dnsMainConf)] = main
		if err := policy.validateSnapshots(fake.snapshots()); err == nil {
			t.Fatal("wrong main config mode accepted")
		}
	})
}

func TestResolvePDNSGroupGIDRequiresStableCanonicalRecord(t *testing.T) {
	ctx := context.Background()
	calls := 0
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if name != "/usr/bin/getent" ||
			!reflect.DeepEqual(args, []string{"group", "pdns"}) {
			return nil, errors.New("unexpected getent invocation")
		}
		return []byte("pdns:x:109:\n"), nil
	}
	gid, err := resolvePDNSGroupGIDWithRunner(ctx, "/usr/bin/getent", runner)
	if err != nil || gid != 109 || calls != 2 {
		t.Fatalf("canonical group proof = gid %d, calls %d, err %v", gid, calls, err)
	}

	for _, record := range []string{
		"pdns:x:109:",
		"pdns:x:109:\nextra\n",
		"powerdns:x:109:\n",
		"pdns:X:109:\n",
		"pdns:x:0109:\n",
		"pdns:x:0:\n",
		"pdns:x:2147483648:\n",
		"pdns:x:109:operator\n",
	} {
		t.Run(fmt.Sprintf("record-%q", record), func(t *testing.T) {
			_, err := resolvePDNSGroupGIDWithRunner(
				ctx, "/usr/bin/getent",
				func(context.Context, string, ...string) ([]byte, error) {
					return []byte(record), nil
				},
			)
			if err == nil {
				t.Fatalf("unsafe group record accepted: %q", record)
			}
		})
	}

	calls = 0
	_, err = resolvePDNSGroupGIDWithRunner(
		ctx, "/usr/bin/getent",
		func(context.Context, string, ...string) ([]byte, error) {
			calls++
			return []byte(fmt.Sprintf("pdns:x:%d:\n", 108+calls)), nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("group identity drift accepted: %v", err)
	}
}

func TestPDNSConfigOwnerAwareApplyAndRestorePreserveExactMetadata(t *testing.T) {
	useTestPDNSConfigPaths(t)
	for _, mainGID := range []uint32{109, 0} {
		t.Run(fmt.Sprintf("main-gid-%d", mainGID), func(t *testing.T) {
			fake := newFakePDNSConfigFS(mainGID)
			cluster := testPDNSConfigSnapshot(
				dnsClusterConf, "legacy cluster config\n", 0o644, 0, 0,
			)
			fake.state[filepath.Clean(dnsClusterConf)] =
				testPDNSConfigObservation(cluster, 91)
			mutation := testPDNSOwnerMutation(t, fake)
			before := mutation.originalSnapshots()

			if err := mutation.applyOwnerAwareWithOps(
				context.Background(), fake.ops(),
			); err != nil {
				t.Fatalf("apply owner-aware config: %v", err)
			}
			if !reflect.DeepEqual(fake.snapshots(), mutation.desiredSnapshots()) {
				t.Fatalf("desired config mismatch:\nwant %#v\ngot  %#v",
					mutation.desiredSnapshots(), fake.snapshots())
			}
			main := fake.state[filepath.Clean(dnsMainConf)].Snapshot
			if main.Mode != 0o640 || main.UID != 0 || main.GID != mainGID {
				t.Fatalf("main config metadata changed: %#v", main)
			}
			managed := fake.state[filepath.Clean(dnsManagedConf)].Snapshot
			if managed.Mode != 0o644 || managed.UID != 0 || managed.GID != 0 {
				t.Fatalf("managed config metadata unsafe: %#v", managed)
			}

			if err := mutation.restoreOwnerAwareWithOps(
				context.Background(), fake.ops(),
			); err != nil {
				t.Fatalf("restore owner-aware config: %v", err)
			}
			if !reflect.DeepEqual(fake.snapshots(), before) {
				t.Fatalf("original config not restored:\nwant %#v\ngot  %#v",
					before, fake.snapshots())
			}
		})
	}
}

func TestPDNSConfigOwnerAwareApplyReconcilesAmbiguousPostRename(t *testing.T) {
	useTestPDNSConfigPaths(t)
	fake := newFakePDNSConfigFS(109)
	mutation := testPDNSOwnerMutation(t, fake)
	before := mutation.originalSnapshots()
	fake.replaceErrorAfterCommitAt = filepath.Clean(dnsMainConf)

	err := mutation.applyOwnerAwareWithOps(context.Background(), fake.ops())
	if err == nil || !strings.Contains(err.Error(), "post-rename") {
		t.Fatalf("ambiguous post-rename outcome not reported: %v", err)
	}
	if !reflect.DeepEqual(fake.snapshots(), before) {
		t.Fatalf("committed ambiguous write not rolled back:\nwant %#v\ngot  %#v",
			before, fake.snapshots())
	}
}

func TestPDNSConfigOwnerAwareApplyDoesNotOverwriteTOCTOUDrift(t *testing.T) {
	useTestPDNSConfigPaths(t)
	fake := newFakePDNSConfigFS(109)
	mutation := testPDNSOwnerMutation(t, fake)
	mainPath := filepath.Clean(dnsMainConf)
	foreign := testPDNSConfigSnapshot(
		dnsMainConf, "# operator replacement\n", 0o640, 0, 109,
	)
	fake.beforeFirstReplacement = func(fake *fakePDNSConfigFS, path string) {
		if path != mainPath {
			t.Fatalf("first replacement path = %s", path)
		}
		fake.state[path] = fake.nextObservation(foreign)
	}

	err := mutation.applyOwnerAwareWithOps(context.Background(), fake.ops())
	if err == nil {
		t.Fatal("TOCTOU drift was accepted")
	}
	if fake.writes != 0 {
		t.Fatalf("mutation wrote after TOCTOU drift: %d writes", fake.writes)
	}
	if !reflect.DeepEqual(fake.state[mainPath].Snapshot, foreign) {
		t.Fatal("operator replacement was overwritten")
	}
}

func TestPDNSConfigOwnerAwareApplyDoesNotOverwriteFinalDrift(t *testing.T) {
	useTestPDNSConfigPaths(t)
	fake := newFakePDNSConfigFS(109)
	mutation := testPDNSOwnerMutation(t, fake)
	before := mutation.originalSnapshots()
	managedPath := filepath.Clean(dnsManagedConf)
	foreign := testPDNSConfigSnapshot(
		dnsManagedConf, "# operator replacement\n", 0o644, 0, 0,
	)
	ops := fake.ops()
	ops.beforeFinal = func() {
		fake.state[managedPath] = fake.nextObservation(foreign)
	}

	err := mutation.applyOwnerAwareWithOps(context.Background(), ops)
	if err == nil {
		t.Fatal("final external drift was accepted")
	}
	if !reflect.DeepEqual(fake.state[managedPath].Snapshot, foreign) {
		t.Fatal("final operator replacement was overwritten")
	}
	mainPath := filepath.Clean(dnsMainConf)
	if !reflect.DeepEqual(
		fake.state[mainPath].Snapshot,
		pdnsConfigSnapshotMap(before)[mainPath],
	) {
		t.Fatal("independent committed main write was not rolled back")
	}
}

func TestPDNSConfigOwnerAwareProofRejectsIdentityAndGroupDrift(t *testing.T) {
	useTestPDNSConfigPaths(t)
	t.Run("same-bytes-new-inode", func(t *testing.T) {
		fake := newFakePDNSConfigFS(109)
		mutation := testPDNSOwnerMutation(t, fake)
		mainPath := filepath.Clean(dnsMainConf)
		current := fake.state[mainPath]
		current.Identity.Inode++
		fake.state[mainPath] = current
		if err := mutation.applyOwnerAwareWithOps(
			context.Background(), fake.ops(),
		); err == nil {
			t.Fatal("same-bytes inode replacement accepted")
		}
		if fake.writes != 0 {
			t.Fatalf("wrote after identity drift: %d writes", fake.writes)
		}
	})

	t.Run("runtime-pdns-gid", func(t *testing.T) {
		fake := newFakePDNSConfigFS(109)
		mutation := testPDNSOwnerMutation(t, fake)
		ops := fake.ops()
		ops.resolve = func(context.Context) (pdnsConfigOwnerPolicy, error) {
			return pdnsConfigOwnerPolicy{pdnsGID: 110}, nil
		}
		if err := mutation.applyOwnerAwareWithOps(
			context.Background(), ops,
		); err == nil {
			t.Fatal("runtime pdns group drift accepted")
		}
		if fake.writes != 0 {
			t.Fatalf("wrote after group drift: %d writes", fake.writes)
		}
	})
}

func TestPDNSSwitchRollbackProvesConfigBeforeAnyRecoveryMutation(t *testing.T) {
	var order []string
	err := rollbackPDNSSwitchAfterConfigProof(
		func() error {
			order = append(order, "proof")
			return nil
		},
		func() error {
			order = append(order, "rollback")
			return nil
		},
	)
	if err != nil || !reflect.DeepEqual(order, []string{"proof", "rollback"}) {
		t.Fatalf("rollback ordering = %v, err %v", order, err)
	}

	order = nil
	proofErr := errors.New("unsafe config state")
	err = rollbackPDNSSwitchAfterConfigProof(
		func() error {
			order = append(order, "proof")
			return proofErr
		},
		func() error {
			order = append(order, "rollback")
			return nil
		},
	)
	if !errors.Is(err, proofErr) || !reflect.DeepEqual(order, []string{"proof"}) {
		t.Fatalf("rollback ran without proof: order %v, err %v", order, err)
	}
}

func TestPDNSConfigJournalStructureCarriesOwnerAwareMetadata(t *testing.T) {
	useTestPDNSConfigPaths(t)
	fake := newFakePDNSConfigFS(109)
	if err := validatePDNSConfigSnapshotSetStructure(fake.snapshots()); err != nil {
		t.Fatalf("Ubuntu owner metadata rejected by journal: %v", err)
	}
	fake = newFakePDNSConfigFS(0)
	if err := validatePDNSConfigSnapshotSetStructure(fake.snapshots()); err != nil {
		t.Fatalf("root-owned compatibility metadata rejected by journal: %v", err)
	}

	managedPath := filepath.Clean(dnsManagedConf)
	unsafe := fake.snapshots()
	for index := range unsafe {
		if unsafe[index].Path == managedPath {
			unsafe[index] = testPDNSConfigSnapshot(
				dnsManagedConf, "unsafe\n", 0o644, 0, 109,
			)
		}
	}
	if err := validatePDNSConfigSnapshotSetStructure(unsafe); err == nil {
		t.Fatal("group-writable-owner drop-in metadata accepted by journal")
	}
}
