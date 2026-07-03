package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// Backup API Handlers

func (p *Panel) handleDomainBackups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract domain ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get domain name
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domains, err := domainRepo.List(context.Background())
	if err != nil {
		http.Error(w, "Failed to get domains", http.StatusInternalServerError)
		return
	}

	var domainName string
	for _, d := range domains {
		if d.ID == domainID {
			domainName = d.Name
			break
		}
	}

	if domainName == "" {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case "GET":
		p.handleListBackups(w, domainName)
	case "POST":
		p.handleCreateBackup(w, r, domainName)
	case "DELETE":
		p.handleDeleteBackup(w, r, domainName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListBackups(w http.ResponseWriter, domainName string) {
	var resp struct {
		Backups []struct {
			Name      string `json:"name"`
			Path      string `json:"path"`
			Size      int64  `json:"size"`
			Type      string `json:"type"`
			CreatedAt string `json:"created_at"`
		} `json:"backups"`
	}

	err := p.agentClient.Call("Agent.ListBackups", &struct{ DomainName string }{DomainName: domainName}, &resp)
	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleCreateBackup(w http.ResponseWriter, r *http.Request, domainName string) {
	var req struct {
		Type         string `json:"type"`
		DatabaseName string `json:"database_name,omitempty"`
		DatabaseType string `json:"database_type,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var resp struct {
		Success bool `json:"success"`
		Backup  struct {
			Name      string `json:"name"`
			Path      string `json:"path"`
			Size      int64  `json:"size"`
			Type      string `json:"type"`
			CreatedAt string `json:"created_at"`
		} `json:"backup,omitempty"`
		Error string `json:"error,omitempty"`
	}

	err := p.agentClient.Call("Agent.CreateBackup", &struct {
		DomainName   string
		Type         string
		DatabaseName string
		DatabaseType string
	}{
		DomainName:   domainName,
		Type:         req.Type,
		DatabaseName: req.DatabaseName,
		DatabaseType: req.DatabaseType,
	}, &resp)

	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleDeleteBackup(w http.ResponseWriter, r *http.Request, domainName string) {
	backupName := r.URL.Query().Get("name")
	if backupName == "" {
		http.Error(w, "Backup name required", http.StatusBadRequest)
		return
	}

	var success bool
	err := p.agentClient.Call("Agent.DeleteBackup", &struct {
		DomainName string
		BackupName string
	}{
		DomainName: domainName,
		BackupName: backupName,
	}, &success)

	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": success})
}

func (p *Panel) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get domain name
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domains, err := domainRepo.List(context.Background())
	if err != nil {
		http.Error(w, "Failed to get domains", http.StatusInternalServerError)
		return
	}

	var domainName string
	for _, d := range domains {
		if d.ID == domainID {
			domainName = d.Name
			break
		}
	}

	if domainName == "" {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	var req struct {
		BackupName string `json:"backup_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}

	err = p.agentClient.Call("Agent.RestoreBackup", &struct {
		DomainName string
		BackupName string
	}{
		DomainName: domainName,
		BackupName: req.BackupName,
	}, &resp)

	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(resp)
}
