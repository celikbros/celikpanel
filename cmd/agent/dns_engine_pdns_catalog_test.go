package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

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
	if second.Serial != 1 || len(second.Members) != 2 {
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
