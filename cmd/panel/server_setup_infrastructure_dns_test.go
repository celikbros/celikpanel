package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/rpc"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func infrastructureDNSTestDraft() serverSetupDraft {
	d := setupDNSTestDraft()
	d.PanelDomain = "frankfurt.example.test"
	d.InfrastructureDNS = &serverSetupInfrastructureDNSDraft{Zone: "example.test", PeerPanelDomain: "boston.example.test"}
	return d
}

func TestServerSetupInfrastructureDNSReviewHasOnlyExplicitAccessRecords(t *testing.T) {
	p := newDNSPanelForTest(t)
	d := infrastructureDNSTestDraft()
	for _, mail := range []bool{false, true} {
		if mail {
			d.Purpose = "web_mail"
			d.MailHostname = "mail.frankfurt.example.test"
		}
		plan, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"frankfurt.example.test": d.LocalIP, "boston.example.test": d.PeerIP, "ns1.example.test": d.LocalIP, "ns2.example.test": d.PeerIP}
		if mail {
			want[d.MailHostname] = d.LocalIP
		}
		addresses := map[string]string{}
		for _, r := range plan.Records {
			if r.Action != "add" {
				t.Fatalf("unexpected new record action: %+v", r)
			}
			switch r.Type {
			case "SOA", "NS":
			case "A":
				addresses[r.Name] = r.Content
			default:
				t.Fatalf("unrelated default seeded: %+v", r)
			}
		}
		if !reflect.DeepEqual(addresses, want) {
			t.Fatalf("addresses=%v want=%v", addresses, want)
		}
		var zones, domains, receipts int
		p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains`).Scan(&zones)
		p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&domains)
		p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM panel_settings WHERE key LIKE 'server_setup_infrastructure_%'`).Scan(&receipts)
		if zones+domains+receipts != 0 {
			t.Fatalf("read-only review mutated DB: %d %d %d", zones, domains, receipts)
		}
	}
}

func TestServerSetupInfrastructureDNSRejectsWrongScopeAndOwnership(t *testing.T) {
	for _, scenario := range []string{"secondary", "external", "outside_zone", "same_peer_hostname", "external_owner", "unclaimed_zone", "child_zone", "parent_zone", "invalid_ipv4"} {
		t.Run(scenario, func(t *testing.T) {
			p := newDNSPanelForTest(t)
			d := infrastructureDNSTestDraft()
			switch scenario {
			case "invalid_ipv4":
				d.LocalIP = "invalid-ip"
			case "secondary":
				d.DNSRole = "secondary"
			case "external":
				d.DNSMode = "external"
			case "outside_zone":
				d.InfrastructureDNS.Zone = "other.test"
			case "same_peer_hostname":
				d.InfrastructureDNS.PeerPanelDomain = d.PanelDomain
			case "external_owner":
				sub := seedSetupDNSOwner(t, p)
				seedSetupDNSDomain(t, p, sub, d.InfrastructureDNS.Zone, setupDNSModeExternal)
			case "unclaimed_zone":
				seedInfrastructureDNSZone(t, p, d.InfrastructureDNS.Zone)
			case "child_zone":
				seedInfrastructureDNSZone(t, p, "delegated.example.test")
			case "parent_zone":
				seedInfrastructureDNSZone(t, p, "example.test")
				d.InfrastructureDNS.Zone = "frankfurt.example.test"
			}
			_, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), d)
			if err == nil {
				t.Fatalf("accepted %s", scenario)
			}
			if scenario == "invalid_ipv4" {
				var reviewError *serverSetupInfrastructureDNSPlanError
				if !errors.As(err, &reviewError) || reviewError.Message != "local DNS IPv4 address is invalid" {
					t.Fatalf("invalid input did not become a safe review blocker: %v", err)
				}
			}
		})
	}
}

func infrastructureDNSOwnedZone(t *testing.T, p *Panel, d serverSetupDraft) {
	t.Helper()
	sub := seedSetupDNSOwner(t, p)
	seedSetupDNSDomain(t, p, sub, d.InfrastructureDNS.Zone, setupDNSModeLocal)
	seedInfrastructureDNSZone(t, p, d.InfrastructureDNS.Zone)
	// The unrelated TXT and MX remain exactly as the owner configured them.
	for _, r := range []serverSetupInfrastructureDNSRecord{
		{Name: d.InfrastructureDNS.Zone, Type: "TXT", Content: "owner-verification", TTL: 777},
		{Name: d.InfrastructureDNS.Zone, Type: "MX", Content: "mx.external.test", TTL: 888},
	} {
		if _, err := p.db.GetDB().Exec(`INSERT INTO pdns_records(domain_id,name,type,content,ttl) SELECT id,?,?,?,? FROM pdns_domains WHERE name=?`, r.Name, r.Type, r.Content, r.TTL, d.InfrastructureDNS.Zone); err != nil {
			t.Fatal(err)
		}
	}
}

