package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Mail RPC Methods
// Manages Postfix/Dovecot via file-based configuration
// Files:
// /etc/postfix/vmailbox (mailbox domains and users)
// /etc/postfix/virtual (aliases and forwardings)
// /etc/dovecot/users (authentication)

// Production paths are the real Postfix/Dovecot map files (root agent).
// CELIKPANEL_MAIL_DIR redirects all four to one directory so a non-root
// development agent can exercise the full account flow.
// Üretim yolları gerçek Postfix/Dovecot map dosyalarıdır (root agent).
// CELIKPANEL_MAIL_DIR dördünü tek dizine yönlendirir; böylece root olmayan
// bir geliştirme agent'ı tam hesap akışını çalıştırabilir.
var (
	postfixVBoxPath    = "/etc/postfix/vmailbox"
	postfixVirtualPath = "/etc/postfix/virtual"
	dovecotUsersPath   = "/etc/dovecot/users"
	mailRootDir        = "/var/mail/vhosts"
)

func init() {
	if d := os.Getenv("CELIKPANEL_MAIL_DIR"); d != "" {
		postfixVBoxPath = filepath.Join(d, "vmailbox")
		postfixVirtualPath = filepath.Join(d, "virtual")
		dovecotUsersPath = filepath.Join(d, "dovecot-users")
		mailRootDir = filepath.Join(d, "vhosts")
		_ = os.MkdirAll(d, 0o700)
	}
}

var mailMutex sync.Mutex

// MailAccount represents a mail user
type MailAccount struct {
	Email    string `json:"email"`
	Password string `json:"password,omitempty"` // Plain text for creation (hashed by agent)
	QuotaMB  int    `json:"quota_mb"`
}

// MailForwarding represents an alias
type MailForwarding struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// MailConfigSyncRequest contains full state to sync files
type MailConfigSyncRequest struct {
	Accounts    []MailAccount    `json:"accounts"`
	Forwardings []MailForwarding `json:"forwardings"`
	Domains     []string         `json:"domains"`
}

// SyncMailConfig updates all mail configuration files
func (a *Agent) SyncMailConfig(req *MailConfigSyncRequest, resp *bool) error {
	mailMutex.Lock()
	defer mailMutex.Unlock()

	// 1. Update /etc/postfix/vmailbox
	// Format: user@domain domain/user/
	var vmailboxLines []string
	for _, acc := range req.Accounts {
		parts := strings.Split(acc.Email, "@")
		if len(parts) != 2 {
			continue
		}
		domain, user := parts[1], parts[0]
		vmailboxLines = append(vmailboxLines, fmt.Sprintf("%s %s/%s/", acc.Email, domain, user))
	}

	// Also add domains to vmailbox? specific configuration dependent.
	// Usually `virtual_mailbox_domains` in main.cf handles domains,
	// or we add `domain.com dummy` in vmailbox if using `virtual_mailbox_domains = hash:/etc/postfix/vmailbox`

	if err := updateMapFile(postfixVBoxPath, vmailboxLines); err != nil {
		return err
	}

	// 2. Update /etc/postfix/virtual (Forwardings)
	// Format: source destination
	var virtualLines []string
	// Include accounts as self-aliases? Usually vmailbox takes precedence,
	// but some setups need user@domain user@domain mapping in virtual.
	// We'll stick to just forwardings for now.
	for _, fwd := range req.Forwardings {
		virtualLines = append(virtualLines, fmt.Sprintf("%s %s", fwd.Source, fwd.Destination))
	}

	if err := updateMapFile(postfixVirtualPath, virtualLines); err != nil {
		return err
	}

	// 3. Update /etc/dovecot/users (Auth)
	// Format: user@domain:{SHA512-CRYPT}hash...::home::userdb_quota_rule=*:storage=1G
	// We need to read existing file to preserve passwords if not provided?
	// The sync request should probably contain HASHED passwords from DB if we want to sync fully.
	// OR: RPC just adds/removes individual users. Syncing ALL passwords is unsafe if we don't have them in plaintext (we shouldn't).

	// REVISION: "Sync" approach is bad for passwords.
	// We should use Add/Delete/UpdatePassword methods.

	return nil
}

