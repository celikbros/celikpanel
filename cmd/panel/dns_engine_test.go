package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type dnsEngineTestAgent struct {
	durableMutationRPCFixture
	mu                         sync.Mutex
	runtimes                   map[transport.DNSEngine]transport.DNSBackendRuntimeState
	port53Conflict             bool
	readinessCalls             int
	onReadiness                func(int)
	readinessAfterSwitchError  string
	dnssec                     bool
	dnssecUnavailable          bool
	dnssecCalls                int
	switchCalls                int
	switchRequests             []transport.SwitchDNSEngineV1Request
	switchError                string
	switchErrorLeavesPackage   bool
	onSwitch                   func()
	firewallEnabled            bool
	firewallError              string
	firewallCalls              int
	firewallRequests           []transport.ApplyFirewallRequest
	scanError                  error
	omitDNSCapabilities        bool
	rollbackEvidenceOutcome    string
	rollbackEvidenceOutcomes   map[int]string
	rollbackEvidenceCommitment string
	rollbackEvidenceCalls      int
	rollbackEvidenceRequests   []transport.DNSEngineRollbackEvidenceRequest
	onRollbackEvidence         func(int)
	mutationStatusCalls        int
	configurePDNSCalls         int
	configurePDNSRequests      []transport.ServiceMutationRequest
	configurePDNSError         string
	syncV3Requests             []transport.SyncDNSZoneV3Request
	syncV3Errors               map[string]string
	events                     []string
	onConfigurePDNS            func()
	onSyncV3                   func(transport.SyncDNSZoneV3Request)
}

func TestPresentDNSEngineOperationSnapshotFieldsAndTimestamps(t *testing.T) {
	op, err := presentDNSEngineOperation(persistedDNSEngineSwitch{
		SwitchID: "operation-id", TargetEngine: transport.DNSEngineBIND,
		Phase: "rolling_back", LastError: "bounded failure detail",
		CreatedAt: "2026-08-26 14:30:45",
		UpdatedAt: "2026-08-26T17:31:46+03:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if op.ID != "operation-id" || op.TargetEngine != transport.DNSEngineBIND ||
		op.Phase != "rolling_back" || op.Status != "rolling_back" ||
		op.StartedAt != "2026-08-26T14:30:45Z" ||
		op.UpdatedAt != "2026-08-26T14:31:46Z" ||
		op.LastError != "bounded failure detail" {
		t.Fatalf("operation snapshot=%+v", op)
	}
}

func TestDNSEngineOperationStatusByPhase(t *testing.T) {
	tests := map[string]string{
		"planned": "running", "staging": "running", "staged": "running",
		"activating": "running", "verifying": "running",
		"rolling_back": "rolling_back", "committed": "succeeded",
		"rolled_back": "rolled_back", "failed": "failed",
	}
	for phase, want := range tests {
		if got, err := dnsEngineOperationStatus(phase); err != nil || got != want {
			t.Errorf("phase=%q status=%q want=%q err=%v", phase, got, want, err)
		}
	}
	if got, err := dnsEngineOperationStatus("unknown"); err == nil || got != "" {
		t.Fatalf("unknown phase status=%q err=%v", got, err)
	}
}

func TestReadPresentedDNSEngineOperationUsesAttachedSwitch(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEngineBIND, strings.Repeat("9", 32),
	)
	operation, err := readPresentedDNSEngineOperation(
		context.Background(), panel.db.GetDB(), persisted.SwitchID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if operation == nil || operation.ID != persisted.SwitchID ||
		operation.TargetEngine != transport.DNSEngineBIND ||
		operation.Phase != "activating" || operation.Status != "running" ||
		operation.StartedAt == "" || operation.UpdatedAt == "" {
		t.Fatalf("attached operation snapshot=%+v", operation)
	}
	if _, err := time.Parse(time.RFC3339, operation.StartedAt); err != nil {
		t.Fatalf("started_at=%q: %v", operation.StartedAt, err)
	}
	if _, err := time.Parse(time.RFC3339, operation.UpdatedAt); err != nil {
		t.Fatalf("updated_at=%q: %v", operation.UpdatedAt, err)
	}
}

func TestEnrichAttachedDNSEngineOperationShowsFailedReceipt(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEngineBIND, strings.Repeat("8", 32),
	)
	job := terminalDNSEngineJob(persisted, agentMutationFailed)
	job.ErrorMessage = "  host mutation failed safely  "
	setDNSEngineMutationJobForTest(t, agent, job, false)
	operation, err := readPresentedDNSEngineOperation(
		context.Background(), panel.db.GetDB(), persisted.SwitchID,
	)
	if err != nil {
		t.Fatal(err)
	}
	panel.enrichAttachedDNSEngineOperation(
		context.Background(), operation, persisted.SwitchID,
	)
	if operation.Status != "recovery_required" ||
		operation.LastError != "host mutation failed safely" {
		t.Fatalf("enriched operation=%+v", operation)
	}
	for name, unsafe := range map[string]string{
		"multiline": "internal path\nsecret detail",
		"oversized": strings.Repeat("x", 513),
	} {
		t.Run(name, func(t *testing.T) {
			job.ErrorMessage = unsafe
			setDNSEngineMutationJobForTest(t, agent, job, false)
			candidate, err := readPresentedDNSEngineOperation(
				context.Background(), panel.db.GetDB(), persisted.SwitchID,
			)
			if err != nil {
				t.Fatal(err)
			}
			panel.enrichAttachedDNSEngineOperation(
				context.Background(), candidate, persisted.SwitchID,
			)
			if candidate.LastError !=
				"The privileged DNS operation failed before the panel could finalize it." {
				t.Fatalf("unsafe receipt was exposed: %+v", candidate)
			}
		})
	}
}

func TestDNSEngineGenericReconcileFinalizesSucceededReceipt(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEngineBIND, strings.Repeat("b", 32),
	)
	agent.mu.Lock()
	target := agent.runtimes[transport.DNSEngineBIND]
	target.Installed, target.Running, target.Managed = true, true, true
	agent.runtimes[transport.DNSEngineBIND] = target
	agent.mu.Unlock()
	setDNSEngineMutationJobForTest(
		t, agent, terminalDNSEngineJob(persisted, agentMutationSucceeded), false,
	)

	response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status=%d body=%s", response.Code, response.Body.String())
	}
	var outcome map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if len(outcome) != 1 || !outcome["reconciled"] {
		t.Fatalf("reconcile outcome=%v body=%s", outcome, response.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND || state.EngineEpoch != 1 ||
		state.CurrentSwitchID != "" || state.Revision != 2 {
		t.Fatalf("reconciled state=%+v", state)
	}
	var phase string
	if err := panel.db.GetDB().QueryRow(`
		SELECT phase FROM dns_engine_switch_snapshots WHERE switch_id = ?`,
		persisted.SwitchID,
	).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != "committed" {
		t.Fatalf("reconciled phase=%q", phase)
	}
	agent.mu.Lock()
	statusCalls := agent.mutationStatusCalls
	agent.mu.Unlock()
	if statusCalls != 1 {
		t.Fatalf("generic reconcile mutation status calls=%d", statusCalls)
	}
	if !panel.serviceMutationMu.TryLock() {
		t.Fatal("generic reconciliation retained the service mutation lock")
	}
	panel.serviceMutationMu.Unlock()
}

func TestDNSEngineGenericReconcileReportsPendingPostCommit(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.firewallEnabled = true
	agent.firewallError = "secret nftables stderr /etc/nftables.conf"
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEngineBIND, strings.Repeat("c", 32),
	)
	agent.mu.Lock()
	target := agent.runtimes[transport.DNSEngineBIND]
	target.Installed, target.Running, target.Managed = true, true, true
	agent.runtimes[transport.DNSEngineBIND] = target
	agent.mu.Unlock()
	setDNSEngineMutationJobForTest(
		t, agent, terminalDNSEngineJob(persisted, agentMutationSucceeded), false,
	)

	for attempt := 1; attempt <= 2; attempt++ {
		response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
		var body apiErrorBody
		if response.Code != http.StatusBadGateway ||
			json.Unmarshal(response.Body.Bytes(), &body) != nil ||
			!body.PartialSuccess || !body.MutationApplied ||
			strings.Contains(response.Body.String(), "nftables.conf") {
			t.Fatalf("attempt %d pending status=%d body=%s",
				attempt, response.Code, response.Body.String())
		}
		state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
		if err != nil {
			t.Fatal(err)
		}
		if state.ActiveEngine != transport.DNSEngineBIND ||
			state.CurrentSwitchID != "" {
			t.Fatalf("attempt %d changed committed state=%+v", attempt, state)
		}
		marker, err := readDNSEngineOperationMarker(
			context.Background(), panel.db.GetDB(),
		)
		if err != nil || marker == nil ||
			marker.Phase != dnsEngineOperationPostCommit {
			t.Fatalf("attempt %d marker=%+v err=%v", attempt, marker, err)
		}
	}
	agent.mu.Lock()
	agent.firewallError = ""
	agent.mu.Unlock()
	response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if response.Code != http.StatusOK {
		t.Fatalf("completed retry status=%d body=%s",
			response.Code, response.Body.String())
	}
	marker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker != nil {
		t.Fatalf("completed retry marker=%+v err=%v", marker, err)
	}
}

func TestDNSEngineSnapshotRejectsConcurrentOperationAttach(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	var persisted persistedDNSEngineSwitch
	agent.onReadiness = func(call int) {
		if call == 1 {
			persisted = persistEmptyDNSEngineSwitchForTest(
				t, panel, transport.DNSEngineBIND, strings.Repeat("d", 32),
			)
		}
	}
	if snapshot, err := panel.dnsEngineSnapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "state changed") {
		t.Fatalf("torn snapshot=%+v err=%v", snapshot, err)
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.SwitchID == "" || state.CurrentSwitchID != persisted.SwitchID {
		t.Fatalf("concurrent operation was not attached: %+v", state)
	}
}

func newDNSEngineTestAgent() *dnsEngineTestAgent {
	return &dnsEngineTestAgent{runtimes: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
		transport.DNSEnginePowerDNS: {
			Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service",
		},
		transport.DNSEngineBIND: {
			Engine: transport.DNSEngineBIND, Unit: "bind9.service",
		},
	}}
}

func (agent *dnsEngineTestAgent) Version(
	_ *transport.Empty,
	response *transport.AgentVersionResponse,
) error {
	if agent.omitDNSCapabilities {
		response.Capabilities = []string{transport.AgentCapabilityFirewallApplyV2}
		return nil
	}
	response.Capabilities = []string{
		transport.AgentCapabilityDNSZoneSyncV3,
		transport.AgentCapabilityDNSZoneRecoverV1,
		transport.AgentCapabilityDNSEngineSwitchV1,
		transport.AgentCapabilityFirewallApplyV2,
	}
	return nil
}

func (agent *dnsEngineTestAgent) HostPlatform(
	_ *transport.Empty,
	response *transport.HostPlatformResponse,
) error {
	*response = debianPolicyTestIdentity()
	return nil
}

func (agent *dnsEngineTestAgent) installedServiceIDsLocked() []string {
	var installed []string
	for engine, runtime := range agent.runtimes {
		if runtime.Installed {
			installed = append(installed, string(engine))
		}
	}
	return installed
}

func (agent *dnsEngineTestAgent) GetServices(
	_ *transport.Empty,
	response *[]core.Service,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.scanError != nil {
		return agent.scanError
	}
	*response = (*response)[:0]
	for engine, runtime := range agent.runtimes {
		if !runtime.Running {
			continue
		}
		unit := runtime.Unit
		if unit == "" {
			unit = string(engine)
		}
		*response = append(*response, core.Service{
			Name:   strings.TrimSuffix(unit, ".service"),
			Status: "active (running)",
		})
	}
	return nil
}

func (agent *dnsEngineTestAgent) InstalledServiceIDs(
	_ *transport.Empty,
	response *[]string,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.scanError != nil {
		return agent.scanError
	}
	*response = append((*response)[:0], agent.installedServiceIDsLocked()...)
	return nil
}

func (agent *dnsEngineTestAgent) InstalledServiceIDsStrict(
	_ *transport.Empty,
	response *[]string,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	*response = append((*response)[:0], agent.installedServiceIDsLocked()...)
	return nil
}

func (agent *dnsEngineTestAgent) InstalledRepoPackages(
	_ *transport.InstalledRepoPackagesRequest,
	response *transport.InstalledRepoPackagesResponse,
) error {
	response.Packages = []string{}
	return nil
}

func (agent *dnsEngineTestAgent) ListServiceInstances(
	_ *transport.ServiceInstancesRequest,
	response *transport.ServiceInstancesResponse,
) error {
	response.Instances = []core.ServiceInstance{}
	return nil
}

func (agent *dnsEngineTestAgent) FirewallStatus(
	_ *transport.Empty,
	response *FirewallStatusResp,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	response.Enabled = agent.firewallEnabled
	response.EngineAvailable = true
	return nil
}

func (agent *dnsEngineTestAgent) ApplyFirewallV2(
	request *transport.ApplyFirewallRequest,
	response *FirewallStatusResp,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.firewallCalls++
	agent.firewallRequests = append(
		agent.firewallRequests,
		transport.ApplyFirewallRequest{
			ServiceMutationBinding: request.ServiceMutationBinding,
			Enabled:                request.Enabled, Persist: request.Persist,
			TCPPorts: append([]int(nil), request.TCPPorts...),
			UDPPorts: append([]int(nil), request.UDPPorts...),
		},
	)
	response.Enabled = request.Enabled
	response.EngineAvailable = true
	response.Error = agent.firewallError
	if response.Error != "" {
		return nil
	}
	if !request.Enabled {
		return errors.New("DNS engine post-commit unexpectedly disabled firewall")
	}
	agent.firewallEnabled = true
	return nil
}

func (agent *dnsEngineTestAgent) DNSBackendReadiness(
	_ *transport.Empty,
	response *transport.DNSBackendReadinessResponse,
) error {
	agent.mu.Lock()
	agent.readinessCalls++
	call := agent.readinessCalls
	response.Engines = []transport.DNSBackendRuntimeState{
		agent.runtimes[transport.DNSEnginePowerDNS],
		agent.runtimes[transport.DNSEngineBIND],
	}
	response.Port53Conflict = agent.port53Conflict
	if agent.switchCalls > 0 && agent.readinessAfterSwitchError != "" {
		response.Error = agent.readinessAfterSwitchError
	}
	hook := agent.onReadiness
	agent.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	return nil
}

func (agent *dnsEngineTestAgent) DNSEngineRollbackEvidenceV1(
	request *transport.DNSEngineRollbackEvidenceRequest,
	response *transport.DNSEngineRollbackEvidenceResponse,
) error {
	agent.mu.Lock()
	agent.rollbackEvidenceCalls++
	call := agent.rollbackEvidenceCalls
	copy := *request
	copy.Zones = append(
		[]transport.DNSEngineSwitchZoneSnapshot(nil), request.Zones...,
	)
	agent.rollbackEvidenceRequests = append(
		agent.rollbackEvidenceRequests, copy,
	)
	response.Outcome = agent.rollbackEvidenceOutcome
	if outcome := agent.rollbackEvidenceOutcomes[call]; outcome != "" {
		response.Outcome = outcome
	}
	if response.Outcome == "" {
		response.Outcome = transport.DNSEngineRollbackSafe
	}
	if response.Outcome == transport.DNSEngineRollbackSafe {
		response.ReceiptCommitment = agent.rollbackEvidenceCommitment
		if response.ReceiptCommitment == "" {
			response.ReceiptCommitment = strings.Repeat("a", 64)
		}
	}
	hook := agent.onRollbackEvidence
	agent.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	return nil
}

func (agent *dnsEngineTestAgent) DNSSECStatus(
	_ *transport.DNSSECRequest,
	response *transport.DNSSECStatusResponse,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.dnssecCalls++
	if agent.dnssecUnavailable {
		// The real agent's answer whenever PowerDNS is not the active engine.
		// Gerçek agent'ın PowerDNS etkin motor olmadığında verdiği yanıt.
		response.Error = "DNSSEC status is unavailable because PowerDNS is not the active DNS engine"
		return nil
	}
	response.Secured = agent.dnssec
	if agent.dnssec {
		response.DS = []string{"12345 13 2 AABBCC"}
	}
	return nil
}

