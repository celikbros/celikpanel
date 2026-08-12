package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type FirewallSyncApplyRequest = transport.ApplyFirewallRequest

type firewallSyncTestAgent struct {
	durableMutationRPCFixture

	mu sync.Mutex

	status              FirewallStatusResp
	statusErr           error
	installed           []string
	installedErr        error
	applyResponseError  string
	applyErr            error
	applyCommitThenErr  error
	applyCalls          int
	legacyApplyCalls    int
	applyRequests       []FirewallSyncApplyRequest
	installedCalls      int
	issueCalls          int
	legacyIssueCalls    int
	issueRequests       []transport.IssuePanelCertificateRequest
	issueResponse       transport.IssuePanelCertificateResponse
	issueErr            error
	beginCalls          int
	beginRequests       []ServiceOperationMutationBeginRequest
	finishCalls         int
	finishErrOnce       error
	mutationStatusCalls int
	callOrder           []string
	mutationEvents      []string
	statusStarted       chan struct{}
	releaseStatus       chan struct{}
	statusOnce          sync.Once
	versionCapabilities *[]string
	discoveryStarted    chan struct{}
	releaseDiscovery    chan struct{}
	discoveryOnce       sync.Once
}

func (a *firewallSyncTestAgent) Version(
	_ *transport.Empty,
	out *transport.AgentVersionResponse,
) error {
	out.Version = "test"
	out.Commit = strings.TrimSpace(buildCommit)
	capabilities := []string{
		transport.AgentCapabilityFirewallApplyV2,
		transport.AgentCapabilityPanelCertificateIssueV2,
	}
	if a.versionCapabilities != nil {
		capabilities = append([]string(nil), (*a.versionCapabilities)...)
	}
	out.Capabilities = capabilities
	return nil
}

func (a *firewallSyncTestAgent) FirewallStatus(_ *transport.Empty, out *FirewallStatusResp) error {
	a.mu.Lock()
	*out = a.status
	err := a.statusErr
	started := a.statusStarted
	release := a.releaseStatus
	a.mu.Unlock()
	if started != nil {
		a.statusOnce.Do(func() { close(started) })
	}
	if release != nil {
		<-release
	}
	return err
}

func (a *firewallSyncTestAgent) InstalledServiceIDsStrict(_ *transport.Empty, out *[]string) error {
	a.mu.Lock()
	*out = append([]string(nil), a.installed...)
	err := a.installedErr
	started := a.discoveryStarted
	release := a.releaseDiscovery
	a.installedCalls++
	a.mutationEvents = append(a.mutationEvents, "discovery")
	a.mu.Unlock()
	if started != nil {
		a.discoveryOnce.Do(func() { close(started) })
	}
	if release != nil {
		<-release
	}
	return err
}

func (a *firewallSyncTestAgent) ApplyFirewallV2(req *FirewallSyncApplyRequest, out *FirewallStatusResp) error {
	a.mu.Lock()
	a.callOrder = append(a.callOrder, "firewall")
	a.mutationEvents = append(a.mutationEvents, "apply-v2")
	a.applyCalls++
	a.applyRequests = append(a.applyRequests, FirewallSyncApplyRequest{
		ServiceMutationBinding: req.ServiceMutationBinding,
		Enabled:                req.Enabled,
		TCPPorts:               append([]int(nil), req.TCPPorts...),
		UDPPorts:               append([]int(nil), req.UDPPorts...),
		Persist:                req.Persist,
	})
	a.status.Enabled = req.Enabled
	a.status.EngineAvailable = true
	if req.Enabled && req.Persist {
		a.status.PersistenceState = "ready"
		a.status.SnapshotVersion = 2
	}
	*out = a.status
	out.Error = a.applyResponseError
	applyErr := a.applyErr
	commitThenErr := a.applyCommitThenErr
	a.mu.Unlock()
	if commitThenErr != nil {
		var terminal ServiceOperationMutationResponse
		if err := a.durableMutationRPCFixture.FinishServiceMutation(
			&ServiceOperationMutationFinishRequest{
				RequestID: req.MutationRequestID,
				OwnerID:   req.MutationOwnerID,
				Success:   true,
			},
			&terminal,
		); err != nil {
			return err
		}
		return commitThenErr
	}
	return applyErr
}

