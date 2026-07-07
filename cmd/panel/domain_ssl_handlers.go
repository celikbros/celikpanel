package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// SSL Certificate Management Handlers

// SSLCertificate represents a domain's SSL certificate
type SSLCertificate struct {
	ID                 int        `json:"id"`
	DomainID           int        `json:"domain_id"`
	Type               string     `json:"type"`
	CertPath           string     `json:"cert_path"`
	KeyPath            string     `json:"key_path"`
	ChainPath          string     `json:"chain_path,omitempty"`
	Issuer             string     `json:"issuer"`
	Subject            string     `json:"subject"`
	IssuedAt           time.Time  `json:"issued_at"`
	ExpiresAt          time.Time  `json:"expires_at"`
	DaysUntilExpiry    int        `json:"days_until_expiry"`
	AutoRenew          bool       `json:"auto_renew"`
	LastRenewalAttempt *time.Time `json:"last_renewal_attempt,omitempty"`
	RenewalStatus      string     `json:"renewal_status,omitempty"`
	Status             string     `json:"status"`
}

// SSLSettings represents SSL settings for a domain
type SSLSettings struct {
	ForceHTTPS  bool `json:"force_https"`
	HSTSEnabled bool `json:"hsts_enabled"`
	HSTSMaxAge  int  `json:"hsts_max_age"`
}

// DomainSSLResponse represents the complete SSL status for a domain
type DomainSSLResponse struct {
	DomainID       int             `json:"domain_id"`
	DomainName     string          `json:"domain_name"`
	HasCertificate bool            `json:"has_certificate"`
	Certificate    *SSLCertificate `json:"certificate,omitempty"`
	Settings       SSLSettings     `json:"settings"`
}

// IssueLetsEncryptRequest represents a request to issue Let's Encrypt certificate
type IssueLetsEncryptRequest struct {
	Email     string `json:"email"`
	AutoRenew bool   `json:"auto_renew"`
}

