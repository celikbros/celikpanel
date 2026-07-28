package main

import (
	"context"
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

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

type FirewallSyncApplyRequest struct {
	MutationRequestID string
	MutationOwnerID   string
	Enabled           bool
	TCPPorts          []int
	UDPPorts          []int
	Persist           bool
}

type firewallSyncTestAgent struct {
	durableMutationRPCFixture

	mu sync.Mutex

	status             FirewallStatusResp
	statusErr          error
	installed          []string
	installedErr       error
	applyResponseError string
	applyErr           error
	applyCalls         int
	applyRequests      []FirewallSyncApplyRequest
	installedCalls     int
	discoveryStarted   chan struct{}
	releaseDiscovery   chan struct{}
	discoveryOnce      sync.Once
}

func (a *firewallSyncTestAgent) FirewallStatus(_ *struct{}, out *FirewallStatusResp) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*out = a.status
	return a.statusErr
}

func (a *firewallSyncTestAgent) InstalledServiceIDsStrict(_ *transport.Empty, out *[]string) error {
	a.mu.Lock()
	*out = append([]string(nil), a.installed...)
	err := a.installedErr
	started := a.discoveryStarted
	release := a.releaseDiscovery
	a.installedCalls++
	a.mu.Unlock()
	if started != nil {
		a.discoveryOnce.Do(func() { close(started) })
	}
	if release != nil {
		<-release
	}
	return err
}

func (a *firewallSyncTestAgent) ApplyFirewall(req *FirewallSyncApplyRequest, out *FirewallStatusResp) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyCalls++
	a.applyRequests = append(a.applyRequests, FirewallSyncApplyRequest{
		MutationRequestID: req.MutationRequestID, MutationOwnerID: req.MutationOwnerID,
		Enabled: req.Enabled, TCPPorts: append([]int(nil), req.TCPPorts...),
		UDPPorts: append([]int(nil), req.UDPPorts...), Persist: req.Persist,
	})
	a.status.Enabled = req.Enabled
	a.status.EngineAvailable = true
	if req.Enabled && req.Persist {
		a.status.PersistenceState = "ready"
		a.status.SnapshotVersion = 2
	}
	*out = a.status
	out.Error = a.applyResponseError
	return a.applyErr
}

func attachFirewallSyncTestAgent(t *testing.T, panel *Panel, agent *firewallSyncTestAgent) {
	t.Helper()
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

func TestSyncFirewallCarriesDurableMutationBindingWhenPresent(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{Enabled: true, EngineAvailable: true},
	}
	panel := &Panel{}
	attachFirewallSyncTestAgent(t, panel, agent)
	ctx := withPanelMutationBinding(context.Background(), agentMutationBinding{
		MutationRequestID: "11111111111111111111111111111111",
		MutationOwnerID:   "22222222222222222222222222222222",
	})
	if err := panel.syncFirewall(ctx); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.applyRequests) != 1 ||
		agent.applyRequests[0].MutationRequestID != "11111111111111111111111111111111" ||
		agent.applyRequests[0].MutationOwnerID != "22222222222222222222222222222222" {
		t.Fatalf("firewall sync binding = %+v", agent.applyRequests)
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
