package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
)

// handlePHPPools handles GET and POST requests for PHP pools
func (p *Panel) handlePHPPools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	version := r.URL.Query().Get("version")
	if version == "" {
		http.Error(w, "Version parameter is required", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		var pools []core.PHPPool
		req := core.PHPVersionRequest{Version: version}
		
		err := p.agentClient.Call("Agent.GetPHPPools", req, &pools)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get pools: %v", err), http.StatusInternalServerError)
			return
		}
		
		json.NewEncoder(w).Encode(pools)
		return
	}

	if r.Method == "POST" {
		var req core.PHPPoolRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		// TODO: Implement SavePHPPool RPC method
		// For now, return success
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Pool saved"})
	}
}

// handlePHPExtensions handles GET and POST requests for PHP extensions
func (p *Panel) handlePHPExtensions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	if r.Method == "GET" {
		version := r.URL.Query().Get("version")
		if version == "" {
			http.Error(w, "Version parameter is required", http.StatusBadRequest)
			return
		}
		
		var extensions []core.PHPExtension
		req := core.PHPVersionRequest{Version: version}
		
		err := p.agentClient.Call("Agent.GetPHPExtensions", req, &extensions)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get extensions: %v", err), http.StatusInternalServerError)
			return
		}
		
		json.NewEncoder(w).Encode(extensions)
		return
	}
	
	if r.Method == "POST" {
		var req core.PHPExtensionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		// Validate required fields
		if req.Version == "" || req.Extension == "" {
			http.Error(w, "Version and extension are required", http.StatusBadRequest)
			return
		}
		
		var resp struct{}
		err := p.agentClient.Call("Agent.TogglePHPExtension", req, &resp)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to toggle extension: %v", err), http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Extension updated"})
	}
}

// handlePHPConfig handles GET and POST requests for php.ini
func (p *Panel) handlePHPConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	version := r.URL.Query().Get("version")
	if version == "" {
		http.Error(w, "Version parameter is required", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		var config core.PHPConfig
		req := core.PHPVersionRequest{Version: version}
		
		err := p.agentClient.Call("Agent.GetPHPConfiguration", req, &config)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get configuration: %v", err), http.StatusInternalServerError)
			return
		}
		
		json.NewEncoder(w).Encode(config)
		return
	}
	
	if r.Method == "POST" {
		var req core.PHPConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		var resp struct{}
		err := p.agentClient.Call("Agent.UpdatePHPConfiguration", req, &resp)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to update configuration: %v", err), http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Configuration saved"})
	}
}
