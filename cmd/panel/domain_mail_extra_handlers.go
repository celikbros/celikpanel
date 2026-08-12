package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Second-round mail features: client setup info (how to configure Thunderbird
// / Outlook / phones), a per-domain catch-all address, and an RBL blocklist
// check for the server's sending IP. All read real state — no invented data.
//
// İkinci tur mail özellikleri: istemci kurulum bilgisi (Thunderbird / Outlook
// / telefon nasıl ayarlanır), domain başına catch-all adresi ve sunucunun
// gönderim IP'si için RBL kara-liste kontrolü. Hepsi gerçek durum okur —
// uydurma veri yok.

// handleMailClientSetup returns the IMAP/POP3/SMTP connection settings a user
// needs to configure a mail client for this domain. Static, cheap, honest:
// the mail host is mail.<domain>, the standard submission/IMAPS/POP3S ports.
// handleMailClientSetup, kullanıcının bu domain için bir posta istemcisi
// ayarlamak üzere gereksindiği IMAP/POP3/SMTP bağlantı ayarlarını döndürür.
type mailClientProtocol struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Security     string `json:"security"`
	AuthRequired bool   `json:"auth_required,omitempty"`
}

type mailClientSetupResponse struct {
	MailHost            string             `json:"mail_host"`
	IMAP                mailClientProtocol `json:"imap"`
	POP3                mailClientProtocol `json:"pop3"`
	SMTP                mailClientProtocol `json:"smtp"`
	UsernameIsFullEmail bool               `json:"username_is_full_email"`
	WebmailAvailable    bool               `json:"webmail_available"`
	WebmailURL          string             `json:"webmail_url"`
}

func (p *Panel) webmailAvailable(ctx context.Context) bool {
	probe := p.webmailReadinessProbe
	if probe == nil {
		probe = probeWebmailReadiness
	}
	return probe(ctx)
}

func (p *Panel) handleMailClientSetup(w http.ResponseWriter, r *http.Request, domain string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	host := "mail." + domain
	available := p.webmailAvailable(r.Context())
	webmailURL := ""
	if available {
		webmailURL = webmailPublicPath
	}
	json.NewEncoder(w).Encode(mailClientSetupResponse{
		MailHost: host,
		IMAP: mailClientProtocol{
			Host: host, Port: 993, Security: "SSL/TLS",
		},
		POP3: mailClientProtocol{
			Host: host, Port: 995, Security: "SSL/TLS",
		},
		SMTP: mailClientProtocol{
			Host: host, Port: 587, Security: "STARTTLS", AuthRequired: true,
		},
		// The username is always the full email address; that is the one
		// detail people get wrong most.
		// Kullanıcı adı her zaman tam e-posta adresidir; insanların en çok
		// yanlış yaptığı ayrıntı budur.
		UsernameIsFullEmail: true,
		WebmailAvailable:    available,
		WebmailURL:          webmailURL,
	})
}

// handleMailCatchAll handles GET/PUT/DELETE for a domain's catch-all address.
// handleMailCatchAll, bir domain'in catch-all adresi için GET/PUT/DELETE'i
// karşılar.
func (p *Panel) handleMailCatchAll(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	pool := p.db.GetDB()

	switch r.Method {
	case http.MethodGet:
		var dest string
		err := pool.QueryRowContext(r.Context(),
			"SELECT destination FROM mail_catch_all WHERE domain_id = ?", domainID).Scan(&dest)
		if errors.Is(err, sql.ErrNoRows) {
			json.NewEncoder(w).Encode(map[string]any{"enabled": false, "destination": ""})
			return
		}
		if err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"enabled": true, "destination": dest})

	case http.MethodPut:
		var req struct {
			Destination string `json:"destination"`
		}
		if err := decodeStrictJSON(w, r, &req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		domain, err := p.domainNameByIDStrict(r.Context(), domainID)
		if err != nil {
			writeServerError(w, err)
			return
		}
		dest, err := transport.CanonicalMailAddress(req.Destination)
		if err != nil {
			writeClientError(w, http.StatusBadRequest, err.Error())
			return
		}
		p.mailMutationMu.Lock()
		err = p.mutateForwardings(r.Context(), domainID, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(r.Context(),
				"INSERT INTO mail_catch_all (domain_id, destination) VALUES (?, ?) "+
					"ON CONFLICT(domain_id) DO UPDATE SET destination = excluded.destination",
				domainID, dest)
			return err
		})
		p.mailMutationMu.Unlock()
		if err != nil {
			writeMailDomainMutationError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"enabled": true, "destination": dest, "source": "@" + domain,
		})

	case http.MethodDelete:
		p.mailMutationMu.Lock()
		err := p.mutateForwardings(r.Context(), domainID, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(r.Context(),
				"DELETE FROM mail_catch_all WHERE domain_id = ?", domainID)
			return err
		})
		p.mailMutationMu.Unlock()
		if err != nil {
			writeMailDomainMutationError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"enabled": false})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMailRBL asks the agent whether this server's sending IP is listed on
