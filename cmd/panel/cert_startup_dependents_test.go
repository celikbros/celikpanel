package main

import (
	"context"
	"strings"
	"testing"
)

func TestStartupCertificateDependentsRepublishesFullDurableMailSnapshot(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "first-startup.example", "/certs/first-startup", "active", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "second-startup.example", "/certs/second-startup", "active", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "revoked-startup.example", "/certs/revoked-startup", "revoked", true,
	)
	addMailTLSIsolationDomain(
		t, p, subscriptionID, "web-only-startup.example", "/certs/web-only-startup", "active", false,
	)

	pdnsResult, err := p.db.GetDB().Exec(`
		INSERT INTO pdns_domains (name, type)
		VALUES ('first-startup.example', 'NATIVE')`)
	if err != nil {
		t.Fatalf("insert startup TLSA zone: %v", err)
	}
	pdnsDomainID, err := pdnsResult.LastInsertId()
	if err != nil {
		t.Fatalf("startup TLSA zone id: %v", err)
	}
	const originalTLSA = "3 0 1 0123456789abcdef"
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO pdns_records (domain_id, name, type, content, ttl)
		VALUES (?, '_25._tcp.mail.first-startup.example', 'TLSA', ?, 3600)`,
		pdnsDomainID, originalTLSA); err != nil {
		t.Fatalf("insert startup TLSA record: %v", err)
	}

	agent := &mailTLSIsolationRPCAgent{certificates: map[string]MailTLSInspectRPCResponse{
		"/certs/first-startup":  validMailTLSCertificate("first-startup.example"),
		"/certs/second-startup": validMailTLSCertificate("second-startup.example"),
	}}
	attachMailTLSIsolationAgent(t, p, agent)

	count, err := p.reconcileCertificateDependentsAtStartup(context.Background())
	if err != nil {
		t.Fatalf("startup certificate dependent reconcile: %v", err)
	}
	if count != 2 {
		t.Fatalf("active secure-mail domain count = %d, want 2", count)
	}
	wantPaths := []string{"/certs/first-startup", "/certs/second-startup"}
	if got := mailTLSSnapshotPaths(agent); strings.Join(got, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("startup mail SNI snapshot paths = %v, want %v", got, wantPaths)
	}
	agent.mu.Lock()
	if len(agent.secureCalls) != 1 {
		agent.mu.Unlock()
		t.Fatalf("SecureMailTLS calls = %d, want one full-state push", len(agent.secureCalls))
	}
	agent.mu.Unlock()

	var secureMailRows int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM ssl_certificates WHERE secure_mail = 1`,
	).Scan(&secureMailRows); err != nil {
		t.Fatalf("count durable secure-mail settings: %v", err)
	}
	if secureMailRows != 3 {
		t.Fatalf("durable secure-mail settings changed: rows = %d, want 3", secureMailRows)
	}
	var currentTLSA string
	if err := p.db.GetDB().QueryRow(`
		SELECT content FROM pdns_records
		WHERE domain_id = ? AND type = 'TLSA'`, pdnsDomainID).Scan(&currentTLSA); err != nil {
		t.Fatalf("read startup TLSA record: %v", err)
	}
	if currentTLSA != originalTLSA {
		t.Fatalf("startup reconcile mutated user TLSA record: got %q, want %q", currentTLSA, originalTLSA)
	}
}

func TestStartupCertificateDependentsPublishesEmptySnapshot(t *testing.T) {
	p, _ := newMailTLSIsolationFixture(t)
	agent := &mailTLSIsolationRPCAgent{
		certificates: map[string]MailTLSInspectRPCResponse{},
	}
	attachMailTLSIsolationAgent(t, p, agent)

	count, err := p.reconcileCertificateDependentsAtStartup(context.Background())
	if err != nil {
		t.Fatalf("empty startup certificate dependent reconcile: %v", err)
	}
	if count != 0 {
		t.Fatalf("active secure-mail domain count = %d, want 0", count)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.inspectCalls) != 0 {
		t.Fatalf("empty startup snapshot inspected certificates: %v", agent.inspectCalls)
	}
	if len(agent.secureCalls) != 1 || len(agent.secureCalls[0]) != 0 {
		t.Fatalf("empty startup SNI publication = %v, want one empty full-state push", agent.secureCalls)
	}
}

func TestStartupCertificateDependentsRejectsPartialSnapshotOverLimit(t *testing.T) {
	p, subscriptionID := newMailTLSIsolationFixture(t)
	for index, name := range []string{
		"one-limit.example",
		"two-limit.example",
		"three-limit.example",
	} {
		addMailTLSIsolationDomain(
			t,
			p,
			subscriptionID,
			name,
			"/certs/limit-"+string(rune('a'+index)),
			"active",
			true,
		)
	}
	agent := &mailTLSIsolationRPCAgent{
		certificates: map[string]MailTLSInspectRPCResponse{},
	}
	attachMailTLSIsolationAgent(t, p, agent)

	count, err := p.reconcileCertificateDependentsAtStartupWithLimit(
		context.Background(),
		2,
	)
	if err == nil || !strings.Contains(err.Error(), "safe startup limit 2") {
		t.Fatalf("over-limit startup reconcile error = %v", err)
	}
	if count != 0 {
		t.Fatalf("over-limit active secure-mail domain count = %d, want 0", count)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.inspectCalls) != 0 || len(agent.secureCalls) != 0 {
		t.Fatalf(
			"over-limit reconcile published a destructive partial snapshot: inspect=%v secure=%v",
			agent.inspectCalls,
			agent.secureCalls,
		)
	}
}
