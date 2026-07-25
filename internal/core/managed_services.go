package core

// FirewallPort is one inbound port a service needs open, with its protocol.
// FirewallPort, bir servisin açık olmasını istediği bir gelen port ve protokolü.
type FirewallPort struct {
	Port  int    // e.g. 443
	Proto string // "tcp" or "udp"
}

// ManagedRepo is an official upstream package repository a service can offer as
// an alternative to the distro's. A distro freezes one major version of a
// database/runtime per release (Debian bookworm ships exactly one PostgreSQL);
// the vendor's own repo carries every current major at once, so the operator
// picks the version they need instead of whatever the OS happened to ship.
//
// Enabling a repo is opt-in, curated and reversible — the same attack-surface
// discipline as installing a service. Only repos declared here (vendor-official,
// pinned to a signing key) can ever be enabled; the UI can never point the agent
// at an arbitrary URL. The key is pinned per-repo with apt's signed-by= (no
// global apt-key trust), and DisableRepo removes the source + key cleanly.
//
// ManagedRepo, bir servisin dağıtımınkine alternatif olarak sunabileceği resmi
// yukarı-akış paket deposudur. Dağıtım her sürümde bir veritabanı/çalışma
// zamanının tek major'unu dondurur (Debian bookworm tek bir PostgreSQL taşır);
// vendor'ın kendi deposu tüm güncel major'ları aynı anda taşır, böylece operatör
// OS'un getirdiğiyle yetinmek yerine ihtiyacı olan sürümü seçer.
//
// Depo açmak opt-in, seçilmiş ve geri alınabilirdir — servis kurmakla aynı
// saldırı-yüzeyi disiplini. Yalnız burada tanımlı depolar (vendor-resmi, imza
// anahtarına sabitli) açılabilir; UI agent'ı asla keyfi bir URL'ye
// yönlendiremez. Anahtar depo başına apt signed-by= ile sabitlenir (küresel
// apt-key güveni yok) ve DisableRepo kaynağı + anahtarı temizce kaldırır.
type ManagedRepo struct {
	ID          string // stable id used in filenames, e.g. "pgdg" ([a-z0-9-]+)
	Name        string // "PostgreSQL Global Development Group (PGDG)"
	Description string // one line, shown in the install dialog
	// KeyURL is the vendor's ASCII-armoured signing key (https). The agent
	// fetches it and pins the source to it with signed-by= — the armoured file
	// is used directly (apt ≥ 1.4), so no gpg dependency is pulled in, which
	// keeps the minimal-install promise.
	// KeyURL, vendor'ın ASCII-zırhlı imza anahtarıdır (https). Agent onu indirir
	// ve kaynağı signed-by= ile ona sabitler — zırhlı dosya doğrudan kullanılır
	// (apt ≥ 1.4), böylece gpg bağımlılığı çekilmez; minimal kurulum sözü korunur.
	KeyURL string
	// SourceTemplate is the apt source line with a {codename} placeholder the
	// agent fills from /etc/os-release (bookworm, noble…). The agent inserts the
	// signed-by= option automatically.
	// SourceTemplate, agent'ın /etc/os-release'ten doldurduğu {codename} yer
	// tutuculu apt kaynak satırıdır. signed-by= seçeneğini agent otomatik ekler.
	SourceTemplate string
	// PackagePattern is a regexp matching the versioned packages this repo
	// provides. The repo — not this catalog — is the source of truth for which
	// versions exist today, so the agent discovers them by matching this against
	// apt-cache. It also bounds what a version-pick install may install: the
	// agent refuses any package that does not match, so the UI can never install
	// an arbitrary name. e.g. `^postgresql-[0-9]+$`.
	// PackagePattern, bu deponun sağladığı sürümlü paketleri eşleyen bir
	// regexp'tir. Hangi sürümlerin bugün var olduğunun kaynağı bu katalog değil
	// depodur; agent bunu apt-cache ile eşleyerek keşfeder. Ayrıca sürüm-seçmeli
	// kurulumun neyi kurabileceğini sınırlar: agent eşleşmeyen paketi reddeder,
	// böylece UI asla keyfi ad kuramaz.
	//
	// If the pattern has a capture group, group 1 is the version prefix that
	// VersionCompanions is templated from — e.g. `^(php[0-9]+\.[0-9]+)-fpm$`
	// captures "php8.3" out of "php8.3-fpm".
	// Desende bir yakalama grubu varsa, 1. grup VersionCompanions'ın
	// şablonlandığı sürüm önekidir — örn. `^(php[0-9]+\.[0-9]+)-fpm$`,
	// "php8.3-fpm" içinden "php8.3"ü yakalar.
	PackagePattern string
	// VersionCompanions are installed alongside a picked version, with {v}
	// replaced by the captured prefix. Without them a version pick installs the
	// bare runtime: `php8.3-fpm` alone has no mysqli, mbstring, curl, xml, gd or
	// zip, so neither WordPress nor Laravel survives its first request — the
	// panel would report a successful install of something that cannot serve a
	// site. The distro's `php-fpm` meta-package hides this by depending on a
	// default set; picking an exact version from a vendor repo does not.
	//
	// Companions are best-effort by design: they are filtered to what apt
	// actually offers for that version and anything missing is REPORTED, never
	// skipped silently. A single absent package must not fail the install —
	// Sury ships no `php8.5-opcache`, so a strict install would refuse PHP 8.5
	// entirely over one optional extension.
	//
	// VersionCompanions, seçilen sürümün yanında kurulur; {v} yakalanan önekle
	// değiştirilir. Onlarsız bir sürüm seçimi çıplak runtime kurar: tek başına
	// `php8.3-fpm`in mysqli, mbstring, curl, xml, gd ve zip'i yoktur, yani ne
	// WordPress ne Laravel ilk isteği geçer — panel, site sunamayacak bir şeyin
	// kurulumunu başarılı bildirirdi. Dağıtımın `php-fpm` meta paketi bunu
	// varsayılan bir sete bağımlı olarak gizler; vendor deposundan tam sürüm
	// seçmek gizlemez.
	//
	// Companion'lar tasarım gereği en-iyi-çabadır: apt'ın o sürüm için gerçekten
	// sunduklarına süzülür ve eksik olan RAPORLANIR, asla sessizce atlanmaz.
	// Tek bir eksik paket kurulumu düşürmemeli — Sury `php8.5-opcache`
	// yayınlamıyor; katı bir kurulum, tek bir isteğe bağlı uzantı yüzünden
	// PHP 8.5'i tümüyle reddederdi.
	VersionCompanions []string
	// Required marks a repository WITHOUT which the package does not exist on
	// this package family at all. Sury and PGDG are optional — the distro ships
	// a PHP and a PostgreSQL, the repo only widens the choice. Netdata is the
	// opposite: Debian/Ubuntu package it nowhere, so pressing Install without
	// the repo fails inside apt with "no installation candidate" — a late,
	// unexplained failure of exactly the kind this project keeps deleting.
	// The UI reads this to present the repo as a required step instead of an
	// optional upgrade.
	// Required, BU paket ailesinde onsuz paketin hiç var olmadığı bir depoyu
	// işaretler. Sury ve PGDG isteğe bağlıdır — dağıtım bir PHP ve bir
	// PostgreSQL getirir, depo yalnız seçeneği genişletir. Netdata bunun tersi:
	// Debian/Ubuntu onu hiçbir yerde paketlemez, dolayısıyla depo olmadan Kur'a
	// basmak apt içinde "kurulum adayı yok" ile düşer — bu projenin sürekli
	// sildiği türden geç ve açıklamasız bir arıza. Arayüz bunu okuyup depoyu
	// isteğe bağlı bir yükseltme değil, zorunlu bir adım olarak sunar.
	Required bool
}