// the common DNS blocklists — the single most useful "why is my mail bouncing"
// signal. Admin-scoped work behind a per-domain page, but the answer is
// server-wide (one IP sends for every domain).
// handleMailRBL, bu sunucunun gönderim IP'sinin yaygın DNS kara-listelerinde
// olup olmadığını agent'a sorar — "postam neden geri dönüyor"un en yararlı
// tek sinyali.
func (p *Panel) handleMailRBL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var resp transport.CheckRBLResponse
	if err := p.callAgent("Agent.CheckRBL", &transport.Empty{}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	json.NewEncoder(w).Encode(resp)
}

// domainNameByID resolves a panel domain id to its name (small helper for the
// mail sub-routes that key off the name).
// domainNameByID, bir panel domain id'sini adına çözer.
func (p *Panel) domainNameByID(ctx context.Context, domainID int) string {
	var name string
	_ = p.db.GetDB().QueryRowContext(ctx, `SELECT name FROM domains WHERE id = ?`, domainID).Scan(&name)
	return name
}

// handleMailConfigure wires Postfix + Dovecot to the panel's virtual
// mailboxes — the one-shot "start delivering mail" action, mail's counterpart
// to /api/v1/pdns/enable. Admin-only via the /api/v1/mail/ prefix.
// handleMailConfigure, Postfix + Dovecot'u panelin sanal posta kutularına
// bağlar — tek seferlik "posta teslimine başla" eylemi; /api/v1/pdns/enable'ın
// mail karşılığı. /api/v1/mail/ öneki üzerinden yalnız admin.
func (p *Panel) handleMailConfigure(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var resp transport.ConfigureMailStackResponse
	err := p.withStandaloneAgentMutation(r.Context(), "mail_configure", "mail-stack", "", func(callCtx context.Context, binding agentMutationBinding) error {
		request := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
		if err := p.callAgentContext(callCtx, "Agent.ConfigureMailStack", &request, &resp); err != nil {
			return err
		}
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		return nil
	})
	if err != nil {
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		writeServerError(w, err)
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	// Receiving without sending is half a mail server: wire submission too.
	// Göndermesiz almak yarım posta sunucusudur: gönderimi de bağla.
	var sub transport.ConfigureMailSubmissionResponse
	err = p.withStandaloneAgentMutation(r.Context(), "mail_submission_configure", "postfix", "", func(callCtx context.Context, binding agentMutationBinding) error {
		request := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
		if err := p.callAgentContext(callCtx, "Agent.ConfigureMailSubmission", &request, &sub); err != nil {
			return err
		}
		if sub.Error != "" {
			return errors.New(sub.Error)
		}
		return nil
	})
	if err != nil {
		if sub.Error != "" {
			writeClientError(w, http.StatusConflict, sub.Error)
			return
		}
		writeServerError(w, err)
		return
	}
	if sub.Error != "" {
		writeClientError(w, http.StatusConflict, sub.Error)
		return
	}
	// Sign what leaves: DKIM records without signatures fail at receivers.
	// Çıkanı imzala: imzasız DKIM kaydı alıcıda "kalır" demektir.
	var sign transport.ConfigureDKIMSigningResponse
	err = p.withStandaloneAgentMutation(r.Context(), "dkim_signing_configure", "opendkim", "", func(callCtx context.Context, binding agentMutationBinding) error {
		request := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
		if err := p.callAgentContext(callCtx, "Agent.ConfigureDKIMSigning", &request, &sign); err != nil {
			return err
		}
		if sign.Error != "" {
			return errors.New(sign.Error)
		}
		return nil
	})
	if err != nil {
		if sign.Error != "" {
			writeClientError(w, http.StatusConflict, sign.Error)
			return
		}
		writeServerError(w, err)
		return
	}
	if sign.Error != "" {
		writeClientError(w, http.StatusConflict, sign.Error)
		return
	}
	p.audit(r, "mail.configure", "", 0)
	json.NewEncoder(w).Encode(map[string]any{"success": true, "detail": resp.Detail + "; " + sub.Detail + "; " + sign.Detail})
}

// validMailLocalPart guards the local part of an email address: non-empty,
// reasonable length, and the conservative charset real mailbox names use.
// validMailLocalPart, bir e-posta adresinin yerel kısmını korur: boş değil,
// makul uzunlukta ve gerçek posta kutusu adlarının kullandığı muhafazakâr
// karakter seti.
func validMailLocalPart(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum && r != '.' && r != '_' && r != '-' && r != '+' {
			return false
		}
	}
	return true
}
