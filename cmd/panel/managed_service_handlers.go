package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ManagedServiceResponse represents a managed service with runtime status
type ManagedServiceResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Category    string   `json:"category"`
	Versions    []string `json:"versions"` // Detected versions
	// Status is "unknown" until this host has actually been observed, and
	// only then "not_installed", "installed", "active (running)" and friends.
	// Status, bu makine gerçekten gözlenene dek "unknown"dır.
	Status string `json:"status"`
	// IsInstalled is a THREE-state answer and the pointer is the whole point
	// (R-040): true = observed present, false = observed absent, null = this
	// panel has never looked at this host for this service. Those are three
	// different facts and the first two must never speak for the third.
	//
	// Before this, a host that had never been scanned served the whole
	// catalogue as `is_installed: false, status: "not_installed"` — the same
	// bytes as a host inspected and found empty. Proven live on 3 September
	// 2026: at the same moment on the same host, /api/v1/dns/engine reported
	// BIND installed (it probes dpkg on every request) while
	// /api/v1/managed-services reported it absent, because no scan row
	// existed. A restored or freshly installed server read as empty.
	//
	// Why a null and not an added `observed` flag: a client that has not been
	// updated must not be able to read a false claim out of this field.
	// `false` alongside a new flag it ignores would keep it lying; `null`
	// fails that client's own type check, so it refuses the payload and says
	// so instead of drawing an inventory nobody took.
	//
	// IsInstalled ÜÇ durumlu bir yanıttır ve işaretçi tam da bunun içindir
	// (R-040): true = gözlendi ve var, false = gözlendi ve yok, null = bu
	// panel bu makineye bu servis için hiç bakmadı. Üçü ayrı olgudur ve ilk
	// ikisi üçüncünün yerine konuşamaz. Güncellenmemiş bir istemci bu alandan
	// yanlış bir iddia okuyamamalıdır; `null` onun kendi tür denetimine
	// takılır ve istemci, kimsenin yapmadığı bir envanteri çizmek yerine
	// yükü reddedip bunu söyler.
	IsInstalled *bool `json:"is_installed"`
	// InstalledEvidence names WHICH question produced IsInstalled on this
	// host: the systemd unit table, the package database, or the
	// application's own files. The DNS engine surface asks the package
	// database directly, so for a masked or sealed engine the two surfaces
	// can honestly disagree; the payload says which one this is rather than
	// pretending they are the same question. See
	// internal/core/service_install_evidence.go for why they are not merged.
	// InstalledEvidence, IsInstalled'i HANGİ sorunun ürettiğini adlandırır.
	InstalledEvidence core.ServiceInstallEvidence `json:"installed_evidence,omitempty"`
	ConflictWith      string                      `json:"conflict_with,omitempty"` // installed member of the same conflict group
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
	// node, roundcube) still need a certified host lifecycle boundary; the
	// RHEL preview therefore keeps them closed too.
	// NotOffered: bu bileşen dağıtım paketinden kurulur ve BU sunucunun ailesi
	// için paket eşlemesi yok — burada bilerek sunulmuyor ("kurulunca çalışır"
	// sözü verilemiyor; bkz. docs/DISTRO-SUPPORT). Arayüz, agent'ta geç
	// patlayacak bir Kur düğmesi yerine dürüst bir rozet gösterir. Taşınabilir
	// bileşenler (boş Packages — node, roundcube) de sertifikalı bir host yaşam
	// döngüsü sınırı ister; RHEL önizlemesinde onlar da kapalıdır.
	NotOffered       bool                                `json:"not_offered,omitempty"`
	NotOfferedKind   core.ManagedServiceInstallBlockKind `json:"not_offered_kind,omitempty"`
	NotOfferedReason string                              `json:"not_offered_reason,omitempty"`
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
	Instances []core.ServiceInstance `json:"instances"`
	Packages  []string               `json:"packages,omitempty"` // distro packages (apt) shown before install
	// RepairPackage is emitted only when the scan provides one unambiguous,
	// managed package name accepted by the service's trusted repo pattern.
	// Re-running Install then repairs that exact version instead of silently
	// switching a stopped versioned service back to its distro meta-package.
	// RepairPackage yalniz tarama, servisin guvenilir depo desenine uyan tek ve
	// belirsiz olmayan bir yonetilen paket adi buldugunda yayimlanir. Install'i
	// yeniden calistirmak dagitim meta-paketine sessizce donmek yerine tam bu
	// surumu onarir.
	RepairPackage string `json:"repair_package,omitempty"`
	// RepairAvailable is false when a version-specific apt service cannot be
	// tied to exactly one installed, catalogue-matching package, or when this
	// host family has no certified repair lifecycle. The UI must not turn an
	// observed package into an unsupported mutation.
	// RepairAvailable, surume ozel apt servisi katalogla eslesen tek bir kurulu
	// pakete baglanamiyorsa ya da bu host ailesinin sertifikali onarim yasam
	// dongusu yoksa false olur. UI, gozlenen paketi desteklenmeyen bir
	// mutasyona cevirmemelidir.
	RepairAvailable bool              `json:"repair_available"`
	ConfigFiles     []core.ConfigFile `json:"config_files"` // Detected config files
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

