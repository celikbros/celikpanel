package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type fakeNameserverSetResolver map[string][]string

func (f fakeNameserverSetResolver) LookupNS(_ context.Context, zone string) ([]*net.NS, error) {
	hosts, ok := f[zone]
	if !ok {
		return nil, errors.New("not found")
	}
	out := make([]*net.NS, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, &net.NS{Host: host})
	}
	return out, nil
}

type fakeNameserverHostResolver map[string][]string

func (f fakeNameserverHostResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	addrs, ok := f[canonicalDNSName(host)]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]string(nil), addrs...), nil
}

type CompensationDNSClusterRequest struct {
	Role   string
	PeerIP string
	PeerNS string
}

type CompensationDNSClusterResponse struct {
	Applied bool
	Detail  string
	Error   string
}

type compensationDNSAgent struct {
	mu       sync.Mutex
	failCall int
	calls    []CompensationDNSClusterRequest
}

func (a *compensationDNSAgent) ConfigureDNSCluster(
	req *CompensationDNSClusterRequest,
	resp *CompensationDNSClusterResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, *req)
	if len(a.calls) == a.failCall {
		resp.Error = "forced rollback failure with internal detail"
		return nil
	}
	resp.Applied = true
	resp.Detail = "configured"
	return nil
}

func attachCompensationDNSAgent(t *testing.T, p *Panel, agent *compensationDNSAgent) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register fake DNS agent: %v", err)
	}
	go server.ServeConn(serverConn)
	rawClient := rpc.NewClient(clientConn)
	p.agentClient = transport.NewReconnectingClient(rawClient)
	t.Cleanup(func() {
		_ = rawClient.Close()
		_ = serverConn.Close()
	})
}

func rejectDNSClusterSettingWrites(t *testing.T, p *Panel) {
	t.Helper()
	if _, err := p.db.GetDB().Exec(`
		CREATE TRIGGER reject_dns_role_insert
		BEFORE INSERT ON panel_settings WHEN NEW.key = 'dns_role'
		BEGIN SELECT RAISE(ABORT, 'forced DNS role save failure'); END;
		CREATE TRIGGER reject_dns_role_update
		BEFORE UPDATE ON panel_settings WHEN NEW.key = 'dns_role'
		BEGIN SELECT RAISE(ABORT, 'forced DNS role save failure'); END;
	`); err != nil {
		t.Fatalf("install DNS role failure trigger: %v", err)
	}
}

func compensationAdminRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/dns-cluster", strings.NewReader(body))
	return req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}))
}

func TestDNSClusterGETDoesNotPresentUnconfiguredModeAsStandalone(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/dns-cluster", nil)
	req = req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}))
	recorder := httptest.NewRecorder()
	p.handleDNSCluster(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var got dnsClusterView
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Configured {
		t.Fatal("fresh DNS cluster reported configured")
	}
	if got.Role != "" {
		t.Fatalf("fresh DNS cluster role = %q, want an explicit empty role", got.Role)
	}
	if got.Steps == nil {
		t.Fatal("fresh DNS cluster steps encoded as null, want an empty array")
	}
	if len(got.Steps) != 0 {
		t.Fatalf("fresh DNS cluster exposed setup steps for an unsaved mode: %+v", got.Steps)
	}
}

