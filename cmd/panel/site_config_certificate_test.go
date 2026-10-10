package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// D-031 step 1b at the Panel: certificate issuance and renewal for a site
// whose configuration file the owner kept. Two paths (the kept file includes
// the Panel's directory, or not) for both triggers (issuance, renewal), the
// ledger reasons and the typed answers.

func siteFileReason(t *testing.T, p *Panel, domainID int) string {
	t.Helper()
	siteID, _, err := p.siteIDForDomain(context.Background(), domainID)
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := services.LoadSiteFileRecord(context.Background(), p.db.GetDB(), siteID, transport.SiteFileKindNginxVhost)
	if err != nil || !found {
		t.Fatalf("ledger row: found=%v err=%v", found, err)
	}
	return record.StateReason
}

func activeCertificateState(t *testing.T, p *Panel, domainID int) (int, string, string) {
	t.Helper()
	var id int
	var path, status string
	if err := p.db.GetDB().QueryRow(`
		SELECT id, cert_path, COALESCE(renewal_status, '') FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'`, domainID).Scan(&id, &path, &status); err != nil {
		t.Fatalf("read active certificate: %v", err)
	}
	return id, path, status
}

func issueOnKeptFile(t *testing.T, validation string) (*Panel, int, *validationRestoreAgent, *httptest.ResponseRecorder) {
	t.Helper()
	const domain = "kept-issue.example"
	p, domainID := newIncludeMailFixture(t, domain)
	agent := &validationRestoreAgent{domain: domain, keptValidation: validation}
	attachValidationRestoreAgent(t, p, agent)
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/ssl/letsencrypt", domainID),
		strings.NewReader(`{"email":"admin@kept-issue.example","auto_renew":true}`))
	recorder := httptest.NewRecorder()
	p.handleIssueLetsEncrypt(recorder, request)
	return p, domainID, agent, recorder
}

func TestIssuanceOnAKeptFileThatIncludesThePanelDirectoryWaitsForTheOwner(t *testing.T) {
	p, domainID, agent, recorder := issueOnKeptFile(t, transport.SiteFileValidationReady)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != sslWaitingForOwner || body["pending_reason"] != services.SiteFileReasonCertificate {
		t.Fatalf("answer = %v", body)
	}
	agent.mu.Lock()
	issues, applies := len(agent.issueCalls), len(agent.applyCalls)
	agent.mu.Unlock()
	// One request; the validation render and the final render, both kept;
	// no "restore" of a file that was not changed.
	if issues != 1 || applies != 2 {
		t.Fatalf("issue calls = %d, apply calls = %d; want 1 and 2", issues, applies)
	}
	_, path, status := activeCertificateState(t, p, domainID)
	if path != "/certs/staged/fullchain.pem" || status != sslWaitingForOwner {
		t.Fatalf("active certificate %q status %q", path, status)
	}
	if reason := siteFileReason(t, p, domainID); reason != services.SiteFileReasonCertificate {
		t.Fatalf("ledger reason = %q", reason)
	}
	var sslEnabled bool
	if err := p.db.GetDB().QueryRow(`SELECT ssl_enabled FROM sites WHERE domain_id = ?`, domainID).Scan(&sslEnabled); err != nil {
		t.Fatal(err)
	}
	// The site is not disabled: CelikPanel's text held beside the file carries
	// the TLS block with the new certificate.
	if !sslEnabled {
		t.Fatal("the site's TLS state was disabled")
	}
}

func TestIssuanceOnAKeptFileWithoutThePanelDirectoryIsRefusedBeforeAnyRequest(t *testing.T) {
	for _, validation := range []string{
		transport.SiteFileValidationIncludeMissing,
		transport.SiteFileValidationNamesMissing,
		transport.SiteFileValidationChallengeKept,
	} {
		t.Run(validation, func(t *testing.T) {
			p, domainID, agent, recorder := issueOnKeptFile(t, validation)
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != errCodeSiteConfigOwnerEdited || body.Reason != services.SiteFileReasonCertificateValidation ||
				body.Detail != validation || !strings.Contains(body.Error, "no certificate was requested") {
				t.Fatalf("answer = %+v", body)
			}
			if body.Vars["include"] != "include /etc/nginx/celikpanel-managed.d/kept-issue.example/*.conf;" {
				t.Fatalf("vars = %v", body.Vars)
			}
			agent.mu.Lock()
			issues, applies := len(agent.issueCalls), len(agent.applyCalls)
			agent.mu.Unlock()
			if issues != 0 || applies != 1 {
				t.Fatalf("issue calls = %d, apply calls = %d; want 0 and 1", issues, applies)
			}
			if reason := siteFileReason(t, p, domainID); reason != services.SiteFileReasonCertificateValidation {
				t.Fatalf("ledger reason = %q", reason)
			}
		})
	}
}

func renewOnKeptFile(t *testing.T, validation, mode string) (*Panel, int, int, *validationRestoreAgent) {
	t.Helper()
	p, domainID, certificateID, agent := newValidationRestoreRenewalFixture(t, mode)
	agent.keptValidation = validation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.renewLetsEncrypt(ctx, certificateID, domainID, "ssl-state.example")
	return p, domainID, certificateID, agent
}

