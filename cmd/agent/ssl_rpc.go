package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// SSL/TLS Certificate Management RPC Methods

// IssueLetsEncryptRequest represents a request to issue a Let's Encrypt certificate
type IssueLetsEncryptRequest struct {
	ExpectedBuildCommit string   `json:"expected_build_commit"`
	Domain         string   `json:"domain"`
	Aliases        []string `json:"aliases"`
	Email          string   `json:"email"`
	SubscriptionID int      `json:"subscription_id"`
	DomainID       int      `json:"domain_id"`
	AutoRenew      bool     `json:"auto_renew"`
	// ACMEServer is the CA directory URL (empty = Let's Encrypt default). The
	// panel resolves the chosen provider to this; the agent only relays it to
	// certbot. certbot writes it into the renewal config, so renewals keep
	// using the same CA without the panel re-specifying it.
	// ACMEServer, CA dizin URL'sidir (boş = Let's Encrypt varsayılanı). Panel
	// seçilen sağlayıcıyı buna çözer; agent yalnız certbot'a aktarır. certbot
	// bunu yenileme yapılandırmasına yazar, böylece yenilemeler panel yeniden
	// belirtmeden aynı CA'yı kullanmaya devam eder.
	ACMEServer string `json:"acme_server,omitempty"`
	// EABKeyID / EABHMACKey: external account binding, required by ZeroSSL and
	// Google. Only sent for those CAs. Not persisted anywhere — certbot binds
	// the account once at first issuance and renewals reuse it, so the HMAC
	// secret never needs to live in our database.
	// EABKeyID / EABHMACKey: dış hesap bağlaması; ZeroSSL ve Google ister.
	// Yalnız o CA'lar için gönderilir. Hiçbir yerde saklanmaz — certbot hesabı
	// ilk vermede bir kez bağlar ve yenilemeler onu yeniden kullanır; böylece
	// HMAC sırrı veritabanımızda yaşamak zorunda kalmaz.
	EABKeyID   string `json:"eab_key_id,omitempty"`
	EABHMACKey string `json:"eab_hmac_key,omitempty"`
	// ForceRenewal explicitly requests a replacement certificate even when
	// certbot considers the current certificate too new to renew. The panel
	// only sets this after a user-confirmed reissue.
	ForceRenewal bool `json:"force_renewal,omitempty"`
	// StageLineage issues a replacement under a fresh, agent-generated
	// certbot name. The currently active lineage/account is selected by its
	// durable name and immutable certificate fingerprint, so the old renewal
	// source remains untouched until the panel commits the new ledger row.
	StageLineage bool `json:"stage_lineage,omitempty"`
	// FreshLineage starts an isolated replacement without selecting an
	// existing certbot lineage. This is used only when a user explicitly
	// replaces an active custom certificate with ACME.
	FreshLineage       bool   `json:"fresh_lineage,omitempty"`
	CurrentCertPath    string `json:"current_cert_path,omitempty"`
	CurrentLineageName string `json:"current_lineage_name,omitempty"`
}

// IssueLetsEncryptResponse represents the response from issuing a certificate
type IssueLetsEncryptResponse struct {
	Success     bool      `json:"success"`
	CertPath    string    `json:"cert_path"`
	KeyPath     string    `json:"key_path"`
	ChainPath   string    `json:"chain_path"`
	ExpiresAt   time.Time `json:"expires_at"`
	DNSNames    []string  `json:"dns_names,omitempty"`
	LineageName string    `json:"lineage_name"`
	Error       string    `json:"error,omitempty"`
}

// RenewCertRequest represents a request to renew a certificate
type RenewCertRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Domain          string `json:"domain"`
	CurrentCertPath string `json:"current_cert_path,omitempty"`
	LineageName     string `json:"lineage_name,omitempty"`
	SubscriptionID  int    `json:"subscription_id"`
	DomainID        int    `json:"domain_id"`
}

// RenewCertResponse represents the response from renewing a certificate
type RenewCertResponse struct {
	Success     bool      `json:"success"`
	CertPath    string    `json:"cert_path"`
	KeyPath     string    `json:"key_path"`
	ChainPath   string    `json:"chain_path"`
	ExpiresAt   time.Time `json:"expires_at"`
	DNSNames    []string  `json:"dns_names,omitempty"`
	LineageName string    `json:"lineage_name"`
	Error       string    `json:"error,omitempty"`
}

// ValidateCertRequest represents a request to validate a certificate
type ValidateCertRequest struct {
	CertContent  string `json:"cert_content"`
	KeyContent   string `json:"key_content"`
	ChainContent string `json:"chain_content,omitempty"`
	Domain       string `json:"domain"`
}

// ValidateCertResponse represents the response from validating a certificate
type ValidateCertResponse struct {
	Valid        bool      `json:"valid"`
	Trusted      bool      `json:"trusted"`
	TrustChecked bool      `json:"trust_checked"`
	TrustError   string    `json:"trust_error,omitempty"`
	Issuer       string    `json:"issuer"`
	Subject      string    `json:"subject"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	DNSNames     []string  `json:"dns_names,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// InstallCertRequest represents a request to install a custom certificate
type InstallCertRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Domain       string `json:"domain"`
	CertContent  string `json:"cert_content"`
	KeyContent   string `json:"key_content"`
	ChainContent string `json:"chain_content,omitempty"`
}