func (a *firewallSyncTestAgent) ApplyFirewall(
	_ *FirewallSyncApplyRequest,
	out *FirewallStatusResp,
) error {
	a.mu.Lock()
	a.legacyApplyCalls++
	a.mutationEvents = append(a.mutationEvents, "apply-v1")
	a.mu.Unlock()
	*out = FirewallStatusResp{Error: "legacy firewall RPC must not be called"}
	return nil
}

func (a *firewallSyncTestAgent) IssuePanelCertificate(
	req *transport.IssuePanelCertificateRequest,
	out *transport.IssuePanelCertificateResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.legacyIssueCalls++
	*out = transport.IssuePanelCertificateResponse{
		Error: "legacy certificate RPC must not be called",
	}
	return nil
}

func (a *firewallSyncTestAgent) IssuePanelCertificateV2(
	req *transport.IssuePanelCertificateV2Request,
	out *transport.IssuePanelCertificateV2Response,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.issueCalls++
	a.issueRequests = append(a.issueRequests, *req)
	a.callOrder = append(a.callOrder, "issue")
	*out = a.issueResponse
	return a.issueErr
}

func (a *firewallSyncTestAgent) BeginServiceMutation(
	req *ServiceOperationMutationBeginRequest,
	out *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	a.beginCalls++
	a.beginRequests = append(a.beginRequests, *req)
	a.mutationEvents = append(a.mutationEvents, "begin")
	a.mu.Unlock()
	return a.durableMutationRPCFixture.BeginServiceMutation(req, out)
}

func (a *firewallSyncTestAgent) FinishServiceMutation(
	req *ServiceOperationMutationFinishRequest,
	out *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	a.finishCalls++
	forcedErr := a.finishErrOnce
	a.finishErrOnce = nil
	a.mu.Unlock()
	a.durableMutationRPCFixture.mu.Lock()
	existing := cloneServiceOperationMutationJob(
		a.durableMutationRPCFixture.jobs[req.RequestID],
	)
	a.durableMutationRPCFixture.mu.Unlock()
	if existing != nil && existing.OwnerID == req.OwnerID &&
		existing.Status == agentMutationSucceeded {
		out.Job = existing
		return forcedErr
	}
	if err := a.durableMutationRPCFixture.FinishServiceMutation(req, out); err != nil {
		return err
	}
	return forcedErr
}

func (a *firewallSyncTestAgent) ServiceMutationStatus(
	req *ServiceOperationMutationStatusRequest,
	out *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	a.mutationStatusCalls++
	a.mu.Unlock()
	a.durableMutationRPCFixture.mu.Lock()
	defer a.durableMutationRPCFixture.mu.Unlock()
	if req.RequestID != "" {
		out.Job = cloneServiceOperationMutationJob(
			a.durableMutationRPCFixture.jobs[req.RequestID],
		)
		return nil
	}
	out.Job = cloneServiceOperationMutationJob(
		a.durableMutationRPCFixture.jobs[a.durableMutationRPCFixture.active],
	)
	return nil
}

func attachFirewallSyncTestAgent(t *testing.T, panel *Panel, agent *firewallSyncTestAgent) {
	t.Helper()
	panel.pkgFamilyVal = "apt"
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register firewall test agent: %v", err)
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

func TestSyncFirewallDoesNotApplyReducedPolicyWhenDiscoveryFails(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status:       FirewallStatusResp{Enabled: true, EngineAvailable: true},
		installedErr: errors.New("forced discovery failure"),
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)

	err := panel.syncFirewall(context.Background())
	if err == nil || !strings.Contains(err.Error(), "discover installed services") {
		t.Fatalf("syncFirewall error = %v, want installed-service discovery failure", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.applyCalls != 0 {
		t.Fatalf("ApplyFirewall called %d times after incomplete discovery, want 0", agent.applyCalls)
	}
}

func TestSyncFirewallReturnsApplyResponseError(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status:             FirewallStatusResp{Enabled: true, EngineAvailable: true},
		applyResponseError: "forced apply failure",
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)

	err := panel.syncFirewall(context.Background())
	if err == nil || !strings.Contains(err.Error(), "forced apply failure") {
		t.Fatalf("syncFirewall error = %v, want apply response failure", err)
	}
}

func TestSyncFirewallDisabledSkipsDiscovery(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status:       FirewallStatusResp{Enabled: false, EngineAvailable: true},
		installedErr: errors.New("must not be reached"),
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)

	if err := panel.syncFirewall(context.Background()); err != nil {
		t.Fatalf("syncFirewall disabled error = %v", err)
	}
}

