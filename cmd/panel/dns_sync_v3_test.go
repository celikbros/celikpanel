package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type dnsZoneV3TestAgent struct {
	durableMutationRPCFixture
	mu                 sync.Mutex
	requests           []transport.SyncDNSZoneV3Request
	v2Requests         []transport.SyncDNSZoneV2Request
	zones              map[string][]transport.ZoneRecord
	publisher          transport.DNSEngine
	responseEpochDelta int64
	syncHook           func(transport.SyncDNSZoneV3Request) error
}

func newDNSZoneV3TestAgent() *dnsZoneV3TestAgent {
	return &dnsZoneV3TestAgent{
		zones:     make(map[string][]transport.ZoneRecord),
		publisher: transport.DNSEngineBIND,
	}
}

func (agent *dnsZoneV3TestAgent) Version(
	_ *transport.Empty,
	response *transport.AgentVersionResponse,
) error {
	response.Capabilities = []string{
		transport.AgentCapabilityDNSZoneSyncV2,
		transport.AgentCapabilityDNSZoneSyncV3,
	}
	return nil
}

func (agent *dnsZoneV3TestAgent) DNSBackendReadiness(
	_ *transport.Empty,
	response *transport.DNSBackendReadinessResponse,
) error {
	publisher := agent.publisher
	response.Engines = []transport.DNSBackendRuntimeState{
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

func (agent *dnsZoneV3TestAgent) SyncDNSZoneV3(
	request *transport.SyncDNSZoneV3Request,
	response *transport.SyncDNSZoneV3Response,
) error {
	copy := *request
	copy.Records = append([]transport.ZoneRecord(nil), request.Records...)
	agent.mu.Lock()
	agent.requests = append(agent.requests, copy)
	if request.Delete {
		delete(agent.zones, request.Domain)
	} else {
		agent.zones[request.Domain] = append(
			[]transport.ZoneRecord(nil), request.Records...,
		)
	}
	hook := agent.syncHook
	delta := agent.responseEpochDelta
	agent.mu.Unlock()
	response.Synced = true
	response.Engine = request.Engine
	response.EngineEpoch = request.EngineEpoch + delta
	response.AppliedGeneration = request.DesiredGeneration
	if hook != nil {
		return hook(copy)
	}
	return nil
}

func (agent *dnsZoneV3TestAgent) SyncDNSZoneV2(
	request *transport.SyncDNSZoneV2Request,
	response *transport.SyncDNSZoneV2Response,
) error {
	agent.mu.Lock()
	agent.v2Requests = append(agent.v2Requests, *request)
	agent.mu.Unlock()
	response.Synced = true
	response.AppliedGeneration = request.DesiredGeneration
	return nil
}

func (agent *dnsZoneV3TestAgent) FinishServiceMutation(
	request *ServiceOperationMutationFinishRequest,
	response *ServiceOperationMutationResponse,
) error {
	agent.durableMutationRPCFixture.mu.Lock()
	job := agent.durableMutationRPCFixture.jobs[request.RequestID]
	if job != nil && !agentMutationActive(job.Status) {
		response.Job = cloneServiceOperationMutationJob(job)
		agent.durableMutationRPCFixture.mu.Unlock()
		return nil
	}
	agent.durableMutationRPCFixture.mu.Unlock()
	return agent.durableMutationRPCFixture.FinishServiceMutation(request, response)
}

func (agent *dnsZoneV3TestAgent) ServiceMutationStatus(
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

func attachDNSZoneV3TestAgent(
	t *testing.T,
	panel *Panel,
	agent *dnsZoneV3TestAgent,
) {
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
	rawClient, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(
		rawClient, connector,
	)
	t.Cleanup(func() { _ = rawClient.Close() })
}

func setDNSZoneV3SucceededReceipt(
	t *testing.T,
	agent *dnsZoneV3TestAgent,
	lease dnsZoneEngineLease,
) {
	t.Helper()
	agent.durableMutationRPCFixture.mu.Lock()
	defer agent.durableMutationRPCFixture.mu.Unlock()
	if agent.durableMutationRPCFixture.jobs == nil {
		agent.durableMutationRPCFixture.jobs = make(map[string]*ServiceOperationMutationJob)
	}
	agent.durableMutationRPCFixture.jobs[lease.RequestID] = &ServiceOperationMutationJob{
		RequestID: lease.RequestID, OwnerID: lease.OwnerID,
		Kind: "dns_zone_sync", Target: lease.ZoneName,
		PackageName: lease.Qualifier, Status: agentMutationSucceeded,
		Phase: "commit/dns-zone-sync/v3/published/" + lease.RequestID + "/" +
			lease.ZoneName + "/" + lease.Qualifier,
	}
}

func seedV3ZoneRecord(
	t *testing.T,
	panel *Panel,
	domain string,
	recordType string,
	content string,
) {
	t.Helper()
	var zoneID int64
	if err := panel.db.GetDB().QueryRow(
		`SELECT id FROM pdns_domains WHERE name = ?`, domain,
	).Scan(&zoneID); err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO pdns_records (
		  domain_id, name, type, content, ttl, prio, disabled
		) VALUES (?, ?, ?, ?, 300, 0, 0)`,
		zoneID, domain, recordType, content,
	); err != nil {
		t.Fatal(err)
	}
}

func switchDNSEngineIdentityForTest(
	t *testing.T,
	panel *Panel,
	target transport.DNSEngine,
) {
	t.Helper()
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	nextEpoch := state.EngineEpoch + 1
	switchID := fmt.Sprintf("%032x", nextEpoch)
	requestID := fmt.Sprintf("%032x", nextEpoch+100)
	ownerID := fmt.Sprintf("%032x", nextEpoch+200)
	var source any
	if state.ActiveEngine != "" {
		source = state.ActiveEngine
	}
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO dns_engine_switch_snapshots (
		  switch_id, request_id, owner_id, mode, source_engine, target_engine,
		  source_epoch, target_epoch, source_state_revision, topology,
		  phase, manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, 'switch', ?, ?, ?, ?, ?, 'standalone', 'planned', ?, 0, 0)`,
		switchID, requestID, ownerID, source, target,
		state.EngineEpoch, nextEpoch, state.Revision,
		"dns-engine-switch/v1:sha256:"+strings.Repeat("d", 64),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(`
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = revision + 1
		WHERE singleton_id = 1`, switchID); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{
		"staging", "staged", "activating", "verifying", "committed",
	} {
		if _, err := panel.db.GetDB().Exec(`
			UPDATE dns_engine_switch_snapshots SET phase = ?
			WHERE switch_id = ?`, phase, switchID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := panel.db.GetDB().Exec(`
		UPDATE dns_engine_state
		SET active_engine = ?, active_epoch = ?, current_switch_id = NULL,
		    revision = revision + 1
		WHERE singleton_id = 1 AND current_switch_id = ?`,
		target, nextEpoch, switchID,
	); err != nil {
		t.Fatal(err)
	}
}

func TestDNSZoneLegacyV2SuccessCannotCrossPowerDNSEngineEpoch(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	seedStrictDNSZone(t, panel, "legacy-cross-epoch.example")
	legacy, err := panel.prepareDNSZoneSyncPlan(
		context.Background(), "legacy-cross-epoch.example", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	initialApplied := legacy.State.AppliedGeneration

	switchDNSEngineIdentityForTest(t, panel, transport.DNSEnginePowerDNS)
	switchDNSEngineIdentityForTest(t, panel, transport.DNSEngineBIND)
	switchDNSEngineIdentityForTest(t, panel, transport.DNSEnginePowerDNS)

	exact, err := panel.recordDNSZoneSyncSuccess(
		context.Background(), legacy.State,
	)
	if err != nil {
		t.Fatal(err)
	}
	if exact {
		t.Fatal("epoch-free V2 receipt was accepted as current PowerDNS authority")
	}
	state, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "legacy-cross-epoch.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.hasLease() || state.Status != "pending" ||
		state.AppliedGeneration != initialApplied {
		t.Fatalf("stale V2 receipt changed applied state: %+v", state)
	}
	engineState, err := readDNSEngineDBState(
		context.Background(), panel.db.GetDB(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if engineState.ActiveEngine != transport.DNSEnginePowerDNS ||
		engineState.EngineEpoch != 3 {
		t.Fatalf("unexpected final engine authority: %+v", engineState)
	}
}

func TestDNSZoneV3BINDCreateUpdateDeletePreservesOtherZones(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "bind-publish.example")
	seedStrictDNSZone(t, panel, "unrelated.example")
	seedV3ZoneRecord(t, panel, "bind-publish.example", "A", "192.0.2.20")
	seedV3ZoneRecord(t, panel, "unrelated.example", "TXT", "keep-me")

	agent := newDNSZoneV3TestAgent()
	unrelatedBefore := []transport.ZoneRecord{{
		Name: "unrelated.example", Type: "TXT", Content: "agent-owned-copy", TTL: 300,
	}}
	agent.zones["unrelated.example"] = append(
		[]transport.ZoneRecord(nil), unrelatedBefore...,
	)
	attachDNSZoneV3TestAgent(t, panel, agent)

	if err := panel.syncZoneToDNS(
		context.Background(), "bind-publish.example", false,
	); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	unrelatedAfterCreate := append(
		[]transport.ZoneRecord(nil), agent.zones["unrelated.example"]...,
	)
	agent.mu.Unlock()
	if len(requests) != 1 || requests[0].Engine != transport.DNSEngineBIND ||
		requests[0].EngineEpoch != 1 || requests[0].Delete ||
		len(requests[0].Records) < 4 {
		t.Fatalf("initial BIND V3 request=%+v", requests)
	}
	if !reflect.DeepEqual(unrelatedAfterCreate, unrelatedBefore) {
		t.Fatalf("unrelated agent zone changed: got=%+v want=%+v",
			unrelatedAfterCreate, unrelatedBefore)
	}

	var appliedEngine, appliedAction, qualifier string
	var appliedEpoch, appliedGeneration, revision int64
	if err := panel.db.GetDB().QueryRow(`
		SELECT engine, engine_epoch, applied_generation,
		       applied_action, qualifier, revision
		FROM dns_zone_engine_applications
		WHERE zone_name = 'bind-publish.example' AND engine = 'bind'`).Scan(
		&appliedEngine, &appliedEpoch, &appliedGeneration,
		&appliedAction, &qualifier, &revision,
	); err != nil {
		t.Fatal(err)
	}
	if appliedEngine != "bind" || appliedEpoch != 1 || appliedAction != "sync" ||
		appliedGeneration != requests[0].DesiredGeneration || revision != 1 ||
		qualifier == "" {
		t.Fatalf("initial BIND application engine=%s epoch=%d generation=%d action=%s revision=%d qualifier=%q",
			appliedEngine, appliedEpoch, appliedGeneration, appliedAction, revision, qualifier)
	}

	if _, err := panel.db.GetDB().Exec(`
		UPDATE pdns_records SET content = '192.0.2.21'
		WHERE name = 'bind-publish.example' AND type = 'A'`); err != nil {
		t.Fatal(err)
	}
	if err := panel.syncZoneToDNS(
		context.Background(), "bind-publish.example", false,
	); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	requests = append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	agent.mu.Unlock()
	if len(requests) != 2 ||
		requests[1].DesiredGeneration <= requests[0].DesiredGeneration {
		t.Fatalf("updated BIND V3 requests=%+v", requests)
	}
	if err := panel.db.GetDB().QueryRow(`
		SELECT applied_generation, revision
		FROM dns_zone_engine_applications
		WHERE zone_name = 'bind-publish.example' AND engine = 'bind'`).Scan(
		&appliedGeneration, &revision,
	); err != nil {
		t.Fatal(err)
	}
	if appliedGeneration != requests[1].DesiredGeneration || revision != 2 {
		t.Fatalf("updated BIND application generation=%d revision=%d",
			appliedGeneration, revision)
	}

	if _, err := panel.db.GetDB().Exec(
		`DELETE FROM pdns_domains WHERE name = 'bind-publish.example'`,
	); err != nil {
		t.Fatal(err)
	}
	if err := panel.syncZoneToDNS(
		context.Background(), "bind-publish.example", true,
	); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	requests = append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	_, targetRemains := agent.zones["bind-publish.example"]
	unrelatedAfterDelete := append(
		[]transport.ZoneRecord(nil), agent.zones["unrelated.example"]...,
	)
	agent.mu.Unlock()
	if len(requests) != 3 || !requests[2].Delete || len(requests[2].Records) != 0 ||
		targetRemains {
		t.Fatalf("delete BIND V3 requests=%+v targetRemains=%v", requests, targetRemains)
	}
	if !reflect.DeepEqual(unrelatedAfterDelete, unrelatedBefore) {
		t.Fatalf("unrelated agent zone changed after delete: got=%+v want=%+v",
			unrelatedAfterDelete, unrelatedBefore)
	}
	var markerCount, stateCount, leaseCount int
	if err := panel.db.GetDB().QueryRow(`
		SELECT
		 (SELECT count(*) FROM dns_zone_deletion_markers
		   WHERE zone_name = 'bind-publish.example'),
		 (SELECT count(*) FROM dns_zone_sync_state
		   WHERE zone_name = 'bind-publish.example'),
		 (SELECT count(*) FROM dns_zone_engine_leases
		   WHERE zone_name = 'bind-publish.example')`).Scan(
		&markerCount, &stateCount, &leaseCount,
	); err != nil {
		t.Fatal(err)
	}
	if markerCount != 0 || stateCount != 0 || leaseCount != 0 {
		t.Fatalf("retired BIND delete marker=%d state=%d lease=%d",
			markerCount, stateCount, leaseCount)
	}
	if err := panel.db.GetDB().QueryRow(`
		SELECT applied_generation, applied_action, revision
		FROM dns_zone_engine_applications
		WHERE zone_name = 'bind-publish.example' AND engine = 'bind'`).Scan(
		&appliedGeneration, &appliedAction, &revision,
	); err != nil {
		t.Fatal(err)
	}
	if appliedAction != "delete" ||
		appliedGeneration != requests[2].DesiredGeneration || revision != 3 {
		t.Fatalf("delete application generation=%d action=%s revision=%d",
			appliedGeneration, appliedAction, revision)
	}
}

func TestDNSZoneV3RejectsStaleEngineEpochBeforeSnapshotMutation(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "stale-epoch.example")
	stateBefore, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "stale-epoch.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	var soaBefore string
	if err := panel.db.GetDB().QueryRow(`
		SELECT r.content FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = 'stale-epoch.example' AND r.type = 'SOA'`).Scan(
		&soaBefore,
	); err != nil {
		t.Fatal(err)
	}

	_, err = panel.prepareDNSZoneSyncV3Plan(
		context.Background(), "stale-epoch.example", false,
		dnsPublisherIdentity{Engine: transport.DNSEngineBIND, Epoch: 2},
	)
	if err == nil || !strings.Contains(err.Error(), "authority changed") {
		t.Fatalf("stale engine epoch preparation error=%v", err)
	}
	stateAfter, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "stale-epoch.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	var soaAfter string
	if err := panel.db.GetDB().QueryRow(`
		SELECT r.content FROM pdns_records r
		JOIN pdns_domains d ON d.id = r.domain_id
		WHERE d.name = 'stale-epoch.example' AND r.type = 'SOA'`).Scan(
		&soaAfter,
	); err != nil {
		t.Fatal(err)
	}
	var leaseCount int
	if err := panel.db.GetDB().QueryRow(`
		SELECT count(*) FROM dns_zone_engine_leases
		WHERE zone_name = 'stale-epoch.example'`).Scan(&leaseCount); err != nil {
		t.Fatal(err)
	}
	if stateAfter.DesiredGeneration != stateBefore.DesiredGeneration ||
		soaAfter != soaBefore || leaseCount != 0 {
		t.Fatalf("stale epoch mutated state generation %d->%d SOA %q->%q leases=%d",
			stateBefore.DesiredGeneration, stateAfter.DesiredGeneration,
			soaBefore, soaAfter, leaseCount)
	}
}

func TestDNSZoneV3ResponseLossUsesExactSucceededReceipt(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "response-loss-v3.example")
	agent := newDNSZoneV3TestAgent()
	agent.syncHook = func(request transport.SyncDNSZoneV3Request) error {
		agent.durableMutationRPCFixture.mu.Lock()
		job := agent.durableMutationRPCFixture.jobs[request.MutationRequestID]
		if job == nil || job.OwnerID != request.MutationOwnerID {
			agent.durableMutationRPCFixture.mu.Unlock()
			return errors.New("response-loss fixture lost the exact V3 job")
		}
		job.Status = agentMutationSucceeded
		job.Phase = "commit/dns-zone-sync/v3/published/" + job.RequestID + "/" +
			job.Target + "/" + job.PackageName
		if agent.durableMutationRPCFixture.active == job.RequestID {
			agent.durableMutationRPCFixture.active = ""
		}
		agent.durableMutationRPCFixture.mu.Unlock()
		return errors.New("injected response loss after exact BIND publication")
	}
	attachDNSZoneV3TestAgent(t, panel, agent)

	if err := panel.syncZoneToDNS(
		context.Background(), "response-loss-v3.example", false,
	); err != nil {
		t.Fatalf("exact succeeded receipt did not reconcile response loss: %v", err)
	}
	state, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "response-loss-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "applied" ||
		state.AppliedGeneration != state.DesiredGeneration {
		t.Fatalf("response-loss V3 state=%+v", state)
	}
	var applications, leases int
	if err := panel.db.GetDB().QueryRow(`
		SELECT
		 (SELECT count(*) FROM dns_zone_engine_applications
		   WHERE zone_name = 'response-loss-v3.example' AND engine = 'bind'),
		 (SELECT count(*) FROM dns_zone_engine_leases
		   WHERE zone_name = 'response-loss-v3.example')`).Scan(
		&applications, &leases,
	); err != nil {
		t.Fatal(err)
	}
	if applications != 1 || leases != 0 {
		t.Fatalf("response-loss applications=%d leases=%d", applications, leases)
	}
}

func TestDNSZoneV3StartupRecoveryFinalizesPersistedSucceededJob(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "startup-recovery-v3.example")
	agent := newDNSZoneV3TestAgent()
	attachDNSZoneV3TestAgent(t, panel, agent)
	plan, err := panel.prepareDNSZoneSyncV3Plan(
		context.Background(), "startup-recovery-v3.example", false,
		dnsPublisherIdentity{Engine: transport.DNSEngineBIND, Epoch: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	setDNSZoneV3SucceededReceipt(t, agent, plan.Lease)

	if err := panel.recoverDNSZoneSyncStateLocked(context.Background()); err != nil {
		t.Fatalf("recover persisted V3 success: %v", err)
	}
	state, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "startup-recovery-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "applied" ||
		state.AppliedGeneration != plan.Lease.DesiredGeneration {
		t.Fatalf("startup-recovered V3 state=%+v", state)
	}
	var applicationRequest string
	var leaseCount int
	if err := panel.db.GetDB().QueryRow(`
		SELECT mutation_request_id,
		       (SELECT count(*) FROM dns_zone_engine_leases
		        WHERE zone_name = 'startup-recovery-v3.example')
		FROM dns_zone_engine_applications
		WHERE zone_name = 'startup-recovery-v3.example' AND engine = 'bind'`).Scan(
		&applicationRequest, &leaseCount,
	); err != nil {
		t.Fatal(err)
	}
	if applicationRequest != plan.Lease.RequestID || leaseCount != 0 {
		t.Fatalf("startup application request=%s leaseCount=%d want=%s/0",
			applicationRequest, leaseCount, plan.Lease.RequestID)
	}
}

func TestDNSZoneV3DesiredGenerationAdvanceRetriesWithFreshCAS(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "advance-v3.example")
	seedV3ZoneRecord(t, panel, "advance-v3.example", "TXT", "before")
	agent := newDNSZoneV3TestAgent()
	var once sync.Once
	var hookErr error
	agent.syncHook = func(_ transport.SyncDNSZoneV3Request) error {
		once.Do(func() {
			_, hookErr = panel.db.GetDB().Exec(`
				UPDATE pdns_records SET content = 'advanced'
				WHERE name = 'advance-v3.example' AND type = 'TXT'`)
		})
		return nil
	}
	attachDNSZoneV3TestAgent(t, panel, agent)

	if err := panel.syncZoneToDNS(
		context.Background(), "advance-v3.example", false,
	); err != nil {
		t.Fatal(err)
	}
	if hookErr != nil {
		t.Fatalf("advance desired V3 generation in flight: %v", hookErr)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	agent.mu.Unlock()
	if len(requests) != 2 ||
		requests[1].DesiredGeneration <= requests[0].DesiredGeneration {
		t.Fatalf("advanced V3 requests=%+v", requests)
	}
	state, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "advance-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	var appliedGeneration, revision int64
	if err := panel.db.GetDB().QueryRow(`
		SELECT applied_generation, revision
		FROM dns_zone_engine_applications
		WHERE zone_name = 'advance-v3.example' AND engine = 'bind'`).Scan(
		&appliedGeneration, &revision,
	); err != nil {
		t.Fatal(err)
	}
	if state.Status != "applied" ||
		state.AppliedGeneration != state.DesiredGeneration ||
		appliedGeneration != requests[1].DesiredGeneration || revision != 2 {
		t.Fatalf("advanced state=%+v application generation=%d revision=%d",
			state, appliedGeneration, revision)
	}
}

func TestDNSEngineSwitchCannotAttachWhileV3ZoneLeaseExists(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "lease-blocks-switch.example")
	plan, err := panel.prepareDNSZoneSyncV3Plan(
		context.Background(), "lease-blocks-switch.example", false,
		dnsPublisherIdentity{Engine: transport.DNSEngineBIND, Epoch: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := panel.db.GetDB().QueryRow(`
		SELECT revision FROM dns_engine_state WHERE singleton_id = 1`).Scan(
		&revision,
	); err != nil {
		t.Fatal(err)
	}
	switchID := strings.Repeat("e", 32)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO dns_engine_switch_snapshots (
		  switch_id, request_id, owner_id, mode, source_engine, target_engine,
		  source_epoch, target_epoch, source_state_revision, topology,
		  phase, manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, 'switch', 'bind', 'pdns', 1, 2, ?, 'standalone',
		          'planned', ?, 0, 0)`,
		switchID, strings.Repeat("f", 32), strings.Repeat("0", 32),
		revision, "dns-engine-switch/v1:sha256:"+strings.Repeat("1", 64),
	); err != nil {
		t.Fatal(err)
	}
	_, err = panel.db.GetDB().Exec(`
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = revision + 1
		WHERE singleton_id = 1 AND current_switch_id IS NULL`, switchID)
	if err == nil || !strings.Contains(err.Error(), "active publication authority") {
		t.Fatalf("engine switch attached over V3 lease: %v", err)
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSwitchID != "" || state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 1 || state.Revision != revision {
		t.Fatalf("engine state changed over V3 lease: %+v", state)
	}
	lease, err := readDNSZoneEngineLease(
		context.Background(), panel.db.GetDB(), "lease-blocks-switch.example",
	)
	if err != nil || lease.RequestID != plan.Lease.RequestID {
		t.Fatalf("exact V3 lease changed after blocked switch: lease=%+v err=%v", lease, err)
	}
}

func TestDNSZonePublisherDispatchBindsPowerDNSToV3Epoch(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	seedStrictDNSZone(t, panel, "pdns-stays-v2.example")
	activateDNSEngineForTest(t, panel, string(transport.DNSEnginePowerDNS))
	agent := newDNSZoneV3TestAgent()
	agent.publisher = transport.DNSEnginePowerDNS
	attachDNSZoneV3TestAgent(t, panel, agent)

	if err := panel.syncZoneToDNS(
		context.Background(), "pdns-stays-v2.example", false,
	); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	v2Requests := append([]transport.SyncDNSZoneV2Request(nil), agent.v2Requests...)
	agent.mu.Unlock()
	if len(requests) != 1 || requests[0].Domain != "pdns-stays-v2.example" ||
		requests[0].Engine != transport.DNSEnginePowerDNS ||
		requests[0].EngineEpoch != 1 {
		t.Fatalf("PowerDNS dispatch requests=%+v", requests)
	}
	if len(v2Requests) != 0 {
		t.Fatalf("active PowerDNS used epoch-free V2: %+v", v2Requests)
	}
	var engineLeaseCount, applicationCount int
	if err := panel.db.GetDB().QueryRow(`
		SELECT
		 (SELECT count(*) FROM dns_zone_engine_leases
		   WHERE zone_name = 'pdns-stays-v2.example'),
		 (SELECT count(*) FROM dns_zone_engine_applications
		   WHERE zone_name = 'pdns-stays-v2.example')`).Scan(
		&engineLeaseCount, &applicationCount,
	); err != nil {
		t.Fatal(err)
	}
	if engineLeaseCount != 0 || applicationCount != 1 {
		t.Fatalf("PowerDNS V3 state: leases=%d applications=%d",
			engineLeaseCount, applicationCount)
	}
}
