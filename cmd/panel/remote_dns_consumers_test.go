package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

func seedRemoteConsumerConnection(t *testing.T, p *Panel, id string) {
	t.Helper()
	_, err := p.db.GetDB().Exec(`INSERT INTO remote_dns_connections(id,endpoint,credential,enrollment_code,label,status,created_at) VALUES(?,'https://127.0.0.1:1','','','unavailable fixture','ready',datetime('now'))`, id)
	if err != nil {
		t.Fatal(err)
	}
}

func attachRemoteConsumerAgent(t *testing.T, p *Panel) *setupDNSHostingAgent {
	t.Helper()
	agent := &setupDNSHostingAgent{hostingCapabilitiesTestAgent: hostingCapabilitiesTestAgent{installed: []string{"nginx"}}}
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	connector := func(context.Context) (*rpc.Client, error) {
		a, b := net.Pipe()
		go server.ServeConn(a)
		return rpc.NewClient(b), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { client.Close() })
	p.orchestrator = services.NewSiteOrchestrator(p.db.GetDB(), p.agentClient)
	return agent
}

func TestRemoteDNSConsumerUnavailableAuthorityCannotCreateHostOrLocalZone(t *testing.T) {
	p := newDNSPanelForTest(t)
	sub := seedSetupDNSOwner(t, p)
	agent := attachRemoteConsumerAgent(t, p)
	connectionID := strings.Repeat("a", 32)
	seedRemoteConsumerConnection(t, p, connectionID)
	for key, value := range map[string]string{settingSetupDNSMode: setupDNSModeExisting, "remote_dns_connection_id": connectionID} {
		if _, err := p.db.GetDB().Exec(`INSERT INTO panel_settings(key,value) VALUES(?,?)`, key, value); err != nil {
			t.Fatal(err)
		}
	}
	r := serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/domains", fmt.Sprintf(`{"domain":"remote.example.test","project_type":"static","subscription_id":%d}`, sub), 1)
	w := httptest.NewRecorder()
	p.handleCreateDomain(w, r)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), errCodeRemoteDNSUnavailable) {
		t.Fatalf("unavailable remote authority did not fail before creation: %d %s", w.Code, w.Body.String())
	}
	for _, table := range []string{"domains", "sites", "pdns_domains", "remote_dns_records"} {
		var count int
		if err := p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed remote preflight changed %s: count=%d err=%v", table, count, err)
		}
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.created) != 0 {
		t.Fatal("remote connection failure reached privileged site creation")
	}
}

func TestRemoteDNSConsumerAssociationSurvivesDefaultChangesAndCannotReparent(t *testing.T) {
	p := newDNSPanelForTest(t)
	sub := seedSetupDNSOwner(t, p)
	first, second := strings.Repeat("a", 32), strings.Repeat("b", 32)
	seedRemoteConsumerConnection(t, p, first)
	seedRemoteConsumerConnection(t, p, second)
	repo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	create := func(name, connection string, parent *int) *core.Domain {
		t.Helper()
		domain := &core.Domain{SubscriptionID: sub, Name: name, Status: "active", DNSManagement: setupDNSModeExisting, DNSRemoteConnectionID: connection, ParentDomainID: parent}
		if err := repo.Create(context.Background(), domain); err != nil {
			t.Fatal(err)
		}
		return domain
	}
	parent := create("first.example.test", first, nil)
	other := create("other.example.test", second, nil)
	child := create("child.first.example.test", first, &parent.ID)
	if _, err := p.db.GetDB().Exec(`INSERT INTO panel_settings(key,value) VALUES('remote_dns_connection_id',?)`, second); err != nil {
		t.Fatal(err)
	}
	for _, domain := range []*core.Domain{parent, child} {
		loaded, err := repo.GetByID(context.Background(), domain.ID)
		if err != nil || loaded.DNSRemoteConnectionID != first {
			t.Fatalf("default connection change migrated domain: %+v %v", loaded, err)
		}
	}
	if _, err := p.db.GetDB().Exec(`UPDATE domains SET dns_remote_connection_id=? WHERE id=?`, second, child.ID); err == nil {
		t.Fatal("connector changed without an explicit migration")
	}
	if _, err := p.db.GetDB().Exec(`UPDATE domains SET parent_domain_id=? WHERE id=?`, other.ID, child.ID); err == nil {
		t.Fatal("reparenting crossed the immutable connector boundary")
	}
	if _, err := p.db.GetDB().Exec(`INSERT INTO pdns_domains(name,type) VALUES (?,'NATIVE')`, parent.Name); err == nil {
		t.Fatal("remote domain acquired a local authoritative zone")
	}
}

func TestRemoteDNSConsumerReviewIdentityIncludesEndpointAndNameservers(t *testing.T) {
	plan := serverSetupPlan{Version: serverSetupPlanVersion, Purpose: "web", RemoteDNSConnection: &serverSetupRemoteDNSConnection{ID: strings.Repeat("a", 32), Endpoint: "https://dns.example.test:2083", Nameservers: []string{"ns1.example.test", "ns2.example.test"}}}
	original := serverSetupPlanIdentity(plan)
	plan.RemoteDNSConnection.Endpoint = "https://replacement.example.test:2083"
	if serverSetupPlanIdentity(plan) == original {
		t.Fatal("endpoint change did not invalidate review")
	}
	plan.RemoteDNSConnection.Endpoint = "https://dns.example.test:2083"
	plan.RemoteDNSConnection.Nameservers[1] = "ns3.example.test"
	if serverSetupPlanIdentity(plan) == original {
		t.Fatal("authority nameserver change did not invalidate review")
	}
}
