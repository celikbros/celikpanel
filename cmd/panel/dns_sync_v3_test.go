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
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type dnsZoneV3TestAgent struct {
	durableMutationRPCFixture
	verifiedAPTAgentRPCFixture

	mu                     sync.Mutex
	requests               []transport.SyncDNSZoneV3Request
	recoverRequests        []transport.RecoverDNSZoneV3Request
	v2Requests             []transport.SyncDNSZoneV2Request
	zones                  map[string][]transport.ZoneRecord
	publisher              transport.DNSEngine
	pairReady              bool
	responseEpochDelta     int64
	syncHook               func(transport.SyncDNSZoneV3Request) error
	syncResponseHook       func(transport.SyncDNSZoneV3Request, *transport.SyncDNSZoneV3Response) error
	recoverHook            func(transport.RecoverDNSZoneV3Request, *transport.RecoverDNSZoneV3Response) error
	resumeBeginHook        func(transport.ServiceMutationBeginRequest) error
	statusHook             func(string, *ServiceOperationMutationJob)
	expireRecoveryOnStatus bool
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
		transport.AgentCapabilityDNSZoneRecoverV1,
	}
	return nil
}

func (agent *dnsZoneV3TestAgent) DNSBackendReadiness(
	_ *transport.Empty,
	response *transport.DNSBackendReadinessResponse,
) error {
	agent.mu.Lock()
	publisher := agent.publisher
	pairReady := agent.pairReady
	agent.mu.Unlock()
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
			PairReady: publisher == transport.DNSEngineBIND && pairReady,
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
	responseHook := agent.syncResponseHook
	delta := agent.responseEpochDelta
	agent.mu.Unlock()
	response.Synced = true
	response.Engine = request.Engine
	response.EngineEpoch = request.EngineEpoch + delta
	response.AppliedGeneration = request.DesiredGeneration
	if responseHook != nil {
		if err := responseHook(copy, response); err != nil {
			return err
		}
	}
	if hook != nil {
		return hook(copy)
	}
	return nil
}

func (agent *dnsZoneV3TestAgent) BeginServiceMutation(
	request *ServiceOperationMutationBeginRequest,
	response *ServiceOperationMutationResponse,
) error {
	if !request.Resume {
		return agent.durableMutationRPCFixture.BeginServiceMutation(request, response)
	}
	agent.durableMutationRPCFixture.mu.Lock()
	job := agent.durableMutationRPCFixture.jobs[request.RequestID]
	identity := agentMutationIdentity{
		RequestID: request.RequestID, OwnerID: request.OwnerID,
		Kind: request.Kind, Target: request.Target,
		PackageName: request.PackageName,
	}
	phase, phaseErr := dnsZoneSyncV3RecoveringPhase(identity)
	if job == nil || phaseErr != nil || job.OwnerID != request.OwnerID ||
		job.Kind != request.Kind || job.Target != request.Target ||
		job.PackageName != request.PackageName || job.Status != agentMutationPending {
		response.Error = "only the exact pending DNS V3 mutation may resume"
		response.Job = cloneServiceOperationMutationJob(job)
		agent.durableMutationRPCFixture.mu.Unlock()
		return nil
	}
	now := time.Now().UTC()
	job.Status = agentMutationRunning
	job.Phase = phase
	job.Attempt++
	job.LeaseExpiresAt = now.Add(time.Minute)
	job.DeadlineAt = now.Add(time.Hour)
	agent.durableMutationRPCFixture.active = job.RequestID
	response.Job = cloneServiceOperationMutationJob(job)
	hook := agent.resumeBeginHook
	agent.durableMutationRPCFixture.mu.Unlock()
	if hook != nil {
		return hook(transport.ServiceMutationBeginRequest(*request))
	}
	return nil
}

