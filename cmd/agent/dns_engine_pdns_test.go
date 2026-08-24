package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func testPDNSPairSecondaryReconfigureManifest(
	t *testing.T,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEnginePowerDNS,
		0, 1, 7, transport.DNSTopologyPaired,
		transport.DNSPairRoleSecondary,
		"192.0.2.20", "ns2.example.test",
		"192.0.2.10", "ns1.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestPDNSPairSecondaryReconfigureManifestIsExact(t *testing.T) {
	manifest := testPDNSPairSecondaryReconfigureManifest(t)
	if !isPDNSPairSecondaryReconfigureManifest(manifest) {
		t.Fatal("exact paired-secondary reconfigure manifest was rejected")
	}
	for _, test := range []struct {
		name   string
		mutate func(*mutationpayload.DNSEngineSwitchManifestCommitment)
	}{
		{
			name: "source engine",
			mutate: func(value *mutationpayload.DNSEngineSwitchManifestCommitment) {
				value.SourceEngine = transport.DNSEngineBIND
				value.SourceEpoch = 1
				value.TargetEpoch = 2
			},
		},
		{
			name: "primary",
			mutate: func(value *mutationpayload.DNSEngineSwitchManifestCommitment) {
				value.PairRole = transport.DNSPairRolePrimary
			},
		},
		{
			name: "zone",
			mutate: func(value *mutationpayload.DNSEngineSwitchManifestCommitment) {
				value.Zones = []transport.DNSEngineSwitchZoneSnapshot{{
					Domain: "unexpected.test",
				}}
			},
		},
		{
			name: "standalone",
			mutate: func(value *mutationpayload.DNSEngineSwitchManifestCommitment) {
				value.Topology = transport.DNSTopologyStandalone
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := manifest
			test.mutate(&changed)
			if isPDNSPairSecondaryReconfigureManifest(changed) {
				t.Fatal("non-exact reconfigure manifest was accepted")
			}
		})
	}
}

func TestClassifyPDNSPairSecondarySourceSeparatesFreshFromReconfigure(t *testing.T) {
	manifest := testPDNSPairSecondaryReconfigureManifest(t)
	tests := []struct {
		name        string
		stateExists bool
		bindActive  bool
		aliasActive bool
		pdnsActive  bool
		want        pdnsPairSecondarySourceClass
		wantErr     bool
	}{
		{name: "fresh source with no running authority", want: pdnsPairSecondarySourceFresh},
		{name: "unreceipted running PowerDNS", pdnsActive: true, want: pdnsPairSecondarySourceReconfigure},
		{name: "durable source state", stateExists: true, wantErr: true},
		{name: "running named authority", bindActive: true, wantErr: true},
		{name: "running bind alias authority", aliasActive: true, wantErr: true},
		{name: "mixed PowerDNS and BIND authorities", bindActive: true, pdnsActive: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := classifyPDNSPairSecondarySource(
				manifest, test.stateExists, test.bindActive,
				test.aliasActive, test.pdnsActive,
			)
			if test.wantErr {
				if err == nil {
					t.Fatal("unsafe paired-secondary source was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("source class = %d, want %d", got, test.want)
			}
		})
	}
}

func TestPDNSPairSecondaryPackagePolicyKeepsReconfigureNoInstallInvariant(
	t *testing.T,
) {
	fresh := dnsEngineSwitchSourceProof{}
	if err := validatePDNSSwitchPackagePolicy(fresh, 2); err != nil {
		t.Fatalf("fresh install rejected missing packages: %v", err)
	}
	reconfigure := dnsEngineSwitchSourceProof{PDNSPairSecondaryReconfigure: true}
	if err := validatePDNSSwitchPackagePolicy(reconfigure, 0); err != nil {
		t.Fatalf("installed reconfiguration was rejected: %v", err)
	}
	if err := validatePDNSSwitchPackagePolicy(reconfigure, 1); err == nil {
		t.Fatal("secondary reconfiguration was allowed to install a package")
	}
}

func TestPDNSSwitchSourceProofCASRejectsFreshReconfigureChanges(t *testing.T) {
	fresh := dnsEngineSwitchSourceProof{}
	reconfigure := dnsEngineSwitchSourceProof{PDNSPairSecondaryReconfigure: true}
	for _, proof := range []dnsEngineSwitchSourceProof{fresh, reconfigure} {
		if err := validatePDNSSwitchSourceProofCAS(proof, proof); err != nil {
			t.Fatalf("stable source proof was rejected: %v", err)
		}
	}
	if err := validatePDNSSwitchSourceProofCAS(fresh, reconfigure); err == nil {
		t.Fatal("fresh-to-reconfigure source change was accepted")
	}
	if err := validatePDNSSwitchSourceProofCAS(reconfigure, fresh); err == nil {
		t.Fatal("reconfigure-to-fresh source change was accepted")
	}
}

func TestDirectPDNSSwitchRollbackRemovesJournalOnlyAfterFinalWrite(t *testing.T) {
	t.Run("final write failure retains journal", func(t *testing.T) {
		writeErr := errors.New("durable final phase write failed")
		journal := dnsEngineSwitchJournal{Phase: dnsSwitchPhaseRollingBack}
		var order []string
		err := finishDNSSwitchRollbackJournal(
			&journal,
			func(current dnsEngineSwitchJournal) error {
				order = append(order, "write:"+current.Phase)
				return writeErr
			},
			func() error {
				order = append(order, "remove")
				return nil
			},
		)
		if !errors.Is(err, writeErr) ||
			strings.Join(order, ",") != "write:"+dnsSwitchPhaseRolledBack ||
			journal.Phase != dnsSwitchPhaseRolledBack {
			t.Fatalf(
				"failed final write did not retain journal: phase=%q order=%v err=%v",
				journal.Phase, order, err,
			)
		}
	})

	t.Run("successful final write precedes removal", func(t *testing.T) {
		journal := dnsEngineSwitchJournal{Phase: dnsSwitchPhaseRollingBack}
		var order []string
		err := finishDNSSwitchRollbackJournal(
			&journal,
			func(current dnsEngineSwitchJournal) error {
				order = append(order, "write:"+current.Phase)
				return nil
			},
			func() error {
				order = append(order, "remove")
				return nil
			},
		)
		if err != nil ||
			strings.Join(order, ",") !=
				"write:"+dnsSwitchPhaseRolledBack+",remove" {
			t.Fatalf("ordered finalization order=%v err=%v", order, err)
		}
	})
}

func initializeEmptyPDNSReconfigureDB(t *testing.T, path string) {
	t.Helper()
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyEmptyStandalonePDNSDatabaseIsByteExact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	initializeEmptyPDNSReconfigureDB(t, path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyEmptyStandalonePDNSDatabase(
		context.Background(), path,
	); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only empty PowerDNS proof changed database bytes")
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("read-only proof retained sidecar %s: %v", suffix, err)
		}
	}
}

