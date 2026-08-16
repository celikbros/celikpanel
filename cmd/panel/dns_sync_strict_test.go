package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type StrictDNSRPCEmpty struct{}

type StrictDNSRPCSyncRequest = transport.SyncDNSZoneV2Request
type StrictDNSRPCSyncResponse = transport.SyncDNSZoneV2Response

type StrictDNSRPCClusterRequest = transport.ConfigureDNSClusterV2Request
type StrictDNSRPCClusterResponse = transport.ConfigureDNSClusterV2Response

type StrictDNSRPCPowerResponse struct {
	Synced bool
	Error  string
}

type strictDNSRPCAgent struct {
	durableMutationRPCFixture
	mu                  sync.Mutex
	failZone            string
	clusterError        string
	clusterRequests     []transport.ConfigureDNSClusterV2Request
	clusterHook         func(transport.ConfigureDNSClusterV2Request, *transport.ConfigureDNSClusterV2Response) error
	clusterEntered      chan struct{}
	clusterRelease      <-chan struct{}
	syncCalls           []string
	syncRequests        []transport.SyncDNSZoneV2Request
	syncV3Requests      []transport.SyncDNSZoneV3Request
	syncHook            func(transport.SyncDNSZoneV2Request)
	secureRequests      []transport.SecureDNSZoneV2Request
	secureHook          func(transport.SecureDNSZoneV2Request, *transport.SecureDNSZoneV2Response) error
	secureResponseError string
	finishError         error
	beginHook           func(*ServiceOperationMutationBeginRequest) error
	statusError         error
	generationDelta     int64
	versionCommit       string
	versionCapabilities *[]string
	versionCalls        int
	beginCalls          int
	clusterCalls        int
	powerDNSCalls       int
	secureDNSCalls      int
	dnssecStatusCalls   int
	dnssecStatusError   string
	cancelCalls         int
	readinessCalls      int
	readinessReady      bool
	readinessDetail     string
	readinessError      error
	publisher           transport.DNSEngine
}

func (a *strictDNSRPCAgent) Version(
	_ *transport.Empty,
	resp *transport.AgentVersionResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.versionCalls++
	resp.Commit = a.versionCommit
	capabilities := []string{
		transport.AgentCapabilityDNSZoneSyncV2,
		transport.AgentCapabilityDNSZoneSyncV3,
		transport.AgentCapabilityDNSSECSecureV2,
		transport.AgentCapabilityDNSClusterConfigureV2,
	}
	if a.versionCapabilities != nil {
		capabilities = append([]string(nil), (*a.versionCapabilities)...)
	}
	resp.Capabilities = capabilities
	return nil
}

func (a *strictDNSRPCAgent) DNSBackendReadiness(
	_ *transport.Empty,
	resp *transport.DNSBackendReadinessResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	publisher := a.publisher
	if publisher == "" {
		publisher = transport.DNSEnginePowerDNS
	}
	resp.Engines = []transport.DNSBackendRuntimeState{
		{
			Engine:    transport.DNSEnginePowerDNS,
			Installed: publisher == transport.DNSEnginePowerDNS,
			Running:   publisher == transport.DNSEnginePowerDNS,
			Managed:   publisher == transport.DNSEnginePowerDNS,
			Unit:      "pdns.service",
		},
		{
			Engine:    transport.DNSEngineBIND,
			Installed: publisher == transport.DNSEngineBIND,
			Running:   publisher == transport.DNSEngineBIND,
			Managed:   publisher == transport.DNSEngineBIND,
			Unit:      "bind9.service",
		},
	}
	return nil
}

func (a *strictDNSRPCAgent) BeginServiceMutation(
	req *ServiceOperationMutationBeginRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	a.beginCalls++
	hook := a.beginHook
	a.mu.Unlock()
	if hook != nil {
		if err := hook(req); err != nil {
			resp.Error = err.Error()
			return nil
		}
	}
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
	a.mu.Lock()
	finishErr := a.finishError
	a.mu.Unlock()
	if finishErr != nil {
		return finishErr
	}
	// A lost mutating RPC response can arrive after the agent has already
	// committed a terminal receipt. Real agents return that receipt unchanged;
	// the strict fake must not rewrite succeeded back to failed.
	a.durableMutationRPCFixture.mu.Lock()
	job := a.durableMutationRPCFixture.jobs[req.RequestID]
	if job != nil && !agentMutationActive(job.Status) {
		resp.Job = cloneServiceOperationMutationJob(job)
		a.durableMutationRPCFixture.mu.Unlock()
		return nil
	}
	a.durableMutationRPCFixture.mu.Unlock()
	return a.durableMutationRPCFixture.FinishServiceMutation(req, resp)
}

