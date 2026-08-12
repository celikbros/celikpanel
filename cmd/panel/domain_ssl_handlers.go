package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	maxCustomCertificateUploadBytes int64 = 3 << 20
	maxCertificatePEMBytes          int64 = 1 << 20
	maxPrivateKeyPEMBytes           int64 = 512 << 10
	maxCertificateChainPEMBytes     int64 = 1 << 20
)

func readCustomCertificatePart(
	r *http.Request,
	field string,
	required bool,
	maxBytes int64,
) ([]byte, error) {
	file, header, err := r.FormFile(field)
	if err != nil {
		if !required && errors.Is(err, http.ErrMissingFile) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	if header.Size > maxBytes {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", field, maxBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", field, err)
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", field, maxBytes)
	}
	if required && len(content) == 0 {
		return nil, fmt.Errorf("%s is empty", field)
	}
	return content, nil
}

// handleACMEProviders: GET /api/v1/ssl/providers — the CA list the SSL
// dialog's provider dropdown reads. Any signed-in user issuing a cert needs
// it, so it is not admin-gated.
// handleACMEProviders: GET /api/v1/ssl/providers — SSL penceresinin sağlayıcı
// menüsünün okuduğu CA listesi. Sertifika alan her oturumlu kullanıcıya
// gerektiğinden admin-kilitli değildir.
func (p *Panel) handleACMEProviders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"providers": core.ACMEProviders})
}

// SSL Certificate Management Handlers

// SSLCertificate represents a domain's SSL certificate
type SSLCertificate struct {
	ID                 int        `json:"id"`
	DomainID           int        `json:"domain_id"`
	Type               string     `json:"type"`
	CertPath           string     `json:"-"`
	KeyPath            string     `json:"-"`
	ChainPath          string     `json:"-"`
	Issuer             string     `json:"issuer"`
	Subject            string     `json:"subject"`
	IssuedAt           time.Time  `json:"issued_at"`
	ExpiresAt          time.Time  `json:"expires_at"`
	DaysUntilExpiry    int        `json:"days_until_expiry"`
	AutoRenew          bool       `json:"auto_renew"`
	LastRenewalAttempt *time.Time `json:"last_renewal_attempt,omitempty"`
	RenewalStatus      string     `json:"renewal_status,omitempty"`
	Status             string     `json:"status"`
	DNSNames           []string   `json:"dns_names"`
	ProviderID         string     `json:"provider_id,omitempty"`
	Activated          bool       `json:"activated"`
	Usable             bool       `json:"usable"`
	TrustStatus        string     `json:"trust_status"`
	TrustError         string     `json:"trust_error,omitempty"`
	ActivationPending  bool       `json:"activation_pending"`
	DependentsPending  bool       `json:"dependents_pending"`
}

// SSLSettings represents SSL settings for a domain
type SSLSettings struct {
	ForceHTTPS      bool       `json:"force_https"`
	HSTSEnabled     bool       `json:"hsts_enabled"`
	HSTSMaxAge      int        `json:"hsts_max_age"`
	HSTSRetireAfter *time.Time `json:"hsts_retire_after,omitempty"`
}

// DomainSSLResponse represents the complete SSL status for a domain
type DomainSSLResponse struct {
	DomainID       int             `json:"domain_id"`
	DomainName     string          `json:"domain_name"`
	HasCertificate bool            `json:"has_certificate"`
	Certificate    *SSLCertificate `json:"certificate,omitempty"`
	ManagedNames   []string        `json:"managed_names,omitempty"`
	Settings       SSLSettings     `json:"settings"`
}

const errCodeSSLActivationPending = "ssl_activation_pending"
const errCodeSSLDependentsPending = "ssl_dependents_pending"

// IssueLetsEncryptRequest represents a request to issue an ACME certificate.
// Provider selects the CA (empty = Let's Encrypt); the name is kept for
// compatibility even though it now issues from any registered ACME provider.
// IssueLetsEncryptRequest bir ACME sertifikası isteğidir. Provider CA'yı
// seçer (boş = Let's Encrypt); ad, artık kayıtlı herhangi bir ACME
// sağlayıcısından verse de uyumluluk için korunur.
type IssueLetsEncryptRequest struct {
	Email       string `json:"email"`
	AutoRenew   bool   `json:"auto_renew"`
	IncludeMail bool   `json:"include_mail,omitempty"`
	Provider    string `json:"provider,omitempty"`
	EABKeyID    string `json:"eab_key_id,omitempty"`
	EABHMACKey  string `json:"eab_hmac_key,omitempty"`
	Reissue     bool   `json:"reissue,omitempty"`
}

// UploadCertificateRequest represents a request to upload custom certificate
type UploadCertificateRequest struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
	Chain       string `json:"chain,omitempty"`
}

type customCertificateReplacementState struct {
	ForceHTTPS         bool
	HSTSEnabled        bool
	HSTSRetireAfter    *time.Time
	PreviousSecureMail bool
}

