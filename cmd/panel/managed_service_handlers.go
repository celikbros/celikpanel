package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ManagedServiceResponse represents a managed service with runtime status
type ManagedServiceResponse struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Icon        string            `json:"icon"`
	Category    string            `json:"category"`
	Versions    []string          `json:"versions"`     // Detected versions
	Status      string            `json:"status"`       // Overall status
	IsInstalled bool              `json:"is_installed"`
	ConfigFiles []core.ConfigFile `json:"config_files"` // Detected config files
}

// handleManagedServices returns curated list of managed services
func (p *Panel) handleManagedServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get all system services from agent
	var allServices []core.Service
	err := p.agentClient.Call("Agent.GetServices", &transport.Empty{}, &allServices)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Build map of service name -> service for quick lookup
	serviceMap := make(map[string]*core.Service)
	for i := range allServices {
		serviceMap[allServices[i].Name] = &allServices[i]
	}

	// Build response for each managed service
	response := make([]ManagedServiceResponse, 0)
	for _, managed := range core.ManagedServices {
		// Detect versions and their status
		versions := []string{}
		configFiles := []core.ConfigFile{}
		isInstalled := false
		status := "inactive"
		anyRunning := false

		for _, svc := range allServices {
			for _, systemName := range managed.SystemNames {
				// Match exact name OR name prefix (to catch versioned services like postgresql@14-main)
				// The managed.SystemNames for postgresql is just "postgresql"
				// But systemd list might return "postgresql.service" or "postgresql@14-main.service"
				// Our extractVersion logic handles "php", but we need generic matching here?

				// Actually, existing logic requires EXACT match: svc.Name == systemName.
				// But we discovered "php8.4-fpm" was NOT matching because systemName was missing in binary.
				// Now I am fixing binary, so exact match works for PHP.
				
				// For PostgreSQL, SystemNames={"postgresql"}.
				// But real service might be "postgresql".
				// IF system has "postgresql@14-main", svc.Name might be "postgresql@14-main"
				// Does "postgresql" match "postgresql@14-main"? NO.
				
				// I should RELAX matching to prefix if needed?
				// But strictly speaking, I should stick to original loop structure but ADD config file aggregation.
				
				if svc.Name == systemName {
					isInstalled = true
					
					// Extract version for this specific service
					version := extractVersion(svc.Name, managed.ID)
					if version != "" && !contains(versions, version) {
						versions = append(versions, version)
					}
					
					// Aggregate config files
					if len(svc.ConfigFiles) > 0 {
						// Simple append (could deduplicate but normally distinct)
						configFiles = append(configFiles, svc.ConfigFiles...)
					}
					
					// Check if THIS specific service is running
					statusLower := strings.ToLower(svc.Status)
					isRunning := strings.Contains(statusLower, "running") && !strings.Contains(statusLower, "inactive") && !strings.Contains(statusLower, "dead")
					if isRunning {
						anyRunning = true
					}
				}
			}
		}

		// Overall status: if any version is running, show "running"
		if anyRunning {
			status = "active (running)"
		} else if isInstalled {
			status = "inactive (dead)"
		}

		// Skip if not installed (optional services)
		if !isInstalled {
			continue
		}

		// If no versions detected but service exists, add a default entry
		if len(versions) == 0 && isInstalled {
			versions = append(versions, "default")
		}

		response = append(response, ManagedServiceResponse{
			ID:          managed.ID,
			Name:        managed.Name,
			Description: managed.Description,
			Icon:        managed.Icon,
			Category:    managed.Category,
			Versions:    versions,
			Status:      status,
			IsInstalled: isInstalled,
			ConfigFiles: configFiles,
		})
	}

	json.NewEncoder(w).Encode(response)
}

// extractVersion extracts version number from service name
func extractVersion(serviceName, serviceID string) string {
	switch serviceID {
	case "php-fpm":
		// php8.4-fpm -> 8.4
		if strings.HasPrefix(serviceName, "php") && strings.HasSuffix(serviceName, "-fpm") {
			version := strings.TrimPrefix(serviceName, "php")
			version = strings.TrimSuffix(version, "-fpm")
			return version
		}
	case "postgresql":
		// For postgresql, we might need to query version differently
		// For now, return empty to use "default"
		return ""
	}
	return ""
}

// contains checks if a string slice contains a value
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