func (agent *dnsEngineTestAgent) SwitchDNSEngineV1(
	request *transport.SwitchDNSEngineV1Request,
	response *transport.SwitchDNSEngineV1Response,
) error {
	if agent.onSwitch != nil {
		agent.onSwitch()
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.switchCalls++
	agent.events = append(agent.events, "switch")
	copy := *request
	copy.Zones = append([]transport.DNSEngineSwitchZoneSnapshot(nil), request.Zones...)
	agent.switchRequests = append(agent.switchRequests, copy)
	if agent.switchError != "" {
		if agent.switchErrorLeavesPackage {
			target := agent.runtimes[request.TargetEngine]
			target.Installed, target.Running, target.Managed = true, false, true
			agent.runtimes[request.TargetEngine] = target
		}
		response.Error = agent.switchError
		return nil
	}
	for engine, runtime := range agent.runtimes {
		if engine == request.TargetEngine {
			runtime.Installed, runtime.Running, runtime.Managed = true, true, true
			runtime.PairReady = request.Topology == transport.DNSTopologyPaired &&
				request.PairRole == transport.DNSPairRolePrimary
		} else {
			runtime.Running = false
			runtime.PairReady = false
		}
		agent.runtimes[engine] = runtime
	}
	response.Applied = true
	response.ActiveEngine = request.TargetEngine
	response.ActiveEpoch = request.TargetEpoch
	response.AppliedZones = len(request.Zones)
	return nil
}

func (agent *dnsEngineTestAgent) ConfigurePowerDNSSQLite(
	request *transport.ServiceMutationRequest,
	response *transport.SyncDNSZoneResponse,
) error {
	agent.mu.Lock()
	agent.configurePDNSCalls++
	agent.events = append(agent.events, "configure")
	agent.configurePDNSRequests = append(
		agent.configurePDNSRequests, *request,
	)
	hook := agent.onConfigurePDNS
	if agent.configurePDNSError != "" {
		response.Error = agent.configurePDNSError
		agent.mu.Unlock()
		if hook != nil {
			hook()
		}
		return nil
	}
	response.Synced = true
	agent.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (agent *dnsEngineTestAgent) SyncDNSZoneV3(
	request *transport.SyncDNSZoneV3Request,
	response *transport.SyncDNSZoneV3Response,
) error {
	copy := *request
	copy.Records = append([]transport.ZoneRecord(nil), request.Records...)
	agent.mu.Lock()
	agent.syncV3Requests = append(agent.syncV3Requests, copy)
	agent.events = append(agent.events, "sync:"+request.Domain)
	detail := agent.syncV3Errors[request.Domain]
	hook := agent.onSyncV3
	agent.mu.Unlock()
	if hook != nil {
		hook(copy)
	}
	if detail != "" {
		response.Error = detail
		return nil
	}
	response.Synced = true
	response.Engine = request.Engine
	response.EngineEpoch = request.EngineEpoch
	response.AppliedGeneration = request.DesiredGeneration
	return nil
}

func (agent *dnsEngineTestAgent) ServiceMutationStatus(
	request *ServiceOperationMutationStatusRequest,
	response *ServiceOperationMutationResponse,
) error {
	agent.mu.Lock()
	agent.mutationStatusCalls++
	agent.mu.Unlock()
	agent.durableMutationRPCFixture.mu.Lock()
	defer agent.durableMutationRPCFixture.mu.Unlock()
	requestID := request.RequestID
	if requestID == "" {
		requestID = agent.durableMutationRPCFixture.active
	}
	response.Job = cloneServiceOperationMutationJob(
		agent.durableMutationRPCFixture.jobs[requestID],
	)
	return nil
}

func attachDNSEngineTestAgent(t *testing.T, panel *Panel, agent *dnsEngineTestAgent) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

func requestDNSEnginePreview(
	t *testing.T,
	panel *Panel,
	target transport.DNSEngine,
	source any,
	revision int64,
) (dnsEngineSwitchPreview, *httptest.ResponseRecorder) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"target_engine": target, "expected_source": source,
		"expected_revision": revision,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/dns/engine/switch/preview",
		strings.NewReader(string(body)),
	)
	panel.handleDNSEngineSwitchPreview(recorder, request)
	var preview dnsEngineSwitchPreview
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil {
			t.Fatalf("decode preview: %v body=%s", err, recorder.Body.String())
		}
	}
	return preview, recorder
}

func hasDNSEngineBlocker(preview dnsEngineSwitchPreview, code string) bool {
	for _, blocker := range preview.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func commitDNSEngineSwitch(
	t *testing.T,
	panel *Panel,
	requestID string,
	target transport.DNSEngine,
	source any,
	revision int64,
	token string,
	acknowledged bool,
) *httptest.ResponseRecorder {
	t.Helper()
	if _, err := panel.db.GetDB().Exec(`
		INSERT OR IGNORE INTO users (
		  id, username, password_hash, email, role
		) VALUES (1, 'dns-engine-admin', 'hash',
		          'dns-engine-admin@example.test', 'admin')
	`); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"request_id": requestID, "target_engine": target,
		"expected_source": source, "expected_revision": revision,
		"preview_token":         token,
		"downtime_acknowledged": acknowledged,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/dns/engine/switch",
		strings.NewReader(string(body)),
	)
	request.RemoteAddr = "198.51.100.44:54321"
	request.Header.Set("User-Agent", "dns-engine-test-client")
	request = request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin},
	))
	panel.handleDNSEngineSwitch(recorder, request)
	return recorder
}

func intSliceContains(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertCachedDNSEngineInstalled(
	t *testing.T,
	panel *Panel,
	engine transport.DNSEngine,
) {
	t.Helper()
	var raw string
	if err := panel.db.GetDB().QueryRow(
		`SELECT data FROM service_scan_cache WHERE id = 1`,
	).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	snapshot, err := decodeScanCacheSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range snapshot.Observations {
		if observation.ID == string(engine) {
			if !observation.IsInstalled ||
				!strings.HasPrefix(observation.Status, "active") {
				t.Fatalf(
					"cached %s observation=%+v, want installed active",
					engine, observation,
				)
			}
			return
		}
	}
	t.Fatalf("cached scan has no %s observation", engine)
}

func TestDNSEngineFreshInstallPostCommitSyncsFirewallAndCache(t *testing.T) {
	for index, target := range []transport.DNSEngine{
		transport.DNSEngineBIND,
		transport.DNSEnginePowerDNS,
	} {
		t.Run(string(target), func(t *testing.T) {
			panel := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, panel, "standalone")
			agent := newDNSEngineTestAgent()
			agent.firewallEnabled = true
			attachDNSEngineTestAgent(t, panel, agent)
			preview, recorder := requestDNSEnginePreview(
				t, panel, target, nil, 0,
			)
			if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
				t.Fatalf(
					"preview status=%d body=%s",
					recorder.Code, recorder.Body.String(),
				)
			}
			commit := commitDNSEngineSwitch(
				t, panel, strings.Repeat(string(rune('a'+index)), 32),
				target, nil, 0, preview.PreviewToken, false,
			)
			if commit.Code != http.StatusOK {
				t.Fatalf(
					"commit status=%d body=%s",
					commit.Code, commit.Body.String(),
				)
			}
			agent.mu.Lock()
			calls := agent.firewallCalls
			requests := append(
				[]transport.ApplyFirewallRequest(nil),
				agent.firewallRequests...,
			)
			agent.mu.Unlock()
			if calls != 1 || len(requests) != 1 ||
				!requests[0].Enabled ||
				!intSliceContains(requests[0].TCPPorts, 53) ||
				!intSliceContains(requests[0].UDPPorts, 53) {
				t.Fatalf(
					"firewall calls=%d requests=%+v, want exact TCP+UDP 53",
					calls, requests,
				)
			}
			assertCachedDNSEngineInstalled(t, panel, target)
		})
	}
}

func TestDNSEnginePostCommitNeverEnablesDisabledFirewall(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	preview, _ := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("9", 32),
		transport.DNSEngineBIND, nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	agent.mu.Lock()
	firewallCalls := agent.firewallCalls
	agent.mu.Unlock()
	if firewallCalls != 0 {
		t.Fatalf("disabled firewall received %d apply calls", firewallCalls)
	}
	assertCachedDNSEngineInstalled(t, panel, transport.DNSEngineBIND)
}

func TestDNSEngineExistingSourceSwitchRetainsPostCommitBehavior(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.firewallEnabled = true
	attachDNSEngineTestAgent(t, panel, agent)

	firstPreview, _ := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 0,
	)
	first := commitDNSEngineSwitch(
		t, panel, strings.Repeat("6", 32),
		transport.DNSEnginePowerDNS, nil, 0,
		firstPreview.PreviewToken, false,
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first install status=%d body=%s", first.Code, first.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	secondPreview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND,
		transport.DNSEnginePowerDNS, state.Revision,
	)
	if recorder.Code != http.StatusOK || len(secondPreview.Blockers) != 0 {
		t.Fatalf(
			"switch preview=%+v status=%d body=%s",
			secondPreview, recorder.Code, recorder.Body.String(),
		)
	}
	second := commitDNSEngineSwitch(
		t, panel, strings.Repeat("7", 32),
		transport.DNSEngineBIND, transport.DNSEnginePowerDNS,
		state.Revision, secondPreview.PreviewToken, true,
	)
	if second.Code != http.StatusOK {
		t.Fatalf("switch status=%d body=%s", second.Code, second.Body.String())
	}
	state, err = readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 2 {
		t.Fatalf("switched state=%+v", state)
	}
	agent.mu.Lock()
	firewallCalls := agent.firewallCalls
	requests := append(
		[]transport.ApplyFirewallRequest(nil), agent.firewallRequests...,
	)
	agent.mu.Unlock()
	if firewallCalls != 2 || len(requests) != 2 ||
		!intSliceContains(requests[1].TCPPorts, 53) ||
		!intSliceContains(requests[1].UDPPorts, 53) {
		t.Fatalf(
			"existing-source firewall calls=%d requests=%+v",
			firewallCalls, requests,
		)
	}
	assertCachedDNSEngineInstalled(t, panel, transport.DNSEngineBIND)
}

