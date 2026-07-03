package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
)

// handleNginxGlobalConfig handles GET and POST requests for Nginx global config
func (p *Panel) handleNginxGlobalConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data - would parse /etc/nginx/nginx.conf
		config := core.NginxGlobalConfig{
			WorkerProcesses:     "auto",
			WorkerConnections:   "1024",
			KeepaliveTimeout:    "65",
			ClientMaxBodySize:   "64m",
			ServerTokens:        "off",
			Gzip:                "on",
		}
		json.NewEncoder(w).Encode(config)
		return
	}

	if r.Method == "POST" {
		var req core.NginxGlobalConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Call agent to update nginx.conf
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Nginx configuration saved"})
	}
}

// handleNginxSSLConfig handles GET and POST requests for Nginx SSL config
func (p *Panel) handleNginxSSLConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data
		config := core.NginxSSLConfig{
			SSLProtocols:           "TLSv1.2 TLSv1.3",
			SSLCiphers:             "ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384",
			SSLPreferServerCiphers: "off",
		}
		json.NewEncoder(w).Encode(config)
		return
	}

	if r.Method == "POST" {
		var req core.NginxSSLConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Call agent to update ssl settings
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "SSL configuration saved"})
	}
}

// handleNginxRateLimits handles GET requests for rate limits
func (p *Panel) handleNginxRateLimits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data
		limits := []core.NginxRateLimit{
			{Name: "limit_req_zone", Zone: "$binary_remote_addr", Size: "10m", Rate: "10r/s"},
			{Name: "limit_conn_zone", Zone: "$binary_remote_addr", Size: "10m", Rate: ""},
		}
		json.NewEncoder(w).Encode(limits)
		return
	}
}
