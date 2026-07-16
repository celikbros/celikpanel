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
	PackagePattern string
}

// ManagedService represents a service that CelikPanel can manage
type ManagedService struct {
	ID          string   // Unique identifier (e.g., "php-fpm", "nginx")
	Name        string   // Display name
	Description string   // Short description
	Icon        string   // Emoji or icon identifier
	Category    string   // "web", "database", "email", "security", "dns", "cache"
	SystemNames []string // Systemd service names to check
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
		SystemNames: []string{"php8.4-fpm", "php8.3-fpm", "php8.2-fpm", "php8.1-fpm", "php8.0-fpm", "php-fpm"},
		Packages:    map[string][]string{"apt": {"php-fpm"}, "pacman": {"php-fpm"}},
	},
	{
		ID:            "nginx",
		Name:          "Nginx",
		Description:   "Reverse Proxy Server",
		Icon:          "🔄",
		Category:      "web",
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
		SystemNames: []string{"postgresql"},
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
		SystemNames: []string{},
		// pacman absent: phpPgAdmin is AUR-only on Arch; the whitelist only
		// carries official-repo packages.
		// pacman yok: phpPgAdmin Arch'ta yalnız AUR'da; whitelist yalnız resmi
		// depo paketlerini taşır.
		Packages: map[string][]string{"apt": {"phppgadmin"}},
		Requires: []string{"postgresql", "web-server", "php-fpm"},
	},
	{
		ID:            "postfix",
		Name:          "Postfix",
		Description:   "SMTP Server",
		Icon:          "📧",
		Category:      "email",
		SystemNames:   []string{"postfix"},
		Packages:      map[string][]string{"apt": {"postfix"}, "pacman": {"postfix"}},
		FirewallPorts: []FirewallPort{{25, "tcp"}, {587, "tcp"}, {465, "tcp"}},
	},
	{
		ID:            "dovecot",
		Name:          "Dovecot",
		Description:   "IMAP/POP3 Server",
		Icon:          "📬",
		Category:      "email",
		SystemNames:   []string{"dovecot"},
		Packages:      map[string][]string{"apt": {"dovecot-imapd", "dovecot-pop3d", "dovecot-lmtpd"}, "pacman": {"dovecot"}},
		FirewallPorts: []FirewallPort{{143, "tcp"}, {993, "tcp"}, {110, "tcp"}, {995, "tcp"}},
	},
	{
		ID:          "spamassassin",
		Name:        "SpamAssassin",
		Description: "Spam Filter",
		Icon:        "🛡️",
		Category:    "email",
		SystemNames: []string{"spamassassin"},
		Packages:    map[string][]string{"apt": {"spamassassin"}, "pacman": {"spamassassin"}},
	},
	{
		ID:            "wireguard",
		Name:          "WireGuard VPN",
		Description:   "Built-in VPN server",
		Icon:          "\U0001F510",
		Category:      "security",
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
		SystemNames: []string{"clamav-daemon", "clamav-freshclam"},
		Packages:    map[string][]string{"apt": {"clamav", "clamav-daemon"}, "pacman": {"clamav"}},
	},
	{
		ID:            "bind",
		Name:          "BIND",
		Description:   "DNS Server",
		Icon:          "🌐",
		Category:      "dns",
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
		SystemNames: []string{"memcached"},
		Packages:    map[string][]string{"apt": {"memcached"}, "pacman": {"memcached"}},
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
