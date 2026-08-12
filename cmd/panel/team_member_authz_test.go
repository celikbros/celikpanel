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

func domainListObjectsForCaller(t *testing.T, panel *Panel, caller *Caller) []map[string]json.RawMessage {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := teamMemberAuthzTestRequest(http.MethodGet, "/api/v1/domains", caller)
	panel.handleDomains(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var domains []map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &domains); err != nil {
		t.Fatalf("decode domain list: %v", err)
	}
	return domains
}

func domainListObjectByID(t *testing.T, domains []map[string]json.RawMessage, domainID int) map[string]json.RawMessage {
	t.Helper()
	for _, domain := range domains {
		var id int
		if err := json.Unmarshal(domain["id"], &id); err != nil {
			t.Fatalf("decode domain id: %v", err)
		}
		if id == domainID {
			return domain
		}
	}
	t.Fatalf("domain %d missing from response %#v", domainID, domains)
	return nil
}

func requireRawDomainField(t *testing.T, domain map[string]json.RawMessage, key, want string) {
	t.Helper()
	raw, ok := domain[key]
	if !ok {
		t.Fatalf("field %q is missing", key)
	}
	if got := string(raw); got != want {
		t.Fatalf("field %q = %s, want %s", key, got, want)
	}
}

func requireOmittedDomainFields(t *testing.T, domain map[string]json.RawMessage, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if raw, ok := domain[key]; ok {
			t.Fatalf("field %q leaked as %s", key, raw)
		}
	}
}

