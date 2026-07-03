package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
)

// handlePostfixQueue handles GET and POST requests for Postfix queue
func (p *Panel) handlePostfixQueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data - would call `postqueue -p`
		queue := []core.PostfixQueueItem{
			{ID: "3A4B5C6D7E", Size: "4096", Sender: "mailer-daemon@example.com", Arrival: "Fri Dec 2 10:00:00", Status: "deferred"},
			{ID: "1F2E3D4C5B", Size: "1024", Sender: "newsletter@example.com", Arrival: "Fri Dec 2 10:05:00", Status: "active"},
		}
		json.NewEncoder(w).Encode(queue)
		return
	}

	if r.Method == "POST" {
		var req core.PostfixActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Call agent to perform queue action (postsuper -d, postqueue -f)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Queue action executed"})
	}
}

// handlePostfixSummary handles GET requests for queue summary
func (p *Panel) handlePostfixSummary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data
		summary := core.PostfixSummary{
			Active:   12,
			Deferred: 45,
			Hold:     0,
			Corrupt:  0,
		}
		json.NewEncoder(w).Encode(summary)
		return
	}
}

// handleDovecotStats handles GET requests for Dovecot statistics
func (p *Panel) handleDovecotStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		// Mock data - would call `doveadm stats dump`
		stats := core.DovecotStats{
			Uptime:      "2 days, 4 hours",
			Connections: 150,
			Logins:      1250,
			AuthSuccess: 1240,
			AuthFail:    10,
		}
		json.NewEncoder(w).Encode(stats)
		return
	}
}