func TestServerSetupInfrastructureDNSPreservesOwnedRecordsAndBlocksConflicts(t *testing.T) {
	for _, scenario := range []string{"unchanged", "address", "cname", "dname", "delegation", "disabled", "nameserver"} {
		t.Run(scenario, func(t *testing.T) {
			p := newDNSPanelForTest(t)
			d := infrastructureDNSTestDraft()
			infrastructureDNSOwnedZone(t, p, d)
			name, typ, content, disabled := d.PanelDomain, "A", d.LocalIP, 0
			switch scenario {
			case "address":
				content = "192.0.2.9"
			case "cname":
				typ = "CNAME"
				content = "elsewhere.test"
			case "dname":
				name = d.InfrastructureDNS.Zone
				typ = "DNAME"
				content = "elsewhere.test"
			case "delegation":
				typ = "NS"
				content = "other-ns.test"
			case "disabled":
				disabled = 1
			case "nameserver":
				name = d.InfrastructureDNS.Zone
				typ = "NS"
				content = "other-ns.test"
			}
			if _, err := p.db.GetDB().Exec(`INSERT INTO pdns_records(domain_id,name,type,content,ttl,disabled) SELECT id,?,?,?,300,? FROM pdns_domains WHERE name=?`, name, typ, content, disabled, d.InfrastructureDNS.Zone); err != nil {
				t.Fatal(err)
			}
			before, err := readServerSetupInfrastructureDNSSnapshot(context.Background(), p.db.GetDB(), d.InfrastructureDNS.Zone)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), d)
			if scenario != "unchanged" {
				if err == nil {
					t.Fatalf("accepted %s conflict", scenario)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				keep := false
				for _, r := range plan.Records {
					if r.Name == d.PanelDomain && r.Type == "A" {
						keep = r.Action == "keep"
					}
				}
				if !keep {
					t.Fatal("matching record not preserved")
				}
			}
			after, err := readServerSetupInfrastructureDNSSnapshot(context.Background(), p.db.GetDB(), d.InfrastructureDNS.Zone)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("review changed owner records")
			}
		})
	}
}

func infrastructureDNSRuntimeFixture(t *testing.T) (*Panel, *dnsZoneV3TestAgent, serverSetupPlan, serverSetupExecutionStep) {
	return infrastructureDNSRuntimeFixtureEngine(t, "bind")
}

func infrastructureDNSRuntimeFixtureEngine(t *testing.T, engine string) (*Panel, *dnsZoneV3TestAgent, serverSetupPlan, serverSetupExecutionStep) {
	t.Helper()
	p := newDNSPanelForTest(t)
	p.license = testPanelLicense(t, "active")
	seedSetupDNSOwner(t, p)
	d := infrastructureDNSTestDraft()
	d.DNSEngine = engine
	engineAgent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, engineAgent)
	t.Setenv("CELIKPANEL_SERVER_IP", d.LocalIP)
	if err := p.startServerSetupDNS(context.Background(), d, strings.Repeat("a", 32), serviceOperationActor{UserID: 1}); err != nil {
		t.Fatal(err)
	}
	agent := newDNSZoneV3TestAgent()
	agent.pairReady = true
	agent.publisher = transport.DNSEngine(engine)
	attachInfrastructureDNSAgent(t, p, agent)
	infra, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	plan := serverSetupPlan{Version: serverSetupPlanVersion, BuildCommit: strings.TrimSpace(buildCommit), Draft: d, InfrastructureDNS: infra, Steps: []serverSetupPlanStep{{ID: "03-infrastructure_dns", Kind: "infrastructure_dns", Target: infra.Zone}}}
	plan.ID = serverSetupPlanIdentity(plan)
	step := serverSetupExecutionStep{serverSetupPlanStep: plan.Steps[0], RequestID: strings.Repeat("b", 32), OwnerID: strings.Repeat("c", 32)}
	return p, agent, plan, step
}

