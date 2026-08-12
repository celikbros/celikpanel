package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

type ServiceStateAtomicRepoRequest struct {
	ServiceID string
}

type ServiceStateAtomicRepoResponse struct {
	Packages []string
	Error    string
}

type ServiceStateAtomicInstancesRequest struct {
	ID string
}

type ServiceStateAtomicInstancesResponse struct {
	Instances []core.ServiceInstance
	Error     string
}

type serviceStateAtomicAgent struct {
	durableMutationRPCFixture
	getServicesCalls atomic.Int32
	uninstallCalls   atomic.Int32
	wireFilterCalls  atomic.Int32
	roundcubeCalls   atomic.Int32
	webmailCalls     atomic.Int32
	nodeRemoveCalls  atomic.Int32
	serviceRemoved   atomic.Bool
	roundcubePresent atomic.Bool

	getServicesError        error
	firewallStatusError     error
	installedIDsError       error
	repoPackagesError       error
	repoPackagesReply       string
	instancesError          error
	instancesReply          string
	serviceActionError      string
	uninstallReplyError     string
	uninstallMutation       bool
	wireFilterReply         string
	roundcubeRemovedFalse   bool
	roundcubeMutation       bool
	roundcubeReplyError     string
	roundcubeRPCFailures    int32
	roundcubeFailureApplies bool
	roundcubeCallHook       func()
	webmailConfiguredFalse  bool
	webmailPresent          bool
	webmailReplyError       string
	webmailRPCFailures      int32
	finishReplyError        string
	roundcubeRequestID      string
	roundcubeOwnerID        string
	webmailRequestID        string
	webmailOwnerID          string
	roundcubeBindings       []transport.ServiceMutationBinding
	webmailBindings         []transport.ServiceMutationBinding
}

func (a *serviceStateAtomicAgent) FinishServiceMutation(
	req *ServiceOperationMutationFinishRequest,
	out *ServiceOperationMutationResponse,
) error {
	if a.finishReplyError != "" {
		out.Error = a.finishReplyError
		return nil
	}
	return a.durableMutationRPCFixture.FinishServiceMutation(req, out)
}

func (a *serviceStateAtomicAgent) GetServices(_ *transport.Empty, out *[]core.Service) error {
	a.getServicesCalls.Add(1)
	if a.getServicesError != nil {
		return a.getServicesError
	}
	if a.serviceRemoved.Load() {
		*out = []core.Service{}
		return nil
	}
	*out = []core.Service{{Name: "redis", Status: "active (running)"}}
	return nil
}

func (a *serviceStateAtomicAgent) InstalledServiceIDs(_ *transport.Empty, out *[]string) error {
	if a.installedIDsError != nil {
		return a.installedIDsError
	}
	var installed []string
	if !a.serviceRemoved.Load() {
		installed = append(installed, "redis")
	}
	if a.roundcubePresent.Load() {
		installed = append(installed, "roundcube")
	}
	*out = installed
	return nil
}

func (a *serviceStateAtomicAgent) InstalledRepoPackages(
	_ *ServiceStateAtomicRepoRequest,
	out *ServiceStateAtomicRepoResponse,
) error {
	if a.repoPackagesError != nil {
		return a.repoPackagesError
	}
	out.Error = a.repoPackagesReply
	return nil
}

func (a *serviceStateAtomicAgent) ListServiceInstances(
	_ *ServiceStateAtomicInstancesRequest,
	out *ServiceStateAtomicInstancesResponse,
) error {
	if a.instancesError != nil {
		return a.instancesError
	}
	out.Error = a.instancesReply
	out.Instances = []core.ServiceInstance{}
	return nil
}

func (a *serviceStateAtomicAgent) PkgFamily(_ *transport.Empty, out *string) error {
	*out = "apt"
	return nil
}

func (a *serviceStateAtomicAgent) ServiceMutationStatus(
	_ *ServiceOperationMutationStatusRequest,
	out *ServiceOperationMutationResponse,
) error {
	out.Job = nil
	out.Error = ""
	return nil
}

func (a *serviceStateAtomicAgent) ServiceAction(
	_ *transport.ServiceActionArgs,
	out *transport.ServiceActionResult,
) error {
	if a.serviceActionError != "" {
		out.Error = a.serviceActionError
		return nil
	}
	out.Success = true
	return nil
}

func (a *serviceStateAtomicAgent) ServiceMutationAction(
	req *transport.ServiceActionArgs,
	out *transport.ServiceActionResult,
) error {
	return a.ServiceAction(req, out)
}