func (a *strictDNSRPCAgent) CancelServiceMutation(
	req *ServiceOperationMutationCancelRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	a.cancelCalls++
	a.mu.Unlock()
	a.durableMutationRPCFixture.mu.Lock()
	defer a.durableMutationRPCFixture.mu.Unlock()
	job := a.durableMutationRPCFixture.jobs[req.RequestID]
	if job == nil || job.OwnerID != req.ExpectedOwner {
		resp.Error = "service mutation owner mismatch"
		resp.Job = cloneServiceOperationMutationJob(job)
		return nil
	}
	job.Status = agentMutationFailed
	job.Phase = "interrupted"
	job.ErrorCode = req.FailureCode
	job.ErrorMessage = req.FailureMessage
	if a.durableMutationRPCFixture.active == req.RequestID {
		a.durableMutationRPCFixture.active = ""
	}
	resp.Job = cloneServiceOperationMutationJob(job)
	return nil
}

func (a *strictDNSRPCAgent) ServiceMutationStatus(
	req *ServiceOperationMutationStatusRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	statusErr := a.statusError
	a.mu.Unlock()
	if statusErr != nil {
		return statusErr
	}
	a.durableMutationRPCFixture.mu.Lock()
	defer a.durableMutationRPCFixture.mu.Unlock()
	requestID := req.RequestID
	if requestID == "" {
		requestID = a.durableMutationRPCFixture.active
	}
	resp.Job = cloneServiceOperationMutationJob(a.durableMutationRPCFixture.jobs[requestID])
	return nil
}

