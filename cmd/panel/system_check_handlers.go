package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

// handleSystemCheck handles GET for system service checks
func (p *Panel) handleSystemCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Call agent to check installed services
	var agentResp transport.CheckInstalledServicesResponse
	err := p.callAgent("Agent.CheckInstalledServices", &transport.Empty{}, &agentResp)
	if err != nil {
		http.Error(w, "Failed to check services", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(agentResp)
}