func TestSyncFirewallUpdatesWithoutCreatingPersistence(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status:    FirewallStatusResp{Enabled: true, EngineAvailable: true},
		installed: []string{"nginx"},
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	if err := panel.syncFirewall(context.Background()); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.applyRequests) != 1 || agent.applyRequests[0].Persist {
		t.Fatalf("sync request = %+v, want Persist=false", agent.applyRequests)
	}
}

func TestSyncFirewallCommitsFrozenPayloadBeforeV2Begin(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status:    FirewallStatusResp{Enabled: true, EngineAvailable: true},
		installed: []string{"nginx"},
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	if err := panel.syncFirewall(context.Background()); err != nil {
		t.Fatal(err)
	}

	agent.mu.Lock()
	if len(agent.beginRequests) != 1 || len(agent.applyRequests) != 1 {
		agent.mu.Unlock()
		t.Fatalf("begin/apply counts = %d/%d", len(agent.beginRequests), len(agent.applyRequests))
	}
	begin := agent.beginRequests[0]
	request := agent.applyRequests[0]
	events := append([]string(nil), agent.mutationEvents...)
	legacyCalls := agent.legacyApplyCalls
	agent.mu.Unlock()

	commitment, err := mutationpayload.CanonicalFirewallApply(
		request.Enabled, request.Persist, request.TCPPorts, request.UDPPorts,
	)
	if err != nil {
		t.Fatal(err)
	}
	if begin.Kind != "firewall_sync" || begin.Target != "nftables" ||
		begin.PackageName != commitment.Qualifier {
		t.Fatalf("begin = %+v, qualifier = %q", begin, commitment.Qualifier)
	}
	if request.MutationRequestID != begin.RequestID ||
		request.MutationOwnerID != begin.OwnerID {
		t.Fatalf("V2 binding = %+v, begin = %+v", request.ServiceMutationBinding, begin)
	}
	if !reflect.DeepEqual(events, []string{"discovery", "begin", "apply-v2"}) {
		t.Fatalf("firewall events = %v", events)
	}
	if legacyCalls != 0 {
		t.Fatalf("legacy ApplyFirewall calls = %d", legacyCalls)
	}
}

func TestApplyFirewallV2ResponseLossUsesExactTerminalSuccess(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status:             FirewallStatusResp{Enabled: false, EngineAvailable: true},
		applyCommitThenErr: errors.New("lost V2 response"),
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	status, err := panel.applyFirewallSetting(true, false)
	if err != nil {
		t.Fatalf("apply after committed response loss: %v", err)
	}
	if !status.Enabled || !status.EngineAvailable {
		t.Fatalf("refreshed committed status = %+v", status)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.applyCalls != 1 || agent.legacyApplyCalls != 0 {
		t.Fatalf("V2/V1 calls = %d/%d", agent.applyCalls, agent.legacyApplyCalls)
	}
}

func TestApplyCanonicalFirewallV2RejectsTamperedPayloadBeforeBegin(t *testing.T) {
	commitment, err := mutationpayload.CanonicalFirewallApply(
		true, false, []int{80, 443}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	commitment.TCPPorts = append(commitment.TCPPorts, 2083)
	agent := &firewallSyncTestAgent{}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	if _, err := panel.applyCanonicalFirewallV2(
		context.Background(), "firewall_sync", commitment,
	); err == nil || !strings.Contains(err.Error(), "qualifier") {
		t.Fatalf("tampered commitment error = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.beginCalls != 0 || agent.applyCalls != 0 || agent.legacyApplyCalls != 0 {
		t.Fatalf(
			"tampered commitment reached mutation: begin=%d V2=%d V1=%d",
			agent.beginCalls, agent.applyCalls, agent.legacyApplyCalls,
		)
	}
}

func TestSyncFirewallWithExtraTCPIncludesACMEPort(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{Enabled: true, EngineAvailable: true},
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	if err := panel.syncFirewallWithExtraTCP(context.Background(), 80); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.applyRequests) != 1 || !containsInt(agent.applyRequests[0].TCPPorts, 80) {
		t.Fatalf("ACME preflight ports = %+v", agent.applyRequests)
	}
}

func useFastPanelCertificateSagaRetry(t *testing.T) {
	t.Helper()
	previous := panelCertificateSagaRetryDelay
	panelCertificateSagaRetryDelay = 5 * time.Millisecond
	t.Cleanup(func() { panelCertificateSagaRetryDelay = previous })
}

func waitPanelCertificateOperation(
	t *testing.T,
	panel *Panel,
	requestID, wantStatus string,
) serviceOperation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		op, err := panel.serviceOperationByRequestID(context.Background(), requestID)
		if err == nil && op.Status == wantStatus {
			return op
		}
		time.Sleep(10 * time.Millisecond)
	}
	op, err := panel.serviceOperationByRequestID(context.Background(), requestID)
	t.Fatalf("panel certificate operation status = %+v, err=%v, want %s", op, err, wantStatus)
	return serviceOperation{}
}

