package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func seedDNSSECManagedDomain(t *testing.T, p *Panel, domain string) int {
	t.Helper()
	result, err := p.db.GetDB().Exec(
		"INSERT INTO users (username, password_hash, email, role) VALUES (?, 'hash', ?, 'customer')",
		"dnssec-owner-"+domain, "owner@"+domain,
	)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = p.db.GetDB().Exec(
		"INSERT INTO subscriptions (owner_id, name) VALUES (?, 'DNSSEC V2')",
		ownerID,
	)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = p.db.GetDB().Exec(
		"INSERT INTO domains (subscription_id, name) VALUES (?, ?)",
		subscriptionID, domain,
	)
	if err != nil {
		t.Fatal(err)
	}
	domainID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	seedStrictDNSZone(t, p, domain)
	return int(domainID)
}

func postDNSSECV2(t *testing.T, p *Panel, domainID int) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := strictDNSAdminRequest(httptest.NewRequest(
		http.MethodPost, fmt.Sprintf("/api/v1/domains/%d/dnssec", domainID), nil,
	))
	p.handleDomainDNSSEC(recorder, request, domainID)
	return recorder
}

func strictDNSState(t *testing.T, p *Panel, domain string) dnsZoneSyncState {
	t.Helper()
	state, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), domain)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func setStrictAgentJobTerminal(
	t *testing.T,
	agent *strictDNSRPCAgent,
	requestID, status string,
) {
	t.Helper()
	agent.durableMutationRPCFixture.mu.Lock()
	defer agent.durableMutationRPCFixture.mu.Unlock()
	job := agent.durableMutationRPCFixture.jobs[requestID]
	if job == nil {
		t.Fatalf("durable fake has no job %s", requestID)
	}
	job.Status = status
	if status == agentMutationSucceeded {
		job.Phase = "completed"
	} else {
		job.Phase = "failed"
		job.ErrorCode = "injected_terminal_failure"
		job.ErrorMessage = "injected terminal failure"
	}
	if agent.durableMutationRPCFixture.active == requestID {
		agent.durableMutationRPCFixture.active = ""
	}
}

func TestDNSSECV2PersistsBridgeBeforeSigningAndPublishesAfterTerminalSuccess(t *testing.T) {
	const domain = "dnssec-order.example"
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	domainID := seedDNSSECManagedDomain(t, p, domain)
	agent := &strictDNSRPCAgent{}

	var mu sync.Mutex
	var beginKinds []string
	var bridge dnsZoneEngineLease
	var hookErr error
	secureCompleted := false
	agent.beginHook = func(req *ServiceOperationMutationBeginRequest) error {
		mu.Lock()
		defer mu.Unlock()
		beginKinds = append(beginKinds, req.Kind)
		switch req.Kind {
		case "dnssec_secure":
			lease, err := readDNSZoneEngineLease(
				context.Background(), p.db.GetDB(), domain,
			)
			if err != nil {
				return err
			}
			if !lease.valid() || lease.DesiredAction != "sync" ||
				lease.ZoneName != req.Target ||
				lease.Engine != transport.DNSEnginePowerDNS ||
				lease.EngineEpoch != 1 {
				return fmt.Errorf("DNSSEC Begin observed no exact V3 pre-sign bridge: %+v", lease)
			}
			bridge = lease
		case "dns_zone_sync":
			if !secureCompleted {
				return errors.New("DNS publication began before signing completed")
			}
		}
		return nil
	}
	agent.secureHook = func(
		req transport.SecureDNSZoneV2Request,
		_ *transport.SecureDNSZoneV2Response,
	) error {
		mu.Lock()
		defer mu.Unlock()
		if req.Zone != domain || !validServiceOperationID(req.MutationRequestID) ||
			!validServiceOperationID(req.MutationOwnerID) {
			hookErr = fmt.Errorf("invalid SecureDNSZoneV2 request: %+v", req)
		}
		agent.mu.Lock()
		syncCalls := len(agent.syncRequests)
		agent.mu.Unlock()
		if syncCalls != 0 {
			hookErr = fmt.Errorf("DNS publication ran before SecureDNSZoneV2")
		}
		secureCompleted = true
		return nil
	}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := postDNSSECV2(t, p, domainID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("DNSSEC V2 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if hookErr != nil {
		t.Fatal(hookErr)
	}
	if !reflect.DeepEqual(beginKinds, []string{"dnssec_secure", "dns_zone_sync"}) {
		t.Fatalf("durable child order=%v", beginKinds)
	}
	if !bridge.valid() {
		t.Fatalf("signing bridge was not captured: %+v", bridge)
	}
	agent.mu.Lock()
	secureRequests := append([]transport.SecureDNSZoneV2Request(nil), agent.secureRequests...)
	syncRequests := append([]transport.SyncDNSZoneV2Request(nil), agent.syncRequests...)
	agent.mu.Unlock()
	if len(secureRequests) != 1 || len(syncRequests) != 1 {
		t.Fatalf("secure/sync requests=%d/%d", len(secureRequests), len(syncRequests))
	}
	if syncRequests[0].DesiredGeneration != bridge.DesiredGeneration ||
		syncRequests[0].MutationRequestID != bridge.RequestID ||
		syncRequests[0].MutationOwnerID != bridge.OwnerID {
		t.Fatalf("published request does not consume pre-sign bridge: request=%+v bridge=%+v",
			syncRequests[0], bridge)
	}
	state := strictDNSState(t, p, domain)
	if state.hasLease() || state.Status != "applied" ||
		state.AppliedGeneration != state.DesiredGeneration {
		t.Fatalf("final DNSSEC publication state=%+v", state)
	}
	if _, err := readDNSZoneEngineLease(
		context.Background(), p.db.GetDB(), domain,
	); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("final DNSSEC V3 lease was not retired: %v", err)
	}
}

