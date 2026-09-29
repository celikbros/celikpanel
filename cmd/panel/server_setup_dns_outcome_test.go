package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/rpc"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Setup DNS step outcome (D-024, pair2 finding P-B). Component tests only;
// they are not native evidence.

type setupDNSOutcomeFixture struct {
	f         serviceOperationTestFixture
	agent     *dnsEngineTestAgent
	plan      serverSetupPlan
	execution serverSetupExecution
}

// newSetupDNSOutcomeFixture stores an accepted plan whose first step is the
// local DNS install of draft, and a running execution for it.
func newSetupDNSOutcomeFixture(t *testing.T, draft serverSetupDraft, agent *dnsEngineTestAgent) *setupDNSOutcomeFixture {
	t.Helper()
	f, state := setupOperationFixture(t)
	draft.PanelDomain = "panel.example.test"
	var err error
	state, err = f.panel.saveServerSetupDraft(context.Background(), state.Revision, draft)
	if err != nil {
		t.Fatal(err)
	}
	attachDNSEngineTestAgent(t, f.panel, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", draft.LocalIP)
	plan := serverSetupPlan{Version: serverSetupPlanVersion, Revision: state.Revision, Draft: state.Draft, Purpose: state.Draft.Purpose,
		Actor: serviceOperationActor{UserID: f.userID},
		Steps: []serverSetupPlanStep{{ID: "01-dns", Kind: "dns", Target: "local"}, {ID: "02-verify", Kind: "verify", Target: state.Draft.Purpose}}}
	plan.ID = serverSetupPlanIdentity(plan)
	raw, _ := json.Marshal(plan)
	if _, err = f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(raw), f.userID, "now"); err != nil {
		t.Fatal(err)
	}
	execution := serverSetupExecution{ID: strings.Repeat("8", 32), RequestID: strings.Repeat("8", 32), PlanID: plan.ID, Status: "running"}
	for _, step := range plan.Steps {
		execution.Steps = append(execution.Steps, serverSetupExecutionStep{serverSetupPlanStep: step, Status: "pending",
			RequestID: serverSetupID(execution.ID, step.ID, "request"), OwnerID: serverSetupID(execution.ID, step.ID, "owner")})
	}
	raw, _ = json.Marshal(execution)
	if _, err = f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,'now','now')`,
		execution.ID, execution.RequestID, plan.ID, execution.Status, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.database.GetDB().Exec(`UPDATE server_setup_state SET status='running' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	return &setupDNSOutcomeFixture{f: f, agent: agent, plan: plan, execution: execution}
}

// poll is one runner pass over the same execution.
func (x *setupDNSOutcomeFixture) poll(t *testing.T) {
	t.Helper()
	if _, err := x.f.panel.advanceServerSetupExecution(x.plan, &x.execution); err != nil {
		t.Fatal(err)
	}
}

func (x *setupDNSOutcomeFixture) switchCalls() int {
	x.agent.mu.Lock()
	defer x.agent.mu.Unlock()
	return x.agent.switchCalls
}

func freshPDNSPrimaryDraft() serverSetupDraft {
	d := setupDNSTestDraft()
	d.DNSEngine = "pdns"
	return d
}

// The exact t3 sequence with the gate open: the Agent refused the fresh
// paired PowerDNS primary, ended its job failed and left the rolled-back
// standby. The first poll shows the rollback with its stable code instead of
// "not confirmed yet", and the draft is released for a new plan.
func TestServerSetupDNSFreshPDNSPrimaryAgentRollbackShownWithinOnePoll(t *testing.T) {
	openPDNSPairedPrimaryGateForTest(t)
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	x := newSetupDNSOutcomeFixture(t, freshPDNSPrimaryDraft(), agent)
	x.poll(t)
	if x.execution.Status != "failed" || x.execution.Steps[0].Status != "failed" || x.execution.Error == nil ||
		x.execution.Error.Code != "server_setup_dns_rolled_back" {
		t.Fatalf("t3 rollback not shown within one poll: %+v", x.execution)
	}
	persisted, err := readDNSEngineSwitchByRequest(context.Background(), x.f.database.GetDB(), x.execution.Steps[0].RequestID)
	if err != nil || persisted.Phase != "rolled_back" {
		t.Fatalf("switch=%+v err=%v", persisted, err)
	}
	if calls := x.switchCalls(); calls != 1 {
		t.Fatalf("host switch calls=%d", calls)
	}
}

