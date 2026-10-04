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
	reply.Jobs = []transport.CronJob{}
	return a.err
}

func (a *cronMutationTestAgent) UpdateCronJob(_ *transport.UpdateCronJobRequest, reply *bool) error {
	*reply = a.success
	return a.err
}

func (a *cronMutationTestAgent) DeleteCronJob(_ *transport.DeleteCronJobRequest, reply *bool) error {
	*reply = a.success
	return a.err
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
			p.handleAddCronJob(w, httptest.NewRequest(http.MethodPost, "/api/v1/domains/34/cron", strings.NewReader(`{"schedule":"0 3 * * *","command":"true"}`)), testCronTenant)
		}},
		{"update", cronReasonWrite, func(p *Panel, w *httptest.ResponseRecorder) {
			p.handleUpdateCronJob(w, httptest.NewRequest(http.MethodPut, "/api/v1/domains/34/cron", strings.NewReader(`{"id":"0000abcd","schedule":"0 3 * * *","command":"true","enabled":true}`)), testCronTenant)
		}},
		{"delete", cronReasonWrite, func(p *Panel, w *httptest.ResponseRecorder) {
			p.handleDeleteCronJob(w, httptest.NewRequest(http.MethodDelete, "/api/v1/domains/34/cron?id=0000abcd", nil), testCronTenant)
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
