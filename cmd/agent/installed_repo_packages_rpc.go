package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// InstalledRepoPackagesRequest identifies a catalogue service. The caller
// never supplies a package regexp: the agent resolves the service and its
// trusted PackagePattern from its own compiled catalogue.
// InstalledRepoPackagesRequest, katalogdaki bir servisi tanımlar. Çağıran paket
// düzenli ifadesi vermez; agent servisi ve güvenilen PackagePattern değerini
// kendi derlenmiş kataloğundan çözer.
type InstalledRepoPackagesRequest = transport.InstalledRepoPackagesRequest

// InstalledRepoPackagesResponse reports installed packages which exactly
// match the selected service's catalogue PackagePattern.
// InstalledRepoPackagesResponse, seçilen servisin katalog PackagePattern
// değeriyle tam eşleşen kurulu paketleri bildirir.
type InstalledRepoPackagesResponse = transport.InstalledRepoPackagesResponse

// InstalledRepoPackages gives the panel a package-level repair identity even
// when a versioned service is stopped and therefore has no useful active unit.
// The command is a fixed program plus argv (never a shell command), and both
// the service id and regexp are re-derived inside the privileged agent.
// InstalledRepoPackages, sürümlü servis durmuş ve yararlı bir etkin unit yokken
// bile panele paket düzeyinde onarım kimliği verir. Komut sabit program ve argv
// biçimindedir, hiçbir zaman shell komutu değildir; servis kimliğiyle desen
// yetkili agent içinde yeniden türetilir.
func (a *Agent) InstalledRepoPackages(req *InstalledRepoPackagesRequest, resp *InstalledRepoPackagesResponse) error {
	resp.Packages = []string{}
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		resp.Error = fmt.Sprintf("detect host platform: %v", err)
		return nil
	}
	service := core.GetManagedServiceByID(strings.TrimSpace(req.ServiceID))
	if service == nil || service.Repo == nil || service.Repo.PackagePattern == "" {
		resp.Error = "service has no versioned package catalogue"
		return nil
	}
	family := string(profile.PackageManager)
	var executable string
	switch family {
	case "apt":
		executable, err = executableForProfile(profile, family, "dpkg-query")
	case "dnf":
		executable, err = executableForProfile(profile, family, "rpm")
	default:
		resp.Error = fmt.Sprintf("installed version package discovery is not supported on %s systems", family)
		return nil
	}
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	packages, err := installedRepoPackagesForServiceWithFamilyExecutable(context.Background(), service, family, executable)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Packages = packages
	return nil
}

// installedRepoPackagesForService resolves installed package names from the
// catalogue-owned pattern. It is shared by scan/repair identity and uninstall,
// so a systemd instance such as postgresql@17-main is never treated as the
// package that apt must purge.
// installedRepoPackagesForService, kurulu paket adlarını kataloğun sahip olduğu
// desenden çözer. Tarama/onarım kimliği ile kaldırma bunu paylaşır; böylece
// postgresql@17-main gibi bir systemd instance'ı apt'nin kaldıracağı paket
// sanılmaz.
func installedRepoPackagesForService(service *core.ManagedService) ([]string, error) {
	return installedRepoPackagesForServiceContext(context.Background(), service)
}

func installedRepoPackagesForServiceContext(ctx context.Context, service *core.ManagedService) ([]string, error) {
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return nil, err
	}
	family := string(profile.PackageManager)
	var executable string
	switch family {
	case "apt":
		executable, err = executableForProfile(profile, family, "dpkg-query")
	case "dnf":
		executable, err = executableForProfile(profile, family, "rpm")
	default:
		return nil, fmt.Errorf("installed version package discovery is not supported on %s systems", family)
	}
	if err != nil {
		return nil, err
	}
	return installedRepoPackagesForServiceWithFamilyExecutable(ctx, service, family, executable)
}

func installedRepoPackagesForServiceWithExecutable(ctx context.Context, service *core.ManagedService, dpkgQuery string) ([]string, error) {
	return installedRepoPackagesForServiceWithFamilyExecutable(ctx, service, "apt", dpkgQuery)
}

