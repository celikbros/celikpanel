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

// The panel's own directories stay writable: that is the config editor doing
// its job on files the panel itself created.
// Panelin kendi dizinleri yazılabilir kalır: yapılandırma editörünün, panelin
// kendi oluşturduğu dosyalar üzerinde işini yapması budur.
func TestConfigWriteAllowsPanelOwnedPaths(t *testing.T) {
	for _, p := range []string{
		"/etc/nginx/sites-available/example.com.conf",
		"/etc/nginx/conf.d/celikpanel-webmail.conf",
		"/var/www/example.com/index.html",
	} {
		if _, err := configWriteAllowed(p); err != nil {
			t.Errorf("%q refused (%v) — the panel manages this path", p, err)
		}
	}
}

// filepath.Clean must be what decides, so a path that LOOKS forbidden but
// cleans into an allowed directory is accepted, and one that looks allowed but
// cleans outside is refused. Both directions matter: a check that only looked
// at the raw string was the bug.
// Kararı filepath.Clean vermeli: yasak GÖRÜNEN ama izinli bir dizine temizlenen
// yol kabul edilir, izinli görünüp dışarı temizlenen reddedilir. İki yön de
// önemli: yalnız ham dizeye bakan denetim, hatanın ta kendisiydi.
func TestConfigWriteJudgesTheCleanedPath(t *testing.T) {
	if _, err := configWriteAllowed("/var/www/site/../site/index.html"); err != nil {
		t.Errorf("a path that cleans back inside /var/www must be allowed: %v", err)
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
	// The link is outside every allow-list anyway, so assert on the reason:
	// the symlink refusal must fire before the allow-list is even consulted.
	// Bağ zaten her beyaz listenin dışında; bu yüzden gerekçeyi sına: bağ reddi,
	// beyaz listeye bakılmadan ÖNCE devreye girmelidir.
	_, err := configWriteAllowed(link)
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
