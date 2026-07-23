package core

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// RequirementsMissing decides whether a dependent tool can be installed, so a
// wrong answer either blocks a valid install or lets a broken one through.
// A group requirement ("web-server") is met by ANY installed member.
func TestRequirementsMissing(t *testing.T) {
	pma := GetManagedServiceByID("phpmyadmin")
	if pma == nil {
		t.Fatal("phpmyadmin missing from catalogue")
	}

	cases := []struct {
		name      string
		installed map[string]bool
		want      []string
	}{
		{"nothing installed", map[string]bool{}, []string{"mariadb", "web-server", "php-fpm"}},
		{"parent only", map[string]bool{"mariadb": true}, []string{"web-server", "php-fpm"}},
		{"web via nginx (group)", map[string]bool{"mariadb": true, "nginx": true}, []string{"php-fpm"}},
		{"web via apache (group)", map[string]bool{"mariadb": true, "apache": true}, []string{"php-fpm"}},
		{"all satisfied", map[string]bool{"mariadb": true, "nginx": true, "php-fpm": true}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RequirementsMissing(pma, c.installed)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("missing = %v, want %v", got, c.want)
			}
		})
	}

	// A service with no Requires is always installable.
	if got := RequirementsMissing(GetManagedServiceByID("nginx"), map[string]bool{}); got != nil {
		t.Errorf("nginx has no requirements, got %v", got)
	}
}

// Kind decides how a row is drawn and operated (D-010), so an entry added
// without one renders wrong SILENTLY: no start/stop, a status the panel cannot
// justify. The old code guessed this from `len(SystemNames) == 0` and could not
// be forgotten; an explicit field can, so the guard moves here.
//
// Kind, satırın nasıl çizilip işletileceğini belirler (D-010); Kind'siz eklenen
// bir kalem SESSİZCE yanlış çizilir: başlat/durdur yok, panelin
// gerekçelendiremediği bir durum. Eski kod bunu `len(SystemNames) == 0`'dan
// tahmin ederdi ve unutulamazdı; açık bir alan unutulabilir, bekçi buraya taşınır.
func TestEveryServiceDeclaresKind(t *testing.T) {
	for _, s := range ManagedServices {
		switch s.Kind {
		case KindService, KindRuntime, KindTool:
		case "":
			t.Errorf("%s: Kind is empty — classify it as service, runtime or tool (D-010)", s.ID)
		default:
			t.Errorf("%s: unknown Kind %q", s.ID, s.Kind)
		}
	}
}

// A repo that declares VersionCompanions must capture the version prefix its
// templates are expanded from, or every "{v}-mysql" stays literal, apt has
// never heard of it, and the companions are silently dropped — leaving a
// runtime the panel reported as installed but which cannot serve a site.
//
// VersionCompanions bildiren bir depo, şablonlarının genişletildiği sürüm
// önekini yakalamalıdır; yoksa her "{v}-mysql" harfi harfine kalır, apt onu hiç
// duymamıştır ve companion'lar sessizce düşer — panelin kurulu bildirdiği ama
// site sunamayan bir runtime kalır.
func TestRepoCompanionsHaveACaptureGroup(t *testing.T) {
	for _, s := range ManagedServices {
		if s.Repo == nil || len(s.Repo.VersionCompanions) == 0 {
			continue
		}
		re, err := regexp.Compile(s.Repo.PackagePattern)
		if err != nil {
			t.Errorf("%s: PackagePattern does not compile: %v", s.ID, err)
			continue
		}
		if re.NumSubexp() < 1 {
			t.Errorf("%s: declares VersionCompanions but PackagePattern %q captures nothing",
				s.ID, s.Repo.PackagePattern)
		}
		for _, c := range s.Repo.VersionCompanions {
			if !strings.Contains(c, "{v}") {
				t.Errorf("%s: companion %q has no {v} placeholder", s.ID, c)
			}
		}
	}
}

