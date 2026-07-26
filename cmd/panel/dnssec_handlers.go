package main

import (
	"context"
	"encoding/json"
	"log"
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

type dnssecAgentResponse struct {
	Secured bool     `json:"secured"`
	DS      []string `json:"ds,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// dnssecResultError keeps failures returned inside an otherwise successful
// RPC response visible to the operator. It also protects a newer panel from an
// older agent that used to report signing success even when pdnsutil produced
// no DS record to publish at the registrar.
func dnssecResultError(resp dnssecAgentResponse, signing bool) string {
	if resp.Error != "" {
		return resp.Error
	}
	if resp.Secured && len(resp.DS) == 0 {
		return "DNSSEC is not ready: the signed zone produced no DS records"
	}
	if signing && !resp.Secured {
		return "DNSSEC signing did not complete"
	}
	return ""
}

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

	var resp dnssecAgentResponse
	switch r.Method {
	case http.MethodGet:
		if err := p.agentClient.Call("Agent.DNSSECStatus", &struct {
			Zone string `json:"zone"`
		}{Zone: domain}, &resp); err != nil {
			writeAgentError(w, err, "DNSSEC")
			return
		}
		if problem := dnssecResultError(resp, false); problem != "" {
			writeClientError(w, http.StatusConflict, problem)
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
		if problem := dnssecResultError(resp, true); problem != "" {
			writeClientError(w, http.StatusConflict, problem)
			return
		}
		// Bump the ledger SOA serial, republish the now-signed primary, rectify
		// it, and only then send NOTIFY. This forces a full signed transfer path
		// before the registrar-facing DS is handed back as a successful result.
		if err := p.syncZoneToDNS(r.Context(), domain, false); err != nil {
			log.Printf("dnssec publish %s: %v", domain, err)
			writeClientError(w, http.StatusConflict,
				"the zone was signed locally but its updated SOA could not be published; check the PowerDNS pair and retry")
			return
		}
		p.audit(r, "dnssec.sign", "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{"secured": resp.Secured, "ds": resp.DS})

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
