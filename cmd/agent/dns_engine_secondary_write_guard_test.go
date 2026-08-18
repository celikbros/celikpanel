//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type pdnsSecondaryWriteGuardFixture struct {
	state      dnsEngineStateReceipt
	commitment mutationpayload.DNSZoneSyncV3Commitment
	assertSafe func()
}

func preparePDNSSecondaryWriteGuardFixture(
	t *testing.T,
	stateDir, operation string,
) pdnsSecondaryWriteGuardFixture {
	t.Helper()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	manifest := testPDNSPairSecondaryReconfigureManifest(t)
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: transport.DNSEngineSwitchModeSwitch,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: manifest.TargetEpoch,
		PairRole: transport.DNSPairRoleSecondary, SourceRevision: manifest.SourceRevision,
		ManifestQualifier: manifest.Qualifier,
		MutationRequestID: testMutationRequestID, MutationOwnerID: testMutationOwnerID,
	}
	if err := writeDNSEngineState(state); err != nil {
		t.Fatal(err)
	}

	domain := operation + ".blocked-secondary.example.test"
	deleteZone := operation == "delete"
	records := testPDNSEngineRecords(domain)
	if deleteZone {
		records = nil
	}
	generation := int64(1)
	if operation == "update" {
		generation = 2
	} else if deleteZone {
		generation = 3
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, state.EngineEpoch, generation,
		domain, deleteZone, "MASTER", records,
	)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "pdns.sqlite3")
	initializeEmptyPDNSReconfigureDB(t, dbPath)
	t.Setenv("CELIKPANEL_PDNS_DB", dbPath)
	prepareManagedDNSReadinessTest(t, dbPath)
	if err := os.WriteFile(dnsClusterConf, []byte("must-not-change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		dnsEngineSwitchJournalPath(), []byte("must-not-be-read-or-rewritten\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		dbPath, dnsManagedConf, dnsMainConf, dnsClusterConf,
		dnsEngineStatePath(), dnsEngineSwitchJournalPath(),
	}
	before := make(map[string][]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	assertPDNSSecondaryDomainAbsent(t, dbPath, domain)
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if _, err := os.Lstat(dbPath + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("fixture retained SQLite sidecar %s: %v", suffix, err)
		}
	}

	oldLookPath := dnsClusterLookPath
	lookups := 0
	dnsClusterLookPath = func(string) (string, error) {
		lookups++
		return "", errors.New("raw runner lookup must not escape")
	}
	t.Cleanup(func() { dnsClusterLookPath = oldLookPath })

	return pdnsSecondaryWriteGuardFixture{
		state: state, commitment: commitment,
		assertSafe: func() {
			t.Helper()
			if lookups != 0 {
				t.Fatalf("secondary rejection performed %d runner lookups", lookups)
			}
			for _, path := range paths {
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before[path], after) {
					t.Fatalf("secondary rejection changed %s", path)
				}
			}
			assertPDNSSecondaryDomainAbsent(t, dbPath, domain)
			for _, suffix := range []string{"-journal", "-wal", "-shm"} {
				if _, err := os.Lstat(dbPath + suffix); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("secondary rejection created SQLite sidecar %s: %v", suffix, err)
				}
			}
		},
	}
}

