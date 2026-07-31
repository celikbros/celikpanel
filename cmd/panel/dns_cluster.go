package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

type dnsClusterAgentState struct {
	Role   string `json:"role"`
	PeerIP string `json:"peer_ip"`
	PeerNS string `json:"peer_ns"`
}

type dnsClusterAgentResponse struct {
	Applied bool   `json:"applied"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

type dnsClusterReadinessResponse struct {
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

func (p *Panel) applyDNSClusterAgent(state dnsClusterAgentState) (dnsClusterAgentResponse, error) {
	var resp dnsClusterAgentResponse
	err := p.agentClient.Call("Agent.ConfigureDNSCluster", &state, &resp)
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
// operator's saved nameserver pair and live DNS facts. It deliberately returns
// no suggestion when either name is ambiguous: a guess here could silently
// configure the wrong machine as the peer.
func suggestDNSClusterPeer(serverIP, ns1, ns2 string, facts []nameserverFact) (localNS, peerNS, peerIP string) {
	localIPv4, ok := canonicalIPv4(serverIP)
	if !ok || len(facts) != 2 {
		return "", "", ""
	}

	ns1, ns2 = canonicalDNSName(ns1), canonicalDNSName(ns2)
	if ns1 == "" || ns2 == "" || ns1 == ns2 {
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

	localNames := make([]string, 0, 1)
	for _, name := range []string{ns1, ns2} {
		fact := byName[name]
		if !fact.PointsHere {
			continue
		}
		localAddressPresent := false
		for _, rawIP := range fact.IPs {
			if ip, valid := canonicalIPv4(rawIP); valid && ip == localIPv4 {
				localAddressPresent = true
				break
			}
		}
		if !localAddressPresent {
			return "", "", ""
		}
		localNames = append(localNames, name)
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
	peerAddresses := make(map[string]struct{})
	for _, rawIP := range byName[peerNS].IPs {
		if ip, valid := canonicalIPv4(rawIP); valid {
			peerAddresses[ip] = struct{}{}
		}
	}
	if len(peerAddresses) != 1 {
		return "", "", ""
	}
	for ip := range peerAddresses {
		if ip == localIPv4 {
			return "", "", ""
		}
		peerIP = ip
	}
	return localNS, peerNS, peerIP
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
			if err := p.agentClient.Call("Agent.DNSClusterReadiness", &transport.Empty{}, &readiness); err == nil {
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
		if savedNS1, savedNS2 := p.configuredNameservers(r.Context()); savedNS1 != "" && savedNS2 != "" {
			v.SuggestedLocalNS, v.SuggestedPeerNS, v.SuggestedPeerIP = suggestDNSClusterPeer(
				v.ServerIP, savedNS1, savedNS2, v.Facts,
			)
		}
		peerPairVerified := false
		if v.Configured && v.Role == "paired" && v.PeerIP != "" {
			peerPairVerified = p.peerServesNameserverPair(r.Context(), v.PeerIP, v.NS1, v.NS2)
		}
		if v.Configured {
			v.Steps = planSteps(v.Role, v.ServerIP, v.PeerIP, v.PeerNS, v.Facts, peerPairVerified)
		}
		json.NewEncoder(w).Encode(v)

	case http.MethodPut:
		p.dnsTopologyMu.Lock()
		defer p.dnsTopologyMu.Unlock()

		var req struct {
			Role   string `json:"role"`
			PeerIP string `json:"peer_ip"`
			PeerNS string `json:"peer_ns"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}
		req.Role = normalizeDNSRole(strings.TrimSpace(req.Role))
		req.PeerIP = strings.TrimSpace(req.PeerIP)
		req.PeerNS = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(req.PeerNS, ".")))
		ns1, ns2 := p.configuredNameservers(r.Context())
		if ns1 == "" || ns2 == "" {
			writeClientError(w, http.StatusConflict, "save the two shared nameserver names before choosing an operating mode")
			return
		}
		localIPv4, ok := canonicalIPv4(serverPrimaryIP())
		if !ok {
			writeClientError(w, http.StatusConflict, "this server has no usable IPv4 address; set CELIKPANEL_SERVER_IP and retry")
			return
		}

		switch req.Role {
		case "standalone":
			req.PeerIP, req.PeerNS = "", ""
		case "paired":
			peerIP := net.ParseIP(req.PeerIP)
			peerIPv4 := peerIP.To4()
			if peerIPv4 == nil || !peerIPv4.IsGlobalUnicast() {
				writeClientError(w, http.StatusBadRequest, "enter the other server's IPv4 address")
				return
			}
			req.PeerIP = peerIPv4.String()
			if !validDNSHostname(req.PeerNS) {
				writeClientError(w, http.StatusBadRequest, "enter the other server's nameserver name, for example ns2.example.com")
				return
			}
			// Pointing a server at itself is not a cluster, and it would make
			// PowerDNS notify and transfer in a loop.
			// Bir sunucuyu kendine yöneltmek küme değildir; PowerDNS'i döngüde
			// bildirim ve aktarım yapmaya sokar.
			if req.PeerIP == localIPv4 {
				writeCodedError(w, http.StatusBadRequest, errCodeDNSClusterPeerIsLocal, "the other server cannot be this server", "")
				return
			}
			if !strings.EqualFold(req.PeerNS, ns1) && !strings.EqualFold(req.PeerNS, ns2) {
				writeClientError(w, http.StatusBadRequest, "the other server's name must be one of the two saved nameservers")
				return
			}
		default:
			writeClientError(w, http.StatusBadRequest, "role must be standalone or paired")
			return
		}

		previous, err := p.dnsClusterAgentSnapshot(r.Context())
		if err != nil {
			writeServerError(w, fmt.Errorf("read previous DNS cluster settings: %w", err))
			return
		}

		resp, err := p.applyDNSClusterAgent(dnsClusterAgentState{
			Role: req.Role, PeerIP: req.PeerIP, PeerNS: req.PeerNS,
		})
		if err != nil {
			writeAgentError(w, err, "DNS cluster")
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		if !resp.Applied {
			writeClientError(w, http.StatusConflict, "the DNS server did not confirm the cluster change")
			return
		}

		if err := p.saveDNSClusterSettingsAndReconcile(
			r.Context(), req.Role, req.PeerIP, req.PeerNS, ns1, ns2, localIPv4,
		); err != nil {
			rollbackResp, rollbackCallErr := p.applyDNSClusterAgent(previous)
			var rollbackErr error
			switch {
			case rollbackCallErr != nil:
				rollbackErr = rollbackCallErr
			case rollbackResp.Error != "":
				rollbackErr = fmt.Errorf("%s", rollbackResp.Error)
			case !rollbackResp.Applied:
				rollbackErr = fmt.Errorf("agent did not confirm the restored DNS role")
			}
			if rollbackErr != nil {
				log.Printf("[500][dns-cluster] save settings after agent apply: %v; restore previous role %q failed: %v",
					err, previous.Role, rollbackErr)
				writeCodedError(w, http.StatusInternalServerError, errCodeInternal,
					"DNS cluster settings could not be saved, and the previous DNS server role could not be restored; check the DNS service before retrying",
					"")
				return
			}
			log.Printf("[500][dns-cluster] save settings after agent apply: %v; previous role %q restored",
				err, previous.Role)
			writeCodedError(w, http.StatusInternalServerError, errCodeInternal,
				"DNS cluster settings could not be saved; the previous DNS server role was restored",
				"")
			return
		}
		// Re-publish every local zone after the mode changes. This advances each
		// SOA serial and sends NOTIFY, so pre-existing zones are copied to the
		// peer immediately instead of waiting for the next record edit.
		if _, err := p.syncAllZonesStrict(r.Context()); err != nil {
			err = fmt.Errorf("publish DNS cluster settings: %w", err)
			if writeDNSPublicationConflict(w, err,
				"DNS cluster settings were saved, but one or more zones could not be published; check the DNS service and retry") {
				return
			}
			writeServerError(w, err)
			return
		}
		p.audit(r, "settings.dns_cluster:"+req.Role+" peer="+req.PeerIP, "settings", 0)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "detail": resp.Detail})

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
