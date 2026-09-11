package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerSetupReviseOnlyReopensFullyAppliedWaitingPlan(t *testing.T) {
	for _, status := range []string{"running", "pending", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			f, state := setupOperationFixture(t)
			plan := saveSetupPlanForTest(t, f, state)
			execution := serverSetupExecution{ID: strings.Repeat("4", 32), RequestID: strings.Repeat("4", 32), PlanID: plan.ID, Status: "waiting", Phase: "verification"}
			for _, step := range plan.Steps {
				execution.Steps = append(execution.Steps, serverSetupExecutionStep{serverSetupPlanStep: step, Status: status, RequestID: serverSetupID(execution.ID, step.ID, "request"), OwnerID: serverSetupID(execution.ID, step.ID, "owner")})
			}
			raw, _ := json.Marshal(execution)
			if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,'waiting',?,'now','now')`, execution.ID, execution.RequestID, plan.ID, string(raw)); err != nil {
				t.Fatal(err)
			}
			request, _ := json.Marshal(map[string]any{"revision": state.Revision, "execution_id": execution.ID})
			w := httptest.NewRecorder()
			f.panel.handleServerSetupRevise(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/setup/revise", string(request), f.userID))
			want := http.StatusConflict
			if status == "succeeded" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("revise: %d %s", w.Code, w.Body.String())
			}
			if status == "succeeded" {
				current, err := f.panel.loadServerSetup(context.Background())
				if err != nil || current.Revision != state.Revision+1 || current.Status != "draft" {
					t.Fatalf("draft revision: %+v %v", current, err)
				}
				if err := f.panel.persistServerSetupExecution(context.Background(), execution); err == nil {
					t.Fatal("late worker resurrected superseded execution")
				}
			}
			if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
				t.Fatal("revising plan changed the host")
			}
		})
	}
}
