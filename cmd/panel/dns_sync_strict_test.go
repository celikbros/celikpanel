package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type StrictDNSRPCEmpty struct{}

type StrictDNSRPCSyncRequest struct {
	Domain string
}

type StrictDNSRPCSyncResponse struct {
	Synced bool
	Error  string
}

type StrictDNSRPCClusterRequest struct {
	Role   string
	PeerIP string
	PeerNS string
}

type StrictDNSRPCClusterResponse struct {
	Applied bool
	Detail  string
	Error   string
}

type StrictDNSRPCPowerResponse struct {
	Synced bool
	Error  string
}

type strictDNSRPCAgent struct {
	durableMutationRPCFixture
	mu            sync.Mutex
	failZone      string
	syncCalls     []string
	clusterCalls  int
	powerDNSCalls int
}

func (a *strictDNSRPCAgent) BeginServiceMutation(
	req *ServiceOperationMutationBeginRequest,
	resp *ServiceOperationMutationResponse,
) error {
	return a.durableMutationRPCFixture.BeginServiceMutation(req, resp)
}

func (a *strictDNSRPCAgent) HeartbeatServiceMutation(
	req *ServiceOperationMutationHeartbeatRequest,
	resp *ServiceOperationMutationResponse,
) error {
	return a.durableMutationRPCFixture.HeartbeatServiceMutation(req, resp)
}

func (a *strictDNSRPCAgent) FinishServiceMutation(
	req *ServiceOperationMutationFinishRequest,
	resp *ServiceOperationMutationResponse,
) error {
	return a.durableMutationRPCFixture.FinishServiceMutation(req, resp)
}

func (a *strictDNSRPCAgent) SyncDNSZone(req *StrictDNSRPCSyncRequest, resp *StrictDNSRPCSyncResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncCalls = append(a.syncCalls, req.Domain)
	if req.Domain == a.failZone {
		resp.Error = "forced publication failure with internal detail"
		return nil
	}
	resp.Synced = true
	return nil
}

func (a *strictDNSRPCAgent) ConfigureDNSCluster(_ *StrictDNSRPCClusterRequest, resp *StrictDNSRPCClusterResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clusterCalls++
	resp.Applied = true
	resp.Detail = "configured"
	return nil
}

func (a *strictDNSRPCAgent) ConfigurePowerDNSSQLite(_ *StrictDNSRPCEmpty, resp *StrictDNSRPCPowerResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.powerDNSCalls++
	resp.Synced = true
	return nil
}

func attachStrictDNSRPCAgent(t *testing.T, p *Panel, agent *strictDNSRPCAgent) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register fake DNS agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	rawClient, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(rawClient, connector)
	t.Cleanup(func() { _ = rawClient.Close() })
}

func strictDNSAdminRequest(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}))
}

func seedStrictDNSZone(t *testing.T, p *Panel, domain string) {
	t.Helper()
	if _, err := p.ensureZone(context.Background(), domain); err != nil {
		t.Fatalf("seed DNS zone %s: %v", domain, err)
	}
}

func assertPublicationConflict(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "could not be published") {
		t.Fatalf("response is not actionable: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "internal detail") {
		t.Fatalf("response leaked agent detail: %s", recorder.Body.String())
	}
}

func TestSyncAllZonesResultAttemptsEveryZoneAndRetainsFailures(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "zeta.example")
	seedStrictDNSZone(t, p, "alpha.example")

	var calls []string
	result, err := p.syncAllZonesResult(context.Background(), func(_ context.Context, domain string, _ bool) error {
		calls = append(calls, domain)
		if domain == "alpha.example" {
			return errors.New("boom")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sync all zones: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"alpha.example", "zeta.example"}) {
		t.Fatalf("publication calls = %v", calls)
	}
	if result.Attempted != 2 || result.Synced != 1 || len(result.Failures) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, ok := result.err().(*dnsSyncInternalError); !ok {
		t.Fatalf("unclassified callback error = %T, want *dnsSyncInternalError", result.err())
	}
}

func TestSyncAllZonesResultClassifiesPanelFailureAsInternal(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")

	result, err := p.syncAllZonesResult(context.Background(), func(_ context.Context, _ string, _ bool) error {
		return errors.New("ledger read failed")
	})
	if err != nil {
		t.Fatalf("sync all zones: %v", err)
	}
	if _, ok := result.err().(*dnsSyncInternalError); !ok {
		t.Fatalf("panel failure = %T, want *dnsSyncInternalError", result.err())
	}
	var publicationErr *dnsPublicationError
	if errors.As(result.err(), &publicationErr) {
		t.Fatalf("panel failure was misclassified as retryable publication error")
	}
}