func TestVerifyEmptyStandalonePDNSDatabaseRejectsEveryAuthorityTable(
	t *testing.T,
) {
	tests := []struct {
		table string
		stmt  string
	}{
		{"domains", `INSERT INTO domains (name, type) VALUES ('zone.test', 'NATIVE')`},
		{"records", `INSERT INTO records (name, type, content) VALUES ('zone.test', 'A', '192.0.2.10')`},
		{"supermasters", `INSERT INTO supermasters (ip, nameserver, account) VALUES ('192.0.2.10', 'ns1.test', 'celikpanel')`},
		{"comments", `INSERT INTO comments (domain_id, name, type, modified_at, comment) VALUES (1, 'zone.test', 'A', 1, 'comment')`},
		{"domainmetadata", `INSERT INTO domainmetadata (domain_id, kind, content) VALUES (1, 'PRESIGNED', '1')`},
		{"cryptokeys", `INSERT INTO cryptokeys (domain_id, flags, active, content) VALUES (1, 257, 1, 'key')`},
		{"tsigkeys", `INSERT INTO tsigkeys (name, algorithm, secret) VALUES ('key', 'hmac-sha256', 'secret')`},
		{"celikpanel_dns_zone_sync_receipts", `INSERT INTO celikpanel_dns_zone_sync_receipts (domain, request_id, qualifier, desired_generation, action, zone_type, schema) VALUES ('zone.test', 'request', 'qualifier', 1, 'sync', 'NATIVE', 'legacy')`},
		{"celikpanel_dns_zone_sync_v3_receipts", `INSERT INTO celikpanel_dns_zone_sync_v3_receipts (domain, engine, engine_epoch, request_id, owner_id, qualifier, desired_generation, action, zone_type, schema) VALUES ('zone.test', 'pdns', 1, 'request', 'owner', 'qualifier', 1, 'sync', 'NATIVE', 'dns-zone-sync/v3')`},
		{"celikpanel_dns_engine_manifest_receipt", `INSERT INTO celikpanel_dns_engine_manifest_receipt (singleton, engine, engine_epoch, request_id, owner_id, qualifier, source_revision, zone_count, snapshot_bytes, schema) VALUES (1, 'pdns', 1, 'request', 'owner', 'qualifier', 1, 0, 0, 'dns-engine-switch/v1')`},
	}
	for _, test := range tests {
		t.Run(test.table, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pdns.sqlite3")
			initializeEmptyPDNSReconfigureDB(t, path)
			db, err := openPDNSEngineDB(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.stmt); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if err := verifyEmptyStandalonePDNSDatabase(
				context.Background(), path,
			); err == nil || !strings.Contains(err.Error(), test.table) {
				t.Fatalf("nonempty %s proof error=%v", test.table, err)
			}
		})
	}
}

func TestVerifyEmptyStandalonePDNSDatabaseAcceptsLegacySchemaAndRejectsExtras(
	t *testing.T,
) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		DROP TABLE celikpanel_dns_zone_sync_v3_receipts;
		DROP TABLE celikpanel_dns_engine_manifest_receipt;
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyEmptyStandalonePDNSDatabase(
		context.Background(), path,
	); err != nil {
		t.Fatalf("legacy empty schema rejected: %v", err)
	}
	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE manual_authority (value TEXT)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyEmptyStandalonePDNSDatabase(
		context.Background(), path,
	); err == nil || !strings.Contains(err.Error(), "unrecognized table") {
		t.Fatalf("unexpected table proof error=%v", err)
	}
}

func TestVerifyEmptyStandalonePDNSDatabaseRejectsSidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	initializeEmptyPDNSReconfigureDB(t, path)
	if err := os.WriteFile(path+"-journal", []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyEmptyStandalonePDNSDatabase(
		context.Background(), path,
	); err == nil || !strings.Contains(err.Error(), "sidecar") {
		t.Fatalf("sidecar proof error=%v", err)
	}
}

func testPDNSEngineBinding() transport.ServiceMutationBinding {
	return transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("a", 32),
		MutationOwnerID:   strings.Repeat("b", 32),
	}
}

func testPDNSEngineRecords(domain string) []transport.ZoneRecord {
	return []transport.ZoneRecord{
		{Name: domain, Type: "SOA", Content: "ns1.example.net hostmaster." + domain + " 2026081601 10800 3600 604800 3600", TTL: 3600},
		{Name: domain, Type: "NS", Content: "ns1.example.net", TTL: 3600},
		{Name: "www." + domain, Type: "A", Content: "192.0.2.10", TTL: 300},
		{Name: "hidden." + domain, Type: "TXT", Content: "disabled remains committed", TTL: 60, Disabled: true},
		{Name: "www." + domain, Type: "A", Content: "192.0.2.10", TTL: 300},
	}
}

func testPDNSSwitchManifest(t *testing.T) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	domain := "example.test"
	zone, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 1, 7, domain, false, "NATIVE",
		testPDNSEngineRecords(domain),
	)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 1, 2, "deleted.test", true, "NATIVE", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEnginePowerDNS, 0, 1, 11,
		transport.DNSTopologyStandalone,
		[]transport.DNSEngineSwitchZoneSnapshot{
			{Domain: domain, DesiredGeneration: 7, ZoneType: "NATIVE", Records: zone.Records, ZoneQualifier: zone.Qualifier},
			{Domain: "deleted.test", DesiredGeneration: 2, Delete: true, ZoneType: "NATIVE", ZoneQualifier: deleted.Qualifier},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestBuildPDNSSwitchCandidatePreservesExactSnapshotAndReceipts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.sqlite3")
	manifest := testPDNSSwitchManifest(t)
	binding := testPDNSEngineBinding()
	if err := buildPDNSSwitchCandidate(context.Background(), path, manifest, binding); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSSwitchDatabase(context.Background(), path, manifest, binding); err != nil {
		t.Fatal(err)
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var total, disabled int
	if err := db.QueryRow(`SELECT COUNT(*), SUM(disabled) FROM records`).Scan(&total, &disabled); err != nil {
		t.Fatal(err)
	}
	if total != len(testPDNSEngineRecords("example.test")) || disabled != 1 {
		t.Fatalf("records total=%d disabled=%d", total, disabled)
	}
	var tombstoneCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM celikpanel_dns_zone_sync_v3_receipts
		WHERE domain = 'deleted.test' AND action = 'delete'
	`).Scan(&tombstoneCount); err != nil || tombstoneCount != 1 {
		t.Fatalf("delete tombstone count=%d err=%v", tombstoneCount, err)
	}
}

func testPairedPDNSSwitchManifest(
	t *testing.T,
	role string,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEngineBIND, transport.DNSEnginePowerDNS,
		3, 4, 9, transport.DNSTopologyPaired,
		role, "192.0.2.10", "ns1.example.test",
		"192.0.2.20", "ns2.example.test", zones,
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func prepareDirectionalPDNSV3Fixture(
	t *testing.T,
) (string, mutationpayload.DNSZoneSyncV3Commitment, transport.ServiceMutationBinding, dnsEngineStateReceipt) {
	t.Helper()
	prepareManagedPDNSCatalogConfig(t)
	config, err := dnsDirectionalClusterConfig(
		transport.DNSPairRolePrimary, "192.0.2.10", "192.0.2.20",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsClusterConf, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	existingDomain := "existing-anchor.test"
	existing, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 4, 1, existingDomain, false, "MASTER",
		testPDNSEngineRecords(existingDomain),
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testPairedPDNSSwitchManifest(
		t, transport.DNSPairRolePrimary,
		[]transport.DNSEngineSwitchZoneSnapshot{{
			Domain: existing.Domain, DesiredGeneration: existing.DesiredGeneration,
			ZoneType: existing.ZoneType, Records: existing.Records,
			ZoneQualifier: existing.Qualifier,
		}},
	)
	path := filepath.Join(t.TempDir(), "directional-primary.sqlite3")
	binding := testPDNSEngineBinding()
	const catalogSerial = uint32(41)
	if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
		context.Background(), path, manifest, binding, catalogSerial,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, manifest.TargetEpoch, 1,
		"strict-anchor.test", false, "MASTER",
		testPDNSEngineRecords("strict-anchor.test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: manifest.Mode,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: manifest.TargetEpoch,
		PairRole: manifest.PairRole, PairLocalIP: manifest.LocalIP,
		PairPeerIP: manifest.PeerIP, PrimaryCatalogSerial: catalogSerial,
		SourceRevision: manifest.SourceRevision, ManifestQualifier: manifest.Qualifier,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
	}
	return path, commitment, binding, state
}

func assertPDNSTestZoneAbsent(t *testing.T, path, domain string) {
	t.Helper()
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE name = ?`, domain).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("PowerDNS zone %q changed before strict authority proof", domain)
	}
}

