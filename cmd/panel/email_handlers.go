package main

import (
	"encoding/json"
	"log"
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
		writeMailQueueReadError(w, err)
		return
	}
	if result.Items == nil {
		result.Items = []core.PostfixQueueItem{}
	}
	json.NewEncoder(w).Encode(result.Items)
}

// The queue could not be read: what happened, that nothing was changed, who
// acts and the action (D-024). The Agent's own line stays in its log.
// Kuyruk okunamadı: ne oldu, hiçbir şeyin değişmediği, kim işlem yapar ve eylem.
//
// No cause is asserted that was not verified (10 Oct 2026). The sentence used
// to end "the server owner checks that Postfix is running"; measured, a
// stopped Postfix does not make the queue unreadable (postqueue then reads it
// directly), and the one measured cause was a main.cf Postfix refuses.
// Doğrulanmamış bir neden ileri sürülmez. Durmuş bir Postfix kuyruğu okunamaz
// yapmaz; ölçülen tek neden Postfix'in reddettiği bir main.cf idi.
const mailQueueUnreadableMessage = "The mail queue could not be read, so it is not shown. This does not mean the queue is empty. Nothing was changed. " +
	"CelikPanel has not established why; what Postfix's queue program said is shown with this message when it said anything. " +
	"Try again. If it keeps failing, the server owner runs sudo postqueue -j on the server, which prints the same reason."

var mailQueueUnreadableCauses = map[string]string{
	transport.UnreadablePostfixConfig: "The mail queue could not be read because Postfix refuses its own configuration: a setting in /etc/postfix/main.cf or master.cf has an error, and Postfix's programs stop on it. " +
		"What Postfix said about it is shown with this message. Nothing was changed, and this does not mean the queue is empty. " +
		"The server owner corrects that setting, runs sudo postfix check until it prints no error, then reloads this page.",
}

// writeMailQueueReadError answers a failed queue read. The Agent's known
// "could not be read" becomes 502 MAIL_QUEUE_UNREADABLE; anything else keeps
// the ordinary classified path. Neither is ever an empty list.
// writeMailQueueReadError, başarısız bir kuyruk okumasını yanıtlar; hiçbiri boş
// liste değildir.
func writeMailQueueReadError(w http.ResponseWriter, err error) {
	known, cause, said := agentUnreadableEvidence(err, transport.PostfixQueueUnreadable)
	if !known {
		writeServerError(w, err)
		return
	}
	message, verified := mailQueueUnreadableCauses[cause]
	if !verified {
		message, cause = mailQueueUnreadableMessage, ""
	}
	var vars map[string]string
	if said = boundedAgentDiagnostic(said); said != "" {
		vars = map[string]string{"detail": said}
	}
	log.Printf("[502][mail queue] could not be read %s", cause)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: errCodeMailQueueUnreadable, Reason: cause, Vars: vars})
}

// handlePostfixSummary returns the real queue counts by status.
// handlePostfixSummary, duruma göre gerçek kuyruk sayılarını döndürür.
func (p *Panel) handlePostfixSummary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var result core.PostfixQueueResult
	if err := p.callAgentContext(r.Context(), "Agent.PostfixQueue", &transport.Empty{}, &result); err != nil {
		writeMailQueueReadError(w, err)
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
