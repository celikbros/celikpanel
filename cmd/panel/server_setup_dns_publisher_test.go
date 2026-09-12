package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func secondaryHostingDraft() serverSetupDraft {
	return serverSetupDraft{Purpose: "web_mail", PanelDomain: "boston.example.test", MailHostname: "mail.boston.example.test", DNSMode: "local", DNSEngine: "pdns", DNSRole: "secondary", NS1: "ns1.example.test", NS2: "ns2.example.test", LocalIP: "192.0.2.2", PeerIP: "192.0.2.1", PeerNS: "ns1.example.test", DNSPublisherEndpoint: "https://frankfurt.example.test:2083"}
}

func TestServerSetupSecondaryHostingReviewPlansBootstrapBeforePublishingAndMail(t *testing.T) {
	for _, purpose := range []string{"web", "web_mail", "application", "custom"} {
		for _, engine := range []string{"pdns", "bind"} {
			t.Run(purpose+"/"+engine, func(t *testing.T) {
				f, state := setupOperationFixture(t)
				caps := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2}
				f.agent.versionCapabilities = &caps
				draft := secondaryHostingDraft()
				draft.Purpose, draft.DNSEngine = purpose, engine
				draft.NodeVersion = "24.1.0"
				if purpose == "custom" {
					draft.Customization = &serverSetupCustomization{Components: []string{"nginx", "postfix", "dovecot"}}
				}
				state.Draft = draft
				plan := saveSetupPlanForTest(t, f, state)
				gate := -1
				for index, step := range plan.Steps {
					if step.Kind == "dns_publisher" {
						gate = index
						if step.Target != draft.DNSPublisherEndpoint {
							t.Fatal("publisher endpoint missing from reviewed step")
						}
					}
					if gate == -1 && (step.Kind == "mail_profile" || step.Kind == "runtime" || (step.Kind == "service" && !slices.Contains([]string{"nginx", "certbot", "nftables"}, step.Target))) {
						t.Fatalf("hosting admitted before publisher authorization: %+v", step)
					}
				}
				if gate < 1 || plan.Steps[0].Kind != "dns" || plan.HostnameChange != "" {
					t.Fatal("review omitted DNS bootstrap or changed OS identity")
				}
				for _, kind := range []string{"firewall", "panel_certificate"} {
					found := false
					for _, step := range plan.Steps[:gate] {
						found = found || step.Kind == kind
					}
					if !found {
						t.Fatalf("%s not prepared before enrollment", kind)
					}
				}
				changed := plan
				changed.Draft.DNSPublisherEndpoint = "https://other.example.test:2083"
				if serverSetupPlanIdentity(changed) == plan.ID {
					t.Fatal("endpoint change did not invalidate review")
				}
				if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
					t.Fatal("review changed host")
				}
			})
		}
	}
}

func TestServerSetupPrimaryHostingPreparesHTTPSBeforeWaitingForSecondary(t *testing.T) {
	f, state := setupOperationFixture(t)
	state.Draft = secondaryHostingDraft()
	state.Draft.Purpose, state.Draft.DNSRole, state.Draft.PeerNS = "web", "primary", state.Draft.NS2
	state.Draft.DNSPublisherEndpoint = ""
	plan := saveSetupPlanForTest(t, f, state)
	certificate, gate, hosting := -1, -1, -1
	for index, step := range plan.Steps {
		switch step.Kind {
		case "panel_certificate":
			certificate = index
		case "dns_readiness":
			gate = index
		case "service":
			if step.Target == "php-fpm" {
				hosting = index
			}
		}
	}
	if certificate < 0 || gate <= certificate || hosting <= gate {
		t.Fatalf("primary bootstrap order certificate=%d gate=%d hosting=%d", certificate, gate, hosting)
	}
}

type secondaryHostingFixture struct {
	f         serviceOperationTestFixture
	plan      serverSetupPlan
	execution serverSetupExecution
	agent     *dnsEngineTestAgent
	proof     remoteDNSAuthority
	id        string
}

