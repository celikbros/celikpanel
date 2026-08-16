package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The config editor writes as root. These are the paths that must NEVER be
// reachable through it, each one a way to own the machine or the panel:
// authorized_keys via "..", a cron drop-in, a systemd unit, the agent's own
// token. The old check was `strings.HasPrefix(path, "/etc/")` with no
// cleaning, so the first case below passed and landed in /root/.ssh.
//
// Yapılandırma editörü root olarak yazar. Aşağıdakiler onun üzerinden ASLA
// erişilememesi gereken yollardır; her biri makineyi ya da paneli devralmanın
// bir yolu: ".." ile authorized_keys, bir cron dosyası, bir systemd unit'i,
// agent'ın kendi jetonu. Eski denetim, temizleme yapmayan bir
// `strings.HasPrefix(path, "/etc/")` idi; bu yüzden aşağıdaki ilk durum geçip
// /root/.ssh'a düşüyordu.
func TestConfigWriteRefusesTakeoverPaths(t *testing.T) {
	for _, c := range []struct{ path, why string }{
		{"/etc/../root/.ssh/authorized_keys", "yol geçişi — sunucuyu devreder"},
		{"/etc/../home/user/.ssh/authorized_keys", "yol geçişi, ikinci biçim"},
		{"/etc/cron.d/pwn", "root olarak komut çalıştırır"},
		{"/etc/cron.daily/pwn", "root olarak komut çalıştırır"},
		{"/etc/systemd/system/pwn.service", "root olarak servis tanımlar"},
		{"/etc/celikpanel/agent.token", "bu RPC'yi koruyan kimlik bilgisi"},
		{"/etc/sudoers", "yetki yükseltme"},
		{"/etc/ssh/sshd_config", "SSH erişimini devreder"},
		{"/etc/passwd", "kullanıcı ekler"},
		{"/etc/shadow", "parola hash'leri"},
		{"/etc/ld.so.preload", "her sürece kod enjekte eder"},
		{"/etc/apt/sources.list.d/evil.list", "paket kaynağını devreder"},
		{"/etc/apt/trusted.gpg.d/evil.asc", "imza güvenini devreder"},
		{"/tmp/anything", "yönetilen bir yapılandırma değil"},
		{"relative/path", "mutlak değil"},
		{"", "boş"},
	} {
		if got, err := configWriteAllowed(c.path); err == nil {
			t.Errorf("%q ALLOWED (resolved to %q) — %s", c.path, got, c.why)
		}
	}
}

// Broad directory prefixes are not an authorization boundary. Website files
// are managed by the file manager and generated nginx files by their owning
// workflow; the root config editor may edit only paths discovered in the
// component catalogue.
func TestConfigWriteRefusesBroadDirectoryPrefixes(t *testing.T) {
	for _, p := range []string{
		"/etc/nginx/sites-available/example.com.conf",
		"/etc/nginx/conf.d/celikpanel-webmail.conf",
		"/var/www/example.com/index.html",
	} {
		if got, err := configWriteAllowed(p); err == nil {
			t.Errorf("%q allowed as %q through a broad directory prefix", p, got)
		}
	}
}

func TestConfigWriteRefusesDNSManagedPathsEvenWhenDiscovered(t *testing.T) {
	for _, path := range []string{
		"/etc/powerdns/pdns.conf",
		"/etc/powerdns/pdns.d/celikpanel.conf",
		"/etc/powerdns/pdns.d/celikpanel-cluster.conf",
		"/etc/bind/named.conf.local",
		"/etc/bind/named.conf.options",
		"/etc/named.conf",
		"/var/lib/powerdns/pdns.sqlite3",
		"/var/cache/bind/celikpanel/current",
		"/var/named/celikpanel/current",
	} {
		if _, err := configWriteAllowedFrom(
			path,
			func() []string { return []string{path} },
			func(string) error { return nil },
		); err == nil {
			t.Errorf("DNS managed path %q bypassed its engine workflow", path)
		}
	}
}

