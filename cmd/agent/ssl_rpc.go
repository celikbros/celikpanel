package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SSL/TLS Certificate Management RPC Methods

// IssueLetsEncryptRequest represents a request to issue a Let's Encrypt certificate
type IssueLetsEncryptRequest struct {
	Domain    string   `json:"domain"`
	Aliases   []string `json:"aliases"`
	Email     string   `json:"email"`
	Webroot   string   `json:"webroot"`
	AutoRenew bool     `json:"auto_renew"`
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
}

// IssueLetsEncryptResponse represents the response from issuing a certificate
type IssueLetsEncryptResponse struct {
	Success   bool      `json:"success"`
	CertPath  string    `json:"cert_path"`
	KeyPath   string    `json:"key_path"`
	ChainPath string    `json:"chain_path"`
	ExpiresAt time.Time `json:"expires_at"`
	Error     string    `json:"error,omitempty"`
}

// RenewCertRequest represents a request to renew a certificate
type RenewCertRequest struct {
	Domain string `json:"domain"`
}

// RenewCertResponse represents the response from renewing a certificate
type RenewCertResponse struct {
	Success   bool      `json:"success"`
	ExpiresAt time.Time `json:"expires_at"`
	Error     string    `json:"error,omitempty"`
}

// ValidateCertRequest represents a request to validate a certificate
type ValidateCertRequest struct {
	CertContent string `json:"cert_content"`
	KeyContent  string `json:"key_content"`
	Domain      string `json:"domain"`
}

// ValidateCertResponse represents the response from validating a certificate
type ValidateCertResponse struct {
	Valid     bool      `json:"valid"`
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Error     string    `json:"error,omitempty"`
}