func TestApplyPDNSV3DirectionalStateAnchorsReceiptSchemaSerialAndPeer(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		path, commitment, binding, state := prepareDirectionalPDNSV3Fixture(t)
		if err := applyPDNSV3ZoneDatabaseForState(
			context.Background(), path, commitment, binding, state,
		); err != nil {
			t.Fatal(err)
		}
	})

	for _, test := range []struct {
		name   string
		tamper func(*testing.T, string, *dnsEngineStateReceipt)
	}{
		{
			name: "manifest receipt",
			tamper: func(t *testing.T, path string, _ *dnsEngineStateReceipt) {
				db, err := openPDNSEngineDB(path, false)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.Exec(`UPDATE celikpanel_dns_engine_manifest_receipt SET owner_id = ? WHERE singleton = 1`, strings.Repeat("f", 32)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "receipt trigger",
			tamper: func(t *testing.T, path string, _ *dnsEngineStateReceipt) {
				db, err := openPDNSEngineDB(path, false)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.Exec(`CREATE TRIGGER hostile_receipt_trigger AFTER UPDATE ON celikpanel_dns_engine_manifest_receipt BEGIN SELECT 1; END`); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "receipt view",
			tamper: func(t *testing.T, path string, _ *dnsEngineStateReceipt) {
				db, err := openPDNSEngineDB(path, false)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.Exec(`DROP TABLE celikpanel_dns_engine_manifest_receipt`); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`CREATE VIEW celikpanel_dns_engine_manifest_receipt AS SELECT 1 AS singleton, 'pdns' AS engine, 4 AS engine_epoch, '' AS request_id, '' AS owner_id, '' AS qualifier, 0 AS source_revision, 0 AS zone_count, 0 AS snapshot_bytes, '' AS schema`); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "catalog serial rollback",
			tamper: func(_ *testing.T, _ string, state *dnsEngineStateReceipt) {
				state.PrimaryCatalogSerial++
			},
		},
		{
			name: "configuration peer",
			tamper: func(t *testing.T, _ string, _ *dnsEngineStateReceipt) {
				config, err := dnsDirectionalClusterConfig(
					transport.DNSPairRolePrimary, "192.0.2.10", "192.0.2.30",
				)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dnsClusterConf, []byte(config), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, commitment, binding, state := prepareDirectionalPDNSV3Fixture(t)
			test.tamper(t, path, &state)
			if _, _, err := readManagedPDNSPrimaryCatalogForState(
				context.Background(), state,
			); err == nil {
				t.Fatal("tampered directional PowerDNS authority passed read-only readiness evidence")
			}
			if err := applyPDNSV3ZoneDatabaseForState(
				context.Background(), path, commitment, binding, state,
			); err == nil {
				t.Fatal("tampered directional PowerDNS authority accepted a V3 mutation")
			}
			assertPDNSTestZoneAbsent(t, path, commitment.Domain)
		})
	}
}

func testPairedPDNSSourceManifest(
	t *testing.T, role string,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	return testPairedPDNSSourceManifestAt(t, role, "192.0.2.10")
}

func testPairedPDNSSourceManifestAt(
	t *testing.T, role, localIP string,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	return testPairedPDNSSourceManifestWithZones(t, role, localIP, nil)
}

func testPairedPDNSSourceManifestWithZones(
	t *testing.T, role, localIP string,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		4, 5, 9, transport.DNSTopologyPaired, role,
		localIP, "ns1.example.test",
		"192.0.2.20", "ns2.example.test", zones,
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func assertPDNSPrimarySourcePreflightRejectsDrift(
	t *testing.T,
	path string,
	verify func() error,
	mutate string,
	mutateArgs []any,
	restore string,
	restoreArgs []any,
) {
	t.Helper()
	db, err := openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(mutate, mutateArgs...); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	beforeDB, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeConfig, configErr := os.ReadFile(dnsClusterConf)
	beforeConfigExists := configErr == nil
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		t.Fatal(configErr)
	}
	if err := verify(); err == nil {
		t.Fatal("drifted PowerDNS primary source passed the preflight")
	}
	afterDB, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterConfig, configErr := os.ReadFile(dnsClusterConf)
	afterConfigExists := configErr == nil
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		t.Fatal(configErr)
	}
	if !bytes.Equal(afterDB, beforeDB) ||
		beforeConfigExists != afterConfigExists ||
		!bytes.Equal(afterConfig, beforeConfig) {
		t.Fatal("PowerDNS source preflight failure changed durable authority")
	}
	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(restore, restoreArgs...); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verify(); err != nil {
		t.Fatalf("restored PowerDNS primary source rejected: %v", err)
	}
}

func TestVerifyStandalonePDNSSourceProjectionRejectsExtraRecords(t *testing.T) {
	candidate := testPDNSSwitchManifest(t)
	path := filepath.Join(t.TempDir(), "standalone-source.sqlite3")
	binding := testPDNSEngineBinding()
	if err := buildPDNSSwitchCandidate(
		context.Background(), path, candidate, binding,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	prepareManagedDNSReadinessTest(t, path)
	sourceZones := make([]transport.DNSEngineSwitchZoneSnapshot, 0, len(candidate.Zones))
	for _, zone := range candidate.Zones {
		target, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEngineBIND, 2, zone.DesiredGeneration,
			zone.Domain, zone.Delete, zone.ZoneType, zone.Records,
		)
		if err != nil {
			t.Fatal(err)
		}
		sourceZones = append(sourceZones, transport.DNSEngineSwitchZoneSnapshot{
			Ordinal: zone.Ordinal, Domain: zone.Domain,
			DesiredGeneration: zone.DesiredGeneration, Delete: zone.Delete,
			ZoneType: zone.ZoneType, Records: target.Records,
			ZoneQualifier: target.Qualifier,
		})
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		1, 2, 12, transport.DNSTopologyStandalone, sourceZones,
	)
	if err != nil {
		t.Fatal(err)
	}
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: candidate.Mode,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: candidate.TargetEpoch,
		SourceRevision:    candidate.SourceRevision,
		ManifestQualifier: candidate.Qualifier,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
	}
	verify := func() error {
		return verifyUnsignedPowerDNSForManifest(context.Background(), manifest, state)
	}
	if err := verify(); err != nil {
		t.Fatalf("exact standalone PowerDNS source rejected: %v", err)
	}
	assertPDNSPrimarySourcePreflightRejectsDrift(
		t, path, verify,
		`INSERT INTO records(domain_id,name,type,content,ttl,prio,disabled,auth)
		 SELECT id,name,'TXT','uncommitted',300,0,0,1 FROM domains WHERE name = ?`,
		[]any{"example.test"},
		`DELETE FROM records WHERE domain_id =
		 (SELECT id FROM domains WHERE name = ?) AND type = 'TXT' AND content = 'uncommitted'`,
		[]any{"example.test"},
	)
}