func TestDNSEnginePostCommitFailureIsRetryableWithoutRollbackOrReplay(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.firewallEnabled = true
	agent.firewallError = "secret nftables stderr /etc/nftables.conf"
	attachDNSEngineTestAgent(t, panel, agent)
	preview, _ := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	requestID := strings.Repeat("8", 32)
	first := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if first.Code != http.StatusBadGateway {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeFirewallSyncFailed ||
		body.Action != "/services" ||
		!body.PartialSuccess || !body.MutationApplied ||
		strings.Contains(first.Body.String(), "nftables.conf") {
		t.Fatalf("unsafe/non-actionable partial response=%+v body=%s", body, first.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 1 || state.CurrentSwitchID != "" {
		t.Fatalf("firewall failure rolled engine back: %+v", state)
	}
	marker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker == nil ||
		marker.Phase != dnsEngineOperationPostCommit {
		t.Fatalf("pending post-commit marker=%+v err=%v", marker, err)
	}
	agent.mu.Lock()
	agent.firewallError = ""
	switchCallsBefore := agent.switchCalls
	agent.mu.Unlock()
	second := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if second.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", second.Code, second.Body.String())
	}
	agent.mu.Lock()
	switchCallsAfter := agent.switchCalls
	firewallCalls := agent.firewallCalls
	agent.mu.Unlock()
	if switchCallsBefore != 1 || switchCallsAfter != 1 ||
		firewallCalls != 2 {
		t.Fatalf(
			"retry switch before/after=%d/%d firewall=%d",
			switchCallsBefore, switchCallsAfter, firewallCalls,
		)
	}
	marker, err = readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker != nil {
		t.Fatalf("successful retry retained marker=%+v err=%v", marker, err)
	}
	var accepted, succeeded, recovered int
	if err := panel.db.GetDB().QueryRow(`
		SELECT
		  COALESCE(SUM(action LIKE 'dns.engine.switch.accepted %'), 0),
		  COALESCE(SUM(action LIKE 'dns.engine.switch.succeeded %'), 0),
		  COALESCE(SUM(action LIKE 'dns.engine.switch.post_commit.recovered %'), 0)
		FROM audit_logs
	`).Scan(&accepted, &succeeded, &recovered); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || succeeded != 1 || recovered != 1 {
		t.Fatalf(
			"audit accepted/succeeded/recovered=%d/%d/%d",
			accepted, succeeded, recovered,
		)
	}
	var userID int
	var ip, userAgent string
	if err := panel.db.GetDB().QueryRow(`
		SELECT user_id, ip_address, user_agent
		FROM audit_logs
		WHERE action LIKE 'dns.engine.switch.accepted %'
	`).Scan(&userID, &ip, &userAgent); err != nil {
		t.Fatal(err)
	}
	if userID != 1 || ip != "198.51.100.44" ||
		userAgent != "dns-engine-test-client" {
		t.Fatalf("accepted actor=%d/%q/%q", userID, ip, userAgent)
	}
}

func TestDNSEngineScanFailureIsCommittedAndSafelyRetryable(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.scanError = errors.New("secret service probe /proc/999/fd")
	attachDNSEngineTestAgent(t, panel, agent)
	preview, _ := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	requestID := strings.Repeat("5", 32)
	first := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	var body apiErrorBody
	if first.Code != http.StatusBadGateway ||
		json.Unmarshal(first.Body.Bytes(), &body) != nil ||
		body.Code != errCodeServiceStateRefreshFailed ||
		body.Action != "/services" ||
		!body.PartialSuccess || !body.MutationApplied ||
		strings.Contains(first.Body.String(), "/proc/999") {
		t.Fatalf("scan partial status=%d body=%s", first.Code, first.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.CurrentSwitchID != "" {
		t.Fatalf("scan failure rolled engine back: %+v", state)
	}
	agent.mu.Lock()
	agent.scanError = nil
	agent.mu.Unlock()
	second := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if second.Code != http.StatusOK {
		t.Fatalf("scan retry status=%d body=%s", second.Code, second.Body.String())
	}
	assertCachedDNSEngineInstalled(t, panel, transport.DNSEngineBIND)
}

func TestDNSEngineAgentRejectionReturnsOnlyVerifiedOutcomeCodes(t *testing.T) {
	tests := []struct {
		name           string
		agentDetail    string
		wantCode       string
		wantMessage    string
		wantDiagnostic string
		leavesPackage  bool
	}{
		{
			name:           "canonical manifest mismatch",
			agentDetail:    "DNS engine switch request is not the exact canonical manifest",
			wantCode:       errCodeDNSEnginePlanRejected,
			wantMessage:    "The DNS agent rejected the reviewed plan. The DNS engine change was not committed. Refresh state before creating a new review.",
			wantDiagnostic: "canonical_manifest_mismatch",
		},
		{
			name:           "unknown detail is omitted",
			agentDetail:    "named-checkconf /etc/bind/private failed: token=do-not-leak",
			wantCode:       errCodeDNSEngineChangeNotCommitted,
			wantMessage:    "The DNS engine change was not committed. The pre-operation serving state was verified; packages or setup files may still have changed. Refresh state before creating a new review.",
			wantDiagnostic: "unclassified_detail_omitted",
			leavesPackage:  true,
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classified := newDNSEngineAgentRejectedError(test.agentDetail)
			if classified.diagnosticCode != test.wantDiagnostic ||
				strings.Contains(classified.diagnosticCode, "/etc/") ||
				strings.Contains(classified.Error(), test.agentDetail) {
				t.Fatalf("unsafe rejection classification=%+v", classified)
			}

			panel := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, panel, "standalone")
			agent := newDNSEngineTestAgent()
			agent.switchError = test.agentDetail
			agent.switchErrorLeavesPackage = test.leavesPackage
			attachDNSEngineTestAgent(t, panel, agent)
			preview, recorder := requestDNSEnginePreview(
				t, panel, transport.DNSEngineBIND, nil, 0,
			)
			if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
				t.Fatalf(
					"preview status=%d body=%s",
					recorder.Code, recorder.Body.String(),
				)
			}
			requestID := strings.Repeat(string(rune('a'+index)), 32)
			commit := commitDNSEngineSwitch(
				t, panel, requestID, transport.DNSEngineBIND,
				nil, 0, preview.PreviewToken, false,
			)
			var body apiErrorBody
			if commit.Code != http.StatusConflict ||
				json.Unmarshal(commit.Body.Bytes(), &body) != nil ||
				body.Code != test.wantCode ||
				body.Error != test.wantMessage ||
				body.PartialSuccess || body.MutationApplied ||
				strings.Contains(commit.Body.String(), test.agentDetail) ||
				strings.Contains(commit.Body.String(), "do-not-leak") ||
				strings.Contains(commit.Body.String(), "/etc/bind") {
				t.Fatalf(
					"unsafe rejection status=%d body=%s",
					commit.Code, commit.Body.String(),
				)
			}
			state, err := readDNSEngineDBState(
				context.Background(), panel.db.GetDB(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if state.ActiveEngine != "" || state.EngineEpoch != 0 ||
				state.CurrentSwitchID != "" {
				t.Fatalf("rejected switch retained an active authority state: %+v", state)
			}
			agent.mu.Lock()
			targetRuntime := agent.runtimes[transport.DNSEngineBIND]
			agent.mu.Unlock()
			if test.leavesPackage &&
				(!targetRuntime.Installed || targetRuntime.Running) {
				t.Fatalf(
					"test did not retain an inactive standby package: %+v",
					targetRuntime,
				)
			}
			var notCommittedAction string
			if err := panel.db.GetDB().QueryRow(
				"SELECT action FROM audit_logs WHERE action LIKE 'dns.engine.switch.change_not_committed %'",
			).Scan(&notCommittedAction); err != nil {
				t.Fatalf("change-not-committed audit missing: %v", err)
			}
			if strings.Contains(notCommittedAction, ".rolled_back ") ||
				strings.Contains(notCommittedAction, ".activation_reverted ") {
				t.Fatalf("audit overclaimed host outcome: %q", notCommittedAction)
			}
		})
	}
}

func TestDNSEngineAppliedRefreshResponseCarriesProofFlags(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeDNSEngineChangeAppliedRefreshRequired(recorder)
	var body apiErrorBody
	if recorder.Code != http.StatusBadGateway ||
		json.Unmarshal(recorder.Body.Bytes(), &body) != nil ||
		body.Code != errCodeDNSEngineChangeAppliedRefresh ||
		!body.PartialSuccess || !body.MutationApplied {
		t.Fatalf(
			"applied refresh response status=%d body=%s",
			recorder.Code, recorder.Body.String(),
		)
	}
}

func TestDNSEngineAppliedReceiptWithUnverifiedFollowupIsPartialSuccess(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.readinessAfterSwitchError = "private readiness detail /proc/123"
	attachDNSEngineTestAgent(t, panel, agent)
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf(
			"preview status=%d body=%s",
			recorder.Code, recorder.Body.String(),
		)
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("e", 32), transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	var body apiErrorBody
	if commit.Code != http.StatusBadGateway ||
		json.Unmarshal(commit.Body.Bytes(), &body) != nil ||
		body.Code != errCodeDNSEngineChangeAppliedRefresh ||
		!body.PartialSuccess || !body.MutationApplied ||
		strings.Contains(commit.Body.String(), "/proc/123") {
		t.Fatalf(
			"applied follow-up status=%d body=%s",
			commit.Code, commit.Body.String(),
		)
	}
}

func TestDNSEngineFirstInstallAndRequestReplay(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
		preview.Action != "install" || preview.SourceEngine != nil {
		t.Fatalf("first install preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	requestID := strings.Repeat("c", 32)
	commit := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	var engine string
	var epoch int64
	if err := panel.db.GetDB().QueryRow(`
		SELECT active_engine, active_epoch FROM dns_engine_state
		WHERE singleton_id = 1`).Scan(&engine, &epoch); err != nil {
		t.Fatal(err)
	}
	if engine != "bind" || epoch != 1 {
		t.Fatalf("committed engine=%s epoch=%d", engine, epoch)
	}
	replay := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	agent.mu.Lock()
	switchCalls, dnssecCalls := agent.switchCalls, agent.dnssecCalls
	agent.mu.Unlock()
	if switchCalls != 1 {
		t.Fatalf("request replay performed %d host switches", switchCalls)
	}
	snapshot, err := panel.dnsEngineSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != dnsEngineStateReady ||
		snapshot.ActiveEngine == nil ||
		*snapshot.ActiveEngine != transport.DNSEngineBIND {
		t.Fatalf("post-switch BIND snapshot=%+v", snapshot)
	}
	agent.mu.Lock()
	postSnapshotDNSSECCalls := agent.dnssecCalls
	agent.mu.Unlock()
	if postSnapshotDNSSECCalls != dnssecCalls {
		t.Fatalf("BIND health called PowerDNS DNSSEC status: before=%d after=%d",
			dnssecCalls, postSnapshotDNSSECCalls)
	}
}

func TestDNSEngineRequestReplayReturnsExactOlderOperation(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)

	firstRequestID := strings.Repeat("1", 32)
	firstPreview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(firstPreview.Blockers) != 0 {
		t.Fatalf("first preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	firstCommit := commitDNSEngineSwitch(
		t, panel, firstRequestID, transport.DNSEngineBIND,
		nil, 0, firstPreview.PreviewToken, false,
	)
	if firstCommit.Code != http.StatusOK {
		t.Fatalf("first commit status=%d body=%s", firstCommit.Code, firstCommit.Body.String())
	}
	var firstSnapshot dnsEngineSnapshot
	if err := json.Unmarshal(firstCommit.Body.Bytes(), &firstSnapshot); err != nil ||
		firstSnapshot.Operation == nil {
		t.Fatalf("decode first snapshot: %v body=%s", err, firstCommit.Body.String())
	}
	firstSwitchID := firstSnapshot.Operation.ID

	current, err := panel.dnsEngineSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondRequestID := strings.Repeat("2", 32)
	secondPreview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		current.Revision,
	)
	if recorder.Code != http.StatusOK || len(secondPreview.Blockers) != 0 {
		t.Fatalf("second preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	secondCommit := commitDNSEngineSwitch(
		t, panel, secondRequestID, transport.DNSEnginePowerDNS,
		transport.DNSEngineBIND, current.Revision,
		secondPreview.PreviewToken, true,
	)
	if secondCommit.Code != http.StatusOK {
		t.Fatalf("second commit status=%d body=%s", secondCommit.Code, secondCommit.Body.String())
	}

	replay := commitDNSEngineSwitch(
		t, panel, firstRequestID, transport.DNSEngineBIND,
		nil, 0, firstPreview.PreviewToken, false,
	)
	if replay.Code != http.StatusOK {
		t.Fatalf("older replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replaySnapshot dnsEngineSnapshot
	if err := json.Unmarshal(replay.Body.Bytes(), &replaySnapshot); err != nil {
		t.Fatalf("decode older replay: %v body=%s", err, replay.Body.String())
	}
	if replaySnapshot.Operation == nil ||
		replaySnapshot.Operation.RequestID != firstRequestID ||
		replaySnapshot.Operation.ID != firstSwitchID ||
		replaySnapshot.Operation.Status != "succeeded" {
		t.Fatalf("older replay operation=%+v, want exact request %s/%s",
			replaySnapshot.Operation, firstRequestID, firstSwitchID)
	}
	if replaySnapshot.ActiveEngine == nil ||
		*replaySnapshot.ActiveEngine != transport.DNSEnginePowerDNS {
		t.Fatalf("older replay current authority=%+v, want PowerDNS", replaySnapshot.ActiveEngine)
	}
}

func TestDNSEngineInitialPairedBINDInstalledStandbyRetriesAsInstall(t *testing.T) {
	t.Setenv(`CELIKPANEL_SERVER_IP`, `72.62.38.15`)
	panel := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	target := agent.runtimes[transport.DNSEngineBIND]
	target.Installed, target.Running, target.Managed = true, false, true
	agent.runtimes[transport.DNSEngineBIND] = target
	attachDNSEngineTestAgent(t, panel, agent)
	seedDNSSetupAuditUser(t, panel)
	stageBody, err := json.Marshal(map[string]string{
		`ns1`: `ns1.celikhost.com`, `ns2`: `ns2.celikhost.com`,
		`role`: `paired`, `peer_ip`: `2.25.80.4`,
		`peer_ns`: `ns2.celikhost.com`,
	})
	if err != nil {
		t.Fatal(err)
	}
	stage := httptest.NewRecorder()
	panel.handleDNSSetup(stage, dnsSetupAdminRequest(string(stageBody)))
	if stage.Code != http.StatusOK {
		t.Fatalf(`Frankfurt identity stage status=%d body=%s`,
			stage.Code, stage.Body.String())
	}
	type acceptedBoundary struct {
		marker                                            *dnsEngineOperationMarker
		state                                             dnsEngineDBState
		phase, pairRole, localIP, localNS, peerIP, peerNS string
		err                                               error
	}
	accepted := make(chan acceptedBoundary, 1)
	agent.onSwitch = func() {
		boundary := acceptedBoundary{}
		boundary.marker, boundary.err = readDNSEngineOperationMarker(
			context.Background(), panel.db.GetDB(),
		)
		if boundary.err == nil {
			boundary.state, boundary.err = readDNSEngineDBState(
				context.Background(), panel.db.GetDB(),
			)
		}
		if boundary.err == nil && boundary.marker != nil {
			boundary.err = panel.db.GetDB().QueryRow(`
				SELECT snapshot.phase, pairing.pair_role,
				       pairing.local_ip, pairing.local_ns,
				       pairing.peer_ip, pairing.peer_ns
				FROM dns_engine_switch_snapshots AS snapshot
				JOIN dns_bind_pair_switches AS pairing
				  ON pairing.switch_id = snapshot.switch_id
				WHERE snapshot.switch_id = ?`,
				boundary.marker.SwitchID,
			).Scan(
				&boundary.phase, &boundary.pairRole,
				&boundary.localIP, &boundary.localNS,
				&boundary.peerIP, &boundary.peerNS,
			)
		}
		accepted <- boundary
	}

	snapshot, err := panel.dnsEngineSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	startRevision := snapshot.Revision
	if snapshot.ActiveEngine != nil || snapshot.EngineEpoch != 0 ||
		startRevision <= 0 ||
		snapshot.State != dnsEngineStateUnconfigured ||
		snapshot.Topology != transport.DNSTopologyPaired ||
		snapshot.PairRole != transport.DNSPairRolePrimary {
		t.Fatalf(`initial Frankfurt snapshot=%+v`, snapshot)
	}
	var bindEntry *dnsEngineEntry
	for index := range snapshot.Engines {
		if snapshot.Engines[index].ID == transport.DNSEngineBIND {
			bindEntry = &snapshot.Engines[index]
			break
		}
	}
	if bindEntry == nil || bindEntry.Status != `installed_standby` ||
		!bindEntry.Installed || bindEntry.Running || !bindEntry.Managed {
		t.Fatalf(`initial Frankfurt BIND entry=%+v`, bindEntry)
	}

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, startRevision,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf(`installed-standby preview status=%d body=%s`,
			recorder.Code, recorder.Body.String())
	}
	if preview.Action != `install` || preview.SourceEngine != nil ||
		preview.RequiresDowntimeAcknowledgement {
		t.Errorf(`installed-standby preview=%+v, want source-free install`, preview)
	}
	if !slices.Equal(preview.Impacts, []string{
		`install_target`, `validate_target`, `publish_zones`, `start_target`,
	}) {
		t.Errorf(`installed-standby impacts=%v`, preview.Impacts)
	}
	requestID := strings.Repeat(`7`, 32)
	commit := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEngineBIND,
		nil, startRevision, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf(`installed-standby commit status=%d body=%s`,
			commit.Code, commit.Body.String())
	}
	boundary := <-accepted
	if boundary.err != nil || boundary.marker == nil {
		t.Fatalf(`accepted agent boundary marker=%+v err=%v`,
			boundary.marker, boundary.err)
	}
	marker := boundary.marker
	if marker.RequestID != requestID ||
		!validServiceOperationID(marker.SwitchID) ||
		marker.SourceEngine != `` ||
		marker.TargetEngine != transport.DNSEngineBIND ||
		marker.Action != `install` ||
		marker.Phase != dnsEngineOperationAccepted {
		t.Fatalf(`accepted operation marker=%+v`, marker)
	}
	if boundary.state.ActiveEngine != `` ||
		boundary.state.EngineEpoch != 0 ||
		boundary.state.Revision != startRevision+1 ||
		boundary.state.CurrentSwitchID != marker.SwitchID ||
		boundary.phase != `activating` {
		t.Fatalf(`accepted durable boundary state=%+v phase=%s`,
			boundary.state, boundary.phase)
	}
	if boundary.pairRole != transport.DNSPairRolePrimary ||
		boundary.localIP != `72.62.38.15` ||
		boundary.localNS != `ns1.celikhost.com` ||
		boundary.peerIP != `2.25.80.4` ||
		boundary.peerNS != `ns2.celikhost.com` {
		t.Fatalf(`accepted paired identity=%s %s/%s %s/%s`,
			boundary.pairRole, boundary.localIP, boundary.localNS,
			boundary.peerIP, boundary.peerNS)
	}
	agent.mu.Lock()
	if agent.switchCalls != 1 || len(agent.switchRequests) != 1 {
		calls, requests := agent.switchCalls, len(agent.switchRequests)
		agent.mu.Unlock()
		t.Fatalf(`agent switch calls=%d requests=%d`, calls, requests)
	}
	request := agent.switchRequests[0]
	agent.mu.Unlock()
	if request.Mode != transport.DNSEngineSwitchModeSwitch ||
		request.SourceEngine != `` ||
		request.TargetEngine != transport.DNSEngineBIND ||
		request.SourceEpoch != 0 || request.TargetEpoch != 1 ||
		request.SourceRevision != startRevision ||
		request.Topology != transport.DNSTopologyPaired ||
		request.PairRole != transport.DNSPairRolePrimary ||
		request.LocalIP != `72.62.38.15` ||
		request.LocalNS != `ns1.celikhost.com` ||
		request.PeerIP != `2.25.80.4` ||
		request.PeerNS != `ns2.celikhost.com` {
		t.Fatalf(`installed-standby agent request=%+v`, request)
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 1 || state.Revision != startRevision+2 ||
		state.CurrentSwitchID != `` ||
		state.Topology != transport.DNSTopologyPaired ||
		state.PairRole != transport.DNSPairRolePrimary ||
		state.LocalIP != `72.62.38.15` ||
		state.LocalNS != `ns1.celikhost.com` ||
		state.PeerIP != `2.25.80.4` ||
		state.PeerNS != `ns2.celikhost.com` {
		t.Fatalf(`installed-standby final state=%+v`, state)
	}
	finalMarker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || finalMarker != nil {
		t.Fatalf(`installed-standby final marker=%+v err=%v`, finalMarker, err)
	}
	for _, outcome := range []string{`accepted`, `succeeded`} {
		wantAction := `dns.engine.switch.` + outcome +
			` request=` + requestID + ` switch=` + marker.SwitchID +
			` source=none target=bind action=install mode=switch`
		var count int
		if err := panel.db.GetDB().QueryRow(
			`SELECT count(*) FROM audit_logs WHERE action = ?`, wantAction,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf(`audit %s count=%d want=1`, wantAction, count)
		}
	}
}

func TestDNSEngineInitialBINDStandbyClassificationIsExact(t *testing.T) {
	pdns := transport.DNSEnginePowerDNS
	standby := transport.DNSBackendRuntimeState{
		Engine:    transport.DNSEngineBIND,
		Installed: true, Managed: true,
	}
	tests := []struct {
		name       string
		snapshot   dnsEngineSnapshot
		target     transport.DNSEngine
		wantAction string
	}{
		{
			name: `exact standalone initial BIND standby`,
			snapshot: dnsEngineSnapshot{
				EngineEpoch: 0, State: dnsEngineStateUnconfigured,
				Topology: transport.DNSTopologyStandalone,
				runtime: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
					transport.DNSEngineBIND: standby,
				},
			},
			target: transport.DNSEngineBIND, wantAction: `install`,
		},
		{
			name: `exact paired initial BIND standby`,
			snapshot: dnsEngineSnapshot{
				EngineEpoch: 0, State: dnsEngineStateUnconfigured,
				Topology: transport.DNSTopologyPaired,
				runtime: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
					transport.DNSEngineBIND: standby,
				},
			},
			target: transport.DNSEngineBIND, wantAction: `install`,
		},
		{
			name: `nonzero durable epoch`,
			snapshot: dnsEngineSnapshot{
				EngineEpoch: 1, State: dnsEngineStateUnconfigured,
				runtime: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
					transport.DNSEngineBIND: standby,
				},
			},
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
		{
			name: `runtime authority is not unconfigured`,
			snapshot: dnsEngineSnapshot{
				EngineEpoch: 0, State: dnsEngineStateUnmanaged,
				runtime: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
					transport.DNSEngineBIND: standby,
				},
			},
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
		{
			name: `durable source exists`,
			snapshot: dnsEngineSnapshot{
				EngineEpoch: 1, ActiveEngine: &pdns, State: dnsEngineStateReady,
				runtime: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
					transport.DNSEngineBIND: standby,
				},
			},
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
		{
			name: `BIND is already running`,
			snapshot: dnsEngineSnapshot{
				EngineEpoch: 0, State: dnsEngineStateUnmanaged,
				runtime: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
					transport.DNSEngineBIND: {
						Engine:    transport.DNSEngineBIND,
						Installed: true, Running: true, Managed: true,
					},
				},
			},
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dnsEngineAction(test.snapshot, test.target); got != test.wantAction {
				t.Fatalf(`action=%s want=%s snapshot=%+v`,
					got, test.wantAction, test.snapshot)
			}
		})
	}
}

func TestDNSEngineSourceFreeRuntimeAuthorityCannotStartMutation(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*dnsEngineTestAgent)
		wantAction string
	}{
		{
			name: `running BIND`, wantAction: `switch`,
			configure: func(agent *dnsEngineTestAgent) {
				runtime := agent.runtimes[transport.DNSEngineBIND]
				runtime.Installed, runtime.Running, runtime.Managed = true, true, true
				agent.runtimes[transport.DNSEngineBIND] = runtime
			},
		},
		{
			name: `other engine running with BIND standby`, wantAction: `switch`,
			configure: func(agent *dnsEngineTestAgent) {
				pdns := agent.runtimes[transport.DNSEnginePowerDNS]
				pdns.Installed, pdns.Running, pdns.Managed = true, true, true
				agent.runtimes[transport.DNSEnginePowerDNS] = pdns
				bind := agent.runtimes[transport.DNSEngineBIND]
				bind.Installed, bind.Running, bind.Managed = true, false, true
				agent.runtimes[transport.DNSEngineBIND] = bind
			},
		},
		{
			name: `other engine running with BIND absent`, wantAction: `install`,
			configure: func(agent *dnsEngineTestAgent) {
				pdns := agent.runtimes[transport.DNSEnginePowerDNS]
				pdns.Installed, pdns.Running, pdns.Managed = true, true, true
				agent.runtimes[transport.DNSEnginePowerDNS] = pdns
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(`CELIKPANEL_SERVER_IP`, `72.62.38.15`)
			panel := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, panel, `paired`)
			agent := newDNSEngineTestAgent()
			test.configure(agent)
			attachDNSEngineTestAgent(t, panel, agent)
			snapshot, err := panel.dnsEngineSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.ActiveEngine != nil ||
				snapshot.State == dnsEngineStateUnconfigured {
				t.Fatalf(`runtime authority snapshot=%+v`, snapshot)
			}
			preview, recorder := requestDNSEnginePreview(
				t, panel, transport.DNSEngineBIND, nil, snapshot.Revision,
			)
			// A blocked preview hands out no token at all, so there is
			// nothing for the operator to spend and nothing the commit can
			// mistake for a race. A commit that arrives with a well-formed
			// token anyway is answered with the blockers by name.
			if recorder.Code != http.StatusOK ||
				preview.Action != test.wantAction ||
				preview.PreviewToken != `` ||
				!hasDNSEngineBlocker(preview, `target_unavailable`) {
				t.Fatalf(`blocked preview=%+v status=%d body=%s`,
					preview, recorder.Code, recorder.Body.String())
			}
			commit := commitDNSEngineSwitch(
				t, panel, strings.Repeat(`8`, 32), transport.DNSEngineBIND,
				nil, snapshot.Revision, strings.Repeat(`c`, 32), false,
			)
			if commit.Code != http.StatusConflict ||
				!strings.Contains(commit.Body.String(), `the preview was blocked`) ||
				!strings.Contains(commit.Body.String(), `target_unavailable`) {
				t.Fatalf(`blocked commit status=%d body=%s`,
					commit.Code, commit.Body.String())
			}
			agent.mu.Lock()
			calls, requests := agent.switchCalls, len(agent.switchRequests)
			agent.mu.Unlock()
			if calls != 0 || requests != 0 {
				t.Fatalf(`blocked runtime authority reached agent: %d/%d`,
					calls, requests)
			}
			var switches int
			var current sql.NullString
			if err := panel.db.GetDB().QueryRow(`
				SELECT (SELECT count(*) FROM dns_engine_switch_snapshots),
				       current_switch_id
				FROM dns_engine_state WHERE singleton_id = 1`,
			).Scan(&switches, &current); err != nil {
				t.Fatal(err)
			}
			marker, err := readDNSEngineOperationMarker(
				context.Background(), panel.db.GetDB(),
			)
			if err != nil || marker != nil || switches != 0 || current.Valid {
				t.Fatalf(`blocked mutation durable state marker=%+v switches=%d current=%+v err=%v`,
					marker, switches, current, err)
			}
		})
	}
}

