package main

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type hostingAgentCall struct {
	Method      string
	ProjectType string
}

type hostingTestAgent struct {
	mu sync.Mutex

	calls []hostingAgentCall

	applyApp   func(*transport.AppApplyRequest, *transport.AppApplyResponse) error
	removeApp  func(*transport.AppControlRequest, *transport.AppApplyResponse) error
	applyVhost func(*transport.ApplyVhostRequest, *transport.ApplyVhostResponse) error
}

func (a *hostingTestAgent) record(method, projectType string) {
	a.mu.Lock()
	a.calls = append(a.calls, hostingAgentCall{Method: method, ProjectType: projectType})
	a.mu.Unlock()
}

func (a *hostingTestAgent) ApplyAppUnit(
	req *transport.AppApplyRequest,
	resp *transport.AppApplyResponse,
) error {
	a.record("apply-app", "")
	if a.applyApp != nil {
		return a.applyApp(req, resp)
	}
	resp.Unit = appUnitNameForHostingTest(req.SiteID)
	return nil
}

func (a *hostingTestAgent) RemoveAppUnit(
	req *transport.AppControlRequest,
	resp *transport.AppApplyResponse,
) error {
	a.record("remove-app", "")
	if a.removeApp != nil {
		return a.removeApp(req, resp)
	}
	resp.Unit = appUnitNameForHostingTest(req.SiteID)
	return nil
}

func (a *hostingTestAgent) ApplyVhost(
	req *transport.ApplyVhostRequest,
	resp *transport.ApplyVhostResponse,
) error {
	a.record("apply-vhost", req.ProjectType)
	if a.applyVhost != nil {
		return a.applyVhost(req, resp)
	}
	resp.Config = "ok"
	return nil
}

func (a *hostingTestAgent) snapshotCalls() []hostingAgentCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]hostingAgentCall(nil), a.calls...)
}

func appUnitNameForHostingTest(siteID int) string {
	return "celikapp-" + fmtIntForHostingTest(siteID)
}

