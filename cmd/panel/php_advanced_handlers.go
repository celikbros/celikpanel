package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
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
		req := struct {
			Version  string `json:"version"`
			PoolName string `json:"pool_name"`
		}{Version: version, PoolName: poolName}
		
		err := p.agentClient.Call("Agent.GetPHPPoolConfig", req, &config)
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
		
		var resp struct{}
		err := p.agentClient.Call("Agent.UpdatePHPPoolConfig", req, &resp)
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
		
		var resp struct{}
		err := p.agentClient.Call("Agent.DeletePHPPool", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}
		
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Pool deleted"})
	}
}

// handlePHPExtendedConfig handles GET/POST for extended PHP configuration
func (p *Panel) handlePHPExtendedConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	if r.Method == "GET" {
		version := r.URL.Query().Get("version")
		if version == "" {
			http.Error(w, "Version parameter required", http.StatusBadRequest)
			return
		}
		
		var config core.ExtendedPHPConfig
		req := core.PHPVersionRequest{Version: version}
		
		err := p.agentClient.Call("Agent.GetExtendedPHPConfig", req, &config)
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
		
		var resp struct{}
		err := p.agentClient.Call("Agent.UpdateExtendedPHPConfig", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}
		
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Configuration updated"})
	}
}

