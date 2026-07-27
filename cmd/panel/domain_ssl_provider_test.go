package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getDomainSSLForProviderTest(t *testing.T, p *Panel, domainID int) DomainSSLResponse {
	t.Helper()

	req := httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/domains/%d/ssl", domainID),
		nil,
	)
	rec := httptest.NewRecorder()
	p.handleDomainSSL(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET SSL status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var response DomainSSLResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode GET SSL response: %v", err)
	}
	if response.Certificate == nil {
		t.Fatal("GET SSL response has no active certificate")
	}
	return response
}

func TestGetDomainSSLUsesDurableACMEProviderIdentity(t *testing.T) {
	p, domainID, certID := newSSLStateFixture(t)
	attachAliasReissueAgent(t, p, &aliasReissueAgent{
		oldNames: []string{"ssl-state.example", "www.ssl-state.example"},
	})

	if _, err := p.db.GetDB().Exec(`
		UPDATE ssl_certificates
		SET type = 'letsencrypt',
		    issuer = 'R13',
		    acme_provider_id = 'zerossl',
		    lineage_name = 'ssl-state.example'
		WHERE id = ?
	`, certID); err != nil {
		t.Fatalf("set ACME certificate identity: %v", err)
	}

	response := getDomainSSLForProviderTest(t, p, domainID)
	if got := response.Certificate.ProviderID; got != "zerossl" {
		t.Fatalf("provider_id = %q, want durable value %q", got, "zerossl")
	}
}

func TestGetDomainSSLDoesNotInferProviderForCustomCertificate(t *testing.T) {
	p, domainID, certID := newSSLStateFixture(t)
	attachAliasReissueAgent(t, p, &aliasReissueAgent{
		oldNames: []string{"ssl-state.example", "www.ssl-state.example"},
	})

	if _, err := p.db.GetDB().Exec(`
		UPDATE ssl_certificates
		SET type = 'custom',
		    issuer = ?,
		    acme_provider_id = NULL,
		    lineage_name = NULL
		WHERE id = ?
	`, "Let's Encrypt", certID); err != nil {
		t.Fatalf("set custom certificate display issuer: %v", err)
	}

	response := getDomainSSLForProviderTest(t, p, domainID)
	if got := response.Certificate.ProviderID; got != "" {
		t.Fatalf("custom certificate provider_id = %q, want empty", got)
	}
}