func TestVerifyDirectionalPDNSPrimarySourceProjectionRejectsRowDrift(t *testing.T) {
	path, _, _, state := prepareDirectionalPDNSV3Fixture(t)
	domain := "existing-anchor.test"
	target, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 5, 1, domain, false, "MASTER",
		testPDNSEngineRecords(domain),
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testPairedPDNSSourceManifestWithZones(
		t, transport.DNSPairRolePrimary, "192.0.2.10",
		[]transport.DNSEngineSwitchZoneSnapshot{{
			Domain: domain, DesiredGeneration: 1, ZoneType: "MASTER",
			Records: target.Records, ZoneQualifier: target.Qualifier,
		}},
	)
	verify := func() error {
		return verifyUnsignedPowerDNSForManifest(context.Background(), manifest, state)
	}
	if err := verify(); err != nil {
		t.Fatalf("exact directional PowerDNS primary source rejected: %v", err)
	}
	for _, test := range []struct {
		name        string
		mutate      string
		mutateArgs  []any
		restore     string
		restoreArgs []any
	}{
		{
			name: "same SOA extra A record",
			mutate: `INSERT INTO records(domain_id,name,type,content,ttl,prio,disabled,auth)
			 SELECT id,name,'A','192.0.2.99',300,0,0,1 FROM domains WHERE name = ?`,
			mutateArgs: []any{domain},
			restore: `DELETE FROM records WHERE domain_id =
			 (SELECT id FROM domains WHERE name = ?) AND type = 'A' AND content = '192.0.2.99'`,
			restoreArgs: []any{domain},
		},
		{
			name:        "receipt generation",
			mutate:      `UPDATE celikpanel_dns_zone_sync_v3_receipts SET desired_generation = 2 WHERE domain = ?`,
			mutateArgs:  []any{domain},
			restore:     `UPDATE celikpanel_dns_zone_sync_v3_receipts SET desired_generation = 1 WHERE domain = ?`,
			restoreArgs: []any{domain},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertPDNSPrimarySourcePreflightRejectsDrift(
				t, path, verify,
				test.mutate, test.mutateArgs, test.restore, test.restoreArgs,
			)
		})
	}
}