func TestDNSEngineOperationMarkerPreservesSourceInvariant(t *testing.T) {
	base := dnsEngineOperationMarker{
		Version:      dnsEngineOperationVersion,
		RequestID:    strings.Repeat(`9`, 32),
		SwitchID:     strings.Repeat(`a`, 32),
		TargetEngine: transport.DNSEngineBIND,
		Phase:        dnsEngineOperationAccepted,
	}
	tests := []struct {
		name, action string
		source       transport.DNSEngine
		wantError    bool
	}{
		{name: `source-free install`, action: `install`},
		{name: `source-free switch`, action: `switch`, wantError: true},
		{
			name: `sourceful switch`, action: `switch`,
			source: transport.DNSEnginePowerDNS,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			marker := base
			marker.Action, marker.SourceEngine = test.action, test.source
			err := validateDNSEngineOperationMarker(marker)
			if (err != nil) != test.wantError {
				t.Fatalf(`marker=%+v err=%v wantError=%v`,
					marker, err, test.wantError)
			}
		})
	}
}

func TestDNSEngineGETMatchesUIWireContract(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dns/engine", nil)
	panel.handleDNSEngine(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{
		"revision", "engine_epoch", "active_engine", "state", "topology",
		"dnssec_zone_count", "zone_count", "pending_zone_count", "engines",
	}
	if len(payload) != len(wantKeys) {
		t.Fatalf("GET keys=%v body=%s", payload, recorder.Body.String())
	}
	for _, key := range wantKeys {
		if _, ok := payload[key]; !ok {
			t.Fatalf("GET missing %q: %s", key, recorder.Body.String())
		}
	}
	var engines []map[string]json.RawMessage
	if err := json.Unmarshal(payload["engines"], &engines); err != nil {
		t.Fatal(err)
	}
	if len(engines) != 2 {
		t.Fatalf("GET engines=%s", payload["engines"])
	}
	for _, engine := range engines {
		for _, key := range []string{"id", "installed", "running", "managed", "status"} {
			if _, ok := engine[key]; !ok {
				t.Fatalf("GET engine missing %q: %s", key, payload["engines"])
			}
		}
	}
}

func TestDNSEngineFirstInstallRequiresStagedDNSIdentity(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK ||
		!hasDNSEngineBlocker(preview, "dns_identity_required") ||
		preview.PreviewToken != "" {
		t.Fatalf("identity blocker preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("0", 32), transport.DNSEngineBIND,
		nil, 0, strings.Repeat("d", 32), false,
	)
	if commit.Code != http.StatusConflict ||
		!strings.Contains(commit.Body.String(), "the preview was blocked") ||
		!strings.Contains(commit.Body.String(), "dns_identity_required") {
		t.Fatalf("uncached blocked commit status=%d body=%s",
			commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if !exactUninitializedDNSEngineState(state) {
		t.Fatalf("blocked preview changed engine state: %+v", state)
	}
	agent.mu.Lock()
	switchCalls := agent.switchCalls
	agent.mu.Unlock()
	if switchCalls != 0 {
		t.Fatalf("blocked identity preview performed %d host switches", switchCalls)
	}
}

func TestDNSEnginePairedBINDPreviewAndDNSSECBlockerWithoutMutation(t *testing.T) {
	tests := []struct {
		name        string
		paired      bool
		dnssec      bool
		wantBlocker string
	}{
		{name: "paired BIND", paired: true},
		{name: "dnssec", dnssec: true, wantBlocker: "dnssec_unsupported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			panel := newDNSPanelForTest(t)
			if test.paired {
				setDNSIdentityForTest(t, panel, "paired")
			} else {
				setDNSIdentityForTest(t, panel, "standalone")
			}
			if test.dnssec {
				seedStrictDNSZone(t, panel, "signed.example")
			}
			agent := newDNSEngineTestAgent()
			agent.dnssec = test.dnssec
			if test.dnssec {
				// A signed zone can only exist where a PowerDNS that signed it
				// exists: the legacy, not-yet-adopted shape. A host with no
				// PowerDNS at all is not probed (R-029, third layer).
				// İmzalı bölge ancak onu imzalayan bir PowerDNS'in olduğu yerde
				// olabilir: eski, henüz devralınmamış biçim. Hiç PowerDNS'i
				// olmayan sunucu sorgulanmaz (R-029, üçüncü kat).
				pdns := agent.runtimes[transport.DNSEnginePowerDNS]
				pdns.Installed = true
				agent.runtimes[transport.DNSEnginePowerDNS] = pdns
			}
			attachDNSEngineTestAgent(t, panel, agent)
			preview, recorder := requestDNSEnginePreview(
				t, panel, transport.DNSEngineBIND, nil, 0,
			)
			if recorder.Code != http.StatusOK ||
				(test.wantBlocker != "" && !hasDNSEngineBlocker(preview, test.wantBlocker)) ||
				(test.wantBlocker == "" && len(preview.Blockers) != 0) {
				t.Fatalf("preview=%+v status=%d body=%s",
					preview, recorder.Code, recorder.Body.String())
			}
			if test.paired && preview.Topology != "paired" {
				t.Fatalf("paired topology misreported as %q", preview.Topology)
			}
			agent.mu.Lock()
			calls := agent.switchCalls
			agent.mu.Unlock()
			if calls != 0 {
				t.Fatalf("blocked preview performed %d host switches", calls)
			}
		})
	}
}

func TestDNSEnginePairedBINDCommitPersistsDirectionalIdentity(t *testing.T) {
	for index, test := range []struct {
		name, localIP, peerIP, peerNS, wantRole, wantLocalNS string
	}{
		{
			name: "primary", localIP: "192.0.2.10", peerIP: "192.0.2.20",
			peerNS: "ns2.celikhost.com", wantRole: transport.DNSPairRolePrimary,
			wantLocalNS: "ns1.celikhost.com",
		},
		{
			name: "secondary", localIP: "192.0.2.20", peerIP: "192.0.2.10",
			peerNS: "ns1.celikhost.com", wantRole: transport.DNSPairRoleSecondary,
			wantLocalNS: "ns2.celikhost.com",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", test.localIP)
			panel := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, panel, "paired")
			if err := panel.setSetting(context.Background(), settingDNSPeerIP, test.peerIP); err != nil {
				t.Fatal(err)
			}
			if err := panel.setSetting(context.Background(), settingDNSPeerNS, test.peerNS); err != nil {
				t.Fatal(err)
			}
			agent := newDNSEngineTestAgent()
			attachDNSEngineTestAgent(t, panel, agent)
			preview, recorder := requestDNSEnginePreview(
				t, panel, transport.DNSEngineBIND, nil, 0,
			)
			if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
				preview.Topology != transport.DNSTopologyPaired {
				t.Fatalf("preview status=%d value=%+v body=%s",
					recorder.Code, preview, recorder.Body.String())
			}
			requestID := strings.Repeat(string(rune('4'+index)), 32)
			commit := commitDNSEngineSwitch(
				t, panel, requestID, transport.DNSEngineBIND,
				nil, 0, preview.PreviewToken, false,
			)
			if commit.Code != http.StatusOK {
				t.Fatalf("commit status=%d body=%s", commit.Code, commit.Body.String())
			}
			agent.mu.Lock()
			if len(agent.switchRequests) != 1 {
				agent.mu.Unlock()
				t.Fatalf("switch requests=%d", len(agent.switchRequests))
			}
			request := agent.switchRequests[0]
			agent.mu.Unlock()
			if request.Topology != transport.DNSTopologyPaired ||
				request.PairRole != test.wantRole || request.LocalIP != test.localIP ||
				request.LocalNS != test.wantLocalNS || request.PeerIP != test.peerIP ||
				request.PeerNS != test.peerNS {
				t.Fatalf("directional request=%+v", request)
			}
			state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
			if err != nil {
				t.Fatal(err)
			}
			if state.ActiveEngine != transport.DNSEngineBIND ||
				state.Topology != transport.DNSTopologyPaired ||
				state.PairRole != test.wantRole || state.LocalIP != test.localIP ||
				state.LocalNS != test.wantLocalNS || state.PeerIP != test.peerIP ||
				state.PeerNS != test.peerNS {
				t.Fatalf("durable directional state=%+v", state)
			}
			snapshot, err := panel.dnsEngineSnapshot(context.Background())
			if err != nil || snapshot.PairReady == nil ||
				*snapshot.PairReady != (test.wantRole == transport.DNSPairRolePrimary) {
				t.Fatalf("paired readiness snapshot=%+v err=%v", snapshot, err)
			}
			identity, ready, err := panel.activeDNSPublisher(context.Background())
			if err != nil || identity.PairRole != test.wantRole ||
				ready != (test.wantRole == transport.DNSPairRolePrimary) {
				t.Fatalf("publisher identity=%+v ready=%v err=%v", identity, ready, err)
			}
			if test.wantRole == transport.DNSPairRolePrimary {
				agent.mu.Lock()
				runtime := agent.runtimes[transport.DNSEngineBIND]
				runtime.PairReady = false
				agent.runtimes[transport.DNSEngineBIND] = runtime
				agent.mu.Unlock()
				identity, ready, err = panel.activeDNSPublisher(context.Background())
				if err != nil || ready || identity.PairRole != transport.DNSPairRolePrimary ||
					!runtime.Installed || !runtime.Running || !runtime.Managed {
					t.Fatalf(
						"unproven primary identity=%+v runtime=%+v ready=%v err=%v",
						identity, runtime, ready, err,
					)
				}
				snapshot, err = panel.dnsEngineSnapshot(context.Background())
				if err != nil || snapshot.PairReady == nil || *snapshot.PairReady {
					t.Fatalf("unproven primary snapshot=%+v err=%v", snapshot, err)
				}
				agent.mu.Lock()
				runtime.PairReady = true
				agent.runtimes[transport.DNSEngineBIND] = runtime
				agent.mu.Unlock()
				snapshot, err = panel.dnsEngineSnapshot(context.Background())
				if err != nil || snapshot.PairReady == nil || !*snapshot.PairReady {
					t.Fatalf("proven primary snapshot=%+v err=%v", snapshot, err)
				}
			}
		})
	}
}

func TestDNSEnginePairedIdentitySurvivesBINDToPowerDNS(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "paired")
	if err := panel.setSetting(context.Background(), settingDNSPeerIP, "192.0.2.20"); err != nil {
		t.Fatal(err)
	}
	if err := panel.setSetting(context.Background(), settingDNSPeerNS, "ns2.celikhost.com"); err != nil {
		t.Fatal(err)
	}
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	bindPreview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(bindPreview.Blockers) != 0 {
		t.Fatalf("BIND preview=%+v status=%d", bindPreview, recorder.Code)
	}
	if commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("6", 32), transport.DNSEngineBIND,
		nil, 0, bindPreview.PreviewToken, false,
	); commit.Code != http.StatusOK {
		t.Fatalf("BIND commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	bindState, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	pdnsPreview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS,
		string(transport.DNSEngineBIND), bindState.Revision,
	)
	if recorder.Code != http.StatusOK || len(pdnsPreview.Blockers) != 0 ||
		pdnsPreview.Topology != transport.DNSTopologyPaired {
		t.Fatalf("PowerDNS preview=%+v status=%d body=%s",
			pdnsPreview, recorder.Code, recorder.Body.String())
	}
	if commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("7", 32), transport.DNSEnginePowerDNS,
		string(transport.DNSEngineBIND), bindState.Revision,
		pdnsPreview.PreviewToken, true,
	); commit.Code != http.StatusOK {
		t.Fatalf("PowerDNS commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEnginePowerDNS ||
		state.Topology != transport.DNSTopologyPaired ||
		state.PairRole != transport.DNSPairRolePrimary ||
		state.LocalIP != "192.0.2.10" || state.PeerIP != "192.0.2.20" ||
		state.LocalNS != "ns1.celikhost.com" || state.PeerNS != "ns2.celikhost.com" {
		t.Fatalf("reverse paired state=%+v", state)
	}
	agent.mu.Lock()
	last := agent.switchRequests[len(agent.switchRequests)-1]
	agent.mu.Unlock()
	if last.TargetEngine != transport.DNSEnginePowerDNS ||
		last.Topology != transport.DNSTopologyPaired ||
		last.PairRole != transport.DNSPairRolePrimary {
		t.Fatalf("reverse paired request=%+v", last)
	}
	noop := httptest.NewRecorder()
	panel.handleDNSSetup(noop, dnsSetupAdminRequest(
		`{"ns1":"ns1.celikhost.com","ns2":"ns2.celikhost.com","role":"paired","peer_ip":"192.0.2.20","peer_ns":"ns2.celikhost.com"}`,
	))
	if noop.Code != http.StatusOK {
		t.Fatalf("exact paired identity retry status=%d body=%s",
			noop.Code, noop.Body.String())
	}
	afterNoop, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if afterNoop != state {
		t.Fatalf("exact paired identity retry changed state: before=%+v after=%+v",
			state, afterNoop)
	}
	locked := httptest.NewRecorder()
	panel.handleDNSSetup(locked, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"192.0.2.30","peer_ns":"ns4.example.net"}`,
	))
	if locked.Code != http.StatusConflict {
		t.Fatalf("paired identity mutation status=%d body=%s",
			locked.Code, locked.Body.String())
	}
	var lockedBody apiErrorBody
	if err := json.Unmarshal(locked.Body.Bytes(), &lockedBody); err != nil ||
		lockedBody.Code != errCodeDNSPairIdentityLocked {
		t.Fatalf("paired identity refusal=%+v err=%v body=%s",
			lockedBody, err, locked.Body.String())
	}
	afterLocked, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if afterLocked != state {
		t.Fatalf("paired identity refusal changed state: before=%+v after=%+v",
			state, afterLocked)
	}
	agent.mu.Lock()
	switchCalls := agent.switchCalls
	agent.mu.Unlock()
	if switchCalls != 2 {
		t.Fatalf("paired identity refusal changed host: switch calls=%d", switchCalls)
	}
}

func TestDNSEngineReconfiguresEmptyLegacyPowerDNSAsPairedSecondary(
	t *testing.T,
) {
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
		t.Fatalf("secondary stage status=%d body=%s",
			stage.Code, stage.Body.String())
	}
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 1,
	)
	wantImpacts := []string{
		"validate_target",
		"replace_existing",
		"restart_target",
		"configure_secondary",
		"brief_dns_interruption",
	}
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
		preview.Action != "reconfigure" || preview.SourceEngine != nil ||
		preview.TargetEngine != transport.DNSEnginePowerDNS ||
		preview.Topology != transport.DNSTopologyPaired ||
		!preview.RequiresDowntimeAcknowledgement ||
		preview.EstimatedDowntimeSeconds != 15 ||
		!slices.Equal(preview.Impacts, wantImpacts) {
		t.Fatalf("reconfigure preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	withoutAck := commitDNSEngineSwitch(
		t, panel, strings.Repeat("8", 32),
		transport.DNSEnginePowerDNS, nil, 1,
		preview.PreviewToken, false,
	)
	if withoutAck.Code != http.StatusBadRequest {
		t.Fatalf("reconfigure without ack status=%d body=%s",
			withoutAck.Code, withoutAck.Body.String())
	}
	preview, recorder = requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 1,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf("second reconfigure preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("9", 32),
		transport.DNSEnginePowerDNS, nil, 1,
		preview.PreviewToken, true,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("reconfigure commit status=%d body=%s",
			commit.Code, commit.Body.String())
	}
	agent.mu.Lock()
	if len(agent.switchRequests) != 1 {
		agent.mu.Unlock()
		t.Fatalf("reconfigure switch requests=%d", len(agent.switchRequests))
	}
	request := agent.switchRequests[0]
	agent.mu.Unlock()
	if request.Mode != transport.DNSEngineSwitchModeSwitch ||
		request.SourceEngine != "" ||
		request.TargetEngine != transport.DNSEnginePowerDNS ||
		request.Topology != transport.DNSTopologyPaired ||
		request.PairRole != transport.DNSPairRoleSecondary ||
		request.LocalIP != "192.0.2.20" ||
		request.LocalNS != "ns2.example.net" ||
		request.PeerIP != "192.0.2.10" ||
		request.PeerNS != "ns1.example.net" ||
		len(request.Zones) != 0 {
		t.Fatalf("reconfigure request=%+v", request)
	}
	state, err := readDNSEngineDBState(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEnginePowerDNS ||
		state.Topology != transport.DNSTopologyPaired ||
		state.PairRole != transport.DNSPairRoleSecondary ||
		state.EngineEpoch != 1 {
		t.Fatalf("reconfigured durable state=%+v", state)
	}
}

func TestDNSEngineStalePreviewCannotStartMutation(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := panel.db.GetDB().Exec(`
		UPDATE dns_engine_state SET revision = 1, updated_at = datetime('now')
		WHERE singleton_id = 1`); err != nil {
		t.Fatal(err)
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("d", 32), transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusConflict {
		t.Fatalf("stale commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.switchCalls != 0 {
		t.Fatalf("stale preview performed %d host switches", agent.switchCalls)
	}
}

func TestDNSEngineUnresolvedRuntimePresentationRequiresExplicitAdopt(t *testing.T) {
	state := dnsEngineDBState{}
	fresh := map[transport.DNSEngine]transport.DNSBackendRuntimeState{
		transport.DNSEnginePowerDNS: {Engine: transport.DNSEnginePowerDNS},
		transport.DNSEngineBIND:     {Engine: transport.DNSEngineBIND},
	}
	status, entries := deriveDNSEnginePresentation(state, fresh, nil, "")
	if status != dnsEngineStateUnconfigured || entries[0].Status != "available" {
		t.Fatalf("fresh presentation=%s %+v", status, entries)
	}
	pdnsManaged := fresh
	pdnsManaged[transport.DNSEnginePowerDNS] = transport.DNSBackendRuntimeState{
		Engine:    transport.DNSEnginePowerDNS,
		Installed: true, Running: true, Managed: true,
	}
	status, entries = deriveDNSEnginePresentation(state, pdnsManaged, nil, "")
	if status != dnsEngineStateUnmanaged ||
		entries[0].Status != "unmanaged" {
		t.Fatalf("legacy managed PDNS was implicitly adopted: %s %+v", status, entries)
	}
	snapshot := dnsEngineSnapshot{runtime: pdnsManaged}
	if action := dnsEngineAction(snapshot, transport.DNSEnginePowerDNS); action != "adopt" {
		t.Fatalf("legacy managed PDNS action=%q, want adopt", action)
	}
	both := map[transport.DNSEngine]transport.DNSBackendRuntimeState{
		transport.DNSEnginePowerDNS: pdnsManaged[transport.DNSEnginePowerDNS],
		transport.DNSEngineBIND: {
			Engine:    transport.DNSEngineBIND,
			Installed: true, Running: true, Managed: false,
		},
	}
	status, _ = deriveDNSEnginePresentation(state, both, nil, "")
	if status != dnsEngineStateConflict {
		t.Fatalf("two running backends state=%s", status)
	}
}

func TestDNSEnginePreviewBlocksUnrelatedPublicPort53Listener(t *testing.T) {
	snapshot := dnsEngineSnapshot{
		Revision: 3, Topology: transport.DNSTopologyStandalone,
		State: dnsEngineStateUnconfigured, port53Conflict: true,
		runtime: map[transport.DNSEngine]transport.DNSBackendRuntimeState{
			transport.DNSEnginePowerDNS: {Engine: transport.DNSEnginePowerDNS},
			transport.DNSEngineBIND:     {Engine: transport.DNSEngineBIND},
		},
	}
	blockers := dnsEnginePreviewBlockers(
		snapshot, transport.DNSEngineBIND, "", snapshot.Revision,
	)
	for _, blocker := range blockers {
		if blocker.Code == "port_53_conflict" {
			return
		}
	}
	t.Fatalf("preview blockers omit public port-53 conflict: %+v", blockers)
}

func TestDNSEnginePreviewReportsIncompatibleAgentSeparatelyFromTargetAvailability(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	agent.omitDNSCapabilities = true
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK ||
		!hasDNSEngineBlocker(preview, "agent_incompatible") ||
		hasDNSEngineBlocker(preview, "target_unavailable") {
		t.Fatalf("incompatible agent preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
}

func TestDNSEngineManagedPDNSRequiresAndCompletesExplicitAdopt(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
		preview.Action != "adopt" ||
		preview.RequiresDowntimeAcknowledgement ||
		!slices.Equal(preview.Impacts, []string{"validate_target", "adopt_existing"}) {
		t.Fatalf("managed PDNS adoption preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("1", 32), transport.DNSEnginePowerDNS,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("managed PDNS adoption status=%d body=%s",
			commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEnginePowerDNS ||
		state.EngineEpoch != 1 || state.Topology != transport.DNSTopologyStandalone ||
		state.CurrentSwitchID != "" {
		t.Fatalf("adopted state=%+v", state)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.switchRequests) != 1 ||
		agent.switchRequests[0].Mode != transport.DNSEngineSwitchModeAdopt ||
		agent.switchRequests[0].Topology != transport.DNSTopologyStandalone ||
		agent.firewallCalls != 0 {
		t.Fatalf("adoption request=%+v firewall_calls=%d",
			agent.switchRequests, agent.firewallCalls)
	}
}

func TestDNSEngineStandaloneAdoptionNormalizesAllExternalPDNSZones(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, transport.DNSTopologyStandalone)
	for _, zone := range []string{
		"alpha-external.example", "middle-external.example", "zeta-external.example",
	} {
		seedStrictDNSZone(t, panel, zone)
	}
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	var (
		lockObservationMu sync.Mutex
		unlockedStages    []string
	)
	observeLocks := func(stage string) {
		lockObservationMu.Lock()
		defer lockObservationMu.Unlock()
		if panel.serviceMutationMu.TryLock() {
			panel.serviceMutationMu.Unlock()
			unlockedStages = append(unlockedStages, stage+":serviceMutation")
		}
		if panel.dnsTopologyMu.TryLock() {
			panel.dnsTopologyMu.Unlock()
			unlockedStages = append(unlockedStages, stage+":dnsTopology")
		}
		if dnsPublicationMu.TryLock() {
			dnsPublicationMu.Unlock()
			unlockedStages = append(unlockedStages, stage+":dnsPublication")
		}
	}
	agent.onConfigurePDNS = func() { observeLocks("configure") }
	agent.onSyncV3 = func(request transport.SyncDNSZoneV3Request) {
		observeLocks("sync:" + request.Domain)
	}
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 0,
	)
	if recorder.Code != http.StatusOK || preview.Action != "adopt" ||
		preview.ZoneCount != 3 || len(preview.Blockers) != 0 {
		t.Fatalf("external adoption preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("e", 32), transport.DNSEnginePowerDNS,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("external adoption status=%d body=%s",
			commit.Code, commit.Body.String())
	}

	agent.mu.Lock()
	configureCalls := agent.configurePDNSCalls
	configureRequests := append(
		[]transport.ServiceMutationRequest(nil), agent.configurePDNSRequests...,
	)
	syncRequests := append(
		[]transport.SyncDNSZoneV3Request(nil), agent.syncV3Requests...,
	)
	events := append([]string(nil), agent.events...)
	agent.mu.Unlock()
	if configureCalls != 1 || len(configureRequests) != 1 ||
		!validServiceOperationID(configureRequests[0].MutationRequestID) ||
		!validServiceOperationID(configureRequests[0].MutationOwnerID) {
		t.Fatalf("configure calls=%d requests=%+v", configureCalls, configureRequests)
	}
	var domains []string
	for _, request := range syncRequests {
		domains = append(domains, request.Domain)
		if request.Engine != transport.DNSEnginePowerDNS ||
			request.EngineEpoch != 1 ||
			!validServiceOperationID(request.MutationRequestID) ||
			!validServiceOperationID(request.MutationOwnerID) {
			t.Fatalf("unbound external-zone normalization request=%+v", request)
		}
	}
	if want := []string{
		"alpha-external.example", "middle-external.example", "zeta-external.example",
	}; !slices.Equal(domains, want) {
		t.Fatalf("normalized domains=%v, want %v", domains, want)
	}
	if want := []string{
		"switch", "configure", "sync:alpha-external.example",
		"sync:middle-external.example", "sync:zeta-external.example",
	}; !slices.Equal(events, want) {
		t.Fatalf("adoption call order=%v, want %v", events, want)
	}
	lockObservationMu.Lock()
	defer lockObservationMu.Unlock()
	if len(unlockedStages) != 0 {
		t.Fatalf(
			"serviceMutation -> dnsTopology -> dnsPublication lock set was incomplete: %v",
			unlockedStages,
		)
	}
	var applications int
	if err := panel.db.GetDB().QueryRow(`
		SELECT count(*) FROM dns_zone_engine_applications
		WHERE engine = 'pdns' AND engine_epoch = 1
	`).Scan(&applications); err != nil {
		t.Fatal(err)
	}
	if applications != 3 {
		t.Fatalf("normalized applications=%d, want 3", applications)
	}
	marker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker != nil {
		t.Fatalf("completed normalization marker=%+v err=%v", marker, err)
	}
}

func TestDNSEngineAdoptionZoneFailureReplaysWithoutSecondSwitchOrConfigure(
	t *testing.T,
) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, transport.DNSTopologyStandalone)
	for _, zone := range []string{
		"alpha-replay.example", "middle-replay.example", "zeta-replay.example",
	} {
		seedStrictDNSZone(t, panel, zone)
	}
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	agent.syncV3Errors = map[string]string{
		"middle-replay.example": "injected external zone normalization failure",
	}
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 0,
	)
	if recorder.Code != http.StatusOK || preview.Action != "adopt" {
		t.Fatalf("adoption preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	requestID := strings.Repeat("d", 32)
	first := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEnginePowerDNS,
		nil, 0, preview.PreviewToken, false,
	)
	if first.Code != http.StatusBadGateway ||
		!strings.Contains(first.Body.String(), errCodeDNSPublicationFailed) {
		t.Fatalf("partial normalization status=%d body=%s",
			first.Code, first.Body.String())
	}
	marker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker == nil ||
		marker.Phase != dnsEngineOperationPostCommit ||
		!marker.ConfigurePDNSComplete ||
		!validServiceOperationID(marker.ConfigurePDNSRequestID) ||
		!validServiceOperationID(marker.ConfigurePDNSOwnerID) {
		t.Fatalf("partial normalization marker=%+v err=%v", marker, err)
	}
	agent.mu.Lock()
	if agent.switchCalls != 1 || agent.configurePDNSCalls != 1 ||
		len(agent.syncV3Requests) != 3 {
		agent.mu.Unlock()
		t.Fatalf("first attempt switch=%d configure=%d sync=%d",
			agent.switchCalls, agent.configurePDNSCalls, len(agent.syncV3Requests))
	}
	delete(agent.syncV3Errors, "middle-replay.example")
	agent.mu.Unlock()

	replay := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEnginePowerDNS,
		nil, 0, preview.PreviewToken, false,
	)
	if replay.Code != http.StatusOK {
		t.Fatalf("normalization replay status=%d body=%s",
			replay.Code, replay.Body.String())
	}
	agent.mu.Lock()
	switchCalls := agent.switchCalls
	configureCalls := agent.configurePDNSCalls
	syncCalls := len(agent.syncV3Requests)
	agent.mu.Unlock()
	if switchCalls != 1 || configureCalls != 1 || syncCalls != 6 {
		t.Fatalf("replay switch=%d configure=%d sync=%d, want 1/1/6",
			switchCalls, configureCalls, syncCalls)
	}
	marker, err = readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker != nil {
		t.Fatalf("successful replay marker=%+v err=%v", marker, err)
	}
}

func TestDNSEngineAdoptionRestartAcceptsOnlyExactConfigureChildReceipt(
	t *testing.T,
) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, transport.DNSTopologyStandalone)
	seedStrictDNSZone(t, panel, "restart-external.example")
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, panel, agent)

	ctx := context.Background()
	state, err := readDNSEngineDBState(ctx, panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := panel.buildDNSEngineManifest(
		ctx, state, transport.DNSEnginePowerDNS, "adopt",
		transport.DNSTopologyStandalone,
	)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := panel.persistDNSEngineSwitch(
		ctx,
		dnsEngineSwitchRequest{
			RequestID:        strings.Repeat("a", 32),
			TargetEngine:     transport.DNSEnginePowerDNS,
			ExpectedSource:   nullableDNSEngine{Set: true},
			ExpectedRevision: state.Revision,
		},
		strings.Repeat("b", 32), strings.Repeat("c", 32),
		"adopt", manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.executeDNSEngineSwitch(ctx, persisted, manifest); err != nil {
		t.Fatal(err)
	}
	marker, err := panel.ensureAdoptionConfigureIdentity(ctx, persisted)
	if err != nil {
		t.Fatal(err)
	}
	if marker.ConfigurePDNSComplete ||
		!validServiceOperationID(marker.ConfigurePDNSRequestID) ||
		!validServiceOperationID(marker.ConfigurePDNSOwnerID) {
		t.Fatalf("persisted configure child=%+v", marker)
	}

	wrong := &agentMutationJob{
		RequestID: marker.ConfigurePDNSRequestID,
		OwnerID:   strings.Repeat("f", 32),
		Kind:      "pdns_configure", Target: "pdns",
		Status: agentMutationSucceeded, Phase: "completed", Attempt: 1,
	}
	setDNSEngineMutationJobForTest(t, agent, wrong, false)
	panel.serviceMutationMu.Lock()
	panel.dnsTopologyMu.Lock()
	dnsPublicationMu.Lock()
	wrongErr := panel.configureAdoptedPowerDNSLocked(ctx, persisted)
	dnsPublicationMu.Unlock()
	panel.dnsTopologyMu.Unlock()
	panel.serviceMutationMu.Unlock()
	if !errors.Is(wrongErr, errAgentMutationIdentityMismatch) {
		t.Fatalf("wrong configure child error=%v, want identity mismatch", wrongErr)
	}
	handled, err := panel.recoverDNSEngineSwitchLocked(ctx, nil)
	if err != nil || !handled {
		t.Fatalf("wrong-child recovery handled=%v err=%v", handled, err)
	}
	retained, err := readDNSEngineOperationMarker(ctx, panel.db.GetDB())
	if err != nil || retained == nil || retained.ConfigurePDNSComplete {
		t.Fatalf("wrong child cleared/advanced marker=%+v err=%v", retained, err)
	}

	exact := *wrong
	exact.OwnerID = marker.ConfigurePDNSOwnerID
	setDNSEngineMutationJobForTest(t, agent, &exact, false)
	handled, err = panel.recoverDNSEngineSwitchLocked(ctx, nil)
	if err != nil || !handled {
		t.Fatalf("exact-child recovery handled=%v err=%v", handled, err)
	}
	finalMarker, err := readDNSEngineOperationMarker(ctx, panel.db.GetDB())
	if err != nil || finalMarker != nil {
		t.Fatalf("exact child did not converge marker=%+v err=%v", finalMarker, err)
	}
	agent.mu.Lock()
	switchCalls := agent.switchCalls
	configureCalls := agent.configurePDNSCalls
	syncRequests := append(
		[]transport.SyncDNSZoneV3Request(nil), agent.syncV3Requests...,
	)
	agent.mu.Unlock()
	if switchCalls != 1 || configureCalls != 0 ||
		len(syncRequests) != 1 ||
		syncRequests[0].Domain != "restart-external.example" {
		t.Fatalf("status recovery switch=%d configure=%d sync=%+v",
			switchCalls, configureCalls, syncRequests)
	}
}

func TestDNSEngineAdoptionRestartRecoversOnlyExactV3ZoneLease(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, transport.DNSTopologyStandalone)
	zone := "lease-restart-external.example"
	seedStrictDNSZone(t, panel, zone)
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, panel, agent)

	ctx := context.Background()
	state, err := readDNSEngineDBState(ctx, panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := panel.buildDNSEngineManifest(
		ctx, state, transport.DNSEnginePowerDNS, "adopt",
		transport.DNSTopologyStandalone,
	)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := panel.persistDNSEngineSwitch(
		ctx,
		dnsEngineSwitchRequest{
			RequestID:        strings.Repeat("1", 32),
			TargetEngine:     transport.DNSEnginePowerDNS,
			ExpectedSource:   nullableDNSEngine{Set: true},
			ExpectedRevision: state.Revision,
		},
		strings.Repeat("2", 32), strings.Repeat("3", 32),
		"adopt", manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.executeDNSEngineSwitch(ctx, persisted, manifest); err != nil {
		t.Fatal(err)
	}
	marker, err := panel.ensureAdoptionConfigureIdentity(ctx, persisted)
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.markAdoptionConfigureComplete(
		ctx, persisted,
		marker.ConfigurePDNSRequestID, marker.ConfigurePDNSOwnerID,
	); err != nil {
		t.Fatal(err)
	}

	panel.serviceMutationMu.Lock()
	panel.dnsTopologyMu.Lock()
	dnsPublicationMu.Lock()
	plan, err := panel.prepareDNSZoneSyncV3Plan(
		ctx, zone, false,
		dnsPublisherIdentity{
			Engine: transport.DNSEnginePowerDNS,
			Epoch:  persisted.TargetEpoch,
		},
	)
	dnsPublicationMu.Unlock()
	panel.dnsTopologyMu.Unlock()
	panel.serviceMutationMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	identity := plan.Lease.identity()
	publishedPhase, required, err := payloadBoundMutationPublishedPhase(identity)
	if err != nil || !required {
		t.Fatalf("V3 published phase=%q required=%v err=%v",
			publishedPhase, required, err)
	}
	terminal := &agentMutationJob{
		RequestID: identity.RequestID, OwnerID: identity.OwnerID,
		Kind: identity.Kind, Target: identity.Target,
		PackageName: identity.PackageName,
		Status:      agentMutationSucceeded, Phase: publishedPhase, Attempt: 1,
	}
	setDNSEngineMutationJobForTest(t, agent, terminal, false)

	wrongGlobal := *terminal
	wrongGlobal.OwnerID = strings.Repeat("f", 32)
	wrongGlobal.Status = agentMutationRunning
	wrongGlobal.Phase = "starting"
	handled, err := panel.recoverDNSEngineSwitchLocked(ctx, &wrongGlobal)
	if err == nil || !handled {
		t.Fatalf("wrong V3 global child handled=%v err=%v", handled, err)
	}
	retained, err := readDNSEngineOperationMarker(ctx, panel.db.GetDB())
	if err != nil || retained == nil || !retained.ConfigurePDNSComplete {
		t.Fatalf("wrong V3 child changed marker=%+v err=%v", retained, err)
	}
	if lease, leaseErr := readDNSZoneEngineLease(
		ctx, panel.db.GetDB(), zone,
	); leaseErr != nil || lease != plan.Lease {
		t.Fatalf("wrong V3 child changed exact lease=%+v err=%v", lease, leaseErr)
	}

	exactGlobal := *terminal
	exactGlobal.Status = agentMutationRunning
	exactGlobal.Phase = "starting"
	handled, err = panel.recoverDNSEngineSwitchLocked(ctx, &exactGlobal)
	if err != nil || !handled {
		t.Fatalf("exact V3 global child handled=%v err=%v", handled, err)
	}
	finalMarker, err := readDNSEngineOperationMarker(ctx, panel.db.GetDB())
	if err != nil || finalMarker != nil {
		t.Fatalf("exact V3 child did not converge marker=%+v err=%v",
			finalMarker, err)
	}
	if _, err := readDNSZoneEngineLease(
		ctx, panel.db.GetDB(), zone,
	); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("exact V3 lease remains after recovery: %v", err)
	}
	agent.mu.Lock()
	configureCalls := agent.configurePDNSCalls
	syncRequests := append(
		[]transport.SyncDNSZoneV3Request(nil), agent.syncV3Requests...,
	)
	agent.mu.Unlock()
	if configureCalls != 0 || len(syncRequests) != 1 ||
		syncRequests[0].Domain != zone {
		t.Fatalf("V3 recovery configure=%d follow-up sync=%+v",
			configureCalls, syncRequests)
	}
}

func TestDNSEngineManagedPDNSAdoptionRetiresAppliedDeletionMarker(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO dns_zone_deletion_markers (zone_name, zone_type)
		VALUES ('retired-adopt.example', 'NATIVE')
	`); err != nil {
		t.Fatal(err)
	}
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
		preview.Action != "adopt" || preview.ZoneCount != 1 ||
		preview.PendingZoneCount != 1 {
		t.Fatalf("delete adoption preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("b", 32), transport.DNSEnginePowerDNS,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("delete adoption status=%d body=%s",
			commit.Code, commit.Body.String())
	}
	var markerCount, stateCount, applicationCount int
	if err := panel.db.GetDB().QueryRow(`
		SELECT
		  (SELECT count(*) FROM dns_zone_deletion_markers
		   WHERE zone_name = 'retired-adopt.example'),
		  (SELECT count(*) FROM dns_zone_sync_state
		   WHERE zone_name = 'retired-adopt.example'),
		  (SELECT count(*) FROM dns_zone_engine_applications
		   WHERE zone_name = 'retired-adopt.example'
		     AND engine = 'pdns' AND engine_epoch = 1
		     AND applied_action = 'delete')
	`).Scan(&markerCount, &stateCount, &applicationCount); err != nil {
		t.Fatal(err)
	}
	if markerCount != 0 || stateCount != 0 || applicationCount != 1 {
		t.Fatalf("retired delete marker=%d state=%d application=%d",
			markerCount, stateCount, applicationCount)
	}
}

