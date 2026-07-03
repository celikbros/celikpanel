package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
)

// handleFail2banJails handles GET and POST requests for jails
func (p *Panel) handleFail2banJails(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data - would call `fail2ban-client status`
		jails := []core.Fail2banJail{
			{Name: "sshd", Enabled: true, Active: true, Banned: 2},
			{Name: "nginx-http-auth", Enabled: true, Active: true, Banned: 0},
			{Name: "postfix", Enabled: true, Active: true, Banned: 5},
			{Name: "dovecot", Enabled: true, Active: true, Banned: 1},
			{Name: "recidive", Enabled: false, Active: false, Banned: 0},
		}
		json.NewEncoder(w).Encode(jails)
		return
	}

	if r.Method == "POST" {
		var req core.Fail2banJailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Call agent to enable/disable jail
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Jail updated"})
	}
}

// handleFail2banBannedIPs handles GET and POST (unban) requests
func (p *Panel) handleFail2banBannedIPs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data - would call `fail2ban-client status <jail>` for each jail
		ips := []core.Fail2banBannedIP{
			{IP: "192.168.1.50", Jail: "sshd", Time: "2023-10-27 10:00:00", Country: "Unknown"},
			{IP: "10.0.0.5", Jail: "postfix", Time: "2023-10-27 11:30:00", Country: "Unknown"},
		}
		json.NewEncoder(w).Encode(ips)
		return
	}

	if r.Method == "POST" {
		var req core.Fail2banUnbanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Call agent to unban IP: fail2ban-client set <jail> unbanip <ip>
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "IP unbanned"})
	}
}

// handleFail2banConfig handles GET and POST requests for global config
func (p *Panel) handleFail2banConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data - would parse /etc/fail2ban/jail.local
		config := core.Fail2banConfig{
			BanTime:  "10m",
			FindTime: "10m",
			MaxRetry: 5,
			IgnoreIP: []string{"127.0.0.1/8", "::1"},
		}
		json.NewEncoder(w).Encode(config)
		return
	}

	if r.Method == "POST" {
		var req core.Fail2banConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Call agent to update config
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Configuration saved"})
	}
}