func TestPanelCertificateFailureRollsBackACMEFirewallAfterIssue(t *testing.T) {
	useFastPanelCertificateSagaRetry(t)
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('cert-admin','x','cert@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{Enabled: true, EngineAvailable: true},
		issueResponse: transport.IssuePanelCertificateResponse{
			ErrorCode: transport.IssuePanelCertificateErrorActivationPending,
			Error:     "forced internal activation detail",
		},
	}
	panel := &Panel{db: database}
	attachFirewallSyncTestAgent(t, panel, agent)
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	t.Setenv("CELIKPANEL_TLS_DIR", panelManagedTLSDirectory)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/settings/panel-certificate",
		strings.NewReader(`{"domain":"panel.example.test","request_id":"11111111111111111111111111111111"}`),
	)
	req = req.WithContext(context.WithValue(
		req.Context(), callerKey, &Caller{ID: int(userID), Role: roleAdmin},
	))
	recorder := httptest.NewRecorder()
	panel.handlePanelCertificate(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	op := waitPanelCertificateOperation(
		t, panel, "11111111111111111111111111111111", serviceOperationFailed,
	)
	if op.Error == nil || op.Error.Message == "forced internal activation detail" {
		t.Fatalf("terminal operation leaked internal detail: %+v", op)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if !reflect.DeepEqual(agent.callOrder, []string{"firewall", "issue", "firewall"}) {
		t.Fatalf("certificate/firewall order = %#v", agent.callOrder)
	}
	if len(agent.applyRequests) != 2 ||
		!containsInt(agent.applyRequests[0].TCPPorts, 80) ||
		containsInt(agent.applyRequests[1].TCPPorts, 80) {
		t.Fatalf("preflight/rollback policies = %+v", agent.applyRequests)
	}
	if agent.beginCalls != 3 || agent.finishCalls != 3 {
		t.Fatalf("durable lifecycle calls = begin %d finish %d, want three direct children", agent.beginCalls, agent.finishCalls)
	}
	if len(agent.issueRequests) != 1 {
		t.Fatalf("certificate requests = %d, want 1", len(agent.issueRequests))
	}
	requestID := agent.issueRequests[0].MutationRequestID
	ownerID := agent.issueRequests[0].MutationOwnerID
	if requestID == "" || ownerID == "" {
		t.Fatalf("certificate mutation binding is empty: %+v", agent.issueRequests[0])
	}
	for i, apply := range agent.applyRequests {
		if apply.MutationRequestID == requestID || apply.MutationOwnerID == ownerID {
			t.Fatalf("firewall child[%d] reused certificate lease: %+v", i, apply)
		}
	}
	if agent.legacyIssueCalls != 0 {
		t.Fatalf("legacy certificate RPC calls = %d", agent.legacyIssueCalls)
	}
}

func TestPanelCertificateRejectsInvalidStoredContactEmailBeforeMutation(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('cert-admin-invalid-email','x','not-an-email','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{Enabled: true, EngineAvailable: true},
	}
	panel := &Panel{db: database}
	attachFirewallSyncTestAgent(t, panel, agent)
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	t.Setenv("CELIKPANEL_TLS_DIR", panelManagedTLSDirectory)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/settings/panel-certificate",
		strings.NewReader(`{"domain":"panel.example.test","request_id":"22222222222222222222222222222222"}`),
	)
	req = req.WithContext(context.WithValue(
		req.Context(), callerKey, &Caller{ID: int(userID), Role: roleAdmin},
	))
	recorder := httptest.NewRecorder()
	panel.handlePanelCertificate(recorder, req)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.beginCalls != 0 || agent.issueCalls != 0 || agent.applyCalls != 0 {
		t.Fatalf(
			"invalid contact email started mutation: begin=%d issue=%d firewall=%d",
			agent.beginCalls, agent.issueCalls, agent.applyCalls,
		)
	}
}

