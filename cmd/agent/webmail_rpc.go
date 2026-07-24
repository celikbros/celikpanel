package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/alicelik/celikpanel/internal/services"
)

// Roundcube is installed from its OWN official tarball, not a distro package
// (operator, 24 Jul: "if it can't be installed on both distros, don't — a
// webmail that runs on all Linux"). This is the Node.js pattern: a pinned
// version, a pinned SHA-256, an HTTPS download from GitHub verified before
// anything is unpacked. ONE path on every Linux — the distro-specific package
// (apt roundcube / pacman roundcubemail, different layouts) is exactly the
// D-004 trap this avoids. Bumping the version is a two-line change here plus a
// live re-hash, the same discipline as Node.
//
// Roundcube dağıtım paketinden değil KENDİ resmi tarball'ından kurulur
// (operatör, 24 Tem: "her iki sürümde kurulamıyorsa kurma — tüm Linux'ta
// çalışan webmail"). Bu Node.js desenidir: sabitlenmiş sürüm, sabitlenmiş
// SHA-256, açılmadan önce doğrulanan HTTPS indirmesi. Her Linux'ta TEK yol —
// dağıtıma özgü paket (apt roundcube / pacman roundcubemail, farklı yerleşim)
// tam da bunun kaçındığı D-004 tuzağıdır. Sürüm yükseltmek burada iki satır +
// canlı yeniden-hash; Node ile aynı disiplin.
const (
	roundcubeVersion = "1.6.15"
	roundcubeSHA256  = "48c9f212c77460132491f670abaf440b765c8276268349a690913764d26afbef"
)

// webmailBaseDir is where the verified tarball is unpacked — a fixed tree we
// own, like the Node runtimes dir. Env-overridable for dev boxes.
// webmailBaseDir, doğrulanmış tarball'ın açıldığı yerdir — sahip olduğumuz
// sabit bir ağaç, Node runtime dizini gibi. Dev makineleri için env ile
// geçersiz kılınabilir.
var webmailBaseDir = func() string {
	if d := os.Getenv("CELIKPANEL_WEBMAIL_DIR"); d != "" {
		return d
	}
	return "/opt/celikpanel/webmail"
}()

func roundcubeInstalled() bool {
	return fileExistsAgent(filepath.Join(webmailBaseDir, "public_html", "index.php"))
}

type InstallRoundcubeResponse struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Error     string `json:"error,omitempty"`
}

