package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func setupOperationFixture(t *testing.T) (serviceOperationTestFixture, serverSetupState) {
	t.Helper()
	f := newServiceOperationTestFixture(t)
	f.panel.license = testPanelLicense(t, "active")
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	t.Setenv("CELIKPANEL_TLS_DIR", panelManagedTLSDirectory)
	state, err := f.panel.loadServerSetup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.panel.saveServerSetupDraft(context.Background(), state.Revision, serverSetupDraft{Purpose: "web", PanelDomain: "panel.example.test", DNSMode: "external"})
	if err != nil {
		t.Fatal(err)
	}
	return f, state
}

func saveSetupPlanForTest(t *testing.T, f serviceOperationTestFixture, state serverSetupState) serverSetupPlan {
	t.Helper()
	plan, err := f.panel.buildServerSetupPlan(context.Background(), state, serviceOperationActor{UserID: f.userID})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CanStart {
		t.Fatalf("plan unexpectedly blocked: %v", plan.Blockers)
	}
	raw, _ := json.Marshal(plan)
	_, err = f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(raw), f.userID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func postSetupStartForTest(t *testing.T, f serviceOperationTestFixture, planID, requestID string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"plan_id": planID, "request_id": requestID, "confirmed": true})
	w := httptest.NewRecorder()
	f.panel.handleServerSetupStart(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/setup/start", string(body), f.userID))
	return w
}

func TestServerSetupReviewIsReadOnlyAndWebDoesNotInstallMailOrDNS(t *testing.T) {
	f, state := setupOperationFixture(t)
	plan := saveSetupPlanForTest(t, f, state)
	for _, step := range plan.Steps {
		if step.Kind == "mail_profile" || slices.Contains([]string{"postfix", "dovecot", "roundcube", "rspamd", "pdns", "bind"}, step.Target) {
			t.Fatalf("web/external plan included unrelated service: %+v", step)
		}
	}
	if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
		t.Fatal("review changed the host")
	}
	if !plan.PersistFirewall || !plan.PreserveSSH || !slices.Contains(plan.TCPPorts, 80) || !slices.Contains(plan.TCPPorts, 443) || !slices.Contains(plan.TCPPorts, panelPort()) {
		t.Fatalf("review omitted required access rules: %+v", plan)
	}
	active, err := f.panel.activeServiceOperation(context.Background())
	if err != nil || active != nil {
		t.Fatalf("review created service operation: %+v %v", active, err)
	}
}

func TestServerSetupStartIsDurableIdempotentAndLocksDraft(t *testing.T) {
	f, state := setupOperationFixture(t)
	plan := saveSetupPlanForTest(t, f, state)
	// Hold the worker at the persisted boundary to simulate a lost response and
	// a panel restart before the first child is admitted.
	serverSetupRunners.Store(f.panel, true)
	t.Cleanup(func() { serverSetupRunners.Delete(f.panel) })
	requestID := strings.Repeat("a", 32)
	first := postSetupStartForTest(t, f, plan.ID, requestID)
	if first.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", first.Code, first.Body.String())
	}
	second := postSetupStartForTest(t, f, plan.ID, requestID)
	if second.Code != http.StatusAccepted || second.Body.String() != first.Body.String() {
		t.Fatalf("lost response replay changed operation: %d %s", second.Code, second.Body.String())
	}
	var rows int
	if err := f.database.GetDB().QueryRow(`SELECT COUNT(*) FROM server_setup_executions`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("duplicate execution: %d %v", rows, err)
	}
	if _, err := f.panel.saveServerSetupDraft(context.Background(), state.Revision, state.Draft); !errors.Is(err, errServerSetupConflict) {
		t.Fatalf("running plan allowed draft change: %v", err)
	}
	var execution serverSetupExecution
	if err := json.Unmarshal(first.Body.Bytes(), &execution); err != nil {
		t.Fatal(err)
	}
	for _, step := range execution.Steps {
		if !validServiceOperationID(step.RequestID) || !validServiceOperationID(step.OwnerID) {
			t.Fatalf("child identity missing before host mutation: %+v", step)
		}
	}
	w := httptest.NewRecorder()
	f.panel.handleServerSetupOperation(w, serviceOperationAdminRequest(t, http.MethodGet, "/api/v1/setup/operation?request_id="+requestID, "", f.userID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), requestID) {
		t.Fatalf("exact reconciliation failed: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	f.panel.handleServerSetupOperation(w, serviceOperationAdminRequest(t, http.MethodGet, "/api/v1/setup/operation?request_id="+strings.Repeat("b", 32), "", f.userID))
	if strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatalf("unknown request bound another execution: %s", w.Body.String())
	}
}

