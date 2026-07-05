package main

import (
	"encoding/json"
	"net/http"
)

// Node runtime management endpoints (admin-only via isAdminOnlyPath).
// Node runtime yönetim uçları (isAdminOnlyPath ile yalnızca admin).

func (p *Panel) handleNodeRuntimes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		var resp struct {
			Installed     []string `json:"installed"`
			SystemVersion string   `json:"system_version"`
		}
		if err := p.agentClient.Call("Agent.ListNodeVersions", &struct{}{}, &resp); err != nil {
			writeServerError(w, err)
			return
		}
		if resp.Installed == nil {
			resp.Installed = []string{}
		}
		json.NewEncoder(w).Encode(resp)

	case http.MethodPost:
		// Listing versions is open to any signed-in user (projects pick from
		// them); installing runtimes stays an administrator action.
		// Sürümleri listelemek oturumdaki herkese açıktır (projeler onlardan
		// seçer); runtime kurmak yönetici işidir.
		if c := currentCaller(r); c == nil || c.Role != roleAdmin {
			writeClientError(w, http.StatusForbidden, "administrator access required")
			return
		}
		var req struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		var resp struct {
			Installed bool   `json:"installed"`
			Error     string `json:"error,omitempty"`
		}
		// Downloads can take a while; the agent verifies the official
		// checksum before anything is unpacked.
		// İndirme sürebilir; agent açmadan önce resmi sağlamayı doğrular.
		if err := p.agentClient.Call("Agent.InstallNodeVersion", &struct {
			Version string `json:"version"`
		}{Version: req.Version}, &resp); err != nil {
			writeServerError(w, err)
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
