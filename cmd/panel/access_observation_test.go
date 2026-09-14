package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/repositories"
)

func TestAuthObservationUnavailablePreservesSessionAndDeniesRequest(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent=%v", parent), func(t *testing.T) {
			f := newAuthzMatrixFixture(t)
			id, lookupID := authzMatrixAdminID, authzMatrixAdminID
			if parent {
				seedAdditionalUserSession(t, &f)
				id, lookupID = authzMatrixAdditionalUserID, authzMatrixCustomerID
			}
			original := f.panel.users
			override := &authzUserRepositoryOverride{UserRepository: original, errs: map[int]error{lookupID: errors.New("SQL secret-token private-account")}}
			f.panel.users = override
			calls := 0
			handler := f.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
			for _, method := range []string{"GET", "POST"} {
				w := requestWithToken(handler, method, "/api/v1/domains", f.tokens[id])
				if w.Code != 503 || !strings.Contains(w.Body.String(), errCodeAuthStatusUnavailable) || calls != 0 || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("unknown auth admitted or mislabeled: %d %s", w.Code, w.Body.String())
				}
				for _, secret := range []string{"SQL", "secret-token", "private-account"} {
					if strings.Contains(w.Body.String(), secret) {
						t.Fatal("internal error leaked")
					}
				}
			}
			if got, err := f.panel.sessions.Validate(context.Background(), f.tokens[id]); err != nil || got != id {
				t.Fatal("unknown lookup revoked the session", got, err)
			}
			override.errs[lookupID] = fmt.Errorf("wrapped: %w", repositories.ErrUserNotFound)
			if w := requestWithToken(handler, "GET", "/api/v1/domains", f.tokens[id]); w.Code != 401 || !strings.Contains(w.Body.String(), errCodeAuthRequired) {
				t.Fatalf("known absence changed: %d %s", w.Code, w.Body.String())
			}
			f.panel.users = original
			if w := requestWithToken(handler, "GET", "/api/v1/domains", f.tokens[id]); w.Code != 204 || calls != 1 {
				t.Fatalf("same session did not recover: %d", w.Code)
			}
		})
	}
}

func TestAuthObservationSessionDatabaseFailureIsNotInvalidSession(t *testing.T) {
	f := newAuthzMatrixFixture(t)
	if w := requestWithToken(f.handler, "GET", "/api/v1/auth/me", "invalid"); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if err := f.panel.db.GetDB().Close(); err != nil {
		t.Fatal(err)
	}
	w := requestWithToken(f.handler, "GET", "/api/v1/auth/me", f.tokens[authzMatrixAdminID])
	if w.Code != 503 || !strings.Contains(w.Body.String(), errCodeAuthStatusUnavailable) || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("DB error mislabeled: %d %s", w.Code, w.Body.String())
	}
}

func TestLicenseObservationUnavailableIsMinimalAndRecoveryIsExact(t *testing.T) {
	f := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &f)
	f.panel.license = nil
	handler := f.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == panelLicenseAccessPath {
			f.panel.handleLicenseAccess(w, r)
			return
		}
		w.WriteHeader(204)
	}))
	for _, id := range []int{authzMatrixAdminID, authzMatrixCustomerID, authzMatrixResellerID, authzMatrixAdditionalUserID} {
		w := requestWithToken(handler, "GET", panelLicenseAccessPath, f.tokens[id])
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(body) != 4 || body["can_use_panel"] != false || body["valid_until"] != float64(0) || body["state"] != "status_unavailable" || body["observation"] != "unavailable" {
			t.Fatalf("unexpected minimal state: %d %v", w.Code, body)
		}
		for _, path := range []string{panelAvailabilityPath, panelRecoveryStatusPath} {
			w = requestWithToken(handler, "GET", path, f.tokens[id])
			want := 204
			if path == panelRecoveryStatusPath && id != authzMatrixAdminID {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("recovery scope role=%d path=%s: %d", id, path, w.Code)
			}
			for _, method := range []string{"POST", "PUT", "DELETE"} {
				if w := requestWithToken(handler, method, path, f.tokens[id]); w.Code < 400 {
					t.Fatal("recovery opened mutation", method, path)
				}
			}
			if w := requestWithToken(handler, "GET", path+"/anything", f.tokens[id]); w.Code < 400 {
				t.Fatal("recovery prefix opened access")
			}
		}
		w = requestWithToken(handler, "POST", "/api/v1/domains", f.tokens[id])
		if w.Code != 503 || !strings.Contains(w.Body.String(), errCodeLicenseStatusUnavailable) {
			t.Fatalf("unknown license not closed: %d %s", w.Code, w.Body.String())
		}
	}
}
