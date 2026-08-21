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

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type dnsEngineTestAgent struct {
	durableMutationRPCFixture
	mu                        sync.Mutex
	runtimes                  map[transport.DNSEngine]transport.DNSBackendRuntimeState
	port53Conflict            bool
	readinessCalls            int
	onReadiness               func(int)
	readinessAfterSwitchError string
	dnssec                    bool
	dnssecCalls               int
	switchCalls               int
	switchRequests            []transport.SwitchDNSEngineV1Request
	switchError               string
	switchErrorLeavesPackage  bool
	onSwitch                  func()
	firewallEnabled           bool
	firewallError             string
	firewallCalls             int
	firewallRequests          []transport.ApplyFirewallRequest
	scanError                 error
	omitDNSCapabilities       bool
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
		!validServiceOperationID(preview.PreviewToken) {
		t.Fatalf("identity blocker preview=%+v status=%d body=%s",
			preview, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("0", 32), transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusConflict {
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

func TestDNSEnginePairedSignedManagedPDNSAdoptionPreservesTopology(t *testing.T) {
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
	if commit.Code != http.StatusOK {
		t.Fatalf("paired signed adoption status=%d body=%s",
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
		agent.switchRequests[0].PeerIP != "2.25.80.4" ||
		agent.switchRequests[0].PeerNS != "ns2.celikhost.com" {
		agent.mu.Unlock()
		t.Fatalf("paired adoption request=%+v", agent.switchRequests)
	}
	agent.mu.Unlock()
	persisted, err := readDNSEngineSwitchByRequest(
		context.Background(), panel.db.GetDB(), strings.Repeat("6", 32),
	)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.PeerIP != "2.25.80.4" || persisted.PeerNS != "ns2.celikhost.com" {
		t.Fatalf("persisted peer tuple=%q/%q", persisted.PeerIP, persisted.PeerNS)
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