func TestRenewalOnAKeptFileWithoutThePanelDirectoryWaitsForTheOwner(t *testing.T) {
	p, domainID, certificateID, agent := renewOnKeptFile(t, transport.SiteFileValidationIncludeMissing, "exact")
	agent.mu.Lock()
	renews, applies := agent.renewCalls, len(agent.applyCalls)
	agent.mu.Unlock()
	if renews != 0 || applies != 1 {
		t.Fatalf("renew calls = %d, apply calls = %d; want 0 and 1", renews, applies)
	}
	id, _, status := activeCertificateState(t, p, domainID)
	// Waiting for the owner, not failed; the certificate in use is unchanged.
	if id != certificateID || status != sslWaitingForOwner {
		t.Fatalf("active certificate %d status %q", id, status)
	}
	if reason := siteFileReason(t, p, domainID); reason != services.SiteFileReasonCertificateValidation {
		t.Fatalf("ledger reason = %q", reason)
	}
	// The site-config page names it, with the days left on the certificate
	// in use.
	certificate := p.siteConfigCertificateFor(context.Background(), domainID, services.SiteFileReasonCertificateValidation, false)
	if certificate == nil || certificate.ServedDaysLeft == nil || certificate.ServedExpiresAt == "" {
		t.Fatalf("certificate part = %+v", certificate)
	}
}

func TestRenewalOnAKeptFileThatIncludesThePanelDirectoryInstallsAndWaits(t *testing.T) {
	p, domainID, certificateID, agent := renewOnKeptFile(t, transport.SiteFileValidationReady, "exact")
	agent.mu.Lock()
	renews, applies := agent.renewCalls, len(agent.applyCalls)
	agent.mu.Unlock()
	if renews != 1 || applies != 2 {
		t.Fatalf("renew calls = %d, apply calls = %d; want 1 and 2", renews, applies)
	}
	id, path, status := activeCertificateState(t, p, domainID)
	if id == certificateID || path != "/certs/renewed/fullchain.pem" || status != sslWaitingForOwner {
		t.Fatalf("active certificate %d %q status %q", id, path, status)
	}
	if reason := siteFileReason(t, p, domainID); reason != services.SiteFileReasonCertificate {
		t.Fatalf("ledger reason = %q", reason)
	}
	certificate := p.siteConfigCertificateFor(context.Background(), domainID, services.SiteFileReasonCertificate, false)
	if certificate == nil || certificate.CertPath != "/certs/renewed/fullchain.pem" || certificate.ServedExpiresAt == "" {
		t.Fatalf("certificate part = %+v", certificate)
	}
}

func TestCertificateReasonEndsWhenTheFileUsesTheCertificateOrIsCelikPanelsText(t *testing.T) {
	for _, testCase := range []struct {
		name string
		file func(transport.SiteFileResult) transport.SiteFileResult
		want string
		ends bool
	}{
		{"kept file names the certificate", func(f transport.SiteFileResult) transport.SiteFileResult {
			f.CertificateReferenced = true
			return f
		}, "", true},
		{"kept file does not", func(f transport.SiteFileResult) transport.SiteFileResult { return f },
			services.SiteFileReasonCertificate, false},
		{"CelikPanel's text taken", func(f transport.SiteFileResult) transport.SiteFileResult {
			f.State, f.Outcome = transport.SiteFileManagedUnchanged, transport.SiteFileOutcomeTaken
			f.WrittenSHA256 = f.RenderSHA256
			return f
		}, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			p, domainID, _, _ := renewOnKeptFile(t, transport.SiteFileValidationReady, "exact")
			siteID, _, err := p.siteIDForDomain(context.Background(), domainID)
			if err != nil {
				t.Fatal(err)
			}
			kept := transport.SiteFileResult{
				Kind: transport.SiteFileKindNginxVhost, Path: "/etc/nginx/sites-available/ssl-state.example.conf",
				State: transport.SiteFileOwnerEdited, Outcome: transport.SiteFileOutcomeKept,
				RenderSHA256: strings.Repeat("b", 64), FileSHA256: strings.Repeat("c", 64),
			}
			// A start renders every site again: the reason is not erased by it.
			p.recordSiteFile(context.Background(), siteID, domainID, "ssl-state.example", transport.SiteFileTriggerStartup, &kept, "")
			if reason := siteFileReason(t, p, domainID); reason != services.SiteFileReasonCertificate {
				t.Fatalf("after a start the reason is %q", reason)
			}
			next := testCase.file(kept)
			p.recordSiteFile(context.Background(), siteID, domainID, "ssl-state.example", transport.SiteFileTriggerChange, &next, "")
			if reason := siteFileReason(t, p, domainID); reason != testCase.want {
				t.Fatalf("reason = %q, want %q", reason, testCase.want)
			}
			_, _, status := activeCertificateState(t, p, domainID)
			if (status == "") != testCase.ends {
				t.Fatalf("renewal status = %q, ends = %v", status, testCase.ends)
			}
		})
	}
}

// The dashboard's attention list carries a certificate waiting for the owner
// whatever its days, with the days left on the certificate in use.
func TestDashboardListsACertificateWaitingForTheOwner(t *testing.T) {
	p, domainID, _, _ := renewOnKeptFile(t, transport.SiteFileValidationIncludeMissing, "exact")
	recorder := httptest.NewRecorder()
	p.handleDashboard(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body dashboardExtras
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.ExpiringCerts) != 1 || !body.ExpiringCerts[0].WaitingForOwner ||
		body.ExpiringCerts[0].DomainID != domainID || body.ExpiringCerts[0].ServedDaysLeft == nil {
		t.Fatalf("attention = %+v", body.ExpiringCerts)
	}
}