func seedDomainListMetadata(t *testing.T, fixture *authzMatrixFixture) {
	t.Helper()
	if _, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES (?, ?, 'parent.matrix.test', 'active')`,
		teamMemberAuthzSecondCustomerDomainID, authzMatrixCustomerSubID,
	); err != nil {
		t.Fatalf("seed parent domain: %v", err)
	}
	if _, err := fixture.panel.db.GetDB().Exec(`
		UPDATE domains SET parent_domain_id = ? WHERE id = ?`,
		teamMemberAuthzSecondCustomerDomainID, authzMatrixCustomerDomainID,
	); err != nil {
		t.Fatalf("seed child parent: %v", err)
	}
	if _, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO sites (
			domain_id, document_root, php_version, ssl_enabled, project_type,
			disk_usage_bytes, traffic_month_bytes
		) VALUES (?, '/srv/customer.matrix.test', '9.9', 1, 'node', 123456789, 987654321)`,
		authzMatrixCustomerDomainID,
	); err != nil {
		t.Fatalf("seed site metadata: %v", err)
	}
	var phpVersion, projectType string
	var sslEnabled bool
	var diskUsage, bandwidth int64
	if err := fixture.panel.db.GetDB().QueryRow(`
		SELECT php_version, ssl_enabled, project_type,
		       disk_usage_bytes, traffic_month_bytes
		FROM sites WHERE domain_id = ?`, authzMatrixCustomerDomainID).Scan(
		&phpVersion, &sslEnabled, &projectType, &diskUsage, &bandwidth,
	); err != nil {
		t.Fatalf("verify domain-list metadata fixture: %v", err)
	}
	if phpVersion != "9.9" || !sslEnabled || projectType != "node" ||
		diskUsage != 123456789 || bandwidth != 987654321 {
		t.Fatalf("unexpected domain-list metadata fixture: php=%q ssl=%t project=%q disk=%d bandwidth=%d",
			phpVersion, sslEnabled, projectType, diskUsage, bandwidth)
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
		{name: "mail password manage", method: http.MethodPut, path: "/api/v1/domains/9902/mail/accounts/password", capability: core.TeamCapabilityMail, mode: core.TeamPermissionManage},
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

func TestTeamMemberMailPasswordRotationRequiresManage(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	caller := teamMemberAuthzTestCaller()
	grantTeamMemberDomainCapability(
		t, &fixture, authzMatrixCustomerDomainID,
		core.TeamCapabilityMail, core.TeamPermissionView,
	)

	request := teamMemberAuthzTestRequest(
		http.MethodPut,
		"/api/v1/domains/9902/mail/accounts/password",
		caller,
	)
	match, ok := matchDomainSubroute(request)
	if !ok || match.kind != "mail" {
		t.Fatalf("password route did not resolve to mail: %#v ok=%v", match, ok)
	}
	recorder := httptest.NewRecorder()
	if fixture.panel.authorizeDomainSubroute(recorder, request, match.domainID, match.kind) {
		t.Fatal("mail view grant authorized password rotation")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("view status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	if _, err := fixture.panel.db.GetDB().Exec(`
		UPDATE additional_user_domain_permissions
		SET mode = 'manage'
		WHERE user_id = ? AND domain_id = ? AND capability = 'mail'`,
		authzMatrixAdditionalUserID, authzMatrixCustomerDomainID,
	); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	if !fixture.panel.authorizeDomainSubroute(recorder, request, match.domainID, match.kind) {
		t.Fatalf("mail manage grant rejected password rotation: status=%d body=%s",
			recorder.Code, recorder.Body.String())
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

func TestHandleDomainsAdditionalUserRedactsSiteMetadataByCapability(t *testing.T) {
	tests := []struct {
		name       string
		capability core.TeamCapability
		visible    map[string]string
		omitted    []string
	}{
		{
			name:       "dns only",
			capability: core.TeamCapabilityDNS,
			omitted:    []string{"php_version", "ssl_enabled", "project_type", "disk_usage", "bandwidth"},
		},
		{
			name:       "mail only",
			capability: core.TeamCapabilityMail,
			omitted:    []string{"php_version", "ssl_enabled", "project_type", "disk_usage", "bandwidth"},
		},
		{
			name:       "database only",
			capability: core.TeamCapabilityDatabases,
			omitted:    []string{"php_version", "ssl_enabled", "project_type", "disk_usage", "bandwidth"},
		},
		{
			name:       "php only",
			capability: core.TeamCapabilityPHP,
			visible:    map[string]string{"php_version": `"9.9"`, "project_type": `"node"`},
			omitted:    []string{"ssl_enabled", "disk_usage", "bandwidth"},
		},
		{
			name:       "ssl only",
			capability: core.TeamCapabilitySSL,
			visible:    map[string]string{"ssl_enabled": "true"},
			omitted:    []string{"php_version", "project_type", "disk_usage", "bandwidth"},
		},
		{
			name:       "statistics only",
			capability: core.TeamCapabilityStatistics,
			visible: map[string]string{
				"project_type": `"node"`,
				"disk_usage":   "123456789",
				"bandwidth":    "987654321",
			},
			omitted: []string{"php_version", "ssl_enabled"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAuthzMatrixFixture(t)
			seedAdditionalUserSession(t, &fixture)
			seedDomainListMetadata(t, &fixture)
			grantTeamMemberDomainCapability(
				t, &fixture, authzMatrixCustomerDomainID,
				test.capability, core.TeamPermissionView,
			)

			domains := domainListObjectsForCaller(t, fixture.panel, teamMemberAuthzTestCaller())
			if len(domains) != 1 {
				t.Fatalf("domain count = %d, want 1", len(domains))
			}
			domain := domainListObjectByID(t, domains, authzMatrixCustomerDomainID)
			for _, key := range []string{"id", "domain_name", "status", "created_at", "access"} {
				if _, ok := domain[key]; !ok {
					t.Fatalf("basic field %q is missing", key)
				}
			}
			for key, want := range test.visible {
				requireRawDomainField(t, domain, key, want)
			}
			requireOmittedDomainFields(t, domain, test.omitted...)
			requireOmittedDomainFields(t, domain, "parent_id")

			var access map[string]string
			if err := json.Unmarshal(domain["access"], &access); err != nil {
				t.Fatalf("decode access: %v", err)
			}
			if got := access[string(test.capability)]; got != string(core.TeamPermissionView) {
				t.Fatalf("access[%s] = %q, want view", test.capability, got)
			}
		})
	}
}

func TestHandleDomainsParentVisibilityAndLegacyMetadataContract(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	seedDomainListMetadata(t, &fixture)
	grantTeamMemberDomainCapability(
		t, &fixture, authzMatrixCustomerDomainID,
		core.TeamCapabilityFiles, core.TeamPermissionView,
	)

	domains := domainListObjectsForCaller(t, fixture.panel, teamMemberAuthzTestCaller())
	child := domainListObjectByID(t, domains, authzMatrixCustomerDomainID)
	requireRawDomainField(t, child, "project_type", `"node"`)
	requireOmittedDomainFields(
		t, child, "php_version", "ssl_enabled", "disk_usage", "bandwidth", "parent_id",
	)

	grantTeamMemberDomainCapability(
		t, &fixture, teamMemberAuthzSecondCustomerDomainID,
		core.TeamCapabilityDNS, core.TeamPermissionView,
	)
	domains = domainListObjectsForCaller(t, fixture.panel, teamMemberAuthzTestCaller())
	child = domainListObjectByID(t, domains, authzMatrixCustomerDomainID)
	requireRawDomainField(t, child, "parent_id", "9904")

	for _, account := range []struct {
		name   string
		caller *Caller
	}{
		{
			name: "customer account",
			caller: &Caller{
				ID:          authzMatrixCustomerID,
				Role:        roleCustomer,
				AccountType: core.AccountTypeAccount,
				CustomerID:  authzMatrixCustomerID,
			},
		},
		{
			name: "administrator account",
			caller: &Caller{
				ID:          authzMatrixAdminID,
				Role:        roleAdmin,
				AccountType: core.AccountTypeAccount,
			},
		},
	} {
		t.Run(account.name, func(t *testing.T) {
			legacyDomains := domainListObjectsForCaller(t, fixture.panel, account.caller)
			legacyChild := domainListObjectByID(t, legacyDomains, authzMatrixCustomerDomainID)
			for key, want := range map[string]string{
				"php_version":  `"9.9"`,
				"ssl_enabled":  "true",
				"project_type": `"node"`,
				"disk_usage":   "123456789",
				"bandwidth":    "987654321",
				"parent_id":    "9904",
			} {
				requireRawDomainField(t, legacyChild, key, want)
			}
			requireOmittedDomainFields(t, legacyChild, "access")

			legacyParent := domainListObjectByID(t, legacyDomains, teamMemberAuthzSecondCustomerDomainID)
			for key, want := range map[string]string{
				"php_version":  `"8.3"`,
				"ssl_enabled":  "false",
				"project_type": `"php"`,
				"disk_usage":   "0",
				"bandwidth":    "0",
			} {
				requireRawDomainField(t, legacyParent, key, want)
			}
			requireOmittedDomainFields(t, legacyParent, "access", "parent_id")
		})
	}
}