// The repo's pattern must actually match the version packages it will be asked
// to install, and must NOT match a neighbouring name. This is the whitelist
// that stops a version pick from becoming an arbitrary package install, so a
// pattern that is too loose is a security regression, not a typo.
//
// Deponun deseni, kurması istenecek sürüm paketlerini gerçekten eşlemeli ve
// komşu bir adı eşlememelidir. Bu, sürüm seçiminin keyfi paket kurulumuna
// dönüşmesini engelleyen beyaz listedir; fazla gevşek bir desen yazım hatası
// değil, güvenlik regresyonudur.
func TestPHPRepoPatternBoundsWhatCanBeInstalled(t *testing.T) {
	php := GetManagedServiceByID("php-fpm")
	if php == nil || php.Repo == nil {
		t.Fatal("php-fpm has no managed repo — multi-version PHP is the point of D-014")
	}
	re := regexp.MustCompile(php.Repo.PackagePattern)

	for _, ok := range []string{"php8.3-fpm", "php8.4-fpm", "php5.6-fpm", "php8.5-fpm"} {
		m := re.FindStringSubmatch(ok)
		if m == nil {
			t.Errorf("%s should be installable", ok)
			continue
		}
		if len(m) < 2 || m[1] != strings.TrimSuffix(ok, "-fpm") {
			t.Errorf("%s: captured %q, want the version prefix", ok, m)
		}
	}
	// Anything that is not "phpX.Y-fpm" must be refused — especially the CLI and
	// dev packages, which are companions chosen by us, never by the caller.
	// "phpX.Y-fpm" olmayan her şey reddedilmeli — özellikle CLI ve dev paketleri;
	// onlar bizim seçtiğimiz companion'lardır, asla çağıranın değil.
	for _, bad := range []string{"php8.3-cli", "php8.3-fpm-extra", "xphp8.3-fpm", "php-fpm", "php8.3", "nginx"} {
		if re.MatchString(bad) {
			t.Errorf("%s must not be accepted as a version package", bad)
		}
	}
}

// A `service` is defined by having a daemon, and the panel reads its state
// from SystemNames. Without one the row can never report "running", so it
// would sit at "stopped" forever and raise a permanent false alarm on the
// dashboard.
//
// A `runtime` is deliberately exempt (B3b): its truth is the instance list
// (Agent.ListServiceInstances), and a runtime may honestly have no unit at
// all — a Node version is a tarball tree executed only by per-site app units.
// The scan gives unit-less installed runtimes the status "installed", so the
// false-alarm failure mode this guard exists for cannot occur there. A
// runtime WITH units (php-fpm) still gets per-unit status from its instances.
// (The tool direction is also not asserted: a tool may grow a unit one day;
// freezing that would restore the very conflation D-010 deleted.)
//
// `service`, daemon'a sahip olmakla tanımlanır ve panel durumunu
// SystemNames'ten okur. Biri yoksa satır asla "çalışıyor" diyemez, sonsuza dek
// "durdu"da kalır ve panoda kalıcı yanlış alarm üretir.
//
// `runtime` bilerek muaftır (B3b): onun gerçeği instance listesidir
// (Agent.ListServiceInstances) ve bir runtime dürüstçe hiç unit taşımayabilir —
// bir Node sürümü, yalnız site başına app unit'lerinin çalıştırdığı bir tarball
// ağacıdır. Tarama, unit'siz kurulu runtime'lara "installed" durumu verir; bu
// bekçinin var olma sebebi olan yanlış-alarm orada oluşamaz. Unit'i OLAN
// runtime (php-fpm) durumunu yine instance'larından, unit başına alır. (Tool
// yönü de doğrulanmaz: bir tool ileride unit kazanabilir; o yönü dondurmak
// D-010'un sildiği karıştırmayı geri getirirdi.)
func TestDaemonKindsHaveUnits(t *testing.T) {
	for _, s := range ManagedServices {
		if s.Kind != KindService {
			continue
		}
		if len(s.SystemNames) == 0 && s.SystemNamePattern == "" {
			t.Errorf("%s: Kind %q has no SystemNames/SystemNamePattern — it could never report running", s.ID, s.Kind)
		}
	}
}

// The requirement names the ROLE, never the product (operator, 23 Jul:
// "maybe I'll install a different SMTP server"): any member of the
// smtp-server seat satisfies SpamAssassin, exactly like web-server for node.
// Gereksinim ürünü değil ROLÜ adlandırır (operatör, 23 Tem: "belki başka bir
// SMTP sunucusu kurarım"): smtp-server koltuğunun herhangi bir üyesi
// SpamAssassin'i tatmin eder — node'un web-server kuralıyla birebir aynı.
func TestRequirementsNameRolesNotProducts(t *testing.T) {
	sa := GetManagedServiceByID("spamassassin")
	if sa == nil {
		t.Fatal("spamassassin missing from catalogue")
	}
	if got := RequirementsMissing(sa, map[string]bool{}); len(got) != 1 || got[0] != "smtp-server" {
		t.Errorf("empty server: missing = %v, want [smtp-server]", got)
	}
	if got := RequirementsMissing(sa, map[string]bool{"postfix": true}); got != nil {
		t.Errorf("postfix installed must satisfy the smtp-server seat, got %v", got)
	}
	if postfix := GetManagedServiceByID("postfix"); postfix.ConflictGroup != "smtp-server" {
		t.Errorf("postfix must own the smtp-server seat, got %q", postfix.ConflictGroup)
	}
}
