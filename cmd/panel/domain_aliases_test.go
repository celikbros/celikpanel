package main

import (
	"context"
	"errors"
	"testing"
)

func newDomainAliasFixture(t *testing.T) (*Panel, int) {
	t.Helper()
	p, domainID, _ := newSSLStateFixture(t)
	if _, err := p.db.GetDB().Exec(`DELETE FROM ssl_certificates WHERE domain_id = ?`, domainID); err != nil {
		t.Fatalf("remove fixture certificate: %v", err)
	}
	if _, err := p.db.GetDB().Exec(`
		UPDATE sites
		SET ssl_enabled = false, ssl_type = 'none',
		    ssl_cert_path = NULL, ssl_key_path = NULL,
		    force_https = false, hsts_enabled = false,
		    hsts_retire_after = NULL
		WHERE domain_id = ?`, domainID); err != nil {
		t.Fatalf("reset fixture site SSL: %v", err)
	}
	return p, domainID
}

func aliasCount(t *testing.T, p *Panel, domainID int, alias string) int {
	t.Helper()
	var count int
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM domain_aliases
		WHERE domain_id = ? AND alias = ?`, domainID, alias).Scan(&count); err != nil {
		t.Fatalf("count alias: %v", err)
	}
	return count
}

func TestCanonicalDomainAlias(t *testing.T) {
	got, err := canonicalDomainAlias("  WWW.Example.TEST. ")
	if err != nil {
		t.Fatalf("canonical alias: %v", err)
	}
	if got != "www.example.test" {
		t.Fatalf("canonical alias = %q", got)
	}
	for _, invalid := range []string{"", "localhost", "127.0.0.1", "bad_name.example"} {
		if _, err := canonicalDomainAlias(invalid); !errors.Is(err, errInvalidDomainAlias) {
			t.Fatalf("canonicalDomainAlias(%q) error = %v", invalid, err)
		}
	}
}

func TestAddDomainAliasRollsBackDatabaseAndVhostOnApplyFailure(t *testing.T) {
	p, domainID := newDomainAliasFixture(t)
	calls := 0
	apply := func(context.Context, int) error {
		calls++
		if calls == 1 {
			return errors.New("nginx validation failed")
		}
		return nil
	}
	if _, err := p.addDomainAlias(
		context.Background(), domainID, "alias.example.test", nil, apply,
	); err == nil {
		t.Fatal("alias add unexpectedly succeeded")
	}
	if got := aliasCount(t, p, domainID, "alias.example.test"); got != 0 {
		t.Fatalf("alias count after rollback = %d, want 0", got)
	}
	if calls != 2 {
		t.Fatalf("vhost apply calls = %d, want failed apply plus restore", calls)
	}
}

func TestDeleteDomainAliasRollsBackDatabaseAndVhostOnApplyFailure(t *testing.T) {
	p, domainID := newDomainAliasFixture(t)
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO domain_aliases (domain_id, alias)
		VALUES (?, 'alias.example.test')`, domainID); err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	calls := 0
	apply := func(context.Context, int) error {
		calls++
		if calls == 1 {
			return errors.New("nginx reload failed")
		}
		return nil
	}
	if err := p.deleteDomainAlias(
		context.Background(), domainID, "ALIAS.EXAMPLE.TEST.", apply,
	); err == nil {
		t.Fatal("alias deletion unexpectedly succeeded")
	}
	if got := aliasCount(t, p, domainID, "alias.example.test"); got != 1 {
		t.Fatalf("alias count after rollback = %d, want 1", got)
	}
	if calls != 2 {
		t.Fatalf("vhost apply calls = %d, want failed apply plus restore", calls)
	}
}

func TestAddDomainAliasRejectsPrimaryImplicitWWWAndUncoveredCertificate(t *testing.T) {
	p, domainID := newDomainAliasFixture(t)
	apply := func(context.Context, int) error {
		t.Fatal("vhost apply must not run for a rejected alias")
		return nil
	}
	for _, alias := range []string{"ssl-state.example", "www.ssl-state.example"} {
		if _, err := p.addDomainAlias(
			context.Background(), domainID, alias, nil, apply,
		); !errors.Is(err, errDomainAliasConflict) {
			t.Fatalf("add primary collision %q error = %v", alias, err)
		}
	}

	verify := func(context.Context, int, string) error {
		return errAliasCertificateCoverage
	}
	if _, err := p.addDomainAlias(
		context.Background(), domainID, "new.example.test", verify, apply,
	); !errors.Is(err, errAliasCertificateCoverage) {
		t.Fatalf("uncovered alias error = %v", err)
	}
	if got := aliasCount(t, p, domainID, "new.example.test"); got != 0 {
		t.Fatalf("uncovered alias count = %d, want 0", got)
	}
}