func (agent *dnsZoneV3TestAgent) markDNSZoneV3Pending(
	requestID, ownerID, domain, qualifier string,
) error {
	agent.durableMutationRPCFixture.mu.Lock()
	defer agent.durableMutationRPCFixture.mu.Unlock()
	job := agent.durableMutationRPCFixture.jobs[requestID]
	identity := agentMutationIdentity{
		RequestID: requestID, OwnerID: ownerID, Kind: "dns_zone_sync",
		Target: domain, PackageName: qualifier,
	}
	phase, err := dnsZoneSyncV3PendingPhase(identity)
	if err != nil || job == nil || job.OwnerID != ownerID ||
		job.Kind != identity.Kind || job.Target != domain ||
		job.PackageName != qualifier {
		return errors.New("pending fixture lost exact DNS V3 identity")
	}
	job.Status = agentMutationPending
	job.Phase = phase
	job.LeaseExpiresAt = time.Time{}
	if agent.durableMutationRPCFixture.active == requestID {
		agent.durableMutationRPCFixture.active = ""
	}
	return nil
}

func (agent *dnsZoneV3TestAgent) markDNSZoneV3Succeeded(
	requestID, ownerID, domain, qualifier string,
) error {
	agent.durableMutationRPCFixture.mu.Lock()
	defer agent.durableMutationRPCFixture.mu.Unlock()
	job := agent.durableMutationRPCFixture.jobs[requestID]
	if job == nil || job.OwnerID != ownerID || job.Kind != "dns_zone_sync" ||
		job.Target != domain || job.PackageName != qualifier {
		return errors.New("success fixture lost exact DNS V3 identity")
	}
	job.Status = agentMutationSucceeded
	job.Phase = "commit/dns-zone-sync/v3/published/" + requestID + "/" +
		domain + "/" + qualifier
	job.LeaseExpiresAt = time.Time{}
	if agent.durableMutationRPCFixture.active == requestID {
		agent.durableMutationRPCFixture.active = ""
	}
	return nil
}

