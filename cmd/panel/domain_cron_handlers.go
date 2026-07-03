package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// Cron Jobs API Handlers

func (p *Panel) handleDomainCronJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

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

	// Get domain name for username
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

	// Use www-data as default, could be domain-specific user
	username := "www-data"

	switch r.Method {
	case "GET":
		p.handleListCronJobs(w, username)
	case "POST":
		p.handleAddCronJob(w, r, username)
	case "PUT":
		p.handleUpdateCronJob(w, r, username)
	case "DELETE":
		p.handleDeleteCronJob(w, r, username)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListCronJobs(w http.ResponseWriter, username string) {
	var resp struct {
		Jobs []struct {
			ID       string `json:"id"`
			Schedule string `json:"schedule"`
			Command  string `json:"command"`
			Enabled  bool   `json:"enabled"`
			Comment  string `json:"comment"`
		} `json:"jobs"`
	}

	err := p.agentClient.Call("Agent.ListCronJobs", &struct{ Username string }{Username: username}, &resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (p *Panel) handleAddCronJob(w http.ResponseWriter, r *http.Request, username string) {
	var req struct {
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		Comment  string `json:"comment,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var success bool
	err := p.agentClient.Call("Agent.AddCronJob", &struct {
		Username string
		Schedule string
		Command  string
		Comment  string
	}{
		Username: username,
		Schedule: req.Schedule,
		Command:  req.Command,
		Comment:  req.Comment,
	}, &success)

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": success})
}

func (p *Panel) handleUpdateCronJob(w http.ResponseWriter, r *http.Request, username string) {
	var req struct {
		ID       string `json:"id"`
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		Enabled  bool   `json:"enabled"`
		Comment  string `json:"comment,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var success bool
	err := p.agentClient.Call("Agent.UpdateCronJob", &struct {
		Username string
		ID       string
		Schedule string
		Command  string
		Enabled  bool
		Comment  string
	}{
		Username: username,
		ID:       req.ID,
		Schedule: req.Schedule,
		Command:  req.Command,
		Enabled:  req.Enabled,
		Comment:  req.Comment,
	}, &success)

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": success})
}

func (p *Panel) handleDeleteCronJob(w http.ResponseWriter, r *http.Request, username string) {
	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	var success bool
	err := p.agentClient.Call("Agent.DeleteCronJob", &struct {
		Username string
		ID       string
	}{
		Username: username,
		ID:       jobID,
	}, &success)

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": success})
}
