package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const pairedDNSSetupBody = `{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`

func pendingDNSClusterSagaFixture(
	t *testing.T,
	p *Panel,
	domainSuffix string,
) dnsClusterSaga {
	t.Helper()
	previous, err := readDNSClusterTopology(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	desired := dnsClusterTopology{
		Role: "paired", PeerIP: "198.51.100.20",
		PeerNS: "ns4." + domainSuffix,
		NS1:    "ns3." + domainSuffix, NS2: "ns4." + domainSuffix,
		LocalIPv4: "192.0.2.10",
		RawRole:   "paired", RawPeerIP: "198.51.100.20",
		RawPeerNS: "ns4." + domainSuffix,
		RawNS1:    "ns3." + domainSuffix, RawNS2: "ns4." + domainSuffix,
	}
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		desired.Role, desired.PeerIP, desired.PeerNS,
	)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := newServiceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	saga := dnsClusterSaga{
		Version: dnsClusterSagaVersion, Phase: dnsClusterSagaDesired,
		RequestID: requestID, OwnerID: ownerID,
		Qualifier: commitment.Qualifier, Desired: desired, Previous: previous,
	}
	if err := persistDNSClusterDesired(context.Background(), p, saga); err != nil {
		t.Fatal(err)
	}
	return saga
}

func publishStrictDNSClusterJob(
	agent *strictDNSRPCAgent,
	saga dnsClusterSaga,
) {
	agent.durableMutationRPCFixture.mu.Lock()
	defer agent.durableMutationRPCFixture.mu.Unlock()
	job := agent.durableMutationRPCFixture.jobs[saga.RequestID]
	if job == nil {
		return
	}
	job.Status = agentMutationSucceeded
	job.Phase = dnsClusterPublishedPhase(saga)
	if agent.durableMutationRPCFixture.active == saga.RequestID {
		agent.durableMutationRPCFixture.active = ""
	}
}

func TestDNSClusterV2DesiredAndIdentityPersistBeforeBegin(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &strictDNSRPCAgent{}
	var observed bool
	agent.beginHook = func(req *ServiceOperationMutationBeginRequest) error {
		if req.Kind != "dns_cluster_configure" {
			return nil
		}
		observed = true
		if req.Target != "pdns" ||
			!mutationpayload.ValidDNSClusterConfigQualifier(req.PackageName) {
			return errors.New("cluster Begin did not carry an exact payload identity")
		}
		pending, err := readPendingDNSClusterSaga(context.Background(), p)
		if err != nil {
			return err
		}
		if pending == nil || pending.RequestID != req.RequestID ||
			pending.OwnerID != req.OwnerID ||
			pending.Qualifier != req.PackageName ||
			pending.Desired.Role != "paired" ||
			pending.Desired.PeerIP != "198.51.100.20" ||
			pending.Desired.PeerNS != "ns4.example.net" {
			return errors.New("desired DNS topology was not exact before Begin")
		}
		if p.setting(context.Background(), settingNS1) != "ns1.celikhost.com" ||
			p.setting(context.Background(), settingNS2) != "ns2.celikhost.com" ||
			p.setting(context.Background(), settingDNSRole) != "paired" ||
			p.setting(context.Background(), settingDNSPeerIP) != "2.25.80.4" ||
			p.setting(context.Background(), settingDNSPeerNS) != "ns2.celikhost.com" {
			return errors.New("active DNS settings changed before exact host success")
		}
		return nil
	}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
	if recorder.Code != http.StatusOK {
		t.Fatalf("DNS cluster V2 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !observed {
		t.Fatal("DNS cluster V2 Begin was not observed")
	}
	agent.mu.Lock()
	requests := append([]transport.ConfigureDNSClusterV2Request(nil), agent.clusterRequests...)
	agent.mu.Unlock()
	if len(requests) != 1 ||
		requests[0].MutationRequestID == "" ||
		requests[0].MutationOwnerID == "" ||
		requests[0].Role != "paired" ||
		requests[0].PeerIP != "198.51.100.20" ||
		requests[0].PeerNS != "ns4.example.net" {
		t.Fatalf("ConfigureDNSClusterV2 requests=%+v", requests)
	}
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("successful topology left pending saga=%+v", pending)
	}
}

