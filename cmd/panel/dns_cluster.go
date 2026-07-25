package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

// The DNS cluster settings the operator enters — and the panel then applies.
//
// This is the answer to "what is the professional way?": two nameservers on two
// different machines, each holding every zone, so one server going down does
// not take every hosted domain with it. One of them owns the data (primary),
// the other keeps a live copy (secondary). Both answer with equal authority,
// and a domain's website can live on either machine — DNS authority and web
// hosting are separate jobs.
//
// The settings are entered here, in the panel, by the operator. That is not a
// formality: a panel whose owner has to be talked through an SSH session to
// finish a feature has not finished the feature.
//
// Operatörün gireceği — ve panelin sonra uygulayacağı — DNS küme ayarları.
//
// Bu, "profesyonel olan nedir?" sorusunun cevabıdır: iki ayrı makinede iki ad
// sunucusu, her biri her zone'u tutar; böylece bir sunucunun düşmesi
// barındırılan her alan adını yanında götürmez. Biri verinin sahibidir
// (birincil), diğeri canlı kopya tutar (ikincil). İkisi de eşit yetkiyle cevap
// verir ve bir alan adının sitesi hangi makinede olursa olsun çalışır — DNS
// yetkisi ile web barındırma ayrı işlerdir.
//
// Ayarlar burada, panelde, operatör tarafından girilir. Bu bir formalite
// değildir: sahibine bir özelliği tamamlatmak için SSH oturumu anlattırılan
// panel, o özelliği tamamlamamıştır.

const (
	settingDNSRole   = "dns_role"
	settingDNSPeerIP = "dns_peer_ip"
	settingDNSPeerNS = "dns_peer_ns"
)

