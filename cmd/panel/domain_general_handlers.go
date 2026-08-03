package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/repositories"
)

// Domain general settings structures
type DomainGeneralSettings struct {
	DomainID             int      `json:"domain_id"`
	DomainName           string   `json:"domain_name"`
	DocumentRoot         string   `json:"document_root"`
	WebServer            string   `json:"web_server"`
	RedirectWWW          bool     `json:"redirect_www"`
	RedirectWWWAvailable bool     `json:"redirect_www_available"`
	RedirectHTTPS        bool     `json:"redirect_https"`
	Aliases              []string `json:"aliases"`
}

type UpdateGeneralSettingsRequest struct {
	DocumentRoot  string `json:"document_root"`
	WebServer     string `json:"web_server"`
	RedirectWWW   bool   `json:"redirect_www"`
	RedirectHTTPS *bool  `json:"redirect_https"`
}

type AddAliasRequest struct {
	Alias                     string `json:"alias"`
	ConfirmCertificateReissue bool   `json:"confirm_certificate_reissue"`
}

type aliasMutationLock struct {
	mu   sync.Mutex
	refs int
}

var (
	domainAliasMutationLocks = struct {
		sync.Mutex
		entries map[string]*aliasMutationLock
	}{entries: make(map[string]*aliasMutationLock)}
	errInvalidDomainAlias           = errors.New("invalid domain alias")
	errDomainAliasConflict          = errors.New("domain alias conflicts with an existing hostname")
	errDomainAliasNotFound          = errors.New("domain alias not found")
	errAliasCertificateCoverage     = errors.New("active certificate does not cover the domain alias")
	errGeneralUnsupportedWebServer  = errors.New("unsupported general-settings web server")
	errGeneralImmutableDocumentRoot = errors.New("document root is immutable")
	errGeneralHTTPSManagedBySSL     = errors.New("HTTPS redirection is managed by SSL/TLS")
	errGeneralWWWUnavailable        = errors.New("managed www hostname is unavailable")
	errGeneralVhostApplyRejected    = errors.New("general settings vhost apply was rejected and restored")
)

type aliasVhostApply func(context.Context, int) error
type aliasCertificateVerifier func(context.Context, int, string) error

// lockDomainAliasMutation serializes only operations that target the same
// canonical alias. The old process-wide mutex kept every tenant blocked while
// an unrelated alias waited on ACME, which could take up to the SSL timeout.
func lockDomainAliasMutation(alias string) func() {
	domainAliasMutationLocks.Lock()
	entry := domainAliasMutationLocks.entries[alias]
	if entry == nil {
		entry = &aliasMutationLock{}
		domainAliasMutationLocks.entries[alias] = entry
	}
	entry.refs++
	domainAliasMutationLocks.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		domainAliasMutationLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(domainAliasMutationLocks.entries, alias)
		}
		domainAliasMutationLocks.Unlock()
	}
}

func canonicalDomainAlias(raw string) (string, error) {
	alias, err := hostname.CanonicalFQDN(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errInvalidDomainAlias, err)
	}
	return alias, nil
}