func assertPDNSSecondaryDomainAbsent(t *testing.T, path, domain string) {
	t.Helper()
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM domains WHERE name = ? COLLATE NOCASE`, domain,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("directional secondary created panel-local domain %q", domain)
	}
}

func TestSyncDNSZoneV3RejectsPDNSSecondaryBeforeHostMutation(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		t.Run(operation+"/direct", func(t *testing.T) {
			root := mutationTestRoot(t)
			fixture := preparePDNSSecondaryWriteGuardFixture(
				t, filepath.Join(root, "state"), operation,
			)
			if _, err := (hostDNSEngineBackend{}).Sync(
				context.Background(), fixture.commitment, mutationTestBinding(),
			); err == nil || err.Error() != dnsSecondaryWriteDeniedError {
				t.Fatalf("host rejection error=%v", err)
			}
			fixture.assertSafe()
		})
		t.Run(operation+"/rpc", func(t *testing.T) {
			manager, root := newMutationTestManager(t)
			fixture := preparePDNSSecondaryWriteGuardFixture(
				t, filepath.Join(root, "state"), operation,
			)
			installGlobalMutationTestManager(t, manager)
			beginMutationTestJobWithIdentity(
				t, manager, "dns_zone_sync", fixture.commitment.Domain, fixture.commitment.Qualifier,
			)
			t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
			useFakeDNSEngineBackend(t, hostDNSEngineBackend{})
			request := SyncDNSZoneV3Request{
				ServiceMutationBinding: mutationTestBinding(),
				Engine:                 transport.DNSEngine(fixture.commitment.Engine),
				EngineEpoch:            fixture.commitment.EngineEpoch,
				DesiredGeneration:      fixture.commitment.DesiredGeneration,
				Domain:                 fixture.commitment.Domain,
				Delete:                 fixture.commitment.Delete,
				ZoneType:               fixture.commitment.ZoneType,
				Records:                fixture.commitment.Records,
			}
			var response SyncDNSZoneV3Response
			if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
				t.Fatal(err)
			}
			if response.Synced || response.AppliedGeneration != 0 ||
				response.Error != "DNS zone publication failed; inspect the agent log" {
				t.Fatalf("unbounded secondary client response=%+v", response)
			}
			fixture.assertSafe()
		})
	}
}

func TestRecoverDNSZoneV3RejectsPDNSSecondaryBeforeHostMutation(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := preparePDNSSecondaryWriteGuardFixture(t, stateDir, "update")
	exact, err := (hostDNSEngineBackend{}).RecoverZone(
		context.Background(), fixture.commitment.Domain,
		fixture.commitment.Qualifier, mutationTestBinding(),
	)
	if exact || err == nil || err.Error() != dnsSecondaryWriteDeniedError {
		t.Fatalf("recovery exact=%v error=%v", exact, err)
	}
	fixture.assertSafe()
}

type bindSecondaryWriteGuardFixture struct {
	state      dnsEngineStateReceipt
	commitment mutationpayload.DNSZoneSyncV3Commitment
	assertSafe func()
}

func snapshotSecondaryGuardTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snapshot := map[string][]byte{}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			snapshot[relative+string(os.PathSeparator)] = nil
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[relative] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func prepareBINDSecondaryWriteGuardFixture(
	t *testing.T,
	stateDir, operation string,
) bindSecondaryWriteGuardFixture {
	t.Helper()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND, 0, 1, 7,
		transport.DNSTopologyPaired, transport.DNSPairRoleSecondary,
		"192.0.2.20", "ns2.example.test",
		"192.0.2.10", "ns1.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: transport.DNSEngineSwitchModeSwitch,
		Engine: transport.DNSEngineBIND, EngineEpoch: manifest.TargetEpoch,
		Generation: strings.Repeat("d", 64), PairRole: transport.DNSPairRoleSecondary,
		SourceRevision: manifest.SourceRevision, ManifestQualifier: manifest.Qualifier,
		MutationRequestID: testMutationRequestID, MutationOwnerID: testMutationOwnerID,
	}
	if err := writeDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	journal := []byte("must-not-be-read-or-rewritten\n")
	if err := os.WriteFile(dnsEngineSwitchJournalPath(), journal, 0o600); err != nil {
		t.Fatal(err)
	}

	domain := operation + ".blocked-secondary.example.test"
	deleteZone := operation == "delete"
	records := testPDNSEngineRecords(domain)
	if deleteZone {
		records = nil
	}
	generation := int64(1)
	if operation == "update" {
		generation = 2
	} else if deleteZone {
		generation = 3
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, state.EngineEpoch, generation,
		domain, deleteZone, "MASTER", records,
	)
	if err != nil {
		t.Fatal(err)
	}
	hostSurface := filepath.Join(t.TempDir(), "bind-host-surface")
	for path, data := range map[string][]byte{
		filepath.Join(hostSurface, "generation", "current"):                        []byte(state.Generation + "\n"),
		filepath.Join(hostSurface, "generation", state.Generation, "receipt.json"): []byte("generation receipt sentinel\n"),
		filepath.Join(hostSurface, "generation", state.Generation, "zones.conf"):   []byte("generation config sentinel\n"),
		filepath.Join(hostSurface, "etc", "named.conf"):                            []byte("main config sentinel\n"),
		filepath.Join(hostSurface, "etc", "named.conf.options"):                    []byte("options config sentinel\n"),
		filepath.Join(hostSurface, "etc", "named.conf.local"):                      []byte("local config sentinel\n"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	beforeTree := snapshotSecondaryGuardTree(t, hostSurface)
	stateBefore, err := os.ReadFile(dnsEngineStatePath())
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(dnsEngineSwitchJournalPath())
	if err != nil {
		t.Fatal(err)
	}
	oldDetector := detectHostPlatform
	hostDetections := 0
	detectHostPlatform = func() (hostplatform.Profile, error) {
		hostDetections++
		return hostplatform.Profile{}, errors.New("BIND host work must not start")
	}
	t.Cleanup(func() { detectHostPlatform = oldDetector })

	return bindSecondaryWriteGuardFixture{
		state: state, commitment: commitment,
		assertSafe: func() {
			t.Helper()
			if hostDetections != 0 {
				t.Fatalf("secondary rejection started %d BIND host operations", hostDetections)
			}
			stateAfter, err := os.ReadFile(dnsEngineStatePath())
			if err != nil || !bytes.Equal(stateBefore, stateAfter) {
				t.Fatalf("secondary rejection changed engine state: %v", err)
			}
			journalAfter, err := os.ReadFile(dnsEngineSwitchJournalPath())
			if err != nil || !bytes.Equal(journalBefore, journalAfter) {
				t.Fatalf("secondary rejection changed switch journal: %v", err)
			}
			if afterTree := snapshotSecondaryGuardTree(t, hostSurface); !reflect.DeepEqual(beforeTree, afterTree) {
				t.Fatalf("secondary rejection changed BIND generation/config surface: before=%v after=%v", beforeTree, afterTree)
			}
		},
	}
}

func TestSyncDNSZoneV3RejectsBINDSecondaryBeforeHostMutation(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		t.Run(operation+"/direct", func(t *testing.T) {
			root := mutationTestRoot(t)
			fixture := prepareBINDSecondaryWriteGuardFixture(
				t, filepath.Join(root, "state"), operation,
			)
			if _, err := (hostDNSEngineBackend{}).Sync(
				context.Background(), fixture.commitment, mutationTestBinding(),
			); err == nil || err.Error() != dnsSecondaryWriteDeniedError {
				t.Fatalf("host rejection error=%v", err)
			}
			fixture.assertSafe()
		})
		t.Run(operation+"/rpc", func(t *testing.T) {
			manager, root := newMutationTestManager(t)
			fixture := prepareBINDSecondaryWriteGuardFixture(
				t, filepath.Join(root, "state"), operation,
			)
			installGlobalMutationTestManager(t, manager)
			beginMutationTestJobWithIdentity(
				t, manager, "dns_zone_sync", fixture.commitment.Domain, fixture.commitment.Qualifier,
			)
			t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
			useFakeDNSEngineBackend(t, hostDNSEngineBackend{})
			request := SyncDNSZoneV3Request{
				ServiceMutationBinding: mutationTestBinding(),
				Engine:                 transport.DNSEngineBIND, EngineEpoch: fixture.commitment.EngineEpoch,
				DesiredGeneration: fixture.commitment.DesiredGeneration,
				Domain:            fixture.commitment.Domain, Delete: fixture.commitment.Delete,
				ZoneType: fixture.commitment.ZoneType, Records: fixture.commitment.Records,
			}
			var response SyncDNSZoneV3Response
			if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
				t.Fatal(err)
			}
			if response.Synced || response.AppliedGeneration != 0 ||
				response.Error != "DNS zone publication failed; inspect the agent log" {
				t.Fatalf("unbounded secondary client response=%+v", response)
			}
			fixture.assertSafe()
		})
	}
}

func TestRecoverDNSZoneV3RejectsBINDSecondaryBeforeHostMutation(t *testing.T) {
	root := mutationTestRoot(t)
	fixture := prepareBINDSecondaryWriteGuardFixture(
		t, filepath.Join(root, "state"), "update",
	)
	exact, err := (hostDNSEngineBackend{}).RecoverZone(
		context.Background(), fixture.commitment.Domain,
		fixture.commitment.Qualifier, mutationTestBinding(),
	)
	if exact || err == nil || err.Error() != dnsSecondaryWriteDeniedError {
		t.Fatalf("recovery exact=%v error=%v", exact, err)
	}
	fixture.assertSafe()
}