func TestSuggestDNSClusterPeerRequiresUnambiguousSavedPair(t *testing.T) {
	const (
		frankfurtIP = "72.62.38.15"
		bostonIP    = "2.25.80.4"
		ns1         = "ns1.celikhost.com"
		ns2         = "ns2.celikhost.com"
	)
	tests := []struct {
		name                    string
		serverIP                string
		facts                   []nameserverFact
		wantLocal, wantPeer, ip string
	}{
		{
			name: "Boston derives ns2 as local", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{frankfurtIP}},
				{Host: ns2, IPs: []string{bostonIP}, PointsHere: true},
			},
			wantLocal: ns2, wantPeer: ns1, ip: frankfurtIP,
		},
		{
			name: "Frankfurt derives ns1 as local", serverIP: frankfurtIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{frankfurtIP}, PointsHere: true},
				{Host: ns2, IPs: []string{bostonIP}},
			},
			wantLocal: ns1, wantPeer: ns2, ip: bostonIP,
		},
		{
			name: "duplicate equivalent peer answers are one canonical IPv4", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{frankfurtIP, "::ffff:72.62.38.15"}},
				{Host: ns2, IPs: []string{bostonIP}, PointsHere: true},
			},
			wantLocal: ns2, wantPeer: ns1, ip: frankfurtIP,
		},
		{
			name: "both names point here", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{bostonIP}, PointsHere: true},
				{Host: ns2, IPs: []string{bostonIP}, PointsHere: true},
			},
		},
		{
			name: "neither name points here", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{frankfurtIP}},
				{Host: ns2, IPs: []string{"198.51.100.2"}},
			},
		},
		{
			name: "peer has multiple distinct IPv4 answers", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{frankfurtIP, "198.51.100.3"}},
				{Host: ns2, IPs: []string{bostonIP}, PointsHere: true},
			},
		},
		{
			name: "peer canonical IPv4 is local address", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{"::ffff:2.25.80.4"}},
				{Host: ns2, IPs: []string{bostonIP}, PointsHere: true},
			},
		},
		{
			name: "points-here marker lacks local IPv4", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{frankfurtIP}},
				{Host: ns2, IPs: []string{"2001:db8::2"}, PointsHere: true},
			},
		},
		{
			name: "facts do not contain both saved names", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns1, IPs: []string{frankfurtIP}},
				{Host: "ns3.celikhost.com", IPs: []string{bostonIP}, PointsHere: true},
			},
		},
		{
			name: "duplicate fact name", serverIP: bostonIP,
			facts: []nameserverFact{
				{Host: ns2, IPs: []string{bostonIP}, PointsHere: true},
				{Host: ns2, IPs: []string{frankfurtIP}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, peer, peerIP := suggestDNSClusterPeer(tt.serverIP, ns1, ns2, tt.facts)
			if local != tt.wantLocal || peer != tt.wantPeer || peerIP != tt.ip {
				t.Fatalf("suggestion = (%q, %q, %q), want (%q, %q, %q)",
					local, peer, peerIP, tt.wantLocal, tt.wantPeer, tt.ip)
			}
		})
	}
}

func TestDNSClusterGETSuggestsSavedPairWithoutMutatingSettings(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "2.25.80.4")
	p := newDNSPanelForTest(t)
	ctx := context.Background()
	for key, value := range map[string]string{
		settingNS1: "ns1.celikhost.com",
		settingNS2: "ns2.celikhost.com",
	} {
		if err := p.setSetting(ctx, key, value); err != nil {
			t.Fatalf("save %s: %v", key, err)
		}
	}

	previousResolvers := nameserverResolvers
	nameserverResolvers = []hostResolver{fakeNameserverHostResolver{
		"ns1.celikhost.com": {"72.62.38.15"},
		"ns2.celikhost.com": {"2.25.80.4"},
	}}
	t.Cleanup(func() { nameserverResolvers = previousResolvers })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/dns-cluster", nil)
	req = req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}))
	recorder := httptest.NewRecorder()
	p.handleDNSCluster(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var got dnsClusterView
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Configured || got.Role != "" {
		t.Fatalf("read-only suggestion configured a mode: configured=%v role=%q", got.Configured, got.Role)
	}
	if got.SuggestedLocalNS != "ns2.celikhost.com" ||
		got.SuggestedPeerNS != "ns1.celikhost.com" ||
		got.SuggestedPeerIP != "72.62.38.15" {
		t.Fatalf("unexpected suggestion: local=%q peer=%q peer_ip=%q", got.SuggestedLocalNS, got.SuggestedPeerNS, got.SuggestedPeerIP)
	}
	for _, key := range []string{settingDNSRole, settingDNSPeerIP, settingDNSPeerNS} {
		if value := p.setting(ctx, key); value != "" {
			t.Fatalf("GET mutated %s to %q", key, value)
		}
	}
}

