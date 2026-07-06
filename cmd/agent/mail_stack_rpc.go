package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
)

// Wiring Postfix and Dovecot to actually deliver to the virtual mailboxes the
// panel manages. Creating an account writes /etc/dovecot/users and
// /etc/postfix/vmailbox, but those files are inert until the MTA and the IMAP
// server are told to read them — otherwise mail bounces and IMAP falls back
// to system users. This RPC does that one-time wiring, the mail counterpart
// of ConfigurePowerDNSSQLite: run it when the mail stack is installed.
//
// Postfix ve Dovecot'u, panelin yönettiği sanal posta kutularına gerçekten
// teslim edecek şekilde bağlama. Hesap oluşturmak /etc/dovecot/users ve
// /etc/postfix/vmailbox yazar ama MTA ve IMAP sunucusuna bunları okumaları
// söylenmedikçe bu dosyalar ölüdür — yoksa posta geri döner ve IMAP sistem
// kullanıcılarına düşer. Bu RPC o tek-seferlik bağlamayı yapar;
// ConfigurePowerDNSSQLite'ın mail karşılığıdır: mail yığını kurulunca çalıştır.

const (
	vmailUser = "vmail"
	vmailUID  = "5000"
	vmailGID  = "5000"
)

type ConfigureMailStackResponse struct {
	Configured bool   `json:"configured"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (a *Agent) ConfigureMailStack(_ *struct{}, resp *ConfigureMailStackResponse) error {
	// A dev agent runs against CELIKPANEL_MAIL_DIR and never touches the real
	// system config; refuse rather than half-configure.
	// Dev agent CELIKPANEL_MAIL_DIR üzerinde çalışır ve gerçek sistem
	// yapılandırmasına dokunmaz; yarım yapılandırmaktansa reddet.
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		resp.Error = "mail stack configuration is a production action; not available with CELIKPANEL_MAIL_DIR set"
		return nil
	}
	if _, err := exec.LookPath("postconf"); err != nil {
		resp.Error = "postfix is not installed"
		return nil
	}

	if err := ensureVmailUser(); err != nil {
		resp.Error = fmt.Sprintf("vmail user: %v", err)
		return nil
	}

	// Ensure the map files exist before postmap/postfix reference them.
	// postmap/postfix onlara başvurmadan önce map dosyalarının var olmasını sağla.
	for _, p := range []string{postfixVBoxPath, postfixVirtualPath, postfixDomainsPath} {
		if !fileExistsAgent(p) {
			_ = os.WriteFile(p, []byte(""), 0o644)
		}
		postmapReadable(p)
	}

	if err := configurePostfixVirtual(); err != nil {
		resp.Error = fmt.Sprintf("postfix: %v", err)
		return nil
	}
	if err := configureDovecotVirtual(); err != nil {
		resp.Error = fmt.Sprintf("dovecot: %v", err)
		return nil
	}

	if out, err := exec.Command("systemctl", "reload-or-restart", "postfix").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("postfix reload: %s", strings.TrimSpace(string(out)))
		return nil
	}
	if out, err := exec.Command("systemctl", "restart", "dovecot").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("dovecot restart: %s", strings.TrimSpace(string(out)))
		return nil
	}

	resp.Configured = true
	resp.Detail = "postfix + dovecot configured for virtual mailboxes"
	return nil
}

// postfixDomainsPath lists the virtual mailbox domains (postfix needs this
// separately from the mailbox map).
var postfixDomainsPath = "/etc/postfix/vmailbox_domains"

// ensureVmailUser creates the dedicated mailbox owner (uid/gid 5000) that
// owns every maildir; a single non-login system user, the standard virtual-
// mailbox pattern.
// ensureVmailUser, her maildir'in sahibi olan ayrılmış posta kutusu sahibini
// (uid/gid 5000) oluşturur; tek, girişsiz bir sistem kullanıcısı.
func ensureVmailUser() error {
	if _, err := user.Lookup(vmailUser); err == nil {
		return ensureMailRoot()
	}
	if _, err := user.LookupGroup(vmailUser); err != nil {
		if out, err := exec.Command("groupadd", "-g", vmailGID, vmailUser).CombinedOutput(); err != nil {
			return fmt.Errorf("groupadd: %s", strings.TrimSpace(string(out)))
		}
	}
	if out, err := exec.Command("useradd", "-r", "-g", vmailGID, "-u", vmailUID,
		"-d", mailRootDir, "-s", "/usr/sbin/nologin", vmailUser).CombinedOutput(); err != nil {
		return fmt.Errorf("useradd: %s", strings.TrimSpace(string(out)))
	}
	return ensureMailRoot()
}

func ensureMailRoot() error {
	if err := os.MkdirAll(mailRootDir, 0o770); err != nil {
		return err
	}
	uid, _ := strconv.Atoi(vmailUID)
	gid, _ := strconv.Atoi(vmailGID)
	return os.Chown(mailRootDir, uid, gid)
}

// configurePostfixVirtual points Postfix at our maps and delivers unmatched-
// but-hosted mail into the maildirs as the vmail user.
// configurePostfixVirtual, Postfix'i map'lerimize yönlendirir ve barındırılan
// postayı vmail kullanıcısı olarak maildir'lere teslim eder.
func configurePostfixVirtual() error {
	settings := [][2]string{
		{"virtual_mailbox_base", mailRootDir},
		{"virtual_mailbox_domains", "hash:" + postfixDomainsPath},
		{"virtual_mailbox_maps", "hash:" + postfixVBoxPath},
		{"virtual_alias_maps", "hash:" + postfixVirtualPath},
		{"virtual_transport", "virtual"},
		{"virtual_uid_maps", "static:" + vmailUID},
		{"virtual_gid_maps", "static:" + vmailGID},
		{"virtual_minimum_uid", "100"},
	}
	for _, s := range settings {
		if out, err := exec.Command("postconf", "-e", s[0]+"="+s[1]).CombinedOutput(); err != nil {
			return fmt.Errorf("postconf %s: %s", s[0], strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// configureDovecotVirtual drops a single override file that makes Dovecot
// authenticate against /etc/dovecot/users and read the maildirs — loaded last
// (99-) so it wins over the distro's default mail_location and system auth.
// configureDovecotVirtual, Dovecot'un /etc/dovecot/users'a karşı doğrulama
// yapıp maildir'leri okumasını sağlayan tek bir override dosyası koyar — en
// son (99-) yüklenir; böylece dağıtımın varsayılan mail_location ve sistem
// auth'una üstün gelir.
func configureDovecotVirtual() error {
	conf := fmt.Sprintf(`# Managed by CelikPanel — do not edit by hand / elle düzenlemeyin
mail_location = maildir:%s/%%d/%%n
mail_uid = %s
mail_gid = %s
first_valid_uid = %s
passdb {
  driver = passwd-file
  args = scheme=CRYPT username_format=%%u %s
}
userdb {
  driver = passwd-file
  args = username_format=%%u %s
  default_fields = uid=%s gid=%s home=%s/%%d/%%n
}
`, mailRootDir, vmailUID, vmailGID, vmailUID,
		dovecotUsersPath, dovecotUsersPath, vmailUID, vmailGID, mailRootDir)

	confDir := "/etc/dovecot/conf.d"
	if !fileExistsAgent(confDir) {
		return fmt.Errorf("dovecot is not installed")
	}
	// /etc/dovecot/users holds password hashes (a secret), so it must NOT be
	// world-readable — but dovecot's auth process runs as the dovecot user,
	// not in the celikpanel group, so root:celikpanel 0640 locks it out
	// ("Permission denied", temporary auth failure). Give it to the dovecot
	// group at 0640: dovecot reads, nobody else does.
	// /etc/dovecot/users parola hash'leri (sır) tutar, dünya-okunur OLMAMALI —
	// ama dovecot'un auth süreci dovecot kullanıcısı olarak çalışır, celikpanel
	// grubunda değildir; root:celikpanel 0640 onu dışarıda bırakır (izin yok,
	// geçici auth hatası). dovecot grubuna 0640 ver: dovecot okur, başkası okumaz.
	if err := os.WriteFile(dovecotUsersPath, mustExistBytes(dovecotUsersPath), 0o640); err != nil {
		return err
	}
	if g, err := user.LookupGroup("dovecot"); err == nil {
		if gid, err := strconv.Atoi(g.Gid); err == nil {
			_ = os.Chown(dovecotUsersPath, 0, gid)
		}
	}
	_ = os.Chmod(dovecotUsersPath, 0o640)
	if err := os.WriteFile(confDir+"/99-celikpanel.conf", []byte(conf), 0o644); err != nil {
		return err
	}

	// Disable system-user auth. This is a virtual-mailbox server: mail users
	// are user@domain rows in our passwd-file, never Linux accounts. With
	// auth-system still enabled its userdb runs first and, finding no such
	// system user, breaks the lookup chain before our passwd-file userdb is
	// tried ("User doesn't exist" despite a valid password). Commenting the
	// include out is idempotent — re-running leaves an already-disabled line
	// untouched.
	// Sistem-kullanıcı auth'unu kapat. Bu sanal-posta-kutusu sunucusu: mail
	// kullanıcıları passwd-file'daki user@domain satırlarıdır, asla Linux
	// hesabı değil. auth-system açıkken userdb'si önce çalışır ve böyle bir
	// sistem kullanıcısı bulamayınca, bizim passwd-file userdb'miz denenmeden
	// lookup zincirini kırar (parola geçerliyken "User doesn't exist").
	// include'u yorumlamak idempotenttir.
	disableDovecotSystemAuth(confDir + "/10-auth.conf")
	return nil
}

// disableDovecotSystemAuth comments out the auth-system include so only our
// virtual passwd-file provides users. No-op if already commented or absent.
// disableDovecotSystemAuth, auth-system include'unu yorumlar; böylece
// kullanıcıları yalnız bizim sanal passwd-file'ımız sağlar.
func disableDovecotSystemAuth(authConf string) {
	data, err := os.ReadFile(authConf)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, line := range lines {
		if strings.TrimSpace(line) == "!include auth-system.conf.ext" {
			lines[i] = "#!include auth-system.conf.ext  # disabled by CelikPanel (virtual mailboxes)"
			changed = true
		}
	}
	if changed {
		_ = os.WriteFile(authConf, []byte(strings.Join(lines, "\n")), 0o644)
	}
}

// mustExistBytes returns a file's contents, or empty when absent — used to
// create /etc/dovecot/users with the right mode without clobbering it.
// mustExistBytes, bir dosyanın içeriğini döndürür, yoksa boş — /etc/dovecot/
// users'ı üzerine yazmadan doğru kiple oluşturmak için.
func mustExistBytes(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		return []byte("")
	}
	return b
}

func fileExistsAgent(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ensurePostfixDomain adds a domain to the virtual-mailbox-domains map if it
// is not already there, then rebuilds the map. Idempotent — adding the same
// domain twice is a no-op. Skipped for a dev agent (no real postfix).
// ensurePostfixDomain, bir domain'i sanal-posta-kutusu-domain'leri haritasına
// zaten yoksa ekler, sonra haritayı yeniden kurar. Idempotenttir.
func ensurePostfixDomain(domain string) {
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" || domain == "" {
		return
	}
	existing, _ := os.ReadFile(postfixDomainsPath)
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.Fields(line) != nil && strings.HasPrefix(strings.TrimSpace(line), domain+" ") {
			return // already present / zaten var
		}
	}
	// The value column is a dummy ("OK"); postfix only checks the key exists.
	// Değer sütunu yer tutucudur ("OK"); postfix yalnız anahtarın varlığına bakar.
	_ = appendToFile(postfixDomainsPath, domain+" OK")
	postmapReadable(postfixDomainsPath)
}

// postmapReadable rebuilds a postfix map and makes both the source and the
// generated .db world-readable. The root agent runs under UMask=0027, so
// without this the .db lands 0640 root:celikpanel and the postfix delivery
// process (not in the celikpanel group) gets "Permission denied" — mail
// defers as a "mail system configuration error". These maps hold
// address→path aliases, not secrets, so 0644 is correct.
// postmapReadable, bir postfix haritasını yeniden kurar ve hem kaynağı hem
// üretilen .db'yi dünya-okunur yapar. Root agent UMask=0027 ile çalışır;
// bu olmadan .db 0640 root:celikpanel olur ve postfix teslim süreci
// (celikpanel grubunda değil) "Permission denied" alır — posta "mail system
// configuration error" ile ertelenir. Bu haritalar sır değil adres→yol
// takma adı taşır; 0644 doğrudur.
func postmapReadable(path string) {
	_ = exec.Command("postmap", path).Run()
	_ = os.Chmod(path, 0o644)
	_ = os.Chmod(path+".db", 0o644)
}