func newSecondaryHostingFixture(t *testing.T) *secondaryHostingFixture {
	t.Helper()
	f, state := setupOperationFixture(t)
	caps := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2}
	f.agent.versionCapabilities = &caps
	state, err := f.panel.saveServerSetupDraft(context.Background(), state.Revision, secondaryHostingDraft())
	if err != nil {
		t.Fatal(err)
	}
	plan := saveSetupPlanForTest(t, f, state)
	serverSetupRunners.Store(f.panel, true)
	t.Cleanup(func() { serverSetupRunners.Delete(f.panel) })
	response := postSetupStartForTest(t, f, plan.ID, strings.Repeat("b", 32))
	if response.Code != http.StatusAccepted {
		t.Fatalf("setup start %d %s", response.Code, response.Body.String())
	}
	fixture := &secondaryHostingFixture{f: f, plan: plan, agent: newDNSEngineTestAgent(), id: strings.Repeat("c", 32)}
	if json.Unmarshal(response.Body.Bytes(), &fixture.execution) != nil {
		t.Fatal("setup response")
	}
	attachDNSEngineTestAgent(t, f.panel, fixture.agent)
	t.Setenv("CELIKPANEL_SERVER_IP", plan.Draft.LocalIP)
	t.Setenv("CELIKPANEL_SERVER_IPV6", "")
	if err := f.panel.startServerSetupDNS(context.Background(), plan.Draft, fixture.execution.Steps[0].RequestID, plan.Actor); err != nil {
		t.Fatal(err)
	}
	fixture.agent.mu.Lock()
	runtime := fixture.agent.runtimes[transport.DNSEnginePowerDNS]
	runtime.SecondaryReady = true
	fixture.agent.runtimes[transport.DNSEnginePowerDNS] = runtime
	fixture.agent.mu.Unlock()
	for index := range fixture.execution.Steps {
		if fixture.execution.Steps[index].Kind == "dns_publisher" {
			break
		}
		fixture.execution.Steps[index].Status = "succeeded"
	}
	if err := f.panel.persistServerSetupExecution(context.Background(), fixture.execution); err != nil {
		t.Fatal(err)
	}
	previousResolvers := setupDNSPublicResolvers
	setupDNSPublicResolvers = func() []hostResolver {
		return []hostResolver{setupNameserverResolver{plan.Draft.NS1: {plan.Draft.PeerIP}, plan.Draft.NS2: {plan.Draft.LocalIP}}}
	}
	t.Cleanup(func() { setupDNSPublicResolvers = previousResolvers })
	fixture.proof = remoteDNSAuthority{ClientID: fixture.id, Ready: true, Engine: "bind", Epoch: 1, Nameservers: []string{plan.Draft.NS1, plan.Draft.NS2}, PrimaryIP: plan.Draft.PeerIP, SecondaryIP: plan.Draft.LocalIP}
	previousExchange := remoteDNSExchange
	remoteDNSExchange = func(_ context.Context, endpoint, path, credential string, request, out any) error {
		if endpoint != plan.Draft.DNSPublisherEndpoint || path != "/api/v1/dns/remote/receiver/status" {
			return errors.New("unexpected receiver")
		}
		raw, _ := json.Marshal(fixture.proof)
		return json.Unmarshal(raw, out)
	}
	t.Cleanup(func() { remoteDNSExchange = previousExchange })
	names, _ := json.Marshal(fixture.proof.Nameservers)
	_, err = f.database.GetDB().Exec(`INSERT INTO remote_dns_connections(id,endpoint,label,credential,enrollment_code,status,nameservers_json,created_at) VALUES(?,?,?,?,'','ready',?,'now')`, fixture.id, plan.Draft.DNSPublisherEndpoint, "primary", strings.Repeat("d", 64), string(names))
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *secondaryHostingFixture) wait(t *testing.T) {
	t.Helper()
	progressed, err := f.f.panel.advanceServerSetupExecution(f.plan, &f.execution)
	if err != nil || progressed || f.execution.Status != "waiting" || f.execution.Phase != "dns_publisher" {
		t.Fatalf("gate advanced without authorization: %v %v %+v", progressed, err, f.execution)
	}
}

func (f *secondaryHostingFixture) bind(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"execution_id": f.execution.ID, "connection_id": id})
	w := httptest.NewRecorder()
	f.f.panel.handleServerSetupPublisher(w, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/setup/publisher", string(body), f.f.userID))
	return w
}

func TestServerSetupSecondaryHostingWaitsBindsAndResumesSameExecution(t *testing.T) {
	f := newSecondaryHostingFixture(t)
	f.wait(t)
	if active, err := f.f.panel.activeServiceOperation(context.Background()); err != nil || active != nil {
		t.Fatal("waiting admitted a hosting or mail child")
	}
	oldPlan := f.plan.ID
	response := f.bind(t, f.id)
	if response.Code != http.StatusAccepted {
		t.Fatalf("bind %d %s", response.Code, response.Body.String())
	}
	if replay := f.bind(t, f.id); replay.Code != http.StatusAccepted {
		t.Fatalf("lost reply could not reconcile: %s", replay.Body.String())
	}
	if replacement := f.bind(t, strings.Repeat("e", 32)); replacement.Code != http.StatusConflict {
		t.Fatal("binding was replaced")
	}
	// A stale pre-confirmation execution write must not erase the binding.
	if err := f.f.panel.persistServerSetupExecution(context.Background(), f.execution); err != nil {
		t.Fatal(err)
	}
	execution, err := f.f.panel.latestServerSetupExecution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	progressed, err := f.f.panel.advanceServerSetupExecution(f.plan, execution)
	if err != nil || !progressed || execution.PlanID != oldPlan || execution.ID != f.execution.ID {
		t.Fatalf("resume changed plan or did not advance %v %v %+v", progressed, err, execution)
	}
	mode, err := f.f.panel.setupDNSManagementMode(context.Background())
	if err != nil || mode != setupDNSModeExisting {
		t.Fatalf("hosting default was not assigned to authorized primary: %s %v", mode, err)
	}
	if ready, err := f.f.panel.serverSetupSecondaryHostingReadiness(context.Background(), f.plan.Draft); err != nil || !ready {
		t.Fatalf("combined proof missing after exact resume: %v %v", ready, err)
	}
	if _, ready, err := f.f.panel.activeDNSPublisher(context.Background()); err != nil || ready {
		t.Fatal("secondary acquired local write authority")
	}
	if f.agent.switchCalls != 1 {
		t.Fatal("resume reinstalled DNS")
	}
}