func (a *strictDNSRPCAgent) SyncDNSZoneV2(req *StrictDNSRPCSyncRequest, resp *StrictDNSRPCSyncResponse) error {
	a.mu.Lock()
	a.syncCalls = append(a.syncCalls, req.Domain)
	copy := *req
	copy.Records = append([]transport.ZoneRecord(nil), req.Records...)
	a.syncRequests = append(a.syncRequests, copy)
	hook := a.syncHook
	delta := a.generationDelta
	a.mu.Unlock()
	if hook != nil {
		hook(copy)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if req.Domain == a.failZone {
		resp.Error = "forced publication failure with internal detail"
		return nil
	}
	resp.Synced = true
	resp.AppliedGeneration = req.DesiredGeneration + delta
	return nil
}

func (a *strictDNSRPCAgent) SyncDNSZoneV3(
	req *transport.SyncDNSZoneV3Request,
	resp *transport.SyncDNSZoneV3Response,
) error {
	copy := *req
	copy.Records = append([]transport.ZoneRecord(nil), req.Records...)
	legacy := transport.SyncDNSZoneV2Request{
		ServiceMutationBinding: req.ServiceMutationBinding,
		DesiredGeneration:      req.DesiredGeneration,
		Domain:                 req.Domain,
		Delete:                 req.Delete,
		ZoneType:               req.ZoneType,
		Records:                append([]transport.ZoneRecord(nil), req.Records...),
	}
	a.mu.Lock()
	a.syncCalls = append(a.syncCalls, req.Domain)
	a.syncV3Requests = append(a.syncV3Requests, copy)
	a.syncRequests = append(a.syncRequests, legacy)
	hook := a.syncHook
	delta := a.generationDelta
	a.mu.Unlock()
	if hook != nil {
		hook(legacy)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if req.Domain == a.failZone {
		resp.Error = "forced publication failure with internal detail"
		return nil
	}
	resp.Synced = true
	resp.Engine = req.Engine
	resp.EngineEpoch = req.EngineEpoch
	resp.AppliedGeneration = req.DesiredGeneration + delta
	return nil
}

func (a *strictDNSRPCAgent) ConfigureDNSClusterV2(req *StrictDNSRPCClusterRequest, resp *StrictDNSRPCClusterResponse) error {
	a.mu.Lock()
	a.clusterCalls++
	a.clusterRequests = append(a.clusterRequests, *req)
	clusterError := a.clusterError
	hook := a.clusterHook
	entered := a.clusterEntered
	release := a.clusterRelease
	a.mu.Unlock()
	if hook != nil {
		if err := hook(*req, resp); err != nil {
			return err
		}
	}
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	if clusterError != "" {
		resp.Error = clusterError
		return nil
	}
	a.durableMutationRPCFixture.mu.Lock()
	job := a.durableMutationRPCFixture.jobs[req.MutationRequestID]
	if job == nil || job.OwnerID != req.MutationOwnerID ||
		job.Kind != "dns_cluster_configure" || job.Target != "pdns" {
		a.durableMutationRPCFixture.mu.Unlock()
		return errors.New("strict DNS cluster fake lost its exact durable job")
	}
	job.Status = agentMutationSucceeded
	job.Phase = dnsClusterPublishedPhase(dnsClusterSaga{
		RequestID: job.RequestID,
		Qualifier: job.PackageName,
	})
	if a.durableMutationRPCFixture.active == job.RequestID {
		a.durableMutationRPCFixture.active = ""
	}
	a.durableMutationRPCFixture.mu.Unlock()
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

func (a *strictDNSRPCAgent) SecureDNSZoneV2(
	req *transport.SecureDNSZoneV2Request,
	resp *transport.SecureDNSZoneV2Response,
) error {
	a.mu.Lock()
	a.secureDNSCalls++
	copy := *req
	a.secureRequests = append(a.secureRequests, copy)
	hook := a.secureHook
	responseError := a.secureResponseError
	a.mu.Unlock()
	if hook != nil {
		if err := hook(copy, resp); err != nil {
			return err
		}
	}
	if responseError != "" {
		resp.Error = responseError
		return nil
	}
	resp.Secured = true
	resp.DS = []string{"12345 13 2 AABBCC"}
	return nil
}

func (a *strictDNSRPCAgent) DNSSECStatus(
	_ *transport.DNSSECRequest,
	resp *transport.DNSSECStatusResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dnssecStatusCalls++
	if a.dnssecStatusError != "" {
		resp.Error = a.dnssecStatusError
		return nil
	}
	resp.Secured = true
	resp.DS = []string{"12345 13 2 AABBCC"}
	return nil
}

func (a *strictDNSRPCAgent) DNSClusterReadiness(
	_ *transport.Empty,
	resp *transport.DNSClusterReadinessResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.readinessCalls++
	if a.readinessError != nil {
		return a.readinessError
	}
	resp.Ready = a.readinessReady || a.readinessDetail == ""
	resp.Detail = a.readinessDetail
	return nil
}

func attachStrictDNSRPCAgent(t *testing.T, p *Panel, agent *strictDNSRPCAgent) {
	attachStrictDNSRPCAgentForEngine(
		t, p, agent, transport.DNSEnginePowerDNS,
	)
}

func attachStrictDNSRPCAgentForEngine(
	t *testing.T,
	p *Panel,
	agent *strictDNSRPCAgent,
	engine transport.DNSEngine,
) {
	t.Helper()
	ensureActiveDNSEngineForTest(t, p, engine)
	agent.mu.Lock()
	agent.publisher = engine
	agent.mu.Unlock()
	p.pkgFamilyVal = "apt"
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
	if !strings.Contains(recorder.Body.String(), `"code":"`+errCodeDNSPublicationFailed+`"`) {
		t.Fatalf("publication failure has no stable code: %s", recorder.Body.String())
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
	if err == nil || !strings.Contains(err.Error(), "scan DNS zone snapshot") {
		t.Fatalf("scan failure = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.syncCalls) != 0 {
		t.Fatalf("partial zone was sent to agent: %v", agent.syncCalls)
	}
}

func TestDNSZoneV3LeaseIsPersistedBeforeAgentBegin(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "lease-before-begin.example")
	beginObserved := false
	agent := &strictDNSRPCAgent{}
	agent.beginHook = func(req *ServiceOperationMutationBeginRequest) error {
		if req.Kind != "dns_zone_sync" {
			return nil
		}
		beginObserved = true
		var requestID, ownerID, qualifier string
		var generation int64
		if err := p.db.GetDB().QueryRow(`
			SELECT request_id, owner_id, qualifier, desired_generation
			FROM dns_zone_engine_leases WHERE zone_name = ?`, req.Target).Scan(
			&requestID, &ownerID, &qualifier, &generation,
		); err != nil {
			return fmt.Errorf("read persisted DNS lease at Begin: %w", err)
		}
		if requestID != req.RequestID || ownerID != req.OwnerID ||
			qualifier != req.PackageName || generation <= 0 {
			return fmt.Errorf("persisted DNS lease does not match Begin identity")
		}
		return nil
	}
	attachStrictDNSRPCAgent(t, p, agent)

	if err := p.syncZoneToDNS(context.Background(), "lease-before-begin.example", false); err != nil {
		t.Fatal(err)
	}
	if !beginObserved {
		t.Fatal("DNS Begin was not observed")
	}
}

func TestDNSZoneV2CapabilityGatePreventsLeaseAndHostTouch(t *testing.T) {
	tests := []struct {
		name         string
		panelCommit  string
		agentCommit  string
		capabilities []string
	}{
		{name: "legacy capability missing", capabilities: []string{}},
		{
			name: "paired build mismatch", panelCommit: "panel-release",
			agentCommit:  "agent-release",
			capabilities: []string{transport.AgentCapabilityDNSZoneSyncV2},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.panelCommit != "" {
				withPanelBuildCommit(t, test.panelCommit)
			}
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "standalone")
			seedStrictDNSZone(t, p, "capability-gate.example")
			before, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "capability-gate.example")
			if err != nil {
				t.Fatal(err)
			}
			capabilities := append([]string(nil), test.capabilities...)
			agent := &strictDNSRPCAgent{
				versionCommit:       test.agentCommit,
				versionCapabilities: &capabilities,
			}
			attachStrictDNSRPCAgent(t, p, agent)

			err = p.syncZoneToDNS(context.Background(), "capability-gate.example", false)
			if err == nil {
				t.Fatal("old or mismatched DNS agent was accepted")
			}
			after, readErr := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "capability-gate.example")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if after.hasLease() || after.DesiredGeneration != before.DesiredGeneration ||
				after.AppliedGeneration != before.AppliedGeneration || after.Status != before.Status {
				t.Fatalf("capability rejection mutated DNS state: before=%+v after=%+v", before, after)
			}
			agent.mu.Lock()
			versionCalls := agent.versionCalls
			syncCalls := len(agent.syncCalls)
			agent.mu.Unlock()
			agent.durableMutationRPCFixture.mu.Lock()
			jobs := len(agent.durableMutationRPCFixture.jobs)
			agent.durableMutationRPCFixture.mu.Unlock()
			if versionCalls != 1 || syncCalls != 0 || jobs != 0 {
				t.Fatalf("capability rejection calls: version=%d sync=%d jobs=%d", versionCalls, syncCalls, jobs)
			}
		})
	}
}