// InstallCertResponse represents the response from installing a certificate
type InstallCertResponse struct {
	Success   bool   `json:"success"`
	CertPath  string `json:"cert_path"`
	KeyPath   string `json:"key_path"`
	ChainPath string `json:"chain_path,omitempty"`
	Error     string `json:"error,omitempty"`
}

const (
	siteCertbotConfigDir   = "/etc/celikpanel/certbot"
	siteCertbotWorkDir     = "/var/lib/celikpanel/certbot"
	siteCertbotLogsDir     = "/var/log/celikpanel/certbot"
	legacyCertbotConfigDir = "/etc/letsencrypt"
	certbotRenewTimeout    = 15 * time.Minute
)

var siteCertbotSlot = make(chan struct{}, 1)
var stagedLineageRandomRead = rand.Read

func acquireSiteCertbot() bool {
	select {
	case siteCertbotSlot <- struct{}{}:
		return true
	default:
		return false
	}
}

func releaseSiteCertbot() {
	<-siteCertbotSlot
}

type certbotStorage struct {
	ConfigDir string
	WorkDir   string
	LogsDir   string
}

func isolatedSiteCertbotStorage() certbotStorage {
	return certbotStorage{
		ConfigDir: siteCertbotConfigDir,
		WorkDir:   siteCertbotWorkDir,
		LogsDir:   siteCertbotLogsDir,
	}
}

func (storage certbotStorage) commandArgs() []string {
	return []string{
		"--config-dir", storage.ConfigDir,
		"--work-dir", storage.WorkDir,
		"--logs-dir", storage.LogsDir,
	}
}

func (storage certbotStorage) certificateDir(certName string) string {
	return filepath.Join(storage.ConfigDir, "live", certName)
}

func ensureIsolatedSiteCertbotStorage(storage certbotStorage) error {
	for _, dir := range []string{storage.ConfigDir, storage.WorkDir, storage.LogsDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create site certbot directory %s: %w", dir, err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("inspect site certbot directory %s: %w", dir, err)
		}
		if info.Mode().Type() == os.ModeSymlink || !info.IsDir() {
			return fmt.Errorf("site certbot path is not a safe directory: %s", dir)
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return fmt.Errorf("secure site certbot directory %s: %w", dir, err)
		}
	}
	return nil
}

func certbotLineageExists(configDir, certName string) bool {
	info, err := os.Lstat(filepath.Join(configDir, "renewal", certName+".conf"))
	return err == nil && info.Mode().IsRegular()
}

func certbotStorageForRenewal(
	certName string,
	currentCertPath string,
	isolated certbotStorage,
	legacyConfigDir string,
) (certbotStorage, error) {
	legacy := isolated
	legacy.ConfigDir = legacyConfigDir
	candidates := make([]certbotStorage, 0, 2)
	if certbotLineageExists(isolated.ConfigDir, certName) {
		candidates = append(candidates, isolated)
	}
	if certbotLineageExists(legacy.ConfigDir, certName) {
		candidates = append(candidates, legacy)
	}
	if len(candidates) == 0 {
		return isolated, nil
	}

	// A failed reissue can leave an isolated lineage while the database still
	// serves an older legacy lineage. Match the live leaf certificate to the
	// immutable snapshot recorded as active instead of blindly preferring one
	// store. This also prevents renewing the panel certificate merely because
	// it shares the same cert-name.
	if strings.TrimSpace(currentCertPath) != "" {
		activeFingerprint, err := certificateLeafFingerprint(currentCertPath)
		if err != nil {
			return certbotStorage{}, fmt.Errorf("inspect active certificate before renewal: %w", err)
		}
		for _, candidate := range candidates {
			matches, err := certbotLineageContainsFingerprint(
				candidate.ConfigDir, certName, activeFingerprint,
			)
			if err == nil && matches {
				return candidate, nil
			}
		}
		return certbotStorage{}, fmt.Errorf("active certificate does not match an available certbot lineage")
	}

	return candidates[0], nil
}

func certbotLineageContainsFingerprint(
	configDir string,
	certName string,
	fingerprint [sha256.Size]byte,
) (bool, error) {
	paths := []string{filepath.Join(configDir, "live", certName, "cert.pem")}
	archived, err := filepath.Glob(filepath.Join(configDir, "archive", certName, "cert*.pem"))
	if err != nil {
		return false, err
	}
	paths = append(paths, archived...)
	var lastErr error
	for _, path := range paths {
		candidate, err := certificateLeafFingerprint(path)
		if err != nil {
			lastErr = err
			continue
		}
		if candidate == fingerprint {
			return true, nil
		}
	}
	return false, lastErr
}

func certificateLeafFingerprint(path string) ([sha256.Size]byte, error) {
	var fingerprint [sha256.Size]byte
	content, err := os.ReadFile(path)
	if err != nil {
		return fingerprint, err
	}
	certificates, err := parseCertificateBundle(string(content))
	if err != nil {
		return fingerprint, err
	}
	return sha256.Sum256(certificates[0].Raw), nil
}

func buildCertbotIssueArgs(req IssueLetsEncryptRequest, webroot string) []string {
	return buildCertbotIssueArgsForStorage(
		req, webroot, isolatedSiteCertbotStorage(), req.Domain,
	)
}

