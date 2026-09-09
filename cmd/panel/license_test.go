package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/licensing"
)

func TestLicenseProvisioningRoutesAndMaintenance(t *testing.T) {
	blocked := []string{"/api/v1/domains/create", "/api/v1/users", "/api/v1/team-members", "/api/v1/subscriptions", "/api/v1/database-servers",
		"/api/v1/database-servers/1/databases", "/api/v1/database-servers/1/users", "/api/v1/domains/1/databases", "/api/v1/domains/1/aliases",
		"/api/v1/domains/1/mail/accounts", "/api/v1/domains/1/mail/forwardings", "/api/v1/domains/1/apps/install", "/api/v1/import/cpanel/apply", "/api/v1/vpn/peers"}
	for _, path := range blocked {
		if !licenseProvisioningRequest(httptest.NewRequest("POST", path, nil)) {
			t.Errorf("ungated creation: %s", path)
		}
		for _, method := range []string{"GET", "DELETE", "OPTIONS"} {
			if licenseProvisioningRequest(httptest.NewRequest(method, path, nil)) {
				t.Errorf("maintenance blocked: %s %s", method, path)
			}
		}
	}
	allowed := []string{"/api/v1/auth/login", "/api/v1/auth/password", panelLicensePath, "/api/v1/panel/certificate", "/api/v1/firewall",
		"/api/v1/domains/1/backups", "/api/v1/domains/1/backups/restore", "/api/v1/domains/1/ssl/letsencrypt", "/api/v1/domains/1/dns/records", "/api/v1/service/install"}
	for _, path := range allowed {
		if licenseProvisioningRequest(httptest.NewRequest("POST", path, nil)) {
			t.Errorf("maintenance blocked: %s", path)
		}
	}
}
func TestLicenseMissingDeniesProvisioningWithoutAgentMutation(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	m, err := licensing.New(filepath.Join(t.TempDir(), "license.json"), pub, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	p := &Panel{license: m}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/domains/create", nil)
	if p.allowLicensedProvisioning(w, r) || w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !p.allowLicensedProvisioning(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/domains/1/backups", nil)) {
		t.Fatal("backup blocked")
	}
}
func TestLicenseEndpointRequiresServerAdministrator(t *testing.T) {
	if !isAdminOnlyPath(panelLicensePath) {
		t.Fatal("license endpoint not admin only")
	}
	for _, role := range []string{"customer", "reseller", ""} {
		request := httptest.NewRequest(http.MethodPost, panelLicensePath, strings.NewReader(`{"action":"refresh"}`))
		request = request.WithContext(context.WithValue(request.Context(), callerKey, &Caller{ID: 1, Role: role}))
		w := httptest.NewRecorder()
		(&Panel{}).handleLicense(w, request)
		if w.Code != 403 {
			t.Fatal(role, w.Code)
		}
	}
}
