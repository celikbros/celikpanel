package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
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
	maxMailSNIEntries = 4096
	maxMailSNINames   = 2
	maxMailTLSPathLen = 4096
)

var postfixTLSManagedSettings = []string{
	"smtpd_tls_cert_file",
	"smtpd_tls_key_file",
	"smtpd_tls_security_level",
	"smtp_tls_security_level",
	"smtpd_tls_protocols",
	"smtp_tls_protocols",
	"smtpd_tls_loglevel",
	"myhostname",
	"tls_server_sni_maps",
}

type MailSNIEntry = transport.MailSNIEntry
type SecureMailTLSRequest = transport.SecureMailTLSRequest
type SecureMailTLSResponse = transport.SecureMailTLSResponse

type mailTLSFileSnapshot struct {
	path    string
	existed bool
	data    []byte
	mode    os.FileMode
}

type postfixTLSSettingSnapshot struct {
	name  string
	value string
}

type mailTLSStateSnapshot struct {
	postfixSettings []postfixTLSSettingSnapshot
	files           []mailTLSFileSnapshot
	dovecotFile     mailTLSFileSnapshot
}

// SecureMailTLS wires TLS into the whole mail stack: default certificate,
// per-domain SNI on both daemons, modern protocol floor, opportunistic
// outbound TLS and a fixed HELO name. Safe to re-run; it converges.
// SecureMailTLS, TLS'i tüm posta yığınına bağlar: varsayılan sertifika, iki
// daemon'da da domain başına SNI, modern protokol tabanı, fırsatçı giden TLS
// ve sabit HELO adı. Yeniden koşmak güvenlidir; yakınsar.
func (a *Agent) SecureMailTLS(req *SecureMailTLSRequest, resp *SecureMailTLSResponse) error {
	if req == nil {
		resp.Error = "mail TLS request is required"
		return nil
	}
	if err := requireExpectedBuildCommit(
		req.ExpectedBuildCommit,
		"securing the mail TLS stack",
	); err != nil {
		resp.Error = err.Error()
		return nil
	}

	mailMutex.Lock()
	defer mailMutex.Unlock()

	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		resp.Error = "mail TLS is a production action; not available with CELIKPANEL_MAIL_DIR set"
		return nil
	}
	if _, err := exec.LookPath("postconf"); err != nil {
		resp.Error = "postfix is not installed"
		return nil
	}

	myhostname, valid, err := validateSecureMailTLSRequest(req)
	if err != nil {
		resp.Error = fmt.Sprintf("mail TLS validation: %v", err)
		return nil
	}
	previous, err := snapshotMailTLSState()
	if err != nil {
		resp.Error = fmt.Sprintf("mail TLS snapshot: %v", err)
		return nil
	}
	if err := ensureDefaultMailCert(); err != nil {
		setMailTLSFailure(resp, "default certificate", err, previous)
		return nil
	}
	resp.DefaultCert = defaultMailCert

	if err := configurePostfixTLS(myhostname, valid); err != nil {
		setMailTLSFailure(resp, "postfix configuration", err, previous)
		return nil
	}
	if err := configureDovecotTLS(valid); err != nil {
		setMailTLSFailure(resp, "dovecot configuration", err, previous)
		return nil
	}
	if err := validatePostfixTLSConfig(); err != nil {
		setMailTLSFailure(resp, "postfix validation", err, previous)
		return nil
	}
	if err := reloadMailTLSService("postfix"); err != nil {
		setMailTLSFailure(resp, "postfix reload", err, previous)
		return nil
	}
	if err := reloadMailTLSService("dovecot"); err != nil {
		setMailTLSFailure(resp, "dovecot reload", err, previous)
		return nil
	}

	resp.Configured = true
	resp.SNICount = len(valid)
	resp.Detail = fmt.Sprintf("mail TLS active (%d SNI entries)", len(valid))
	return nil
}

func validateSecureMailTLSRequest(req *SecureMailTLSRequest) (string, []MailSNIEntry, error) {
	if req == nil {
		return "", nil, fmt.Errorf("request is required")
	}
	myhostname := ""
	if strings.TrimSpace(req.Myhostname) != "" {
		var err error
		myhostname, err = hostname.CanonicalFQDN(req.Myhostname)
		if err != nil {
			return "", nil, fmt.Errorf("invalid server hostname")
		}
	}
	entries, err := validateMailSNIEntries(req.SNI)
	if err != nil {
		return "", nil, fmt.Errorf("SNI: %w", err)
	}
	return myhostname, entries, nil
}

