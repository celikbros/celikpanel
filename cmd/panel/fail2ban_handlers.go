package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// handleFail2banJails serves the real jail list (GET) and toggles a jail at
// runtime (POST), both via the agent — no fabricated jails.
// handleFail2banJails, gerçek jail listesini (GET) sunar ve bir jail'i
// çalışma zamanında açar/kapatır (POST); ikisi de agent'tan — uydurma jail yok.
func (p *Panel) handleFail2banJails(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		var req core.Fail2banJailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}
		var ok bool
		if err := p.agentClient.Call("Agent.Fail2banToggleJail", &req, &ok); err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": ok})
		return
	}

	var result core.Fail2banStatusResult
	if err := p.agentClient.Call("Agent.Fail2banStatus", &transport.Empty{}, &result); err != nil {
		writeServerError(w, err)
		return
	}
	if result.Jails == nil {
		result.Jails = []core.Fail2banJail{}
	}
	json.NewEncoder(w).Encode(result.Jails)
}

// handleFail2banBannedIPs serves the real banned IPs (GET) and unbans one
// (POST) via the agent.
// handleFail2banBannedIPs, gerçek banlı IP'leri (GET) sunar ve birini yasağını
// kaldırır (POST); agent üzerinden.
func (p *Panel) handleFail2banBannedIPs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		var req core.Fail2banUnbanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}
		var ok bool
		if err := p.agentClient.Call("Agent.Fail2banUnban", &req, &ok); err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": ok})
		return
	}

	var result core.Fail2banStatusResult
	if err := p.agentClient.Call("Agent.Fail2banStatus", &transport.Empty{}, &result); err != nil {
		writeServerError(w, err)
		return
	}
	if result.Banned == nil {
		result.Banned = []core.Fail2banBannedIP{}
	}
	json.NewEncoder(w).Encode(result.Banned)
}

// handleFail2banConfig serves the real global defaults parsed from the
// fail2ban config file.
// handleFail2banConfig, fail2ban config dosyasından ayrıştırılan gerçek
// global varsayılanları sunar.
func (p *Panel) handleFail2banConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		// Editing the fail2ban config is not implemented yet; be honest
		// rather than pretend it saved.
		// fail2ban config'ini düzenleme henüz uygulanmadı; kaydettik gibi
		// yapmak yerine dürüst ol.
		writeClientError(w, http.StatusNotImplemented, "editing fail2ban config is not supported yet")
		return
	}

	var config core.Fail2banConfig
	if err := p.agentClient.Call("Agent.Fail2banConfig", &transport.Empty{}, &config); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(config)
}
