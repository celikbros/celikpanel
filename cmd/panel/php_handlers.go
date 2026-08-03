package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

// handlePHPPools handles GET and POST requests for PHP pools
func (p *Panel) handlePHPPools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		rejectRouteMethod(w, []string{http.MethodGet})
		return
	}

	version := r.URL.Query().Get("version")
	if version == "" {
		http.Error(w, "Version parameter is required", http.StatusBadRequest)
		return
	}

	var pools []transport.PHPPool
	req := transport.PHPVersionRequest{Version: version}

	err := p.callAgent("Agent.GetPHPPools", req, &pools)
	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(pools)
}

// handlePHPExtensions handles GET and POST requests for PHP extensions
func (p *Panel) handlePHPExtensions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost})
		return
	}

	if r.Method == "GET" {
		version := r.URL.Query().Get("version")
		if version == "" {
			http.Error(w, "Version parameter is required", http.StatusBadRequest)
			return
		}

		var extensions []transport.PHPExtension
		req := transport.PHPVersionRequest{Version: version}

		err := p.callAgent("Agent.GetPHPExtensions", req, &extensions)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(extensions)
		return
	}

	if r.Method == "POST" {
		var req transport.PHPExtensionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// Validate required fields
		if req.Version == "" || req.Extension == "" {
			http.Error(w, "Version and extension are required", http.StatusBadRequest)
			return
		}

		var resp transport.Empty
		err := p.callAgent("Agent.TogglePHPExtension", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Extension updated"})
	}
}

// handlePHPConfig handles GET and POST requests for php.ini
func (p *Panel) handlePHPConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost})
		return
	}

	version := r.URL.Query().Get("version")
	if version == "" {
		http.Error(w, "Version parameter is required", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		var config transport.PHPConfig
		req := transport.PHPVersionRequest{Version: version}

		err := p.callAgent("Agent.GetPHPConfiguration", req, &config)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(config)
		return
	}

	if r.Method == "POST" {
		var req transport.PHPConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		var resp transport.Empty
		err := p.callAgent("Agent.UpdatePHPConfiguration", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Configuration saved"})
	}
}