func validateMailSNIEntries(entries []MailSNIEntry) ([]MailSNIEntry, error) {
	if len(entries) > maxMailSNIEntries {
		return nil, fmt.Errorf("too many entries")
	}
	validated := make([]MailSNIEntry, 0, len(entries))
	claimedNames := make(map[string]int)
	for entryIndex, entry := range entries {
		if len(entry.Names) == 0 {
			return nil, fmt.Errorf("entry %d has no names", entryIndex+1)
		}
		if len(entry.Names) > maxMailSNINames {
			return nil, fmt.Errorf("entry %d has too many names", entryIndex+1)
		}

		names := make([]string, 0, len(entry.Names))
		seenNames := make(map[string]struct{}, len(entry.Names))
		for nameIndex, name := range entry.Names {
			canonical, err := hostname.CanonicalFQDN(name)
			if err != nil {
				return nil, fmt.Errorf("entry %d name %d is not a valid FQDN", entryIndex+1, nameIndex+1)
			}
			if _, exists := seenNames[canonical]; exists {
				continue
			}
			seenNames[canonical] = struct{}{}
			names = append(names, canonical)
		}

		certDomain, certPath, err := requireManagedMailTLSCertificateFile(
			entry.CertPath, "fullchain.pem", 0o644,
		)
		if err != nil {
			return nil, fmt.Errorf("entry %d certificate: %w", entryIndex+1, err)
		}
		keyDomain, keyPath, err := requireManagedMailTLSCertificateFile(
			entry.KeyPath, "privkey.pem", 0o600,
		)
		if err != nil {
			return nil, fmt.Errorf("entry %d private key: %w", entryIndex+1, err)
		}
		if certDomain != keyDomain || filepath.Dir(certPath) != filepath.Dir(keyPath) {
			return nil, fmt.Errorf("entry %d certificate and private key are not from the same managed snapshot", entryIndex+1)
		}

		mailName, err := hostname.MailFQDN(certDomain)
		if err != nil {
			return nil, fmt.Errorf("entry %d certificate domain is invalid", entryIndex+1)
		}
		hasMailName := false
		for _, name := range names {
			if name != certDomain && name != mailName {
				return nil, fmt.Errorf("entry %d name %q does not belong to certificate domain %q", entryIndex+1, name, certDomain)
			}
			if previousEntry, exists := claimedNames[name]; exists {
				return nil, fmt.Errorf("entry %d name %q is already claimed by entry %d", entryIndex+1, name, previousEntry)
			}
			hasMailName = hasMailName || name == mailName
		}
		if !hasMailName {
			return nil, fmt.Errorf("entry %d does not include the managed mail hostname %q", entryIndex+1, mailName)
		}
		for _, name := range names {
			claimedNames[name] = entryIndex + 1
		}

		entry.Names = names
		entry.CertPath = certPath
		entry.KeyPath = keyPath
		validated = append(validated, entry)
	}
	return validated, nil
}

