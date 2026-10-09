package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The rule that decides `409 server_setup_busy` (11 Oct 2026).
//
// Measured on Debian 13 and Ubuntu 24.04: a setup that only waits for a public
// DNS record refused a Stop and a Start on the Services page, for the moments
// in which its runner asked the resolvers again. Such a wait can last until
// the owner's record exists; the services on the host stay the owner's to
// start and stop meanwhile. A step that changes the host keeps the refusal.
//
// `409 server_setup_busy` kararını veren kural. Yalnızca genel bir DNS kaydını
// bekleyen kurulum, Hizmetler sayfasındaki işlemleri reddetmez; sunucuyu
// değiştiren bir adım reddetmeyi sürdürür.

func insertSetupExecutionForBusyRule(t *testing.T, f serviceOperationTestFixture, id, status string, steps []serverSetupExecutionStep) {
	t.Helper()
	execution := serverSetupExecution{ID: id, RequestID: id, PlanID: "plan-" + id, Status: status, Phase: "access_dns", Steps: steps}
	raw, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`DELETE FROM server_setup_executions`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO NOTHING`, execution.PlanID, 1, `{}`, f.userID, "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		execution.ID, execution.RequestID, execution.PlanID, execution.Status, string(raw), "now", "now"); err != nil {
		t.Fatal(err)
	}
}

func busyRuleStep(id, kind, status string) serverSetupExecutionStep {
	return serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{ID: id, Kind: kind}, Status: status}
}

func TestServerSetupBusyRule(t *testing.T) {
	f, _ := setupOperationFixture(t)
	done := []serverSetupExecutionStep{
		busyRuleStep("01-dns", "dns", "succeeded"),
		busyRuleStep("02-service", "service", "succeeded"),
	}
	with := func(more ...serverSetupExecutionStep) []serverSetupExecutionStep {
		return append(append([]serverSetupExecutionStep{}, done...), more...)
	}
	for _, c := range []struct {
		name     string
		status   string
		steps    []serverSetupExecutionStep
		mutating bool
	}{
		{
			// The measured case: the runner claimed the waiting row to ask
			// the public resolvers again.
			name: "the public address is being checked again", status: "running",
			steps: with(busyRuleStep("03-access_dns", "access_dns", "running"),
				busyRuleStep("04-panel_certificate", "panel_certificate", "pending"),
				busyRuleStep("05-verify", "verify", "pending")),
		},
		{
			name: "the public address check runs for the first time", status: "running",
			steps: with(busyRuleStep("03-access_dns", "access_dns", "pending"),
				busyRuleStep("04-panel_certificate", "panel_certificate", "pending")),
		},
		{
			name: "waiting for the owner's DNS record", status: "waiting",
			steps: with(busyRuleStep("03-access_dns", "access_dns", "running")),
		},
		{
			name: "a service is being installed", status: "running", mutating: true,
			steps: with(busyRuleStep("03-service", "service", "running"),
				busyRuleStep("04-access_dns", "access_dns", "pending")),
		},
		{
			// The address check succeeded and the next step is the one at work.
			name: "the certificate step after the address check", status: "running", mutating: true,
			steps: with(busyRuleStep("03-access_dns", "access_dns", "succeeded"),
				busyRuleStep("04-panel_certificate", "panel_certificate", "pending")),
		},
		{
			name: "the native DNS records are being published again", status: "running", mutating: true,
			steps: with(busyRuleStep("03-infrastructure_dns", "infrastructure_dns", "running")),
		},
		{
			name: "every step succeeded and the result is being recorded", status: "running", mutating: true,
			steps: with(busyRuleStep("03-verify", "verify", "pending")),
		},
		{name: "a running row without steps", status: "running", mutating: true, steps: nil},
	} {
		insertSetupExecutionForBusyRule(t, f, strings.Repeat("c", 32), c.status, c.steps)
		mutating, err := f.panel.serverSetupExecutionMutating(context.Background())
		if err != nil || mutating != c.mutating {
			t.Fatalf("%s: mutating=%t err=%v, want %t", c.name, mutating, err, c.mutating)
		}
		// What the Services page gets: the refusal exactly while the setup
		// holds a change, and never from the setup otherwise.
		recorder := httptest.NewRecorder()
		release, refused := f.panel.beginServiceMutation(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/service/action", nil))
		if release != nil {
			release()
		}
		busy := refused && strings.Contains(recorder.Body.String(), `"server_setup_busy"`)
		if busy != c.mutating {
			t.Fatalf("%s: refused=%t status=%d body=%s, want server_setup_busy=%t", c.name, refused, recorder.Code, recorder.Body.String(), c.mutating)
		}
		if c.mutating && recorder.Code != http.StatusConflict {
			t.Fatalf("%s: status %d", c.name, recorder.Code)
		}
	}

	// A running row that cannot be read as an execution keeps the refusal.
	if _, err := f.database.GetDB().Exec(`UPDATE server_setup_executions SET status='running', execution_json='{not json'`); err != nil {
		t.Fatal(err)
	}
	if mutating, err := f.panel.serverSetupExecutionMutating(context.Background()); err != nil || !mutating {
		t.Fatalf("unreadable row: mutating=%t err=%v", mutating, err)
	}
	if serverSetupOnlyChecksPublicAddress(`{"steps":[]}`) || serverSetupOnlyChecksPublicAddress(``) {
		t.Fatal("an execution without steps was exempted")
	}
}
