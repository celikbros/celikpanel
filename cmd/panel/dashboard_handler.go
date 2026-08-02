package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"
)

// Dashboard extras: the few aggregates the dashboard cannot assemble from
// existing endpoints — entity counts and certificates nearing expiry.
// Everything else on the dashboard (system stats, services, firewall,
// domains, audit trail) comes from the endpoints that already exist.
//
// Pano ekleri: panonun mevcut uçlardan derleyemediği birkaç toplam —
// varlık sayaçları ve süresi yaklaşan sertifikalar. Panodaki diğer her şey
// (sistem istatistikleri, servisler, güvenlik duvarı, domainler, denetim
// izi) zaten var olan uçlardan gelir.

type dashboardExpiringCert struct {
	DomainName string `json:"domain_name"`
	DaysLeft   int    `json:"days_left"`
}

type dashboardExtras struct {
	Databases     int                     `json:"databases"`
	MailAccounts  int                     `json:"mail_accounts"`
	ExpiringCerts []dashboardExpiringCert `json:"expiring_certs"`
}

func (p *Panel) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	out := dashboardExtras{ExpiringCerts: []dashboardExpiringCert{}}
	db := p.db.GetDB()

	if err := db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM databases_v2`,
	).Scan(&out.Databases); err != nil {
		writeServerError(w, fmt.Errorf("count databases for dashboard: %w", err))
		return
	}
	if err := db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM email_accounts`,
	).Scan(&out.MailAccounts); err != nil {
		writeServerError(w, fmt.Errorf("count mail accounts for dashboard: %w", err))
		return
	}

	rows, err := db.QueryContext(r.Context(), `
		SELECT d.name, c.expires_at
		FROM ssl_certificates c
		JOIN domains d ON d.id = c.domain_id
		WHERE c.status = 'active'`)
	if err != nil {
		writeServerError(w, fmt.Errorf("list certificates for dashboard: %w", err))
		return
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var name, expires string
		if err := rows.Scan(&name, &expires); err != nil {
			writeServerError(w, fmt.Errorf("scan dashboard certificate: %w", err))
			return
		}
		exp, ok := parseCertTime(expires)
		if !ok {
			writeServerError(w, fmt.Errorf(
				"active certificate for %q has an invalid expiry timestamp",
				name,
			))
			return
		}
		if days := int(exp.Sub(now).Hours() / 24); days <= 30 {
			out.ExpiringCerts = append(out.ExpiringCerts, dashboardExpiringCert{DomainName: name, DaysLeft: days})
		}
	}
	if err := rows.Err(); err != nil {
		writeServerError(w, fmt.Errorf("iterate dashboard certificates: %w", err))
		return
	}
	sort.Slice(out.ExpiringCerts, func(i, j int) bool {
		return out.ExpiringCerts[i].DaysLeft < out.ExpiringCerts[j].DaysLeft
	})
	if len(out.ExpiringCerts) > 6 {
		out.ExpiringCerts = out.ExpiringCerts[:6]
	}

	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("encode dashboard response: %v", err)
	}
}

// expires_at is TEXT and historic rows carry mixed layouts (the sqlite
// time.Time scan issue) — accept every layout we have ever written.
// expires_at TEXT'tir ve eski satırlar karışık biçim taşır (sqlite
// time.Time tarama sorunu) — bugüne dek yazdığımız her biçimi kabul et.
func parseCertTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
