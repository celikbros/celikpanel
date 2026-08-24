package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func prepareManagedPDNSCatalogConfig(t *testing.T) {
	t.Helper()
	oldConf := dnsClusterConf
	oldRequiredOwnerUID := dnsClusterConfigRequiredOwnerUID
	oldLocalProof := dnsPairLocalProofAddress
	oldHostAddresses := dnsPairHostOwnedAddresses
	dnsClusterConf = filepath.Join(t.TempDir(), "celikpanel-cluster.conf")
	if runtime.GOOS == "linux" {
		dnsClusterConfigRequiredOwnerUID = uint32(os.Geteuid())
	}
	dnsPairLocalProofAddress = func() (string, error) {
		return "192.0.2.10", nil
	}
	dnsPairHostOwnedAddresses = func() ([]string, error) {
		return []string{"192.0.2.10"}, nil
	}
	config := dnsClusterConfig(&DNSClusterRequest{
		Role: dnsRolePaired, PeerIP: "192.0.2.20", PeerNS: "ns2.example.test",
	})
	if err := os.WriteFile(dnsClusterConf, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dnsClusterConf = oldConf
		dnsClusterConfigRequiredOwnerUID = oldRequiredOwnerUID
		dnsPairLocalProofAddress = oldLocalProof
		dnsPairHostOwnedAddresses = oldHostAddresses
	})
}