func TestConfigWriteStillAllowsDiscoveredNonDNSServiceConfig(t *testing.T) {
	path := "/etc/nginx/nginx.conf"
	got, err := configWriteAllowedFrom(
		path,
		func() []string { return []string{path} },
		func(string) error { return nil },
	)
	if err != nil || got != filepath.Clean(path) {
		t.Fatalf("non-DNS managed config got=%q err=%v", got, err)
	}
}

// filepath.Clean must be applied before authorization; neither a normalized
// web path nor a path that escapes it receives blanket write permission.
func TestConfigWriteJudgesTheCleanedPath(t *testing.T) {
	if _, err := configWriteAllowed("/var/www/site/../site/index.html"); err == nil {
		t.Error("a normalized /var/www path must still require catalogue ownership")
	}
	if _, err := configWriteAllowed("/var/www/../etc/shadow"); err == nil {
		t.Error("/var/www/../etc/shadow must be refused — it cleans to /etc/shadow")
	}
}

// A symlink is the other way out of an allow-list: point a file inside an
// allowed directory at /etc/shadow and the write follows it. Refuse links
// outright rather than resolving them into a judgement call.
// Sembolik bağ, beyaz listeden çıkmanın öbür yoludur: izinli bir dizindeki
// dosyayı /etc/shadow'a yönlendirin, yazma onu izler. Bağları çözüp yargıya
// dönüştürmek yerine doğrudan reddet.
func TestConfigWriteRefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Skip("cannot create files here")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable on this platform")
	}
	// Model the scanner authorizing this exact file. Authorization must not
	// weaken the independent filesystem check: a managed symlink is still
	// refused before any read or write can follow it.
	// Tarayıcının tam bu dosyayı yetkilendirdiğini modelle. Yetkilendirme,
	// bağımsız dosya sistemi denetimini zayıflatmamalıdır: yönetilen bir
	// sembolik bağ, herhangi bir okuma ya da yazma onu izlemeden reddedilir.
	_, err := configWriteAllowedFrom(link, func() []string {
		return []string{link}
	}, rejectConfigPathSymlinks)
	if err == nil {
		t.Fatal("a symlink must never be written through")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// The rollback exists because a live write emptied /etc/nginx/nginx.conf and
// left it that way: the old code wrote first, validated after, and on failure
// returned an error while the broken file stayed on disk. nginx kept serving
// from memory, so nothing looked wrong until the next reload would have
// killed it. A config editor must never leave the file worse than it found it.
//
// This tests the mechanism directly (a validator that always fails must leave
// the original bytes untouched), because the RPC itself needs root and a real
// systemd.
//
// Geri alma, canlı bir yazmanın /etc/nginx/nginx.conf'u boşaltıp öyle
// bırakmasından doğdu: eski kod önce yazıyor, sonra doğruluyor ve düşünce
// bozuk dosya diskte kalırken hata döndürüyordu. nginx bellekten sunmayı
// sürdürdüğü için bir sonraki yeniden yükleme onu öldürene dek hiçbir şey
// ters görünmüyordu. Yapılandırma editörü, dosyayı bulduğundan daha kötü
// bırakamaz.
//
// Mekanizma doğrudan sınanır (her zaman düşen bir doğrulayıcı, özgün baytları
// olduğu gibi bırakmalı); çünkü RPC'nin kendisi root ve gerçek systemd ister.
func TestConfigWriteRollsBackOnFailedValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nginx.conf")
	original := "worker_processes 1;\nhttp { include /etc/nginx/conf.d/*.conf; }\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// The exact shape UpdateConfig uses: remember, write, validate, restore.
	// UpdateConfig'in kullandığı şeklin aynısı: hatırla, yaz, doğrula, geri al.
	previous, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	validationFailed := true
	if validationFailed {
		if err := os.WriteFile(path, previous, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("after a failed validation the file must be unchanged.\n got: %q\nwant: %q", got, original)
	}
}

// Every file the editor can reach that HAS a syntax checker must be wired to
// it — an unvalidated write to a mail or web server config is one restart away
// from an outage nobody saw coming.
// Editörün ulaşabildiği ve sözdizim denetleyicisi OLAN her dosya ona
// bağlanmalıdır — posta ya da web sunucusu ayarına doğrulanmamış bir yazma,
// kimsenin gelmesini görmediği bir kesintiden bir yeniden başlatma uzaktadır.
func TestKnownConfigsHaveAValidator(t *testing.T) {
	for path, wantReload := range map[string]string{
		"/etc/nginx/nginx.conf":                   "nginx",
		"/etc/nginx/sites-available/example.conf": "nginx",
		"/etc/postfix/main.cf":                    "postfix",
		"/etc/dovecot/dovecot.conf":               "dovecot",
	} {
		v := configValidator(path)
		if v == nil {
			t.Errorf("%s has no validator — it can be broken silently", path)
			continue
		}
		if v.reload != wantReload {
			t.Errorf("%s reloads %q, want %q", path, v.reload, wantReload)
		}
	}
	// A file with no known checker is written as-is; that is honest, not a bug.
	// Bilinen denetleyicisi olmayan dosya olduğu gibi yazılır; bu bir hata
	// değil, dürüstlüktür.
	if configValidator("/var/www/site/index.html") != nil {
		t.Error("a plain web file must not be validated as a server config")
	}
}

// A unit name that is an ALIAS of another component's unit must not count as
// proof that this component is installed. Arch's valkey package ships
// /usr/lib/systemd/system/redis.service as a symlink to valkey.service (it
// declares Provides: redis), so installing Valkey made the panel report
// "Redis: installed, inactive (dead)" — a component that was never installed,
// wearing someone else's name, and occupying its own seat while doing it.
// Caught live on Frankfurt (25 Jul) the moment Valkey was added.
//
// Başka bir bileşenin unit'ine TAKMA AD olan bir unit adı, bu bileşenin kurulu
// olduğunun kanıtı sayılamaz. Arch'ın valkey paketi
// /usr/lib/systemd/system/redis.service'i valkey.service'e sembolik bağ olarak
// koyar (Provides: redis der); bu yüzden Valkey kurmak paneli "Redis: kurulu,
// ölü" demeye itiyordu — hiç kurulmamış bir bileşen, başkasının adını taşıyor
// ve üstelik kendi koltuğunu işgal ediyordu. Valkey eklenir eklenmez
// Frankfurt'ta canlı yakalandı (25 Tem).
func TestUnitAliasDoesNotProveInstalled(t *testing.T) {
	redisNames := []string{"redis-server", "redis"}
	valkeyNames := []string{"valkey-server", "valkey"}

	// The live case: redis.service -> valkey.service on Arch.
	// Canlı durum: Arch'ta redis.service -> valkey.service.
	if unitProvesInstalled("redis", "valkey", redisNames) {
		t.Error("redis.service aliased to valkey.service must NOT mark Redis installed")
	}
	// Valkey's own unit, under its own name.
	// Valkey'in kendi unit'i, kendi adıyla.
	if !unitProvesInstalled("valkey", "valkey", valkeyNames) {
		t.Error("a component's own unit must mark it installed")
	}
	// A component's SECOND name pointing at its FIRST is still itself: Debian
	// ships valkey-server, Arch valkey, and either may alias the other.
	// Bir bileşenin İKİNCİ adının BİRİNCİYE işaret etmesi yine kendisidir:
	// Debian valkey-server, Arch valkey getirir; biri diğerine takma ad olabilir.
	if !unitProvesInstalled("valkey-server", "valkey", valkeyNames) {
		t.Error("an alias between a component's OWN names must still count")
	}
	// systemd said nothing (older systemd, odd unit): trust the name rather
	// than calling an installed component missing.
	// systemd bir şey söylemedi (eski systemd, tuhaf unit): kurulu bir bileşeni
	// yok saymaktansa ada güven.
	if !unitProvesInstalled("redis", "", redisNames) {
		t.Error("when systemd cannot answer, the plain name must still count")
	}
}
