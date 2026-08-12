package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/transport"
)

type ServiceOperationInstallRequest struct {
	MutationRequestID string
	MutationOwnerID   string
	ID                string
	Package           string
}

type ServiceOperationInstallResponse struct {
	Installed bool
	Detail    string
	Unit      string
	Error     string
}

type ServiceOperationNodeRequest struct {
	MutationRequestID string
	MutationOwnerID   string
	Version           string
}

type ServiceOperationMutationBeginRequest struct {
	RequestID   string
	OwnerID     string
	Kind        string
	Target      string
	PackageName string
	Resume      bool
}

type ServiceOperationMutationHeartbeatRequest struct {
	RequestID string
	OwnerID   string
	Phase     string
}

type ServiceOperationMutationStatusRequest struct {
	RequestID string
}

type ServiceOperationMutationCancelRequest struct {
	RequestID      string
	ExpectedOwner  string
	Reason         string
	FailureCode    string
	FailureMessage string
}

type ServiceOperationMutationFinishRequest struct {
	RequestID   string
	OwnerID     string
	Success     bool
	FailureCode string
	Message     string
}

type ServiceOperationMutationJob struct {
	RequestID      string
	OwnerID        string
	Kind           string
	Target         string
	PackageName    string
	Status         string
	Phase          string
	Attempt        int
	LeaseExpiresAt time.Time
	DeadlineAt     time.Time
	ErrorCode      string
	ErrorMessage   string
	WorkerPID      int
	WorkerStarted  string
}

type ServiceOperationMutationResponse struct {
	Job   *ServiceOperationMutationJob
	Error string
}

type ServiceOperationNodeResponse struct {
	Installed bool
	Error     string
}

type ServiceOperationInstancesRequest struct {
	ID string
}

type ServiceOperationInstancesResponse struct {
	Instances []core.ServiceInstance
	Error     string
}

type ServiceOperationDNSResponse struct {
	Synced bool
	Error  string
}

type ServiceOperationVPNRequest struct {
	Port int
}

type ServiceOperationVPNResponse struct {
	Created bool
	Detail  string
	Error   string
}

type ServiceOperationPeerSpec struct {
	PublicKey    string
	PresharedKey string
	IP           string
}

type ServiceOperationPeerRequest struct {
	DesiredGeneration int64
	Peers             []ServiceOperationPeerSpec
}

type ServiceOperationPeerResponse struct {
	Applied           bool
	AppliedGeneration int64
	Error             string
}

type serviceOperationTestAgent struct {
	mu sync.Mutex

	installStarted chan struct{}
	releaseInstall <-chan struct{}
	startOnce      sync.Once

	installCalls atomic.Int32
	nodeCalls    atomic.Int32

	installRPCError error
	installError    string
	installNoop     bool
	installUnit     string
	repoEnabled     bool
	// dropActiveBeforeGetServices simulates a selected unit that was started
	// and reported by InstallService, then died before the final host scan.
	// dropActiveBeforeGetServices, InstallService tarafından başlatılıp
	// bildirilen seçili unit'in son makine taramasından önce ölmesini taklit eder.
	dropActiveBeforeGetServices string
	nodeError                   string
	nodeNoop                    bool
	dnsError                    string
	dnsV2Requests               []transport.SyncDNSZoneV2Request
	versionCapabilities         *[]string
	versionCommit               string
	serviceError                string
	serviceSuccess              bool
	vpnError                    string
	vpnCreated                  bool
	peerError                   string
	peerStarted                 chan struct{}
	releasePeer                 <-chan struct{}
	peerStartOnce               sync.Once
	firewallEnabled             bool
	firewallError               string
	firewallCalls               int
	legacyFirewallCalls         int
	firewallRequests            []transport.ApplyFirewallRequest
	firewallStarted             chan struct{}
	releaseFirewall             <-chan struct{}
	firewallStartOnce           sync.Once

	installed    map[string]bool
	active       map[string]bool
	nodeVersions map[string]bool

	mutationActive   string
	mutationJobs     map[string]*ServiceOperationMutationJob
	mutationDeadline time.Time
	mutationEvents   []string
	finishLossKind   string
	finishLossUsed   bool

	activateAfterGlobalStatus *ServiceOperationMutationJob
	activationTriggered       bool
	activationStarted         chan struct{}
	releaseActivation         <-chan struct{}
}

func (a *serviceOperationTestAgent) Version(
	_ *transport.Empty,
	resp *transport.AgentVersionResponse,
) error {
	resp.Commit = strings.TrimSpace(buildCommit)
	if a.versionCommit != "" {
		resp.Commit = a.versionCommit
	}
	capabilities := []string{
		transport.AgentCapabilityFirewallApplyV2,
		transport.AgentCapabilityPanelCertificateIssueV2,
		transport.AgentCapabilityDNSZoneSyncV2,
		transport.AgentCapabilityMailTLSSyncV2,
	}
	if a.versionCapabilities != nil {
		capabilities = append([]string(nil), (*a.versionCapabilities)...)
	}
	resp.Capabilities = capabilities
	return nil
}

func newServiceOperationTestAgent() *serviceOperationTestAgent {
	return &serviceOperationTestAgent{
		serviceSuccess: true,
		vpnCreated:     true,
		repoEnabled:    true,
		installed:      map[string]bool{},
		active:         map[string]bool{},
		nodeVersions:   map[string]bool{},
		mutationJobs:   map[string]*ServiceOperationMutationJob{},
	}
}

func cloneServiceOperationMutationJob(job *ServiceOperationMutationJob) *ServiceOperationMutationJob {
	if job == nil {
		return nil
	}
	copy := *job
	return &copy
}

func (a *serviceOperationTestAgent) BeginServiceMutation(
	req *ServiceOperationMutationBeginRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if active := a.mutationJobs[a.mutationActive]; active != nil &&
		(active.Status == agentMutationRunning ||
			active.Status == agentMutationCancelling ||
			active.Status == agentMutationOrphaned) {
		if active.RequestID == req.RequestID && active.OwnerID == req.OwnerID {
			resp.Job = cloneServiceOperationMutationJob(active)
			return nil
		}
		resp.Error = "another service mutation owns the host lease"
		resp.Job = cloneServiceOperationMutationJob(active)
		return nil
	}
	now := time.Now().UTC()
	deadline := now.Add(time.Hour)
	if !a.mutationDeadline.IsZero() {
		deadline = a.mutationDeadline
	}
	job := &ServiceOperationMutationJob{
		RequestID: req.RequestID, OwnerID: req.OwnerID, Kind: req.Kind,
		Target: req.Target, PackageName: req.PackageName,
		Status: agentMutationRunning, Phase: "starting", Attempt: 1,
		LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: deadline,
	}
	a.mutationJobs[req.RequestID] = job
	a.mutationActive = req.RequestID
	a.mutationEvents = append(a.mutationEvents, "begin:"+req.Kind)
	resp.Job = cloneServiceOperationMutationJob(job)
	return nil
}

func (a *serviceOperationTestAgent) HeartbeatServiceMutation(
	req *ServiceOperationMutationHeartbeatRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	job := a.mutationJobs[req.RequestID]
	if job == nil || job.OwnerID != req.OwnerID || job.Status != agentMutationRunning {
		resp.Error = "service mutation lease is not running"
		resp.Job = cloneServiceOperationMutationJob(job)
		return nil
	}
	if req.Phase != "" {
		job.Phase = req.Phase
	}
	job.LeaseExpiresAt = time.Now().UTC().Add(time.Minute)
	resp.Job = cloneServiceOperationMutationJob(job)
	return nil
}