func TestVerifyLegacyPDNSSourceRoleBindsReleasedSymmetricAuthority(t *testing.T) {
	t.Run("producer", func(t *testing.T) {
		prepareManagedPDNSCatalogConfig(t)
		domain := "legacy-producer.test"
		zone, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, 4, 1, domain, false, "MASTER",
			testPDNSEngineRecords(domain),
		)
		if err != nil {
			t.Fatal(err)
		}
		candidate := testPairedPDNSSwitchManifest(
			t, transport.DNSPairRolePrimary,
			[]transport.DNSEngineSwitchZoneSnapshot{{
				Domain: domain, DesiredGeneration: 1, ZoneType: "MASTER",
				Records: zone.Records, ZoneQualifier: zone.Qualifier,
			}},
		)
		path := filepath.Join(t.TempDir(), "legacy-producer.sqlite3")
		if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
			context.Background(), path, candidate, testPDNSEngineBinding(), 41,
		); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CELIKPANEL_PDNS_DB", path)
		sourceZone, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEngineBIND, 5, 1, domain, false, "MASTER",
			testPDNSEngineRecords(domain),
		)
		if err != nil {
			t.Fatal(err)
		}
		sourceManifest := testPairedPDNSSourceManifestWithZones(
			t, transport.DNSPairRolePrimary, "192.0.2.10",
			[]transport.DNSEngineSwitchZoneSnapshot{{
				Domain: domain, DesiredGeneration: 1, ZoneType: "MASTER",
				Records: sourceZone.Records, ZoneQualifier: sourceZone.Qualifier,
			}},
		)
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), sourceManifest,
		); err != nil {
			t.Fatalf("exact legacy producer role rejected: %v", err)
		}
		wrongQualifier, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, 4, 2, domain, false, "MASTER",
			testPDNSEngineRecords(domain),
		)
		if err != nil {
			t.Fatal(err)
		}
		verify := func() error {
			return verifyLegacyPDNSSourceRole(context.Background(), sourceManifest)
		}
		for _, test := range []struct {
			name        string
			mutate      string
			mutateArgs  []any
			restore     string
			restoreArgs []any
		}{
			{
				name: "same SOA extra A record",
				mutate: `INSERT INTO records(domain_id,name,type,content,ttl,prio,disabled,auth)
				 SELECT id,name,'A','192.0.2.99',300,0,0,1 FROM domains WHERE name = ?`,
				mutateArgs: []any{domain},
				restore: `DELETE FROM records WHERE domain_id =
				 (SELECT id FROM domains WHERE name = ?) AND type = 'A' AND content = '192.0.2.99'`,
				restoreArgs: []any{domain},
			},
			{
				name:        "record auth",
				mutate:      `UPDATE records SET auth = 0 WHERE domain_id = (SELECT id FROM domains WHERE name = ?) AND type = 'SOA'`,
				mutateArgs:  []any{domain},
				restore:     `UPDATE records SET auth = 1 WHERE domain_id = (SELECT id FROM domains WHERE name = ?) AND type = 'SOA'`,
				restoreArgs: []any{domain},
			},
			{
				name:        "receipt epoch",
				mutate:      `UPDATE celikpanel_dns_zone_sync_v3_receipts SET engine_epoch = 3 WHERE domain = ?`,
				mutateArgs:  []any{domain},
				restore:     `UPDATE celikpanel_dns_zone_sync_v3_receipts SET engine_epoch = 4 WHERE domain = ?`,
				restoreArgs: []any{domain},
			},
			{
				name:        "receipt qualifier",
				mutate:      `UPDATE celikpanel_dns_zone_sync_v3_receipts SET qualifier = ? WHERE domain = ?`,
				mutateArgs:  []any{wrongQualifier.Qualifier, domain},
				restore:     `UPDATE celikpanel_dns_zone_sync_v3_receipts SET qualifier = ? WHERE domain = ?`,
				restoreArgs: []any{zone.Qualifier, domain},
			},
			{
				name:        "receipt generation",
				mutate:      `UPDATE celikpanel_dns_zone_sync_v3_receipts SET desired_generation = 2 WHERE domain = ?`,
				mutateArgs:  []any{domain},
				restore:     `UPDATE celikpanel_dns_zone_sync_v3_receipts SET desired_generation = 1 WHERE domain = ?`,
				restoreArgs: []any{domain},
			},
			{
				name:        "receipt action",
				mutate:      `UPDATE celikpanel_dns_zone_sync_v3_receipts SET action = 'delete' WHERE domain = ?`,
				mutateArgs:  []any{domain},
				restore:     `UPDATE celikpanel_dns_zone_sync_v3_receipts SET action = 'sync' WHERE domain = ?`,
				restoreArgs: []any{domain},
			},
			{
				name:        "receipt zone type",
				mutate:      `UPDATE celikpanel_dns_zone_sync_v3_receipts SET zone_type = 'NATIVE' WHERE domain = ?`,
				mutateArgs:  []any{domain},
				restore:     `UPDATE celikpanel_dns_zone_sync_v3_receipts SET zone_type = 'MASTER' WHERE domain = ?`,
				restoreArgs: []any{domain},
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				assertPDNSPrimarySourcePreflightRejectsDrift(
					t, path, verify,
					test.mutate, test.mutateArgs, test.restore, test.restoreArgs,
				)
			})
		}
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), testPairedPDNSSourceManifest(t, transport.DNSPairRoleSecondary),
		); err == nil {
			t.Fatal("legacy producer was accepted as a secondary source")
		}
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), testPairedPDNSSourceManifestAt(
				t, transport.DNSPairRolePrimary, "192.0.2.30",
			),
		); err == nil {
			t.Fatal("legacy producer accepted a non-host local identity")
		}
	})

	t.Run("empty symmetric producer may become secondary", func(t *testing.T) {
		prepareManagedPDNSCatalogConfig(t)
		path := filepath.Join(t.TempDir(), "legacy-empty-producer.sqlite3")
		db, err := initializePDNSEngineDB(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO supermasters(ip,nameserver,account) VALUES('192.0.2.20','ns2.example.test','celikpanel')`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		seedManagedPDNSCatalog(t, path, "192.0.2.10")
		t.Setenv("CELIKPANEL_PDNS_DB", path)
		peerDomain, err := binddns.CatalogDomain("192.0.2.20")
		if err != nil {
			t.Fatal(err)
		}
		oldSOA := probeDNSZoneSOA
		oldBound := probeDNSBoundCatalogAXFR
		peerSerial := uint32(7)
		probeDNSBoundCatalogAXFR = func(
			_ context.Context, source, address, domain string,
		) (dnsCatalogAXFRResult, error) {
			if source != "192.0.2.10" || address != "192.0.2.20" || domain != peerDomain {
				t.Fatalf("peer proof tuple=%s/%s/%s", source, address, domain)
			}
			return dnsCatalogAXFRResult{Serial: peerSerial, Members: []string{}}, nil
		}
		probeDNSZoneSOA = func(
			_ context.Context, network, address, domain string,
		) (dnsSOAProbeResult, error) {
			if (network != "udp" && network != "tcp") ||
				address != "192.0.2.20" || domain != peerDomain {
				t.Fatalf("peer SOA tuple=%s/%s/%s", network, address, domain)
			}
			return dnsSOAProbeResult{
				Authoritative: true, RCode: dnsRCodeNoError,
				SOASerials: []uint32{7},
			}, nil
		}
		t.Cleanup(func() {
			probeDNSZoneSOA = oldSOA
			probeDNSBoundCatalogAXFR = oldBound
		})
		secondary := testPairedPDNSSourceManifest(t, transport.DNSPairRoleSecondary)
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), secondary,
		); err != nil {
			t.Fatalf("exact empty symmetric source rejected: %v", err)
		}
		peerSerial = 6
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), secondary,
		); err == nil {
			t.Fatal("stale peer producer proof authorized a secondary transition")
		}
		peerSerial = 7
		db, err = openPDNSEngineDB(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO supermasters(ip,nameserver,account) VALUES('192.0.2.99','manual.example.test','manual')`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), secondary,
		); err == nil {
			t.Fatal("extra manual supermaster passed the empty secondary proof")
		}
		db, err = openPDNSEngineDB(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`DELETE FROM supermasters WHERE account = 'manual'`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name, insert, cleanup string
		}{
			{
				name:    "TSIG key",
				insert:  `INSERT INTO tsigkeys(name,algorithm,secret) VALUES('hidden','hmac-sha256','secret')`,
				cleanup: `DELETE FROM tsigkeys`,
			},
			{
				name:    "orphan record",
				insert:  `INSERT INTO records(domain_id,name,type,content,ttl) VALUES(NULL,'orphan.test','A','192.0.2.99',300)`,
				cleanup: `DELETE FROM records WHERE domain_id IS NULL`,
			},
			{
				name:    "comment",
				insert:  `INSERT INTO comments(domain_id,name,type,modified_at,comment) SELECT id,name,'SOA',1,'hidden' FROM domains LIMIT 1`,
				cleanup: `DELETE FROM comments`,
			},
			{
				name:    "metadata",
				insert:  `INSERT INTO domainmetadata(domain_id,kind,content) SELECT id,'ALLOW-AXFR-FROM','0.0.0.0/0' FROM domains LIMIT 1`,
				cleanup: `DELETE FROM domainmetadata`,
			},
		} {
			db, err = openPDNSEngineDB(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.insert); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if err := verifyLegacyPDNSSourceRole(context.Background(), secondary); err == nil {
				t.Fatalf("hidden %s passed the empty producer proof", test.name)
			}
			db, err = openPDNSEngineDB(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.cleanup); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if err := verifyPDNSReplacementDatabaseEnvelope(context.Background(), path); err != nil {
			t.Fatalf("exact producer database envelope rejected: %v", err)
		}
		db, err = openPDNSEngineDB(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE hidden_authority(value TEXT)`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := verifyPDNSReplacementDatabaseEnvelope(context.Background(), path); err == nil {
			t.Fatal("unknown application table passed the replacement envelope")
		}
		db, err = openPDNSEngineDB(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`DROP TABLE hidden_authority`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+"-wal", []byte("unresolved"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := verifyPDNSReplacementDatabaseEnvelope(context.Background(), path); err == nil {
			t.Fatal("SQLite sidecar passed the replacement envelope")
		}
		if err := os.Remove(path + "-wal"); err != nil {
			t.Fatal(err)
		}
		db, err = openPDNSEngineDB(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO domains(name,type) VALUES('local-authority.test','MASTER')`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), secondary,
		); err == nil {
			t.Fatal("legacy source with local authority was demoted to secondary")
		}
	})

	t.Run("released tuple-less consumer remains secondary authority", func(t *testing.T) {
		prepareManagedPDNSCatalogConfig(t)
		peerDomain, err := binddns.CatalogDomain("192.0.2.20")
		if err != nil {
			t.Fatal(err)
		}
		oldSOA := probeDNSZoneSOA
		oldBound := probeDNSBoundCatalogAXFR
		oldAXFR := probeDNSCatalogAXFR
		peerMembers := []string{}
		localMemberSerial, peerMemberSerial := uint32(11), uint32(11)
		probeDNSCatalogAXFR = func(
			_ context.Context, address, domain string,
		) (dnsCatalogAXFRResult, error) {
			if address != "192.0.2.20" || domain != peerDomain {
				t.Fatalf("candidate peer proof tuple=%s/%s", address, domain)
			}
			return dnsCatalogAXFRResult{Serial: 7, Members: peerMembers}, nil
		}
		probeDNSBoundCatalogAXFR = func(
			_ context.Context, source, address, domain string,
		) (dnsCatalogAXFRResult, error) {
			if source != "192.0.2.10" || address != "192.0.2.20" || domain != peerDomain {
				t.Fatalf("consumer peer proof tuple=%s/%s/%s", source, address, domain)
			}
			return dnsCatalogAXFRResult{Serial: 7, Members: peerMembers}, nil
		}
		probeDNSZoneSOA = func(
			_ context.Context, network, address, domain string,
		) (dnsSOAProbeResult, error) {
			if network != "udp" && network != "tcp" {
				t.Fatalf("consumer SOA network=%s", network)
			}
			serial := uint32(7)
			if domain == "member.test" && address == "192.0.2.10" {
				serial = localMemberSerial
			} else if domain == "member.test" && address == "192.0.2.20" {
				serial = peerMemberSerial
			} else if address != "192.0.2.20" || domain != peerDomain {
				t.Fatalf("consumer peer SOA tuple=%s/%s/%s", network, address, domain)
			}
			return dnsSOAProbeResult{
				Authoritative: true, RCode: dnsRCodeNoError, SOASerials: []uint32{serial},
			}, nil
		}
		t.Cleanup(func() {
			probeDNSZoneSOA = oldSOA
			probeDNSBoundCatalogAXFR = oldBound
			probeDNSCatalogAXFR = oldAXFR
		})
		candidate := testPairedPDNSSwitchManifest(
			t, transport.DNSPairRoleSecondary, nil,
		)
		path := filepath.Join(t.TempDir(), "legacy-consumer.sqlite3")
		if err := buildPDNSSwitchCandidate(
			context.Background(), path, candidate, testPDNSEngineBinding(),
		); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CELIKPANEL_PDNS_DB", path)
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), testPairedPDNSSourceManifest(t, transport.DNSPairRoleSecondary),
		); err != nil {
			t.Fatalf("exact released consumer secondary rejected: %v", err)
		}
		db, err := openPDNSEngineDB(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			INSERT INTO domains(name,type,master,account,catalog)
			VALUES('member.test','SLAVE','192.0.2.20',?,?)
		`, pdnsPeerCatalogAccount, peerDomain); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			INSERT INTO records(domain_id,name,type,content,ttl,disabled,auth)
			SELECT id,name,'SOA','ns1.member.test hostmaster.member.test 11 3600 600 86400 300',300,0,1
			FROM domains WHERE name='member.test'
		`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		peerMembers = []string{"member.test"}
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), testPairedPDNSSourceManifest(t, transport.DNSPairRoleSecondary),
		); err != nil {
			t.Fatalf("populated released consumer secondary rejected: %v", err)
		}
		localMemberSerial = 10
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), testPairedPDNSSourceManifest(t, transport.DNSPairRoleSecondary),
		); err == nil {
			t.Fatal("stale local consumer member was accepted")
		}
		localMemberSerial = 11
		peerMembers = []string{"other.test"}
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), testPairedPDNSSourceManifest(t, transport.DNSPairRoleSecondary),
		); err == nil {
			t.Fatal("consumer member mismatch was accepted")
		}
		peerMembers = []string{"member.test"}
		for _, test := range []struct {
			name, insert, cleanup string
		}{
			{
				name:    "foreign domain",
				insert:  `INSERT INTO domains(name,type) VALUES('foreign.test','NATIVE')`,
				cleanup: `DELETE FROM domains WHERE name='foreign.test'`,
			},
			{
				name:    "orphan record",
				insert:  `INSERT INTO records(domain_id,name,type,content,ttl) VALUES(NULL,'orphan.test','A','192.0.2.99',300)`,
				cleanup: `DELETE FROM records WHERE domain_id IS NULL`,
			},
			{
				name:    "TSIG key",
				insert:  `INSERT INTO tsigkeys(name,algorithm,secret) VALUES('hidden','hmac-sha256','secret')`,
				cleanup: `DELETE FROM tsigkeys`,
			},
		} {
			db, err = openPDNSEngineDB(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.insert); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if err := verifyLegacyPDNSSourceRole(
				context.Background(), testPairedPDNSSourceManifest(t, transport.DNSPairRoleSecondary),
			); err == nil {
				t.Fatalf("consumer hidden %s was accepted", test.name)
			}
			db, err = openPDNSEngineDB(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.cleanup); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if err := verifyLegacyPDNSSourceRole(
			context.Background(), testPairedPDNSSourceManifest(t, transport.DNSPairRolePrimary),
		); err == nil {
			t.Fatal("legacy consumer was accepted as a primary source")
		}
	})
}

