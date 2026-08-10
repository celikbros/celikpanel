package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

const teamMemberAuthzSecondCustomerDomainID = 9904

func teamMemberAuthzTestCaller() *Caller {
	return &Caller{
		ID:          authzMatrixAdditionalUserID,
		Role:        core.EffectiveRoleAdditionalUser,
		AccountType: core.AccountTypeAdditionalUser,
		CustomerID:  authzMatrixCustomerID,
	}
}

func teamMemberAuthzTestRequest(method, path string, caller *Caller) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	return req.WithContext(context.WithValue(req.Context(), callerKey, caller))
}

func grantTeamMemberDomainCapability(
	t *testing.T,
	fixture *authzMatrixFixture,
	domainID int,
	capability core.TeamCapability,
	mode core.TeamPermissionMode,
) {
	t.Helper()
	if _, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO additional_user_domain_permissions
			(user_id, domain_id, capability, mode)
		VALUES (?, ?, ?, ?)`,
		authzMatrixAdditionalUserID, domainID, capability, mode,
	); err != nil {
		t.Fatalf("grant domain capability: %v", err)
	}
}

func grantTeamMemberSubscriptionCapability(
	t *testing.T,
	fixture *authzMatrixFixture,
	capability core.TeamCapability,
	mode core.TeamPermissionMode,
) {
	t.Helper()
	if _, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO additional_user_subscription_permissions
			(user_id, subscription_id, capability, mode)
		VALUES (?, ?, ?, ?)`,
		authzMatrixAdditionalUserID, authzMatrixCustomerSubID, capability, mode,
	); err != nil {
		t.Fatalf("grant subscription capability: %v", err)
	}
}

func TestTeamMemberDomainRouteRequirementsFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		capability core.TeamCapability
		mode       core.TeamPermissionMode
	}{
		{name: "files view", method: http.MethodGet, path: "/api/v1/domains/9902/files", capability: core.TeamCapabilityFiles, mode: core.TeamPermissionView},
		{name: "files manage", method: http.MethodPost, path: "/api/v1/domains/9902/files", capability: core.TeamCapabilityFiles, mode: core.TeamPermissionManage},
		{name: "databases view", method: http.MethodGet, path: "/api/v1/domains/9902/databases", capability: core.TeamCapabilityDatabases, mode: core.TeamPermissionView},
		{name: "databases manage", method: http.MethodPost, path: "/api/v1/domains/9902/databases", capability: core.TeamCapabilityDatabases, mode: core.TeamPermissionManage},
		{name: "mail view", method: http.MethodGet, path: "/api/v1/domains/9902/mail/accounts", capability: core.TeamCapabilityMail, mode: core.TeamPermissionView},
		{name: "mail manage", method: http.MethodPost, path: "/api/v1/domains/9902/mail/accounts", capability: core.TeamCapabilityMail, mode: core.TeamPermissionManage},
		{name: "dns view", method: http.MethodGet, path: "/api/v1/domains/9902/dns/records", capability: core.TeamCapabilityDNS, mode: core.TeamPermissionView},
		{name: "dns manage", method: http.MethodPost, path: "/api/v1/domains/9902/dns/records", capability: core.TeamCapabilityDNS, mode: core.TeamPermissionManage},
		{name: "ssl view", method: http.MethodGet, path: "/api/v1/domains/9902/ssl", capability: core.TeamCapabilitySSL, mode: core.TeamPermissionView},
		{name: "ssl manage", method: http.MethodDelete, path: "/api/v1/domains/9902/ssl", capability: core.TeamCapabilitySSL, mode: core.TeamPermissionManage},
		{name: "cron view", method: http.MethodGet, path: "/api/v1/domains/9902/cron", capability: core.TeamCapabilityCron, mode: core.TeamPermissionView},
		{name: "cron manage", method: http.MethodPost, path: "/api/v1/domains/9902/cron", capability: core.TeamCapabilityCron, mode: core.TeamPermissionManage},
		{name: "backups view", method: http.MethodGet, path: "/api/v1/domains/9902/backups", capability: core.TeamCapabilityBackups, mode: core.TeamPermissionView},
		{name: "backups manage", method: http.MethodPost, path: "/api/v1/domains/9902/backups", capability: core.TeamCapabilityBackups, mode: core.TeamPermissionManage},
		{name: "php view", method: http.MethodGet, path: "/api/v1/domains/9902/php", capability: core.TeamCapabilityPHP, mode: core.TeamPermissionView},
		{name: "php manage", method: http.MethodPost, path: "/api/v1/domains/9902/php", capability: core.TeamCapabilityPHP, mode: core.TeamPermissionManage},
		{name: "statistics view", method: http.MethodGet, path: "/api/v1/domains/9902/usage", capability: core.TeamCapabilityStatistics, mode: core.TeamPermissionView},
		{name: "statistics manage", method: http.MethodDelete, path: "/api/v1/domains/9902/logs/access", capability: core.TeamCapabilityStatistics, mode: core.TeamPermissionManage},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, nil)
			match, ok := matchDomainSubroute(req)
			if !ok {
				t.Fatalf("route did not match: %s %s", test.method, test.path)
			}
			requirement, ok := teamMemberDomainRequirementFor(match.kind, test.method)
			if !ok {
				t.Fatalf("canonical route kind %q is unmapped", match.kind)
			}
			if requirement.capability != test.capability || requirement.mode != test.mode {
				t.Fatalf("requirement = %#v, want %s/%s", requirement, test.capability, test.mode)
			}
		})
	}

	for _, key := range []teamMemberRouteKey{
		{kind: "hosting", method: http.MethodGet},
		{kind: "unknown", method: http.MethodGet},
		{kind: "files", method: http.MethodDelete},
	} {
		if requirement, ok := teamMemberDomainRequirementFor(key.kind, key.method); ok {
			t.Fatalf("unexpected mapping for %#v: %#v", key, requirement)
		}
	}
	if !teamMemberModeAllows(core.TeamPermissionManage, core.TeamPermissionView) {
		t.Fatal("manage must imply view")
	}
	if teamMemberModeAllows(core.TeamPermissionView, core.TeamPermissionManage) {
		t.Fatal("view must never imply mutation")
	}
}