func TestDNSZoneV2RHELPolicyDenialPreventsSnapshotMutation(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "rhel-denied.example")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)
	p.pkgFamilyMu.Lock()
	p.pkgFamilyVal = "dnf"
	p.hostPlatformVal = rhelPolicyTestIdentity()
	p.hostPlatformKnown = true
	p.pkgFamilyMu.Unlock()

	before, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "rhel-denied.example")
	if err != nil {
		t.Fatal(err)
	}
	var beforeSOA string
	if err := p.db.GetDB().QueryRow(`
		SELECT r.content
		FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = 'rhel-denied.example' AND r.type = 'SOA'`,
	).Scan(&beforeSOA); err != nil {
		t.Fatal(err)
	}

	err = p.syncZoneToDNS(context.Background(), "rhel-denied.example", false)
	if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
		t.Fatalf("RHEL DNS V2 denial=%v", err)
	}
	after, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "rhel-denied.example")
	if err != nil {
		t.Fatal(err)
	}
	var afterSOA string
	if err := p.db.GetDB().QueryRow(`
		SELECT r.content
		FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = 'rhel-denied.example' AND r.type = 'SOA'`,
	).Scan(&afterSOA); err != nil {
		t.Fatal(err)
	}
	if after.hasLease() || after.DesiredGeneration != before.DesiredGeneration ||
		after.AppliedGeneration != before.AppliedGeneration || after.Status != before.Status ||
		afterSOA != beforeSOA {
		t.Fatalf("RHEL denial mutated snapshot: before=%+v/%q after=%+v/%q", before, beforeSOA, after, afterSOA)
	}
	agent.mu.Lock()
	syncCalls := len(agent.syncCalls)
	agent.mu.Unlock()
	agent.durableMutationRPCFixture.mu.Lock()
	jobs := len(agent.durableMutationRPCFixture.jobs)
	agent.durableMutationRPCFixture.mu.Unlock()
	if syncCalls != 0 || jobs != 0 || len(rhelPreviewAgentRPCMethodGrants) != 0 {
		t.Fatalf("RHEL denial host/jobs/grants=%d/%d/%v", syncCalls, jobs, rhelPreviewAgentRPCMethodGrants)
	}
}

