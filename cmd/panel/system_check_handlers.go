package main

import (
	"encoding/json"
	"net/http"
)

// handleSystemCheck handles GET for system service checks
func (p *Panel) handleSystemCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Call agent to check installed services
	var agentResp struct {
		Nginx      bool `json:"nginx"`
		Apache     bool `json:"apache"`
		MySQL      bool `json:"mysql"`
		PostgreSQL bool `json:"postgresql"`
		PHP        bool `json:"php"`
	}

	agentReq := struct{}{}

	err := p.agentClient.Call("Agent.CheckInstalledServices", agentReq, &agentResp)
	if err != nil {
		http.Error(w, "Failed to check services", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(agentResp)
}