// AddMailAccount creates a new mail account
func (a *Agent) AddMailAccount(req *MailAccount, resp *bool) error {
	mailMutex.Lock()
	defer mailMutex.Unlock()

	// 1. Create hash
	hash, err := generateDovecotHash(req.Password)
	if err != nil {
		return err
	}

	// 2. Add to dovecot users
	// user@domain:{SCHEME}hash::uid:gid::home::userdb_quota_rule=*:storage=XXM
	line := fmt.Sprintf("%s:%s::::::userdb_quota_rule=*:storage=%dM", req.Email, hash, req.QuotaMB)
	if err := appendToFile(dovecotUsersPath, line); err != nil {
		return err
	}

	// 3. Add to Postfix vmailbox
	parts := strings.Split(req.Email, "@")
	domain, user := parts[1], parts[0]
	vboxLine := fmt.Sprintf("%s %s/%s/", req.Email, domain, user)
	if err := appendToFile(postfixVBoxPath, vboxLine); err != nil {
		return err
	}

	// Postfix needs the domain registered as a virtual mailbox domain,
	// separately from the mailbox map, or it rejects mail for it.
	// Postfix, domain'i posta kutusu haritasından ayrı olarak sanal posta
	// kutusu domain'i diye kayıtlı ister; yoksa o domain'e postayı reddeder.
	ensurePostfixDomain(domain)

	// Rebuild map
	postmapReadable(postfixVBoxPath)

	// 4. Create the maildir owned by vmail (uid/gid 5000), the user postfix
	// delivers as. A dev agent has no vmail/maildir and skips this.
	// 4. vmail (uid/gid 5000) sahipliğinde maildir oluştur; postfix bu
	// kullanıcı olarak teslim eder. Dev agent'ın vmail/maildir'i yok, atlar.
	if os.Getenv("CELIKPANEL_MAIL_DIR") == "" {
		maildir := filepath.Join(mailRootDir, domain, user)
		if err := os.MkdirAll(maildir, 0o700); err != nil {
			return err
		}
		if uid, err := strconv.Atoi(vmailUID); err == nil {
			gid, _ := strconv.Atoi(vmailGID)
			_ = chownRecursive(filepath.Join(mailRootDir, domain), uid, gid)
		}
	}

	*resp = true
	return nil
}

// chownRecursive chowns a path and everything under it — the maildir plus its
// parent domain directory must belong to vmail for postfix to deliver.
// chownRecursive, bir yolu ve altındaki her şeyi chown eder — maildir ve üst
// domain dizini, postfix teslim edebilsin diye vmail'e ait olmalı.
func chownRecursive(root string, uid, gid int) error {
	return filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(p, uid, gid)
	})
}

// DeleteMailAccount removes a mail account
func (a *Agent) DeleteMailAccount(req *struct{ Email string }, resp *bool) error {
	mailMutex.Lock()
	defer mailMutex.Unlock()

	// Remove from dovecot users
	if err := removeLineFromFile(dovecotUsersPath, req.Email+":"); err != nil {
		return err
	}

	// Remove from postfix vmailbox
	if err := removeLineFromFile(postfixVBoxPath, req.Email+" "); err != nil {
		return err
	}
	postmapReadable(postfixVBoxPath)

	// Optional: Delete mail data?
	// Usually safer to keep data or move to trash?
	// We'll leave data for now implementation-wise.

	*resp = true
	return nil
}

// UpdateMailForwarding updates aliases
func (a *Agent) UpdateMailForwarding(req *struct {
	Forwardings []MailForwarding `json:"forwardings"`
}, resp *bool) error {
	mailMutex.Lock()
	defer mailMutex.Unlock()

	var lines []string
	for _, f := range req.Forwardings {
		lines = append(lines, fmt.Sprintf("%s %s", f.Source, f.Destination))
	}

	if err := updateMapFile(postfixVirtualPath, lines); err != nil {
		return err
	}

	postmapReadable(postfixVirtualPath)
	*resp = true
	return nil
}

// ImportMailAccount adds an account with an ALREADY-HASHED password (cPanel
// migration: shadow files carry crypt hashes, dovecot verifies them via the
// {CRYPT} scheme — users keep their passwords across the migration).
// ImportMailAccount, parolası ZATEN ÖZETLENMİŞ bir hesap ekler (cPanel göçü:
// shadow dosyaları crypt özetleri taşır, dovecot bunları {CRYPT} şemasıyla
// doğrular — kullanıcılar göçte parolalarını korur).
func (a *Agent) ImportMailAccount(req *struct {
	Email     string `json:"email"`
	CryptHash string `json:"crypt_hash"`
	QuotaMB   int    `json:"quota_mb"`
}, resp *bool) error {
	mailMutex.Lock()
	defer mailMutex.Unlock()

	parts := strings.Split(req.Email, "@")
	if len(parts) != 2 || req.CryptHash == "" || strings.ContainsAny(req.CryptHash, ":\n") {
		return fmt.Errorf("invalid email or hash")
	}

	// Re-importing the same account replaces its line (idempotent import).
	// Aynı hesabı yeniden içe almak satırını değiştirir (bağımsız içe aktarım).
	_ = removeLineFromFile(dovecotUsersPath, req.Email+":")
	quota := req.QuotaMB
	if quota <= 0 {
		quota = 1024
	}
	line := fmt.Sprintf("%s:{CRYPT}%s::::::userdb_quota_rule=*:storage=%dM", req.Email, req.CryptHash, quota)
	if err := appendToFile(dovecotUsersPath, line); err != nil {
		return err
	}

	domain, user := parts[1], parts[0]
	_ = removeLineFromFile(postfixVBoxPath, req.Email+" ")
	if err := appendToFile(postfixVBoxPath, fmt.Sprintf("%s %s/%s/", req.Email, domain, user)); err != nil {
		return err
	}
	postmapReadable(postfixVBoxPath)

	maildir := filepath.Join(mailRootDir, domain, user)
	if err := os.MkdirAll(maildir, 0700); err != nil {
		return err
	}

	*resp = true
	return nil
}