func (p *Panel) loadCustomCertificateReplacementState(
	ctx context.Context,
	domainID int,
) (customCertificateReplacementState, error) {
	var (
		state           customCertificateReplacementState
		hstsRetireAfter sql.NullString
	)
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(s.force_https, false),
		       COALESCE(s.hsts_enabled, false),
		       s.hsts_retire_after,
		       COALESCE((
		           SELECT MAX(c.secure_mail)
		           FROM ssl_certificates c
		           WHERE c.domain_id = s.domain_id AND c.status = 'active'
		       ), 0)
		FROM sites s
		WHERE s.domain_id = ?`, domainID).
		Scan(
			&state.ForceHTTPS,
			&state.HSTSEnabled,
			&hstsRetireAfter,
			&state.PreviousSecureMail,
		)
	if err != nil {
		return customCertificateReplacementState{}, err
	}
	state.HSTSRetireAfter, err = parseOptionalDBTime(hstsRetireAfter)
	if err != nil {
		return customCertificateReplacementState{},
			fmt.Errorf("read HSTS retirement state: %w", err)
	}
	return state, nil
}

func validateCustomCertificateReplacement(
	state customCertificateReplacementState,
	trustChecked, trusted bool,
	dnsNames []string,
	domain string,
	now time.Time,
) error {
	hstsRetirementActive := state.HSTSRetireAfter != nil &&
		state.HSTSRetireAfter.After(now)
	if (!trustChecked || !trusted) &&
		(state.ForceHTTPS || state.HSTSEnabled ||
			hstsRetirementActive || state.PreviousSecureMail) {
		return errors.New(
			"the uploaded certificate is not trusted and cannot replace the active certificate while HTTPS, HSTS retirement, or secure mail protection is active",
		)
	}
	if state.PreviousSecureMail {
		mailName, err := mailCertificateHostname(domain)
		if err != nil {
			return fmt.Errorf("derive mail certificate hostname: %w", err)
		}
		if !certificateCoversHostname(dnsNames, mailName) {
			return fmt.Errorf(
				"the uploaded certificate does not cover %s and cannot replace the active certificate while secure mail is enabled",
				mailName,
			)
		}
	}
	return nil
}

// UpdateSSLSettingsRequest represents a request to update SSL settings
type UpdateSSLSettingsRequest struct {
	ForceHTTPS  bool `json:"force_https"`
	HSTSEnabled bool `json:"hsts_enabled"`
	HSTSMaxAge  int  `json:"hsts_max_age"`
}

// handleDomainSSL handles GET for domain SSL status
func (p *Panel) handleDomainSSL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract domain ID
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/ssl")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	if r.Method != "GET" && r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Method == "GET" {
		p.handleGetDomainSSL(w, r, domainID)
	} else {
		p.handleDeleteSSL(w, r, domainID)
	}
}

func (p *Panel) handleGetDomainSSL(w http.ResponseWriter, r *http.Request, domainID int) {
	ctx := r.Context()
	pool := p.db.GetDB()

	// Get domain info
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	response := DomainSSLResponse{
		DomainID:       domain.ID,
		DomainName:     domain.Name,
		HasCertificate: false,
	}
	managedNames, err := p.managedSiteHostnames(ctx, domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	response.ManagedNames = managedNames

	// Get SSL settings from sites table
	var (
		settings        SSLSettings
		hstsRetireAfter sql.NullString
		siteActivated   bool
	)
	err = pool.QueryRowContext(ctx, `
		SELECT COALESCE(force_https, false),
		       COALESCE(hsts_enabled, false),
		       COALESCE(hsts_max_age, 31536000),
		       hsts_retire_after,
		       COALESCE(ssl_enabled, false)
		FROM sites WHERE domain_id = ?
	`, domainID).Scan(
		&settings.ForceHTTPS, &settings.HSTSEnabled,
		&settings.HSTSMaxAge, &hstsRetireAfter, &siteActivated,
	)

	if errors.Is(err, sql.ErrNoRows) {
		settings = SSLSettings{
			ForceHTTPS:  false,
			HSTSEnabled: false,
			HSTSMaxAge:  31536000,
		}
		siteActivated = false
	} else if err != nil {
		writeServerError(w, err)
		return
	}
	if hstsRetireAfter.Valid {
		retireAfter, parseErr := parseOptionalDBTime(hstsRetireAfter)
		if parseErr != nil {
			writeServerError(w, parseErr)
			return
		}
		settings.HSTSRetireAfter = retireAfter
	}
	response.Settings = settings

	// Get certificate info. The sqlite driver hands TEXT columns back as
	// strings and refuses to scan them into time.Time, so timestamps go
	// through strings + flexible parsing.
	// Sertifika bilgisini al. sqlite sürücüsü TEXT kolonları string döndürür
	// ve time.Time'a taramayı reddeder; bu yüzden zaman damgaları string +
	// esnek ayrıştırmadan geçer.
	var cert SSLCertificate
	var issuedAtStr, expiresAtStr string
	var lastRenewalStr *string
	err = pool.QueryRowContext(ctx, `
		SELECT id, domain_id, type, cert_path, key_path, 
		       COALESCE(chain_path, ''), COALESCE(issuer, ''), COALESCE(subject, ''),
		       COALESCE(acme_provider_id, ''),
		       COALESCE(issued_at, datetime('now')), expires_at, auto_renew,
		       last_renewal_attempt, COALESCE(renewal_status, ''), status
		FROM ssl_certificates 
		WHERE domain_id = ? AND status = 'active'
		ORDER BY created_at DESC LIMIT 1
	`, domainID).Scan(
		&cert.ID, &cert.DomainID, &cert.Type, &cert.CertPath, &cert.KeyPath,
		&cert.ChainPath, &cert.Issuer, &cert.Subject,
		&cert.ProviderID,
		&issuedAtStr, &expiresAtStr, &cert.AutoRenew,
		&lastRenewalStr, &cert.RenewalStatus, &cert.Status,
	)

	if err == nil {
		cert.IssuedAt = parseDBTime(issuedAtStr)
		cert.ExpiresAt = parseDBTime(expiresAtStr)
		if lastRenewalStr != nil {
			t := parseDBTime(*lastRenewalStr)
			cert.LastRenewalAttempt = &t
		}
		cert.DaysUntilExpiry = int(time.Until(cert.ExpiresAt).Hours() / 24)
		if cert.Type == "letsencrypt" && strings.TrimSpace(cert.ProviderID) == "" {
			cert.ProviderID = acmeProviderIDForIssuer(cert.Issuer)
		}
		runtime, runtimeErr := p.loadCertificateRuntimeStatus(ctx, domainID, managedNames)
		if runtimeErr != nil {
			log.Printf("SSL status domain %d: runtime state: %v", domainID, runtimeErr)
			cert.Activated = siteActivated
			cert.TrustStatus = "unknown"
			cert.TrustError = "certificate runtime status is unavailable"
		} else {
			cert.DNSNames = runtime.Info.DNSNames
			cert.Activated = runtime.Activated
			cert.Usable = runtime.Usable
			cert.ActivationPending = runtime.ActivationPending
			cert.DependentsPending = runtime.DependentsPending
			if runtime.Info.Error != "" || runtime.Info.TrustError != "" {
				log.Printf(
					"SSL status domain %d: certificate detail: validation=%q trust=%q",
					domainID, runtime.Info.Error, runtime.Info.TrustError,
				)
			}
			switch {
			case !runtime.Info.Valid:
				cert.TrustStatus = "invalid"
				cert.TrustError = "the installed certificate or private key is invalid"
			case !runtime.Info.TrustChecked:
				cert.TrustStatus = "unknown"
				cert.TrustError = "certificate trust could not be verified"
			case runtime.Info.Trusted:
				cert.TrustStatus = "trusted"
			default:
				cert.TrustStatus = "untrusted"
				cert.TrustError = "the certificate chain is not trusted by this server"
			}
		}
		response.HasCertificate = true
		response.Certificate = &cert
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(response)
}

// handleIssueLetsEncrypt handles POST /api/v1/domains/:id/ssl/letsencrypt
func (p *Panel) handleIssueLetsEncrypt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/ssl/letsencrypt")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()

	var req IssueLetsEncryptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx, cancel := sslDurableContext(r.Context())
	defer cancel()
	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		writeClientError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	pool := p.db.GetDB()

	// Get domain info
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// The privileged agent derives the ACME webroot from these immutable IDs;
	// a database path is never an RPC authority.
	var subscriptionID int
	err = pool.QueryRowContext(ctx, `
		SELECT d.subscription_id
		FROM sites s JOIN domains d ON d.id = s.domain_id
		WHERE s.domain_id = ?`, domainID).Scan(&subscriptionID)
	if err != nil {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}

	// The certificate and nginx must use the exact same name set. That includes
	// www for root domains and every explicit alias, but never invents
	// www.<hosted-subdomain>.
	managedNames, err := p.managedSiteHostnames(ctx, domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	aliases := append([]string(nil), managedNames[1:]...)
	var mailName string
	if req.IncludeMail {
		mailName, err = mailCertificateHostname(domain.Name)
		if err != nil {
			writeClientError(w, http.StatusBadRequest,
				"the domain cannot have a valid mail certificate hostname")
			return
		}
		aliases = append(aliases, mailName)
	}

	var (
		activeCount        int
		previousSecureMail bool
		currentCertPath    string
		currentLineageName string
		currentCertType    string
	)
	if err := pool.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MAX(secure_mail), 0) FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'`, domainID).
		Scan(&activeCount, &previousSecureMail); err != nil {
		writeServerError(w, err)
		return
	}
	if activeCount > 0 && !req.Reissue {
		writeClientError(w, http.StatusConflict,
			"an active certificate already exists -- use Reissue and confirm the replacement")
		return
	}
	if activeCount > 0 {
		if err := pool.QueryRowContext(ctx, `
			SELECT type, cert_path, COALESCE(lineage_name, ?)
			FROM ssl_certificates
			WHERE domain_id = ? AND status = 'active'
			ORDER BY id DESC LIMIT 1`,
			domain.Name, domainID,
		).Scan(
			&currentCertType,
			&currentCertPath,
			&currentLineageName,
		); err != nil {
			writeServerError(w, err)
			return
		}
	}

	// Call agent to issue certificate
	var agentResp transport.IssueLetsEncryptResponse

	// Empty keeps the documented Let's Encrypt default. A non-empty unknown
	// provider is a client error; never silently issue from a different CA.
	providerID := strings.TrimSpace(req.Provider)
	if providerID == "" {
		providerID = "letsencrypt"
	}
	provider := core.ACMEProviderByID(providerID)
	if provider == nil {
		writeClientError(w, http.StatusBadRequest, "unknown ACME provider")
		return
	}
	if !provider.NeedsEAB {
		// Never forward stale credentials from a previous provider selection.
		req.EABKeyID = ""
		req.EABHMACKey = ""
	}

	// A CA that requires EAB cannot issue without credentials — refuse here
	// with a clear message instead of letting certbot fail cryptically.
	// EAB isteyen bir CA, bilgiler olmadan veremez — certbot'un anlaşılmaz
	// biçimde patlamasına izin vermek yerine burada net mesajla reddet.
	if provider.NeedsEAB && (req.EABKeyID == "" || req.EABHMACKey == "") {
		writeClientError(w, http.StatusBadRequest,
			provider.Name+" needs EAB credentials — enter the key ID and HMAC key from your "+provider.Name+" account.")
		return
	}

	// Publish every requested name on port 80 before asking the CA to validate
	// it. The HTTP vhost keeps the ACME challenge path reachable even when
	// Force HTTPS is enabled.
	var validationChallengeNames []string
	if mailName != "" {
		validationChallengeNames = []string{mailName}
	}
	if err := p.applyVhostForDomainWithACMEChallengeNames(
		ctx,
		domainID,
		validationChallengeNames,
	); err != nil {
		log.Printf("SSL issue domain %d: prepare validation vhost: %v", domainID, err)
		writeClientError(w, http.StatusConflict,
			"certificate request was not started because the validation web server configuration could not be prepared")
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
			log.Printf("SSL issue domain %d: restore validation vhost: %v", domainID, err)
		}
	}()

	agentReq := transport.IssueLetsEncryptRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		Domain:              domain.Name,
		Aliases:             aliases,
		Email:               req.Email,
		SubscriptionID:      subscriptionID,
		DomainID:            domainID,
		AutoRenew:           req.AutoRenew,
		ForceRenewal:        activeCount > 0 && req.Reissue,
		StageLineage:        activeCount > 0 && req.Reissue,
		FreshLineage: activeCount > 0 && req.Reissue &&
			currentCertType != "letsencrypt",
		CurrentCertPath:    currentCertPath,
		CurrentLineageName: currentLineageName,
		ACMEServer:         provider.Directory,
		EABKeyID:           req.EABKeyID,
		EABHMACKey:         req.EABHMACKey,
	}

	err = p.callAgentContext(ctx, "Agent.IssueLetsEncryptCertificate", agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		cleanupCtx, cleanupCancel := sslCompensationContext()
		defer cleanupCancel()
		cleanupTarget := certificateCleanupTarget{
			Domain:          domain.Name,
			DeleteCanonical: activeCount == 0,
			LineageName:     agentResp.LineageName,
			CertPath:        agentResp.CertPath,
			KeyPath:         agentResp.KeyPath,
			ChainPath:       agentResp.ChainPath,
		}
		if cleanupTarget.DeleteCanonical {
			cleanupTarget.LineageName = ""
		}
		// A transport error may arrive after net/rpc decoded lineage/path
		// fields. Those exact returned identities still authorize cleanup.
		p.cleanupUncommittedCertificate(cleanupCtx, cleanupTarget)
		writeAgentError(w, err, agentResp.Error)
		return
	}
	keepIssuedLineage := false
	defer func() {
		if keepIssuedLineage {
			return
		}
		cleanupCtx, cleanupCancel := sslCompensationContext()
		defer cleanupCancel()
		cleanupTarget := certificateCleanupTarget{
			Domain:          domain.Name,
			DeleteCanonical: activeCount == 0,
			LineageName:     agentResp.LineageName,
			CertPath:        agentResp.CertPath,
			KeyPath:         agentResp.KeyPath,
			ChainPath:       agentResp.ChainPath,
		}
		if cleanupTarget.DeleteCanonical {
			cleanupTarget.LineageName = ""
		}
		p.cleanupUncommittedCertificate(cleanupCtx, cleanupTarget)
	}()
	if strings.TrimSpace(agentResp.CertPath) == "" ||
		strings.TrimSpace(agentResp.KeyPath) == "" ||
		strings.TrimSpace(agentResp.ChainPath) == "" {
		writeAgentError(w, nil,
			"the ACME agent returned incomplete immutable certificate paths")
		return
	}
	if err := validateIssuedCertificateLineage(
		domain.Name,
		domainID,
		activeCount > 0,
		agentResp.LineageName,
	); err != nil {
		writeAgentError(w, nil, err.Error())
		return
	}
	info, err := p.inspectManagedCertificate(
		ctx,
		domain.Name,
		agentResp.CertPath,
		agentResp.KeyPath,
		agentResp.ChainPath,
	)
	if err != nil {
		writeAgentError(w, err, "inspect issued certificate snapshot")
		return
	}
	if !info.Valid || !info.TrustChecked || !info.Trusted {
		detail := strings.TrimSpace(info.TrustError)
		if detail == "" {
			detail = strings.TrimSpace(info.Error)
		}
		if detail == "" {
			detail = "issued certificate trust validation failed"
		}
		writeAgentError(w, nil, detail)
		return
	}
	requestedNames := append([]string(nil), managedNames...)
	if mailName != "" {
		requestedNames = append(requestedNames, mailName)
	}
	if err := exactCertificateDNSNames(info.DNSNames, requestedNames); err != nil {
		writeAgentError(w, nil, err.Error())
		return
	}
	expiresAt := info.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = agentResp.ExpiresAt
	}
	if expiresAt.IsZero() {
		writeAgentError(w, nil, "issued certificate has no expiry")
		return
	}
	subject := strings.TrimSpace(info.Subject)
	if subject == "" {
		subject = domain.Name
	}
	secureMail := previousSecureMail &&
		certificateCoversHostname(info.DNSNames, "mail."+domain.Name)

	// Store certificate in database. issuer is the chosen CA's display name so
	// the SSL page shows who signed it, not a hardcoded "Let's Encrypt".
	// Sertifikayı veritabanına yaz. issuer, seçilen CA'nın görünen adıdır;
	// böylece SSL sayfası sabit "Let's Encrypt" değil, imzalayanı gösterir.
	_, err = p.activateCertificate(ctx, domainID, certificateInstall{
		DomainName:     domain.Name,
		Type:           "letsencrypt",
		CertPath:       agentResp.CertPath,
		KeyPath:        agentResp.KeyPath,
		ChainPath:      agentResp.ChainPath,
		LineageName:    agentResp.LineageName,
		ACMEProviderID: provider.ID,
		Issuer:         provider.Name,
		Subject:        subject,
		IssuedAt:       info.IssuedAt,
		ExpiresAt:      expiresAt,
		AutoRenew:      req.AutoRenew,
		SecureMail:     secureMail,
	})
	if err != nil {
		writeServerError(w, err)
		return
	}
	keepIssuedLineage = true
	// From this point the durable certificate ledger is authoritative. A
	// failed final apply is exposed as activation_pending and retried from
	// that state; restoring the pre-activation vhost would serve stale truth.
	validationVhostPrepared = false

	// The certificate only matters once nginx serves it: regenerate the
	// vhost (adds the 443 block) and reload.
	// Sertifika ancak nginx sunduğunda işe yarar: vhost'u yeniden üret (443
	// bloğu eklenir) ve yeniden yükle.
	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		// Certbot has already advanced its lineage. Keep the new immutable
		// snapshot as the active ledger entry and expose a durable retry state;
		// rolling back to the old DB row would make future renewals point at a
		// different lineage version and strand the new certificate.
		if markErr := p.markCertificatePendingDetached(
			ctx, domainID, sslPendingActivation, true,
		); markErr != nil {
			writeServerError(w, fmt.Errorf(
				"certificate activation failed: %v; pending state failed: %w",
				err, markErr,
			))
			return
		}
		p.audit(r, "ssl.issue.partial", "domain", domainID)
		writeCodedError(w, http.StatusConflict, errCodeSSLActivationPending,
			"certificate was issued, but the web server could not activate it; use Retry activation", "")
		return
	}

	// Keep mail SNI in step with the new certificate if mail is secured.
	// Posta korunuyorsa posta SNI'ını yeni sertifikayla adımda tut.
	if err := p.syncCertificateDependents(ctx, domainID); err != nil {
		if markErr := p.markCertificatePendingDetached(
			ctx, domainID, sslPendingDependents, false,
		); markErr != nil {
			writeServerError(w, fmt.Errorf(
				"certificate dependent sync failed: %v; pending state failed: %w",
				err, markErr,
			))
			return
		}
		p.audit(r, "ssl.issue.dependents_partial", "domain", domainID)
		writeCodedError(w, http.StatusConflict, errCodeSSLDependentsPending,
			"the website certificate is active, but mail TLS synchronization is pending; use Retry activation", "")
		return
	}
	if err := p.clearCertificatePendingDetached(ctx, domainID); err != nil {
		writeServerError(w, err)
		return
	}

	action := "ssl.issue:" + provider.ID
	if activeCount > 0 {
		action = "ssl.reissue:" + provider.ID
	}
	p.audit(r, action, "domain", domainID)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "success",
		"expires_at": expiresAt,
	})
}