func TestTeamMemberCapabilityAuthorizationIsGrantOnly(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	caller := teamMemberAuthzTestCaller()
	grantTeamMemberDomainCapability(t, &fixture, authzMatrixCustomerDomainID, core.TeamCapabilityFiles, core.TeamPermissionView)

	tests := []struct {
		name     string
		domainID int
		kind     string
		method   string
		want     error
	}{
		{name: "view grant permits read", domainID: authzMatrixCustomerDomainID, kind: "files", method: http.MethodGet},
		{name: "view grant rejects mutation", domainID: authzMatrixCustomerDomainID, kind: "files", method: http.MethodPost, want: errTeamMemberCapabilityDenied},
		{name: "different capability is forbidden", domainID: authzMatrixCustomerDomainID, kind: "dns", method: http.MethodGet, want: errTeamMemberCapabilityDenied},
		{name: "unmapped owner route is forbidden", domainID: authzMatrixCustomerDomainID, kind: "hosting", method: http.MethodGet, want: errTeamMemberCapabilityDenied},
		{name: "foreign domain is hidden", domainID: authzMatrixOutsiderDomainID, kind: "files", method: http.MethodGet, want: errNotFound},
		{name: "missing domain is hidden", domainID: 999999, kind: "files", method: http.MethodGet, want: errNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := fixture.panel.canTeamMemberAccessDomainSubroute(context.Background(), caller, test.domainID, test.kind, test.method)
			if test.want == nil && err != nil {
				t.Fatalf("access error = %v, want nil", err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("access error = %v, want %v", err, test.want)
			}
		})
	}

	if _, err := fixture.panel.db.GetDB().Exec(`
		UPDATE additional_user_domain_permissions
		SET mode = 'manage'
		WHERE user_id = ? AND domain_id = ? AND capability = 'files'`,
		authzMatrixAdditionalUserID, authzMatrixCustomerDomainID,
	); err != nil {
		t.Fatalf("promote direct grant: %v", err)
	}
	if err := fixture.panel.canTeamMemberAccessDomainSubroute(context.Background(), caller, authzMatrixCustomerDomainID, "files", http.MethodPost); err != nil {
		t.Fatalf("manage grant rejected mutation: %v", err)
	}

	grantTeamMemberSubscriptionCapability(t, &fixture, core.TeamCapabilityDNS, core.TeamPermissionManage)
	grantTeamMemberDomainCapability(t, &fixture, authzMatrixCustomerDomainID, core.TeamCapabilityDNS, core.TeamPermissionView)
	capabilities, err := fixture.panel.teamMemberEffectiveDomainCapabilities(context.Background(), caller, authzMatrixCustomerDomainID)
	if err != nil {
		t.Fatalf("effective capabilities: %v", err)
	}
	if got := capabilities[core.TeamCapabilityDNS]; got != core.TeamPermissionManage {
		t.Fatalf("effective DNS mode = %q, want manage", got)
	}
}

