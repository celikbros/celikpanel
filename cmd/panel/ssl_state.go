package main

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

type sslOperationLock struct {
	mu   sync.Mutex
	refs int
}

var domainSSLOperationLocks = struct {
	sync.Mutex
	entries map[int]*sslOperationLock
}{entries: make(map[int]*sslOperationLock)}

const sslMutationTimeout = 20 * time.Minute

func lockDomainSSLOperation(domainID int) func() {
	domainSSLOperationLocks.Lock()
	entry := domainSSLOperationLocks.entries[domainID]
	if entry == nil {
		entry = &sslOperationLock{}
		domainSSLOperationLocks.entries[domainID] = entry
	}
	entry.refs++
	domainSSLOperationLocks.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		domainSSLOperationLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(domainSSLOperationLocks.entries, domainID)
		}
		domainSSLOperationLocks.Unlock()
	}
}

func sslCompensationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// sslDurableContext keeps an already-authorized certificate mutation
// consistent if the browser disconnects, but still places a hard upper bound
// on the domain lock and every downstream RPC/system operation.
func sslDurableContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), sslMutationTimeout)
}

type certificateInstall struct {
	DomainName     string
	Type           string
	CertPath       string
	KeyPath        string
	ChainPath      string
	LineageName    string
	ACMEProviderID string
	Issuer         string
	Subject        string
	IssuedAt       time.Time
	ExpiresAt      time.Time
	AutoRenew      bool
	SecureMail     bool
}

type siteSSLState struct {
	Enabled         bool
	Type            string
	CertPath        sql.NullString
	KeyPath         sql.NullString
	ForceHTTPS      bool
	HSTSEnabled     bool
	HSTSMaxAge      int
	HSTSRetireAfter sql.NullString
}

type certificateActivation struct {
	DomainID     int
	NewCertID    int64
	OldCertIDs   []int64
	PreviousSite siteSSLState
}

func (p *Panel) activateCertificate(ctx context.Context, domainID int, cert certificateInstall) (*certificateActivation, error) {
	pool := p.db.GetDB()
	state, err := loadSiteSSLState(ctx, pool, domainID)
	if err != nil {
		return nil, err
	}
	oldIDs, err := activeCertificateIDs(ctx, pool, domainID)
	if err != nil {
		return nil, err
	}

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	newID, err := applyCertificateActivationTx(ctx, tx, domainID, cert)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &certificateActivation{
		DomainID: domainID, NewCertID: newID, OldCertIDs: oldIDs, PreviousSite: state,
	}, nil
}

// mutateAliasAndActivateCertificate commits the public hostname namespace and
// the certificate ledger/site pointers as one SQLite transaction. A staged
// certbot lineage may already exist at this point, but it cannot become the
// renewal source unless this transaction commits its exact lineage identity.
func (p *Panel) mutateAliasAndActivateCertificate(
	ctx context.Context,
	domainID int,
	alias string,
	add bool,
	cert certificateInstall,
) (*certificateActivation, error) {
	pool := p.db.GetDB()
	state, err := loadSiteSSLState(ctx, pool, domainID)
	if err != nil {
		return nil, err
	}
	oldIDs, err := activeCertificateIDs(ctx, pool, domainID)
	if err != nil {
		return nil, err
	}

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if add {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO domain_aliases (domain_id, alias) VALUES (?, ?)`,
			domainID, alias,
		); err != nil {
			return nil, err
		}
	} else {
		result, err := tx.ExecContext(ctx,
			`DELETE FROM domain_aliases WHERE domain_id = ? AND alias = ? COLLATE NOCASE`,
			domainID, alias,
		)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, sql.ErrNoRows
		}
	}

	newID, err := applyCertificateActivationTx(ctx, tx, domainID, cert)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &certificateActivation{
		DomainID: domainID, NewCertID: newID, OldCertIDs: oldIDs, PreviousSite: state,
	}, nil
}

func applyCertificateActivationTx(
	ctx context.Context,
	tx *sql.Tx,
	domainID int,
	cert certificateInstall,
) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
		UPDATE ssl_certificates
		SET status = 'revoked', updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active'`, domainID); err != nil {
		return 0, err
	}

	issuedAt := time.Now().UTC()
	if !cert.IssuedAt.IsZero() {
		issuedAt = cert.IssuedAt.UTC()
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, chain_path,
			lineage_name, acme_provider_id, issuer, subject, issued_at,
			expires_at, auto_renew, secure_mail, renewal_status, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
		domainID, cert.Type, cert.CertPath, cert.KeyPath, nullableString(cert.ChainPath),
		nullableString(cert.LineageName), nullableString(cert.ACMEProviderID),
		cert.Issuer, cert.Subject, issuedAt.Format(time.RFC3339),
		cert.ExpiresAt.UTC().Format(time.RFC3339), cert.AutoRenew, cert.SecureMail,
		sslPendingActivation)
	if err != nil {
		return 0, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE sites
		SET ssl_enabled = true,
		    ssl_type = ?,
		    ssl_cert_path = ?,
		    ssl_key_path = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?`,
		cert.Type, cert.CertPath, cert.KeyPath, domainID); err != nil {
		return 0, err
	}
	return newID, nil
}

func (p *Panel) rollbackCertificateActivation(ctx context.Context, activation *certificateActivation) error {
	if activation == nil {
		return nil
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM ssl_certificates WHERE id = ?`, activation.NewCertID); err != nil {
		return err
	}
	for _, id := range activation.OldCertIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ssl_certificates SET status = 'active', updated_at = datetime('now')
			WHERE id = ?`, id); err != nil {
			return err
		}
	}
	if err := restoreSiteSSLState(ctx, tx, activation.DomainID, activation.PreviousSite); err != nil {
		return err
	}
	return tx.Commit()
}

func loadSiteSSLState(ctx context.Context, db *sql.DB, domainID int) (siteSSLState, error) {
	var state siteSSLState
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(ssl_enabled, false), COALESCE(ssl_type, 'none'),
		       ssl_cert_path, ssl_key_path,
		       COALESCE(force_https, false), COALESCE(hsts_enabled, false),
		       COALESCE(hsts_max_age, 31536000), hsts_retire_after
		FROM sites WHERE domain_id = ?`, domainID).
		Scan(&state.Enabled, &state.Type, &state.CertPath, &state.KeyPath,
			&state.ForceHTTPS, &state.HSTSEnabled, &state.HSTSMaxAge,
			&state.HSTSRetireAfter)
	return state, err
}