func TestServerSetupChangedHostInvalidatesReviewBeforeMutation(t *testing.T) {
	f, state := setupOperationFixture(t)
	plan := saveSetupPlanForTest(t, f, state)
	seedInstalledServices(f.agent, "nginx")
	w := postSetupStartForTest(t, f, plan.ID, strings.Repeat("c", 32))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "server_setup_review_stale") {
		t.Fatalf("stale host plan accepted: %d %s", w.Code, w.Body.String())
	}
	if f.agent.installCalls.Load() != 0 {
		t.Fatal("stale review started install")
	}
	active, err := f.panel.serverSetupExecutionActive(context.Background())
	if err != nil || active {
		t.Fatalf("stale review persisted active execution: %v %v", active, err)
	}
}

func TestServerSetupRoleDenialPrecedesStorageAndRPC(t *testing.T) {
	p := &Panel{}
	for _, role := range []string{roleReseller, roleCustomer, "additional"} {
		for _, endpoint := range []struct {
			method  string
			handler http.HandlerFunc
		}{{http.MethodPost, p.handleServerSetupPlan}, {http.MethodPost, p.handleServerSetupStart}, {http.MethodGet, p.handleServerSetupOperation}} {
			r := httptest.NewRequest(endpoint.method, "/api/v1/setup", strings.NewReader(`{}`))
			r = r.WithContext(context.WithValue(r.Context(), callerKey, &Caller{ID: 1, Role: role}))
			w := httptest.NewRecorder()
			endpoint.handler(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s role status %d", role, w.Code)
			}
		}
	}
}

func TestServerSetupChildReplayUsesDurableReceiptWithoutNewInstallation(t *testing.T) {
	f, _ := setupOperationFixture(t)
	step := serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{ID: "service", Kind: "service", Target: "nginx"}, RequestID: strings.Repeat("d", 32), OwnerID: strings.Repeat("e", 32)}
	op, err := f.panel.createServiceOperationRequest(context.Background(), serviceOperationKindInstall, "nginx", "", step.RequestID, serviceOperationActor{UserID: f.userID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`UPDATE service_operations SET status='succeeded',result_json='{"success":true,"installed":true}' WHERE id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		done, err := f.panel.runServerSetupStep(context.Background(), serverSetupPlan{}, &step)
		if err != nil || !done || step.OperationID != op.ID {
			t.Fatalf("resume: %v %v %+v", done, err, step)
		}
	}
	if f.agent.installCalls.Load() != 0 {
		t.Fatal("receipt replay installed again")
	}
	if _, err := f.database.GetDB().Exec(`UPDATE service_operations SET status='failed',error_code='test_failure',error_message='install failed' WHERE id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	done, err := f.panel.runServerSetupStep(context.Background(), serverSetupPlan{}, &step)
	if err == nil || done {
		t.Fatalf("failed child became success: %v %v", done, err)
	}
}

func TestServerSetupFirewallRefusesMissingSSHProof(t *testing.T) {
	f, _ := setupOperationFixture(t)
	done, err := f.panel.runServerSetupFirewall(context.Background(), serverSetupPlan{TCPPorts: []int{80, 443, 2083}}, serverSetupExecutionStep{RequestID: strings.Repeat("e", 32), OwnerID: strings.Repeat("f", 32)})
	if !errors.Is(err, errFirewallSSHUnprovable) || done {
		t.Fatalf("missing SSH proof was accepted: %v %v", done, err)
	}
	if f.agent.firewallCalls != 0 {
		t.Fatal("firewall applied without SSH proof")
	}
}

func TestServerSetupMailHostnameSurvivesLaterSettingChanges(t *testing.T) {
	f, _ := setupOperationFixture(t)
	if err := f.panel.setSetting(context.Background(), settingMailHostname, "changed.example.test"); err != nil {
		t.Fatal(err)
	}
	op := serviceOperation{OperationData: `{"setup_mail_hostname":"mail.reviewed.test"}`}
	reviewed, err := decodeServerSetupMailChild(op)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), serverSetupMailHostnameKey{}, reviewed)
	got, err := f.panel.resolveMailProfileHostname(ctx)
	if err != nil || got != "mail.reviewed.test" {
		t.Fatalf("reviewed identity changed during recovery: %q %v", got, err)
	}
	op.OperationData = `{"setup_mail_hostname":"127.0.0.1"}`
	if _, err := decodeServerSetupMailChild(op); err == nil {
		t.Fatal("malformed saved mail identity accepted")
	}
}

