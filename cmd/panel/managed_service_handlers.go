package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ManagedServiceResponse represents a managed service with runtime status
type ManagedServiceResponse struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Icon         string   `json:"icon"`
	Category     string   `json:"category"`
	Versions     []string `json:"versions"` // Detected versions
	Status       string   `json:"status"`   // Overall status
	IsInstalled  bool     `json:"is_installed"`
	ConflictWith string   `json:"conflict_with,omitempty"` // installed member of the same conflict group
	// RequiresMissing: unmet requirements blocking install (service ids or
	// group names) — the UI disables Install and says what to install first.
	// RequiresMissing: kurulumu engelleyen karşılanmamış gereksinimler (servis
	// id'leri ya da grup adları) — UI Kur'u kapatır ve önce neyin kurulacağını söyler.
	RequiresMissing []string `json:"requires_missing,omitempty"`
	// NotOffered: this component installs from distro packages and has no
	// package mapping for THIS server's family — deliberately not offered here
	// ("installed means working" cannot be promised; see docs/DISTRO-SUPPORT).
	// The UI shows an honest badge instead of an Install button that would
	// only fail late in the agent. Portable components (empty Packages map —
	// node, roundcube) are never marked: their install path works everywhere.
	// NotOffered: bu bileşen dağıtım paketinden kurulur ve BU sunucunun ailesi
	// için paket eşlemesi yok — burada bilerek sunulmuyor ("kurulunca çalışır"
	// sözü verilemiyor; bkz. docs/DISTRO-SUPPORT). Arayüz, agent'ta geç
	// patlayacak bir Kur düğmesi yerine dürüst bir rozet gösterir. Taşınabilir
	// bileşenler (boş Packages — node, roundcube) asla işaretlenmez: onların
	// kurulum yolu her yerde çalışır.
	NotOffered       bool   `json:"not_offered,omitempty"`
	NotOfferedReason string `json:"not_offered_reason,omitempty"`
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
	Kind core.ServiceKind `json:"kind"`
	// Unit: the real systemd unit behind this row, as found by the scan. The
	// UI's start/stop/restart targets THIS, not the id (BIND's id is "bind",
	// its unit is "named").
	// Unit: bu satırın arkasındaki gerçek systemd unit'i, taramanın bulduğu
	// hâliyle. Arayüzün başlat/durdur/yeniden başlatı BUNU hedefler, id'yi
	// değil (BIND'in id'si "bind", unit'i "named").
	Unit string `json:"unit,omitempty"`
	// Instances: the per-copy truth behind a runtime row (B3b) — one entry
	// per installed version, with unit/status/path/size. The version drawer
	// renders these; Versions above is just their version strings.
	// Instances: runtime satırının arkasındaki kopya-başına gerçek (B3b) —
	// kurulu sürüm başına bir kayıt: unit/durum/yol/boyut. Sürüm çekmecesi
	// bunları çizer; yukarıdaki Versions yalnız sürüm dizeleridir.
	Instances   []core.ServiceInstance `json:"instances"`
	Packages    []string               `json:"packages,omitempty"` // distro packages (apt) shown before install
	ConfigFiles []core.ConfigFile      `json:"config_files"`       // Detected config files
	// Ports: the inbound ports this component exposes ("443/tcp"), from the
	// catalogue. The firewall already opens exactly these on install; showing
	// them per component answers "what did this open on my server?" without
	// reading the global port strip and guessing which service owns what.
	// Ports: bu bileşenin dışa açtığı gelen portlar ("443/tcp"), katalogdan.
	// Güvenlik duvarı kurulumda zaten tam bunları açar; bileşen başına
	// göstermek, "bu sunucumda neyi açtı?" sorusunu, genel port şeridini okuyup
	// hangisinin kime ait olduğunu tahmin etmeden yanıtlar.
	Ports []string `json:"ports,omitempty"`
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
	ID          string `json:"id"`
	IsInstalled bool   `json:"is_installed"`
	Status      string `json:"status"`
	// Unit is the systemd unit the scan ACTUALLY found for this service —
	// "named" for BIND, "apache2" for Apache. Start/stop/restart must target
	// this, never the catalogue id: the two are equal for most services
	// (nginx, postfix) and different for exactly the ones that broke live
	// (operator, 24 Jul: BIND installed, stop/restart did nothing — the panel
	// was calling `systemctl stop bind`, a unit that does not exist).
	// Unit, taramanın bu servis için GERÇEKTEN bulduğu systemd unit'idir —
	// BIND'de "named", Apache'de "apache2". Başlat/durdur/yeniden başlat bunu
	// hedeflemeli, asla katalog id'sini: ikisi çoğu serviste aynıdır (nginx,
	// postfix) ve tam da canlıda kırılanlarda farklıdır (operatör, 24 Tem:
	// BIND kurulu, durdur/yeniden başlat hiçbir şey yapmıyordu — panel
	// `systemctl stop bind` çağırıyordu; öyle bir unit yok).
	Unit        string            `json:"unit,omitempty"`
	ConfigFiles []core.ConfigFile `json:"config_files,omitempty"`
	// Instances is the per-copy truth for runtimes (B3b), straight from
	// Agent.ListServiceInstances: version, unit, path, managed, per-unit
	// status. The scan no longer parses versions out of unit names.
	// Instances, runtime'lar için kopya-başına gerçektir (B3b), doğrudan
	// Agent.ListServiceInstances'tan gelir: sürüm, unit, yol, yönetilen,
	// unit başına durum. Tarama artık unit adından sürüm ayrıştırmaz.
	Instances []core.ServiceInstance `json:"instances,omitempty"`
	// Versions is kept ONLY so cache rows written before B3b still decode —
	// new scans never write it. When Instances is empty, catalogView falls
	// back to it (minus the dead "default" sentinel) so an upgraded panel
	// keeps showing versions until the next scan.
	// Versions YALNIZ B3b öncesi yazılmış önbellek satırları çözülebilsin diye
	// durur — yeni taramalar onu hiç yazmaz. Instances boşken catalogView ona
	// düşer (ölü "default" sentinel'i hariç); güncellenen panel bir sonraki
	// taramaya dek sürümleri göstermeyi sürdürür.
	Versions []string `json:"versions,omitempty"`
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
		instances := o.Instances
		if instances == nil {
			instances = []core.ServiceInstance{}
		}
		// Versions is derived, never stored: the managed instances' versions,
		// newest-first as the agent reported them. The "default" sentinel is
		// dead — an installed service with no known versions now says [] and
		// the UI renders the honest "—". (It used to say ["default"], a word
		// that leaked into router state, ?version= queries and even RPC
		// calls, meaning three different things on the way.)
		// Versions türetilir, saklanmaz: yönetilen kopyaların sürümleri,
		// agent'ın bildirdiği gibi en yeni önce. "default" sentinel'i öldü —
		// sürümü bilinmeyen kurulu servis artık [] der ve arayüz dürüst "—"
		// çizer. (Eskiden ["default"] derdi; o kelime yönlendirici durumuna,
		// ?version= sorgularına, hatta RPC çağrılarına sızıyor ve yol boyunca
		// üç ayrı anlama geliyordu.)
		versions := []string{}
		for _, in := range instances {
			if in.Managed && in.Version != "" && !contains(versions, in.Version) {
				versions = append(versions, in.Version)
			}
		}
		if len(versions) == 0 {
			// Pre-B3b cache rows carry Versions instead of Instances; honor
			// them (minus the sentinel) until the next scan replaces them.
			// B3b öncesi önbellek satırları Instances yerine Versions taşır;
			// bir sonraki tarama yenileyene dek (sentinel hariç) onlara uy.
			for _, v := range o.Versions {
				if v != "default" && !contains(versions, v) {
					versions = append(versions, v)
				}
			}
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
		notOffered := false
		notOfferedReason := ""
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
			// Package-installed component with no mapping for this family →
			// honestly not offered here. This outranks conflicts and
			// requirements: telling someone to install an SMTP server first,
			// for a filter this distro cannot run, would be theatre.
			// Paketle kurulan bileşenin bu aile için eşlemesi yoksa → burada
			// dürüstçe sunulmuyor. Bu, çakışma ve gereksinimlerden önce gelir:
			// bu dağıtımın koşturamayacağı bir filtre için önce SMTP sunucusu
			// kurdurmak tiyatro olurdu.
			notOfferedReason = core.ManagedServiceInstallDisabledReason(&managed, pkgFamily)
			notOffered = notOfferedReason != ""
		}

		response = append(response, ManagedServiceResponse{
			ID:               managed.ID,
			Unit:             o.Unit,
			Name:             managed.Name,
			Description:      managed.Description,
			Icon:             managed.Icon,
			Category:         managed.Category,
			Versions:         versions,
			Instances:        instances,
			Status:           status,
			IsInstalled:      o.IsInstalled,
			ConflictWith:     conflictWith,
			RequiresMissing:  requiresMissing,
			NotOffered:       notOffered,
			NotOfferedReason: notOfferedReason,
			Ports:            portStrings(managed.FirewallPorts),
			Kind:             managed.Kind,
			Packages:         managed.Packages[pkgFamily],
			ConfigFiles:      configFiles,
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
	pkgFamily := p.packageFamily()

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
		configFiles := []core.ConfigFile{}
		isInstalled := false
		status := "inactive"
		anyRunning := false

		// A versioned runtime matches by pattern as well as by name, so a PHP
		// version this catalogue has never heard of is still observed. Without
		// it, installing php8.5-fpm from Sury produced a unit that is running
		// and serving sites while the panel reported PHP as not installed.
		// Sürümlü bir runtime, ad kadar desenle de eşleşir; böylece bu kataloğun
		// hiç duymadığı bir PHP sürümü yine de gözlenir. Bu olmadan Sury'den
		// kurulan php8.5-fpm, çalışan ve site sunan bir unit üretirken panel
		// PHP'yi kurulu değil diye bildiriyordu.
		managed := managed // loop-var capture for the pointer below
		primaryUnit := ""
		for _, svc := range allServices {
			// core.UnitBelongsTo is the single owner of unit→service matching,
			// shared with the agent scanner (scan_match.go) — one rule, so the
			// scan and the fold can never disagree about who owns a unit.
			// core.UnitBelongsTo, unit→servis eşleştirmesinin tek sahibidir,
			// agent tarayıcısıyla paylaşılır (scan_match.go) — tek kural;
			// böylece tarama ile birleştirme bir unit'in sahibi konusunda
			// asla çelişemez.
			if !core.UnitBelongsTo(svc.Name, &managed) {
				continue
			}
			isInstalled = true
			// Remember the unit that answered, so the row's start/stop targets
			// a unit that exists. A running one wins over a dead one: with
			// bind9.service (alias) and named.service both present, the row
			// must act on whichever systemd actually loaded.
			// Cevap veren unit'i hatırla ki satırın başlat/durduru var olan bir
			// unit'i hedeflesin. Çalışan olan ölüye üstün gelir: bind9.service
			// (takma ad) ve named.service birlikteyken satır, systemd'nin
			// gerçekten yüklediği hangisiyse ona etki etmeli.
			unitReady := managedServiceUnitReady(managed.ID, pkgFamily, svc.Name, svc.Status)
			if primaryUnit == "" || unitReady {
				primaryUnit = svc.Name
			}

			if len(svc.ConfigFiles) > 0 {
				configFiles = append(configFiles, svc.ConfigFiles...)
			}

			// "active (running)" for daemons; oneshot units like
			// wg-quick@wg0 report "active (exited)" — both are up.
			// Daemon'larda "active (running)"; wg-quick@wg0 gibi oneshot
			// unit'ler "active (exited)" bildirir — ikisi de ayaktadır.
			if unitReady {
				anyRunning = true
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

		// A tool has no daemon of ours, so "inactive (dead)" is a lie for it:
		// phpMyAdmin and Roundcube are PHP apps, not stopped services. The UI
		// already masked this via kind, but the API status must be honest too
		// — a status of "installed" is the whole truth (D-010/B3b).
		// Bir tool'un bize ait daemon'ı yoktur; "inactive (dead)" onun için
		// yalandır: phpMyAdmin ve Roundcube PHP uygulamalarıdır, durmuş servis
		// değil. UI bunu zaten kind'le maskeliyordu ama API durumu da dürüst
		// olmalı — "installed" tam gerçektir (D-010/B3b).
		if isInstalled && managed.Kind == core.KindTool {
			status = "installed"
		}

		// Runtimes are the per-copy exception: their truth is the instance
		// list, not unit-name parsing (B3b — extractVersion is gone). A
		// managed instance means installed even when nothing unit-based
		// noticed (a Node tree has no unit and no package, so both probes
		// above are blind to it).
		// Runtime'lar kopya-başına istisnadır: gerçekleri unit adı ayrıştırma
		// değil instance listesidir (B3b — extractVersion gitti). Yönetilen
		// bir kopya, unit tabanlı hiçbir şey fark etmese bile kurulu demektir
		// (Node ağacının unit'i de paketi de yok; yukarıdaki iki yoklama da
		// onu göremez).
		var instances []core.ServiceInstance
		if managed.Kind == core.KindRuntime {
			var ir struct {
				Instances []core.ServiceInstance `json:"instances"`
				Error     string                 `json:"error,omitempty"`
			}
			req := struct {
				ID string `json:"id"`
			}{ID: managed.ID}
			if err := p.agentClient.Call("Agent.ListServiceInstances", &req, &ir); err == nil {
				instances = ir.Instances
			}

			anyManaged, anyUnit, anyUnitActive := false, false, false
			for _, in := range instances {
				if in.Managed {
					anyManaged = true
				}
				if in.Unit != "" {
					anyUnit = true
					if strings.HasPrefix(strings.ToLower(in.Status), "active") {
						anyUnitActive = true
					}
				}
			}
			if anyManaged {
				isInstalled = true
			}
			// Per-unit status beats the aggregate guess; and an installed
			// runtime with NO unit at all (node) is "installed" — there is
			// no daemon to be alive or dead, so running/stopped would be the
			// same false alarm D-010 removed for tools.
			// Unit başına durum, toplu tahmini yener; hiç unit'i olmayan
			// kurulu runtime (node) ise "installed"dır — yaşayacak ya da
			// ölecek daemon yok; çalışıyor/durdu demek, D-010'un tool'lar
			// için kaldırdığı yanlış alarmın aynısı olurdu.
			if isInstalled {
				switch {
				case anyUnitActive:
					status = "active (running)"
				case anyUnit:
					status = "inactive (dead)"
				default:
					status = "installed"
				}
			}
		}

		observations = append(observations, serviceObservation{
			ID:          managed.ID,
			IsInstalled: isInstalled,
			Status:      status,
			Unit:        primaryUnit,
			Instances:   instances,
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
	return catalogView(observations, pkgFamily), nil
}

// managedServiceUnitReady decides whether one active unit proves that the
// catalogue service is actually ready. Debian and Ubuntu package PostgreSQL
// with an aggregate postgresql.service wrapper plus real per-cluster units
// named postgresql@<major>-<cluster>. The wrapper can remain active (exited)
// while every database cluster is down, so on apt hosts only an active cluster
// unit is proof. Arch and other non-cluster layouts use postgresql.service as
// the real daemon and keep the normal rule.
//
// managedServiceUnitReady, etkin bir unit'in katalog servisini gerçekten hazır
// kanıtlayıp kanıtlamadığına karar verir. Debian ve Ubuntu, PostgreSQL'i toplu
// postgresql.service sarmalayıcısı ve gerçek postgresql@<major>-<cluster>
// unit'leriyle paketler. Tüm veritabanı kümeleri durmuşken sarmalayıcı etkin
// (exited) kalabildiği için apt makinelerde yalnız etkin küme unit'i kanıttır.
// Arch ve diğer kümesiz düzenlerde postgresql.service gerçek daemon'dır ve
// olağan kural geçerlidir.
func managedServiceUnitReady(serviceID, pkgFamily, unit, status string) bool {
	if !strings.HasPrefix(strings.ToLower(status), "active") {
		return false
	}
	if serviceID == "postgresql" && pkgFamily == "apt" {
		return strings.HasPrefix(unit, "postgresql@")
	}
	return true
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

// portStrings renders catalogue ports as "443/tcp", matching the wording of
// the firewall strip so the two read as the same fact.
// portStrings, katalog portlarını "443/tcp" olarak çizer; güvenlik duvarı
// şeridiyle aynı dili kullanır ki ikisi aynı gerçek olarak okunsun.
func portStrings(ports []core.FirewallPort) []string {
	if len(ports) == 0 {
		return nil
	}
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, fmt.Sprintf("%d/%s", p.Port, p.Proto))
	}
	return out
}