func (a *serviceOperationTestAgent) ServiceMutationStatus(
	req *ServiceOperationMutationStatusRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if req.RequestID != "" {
		resp.Job = cloneServiceOperationMutationJob(a.mutationJobs[req.RequestID])
		if resp.Job != nil {
			a.mutationEvents = append(
				a.mutationEvents,
				"status:"+resp.Job.Kind+":"+resp.Job.Status,
			)
		}
		return nil
	}
	resp.Job = cloneServiceOperationMutationJob(a.mutationJobs[a.mutationActive])
	if !a.activationTriggered && a.activateAfterGlobalStatus != nil {
		a.activationTriggered = true
		activation := cloneServiceOperationMutationJob(a.activateAfterGlobalStatus)
		a.mutationJobs[activation.RequestID] = activation
		a.mutationActive = activation.RequestID
		a.mutationEvents = append(a.mutationEvents, "activation:started")
		if a.activationStarted != nil {
			closeOnce(a.activationStarted)
		}
		release := a.releaseActivation
		go func() {
			if release != nil {
				<-release
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			job := a.mutationJobs[activation.RequestID]
			if job == nil || !agentMutationActive(job.Status) {
				return
			}
			job.Status = agentMutationSucceeded
			job.Phase = "completed"
			if a.mutationActive == activation.RequestID {
				a.mutationActive = ""
			}
			a.mutationEvents = append(a.mutationEvents, "activation:succeeded")
		}()
	}
	return nil
}

func (a *serviceOperationTestAgent) CancelServiceMutation(
	req *ServiceOperationMutationCancelRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	job := a.mutationJobs[req.RequestID]
	if job == nil || job.OwnerID != req.ExpectedOwner {
		resp.Error = "service mutation owner mismatch"
		resp.Job = cloneServiceOperationMutationJob(job)
		return nil
	}
	job.Status = agentMutationFailed
	job.Phase = "interrupted"
	job.ErrorCode = req.FailureCode
	job.ErrorMessage = req.FailureMessage
	if a.mutationActive == req.RequestID {
		a.mutationActive = ""
	}
	a.mutationEvents = append(a.mutationEvents, "cancel:"+job.Kind)
	resp.Job = cloneServiceOperationMutationJob(job)
	return nil
}

func (a *serviceOperationTestAgent) FinishServiceMutation(
	req *ServiceOperationMutationFinishRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	job := a.mutationJobs[req.RequestID]
	if job == nil || job.OwnerID != req.OwnerID {
		resp.Error = "service mutation owner mismatch"
		resp.Job = cloneServiceOperationMutationJob(job)
		return nil
	}
	if req.Success {
		job.Status = agentMutationSucceeded
		identity := agentMutationIdentity{
			RequestID:   job.RequestID,
			OwnerID:     job.OwnerID,
			Kind:        job.Kind,
			Target:      job.Target,
			PackageName: job.PackageName,
		}
		phase, required, err := payloadBoundMutationPublishedPhase(identity)
		if required && err == nil {
			job.Phase = phase
		} else {
			job.Phase = "completed"
		}
	} else {
		job.Status = agentMutationFailed
		job.Phase = "failed"
		job.ErrorCode = req.FailureCode
		job.ErrorMessage = req.Message
	}
	if a.mutationActive == req.RequestID {
		a.mutationActive = ""
	}
	a.mutationEvents = append(a.mutationEvents, "finish:"+job.Kind+":"+job.Status)
	resp.Job = cloneServiceOperationMutationJob(job)
	if !a.finishLossUsed && job.Kind == a.finishLossKind {
		a.finishLossUsed = true
		return errors.New("simulated FinishServiceMutation response loss")
	}
	return nil
}

func (a *serviceOperationTestAgent) InstallService(
	req *ServiceOperationInstallRequest,
	resp *ServiceOperationInstallResponse,
) error {
	a.installCalls.Add(1)
	if a.installStarted != nil {
		a.startOnce.Do(func() { close(a.installStarted) })
	}
	if a.releaseInstall != nil {
		<-a.releaseInstall
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.installRPCError != nil {
		return a.installRPCError
	}
	if a.installError != "" {
		resp.Error = a.installError
		return nil
	}
	a.installed[req.ID] = true
	resp.Installed = !a.installNoop
	if a.installUnit != "" {
		resp.Unit = a.installUnit
	} else if req.ID == "postgresql" {
		resp.Unit, _ = core.PostgreSQLClusterUnitForPackage(req.Package)
	}
	if resp.Unit != "" {
		a.active[resp.Unit] = true
	}
	return nil
}

func (a *serviceOperationTestAgent) InstallNodeVersion(
	req *ServiceOperationNodeRequest,
	resp *ServiceOperationNodeResponse,
) error {
	a.nodeCalls.Add(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nodeError != "" {
		resp.Error = a.nodeError
		return nil
	}
	a.nodeVersions[req.Version] = true
	resp.Installed = !a.nodeNoop
	return nil
}

func (a *serviceOperationTestAgent) GetServices(_ *transport.Empty, out *[]core.Service) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dropActiveBeforeGetServices != "" && a.active[a.dropActiveBeforeGetServices] {
		a.active[a.dropActiveBeforeGetServices] = false
		a.dropActiveBeforeGetServices = ""
	}
	services := make([]core.Service, 0, len(a.active))
	for id, active := range a.active {
		if !active {
			continue
		}
		unit := id
		managed := core.GetManagedServiceByID(id)
		if managed != nil && len(managed.SystemNames) > 0 {
			unit = managed.SystemNames[0]
		} else {
			managed = core.ServiceForUnit(id)
		}
		if managed == nil {
			continue
		}
		services = append(services, core.Service{Name: unit, Status: "active (running)"})
	}
	*out = services
	return nil
}

func (a *serviceOperationTestAgent) InstalledServiceIDs(_ *transport.Empty, out *[]string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.installed))
	for id, installed := range a.installed {
		if installed {
			ids = append(ids, id)
		}
	}
	*out = ids
	return nil
}

func (a *serviceOperationTestAgent) ListServiceInstances(
	req *ServiceOperationInstancesRequest,
	resp *ServiceOperationInstancesResponse,
) error {
	if req.ID != "node" {
		resp.Instances = []core.ServiceInstance{}
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for version, installed := range a.nodeVersions {
		if installed {
			resp.Instances = append(resp.Instances, core.ServiceInstance{Version: version, Managed: true})
		}
	}
	return nil
}

func (a *serviceOperationTestAgent) PkgFamily(_ *transport.Empty, out *string) error {
	*out = "apt"
	return nil
}

func (a *serviceOperationTestAgent) FirewallStatus(_ *transport.Empty, out *FirewallStatusResp) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	out.Enabled = a.firewallEnabled
	out.EngineAvailable = true
	return nil
}

func (a *serviceOperationTestAgent) InstalledServiceIDsStrict(
	_ *transport.Empty,
	out *[]string,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*out = (*out)[:0]
	for id, installed := range a.installed {
		if installed {
			*out = append(*out, id)
		}
	}
	return nil
}

func (a *serviceOperationTestAgent) ApplyFirewallV2(
	req *transport.ApplyFirewallRequest,
	out *FirewallStatusResp,
) error {
	if a.firewallStarted != nil {
		a.firewallStartOnce.Do(func() { close(a.firewallStarted) })
	}
	if a.releaseFirewall != nil {
		<-a.releaseFirewall
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.firewallCalls++
	a.firewallRequests = append(a.firewallRequests, transport.ApplyFirewallRequest{
		ServiceMutationBinding: req.ServiceMutationBinding,
		Enabled:                req.Enabled,
		Persist:                req.Persist,
		TCPPorts:               append([]int(nil), req.TCPPorts...),
		UDPPorts:               append([]int(nil), req.UDPPorts...),
	})
	a.mutationEvents = append(a.mutationEvents, "call:firewall_sync")
	if a.firewallError != "" {
		out.Error = a.firewallError
		return nil
	}
	a.firewallEnabled = req.Enabled
	out.Enabled = req.Enabled
	out.EngineAvailable = true
	return nil
}

func (a *serviceOperationTestAgent) ApplyFirewall(
	_ *transport.ApplyFirewallRequest,
	out *FirewallStatusResp,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.legacyFirewallCalls++
	out.Error = "legacy firewall RPC must not be called"
	return nil
}

func (a *serviceOperationTestAgent) ConfigurePowerDNSSQLite(_ *struct{}, resp *ServiceOperationDNSResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dnsError != "" {
		resp.Error = a.dnsError
		return nil
	}
	resp.Synced = true
	return nil
}

func (a *serviceOperationTestAgent) SyncDNSZoneV2(
	req *transport.SyncDNSZoneV2Request,
	resp *transport.SyncDNSZoneV2Response,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copy := *req
	copy.Records = append([]transport.ZoneRecord(nil), req.Records...)
	a.dnsV2Requests = append(a.dnsV2Requests, copy)
	a.mutationEvents = append(a.mutationEvents, "call:dns_zone_sync")
	if a.dnsError != "" {
		resp.Error = a.dnsError
		return nil
	}
	resp.Synced = true
	resp.AppliedGeneration = req.DesiredGeneration
	return nil
}

func (a *serviceOperationTestAgent) ServiceAction(
	req *transport.ServiceActionArgs,
	resp *transport.ServiceActionResult,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.serviceError != "" {
		resp.Error = a.serviceError
		return nil
	}
	resp.Success = a.serviceSuccess
	if resp.Success && (req.Action == "start" || req.Action == "restart") {
		a.active[req.ServiceName] = true
	}
	return nil
}

func (a *serviceOperationTestAgent) ServiceMutationAction(
	req *transport.ServiceActionArgs,
	resp *transport.ServiceActionResult,
) error {
	return a.ServiceAction(req, resp)
}

func (a *serviceOperationTestAgent) SetupVPN(
	_ *ServiceOperationVPNRequest,
	resp *ServiceOperationVPNResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vpnError != "" {
		resp.Error = a.vpnError
		return nil
	}
	resp.Created = a.vpnCreated
	return nil
}

func (a *serviceOperationTestAgent) SyncVPNPeersV2(
	req *ServiceOperationPeerRequest,
	resp *ServiceOperationPeerResponse,
) error {
	if a.peerStarted != nil {
		a.peerStartOnce.Do(func() { close(a.peerStarted) })
	}
	if a.releasePeer != nil {
		<-a.releasePeer
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationEvents = append(a.mutationEvents, "call:vpn_peer_sync")
	if a.peerError != "" {
		resp.Error = a.peerError
		return nil
	}
	resp.Applied = true
	resp.AppliedGeneration = req.DesiredGeneration
	return nil
}

func (a *serviceOperationTestAgent) capturedMutationEvents() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.mutationEvents...)
}

func attachServiceOperationTestAgent(t *testing.T, p *Panel, agent *serviceOperationTestAgent) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register service operation agent: %v", err)
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
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

type serviceOperationTestFixture struct {
	panel    *Panel
	agent    *serviceOperationTestAgent
	database *paneldb.SQLiteDB
	userID   int
}

func newServiceOperationTestFixture(t *testing.T) serviceOperationTestFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open service operation database: %v", err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('operation-admin','x','operation@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	panel := &Panel{db: database}
	agent := newServiceOperationTestAgent()
	attachServiceOperationTestAgent(t, panel, agent)
	return serviceOperationTestFixture{panel: panel, agent: agent, database: database, userID: int(userID)}
}

func serviceOperationAdminRequest(t *testing.T, method, target, body string, userID int) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = "198.51.100.25:43120"
	req.Header.Set("User-Agent", "service-operation-test-agent")
	return req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: userID, Role: roleAdmin}))
}

func decodeServiceOperationEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) *serviceOperation {
	t.Helper()
	var envelope struct {
		Operation *serviceOperation `json:"operation"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode operation response %q: %v", recorder.Body.String(), err)
	}
	return envelope.Operation
}

func getServiceOperation(t *testing.T, p *Panel, userID int, id string) (*serviceOperation, string) {
	t.Helper()
	target := "/api/v1/service/operation"
	if id != "" {
		target += "?id=" + id
	}
	recorder := httptest.NewRecorder()
	p.handleServiceOperation(recorder, serviceOperationAdminRequest(t, http.MethodGet, target, "", userID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET operation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	return decodeServiceOperationEnvelope(t, recorder), recorder.Body.String()
}

func waitForServiceOperation(
	t *testing.T,
	p *Panel,
	userID int,
	id string,
	wantStatus string,
) (*serviceOperation, string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		op, body := getServiceOperation(t, p, userID, id)
		if op != nil && op.Status == wantStatus {
			return op, body
		}
		time.Sleep(10 * time.Millisecond)
	}
	op, body := getServiceOperation(t, p, userID, id)
	t.Fatalf("operation did not reach %s; last=%+v body=%s", wantStatus, op, body)
	return nil, ""
}
func mustServiceOperationRequestID(t *testing.T) string {
	t.Helper()
	id, err := newServiceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func serviceOperationBoundContext() context.Context {
	return withPanelMutationBinding(context.Background(), agentMutationBinding{
		MutationRequestID: "00112233445566778899aabbccddeeff",
		MutationOwnerID:   "ffeeddccbbaa99887766554433221100",
	})
}

func postServiceInstall(t *testing.T, f serviceOperationTestFixture, serviceID string) (*httptest.ResponseRecorder, *serviceOperation) {
	t.Helper()
	return postServiceInstallRequest(t, f, serviceInstallRequest{ServiceID: serviceID, RequestID: mustServiceOperationRequestID(t)})
}

func postServiceInstallRequest(
	t *testing.T,
	f serviceOperationTestFixture,
	request serviceInstallRequest,
) (*httptest.ResponseRecorder, *serviceOperation) {
	t.Helper()
	recorder := httptest.NewRecorder()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	f.panel.handleServiceInstall(
		recorder,
		serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/service/install", string(body), f.userID),
	)
	return recorder, decodeServiceOperationEnvelope(t, recorder)
}

func TestInstallPostsRequireCanonicalRequestIDBeforePersistenceOrRPC(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	const uppercaseID = "00112233445566778899AABBCCDDEEFF"
	tests := []struct {
		name      string
		target    string
		body      string
		handler   func(http.ResponseWriter, *http.Request)
		callCount func() int32
	}{
		{
			name:      "service blank",
			target:    "/api/v1/service/install",
			body:      `{"service_id":"certbot"}`,
			handler:   f.panel.handleServiceInstall,
			callCount: f.agent.installCalls.Load,
		},
		{
			name:      "service uppercase",
			target:    "/api/v1/service/install",
			body:      `{"service_id":"certbot","request_id":"` + uppercaseID + `"}`,
			handler:   f.panel.handleServiceInstall,
			callCount: f.agent.installCalls.Load,
		},
		{
			name:      "runtime blank",
			target:    "/api/v1/runtimes/node",
			body:      `{"version":"22.4.1"}`,
			handler:   f.panel.handleNodeRuntimes,
			callCount: f.agent.nodeCalls.Load,
		},
		{
			name:      "runtime uppercase",
			target:    "/api/v1/runtimes/node",
			body:      `{"version":"22.4.1","request_id":"` + uppercaseID + `"}`,
			handler:   f.panel.handleNodeRuntimes,
			callCount: f.agent.nodeCalls.Load,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeCalls := tt.callCount()
			var beforeRows int
			if err := f.database.GetDB().QueryRow(`SELECT COUNT(*) FROM service_operations`).Scan(&beforeRows); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			tt.handler(
				recorder,
				serviceOperationAdminRequest(t, http.MethodPost, tt.target, tt.body, f.userID),
			)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if got := tt.callCount(); got != beforeCalls {
				t.Fatalf("RPC calls=%d want %d", got, beforeCalls)
			}
			var afterRows int
			if err := f.database.GetDB().QueryRow(`SELECT COUNT(*) FROM service_operations`).Scan(&afterRows); err != nil {
				t.Fatal(err)
			}
			if afterRows != beforeRows {
				t.Fatalf("operation rows=%d want %d", afterRows, beforeRows)
			}
		})
	}
}

func TestServiceInstallRequestIDIsExactAndIdempotent(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	f.agent.installStarted = started
	f.agent.releaseInstall = release
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	const requestID = "00112233445566778899aabbccddeeff"
	request := serviceInstallRequest{ServiceID: "certbot", RequestID: requestID}
	firstRecorder, first := postServiceInstallRequest(t, f, request)
	if firstRecorder.Code != http.StatusAccepted || first == nil {
		t.Fatalf("first request status=%d operation=%+v body=%s", firstRecorder.Code, first, firstRecorder.Body.String())
	}
	if first.RequestID != requestID {
		t.Fatalf("first request_id=%q want %q", first.RequestID, requestID)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background install did not start")
	}

	// Replaying the same immutable request returns the same durable operation
	// even while the package mutation lock is held; it never launches another.
	// Aynı değişmez isteği yeniden göndermek paket değişikliği kilitliyken bile
	// aynı kalıcı işlemi döndürür; ikinci bir işlem başlatmaz.
	replayRecorder, replay := postServiceInstallRequest(t, f, request)
	if replayRecorder.Code != http.StatusAccepted || replay == nil || replay.ID != first.ID {
		t.Fatalf("replay status=%d operation=%+v first=%+v body=%s", replayRecorder.Code, replay, first, replayRecorder.Body.String())
	}

	recoveryRecorder := httptest.NewRecorder()
	f.panel.handleServiceOperation(
		recoveryRecorder,
		serviceOperationAdminRequest(
			t,
			http.MethodGet,
			"/api/v1/service/operation?request_id="+requestID,
			"",
			f.userID,
		),
	)
	recovered := decodeServiceOperationEnvelope(t, recoveryRecorder)
	if recoveryRecorder.Code != http.StatusOK || recovered == nil || recovered.ID != first.ID {
		t.Fatalf("recover status=%d operation=%+v body=%s", recoveryRecorder.Code, recovered, recoveryRecorder.Body.String())
	}

	conflictRecorder, _ := postServiceInstallRequest(
		t,
		f,
		serviceInstallRequest{ServiceID: "nginx", RequestID: requestID},
	)
	if conflictRecorder.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflictRecorder.Code, conflictRecorder.Body.String())
	}
	var conflict apiErrorBody
	if err := json.Unmarshal(conflictRecorder.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Code != "service_operation_request_conflict" {
		t.Fatalf("conflict code=%q body=%s", conflict.Code, conflictRecorder.Body.String())
	}

	close(release)
	succeeded, _ := waitForServiceOperation(t, f.panel, f.userID, first.ID, serviceOperationSucceeded)
	if succeeded.RequestID != requestID {
		t.Fatalf("succeeded request_id=%q want %q", succeeded.RequestID, requestID)
	}
	terminalRecorder, terminalReplay := postServiceInstallRequest(t, f, request)
	if terminalRecorder.Code != http.StatusAccepted || terminalReplay == nil ||
		terminalReplay.ID != first.ID || terminalReplay.Status != serviceOperationSucceeded {
		t.Fatalf("terminal replay status=%d operation=%+v body=%s", terminalRecorder.Code, terminalReplay, terminalRecorder.Body.String())
	}

	var count int
	if err := f.database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM service_operations WHERE request_id=?`,
		requestID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("request_id row count=%d want 1", count)
	}
}

func assertBusyResponse(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("busy status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeServiceOperationBusy {
		t.Fatalf("busy code=%q body=%s", body.Code, recorder.Body.String())
	}
}