func buildCertbotIssueArgsForStorage(
	req IssueLetsEncryptRequest,
	webroot string,
	storage certbotStorage,
	certName string,
) []string {
	args := []string{
		"certonly",
	}
	args = append(args, storage.commandArgs()...)
	args = append(args,
		"--webroot",
		"-w", webroot,
		"--agree-tos",
		"--non-interactive",
		"--cert-name", certName,
	)
	if strings.TrimSpace(req.Email) != "" {
		args = append(args, "--email", strings.TrimSpace(req.Email))
	}
	if req.ForceRenewal {
		args = append(args, "--force-renewal")
	}
	if req.ACMEServer != "" {
		args = append(args, "--server", req.ACMEServer)
	}
	if req.EABKeyID != "" && req.EABHMACKey != "" {
		args = append(args, "--eab-kid", req.EABKeyID, "--eab-hmac-key", req.EABHMACKey)
	}

	seen := make(map[string]struct{}, len(req.Aliases)+1)
	for _, domain := range append([]string{req.Domain}, req.Aliases...) {
		domain = strings.TrimSpace(domain)
		key := strings.ToLower(domain)
		if domain == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		args = append(args, "-d", domain)
	}
	return args
}

func normalizeCurrentLineageName(
	raw string,
	domain string,
	domainID int,
) (string, error) {
	lineage := strings.ToLower(strings.TrimSpace(raw))
	if lineage == "" {
		return domain, nil
	}
	if lineage == domain {
		return lineage, nil
	}
	if !validStagedSiteLineage.MatchString(lineage) ||
		!strings.HasPrefix(lineage, fmt.Sprintf("cp-site-%d-", domainID)) {
		return "", fmt.Errorf("invalid certificate lineage identity")
	}
	return lineage, nil
}

func newStagedSiteLineage(domainID int) (string, error) {
	if domainID <= 0 {
		return "", fmt.Errorf("invalid domain identity")
	}
	random := make([]byte, 12)
	n, err := stagedLineageRandomRead(random)
	if err != nil {
		return "", fmt.Errorf("generate staged lineage identity: %w", err)
	}
	if n != len(random) {
		return "", fmt.Errorf("generate staged lineage identity: short random read")
	}
	return fmt.Sprintf("cp-site-%d-%x", domainID, random), nil
}

func selectIssueLineage(
	req IssueLetsEncryptRequest,
	isolated certbotStorage,
	legacyConfigDir string,
) (certbotStorage, string, error) {
	storage := isolated
	lineageName := req.Domain
	if req.FreshLineage && (!req.StageLineage || !req.ForceRenewal) {
		return storage, "", fmt.Errorf(
			"fresh lineage requires an explicit staged replacement request",
		)
	}
	if !req.StageLineage {
		return storage, lineageName, nil
	}
	if !req.ForceRenewal {
		return storage, "", fmt.Errorf(
			"staged lineage requires an explicit replacement request",
		)
	}
	if !req.FreshLineage {
		if strings.TrimSpace(req.CurrentCertPath) == "" {
			return storage, "", fmt.Errorf(
				"staged lineage requires the active immutable certificate path",
			)
		}
		currentLineage, err := normalizeCurrentLineageName(
			req.CurrentLineageName, req.Domain, req.DomainID,
		)
		if err != nil {
			return storage, "", err
		}
		storage, err = certbotStorageForExistingLineage(
			currentLineage,
			req.CurrentCertPath,
			storage,
			legacyConfigDir,
		)
		if err != nil {
			return storage, "", err
		}
	}
	lineageName, err := newStagedSiteLineage(req.DomainID)
	if err != nil {
		return storage, "", err
	}
	return storage, lineageName, nil
}

func certbotStorageForExistingLineage(
	lineageName string,
	currentCertPath string,
	isolated certbotStorage,
	legacyConfigDir string,
) (certbotStorage, error) {
	storage, err := certbotStorageForRenewal(
		lineageName, currentCertPath, isolated, legacyConfigDir,
	)
	if err != nil {
		return certbotStorage{}, err
	}
	if !certbotLineageExists(storage.ConfigDir, lineageName) {
		return certbotStorage{}, fmt.Errorf(
			"active certbot lineage %q is not available", lineageName,
		)
	}
	return storage, nil
}

func normalizeCertificateAliases(domain string, aliases []string) ([]string, error) {
	if len(aliases) > 99 {
		return nil, fmt.Errorf("a certificate may contain at most 100 DNS names")
	}
	seen := map[string]struct{}{domain: {}}
	normalized := make([]string, 0, len(aliases))
	for _, raw := range aliases {
		alias, err := canonicalCertificateDomain(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid certificate alias: %w", err)
		}
		if _, exists := seen[alias]; exists {
			continue
		}
		seen[alias] = struct{}{}
		normalized = append(normalized, alias)
	}
	return normalized, nil
}

func buildCertbotRenewArgs(certName string, storage certbotStorage, webroot string) []string {
	args := []string{"renew"}
	args = append(args, storage.commandArgs()...)
	return append(args,
		"--cert-name", certName,
		"--webroot",
		"-w", webroot,
		"--no-random-sleep-on-renew",
		"--non-interactive",
	)
}

func certbotRenewContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, certbotRenewTimeout)
}

// IssueLetsEncryptCertificate issues a new Let's Encrypt certificate using certbot
func (a *Agent) IssueLetsEncryptCertificate(req IssueLetsEncryptRequest, resp *IssueLetsEncryptResponse) error {
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "issuing a site certificate"); err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}

	domain, err := canonicalCertificateDomain(req.Domain)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}
	req.Domain = domain
	req.Aliases, err = normalizeCertificateAliases(domain, req.Aliases)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}
	webroot, err := prepareACMEChallengeRoot(req.SubscriptionID, req.DomainID)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("prepare identity-derived ACME challenge root: %v", err)
		return nil
	}
	if !acquireSiteCertbot() {
		resp.Success = false
		resp.Error = "another site certificate operation is already running; retry shortly"
		return nil
	}
	defer releaseSiteCertbot()

	storage := isolatedSiteCertbotStorage()
	if err := ensureIsolatedSiteCertbotStorage(storage); err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to prepare site certbot storage: %v", err)
		return nil
	}
	storage, lineageName, err := selectIssueLineage(
		req, storage, legacyCertbotConfigDir,
	)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}
	// Return the exact isolated lineage identity even when certbot later
	// fails. The panel can then request idempotent cleanup of any partial
	// certbot state without guessing a path or name.
	resp.LineageName = lineageName
	args := buildCertbotIssueArgsForStorage(
		req, webroot, storage, lineageName,
	)

	// Execute certbot
	ctx, cancel := certbotRenewContext(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "certbot", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		if ctx.Err() == context.DeadlineExceeded {
			resp.Error = fmt.Sprintf("certbot issue timed out after %s\nOutput: %s", certbotRenewTimeout, string(output))
			return nil
		}
		resp.Error = fmt.Sprintf("certbot failed: %v\nOutput: %s", err, string(output))
		return nil
	}

	paths, cert, err := snapshotCertbotCertificateFromLineage(
		lineageName, req.Domain, storage,
	)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to snapshot issued certificate: %v", err)
		return nil
	}

	resp.CertPath = paths.Fullchain
	resp.KeyPath = paths.Key
	resp.ChainPath = paths.Chain
	resp.ExpiresAt = cert.NotAfter
	resp.DNSNames = append([]string(nil), cert.DNSNames...)
	resp.Success = true
	return nil
}

// RenewLetsEncryptCertificate renews an existing Let's Encrypt certificate
func (a *Agent) RenewLetsEncryptCertificate(req RenewCertRequest, resp *RenewCertResponse) error {
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "renewing a site certificate"); err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}

	domain, err := canonicalCertificateDomain(req.Domain)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}
	lineageName, err := normalizeCurrentLineageName(
		req.LineageName, domain, req.DomainID,
	)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}
	webroot, err := prepareACMEChallengeRoot(req.SubscriptionID, req.DomainID)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("prepare identity-derived ACME challenge root: %v", err)
		return nil
	}
	if !acquireSiteCertbot() {
		resp.Success = false
		resp.Error = "another site certificate operation is already running; retry shortly"
		return nil
	}
	defer releaseSiteCertbot()
	isolated := isolatedSiteCertbotStorage()
	if err := ensureIsolatedSiteCertbotStorage(isolated); err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to prepare site certbot storage: %v", err)
		return nil
	}
	storage, err := certbotStorageForExistingLineage(
		lineageName, req.CurrentCertPath, isolated, legacyCertbotConfigDir,
	)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}

	// Execute certbot renew for specific domain
	// No --force-renewal: certbot only renews near expiry, which respects
	// Let's Encrypt rate limits even if this is called too often.
	// --force-renewal yok: certbot yalnız bitime yakın yeniler; bu, çok sık
	// çağrılsa bile Let's Encrypt hız sınırlarına saygı gösterir.
	ctx, cancel := certbotRenewContext(context.Background())
	defer cancel()
	cmd := exec.CommandContext(
		ctx, "certbot", buildCertbotRenewArgs(lineageName, storage, webroot)...,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		if ctx.Err() == context.DeadlineExceeded {
			resp.Error = fmt.Sprintf("certbot renew timed out after %s\nOutput: %s", certbotRenewTimeout, string(output))
			return nil
		}
		resp.Error = fmt.Sprintf("certbot renew failed: %v\nOutput: %s", err, string(output))
		return nil
	}

	paths, cert, err := snapshotCertbotCertificateFromLineage(
		lineageName, domain, storage,
	)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to snapshot renewed certificate: %v", err)
		return nil
	}

	resp.CertPath = paths.Fullchain
	resp.KeyPath = paths.Key
	resp.ChainPath = paths.Chain
	resp.ExpiresAt = cert.NotAfter
	resp.DNSNames = append([]string(nil), cert.DNSNames...)
	resp.LineageName = lineageName
	resp.Success = true
	return nil
}

func parsePrivateKey(content string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(content))
	if block == nil {
		return nil, fmt.Errorf("invalid private key PEM format")
	}

	var key any
	var err error
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported private key PEM type %q", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("unsupported private key type %T", key)
	}
	switch signer.Public().(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey, ed25519.PublicKey:
		return signer, nil
	default:
		return nil, fmt.Errorf("unsupported private key type %T", key)
	}
}

