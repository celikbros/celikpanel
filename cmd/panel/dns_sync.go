package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
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

type dnsZoneSyncFailure struct {
	Zone string
	Err  error
}

// dnsSyncAllResult records the whole publication attempt. Settings changes use
// Failures as a hard error; ordinary record edits still call syncZoneToDNS
// directly and remain best-effort.
type dnsSyncAllResult struct {
	Attempted int
	Synced    int
	Failures  []dnsZoneSyncFailure
}

type dnsPublicationError struct {
	Result dnsSyncAllResult
}

// dnsAgentPublicationError marks failures that happened at the publication
// boundary: the agent RPC failed, rejected the zone, or did not confirm it.
// Only these failures are safe to present as a retryable HTTP 409. Ledger
// preparation/read failures remain internal HTTP 500 errors.
type dnsAgentPublicationError struct {
	Err error
}

func (e *dnsAgentPublicationError) Error() string {
	return e.Err.Error()
}

func (e *dnsAgentPublicationError) Unwrap() error {
	return e.Err
}

type dnsSyncInternalError struct {
	Result dnsSyncAllResult
}

func formatDNSFailures(prefix string, result dnsSyncAllResult) string {
	details := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		details = append(details, fmt.Sprintf("%s: %v", failure.Zone, failure.Err))
	}
	return fmt.Sprintf("%s for %d of %d zones: %s",
		prefix, len(result.Failures), result.Attempted, strings.Join(details, "; "))
}

func (e *dnsPublicationError) Error() string {
	return formatDNSFailures("DNS publication failed", e.Result)
}

func (e *dnsSyncInternalError) Error() string {
	return formatDNSFailures("DNS synchronization failed", e.Result)
}

func (r dnsSyncAllResult) err() error {
	if len(r.Failures) == 0 {
		return nil
	}
	for _, failure := range r.Failures {
		var publicationErr *dnsAgentPublicationError
		if !errors.As(failure.Err, &publicationErr) {
			return &dnsSyncInternalError{Result: r}
		}
	}
	return &dnsPublicationError{Result: r}
}

// writeDNSPublicationConflict exposes an operational, retryable publication
// failure without leaking agent output. It returns false for database and
// other internal errors so callers can keep treating those as HTTP 500.
func writeDNSPublicationConflict(w http.ResponseWriter, err error, safeMessage string) bool {
	var publicationErr *dnsPublicationError
	if !errors.As(err, &publicationErr) {
		return false
	}
	log.Printf("[409][dns] %v", err)
	writeClientError(w, http.StatusConflict, safeMessage)
	return true
}

// syncZoneToDNS pushes one domain's current record set from the ledger to
// the pdns database (or removes the zone when deleted is true).
// syncZoneToDNS, bir domain'in defterdeki güncel kayıt setini pdns
// veritabanına iter (deleted true ise zone'u kaldırır).
func (p *Panel) syncZoneToDNS(ctx context.Context, domain string, deleted bool) error {
	req := struct {
		Domain   string       `json:"domain"`
		Delete   bool         `json:"delete"`
		ZoneType string       `json:"zone_type,omitempty"`
		Records  []zoneRecord `json:"records"`
	}{Domain: domain, Delete: deleted, ZoneType: p.dnsZoneType(ctx)}

	if !deleted {
		if err := p.prepareZoneForSync(ctx, domain); err != nil {
			log.Printf("dns sync %s: prepare zone: %v", domain, err)
			return fmt.Errorf("prepare zone: %w", err)
		}
		rows, err := p.db.GetDB().QueryContext(ctx, `
			SELECT r.name, r.type, r.content, COALESCE(r.ttl, 3600), COALESCE(r.prio, 0), r.disabled
			FROM pdns_records r JOIN pdns_domains d ON d.id = r.domain_id
			WHERE d.name = ?`, domain)
		if err != nil {
			log.Printf("dns sync %s: read zone: %v", domain, err)
			return fmt.Errorf("read zone: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var rec zoneRecord
			if err := rows.Scan(&rec.Name, &rec.Type, &rec.Content, &rec.TTL, &rec.Prio, &rec.Disabled); err != nil {
				return fmt.Errorf("scan zone record: %w", err)
			}
			req.Records = append(req.Records, rec)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read zone rows: %w", err)
		}
		// A zone with no ledger rows means it was never created — nothing
		// to serve, nothing to push.
		// Defterde satırı olmayan zone hiç oluşturulmamış demektir — sunacak
		// da itecek de bir şey yok.
		if len(req.Records) == 0 {
			return fmt.Errorf("zone has no records")
		}
	}

	var resp struct {
		Synced bool   `json:"synced"`
		Error  string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.SyncDNSZone", &req, &resp); err != nil {
		log.Printf("dns sync %s: %v", domain, err)
		return &dnsAgentPublicationError{Err: err}
	}
	if resp.Error != "" {
		log.Printf("dns sync %s: %s", domain, resp.Error)
		return &dnsAgentPublicationError{Err: errors.New(resp.Error)}
	}
	if !resp.Synced {
		return &dnsAgentPublicationError{Err: errors.New("agent did not confirm DNS publication")}
	}
	return nil
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

	result, err := p.syncAllZonesStrict(r.Context())
	if err != nil {
		err = fmt.Errorf("publish PowerDNS zones: %w", err)
		if writeDNSPublicationConflict(w, err,
			"PowerDNS was configured, but one or more DNS zones could not be published; check the DNS service and retry") {
			return
		}
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"success": true, "zones_synced": result.Synced})
}

// syncAllZonesResult pushes every ledger zone and retains every failure. The
// callback keeps aggregation testable without changing Panel's RPC client.
func (p *Panel) syncAllZonesResult(
	ctx context.Context,
	syncZone func(context.Context, string, bool) error,
) (dnsSyncAllResult, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT name FROM pdns_domains ORDER BY name`)
	if err != nil {
		return dnsSyncAllResult{}, fmt.Errorf("list DNS zones: %w", err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return dnsSyncAllResult{}, fmt.Errorf("scan DNS zone: %w", err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return dnsSyncAllResult{}, fmt.Errorf("list DNS zones: %w", err)
	}
	if err := rows.Close(); err != nil {
		return dnsSyncAllResult{}, fmt.Errorf("close DNS zone list: %w", err)
	}

	result := dnsSyncAllResult{Attempted: len(names)}
	for _, n := range names {
		if err := syncZone(ctx, n, false); err != nil {
			result.Failures = append(result.Failures, dnsZoneSyncFailure{Zone: n, Err: err})
			continue
		}
		result.Synced++
	}
	return result, nil
}

// syncAllZonesStrict is for settings operations whose success promises that
// the new topology is already published. It still attempts every zone so one
// bad zone does not prevent healthy zones from receiving the update.
func (p *Panel) syncAllZonesStrict(ctx context.Context) (dnsSyncAllResult, error) {
	result, err := p.syncAllZonesResult(ctx, p.syncZoneToDNS)
	if err != nil {
		return result, err
	}
	return result, result.err()
}

// syncAllZones is the legacy best-effort wrapper used by repair/install
// flows. It logs partial failure and returns the number actually published.
func (p *Panel) syncAllZones(ctx context.Context) int {
	result, err := p.syncAllZonesResult(ctx, p.syncZoneToDNS)
	if err != nil {
		log.Printf("dns sync all: %v", err)
		return result.Synced
	}
	if err := result.err(); err != nil {
		log.Printf("dns sync all: %v", err)
	}
	return result.Synced
}
