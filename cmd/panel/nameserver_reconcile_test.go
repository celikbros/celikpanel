package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func seedReconcileZone(t *testing.T, p *Panel, name string) int {
	t.Helper()
	ctx := context.Background()
	result, err := p.db.GetDB().ExecContext(ctx,
		`INSERT INTO pdns_domains (name, type) VALUES (?, 'NATIVE')`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	soa := fmt.Sprintf("old-ns.%s hostmaster.%s 2026072600 10800 3600 604800 3600", name, name)
	for _, record := range []struct{ name, typ, content string }{
		{name, "SOA", soa},
		{name, "NS", "old-ns." + name},
	} {
		if _, err := p.db.GetDB().ExecContext(ctx, `
			INSERT INTO pdns_records (domain_id, name, type, content, ttl, disabled, auth)
			VALUES (?, ?, ?, ?, 3600, 0, 1)`, id, record.name, record.typ, record.content); err != nil {
			t.Fatal(err)
		}
	}
	return int(id)
}

func insertReconcileRecord(t *testing.T, p *Panel, zoneID int, name, typ, content string) {
	t.Helper()
	if _, err := p.db.GetDB().ExecContext(context.Background(), `
		INSERT INTO pdns_records (domain_id, name, type, content, ttl, disabled, auth)
		VALUES (?, ?, ?, ?, 300, 0, 1)`, zoneID, name, typ, content); err != nil {
		t.Fatal(err)
	}
}

func assertSingleReconciledA(t *testing.T, p *Panel, zoneID int, name, want string) {
	t.Helper()
	var count int
	var minimum, maximum string
	if err := p.db.GetDB().QueryRowContext(context.Background(), `
		SELECT COUNT(*), COALESCE(MIN(content), ''), COALESCE(MAX(content), '')
		FROM pdns_records
		WHERE domain_id = ? AND LOWER(TRIM(name, '.')) = ? AND UPPER(type) = 'A'`,
		zoneID, canonicalDNSName(name)).Scan(&count, &minimum, &maximum); err != nil {
		t.Fatal(err)
	}
	if count != 1 || minimum != want || maximum != want {
		t.Fatalf("%s A RRset = count %d, range %q..%q; want one %s", name, count, minimum, maximum, want)
	}
}

func recordContent(t *testing.T, p *Panel, zoneID int, name, typ string) string {
	t.Helper()
	var content string
	if err := p.db.GetDB().QueryRowContext(context.Background(), `
		SELECT content FROM pdns_records WHERE domain_id = ? AND name = ? AND type = ?`,
		zoneID, name, typ).Scan(&content); err != nil {
		t.Fatal(err)
	}
	return content
}

func TestSaveNameserversLedgerReconcilesStandaloneOwnerZone(t *testing.T) {
	p := newDNSPanelForTest(t)
	ctx := context.Background()
	ownerID := seedReconcileZone(t, p, "celikhost.com")
	customerID := seedReconcileZone(t, p, "biovision.health")
	if err := p.setSetting(ctx, settingDNSRole, "standalone"); err != nil {
		t.Fatal(err)
	}

	insertReconcileRecord(t, p, ownerID, "ns1.celikhost.com", "A", "203.0.113.1")
	insertReconcileRecord(t, p, ownerID, "NS1.CELIKHOST.COM.", "a", "203.0.113.2")
	insertReconcileRecord(t, p, ownerID, "ns2.celikhost.com", "A", "203.0.113.3")
	insertReconcileRecord(t, p, ownerID, "api.celikhost.com", "A", "203.0.113.44")
	insertReconcileRecord(t, p, ownerID, "ns1.celikhost.com", "TXT", "keep-related-other-type")
	insertReconcileRecord(t, p, customerID, "ns1.celikhost.com", "A", "198.51.100.9")

	zones, err := p.saveNameserversLedger(ctx,
		"ns1.celikhost.com", "ns2.celikhost.com", "72.62.38.15")
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 2 {
		t.Fatalf("zones queued for sync = %v, want both ledger zones", zones)
	}
	assertSingleReconciledA(t, p, ownerID, "ns1.celikhost.com", "72.62.38.15")
	assertSingleReconciledA(t, p, ownerID, "ns2.celikhost.com", "72.62.38.15")
	if got := recordContent(t, p, ownerID, "api.celikhost.com", "A"); got != "203.0.113.44" {
		t.Fatalf("unrelated A record changed to %q", got)
	}
	if got := recordContent(t, p, ownerID, "ns1.celikhost.com", "TXT"); got != "keep-related-other-type" {
		t.Fatalf("non-A nameserver record changed to %q", got)
	}
	if got := recordContent(t, p, customerID, "ns1.celikhost.com", "A"); got != "198.51.100.9" {
		t.Fatalf("out-of-bailiwick customer-zone record changed to %q", got)
	}

	var soa string
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT content FROM pdns_records WHERE domain_id = ? AND type = 'SOA'`, ownerID).Scan(&soa); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(soa, "ns1.celikhost.com ") {
		t.Fatalf("SOA primary was not updated: %q", soa)
	}
	if p.setting(ctx, settingNS1) != "ns1.celikhost.com" || p.setting(ctx, settingNS2) != "ns2.celikhost.com" {
		t.Fatal("nameserver settings and ledger rewrite were not committed together")
	}
}

func TestPairedReconciliationMapsEachNameserverInItsOwningZone(t *testing.T) {
	p := newDNSPanelForTest(t)
	ctx := context.Background()
	localOwnerID := seedReconcileZone(t, p, "celikhost.com")
	peerOwnerID := seedReconcileZone(t, p, "otherdns.net")
	insertReconcileRecord(t, p, localOwnerID, "unrelated.celikhost.com", "A", "203.0.113.10")

	err := p.saveDNSClusterSettingsAndReconcile(ctx,
		"paired", "2.25.80.4", "ns2.otherdns.net",
		"ns1.celikhost.com", "ns2.otherdns.net", "72.62.38.15")
	if err != nil {
		t.Fatal(err)
	}
	assertSingleReconciledA(t, p, localOwnerID, "ns1.celikhost.com", "72.62.38.15")
	assertSingleReconciledA(t, p, peerOwnerID, "ns2.otherdns.net", "2.25.80.4")
	if got := recordContent(t, p, localOwnerID, "unrelated.celikhost.com", "A"); got != "203.0.113.10" {
		t.Fatalf("unrelated owner-zone record changed to %q", got)
	}

	var peerAAAA int
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pdns_records
		WHERE domain_id = ? AND name = 'ns2.otherdns.net' AND type = 'AAAA'`, peerOwnerID).Scan(&peerAAAA); err != nil {
		t.Fatal(err)
	}
	if peerAAAA != 0 {
		t.Fatalf("reconciliation invented %d peer IPv6 records", peerAAAA)
	}
	if p.setting(ctx, settingDNSRole) != "paired" ||
		p.setting(ctx, settingDNSPeerIP) != "2.25.80.4" ||
		p.setting(ctx, settingDNSPeerNS) != "ns2.otherdns.net" {
		t.Fatal("paired settings were not committed with the address mapping")
	}
}