// handleUploadCertificate handles POST /api/v1/domains/:id/ssl/upload
func (p *Panel) handleUploadCertificate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/ssl/upload")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCustomCertificateUploadBytes)
	err = r.ParseMultipartForm(512 << 10)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Certificate upload is too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	certContent, err := readCustomCertificatePart(
		r, "certificate", true, maxCertificatePEMBytes,
	)
	if err != nil {
		http.Error(w, "Certificate file is invalid: "+err.Error(), http.StatusBadRequest)
		return
	}
	keyContent, err := readCustomCertificatePart(
		r, "private_key", true, maxPrivateKeyPEMBytes,
	)
	if err != nil {
		http.Error(w, "Private key file is invalid: "+err.Error(), http.StatusBadRequest)
		return
	}
	chainContent, err := readCustomCertificatePart(
		r, "chain", false, maxCertificateChainPEMBytes,
	)
	if err != nil {
		http.Error(w, "Certificate chain file is invalid: "+err.Error(), http.StatusBadRequest)
		return
	}

	unlock := lockDomainSSLOperation(domainID)
	defer unlock()

	ctx, cancel := sslDurableContext(r.Context())
	defer cancel()
	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		writeClientError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	pool := p.db.GetDB()

	// Get domain name
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Validate certificate via agent
	var validateResp transport.ValidateCertResponse

	validateReq := transport.ValidateCertRequest{
		CertContent:  string(certContent),
		KeyContent:   string(keyContent),
		ChainContent: string(chainContent),
		Domain:       domain.Name,
	}

	err = p.callAgentContext(ctx, "Agent.ValidateCertificate", validateReq, &validateResp)
	if err != nil {
		writeAgentError(w, err, "custom certificate validation RPC")
		return
	}
	if !validateResp.Valid {
		if validateResp.Error != "" {
			log.Printf("custom certificate validation rejected for domain %d: %s", domainID, validateResp.Error)
		}
		message := strings.TrimSpace(validateResp.Error)
		if message == "" {
			message = "the certificate or private key is invalid"
		}
		writeClientError(w, http.StatusBadRequest, message)
		return
	}
	managedNames, err := p.managedSiteHostnames(ctx, domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	for _, name := range managedNames {
		if !certificateCoversHostname(validateResp.DNSNames, name) {
			writeClientError(w, http.StatusBadRequest,
				"the uploaded certificate does not cover managed hostname "+name)
			return
		}
	}
	replacementState, err := p.loadCustomCertificateReplacementState(ctx, domainID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	if err := validateCustomCertificateReplacement(
		replacementState,
		validateResp.TrustChecked,
		validateResp.Trusted,
		validateResp.DNSNames,
		domain.Name,
		time.Now().UTC(),
	); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}
	previousSecureMail := replacementState.PreviousSecureMail

	// Install certificate via agent
	var installResp transport.InstallCertResponse

	installReq := transport.InstallCertRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		Domain:              domain.Name,
		CertContent:         string(certContent),
		KeyContent:          string(keyContent),
		ChainContent:        string(chainContent),
	}

	keepInstalledSnapshot := false
	defer func() {
		if keepInstalledSnapshot {
			return
		}
		cleanupCtx, cleanupCancel := sslCompensationContext()
		defer cleanupCancel()
		p.cleanupUncommittedCertificate(
			cleanupCtx,
			certificateCleanupTarget{
				Domain:    domain.Name,
				CertPath:  installResp.CertPath,
				KeyPath:   installResp.KeyPath,
				ChainPath: installResp.ChainPath,
			},
		)
	}()
	err = p.callAgentContext(ctx, "Agent.InstallCustomCertificate", installReq, &installResp)
	if err != nil || !installResp.Success {
		writeAgentError(w, err, installResp.Error)
		return
	}

	if strings.TrimSpace(installResp.CertPath) == "" ||
		strings.TrimSpace(installResp.KeyPath) == "" {
		writeAgentError(
			w,
			nil,
			"custom certificate install returned incomplete managed snapshot paths",
		)
		return
	}
	installedInfo, err := p.inspectManagedCertificate(
		ctx,
		domain.Name,
		installResp.CertPath,
		installResp.KeyPath,
		installResp.ChainPath,
	)
	if err != nil {
		writeAgentError(w, err, "inspect installed custom certificate snapshot")
		return
	}
	if !installedInfo.Valid {
		detail := strings.TrimSpace(installedInfo.Error)
		if detail == "" {
			detail = "installed custom certificate snapshot is invalid"
		}
		writeAgentError(w, nil, detail)
		return
	}
	for _, name := range managedNames {
		if !certificateCoversHostname(installedInfo.DNSNames, name) {
			writeAgentError(
				w,
				nil,
				"installed custom certificate snapshot does not cover managed hostname",
			)
			return
		}
	}
	if err := validateCustomCertificateReplacement(
		replacementState,
		installedInfo.TrustChecked,
		installedInfo.Trusted,
		installedInfo.DNSNames,
		domain.Name,
		time.Now().UTC(),
	); err != nil {
		writeAgentError(
			w,
			err,
			"installed custom certificate snapshot failed the replacement guard",
		)
		return
	}
	if installedInfo.ExpiresAt.IsZero() {
		writeAgentError(
			w,
			nil,
			"installed custom certificate snapshot has no expiry",
		)
		return
	}
	installedSubject := strings.TrimSpace(installedInfo.Subject)
	if installedSubject == "" {
		installedSubject = domain.Name
	}

	_, err = p.activateCertificate(ctx, domainID, certificateInstall{
		DomainName: domain.Name,
		Type:       "custom",
		CertPath:   installResp.CertPath,
		KeyPath:    installResp.KeyPath,
		ChainPath:  installResp.ChainPath,
		Issuer:     installedInfo.Issuer,
		Subject:    installedSubject,
		IssuedAt:   installedInfo.IssuedAt,
		ExpiresAt:  installedInfo.ExpiresAt,
		SecureMail: previousSecureMail,
	})
	if err != nil {
		writeServerError(w, err)
		return
	}
	keepInstalledSnapshot = true

	// The certificate only matters once nginx serves it: regenerate the
	// vhost (adds the 443 block) and reload. Without this the cert sat on
	// disk while the site stayed HTTP-only.
	// Sertifika ancak nginx sunduğunda işe yarar: vhost'u yeniden üret (443
	// bloğu eklenir) ve yeniden yükle. Bu olmadan sertifika diskte dururken
	// site yalnız HTTP kalıyordu.
	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		if markErr := p.markCertificatePendingDetached(
			ctx, domainID, sslPendingActivation, true,
		); markErr != nil {
			writeServerError(w, fmt.Errorf(
				"custom certificate activation failed: %v; pending state failed: %w",
				err, markErr,
			))
			return
		}
		p.audit(r, "ssl.upload.partial", "domain", domainID)
		writeCodedError(w, http.StatusConflict, errCodeSSLActivationPending,
			"the custom certificate was saved, but the web server could not activate it; use Retry activation", "")
		return
	}

	if err := p.syncCertificateDependents(ctx, domainID); err != nil {
		if markErr := p.markCertificatePendingDetached(
			ctx, domainID, sslPendingDependents, false,
		); markErr != nil {
			writeServerError(w, fmt.Errorf(
				"custom certificate dependent sync failed: %v; pending state failed: %w",
				err, markErr,
			))
			return
		}
		p.audit(r, "ssl.upload.dependents_partial", "domain", domainID)
		writeCodedError(w, http.StatusConflict, errCodeSSLDependentsPending,
			"the custom certificate is active, but mail TLS synchronization is pending; use Retry activation", "")
		return
	}
	if err := p.clearCertificatePendingDetached(ctx, domainID); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "ssl.upload", "domain", domainID)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleSSLSettings handles POST /api/v1/domains/:id/ssl/settings