func TestPanelCertificateFinalizeFailureRunsStandaloneCompensationAndAudits(t *testing.T) {
	useFastPanelCertificateSagaRetry(t)
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('cert-finalize-admin','x','cert@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	agent := &firewallSyncTestAgent{
		status:        FirewallStatusResp{Enabled: true, EngineAvailable: true},
		issueResponse: transport.IssuePanelCertificateResponse{Issued: true},
		finishErrOnce: errors.New("forced finish transport failure"),
	}
	panel := &Panel{db: database}
	attachFirewallSyncTestAgent(t, panel, agent)
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	t.Setenv("CELIKPANEL_TLS_DIR", panelManagedTLSDirectory)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/settings/panel-certificate",
		strings.NewReader(`{"domain":" PANEL.Example.TEST. ","request_id":"33333333333333333333333333333333"}`),
	)
	req = req.WithContext(context.WithValue(
		req.Context(), callerKey, &Caller{ID: int(userID), Role: roleAdmin},
	))
	recorder := httptest.NewRecorder()
	panel.handlePanelCertificate(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	op := waitPanelCertificateOperation(
		t, panel, "33333333333333333333333333333333", serviceOperationSucceeded,
	)
	if op.Error != nil {
		t.Fatalf("response-loss reconciled operation error = %+v", op.Error)
	}

	agent.mu.Lock()
	if agent.beginCalls != 3 || agent.finishCalls != 3 {
		agent.mu.Unlock()
		t.Fatalf("durable lifecycle calls = begin %d finish %d, want three direct children", agent.beginCalls, agent.finishCalls)
	}
	if len(agent.issueRequests) != 1 || agent.issueRequests[0].Domain != "panel.example.test" {
		requests := append([]transport.IssuePanelCertificateRequest(nil), agent.issueRequests...)
		agent.mu.Unlock()
		t.Fatalf("canonical certificate request = %+v", requests)
	}
	if len(agent.applyRequests) != 2 {
		requests := append([]FirewallSyncApplyRequest(nil), agent.applyRequests...)
		agent.mu.Unlock()
		t.Fatalf("firewall applications = %+v, want preflight/final", requests)
	}
	if !containsInt(agent.applyRequests[0].TCPPorts, 80) ||
		containsInt(agent.applyRequests[1].TCPPorts, 80) {
		agent.mu.Unlock()
		t.Fatalf("preflight/final firewall policies = %+v", agent.applyRequests)
	}
	issueRequestID := agent.issueRequests[0].MutationRequestID
	issueOwnerID := agent.issueRequests[0].MutationOwnerID
	if agent.applyRequests[0].MutationRequestID == issueRequestID ||
		agent.applyRequests[0].MutationOwnerID == issueOwnerID ||
		agent.applyRequests[1].MutationRequestID == issueRequestID ||
		agent.applyRequests[1].MutationOwnerID == issueOwnerID ||
		agent.applyRequests[0].MutationRequestID == agent.applyRequests[1].MutationRequestID {
		requests := append([]FirewallSyncApplyRequest(nil), agent.applyRequests...)
		agent.mu.Unlock()
		t.Fatalf("child leases were not distinct: issue=%s/%s firewall=%+v",
			issueRequestID, issueOwnerID, requests[:2])
	}
	legacyIssueCalls := agent.legacyIssueCalls
	agent.mu.Unlock()
	if legacyIssueCalls != 0 {
		t.Fatalf("legacy certificate RPC calls = %d", legacyIssueCalls)
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSyncFirewallRejectsNestedMutationBindingBeforeHostAccess(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{Enabled: true, EngineAvailable: true},
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	ctx := withPanelMutationBinding(context.Background(), agentMutationBinding{
		MutationRequestID: "11111111111111111111111111111111",
		MutationOwnerID:   "22222222222222222222222222222222",
	})
	if err := panel.syncFirewall(ctx); !errors.Is(err, errFirewallNestedMutation) {
		t.Fatalf("syncFirewall nested error = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.applyCalls != 0 || agent.installedCalls != 0 || agent.beginCalls != 0 {
		t.Fatalf(
			"nested firewall reached host access: apply=%d discovery=%d begin=%d",
			agent.applyCalls, agent.installedCalls, agent.beginCalls,
		)
	}
}

func TestFirewallGETDoesNotApplyOrCreatePersistence(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{
			Enabled: true, EngineAvailable: true, PersistenceState: "missing",
		},
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firewall", nil)
	req = req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}))
	recorder := httptest.NewRecorder()
	panel.handleFirewall(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"persistence_state":"missing"`) {
		t.Fatalf("GET response = %d %s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.applyCalls != 0 {
		t.Fatalf("GET called ApplyFirewall %d times", agent.applyCalls)
	}
}

func TestSaveForRebootPOSTPersistsAndAuditsExplicitAction(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('firewall-admin','x','firewall@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{
			Enabled: true, EngineAvailable: true, PersistenceState: "missing",
		},
	}
	panel := &Panel{db: database}
	attachFirewallSyncTestAgent(t, panel, agent)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firewall", strings.NewReader(`{"action":"save_for_reboot"}`))
	req = req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: int(userID), Role: roleAdmin}))
	recorder := httptest.NewRecorder()
	panel.handleFirewall(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"persistence_state":"ready"`) {
		t.Fatalf("POST response = %d %s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	if len(agent.applyRequests) != 1 || !agent.applyRequests[0].Persist || !agent.applyRequests[0].Enabled {
		agent.mu.Unlock()
		t.Fatalf("save-for-reboot apply requests = %+v", agent.applyRequests)
	}
	agent.mu.Unlock()
	var count int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='firewall.persistence.enable'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("persistence audit rows = %d, want 1", count)
	}
}

