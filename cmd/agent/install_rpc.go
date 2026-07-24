package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

// One-click service installation — the flip side of "what isn't installed is
// invisible": nothing is preinstalled, and the admin adds exactly the
// services they want. The service ID is validated against the fixed
// catalogue, so only known packages are ever installed; the distro family
// decides the real package names.
//
// Tek tıkla servis kurulumu — "kurulu olmayan görünmez"in öteki yüzü: hiçbir
// şey önceden kurulmaz ve yönetici tam istediği servisleri ekler. Servis
// ID'si sabit kataloğa karşı doğrulanır; böylece yalnız bilinen paketler
// kurulur; gerçek paket adlarına dağıtım ailesi karar verir.

type InstallServiceRequest struct {
	ID string `json:"id"` // managed service id, e.g. "postgresql"
	// Package, when set, is a specific version package chosen from the service's
	// managed repo (e.g. "postgresql-17") to install instead of the distro
	// default. It is accepted only if the service has a Repo and the name
	// matches that repo's PackagePattern — so a version pick can never turn into
	// an arbitrary package install.
	// Package, ayarlıysa, servisin yönetilen deposundan seçilmiş belirli bir
	// sürüm paketidir (örn. "postgresql-17"); dağıtım varsayılanı yerine kurulur.
	// Yalnız servisin bir Repo'su varsa ve ad o deponun PackagePattern'ına
	// uyuyorsa kabul edilir — böylece sürüm seçimi asla keyfi paket kurulumuna
	// dönüşemez.
	Package string `json:"package,omitempty"`
}

type InstallServiceResponse struct {
	Installed bool   `json:"installed"` // false if it was already present
	Detail    string `json:"detail,omitempty"`
	Error     string `json:"error,omitempty"`
}

