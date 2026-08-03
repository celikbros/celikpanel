package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMailAuthApplyReportsSavedButUnpublished(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	attachStrictDNSRPCAgent(t, p, &strictDNSRPCAgent{failZone: "biovision.health"})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/mail/auth/apply",
		strings.NewReader(`{"record":"spf"}`))
	recorder := httptest.NewRecorder()
	p.handleMailAuthApply(recorder, request, "biovision.health")
	assertPublicationConflict(t, recorder)

	var content string
	if err := p.db.GetDB().QueryRow(`
		SELECT r.content FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = ? AND r.name = ? AND r.type = 'TXT'`,
		"biovision.health", "biovision.health",
	).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, spfRecommended()) {
		t.Fatalf("durable SPF content = %q, want %q", content, spfRecommended())
	}
}

func TestUpsertTXTUpdatesExistingRecordWithoutDuplicates(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	zoneID, err := p.ensureZone(context.Background(), "biovision.health")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.upsertTXT(context.Background(), zoneID, "_dmarc.biovision.health", "v=DMARC1; p=none"); err != nil {
		t.Fatal(err)
	}
	if err := p.upsertTXT(context.Background(), zoneID, "_dmarc.biovision.health", "v=DMARC1; p=reject"); err != nil {
		t.Fatal(err)
	}

	var count int
	var content string
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*), MAX(content) FROM pdns_records
		WHERE domain_id = ? AND name = ? AND type = 'TXT'`,
		zoneID, "_dmarc.biovision.health",
	).Scan(&count, &content); err != nil {
		t.Fatal(err)
	}
	if count != 1 || !strings.Contains(content, "p=reject") {
		t.Fatalf("TXT state = count:%d content:%q", count, content)
	}
}
