package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func (a *cronMutationTestAgent) ListCronJobs(_ *transport.ListCronJobsRequest, reply *transport.ListCronJobsResponse) error {
	a.calls++
	reply.Jobs = []transport.CronJob{}
	reply.Version = a.listVersion
	return a.err
}

func (a *cronMutationTestAgent) UpdateCronJob(req *transport.UpdateCronJobRequest, reply *bool) error {
	a.calls++
	a.receivedVersion = req.Version
	*reply = a.success
	return a.err
}

func (a *cronMutationTestAgent) DeleteCronJob(req *transport.DeleteCronJobRequest, reply *bool) error {
	a.calls++
	a.receivedVersion = req.Version
	*reply = a.success
	return a.err
}

type cronHandlerCall struct {
	name string
	call func(*Panel, *httptest.ResponseRecorder, string)
}

// cronChangeCalls are the three cron writes; version is what the page sends.
func cronChangeCalls() []cronHandlerCall {
	body := func(fields, version string) *strings.Reader {
		if version != "" {
			fields += `,"version":"` + version + `"`
		}
		return strings.NewReader("{" + fields + "}")
	}
	return []cronHandlerCall{
		{"create", func(p *Panel, w *httptest.ResponseRecorder, version string) {
			p.handleAddCronJob(w, httptest.NewRequest(http.MethodPost, "/api/v1/domains/34/cron", body(`"schedule":"0 3 * * *","command":"true"`, version)), testCronTenant)
		}},
		{"update", func(p *Panel, w *httptest.ResponseRecorder, version string) {
			p.handleUpdateCronJob(w, httptest.NewRequest(http.MethodPut, "/api/v1/domains/34/cron", body(`"id":"0000abcd","schedule":"0 3 * * *","command":"true","enabled":true`, version)), testCronTenant)
		}},
		{"delete", func(p *Panel, w *httptest.ResponseRecorder, version string) {
			target := "/api/v1/domains/34/cron?id=0000abcd"
			if version != "" {
				target += "&version=" + version
			}
			p.handleDeleteCronJob(w, httptest.NewRequest(http.MethodDelete, target, nil), testCronTenant)
		}},
	}
}

func decodeAPIError(t *testing.T, recorder *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body %q: %v", recorder.Body.String(), err)
	}
	return body
}

// A cron change that does not say which crontab it was built from is refused
// by the Panel before the Agent is asked: a page that never loaded the list,
// or an older cached page, cannot write.
func TestCronChangesWithoutAVersionAreRefusedBeforeTheAgent(t *testing.T) {
	for _, tc := range cronChangeCalls() {
		t.Run(tc.name, func(t *testing.T) {
			agent := &cronMutationTestAgent{success: true}
			recorder := httptest.NewRecorder()
			tc.call(newCronMutationTestPanel(t, agent), recorder, "")
			body := decodeAPIError(t, recorder)
			if recorder.Code != http.StatusConflict || body.Code != errCodeSettingsVersionRequired || body.Reason != settingsResourceScheduledTasks {
				t.Fatalf("status/code/reason = %d/%q/%q", recorder.Code, body.Code, body.Reason)
			}
			if agent.calls != 0 {
				t.Fatalf("the Agent was asked %d times", agent.calls)
			}
			for _, part := range []string{"nothing was changed", "Reload the page"} {
				if !strings.Contains(body.Error, part) {
					t.Errorf("guidance lacks %q: %q", part, body.Error)
				}
			}
		})
	}
}

// The version the page sends reaches the Agent, which is the authority.
func TestCronChangesCarryThePageVersionToTheAgent(t *testing.T) {
	for _, tc := range cronChangeCalls() {
		t.Run(tc.name, func(t *testing.T) {
			agent := &cronMutationTestAgent{success: true}
			recorder := httptest.NewRecorder()
			tc.call(newCronMutationTestPanel(t, agent), recorder, "ct1-abc")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d body=%q", recorder.Code, recorder.Body.String())
			}
			version := agent.receivedVersion
			if tc.name == "create" {
				version = agent.received.Version
			}
			if version != "ct1-abc" {
				t.Fatalf("the Agent received version %q", version)
			}
		})
	}
}

