package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteDNSMiddlewareAuthenticatesBeforeCSRFAndSessionException(t *testing.T) {
	f := newAuthzMatrixFixture(t)
	id, credential := strings.Repeat("a", 32), strings.Repeat("b", 64)
	if _, err := f.panel.db.GetDB().Exec(`INSERT INTO remote_dns_clients(id,credential_hash,label,created_at) VALUES(?,?,?,?)`, id, remoteDNSHash(credential), "fixture", "now"); err != nil {
		t.Fatal(err)
	}
	const statusPath = "/api/v1/dns/remote/receiver/status"
	for _, test := range []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"verified machine", func(*http.Request) {}, http.StatusNoContent},
		{"TLS and no headers are insufficient", func(r *http.Request) { r.Header.Del("Authorization") }, http.StatusUnauthorized},
		{"wrong credential", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+id+"."+strings.Repeat("c", 64)) }, http.StatusUnauthorized},
		{"plain HTTP", func(r *http.Request) { r.TLS = nil }, http.StatusUnauthorized},
		{"browser origin", func(r *http.Request) { r.Header.Set("Origin", "https://panel.example") }, http.StatusUnauthorized},
		{"empty origin", func(r *http.Request) { r.Header.Set("Origin", "") }, http.StatusUnauthorized},
		{"referer", func(r *http.Request) { r.Header.Set("Referer", "https://panel.example/") }, http.StatusUnauthorized},
		{"cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: f.tokens[authzMatrixAdminID]})
		}, http.StatusUnauthorized},
		{"fetch mode", func(r *http.Request) { r.Header.Set("Sec-Fetch-Mode", "cors") }, http.StatusUnauthorized},
		{"fetch destination", func(r *http.Request) { r.Header.Set("Sec-Fetch-Dest", "empty") }, http.StatusUnauthorized},
		{"fetch user", func(r *http.Request) { r.Header.Set("Sec-Fetch-User", "?1") }, http.StatusUnauthorized},
		{"two authorizations", func(r *http.Request) { r.Header.Add("Authorization", "Bearer extra") }, http.StatusUnauthorized},
		{"query", func(r *http.Request) { r.URL.RawQuery = "scope=admin" }, http.StatusUnauthorized},
		{"empty query", func(r *http.Request) { r.URL.ForceQuery = true }, http.StatusUnauthorized},
		{"encoded path alias", func(r *http.Request) { r.URL.RawPath = "/api/v1/dns/remote/receiver/%73tatus" }, http.StatusUnauthorized},
		{"method", func(r *http.Request) { r.Method = http.MethodGet }, http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			stack := f.panel.requireRemoteDNSMachineAuth(csrfProtect(f.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if !remoteDNSMachineVerified(r) || currentCaller(r) != nil {
					t.Error("machine credential became a browser/admin session")
				}
				w.WriteHeader(http.StatusNoContent)
			}))))
			r := httptest.NewRequest(http.MethodPost, "https://panel.example"+statusPath, strings.NewReader(`{}`))
			r.Header.Set("Authorization", "Bearer "+id+"."+credential)
			test.change(r)
			w := httptest.NewRecorder()
			stack.ServeHTTP(w, r)
			if w.Code != test.status || called != (test.status == http.StatusNoContent) {
				t.Fatalf("status=%d called=%v", w.Code, called)
			}
			if strings.Contains(w.Body.String(), credential) {
				t.Fatal("credential disclosed in error")
			}
		})
	}
	for _, licensed := range []bool{false, true} {
		if licensed {
			f.panel.license = testPanelLicense(t, "active")
			_, _ = f.panel.db.GetDB().Exec(`UPDATE remote_dns_clients SET revoked=1 WHERE id=?`, id)
		} else {
			f.panel.license = testPanelLicense(t, "expired")
		}
		called := false
		stack := f.panel.requireRemoteDNSMachineAuth(csrfProtect(f.panel.requireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))))
		r := httptest.NewRequest(http.MethodPost, "https://panel.example"+statusPath, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+id+"."+credential)
		w := httptest.NewRecorder()
		stack.ServeHTTP(w, r)
		if called || (licensed && w.Code != http.StatusUnauthorized) || (!licensed && w.Code != http.StatusForbidden) {
			t.Fatalf("license/revocation was bypassed: %d", w.Code)
		}
	}
}

func TestRemoteDNSMiddlewareEnrollmentRequiresRealUnexpiredCode(t *testing.T) {
	f := newAuthzMatrixFixture(t)
	code := strings.Repeat("d", 64)
	if _, err := f.panel.db.GetDB().Exec(`INSERT INTO remote_dns_enrollments(code_hash,expires_at) VALUES(?,?)`, remoteDNSHash(code), time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	for _, valid := range []bool{true, false} {
		if !valid {
			_, _ = f.panel.db.GetDB().Exec(`UPDATE remote_dns_enrollments SET expires_at=0`)
		}
		called := false
		stack := f.panel.requireRemoteDNSMachineAuth(csrfProtect(f.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))))
		body := `{"client_id":"` + strings.Repeat("e", 32) + `","credential":"` + strings.Repeat("f", 64) + `","label":"fixture"}`
		r := httptest.NewRequest(http.MethodPost, "https://panel.example/api/v1/dns/remote/accept", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+code)
		w := httptest.NewRecorder()
		stack.ServeHTTP(w, r)
		if called != valid || (!valid && w.Code != http.StatusUnauthorized) {
			t.Fatalf("enrollment boundary: %d called=%v", w.Code, called)
		}
	}
}

func TestRemoteDNSMachineCredentialCannotAuthorizeAdministrativePaths(t *testing.T) {
	f := newAuthzMatrixFixture(t)
	for _, path := range []string{"/api/v1/dns/remote/enrollments", "/api/v1/dns/remote/connections", "/api/v1/dns/remote/clients", panelUpdateStartPath, "/api/v1/config"} {
		t.Run(path, func(t *testing.T) {
			called := false
			stack := f.panel.requireRemoteDNSMachineAuth(csrfProtect(f.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))))
			r := httptest.NewRequest(http.MethodPost, "https://panel.example"+path, strings.NewReader(`{}`))
			r.TLS = &tls.ConnectionState{}
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32)+"."+strings.Repeat("b", 64))
			if isPublicPath(r) {
				t.Fatal("machine sibling became public")
			}
			w := httptest.NewRecorder()
			stack.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || called {
				t.Fatal("missing CSRF proof accepted on administrative path")
			}
			r.Header.Set("Origin", "https://panel.example")
			w = httptest.NewRecorder()
			stack.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized || called {
				t.Fatal("bearer credential substituted for an admin session")
			}
		})
	}
	for _, user := range []int{authzMatrixAdminID, authzMatrixResellerID, authzMatrixCustomerID} {
		called := false
		stack := f.panel.requireRemoteDNSMachineAuth(csrfProtect(f.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))))
		r := httptest.NewRequest(http.MethodPost, "https://panel.example/api/v1/dns/remote/enrollments", strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://panel.example")
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: f.tokens[user]})
		w := httptest.NewRecorder()
		stack.ServeHTTP(w, r)
		if called != (user == authzMatrixAdminID) || (user != authzMatrixAdminID && w.Code != http.StatusForbidden) {
			t.Fatalf("admin scope failed for user %d: %d", user, w.Code)
		}
	}
}
