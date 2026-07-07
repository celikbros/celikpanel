package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
)

// Panel side of mail TLS. The ledger is ssl_certificates.secure_mail; every
// change re-pushes the full SNI set to the agent (the same full-state-push as
// DNS zones and VPN peers). The default certificate and the daemons are the
// agent's business.
//
// Posta TLS'inin panel tarafı. Defter ssl_certificates.secure_mail'dir; her
// değişiklik tam SNI setini agent'a yeniden iter (DNS zone'ları ve VPN
// peer'larıyla aynı tam-durum-itme). Varsayılan sertifika ve daemon'lar
// agent'ın işidir.

// resyncMailTLS pushes every active secure_mail certificate to the agent as
// SNI entries covering domain + mail./webmail. subdomains.
// resyncMailTLS, aktif her secure_mail sertifikasını domain + mail./webmail.
// alt adlarını kapsayan SNI girdileri olarak agent'a iter.
func (p *Panel) resyncMailTLS(ctx context.Context) error {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT d.name, sc.cert_path, sc.key_path
		FROM ssl_certificates sc JOIN domains d ON d.id = sc.domain_id
		WHERE sc.status = 'active' AND sc.secure_mail = 1`)
	if err != nil {
		return err
	}
	type entry struct {
		Names    []string `json:"names"`
		CertPath string   `json:"cert_path"`
		KeyPath  string   `json:"key_path"`
	}
	var sni []entry
	for rows.Next() {
		var name, cert, key string
		if rows.Scan(&name, &cert, &key) == nil {
			sni = append(sni, entry{
				Names:    []string{name, "mail." + name, "webmail." + name},
				CertPath: cert,
				KeyPath:  key,
			})
		}
	}
	rows.Close()

	host, _ := os.Hostname()
	var resp struct {
		Configured bool   `json:"configured"`
		SNICount   int    `json:"sni_count"`
		Error      string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.SecureMailTLS", &struct {
		Myhostname string  `json:"myhostname"`
		SNI        []entry `json:"sni"`
	}{Myhostname: host, SNI: sni}, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return &backupError{resp.Error}
	}
	return nil
}

// handleDomainSSLMail toggles "secure mail with this certificate" for a
// domain's active certificate and re-syncs the stack.
// handleDomainSSLMail, bir domain'in aktif sertifikası için "maili bu
// sertifikayla koru"yu açıp kapatır ve yığını yeniden senkronlar.
func (p *Panel) handleDomainSSLMail(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		var enabled int
		_ = p.db.GetDB().QueryRowContext(r.Context(), `
			SELECT COALESCE(MAX(secure_mail), 0) FROM ssl_certificates
			WHERE domain_id = ? AND status = 'active'`, domainID).Scan(&enabled)
		json.NewEncoder(w).Encode(map[string]any{"secure_mail": enabled == 1})

	case http.MethodPut:
		var req struct {
			SecureMail bool `json:"secure_mail"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		val := 0
		if req.SecureMail {
			val = 1
		}
		res, err := p.db.GetDB().ExecContext(r.Context(), `
			UPDATE ssl_certificates SET secure_mail = ?
			WHERE domain_id = ? AND status = 'active'`, val, domainID)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeClientError(w, http.StatusConflict, "the domain has no active certificate — install one first")
			return
		}
		if err := p.resyncMailTLS(r.Context()); err != nil {
			writeServerError(w, err)
			return
		}
		action := "ssl.mail.off"
		if req.SecureMail {
			action = "ssl.mail.on"
		}
		p.audit(r, action, "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