func TestDNSSECV2ActiveAmbiguityRetainsExactBridgeAndNeverPublishes(t *testing.T) {
	const domain = "dnssec-active-ambiguity.example"
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	domainID := seedDNSSECManagedDomain(t, p, domain)
	agent := &strictDNSRPCAgent{
		finishError: errors.New("injected FinishServiceMutation response loss"),
	}
	var bridge dnsZoneEngineLease
	var bridgeErr error
	agent.secureHook = func(
		_ transport.SecureDNSZoneV2Request,
		_ *transport.SecureDNSZoneV2Response,
	) error {
		bridge, bridgeErr = readDNSZoneEngineLease(
			context.Background(), p.db.GetDB(), domain,
		)
		return errors.New("injected SecureDNSZoneV2 response loss")
	}
	attachStrictDNSRPCAgent(t, p, agent)

	previousWait := waitExpectedAgentMutationTerminalFn
	waitExpectedAgentMutationTerminalFn = func(
		panel *Panel,
		ctx context.Context,
		identity agentMutationIdentity,
	) (*agentMutationJob, error) {
		return panel.statusAgentMutation(ctx, identity.RequestID)
	}
	t.Cleanup(func() { waitExpectedAgentMutationTerminalFn = previousWait })

	recorder := postDNSSECV2(t, p, domainID)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("ambiguous DNSSEC status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if bridgeErr != nil {
		t.Fatal(bridgeErr)
	}
	if !bridge.valid() {
		t.Fatalf("SecureDNSZoneV2 did not observe a persisted bridge: %+v", bridge)
	}
	after, afterErr := readDNSZoneEngineLease(
		context.Background(), p.db.GetDB(), domain,
	)
	if afterErr != nil {
		t.Fatal(afterErr)
	}
	if !reflect.DeepEqual(after, bridge) {
		t.Fatalf("ambiguous DNSSEC mutated exact bridge: before=%+v after=%+v", bridge, after)
	}
	agent.mu.Lock()
	secureCalls, syncCalls := agent.secureDNSCalls, len(agent.syncRequests)
	agent.mu.Unlock()
	if secureCalls != 1 || syncCalls != 0 {
		t.Fatalf("ambiguous DNSSEC secure/sync calls=%d/%d", secureCalls, syncCalls)
	}
}

