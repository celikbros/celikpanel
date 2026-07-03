package core

// ManagedService represents a service that CelikPanel can manage
type ManagedService struct {
	ID          string   // Unique identifier (e.g., "php-fpm", "nginx")
	Name        string   // Display name
	Description string   // Short description
	Icon        string   // Emoji or icon identifier
	Category    string   // "web", "database", "email", "security", "dns", "cache"
	SystemNames []string // Systemd service names to check
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
	},
	{
		ID:          "nginx",
		Name:        "Nginx",
		Description: "Reverse Proxy Server",
		Icon:        "🔄",
		Category:    "web",
		SystemNames: []string{"nginx"},
	},
	{
		ID:          "apache",
		Name:        "Apache",
		Description: "Web Server",
		Icon:        "🪶",
		Category:    "web",
		SystemNames: []string{"apache2"},
	},
	{
		ID:          "postgresql",
		Name:        "PostgreSQL",
		Description: "Database Server",
		Icon:        "🐘",
		Category:    "database",
		SystemNames: []string{"postgresql"},
	},
	{
		ID:          "mariadb",
		Name:        "MariaDB",
		Description: "Database Server",
		Icon:        "🦭",
		Category:    "database",
		SystemNames: []string{"mariadb", "mysql"},
	},
	{
		ID:          "postfix",
		Name:        "Postfix",
		Description: "SMTP Server",
		Icon:        "📧",
		Category:    "email",
		SystemNames: []string{"postfix"},
	},
	{
		ID:          "dovecot",
		Name:        "Dovecot",
		Description: "IMAP/POP3 Server",
		Icon:        "📬",
		Category:    "email",
		SystemNames: []string{"dovecot"},
	},
	{
		ID:          "spamassassin",
		Name:        "SpamAssassin",
		Description: "Spam Filter",
		Icon:        "🛡️",
		Category:    "email",
		SystemNames: []string{"spamassassin"},
	},
	{
		ID:          "fail2ban",
		Name:        "Fail2ban",
		Description: "Intrusion Prevention",
		Icon:        "🚫",
		Category:    "security",
		SystemNames: []string{"fail2ban"},
	},
	{
		ID:          "bind",
		Name:        "BIND",
		Description: "DNS Server",
		Icon:        "🌐",
		Category:    "dns",
		SystemNames: []string{"bind9", "named"},
	},
	{
		ID:          "pdns",
		Name:        "PowerDNS",
		Description: "DNS Server",
		Icon:        "⚡",
		Category:    "dns",
		SystemNames: []string{"pdns"},
	},
	{
		ID:          "vsftpd",
		Name:        "vsftpd",
		Description: "FTP Server",
		Icon:        "📂",
		Category:    "ftp",
		SystemNames: []string{"vsftpd"},
	},
	{
		ID:          "redis",
		Name:        "Redis",
		Description: "Cache Server",
		Icon:        "⚡",
		Category:    "cache",
		SystemNames: []string{"redis-server", "redis"},
	},
	{
		ID:          "memcached",
		Name:        "Memcached",
		Description: "Cache Server",
		Icon:        "💾",
		Category:    "cache",
		SystemNames: []string{"memcached"},
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
