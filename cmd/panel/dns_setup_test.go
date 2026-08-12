package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func assertDNSSetupRequired(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode legacy DNS refusal: %v", err)
	}
	if body.Code != errCodeDNSSetupRequired {
		t.Fatalf("code = %q, want %q", body.Code, errCodeDNSSetupRequired)
	}
	if body.Action != "/settings?section=dns" {
		t.Fatalf("action = %q, want DNS settings", body.Action)
	}
	if !strings.Contains(body.Error, "/api/v1/settings/dns-setup") {
		t.Fatalf("migration endpoint missing from response: %q", body.Error)
	}
}

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

func TestDNSSetupKnownTerminalFailureRetainsDesiredLedgerAndSaga(t *testing.T) {
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
	pending, err := readPendingDNSClusterSaga(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || pending.Desired.Role != "paired" ||
		pending.Desired.PeerIP != "198.51.100.20" ||
		pending.Desired.PeerNS != "ns4.example.net" {
		t.Fatalf("known terminal failure lost desired recovery saga=%+v", pending)
	}
	agent.mu.Lock()
	clusterCalls, syncCalls := agent.clusterCalls, len(agent.syncRequests)
	agent.mu.Unlock()
	if clusterCalls != 1 || syncCalls != 0 {
		t.Fatalf("terminal failure cluster/sync calls=%d/%d, want 1/0",
			clusterCalls, syncCalls)
	}
}

func TestDNSSetupV2PreflightRejectsBeforeHostOrLedgerMutation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		capabilities []string
		rhel         bool
		wantStatus   int
	}{
		{name: "legacy agent", capabilities: []string{}, wantStatus: http.StatusInternalServerError},
		{
			name: "RHEL policy denial", capabilities: []string{transport.AgentCapabilityDNSZoneSyncV2},
			rhel: true, wantStatus: http.StatusConflict,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "paired")
			zoneID := seedReconcileZone(t, p, "preflight.example")
			beforeState, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "preflight.example")
			if err != nil {
				t.Fatal(err)
			}
			beforeSOA := recordContent(t, p, zoneID, "preflight.example", "SOA")
			capabilities := append([]string(nil), tc.capabilities...)
			agent := &strictDNSRPCAgent{versionCapabilities: &capabilities}
			attachStrictDNSRPCAgent(t, p, agent)
			if tc.rhel {
				p.pkgFamilyMu.Lock()
				p.pkgFamilyVal = "dnf"
				p.hostPlatformVal = rhelPolicyTestIdentity()
				p.hostPlatformKnown = true
				p.pkgFamilyMu.Unlock()
			}

			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(
				`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`,
			))
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			assertDNSSetupSettings(t, p, map[string]string{
				settingNS1:       "ns1.celikhost.com",
				settingNS2:       "ns2.celikhost.com",
				settingDNSRole:   "paired",
				settingDNSPeerIP: "2.25.80.4",
				settingDNSPeerNS: "ns2.celikhost.com",
			})
			if got := recordContent(t, p, zoneID, "preflight.example", "SOA"); got != beforeSOA {
				t.Fatalf("preflight rejection rewrote SOA: before=%q after=%q", beforeSOA, got)
			}
			afterState, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "preflight.example")
			if err != nil {
				t.Fatal(err)
			}
			if afterState.hasLease() || afterState.DesiredGeneration != beforeState.DesiredGeneration ||
				afterState.AppliedGeneration != beforeState.AppliedGeneration || afterState.Status != beforeState.Status {
				t.Fatalf("preflight rejection mutated DNS ledger: before=%+v after=%+v", beforeState, afterState)
			}

			agent.mu.Lock()
			clusterCalls := agent.clusterCalls
			beginCalls := agent.beginCalls
			syncCalls := len(agent.syncCalls)
			agent.mu.Unlock()
			agent.durableMutationRPCFixture.mu.Lock()
			jobs := len(agent.durableMutationRPCFixture.jobs)
			agent.durableMutationRPCFixture.mu.Unlock()
			if clusterCalls != 0 || beginCalls != 0 || syncCalls != 0 || jobs != 0 {
				t.Fatalf("preflight rejection reached host: configure=%d begin=%d V2=%d jobs=%d",
					clusterCalls, beginCalls, syncCalls, jobs)
			}
		})
	}
}

