package main

import (
	"context"
	"strings"
	"testing"
)

func TestResolveParentDomainReturnsDatabaseFailure(t *testing.T) {
	p := newDNSPanelForTest(t)
	p.db.Close()

	_, _, _, err := p.resolveParentDomain(context.Background(), 1, "api.example.test")
	if err == nil || !strings.Contains(err.Error(), "query parent domains") {
		t.Fatalf("resolve error = %v, want explicit query failure", err)
	}
}

func TestAddSubdomainPublicationFailureKeepsRetryableLedger(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "2.25.80.4")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "example.test")
	agent := &strictDNSRPCAgent{failZone: "example.test"}
	attachStrictDNSRPCAgent(t, p, agent)

	result, err := p.addSubdomainToParentZone(
		context.Background(),
		"example.test",
		"api.example.test",
	)
	if err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	if !result.ParentZoneExists || !result.LedgerChanged || !result.LedgerReady || result.Published {
		t.Fatalf("first mutation result = %+v", result)
	}
	assertSubdomainRecordCount(t, p, "example.test", "api.example.test", "A", "2.25.80.4", 1)

	agent.mu.Lock()
	agent.failZone = ""
	agent.mu.Unlock()
	retry, err := p.addSubdomainToParentZone(
		context.Background(),
		"example.test",
		"api.example.test",
	)
	if err != nil {
		t.Fatalf("retry publication: %v", err)
	}
	if retry.LedgerChanged || !retry.LedgerReady || !retry.Published {
		t.Fatalf("retry result = %+v", retry)
	}
	assertSubdomainRecordCount(t, p, "example.test", "api.example.test", "A", "2.25.80.4", 1)
}

func TestAddSubdomainRefusesDifferentExistingAddress(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "2.25.80.4")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "example.test")
	zoneID := strictDNSZoneID(t, p, "example.test")
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO pdns_records (domain_id, name, type, content, ttl)
		VALUES (?, 'api.example.test', 'A', '2.25.80.4', 3600),
		       (?, 'api.example.test', 'A', '192.0.2.44', 3600)`, zoneID, zoneID); err != nil {
		t.Fatalf("insert conflicting record: %v", err)
	}

	result, err := p.addSubdomainToParentZone(
		context.Background(),
		"example.test",
		"api.example.test",
	)
	if err == nil || !strings.Contains(err.Error(), "different A address") {
		t.Fatalf("conflict error = %v", err)
	}
	if !result.ParentZoneExists || result.LedgerChanged || result.LedgerReady || result.Published {
		t.Fatalf("conflict result = %+v", result)
	}
	assertSubdomainRecordCount(t, p, "example.test", "api.example.test", "A", "2.25.80.4", 1)
	assertSubdomainRecordCount(t, p, "example.test", "api.example.test", "A", "192.0.2.44", 1)
}

func TestRemoveSubdomainPublicationFailureKeepsDeletionLedger(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "example.test")
	zoneID := strictDNSZoneID(t, p, "example.test")
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO pdns_records (domain_id, name, type, content, ttl)
		VALUES (?, 'api.example.test', 'A', '2.25.80.4', 3600)`, zoneID); err != nil {
		t.Fatalf("insert child record: %v", err)
	}
	agent := &strictDNSRPCAgent{failZone: "example.test"}
	attachStrictDNSRPCAgent(t, p, agent)

	result, err := p.removeSubdomainFromParentZone(
		context.Background(),
		"example.test",
		"api.example.test",
	)
	if err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	if !result.ParentZoneExists || !result.LedgerChanged || !result.LedgerReady || result.Published {
		t.Fatalf("first removal result = %+v", result)
	}
	assertSubdomainRecordCount(t, p, "example.test", "api.example.test", "A", "2.25.80.4", 0)

	agent.mu.Lock()
	agent.failZone = ""
	agent.mu.Unlock()
	retry, err := p.removeSubdomainFromParentZone(
		context.Background(),
		"example.test",
		"api.example.test",
	)
	if err != nil {
		t.Fatalf("retry removal publication: %v", err)
	}
	if retry.LedgerChanged || !retry.LedgerReady || !retry.Published {
		t.Fatalf("retry result = %+v", retry)
	}
}

func strictDNSZoneID(t *testing.T, p *Panel, domain string) int {
	t.Helper()
	var zoneID int
	if err := p.db.GetDB().QueryRow(
		`SELECT id FROM pdns_domains WHERE name = ?`,
		domain,
	).Scan(&zoneID); err != nil {
		t.Fatalf("find zone %s: %v", domain, err)
	}
	return zoneID
}

func assertSubdomainRecordCount(
	t *testing.T,
	p *Panel,
	parent string,
	name string,
	typ string,
	content string,
	want int,
) {
	t.Helper()
	var got int
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*)
		FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = ? AND r.name = ? AND r.type = ? AND r.content = ?`,
		parent,
		name,
		typ,
		content,
	).Scan(&got); err != nil {
		t.Fatalf("count subdomain record: %v", err)
	}
	if got != want {
		t.Fatalf("record count for %s %s %s = %d, want %d", name, typ, content, got, want)
	}
}
