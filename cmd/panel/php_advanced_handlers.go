package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// handlePHPPoolConfig handles GET/POST/DELETE for pool configuration
func (p *Panel) handlePHPPoolConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	version := r.URL.Query().Get("version")
	poolName := r.URL.Query().Get("pool")

	if version == "" {
		http.Error(w, "Version parameter required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		if poolName == "" {
			http.Error(w, "Pool name required", http.StatusBadRequest)
			return
		}

		var config core.PHPPoolConfig
		req := transport.GetPHPPoolConfigRequest{Version: version, PoolName: poolName}

		err := p.callAgent("Agent.GetPHPPoolConfig", req, &config)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(config)

	case "POST":
		var req core.PHPPoolConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		var resp transport.Empty
		err := p.callAgent("Agent.UpdatePHPPoolConfig", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Pool updated"})

	case "DELETE":
		if poolName == "" {
			http.Error(w, "Pool name required", http.StatusBadRequest)
			return
		}

		req := core.DeletePHPPoolRequest{
			Version:  version,
			PoolName: poolName,
		}

		var resp transport.Empty
		err := p.callAgent("Agent.DeletePHPPool", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Pool deleted"})

	default:
		rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost, http.MethodDelete})
	}
}

// handlePHPExtendedConfig handles GET/POST for extended PHP configuration
func (p *Panel) handlePHPExtendedConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost})
		return
	}

	if r.Method == "GET" {
		version := r.URL.Query().Get("version")
		if version == "" {
			http.Error(w, "Version parameter required", http.StatusBadRequest)
			return
		}

		var config core.ExtendedPHPConfig
		req := core.PHPVersionRequest{Version: version}

		err := p.callAgent("Agent.GetExtendedPHPConfig", req, &config)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(config)
		return
	}

	if r.Method == "POST" {
		var req core.ExtendedPHPConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// Validate version is provided
		if req.Version == "" {
			http.Error(w, "Version required in request body", http.StatusBadRequest)
			return
		}

		var resp transport.Empty
		err := p.callAgent("Agent.UpdateExtendedPHPConfig", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Configuration updated"})
	}
}
