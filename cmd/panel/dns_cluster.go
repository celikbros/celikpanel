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