// Installed answers the only question the panel's own logic may ask of a
// three-state field: is this service KNOWN to be present. Unobserved is not
// "absent" — it is "we have not looked" — and every internal caller that is
// about to install, gate or count must treat it as a fact it does not have.
// Installed, panelin kendi mantığının üç durumlu bir alana sorabileceği tek
// soruyu yanıtlar: bu servisin var olduğu BİLİNİYOR mu? Gözlenmemiş, "yok"
// değildir.
func (r ManagedServiceResponse) Installed() bool {
	return r.IsInstalled != nil && *r.IsInstalled
}

// Observed reports whether this host was ever actually asked about this
// service. / Observed, bu makineye bu servisin gerçekten sorulup
// sorulmadığını bildirir.
func (r ManagedServiceResponse) Observed() bool { return r.IsInstalled != nil }

// managedServicesPayload is what both endpoints return: the cached scan and
// when it ran. A null scanned_at means no scan has ever run.
// managedServicesPayload iki uç noktanın da döndürdüğüdür: önbellekteki
// tarama ve ne zaman koştuğu. scanned_at null ise hiç tarama koşmamıştır.
type managedServicesPayload struct {
	ScannedAt        *time.Time               `json:"scanned_at"`
	Services         []ManagedServiceResponse `json:"services"`
	Profiles         []MailProfileResponse    `json:"profiles"`
	DNSIdentityReady bool                     `json:"dns_identity_ready"`
	// MailHostname is what the mail install screen needs to know before it
	// asks: the name this server carries now, the name the install would use,
	// and where that name came from.
	// MailHostname, posta kurulum ekranının sormadan önce bilmesi gerekendir:
	// bu sunucunun şu an taşıdığı ad, kurulumun kullanacağı ad ve o adın
	// nereden geldiği.
	MailHostname MailHostnameIdentity `json:"mail_hostname"`
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
	// InstalledRepoPackages comes from Agent.InstalledRepoPackages. Unlike a
	// unit name, this remains truthful when PostgreSQL/PHP is stopped. It is
	// persisted as observed host state; old cache rows simply decode it as an
	// empty slice and therefore fail closed for version-specific Repair.
	// InstalledRepoPackages, Agent.InstalledRepoPackages'tan gelir; PostgreSQL
	// veya PHP durmuş olsa da unit adından daha doğru kalır. Gözlenen makine
	// durumu olarak saklanır; eski önbellek satırları boş dilime çözülür ve sürüme
	// özel Onarım için güvenli biçimde kapalı kalır.
	InstalledRepoPackages []string `json:"installed_repo_packages,omitempty"`
}

// scanCacheDoc is the persisted shape. An object (not a bare array) so the
// legacy format is told apart by its first byte.
// scanCacheDoc kalıcılaştırılan biçimdir. Eski biçimden ilk baytıyla ayrılsın
// diye çıplak dizi değil nesnedir.
type scanCacheDoc struct {
	Observations []serviceObservation `json:"observations"`
	WebmailReady *bool                `json:"webmail_ready,omitempty"`
}

type decodedScanCache struct {
	Observations  []serviceObservation
	WebmailReady  bool
	WebmailProven bool
}