// Each Agent refusal that protects the owner's crontab has its typed answer;
// the Agent's own line is never the message.
func TestCronHandlersAnswerCrontabProtectionRefusals(t *testing.T) {
	refusals := []struct {
		agent  string
		status int
		code   string
		reason string
		says   []string
	}{
		{transport.CronStateUnreadable, http.StatusBadGateway, errCodeCurrentSettingsUnreadable, settingsResourceScheduledTasks, []string{"could not read", "nothing was changed", "Reload the page"}},
		{transport.CronVersionRequired, http.StatusConflict, errCodeSettingsVersionRequired, settingsResourceScheduledTasks, []string{"nothing was changed", "Reload the page"}},
		{transport.CronStateChanged, http.StatusConflict, errCodeSettingsChanged, settingsResourceScheduledTasks, []string{"changed on the server since this page loaded", "nothing was changed", "Reload the page"}},
		{transport.CronJobDuplicate, http.StatusConflict, errCodeCronJobDuplicate, "", []string{"already exists", "nothing was added"}},
	}
	for _, refusal := range refusals {
		for _, tc := range cronChangeCalls() {
			t.Run(refusal.code+" on "+tc.name, func(t *testing.T) {
				panel := newCronMutationTestPanel(t, &cronMutationTestAgent{err: errors.New(refusal.agent)})
				recorder := httptest.NewRecorder()
				tc.call(panel, recorder, "ct1-abc")
				body := decodeAPIError(t, recorder)
				if recorder.Code != refusal.status || body.Code != refusal.code || body.Reason != refusal.reason {
					t.Fatalf("status/code/reason = %d/%q/%q, want %d/%q/%q", recorder.Code, body.Code, body.Reason, refusal.status, refusal.code, refusal.reason)
				}
				if strings.Contains(body.Error, refusal.agent) || body.MutationApplied || body.PartialSuccess {
					t.Fatalf("unexpected answer: %+v", body)
				}
				for _, part := range refusal.says {
					if !strings.Contains(body.Error, part) {
						t.Errorf("guidance lacks %q: %q", part, body.Error)
					}
				}
			})
		}
	}
}

