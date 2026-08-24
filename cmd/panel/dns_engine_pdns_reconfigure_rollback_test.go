package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func newLegacyPDNSSecondaryPanelForTest(
	t *testing.T,
) (*Panel, *dnsEngineTestAgent) {
	t.Helper()
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.20")
	panel := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, panel, agent)
	seedDNSSetupAuditUser(t, panel)
	stage := httptest.NewRecorder()
	panel.handleDNSSetup(stage, dnsSetupAdminRequest(
		`{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.10","peer_ns":"ns1.example.net"}`,
	))
	if stage.Code != http.StatusOK {
		t.Fatalf("secondary stage status=%d body=%s", stage.Code, stage.Body.String())
	}
	return panel, agent
}

func persistLegacyPDNSSecondaryReconfigureForTest(
	t *testing.T,
	panel *Panel,
	requestID string,
) persistedDNSEngineSwitch {
	t.Helper()
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := panel.buildDNSEngineManifest(
		context.Background(), state, transport.DNSEnginePowerDNS,
		"reconfigure", transport.DNSTopologyPaired,
	)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, _ := newServiceOperationID()
	switchID, _ := newServiceOperationID()
	persisted, err := panel.persistDNSEngineSwitch(
		context.Background(),
		dnsEngineSwitchRequest{
			RequestID: requestID, TargetEngine: transport.DNSEnginePowerDNS,
			ExpectedSource: nullableDNSEngine{Set: true}, ExpectedRevision: state.Revision,
		},
		ownerID, switchID, "reconfigure", manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLegacyPDNSPairSecondaryReconfigureScope(persisted); err != nil {
		t.Fatalf("persisted reconfigure scope: %v", err)
	}
	return persisted
}

func assertPDNSReconfigureRolledBack(
	t *testing.T,
	panel *Panel,
	persisted persistedDNSEngineSwitch,
) {
	t.Helper()
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSwitchID != "" || state.ActiveEngine != "" ||
		state.EngineEpoch != 0 || state.Topology != transport.DNSTopologyStandalone {
		t.Fatalf("reconfigure rollback state=%+v", state)
	}
	var phase string
	if err := panel.db.GetDB().QueryRow(
		"SELECT phase FROM dns_engine_switch_snapshots WHERE switch_id = ?",
		persisted.SwitchID,
	).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != "rolled_back" {
		t.Fatalf("reconfigure rollback phase=%q", phase)
	}
	marker, err := readDNSEngineOperationMarker(context.Background(), panel.db.GetDB())
	if err != nil || marker != nil {
		t.Fatalf("reconfigure rollback marker=%+v err=%v", marker, err)
	}
}

func TestPDNSReconfigureDirectFailureAcceptsOnlyStablePreimageProof(t *testing.T) {
	t.Run("exact restored preimage detaches", func(t *testing.T) {
		panel, agent := newLegacyPDNSSecondaryPanelForTest(t)
		agent.switchError = "DNS engine switch did not complete; inspect the agent log"
		preview, recorder := requestDNSEnginePreview(
			t, panel, transport.DNSEnginePowerDNS, nil, 1,
		)
		if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
			preview.Action != "reconfigure" {
			t.Fatalf("reconfigure preview=%+v status=%d body=%s",
				preview, recorder.Code, recorder.Body.String())
		}
		requestID := strings.Repeat("a", 32)
		response := commitDNSEngineSwitch(
			t, panel, requestID, transport.DNSEnginePowerDNS,
			nil, 1, preview.PreviewToken, true,
		)
		var body apiErrorBody
		if response.Code != http.StatusConflict ||
			json.Unmarshal(response.Body.Bytes(), &body) != nil ||
			body.Code != errCodeDNSEngineChangeNotCommitted {
			t.Fatalf("direct rollback status=%d body=%s", response.Code, response.Body.String())
		}
		persisted, err := readDNSEngineSwitchByRequest(
			context.Background(), panel.db.GetDB(), requestID,
		)
		if err != nil {
			t.Fatal(err)
		}
		assertPDNSReconfigureRolledBack(t, panel, persisted)
		agent.mu.Lock()
		defer agent.mu.Unlock()
		if agent.rollbackEvidenceCalls != 2 || len(agent.rollbackEvidenceRequests) != 2 {
			t.Fatalf("direct evidence calls=%d requests=%d",
				agent.rollbackEvidenceCalls, len(agent.rollbackEvidenceRequests))
		}
		for _, request := range agent.rollbackEvidenceRequests {
			if request.MutationRequestID != persisted.RequestID ||
				request.MutationOwnerID != persisted.OwnerID ||
				request.SourceEngine != "" ||
				request.TargetEngine != transport.DNSEnginePowerDNS ||
				request.PairRole != transport.DNSPairRoleSecondary ||
				len(request.Zones) != 0 {
				t.Fatalf("direct evidence lost frozen preimage identity: %+v", request)
			}
		}
	})

	t.Run("missing preimage proof retains attachment", func(t *testing.T) {
		panel, agent := newLegacyPDNSSecondaryPanelForTest(t)
		agent.switchError = "DNS engine switch did not complete; inspect the agent log"
		agent.rollbackEvidenceOutcome = transport.DNSEngineRollbackIdentityMismatch
		preview, _ := requestDNSEnginePreview(
			t, panel, transport.DNSEnginePowerDNS, nil, 1,
		)
		requestID := strings.Repeat("b", 32)
		response := commitDNSEngineSwitch(
			t, panel, requestID, transport.DNSEnginePowerDNS,
			nil, 1, preview.PreviewToken, true,
		)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("unproven direct rollback status=%d body=%s",
				response.Code, response.Body.String())
		}
		persisted, err := readDNSEngineSwitchByRequest(
			context.Background(), panel.db.GetDB(), requestID,
		)
		if err != nil {
			t.Fatal(err)
		}
		state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
		if err != nil {
			t.Fatal(err)
		}
		if state.CurrentSwitchID != persisted.SwitchID || state.ActiveEngine != "" {
			t.Fatalf("unproven direct rollback detached state: %+v", state)
		}
	})
}

