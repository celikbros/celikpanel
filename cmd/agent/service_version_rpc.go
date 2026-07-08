package main

import (
	"os/exec"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

// ServiceCandidateVersion reports the version apt would install for a
// catalogue service right now — the honest "what will land" for the install
// modal. It reads the Candidate line of `apt-cache policy` for the service's
// primary package. Empty when unknown (no apt, package not in the index).
//
// ServiceCandidateVersion, apt'ın bir katalog servisi için şu an kuracağı
// sürümü bildirir — kurulum modalı için dürüst "ne inecek". Servisin birincil
// paketi için `apt-cache policy`'nin Candidate satırını okur. Bilinmiyorsa
// boştur (apt yok, paket dizinde değil).
func (a *Agent) ServiceCandidateVersion(req *InstallServiceRequest, reply *string) error {
	svc := core.GetManagedServiceByID(req.ID)
	if svc == nil {
		return nil
	}
	pkgs := svc.Packages[detectPkgFamily()]
	if len(pkgs) == 0 {
		return nil
	}
	out, err := exec.Command("apt-cache", "policy", pkgs[0]).Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Candidate:"); ok {
			v = strings.TrimSpace(v)
			if v == "(none)" {
				return nil
			}
			*reply = cleanAptVersion(v)
			return nil
		}
	}
	return nil
}

// cleanAptVersion trims apt's version to something human: drops the epoch
// ("2:8.3+..." → "8.3+...") and the Debian revision after the first "-" or
// "+", so "16+257build1.1" → "16", "1.24.0-2ubuntu7" → "1.24.0".
// cleanAptVersion, apt sürümünü insanileştirir: epoch'u atar ve ilk "-"/"+"
// sonrasını keser.
func cleanAptVersion(v string) string {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		v = v[i+1:]
	}
	if i := strings.IndexAny(v, "-+~"); i >= 0 {
		v = v[:i]
	}
	return v
}
