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

// D-031 step 1b, second round (2026-10-10): the kept file's readiness is
// measured by the Agent's probe; a measured "ready" ends the
// certificate_validation reason at the site-config read and at the next
// certificate operation; an unanswered probe is unknown, not "not ready";
// the alias-certificate path follows the same two paths as issuance.

func TestCertificateOperationsAskForTheProbeAndOtherRendersDoNot(t *testing.T) {
	p, _, agent, recorder := issueOnKeptFile(t, transport.SiteFileValidationReady)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	_ = p
	calls := validationRestoreApplyCalls(agent)
	if len(calls) != 2 || !calls[0].ProbeValidation || calls[1].ProbeValidation {
		t.Fatalf("probe asked for = %v", calls)
	}
}

func TestARefusalNamesTheNameAndStatusTheProbeGot(t *testing.T) {
	const domain = "kept-issue.example"
	p, domainID := newIncludeMailFixture(t, domain)
	agent := &validationRestoreAgent{domain: domain, keptValidation: transport.SiteFileValidationIncludeMissing,
		keptName: "www." + domain, keptStatus: http.StatusMovedPermanently}
	attachValidationRestoreAgent(t, p, agent)
	recorder := httptest.NewRecorder()
	p.handleIssueLetsEncrypt(recorder, httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/ssl/letsencrypt", domainID),
		strings.NewReader(`{"email":"admin@kept-issue.example"}`)))
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || recorder.Code != http.StatusConflict {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}
	if body.Detail != transport.SiteFileValidationIncludeMissing || body.Vars["name"] != "www."+domain ||
		body.Vars["status"] != "301" || body.Vars["include"] == "" {
		t.Fatalf("answer = %+v", body)
	}
	if !strings.Contains(body.Error, "The server administrator chooses") {
		t.Fatalf("the actor is not named: %q", body.Error)
	}
}

func TestAnUnansweredProbeIsUnknownAndRecordsNoReason(t *testing.T) {
	p, domainID, agent, recorder := issueOnKeptFile(t, transport.SiteFileValidationUnknown)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeCertificateValidationUnknown || body.Reason != "" ||
		!strings.Contains(body.Error, "no certificate was requested") {
		t.Fatalf("answer = %+v", body)
	}
	agent.mu.Lock()
	issues := len(agent.issueCalls)
	agent.mu.Unlock()
	if issues != 0 {
		t.Fatalf("issue calls = %d", issues)
	}
	if reason := siteFileReason(t, p, domainID); reason != "" {
		t.Fatalf("an unknown readiness wrote the reason %q", reason)
	}

	// Renewal: neither waiting for the owner nor failed; nothing recorded.
	p2, domainID2, certificateID, agent2 := renewOnKeptFile(t, transport.SiteFileValidationUnknown, "exact")
	id, _, status := activeCertificateState(t, p2, domainID2)
	agent2.mu.Lock()
	renews := agent2.renewCalls
	agent2.mu.Unlock()
	if id != certificateID || status != "" || renews != 0 {
		t.Fatalf("renewal with unknown readiness: id=%d status=%q renews=%d", id, status, renews)
	}
}

func readSiteConfigView(t *testing.T, p *Panel, domainID int) siteConfigView {
	t.Helper()
	recorder := httptest.NewRecorder()
	p.handleSiteConfigRead(recorder, httptest.NewRequest(http.MethodGet, "/x", nil), domainID)
	var view siteConfigView
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("site-config read: %d %s", recorder.Code, recorder.Body.String())
	}
	return view
}

func TestTheSiteConfigReadEndsTheValidationReasonWhenTheProbeIsReady(t *testing.T) {
	p, domainID, _, agent := renewOnKeptFile(t, transport.SiteFileValidationIncludeMissing, "exact")
	if _, _, status := activeCertificateState(t, p, domainID); status != sslWaitingForOwner {
		t.Fatalf("renewal status %q", status)
	}
	if reason := p.certificateWaitingReason(context.Background(), domainID); reason != services.SiteFileReasonCertificateValidation {
		t.Fatalf("SSL waiting reason %q", reason)
	}

	// Still not ready: the reason stays, the read names the probe's answer.
	agent.mu.Lock()
	agent.keptName, agent.keptStatus = "ssl-state.example", http.StatusNotFound
	agent.mu.Unlock()
	view := readSiteConfigView(t, p, domainID)
	if view.PendingReason != services.SiteFileReasonCertificateValidation || view.Validation != transport.SiteFileValidationIncludeMissing ||
		view.ValidationName != "ssl-state.example" || view.ValidationStatus != http.StatusNotFound || view.ResolvedReason != "" {
		t.Fatalf("not ready: %+v", view)
	}

	// The owner fixed the file: the read measures ready and ends the reason,
	// the renewal's waiting state and with them the dashboard entry.
	agent.mu.Lock()
	agent.keptValidation = transport.SiteFileValidationReady
	agent.mu.Unlock()
	view = readSiteConfigView(t, p, domainID)
	if view.ResolvedReason != services.SiteFileReasonCertificateValidation || view.PendingReason != "" ||
		view.Validation != transport.SiteFileValidationReady {
		t.Fatalf("ready: %+v", view)
	}
	if reason := siteFileReason(t, p, domainID); reason != "" {
		t.Fatalf("ledger reason after ready = %q", reason)
	}
	if _, _, status := activeCertificateState(t, p, domainID); status != "" {
		t.Fatalf("renewal status after ready = %q", status)
	}
	recorder := httptest.NewRecorder()
	p.handleDashboard(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil))
	var dashboard dashboardExtras
	_ = json.Unmarshal(recorder.Body.Bytes(), &dashboard)
	for _, entry := range dashboard.ExpiringCerts {
		if entry.WaitingForOwner {
			t.Fatalf("dashboard still lists it: %+v", entry)
		}
	}

	// With no reason left the read asks for no probe.
	view = readSiteConfigView(t, p, domainID)
	agent.mu.Lock()
	calls := append([]ValidationRestoreApplyRequest(nil), agent.inspectCalls...)
	agent.mu.Unlock()
	if len(calls) != 3 || !calls[0].ProbeValidation || !calls[1].ProbeValidation || calls[2].ProbeValidation {
		t.Fatalf("probe asked for at reads = %+v", calls)
	}
	if view.ResolvedReason != "" || view.Validation != "" {
		t.Fatalf("read without a reason: %+v", view)
	}
}