func TestAuthorizeDomainSubrouteStatusContract(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	caller := teamMemberAuthzTestCaller()
	grantTeamMemberDomainCapability(t, &fixture, authzMatrixCustomerDomainID, core.TeamCapabilityFiles, core.TeamPermissionView)

	for _, test := range []struct {
		name     string
		domainID int
		kind     string
		method   string
		want     int
	}{
		{name: "granted read passes", domainID: authzMatrixCustomerDomainID, kind: "files", method: http.MethodGet, want: http.StatusOK},
		{name: "in-scope capability denial is forbidden", domainID: authzMatrixCustomerDomainID, kind: "dns", method: http.MethodGet, want: http.StatusForbidden},
		{name: "unmapped in-scope route is forbidden", domainID: authzMatrixCustomerDomainID, kind: "hosting", method: http.MethodGet, want: http.StatusForbidden},
		{name: "foreign domain is not found", domainID: authzMatrixOutsiderDomainID, kind: "files", method: http.MethodGet, want: http.StatusNotFound},
		{name: "missing domain is not found", domainID: 999999, kind: "files", method: http.MethodGet, want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := teamMemberAuthzTestRequest(test.method, fmt.Sprintf("/api/v1/domains/%d/placeholder", test.domainID), caller)
			allowed := fixture.panel.authorizeDomainSubroute(recorder, req, test.domainID, test.kind)
			if allowed != (test.want == http.StatusOK) {
				t.Fatalf("allowed = %v, want status %d", allowed, test.want)
			}
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}

	legacyCustomer := &Caller{ID: authzMatrixCustomerID, Role: roleCustomer, AccountType: core.AccountTypeAccount, CustomerID: authzMatrixCustomerID}
	recorder := httptest.NewRecorder()
	req := teamMemberAuthzTestRequest(http.MethodGet, "/api/v1/domains/9902/hosting", legacyCustomer)
	if !fixture.panel.authorizeDomainSubroute(recorder, req, authzMatrixCustomerDomainID, "hosting") {
		t.Fatalf("legacy customer behavior changed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleDomainsAdditionalUserListsOnlyGrantedDomainsWithAccess(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	if _, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES (?, ?, 'ungranted.matrix.test', 'active')`,
		teamMemberAuthzSecondCustomerDomainID, authzMatrixCustomerSubID,
	); err != nil {
		t.Fatalf("seed second customer domain: %v", err)
	}
	grantTeamMemberDomainCapability(t, &fixture, authzMatrixCustomerDomainID, core.TeamCapabilityFiles, core.TeamPermissionView)

	recorder := httptest.NewRecorder()
	req := teamMemberAuthzTestRequest(http.MethodGet, "/api/v1/domains", teamMemberAuthzTestCaller())
	fixture.panel.handleDomains(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var domains []DomainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &domains); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(domains) != 1 || domains[0].ID != authzMatrixCustomerDomainID {
		t.Fatalf("domains = %#v, want only granted domain %d", domains, authzMatrixCustomerDomainID)
	}
	if len(domains[0].Access) != len(teamMemberCapabilities) {
		t.Fatalf("access = %#v, want all %d capabilities", domains[0].Access, len(teamMemberCapabilities))
	}
	for _, capability := range teamMemberCapabilities {
		want := "none"
		if capability == core.TeamCapabilityFiles {
			want = string(core.TeamPermissionView)
		}
		if got := domains[0].Access[string(capability)]; got != want {
			t.Errorf("access[%s] = %q, want %q", capability, got, want)
		}
	}

	legacyRecorder := httptest.NewRecorder()
	legacyCustomer := &Caller{ID: authzMatrixCustomerID, Role: roleCustomer, AccountType: core.AccountTypeAccount, CustomerID: authzMatrixCustomerID}
	legacyReq := teamMemberAuthzTestRequest(http.MethodGet, "/api/v1/domains", legacyCustomer)
	fixture.panel.handleDomains(legacyRecorder, legacyReq)
	if legacyRecorder.Code != http.StatusOK {
		t.Fatalf("legacy status = %d, want 200; body=%s", legacyRecorder.Code, legacyRecorder.Body.String())
	}
	var legacyDomains []DomainResponse
	if err := json.Unmarshal(legacyRecorder.Body.Bytes(), &legacyDomains); err != nil {
		t.Fatalf("decode legacy response: %v", err)
	}
	if len(legacyDomains) != 2 {
		t.Fatalf("legacy domain count = %d, want 2", len(legacyDomains))
	}
	for _, domain := range legacyDomains {
		if domain.Access != nil {
			t.Fatalf("legacy domain %d unexpectedly exposes access: %#v", domain.ID, domain.Access)
		}
	}
}
