package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Certificate issuance and renewal for a site whose configuration file the
// owner kept (D-031 step 1b, 10 Oct 2026; D-022; D-024; D-025 invariants 1, 2
// and 4).
//
// The ACME HTTP-01 location is no longer written into the vhost: it is a file
// in the Panel's own include directory of the site
// (/etc/nginx/celikpanel-managed.d/<domain>/acme-http-01.conf), which every
// server block of CelikPanel's text includes. So the validation needs no
// change to a file the owner kept, as long as that file still has the include
// line:
//
//   - Issuance on a kept file that has the line goes ahead without touching
//     the file. The new certificate is installed in the Panel's certificate
//     store as always and the site's TLS block becomes the Panel's text held
//     beside the file; the file keeps naming the certificate it named, so the
//     site keeps serving that one until the owner takes CelikPanel's text or
//     updates the certificate lines. Recorded as the ledger reason
//     "certificate" and the certificate's renewal status "waiting_for_owner".
//   - Issuance on a kept file without the line (or without the validation
//     block of mail.<domain> when that name is asked for) is refused before
//     anything is requested: 409 SITE_CONFIG_OWNER_EDITED, reason
//     certificate_validation, detail = the typed validation state, and the
//     ledger reason "certificate_validation".
//   - Renewal follows the same two paths. A renewal that cannot proceed is
//     recorded as "waiting_for_owner", not failed; nothing retries it except
//     the normal renewal schedule, which reads the file's state again.
//
// Sahibin koruduğu dosyada sertifika alma ve yenileme. Doğrulama konumu artık
// vhost'ta değil, Panel'in site için kendi ekleme dizinindedir; dosyada ekleme
// satırı durdukça doğrulama dosyaya dokunmadan yapılır.

const (
	// sslWaitingForOwner is the certificate's renewal status while the site's
	// configuration file the owner kept stops CelikPanel: the new certificate
	// is not served yet, or a renewal could not be started.
	sslWaitingForOwner = "waiting_for_owner"

	siteConfigCertificateValidationMessage = "This site’s nginx configuration file was changed outside CelikPanel and does not let CelikPanel publish the certificate validation without changing the file, so no certificate was requested and nothing was changed. " +
		"The server administrator chooses on the domain’s Configuration file page: take CelikPanel’s text, or add what that page shows to the file and reload nginx; then request the certificate again."

	// errCodeCertificateValidationUnknown: whether the kept file lets the
	// validation run could not be measured (nginx did not answer the probe).
	errCodeCertificateValidationUnknown        = "CERTIFICATE_VALIDATION_UNKNOWN"
	siteConfigCertificateValidationUnknownText = "CelikPanel could not check whether this site’s nginx configuration file lets the certificate validation run, because nginx on this server did not answer, so no certificate was requested and nothing was changed. " +
		"The server administrator checks that nginx is running; then request the certificate again."
)

// certificateValidationHeldError is a certificate operation stopped before
// anything was requested, because the kept file does not let the validation
// be published.
type certificateValidationHeldError struct {
	Domain string
	File   transport.SiteFileResult
}

func (e *certificateValidationHeldError) Error() string {
	return fmt.Sprintf("certificate validation for %s not prepared: the configuration file was kept (%s) and %s",
		e.Domain, e.File.State, e.File.Validation)
}

// certificateValidationUnknownError is a certificate operation that did not
// start because the probe got no answer: not "not ready", not known (D-025
// invariant 2). No ledger reason is written.
type certificateValidationUnknownError struct {
	Domain string
	File   transport.SiteFileResult
}

func (e *certificateValidationUnknownError) Error() string {
	return fmt.Sprintf("certificate validation for %s not prepared: whether the kept configuration file lets it run is unknown (%s)",
		e.Domain, e.File.ValidationDetail)
}

// keptSiteFile returns the file of a render the Agent did not apply because it
// kept the file (owner-edited, foreign, unknown origin).
func keptSiteFile(err error) (*transport.SiteFileResult, bool) {
	var held *siteFileHeldError
	if !errors.As(err, &held) || held.File.Outcome != transport.SiteFileOutcomeKept {
		return nil, false
	}
	file := held.File
	return &file, true
}

// prepareCertificateValidation publishes the validation before a certificate
// is requested. It returns kept=true when the site's file was kept and lets
// the validation go ahead unchanged: the caller then must not "restore" the
// vhost afterwards (nothing was changed in it) and must expect the final
// render to be kept too.
func (p *Panel) prepareCertificateValidation(ctx context.Context, domainID int, names []string) (bool, error) {
	err := p.applyVhostForCertificateValidation(ctx, domainID, names)
	if err == nil {
		return false, nil
	}
	file, kept := keptSiteFile(err)
	if !kept {
		return false, err
	}
	siteID, domain, idErr := p.siteIDForDomain(ctx, domainID)
	switch file.Validation {
	case transport.SiteFileValidationReady:
		// Measured ready: a reason the file gave earlier has ended.
		if idErr == nil {
			p.endCertificateValidationReason(ctx, siteID, domainID, domain)
		}
		return true, nil
	case transport.SiteFileValidationUnknown:
		return false, &certificateValidationUnknownError{Domain: domain, File: *file}
	}
	if idErr == nil {
		if _, setErr := services.SetSiteFileCertificateReason(ctx, p.db.GetDB(), siteID,
			services.SiteFileReasonCertificateValidation); setErr != nil {
			log.Printf("site configuration %s: record the certificate validation reason: %v", domain, setErr)
		}
	}
	if file.Validation == "" {
		// An Agent that does not say whether the validation can be published.
		file.Validation = transport.SiteFileValidationIncludeMissing
	}
	return false, &certificateValidationHeldError{Domain: domain, File: *file}
}