func (p *Panel) handleSSLSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/ssl/settings")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()

	var req UpdateSSLSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	pool := p.db.GetDB()

	var previous SSLSettings
	var previousHSTSRetireAfter sql.NullString
	if err := pool.QueryRowContext(ctx, `
		SELECT COALESCE(force_https, false), COALESCE(hsts_enabled, false),
		       COALESCE(hsts_max_age, 31536000), hsts_retire_after
		FROM sites WHERE domain_id = ?`, domainID).
		Scan(
			&previous.ForceHTTPS,
			&previous.HSTSEnabled,
			&previous.HSTSMaxAge,
			&previousHSTSRetireAfter,
		); err != nil {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}
	previous.HSTSRetireAfter, err = parseOptionalDBTime(previousHSTSRetireAfter)
	if err != nil {
		writeServerError(w, err)
		return
	}
	managedNames, err := p.managedSiteHostnames(ctx, domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	runtime, runtimeErr := p.loadCertificateRuntimeStatus(ctx, domainID, managedNames)
	hasUsableCertificate := runtimeErr == nil && runtime.Usable
	if runtimeErr != nil && !errors.Is(runtimeErr, sql.ErrNoRows) {
		writeServerError(w, runtimeErr)
		return
	}
	next := SSLSettings{
		ForceHTTPS: req.ForceHTTPS, HSTSEnabled: req.HSTSEnabled, HSTSMaxAge: req.HSTSMaxAge,
	}
	if err := validateSSLSettings(next, previous, hasUsableCertificate); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}
	next.HSTSRetireAfter = nextHSTSRetirement(previous, next, time.Now().UTC())

	if _, err = pool.ExecContext(ctx, `
		UPDATE sites
		SET force_https = ?,
		    hsts_enabled = ?,
		    hsts_max_age = ?,
		    hsts_retire_after = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?
	`, req.ForceHTTPS, req.HSTSEnabled, req.HSTSMaxAge,
		nullableDBTime(next.HSTSRetireAfter), domainID); err != nil {
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		rollbackCtx, cancel := sslCompensationContext()
		defer cancel()
		_, rollbackErr := pool.ExecContext(rollbackCtx, `
			UPDATE sites
			SET force_https = ?, hsts_enabled = ?, hsts_max_age = ?,
			    hsts_retire_after = ?,
			    updated_at = datetime('now')
			WHERE domain_id = ?`,
			previous.ForceHTTPS, previous.HSTSEnabled, previous.HSTSMaxAge,
			nullableDBTime(previous.HSTSRetireAfter), domainID)
		if rollbackErr == nil {
			rollbackErr = p.applyVhostForDomain(rollbackCtx, domainID)
		}
		if rollbackErr != nil {
			writeServerError(w, fmt.Errorf("SSL settings apply failed: %v; rollback failed: %w", err, rollbackErr))
			return
		}
		writeClientError(w, http.StatusConflict,
			"SSL settings were not saved because the web server rejected the new configuration")
		return
	}

	p.audit(r, fmt.Sprintf("ssl.settings:https=%t,hsts=%t,max-age=%d",
		req.ForceHTTPS, req.HSTSEnabled, req.HSTSMaxAge), "domain", domainID)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleDeleteSSL handles DELETE /api/v1/domains/:id/ssl