func TestBuildPDNSPairedPrimaryPublishesEngineNeutralCatalog(t *testing.T) {
	domain := "primary.test"
	zone, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 4, 1, domain, false, "MASTER",
		testPDNSEngineRecords(domain),
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testPairedPDNSSwitchManifest(t, transport.DNSPairRolePrimary,
		[]transport.DNSEngineSwitchZoneSnapshot{{
			Domain: domain, DesiredGeneration: 1, ZoneType: "MASTER",
			Records: zone.Records, ZoneQualifier: zone.Qualifier,
		}})
	path := filepath.Join(t.TempDir(), "paired-primary.sqlite3")
	binding := testPDNSEngineBinding()
	if err := buildPDNSSwitchCandidate(
		context.Background(), path, manifest, binding,
	); err == nil {
		t.Fatal("paired primary candidate accepted an implicit catalog serial")
	}
	const catalogSerial = uint32(41)
	if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
		context.Background(), path, manifest, binding, catalogSerial,
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
		context.Background(), path, manifest, binding, catalogSerial,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: manifest.Mode,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: manifest.TargetEpoch,
		PairRole: manifest.PairRole, PairLocalIP: manifest.LocalIP,
		PairPeerIP: manifest.PeerIP, PrimaryCatalogSerial: catalogSerial,
		SourceRevision:    manifest.SourceRevision,
		ManifestQualifier: manifest.Qualifier,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
	}
	if err := verifyPDNSStateManifestReceipt(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	state.ManifestQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("0", 64)
	if err := verifyPDNSStateManifestReceipt(context.Background(), state); err == nil {
		t.Fatal("PowerDNS database receipt accepted a different active state")
	}
	if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
		context.Background(), path, manifest, binding, catalogSerial+1,
	); err == nil {
		t.Fatal("PowerDNS candidate accepted a different catalog handoff serial")
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var catalogs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE account=?`, pdnsBINDCatalogAccount).Scan(&catalogs); err != nil {
		t.Fatal(err)
	}
	if catalogs != 1 {
		t.Fatalf("managed primary catalogs=%d", catalogs)
	}
}

func TestPDNSPrimaryCatalogMaximumSwitchThenMembershipFailsClosed(t *testing.T) {
	prepareManagedPDNSCatalogConfig(t)
	domain := "existing.test"
	zone, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 4, 1, domain, false, "MASTER",
		testPDNSEngineRecords(domain),
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testPairedPDNSSwitchManifest(
		t, transport.DNSPairRolePrimary,
		[]transport.DNSEngineSwitchZoneSnapshot{{
			Domain: domain, DesiredGeneration: 1, ZoneType: "MASTER",
			Records: zone.Records, ZoneQualifier: zone.Qualifier,
		}},
	)
	path := filepath.Join(t.TempDir(), "paired-primary-max.sqlite3")
	binding := testPDNSEngineBinding()
	if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
		context.Background(), path, manifest, binding, ^uint32(0),
	); err != nil {
		t.Fatal(err)
	}
	added, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, manifest.TargetEpoch,
		1, "new.test", false, "MASTER", testPDNSEngineRecords("new.test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPDNSV3ZoneDatabase(
		context.Background(), path, added, binding,
	); err == nil {
		t.Fatal("PowerDNS membership change wrapped an exhausted catalog serial")
	}
	if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
		context.Background(), path, manifest, binding, ^uint32(0),
	); err != nil {
		t.Fatalf("failed membership transaction changed prior catalog: %v", err)
	}
}

func TestBuildPDNSPairedSecondaryStagesExactPeerCatalog(t *testing.T) {
	previous := probeDNSCatalogAXFR
	probeDNSCatalogAXFR = func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
		if address != "192.0.2.20" {
			t.Fatalf("catalog address=%q", address)
		}
		return dnsCatalogAXFRResult{Serial: 7, Members: []string{"one.test", "two.test"}}, nil
	}
	t.Cleanup(func() { probeDNSCatalogAXFR = previous })
	manifest := testPairedPDNSSwitchManifest(t, transport.DNSPairRoleSecondary, nil)
	path := filepath.Join(t.TempDir(), "paired-secondary.sqlite3")
	if err := buildPDNSSwitchCandidate(
		context.Background(), path, manifest, testPDNSEngineBinding(),
	); err != nil {
		t.Fatal(err)
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var total, exact int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM domains
		WHERE UPPER(type)='CONSUMER' AND master='192.0.2.20'
		  AND account=? AND catalog IS NULL
	`, pdnsPeerCatalogAccount).Scan(&exact); err != nil {
		t.Fatal(err)
	}
	if total != 1 || exact != 1 {
		t.Fatalf("secondary total=%d exact=%d", total, exact)
	}
}