func requireManagedMailTLSCertificateFile(rawPath, expectedName string, expectedMode os.FileMode) (string, string, error) {
	if strings.TrimSpace(rawPath) == "" {
		return "", "", fmt.Errorf("path is empty")
	}
	if rawPath != strings.TrimSpace(rawPath) ||
		len(rawPath) > maxMailTLSPathLen ||
		strings.ContainsAny(rawPath, " \t\r\n\x00") ||
		!filepath.IsAbs(rawPath) ||
		filepath.Clean(rawPath) != rawPath {
		return "", "", fmt.Errorf("path is not a canonical absolute path")
	}
	if filepath.Base(rawPath) != expectedName {
		return "", "", fmt.Errorf("path must end in %s", expectedName)
	}

	configuredRoot := strings.TrimSpace(os.Getenv("CELIKPANEL_CUSTOM_CERT_ROOT"))
	root := configuredRoot
	if root == "" {
		root = "/etc/ssl/celikpanel"
	}
	if !filepath.IsAbs(root) {
		return "", "", fmt.Errorf("managed certificate root is not absolute")
	}
	root = filepath.Clean(root)
	relative, err := filepath.Rel(root, rawPath)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path is outside the managed certificate root")
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 3 {
		return "", "", fmt.Errorf("path is not an immutable managed certificate snapshot")
	}
	domain, err := hostname.CanonicalFQDN(parts[0])
	if err != nil || domain != parts[0] {
		return "", "", fmt.Errorf("managed certificate domain is invalid")
	}
	if !certificateVersionPattern.MatchString(parts[1]) {
		return "", "", fmt.Errorf("managed certificate version is invalid")
	}
	if parts[2] != expectedName {
		return "", "", fmt.Errorf("managed certificate filename is invalid")
	}

	rootInfo, err := requireMailTLSDirectory(root, nil, 0)
	if err != nil {
		return "", "", fmt.Errorf("managed certificate root: %w", err)
	}
	rootOwner, ownerKnown := mailTLSFileOwner(rootInfo)
	if configuredRoot == "" && ownerKnown && rootOwner != 0 {
		return "", "", fmt.Errorf("managed certificate root must be owned by root")
	}
	var expectedOwner *uint64
	if ownerKnown {
		expectedOwner = &rootOwner
	}
	domainDir := filepath.Join(root, domain)
	if _, err := requireMailTLSDirectory(domainDir, expectedOwner, 0o750); err != nil {
		return "", "", fmt.Errorf("managed certificate domain directory: %w", err)
	}
	versionDir := filepath.Join(domainDir, parts[1])
	if _, err := requireMailTLSDirectory(versionDir, expectedOwner, 0o750); err != nil {
		return "", "", fmt.Errorf("managed certificate version directory: %w", err)
	}
	if err := requireMailTLSRegularFile(rawPath, expectedOwner, expectedMode); err != nil {
		return "", "", err
	}
	return domain, rawPath, nil
}

func requireMailTLSDirectory(path string, expectedOwner *uint64, exactMode os.FileMode) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a safe directory", path)
	}
	if runtime.GOOS != "windows" {
		mode := info.Mode().Perm()
		if exactMode != 0 && mode != exactMode {
			return nil, fmt.Errorf("%s has unsafe permissions %04o", path, mode)
		}
		if exactMode == 0 && mode&0o022 != 0 {
			return nil, fmt.Errorf("%s is writable by group or others", path)
		}
	}
	if err := requireMailTLSOwner(info, expectedOwner); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return info, nil
}

func requireMailTLSRegularFile(path string, expectedOwner *uint64, expectedMode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != expectedMode {
		return fmt.Errorf("%s has unsafe permissions %04o", path, info.Mode().Perm())
	}
	if err := requireMailTLSOwner(info, expectedOwner); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if links, ok := mailTLSFileLinkCount(info); ok && links != 1 {
		return fmt.Errorf("%s has %d hard links", path, links)
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return fmt.Errorf("%s changed during validation", path)
	}
	return nil
}

func requireMailTLSOwner(info os.FileInfo, expectedOwner *uint64) error {
	if expectedOwner == nil {
		return nil
	}
	owner, ok := mailTLSFileOwner(info)
	if !ok {
		return nil
	}
	if owner != *expectedOwner {
		return fmt.Errorf("owner %d does not match managed root owner %d", owner, *expectedOwner)
	}
	return nil
}

func mailTLSFileOwner(info os.FileInfo) (uint64, bool) {
	return mailTLSStatUint(info, "Uid")
}

func mailTLSFileLinkCount(info os.FileInfo) (uint64, bool) {
	return mailTLSStatUint(info, "Nlink")
}

func mailTLSStatUint(info os.FileInfo, fieldName string) (uint64, bool) {
	if info == nil || info.Sys() == nil {
		return 0, false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, false
	}
	field := value.FieldByName(fieldName)
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Int() < 0 {
			return 0, false
		}
		return uint64(field.Int()), true
	default:
		return 0, false
	}
}

func snapshotMailTLSFile(path string) (mailTLSFileSnapshot, error) {
	snapshot := mailTLSFileSnapshot{path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshot, nil
		}
		return snapshot, err
	}
	if !info.Mode().IsRegular() {
		return snapshot, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	snapshot.existed = true
	snapshot.data = data
	snapshot.mode = info.Mode().Perm()
	return snapshot, nil
}