func TestDNSSECV2ResponseLossWithExactSucceededReceiptPublishesAndRecoversDS(t *testing.T) {
	const domain = "dnssec-response-loss.example"
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	domainID := seedDNSSECManagedDomain(t, p, domain)
	agent := &strictDNSRPCAgent{}
	agent.secureHook = func(
		req transport.SecureDNSZoneV2Request,
		_ *transport.SecureDNSZoneV2Response,
	) error {
		setStrictAgentJobTerminal(
			t, agent, req.MutationRequestID, agentMutationSucceeded,
		)
		return errors.New("injected response loss after exact DNSSEC commit")
	}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := postDNSSECV2(t, p, domainID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("terminal response-loss status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "12345 13 2 AABBCC") {
		t.Fatalf("response-loss success did not recover DS: %s", recorder.Body.String())
	}
	state := strictDNSState(t, p, domain)
	if state.hasLease() || state.Status != "applied" ||
		state.AppliedGeneration != state.DesiredGeneration {
		t.Fatalf("response-loss DNS state=%+v", state)
	}
	agent.mu.Lock()
	secureCalls := agent.secureDNSCalls
	statusCalls := agent.dnssecStatusCalls
	syncCalls := len(agent.syncRequests)
	agent.mu.Unlock()
	if secureCalls != 1 || statusCalls != 1 || syncCalls != 1 {
		t.Fatalf("response-loss secure/status/sync=%d/%d/%d",
			secureCalls, statusCalls, syncCalls)
	}
}

func TestDNSSECV2ReconcilesNilAndTerminalPriorDNSLeasesBeforeSigning(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "proven never begun"},
		{name: "terminal failed", status: agentMutationFailed},
		{name: "terminal succeeded", status: agentMutationSucceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const domain = "dnssec-prior-lease.example"
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "standalone")
			domainID := seedDNSSECManagedDomain(t, p, domain)
			prior, err := p.prepareDNSZoneSyncPlan(
				context.Background(), domain, false,
			)
			if err != nil {
				t.Fatal(err)
			}
			agent := &strictDNSRPCAgent{}
			if test.status != "" {
				phase := "failed"
				if test.status == agentMutationSucceeded {
					phase = "commit/dns-zone-sync/v1/published/" +
						prior.RequestID + "/" + domain + "/" + prior.Commitment.Qualifier
				}
				agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
					prior.RequestID: {
						RequestID: prior.RequestID, OwnerID: prior.OwnerID,
						Kind: "dns_zone_sync", Target: domain,
						PackageName: prior.Commitment.Qualifier,
						Status:      test.status,
						Phase:       phase,
					},
				}
			}
			attachStrictDNSRPCAgent(t, p, agent)

			recorder := postDNSSECV2(t, p, domainID)
			if recorder.Code != http.StatusOK {
				t.Fatalf("prior lease status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			state := strictDNSState(t, p, domain)
			if state.hasLease() || state.Status != "applied" ||
				state.AppliedGeneration != state.DesiredGeneration {
				t.Fatalf("reconciled prior lease state=%+v", state)
			}
			agent.mu.Lock()
			secureCalls := agent.secureDNSCalls
			requests := append([]transport.SyncDNSZoneV2Request(nil), agent.syncRequests...)
			agent.mu.Unlock()
			if secureCalls != 1 || len(requests) != 1 ||
				requests[0].MutationRequestID == prior.RequestID {
				t.Fatalf("prior lease was not replaced before signing: secure=%d requests=%+v prior=%s",
					secureCalls, requests, prior.RequestID)
			}
		})
	}
}

