package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
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
		WHERE sc.status = 'active' AND sc.expires_at < ?
		ORDER BY sc.id`, cutoff)
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
		if err := rows.Scan(
			&j.certID, &j.domainID, &j.name, &j.ctype, &j.autoRenew, &j.expiresAt,
		); err != nil {
			log.Printf("cert renewal scheduler scan: %v", err)
			rows.Close()
			return
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		log.Printf("cert renewal scheduler rows: %v", err)
		rows.Close()
		return
	}
	if err := rows.Close(); err != nil {
		log.Printf("cert renewal scheduler close: %v", err)
		return
	}

	for _, j := range jobs {
		switch {
		case j.ctype == "letsencrypt" && j.autoRenew:
			// Each renewal owns a separate hard deadline. A wedged agent,
			// certificate authority, web-server reload or mail reconcile must
			// never hold this domain's SSL lock (or stall every later job)
			// indefinitely.
			jobCtx, cancel := context.WithTimeout(ctx, sslMutationTimeout)
			p.renewLetsEncrypt(jobCtx, j.certID, j.domainID, j.name)
			cancel()
		default:
			// Uploaded certificates renew at their CA, not here. Flag once so
			// the UI can warn; do not overwrite a fresher status every run.
			// Yüklenmiş sertifikalar kendi CA'sında yenilenir, burada değil.
			// UI uyarabilsin diye bir kez işaretle.
			if _, err := p.db.GetDB().ExecContext(ctx, `
				UPDATE ssl_certificates SET renewal_status = 'expiring'
				WHERE id = ? AND (renewal_status IS NULL OR renewal_status = '')`, j.certID); err != nil {
				log.Printf("cert renewal %s: record expiring status: %v", j.name, err)
			}
		}
	}
}

// renewLetsEncrypt renews one certificate and pushes the result everywhere it
// is served: vhost and, when mail is secured with it, the mail SNI maps.
// renewLetsEncrypt bir sertifikayı yeniler ve sonucu sunulduğu her yere iter:
// vhost'a ve posta onunla korunuyorsa posta SNI map'lerine.
func (p *Panel) renewLetsEncrypt(ctx context.Context, certID, domainID int, domainName string) {
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		p.recordCertificateRenewalFailure(
			ctx, certID, domainName, now,
			"paired agent upgrade is incomplete: "+err.Error(),
		)
		return
	}
	var current struct {
		CertPath, KeyPath, ChainPath string
		LineageName, ACMEProviderID  string
		Issuer, Subject              string
		AutoRenew, SecureMail        bool
		SubscriptionID               int
	}
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT c.cert_path, c.key_path, COALESCE(c.chain_path, ''),
		       COALESCE(c.lineage_name, d.name),
		       COALESCE(c.acme_provider_id, ''),
		       COALESCE(c.issuer, ''), COALESCE(c.subject, ''),
		       COALESCE(c.auto_renew, true), COALESCE(c.secure_mail, false),
		       d.subscription_id
		FROM ssl_certificates c
		JOIN domains d ON d.id = c.domain_id
		WHERE c.id = ? AND c.domain_id = ? AND c.status = 'active'`,
		certID, domainID,
	).Scan(
		&current.CertPath, &current.KeyPath, &current.ChainPath,
		&current.LineageName, &current.ACMEProviderID,
		&current.Issuer, &current.Subject,
		&current.AutoRenew, &current.SecureMail,
		&current.SubscriptionID,
	); err != nil {
		log.Printf("cert renewal %s: active certificate changed before renewal: %v", domainName, err)
		return
	}

	currentInfo, err := p.inspectInstalledCertificate(ctx, current.CertPath, current.KeyPath)
	if err != nil {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"inspect current certificate before renewal: "+err.Error())
		return
	}
	mailName, err := mailCertificateHostname(domainName)
	if err != nil {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"derive mail certificate hostname: "+err.Error())
		return
	}
	preserveMailSAN := certificateCoversHostname(currentInfo.DNSNames, mailName)
	var challengeNames []string
	if preserveMailSAN {
		challengeNames = []string{mailName}
	}
	if err := p.applyVhostForDomainWithACMEChallengeNames(
		ctx,
		domainID,
		challengeNames,
	); err != nil {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"prepare renewal validation vhost: "+err.Error())
		return
	}
	validationVhostPrepared := true
	defer func() {
		if !validationVhostPrepared {
			return
		}
		restoreCtx, restoreCancel := sslCompensationContext()
		defer restoreCancel()
		if err := p.applyVhostForDomain(restoreCtx, domainID); err != nil {
			log.Printf("cert renewal %s: restore validation vhost: %v", domainName, err)
		}
	}()

	var resp transport.RenewCertResponse
	err = p.agentClient.CallContext(ctx, "Agent.RenewLetsEncryptCertificate",
		&transport.RenewCertRequest{
			ExpectedBuildCommit: strings.TrimSpace(buildCommit),
			Domain:              domainName,
			CurrentCertPath:     current.CertPath,
			LineageName:         current.LineageName,
			SubscriptionID:      current.SubscriptionID,
			DomainID:            domainID,
		}, &resp)
	if err != nil || !resp.Success {
		detail := resp.Error
		if err != nil {
			detail = err.Error()
		}
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now, detail)
		return
	}

	if strings.TrimSpace(resp.CertPath) == "" || strings.TrimSpace(resp.KeyPath) == "" {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"agent returned no immutable certificate paths")
		return
	}
	if strings.TrimSpace(resp.LineageName) == "" ||
		!strings.EqualFold(resp.LineageName, current.LineageName) {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"agent returned a different or empty certificate lineage identity")
		return
	}
	replacementLineage := !strings.EqualFold(
		strings.TrimSpace(resp.LineageName),
		strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domainName)), "."),
	)
	if err := validateIssuedCertificateLineage(
		domainName, domainID, replacementLineage, resp.LineageName,
	); err != nil {
		p.recordCertificateRenewalFailure(
			ctx, certID, domainName, now, err.Error(),
		)
		return
	}
	managedNames, err := p.managedSiteHostnames(ctx, domainID)
	if err != nil {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now, err.Error())
		return
	}
	info, err := p.inspectManagedCertificate(
		ctx,
		domainName,
		resp.CertPath,
		resp.KeyPath,
		resp.ChainPath,
	)
	if err != nil {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"read renewed certificate: "+err.Error())
		return
	}
	if !info.Valid {
		detail := strings.TrimSpace(info.Error)
		if detail == "" {
			detail = "renewed certificate is invalid"
		}
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now, detail)
		return
	}
	if !info.TrustChecked || !info.Trusted {
		detail := strings.TrimSpace(info.TrustError)
		if detail == "" {
			detail = "renewed certificate trust could not be verified"
		}
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now, detail)
		return
	}
	expectedNames := append([]string(nil), managedNames...)
	if preserveMailSAN {
		expectedNames = append(expectedNames, mailName)
	}
	if err := exactCertificateDNSNames(info.DNSNames, expectedNames); err != nil {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"renewed certificate DNS names: "+err.Error())
		return
	}
	if info.ExpiresAt.IsZero() {
		info.ExpiresAt = resp.ExpiresAt
	}
	if info.ExpiresAt.IsZero() {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"renewed certificate has no expiry")
		return
	}

	// Certbot can report success without replacing a certificate that is not
	// due yet. The immutable fingerprint path then stays the same; record a
	// successful check, but never claim that a new certificate was activated.
	if resp.CertPath == current.CertPath && resp.KeyPath == current.KeyPath {
		if _, err := p.db.GetDB().ExecContext(ctx, `
			UPDATE ssl_certificates
			SET expires_at = ?, last_renewal_attempt = ?, renewal_status = 'current',
			    updated_at = datetime('now')
			WHERE id = ? AND status = 'active'`,
			info.ExpiresAt.UTC().Format(time.RFC3339), now, certID,
		); err != nil {
			log.Printf("cert renewal %s: record current certificate: %v", domainName, err)
		}
		return
	}

	issuer := current.Issuer
	subject := strings.TrimSpace(info.Subject)
	if subject == "" {
		subject = current.Subject
	}
	activation, err := p.activateCertificate(ctx, domainID, certificateInstall{
		Type:           "letsencrypt",
		CertPath:       resp.CertPath,
		KeyPath:        resp.KeyPath,
		ChainPath:      resp.ChainPath,
		LineageName:    resp.LineageName,
		ACMEProviderID: current.ACMEProviderID,
		Issuer:         issuer,
		Subject:        subject,
		IssuedAt:       info.IssuedAt,
		ExpiresAt:      info.ExpiresAt,
		AutoRenew:      current.AutoRenew,
		SecureMail:     current.SecureMail,
	})
	if err != nil {
		p.recordCertificateRenewalFailure(ctx, certID, domainName, now,
			"activate renewed certificate in ledger: "+err.Error())
		return
	}
	// The new immutable snapshot is now the durable source of truth. Any
	// later vhost failure is recorded as activation_pending and retried.
	validationVhostPrepared = false

	persistPending := func(stage string, stageErr error, pending string, disableSite bool) {
		markErr := p.persistCertificateRenewalPending(
			domainID, activation.NewCertID, now, pending, disableSite,
		)
		log.Printf(
			"cert renewal %s: %s failed: %v; durable pending state: %v",
			domainName, stage, stageErr, markErr,
		)
	}

	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		persistPending("web server activation", err, sslPendingActivation, true)
		return
	}
	if err := p.syncCertificateDependents(ctx, domainID); err != nil {
		persistPending("mail TLS synchronization", err, sslPendingDependents, false)
		return
	}
	if err := p.completeCertificateRenewal(
		ctx, activation.NewCertID, now,
	); err != nil {
		log.Printf("cert renewal %s: record success: %v", domainName, err)
	}
	log.Printf("cert renewal %s: renewed until %s", domainName, info.ExpiresAt.Format("2006-01-02"))
}

