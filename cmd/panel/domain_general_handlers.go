package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// Domain general settings structures
type DomainGeneralSettings struct {
	DomainID      int      `json:"domain_id"`
	DomainName    string   `json:"domain_name"`
	DocumentRoot  string   `json:"document_root"`
	WebServer     string   `json:"web_server"`
	RedirectWWW   bool     `json:"redirect_www"`
	RedirectHTTPS bool     `json:"redirect_https"`
	Aliases       []string `json:"aliases"`
}

type UpdateGeneralSettingsRequest struct {
	DocumentRoot  string `json:"document_root"`
	WebServer     string `json:"web_server"`
	RedirectWWW   bool   `json:"redirect_www"`
	RedirectHTTPS bool   `json:"redirect_https"`
}

type AddAliasRequest struct {
	Alias string `json:"alias"`
}

// GET /api/v1/domains/:id/general
func (p *Panel) handleDomainGeneralSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		p.handleGetGeneralSettings(w, r, domainID)
	} else {
		p.handleUpdateGeneralSettings(w, r, domainID)
	}
}

func (p *Panel) handleGetGeneralSettings(w http.ResponseWriter, r *http.Request, domainID int) {
	ctx := context.Background()
	pool := p.db.GetDB()

	// Get domain info using repository
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Get site info
	var site struct {
		DocumentRoot  string
		WebServer     string
		RedirectWWW   bool
		RedirectHTTPS bool
	}
	err = pool.QueryRowContext(ctx, `
		SELECT document_root, 
		       COALESCE(web_server, 'nginx') as web_server,
		       COALESCE(redirect_www, false) as redirect_www,
		       COALESCE(redirect_https, false) as redirect_https
		FROM sites WHERE domain_id = ?
	`, domainID).Scan(&site.DocumentRoot, &site.WebServer, &site.RedirectWWW, &site.RedirectHTTPS)

	if err != nil {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}

	// Get aliases
	rows, err := pool.QueryContext(ctx, "SELECT alias FROM domain_aliases WHERE domain_id = ?", domainID)
	if err != nil {
		http.Error(w, "Failed to load aliases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var aliases []string
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			continue
		}
		aliases = append(aliases, alias)
	}

	settings := DomainGeneralSettings{
		DomainID:      domain.ID,
		DomainName:    domain.Name,
		DocumentRoot:  site.DocumentRoot,
		WebServer:     site.WebServer,
		RedirectWWW:   site.RedirectWWW,
		RedirectHTTPS: site.RedirectHTTPS,
		Aliases:       aliases,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

func (p *Panel) handleUpdateGeneralSettings(w http.ResponseWriter, r *http.Request, domainID int) {
	var req UpdateGeneralSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate web server
	if req.WebServer != "nginx" && req.WebServer != "apache" {
		http.Error(w, "Invalid web server", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	pool := p.db.GetDB()

	// Update site settings
	_, err := pool.ExecContext(ctx, `
		UPDATE sites 
		SET document_root = ?,
		    web_server = ?,
		    redirect_www = ?,
		    redirect_https = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?
	`, req.DocumentRoot, req.WebServer, req.RedirectWWW, req.RedirectHTTPS, domainID)

	if err != nil {
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	// TODO: Regenerate nginx/apache config

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// POST /api/v1/domains/:id/aliases
func (p *Panel) handleDomainAliases(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	if r.Method == "POST" {
		p.handleAddAlias(w, r, domainID)
	} else {
		p.handleDeleteAlias(w, r, domainID, pathParts)
	}
}

func (p *Panel) handleAddAlias(w http.ResponseWriter, r *http.Request, domainID int) {
	var req AddAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate alias
	if req.Alias == "" {
		http.Error(w, "Alias cannot be empty", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	pool := p.db.GetDB()

	// Insert alias
	_, err := pool.ExecContext(ctx, `
		INSERT INTO domain_aliases (domain_id, alias) 
		VALUES (?, ?)
	`, domainID, req.Alias)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			http.Error(w, "Alias already exists", http.StatusConflict)
		} else {
			http.Error(w, "Failed to add alias", http.StatusInternalServerError)
		}
		return
	}

	// TODO: Regenerate nginx config

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "alias": req.Alias})
}

func (p *Panel) handleDeleteAlias(w http.ResponseWriter, r *http.Request, domainID int, pathParts []string) {
	if len(pathParts) < 7 {
		http.Error(w, "Alias not specified", http.StatusBadRequest)
		return
	}

	alias := pathParts[6]

	ctx := context.Background()
	pool := p.db.GetDB()

	// Delete alias
	result, err := pool.ExecContext(ctx, `
		DELETE FROM domain_aliases 
		WHERE domain_id = ? AND alias = ?
	`, domainID, alias)

	if err != nil {
		http.Error(w, "Failed to delete alias", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, "Alias not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