func (p *Panel) ensureActiveCertificateCoversAlias(
	ctx context.Context,
	domainID int,
	alias string,
) error {
	var certPath, keyPath string
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT cert_path, key_path
		FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'
		ORDER BY created_at DESC LIMIT 1`, domainID).
		Scan(&certPath, &keyPath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := p.inspectInstalledCertificate(ctx, certPath, keyPath)
	if err != nil {
		return fmt.Errorf("verify the active certificate before adding the alias: %w", err)
	}
	if !info.Valid || !certificateCoversHostname(info.DNSNames, alias) {
		return fmt.Errorf(
			"%w %q; remove or replace the certificate before adding this alias",
			errAliasCertificateCoverage,
			alias,
		)
	}
	return nil
}

func (p *Panel) aliasConflicts(ctx context.Context, domainID int, alias string) error {
	var domainExists bool
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM domains WHERE id = ?)`, domainID,
	).Scan(&domainExists); err != nil {
		return err
	}
	if !domainExists {
		return sql.ErrNoRows
	}

	var conflict bool
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM hostname_reservations
			WHERE hostname = ? COLLATE NOCASE
		)`, alias,
	).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return errDomainAliasConflict
	}
	return nil
}

func (p *Panel) addDomainAlias(
	ctx context.Context,
	domainID int,
	rawAlias string,
	verify aliasCertificateVerifier,
	apply aliasVhostApply,
) (string, error) {
	alias, err := canonicalDomainAlias(rawAlias)
	if err != nil {
		return "", err
	}
	if err := p.aliasConflicts(ctx, domainID, alias); err != nil {
		return "", err
	}
	if verify != nil {
		if err := verify(ctx, domainID, alias); err != nil {
			return "", err
		}
	}
	if _, err := p.db.GetDB().ExecContext(ctx,
		`INSERT INTO domain_aliases (domain_id, alias) VALUES (?, ?)`,
		domainID, alias,
	); err != nil {
		if hostname.IsNamespaceConflict(err) {
			return "", errDomainAliasConflict
		}
		return "", err
	}
	if err := apply(ctx, domainID); err == nil {
		return alias, nil
	} else {
		applyErr := err
		rollbackCtx, cancel := sslCompensationContext()
		defer cancel()
		_, rollbackErr := p.db.GetDB().ExecContext(rollbackCtx,
			`DELETE FROM domain_aliases WHERE domain_id = ? AND alias = ?`,
			domainID, alias,
		)
		restoreErr := apply(rollbackCtx, domainID)
		if rollbackErr != nil || restoreErr != nil {
			return "", fmt.Errorf(
				"apply alias vhost: %v; database rollback: %v; vhost restore: %v",
				applyErr, rollbackErr, restoreErr,
			)
		}
		return "", fmt.Errorf("apply alias vhost: %w", applyErr)
	}
}

func (p *Panel) deleteDomainAlias(
	ctx context.Context,
	domainID int,
	rawAlias string,
	apply aliasVhostApply,
) error {
	alias, err := canonicalDomainAlias(rawAlias)
	if err != nil {
		return err
	}
	if err := p.ensureHSTSAllowsHostnameRemoval(ctx, domainID); err != nil {
		return err
	}
	result, err := p.db.GetDB().ExecContext(ctx, `
		DELETE FROM domain_aliases
		WHERE domain_id = ? AND lower(alias) = ?`, domainID, alias)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errDomainAliasNotFound
	}
	if err := apply(ctx, domainID); err == nil {
		return nil
	} else {
		applyErr := err
		rollbackCtx, cancel := sslCompensationContext()
		defer cancel()
		_, rollbackErr := p.db.GetDB().ExecContext(rollbackCtx,
			`INSERT INTO domain_aliases (domain_id, alias) VALUES (?, ?)`,
			domainID, alias,
		)
		restoreErr := apply(rollbackCtx, domainID)
		if rollbackErr != nil || restoreErr != nil {
			return fmt.Errorf(
				"apply alias removal vhost: %v; database rollback: %v; vhost restore: %v",
				applyErr, rollbackErr, restoreErr,
			)
		}
		return fmt.Errorf("apply alias removal vhost: %w", applyErr)
	}
}

type domainGeneralState struct {
	SubscriptionID int
	DomainName     string
	DocumentRoot   string
	WebServer      string
	RedirectWWW    bool
}

func (p *Panel) managedWWWRedirectAvailable(
	ctx context.Context,
	domainID int,
	domainName string,
) (bool, error) {
	domain, err := hostname.CanonicalFQDN(domainName)
	if err != nil {
		return false, err
	}
	names, err := p.managedSiteHostnames(ctx, domainID)
	if err != nil {
		return false, err
	}
	target := "www." + domain
	for _, name := range names {
		if name == target {
			return true, nil
		}
	}
	return false, nil
}

// updateDomainGeneralSettings mutates only settings backed by the live nginx
// adapter. The database change and vhost activation form one compensated
// operation: an activation failure restores both the prior row and prior
// runtime vhost before returning.
func (p *Panel) updateDomainGeneralSettings(
	ctx context.Context,
	domainID int,
	req UpdateGeneralSettingsRequest,
	apply aliasVhostApply,
) error {
	var previous domainGeneralState
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT d.subscription_id, d.name, s.document_root,
		       COALESCE(s.web_server, 'nginx'),
		       COALESCE(s.redirect_www, false)
		FROM sites s
		JOIN domains d ON d.id = s.domain_id
		WHERE s.domain_id = ?`, domainID).Scan(
		&previous.SubscriptionID,
		&previous.DomainName,
		&previous.DocumentRoot,
		&previous.WebServer,
		&previous.RedirectWWW,
	); err != nil {
		return err
	}
	if req.RedirectHTTPS != nil {
		return errGeneralHTTPSManagedBySSL
	}
	if req.WebServer != "nginx" {
		return errGeneralUnsupportedWebServer
	}
	if req.DocumentRoot != previous.DocumentRoot {
		return errGeneralImmutableDocumentRoot
	}
	if err := hostingpath.ValidateDocumentRoot(
		req.DocumentRoot,
		previous.SubscriptionID,
		domainID,
	); err != nil {
		return fmt.Errorf("%w: %v", errGeneralImmutableDocumentRoot, err)
	}
	if req.RedirectWWW {
		available, err := p.managedWWWRedirectAvailable(
			ctx,
			domainID,
			previous.DomainName,
		)
		if err != nil {
			return err
		}
		if !available {
			return errGeneralWWWUnavailable
		}
	}

	if _, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE sites
		SET web_server = 'nginx',
		    redirect_www = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?`, req.RedirectWWW, domainID); err != nil {
		return err
	}
	if err := apply(ctx, domainID); err == nil {
		return nil
	} else {
		applyErr := err
		rollbackCtx, cancel := sslCompensationContext()
		defer cancel()
		if _, rollbackErr := p.db.GetDB().ExecContext(rollbackCtx, `
			UPDATE sites
			SET web_server = ?,
			    redirect_www = ?,
			    updated_at = datetime('now')
			WHERE domain_id = ?`,
			previous.WebServer,
			previous.RedirectWWW,
			domainID,
		); rollbackErr != nil {
			return fmt.Errorf(
				"apply general settings vhost: %v; database rollback: %w",
				applyErr,
				rollbackErr,
			)
		}
		if restoreErr := apply(rollbackCtx, domainID); restoreErr != nil {
			return fmt.Errorf(
				"apply general settings vhost: %v; vhost restore: %w",
				applyErr,
				restoreErr,
			)
		}
		return fmt.Errorf("%w: %v", errGeneralVhostApplyRejected, applyErr)
	}
}

// GET /api/v1/domains/:id/general
func (p *Panel) handleDomainGeneralSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		p.handleGetGeneralSettings(w, r, domainID)
	} else {
		p.handleUpdateGeneralSettings(w, r, domainID)
	}
}

func (p *Panel) handleGetGeneralSettings(w http.ResponseWriter, r *http.Request, domainID int) {
	ctx := r.Context()
	pool := p.db.GetDB()

	// Get domain info using repository
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Get site info
	var site struct {
		DocumentRoot  string
		WebServer     string
		RedirectWWW   bool
		RedirectHTTPS bool
	}
	err = pool.QueryRowContext(ctx, `
		SELECT document_root, 
		       COALESCE(web_server, 'nginx') as web_server,
		       COALESCE(redirect_www, false) as redirect_www,
		       COALESCE(force_https, false) as redirect_https
		FROM sites WHERE domain_id = ?
	`, domainID).Scan(&site.DocumentRoot, &site.WebServer, &site.RedirectWWW, &site.RedirectHTTPS)

	if err != nil {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}
	redirectWWWAvailable, err := p.managedWWWRedirectAvailable(
		ctx,
		domainID,
		domain.Name,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Get aliases
	rows, err := pool.QueryContext(ctx, "SELECT alias FROM domain_aliases WHERE domain_id = ?", domainID)
	if err != nil {
		http.Error(w, "Failed to load aliases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	aliases := make([]string, 0)
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			writeServerError(w, err)
			return
		}
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		writeServerError(w, err)
		return
	}

	settings := DomainGeneralSettings{
		DomainID:             domain.ID,
		DomainName:           domain.Name,
		DocumentRoot:         site.DocumentRoot,
		WebServer:            site.WebServer,
		RedirectWWW:          site.RedirectWWW,
		RedirectWWWAvailable: redirectWWWAvailable,
		RedirectHTTPS:        site.RedirectHTTPS,
		Aliases:              aliases,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

func (p *Panel) handleUpdateGeneralSettings(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	var req UpdateGeneralSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	ctx, cancel := sslDurableContext(r.Context())
	defer cancel()

	if err := p.updateDomainGeneralSettings(
		ctx,
		domainID,
		req,
		p.applyVhostForDomain,
	); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeClientError(w, http.StatusNotFound, "domain site not found")
		case errors.Is(err, errGeneralUnsupportedWebServer):
			writeClientError(w, http.StatusBadRequest, "Nginx is the only supported web-server adapter")
		case errors.Is(err, errGeneralImmutableDocumentRoot):
			writeClientError(w, http.StatusBadRequest, "document root is fixed to this site's managed directory")
		case errors.Is(err, errGeneralHTTPSManagedBySSL):
			writeClientError(w, http.StatusBadRequest, "Force HTTPS is managed in the SSL/TLS section")
		case errors.Is(err, errGeneralWWWUnavailable):
			writeClientError(w, http.StatusConflict, "www redirection requires a managed www hostname for this site")
		case errors.Is(err, errGeneralVhostApplyRejected):
			writeCodedError(
				w,
				http.StatusConflict,
				"GENERAL_VHOST_APPLY_FAILED",
				"settings were not saved because the web-server configuration could not be activated",
				"",
			)
		default:
			writeServerError(w, err)
		}
		return
	}

	p.audit(r, "domain.general.update", "domain", domainID)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// POST /api/v1/domains/:id/aliases
func (p *Panel) handleDomainAliases(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	if r.Method == "POST" {
		p.handleAddAlias(w, r, domainID)
	} else {
		p.handleDeleteAlias(w, r, domainID, pathParts)
	}
}

func (p *Panel) handleAddAlias(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	var req AddAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	alias, err := canonicalDomainAlias(req.Alias)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	unlockAlias := lockDomainAliasMutation(alias)
	defer unlockAlias()
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	ctx, cancel := sslDurableContext(r.Context())
	defer cancel()

	if err := p.aliasConflicts(ctx, domainID, alias); err != nil {
		if errors.Is(err, errDomainAliasConflict) {
			writeClientError(w, http.StatusConflict, err.Error())
		} else if errors.Is(err, sql.ErrNoRows) {
			writeClientError(w, http.StatusNotFound, "domain not found")
		} else {
			writeServerError(w, err)
		}
		return
	}

	activeCert, err := p.loadActiveAliasCertificate(ctx, domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	needsManagedReissue := false
	if activeCert != nil {
		info, inspectErr := p.inspectInstalledCertificate(
			ctx, activeCert.CertPath, activeCert.KeyPath,
		)
		if inspectErr != nil {
			writeServerError(w, fmt.Errorf(
				"inspect active certificate before adding alias: %w", inspectErr,
			))
			return
		}
		if !certificateCoversHostname(info.DNSNames, alias) {
			if activeCert.Type != "letsencrypt" {
				writeClientError(
					w,
					http.StatusConflict,
					"the active custom certificate does not cover the alias; upload a replacement certificate first",
				)
				return
			}
			needsManagedReissue = true
		}
	}

	if needsManagedReissue && !req.ConfirmCertificateReissue {
		writeCodedError(
			w,
			http.StatusConflict,
			errCodeAliasCertificateReissueRequired,
			"adding this alias requires reissuing the active ACME certificate with the new hostname",
			"confirm_certificate_reissue",
		)
		return
	}

	if needsManagedReissue {
		currentNames, namesErr := p.managedSiteHostnames(ctx, domainID)
		if namesErr != nil {
			writeServerError(w, namesErr)
			return
		}
		desiredNames, namesErr := desiredAliasCertificateNames(
			currentNames, alias, "",
		)
		if namesErr != nil {
			writeServerError(w, namesErr)
			return
		}
		install, issueErr := p.issueAliasCertificateSnapshot(
			ctx, domainID, activeCert, desiredNames, []string{alias},
		)
		if issueErr != nil {
			writeClientError(w, http.StatusConflict,
				"the alias certificate could not be reissued: "+issueErr.Error())
			return
		}

		activation, err := p.mutateAliasAndActivateCertificate(
			ctx, domainID, alias, true, install,
		)
		if err != nil {
			cleanupCtx, cleanupCancel := sslCompensationContext()
			defer cleanupCancel()
			p.cleanupUncommittedCertificate(
				cleanupCtx,
				certificateCleanupTarget{
					Domain:      install.DomainName,
					LineageName: install.LineageName,
					CertPath:    install.CertPath,
					KeyPath:     install.KeyPath,
					ChainPath:   install.ChainPath,
				},
			)
			writeServerError(w, fmt.Errorf(
				"atomically reserve alias and activate certificate: %w", err,
			))
			return
		}
		if err := p.applyVhostForDomain(ctx, domainID); err != nil {
			if markErr := p.markCertificatePendingDetached(
				ctx, domainID, sslPendingActivation, true,
			); markErr != nil {
				writeServerError(w, fmt.Errorf(
					"alias certificate activation failed: %v; pending state failed: %w",
					err, markErr,
				))
				return
			}
			_ = activation
			p.audit(r, "domain.alias.add.reissue.partial", "domain", domainID)
			writeCodedError(
				w,
				http.StatusConflict,
				errCodeAliasCertificatePending,
				"the alias and certificate were saved, but web-server activation is pending; use Retry activation on SSL/TLS",
				"retry_ssl_activation",
			)
			return
		}
		if err := p.syncCertificateDependents(ctx, domainID); err != nil {
			if markErr := p.markCertificatePendingDetached(
				ctx, domainID, sslPendingDependents, false,
			); markErr != nil {
				writeServerError(w, fmt.Errorf(
					"alias certificate dependent sync failed: %v; pending state failed: %w",
					err, markErr,
				))
				return
			}
			p.audit(r, "domain.alias.add.reissue.dependents_partial", "domain", domainID)
			writeCodedError(
				w,
				http.StatusConflict,
				errCodeAliasCertificatePending,
				"the alias certificate is active, but mail TLS synchronization is pending; use Retry activation on SSL/TLS",
				"retry_ssl_activation",
			)
			return
		}
		if err := p.clearCertificatePendingDetached(ctx, domainID); err != nil {
			writeServerError(w, err)
			return
		}
		p.audit(r, "domain.alias.add.reissue", "domain", domainID)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"status":                 "success",
			"alias":                  alias,
			"certificate_reissued":   true,
			"certificate_expires_at": install.ExpiresAt,
		})
		return
	}

	alias, err = p.addDomainAlias(
		ctx,
		domainID,
		req.Alias,
		p.ensureActiveCertificateCoversAlias,
		p.applyVhostForDomain,
	)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidDomainAlias):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, errDomainAliasConflict),
			errors.Is(err, errAliasCertificateCoverage):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			writeServerError(w, err)
		}
		return
	}

	p.audit(r, "domain.alias.add", "domain", domainID)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "alias": alias})
}

func (p *Panel) handleDeleteAlias(w http.ResponseWriter, r *http.Request, domainID int, pathParts []string) {
	w.Header().Set("Content-Type", "application/json")
	if len(pathParts) < 7 {
		http.Error(w, "Alias not specified", http.StatusBadRequest)
		return
	}

	alias, err := canonicalDomainAlias(pathParts[6])
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	unlockAlias := lockDomainAliasMutation(alias)
	defer unlockAlias()
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	ctx, cancel := sslDurableContext(r.Context())
	defer cancel()

	if err := p.ensureHSTSAllowsHostnameRemoval(ctx, domainID); err != nil {
		if message, guarded := hstsRemovalConflictMessage(err, "the alias"); guarded {
			writeClientError(w, http.StatusConflict, message)
		} else {
			writeServerError(w, err)
		}
		return
	}
	var aliasExists bool
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM domain_aliases
			WHERE domain_id = ? AND alias = ? COLLATE NOCASE
		)`, domainID, alias).Scan(&aliasExists); err != nil {
		writeServerError(w, err)
		return
	}
	if !aliasExists {
		writeClientError(w, http.StatusNotFound, "Alias not found")
		return
	}

	activeCert, err := p.loadActiveAliasCertificate(ctx, domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	needsManagedReissue := false
	if activeCert != nil && activeCert.Type == "letsencrypt" {
		info, inspectErr := p.inspectInstalledCertificate(
			ctx, activeCert.CertPath, activeCert.KeyPath,
		)
		if inspectErr != nil {
			writeServerError(w, fmt.Errorf(
				"inspect active certificate before deleting alias: %w", inspectErr,
			))
			return
		}
		needsManagedReissue = certificateCoversHostname(info.DNSNames, alias)
	}
	confirmedReissue := r.URL.Query().Get("confirm_certificate_reissue") == "true"
	if needsManagedReissue && !confirmedReissue {
		writeCodedError(
			w,
			http.StatusConflict,
			errCodeAliasCertificateReissueRequired,
			"removing this alias requires reissuing the active ACME certificate without that hostname",
			"confirm_certificate_reissue",
		)
		return
	}

	if needsManagedReissue {
		currentNames, namesErr := p.managedSiteHostnames(ctx, domainID)
		if namesErr != nil {
			writeServerError(w, namesErr)
			return
		}
		desiredNames, namesErr := desiredAliasCertificateNames(
			currentNames, "", alias,
		)
		if namesErr != nil {
			writeServerError(w, namesErr)
			return
		}
		install, issueErr := p.issueAliasCertificateSnapshot(
			ctx, domainID, activeCert, desiredNames, nil,
		)
		if issueErr != nil {
			writeClientError(w, http.StatusConflict,
				"the alias certificate could not be reissued: "+issueErr.Error())
			return
		}
		if _, err := p.mutateAliasAndActivateCertificate(
			ctx, domainID, alias, false, install,
		); err != nil {
			cleanupCtx, cleanupCancel := sslCompensationContext()
			defer cleanupCancel()
			p.cleanupUncommittedCertificate(
				cleanupCtx,
				certificateCleanupTarget{
					Domain:      install.DomainName,
					LineageName: install.LineageName,
					CertPath:    install.CertPath,
					KeyPath:     install.KeyPath,
					ChainPath:   install.ChainPath,
				},
			)
			writeServerError(w, fmt.Errorf(
				"atomically remove alias and activate certificate: %w", err,
			))
			return
		}
		if err := p.applyVhostForDomain(ctx, domainID); err != nil {
			if markErr := p.markCertificatePendingDetached(
				ctx, domainID, sslPendingActivation, true,
			); markErr != nil {
				writeServerError(w, fmt.Errorf(
					"alias removal certificate activation failed: %v; pending state failed: %w",
					err, markErr,
				))
				return
			}
			p.audit(r, "domain.alias.delete.reissue.partial", "domain", domainID)
			writeCodedError(
				w,
				http.StatusConflict,
				errCodeAliasCertificatePending,
				"the alias was removed and its certificate was saved, but web-server activation is pending; use Retry activation on SSL/TLS",
				"retry_ssl_activation",
			)
			return
		}
		if err := p.syncCertificateDependents(ctx, domainID); err != nil {
			if markErr := p.markCertificatePendingDetached(
				ctx, domainID, sslPendingDependents, false,
			); markErr != nil {
				writeServerError(w, fmt.Errorf(
					"alias removal dependent sync failed: %v; pending state failed: %w",
					err, markErr,
				))
				return
			}
			p.audit(r, "domain.alias.delete.reissue.dependents_partial", "domain", domainID)
			writeCodedError(
				w,
				http.StatusConflict,
				errCodeAliasCertificatePending,
				"the alias was removed, but mail TLS synchronization is pending; use Retry activation on SSL/TLS",
				"retry_ssl_activation",
			)
			return
		}
		if err := p.clearCertificatePendingDetached(ctx, domainID); err != nil {
			writeServerError(w, err)
			return
		}
		p.audit(r, "domain.alias.delete.reissue", "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{
			"status":               "success",
			"certificate_reissued": true,
		})
		return
	}

	err = p.deleteDomainAlias(
		ctx, domainID, alias, p.applyVhostForDomain,
	)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidDomainAlias):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, errDomainAliasNotFound):
			http.Error(w, "Alias not found", http.StatusNotFound)
		default:
			if message, guarded := hstsRemovalConflictMessage(err, "the alias"); guarded {
				writeClientError(w, http.StatusConflict, message)
				return
			}
			writeServerError(w, err)
		}
		return
	}

	p.audit(r, "domain.alias.delete", "domain", domainID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
