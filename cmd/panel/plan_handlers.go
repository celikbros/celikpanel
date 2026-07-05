package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Service plans: reusable quota templates (Plesk "Service Plans"). Reading
// is open to admins and resellers (they pick a plan when onboarding a
// customer); writing is admin-only in v1.
//
// Servis planları: yeniden kullanılabilir kota şablonları (Plesk "Service
// Plans"). Okuma admin ve bayilere açıktır (müşteri açarken plan seçerler);
// yazma v1'de yalnızca admin.

type planPayload struct {
	ID               int    `json:"id,omitempty"`
	Name             string `json:"name"`
	MaxDomains       int    `json:"max_domains"`
	MaxDatabases     int    `json:"max_databases"`
	MaxEmailAccounts int    `json:"max_email_accounts"`
	DiskQuotaMB      int    `json:"disk_quota_mb"`
	BandwidthQuotaMB int    `json:"bandwidth_quota_mb"`
	Subscribers      int    `json:"subscribers,omitempty"`
}

func (p *Panel) handlePlans(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := p.requireManager(w, r)
	if c == nil {
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := p.db.GetDB().QueryContext(r.Context(), `
			SELECT sp.id, sp.name, sp.max_domains, sp.max_databases, sp.max_email_accounts,
			       sp.disk_quota_mb, sp.bandwidth_quota_mb,
			       (SELECT COUNT(*) FROM subscriptions s WHERE s.plan_id = sp.id)
			FROM service_plans sp ORDER BY sp.name`)
		if err != nil {
			writeServerError(w, err)
			return
		}
		defer rows.Close()

		plans := make([]planPayload, 0)
		for rows.Next() {
			var pl planPayload
			if err := rows.Scan(&pl.ID, &pl.Name, &pl.MaxDomains, &pl.MaxDatabases,
				&pl.MaxEmailAccounts, &pl.DiskQuotaMB, &pl.BandwidthQuotaMB, &pl.Subscribers); err != nil {
				continue
			}
			plans = append(plans, pl)
		}
		json.NewEncoder(w).Encode(map[string]any{"plans": plans})

	case http.MethodPost:
		if c.Role != roleAdmin {
			writeClientError(w, http.StatusForbidden, "administrator access required")
			return
		}
		pl, ok := decodePlan(w, r)
		if !ok {
			return
		}
		res, err := p.db.GetDB().ExecContext(r.Context(), `
			INSERT INTO service_plans (name, max_domains, max_databases, max_email_accounts, disk_quota_mb, bandwidth_quota_mb)
			VALUES (?, ?, ?, ?, ?, ?)`,
			pl.Name, pl.MaxDomains, pl.MaxDatabases, pl.MaxEmailAccounts, pl.DiskQuotaMB, pl.BandwidthQuotaMB)
		if err != nil {
			writeServerError(w, err)
			return
		}
		id, _ := res.LastInsertId()
		json.NewEncoder(w).Encode(map[string]any{"success": true, "id": id})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handlePlanByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := p.requireManager(w, r)
	if c == nil {
		return
	}
	if c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "administrator access required")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/plans/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid plan id")
		return
	}

	switch r.Method {
	case http.MethodPut:
		pl, ok := decodePlan(w, r)
		if !ok {
			return
		}
		// Plan edits also refresh subscriptions born from the plan — that is
		// the point of a template (Plesk: "apply changes to subscribers").
		// Plan düzenlemeleri, plandan doğan abonelikleri de tazeler — bir
		// şablonun amacı budur (Plesk: "değişiklikleri abonelere uygula").
		if _, err := p.db.GetDB().ExecContext(r.Context(), `
			UPDATE service_plans SET name = ?, max_domains = ?, max_databases = ?,
			       max_email_accounts = ?, disk_quota_mb = ?, bandwidth_quota_mb = ?,
			       updated_at = datetime('now')
			WHERE id = ?`,
			pl.Name, pl.MaxDomains, pl.MaxDatabases, pl.MaxEmailAccounts,
			pl.DiskQuotaMB, pl.BandwidthQuotaMB, id); err != nil {
			writeServerError(w, err)
			return
		}
		if _, err := p.db.GetDB().ExecContext(r.Context(), `
			UPDATE subscriptions SET max_domains = ?, max_databases = ?,
			       max_email_accounts = ?, disk_quota_mb = ?, bandwidth_quota_mb = ?,
			       updated_at = datetime('now')
			WHERE plan_id = ?`,
			pl.MaxDomains, pl.MaxDatabases, pl.MaxEmailAccounts,
			pl.DiskQuotaMB, pl.BandwidthQuotaMB, id); err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case http.MethodDelete:
		var subscribers int
		_ = p.db.GetDB().QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM subscriptions WHERE plan_id = ?`, id).Scan(&subscribers)
		if subscribers > 0 {
			writeClientError(w, http.StatusConflict, "plan has subscriptions; move them to another plan first")
			return
		}
		if _, err := p.db.GetDB().ExecContext(r.Context(), `DELETE FROM service_plans WHERE id = ?`, id); err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func decodePlan(w http.ResponseWriter, r *http.Request) (planPayload, bool) {
	var pl planPayload
	if err := json.NewDecoder(r.Body).Decode(&pl); err != nil || strings.TrimSpace(pl.Name) == "" {
		writeClientError(w, http.StatusBadRequest, "plan name is required")
		return pl, false
	}
	if pl.MaxDomains <= 0 || pl.MaxDatabases < 0 || pl.MaxEmailAccounts < 0 || pl.DiskQuotaMB <= 0 {
		writeClientError(w, http.StatusBadRequest, "limits must be positive")
		return pl, false
	}
	return pl, true
}
