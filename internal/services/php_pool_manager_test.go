package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

// withPoolTree points the pool writer at a temp PHP tree and seeds one pool
// exactly as site creation would write it.
// withPoolTree, havuz yazıcısını geçici bir PHP ağacına yöneltir ve site
// oluşturmanın yazacağı gibi tek bir havuz tohumlar.
func withPoolTree(t *testing.T, version, pool, body string) string {
	t.Helper()
	dir := t.TempDir()
	old := phpEtcDir
	phpEtcDir = dir
	t.Cleanup(func() { phpEtcDir = old })

	poolDir := filepath.Join(dir, version, "fpm", "pool.d")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(poolDir, pool+".conf")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const seededPool = `[site42]
user = celik_site42
group = celik_site42
listen = /var/run/php/php8.3-fpm-site42.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = dynamic
pm.max_children = 5
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
pm.max_requests = 500
chdir = /
`

// THE security regression guard. POST /api/v1/domains/{id}/php/pool is
// authorised by domain OWNERSHIP, not by admin, and it reaches this writer. The
// handler pinned only Version and Name, so a customer could send
// {"pool_config":{"user":"root","group":"root"}} and have their PHP run as root
// after the next FPM reload. Identity must come from the pool on disk — which
// the panel wrote — and never from the request.
//
// ASIL güvenlik regresyon bekçisi. POST /api/v1/domains/{id}/php/pool admin ile
// değil alan adı SAHİPLİĞİ ile yetkilendirilir ve bu yazıcıya ulaşır. Handler
// yalnız Version ve Name'i sabitliyordu; yani bir müşteri
// {"pool_config":{"user":"root","group":"root"}} gönderip bir sonraki FPM
// yeniden yüklemesinden sonra PHP'sini root olarak koşturabilirdi. Kimlik,
// panelin yazdığı diskteki havuzdan gelmeli, asla istekten değil.
func TestUpdatePoolConfigRefusesToTakeIdentityFromTheCaller(t *testing.T) {
	path := withPoolTree(t, "8.3", "site42", seededPool)
	pm := NewPHPPoolManager()

	// Everything a hostile tenant would try at once.
	// Kötü niyetli bir kiracının hep birlikte deneyeceği her şey.
	hostile := &core.PHPPoolConfig{
		Name:          "site42",
		User:          "root",
		Group:         "root",
		Listen:        "/var/run/php/php8.3-fpm-site7.sock", // another tenant's socket
		ListenOwner:   "root",
		ListenGroup:   "root",
		ListenMode:    "0666",
		PM:            "dynamic",
		PMMaxChildren: 6,
	}
	// The reload will fail in a temp tree (no systemctl); the write must have
	// happened before that, so the error is ignored and the file is inspected.
	// Geçici ağaçta reload başarısız olur (systemctl yok); yazma ondan önce
	// gerçekleşmiş olmalı, bu yüzden hata yok sayılıp dosya incelenir.
	_ = pm.UpdatePoolConfig("8.3", hostile)

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	for _, forbidden := range []string{
		"user = root",
		"group = root",
		"listen.owner = root",
		"listen.group = root",
		"listen.mode = 0666",
		"site7.sock",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("caller-supplied identity reached the pool file: %q\n---\n%s", forbidden, got)
		}
	}
	for _, required := range []string{
		"user = celik_site42",
		"group = celik_site42",
		"listen = /var/run/php/php8.3-fpm-site42.sock",
		"listen.owner = www-data",
		"listen.mode = 0660",
	} {
		if !strings.Contains(got, required) {
			t.Errorf("the pool's own identity was lost: %q missing\n---\n%s", required, got)
		}
	}
}

// pm.max_children multiplies a tenant's memory across the host, so an unclamped
// value from a customer is a one-request denial of service against every other
// site on the machine.
// pm.max_children, bir kiracının belleğini makine boyunca çarpar; müşteriden
// gelen sınırsız bir değer, makinedeki diğer tüm sitelere karşı tek istekle
// hizmet reddidir.
func TestUpdatePoolConfigClampsResourceTunables(t *testing.T) {
	path := withPoolTree(t, "8.3", "site42", seededPool)
	pm := NewPHPPoolManager()

	_ = pm.UpdatePoolConfig("8.3", &core.PHPPoolConfig{
		Name:          "site42",
		PM:            "dynamic",
		PMMaxChildren: 999999,
		PMMaxRequests: 999999999,
	})
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "pm.max_children = 999999") {
		t.Errorf("pm.max_children was not clamped:\n%s", got)
	}
	if !strings.Contains(string(got), "pm.max_children = 200") {
		t.Errorf("expected the clamp ceiling, got:\n%s", got)
	}

	// A zero must become the default, never a literal 0 — a pool with
	// pm.max_children = 0 does not start.
	// Sıfır varsayılana dönmeli, asla düz 0 olmamalı — pm.max_children = 0 olan
	// havuz başlamaz.
	_ = pm.UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: "site42", PM: "ondemand"})
	got, _ = os.ReadFile(path)
	if strings.Contains(string(got), "pm.max_children = 0") {
		t.Errorf("zero was written literally:\n%s", got)
	}
	if !strings.Contains(string(got), "pm = ondemand") {
		t.Errorf("a valid pm mode should be accepted:\n%s", got)
	}
}

