package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The DNS cluster settings the operator enters — and the panel then applies.
//
// Two machines hold every zone and answer with equal authority. Ownership is
// per-zone: the panel that creates a zone is its primary and the peer keeps a
// secondary copy. This lets operators add domains on either panel without
// confusing DNS authority with where the website itself runs.
//
// The settings are entered here, in the panel, by the operator. That is not a
// formality: a panel whose owner has to be talked through an SSH session to
// finish a feature has not finished the feature.
//
// Operatörün gireceği — ve panelin sonra uygulayacağı — DNS küme ayarları.
//
// İki makine her zone'u tutar ve eşit yetkiyle cevap verir. Sahiplik zone
// başınadır: zone'u oluşturan panel onun birincili, eş makine ikincil kopyasıdır.
// Böylece DNS yetkisi ile sitenin koştuğu yer karıştırılmadan iki panele de
// domain eklenebilir.
//
// Ayarlar burada, panelde, operatör tarafından girilir. Bu bir formalite
// değildir: sahibine bir özelliği tamamlatmak için SSH oturumu anlattırılan
// panel, o özelliği tamamlamamıştır.

const (
	settingDNSRole   = "dns_role"
	settingDNSPeerIP = "dns_peer_ip"
	settingDNSPeerNS = "dns_peer_ns"
)

type dnsClusterAgentState = transport.DNSClusterRequest

type dnsClusterAgentResponse = transport.DNSClusterResponse

type dnsClusterReadinessResponse = transport.DNSClusterReadinessResponse

func (p *Panel) applyDNSClusterAgent(state dnsClusterAgentState) (dnsClusterAgentResponse, error) {
	var resp dnsClusterAgentResponse
	err := p.callAgent("Agent.ConfigureDNSCluster", &state, &resp)
	return resp, err
}

// dnsClusterAgentSnapshot reads the three related settings consistently before
// the agent is changed. An empty role is the pre-configuration state and maps
// to standalone, which is the only safe agent state to restore.
func (p *Panel) dnsClusterAgentSnapshot(ctx context.Context) (dnsClusterAgentState, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return dnsClusterAgentState{}, err
	}
	defer tx.Rollback()

	rawRole, err := settingTx(ctx, tx, settingDNSRole)
	if err != nil {
		return dnsClusterAgentState{}, err
	}
	peerIP, err := settingTx(ctx, tx, settingDNSPeerIP)
	if err != nil {
		return dnsClusterAgentState{}, err
	}
	peerNS, err := settingTx(ctx, tx, settingDNSPeerNS)
	if err != nil {
		return dnsClusterAgentState{}, err
	}
	if err := tx.Commit(); err != nil {
		return dnsClusterAgentState{}, err
	}

	rawRole = strings.TrimSpace(rawRole)
	role := normalizeDNSRole(rawRole)
	if rawRole == "" {
		role = "standalone"
	} else if role == "" {
		return dnsClusterAgentState{}, fmt.Errorf("stored DNS role %q is invalid", rawRole)
	}
	if role == "standalone" {
		peerIP, peerNS = "", ""
	}
	return dnsClusterAgentState{
		Role:   role,
		PeerIP: strings.TrimSpace(peerIP),
		PeerNS: strings.ToLower(strings.TrimSpace(strings.TrimSuffix(peerNS, "."))),
	}, nil
}

