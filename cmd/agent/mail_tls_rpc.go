package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
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
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
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
	defaultMailCert   = transport.DefaultMailTLSCertificatePath
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

var lookupMailTLSCommand = exec.LookPath

type MailSNIEntry = transport.MailSNIEntry
type SecureMailTLSRequest = transport.SecureMailTLSRequest
type ReconcileMailTLSMutationRequest = transport.ReconcileMailTLSMutationRequest
type SyncMailTLSV2Request = transport.SyncMailTLSV2Request
type SecureMailTLSResponse = transport.SecureMailTLSResponse

const mailTLSLegacyUnsupportedError = "legacy mail TLS mutation is unsupported; use Agent.SyncMailTLSV2 with a payload-bound direct mutation lease"

type mailTLSCommandRunner func(name string, args ...string) ([]byte, error)

// defaultMailTLSWriter publishes one half of the default mail certificate pair
// with the ownership the managed directory carries. The owner is not optional:
// the agent runs as User=root with Group=celikpanel, so a file it creates
// inherits group celikpanel, while the managed directory is root:root - and
// the readback below demands that the file and its directory agree.
// defaultMailTLSWriter, varsayilan posta sertifika ciftinin bir yarisini,
// yonetilen dizinin tasidigi sahiplikle yayimlar. Sahip istege bagli degildir:
// agent User=root ve Group=celikpanel ile calistigi icin olusturdugu dosya
// celikpanel grubunu devralir; oysa yonetilen dizin root:root'tur ve asagidaki
// geri-okuma dosya ile dizininin ortusmesini sart kosar.
type defaultMailTLSWriter func(
	path string,
	content []byte,
	mode os.FileMode,
	owner mailTLSDirectoryOwner,
) error

// secureWriteDefaultMailTLSFile is the production writer for that pair.
// secureWriteDefaultMailTLSFile, o cift icin uretim yazicisidir.
func secureWriteDefaultMailTLSFile(
	path string,
	content []byte,
	mode os.FileMode,
	owner mailTLSDirectoryOwner,
) error {
	if owner.uid != uint64(uint32(owner.uid)) || owner.gid != uint64(uint32(owner.gid)) {
		return fmt.Errorf("managed mail TLS directory owner is out of range")
	}
	return secureWriteConfigOwnedBy(
		path, content, mode, uint32(owner.uid), uint32(owner.gid),
	)
}

type mailTLSDirectoryOwner struct {
	uid uint64
	gid uint64
}

type mailTLSCommandPreflight struct {
	run        mailTLSCommandRunner
	sniMapType string
}

