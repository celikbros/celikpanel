package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
)

// DomainPHPSettingsResponse represents PHP settings for a domain
type DomainPHPSettingsResponse struct {
	DomainID    int    `json:"domain_id"`
	DomainName  string `json:"domain_name"`
	PHPVersion  string `json:"php_version"`
	PoolName    string `json:"pool_name"`
	PoolConfig  *core.PHPPoolConfig `json:"pool_config,omitempty"`
}

// UpdateDomainPHPRequest represents a request to update domain PHP settings
type UpdateDomainPHPRequest struct {
	PHPVersion string `json:"php_version"`
}

// handleDomainPHPSettings handles GET/POST for domain PHP settings
func (p *Panel) handleDomainPHPSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Extract domain ID from URL
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/php")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get domain
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domain, err := domainRepo.GetByID(context.Background(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Get site info
	var siteID int
	var phpVersion string
	var phpFPMSocket *string
	query := `SELECT id, php_version, php_fpm_socket FROM sites WHERE domain_id = ? LIMIT 1`
	err = p.db.GetDB().QueryRowContext(context.Background(), query, domainID).Scan(&siteID, &phpVersion, &phpFPMSocket)
	if err != nil {
		http.Error(w, "Site not found for domain", http.StatusNotFound)
		return
	}

	// Determine pool name from socket or site ID
	poolName := fmt.Sprintf("site%d", siteID)
	if phpFPMSocket != nil && *phpFPMSocket != "" {
		// Extract pool name from socket path
		// e.g., /var/run/php/php8.3-fpm-site12.sock -> site12
		parts := strings.Split(*phpFPMSocket, "-")
		if len(parts) >= 2 {
			poolName = strings.TrimSuffix(parts[len(parts)-1], ".sock")
		}
	}

	if r.Method == "GET" {
		// Get pool config from agent
		var poolConfig core.PHPPoolConfig
		req := struct {
			Version  string `json:"version"`
			PoolName string `json:"pool_name"`
		}{
			Version:  phpVersion,
			PoolName: poolName,
		}

		err = p.agentClient.Call("Agent.GetPHPPoolConfig", req, &poolConfig)
		if err != nil {
			// Pool config not found - try to create a default one
			defaultConfig := core.PHPPoolConfig{
				Name:             poolName,
				User:             domain.Name,
				Group:            domain.Name,
				Listen:           fmt.Sprintf("/var/run/php/php%s-fpm-%s.sock", phpVersion, poolName),
				ListenOwner:      "www-data",
				ListenGroup:      "www-data",
				ListenMode:       "0660",
				PM:               "dynamic",
				PMMaxChildren:    5,
				PMStartServers:   2,
				PMMinSpareServers: 1,
				PMMaxSpareServers: 3,
				PMMaxRequests:    500,
			}

			// Try to create the pool
			createReq := core.PHPPoolConfigRequest{
				Version:    phpVersion,
				PoolConfig: defaultConfig,
			}
			var createResp struct{}
			createErr := p.agentClient.Call("Agent.UpdatePHPPoolConfig", createReq, &createResp)
			if createErr != nil {
				// Still failed, return basic info without pool config
				json.NewEncoder(w).Encode(DomainPHPSettingsResponse{
					DomainID:   domainID,
					DomainName: domain.Name,
					PHPVersion: phpVersion,
					PoolName:   poolName,
				})
				return
			}

			// Pool created, return with config
			json.NewEncoder(w).Encode(DomainPHPSettingsResponse{
				DomainID:   domainID,
				DomainName: domain.Name,
				PHPVersion: phpVersion,
				PoolName:   poolName,
				PoolConfig: &defaultConfig,
			})
			return
		}

		json.NewEncoder(w).Encode(DomainPHPSettingsResponse{
			DomainID:   domainID,
			DomainName: domain.Name,
			PHPVersion: phpVersion,
			PoolName:   poolName,
			PoolConfig: &poolConfig,
		})
		return
	}

	if r.Method == "POST" {
		var req UpdateDomainPHPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// Validate PHP version
		if req.PHPVersion == "" {
			http.Error(w, "PHP version is required", http.StatusBadRequest)
			return
		}

		// Only migrate if version is actually changing
		if req.PHPVersion != phpVersion {
			// Migrate pool from old version to new version
			migrateReq := struct {
				OldVersion string `json:"old_version"`
				NewVersion string `json:"new_version"`
				PoolName   string `json:"pool_name"`
			}{
				OldVersion: phpVersion,
				NewVersion: req.PHPVersion,
				PoolName:   poolName,
			}

			var migrateResp struct{}
			err = p.agentClient.Call("Agent.MigratePHPPool", migrateReq, &migrateResp)
			if err != nil {
				writeServerError(w, err)
				return
			}

			// Update php_fpm_socket with new version path
			newSocket := fmt.Sprintf("/var/run/php/php%s-fpm-%s.sock", req.PHPVersion, poolName)
			updateQuery := `UPDATE sites SET php_version = ?, php_fpm_socket = ?, updated_at = datetime('now') WHERE domain_id = ?`
			_, err = p.db.GetDB().ExecContext(context.Background(), updateQuery, req.PHPVersion, newSocket, domainID)
		} else {
			// Just update version in DB (no migration needed)
			updateQuery := `UPDATE sites SET php_version = ?, updated_at = datetime('now') WHERE domain_id = ?`
			_, err = p.db.GetDB().ExecContext(context.Background(), updateQuery, req.PHPVersion, domainID)
		}

		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "PHP version updated",
		})
	}
}

// handleDomainPHPPool handles GET/POST for domain-specific pool configuration
func (p *Panel) handleDomainPHPPool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Extract domain ID from URL
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/php/pool")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get site info
	var phpVersion string
	var siteID int
	query := `SELECT id, php_version FROM sites WHERE domain_id = ? LIMIT 1`
	err = p.db.GetDB().QueryRowContext(context.Background(), query, domainID).Scan(&siteID, &phpVersion)
	if err != nil {
		http.Error(w, "Site not found for domain", http.StatusNotFound)
		return
	}

	poolName := fmt.Sprintf("site%d", siteID)

	if r.Method == "GET" {
		var poolConfig core.PHPPoolConfig
		req := struct {
			Version  string `json:"version"`
			PoolName string `json:"pool_name"`
		}{
			Version:  phpVersion,
			PoolName: poolName,
		}

		err = p.agentClient.Call("Agent.GetPHPPoolConfig", req, &poolConfig)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(poolConfig)
		return
	}

	if r.Method == "POST" {
		var req core.PHPPoolConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// Ensure version and pool name match the domain's site
		req.Version = phpVersion
		req.PoolConfig.Name = poolName

		var resp struct{}
		err = p.agentClient.Call("Agent.UpdatePHPPoolConfig", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "Pool configuration updated",
		})
	}
}

// extractDomainID extracts domain ID from URL path
// e.g., /api/v1/domains/123/php -> 123
func extractDomainID(path, prefix, suffix string) (int, error) {
	// Remove prefix and suffix
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimSuffix(path, suffix)
	
	// Extract ID
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid path")
	}
	
	return strconv.Atoi(parts[0])
}
