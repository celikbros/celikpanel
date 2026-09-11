package main

import (
	"context"
	"errors"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestSetupDNSRejectedChildFinalizesExactRollbackBeforeNewAdmission(t *testing.T) {
	p := newDNSPanelForTest(t)
	seedSetupDNSOwner(t, p)
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	attachDNSEngineTestAgent(t, p, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	ctx := context.Background()
	id := strings.Repeat("4", 32)
	err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), id, serviceOperationActor{UserID: 1})
	if err == nil || errors.Is(err, errServerSetupDNSReconciliationRequired) {
		t.Fatalf("proven rejected child outcome: %v", err)
	}
	old, err := readDNSEngineSwitchByRequest(ctx, p.db.GetDB(), id)
	if err != nil || old.Phase != "rolled_back" {
		t.Fatalf("failed accepted child retained pending switch: %+v %v", old, err)
	}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil || state.CurrentSwitchID != "" || state.ActiveEngine != "" {
		t.Fatalf("rejected authority was not restored: %+v %v", state, err)
	}
	agent.mu.Lock()
	proofCalls := agent.rollbackEvidenceCalls
	agent.switchError = ""
	agent.mu.Unlock()
	if proofCalls < 2 {
		t.Fatal("rollback lacked stable exact agent evidence")
	}
	if err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), id, serviceOperationActor{UserID: 1}); err == nil {
		t.Fatal("failed child identity was reused for another installation")
	}
	if err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), strings.Repeat("5", 32), serviceOperationActor{UserID: 1}); err != nil {
		t.Fatalf("new reviewed identity could not proceed after proven rollback: %v", err)
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 2 {
		t.Fatalf("unexpected host install count: %d", calls)
	}
}

func TestSetupDNSUnprovenFailureKeepsExactChildPending(t *testing.T) {
	for _, applied := range []bool{false, true} {
		name := "unknown_failed_receipt"
		if applied {
			name = "applied_followup_unavailable"
		}
		t.Run(name, func(t *testing.T) {
			p := newDNSPanelForTest(t)
			seedSetupDNSOwner(t, p)
			agent := newDNSEngineTestAgent()
			if applied {
				agent.readinessAfterSwitchError = "fixture post-apply readiness unavailable"
			} else {
				agent.switchError = "DNS engine switch did not complete; inspect the agent log"
				agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackIdentityMismatch
			}
			attachDNSEngineTestAgent(t, p, agent)
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
			ctx := context.Background()
			id := strings.Repeat("6", 32)
			err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), id, serviceOperationActor{UserID: 1})
			if !errors.Is(err, errServerSetupDNSReconciliationRequired) {
				t.Fatalf("unproven child became terminal: %v", err)
			}
			state, err := readDNSEngineDBState(ctx, p.db.GetDB())
			if err != nil || state.CurrentSwitchID == "" {
				t.Fatalf("unproven accepted identity was discarded: %+v %v", state, err)
			}
			if err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), strings.Repeat("7", 32), serviceOperationActor{UserID: 1}); err == nil {
				t.Fatal("new identity replaced unproven child")
			}
			originalClient := p.agentClient
			p.agentClient = transport.NewReconnectingClientWithContextConnector(nil, func(context.Context) (*rpc.Client, error) { return nil, errors.New("fixture agent unavailable") })
			if err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), id, serviceOperationActor{UserID: 1}); !errors.Is(err, errServerSetupDNSReconciliationRequired) {
				t.Fatalf("saved-child status transport loss became terminal: %v", err)
			}
			p.agentClient = originalClient
			if applied {
				agent.mu.Lock()
				agent.readinessAfterSwitchError = ""
				agent.mu.Unlock()
				if err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), id, serviceOperationActor{UserID: 1}); err != nil {
					t.Fatal(err)
				}
				agent.mu.Lock()
				calls := agent.switchCalls
				agent.mu.Unlock()
				if calls != 1 {
					t.Fatalf("applied child installed twice: %d", calls)
				}
			}
		})
	}
}

func TestSetupDNSCommittedModeSaveFailureResumesWithoutReinstall(t *testing.T) {
	p := newDNSPanelForTest(t)
	seedSetupDNSOwner(t, p)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	if _, err := p.db.GetDB().Exec(`CREATE TRIGGER fixture_dns_mode_write BEFORE INSERT ON panel_settings WHEN NEW.key='` + settingSetupDNSMode + `' BEGIN SELECT RAISE(ABORT,'fixture mode storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := strings.Repeat("8", 32)
	if err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), id, serviceOperationActor{UserID: 1}); !errors.Is(err, errServerSetupDNSReconciliationRequired) {
		t.Fatalf("committed DNS mode write became terminal: %v", err)
	}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil || state.ActiveEngine != transport.DNSEngineBIND || state.CurrentSwitchID != "" {
		t.Fatalf("committed authority changed: %+v %v", state, err)
	}
	if _, err := p.db.GetDB().Exec(`DROP TRIGGER fixture_dns_mode_write`); err != nil {
		t.Fatal(err)
	}
	if err := p.startServerSetupDNS(ctx, setupDNSTestDraft(), id, serviceOperationActor{UserID: 1}); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("committed child reinstalled: %d", calls)
	}
}