func TestDNSClusterPendingIntentKeepsPreviousTopologyForZoneCreation(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	saga := pendingDNSClusterSagaFixture(t, p, "example.net")

	zoneID, err := p.ensureZone(
		context.Background(), "pending-intent-zone.example",
	)
	if err != nil {
		t.Fatalf("create zone under pending topology intent: %v", err)
	}
	rows, err := p.db.GetDB().Query(`
		SELECT type, content
		FROM pdns_records
		WHERE domain_id = ? AND type IN ('SOA', 'NS')
		ORDER BY type, content`, zoneID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var soa string
	nameservers := map[string]bool{}
	for rows.Next() {
		var recordType, content string
		if err := rows.Scan(&recordType, &content); err != nil {
			t.Fatal(err)
		}
		if recordType == "SOA" {
			soa = content
		} else {
			nameservers[content] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(
		soa,
		"ns1.celikhost.com hostmaster.pending-intent-zone.example ",
	) || !nameservers["ns1.celikhost.com"] ||
		!nameservers["ns2.celikhost.com"] ||
		nameservers[saga.Desired.NS1] || nameservers[saga.Desired.NS2] {
		t.Fatalf("pending intent zone used wrong topology: soa=%q NS=%v",
			soa, nameservers)
	}
	if pending, err := readPendingDNSClusterSaga(
		context.Background(), p,
	); err != nil {
		t.Fatal(err)
	} else if pending == nil || !reflect.DeepEqual(*pending, saga) {
		t.Fatalf("zone creation changed pending saga=%+v", pending)
	}
}

func TestDNSClusterV2ResponseLossWithExactSucceededReceiptFinalizes(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &strictDNSRPCAgent{}
	agent.clusterHook = func(
		req transport.ConfigureDNSClusterV2Request,
		_ *transport.ConfigureDNSClusterV2Response,
	) error {
		agent.durableMutationRPCFixture.mu.Lock()
		job := agent.durableMutationRPCFixture.jobs[req.MutationRequestID]
		if job == nil {
			agent.durableMutationRPCFixture.mu.Unlock()
			return errors.New("response-loss fixture lost its exact job")
		}
		job.Status = agentMutationSucceeded
		job.Phase = dnsClusterPublishedPhase(dnsClusterSaga{
			RequestID: job.RequestID,
			Qualifier: job.PackageName,
		})
		if agent.durableMutationRPCFixture.active == job.RequestID {
			agent.durableMutationRPCFixture.active = ""
		}
		agent.durableMutationRPCFixture.mu.Unlock()
		return errors.New("injected response loss after DNS topology commit")
	}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
	if recorder.Code != http.StatusOK {
		t.Fatalf("terminal response-loss status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("terminal success retained saga=%+v", pending)
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1: "ns3.example.net", settingNS2: "ns4.example.net",
		settingDNSRole: "paired", settingDNSPeerIP: "198.51.100.20",
		settingDNSPeerNS: "ns4.example.net",
	})
}

func TestDNSClusterV2RejectsNonPublishedSucceededReceipt(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &strictDNSRPCAgent{}
	agent.clusterHook = func(
		req transport.ConfigureDNSClusterV2Request,
		_ *transport.ConfigureDNSClusterV2Response,
	) error {
		setStrictAgentJobTerminal(
			t, agent, req.MutationRequestID, agentMutationSucceeded,
		)
		return errors.New("injected response loss before canonical publication")
	}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("non-published success status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	pending, err := readPendingDNSClusterSaga(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || pending.Desired.Role != "paired" ||
		pending.Desired.PeerIP != "198.51.100.20" {
		t.Fatalf("non-published success lost desired saga=%+v", pending)
	}
	agent.mu.Lock()
	clusterCalls, syncCalls := agent.clusterCalls, len(agent.syncRequests)
	agent.mu.Unlock()
	if clusterCalls != 1 || syncCalls != 0 {
		t.Fatalf("non-published success cluster/sync=%d/%d",
			clusterCalls, syncCalls)
	}
}

func TestDNSClusterV2ActiveAmbiguityRetainsExactDesiredSaga(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &strictDNSRPCAgent{
		finishError: errors.New("injected FinishServiceMutation loss"),
	}
	agent.clusterHook = func(
		_ transport.ConfigureDNSClusterV2Request,
		_ *transport.ConfigureDNSClusterV2Response,
	) error {
		return errors.New("injected ConfigureDNSClusterV2 response loss")
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

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("ambiguous topology status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	pending, err := readPendingDNSClusterSaga(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || pending.Desired.Role != "paired" ||
		pending.Desired.PeerIP != "198.51.100.20" ||
		pending.Desired.PeerNS != "ns4.example.net" {
		t.Fatalf("ambiguous topology lost exact desired saga=%+v", pending)
	}
	before := *pending
	retry := httptest.NewRecorder()
	p.handleDNSSetup(retry, dnsSetupAdminRequest(pairedDNSSetupBody))
	if retry.Code != http.StatusConflict {
		t.Fatalf("pending retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	after, err := readPendingDNSClusterSaga(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if after == nil || !reflect.DeepEqual(*after, before) {
		t.Fatalf("pending retry changed exact saga: before=%+v after=%+v", before, after)
	}
	agent.mu.Lock()
	beginCalls := agent.beginCalls
	clusterCalls := agent.clusterCalls
	syncCalls := len(agent.syncRequests)
	agent.mu.Unlock()
	if beginCalls != 1 || clusterCalls != 1 || syncCalls != 0 {
		t.Fatalf("ambiguous/retry begin/cluster/sync calls=%d/%d/%d",
			beginCalls, clusterCalls, syncCalls)
	}
}

func TestDNSClusterV2CrashBeforeBeginExactPUTReusesPersistedIdentity(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	saga := pendingDNSClusterSagaFixture(t, p, "example.net")
	agent := &strictDNSRPCAgent{}
	var began bool
	agent.beginHook = func(req *ServiceOperationMutationBeginRequest) error {
		if req.Kind != "dns_cluster_configure" {
			return nil
		}
		began = true
		if req.RequestID != saga.RequestID ||
			req.OwnerID != saga.OwnerID ||
			req.Target != "pdns" ||
			req.PackageName != saga.Qualifier {
			return errors.New("same-desired retry replaced its persisted identity")
		}
		return nil
	}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
	if recorder.Code != http.StatusOK {
		t.Fatalf("crash-before-Begin exact retry status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	if !began {
		t.Fatal("crash-before-Begin exact retry did not begin")
	}
	agent.mu.Lock()
	beginCalls, clusterCalls := agent.beginCalls, agent.clusterCalls
	requests := append(
		[]transport.ConfigureDNSClusterV2Request(nil),
		agent.clusterRequests...,
	)
	agent.mu.Unlock()
	if beginCalls != 1 || clusterCalls != 1 || len(requests) != 1 {
		t.Fatalf("crash-before-Begin retry begin/cluster/requests=%d/%d/%d",
			beginCalls, clusterCalls, len(requests))
	}
	if requests[0].MutationRequestID != saga.RequestID ||
		requests[0].MutationOwnerID != saga.OwnerID {
		t.Fatalf("crash-before-Begin retry binding=%+v want request=%s owner=%s",
			requests[0].ServiceMutationBinding, saga.RequestID, saga.OwnerID)
	}
	if pending, err := readPendingDNSClusterSaga(
		context.Background(), p,
	); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("successful crash-before-Begin retry retained saga=%+v", pending)
	}
}

func TestDNSClusterV2DefinitePreBeginFailureCompensatesWithoutHostTouch(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	before, err := readDNSClusterTopology(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	agent := &strictDNSRPCAgent{}
	agent.beginHook = func(req *ServiceOperationMutationBeginRequest) error {
		if req.Kind == "dns_cluster_configure" {
			return errors.New("injected definite Begin rejection")
		}
		return nil
	}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("pre-Begin failure status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	after, err := readDNSClusterTopology(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("pre-Begin compensation before=%+v after=%+v", before, after)
	}
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("pre-Begin compensation retained saga=%+v", pending)
	}
	agent.mu.Lock()
	clusterCalls, syncCalls := agent.clusterCalls, len(agent.syncRequests)
	agent.mu.Unlock()
	if clusterCalls != 0 || syncCalls != 0 {
		t.Fatalf("pre-Begin failure host cluster/sync=%d/%d", clusterCalls, syncCalls)
	}
}

func TestDNSClusterV2PreflightReadinessRejectsBeforeDesiredOrBegin(t *testing.T) {
	for _, test := range []struct {
		name   string
		agent  *strictDNSRPCAgent
		status int
	}{
		{
			name: "not ready",
			agent: &strictDNSRPCAgent{
				readinessDetail: "PowerDNS is installed but not configured",
			},
			status: http.StatusConflict,
		},
		{
			name: "readiness transport error",
			agent: &strictDNSRPCAgent{
				readinessError: errors.New("injected readiness transport loss"),
			},
			status: http.StatusInternalServerError,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "paired")
			before, err := readDNSClusterTopology(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			attachStrictDNSRPCAgent(t, p, test.agent)
			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
			if recorder.Code != test.status {
				t.Fatalf("preflight status=%d want=%d body=%s",
					recorder.Code, test.status, recorder.Body.String())
			}
			after, err := readDNSClusterTopology(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("readiness preflight mutated topology: before=%+v after=%+v",
					before, after)
			}
			if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
				t.Fatal(err)
			} else if pending != nil {
				t.Fatalf("readiness preflight persisted saga=%+v", pending)
			}
			test.agent.mu.Lock()
			beginCalls, clusterCalls :=
				test.agent.beginCalls, test.agent.clusterCalls
			test.agent.mu.Unlock()
			if beginCalls != 0 || clusterCalls != 0 {
				t.Fatalf("readiness preflight begin/cluster=%d/%d",
					beginCalls, clusterCalls)
			}
		})
	}
}

func TestDNSClusterV2ReadinessIsRecheckedAfterWaitingForGlobalLock(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	before, err := readDNSClusterTopology(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	p.serviceMutationMu.Lock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
		done <- recorder
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		agent.mu.Lock()
		versionCalls := agent.versionCalls
		agent.mu.Unlock()
		if versionCalls != 0 {
			break
		}
		if time.Now().After(deadline) {
			p.serviceMutationMu.Unlock()
			t.Fatal("DNS setup did not reach the capability preflight")
		}
		time.Sleep(10 * time.Millisecond)
	}
	agent.mu.Lock()
	agent.readinessDetail = "PowerDNS readiness changed while the request waited"
	agent.mu.Unlock()
	p.serviceMutationMu.Unlock()

	select {
	case recorder := <-done:
		if recorder.Code != http.StatusConflict {
			t.Fatalf("post-lock readiness status=%d body=%s",
				recorder.Code, recorder.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DNS setup did not finish after releasing the global lock")
	}
	after, err := readDNSClusterTopology(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("post-lock readiness rejection changed topology: before=%+v after=%+v",
			before, after)
	}
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("post-lock readiness rejection persisted saga=%+v", pending)
	}
	agent.mu.Lock()
	beginCalls, clusterCalls := agent.beginCalls, agent.clusterCalls
	agent.mu.Unlock()
	if beginCalls != 0 || clusterCalls != 0 {
		t.Fatalf("post-lock readiness rejection begin/cluster=%d/%d",
			beginCalls, clusterCalls)
	}
}

func TestDNSClusterV2GlobalMutationLockSerializesTopologyCalls(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	agent := &strictDNSRPCAgent{
		clusterEntered: entered, clusterRelease: release,
	}
	attachStrictDNSRPCAgent(t, p, agent)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
		firstDone <- recorder
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first topology call did not enter V2 agent")
	}
	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		p.handleDNSSetup(recorder, dnsSetupAdminRequest(pairedDNSSetupBody))
		secondDone <- recorder
	}()
	time.Sleep(100 * time.Millisecond)
	agent.mu.Lock()
	beforeRelease := agent.clusterCalls
	agent.mu.Unlock()
	if beforeRelease != 1 {
		t.Fatalf("global lock admitted %d concurrent topology calls", beforeRelease)
	}
	close(release)
	for index, result := range []<-chan *httptest.ResponseRecorder{
		firstDone, secondDone,
	} {
		select {
		case recorder := <-result:
			if recorder.Code != http.StatusOK {
				t.Fatalf("serialized request %d status=%d body=%s",
					index, recorder.Code, recorder.Body.String())
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("serialized request %d did not finish", index)
		}
	}
}

func TestPendingDNSClusterSagaGatesDNSDNSSECAndPowerDNSMutations(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	domainID := seedDNSSECManagedDomain(t, p, "pending-topology.example")
	_ = pendingDNSClusterSagaFixture(t, p, "example.net")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	if err := p.syncZoneToDNS(
		context.Background(), "pending-topology.example", false,
	); err == nil || !strings.Contains(err.Error(), "topology is pending") {
		t.Fatalf("pending topology DNS publication error=%v", err)
	}
	dnssec := postDNSSECV2(t, p, domainID)
	if dnssec.Code != http.StatusConflict {
		t.Fatalf("pending topology DNSSEC status=%d body=%s",
			dnssec.Code, dnssec.Body.String())
	}
	pdns := httptest.NewRecorder()
	p.handlePDNSEnable(pdns, strictDNSAdminRequest(httptest.NewRequest(
		http.MethodPost, "/api/v1/pdns/enable", nil,
	)))
	if pdns.Code != http.StatusConflict {
		t.Fatalf("pending topology PowerDNS status=%d body=%s",
			pdns.Code, pdns.Body.String())
	}
	install := httptest.NewRecorder()
	installRequest := strictDNSAdminRequest(httptest.NewRequest(
		http.MethodPost,
		"/api/v1/services/install",
		strings.NewReader(
			`{"service_id":"pdns","request_id":"0123456789abcdef0123456789abcdef"}`,
		),
	))
	p.handleServiceInstall(install, installRequest)
	if install.Code != http.StatusConflict {
		t.Fatalf("pending topology PDNS install status=%d body=%s",
			install.Code, install.Body.String())
	}
	var serviceRows int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM service_operations`,
	).Scan(&serviceRows); err != nil {
		t.Fatal(err)
	}
	if serviceRows != 0 {
		t.Fatalf("pending topology created %d service operation rows", serviceRows)
	}
	agent.mu.Lock()
	beginCalls, syncCalls := agent.beginCalls, len(agent.syncRequests)
	secureCalls, powerCalls := agent.secureDNSCalls, agent.powerDNSCalls
	agent.mu.Unlock()
	if beginCalls != 0 || syncCalls != 0 ||
		secureCalls != 0 || powerCalls != 0 {
		t.Fatalf("pending topology host begin/sync/secure/pdns=%d/%d/%d/%d",
			beginCalls, syncCalls, secureCalls, powerCalls)
	}
}

func TestDNSClusterStartupFinalizesExactPublishedActiveChild(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	saga := pendingDNSClusterSagaFixture(t, p, "example.net")
	now := time.Now().UTC()
	job := &ServiceOperationMutationJob{
		RequestID: saga.RequestID, OwnerID: saga.OwnerID,
		Kind: "dns_cluster_configure", Target: "pdns",
		PackageName: saga.Qualifier, Status: agentMutationRunning,
		LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour),
	}
	agent := &strictDNSRPCAgent{}
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		saga.RequestID: job,
	}
	agent.durableMutationRPCFixture.active = saga.RequestID
	attachStrictDNSRPCAgent(t, p, agent)
	go func() {
		time.Sleep(50 * time.Millisecond)
		publishStrictDNSClusterJob(agent, saga)
	}()

	if _, err := p.recoverInterruptedServiceOperations(context.Background()); err != nil {
		t.Fatalf("observe exact active cluster child: %v", err)
	}
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("startup retained exact published saga=%+v", pending)
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1: saga.Desired.NS1, settingNS2: saga.Desired.NS2,
		settingDNSRole:   saga.Desired.Role,
		settingDNSPeerIP: saga.Desired.PeerIP,
		settingDNSPeerNS: saga.Desired.PeerNS,
	})
	agent.mu.Lock()
	cancelCalls, clusterCalls := agent.cancelCalls, agent.clusterCalls
	syncCalls := len(agent.syncRequests)
	agent.mu.Unlock()
	if cancelCalls != 0 || clusterCalls != 0 || syncCalls != 0 {
		t.Fatalf("startup observation mutated host cancel/cluster/sync=%d/%d/%d",
			cancelCalls, clusterCalls, syncCalls)
	}
}

func TestDNSClusterStartupRejectsSucceededReceiptWithoutPublishedPhase(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	saga := pendingDNSClusterSagaFixture(t, p, "example.net")
	agent := &strictDNSRPCAgent{}
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		saga.RequestID: {
			RequestID: saga.RequestID, OwnerID: saga.OwnerID,
			Kind: "dns_cluster_configure", Target: "pdns",
			PackageName: saga.Qualifier, Status: agentMutationSucceeded,
			Phase: "completed",
		},
	}
	attachStrictDNSRPCAgent(t, p, agent)

	if _, err := p.recoverInterruptedServiceOperations(
		context.Background(),
	); err == nil || !strings.Contains(err.Error(), "canonical published receipt") {
		t.Fatalf("non-published succeeded receipt error=%v", err)
	}
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending == nil || !reflect.DeepEqual(*pending, saga) {
		t.Fatalf("non-published success changed saga=%+v", pending)
	}
	agent.mu.Lock()
	clusterCalls, syncCalls := agent.clusterCalls, len(agent.syncRequests)
	agent.mu.Unlock()
	if clusterCalls != 0 || syncCalls != 0 {
		t.Fatalf("non-published success host cluster/sync=%d/%d",
			clusterCalls, syncCalls)
	}
}

func TestDNSClusterStartupCompensatesProvenPreCommitFailure(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	saga := pendingDNSClusterSagaFixture(t, p, "example.net")
	agent := &strictDNSRPCAgent{}
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		saga.RequestID: {
			RequestID: saga.RequestID, OwnerID: saga.OwnerID,
			Kind: "dns_cluster_configure", Target: "pdns",
			PackageName: saga.Qualifier, Status: agentMutationFailed,
			Phase: "failed",
		},
	}
	attachStrictDNSRPCAgent(t, p, agent)

	if _, err := p.recoverInterruptedServiceOperations(context.Background()); err != nil {
		t.Fatalf("compensate exact pre-commit failure: %v", err)
	}
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("pre-commit failure retained saga=%+v", pending)
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1: saga.Previous.RawNS1, settingNS2: saga.Previous.RawNS2,
		settingDNSRole:   saga.Previous.RawRole,
		settingDNSPeerIP: saga.Previous.RawPeerIP,
		settingDNSPeerNS: saga.Previous.RawPeerNS,
	})
	agent.mu.Lock()
	beginCalls, clusterCalls := agent.beginCalls, agent.clusterCalls
	agent.mu.Unlock()
	if beginCalls != 0 || clusterCalls != 0 {
		t.Fatalf("startup compensation touched host begin/cluster=%d/%d",
			beginCalls, clusterCalls)
	}
}

func TestDNSClusterStartupRetainsCrashBeforeBeginMarker(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	saga := pendingDNSClusterSagaFixture(t, p, "example.net")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)
	if _, err := p.recoverInterruptedServiceOperations(context.Background()); err != nil {
		t.Fatalf("observe crash-before-Begin saga: %v", err)
	}
	pending, err := readPendingDNSClusterSaga(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || !reflect.DeepEqual(*pending, saga) {
		t.Fatalf("crash-before-Begin saga changed: before=%+v after=%+v", saga, pending)
	}
	agent.mu.Lock()
	beginCalls, clusterCalls := agent.beginCalls, agent.clusterCalls
	agent.mu.Unlock()
	if beginCalls != 0 || clusterCalls != 0 {
		t.Fatalf("crash-before-Begin startup touched host begin/cluster=%d/%d",
			beginCalls, clusterCalls)
	}
}

func TestDNSClusterPendingSagaAllowsOrphanFirewallRecoveryButSkipsDNS(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	fixture := newServiceOperationTestFixture(t)
	setDNSIdentityForTest(t, fixture.panel, "paired")
	seedStrictDNSZone(t, fixture.panel, "pending-firewall.example")
	saga := pendingDNSClusterSagaFixture(t, fixture.panel, "example.net")
	interrupted := seedActiveFirewallChild(t, fixture, serviceOperation{})

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 0 {
		t.Fatalf("pending-saga firewall recovery recovered=%d err=%v",
			recovered, err)
	}
	fixture.agent.mu.Lock()
	after := cloneServiceOperationMutationJob(
		fixture.agent.mutationJobs[interrupted.RequestID],
	)
	dnsCalls := len(fixture.agent.dnsV2Requests)
	firewallCalls := fixture.agent.firewallCalls
	fixture.agent.mu.Unlock()
	if after == nil || after.Status != agentMutationFailed {
		t.Fatalf("pending-saga orphan firewall child=%+v", after)
	}
	if firewallCalls != 0 {
		t.Fatalf("pending-saga recovery fabricated firewall payload %d time(s)",
			firewallCalls)
	}
	if dnsCalls != 0 {
		t.Fatalf("pending-saga recovery published DNS %d time(s)", dnsCalls)
	}
	if pending, err := readPendingDNSClusterSaga(
		context.Background(), fixture.panel,
	); err != nil {
		t.Fatal(err)
	} else if pending == nil || !reflect.DeepEqual(*pending, saga) {
		t.Fatalf("unrelated firewall recovery changed cluster saga=%+v", pending)
	}
}

func TestDNSClusterPendingSagaAllowsStaleVPNLeaseRecoveryButSkipsDNS(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	fixture := newServiceOperationTestFixture(t)
	prepareWireGuardOperationFixture(t, fixture)
	setDNSIdentityForTest(t, fixture.panel, "paired")
	seedStrictDNSZone(t, fixture.panel, "pending-vpn.example")
	saga := pendingDNSClusterSagaFixture(t, fixture.panel, "example.net")
	if _, err := fixture.database.GetDB().Exec(`
		UPDATE vpn_sync_state
		SET status = 'pending', lease_token = 'crashed-panel-token',
		    lease_expires_at = datetime('now', '+2 minutes')
		WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	recovered, err := fixture.panel.recoverInterruptedServiceOperations(
		context.Background(),
	)
	if err != nil || recovered != 0 {
		t.Fatalf("pending-saga VPN recovery recovered=%d err=%v",
			recovered, err)
	}
	assertRecoveredVPNStateApplied(t, fixture)
	fixture.agent.mu.Lock()
	dnsCalls := len(fixture.agent.dnsV2Requests)
	fixture.agent.mu.Unlock()
	if dnsCalls != 0 {
		t.Fatalf("pending-saga VPN recovery published DNS %d time(s)", dnsCalls)
	}
	if pending, err := readPendingDNSClusterSaga(
		context.Background(), fixture.panel,
	); err != nil {
		t.Fatal(err)
	} else if pending == nil || !reflect.DeepEqual(*pending, saga) {
		t.Fatalf("VPN recovery changed pending cluster saga=%+v", pending)
	}
}