// Kind answers "how is this row drawn and operated?" — the single question
// that used to be guessed from `len(SystemNames) == 0`, a flag that marked
// three unrelated things at once (a file install, a kernel-driven engine, a
// tarball tree). See D-010.
//
// Kind, "bu satır nasıl çizilir ve işletilir?" sorusunu yanıtlar — eskiden
// `len(SystemNames) == 0`'dan tahmin edilen ve üç ayrı şeyi (dosya kurulumu,
// çekirdek-sürümlü motor, tarball ağacı) aynı anda işaretleyen bayrağın yerine.
// Bkz. D-010.
type ServiceKind string

const (
	// KindService: a long-running systemd daemon the panel starts/stops.
	// KindService: panelin başlatıp durdurduğu, sürekli koşan systemd daemon'ı.
	KindService ServiceKind = "service"
	// KindRuntime: a versioned interpreter. Several versions coexist and a
	// SITE picks one; the row shows version badges and a version drawer, never
	// a start/stop button (an individual version's daemon, if any, is operated
	// inside the drawer).
	// KindRuntime: sürümlü yorumlayıcı. Birden çok sürüm yan yana yaşar ve
	// SİTE birini seçer; satır sürüm rozetleri ve sürüm çekmecesi gösterir,
	// asla başlat/durdur düğmesi değil.
	KindRuntime ServiceKind = "runtime"
	// KindTool: no daemon of our own to start or stop — either files served by
	// a web server (phpMyAdmin) or a mechanism the panel drives on demand
	// (nftables: we push our own table with `nft -f` and never touch
	// nftables.service). Rendered as install/remove, plus Open when it has a
	// URL. "Running" is meaningless here and is not drawn.
	// KindTool: başlatıp durduracağımız kendi daemon'ı yok — ya web sunucusunun
	// sunduğu dosyalar (phpMyAdmin) ya da panelin talep üzerine sürdüğü bir
	// mekanizma (nftables: kendi tablomuzu `nft -f` ile iteriz). Kur/kaldır
	// olarak çizilir, URL'i varsa Aç. "Çalışıyor" burada anlamsızdır.
	KindTool ServiceKind = "tool"
)