func TestDNSSECV2RetainsExactActivePriorDNSLeaseWithoutSigning(t *testing.T) {
	const domain = "dnssec-prior-active.example"
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	domainID := seedDNSSECManagedDomain(t, p, domain)
	prior, err := p.prepareDNSZoneSyncPlan(context.Background(), domain, false)
	if err != nil {
		t.Fatal(err)
	}
	agent := &strictDNSRPCAgent{}
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		prior.RequestID: {
			RequestID: prior.RequestID, OwnerID: prior.OwnerID,
			Kind: "dns_zone_sync", Target: domain,
			PackageName: prior.Commitment.Qualifier,
			Status:      agentMutationRunning,
		},
	}
	agent.durableMutationRPCFixture.active = prior.RequestID
	attachStrictDNSRPCAgent(t, p, agent)
	before := strictDNSState(t, p, domain)

	recorder := postDNSSECV2(t, p, domainID)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("active prior lease status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	after := strictDNSState(t, p, domain)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("active prior lease mutated: before=%+v after=%+v", before, after)
	}
	agent.mu.Lock()
	secureCalls, syncCalls, beginCalls :=
		agent.secureDNSCalls, len(agent.syncRequests), agent.beginCalls
	agent.mu.Unlock()
	if secureCalls != 0 || syncCalls != 0 || beginCalls != 0 {
		t.Fatalf("active prior lease touched host secure/sync/begin=%d/%d/%d",
			secureCalls, syncCalls, beginCalls)
	}
}

func newActiveDNSSECRecoveryJob(domain string) *ServiceOperationMutationJob {
	now := time.Now().UTC()
	return &ServiceOperationMutationJob{
		RequestID:      "11111111111111111111111111111111",
		OwnerID:        "22222222222222222222222222222222",
		Kind:           "dnssec_secure",
		Target:         domain,
		Status:         agentMutationRunning,
		Phase:          "securing",
		LeaseExpiresAt: now.Add(time.Minute),
		DeadlineAt:     now.Add(time.Hour),
	}
}

func attachActiveDNSSECRecoveryJob(
	agent *strictDNSRPCAgent,
	job *ServiceOperationMutationJob,
) {
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		job.RequestID: job,
	}
	agent.durableMutationRPCFixture.active = job.RequestID
}

func TestDNSSECStartupActiveJobWithExactBridgeRetriesAndPublishes(t *testing.T) {
	const domain = "dnssec-startup-active.example"
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, domain)
	bridge, err := p.prepareDNSZoneSyncPlan(context.Background(), domain, false)
	if err != nil {
		t.Fatal(err)
	}
	agent := &strictDNSRPCAgent{}
	attachActiveDNSSECRecoveryJob(agent, newActiveDNSSECRecoveryJob(domain))
	attachStrictDNSRPCAgent(t, p, agent)

	if _, err := p.recoverInterruptedServiceOperations(context.Background()); err != nil {
		t.Fatalf("recover active DNSSEC bridge: %v", err)
	}
	state := strictDNSState(t, p, domain)
	if state.hasLease() || state.Status != "applied" ||
		state.AppliedGeneration != state.DesiredGeneration {
		t.Fatalf("startup active DNSSEC state=%+v", state)
	}
	agent.mu.Lock()
	cancelCalls, secureCalls := agent.cancelCalls, agent.secureDNSCalls
	requests := append([]transport.SyncDNSZoneV2Request(nil), agent.syncRequests...)
	agent.mu.Unlock()
	if cancelCalls != 1 || secureCalls != 1 || len(requests) != 1 {
		t.Fatalf("startup cancel/secure/sync=%d/%d/%d",
			cancelCalls, secureCalls, len(requests))
	}
	if requests[0].MutationRequestID == bridge.RequestID {
		t.Fatalf("startup reused pre-sign DNS child identity %s", bridge.RequestID)
	}
}

