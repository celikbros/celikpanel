package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

const (
	sslPendingActivation = "activation_pending"
	sslPendingDependents = "dependents_pending"
)

type certificateRuntimeStatus struct {
	CertPath          string
	KeyPath           string
	Type              string
	RenewalStatus     string
	Activated         bool
	SitePathsMatch    bool
	ActivationPending bool
	DependentsPending bool
	CoversManaged     bool
	Info              installedCertificateInfo
	Usable            bool
}

func acmeProviderIDForIssuer(issuer string) string {
	for _, provider := range core.ACMEProviders {
		if strings.EqualFold(strings.TrimSpace(provider.Name), strings.TrimSpace(issuer)) {
			return provider.ID
		}
	}
	return ""
}

func (p *Panel) loadCertificateRuntimeStatus(
	ctx context.Context,
	domainID int,
	managedNames []string,
) (certificateRuntimeStatus, error) {
	var status certificateRuntimeStatus
	var (
		siteEnabled  bool
		siteCertPath sql.NullString
		siteKeyPath  sql.NullString
	)
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT sc.cert_path, sc.key_path, sc.type,
		       COALESCE(sc.renewal_status, ''), COALESCE(s.ssl_enabled, false),
		       s.ssl_cert_path, s.ssl_key_path
		FROM ssl_certificates sc
		JOIN sites s ON s.domain_id = sc.domain_id
		WHERE sc.domain_id = ? AND sc.status = 'active'
		ORDER BY sc.created_at DESC LIMIT 1`, domainID).
		Scan(&status.CertPath, &status.KeyPath, &status.Type,
			&status.RenewalStatus, &siteEnabled, &siteCertPath, &siteKeyPath)
	if err != nil {
		return status, err
	}

	status.SitePathsMatch =
		siteCertPath.Valid && siteKeyPath.Valid &&
			siteCertPath.String == status.CertPath &&
			siteKeyPath.String == status.KeyPath
	status.Activated = siteEnabled && status.SitePathsMatch

	status.ActivationPending = status.RenewalStatus == sslPendingActivation
	status.DependentsPending = status.RenewalStatus == sslPendingDependents
	info, err := p.inspectInstalledCertificate(ctx, status.CertPath, status.KeyPath)
	if err != nil {
		// Runtime read/parse failures are certificate state, not a panel
		// database failure. Keep the reason for the UI and make the
		// certificate unusable for redirect, HSTS and mail TLS.
		status.Info.Error = err.Error()
		return status, nil
	}
	status.Info = info
	status.CoversManaged = true
	for _, name := range managedNames {
		if !certificateCoversHostname(info.DNSNames, name) {
			status.CoversManaged = false
			break
		}
	}
	status.Usable = status.Activated &&
		status.SitePathsMatch &&
		!status.ActivationPending &&
		info.Valid &&
		info.TrustChecked &&
		info.Trusted &&
		status.CoversManaged
	return status, nil
}

func (p *Panel) markCertificatePending(
	ctx context.Context,
	domainID int,
	pending string,
	disableSite bool,
) error {
	if pending != sslPendingActivation && pending != sslPendingDependents {
		return fmt.Errorf("invalid SSL pending state %q", pending)
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE ssl_certificates
		SET renewal_status = ?, updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active'`, pending, domainID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return errNoActiveCertificate
	}
	if disableSite {
		if _, err := tx.ExecContext(ctx, `
			UPDATE sites
			SET ssl_enabled = false, updated_at = datetime('now')
			WHERE domain_id = ?`, domainID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// markCertificatePendingDetached records post-commit compensation even when
// the request/job context has already expired. The activation transaction
// itself always inserts activation_pending first; this helper only refines
// that durable outbox state and, after a failed vhost apply, prevents a site
// row from claiming that the new certificate is already being served.
func (p *Panel) markCertificatePendingDetached(
	_ context.Context,
	domainID int,
	pending string,
	disableSite bool,
) error {
	ctx, cancel := sslCompensationContext()
	defer cancel()
	return p.markCertificatePending(ctx, domainID, pending, disableSite)
}

func (p *Panel) clearCertificatePending(ctx context.Context, domainID int) error {
	_, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE ssl_certificates
		SET renewal_status = '', updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active'
		  AND renewal_status IN (?, ?)`,
		domainID, sslPendingActivation, sslPendingDependents)
	return err
}

// clearCertificatePendingDetached is the only success-path eraser for the
// activation outbox. It is called only after both the web vhost and every
// certificate dependent have been applied successfully.
func (p *Panel) clearCertificatePendingDetached(
	_ context.Context,
	domainID int,
) error {
	ctx, cancel := sslCompensationContext()
	defer cancel()
	return p.clearCertificatePending(ctx, domainID)
}

func (p *Panel) enableActiveCertificateForRetry(ctx context.Context, domainID int) error {
	var certType, certPath, keyPath string
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT type, cert_path, key_path
		FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'
		ORDER BY created_at DESC LIMIT 1`, domainID).
		Scan(&certType, &certPath, &keyPath); err != nil {
		return err
	}
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE sites
		SET ssl_enabled = true, ssl_type = ?,
		    ssl_cert_path = ?, ssl_key_path = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?`, certType, certPath, keyPath, domainID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (p *Panel) retryPendingCertificate(ctx context.Context, domainID int) error {
	managedNames, err := p.managedSiteHostnames(ctx, domainID)
	if err != nil {
		return err
	}
	status, err := p.loadCertificateRuntimeStatus(ctx, domainID, managedNames)
	if err != nil {
		return err
	}
	if !status.ActivationPending && !status.DependentsPending {
		return errors.New("the certificate has no pending activation to retry")
	}
	if !status.Info.Valid || !status.Info.TrustChecked || !status.Info.Trusted || !status.CoversManaged {
		detail := strings.TrimSpace(status.Info.Error)
		if detail == "" {
			detail = strings.TrimSpace(status.Info.TrustError)
		}
		if detail == "" {
			detail = "the certificate is invalid or no longer covers every managed hostname"
		}
		return errors.New(detail)
	}

	if status.ActivationPending {
		if err := p.enableActiveCertificateForRetry(ctx, domainID); err != nil {
			return err
		}
		if err := p.applyVhostForDomain(ctx, domainID); err != nil {
			rollbackCtx, cancel := sslCompensationContext()
			defer cancel()
			_, disableErr := p.db.GetDB().ExecContext(rollbackCtx, `
				UPDATE sites
				SET ssl_enabled = false, updated_at = datetime('now')
				WHERE domain_id = ?`, domainID)
			if disableErr != nil {
				return fmt.Errorf("web server activation failed: %v; pending-state rollback failed: %w", err, disableErr)
			}
			return err
		}
	}

	if err := p.syncCertificateDependents(ctx, domainID); err != nil {
		if markErr := p.markCertificatePendingDetached(
			ctx, domainID, sslPendingDependents, false,
		); markErr != nil {
			return fmt.Errorf("certificate dependent sync failed: %v; pending state failed: %w", err, markErr)
		}
		return err
	}
	return p.clearCertificatePendingDetached(ctx, domainID)
}
