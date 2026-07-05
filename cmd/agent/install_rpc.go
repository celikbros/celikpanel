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

// firstPresentUnit returns the first of a service's candidate systemd unit
// names that the system recognises, or "" if none is installed.
// firstPresentUnit, bir servisin aday systemd unit adlarından sistemin
// tanıdığı ilkini döndürür, hiçbiri kurulu değilse "".
func (a *Agent) firstPresentUnit(svc *core.ManagedService) string {
	for _, unit := range svc.SystemNames {
		out, err := exec.Command("systemctl", "list-unit-files", unit+".service", "--no-legend").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return unit
		}
	}
	return ""
}
