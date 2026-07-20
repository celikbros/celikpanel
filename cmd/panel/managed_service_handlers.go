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
	// RequiresMissing: unmet requirements blocking install (service ids or
	// group names) — the UI disables Install and says what to install first.
	// RequiresMissing: kurulumu engelleyen karşılanmamış gereksinimler (servis
	// id'leri ya da grup adları) — UI Kur'u kapatır ve önce neyin kurulacağını söyler.
	RequiresMissing []string `json:"requires_missing,omitempty"`
	// Kind decides how the row is drawn and operated (D-010): "service" has a
	// daemon to start/stop, "runtime" is versioned and picked per site, "tool"
	// has no daemon of ours at all. It replaces the old `daemonless` flag,
	// which was inferred from an empty SystemNames list and therefore marked
	// three unrelated things with one bit.
	// Kind, satırın nasıl çizilip işletileceğini belirler (D-010): "service"
	// başlatılıp durdurulan bir daemon'a sahiptir, "runtime" sürümlüdür ve site
	// başına seçilir, "tool"un bize ait daemon'ı hiç yoktur. Boş SystemNames'ten
	// çıkarılan ve üç ayrı şeyi tek bitle işaretleyen eski `daemonless`
	// bayrağının yerine geçer.
	Kind        core.ServiceKind  `json:"kind"`
	Packages    []string          `json:"packages,omitempty"` // distro packages (apt) shown before install
	ConfigFiles []core.ConfigFile `json:"config_files"`       // Detected config files
}

// managedServicesPayload is what both endpoints return: the cached scan and
// when it ran. A null scanned_at means no scan has ever run.
// managedServicesPayload iki uç noktanın da döndürdüğüdür: önbellekteki
// tarama ve ne zaman koştuğu. scanned_at null ise hiç tarama koşmamıştır.
type managedServicesPayload struct {
	ScannedAt *time.Time               `json:"scanned_at"`
	Services  []ManagedServiceResponse `json:"services"`
}

// serviceObservation is everything a scan can DISCOVER about this host: is the
// package there, is the unit up, which versions exist, where are the configs.
// Catalogue facts — name, description, icon, kind, package names — are
// deliberately absent, because they are not properties of the host. They live
// in code and are re-joined on every read.
//
// This split is the fix for a real bug: the cache used to store whole API
// responses, catalogue fields included. Every catalogue edit then stayed
// invisible until someone happened to press Scan — a service renamed in code
// kept its old name on screen, and a newly added one did not appear at all.
// The Kind field shipped empty on both live servers for exactly this reason.
// Caching a fact that lives in code is how code and screen drift apart.
//
// serviceObservation, bir taramanın bu makine hakkında KEŞFEDEBİLECEĞİ her
// şeydir: paket var mı, unit ayakta mı, hangi sürümler var, config'ler nerede.
// Katalog gerçekleri — ad, açıklama, ikon, kind, paket adları — bilerek yoktur;
// çünkü bunlar makinenin özellikleri değildir. Kodda yaşarlar ve her okumada
// yeniden birleştirilirler.
//
// Bu ayrım gerçek bir hatanın düzeltmesidir: önbellek eskiden katalog alanları
// dahil tüm API yanıtlarını saklıyordu. Böylece her katalog düzeltmesi, biri
// Tara'ya basana dek görünmez kalıyordu — kodda adı değişen servis ekranda eski
// adıyla duruyor, yeni eklenen hiç çıkmıyordu. Kind alanının iki canlı sunucuda
// da boş yayınlanmasının sebebi tam buydu. Kodda yaşayan bir gerçeği
// önbelleğe almak, kod ile ekranın birbirinden ayrı düşme biçimidir.
type serviceObservation struct {
	ID          string            `json:"id"`
	IsInstalled bool              `json:"is_installed"`
	Status      string            `json:"status"`
	Versions    []string          `json:"versions,omitempty"`
	ConfigFiles []core.ConfigFile `json:"config_files,omitempty"`
}

// scanCacheDoc is the persisted shape. An object (not a bare array) so the
// legacy format is told apart by its first byte.
// scanCacheDoc kalıcılaştırılan biçimdir. Eski biçimden ilk baytıyla ayrılsın
// diye çıplak dizi değil nesnedir.
type scanCacheDoc struct {
	Observations []serviceObservation `json:"observations"`
}

// decodeScanCache reads both formats. A row written before the split is a
// JSON array of full responses; its observed fields are still in there, so an
// upgraded panel keeps showing the right state instead of blanking the page
// until the operator reruns a scan.
// decodeScanCache iki biçimi de okur. Ayrımdan önce yazılmış satır, tam
// yanıtlardan oluşan bir JSON dizisidir; gözlem alanları hâlâ içindedir, bu
// yüzden güncellenen panel, operatör yeniden tarama koşturana dek sayfayı
// boşaltmak yerine doğru durumu göstermeyi sürdürür.
func decodeScanCache(data string) []serviceObservation {
	trimmed := strings.TrimSpace(data)
	if strings.HasPrefix(trimmed, "[") {
		var legacy []ManagedServiceResponse
		if json.Unmarshal([]byte(trimmed), &legacy) != nil {
			return nil
		}
		obs := make([]serviceObservation, 0, len(legacy))
		for _, l := range legacy {
			obs = append(obs, serviceObservation{
				ID:          l.ID,
				IsInstalled: l.IsInstalled,
				Status:      l.Status,
				Versions:    l.Versions,
				ConfigFiles: l.ConfigFiles,
			})
		}
		return obs
	}
	var doc scanCacheDoc
	if json.Unmarshal([]byte(trimmed), &doc) != nil {
		return nil
	}
	return doc.Observations
}