func seedManagedPDNSCatalog(t *testing.T, path, localIP string) {
	t.Helper()
	db, err := openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconcilePDNSBINDCatalogTx(
		context.Background(), tx, true, localIP,
	); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func readPDNSTestCatalogTx(
	t *testing.T,
	tx *sql.Tx,
	localIP string,
) managedPDNSCatalog {
	t.Helper()
	domain, err := binddns.CatalogDomain(localIP)
	if err != nil {
		t.Fatal(err)
	}
	var domainID int64
	if err := tx.QueryRowContext(context.Background(), `
		SELECT id FROM domains
		WHERE name = ? COLLATE NOCASE
		  AND UPPER(type) = 'PRODUCER' AND account = ?
	`, domain, pdnsBINDCatalogAccount).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	_, serial, err := readPDNSBINDCatalogRecordsTx(
		context.Background(), tx, domainID, domain,
	)
	if err != nil {
		t.Fatal(err)
	}
	members, err := readPDNSBINDCatalogMembersTx(
		context.Background(), tx, domain,
	)
	if err != nil {
		t.Fatal(err)
	}
	return managedPDNSCatalog{
		Domain: domain, LocalIP: localIP, Serial: serial, Members: members,
	}
}

func readPDNSTestCatalog(
	t *testing.T,
	path string,
	localIP string,
) managedPDNSCatalog {
	t.Helper()
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	catalog := readPDNSTestCatalogTx(t, tx, localIP)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func pdnsCatalogTestCommitment(
	t *testing.T,
	domain string,
	generation int64,
	deleteZone bool,
	records []transport.ZoneRecord,
) mutationpayload.DNSZoneSyncV3Commitment {
	t.Helper()
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 3, generation, domain, deleteZone, "MASTER",
		records,
	)
	if err != nil {
		t.Fatal(err)
	}
	return commitment
}

func applyPDNSTestZone(t *testing.T, dbPath, domain string, generation int64) {
	t.Helper()
	db, err := openPDNSEngineDB(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 3, generation, domain, false, "MASTER",
		testPDNSEngineRecords(domain),
	)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPDNSV3ZoneTx(
		context.Background(), tx, commitment, testPDNSEngineBinding(), true,
	); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilePDNSBINDCatalogTracksPrimaryZonesIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	applyPDNSTestZone(t, path, "one.test", 1)

	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	reconcile := func(enabled bool) managedPDNSCatalog {
		t.Helper()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := reconcilePDNSBINDCatalogTx(
			context.Background(), tx, enabled, "192.0.2.10",
		)
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return catalog
	}
	first := reconcile(true)
	wantDomain, err := binddns.CatalogDomain("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if first.Domain != wantDomain || first.Serial != 1 ||
		len(first.Members) != 1 || first.Members[0] != "one.test" {
		t.Fatalf("unexpected first catalog: %#v", first)
	}
	if again := reconcile(true); again.Serial != first.Serial {
		t.Fatalf("idempotent reconcile advanced serial: %d -> %d", first.Serial, again.Serial)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	applyPDNSTestZone(t, path, "two.test", 2)
	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	second := reconcile(true)
	if second.Serial != 2 || len(second.Members) != 2 {
		t.Fatalf("producer membership did not converge: %#v", second)
	}
	var producers, members int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM domains
		WHERE name=? AND UPPER(type)='PRODUCER' AND account=?
	`, wantDomain, pdnsBINDCatalogAccount).Scan(&producers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE catalog=?`, wantDomain).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if producers != 1 || members != 2 {
		t.Fatalf("producer=%d members=%d", producers, members)
	}
	reconcile(false)
	var catalogs, userZones int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE account = ?`, pdnsBINDCatalogAccount).Scan(&catalogs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE account IS NULL OR account <> ?`, pdnsBINDCatalogAccount).Scan(&userZones); err != nil {
		t.Fatal(err)
	}
	if catalogs != 0 || userZones != 2 {
		t.Fatalf("disable catalogs=%d user zones=%d", catalogs, userZones)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE catalog IS NOT NULL`).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 0 {
		t.Fatalf("disabled producer left %d catalog members", members)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilePDNSBINDCatalogRejectsForeignAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := binddns.CatalogDomain("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domains(name,type,account) VALUES(?, 'MASTER', 'foreign')`, domain); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconcilePDNSBINDCatalogTx(context.Background(), tx, true, "192.0.2.10"); err == nil {
		tx.Rollback()
		t.Fatal("foreign catalog authority was accepted")
	}
	_ = tx.Rollback()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPDNSV3ZoneDatabaseAdvancesCatalogSerialOnlyForMembership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	prepareManagedPDNSCatalogConfig(t)
	seedManagedPDNSCatalog(t, path, "192.0.2.10")
	binding := testPDNSEngineBinding()

	first := pdnsCatalogTestCommitment(
		t, "one.test", 1, false, testPDNSEngineRecords("one.test"),
	)
	if err := applyPDNSV3ZoneDatabase(
		context.Background(), path, first, binding,
	); err != nil {
		t.Fatal(err)
	}
	catalog := readPDNSTestCatalog(t, path, "192.0.2.10")
	if catalog.Serial != 2 ||
		!reflect.DeepEqual(catalog.Members, []string{"one.test"}) {
		t.Fatalf("first membership catalog = %#v", catalog)
	}

	updatedRecords := append(
		testPDNSEngineRecords("one.test"),
		transport.ZoneRecord{
			Name: "one.test", Type: "TXT", Content: `"record-only"`,
			TTL: 300,
		},
	)
	recordOnly := pdnsCatalogTestCommitment(
		t, "one.test", 2, false, updatedRecords,
	)
	if err := applyPDNSV3ZoneDatabase(
		context.Background(), path, recordOnly, binding,
	); err != nil {
		t.Fatal(err)
	}
	catalog = readPDNSTestCatalog(t, path, "192.0.2.10")
	if catalog.Serial != 2 ||
		!reflect.DeepEqual(catalog.Members, []string{"one.test"}) {
		t.Fatalf("record-only update changed membership serial: %#v", catalog)
	}

	deleted := pdnsCatalogTestCommitment(t, "one.test", 3, true, nil)
	if err := applyPDNSV3ZoneDatabase(
		context.Background(), path, deleted, binding,
	); err != nil {
		t.Fatal(err)
	}
	catalog = readPDNSTestCatalog(t, path, "192.0.2.10")
	if catalog.Serial != 3 || len(catalog.Members) != 0 {
		t.Fatalf("delete did not advance catalog once: %#v", catalog)
	}

	readded := pdnsCatalogTestCommitment(
		t, "one.test", 4, false, testPDNSEngineRecords("one.test"),
	)
	if err := applyPDNSV3ZoneDatabase(
		context.Background(), path, readded, binding,
	); err != nil {
		t.Fatal(err)
	}
	catalog = readPDNSTestCatalog(t, path, "192.0.2.10")
	if catalog.Serial != 4 ||
		!reflect.DeepEqual(catalog.Members, []string{"one.test"}) {
		t.Fatalf("re-add did not advance catalog once: %#v", catalog)
	}
	if err := applyPDNSV3ZoneDatabase(
		context.Background(), path, readded, binding,
	); err != nil {
		t.Fatal(err)
	}
	replayed := readPDNSTestCatalog(t, path, "192.0.2.10")
	if replayed.Serial != catalog.Serial ||
		!reflect.DeepEqual(replayed.Members, catalog.Members) {
		t.Fatalf("idempotent replay changed catalog: %#v", replayed)
	}
}

func TestVerifyManagedPDNSCatalogRequiresExactLiveSerial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	prepareManagedPDNSCatalogConfig(t)
	seedManagedPDNSCatalog(t, path, "192.0.2.10")
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	commitment := pdnsCatalogTestCommitment(
		t, "one.test", 1, false, testPDNSEngineRecords("one.test"),
	)
	if err := applyPDNSV3ZoneDatabase(
		context.Background(), path, commitment, testPDNSEngineBinding(),
	); err != nil {
		t.Fatal(err)
	}

	oldPurge := dnsClusterPurge
	oldProbe := probeDNSCatalogAXFR
	dnsClusterPurge = func(context.Context, string) ([]byte, error) {
		return nil, nil
	}
	liveSerial := uint32(2)
	probeDNSCatalogAXFR = func(
		_ context.Context,
		address string,
		domain string,
	) (dnsCatalogAXFRResult, error) {
		if address != "192.0.2.10" {
			t.Fatalf("live catalog address = %q", address)
		}
		return dnsCatalogAXFRResult{
			Serial: liveSerial, Members: []string{"one.test"},
		}, nil
	}
	t.Cleanup(func() {
		dnsClusterPurge = oldPurge
		probeDNSCatalogAXFR = oldProbe
	})
	if err := verifyManagedPDNSBINDCatalogLive(context.Background()); err != nil {
		t.Fatalf("exact live catalog rejected: %v", err)
	}
	liveSerial = 1
	if err := verifyManagedPDNSBINDCatalogLive(context.Background()); err == nil {
		t.Fatal("stale live catalog serial was accepted")
	}
}