// InstallService installs a managed service by its catalog ID, then enables +
// starts its systemd unit so it is actually running (and survives reboot)
// right after. Already-present services are a no-op reported honestly.
// InstallService, katalog kimliğiyle yönetilen bir servisi kurar, sonra
// systemd unit'ini etkinleştirip başlatır; böylece hemen ardından gerçekten
// çalışır (ve reboot'tan sağ çıkar). Zaten kurulu servisler dürüstçe
// bildirilen bir no-op'tur.
func (a *Agent) InstallService(req *InstallServiceRequest, resp *InstallServiceResponse) error {
	svc := core.GetManagedServiceByID(req.ID)
	if svc == nil {
		resp.Error = "unknown service"
		return nil
	}

	// A version pick is validated before anything else, because it changes what
	// "already installed" even means.
	// Sürüm seçimi her şeyden önce doğrulanır, çünkü "zaten kurulu"nun ne
	// demek olduğunu değiştiren şey odur.
	var versionPrefix string
	if req.Package != "" {
		if svc.Repo == nil {
			resp.Error = "this service does not offer version selection"
			return nil
		}
		// Version selection comes from a managed vendor repo, which is apt-only
		// today. Without this the pick sails through validation and dies inside
		// pacman as "target not found: php8.3-fpm" — a package-manager error
		// shown to someone who asked the panel a product question.
		// Sürüm seçimi yönetilen bir vendor deposundan gelir ve bugün yalnız
		// apt'te vardır. Bu olmadan seçim doğrulamayı geçip pacman'in içinde
		// "target not found: php8.3-fpm" diye ölür — panele ürün sorusu soran
		// birine gösterilen bir paket-yöneticisi hatası.
		if detectPkgFamily() != "apt" {
			resp.Error = "choosing a version needs a managed repository, which is only supported on apt (Debian/Ubuntu) systems yet"
			return nil
		}
		re, err := regexp.Compile(svc.Repo.PackagePattern)
		if err != nil {
			resp.Error = "invalid package pattern in catalogue"
			return nil
		}
		m := re.FindStringSubmatch(req.Package)
		if m == nil {
			resp.Error = fmt.Sprintf("%q is not a valid version package for %s", req.Package, svc.Name)
			return nil
		}
		if len(m) > 1 {
			versionPrefix = m[1]
		}
	}

	// For a plain install the question is "is this service here?"; for a version
	// pick it is "is THIS VERSION here?". Asking the first question for both is
	// what made a second PHP impossible: with 8.4 present, a request for 8.3 was
	// answered "PHP-FPM is already installed" and refused — the admin could not
	// give a customer the version they needed even with Sury enabled, which is
	// the whole point of D-014's chain.
	// Düz kurulumda soru "bu servis burada mı?"dır; sürüm seçiminde "BU SÜRÜM
	// burada mı?". İkisi için de ilk soruyu sormak, ikinci bir PHP'yi imkânsız
	// kılan şeydi: 8.4 varken 8.3 isteği "PHP-FPM zaten kurulu" diye
	// reddediliyordu — Sury açık olsa bile admin, müşterinin ihtiyacı olan
	// sürümü veremiyordu; D-014'ün zincirinin tüm amacı buydu.
	if req.Package != "" {
		if packageInstalled(req.Package) {
			resp.Installed = false
			resp.Detail = req.Package + " is already installed"
			return nil
		}
	} else if a.serviceInstalled(svc) {
		resp.Installed = false
		resp.Detail = svc.Name + " is already installed"
		return nil
	}

	// Requirements first: a dependent tool without its parent service would
	// install broken (phpMyAdmin with no MariaDB, no web server, no PHP). The
	// UI blocks this too; the agent is the enforcement that cannot be skipped.
	// Önce gereksinimler: üst servisi olmayan bağımlı araç bozuk kurulur
	// (MariaDB'siz, web sunucususuz, PHP'siz phpMyAdmin). UI da engeller;
	// atlatılamayan uygulayıcı agent'tır.
	if missing := core.RequirementsMissing(svc, a.installedServiceSet()); len(missing) > 0 {
		resp.Error = fmt.Sprintf("%s requires %s — install that first from Services",
			svc.Name, strings.Join(missing, ", "))
		return nil
	}

	family := detectPkgFamily()
	pkgs := svc.Packages[family]
	if len(pkgs) == 0 {
		resp.Error = fmt.Sprintf("%s cannot be installed automatically on this system yet", svc.Name)
		return nil
	}

	// A version pick from the service's managed repo replaces the distro
	// default — but only if the service actually has a repo and the requested
	// name matches its pattern, so this can never install an arbitrary package.
	// Servisin yönetilen deposundan bir sürüm seçimi dağıtım varsayılanının
	// yerini alır — ama yalnız servisin gerçekten bir deposu varsa ve istenen ad
	// desenine uyuyorsa; böylece bu asla keyfi bir paket kuramaz.
	var skipped []string
	if req.Package != "" {
		pkgs = []string{req.Package}

		// A picked version installs bare without its companions — `php8.3-fpm`
		// alone has no database driver, mbstring or curl, so the panel would
		// report success for a runtime that cannot serve WordPress or Laravel.
		// Companions are filtered to what apt actually offers for THIS version
		// and the rest is reported: Sury publishes no php8.5-opcache, and a
		// strict install would refuse PHP 8.5 entirely over one extension.
		// Seçilen bir sürüm companion'ları olmadan çıplak kurulur — tek başına
		// `php8.3-fpm`in veritabanı sürücüsü, mbstring'i, curl'ü yoktur; panel,
		// WordPress ya da Laravel sunamayacak bir runtime için başarı
		// bildirirdi. Companion'lar apt'ın BU sürüm için gerçekten sunduklarına
		// süzülür, kalanı raporlanır: Sury php8.5-opcache yayınlamıyor ve katı
		// bir kurulum tek bir uzantı yüzünden PHP 8.5'i tümüyle reddederdi.
		if versionPrefix != "" && len(svc.Repo.VersionCompanions) > 0 {
			for _, tpl := range svc.Repo.VersionCompanions {
				name := strings.ReplaceAll(tpl, "{v}", versionPrefix)
				if packageAvailable(name) {
					pkgs = append(pkgs, name)
				} else {
					skipped = append(skipped, name)
				}
			}
		}
	}

	if _, err := installPackages(family, pkgs); err != nil {
		resp.Error = fmt.Sprintf("package install failed: %v", err)
		return nil
	}

	// After a version pick, enable the unit that version actually brought —
	// firstPresentUnit returns the NEWEST present unit, which after installing
	// 8.3 next to an existing 8.4 would start 8.4 and leave the version the
	// operator just asked for stopped.
	// Sürüm seçiminden sonra, o sürümün gerçekten getirdiği unit etkinleştirilir
	// — firstPresentUnit mevcut EN YENİ unit'i döndürür; var olan 8.4'ün yanına
	// 8.3 kurulduktan sonra bu, 8.4'ü başlatıp operatörün az önce istediği
	// sürümü durmuş bırakırdı.
	unit := ""
	if req.Package != "" && unitExists(req.Package) {
		unit = req.Package
	} else {
		unit = a.firstPresentUnit(svc)
	}
	if unit != "" {
		_ = exec.Command("systemctl", "enable", "--now", unit).Run()
	}

	resp.Installed = true
	resp.Detail = fmt.Sprintf("installed %s", strings.Join(pkgs, ", "))
	if len(skipped) > 0 {
		resp.Detail += fmt.Sprintf(" — not offered for this version: %s", strings.Join(skipped, ", "))
	}
	return nil
}

