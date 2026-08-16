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

type CompensationDNSClusterReadinessResponse struct {
	Ready  bool
	Detail string
}

type compensationDNSAgent struct {
	mu              sync.Mutex
	failCall        int
	calls           []CompensationDNSClusterRequest
	readinessReady  bool
	readinessDetail string
}

func (a *compensationDNSAgent) Version(
	_ *transport.Empty,
	resp *transport.AgentVersionResponse,
) error {
	resp.Capabilities = []string{transport.AgentCapabilityDNSZoneSyncV2}
	return nil
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

func (a *compensationDNSAgent) DNSClusterReadiness(
	_ *transport.Empty,
	resp *CompensationDNSClusterReadinessResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	resp.Ready = a.readinessReady
	resp.Detail = a.readinessDetail
	return nil
}

func attachCompensationDNSAgent(t *testing.T, p *Panel, agent *compensationDNSAgent) {
	t.Helper()
	p.pkgFamilyVal = "apt"
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register fake DNS agent: %v", err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(
		nil,
		func(context.Context) (*rpc.Client, error) {
			serverConn, clientConn := net.Pipe()
			go server.ServeConn(serverConn)
			return rpc.NewClient(clientConn), nil
		},
	)
}

func rejectDNSClusterSettingWrites(t *testing.T, p *Panel) {
	t.Helper()
	if _, err := p.db.GetDB().Exec(`
		CREATE TRIGGER reject_dns_cluster_saga_insert
		BEFORE INSERT ON panel_settings WHEN NEW.key = 'dns_cluster_saga_v1'
		BEGIN SELECT RAISE(ABORT, 'forced DNS saga save failure'); END;
		CREATE TRIGGER reject_dns_cluster_saga_update
		BEFORE UPDATE ON panel_settings WHEN NEW.key = 'dns_cluster_saga_v1'
		BEGIN SELECT RAISE(ABORT, 'forced DNS saga save failure'); END;
	`); err != nil {
		t.Fatalf("install DNS saga failure trigger: %v", err)
	}
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
	if got.DNSServiceKnown {
		t.Fatal("GET without an agent reported a known DNS service state")
	}
}

func TestDNSClusterGETExposesPowerDNSReadiness(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ready     bool
		detail    string
		wantReady bool
	}{
		{
			name:      "installed",
			ready:     true,
			detail:    "PowerDNS is installed on this server",
			wantReady: true,
		},
		{
			name:      "missing",
			detail:    "PowerDNS is not installed on this server",
			wantReady: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			agent := &strictDNSRPCAgent{
				readinessReady:  tc.ready,
				readinessDetail: tc.detail,
			}
			attachStrictDNSRPCAgent(t, p, agent)

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
			if !got.DNSServiceKnown {
				t.Fatal("successful readiness RPC was reported as unknown")
			}
			if got.DNSServiceReady != tc.wantReady ||
				!strings.Contains(got.DNSServiceDetail, "PowerDNS") {
				t.Fatalf("DNS service = ready:%v detail:%q, want safe PowerDNS readiness %v",
					got.DNSServiceReady, got.DNSServiceDetail, tc.wantReady)
			}
			if got.DNSServiceDetail == tc.detail {
				t.Fatalf("raw agent readiness detail reached client: %q", got.DNSServiceDetail)
			}
		})
	}
}

func TestDNSClusterGETExposesExactActiveBINDWithoutPDNSReadiness(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := &strictDNSRPCAgent{
		readinessDetail: "secret standby PowerDNS path /etc/pdns/pdns.conf",
	}
	attachStrictDNSRPCAgentForEngine(
		t, p, agent, transport.DNSEngineBIND,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/dns-cluster", nil)
	req = req.WithContext(context.WithValue(
		req.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin},
	))
	recorder := httptest.NewRecorder()
	p.handleDNSCluster(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var got dnsClusterView
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.DNSServiceKnown || !got.DNSServiceReady ||
		!strings.Contains(got.DNSServiceDetail, "BIND") ||
		strings.Contains(recorder.Body.String(), "/etc/pdns") {
		t.Fatalf("active BIND readiness is unsafe or incomplete: %+v", got)
	}
	agent.mu.Lock()
	readinessCalls := agent.readinessCalls
	agent.mu.Unlock()
	if readinessCalls != 0 {
		t.Fatalf("active BIND queried standby PowerDNS readiness %d time(s)", readinessCalls)
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