func TestServerSetupSecondaryHostingRejectsUnreviewedOrUnreadyPublisher(t *testing.T) {
	for _, scenario := range []string{"endpoint", "nameservers", "swapped_names", "swapped_ips", "unready", "legacy_no_pair", "revoked", "local_unready", "before_gate", "license"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSecondaryHostingFixture(t)
			if scenario != "before_gate" {
				f.wait(t)
			}
			switch scenario {
			case "endpoint":
				oldID := f.id
				f.id = strings.Repeat("e", 32)
				_, err := f.f.database.GetDB().Exec(`INSERT INTO remote_dns_connections(id,endpoint,label,credential,enrollment_code,status,nameservers_json,created_at) SELECT ?,'https://other.example.test:2083',label,credential,enrollment_code,status,nameservers_json,created_at FROM remote_dns_connections WHERE id=?`, f.id, oldID)
				if err != nil {
					t.Fatal(err)
				}
			case "nameservers":
				f.proof.Nameservers = []string{"ns1.other.test", "ns2.other.test"}
			case "swapped_names":
				f.proof.Nameservers[0], f.proof.Nameservers[1] = f.proof.Nameservers[1], f.proof.Nameservers[0]
			case "swapped_ips":
				f.proof.PrimaryIP, f.proof.SecondaryIP = f.proof.SecondaryIP, f.proof.PrimaryIP
			case "unready":
				f.proof.Ready = false
			case "legacy_no_pair":
				f.proof.PrimaryIP, f.proof.SecondaryIP = "", ""
			case "revoked":
				_, _ = f.f.database.GetDB().Exec(`UPDATE remote_dns_connections SET status='revoked' WHERE id=?`, f.id)
			case "local_unready":
				f.agent.mu.Lock()
				runtime := f.agent.runtimes[transport.DNSEnginePowerDNS]
				runtime.SecondaryReady = false
				f.agent.runtimes[transport.DNSEnginePowerDNS] = runtime
				f.agent.mu.Unlock()
			case "license":
				f.f.panel.license = testPanelLicense(t, "missing")
			}
			response := f.bind(t, f.id)
			if response.Code != http.StatusConflict && response.Code != http.StatusForbidden {
				t.Fatalf("unsafe binding accepted %d %s", response.Code, response.Body.String())
			}
			binding, err := f.f.panel.readServerSetupDNSPublisher(context.Background(), f.plan, f.execution.ID)
			if err != nil || binding != nil {
				t.Fatal("failed validation persisted a binding")
			}
		})
	}
}

func TestServerSetupSecondaryHostingRechecksRevocationAfterBinding(t *testing.T) {
	f := newSecondaryHostingFixture(t)
	f.wait(t)
	if response := f.bind(t, f.id); response.Code != http.StatusAccepted {
		t.Fatal(response.Body.String())
	}
	_, _ = f.f.database.GetDB().Exec(`UPDATE remote_dns_connections SET status='revoked' WHERE id=?`, f.id)
	f.wait(t)
	mode, err := f.f.panel.setupDNSManagementMode(context.Background())
	if err != nil || mode != setupDNSModeLocal {
		t.Fatal("revoked publisher activated hosting default")
	}
	if ready, _ := f.f.panel.serverSetupSecondaryHostingReadiness(context.Background(), f.plan.Draft); ready {
		t.Fatal("revoked connection completed setup")
	}
}

func TestServerSetupPublisherEndpointCanonicalAndRestricted(t *testing.T) {
	for _, value := range []string{"http://primary.example.test", "https://127.0.0.1:2083", "https://user:pass@primary.example.test:2083", "https://primary.example.test:2083/path"} {
		draft := secondaryHostingDraft()
		draft.DNSPublisherEndpoint = value
		if _, err := canonicalServerSetupDraft(draft); err == nil {
			t.Fatalf("invalid primary endpoint accepted: %s", value)
		}
	}
}
