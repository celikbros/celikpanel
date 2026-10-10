package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func decodeServiceActionAnswer(t *testing.T, recorder *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("answer is not JSON: %v: %s", err, recorder.Body.String())
	}
	return body
}

// The Agent reports that Postfix refused its configuration on "Reload". The
// screen gets a verified failure with the stage, Postfix's own line and the
// command that shows it; the answer used to be 500 "internal server error".
func TestServiceActionVerifiedFailureIsAnsweredWithItsStageAndTheServicesOwnLine(t *testing.T) {
	recorder := httptest.NewRecorder()
	reply := &transport.ServiceActionResult{
		Error:   "Postfix refuses its configuration: postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised",
		Outcome: transport.ServiceActionFailed, Stage: "check",
		Detail: "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised",
	}
	if !writeServiceActionOutcome(recorder, "postfix", "reload", reply) {
		t.Fatal("a classified failure was not answered")
	}
	body := decodeServiceActionAnswer(t, recorder)
	if recorder.Code != http.StatusBadGateway || body.Code != errCodeServiceActionFailed || body.Reason != "check" {
		t.Fatalf("status %d, body %+v", recorder.Code, body)
	}
	if body.Vars["unit"] != "postfix" || body.Vars["action"] != "reload" || body.Vars["command"] != "sudo postfix check" ||
		!strings.Contains(body.Vars["detail"], "default_process_limit") {
		t.Fatalf("vars = %v", body.Vars)
	}
	if body.MutationApplied || body.PartialSuccess {
		t.Fatalf("a failure was marked as applied: %+v", body)
	}
	// The sentence is the Panel's own: reason, who acts, the next step, and
	// that nothing repeats by itself. The service's line is not folded into it.
	for _, fragment := range []string{"Nothing was changed", "server owner", "command shown", "repeats this action", "nothing repeats it automatically"} {
		if !strings.Contains(body.Error, fragment) {
			t.Fatalf("sentence lacks %q: %s", fragment, body.Error)
		}
	}
	if strings.Contains(body.Error, "default_process_limit") {
		t.Fatalf("the service's output is in the sentence: %s", body.Error)
	}
}

// Every stage the Agent names has its own sentence; one it does not know gets
// the general one and no reason.
func TestServiceActionFailureSentences(t *testing.T) {
	for _, stage := range []string{"check", "reload", "start", "stop", "verify", "command"} {
		recorder := httptest.NewRecorder()
		writeServiceActionOutcome(recorder, "dovecot", "restart", &transport.ServiceActionResult{
			Error: "x", Outcome: transport.ServiceActionFailed, Stage: stage,
		})
		body := decodeServiceActionAnswer(t, recorder)
		if body.Reason != stage || body.Error == serviceActionFailedMessage || !strings.HasSuffix(body.Error, serviceActionResume) {
			t.Fatalf("stage %s: %+v", stage, body)
		}
		if body.Vars["command"] != "sudo systemctl status dovecot" {
			t.Fatalf("stage %s: command %q", stage, body.Vars["command"])
		}
	}
	recorder := httptest.NewRecorder()
	writeServiceActionOutcome(recorder, "dovecot", "restart", &transport.ServiceActionResult{
		Error: "x", Outcome: transport.ServiceActionFailed, Stage: "a-later-stage",
	})
	if body := decodeServiceActionAnswer(t, recorder); body.Reason != "" || body.Error != serviceActionFailedMessage {
		t.Fatalf("unknown stage: %+v", body)
	}
}

// Unknown is its own answer: not a verified failure, never a success. For a
// wrapper unit the command names the unit that owns the daemon.
func TestServiceActionUnknownOutcomeIsNotAFailureAndNotASuccess(t *testing.T) {
	recorder := httptest.NewRecorder()
	reply := &transport.ServiceActionResult{
		Error:   "systemctl restart postgresql reported success, but postgresql.service only groups other units and the result could not be verified",
		Outcome: transport.ServiceActionUnknown, Stage: "verify", Unit: "postgresql@17-main.service",
		Detail: "postgresql@17-main.service could not be read: Failed to get properties: Connection timed out",
	}
	if !writeServiceActionOutcome(recorder, "postgresql", "restart", reply) {
		t.Fatal("an unknown outcome was not answered")
	}
	body := decodeServiceActionAnswer(t, recorder)
	if recorder.Code != http.StatusBadGateway || body.Code != errCodeServiceActionUnknown || body.Reason != "" {
		t.Fatalf("status %d, body %+v", recorder.Code, body)
	}
	if body.Vars["owner_unit"] != "postgresql@17-main.service" || body.Vars["command"] != "sudo systemctl status postgresql@17-main.service" {
		t.Fatalf("vars = %v", body.Vars)
	}
	for _, fragment := range []string{"could not be verified", "not reported as done", "not a verified failure", "server owner", "only if it is still needed"} {
		if !strings.Contains(body.Error, fragment) {
			t.Fatalf("sentence lacks %q: %s", fragment, body.Error)
		}
	}
}

func TestServiceActionOutcomeLeavesWhatTheAgentDidNotClassify(t *testing.T) {
	for name, reply := range map[string]*transport.ServiceActionResult{
		"nil":           nil,
		"success":       {Success: true, Outcome: transport.ServiceActionVerified},
		"older Agent":   {Error: "exit status 1: Job for nginx.service failed"},
		"other outcome": {Error: "x", Outcome: "something-else"},
		"no error":      {Outcome: transport.ServiceActionFailed},
	} {
		recorder := httptest.NewRecorder()
		if writeServiceActionOutcome(recorder, "nginx", "restart", reply) || recorder.Body.Len() != 0 {
			t.Fatalf("%s: an unclassified answer was written: %s", name, recorder.Body.String())
		}
	}
}