func TestDNSClusterPUTRejectsLocalPeerWithStableCodeBeforeMutation(t *testing.T) {
	for _, peerIP := range []string{"2.25.80.4", "::ffff:2.25.80.4"} {
		t.Run(peerIP, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "2.25.80.4")
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "standalone")
			agent := &compensationDNSAgent{}
			attachCompensationDNSAgent(t, p, agent)

			recorder := httptest.NewRecorder()
			p.handleDNSCluster(recorder, compensationAdminRequest(
				`{"role":"paired","peer_ip":"`+peerIP+`","peer_ns":"ns1.celikhost.com"}`,
			))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			var body apiErrorBody
			if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body.Code != errCodeDNSClusterPeerIsLocal {
				t.Fatalf("code = %q, want %q", body.Code, errCodeDNSClusterPeerIsLocal)
			}

			agent.mu.Lock()
			callCount := len(agent.calls)
			agent.mu.Unlock()
			if callCount != 0 {
				t.Fatalf("self-peer rejection reached the agent %d time(s)", callCount)
			}
			ctx := context.Background()
			if role := p.setting(ctx, settingDNSRole); role != "standalone" {
				t.Fatalf("stored role = %q, want unchanged standalone", role)
			}
			if peer := p.setting(ctx, settingDNSPeerIP); peer != "" {
				t.Fatalf("stored peer IP mutated to %q", peer)
			}
			if peerNS := p.setting(ctx, settingDNSPeerNS); peerNS != "" {
				t.Fatalf("stored peer NS mutated to %q", peerNS)
			}
		})
	}
}

func TestDNSClusterAgentSnapshotStillRestoresEmptyRoleAsStandalone(t *testing.T) {
	p := newDNSPanelForTest(t)

	got, err := p.dnsClusterAgentSnapshot(context.Background())
	if err != nil {
		t.Fatalf("read agent snapshot: %v", err)
	}
	if got.Role != "standalone" || got.PeerIP != "" || got.PeerNS != "" {
		t.Fatalf("empty stored role snapshot = %+v, want safe standalone agent state", got)
	}
}

