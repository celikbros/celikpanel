package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ServiceStatusResponse represents the status of a service
type ServiceStatusResponse struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
	PID    string `json:"pid,omitempty"`
}

// handleServiceStatus handles GET requests for service status
func (p *Panel) handleServiceStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Service name is required", http.StatusBadRequest)
		return
	}

	// Get all services from agent to find the real status
	var services []core.Service
	err := p.agentClient.Call("Agent.GetServices", &transport.Empty{}, &services)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Find the service by name
	var found bool
	var isActive bool
	var pid string

	log.Printf("DEBUG: Checking status for service '%s'", name)

	for _, svc := range services {
		// Log detailed service info for debugging
		if strings.Contains(svc.Name, "mariadb") || strings.Contains(svc.Name, "mysql") {
			log.Printf("DEBUG: Found candidate service: Name='%s', Status='%s'", svc.Name, svc.Status)
		}

		// Flexible matching: "mariadb" should match "mariadb", "mariadb.service", or even "mysql" if it's an alias?
		// For now, let's just handle the .service suffix and direct match.
		if svc.Name == name || svc.Name == name+".service" {
			found = true
			log.Printf("DEBUG: Match found for '%s' -> '%s'", name, svc.Name)

			// Check if status contains "running" or "active" - this is the reliable indicator
			// Avoid false positives from "inactive" containing "active"
			statusLower := strings.ToLower(svc.Status)
			// Service is active if it contains "active" but NOT "inactive" or "dead"
			// This handles both "running" and "active (exited)" states
			isActive = (strings.Contains(statusLower, "running") || strings.Contains(statusLower, "active")) &&
				!strings.Contains(statusLower, "inactive") &&
				!strings.Contains(statusLower, "dead")

			log.Printf("DEBUG: Status check: Raw='%s', Active=%v", svc.Status, isActive)

			// Extract PID from status if available
			if isActive {
				pid = "active" // Real PID extraction would need parsing systemctl output
			}
			break
		}
	}

	if !found {
		log.Printf("DEBUG: Service '%s' not found", name)
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	response := ServiceStatusResponse{
		Name:   name,
		Active: isActive,
		PID:    pid,
	}

	json.NewEncoder(w).Encode(response)
}