func TestPDNSCatalogMembershipVerificationRejectsStaleExtraMember(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	applyPDNSTestZone(t, path, "one.test", 1)
	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	catalog, err := reconcilePDNSBINDCatalogTx(
		context.Background(), tx, true, "192.0.2.10",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO domains(name, type, catalog)
		VALUES('stale.test', 'MASTER', ?)
	`, catalog.Domain); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSProducerMembershipTx(
		context.Background(), tx, "192.0.2.10", []string{"one.test"},
	); err == nil {
		t.Fatal("stale extra catalog member was accepted")
	}
}

func TestPDNSCatalogSerialAndMembershipRollbackTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	applyPDNSTestZone(t, path, "one.test", 1)
	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	initial, err := reconcilePDNSBINDCatalogTx(
		context.Background(), tx, true, "192.0.2.10",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if initial.Serial != 1 {
		t.Fatalf("initial serial = %d", initial.Serial)
	}

	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	previous, err := reconcilePDNSBINDCatalogTx(
		context.Background(), tx, true, "192.0.2.10",
	)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	commitment := pdnsCatalogTestCommitment(
		t, "two.test", 2, false, testPDNSEngineRecords("two.test"),
	)
	if err := applyPDNSV3ZoneTx(
		context.Background(), tx, commitment, testPDNSEngineBinding(), true,
	); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	changed, err := reconcilePDNSBINDCatalogFromSnapshotTx(
		context.Background(), tx, true, "192.0.2.10", &previous,
	)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if changed.Serial != 2 ||
		!reflect.DeepEqual(changed.Members, []string{"one.test", "two.test"}) {
		tx.Rollback()
		t.Fatalf("transactional catalog = %#v", changed)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	after := readPDNSTestCatalog(t, path, "192.0.2.10")
	if after.Serial != 1 ||
		!reflect.DeepEqual(after.Members, []string{"one.test"}) {
		t.Fatalf("rollback leaked catalog mutation: %#v", after)
	}
}

func TestPDNSCatalogSerialExhaustionFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	applyPDNSTestZone(t, path, "one.test", 1)
	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := reconcilePDNSBINDCatalogTx(
		context.Background(), tx, true, "192.0.2.10",
	)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	records, err := canonicalPDNSCatalogBaseRecords(
		"192.0.2.10", ^uint32(0), catalog.Members,
	)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	var soa transport.ZoneRecord
	for _, record := range records {
		if record.Type == "SOA" {
			soa = record
		}
	}
	if _, err := tx.ExecContext(context.Background(), `
		UPDATE records SET content = ?
		WHERE domain_id = (
			SELECT id FROM domains WHERE name = ? COLLATE NOCASE
		) AND name = ? COLLATE BINARY AND type = 'SOA'
	`, soa.Content, catalog.Domain, catalog.Domain); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	prepareManagedPDNSCatalogConfig(t)
	second := pdnsCatalogTestCommitment(
		t, "two.test", 2, false, testPDNSEngineRecords("two.test"),
	)
	err = applyPDNSV3ZoneDatabase(
		context.Background(), path, second, testPDNSEngineBinding(),
	)
	if err == nil || !strings.Contains(err.Error(), "catalog serial is exhausted") {
		t.Fatalf("serial exhaustion error = %v", err)
	}
	after := readPDNSTestCatalog(t, path, "192.0.2.10")
	if after.Serial != ^uint32(0) ||
		!reflect.DeepEqual(after.Members, []string{"one.test"}) {
		t.Fatalf("serial exhaustion changed catalog: %#v", after)
	}
	db, err = openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var leaked int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM domains WHERE name = 'two.test'
	`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatal("serial exhaustion committed the new member")
	}
}
