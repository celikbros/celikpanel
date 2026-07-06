package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// handleDomainUsage measures one domain's real disk and monthly traffic via
// the agent, caches the numbers on the site row and returns them. Called by
// the domain detail page in the background after render — lists never
// trigger measurements, they read the cached columns.
// handleDomainUsage, bir domain'in gerçek disk ve aylık trafiğini agent
// üzerinden ölçer, sayıları site satırına önbellekler ve döndürür. Domain
// detay sayfası render'dan sonra arka planda çağırır — listeler asla ölçüm
// tetiklemez, önbellekli sütunları okur.
func (p *Panel) handleDomainUsage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	domainID, err := strconv.Atoi(parts[4])
	if err != nil {
		http.Error(w, "invalid domain ID", http.StatusBadRequest)
		return
	}

	var domainName, docroot string
	err = p.db.GetDB().QueryRowContext(r.Context(), `
		SELECT d.name, s.document_root
		FROM sites s JOIN domains d ON d.id = s.domain_id
		WHERE s.domain_id = ?`, domainID).Scan(&domainName, &docroot)
	if err != nil {
		http.Error(w, "site not found", http.StatusNotFound)
		return
	}

	var resp struct {
		DiskBytes         int64  `json:"disk_bytes"`
		TrafficMonthBytes int64  `json:"traffic_month_bytes"`
		Error             string `json:"error,omitempty"`
	}
	req := struct {
		SiteHome string `json:"site_home"`
		Domain   string `json:"domain"`
	}{SiteHome: filepath.Dir(docroot), Domain: domainName}
	if err := p.agentClient.Call("Agent.SiteUsage", &req, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}

	p.db.GetDB().ExecContext(r.Context(), `
		UPDATE sites SET disk_usage_bytes = ?, traffic_month_bytes = ?, usage_updated_at = ?
		WHERE domain_id = ?`,
		resp.DiskBytes, resp.TrafficMonthBytes, time.Now().UTC().Format(time.RFC3339), domainID)

	json.NewEncoder(w).Encode(map[string]any{
		"disk_usage": resp.DiskBytes,
		"bandwidth":  resp.TrafficMonthBytes,
	})
}