func TestBuildPDNSPairedSecondaryRejectsNoncanonicalPeerCatalog(t *testing.T) {
	manifest := testPairedPDNSSwitchManifest(
		t, transport.DNSPairRoleSecondary, nil,
	)
	for _, test := range []struct {
		name    string
		catalog dnsCatalogAXFRResult
	}{
		{
			name:    "zero serial",
			catalog: dnsCatalogAXFRResult{Members: []string{"one.test"}},
		},
		{
			name: "unsorted members",
			catalog: dnsCatalogAXFRResult{
				Serial: 7, Members: []string{"two.test", "one.test"},
			},
		},
		{
			name: "duplicate members",
			catalog: dnsCatalogAXFRResult{
				Serial: 7, Members: []string{"one.test", "one.test"},
			},
		},
		{
			name: "catalog self member",
			catalog: dnsCatalogAXFRResult{
				Serial: 7,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalogDomain, err := binddns.CatalogDomain(manifest.PeerIP)
			if err != nil {
				t.Fatal(err)
			}
			catalog := test.catalog
			if test.name == "catalog self member" {
				catalog.Members = []string{catalogDomain}
			}
			previous := probeDNSCatalogAXFR
			probeDNSCatalogAXFR = func(
				context.Context, string, string,
			) (dnsCatalogAXFRResult, error) {
				return catalog, nil
			}
			t.Cleanup(func() { probeDNSCatalogAXFR = previous })
			path := filepath.Join(t.TempDir(), "unsafe-secondary.sqlite3")
			if err := buildPDNSSwitchCandidate(
				context.Background(), path, manifest, testPDNSEngineBinding(),
			); err == nil {
				t.Fatal("noncanonical peer catalog reached a switch candidate")
			}
		})
	}
}