func TestDNSEngineLegacyPairedAdoptionFailsClosedWithoutDirectionalRole(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "paired")
	seedStrictDNSZone(t, panel, "signed-adopt.example")
	agent := newDNSEngineTestAgent()
	agent.dnssec = true
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
		preview.Action != "adopt" || preview.Topology != transport.DNSTopologyPaired ||
		preview.DNSSECZoneCount != 1 || preview.RequiresDowntimeAcknowledgement {
		t.Fatalf("paired signed adoption preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("6", 32), transport.DNSEnginePowerDNS,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusBadGateway ||
		!strings.Contains(commit.Body.String(), errCodeDNSPublicationFailed) ||
		strings.Contains(commit.Body.String(), "directional catalog") {
		t.Fatalf("legacy paired adoption status=%d body=%s",
			commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEnginePowerDNS ||
		state.EngineEpoch != 1 || state.Topology != transport.DNSTopologyPaired ||
		state.CurrentSwitchID != "" {
		t.Fatalf("paired adopted state=%+v", state)
	}
	agent.mu.Lock()
	if len(agent.switchRequests) != 1 ||
		agent.switchRequests[0].Mode != transport.DNSEngineSwitchModeAdopt ||
		agent.switchRequests[0].Topology != transport.DNSTopologyPaired ||
		agent.switchRequests[0].PairRole != "" ||
		agent.switchRequests[0].PeerIP != "2.25.80.4" ||
		agent.switchRequests[0].PeerNS != "ns2.celikhost.com" ||
		agent.configurePDNSCalls != 0 || len(agent.syncV3Requests) != 0 {
		agent.mu.Unlock()
		t.Fatalf("legacy paired adoption request=%+v configure=%d sync=%d",
			agent.switchRequests, agent.configurePDNSCalls, len(agent.syncV3Requests))
	}
	agent.mu.Unlock()
	marker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker == nil ||
		marker.Phase != dnsEngineOperationPostCommit ||
		marker.ConfigurePDNSRequestID != "" ||
		marker.ConfigurePDNSOwnerID != "" ||
		marker.ConfigurePDNSComplete {
		t.Fatalf("legacy paired fail-closed marker=%+v err=%v", marker, err)
	}
	persisted, err := readDNSEngineSwitchByRequest(
		context.Background(), panel.db.GetDB(), strings.Repeat("6", 32),
	)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.PairRole != "" ||
		persisted.PeerIP != "2.25.80.4" ||
		persisted.PeerNS != "ns2.celikhost.com" {
		t.Fatalf("persisted legacy pair identity=%q %q/%q",
			persisted.PairRole, persisted.PeerIP, persisted.PeerNS)
	}
	if err := panel.setSetting(
		context.Background(), settingDNSPeerIP, "192.0.2.99",
	); err != nil {
		t.Fatal(err)
	}
	reconstructed, err := panel.reconstructPersistedDNSEngineManifest(
		context.Background(), persisted,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reconstructed.PeerIP != "2.25.80.4" ||
		reconstructed.PeerNS != "ns2.celikhost.com" {
		t.Fatalf("recovery reread mutable settings: %+v", reconstructed)
	}
}

func TestDNSEngineAdoptionFailClosedScopeIsOnlyLegacyPairedShape(t *testing.T) {
	base := persistedDNSEngineSwitch{
		Action: "adopt", TargetEngine: transport.DNSEnginePowerDNS,
		Topology: transport.DNSTopologyStandalone,
	}
	if legacyNonDirectionalPairedAdoption(base) {
		t.Fatal("standalone adoption was classified as legacy paired")
	}
	for _, role := range []string{
		transport.DNSPairRolePrimary, transport.DNSPairRoleSecondary,
	} {
		directional := base
		directional.Topology = transport.DNSTopologyPaired
		directional.PairRole = role
		if legacyNonDirectionalPairedAdoption(directional) {
			t.Fatalf("directional paired role %q was stranded", role)
		}
	}
	legacy := base
	legacy.Topology = transport.DNSTopologyPaired
	if !legacyNonDirectionalPairedAdoption(legacy) {
		t.Fatal("legacy paired adoption without PairRole was not fail-closed")
	}
}

func TestDNSEnginePairedPeerChangeBeforePersistenceIsZeroTouch(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "paired")
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := panel.buildDNSEngineManifest(
		context.Background(), state, transport.DNSEnginePowerDNS,
		"adopt", transport.DNSTopologyPaired,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.setSetting(
		context.Background(), settingDNSPeerIP, "192.0.2.99",
	); err != nil {
		t.Fatal(err)
	}
	request := dnsEngineSwitchRequest{
		RequestID:      strings.Repeat("7", 32),
		TargetEngine:   transport.DNSEnginePowerDNS,
		ExpectedSource: nullableDNSEngine{Set: true}, ExpectedRevision: 0,
	}
	if _, err := panel.persistDNSEngineSwitch(
		context.Background(), request, strings.Repeat("8", 32),
		strings.Repeat("9", 32), "adopt", manifest,
	); err == nil {
		t.Fatal("paired adoption persisted after the peer identity changed")
	}
	var snapshots int
	var attached sql.NullString
	if err := panel.db.GetDB().QueryRow(`
		SELECT (SELECT count(*) FROM dns_engine_switch_snapshots), current_switch_id
		FROM dns_engine_state WHERE singleton_id = 1
	`).Scan(&snapshots, &attached); err != nil {
		t.Fatal(err)
	}
	if snapshots != 0 || attached.Valid {
		t.Fatalf("peer mismatch mutated durable state: snapshots=%d attached=%v",
			snapshots, attached)
	}
}

func TestDNSEngineAdoptionFailureProvesUnchangedRuntimeAndDetaches(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	agent.switchError = "private adoption validation detail"
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf("adoption preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("7", 32), transport.DNSEnginePowerDNS,
		nil, 0, preview.PreviewToken, false,
	)
	var body apiErrorBody
	if commit.Code != http.StatusConflict ||
		json.Unmarshal(commit.Body.Bytes(), &body) != nil ||
		body.Code != errCodeDNSEngineChangeNotCommitted ||
		body.Error != "The DNS engine change was not committed. The pre-operation serving state was verified; packages or setup files may still have changed. Refresh state before creating a new review." ||
		body.PartialSuccess || body.MutationApplied ||
		strings.Contains(strings.ToLower(body.Error), "not serving") ||
		strings.Contains(commit.Body.String(), "private adoption") {
		t.Fatalf("failed adoption status=%d body=%s", commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != "" || state.EngineEpoch != 0 ||
		state.CurrentSwitchID != "" {
		t.Fatalf("failed adoption state=%+v", state)
	}
	agent.mu.Lock()
	pdns = agent.runtimes[transport.DNSEnginePowerDNS]
	agent.mu.Unlock()
	if !pdns.Installed || !pdns.Running || !pdns.Managed {
		t.Fatalf("failed registration-only adoption changed serving PowerDNS: %+v", pdns)
	}
	var notCommittedAction string
	if err := panel.db.GetDB().QueryRow(
		"SELECT action FROM audit_logs WHERE action LIKE 'dns.engine.switch.change_not_committed %'",
	).Scan(&notCommittedAction); err != nil {
		t.Fatalf("adoption change-not-committed audit missing: %v", err)
	}
	if strings.Contains(notCommittedAction, "activation_reverted") {
		t.Fatalf("adoption audit falsely claimed activation reversion: %q", notCommittedAction)
	}
}

func TestDNSEngineUnmanagedBINDCannotBeAdopted(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	bind := agent.runtimes[transport.DNSEngineBIND]
	bind.Installed, bind.Running, bind.Managed = true, true, false
	agent.runtimes[transport.DNSEngineBIND] = bind
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || preview.Action != "switch" ||
		!hasDNSEngineBlocker(preview, "unmanaged_dns_detected") {
		t.Fatalf("unmanaged BIND preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.switchCalls != 0 {
		t.Fatalf("unmanaged BIND preview performed %d switches", agent.switchCalls)
	}
}

func TestDNSEngineActiveSourceCannotRegistrationAdoptUnmanagedStandby(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	first, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEnginePowerDNS, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(first.Blockers) != 0 {
		t.Fatalf("first PowerDNS preview status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("8", 32), transport.DNSEnginePowerDNS,
		nil, 0, first.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("first PowerDNS commit status=%d body=%s",
			commit.Code, commit.Body.String())
	}
	agent.mu.Lock()
	bind := agent.runtimes[transport.DNSEngineBIND]
	bind.Installed, bind.Running, bind.Managed = true, false, false
	agent.runtimes[transport.DNSEngineBIND] = bind
	beforeCalls := agent.switchCalls
	agent.mu.Unlock()
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND,
		transport.DNSEnginePowerDNS, state.Revision,
	)
	if recorder.Code != http.StatusOK || preview.Action != "switch" ||
		!hasDNSEngineBlocker(preview, "unmanaged_dns_detected") {
		t.Fatalf("unmanaged standby preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.switchCalls != beforeCalls {
		t.Fatalf("blocked unmanaged standby performed a host mutation")
	}
}

func TestActiveDNSPublisherBindsEpochAndFailsClosed(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("2", 32), transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	agent.mu.Lock()
	dnssecCalls := agent.dnssecCalls
	agent.mu.Unlock()
	identity, ready, err := panel.activeDNSPublisher(context.Background())
	if err != nil || !ready ||
		identity.Engine != transport.DNSEngineBIND || identity.Epoch != 1 {
		t.Fatalf("publisher identity=%+v ready=%v err=%v", identity, ready, err)
	}
	agent.mu.Lock()
	if agent.dnssecCalls != dnssecCalls {
		t.Fatalf("lightweight publisher performed DNSSEC probes")
	}
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	agent.mu.Unlock()
	if _, ready, err := panel.activeDNSPublisher(context.Background()); err != nil || ready {
		t.Fatalf("conflicting runtime publisher ready=%v err=%v", ready, err)
	}
	agent.mu.Lock()
	pdns.Running = false
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	agent.mu.Unlock()
	persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEnginePowerDNS, strings.Repeat("3", 32),
	)
	if _, ready, err := panel.activeDNSPublisher(context.Background()); err != nil || ready {
		t.Fatalf("attached switch publisher ready=%v err=%v", ready, err)
	}
}

func TestDNSEngineBrowserCancellationDoesNotStrandSwitch(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	agent.onSwitch = cancel
	body, _ := json.Marshal(map[string]any{
		"request_id":            strings.Repeat("4", 32),
		"target_engine":         transport.DNSEngineBIND,
		"expected_source":       nil,
		"expected_revision":     0,
		"preview_token":         preview.PreviewToken,
		"downtime_acknowledged": false,
	})
	commit := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/dns/engine/switch",
		strings.NewReader(string(body)),
	).WithContext(ctx)
	panel.handleDNSEngineSwitch(commit, request)
	if commit.Code != http.StatusOK {
		t.Fatalf("canceled-browser commit status=%d body=%s",
			commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 1 || state.CurrentSwitchID != "" {
		t.Fatalf("canceled-browser state=%+v", state)
	}
}

func persistEmptyDNSEngineSwitchForTest(
	t *testing.T,
	panel *Panel,
	target transport.DNSEngine,
	requestID string,
) persistedDNSEngineSwitch {
	t.Helper()
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	mode := transport.DNSEngineSwitchModeSwitch
	action := "install"
	topology := state.Topology
	if state.ActiveEngine != "" {
		action = "switch"
	}
	if target == transport.DNSEnginePowerDNS &&
		state.ActiveEngine == "" &&
		normalizeDNSRole(panel.setting(
			context.Background(), settingDNSRole,
		)) == transport.DNSTopologyPaired {
		mode = transport.DNSEngineSwitchModeAdopt
		action = "adopt"
		topology = transport.DNSTopologyPaired
	}
	peerIP, peerNS := "", ""
	if topology == transport.DNSTopologyPaired {
		snapshot, snapshotErr := panel.dnsClusterAgentSnapshot(context.Background())
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		peerIP, peerNS = snapshot.PeerIP, snapshot.PeerNS
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPeer(
		mode,
		state.ActiveEngine, target, state.EngineEpoch, state.EngineEpoch+1,
		state.Revision, topology, peerIP, peerNS, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := dnsEngineSwitchRequest{
		RequestID: requestID, TargetEngine: target,
		ExpectedSource:   nullableDNSEngine{Set: true},
		ExpectedRevision: state.Revision,
	}
	ownerID, _ := newServiceOperationID()
	switchID, _ := newServiceOperationID()
	persisted, err := panel.persistDNSEngineSwitch(
		context.Background(), request, ownerID, switchID, action, manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	return persisted
}

func ensureActiveDNSEngineForTest(
	t *testing.T,
	panel *Panel,
	engine transport.DNSEngine,
) {
	t.Helper()
	// A few transport-only unit tests intentionally construct a Panel without
	// a database. They do not exercise DNS authority and need no durable seed.
	if panel == nil || panel.db == nil || panel.db.GetDB() == nil {
		return
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine == engine && state.CurrentSwitchID == "" {
		alignActivePowerDNSTopologyForTest(t, panel, engine)
		return
	}
	if state.ActiveEngine != "" || state.CurrentSwitchID != "" {
		t.Fatalf("cannot seed DNS engine %q from state %+v", engine, state)
	}
	requestChar := "4"
	if engine == transport.DNSEngineBIND {
		requestChar = "5"
	}
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, engine, strings.Repeat(requestChar, 32),
	)
	if err := panel.finalizeDNSEngineSwitchSuccess(
		context.Background(), persisted,
	); err != nil {
		t.Fatalf("seed active DNS engine %q: %v", engine, err)
	}
	if err := panel.clearDNSEnginePostCommitMarker(
		context.Background(), persisted,
	); err != nil {
		t.Fatalf("finalize test DNS engine follow-up: %v", err)
	}
	alignActivePowerDNSTopologyForTest(t, panel, engine)
}

func alignActivePowerDNSTopologyForTest(
	t *testing.T,
	panel *Panel,
	engine transport.DNSEngine,
) {
	t.Helper()
	if engine != transport.DNSEnginePowerDNS {
		return
	}
	role := normalizeDNSRole(panel.setting(context.Background(), settingDNSRole))
	if role == "" {
		role = transport.DNSTopologyStandalone
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.Topology == role {
		return
	}
	if pending := panel.setting(
		context.Background(), dnsClusterSagaSetting,
	); pending != "" {
		t.Fatalf("pending DNS cluster saga conflicts with test topology %q -> %q",
			state.Topology, role)
	}
	if role != transport.DNSTopologyPaired ||
		state.Topology != transport.DNSTopologyStandalone {
		t.Fatalf("cannot align test PowerDNS topology %q -> %q",
			state.Topology, role)
	}
	marker, err := json.Marshal(map[string]any{
		"version": 1,
		"phase":   dnsClusterSagaPublished,
		"previous": map[string]any{
			"role": state.Topology,
		},
		"desired": map[string]any{
			"role": role,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.setSetting(
		context.Background(), dnsClusterSagaSetting, string(marker),
	); err != nil {
		t.Fatal(err)
	}
	result, err := panel.db.GetDB().Exec(`
		UPDATE dns_engine_state
		SET topology = ?, revision = revision + 1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE singleton_id = 1 AND active_engine = 'pdns'
		  AND topology = ? AND current_switch_id IS NULL`,
		role, state.Topology,
	)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		t.Fatalf("align test PowerDNS topology rows=%d err=%v", changed, err)
	}
	if err := panel.setSetting(
		context.Background(), dnsClusterSagaSetting, "",
	); err != nil {
		t.Fatal(err)
	}
}

func terminalDNSEngineJob(
	persisted persistedDNSEngineSwitch,
	status string,
) *agentMutationJob {
	now := time.Now().UTC()
	job := &agentMutationJob{
		RequestID: persisted.RequestID, OwnerID: persisted.OwnerID,
		Kind: dnsEngineSwitchKind, Target: string(persisted.TargetEngine),
		PackageName: persisted.Qualifier, Status: status, Attempt: 1,
		StartedAt:  now.Add(-2 * time.Minute),
		UpdatedAt:  now.Add(-time.Minute),
		DeadlineAt: now.Add(time.Hour),
		FinishedAt: now,
	}
	if status == agentMutationSucceeded {
		job.Phase = dnsEngineSwitchFinalizedPhasePrefix +
			persisted.RequestID + "/" + persisted.Qualifier
	} else {
		job.Phase = "failed"
	}
	return job
}

func TestDNSEngineRecoveryLegacyV1ReceiptRemainsRecoveryRequired(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEngineBIND, strings.Repeat("d", 32),
	)
	job := terminalDNSEngineJob(persisted, agentMutationSucceeded)
	job.Phase = dnsEngineSwitchLegacyPublishedPhasePrefix +
		persisted.RequestID + "/" + persisted.Qualifier
	setDNSEngineMutationJobForTest(t, agent, job, false)

	handled, err := panel.recoverDNSEngineSwitchLocked(context.Background(), job)
	if !handled || !errors.Is(err, errAgentMutationRecoveryRequired) {
		t.Fatalf("legacy recovery handled=%v err=%v, want recovery required", handled, err)
	}
	state, readErr := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if state.CurrentSwitchID != persisted.SwitchID || state.ActiveEngine != "" ||
		state.EngineEpoch != 0 {
		t.Fatalf("legacy receipt changed panel authority: %+v", state)
	}
	operation, readErr := readPresentedDNSEngineOperation(
		context.Background(), panel.db.GetDB(), persisted.SwitchID,
	)
	if readErr != nil {
		t.Fatal(readErr)
	}
	panel.enrichAttachedDNSEngineOperation(
		context.Background(), operation, persisted.SwitchID,
	)
	if operation.Status != "recovery_required" ||
		operation.LastError != "The DNS engine switch is waiting for privileged recovery finalization." {
		t.Fatalf("legacy presented operation = %+v", operation)
	}

	recovered, startupErr := panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if startupErr != nil || recovered != 0 {
		t.Fatalf(
			"legacy startup recovery recovered=%d err=%v, want nonfatal operator gate",
			recovered, startupErr,
		)
	}
	state, readErr = readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if state.CurrentSwitchID != persisted.SwitchID || state.ActiveEngine != "" ||
		state.EngineEpoch != 0 {
		t.Fatalf("nonfatal startup changed legacy authority: %+v", state)
	}
}

func TestDNSEngineRecoveryMalformedSuccessRemainsRecoveryRequired(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEngineBIND, strings.Repeat("c", 32),
	)
	job := terminalDNSEngineJob(persisted, agentMutationSucceeded)
	job.Phase = "completed"
	setDNSEngineMutationJobForTest(t, agent, job, false)

	recovered, err := panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 0 {
		t.Fatalf(
			"malformed startup recovery recovered=%d err=%v, want nonfatal operator gate",
			recovered, err,
		)
	}
	operation, err := readPresentedDNSEngineOperation(
		context.Background(), panel.db.GetDB(), persisted.SwitchID,
	)
	if err != nil {
		t.Fatal(err)
	}
	panel.enrichAttachedDNSEngineOperation(
		context.Background(), operation, persisted.SwitchID,
	)
	if operation.Status != "recovery_required" ||
		operation.LastError != "The DNS engine switch is waiting for privileged recovery finalization." {
		t.Fatalf("malformed presented operation = %+v", operation)
	}
}

func TestDNSEngineRecoveryForwardFinalizesLostResponse(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEngineBIND, strings.Repeat("e", 32),
	)
	agent.mu.Lock()
	target := agent.runtimes[transport.DNSEngineBIND]
	target.Installed, target.Running, target.Managed = true, true, true
	agent.runtimes[transport.DNSEngineBIND] = target
	agent.mu.Unlock()
	job := terminalDNSEngineJob(persisted, agentMutationSucceeded)
	handled, err := panel.recoverDNSEngineSwitchLocked(context.Background(), job)
	if err != nil || !handled {
		t.Fatalf("response-loss recovery handled=%v err=%v", handled, err)
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 1 || state.CurrentSwitchID != "" {
		t.Fatalf("recovered state=%+v", state)
	}
	var userID sql.NullInt64
	var ip, userAgent sql.NullString
	if err := panel.db.GetDB().QueryRow(`
		SELECT user_id, ip_address, user_agent
		FROM audit_logs
		WHERE action LIKE 'dns.engine.switch.recovered.succeeded %'
	`).Scan(&userID, &ip, &userAgent); err != nil {
		t.Fatal(err)
	}
	if userID.Valid || ip.Valid || userAgent.Valid {
		t.Fatalf(
			"startup recovery audit was not system-attributed: user=%v ip=%v ua=%v",
			userID, ip, userAgent,
		)
	}
}

func TestDNSEngineRecoveryKeepsLockWithoutRollbackRuntimeProof(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistEmptyDNSEngineSwitchForTest(
		t, panel, transport.DNSEngineBIND, strings.Repeat("f", 32),
	)
	agent.mu.Lock()
	target := agent.runtimes[transport.DNSEngineBIND]
	target.Installed, target.Running, target.Managed = true, true, true
	agent.runtimes[transport.DNSEngineBIND] = target
	agent.mu.Unlock()
	job := terminalDNSEngineJob(persisted, agentMutationFailed)
	handled, err := panel.recoverDNSEngineSwitchLocked(context.Background(), job)
	if err == nil || !handled {
		t.Fatalf("unproven rollback handled=%v err=%v", handled, err)
	}
	state, readErr := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if state.CurrentSwitchID != persisted.SwitchID || state.ActiveEngine != "" {
		t.Fatalf("unproven rollback detached authority: %+v", state)
	}
}

func setDNSEngineMutationJobForTest(
	t *testing.T,
	agent *dnsEngineTestAgent,
	job *agentMutationJob,
	active bool,
) {
	t.Helper()
	agent.durableMutationRPCFixture.mu.Lock()
	defer agent.durableMutationRPCFixture.mu.Unlock()
	if agent.durableMutationRPCFixture.jobs == nil {
		agent.durableMutationRPCFixture.jobs =
			make(map[string]*ServiceOperationMutationJob)
	}
	agent.durableMutationRPCFixture.jobs[job.RequestID] =
		serviceOperationMutationJobFromAgent(job)
	agent.durableMutationRPCFixture.active = ""
	if active {
		agent.durableMutationRPCFixture.active = job.RequestID
	}
}

func serviceOperationMutationJobFromAgent(
	job *agentMutationJob,
) *ServiceOperationMutationJob {
	if job == nil {
		return nil
	}
	return &ServiceOperationMutationJob{
		RequestID: job.RequestID, OwnerID: job.OwnerID,
		Kind: job.Kind, Target: job.Target, PackageName: job.PackageName,
		Status: job.Status, Phase: job.Phase, Attempt: job.Attempt,
		StartedAt: job.StartedAt, UpdatedAt: job.UpdatedAt,
		LeaseExpiresAt: job.LeaseExpiresAt, DeadlineAt: job.DeadlineAt,
		FinishedAt: job.FinishedAt,
		ErrorCode:  job.ErrorCode, ErrorMessage: job.ErrorMessage,
		WorkerPID: job.WorkerPID, WorkerStarted: job.WorkerStarted,
		WorkerCommand: job.WorkerCommand,
	}
}

func reconcileDNSEngineForTest(
	t *testing.T,
	panel *Panel,
	method string,
) *httptest.ResponseRecorder {
	t.Helper()
	if _, err := panel.db.GetDB().Exec(`
		INSERT OR IGNORE INTO users (
		  id, username, password_hash, email, role
		) VALUES (1, 'dns-reconcile-admin', 'hash',
		          'dns-reconcile-admin@example.test', 'admin')
	`); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		method, "/api/v1/dns/engine/reconcile", nil,
	)
	request.RemoteAddr = "198.51.100.45:54321"
	request.Header.Set("User-Agent", "dns-reconcile-test-client")
	request = request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin},
	))
	panel.handleDNSEngineReconcile(recorder, request)
	return recorder
}

func newFailedBINDReconcileFixture(
	t *testing.T,
) (*Panel, *dnsEngineTestAgent, persistedDNSEngineSwitch, *agentMutationJob) {
	t.Helper()
	t.Setenv("CELIKPANEL_SERVER_IP", "72.62.38.15")
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "paired")
	agent := newDNSEngineTestAgent()
	target := agent.runtimes[transport.DNSEngineBIND]
	target.Installed, target.Running, target.Managed = true, false, true
	agent.runtimes[transport.DNSEngineBIND] = target
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistInitialBINDReconcileSwitchForTest(
		t, panel, strings.Repeat("6", 32),
	)
	job := terminalDNSEngineJob(persisted, agentMutationFailed)
	job.ErrorCode = "service_operation_lease_lost"
	job.ErrorMessage = "The privileged host operation lost its durable lease."
	return panel, agent, persisted, job
}

func persistInitialBINDReconcileSwitchForTest(
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
		context.Background(), state, transport.DNSEngineBIND,
		"install", transport.DNSTopologyPaired,
	)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, _ := newServiceOperationID()
	switchID, _ := newServiceOperationID()
	persisted, err := panel.persistDNSEngineSwitch(
		context.Background(),
		dnsEngineSwitchRequest{
			RequestID: requestID, TargetEngine: transport.DNSEngineBIND,
			ExpectedSource:   nullableDNSEngine{Set: true},
			ExpectedRevision: state.Revision,
		},
		ownerID, switchID, "install", manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	return persisted
}

func TestDNSEngineReconcileClearsExactFrankfurtFailedInstallOnly(t *testing.T) {
	panel, agent, persisted, job := newFailedBINDReconcileFixture(t)
	setDNSEngineMutationJobForTest(t, agent, job, false)
	if persisted.Topology != transport.DNSTopologyPaired ||
		persisted.PairRole != transport.DNSPairRolePrimary ||
		persisted.LocalIP != "72.62.38.15" ||
		persisted.LocalNS != "ns1.celikhost.com" ||
		persisted.PeerIP != "2.25.80.4" ||
		persisted.PeerNS != "ns2.celikhost.com" {
		t.Fatalf("Frankfurt frozen pair identity=%+v", persisted)
	}
	manifest, err := panel.reconstructPersistedDNSEngineManifest(
		context.Background(), persisted,
	)
	if err != nil {
		t.Fatalf("reconstruct Frankfurt manifest: %v", err)
	}
	if _, err := panel.verifyDNSEngineRollbackEvidence(
		context.Background(), persisted, manifest,
	); err != nil {
		t.Fatalf("verify Frankfurt rollback evidence: %v", err)
	}
	if err := panel.verifyDNSEngineRollbackRuntime(
		context.Background(), persisted,
	); err != nil {
		t.Fatalf("verify Frankfurt rollback runtime: %v", err)
	}
	agent.mu.Lock()
	agent.rollbackEvidenceCalls = 0
	agent.rollbackEvidenceRequests = nil
	agent.readinessCalls = 0
	agent.mu.Unlock()

	response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status=%d body=%s", response.Code, response.Body.String())
	}
	var outcome map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if len(outcome) != 1 || !outcome["reconciled"] {
		t.Fatalf("reconcile outcome=%v body=%s", outcome, response.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSwitchID != "" || state.ActiveEngine != "" ||
		state.EngineEpoch != 0 || state.Revision != 2 {
		t.Fatalf("reconciled state=%+v", state)
	}
	marker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker != nil {
		t.Fatalf("reconciled marker=%+v err=%v", marker, err)
	}
	var phase string
	if err := panel.db.GetDB().QueryRow(`
		SELECT phase FROM dns_engine_switch_snapshots
		WHERE switch_id = ?`, persisted.SwitchID,
	).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != "rolled_back" {
		t.Fatalf("reconciled switch phase=%q", phase)
	}

	agent.mu.Lock()
	target := agent.runtimes[transport.DNSEngineBIND]
	evidenceCalls := agent.rollbackEvidenceCalls
	evidenceRequests := append(
		[]transport.DNSEngineRollbackEvidenceRequest(nil),
		agent.rollbackEvidenceRequests...,
	)
	switchCalls := agent.switchCalls
	statusCalls := agent.mutationStatusCalls
	agent.mu.Unlock()
	if !target.Installed || target.Running || !target.Managed {
		t.Fatalf("transitional installed BIND artifact was not preserved: %+v", target)
	}
	if switchCalls != 0 || statusCalls != 1 ||
		evidenceCalls != 2 || len(evidenceRequests) != 2 {
		t.Fatalf("switch calls=%d status calls=%d evidence calls=%d requests=%d",
			switchCalls, statusCalls, evidenceCalls, len(evidenceRequests))
	}
	for _, request := range evidenceRequests {
		if request.MutationRequestID != persisted.RequestID ||
			request.MutationOwnerID != persisted.OwnerID ||
			request.TargetEngine != persisted.TargetEngine ||
			request.SourceEpoch != persisted.SourceEpoch ||
			request.TargetEpoch != persisted.TargetEpoch ||
			request.ManifestQualifier != persisted.Qualifier {
			t.Fatalf("evidence request lost frozen identity: %+v", request)
		}
	}
	var auditUser int
	var auditIP, auditAgent string
	if err := panel.db.GetDB().QueryRow(`
		SELECT user_id, ip_address, user_agent
		FROM audit_logs
		WHERE action LIKE 'dns.engine.switch.reconciled_operation %'
	`).Scan(&auditUser, &auditIP, &auditAgent); err != nil {
		t.Fatal(err)
	}
	if auditUser != 1 || auditIP != "198.51.100.45" ||
		auditAgent != "dns-reconcile-test-client" {
		t.Fatalf("reconcile audit user=%d ip=%q agent=%q",
			auditUser, auditIP, auditAgent)
	}

	second := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if second.Code != http.StatusOK {
		t.Fatalf("idempotent reconcile status=%d body=%s",
			second.Code, second.Body.String())
	}
	var secondOutcome map[string]bool
	if err := json.Unmarshal(second.Body.Bytes(), &secondOutcome); err != nil {
		t.Fatal(err)
	}
	if len(secondOutcome) != 1 || secondOutcome["reconciled"] {
		t.Fatalf("idempotent reconcile outcome=%v body=%s",
			secondOutcome, second.Body.String())
	}
	after, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	secondEvidenceCalls := agent.rollbackEvidenceCalls
	agent.mu.Unlock()
	if after.Revision != state.Revision || secondEvidenceCalls != evidenceCalls {
		t.Fatalf("idempotent reconcile state=%+v evidence=%d",
			after, secondEvidenceCalls)
	}
}

func TestDNSEngineReconcileRetainsEveryUnprovenMarker(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dnsEngineTestAgent, persistedDNSEngineSwitch, *ServiceOperationMutationJob)
	}{
		{
			name: "missing exact terminal receipt",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				agent.rollbackEvidenceOutcome =
					transport.DNSEngineRollbackIdentityMismatch
			},
		},
		{
			name: "terminal success receipt",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				agent.rollbackEvidenceOutcome =
					transport.DNSEngineRollbackIdentityMismatch
			},
		},
		{
			name: "failed job with published phase",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				agent.rollbackEvidenceOutcome =
					transport.DNSEngineRollbackIdentityMismatch
			},
		},
		{
			name: "identity mismatch",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				agent.rollbackEvidenceOutcome =
					transport.DNSEngineRollbackIdentityMismatch
			},
		},
		{
			name: "terminal receipt retains lease",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				agent.rollbackEvidenceOutcome =
					transport.DNSEngineRollbackIdentityMismatch
			},
		},
		{
			name: "active global mutation",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				agent.rollbackEvidenceOutcome =
					transport.DNSEngineRollbackActiveOperation
			},
		},
		{
			name: "bounded agent evidence refuses journal",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				agent.rollbackEvidenceOutcome =
					transport.DNSEngineRollbackJournalPresent
			},
		},
		{
			name: "terminal receipt commitment is malformed",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				agent.rollbackEvidenceCommitment = "raw-receipt-detail"
			},
		},
		{
			name: "target runtime still serving",
			mutate: func(agent *dnsEngineTestAgent, _ persistedDNSEngineSwitch, _ *ServiceOperationMutationJob) {
				target := agent.runtimes[transport.DNSEngineBIND]
				target.Running = true
				agent.runtimes[transport.DNSEngineBIND] = target
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, agent, persisted, agentJob := newFailedBINDReconcileFixture(t)
			job := serviceOperationMutationJobFromAgent(agentJob)
			agent.durableMutationRPCFixture.mu.Lock()
			agent.durableMutationRPCFixture.jobs =
				map[string]*ServiceOperationMutationJob{job.RequestID: job}
			test.mutate(agent, persisted, job)
			agent.durableMutationRPCFixture.mu.Unlock()

			response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
			if response.Code != http.StatusBadGateway {
				t.Fatalf("unproven reconcile status=%d body=%s",
					response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), persisted.RequestID) ||
				strings.Contains(response.Body.String(), persisted.Qualifier) ||
				strings.Contains(response.Body.String(), "journal_present") {
				t.Fatalf("reconcile leaked proof detail: %s", response.Body.String())
			}
			state, err := readDNSEngineDBState(
				context.Background(), panel.db.GetDB(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if state.CurrentSwitchID != persisted.SwitchID ||
				state.ActiveEngine != "" || state.Revision != 1 {
				t.Fatalf("unproven reconcile changed state: %+v", state)
			}
			marker, err := readDNSEngineOperationMarker(
				context.Background(), panel.db.GetDB(),
			)
			if err != nil || marker == nil ||
				marker.SwitchID != persisted.SwitchID ||
				marker.Phase != dnsEngineOperationAccepted {
				t.Fatalf("unproven reconcile marker=%+v err=%v", marker, err)
			}
		})
	}
}

