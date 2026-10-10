package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The mail identity check names the first unmet condition and the observed
// values the owner acts on (owner report 2026-10-10: setup waited for a reverse
// DNS record and the screen did not say so). A ready or unknown check carries
// no reason; the state and code are unchanged.
// Posta kimligi kontrolu karsilanmayan ilk kosulu ve gozlenen degerleri tasir.
func TestServerSetupMailIdentityCheckNamesTheTypedReason(t *testing.T) {
	base := transport.MailHealthResponse{ServerIP: "203.0.113.42", Myhostname: "mail.example.test", HostnameFQDN: true, PTR: "mail.example.test.", PTRAligned: true, FCrDNS: true}
	if check := setupMailIdentityCheck(base, nil, "mail.example.test"); check.State != "ready" || check.Reason != "" || check.Vars != nil {
		t.Fatalf("ready check = %+v", check)
	}
	if check := setupMailIdentityCheck(base, errors.New("agent unavailable"), "mail.example.test"); check.State != "unknown" || check.Code != "mail_identity_unavailable" || check.Reason != "" || check.Vars != nil {
		t.Fatalf("unknown check = %+v", check)
	}
	for _, test := range []struct {
		name   string
		change func(*transport.MailHealthResponse)
		reason string
		vars   map[string]string
	}{
		{"provider PTR", func(h *transport.MailHealthResponse) {
			h.PTR, h.PTRAligned, h.FCrDNS = "static.42.113.0.203.provider.example.", false, false
		}, "reverse_dns_mismatch", map[string]string{"hostname": "mail.example.test", "ip": "203.0.113.42", "ptr": "static.42.113.0.203.provider.example"}},
		{"no PTR found", func(h *transport.MailHealthResponse) {
			h.PTR, h.PTRAligned, h.FCrDNS = "", false, false
		}, "reverse_dns_mismatch", map[string]string{"hostname": "mail.example.test", "ip": "203.0.113.42"}},
		{"PTR not a plain name", func(h *transport.MailHealthResponse) {
			h.PTR, h.PTRAligned = "<b>x</b>", false
		}, "reverse_dns_mismatch", map[string]string{"hostname": "mail.example.test", "ip": "203.0.113.42"}},
		{"forward does not return", func(h *transport.MailHealthResponse) { h.FCrDNS = false },
			"forward_dns_mismatch", map[string]string{"hostname": "mail.example.test", "ip": "203.0.113.42"}},
		{"mail name differs", func(h *transport.MailHealthResponse) { h.Myhostname = "debian"; h.HostnameFQDN = false },
			"mail_name_differs", map[string]string{"hostname": "mail.example.test", "ip": "203.0.113.42", "current": "debian"}},
		{"private address", func(h *transport.MailHealthResponse) { h.ServerIP = "192.168.1.1" },
			"server_address_not_public", map[string]string{"ip": "192.168.1.1"}},
		{"no address", func(h *transport.MailHealthResponse) { h.ServerIP = "" },
			"server_address_not_public", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			health := base
			test.change(&health)
			check := setupMailIdentityCheck(health, nil, "mail.example.test")
			if check.State != "action_required" || check.Code != "mail_identity_required" {
				t.Fatalf("state/code changed: %+v", check)
			}
			if check.Reason != test.reason || !reflect.DeepEqual(check.Vars, test.vars) {
				t.Fatalf("reason=%q vars=%v, want %q %v", check.Reason, check.Vars, test.reason, test.vars)
			}
		})
	}
	// A record written before the reason existed decodes to the same check.
	var old serverSetupCheck
	if err := json.Unmarshal([]byte(`{"id":"mail_identity","state":"action_required","code":"mail_identity_required"}`), &old); err != nil || old.Reason != "" || old.Vars != nil {
		t.Fatalf("old record = %+v err=%v", old, err)
	}
	encoded, _ := json.Marshal(serverSetupCheck{ID: "dns", State: "ready", Code: "ready"})
	if string(encoded) != `{"id":"dns","state":"ready","code":"ready"}` {
		t.Fatalf("a check without a reason grew fields: %s", encoded)
	}
}

// While the Agent's record still says running, the Panel answering is already
// the target build: the status says "verifying" so the notice and the card can
// say the version is installed and being verified (owner report 2026-10-10).
// Kayit running iken yanit veren Panel hedef surumse durum "verifying" der.
func TestPanelUpdateStatusSaysVerifyingWhenThisPanelIsTheTarget(t *testing.T) {
	status := func(state string) transport.SystemUpdateStatusResponse {
		return transport.SystemUpdateStatusResponse{
			Found: true, RequestID: updateTestRequestID, Status: state,
			TargetVersion: updateTestTargetVersion, TargetCommit: updateTestTargetCommit,
			TargetSequence: "14", TargetOS: "linux", TargetArch: "amd64",
			TargetArchiveSHA256: updateTestTargetSHA, TargetArchiveSize: "1048576",
		}
	}
	read := func(t *testing.T, fixture systemUpdateTestFixture) panelUpdateStatusResponse {
		t.Helper()
		recorder := httptest.NewRecorder()
		fixture.panel.handlePanelUpdateStatus(recorder, systemUpdateRequest(
			http.MethodGet, panelUpdateStatusPath+"?request_id="+updateTestRequestID, "", roleAdmin,
		))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var response panelUpdateStatusResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	t.Run("old panel still answering", func(t *testing.T) {
		withSystemUpdateBuild(t)
		fixture := newSystemUpdateTestFixture(t)
		fixture.agent.status = status("running")
		if got := read(t, fixture); got.Status != "running" || got.Phase != "" {
			t.Fatalf("applying = %+v", got)
		}
	})
	for _, test := range []struct{ state, phase string }{{"running", "verifying"}, {"succeeded", ""}, {"failed", ""}, {"queued", ""}} {
		t.Run("target panel answering "+test.state, func(t *testing.T) {
			oldVersion, oldCommit := buildVersion, buildCommit
			buildVersion, buildCommit = updateTestTargetVersion, updateTestTargetCommit
			t.Cleanup(func() { buildVersion, buildCommit = oldVersion, oldCommit })
			fixture := newSystemUpdateTestFixture(t)
			fixture.agent.version.Version = updateTestTargetVersion
			fixture.agent.version.Commit = updateTestTargetCommit
			fixture.agent.status = status(test.state)
			if got := read(t, fixture); got.Status != test.state || got.Phase != test.phase {
				t.Fatalf("%s = %+v, want phase %q", test.state, got, test.phase)
			}
		})
	}
	if panelUpdateStatusPhase("running", panelUpdateTarget{Version: buildVersion, Commit: "other"}) != "" {
		t.Fatal("a different commit of the same version was called installed")
	}
}
