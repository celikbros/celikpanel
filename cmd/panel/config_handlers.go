package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

// handleGetPHPConfig returns PHP configuration
func (p *Panel) handleGetPHPConfig(w http.ResponseWriter, r *http.Request) {
	phpVersion := r.URL.Query().Get("version")
	if phpVersion == "" {
		http.Error(w, "version parameter required", http.StatusBadRequest)
		return
	}

	var req transport.GetPHPConfigRequest
	req.PHPVersion = phpVersion

	var resp transport.GetPHPConfigResponse
	if err := p.agentClient.Call("Agent.GetPHPConfig", req, &resp); err != nil {
		writeServerError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleUpdatePHPConfig updates PHP configuration
func (p *Panel) handleUpdatePHPConfig(w http.ResponseWriter, r *http.Request) {
	var req transport.UpdatePHPConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
		return
	}

	var resp struct{}
	if err := p.agentClient.Call("Agent.UpdatePHPConfig", req, &resp); err != nil {
		writeServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleGetMySQLConfig returns MySQL configuration
func (p *Panel) handleGetMySQLConfig(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	var resp transport.GetMySQLConfigResponse

	if err := p.agentClient.Call("Agent.GetMySQLConfig", req, &resp); err != nil {
		writeServerError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleUpdateMySQLConfig updates MySQL configuration
func (p *Panel) handleUpdateMySQLConfig(w http.ResponseWriter, r *http.Request) {
	var req transport.UpdateMySQLConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
		return
	}

	var resp struct{}
	if err := p.agentClient.Call("Agent.UpdateMySQLConfig", req, &resp); err != nil {
		writeServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