func TestDNSZoneV2StatusNilClearsFutureLeaseAndRepublishes(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "future-lease.example")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	plan, err := p.prepareDNSZoneSyncPlan(context.Background(), "future-lease.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.State.hasLease() {
		t.Fatal("prepared plan did not persist a future lease")
	}
	if err := p.syncZoneToDNS(context.Background(), "future-lease.example", false); err != nil {
		t.Fatalf("republish after proven no-job lease: %v", err)
	}
	state, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "future-lease.example")
	if err != nil {
		t.Fatal(err)
	}
	if state.hasLease() || state.Status != "applied" ||
		state.AppliedGeneration != state.DesiredGeneration {
		t.Fatalf("recovered DNS state=%+v", state)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV2Request(nil), agent.syncRequests...)
	agent.mu.Unlock()
	if len(requests) != 1 || requests[0].MutationRequestID == plan.RequestID {
		t.Fatalf("fresh publication requests=%+v old request=%s", requests, plan.RequestID)
	}
}

func TestDNSZoneV2AmbiguousStatusRetainsExactLease(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "ambiguous.example")
	agent := &strictDNSRPCAgent{
		failZone:    "ambiguous.example",
		statusError: errors.New("simulated status transport loss"),
	}
	attachStrictDNSRPCAgent(t, p, agent)

	err := p.syncZoneToDNS(context.Background(), "ambiguous.example", false)
	if err == nil || !strings.Contains(err.Error(), "terminal status is ambiguous") {
		t.Fatalf("ambiguous publication error=%v", err)
	}
	state, readErr := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "ambiguous.example")
	if readErr != nil {
		t.Fatal(readErr)
	}
	lease, leaseErr := readDNSZoneEngineLease(
		context.Background(), p.db.GetDB(), "ambiguous.example",
	)
	if leaseErr != nil || !lease.valid() || state.hasLease() ||
		state.Status != "pending" || state.LastError.Valid {
		t.Fatalf(
			"ambiguous publication did not retain exact V3 lease: state=%+v lease=%+v err=%v",
			state, lease, leaseErr,
		)
	}
}

func TestDNSZoneV2BeginErrorWithExactStatusNilClearsLease(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "begin-error.example")
	agent := &strictDNSRPCAgent{}
	agent.beginHook = func(req *ServiceOperationMutationBeginRequest) error {
		if req.Kind == "dns_zone_sync" {
			return errors.New("simulated Begin rejection")
		}
		return nil
	}
	attachStrictDNSRPCAgent(t, p, agent)

	err := p.syncZoneToDNS(context.Background(), "begin-error.example", false)
	if err == nil || !strings.Contains(err.Error(), "simulated Begin rejection") {
		t.Fatalf("Begin rejection error=%v", err)
	}
	state, readErr := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "begin-error.example")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if state.hasLease() || state.Status != "error" || !state.LastError.Valid {
		t.Fatalf("proven no-job Begin error left stale lease: %+v", state)
	}
	agent.mu.Lock()
	syncCalls := len(agent.syncCalls)
	agent.mu.Unlock()
	if syncCalls != 0 {
		t.Fatalf("Begin rejection touched DNS host state %d times", syncCalls)
	}
}

