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
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

type ServiceOperationInstallRequest struct {
	ID      string
	Package string
}

type ServiceOperationInstallResponse struct {
	Installed bool
	Detail    string
	Error     string
}

type ServiceOperationNodeRequest struct {
	Version string
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
	Peers []ServiceOperationPeerSpec
}

type ServiceOperationPeerResponse struct {
	Applied bool
	Error   string
}

type serviceOperationTestAgent struct {
	mu sync.Mutex

	installStarted chan struct{}
	releaseInstall <-chan struct{}
	startOnce      sync.Once

	installRPCError error
	installError    string
	installNoop     bool
	nodeError       string
	nodeNoop        bool
	dnsError        string
	serviceError    string
	serviceSuccess  bool
	vpnError        string
	vpnCreated      bool
	peerError       string

	installed    map[string]bool
	active       map[string]bool
	nodeVersions map[string]bool
}

func newServiceOperationTestAgent() *serviceOperationTestAgent {
	return &serviceOperationTestAgent{
		serviceSuccess: true,
		vpnCreated:     true,
		installed:      map[string]bool{},
		active:         map[string]bool{},
		nodeVersions:   map[string]bool{},
	}
}

func (a *serviceOperationTestAgent) InstallService(
	req *ServiceOperationInstallRequest,
	resp *ServiceOperationInstallResponse,
) error {
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
	return nil
}

func (a *serviceOperationTestAgent) InstallNodeVersion(
	req *ServiceOperationNodeRequest,
	resp *ServiceOperationNodeResponse,
) error {
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
	services := make([]core.Service, 0, len(a.active))
	for id, active := range a.active {
		if !active {
			continue
		}
		managed := core.GetManagedServiceByID(id)
		if managed == nil || len(managed.SystemNames) == 0 {
			continue
		}
		services = append(services, core.Service{Name: managed.SystemNames[0], Status: "active (running)"})
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

func (a *serviceOperationTestAgent) FirewallStatus(_ *struct{}, out *FirewallStatusResp) error {
	out.Enabled = false
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

func (a *serviceOperationTestAgent) SyncVPNPeers(
	_ *ServiceOperationPeerRequest,
	resp *ServiceOperationPeerResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.peerError != "" {
		resp.Error = a.peerError
		return nil
	}
	resp.Applied = true
	return nil
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

func postServiceInstall(t *testing.T, f serviceOperationTestFixture, serviceID string) (*httptest.ResponseRecorder, *serviceOperation) {
	t.Helper()
	recorder := httptest.NewRecorder()
	body, err := json.Marshal(serviceInstallRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatal(err)
	}
	f.panel.handleServiceInstall(
		recorder,
		serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/service/install", string(body), f.userID),
	)
	return recorder, decodeServiceOperationEnvelope(t, recorder)
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
		serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/runtimes/node", `{"version":"22.4.1"}`, f.userID),
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

func TestPowerDNSAndWireGuardIdempotentPostConfiguration(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	f.agent.installNoop = true
	f.agent.vpnCreated = false
	f.agent.installed["pdns"] = true
	f.agent.installed["wireguard"] = true
	f.agent.active["wireguard"] = true

	phases := []string{}
	result, failure := f.panel.runServiceInstall(context.Background(), serviceInstallRequest{ServiceID: "pdns"}, func(phase string) error {
		phases = append(phases, phase)
		return nil
	})
	if failure != nil || result["success"] != true {
		t.Fatalf("PowerDNS idempotent setup result=%v failure=%+v", result, failure)
	}
	if strings.Join(phases, ",") != "configuring,starting,syncing,scanning,firewall" {
		t.Fatalf("PowerDNS phases=%v", phases)
	}

	phases = nil
	result, failure = f.panel.runServiceInstall(context.Background(), serviceInstallRequest{ServiceID: "wireguard"}, func(phase string) error {
		phases = append(phases, phase)
		return nil
	})
	if failure != nil || result["success"] != true {
		t.Fatalf("WireGuard idempotent setup result=%v failure=%+v", result, failure)
	}
	if strings.Join(phases, ",") != "configuring,starting,syncing,scanning,firewall" {
		t.Fatalf("WireGuard phases=%v", phases)
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
		loaded.Error.Code != "panel_restarted_before_verification" {
		t.Fatalf("recovered operation=%+v", loaded)
	}
	if !bytes.Equal(loaded.Result, []byte(`{"success":false}`)) {
		t.Fatalf("recovery result=%s", loaded.Result)
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
