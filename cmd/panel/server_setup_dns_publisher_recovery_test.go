package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerSetupPublisherRoleDenialBeforeStorageOrRemoteCalls(t *testing.T) {
	p := &Panel{}
	for _, role := range []string{roleReseller, roleCustomer, "additional"} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/setup/publisher", strings.NewReader(`{}`))
		r = r.WithContext(context.WithValue(r.Context(), callerKey, &Caller{ID: 1, Role: role}))
		w := httptest.NewRecorder()
		p.handleServerSetupPublisher(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s allowed publisher binding: %d", role, w.Code)
		}
	}
}

func TestServerSetupPublisherRecoveryRevisesOnlyCompletedBootstrap(t *testing.T) {
	for _, scenario := range []string{"revoked", "prior_pending", "later_admitted", "gate_operation"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSecondaryHostingFixture(t)
			f.wait(t)
			if response := f.bind(t, f.id); response.Code != http.StatusAccepted {
				t.Fatal(response.Body.String())
			}
			_, err := f.f.database.GetDB().Exec(`UPDATE remote_dns_connections SET status='revoked' WHERE id=?`, f.id)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "prior_pending":
				f.execution.Steps[0].Status = "pending"
			case "later_admitted":
				f.execution.Steps[len(f.execution.Steps)-1].Status = "running"
			case "gate_operation":
				for index := range f.execution.Steps {
					if f.execution.Steps[index].Kind == "dns_publisher" {
						f.execution.Steps[index].OperationID = strings.Repeat("f", 32)
					}
				}
			}
			if err := f.f.panel.persistServerSetupExecution(context.Background(), f.execution); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"revision": f.plan.Revision, "execution_id": f.execution.ID})
			response := httptest.NewRecorder()
			f.f.panel.handleServerSetupRevise(response, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/setup/revise", string(body), f.f.userID))
			want := http.StatusConflict
			if scenario == "revoked" {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Fatalf("revision status %d %s", response.Code, response.Body.String())
			}
			if scenario == "revoked" {
				state, err := f.f.panel.loadServerSetup(context.Background())
				if err != nil || state.Revision != f.plan.Revision+1 || state.Status != "draft" {
					t.Fatalf("revision state %+v %v", state, err)
				}
				if err := f.f.panel.persistServerSetupExecution(context.Background(), f.execution); err == nil {
					t.Fatal("stale runner revived superseded execution")
				}
				if plan, err := f.f.panel.loadServerSetupPlan(context.Background(), f.plan.ID); err != nil || plan.ID != f.plan.ID {
					t.Fatal("revision changed reviewed plan")
				}
			}
			if f.agent.switchCalls != 1 {
				t.Fatal("revision reran DNS setup")
			}
		})
	}
}

func TestServerSetupPrimaryReadinessWaitPreservesSetupInsteadOfFailingMail(t *testing.T) {
	f, state := setupOperationFixture(t)
	state.Draft = secondaryHostingDraft()
	state.Draft.Purpose, state.Draft.DNSRole, state.Draft.PeerNS = "web", "primary", state.Draft.NS2
	state.Draft.DNSPublisherEndpoint = ""
	var err error
	state, err = f.panel.saveServerSetupDraft(context.Background(), state.Revision, state.Draft)
	if err != nil {
		t.Fatal(err)
	}
	plan := saveSetupPlanForTest(t, f, state)
	serverSetupRunners.Store(f.panel, true)
	t.Cleanup(func() { serverSetupRunners.Delete(f.panel) })
	response := postSetupStartForTest(t, f, plan.ID, strings.Repeat("a", 32))
	if response.Code != http.StatusAccepted {
		t.Fatal(response.Body.String())
	}
	var execution serverSetupExecution
	if json.Unmarshal(response.Body.Bytes(), &execution) != nil {
		t.Fatal("invalid setup response")
	}
	for index := range execution.Steps {
		if execution.Steps[index].Kind == "dns_readiness" {
			break
		}
		execution.Steps[index].Status = "succeeded"
	}
	progressed, err := f.panel.advanceServerSetupExecution(plan, &execution)
	if err != nil || progressed || execution.Status != "waiting" || execution.Phase != "dns_readiness" {
		t.Fatalf("pair wait lost durable setup: %v %v %+v", progressed, err, execution)
	}
	if !serverSetupExecutionCanRevise(plan, execution) {
		t.Fatal("unadmitted host work prevents safe plan correction")
	}
	if f.agent.installCalls.Load() != 0 {
		t.Fatal("waiting for secondary admitted hosting")
	}
}
