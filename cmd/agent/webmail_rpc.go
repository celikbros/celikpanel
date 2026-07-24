package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/alicelik/celikpanel/internal/services"
)

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
	// roundcubeRoot is where Debian's package puts the public entry point;
	// its skins/plugins are symlinks up one level, program/ is real. nginx
	// follows the symlinks, so this single root serves the whole app.
	// roundcubeRoot, Debian paketinin genel giriş noktasını koyduğu yerdir;
	// skins/plugins bir üst düzeye symlink, program/ gerçek dizindir. nginx
	// symlink'leri izler, bu tek kök tüm uygulamayı sunar.
	roundcubeRoot = "/var/lib/roundcube/public_html"
)

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

	// Presence is the entry point's existence — the Debian package writes it
	// only after a successful configure, so a half-install is not served.
	// Varlık, giriş noktasının varlığıdır — Debian paketi onu yalnız başarılı
	// yapılandırmadan sonra yazar; yani yarım kurulum sunulmaz.
	if !fileExistsAgent(roundcubeRoot + "/index.php") {
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
`, webmailAddr, roundcubeRoot, roundcubeRoot, socket)

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
