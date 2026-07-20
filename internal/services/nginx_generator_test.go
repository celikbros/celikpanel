package services

import (
	"strings"
	"testing"
)

// The vhost template is the panel's security boundary for anything sitting in
// a document root, so these are not style assertions — each one is a leak that
// reached production once (see AUTOPSY A6) or was found on the way there.
//
// vhost şablonu, doküman kökündeki her şey için panelin güvenlik sınırıdır;
// bu yüzden buradakiler biçim iddiası değildir — her biri ya üretime ulaşmış
// (bkz. AUTOPSY A6) ya da oraya giderken yakalanmış bir sızıntıdır.
func TestVhostServingRules(t *testing.T) {
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}

	render := func(t *testing.T, d VhostData) string {
		t.Helper()
		out, err := ng.Render(d)
		if err != nil {
			t.Fatalf("render %s: %v", d.ProjectType, err)
		}
		return out
	}

	static := render(t, VhostData{SiteID: 1, Domain: "ornek.com", DocumentRoot: "/var/www/ornek", ProjectType: "static"})
	php := render(t, VhostData{SiteID: 2, Domain: "ornek.com", DocumentRoot: "/var/www/ornek", ProjectType: "php", PHPSocket: "/run/php/php8.3-fpm.sock"})

	// A static site has no PHP handler. Without an explicit deny, nginx hands
	// the file out as application/octet-stream — the browser downloads the
	// SOURCE, credentials included. Verified against live nginx: 403, and the
	// body is nginx's error page, not the file.
	// Statik sitenin PHP işleyicisi yoktur. Açık bir ret olmadan nginx dosyayı
	// application/octet-stream olarak verir — tarayıcı KAYNAĞI indirir, içindeki
	// bilgilerle birlikte. Canlı nginx'te doğrulandı: 403 ve gövde dosya değil,
	// nginx'in hata sayfası.
	if !strings.Contains(static, `location ~ \.(php|phtml|phps|php[0-9])$`) {
		t.Error("static vhost does not deny PHP files — the source would be served as a download")
	}
	// The PHP vhost must NOT carry that deny: there the files are meant to run.
	// PHP vhost'u o reddi TAŞIMAMALI: orada dosyaların çalışması beklenir.
	if strings.Contains(php, `location ~ \.(php|phtml|phps|php[0-9])$`) {
		t.Error("php vhost denies PHP files — the site could never run")
	}
	if !strings.Contains(php, "fastcgi_pass unix:/run/php/php8.3-fpm.sock") {
		t.Error("php vhost does not pass to the FPM socket")
	}

	// A6, both halves: dotfiles are denied so .env/.git stay secret, but
	// .well-known is exempt or certbot --webroot can never validate — that
	// broke every issuance and renewal for static sites.
	// A6'nın iki yarısı: .env/.git gizli kalsın diye nokta dosyalar reddedilir,
	// ama .well-known muaftır; yoksa certbot --webroot hiç doğrulayamaz — bu,
	// statik sitelerin her sertifika alma ve yenilemesini kırmıştı.
	for name, conf := range map[string]string{"static": static, "php": php} {
		if !strings.Contains(conf, `location ~ /\.(?!well-known).*`) {
			t.Errorf("%s vhost: dotfile rule missing or not ACME-safe", name)
		}
	}
}

// A php vhost with no FPM socket renders `unix:` with no path and nginx
// refuses to load it — taking the whole web server down on reload, not just
// this site. Refusing to render is the honest failure.
// FPM soketi olmayan bir php vhost'u yolsuz `unix:` üretir ve nginx onu
// yüklemeyi reddeder — reload'da yalnız bu siteyi değil, tüm web sunucusunu
// düşürür. Üretmeyi reddetmek dürüst olan başarısızlıktır.
func TestPHPVhostRefusesWithoutSocket(t *testing.T) {
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ng.Render(VhostData{SiteID: 1, Domain: "ornek.com", ProjectType: "php"}); err == nil {
		t.Error("rendered a php vhost with no FPM socket — nginx would reject the config")
	}
}
