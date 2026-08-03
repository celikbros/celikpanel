package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
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
		var resp transport.MailPolicyResponse
		if err := p.callAgent("Agent.GetMailPolicy", &transport.Empty{}, &resp); err != nil {
			writeAgentError(w, err, "mail policy")
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		json.NewEncoder(w).Encode(resp.Policy)

	case http.MethodPut:
		var req transport.MailPolicy
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		var resp transport.MailPolicyResponse
		if err := p.callAgent("Agent.SetMailPolicy", &req, &resp); err != nil {
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
