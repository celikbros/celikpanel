package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

// The audit trail: who did what, from where, when. The audit_logs table has
// existed since the first schema but nothing ever wrote to it — a panel that
// looks like it keeps a record but does not. Every sensitive action now
// leaves a row. Best-effort by design: an audit write must never fail the
// action it records, but a failure is logged so a broken trail is visible.
//
// Denetim izi: kim, nereden, ne zaman, ne yaptı. audit_logs tablosu ilk
// şemadan beri var ama hiçbir şey ona yazmıyordu — kayıt tutuyormuş gibi
// görünüp tutmayan bir panel. Artık her hassas eylem bir satır bırakır.
// Bilerek en-iyi-çaba: bir denetim yazımı, kaydettiği eylemi asla
// başarısız kılmamalı; ama başarısızlık günlüğe düşer ki bozuk iz görünsün.

// audit records an action performed by the current caller (resolved from the
// session). resourceID 0 is stored as NULL.
// audit, geçerli çağıran tarafından yapılan bir eylemi kaydeder. resourceID 0
// NULL saklanır.
func (p *Panel) audit(r *http.Request, action, resourceType string, resourceID int) {
	uid := 0
	if c := currentCaller(r); c != nil {
		uid = c.ID
	}
	p.auditAs(r, uid, action, resourceType, resourceID)
}

// auditAs records an action attributed to an explicit user — used at login,
// where the session (and thus currentCaller) does not exist yet.
// auditAs, açık bir kullanıcıya atfedilen bir eylemi kaydeder — oturumun
// (dolayısıyla currentCaller'ın) henüz olmadığı girişte kullanılır.
func (p *Panel) auditAs(r *http.Request, userID int, action, resourceType string, resourceID int) {
	var uid any
	if userID > 0 {
		uid = userID
	}
	var rid any
	if resourceID > 0 {
		rid = resourceID
	}
	var rtype any
	if resourceType != "" {
		rtype = resourceType
	}
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	if _, err := p.db.GetDB().ExecContext(r.Context(), `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)`,
		uid, action, rtype, rid, clientIP(r), ua); err != nil {
		log.Printf("audit write failed (%s): %v", action, err)
	}
}

type auditEntry struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   *int   `json:"resource_id,omitempty"`
	IPAddress    string `json:"ip_address,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// handleAuditLogs returns recent audit entries, newest first. Admin-only via
// the /api/v1/audit-logs path (server-wide history is not a per-tenant view).
// handleAuditLogs, en yeniden eskiye son denetim kayıtlarını döndürür.
// /api/v1/audit-logs yolu üzerinden yalnız admin (sunucu-geneli geçmiş
// kiracı-başına bir görünüm değildir).
func (p *Panel) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	rows, err := p.db.GetDB().QueryContext(r.Context(), `
		SELECT a.id, COALESCE(u.username, ''), a.action,
		       COALESCE(a.resource_type, ''), a.resource_id,
		       COALESCE(a.ip_address, ''), a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.id = a.user_id
		ORDER BY a.id DESC LIMIT ?`, limit)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()

	entries := []auditEntry{}
	for rows.Next() {
		var e auditEntry
		var rid sql.NullInt64
		if rows.Scan(&e.ID, &e.Username, &e.Action, &e.ResourceType, &rid, &e.IPAddress, &e.CreatedAt) != nil {
			continue
		}
		if rid.Valid {
			v := int(rid.Int64)
			e.ResourceID = &v
		}
		if e.Username == "" {
			e.Username = "system"
		}
		entries = append(entries, e)
	}
	json.NewEncoder(w).Encode(map[string]any{"entries": entries})
}
