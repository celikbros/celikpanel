package main

import (
	"context"
	"log"
	"time"
)

// Automatic certificate renewal — what makes "Will be automatically renewed"
// true instead of a label. A daily loop finds active auto-renew certificates
// within 30 days of expiry: Let's Encrypt ones are renewed through the agent
// and re-applied to the vhost and the mail SNI maps; custom (uploaded) ones
// cannot be renewed by us, so they are flagged for the UI instead of silently
// dying.
//
// Otomatik sertifika yenileme — "Otomatik yenilenecek"i etiket olmaktan
// çıkarıp gerçek yapan şey. Günlük bir döngü, bitimine 30 gün kalmış aktif
// otomatik-yenileme sertifikalarını bulur: Let's Encrypt olanlar agent
// üzerinden yenilenir ve vhost ile posta SNI map'lerine yeniden uygulanır;
// custom (yüklenmiş) olanları biz yenileyemeyiz — sessizce ölmek yerine UI
// için işaretlenir.

const certRenewWindow = 30 * 24 * time.Hour

func (p *Panel) startCertRenewalScheduler() {
	go func() {
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		p.runDueCertRenewals()
		for range ticker.C {
			p.runDueCertRenewals()
		}
	}()
}

func (p *Panel) runDueCertRenewals() {
	ctx := context.Background()
	cutoff := time.Now().Add(certRenewWindow).UTC().Format(time.RFC3339)
	// No auto_renew filter here: the expiring flag must reach certificates
	// we cannot renew too. auto_renew only gates the actual LE renewal below.
	// Burada auto_renew süzgeci yok: yakında-doluyor işareti yenileyemediğimiz
	// sertifikalara da ulaşmalı. auto_renew yalnız aşağıdaki LE yenilemesini
	// kapılar.
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT sc.id, sc.domain_id, d.name, sc.type, sc.auto_renew, sc.expires_at
		FROM ssl_certificates sc JOIN domains d ON d.id = sc.domain_id
		WHERE sc.status = 'active' AND sc.expires_at < ?`, cutoff)
	if err != nil {
		log.Printf("cert renewal scheduler: %v", err)
		return
	}
	type due struct {
		certID, domainID       int
		name, ctype, expiresAt string
		autoRenew              bool
	}
	var jobs []due
	for rows.Next() {
		var j due
		if rows.Scan(&j.certID, &j.domainID, &j.name, &j.ctype, &j.autoRenew, &j.expiresAt) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, j := range jobs {
		switch {
		case j.ctype == "letsencrypt" && j.autoRenew:
			p.renewLetsEncrypt(ctx, j.certID, j.domainID, j.name)
		default:
			// Uploaded certificates renew at their CA, not here. Flag once so
			// the UI can warn; do not overwrite a fresher status every run.
			// Yüklenmiş sertifikalar kendi CA'sında yenilenir, burada değil.
			// UI uyarabilsin diye bir kez işaretle.
			p.db.GetDB().ExecContext(ctx, `
				UPDATE ssl_certificates SET renewal_status = 'expiring'
				WHERE id = ? AND (renewal_status IS NULL OR renewal_status = '')`, j.certID)
		}
	}
}

// renewLetsEncrypt renews one certificate and pushes the result everywhere it
// is served: vhost and, when mail is secured with it, the mail SNI maps.
// renewLetsEncrypt bir sertifikayı yeniler ve sonucu sunulduğu her yere iter:
// vhost'a ve posta onunla korunuyorsa posta SNI map'lerine.
func (p *Panel) renewLetsEncrypt(ctx context.Context, certID, domainID int, domainName string) {
	now := time.Now().UTC().Format(time.RFC3339)
	var resp struct {
		Success   bool      `json:"success"`
		ExpiresAt time.Time `json:"expires_at"`
		Error     string    `json:"error,omitempty"`
	}
	err := p.agentClient.Call("Agent.RenewLetsEncryptCertificate",
		&struct {
			Domain string `json:"domain"`
		}{Domain: domainName}, &resp)
	if err != nil || !resp.Success {
		detail := resp.Error
		if err != nil {
			detail = err.Error()
		}
		if len(detail) > 200 {
			detail = detail[:200]
		}
		log.Printf("cert renewal %s: %s", domainName, detail)
		p.db.GetDB().ExecContext(ctx, `
			UPDATE ssl_certificates SET last_renewal_attempt = ?, renewal_status = ?
			WHERE id = ?`, now, "failed: "+detail, certID)
		return
	}

	p.db.GetDB().ExecContext(ctx, `
		UPDATE ssl_certificates
		SET expires_at = ?, last_renewal_attempt = ?, renewal_status = 'renewed'
		WHERE id = ?`, resp.ExpiresAt.UTC().Format(time.RFC3339), now, certID)

	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		log.Printf("cert renewal %s: vhost apply: %v", domainName, err)
	}
	_ = p.resyncMailTLS(ctx)
	log.Printf("cert renewal %s: renewed until %s", domainName, resp.ExpiresAt.Format("2006-01-02"))
}
