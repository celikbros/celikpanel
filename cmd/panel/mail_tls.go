package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/transport"
)

var mailTLSSyncMu sync.Mutex

type mailTLSReconcileMode uint8

const (
	mailTLSReconcileNone mailTLSReconcileMode = iota
	mailTLSReconcileCleanup
	mailTLSReconcileStrict
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

// resyncMailTLS pushes active secure_mail certificates only for DNS names
// each installed certificate actually covers.
// resyncMailTLS, aktif secure_mail sertifikalarını yalnızca sertifikanın
// gerçekten kapsadığı DNS adları için agent'a iter.
func (p *Panel) resyncMailTLS(ctx context.Context) error {
	return p.resyncMailTLSForTarget(ctx, 0)
}

// resyncMailTLSForTarget preserves the agent's full-state SNI contract while
// making certificate validity tenant-scoped. The target domain is strict: its
// broken certificate aborts its own operation. Broken certificates belonging
// to other domains are omitted from the safe snapshot instead of blocking the
// target; omission also removes an unusable certificate from the live SNI map.
//
// resyncMailTLSForTarget, agent'ın tam-durum SNI sözleşmesini korurken
// sertifika geçerliliğini tenant kapsamına alır. Hedef domain katıdır: bozuk
// sertifikası kendi işlemini durdurur. Başka domain'lerin bozuk sertifikaları
// hedefi durdurmak yerine güvenli snapshot'tan çıkarılır; bu çıkarma,
// kullanılamaz sertifikayı canlı SNI haritasından da düşürür.
func (p *Panel) resyncMailTLSForTarget(ctx context.Context, strictDomainID int) error {
	// The SNI request is a full-state snapshot. Serialize snapshot + RPC so a
	// slower stale push for one domain cannot erase another domain's change.
	mailTLSSyncMu.Lock()
	defer mailTLSSyncMu.Unlock()

	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT sc.domain_id, d.name, sc.cert_path, sc.key_path
		FROM ssl_certificates sc JOIN domains d ON d.id = sc.domain_id
		WHERE sc.status = 'active' AND sc.secure_mail = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var sni []transport.MailSNIEntry
	strictTargetSeen := false
	for rows.Next() {
		var domainID int
		var name, cert, key string
		if err := rows.Scan(&domainID, &name, &cert, &key); err != nil {
			return err
		}
		isStrictTarget := strictDomainID > 0 && domainID == strictDomainID
		if isStrictTarget {
			strictTargetSeen = true
		}
		if strings.TrimSpace(cert) == "" || strings.TrimSpace(key) == "" {
			if isStrictTarget {
				return fmt.Errorf("mail TLS certificate for %s has an empty certificate or private-key path", name)
			}
			log.Printf("mail TLS snapshot: omit unrelated domain %s (%d): empty certificate or private-key path",
				name, domainID)
			continue
		}
		info, infoErr := p.inspectInstalledCertificate(ctx, cert, key)
		if infoErr != nil {
			// A transport failure means the panel could not safely inspect the
			// full snapshot at all. Do not publish a destructive partial map.
			return fmt.Errorf("inspect mail TLS certificate for %s: %w", name, infoErr)
		}
		if !info.Valid {
			if isStrictTarget {
				return fmt.Errorf("mail TLS certificate for %s is invalid: %s", name, info.Error)
			}
			log.Printf("mail TLS snapshot: omit unrelated invalid certificate for %s (%d): %s",
				name, domainID, strings.TrimSpace(info.Error))
			continue
		}
		if !info.TrustChecked || !info.Trusted {
			if isStrictTarget {
				return fmt.Errorf("mail TLS certificate for %s is not trusted", name)
			}
			log.Printf("mail TLS snapshot: omit unrelated untrusted certificate for %s (%d)", name, domainID)
			continue
		}
		mailName := "mail." + name
		if !certificateCoversHostname(info.DNSNames, mailName) {
			if isStrictTarget {
				return fmt.Errorf("mail TLS certificate does not cover %s", mailName)
			}
			log.Printf("mail TLS snapshot: omit unrelated certificate for %s (%d): it does not cover %s",
				name, domainID, mailName)
			continue
		}
		names := []string{mailName}
		if certificateCoversHostname(info.DNSNames, name) {
			names = append(names, name)
		}
		sni = append(sni, transport.MailSNIEntry{
			Names:    names,
			CertPath: cert,
			KeyPath:  key,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if strictDomainID > 0 && !strictTargetSeen {
		return fmt.Errorf("target domain %d is not active in the secure-mail ledger", strictDomainID)
	}

	host, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read mail server hostname: %w", err)
	}
	var resp transport.SecureMailTLSResponse
	if err := p.callAgentContext(ctx, "Agent.SecureMailTLS", &transport.SecureMailTLSRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		Myhostname:          host, SNI: sni}, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return &backupError{resp.Error}
	}
	if !resp.Configured || resp.SNICount != len(sni) {
		return fmt.Errorf(
			"mail TLS agent applied %d of %d SNI entries",
			resp.SNICount, len(sni),
		)
	}
	return nil
}

// mailTLSModeForDomain decides whether a certificate mutation can affect the
// mail SNI map. An active secure_mail certificate requires strict validation.
// A previously-secured inactive certificate requires a full-state cleanup
// after detach or replacement without mail coverage. Domains that never
// participated in secure_mail do not trigger a global mail reconcile.
func (p *Panel) mailTLSModeForDomain(ctx context.Context, domainID int) (mailTLSReconcileMode, error) {
	var activeSecure, inactiveSecure int
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT
			COALESCE(MAX(CASE WHEN status = 'active' AND secure_mail = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(MAX(CASE WHEN status <> 'active' AND secure_mail = 1 THEN 1 ELSE 0 END), 0)
		FROM ssl_certificates
		WHERE domain_id = ?`, domainID).Scan(&activeSecure, &inactiveSecure); err != nil {
		return mailTLSReconcileNone, err
	}
	switch {
	case activeSecure == 1:
		return mailTLSReconcileStrict, nil
	case inactiveSecure == 1:
		return mailTLSReconcileCleanup, nil
	default:
		return mailTLSReconcileNone, nil
	}
}

func (p *Panel) syncCertificateDependents(ctx context.Context, domainID int) error {
	mode, err := p.mailTLSModeForDomain(ctx, domainID)
	if err != nil {
		return fmt.Errorf("read mail TLS participation: %w", err)
	}
	switch mode {
	case mailTLSReconcileStrict:
		if err := p.resyncMailTLSForTarget(ctx, domainID); err != nil {
			return fmt.Errorf("mail TLS sync: %w", err)
		}
	case mailTLSReconcileCleanup:
		if err := p.resyncMailTLS(ctx); err != nil {
			return fmt.Errorf("mail TLS cleanup: %w", err)
		}
	}
	if err := p.refreshTLSARecords(ctx, domainID); err != nil {
		return fmt.Errorf("TLSA sync: %w", err)
	}
	return nil
}

func (p *Panel) resyncMailTLSForState(ctx context.Context, domainID int, secureMail bool) error {
	if secureMail {
		return p.resyncMailTLSForTarget(ctx, domainID)
	}
	return p.resyncMailTLS(ctx)
}

type domainMailTLSRemoval struct {
	DomainID       int
	CertificateIDs []int64
}

// prepareDomainMailTLSRemoval removes a soon-to-be-deleted domain from the
// full mail SNI snapshot before its certificate rows and lineage disappear.
// If publication fails, the ledger and previous snapshot are restored.
func (p *Panel) prepareDomainMailTLSRemoval(
	ctx context.Context,
	domainID int,
) (*domainMailTLSRemoval, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT id FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active' AND secure_mail = 1
		ORDER BY id`, domainID)
	if err != nil {
		return nil, err
	}
	var certificateIDs []int64
	for rows.Next() {
		var certificateID int64
		if err := rows.Scan(&certificateID); err != nil {
			rows.Close()
			return nil, err
		}
		certificateIDs = append(certificateIDs, certificateID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(certificateIDs) == 0 {
		return &domainMailTLSRemoval{DomainID: domainID}, nil
	}

	removal := &domainMailTLSRemoval{
		DomainID:       domainID,
		CertificateIDs: certificateIDs,
	}
	if _, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE ssl_certificates SET secure_mail = 0, updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active' AND secure_mail = 1`,
		domainID,
	); err != nil {
		return nil, err
	}
	if err := p.resyncMailTLS(ctx); err != nil {
		rollbackCtx, cancel := sslCompensationContext()
		defer cancel()
		rollbackErr := p.rollbackDomainMailTLSRemoval(rollbackCtx, removal)
		if rollbackErr != nil {
			return nil, fmt.Errorf(
				"remove domain from mail TLS: %v; rollback failed: %w",
				err,
				rollbackErr,
			)
		}
		return nil, fmt.Errorf("remove domain from mail TLS: %w", err)
	}
	return removal, nil
}

func (p *Panel) rollbackDomainMailTLSRemoval(
	ctx context.Context,
	removal *domainMailTLSRemoval,
) error {
	if removal == nil || len(removal.CertificateIDs) == 0 {
		return nil
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, certificateID := range removal.CertificateIDs {
		result, err := tx.ExecContext(ctx, `
			UPDATE ssl_certificates SET secure_mail = 1, updated_at = datetime('now')
			WHERE id = ? AND domain_id = ? AND status = 'active'`,
			certificateID,
			removal.DomainID,
		)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return fmt.Errorf("active secure-mail certificate %d is no longer available", certificateID)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return p.resyncMailTLSForTarget(ctx, removal.DomainID)
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
		unlock := lockDomainSSLOperation(domainID)
		defer unlock()
		var req struct {
			SecureMail bool `json:"secure_mail"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		var (
			domainName      string
			certPath        string
			previousEnabled int
		)
		if err := p.db.GetDB().QueryRowContext(r.Context(), `
			SELECT d.name, sc.cert_path, COALESCE(sc.secure_mail, 0)
			FROM ssl_certificates sc JOIN domains d ON d.id = sc.domain_id
			WHERE sc.domain_id = ? AND sc.status = 'active'
			ORDER BY sc.created_at DESC LIMIT 1`, domainID).
			Scan(&domainName, &certPath, &previousEnabled); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeClientError(w, http.StatusConflict, "the domain has no active certificate")
			} else {
				writeServerError(w, err)
			}
			return
		}
		if req.SecureMail {
			managedNames, err := p.managedSiteHostnames(r.Context(), domainID)
			if err != nil {
				writeServerError(w, err)
				return
			}
			runtime, err := p.loadCertificateRuntimeStatus(r.Context(), domainID, managedNames)
			if err != nil {
				writeServerError(w, err)
				return
			}
			if !runtime.Usable {
				writeClientError(w, http.StatusConflict,
					"mail TLS requires an activated, trusted certificate covering every managed website hostname")
				return
			}
			mailName := "mail." + domainName
			if !certificateCoversHostname(runtime.Info.DNSNames, mailName) {
				writeClientError(w, http.StatusConflict,
					"install a certificate that covers "+mailName+" before securing mail")
				return
			}
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
		if err := p.resyncMailTLSForState(r.Context(), domainID, req.SecureMail); err != nil {
			rollbackCtx, cancel := sslCompensationContext()
			defer cancel()
			_, rollbackErr := p.db.GetDB().ExecContext(rollbackCtx, `
				UPDATE ssl_certificates SET secure_mail = ?
				WHERE domain_id = ? AND status = 'active'`, previousEnabled, domainID)
			if rollbackErr == nil {
				rollbackErr = p.resyncMailTLSForState(rollbackCtx, domainID, previousEnabled == 1)
			}
			if rollbackErr != nil {
				writeServerError(w, fmt.Errorf("mail TLS apply failed: %v; rollback failed: %w", err, rollbackErr))
				return
			}
			writeServerError(w, err)
			return
		}
		// DANE follows the toggle: publish TLSA when securing, drop when not.
		// DANE anahtarı izler: korurken TLSA yayımla, korumuyorken düşür.
		if err := p.refreshTLSARecords(r.Context(), domainID); err != nil {
			rollbackCtx, cancel := sslCompensationContext()
			defer cancel()
			_, rollbackErr := p.db.GetDB().ExecContext(rollbackCtx, `
				UPDATE ssl_certificates SET secure_mail = ?
				WHERE domain_id = ? AND status = 'active'`, previousEnabled, domainID)
			if rollbackErr == nil {
				rollbackErr = p.resyncMailTLSForState(rollbackCtx, domainID, previousEnabled == 1)
			}
			if rollbackErr == nil {
				rollbackErr = p.refreshTLSARecords(rollbackCtx, domainID)
			}
			if rollbackErr != nil {
				writeServerError(w, fmt.Errorf(
					"TLSA publish failed: %v; secure-mail rollback failed: %w",
					err, rollbackErr,
				))
				return
			}
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