func TestBostonPairedPlanUsesNS2AsLocalSOAMName(t *testing.T) {
	p := newDNSPanelForTest(t)
	ctx := context.Background()
	ownerID := seedReconcileZone(t, p, "celikhost.com")
	customerID := seedReconcileZone(t, p, "biovision.health")

	err := p.saveDNSClusterSettingsAndReconcile(ctx,
		"paired", "72.62.38.15", "ns1.celikhost.com",
		"ns1.celikhost.com", "ns2.celikhost.com", "2.25.80.4")
	if err != nil {
		t.Fatal(err)
	}
	for _, zone := range []struct {
		id   int
		name string
	}{{ownerID, "celikhost.com"}, {customerID, "biovision.health"}} {
		soa := recordContent(t, p, zone.id, zone.name, "SOA")
		if !strings.HasPrefix(soa, "ns2.celikhost.com ") {
			t.Fatalf("%s SOA MNAME = %q, want Boston-local ns2", zone.name, soa)
		}
	}
	assertSingleReconciledA(t, p, ownerID, "ns1.celikhost.com", "72.62.38.15")
	assertSingleReconciledA(t, p, ownerID, "ns2.celikhost.com", "2.25.80.4")

	// Saving the shared names later must not silently put the peer (ns1) back
	// into SOA MNAME.
	if _, err := p.saveNameserversLedger(ctx,
		"ns1.celikhost.com", "ns2.celikhost.com", "2.25.80.4"); err != nil {
		t.Fatal(err)
	}
	for _, zone := range []struct {
		id   int
		name string
	}{{ownerID, "celikhost.com"}, {customerID, "biovision.health"}} {
		soa := recordContent(t, p, zone.id, zone.name, "SOA")
		if !strings.HasPrefix(soa, "ns2.celikhost.com ") {
			t.Fatalf("nameserver save changed %s SOA MNAME to %q", zone.name, soa)
		}
	}
}