// Gate closed (as shipped): a fresh paired PowerDNS primary is outside the
// rollback-evidence scope on both sides.
func TestFreshPDNSPrimaryReconcileScopeFollowsTheGate(t *testing.T) {
	persisted := persistedDNSEngineSwitch{Mode: transport.DNSEngineSwitchModeSwitch, Action: "install",
		TargetEngine: transport.DNSEnginePowerDNS, TargetEpoch: 1, Topology: transport.DNSTopologyPaired,
		PairRole: transport.DNSPairRolePrimary, LocalIP: "192.0.2.1", LocalNS: "ns1.example.test",
		PeerIP: "192.0.2.2", PeerNS: "ns2.example.test"}
	if validateInitialDNSEngineInstallReconcileScope(persisted) == nil {
		t.Fatal("closed gate admitted the fresh paired PowerDNS primary rollback")
	}
	openPDNSPairedPrimaryGateForTest(t)
	if err := validateInitialDNSEngineInstallReconcileScope(persisted); err != nil {
		t.Fatalf("open gate refused the fresh paired PowerDNS primary rollback: %v", err)
	}
	for name, edit := range map[string]func(*persistedDNSEngineSwitch){
		"source":       func(p *persistedDNSEngineSwitch) { p.SourceEngine, p.SourceEpoch = transport.DNSEngineBIND, 1 },
		"reconfigure":  func(p *persistedDNSEngineSwitch) { p.Action = "reconfigure" },
		"epoch 2":      func(p *persistedDNSEngineSwitch) { p.TargetEpoch = 2 },
		"partial pair": func(p *persistedDNSEngineSwitch) { p.PeerNS = "" },
	} {
		changed := persisted
		edit(&changed)
		if validateInitialDNSEngineInstallReconcileScope(changed) == nil {
			t.Fatalf("%s: open gate widened beyond the fresh first install", name)
		}
	}
}

// The Agent's job for the exact request is failed but the Panel cannot prove
// the rollback: the first poll shows the verified failure with its reason,
// who acts and the command, keeps the step and the draft locked, and polling
// never starts a second host mutation.
func TestServerSetupDNSAgentFailedJobShownWithinOnePollWithoutRetry(t *testing.T) {
	openPDNSPairedPrimaryGateForTest(t)
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackIdentityMismatch
	x := newSetupDNSOutcomeFixture(t, freshPDNSPrimaryDraft(), agent)
	for poll := 0; poll < 3; poll++ {
		x.poll(t)
		requestID := x.execution.Steps[0].RequestID
		if x.execution.Status != "waiting" || x.execution.Steps[0].Status != "running" || x.execution.Error == nil ||
			x.execution.Error.Code != serverSetupDNSAgentFailedCode ||
			!strings.Contains(x.execution.Error.Message, "as failed") ||
			!strings.Contains(x.execution.Error.Message, "Reason: ") ||
			!strings.Contains(x.execution.Error.Message, "The server administrator") ||
			!strings.Contains(x.execution.Error.Message, "sudo /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id "+requestID) ||
			strings.Contains(x.execution.Error.Message, "not been confirmed yet") {
			t.Fatalf("poll %d: verified failure not shown: %+v", poll, x.execution)
		}
	}
	if calls := x.switchCalls(); calls != 1 {
		t.Fatalf("polling started another host mutation: %d", calls)
	}
	state, err := readDNSEngineDBState(context.Background(), x.f.database.GetDB())
	if err != nil || state.CurrentSwitchID == "" {
		t.Fatalf("unproven rollback released the accepted switch: %+v %v", state, err)
	}
	// Once the Agent's evidence proves the rollback, the same poll records it.
	agent.mu.Lock()
	agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackSafe
	agent.mu.Unlock()
	x.poll(t)
	if x.execution.Status != "failed" || x.execution.Error == nil || x.execution.Error.Code != "server_setup_dns_rolled_back" {
		t.Fatalf("proven rollback not shown: %+v", x.execution)
	}
}

// A job the Agent released without a verified native result (journal kept,
// DNS changes blocked) is shown as held with the Agent's own guidance.
func TestServerSetupDNSAgentReleasedJobShownAsHeld(t *testing.T) {
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackJournalPresent
	x := newSetupDNSOutcomeFixture(t, setupDNSTestDraft(), agent)
	x.poll(t)
	requestID := x.execution.Steps[0].RequestID
	agent.mu.Lock()
	job := agent.jobs[requestID]
	if job == nil {
		agent.mu.Unlock()
		t.Fatal("fixture has no Agent job for the request")
	}
	job.Phase = "interrupted"
	job.ErrorCode = dnsengineartifact.ReleasedNativeUnknownCode
	job.ErrorMessage = "The DNS engine switch did not complete and its native result could not be verified. Its exact journal remains for DNS recovery.\nRun the status command."
	agent.mu.Unlock()
	x.poll(t)
	if x.execution.Status != "waiting" || x.execution.Error == nil || x.execution.Error.Code != serverSetupDNSRecoveryHeldCode ||
		!strings.Contains(x.execution.Error.Message, "Agent: The DNS engine switch did not complete and its native result could not be verified.") ||
		strings.Contains(x.execution.Error.Message, "\n") ||
		!strings.Contains(x.execution.Error.Message, "--request-id "+requestID) {
		t.Fatalf("released job not shown as held: %+v", x.execution)
	}
	if calls := x.switchCalls(); calls != 1 {
		t.Fatalf("polling started another host mutation: %d", calls)
	}
}

