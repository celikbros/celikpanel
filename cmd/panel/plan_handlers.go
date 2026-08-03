package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

const maxPlanRequestBytes = 32 << 10

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
				writeServerError(w, fmt.Errorf("scan service plan: %w", err))
				return
			}
			plans = append(plans, pl)
		}
		if err := rows.Err(); err != nil {
			writeServerError(w, fmt.Errorf("read service plans: %w", err))
			return
		}
		if err := rows.Close(); err != nil {
			writeServerError(w, fmt.Errorf("close service plan rows: %w", err))
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"plans": plans}); err != nil {
			log.Printf("encode service plans: %v", err)
		}

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
		id, err := res.LastInsertId()
		if err != nil {
			writeServerError(w, fmt.Errorf("read inserted service plan identity: %w", err))
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"success": true, "id": id}); err != nil {
			log.Printf("encode service plan creation: %v", err)
		}

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
	if err != nil || id <= 0 {
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
		tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
		if err != nil {
			writeServerError(w, fmt.Errorf("begin service plan update: %w", err))
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `
			UPDATE service_plans SET name = ?, max_domains = ?, max_databases = ?,
			       max_email_accounts = ?, disk_quota_mb = ?, bandwidth_quota_mb = ?,
			       updated_at = datetime('now')
			WHERE id = ?`,
			pl.Name, pl.MaxDomains, pl.MaxDatabases, pl.MaxEmailAccounts,
			pl.DiskQuotaMB, pl.BandwidthQuotaMB, id)
		if err != nil {
			writeServerError(w, fmt.Errorf("update service plan: %w", err))
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			writeServerError(w, fmt.Errorf("verify service plan update: %w", err))
			return
		}
		if affected == 0 {
			writeClientError(w, http.StatusNotFound, "service plan not found")
			return
		}
		if affected != 1 {
			writeServerError(w, fmt.Errorf("update service plan: expected one affected row, got %d", affected))
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE subscriptions SET max_domains = ?, max_databases = ?,
			       max_email_accounts = ?, disk_quota_mb = ?, bandwidth_quota_mb = ?,
			       updated_at = datetime('now')
			WHERE plan_id = ?`,
			pl.MaxDomains, pl.MaxDatabases, pl.MaxEmailAccounts,
			pl.DiskQuotaMB, pl.BandwidthQuotaMB, id); err != nil {
			writeServerError(w, fmt.Errorf("apply service plan to subscriptions: %w", err))
			return
		}
		if err := tx.Commit(); err != nil {
			writeServerError(w, fmt.Errorf("commit service plan update: %w", err))
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
			log.Printf("encode service plan update: %v", err)
		}

	case http.MethodDelete:
		tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
		if err != nil {
			writeServerError(w, fmt.Errorf("begin service plan deletion: %w", err))
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `
			DELETE FROM service_plans
			WHERE id = ?
			  AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE plan_id = service_plans.id)`, id)
		if err != nil {
			writeServerError(w, fmt.Errorf("delete service plan: %w", err))
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			writeServerError(w, fmt.Errorf("verify service plan deletion: %w", err))
			return
		}
		if affected == 0 {
			var exists bool
			if err := tx.QueryRowContext(r.Context(),
				`SELECT EXISTS(SELECT 1 FROM service_plans WHERE id = ?)`, id,
			).Scan(&exists); err != nil {
				writeServerError(w, fmt.Errorf("verify service plan existence: %w", err))
				return
			}
			if !exists {
				writeClientError(w, http.StatusNotFound, "service plan not found")
				return
			}
			writeClientError(w, http.StatusConflict, "plan has subscriptions; move them to another plan first")
			return
		}
		if affected != 1 {
			writeServerError(w, fmt.Errorf("delete service plan: expected one affected row, got %d", affected))
			return
		}
		if err := tx.Commit(); err != nil {
			writeServerError(w, fmt.Errorf("commit service plan deletion: %w", err))
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
			log.Printf("encode service plan deletion: %v", err)
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func decodePlan(w http.ResponseWriter, r *http.Request) (planPayload, bool) {
	var pl planPayload
	r.Body = http.MaxBytesReader(w, r.Body, maxPlanRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pl); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid service plan")
		return pl, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeClientError(w, http.StatusBadRequest, "invalid service plan")
		return pl, false
	}
	pl.Name = strings.TrimSpace(pl.Name)
	if pl.Name == "" {
		writeClientError(w, http.StatusBadRequest, "plan name is required")
		return pl, false
	}
	if pl.MaxDomains <= 0 || pl.MaxDatabases < 0 || pl.MaxEmailAccounts < 0 ||
		pl.DiskQuotaMB <= 0 || pl.BandwidthQuotaMB < 0 {
		writeClientError(w, http.StatusBadRequest, "limits must be positive or explicitly zero where unlimited is supported")
		return pl, false
	}
	return pl, true
}