func installedRepoPackagesForServiceWithFamilyExecutable(ctx context.Context, service *core.ManagedService, family, executable string) ([]string, error) {
	if service == nil || service.Repo == nil || service.Repo.PackagePattern == "" {
		return nil, fmt.Errorf("service has no versioned package catalogue")
	}
	pattern, err := regexp.Compile(service.Repo.PackagePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid catalogue package pattern")
	}
	switch family {
	case "apt":
		out, err := serviceMutationCommand(ctx,
			executable,
			"-W",
			"-f=${Package}\t${db:Status-Abbrev}\n",
		).Output()
		if err != nil {
			return nil, fmt.Errorf("dpkg-query failed: %v", err)
		}
		return installedPackagesMatchingPattern(string(out), pattern), nil
	case "dnf":
		out, err := serviceMutationCommand(ctx,
			executable,
			rpmInventoryArgs()...,
		).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("rpm inventory failed: %v: %s", err, firstLine(string(out)))
		}
		return installedRPMPackagesMatchingPattern(string(out), pattern), nil
	default:
		return nil, fmt.Errorf("installed version package discovery is not supported on %s systems", family)
	}
}

func rpmInventoryArgs() []string {
	return []string{
		"-qa",
		"--qf",
		"CELIKPANEL_PACKAGE:%{NAME}\n",
	}
}

func installedRPMPackagesMatchingPattern(output string, pattern *regexp.Regexp) []string {
	if pattern == nil {
		return []string{}
	}
	const prefix = "CELIKPANEL_PACKAGE:"
	seen := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if !validPackageName(name) || pattern.FindString(name) != name {
			continue
		}
		seen[name] = struct{}{}
	}
	packages := make([]string, 0, len(seen))
	for name := range seen {
		packages = append(packages, name)
	}
	sort.Slice(packages, func(i, j int) bool {
		ai, an := versionOf(packages[i])
		bi, bn := versionOf(packages[j])
		if ai != bi {
			return ai > bi
		}
		return an > bn
	})
	return packages
}

// installedPackagesMatchingPattern parses dpkg-query's package/status rows,
// accepts packages whose desired action is install and whose current state is
// installed or recoverable by another apt install, then applies an exact
// regexp match. Removed/config-only packages are deliberately not proof.
//
// installedPackagesMatchingPattern, dpkg-query paket/durum satırlarını ayrıştırır;
// istenen işlemi kurulum olan ve mevcut durumu kurulu ya da yeni bir apt
// kurulumuyla onarılabilir paketleri kabul eder. Silinmiş/yapılandırma-artığı
// paketler bilerek kanıt sayılmaz.
func installedPackagesMatchingPattern(output string, pattern *regexp.Regexp) []string {
	if pattern == nil {
		return []string{}
	}
	seen := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 || !dpkgRecoverableInstallState(fields[1]) {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name == "" || pattern.FindString(name) != name {
			continue
		}
		seen[name] = struct{}{}
	}
	packages := make([]string, 0, len(seen))
	for name := range seen {
		packages = append(packages, name)
	}
	sort.Slice(packages, func(i, j int) bool {
		ai, an := versionOf(packages[i])
		bi, bn := versionOf(packages[j])
		if ai != bi {
			return ai > bi
		}
		return an > bn
	})
	return packages
}

// dpkgRecoverableInstallState reads the first two characters of
// ${db:Status-Abbrev}: desired action, then current package state. Only
// desired=install is ours to repair. U/F/H/W/T (and dpkg's lowercase t) are
// incomplete states that apt can resume; n/c and remove/purge/hold desires are
// excluded so stale package metadata cannot enable Repair.
//
// dpkgRecoverableInstallState, ${db:Status-Abbrev} değerinin ilk iki
// karakterini okur: istenen işlem ve mevcut paket durumu. Yalnız
// desired=install durumunu onarırız. U/F/H/W/T (ve dpkg'nin küçük t'si) apt'nin
// sürdürebileceği eksik durumlardır; n/c ile remove/purge/hold istekleri,
// eski paket metadatası Repair'i açamasın diye dışarıda bırakılır.
func dpkgRecoverableInstallState(status string) bool {
	status = strings.TrimSpace(status)
	if len(status) < 2 || status[0] != 'i' {
		return false
	}
	switch status[1] {
	case 'i', 'U', 'F', 'H', 'W', 'T', 't':
		return true
	default:
		return false
	}
}