func TestSaveForRebootEnableFailureReturnsErrorAndAuditsFailure(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('firewall-failure-admin','x','firewall-failure@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{
			Enabled: true, EngineAvailable: true, PersistenceState: "missing",
		},
		applyResponseError: "enable firewall restore unit failed",
	}
	panel := &Panel{db: database}
	attachFirewallSyncTestAgent(t, panel, agent)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firewall", strings.NewReader(`{"action":"save_for_reboot"}`))
	req = req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: int(userID), Role: roleAdmin}))
	recorder := httptest.NewRecorder()
	panel.handleFirewall(recorder, req)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "enable firewall restore unit failed") ||
		strings.Contains(recorder.Body.String(), `"persistence_state":"ready"`) {
		t.Fatalf("failed save response = %d %s", recorder.Code, recorder.Body.String())
	}
	var successCount, failureCount int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='firewall.persistence.enable'`).Scan(&successCount); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'firewall.persistence.enable.failed%'`).Scan(&failureCount); err != nil {
		t.Fatal(err)
	}
	if successCount != 0 || failureCount != 1 {
		t.Fatalf("save audit rows: success=%d failure=%d", successCount, failureCount)
	}
}

func TestLiveEnableDoesNotPersistAndExplicitDisableDoes(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{Enabled: false, EngineAvailable: true},
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	for _, tc := range []struct {
		enabled bool
		persist bool
	}{{enabled: true, persist: false}, {enabled: false, persist: true}} {
		st, err := panel.applyFirewallSetting(tc.enabled, tc.persist)
		if err != nil || st.Error != "" {
			t.Fatalf("applyFirewallSetting(%v, %v) = %+v, %v", tc.enabled, tc.persist, st, err)
		}
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.applyRequests) != 2 {
		t.Fatalf("apply requests = %d, want 2", len(agent.applyRequests))
	}
	for _, req := range agent.applyRequests {
		if !validServiceOperationID(req.MutationRequestID) ||
			!validServiceOperationID(req.MutationOwnerID) {
			t.Fatalf("firewall apply escaped durable mutation binding: %+v", req)
		}
	}
	if agent.applyRequests[0].Persist || !agent.applyRequests[0].Enabled {
		t.Fatalf("live enable created persistence: %+v", agent.applyRequests[0])
	}
	if !agent.applyRequests[1].Persist || agent.applyRequests[1].Enabled {
		t.Fatalf("explicit disable did not remove persistence: %+v", agent.applyRequests[1])
	}
}

