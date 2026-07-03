package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
)

// DomainResponse is the API response for domain listing
type DomainResponse struct {
	ID          int    `json:"id"`
	DomainName  string `json:"domain_name"`
	PHPVersion  string `json:"php_version"`
	SSLEnabled  bool   `json:"ssl_enabled"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

// handleDomains lists all domains
func (p *Panel) handleDomains(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get domains
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domains, err := domainRepo.List(context.Background())
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Build response with proper field names for frontend
	response := make([]DomainResponse, 0, len(domains))
	for _, domain := range domains {
		// Query site info from database directly
		var phpVersion string
		var sslType string
		
		query := `SELECT php_version, ssl_type FROM sites WHERE domain_id = ? LIMIT 1`
		err := p.db.GetDB().QueryRowContext(context.Background(), query, domain.ID).Scan(&phpVersion, &sslType)
		if err != nil {
			// Default values if site not found
			phpVersion = "8.3"
			sslType = "none"
		}

		response = append(response, DomainResponse{
			ID:         domain.ID,
			DomainName: domain.Name,
			PHPVersion: phpVersion,
			SSLEnabled: sslType != "none",
			Status:     domain.Status,
			CreatedAt:  domain.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	json.NewEncoder(w).Encode(response)
}

// handleCreateDomain creates a new domain with site
func (p *Panel) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req services.CreateSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Default to admin subscription if not specified
	if req.SubscriptionID == 0 {
		req.SubscriptionID = 1
	}

	// Default PHP version
	if req.PHPVersion == "" {
		req.PHPVersion = "8.3"
	}

	// Default SSL type
	if req.SSLType == "" {
		req.SSLType = "none"
	}

	// Default access method
	if req.AccessMethod == "" {
		req.AccessMethod = "sftp"
	}

	result, err := p.orchestrator.CreateSite(context.Background(), &req)
	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(result)
}

// handleDeleteDomain deletes a domain
func (p *Panel) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "DELETE" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from URL path
	idStr := r.URL.Path[len("/api/v1/domains/"):]
	domainID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get domain
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domain, err := domainRepo.GetByID(context.Background(), domainID)
	if err != nil {
		http.Error(w, "domain not found", http.StatusNotFound)
		return
	}

	// Delete domain (cascades to sites via foreign key)
	if err := domainRepo.Delete(context.Background(), domainID); err != nil {
		writeServerError(w, err)
		return
	}

	// TODO: Clean up nginx configs, PHP pools, etc. via Agent RPC
	// For now, just delete the database record
	
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"domain": domain.Name,
	})
}