func certificateMatchesPrivateKey(cert *x509.Certificate, key crypto.Signer) (bool, error) {
	certPublicKey, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return false, fmt.Errorf("failed to encode certificate public key: %w", err)
	}
	privatePublicKey, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return false, fmt.Errorf("failed to encode private key public key: %w", err)
	}
	return bytes.Equal(certPublicKey, privatePublicKey), nil
}

func populateCertificateInfo(cert *x509.Certificate, resp *ValidateCertResponse) {
	resp.Valid = true
	resp.Issuer = cert.Issuer.CommonName
	resp.Subject = cert.Subject.CommonName
	resp.IssuedAt = cert.NotBefore
	resp.ExpiresAt = cert.NotAfter
	resp.DNSNames = append([]string(nil), cert.DNSNames...)
}

func parseCertificateBundle(contents ...string) ([]*x509.Certificate, error) {
	var certificates []*x509.Certificate
	for _, content := range contents {
		rest := []byte(content)
		for len(rest) > 0 {
			block, remaining := pem.Decode(rest)
			if block == nil {
				break
			}
			rest = remaining
			if block.Type != "CERTIFICATE" {
				continue
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse certificate: %w", err)
			}
			certificates = append(certificates, certificate)
		}
	}
	if len(certificates) == 0 {
		return nil, fmt.Errorf("invalid certificate PEM format")
	}
	return certificates, nil
}

func verifyCertificateTrust(leaf *x509.Certificate, bundle []*x509.Certificate, resp *ValidateCertResponse) {
	intermediates := x509.NewCertPool()
	for _, certificate := range bundle {
		if certificate == nil || bytes.Equal(certificate.Raw, leaf.Raw) {
			continue
		}
		intermediates.AddCert(certificate)
	}
	resp.TrustChecked = true
	if _, err := leaf.Verify(x509.VerifyOptions{Intermediates: intermediates}); err != nil {
		resp.Trusted = false
		resp.TrustError = err.Error()
		return
	}
	resp.Trusted = true
	resp.TrustError = ""
}

// ValidateCertificate validates a certificate and extracts information
func (a *Agent) ValidateCertificate(req ValidateCertRequest, resp *ValidateCertResponse) error {
	certificates, err := parseCertificateBundle(req.CertContent, req.ChainContent)
	if err != nil {
		resp.Valid = false
		resp.Error = err.Error()
		return nil
	}
	cert := certificates[0]

	// Validate certificate matches domain
	if err := cert.VerifyHostname(req.Domain); err != nil {
		resp.Valid = false
		resp.Error = fmt.Sprintf("certificate does not match domain %s: %v", req.Domain, err)
		return nil
	}

	// Check if certificate is expired
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		resp.Valid = false
		resp.Error = "certificate is expired or not yet valid"
		return nil
	}

	privateKey, err := parsePrivateKey(req.KeyContent)
	if err != nil {
		resp.Valid = false
		resp.Error = err.Error()
		return nil
	}
	matches, err := certificateMatchesPrivateKey(cert, privateKey)
	if err != nil {
		resp.Valid = false
		resp.Error = err.Error()
		return nil
	}
	if !matches {
		resp.Valid = false
		resp.Error = "private key does not match certificate"
		return nil
	}

	// Extract certificate info
	populateCertificateInfo(cert, resp)
	verifyCertificateTrust(cert, certificates, resp)

	return nil
}

func canonicalCertificateDomain(domain string) (string, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" || len(domain) > 253 {
		return "", fmt.Errorf("invalid certificate domain")
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid certificate domain")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return "", fmt.Errorf("invalid certificate domain")
			}
		}
	}
	return domain, nil
}

func customCertificateDirectory(domain string) (string, error) {
	domain, err := canonicalCertificateDomain(domain)
	if err != nil {
		return "", err
	}
	root := os.Getenv("CELIKPANEL_CUSTOM_CERT_ROOT")
	if root == "" {
		root = "/etc/ssl/celikpanel"
	}
	return filepath.Join(root, domain), nil
}

func ensureManagedCertificateDirectory(domain string) (string, error) {
	certDir, err := customCertificateDirectory(domain)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(certDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create cert directory: %w", err)
	}
	certDirInfo, err := os.Lstat(certDir)
	if err != nil || certDirInfo.Mode().Type() == os.ModeSymlink || !certDirInfo.IsDir() {
		return "", fmt.Errorf("certificate directory is not a safe directory")
	}
	if err := os.Chmod(certDir, 0750); err != nil {
		return "", fmt.Errorf("failed to secure cert directory: %w", err)
	}
	return certDir, nil
}

func writeCertificateFile(path string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func pemSequence(parts ...string) []byte {
	var result strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if result.Len() > 0 {
			result.WriteByte('\n')
		}
		result.WriteString(part)
		result.WriteByte('\n')
	}
	return []byte(result.String())
}

type certificateVersionContent struct {
	Cert      []byte
	Key       []byte
	Chain     []byte
	Fullchain []byte
}

type certificateVersionPaths struct {
	Cert      string
	Key       string
	Chain     string
	Fullchain string
}

type certificateFileWriter func(path string, content []byte, mode os.FileMode) error

