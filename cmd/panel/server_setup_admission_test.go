package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServerSetupMissingLicensePausesNewStepsAndPreservesCommittedReceipts(t *testing.T) {
	f, state := setupOperationFixture(t)
	plan := saveSetupPlanForTest(t, f, state)
	serverSetupRunners.Store(f.panel, true)
	t.Cleanup(func() { serverSetupRunners.Delete(f.panel) })
	response := postSetupStartForTest(t, f, plan.ID, strings.Repeat("9", 32))
	if response.Code != http.StatusAccepted {
		t.Fatalf("start %d %s", response.Code, response.Body.String())
	}
	var execution serverSetupExecution
	if err := json.Unmarshal(response.Body.Bytes(), &execution); err != nil {
		t.Fatal(err)
	}
	f.panel.license = testPanelLicense(t, "expired")
	_, err := f.panel.advanceServerSetupExecution(plan, &execution)
	if err != nil || execution.Status != "waiting" || execution.Phase != "license" || execution.Error == nil || execution.Error.Code != "license_required" {
		t.Fatalf("unlicensed execution did not pause: %+v %v", execution, err)
	}
	if len(f.agent.capturedMutationEvents()) != 0 || f.agent.installCalls.Load() != 0 {
		t.Fatal("unlicensed setup mutated host")
	}
	step := serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{ID: "service", Kind: "service", Target: "nginx"}, RequestID: strings.Repeat("7", 32), OwnerID: strings.Repeat("8", 32)}
	if _, err = f.panel.ensureServerSetupChild(context.Background(), plan, step); !errors.Is(err, errServerSetupLicenseRequired) {
		t.Fatalf("new child admission: %v", err)
	}
	op, err := f.panel.createServiceOperationRequest(context.Background(), serviceOperationKindInstall, "nginx", "", step.RequestID, plan.Actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.database.GetDB().Exec(`UPDATE service_operations SET status='succeeded',result_json='{"success":true}' WHERE id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	done, err := f.panel.runServerSetupStep(context.Background(), plan, &step)
	if err != nil || !done {
		t.Fatalf("license expiry blocked existing receipt: %v %v", done, err)
	}
	f.panel.license = testPanelLicense(t, "active")
	_, err = f.panel.advanceServerSetupExecution(plan, &execution)
	if err != nil || execution.Status != "running" || execution.Steps[0].Status != "succeeded" {
		t.Fatalf("restored license did not resume same plan: %+v %v", execution, err)
	}
}

func TestServerSetupRunnerHandsOffOnlyToDifferentDurableRequest(t *testing.T) {
	f, state := setupOperationFixture(t)
	plan := serverSetupPlan{Version: serverSetupPlanVersion, Revision: state.Revision, Draft: state.Draft, Purpose: "web", Actor: serviceOperationActor{UserID: f.userID}, Steps: []serverSetupPlanStep{}}
	plan.ID = serverSetupPlanIdentity(plan)
	raw, _ := json.Marshal(plan)
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(raw), f.userID, "now"); err != nil {
		t.Fatal(err)
	}
	execution := serverSetupExecution{ID: strings.Repeat("6", 32), RequestID: strings.Repeat("6", 32), PlanID: plan.ID, Status: "running", Steps: []serverSetupExecutionStep{}}
	raw, _ = json.Marshal(execution)
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, execution.ID, execution.RequestID, plan.ID, execution.Status, string(raw), "now", "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`UPDATE server_setup_state SET status='running' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	f.panel.serverSetupProbe = func(context.Context, serverSetupDraft) ([]serverSetupCheck, error) {
		return []serverSetupCheck{{ID: "verified", State: "ready", Code: "ready"}}, nil
	}
	serverSetupRunners.Store(f.panel, true)
	f.panel.finishServerSetupRunner(execution.ID)
	if _, running := serverSetupRunners.Load(f.panel); running {
		t.Fatal("same failed runner was restarted in a hot loop")
	}
	serverSetupRunners.Store(f.panel, true)
	f.panel.finishServerSetupRunner(strings.Repeat("5", 32))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		latest, err := f.panel.latestServerSetupExecution(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if latest.Status == "succeeded" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("new request remained orphaned after old runner exited")
}

func TestServerSetupFirewallReplaysReceiptBeforeLivePolicyChecks(t *testing.T) {
	f, _ := setupOperationFixture(t)
	plan := serverSetupPlan{TCPPorts: []int{80, 2083}, UDPPorts: []int{}}
	step := serverSetupExecutionStep{RequestID: strings.Repeat("a", 32), OwnerID: strings.Repeat("b", 32)}
	commitment, err := mutationpayload.CanonicalFirewallApply(true, true, plan.TCPPorts, plan.UDPPorts)
	if err != nil {
		t.Fatal(err)
	}
	f.agent.mu.Lock()
	f.agent.mutationJobs[step.RequestID] = &ServiceOperationMutationJob{RequestID: step.RequestID, OwnerID: step.OwnerID, Kind: "firewall_apply", Target: "nftables", PackageName: commitment.Qualifier, Status: agentMutationSucceeded, Phase: "commit/firewall-apply/v2/published/" + step.RequestID + "/" + commitment.Qualifier}
	f.agent.mu.Unlock()
	identity := agentMutationIdentityForOperation(serviceOperation{RequestID: step.RequestID, Kind: "firewall_apply", ServiceID: "nftables", PackageName: commitment.Qualifier}, step.OwnerID)
	phase, _, err := payloadBoundMutationPublishedPhase(identity)
	if err != nil {
		t.Fatal(err)
	}
	f.agent.mu.Lock()
	f.agent.mutationJobs[step.RequestID].Phase = phase
	f.agent.mu.Unlock()
	f.panel.license = testPanelLicense(t, "expired")
	// This fixture has no live SSH discovery proof. A committed child receipt
	// must still replay; final setup readiness separately re-proves live access.
	done, err := f.panel.runServerSetupFirewall(context.Background(), plan, step)
	if err != nil || !done {
		t.Fatalf("committed firewall replay depends on fresh policy: %v %v", done, err)
	}
	if f.agent.firewallCalls != 0 {
		t.Fatal("replay applied firewall again")
	}
}