// These wire types mirror Agent.InstalledRepoPackages. Only ServiceID crosses
// the privilege boundary; the package pattern remains agent-owned catalogue
// data and can never be supplied by the browser or panel.
// Bu wire tipleri Agent.InstalledRepoPackages'i yansitir. Yetki sinirini yalniz
// ServiceID gecer; paket deseni agent katalogunda kalir ve tarayici ya da panel
// tarafindan verilemez.
type installedRepoPackagesReq = transport.InstalledRepoPackagesRequest

type installedRepoPackagesResp = transport.InstalledRepoPackagesResponse

// decodeScanCache reads both formats. A row written before the split is a
// JSON array of full responses; its observed fields are still in there, so an
// upgraded panel keeps showing the right state instead of blanking the page
// until the operator reruns a scan.
// decodeScanCache iki biçimi de okur. Ayrımdan önce yazılmış satır, tam
// yanıtlardan oluşan bir JSON dizisidir; gözlem alanları hâlâ içindedir, bu
// yüzden güncellenen panel, operatör yeniden tarama koşturana dek sayfayı
// boşaltmak yerine doğru durumu göstermeyi sürdürür.
func decodeScanCache(data string) ([]serviceObservation, error) {
	decoded, err := decodeScanCacheSnapshot(data)
	return decoded.Observations, err
}