func newCertificateVersionContent(cert, key, chain string) certificateVersionContent {
	return certificateVersionContent{
		Cert:      pemSequence(cert),
		Key:       pemSequence(key),
		Chain:     pemSequence(chain),
		Fullchain: pemSequence(cert, chain),
	}
}

func certificateVersionID(content certificateVersionContent) string {
	hash := sha256.New()
	for _, part := range [][]byte{content.Cert, content.Key, content.Chain} {
		_, _ = fmt.Fprintf(hash, "%d:", len(part))
		_, _ = hash.Write(part)
	}
	return fmt.Sprintf("sha256-%x", hash.Sum(nil))
}

func pathsForCertificateVersion(dir string, hasChain bool) certificateVersionPaths {
	paths := certificateVersionPaths{
		Cert:      filepath.Join(dir, "cert.pem"),
		Key:       filepath.Join(dir, "privkey.pem"),
		Fullchain: filepath.Join(dir, "fullchain.pem"),
	}
	if hasChain {
		paths.Chain = filepath.Join(dir, "chain.pem")
	}
	return paths
}

func verifyCertificateFile(path string, content []byte, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Type() == os.ModeSymlink {
		return fmt.Errorf("certificate path is a symlink: %s", filepath.Base(path))
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("certificate path is not a regular file: %s", filepath.Base(path))
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != mode {
			return fmt.Errorf("certificate file has unsafe permissions: %s", filepath.Base(path))
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, content) {
		return fmt.Errorf("certificate file content does not match fingerprint: %s", filepath.Base(path))
	}
	return nil
}

func verifyCertificateVersion(dir string, content certificateVersionContent) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Type() == os.ModeSymlink || !info.IsDir() {
		return fmt.Errorf("certificate version path is not a directory")
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0750 {
			return fmt.Errorf("certificate version directory has unsafe permissions")
		}
	}

	paths := pathsForCertificateVersion(dir, len(content.Chain) > 0)
	if err := verifyCertificateFile(paths.Cert, content.Cert, 0644); err != nil {
		return err
	}
	if err := verifyCertificateFile(paths.Key, content.Key, 0600); err != nil {
		return err
	}
	if err := verifyCertificateFile(paths.Fullchain, content.Fullchain, 0644); err != nil {
		return err
	}
	if paths.Chain != "" {
		if err := verifyCertificateFile(paths.Chain, content.Chain, 0644); err != nil {
			return err
		}
	}
	return nil
}

func publishCertificateVersion(certDir string, content certificateVersionContent, writer certificateFileWriter) (certificateVersionPaths, error) {
	versionID := certificateVersionID(content)
	versionDir := filepath.Join(certDir, versionID)
	paths := pathsForCertificateVersion(versionDir, len(content.Chain) > 0)
	if writer == nil {
		return certificateVersionPaths{}, fmt.Errorf("certificate writer is required")
	}

	if _, err := os.Lstat(versionDir); err == nil {
		if err := verifyCertificateVersion(versionDir, content); err != nil {
			return certificateVersionPaths{}, fmt.Errorf("existing certificate version failed verification: %w", err)
		}
		return paths, nil
	} else if !os.IsNotExist(err) {
		return certificateVersionPaths{}, err
	}

	stagingDir, err := os.MkdirTemp(certDir, "."+versionID+".staging-")
	if err != nil {
		return certificateVersionPaths{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stagingDir)
		}
	}()
	if err := os.Chmod(stagingDir, 0750); err != nil {
		return certificateVersionPaths{}, err
	}

	stagingPaths := pathsForCertificateVersion(stagingDir, len(content.Chain) > 0)
	if err := writer(stagingPaths.Cert, content.Cert, 0644); err != nil {
		return certificateVersionPaths{}, fmt.Errorf("failed to stage certificate: %w", err)
	}
	if err := writer(stagingPaths.Key, content.Key, 0600); err != nil {
		return certificateVersionPaths{}, fmt.Errorf("failed to stage private key: %w", err)
	}
	if stagingPaths.Chain != "" {
		if err := writer(stagingPaths.Chain, content.Chain, 0644); err != nil {
			return certificateVersionPaths{}, fmt.Errorf("failed to stage certificate chain: %w", err)
		}
	}
	if err := writer(stagingPaths.Fullchain, content.Fullchain, 0644); err != nil {
		return certificateVersionPaths{}, fmt.Errorf("failed to stage full certificate chain: %w", err)
	}
	if err := verifyCertificateVersion(stagingDir, content); err != nil {
		return certificateVersionPaths{}, fmt.Errorf("staged certificate version failed verification: %w", err)
	}

	if err := os.Rename(stagingDir, versionDir); err != nil {
		if existingErr := verifyCertificateVersion(versionDir, content); existingErr == nil {
			return paths, nil
		}
		return certificateVersionPaths{}, fmt.Errorf("failed to publish certificate version: %w", err)
	}
	published = true
	return paths, nil
}

func snapshotCertbotCertificate(domain string, storage certbotStorage) (certificateVersionPaths, *x509.Certificate, error) {
	return snapshotCertbotCertificateFromLineage(domain, domain, storage)
}