func TestBostonPowerDNSSecondaryUsesExplicitCatalogWithoutAutoSecondary(t *testing.T) {
	previous := probeDNSCatalogAXFR
	peerCatalogDomain, err := binddns.CatalogDomain("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	probeDNSCatalogAXFR = func(
		_ context.Context, address, domain string,
	) (dnsCatalogAXFRResult, error) {
		if address != "192.0.2.10" || domain != peerCatalogDomain {
			t.Fatalf("catalog proof tuple=%q/%q", address, domain)
		}
		return dnsCatalogAXFRResult{
			Serial: 17, Members: []string{"example.test"},
		}, nil
	}
	t.Cleanup(func() { probeDNSCatalogAXFR = previous })

	manifest := testPDNSPairSecondaryReconfigureManifest(t)
	config, err := dnsClusterConfigForSwitchManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := "# Managed by CelikPanel - do not edit by hand / elle duzenlemeyin\n" +
		"# Directional DNS pair: AXFR is restricted to the trusted local proof and exact peer.\n" +
		"# Yonlu DNS cifti: AXFR guvenilir yerel kanit ve tam es ile sinirlidir.\n" +
		"primary=yes\n" +
		"secondary=yes\n" +
		"allow-axfr-ips=192.0.2.10\n"
	if config != wantConfig {
		t.Fatalf("Boston secondary config differs:\n%s", config)
	}
	if strings.Contains(strings.ToLower(config), "autosecondary") ||
		strings.Contains(strings.ToLower(config), "autoprimary") {
		t.Fatalf("Boston secondary config enables automatic discovery:\n%s", config)
	}

	path := filepath.Join(t.TempDir(), "boston-secondary.sqlite3")
	binding := testPDNSEngineBinding()
	if err := buildPDNSSwitchCandidate(
		context.Background(), path, manifest, binding,
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSSwitchDatabase(
		context.Background(), path, manifest, binding,
	); err != nil {
		t.Fatal(err)
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var exact int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM domains
		WHERE name=? COLLATE BINARY AND UPPER(type)='CONSUMER'
		  AND master='192.0.2.10' AND account=?
		  AND last_check IS NULL AND notified_serial IS NULL
		  AND options IS NULL AND catalog IS NULL
	`, peerCatalogDomain, pdnsPeerCatalogAccount).Scan(&exact); err != nil {
		t.Fatal(err)
	}
	if exact != 1 {
		t.Fatal("Boston secondary lacks its exact pre-start catalog consumer")
	}
}

func TestPDNSPairedSecondaryVerifierAcceptsOnlyCompleteCatalogProjection(t *testing.T) {
	previous := probeDNSCatalogAXFR
	probeDNSCatalogAXFR = func(
		_ context.Context, _ string, _ string,
	) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{
			Serial: 17, Members: []string{"one.test", "two.test"},
		}, nil
	}
	t.Cleanup(func() { probeDNSCatalogAXFR = previous })
	manifest := testPairedPDNSSwitchManifest(
		t, transport.DNSPairRoleSecondary, nil,
	)
	catalogDomain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		members []string
		wantErr bool
	}{
		{name: "pre-start consumer only"},
		{
			name:    "complete post-retrieve projection",
			members: []string{"one.test", "two.test"},
		},
		{
			name:    "partial projection",
			members: []string{"one.test"},
			wantErr: true,
		},
		{
			name:    "foreign projection",
			members: []string{"foreign.test", "one.test", "two.test"},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secondary.sqlite3")
			binding := testPDNSEngineBinding()
			if err := buildPDNSSwitchCandidate(
				context.Background(), path, manifest, binding,
			); err != nil {
				t.Fatal(err)
			}
			db, err := openPDNSEngineDB(path, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, member := range test.members {
				if _, err := db.Exec(`
					INSERT INTO domains(name,type,master,catalog)
					VALUES(?, 'SECONDARY', ?, ?)
				`, member, manifest.PeerIP, catalogDomain); err != nil {
					db.Close()
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			err = verifyPDNSSwitchDatabase(
				context.Background(), path, manifest, binding,
			)
			if test.wantErr && err == nil {
				t.Fatal("non-exact catalog projection was accepted")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("exact catalog projection rejected: %v", err)
			}
		})
	}
}

func TestRetrievePDNSPairedSecondaryWaitsForExactSQLiteProjection(t *testing.T) {
	previousAXFR := probeDNSCatalogAXFR
	previousRetrieve := dnsClusterRetrieve
	previousPurge := dnsClusterPurge
	manifest := testPDNSPairSecondaryReconfigureManifest(t)
	catalogDomain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		t.Fatal(err)
	}
	catalog := dnsCatalogAXFRResult{
		Serial: 17, Members: []string{"one.test"},
	}
	probeDNSCatalogAXFR = func(
		_ context.Context, address, domain string,
	) (dnsCatalogAXFRResult, error) {
		if address != manifest.PeerIP || domain != catalogDomain {
			t.Fatalf("catalog proof tuple=%q/%q", address, domain)
		}
		return catalog, nil
	}
	path := filepath.Join(t.TempDir(), "live-secondary.sqlite3")
	binding := testPDNSEngineBinding()
	if err := buildPDNSSwitchCandidate(
		context.Background(), path, manifest, binding,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	var retrieved, purged []string
	dnsClusterRetrieve = func(_ context.Context, zone string) ([]byte, error) {
		retrieved = append(retrieved, zone)
		if zone == "one.test" {
			db, err := openPDNSEngineDB(path, false)
			if err != nil {
				return nil, err
			}
			_, err = db.Exec(`
				INSERT OR IGNORE INTO domains(name,type,master,catalog)
				VALUES('one.test', 'SECONDARY', ?, ?)
			`, manifest.PeerIP, catalogDomain)
			closeErr := db.Close()
			if err != nil {
				return nil, err
			}
			return nil, closeErr
		}
		return nil, nil
	}
	dnsClusterPurge = func(_ context.Context, zone string) ([]byte, error) {
		purged = append(purged, zone)
		return nil, nil
	}
	t.Cleanup(func() {
		probeDNSCatalogAXFR = previousAXFR
		dnsClusterRetrieve = previousRetrieve
		dnsClusterPurge = previousPurge
	})
	if err := retrievePDNSPairSecondaryZones(
		context.Background(), manifest,
	); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(retrieved, []string{catalogDomain, "one.test"}) ||
		!slices.Equal(purged, []string{catalogDomain, "one.test"}) {
		t.Fatalf("retrieved=%v purged=%v", retrieved, purged)
	}
}

func TestVerifyPDNSSwitchDatabaseRejectsTamperedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.sqlite3")
	manifest := testPDNSSwitchManifest(t)
	binding := testPDNSEngineBinding()
	if err := buildPDNSSwitchCandidate(context.Background(), path, manifest, binding); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		UPDATE records SET content = '192.0.2.99'
		WHERE id = (SELECT id FROM records WHERE type = 'A' ORDER BY id LIMIT 1)
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSSwitchDatabase(context.Background(), path, manifest, binding); err == nil {
		t.Fatal("tampered PowerDNS record rows passed exact verification")
	}
}

func TestApplyPDNSV3ZoneRejectsGenerationBindingReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 4, 9, "same.test", false, "NATIVE",
		testPDNSEngineRecords("same.test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPDNSV3ZoneTx(context.Background(), tx, commitment, testPDNSEngineBinding(), true); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	other := testPDNSEngineBinding()
	other.MutationRequestID = strings.Repeat("c", 32)
	if err := applyPDNSV3ZoneTx(context.Background(), tx, commitment, other, true); err == nil {
		tx.Rollback()
		t.Fatal("equal generation with a different request binding was accepted")
	}
	_ = tx.Rollback()
}

func TestApplyPDNSV3ZoneDatabasePreservesDNSSECStateOnSync(t *testing.T) {
	previousClusterConf := dnsClusterConf
	dnsClusterConf = filepath.Join(t.TempDir(), "celikpanel-cluster.conf")
	t.Cleanup(func() { dnsClusterConf = previousClusterConf })
	path := filepath.Join(t.TempDir(), "live.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO domains (name, type) VALUES ('secure.test', 'NATIVE')`)
	if err != nil {
		t.Fatal(err)
	}
	domainID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO domainmetadata (domain_id, kind, content) VALUES (?, 'PRESIGNED', '1')`,
		`INSERT INTO cryptokeys (domain_id, flags, active, published, content) VALUES (?, 257, 1, 1, 'private-key')`,
		`INSERT INTO comments (domain_id, name, type, modified_at, account, comment) VALUES (?, 'secure.test', 'SOA', 1, 'test', 'keep')`,
	} {
		if _, err := db.Exec(statement, domainID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 3, 4, "secure.test", false, "NATIVE",
		testPDNSEngineRecords("secure.test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPDNSV3ZoneDatabase(
		context.Background(), path, commitment, testPDNSEngineBinding(),
	); err != nil {
		t.Fatal(err)
	}
	db, err = openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"domainmetadata", "cryptokeys", "comments"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE domain_id = ?", domainID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s rows = %d, want preserved", table, count)
		}
	}
	exact, err := verifyPDNSV3ZoneDatabase(
		context.Background(), path, commitment, testPDNSEngineBinding(),
	)
	if err != nil || !exact {
		t.Fatalf("exact=%t err=%v", exact, err)
	}
}

func TestValidatePDNSEngineReceiptSchemaRejectsLooseAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loose.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE celikpanel_dns_zone_sync_v3_receipts`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE celikpanel_dns_zone_sync_v3_receipts (
			domain TEXT PRIMARY KEY, engine TEXT, engine_epoch INTEGER,
			request_id TEXT, owner_id TEXT, qualifier TEXT,
			desired_generation INTEGER, action TEXT, zone_type TEXT, schema TEXT
		)
	`); err != nil {
		t.Fatal(err)
	}
	if err := validatePDNSEngineReceiptSchema(context.Background(), db); err == nil {
		t.Fatal("loose rowid receipt authority was accepted")
	}
	_ = db.Close()
}
