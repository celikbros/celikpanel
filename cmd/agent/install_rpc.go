package main

import (
	"fmt"
	"os/exec"
	"regexp"
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

	if a.serviceInstalled(svc) {
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
	if req.Package != "" {
		if svc.Repo == nil {
			resp.Error = "this service does not offer version selection"
			return nil
		}
		if ok, _ := regexp.MatchString(svc.Repo.PackagePattern, req.Package); !ok {
			resp.Error = fmt.Sprintf("%q is not a valid version package for %s", req.Package, svc.Name)
			return nil
		}
		pkgs = []string{req.Package}
	}

	if _, err := installPackages(family, pkgs); err != nil {
		resp.Error = fmt.Sprintf("package install failed: %v", err)
		return nil
	}

	if unit := a.firstPresentUnit(svc); unit != "" {
		_ = exec.Command("systemctl", "enable", "--now", unit).Run()
	}

	resp.Installed = true
	resp.Detail = fmt.Sprintf("installed %s", strings.Join(pkgs, ", "))
	return nil
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
	pkgs := svc.Packages[family]
	if len(pkgs) == 0 {
		resp.Error = fmt.Sprintf("%s cannot be removed automatically on this system yet", svc.Name)
		return nil
	}

	// Stop + disable every present unit first, so purge does not fight a
	// running process. Template instances ("wg-quick@wg0") are handled by
	// their exact SystemNames entry.
	// Önce mevcut her unit'i durdur + devre dışı bırak ki purge çalışan bir
	// süreçle boğuşmasın.
	for _, unit := range svc.SystemNames {
		_ = exec.Command("systemctl", "disable", "--now", unit).Run()
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
func (a *Agent) firstPresentUnit(svc *core.ManagedService) string {
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
	return ""
}