// completeCertificateRenewal is the renewal success-path outbox clear. The
// caller reaches it only after nginx and all certificate dependents accepted
// the new immutable snapshot. Use a detached, bounded context so a deadline
// expiring immediately after those external writes cannot leave the database
// reporting a false failure; if this write itself fails, activation_pending
// remains durable and startup/retry safely reapplies the idempotent stages.
func (p *Panel) completeCertificateRenewal(
	_ context.Context,
	certID int64,
	at string,
) error {
	ctx, cancel := sslCompensationContext()
	defer cancel()

	_, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE ssl_certificates
		SET last_renewal_attempt = ?, renewal_status = 'renewed',
		    updated_at = datetime('now')
		WHERE id = ? AND status = 'active'`, at, certID)
	return err
}

// persistCertificateRenewalPending keeps the newly issued immutable snapshot
// as the active ledger entry when a later activation stage fails. Certbot has
// already advanced its lineage at this point, so restoring the old database
// row would make the visible state disagree with the next renewal source.
// The pending state is intentionally written with a detached, bounded context
// so a caller cancellation cannot erase the user's visible Retry action.
func (p *Panel) persistCertificateRenewalPending(
	domainID int,
	certID int64,
	at string,
	pending string,
	disableSite bool,
) error {
	ctx, cancel := sslCompensationContext()
	defer cancel()

	if err := p.markCertificatePending(ctx, domainID, pending, disableSite); err != nil {
		return err
	}
	_, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE ssl_certificates
		SET last_renewal_attempt = ?, updated_at = datetime('now')
		WHERE id = ? AND status = 'active'`, at, certID)
	return err
}

func (p *Panel) recordCertificateRenewalFailure(
	_ context.Context,
	certID int,
	domainName string,
	at string,
	detail string,
) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "renewal failed"
	}
	if len(detail) > 500 {
		detail = detail[:500]
	}
	log.Printf("cert renewal %s: %s", domainName, detail)
	// The failure is commonly caused by the job context expiring. Persist it
	// through a detached, bounded context so the UI does not keep claiming the
	// previous renewal state after a timeout.
	writeCtx, cancel := sslCompensationContext()
	defer cancel()
	if _, err := p.db.GetDB().ExecContext(writeCtx, `
		UPDATE ssl_certificates
		SET last_renewal_attempt = ?, renewal_status = 'failed',
		    updated_at = datetime('now')
		WHERE id = ? AND status = 'active'`, at, certID,
	); err != nil {
		log.Printf("cert renewal %s: record failure: %v", domainName, err)
	}
}