func decodeScanCacheSnapshot(data string) (decodedScanCache, error) {
	trimmed := strings.TrimSpace(data)
	if strings.HasPrefix(trimmed, "[") {
		var legacy []ManagedServiceResponse
		if err := json.Unmarshal([]byte(trimmed), &legacy); err != nil {
			return decodedScanCache{}, fmt.Errorf("decode legacy service scan cache: %w", err)
		}
		obs := make([]serviceObservation, 0, len(legacy))
		for _, l := range legacy {
			obs = append(obs, serviceObservation{
				ID:          l.ID,
				IsInstalled: l.Installed(),
				Status:      l.Status,
				Versions:    l.Versions,
				ConfigFiles: l.ConfigFiles,
			})
		}
		return decodedScanCache{Observations: obs}, nil
	}
	var envelope struct {
		Observations json.RawMessage `json:"observations"`
		WebmailReady *bool           `json:"webmail_ready"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return decodedScanCache{}, fmt.Errorf("decode service scan cache: %w", err)
	}
	if len(envelope.Observations) == 0 || string(envelope.Observations) == "null" {
		return decodedScanCache{}, fmt.Errorf("decode service scan cache: observations are missing")
	}
	var observations []serviceObservation
	if err := json.Unmarshal(envelope.Observations, &observations); err != nil {
		return decodedScanCache{}, fmt.Errorf("decode service scan observations: %w", err)
	}
	decoded := decodedScanCache{Observations: observations}
	if envelope.WebmailReady != nil {
		decoded.WebmailReady = *envelope.WebmailReady
		decoded.WebmailProven = true
	}
	return decoded, nil
}

// safeRepairPackage returns observed package identity only when it is
// unambiguous and bounded again by the panel catalogue's trusted package
// regexp. The agent already performs this validation against its own
// catalogue; repeating it here prevents a stale or mismatched agent response
// from becoming an install argument.
// safeRepairPackage, gozlenen paket kimligini yalniz tek anlamliysa ve panel
// katalogunun guvenilir paket regexp'iyle yeniden sinirlanmissa dondurur. Agent
// ayni dogrulamayi kendi katalogunda yapar; burada tekrarlamak eski veya uyumsuz
// agent yanitinin kurulum argumani olmasini engeller.
func safeRepairPackage(managed *core.ManagedService, o serviceObservation, pkgFamily string) string {
	if managed == nil || pkgFamily != "apt" || managed.Repo == nil || managed.Repo.PackagePattern == "" {
		return ""
	}
	re, err := regexp.Compile(managed.Repo.PackagePattern)
	if err != nil {
		return ""
	}
	candidates := map[string]struct{}{}
	for _, raw := range o.InstalledRepoPackages {
		candidate := strings.TrimSpace(raw)
		if candidate != "" && re.FindString(candidate) == candidate {
			candidates[candidate] = struct{}{}
		}
	}
	if len(candidates) != 1 {
		return ""
	}
	for candidate := range candidates {
		return candidate
	}
	return ""
}

// repairSelection makes the fail-closed choice explicit. Established apt and
// pacman lifecycles preserve their current repair behavior. An apt service
// with a versioned repository pattern must reuse exactly one observed package.
// Every other family, including the still-uncertified dnf preview, is blocked.
// repairSelection, guvenli-kapali secimi acik eder. Yerlesik apt ve pacman
// yasam donguleri mevcut onarim davranislarini korur. Surumlu depo deseni olan
// apt servisi tam bir gozlenen kurulu paketi yeniden kullanmalidir. Henuz
// sertifikali olmayan dnf onizlemesi dahil diger her aile kapali kalir.
func repairSelection(managed *core.ManagedService, o serviceObservation, pkgFamily string) (string, bool) {
	if managed == nil {
		return "", false
	}
	switch pkgFamily {
	case "apt":
		if managed.Repo != nil && managed.Repo.PackagePattern != "" {
			pkg := safeRepairPackage(managed, o, pkgFamily)
			return pkg, pkg != ""
		}
		return "", true
	case "pacman":
		return "", true
	default:
		return "", false
	}
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
	return catalogViewForHost(obs, core.ManagedServiceHostProfile{PackageFamily: pkgFamily})
}

func catalogViewForHost(obs []serviceObservation, host core.ManagedServiceHostProfile) []ManagedServiceResponse {
	pkgFamily := host.PackageFamily
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
		// `observed` is the difference between "the host answered about this
		// service" and "nobody asked". A scan writes one observation per
		// catalogue entry, so a missing entry means exactly one of two
		// things, and both are honestly "not checked": no scan has ever run
		// on this host, or this service entered the catalogue after the last
		// scan was taken. The zero value of the map lookup used to be read as
		// "absent", which is how R-040 turned silence into a claim.
		// `observed`, "makine bu servis hakkında yanıt verdi" ile "kimse
		// sormadı" arasındaki farktır. Tarama, katalog kalemi başına bir
		// gözlem yazar; kayıt yoksa ya bu makinede hiç tarama koşmamıştır ya
		// da servis son taramadan sonra kataloğa girmiştir — ikisi de dürüstçe
		// "bakılmadı"dır.
		o, observed := byID[managed.ID]
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
		repairPackage, repairAvailable := repairSelection(&managed, o, pkgFamily)

		// Not-installed catalogue services are listed too, so the panel can
		// offer a one-click install. They carry status "not_installed"; the UI
		// shows an Install button instead of start/stop/manage.
		// Kurulu-olmayan katalog servisleri de listelenir ki panel tek-tık
		// kurulum sunabilsin. "not_installed" durumu taşırlar; arayüz
		// başlat/durdur/yönet yerine Kur düğmesi gösterir.
		notOffered := false
		notOfferedKind := core.ManagedServiceInstallBlockNone
		notOfferedReason := ""
		var isInstalled *bool
		switch {
		case !observed:
			// Never looked. Say exactly that, in both fields that carry it:
			// is_installed is null and status is "unknown". Conflicts and
			// unmet requirements are withheld too — both are read off the
			// installed set, and an empty set here means "unknown", not
			// "nothing is installed". Not-offered stays: it is a catalogue
			// and host-family fact that needs no observation.
			// Hiç bakılmadı. Bunu tam olarak söyle: is_installed null,
			// status "unknown". Çakışma ve karşılanmamış gereksinimler de
			// söylenmez — ikisi de kurulu kümeden okunur ve buradaki boş küme
			// "hiçbir şey kurulu değil" değil "bilinmiyor" demektir.
			status = "unknown"
			notOfferedKind, notOfferedReason = core.ManagedServiceInstallBlockForHost(&managed, host)
			notOffered = notOfferedReason != ""
		case !o.IsInstalled:
			installedFalse := false
			isInstalled = &installedFalse
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
			// dürüstçe sunulmuyor. Arayüz mevcut bir rakip servis çakışmasını
			// bu rozetten önce gösterir; böylece BIND/Exim için asıl engel
			// kaybolmaz. Gereksinimlerse ancak kurulum sunulduğunda anlamlıdır.
			notOfferedKind, notOfferedReason = core.ManagedServiceInstallBlockForHost(&managed, host)
			notOffered = notOfferedReason != ""
		default:
			installedTrue := true
			isInstalled = &installedTrue
		}

		response = append(response, ManagedServiceResponse{
			ID:                managed.ID,
			Unit:              o.Unit,
			Name:              managed.Name,
			Description:       managed.Description,
			Icon:              managed.Icon,
			Category:          managed.Category,
			Versions:          versions,
			Instances:         instances,
			Status:            status,
			IsInstalled:       isInstalled,
			InstalledEvidence: core.InstalledEvidenceFor(&managed, pkgFamily),
			ConflictWith:      conflictWith,
			RequiresMissing:   requiresMissing,
			NotOffered:        notOffered,
			NotOfferedKind:    notOfferedKind,
			NotOfferedReason:  notOfferedReason,
			Ports:             portStrings(managed.FirewallPorts),
			Kind:              managed.Kind,
			Packages:          managed.Packages[pkgFamily],
			RepairPackage:     repairPackage,
			RepairAvailable:   repairAvailable,
			ConfigFiles:       configFiles,
		})
	}
	return response
}

// handleManagedServices serves the catalogue joined with the cached
// observation. It never performs the expensive host-wide probe itself; the
// page conditionally refreshes a missing/stale cache through the scan endpoint.
// handleManagedServices kataloğu önbellekteki gözlemle birleştirerek sunar.
// Pahalı sistem geneli taramayı kendisi yapmaz; sayfa eksik/bayat önbelleği
// tarama uç noktası üzerinden koşullu olarak yeniler.
func (p *Panel) handleManagedServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	profileAttempts, proofErr := p.latestMailProfileAttemptProofs(r.Context())
	if proofErr != nil {
		writeServerError(w, proofErr)
		return
	}

	// A fresh installation has no observation row yet, but its installable
	// catalogue is still known. Never render an empty Components page merely
	// because the first scan has not completed.
	host := p.managedServiceHostProfile()
	packageFamily := host.PackageFamily
	dnsIdentityReady := p.managedServicesDNSIdentityReady(r.Context())
	payload := managedServicesPayload{
		Services:         catalogViewForHost(nil, host),
		Profiles:         mailProfilesView(nil, false, packageFamily, mailProfileCatalogBlockedReason(mailProfileHostBlockedReason(), dnsIdentityReady), false, false, profileAttempts),
		DNSIdentityReady: dnsIdentityReady,
		MailHostname:     p.mailHostnameIdentity(r.Context()),
	}

	var data string
	var scannedAt string
	profilesVerified := false
	err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`).Scan(&data, &scannedAt)
	if err == nil {
		snapshot, decodeErr := decodeScanCacheSnapshot(data)
		if decodeErr != nil {
			log.Printf("cached service state is unverified: %v", decodeErr)
			writeCodedError(w, http.StatusServiceUnavailable, errCodeServiceStateUnverified,
				"cached service state could not be verified; run a fresh scan", "/services")
			return
		}
		if t, terr := time.Parse(time.RFC3339, scannedAt); terr == nil {
			payload.ScannedAt = &t
			age := time.Since(t)
			profilesVerified = age >= 0 && age <= 5*time.Minute
		}
		// The catalogue is joined on at read time, so an upgraded panel tells
		// the truth about its own catalogue immediately — no rescan needed to
		// see a renamed service, a new one, or a corrected description.
		// Katalog okuma anında birleştirilir; böylece güncellenen panel kendi
		// kataloğu hakkında anında doğruyu söyler — adı değişen bir servisi,
		// yenisini ya da düzeltilmiş bir açıklamayı görmek için tarama gerekmez.
		payload.Services = catalogViewForHost(snapshot.Observations, host)
		payload.Profiles = mailProfilesView(
			payload.Services,
			profilesVerified,
			packageFamily,
			mailProfileCatalogBlockedReason(mailProfileHostBlockedReason(), dnsIdentityReady),
			snapshot.WebmailReady,
			snapshot.WebmailProven,
			profileAttempts,
		)
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeServerError(w, fmt.Errorf("read service scan cache: %w", err))
		return
	}

	json.NewEncoder(w).Encode(payload)
}

