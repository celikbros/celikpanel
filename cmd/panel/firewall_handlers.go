package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Firewall, panel side. The panel decides policy — which ports should be open
// — and the agent enforces it in nftables. The desired set is derived, never
// hand-typed: the panel's own port plus the firewall ports of every installed
// service. SSH is added by the agent from live listeners, so it is never
// possible to lock the box. Whenever a service is installed or removed, the
// firewall re-syncs, so the open ports always track the running services.
//
// Güvenlik duvarı, panel tarafı. Politikayı panel belirler (hangi portlar
// açık olmalı), agent nftables'ta uygular. İstenen küme türetilir, elle
// yazılmaz: panelin kendi portu + kurulu her servisin güvenlik-duvarı
// portları. SSH'ı agent canlı dinleyicilerden ekler; kutuyu kilitlemek asla
// mümkün değil. Bir servis kurulunca/kaldırılınca güvenlik duvarı yeniden
// senkronlanır; açık portlar daima koşan servisleri izler.

// panelPort returns the TCP port the panel itself listens on, so the firewall
// never closes the door it is administered through.
// panelPort, panelin dinlediği TCP portunu döndürür; güvenlik duvarı,
// yönetildiği kapıyı asla kapatmaz.
func panelPort() int {
	addr := listenAddr() // ":2083" or "1.2.3.4:2083"
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		if n, err := strconv.Atoi(addr[i+1:]); err == nil {
			return n
		}
	}
	return 1983
}

// desiredFirewallPorts computes the ports that must be open: the panel port
// plus every installed service's declared ports. Local-only services (empty
// FirewallPorts) contribute nothing — exactly the point.
// desiredFirewallPorts, açık olması gereken portları hesaplar: panel portu +
// kurulu her servisin bildirdiği portlar. Yalnız-yerel servisler katkı vermez.
func (p *Panel) desiredFirewallPorts() (tcp []int, udp []int) {
	tcp = append(tcp, panelPort())

	var installed []string
	_ = p.agentClient.Call("Agent.InstalledServiceIDs", &transport.Empty{}, &installed)
	installedSet := map[string]bool{}
	for _, id := range installed {
		installedSet[id] = true
	}
	for i := range core.ManagedServices {
		m := &core.ManagedServices[i]
		if !installedSet[m.ID] {
			continue
		}
		for _, fp := range m.FirewallPorts {
			if fp.Proto == "udp" {
				udp = append(udp, fp.Port)
			} else {
				tcp = append(tcp, fp.Port)
			}
		}
	}
	return tcp, udp
}

// syncFirewall re-applies the firewall to match the installed service set,
// but only when the firewall is already enabled (never turns it on by
// surprise). Called after install/uninstall.
// syncFirewall, güvenlik duvarını kurulu servis kümesiyle eşleşecek şekilde
// yeniden uygular — yalnız zaten etkinken (asla sürpriz açmaz). Kurulum/
// kaldırma sonrası çağrılır.
func (p *Panel) syncFirewall() {
	var st FirewallStatusResp
	if err := p.agentClient.Call("Agent.FirewallStatus", &struct{}{}, &st); err != nil || !st.Enabled {
		return
	}
	tcp, udp := p.desiredFirewallPorts()
	var out FirewallStatusResp
	_ = p.agentClient.Call("Agent.ApplyFirewall", &applyFirewallReq{Enabled: true, TCPPorts: tcp, UDPPorts: udp}, &out)
}

type applyFirewallReq struct {
	Enabled  bool  `json:"enabled"`
	TCPPorts []int `json:"tcp_ports"`
	UDPPorts []int `json:"udp_ports"`
}

// Must mirror the agent's FirewallStatusResponse field-for-field: net/rpc
// (gob) transfers by exported field NAME, so a field missing here is silently
// dropped in transit. EngineAvailable was added to the agent but not here —
// that is why the panel never saw "nftables not installed" and the UI kept
// offering "Turn on" against a missing engine.
// Agent'ın FirewallStatusResponse'unu alan alan yansıtmalı: net/rpc (gob)
// dışa açık alan ADIYLA taşır; burada eksik bir alan sessizce yolda düşer.
// EngineAvailable agent'a eklenmiş ama buraya eklenmemişti — panelin
// 'nftables kurulu değil'i asla görmemesinin ve motor yokken 'Turn on'
// sunmaya devam etmesinin nedeni buydu.
type FirewallStatusResp struct {
	Enabled         bool   `json:"enabled"`
	EngineAvailable bool   `json:"engine_available"`
	TCPPorts        []int  `json:"tcp_ports"`
	UDPPorts        []int  `json:"udp_ports"`
	SSHPorts        []int  `json:"ssh_ports"`
	Error           string `json:"error,omitempty"`
}

// handleFirewall: GET status, POST {enabled} to turn on/off (admin-only).
// Turning on computes the desired ports; turning off removes the table.
// handleFirewall: GET durum, POST {enabled} aç/kapat (yalnız admin).
func (p *Panel) handleFirewall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	switch r.Method {
	case http.MethodGet:
		var st FirewallStatusResp
		if err := p.agentClient.Call("Agent.FirewallStatus", &struct{}{}, &st); err != nil {
			writeAgentError(w, err, "firewall")
			return
		}
		json.NewEncoder(w).Encode(st)

	case http.MethodPost:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		call := applyFirewallReq{Enabled: req.Enabled}
		if req.Enabled {
			call.TCPPorts, call.UDPPorts = p.desiredFirewallPorts()
		}
		var st FirewallStatusResp
		if err := p.agentClient.Call("Agent.ApplyFirewall", &call, &st); err != nil {
			writeAgentError(w, err, "firewall")
			return
		}
		if st.Error != "" {
			writeClientError(w, http.StatusConflict, st.Error)
			return
		}
		action := "firewall.off"
		if req.Enabled {
			action = "firewall.on"
		}
		p.audit(r, action, "", 0)
		json.NewEncoder(w).Encode(st)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
