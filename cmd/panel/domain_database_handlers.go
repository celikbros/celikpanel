package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// Domain Database Management Handlers

// DatabaseInfo represents a database associated with a domain
type DatabaseInfo struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"` // mysql or postgresql
	User         string `json:"user"`
	CreatedAt    string `json:"created_at"`
	Size         string `json:"size,omitempty"`
}

// CreateDatabaseRequest represents a request to create a database
type CreateDatabaseRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // mysql or postgresql
	Password string `json:"password"`
}

// handleDomainDatabases handles GET/POST for domain databases
func (p *Panel) handleDomainDatabases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract domain ID
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/databases")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		p.handleGetDomainDatabases(w, r, domainID)
	} else if r.Method == "POST" {
		p.handleCreateDatabase(w, r, domainID)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleGetDomainDatabases(w http.ResponseWriter, r *http.Request, domainID int) {
	ctx := context.Background()
	pool := p.db.GetDB()

	// Get domain info
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Get databases for this domain
	rows, err := pool.QueryContext(ctx, `
		SELECT id, name, type, db_user, created_at
		FROM databases
		WHERE domain_id = ?
		ORDER BY created_at DESC
	`, domainID)
	if err != nil {
		http.Error(w, "Failed to load databases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var databases []DatabaseInfo
	for rows.Next() {
		var db DatabaseInfo
		if err := rows.Scan(&db.ID, &db.Name, &db.Type, &db.User, &db.CreatedAt); err != nil {
			continue
		}
		databases = append(databases, db)
	}

	response := map[string]interface{}{
		"domain_id":   domain.ID,
		"domain_name": domain.Name,
		"databases":   databases,
	}

	json.NewEncoder(w).Encode(response)
}

func (p *Panel) handleCreateDatabase(w http.ResponseWriter, r *http.Request, domainID int) {
	var req CreateDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.Name == "" || req.Type == "" || req.Password == "" {
		http.Error(w, "Name, type, and password are required", http.StatusBadRequest)
		return
	}

	if req.Type != "mysql" && req.Type != "postgresql" {
		http.Error(w, "Type must be 'mysql' or 'postgresql'", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	pool := p.db.GetDB()

	// Get domain info
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Generate database name (prefix with domain for organization)
	dbName := fmt.Sprintf("%s_%s", sanitizeName(domain.Name), req.Name)
	dbUser := dbName + "_user"

	// Check if database already exists
	var exists bool
	err = pool.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM databases WHERE name = ?)", dbName).Scan(&exists)
	if err != nil {
		http.Error(w, "Failed to check database existence", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, "Database already exists", http.StatusConflict)
		return
	}

	// Create database via agent RPC
	var agentResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}

	agentReq := struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		User     string `json:"user"`
		Password string `json:"password"`
	}{
		Type:     req.Type,
		Name:     dbName,
		User:     dbUser,
		Password: req.Password,
	}

	rpcMethod := "Agent.CreateDatabase"
	err = p.agentClient.Call(rpcMethod, agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		writeAgentError(w, err, agentResp.Error)
		return
	}

	// Store in panel database
	_, err = pool.ExecContext(ctx, `
		INSERT INTO databases (domain_id, name, type, db_user, db_password_hash)
		VALUES (?, ?, ?, ?, ?)
	`, domainID, dbName, req.Type, dbUser, req.Password) // TODO: Hash password

	if err != nil {
		http.Error(w, "Failed to store database info", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "success",
		"name":     dbName,
		"user":     dbUser,
		"type":     req.Type,
	})
}

// handleDeleteDatabase handles DELETE for a specific database
func (p *Panel) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID and database ID from URL
	// /api/v1/domains/:domain_id/databases/:db_id
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/databases/")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Extract database ID from end of path
	pathParts := splitPath(r.URL.Path)
	if len(pathParts) < 7 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}
	dbID := pathParts[6]

	ctx := context.Background()
	pool := p.db.GetDB()

	// Get database info
	var dbName, dbType, dbUser string
	err = pool.QueryRowContext(ctx, `
		SELECT name, type, db_user
		FROM databases
		WHERE id = ? AND domain_id = ?
	`, dbID, domainID).Scan(&dbName, &dbType, &dbUser)

	if err != nil {
		http.Error(w, "Database not found", http.StatusNotFound)
		return
	}

	// Delete database via agent RPC
	var agentResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}

	agentReq := struct {
		Type string `json:"type"`
		Name string `json:"name"`
		User string `json:"user"`
	}{
		Type: dbType,
		Name: dbName,
		User: dbUser,
	}

	err = p.agentClient.Call("Agent.DeleteDatabase", agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		writeAgentError(w, err, agentResp.Error)
		return
	}

	// Remove from panel database
	_, err = pool.ExecContext(ctx, "DELETE FROM databases WHERE id = ?", dbID)
	if err != nil {
		http.Error(w, "Failed to remove database record", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// Helper function to sanitize database names
func sanitizeName(name string) string {
	// Remove dots and hyphens, replace with underscores
	result := ""
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			result += string(char)
		} else if char == '.' || char == '-' {
			result += "_"
		}
	}
	return result
}

// Helper function to split path
func splitPath(path string) []string {
	parts := []string{}
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}
