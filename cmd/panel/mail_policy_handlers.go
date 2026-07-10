package main

import (
	"encoding/json"
	"net/http"
)

// Admin-only server mail policy: message size and inbound DNSBL protection.
// Yalnız yönetici sunucu posta politikası: mesaj boyutu ve gelen DNSBL koruması.
func (p *Panel) handleMailPolicy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	switch r.Method {
	case http.MethodGet:
		var resp struct {
			Policy struct {
				MessageSizeMB     int      `json:"message_size_mb"`
				DNSBLZones        []string `json:"dnsbl_zones"`
				OutboundRateLimit int      `json:"outbound_rate_limit"`
			} `json:"policy"`
			Error string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.GetMailPolicy", &struct{}{}, &resp); err != nil {
			writeAgentError(w, err, "mail policy")
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		json.NewEncoder(w).Encode(resp.Policy)

	case http.MethodPut:
		var req struct {
			MessageSizeMB     int      `json:"message_size_mb"`
			DNSBLZones        []string `json:"dnsbl_zones"`
			OutboundRateLimit int      `json:"outbound_rate_limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		var resp struct {
			Policy struct {
				MessageSizeMB     int      `json:"message_size_mb"`
				DNSBLZones        []string `json:"dnsbl_zones"`
				OutboundRateLimit int      `json:"outbound_rate_limit"`
			} `json:"policy"`
			Error string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.SetMailPolicy", &req, &resp); err != nil {
			writeAgentError(w, err, "mail policy")
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		p.audit(r, "mail.policy", "", 0)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "policy": resp.Policy})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