// InstallRoundcube downloads, verifies and configures Roundcube. Idempotent:
// an existing install returns success. On any failure the staging tree is
// discarded so a half-install is never served.
// InstallRoundcube, Roundcube'u indirir, doğrular ve yapılandırır.
// Idempotent: mevcut kurulum başarı döner. Herhangi bir hatada hazırlık ağacı
// atılır; yarım kurulum asla sunulmaz.
func (a *Agent) InstallRoundcube(_ *struct{}, resp *InstallRoundcubeResponse) error {
	resp.Version = roundcubeVersion
	if roundcubeInstalled() {
		resp.Installed = true
		return nil
	}
	phpVer := services.DetectInstalledPHPVersion()
	if phpVer == "" {
		resp.Error = "PHP-FPM is not installed"
		return nil
	}

	url := fmt.Sprintf("https://github.com/roundcube/roundcubemail/releases/download/%s/roundcubemail-%s-complete.tar.gz",
		roundcubeVersion, roundcubeVersion)
	client := &http.Client{Timeout: 5 * time.Minute}

	if err := os.MkdirAll(filepath.Dir(webmailBaseDir), 0o755); err != nil {
		resp.Error = err.Error()
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(webmailBaseDir), "rc-dl-*")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	dl, err := client.Get(url)
	if err != nil {
		resp.Error = fmt.Sprintf("download failed: %v", err)
		return nil
	}
	defer dl.Body.Close()
	if dl.StatusCode != http.StatusOK {
		resp.Error = fmt.Sprintf("download failed: HTTP %d", dl.StatusCode)
		return nil
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), dl.Body); err != nil {
		resp.Error = fmt.Sprintf("download failed: %v", err)
		return nil
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != roundcubeSHA256 {
		resp.Error = "checksum mismatch — download discarded"
		return nil
	}

	// Extract to a staging dir, then move into place — a half-extracted tree
	// must never look installed (Node pattern).
	// Hazırlık dizinine aç, sonra yerine taşı — yarım açılmış ağaç asla kurulu
	// görünmemeli (Node deseni).
	stage, err := os.MkdirTemp(filepath.Dir(webmailBaseDir), "rc-stage-*")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer os.RemoveAll(stage)
	if out, err := exec.Command("tar", "-xzf", tmp.Name(), "-C", stage, "--strip-components=1").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("extract failed: %v: %s", err, string(out))
		return nil
	}

	if err := writeRoundcubeConfig(stage, phpVer); err != nil {
		resp.Error = err.Error()
		return nil
	}
	// Initialize the SQLite schema via Roundcube's own initdb (reads the config
	// we just wrote). PHP CLI only — no sqlite3 binary needed, so it works on
	// any distro.
	// SQLite şemasını Roundcube'un kendi initdb'siyle kur (az önce yazdığımız
	// config'i okur). Yalnız PHP CLI — sqlite3 binary gerekmez, her dağıtımda
	// çalışır.
	if out, err := exec.Command("php", filepath.Join(stage, "bin", "initdb.sh"), "--dir", filepath.Join(stage, "SQL")).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("db init failed: %v: %s", err, firstLine(string(out)))
		return nil
	}

	if roundcubeInstalled() {
		_ = os.RemoveAll(webmailBaseDir + ".old")
		_ = os.Rename(webmailBaseDir, webmailBaseDir+".old")
	}
	if err := os.Rename(stage, webmailBaseDir); err != nil {
		resp.Error = err.Error()
		return nil
	}
	_ = os.RemoveAll(webmailBaseDir + ".old")

	// The FPM pool that serves /webmail/ (the default www pool) must be able to
	// read the tree and WRITE the sqlite db + temp/logs. Give the whole tree to
	// the web group and make exactly those three writable — never world.
	// /webmail/'i sunan FPM havuzu (varsayılan www havuzu) ağacı okuyabilmeli ve
	// sqlite db + temp/logs'a YAZABİLMELİ. Ağacın tümünü web grubuna ver ve tam
	// o üçünü yazılabilir yap — asla herkese değil.
	if grp := webServerGroup(); grp != "" {
		_ = exec.Command("chown", "-R", "root:"+grp, webmailBaseDir).Run()
		for _, p := range []string{"temp", "logs"} {
			_ = os.Chmod(filepath.Join(webmailBaseDir, p), 0o775)
		}
		for _, f := range []string{"roundcube.sqlite3"} {
			_ = os.Chmod(filepath.Join(webmailBaseDir, f), 0o664)
		}
	}

	resp.Installed = true
	return nil
}