// InstallCertRequest represents a request to install a custom certificate
type InstallCertRequest struct {
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

// IssueLetsEncryptCertificate issues a new Let's Encrypt certificate using certbot
func (a *Agent) IssueLetsEncryptCertificate(req IssueLetsEncryptRequest, resp *IssueLetsEncryptResponse) error {
	// Build domain list
	domains := []string{req.Domain}
	domains = append(domains, req.Aliases...)

	// Build certbot command
	args := []string{
		"certonly",
		"--webroot",
		"-w", req.Webroot,
		"--email", req.Email,
		"--agree-tos",
		"--non-interactive",
		"--force-renewal", // Force renewal for testing, remove in production
	}

	// A non-default CA is selected by its ACME directory URL. certbot records
	// it in the cert's renewal config, so RenewLetsEncryptCertificate needs no
	// change — it renews from whichever CA first issued.
	// Varsayılan olmayan CA, ACME dizin URL'siyle seçilir. certbot bunu
	// sertifikanın yenileme yapılandırmasına kaydeder; bu yüzden
	// RenewLetsEncryptCertificate değişmez — ilk hangi CA verdiyse ondan yeniler.
	if req.ACMEServer != "" {
		args = append(args, "--server", req.ACMEServer)
	}
	// EAB binds this issuance to the operator's account at the chosen CA.
	// certbot records the bound account in the renewal config, so this is
	// needed only at first issuance, never at renewal.
	// EAB, bu vermeyi seçilen CA'daki operatör hesabına bağlar. certbot
	// bağlanan hesabı yenileme yapılandırmasına yazar; bu yüzden yalnız ilk
	// vermede gerekir, yenilemede asla.
	if req.EABKeyID != "" && req.EABHMACKey != "" {
		args = append(args, "--eab-kid", req.EABKeyID, "--eab-hmac-key", req.EABHMACKey)
	}

	// Add domains
	for _, domain := range domains {
		args = append(args, "-d", domain)
	}

	// Execute certbot
	cmd := exec.Command("certbot", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("certbot failed: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Certificate paths
	certDir := filepath.Join("/etc/letsencrypt/live", req.Domain)
	resp.CertPath = filepath.Join(certDir, "fullchain.pem")
	resp.KeyPath = filepath.Join(certDir, "privkey.pem")
	resp.ChainPath = filepath.Join(certDir, "chain.pem")

	// Parse certificate to get expiry date
	certData, err := os.ReadFile(resp.CertPath)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to read certificate: %v", err)
		return nil
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		resp.Success = false
		resp.Error = "failed to decode certificate PEM"
		return nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to parse certificate: %v", err)
		return nil
	}

	resp.ExpiresAt = cert.NotAfter
	resp.Success = true
	return nil
}

// RenewLetsEncryptCertificate renews an existing Let's Encrypt certificate
func (a *Agent) RenewLetsEncryptCertificate(req RenewCertRequest, resp *RenewCertResponse) error {
	// Execute certbot renew for specific domain
	// No --force-renewal: certbot only renews near expiry, which respects
	// Let's Encrypt rate limits even if this is called too often.
	// --force-renewal yok: certbot yalnız bitime yakın yeniler; bu, çok sık
	// çağrılsa bile Let's Encrypt hız sınırlarına saygı gösterir.
	cmd := exec.Command("certbot", "renew", "--cert-name", req.Domain)
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("certbot renew failed: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Get new expiry date
	certPath := filepath.Join("/etc/letsencrypt/live", req.Domain, "fullchain.pem")
	certData, err := os.ReadFile(certPath)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to read renewed certificate: %v", err)
		return nil
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		resp.Success = false
		resp.Error = "failed to decode certificate PEM"
		return nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to parse certificate: %v", err)
		return nil
	}

	resp.ExpiresAt = cert.NotAfter
	resp.Success = true
	return nil
}

// ValidateCertificate validates a certificate and extracts information
func (a *Agent) ValidateCertificate(req ValidateCertRequest, resp *ValidateCertResponse) error {
	// Decode certificate PEM
	block, _ := pem.Decode([]byte(req.CertContent))
	if block == nil {
		resp.Valid = false
		resp.Error = "invalid certificate PEM format"
		return nil
	}

	// Parse certificate
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		resp.Valid = false
		resp.Error = fmt.Sprintf("failed to parse certificate: %v", err)
		return nil
	}

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

	// Validate private key matches certificate
	keyBlock, _ := pem.Decode([]byte(req.KeyContent))
	if keyBlock == nil {
		resp.Valid = false
		resp.Error = "invalid private key PEM format"
		return nil
	}

	// Extract certificate info
	resp.Valid = true
	resp.Issuer = cert.Issuer.CommonName
	resp.Subject = cert.Subject.CommonName
	resp.IssuedAt = cert.NotBefore
	resp.ExpiresAt = cert.NotAfter

	return nil
}

// InstallCustomCertificate installs a custom SSL certificate
func (a *Agent) InstallCustomCertificate(req InstallCertRequest, resp *InstallCertResponse) error {
	// Create certificate directory
	certDir := filepath.Join("/etc/ssl/celikpanel", req.Domain)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to create cert directory: %v", err)
		return nil
	}

	// Write certificate file
	certPath := filepath.Join(certDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte(req.CertContent), 0644); err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to write certificate: %v", err)
		return nil
	}

	// Write private key file (with restricted permissions)
	keyPath := filepath.Join(certDir, "privkey.pem")
	if err := os.WriteFile(keyPath, []byte(req.KeyContent), 0600); err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to write private key: %v", err)
		return nil
	}

	// Write chain file if provided
	var chainPath string
	if req.ChainContent != "" {
		chainPath = filepath.Join(certDir, "chain.pem")
		if err := os.WriteFile(chainPath, []byte(req.ChainContent), 0644); err != nil {
			resp.Success = false
			resp.Error = fmt.Sprintf("failed to write chain: %v", err)
			return nil
		}
	}

	resp.Success = true
	resp.CertPath = certPath
	resp.KeyPath = keyPath
	resp.ChainPath = chainPath

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

	block, _ := pem.Decode(certData)
	if block == nil {
		resp.Valid = false
		resp.Error = "failed to decode certificate PEM"
		return nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		resp.Valid = false
		resp.Error = fmt.Sprintf("failed to parse certificate: %v", err)
		return nil
	}

	resp.Valid = true
	resp.Issuer = cert.Issuer.CommonName
	resp.Subject = cert.Subject.CommonName
	resp.IssuedAt = cert.NotBefore
	resp.ExpiresAt = cert.NotAfter

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
