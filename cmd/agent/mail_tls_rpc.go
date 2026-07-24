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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Mail TLS — the equivalent of Plesk's "assign the certificate to the mail
// domain". Postfix and Dovecot get a server-default certificate (self-signed
// for the hostname if nothing better exists; receiving MTAs do opportunistic
// TLS and do not validate it) plus per-domain SNI entries so mail clients
// connecting to mail.<domain> see that domain's real certificate. Outbound
// TLS is enabled too — sending in the clear costs reputation points with the
// big providers.
//
// Posta TLS'i — Plesk'in "sertifikayı posta alanına ata"sının eşleniği.
// Postfix ve Dovecot sunucu-varsayılanı bir sertifika alır (daha iyisi yoksa
// hostname için self-signed; alıcı MTA'lar fırsatçı TLS yapar ve doğrulamaz)
// artı domain başına SNI girdileri; böylece mail.<domain>'e bağlanan posta
// istemcileri o domain'in gerçek sertifikasını görür. Giden TLS de açılır —
// düz metin göndermek büyük sağlayıcılarda itibar puanı kaybettirir.

const (
	mailTLSDir        = "/etc/ssl/celikpanel/_mail"
	postfixSNIPath    = "/etc/postfix/celikpanel_sni"
	dovecotTLSConf    = "/etc/dovecot/conf.d/98-celikpanel-tls.conf"
	defaultMailCert   = mailTLSDir + "/default-cert.pem"
	defaultMailKey    = mailTLSDir + "/default-key.pem"
	mailCertValidDays = 2 * 365
)

type MailSNIEntry struct {
	// Names this certificate should answer for (domain, mail.domain, …).
	// Bu sertifikanın yanıt vereceği adlar (domain, mail.domain, …).
	Names    []string `json:"names"`
	CertPath string   `json:"cert_path"`
	KeyPath  string   `json:"key_path"`
}

type SecureMailTLSRequest struct {
	// Myhostname fixes Postfix's HELO name; empty keeps the system FQDN.
	// Myhostname, Postfix'in HELO adını sabitler; boşsa sistem FQDN'i kalır.
	Myhostname string         `json:"myhostname"`
	SNI        []MailSNIEntry `json:"sni"`
}

type SecureMailTLSResponse struct {
	Configured  bool   `json:"configured"`
	DefaultCert string `json:"default_cert"`
	SNICount    int    `json:"sni_count"`
	Detail      string `json:"detail,omitempty"`
	Error       string `json:"error,omitempty"`
}

// SecureMailTLS wires TLS into the whole mail stack: default certificate,
// per-domain SNI on both daemons, modern protocol floor, opportunistic
// outbound TLS and a fixed HELO name. Safe to re-run; it converges.
// SecureMailTLS, TLS'i tüm posta yığınına bağlar: varsayılan sertifika, iki
// daemon'da da domain başına SNI, modern protokol tabanı, fırsatçı giden TLS
// ve sabit HELO adı. Yeniden koşmak güvenlidir; yakınsar.
func (a *Agent) SecureMailTLS(req *SecureMailTLSRequest, resp *SecureMailTLSResponse) error {
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		resp.Error = "mail TLS is a production action; not available with CELIKPANEL_MAIL_DIR set"
		return nil
	}
	if _, err := exec.LookPath("postconf"); err != nil {
		resp.Error = "postfix is not installed"
		return nil
	}

	if err := ensureDefaultMailCert(); err != nil {
		resp.Error = fmt.Sprintf("default certificate: %v", err)
		return nil
	}
	resp.DefaultCert = defaultMailCert

	// Validate SNI entries strictly: both files must exist, or the maps
	// would break the daemons on reload.
	// SNI girdilerini sıkı doğrula: iki dosya da var olmalı, yoksa map'ler
	// reload'da daemon'ları kırar.
	var valid []MailSNIEntry
	for _, e := range req.SNI {
		if len(e.Names) == 0 {
			continue
		}
		if fileExists(e.CertPath) && fileExists(e.KeyPath) {
			valid = append(valid, e)
		}
	}

	if err := configurePostfixTLS(req.Myhostname, valid); err != nil {
		resp.Error = fmt.Sprintf("postfix: %v", err)
		return nil
	}
	if err := configureDovecotTLS(valid); err != nil {
		resp.Error = fmt.Sprintf("dovecot: %v", err)
		return nil
	}

	_ = exec.Command("systemctl", "reload-or-restart", "postfix").Run()
	_ = exec.Command("systemctl", "reload-or-restart", "dovecot").Run()

	resp.Configured = true
	resp.SNICount = len(valid)
	resp.Detail = fmt.Sprintf("mail TLS active (%d SNI entries)", len(valid))
	return nil
}

