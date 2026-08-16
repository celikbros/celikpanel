package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

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