// writeCertificateSiteFileRefusal answers a certificate operation that the
// site's configuration file stopped before anything was requested. It returns
// false for any other error.
func writeCertificateSiteFileRefusal(w http.ResponseWriter, err error) bool {
	var validation *certificateValidationHeldError
	if errors.As(err, &validation) {
		log.Printf("[409] %v", err)
		body := apiErrorBody{
			Error:  siteConfigCertificateValidationMessage,
			Code:   errCodeSiteConfigOwnerEdited,
			Reason: services.SiteFileReasonCertificateValidation,
			Detail: validation.File.Validation,
		}
		body.Vars = certificateValidationVars(validation.File)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(body)
		return true
	}
	var unknown *certificateValidationUnknownError
	if errors.As(err, &unknown) {
		log.Printf("[503] %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(apiErrorBody{
			Error:  siteConfigCertificateValidationUnknownText,
			Code:   errCodeCertificateValidationUnknown,
			Detail: transport.SiteFileValidationUnknown,
		})
		return true
	}
	if classification, ok := classifySiteFileError(err); ok {
		log.Printf("[%d] %v", classification.Status, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(classification.Status)
		_ = json.NewEncoder(w).Encode(apiErrorBody{
			Error: classification.Message, Code: classification.Code, Reason: classification.Reason,
		})
		return true
	}
	return false
}

// certificateValidationVars are the refusal's variables: the include line and,
// when the probe got an answer it did not expect, the first name that did not
// serve it and nginx's HTTP status for it.
func certificateValidationVars(file transport.SiteFileResult) map[string]string {
	vars := map[string]string{}
	if file.ManagedInclude != "" {
		vars["include"] = file.ManagedInclude
	}
	if file.ValidationName != "" {
		vars["name"] = file.ValidationName
	}
	if file.ValidationStatus != 0 {
		vars["status"] = strconv.Itoa(file.ValidationStatus)
	}
	if len(vars) == 0 {
		return nil
	}
	return vars
}

// endCertificateValidationReason ends the "certificate_validation" reason once
// the probe found the kept file ready: the ledger reason, and with it the
// notice, the Domains badge and a renewal's waiting state (the dashboard
// entry). The "certificate" reason is not touched: a new certificate the file
// does not use yet still waits.
func (p *Panel) endCertificateValidationReason(ctx context.Context, siteID, domainID int, domain string) bool {
	// One statement: a "certificate" reason written meanwhile stays.
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE managed_site_files SET state_reason = ''
		WHERE site_id = ? AND kind = ? AND state_reason = ?`,
		siteID, transport.SiteFileKindNginxVhost, services.SiteFileReasonCertificateValidation)
	if err != nil {
		log.Printf("site configuration %s: clear the certificate validation reason: %v", domain, err)
		return false
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		return false
	}
	p.releaseCertificateWaitingForOwner(ctx, domainID)
	log.Printf("site configuration %s: the kept file now lets the certificate validation run; the waiting reason ended", domain)
	return true
}

// holdCertificateForOwner records that a new certificate is in the Panel's
// store and the kept file does not use it: the ledger reason "certificate"
// and the active certificate's renewal status "waiting_for_owner". The site
// is not disabled and nothing is retried: the file is the owner's.
func (p *Panel) holdCertificateForOwner(ctx context.Context, domainID int, at string) error {
	writeCtx, cancel := sslCompensationContext()
	defer cancel()
	siteID, domain, err := p.siteIDForDomain(writeCtx, domainID)
	if err != nil {
		return err
	}
	if _, err := services.SetSiteFileCertificateReason(writeCtx, p.db.GetDB(), siteID,
		services.SiteFileReasonCertificate); err != nil {
		return err
	}
	if _, err := p.db.GetDB().ExecContext(writeCtx, `
		UPDATE ssl_certificates
		SET renewal_status = ?, last_renewal_attempt = COALESCE(NULLIF(?, ''), last_renewal_attempt),
		    updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active'`, sslWaitingForOwner, at, domainID); err != nil {
		return err
	}
	log.Printf("site configuration %s: a new certificate is installed; the configuration file the owner kept does not use it yet", domain)
	return nil
}