func TestPDNSReconfigureStartupRecoveryAcceptsExactRestoration(t *testing.T) {
	panel, agent := newLegacyPDNSSecondaryPanelForTest(t)
	persisted := persistLegacyPDNSSecondaryReconfigureForTest(
		t, panel, strings.Repeat("c", 32),
	)
	job := terminalDNSEngineJob(persisted, agentMutationFailed)
	handled, err := panel.recoverDNSEngineSwitchLocked(context.Background(), job)
	if err != nil || !handled {
		t.Fatalf("startup reconfigure rollback handled=%v err=%v", handled, err)
	}
	assertPDNSReconfigureRolledBack(t, panel, persisted)
	agent.mu.Lock()
	evidenceCalls := agent.rollbackEvidenceCalls
	agent.mu.Unlock()
	if evidenceCalls != 2 {
		t.Fatalf("startup reconfigure evidence calls=%d", evidenceCalls)
	}
}

func TestPDNSReconfigureManualReconcileAcceptsExactRestoration(t *testing.T) {
	panel, agent := newLegacyPDNSSecondaryPanelForTest(t)
	persisted := persistLegacyPDNSSecondaryReconfigureForTest(
		t, panel, strings.Repeat("d", 32),
	)
	job := terminalDNSEngineJob(persisted, agentMutationFailed)
	setDNSEngineMutationJobForTest(t, agent, job, false)
	response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if response.Code != http.StatusOK {
		t.Fatalf("manual reconfigure reconcile status=%d body=%s",
			response.Code, response.Body.String())
	}
	var outcome map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if len(outcome) != 1 || !outcome["reconciled"] {
		t.Fatalf("manual reconfigure reconcile outcome=%v", outcome)
	}
	assertPDNSReconfigureRolledBack(t, panel, persisted)
	agent.mu.Lock()
	evidenceCalls := agent.rollbackEvidenceCalls
	agent.mu.Unlock()
	if evidenceCalls != 2 {
		t.Fatalf("manual reconfigure evidence calls=%d", evidenceCalls)
	}
}

func TestFreshSourceEmptyPDNSNeverUsesReconfigureRestorationException(t *testing.T) {
	panel, agent := newLegacyPDNSSecondaryPanelForTest(t)
	persisted := persistLegacyPDNSSecondaryReconfigureForTest(
		t, panel, strings.Repeat("e", 32),
	)
	marker, err := readDNSEngineOperationMarker(context.Background(), panel.db.GetDB())
	if err != nil || marker == nil {
		t.Fatalf("read fresh-source marker=%+v err=%v", marker, err)
	}
	marker.Action = "install"
	raw, err := encodeDNSEngineOperationMarker(*marker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(
		"UPDATE panel_settings SET value = ? WHERE key = ?",
		raw, dnsEngineOperationSetting,
	); err != nil {
		t.Fatal(err)
	}
	persisted.Action = "install"
	if err := validateLegacyPDNSPairSecondaryReconfigureScope(persisted); err == nil {
		t.Fatal("fresh source-empty install entered reconfigure scope")
	}
	if err := panel.verifyDNSEngineRollbackOutcome(
		context.Background(), persisted,
	); err == nil {
		t.Fatal("fresh source-empty install accepted a running PowerDNS authority")
	}
	response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("fresh source-empty manual reconcile status=%d body=%s",
			response.Code, response.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSwitchID != persisted.SwitchID || state.ActiveEngine != "" {
		t.Fatalf("fresh source-empty exception detached authority: %+v", state)
	}
	agent.mu.Lock()
	evidenceCalls := agent.rollbackEvidenceCalls
	agent.mu.Unlock()
	if evidenceCalls != 0 {
		t.Fatalf("fresh source-empty install requested reconfigure evidence %d time(s)", evidenceCalls)
	}
}