// The checklist is the guidance, so it is tested as guidance: for each real
// situation, does it name the ONE thing that is actually wrong?
//
// The screen it replaced drew a green tick beside every nameserver name that
// resolved to this server. On a lone machine both names do — so it drew two
// green ticks over the exact failure the pair exists to prevent, and the
// operator said so plainly: "you are guiding me terribly."
//
// Kontrol listesi rehberliğin kendisidir; bu yüzden rehberlik olarak sınanır:
// her gerçek durum için, gerçekten yanlış olan TEK şeyi adıyla söylüyor mu?
//
// Yerine geçtiği ekran, bu sunucuya çözülen her ad sunucusu adının yanına
// yeşil bir tik çiziyordu. Tek başına bir makinede iki ad da öyle çözülür —
// yani çiftin engellemek için var olduğu arızanın üstüne iki yeşil tik
// çiziyordu ve operatör bunu açıkça söyledi: "berbat yönlendiriyorsun."
func TestPlanStepsNamesTheRealProblem(t *testing.T) {
	const here, peer = "127.0.0.1", "127.0.0.2"
	facts := func(ip1, ip2 string) []nameserverFact {
		f := []nameserverFact{{Host: "ns1.celikhost.com"}, {Host: "ns2.celikhost.com"}}
		for i, ip := range []string{ip1, ip2} {
			if ip != "" {
				f[i].IPs = []string{ip}
				f[i].PointsHere = ip == here
			}
		}
		return f
	}
	codes := func(steps []clusterStep) map[string]clusterStep {
		m := map[string]clusterStep{}
		for _, s := range steps {
			m[s.Code] = s
		}
		return m
	}

	// Standalone: not a fault, but the consequence must be stated, not hidden.
	// Tek başına: arıza değil, ama sonucu gizlenmeyip söylenmeli.
	if got := codes(planSteps("standalone", here, "", "", facts(here, here), false)); len(got) != 1 || got["aloneNoBackup"].Code == "" {
		t.Errorf("standalone must say it has no backup, got %v", got)
	}

	// A configured pair still has work left while both names point here.
	// Yapılandırılmış bir çiftte iki ad da burayı gösteriyorsa iş bitmemiştir.
	got := codes(planSteps("paired", here, peer, "ns2.celikhost.com", facts(here, here), false))
	if !got["localName"].Done {
		t.Error("the local nameserver should be ready")
	}
	if got["peerName"].Done {
		t.Error("with both names here, the peer nameserver is NOT ready")
	}

	// Correctly split pair: one name here, one at the other server.
	// Doğru bölünmüş çift: bir ad burada, biri diğer sunucuda.
	got = codes(planSteps("paired", here, peer, "ns2.celikhost.com", facts(here, peer), true))
	if !got["localName"].Done || !got["peerName"].Done {
		t.Errorf("a correctly split pair must tick both name steps, got %+v", got)
	}
	// A direct query to the peer turns the former manual comparison into an
	// automatic operational check.
	// Eşe doğrudan sorgu, eski elle karşılaştırmayı otomatik çalışma
	// denetimine dönüştürür.
	if !got["samePairOnPeer"].Done || got["samePairOnPeer"].Manual {
		t.Error("a peer serving the saved pair must pass the automatic pair check")
	}

	// A name that resolves to a third machine is not "at the peer", and the
	// checklist must still point at the peer's address as the target.
	// Üçüncü bir makineye çözülen ad "diğer sunucuda" değildir ve liste hedef
	// olarak yine eşin adresini göstermelidir.
	got = codes(planSteps("paired", here, peer, "ns2.celikhost.com", facts(here, "203.0.113.7"), false))
	if got["peerName"].Done {
		t.Error("a name pointing at a third machine must not count as pointing at the peer")
	}
	if len(got["peerName"].Args) != 2 || got["peerName"].Args[1] != peer {
		t.Errorf("the step must name the peer address as the target, got %v", got["peerName"].Args)
	}
}

func TestResolverServesNameserverPair(t *testing.T) {
	resolver := fakeNameserverSetResolver{
		"empty.example": {"ns9.example.net."},
		"celikhost.com": {"NS2.CELIKHOST.COM.", "ns1.celikhost.com."},
	}
	if !resolverServesNameserverPair(
		context.Background(),
		resolver,
		[]string{"empty.example", "celikhost.com", "celikhost.com"},
		"ns1.celikhost.com",
		"ns2.celikhost.com",
	) {
		t.Fatal("the exact pair served by the peer was not detected")
	}
	if resolverServesNameserverPair(
		context.Background(),
		resolver,
		[]string{"empty.example"},
		"ns1.celikhost.com",
		"ns2.celikhost.com",
	) {
		t.Fatal("a partial or different NS set must not verify the pair")
	}
}