func TestServerSetupInfrastructureDNSPublishesOnceWithoutTenant(t *testing.T) {
	p, agent, plan, step := infrastructureDNSRuntimeFixture(t)
	for i := 0; i < 2; i++ {
		done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step)
		if err != nil || !done {
			t.Fatalf("run %d done=%v err=%v", i, done, err)
		}
	}
	agent.mu.Lock()
	calls := len(agent.requests)
	payload := append([]transport.ZoneRecord(nil), agent.requests[0].Records...)
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("published %d times", calls)
	}
	var tenants int
	p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&tenants)
	if tenants != 0 {
		t.Fatal("wizard created a tenant")
	}
	for _, r := range payload {
		if r.Type == "MX" || r.Type == "TXT" || r.Name == "www.example.test" {
			t.Fatalf("unrelated record %+v", r)
		}
	}
	// An owner edit after completion is neither lost nor overwritten on resume.
	if _, err := p.db.GetDB().Exec(`UPDATE pdns_records SET content='192.0.2.99' WHERE name=? AND type='A'`, plan.Draft.PanelDomain); err != nil {
		t.Fatal(err)
	}
	if _, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); !errors.Is(err, errServerSetupInfrastructureDNSChanged) {
		t.Fatalf("changed record: %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.requests) != 1 {
		t.Fatal("republished after owner edit")
	}
}