// InstalledRepoPackages keeps the older service-operation fixture compatible
// with the now-essential repository-package probe used by every fresh scan.
// InstalledRepoPackages, eski servis-işlemi fixture'ını artık her taze taramada
// zorunlu olan depo-paketi yoklamasıyla uyumlu tutar.
func (a *serviceOperationTestAgent) InstalledRepoPackages(
	_ *ServiceStateAtomicRepoRequest,
	out *ServiceStateAtomicRepoResponse,
) error {
	out.Packages = []string{}
	return nil
}

func newServiceStateAtomicPanel(t *testing.T) (*Panel, *serviceStateAtomicAgent, *paneldb.SQLiteDB, int) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('state-admin','x','state@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	agent := &serviceStateAtomicAgent{roundcubeMutation: true}
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
	t.Cleanup(func() { _ = client.Close() })
	panel := &Panel{
		db:           database,
		agentClient:  transport.NewReconnectingClientWithContextConnector(client, connector),
		pkgFamilyVal: "apt",
	}
	return panel, agent, database, int(userID)
}

func serviceStateAtomicRequest(method, target, body string, userID int) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	return req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: userID, Role: roleAdmin}))
}

func TestServiceActionReportsRefreshFailureAndAuditsBothOutcomes(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	agent.getServicesError = errors.New("state probe unavailable")
	recorder := httptest.NewRecorder()
	panel.handleServiceAction(recorder, serviceStateAtomicRequest(
		http.MethodPost,
		"/api/v1/service/action",
		`{"service_name":" redis.service ","action":"restart"}`,
		userID,
	))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeServiceStateRefreshFailed {
		t.Fatalf("code=%q body=%s", body.Code, recorder.Body.String())
	}

	rows, err := database.GetDB().Query(`SELECT action FROM audit_logs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	if len(actions) != 2 || actions[0] != "service.restart:redis" ||
		!strings.HasPrefix(actions[1], "service.restart.refresh.failed:redis — ") {
		t.Fatalf("audit actions=%q", actions)
	}
}

func TestManualServiceScanUsesMutationGate(t *testing.T) {
	panel, agent, _, userID := newServiceStateAtomicPanel(t)
	panel.serviceMutationMu.Lock()
	recorder := httptest.NewRecorder()
	panel.handleManagedServicesScan(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/managed-services/scan", "", userID,
	))
	panel.serviceMutationMu.Unlock()

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if agent.getServicesCalls.Load() != 0 {
		t.Fatalf("scan reached agent %d times while mutation gate was held", agent.getServicesCalls.Load())
	}
}

func TestEssentialServiceProbeFailuresPreservePreviousCache(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*serviceStateAtomicAgent)
	}{
		{"installed ids transport", func(a *serviceStateAtomicAgent) { a.installedIDsError = errors.New("ids unavailable") }},
		{"repo packages transport", func(a *serviceStateAtomicAgent) { a.repoPackagesError = errors.New("repo packages unavailable") }},
		{"repo packages reply", func(a *serviceStateAtomicAgent) { a.repoPackagesReply = "repo package probe refused" }},
		{"instances transport", func(a *serviceStateAtomicAgent) { a.instancesError = errors.New("instances unavailable") }},
		{"instances reply", func(a *serviceStateAtomicAgent) { a.instancesReply = "instance probe refused" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, agent, database, _ := newServiceStateAtomicPanel(t)
			const oldData = `{"observations":[{"id":"redis","is_installed":false,"status":"inactive (dead)"}]}`
			const oldScannedAt = "2026-07-20T01:02:03Z"
			if _, err := database.GetDB().Exec(
				`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)`,
				oldData, oldScannedAt,
			); err != nil {
				t.Fatal(err)
			}
			test.configure(agent)

			if _, err := panel.scanManagedServices(context.Background()); err == nil {
				t.Fatal("scan succeeded despite an essential probe failure")
			}
			var gotData, gotScannedAt string
			if err := database.GetDB().QueryRow(
				`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`,
			).Scan(&gotData, &gotScannedAt); err != nil {
				t.Fatal(err)
			}
			if gotData != oldData || gotScannedAt != oldScannedAt {
				t.Fatalf("cache changed to data=%q scanned_at=%q", gotData, gotScannedAt)
			}
		})
	}
}

func TestManagedServicesRejectsCorruptCacheAsUnverified(t *testing.T) {
	panel, _, database, userID := newServiceStateAtomicPanel(t)
	if _, err := database.GetDB().Exec(
		`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)`,
		`{"observations":[`,
		"2026-07-20T01:02:03Z",
	); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	panel.handleManagedServices(recorder, serviceStateAtomicRequest(
		http.MethodGet,
		"/api/v1/managed-services",
		"",
		userID,
	))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeServiceStateUnverified {
		t.Fatalf("code=%q body=%s", body.Code, recorder.Body.String())
	}
}
