package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/alicelik/celikpanel/internal/recoveryobs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOwnerViewOnlyAdmitsClosedSingleRequestOptions(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, args := range [][]string{{"view", "--request-id", id}, {"view", "--lang", "tr", "--request-id", id, "--port", "2090"}} {
		if _, ok := parseOwnerView(args); !ok {
			t.Fatalf("valid: %v", args)
		}
	}
	for _, args := range [][]string{{"view"}, {"view", "--request-id", "../file"}, {"view", "--request-id", id, "--request-id", id}, {"view", "--request-id", id, "--host", "0.0.0.0"}, {"view", "--request-id", id, "--port", "80"}, {"view", "--request-id", id, "--port", "02084"}, {"view", "--request-id", id, "--port", "65536"}, {"view", "--request-id", id, "--lang", "xx"}, {"view", "--request-id", id, "--snapshot", "x"}} {
		if _, ok := parseOwnerView(args); ok {
			t.Fatalf("accepted: %v", args)
		}
	}
}
func TestOwnerViewAuthenticatesBeforeReadingOneOperation(t *testing.T) {
	id := strings.Repeat("a", 32)
	secret := []byte(strings.Repeat("b", 32))
	token := hex.EncodeToString(secret)
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	reads := 0
	handler := ownerViewHandler(ownerViewOptions{id, 2084, "en"}, sha256.Sum256(secret), now.Add(time.Minute), func() time.Time { return now }, func(got string) recoveryobs.Status {
		reads++
		if got != id {
			t.Fatalf("wrong operation: %s", got)
		}
		return recoveryobs.Status{Schema: recoveryobs.StatusSchema, RequestID: id, Observation: "known", Phase: "recovery_required", TerminalProof: "none", Reason: "recovery_incomplete", AutomaticRecovery: "paused_retry_limit", ObservedAt: "2026-09-22T00:00:00Z", PreviousFailure: "update_failed"}
	})
	request := func(method, path, host, origin, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:2084"+path, nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, test := range []struct {
		method, path, host, origin, auth string
		code                             int
	}{
		{"GET", "/status", "127.0.0.1:2084", "", "", 401},
		{"GET", "/status", "127.0.0.1:2084", "", "Bearer " + strings.Repeat("0", 64), 401},
		{"GET", "/status", "attacker.test:2084", "", "Bearer " + token, 403},
		{"GET", "/status", "127.0.0.1:2084", "https://attacker.test", "Bearer " + token, 403},
		{"POST", "/status", "127.0.0.1:2084", "", "Bearer " + token, 405},
		{"GET", "/status?request_id=" + strings.Repeat("c", 32), "127.0.0.1:2084", "", "Bearer " + token, 400},
		{"GET", "/api/v1/panel/update/start", "127.0.0.1:2084", "", "Bearer " + token, 404},
		{"GET", "/status/retry", "127.0.0.1:2084", "", "Bearer " + token, 404},
	} {
		w := request(test.method, test.path, test.host, test.origin, test.auth)
		if w.Code != test.code {
			t.Fatalf("%+v: %d", test, w.Code)
		}
	}
	if reads != 0 {
		t.Fatal("unauthorized request read product state")
	}
	for _, path := range []string{"/", "/view.js", "/view.css"} {
		w := request("GET", path, "127.0.0.1:2084", "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), id) {
			t.Fatalf("asset leaks scope or secret: %s", path)
		}
		if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("missing access headers")
		}
	}
	w := request("GET", "/status", "127.0.0.1:2084", "http://127.0.0.1:2084", "Bearer "+token)
	if w.Code != 200 || reads != 1 {
		t.Fatalf("authorized read: %d / %d", w.Code, reads)
	}
	var value struct {
		Status recoveryobs.Status
		EN, TR string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Status.RequestID != id || !strings.Contains(value.EN, "one-time same-operation retry") || !strings.Contains(value.TR, "tek seferlik") {
		t.Fatal("owner guidance missing")
	}
	now = now.Add(time.Minute)
	if w = request("GET", "/status", "127.0.0.1:2084", "", "Bearer "+token); w.Code != 410 || reads != 1 {
		t.Fatal("expired capability read state")
	}
}
func TestOwnerViewUnknownCannotSubstituteAnotherOperation(t *testing.T) {
	secret := make([]byte, 32)
	id := strings.Repeat("a", 32)
	h := ownerViewHandler(ownerViewOptions{id, 2084, "en"}, sha256.Sum256(secret), time.Now().Add(time.Minute), time.Now, func(string) recoveryobs.Status {
		return recoveryobs.Status{Schema: recoveryobs.StatusSchema, RequestID: strings.Repeat("b", 32), Observation: "known", Phase: "succeeded", TerminalProof: "update_verified"}
	})
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:2084/status", nil)
	r.Header.Set("Authorization", "Bearer "+hex.EncodeToString(secret))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"observation":"unavailable"`) || strings.Contains(w.Body.String(), "update_verified") {
		t.Fatalf("untrusted success: %s", w.Body.String())
	}
}
