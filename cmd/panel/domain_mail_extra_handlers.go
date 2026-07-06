package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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
func (p *Panel) handleMailClientSetup(w http.ResponseWriter, domain string) {
	w.Header().Set("Content-Type", "application/json")
	host := "mail." + domain
	json.NewEncoder(w).Encode(map[string]any{
		"mail_host": host,
		"imap": map[string]any{
			"host": host, "port": 993, "security": "SSL/TLS",
		},
		"pop3": map[string]any{
			"host": host, "port": 995, "security": "SSL/TLS",
		},
		"smtp": map[string]any{
			"host": host, "port": 587, "security": "STARTTLS", "auth_required": true,
		},
		// The username is always the full email address; that is the one
		// detail people get wrong most.
		// Kullanıcı adı her zaman tam e-posta adresidir; insanların en çok
		// yanlış yaptığı ayrıntı budur.
		"username_is_full_email": true,
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
			`SELECT destination FROM mail_catch_all WHERE domain_id = ?`, domainID).Scan(&dest)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"enabled": false, "destination": ""})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"enabled": true, "destination": dest})

	case http.MethodPut:
		var req struct {
			Destination string `json:"destination"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		dest := strings.TrimSpace(req.Destination)
		if !strings.Contains(dest, "@") || strings.ContainsAny(dest, " \n\t") {
			writeClientError(w, http.StatusBadRequest, "destination must be a valid email address")
			return
		}
		if _, err := pool.ExecContext(r.Context(), `
			INSERT INTO mail_catch_all (domain_id, destination) VALUES (?, ?)
			ON CONFLICT(domain_id) DO UPDATE SET destination = excluded.destination`,
			domainID, dest); err != nil {
			writeServerError(w, err)
			return
		}
		p.pushForwardingsToAgent(r.Context())
		json.NewEncoder(w).Encode(map[string]any{"enabled": true, "destination": dest})

	case http.MethodDelete:
		if _, err := pool.ExecContext(r.Context(),
			`DELETE FROM mail_catch_all WHERE domain_id = ?`, domainID); err != nil {
			writeServerError(w, err)
			return
		}
		p.pushForwardingsToAgent(r.Context())
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

	var resp struct {
		IP      string `json:"ip"`
		Results []struct {
			Zone   string `json:"zone"`
			Listed bool   `json:"listed"`
			Detail string `json:"detail,omitempty"`
		} `json:"results"`
		Error string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.CheckRBL", &struct{}{}, &resp); err != nil {
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