func fmtIntForHostingTest(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func attachHostingTestAgent(t *testing.T, panel *Panel, agent *hostingTestAgent) {
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

func resetHostingTestState(
	t *testing.T,
	panel *Panel,
	domainID int,
	projectType string,
) {
	t.Helper()
	var (
		appPort      any
		startCommand any
		runtime      any
	)
	if projectType == "node" {
		appPort = 3100
		startCommand = "npm start"
		runtime = "24.18.0"
	}
	if _, err := panel.db.GetDB().Exec(`
		UPDATE sites
		SET project_type = ?, app_port = ?, start_command = ?,
		    runtime_version = ?, forward_to = NULL, forward_code = NULL
		WHERE domain_id = ?`,
		projectType,
		appPort,
		startCommand,
		runtime,
		domainID,
	); err != nil {
		t.Fatalf("reset hosting state: %v", err)
	}
}

func hostingTestState(t *testing.T, panel *Panel, domainID int) hostingRuntimeState {
	t.Helper()
	state, err := panel.loadHostingRuntimeState(context.Background(), domainID)
	if err != nil {
		t.Fatalf("load hosting state: %v", err)
	}
	return state
}

func assertHostingCalls(
	t *testing.T,
	agent *hostingTestAgent,
	want []hostingAgentCall,
) {
	t.Helper()
	got := agent.snapshotCalls()
	if len(got) != len(want) {
		t.Fatalf("agent calls = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("agent call %d = %#v, want %#v (all: %#v)", index, got[index], want[index], got)
		}
	}
}

func nodeHostingRequest() hostingSettings {
	return hostingSettings{
		ProjectType:    "node",
		AppPort:        3200,
		StartCommand:   "node server.js",
		RuntimeVersion: "24.18.0",
	}
}

func TestHostingTransitionRestoresStaticStateOnApplyAppResponseFailure(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	resetHostingTestState(t, panel, domainID, "static")
	agent := &hostingTestAgent{
		applyApp: func(
			*transport.AppApplyRequest,
			*transport.AppApplyResponse,
		) error {
			return nil
		},
	}
	agent.applyApp = func(
		_ *transport.AppApplyRequest,
		resp *transport.AppApplyResponse,
	) error {
		resp.Error = "injected start failure"
		return nil
	}
	attachHostingTestAgent(t, panel, agent)

	_, err := panel.transitionHosting(context.Background(), domainID, nodeHostingRequest())
	if !errors.Is(err, errHostingActivation) {
		t.Fatalf("transition error = %v, want restored activation failure", err)
	}
	state := hostingTestState(t, panel, domainID)
	if state.ProjectType != "static" || state.AppPort.Valid {
		t.Fatalf("restored state = %#v, want static without app port", state)
	}
	assertHostingCalls(t, agent, []hostingAgentCall{
		{Method: "apply-app"},
		{Method: "remove-app"},
		{Method: "apply-vhost", ProjectType: "static"},
	})
}

func TestHostingTransitionChecksTransportFailureAndCompensates(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	resetHostingTestState(t, panel, domainID, "static")
	agent := &hostingTestAgent{
		applyApp: func(
			*transport.AppApplyRequest,
			*transport.AppApplyResponse,
		) error {
			return errors.New("injected RPC transport failure")
		},
	}
	attachHostingTestAgent(t, panel, agent)

	_, err := panel.transitionHosting(context.Background(), domainID, nodeHostingRequest())
	if !errors.Is(err, errHostingActivation) {
		t.Fatalf("transition error = %v, want restored activation failure", err)
	}
	if state := hostingTestState(t, panel, domainID); state.ProjectType != "static" {
		t.Fatalf("project type after transport failure = %q, want static", state.ProjectType)
	}
	assertHostingCalls(t, agent, []hostingAgentCall{
		{Method: "apply-app"},
		{Method: "remove-app"},
		{Method: "apply-vhost", ProjectType: "static"},
	})
}

func TestHostingTransitionRestoresDatabaseAppAndVhostOnVhostFailure(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	resetHostingTestState(t, panel, domainID, "static")
	vhostCalls := 0
	agent := &hostingTestAgent{
		applyVhost: func(
			_ *transport.ApplyVhostRequest,
			resp *transport.ApplyVhostResponse,
		) error {
			vhostCalls++
			if vhostCalls == 1 {
				resp.Error = "injected nginx validation failure"
			}
			return nil
		},
	}
	attachHostingTestAgent(t, panel, agent)

	_, err := panel.transitionHosting(context.Background(), domainID, nodeHostingRequest())
	if !errors.Is(err, errHostingActivation) {
		t.Fatalf("transition error = %v, want restored activation failure", err)
	}
	state := hostingTestState(t, panel, domainID)
	if state.ProjectType != "static" || state.AppPort.Valid {
		t.Fatalf("restored state = %#v, want static without app port", state)
	}
	assertHostingCalls(t, agent, []hostingAgentCall{
		{Method: "apply-app"},
		{Method: "apply-vhost", ProjectType: "node"},
		{Method: "remove-app"},
		{Method: "apply-vhost", ProjectType: "static"},
	})
}

func TestHostingTransitionRestoresNodeWhenRemoveResponseFails(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	resetHostingTestState(t, panel, domainID, "node")
	removeCalls := 0
	agent := &hostingTestAgent{
		removeApp: func(
			_ *transport.AppControlRequest,
			resp *transport.AppApplyResponse,
		) error {
			removeCalls++
			if removeCalls == 1 {
				resp.Error = "injected remove failure"
			}
			return nil
		},
	}
	attachHostingTestAgent(t, panel, agent)

	_, err := panel.transitionHosting(context.Background(), domainID, hostingSettings{
		ProjectType: "static",
	})
	if !errors.Is(err, errHostingActivation) {
		t.Fatalf("transition error = %v, want restored activation failure", err)
	}
	state := hostingTestState(t, panel, domainID)
	if state.ProjectType != "node" ||
		!state.AppPort.Valid ||
		state.AppPort.Int64 != 3100 {
		t.Fatalf("restored state = %#v, want original node state", state)
	}
	assertHostingCalls(t, agent, []hostingAgentCall{
		{Method: "apply-vhost", ProjectType: "static"},
		{Method: "remove-app"},
		{Method: "apply-app"},
		{Method: "apply-vhost", ProjectType: "node"},
	})
}

func TestHostingTransitionCompensatesNodeAfterDatabaseFailure(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	resetHostingTestState(t, panel, domainID, "static")
	if _, err := panel.db.GetDB().Exec(`
		CREATE TRIGGER reject_hosting_update
		BEFORE UPDATE OF project_type ON sites
		BEGIN
			SELECT RAISE(ABORT, 'injected hosting update failure');
		END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	agent := &hostingTestAgent{}
	attachHostingTestAgent(t, panel, agent)

	_, err := panel.transitionHosting(context.Background(), domainID, nodeHostingRequest())
	if !errors.Is(err, errHostingActivation) {
		t.Fatalf("transition error = %v, want restored activation failure", err)
	}
	if state := hostingTestState(t, panel, domainID); state.ProjectType != "static" {
		t.Fatalf("project type after DB failure = %q, want static", state.ProjectType)
	}
	assertHostingCalls(t, agent, []hostingAgentCall{
		{Method: "apply-app"},
		{Method: "remove-app"},
		{Method: "apply-vhost", ProjectType: "static"},
	})
}

func TestHostingTransitionReconcilesToConcurrentDatabaseWinner(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	resetHostingTestState(t, panel, domainID, "static")
	agent := &hostingTestAgent{}
	agent.applyApp = func(
		_ *transport.AppApplyRequest,
		_ *transport.AppApplyResponse,
	) error {
		if _, err := panel.db.GetDB().Exec(`
			UPDATE sites
			SET project_type = 'forwarding',
			    app_port = NULL,
			    start_command = NULL,
			    runtime_version = NULL,
			    forward_to = 'https://winner.example',
			    forward_code = 302
			WHERE domain_id = ?`, domainID); err != nil {
			return err
		}
		return nil
	}
	attachHostingTestAgent(t, panel, agent)

	_, err := panel.transitionHosting(
		context.Background(),
		domainID,
		nodeHostingRequest(),
	)
	if !errors.Is(err, errHostingConcurrentChange) {
		t.Fatalf("transition error = %v, want concurrent change", err)
	}
	state := hostingTestState(t, panel, domainID)
	if state.ProjectType != "forwarding" ||
		!state.ForwardTo.Valid ||
		state.ForwardTo.String != "https://winner.example" ||
		!state.ForwardCode.Valid ||
		state.ForwardCode.Int64 != 302 {
		t.Fatalf("winning database state was overwritten: %#v", state)
	}
	assertHostingCalls(t, agent, []hostingAgentCall{
		{Method: "apply-app"},
		{Method: "remove-app"},
		{Method: "apply-vhost", ProjectType: "forwarding"},
	})
}

func TestHostingTransitionSerializesConcurrentRequestsForSameDomain(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	resetHostingTestState(t, panel, domainID, "static")
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	vhostCalls := 0
	agent := &hostingTestAgent{
		applyVhost: func(
			_ *transport.ApplyVhostRequest,
			_ *transport.ApplyVhostResponse,
		) error {
			vhostCalls++
			if vhostCalls == 1 {
				close(firstEntered)
				<-releaseFirst
			}
			return nil
		},
	}
	attachHostingTestAgent(t, panel, agent)

	firstResult := make(chan error, 1)
	go func() {
		_, err := panel.transitionHosting(context.Background(), domainID, hostingSettings{
			ProjectType: "forwarding",
			ForwardTo:   "https://first.example",
			ForwardCode: 302,
		})
		firstResult <- err
	}()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first transition did not reach vhost activation")
	}

	secondResult := make(chan error, 1)
	go func() {
		_, err := panel.transitionHosting(context.Background(), domainID, hostingSettings{
			ProjectType: "proxy",
			ForwardTo:   "https://second.example",
		})
		secondResult <- err
	}()

	select {
	case err := <-secondResult:
		t.Fatalf("second transition completed before first released: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if calls := agent.snapshotCalls(); len(calls) != 1 {
		t.Fatalf("agent calls while first transition blocked = %#v, want one", calls)
	}

	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatalf("first transition: %v", err)
	}
	if err := <-secondResult; err != nil {
		t.Fatalf("second transition: %v", err)
	}

	state := hostingTestState(t, panel, domainID)
	if state.ProjectType != "proxy" ||
		!state.ForwardTo.Valid ||
		state.ForwardTo.String != "https://second.example" {
		t.Fatalf("final hosting state = %#v, want second proxy request", state)
	}
	assertHostingCalls(t, agent, []hostingAgentCall{
		{Method: "apply-vhost", ProjectType: "forwarding"},
		{Method: "apply-vhost", ProjectType: "proxy"},
	})
}
