package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// handlePostfixQueue serves the real Postfix queue (GET) and queue actions
// (POST), both sourced from the agent — no fabricated data.
// handlePostfixQueue, gerçek Postfix kuyruğunu (GET) ve kuyruk eylemlerini
// (POST) sunar; ikisi de agent'tan gelir — uydurma veri yok.
func (p *Panel) handlePostfixQueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		var req core.PostfixActionRequest
		if err := decodeStrictJSON(w, r, &req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}
		var ok bool
		if err := p.callAgentContext(r.Context(), "Agent.PostfixQueueAction", &req, &ok); err != nil {
			writeServerError(w, err)
			return
		}
		if !ok {
			http.Error(w, "mail queue action was not confirmed by the agent", http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": ok})
		return
	}
	if r.Method != http.MethodGet {
		rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost})
		return
	}

	var result core.PostfixQueueResult
	if err := p.callAgentContext(r.Context(), "Agent.PostfixQueue", &transport.Empty{}, &result); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(result.Items)
}

// handlePostfixSummary returns the real queue counts by status.
// handlePostfixSummary, duruma göre gerçek kuyruk sayılarını döndürür.
func (p *Panel) handlePostfixSummary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var result core.PostfixQueueResult
	if err := p.callAgentContext(r.Context(), "Agent.PostfixQueue", &transport.Empty{}, &result); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(result.Summary)
}

// handleDovecotStats returns the measurable Dovecot state from the agent.
// handleDovecotStats, agent'tan ölçülebilir Dovecot durumunu döndürür.
func (p *Panel) handleDovecotStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var result core.DovecotStatsResult
	if err := p.callAgentContext(r.Context(), "Agent.DovecotStats", &transport.Empty{}, &result); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(result.Stats)
}