// ensureDefaultMailCert creates a self-signed certificate for the machine's
// hostname once. Opportunistic SMTP TLS never validates the chain, so this is
// enough to stop plaintext transport; domains with real certificates override
// it via SNI.
// ensureDefaultMailCert, makinenin hostname'i için bir kez self-signed
// sertifika üretir. Fırsatçı SMTP TLS zinciri asla doğrulamaz; bu, düz metin
// taşımayı durdurmaya yeter. Gerçek sertifikalı domain'ler SNI ile ezer.
func ensureDefaultMailCert() error {
	if fileExists(defaultMailCert) && fileExists(defaultMailKey) {
		return nil
	}
	if err := os.MkdirAll(mailTLSDir, 0o755); err != nil {
		return err
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "mail.local"
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(0, 0, mailCertValidDays),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(defaultMailCert,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	return os.WriteFile(defaultMailKey,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}

// configurePostfixTLS sets the default certificate, protocol floor,
// opportunistic TLS both ways, HELO name and the SNI map.
// configurePostfixTLS, varsayılan sertifikayı, protokol tabanını, iki yönlü
// fırsatçı TLS'i, HELO adını ve SNI map'ini ayarlar.
func configurePostfixTLS(myhostname string, sni []MailSNIEntry) error {
	settings := [][2]string{
		{"smtpd_tls_cert_file", defaultMailCert},
		{"smtpd_tls_key_file", defaultMailKey},
		// "may" = offer TLS, accept plaintext — mandatory TLS on port 25
		// violates RFC and loses mail from old senders.
		// "may" = TLS öner, düz metni kabul et — 25'te zorunlu TLS RFC'ye
		// aykırıdır ve eski göndericilerden postayı kaybettirir.
		{"smtpd_tls_security_level", "may"},
		{"smtp_tls_security_level", "may"},
		{"smtpd_tls_protocols", ">=TLSv1.2"},
		{"smtp_tls_protocols", ">=TLSv1.2"},
		{"smtpd_tls_loglevel", "1"},
	}
	if myhostname != "" {
		settings = append(settings, [2]string{"myhostname", myhostname})
	}
	if len(sni) > 0 {
		if err := writePostfixSNIMap(sni); err != nil {
			return err
		}
		settings = append(settings, [2]string{"tls_server_sni_maps", postfixMapType() + ":" + postfixSNIPath})
	} else {
		settings = append(settings, [2]string{"tls_server_sni_maps", ""})
	}
	for _, s := range settings {
		if out, err := exec.Command("postconf", "-e", s[0]+"="+s[1]).CombinedOutput(); err != nil {
			return fmt.Errorf("postconf %s: %s", s[0], strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// writePostfixSNIMap writes the source map and compiles it with postmap -F,
// which embeds the PEM contents into the .db (required for SNI maps).
// writePostfixSNIMap, kaynak map'i yazar ve postmap -F ile derler; -F, PEM
// içeriklerini .db'ye gömer (SNI map'leri için gereklidir).
func writePostfixSNIMap(sni []MailSNIEntry) error {
	var b strings.Builder
	b.WriteString("# Managed by CelikPanel — per-domain mail certificates (SNI).\n")
	for _, e := range sni {
		for _, name := range e.Names {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			fmt.Fprintf(&b, "%s %s %s\n", name, e.KeyPath, e.CertPath)
		}
	}
	if err := os.WriteFile(postfixSNIPath, []byte(b.String()), 0o600); err != nil {
		return err
	}
	// Same portability trap as the virtual maps: `hash:` is unusable on distros
	// that build postfix without Berkeley DB (Arch), and per-domain mail
	// certificates would silently never load. An SNI map MUST be indexed —
	// postmap -F embeds the PEM contents into the index — so texthash is not an
	// option here; say so instead of writing a map postfix cannot read.
	// Sanal haritalardaki taşınabilirlik tuzağının aynısı: `hash:`, postfix'i
	// Berkeley DB'siz derleyen dağıtımlarda (Arch) kullanılamaz ve alan başına
	// posta sertifikaları sessizce hiç yüklenmezdi. SNI haritası dizinli
	// OLMALIDIR — postmap -F, PEM içeriğini dizine gömer — bu yüzden texthash
	// burada seçenek değil; postfix'in okuyamayacağı bir harita yazmaktansa
	// durumu söyle.
	mt := postfixMapType()
	if mt == "texthash" {
		return fmt.Errorf("postfix on this system has no indexed table type (lmdb/hash/btree); per-domain mail certificates need one")
	}
	if out, err := exec.Command("postmap", "-F", mt+":"+postfixSNIPath).CombinedOutput(); err != nil {
		return fmt.Errorf("postmap -F: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// configureDovecotTLS writes our TLS drop-in: default certificate plus a
// local_name block per SNI name so IMAP/POP clients get the right chain.
// configureDovecotTLS, TLS ekimizi yazar: varsayılan sertifika artı SNI adı
// başına bir local_name bloğu; IMAP/POP istemcileri doğru zinciri alır.
func configureDovecotTLS(sni []MailSNIEntry) error {
	if err := os.MkdirAll(filepath.Dir(dovecotTLSConf), 0o755); err != nil {
		return err
	}
	// Dialect-aware (2.3 ssl_cert=< vs 2.4 ssl_server_cert_file=), and
	// validated by dovecot's parser before any restart — see dovecot_dialect.go.
	// Lehçe-farkında (2.3 ssl_cert=< vs 2.4 ssl_server_cert_file=) ve yeniden
	// başlatmadan önce dovecot ayrıştırıcısıyla doğrulanır.
	conf := buildDovecotTLSConf(dovecotIs24(), defaultMailCert, defaultMailKey, sni)
	return applyDovecotConf(dovecotTLSConf, conf)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
