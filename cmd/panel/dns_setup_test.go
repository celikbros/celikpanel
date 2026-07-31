package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func dnsSetupAdminRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/dns-setup", strings.NewReader(body))
	return strictDNSAdminRequest(req)
}

func dnsSetupSystemAdminRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/dns-setup", strings.NewReader(body))
	return req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{Role: roleAdmin}))
}

func assertDNSSetupSettings(t *testing.T, p *Panel, want map[string]string) {
	t.Helper()
	for key, value := range want {
		if got := p.setting(context.Background(), key); got != value {
			t.Fatalf("setting %s = %q, want %q", key, got, value)
		}
	}
}

func TestDNSSetupRequiresAdminAndPUT(t *testing.T) {
	p := newDNSPanelForTest(t)

	unauthorized := httptest.NewRecorder()
	p.handleDNSSetup(unauthorized, httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/dns-setup",
		strings.NewReader(`{"role":"standalone"}`),
	))
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d, want 403; body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	wrongMethod := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings/dns-setup", nil)
	request = request.WithContext(context.WithValue(request.Context(), callerKey, &Caller{Role: roleAdmin}))
	p.handleDNSSetup(wrongMethod, request)
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405; body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestDNSSetupPairedRenameCommitsOneTopology(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	zoneID := seedReconcileZone(t, p, "biovision.health")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns3.biovision.health","ns2":"ns4.biovision.health","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.biovision.health"}`,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns3.biovision.health",
		settingNS2:       "ns4.biovision.health",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "198.51.100.20",
		settingDNSPeerNS: "ns4.biovision.health",
	})

	rows, err := p.db.GetDB().QueryContext(context.Background(), `
		SELECT content FROM pdns_records
		WHERE domain_id = ? AND LOWER(TRIM(name, '.')) = 'biovision.health' AND UPPER(type) = 'NS'
		ORDER BY content`, zoneID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var gotNS []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		gotNS = append(gotNS, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ns3.biovision.health", "ns4.biovision.health"}; !reflect.DeepEqual(gotNS, want) {
		t.Fatalf("apex NS RRset = %v, want %v", gotNS, want)
	}
	if soa := recordContent(t, p, zoneID, "biovision.health", "SOA"); !strings.HasPrefix(soa, "ns3.biovision.health ") {
		t.Fatalf("SOA MNAME = %q, want local ns3", soa)
	}
	assertSingleReconciledA(t, p, zoneID, "ns3.biovision.health", "192.0.2.10")
	assertSingleReconciledA(t, p, zoneID, "ns4.biovision.health", "198.51.100.20")
}

func TestDNSSetupRejectsInvalidPairedTupleBeforeAgent(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "self peer",
			body: `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.10","peer_ns":"ns2.example.net"}`,
		},
		{
			name: "peer name outside pair",
			body: `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns9.example.net"}`,
		},
		{
			name: "unspecified peer",
			body: `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"0.0.0.0","peer_ns":"ns2.example.net"}`,
		},
		{
			name: "nameserver label too long",
			body: `{"ns1":"` + strings.Repeat("a", 64) + `.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns2.example.net"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			agent := &strictDNSRPCAgent{}
			attachStrictDNSRPCAgent(t, p, agent)

			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(tc.body))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			agent.mu.Lock()
			calls := agent.clusterCalls
			agent.mu.Unlock()
			if calls != 0 {
				t.Fatalf("invalid tuple reached the agent %d time(s)", calls)
			}
			assertDNSSetupSettings(t, p, map[string]string{
				settingNS1: "", settingNS2: "", settingDNSRole: "",
			})
		})
	}
}

func TestDNSSetupAgentFailureLeavesLedgerUntouched(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &strictDNSRPCAgent{clusterError: "forced agent rejection"}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`,
	))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns1.celikhost.com",
		settingNS2:       "ns2.celikhost.com",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "2.25.80.4",
		settingDNSPeerNS: "ns2.celikhost.com",
	})
}