// recordCertificateWaitingForOwner is a renewal that could not start because
// of the site's configuration file: "waiting_for_owner", not failed.
func (p *Panel) recordCertificateWaitingForOwner(certID int, domainName, at, detail string) {
	log.Printf("cert renewal %s: waiting for the owner: %s", domainName, strings.TrimSpace(detail))
	writeCtx, cancel := sslCompensationContext()
	defer cancel()
	if _, err := p.db.GetDB().ExecContext(writeCtx, `
		UPDATE ssl_certificates
		SET last_renewal_attempt = ?, renewal_status = ?, updated_at = datetime('now')
		WHERE id = ? AND status = 'active'`, at, sslWaitingForOwner, certID,
	); err != nil {
		log.Printf("cert renewal %s: record the waiting state: %v", domainName, err)
	}
}

// releaseCertificateWaitingForOwner ends "waiting_for_owner" once the site's
// file serves CelikPanel's text again, or the kept file names the active
// certificate. The ledger reason is cleared by the same observation.
func (p *Panel) releaseCertificateWaitingForOwner(ctx context.Context, domainID int) {
	if _, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE ssl_certificates SET renewal_status = '', updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active' AND renewal_status = ?`,
		domainID, sslWaitingForOwner); err != nil {
		log.Printf("site configuration of domain %d: end the certificate's waiting state: %v", domainID, err)
	}
}

// observeSiteFileCertificate is called with every recorded result of a site's
// file: CelikPanel's text in place ends both certificate reasons; a kept file
// that names the active certificate ends the "certificate" one.
func (p *Panel) observeSiteFileCertificate(ctx context.Context, siteID, domainID int, previousReason string, file transport.SiteFileResult) {
	if !services.SiteFileCertificateReason(previousReason) {
		return
	}
	resolved := siteFileApplied(file.Outcome) ||
		(previousReason == services.SiteFileReasonCertificate &&
			file.Outcome == transport.SiteFileOutcomeKept && file.CertificateReferenced)
	if !resolved {
		return
	}
	if _, err := services.SetSiteFileCertificateReason(ctx, p.db.GetDB(), siteID, ""); err != nil {
		log.Printf("site configuration of domain %d: clear the certificate reason: %v", domainID, err)
	}
	// A renewal that waited can go ahead at its next scheduled run; the
	// certificate the file now names is the active one.
	p.releaseCertificateWaitingForOwner(ctx, domainID)
}

// siteConfigCertificate is the site-config page's certificate part.
type siteConfigCertificate struct {
	// CertPath and KeyPath: the active certificate in the Panel's store, for
	// the owner who updates the certificate lines by hand.
	CertPath  string `json:"cert_path,omitempty"`
	KeyPath   string `json:"key_path,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	// ServedExpiresAt and ServedDaysLeft are of the certificate the kept file
	// most likely still serves: the previous one for "certificate", the
	// active one for "certificate_validation".
	ServedExpiresAt string `json:"served_expires_at,omitempty"`
	ServedDaysLeft  *int   `json:"served_days_left,omitempty"`
	// Referenced: the kept file names the active certificate.
	Referenced bool `json:"referenced"`
}

// siteConfigCertificateFor reads the certificate part of the view; nil when
// the file has no certificate reason.
func (p *Panel) siteConfigCertificateFor(ctx context.Context, domainID int, reason string, referenced bool) *siteConfigCertificate {
	if !services.SiteFileCertificateReason(reason) {
		return nil
	}
	view := &siteConfigCertificate{Referenced: referenced}
	var activeID int64
	var expires string
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT id, cert_path, key_path, expires_at FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active' ORDER BY id DESC LIMIT 1`, domainID).
		Scan(&activeID, &view.CertPath, &view.KeyPath, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return view
	}
	if err != nil {
		log.Printf("site configuration of domain %d: read the certificate: %v", domainID, err)
		return view
	}
	if parsed, ok := parseCertTime(expires); ok {
		view.ExpiresAt = parsed.UTC().Format(time.RFC3339)
	}
	served := expires
	if reason == services.SiteFileReasonCertificate {
		served = ""
		if err := p.db.GetDB().QueryRowContext(ctx, `
			SELECT expires_at FROM ssl_certificates
			WHERE domain_id = ? AND id < ? ORDER BY id DESC LIMIT 1`, domainID, activeID).
			Scan(&served); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("site configuration of domain %d: read the previous certificate: %v", domainID, err)
		}
	}
	if parsed, ok := parseCertTime(served); ok {
		view.ServedExpiresAt = parsed.UTC().Format(time.RFC3339)
		days := int(time.Until(parsed).Hours() / 24)
		view.ServedDaysLeft = &days
	}
	return view
}

// certificateWaitingReason is why a certificate waits for the owner: the
// ledger's "certificate" reason (a new certificate the kept file does not use
// yet), otherwise "certificate_validation". A renewal stopped by a missing or
// unreadable file writes no ledger reason and is the second kind too.
func (p *Panel) certificateWaitingReason(ctx context.Context, domainID int) string {
	if siteID, _, err := p.siteIDForDomain(ctx, domainID); err == nil {
		record, found, err := services.LoadSiteFileRecord(ctx, p.db.GetDB(), siteID, transport.SiteFileKindNginxVhost)
		if err == nil && found && record.StateReason == services.SiteFileReasonCertificate {
			return services.SiteFileReasonCertificate
		}
	}
	return services.SiteFileReasonCertificateValidation
}
