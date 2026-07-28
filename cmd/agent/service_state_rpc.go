package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

// InstalledServiceIDs reports which catalogue services have their package
// present, independent of whether the unit is currently running. This closes
// the gap where a service is installed but not started — most visibly
// WireGuard, whose wg-quick@wg0 template unit only starts once a config
// exists, so a "running units" scan alone would keep calling it "not
// installed". firstPresentUnit already handles template units via
// list-unit-files, so we reuse it here.
//
// InstalledServiceIDs, hangi katalog servislerinin paketinin var olduğunu
// bildirir — unit'in şu an çalışıp çalışmadığından bağımsız. Bu, bir servis
// kurulu ama başlatılmamışken oluşan boşluğu kapatır — en görünür örnek
// WireGuard: wg-quick@wg0 şablon unit'i ancak bir config varken başlar, bu
// yüzden yalnız "çalışan unit" taraması onu sürekli "kurulu değil" sanardı.
func (a *Agent) InstalledServiceIDs(_ *struct{}, reply *[]string) error {
	var ids []string
	for i := range core.ManagedServices {
		svc := &core.ManagedServices[i]
		// serviceInstalled also covers daemonless tools (no unit, package
		// presence decides) — phpMyAdmin and friends.
		// serviceInstalled, daemon'suz araçları da kapsar (unit yok, paket
		// varlığı belirler) — phpMyAdmin ve benzerleri.
		if a.serviceInstalled(svc) {
			ids = append(ids, svc.ID)
		}
	}
	*reply = ids
	return nil
}

// strictServiceStateProbe is the fail-closed discovery surface used only for
// firewall policy calculation. The ordinary Services scan remains tolerant;
// a firewall refresh cannot be tolerant because an incomplete installed set
// would close ports that active services still need.
//
// strictServiceStateProbe yalnız güvenlik duvarı politikası hesabında kullanılan
// hata-kapalı keşif yüzeyidir. Normal Servisler taraması toleranslı kalır;
// firewall yenilemesi toleranslı olamaz çünkü eksik kurulu seti, çalışan
// servislerin hâlâ ihtiyaç duyduğu portları kapatır.
type strictServiceStateProbe interface {
	UnitFiles() (map[string]struct{}, error)
	CanonicalUnit(string) (string, error)
	InstalledPackages(string) (map[string]struct{}, error)
}

type hostStrictServiceStateProbe struct{}

func (hostStrictServiceStateProbe) UnitFiles() (map[string]struct{}, error) {
	out, err := exec.Command(
		"systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("list systemd unit files: %w", err)
	}
	units := make(map[string]struct{})
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		units[strings.TrimSuffix(fields[0], ".service")] = struct{}{}
	}
	return units, nil
}

func (hostStrictServiceStateProbe) CanonicalUnit(unit string) (string, error) {
	out, err := exec.Command("systemctl", "show", unit+".service", "-p", "Id", "--value").Output()
	if err != nil {
		return "", fmt.Errorf("resolve systemd unit %s: %w", unit, err)
	}
	canonical := strings.TrimSuffix(strings.TrimSpace(string(out)), ".service")
	if canonical == "" {
		return "", fmt.Errorf("resolve systemd unit %s: empty canonical name", unit)
	}
	return canonical, nil
}

func (hostStrictServiceStateProbe) InstalledPackages(family string) (map[string]struct{}, error) {
	var out []byte
	var err error
	switch family {
	case "apt":
		out, err = exec.Command(
			"dpkg-query", "-W", "-f=${binary:Package}\t${Status}\n",
		).Output()
	case "pacman":
		out, err = exec.Command("pacman", "-Qq").Output()
	case "dnf":
		out, err = exec.Command("rpm", "-qa", "--qf", "%{NAME}\n").Output()
	default:
		return nil, fmt.Errorf("query installed packages: unsupported package family %q", family)
	}
	if err != nil {
		return nil, fmt.Errorf("query installed packages with %s: %w", family, err)
	}

	packages := make(map[string]struct{})
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name := line
		if family == "apt" {
			fields := strings.Fields(line)
			if len(fields) < 4 || strings.Join(fields[len(fields)-3:], " ") != "install ok installed" {
				continue
			}
			name = fields[0]
			if colon := strings.IndexByte(name, ':'); colon > 0 {
				name = name[:colon]
			}
		}
		packages[name] = struct{}{}
	}
	return packages, nil
}

// InstalledServiceIDsStrict is the firewall-safe counterpart of
// InstalledServiceIDs. Probe failures are returned to the panel instead of
// being converted into an incomplete "not installed" answer.
//
// InstalledServiceIDsStrict, InstalledServiceIDs'nin firewall için güvenli
// karşılığıdır. Yoklama hataları eksik bir "kurulu değil" cevabına çevrilmez,
// panele geri döndürülür.
func (a *Agent) InstalledServiceIDsStrict(_ *struct{}, reply *[]string) error {
	ids, err := discoverInstalledServiceIDsStrict(hostStrictServiceStateProbe{}, detectPkgFamily())
	if err != nil {
		return err
	}
	*reply = ids
	return nil
}

func discoverInstalledServiceIDsStrict(probe strictServiceStateProbe, family string) ([]string, error) {
	units, err := probe.UnitFiles()
	if err != nil {
		return nil, fmt.Errorf("strict service discovery: %w", err)
	}

	var packages map[string]struct{}
	packagesLoaded := false
	var ids []string
	for i := range core.ManagedServices {
		svc := &core.ManagedServices[i]
		installed := false

		if svc.ID == "roundcube" {
			installed = roundcubeInstalled()
		} else if len(svc.SystemNames) > 0 || svc.SystemNamePattern != "" {
			for _, unit := range svc.SystemNames {
				lookup := unit
				if at := strings.IndexByte(unit, '@'); at >= 0 {
					lookup = unit[:at+1]
				}
				if _, ok := units[lookup]; !ok {
					continue
				}
				canonical, err := probe.CanonicalUnit(unit)
				if err != nil {
					return nil, fmt.Errorf("strict service discovery for %s: %w", svc.ID, err)
				}
				if unitProvesInstalled(unit, canonical, svc.SystemNames) {
					installed = true
					break
				}
			}
			if !installed && svc.SystemNamePattern != "" {
				re, err := regexp.Compile(svc.SystemNamePattern)
				if err != nil {
					return nil, fmt.Errorf("strict service discovery for %s: invalid unit pattern: %w", svc.ID, err)
				}
				for unit := range units {
					if re.MatchString(unit) {
						installed = true
						break
					}
				}
			}
		} else if candidates := svc.Packages[family]; len(candidates) > 0 {
			if !packagesLoaded {
				packages, err = probe.InstalledPackages(family)
				if err != nil {
					return nil, fmt.Errorf("strict service discovery: %w", err)
				}
				packagesLoaded = true
			}
			for _, candidate := range candidates {
				if _, ok := packages[candidate]; ok {
					installed = true
					break
				}
			}
		}

		if installed {
			ids = append(ids, svc.ID)
		}
	}
	return ids, nil
}
