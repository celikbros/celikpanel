package core

// FirewallPort is one inbound port a service needs open, with its protocol.
// FirewallPort, bir servisin açık olmasını istediği bir gelen port ve protokolü.
type FirewallPort struct {
	Port  int    // e.g. 443
	Proto string // "tcp" or "udp"
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
	// packages — a fixed whitelist, never an arbitrary name. Only "apt"
	// (Ubuntu/Debian) is filled and tested today; other families return an
	// honest "not supported on this distro yet" instead of guessing names.
	//
	// Packages, bir paket-yöneticisi ailesini ("apt", "dnf", "pacman") bu
	// servisi kuran dağıtım paketlerine eşler. Panel kurulum anında hiçbir şey
	// kurmaz (anayasa: kurulu olmayan görünmez); yönetici servisi talep
	// üzerine kurar ve agent tam olarak bu paketleri kurar — sabit whitelist,
	// asla keyfi ad değil. Bugün yalnız "apt" (Ubuntu/Debian) dolu ve test
	// edilmiştir; diğer aileler ad tahmin etmek yerine dürüst "bu dağıtımda
	// henüz desteklenmiyor" döndürür.
	Packages map[string][]string
}

// ManagedServices is the list of services CelikPanel manages
var ManagedServices = []ManagedService{
	{
		ID:          "php-fpm",
		Name:        "PHP-FPM",
		Description: "PHP FastCGI Process Manager",
		Icon:        "🐘",
		Category:    "web",
		SystemNames: []string{"php8.4-fpm", "php8.3-fpm", "php8.2-fpm", "php8.1-fpm", "php8.0-fpm"},
		Packages:    map[string][]string{"apt": {"php-fpm"}},
	},
	{
		ID:            "nginx",
		Name:          "Nginx",
		Description:   "Reverse Proxy Server",
		Icon:          "🔄",
		Category:      "web",
		SystemNames:   []string{"nginx"},
		ConflictGroup: "web-server",
		Packages:      map[string][]string{"apt": {"nginx"}},
		FirewallPorts: []FirewallPort{{80, "tcp"}, {443, "tcp"}},
	},
	{
		ID:            "apache",
		Name:          "Apache",
		Description:   "Web Server",
		Icon:          "🪶",
		Category:      "web",
		SystemNames:   []string{"apache2"},
		ConflictGroup: "web-server",
		Packages:      map[string][]string{"apt": {"apache2"}},
		FirewallPorts: []FirewallPort{{80, "tcp"}, {443, "tcp"}},
	},
	{
		ID:          "postgresql",
		Name:        "PostgreSQL",
		Description: "Database Server",
		Icon:        "🐘",
		Category:    "database",
		SystemNames: []string{"postgresql"},
		Packages:    map[string][]string{"apt": {"postgresql"}},
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
	{
		ID:            "postfix",
		Name:          "Postfix",
		Description:   "SMTP Server",
		Icon:          "📧",
		Category:      "email",
		SystemNames:   []string{"postfix"},
		Packages:      map[string][]string{"apt": {"postfix"}},
		FirewallPorts: []FirewallPort{{25, "tcp"}, {587, "tcp"}, {465, "tcp"}},
	},
	{
		ID:            "dovecot",
		Name:          "Dovecot",
		Description:   "IMAP/POP3 Server",
		Icon:          "📬",
		Category:      "email",
		SystemNames:   []string{"dovecot"},
		Packages:      map[string][]string{"apt": {"dovecot-imapd", "dovecot-pop3d", "dovecot-lmtpd"}},
		FirewallPorts: []FirewallPort{{143, "tcp"}, {993, "tcp"}, {110, "tcp"}, {995, "tcp"}},
	},
	{
		ID:          "spamassassin",
		Name:        "SpamAssassin",
		Description: "Spam Filter",
		Icon:        "🛡️",
		Category:    "email",
		SystemNames: []string{"spamassassin"},
		Packages:    map[string][]string{"apt": {"spamassassin"}},
	},
	{
		ID:            "wireguard",
		Name:          "WireGuard VPN",
		Description:   "Built-in VPN server",
		Icon:          "\U0001F510",
		Category:      "security",
		SystemNames:   []string{"wg-quick@wg0"},
		Packages:      map[string][]string{"apt": {"wireguard"}},
		FirewallPorts: []FirewallPort{{51820, "udp"}},
	},
	{
		ID:          "fail2ban",
		Name:        "Fail2ban",
		Description: "Intrusion Prevention",
		Icon:        "🚫",
		Category:    "security",
		SystemNames: []string{"fail2ban"},
		Packages:    map[string][]string{"apt": {"fail2ban"}},
	},
	{
		ID:            "bind",
		Name:          "BIND",
		Description:   "DNS Server",
		Icon:          "🌐",
		Category:      "dns",
		SystemNames:   []string{"bind9", "named"},
		ConflictGroup: "dns-server",
		Packages:      map[string][]string{"apt": {"bind9"}},
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
		Packages:      map[string][]string{"apt": {"pdns-server", "pdns-backend-sqlite3"}},
		FirewallPorts: []FirewallPort{{53, "tcp"}, {53, "udp"}},
	},
	{
		ID:            "vsftpd",
		Name:          "vsftpd",
		Description:   "FTP Server",
		Icon:          "📂",
		Category:      "ftp",
		SystemNames:   []string{"vsftpd"},
		Packages:      map[string][]string{"apt": {"vsftpd"}},
		FirewallPorts: []FirewallPort{{21, "tcp"}},
	},
	{
		ID:          "redis",
		Name:        "Redis",
		Description: "Cache Server",
		Icon:        "⚡",
		Category:    "cache",
		SystemNames: []string{"redis-server", "redis"},
		Packages:    map[string][]string{"apt": {"redis-server"}},
	},
	{
		ID:          "memcached",
		Name:        "Memcached",
		Description: "Cache Server",
		Icon:        "💾",
		Category:    "cache",
		SystemNames: []string{"memcached"},
		Packages:    map[string][]string{"apt": {"memcached"}},
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