type mailTLSFileSnapshot struct {
	path    string
	existed bool
	data    []byte
	mode    os.FileMode
	uid     int
	gid     int
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

func configuredMailTLSManagedRoot() (string, error) {
	raw := os.Getenv("CELIKPANEL_CUSTOM_CERT_ROOT")
	if raw == "" {
		return "/etc/ssl/celikpanel", nil
	}
	root := strings.TrimSpace(raw)
	if root != raw {
		return "", errors.New("managed certificate root is not canonical")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root ||
		root == string(filepath.Separator) {
		return "", errors.New("managed certificate root is not a canonical absolute path")
	}
	return filepath.ToSlash(root), nil
}

// SecureMailTLS wires TLS into the whole mail stack: default certificate,
// per-domain SNI on both daemons, modern protocol floor, opportunistic
// outbound TLS and a fixed HELO name. Safe to re-run; it converges.
// SecureMailTLS, TLS'i tüm posta yığınına bağlar: varsayılan sertifika, iki
// daemon'da da domain başına SNI, modern protokol tabanı, fırsatçı giden TLS
// ve sabit HELO adı. Yeniden koşmak güvenlidir; yakınsar.
func (a *Agent) SecureMailTLS(req *SecureMailTLSRequest, resp *SecureMailTLSResponse) error {
	if resp == nil {
		return fmt.Errorf("mail TLS response is required")
	}
	*resp = SecureMailTLSResponse{}
	resp.Error = mailTLSLegacyUnsupportedError
	return nil
}

// ReconcileMailTLSMutation is the durable, lease-bound entry point used by
// multi-service orchestration. It deliberately reuses the legacy lifecycle
// implementation, but every host command is executed through the tracked
// service-mutation runner below.
func (a *Agent) ReconcileMailTLSMutation(
	req *ReconcileMailTLSMutationRequest,
	resp *SecureMailTLSResponse,
) error {
	if resp == nil {
		return fmt.Errorf("mail TLS reconciliation response is required")
	}
	*resp = SecureMailTLSResponse{}
	resp.Error = mailTLSLegacyUnsupportedError
	return nil
}

// SyncMailTLSV2 freezes the full snapshot before touching the mutation ledger
// or host, then executes it only under the exact direct payload-bound lease.
func (a *Agent) SyncMailTLSV2(
	req *SyncMailTLSV2Request,
	resp *SecureMailTLSResponse,
) error {
	if resp == nil {
		return fmt.Errorf("mail TLS V2 response is required")
	}
	*resp = SecureMailTLSResponse{}
	if req == nil {
		resp.Error = "mail TLS V2 request is required"
		return nil
	}
	managedRoot, err := configuredMailTLSManagedRoot()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	commitment, err := mutationpayload.CanonicalMailTLSSync(
		managedRoot,
		req.Myhostname,
		req.SNI,
	)
	if err != nil {
		resp.Error = fmt.Sprintf("mail TLS V2 validation: %v", err)
		return nil
	}
	if err := requireExpectedBuildCommit(
		req.ExpectedBuildCommit,
		"synchronizing the mail TLS stack",
	); err != nil {
		resp.Error = err.Error()
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(
			serviceMutationStepSyncMailTLS,
			"mail-tls",
			commitment.Qualifier,
			"sync",
		),
	)
	if err != nil {
		*resp = SecureMailTLSResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()

	if err := lockMailMutation(ctx); err != nil {
		resp.Error = fmt.Sprintf(
			"service mutation lease expired before mail TLS reconciliation: %v",
			err,
		)
		return nil
	}
	defer mailMutex.Unlock()
	if err := ctx.Err(); err != nil {
		resp.Error = fmt.Sprintf("service mutation lease expired before mail TLS V2 synchronization: %v", err)
		return nil
	}
	return syncMailTLSV2(ctx, commitment, resp)
}

func reconcileMailTLS(
	req *SecureMailTLSRequest,
	resp *SecureMailTLSResponse,
	run mailTLSCommandRunner,
) error {
	_, err := reconcileMailTLSHost(req, resp, run)
	return err
}

// reconcileMailTLSHost is reconcileMailTLS with the one fact its durable
// callers need beside the response: what the run left on the host.
// reconcileMailTLSHost, reconcileMailTLS'in kalici cagiranlarinin yanitin
// yaninda ihtiyac duydugu tek olguyu da veren halidir: kosunun makinede ne
// biraktigi.
func reconcileMailTLSHost(
	req *SecureMailTLSRequest,
	resp *SecureMailTLSResponse,
	run mailTLSCommandRunner,
) (mailTLSHostOutcome, error) {

	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		resp.Error = "mail TLS is a production action; not available with CELIKPANEL_MAIL_DIR set"
		return mailTLSHostUntouched, nil
	}
	myhostname, valid, err := validateSecureMailTLSRequest(req)
	if err != nil {
		resp.Error = fmt.Sprintf("mail TLS validation: %v", err)
		return mailTLSHostUntouched, nil
	}
	preflight, err := preflightMailTLSCommands(len(valid) > 0, run)
	if err != nil {
		resp.Error = err.Error()
		return mailTLSHostUntouched, nil
	}
	run = preflight.run

	previous, err := snapshotMailTLSState(run)
	if err != nil {
		resp.Error = fmt.Sprintf("mail TLS snapshot: %v", err)
		return mailTLSHostUntouched, nil
	}
	if err := ensureDefaultMailCert(myhostname); err != nil {
		return setMailTLSFailure(resp, "default certificate", err, previous, run), nil
	}
	resp.DefaultCert = defaultMailCert

	if err := configurePostfixTLS(myhostname, valid, preflight.sniMapType, run); err != nil {
		return setMailTLSFailure(resp, "postfix configuration", err, previous, run), nil
	}
	if err := configureDovecotTLS(valid, run); err != nil {
		return setMailTLSFailure(resp, "dovecot configuration", err, previous, run), nil
	}
	if err := validatePostfixTLSConfig(run); err != nil {
		return setMailTLSFailure(resp, "postfix validation", err, previous, run), nil
	}
	if err := reloadMailTLSService("postfix", run); err != nil {
		return setMailTLSFailure(resp, "postfix reload", err, previous, run), nil
	}
	if err := reloadMailTLSService("dovecot", run); err != nil {
		return setMailTLSFailure(resp, "dovecot reload", err, previous, run), nil
	}

	resp.Configured = true
	resp.SNICount = len(valid)
	resp.Detail = fmt.Sprintf("mail TLS active (%d SNI entries)", len(valid))
	return mailTLSHostConverged, nil
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

func preflightMailTLSCommands(needsSNIMap bool, execute mailTLSCommandRunner) (mailTLSCommandPreflight, error) {
	if execute == nil {
		return mailTLSCommandPreflight{}, fmt.Errorf("mail TLS command runner is required")
	}
	required := []string{"postconf", "doveconf", "postfix", "dovecot", "systemctl"}
	if needsSNIMap {
		required = append(required, "postmap")
	}
	resolved := make(map[string]string, len(required))
	for _, name := range required {
		commandPath, err := lookupMailTLSCommand(name)
		if err != nil {
			switch name {
			case "postconf":
				return mailTLSCommandPreflight{}, fmt.Errorf("postfix is not installed")
			case "doveconf":
				return mailTLSCommandPreflight{}, fmt.Errorf("dovecot is not installed")
			default:
				return mailTLSCommandPreflight{}, fmt.Errorf("mail TLS prerequisite command %q is unavailable: %w", name, err)
			}
		}
		if commandPath != strings.TrimSpace(commandPath) || !filepath.IsAbs(commandPath) || filepath.Clean(commandPath) != commandPath {
			return mailTLSCommandPreflight{}, fmt.Errorf("mail TLS prerequisite command %q did not resolve to a canonical absolute path", name)
		}
		resolved[name] = commandPath
	}
	pinned := func(name string, args ...string) ([]byte, error) {
		commandPath, ok := resolved[name]
		if !ok {
			return nil, fmt.Errorf("mail TLS command %q was not pinned during preflight", name)
		}
		return execute(commandPath, args...)
	}
	preflight := mailTLSCommandPreflight{run: pinned}
	if needsSNIMap {
		mapType, err := probePostfixTLSMapType(pinned)
		if err != nil {
			return mailTLSCommandPreflight{}, err
		}
		preflight.sniMapType = mapType
	}
	return preflight, nil
}

func probePostfixTLSMapType(run mailTLSCommandRunner) (string, error) {
	dir, err := os.MkdirTemp("", "celikpanel-mail-tls-map-")
	if err != nil {
		return "", fmt.Errorf("prepare Postfix SNI map preflight: %w", err)
	}
	defer os.RemoveAll(dir)
	keyPath := filepath.Join(dir, "probe-key.pem")
	certPath := filepath.Join(dir, "probe-cert.pem")
	if err := os.WriteFile(keyPath, []byte("probe private key\n"), 0o600); err != nil {
		return "", fmt.Errorf("prepare Postfix SNI key probe: %w", err)
	}
	if err := os.WriteFile(certPath, []byte("probe certificate\n"), 0o600); err != nil {
		return "", fmt.Errorf("prepare Postfix SNI certificate probe: %w", err)
	}
	probe := filepath.Join(dir, "probe")
	probeLine := fmt.Sprintf("probe.example.invalid %s %s\n", keyPath, certPath)
	if err := os.WriteFile(probe, []byte(probeLine), 0o600); err != nil {
		return "", fmt.Errorf("prepare Postfix SNI map probe: %w", err)
	}
	for _, mapType := range []string{"lmdb", "hash", "btree"} {
		for _, extension := range []string{".db", ".lmdb"} {
			if err := os.Remove(probe + extension); err != nil && !os.IsNotExist(err) {
				return "", fmt.Errorf("reset Postfix SNI map probe: %w", err)
			}
		}
		if _, err := run("postmap", "-F", mapType+":"+probe); err != nil {
			continue
		}
		if fileExists(probe+".db") || fileExists(probe+".lmdb") {
			return mapType, nil
		}
	}
	return "", fmt.Errorf("postfix on this system has no usable indexed table type (lmdb/hash/btree); per-domain mail certificates require one")
}

func validateMailSNIEntries(entries []MailSNIEntry) ([]MailSNIEntry, error) {
	if len(entries) > maxMailSNIEntries {
		return nil, fmt.Errorf("too many entries")
	}
	if len(entries) == 0 {
		return nil, nil
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
		if err := verifyManagedCertificateSnapshot(
			certDomain, certPath, keyPath, "",
		); err != nil {
			return nil, fmt.Errorf("entry %d immutable certificate snapshot: %w", entryIndex+1, err)
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

func mailTLSFileGroup(info os.FileInfo) (uint64, bool) {
	return mailTLSStatUint(info, "Gid")
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
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshot, nil
		}
		return snapshot, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return snapshot, fmt.Errorf("%s is not a regular file", path)
	}
	if links, ok := mailTLSFileLinkCount(info); ok && links != 1 {
		return snapshot, fmt.Errorf("%s has %d hard links", path, links)
	}
	data, mode, uid, gid, err := secureSnapshotMailFile(path)
	if err != nil {
		return snapshot, err
	}
	snapshot.existed = true
	snapshot.data = data
	snapshot.mode = mode
	snapshot.uid = uid
	snapshot.gid = gid
	return snapshot, nil
}

func (snapshot mailTLSFileSnapshot) restore() error {
	if !snapshot.existed {
		if err := secureRemoveConfig(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := secureWriteConfig(snapshot.path, snapshot.data, snapshot.mode); err != nil {
		return err
	}
	return secureSetMailFileMetadata(
		snapshot.path,
		snapshot.mode,
		snapshot.uid,
		snapshot.gid,
	)
}

func snapshotMailTLSState(run mailTLSCommandRunner) (*mailTLSStateSnapshot, error) {
	snapshot := &mailTLSStateSnapshot{}
	for _, name := range postfixTLSManagedSettings {
		out, err := run("postconf", "-h", name)
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

func validatePostfixTLSConfig(run mailTLSCommandRunner) error {
	out, err := run("postfix", "check")
	if err != nil {
		return mailTLSCommandError("postfix check", out, err)
	}
	return nil
}

func validateDovecotTLSConfig(run mailTLSCommandRunner) error {
	out, err := run("doveconf", "-n")
	if err != nil {
		return mailTLSCommandError("doveconf -n", out, err)
	}
	return nil
}

func reloadMailTLSService(service string, run mailTLSCommandRunner) error {
	out, err := run("systemctl", "reload-or-restart", service)
	if err != nil {
		return mailTLSCommandError("systemctl reload-or-restart "+service, out, err)
	}
	return nil
}

func (snapshot *mailTLSStateSnapshot) rollback(run mailTLSCommandRunner) error {
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
		out, err := run("postconf", "-e", setting.name+"="+setting.value)
		if err != nil {
			rollbackErrors = append(rollbackErrors, mailTLSCommandError("restore postconf "+setting.name, out, err))
		}
	}

	if err := validatePostfixTLSConfig(run); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback postfix validation: %w", err))
	} else if err := reloadMailTLSService("postfix", run); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback postfix reload: %w", err))
	}
	if err := validateDovecotTLSConfig(run); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback dovecot validation: %w", err))
	} else if err := reloadMailTLSService("dovecot", run); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback dovecot reload: %w", err))
	}
	return errors.Join(rollbackErrors...)
}

// What a reconciliation leaves behind on the host is the fact a committed
// plan's failure has to be judged on, and the judgement is not this path's to
// make: it lives once, in host_mutation_outcome.go, next to the ledger it
// protects. This path asked the question first (R-046); the firewall asked it
// again (R-054); R-055 asked for it to stop being redrawn. These names stay
// because this path's tests and call sites read in this path's vocabulary -
// they are the same type and the same four values, not a second set.
//
// Bir uzlastirmanin makinede biraktigi durum, taahhut edilmis bir planin
// basarisizliginin uzerinde yargilanacagi olgudur; ancak yargi bu yolun degil,
// defterin yanindaki tek yerin isidir. Bu adlar, bu yolun testleri ve cagri
// yerleri kendi sozlugunde okunsun diye durur; ayni tur ve ayni dort degerdir.
type mailTLSHostOutcome = hostMutationOutcome

const (
	// mailTLSHostUntouched: the failure happened before any host change.
	mailTLSHostUntouched = hostMutationUntouched
	// mailTLSHostRestored: the host was changed, then every managed file and
	// postfix setting was restored, both daemon configurations validated and
	// both daemons reloaded.
	mailTLSHostRestored = hostMutationRestored
	// mailTLSHostConverged: the committed plan is applied.
	mailTLSHostConverged = hostMutationConverged
	// mailTLSHostAmbiguous: the host was changed and the restoration could not
	// be proved. This is the only outcome that may hold the ledger.
	mailTLSHostAmbiguous = hostMutationAmbiguous
)

func setMailTLSFailure(
	resp *SecureMailTLSResponse,
	stage string,
	failure error,
	snapshot *mailTLSStateSnapshot,
	run mailTLSCommandRunner,
) mailTLSHostOutcome {
	resp.Configured = false
	resp.DefaultCert = ""
	resp.SNICount = 0
	resp.Error = fmt.Sprintf("%s: %v", stage, failure)
	if rollbackErr := snapshot.rollback(run); rollbackErr != nil {
		resp.Error += fmt.Sprintf("; rollback incomplete: %v", rollbackErr)
		return mailTLSHostAmbiguous
	}
	resp.Detail = "previous mail TLS state restored, validated, and reloaded"
	return mailTLSHostRestored
}

// ensureDefaultMailCert creates a self-signed certificate for the machine's
// hostname once. Opportunistic SMTP TLS never validates the chain, so this is
// enough to stop plaintext transport; domains with real certificates override
// it via SNI.
// ensureDefaultMailCert, makinenin hostname'i için bir kez self-signed
// sertifika üretir. Fırsatçı SMTP TLS zinciri asla doğrulamaz; bu, düz metin
// taşımayı durdurmaya yeter. Gerçek sertifikalı domain'ler SNI ile ezer.
func ensureDefaultMailCert(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		host, _ = os.Hostname()
		host = strings.TrimSpace(host)
		if host == "" {
			host = "mail.local"
		}
	}
	return ensureDefaultMailCertPair(
		defaultMailCert,
		defaultMailKey,
		host,
		time.Now(),
		secureWriteDefaultMailTLSFile,
	)
}

func validateDefaultMailTLSDirectoryPaths(certPath, keyPath string) (string, bool, error) {
	for label, candidate := range map[string]string{"certificate": certPath, "private key": keyPath} {
		if candidate != strings.TrimSpace(candidate) || len(candidate) > maxMailTLSPathLen || !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
			return "", false, fmt.Errorf("default mail %s path must be canonical and absolute", label)
		}
	}
	separator := string(filepath.Separator)
	usesProductionTree := func(candidate string) bool {
		return candidate == mailTLSDir || strings.HasPrefix(candidate, mailTLSDir+separator)
	}
	if usesProductionTree(certPath) || usesProductionTree(keyPath) {
		if certPath != defaultMailCert || keyPath != defaultMailKey {
			return "", false, fmt.Errorf("production default mail TLS paths must use the exact certificate and private-key pair")
		}
		return mailTLSDir, true, nil
	}
	certDir := filepath.Dir(certPath)
	if certDir == "." || certDir == string(filepath.Separator) || certDir != filepath.Dir(keyPath) {
		return "", false, fmt.Errorf("default mail certificate and key must share one non-root directory")
	}
	return certDir, false, nil
}

func ensureDefaultMailCertPair(
	certPath string,
	keyPath string,
	host string,
	now time.Time,
	write defaultMailTLSWriter,
) error {
	if write == nil {
		return fmt.Errorf("default mail TLS writer is required")
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("default mail TLS hostname is required")
	}
	owner, err := prepareDefaultMailTLSDirectory(certPath, keyPath)
	if err != nil {
		return err
	}

	certExists, err := inspectDefaultMailTLSFile(certPath, owner, 0o644)
	if err != nil {
		return fmt.Errorf("default mail certificate: %w", err)
	}
	keyExists, err := inspectDefaultMailTLSFile(keyPath, owner, 0o600)
	if err != nil {
		return fmt.Errorf("default mail private key: %w", err)
	}
	if certExists && keyExists {
		if err := validateDefaultMailCertPair(certPath, keyPath, host, now); err == nil {
			return nil
		}
	}

	certPEM, keyPEM, err := generateDefaultMailCertPair(host, now)
	if err != nil {
		return err
	}
	// Each publish uses secureWriteConfig: no-follow temp creation, explicit
	// chmod, file fsync, rename and parent-directory fsync. A crash between the
	// two publishes leaves a detectable mismatch which the next retry replaces.
	if err := write(certPath, certPEM, 0o644, owner); err != nil {
		return fmt.Errorf("publish default mail certificate: %w", err)
	}
	if err := write(keyPath, keyPEM, 0o600, owner); err != nil {
		return fmt.Errorf("publish default mail private key: %w", err)
	}
	if _, err := inspectDefaultMailTLSFile(certPath, owner, 0o644); err != nil {
		return fmt.Errorf("verify default mail certificate metadata: %w", err)
	}
	if _, err := inspectDefaultMailTLSFile(keyPath, owner, 0o600); err != nil {
		return fmt.Errorf("verify default mail private key metadata: %w", err)
	}
	if err := validateDefaultMailCertPair(certPath, keyPath, host, now); err != nil {
		return fmt.Errorf("verify default mail certificate pair: %w", err)
	}
	return nil
}

func inspectDefaultMailTLSFile(
	path string,
	expectedOwner mailTLSDirectoryOwner,
	expectedMode os.FileMode,
) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := requireMailTLSRegularFile(path, &expectedOwner.uid, expectedMode); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	group, ok := mailTLSFileGroup(info)
	if !ok || group != expectedOwner.gid {
		return false, fmt.Errorf("%s group does not match managed directory group", path)
	}
	return true, nil
}