// writeRoundcubeConfig writes a minimal working config: a random des_key, an
// on-disk SQLite store (no database server to provision — the whole point of
// picking SQLite for a per-server webmail), and localhost IMAP/SMTP so it
// talks to the Dovecot/Postfix this panel installs.
// writeRoundcubeConfig, çalışan asgari bir config yazar: rastgele des_key,
// diskte SQLite deposu (kurulacak veritabanı sunucusu yok — sunucu-başı
// webmail için SQLite seçmenin bütün amacı) ve bu panelin kurduğu
// Dovecot/Postfix'e konuşması için localhost IMAP/SMTP.
func writeRoundcubeConfig(root, phpVer string) error {
	key := make([]byte, 18)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("des_key: %v", err)
	}
	dbPath := filepath.Join(webmailBaseDir, "roundcube.sqlite3")
	conf := fmt.Sprintf(`<?php
// Managed by CelikPanel. Roundcube installed from the official tarball (D-004:
// one path on every Linux). SQLite store, localhost IMAP/SMTP.
$config = [];
$config['db_dsnw'] = 'sqlite:///%s?mode=0664';
$config['imap_host'] = 'localhost:143';
$config['smtp_host'] = 'localhost:587';
$config['smtp_user'] = '%%u';
$config['smtp_pass'] = '%%p';
$config['support_url'] = '';
$config['des_key'] = '%s';
$config['product_name'] = 'CelikPanel Webmail';
$config['plugins'] = ['archive', 'zipdownload'];
$config['skin'] = 'elastic';
$config['enable_installer'] = false;
`, dbPath, hex.EncodeToString(key))
	// The staging tree's config path (moved into place with the tree).
	// Hazırlık ağacının config yolu (ağaçla birlikte yerine taşınır).
	return os.WriteFile(filepath.Join(root, "config", "config.inc.php"), []byte(conf), 0o640)
}

type RemoveRoundcubeResponse struct {
	Removed bool   `json:"removed"`
	Error   string `json:"error,omitempty"`
}

// RemoveRoundcube deletes the whole webmail tree — the fixed base dir means
// there is nothing else it could delete. Idempotent.
// RemoveRoundcube tüm webmail ağacını siler — sabit taban dizini, silebileceği
// başka bir şey olmadığı anlamına gelir. Idempotenttir.
func (a *Agent) RemoveRoundcube(_ *struct{}, resp *RemoveRoundcubeResponse) error {
	if err := os.RemoveAll(webmailBaseDir); err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Removed = true
	return nil
}

// Serving Roundcube webmail. Unlike the database tools (admin-only, behind a
// panel session), webmail is for END USERS: a customer with only a mailbox —
// no panel account — signs in with their email credentials. So the panel
// fronts it at a PUBLIC /webmail/ path (Roundcube's own login is the auth),
// and the agent serves it loopback-only on 127.0.0.1:8307. The split from the
// db-tools server (8306) is deliberate: different audience, different access
// rule, so a change to one can never widen the other.
//
// Roundcube webmail sunumu. Veritabanı araçlarının (yalnız-admin, panel
// oturumu arkasında) aksine webmail SON KULLANICI içindir: yalnız posta
// kutusu olan bir müşteri — panel hesabı olmadan — e-posta bilgileriyle girer.
// Bu yüzden panel onu PUBLIC bir /webmail/ yolunda önler (kimlik doğrulama
// Roundcube'un kendi girişidir) ve agent onu yalnız-loopback 127.0.0.1:8307'de
// sunar. db-araçları sunucusundan (8306) ayrı olması bilinçlidir: farklı
// kitle, farklı erişim kuralı; birine yapılan değişiklik diğerini asla
// genişletemez.

const (
	webmailConfPath = "/etc/nginx/conf.d/celikpanel-webmail.conf"
	webmailAddr     = "127.0.0.1:8307"
)

// roundcubeRoot is the public entry point inside our own tarball tree. The
// complete tarball's public_html holds index.php with skins/plugins symlinked
// up one level and program/ real; nginx follows the symlinks, so this single
// root serves the whole app.
// roundcubeRoot, kendi tarball ağacımızın içindeki genel giriş noktasıdır.
// Complete tarball'ın public_html'i index.php'yi tutar; skins/plugins bir üst
// düzeye symlink, program/ gerçek dizindir; nginx symlink'leri izler, bu tek
// kök tüm uygulamayı sunar.
func roundcubeRootDir() string { return filepath.Join(webmailBaseDir, "public_html") }

type ConfigureWebmailResponse struct {
	Configured bool   `json:"configured"`
	Present    bool   `json:"present"`
	Error      string `json:"error,omitempty"`
}

