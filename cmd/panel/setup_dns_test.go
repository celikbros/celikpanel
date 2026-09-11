package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

func seedSetupDNSOwner(t *testing.T, p *Panel) int {
	t.Helper()
	result, err := p.db.GetDB().Exec(`INSERT INTO users(id,username,password_hash,email,role) VALUES (1,'dns-owner','hash','dns-owner@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = p.db.GetDB().Exec(`INSERT INTO subscriptions(owner_id,name) VALUES (?,'DNS ownership test')`, id)
	if err != nil {
		t.Fatal(err)
	}
	id, err = result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func seedSetupDNSDomain(t *testing.T, p *Panel, sub int, name, mode string) int {
	t.Helper()
	domain := &core.Domain{SubscriptionID: sub, Name: name, Status: "active", DNSManagement: mode}
	if err := repositories.NewPostgresDomainRepository(p.db.GetDB()).Create(context.Background(), domain); err != nil {
		t.Fatal(err)
	}
	return domain.ID
}

func TestSetupDNSOwnershipSurvivesDefaultChangesAndDirectWrites(t *testing.T) {
	p := newDNSPanelForTest(t)
	ctx := context.Background()
	sub := seedSetupDNSOwner(t, p)
	local := seedSetupDNSDomain(t, p, sub, "local.example.test", "")
	if err := p.saveSetupDNSManagementMode(ctx, setupDNSModeExternal); err != nil {
		t.Fatal(err)
	}
	if mode, err := p.domainDNSManagementMode(ctx, "local.example.test"); err != nil || mode != setupDNSModeLocal {
		t.Fatalf("legacy ownership=%q %v", mode, err)
	}
	external := seedSetupDNSDomain(t, p, sub, "external.example.test", setupDNSModeExternal)
	if err := p.saveSetupDNSManagementMode(ctx, setupDNSModeLocal); err != nil {
		t.Fatal(err)
	}
	if mode, err := p.domainDNSManagementMode(ctx, "external.example.test"); err != nil || mode != setupDNSModeExternal {
		t.Fatalf("external ownership=%q %v", mode, err)
	}
	for _, id := range []int{local, external} {
		if _, err := p.db.GetDB().Exec(`UPDATE domains SET dns_management=CASE dns_management WHEN 'local' THEN 'external' ELSE 'local' END WHERE id=?`, id); err == nil {
			t.Fatal("ownership reclassification was accepted")
		}
	}
	if _, err := p.db.GetDB().Exec(`INSERT INTO pdns_domains(name,type) VALUES ('external.example.test','NATIVE')`); err == nil {
		t.Fatal("external domain acquired a local authoritative zone")
	}
	if _, err := p.db.GetDB().Exec(`INSERT INTO domains(subscription_id,name,parent_domain_id,dns_management) VALUES (?,'child.external.example.test',?,'local')`, sub, external); err == nil {
		t.Fatal("child lost external parent ownership")
	}
	if _, err := p.db.GetDB().Exec(`INSERT INTO domains(subscription_id,name,parent_domain_id,dns_management) VALUES (?,'child.external.example.test',?,'external')`, sub, external); err != nil {
		t.Fatal(err)
	}
	if err := p.saveSetupDNSManagementMode(ctx, setupDNSModeExisting); err == nil {
		t.Fatal("unimplemented remote management accepted")
	}
}

func TestSetupDNSExternalReadsInstructionsAndRefusesLocalPublication(t *testing.T) {
	p := newDNSPanelForTest(t)
	sub := seedSetupDNSOwner(t, p)
	id := seedSetupDNSDomain(t, p, sub, "external.example.test", setupDNSModeExternal)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.44")
	get := httptest.NewRecorder()
	p.handleDomainDNS(get, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/domains/%d/dns/records", id), nil))
	var records struct {
		Records    []DNSRecord `json:"records"`
		Management string      `json:"management"`
		Published  bool        `json:"published"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if get.Code != http.StatusOK || records.Management != setupDNSModeExternal || records.Published || len(records.Records) < 2 {
		t.Fatalf("instructions=%s", get.Body.String())
	}
	for _, record := range records.Records {
		if record.Type == "NS" || record.Type == "SOA" {
			t.Fatal("invented provider authority")
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		p.handleDomainDNS(recorder, httptest.NewRequest(method, fmt.Sprintf("/api/v1/domains/%d/dns/records", id), strings.NewReader(`{"name":"@","type":"A","content":"192.0.2.3"}`)))
		if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), errCodeExternalDNSManaged) {
			t.Fatalf("mutation=%d %s", recorder.Code, recorder.Body.String())
		}
	}
	mail := httptest.NewRecorder()
	p.handleMailAuthApply(mail, httptest.NewRequest(http.MethodPost, "/mail/auth/apply", strings.NewReader(`{"record":"spf"}`)), "external.example.test")
	if mail.Code != http.StatusConflict || !strings.Contains(mail.Body.String(), errCodeExternalDNSManaged) {
		t.Fatalf("mail apply=%s", mail.Body.String())
	}
	if err := p.syncZoneToDNS(context.Background(), "external.example.test", false); err == nil {
		t.Fatal("external zone publication accepted")
	}
	// A nil agent is intentional: external DNS cleanup requires no DNS RPC.
	if err := p.removeDomainDNSForDeletion(context.Background(), "external.example.test", ""); err != nil {
		t.Fatal(err)
	}
	if err := p.saveSetupDNSManagementMode(context.Background(), setupDNSModeExternal); err != nil {
		t.Fatal(err)
	}
	if ready, err := p.mailProfileDNSIdentityReady(context.Background()); err != nil || !ready {
		t.Fatalf("mail installation wrongly requires local DNS: %v %v", ready, err)
	}
}