func validateDefaultMailCertPair(certPath, keyPath, host string, now time.Time) error {
	certPEM, err := secureReadConfig(certPath)
	if err != nil {
		return fmt.Errorf("read certificate: %w", err)
	}
	keyPEM, err := secureReadConfig(keyPath)
	if err != nil {
		return fmt.Errorf("read private key: %w", err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("parse certificate and private key: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return fmt.Errorf("certificate chain is empty")
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}
	if now.Before(certificate.NotBefore) {
		return fmt.Errorf("certificate is not valid yet")
	}
	if !now.Before(certificate.NotAfter) {
		return fmt.Errorf("certificate has expired")
	}
	if err := certificate.VerifyHostname(host); err != nil {
		return fmt.Errorf("certificate does not cover %s: %w", host, err)
	}
	if err := certificate.CheckSignature(
		certificate.SignatureAlgorithm,
		certificate.RawTBSCertificate,
		certificate.Signature,
	); err != nil {
		return fmt.Errorf("certificate self-signature: %w", err)
	}
	return nil
}

func generateDefaultMailCertPair(host string, now time.Time) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(0, 0, mailCertValidDays),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

// configurePostfixTLS sets the default certificate, protocol floor,
// opportunistic TLS both ways, HELO name and the SNI map.
// configurePostfixTLS, varsayılan sertifikayı, protokol tabanını, iki yönlü
// fırsatçı TLS'i, HELO adını ve SNI map'ini ayarlar.
func configurePostfixTLS(
	myhostname string,
	sni []MailSNIEntry,
	sniMapType string,
	run mailTLSCommandRunner,
) error {
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
		if sniMapType == "" {
			return fmt.Errorf("Postfix SNI map type was not selected during preflight")
		}
		if err := writePostfixSNIMap(sni, sniMapType, run); err != nil {
			return err
		}
		settings = append(settings, [2]string{"tls_server_sni_maps", sniMapType + ":" + postfixSNIPath})
	} else {
		settings = append(settings, [2]string{"tls_server_sni_maps", ""})
	}
	for _, s := range settings {
		if out, err := run("postconf", "-e", s[0]+"="+s[1]); err != nil {
			return mailTLSCommandError("postconf "+s[0], out, err)
		}
	}
	return nil
}

// writePostfixSNIMap writes the source map and compiles it with postmap -F,
// which embeds the PEM contents into the .db (required for SNI maps).
// writePostfixSNIMap, kaynak map'i yazar ve postmap -F ile derler; -F, PEM
// içeriklerini .db'ye gömer (SNI map'leri için gereklidir).
func writePostfixSNIMap(
	sni []MailSNIEntry,
	mapType string,
	run mailTLSCommandRunner,
) error {
	if mapType != "lmdb" && mapType != "hash" && mapType != "btree" {
		return fmt.Errorf("invalid preflighted Postfix SNI map type %q", mapType)
	}
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
	if err := secureWriteConfig(postfixSNIPath, []byte(b.String()), 0o600); err != nil {
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
	if out, err := run("postmap", "-F", mapType+":"+postfixSNIPath); err != nil {
		return mailTLSCommandError("postmap -F", out, err)
	}
	return nil
}

// configureDovecotTLS writes our TLS drop-in: default certificate plus a
// local_name block per SNI name so IMAP/POP clients get the right chain.
// configureDovecotTLS, TLS ekimizi yazar: varsayılan sertifika artı SNI adı
// başına bir local_name bloğu; IMAP/POP istemcileri doğru zinciri alır.
func configureDovecotTLS(sni []MailSNIEntry, run mailTLSCommandRunner) error {
	if err := os.MkdirAll(filepath.Dir(dovecotTLSConf), 0o755); err != nil {
		return err
	}
	// Dialect-aware (2.3 ssl_cert=< vs 2.4 ssl_server_cert_file=), and
	// validated by dovecot's parser before any restart — see dovecot_dialect.go.
	// Lehçe-farkında (2.3 ssl_cert=< vs 2.4 ssl_server_cert_file=) ve yeniden
	// başlatmadan önce dovecot ayrıştırıcısıyla doğrulanır.
	conf := buildDovecotTLSConf(dovecotIs24WithRunner(run), defaultMailCert, defaultMailKey, sni)
	return applyDovecotTLSConf(dovecotTLSConf, conf, run)
}

func dovecotIs24WithRunner(run mailTLSCommandRunner) bool {
	out, err := run("dovecot", "--version")
	if err != nil {
		return true
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return true
	}
	parts := strings.SplitN(fields[0], ".", 3)
	if len(parts) < 2 {
		return true
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return true
	}
	return major > 2 || (major == 2 && minor >= 4)
}

func applyDovecotTLSConf(
	path string,
	content string,
	run mailTLSCommandRunner,
) error {
	snapshot, err := snapshotMailFile(path)
	if err != nil {
		return fmt.Errorf("snapshot dovecot configuration: %w", err)
	}
	if err := secureWriteConfig(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write dovecot configuration: %w", err)
	}
	out, err := run("doveconf", "-n")
	if err == nil {
		return nil
	}
	if rollbackErr := restoreMailFile(snapshot); rollbackErr != nil {
		return fmt.Errorf(
			"dovecot rejected the configuration and rollback failed: %s: %v",
			dovecotFirstError(string(out)),
			rollbackErr,
		)
	}
	detail := dovecotFirstError(string(out))
	if strings.TrimSpace(string(out)) == "" {
		detail = err.Error()
	}
	return fmt.Errorf("dovecot rejected the configuration: %s", detail)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