func TestServerSetupInfrastructureDNSWaitsForPairBeforeAnyWrite(t *testing.T) {
	p, agent, plan, step := infrastructureDNSRuntimeFixture(t)
	agent.pairReady = false
	done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step)
	if done || !errors.Is(err, errServerSetupInfrastructureDNSWaiting) {
		t.Fatalf("done=%v err=%v", done, err)
	}
	var n int
	p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains`).Scan(&n)
	if n != 0 {
		t.Fatal("prepared zone before pair readiness")
	}
	if len(agent.requests) != 0 {
		t.Fatal("published before pair readiness")
	}
}

func TestServerSetupInfrastructureDNSReviewDriftAndIdentityAreRejected(t *testing.T) {
	for _, scenario := range []string{"changed_plan", "changed_step", "new_zone", "changed_address"} {
		t.Run(scenario, func(t *testing.T) {
			p, agent, plan, step := infrastructureDNSRuntimeFixture(t)
			switch scenario {
			case "changed_plan":
				plan.InfrastructureDNS.Records[0].Content = "changed"
			case "changed_step":
				step.Target = "other.test"
			case "new_zone":
				seedInfrastructureDNSZone(t, p, plan.InfrastructureDNS.Zone)
			case "changed_address":
				plan.Draft.LocalIP = "192.0.2.9"
				plan.ID = serverSetupPlanIdentity(plan)
			}
			if _, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); err == nil {
				t.Fatal("accepted changed review")
			}
			if len(agent.requests) != 0 {
				t.Fatal("published changed review")
			}
		})
	}
}

func TestServerSetupInfrastructureDNSResumesPreparedReceipt(t *testing.T) {
	p, agent, plan, step := infrastructureDNSRuntimeFixture(t)
	publisher, ready, err := p.activeDNSPublisher(context.Background())
	if err != nil || !ready {
		t.Fatal(err)
	}
	receipt, err := p.prepareServerSetupInfrastructureDNSRecords(context.Background(), plan, step, publisher)
	if err != nil {
		t.Fatal(err)
	}
	// Simulates process loss after the atomic desired-record/receipt transaction.
	raw, _ := json.Marshal(receipt)
	if !strings.Contains(string(raw), step.RequestID) {
		t.Fatal("receipt omitted exact child")
	}
	if done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); err != nil || !done {
		t.Fatalf("resume done=%v err=%v", done, err)
	}
	if len(agent.requests) != 1 {
		t.Fatal("resume duplicated publication")
	}
}

func TestServerSetupInfrastructureDNSUnknownPublicationKeepsExactLease(t *testing.T) {
	p, agent, plan, step := infrastructureDNSRuntimeFixture(t)
	configureDNSZoneV3PendingSync(agent, false)
	configureDNSZoneV3PendingRecovery(agent)
	done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step)
	if done || !errors.Is(err, errServerSetupInfrastructureDNSUnknown) {
		t.Fatalf("pending done=%v err=%v", done, err)
	}
	lease, err := readDNSZoneEngineLease(context.Background(), p.db.GetDB(), plan.InfrastructureDNS.Zone)
	if err != nil {
		t.Fatal(err)
	}
	agent.pairReady = false
	configureDNSZoneV3PendingRecovery(agent)
	done, err = p.runServerSetupInfrastructureDNS(context.Background(), plan, step)
	if done || !errors.Is(err, errServerSetupInfrastructureDNSUnknown) {
		t.Fatalf("reconcile done=%v err=%v", done, err)
	}
	after, err := readDNSZoneEngineLease(context.Background(), p.db.GetDB(), plan.InfrastructureDNS.Zone)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lease, after) {
		t.Fatal("replaced pending exact lease")
	}
	if len(agent.requests) != 1 {
		t.Fatal("duplicate publication during reconciliation")
	}
}

func seedInfrastructureDNSZone(t *testing.T, p *Panel, name string) {
	t.Helper()
	if _, err := p.db.GetDB().Exec(`INSERT INTO pdns_domains(name,type) VALUES(?,'MASTER')`, name); err != nil {
		t.Fatal(err)
	}
	for _, record := range []serverSetupInfrastructureDNSRecord{
		{Name: name, Type: "SOA", Content: "ns1.example.test hostmaster.example.test 1 3600 600 604800 300", TTL: 3600},
		{Name: name, Type: "NS", Content: "ns1.example.test", TTL: 3600},
		{Name: name, Type: "NS", Content: "ns2.example.test", TTL: 3600},
	} {
		if _, err := p.db.GetDB().Exec(`INSERT INTO pdns_records(domain_id,name,type,content,ttl) SELECT id,?,?,?,? FROM pdns_domains WHERE name=?`, record.Name, record.Type, record.Content, record.TTL, name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestServerSetupInfrastructureDNSKnownFailureRequiresNewReviewedIdentity(t *testing.T) {
	p, agent, plan, step := infrastructureDNSRuntimeFixture(t)
	agent.syncResponseHook = func(transport.SyncDNSZoneV3Request, *transport.SyncDNSZoneV3Response) error {
		return errors.New("injected precommit failure")
	}
	done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step)
	if done || err == nil || errors.Is(err, errServerSetupInfrastructureDNSUnknown) || errors.Is(err, errServerSetupInfrastructureDNSWaiting) {
		t.Fatalf("failure done=%v err=%v", done, err)
	}
	calls := len(agent.requests)
	agent.syncResponseHook = nil
	if done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); done || err == nil {
		t.Fatal("known failure silently retried")
	}
	if len(agent.requests) != calls {
		t.Fatal("repeated mutation for failed exact plan")
	}
	// A new explicit review sees preserved desired records. It can retry after
	// the cause is resolved without duplicating them or adopting the old failure.
	revised, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), plan.Draft)
	if err != nil {
		t.Fatal(err)
	}
	plan.InfrastructureDNS = revised
	plan.Revision++
	plan.ID = serverSetupPlanIdentity(plan)
	step.RequestID = strings.Repeat("d", 32)
	step.OwnerID = strings.Repeat("e", 32)
	publisher, ready, err := p.activeDNSPublisher(context.Background())
	if err != nil || !ready {
		t.Fatal(err)
	}
	if _, err := p.prepareServerSetupInfrastructureDNSRecords(context.Background(), plan, step, publisher); err != nil {
		t.Fatal(err)
	}
	// Crash just after the new receipt still leaves old state.error but no new
	// attempted generation; resume must not attach that old failure to this plan.
	if done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); !done || err != nil {
		t.Fatalf("new review done=%v err=%v", done, err)
	}
	if len(agent.requests) != calls+1 {
		t.Fatal("new review did not publish once")
	}
}

func TestServerSetupInfrastructureDNSDNSSECEvidenceBlocksUnsignedPreparation(t *testing.T) {
	p := newDNSPanelForTest(t)
	d := infrastructureDNSTestDraft()
	infrastructureDNSOwnedZone(t, p, d)
	if _, err := p.db.GetDB().Exec(`INSERT INTO pdns_domainmetadata(domain_id,kind,content) SELECT id,'PRESIGNED','1' FROM pdns_domains WHERE name=?`, d.InfrastructureDNS.Zone); err != nil {
		t.Fatal(err)
	}
	_, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), d)
	var conflict *serverSetupInfrastructureDNSPlanError
	if !errors.As(err, &conflict) || !strings.Contains(conflict.Message, "DNSSEC") {
		t.Fatalf("DNSSEC evidence not blocked: %v", err)
	}
}

func TestServerSetupInfrastructureDNSExplicitEmptyZoneIsNotSilentlySkipped(t *testing.T) {
	p := newDNSPanelForTest(t)
	d := infrastructureDNSTestDraft()
	d.InfrastructureDNS.Zone = ""
	if _, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), d); err == nil {
		t.Fatal("empty opted-in zone silently omitted")
	}
	d.InfrastructureDNS = nil
	if plan, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), d); err != nil || plan != nil {
		t.Fatalf("nil option changed legacy plan: %+v %v", plan, err)
	}
}

func TestServerSetupInfrastructureDNSBuildChangeBlocksNewMutationButNotExactResume(t *testing.T) {
	p, agent, plan, step := infrastructureDNSRuntimeFixture(t)
	plan.BuildCommit = "old-uninstalled-build"
	plan.ID = serverSetupPlanIdentity(plan)
	if _, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); !errors.Is(err, errServerSetupBuildChanged) {
		t.Fatalf("build change error=%v", err)
	}
	if len(agent.requests) != 0 {
		t.Fatal("new mutation after build change")
	}
	// This represents records prepared before the build changed, for the same
	// immutable accepted step. Exact reconciliation remains allowed.
	publisher, ready, err := p.activeDNSPublisher(context.Background())
	if err != nil || !ready {
		t.Fatal(err)
	}
	if _, err := p.prepareServerSetupInfrastructureDNSRecords(context.Background(), plan, step, publisher); err != nil {
		t.Fatal(err)
	}
	if done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); err != nil || !done {
		t.Fatalf("accepted receipt failed resume: %v", err)
	}
}

func TestServerSetupInfrastructureDNSOwnedZoneApplyPreservesAllOtherRecords(t *testing.T) {
	p, agent, plan, step := infrastructureDNSRuntimeFixture(t)
	// Reuse the existing subscription seeded by the runtime fixture.
	var sub int
	if err := p.db.GetDB().QueryRow(`SELECT id FROM subscriptions LIMIT 1`).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	seedSetupDNSDomain(t, p, sub, plan.InfrastructureDNS.Zone, setupDNSModeLocal)
	seedInfrastructureDNSZone(t, p, plan.InfrastructureDNS.Zone)
	if _, err := p.db.GetDB().Exec(`INSERT INTO pdns_records(domain_id,name,type,content,ttl,prio) SELECT id,name,'MX','mail.provider.test',777,25 FROM pdns_domains WHERE name=?`, plan.InfrastructureDNS.Zone); err != nil {
		t.Fatal(err)
	}
	revised, err := p.buildServerSetupInfrastructureDNSPlan(context.Background(), plan.Draft)
	if err != nil {
		t.Fatal(err)
	}
	plan.InfrastructureDNS = revised
	plan.ID = serverSetupPlanIdentity(plan)
	if done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); err != nil || !done {
		t.Fatalf("owned apply done=%v err=%v", done, err)
	}
	var content string
	var ttl, priority int
	if err := p.db.GetDB().QueryRow(`SELECT content,ttl,prio FROM pdns_records WHERE name=? AND type='MX'`, plan.InfrastructureDNS.Zone).Scan(&content, &ttl, &priority); err != nil {
		t.Fatal(err)
	}
	if content != "mail.provider.test" || ttl != 777 || priority != 25 {
		t.Fatal("existing MX was changed")
	}
	if len(agent.requests) != 1 {
		t.Fatal("unexpected publication count")
	}
}

// The shared V3 fake models primary BIND readiness. This wrapper provides
// the equivalent paired PowerDNS proof without relaxing the production gate.
type infrastructureDNSPairedAgent struct{ *dnsZoneV3TestAgent }

func (a *infrastructureDNSPairedAgent) DNSBackendReadiness(req *transport.Empty, response *transport.DNSBackendReadinessResponse) error {
	if err := a.dnsZoneV3TestAgent.DNSBackendReadiness(req, response); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range response.Engines {
		response.Engines[i].PairReady = response.Engines[i].Engine == a.publisher && a.pairReady
	}
	return nil
}
func attachInfrastructureDNSAgent(t *testing.T, p *Panel, agent *dnsZoneV3TestAgent) {
	t.Helper()
	p.pkgFamilyVal = "apt"
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", &infrastructureDNSPairedAgent{agent}); err != nil {
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
	raw, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(raw, connector)
	t.Cleanup(func() { _ = raw.Close() })
}
func TestServerSetupInfrastructureDNSNativePowerDNSPrimary(t *testing.T) {
	p, agent, plan, step := infrastructureDNSRuntimeFixtureEngine(t, "pdns")
	if done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); err != nil || !done {
		t.Fatalf("PowerDNS done=%v err=%v", done, err)
	}
	if len(agent.requests) != 1 || agent.requests[0].Engine != transport.DNSEnginePowerDNS {
		t.Fatal("publication did not bind native PowerDNS engine")
	}
	if done, err := p.runServerSetupInfrastructureDNS(context.Background(), plan, step); err != nil || !done {
		t.Fatal(err)
	}
	if len(agent.requests) != 1 {
		t.Fatal("PowerDNS duplicated exact publication")
	}
}
