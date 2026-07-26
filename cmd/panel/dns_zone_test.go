package main

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func newDNSPanelForTest(t *testing.T) *Panel {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(database.Close)
	return &Panel{db: database}
}

func setDNSIdentityForTest(t *testing.T, p *Panel, role string) {
	t.Helper()
	ctx := context.Background()
	settings := map[string]string{
		settingNS1:     "ns1.celikhost.com",
		settingNS2:     "ns2.celikhost.com",
		settingDNSRole: role,
	}
	if role == "paired" {
		settings[settingDNSPeerIP] = "2.25.80.4"
		settings[settingDNSPeerNS] = "ns2.celikhost.com"
	}
	for key, value := range settings {
		if err := p.setSetting(ctx, key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
}

func TestEnsureZoneUsesSharedNameserversAndCurrentKind(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	ctx := context.Background()

	zoneID, err := p.ensureZone(ctx, "biovision.health")
	if err != nil {
		t.Fatalf("ensure zone: %v", err)
	}
	var kind string
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT type FROM pdns_domains WHERE id = ?`, zoneID).Scan(&kind); err != nil {
		t.Fatalf("read zone kind: %v", err)
	}
	if kind != "MASTER" {
		t.Fatalf("zone kind = %q, want MASTER", kind)
	}

	rows, err := p.db.GetDB().QueryContext(ctx,
		`SELECT type, content FROM pdns_records WHERE domain_id = ? ORDER BY type, content`, zoneID)
	if err != nil {
		t.Fatalf("read baseline records: %v", err)
	}
	defer rows.Close()
	var soa string
	nameservers := map[string]bool{}
	count := 0
	for rows.Next() {
		var typ, content string
		if err := rows.Scan(&typ, &content); err != nil {
			t.Fatalf("scan baseline record: %v", err)
		}
		count++
		switch typ {
		case "SOA":
			soa = content
		case "NS":
			nameservers[content] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate baseline records: %v", err)
	}
	if count != 3 {
		t.Fatalf("baseline record count = %d, want 3", count)
	}
	if !nameservers["ns1.celikhost.com"] || !nameservers["ns2.celikhost.com"] {
		t.Fatalf("shared nameservers not installed: %v", nameservers)
	}
	if !strings.HasPrefix(soa, "ns1.celikhost.com hostmaster.biovision.health ") {
		t.Fatalf("unexpected SOA: %q", soa)
	}

	again, err := p.ensureZone(ctx, "biovision.health")
	if err != nil {
		t.Fatalf("ensure existing zone: %v", err)
	}
	if again != zoneID {
		t.Fatalf("existing zone id = %d, want %d", again, zoneID)
	}
}

func TestZoneCreationRequiresExplicitlySavedDNSIdentity(t *testing.T) {
	p := newDNSPanelForTest(t)
	ctx := context.Background()

	if _, err := p.ensureZone(ctx, "not-configured.example"); err == nil {
		t.Fatal("ensureZone accepted inferred or empty nameserver settings")
	}
	if _, _, err := p.createZoneWithTemplate(ctx, "not-configured.example"); err == nil {
		t.Fatal("createZoneWithTemplate accepted inferred or empty nameserver settings")
	}
	var count int
	if err := p.db.GetDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM pdns_domains`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed DNS preflight left %d zones behind", count)
	}

	for key, value := range map[string]string{
		settingNS1: "ns1.celikhost.com",
		settingNS2: "ns2.celikhost.com",
	} {
		if err := p.setSetting(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if p.dnsIdentityConfigured(ctx) {
		t.Fatal("saved names without an operating mode reported ready")
	}
	if err := p.setSetting(ctx, settingDNSRole, "primary"); err != nil {
		t.Fatal(err)
	}
	if p.dnsIdentityConfigured(ctx) {
		t.Fatal("legacy role reported ready before paired-mode migration")
	}
	if err := p.setSetting(ctx, settingDNSRole, "paired"); err != nil {
		t.Fatal(err)
	}
	if p.dnsIdentityConfigured(ctx) {
		t.Fatal("paired role without peer address/name reported ready")
	}
	if err := p.setSetting(ctx, settingDNSPeerIP, "2.25.80.4"); err != nil {
		t.Fatal(err)
	}
	if err := p.setSetting(ctx, settingDNSPeerNS, "ns2.celikhost.com"); err != nil {
		t.Fatal(err)
	}
	if !p.dnsIdentityConfigured(ctx) {
		t.Fatal("explicit shared names and paired mode did not report ready")
	}
}

func TestEnsureZoneRollsBackIncompleteBaseline(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	ctx := context.Background()
	if _, err := p.db.GetDB().ExecContext(ctx, `
		CREATE TRIGGER reject_ns_baseline
		BEFORE INSERT ON pdns_records WHEN NEW.type = 'NS'
		BEGIN SELECT RAISE(ABORT, 'reject NS baseline'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, err := p.ensureZone(ctx, "rollback.example"); err == nil {
		t.Fatal("ensureZone unexpectedly succeeded")
	}
	var count int
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pdns_domains WHERE name = 'rollback.example'`).Scan(&count); err != nil {
		t.Fatalf("count rolled-back zones: %v", err)
	}
	if count != 0 {
		t.Fatalf("partial zone survived transaction rollback: count=%d", count)
	}
}

func TestZoneTemplateSplitsInBailiwickNameserverAddresses(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	t.Setenv("CELIKPANEL_SERVER_IP", "72.62.38.15")
	ctx := context.Background()

	zoneID, created, err := p.createZoneWithTemplate(ctx, "celikhost.com")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("owner zone was not created")
	}
	rows, err := p.db.GetDB().QueryContext(ctx,
		`SELECT name, content FROM pdns_records WHERE domain_id = ? AND type = 'A' AND name IN (?, ?)`,
		zoneID, "ns1.celikhost.com", "ns2.celikhost.com")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, address string
		if err := rows.Scan(&name, &address); err != nil {
			t.Fatal(err)
		}
		got[name] = address
	}
	if got["ns1.celikhost.com"] != "72.62.38.15" || got["ns2.celikhost.com"] != "2.25.80.4" {
		t.Fatalf("nameserver A mapping = %v, want ns1 local and ns2 peer", got)
	}
}

func TestBostonPairedZoneTemplateUsesLocalNS2InSOA(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	t.Setenv("CELIKPANEL_SERVER_IP", "2.25.80.4")
	ctx := context.Background()
	if err := p.setSetting(ctx, settingDNSPeerIP, "72.62.38.15"); err != nil {
		t.Fatal(err)
	}
	if err := p.setSetting(ctx, settingDNSPeerNS, "ns1.celikhost.com"); err != nil {
		t.Fatal(err)
	}

	zoneID, created, err := p.createZoneWithTemplate(ctx, "biovision.health")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("zone was not created")
	}
	var soa string
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT content FROM pdns_records WHERE domain_id = ? AND type = 'SOA'`, zoneID).Scan(&soa); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(soa, "ns2.celikhost.com ") {
		t.Fatalf("SOA MNAME = %q, want Boston-local ns2.celikhost.com", soa)
	}
}

func TestZoneTemplateRollsBackAllRowsOnRecordFailure(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	ctx := context.Background()
	if _, err := p.db.GetDB().ExecContext(ctx, `
		CREATE TRIGGER reject_template_txt
		BEFORE INSERT ON pdns_records WHEN NEW.type = 'TXT'
		BEGIN SELECT RAISE(ABORT, 'reject template TXT'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.createZoneWithTemplate(ctx, "rollback-template.example"); err == nil {
		t.Fatal("zone template unexpectedly succeeded")
	}
	var count int
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pdns_domains WHERE name = 'rollback-template.example'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial template zone survived rollback: %d", count)
	}
}