func TestDNSSetupDatabaseFailureRestoresAgentAndLedger(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &compensationDNSAgent{}
	attachCompensationDNSAgent(t, p, agent)
	rejectDNSClusterSettingWrites(t, p)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`,
	))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	calls := append([]CompensationDNSClusterRequest(nil), agent.calls...)
	agent.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("agent calls = %+v, want apply plus rollback", calls)
	}
	if calls[0].Role != "paired" || calls[0].PeerIP != "198.51.100.20" || calls[0].PeerNS != "ns4.example.net" {
		t.Fatalf("desired agent state = %+v", calls[0])
	}
	if calls[1].Role != "paired" || calls[1].PeerIP != "2.25.80.4" || calls[1].PeerNS != "ns2.celikhost.com" {
		t.Fatalf("rollback agent state = %+v", calls[1])
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns1.celikhost.com",
		settingNS2:       "ns2.celikhost.com",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "2.25.80.4",
		settingDNSPeerNS: "ns2.celikhost.com",
	})
}

func TestDNSSetupPublicationFailureCanRetrySameMutation(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	seedReconcileZone(t, p, "biovision.health")
	agent := &strictDNSRPCAgent{failZone: "biovision.health"}
	attachStrictDNSRPCAgent(t, p, agent)
	body := `{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`

	first := httptest.NewRecorder()
	p.handleDNSSetup(first, dnsSetupSystemAdminRequest(body))
	assertPublicationConflict(t, first)
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns3.example.net",
		settingNS2:       "ns4.example.net",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "198.51.100.20",
		settingDNSPeerNS: "ns4.example.net",
	})
	var savedAudits, publishedAudits int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'settings.dns_setup_saved:%'`,
	).Scan(&savedAudits); err != nil {
		t.Fatal(err)
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'settings.dns_setup_published:%'`,
	).Scan(&publishedAudits); err != nil {
		t.Fatal(err)
	}
	if savedAudits != 1 || publishedAudits != 0 {
		t.Fatalf("publication failure audits = saved:%d published:%d, want saved:1 published:0",
			savedAudits, publishedAudits)
	}

	agent.mu.Lock()
	agent.failZone = ""
	agent.mu.Unlock()
	second := httptest.NewRecorder()
	p.handleDNSSetup(second, dnsSetupSystemAdminRequest(body))
	if second.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200; body=%s", second.Code, second.Body.String())
	}
	agent.mu.Lock()
	clusterCalls := agent.clusterCalls
	syncCalls := append([]string(nil), agent.syncCalls...)
	agent.mu.Unlock()
	if clusterCalls != 2 {
		t.Fatalf("same setup mutation reached the agent %d times, want 2", clusterCalls)
	}
	if !reflect.DeepEqual(syncCalls, []string{"biovision.health", "biovision.health"}) {
		t.Fatalf("zone publication calls = %v, want failed attempt plus retry", syncCalls)
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'settings.dns_setup_saved:%'`,
	).Scan(&savedAudits); err != nil {
		t.Fatal(err)
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'settings.dns_setup_published:%'`,
	).Scan(&publishedAudits); err != nil {
		t.Fatal(err)
	}
	if savedAudits != 2 || publishedAudits != 1 {
		t.Fatalf("retry audits = saved:%d published:%d, want saved:2 published:1",
			savedAudits, publishedAudits)
	}
}

func TestDNSTopologyPUTsAreSerialized(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	seedReconcileZone(t, p, "biovision.health")
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	agent := &strictDNSRPCAgent{clusterEntered: entered, clusterRelease: release}
	attachStrictDNSRPCAgent(t, p, agent)

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		p.handleDNSSetup(recorder, dnsSetupAdminRequest(
			`{"ns1":"ns1.celikhost.com","ns2":"ns2.celikhost.com","role":"paired","peer_ip":"2.25.80.4","peer_ns":"ns2.celikhost.com"}`,
		))
		firstDone <- recorder
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first DNS topology mutation did not reach the blocked agent")
	}

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		p.handleNameserverSettings(recorder, strictDNSAdminRequest(httptest.NewRequest(
			http.MethodPut, "/api/v1/settings/nameservers",
			strings.NewReader(`{"ns1":"ns1.celikhost.com","ns2":"ns2.celikhost.com"}`),
		)))
		secondDone <- recorder
	}()
	select {
	case recorder := <-secondDone:
		t.Fatalf("legacy topology PUT interleaved with setup PUT; status=%d body=%s", recorder.Code, recorder.Body.String())
	case <-time.After(150 * time.Millisecond):
	}
	close(release)

	for name, done := range map[string]<-chan *httptest.ResponseRecorder{
		"setup": firstDone, "legacy nameserver": secondDone,
	} {
		select {
		case recorder := <-done:
			if recorder.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200; body=%s", name, recorder.Code, recorder.Body.String())
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not finish after releasing the topology lock", name)
		}
	}
}