func TestDNSZoneV2StaleGenerationReleasesAndRetries(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "stale-generation.example")
	var zoneID int64
	if err := p.db.GetDB().QueryRow(`
		SELECT id FROM pdns_domains WHERE name = 'stale-generation.example'`,
	).Scan(&zoneID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio, disabled)
		VALUES (?, 'stale-generation.example', 'TXT', 'before', 300, 0, 0)`, zoneID); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	var hookErr error
	agent := &strictDNSRPCAgent{}
	agent.syncHook = func(_ transport.SyncDNSZoneV2Request) {
		once.Do(func() {
			_, hookErr = p.db.GetDB().Exec(`
				UPDATE pdns_records SET content = 'advanced'
				WHERE domain_id = ? AND type = 'TXT'`, zoneID)
		})
	}
	attachStrictDNSRPCAgent(t, p, agent)

	if err := p.syncZoneToDNS(context.Background(), "stale-generation.example", false); err != nil {
		t.Fatal(err)
	}
	if hookErr != nil {
		t.Fatalf("advance desired generation in flight: %v", hookErr)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV2Request(nil), agent.syncRequests...)
	agent.mu.Unlock()
	if len(requests) != 2 || requests[1].DesiredGeneration <= requests[0].DesiredGeneration {
		t.Fatalf("stale-generation V2 requests=%+v", requests)
	}
	state, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "stale-generation.example")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "applied" || state.AppliedGeneration != state.DesiredGeneration || state.hasLease() {
		t.Fatalf("final stale-generation state=%+v", state)
	}
}

func TestDNSZoneBatchContextOutlivesGenericRecoveryWindow(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	ctx, cancel := dnsZoneBatchContext(parent)
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatalf("DNS batch inherited canceled startup context: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("DNS batch has no bounded deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 40*time.Minute || remaining > dnsZoneSyncBatchTimeout {
		t.Fatalf("DNS batch deadline remaining=%s", remaining)
	}
	if dnsZoneSyncBatchTimeout <= panelMutationRecoveryTimeout {
		t.Fatalf("DNS batch timeout=%s must exceed generic recovery=%s", dnsZoneSyncBatchTimeout, panelMutationRecoveryTimeout)
	}
}

func TestDNSStartupRecoveryAcceptsOnlyExactPersistedActiveChild(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "startup-child.example")
	plan, err := p.prepareDNSZoneSyncPlan(context.Background(), "startup-child.example", false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	observed := &agentMutationJob{
		RequestID:      plan.RequestID,
		OwnerID:        plan.OwnerID,
		Kind:           "dns_zone_sync",
		Target:         plan.Commitment.Domain,
		PackageName:    plan.Commitment.Qualifier,
		Status:         agentMutationRunning,
		LeaseExpiresAt: now.Add(time.Minute),
		DeadlineAt:     now.Add(time.Hour),
	}
	agent := &strictDNSRPCAgent{}
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		plan.RequestID: {
			RequestID:   observed.RequestID,
			OwnerID:     observed.OwnerID,
			Kind:        observed.Kind,
			Target:      observed.Target,
			PackageName: observed.PackageName,
			Status:      agentMutationSucceeded,
			Phase: "commit/dns-zone-sync/v1/published/" +
				observed.RequestID + "/" + observed.Target + "/" + observed.PackageName,
		},
	}
	attachStrictDNSRPCAgent(t, p, agent)

	confused := *observed
	confused.OwnerID = "ffeeddccbbaa99887766554433221100"
	if _, err := p.exactDNSZoneLeaseForJob(context.Background(), &confused); err == nil {
		t.Fatal("confused active DNS child matched a different persisted owner")
	}
	before, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "startup-child.example")
	if err != nil {
		t.Fatal(err)
	}
	if !before.hasLease() {
		t.Fatal("confused child check unexpectedly released the lease")
	}

	p.serviceMutationMu.Lock()
	err = p.recoverDirectDNSZoneSyncLocked(context.Background(), observed)
	p.serviceMutationMu.Unlock()
	if err != nil {
		t.Fatalf("recover exact DNS child: %v", err)
	}
	after, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "startup-child.example")
	if err != nil {
		t.Fatal(err)
	}
	if after.hasLease() || after.Status != "applied" ||
		after.AppliedGeneration != after.DesiredGeneration {
		t.Fatalf("exact startup child state=%+v", after)
	}
}

func TestDNSStartupRecoveryRepairsPendingErrorAndStaleLease(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "startup-stale.example")
	seedStrictDNSZone(t, p, "startup-error.example")
	stale, err := p.prepareDNSZoneSyncPlan(context.Background(), "startup-stale.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if !stale.State.hasLease() {
		t.Fatal("stale startup fixture has no persisted lease")
	}
	if _, err := p.db.GetDB().Exec(`
		UPDATE dns_zone_sync_state
		SET status = 'error', last_error = 'previous publication failed'
		WHERE zone_name = 'startup-error.example'`); err != nil {
		t.Fatal(err)
	}
	agent := &strictDNSRPCAgent{readinessReady: true}
	attachStrictDNSRPCAgent(t, p, agent)

	p.serviceMutationMu.Lock()
	err = p.recoverDNSZoneSyncStateLocked(context.Background())
	p.serviceMutationMu.Unlock()
	if err != nil {
		t.Fatalf("startup DNS state recovery: %v", err)
	}
	for _, zone := range []string{"startup-stale.example", "startup-error.example"} {
		state, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), zone)
		if err != nil {
			t.Fatal(err)
		}
		if state.hasLease() || state.Status != "applied" || state.LastError.Valid ||
			state.AppliedGeneration != state.DesiredGeneration {
			t.Fatalf("recovered startup state %s=%+v", zone, state)
		}
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV2Request(nil), agent.syncRequests...)
	agent.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("startup recovery V2 requests=%+v", requests)
	}
}

func TestDNSStartupDefersLeaseLessPendingWhenPowerDNSIsNotReady(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "startup-deferred.example")
	before, err := readDNSZoneSyncState(
		context.Background(), p.db.GetDB(), "startup-deferred.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if before.hasLease() || before.Status != "pending" {
		t.Fatalf("pending fixture=%+v", before)
	}
	agent := &strictDNSRPCAgent{
		readinessDetail: "PowerDNS is not installed on this server",
	}
	attachStrictDNSRPCAgent(t, p, agent)
	p.serviceMutationMu.Lock()
	err = p.recoverDNSZoneSyncStateLocked(context.Background())
	p.serviceMutationMu.Unlock()
	if err != nil {
		t.Fatalf("defer unavailable PowerDNS: %v", err)
	}
	after, err := readDNSZoneSyncState(
		context.Background(), p.db.GetDB(), "startup-deferred.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if after.hasLease() || after.Status != before.Status ||
		after.DesiredGeneration != before.DesiredGeneration ||
		after.AppliedGeneration != before.AppliedGeneration {
		t.Fatalf("deferred state before=%+v after=%+v", before, after)
	}
	agent.mu.Lock()
	readinessCalls := agent.readinessCalls
	syncCalls := len(agent.syncCalls)
	beginCalls := agent.beginCalls
	agent.mu.Unlock()
	if readinessCalls != 1 || syncCalls != 0 || beginCalls != 0 {
		t.Fatalf(
			"deferred calls readiness=%d sync=%d begin=%d",
			readinessCalls, syncCalls, beginCalls,
		)
	}
}

func TestDNSStartupReadinessAmbiguityIsHardError(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "startup-readiness-error.example")
	agent := &strictDNSRPCAgent{
		readinessError: errors.New("injected readiness transport failure"),
	}
	attachStrictDNSRPCAgent(t, p, agent)
	p.serviceMutationMu.Lock()
	err := p.recoverDNSZoneSyncStateLocked(context.Background())
	p.serviceMutationMu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "read PowerDNS startup readiness") {
		t.Fatalf("readiness ambiguity=%v", err)
	}
	state, readErr := readDNSZoneSyncState(
		context.Background(), p.db.GetDB(), "startup-readiness-error.example",
	)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if state.hasLease() || state.Status != "pending" {
		t.Fatalf("readiness error mutated state=%+v", state)
	}
	agent.mu.Lock()
	syncCalls, beginCalls := len(agent.syncCalls), agent.beginCalls
	agent.mu.Unlock()
	if syncCalls != 0 || beginCalls != 0 {
		t.Fatalf("readiness error sync/begin=%d/%d", syncCalls, beginCalls)
	}
}

func TestDNSStartupLegacyReceiptDefersV3UntilPowerDNSReadiness(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, "startup-exact-before-readiness.example")
	plan, err := p.prepareDNSZoneSyncPlan(
		context.Background(), "startup-exact-before-readiness.example", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	agent := &strictDNSRPCAgent{
		readinessDetail: "PowerDNS is not installed on this server",
	}
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		plan.RequestID: {
			RequestID:   plan.RequestID,
			OwnerID:     plan.OwnerID,
			Kind:        "dns_zone_sync",
			Target:      plan.Commitment.Domain,
			PackageName: plan.Commitment.Qualifier,
			Status:      agentMutationSucceeded,
			Phase: "commit/dns-zone-sync/v1/published/" +
				plan.RequestID + "/" + plan.Commitment.Domain + "/" + plan.Commitment.Qualifier,
		},
	}
	attachStrictDNSRPCAgent(t, p, agent)
	p.serviceMutationMu.Lock()
	err = p.recoverDNSZoneSyncStateLocked(context.Background())
	p.serviceMutationMu.Unlock()
	if err != nil {
		t.Fatalf("exact lease reconcile: %v", err)
	}
	state, err := readDNSZoneSyncState(
		context.Background(), p.db.GetDB(), plan.Commitment.Domain,
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.hasLease() || state.Status != "pending" ||
		state.AppliedGeneration == state.DesiredGeneration {
		t.Fatalf("exact lease state=%+v", state)
	}
	agent.mu.Lock()
	readinessCalls := agent.readinessCalls
	syncCalls := len(agent.syncCalls)
	agent.mu.Unlock()
	if readinessCalls != 1 || syncCalls != 0 {
		t.Fatalf(
			"exact terminal child readiness/sync=%d/%d",
			readinessCalls, syncCalls,
		)
	}
}

func TestDNSStartupDefersLeaseLessPendingForPermanentV2Denial(t *testing.T) {
	tests := []struct {
		name         string
		capabilities []string
		rhel         bool
	}{
		{name: "missing capability", capabilities: []string{}},
		{
			name:         "RHEL policy denial",
			capabilities: []string{transport.AgentCapabilityDNSZoneSyncV2},
			rhel:         true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "standalone")
			seedStrictDNSZone(t, p, "startup-permanent-denial.example")
			capabilities := append([]string(nil), test.capabilities...)
			agent := &strictDNSRPCAgent{
				versionCapabilities: &capabilities,
				readinessReady:      true,
			}
			attachStrictDNSRPCAgent(t, p, agent)
			if test.rhel {
				p.pkgFamilyMu.Lock()
				p.pkgFamilyVal = "dnf"
				p.hostPlatformVal = rhelPolicyTestIdentity()
				p.hostPlatformKnown = true
				p.pkgFamilyMu.Unlock()
			}
			p.serviceMutationMu.Lock()
			err := p.recoverDNSZoneSyncStateLocked(context.Background())
			p.serviceMutationMu.Unlock()
			if err != nil {
				t.Fatalf("permanent denial blocked startup: %v", err)
			}
			state, err := readDNSZoneSyncState(
				context.Background(), p.db.GetDB(),
				"startup-permanent-denial.example",
			)
			if err != nil {
				t.Fatal(err)
			}
			if state.hasLease() || state.Status != "pending" {
				t.Fatalf("permanent denial mutated state=%+v", state)
			}
			agent.mu.Lock()
			readinessCalls := agent.readinessCalls
			syncCalls, beginCalls := len(agent.syncCalls), agent.beginCalls
			agent.mu.Unlock()
			if readinessCalls != 0 || syncCalls != 0 || beginCalls != 0 {
				t.Fatalf(
					"permanent denial readiness/sync/begin=%d/%d/%d",
					readinessCalls, syncCalls, beginCalls,
				)
			}
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

func TestPDNSEnableRejectsActiveBINDWithoutAgentMutation(t *testing.T) {
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgentForEngine(
		t, p, agent, transport.DNSEngineBIND,
	)

	recorder := httptest.NewRecorder()
	p.handlePDNSEnable(
		recorder,
		httptest.NewRequest(http.MethodPost, "/api/v1/pdns/enable", nil),
	)
	if recorder.Code != http.StatusConflict ||
		!strings.Contains(
			recorder.Body.String(),
			`"code":"`+errCodeDNSEngineWorkflowRequired+`"`,
		) {
		t.Fatalf("active BIND PowerDNS enable status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	beginCalls, powerCalls, versionCalls :=
		agent.beginCalls, agent.powerDNSCalls, agent.versionCalls
	agent.mu.Unlock()
	if beginCalls != 0 || powerCalls != 0 || versionCalls != 0 {
		t.Fatalf(
			"active BIND reached PowerDNS mutation path: begin=%d power=%d version=%d",
			beginCalls, powerCalls, versionCalls,
		)
	}
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
	if !strings.Contains(recorder.Body.String(), `"code":"`+errCodeDNSPublicationFailed+`"`) {
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
