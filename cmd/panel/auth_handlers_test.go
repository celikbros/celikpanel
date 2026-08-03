package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
)

const authHandlerTestUserID = 7601

func newAuthHandlerTestPanel(t *testing.T, secureCookies bool) (*Panel, string) {
	t.Helper()

	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open auth handler database: %v", err)
	}
	t.Cleanup(database.Close)

	sqlDB := database.GetDB()
	if _, err := sqlDB.Exec(`
		INSERT INTO users (id, username, password_hash, email, role, status)
		VALUES (?, 'session-admin', 'unused', 'admin@example.test', 'admin', 'active')
	`, authHandlerTestUserID); err != nil {
		t.Fatalf("seed auth handler user: %v", err)
	}

	sessions := auth.NewSessionStore(sqlDB)
	token, err := sessions.Create(context.Background(), authHandlerTestUserID)
	if err != nil {
		t.Fatalf("create auth handler session: %v", err)
	}

	return &Panel{
		db:            database,
		sessions:      sessions,
		users:         repositories.NewPostgresUserRepository(sqlDB),
		secureCookies: secureCookies,
	}, token
}

func TestSessionCookieSecurityAttributes(t *testing.T) {
	expires := time.Date(2031, time.March, 4, 5, 6, 7, 0, time.UTC)

	for _, testCase := range []struct {
		name          string
		secureCookies bool
		wantCookieTLS bool
	}{
		{name: "TLS deployment", secureCookies: true, wantCookieTLS: true},
		{name: "explicit HTTP development", secureCookies: false, wantCookieTLS: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			panel := &Panel{secureCookies: testCase.secureCookies}
			cookie := panel.sessionCookie("raw-session-token", expires)

			if cookie.Name != sessionCookieName || cookie.Value != "raw-session-token" {
				t.Fatalf("cookie identity = %q/%q", cookie.Name, cookie.Value)
			}
			if cookie.Path != "/" || cookie.Domain != "" {
				t.Fatalf("cookie scope = domain %q path %q", cookie.Domain, cookie.Path)
			}
			if !cookie.HttpOnly {
				t.Fatal("session cookie is not HttpOnly")
			}
			if cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("SameSite = %v, want Lax", cookie.SameSite)
			}
			if cookie.Secure != testCase.wantCookieTLS {
				t.Fatalf("Secure = %v, want %v", cookie.Secure, testCase.wantCookieTLS)
			}
			if !cookie.Expires.Equal(expires) {
				t.Fatalf("Expires = %v, want %v", cookie.Expires, expires)
			}
		})
	}
}

func TestHandleLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	panel, token := newAuthHandlerTestPanel(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	recorder := httptest.NewRecorder()

	panel.handleLogout(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if _, err := panel.sessions.Validate(context.Background(), token); err != auth.ErrSessionInvalid {
		t.Fatalf("session remains valid after logout: %v", err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("logout cookies = %d, want 1", len(cookies))
	}
	cleared := cookies[0]
	if cleared.Name != sessionCookieName || cleared.Value != "" {
		t.Fatalf("cleared cookie identity = %q/%q", cleared.Name, cleared.Value)
	}
	if !cleared.HttpOnly || !cleared.Secure || cleared.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cleared cookie flags = HttpOnly:%v Secure:%v SameSite:%v", cleared.HttpOnly, cleared.Secure, cleared.SameSite)
	}
	if !cleared.Expires.Before(time.Now()) {
		t.Fatalf("cleared cookie expiry = %v, want past time", cleared.Expires)
	}
}

func TestHandleLogoutIsIdempotentWithoutCookie(t *testing.T) {
	panel, _ := newAuthHandlerTestPanel(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	recorder := httptest.NewRecorder()

	panel.handleLogout(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != sessionCookieName {
		t.Fatalf("idempotent logout did not clear the browser cookie: %#v", cookies)
	}
}

func TestHandleLogoutRejectsNonPOSTWithoutRevokingSession(t *testing.T) {
	panel, token := newAuthHandlerTestPanel(t, true)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	recorder := httptest.NewRecorder()

	panel.handleLogout(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if _, err := panel.sessions.Validate(context.Background(), token); err != nil {
		t.Fatalf("rejected GET revoked the session: %v", err)
	}
	if got := recorder.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("rejected GET changed cookies: %v", got)
	}
}

func TestHandleLogoutDoesNotClaimSuccessWhenRevocationFails(t *testing.T) {
	panel, token := newAuthHandlerTestPanel(t, true)
	panel.db.Close()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	recorder := httptest.NewRecorder()

	panel.handleLogout(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].Value != "" {
		t.Fatalf("failed revocation did not clear the browser cookie: %#v", cookies)
	}
}

func TestHandleUnimpersonateRevokesCurrentSessionAndRestoresOriginal(t *testing.T) {
	panel, originalToken := newAuthHandlerTestPanel(t, true)
	impersonatedToken, err := panel.sessions.Create(context.Background(), authHandlerTestUserID)
	if err != nil {
		t.Fatalf("create impersonated session: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unimpersonate", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: impersonatedToken})
	request.AddCookie(&http.Cookie{Name: impersonatorCookieName, Value: originalToken})
	recorder := httptest.NewRecorder()

	panel.handleUnimpersonate(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	assertUnimpersonateCookies(t, recorder, originalToken)
	if _, err := panel.sessions.Validate(context.Background(), impersonatedToken); err != auth.ErrSessionInvalid {
		t.Fatalf("impersonated session remains valid: %v", err)
	}
	if userID, err := panel.sessions.Validate(context.Background(), originalToken); err != nil || userID != authHandlerTestUserID {
		t.Fatalf("original session = user %d, %v", userID, err)
	}
}

func TestHandleUnimpersonateDoesNotClaimSuccessWhenRevocationFails(t *testing.T) {
	panel, originalToken := newAuthHandlerTestPanel(t, true)
	impersonatedToken, err := panel.sessions.Create(context.Background(), authHandlerTestUserID)
	if err != nil {
		t.Fatalf("create impersonated session: %v", err)
	}
	if _, err := panel.db.GetDB().Exec(`
		CREATE TRIGGER fail_session_delete
		BEFORE DELETE ON sessions
		BEGIN
			SELECT RAISE(ABORT, 'forced session delete failure');
		END
	`); err != nil {
		t.Fatalf("create session delete failure trigger: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unimpersonate", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: impersonatedToken})
	request.AddCookie(&http.Cookie{Name: impersonatorCookieName, Value: originalToken})
	recorder := httptest.NewRecorder()

	panel.handleUnimpersonate(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	assertUnimpersonateCookies(t, recorder, "")
	if _, err := panel.sessions.Validate(context.Background(), impersonatedToken); err != nil {
		t.Fatalf("failed delete unexpectedly removed impersonated session: %v", err)
	}
}

func TestHandleUnimpersonateRevokesCurrentSessionWhenOriginalExpired(t *testing.T) {
	panel, originalToken := newAuthHandlerTestPanel(t, true)
	impersonatedToken, err := panel.sessions.Create(context.Background(), authHandlerTestUserID)
	if err != nil {
		t.Fatalf("create impersonated session: %v", err)
	}
	if err := panel.sessions.Delete(context.Background(), originalToken); err != nil {
		t.Fatalf("expire original session: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unimpersonate", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: impersonatedToken})
	request.AddCookie(&http.Cookie{Name: impersonatorCookieName, Value: originalToken})
	recorder := httptest.NewRecorder()

	panel.handleUnimpersonate(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	assertUnimpersonateCookies(t, recorder, "")
	if _, err := panel.sessions.Validate(context.Background(), impersonatedToken); err != auth.ErrSessionInvalid {
		t.Fatalf("impersonated session remains valid after original expiry: %v", err)
	}
}

func TestHandleUnimpersonateRejectsDuplicatedSessionCookieWithoutRevokingIt(t *testing.T) {
	panel, token := newAuthHandlerTestPanel(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unimpersonate", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	request.AddCookie(&http.Cookie{Name: impersonatorCookieName, Value: token})
	recorder := httptest.NewRecorder()

	panel.handleUnimpersonate(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != impersonatorCookieName || cookies[0].Value != "" || cookies[0].MaxAge >= 0 {
		t.Fatalf("duplicated-state cookies = %#v", cookies)
	}
	if _, err := panel.sessions.Validate(context.Background(), token); err != nil {
		t.Fatalf("valid operator session was revoked: %v", err)
	}
}

func assertUnimpersonateCookies(t *testing.T, recorder *httptest.ResponseRecorder, restoredToken string) {
	t.Helper()
	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("unimpersonate cookies = %d, want 2: %#v", len(cookies), cookies)
	}
	byName := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	sessionCookie := byName[sessionCookieName]
	if sessionCookie == nil || sessionCookie.Value != restoredToken {
		t.Fatalf("session cookie = %#v, want value %q", sessionCookie, restoredToken)
	}
	if restoredToken == "" && !sessionCookie.Expires.Before(time.Now()) {
		t.Fatalf("cleared session expiry = %v, want past time", sessionCookie.Expires)
	}
	impersonatorCookie := byName[impersonatorCookieName]
	if impersonatorCookie == nil || impersonatorCookie.Value != "" || impersonatorCookie.MaxAge >= 0 {
		t.Fatalf("impersonator cookie was not cleared: %#v", impersonatorCookie)
	}
}

func TestHandleMeReturnsAuthenticatedProfileAndImpersonationState(t *testing.T) {
	panel, _ := newAuthHandlerTestPanel(t, true)

	for _, testCase := range []struct {
		name              string
		impersonatorToken string
		wantImpersonating bool
	}{
		{name: "ordinary session"},
		{name: "impersonated session", impersonatorToken: "operator-session", wantImpersonating: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			request = request.WithContext(context.WithValue(
				request.Context(), callerKey, &Caller{ID: authHandlerTestUserID, Role: roleAdmin},
			))
			if testCase.impersonatorToken != "" {
				request.AddCookie(&http.Cookie{Name: impersonatorCookieName, Value: testCase.impersonatorToken})
			}
			recorder := httptest.NewRecorder()

			panel.handleMe(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			var response struct {
				Username      string `json:"username"`
				Role          string `json:"role"`
				Email         string `json:"email"`
				Impersonating bool   `json:"impersonating"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode /me response: %v", err)
			}
			if response.Username != "session-admin" || response.Role != roleAdmin ||
				response.Email != "admin@example.test" ||
				response.Impersonating != testCase.wantImpersonating {
				t.Fatalf("/me response = %+v", response)
			}
		})
	}
}

func TestHandleMeRejectsWrongMethodAndMissingCaller(t *testing.T) {
	panel, _ := newAuthHandlerTestPanel(t, true)

	for _, testCase := range []struct {
		name   string
		method string
		caller *Caller
		want   int
	}{
		{name: "wrong method", method: http.MethodPost, caller: &Caller{ID: authHandlerTestUserID, Role: roleAdmin}, want: http.StatusMethodNotAllowed},
		{name: "missing caller", method: http.MethodGet, want: http.StatusUnauthorized},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "/api/v1/auth/me", nil)
			if testCase.caller != nil {
				request = request.WithContext(context.WithValue(request.Context(), callerKey, testCase.caller))
			}
			recorder := httptest.NewRecorder()

			panel.handleMe(recorder, request)

			if recorder.Code != testCase.want {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.want)
			}
		})
	}
}