// ConfigureWebmail (re)writes the loopback nginx server when Roundcube is
// installed, and removes it when it is not — the mirror of ConfigureDBTools,
// called by the panel after roundcube is installed or uninstalled. Idempotent.
// ConfigureWebmail, Roundcube kuruluyken loopback nginx sunucusunu (yeniden)
// yazar, değilken kaldırır — ConfigureDBTools'un aynası; panel roundcube
// kurulunca/kaldırılınca çağırır. Idempotenttir.
func (a *Agent) ConfigureWebmail(_ *struct{}, resp *ConfigureWebmailResponse) error {
	if _, err := exec.LookPath("nginx"); err != nil {
		resp.Error = "nginx is not installed"
		return nil
	}

	// Presence is the entry point's existence — InstallRoundcube renames the
	// tree into place only after a successful build, so a half-install is
	// never served.
	// Varlık, giriş noktasının varlığıdır — InstallRoundcube ağacı yalnız
	// başarılı yapımdan sonra yerine taşır; yarım kurulum asla sunulmaz.
	if !fileExistsAgent(filepath.Join(roundcubeRootDir(), "index.php")) {
		_ = os.Remove(webmailConfPath)
		_ = exec.Command("systemctl", "reload", "nginx").Run()
		resp.Configured = true
		resp.Present = false
		return nil
	}
	resp.Present = true

	// Roundcube is PHP: hand .php to the installed PHP version's default FPM
	// pool. The path is version-dependent, so detect — never assume.
	// Roundcube PHP'dir: .php'yi kurulu PHP sürümünün varsayılan FPM havuzuna
	// ver. Yol sürüme bağlıdır; tespit et — asla varsayma.
	phpVer := services.DetectInstalledPHPVersion()
	if phpVer == "" {
		resp.Error = "PHP-FPM is not installed"
		return nil
	}
	socket := fmt.Sprintf("/run/php/php%s-fpm.sock", phpVer)

	// Path-preserving: the browser path (/webmail/…) and this server's path
	// are identical, so Roundcube's own absolute URLs survive the panel proxy
	// without rewriting — the same trick the db-tools server uses.
	// Yol-koruyan: tarayıcı yolu (/webmail/…) ile bu sunucunun yolu aynıdır;
	// böylece Roundcube'un mutlak URL'leri panel vekilinden yeniden yazım
	// olmadan sağ çıkar — db-araçları sunucusunun kullandığı aynı numara.
	conf := fmt.Sprintf(`# Managed by CelikPanel — Roundcube webmail, loopback only.
# The panel reverse-proxies /webmail/ here; Roundcube's own login is the auth.
server {
    listen %s;
    server_name _;
    client_max_body_size 25m;

    location /webmail/ {
        alias %s/;
        index index.php;
        try_files $uri $uri/ /webmail/index.php$is_args$args;

        location ~ ^/webmail/(.+\.php)$ {
            alias %s/$1;
            include fastcgi_params;
            fastcgi_param SCRIPT_FILENAME $request_filename;
            fastcgi_pass unix:%s;
        }
    }
}
`, webmailAddr, roundcubeRootDir(), roundcubeRootDir(), socket)

	if err := os.WriteFile(webmailConfPath, []byte(conf), 0o644); err != nil {
		resp.Error = fmt.Sprintf("write nginx config: %v", err)
		return nil
	}
	// Validate before reload — a broken webmail config must not take the web
	// server (and every customer site) down with it.
	// Yeniden yüklemeden önce doğrula — bozuk bir webmail yapılandırması web
	// sunucusunu (ve tüm müşteri sitelerini) beraberinde düşürmemeli.
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		_ = os.Remove(webmailConfPath)
		resp.Error = fmt.Sprintf("nginx rejected the webmail config: %s", firstLine(string(out)))
		return nil
	}
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("nginx reload failed: %s", firstLine(string(out)))
		return nil
	}

	resp.Configured = true
	return nil
}
