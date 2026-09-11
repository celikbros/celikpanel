package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestServerSetupDNSUnprovenChildKeepsOuterExecutionAndDraftLocked(t *testing.T) {
	f, state := setupOperationFixture(t)
	draft := setupDNSTestDraft()
	draft.PanelDomain = "panel.example.test"
	var err error
	state, err = f.panel.saveServerSetupDraft(context.Background(), state.Revision, draft)
	if err != nil {
		t.Fatal(err)
	}
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackIdentityMismatch
	attachDNSEngineTestAgent(t, f.panel, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	plan := serverSetupPlan{Version: serverSetupPlanVersion, Revision: state.Revision, Draft: state.Draft, Purpose: state.Draft.Purpose, Actor: serviceOperationActor{UserID: f.userID}, Steps: []serverSetupPlanStep{{ID: "01-dns", Kind: "dns", Target: "local"}, {ID: "02-verify", Kind: "verify", Target: state.Draft.Purpose}}}
	plan.ID = serverSetupPlanIdentity(plan)
	raw, _ := json.Marshal(plan)
	if _, err = f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(raw), f.userID, "now"); err != nil {
		t.Fatal(err)
	}
	execution := serverSetupExecution{ID: strings.Repeat("9", 32), RequestID: strings.Repeat("9", 32), PlanID: plan.ID, Status: "running"}
	for _, step := range plan.Steps {
		execution.Steps = append(execution.Steps, serverSetupExecutionStep{serverSetupPlanStep: step, Status: "pending", RequestID: serverSetupID(execution.ID, step.ID, "request"), OwnerID: serverSetupID(execution.ID, step.ID, "owner")})
	}
	raw, _ = json.Marshal(execution)
	if _, err = f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,'now','now')`, execution.ID, execution.RequestID, plan.ID, execution.Status, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.database.GetDB().Exec(`UPDATE server_setup_state SET status='running' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.panel.advanceServerSetupExecution(plan, &execution); err != nil {
		t.Fatal(err)
	}
	if execution.Status != "running" || execution.Steps[0].Status != "running" || execution.Error == nil || execution.Error.Code != "server_setup_reconciling" {
		t.Fatalf("uncertain DNS child released outer execution: %+v", execution)
	}
	if _, err = f.panel.saveServerSetupDraft(context.Background(), state.Revision, draft); !errors.Is(err, errServerSetupConflict) {
		t.Fatalf("uncertain child allowed revised plan: %v", err)
	}
	agent.mu.Lock()
	agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackSafe
	agent.mu.Unlock()
	if _, err = f.panel.advanceServerSetupExecution(plan, &execution); err != nil {
		t.Fatal(err)
	}
	if execution.Status != "failed" || execution.Steps[0].Status != "failed" {
		t.Fatalf("proven rollback did not release terminal plan: %+v", execution)
	}
	if _, err = f.panel.saveServerSetupDraft(context.Background(), state.Revision, draft); err != nil {
		t.Fatalf("proven rollback still blocks a new review: %v", err)
	}
	dnsState, err := readDNSEngineDBState(context.Background(), f.database.GetDB())
	if err != nil || dnsState.CurrentSwitchID != "" {
		t.Fatalf("outer failed before exact DNS rollback: %+v %v", dnsState, err)
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("reconciliation repeated host mutation: %d", calls)
	}
}
