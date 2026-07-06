package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// The panel must not serve its own login and session cookies over plain HTTP
// in production — credentials in the clear. HTTPS is on whenever a cert/key
// pair is configured, or when CELIKPANEL_TLS=1 asks us to self-sign one so it
// works out of the box (the operator can later drop in a real certificate for
// the panel's hostname). Dev keeps plain HTTP via the --insecure-cookies flag.
//
// Panel, üretimde kendi giriş ve oturum çerezlerini düz HTTP'de sunmamalı —
// açık kimlik bilgisi. Bir cert/key çifti yapılandırıldığında ya da
// CELIKPANEL_TLS=1 kendinden-imzalı üretmemizi istediğinde HTTPS açıktır
// (operatör sonradan panel host'u için gerçek sertifika koyabilir). Dev,
// --insecure-cookies bayrağıyla düz HTTP'de kalır.

// tlsSettings reports whether to serve HTTPS and the cert/key paths to use.
// When TLS is requested but no explicit pair is given, it self-signs one
// under the config dir and returns those paths.
// tlsSettings, HTTPS sunulup sunulmayacağını ve kullanılacak cert/key
// yollarını bildirir. TLS istenmiş ama açık çift verilmemişse config dizini
// altında kendinden-imzalı üretir ve o yolları döndürür.
func tlsSettings() (enabled bool, certPath, keyPath string, err error) {
	certPath = os.Getenv("CELIKPANEL_TLS_CERT")
	keyPath = os.Getenv("CELIKPANEL_TLS_KEY")

	// An explicit pair means "serve HTTPS with exactly this certificate".
	// Açık bir çift "tam olarak bu sertifikayla HTTPS sun" demektir.
	if certPath != "" && keyPath != "" {
		return true, certPath, keyPath, nil
	}

	if os.Getenv("CELIKPANEL_TLS") != "1" {
		return false, "", "", nil
	}

	// Self-sign on demand into the config/tls dir; reuse an existing one so
	// the certificate is stable across restarts (browsers remember it).
	// Talep üzerine config/tls dizinine kendinden-imzala; sertifika yeniden
	// başlatmalarda kararlı kalsın diye var olanı yeniden kullan.
	dir := tlsDir()
	certPath = filepath.Join(dir, "panel.crt")
	keyPath = filepath.Join(dir, "panel.key")
	if fileExists(certPath) && fileExists(keyPath) {
		return true, certPath, keyPath, nil
	}
	if err := generateSelfSigned(certPath, keyPath); err != nil {
		return false, "", "", err
	}
	return true, certPath, keyPath, nil
}

// tlsDir is where the self-signed material lives (CELIKPANEL_TLS_DIR, else a
// tls/ subdir of the data dir).
// tlsDir, kendinden-imzalı malzemenin yaşadığı yerdir.
func tlsDir() string {
	if d := os.Getenv("CELIKPANEL_TLS_DIR"); d != "" {
		return d
	}
	return filepath.Join(dataDir(), "tls")
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// generateSelfSigned writes a fresh ECDSA P-256 self-signed certificate valid
// for this host's name and every non-loopback IP, so it is reachable both by
// hostname and by bare IP. Valid for two years; the key is written 0600.
// generateSelfSigned, bu makinenin adı ve her loopback-olmayan IP'si için
// geçerli, taze bir ECDSA P-256 kendinden-imzalı sertifika yazar; böylece hem
// host adıyla hem çıplak IP ile erişilir. İki yıl geçerli; anahtar 0600 yazılır.
func generateSelfSigned(certPath, keyPath string) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o750); err != nil {
		return err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("serial: %w", err)
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "celikpanel"
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host, Organization: []string{"CelikPanel"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(2, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{host, "localhost"},
	}
	tmpl.IPAddresses = append(tmpl.IPAddresses, net.IPv4(127, 0, 0, 1), net.IPv6loopback)
	for _, ip := range hostIPs() {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create cert: %w", err)
	}

	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return err
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
}

// hostIPs returns this host's non-loopback unicast IPs so the self-signed
// certificate validates when the panel is reached by its LAN/public address.
// hostIPs, panel LAN/genel adresiyle erişildiğinde kendinden-imzalı
// sertifikanın doğrulanması için makinenin loopback-olmayan tekli IP'lerini
// döndürür.
func hostIPs() []net.IP {
	var ips []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || !ipnet.IP.IsGlobalUnicast() {
			continue
		}
		ips = append(ips, ipnet.IP)
	}
	return ips
}