// UploadCertificateRequest represents a request to upload custom certificate
type UploadCertificateRequest struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
	Chain       string `json:"chain,omitempty"`
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
	ctx := context.Background()
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

	// Get SSL settings from sites table
	var settings SSLSettings
	err = pool.QueryRowContext(ctx, `
		SELECT COALESCE(force_https, false),
		       COALESCE(hsts_enabled, false),
		       COALESCE(hsts_max_age, 31536000)
		FROM sites WHERE domain_id = ?
	`, domainID).Scan(&settings.ForceHTTPS, &settings.HSTSEnabled, &settings.HSTSMaxAge)

	if err != nil {
		// Site not found, use defaults
		settings = SSLSettings{
			ForceHTTPS:  false,
			HSTSEnabled: false,
			HSTSMaxAge:  31536000,
		}
	}
	response.Settings = settings

	// Get certificate info
	var cert SSLCertificate
	err = pool.QueryRowContext(ctx, `
		SELECT id, domain_id, type, cert_path, key_path, 
		       COALESCE(chain_path, ''), COALESCE(issuer, ''), COALESCE(subject, ''),
		       COALESCE(issued_at, datetime('now')), expires_at, auto_renew,
		       last_renewal_attempt, COALESCE(renewal_status, ''), status
		FROM ssl_certificates 
		WHERE domain_id = ? AND status = 'active'
		ORDER BY created_at DESC LIMIT 1
	`, domainID).Scan(
		&cert.ID, &cert.DomainID, &cert.Type, &cert.CertPath, &cert.KeyPath,
		&cert.ChainPath, &cert.Issuer, &cert.Subject,
		&cert.IssuedAt, &cert.ExpiresAt, &cert.AutoRenew,
		&cert.LastRenewalAttempt, &cert.RenewalStatus, &cert.Status,
	)

	if err == nil {
		// Calculate days until expiry
		cert.DaysUntilExpiry = int(time.Until(cert.ExpiresAt).Hours() / 24)
		response.HasCertificate = true
		response.Certificate = &cert
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

	var req IssueLetsEncryptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	pool := p.db.GetDB()

	// Get domain info
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Get site document root
	var documentRoot string
	err = pool.QueryRowContext(ctx, "SELECT document_root FROM sites WHERE domain_id = ?", domainID).Scan(&documentRoot)
	if err != nil {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}

	// Get domain aliases
	rows, err := pool.QueryContext(ctx, "SELECT alias FROM domain_aliases WHERE domain_id = ?", domainID)
	if err != nil {
		http.Error(w, "Failed to load aliases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var aliases []string
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err == nil {
			aliases = append(aliases, alias)
		}
	}

	// Call agent to issue certificate
	var agentResp struct {
		Success   bool      `json:"success"`
		CertPath  string    `json:"cert_path"`
		KeyPath   string    `json:"key_path"`
		ChainPath string    `json:"chain_path"`
		ExpiresAt time.Time `json:"expires_at"`
		Error     string    `json:"error"`
	}

	agentReq := struct {
		Domain    string   `json:"domain"`
		Aliases   []string `json:"aliases"`
		Email     string   `json:"email"`
		Webroot   string   `json:"webroot"`
		AutoRenew bool     `json:"auto_renew"`
	}{
		Domain:    domain.Name,
		Aliases:   aliases,
		Email:     req.Email,
		Webroot:   documentRoot,
		AutoRenew: req.AutoRenew,
	}

	err = p.agentClient.Call("Agent.IssueLetsEncryptCertificate", agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		writeAgentError(w, err, agentResp.Error)
		return
	}

	// Store certificate in database
	_, err = pool.ExecContext(ctx, `
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, chain_path,
			issuer, subject, expires_at, auto_renew, status
		) VALUES (?, 'letsencrypt', ?, ?, ?, 'Let''s Encrypt', ?, ?, ?, 'active')
	`, domainID, agentResp.CertPath, agentResp.KeyPath, agentResp.ChainPath,
		domain.Name, agentResp.ExpiresAt, req.AutoRenew)

	if err != nil {
		writeServerError(w, err)
		return
	}

	// Update site SSL status
	_, err = pool.ExecContext(ctx, `
		UPDATE sites
		SET ssl_enabled = true,
		    ssl_type = 'letsencrypt',
		    ssl_cert_path = ?,
		    ssl_key_path = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?
	`, agentResp.CertPath, agentResp.KeyPath, domainID)

	if err != nil {
		http.Error(w, "Failed to update site", http.StatusInternalServerError)
		return
	}

	// The certificate only matters once nginx serves it: regenerate the
	// vhost (adds the 443 block) and reload.
	// Sertifika ancak nginx sunduğunda işe yarar: vhost'u yeniden üret (443
	// bloğu eklenir) ve yeniden yükle.
	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		writeClientError(w, http.StatusConflict, "certificate issued but enabling it in nginx failed: "+err.Error())
		return
	}

	// Keep mail SNI in step with the new certificate if mail is secured.
	// Posta korunuyorsa posta SNI'ını yeni sertifikayla adımda tut.
	_ = p.resyncMailTLS(ctx)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "success",
		"expires_at": agentResp.ExpiresAt,
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

	// Parse multipart form
	err = r.ParseMultipartForm(10 << 20) // 10 MB max
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Read certificate file
	certFile, _, err := r.FormFile("certificate")
	if err != nil {
		http.Error(w, "Certificate file required", http.StatusBadRequest)
		return
	}
	defer certFile.Close()
	certContent, _ := io.ReadAll(certFile)

	// Read private key file
	keyFile, _, err := r.FormFile("private_key")
	if err != nil {
		http.Error(w, "Private key file required", http.StatusBadRequest)
		return
	}
	defer keyFile.Close()
	keyContent, _ := io.ReadAll(keyFile)

	// Read chain file (optional)
	var chainContent []byte
	chainFile, _, err := r.FormFile("chain")
	if err == nil {
		defer chainFile.Close()
		chainContent, _ = io.ReadAll(chainFile)
	}

	ctx := context.Background()
	pool := p.db.GetDB()

	// Get domain name
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Validate certificate via agent
	var validateResp struct {
		Valid     bool      `json:"valid"`
		Issuer    string    `json:"issuer"`
		Subject   string    `json:"subject"`
		IssuedAt  time.Time `json:"issued_at"`
		ExpiresAt time.Time `json:"expires_at"`
		Error     string    `json:"error"`
	}

	validateReq := struct {
		CertContent string `json:"cert_content"`
		KeyContent  string `json:"key_content"`
		Domain      string `json:"domain"`
	}{
		CertContent: string(certContent),
		KeyContent:  string(keyContent),
		Domain:      domain.Name,
	}

	err = p.agentClient.Call("Agent.ValidateCertificate", validateReq, &validateResp)
	if err != nil || !validateResp.Valid {
		errorMsg := "Invalid certificate"
		if validateResp.Error != "" {
			errorMsg = validateResp.Error
		}
		http.Error(w, errorMsg, http.StatusBadRequest)
		return
	}

	// Install certificate via agent
	var installResp struct {
		Success   bool   `json:"success"`
		CertPath  string `json:"cert_path"`
		KeyPath   string `json:"key_path"`
		ChainPath string `json:"chain_path"`
		Error     string `json:"error"`
	}

	installReq := struct {
		Domain       string `json:"domain"`
		CertContent  string `json:"cert_content"`
		KeyContent   string `json:"key_content"`
		ChainContent string `json:"chain_content"`
	}{
		Domain:       domain.Name,
		CertContent:  string(certContent),
		KeyContent:   string(keyContent),
		ChainContent: string(chainContent),
	}

	err = p.agentClient.Call("Agent.InstallCustomCertificate", installReq, &installResp)
	if err != nil || !installResp.Success {
		writeAgentError(w, err, installResp.Error)
		return
	}

	// Store in database
	_, err = pool.ExecContext(ctx, `
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, chain_path,
			issuer, subject, issued_at, expires_at, auto_renew, status
		) VALUES (?, 'custom', ?, ?, ?, ?, ?, ?, ?, false, 'active')
	`, domainID, installResp.CertPath, installResp.KeyPath, installResp.ChainPath,
		validateResp.Issuer, validateResp.Subject, validateResp.IssuedAt, validateResp.ExpiresAt)

	if err != nil {
		http.Error(w, "Failed to store certificate", http.StatusInternalServerError)
		return
	}

	// Update site
	_, err = pool.ExecContext(ctx, `
		UPDATE sites
		SET ssl_enabled = true,
		    ssl_type = 'custom',
		    ssl_cert_path = ?,
		    ssl_key_path = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?
	`, installResp.CertPath, installResp.KeyPath, domainID)
	if err != nil {
		http.Error(w, "Failed to update site", http.StatusInternalServerError)
		return
	}

	// The certificate only matters once nginx serves it: regenerate the
	// vhost (adds the 443 block) and reload. Without this the cert sat on
	// disk while the site stayed HTTP-only.
	// Sertifika ancak nginx sunduğunda işe yarar: vhost'u yeniden üret (443
	// bloğu eklenir) ve yeniden yükle. Bu olmadan sertifika diskte dururken
	// site yalnız HTTP kalıyordu.
	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		writeClientError(w, http.StatusConflict, "certificate installed but enabling it in nginx failed: "+err.Error())
		return
	}

	_ = p.resyncMailTLS(ctx)
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

	var req UpdateSSLSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	pool := p.db.GetDB()

	// Update settings
	_, err = pool.ExecContext(ctx, `
		UPDATE sites
		SET force_https = ?,
		    hsts_enabled = ?,
		    hsts_max_age = ?,
		    updated_at = datetime('now')
		WHERE domain_id = ?
	`, req.ForceHTTPS, req.HSTSEnabled, req.HSTSMaxAge, domainID)

	if err != nil {
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	// TODO: Regenerate nginx config

	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleDeleteSSL handles DELETE /api/v1/domains/:id/ssl
func (p *Panel) handleDeleteSSL(w http.ResponseWriter, r *http.Request, domainID int) {
	ctx := context.Background()
	pool := p.db.GetDB()

	// Mark certificate as revoked
	_, err := pool.ExecContext(ctx, `
		UPDATE ssl_certificates
		SET status = 'revoked', updated_at = datetime('now')
		WHERE domain_id = ? AND status = 'active'
	`, domainID)

	if err != nil {
		http.Error(w, "Failed to revoke certificate", http.StatusInternalServerError)
		return
	}

	// Update site
	_, err = pool.ExecContext(ctx, `
		UPDATE sites
		SET ssl_enabled = false,
		    ssl_cert_path = NULL,
		    ssl_key_path = NULL,
		    updated_at = datetime('now')
		WHERE domain_id = ?
	`, domainID)

	if err != nil {
		http.Error(w, "Failed to update site", http.StatusInternalServerError)
		return
	}

	// TODO: Regenerate nginx config

	// A removed certificate must drop out of the mail SNI maps too.
	// Kaldırılan sertifika posta SNI map'lerinden de düşmelidir.
	_ = p.resyncMailTLS(r.Context())
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