// catalogView joins observations onto the catalogue and derives what depends
// on both: install-blocking conflicts and unmet requirements. It is the only
// place a ManagedServiceResponse is built, so the cached read and a fresh scan
// cannot answer differently.
// catalogView, gözlemleri kataloğa birleştirir ve ikisine birden bağlı olanı
// türetir: kurulumu engelleyen çakışmalar ve karşılanmamış gereksinimler.
// ManagedServiceResponse'un kurulduğu tek yer burasıdır; böylece önbellekten
// okuma ile taze tarama farklı yanıt veremez.
func catalogView(obs []serviceObservation, pkgFamily string) []ManagedServiceResponse {
	byID := make(map[string]serviceObservation, len(obs))
	installedSet := map[string]bool{}
	for _, o := range obs {
		byID[o.ID] = o
		if o.IsInstalled {
			installedSet[o.ID] = true
		}
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

	response := make([]ManagedServiceResponse, 0, len(core.ManagedServices))
	for _, managed := range core.ManagedServices {
		o := byID[managed.ID]
		versions := o.Versions
		if versions == nil {
			versions = []string{}
		}
		configFiles := o.ConfigFiles
		if configFiles == nil {
			configFiles = []core.ConfigFile{}
		}
		status := o.Status
		conflictWith := ""
		var requiresMissing []string

		// Not-installed catalogue services are listed too, so the panel can
		// offer a one-click install. They carry status "not_installed"; the UI
		// shows an Install button instead of start/stop/manage.
		// Kurulu-olmayan katalog servisleri de listelenir ki panel tek-tık
		// kurulum sunabilsin. "not_installed" durumu taşırlar; arayüz
		// başlat/durdur/yönet yerine Kur düğmesi gösterir.
		if !o.IsInstalled {
			status = "not_installed"
			// Blocked only if the group's installed member is someone else.
			// Yalnız grubun kurulu üyesi bir başkasıysa engellenir.
			if managed.ConflictGroup != "" {
				if owner, ok := groupOwner[managed.ConflictGroup]; ok && owner != managed.Name {
					conflictWith = owner
				}
			}
			requiresMissing = core.RequirementsMissing(&managed, installedSet)
		} else if len(versions) == 0 {
			versions = append(versions, "default")
		}

		response = append(response, ManagedServiceResponse{
			ID:              managed.ID,
			Name:            managed.Name,
			Description:     managed.Description,
			Icon:            managed.Icon,
			Category:        managed.Category,
			Versions:        versions,
			Status:          status,
			IsInstalled:     o.IsInstalled,
			ConflictWith:    conflictWith,
			RequiresMissing: requiresMissing,
			Kind:            managed.Kind,
			Packages:        managed.Packages[pkgFamily],
			ConfigFiles:     configFiles,
		})
	}
	return response
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
		// The catalogue is joined on at read time, so an upgraded panel tells
		// the truth about its own catalogue immediately — no rescan needed to
		// see a renamed service, a new one, or a corrected description.
		// Katalog okuma anında birleştirilir; böylece güncellenen panel kendi
		// kataloğu hakkında anında doğruyu söyler — adı değişen bir servisi,
		// yenisini ya da düzeltilmiş bir açıklamayı görmek için tarama gerekmez.
		payload.Services = catalogView(decodeScanCache(data), p.packageFamily())
	}

	json.NewEncoder(w).Encode(payload)
}

// packageFamily returns the host's package-manager family, asked from the
// agent once and kept. This is the one cheap fact the cached GET may fetch:
// it is a single RPC that reads the distro id, not the system-wide probe the
// cache exists to avoid. A failed call answers "apt" without memoising it, so
// a momentarily-down agent cannot freeze the wrong answer for the process's
// lifetime.
// packageFamily, makinenin paket-yöneticisi ailesini döndürür; agent'a bir kez
// sorulup saklanır. Önbellekli GET'in çekmesine izin verilen tek ucuz gerçek
// budur: dağıtım kimliğini okuyan tek bir RPC'dir, önbelleğin var olma sebebi
// olan sistem geneli yoklama değil. Başarısız çağrı, belleğe yazmadan "apt"
// yanıtlar; böylece anlık düşmüş bir agent yanlış yanıtı süreç boyunca
// dondurmaz.
func (p *Panel) packageFamily() string {
	p.pkgFamilyMu.Lock()
	defer p.pkgFamilyMu.Unlock()
	if p.pkgFamilyVal != "" {
		return p.pkgFamilyVal
	}
	var fam string
	if err := p.agentClient.Call("Agent.PkgFamily", &transport.Empty{}, &fam); err == nil && fam != "" {
		p.pkgFamilyVal = fam
		return fam
	}
	return "apt"
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
	// This loop observes the host and nothing else. What the catalogue says
	// about each service is added later, on every read, by catalogView.
	// Bu döngü yalnız makineyi gözler, başka bir şey değil. Kataloğun her
	// servis hakkında söyledikleri sonradan, her okumada, catalogView tarafından
	// eklenir.
	observations := make([]serviceObservation, 0, len(core.ManagedServices))
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

		// A present package counts as installed even if no unit is running
		// yet (WireGuard before its first config, a stopped service…).
		// Paket varsa, henüz çalışan unit olmasa da kurulu sayılır.
		if !isInstalled && installedSet[managed.ID] {
			isInstalled = true
			status = "inactive (dead)"
		}

		observations = append(observations, serviceObservation{
			ID:          managed.ID,
			IsInstalled: isInstalled,
			Status:      status,
			Versions:    versions,
			ConfigFiles: configFiles,
		})
	}

	data, err := json.Marshal(scanCacheDoc{Observations: observations})
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
	return catalogView(observations, p.packageFamily()), nil
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
