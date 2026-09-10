package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/licensing"
)

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

// Real signed local receipts keep authorization fixtures independent of the network.
func testPanelLicense(t *testing.T, state string) *licensing.Manager {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "license.json")
	now := time.Now().Unix()
	c := licensing.Claims{Format: "celikpanel-license-v1", Product: "celikpanel", LicenseID: strings.Repeat("a", 32), ServerID: strings.Repeat("b", 64), ActivatedAt: now - 86400, IssuedAt: now - 10, ExpiresAt: now + 86400, RefreshAfter: now + 3600, OfflineUntil: now + 7200}
	if state == "expired" {
		c.ExpiresAt = now - 1
		c.OfflineUntil = now - 1
	}
	if state == "verification_unavailable" {
		c.OfflineUntil = now - 1
	}
	if state != "missing" {
		payload, _ := json.Marshal(c)
		e := licensing.Envelope{Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload)), ActivationToken: strings.Repeat("c", 64)}
		if state == "invalid" {
			e.Signature = "invalid"
		}
		data, _ := json.Marshal(e)
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	m, err := licensing.New(file, pub, c.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLicensePanelGateAcrossRolesStatesAndMethods(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	for _, state := range []string{"missing", "expired", "invalid", "verification_unavailable", "active", "unconfigured"} {
		t.Run(state, func(t *testing.T) {
			fixture.panel.license = nil
			if state != "unconfigured" {
				fixture.panel.license = testPanelLicense(t, state)
			}
			reached := 0
			handler := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++; w.WriteHeader(204) }))
			for _, id := range []int{authzMatrixAdminID, authzMatrixResellerID, authzMatrixCustomerID, authzMatrixAdditionalUserID} {
				for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"} {
					for _, path := range []string{"/api/v1/domains", "/api/v1/domains/9902/backups", "/api/v2/databases", "/dbtool/tool/", "/api/v1/future-management-route"} {
						before := reached
						w := requestWithToken(handler, method, path, fixture.tokens[id])
						if state != "active" && (w.Code != 403 || reached != before) {
							t.Fatalf("%s %s role %d: %d handler=%d", method, path, id, w.Code, reached-before)
						}
						if state == "active" && id == authzMatrixAdminID && w.Code != 204 {
							t.Fatalf("active blocked: %s %s %d", method, path, w.Code)
						}
					}
				}
			}
			for _, path := range []string{"/api/v1/firewall", "/api/v1/service/install", "/api/v1/panel/update", "/api/v1/config", "/api/v1/system/stats"} {
				before := reached
				w := requestWithToken(handler, "POST", path, fixture.tokens[authzMatrixAdminID])
				if state != "active" && (w.Code != 403 || reached != before || !strings.Contains(w.Body.String(), "license_required")) {
					t.Fatalf("management escaped: %s %d", path, w.Code)
				}
			}
			for _, id := range []int{authzMatrixAdminID, authzMatrixResellerID, authzMatrixCustomerID, authzMatrixAdditionalUserID} {
				for _, item := range []struct{ method, path string }{{"GET", "/api/v1/auth/me"}, {"GET", panelLicenseAccessPath}, {"POST", "/api/v1/auth/password"}, {"POST", "/api/v1/auth/logout"}} {
					w := requestWithToken(handler, item.method, item.path, fixture.tokens[id])
					if w.Code != 204 {
						t.Fatalf("recovery blocked role=%d %s: %d %s", id, item.path, w.Code, w.Body.String())
					}
				}
			}
			w := requestWithToken(handler, "POST", panelLicensePath, fixture.tokens[authzMatrixAdminID])
			if w.Code != 204 {
				t.Fatal("admin activation blocked", w.Code)
			}
			w = requestWithToken(handler, "POST", panelLicensePath, fixture.tokens[authzMatrixCustomerID])
			if w.Code != 403 {
				t.Fatal("tenant activation allowed", w.Code)
			}
			for _, path := range []string{panelLicensePath + "/anything", panelLicenseAccessPath + "/anything", "/api/v1/auth/me/anything", "/api/v1/auth/password/anything"} {
				if licenseRecoveryRequest(httptest.NewRequest("GET", path, nil)) {
					t.Fatal("prefix recovery bypass", path)
				}
			}
		})
	}
}