func TestServiceInstallOperationReturns202TransitionsAndGatesMutations(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	f.agent.installStarted = started
	f.agent.releaseInstall = release
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	recorder, queued := postServiceInstall(t, f, "certbot")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if queued == nil || queued.ID == "" || queued.Kind != serviceOperationKindInstall ||
		queued.ServiceID != "certbot" || queued.Status != serviceOperationQueued ||
		queued.Phase != "queued" || queued.StartedAt == "" {
		t.Fatalf("unexpected queued operation: %+v", queued)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background install did not start")
	}
	running, _ := waitForServiceOperation(t, f.panel, f.userID, queued.ID, serviceOperationRunning)
	if running.Phase != "installing" {
		t.Fatalf("running phase=%q", running.Phase)
	}

	second, _ := postServiceInstall(t, f, "redis")
	assertBusyResponse(t, second)

	mutations := []struct {
		name   string
		invoke func(*httptest.ResponseRecorder)
	}{
		{"uninstall", func(w *httptest.ResponseRecorder) {
			f.panel.handleServiceUninstall(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"redis"}`, f.userID))
		}},
		{"repo", func(w *httptest.ResponseRecorder) {
			f.panel.handleRepo(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/repo", `{"service_id":"postgresql","action":"enable"}`, f.userID))
		}},
		{"service action", func(w *httptest.ResponseRecorder) {
			f.panel.handleServiceAction(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/service/action", `{"service_name":"redis","action":"restart"}`, f.userID))
		}},
		{"node delete", func(w *httptest.ResponseRecorder) {
			f.panel.handleNodeRuntimeSub(w, serviceOperationAdminRequest(t, http.MethodDelete, "/api/v1/runtimes/node/20.1.0", "", f.userID))
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			mutation.invoke(w)
			assertBusyResponse(t, w)
		})
	}

	close(release)
	succeeded, _ := waitForServiceOperation(t, f.panel, f.userID, queued.ID, serviceOperationSucceeded)
	if succeeded.Phase != "completed" || succeeded.FinishedAt == "" || succeeded.Error != nil {
		t.Fatalf("unexpected succeeded operation: %+v", succeeded)
	}
	var result map[string]any
	if err := json.Unmarshal(succeeded.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["success"] != true || result["installed"] != true {
		t.Fatalf("success result=%v", result)
	}
	latest, _ := getServiceOperation(t, f.panel, f.userID, "")
	if latest == nil || latest.ID != queued.ID {
		t.Fatalf("latest operation=%+v want id %s", latest, queued.ID)
	}

	deadline := time.Now().Add(time.Second)
	for {
		var userID int
		var action, ip, userAgent string
		err := f.database.GetDB().QueryRow(`
			SELECT user_id, action, ip_address, user_agent
			FROM audit_logs WHERE action='service.install:certbot'
			ORDER BY id DESC LIMIT 1`).Scan(&userID, &action, &ip, &userAgent)
		if err == nil {
			if userID != f.userID || ip != "198.51.100.25" || userAgent != "service-operation-test-agent" {
				t.Fatalf("audit identity user=%d ip=%q ua=%q", userID, ip, userAgent)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation audit was not written: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServiceInstallOperationSanitizesAgentFailure(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	f.agent.installError = "SECRET apt output: /root/token\ncommand failed"
	recorder, queued := postServiceInstall(t, f, "certbot")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	failed, body := waitForServiceOperation(t, f.panel, f.userID, queued.ID, serviceOperationFailed)
	if strings.Contains(body, "SECRET") || strings.Contains(body, "/root/token") {
		t.Fatalf("raw agent output leaked: %s", body)
	}
	if failed.Error == nil || failed.Error.Code != "service_install_failed" ||
		failed.Error.Message != "The service could not be installed and verified." {
		t.Fatalf("unexpected safe failure: %+v", failed)
	}
	var result map[string]any
	if err := json.Unmarshal(failed.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["success"] != false || result["installed"] != false {
		t.Fatalf("failure result=%v", result)
	}
}

func TestServiceOperationStartFailureDoesNotLeaveQueuedLock(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	if _, err := f.database.GetDB().Exec(`
		CREATE TRIGGER reject_operation_running
		BEFORE UPDATE OF status ON service_operations
		WHEN NEW.status='running'
		BEGIN SELECT RAISE(ABORT, 'forced running transition failure'); END`); err != nil {
		t.Fatal(err)
	}
	recorder, queued := postServiceInstall(t, f, "certbot")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	failed, _ := waitForServiceOperation(t, f.panel, f.userID, queued.ID, serviceOperationFailed)
	if failed.Phase != "start_failed" || failed.Error == nil || failed.Error.Code != "operation_start_failed" {
		t.Fatalf("unexpected start fallback: %+v", failed)
	}
	if active, err := f.panel.activeServiceOperation(context.Background()); err != nil || active != nil {
		t.Fatalf("active lock remained after start failure: active=%+v err=%v", active, err)
	}
}

func TestNodeRuntimeInstallUsesDurable202ContractAndIdempotentVerification(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	seedInstalledServices(f.agent, "nginx")
	f.agent.nodeNoop = true
	f.agent.nodeVersions["22.4.1"] = true
	recorder := httptest.NewRecorder()
	f.panel.handleNodeRuntimes(
		recorder,
		serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/runtimes/node", `{"version":"22.4.1","request_id":"`+mustServiceOperationRequestID(t)+`"}`, f.userID),
	)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("node install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	queued := decodeServiceOperationEnvelope(t, recorder)
	if queued == nil || queued.Kind != serviceOperationKindRuntimeInstall || queued.ServiceID != "node" ||
		queued.PackageName != "22.4.1" {
		t.Fatalf("node operation=%+v", queued)
	}
	succeeded, _ := waitForServiceOperation(t, f.panel, f.userID, queued.ID, serviceOperationSucceeded)
	if succeeded.PackageName != "22.4.1" {
		t.Fatalf("persisted node operation target=%q", succeeded.PackageName)
	}
	var result map[string]any
	if err := json.Unmarshal(succeeded.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["success"] != true || result["installed"] != true || result["version"] != "22.4.1" {
		t.Fatalf("node result=%v", result)
	}
}

func TestNodeRuntimeInstallRequestIDReplaysExactly(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	seedInstalledServices(f.agent, "nginx")
	const requestID = "11223344556677889900aabbccddeeff"
	post := func(version string) (*httptest.ResponseRecorder, *serviceOperation) {
		recorder := httptest.NewRecorder()
		bodyBytes, err := json.Marshal(map[string]string{"version": version, "request_id": requestID})
		if err != nil {
			t.Fatal(err)
		}
		f.panel.handleNodeRuntimes(
			recorder,
			serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/runtimes/node", string(bodyBytes), f.userID),
		)
		return recorder, decodeServiceOperationEnvelope(t, recorder)
	}

	firstRecorder, first := post("22.4.1")
	if firstRecorder.Code != http.StatusAccepted || first == nil || first.RequestID != requestID {
		t.Fatalf("first node request status=%d operation=%+v body=%s", firstRecorder.Code, first, firstRecorder.Body.String())
	}
	waitForServiceOperation(t, f.panel, f.userID, first.ID, serviceOperationSucceeded)

	replayRecorder, replay := post("22.4.1")
	if replayRecorder.Code != http.StatusAccepted || replay == nil || replay.ID != first.ID {
		t.Fatalf("node replay status=%d operation=%+v first=%+v body=%s", replayRecorder.Code, replay, first, replayRecorder.Body.String())
	}

	conflictRecorder, _ := post("22.5.0")
	if conflictRecorder.Code != http.StatusConflict {
		t.Fatalf("node conflict status=%d body=%s", conflictRecorder.Code, conflictRecorder.Body.String())
	}
	var conflict apiErrorBody
	if err := json.Unmarshal(conflictRecorder.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Code != "service_operation_request_conflict" {
		t.Fatalf("node conflict code=%q body=%s", conflict.Code, conflictRecorder.Body.String())
	}

	var count int
	if err := f.database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM service_operations WHERE request_id=?`,
		requestID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("node request_id row count=%d want 1", count)
	}
}

func TestServiceInstallRequiresDaemonActiveButAllowsIdempotentRepair(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	f.agent.installNoop = true
	f.agent.installed["redis"] = true
	recorder, queued := postServiceInstall(t, f, "redis")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("redis install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	failed, _ := waitForServiceOperation(t, f.panel, f.userID, queued.ID, serviceOperationFailed)
	if failed.Phase != "scanning" {
		t.Fatalf("inactive daemon failed in phase %q", failed.Phase)
	}

	f.agent.mu.Lock()
	f.agent.active["redis"] = true
	f.agent.mu.Unlock()
	recorder, queued = postServiceInstall(t, f, "redis")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("redis repair status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if succeeded, _ := waitForServiceOperation(t, f.panel, f.userID, queued.ID, serviceOperationSucceeded); succeeded.Error != nil {
		t.Fatalf("idempotent daemon repair did not succeed: %+v", succeeded)
	}
}

func TestPostgreSQLVersionInstallRequiresExactClusterProof(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	f.agent.installNoop = true
	f.agent.installed["postgresql"] = true
	f.agent.active["postgresql@16-main"] = true
	f.agent.installUnit = "postgresql@16-main"

	result, failure := f.panel.runServiceInstall(
		serviceOperationBoundContext(),
		serviceInstallRequest{ServiceID: "postgresql", Package: "postgresql-17"},
		func(string) error { return nil },
	)
	if failure == nil {
		t.Fatalf("another running major produced success: result=%v", result)
	}
	if result["success"] != false {
		t.Fatalf("failed exact-unit proof result=%v", result)
	}

	f.agent.installUnit = ""
	result, failure = f.panel.runServiceInstall(
		serviceOperationBoundContext(),
		serviceInstallRequest{ServiceID: "postgresql", Package: "postgresql-17"},
		func(string) error { return nil },
	)
	if failure != nil || result["success"] != true {
		t.Fatalf("exact selected cluster was not accepted: result=%v failure=%+v", result, failure)
	}
	f.agent.mu.Lock()
	exactActive := f.agent.active["postgresql@17-main"]
	f.agent.mu.Unlock()
	if !exactActive {
		t.Fatal("selected postgresql@17-main was not started")
	}
}

func TestPostgreSQLVersionInstallFailsWhenExactClusterStopsBeforeFinalScan(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	f.agent.installNoop = true
	f.agent.installed["postgresql"] = true
	f.agent.active["postgresql@16-main"] = true
	f.agent.dropActiveBeforeGetServices = "postgresql@17-main"

	result, failure := f.panel.runServiceInstall(
		serviceOperationBoundContext(),
		serviceInstallRequest{ServiceID: "postgresql", Package: "postgresql-17"},
		func(string) error { return nil },
	)
	if failure == nil {
		t.Fatalf("old active major masked stopped selected cluster: result=%v", result)
	}
	if failure.Cause == nil || !strings.Contains(failure.Cause.Error(), "postgresql@17-main") {
		t.Fatalf("failure did not identify the selected cluster: %+v", failure)
	}
	if result["success"] != false {
		t.Fatalf("stopped exact cluster result=%v", result)
	}
}

func TestPowerDNSAndWireGuardIdempotentPostConfiguration(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	box, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	f.panel.secrets = box
	f.agent.installNoop = true
	f.agent.vpnCreated = false
	f.agent.installed["pdns"] = true
	f.agent.installed["wireguard"] = true
	f.agent.active["wireguard"] = true

	phases := []string{}
	result, failure := f.panel.runServiceInstall(serviceOperationBoundContext(), serviceInstallRequest{ServiceID: "pdns"}, func(phase string) error {
		phases = append(phases, phase)
		return nil
	})
	if failure != nil || result["success"] != true {
		t.Fatalf("PowerDNS idempotent setup result=%v failure=%+v", result, failure)
	}
	if strings.Join(phases, ",") != "configuring,starting,scanning" {
		t.Fatalf("PowerDNS phases=%v", phases)
	}

	phases = nil
	result, failure = f.panel.runServiceInstall(serviceOperationBoundContext(), serviceInstallRequest{ServiceID: "wireguard"}, func(phase string) error {
		phases = append(phases, phase)
		return nil
	})
	if failure != nil || result["success"] != true {
		t.Fatalf("WireGuard idempotent setup result=%v failure=%+v", result, failure)
	}
	if strings.Join(phases, ",") != "configuring,starting,scanning" {
		t.Fatalf("WireGuard phases=%v", phases)
	}
	for _, event := range f.agent.capturedMutationEvents() {
		if event == "call:vpn_peer_sync" {
			t.Fatal("runServiceInstall performed a nested VPN peer sync")
		}
	}
}

func TestPowerDNSInstallPublishesV2OnlyAfterOuterTerminal(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	setDNSIdentityForTest(t, f.panel, "standalone")
	seedStrictDNSZone(t, f.panel, "post-install.example")
	f.agent.installNoop = true
	f.agent.installed["pdns"] = true

	recorder, started := postServiceInstall(t, f, "pdns")
	if recorder.Code != http.StatusAccepted || started == nil {
		t.Fatalf("start status=%d operation=%+v body=%s", recorder.Code, started, recorder.Body.String())
	}
	terminal, _ := waitForServiceOperation(t, f.panel, f.userID, started.ID, serviceOperationSucceeded)
	if terminal.Phase != "completed" {
		t.Fatalf("terminal phase=%q", terminal.Phase)
	}

	f.agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV2Request(nil), f.agent.dnsV2Requests...)
	events := append([]string(nil), f.agent.mutationEvents...)
	f.agent.mu.Unlock()
	if len(requests) != 1 || requests[0].Domain != "post-install.example" ||
		requests[0].Delete || requests[0].DesiredGeneration <= 0 || len(requests[0].Records) == 0 {
		t.Fatalf("post-install V2 requests=%+v", requests)
	}
	outerFinish, dnsBegin := -1, -1
	for index, event := range events {
		if event == "finish:service_install:succeeded" {
			outerFinish = index
		}
		if event == "begin:dns_zone_sync" && dnsBegin == -1 {
			dnsBegin = index
		}
	}
	if outerFinish < 0 || dnsBegin <= outerFinish {
		t.Fatalf("mutation event order=%v, want outer terminal before DNS child", events)
	}
}

func TestPowerDNSInstallCapabilityGateCreatesNoRowAndTouchesNoHost(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	legacy := []string{
		transport.AgentCapabilityFirewallApplyV2,
		transport.AgentCapabilityPanelCertificateIssueV2,
	}
	f.agent.versionCapabilities = &legacy

	recorder, operation := postServiceInstall(t, f, "pdns")
	if recorder.Code != http.StatusInternalServerError || operation != nil {
		t.Fatalf("legacy pdns response=%d operation=%+v body=%s", recorder.Code, operation, recorder.Body.String())
	}
	var rows int
	if err := f.database.GetDB().QueryRow(`SELECT COUNT(*) FROM service_operations`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 || f.agent.installCalls.Load() != 0 {
		t.Fatalf("legacy DNS agent created rows=%d install calls=%d", rows, f.agent.installCalls.Load())
	}
}

func prepareWireGuardOperationFixture(
	t *testing.T,
	fixture serviceOperationTestFixture,
) {
	t.Helper()
	box, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "vpn-secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.panel.secrets = box
	fixture.agent.mu.Lock()
	fixture.agent.active["wireguard"] = true
	fixture.agent.mu.Unlock()
}

func mutationEventIndex(events []string, expected string) int {
	for index, event := range events {
		if event == expected {
			return index
		}
	}
	return -1
}

func TestServiceInstallUsesExactPostTerminalFirewallChildAndKeepsProcessLock(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	fixture.agent.mu.Lock()
	fixture.agent.firewallEnabled = true
	fixture.agent.firewallStarted = started
	fixture.agent.releaseFirewall = release
	fixture.agent.mu.Unlock()

	recorder, queued := postServiceInstall(t, fixture, "certbot")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("post-terminal firewall child did not start")
	}
	if fixture.panel.serviceMutationMu.TryLock() {
		fixture.panel.serviceMutationMu.Unlock()
		t.Fatal("process mutation lock was released during firewall child")
	}

	running, err := fixture.panel.serviceOperationByID(context.Background(), queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	child, ok := parseFirewallChildPhase(running.Phase)
	if !ok {
		t.Fatalf("running firewall child phase = %q", running.Phase)
	}
	fixture.agent.mu.Lock()
	job := cloneServiceOperationMutationJob(fixture.agent.mutationJobs[child.RequestID])
	fixture.agent.mu.Unlock()
	if job == nil || job.OwnerID != child.OwnerID ||
		job.Kind != "firewall_sync" || job.Target != "nftables" ||
		job.PackageName != child.Qualifier || job.Status != agentMutationRunning {
		t.Fatalf("persisted child=%+v agent job=%+v", child, job)
	}

	close(release)
	completed, _ := waitForServiceOperation(
		t, fixture.panel, fixture.userID, queued.ID, serviceOperationSucceeded,
	)
	if completed.Error != nil {
		t.Fatalf("completed install = %+v", completed)
	}
	events := fixture.agent.capturedMutationEvents()
	outerFinish := mutationEventIndex(events, "finish:service_install:succeeded")
	childBegin := mutationEventIndex(events, "begin:firewall_sync")
	childCall := mutationEventIndex(events, "call:firewall_sync")
	childFinish := mutationEventIndex(events, "finish:firewall_sync:succeeded")
	if outerFinish < 0 || childBegin <= outerFinish || childCall <= childBegin ||
		childFinish <= childCall {
		t.Fatalf("mutation events=%v", events)
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.firewallCalls != 1 || fixture.agent.legacyFirewallCalls != 0 ||
		len(fixture.agent.firewallRequests) != 1 {
		t.Fatalf(
			"firewall V2/V1/requests = %d/%d/%d",
			fixture.agent.firewallCalls,
			fixture.agent.legacyFirewallCalls,
			len(fixture.agent.firewallRequests),
		)
	}
	request := fixture.agent.firewallRequests[0]
	if request.MutationRequestID != child.RequestID ||
		request.MutationOwnerID != child.OwnerID {
		t.Fatalf("firewall request binding=%+v child=%+v", request.ServiceMutationBinding, child)
	}
}

func TestWireGuardInstallUsesSequentialOuterAndPayloadBoundPeerLeases(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	recorder, queued := postServiceInstall(t, fixture, "wireguard")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	completed, _ := waitForServiceOperation(
		t,
		fixture.panel,
		fixture.userID,
		queued.ID,
		serviceOperationSucceeded,
	)
	if completed.Error != nil {
		t.Fatalf("WireGuard install failed: %+v", completed)
	}

	events := fixture.agent.capturedMutationEvents()
	outerFinish := mutationEventIndex(events, "finish:service_install:succeeded")
	peerBegin := mutationEventIndex(events, "begin:vpn_peer_sync")
	peerCall := mutationEventIndex(events, "call:vpn_peer_sync")
	peerFinish := mutationEventIndex(events, "finish:vpn_peer_sync:succeeded")
	if outerFinish < 0 || peerBegin <= outerFinish || peerCall <= peerBegin ||
		peerFinish <= peerCall {
		t.Fatalf("mutation events=%v", events)
	}

	fixture.agent.mu.Lock()
	jobs := make([]*ServiceOperationMutationJob, 0, len(fixture.agent.mutationJobs))
	for _, job := range fixture.agent.mutationJobs {
		jobs = append(jobs, cloneServiceOperationMutationJob(job))
	}
	fixture.agent.mu.Unlock()
	var outerJob, peerJob *ServiceOperationMutationJob
	for _, job := range jobs {
		switch job.Kind {
		case serviceOperationKindInstall:
			outerJob = job
		case "vpn_peer_sync":
			peerJob = job
		}
	}
	if outerJob == nil || peerJob == nil || outerJob.RequestID == peerJob.RequestID ||
		outerJob.Status != agentMutationSucceeded ||
		peerJob.Status != agentMutationSucceeded ||
		peerJob.Target != "wireguard" ||
		!mutationpayload.ValidVPNPeerSyncQualifier(peerJob.PackageName) {
		t.Fatalf("outer job=%+v peer job=%+v", outerJob, peerJob)
	}
}

func TestWireGuardOuterFinishResponseLossReconcilesBeforePeerLease(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	fixture.agent.finishLossKind = serviceOperationKindInstall
	recorder, queued := postServiceInstall(t, fixture, "wireguard")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	completed, _ := waitForServiceOperation(
		t,
		fixture.panel,
		fixture.userID,
		queued.ID,
		serviceOperationSucceeded,
	)
	if completed.Error != nil {
		t.Fatalf("response-loss install failed: %+v", completed)
	}
	events := fixture.agent.capturedMutationEvents()
	outerFinish := mutationEventIndex(events, "finish:service_install:succeeded")
	outerStatus := mutationEventIndex(events, "status:service_install:succeeded")
	peerBegin := mutationEventIndex(events, "begin:vpn_peer_sync")
	peerFinish := mutationEventIndex(events, "finish:vpn_peer_sync:succeeded")
	if outerFinish < 0 || outerStatus <= outerFinish || peerBegin <= outerStatus ||
		peerFinish <= peerBegin {
		t.Fatalf("response-loss mutation events=%v", events)
	}
	if !fixture.agent.finishLossUsed {
		t.Fatal("test did not lose the outer Finish response")
	}
}

func TestWireGuardPostInstallPeerSyncKeepsProcessLockUntilPanelTerminal(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	started := make(chan struct{})
	release := make(chan struct{})
	fixture.agent.peerStarted = started
	fixture.agent.releasePeer = release
	recorder, queued := postServiceInstall(t, fixture, "wireguard")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("post-install peer sync did not start")
	}
	if fixture.panel.serviceMutationMu.TryLock() {
		fixture.panel.serviceMutationMu.Unlock()
		t.Fatal("process mutation lock was released during post-install peer sync")
	}
	running, err := fixture.panel.serviceOperationByID(context.Background(), queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != serviceOperationRunning || running.Phase != "syncing" {
		t.Fatalf("operation during peer sync=%+v", running)
	}
	fixture.agent.mu.Lock()
	outer := fixture.agent.mutationJobs[queued.RequestID]
	direct := fixture.agent.mutationJobs[fixture.agent.mutationActive]
	outerStatus := ""
	if outer != nil {
		outerStatus = outer.Status
	}
	directKind := ""
	if direct != nil {
		directKind = direct.Kind
	}
	fixture.agent.mu.Unlock()
	if outerStatus != agentMutationSucceeded || directKind != "vpn_peer_sync" {
		t.Fatalf("outer status=%q active direct kind=%q", outerStatus, directKind)
	}
	close(release)
	waitForServiceOperation(
		t,
		fixture.panel,
		fixture.userID,
		queued.ID,
		serviceOperationSucceeded,
	)
	if !fixture.panel.serviceMutationMu.TryLock() {
		t.Fatal("process mutation lock remained held after panel terminal success")
	}
	fixture.panel.serviceMutationMu.Unlock()
}

func TestWireGuardPostInstallPeerSyncFailureFailsPanelOperation(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	fixture.agent.peerError = "simulated post-install peer failure"
	recorder, queued := postServiceInstall(t, fixture, "wireguard")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	failed, _ := waitForServiceOperation(
		t,
		fixture.panel,
		fixture.userID,
		queued.ID,
		serviceOperationFailed,
	)
	if failed.Phase != "syncing" || failed.Error == nil ||
		failed.Error.Code != "service_install_failed" {
		t.Fatalf("failed operation=%+v", failed)
	}
	var result serviceOperationResult
	if err := json.Unmarshal(failed.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["success"] != false {
		t.Fatalf("failed result=%v", result)
	}
	events := fixture.agent.capturedMutationEvents()
	outerFinish := mutationEventIndex(events, "finish:service_install:succeeded")
	peerBegin := mutationEventIndex(events, "begin:vpn_peer_sync")
	peerFinish := mutationEventIndex(events, "finish:vpn_peer_sync:failed")
	if outerFinish < 0 || peerBegin <= outerFinish || peerFinish <= peerBegin {
		t.Fatalf("mutation events=%v", events)
	}
	if !fixture.panel.serviceMutationMu.TryLock() {
		t.Fatal("process mutation lock was released before panel failure became terminal")
	}
	fixture.panel.serviceMutationMu.Unlock()
}

func seedSucceededWireGuardOuterOperation(
	t *testing.T,
	fixture serviceOperationTestFixture,
) serviceOperation {
	t.Helper()
	op, err := fixture.panel.createServiceOperation(
		context.Background(),
		serviceOperationKindInstall,
		"wireguard",
		"",
		serviceOperationActor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.panel.markServiceOperationRunning(
		context.Background(),
		op.ID,
		"installing",
	); err != nil {
		t.Fatal(err)
	}
	fixture.agent.mu.Lock()
	fixture.agent.mutationJobs[op.RequestID] = &ServiceOperationMutationJob{
		RequestID: op.RequestID,
		OwnerID:   strings.Repeat("7", 32),
		Kind:      op.Kind,
		Target:    op.ServiceID,
		Status:    agentMutationSucceeded,
		Phase:     "completed",
		Attempt:   1,
	}
	fixture.agent.mu.Unlock()
	return op
}

func seedSucceededFirewallOuterOperation(
	t *testing.T,
	fixture serviceOperationTestFixture,
) serviceOperation {
	t.Helper()
	op, err := fixture.panel.createServiceOperation(
		context.Background(),
		serviceOperationKindInstall,
		"nginx",
		"",
		serviceOperationActor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.panel.markServiceOperationRunning(
		context.Background(), op.ID, "firewall",
	); err != nil {
		t.Fatal(err)
	}
	fixture.agent.mu.Lock()
	fixture.agent.installed["nginx"] = true
	fixture.agent.firewallEnabled = true
	fixture.agent.mutationJobs[op.RequestID] = &ServiceOperationMutationJob{
		RequestID: op.RequestID,
		OwnerID:   strings.Repeat("7", 32),
		Kind:      op.Kind,
		Target:    op.ServiceID,
		Status:    agentMutationSucceeded,
		Phase:     "completed",
		Attempt:   1,
	}
	fixture.agent.mu.Unlock()
	return op
}

func seedActiveFirewallChild(
	t *testing.T,
	fixture serviceOperationTestFixture,
	op serviceOperation,
) firewallChildIdentity {
	t.Helper()
	commitment, err := mutationpayload.CanonicalFirewallApply(
		true, false, []int{80, 2083}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	identity := firewallChildIdentity{
		RequestID: strings.Repeat("8", 32),
		OwnerID:   strings.Repeat("9", 32),
		Qualifier: commitment.Qualifier,
	}
	phase, err := encodeFirewallChildPhase(identity)
	if err != nil {
		t.Fatal(err)
	}
	if op.ID != "" {
		if err := fixture.panel.updateServiceOperationPhase(
			context.Background(), op.ID, phase,
		); err != nil {
			t.Fatal(err)
		}
	}
	fixture.agent.mu.Lock()
	fixture.agent.mutationJobs[identity.RequestID] = &ServiceOperationMutationJob{
		RequestID:      identity.RequestID,
		OwnerID:        identity.OwnerID,
		Kind:           "firewall_sync",
		Target:         "nftables",
		PackageName:    identity.Qualifier,
		Status:         agentMutationRunning,
		Phase:          "starting",
		Attempt:        1,
		LeaseExpiresAt: time.Now().UTC().Add(time.Minute),
		DeadlineAt:     time.Now().UTC().Add(time.Hour),
	}
	fixture.agent.mutationActive = identity.RequestID
	fixture.agent.mu.Unlock()
	return identity
}

func TestStartupRecoveryWaitsForActivationStartedAfterInitialStatusBeforeFirewallChild(
	t *testing.T,
) {
	fixture := newServiceOperationTestFixture(t)
	op := seedSucceededFirewallOuterOperation(t, fixture)
	started := make(chan struct{})
	release := make(chan struct{})
	activation := &ServiceOperationMutationJob{
		RequestID: strings.Repeat("a", 32),
		OwnerID:   strings.Repeat("b", 32),
		Kind:      "panel-certificate-activation",
		Target:    "renewing-panel.example.test",
		Status:    agentMutationRunning,
		Phase:     "panel-certificate-activation",
	}
	fixture.agent.mu.Lock()
	fixture.agent.activateAfterGlobalStatus = activation
	fixture.agent.activationStarted = started
	fixture.agent.releaseActivation = release
	fixture.agent.mu.Unlock()

	type recoveryResult struct {
		recovered int64
		err       error
	}
	result := make(chan recoveryResult, 1)
	go func() {
		recovered, err := fixture.panel.recoverInterruptedServiceOperations(
			context.Background(),
		)
		result <- recoveryResult{recovered: recovered, err: err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("activation was not injected after the first global status")
	}
	select {
	case got := <-result:
		t.Fatalf("recovery returned before activation terminalized: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
	fixture.agent.mu.Lock()
	statusBeforeRelease := fixture.agent.mutationJobs[activation.RequestID].Status
	fixture.agent.mu.Unlock()
	if statusBeforeRelease != agentMutationRunning {
		t.Fatalf("activation status before listener release=%q", statusBeforeRelease)
	}
	close(release)

	var got recoveryResult
	select {
	case got = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not finish after activation terminalized")
	}
	if got.err != nil || got.recovered != 1 {
		t.Fatalf("recovered=%d err=%v", got.recovered, got.err)
	}
	loaded, err := fixture.panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationSucceeded {
		t.Fatalf("recovered operation=%+v", loaded)
	}
	fixture.agent.mu.Lock()
	activationAfter := cloneServiceOperationMutationJob(
		fixture.agent.mutationJobs[activation.RequestID],
	)
	firewallCalls := fixture.agent.firewallCalls
	fixture.agent.mu.Unlock()
	if activationAfter == nil || activationAfter.Status != agentMutationSucceeded {
		t.Fatalf("activation was cancelled or lost: %+v", activationAfter)
	}
	if firewallCalls != 1 {
		t.Fatalf("fresh firewall calls=%d, want 1", firewallCalls)
	}
	events := fixture.agent.capturedMutationEvents()
	activationDone := mutationEventIndex(events, "activation:succeeded")
	firewallBegin := mutationEventIndex(events, "begin:firewall_sync")
	if activationDone < 0 || firewallBegin <= activationDone {
		t.Fatalf("startup recovery events=%v", events)
	}
}

func TestStartupRecoveryTerminalizesExactFirewallChildThenReplaysDesiredWork(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	op := seedSucceededFirewallOuterOperation(t, fixture)
	interrupted := seedActiveFirewallChild(t, fixture, op)

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	loaded, err := fixture.panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationSucceeded || loaded.Phase != "completed" {
		t.Fatalf("recovered operation=%+v", loaded)
	}
	fixture.agent.mu.Lock()
	interruptedJob := cloneServiceOperationMutationJob(
		fixture.agent.mutationJobs[interrupted.RequestID],
	)
	firewallCalls := fixture.agent.firewallCalls
	legacyCalls := fixture.agent.legacyFirewallCalls
	fixture.agent.mu.Unlock()
	if interruptedJob == nil || interruptedJob.Status != agentMutationFailed {
		t.Fatalf("interrupted child=%+v", interruptedJob)
	}
	if firewallCalls != 1 || legacyCalls != 0 {
		t.Fatalf("fresh V2/V1 calls=%d/%d", firewallCalls, legacyCalls)
	}
	events := fixture.agent.capturedMutationEvents()
	cancelled := mutationEventIndex(events, "cancel:firewall_sync")
	freshBegin := mutationEventIndex(events, "begin:firewall_sync")
	freshCall := mutationEventIndex(events, "call:firewall_sync")
	if cancelled < 0 || freshBegin <= cancelled || freshCall <= freshBegin {
		t.Fatalf("recovery events=%v", events)
	}
}

func TestStartupRecoveryOrphanFirewallChildDoesNotFabricatePayload(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	fixture.agent.mu.Lock()
	fixture.agent.firewallEnabled = true
	fixture.agent.mu.Unlock()
	interrupted := seedActiveFirewallChild(t, fixture, serviceOperation{})

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(context.Background())
	if err != nil || recovered != 0 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	fixture.agent.mu.Lock()
	job := cloneServiceOperationMutationJob(fixture.agent.mutationJobs[interrupted.RequestID])
	firewallCalls := fixture.agent.firewallCalls
	legacyCalls := fixture.agent.legacyFirewallCalls
	fixture.agent.mu.Unlock()
	if job == nil || job.Status != agentMutationFailed {
		t.Fatalf("orphan child=%+v", job)
	}
	if firewallCalls != 0 || legacyCalls != 0 {
		t.Fatalf("orphan recovery fabricated V2/V1 payload: %d/%d", firewallCalls, legacyCalls)
	}
}

func TestStartupRecoveryRejectsFirewallChildNotMatchingPersistedIdentity(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	op := seedSucceededFirewallOuterOperation(t, fixture)
	seedActiveFirewallChild(t, fixture, op)
	if _, err := fixture.database.GetDB().Exec(
		"UPDATE service_operations SET phase=? WHERE id=?",
		firewallChildPhasePrefix+strings.Repeat("a", 32)+"|"+
			strings.Repeat("b", 32)+"|"+
			mustFirewallQualifier(t),
		op.ID,
	); err != nil {
		t.Fatal(err)
	}

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(context.Background())
	if err == nil || recovered != 0 || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.firewallCalls != 0 {
		t.Fatalf("identity mismatch applied firewall %d times", fixture.agent.firewallCalls)
	}
}

func mustFirewallQualifier(t *testing.T) string {
	t.Helper()
	commitment, err := mutationpayload.CanonicalFirewallApply(true, false, []int{2083}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return commitment.Qualifier
}

func vpnRecoveryQualifier(t *testing.T, fixture serviceOperationTestFixture) string {
	t.Helper()
	var generation int64
	if err := fixture.database.GetDB().QueryRow(
		"SELECT desired_generation FROM vpn_sync_state WHERE id = 1",
	).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, nil)
	if err != nil {
		t.Fatal(err)
	}
	return commitment.Qualifier
}

func seedActiveDirectVPNRecoveryJob(
	t *testing.T,
	fixture serviceOperationTestFixture,
	qualifier string,
) *ServiceOperationMutationJob {
	t.Helper()
	job := &ServiceOperationMutationJob{
		RequestID:      strings.Repeat("8", 32),
		OwnerID:        strings.Repeat("9", 32),
		Kind:           "vpn_peer_sync",
		Target:         "wireguard",
		PackageName:    qualifier,
		Status:         agentMutationRunning,
		Phase:          "syncing",
		Attempt:        1,
		LeaseExpiresAt: time.Now().UTC().Add(time.Minute),
		DeadlineAt:     time.Now().UTC().Add(time.Hour),
	}
	fixture.agent.mu.Lock()
	fixture.agent.mutationJobs[job.RequestID] = job
	fixture.agent.mutationActive = job.RequestID
	fixture.agent.mu.Unlock()
	if _, err := fixture.database.GetDB().Exec(`
		UPDATE vpn_sync_state
		SET status = 'pending', lease_token = 'crashed-panel-token',
		    lease_expires_at = datetime('now', '+2 minutes')
		WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	return cloneServiceOperationMutationJob(job)
}

func assertRecoveredVPNStateApplied(
	t *testing.T,
	fixture serviceOperationTestFixture,
) {
	t.Helper()
	var status string
	var leaseToken *string
	var desiredGeneration, appliedGeneration int64
	if err := fixture.database.GetDB().QueryRow(`
		SELECT status, lease_token, desired_generation, applied_generation
		FROM vpn_sync_state WHERE id = 1`).Scan(
		&status,
		&leaseToken,
		&desiredGeneration,
		&appliedGeneration,
	); err != nil {
		t.Fatal(err)
	}
	if status != "applied" || leaseToken != nil ||
		appliedGeneration != desiredGeneration {
		t.Fatalf(
			"VPN state=%q lease=%v desired=%d applied=%d",
			status,
			leaseToken,
			desiredGeneration,
			appliedGeneration,
		)
	}
}

func TestValidDirectVPNPeerSyncRequiresExactRecoveryIdentity(t *testing.T) {
	valid := &agentMutationJob{
		RequestID:   strings.Repeat("a", 32),
		OwnerID:     strings.Repeat("b", 32),
		Kind:        "vpn_peer_sync",
		Target:      "wireguard",
		PackageName: "vpn-peer-sync/v1:sha256:" + strings.Repeat("c", 64),
		Status:      agentMutationRunning,
	}
	if !validDirectVPNPeerSync(valid) {
		t.Fatal("exact direct VPN recovery identity was rejected")
	}
	tests := []struct {
		name   string
		mutate func(*agentMutationJob)
	}{
		{"request", func(job *agentMutationJob) { job.RequestID = "short" }},
		{"owner", func(job *agentMutationJob) { job.OwnerID = "short" }},
		{"kind", func(job *agentMutationJob) { job.Kind = serviceOperationKindInstall }},
		{"target", func(job *agentMutationJob) { job.Target = "wg1" }},
		{"qualifier", func(job *agentMutationJob) { job.PackageName = "unbound" }},
		{"terminal", func(job *agentMutationJob) { job.Status = agentMutationSucceeded }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := cloneAgentMutationJob(valid)
			test.mutate(job)
			if validDirectVPNPeerSync(job) {
				t.Fatalf("invalid recovery identity accepted: %+v", job)
			}
		})
	}
}

func TestStartupRecoveryFreshSyncsSucceededWireGuardInstall(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	op := seedSucceededWireGuardOuterOperation(t, fixture)

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	loaded, err := fixture.panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationSucceeded || loaded.Phase != "completed" {
		t.Fatalf("recovered operation=%+v", loaded)
	}
	events := fixture.agent.capturedMutationEvents()
	if mutationEventIndex(events, "begin:vpn_peer_sync") < 0 ||
		mutationEventIndex(events, "call:vpn_peer_sync") < 0 ||
		mutationEventIndex(events, "finish:vpn_peer_sync:succeeded") < 0 {
		t.Fatalf("recovery events=%v", events)
	}
	assertRecoveredVPNStateApplied(t, fixture)
}

func TestStartupRecoveryFailsSucceededWireGuardInstallWhenFreshSyncFails(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	fixture.agent.peerError = "simulated recovery peer failure"
	op := seedSucceededWireGuardOuterOperation(t, fixture)

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	loaded, err := fixture.panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationFailed || loaded.Phase != "syncing" ||
		loaded.Error == nil || loaded.Error.Code != "service_install_failed" {
		t.Fatalf("recovered operation=%+v", loaded)
	}
}

func TestStartupRecoveryTerminalizesDirectVPNJobClearsLeaseAndFreshSyncs(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	op := seedSucceededWireGuardOuterOperation(t, fixture)
	interrupted := seedActiveDirectVPNRecoveryJob(
		t,
		fixture,
		vpnRecoveryQualifier(t, fixture),
	)

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	loaded, err := fixture.panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationSucceeded {
		t.Fatalf("recovered operation=%+v", loaded)
	}
	fixture.agent.mu.Lock()
	interruptedAfter := cloneServiceOperationMutationJob(
		fixture.agent.mutationJobs[interrupted.RequestID],
	)
	var freshPeerJobs int
	for requestID, job := range fixture.agent.mutationJobs {
		if requestID != interrupted.RequestID && job.Kind == "vpn_peer_sync" {
			freshPeerJobs++
		}
	}
	fixture.agent.mu.Unlock()
	if interruptedAfter == nil || interruptedAfter.Status != agentMutationFailed ||
		freshPeerJobs != 1 {
		t.Fatalf(
			"interrupted=%+v fresh peer jobs=%d",
			interruptedAfter,
			freshPeerJobs,
		)
	}
	events := fixture.agent.capturedMutationEvents()
	cancelled := mutationEventIndex(events, "cancel:vpn_peer_sync")
	freshBegin := mutationEventIndex(events, "begin:vpn_peer_sync")
	freshCall := mutationEventIndex(events, "call:vpn_peer_sync")
	if cancelled < 0 || freshBegin <= cancelled || freshCall <= freshBegin {
		t.Fatalf("recovery events=%v", events)
	}
	assertRecoveredVPNStateApplied(t, fixture)
}

func TestStartupRecoveryFreshSyncsDirectVPNJobWithoutPanelOperation(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	interrupted := seedActiveDirectVPNRecoveryJob(
		t,
		fixture,
		vpnRecoveryQualifier(t, fixture),
	)

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 0 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	fixture.agent.mu.Lock()
	interruptedAfter := cloneServiceOperationMutationJob(
		fixture.agent.mutationJobs[interrupted.RequestID],
	)
	var freshPeerJobs int
	for requestID, job := range fixture.agent.mutationJobs {
		if requestID != interrupted.RequestID && job.Kind == "vpn_peer_sync" {
			freshPeerJobs++
		}
	}
	fixture.agent.mu.Unlock()
	if interruptedAfter == nil || interruptedAfter.Status != agentMutationFailed ||
		freshPeerJobs != 1 {
		t.Fatalf(
			"interrupted=%+v fresh peer jobs=%d",
			interruptedAfter,
			freshPeerJobs,
		)
	}
	assertRecoveredVPNStateApplied(t, fixture)
}

func TestStartupRecoveryFreshSyncsOrphanedDBLeaseAfterAgentAlreadyTerminal(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	terminal := seedActiveDirectVPNRecoveryJob(
		t,
		fixture,
		vpnRecoveryQualifier(t, fixture),
	)
	fixture.agent.mu.Lock()
	fixture.agent.mutationJobs[terminal.RequestID].Status = agentMutationSucceeded
	fixture.agent.mutationJobs[terminal.RequestID].Phase =
		"commit/vpn-peer-sync/v1/published/" + terminal.RequestID + "/" + terminal.PackageName
	fixture.agent.mutationActive = ""
	fixture.agent.mu.Unlock()

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 0 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	fixture.agent.mu.Lock()
	var freshPeerJobs int
	for requestID, job := range fixture.agent.mutationJobs {
		if requestID != terminal.RequestID && job.Kind == "vpn_peer_sync" {
			freshPeerJobs++
		}
	}
	fixture.agent.mu.Unlock()
	if freshPeerJobs != 1 {
		t.Fatalf("fresh peer jobs=%d, want 1", freshPeerJobs)
	}
	assertRecoveredVPNStateApplied(t, fixture)
}

func TestStartupRecoveryDoesNotSyncWithoutPanelOperationOrStaleVPNLease(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 0 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	if events := fixture.agent.capturedMutationEvents(); len(events) != 0 {
		t.Fatalf("unexpected recovery mutations=%v", events)
	}
}

func TestStartupRecoveryRejectsUnqualifiedDirectVPNMismatch(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	seedSucceededWireGuardOuterOperation(t, fixture)
	interrupted := seedActiveDirectVPNRecoveryJob(
		t,
		fixture,
		"unbound-vpn-payload",
	)

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err == nil || recovered != 0 ||
		!strings.Contains(err.Error(), "does not match active panel operation") {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	fixture.agent.mu.Lock()
	status := fixture.agent.mutationJobs[interrupted.RequestID].Status
	fixture.agent.mu.Unlock()
	if status != agentMutationRunning {
		t.Fatalf("unqualified job status=%q, want running fail-closed", status)
	}
	var leaseToken *string
	if err := fixture.database.GetDB().QueryRow(
		"SELECT lease_token FROM vpn_sync_state WHERE id = 1",
	).Scan(&leaseToken); err != nil {
		t.Fatal(err)
	}
	if leaseToken == nil || *leaseToken != "crashed-panel-token" {
		t.Fatalf("unqualified job cleared DB lease: %v", leaseToken)
	}
}

func TestServiceOperationPersistsAndStartupRecoveryFailsInterruptedWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := paneldb.NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	panel := &Panel{db: database}
	op, err := panel.createServiceOperation(context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{})
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.markServiceOperationRunning(context.Background(), op.ID, "installing"); err != nil {
		t.Fatal(err)
	}
	if err := panel.finishServiceOperationSucceeded(context.Background(), op.ID, serviceOperationResult{"success": true, "installed": true}); err != nil {
		t.Fatal(err)
	}
	database.Close()

	database, err = paneldb.NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	panel = &Panel{db: database}
	loaded, err := panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationSucceeded || loaded.FinishedAt == "" {
		t.Fatalf("reloaded operation=%+v", loaded)
	}

	interrupted, err := panel.createServiceOperation(context.Background(), serviceOperationKindInstall, "redis", "", serviceOperationActor{})
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.markServiceOperationRunning(context.Background(), interrupted.ID, "scanning"); err != nil {
		t.Fatal(err)
	}
	database.Close()

	database, err = paneldb.NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	panel = &Panel{db: database}
	attachServiceOperationTestAgent(t, panel, newServiceOperationTestAgent())
	recovered, err := panel.recoverInterruptedServiceOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d want 1", recovered)
	}
	loaded, err = panel.serviceOperationByID(context.Background(), interrupted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationFailed || loaded.Phase != "interrupted" || loaded.Error == nil ||
		loaded.Error.Code != "panel_restarted_without_agent_ledger" {
		t.Fatalf("recovered operation=%+v", loaded)
	}
	if !bytes.Equal(loaded.Result, []byte(`{"success":false}`)) {
		t.Fatalf("recovery result=%s", loaded.Result)
	}
}

func TestStartupRecoveryCommitsAgentTerminalSuccess(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	op, err := f.panel.createServiceOperation(
		context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.panel.markServiceOperationRunning(context.Background(), op.ID, "installing"); err != nil {
		t.Fatal(err)
	}
	f.agent.mu.Lock()
	f.agent.mutationJobs[op.RequestID] = &ServiceOperationMutationJob{
		RequestID: op.RequestID,
		OwnerID:   "00112233445566778899aabbccddeeff",
		Kind:      op.Kind,
		Target:    op.ServiceID,
		Status:    agentMutationSucceeded,
		Phase:     "completed",
		Attempt:   1,
	}
	f.agent.mu.Unlock()

	recovered, err := f.panel.recoverInterruptedServiceOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d want 1", recovered)
	}
	loaded, err := f.panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationSucceeded || loaded.Phase != "completed" {
		t.Fatalf("recovered operation=%+v", loaded)
	}
	var result map[string]any
	if err := json.Unmarshal(loaded.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["success"] != true || result["recovered"] != true {
		t.Fatalf("recovered result=%v", result)
	}
}

func TestStartupRecoveryCancelsAndResumesMatchingActiveAgentMutation(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	op, err := f.panel.createServiceOperation(
		context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.panel.markServiceOperationRunning(context.Background(), op.ID, "installing"); err != nil {
		t.Fatal(err)
	}
	f.agent.mu.Lock()
	f.agent.mutationJobs[op.RequestID] = &ServiceOperationMutationJob{
		RequestID:      op.RequestID,
		OwnerID:        "00112233445566778899aabbccddeeff",
		Kind:           op.Kind,
		Target:         op.ServiceID,
		Status:         agentMutationRunning,
		Phase:          "installing",
		Attempt:        1,
		LeaseExpiresAt: time.Now().UTC().Add(time.Minute),
		DeadlineAt:     time.Now().UTC().Add(time.Hour),
	}
	f.agent.mutationActive = op.RequestID
	f.agent.mu.Unlock()

	recovered, err := f.panel.recoverInterruptedServiceOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 0 {
		t.Fatalf("recovered=%d want asynchronous resume", recovered)
	}
	loaded, _ := waitForServiceOperation(t, f.panel, f.userID, op.ID, serviceOperationSucceeded)
	if loaded.Phase != "completed" || f.agent.installCalls.Load() != 1 {
		t.Fatalf("resumed operation=%+v install calls=%d", loaded, f.agent.installCalls.Load())
	}
	f.agent.mu.Lock()
	job := cloneServiceOperationMutationJob(f.agent.mutationJobs[op.RequestID])
	f.agent.mu.Unlock()
	if job == nil || job.Status != agentMutationSucceeded || job.Attempt != 1 {
		t.Fatalf("agent terminal job=%+v", job)
	}
}

func TestWorkerDeadlineStillPersistsTerminalOperation(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	f.agent.mutationDeadline = time.Now().UTC().Add(250 * time.Millisecond)
	op, err := f.panel.createServiceOperation(
		context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	var releaseOnce sync.Once
	f.panel.launchServiceOperationWithAudit(
		op,
		serviceOperationActor{},
		"installing",
		"service.install:certbot",
		"service.install.failed:certbot",
		func() { releaseOnce.Do(func() { close(released) }) },
		func(ctx context.Context, _ func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			<-ctx.Done()
			return serviceOperationResult{"success": false}, operationFailure(
				"service_operation_deadline_test",
				"The package operation deadline expired.",
				ctx.Err(),
			)
		},
		func(context.Context, serviceOperationActor, string) {},
	)

	failed, _ := waitForServiceOperation(t, f.panel, f.userID, op.ID, serviceOperationFailed)
	if failed.Error == nil || failed.Error.Code != "service_operation_deadline_test" {
		t.Fatalf("deadline operation=%+v", failed)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("mutation lock was not released after terminal persistence")
	}
}

func TestActiveOperationQueryNeverFallsBackToLatestTerminalRow(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	terminal, err := f.panel.createServiceOperation(
		context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.panel.markServiceOperationRunning(context.Background(), terminal.ID, "installing"); err != nil {
		t.Fatal(err)
	}
	if err := f.panel.finishServiceOperationSucceeded(
		context.Background(), terminal.ID, serviceOperationResult{"success": true},
	); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	f.panel.handleServiceOperation(
		recorder,
		serviceOperationAdminRequest(t, http.MethodGet, "/api/v1/service/operation?active=1", "", f.userID),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("terminal-only active status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if op := decodeServiceOperationEnvelope(t, recorder); op != nil {
		t.Fatalf("active query fell back to terminal operation: %+v", op)
	}

	active, err := f.panel.createServiceOperation(
		context.Background(), serviceOperationKindInstall, "nginx", "", serviceOperationActor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	f.panel.handleServiceOperation(
		recorder,
		serviceOperationAdminRequest(t, http.MethodGet, "/api/v1/service/operation?active=1", "", f.userID),
	)
	if got := decodeServiceOperationEnvelope(t, recorder); recorder.Code != http.StatusOK || got == nil || got.ID != active.ID {
		t.Fatalf("active status=%d operation=%+v body=%s", recorder.Code, got, recorder.Body.String())
	}

	invalid := httptest.NewRecorder()
	f.panel.handleServiceOperation(
		invalid,
		serviceOperationAdminRequest(t, http.MethodGet, "/api/v1/service/operation?active=0", "", f.userID),
	)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("active=0 status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestServiceOperationRunnerPanicFailsRowAndReleasesBeforeAudit(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	op, err := f.panel.createServiceOperationRequest(
		context.Background(),
		serviceOperationKindInstall,
		"certbot",
		"",
		mustServiceOperationRequestID(t),
		serviceOperationActor{UserID: f.userID},
	)
	if err != nil {
		t.Fatal(err)
	}

	var releaseCount atomic.Int32
	released := make(chan struct{})
	auditStarted := make(chan struct{})
	allowAudit := make(chan struct{})
	release := func() {
		if releaseCount.Add(1) == 1 {
			close(released)
		}
	}
	audit := func(context.Context, serviceOperationActor, string) {
		close(auditStarted)
		<-allowAudit
	}
	f.panel.launchServiceOperationWithAudit(
		op,
		serviceOperationActor{UserID: f.userID},
		"installing",
		"service.install:certbot",
		"service.install.failed:certbot",
		release,
		func(context.Context, func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			panic("forced runner panic")
		},
		audit,
	)

	select {
	case <-auditStarted:
	case <-time.After(time.Second):
		t.Fatal("panic audit did not start")
	}
	select {
	case <-released:
	default:
		t.Fatal("mutation lock was still held when audit started")
	}
	failed, _ := waitForServiceOperation(t, f.panel, f.userID, op.ID, serviceOperationFailed)
	if failed.Error == nil || failed.Error.Code != errCodeServiceOperationRunnerPanicked {
		t.Fatalf("panic operation=%+v", failed)
	}
	if releaseCount.Load() != 1 {
		t.Fatalf("release count=%d want 1", releaseCount.Load())
	}
	close(allowAudit)
	time.Sleep(20 * time.Millisecond)
	if releaseCount.Load() != 1 {
		t.Fatalf("release count after audit=%d want 1", releaseCount.Load())
	}
}

func TestServiceOperationRunnerFailureResultIsDurable(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	result := serviceOperationResult{"success": false, "installed": true}
	op, err := f.panel.createServiceOperation(context.Background(), serviceOperationKindInstall, "nginx", "", serviceOperationActor{UserID: f.userID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.panel.markServiceOperationRunning(context.Background(), op.ID, "configuring"); err != nil {
		t.Fatal(err)
	}
	failure := serviceInstallFailure(errors.New("raw command output must stay server-side"))
	if err := f.panel.finishServiceOperationFailed(context.Background(), op.ID, "configuring", result, failure); err != nil {
		t.Fatal(err)
	}
	loaded, err := f.panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationFailed || loaded.Error == nil || loaded.Error.Code != "service_install_failed" {
		t.Fatalf("durable failed operation=%+v", loaded)
	}
	if bytes.Contains(loaded.Result, []byte("raw command")) {
		t.Fatalf("raw failure leaked into result: %s", loaded.Result)
	}
}
