package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Domain Logs Handlers

// LogsResponse represents log data
type LogsResponse struct {
	Success    bool                           `json:"success"`
	Lines      []string                       `json:"lines"`
	Total      int                            `json:"total"`
	LogPath    string                         `json:"log_path"`
	Truncated  bool                           `json:"truncated,omitempty"`
	Warning    string                         `json:"warning,omitempty"`
	TimeFilter *transport.LogTimeFilterResult `json:"time_filter,omitempty"`
}

type domainLogQuery struct {
	lines     int
	filter    string
	startTime string
	endTime   string
}

func parseDomainLogQuery(values url.Values) (domainLogQuery, error) {
	result := domainLogQuery{
		lines:     100,
		filter:    values.Get("filter"),
		startTime: values.Get("start_time"),
		endTime:   values.Get("end_time"),
	}
	if raw := values.Get("lines"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 5000 {
			return domainLogQuery{}, fmt.Errorf("lines must be an integer between 1 and 5000")
		}
		result.lines = parsed
	}
	if len(result.filter) > 1024 {
		return domainLogQuery{}, fmt.Errorf("filter exceeds the 1024-byte limit")
	}

	parseTime := func(field, raw string) (*time.Time, error) {
		if raw == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be RFC3339 with an explicit timezone", field)
		}
		return &parsed, nil
	}
	start, err := parseTime("start_time", result.startTime)
	if err != nil {
		return domainLogQuery{}, err
	}
	end, err := parseTime("end_time", result.endTime)
	if err != nil {
		return domainLogQuery{}, err
	}
	if start != nil && end != nil && start.After(*end) {
		return domainLogQuery{}, fmt.Errorf("start_time must not be after end_time")
	}
	return result, nil
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
	ctx := r.Context()
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

	query, err := parseDomainLogQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Call agent to get logs
	var agentResp transport.GetLogsResponse

	agentReq := transport.GetLogsRequest{
		LogPath:   logPath,
		Lines:     query.lines,
		Filter:    query.filter,
		StartTime: query.startTime,
		EndTime:   query.endTime,
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

	err = p.callAgent(rpcMethod, agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		// A missing log file is not a server error: a fresh domain simply has
		// no traffic yet. Report it as an honest empty log.
		// Eksik bir günlük dosyası sunucu hatası değildir: yeni bir domain'in
		// henüz trafiği yoktur. Bunu dürüst bir boş günlük olarak bildir.
		msg := agentResp.Error
		if err != nil {
			msg = err.Error()
		}
		if strings.Contains(msg, "log file not found") || strings.Contains(msg, "no such file") {
			json.NewEncoder(w).Encode(LogsResponse{Success: true, Lines: []string{}, Total: 0, LogPath: logPath})
			return
		}
		writeAgentError(w, err, agentResp.Error)
		return
	}

	response := LogsResponse{
		Success:    true,
		Lines:      agentResp.Lines,
		Total:      agentResp.Total,
		LogPath:    logPath,
		Truncated:  agentResp.Truncated,
		Warning:    agentResp.Warning,
		TimeFilter: agentResp.TimeFilter,
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
	var agentResp transport.ClearLogsResponse

	agentReq := transport.ClearLogsRequest{
		LogPath: logPath,
	}

	err = p.callAgent("Agent.ClearLogs", agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		writeAgentError(w, err, agentResp.Error)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
