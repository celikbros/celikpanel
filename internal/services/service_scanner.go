package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

// ServiceScanner auto-discovers service configuration paths
type ServiceScanner struct{}

func NewServiceScanner() *ServiceScanner {
	return &ServiceScanner{}
}

// ScanService discovers configuration files for a given service
func (s *ServiceScanner) ScanService(serviceName string) ([]core.ConfigFile, error) {
	var configs []core.ConfigFile

	// Try multiple discovery methods
	configs = append(configs, s.discoverFromSystemctl(serviceName)...)
	configs = append(configs, s.discoverFromCommonPaths(serviceName)...)
	
	// Deduplicate
	seen := make(map[string]bool)
	unique := []core.ConfigFile{}
	for _, cfg := range configs {
		if !seen[cfg.Path] {
			seen[cfg.Path] = true
			// Verify file exists
			if _, err := os.Stat(cfg.Path); err == nil {
				unique = append(unique, cfg)
			}
		}
	}
	
	return unique, nil
}

// discoverFromSystemctl uses systemctl show to find config paths
func (s *ServiceScanner) discoverFromSystemctl(serviceName string) []core.ConfigFile {
	var configs []core.ConfigFile
	
	cmd := exec.Command("systemctl", "show", serviceName, "--property=ExecStart")
	output, err := cmd.Output()
	if err != nil {
		return configs
	}
	
	// Parse ExecStart line to find --config or -c flags
	execLine := strings.TrimSpace(string(output))
	if strings.Contains(execLine, "--config") || strings.Contains(execLine, "-c") {
		fields := strings.Fields(execLine)
		for i, field := range fields {
			if (field == "--config" || field == "-c") && i+1 < len(fields) {
				configPath := fields[i+1]
				configs = append(configs, core.ConfigFile{
					Path:      configPath,
					IsManaged: true,
				})
			}
		}
	}
	
	return configs
}

// discoverFromCommonPaths searches common configuration directories
func (s *ServiceScanner) discoverFromCommonPaths(serviceName string) []core.ConfigFile {
	var configs []core.ConfigFile
	
	// Service-specific search paths
	searchPaths := s.getSearchPaths(serviceName)
	
	for _, searchPath := range searchPaths {
		// Check if path exists
		if _, err := os.Stat(searchPath); err == nil {
			configs = append(configs, core.ConfigFile{
				Path:      searchPath,
				IsManaged: true,
			})
		}
	}
	
	return configs
}

// getSearchPaths returns common config paths for known services
func (s *ServiceScanner) getSearchPaths(serviceName string) []string {
	paths := []string{}
	
	if strings.Contains(serviceName, "nginx") {
		paths = []string{
			"/etc/nginx/nginx.conf",
			"/usr/local/nginx/conf/nginx.conf",
			"/opt/nginx/conf/nginx.conf",
		}
		// Also scan sites-enabled
		if _, err := os.Stat("/etc/nginx/sites-enabled"); err == nil {
			matches, _ := filepath.Glob("/etc/nginx/sites-enabled/*")
			paths = append(paths, matches...)
		}
	} else if strings.Contains(serviceName, "php") && strings.Contains(serviceName, "fpm") {
		// Extract version
		version := s.extractPHPVersion(serviceName)
		paths = []string{
			fmt.Sprintf("/etc/php/%s/fpm/php-fpm.conf", version),
			fmt.Sprintf("/etc/php/%s/fpm/pool.d/www.conf", version),
			fmt.Sprintf("/usr/local/etc/php-fpm.d/www.conf"),
		}
	} else if strings.Contains(serviceName, "mariadb") || strings.Contains(serviceName, "mysql") {
		paths = []string{
			"/etc/mysql/my.cnf",
			"/etc/my.cnf",
			"/etc/mysql/mariadb.cnf",
			"/etc/mysql/mariadb.conf.d/50-server.cnf",
			"/usr/local/mysql/my.cnf",
		}
	} else if strings.Contains(serviceName, "postgres") {
		// Try to find active cluster
		paths = []string{
			"/etc/postgresql/16/main/postgresql.conf",
			"/etc/postgresql/15/main/postgresql.conf",
			"/etc/postgresql/14/main/postgresql.conf",
		}
		// Also check for pg_hba.conf
		for i, path := range paths {
			if strings.HasSuffix(path, "postgresql.conf") {
				dir := filepath.Dir(path)
				paths = append(paths, filepath.Join(dir, "pg_hba.conf"))
			}
			_ = i
		}
	} else if strings.Contains(serviceName, "certbot") {
		paths = []string{
			"/etc/letsencrypt/cli.ini",
			"/etc/letsencrypt/renewal",
		}
	} else if strings.Contains(serviceName, "pdns") {
		// The panel writes its backend config as a drop-in; list both the
		// distro main file and our drop-in so the manage page shows what
		// is actually in effect.
		// Panel arka uç yapılandırmasını drop-in olarak yazar; yönetim
		// sayfası fiilen geçerli olanı gösterebilsin diye hem dağıtımın
		// ana dosyası hem bizim drop-in listelenir.
		paths = []string{
			"/etc/powerdns/pdns.conf",
			"/etc/powerdns/pdns.d/celikpanel.conf",
		}
	} else if strings.Contains(serviceName, "vsftpd") {
		paths = []string{
			"/etc/vsftpd.conf",
			"/etc/vsftpd/vsftpd.conf",
		}
	} else if strings.Contains(serviceName, "fail2ban") {
		paths = []string{
			"/etc/fail2ban/fail2ban.conf",
			"/etc/fail2ban/jail.conf",
		}
	} else if strings.Contains(serviceName, "postfix") {
		paths = []string{
			"/etc/postfix/main.cf",
			"/etc/postfix/master.cf",
		}
	} else if strings.Contains(serviceName, "dovecot") {
		paths = []string{
			"/etc/dovecot/dovecot.conf",
		}
	}
	
	return paths
}

// extractPHPVersion extracts version from service name (e.g., php8.3-fpm -> 8.3).
// A bare "php-fpm" name carries no version, so fall back to what is actually
// installed on this host rather than a constant.
// extractPHPVersion, sürümü servis adından çıkarır (php8.3-fpm → 8.3). Yalın
// "php-fpm" adı sürüm taşımaz; sabit yerine makinede gerçekten kurulu olana düş.
func (s *ServiceScanner) extractPHPVersion(serviceName string) string {
	parts := strings.Split(serviceName, "-")
	if len(parts) > 0 && strings.HasPrefix(parts[0], "php") && parts[0] != "php" {
		return strings.TrimPrefix(parts[0], "php")
	}
	if v := DetectInstalledPHPVersion(); v != "" {
		return v
	}
	return "8.3" // default
}