// A unit name that is not one is not put into a command the owner is told to
// run; the acted unit is used instead.
func TestServiceActionCommandNamesOnlyAUnit(t *testing.T) {
	reply := &transport.ServiceActionResult{Error: "x", Outcome: transport.ServiceActionFailed, Stage: "start", Unit: "postgresql@17-main.service; rm -rf /"}
	recorder := httptest.NewRecorder()
	writeServiceActionOutcome(recorder, "postgresql", "start", reply)
	body := decodeServiceActionAnswer(t, recorder)
	if body.Vars["command"] != "sudo systemctl status postgresql" || body.Vars["owner_unit"] != "" {
		t.Fatalf("vars = %v", body.Vars)
	}
	for action, want := range map[string]string{"reload": "sudo postfix reload", "restart": "sudo postfix status", "stop": "sudo postfix status"} {
		if got := serviceActionCommand("postfix", action, &transport.ServiceActionResult{Stage: "verify"}); got != want {
			t.Fatalf("postfix %s: %q, want %q", action, got, want)
		}
	}
}

// A failed reload says only what was verified (11 Oct 2026). Measured on
// Debian 13 and Ubuntu 24.04: "keeps running with the settings it had" was
// answered for a PostgreSQL that had re-read its files and for a Postfix and a
// Dovecot that were not running. The plain "reload" sentence claims neither
// state; the PostgreSQL stages repeat what the server answered; a stopped
// service is an unmet prerequisite with its own status and no command to
// "see why".
func TestFailedReloadSaysOnlyWhatWasVerified(t *testing.T) {
	answer := func(unit, stage string) (int, apiErrorBody) {
		recorder := httptest.NewRecorder()
		if !writeServiceActionOutcome(recorder, unit, "reload", &transport.ServiceActionResult{
			Error: "x", Outcome: transport.ServiceActionFailed, Stage: stage, Detail: "one line",
		}) {
			t.Fatalf("%s %s was not answered", unit, stage)
		}
		return recorder.Code, decodeServiceActionAnswer(t, recorder)
	}

	status, body := answer("dovecot", "reload")
	if status != http.StatusBadGateway || body.Reason != "reload" {
		t.Fatalf("status %d, body %+v", status, body)
	}
	lower := strings.ToLower(body.Error)
	for _, claim := range []string{"keeps running", "is running with the settings it had", "are in effect"} {
		if strings.Contains(lower, claim) {
			t.Fatalf("the plain reload sentence claims a state nobody read (%q): %s", claim, body.Error)
		}
	}
	for _, fragment := range []string{"reported that the reload failed", "cannot read", "says neither", "server owner"} {
		if !strings.Contains(body.Error, fragment) {
			t.Fatalf("sentence lacks %q: %s", fragment, body.Error)
		}
	}

	status, body = answer("postgresql", transport.ServiceActionStageReloadReread)
	if status != http.StatusBadGateway || body.Code != errCodeServiceActionFailed || body.Reason != "reload_reread" {
		t.Fatalf("status %d, body %+v", status, body)
	}
	for _, fragment := range []string{"reported the reload as failed", "PostgreSQL itself re-read", "in effect now", "need a restart", "does not need to be repeated"} {
		if !strings.Contains(body.Error, fragment) {
			t.Fatalf("sentence lacks %q: %s", fragment, body.Error)
		}
	}
	if strings.Contains(body.Error, "settings it had") {
		t.Fatalf("a server that re-read its files is said to keep its settings: %s", body.Error)
	}

	status, body = answer("postgresql", transport.ServiceActionStageReloadNotReread)
	if status != http.StatusBadGateway || body.Reason != "reload_not_reread" ||
		!strings.Contains(body.Error, "did not re-read") || !strings.Contains(body.Error, "settings it had before") {
		t.Fatalf("status %d, body %+v", status, body)
	}

	// Not running: 409, nothing to look up, Start is the action that applies.
	for unit, command := range map[string]string{"postfix": "sudo postfix status", "dovecot": "sudo systemctl status dovecot"} {
		status, body = answer(unit, transport.ServiceActionStageNotRunning)
		if status != http.StatusConflict || body.Code != errCodeServiceActionFailed || body.Reason != "not_running" {
			t.Fatalf("%s: status %d, body %+v", unit, status, body)
		}
		if body.Vars["command"] != command {
			t.Fatalf("%s: command %q, want %q", unit, body.Vars["command"], command)
		}
		for _, fragment := range []string{"is not running", "nothing to reload", "nothing was changed", "Start"} {
			if !strings.Contains(body.Error, fragment) {
				t.Fatalf("%s: sentence lacks %q: %s", unit, fragment, body.Error)
			}
		}
		if strings.Contains(strings.ToLower(body.Error), "keeps running") || body.MutationApplied {
			t.Fatalf("%s: %+v", unit, body)
		}
	}

	// No sentence of a service action says "keeps running with the settings
	// it had" any more.
	for stage, sentence := range serviceActionFailedMessages {
		if strings.Contains(sentence, "keeps running with the settings it had") {
			t.Fatalf("stage %s still claims it: %s", stage, sentence)
		}
	}
}