func TestManualOffWinsAgainstInFlightServiceSync(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	agent := &firewallSyncTestAgent{
		status:           FirewallStatusResp{Enabled: true, EngineAvailable: true},
		discoveryStarted: started,
		releaseDiscovery: release,
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)

	syncDone := make(chan error, 1)
	go func() { syncDone <- panel.syncFirewall(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("service sync did not reach desired-port discovery")
	}
	offDone := make(chan error, 1)
	go func() {
		st, err := panel.applyFirewallSetting(false, true)
		if err == nil && st.Error != "" {
			err = errors.New(st.Error)
		}
		offDone <- err
	}()
	select {
	case err := <-offDone:
		t.Fatalf("manual off crossed in-flight sync mutex: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	if err := <-syncDone; err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if err := <-offDone; err != nil {
		t.Fatalf("manual off failed: %v", err)
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.status.Enabled {
		t.Fatal("service sync re-enabled firewall after manual off")
	}
	if len(agent.applyRequests) != 2 || !agent.applyRequests[0].Enabled || agent.applyRequests[1].Enabled {
		t.Fatalf("apply order = %+v, want sync-on then explicit-off", agent.applyRequests)
	}
	if agent.applyRequests[0].Persist || !agent.applyRequests[1].Persist {
		t.Fatalf("persistence flags = %+v", agent.applyRequests)
	}
}

func TestFirewallPOSTHoldsGlobalMutationLockBeforeDiscovery(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('firewall-lock-admin','x','firewall-lock@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	agent := &firewallSyncTestAgent{
		status:           FirewallStatusResp{Enabled: false, EngineAvailable: true},
		discoveryStarted: started,
		releaseDiscovery: release,
	}
	panel := &Panel{db: database}
	attachFirewallSyncTestAgent(t, panel, agent)

	manualDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/firewall",
			strings.NewReader(`{"enabled":true}`),
		)
		req = req.WithContext(context.WithValue(
			req.Context(), callerKey, &Caller{ID: int(userID), Role: roleAdmin},
		))
		panel.handleFirewall(recorder, req)
		manualDone <- recorder
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("manual firewall POST did not reach desired-port discovery")
	}
	if panel.serviceMutationMu.TryLock() {
		panel.serviceMutationMu.Unlock()
		t.Fatal("manual firewall discovery did not hold the global mutation lock")
	}

	competing := httptest.NewRecorder()
	competingReq := httptest.NewRequest(http.MethodPost, "/api/v1/service/install", nil)
	if releaseCompeting, busy := panel.beginServiceMutation(competing, competingReq); !busy {
		releaseCompeting()
		t.Fatal("competing service operation crossed manual firewall snapshot")
	}
	if competing.Code != http.StatusConflict {
		t.Fatalf("competing admission status=%d body=%s", competing.Code, competing.Body.String())
	}
	close(release)
	select {
	case recorder := <-manualDone:
		if recorder.Code != http.StatusOK {
			t.Fatalf("manual firewall response=%d body=%s", recorder.Code, recorder.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manual firewall POST deadlocked after discovery release")
	}
}