func (agent *dnsZoneV3TestAgent) RecoverDNSZoneV3(
	request *transport.RecoverDNSZoneV3Request,
	response *transport.RecoverDNSZoneV3Response,
) error {
	copy := *request
	agent.mu.Lock()
	agent.recoverRequests = append(agent.recoverRequests, copy)
	hook := agent.recoverHook
	agent.mu.Unlock()
	if hook != nil {
		return hook(copy, response)
	}
	if err := agent.markDNSZoneV3Succeeded(
		request.MutationRequestID, request.MutationOwnerID,
		request.Domain, request.Qualifier,
	); err != nil {
		response.Error = err.Error()
		return nil
	}
	response.Recovered = true
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
	job := agent.durableMutationRPCFixture.jobs[requestID]
	if agent.expireRecoveryOnStatus && job != nil &&
		agentMutationActive(job.Status) {
		identity := agentMutationIdentity{
			RequestID: job.RequestID, OwnerID: job.OwnerID, Kind: job.Kind,
			Target: job.Target, PackageName: job.PackageName,
		}
		if phase, err := dnsZoneSyncV3PendingPhase(identity); err == nil {
			job.Status = agentMutationPending
			job.Phase = phase
			job.LeaseExpiresAt = time.Time{}
			agent.durableMutationRPCFixture.active = ""
			agent.expireRecoveryOnStatus = false
		}
	}
	if agent.statusHook != nil {
		agent.statusHook(requestID, job)
	}
	response.Job = cloneServiceOperationMutationJob(job)
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

func setDNSZoneV3PendingReceipt(
	t *testing.T,
	agent *dnsZoneV3TestAgent,
	lease dnsZoneEngineLease,
) {
	t.Helper()
	phase, err := dnsZoneSyncV3PendingPhase(lease.identity())
	if err != nil {
		t.Fatal(err)
	}
	agent.durableMutationRPCFixture.mu.Lock()
	defer agent.durableMutationRPCFixture.mu.Unlock()
	if agent.durableMutationRPCFixture.jobs == nil {
		agent.durableMutationRPCFixture.jobs = make(map[string]*ServiceOperationMutationJob)
	}
	agent.durableMutationRPCFixture.jobs[lease.RequestID] = &ServiceOperationMutationJob{
		RequestID: lease.RequestID, OwnerID: lease.OwnerID,
		Kind: "dns_zone_sync", Target: lease.ZoneName,
		PackageName: lease.Qualifier, Status: agentMutationPending,
		Phase: phase, Attempt: 1,
	}
}

func configureDNSZoneV3PendingSync(agent *dnsZoneV3TestAgent, loseResponse bool) {
	agent.syncResponseHook = func(
		request transport.SyncDNSZoneV3Request,
		response *transport.SyncDNSZoneV3Response,
	) error {
		agent.durableMutationRPCFixture.mu.Lock()
		job := agent.durableMutationRPCFixture.jobs[request.MutationRequestID]
		qualifier := ""
		if job != nil {
			qualifier = job.PackageName
		}
		agent.durableMutationRPCFixture.mu.Unlock()
		if err := agent.markDNSZoneV3Pending(
			request.MutationRequestID, request.MutationOwnerID,
			request.Domain, qualifier,
		); err != nil {
			return err
		}
		*response = transport.SyncDNSZoneV3Response{
			RecoveryPending:   true,
			Engine:            request.Engine,
			EngineEpoch:       request.EngineEpoch,
			AppliedGeneration: request.DesiredGeneration,
		}
		if loseResponse {
			return errors.New("injected response loss after durable pending receipt")
		}
		return nil
	}
}

func configureDNSZoneV3PendingRecovery(agent *dnsZoneV3TestAgent) {
	agent.recoverHook = func(
		request transport.RecoverDNSZoneV3Request,
		response *transport.RecoverDNSZoneV3Response,
	) error {
		if err := agent.markDNSZoneV3Pending(
			request.MutationRequestID, request.MutationOwnerID,
			request.Domain, request.Qualifier,
		); err != nil {
			response.Error = err.Error()
			return nil
		}
		response.RecoveryPending = true
		return nil
	}
}

func activatePairedPrimaryBINDForV3Test(t *testing.T, panel *Panel) {
	t.Helper()
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	setDNSIdentityForTest(t, panel, "paired")
	if err := panel.setSetting(
		context.Background(), settingDNSPeerIP, "192.0.2.20",
	); err != nil {
		t.Fatal(err)
	}
	if err := panel.setSetting(
		context.Background(), settingDNSPeerNS, "ns2.celikhost.com",
	); err != nil {
		t.Fatal(err)
	}
	engineAgent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, engineAgent)
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != 200 || len(preview.Blockers) != 0 {
		t.Fatalf("paired BIND preview status=%d preview=%+v body=%s",
			recorder.Code, preview, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("7", 32), transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != 200 {
		t.Fatalf("paired BIND commit status=%d body=%s", commit.Code, commit.Body.String())
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
	deleteGeneration := requests[2].DesiredGeneration
	deleteRequestID := requests[2].MutationRequestID
	deleteOwnerID := requests[2].MutationOwnerID
	deleteZoneType := requests[2].ZoneType
	deleteQualifier := qualifier

	seedStrictDNSZone(t, panel, "bind-publish.example")
	seedV3ZoneRecord(t, panel, "bind-publish.example", "A", "192.0.2.30")
	if err := panel.syncZoneToDNS(
		context.Background(), "bind-publish.example", false,
	); err != nil {
		t.Fatalf("republish same-name zone after applied deletion: %v", err)
	}
	agent.mu.Lock()
	requests = append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	_, recreatedOnAgent := agent.zones["bind-publish.example"]
	agent.mu.Unlock()
	if len(requests) != 4 || requests[3].Delete || !recreatedOnAgent ||
		requests[3].DesiredGeneration <= deleteGeneration {
		t.Fatalf("resurrected BIND V3 requests=%+v recreated=%v delete_generation=%d",
			requests, recreatedOnAgent, deleteGeneration)
	}
	if err := panel.db.GetDB().QueryRow(`
		SELECT applied_generation, applied_action, qualifier, revision
		FROM dns_zone_engine_applications
		WHERE zone_name = 'bind-publish.example' AND engine = 'bind'`).Scan(
		&appliedGeneration, &appliedAction, &qualifier, &revision,
	); err != nil {
		t.Fatal(err)
	}
	if appliedAction != "sync" ||
		appliedGeneration != requests[3].DesiredGeneration || revision != 4 {
		t.Fatalf("resurrected application generation=%d action=%s revision=%d",
			appliedGeneration, appliedAction, revision)
	}
	staleDelete := dnsZoneEngineLease{
		ZoneName: "bind-publish.example", Engine: transport.DNSEngineBIND,
		EngineEpoch: 1, RequestID: deleteRequestID, OwnerID: deleteOwnerID,
		DesiredGeneration: deleteGeneration, DesiredAction: "delete",
		DesiredZoneType: deleteZoneType, Qualifier: deleteQualifier,
		ExpiresAt: "2099-01-01T00:00:00Z",
	}
	if exact, err := panel.recordDNSZoneSyncV3Success(
		context.Background(), staleDelete,
	); err == nil || exact {
		t.Fatalf("stale delete receipt exact=%v err=%v", exact, err)
	}
	if err := panel.db.GetDB().QueryRow(`
		SELECT applied_generation, applied_action, revision
		FROM dns_zone_engine_applications
		WHERE zone_name = 'bind-publish.example' AND engine = 'bind'`).Scan(
		&appliedGeneration, &appliedAction, &revision,
	); err != nil {
		t.Fatal(err)
	}
	if appliedGeneration != requests[3].DesiredGeneration ||
		appliedAction != "sync" || revision != 4 {
		t.Fatalf("stale delete changed application generation=%d action=%s revision=%d",
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

func TestDNSZoneV3PendingAndLostPendingResponseRecoverExactLease(t *testing.T) {
	for _, test := range []struct {
		name         string
		loseResponse bool
	}{
		{name: "pending response"},
		{name: "pending response lost", loseResponse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			panel := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, panel, "standalone")
			activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
			seedStrictDNSZone(t, panel, "pending-recover-v3.example")
			agent := newDNSZoneV3TestAgent()
			configureDNSZoneV3PendingSync(agent, test.loseResponse)
			var observedLease dnsZoneEngineLease
			var observedLeaseErr error
			agent.recoverHook = func(
				request transport.RecoverDNSZoneV3Request,
				response *transport.RecoverDNSZoneV3Response,
			) error {
				observedLease, observedLeaseErr = readDNSZoneEngineLease(
					context.Background(), panel.db.GetDB(), request.Domain,
				)
				if observedLeaseErr != nil {
					return observedLeaseErr
				}
				if err := agent.markDNSZoneV3Succeeded(
					request.MutationRequestID, request.MutationOwnerID,
					request.Domain, request.Qualifier,
				); err != nil {
					return err
				}
				response.Recovered = true
				return nil
			}
			attachDNSZoneV3TestAgent(t, panel, agent)

			if err := panel.syncZoneToDNS(
				context.Background(), "pending-recover-v3.example", false,
			); err != nil {
				t.Fatalf("pending exact recovery: %v", err)
			}
			agent.mu.Lock()
			requests := append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
			recoveries := append([]transport.RecoverDNSZoneV3Request(nil), agent.recoverRequests...)
			agent.mu.Unlock()
			if len(requests) != 1 || len(recoveries) != 1 ||
				recoveries[0].MutationRequestID != requests[0].MutationRequestID ||
				recoveries[0].MutationOwnerID != requests[0].MutationOwnerID ||
				recoveries[0].Domain != requests[0].Domain {
				t.Fatalf("pending request=%+v recovery=%+v", requests, recoveries)
			}
			if observedLeaseErr != nil ||
				observedLease.RequestID != requests[0].MutationRequestID ||
				observedLease.OwnerID != requests[0].MutationOwnerID {
				t.Fatalf("lease during exact recovery=%+v err=%v",
					observedLease, observedLeaseErr)
			}
			state, err := readDNSZoneSyncState(
				context.Background(), panel.db.GetDB(), "pending-recover-v3.example",
			)
			if err != nil {
				t.Fatal(err)
			}
			var leases int
			if err := panel.db.GetDB().QueryRow(`
				SELECT count(*) FROM dns_zone_engine_leases
				WHERE zone_name = 'pending-recover-v3.example'`).Scan(&leases); err != nil {
				t.Fatal(err)
			}
			if state.Status != "applied" ||
				state.AppliedGeneration != state.DesiredGeneration || leases != 0 {
				t.Fatalf("recovered state=%+v leases=%d", state, leases)
			}
		})
	}
}

func TestDNSZoneV3SameDomainRetryRecoversBeforePairReadiness(t *testing.T) {
	panel := newDNSPanelForTest(t)
	activatePairedPrimaryBINDForV3Test(t, panel)
	seedStrictDNSZone(t, panel, "pair-retry-v3.example")
	agent := newDNSZoneV3TestAgent()
	agent.pairReady = true
	configureDNSZoneV3PendingSync(agent, false)
	baseSyncHook := agent.syncResponseHook
	agent.syncResponseHook = func(
		request transport.SyncDNSZoneV3Request,
		response *transport.SyncDNSZoneV3Response,
	) error {
		err := baseSyncHook(request, response)
		agent.mu.Lock()
		agent.pairReady = false
		agent.mu.Unlock()
		return err
	}
	configureDNSZoneV3PendingRecovery(agent)
	attachDNSZoneV3TestAgent(t, panel, agent)

	err := panel.syncZoneToDNS(
		context.Background(), "pair-retry-v3.example", false,
	)
	var publicationErr *dnsAgentPublicationError
	if err == nil || !errors.Is(err, errDNSZoneV3PropagationDeferred) ||
		!errors.As(err, &publicationErr) {
		t.Fatalf("initial peer-pending error=%v", err)
	}
	lease, err := readDNSZoneEngineLease(
		context.Background(), panel.db.GetDB(), "pair-retry-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.recoverDNSZoneSyncStateLocked(context.Background()); err != nil {
		t.Fatalf("startup-style peer-down recovery must defer: %v", err)
	}
	retained, err := readDNSZoneEngineLease(
		context.Background(), panel.db.GetDB(), lease.ZoneName,
	)
	if err != nil || retained != lease {
		t.Fatalf("deferred lease=%+v err=%v want=%+v", retained, err, lease)
	}
	agent.recoverHook = nil
	if err := panel.syncZoneToDNS(
		context.Background(), "pair-retry-v3.example", false,
	); err != nil {
		t.Fatalf("same-domain retry did not recover before PairReady: %v", err)
	}
	agent.mu.Lock()
	syncRequests := len(agent.requests)
	recoveries := append([]transport.RecoverDNSZoneV3Request(nil), agent.recoverRequests...)
	agent.mu.Unlock()
	if syncRequests != 1 || len(recoveries) != 3 {
		t.Fatalf("same-generation retry sync=%d recoveries=%+v", syncRequests, recoveries)
	}
	for _, recovery := range recoveries {
		if recovery.MutationRequestID != lease.RequestID ||
			recovery.MutationOwnerID != lease.OwnerID {
			t.Fatalf("recovery changed exact binding: %+v want=%+v", recovery, lease)
		}
	}
	var leases int
	if err := panel.db.GetDB().QueryRow(`
		SELECT count(*) FROM dns_zone_engine_leases
		WHERE zone_name = 'pair-retry-v3.example'`).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	if leases != 0 {
		t.Fatalf("same-domain recovery retained %d lease(s)", leases)
	}
}

func TestDNSZoneV3RecoveryPublishesDesiredGenerationAdvancedWhilePending(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "advanced-pending-v3.example")
	seedV3ZoneRecord(t, panel, "advanced-pending-v3.example", "TXT", "before")
	agent := newDNSZoneV3TestAgent()
	configureDNSZoneV3PendingSync(agent, false)
	configureDNSZoneV3PendingRecovery(agent)
	attachDNSZoneV3TestAgent(t, panel, agent)
	if err := panel.syncZoneToDNS(
		context.Background(), "advanced-pending-v3.example", false,
	); !errors.Is(err, errDNSZoneV3PropagationDeferred) {
		t.Fatalf("initial pending err=%v", err)
	}
	if _, err := panel.db.GetDB().Exec(`
		UPDATE pdns_records SET content = 'advanced'
		WHERE name = 'advanced-pending-v3.example' AND type = 'TXT'`); err != nil {
		t.Fatal(err)
	}
	agent.recoverHook = nil
	agent.syncResponseHook = nil
	if err := panel.syncZoneToDNS(
		context.Background(), "advanced-pending-v3.example", false,
	); err != nil {
		t.Fatalf("publish advanced desired state after exact recovery: %v", err)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	agent.mu.Unlock()
	if len(requests) != 2 ||
		requests[1].DesiredGeneration <= requests[0].DesiredGeneration {
		t.Fatalf("advanced pending requests=%+v", requests)
	}
}

func TestDNSZoneV3StartupDeferredLeaseBlocksFreshPendingPublication(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "a-deferred-v3.example")
	seedStrictDNSZone(t, panel, "b-fresh-v3.example")
	agent := newDNSZoneV3TestAgent()
	attachDNSZoneV3TestAgent(t, panel, agent)
	plan, err := panel.prepareDNSZoneSyncV3Plan(
		context.Background(), "a-deferred-v3.example", false,
		dnsPublisherIdentity{Engine: transport.DNSEngineBIND, Epoch: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	setDNSZoneV3PendingReceipt(t, agent, plan.Lease)
	configureDNSZoneV3PendingRecovery(agent)
	bBefore, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "b-fresh-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := panel.recoverDNSZoneSyncStateLocked(context.Background()); err != nil {
		t.Fatalf("deferred startup recovery: %v", err)
	}
	bAfter, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "b-fresh-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := readDNSZoneEngineLease(
		context.Background(), panel.db.GetDB(), plan.Lease.ZoneName,
	)
	if err != nil || retained != plan.Lease || !reflect.DeepEqual(bAfter, bBefore) {
		t.Fatalf("startup A lease=%+v err=%v B before=%+v after=%+v",
			retained, err, bBefore, bAfter)
	}
	agent.mu.Lock()
	syncRequests := len(agent.requests)
	agent.mu.Unlock()
	if syncRequests != 0 {
		t.Fatalf("startup attempted %d fresh Sync RPC(s) behind deferred lease", syncRequests)
	}
}

func TestDNSZoneV3StartupConsumesLateSuccessBetweenDeferredPasses(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "late-success-v3.example")
	seedStrictDNSZone(t, panel, "late-success-follower-v3.example")
	agent := newDNSZoneV3TestAgent()
	attachDNSZoneV3TestAgent(t, panel, agent)
	plan, err := panel.prepareDNSZoneSyncV3Plan(
		context.Background(), "late-success-v3.example", false,
		dnsPublisherIdentity{Engine: transport.DNSEngineBIND, Epoch: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	setDNSZoneV3PendingReceipt(t, agent, plan.Lease)
	configureDNSZoneV3PendingRecovery(agent)
	statusCalls := 0
	agent.statusHook = func(requestID string, job *ServiceOperationMutationJob) {
		if requestID != plan.Lease.RequestID || job == nil {
			return
		}
		statusCalls++
		if statusCalls == 3 {
			job.Status = agentMutationSucceeded
			job.Phase = "commit/dns-zone-sync/v3/published/" +
				job.RequestID + "/" + job.Target + "/" + job.PackageName
		}
	}
	if err := panel.recoverDNSZoneSyncStateLocked(context.Background()); err != nil {
		t.Fatalf("consume late exact success: %v", err)
	}
	var leases int
	if err := panel.db.GetDB().QueryRow(`
		SELECT count(*) FROM dns_zone_engine_leases
		WHERE zone_name = 'late-success-v3.example'`).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	state, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "late-success-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if statusCalls < 3 || leases != 0 || state.Status != "applied" {
		t.Fatalf("late success calls=%d leases=%d state=%+v", statusCalls, leases, state)
	}
	follower, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "late-success-follower-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	agent.mu.Unlock()
	if follower.Status != "applied" || len(requests) != 1 ||
		requests[0].Domain != follower.ZoneName {
		t.Fatalf("late-success follower=%+v requests=%+v", follower, requests)
	}
}

func TestDNSZoneV3StartupBeginResumeResponseLossDefersAndLaterRecovers(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "begin-loss-v3.example")
	agent := newDNSZoneV3TestAgent()
	attachDNSZoneV3TestAgent(t, panel, agent)
	plan, err := panel.prepareDNSZoneSyncV3Plan(
		context.Background(), "begin-loss-v3.example", false,
		dnsPublisherIdentity{Engine: transport.DNSEngineBIND, Epoch: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	setDNSZoneV3PendingReceipt(t, agent, plan.Lease)
	agent.expireRecoveryOnStatus = true
	agent.resumeBeginHook = func(transport.ServiceMutationBeginRequest) error {
		return errors.New("injected Begin response loss after recovering receipt")
	}
	started := time.Now()
	if err := panel.recoverDNSZoneSyncStateLocked(context.Background()); err != nil {
		t.Fatalf("Begin response loss must defer startup: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Begin response loss escaped bounded recovery handoff: %v", elapsed)
	}
	retained, err := readDNSZoneEngineLease(
		context.Background(), panel.db.GetDB(), plan.Lease.ZoneName,
	)
	if err != nil || retained != plan.Lease {
		t.Fatalf("Begin-loss lease=%+v err=%v want=%+v", retained, err, plan.Lease)
	}
	agent.mu.Lock()
	recoverCalls := len(agent.recoverRequests)
	agent.mu.Unlock()
	if recoverCalls != 0 {
		t.Fatalf("Begin response loss unexpectedly called Recover %d time(s)", recoverCalls)
	}
	agent.resumeBeginHook = nil
	if err := panel.syncZoneToDNS(
		context.Background(), "begin-loss-v3.example", false,
	); err != nil {
		t.Fatalf("later exact recovery after Begin loss: %v", err)
	}
}

func TestDNSZoneV3PrecommitFailureRetiresExactPanelLease(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	activateDNSEngineForTest(t, panel, string(transport.DNSEngineBIND))
	seedStrictDNSZone(t, panel, "precommit-failure-v3.example")
	agent := newDNSZoneV3TestAgent()
	agent.syncResponseHook = func(
		transport.SyncDNSZoneV3Request,
		*transport.SyncDNSZoneV3Response,
	) error {
		return errors.New("injected permanent precommit failure")
	}
	attachDNSZoneV3TestAgent(t, panel, agent)
	if err := panel.syncZoneToDNS(
		context.Background(), "precommit-failure-v3.example", false,
	); err == nil {
		t.Fatal("precommit failure unexpectedly succeeded")
	}
	var leases int
	if err := panel.db.GetDB().QueryRow(`
		SELECT count(*) FROM dns_zone_engine_leases
		WHERE zone_name = 'precommit-failure-v3.example'`).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	state, err := readDNSZoneSyncState(
		context.Background(), panel.db.GetDB(), "precommit-failure-v3.example",
	)
	if err != nil {
		t.Fatal(err)
	}
	if leases != 0 || state.Status != "error" {
		t.Fatalf("precommit leases=%d state=%+v", leases, state)
	}
}

func TestDNSZoneV3RPCBoundariesRejectMixedFailureTuples(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := newDNSZoneV3TestAgent()
	agent.syncResponseHook = func(
		request transport.SyncDNSZoneV3Request,
		response *transport.SyncDNSZoneV3Response,
	) error {
		*response = transport.SyncDNSZoneV3Response{
			Error: "ordinary failure", Engine: request.Engine,
			EngineEpoch: request.EngineEpoch,
		}
		return nil
	}
	agent.recoverHook = func(
		_ transport.RecoverDNSZoneV3Request,
		response *transport.RecoverDNSZoneV3Response,
	) error {
		*response = transport.RecoverDNSZoneV3Response{
			Error: "ordinary recovery failure", RecoveryPending: true,
		}
		return nil
	}
	attachDNSZoneV3TestAgent(t, panel, agent)
	request := transport.SyncDNSZoneV3Request{
		Engine: transport.DNSEngineBIND, EngineEpoch: 1,
		DesiredGeneration: 3, Domain: "mixed-v3.example",
		ZoneType: "MASTER",
	}
	var syncResponse transport.SyncDNSZoneV3Response
	if err := panel.callSyncDNSZoneV3(
		context.Background(), &request, &syncResponse,
	); err == nil || !strings.Contains(err.Error(), "mixed") {
		t.Fatalf("mixed Sync tuple response=%+v err=%v", syncResponse, err)
	}
	lease := dnsZoneEngineLease{
		ZoneName: "mixed-v3.example", Engine: transport.DNSEngineBIND,
		EngineEpoch: 1, RequestID: strings.Repeat("8", 32),
		OwnerID: strings.Repeat("9", 32), DesiredGeneration: 3,
		DesiredAction: "sync", DesiredZoneType: "MASTER",
		Qualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("a", 64),
		ExpiresAt: "2099-01-01T00:00:00Z",
	}
	var recoverResponse transport.RecoverDNSZoneV3Response
	if err := panel.callRecoverDNSZoneV3(
		context.Background(), lease,
		agentMutationBinding{
			MutationRequestID: lease.RequestID,
			MutationOwnerID:   lease.OwnerID,
		},
		&recoverResponse,
	); err == nil || !strings.Contains(err.Error(), "mixed") {
		t.Fatalf("mixed Recover tuple response=%+v err=%v", recoverResponse, err)
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
