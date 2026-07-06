package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
)

// The ledger↔pdns bridge. Zone records live in the panel's own database
// (the ledger, ownership-filtered, edited via the UI); PowerDNS serves from
// its own separate sqlite file. Every zone mutation ends with a push of the
// full zone through the agent — full-zone rewrite is idempotent, so a missed
// push is repaired by the next one. Best-effort by design: DNS serving being
// down must not block panel edits; failures land in the log.
//
// Defter↔pdns köprüsü. Zone kayıtları panelin kendi veritabanında yaşar
// (defter, sahiplik-süzgeçli, arayüzden düzenlenir); PowerDNS kendi ayrı
// sqlite dosyasından sunar. Her zone değişikliği, tam zone'un agent
// üzerinden itilmesiyle biter — tam-zone yazımı idempotenttir; kaçan bir
// itiş sonrakiyle onarılır. Bilerek en-iyi-çaba: DNS sunumunun kapalı olması
// panel düzenlemelerini engellememeli; hatalar günlüğe düşer.

type zoneRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Prio     int    `json:"prio"`
	Disabled bool   `json:"disabled"`
}

// syncZoneToDNS pushes one domain's current record set from the ledger to
// the pdns database (or removes the zone when deleted is true).
// syncZoneToDNS, bir domain'in defterdeki güncel kayıt setini pdns
// veritabanına iter (deleted true ise zone'u kaldırır).
func (p *Panel) syncZoneToDNS(ctx context.Context, domain string, deleted bool) {
	req := struct {
		Domain  string       `json:"domain"`
		Delete  bool         `json:"delete"`
		Records []zoneRecord `json:"records"`
	}{Domain: domain, Delete: deleted}

	if !deleted {
		rows, err := p.db.GetDB().QueryContext(ctx, `
			SELECT r.name, r.type, r.content, COALESCE(r.ttl, 3600), COALESCE(r.prio, 0), r.disabled
			FROM pdns_records r JOIN pdns_domains d ON d.id = r.domain_id
			WHERE d.name = ?`, domain)
		if err != nil {
			log.Printf("dns sync %s: read zone: %v", domain, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var rec zoneRecord
			if rows.Scan(&rec.Name, &rec.Type, &rec.Content, &rec.TTL, &rec.Prio, &rec.Disabled) == nil {
				req.Records = append(req.Records, rec)
			}
		}
		// A zone with no ledger rows means it was never created — nothing
		// to serve, nothing to push.
		// Defterde satırı olmayan zone hiç oluşturulmamış demektir — sunacak
		// da itecek de bir şey yok.
		if len(req.Records) == 0 {
			return
		}
	}

	var resp struct {
		Synced bool   `json:"synced"`
		Error  string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.SyncDNSZone", &req, &resp); err != nil {
		log.Printf("dns sync %s: %v", domain, err)
		return
	}
	if resp.Error != "" {
		log.Printf("dns sync %s: %s", domain, resp.Error)
	}
}

// handlePDNSEnable configures PowerDNS with our dedicated sqlite backend and
// pushes every ledger zone into it — the one-shot "start serving DNS"
// action. Admin-only via the /api/v1/pdns/ prefix.
// handlePDNSEnable, PowerDNS'i bize ayrılmış sqlite backend'iyle yapılandırır
// ve defterdeki her zone'u içine iter — tek seferlik "DNS sunmaya başla"
// eylemi. /api/v1/pdns/ öneki üzerinden yalnız admin.
func (p *Panel) handlePDNSEnable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var resp struct {
		Synced bool   `json:"synced"`
		Error  string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.ConfigurePowerDNSSQLite", &struct{}{}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}

	synced := p.syncAllZones(r.Context())
	json.NewEncoder(w).Encode(map[string]any{"success": true, "zones_synced": synced})
}

// syncAllZones pushes every zone in the ledger; returns how many.
// syncAllZones, defterdeki her zone'u iter; kaç tane olduğunu döndürür.
func (p *Panel) syncAllZones(ctx context.Context) int {
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT name FROM pdns_domains`)
	if err != nil {
		log.Printf("dns sync all: %v", err)
		return 0
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names = append(names, n)
		}
	}
	for _, n := range names {
		p.syncZoneToDNS(ctx, n, false)
	}
	return len(names)
}
