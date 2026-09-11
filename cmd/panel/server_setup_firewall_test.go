package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func setupFirewallChildFixture(t *testing.T, p *Panel, kind string) (serviceOperation, panelCertificateSagaData) {
	t.Helper()
	userID := insertPanelCertificateSagaAdmin(t, p.db)
	step := serverSetupPlanStep{ID: "01-" + kind, Kind: kind, Target: "nginx"}
	if kind == "panel_certificate" {
		step.Target = "panel.example.test"
	}
	plan := serverSetupPlan{Version: serverSetupPlanVersion, Revision: 1, Purpose: "web", Steps: []serverSetupPlanStep{step}, CanStart: true, PreserveSSH: true, PersistFirewall: true,
		TCPPorts: []int{22, 80, 443, panelPort(), 8443}, UDPPorts: []int{5353}, ContactEmail: "cert-saga-private@example.test", BuildCommit: strings.TrimSpace(buildCommit),
		Draft: serverSetupDraft{Purpose: "web", DNSMode: "external", PanelDomain: "panel.example.test"}}
	plan.Actor = serviceOperationActor{UserID: userID}
	plan.ID = serverSetupPlanIdentity(plan)
	raw, _ := json.Marshal(plan)
	if _, err := p.db.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, 1, string(raw), userID, "now"); err != nil {
		t.Fatal(err)
	}
	execution := serverSetupExecution{ID: strings.Repeat("b", 32), RequestID: strings.Repeat("b", 32), PlanID: plan.ID, Status: "running"}
	requestID := serverSetupID(execution.ID, step.ID, "request")
	var op serviceOperation
	var data panelCertificateSagaData
	var err error
	if kind == "panel_certificate" {
		op, data = createPanelCertificateSagaTestOperation(t, p, requestID)
		if err := p.startPanelCertificateSaga(&op); err != nil {
			t.Fatal(err)
		}
	} else {
		op, err = p.createServiceOperationRequest(context.Background(), serviceOperationKindInstall, step.Target, "", requestID, serviceOperationActor{})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.markServiceOperationRunning(context.Background(), op.ID, "installed"); err != nil {
			t.Fatal(err)
		}
		op.Status = serviceOperationRunning
	}
	execution.Steps = []serverSetupExecutionStep{{serverSetupPlanStep: step, Status: "running", RequestID: requestID, OwnerID: serverSetupID(execution.ID, step.ID, "owner"), OperationID: op.ID}}
	raw, _ = json.Marshal(execution)
	if _, err := p.db.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, execution.ID, execution.RequestID, plan.ID, execution.Status, string(raw), "now", "now"); err != nil {
		t.Fatal(err)
	}
	return op, data
}

func setupFirewallAgent() *firewallSyncTestAgent {
	return &firewallSyncTestAgent{status: FirewallStatusResp{Enabled: true, EngineAvailable: true, TCPPorts: []int{22, 80, 443, panelPort(), 8443}, UDPPorts: []int{5353}, SSHPorts: []int{22}}, installed: []string{"nginx"}}
}

func TestServerSetupServiceFirewallChildPreservesReviewedPortsAfterRestart(t *testing.T) {
	agent := setupFirewallAgent()
	p, database := newPanelCertificateSagaTestPanel(t, agent)
	op, _ := setupFirewallChildFixture(t, p, "service")
	restarted := &Panel{db: database}
	attachFirewallSyncTestAgent(t, restarted, agent)
	if err := restarted.syncFirewallForServiceOperation(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.applyRequests) != 1 {
		t.Fatalf("apply count %d", len(agent.applyRequests))
	}
	req := agent.applyRequests[0]
	if !slices.Contains(req.TCPPorts, 8443) || !slices.Contains(req.UDPPorts, 5353) || req.Persist {
		t.Fatalf("reviewed live ports lost: %+v", req)
	}
	if agent.legacyApplyCalls != 0 {
		t.Fatal("legacy firewall path used")
	}
}

func TestServerSetupCertificateFirewallChildPreservesReviewedPorts(t *testing.T) {
	agent := setupFirewallAgent()
	p, _ := newPanelCertificateSagaTestPanel(t, agent)
	op, data := setupFirewallChildFixture(t, p, "panel_certificate")
	if err := p.planPanelCertificateFirewall(context.Background(), &op, &data, panelCertificateChildPreflight, panelCertificatePhasePreflightChild, panelCertificatePhasePreflightSkipped, 80); err != nil {
		t.Fatal(err)
	}
	if data.Child == nil || data.Child.Firewall == nil || !slices.Contains(data.Child.Firewall.TCPPorts, 8443) || !slices.Contains(data.Child.Firewall.UDPPorts, 5353) {
		t.Fatalf("certificate reduced reviewed policy: %+v", data.Child)
	}
	if err := p.executePanelCertificateFirewall(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.applyRequests) != 1 || !slices.Contains(agent.applyRequests[0].TCPPorts, 8443) {
		t.Fatalf("wrong applied policy: %+v", agent.applyRequests)
	}
}

func TestServerSetupFirewallChildRefusesUnreviewedAccessWithoutMutation(t *testing.T) {
	agent := setupFirewallAgent()
	p, _ := newPanelCertificateSagaTestPanel(t, agent)
	op, _ := setupFirewallChildFixture(t, p, "service")
	agent.status.TCPPorts = append(agent.status.TCPPorts, 9000)
	if err := p.syncFirewallForServiceOperation(context.Background(), op); err == nil || !strings.Contains(err.Error(), "changed after setup review") {
		t.Fatalf("unreviewed policy accepted: %v", err)
	}
	if agent.applyCalls != 0 || agent.beginCalls != 0 {
		t.Fatalf("mutations after failed review: %d/%d", agent.applyCalls, agent.beginCalls)
	}
}

func TestServerSetupFirewallPolicyCannotBeBorrowedByUnrelatedOperation(t *testing.T) {
	agent := setupFirewallAgent()
	p, _ := newPanelCertificateSagaTestPanel(t, agent)
	op, _ := setupFirewallChildFixture(t, p, "service")
	op.RequestID = strings.Repeat("f", 32)
	tcp, udp, err := p.setupChildFirewallPorts(context.Background(), op, transport.FirewallStatusResponse{}, []int{80}, nil)
	if err != nil || len(tcp) != 1 || tcp[0] != 80 || len(udp) != 0 {
		t.Fatalf("unrelated operation inherited setup ports: %v %v %v", tcp, udp, err)
	}
}
