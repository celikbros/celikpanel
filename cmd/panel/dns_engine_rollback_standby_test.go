package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Decision B (2026-09-30): a first install the Agent rolled back leaves its
// packages as rollback standby. A new plan in the setup wizard retries that
// first install with a new request id; the rolled-back id keeps its terminal
// answer; an owner-installed stopped engine is never treated as standby.
func TestSetupDNSRetriesARolledBackFirstInstall(t *testing.T) {
	previous := probeServerSetupPrimaryCatalogSOA
	t.Cleanup(func() { probeServerSetupPrimaryCatalogSOA = previous })
	probeServerSetupPrimaryCatalogSOA = func(context.Context, string, string) (uint32, error) { return 7, nil }
	for _, tc := range []struct {
		name   string
		engine string
		role   string
	}{
		{"bind-paired-primary", "bind", "primary"},
		{"bind-paired-secondary", "bind", "secondary"},
		{"pdns-paired-secondary", "pdns", "secondary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newDNSPanelForTest(t)
			seedSetupDNSOwner(t, p)
			agent := newDNSEngineTestAgent()
			agent.switchError = "DNS engine switch did not complete; inspect the agent log"
			agent.switchErrorLeavesPackage = true
			agent.switchErrorLeavesStandby = true
			attachDNSEngineTestAgent(t, p, agent)
			draft := setupDNSTestDraft()
			draft.DNSEngine, draft.DNSRole = tc.engine, tc.role
			if tc.role == "secondary" {
				draft.LocalIP, draft.PeerIP, draft.PeerNS = "192.0.2.2", "192.0.2.1", "ns1.example.test"
			}
			t.Setenv("CELIKPANEL_SERVER_IP", draft.LocalIP)
			ctx := context.Background()
			first := strings.Repeat("4", 32)
			firstErr := p.startServerSetupDNS(ctx, draft, first, serviceOperationActor{UserID: 1})
			if firstErr == nil || errors.Is(firstErr, errServerSetupDNSReconciliationRequired) ||
				!errors.Is(firstErr, errServerSetupDNSRolledBack) {
				t.Fatalf("first install was not a proven rollback: %v", firstErr)
			}
			if failure := serverSetupFailureForStep(serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "dns"}}, firstErr); failure.Code != "server_setup_dns_rolled_back" ||
				!strings.Contains(failure.Message, "start this DNS step again") {
				t.Fatalf("wizard guidance for the rolled-back step = %+v", failure)
			}
			agent.mu.Lock()
			standby := agent.runtimes[transport.DNSEngine(tc.engine)]
			agent.switchError = ""
			agent.mu.Unlock()
			if !standby.Installed || standby.Running || !standby.RollbackStandby {
				t.Fatalf("fixture did not leave a rollback standby: %+v", standby)
			}
			if err := p.startServerSetupDNS(ctx, draft, first, serviceOperationActor{UserID: 1}); !errors.Is(err, errServerSetupDNSRolledBack) {
				t.Fatalf("the rolled-back request id lost its terminal answer: %v", err)
			}
			second := strings.Repeat("5", 32)
			if err := p.startServerSetupDNS(ctx, draft, second, serviceOperationActor{UserID: 1}); err != nil {
				t.Fatalf("a new plan could not retry the rolled-back first install: %v", err)
			}
			agent.mu.Lock()
			defer agent.mu.Unlock()
			if len(agent.switchRequests) != 2 ||
				agent.switchRequests[1].ServiceMutationBinding.MutationRequestID != second ||
				agent.switchRequests[1].SourceEngine != "" ||
				agent.switchRequests[1].TargetEngine != transport.DNSEngine(tc.engine) {
				t.Fatalf("retry did not reach the Agent as a first install: %+v", agent.switchRequests)
			}
		})
	}
}

func TestSetupDNSStillRefusesAnOwnerInstalledStoppedEngine(t *testing.T) {
	previous := probeServerSetupPrimaryCatalogSOA
	t.Cleanup(func() { probeServerSetupPrimaryCatalogSOA = previous })
	probeServerSetupPrimaryCatalogSOA = func(context.Context, string, string) (uint32, error) { return 7, nil }
	for _, engine := range []transport.DNSEngine{transport.DNSEngineBIND, transport.DNSEnginePowerDNS} {
		t.Run(string(engine), func(t *testing.T) {
			p := newDNSPanelForTest(t)
			seedSetupDNSOwner(t, p)
			agent := newDNSEngineTestAgent()
			runtime := agent.runtimes[engine]
			// Installed and stopped; even a Managed reading (an adopted
			// install receipt) is not the Agent's rollback standby.
			runtime.Installed, runtime.Running, runtime.Managed = true, false, engine == transport.DNSEnginePowerDNS
			agent.runtimes[engine] = runtime
			attachDNSEngineTestAgent(t, p, agent)
			draft := setupDNSTestDraft()
			draft.DNSEngine = string(engine)
			// PowerDNS as paired secondary: the paired primary is gated.
			draft.DNSRole = "secondary"
			draft.LocalIP, draft.PeerIP, draft.PeerNS = "192.0.2.2", "192.0.2.1", "ns1.example.test"
			t.Setenv("CELIKPANEL_SERVER_IP", draft.LocalIP)
			err := p.startServerSetupDNS(context.Background(), draft, strings.Repeat("6", 32), serviceOperationActor{UserID: 1})
			if err == nil || !strings.Contains(err.Error(), "cannot replace an existing DNS installation") {
				t.Fatalf("owner-installed stopped %s was not refused: %v", engine, err)
			}
			agent.mu.Lock()
			calls := agent.switchCalls
			agent.mu.Unlock()
			if calls != 0 {
				t.Fatal("setup reached the Agent for an owner-installed engine")
			}
		})
	}
}