func TestTheNextCertificateOperationEndsTheValidationReasonWhenReady(t *testing.T) {
	p, domainID, certificateID, agent := renewOnKeptFile(t, transport.SiteFileValidationIncludeMissing, "exact")
	agent.mu.Lock()
	agent.keptValidation = transport.SiteFileValidationReady
	agent.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.renewLetsEncrypt(ctx, certificateID, domainID, "ssl-state.example")
	// Renewed on the kept file: the reason is now the new certificate's.
	if reason := siteFileReason(t, p, domainID); reason != services.SiteFileReasonCertificate {
		t.Fatalf("reason = %q", reason)
	}
	if reason := p.certificateWaitingReason(context.Background(), domainID); reason != services.SiteFileReasonCertificate {
		t.Fatalf("SSL waiting reason %q", reason)
	}
}

func TestTheAliasCertificatePathFollowsTheIssuancePaths(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		validation string
		wantIssue  int
		wantApply  int
	}{
		{"not ready", transport.SiteFileValidationIncludeMissing, 0, 1},
		{"ready", transport.SiteFileValidationReady, 1, 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			p, domainID, _, agent := newValidationRestoreRenewalFixture(t, "exact")
			agent.keptValidation = testCase.validation
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			current, err := p.loadActiveAliasCertificate(ctx, domainID)
			if err != nil || current == nil {
				t.Fatalf("active certificate: %v %v", current, err)
			}
			_, issueErr := p.issueAliasCertificateSnapshot(ctx, domainID, current,
				[]string{"ssl-state.example", "www.ssl-state.example", "alias.example"}, []string{"alias.example"})
			if issueErr == nil {
				t.Fatal("the fake's issued names differ; an error was expected")
			}
			agent.mu.Lock()
			issues, applies := len(agent.issueCalls), len(agent.applyCalls)
			agent.mu.Unlock()
			// No "restore" render of a kept file that was not changed.
			if issues != testCase.wantIssue || applies != testCase.wantApply {
				t.Fatalf("issue calls = %d, apply calls = %d", issues, applies)
			}
			recorder := httptest.NewRecorder()
			handled := writeCertificateSiteFileRefusal(recorder, issueErr)
			if testCase.validation == transport.SiteFileValidationReady {
				if handled || strings.Contains(issueErr.Error(), "validation vhost") {
					t.Fatalf("ready path stopped at the file: %v", issueErr)
				}
				return
			}
			var body apiErrorBody
			_ = json.Unmarshal(recorder.Body.Bytes(), &body)
			if !handled || recorder.Code != http.StatusConflict || body.Code != errCodeSiteConfigOwnerEdited ||
				body.Reason != services.SiteFileReasonCertificateValidation {
				t.Fatalf("not ready: handled=%v %d %+v", handled, recorder.Code, body)
			}
		})
	}
}

func TestDashboardOrdersWaitingCertificatesExpiredFirst(t *testing.T) {
	days := func(value int) *int { return &value }
	entries := []dashboardExpiringCert{
		{DomainName: "plain.example", DaysLeft: 3},
		{DomainName: "waiting-later.example", DaysLeft: 80, WaitingForOwner: true, ServedDaysLeft: days(12)},
		{DomainName: "waiting-expired.example", DaysLeft: 80, WaitingForOwner: true, ServedDaysLeft: days(-4)},
		{DomainName: "waiting-soon.example", DaysLeft: 80, WaitingForOwner: true, ServedDaysLeft: days(2)},
	}
	sortDashboardCertificates(entries)
	var order []string
	for _, entry := range entries {
		order = append(order, entry.DomainName)
	}
	if strings.Join(order, ",") != "waiting-expired.example,waiting-soon.example,waiting-later.example,plain.example" {
		t.Fatalf("order = %v", order)
	}
}