// packageFamily returns the host's package-manager family, asked from the
// agent once and kept. This is one bounded identity/readiness preflight, not a
// service-state inventory; caching prevents repeating its executable and
// systemd verification on every GET. A failed call returns an empty family without
// memoising it. Callers then fail closed for package-backed services instead
// of presenting apt operations on an unknown host.
// packageFamily, makinenin paket-yöneticisi ailesini döndürür; agent'a bir kez
// sorulup saklanır. Bu, servis durumu envanteri değil; sınırlı bir kimlik ve
// hazır-oluş ön kontrolüdür. Önbellek, executable ve systemd doğrulamasının her
// GET'te tekrarlanmasını önler. Başarısız çağrı, belleğe yazmadan boş aile
// döndürür. Böylece bilinmeyen bir makinede apt işlemleri sunulmaz ve anlık
// düşmüş bir agent yanlış yanıtı süreç boyunca dondurmaz.
func (p *Panel) packageFamily() string {
	p.pkgFamilyMu.Lock()
	defer p.pkgFamilyMu.Unlock()
	if p.pkgFamilyVal != "" {
		return p.pkgFamilyVal
	}
	var fam string
	if err := p.callAgent("Agent.PkgFamily", &transport.Empty{}, &fam); err == nil && fam != "" {
		p.pkgFamilyVal = fam
		return fam
	}
	return ""
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

	maxAge, conditional, err := managedServiceScanMaxAge(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Serialize page-triggered and manual scans before taking the wider
	// mutation lease. A second tab waits for the first scan, observes its new
	// timestamp and returns that cache instead of probing the host again.
	p.serviceScanMu.Lock()
	defer p.serviceScanMu.Unlock()
	if conditional {
		cached, fresh, err := p.managedServicesCacheWithin(r.Context(), maxAge)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if fresh {
			json.NewEncoder(w).Encode(cached)
			return
		}
	}

	release, busy := p.beginServiceMutation(w, r)
	if busy {
		return
	}
	defer release()

	services, err := p.scanManagedServices(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	webmailReady, webmailProven, err := p.cachedWebmailReadinessProof(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	profileAttempts, err := p.latestMailProfileAttemptProofs(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}

	now := time.Now().UTC()
	dnsIdentityReady := p.managedServicesDNSIdentityReady(r.Context())
	json.NewEncoder(w).Encode(managedServicesPayload{
		ScannedAt:        &now,
		Services:         services,
		Profiles:         mailProfilesView(services, true, p.packageFamily(), mailProfileCatalogBlockedReason(mailProfileHostBlockedReason(), dnsIdentityReady), webmailReady, webmailProven, profileAttempts),
		DNSIdentityReady: dnsIdentityReady,
		MailHostname:     p.mailHostnameIdentity(r.Context()),
	})
}

// managedServiceScanMaxAge distinguishes automatic conditional refreshes from
// an operator's explicit Rescan. Manual requests omit the parameter and always
// probe the host.
func managedServiceScanMaxAge(r *http.Request) (time.Duration, bool, error) {
	raw := r.URL.Query().Get("max_age_seconds")
	if raw == "" {
		return 0, false, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 1 || seconds > 86400 {
		return 0, false, fmt.Errorf("max_age_seconds must be between 1 and 86400")
	}
	return time.Duration(seconds) * time.Second, true, nil
}

func (p *Panel) managedServicesCacheWithin(ctx context.Context, maxAge time.Duration) (managedServicesPayload, bool, error) {
	var data string
	var scannedAt string
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`).Scan(&data, &scannedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return managedServicesPayload{}, false, nil
	}
	if err != nil {
		return managedServicesPayload{}, false, err
	}

	scanned, err := time.Parse(time.RFC3339, scannedAt)
	if err != nil {
		return managedServicesPayload{}, false, nil
	}
	age := time.Since(scanned)
	if age < 0 {
		// A future cache timestamp cannot prove freshness. Treat it exactly like
		// a missing/stale observation and force the conditional caller to rescan.
		return managedServicesPayload{}, false, nil
	}
	if age > maxAge {
		return managedServicesPayload{}, false, nil
	}

	snapshot, err := decodeScanCacheSnapshot(data)
	if err != nil {
		return managedServicesPayload{}, false, nil
	}
	host := p.managedServiceHostProfile()
	packageFamily := host.PackageFamily
	services := catalogViewForHost(snapshot.Observations, host)
	dnsIdentityReady := p.managedServicesDNSIdentityReady(ctx)
	profileAttempts, err := p.latestMailProfileAttemptProofs(ctx)
	if err != nil {
		return managedServicesPayload{}, false, err
	}
	return managedServicesPayload{
		ScannedAt:        &scanned,
		Services:         services,
		Profiles:         mailProfilesView(services, true, packageFamily, mailProfileCatalogBlockedReason(mailProfileHostBlockedReason(), dnsIdentityReady), snapshot.WebmailReady, snapshot.WebmailProven, profileAttempts),
		DNSIdentityReady: dnsIdentityReady,
	}, true, nil
}

// scanManagedServices asks the agent for the real system state, folds it
// into the curated catalogue and persists the result.
// scanManagedServices, agent'tan gerçek sistem durumunu ister, seçili
// kataloğa işler ve sonucu kalıcılaştırır.
func (p *Panel) scanManagedServices(ctx context.Context) ([]ManagedServiceResponse, error) {
	var allServices []core.Service
	if err := p.callAgentContext(ctx, "Agent.GetServices", &transport.Empty{}, &allServices); err != nil {
		return nil, err
	}
	host := p.managedServiceHostProfile()
	pkgFamily := host.PackageFamily

	// Which catalogue packages are present (installed but maybe not running).
	// Hangi katalog paketleri var (kurulu ama belki çalışmıyor).
	var installedIDs []string
	if err := p.callAgentContext(ctx, "Agent.InstalledServiceIDs", &transport.Empty{}, &installedIDs); err != nil {
		return nil, fmt.Errorf("probe installed service ids: %w", err)
	}
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
		var installedRepoPackages []string
		if pkgFamily == "apt" && managed.Repo != nil && managed.Repo.PackagePattern != "" {
			var packageResult installedRepoPackagesResp
			request := installedRepoPackagesReq{ServiceID: managed.ID}
			if err := p.callAgentContext(ctx, "Agent.InstalledRepoPackages", &request, &packageResult); err != nil {
				return nil, fmt.Errorf("probe installed repository packages for %s: %w", managed.ID, err)
			}
			if packageResult.Error != "" {
				return nil, fmt.Errorf("probe installed repository packages for %s: %s", managed.ID, packageResult.Error)
			}
			installedRepoPackages = append([]string(nil), packageResult.Packages...)
		}

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
			var ir transport.ServiceInstancesResponse
			req := transport.ServiceInstancesRequest{ID: managed.ID}
			if err := p.callAgentContext(ctx, "Agent.ListServiceInstances", &req, &ir); err != nil {
				return nil, fmt.Errorf("probe service instances for %s: %w", managed.ID, err)
			}
			if ir.Error != "" {
				return nil, fmt.Errorf("probe service instances for %s: %s", managed.ID, ir.Error)
			}
			instances = ir.Instances

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
			ID:                    managed.ID,
			IsInstalled:           isInstalled,
			Status:                status,
			Unit:                  primaryUnit,
			Instances:             instances,
			ConfigFiles:           configFiles,
			InstalledRepoPackages: installedRepoPackages,
		})
	}

	webmailReady := false
	if installedSet["roundcube"] {
		webmailReady = p.webmailAvailable(ctx)
	}
	data, err := json.Marshal(scanCacheDoc{
		Observations: observations,
		WebmailReady: &webmailReady,
	})
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
	return catalogViewForHost(observations, host), nil
}

func (p *Panel) cachedWebmailReadinessProof(ctx context.Context) (bool, bool, error) {
	var data string
	if err := p.db.GetDB().QueryRowContext(
		ctx,
		`SELECT data FROM service_scan_cache WHERE id = 1`,
	).Scan(&data); err != nil {
		return false, false, fmt.Errorf("read cached webmail readiness proof: %w", err)
	}
	snapshot, err := decodeScanCacheSnapshot(data)
	if err != nil {
		return false, false, err
	}
	return snapshot.WebmailReady, snapshot.WebmailProven, nil
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