// A crontab the Agent could not read is not listed as "no tasks".
func TestCronListReportsAnUnreadableCrontabAndCarriesTheVersion(t *testing.T) {
	panel := newCronMutationTestPanel(t, &cronMutationTestAgent{err: errors.New(transport.CronStateUnreadable)})
	recorder := httptest.NewRecorder()
	panel.handleListCronJobs(recorder, testCronTenant)
	body := decodeAPIError(t, recorder)
	if recorder.Code != http.StatusBadGateway || body.Code != errCodeCurrentSettingsUnreadable {
		t.Fatalf("status/code = %d/%q", recorder.Code, body.Code)
	}
	if strings.Contains(recorder.Body.String(), `"jobs"`) {
		t.Fatalf("an unreadable crontab still answered a list: %q", recorder.Body.String())
	}

	panel = newCronMutationTestPanel(t, &cronMutationTestAgent{listVersion: "ct1-abc"})
	recorder = httptest.NewRecorder()
	panel.handleListCronJobs(recorder, testCronTenant)
	var list struct {
		Jobs    []transport.CronJob `json:"jobs"`
		Version string              `json:"version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || list.Jobs == nil || list.Version != "ct1-abc" {
		t.Fatalf("list = %d %q", recorder.Code, recorder.Body.String())
	}
}

// upd1 (1 Oct 2026): a fresh web_mail server had no cron, the Agent said so,
// and the Panel answered 500 INTERNAL. Every cron request must now answer the
// known condition with its typed code, the reason for the request kind and the
// owner's next action, without echoing the Agent's line.
func TestCronHandlersAnswerCronNotInstalledWithTypedGuidance(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		call   func(*Panel, *httptest.ResponseRecorder)
	}{
		{"list", cronReasonRead, func(p *Panel, w *httptest.ResponseRecorder) {
			p.handleListCronJobs(w, testCronTenant)
		}},
		{"create", cronReasonWrite, func(p *Panel, w *httptest.ResponseRecorder) {
			p.handleAddCronJob(w, httptest.NewRequest(http.MethodPost, "/api/v1/domains/34/cron", strings.NewReader(`{"schedule":"0 3 * * *","command":"true","version":"ct1-test"}`)), testCronTenant)
		}},
		{"update", cronReasonWrite, func(p *Panel, w *httptest.ResponseRecorder) {
			p.handleUpdateCronJob(w, httptest.NewRequest(http.MethodPut, "/api/v1/domains/34/cron", strings.NewReader(`{"id":"0000abcd","schedule":"0 3 * * *","command":"true","enabled":true,"version":"ct1-test"}`)), testCronTenant)
		}},
		{"delete", cronReasonWrite, func(p *Panel, w *httptest.ResponseRecorder) {
			p.handleDeleteCronJob(w, httptest.NewRequest(http.MethodDelete, "/api/v1/domains/34/cron?id=0000abcd&version=ct1-test", nil), testCronTenant)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			panel := newCronMutationTestPanel(t, &cronMutationTestAgent{err: errors.New(transport.CronNotInstalled)})
			recorder := httptest.NewRecorder()
			tc.call(panel, recorder)
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body=%q", recorder.Code, recorder.Body.String())
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != errCodeCronNotInstalled || body.Reason != tc.reason {
				t.Fatalf("code/reason = %q/%q, want %q/%q", body.Code, body.Reason, errCodeCronNotInstalled, tc.reason)
			}
			if strings.Contains(body.Error, transport.CronNotInstalled) {
				t.Fatalf("the Agent line leaked into the answer: %q", body.Error)
			}
			for _, part := range []string{"server owner", "Components", "Scheduled tasks (cron)", "sudo apt-get install cron", "sudo pacman -S cronie", "sudo systemctl enable --now cronie"} {
				if !strings.Contains(body.Error, part) {
					t.Errorf("guidance lacks %q: %q", part, body.Error)
				}
			}
			if tc.reason == cronReasonWrite && !strings.Contains(body.Error, "nothing was changed") {
				t.Errorf("a refused change must say nothing was changed: %q", body.Error)
			}
		})
	}
}

// Only the Agent's exact answer is the known condition. A different Agent
// failure, or the text arriving from somewhere other than the Agent, keeps the
// masked INTERNAL answer and leaks nothing.
func TestCronNotInstalledClassificationIsExact(t *testing.T) {
	if !agentReportedCronNotInstalled(fmt.Errorf("call: %w", rpc.ServerError(transport.CronNotInstalled))) {
		t.Fatal("wrapped Agent answer must classify")
	}
	for _, err := range []error{
		nil,
		errors.New(transport.CronNotInstalled),
		rpc.ServerError("crontab: " + transport.CronNotInstalled),
		rpc.ServerError("look up cron user \"example_com\": unknown user"),
	} {
		if agentReportedCronNotInstalled(err) {
			t.Fatalf("%v must not classify as cron not installed", err)
		}
	}
	panel := newCronMutationTestPanel(t, &cronMutationTestAgent{err: errors.New("crontab: secret detail")})
	recorder := httptest.NewRecorder()
	panel.handleListCronJobs(recorder, testCronTenant)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "secret detail") {
		t.Fatalf("unclassified failure must stay INTERNAL without detail: %d %q", recorder.Code, recorder.Body.String())
	}
}

// The generic uninstall would stop cron and purge its package, ending every
// scheduled job on the server including the owner's own (D-022).
func TestNativeCronUninstallIsRefusedBeforeAnyMutation(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/services/uninstall", strings.NewReader(`{"service_id":"cron"}`))
	(&Panel{}).handleServiceUninstall(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%q", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeNativeCronRemovalRefused || body.MutationApplied || body.PartialSuccess {
		t.Fatalf("unexpected refusal: %+v", body)
	}
}

// Audit of the domain handlers (1 Oct 2026): two fixed Agent "busy" answers
// were masked as INTERNAL. Both now use the existing HOST_MUTATION_BUSY answer
// that already names the actor and the retry in both languages.
func TestAgentBusyAnswersBecomeHostMutationBusy(t *testing.T) {
	for _, text := range []string{transport.MailConfigurationBusy, transport.SiteCertificateBusy} {
		if !agentAnsweredExactly(rpc.ServerError(text), text) || agentAnsweredExactly(rpc.ServerError(text+" (extra)"), text) {
			t.Fatalf("%q must classify exactly", text)
		}
	}
	recorder := httptest.NewRecorder()
	writeServerError(recorder, agentMutationBusy())
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusConflict || body.Code != errCodeHostMutationBusy || body.Reason != transport.HostMutationReasonAgentMutation {
		t.Fatalf("busy answer = %d %+v", recorder.Code, body)
	}
}
