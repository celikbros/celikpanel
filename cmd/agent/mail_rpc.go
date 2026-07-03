package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Mail RPC Methods
// Manages Postfix/Dovecot via file-based configuration
// Files:
// /etc/postfix/vmailbox (mailbox domains and users)
// /etc/postfix/virtual (aliases and forwardings)
// /etc/dovecot/users (authentication)

const (
	postfixVBoxPath  = "/etc/postfix/vmailbox"
	postfixVirtualPath = "/etc/postfix/virtual"
	dovecotUsersPath = "/etc/dovecot/users"
	mailRootDir      = "/var/mail/vhosts"
)

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
	
	// Rebuild map
	exec.Command("postmap", postfixVBoxPath).Run()

	// 4. Create directory structure
	maildir := filepath.Join(mailRootDir, domain, user)
	if err := os.MkdirAll(maildir, 0700); err != nil {
		return err
	}
	// Chown to vmail user (usually 5000:5000 or similar)
	// For now we assume agent runs as root, checking vmail uid/gid
	// exec.Command("chown", "-R", "vmail:vmail", maildir).Run() // TODO: Determine correct user

	*resp = true
	return nil
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
	exec.Command("postmap", postfixVBoxPath).Run()

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

	exec.Command("postmap", postfixVirtualPath).Run()
	*resp = true
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