func TestServerSetupInstalledChildCannotCompleteWithoutFreshReadiness(t *testing.T) {
	f, state := setupOperationFixture(t)
	plan := serverSetupPlan{Version: serverSetupPlanVersion, Revision: state.Revision, Draft: state.Draft, Purpose: "web", Actor: serviceOperationActor{UserID: f.userID}, Steps: []serverSetupPlanStep{{ID: "01-service", Kind: "service", Target: "nginx"}, {ID: "02-verify", Kind: "verify", Target: "web"}}}
	plan.ID = serverSetupPlanIdentity(plan)
	raw, _ := json.Marshal(plan)
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(raw), f.userID, "now"); err != nil {
		t.Fatal(err)
	}
	execution := serverSetupExecution{ID: strings.Repeat("1", 32), RequestID: strings.Repeat("1", 32), PlanID: plan.ID, Status: "running"}
	for _, step := range plan.Steps {
		execution.Steps = append(execution.Steps, serverSetupExecutionStep{serverSetupPlanStep: step, Status: "pending", RequestID: serverSetupID(execution.ID, step.ID, "request"), OwnerID: serverSetupID(execution.ID, step.ID, "owner")})
	}
	raw, _ = json.Marshal(execution)
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, execution.ID, execution.RequestID, plan.ID, "running", string(raw), "now", "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`UPDATE server_setup_state SET status='running' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	op, err := f.panel.createServiceOperationRequest(context.Background(), serviceOperationKindInstall, "nginx", "", execution.Steps[0].RequestID, plan.Actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`UPDATE service_operations SET status='succeeded',result_json='{"success":true}' WHERE id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	ready := false
	probes := 0
	f.panel.serverSetupProbe = func(context.Context, serverSetupDraft) ([]serverSetupCheck, error) {
		probes++
		if ready {
			return []serverSetupCheck{{ID: "panel_https", State: "ready", Code: "ready"}}, nil
		}
		return []serverSetupCheck{{ID: "panel_https", State: "action_required", Code: "panel_https_required"}}, nil
	}
	for range 3 {
		if _, err := f.panel.advanceServerSetupExecution(plan, &execution); err != nil {
			t.Fatal(err)
		}
	}
	if execution.Status != "waiting" || probes == 0 {
		t.Fatalf("missing readiness was completed: %+v probes=%d", execution, probes)
	}
	state, err = f.panel.loadServerSetup(context.Background())
	if err != nil || state.Status == "ready" {
		t.Fatalf("installed component falsely completed setup: %+v %v", state, err)
	}
	ready = true
	if _, err := f.panel.advanceServerSetupExecution(plan, &execution); err != nil {
		t.Fatal(err)
	}
	state, err = f.panel.loadServerSetup(context.Background())
	if err != nil || execution.Status != "succeeded" || state.Status != "ready" {
		t.Fatalf("fresh readiness did not complete: %+v %+v %v", execution, state, err)
	}
	if f.agent.installCalls.Load() != 0 {
		t.Fatal("readiness retry repeated child installation")
	}
}

func TestServerSetupCorruptedExecutionCannotChangeReviewedTarget(t *testing.T) {
	plan := serverSetupPlan{ID: "plan", Steps: []serverSetupPlanStep{{ID: "01-service", Kind: "service", Target: "nginx"}}}
	execution := serverSetupExecution{ID: strings.Repeat("9", 32), RequestID: strings.Repeat("9", 32), PlanID: plan.ID}
	step := plan.Steps[0]
	execution.Steps = []serverSetupExecutionStep{{serverSetupPlanStep: step, Status: "pending", RequestID: serverSetupID(execution.ID, step.ID, "request"), OwnerID: serverSetupID(execution.ID, step.ID, "owner")}}
	if err := validateServerSetupExecution(plan, execution); err != nil {
		t.Fatal(err)
	}
	execution.Steps[0].Target = "postfix"
	if err := validateServerSetupExecution(plan, execution); err == nil {
		t.Fatal("changed persisted target bypassed plan identity")
	}
}
