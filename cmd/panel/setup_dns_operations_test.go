package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func setupDNSTestDraft() serverSetupDraft {
	return serverSetupDraft{Purpose: "web", DNSMode: setupDNSModeLocal, DNSEngine: "bind", DNSRole: "primary", NS1: "ns1.example.test", NS2: "ns2.example.test", LocalIP: "192.0.2.1", PeerIP: "192.0.2.2", PeerNS: "ns2.example.test"}
}

func TestSetupDNSLocalInstallReplaysExactChildWithoutReinstallation(t *testing.T) {
	p := newDNSPanelForTest(t)
	seedSetupDNSOwner(t, p)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	draft := setupDNSTestDraft()
	requestID := strings.Repeat("a", 32)
	for i := 0; i < 2; i++ {
		if err := p.startServerSetupDNS(context.Background(), draft, requestID, serviceOperationActor{UserID: 1}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	status, err := p.serverSetupDNSOperationStatus(context.Background(), requestID)
	if err != nil || status != "succeeded" {
		t.Fatalf("status=%q %v", status, err)
	}
	state, err := readDNSEngineDBState(context.Background(), p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND || state.PairRole != transport.DNSPairRolePrimary || state.PeerIP != draft.PeerIP {
		t.Fatalf("state=%+v", state)
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("duplicate authoritative install: %d", calls)
	}
	changed := draft
	changed.PeerIP = "192.0.2.3"
	if err := p.startServerSetupDNS(context.Background(), changed, requestID, serviceOperationActor{UserID: 1}); err == nil {
		t.Fatal("replay accepted changed topology")
	}
	changed = draft
	changed.DNSEngine = "pdns"
	if err := p.startServerSetupDNS(context.Background(), changed, strings.Repeat("b", 32), serviceOperationActor{UserID: 1}); err == nil {
		t.Fatal("setup replaced existing DNS authority")
	}
	if ready, err := p.setupDNSModeReadiness(context.Background(), setupDNSModeLocal); err != nil || !ready {
		t.Fatalf("verified pair readiness=%v %v", ready, err)
	}
	agent.mu.Lock()
	runtime := agent.runtimes[transport.DNSEngineBIND]
	runtime.PairReady = false
	agent.runtimes[transport.DNSEngineBIND] = runtime
	agent.mu.Unlock()
	if ready, err := p.setupDNSModeReadiness(context.Background(), setupDNSModeLocal); err != nil || ready {
		t.Fatalf("missing peer readiness=%v %v", ready, err)
	}
}

func TestSetupDNSLocalDoesNotAdoptUnmanagedServices(t *testing.T) {
	p := newDNSPanelForTest(t)
	seedSetupDNSOwner(t, p)
	agent := newDNSEngineTestAgent()
	runtime := agent.runtimes[transport.DNSEngineBIND]
	runtime.Installed = true
	runtime.Running = true
	agent.runtimes[transport.DNSEngineBIND] = runtime
	attachDNSEngineTestAgent(t, p, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	if err := p.startServerSetupDNS(context.Background(), setupDNSTestDraft(), strings.Repeat("c", 32), serviceOperationActor{UserID: 1}); err == nil {
		t.Fatal("unmanaged authority silently adopted")
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 0 {
		t.Fatalf("unmanaged mutation calls=%d", calls)
	}
	var count int
	if err := p.db.GetDB().QueryRow(`SELECT count(*) FROM dns_engine_switch_snapshots`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unmanaged install snapshots=%d %v", count, err)
	}
}

func TestSetupDNSResumesAfterIdentityStagingBeforeSwitchPersistence(t *testing.T) {
	p := newDNSPanelForTest(t)
	seedSetupDNSOwner(t, p)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	draft := setupDNSTestDraft()
	ctx := context.Background()
	request, local, err := setupDNSIdentity(draft)
	if err != nil {
		t.Fatal(err)
	}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.stageDNSClusterSettingsAndReconcile(ctx, state, dnsIdentityStagingFresh, request.Role, request.PeerIP, request.PeerNS, request.NS1, request.NS2, local); err != nil {
		t.Fatal(err)
	}
	staged, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil || staged.Revision != state.Revision+1 {
		t.Fatalf("staging=%+v %v", staged, err)
	}
	// New process-local state, same durable DB and agent: no child exists yet.
	restarted := &Panel{db: p.db, agentClient: p.agentClient}
	if err := restarted.startServerSetupDNS(ctx, draft, strings.Repeat("d", 32), serviceOperationActor{UserID: 1}); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("installs=%d", calls)
	}
}

func TestSetupDNSSecondaryCompletionNeedsConsumerProofWithoutPublisherAuthority(t *testing.T) {
	p := newDNSPanelForTest(t)
	seedSetupDNSOwner(t, p)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	draft := setupDNSTestDraft()
	draft.Purpose = "dns"
	draft.DNSRole = transport.DNSPairRoleSecondary
	draft.PeerNS = draft.NS1
	if err := p.startServerSetupDNS(context.Background(), draft, strings.Repeat("e", 32), serviceOperationActor{UserID: 1}); err != nil {
		t.Fatal(err)
	}
	if ready, err := p.setupDNSModeReadiness(context.Background(), setupDNSModeLocal); err != nil || ready {
		t.Fatalf("unverified secondary readiness=%v %v", ready, err)
	}
	agent.mu.Lock()
	runtime := agent.runtimes[transport.DNSEngineBIND]
	runtime.SecondaryReady = true
	agent.runtimes[transport.DNSEngineBIND] = runtime
	agent.mu.Unlock()
	if ready, err := p.setupDNSModeReadiness(context.Background(), setupDNSModeLocal); err != nil || !ready {
		t.Fatalf("verified secondary readiness=%v %v", ready, err)
	}
	if _, ready, err := p.activeDNSPublisher(context.Background()); err != nil || ready {
		t.Fatalf("secondary gained publication permission: ready=%v err=%v", ready, err)
	}
}

func TestSetupDNSManualSecondaryHostingUsesNativeProofAndExternalDomainDefault(t *testing.T) {
	for _, engine := range []string{"bind", "pdns"} {
		t.Run(engine, func(t *testing.T) {
			p := newDNSPanelForTest(t)
			seedSetupDNSOwner(t, p)
			agent := newDNSEngineTestAgent()
			attachDNSEngineTestAgent(t, p, agent)
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
			draft := setupDNSTestDraft()
			draft.DNSRole, draft.PeerNS, draft.DNSEngine = "secondary", draft.NS1, engine
			draft.DNSHostingManagement = "manual"
			ctx := context.Background()
			oldExchange := remoteDNSExchange
			remoteDNSExchange = func(context.Context, string, string, string, any, any) error {
				t.Fatal("standard replication called remote panel")
				return nil
			}
			defer func() { remoteDNSExchange = oldExchange }()
			oldResolvers := setupDNSPublicResolvers
			setupDNSPublicResolvers = func() []hostResolver {
				return []hostResolver{setupNameserverResolver{draft.NS1: {draft.PeerIP}, draft.NS2: {draft.LocalIP}}}
			}
			defer func() { setupDNSPublicResolvers = oldResolvers }()
			for i := 0; i < 2; i++ {
				if err := p.startServerSetupDNS(ctx, draft, strings.Repeat("f", 32), serviceOperationActor{UserID: 1}); err != nil {
					t.Fatal(err)
				}
			}
			mode, err := p.setupDNSManagementMode(ctx)
			if err != nil || mode != setupDNSModeExternal {
				t.Fatalf("domain default=%s %v", mode, err)
			}
			if ready, err := p.serverSetupManualSecondaryHostingReadiness(ctx, draft); err != nil || ready {
				t.Fatalf("unverified secondary accepted: %v %v", ready, err)
			}
			agent.mu.Lock()
			runtime := agent.runtimes[transport.DNSEngine(engine)]
			runtime.SecondaryReady = true
			agent.runtimes[transport.DNSEngine(engine)] = runtime
			calls := agent.switchCalls
			agent.mu.Unlock()
			if calls != 1 {
				t.Fatal("replay reinstalled native DNS")
			}
			if ready, err := p.serverSetupManualSecondaryHostingReadiness(ctx, draft); err != nil || !ready {
				t.Fatalf("native pair proof rejected: %v %v", ready, err)
			}
			p.license = testPanelLicense(t, "active")
			plan := serverSetupPlan{Draft: draft}
			step := &serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "dns_readiness", Target: "secondary"}}
			if done, err := p.runServerSetupStep(ctx, plan, step); err != nil || !done {
				t.Fatalf("native gate did not proceed: %v %v", done, err)
			}
			if err := p.saveSetupDNSManagementMode(ctx, setupDNSModeLocal); err != nil {
				t.Fatal(err)
			}
			if done, err := p.runServerSetupStep(ctx, plan, step); done || err != errServerSetupDNSReadinessRequired {
				t.Fatalf("owner default change ignored: %v %v", done, err)
			}
			if _, ready, err := p.activeDNSPublisher(ctx); err != nil || ready {
				t.Fatalf("secondary gained write authority: %v %v", ready, err)
			}
		})
	}
}
