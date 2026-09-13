package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const panelAccessAddressPath = "/api/v1/panel/access-address"

// This only recognizes a pair selected by tlsSettings from our immutable
// managed directory. Explicit owner TLS configuration never gets an override.
// Yalnızca yönetilen sürüm dizininden seçilen çift tanınır; sahibin açık TLS ayarı değiştirilmez.
func isManagedPanelPair(certPath, keyPath string) bool {
	if os.Getenv("CELIKPANEL_TLS_CERT") != "" || os.Getenv("CELIKPANEL_TLS_KEY") != "" || os.Getenv("CELIKPANEL_TLS") != "1" {
		return false
	}
	dir := filepath.Dir(certPath)
	return filepath.Clean(filepath.Dir(dir)) == filepath.Clean(tlsDir()) &&
		validManagedPanelCertVersionName(filepath.Base(dir)) &&
		filepath.Base(certPath) == "panel.crt" && keyPath == filepath.Join(dir, "panel.key")
}

func panelIPCertificate(certPath, keyPath string) *tls.Certificate {
	if !isManagedPanelPair(certPath, keyPath) {
		return nil
	}
	// Missing or unusable optional bootstrap material must not prevent the
	// valid managed certificate from serving. Never generate or rewrite it here.
	// Eksik başlangıç sertifikası yönetilen sertifikayı engellemez; burada dosya üretilmez veya değiştirilmez.
	pair, err := tls.LoadX509KeyPair(filepath.Join(tlsDir(), "panel.crt"), filepath.Join(tlsDir(), "panel.key"))
	if err != nil {
		return nil
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || len(leaf.IPAddresses) == 0 || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil
	}
	// Only the original self-signed bootstrap identity belongs on the IP path.
	// IP yolunda yalnızca kendinden imzalı başlangıç kimliği korunur.
	if leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) != nil {
		return nil
	}
	return &pair
}

// Public, read-only metadata for the sign-in/recovery hint. It exposes only
// the hostname already in the active managed certificate, never keys, sessions,
// agent state, or an address supplied through Host/Forwarded/query headers.
// Giriş yardımı yalnızca etkin sertifikadaki alan adını gösterir; anahtar, oturum,
// agent durumu veya isteğin Host/Forwarded/sorgu alanlarındaki adresler yayımlanmaz.
func panelAccessAddressHandler(certPath, keyPath string) http.HandlerFunc {
	var leaf *x509.Certificate
	if isManagedPanelPair(certPath, keyPath) {
		if pair, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
			leaf, _ = x509.ParseCertificate(pair.Certificate[0])
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		hostname := ""
		if leaf != nil && !time.Now().Before(leaf.NotBefore) && time.Now().Before(leaf.NotAfter) && len(leaf.DNSNames) == 1 {
			name := strings.ToLower(leaf.DNSNames[0])
			if validPanelAccessHostname(name) {
				hostname = name
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Hostname string `json:"hostname"`
		}{hostname})
	}
}

func validPanelAccessHostname(name string) bool {
	if len(name) > 253 || net.ParseIP(name) != nil || !strings.Contains(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}