func TestDNSSECStartupCrashPublishesGenerationAdvancedDuringSigning(t *testing.T) {
	const domain = "dnssec-startup-advanced.example"
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, domain)
	bridge, err := p.prepareDNSZoneSyncPlan(context.Background(), domain, false)
	if err != nil {
		t.Fatal(err)
	}
	var zoneID int64
	if err := p.db.GetDB().QueryRow(
		"SELECT id FROM pdns_domains WHERE name = ?", domain,
	).Scan(&zoneID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.GetDB().Exec(
		"INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio, disabled) VALUES (?, ?, 'TXT', 'advanced-during-sign', 300, 0, 0)",
		zoneID, domain,
	); err != nil {
		t.Fatal(err)
	}
	advanced := strictDNSState(t, p, domain)
	if advanced.DesiredGeneration <= bridge.Commitment.DesiredGeneration ||
		advanced.LeaseGeneration.Int64 != bridge.Commitment.DesiredGeneration {
		t.Fatalf("advanced bridge fixture=%+v initial=%+v", advanced, bridge.State)
	}
	agent := &strictDNSRPCAgent{}
	attachActiveDNSSECRecoveryJob(agent, newActiveDNSSECRecoveryJob(domain))
	attachStrictDNSRPCAgent(t, p, agent)

	if _, err := p.recoverInterruptedServiceOperations(context.Background()); err != nil {
		t.Fatalf("recover advanced DNSSEC bridge: %v", err)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV2Request(nil), agent.syncRequests...)
	agent.mu.Unlock()
	if len(requests) != 1 ||
		requests[0].DesiredGeneration <= bridge.Commitment.DesiredGeneration ||
		requests[0].DesiredGeneration < advanced.DesiredGeneration {
		t.Fatalf("startup published stale generation: requests=%+v advanced=%+v",
			requests, advanced)
	}
	foundAdvanced := false
	for _, record := range requests[0].Records {
		if record.Type == "TXT" && record.Content == "advanced-during-sign" {
			foundAdvanced = true
		}
	}
	if !foundAdvanced {
		t.Fatalf("startup current snapshot omitted in-flight edit: %+v", requests[0].Records)
	}
}

func TestDNSSECStartupTerminalFreshRetryFailureStillPublishesBridge(t *testing.T) {
	const domain = "dnssec-startup-terminal-failure.example"
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, domain)
	if _, err := p.prepareDNSZoneSyncPlan(context.Background(), domain, false); err != nil {
		t.Fatal(err)
	}
	agent := &strictDNSRPCAgent{
		secureResponseError: "injected exact terminal DNSSEC failure",
	}
	attachActiveDNSSECRecoveryJob(agent, newActiveDNSSECRecoveryJob(domain))
	attachStrictDNSRPCAgent(t, p, agent)

	if _, recoveryErr := p.recoverInterruptedServiceOperations(
		context.Background(),
	); recoveryErr != nil {
		t.Fatalf("known terminal signing failure blocked repaired startup: %v", recoveryErr)
	}
	state := strictDNSState(t, p, domain)
	if state.hasLease() || state.Status != "applied" ||
		state.AppliedGeneration != state.DesiredGeneration {
		t.Fatalf("terminal signing failure did not repair bridge: %+v", state)
	}
	agent.mu.Lock()
	cancelCalls, secureCalls, syncCalls :=
		agent.cancelCalls, agent.secureDNSCalls, len(agent.syncRequests)
	agent.mu.Unlock()
	if cancelCalls != 1 || secureCalls != 1 || syncCalls != 1 {
		t.Fatalf("terminal failure cancel/secure/sync=%d/%d/%d",
			cancelCalls, secureCalls, syncCalls)
	}
}

func TestDNSSECStartupRejectsJobWithoutSameDomainBridge(t *testing.T) {
	const bridgedDomain = "dnssec-bridged.example"
	const jobDomain = "dnssec-unbridged.example"
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	seedStrictDNSZone(t, p, bridgedDomain)
	if _, err := p.prepareDNSZoneSyncPlan(
		context.Background(), bridgedDomain, false,
	); err != nil {
		t.Fatal(err)
	}
	before := strictDNSState(t, p, bridgedDomain)
	agent := &strictDNSRPCAgent{}
	attachActiveDNSSECRecoveryJob(agent, newActiveDNSSECRecoveryJob(jobDomain))
	attachStrictDNSRPCAgent(t, p, agent)

	if _, err := p.recoverInterruptedServiceOperations(context.Background()); err == nil {
		t.Fatal("unbridged active DNSSEC job was accepted")
	}
	after := strictDNSState(t, p, bridgedDomain)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("unbridged job mutated unrelated bridge: before=%+v after=%+v",
			before, after)
	}
	agent.mu.Lock()
	cancelCalls, secureCalls, syncCalls :=
		agent.cancelCalls, agent.secureDNSCalls, len(agent.syncRequests)
	agent.mu.Unlock()
	if cancelCalls != 0 || secureCalls != 0 || syncCalls != 0 {
		t.Fatalf("unbridged job host calls cancel/secure/sync=%d/%d/%d",
			cancelCalls, secureCalls, syncCalls)
	}
}
