package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	_ "modernc.org/sqlite"
)

// The deletion guards decide whether live sites keep working, so their SQL
// runs here against a real SQLite with the schema quirks that matter:
// NULL project_type meaning php, ghost php_version values on non-php sites,
// NULL runtime_version meaning "system node", dnsonly rows in sites.
//
// Silme bekçileri yaşayan sitelerin çalışıp çalışmayacağına karar verir; bu
// yüzden SQL'leri burada, önemli şema tuhaflıklarını taşıyan gerçek bir
// SQLite'a karşı koşar: php demek olan NULL project_type, php-olmayan
// sitelerdeki hayalet php_version değerleri, "sistem node'u" demek olan NULL
// runtime_version, sites içindeki dnsonly satırları.
func newDependentsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	schema := `
	CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT);
	CREATE TABLE subscriptions (id INTEGER PRIMARY KEY, owner_id INTEGER);
	CREATE TABLE domains (id INTEGER PRIMARY KEY, subscription_id INTEGER, name TEXT);
	CREATE TABLE sites (id INTEGER PRIMARY KEY, domain_id INTEGER, project_type TEXT, php_version TEXT, runtime_version TEXT);
	CREATE TABLE email_accounts (id INTEGER PRIMARY KEY, domain_id INTEGER, address TEXT);
	CREATE TABLE email_forwardings (id INTEGER PRIMARY KEY, domain_id INTEGER, source TEXT);
	CREATE TABLE database_server_types (id INTEGER PRIMARY KEY, name TEXT);
	CREATE TABLE database_servers (id INTEGER PRIMARY KEY, type_id INTEGER);
	CREATE TABLE databases_v2 (id INTEGER PRIMARY KEY, server_id INTEGER, name TEXT);
	CREATE TABLE databases (id INTEGER PRIMARY KEY, name TEXT, db_type TEXT);

	INSERT INTO users VALUES (1, 'ali'), (2, 'veli');
	INSERT INTO subscriptions VALUES (10, 1), (20, 2);
	INSERT INTO domains VALUES
		(100, 10, 'php83.example'),
		(101, 10, 'php84.example'),
		(102, 20, 'static.example'),
		(103, 20, 'nodeapp.example'),
		(104, 20, 'legacynode.example'),
		(105, 10, 'dnsonly.example'),
		(106, 10, 'legacyrow.example');
	INSERT INTO sites (id, domain_id, project_type, php_version, runtime_version) VALUES
		(1, 100, 'php',     '8.3', NULL),
		(2, 101, 'php',     '8.4', NULL),
		-- ghost php_version on a static site: must never block PHP removals
		-- statik sitede hayalet php_version: PHP kaldırmayı asla engellememeli
		(3, 102, 'static',  '8.3', NULL),
		(4, 103, 'node',    '',    '24.18.0'),
		-- legacy system-node row: blocks removing node entirely, but no
		-- specific version / eski sistem-node satırı: node'un bütününü
		-- engeller ama belirli bir sürümü engellemez
		(5, 104, 'node',    '',    NULL),
		(6, 105, 'dnsonly', '',    NULL),
		-- pre-005 legacy row: NULL project_type means php
		-- 005 öncesi satır: NULL project_type php demektir
		(7, 106, NULL,      '8.3', NULL);
	INSERT INTO email_accounts VALUES (1, 100, 'info@php83.example');
	INSERT INTO database_server_types VALUES (1, 'postgresql'), (2, 'mariadb');
	INSERT INTO database_servers VALUES (1, 2);
	INSERT INTO databases_v2 VALUES (1, 1, 'shopdb');
	INSERT INTO databases VALUES (1, 'olddb', 'mysql');
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func TestRuntimeVersionBlockers(t *testing.T) {
	db := newDependentsTestDB(t)
	ctx := context.Background()

	// PHP 8.3: the real php site AND the NULL-project_type legacy row block;
	// the static site's ghost value does not.
	// PHP 8.3: gerçek php sitesi VE NULL project_type'lı eski satır engeller;
	// statik sitenin hayalet değeri engellemez.
	count, lines, err := runtimeVersionBlockers(ctx, db, "php-fpm", "8.3")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("php 8.3 blockers = %d (%v), want 2", count, lines)
	}
	for _, l := range lines {
		if l == "static.example (veli)" {
			t.Error("a static site's ghost php_version must not block")
		}
	}

	// PHP 8.5: nobody uses it — removable.
	if count, _, _ := runtimeVersionBlockers(ctx, db, "php-fpm", "8.5"); count != 0 {
		t.Errorf("php 8.5 blockers = %d, want 0", count)
	}

	// Node 24.18.0: one pinned site; the legacy NULL row must NOT pin it.
	count, lines, err = runtimeVersionBlockers(ctx, db, "node", "24.18.0")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || lines[0] != "nodeapp.example (veli)" {
		t.Errorf("node 24.18.0 blockers = %d %v, want 1 [nodeapp.example (veli)]", count, lines)
	}
	if count, _, _ := runtimeVersionBlockers(ctx, db, "node", "22.1.0"); count != 0 {
		t.Errorf("unused node version must be removable")
	}
}

func TestServiceDependents(t *testing.T) {
	db := newDependentsTestDB(t)
	ctx := context.Background()

	cases := []struct {
		serviceID string
		want      int
		why       string
	}{
		{"php-fpm", 3, "iki php sitesi + NULL-tip eski satır"},
		{"node", 2, "sürümlü site + eski sistem-node satırı"},
		{"nginx", 6, "dnsonly hariç tüm siteler"},
		{"apache", 6, "web-server grubunun diğer üyesi, aynı sayım"},
		{"pdns", 7, "tüm domain'ler (D-009: DNS'siz domain olmaz)"},
		{"postfix", 1, "bir posta kutusu"},
		{"mariadb", 2, "v2'de shopdb + eski tabloda mysql eşanlamlısı"},
		{"postgresql", 0, "hiç postgres veritabanı yok"},
		{"redis", 0, "bağımlısı bilinmeyen servis bekçisiz — tiyatro yok"},
	}
	for _, c := range cases {
		count, lines, err := serviceDependents(ctx, db, c.serviceID)
		if err != nil {
			t.Fatalf("%s: %v", c.serviceID, err)
		}
		if count != c.want {
			t.Errorf("%s: dependents = %d (%v), want %d — %s", c.serviceID, count, lines, c.want, c.why)
		}
	}
}

// The cap keeps refusals readable and HONEST: more blockers than lines →
// the tail says how many were not shown.
// Sınır, retleri okunur ve DÜRÜST tutar: satırdan çok engelleyici varsa
// kuyruk kaçının gösterilmediğini söyler.
func TestBlockerCapIsHonest(t *testing.T) {
	db := newDependentsTestDB(t)
	ctx := context.Background()
	for i := 0; i < 15; i++ {
		if _, err := db.Exec(`INSERT INTO domains (subscription_id, name) VALUES (10, printf('bulk%02d.example', ?));`, i); err != nil {
			t.Fatal(err)
		}
	}
	count, lines, err := serviceDependents(ctx, db, "pdns")
	if err != nil {
		t.Fatal(err)
	}
	if count != 22 {
		t.Fatalf("count = %d, want 22", count)
	}
	if len(lines) != blockerCap+1 {
		t.Fatalf("lines = %d, want cap %d + honest tail", len(lines), blockerCap)
	}
	if lines[len(lines)-1] != "+12" {
		t.Errorf("tail = %q, want +12", lines[len(lines)-1])
	}
}

// versionFromPackage: the only door from a package name back to a version.
// versionFromPackage: paket adından sürüme dönen tek kapı.
func TestVersionFromPackage(t *testing.T) {
	php := core.GetManagedServiceByID("php-fpm")
	if php == nil {
		t.Fatal("php-fpm missing from catalogue")
	}
	if v := versionFromPackage(php, "php8.3-fpm"); v != "8.3" {
		t.Errorf("php8.3-fpm → %q, want 8.3", v)
	}
	if v := versionFromPackage(php, "nginx"); v != "" {
		t.Error("a foreign package must not map to a version")
	}
	if v := versionFromPackage(php, "php8.3-cli"); v != "" {
		t.Error("companions are removed via the pick, never named directly")
	}
	if v := versionFromPackage(nil, "php8.3-fpm"); v != "" {
		t.Error("nil service must yield nothing")
	}
}

// The row's start/stop/restart targets a UNIT, and the unit is not always the
// catalogue id: BIND's id is "bind" but its unit is "named"/"bind9", Apache's
// id is "apache" but its unit is "apache2". Shipping the id as the unit made
// every stop/restart on those services a no-op against a unit that does not
// exist (operator, 24 Jul: "I said stop, it didn't stop"). catalogView must
// therefore carry the scanned unit through to the API.
//
// Satırın başlat/durdur/yeniden başlatı bir UNIT'i hedefler ve unit her zaman
// katalog id'si değildir: BIND'in id'si "bind" ama unit'i "named"/"bind9",
// Apache'nin id'si "apache" ama unit'i "apache2". Id'yi unit diye göndermek, o
// servislerde her durdur/yeniden başlatı var olmayan bir unit'e karşı boş
// işleme çeviriyordu (operatör, 24 Tem: "durdur dedim durdurmadı").
// catalogView bu yüzden taranan unit'i API'ye taşımalıdır.
func TestCatalogViewCarriesTheScannedUnit(t *testing.T) {
	obs := []serviceObservation{
		{ID: "bind", IsInstalled: true, Status: "active (running)", Unit: "named"},
		{ID: "apache", IsInstalled: true, Status: "active (running)", Unit: "apache2"},
		{ID: "nginx", IsInstalled: true, Status: "active (running)", Unit: "nginx"},
	}
	byID := map[string]ManagedServiceResponse{}
	for _, s := range catalogView(obs, "apt") {
		byID[s.ID] = s
	}

	for id, wantUnit := range map[string]string{"bind": "named", "apache": "apache2", "nginx": "nginx"} {
		if got := byID[id].Unit; got != wantUnit {
			t.Errorf("%s: unit = %q, want %q — the UI acts on this, not the id", id, got, wantUnit)
		}
	}
	// The two that differ are exactly the ones that broke; pin the difference
	// so nobody "simplifies" the field away again.
	// Farklı olan ikisi tam da kırılanlardır; kimse alanı "sadeleştirip"
	// silmesin diye farkı sabitle.
	if byID["bind"].Unit == byID["bind"].ID {
		t.Error("bind's unit must not equal its id — that equality was the bug")
	}
}

// A component that cannot be installed on this distro must SAY so, not show an
// Install button that fails late in the agent. The rule: package-installed
// component with no mapping for this family → not_offered. Portable components
// (no package mapping at all — node, roundcube) install the same way
// everywhere and must never be marked.
//
// Bu dağıtımda kurulamayan bileşen bunu SÖYLEMELİ; agent'ta geç patlayan bir
// Kur düğmesi göstermemeli. Kural: paketle kurulan bileşenin bu aile için
// eşlemesi yoksa → not_offered. Taşınabilir bileşenler (hiç paket eşlemesi
// olmayanlar — node, roundcube) her yerde aynı yoldan kurulur, asla
// işaretlenmez.
func TestNotOfferedFollowsThePackageMapping(t *testing.T) {
	for _, c := range []struct {
		family string
		id     string
		want   bool
		why    string
	}{
		{"pacman", "spamassassin", true, "spamass-milter Arch depolarında yok — süzemeyecek kurulum sunulmaz"},
		{"apt", "spamassassin", false, "apt eşlemesi tam"},
		{"apt", "netdata", false, "Debian'da üreticinin deposundan kurulur (zorunlu depo)"},
		{"pacman", "netdata", false, "pacman eşlemesi var"},
		{"pacman", "roundcube", false, "taşınabilir (tarball) — her dağıtımda kurulur"},
		{"apt", "node", false, "taşınabilir (resmi sürüm) — her dağıtımda kurulur"},
		{"apt", "apache", true, "paket var ama panel vhost adaptörü yok"},
		{"pacman", "bind", true, "paket var ama panel zone adaptörü yok"},
		{"apt", "exim", true, "paket var ama panel mail adaptörü yok"},
	} {
		var got *ManagedServiceResponse
		for _, s := range catalogView(nil, c.family) {
			if s.ID == c.id {
				s := s
				got = &s
				break
			}
		}
		if got == nil {
			t.Fatalf("%s missing from catalogView", c.id)
		}
		if got.NotOffered != c.want {
			t.Errorf("%s on %s: not_offered = %v, want %v — %s", c.id, c.family, got.NotOffered, c.want, c.why)
		}
	}
}

func TestNotOfferedKindDistinguishesIntegrationFromDistribution(t *testing.T) {
	for _, c := range []struct {
		family string
		id     string
		want   core.ManagedServiceInstallBlockKind
	}{
		{"apt", "apache", core.ManagedServiceInstallBlockIntegration},
		{"apt", "bind", core.ManagedServiceInstallBlockIntegration},
		{"apt", "exim", core.ManagedServiceInstallBlockIntegration},
		{"apt", "vsftpd", core.ManagedServiceInstallBlockIntegration},
		{"pacman", "spamassassin", core.ManagedServiceInstallBlockDistribution},
		{"apt", "nginx", core.ManagedServiceInstallBlockNone},
	} {
		var got *ManagedServiceResponse
		for _, service := range catalogView(nil, c.family) {
			if service.ID == c.id {
				service := service
				got = &service
				break
			}
		}
		if got == nil {
			t.Fatalf("%s missing from catalogView", c.id)
		}
		if got.NotOfferedKind != c.want {
			t.Errorf("%s on %s: not_offered_kind = %q, want %q", c.id, c.family, got.NotOfferedKind, c.want)
		}
	}
}