func (p *Panel) handleDeleteSSL(w http.ResponseWriter, r *http.Request, domainID int) {
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	ctx := r.Context()
	detach, err := p.detachCertificate(ctx, domainID)
	if err != nil {
		if message, guarded := hstsRemovalConflictMessage(err, "the certificate"); guarded {
			writeClientError(w, http.StatusConflict, message)
		} else if errors.Is(err, errNoActiveCertificate) {
			writeClientError(w, http.StatusConflict, "the domain has no active certificate")
		} else {
			writeServerError(w, err)
		}
		return
	}

	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		rollbackCtx, cancel := sslCompensationContext()
		defer cancel()
		rollbackErr := p.rollbackCertificateDetach(rollbackCtx, detach)
		if rollbackErr == nil {
			rollbackErr = p.applyVhostForDomain(rollbackCtx, domainID)
		}
		if rollbackErr != nil {
			writeServerError(w, fmt.Errorf("certificate detach failed: %v; rollback failed: %w", err, rollbackErr))
			return
		}
		writeClientError(w, http.StatusConflict,
			"certificate was not detached because the web server rejected the HTTP-only configuration")
		return
	}

	// A removed certificate must drop out of the mail SNI maps too.
	// Kaldırılan sertifika posta SNI map'lerinden de düşmelidir.
	if err := p.syncCertificateDependents(ctx, domainID); err != nil {
		rollbackCtx, cancel := sslCompensationContext()
		defer cancel()
		rollbackErr := p.rollbackCertificateDetach(rollbackCtx, detach)
		if rollbackErr == nil {
			rollbackErr = p.applyVhostForDomain(rollbackCtx, domainID)
		}
		if rollbackErr == nil {
			rollbackErr = p.syncCertificateDependents(rollbackCtx, domainID)
		}
		if rollbackErr != nil {
			writeServerError(w, fmt.Errorf(
				"certificate dependent cleanup failed: %v; rollback failed: %w",
				err, rollbackErr,
			))
			return
		}
		writeClientError(w, http.StatusConflict,
			"the certificate was not detached because mail TLS cleanup failed")
		return
	}
	p.audit(r, "ssl.detach", "domain", domainID)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleSSLRenewalSetting lets the user change the scheduler policy for an