func snapshotCertbotCertificateFromLineage(
	lineageName string,
	domain string,
	storage certbotStorage,
) (certificateVersionPaths, *x509.Certificate, error) {
	sourceDir := storage.certificateDir(lineageName)
	read := func(name string) ([]byte, error) {
		content, err := os.ReadFile(filepath.Join(sourceDir, name))
		if err != nil {
			return nil, fmt.Errorf("read certbot %s: %w", name, err)
		}
		return content, nil
	}

	certPEM, err := read("cert.pem")
	if err != nil {
		return certificateVersionPaths{}, nil, err
	}
	keyPEM, err := read("privkey.pem")
	if err != nil {
		return certificateVersionPaths{}, nil, err
	}
	chainPEM, err := read("chain.pem")
	if err != nil {
		return certificateVersionPaths{}, nil, err
	}

	content := newCertificateVersionContent(string(certPEM), string(keyPEM), string(chainPEM))
	block, _ := pem.Decode(content.Cert)
	if block == nil {
		return certificateVersionPaths{}, nil, fmt.Errorf("failed to decode certbot certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return certificateVersionPaths{}, nil, fmt.Errorf("failed to parse certbot certificate: %w", err)
	}
	privateKey, err := parsePrivateKey(string(content.Key))
	if err != nil {
		return certificateVersionPaths{}, nil, fmt.Errorf("invalid certbot private key: %w", err)
	}
	matches, err := certificateMatchesPrivateKey(cert, privateKey)
	if err != nil {
		return certificateVersionPaths{}, nil, err
	}
	if !matches {
		return certificateVersionPaths{}, nil, fmt.Errorf("certbot private key does not match certificate")
	}

	certDir, err := ensureManagedCertificateDirectory(domain)
	if err != nil {
		return certificateVersionPaths{}, nil, err
	}
	paths, err := publishCertificateVersion(certDir, content, writeCertificateFile)
	if err != nil {
		return certificateVersionPaths{}, nil, err
	}
	return paths, cert, nil
}

// InstallCustomCertificate installs a custom SSL certificate
func (a *Agent) InstallCustomCertificate(req InstallCertRequest, resp *InstallCertResponse) error {
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "installing a custom certificate"); err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}

	var validation ValidateCertResponse
	if err := a.ValidateCertificate(ValidateCertRequest{
		CertContent:  req.CertContent,
		KeyContent:   req.KeyContent,
		ChainContent: req.ChainContent,
		Domain:       req.Domain,
	}, &validation); err != nil {
		return err
	}
	if !validation.Valid {
		resp.Success = false
		resp.Error = validation.Error
		return nil
	}

	// Create certificate directory
	certDir, err := ensureManagedCertificateDirectory(req.Domain)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}

	content := newCertificateVersionContent(req.CertContent, req.KeyContent, req.ChainContent)
	paths, err := publishCertificateVersion(certDir, content, writeCertificateFile)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to publish certificate version: %v", err)
		return nil
	}

	resp.Success = true
	resp.CertPath = paths.Fullchain
	resp.KeyPath = paths.Key
	resp.ChainPath = paths.Chain

	return nil
}

// GetCertificateInfo retrieves information about an installed certificate
func (a *Agent) GetCertificateInfo(certPath string, resp *ValidateCertResponse) error {
	certData, err := os.ReadFile(certPath)
	if err != nil {
		resp.Valid = false
		resp.Error = fmt.Sprintf("failed to read certificate: %v", err)
		return nil
	}

	certificates, err := parseCertificateBundle(string(certData))
	if err != nil {
		resp.Valid = false
		resp.Error = err.Error()
		return nil
	}
	cert := certificates[0]
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		resp.Valid = false
		resp.Error = "certificate is expired or not yet valid"
		populateCertificateInfo(cert, resp)
		resp.Valid = false
		return nil
	}

	populateCertificateInfo(cert, resp)
	verifyCertificateTrust(cert, certificates, resp)

	return nil
}

type InspectCertificateRequest struct {
	Domain    string `json:"domain,omitempty"`
	CertPath  string `json:"cert_path"`
	KeyPath   string `json:"key_path"`
	ChainPath string `json:"chain_path,omitempty"`
}

var certificateVersionDirectoryRE = regexp.MustCompile(`^sha256-[0-9a-f]{64}$`)

func managedCertificateVersionPath(
	domain string,
	candidate string,
	expectedFile string,
) (string, error) {
	certDir, err := customCertificateDirectory(domain)
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(candidate)
	if raw == "" || raw != candidate {
		return "", fmt.Errorf("managed certificate path must be canonical")
	}
	cleaned := filepath.Clean(raw)
	if cleaned != raw {
		return "", fmt.Errorf("managed certificate path must be canonical")
	}
	if cleaned == "." || !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("managed certificate path must be absolute")
	}
	rel, err := filepath.Rel(certDir, cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve managed certificate path: %w", err)
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) != 2 || parts[1] != expectedFile ||
		!certificateVersionDirectoryRE.MatchString(parts[0]) {
		return "", fmt.Errorf("certificate path is outside the managed immutable snapshot")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("certificate path escapes the managed certificate directory")
	}
	return filepath.Join(certDir, parts[0]), nil
}