// A job the Agent still runs is progress, not an unknown.
func TestServerSetupDNSAgentRunningJobIsProgress(t *testing.T) {
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackIdentityMismatch
	x := newSetupDNSOutcomeFixture(t, setupDNSTestDraft(), agent)
	x.poll(t)
	requestID := x.execution.Steps[0].RequestID
	agent.mu.Lock()
	agent.jobs[requestID].Status = agentMutationRunning
	agent.jobs[requestID].FinishedAt = time.Time{}
	agent.mu.Unlock()
	x.poll(t)
	if x.execution.Status != "running" || x.execution.Error != nil {
		t.Fatalf("running Agent job not shown as progress: %+v", x.execution)
	}
	if calls := x.switchCalls(); calls != 1 {
		t.Fatalf("polling started another host mutation: %d", calls)
	}
}

// Unknown to the Panel (the Agent cannot be read): "being checked" within the
// bound, then a text naming the unknown, since when, the check and that
// nothing is started twice.
func TestServerSetupDNSUnknownResultIsTimeBounded(t *testing.T) {
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackIdentityMismatch
	x := newSetupDNSOutcomeFixture(t, setupDNSTestDraft(), agent)
	x.poll(t)
	requestID := x.execution.Steps[0].RequestID
	x.f.panel.agentClient = transport.NewReconnectingClientWithContextConnector(nil, func(context.Context) (*rpc.Client, error) {
		return nil, errors.New("fixture agent unavailable")
	})
	x.poll(t)
	if x.execution.Status != "running" || x.execution.Error == nil || x.execution.Error.Code != "server_setup_reconciling" {
		t.Fatalf("fresh unknown not shown as being checked: %+v", x.execution)
	}
	old := time.Now().UTC().Add(-serverSetupDNSUnknownBound - time.Minute).Format("2006-01-02 15:04:05")
	if _, err := x.f.database.GetDB().Exec(`UPDATE dns_engine_switch_snapshots SET updated_at=? WHERE request_id=?`, old, requestID); err != nil {
		t.Fatal(err)
	}
	x.poll(t)
	if x.execution.Status != "running" || x.execution.Error == nil || x.execution.Error.Code != serverSetupDNSResultUnknownCode ||
		!strings.Contains(x.execution.Error.Message, "still unknown") ||
		!strings.Contains(x.execution.Error.Message, "since "+old[:16]+" UTC") ||
		!strings.Contains(x.execution.Error.Message, "not a verified failure") ||
		!strings.Contains(x.execution.Error.Message, "nothing will be started twice") ||
		!strings.Contains(x.execution.Error.Message, "recovery dns-switch-status --quiesced --request-id "+requestID) {
		t.Fatalf("prolonged unknown not bounded: %+v", x.execution)
	}
	if calls := x.switchCalls(); calls != 1 {
		t.Fatalf("polling started another host mutation: %d", calls)
	}
}

func TestServerSetupDNSReconcilingErrorBound(t *testing.T) {
	now := time.Date(2026, 9, 29, 22, 36, 0, 0, time.UTC)
	id := strings.Repeat("e", 32)
	if got := serverSetupDNSReconcilingError(id, now.Add(-serverSetupDNSUnknownBound+time.Second), now); got.Code != "server_setup_reconciling" {
		t.Fatalf("within bound: %+v", got)
	}
	if got := serverSetupDNSReconcilingError(id, time.Time{}, now); got.Code != "server_setup_reconciling" {
		t.Fatalf("no since: %+v", got)
	}
	got := serverSetupDNSReconcilingError(id, now.Add(-45*time.Minute), now)
	if got.Code != serverSetupDNSResultUnknownCode || !strings.Contains(got.Message, "since 2026-09-29 21:51 UTC (45 minutes)") {
		t.Fatalf("after bound: %+v", got)
	}
	if serverSetupDNSUnknownBound != panelDNSCommitReconcileTimeout {
		t.Fatalf("unknown bound=%v", serverSetupDNSUnknownBound)
	}
}