func TestNextSOASerialIsStrictlyMonotonic(t *testing.T) {
	now := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	soa := "ns1.celikhost.com hostmaster.biovision.health 2026072600 10800 3600 604800 3600"
	first, err := nextSOASerial(soa, now)
	if err != nil {
		t.Fatalf("first serial: %v", err)
	}
	second, err := nextSOASerial(first, now)
	if err != nil {
		t.Fatalf("second serial: %v", err)
	}
	serial := func(content string) uint64 {
		t.Helper()
		fields := strings.Fields(content)
		if len(fields) < 3 {
			t.Fatalf("invalid SOA in test: %q", content)
		}
		value, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			t.Fatalf("parse SOA serial: %v", err)
		}
		return value
	}
	if serial(first) != 2026072601 || serial(second) != 2026072602 {
		t.Fatalf("serials = %d, %d; want 2026072601, 2026072602", serial(first), serial(second))
	}

	future := "ns1.celikhost.com hostmaster.biovision.health 2026072699 10800 3600 604800 3600"
	next, err := nextSOASerial(future, now)
	if err != nil {
		t.Fatalf("future serial: %v", err)
	}
	if serial(next) != 2026072700 {
		t.Fatalf("future serial moved non-monotonically: %d", serial(next))
	}
}

func TestPrepareZoneForSyncAdvancesLedgerSerial(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	ctx := context.Background()
	zoneID, err := p.ensureZone(ctx, "serial.example")
	if err != nil {
		t.Fatalf("ensure zone: %v", err)
	}
	readSerial := func() uint64 {
		t.Helper()
		var soa string
		if err := p.db.GetDB().QueryRowContext(ctx,
			`SELECT content FROM pdns_records WHERE domain_id = ? AND type = 'SOA'`, zoneID).Scan(&soa); err != nil {
			t.Fatalf("read SOA: %v", err)
		}
		fields := strings.Fields(soa)
		value, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			t.Fatalf("parse SOA: %v", err)
		}
		return value
	}
	before := readSerial()
	if err := p.prepareZoneForSync(ctx, "serial.example"); err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	first := readSerial()
	if err := p.prepareZoneForSync(ctx, "serial.example"); err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	second := readSerial()
	if !(before < first && first < second) {
		t.Fatalf("SOA serial did not advance monotonically: %d, %d, %d", before, first, second)
	}
}
