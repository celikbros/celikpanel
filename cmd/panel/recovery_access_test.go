package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

const recoveryHTTPTestID = "0123456789abcdef0123456789abcdef"

func TestStartupRecoveryServesExistingAuthenticationAndStaticShellWithoutAgent(t *testing.T) {
	p, token := newAuthHandlerTestPanel(t, true)
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("recovery shell fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	gate := newPanelHTTPStartupGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	p.startupGate = gate
	gate.recovery = p.startupRecoveryHandler(web, "", "")
	for _, path := range []string{"/", "/setup", "/activate"} {
		rr := requestWithToken(gate, http.MethodGet, path, "")
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), "recovery shell fixture") {
			t.Fatalf("shell %s: %d %s", path, rr.Code, rr.Body)
		}
	}
	for _, path := range []string{"/api/v1/auth/me", panelAvailabilityPath, panelRecoveryStatusPath + "?request_id=" + recoveryHTTPTestID} {
		rr := requestWithToken(gate, http.MethodGet, path, token)
		if rr.Code != 200 {
			t.Fatalf("read %s: %d %s", path, rr.Code, rr.Body)
		}
	}
	rr := requestWithToken(gate, http.MethodGet, panelAvailabilityPath, token)
	if !strings.Contains(rr.Body.String(), `"state":"starting"`) || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rr.Body.String())
	}
	rr = requestWithToken(gate, http.MethodGet, panelAvailabilityPath, "")
	if rr.Code != 401 {
		t.Fatalf("anonymous availability %d", rr.Code)
	}
	// No initial license verification or Agent client is needed for these reads.
	if p.agentClient != nil || p.license != nil || calls != 0 {
		t.Fatal("early recovery reached normal management")
	}
	gate.Open()
	rr = requestWithToken(gate, http.MethodGet, "/api/v1/domains", token)
	if rr.Code != 204 || calls != 1 || p.panelAvailabilityState() != "ready" {
		t.Fatal("explicit startup admission did not open normal handler")
	}
}

func TestStartupRecoveryNeverReachesManagementMutationsOrProxies(t *testing.T) {
	p, token := newAuthHandlerTestPanel(t, true)
	handler := p.startupRecoveryHandler(t.TempDir(), "", "")
	for _, path := range []string{"/api/v1/domains", "/api/v1/system/update/start", "/api/v1/server/setup", "/api/v1/panel/license", "/api/v1/recovery/status/retry", "/api/v1/auth/change-password", "/dbtool/phpmyadmin/", "/webmail/"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE", "HEAD"} {
			rr := requestWithToken(handler, method, path, token)
			if rr.Code != 503 || rr.Header().Get("Retry-After") != "3" || rr.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s %s: %d %s", method, path, rr.Code, rr.Body)
			}
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		rr := requestWithToken(handler, method, panelRecoveryStatusPath+"?request_id="+recoveryHTTPTestID, token)
		if rr.Code != 503 {
			t.Fatalf("recovery mutation %s: %d", method, rr.Code)
		}
	}
}

func TestRecoveryObservationHTTPAuthorizesExactAdminAndExactRequestBeforeRead(t *testing.T) {
	f := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &f)
	p := f.panel
	calls := 0
	read := func(id string) recoveryobs.Status {
		calls++
		if id != recoveryHTTPTestID {
			t.Fatalf("wrong ID read: %q", id)
		}
		return recoveryobs.Status{Schema: recoveryobs.StatusSchema, RequestID: id, Observation: "known", Phase: "recovered", TerminalProof: "rollback_verified", Reason: "rollback_verified", ObservedAt: "2026-09-14T05:53:39Z", PreviousFailure: "update_failed"}
	}
	handler := p.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.serveRecoveryStatus(w, r, read) }))
	for _, id := range []int{authzMatrixResellerID, authzMatrixCustomerID, authzMatrixOutsiderID, 9710} {
		rr := requestWithToken(handler, "GET", panelRecoveryStatusPath+"?request_id="+recoveryHTTPTestID, f.tokens[id])
		if rr.Code != 403 || calls != 0 {
			t.Fatalf("tenant %d: %d reads=%d", id, rr.Code, calls)
		}
	}
	for _, query := range []string{"", "?request_id=bad", "?request_id=" + recoveryHTTPTestID + "&request_id=" + recoveryHTTPTestID, "?request_id=" + recoveryHTTPTestID + "&latest=1", "?request_id=%zz", "?request_id=" + strings.Repeat("a", 97)} {
		rr := requestWithToken(handler, "GET", panelRecoveryStatusPath+query, f.tokens[authzMatrixAdminID])
		if rr.Code != 400 || calls != 0 {
			t.Fatalf("query %q: %d reads=%d", query, rr.Code, calls)
		}
	}
	rr := requestWithToken(handler, "GET", panelRecoveryStatusPath+"?request_id="+recoveryHTTPTestID, f.tokens[authzMatrixAdminID])
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 200 || calls != 1 || body["request_id"] != recoveryHTTPTestID || body["terminal_proof"] != "rollback_verified" || body["previous_failure"] != "update_failed" || body["panel_state"] != "starting" {
		t.Fatalf("response %d: %s", rr.Code, rr.Body)
	}
	if len(body) != 9 || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected public shape: %v", body)
	}
	// Explicit handler method defense in addition to the startup mux allowlist.
	request := httptest.NewRequest("POST", panelRecoveryStatusPath, nil)
	response := httptest.NewRecorder()
	p.serveRecoveryStatus(response, request, read)
	if response.Code != 405 || calls != 1 {
		t.Fatal("non-read reached native reader")
	}
}