func TestDNSEngineReconcileScopeIsInitialBINDInstallOnly(t *testing.T) {
	base := persistedDNSEngineSwitch{
		Mode:         transport.DNSEngineSwitchModeSwitch,
		Action:       "install",
		TargetEngine: transport.DNSEngineBIND,
		TargetEpoch:  1,
		Topology:     transport.DNSTopologyStandalone,
	}
	if err := validateInitialBINDInstallReconcileScope(base); err != nil {
		t.Fatalf("exact Frankfurt scope rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*persistedDNSEngineSwitch)
	}{
		{name: "source present", mutate: func(persisted *persistedDNSEngineSwitch) {
			persisted.SourceEngine = transport.DNSEnginePowerDNS
		}},
		{name: "source epoch", mutate: func(persisted *persistedDNSEngineSwitch) {
			persisted.SourceEpoch = 1
		}},
		{name: "adopt", mutate: func(persisted *persistedDNSEngineSwitch) {
			persisted.Mode = transport.DNSEngineSwitchModeAdopt
			persisted.Action = "adopt"
		}},
		{name: "reconfigure", mutate: func(persisted *persistedDNSEngineSwitch) {
			persisted.Action = "reconfigure"
		}},
		{name: "PowerDNS target", mutate: func(persisted *persistedDNSEngineSwitch) {
			persisted.TargetEngine = transport.DNSEnginePowerDNS
		}},
		{name: "later target epoch", mutate: func(persisted *persistedDNSEngineSwitch) {
			persisted.TargetEpoch = 2
		}},
		{name: "paired secondary", mutate: func(persisted *persistedDNSEngineSwitch) {
			persisted.Topology = transport.DNSTopologyPaired
			persisted.PairRole = transport.DNSPairRoleSecondary
			persisted.LocalIP, persisted.LocalNS = "192.0.2.10", "ns1.example.test"
			persisted.PeerIP, persisted.PeerNS = "192.0.2.11", "ns2.example.test"
		}},
	}
	paired := base
	paired.Topology = transport.DNSTopologyPaired
	paired.PairRole = transport.DNSPairRolePrimary
	paired.LocalIP, paired.LocalNS = "192.0.2.10", "ns1.example.test"
	paired.PeerIP, paired.PeerNS = "192.0.2.11", "ns2.example.test"
	if err := validateInitialBINDInstallReconcileScope(paired); err != nil {
		t.Fatalf("exact paired-primary Frankfurt scope rejected: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			persisted := base
			test.mutate(&persisted)
			if err := validateInitialBINDInstallReconcileScope(persisted); err == nil {
				t.Fatalf("unsupported reconciliation scope was accepted: %+v", persisted)
			}
		})
	}
}

