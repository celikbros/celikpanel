package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

func TestMailSetupWebmailAvailabilityStaysTenantScoped(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	grantTeamMemberDomainCapability(
		t, &fixture, authzMatrixCustomerDomainID,
		core.TeamCapabilityMail, core.TeamPermissionView,
	)

	probeCalls := 0
	fixture.panel.webmailReadinessProbe = func(context.Context) bool {
		probeCalls++
		return true
	}
	caller := teamMemberAuthzTestCaller()

	foreign := teamMemberAuthzTestRequest(
		http.MethodGet,
		"/api/v1/domains/9903/mail/setup",
		caller,
	)
	foreignRecorder := httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(foreignRecorder, foreign)
	if foreignRecorder.Code != http.StatusNotFound {
		t.Fatalf("foreign status = %d, want 404; body=%s", foreignRecorder.Code, foreignRecorder.Body.String())
	}
	if probeCalls != 0 {
		t.Fatal("foreign-domain request reached global webmail readiness probe")
	}

	owned := teamMemberAuthzTestRequest(
		http.MethodGet,
		"/api/v1/domains/9902/mail/setup",
		caller,
	)
	ownedRecorder := httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(ownedRecorder, owned)
	if ownedRecorder.Code != http.StatusOK {
		t.Fatalf("owned status = %d; body=%s", ownedRecorder.Code, ownedRecorder.Body.String())
	}
	if probeCalls != 1 {
		t.Fatalf("readiness probe calls = %d, want 1", probeCalls)
	}
	var response mailClientSetupResponse
	if err := json.Unmarshal(ownedRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.WebmailAvailable || response.WebmailURL != "/webmail/" {
		t.Fatalf("webmail response = available:%v url:%q", response.WebmailAvailable, response.WebmailURL)
	}
	if response.MailHost != "mail.customer.matrix.test" {
		t.Fatalf("mail host = %q", response.MailHost)
	}
	if got := ownedRecorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestMailSetupFailClosedOmitsWebmailURL(t *testing.T) {
	panel := &Panel{webmailReadinessProbe: func(context.Context) bool { return false }}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/domains/1/mail/setup", nil)
	recorder := httptest.NewRecorder()
	panel.handleMailClientSetup(recorder, request, "example.test")

	var response mailClientSetupResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.WebmailAvailable || response.WebmailURL != "" {
		t.Fatalf("unready webmail response = available:%v url:%q", response.WebmailAvailable, response.WebmailURL)
	}
	if response.IMAP.Port != 993 || response.POP3.Port != 995 || response.SMTP.Port != 587 {
		t.Fatalf("mail client settings changed: %+v", response)
	}
}
