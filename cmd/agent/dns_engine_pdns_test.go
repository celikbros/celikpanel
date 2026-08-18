package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		PairRole: manifest.PairRole, PrimaryCatalogSerial: catalogSerial,
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