// already-installed ACME certificate without needlessly reissuing it.
func (p *Panel) handleSSLRenewalSetting(
	w http.ResponseWriter,
	r *http.Request,
	domainID int,
) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AutoRenew *bool `json:"auto_renew"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AutoRenew == nil {
		writeClientError(w, http.StatusBadRequest, "auto_renew is required")
		return
	}

	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	result, err := p.db.GetDB().ExecContext(r.Context(), `
		UPDATE ssl_certificates
		SET auto_renew = ?, updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active' AND type = 'letsencrypt'`,
		*req.AutoRenew, domainID,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if changed == 0 {
		writeClientError(w, http.StatusConflict,
			"automatic renewal is available only for an active ACME certificate")
		return
	}
	p.audit(r, fmt.Sprintf("ssl.auto-renew:%t", *req.AutoRenew), "domain", domainID)
	json.NewEncoder(w).Encode(map[string]any{
		"status":     "success",
		"auto_renew": *req.AutoRenew,
	})
}

// handleRetrySSLActivation handles POST /api/v1/domains/:id/ssl/retry.
func (p *Panel) handleRetrySSLActivation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/ssl/retry")
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid domain ID")
		return
	}
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	ctx, cancel := sslDurableContext(r.Context())
	defer cancel()
	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		writeClientError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err := p.retryPendingCertificate(ctx, domainID); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}
	p.audit(r, "ssl.retry", "domain", domainID)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// parseDBTime accepts every timestamp format that has ever landed in the
// ssl_certificates table: RFC3339 (what we write now), SQLite's datetime()
// output, and Go's time.Time default String (written by an older bug).
// parseDBTime, ssl_certificates tablosuna bugüne dek düşmüş her zaman-damgası
// biçimini kabul eder: RFC3339 (şimdi yazdığımız), SQLite datetime() çıktısı
// ve Go time.Time varsayılan String'i (eski bir hatanın yazdığı).
func parseDBTime(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700 MST",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
