package systemd

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/services"
)

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

// ListServices scans for known services
func (m *Manager) ListServices() ([]core.Service, error) {
	// We are looking for specific services
	targets := []string{"nginx", "php", "postgresql", "mysql", "mariadb", "apache2", "caddy",
		"certbot", "vsftpd", "fail2ban", "postfix", "dovecot", "spamassassin", "pdns", "wg-quick"}

	services := make([]core.Service, 0)

	// Patterns to match
	patterns := []string{"nginx*", "php*", "postgresql*", "mysql*", "mariadb*", "apache2*", "caddy*",
		// spamd*: Debian 13's SpamAssassin daemon unit. Third proven miss of
		// this hardcoded list (pdns config Jul 10, sleeping bind Jul 16,
		// spamd Jul 23) — the B3 remainder replaces it with patterns derived
		// from the catalog, the single owner of unit names.
		// spamd*: Debian 13'ün SpamAssassin daemon unit'i. Bu elle yazılmış
		// listenin kanıtlı ÜÇÜNCÜ kaçırması (pdns config 10 Tem, uyuyan bind
		// 16 Tem, spamd 23 Tem) — B3 kalanı bu listeyi, unit adlarının tek
		// sahibi olan katalogdan türetilen desenlerle değiştirecek.
		"certbot*", "vsftpd*", "fail2ban*", "postfix*", "dovecot*", "spamassassin*", "spamd*", "pdns*", "wg-quick*"}

	// Get specific active and inactive units
	args := []string{"list-units", "--type=service", "--all", "--no-legend", "--no-pager"}
	args = append(args, patterns...)

	cmd := exec.Command("systemctl", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list units: %v", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		unitName := fields[0]    // e.g., nginx.service
		activeState := fields[2] // active
		subState := fields[3]    // running

		// Check if this unit matches one of our targets
		name := strings.TrimSuffix(unitName, ".service")

		var serviceType core.ServiceType
		matched := false

		for _, t := range targets {
			if strings.Contains(name, t) {
				matched = true
				switch {
				case strings.Contains(name, "nginx") || strings.Contains(name, "apache") || strings.Contains(name, "caddy"):
					serviceType = core.ServiceNginx // Generic web server for now
				case strings.Contains(name, "php"):
					serviceType = core.ServicePHP
				case strings.Contains(name, "postgres"):
					serviceType = core.ServicePostgres
				case strings.Contains(name, "mysql") || strings.Contains(name, "mariadb"):
					serviceType = core.ServiceMySQL
				default:
					serviceType = core.ServiceSystemd
				}
				break
			}
		}

		if matched {
			// Get version
			version := m.getServiceVersion(name, string(serviceType))

			// Get config files
			configFiles := m.getConfigFiles(name, string(serviceType))

			// Determine if this is a primary service (not a helper)
			isPrimary := m.isPrimaryService(name)

			s := core.Service{
				ID:          name,
				Name:        name,
				Type:        serviceType,
				Status:      fmt.Sprintf("%s (%s)", activeState, subState),
				Version:     version,
				ConfigFiles: configFiles,
				IsPrimary:   isPrimary,
			}

			services = append(services, s)
		}
	}

	// Ensure we never return nil
	if services == nil {
		services = []core.Service{}
	}

	return services, nil
}

func (m *Manager) getServiceVersion(name string, serviceType string) string {
	// Use generic version detector
	detector := services.NewVersionDetector()
	return detector.DetectVersion(name)
}
func (m *Manager) getConfigFiles(name string, serviceType string) []core.ConfigFile {
	// Use ServiceScanner for intelligent discovery
	scanner := services.NewServiceScanner()
	configs, err := scanner.ScanService(name)
	if err != nil || len(configs) == 0 {
		// Fallback to empty if scan fails
		return []core.ConfigFile{}
	}

	return configs
}

func (m *Manager) isPrimaryService(name string) bool {
	// Helper services that should be filtered out by default
	helpers := []string{
		"phpsessionclean",
		"postgresql", // Generic postgresql (we want postgresql@VERSION)
	}

	for _, helper := range helpers {
		if name == helper {
			return false
		}
	}

	return true
}

func (m *Manager) Reload(serviceName string) error {
	out, err := exec.Command("systemctl", "reload", serviceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl reload failed: %v, output: %s", err, string(out))
	}
	return nil
}

func (m *Manager) Restart(serviceName string) error {
	out, err := exec.Command("systemctl", "restart", serviceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart failed: %v, output: %s", err, string(out))
	}
	return nil
}

func (m *Manager) Start(serviceName string) error {
	out, err := exec.Command("systemctl", "start", serviceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl start failed: %v, output: %s", err, string(out))
	}
	return nil
}

func (m *Manager) Stop(serviceName string) error {
	cmd := exec.Command("systemctl", "stop", serviceName)
	out, err := cmd.CombinedOutput()

	// Log the output regardless of error
	if len(out) > 0 {
		fmt.Printf("systemctl stop %s output: %s\n", serviceName, string(out))
	}

	if err != nil {
		return fmt.Errorf("systemctl stop failed: %v, output: %s", err, string(out))
	}
	return nil
}
