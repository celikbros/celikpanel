package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// --- a domain's catch-all ------------------------------------------------------

func catchAllRequest(method, version, body string) *http.Request {
	target := "/mail/catch-all"
	if version != "" {
		target += "?version=" + version
	}
	if body == "" {
		return httptest.NewRequest(method, target, nil)
	}
	return httptest.NewRequest(method, target, strings.NewReader(body))
}

func readCatchAllForTest(t *testing.T, panel *Panel, domainID int) (enabled bool, destination, version string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	panel.handleMailCatchAll(recorder, catchAllRequest(http.MethodGet, "", ""), domainID)
	var answer struct {
		Enabled     bool   `json:"enabled"`
		Destination string `json:"destination"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("read = %d %q (%v)", recorder.Code, recorder.Body.String(), err)
	}
	if answer.Version == "" {
		t.Fatalf("the read carries no version: %q", recorder.Body.String())
	}
	return answer.Enabled, answer.Destination, answer.Version
}

func storedCatchAll(t *testing.T, panel *Panel, domainID int) string {
	t.Helper()
	state, err := panel.readMailCatchAll(context.Background(), domainID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.exists {
		return ""
	}
	return state.destination
}

// The defect: the address field was editable before the read answered, and
// what was typed replaced, by upsert, an address the page had never shown.
func TestCatchAllWritesNeedTheVersionOfTheCatchAllTheyReplace(t *testing.T) {
	agent := &mailMutationPanelAgent{forwardingApplied: true}
	panel, domainID := newMailMutationPanel(t, agent)
	if _, err := panel.db.GetDB().Exec(`INSERT INTO mail_catch_all (domain_id, destination) VALUES (?, 'owner@example.net')`, domainID); err != nil {
		t.Fatal(err)
	}
	enabled, destination, version := readCatchAllForTest(t, panel, domainID)
	if !enabled || destination != "owner@example.net" {
		t.Fatalf("read = %v %q", enabled, destination)
	}

	refused := func(name string, request *http.Request, code string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		panel.handleMailCatchAll(recorder, request, domainID)
		body := decodeAPIError(t, recorder)
		if recorder.Code != http.StatusConflict || body.Code != code || body.Reason != settingsResourceMailCatchAll {
			t.Fatalf("%s: answer = %d %+v, want 409 %s", name, recorder.Code, body, code)
		}
		if got := storedCatchAll(t, panel, domainID); got != "owner@example.net" {
			t.Fatalf("%s: the catch-all was changed to %q", name, got)
		}
		if calls := len(agent.forwardingCalls); calls != 0 {
			t.Fatalf("%s: a refused write reached Postfix (%d calls)", name, calls)
		}
	}
	absent := mailCatchAllState{}.version()
	refused("a form that never loaded", catchAllRequest(http.MethodPut, "", `{"destination":"typed@example.org"}`), errCodeSettingsVersionRequired)
	refused("a form that loaded 'no catch-all'", catchAllRequest(http.MethodPut, "", `{"destination":"typed@example.org","version":"`+absent+`"}`), errCodeSettingsChanged)
	refused("turning off without a version", catchAllRequest(http.MethodDelete, "", ""), errCodeSettingsVersionRequired)
	refused("turning off what another page already changed", catchAllRequest(http.MethodDelete, mailCatchAllState{exists: true, destination: "older@example.net"}.version(), ""), errCodeSettingsChanged)

	// With the version that was read, the write goes through and answers the
	// version the next one needs.
	recorder := httptest.NewRecorder()
	panel.handleMailCatchAll(recorder, catchAllRequest(http.MethodPut, "", `{"destination":"New@Example.org","version":"`+version+`"}`), domainID)
	var saved struct {
		Enabled     bool   `json:"enabled"`
		Destination string `json:"destination"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("save = %d %q (%v)", recorder.Code, recorder.Body.String(), err)
	}
	stored := storedCatchAll(t, panel, domainID)
	if !saved.Enabled || saved.Destination != stored || stored == "owner@example.net" || saved.Version == version || saved.Version == "" {
		t.Fatalf("saved = %+v, stored %q", saved, stored)
	}
	// The version the page held before is stale now.
	recorder = httptest.NewRecorder()
	panel.handleMailCatchAll(recorder, catchAllRequest(http.MethodDelete, version, ""), domainID)
	if body := decodeAPIError(t, recorder); body.Code != errCodeSettingsChanged || storedCatchAll(t, panel, domainID) != stored {
		t.Fatalf("a stale turn-off: %+v", body)
	}
	recorder = httptest.NewRecorder()
	panel.handleMailCatchAll(recorder, catchAllRequest(http.MethodDelete, saved.Version, ""), domainID)
	var off struct {
		Enabled bool   `json:"enabled"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &off); err != nil || recorder.Code != http.StatusOK || off.Enabled || off.Version != absent {
		t.Fatalf("turn-off = %d %q", recorder.Code, recorder.Body.String())
	}
	if storedCatchAll(t, panel, domainID) != "" {
		t.Fatal("the catch-all was not removed")
	}

	// And from "none", a first address.
	recorder = httptest.NewRecorder()
	panel.handleMailCatchAll(recorder, catchAllRequest(http.MethodPut, "", `{"destination":"first@example.org","version":"`+absent+`"}`), domainID)
	if recorder.Code != http.StatusOK || storedCatchAll(t, panel, domainID) != "first@example.org" {
		t.Fatalf("first address = %d %q", recorder.Code, recorder.Body.String())
	}
}

// --- the mail queue ------------------------------------------------------------

type mailQueueTestAgent struct {
	verifiedAPTAgentRPCFixture
	result core.PostfixQueueResult
	err    error
}

func (a *mailQueueTestAgent) PostfixQueue(_ *transport.Empty, resp *core.PostfixQueueResult) error {
	*resp = a.result
	return a.err
}

func newAgentPanel(t *testing.T, agent any) *Panel {
	t.Helper()
	panel := newPanelBackupFixture(t).panel
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register test agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	panel.pkgFamilyVal = "apt"
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	return panel
}

// The defect: a queue that could not be read was shown as "the queue is empty".
func TestMailQueueThatCouldNotBeReadIsNotAnEmptyQueue(t *testing.T) {
	for _, route := range []struct {
		name    string
		handler func(*Panel) http.HandlerFunc
	}{
		{"the list", func(p *Panel) http.HandlerFunc { return p.handlePostfixQueue }},
		{"the counts", func(p *Panel) http.HandlerFunc { return p.handlePostfixSummary }},
	} {
		t.Run(route.name+": the Agent could not read it", func(t *testing.T) {
			panel := newAgentPanel(t, &mailQueueTestAgent{err: errors.New(transport.PostfixQueueUnreadable)})
			recorder := httptest.NewRecorder()
			route.handler(panel)(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/postfix/queue", nil))
			body := decodeAPIError(t, recorder)
			if recorder.Code != http.StatusBadGateway || body.Code != errCodeMailQueueUnreadable {
				t.Fatalf("answer = %d %+v", recorder.Code, body)
			}
			for _, part := range []string{"could not be read", "does not mean the queue is empty", "Nothing was changed", "sudo systemctl status postfix"} {
				if !strings.Contains(body.Error, part) {
					t.Errorf("guidance lacks %q: %q", part, body.Error)
				}
			}
		})
		t.Run(route.name+": another Agent failure is not an empty queue either", func(t *testing.T) {
			panel := newAgentPanel(t, &mailQueueTestAgent{err: errors.New("exec: postqueue: permission denied")})
			recorder := httptest.NewRecorder()
			route.handler(panel)(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/postfix/queue", nil))
			if recorder.Code < 500 || strings.Contains(recorder.Body.String(), "permission denied") {
				t.Fatalf("answer = %d %q", recorder.Code, recorder.Body.String())
			}
		})
	}

	// A queue that was read and is empty is the list `[]`, never `null`.
	panel := newAgentPanel(t, &mailQueueTestAgent{result: core.PostfixQueueResult{Installed: true}})
	recorder := httptest.NewRecorder()
	panel.handlePostfixQueue(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/postfix/queue", nil))
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != "[]" {
		t.Fatalf("an empty queue = %d %q", recorder.Code, recorder.Body.String())
	}
}

// --- the mail policy: written, not reloaded -------------------------------------

// The defect: a Postfix reload that failed after main.cf was written was logged
// and the save was answered as a success.
func TestMailPolicyWrittenButNotReloadedIsAVerifiedFailureAfterAChange(t *testing.T) {
	agent := &mailPolicyTestAgent{set: transport.MailPolicyResponse{
		Code: transport.MailPolicyNotReloaded, Error: "x",
		Reason: "Job for postfix.service failed because the control process exited with error code.",
		Policy: transport.MailPolicy{MessageSizeMB: 50, Version: "mp1-next"},
	}}
	panel := newMailPolicyTestPanel(t, agent)
	recorder := httptest.NewRecorder()
	panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodPut, `{"message_size_mb":50,"version":"mp1-abc"}`))
	body := decodeAPIError(t, recorder)
	if recorder.Code != http.StatusBadGateway || body.Code != errCodeMailPolicyNotReloaded {
		t.Fatalf("answer = %d %+v", recorder.Code, body)
	}
	// The change did happen; the answer says so in its proof-bearing field.
	if !body.MutationApplied || !body.PartialSuccess {
		t.Fatalf("the answer does not say main.cf was changed: %+v", body)
	}
	for _, part := range []string{"saved to /etc/postfix/main.cf", "could not be reloaded", "still running with the previous settings",
		"Nothing was rolled back", "server owner", "sudo postfix check", "sudo systemctl reload postfix", "Reload this page"} {
		if !strings.Contains(body.Error, part) {
			t.Errorf("guidance lacks %q: %q", part, body.Error)
		}
	}
	if body.Vars["detail"] != "Job for postfix.service failed because the control process exited with error code." {
		t.Fatalf("vars = %v", body.Vars)
	}
}

// --- scheduled tasks: the same task on two lines ---------------------------------

func TestCronJobThatStandsTwiceIsATypedRefusal(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeCronAgentError(recorder, rpc.ServerError(transport.CronJobAmbiguous), cronReasonWrite)
	body := decodeAPIError(t, recorder)
	if recorder.Code != http.StatusConflict || body.Code != errCodeCronJobAmbiguous {
		t.Fatalf("answer = %d %+v", recorder.Code, body)
	}
	for _, part := range []string{"stands twice", "changed nothing", "server owner", "crontab -u", "reloads this list"} {
		if !strings.Contains(body.Error, part) {
			t.Errorf("guidance lacks %q: %q", part, body.Error)
		}
	}
}