func TestDNSClusterReconciliationRollsBackSettingsAndAddresses(t *testing.T) {
	p := newDNSPanelForTest(t)
	ctx := context.Background()
	ownerID := seedReconcileZone(t, p, "celikhost.com")
	insertReconcileRecord(t, p, ownerID, "ns1.celikhost.com", "A", "198.51.100.1")
	insertReconcileRecord(t, p, ownerID, "ns2.celikhost.com", "A", "198.51.100.2")
	for key, value := range map[string]string{
		settingDNSRole: "standalone", settingDNSPeerIP: "", settingDNSPeerNS: "",
	} {
		if err := p.setSetting(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.db.GetDB().ExecContext(ctx, `
		CREATE TRIGGER reject_reconciled_peer_a
		BEFORE INSERT ON pdns_records
		WHEN NEW.type = 'A' AND NEW.name = 'ns2.celikhost.com'
		BEGIN SELECT RAISE(ABORT, 'reject peer A'); END`); err != nil {
		t.Fatal(err)
	}

	if err := p.saveDNSClusterSettingsAndReconcile(ctx,
		"paired", "2.25.80.4", "ns2.celikhost.com",
		"ns1.celikhost.com", "ns2.celikhost.com", "72.62.38.15"); err == nil {
		t.Fatal("reconciliation unexpectedly succeeded")
	}
	if p.setting(ctx, settingDNSRole) != "standalone" ||
		p.setting(ctx, settingDNSPeerIP) != "" ||
		p.setting(ctx, settingDNSPeerNS) != "" {
		t.Fatal("failed address rewrite left partially saved cluster settings")
	}
	assertSingleReconciledA(t, p, ownerID, "ns1.celikhost.com", "198.51.100.1")
	assertSingleReconciledA(t, p, ownerID, "ns2.celikhost.com", "198.51.100.2")
	if soa := recordContent(t, p, ownerID, "celikhost.com", "SOA"); !strings.HasPrefix(soa, "old-ns.celikhost.com ") {
		t.Fatalf("failed reconciliation left a partial SOA MNAME rewrite: %q", soa)
	}
}

func TestNameserverAddressPlanRejectsIPv6ForARecords(t *testing.T) {
	if _, _, err := nameserverAddressesForPlan(nameserverAddressPlan{
		Role: "paired", NS1: "ns1.celikhost.com", NS2: "ns2.celikhost.com",
		PeerNS: "ns2.celikhost.com", LocalIPv4: "72.62.38.15", PeerIPv4: "2001:db8::2",
	}); err == nil {
		t.Fatal("IPv6 peer address was accepted for an A-record mapping")
	}
}

func TestNameserverAddressPlanRejectsLocalAddressAsPeer(t *testing.T) {
	for _, peerIP := range []string{"72.62.38.15", "::ffff:72.62.38.15"} {
		t.Run(peerIP, func(t *testing.T) {
			if _, _, err := nameserverAddressesForPlan(nameserverAddressPlan{
				Role: "paired", NS1: "ns1.celikhost.com", NS2: "ns2.celikhost.com",
				PeerNS: "ns2.celikhost.com", LocalIPv4: "72.62.38.15", PeerIPv4: peerIP,
			}); err == nil {
				t.Fatal("local server address was accepted as the peer A-record mapping")
			}
		})
	}
}
