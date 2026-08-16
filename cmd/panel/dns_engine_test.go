package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type dnsEngineTestAgent struct {
	durableMutationRPCFixture
	mu             sync.Mutex
	runtimes       map[transport.DNSEngine]transport.DNSBackendRuntimeState
	dnssec         bool
	dnssecCalls    int
	switchCalls    int
	switchRequests []transport.SwitchDNSEngineV1Request
	onSwitch       func()
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
	response.Capabilities = []string{
		transport.AgentCapabilityDNSZoneSyncV3,
		transport.AgentCapabilityDNSEngineSwitchV1,
	}
	return nil
}

func (agent *dnsEngineTestAgent) DNSBackendReadiness(
	_ *transport.Empty,
	response *transport.DNSBackendReadinessResponse,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	response.Engines = []transport.DNSBackendRuntimeState{
		agent.runtimes[transport.DNSEnginePowerDNS],
		agent.runtimes[transport.DNSEngineBIND],
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
	copy := *request
	copy.Zones = append([]transport.DNSEngineSwitchZoneSnapshot(nil), request.Zones...)
	agent.switchRequests = append(agent.switchRequests, copy)
	for engine, runtime := range agent.runtimes {
		if engine == request.TargetEngine {
			runtime.Installed, runtime.Running, runtime.Managed = true, true, true
		} else {
			runtime.Running = false
		}
		agent.runtimes[engine] = runtime
	}
	response.Applied = true
	response.ActiveEngine = request.TargetEngine
	response.ActiveEpoch = request.TargetEpoch
	response.AppliedZones = len(request.Zones)
	return nil
}

func (agent *dnsEngineTestAgent) ServiceMutationStatus(
	request *ServiceOperationMutationStatusRequest,
	response *ServiceOperationMutationResponse,
) error {
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
	panel.pkgFamilyVal = "apt"
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
	panel.handleDNSEngineSwitch(recorder, request)
	return recorder
}

func TestDNSEngineFirstInstallAndRequestReplay(t *testing.T) {
	panel := newDNSPanelForTest(t)
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

func TestDNSEnginePairedAndDNSSECPreviewBlockWithoutMutation(t *testing.T) {
	tests := []struct {
		name        string
		paired      bool
		dnssec      bool
		wantBlocker string
	}{
		{name: "paired", paired: true, wantBlocker: "paired_topology_unsupported"},
		{name: "dnssec", dnssec: true, wantBlocker: "dnssec_unsupported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
			attachDNSEngineTestAgent(t, panel, agent)
			preview, recorder := requestDNSEnginePreview(
				t, panel, transport.DNSEngineBIND, nil, 0,
			)
			if recorder.Code != http.StatusOK ||
				!hasDNSEngineBlocker(preview, test.wantBlocker) {
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

func TestDNSEngineStalePreviewCannotStartMutation(t *testing.T) {
	panel := newDNSPanelForTest(t)
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
	status, entries := deriveDNSEnginePresentation(state, fresh, nil)
	if status != dnsEngineStateUnconfigured || entries[0].Status != "available" {
		t.Fatalf("fresh presentation=%s %+v", status, entries)
	}
	pdnsManaged := fresh
	pdnsManaged[transport.DNSEnginePowerDNS] = transport.DNSBackendRuntimeState{
		Engine:    transport.DNSEnginePowerDNS,
		Installed: true, Running: true, Managed: true,
	}
	status, entries = deriveDNSEnginePresentation(state, pdnsManaged, nil)
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
	status, _ = deriveDNSEnginePresentation(state, both, nil)
	if status != dnsEngineStateConflict {
		t.Fatalf("two running backends state=%s", status)
	}
}

func TestDNSEngineManagedPDNSRequiresAndCompletesExplicitAdopt(t *testing.T) {
	panel := newDNSPanelForTest(t)
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
		!preview.RequiresDowntimeAcknowledgement {
		t.Fatalf("managed PDNS adoption preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("1", 32), transport.DNSEnginePowerDNS,
		nil, 0, preview.PreviewToken, true,
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
		state.EngineEpoch != 1 || state.CurrentSwitchID != "" {
		t.Fatalf("adopted state=%+v", state)
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
	if recorder.Code != http.StatusOK || preview.Action != "adopt" ||
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

func TestActiveDNSPublisherBindsEpochAndFailsClosed(t *testing.T) {
	panel := newDNSPanelForTest(t)
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
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		state.ActiveEngine, target, state.EngineEpoch, state.EngineEpoch+1,
		state.Revision, state.Topology, nil,
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
		context.Background(), request, ownerID, switchID, manifest,
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
}

func terminalDNSEngineJob(
	persisted persistedDNSEngineSwitch,
	status string,
) *agentMutationJob {
	job := &agentMutationJob{
		RequestID: persisted.RequestID, OwnerID: persisted.OwnerID,
		Kind: dnsEngineSwitchKind, Target: string(persisted.TargetEngine),
		PackageName: persisted.Qualifier, Status: status,
	}
	if status == agentMutationSucceeded {
		job.Phase = "commit/dns-engine-switch/v1/published/" +
			persisted.RequestID + "/" + persisted.Qualifier
	} else {
		job.Phase = "failed"
	}
	return job
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