type dnsClusterView struct {
	Role   string `json:"role"`
	PeerIP string `json:"peer_ip"`
	PeerNS string `json:"peer_ns"`
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

// planSteps works out what still has to happen, from facts only.
// planSteps, geriye ne kaldığını yalnızca olgulardan çıkarır.
func planSteps(role, serverIP, peerIP string, facts []nameserverFact) []clusterStep {
	here, atPeer, unresolved := []string{}, []string{}, []string{}
	for _, f := range facts {
		switch {
		case len(f.IPs) == 0:
			unresolved = append(unresolved, f.Host)
		case f.PointsHere:
			here = append(here, f.Host)
		case peerIP != "" && containsStr(f.IPs, peerIP):
			atPeer = append(atPeer, f.Host)
		default:
			// resolves, but to neither of the two servers
			unresolved = append(unresolved, f.Host)
		}
	}

	if role == "standalone" {
		// Not a fault — a deliberate choice. But say the consequence out loud
		// instead of drawing green ticks over it.
		// Arıza değil — bilinçli bir tercih. Ama sonucunu yeşil tiklerle
		// örtmek yerine yüksek sesle söyle.
		return []clusterStep{{Code: "aloneNoBackup", Done: false, Manual: true}}
	}

	steps := []clusterStep{
		{Code: "oneNameHere", Done: len(here) == 1, Args: here},
		{Code: "otherNameAtPeer", Done: len(atPeer) == 1, Manual: true, Args: append(append([]string{}, unresolved...), peerIP)},
		{Code: "peerAnswers", Done: peerIP != "" && dnsPortAnswers(peerIP)},
	}
	if role == "primary" {
		steps = append(steps, clusterStep{Code: "peerIsSecondary", Manual: true, Args: []string{peerIP}})
	} else {
		steps = append(steps, clusterStep{Code: "peerIsPrimary", Manual: true, Args: []string{peerIP}})
	}
	// Both names on this machine is the failure the pair exists to prevent —
	// name it explicitly rather than leaving it implied by an unticked box.
	// İki adın da bu makinede olması, çiftin engellemek için var olduğu
	// arızadır — işaretsiz bir kutuyla ima etmek yerine adıyla söyle.
	if len(here) == 2 {
		steps = append([]clusterStep{{Code: "bothNamesHere", Args: []string{serverIP}}}, steps...)
	}
	return steps
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func (p *Panel) handleDNSCluster(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}

	switch r.Method {
	case http.MethodGet:
		v := dnsClusterView{
			Role:     p.setting(r.Context(), settingDNSRole),
			PeerIP:   p.setting(r.Context(), settingDNSPeerIP),
			PeerNS:   p.setting(r.Context(), settingDNSPeerNS),
			ServerIP: serverPrimaryIP(),
		}
		if v.Role == "" {
			v.Role = "standalone"
		}
		if v.PeerIP != "" {
			v.PeerReachable = dnsPortAnswers(v.PeerIP)
		}
		v.NS1, v.NS2 = p.serverNameservers(r.Context())
		v.Facts = verifyNameservers(r.Context(), []string{v.NS1, v.NS2}, serverPrimaryIP(), serverPrimaryIPv6())
		v.Steps = planSteps(v.Role, v.ServerIP, v.PeerIP, v.Facts)
		json.NewEncoder(w).Encode(v)

	case http.MethodPut:
		var req struct {
			Role   string `json:"role"`
			PeerIP string `json:"peer_ip"`
			PeerNS string `json:"peer_ns"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}
		req.Role = strings.TrimSpace(req.Role)
		req.PeerIP = strings.TrimSpace(req.PeerIP)
		req.PeerNS = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(req.PeerNS, ".")))

		switch req.Role {
		case "standalone":
			req.PeerIP, req.PeerNS = "", ""
		case "primary", "secondary":
			if net.ParseIP(req.PeerIP) == nil {
				writeClientError(w, http.StatusBadRequest, "enter the other server's IP address")
				return
			}
			if !validHostname.MatchString(req.PeerNS) {
				writeClientError(w, http.StatusBadRequest, "enter the other server's nameserver name, for example ns2.example.com")
				return
			}
			// Pointing a server at itself is not a cluster, and it would make
			// PowerDNS notify and transfer in a loop.
			// Bir sunucuyu kendine yöneltmek küme değildir; PowerDNS'i döngüde
			// bildirim ve aktarım yapmaya sokar.
			if req.PeerIP == serverPrimaryIP() {
				writeClientError(w, http.StatusBadRequest, "the other server cannot be this server")
				return
			}
		default:
			writeClientError(w, http.StatusBadRequest, "role must be standalone, primary or secondary")
			return
		}

		var resp struct {
			Applied bool   `json:"applied"`
			Detail  string `json:"detail,omitempty"`
			Error   string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigureDNSCluster", &struct {
			Role   string `json:"role"`
			PeerIP string `json:"peer_ip"`
			PeerNS string `json:"peer_ns"`
		}{req.Role, req.PeerIP, req.PeerNS}, &resp); err != nil {
			writeAgentError(w, err, "DNS cluster")
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}

		for k, v := range map[string]string{
			settingDNSRole:   req.Role,
			settingDNSPeerIP: req.PeerIP,
			settingDNSPeerNS: req.PeerNS,
		} {
			if err := p.setSetting(r.Context(), k, v); err != nil {
				writeServerError(w, err)
				return
			}
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

// dnsZoneType is the type a NEW zone gets on this server: MASTER when this
// machine is the primary of a cluster (so the secondary is notified the moment
// the zone appears), NATIVE otherwise. A secondary never creates zones itself —
// they arrive from the primary — so its answer is irrelevant here.
// dnsZoneType, bu sunucuda YENİ bir zone'un alacağı tiptir: bu makine bir
// kümenin birincili ise MASTER (böylece zone belirir belirmez ikincile haber
// verilir), aksi hâlde NATIVE. İkincil kendisi hiç zone oluşturmaz — onlar
// birincilden gelir — bu yüzden onun cevabı burada önemsizdir.
func (p *Panel) dnsZoneType(ctx context.Context) string {
	if p.setting(ctx, settingDNSRole) == "primary" {
		return "MASTER"
	}
	return "NATIVE"
}