func TestDNSEngineReconcileDoubleReadAndConcurrentLockFailClosed(t *testing.T) {
	t.Run("terminal receipt changes after runtime proof", func(t *testing.T) {
		panel, agent, persisted, job := newFailedBINDReconcileFixture(t)
		setDNSEngineMutationJobForTest(t, agent, job, false)
		agent.onReadiness = func(call int) {
			if call != 1 {
				return
			}
			agent.mu.Lock()
			agent.rollbackEvidenceCommitment = strings.Repeat("b", 64)
			agent.mu.Unlock()
		}
		response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("changed receipt status=%d body=%s",
				response.Code, response.Body.String())
		}
		state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
		if err != nil {
			t.Fatal(err)
		}
		if state.CurrentSwitchID != persisted.SwitchID {
			t.Fatalf("changed receipt detached switch: %+v", state)
		}
	})

	t.Run("PowerDNS appears after runtime proof", func(t *testing.T) {
		panel, agent, persisted, job := newFailedBINDReconcileFixture(t)
		setDNSEngineMutationJobForTest(t, agent, job, false)
		agent.rollbackEvidenceOutcomes = map[int]string{
			2: transport.DNSEngineRollbackRuntimeUnsealed,
		}
		agent.onReadiness = func(call int) {
			if call != 1 {
				return
			}
			agent.mu.Lock()
			pdns := agent.runtimes[transport.DNSEnginePowerDNS]
			pdns.Running = true
			agent.runtimes[transport.DNSEnginePowerDNS] = pdns
			agent.mu.Unlock()
		}
		response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("late PowerDNS status=%d body=%s",
				response.Code, response.Body.String())
		}
		state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
		if err != nil {
			t.Fatal(err)
		}
		agent.mu.Lock()
		evidenceCalls := agent.rollbackEvidenceCalls
		agent.mu.Unlock()
		if state.CurrentSwitchID != persisted.SwitchID ||
			state.Revision != 1 || evidenceCalls != 2 {
			t.Fatalf("late PowerDNS detached switch: state=%+v evidence=%d",
				state, evidenceCalls)
		}
	})

	t.Run("malformed filtered listener appears before second evidence", func(t *testing.T) {
		panel, agent, persisted, job := newFailedBINDReconcileFixture(t)
		setDNSEngineMutationJobForTest(t, agent, job, false)
		// The agent maps any malformed nonblank row returned by its
		// sport=:53 probe to the bounded runtime_unsealed outcome. Simulate
		// that result only on the second evidence read, after the panel's
		// ordinary runtime proof was clean.
		agent.rollbackEvidenceOutcomes = map[int]string{
			2: transport.DNSEngineRollbackRuntimeUnsealed,
		}
		response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
		assertDNSEngineReconcileRetained(
			t, panel, persisted, response, "activating",
		)
		agent.mu.Lock()
		target := agent.runtimes[transport.DNSEngineBIND]
		evidenceCalls := agent.rollbackEvidenceCalls
		statusCalls := agent.mutationStatusCalls
		agent.mu.Unlock()
		if evidenceCalls != 2 || statusCalls != 1 {
			t.Fatalf(
				"malformed listener evidence calls=%d status calls=%d",
				evidenceCalls, statusCalls,
			)
		}
		if !target.Installed || target.Running || !target.Managed {
			t.Fatalf(
				"malformed listener proof changed transitional BIND artifact: %+v",
				target,
			)
		}
	})

	t.Run("panel mutation lock is busy", func(t *testing.T) {
		panel, agent, persisted, job := newFailedBINDReconcileFixture(t)
		setDNSEngineMutationJobForTest(t, agent, job, false)
		panel.serviceMutationMu.Lock()
		response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
		panel.serviceMutationMu.Unlock()
		if response.Code != http.StatusBadGateway {
			t.Fatalf("busy reconcile status=%d body=%s",
				response.Code, response.Body.String())
		}
		state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
		if err != nil {
			t.Fatal(err)
		}
		if state.CurrentSwitchID != persisted.SwitchID {
			t.Fatalf("busy reconcile detached switch: %+v", state)
		}
	})
}