// ManagedService represents a service that CelikPanel can manage
type ManagedService struct {
	ID          string      // Unique identifier (e.g., "php-fpm", "nginx")
	Name        string      // Display name
	Description string      // Short description
	Icon        string      // Emoji or icon identifier
	Category    string      // "web", "database", "email", "security", "dns", "cache"
	Kind        ServiceKind // service | runtime | tool — decides how the row is drawn (D-010)
	SystemNames []string    // Systemd service names to check
	// SystemNamePattern additionally matches units whose name carries a version,
	// so the set of units does not have to be enumerated in code. A runtime with
	// a vendor repo can install versions this file has never heard of: Sury
	// serves PHP 5.6 through 8.6, and a hardcoded `php8.4-fpm … php8.0-fpm` list
	// makes php8.5-fpm INVISIBLE — installed, running, serving sites, and absent
	// from the panel. Enumerating versions in code is a list that rots by
	// design; the pattern cannot.
	// SystemNamePattern, adında sürüm taşıyan unit'leri de eşler; böylece unit
	// kümesinin kodda sayılması gerekmez. Vendor deposu olan bir runtime, bu
	// dosyanın hiç duymadığı sürümleri kurabilir: Sury PHP 5.6'dan 8.6'ya
	// sunuyor ve elle yazılmış bir `php8.4-fpm … php8.0-fpm` listesi
	// php8.5-fpm'i GÖRÜNMEZ yapar — kurulu, çalışıyor, site sunuyor ve panelde
	// yok. Sürümleri kodda saymak tasarımı gereği bayatlayan bir listedir;
	// desen bayatlayamaz.
	SystemNamePattern string
	// ConflictGroup: services sharing a group are mutually exclusive — they
	// bind the same role/port (a web server on :80, a DNS server on :53) and
	// cannot run together. Empty means no conflict (coexists with everything).
	// The panel blocks installing a service whose group already has an
	// installed member. Databases/caches have no group: they coexist.
	// ConflictGroup: aynı gruptaki servisler karşılıklı dışlar — aynı rol/portu
	// tutarlar (:80'de web sunucusu, :53'te DNS) ve birlikte koşamazlar. Boş
	// ise çakışma yok (her şeyle yan yana). Panel, grubunda zaten kurulu üye
	// olan bir servisin kurulumunu engeller. Veritabanı/önbelleğin grubu yok.
	ConflictGroup string
	// FirewallPorts: the inbound ports this service must expose to the world.
	// The panel opens exactly these when the service is installed and closes
	// them when it is removed — the firewall follows the installed set, so a
	// server only ever exposes what it actually runs. Empty = local-only
	// service (php-fpm, mariadb, redis…): nothing to open, and that is the
	// safest default.
	// FirewallPorts: bu servisin dünyaya açması gereken gelen portlar. Panel
	// servis kurulunca tam bunları açar, kaldırılınca kapatır — güvenlik
	// duvarı kurulu seti izler; sunucu yalnız gerçekten koşturduğunu açar.
	// Boş = yalnız-yerel servis (php-fpm, mariadb, redis…): açılacak port yok,
	// en güvenli varsayılan.
	FirewallPorts []FirewallPort
	// Packages maps a package-manager family ("apt", "dnf", "pacman") to the
	// distro packages that install this service. The panel installs nothing at
	// setup time (the constitution: what isn't installed is invisible); the
	// admin installs a service on demand and the agent installs exactly these
	// packages — a fixed whitelist, never an arbitrary name. "apt"
	// (Ubuntu/Debian) is first-class and tested; "pacman" (Arch, dev-test
	// target per the D-004 amendment) is filled only where the mapping is
	// certain AND the package works without a distro-specific init step — a
	// missing entry keeps the honest "not supported on this distro yet".
	//
	// Packages, bir paket-yöneticisi ailesini ("apt", "dnf", "pacman") bu
	// servisi kuran dağıtım paketlerine eşler. Panel kurulum anında hiçbir şey
	// kurmaz (anayasa: kurulu olmayan görünmez); yönetici servisi talep
	// üzerine kurar ve agent tam olarak bu paketleri kurar — sabit whitelist,
	// asla keyfi ad değil. "apt" (Ubuntu/Debian) birinci sınıf ve test edilmiş;
	// "pacman" (Arch, D-004 ekiyle geliştirme-test hedefi) yalnız eşlemenin
	// kesin olduğu VE paketin dağıtıma özgü init adımı istemediği yerde dolu —
	// boş girdi, dürüst "bu dağıtımda henüz desteklenmiyor"u korur.
	Packages map[string][]string
	// Repo, when set, is the optional vendor repository this service can enable
	// to unlock version choice (see ManagedRepo). nil means the service is only
	// ever installed from the distro — the common, most conservative case.
	// Repo, ayarlıysa, bu servisin sürüm seçimini açmak için etkinleştirebileceği
	// isteğe bağlı vendor deposudur (bkz. ManagedRepo). nil ise servis yalnız
	// dağıtımdan kurulur — yaygın ve en muhafazakâr durum.
	Repo *ManagedRepo
	// Requires: services that must already be installed before this one may be
	// (the inverse of ConflictGroup: "needs" instead of "cannot coexist"). An
	// entry is either a service ID ("mariadb") or a conflict-group name
	// ("web-server" = any installed member satisfies it). Dependent tools like
	// phpMyAdmin only make sense on top of their parent service: the UI hides
	// or disables them until the requirement is met, and the agent enforces it.
	// Requires: bundan önce kurulu olması gereken servisler (ConflictGroup'un
	// tersi: "birlikte olamaz" değil "şuna muhtaç"). Bir girdi ya servis ID'si
	// ("mariadb") ya da çakışma-grubu adıdır ("web-server" = kurulu herhangi
	// bir üye yeter). phpMyAdmin gibi bağımlı araçlar ancak üst servisin
	// üzerinde anlamlıdır: UI, gereksinim karşılanana dek gizler/kapatır;
	// agent da uygular.
	Requires []string
	// HelperUnits: units this service needs running that are NOT the service
	// itself — the bridge daemons that connect it to the rest of the stack.
	// SpamAssassin is the case that forced this field: its own daemon (spamd)
	// only scores a message handed to it; the piece that hands Postfix's mail
	// over is a SEPARATE package with a SEPARATE unit (spamass-milter). Starting
	// only the service's own unit produced the exact lie this project keeps
	// hunting — "installed", "Running", and not one message filtered.
	// The panel enables and starts every helper unit that is present after an
	// install, and stops them on removal.
	//
	// HelperUnits: bu servisin çalışır olmasına ihtiyaç duyduğu ama servisin
	// KENDİSİ olmayan unit'ler — onu yığının geri kalanına bağlayan köprü
	// daemon'ları. Bu alanı zorunlu kılan durum SpamAssassin'dir: kendi
	// daemon'ı (spamd) yalnız kendisine verilen iletiyi puanlar; Postfix'in
	// postasını ona uzatan parça AYRI bir pakette AYRI bir unit'tir
	// (spamass-milter). Yalnız servisin kendi unit'ini başlatmak, bu projenin
	// sürekli avladığı yalanın ta kendisini üretiyordu — "kurulu", "Çalışıyor"
	// ve süzülen tek ileti yok.
	// Panel, kurulumdan sonra mevcut olan her yardımcı unit'i etkinleştirip
	// başlatır, kaldırmada durdurur.
	HelperUnits []string
}

// RequirementsMissing returns which of a service's requirements are not met by
// the given installed-service ID set (group entries are satisfied by any
// installed member of that conflict group). Empty means installable.
// RequirementsMissing, bir servisin gereksinimlerinden hangilerinin verilen
// kurulu-servis kümesince karşılanmadığını döndürür (grup girdilerini o
// çakışma grubunun kurulu herhangi bir üyesi karşılar). Boş = kurulabilir.
func RequirementsMissing(svc *ManagedService, installed map[string]bool) []string {
	var missing []string
	for _, req := range svc.Requires {
		if installed[req] {
			continue
		}
		satisfied := false
		for i := range ManagedServices {
			if ManagedServices[i].ConflictGroup == req && installed[ManagedServices[i].ID] {
				satisfied = true
				break
			}
		}
		if !satisfied {
			missing = append(missing, req)
		}
	}
	return missing
}

