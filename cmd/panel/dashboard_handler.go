package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
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
	// WaitingForOwner (D-031 step 1b): the site's configuration file the
	// owner kept does not use the new certificate yet, or stopped its
	// renewal. ServedDaysLeft is of the certificate the file most likely
	// still serves (absent when none is known); DomainID opens the page.
	WaitingForOwner bool `json:"waiting_for_owner,omitempty"`
	ServedDaysLeft  *int `json:"served_days_left,omitempty"`
	DomainID        int  `json:"domain_id,omitempty"`
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
		SELECT d.id, d.name, c.expires_at, COALESCE(c.renewal_status, '')
		FROM ssl_certificates c
		JOIN domains d ON d.id = c.domain_id
		WHERE c.status = 'active'`)
	if err != nil {
		writeServerError(w, fmt.Errorf("list certificates for dashboard: %w", err))
		return
	}
	defer rows.Close()
	now := time.Now()
	var waiting []int
	for rows.Next() {
		var domainID int
		var name, expires, renewal string
		if err := rows.Scan(&domainID, &name, &expires, &renewal); err != nil {
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
		days := int(exp.Sub(now).Hours() / 24)
		if renewal == sslWaitingForOwner {
			// Listed whatever the days: the owner's choice is needed.
			waiting = append(waiting, len(out.ExpiringCerts))
			out.ExpiringCerts = append(out.ExpiringCerts, dashboardExpiringCert{
				DomainName: name, DaysLeft: days, WaitingForOwner: true, DomainID: domainID,
			})
			continue
		}
		if days <= 30 {
			out.ExpiringCerts = append(out.ExpiringCerts, dashboardExpiringCert{DomainName: name, DaysLeft: days})
		}
	}
	if err := rows.Err(); err != nil {
		writeServerError(w, fmt.Errorf("iterate dashboard certificates: %w", err))
		return
	}
	for _, index := range waiting {
		entry := &out.ExpiringCerts[index]
		reason := ""
		if siteID, _, err := p.siteIDForDomain(r.Context(), entry.DomainID); err == nil {
			if record, found, err := services.LoadSiteFileRecord(r.Context(), db, siteID, transport.SiteFileKindNginxVhost); err == nil && found {
				reason = record.StateReason
			}
		}
		if certificate := p.siteConfigCertificateFor(r.Context(), entry.DomainID, reason, false); certificate != nil {
			entry.ServedDaysLeft = certificate.ServedDaysLeft
		} else {
			served := entry.DaysLeft
			entry.ServedDaysLeft = &served
		}
	}
	sortDashboardCertificates(out.ExpiringCerts)
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

// sortDashboardCertificates: certificates waiting for the owner first; a
// waiting one is ordered by the certificate in use (an expired one first,
// negative days, then the fewest days left), the others by their own days.
func sortDashboardCertificates(entries []dashboardExpiringCert) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.WaitingForOwner != b.WaitingForOwner {
			return a.WaitingForOwner
		}
		return dashboardServedDays(a) < dashboardServedDays(b)
	})
}

func dashboardServedDays(entry dashboardExpiringCert) int {
	if entry.WaitingForOwner && entry.ServedDaysLeft != nil {
		return *entry.ServedDaysLeft
	}
	return entry.DaysLeft
}