func (snapshot mailTLSFileSnapshot) restore() error {
	if !snapshot.existed {
		if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(snapshot.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(snapshot.path, snapshot.data, snapshot.mode)
}

func snapshotMailTLSState() (*mailTLSStateSnapshot, error) {
	snapshot := &mailTLSStateSnapshot{}
	for _, name := range postfixTLSManagedSettings {
		out, err := runMailTLSCommand("postconf", "-h", name)
		if err != nil {
			return nil, mailTLSCommandError("postconf read "+name, out, err)
		}
		snapshot.postfixSettings = append(snapshot.postfixSettings, postfixTLSSettingSnapshot{
			name:  name,
			value: strings.TrimSpace(string(out)),
		})
	}

	for _, path := range []string{
		postfixSNIPath,
		postfixSNIPath + ".db",
		postfixSNIPath + ".lmdb",
		defaultMailCert,
		defaultMailKey,
	} {
		fileSnapshot, err := snapshotMailTLSFile(path)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		snapshot.files = append(snapshot.files, fileSnapshot)
	}
	dovecotSnapshot, err := snapshotMailTLSFile(dovecotTLSConf)
	if err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", dovecotTLSConf, err)
	}
	snapshot.dovecotFile = dovecotSnapshot
	return snapshot, nil
}

func mailTLSCommandError(label string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", label, err)
	}
	return fmt.Errorf("%s: %s: %w", label, detail, err)
}

func validatePostfixTLSConfig() error {
	out, err := runMailTLSCommand("postfix", "check")
	if err != nil {
		return mailTLSCommandError("postfix check", out, err)
	}
	return nil
}

func validateDovecotTLSConfig() error {
	out, err := runMailTLSCommand("doveconf", "-n")
	if err != nil {
		return mailTLSCommandError("doveconf -n", out, err)
	}
	return nil
}

func reloadMailTLSService(service string) error {
	out, err := runMailTLSCommand("systemctl", "reload-or-restart", service)
	if err != nil {
		return mailTLSCommandError("systemctl reload-or-restart "+service, out, err)
	}
	return nil
}

func (snapshot *mailTLSStateSnapshot) rollback() error {
	var rollbackErrors []error
	for _, fileSnapshot := range snapshot.files {
		if err := fileSnapshot.restore(); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", fileSnapshot.path, err))
		}
	}
	if err := snapshot.dovecotFile.restore(); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", snapshot.dovecotFile.path, err))
	}
	for _, setting := range snapshot.postfixSettings {
		out, err := runMailTLSCommand("postconf", "-e", setting.name+"="+setting.value)
		if err != nil {
			rollbackErrors = append(rollbackErrors, mailTLSCommandError("restore postconf "+setting.name, out, err))
		}
	}

	if err := validatePostfixTLSConfig(); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback postfix validation: %w", err))
	} else if err := reloadMailTLSService("postfix"); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback postfix reload: %w", err))
	}
	if err := validateDovecotTLSConfig(); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback dovecot validation: %w", err))
	} else if err := reloadMailTLSService("dovecot"); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback dovecot reload: %w", err))
	}
	return errors.Join(rollbackErrors...)
}

func setMailTLSFailure(resp *SecureMailTLSResponse, stage string, failure error, snapshot *mailTLSStateSnapshot) {
	resp.Configured = false
	resp.DefaultCert = ""
	resp.SNICount = 0
	resp.Error = fmt.Sprintf("%s: %v", stage, failure)
	if rollbackErr := snapshot.rollback(); rollbackErr != nil {
		resp.Error += fmt.Sprintf("; rollback incomplete: %v", rollbackErr)
		return
	}
	resp.Detail = "previous mail TLS state restored, validated, and reloaded"
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
		if out, err := runMailTLSCommand("postconf", "-e", s[0]+"="+s[1]); err != nil {
			return mailTLSCommandError("postconf "+s[0], out, err)
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
	if out, err := runMailTLSCommand("postmap", "-F", mt+":"+postfixSNIPath); err != nil {
		return mailTLSCommandError("postmap -F", out, err)
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