// ManagedServices is the list of services CelikPanel manages
var ManagedServices = []ManagedService{
	{
		ID:          "php-fpm",
		Name:        "PHP-FPM",
		Description: "PHP FastCGI Process Manager",
		Icon:        "🐘",
		Category:    "web",
		Kind: KindRuntime,
		// The unversioned unit covers Arch (a single `php-fpm`); every versioned
		// unit is matched by the pattern instead of being listed, so a version
		// installed from Sury is never invisible to the panel.
		// Sürümsüz unit Arch'ı kapsar (tek bir `php-fpm`); sürümlü unit'ler
		// listelenmek yerine desenle eşlenir, böylece Sury'den kurulan bir
		// sürüm panele asla görünmez kalmaz.
		SystemNames:       []string{"php-fpm"},
		SystemNamePattern: `^php[0-9]+\.[0-9]+-fpm$`,
		Packages:          map[string][]string{"apt": {"php-fpm"}, "pacman": {"php-fpm"}},
		// The distro freezes one PHP; Sury carries every maintained major-minor
		// side by side, which is what makes "the customer needs 8.3 while the
		// server runs 8.4" answerable at all (D-014: the entitlement unit is the
		// version, and a chain with one thing to give is vacuous).
		// Dağıtım tek bir PHP dondurur; Sury bakımdaki tüm major-minor'ları yan
		// yana taşır — "sunucu 8.4 koşarken müşterinin 8.3'e ihtiyacı var"ı
		// cevaplanabilir kılan şey budur (D-014: hak birimi sürümdür ve verecek
		// tek şeyi olan bir zincir boştur).
		Repo: &ManagedRepo{
			ID:             "sury",
			Name:           "Sury PHP (packages.sury.org)",
			Description:    "Debian/Ubuntu's standard PHP repository — every maintained version, side by side.",
			KeyURL:         "https://packages.sury.org/php/apt.gpg",
			SourceTemplate: "deb https://packages.sury.org/php/ {codename} main",
			PackagePattern: `^(php[0-9]+\.[0-9]+)-fpm$`,
			// A bare php8.x-fpm runs no real site: no database driver, no
			// mbstring, no curl. These are the extensions WordPress, Laravel and
			// WooCommerce need before their first request.
			// Çıplak bir php8.x-fpm gerçek site koşturmaz: veritabanı sürücüsü,
			// mbstring, curl yok. Bunlar WordPress, Laravel ve WooCommerce'in
			// ilk istekten önce ihtiyaç duyduğu uzantılardır.
			VersionCompanions: []string{
				"{v}-cli", "{v}-common", "{v}-opcache",
				"{v}-mysql", "{v}-pgsql", "{v}-sqlite3",
				"{v}-mbstring", "{v}-xml", "{v}-curl",
				"{v}-gd", "{v}-zip", "{v}-intl", "{v}-bcmath",
			},
		},
	},
	{
		// Node.js has been RUNNABLE for a while (runtime_rpc.go installs
		// verified tarballs, app_rpc.go runs per-site units) but was never
		// declared here — so the Services page, which promises to show what
		// this server can run, said nothing about it. This entry is
		// visibility work, not new capability (B3b).
		// Node.js bir süredir ÇALIŞTIRILABİLİR (runtime_rpc.go doğrulanmış
		// tarball kurar, app_rpc.go site başına unit koşturur) ama burada hiç
		// ilan edilmedi — bu sunucunun ne koşturabildiğini göstermeye söz
		// veren Servisler sayfası ondan hiç bahsetmiyordu. Bu kayıt yeni
		// yetenek değil, görünürlük işidir (B3b).
		ID:          "node",
		Name:        "Node.js",
		Description: "JavaScript Runtime",
		Icon:        "🟩",
		Category:    "web",
		Kind:        KindRuntime,
		// No SystemNames and no Packages, and both are the truth: a Node
		// version is a checksum-verified tarball tree under
		// /opt/celikpanel/runtimes/node/<semver>, not a distro package, and
		// it has no daemon of its own — only per-site app units execute it.
		// Discovery therefore goes through Agent.ListServiceInstances, the
		// same contract PHP uses, instead of unit/package probing.
		// SystemNames de Packages de yok ve ikisi de gerçeğin kendisi: bir
		// Node sürümü /opt/celikpanel/runtimes/node/<semver> altında sağlama
		// toplamı doğrulanmış bir tarball ağacıdır, dağıtım paketi değildir;
		// kendine ait daemon'ı da yoktur — onu yalnız site başına app
		// unit'leri çalıştırır. Keşif bu yüzden unit/paket yoklaması yerine
		// PHP'nin de kullandığı sözleşmeden, Agent.ListServiceInstances'tan
		// geçer.
		// Requires makes the until-now unwritten rule declarative: a Node
		// app is reached only through a reverse proxy, so a web server must
		// exist first (any member of the group satisfies it).
		// Requires, bugüne dek hiçbir yerde yazmayan kuralı bildirime çevirir:
		// bir Node uygulamasına yalnız ters vekille ulaşılır, önce bir web
		// sunucusu olmalıdır (grubun herhangi bir üyesi yeter).
		Requires: []string{"web-server"},
	},
	{
		ID:            "nginx",
		Name:          "Nginx",
		Description:   "Reverse Proxy Server",
		Icon:          "🔄",
		Category:      "web",
		Kind:        KindService,
		SystemNames:   []string{"nginx"},
		ConflictGroup: "web-server",
		Packages:      map[string][]string{"apt": {"nginx"}, "pacman": {"nginx"}},
		FirewallPorts: []FirewallPort{{80, "tcp"}, {443, "tcp"}},
	},
	{
		ID:            "apache",
		Name:          "Apache",
		Description:   "Web Server",
		Icon:          "🪶",
		Category:      "web",
		Kind:        KindService,
		SystemNames:   []string{"apache2", "httpd"},
		ConflictGroup: "web-server",
		Packages:      map[string][]string{"apt": {"apache2"}, "pacman": {"apache"}},
		FirewallPorts: []FirewallPort{{80, "tcp"}, {443, "tcp"}},
	},
	{
		ID:          "postgresql",
		Name:        "PostgreSQL",
		Description: "Database Server",
		Icon:        "🐘",
		Category:    "database",
		Kind:        KindService,
		// "postgresql" is Debian's wrapper unit; the real per-cluster unit is
		// postgresql@<major>-main. The wrapper alone reports the server up, but
		// the pattern also matches the versioned unit so the scan is right even
		// where only the cluster unit exists. Without it, the catalogue-derived
		// ownership test (scan_match.go) would miss postgresql@16-main.
		// "postgresql" Debian'ın sarmalayıcı unit'idir; gerçek küme-başına unit
		// postgresql@<major>-main'dir. Sarmalayıcı tek başına sunucuyu ayakta
		// bildirir ama desen, sürümlü unit'i de eşler; böylece yalnız küme
		// unit'inin var olduğu yerde bile tarama doğrudur. Bu olmadan
		// katalog-türevli sahiplik testi (scan_match.go) postgresql@16-main'i
		// kaçırırdı.
		SystemNames:       []string{"postgresql"},
		SystemNamePattern: `^postgresql@`,
		// pacman deliberately absent: on Arch the cluster needs a manual
		// initdb before first start — mapping the package alone would install
		// a server that cannot start. Same for MariaDB below.
		// pacman bilerek yok: Arch'ta küme ilk başlatmadan önce elle initdb
		// ister — yalnız paketi eşlemek, başlayamayan bir sunucu kurardı.
		// Aşağıdaki MariaDB için de aynısı geçerli.
		Packages: map[string][]string{"apt": {"postgresql"}},
		// The distro ships one PostgreSQL major; PGDG carries every current
		// major, so an admin who needs 17 (or must stay on 16) can pick it.
		// Dağıtım tek bir PostgreSQL major'u getirir; PGDG tüm güncel major'ları
		// taşır, böylece 17'ye ihtiyacı olan (ya da 16'da kalması gereken)
		// yönetici onu seçebilir.
		Repo: &ManagedRepo{
			ID:             "pgdg",
			Name:           "PostgreSQL Global Development Group (PGDG)",
			Description:    "Official PostgreSQL repository — every current major version, kept up to date.",
			KeyURL:         "https://www.postgresql.org/media/keys/ACCC4CF8.asc",
			SourceTemplate: "deb https://apt.postgresql.org/pub/repos/apt {codename}-pgdg main",
			PackagePattern: "^postgresql-[0-9]+$",
		},
	},
	{
		ID:          "mariadb",
		Name:        "MariaDB",
		Description: "Database Server",
		Icon:        "🦭",
		Category:    "database",
		Kind:        KindService,
		SystemNames: []string{"mariadb", "mysql"},
		Packages:    map[string][]string{"apt": {"mariadb-server"}},
	},
	// Web admin tools for the database servers. Daemonless (no systemd unit —
	// just PHP files served by the web server), and only meaningful on top of
	// their parent: Requires hides/blocks them until the parent, a web server
	// and PHP are installed. Served locally and reverse-proxied by the panel.
	// Veritabanı sunucularının web yönetim araçları. Unit'siz (systemd servisi
	// yok — web sunucusunun sunduğu PHP dosyaları) ve ancak üst servisin
	// üzerinde anlamlı: Requires, üst servis + web sunucusu + PHP kurulana dek
	// gizler/engeller. Yerelde sunulur, panel ters-vekiller.
	{
		ID:          "phpmyadmin",
		Name:        "phpMyAdmin",
		Description: "MariaDB/MySQL web admin tool",
		Icon:        "🐬",
		Category:    "database",
		Kind:        KindTool,
		SystemNames: []string{},
		Packages:    map[string][]string{"apt": {"phpmyadmin"}, "pacman": {"phpmyadmin"}},
		Requires:    []string{"mariadb", "web-server", "php-fpm"},
	},
	{
		ID:          "phppgadmin",
		Name:        "phpPgAdmin",
		Description: "PostgreSQL web admin tool",
		Icon:        "🐘",
		Category:    "database",
		Kind:        KindTool,
		SystemNames: []string{},
		// pacman absent: phpPgAdmin is AUR-only on Arch; the whitelist only
		// carries official-repo packages.
		// pacman yok: phpPgAdmin Arch'ta yalnız AUR'da; whitelist yalnız resmi
		// depo paketlerini taşır.
		Packages: map[string][]string{"apt": {"phppgadmin"}},
		Requires: []string{"postgresql", "web-server", "php-fpm"},
	},
	{
		ID:          "postfix",
		Name:        "Postfix",
		Description: "SMTP Server",
		Icon:        "📧",
		Category:    "email",
		Kind:        KindService,
		SystemNames: []string{"postfix"},
		// The SMTP seat: port 25 has one owner, like :53 and :80. The group
		// does double duty — mutual exclusion AND role-requirements: anything
		// that needs "an SMTP server" (SpamAssassin) names the ROLE, never
		// this product (operator, 23 Jul: "maybe I'll install a professional
		// mail solution instead"). A future commercial mail component joins
		// this group and satisfies the same dependents (D-012/D-015).
		// SMTP koltuğu: 25 portunun tek sahibi olur, :53 ve :80 gibi. Grup
		// çifte iş görür — karşılıklı dışlama VE rol-gereksinimi: "bir SMTP
		// sunucusu" isteyen her şey (SpamAssassin) ürünü değil ROLÜ adlandırır
		// (operatör, 23 Tem: "belki profesyonel bir posta çözümü kurarım").
		// İleride ticari bir posta bileşeni bu gruba katılır ve aynı
		// bağımlıları tatmin eder (D-012/D-015).
		ConflictGroup: "smtp-server",
		Packages:      map[string][]string{"apt": {"postfix"}, "pacman": {"postfix"}},
		FirewallPorts: []FirewallPort{{25, "tcp"}, {587, "tcp"}, {465, "tcp"}},
	},
	{
		// The smtp-server seat's SECOND member — the reason the seat is a
		// role and not a product name (operator, 23 Jul: "maybe I'll install
		// a different SMTP server"). Installing Exim while Postfix sits in
		// the seat is refused by the group, and everything that Requires
		// "smtp-server" (rspamd, SpamAssassin) is satisfied by either.
		// smtp-server koltuğunun İKİNCİ üyesi — koltuğun ürün adı değil rol
		// olmasının sebebi (operatör, 23 Tem: "belki başka bir SMTP sunucusu
		// kurarım"). Postfix koltuktayken Exim kurmak grupça reddedilir;
		// "smtp-server" isteyen her şey (rspamd, SpamAssassin) ikisinden
		// biriyle tatmin olur.
		ID:            "exim",
		Name:          "Exim",
		Description:   "SMTP Server",
		Icon:          "📮",
		Category:      "email",
		Kind:          KindService,
		SystemNames:   []string{"exim4", "exim"}, // Debian unit exim4, Arch exim
		ConflictGroup: "smtp-server",
		Packages:      map[string][]string{"apt": {"exim4-daemon-light"}, "pacman": {"exim"}},
		FirewallPorts: []FirewallPort{{25, "tcp"}, {587, "tcp"}, {465, "tcp"}},
	},
	{
		// The modern spam filter (operator, 23 Jul: "don't offer only
		// SpamAssassin — free alternatives too"). Rspamd also signs DKIM,
		// which SpamAssassin never will.
		// Modern spam süzgeci (operatör, 23 Tem: "sadece SpamAssassin
		// olmasın, ücretsiz alternatifler olsun"). Rspamd DKIM imzalamayı da
		// yapar; SpamAssassin bunu hiç yapmayacak.
		ID:          "rspamd",
		Name:        "Rspamd",
		Description: "Spam Filter & DKIM",
		Icon:        "🧹",
		Category:    "email",
		Kind:        KindService,
		SystemNames: []string{"rspamd"},
		// The spam-filter seat (operator, 24 Jul: "can rspamd and SpamAssassin
		// be installed together, do they work together?"). Two spam filters on
		// one mail server is not a feature: Postfix hands mail to ONE filter
		// chain, so a second would either be dead weight or double-scan with
		// two verdicts. Same reasoning as the SMTP, DNS and IMAP seats.
		// Spam-filtresi koltuğu (operatör, 24 Tem: "rspamd ile SpamAssassin
		// birlikte kurulabiliyor mu, birlikte çalışırlar mı?"). Tek posta
		// sunucusunda iki spam filtresi özellik değildir: Postfix postayı TEK
		// filtre zincirine verir; ikincisi ya ölü yük olur ya iki ayrı kararla
		// çift tarama yapar. SMTP, DNS ve IMAP koltuklarıyla aynı gerekçe.
		ConflictGroup: "spam-filter",
		Packages:      map[string][]string{"apt": {"rspamd"}, "pacman": {"rspamd"}},
		Requires:      []string{"smtp-server"},
	},
	{
		ID:          "dovecot",
		Name:        "Dovecot",
		Description: "IMAP/POP3 Server",
		Icon:        "📬",
		Category:    "email",
		Kind:        KindService,
		SystemNames: []string{"dovecot"},
		// The IMAP seat, like smtp-server: ports 143/993 have one owner, and
		// anything that needs "an IMAP server" (webmail) names the ROLE, not
		// Dovecot. A future Cyrus/Stalwart joins this group and satisfies the
		// same dependents.
		// IMAP koltuğu, smtp-server gibi: 143/993 portlarının tek sahibi olur;
		// "bir IMAP sunucusu" isteyen her şey (webmail) ürünü değil ROLÜ
		// adlandırır. İleride Cyrus/Stalwart bu gruba katılır ve aynı
		// bağımlıları tatmin eder.
		ConflictGroup: "imap-server",
		Packages:      map[string][]string{"apt": {"dovecot-imapd", "dovecot-pop3d", "dovecot-lmtpd"}, "pacman": {"dovecot"}},
		FirewallPorts: []FirewallPort{{143, "tcp"}, {993, "tcp"}, {110, "tcp"}, {995, "tcp"}},
	},
	{
		// Webmail (operator, 23 Jul: "webmails too"; 24 Jul: "if it can't be
		// installed on both distros, don't — isn't there a webmail that runs
		// on all Linux?"). The distro package (roundcube on apt, roundcubemail
		// on pacman with a different layout) was exactly the distro-specific
		// trap D-004 forbids. So Roundcube is installed from its OWN official
		// tarball — the Node.js pattern: download, verify a pinned checksum,
		// unpack under /opt/celikpanel/webmail. ONE path on every Linux. That
		// is why Packages is empty: install goes through Agent.InstallRoundcube,
		// not the package manager. Still a tool (a PHP app, no daemon of ours);
		// still gates on imap-server + web-server + php-fpm.
		// Webmail (operatör, 23 Tem: "webmailler olsun"; 24 Tem: "her iki
		// sürümde kurulamıyorsa kurma — tüm Linux'ta çalışan webmail yok mu?").
		// Dağıtım paketi (apt'ta roundcube, pacman'da farklı yerleşimli
		// roundcubemail) tam da D-004'ün yasakladığı dağıtıma-özgü tuzaktı. Bu
		// yüzden Roundcube KENDİ resmi tarball'ından kurulur — Node.js deseni:
		// indir, sabitlenmiş checksum'ı doğrula, /opt/celikpanel/webmail altına
		// aç. Her Linux'ta TEK yol. Packages'ın boş olma sebebi bu: kurulum
		// paket yöneticisinden değil Agent.InstallRoundcube'dan geçer. Yine
		// tool (PHP uygulaması, bize ait daemon yok); yine imap-server +
		// web-server + php-fpm kapısından geçer.
		ID:          "roundcube",
		Name:        "Roundcube",
		Description: "Webmail",
		Icon:        "✉️",
		Category:    "email",
		Kind:        KindTool,
		Requires:    []string{"imap-server", "web-server", "php-fpm"},
	},
	{
		ID:          "spamassassin",
		Name:        "SpamAssassin",
		Description: "Spam Filter",
		Icon:        "🛡️",
		Category:    "email",
		Kind:        KindService,
		// Debian split SpamAssassin in two: `spamassassin` is rules and
		// tools only, the daemon lives in the separate `spamd` package with
		// a `spamd.service` unit. Installing just `spamassassin` LOOKS
		// successful and leaves nothing to run — caught live (Boston,
		// 23 Jul: "Installing…" flipped back to "Not installed" because the
		// scan honestly found no unit). Arch still ships one package with a
		// `spamassassin` unit, hence both names.
		// Debian, SpamAssassin'i ikiye böldü: `spamassassin` yalnız kural ve
		// araçlar; daemon, `spamd.service` unit'iyle ayrı `spamd`
		// paketinde. Yalnız `spamassassin` kurmak BAŞARILI görünür ve ortada
		// koşacak şey bırakmaz — canlıda yakalandı (Boston, 23 Tem:
		// "Installing…" sonra "Not installed"e döndü; tarama dürüstçe unit
		// bulamamıştı). Arch hâlâ tek paket + `spamassassin` unit'i taşır;
		// iki adın sebebi bu.
		SystemNames:   []string{"spamd", "spamassassin"},
		ConflictGroup: "spam-filter",
		// apt only: wiring SpamAssassin into Postfix needs spamass-milter, and
		// that package does not exist in Arch's repos (AUR only, verified live
		// 24 Jul). Offering an install that cannot filter mail would break
		// "installed means working", so on Arch this row says, honestly, that
		// it is not supported yet — Rspamd is the filter that works on both.
		// Yalnız apt: SpamAssassin'i Postfix'e bağlamak spamass-milter ister ve
		// o paket Arch depolarında yok (yalnız AUR, 24 Tem canlı doğrulandı).
		// Postayı süzemeyecek bir kurulum sunmak "kurulunca çalışır"ı bozardı;
		// bu yüzden Arch'ta bu satır dürüstçe "henüz desteklenmiyor" der —
		// ikisinde de çalışan filtre Rspamd'dir.
		Packages:    map[string][]string{"apt": {"spamassassin", "spamd", "spamass-milter"}},
		HelperUnits: []string{"spamass-milter"},
		// A spam filter without an SMTP server filters nothing — and the
		// requirement names the ROLE, not a product: any member of the
		// smtp-server seat satisfies it (operator, 23 Jul: "maybe I'll
		// install a different SMTP server, maybe a professional mail
		// solution"). Same rule as node's web-server requirement.
		// SMTP sunucusuz spam süzgeci hiçbir şeyi süzmez — ve gereksinim
		// ürünü değil ROLÜ adlandırır: smtp-server koltuğunun herhangi bir
		// üyesi tatmin eder (operatör, 23 Tem: "belki başka bir SMTP
		// sunucusu, belki profesyonel bir posta çözümü kurarım"). Node'un
		// web-server gereksinimiyle aynı kural.
		Requires: []string{"smtp-server"},
	},
	{
		ID:            "wireguard",
		Name:          "WireGuard VPN",
		Description:   "Built-in VPN server",
		Icon:          "\U0001F510",
		Category:      "security",
		Kind:        KindService,
		SystemNames:   []string{"wg-quick@wg0"},
		Packages:      map[string][]string{"apt": {"wireguard"}, "pacman": {"wireguard-tools"}},
		FirewallPorts: []FirewallPort{{51820, "udp"}},
	},
	{
		ID:          "fail2ban",
		Name:        "Fail2ban",
		Description: "Intrusion Prevention",
		Icon:        "🚫",
		Category:    "security",
		Kind:        KindService,
		SystemNames: []string{"fail2ban"},
		Packages:    map[string][]string{"apt": {"fail2ban"}, "pacman": {"fail2ban"}},
	},
	// The firewall ENGINE the panel drives. Not a daemon we manage (we push our
	// own nftables table via `nft -f` and never touch nftables.service), so it
	// is daemonless: empty SystemNames → "installed" is a package check. It is
	// a deliberate CHOICE, not a forced default — the panel never auto-installs
	// it. An operator who wants the panel's firewall installs it here; one using
	// ufw/firewalld/a cloud firewall simply never does, and the panel firewall
	// stays off (honest). Turning the firewall on requires this to be present.
	//
	// Panelin kullandığı güvenlik duvarı MOTORU. Yönettiğimiz bir daemon değil
	// (kendi nftables tablomuzu `nft -f` ile iteriz, nftables.service'e hiç
	// dokunmayız), o yüzden daemonless: boş SystemNames → "kurulu" bir paket
	// denetimidir. Bilinçli bir SEÇİMDİR, dayatılan varsayılan değil — panel
	// asla oto-kurmaz. Panelin duvarını isteyen operatör buradan kurar;
	// ufw/firewalld/bulut duvarı kullanan hiç kurmaz, panel duvarı kapalı kalır
	// (dürüst). Güvenlik duvarını açmak bunun var olmasını gerektirir.
	{
		ID:          "nftables",
		Name:        "Firewall engine",
		Description: "nftables — the panel firewall's packet-filtering engine",
		Icon:        "🧱",
		Category:    "security",
		Kind:        KindTool,
		SystemNames: []string{},
		Packages:    map[string][]string{"apt": {"nftables"}, "pacman": {"nftables"}},
	},
	// Antivirus / malware scanner. A daemon plus its signature-updater
	// (freshclam) — both must be present to count as installed. Local scanner:
	// it opens no inbound port, which is exactly the safest shape (D-006). The
	// operator's "first things on a server: firewall, antivirus, spam" — the
	// firewall is a panel feature, spam is SpamAssassin (Email group), and this
	// is the missing third.
	// Antivirüs / kötücül yazılım tarayıcısı. Bir daemon artı imza-güncelleyici
	// (freshclam) — kurulu sayılmak için ikisi de var olmalı. Yerel tarayıcı:
	// hiçbir gelen port açmaz; en güvenli biçim tam da budur (D-006).
	// Operatörün "bir sunucuya ilk kurulacaklar: firewall, antivirüs, spam" —
	// firewall panel özelliği, spam SpamAssassin (E-posta grubu), bu da eksik
	// üçüncü.
	{
		ID:          "clamav",
		Name:        "ClamAV",
		Description: "Antivirus / malware scanner",
		Icon:        "🦠",
		Category:    "security",
		Kind:        KindService,
		SystemNames: []string{"clamav-daemon", "clamav-freshclam"},
		Packages:    map[string][]string{"apt": {"clamav", "clamav-daemon"}, "pacman": {"clamav"}},
	},
	{
		ID:            "bind",
		Name:          "BIND",
		Description:   "DNS Server",
		Icon:          "🌐",
		Category:      "dns",
		Kind:        KindService,
		SystemNames:   []string{"bind9", "named"},
		ConflictGroup: "dns-server",
		Packages:      map[string][]string{"apt": {"bind9"}, "pacman": {"bind"}},
		FirewallPorts: []FirewallPort{{53, "tcp"}, {53, "udp"}},
	},
	{
		ID:            "pdns",
		Name:          "PowerDNS",
		Description:   "DNS Server",
		Icon:          "⚡",
		Category:      "dns",
		Kind:        KindService,
		SystemNames:   []string{"pdns"},
		ConflictGroup: "dns-server",
		// Arch's powerdns ships the sqlite3 backend inside the main package —
		// no separate backend package exists there.
		// Arch'ın powerdns'i sqlite3 arka ucunu ana pakette taşır — orada ayrı
		// backend paketi yoktur.
		Packages: map[string][]string{"apt": {"pdns-server", "pdns-backend-sqlite3"}, "pacman": {"powerdns"}},
		FirewallPorts: []FirewallPort{{53, "tcp"}, {53, "udp"}},
	},
	{
		ID:            "vsftpd",
		Name:          "vsftpd",
		Description:   "FTP Server",
		Icon:          "📂",
		Category:      "ftp",
		Kind:        KindService,
		SystemNames:   []string{"vsftpd"},
		Packages:      map[string][]string{"apt": {"vsftpd"}, "pacman": {"vsftpd"}},
		FirewallPorts: []FirewallPort{{21, "tcp"}},
	},
	{
		ID:          "redis",
		Name:        "Redis",
		Description: "Cache Server",
		Icon:        "⚡",
		Category:    "cache",
		Kind:        KindService,
		SystemNames: []string{"redis-server", "redis"},
		// pacman absent: Arch replaced Redis with the Valkey fork; silently
		// installing a fork under the name "Redis" would be a product decision
		// disguised as a package alias.
		// pacman yok: Arch, Redis'i Valkey çatalıyla değiştirdi; "Redis" adı
		// altında sessizce çatal kurmak, paket takma adı kılığında bir ürün
		// kararı olurdu.
		Packages: map[string][]string{"apt": {"redis-server"}},
	},
	{
		ID:          "memcached",
		Name:        "Memcached",
		Description: "Cache Server",
		Icon:        "💾",
		Category:    "cache",
		Kind:        KindService,
		SystemNames: []string{"memcached"},
		Packages:    map[string][]string{"apt": {"memcached"}, "pacman": {"memcached"}},
	},
	{
		// Deep per-second metrics next to the panel's own Monitoring page
		// (which owns the 48h history either way). No apt entry on purpose:
		// Debian 13 does not carry netdata; the honest path there is
		// Netdata's official vendor repo via the ManagedRepo mechanism —
		// a follow-up, recorded, not faked with a package name that
		// installs nothing. No firewall port either: the packaged default
		// listens on localhost, and that is the right default.
		// Panelin kendi İzleme sayfasının (48 saatlik geçmişin sahibi her
		// hâlükârda orası) yanına saniyelik derin metrikler. apt kaydı
		// bilerek yok: Debian 13 netdata taşımıyor; oradaki dürüst yol,
		// ManagedRepo mekanizmasıyla Netdata'nın resmi deposu — kayıtlı bir
		// devam işi, hiçbir şey kurmayan bir paket adıyla sahtelenmez.
		// Güvenlik duvarı portu da yok: paket varsayılanı localhost dinler
		// ve doğru varsayılan budur.
		ID:          "netdata",
		Name:        "Netdata",
		Description: "Real-time metrics agent",
		Icon:        "📈",
		Category:    "monitoring",
		Kind:        KindService,
		SystemNames: []string{"netdata"},
		// Debian/Ubuntu ship NO netdata package at all — `apt-cache policy
		// netdata` answers "Candidate: (none)" on Debian 13, which is why the
		// operator's install attempt failed (25 Jul: "net data kurulamadı").
		// Arch packages it, Debian does not; the vendor's own repository is
		// the only honest apt source, exactly like Sury for PHP.
		//
		// Debian/Ubuntu netdata paketini HİÇ getirmez — Debian 13'te
		// `apt-cache policy netdata` "Candidate: (none)" der; operatörün
		// kurulum denemesinin düşme sebebi buydu (25 Tem: "net data
		// kurulamadı"). Arch paketler, Debian paketlemez; tek dürüst apt
		// kaynağı, tıpkı PHP'de Sury gibi, üreticinin kendi deposudur.
		Packages: map[string][]string{"apt": {"netdata"}, "pacman": {"netdata"}},
		Repo: &ManagedRepo{
			ID:          "netdata",
			Name:        "Netdata (repo.netdata.cloud)",
			Description: "Netdata's official repository — the only apt source for Debian/Ubuntu, which do not package it.",
			KeyURL:      "https://repo.netdata.cloud/netdatabot.gpg.key",
			// A FLAT repository: the distribution is "{codename}/" with a
			// trailing slash and no component list. Writing the usual
			// "… {codename} main" here yields a 404 that apt reports as a
			// missing Release file. Verified live before shipping (25 Jul):
			// .../debian/trixie/Release is 200 and its Packages index carries
			// netdata 2.10.0 — the rule from the Buypass incident is that an
			// external endpoint is checked against the real service first.
			//
			// DÜZ (flat) depo: dağıtım adı sonunda eğik çizgiyle "{codename}/"
			// ve bileşen listesi yok. Buraya alışılmış "… {codename} main"
			// yazmak, apt'ın "Release dosyası yok" diye bildirdiği bir 404
			// üretir. Göndermeden önce canlı doğrulandı (25 Tem):
			// .../debian/trixie/Release 200 ve Packages dizini netdata
			// 2.10.0 taşıyor — Buypass olayından kalan kural, dış ucun önce
			// gerçek servise karşı sınanmasıdır.
			SourceTemplate: "deb https://repo.netdata.cloud/repos/stable/debian/ {codename}/",
			Required:       true,
			// No PackagePattern: the repo serves one current netdata, not a
			// menu of versions. Version choice is a PHP/PostgreSQL affair.
			// PackagePattern yok: depo sürüm menüsü değil, tek güncel netdata
			// sunar. Sürüm seçimi PHP/PostgreSQL işidir.
		},
	},
}

// GetManagedServiceByID returns a managed service by its ID
func GetManagedServiceByID(id string) *ManagedService {
	for i := range ManagedServices {
		if ManagedServices[i].ID == id {
			return &ManagedServices[i]
		}
	}
	return nil
}

// GetManagedServicesByCategory returns all managed services in a category
func GetManagedServicesByCategory(category string) []ManagedService {
	var services []ManagedService
	for _, svc := range ManagedServices {
		if svc.Category == category {
			services = append(services, svc)
		}
	}
	return services
}
