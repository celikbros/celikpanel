package main

import (
	"fmt"
	"os/exec"
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

	if a.firstPresentUnit(svc) != "" {
		resp.Installed = false
		resp.Detail = svc.Name + " is already installed"
		return nil
	}

	family := detectPkgFamily()
	pkgs := svc.Packages[family]
	if len(pkgs) == 0 {
		resp.Error = fmt.Sprintf("%s cannot be installed automatically on this system yet", svc.Name)
		return nil
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
