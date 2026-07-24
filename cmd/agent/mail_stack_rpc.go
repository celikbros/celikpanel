package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
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
	// Dovecot alone is a legitimate state: an operator may install IMAP first
	// (or only IMAP, delivering mail from elsewhere). Configure whichever half
	// is present instead of refusing outright — refusing left a freshly
	// installed Dovecot completely unconfigured, i.e. "installed" but broken.
	// Yalnız Dovecot meşru bir durumdur: operatör önce IMAP kurabilir (ya da
	// yalnız IMAP; postayı başka yerden teslim edebilir). Toptan reddetmek
	// yerine hangi yarı varsa onu yapılandır — reddetmek, yeni kurulmuş bir
	// Dovecot'u tamamen yapılandırılmamış bırakıyordu: "kurulu" ama bozuk.
	hasPostfix := true
	if _, err := exec.LookPath("postconf"); err != nil {
		hasPostfix = false
	}
	hasDovecot := fileExistsAgent("/etc/dovecot")
	if !hasPostfix && !hasDovecot {
		resp.Error = "no mail server is installed"
		return nil
	}

	if err := ensureVmailUser(); err != nil {
		resp.Error = fmt.Sprintf("vmail user: %v", err)
		return nil
	}

	if hasPostfix {
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
		ensureAliasDatabase()
	}

	if hasDovecot {
		if err := ensureDovecotSSLCert(); err != nil {
			resp.Error = fmt.Sprintf("dovecot tls: %v", err)
			return nil
		}
		if err := configureDovecotVirtual(); err != nil {
			resp.Error = fmt.Sprintf("dovecot: %v", err)
			return nil
		}
	}

	if hasPostfix {
		// Wire whatever filters are installed INTO Postfix. Without this a
		// spam filter runs beside the mail server instead of inside it —
		// installed, "Running", filtering nothing (operator, 24 Jul).
		// Kurulu olan filtreleri Postfix'in İÇİNE bağla. Bu olmadan spam
		// filtresi posta sunucusunun yanında koşar, içinde değil — kurulu,
		// "Çalışıyor", hiçbir şey süzmüyor (operatör, 24 Tem).
		if err := applyMilterChain(); err != nil {
			resp.Error = fmt.Sprintf("milter wiring: %v", err)
			return nil
		}
		if out, err := exec.Command("systemctl", "reload-or-restart", "postfix").CombinedOutput(); err != nil {
			resp.Error = fmt.Sprintf("postfix reload: %s", strings.TrimSpace(string(out)))
			return nil
		}
	}
	if hasDovecot {
		if out, err := exec.Command("systemctl", "restart", "dovecot").CombinedOutput(); err != nil {
			resp.Error = fmt.Sprintf("dovecot restart: %s", strings.TrimSpace(string(out)))
			return nil
		}
	}

	resp.Configured = true
	switch {
	case hasPostfix && hasDovecot:
		resp.Detail = "postfix + dovecot configured for virtual mailboxes"
	case hasPostfix:
		resp.Detail = "postfix configured for virtual mailboxes"
	default:
		resp.Detail = "dovecot configured for virtual mailboxes"
	}
	return nil
}

// postfixDomainsPath lists the virtual mailbox domains (postfix needs this
// separately from the mailbox map).
var postfixDomainsPath = "/etc/postfix/vmailbox_domains"