func restoreSiteSSLState(ctx context.Context, tx *sql.Tx, domainID int, state siteSSLState) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE sites
		SET ssl_enabled = ?, ssl_type = ?, ssl_cert_path = ?, ssl_key_path = ?,
		    force_https = ?, hsts_enabled = ?, hsts_max_age = ?,
		    hsts_retire_after = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?`,
		state.Enabled, state.Type, nullStringValue(state.CertPath), nullStringValue(state.KeyPath),
		state.ForceHTTPS, state.HSTSEnabled, state.HSTSMaxAge,
		nullStringValue(state.HSTSRetireAfter), domainID)
	return err
}

func activeCertificateIDs(ctx context.Context, db *sql.DB, domainID int) ([]int64, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM ssl_certificates WHERE domain_id = ? AND status = 'active' ORDER BY id`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func parseOptionalDBTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed := parseDBTime(value.String)
	if parsed.IsZero() {
		return nil, fmt.Errorf("invalid HSTS retirement timestamp")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func nullableDBTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

// nextHSTSRetirement derives monotonic, server-owned retirement state. A
// browser may still hold the old max-age when the operator lowers it or
// re-enables HSTS, so no settings change may shorten an existing window.
// Clients can only toggle HSTS and choose max-age; they cannot set this value.
func nextHSTSRetirement(previous, next SSLSettings, now time.Time) *time.Time {
	var retireAfter *time.Time
	keepLater := func(candidate time.Time) {
		candidate = candidate.UTC().Truncate(time.Second)
		if retireAfter == nil || candidate.After(*retireAfter) {
			retireAfter = &candidate
		}
	}
	if previous.HSTSRetireAfter != nil {
		keepLater(*previous.HSTSRetireAfter)
	}
	now = now.UTC().Truncate(time.Second)
	if previous.HSTSEnabled {
		keepLater(now.Add(time.Duration(previous.HSTSMaxAge) * time.Second))
	}
	if next.HSTSEnabled {
		keepLater(now.Add(time.Duration(next.HSTSMaxAge) * time.Second))
	}
	return retireAfter
}

func validateSSLSettings(settings, previous SSLSettings, hasUsableCertificate bool) error {
	if settings.HSTSMaxAge < 0 || settings.HSTSMaxAge > 63072000 {
		return fmt.Errorf("HSTS max-age must be between 0 and 63072000 seconds")
	}
	if settings.HSTSEnabled && settings.HSTSMaxAge == 0 {
		return fmt.Errorf("HSTS max-age must be greater than zero while HSTS is enabled")
	}
	if settings.HSTSEnabled && !settings.ForceHTTPS {
		return fmt.Errorf("HTTPS redirect must stay enabled while HSTS is enabled")
	}
	enablingStrictHTTPS :=
		(settings.ForceHTTPS && !previous.ForceHTTPS) ||
			(settings.HSTSEnabled && !previous.HSTSEnabled)
	if enablingStrictHTTPS && !hasUsableCertificate {
		return fmt.Errorf("install and activate a trusted certificate covering every managed hostname before enabling HTTPS redirect or HSTS")
	}
	return nil
}