type setupDNSHostingAgent struct {
	hostingCapabilitiesTestAgent
	mu      sync.Mutex
	created []transport.CreateSiteRequest
}

func (a *setupDNSHostingAgent) CreateSite(req *transport.CreateSiteRequest, out *transport.CreateSiteResponse) error {
	a.mu.Lock()
	a.created = append(a.created, *req)
	a.mu.Unlock()
	out.Success = true
	return nil
}

func TestSetupDNSExternalHostingCreatesNoZoneAndDNSOnlyStaysGuarded(t *testing.T) {
	p := newDNSPanelForTest(t)
	sub := seedSetupDNSOwner(t, p)
	ctx := context.Background()
	if err := p.saveSetupDNSManagementMode(ctx, setupDNSModeExternal); err != nil {
		t.Fatal(err)
	}
	agent := &setupDNSHostingAgent{hostingCapabilitiesTestAgent: hostingCapabilitiesTestAgent{installed: []string{"nginx"}}}
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		a, b := net.Pipe()
		go server.ServeConn(a)
		return rpc.NewClient(b), nil
	}
	client, err := connector(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	p.orchestrator = services.NewSiteOrchestrator(p.db.GetDB(), p.agentClient)
	request := func(name, kind string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/domains", strings.NewReader(fmt.Sprintf(`{"domain":%q,"project_type":%q,"subscription_id":%d}`, name, kind, sub)))
		r = r.WithContext(context.WithValue(r.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}))
		w := httptest.NewRecorder()
		p.handleCreateDomain(w, r)
		return w
	}
	web := request("external.example.test", "static")
	if web.Code != http.StatusOK {
		t.Fatalf("external create=%d %s", web.Code, web.Body.String())
	}
	mode, err := p.domainDNSManagementMode(ctx, "external.example.test")
	if err != nil || mode != setupDNSModeExternal {
		t.Fatalf("ownership=%q %v", mode, err)
	}
	var zones int
	if err := p.db.GetDB().QueryRow(`SELECT count(*) FROM pdns_domains`).Scan(&zones); err != nil || zones != 0 {
		t.Fatalf("local zones=%d %v", zones, err)
	}
	dns := request("dnsonly.example.test", "dnsonly")
	if dns.Code != http.StatusConflict || !strings.Contains(dns.Body.String(), errCodeDNSServerRequired) {
		t.Fatalf("DNS-only=%d %s", dns.Code, dns.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.created) != 1 {
		t.Fatalf("site creation calls=%d", len(agent.created))
	}
}

func TestSetupDNSRejectsSameHostRedundancyAndIncorrectPairRole(t *testing.T) {
	base := serverSetupDraft{DNSMode: setupDNSModeLocal, DNSEngine: "bind", DNSRole: "primary", NS1: "ns1.example.test", NS2: "ns2.example.test", LocalIP: "192.0.2.1", PeerIP: "192.0.2.2", PeerNS: "ns2.example.test"}
	if _, _, err := setupDNSIdentity(base); err != nil {
		t.Fatal(err)
	}
	bad := base
	bad.PeerIP = bad.LocalIP
	if _, _, err := setupDNSIdentity(bad); err == nil {
		t.Fatal("same-host redundancy accepted")
	}
	bad = base
	bad.DNSRole = "secondary"
	if _, _, err := setupDNSIdentity(bad); err == nil {
		t.Fatal("secondary with secondary peer accepted")
	}
	bad = base
	bad.DNSMode = setupDNSModeExisting
	if _, _, err := setupDNSIdentity(bad); err == nil {
		t.Fatal("peer address became remote publication authority")
	}
}