func TestSyncZoneScanFailureNeverPublishesPartialZone(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	var zoneID int
	if err := p.db.GetDB().QueryRow(
		`SELECT id FROM pdns_domains WHERE name = 'biovision.health'`,
	).Scan(&zoneID); err != nil {
		t.Fatalf("find zone: %v", err)
	}
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio, disabled)
		VALUES (?, NULL, 'TXT', '"must-not-be-dropped"', 3600, 0, 0)`, zoneID); err != nil {
		t.Fatalf("insert malformed record: %v", err)
	}
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	err := p.syncZoneToDNS(context.Background(), "biovision.health", false)
	if err == nil || !strings.Contains(err.Error(), "scan zone record") {
		t.Fatalf("scan failure = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.syncCalls) != 0 {
		t.Fatalf("partial zone was sent to agent: %v", agent.syncCalls)
	}
}

func TestDNSSettingsEndpointsRefuseSuccessWhenZonePublicationFails(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  string
		path    string
		body    string
		handler func(*Panel, http.ResponseWriter, *http.Request)
	}{
		{
			name:    "nameservers",
			method:  http.MethodPut,
			path:    "/api/v1/settings/nameservers",
			body:    `{"ns1":"ns1.example.net","ns2":"ns2.example.net"}`,
			handler: (*Panel).handleNameserverSettings,
		},
		{
			name:    "cluster role",
			method:  http.MethodPut,
			path:    "/api/v1/settings/dns-cluster",
			body:    `{"role":"standalone"}`,
			handler: (*Panel).handleDNSCluster,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "standalone")
			seedStrictDNSZone(t, p, "biovision.health")
			agent := &strictDNSRPCAgent{failZone: "biovision.health"}
			attachStrictDNSRPCAgent(t, p, agent)

			req := strictDNSAdminRequest(httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			recorder := httptest.NewRecorder()
			tc.handler(p, recorder, req)
			assertPublicationConflict(t, recorder)
		})
	}
}

func TestPDNSEnableRefusesSuccessWhenAnyZonePublicationFails(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	agent := &strictDNSRPCAgent{failZone: "biovision.health"}
	attachStrictDNSRPCAgent(t, p, agent)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pdns/enable", nil)
	recorder := httptest.NewRecorder()
	p.handlePDNSEnable(recorder, req)
	assertPublicationConflict(t, recorder)
}

func TestPDNSEnableReportsLedgerReadFailureAsInternalError(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	if _, err := p.db.GetDB().Exec(`
		DELETE FROM pdns_records
		WHERE domain_id = (SELECT id FROM pdns_domains WHERE name = 'biovision.health')
		  AND type = 'SOA'`); err != nil {
		t.Fatalf("remove SOA: %v", err)
	}
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pdns/enable", nil)
	recorder := httptest.NewRecorder()
	p.handlePDNSEnable(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"INTERNAL"`) {
		t.Fatalf("internal error contract missing: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "valid SOA") {
		t.Fatalf("response leaked ledger detail: %s", recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.syncCalls) != 0 {
		t.Fatalf("invalid ledger zone was sent to agent: %v", agent.syncCalls)
	}
}

func TestNewTopLevelDomainPublicationFailureIsReturnedAndZoneIsRetained(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	agent := &strictDNSRPCAgent{failZone: "biovision.health"}
	attachStrictDNSRPCAgent(t, p, agent)

	zoneExists, err := p.publishNewTopLevelDomainZone(context.Background(), "biovision.health")
	if err == nil || !strings.Contains(err.Error(), "publish DNS zone") {
		t.Fatalf("publication error = %v", err)
	}
	if !zoneExists {
		t.Fatal("publication failure did not report the retained DNS zone")
	}
	var zones int
	if err := p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains WHERE name = 'biovision.health'`).Scan(&zones); err != nil {
		t.Fatalf("count retained zone: %v", err)
	}
	if zones != 1 {
		t.Fatalf("retained zones = %d, want 1", zones)
	}
}

func TestDomainCreatePartialSuccessCarriesRecoveryContext(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeDomainCreatePartialSuccess(recorder, 42, "biovision.health", true)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	var body domainCreatePartialSuccess
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.PartialSuccess || body.Code != errCodeDNSPublicationPending || body.DomainID != 42 {
		t.Fatalf("partial-success identity = %+v", body)
	}
	if body.Domain != "biovision.health" || !body.ZoneExists || body.Action != "/domains/biovision.health?tab=dns" {
		t.Fatalf("partial-success recovery context = %+v", body)
	}
}

func TestDNSZonePostRepublishesExistingZone(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleCreateDNSZone(recorder,
		httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/dns/zone", nil),
		"biovision.health")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Created bool `json:"created"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.Created {
		t.Fatalf("existing-zone response = %+v", body)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if !reflect.DeepEqual(agent.syncCalls, []string{"biovision.health"}) {
		t.Fatalf("publication calls = %v", agent.syncCalls)
	}
}

func TestDNSZonePostReportsPublicationFailureAsConflict(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	agent := &strictDNSRPCAgent{failZone: "biovision.health"}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleCreateDNSZone(recorder,
		httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/dns/zone", nil),
		"biovision.health")

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"DNS_PUBLICATION_FAILED"`) {
		t.Fatalf("publication error contract missing: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "internal detail") {
		t.Fatalf("response leaked agent detail: %s", recorder.Body.String())
	}
}

func TestDNSZonePostReportsLedgerFailureAsInternal(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "biovision.health")
	if _, err := p.db.GetDB().Exec(`
		DELETE FROM pdns_records
		WHERE domain_id = (SELECT id FROM pdns_domains WHERE name = 'biovision.health')
		  AND type = 'SOA'`); err != nil {
		t.Fatalf("remove SOA: %v", err)
	}
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleCreateDNSZone(recorder,
		httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/dns/zone", nil),
		"biovision.health")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"INTERNAL"`) {
		t.Fatalf("internal error contract missing: %s", recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.syncCalls) != 0 {
		t.Fatalf("invalid ledger zone was sent to agent: %v", agent.syncCalls)
	}
}