// WireMailFilters re-composes Postfix's milter chain from whatever filters are
// installed right now. It is called after a spam filter is installed OR
// removed: installing one must start filtering, and removing one must stop
// Postfix pointing at a socket that no longer answers.
//
// WireMailFilters, Postfix'in milter zincirini şu an kurulu olan filtrelerden
// yeniden bestelemektedir. Bir spam filtresi kurulduktan VEYA kaldırıldıktan
// sonra çağrılır: kurmak süzmeyi başlatmalı, kaldırmak da Postfix'i artık
// cevap vermeyen bir sokete bakar hâlde bırakmamalıdır.
type WireMailFiltersResponse struct {
	Wired  bool   `json:"wired"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (a *Agent) WireMailFilters(_ *struct{}, resp *WireMailFiltersResponse) error {
	// Repair the lookup tables while we are here. A server configured before
	// the table type was discovered still carries `hash:` in main.cf, and on a
	// postfix built without Berkeley DB that means every incoming message is
	// rejected with 451. Re-applying is idempotent, costs one postconf pass,
	// and turns the upgrade itself into the repair — no daemon is restarted.
	// Buradayken arama tablolarını da onar. Tablo tipi keşfedilmeden önce
	// yapılandırılmış bir sunucu main.cf'inde hâlâ `hash:` taşır ve Berkeley
	// DB'siz derlenmiş bir postfix'te bu, gelen her iletinin 451 ile
	// reddedilmesi demektir. Yeniden uygulamak değişmez etkilidir, bir postconf
	// turu tutar ve yükseltmenin kendisini onarıma çevirir — daemon yeniden
	// başlatılmaz.
	if _, err := exec.LookPath("postconf"); err == nil && fileExistsAgent(postfixVBoxPath) {
		if err := configurePostfixVirtual(); err != nil {
			resp.Error = err.Error()
			return nil
		}
		for _, p := range []string{postfixVBoxPath, postfixVirtualPath, postfixDomainsPath} {
			postmapReadable(p)
		}
		ensureAliasDatabase()
	}
	if err := applyMilterChain(); err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Wired = true
	resp.Detail = fmt.Sprintf("milters=%q maps=%s", postconfValue("smtpd_milters"), postfixMapType())
	return nil
}

// rspamdMilter is rspamd_proxy in milter mode — a TCP port, identical on every
// distro, so there is nothing to discover.
// rspamdMilter, milter kipindeki rspamd_proxy'dir — her dağıtımda aynı olan bir
// TCP portu; keşfedilecek bir şey yok.
const rspamdMilter = "inet:localhost:11332"

// spamassMilterEndpoint finds where spamass-milter actually listens, instead of
// hardcoding a path. Two facts make a constant wrong here:
//   - the socket path is a packaging choice (Debian sets it in
//     /etc/default/spamass-milter; an operator may move it), and
//   - Postfix runs CHROOTED in its queue directory, so a socket living under
//     /var/spool/postfix must be named relative to that root or postfix simply
//     cannot see it.
//
// Guessing a socket path is the exact mistake that made webmail's PHP-FPM
// socket wrong on Arch. When the path cannot be determined we return "" and the
// caller skips the filter — a missing milter line is honest; a wrong one is a
// mail server talking to nothing.
//
// spamassMilterEndpoint, bir yolu koda gömmek yerine spamass-milter'ın gerçekte
// nerede dinlediğini bulur. Sabit kullanmayı iki olgu yanlışlar:
//   - soket yolu bir paketleme tercihidir (Debian bunu
//     /etc/default/spamass-milter'da belirler; operatör taşıyabilir) ve
//   - Postfix kuyruk dizininde CHROOT'lu koşar; /var/spool/postfix altındaki bir
//     soket, o köke GÖRE adlandırılmalıdır yoksa postfix onu göremez.
//
// Soket yolunu tahmin etmek, webmail'in PHP-FPM soketini Arch'ta yanlışlayan
// hatanın ta kendisidir. Yol saptanamazsa "" döneriz ve çağıran filtreyi atlar —
// eksik bir milter satırı dürüsttür; yanlış olanı, hiçliğe konuşan bir posta
// sunucusudur.
func spamassMilterEndpoint() string {
	sock := ""
	// The unit's own command line is the most truthful source: -p <socket>.
	// Unit'in kendi komut satırı en doğru kaynaktır: -p <soket>.
	if out, err := exec.Command("systemctl", "show", "spamass-milter.service", "-p", "ExecStart", "--value").Output(); err == nil {
		if m := regexp.MustCompile(`-p\s+(\S+)`).FindStringSubmatch(string(out)); len(m) == 2 {
			sock = m[1]
		}
	}
	if sock == "" {
		// Debian keeps it in the defaults file the unit sources.
		// Debian bunu, unit'in okuduğu defaults dosyasında tutar.
		if b, err := os.ReadFile("/etc/default/spamass-milter"); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "SOCKET=") {
					continue
				}
				if v := strings.Trim(strings.TrimPrefix(line, "SOCKET="), "\"' "); v != "" {
					sock = v
				}
			}
		}
	}
	if sock == "" || !strings.HasPrefix(sock, "/") {
		return ""
	}
	// Inside the chroot the path loses the queue directory prefix.
	// Chroot içinde yol, kuyruk dizini önekini kaybeder.
	if queue := strings.TrimSpace(postconfValue("queue_directory")); queue != "" {
		if rel := strings.TrimPrefix(sock, strings.TrimSuffix(queue, "/")); rel != sock && strings.HasPrefix(rel, "/") {
			return "unix:" + rel
		}
	}
	return "unix:" + sock
}

// postconfValue reads one Postfix setting; "" when postfix is absent.
// postconfValue tek bir Postfix ayarını okur; postfix yoksa "".
func postconfValue(key string) string {
	out, err := exec.Command("postconf", "-h", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// postconfExpanded reads a setting with $variable references resolved.
// postconfExpanded, bir ayarı $değişken başvuruları çözülmüş olarak okur.
func postconfExpanded(key string) string {
	out, err := exec.Command("postconf", "-x", "-h", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// applyMilterChain is the SINGLE owner of Postfix's milter settings. Every
// filter the panel installs (DKIM signing, a spam filter) is a milter, and
// they must COMPOSE: before this existed the DKIM step wrote
// `smtpd_milters = <opendkim>` outright, so any second filter would have
// silently replaced DKIM signing or been replaced by it — last writer wins,
// no error, mail quietly unsigned or unfiltered.
//
// It also answers the operator's finding (24 Jul): Rspamd was installed and
// "Running" while `smtpd_milters` was EMPTY — the daemon was up and not one
// message passed through it. A filter the panel installed must actually
// filter, the same rule as every other component.
//
// applyMilterChain, Postfix'in milter ayarlarının TEK sahibidir. Panelin
// kurduğu her filtre (DKIM imzalama, spam filtresi) bir milter'dır ve
// BİRLEŞMELİdirler: bu var olmadan önce DKIM adımı doğrudan
// `smtpd_milters = <opendkim>` yazıyordu; ikinci bir filtre DKIM imzalamayı
// sessizce siler ya da onun tarafından silinirdi — son yazan kazanır, hata
// yok, posta sessizce imzasız ya da süzgeçsiz.
//
// Ayrıca operatörün bulgusunu (24 Tem) yanıtlar: Rspamd kurulu ve
// "Çalışıyor"ken `smtpd_milters` BOŞTU — daemon ayaktaydı ve içinden tek bir
// ileti geçmiyordu. Panelin kurduğu filtre gerçekten süzmelidir; her bileşen
// için geçerli olan aynı kural.
func applyMilterChain() error {
	if _, err := exec.LookPath("postconf"); err != nil {
		return nil // no postfix here, nothing to wire
	}

	spam := ""
	switch {
	case unitExists("rspamd"):
		spam = rspamdMilter
	case unitExists("spamass-milter"):
		spam = spamassMilterEndpoint()
	}
	incoming, outgoing := composeMilterChain(unitExists("opendkim") && fileExistsAgent(opendkimConfPath), spam)

	settings := [][2]string{
		{"smtpd_milters", incoming},
		{"non_smtpd_milters", outgoing},
		// A dead filter must never eat mail — accept beats bounce.
		// Ölü filtre asla posta yutmamalı — kabul, reddi yener.
		{"milter_default_action", "accept"},
		{"milter_protocol", "6"},
	}
	for _, kv := range settings {
		if out, err := exec.Command("postconf", "-e", kv[0]+"="+kv[1]).CombinedOutput(); err != nil {
			return fmt.Errorf("postconf %s: %s", kv[0], strings.TrimSpace(string(out)))
		}
	}
	_ = exec.Command("systemctl", "reload-or-restart", "postfix").Run()
	return nil
}

// composeMilterChain decides the two Postfix milter lists from what is present.
// It is separated from the system calls so the ORDER and the two-list split —
// the parts that decide whether mail is signed and filtered — can be tested
// without a mail server.
//
// Rules:
//   - DKIM first: it signs what leaves, so it must see the final content.
//   - The spam filter judges only ARRIVING mail. Scanning our own outgoing
//     messages burns CPU and can bounce legitimate mail we created ourselves.
//   - An unknown spam endpoint contributes nothing rather than a broken line.
//
// composeMilterChain, iki Postfix milter listesini mevcut olandan karar verir.
// Sistem çağrılarından ayrıdır; böylece SIRA ve iki-liste ayrımı — postanın
// imzalanıp süzülüp süzülmeyeceğine karar veren parçalar — posta sunucusu
// olmadan test edilebilir.
//
// Kurallar:
//   - Önce DKIM: çıkanı imzalar, bu yüzden nihai içeriği görmelidir.
//   - Spam filtresi yalnız GELEN postayı yargılar. Kendi çıkan iletilerimizi
//     taramak CPU yakar ve kendi ürettiğimiz meşru postayı geri çevirebilir.
//   - Bilinmeyen bir spam ucu, bozuk bir satır yerine hiçbir şey katmaz.
func composeMilterChain(hasDKIM bool, spamEndpoint string) (incoming, outgoing string) {
	in := []string{}
	out := []string{}
	if hasDKIM {
		in = append(in, opendkimMilter)
		// Locally submitted mail must be signed too.
		// Yerel gönderilen posta da imzalanmalı.
		out = append(out, opendkimMilter)
	}
	if spamEndpoint != "" {
		in = append(in, spamEndpoint)
	}
	return strings.Join(in, ", "), strings.Join(out, ", ")
}

// ensureDovecotSSLCert creates the TLS keypair Dovecot's shipped config points
// at, when the distro did not. Debian's dovecot-core generates one on install;
// Arch does not, so its stock dovecot.conf references
// /etc/dovecot/ssl-{cert,key}.pem and the daemon REFUSES TO START without them
// — caught live on Frankfurt (24 Jul): Dovecot installed, enabled, and dead
// with "cert_file: open(...) failed". A component the panel installed must
// actually run, so the panel provides what the distro left out.
//
// Self-signed is the honest default: it makes IMAPS work today, and a real
// certificate arrives with the domain's own (the panel already issues those).
// Existing files are never touched — a real cert installed later must survive
// any reconfigure.
//
// ensureDovecotSSLCert, Dovecot'un getirdiği yapılandırmanın işaret ettiği TLS
// anahtar çiftini, dağıtım üretmediyse üretir. Debian'ın dovecot-core'u
// kurulumda üretir; Arch üretmez, bu yüzden hazır dovecot.conf'u
// /etc/dovecot/ssl-{cert,key}.pem'e başvurur ve daemon onlarsız BAŞLAMAYI
// REDDEDER — Frankfurt'ta canlı yakalandı (24 Tem): Dovecot kurulu, etkin ve
// ölü, "cert_file: open(...) failed". Panelin kurduğu bir bileşen gerçekten
// çalışmalıdır; bu yüzden dağıtımın eksik bıraktığını panel tamamlar.
//
// Kendi-imzalı dürüst varsayılandır: IMAPS'i bugün çalışır kılar, gerçek
// sertifika domain'in kendisiyle gelir (panel onları zaten veriyor). Var olan
// dosyalara asla dokunulmaz — sonradan kurulan gerçek bir sertifika her
// yeniden yapılandırmadan sağ çıkmalıdır.
func ensureDovecotSSLCert() error {
	const (
		certPath = "/etc/dovecot/ssl-cert.pem"
		keyPath  = "/etc/dovecot/ssl-key.pem"
	)
	// Only act when the shipped config actually asks for these files; on a
	// distro that points elsewhere (or has its own pair) this is a no-op.
	// Yalnız getirilen yapılandırma bu dosyaları gerçekten istediğinde davran;
	// başka yeri işaret eden (ya da kendi çiftini taşıyan) dağıtımda işlem yok.
	conf, err := os.ReadFile("/etc/dovecot/dovecot.conf")
	if err != nil || !strings.Contains(string(conf), certPath) {
		return nil
	}
	if fileExistsAgent(certPath) && fileExistsAgent(keyPath) {
		return nil
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		return fmt.Errorf("openssl is required to create the mail TLS certificate")
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}
	out, err := exec.Command("openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048",
		"-days", "3650",
		"-subj", "/CN="+host,
		"-keyout", keyPath, "-out", certPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("openssl: %s", firstLine(string(out)))
	}
	// The private key is a secret: root-only. The certificate is public.
	// Özel anahtar bir sırdır: yalnız root. Sertifika geneldir.
	_ = os.Chmod(keyPath, 0o600)
	_ = os.Chmod(certPath, 0o644)
	return nil
}

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
	// The table type is DISCOVERED, never assumed — see postfixMapType.
	// Tablo tipi VARSAYILMAZ, keşfedilir — bkz. postfixMapType.
	mt := postfixMapType() + ":"
	settings := [][2]string{
		{"virtual_mailbox_base", mailRootDir},
		{"virtual_mailbox_domains", mt + postfixDomainsPath},
		{"virtual_mailbox_maps", mt + postfixVBoxPath},
		{"virtual_alias_maps", mt + postfixVirtualPath},
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
	// The dialect follows the installed Dovecot: 2.3 (mail_location) vs 2.4
	// (mail_driver/mail_path) — see dovecot_dialect.go for the why.
	// Lehçe kurulu Dovecot'u izler: 2.3 (mail_location) vs 2.4
	// (mail_driver/mail_path) — nedeni için dovecot_dialect.go.
	conf := buildDovecotVirtualConf(dovecotIs24())

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
	// Validate with dovecot's own parser before the service is restarted; a
	// dialect mistake must surface here as an error, not as a dead service.
	// Servis yeniden başlatılmadan önce dovecot'un kendi ayrıştırıcısıyla
	// doğrula; bir lehçe hatası ölü servis olarak değil burada hata olarak
	// yüzeye çıkmalı.
	if err := applyDovecotConf(confDir+"/99-celikpanel.conf", conf); err != nil {
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
	t := postfixMapType()
	if t == "texthash" {
		// texthash needs no index at all — postfix reads the plain file.
		// texthash hiç dizin istemez — postfix düz dosyayı okur.
		_ = os.Chmod(path, 0o644)
		return
	}
	_ = exec.Command("postmap", t+":"+path).Run()
	_ = os.Chmod(path, 0o644)
	for _, ext := range []string{".db", ".lmdb"} {
		_ = os.Chmod(path+ext, 0o644)
	}
}

// ensureAliasDatabase builds the local alias index when the distro shipped only
// the text file. Debian runs newaliases in its postinst; Arch does not, so
// postfix starts with `alias_maps = lmdb:/etc/postfix/aliases` and no
// aliases.lmdb — and every LOCAL recipient (root, cron reports, bounce
// notifications) is refused with `451 Temporary lookup failure`. Found live on
// Frankfurt (25 Jul) right after the virtual maps were fixed: the rejection
// simply moved from one missing index to the next.
//
// ensureAliasDatabase, dağıtım yalnız metin dosyasını getirdiyse yerel takma ad
// dizinini kurar. Debian postinst'inde newaliases koşturur, Arch koşturmaz;
// böylece postfix `alias_maps = lmdb:/etc/postfix/aliases` ile açılır ve
// aliases.lmdb yoktur — YEREL alıcıların hepsi (root, cron raporları, geri
// dönüş bildirimleri) `451 Temporary lookup failure` ile reddedilir. 25 Tem'de
// Frankfurt'ta, sanal haritalar düzeltilir düzeltilmez canlı bulundu: ret,
// eksik bir dizinden diğerine taşınmıştı.
func ensureAliasDatabase() {
	// -x expands $variables. Arch ships `alias_database = $alias_maps`
	// literally, and without expansion the value has no "type:path" to parse —
	// the repair silently did nothing on exactly the distro that needed it.
	// -x, $değişkenleri açar. Arch, `alias_database = $alias_maps`ı olduğu gibi
	// getirir; açılmadan değerde ayrıştırılacak "tip:yol" yoktur — onarım, tam
	// da ona ihtiyaç duyan dağıtımda sessizce hiçbir şey yapmıyordu.
	db := postconfExpanded("alias_database")
	if db == "" {
		db = postconfExpanded("alias_maps")
	}
	// alias_database is "type:path" and may list several, comma separated.
	// alias_database "tip:yol"dur ve virgülle birkaç tane sayabilir.
	for _, entry := range strings.Split(db, ",") {
		entry = strings.TrimSpace(entry)
		i := strings.Index(entry, ":")
		if i < 0 {
			continue
		}
		typ, path := entry[:i], entry[i+1:]
		if !fileExistsAgent(path) {
			continue // no source file: nothing this panel should invent
		}
		indexed := false
		for _, ext := range []string{".db", ".lmdb"} {
			if fileExistsAgent(path + ext) {
				indexed = true
			}
		}
		if indexed || typ == "texthash" {
			continue
		}
		_ = exec.Command("newaliases").Run()
		return
	}
}

// postfixMapType finds an indexed table type this postfix can ACTUALLY use.
//
// `hash:` was hardcoded, and it is not portable: Arch builds postfix without
// Berkeley DB, so every lookup failed at runtime with "Berkeley DB support for
// 'hash:...' is not available for this OS distribution" and postfix answered
// every incoming message with `451 Temporary lookup failure`. Installed,
// "Running", and rejecting all mail — found live on Frankfurt (25 Jul) by
// sending one test message through the freshly wired milter chain.
//
// `postconf -m` cannot be trusted alone: it lists `hash` on Arch too, because
// the type is COMPILED IN but its backend library is missing. The only honest
// test is to run postmap on a probe file and see whether it works, so that is
// what we do. Preference order puts lmdb first (Arch's real backend, and
// faster), then hash (Debian/Ubuntu), then btree. If none index, texthash is
// the universal floor: no index file, postfix reads the plain text.
//
// postfixMapType, bu postfix'in GERÇEKTEN kullanabildiği bir dizinli tablo
// tipini bulur.
//
// `hash:` koda gömülüydü ve taşınabilir değil: Arch, postfix'i Berkeley DB'siz
// derler; bu yüzden her arama çalışma anında "Berkeley DB support for
// 'hash:...' is not available for this OS distribution" ile düşüyor ve postfix
// gelen her iletiye `451 Temporary lookup failure` diyordu. Kurulu,
// "Çalışıyor" ve tüm postayı reddediyor — 25 Tem'de Frankfurt'ta, yeni
// bağlanan milter zincirinden tek bir test iletisi geçirilerek canlı bulundu.
//
// `postconf -m`'e tek başına güvenilemez: Arch'ta da `hash` listeler, çünkü tip
// DERLENMİŞTİR ama arka uç kütüphanesi yoktur. Tek dürüst sınav, bir deneme
// dosyasında postmap koşturup işe yarayıp yaramadığına bakmaktır; yaptığımız
// da budur. Tercih sırası lmdb'yi öne alır (Arch'ın gerçek arka ucu ve daha
// hızlı), sonra hash (Debian/Ubuntu), sonra btree. Hiçbiri dizinlemezse
// texthash evrensel tabandır: dizin dosyası yok, postfix düz metni okur.
func postfixMapType() string {
	for _, t := range []string{"lmdb", "hash", "btree"} {
		if postmapTypeWorks(t) {
			return t
		}
	}
	return "texthash"
}

func postmapTypeWorks(t string) bool {
	dir, err := os.MkdirTemp("", "cpmap")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	probe := dir + "/probe"
	if err := os.WriteFile(probe, []byte("probe@example.invalid OK\n"), 0o600); err != nil {
		return false
	}
	// A type that is compiled in but unusable fails HERE, which is the whole
	// point: we ask postmap, not the feature list.
	// Derlenmiş ama kullanılamaz bir tip BURADA düşer; bütün mesele bu:
	// özellik listesine değil, postmap'e soruyoruz.
	if err := exec.Command("postmap", t+":"+probe).Run(); err != nil {
		return false
	}
	// postmap can exit 0 and still write nothing usable; require the index.
	// postmap 0 ile çıkıp kullanılır bir şey yazmamış olabilir; dizini şart koş.
	for _, ext := range []string{".db", ".lmdb"} {
		if fileExistsAgent(probe + ext) {
			return true
		}
	}
	return false
}
