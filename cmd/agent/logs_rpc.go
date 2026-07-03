package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Log Viewer RPC Methods

// GetLogsRequest represents a request to get logs
type GetLogsRequest struct {
	LogPath   string `json:"log_path"`
	Lines     int    `json:"lines"`      // Number of lines to return (tail -n)
	Filter    string `json:"filter"`     // Optional filter string
	StartTime string `json:"start_time"` // Optional start time (ISO 8601)
	EndTime   string `json:"end_time"`   // Optional end time (ISO 8601)
}

// GetLogsResponse represents log data
type GetLogsResponse struct {
	Success bool     `json:"success"`
	Lines   []string `json:"lines"`
	Total   int      `json:"total"`
	Error   string   `json:"error,omitempty"`
}

// ClearLogsRequest represents a request to clear logs
type ClearLogsRequest struct {
	LogPath string `json:"log_path"`
}

// ClearLogsResponse represents the response from clearing logs
type ClearLogsResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GetAccessLogs retrieves nginx access logs for a domain
func (a *Agent) GetAccessLogs(req GetLogsRequest, resp *GetLogsResponse) error {
	return a.getLogs(req, resp)
}

// GetErrorLogs retrieves nginx error logs for a domain
func (a *Agent) GetErrorLogs(req GetLogsRequest, resp *GetLogsResponse) error {
	return a.getLogs(req, resp)
}

// GetPHPLogs retrieves PHP error logs for a domain
func (a *Agent) GetPHPLogs(req GetLogsRequest, resp *GetLogsResponse) error {
	return a.getLogs(req, resp)
}

// getLogs is a generic log retrieval function
func (a *Agent) getLogs(req GetLogsRequest, resp *GetLogsResponse) error {
	// Validate log path
	if req.LogPath == "" {
		resp.Success = false
		resp.Error = "log path is required"
		return nil
	}

	// Security: Ensure log path is within allowed directories
	allowedDirs := []string{"/var/log/nginx", "/var/log/php", "/var/www"}
	allowed := false
	for _, dir := range allowedDirs {
		if strings.HasPrefix(req.LogPath, dir) {
			allowed = true
			break
		}
	}
	if !allowed {
		resp.Success = false
		resp.Error = "access denied: log path not in allowed directories"
		return nil
	}

	// Check if file exists
	if _, err := os.Stat(req.LogPath); os.IsNotExist(err) {
		resp.Success = false
		resp.Error = "log file not found"
		return nil
	}

	// Read log file
	file, err := os.Open(req.LogPath)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to open log file: %v", err)
		return nil
	}
	defer file.Close()

	// Read lines
	var lines []string
	scanner := bufio.NewScanner(file)
	
	// If lines limit is set, use tail-like behavior
	if req.Lines > 0 {
		// Read all lines first
		var allLines []string
		for scanner.Scan() {
			line := scanner.Text()
			
			// Apply filter if specified
			if req.Filter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(req.Filter)) {
				continue
			}
			
			// Apply time filter if specified
			if req.StartTime != "" || req.EndTime != "" {
				// Parse log timestamp (nginx format: [01/Jan/2024:12:00:00 +0000])
				// This is simplified - you'd need proper parsing for production
				if !isWithinTimeRange(line, req.StartTime, req.EndTime) {
					continue
				}
			}
			
			allLines = append(allLines, line)
		}
		
		// Get last N lines
		start := 0
		if len(allLines) > req.Lines {
			start = len(allLines) - req.Lines
		}
		lines = allLines[start:]
	} else {
		// Read all lines
		for scanner.Scan() {
			line := scanner.Text()
			
			if req.Filter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(req.Filter)) {
				continue
			}
			
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("error reading log file: %v", err)
		return nil
	}

	resp.Success = true
	resp.Lines = lines
	resp.Total = len(lines)
	return nil
}

// ClearLogs truncates a log file
func (a *Agent) ClearLogs(req ClearLogsRequest, resp *ClearLogsResponse) error {
	// Validate log path
	if req.LogPath == "" {
		resp.Success = false
		resp.Error = "log path is required"
		return nil
	}

	// Security: Ensure log path is within allowed directories
	allowedDirs := []string{"/var/log/nginx", "/var/log/php", "/var/www"}
	allowed := false
	for _, dir := range allowedDirs {
		if strings.HasPrefix(req.LogPath, dir) {
			allowed = true
			break
		}
	}
	if !allowed {
		resp.Success = false
		resp.Error = "access denied: log path not in allowed directories"
		return nil
	}

	// Truncate file
	err := os.Truncate(req.LogPath, 0)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to clear log file: %v", err)
		return nil
	}

	resp.Success = true
	return nil
}

// GetDomainLogPaths returns the log file paths for a domain
func (a *Agent) GetDomainLogPaths(domain string, resp *struct {
	AccessLog string `json:"access_log"`
	ErrorLog  string `json:"error_log"`
	PHPLog    string `json:"php_log"`
}) error {
	// Standard nginx log paths
	resp.AccessLog = filepath.Join("/var/log/nginx", domain+"-access.log")
	resp.ErrorLog = filepath.Join("/var/log/nginx", domain+"-error.log")
	resp.PHPLog = filepath.Join("/var/log/php", domain+"-error.log")
	
	return nil
}

// isWithinTimeRange checks if a log line is within the specified time range
// This is a simplified implementation - you'd need proper timestamp parsing for production
func isWithinTimeRange(line, startTime, endTime string) bool {
	// TODO: Implement proper timestamp parsing from log line
	// For now, just return true
	return true
}

// Helper function to parse nginx log timestamp
func parseNginxTimestamp(timestamp string) (time.Time, error) {
	// nginx format: [01/Jan/2024:12:00:00 +0000]
	layout := "02/Jan/2006:15:04:05 -0700"
	timestamp = strings.Trim(timestamp, "[]")
	return time.Parse(layout, timestamp)
}
