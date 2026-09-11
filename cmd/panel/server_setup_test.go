package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerSetupStateRoutingAndCheapRead(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	probes := 0
	f.panel.serverSetupProbe = func(context.Context, serverSetupDraft) ([]serverSetupCheck, error) {
		probes++
		return []serverSetupCheck{}, nil
	}
	for _, status := range []string{"new", "draft", "running", "waiting", "failed", "legacy", "ready"} {
		if _, err := f.database.GetDB().Exec(`UPDATE server_setup_state SET status=? WHERE id=1`, status); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		f.panel.handleServerSetup(w, serviceOperationAdminRequest(t, http.MethodGet, serverSetupPath, "", f.userID))
		var state serverSetupState
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &state) != nil {
			t.Fatalf("read: %d %s", w.Code, w.Body.String())
		}
		if state.Required != (status != "legacy" && status != "ready") {
			t.Fatalf("incorrect routing: %+v", state)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("setup snapshot can be cached")
		}
	}
	if probes != 0 || f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
		t.Fatal("opening setup performed a host operation")
	}
	w := httptest.NewRecorder()
	f.panel.handleServerSetup(w, serviceOperationAdminRequest(t, http.MethodGet, serverSetupPath+"?check=1", "", f.userID))
	if w.Code != http.StatusOK || probes != 1 {
		t.Fatalf("explicit evidence read: %d probes=%d", w.Code, probes)
	}
}

func TestServerSetupStateEndpointsDenyAllNonAdministrators(t *testing.T) {
	p := &Panel{}
	for _, role := range []string{"", roleReseller, roleCustomer, "additional"} {
		for _, endpoint := range []struct {
			method  string
			handler http.HandlerFunc
		}{
			{http.MethodGet, p.handleServerSetup}, {http.MethodPut, p.handleServerSetup}, {http.MethodPost, p.handleServerSetupComplete},
		} {
			r := httptest.NewRequest(endpoint.method, serverSetupPath, strings.NewReader(`{}`))
			if role != "" {
				r = r.WithContext(context.WithValue(r.Context(), callerKey, &Caller{ID: 1, Role: role}))
			}
			w := httptest.NewRecorder()
			endpoint.handler(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("role %q: %d", role, w.Code)
			}
		}
	}
	for _, path := range []string{serverSetupPath, serverSetupPath + "/plan", serverSetupPath + "/start", serverSetupPath + "/operation", serverSetupPath + "/complete"} {
		if !isAdminOnlyPath(path) {
			t.Fatalf("setup middleware omitted %s", path)
		}
	}
}

func TestServerSetupDraftCanonicalizationAndRevisionConflict(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	draft := defaultServerSetupDraft()
	draft.PanelDomain = "  PANEL.Example.COM  "
	state, err := f.panel.saveServerSetupDraft(context.Background(), 0, draft)
	if err != nil {
		t.Fatal(err)
	}
	if state.Revision != 1 || state.Draft.PanelDomain != "panel.example.com" {
		t.Fatalf("draft not canonical: %+v", state)
	}
	draft.PanelDomain = "other.example.com"
	if _, err := f.panel.saveServerSetupDraft(context.Background(), 0, draft); !errors.Is(err, errServerSetupConflict) {
		t.Fatalf("stale update: %v", err)
	}
	state, err = f.panel.loadServerSetup(context.Background())
	if err != nil || state.Draft.PanelDomain != "panel.example.com" {
		t.Fatalf("stale update replaced reviewed data: %+v %v", state, err)
	}
	for _, invalid := range []serverSetupDraft{
		{Purpose: "web", DNSMode: "unchecked"},
		{Purpose: "web", DNSMode: "external", PanelDomain: "127.0.0.1"},
		{Purpose: "dns", DNSMode: "local", PeerIP: "0.0.0.0"},
		{Purpose: "application", DNSMode: "external", NodeVersion: "22;shutdown"},
	} {
		if _, err := canonicalServerSetupDraft(invalid); err == nil {
			t.Fatalf("invalid draft accepted: %+v", invalid)
		}
	}
}

func TestServerSetupCompletionRequiresFreshNonemptyEvidence(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	state, err := f.panel.saveServerSetupDraft(context.Background(), 0, defaultServerSetupDraft())
	if err != nil {
		t.Fatal(err)
	}
	var evidence []serverSetupCheck
	var probeErr error
	probes := 0
	f.panel.serverSetupProbe = func(context.Context, serverSetupDraft) ([]serverSetupCheck, error) {
		probes++
		return evidence, probeErr
	}
	for _, checks := range [][]serverSetupCheck{nil, {}, {{ID: "panel_https", State: "unknown"}}, {{ID: "panel_https", State: "ready"}, {ID: "firewall", State: "action_required"}}} {
		evidence = checks
		if err := f.panel.completeServerSetup(context.Background(), state.Revision); err == nil {
			t.Fatalf("completed with insufficient evidence: %+v", checks)
		}
	}
	evidence = []serverSetupCheck{{ID: "panel_https", State: "ready"}}
	probeErr = errors.New("agent unavailable")
	if err := f.panel.completeServerSetup(context.Background(), state.Revision); err == nil {
		t.Fatal("probe error became ready")
	}
	probeErr = nil
	if err := f.panel.completeServerSetup(context.Background(), state.Revision-1); !errors.Is(err, errServerSetupConflict) {
		t.Fatalf("stale completion: %v", err)
	}
	if err := f.panel.completeServerSetup(context.Background(), state.Revision); err != nil {
		t.Fatal(err)
	}
	state, err = f.panel.loadServerSetup(context.Background())
	if err != nil || state.Status != "ready" || state.Required || state.CompletedAt == "" {
		t.Fatalf("completion not persisted: %+v %v", state, err)
	}
	before := probes
	probeErr = errors.New("later outage")
	if err := f.panel.completeServerSetup(context.Background(), state.Revision); err != nil || probes != before {
		t.Fatalf("established server re-entered setup: %v probes=%d", err, probes)
	}
}

func TestServerSetupCorruptPersistedDraftCannotMarkReady(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	for _, raw := range []string{`not-json`, `{"dns_mode":"skip"}`, `{"panel_domain":"127.0.0.1"}`} {
		if _, err := f.database.GetDB().Exec(`UPDATE server_setup_state SET draft_json=? WHERE id=1`, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := f.panel.loadServerSetup(context.Background()); err == nil {
			t.Fatalf("corrupt draft accepted: %s", raw)
		}
	}
}