// The paired PowerDNS primary still follows its closed gate after a
// rollback, whatever the standby signal says. The gate is set closed
// explicitly; the open gate is covered by the gate tests.
func TestSetupDNSPairedPDNSPrimaryStandbyStillFollowsTheGate(t *testing.T) {
	closePDNSPairedPrimaryGateForTest(t)
	p := newDNSPanelForTest(t)
	seedSetupDNSOwner(t, p)
	agent := newDNSEngineTestAgent()
	runtime := agent.runtimes[transport.DNSEnginePowerDNS]
	runtime.Installed, runtime.Managed, runtime.RollbackStandby = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = runtime
	attachDNSEngineTestAgent(t, p, agent)
	draft := setupDNSTestDraft()
	draft.DNSEngine = "pdns"
	t.Setenv("CELIKPANEL_SERVER_IP", draft.LocalIP)
	err := p.startServerSetupDNS(context.Background(), draft, strings.Repeat("7", 32), serviceOperationActor{UserID: 1})
	if err == nil || !strings.Contains(err.Error(), "prerequisites changed") {
		t.Fatalf("paired PowerDNS primary passed the closed gate: %v", err)
	}
}

// The engine card's action for each runtime shape: the Agent's standby signal
// makes a stopped engine a first install for either engine; without it the
// earlier decisions are unchanged.
func TestDNSEngineActionRollbackStandbyIsEngineNeutral(t *testing.T) {
	base := func(target transport.DNSEngine, runtime transport.DNSBackendRuntimeState) dnsEngineSnapshot {
		runtime.Engine = target
		return dnsEngineSnapshot{
			State: dnsEngineStateUnconfigured, Topology: transport.DNSTopologyStandalone,
			runtime: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
				target: runtime,
			},
		}
	}
	for _, tc := range []struct {
		name    string
		target  transport.DNSEngine
		runtime transport.DNSBackendRuntimeState
		want    string
	}{
		{"pdns-standby", transport.DNSEnginePowerDNS, transport.DNSBackendRuntimeState{Installed: true, Managed: true, RollbackStandby: true}, "install"},
		{"bind-standby", transport.DNSEngineBIND, transport.DNSBackendRuntimeState{Installed: true, Managed: true, RollbackStandby: true}, "install"},
		{"bind-older-agent-managed", transport.DNSEngineBIND, transport.DNSBackendRuntimeState{Installed: true, Managed: true}, "install"},
		{"pdns-installed-no-signal", transport.DNSEnginePowerDNS, transport.DNSBackendRuntimeState{Installed: true, Managed: true}, "switch"},
		{"bind-owner-stopped", transport.DNSEngineBIND, transport.DNSBackendRuntimeState{Installed: true}, dnsEngineActionAdoptUnmanaged},
		{"pdns-standby-running", transport.DNSEnginePowerDNS, transport.DNSBackendRuntimeState{Installed: true, Running: true, Managed: true, RollbackStandby: true}, "adopt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dnsEngineAction(base(tc.target, tc.runtime), tc.target); got != tc.want {
				t.Fatalf("action = %q, want %q", got, tc.want)
			}
		})
	}
	withEpoch := base(transport.DNSEnginePowerDNS, transport.DNSBackendRuntimeState{Installed: true, Managed: true, RollbackStandby: true})
	withEpoch.EngineEpoch = 1
	if dnsEngineAction(withEpoch, transport.DNSEnginePowerDNS) == "install" {
		t.Fatal("a standby signal overrode a recorded engine epoch")
	}
}

// The card retries a rolled-back standalone PowerDNS first install as an
// install with a new request id.
func TestDNSEngineCardRetriesPowerDNSStandbyAsInstall(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	agent.switchErrorLeavesPackage = true
	agent.switchErrorLeavesStandby = true
	attachDNSEngineTestAgent(t, panel, agent)
	preview, recorder := requestDNSEnginePreview(t, panel, transport.DNSEnginePowerDNS, nil, 0)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 || preview.Action != "install" {
		t.Fatalf("first preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(t, panel, strings.Repeat("a", 32), transport.DNSEnginePowerDNS, nil, 0, preview.PreviewToken, false)
	if commit.Code != http.StatusConflict {
		t.Fatalf("rolled-back first install status=%d body=%s", commit.Code, commit.Body.String())
	}
	agent.mu.Lock()
	agent.switchError = ""
	agent.mu.Unlock()
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	retryPreview, recorder := requestDNSEnginePreview(t, panel, transport.DNSEnginePowerDNS, nil, state.Revision)
	if recorder.Code != http.StatusOK || len(retryPreview.Blockers) != 0 || retryPreview.Action != "install" {
		t.Fatalf("retry preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	retry := commitDNSEngineSwitch(t, panel, strings.Repeat("b", 32), transport.DNSEnginePowerDNS, nil, state.Revision, retryPreview.PreviewToken, false)
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
}

func TestServerSetupRolledBackDNSGuidanceCarriesTheHostReason(t *testing.T) {
	sentence := "The server's package manager did not install bind9 (exit status 100). Package manager: E: Unable to locate package bind9"
	cause := &serverSetupDNSRolledBackError{cause: namedHostOperationFailure(sentence, errors.New("agent rejected DNS engine switch"))}
	failure := serverSetupFailureForStep(serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "dns"}}, cause)
	if failure.Code != "server_setup_dns_rolled_back" || !strings.HasSuffix(failure.Message, "Reason: "+sentence) {
		t.Fatalf("failure = %+v", failure)
	}
	plain := serverSetupFailureForStep(serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "dns"}}, errors.New("other"))
	if plain.Code != "server_setup_dns_failed" {
		t.Fatalf("an unrelated DNS failure changed code: %+v", plain)
	}
}