func TestLicenseAccessResponseIsMinimalAndTracksState(t *testing.T) {
	p := &Panel{}
	for _, state := range []string{"missing", "expired", "invalid", "verification_unavailable", "active"} {
		p.license = testPanelLicense(t, state)
		w := httptest.NewRecorder()
		p.handleLicenseAccess(w, httptest.NewRequest("GET", panelLicenseAccessPath, nil))
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result) != 2 || result["can_use_panel"] != (state == "active") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(state, result)
		}
	}
}

func TestLicenseUpdateExceptionIsExactAndAdministratorOnly(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	allowed := map[string]string{
		panelUpdateCheckPath: "GET", panelUpdateStatusPath: "GET",
		panelUpdateStartPath: "POST", panelUpdateAbandonPath: "POST",
		"/api/v1/panel/version": "GET", hostMutationReadinessPath: "GET",
	}
	for _, state := range []string{"missing", "expired", "invalid", "verification_unavailable", "unconfigured"} {
		fixture.panel.license = nil
		if state != "unconfigured" {
			fixture.panel.license = testPanelLicense(t, state)
		}
		reached := 0
		handler := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++; w.WriteHeader(204) }))
		for path, permitted := range allowed {
			for _, id := range []int{authzMatrixAdminID, authzMatrixResellerID, authzMatrixCustomerID, authzMatrixAdditionalUserID} {
				for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"} {
					before := reached
					w := requestWithToken(handler, method, path, fixture.tokens[id])
					want := id == authzMatrixAdminID && method == permitted
					if (reached != before) != want || (want && w.Code != 204) || (!want && w.Code != 403) {
						t.Fatalf("%s %s %s role=%d status=%d", state, method, path, id, w.Code)
					}
				}
			}
			before := reached
			w := requestWithToken(handler, permitted, path, "")
			if reached != before || w.Code != 401 {
				t.Fatalf("unauthenticated update: %s %d", path, w.Code)
			}
			for _, suffix := range []string{"/", "/anything"} {
				w = requestWithToken(handler, permitted, path+suffix, fixture.tokens[authzMatrixAdminID])
				if reached != before || w.Code != 403 {
					t.Fatalf("update prefix bypass: %s", path+suffix)
				}
			}
		}
		w := httptest.NewRecorder()
		fixture.panel.handleLicenseAccess(w, httptest.NewRequest("GET", panelLicenseAccessPath, nil))
		if !strings.Contains(w.Body.String(), `"can_use_panel":false`) {
			t.Fatal("update access unlocked license", w.Body.String())
		}
	}
}

func TestLicenseLockedSignedUpdatePreservesLicenseAndTargetChecks(t *testing.T) {
	withSystemUpdateBuild(t)
	for _, state := range []string{"missing", "expired"} {
		t.Run(state, func(t *testing.T) {
			fixture := newSystemUpdateTestFixture(t)
			fixture.panel.license = testPanelLicense(t, state)
			before := fixture.panel.license.Status()
			invoke := func(path, body string) *httptest.ResponseRecorder {
				r := systemUpdateRequest("POST", path, body, roleAdmin)
				w := httptest.NewRecorder()
				if !fixture.panel.allowLicensedPanel(w, r) {
					t.Fatal("signed update blocked", w.Body.String())
				}
				csrfProtect(http.HandlerFunc(fixture.panel.handlePanelUpdateStart)).ServeHTTP(w, r)
				return w
			}
			bad := strings.Replace(systemUpdateStartBody(updateTestTargetVersion), updateTestTargetSHA, strings.Repeat("f", 64), 1)
			if w := invoke(panelUpdateStartPath, bad); w.Code < 400 {
				t.Fatal("unverified target accepted", w.Body.String())
			}
			if fixture.agent.startCalls != 0 {
				t.Fatal("bad target reached start")
			}
			w := invoke(panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion))
			if w.Code != http.StatusAccepted {
				t.Fatalf("valid update: %d %s", w.Code, w.Body.String())
			}
			if fixture.agent.startCalls != 1 {
				t.Fatal("valid target not started once")
			}
			if after := fixture.panel.license.Status(); after != before {
				t.Fatal("update changed license", before, after)
			}
			w = httptest.NewRecorder()
			if fixture.panel.allowLicensedPanel(w, systemUpdateRequest("POST", "/api/v1/firewall", "{}", roleAdmin)) {
				t.Fatal("update unlocked management")
			}
		})
	}
}
