package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// Domain Logs Handlers

// LogsResponse represents log data
type LogsResponse struct {
	Success bool     `json:"success"`
	Lines   []string `json:"lines"`
	Total   int      `json:"total"`
	LogPath string   `json:"log_path"`
}

// handleDomainLogs handles GET for domain logs
func (p *Panel) handleDomainLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" && r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID and log type from URL
	// /api/v1/domains/:id/logs/:type
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/logs/")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get log type from path
	logType := ""
	if r.URL.Path[len(r.URL.Path)-6:] == "access" {
		logType = "access"
	} else if r.URL.Path[len(r.URL.Path)-5:] == "error" {
		logType = "error"
	} else if r.URL.Path[len(r.URL.Path)-3:] == "php" {
		logType = "php"
	} else {
		http.Error(w, "Invalid log type. Use: access, error, or php", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		p.handleGetLogs(w, r, domainID, logType)
	} else {
		p.handleClearLogs(w, r, domainID, logType)
	}
}

func (p *Panel) handleGetLogs(w http.ResponseWriter, r *http.Request, domainID int, logType string) {
	ctx := context.Background()
	pool := p.db.GetDB()

	// Get domain name
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Determine log path based on type
	var logPath string
	switch logType {
	case "access":
		logPath = filepath.Join("/var/log/nginx", domain.Name+"-access.log")
	case "error":
		logPath = filepath.Join("/var/log/nginx", domain.Name+"-error.log")
	case "php":
		logPath = filepath.Join("/var/log/php", domain.Name+"-error.log")
	default:
		http.Error(w, "Invalid log type", http.StatusBadRequest)
		return
	}

	// Get query parameters
	lines := 100 // default
	if linesParam := r.URL.Query().Get("lines"); linesParam != "" {
		fmt.Sscanf(linesParam, "%d", &lines)
	}

	filter := r.URL.Query().Get("filter")

	// Call agent to get logs
	var agentResp struct {
		Success bool     `json:"success"`
		Lines   []string `json:"lines"`
		Total   int      `json:"total"`
		Error   string   `json:"error"`
	}

	agentReq := struct {
		LogPath string `json:"log_path"`
		Lines   int    `json:"lines"`
		Filter  string `json:"filter"`
	}{
		LogPath: logPath,
		Lines:   lines,
		Filter:  filter,
	}

	var rpcMethod string
	switch logType {
	case "access":
		rpcMethod = "Agent.GetAccessLogs"
	case "error":
		rpcMethod = "Agent.GetErrorLogs"
	case "php":
		rpcMethod = "Agent.GetPHPLogs"
	}

	err = p.agentClient.Call(rpcMethod, agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		errorMsg := "Failed to retrieve logs"
		if agentResp.Error != "" {
			errorMsg = agentResp.Error
		}
		http.Error(w, errorMsg, http.StatusInternalServerError)
		return
	}

	response := LogsResponse{
		Success: true,
		Lines:   agentResp.Lines,
		Total:   agentResp.Total,
		LogPath: logPath,
	}

	json.NewEncoder(w).Encode(response)
}

func (p *Panel) handleClearLogs(w http.ResponseWriter, r *http.Request, domainID int, logType string) {
	ctx := context.Background()
	pool := p.db.GetDB()

	// Get domain name
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Determine log path
	var logPath string
	switch logType {
	case "access":
		logPath = filepath.Join("/var/log/nginx", domain.Name+"-access.log")
	case "error":
		logPath = filepath.Join("/var/log/nginx", domain.Name+"-error.log")
	case "php":
		logPath = filepath.Join("/var/log/php", domain.Name+"-error.log")
	default:
		http.Error(w, "Invalid log type", http.StatusBadRequest)
		return
	}

	// Call agent to clear logs
	var agentResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}

	agentReq := struct {
		LogPath string `json:"log_path"`
	}{
		LogPath: logPath,
	}

	err = p.agentClient.Call("Agent.ClearLogs", agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		errorMsg := "Failed to clear logs"
		if agentResp.Error != "" {
			errorMsg = agentResp.Error
		}
		http.Error(w, errorMsg, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
