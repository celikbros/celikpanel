package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// DNSSEC + DANE, panel side. Signing happens in pdns through the agent; the
// panel's job is the DS handoff (shown to the operator for the registrar)
// and keeping TLSA records in the zone ledger in step with the mail
// certificate — publish on secure-mail, refresh on renewal, remove on
// disable. Without a registrar DS the TLSA records are treated as insecure
// by validators (exactly what Plesk warns about), so the UI shows both
// together.
//
// DNSSEC + DANE, panel tarafı. İmzalama agent üzerinden pdns'te olur;
// panelin işi DS teslimi (operatöre registrar için gösterilir) ve TLSA
// kayıtlarını zone defterinde posta sertifikasıyla adımda tutmaktır —
// posta-koruma açılınca yayımla, yenilemede tazele, kapanınca kaldır.
// Registrar'da DS olmadan doğrulayıcılar TLSA kayıtlarını güvensiz sayar
// (tam Plesk'in uyardığı şey); bu yüzden arayüz ikisini birlikte gösterir.

// mailTLSAPorts are the mail service ports that get TLSA records, matching
// what Plesk publishes: SMTP, POP3(S), submission(s), IMAPS.
// mailTLSAPorts, TLSA kaydı alan posta servis portlarıdır; Plesk'in
// yayımladığıyla eşleşir.
var mailTLSAPorts = []string{"25", "110", "465", "587", "993", "995"}

// handleDomainDNSSEC: GET returns signing status + DS records; POST signs
// the zone (admins and the domain's managers — same dispatcher authz as the
// other domain endpoints).
// handleDomainDNSSEC: GET imza durumu + DS kayıtlarını döndürür; POST zone'u
// imzalar.
func (p *Panel) handleDomainDNSSEC(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	var domain string
	if p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT name FROM domains WHERE id = ?`, domainID).Scan(&domain) != nil {
		writeClientError(w, http.StatusNotFound, "domain not found")
		return
	}

	var resp struct {
		Secured bool     `json:"secured"`
		DS      []string `json:"ds,omitempty"`
		Error   string   `json:"error,omitempty"`
	}
	switch r.Method {
	case http.MethodGet:
		if err := p.agentClient.Call("Agent.DNSSECStatus", &struct {
			Zone string `json:"zone"`
		}{Zone: domain}, &resp); err != nil {
			writeAgentError(w, err, "DNSSEC")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"secured": resp.Secured, "ds": resp.DS})

	case http.MethodPost:
		if err := p.agentClient.Call("Agent.SecureDNSZone", &struct {
			Zone string `json:"zone"`
		}{Zone: domain}, &resp); err != nil {
			writeAgentError(w, err, "DNSSEC")
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		p.audit(r, "dnssec.sign", "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{"secured": true, "ds": resp.DS})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// refreshTLSARecords brings the zone's TLSA records in line with reality:
// when the domain's mail is secured by its certificate, publish TLSA for the
// mail ports at mail.<domain>; otherwise remove them. Ends with a zone push.
// refreshTLSARecords, zone'un TLSA kayıtlarını gerçekle hizalar: domain'in
// postası sertifikasıyla korunuyorsa mail.<domain>'de posta portları için
// TLSA yayımlar; değilse kaldırır. Zone itişiyle biter.
func (p *Panel) refreshTLSARecords(ctx context.Context, domainID int) error {
	var domain string
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT name FROM domains WHERE id = ?`, domainID).Scan(&domain); err != nil {
		return err
	}

	var certPath string
	_ = p.db.GetDB().QueryRowContext(ctx, `
		SELECT cert_path FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active' AND secure_mail = 1
		ORDER BY created_at DESC LIMIT 1`, domainID).Scan(&certPath)

	// Drop the old TLSA rows either way; re-add below when mail is secured.
	// Eski TLSA satırlarını her durumda düşür; posta korunuyorsa aşağıda
	// yeniden ekle.
	var zoneID int
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, domain).Scan(&zoneID); err != nil {
		// No zone in the ledger — nothing to publish TLSA into.
		// Defterde zone yok — TLSA yayımlanacak yer yok.
		return nil
	}
	if _, err := p.db.GetDB().ExecContext(ctx,
		`DELETE FROM pdns_records WHERE domain_id = ? AND type = 'TLSA'`, zoneID); err != nil {
		return err
	}

	if certPath != "" {
		var tlsa struct {
			Content string `json:"content"`
			Error   string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ComputeTLSA", &struct {
			CertPath string `json:"cert_path"`
		}{CertPath: certPath}, &tlsa); err != nil {
			return err
		}
		if tlsa.Error != "" {
			return &backupError{tlsa.Error}
		}
		for _, port := range mailTLSAPorts {
			name := "_" + port + "._tcp.mail." + domain
			if _, err := p.db.GetDB().ExecContext(ctx, `
				INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio, disabled)
				VALUES (?, ?, 'TLSA', ?, 3600, 0, 0)`, zoneID, name, tlsa.Content); err != nil {
				return err
			}
		}
	}

	p.syncZoneToDNS(ctx, domain, false)
	return nil
}