func assertDNSEngineReconcileRetained(
	t *testing.T,
	panel *Panel,
	persisted persistedDNSEngineSwitch,
	response *httptest.ResponseRecorder,
	wantPhase string,
) {
	t.Helper()
	if response.Code != http.StatusBadGateway {
		t.Fatalf("drift reconcile status=%d body=%s",
			response.Code, response.Body.String())
	}
	var current sql.NullString
	if err := panel.db.GetDB().QueryRow(`
		SELECT current_switch_id FROM dns_engine_state
		WHERE singleton_id = 1`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if !current.Valid || current.String != persisted.SwitchID {
		t.Fatalf("drift detached current switch: %+v", current)
	}
	var phase string
	if err := panel.db.GetDB().QueryRow(`
		SELECT phase FROM dns_engine_switch_snapshots
		WHERE switch_id = ?`, persisted.SwitchID).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != wantPhase {
		t.Fatalf("drift snapshot phase=%q want=%q", phase, wantPhase)
	}
	marker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || marker == nil ||
		marker.SwitchID != persisted.SwitchID ||
		marker.Phase != dnsEngineOperationAccepted {
		t.Fatalf("drift marker=%+v err=%v", marker, err)
	}
}

func TestDNSEngineReconcileExactCASRejectsConcurrentAuthorityDrift(t *testing.T) {
	tests := []struct {
		name      string
		wantPhase string
		mutate    func(*Panel, persistedDNSEngineSwitch) error
	}{
		{
			name: "snapshot identity",
			mutate: func(panel *Panel, persisted persistedDNSEngineSwitch) error {
				if _, err := panel.db.GetDB().Exec(
					"DROP TRIGGER dns_engine_switch_snapshot_identity_immutable",
				); err != nil {
					return err
				}
				_, err := panel.db.GetDB().Exec(`
					UPDATE dns_engine_switch_snapshots SET owner_id = ?
					WHERE switch_id = ?`,
					strings.Repeat("7", 32), persisted.SwitchID,
				)
				return err
			},
		},
		{
			name:      "snapshot phase",
			wantPhase: "verifying",
			mutate: func(panel *Panel, persisted persistedDNSEngineSwitch) error {
				_, err := panel.db.GetDB().Exec(`
					UPDATE dns_engine_switch_snapshots SET phase = 'verifying'
					WHERE switch_id = ? AND phase = 'activating'`,
					persisted.SwitchID,
				)
				return err
			},
		},
		{
			name: "source authority",
			mutate: func(panel *Panel, _ persistedDNSEngineSwitch) error {
				_, err := panel.db.GetDB().Exec(`
					UPDATE dns_engine_state SET revision = revision + 1
					WHERE singleton_id = 1`)
				return err
			},
		},
		{
			name: "frozen pair identity",
			mutate: func(panel *Panel, persisted persistedDNSEngineSwitch) error {
				if _, err := panel.db.GetDB().Exec(
					"DROP TRIGGER dns_bind_pair_switch_immutable",
				); err != nil {
					return err
				}
				_, err := panel.db.GetDB().Exec(`
					UPDATE dns_bind_pair_switches SET peer_ip = '2.25.80.5'
					WHERE switch_id = ?`, persisted.SwitchID)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, agent, persisted, job := newFailedBINDReconcileFixture(t)
			setDNSEngineMutationJobForTest(t, agent, job, false)
			var mutationErr error
			agent.onRollbackEvidence = func(call int) {
				if call == 2 {
					mutationErr = test.mutate(panel, persisted)
				}
			}
			response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
			if mutationErr != nil {
				t.Fatal(mutationErr)
			}
			wantPhase := test.wantPhase
			if wantPhase == "" {
				wantPhase = "activating"
			}
			assertDNSEngineReconcileRetained(
				t, panel, persisted, response, wantPhase,
			)
		})
	}
}

func TestDNSEngineReconcileExactCASRejectsSameSizeZoneContentDrift(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "72.62.38.15")
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "paired")
	seedStrictDNSZone(t, panel, "drift.example")
	agent := newDNSEngineTestAgent()
	target := agent.runtimes[transport.DNSEngineBIND]
	target.Installed, target.Running, target.Managed = true, false, true
	agent.runtimes[transport.DNSEngineBIND] = target
	attachDNSEngineTestAgent(t, panel, agent)
	persisted := persistInitialBINDReconcileSwitchForTest(
		t, panel, strings.Repeat("8", 32),
	)
	job := terminalDNSEngineJob(persisted, agentMutationFailed)
	setDNSEngineMutationJobForTest(t, agent, job, false)

	var mutationErr error
	agent.onRollbackEvidence = func(call int) {
		if call != 2 {
			return
		}
		var (
			domain, action, zoneType, recordsJSON string
			desiredGeneration                     int64
		)
		mutationErr = panel.db.GetDB().QueryRow(`
			SELECT zone_name, desired_generation, desired_action,
			       desired_zone_type, records_json
			FROM dns_engine_switch_zones
			WHERE switch_id = ? AND ordinal = 0`,
			persisted.SwitchID,
		).Scan(
			&domain, &desiredGeneration, &action, &zoneType, &recordsJSON,
		)
		if mutationErr != nil {
			return
		}
		var records []transport.ZoneRecord
		if mutationErr = json.Unmarshal([]byte(recordsJSON), &records); mutationErr != nil {
			return
		}
		if len(records) == 0 {
			mutationErr = errors.New("zone drift fixture has no records")
			return
		}
		records[0].TTL++
		encoded, err := mutationpayload.MarshalDNSZoneSnapshotRecords(records)
		if err != nil {
			mutationErr = err
			return
		}
		if len(encoded) != len([]byte(recordsJSON)) {
			mutationErr = errors.New("zone drift changed snapshot byte length")
			return
		}
		commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEngineBIND, persisted.TargetEpoch,
			desiredGeneration, domain, action == "delete", zoneType, records,
		)
		if err != nil {
			mutationErr = err
			return
		}
		if _, err := panel.db.GetDB().Exec(
			"DROP TRIGGER dns_engine_switch_zone_identity_immutable",
		); err != nil {
			mutationErr = err
			return
		}
		_, mutationErr = panel.db.GetDB().Exec(`
			UPDATE dns_engine_switch_zones
			SET records_json = ?, zone_qualifier = ?
			WHERE switch_id = ? AND ordinal = 0`,
			string(encoded), commitment.Qualifier, persisted.SwitchID,
		)
	}
	response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	assertDNSEngineReconcileRetained(
		t, panel, persisted, response, "activating",
	)
	var count int
	var bytes int64
	if err := panel.db.GetDB().QueryRow(`
		SELECT count(*), COALESCE(sum(records_bytes), 0)
		FROM dns_engine_switch_zones WHERE switch_id = ?`,
		persisted.SwitchID,
	).Scan(&count, &bytes); err != nil {
		t.Fatal(err)
	}
	if count != persisted.ZoneCount || bytes != persisted.SnapshotBytes {
		t.Fatalf("zone drift did not preserve count/bytes: count=%d bytes=%d",
			count, bytes)
	}
}

func TestDNSEngineReconcileExactCASRejectsConcurrentMarkerDrift(t *testing.T) {
	panel, agent, persisted, job := newFailedBINDReconcileFixture(t)
	setDNSEngineMutationJobForTest(t, agent, job, false)
	var mutationErr error
	agent.onRollbackEvidence = func(call int) {
		if call != 2 {
			return
		}
		marker, err := readDNSEngineOperationMarker(
			context.Background(), panel.db.GetDB(),
		)
		if err != nil || marker == nil {
			mutationErr = errors.Join(
				errors.New("missing marker drift fixture"), err,
			)
			return
		}
		marker.Phase = dnsEngineOperationPostCommit
		raw, err := encodeDNSEngineOperationMarker(*marker)
		if err != nil {
			mutationErr = err
			return
		}
		_, mutationErr = panel.db.GetDB().Exec(`
			UPDATE panel_settings SET value = ?
			WHERE key = ?`, raw, dnsEngineOperationSetting,
		)
	}
	response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	if response.Code != http.StatusBadGateway {
		t.Fatalf("marker drift status=%d body=%s",
			response.Code, response.Body.String())
	}
	var current sql.NullString
	var phase string
	if err := panel.db.GetDB().QueryRow(`
		SELECT state.current_switch_id, snapshot.phase
		FROM dns_engine_state AS state
		JOIN dns_engine_switch_snapshots AS snapshot
		  ON snapshot.switch_id = state.current_switch_id
		WHERE state.singleton_id = 1`).Scan(&current, &phase); err != nil {
		t.Fatal(err)
	}
	marker, err := readDNSEngineOperationMarker(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil || !current.Valid || current.String != persisted.SwitchID ||
		phase != "activating" || marker == nil ||
		marker.Phase != dnsEngineOperationPostCommit {
		t.Fatalf("marker drift was not atomic: current=%v phase=%q marker=%+v err=%v",
			current, phase, marker, err)
	}
}

func TestDNSEngineReconcilePOSTDoesNotRenderSnapshot(t *testing.T) {
	panel, agent, _, job := newFailedBINDReconcileFixture(t)
	setDNSEngineMutationJobForTest(t, agent, job, false)
	response := reconcileDNSEngineForTest(t, panel, http.MethodPost)
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status=%d body=%s",
			response.Code, response.Body.String())
	}
	var outcome map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if len(outcome) != 1 || !outcome["reconciled"] {
		t.Fatalf("reconcile outcome=%v body=%s", outcome, response.Body.String())
	}
	agent.mu.Lock()
	readinessCalls := agent.readinessCalls
	evidenceCalls := agent.rollbackEvidenceCalls
	statusCalls := agent.mutationStatusCalls
	agent.mu.Unlock()
	if readinessCalls != 1 || evidenceCalls != 2 || statusCalls != 1 {
		t.Fatalf("POST calls readiness=%d evidence=%d status=%d",
			readinessCalls, evidenceCalls, statusCalls)
	}
	if !panel.serviceMutationMu.TryLock() {
		t.Fatal("completed reconciliation retained the service mutation lock")
	}
	panel.serviceMutationMu.Unlock()
}

func TestDNSEngineReconcileRouteContractIsPOSTAndAdminOnly(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	response := reconcileDNSEngineForTest(t, panel, http.MethodGet)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET reconcile status=%d body=%s",
			response.Code, response.Body.String())
	}
	if !isAdminOnlyPath("/api/v1/dns/engine/reconcile") {
		t.Fatal("DNS engine reconciliation route is not admin-only")
	}
}
