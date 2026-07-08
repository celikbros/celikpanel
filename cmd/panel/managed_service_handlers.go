package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ManagedServiceResponse represents a managed service with runtime status
type ManagedServiceResponse struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Icon         string            `json:"icon"`
	Category     string            `json:"category"`
	Versions     []string          `json:"versions"` // Detected versions
	Status       string            `json:"status"`   // Overall status
	IsInstalled  bool              `json:"is_installed"`
	ConflictWith string            `json:"conflict_with,omitempty"` // installed member of the same conflict group
	Packages     []string          `json:"packages,omitempty"`      // distro packages (apt) shown before install
	ConfigFiles  []core.ConfigFile `json:"config_files"`            // Detected config files
}

// managedServicesPayload is what both endpoints return: the cached scan and
// when it ran. A null scanned_at means no scan has ever run.
// managedServicesPayload iki uç noktanın da döndürdüğüdür: önbellekteki
// tarama ve ne zaman koştuğu. scanned_at null ise hiç tarama koşmamıştır.
type managedServicesPayload struct {
	ScannedAt *time.Time               `json:"scanned_at"`
	Services  []ManagedServiceResponse `json:"services"`
}

// handleManagedServices serves the CACHED scan only — opening a page must
// never probe the whole system (a dozen units × version execs × config
// scans made every navigation slow). A fresh probe is an explicit user
// action: POST /api/v1/managed-services/scan.
// handleManagedServices YALNIZ önbellekteki taramayı sunar — bir sayfayı
// açmak asla tüm sistemi yoklamamalı (bir düzine unit × sürüm çalıştırması ×
// config taraması her gezinmeyi yavaşlatıyordu). Taze yoklama açık bir
// kullanıcı eylemidir: POST /api/v1/managed-services/scan.
func (p *Panel) handleManagedServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	payload := managedServicesPayload{Services: []ManagedServiceResponse{}}

	var data string
	var scannedAt string
	err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`).Scan(&data, &scannedAt)
	if err == nil {
		if t, terr := time.Parse(time.RFC3339, scannedAt); terr == nil {
			payload.ScannedAt = &t
		}
		_ = json.Unmarshal([]byte(data), &payload.Services)
	}

	json.NewEncoder(w).Encode(payload)
}

// handleManagedServicesScan runs a fresh scan on user request, caches it and
// returns the same payload shape as the GET.
// handleManagedServicesScan, kullanıcı isteğiyle taze bir tarama koşar,
// önbelleğe alır ve GET ile aynı yükü döndürür.
func (p *Panel) handleManagedServicesScan(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	services, err := p.scanManagedServices(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}

	now := time.Now().UTC()
	json.NewEncoder(w).Encode(managedServicesPayload{ScannedAt: &now, Services: services})
}

// scanManagedServices asks the agent for the real system state, folds it
// into the curated catalogue and persists the result.
// scanManagedServices, agent'tan gerçek sistem durumunu ister, seçili
// kataloğa işler ve sonucu kalıcılaştırır.
func (p *Panel) scanManagedServices(ctx context.Context) ([]ManagedServiceResponse, error) {
	var allServices []core.Service
	if err := p.agentClient.Call("Agent.GetServices", &transport.Empty{}, &allServices); err != nil {
		return nil, err
	}

	// Which catalogue packages are present (installed but maybe not running).
	// Hangi katalog paketleri var (kurulu ama belki çalışmıyor).
	var installedIDs []string
	_ = p.agentClient.Call("Agent.InstalledServiceIDs", &transport.Empty{}, &installedIDs)
	installedSet := map[string]bool{}
	for _, id := range installedIDs {
		installedSet[id] = true
	}
	// Conflict groups: which group already has an installed member, and who.
	// Çakışma grupları: hangi grupta zaten kurulu üye var ve kim.
	groupOwner := map[string]string{}
	for i := range core.ManagedServices {
		m := &core.ManagedServices[i]
		if m.ConflictGroup != "" && installedSet[m.ID] {
			groupOwner[m.ConflictGroup] = m.Name
		}
	}

	response := make([]ManagedServiceResponse, 0)
	for _, managed := range core.ManagedServices {
		versions := []string{}
		configFiles := []core.ConfigFile{}
		isInstalled := false
		status := "inactive"
		anyRunning := false

		for _, svc := range allServices {
			for _, systemName := range managed.SystemNames {
				if svc.Name != systemName {
					continue
				}
				isInstalled = true

				version := extractVersion(svc.Name, managed.ID)
				if version != "" && !contains(versions, version) {
					versions = append(versions, version)
				}

				if len(svc.ConfigFiles) > 0 {
					configFiles = append(configFiles, svc.ConfigFiles...)
				}

				// "active (running)" for daemons; oneshot units like
				// wg-quick@wg0 report "active (exited)" — both are up.
				// Daemon'larda "active (running)"; wg-quick@wg0 gibi oneshot
				// unit'ler "active (exited)" bildirir — ikisi de ayaktadır.
				statusLower := strings.ToLower(svc.Status)
				if strings.HasPrefix(statusLower, "active") {
					anyRunning = true
				}
			}
		}

		if anyRunning {
			status = "active (running)"
		} else if isInstalled {
			status = "inactive (dead)"
		}

		// Not-installed catalogue services are included too, so the panel
		// can offer a one-click install. They carry status "not_installed";
		// the UI shows an Install button instead of start/stop/manage.
		// Kurulu-olmayan katalog servisleri de dahildir ki panel tek-tık
		// kurulum sunabilsin. "not_installed" durumu taşırlar; arayüz
		// başlat/durdur/yönet yerine Kur düğmesi gösterir.
		// A present package counts as installed even if no unit is running
		// yet (WireGuard before its first config, a stopped service…).
		// Paket varsa, henüz çalışan unit olmasa da kurulu sayılır.
		if !isInstalled && installedSet[managed.ID] {
			isInstalled = true
			status = "inactive (dead)"
		}

		conflictWith := ""
		if !isInstalled {
			status = "not_installed"
			// Blocked only if the group's installed member is someone else.
			// Yalnız grubun kurulu üyesi bir başkasıysa engellenir.
			if managed.ConflictGroup != "" {
				if owner, ok := groupOwner[managed.ConflictGroup]; ok && owner != managed.Name {
					conflictWith = owner
				}
			}
		} else if len(versions) == 0 {
			versions = append(versions, "default")
		}

		response = append(response, ManagedServiceResponse{
			ID:           managed.ID,
			Name:         managed.Name,
			Description:  managed.Description,
			Icon:         managed.Icon,
			Category:     managed.Category,
			Versions:     versions,
			Status:       status,
			IsInstalled:  isInstalled,
			ConflictWith: conflictWith,
			Packages:     managed.Packages["apt"],
			ConfigFiles:  configFiles,
		})
	}

	data, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	_, err = p.db.GetDB().ExecContext(ctx, `
		INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data, scanned_at = excluded.scanned_at`,
		string(data), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return response, nil
}

// extractVersion extracts version number from service name
func extractVersion(serviceName, serviceID string) string {
	switch serviceID {
	case "php-fpm":
		// php8.4-fpm -> 8.4
		if strings.HasPrefix(serviceName, "php") && strings.HasSuffix(serviceName, "-fpm") {
			version := strings.TrimPrefix(serviceName, "php")
			version = strings.TrimSuffix(version, "-fpm")
			return version
		}
	case "postgresql":
		// For postgresql, we might need to query version differently
		// For now, return empty to use "default"
		return ""
	}
	return ""
}

// contains checks if a string slice contains a value
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
