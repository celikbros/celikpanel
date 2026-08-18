//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	stateDir string,
) pdnsSecondaryWriteGuardFixture {
	t.Helper()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)

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

	domain := "blocked-secondary.example.test"
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, state.EngineEpoch, 1, domain, false, "MASTER",
		testPDNSEngineRecords(domain),
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
	manager, root := newMutationTestManager(t)
	fixture := preparePDNSSecondaryWriteGuardFixture(
		t, filepath.Join(root, "state"),
	)
	binding := mutationTestBinding()
	if _, err := (hostDNSEngineBackend{}).Sync(
		context.Background(), fixture.commitment, binding,
	); err == nil || err.Error() != pdnsSecondaryWriteDeniedError {
		t.Fatalf("host rejection error=%v", err)
	}
	fixture.assertSafe()

	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_zone_sync", fixture.commitment.Domain, fixture.commitment.Qualifier,
	)
	t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
	useFakeDNSEngineBackend(t, hostDNSEngineBackend{})
	request := SyncDNSZoneV3Request{
		ServiceMutationBinding: binding,
		Engine:                 transport.DNSEngine(fixture.commitment.Engine), EngineEpoch: fixture.commitment.EngineEpoch,
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
}

func TestRecoverDNSZoneV3RejectsPDNSSecondaryBeforeHostMutation(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := preparePDNSSecondaryWriteGuardFixture(t, stateDir)
	exact, err := (hostDNSEngineBackend{}).RecoverZone(
		context.Background(), fixture.commitment.Domain,
		fixture.commitment.Qualifier, mutationTestBinding(),
	)
	if exact || err == nil || err.Error() != pdnsSecondaryWriteDeniedError {
		t.Fatalf("recovery exact=%v error=%v", exact, err)
	}
	fixture.assertSafe()
}