// UpdateMailQuota rewrites the quota rule on an existing dovecot users line.
// UpdateMailQuota, mevcut bir dovecot kullanıcı satırındaki kota kuralını
// yeniden yazar.
func (a *Agent) UpdateMailQuota(req *struct {
	Email   string `json:"email"`
	QuotaMB int    `json:"quota_mb"`
}, resp *bool) error {
	mailMutex.Lock()
	defer mailMutex.Unlock()

	content, err := os.ReadFile(dovecotUsersPath)
	if err != nil {
		return fmt.Errorf("cannot read dovecot users: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if !strings.HasPrefix(line, req.Email+":") {
			continue
		}
		found = true
		// The quota rule is the extra-fields tail; replace it wholesale so a
		// hand-edited line cannot leave two conflicting rules behind.
		// Kota kuralı ek-alanlar kuyruğudur; elle düzenlenmiş bir satır iki
		// çelişen kural bırakamasın diye tümüyle değiştirilir.
		if idx := strings.Index(line, "userdb_quota_rule="); idx >= 0 {
			lines[i] = line[:idx] + fmt.Sprintf("userdb_quota_rule=*:storage=%dM", req.QuotaMB)
		} else {
			lines[i] = line + fmt.Sprintf(":userdb_quota_rule=*:storage=%dM", req.QuotaMB)
		}
	}
	if !found {
		return fmt.Errorf("mail account not found")
	}

	if err := os.WriteFile(dovecotUsersPath, []byte(strings.Join(lines, "\n")), 0644); err != nil { //nosec G703 -- fixed dovecot users path
		return err
	}
	*resp = true
	return nil
}

// MailQuotaUsage is one account's live quota state from doveadm.
// MailQuotaUsage, doveadm'den bir hesabın canlı kota durumudur.
type MailQuotaUsage struct {
	Email     string `json:"email"`
	UsedKB    int64  `json:"used_kb"`
	LimitKB   int64  `json:"limit_kb"` // 0 = unlimited / kural yok
	Available bool   `json:"available"`
}

type MailQuotaStatusRequest struct {
	Emails []string `json:"emails"`
}

type MailQuotaStatusResponse struct {
	PluginEnabled bool             `json:"plugin_enabled"`
	Usages        []MailQuotaUsage `json:"usages"`
}

// GetMailQuotaStatus reports whether dovecot's quota plugin is active and,
// per account, the live usage from `doveadm quota get`. Honesty first: when
// the plugin is off, the panel must say quotas are NOT being enforced instead
// of showing stored numbers as if they were.
//
// GetMailQuotaStatus, dovecot'un quota eklentisinin etkin olup olmadığını ve
// hesap başına `doveadm quota get` canlı kullanımını bildirir. Önce dürüstlük:
// eklenti kapalıyken panel, saklanan sayıları uygulanıyormuş gibi göstermek
// yerine kotaların uygulanMAdığını söylemelidir.
func (a *Agent) GetMailQuotaStatus(req *MailQuotaStatusRequest, resp *MailQuotaStatusResponse) error {
	if out, err := exec.Command("doveconf", "-h", "mail_plugins").Output(); err == nil {
		resp.PluginEnabled = strings.Contains(string(out), "quota")
	}

	resp.Usages = make([]MailQuotaUsage, 0, len(req.Emails))
	if !resp.PluginEnabled {
		return nil
	}

	for _, email := range req.Emails {
		u := MailQuotaUsage{Email: email}
		out, err := exec.Command("doveadm", "quota", "get", "-u", email).Output()
		if err == nil {
			// Output columns: Quota name / Type / Value(KB) / Limit / %
			// Çıktı sütunları: kota adı / tür / değer(KB) / sınır / %
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 5 || fields[len(fields)-4] != "STORAGE" {
					continue
				}
				fmt.Sscanf(fields[len(fields)-3], "%d", &u.UsedKB)
				if fields[len(fields)-2] != "-" {
					fmt.Sscanf(fields[len(fields)-2], "%d", &u.LimitKB)
				}
				u.Available = true
			}
		}
		resp.Usages = append(resp.Usages, u)
	}
	return nil
}

// Helpers

func updateMapFile(path string, lines []string) error {
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func appendToFile(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}
	return nil
}

func removeLineFromFile(path, prefix string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, prefix) {
			newLines = append(newLines, line)
		}
	}

	return os.WriteFile(path, []byte(strings.Join(newLines, "\n")+"\n"), 0644) //nosec G703 -- callers pass fixed system map-file paths (postfix/dovecot)
}

func generateDovecotHash(password string) (string, error) {
	// Use doveadm pw if available
	cmd := exec.Command("doveadm", "pw", "-s", "SHA512-CRYPT", "-p", password)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Output format: {SHA512-CRYPT}$6$....
	return strings.TrimSpace(string(output)), nil
}
