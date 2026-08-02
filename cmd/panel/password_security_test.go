package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
)

func TestChangeOwnPasswordRevokesAllSessionsAndClearsCookies(t *testing.T) {
	isolatePendingLogins(t)
	panel, database, user := newTOTPSecurityPanel(t)
	state, err := panel.userAuthState(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	pendingToken, err := newPendingToken(state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/password`, strings.NewReader(
		`{"current_password":"correct horse battery staple","new_password":"new secure password"}`,
	))
	request = request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: user.ID, Role: roleCustomer},
	))
	panel.handleChangeOwnPassword(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf(`password change status = %d; body=%s`, recorder.Code, recorder.Body.String())
	}

	for _, token := range []string{first, second} {
		if _, err := panel.sessions.Validate(t.Context(), token); err == nil {
			t.Fatal(`old session remained valid after password change`)
		}
	}
	var sessions int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf(`password change left %d sessions`, sessions)
	}
	if _, ok := consumePendingToken(pendingToken); ok {
		t.Fatal(`password change left a pre-change pending login valid`)
	}
	var epoch int64
	if err := database.GetDB().QueryRow(`SELECT auth_epoch FROM users WHERE id = ?`, user.ID).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if epoch != state.authEpoch+1 {
		t.Fatalf(`password change auth_epoch = %d, want %d`, epoch, state.authEpoch+1)
	}
	updated, err := panel.users.GetByID(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := auth.VerifyPassword(`new secure password`, updated.PasswordHash)
	if err != nil || !ok {
		t.Fatalf(`new password did not verify: ok=%v err=%v`, ok, err)
	}

	cookies := recorder.Result().Cookies()
	cleared := map[string]bool{}
	for _, cookie := range cookies {
		if cookie.MaxAge < 0 || cookie.Expires.Before(time.Now()) {
			cleared[cookie.Name] = true
		}
	}
	if !cleared[sessionCookieName] || !cleared[impersonatorCookieName] {
		t.Fatalf(`cleared cookies = %v`, cleared)
	}
}

func TestManagedPasswordResetRevokesAllTargetSessions(t *testing.T) {
	panel, database, user := newTOTPSecurityPanel(t)
	token, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, `/api/v1/users/1`, strings.NewReader(
		`{"password":"managed reset password"}`,
	))
	panel.handleUpdateUser(recorder, request, user)
	if recorder.Code != http.StatusOK {
		t.Fatalf(`managed reset status = %d; body=%s`, recorder.Code, recorder.Body.String())
	}
	if _, err := panel.sessions.Validate(t.Context(), token); err == nil {
		t.Fatal(`target session remained valid after managed password reset`)
	}
	var sessions int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf(`managed reset left %d sessions`, sessions)
	}
	updated, err := panel.users.GetByID(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := auth.VerifyPassword(`managed reset password`, updated.PasswordHash)
	if err != nil || !ok {
		t.Fatalf(`managed reset password did not verify: ok=%v err=%v`, ok, err)
	}
}

func TestChangeOwnPasswordIsRateLimitedBeforeCredentialMutation(t *testing.T) {
	panel, _, user := newTOTPSecurityPanel(t)
	panel.loginLimiter = newRateLimiter(1, time.Minute)
	existing, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	callerContext := func(request *http.Request) *http.Request {
		return request.WithContext(context.WithValue(
			request.Context(), callerKey, &Caller{ID: user.ID, Role: roleCustomer},
		))
	}

	wrongRecorder := httptest.NewRecorder()
	wrongRequest := callerContext(httptest.NewRequest(http.MethodPost, `/api/v1/auth/password`, strings.NewReader(
		`{"current_password":"wrong password","new_password":"attacker supplied password"}`,
	)))
	panel.handleChangeOwnPassword(wrongRecorder, wrongRequest)
	if wrongRecorder.Code != http.StatusForbidden {
		t.Fatalf(`first password attempt status = %d, want 403; body=%s`,
			wrongRecorder.Code, wrongRecorder.Body.String())
	}

	blockedRecorder := httptest.NewRecorder()
	blockedRequest := callerContext(httptest.NewRequest(http.MethodPost, `/api/v1/auth/password`, strings.NewReader(
		`{"current_password":"correct horse battery staple","new_password":"new secure password"}`,
	)))
	panel.handleChangeOwnPassword(blockedRecorder, blockedRequest)
	if blockedRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf(`second password attempt status = %d, want 429; body=%s`,
			blockedRecorder.Code, blockedRecorder.Body.String())
	}

	unchanged, err := panel.users.GetByID(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := auth.VerifyPassword(`correct horse battery staple`, unchanged.PasswordHash)
	if err != nil || !ok {
		t.Fatalf(`rate-limited password was unexpectedly mutated: ok=%v err=%v`, ok, err)
	}
	requireSessionValidForUser(t, panel, existing, user.ID)
}
