package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"slices"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

type setupComponentsTestAgent struct {
	*serviceOperationTestAgent
	versions       []string
	inventoryError bool
}

func (a *setupComponentsTestAgent) ListNodeVersions(_ *transport.Empty, response *transport.NodeVersionsResponse) error {
	response.Installed = append([]string{}, a.versions...)
	return nil
}

func (a *setupComponentsTestAgent) InstalledServiceIDsStrict(request *transport.Empty, response *[]string) error {
	if a.inventoryError {
		return errors.New("inventory unavailable")
	}
	return a.serviceOperationTestAgent.InstalledServiceIDsStrict(request, response)
}

func attachSetupComponentsAgent(t *testing.T, f serviceOperationTestFixture) *setupComponentsTestAgent {
	t.Helper()
	a := &setupComponentsTestAgent{serviceOperationTestAgent: f.agent}
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", a); err != nil {
		t.Fatal(err)
	}
	connector := func(context.Context) (*rpc.Client, error) {
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, _ := connector(context.Background())
	f.panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { client.Close() })
	return a
}

func customSetupPlanForTest(t *testing.T, f serviceOperationTestFixture, state serverSetupState, ids ...string) serverSetupPlan {
	t.Helper()
	state.Draft.Customization = &serverSetupCustomization{Components: ids}
	var err error
	state.Draft, err = canonicalServerSetupDraft(state.Draft)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.panel.buildServerSetupPlan(context.Background(), state, serviceOperationActor{UserID: f.userID})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestServerSetupCustomizationCanonicalAndLegacyWireIdentity(t *testing.T) {
	legacy := `{"id":"","version":1,"revision":7,"purpose":"web","steps":[{"id":"01-service","kind":"service","target":"nginx"}],"blockers":[],"can_start":true,"tcp_ports":[80,443,2083],"udp_ports":[],"preserve_ssh":true,"persist_firewall":true,"draft":{"purpose":"web","remote_dns_connection_id":"","panel_domain":"panel.example.test","mail_hostname":"","dns_mode":"external","dns_engine":"pdns","dns_role":"primary","ns1":"","ns2":"","local_ip":"","peer_ip":"","peer_ns":"","node_version":"","database":"mariadb"},"build_commit":"e3ed58af9c9becb8bf20e6da0a4219204265a283","contact_email":"admin@example.test","actor":{"UserID":1,"IP":"","UserAgent":""}}`
	// This literal uses Alpha66's serialized field order and no customization.
	// Hash the frozen wire bytes rather than generating a baseline with new code.
	var oldPlan serverSetupPlan
	if err := json.Unmarshal([]byte(legacy), &oldPlan); err != nil {
		t.Fatal(err)
	}
	if serverSetupPlanIdentity(oldPlan) != serverSetupID(legacy) {
		t.Fatal("Alpha66 plan identity changed")
	}
	encoded, _ := json.Marshal(oldPlan)
	if string(encoded) != legacy {
		t.Fatalf("legacy wire changed: %s", encoded)
	}

	draft := defaultServerSetupDraft()
	draft.Customization = &serverSetupCustomization{Components: []string{"phpmyadmin", "nginx", "phpmyadmin", " nginx "}}
	canonical, err := canonicalServerSetupDraft(draft)
	if err != nil || !slices.Equal(canonical.Customization.Components, []string{"nginx", "phpmyadmin"}) {
		t.Fatalf("canonical selection: %+v %v", canonical.Customization, err)
	}
	for _, id := range []string{"apache", "exim", "vsftpd", "bind", "pdns", "netdata", "clamav", "nftables", "certbot", "../../bin/sh"} {
		draft.Customization = &serverSetupCustomization{Components: []string{id}}
		if _, err := canonicalServerSetupDraft(draft); err == nil {
			t.Fatalf("unsupported arbitrary input accepted: %s", id)
		}
	}
	draft.Purpose = "custom"
	draft.Customization = nil
	canonical, err = canonicalServerSetupDraft(draft)
	if err != nil || canonical.Customization == nil || canonical.Customization.Components == nil {
		t.Fatal("custom empty selection was lost")
	}
}

func TestServerSetupCustomizationCatalogueReadOnlyAndRoles(t *testing.T) {
	f, _ := setupOperationFixture(t)
	a := attachSetupComponentsAgent(t, f)
	a.versions = []string{"22.14.0"}
	seedInstalledServices(f.agent, "nginx")
	w := httptest.NewRecorder()
	f.panel.handleServerSetupComponents(w, serviceOperationAdminRequest(t, http.MethodGet, serverSetupPath+"/components", "", f.userID))
	var catalog serverSetupComponentCatalog
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &catalog) != nil || catalog.InventoryState != "ready" {
		t.Fatalf("catalog: %d %s", w.Code, w.Body.String())
	}
	for _, id := range []string{"nginx", "node"} {
		found := false
		for _, choice := range catalog.Components {
			if choice.ID == id {
				found = choice.Installed
			}
		}
		if !found {
			t.Fatalf("existing %s absent from inventory", id)
		}
	}
	if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
		t.Fatal("catalog read mutated host")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("catalog cached")
	}
	p := &Panel{}
	for _, role := range []string{"", roleCustomer, roleReseller, "additional"} {
		r := httptest.NewRequest(http.MethodGet, serverSetupPath+"/components", nil)
		if role != "" {
			r = r.WithContext(context.WithValue(r.Context(), callerKey, &Caller{Role: role}))
		}
		w := httptest.NewRecorder()
		p.handleServerSetupComponents(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("role %s not denied", role)
		}
	}
	a.inventoryError = true
	w = httptest.NewRecorder()
	f.panel.handleServerSetupComponents(w, serviceOperationAdminRequest(t, http.MethodGet, serverSetupPath+"/components", "", f.userID))
	if json.Unmarshal(w.Body.Bytes(), &catalog) != nil || catalog.InventoryState != "unknown" {
		t.Fatalf("unknown inventory misreported: %s", w.Body.String())
	}
	for _, choice := range catalog.Components {
		if choice.Supported || choice.Installed {
			t.Fatalf("unverified inventory accepted: %+v", choice)
		}
	}
}

