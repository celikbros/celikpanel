package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var errNoActiveCertificate = errors.New("the domain has no active certificate")
var errHSTSEnabled = errors.New("HSTS is enabled")

type hstsRetirementActiveError struct {
	Until time.Time
}

func (e *hstsRetirementActiveError) Error() string {
	return fmt.Sprintf("HSTS retirement remains active until %s", e.Until.UTC().Format(time.RFC3339))
}

func hstsHostnameRemovalGuard(state siteSSLState, now time.Time) error {
	if state.HSTSEnabled {
		return errHSTSEnabled
	}
	retireAfter, err := parseOptionalDBTime(state.HSTSRetireAfter)
	if err != nil {
		return err
	}
	if retireAfter != nil && retireAfter.After(now.UTC()) {
		return &hstsRetirementActiveError{Until: *retireAfter}
	}
	return nil
}

// ensureHSTSAllowsHostnameRemoval protects every removal path, not only the
// certificate button. Deleting a domain or alias also removes the HTTPS
// hostname visitors may still be required to use by a cached HSTS policy.
func (p *Panel) ensureHSTSAllowsHostnameRemoval(ctx context.Context, domainID int) error {
	state, err := loadSiteSSLState(ctx, p.db.GetDB(), domainID)
	if errors.Is(err, sql.ErrNoRows) {
		// DNS-only domains have no web hostname and therefore advertise no HSTS.
		return nil
	}
	if err != nil {
		return err
	}
	return hstsHostnameRemovalGuard(state, time.Now().UTC())
}

func hstsRemovalConflictMessage(err error, subject string) (string, bool) {
	if errors.Is(err, errHSTSEnabled) {
		return fmt.Sprintf(
			"%s cannot be removed while HSTS is enabled; disable HSTS first so HTTPS can advertise max-age=0",
			subject,
		), true
	}
	var retirementErr *hstsRetirementActiveError
	if errors.As(err, &retirementErr) {
		return fmt.Sprintf(
			"%s must remain available until the previous HSTS policy expires at %s",
			subject,
			retirementErr.Until.UTC().Format(time.RFC3339),
		), true
	}
	return "", false
}

type certificateDetach struct {
	DomainID     int
	OldCertIDs   []int64
	PreviousSite siteSSLState
}

func (p *Panel) detachCertificate(ctx context.Context, domainID int) (*certificateDetach, error) {
	pool := p.db.GetDB()
	state, err := loadSiteSSLState(ctx, pool, domainID)
	if err != nil {
		return nil, err
	}
	if err := hstsHostnameRemovalGuard(state, time.Now().UTC()); err != nil {
		return nil, err
	}
	oldIDs, err := activeCertificateIDs(ctx, pool, domainID)
	if err != nil {
		return nil, err
	}
	if len(oldIDs) == 0 {
		return nil, errNoActiveCertificate
	}

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE ssl_certificates
		SET status = 'revoked', updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active'`, domainID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE sites
		SET ssl_enabled = false, ssl_type = 'none',
		    ssl_cert_path = NULL, ssl_key_path = NULL,
		    force_https = false, hsts_enabled = false,
		    hsts_retire_after = NULL,
		    updated_at = datetime('now')
		WHERE domain_id = ?`, domainID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &certificateDetach{
		DomainID: domainID, OldCertIDs: oldIDs, PreviousSite: state,
	}, nil
}

func (p *Panel) rollbackCertificateDetach(ctx context.Context, detach *certificateDetach) error {
	if detach == nil {
		return nil
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range detach.OldCertIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ssl_certificates SET status = 'active', updated_at = datetime('now')
			WHERE id = ?`, id); err != nil {
			return err
		}
	}
	if err := restoreSiteSSLState(ctx, tx, detach.DomainID, detach.PreviousSite); err != nil {
		return err
	}
	return tx.Commit()
}