type dnsClusterView struct {
	Configured       bool   `json:"configured"`
	Role             string `json:"role"`
	PeerIP           string `json:"peer_ip"`
	PeerNS           string `json:"peer_ns"`
	DNSServiceKnown  bool   `json:"dns_service_known"`
	DNSServiceReady  bool   `json:"dns_service_ready"`
	DNSServiceDetail string `json:"dns_service_detail,omitempty"`
	SuggestedLocalNS string `json:"suggested_local_ns,omitempty"`
	SuggestedPeerNS  string `json:"suggested_peer_ns,omitempty"`
	SuggestedPeerIP  string `json:"suggested_peer_ip,omitempty"`
	// PeerReachable reports whether the other server actually answers DNS on
	// port 53 from here. A cluster whose halves cannot reach each other is a
	// cluster on paper only, and the operator should learn that on the settings
	// screen rather than from a domain that stops resolving next month.
	// PeerReachable, diğer sunucunun buradan 53 portunda gerçekten DNS'e cevap
	// verip vermediğini bildirir. Yarıları birbirine ulaşamayan küme yalnız
	// kâğıt üstündedir; operatör bunu gelecek ay çözülmeyi bırakan bir alan
	// adından değil, ayar ekranından öğrenmelidir.
	PeerReachable bool   `json:"peer_reachable"`
	ServerIP      string `json:"server_ip"`

	// The names and where they really point, plus what is LEFT TO DO. The
	// first version of this screen showed a green tick beside each name that
	// resolved to this server — and on a lone machine BOTH names do, so the
	// screen drew a tidy pair of green ticks over the exact problem the
	// feature exists to fix: no backup at all. The operator's verdict was
	// blunt and correct: "you are guiding me terribly."
	//
	// A checklist replaces the ticks. It is computed from what is actually
	// true, it says which name is wrong and where it must point instead, and
	// it names the steps that must happen somewhere else — at the registrar,
	// or on the other server's own panel. A screen that only validates fields
	// leaves the operator to assemble the plan; this one hands them the plan.
	//
	// Adlar, gerçekte nereyi gösterdikleri ve GERİYE NE KALDIĞI. Bu ekranın ilk
	// hâli, bu sunucuya çözülen her adın yanına yeşil bir tik koyuyordu — ve tek
	// başına bir makinede İKİ ad da öyle çözülür; yani ekran, özelliğin var olma
	// sebebi olan sorunun üstüne düzgün bir çift yeşil tik çiziyordu: hiç yedek
	// yok. Operatörün hükmü açık ve doğruydu: "berbat yönlendiriyorsun."
	//
	// Tiklerin yerine bir kontrol listesi geçti. Gerçekten doğru olandan
	// hesaplanır, hangi adın yanlış olduğunu ve bunun yerine nereyi göstermesi
	// gerektiğini söyler, ve başka bir yerde — kayıtçıda ya da diğer sunucunun
	// kendi panelinde — olması gereken adımları adıyla anar. Yalnız alan
	// doğrulayan bir ekran, planı operatöre kurdurur; bu ekran planı ona verir.
	NS1   string           `json:"ns1"`
	NS2   string           `json:"ns2"`
	Facts []nameserverFact `json:"facts"`
	Steps []clusterStep    `json:"steps"`
}

// clusterStep is one line of "what is left". Manual marks a step this server
// cannot verify because it happens elsewhere — at the registrar, or on the
// other machine. Pretending to check those would be worse than admitting we
// cannot.
// clusterStep, "geriye ne kaldı"nın bir satırıdır. Manual, bu sunucunun
// doğrulayamayacağı bir adımı işaretler; çünkü başka bir yerde olur — kayıtçıda
// ya da öbür makinede. Onları kontrol ediyormuş gibi yapmak, edemediğimizi
// kabul etmekten kötü olurdu.
type clusterStep struct {
	Code   string   `json:"code"`
	Done   bool     `json:"done"`
	Manual bool     `json:"manual"`
	Args   []string `json:"args,omitempty"`
}

// planSteps works out what still has to happen from saved topology and live
// address facts. Its four paired-mode steps have stable argument positions so
// the UI never has to guess which value is a hostname and which is an IP.
func planSteps(role, serverIP, peerIP, peerNS string, facts []nameserverFact, peerPairVerified bool) []clusterStep {
	if role == "standalone" {
		return []clusterStep{{Code: "aloneNoBackup", Manual: true}}
	}

	localNS := ""
	for _, f := range facts {
		if !strings.EqualFold(f.Host, peerNS) {
			localNS = f.Host
			break
		}
	}
	localReady, peerReady := false, false
	for _, f := range facts {
		switch {
		case strings.EqualFold(f.Host, localNS):
			localReady = f.PointsHere
		case strings.EqualFold(f.Host, peerNS):
			peerReady = peerIP != "" && containsStr(f.IPs, peerIP)
		}
	}
	return []clusterStep{
		{Code: "localName", Done: localReady, Manual: true, Args: []string{localNS, serverIP}},
		{Code: "peerName", Done: peerReady, Manual: true, Args: []string{peerNS, peerIP}},
		{Code: "peerPort", Done: peerIP != "" && dnsPortAnswers(peerIP)},
		{Code: "samePairOnPeer", Done: peerPairVerified, Args: []string{peerIP}},
	}
}

type nameserverSetResolver interface {
	LookupNS(context.Context, string) ([]*net.NS, error)
}

