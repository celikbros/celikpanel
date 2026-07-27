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

// ListServices scans for the units of every catalogue service. Both the
// systemctl glob (what to fetch) and the ownership test (what a unit belongs
// to) come from the catalogue via core.ServiceScanGlobs / core.ServiceForUnit
// — there is no hand-written unit list to keep in sync. That hand-written
// list (serviceBases) shipped the same silent bug three times: spamd, then
// rspamd, then netdata were installed and running while the panel showed
// "stopped" because the scanner never fetched their units. Add a service to
// the catalogue and the scanner sees it; the class of bug is gone.
//
// ListServices, her katalog servisinin unit'lerini tarar. Hem systemctl glob'u
// (neyi çekeceği) hem sahiplik testi (bir unit'in neye ait olduğu) katalogdan
// gelir — core.ServiceScanGlobs / core.ServiceForUnit — eşzamanlı tutulacak
// elle yazılmış unit listesi yoktur. O elle liste (serviceBases) aynı sessiz
// hatayı üç kez gönderdi: spamd, sonra rspamd, sonra netdata kurulu ve
// çalışırken panel "durdu" gösterdi çünkü tarayıcı unit'lerini hiç çekmedi.
// Kataloğa bir servis ekle, tarayıcı görür; hata sınıfı bitti.
func (m *Manager) ListServices() ([]core.Service, error) {
	services := make([]core.Service, 0)

	// Get specific active and inactive units
	args := []string{"list-units", "--type=service", "--all", "--no-legend", "--no-pager"}
	args = append(args, core.ServiceScanGlobs()...)

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

		name := strings.TrimSuffix(unitName, ".service")

		// The catalogue decides ownership — a unit the glob over-collected but
		// no service claims is skipped.
		// Sahipliğe katalog karar verir — glob'un fazladan topladığı ama hiçbir
		// servisin sahiplenmediği unit atlanır.
		owner := core.ServiceForUnit(name)
		if owner == nil {
			continue
		}

		serviceType := serviceTypeForService(owner)

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

	// Ensure we never return nil
	if services == nil {
		services = []core.Service{}
	}

	return services, nil
}

// serviceTypeForService maps a catalogue service to the coarse ServiceType the
// version/config detectors branch on. Keyed by catalogue id, it replaces the
// old name-substring switch — the id is exact, so php-fpm can never be
// misread as a generic web server the way `Contains(name, "php")` once could.
// serviceTypeForService, bir katalog servisini sürüm/config tespitçilerinin
// dallandığı kaba ServiceType'a eşler. Katalog id'siyle anahtarlanır, eski
// ad-alt-dize switch'inin yerine geçer — id kesindir, bu yüzden php-fpm eskiden
// `Contains(name, "php")`'in yapabildiği gibi genel bir web sunucusu diye
// yanlış okunamaz.
func serviceTypeForService(svc *core.ManagedService) core.ServiceType {
	switch svc.ID {
	case "php-fpm":
		return core.ServicePHP
	case "nginx", "apache":
		return core.ServiceNginx
	case "postgresql":
		return core.ServicePostgres
	case "mariadb":
		return core.ServiceMySQL
	default:
		return core.ServiceSystemd
	}
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
	return runUnitChange(serviceName, true, "restart", serviceName)
}

func (m *Manager) Start(serviceName string) error {
	return runUnitChange(serviceName, true, "start", serviceName)
}

func (m *Manager) Stop(serviceName string) error {
	return runUnitChange(serviceName, false, "stop", serviceName)
}
