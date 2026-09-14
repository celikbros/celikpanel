package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthObservationMeSecondReadFailureDoesNotClearIdentity(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent=%v", parent), func(t *testing.T) {
			f := newAuthzMatrixFixture(t)
			id, lookupID := authzMatrixAdminID, authzMatrixAdminID
			if parent {
				seedAdditionalUserSession(t, &f)
				id, lookupID = authzMatrixAdditionalUserID, authzMatrixCustomerID
			}
			original := f.panel.users
			handler := f.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Middleware established identity; the handler lookup now fails.
				f.panel.users = &authzUserRepositoryOverride{UserRepository: original, errs: map[int]error{lookupID: errors.New("private database error")}}
				f.panel.handleMe(w, r)
			}))
			w := requestWithToken(handler, "GET", "/api/v1/auth/me", f.tokens[id])
			if w.Code != 503 || !strings.Contains(w.Body.String(), errCodeAuthStatusUnavailable) || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "private database") {
				t.Fatalf("second read: %d %s", w.Code, w.Body.String())
			}
			if _, err := f.panel.sessions.Validate(context.Background(), f.tokens[id]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAuthObservationLoginKeepsInvalidCredentialsIndistinguishable(t *testing.T) {
	p, database, user := newTOTPSecurityPanel(t)
	login := func(username, password string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		p.handleLogin(w, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":%q}`, username, password))))
		return w
	}
	missing := login("absent", "wrong")
	wrong := login(user.Username, "wrong")
	if missing.Code != 401 || wrong.Code != 401 || missing.Body.String() != wrong.Body.String() || missing.Header().Get("Set-Cookie") != "" || wrong.Header().Get("Set-Cookie") != "" {
		t.Fatal("invalid credentials became distinguishable")
	}
	p.users = &authzUserRepositoryOverride{UserRepository: p.users, errs: map[int]error{user.ID: errors.New("private canonical read error")}}
	w := login(user.Username, "correct horse battery staple")
	if w.Code != 503 || !strings.Contains(w.Body.String(), errCodeAuthStatusUnavailable) || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "private canonical") {
		t.Fatalf("canonical login read: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := database.GetDB().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal("unavailable login created session", count, err)
	}
	if err := database.GetDB().Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{user.Username, "absent"} {
		w := login(name, "correct horse battery staple")
		if w.Code != 503 || !strings.Contains(w.Body.String(), errCodeAuthStatusUnavailable) || w.Header().Get("Set-Cookie") != "" {
			t.Fatalf("DB failure for lookup: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestAuthObservationTOTPUnavailableDoesNotReuseConsumedProof(t *testing.T) {
	for _, failure := range []string{"database", "canonical user", "canonical parent"} {
		t.Run(failure, func(t *testing.T) {
			isolatePendingLogins(t)
			p, database, user := newTOTPSecurityPanel(t)
			lookupID := user.ID
			if failure == "canonical parent" {
				makeTOTPSecurityAdditionalUser(t, p, database, user)
				lookupID = *user.ParentID
			}
			state, secret := enableTOTPForSecurityTest(t, p, database, user.ID)
			pending, err := newPendingToken(state)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "database" {
				if err := database.GetDB().Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				p.users = &authzUserRepositoryOverride{UserRepository: p.users, errs: map[int]error{lookupID: errors.New("private read error")}}
			}
			w := completePendingTOTPForSecurityTest(p, pending, totpCodeForSecurityTest(t, secret))
			if w.Code != 503 || !strings.Contains(w.Body.String(), errCodeAuthStatusUnavailable) || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "private read") {
				t.Fatalf("unavailable TOTP: %d %s", w.Code, w.Body.String())
			}
			if _, ok := consumePendingToken(pending); ok {
				t.Fatal("consumed TOTP proof became reusable")
			}
			if failure != "database" {
				var count int
				if err := database.GetDB().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 0 {
					t.Fatal("unavailable TOTP created session", count, err)
				}
			}
		})
	}
}