func verifyManagedCertificateVersionPath(
	domain string,
	candidate string,
) (string, error) {
	expectedFile := filepath.Base(strings.TrimSpace(candidate))
	switch expectedFile {
	case "cert.pem", "privkey.pem", "chain.pem", "fullchain.pem":
	default:
		return "", fmt.Errorf("managed certificate path has an invalid file name")
	}
	versionDir, err := managedCertificateVersionPath(
		domain, candidate, expectedFile,
	)
	if err != nil {
		return "", err
	}

	certDir := filepath.Dir(versionDir)
	for _, dir := range []string{filepath.Dir(certDir), certDir, versionDir} {
		info, err := os.Lstat(dir)
		if err != nil {
			return "", fmt.Errorf("inspect managed certificate directory: %w", err)
		}
		if info.Mode().Type() == os.ModeSymlink || !info.IsDir() {
			return "", fmt.Errorf("managed certificate directory is not a safe directory")
		}
	}
	read := func(name string) ([]byte, error) {
		content, err := os.ReadFile(filepath.Join(versionDir, name))
		if err != nil {
			return nil, err
		}
		return content, nil
	}
	cert, err := read("cert.pem")
	if err != nil {
		return "", fmt.Errorf("read managed certificate snapshot: %w", err)
	}
	key, err := read("privkey.pem")
	if err != nil {
		return "", fmt.Errorf("read managed private key snapshot: %w", err)
	}
	var chain []byte
	chainPath := filepath.Join(versionDir, "chain.pem")
	if _, err := os.Lstat(chainPath); err == nil {
		chain, err = read("chain.pem")
		if err != nil {
			return "", fmt.Errorf("read managed certificate chain snapshot: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect managed certificate chain snapshot: %w", err)
	}
	content := certificateVersionContent{
		Cert:      cert,
		Key:       key,
		Chain:     chain,
		Fullchain: pemSequence(string(cert), string(chain)),
	}
	if filepath.Base(versionDir) != certificateVersionID(content) {
		return "", fmt.Errorf("managed certificate snapshot fingerprint does not match its directory")
	}
	if err := verifyCertificateVersion(versionDir, content); err != nil {
		return "", fmt.Errorf("verify managed certificate snapshot: %w", err)
	}
	return versionDir, nil
}

func verifyManagedCertificateSnapshot(
	domain string,
	certPath string,
	keyPath string,
	chainPath string,
) error {
	versionDir, err := managedCertificateVersionPath(domain, certPath, "fullchain.pem")
	if err != nil {
		return err
	}
	keyVersionDir, err := managedCertificateVersionPath(domain, keyPath, "privkey.pem")
	if err != nil {
		return err
	}
	if keyVersionDir != versionDir {
		return fmt.Errorf("certificate and private key are from different immutable snapshots")
	}
	if strings.TrimSpace(chainPath) != "" {
		chainVersionDir, err := managedCertificateVersionPath(domain, chainPath, "chain.pem")
		if err != nil {
			return err
		}
		if chainVersionDir != versionDir {
			return fmt.Errorf("certificate chain is from a different immutable snapshot")
		}
	}
	verifiedVersionDir, err := verifyManagedCertificateVersionPath(
		domain, certPath,
	)
	if err != nil {
		return err
	}
	if verifiedVersionDir != versionDir {
		return fmt.Errorf("managed certificate snapshot identity changed during verification")
	}
	return nil
}

// InspectInstalledCertificate validates the complete runtime pair. A
// certificate file alone is not usable when its private key is missing or
// belongs to another certificate.
func (a *Agent) InspectInstalledCertificate(req InspectCertificateRequest, resp *ValidateCertResponse) error {
	if strings.TrimSpace(req.Domain) != "" {
		if err := verifyManagedCertificateSnapshot(
			req.Domain, req.CertPath, req.KeyPath, req.ChainPath,
		); err != nil {
			resp.Valid = false
			resp.Error = err.Error()
			return nil
		}
	}
	if err := a.GetCertificateInfo(req.CertPath, resp); err != nil || !resp.Valid {
		return err
	}
	keyContent, err := os.ReadFile(req.KeyPath)
	if err != nil {
		resp.Valid = false
		resp.Error = fmt.Sprintf("failed to read private key: %v", err)
		return nil
	}
	privateKey, err := parsePrivateKey(string(keyContent))
	if err != nil {
		resp.Valid = false
		resp.Error = err.Error()
		return nil
	}
	certContent, err := os.ReadFile(req.CertPath)
	if err != nil {
		resp.Valid = false
		resp.Error = fmt.Sprintf("failed to read certificate: %v", err)
		return nil
	}
	certificates, err := parseCertificateBundle(string(certContent))
	if err != nil {
		resp.Valid = false
		resp.Error = err.Error()
		return nil
	}
	matches, err := certificateMatchesPrivateKey(certificates[0], privateKey)
	if err != nil {
		resp.Valid = false
		resp.Error = err.Error()
		return nil
	}
	if !matches {
		resp.Valid = false
		resp.Error = "private key does not match certificate"
	}
	return nil
}

// CheckCertbotInstalled checks if certbot is installed
func checkCertbotInstalled() bool {
	cmd := exec.Command("which", "certbot")
	err := cmd.Run()
	return err == nil
}

// GetCertbotVersion returns the installed certbot version
func getCertbotVersion() (string, error) {
	cmd := exec.Command("certbot", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