func TestServerSetupCustomizationCatalogueUsesHostAndConflicts(t *testing.T) {
	for _, test := range []struct {
		family, id string
		installed  []string
	}{
		{"pacman", "phppgadmin", nil}, {"apt", "nginx", []string{"apache"}}, {"apt", "valkey", []string{"redis"}}, {"apt", "roundcube", []string{"exim"}},
	} {
		catalog := buildServerSetupComponentCatalog(core.ManagedServiceHostProfile{PackageFamily: test.family}, test.installed, true)
		for _, choice := range catalog.Components {
			if choice.ID == test.id && (choice.Supported || choice.Reason == "") {
				t.Fatalf("unsupported/conflicting %s offered: %+v", test.id, choice)
			}
			for _, dependency := range choice.Dependencies {
				if !slices.Contains(serverSetupSelectableComponents, dependency) {
					t.Fatalf("unreviewed dependency %s", dependency)
				}
			}
		}
	}
	for _, id := range serverSetupSelectableComponents {
		service := core.GetManagedServiceByID(id)
		for _, helper := range service.HelperUnits {
			if core.ServiceForUnit(helper) == nil {
				t.Fatalf("%s helper cannot be observed by current service scanner", id)
			}
		}
	}
}

func TestServerSetupCustomizationDependenciesAndNoDeselectionRemoval(t *testing.T) {
	f, state := setupOperationFixture(t)
	seedInstalledServices(f.agent, "nginx", "postfix", "dovecot", "mariadb")
	plan := customSetupPlanForTest(t, f, state, "phppgadmin")
	if !plan.CanStart {
		t.Fatalf("valid custom plan blocked: %v", plan.Blockers)
	}
	positions := map[string]int{}
	for index, step := range plan.Steps {
		positions[step.Target] = index
		if strings.Contains(step.Kind, "remove") || slices.Contains([]string{"postfix", "dovecot", "mariadb", "nginx"}, step.Target) {
			t.Fatalf("deselection altered existing service: %+v", step)
		}
	}
	if positions["postgresql"] >= positions["phppgadmin"] || positions["php-fpm"] >= positions["phppgadmin"] {
		t.Fatalf("dependency order wrong: %+v", plan.Steps)
	}
	for _, port := range []int{25, 587, 993, 80, 443, panelPort()} {
		if !slices.Contains(plan.TCPPorts, port) {
			t.Fatalf("existing workload port %d lost", port)
		}
	}
	for _, component := range plan.Components {
		if component.ID == "nginx" && (!component.Installed || !component.Required || component.Selected) {
			t.Fatalf("dependency summary wrong: %+v", component)
		}
	}
	if f.agent.installCalls.Load() != 0 {
		t.Fatal("review installed dependency")
	}

	first := customSetupPlanForTest(t, f, state, "phpmyadmin", "nginx")
	second := customSetupPlanForTest(t, f, state, "nginx", "phpmyadmin", "phpmyadmin")
	if first.ID != second.ID {
		t.Fatal("order/dedup changed review identity")
	}
	third := customSetupPlanForTest(t, f, state, "nginx")
	if third.ID == first.ID {
		t.Fatal("material selection did not invalidate review")
	}
	conflict := customSetupPlanForTest(t, f, state, "redis", "valkey")
	if conflict.CanStart || !strings.Contains(strings.Join(conflict.Blockers, ","), "server_setup_service_conflict:") {
		t.Fatalf("competing services accepted: %+v", conflict)
	}
}