// An unknown pm mode makes the FPM master refuse to start the pool, so it must
// never reach the file — every site on that version would go down.
// Bilinmeyen bir pm kipi, FPM master'ının havuzu başlatmayı reddetmesine yol
// açar; bu yüzden dosyaya asla ulaşmamalı — o sürümdeki tüm siteler düşerdi.
func TestUpdatePoolConfigRejectsUnknownPMMode(t *testing.T) {
	path := withPoolTree(t, "8.3", "site42", seededPool)
	pm := NewPHPPoolManager()
	_ = pm.UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: "site42", PM: "; malicious", PMMaxChildren: 5})
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "pm = dynamic") {
		t.Errorf("an invalid pm mode was not replaced by a safe one:\n%s", got)
	}
}

// A pool name selects the file this writes under the PHP tree, so it is bounded
// to the panel's own scheme — otherwise "../../../../etc/passwd" is a filename.
// Bir havuz adı, PHP ağacı altında yazılacak dosyayı seçer; bu yüzden panelin
// kendi şemasına sınırlanır — yoksa "../../../../etc/passwd" bir dosya adıdır.
func TestUpdatePoolConfigBoundsThePoolName(t *testing.T) {
	withPoolTree(t, "8.3", "site42", seededPool)
	pm := NewPHPPoolManager()
	for _, bad := range []string{"../../../../etc/cron.d/evil", "site42; rm -rf /", "www", ""} {
		if err := pm.UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: bad, PM: "dynamic"}); err == nil {
			t.Errorf("pool name %q was accepted", bad)
		}
	}
}

// Updating a pool that does not exist must refuse rather than invent one: a
// pool whose user cannot be resolved makes the FPM master refuse to start, which
// takes down every site on that version — not just this one.
// Var olmayan bir havuzu güncellemek, uydurmak yerine reddetmelidir: kullanıcısı
// çözülemeyen bir havuz FPM master'ının başlamayı reddetmesine yol açar ve o
// sürümdeki yalnız bu sitenin değil TÜM sitelerin düşmesine sebep olur.
func TestUpdatePoolConfigRefusesWhenThePoolIsAbsent(t *testing.T) {
	withPoolTree(t, "8.3", "site42", seededPool)
	pm := NewPHPPoolManager()
	if err := pm.UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: "site99", PM: "dynamic"}); err == nil {
		t.Error("updating a non-existent pool should refuse, not create one")
	}
	if _, err := os.Stat(filepath.Join(phpEtcDir, "8.3", "fpm", "pool.d", "site99.conf")); err == nil {
		t.Error("a pool file was invented for a site that has none")
	}
}

// Migration must WRITE the new version's pool even though UpdatePoolConfig
// refuses to create pools. The regression this pins: the identity-forgery
// gate (FPM-as-root fix) made 8.3→8.4 switching return 500 in production,
// because migration rode through the tenant-facing writer. Identity must
// come from the OLD pool file; only the socket path may change.
// Taşıma, UpdatePoolConfig havuz yaratmayı reddettiği hâlde yeni sürümün
// havuzunu YAZMALIDIR. Sabitlenen gerileme: kimlik-uydurma kapısı (FPM-root
// düzeltmesi) 8.3→8.4 geçişini canlıda 500'e çevirdi; çünkü taşıma, kiracıya
// dönük yazıcının üstünden geçiyordu. Kimlik ESKİ havuz dosyasından gelmeli;
// yalnız soket yolu değişebilir.
func TestMigratePoolWritesNewVersionDirectly(t *testing.T) {
	oldPath := withPoolTree(t, "8.3", "site42", seededPool)
	if err := os.MkdirAll(filepath.Join(phpEtcDir, "8.4", "fpm", "pool.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldReload := reloadPHPFPM
	reloaded := []string{}
	reloadPHPFPM = func(v string) error { reloaded = append(reloaded, v); return nil }
	t.Cleanup(func() { reloadPHPFPM = oldReload })

	pm := NewPHPPoolManager()
	if err := pm.MigratePool("8.3", "8.4", "site42"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	newBody, err := os.ReadFile(filepath.Join(phpEtcDir, "8.4", "fpm", "pool.d", "site42.conf"))
	if err != nil {
		t.Fatalf("new pool not written: %v", err)
	}
	s := string(newBody)
	for _, want := range []string{
		"user = celik_site42", // identity preserved from the OLD pool
		"listen = /var/run/php/php8.4-fpm-site42.sock", // socket re-derived
		"listen.owner = www-data",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("new pool missing %q\n%s", want, s)
		}
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old pool file must be gone after migration")
	}
	if len(reloaded) != 2 || reloaded[0] != "8.4" || reloaded[1] != "8.3" {
		t.Errorf("reload order = %v, want [8.4 8.3] (new first, then old after delete)", reloaded)
	}

	// The gate itself must still hold: updating a pool that does not exist
	// stays refused. / Kapı yerinde durmalı: olmayan havuzu güncellemek
	// reddedilmeye devam eder.
	if err := pm.UpdatePoolConfig("8.5", &core.PHPPoolConfig{Name: "site42"}); err == nil {
		t.Error("UpdatePoolConfig must still refuse to invent pools")
	}
}