func TestNameserverPairUsableRequiresTwoDistinctNames(t *testing.T) {
	const here, peer = "72.62.38.15", "2.25.80.4"
	correct := []nameserverFact{
		{Host: "ns1.celikhost.com", IPs: []string{here}, PointsHere: true},
		{Host: "ns2.celikhost.com", IPs: []string{peer}},
	}
	if !nameserverPairUsable("paired", peer, correct) {
		t.Fatal("one local name plus one peer name must be usable")
	}
	if nameserverPairUsable("paired", peer, []nameserverFact{
		{Host: "ns1.celikhost.com", IPs: []string{here}, PointsHere: true},
		{Host: "ns2.celikhost.com", IPs: []string{here}, PointsHere: true},
	}) {
		t.Fatal("two names on the local server are not a usable pair")
	}
	if nameserverPairUsable("paired", peer, []nameserverFact{
		{Host: "ns1.celikhost.com", IPs: []string{here, peer}, PointsHere: true},
		{Host: "ns2.celikhost.com", IPs: nil},
	}) {
		t.Fatal("one multi-address name must not stand in for two distinct nameservers")
	}
	if !nameserverPairUsable("standalone", "", []nameserverFact{
		{Host: "ns1.celikhost.com", IPs: []string{here}, PointsHere: true},
		{Host: "ns2.celikhost.com", IPs: []string{here}, PointsHere: true},
	}) {
		t.Fatal("standalone mode may deliberately serve both names locally")
	}
	if nameserverPairUsable("", "", []nameserverFact{
		{Host: "ns1.celikhost.com", IPs: []string{here}, PointsHere: true},
		{Host: "ns2.celikhost.com", IPs: []string{here}, PointsHere: true},
	}) {
		t.Fatal("an unconfigured role must not report a ready DNS identity")
	}
}

func TestDNSClusterSaveFailureRestoresPreviousAgentRole(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "72.62.38.15")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &compensationDNSAgent{}
	attachCompensationDNSAgent(t, p, agent)
	rejectDNSClusterSettingWrites(t, p)

	recorder := httptest.NewRecorder()
	p.handleDNSCluster(recorder, compensationAdminRequest(`{"role":"standalone"}`))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "previous DNS server role was restored") {
		t.Fatalf("rollback success was not reported: %s", recorder.Body.String())
	}

	agent.mu.Lock()
	calls := append([]CompensationDNSClusterRequest(nil), agent.calls...)
	agent.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("agent calls = %+v, want apply plus compensation", calls)
	}
	if calls[0].Role != "standalone" || calls[0].PeerIP != "" || calls[0].PeerNS != "" {
		t.Fatalf("new agent state = %+v, want standalone", calls[0])
	}
	if calls[1].Role != "paired" || calls[1].PeerIP != "2.25.80.4" || calls[1].PeerNS != "ns2.celikhost.com" {
		t.Fatalf("compensation state = %+v, want previous paired state", calls[1])
	}
	if got := p.setting(context.Background(), settingDNSRole); got != "paired" {
		t.Fatalf("failed panel save changed stored role to %q", got)
	}
}

func TestDNSClusterRollbackFailureIsExplicitAndEmptyPreviousRoleMeansStandalone(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "72.62.38.15")
	p := newDNSPanelForTest(t)
	for key, value := range map[string]string{
		settingNS1: "ns1.celikhost.com",
		settingNS2: "ns2.celikhost.com",
	} {
		if err := p.setSetting(context.Background(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	agent := &compensationDNSAgent{failCall: 2}
	attachCompensationDNSAgent(t, p, agent)
	rejectDNSClusterSettingWrites(t, p)

	recorder := httptest.NewRecorder()
	p.handleDNSCluster(recorder, compensationAdminRequest(
		`{"role":"paired","peer_ip":"2.25.80.4","peer_ns":"ns2.celikhost.com"}`,
	))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "previous DNS server role could not be restored") {
		t.Fatalf("rollback failure was not explicit: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "forced rollback failure") {
		t.Fatalf("agent internals leaked in response: %s", recorder.Body.String())
	}

	agent.mu.Lock()
	calls := append([]CompensationDNSClusterRequest(nil), agent.calls...)
	agent.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("agent calls = %+v, want apply plus failed compensation", calls)
	}
	if calls[1].Role != "standalone" || calls[1].PeerIP != "" || calls[1].PeerNS != "" {
		t.Fatalf("empty previous role restored as %+v, want standalone", calls[1])
	}
	if got := p.setting(context.Background(), settingDNSRole); got != "" {
		t.Fatalf("failed panel save created stored role %q", got)
	}
}