// peerServesNameserverPair asks the configured peer directly, bypassing the
// operating system resolver and its cache. A peer that returns the exact saved
// NS set for one of our locally managed zones (or for the parent that owns the
// nameserver names) proves the useful fact: both machines are serving the same
// DNS identity. That is stronger than asking an operator to compare two forms.
func (p *Panel) peerServesNameserverPair(ctx context.Context, peerIP, ns1, ns2 string) bool {
	if net.ParseIP(peerIP) == nil || ns1 == "" || ns2 == "" {
		return false
	}

	var zones []string
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT name FROM pdns_domains ORDER BY id LIMIT 8`)
	if err == nil {
		for rows.Next() {
			var zone string
			if rows.Scan(&zone) == nil {
				zones = append(zones, zone)
			}
		}
		_ = rows.Close()
	}
	for _, host := range []string{ns1, ns2} {
		if owner := baseDomainOf(host); owner != "" {
			zones = append(zones, owner)
		}
	}

	dialer := net.Dialer{Timeout: 1500 * time.Millisecond}
	resolver := &net.Resolver{
		PreferGo:     true,
		StrictErrors: true,
		Dial: func(queryCtx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(queryCtx, network, net.JoinHostPort(peerIP, "53"))
		},
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return resolverServesNameserverPair(queryCtx, resolver, zones, ns1, ns2)
}

func resolverServesNameserverPair(
	ctx context.Context,
	resolver nameserverSetResolver,
	zones []string,
	ns1, ns2 string,
) bool {
	want1 := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(ns1), "."))
	want2 := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(ns2), "."))
	seenZones := make(map[string]bool)
	for _, rawZone := range zones {
		zone := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rawZone), "."))
		if zone == "" || seenZones[zone] {
			continue
		}
		seenZones[zone] = true
		records, err := resolver.LookupNS(ctx, zone)
		if err != nil {
			continue
		}
		got1, got2 := false, false
		for _, record := range records {
			host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(record.Host), "."))
			got1 = got1 || host == want1
			got2 = got2 || host == want2
		}
		if got1 && got2 {
			return true
		}
	}
	return false
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// PowerDNS roles are per-zone. A paired server is primary for zones created
// on its own panel and secondary for zones arriving from its peer. Legacy
// machine-wide primary/secondary settings migrate to this symmetric model.
func normalizeDNSRole(role string) string {
	switch role {
	case "paired", "primary", "secondary":
		return "paired"
	case "standalone":
		return "standalone"
	default:
		return ""
	}
}

// suggestDNSClusterPeer derives a draft paired-node topology only from the
// effective nameserver pair and live DNS facts. The effective pair may be the
// operator's saved pair or the safe ns1/ns2 default derived from the panel
// hostname on a fresh install. It deliberately returns no suggestion when
// either name or address is ambiguous: a guess here could silently configure
// the wrong machine as the peer.
func suggestDNSClusterPeer(serverIP, ns1, ns2 string, facts []nameserverFact) (localNS, peerNS, peerIP string) {
	localIPv4, ok := canonicalIPv4(serverIP)
	localIP := net.ParseIP(localIPv4)
	if !ok || localIP == nil || !localIP.To4().IsGlobalUnicast() || len(facts) != 2 {
		return "", "", ""
	}

	ns1, ns2 = canonicalDNSName(ns1), canonicalDNSName(ns2)
	if !validDNSHostname(ns1) || !validDNSHostname(ns2) || ns1 == ns2 {
		return "", "", ""
	}

	saved := map[string]struct{}{ns1: {}, ns2: {}}
	byName := make(map[string]nameserverFact, 2)
	for _, fact := range facts {
		name := canonicalDNSName(fact.Host)
		if _, isSaved := saved[name]; !isSaved {
			return "", "", ""
		}
		if _, duplicate := byName[name]; duplicate {
			return "", "", ""
		}
		byName[name] = fact
	}
	if len(byName) != 2 {
		return "", "", ""
	}

	// A local name is safe to infer only when every IPv4 answer canonicalizes
	// to this server. In particular, an A record that contains both machines
	// must not be treated as this node's identity.
	localNames := make([]string, 0, 1)
	for _, name := range []string{ns1, ns2} {
		fact := byName[name]
		addresses := canonicalIPv4AnswerSet(fact.IPs)
		if len(addresses) == 1 {
			if _, isLocal := addresses[localIPv4]; isLocal {
				localNames = append(localNames, name)
			}
		}
	}
	if len(localNames) != 1 {
		return "", "", ""
	}

	localNS = localNames[0]
	if localNS == ns1 {
		peerNS = ns2
	} else {
		peerNS = ns1
	}
	peerAddresses := canonicalIPv4AnswerSet(byName[peerNS].IPs)
	if len(peerAddresses) != 1 {
		return "", "", ""
	}
	for ip := range peerAddresses {
		if !safeDNSClusterPeerIPv4(ip, localIPv4) {
			return "", "", ""
		}
		peerIP = ip
	}
	return localNS, peerNS, peerIP
}

// canonicalIPv4AnswerSet collapses equivalent DNS answers (for example an
// IPv4 address and its IPv4-mapped representation) without hiding additional
// IPv4 addresses. IPv6 answers are outside this IPv4 cluster tuple and do not
// make an otherwise exact A-record mapping ambiguous.
func canonicalIPv4AnswerSet(raw []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, value := range raw {
		if ip, ok := canonicalIPv4(value); ok {
			out[ip] = struct{}{}
		}
	}
	return out
}

// safeDNSClusterPeerIPv4 mirrors the DNS setup validator: the peer must be a
// canonical global-unicast IPv4 address and must not be this server. Go treats
// routed private addresses as global-unicast; keeping that behavior is
// intentional because the setup endpoint permits private replication links.
func safeDNSClusterPeerIPv4(value, localIPv4 string) bool {
	canonical, ok := canonicalIPv4(value)
	if !ok || canonical == localIPv4 {
		return false
	}
	ip := net.ParseIP(canonical)
	return ip != nil && ip.To4() != nil && ip.To4().IsGlobalUnicast()
}

func (p *Panel) handleDNSCluster(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}

	switch r.Method {
	case http.MethodGet:
		rawRole := p.setting(r.Context(), settingDNSRole)
		v := dnsClusterView{
			Role:       normalizeDNSRole(rawRole),
			Configured: rawRole == "standalone" || rawRole == "paired",
			PeerIP:     p.setting(r.Context(), settingDNSPeerIP),
			PeerNS:     p.setting(r.Context(), settingDNSPeerNS),
			ServerIP:   serverPrimaryIP(),
			Steps:      make([]clusterStep, 0),
		}
		if p.agentClient != nil {
			var readiness dnsClusterReadinessResponse
			if err := p.callAgent("Agent.DNSClusterReadiness", &transport.Empty{}, &readiness); err == nil {
				v.DNSServiceKnown = true
				v.DNSServiceReady = readiness.Ready
				v.DNSServiceDetail = readiness.Detail
			}
		}
		if v.PeerIP != "" {
			v.PeerReachable = dnsPortAnswers(v.PeerIP)
		}
		v.NS1, v.NS2 = p.serverNameservers(r.Context())
		v.Facts = verifyNameservers(r.Context(), []string{v.NS1, v.NS2}, serverPrimaryIP(), serverPrimaryIPv6())
		v.SuggestedLocalNS, v.SuggestedPeerNS, v.SuggestedPeerIP = suggestDNSClusterPeer(
			v.ServerIP, v.NS1, v.NS2, v.Facts,
		)
		peerPairVerified := false
		if v.Configured && v.Role == "paired" && v.PeerIP != "" {
			peerPairVerified = p.peerServesNameserverPair(r.Context(), v.PeerIP, v.NS1, v.NS2)
		}
		if v.Configured {
			v.Steps = planSteps(v.Role, v.ServerIP, v.PeerIP, v.PeerNS, v.Facts, peerPairVerified)
		}
		json.NewEncoder(w).Encode(v)

	case http.MethodPut:
		writeDNSSetupRequired(w)

	default:
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// dnsPortAnswers reports whether something is listening for DNS at that
// address. TCP/53 because a UDP probe cannot distinguish "no answer" from
// "silently dropped", and every authoritative server must accept TCP anyway.
// dnsPortAnswers, o adreste DNS'i dinleyen bir şey olup olmadığını bildirir.
// TCP/53, çünkü UDP yoklaması "cevap yok" ile "sessizce düşürüldü"yü ayırt
// edemez ve zaten her yetkili sunucu TCP kabul etmek zorundadır.
func dnsPortAnswers(ip string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "53"), 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// dnsZoneType is the type a NEW local zone gets on this server: MASTER in
// paired mode so it is transferred to the peer, NATIVE when this server works
// alone. Zones created by the peer arrive separately as SLAVE/SECONDARY.
// dnsZoneType, bu sunucuda YENİ yerel zone'un alacağı tiptir: eşli modda eşe
// aktarılabilmesi için MASTER, sunucu tek başına çalışıyorsa NATIVE. Eşin
// oluşturduğu zone'lar ayrıca SLAVE/SECONDARY olarak gelir.
func (p *Panel) dnsZoneType(ctx context.Context) string {
	if normalizeDNSRole(p.setting(ctx, settingDNSRole)) == "paired" {
		return "MASTER"
	}
	return "NATIVE"
}
