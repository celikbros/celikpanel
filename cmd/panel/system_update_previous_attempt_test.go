package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

// The check's previous_attempt is additive owner guidance read from the native
// recovery observations. It never changes availability or the offered target.
func TestPanelUpdateCheckDescribesAPreviouslyFailedTargetOnlyWhenExact(t *testing.T) {
	withSystemUpdateBuild(t)
	attemptID := strings.Repeat("a", 32)
	cases := []struct {
		name    string
		attempt recoveryobs.Attempt
		ok      bool
		want    string
	}{
		{name: "no observation", ok: false},
		{name: "recovered with code", ok: true,
			attempt: recoveryobs.Attempt{RequestID: attemptID, Phase: "recovered", FailureCode: "candidate_panel_startup_check_failed", FinishedAt: "2026-09-30T15:53:24Z"},
			want:    `{"request_id":"` + attemptID + `","phase":"recovered","failure_code":"candidate_panel_startup_check_failed","finished_at":"2026-09-30T15:53:24Z"}`},
		{name: "failed without code", ok: true,
			attempt: recoveryobs.Attempt{RequestID: attemptID, Phase: "failed", FinishedAt: "2026-09-30T15:53:24Z"},
			want:    `{"request_id":"` + attemptID + `","phase":"failed","finished_at":"2026-09-30T15:53:24Z"}`},
		{name: "in progress", ok: true, attempt: recoveryobs.Attempt{RequestID: attemptID, Phase: "recovering", FinishedAt: "2026-09-30T15:53:24Z"}},
		{name: "unknown code", ok: true, attempt: recoveryobs.Attempt{RequestID: attemptID, Phase: "recovered", FailureCode: "private detail", FinishedAt: "2026-09-30T15:53:24Z"}},
		{name: "malformed id", ok: true, attempt: recoveryobs.Attempt{RequestID: "../x", Phase: "recovered", FinishedAt: "2026-09-30T15:53:24Z"}},
		{name: "malformed time", ok: true, attempt: recoveryobs.Attempt{RequestID: attemptID, Phase: "recovered", FinishedAt: "2026-09-30 15:53:24"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSystemUpdateTestFixture(t)
			var asked []string
			fixture.panel.lastUpdateAttempt = func(commit string) (recoveryobs.Attempt, bool) {
				asked = append(asked, commit)
				return test.attempt, test.ok
			}
			recorder := httptest.NewRecorder()
			fixture.panel.handlePanelUpdateCheck(recorder, systemUpdateRequest(http.MethodGet, panelUpdateCheckPath, "", roleAdmin))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if len(asked) != 1 || asked[0] != updateTestTargetCommit {
				t.Fatalf("observations were not read for the exact offered commit: %v", asked)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if string(body["available"]) != "true" || body["target"] == nil {
				t.Fatalf("the offer changed: %s", recorder.Body.String())
			}
			got, present := body["previous_attempt"]
			if test.want == "" {
				if present {
					t.Fatalf("previous_attempt must be absent: %s", recorder.Body.String())
				}
				return
			}
			if string(got) != test.want {
				t.Fatalf("previous_attempt=%s want %s", got, test.want)
			}
			fixture.agent.mu.Lock()
			defer fixture.agent.mu.Unlock()
			if fixture.agent.startCalls != 0 || fixture.agent.abandonCalls != 0 || fixture.agent.statusCalls != 0 {
				t.Fatal("reading the previous attempt reached a mutation or status RPC")
			}
		})
	}
}

func TestPanelUpdateCheckWithoutAnOfferReadsNoAttempt(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	fixture.agent.check.Available = false
	fixture.agent.check.TargetVersion, fixture.agent.check.TargetCommit = "", ""
	fixture.panel.lastUpdateAttempt = func(string) (recoveryobs.Attempt, bool) {
		t.Fatal("no offered target, but observations were read")
		return recoveryobs.Attempt{}, false
	}
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateCheck(recorder, systemUpdateRequest(http.MethodGet, panelUpdateCheckPath, "", roleAdmin))
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "previous_attempt") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