func TestServerSetupCustomizationMailUsesOnlyAuditedProfiles(t *testing.T) {
	for _, test := range []struct {
		selected string
		profiles []string
	}{
		{"postfix", []string{core.MailProfileCore}}, {"dovecot", []string{core.MailProfileCore}}, {"rspamd", []string{core.MailProfileProtected}}, {"roundcube", []string{core.MailProfileWebmail}},
	} {
		t.Run(test.selected, func(t *testing.T) {
			f, state := setupOperationFixture(t)
			capabilities := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
			f.agent.versionCapabilities = &capabilities
			state.Draft.Purpose = "custom"
			state.Draft.MailHostname = "mail.example.test"
			plan := customSetupPlanForTest(t, f, state, test.selected)
			if !plan.CanStart {
				t.Fatalf("mail plan unexpectedly blocked: %v", plan.Blockers)
			}
			profiles := []string{}
			for _, step := range plan.Steps {
				if step.Kind == "mail_profile" {
					profiles = append(profiles, step.Target)
				}
				if step.Kind == "service" && slices.Contains([]string{"postfix", "dovecot", "rspamd", "roundcube"}, step.Target) {
					t.Fatalf("unsafe generic mail install: %+v", step)
				}
			}
			if !slices.Equal(profiles, test.profiles) {
				t.Fatalf("unrelated mail profile required: %v", profiles)
			}
			if !slices.Equal(serverSetupMailProfileIDs(plan.Draft), test.profiles) {
				t.Fatal("mail readiness requested different profile receipts")
			}
			if plan.HostnameChange != "" || !serverSetupHasComponent(plan.Draft, "postfix") || !serverSetupHasComponent(plan.Draft, "dovecot") {
				t.Fatal("mail identity/core lifecycle missing")
			}
		})
	}
}

func TestServerSetupCustomizationNodeExactVersionAndSelectionReadiness(t *testing.T) {
	f, state := setupOperationFixture(t)
	a := attachSetupComponentsAgent(t, f)
	a.versions = []string{"22.14.0"}
	state.Draft.NodeVersion = "22.15.0"
	plan := customSetupPlanForTest(t, f, state, "node")
	if !plan.CanStart {
		t.Fatalf("node plan blocked: %v", plan.Blockers)
	}
	for _, component := range plan.Components {
		if component.ID == "node" && component.Installed {
			t.Fatal("different Node version counted as installed")
		}
	}
	state.Draft.NodeVersion = "22.14.0"
	plan = customSetupPlanForTest(t, f, state, "node")
	for _, component := range plan.Components {
		if component.ID == "node" && !component.Installed {
			t.Fatal("exact Node version missed")
		}
	}
	seedInstalledServices(f.agent, "nginx", "certbot", "nftables")
	f.agent.active["nginx"] = true
	draft := plan.Draft
	draft.PanelDomain = ""
	checkServices := func() string {
		checks, err := f.panel.serverSetupCompletionChecks(context.Background(), draft)
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range checks {
			if check.ID == "services" {
				return check.State
			}
		}
		t.Fatal("missing services check")
		return ""
	}
	if got := checkServices(); got != "ready" {
		t.Fatalf("custom Node was not verified: %s", got)
	}
	a.versions = []string{"22.15.0"}
	if got := checkServices(); got == "ready" {
		t.Fatal("wrong Node version falsely ready")
	}
	draft.Customization = &serverSetupCustomization{Components: []string{"nginx"}}
	if got := checkServices(); got != "ready" {
		t.Fatalf("deselected Node/PHP/database still required: %s", got)
	}
	seedInstalledServices(f.agent, "apache")
	if got := checkServices(); got == "ready" {
		t.Fatal("installed conflict bypassed completion gate")
	}
	f.agent.installed["apache"] = false
	f.agent.active["nginx"] = false
	if got := checkServices(); got == "ready" {
		t.Fatal("stopped selected dependency falsely ready")
	}
}

func TestServerSetupCustomizationEmptyAndDNSPublisherPolicy(t *testing.T) {
	f, state := setupOperationFixture(t)
	state.Draft.Purpose = "custom"
	for _, mode := range []string{"external", "existing", "local"} {
		state.Draft.DNSMode = mode
		plan := customSetupPlanForTest(t, f, state)
		blocked := slices.Contains(plan.Blockers, "server_setup_components_required")
		if blocked != (mode != "local") {
			t.Fatalf("empty %s purpose decision wrong: %v", mode, plan.Blockers)
		}
	}
	for _, test := range []struct {
		ids       []string
		publisher bool
	}{
		{nil, false}, {[]string{"mariadb", "fail2ban"}, false}, {[]string{"nginx"}, true}, {[]string{"node"}, true}, {[]string{"roundcube"}, true},
	} {
		draft := serverSetupDraft{Purpose: "custom", Customization: &serverSetupCustomization{Components: test.ids}}
		if serverSetupNeedsDNSPublisher(draft) != test.publisher {
			t.Fatalf("wrong DNS authority role for %v", test.ids)
		}
	}
}