func TestDNSSetupDesiredPersistenceFailurePrecedesAgentBegin(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)
	rejectDNSClusterSettingWrites(t, p)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`,
	))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	beginCalls, clusterCalls := agent.beginCalls, agent.clusterCalls
	agent.mu.Unlock()
	if beginCalls != 0 || clusterCalls != 0 {
		t.Fatalf("failed desired transaction reached host begin/cluster=%d/%d",
			beginCalls, clusterCalls)
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns1.celikhost.com",
		settingNS2:       "ns2.celikhost.com",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "2.25.80.4",
		settingDNSPeerNS: "ns2.celikhost.com",
	})
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("rolled-back desired transaction retained saga=%+v", pending)
	}
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

func TestLegacyDNSSettingsPUTsRequireCompleteSetupWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		path    string
		body    string
		handler func(*Panel, http.ResponseWriter, *http.Request)
	}{
		{
			name:    "nameserver pair",
			path:    "/api/v1/settings/nameservers",
			body:    `{"ns1":"ns3.example.net","ns2":"ns4.example.net"}`,
			handler: (*Panel).handleNameserverSettings,
		},
		{
			name:    "cluster tuple",
			path:    "/api/v1/settings/dns-cluster",
			body:    `{"role":"standalone"}`,
			handler: (*Panel).handleDNSCluster,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "paired")
			agent := &strictDNSRPCAgent{}
			attachStrictDNSRPCAgent(t, p, agent)

			recorder := httptest.NewRecorder()
			request := strictDNSAdminRequest(httptest.NewRequest(
				http.MethodPut, tc.path, strings.NewReader(tc.body),
			))
			tc.handler(p, recorder, request)
			assertDNSSetupRequired(t, recorder)

			assertDNSSetupSettings(t, p, map[string]string{
				settingNS1:       "ns1.celikhost.com",
				settingNS2:       "ns2.celikhost.com",
				settingDNSRole:   "paired",
				settingDNSPeerIP: "2.25.80.4",
				settingDNSPeerNS: "ns2.celikhost.com",
			})
			agent.mu.Lock()
			clusterCalls := agent.clusterCalls
			syncCalls := append([]string(nil), agent.syncCalls...)
			agent.mu.Unlock()
			if clusterCalls != 0 || len(syncCalls) != 0 {
				t.Fatalf("legacy PUT reached agent: cluster=%d sync=%v", clusterCalls, syncCalls)
			}
		})
	}
}

func TestLegacyDNSSettingsGETsRemainCompatible(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	previousResolvers := nameserverResolvers
	nameserverResolvers = []hostResolver{fakeNameserverHostResolver{
		"ns1.celikhost.com": {"192.0.2.10"},
		"ns2.celikhost.com": {"192.0.2.10"},
	}}
	t.Cleanup(func() { nameserverResolvers = previousResolvers })

	namesRecorder := httptest.NewRecorder()
	p.handleNameserverSettings(namesRecorder, strictDNSAdminRequest(httptest.NewRequest(
		http.MethodGet, "/api/v1/settings/nameservers", nil,
	)))
	if namesRecorder.Code != http.StatusOK {
		t.Fatalf("nameserver GET status = %d, want 200; body=%s", namesRecorder.Code, namesRecorder.Body.String())
	}
	var names nameserverSettings
	if err := json.NewDecoder(namesRecorder.Body).Decode(&names); err != nil {
		t.Fatalf("decode nameserver GET: %v", err)
	}
	if names.NS1 != "ns1.celikhost.com" || names.NS2 != "ns2.celikhost.com" || names.Derived {
		t.Fatalf("nameserver GET changed contract: %+v", names)
	}

	clusterRecorder := httptest.NewRecorder()
	p.handleDNSCluster(clusterRecorder, strictDNSAdminRequest(httptest.NewRequest(
		http.MethodGet, "/api/v1/settings/dns-cluster", nil,
	)))
	if clusterRecorder.Code != http.StatusOK {
		t.Fatalf("cluster GET status = %d, want 200; body=%s", clusterRecorder.Code, clusterRecorder.Body.String())
	}
	var cluster dnsClusterView
	if err := json.NewDecoder(clusterRecorder.Body).Decode(&cluster); err != nil {
		t.Fatalf("decode cluster GET: %v", err)
	}
	if !cluster.Configured || cluster.Role != "standalone" ||
		cluster.NS1 != names.NS1 || cluster.NS2 != names.NS2 {
		t.Fatalf("cluster GET changed contract: %+v", cluster)
	}
}

func TestLegacyDNSTopologyPUTFailsClosedWhileAtomicSetupIsInFlight(t *testing.T) {
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

	legacy := httptest.NewRecorder()
	p.handleNameserverSettings(legacy, strictDNSAdminRequest(httptest.NewRequest(
		http.MethodPut, "/api/v1/settings/nameservers",
		strings.NewReader(`{"ns1":"ns3.example.net","ns2":"ns4.example.net"}`),
	)))
	assertDNSSetupRequired(t, legacy)
	agent.mu.Lock()
	clusterCalls := agent.clusterCalls
	agent.mu.Unlock()
	if clusterCalls != 1 {
		t.Fatalf("legacy PUT reached the agent while setup was in flight: cluster calls=%d", clusterCalls)
	}
	close(release)

	select {
	case recorder := <-firstDone:
		if recorder.Code != http.StatusOK {
			t.Fatalf("setup status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("setup did not finish after releasing the topology lock")
	}
}