// packageAvailable reports whether apt knows a package name at all, so an
// optional companion that a vendor does not publish for one version is skipped
// instead of failing the whole install.
// packageAvailable, apt'ın bir paket adını hiç tanıyıp tanımadığını bildirir;
// böylece vendor'ın bir sürüm için yayınlamadığı isteğe bağlı bir companion,
// tüm kurulumu düşürmek yerine atlanır.
func packageAvailable(name string) bool {
	out, err := exec.Command("apt-cache", "policy", name).Output()
	if err != nil {
		return false
	}
	// An unknown package prints nothing; a known one prints a stanza whose
	// Candidate is a real version rather than "(none)".
	// Bilinmeyen paket hiçbir şey basmaz; bilinen paket, Candidate'i "(none)"
	// değil gerçek bir sürüm olan bir kayıt basar.
	text := strings.TrimSpace(string(out))
	if text == "" {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		if s := strings.TrimSpace(line); strings.HasPrefix(s, "Candidate:") {
			return strings.TrimSpace(strings.TrimPrefix(s, "Candidate:")) != "(none)"
		}
	}
	return false
}

// unitExists reports whether systemd knows a unit by exactly this name.
// unitExists, systemd'nin tam bu adda bir unit tanıyıp tanımadığını bildirir.
func unitExists(name string) bool {
	out, err := exec.Command("systemctl", "list-unit-files", name+".service", "--no-legend").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

type UninstallServiceResponse struct {
	Removed bool   `json:"removed"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

// UninstallService stops and disables a managed service, then purges its
// packages — the mirror of InstallService, for shrinking the attack surface.
// Every installed service is code that can be exploited; the operator must be
// able to take one back off, not only add it. Refuses services we never
// install (protects nothing) and reports a not-installed service honestly.
// UninstallService, yönetilen bir servisi durdurur, devre dışı bırakır ve
// paketlerini purge eder — InstallService'in aynası, saldırı yüzeyini
// küçültmek için. Kurulu her servis sömürülebilir koddur; operatör bir
// servisi yalnız ekleyebilmemeli, geri de alabilmeli.
func (a *Agent) UninstallService(req *InstallServiceRequest, resp *UninstallServiceResponse) error {
	svc := core.GetManagedServiceByID(req.ID)
	if svc == nil {
		resp.Error = "unknown service"
		return nil
	}
	family := detectPkgFamily()

	// Version pick (B3d): remove ONE version of a runtime, the mirror of the
	// install pick. Same gates as install: the service must have a Repo, the
	// package must match its PackagePattern, apt only. Companions go with it
	// ({v}-cli, {v}-mysql… — orphaned config-only leftovers are what made
	// "did I really remove 8.3?" unanswerable on classic panels).
	// Sürüm seçimi (B3d): bir runtime'ın TEK sürümünü kaldır — kurulum
	// seçiminin aynası. Kapılar aynı: serviste Repo olmalı, paket
	// PackagePattern'e uymalı, yalnız apt. Yol arkadaşları da gider
	// ({v}-cli, {v}-mysql… — öksüz kalıntılar, klasik panellerde "8.3'ü
	// gerçekten kaldırdım mı?" sorusunu cevapsız bırakan şeydi).
	if req.Package != "" {
		if svc.Repo == nil {
			resp.Error = fmt.Sprintf("%s has no per-version packages", svc.Name)
			return nil
		}
		if family != "apt" {
			resp.Error = "per-version removal is only supported on apt-based systems"
			return nil
		}
		re, err := regexp.Compile(svc.Repo.PackagePattern)
		if err != nil || !re.MatchString(req.Package) {
			resp.Error = "package does not belong to this service"
			return nil
		}
		if !packageInstalled(req.Package) {
			resp.Removed = true
			resp.Detail = fmt.Sprintf("%s is not installed", req.Package)
			return nil
		}
		versionPrefix := ""
		if m := re.FindStringSubmatch(req.Package); len(m) > 1 {
			versionPrefix = m[1]
		}
		pkgs := []string{req.Package}
		for _, tpl := range svc.Repo.VersionCompanions {
			name := strings.ReplaceAll(tpl, "{v}", versionPrefix)
			if packageInstalled(name) {
				pkgs = append(pkgs, name)
			}
		}
		// Unit name == package name for version-pick installs (php8.3-fpm).
		// Sürüm-seçimli kurulumda unit adı == paket adı (php8.3-fpm).
		if unitExists(req.Package) {
			_ = exec.Command("systemctl", "disable", "--now", req.Package).Run()
		}
		if _, err := removePackages(family, pkgs); err != nil {
			resp.Error = fmt.Sprintf("package removal failed: %v", err)
			return nil
		}
		resp.Removed = true
		resp.Detail = fmt.Sprintf("removed %s", strings.Join(pkgs, ", "))
		return nil
	}

	pkgs := svc.Packages[family]
	if len(pkgs) == 0 {
		resp.Error = fmt.Sprintf("%s cannot be removed automatically on this system yet", svc.Name)
		return nil
	}

	// Stop + disable every present unit first, so purge does not fight a
	// running process. Template instances ("wg-quick@wg0") are handled by
	// their exact SystemNames entry; pattern-matched units (php8.3-fpm from
	// Sury) are stopped AND their packages added to the purge — before B3d,
	// uninstalling php-fpm removed only the meta package and left every
	// versioned daemon installed, running and serving.
	// Önce mevcut her unit'i durdur + devre dışı bırak ki purge çalışan bir
	// süreçle boğuşmasın. Desenle eşleşen unit'ler (Sury'den php8.3-fpm) de
	// durdurulur VE paketleri purge listesine eklenir — B3d'den önce php-fpm'i
	// kaldırmak yalnız meta paketi söküyor, sürümlü daemon'ların hepsini
	// kurulu, çalışır ve sunar hâlde bırakıyordu.
	for _, unit := range svc.SystemNames {
		_ = exec.Command("systemctl", "disable", "--now", unit).Run()
	}
	if svc.SystemNamePattern != "" {
		for _, unit := range unitsMatching(svc.SystemNamePattern) {
			_ = exec.Command("systemctl", "disable", "--now", unit).Run()
			if family == "apt" && packageInstalled(unit) {
				pkgs = append(pkgs, unit)
				if svc.Repo != nil {
					if re, err := regexp.Compile(svc.Repo.PackagePattern); err == nil {
						if m := re.FindStringSubmatch(unit); len(m) > 1 {
							for _, tpl := range svc.Repo.VersionCompanions {
								name := strings.ReplaceAll(tpl, "{v}", m[1])
								if packageInstalled(name) {
									pkgs = append(pkgs, name)
								}
							}
						}
					}
				}
			}
		}
	}

	if _, err := removePackages(family, pkgs); err != nil {
		resp.Error = fmt.Sprintf("package removal failed: %v", err)
		return nil
	}

	resp.Removed = true
	resp.Detail = fmt.Sprintf("removed %s", strings.Join(pkgs, ", "))
	return nil
}

// serviceInstalled reports whether a catalogue service is present: by its
// systemd unit when it has one, by its package otherwise (daemonless tools
// like phpMyAdmin are files, not units).
// serviceInstalled, bir katalog servisinin var olup olmadığını bildirir:
// unit'i varsa unit'inden, yoksa paketinden (phpMyAdmin gibi daemon'suz
// araçlar unit değil dosyadır).
func (a *Agent) serviceInstalled(svc *core.ManagedService) bool {
	// Roundcube is a tarball tree, not a unit or a package (D-004): presence
	// is the tree on disk, the same way Node's is. Without this it has no
	// SystemNames and no Packages, so the loops below both say "not
	// installed" even right after a successful install.
	// Roundcube bir tarball ağacıdır, unit ya da paket değil (D-004): varlık
	// diskteki ağaçtır, Node'unki gibi. Bu olmadan SystemNames'i de Packages'ı
	// da yoktur; aşağıdaki iki döngü de başarılı bir kurulumdan hemen sonra
	// bile "kurulu değil" der.
	if svc.ID == "roundcube" {
		return roundcubeInstalled()
	}
	if len(svc.SystemNames) > 0 {
		return a.firstPresentUnit(svc) != ""
	}
	for _, pkg := range svc.Packages[detectPkgFamily()] {
		if packageInstalled(pkg) {
			return true
		}
	}
	return false
}

// installedServiceSet is the catalogue-wide installed map, for requirement
// and conflict decisions.
// installedServiceSet, gereksinim ve çakışma kararları için katalog çapında
// kurulu haritasıdır.
func (a *Agent) installedServiceSet() map[string]bool {
	set := map[string]bool{}
	for i := range core.ManagedServices {
		if a.serviceInstalled(&core.ManagedServices[i]) {
			set[core.ManagedServices[i].ID] = true
		}
	}
	return set
}

// firstPresentUnit returns the first of a service's candidate systemd unit
// names that the system recognises, or "" if none is installed.
// firstPresentUnit, bir servisin aday systemd unit adlarından sistemin
// tanıdığı ilkini döndürür, hiçbiri kurulu değilse "".
// unitsMatching returns the installed unit names matching a catalogue pattern,
// newest version first. Listing the unit files and filtering here is what keeps
// versioned runtimes out of the catalogue's hardcoded name list — the panel
// learns which PHP versions exist from the machine, not from this file.
// unitsMatching, bir katalog desenine uyan kurulu unit adlarını en yeni sürüm
// başta döndürür. Unit dosyalarını listeleyip burada süzmek, sürümlü
// runtime'ları katalogdaki elle yazılmış ad listesinden kurtaran şeydir —
// panel hangi PHP sürümlerinin var olduğunu bu dosyadan değil makineden öğrenir.
func unitsMatching(pattern string) []string {
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	out, err := exec.Command("systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager").Output()
	if err != nil {
		return nil
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ".service")
		if re.MatchString(name) {
			units = append(units, name)
		}
	}
	sort.Slice(units, func(i, j int) bool {
		ai, an := versionOf(units[i])
		bi, bn := versionOf(units[j])
		if ai != bi {
			return ai > bi
		}
		return an > bn
	})
	return units
}

func (a *Agent) firstPresentUnit(svc *core.ManagedService) string {
	// Pattern matches are consulted after the explicit names but count just as
	// much: with only php8.3-fpm installed there is no plain `php-fpm` unit, and
	// without this the runtime would read as not installed at all.
	// Desen eşleşmeleri açık adlardan sonra bakılır ama aynı ölçüde geçerlidir:
	// yalnız php8.3-fpm kuruluyken düz bir `php-fpm` unit'i yoktur ve bu olmadan
	// runtime hiç kurulu değilmiş gibi okunurdu.
	for _, unit := range svc.SystemNames {
		// Instances of template units ("wg-quick@wg0") never appear in
		// list-unit-files — look their template ("wg-quick@") up instead.
		// Şablon unit örnekleri ("wg-quick@wg0") list-unit-files'ta hiç
		// görünmez — onun yerine şablonunu ("wg-quick@") ara.
		lookup := unit
		if at := strings.IndexByte(unit, '@'); at >= 0 {
			lookup = unit[:at+1]
		}
		out, err := exec.Command("systemctl", "list-unit-files", lookup+".service", "--no-legend").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return unit
		}
	}
	if units := unitsMatching(svc.SystemNamePattern); len(units) > 0 {
		return units[0] // newest version present
	}
	return ""
}
