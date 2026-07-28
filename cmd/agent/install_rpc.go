package main

import (
	"context"
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
	ServiceMutationBinding
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
	// Unit is the exact daemon unit started and verified by this operation.
	// It matters for versioned PostgreSQL packages, whose package name
	// (postgresql-17) is not their cluster unit (postgresql@17-main).
	// Unit, bu işlemin başlattığı ve doğruladığı tam daemon unit'idir. Paket adı
	// cluster unit'i olmayan sürümlü PostgreSQL paketlerinde önemlidir:
	// postgresql-17 paketinin gerçek hedefi postgresql@17-main'dir.
	Unit  string `json:"unit,omitempty"`
	Error string `json:"error,omitempty"`
}

// validateRepoPackageSelection accepts a caller-selected package only when the
// service catalogue explicitly declares a non-empty PackagePattern and that
// pattern matches the entire package name. This validation is intentionally
// pure so it runs before any package-manager probe.
//
// validateRepoPackageSelection, çağıranın seçtiği paketi yalnızca servis
// kataloğu boş olmayan bir PackagePattern tanımlıyorsa ve desen paket adının
// tamamıyla eşleşiyorsa kabul eder. Bu doğrulama bilerek saftır; paket
// yöneticisine herhangi bir sorgu yapılmadan önce çalışır.
func validateRepoPackageSelection(svc *core.ManagedService, packageName string) ([]string, error) {
	if svc == nil || svc.Repo == nil {
		return nil, fmt.Errorf("this service does not offer version selection")
	}
	pattern := strings.TrimSpace(svc.Repo.PackagePattern)
	if pattern == "" {
		return nil, fmt.Errorf("this service does not offer version selection")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid package pattern in catalogue")
	}
	match := re.FindStringSubmatch(packageName)
	if match == nil || match[0] != packageName {
		return nil, fmt.Errorf("%q is not a valid version package for %s", packageName, svc.Name)
	}
	return match, nil
}

// InstallService installs a managed service by its catalog ID, then enables +
// starts its systemd unit so it is actually running (and survives reboot)
// right after. Already-present services take the same idempotent preparation,
// configuration and readiness path so a partial install can be repaired.
// InstallService, katalog kimliğiyle yönetilen bir servisi kurar, sonra
// systemd unit'ini etkinleştirip başlatır; böylece hemen ardından gerçekten
// çalışır (ve reboot'tan sağ çıkar). Zaten kurulu servisler dürüstçe
// bildirilen bir no-op'tur.
func (a *Agent) InstallService(req *InstallServiceRequest, resp *InstallServiceResponse) error {
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	svc := core.GetManagedServiceByID(req.ID)
	if svc == nil {
		resp.Error = "unknown service"
		return nil
	}

	// Reject an arbitrary or non-versioned package before detectPkgFamily or
	// packageInstalled can invoke a package-manager command.
	// Keyfi ya da sürümlü olmayan paketi detectPkgFamily veya packageInstalled
	// bir paket-yöneticisi komutu çalıştırmadan önce reddet.
	var selectedPackageMatch []string
	if req.Package != "" {
		var err error
		selectedPackageMatch, err = validateRepoPackageSelection(svc, req.Package)
		if err != nil {
			resp.Error = err.Error()
			return nil
		}
	}

	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer finishStep()

	family := detectPkgFamily()
	if reason := core.ManagedServiceInstallDisabledReason(svc, family); reason != "" {
		resp.Error = reason
		return nil
	}

	// A version pick is validated before anything else, because it changes what
	// "already installed" even means.
	// Sürüm seçimi her şeyden önce doğrulanır, çünkü "zaten kurulu"nun ne
	// demek olduğunu değiştiren şey odur.
	var versionPrefix string
	if req.Package != "" {
		// Version selection comes from a managed vendor repo, which is apt-only
		// today. Without this the pick sails through validation and dies inside
		// pacman as "target not found: php8.3-fpm" — a package-manager error
		// shown to someone who asked the panel a product question.
		// Sürüm seçimi yönetilen bir vendor deposundan gelir ve bugün yalnız
		// apt'te vardır. Bu olmadan seçim doğrulamayı geçip pacman'in içinde
		// "target not found: php8.3-fpm" diye ölür — panele ürün sorusu soran
		// birine gösterilen bir paket-yöneticisi hatası.
		if family != "apt" {
			resp.Error = "choosing a version needs a managed repository, which is only supported on apt (Debian/Ubuntu) systems yet"
			return nil
		}
		if len(selectedPackageMatch) > 1 {
			versionPrefix = selectedPackageMatch[1]
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
	alreadyPresent := a.serviceInstalled(svc)
	if req.Package != "" {
		alreadyPresent = packageInstalled(req.Package)
	}

	// Requirements first: a dependent tool without its parent service would
	// install broken (phpMyAdmin with no MariaDB, no web server, no PHP). The
	// UI blocks this too; the agent is the enforcement that cannot be skipped.
	// Önce gereksinimler: üst servisi olmayan bağımlı araç bozuk kurulur
	// (MariaDB'siz, web sunucususuz, PHP'siz phpMyAdmin). UI da engeller;
	// atlatılamayan uygulayıcı agent'tır.
	installed := a.installedServiceSet()
	if missing := core.RequirementsMissing(svc, installed); len(missing) > 0 {
		resp.Error = fmt.Sprintf("%s requires %s — install that first from Services",
			svc.Name, strings.Join(missing, ", "))
		return nil
	}

	// The seat, enforced HERE and not only in the browser. Two members of a
	// seat bind the same port, so the second one installs and immediately dies
	// — proven live on Boston: with Redis holding 6379, a panel call installed
	// valkey-server and systemd gave up after five failed starts. The row said
	// "conflicts with Redis"; the API did it anyway.
	// Koltuk, yalnız tarayıcıda değil BURADA uygulanır. Bir koltuğun iki üyesi
	// aynı portu tutar; ikincisi kurulur ve anında ölür — Boston'da canlı
	// kanıtlandı: Redis 6379'u tutarken bir panel çağrısı valkey-server kurdu ve
	// systemd beş başarısız başlatmadan sonra pes etti. Satır "Redis ile
	// çakışıyor" diyordu; API yine de yaptı.
	if taken := core.SeatTakenBy(svc, installed); taken != "" {
		resp.Error = fmt.Sprintf("%s cannot run alongside %s — they do the same job and would fight over the same port. Remove %s first.",
			svc.Name, taken, taken)
		return nil
	}

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
	if family == "apt" && svc.Repo != nil && svc.Repo.Required {
		var repoStatus RepoStatusResponse
		if err := a.RepoStatus(&EnableRepoRequest{RepoID: svc.Repo.ID}, &repoStatus); err != nil {
			resp.Error = fmt.Sprintf("required repository status failed: %v", err)
			return nil
		}
		if repoStatus.Error != "" || !repoStatus.Enabled {
			resp.Error = fmt.Sprintf("%s requires the %s repository; enable it from Services first", svc.Name, svc.Repo.Name)
			return nil
		}
	}

	missingPackages := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		if !packageInstalled(pkg) {
			missingPackages = append(missingPackages, pkg)
		}
	}
	if len(missingPackages) > 0 {
		if _, err := installPackagesContext(ctx, family, missingPackages); err != nil {
			resp.Error = fmt.Sprintf("package install failed: %v", err)
			return nil
		}
	}
	if err := prepareInstalledServiceContext(ctx, req.ID, family); err != nil {
		resp.Error = fmt.Sprintf("service preparation failed: %v", err)
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
	if exactUnit, exact := exactInstallUnit(req.ID, family, req.Package); exact {
		// A selected package has one exact service target. Never fall back to
		// another installed PHP major, postgresql.service or another PG
		// cluster: that would report success while starting a different
		// version than the operator selected.
		//
		// Seçilen paketin tek bir tam servis hedefi vardır. Başka kurulu PHP
		// major'una, postgresql.service'e veya başka PG cluster'ına asla düşme;
		// aksi hâlde operatörün seçtiğinden farklı sürüm başlatılıp başarı
		// bildirilirdi.
		if exactUnit == "" || !unitExists(exactUnit) {
			resp.Error = fmt.Sprintf("selected package %s did not provide its exact service unit", req.Package)
			return nil
		}
		unit = exactUnit
	} else {
		unit = a.firstPresentUnit(svc)
	}
	if unit != "" {
		var err error
		if serviceStartsAfterPanelSetup(req.ID) {
			err = enableServiceForMutation(ctx, unit, false)
		} else {
			err = enableServiceForMutation(ctx, unit, true)
		}
		if err != nil {
			resp.Error = fmt.Sprintf("service did not become ready: %v", err)
			return nil
		}
		resp.Unit = unit
	}
	// Bridge daemons come in their own packages with their own units, and
	// nothing else starts them: SpamAssassin's spamd scores a message handed
	// to it, but spamass-milter is what hands Postfix's mail over. Leaving it
	// stopped produced "installed", "Running", zero mail filtered.
	// Köprü daemon'ları kendi paketlerinde, kendi unit'leriyle gelir ve onları
	// başka hiçbir şey başlatmaz: SpamAssassin'in spamd'si kendisine verilen
	// iletiyi puanlar ama Postfix'in postasını uzatan spamass-milter'dır. Onu
	// durmuş bırakmak "kurulu", "Çalışıyor", süzülen posta sıfır üretiyordu.
	for _, h := range svc.HelperUnits {
		if unitExists(h) {
			if err := enableServiceForMutation(ctx, h, true); err != nil {
				resp.Error = fmt.Sprintf("helper service did not become ready: %v", err)
				return nil
			}
		}
	}

	resp.Installed = !alreadyPresent || len(missingPackages) > 0
	if resp.Installed {
		resp.Detail = fmt.Sprintf("installed %s", strings.Join(pkgs, ", "))
	} else {
		resp.Detail = fmt.Sprintf("verified and repaired %s", svc.Name)
	}
	if len(skipped) > 0 {
		resp.Detail += fmt.Sprintf(" — not offered for this version: %s", strings.Join(skipped, ", "))
	}
	return nil
}

func serviceStartsAfterPanelSetup(serviceID string) bool {
	switch serviceID {
	case "pdns", "postfix", "dovecot", "wireguard":
		return true
	default:
		return false
	}
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

// unitCanonicalName asks systemd what a unit name really resolves to. For a
// plain unit the answer is itself; for an alias it is the target
// ("redis.service" -> "valkey.service"). Returns "" when systemd cannot say.
// unitCanonicalName, bir unit adının gerçekte neye çözüldüğünü systemd'ye
// sorar. Düz bir unit için cevap kendisidir; takma ad için hedeftir
// ("redis.service" -> "valkey.service"). systemd söyleyemezse "" döner.
func unitCanonicalName(unit string) string {
	out, err := exec.Command("systemctl", "show", unit+".service", "-p", "Id", "--value").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), ".service")
}

// unitProvesInstalled decides whether the presence of `unit` really proves
// this component is installed, given what systemd says the name resolves to.
// Separated from the exec call so the RULE can be tested without systemd.
//
// - canonical == "" : systemd could not say; trust the name (a plain unit).
// - canonical == unit : a real unit of its own. Installed.
// - canonical is one of OUR names : still ours, just a second name for it.
// - otherwise : the name is an alias for somebody else's unit. Not ours.
//
// unitProvesInstalled, `unit`in varlığının bu bileşenin kurulu olduğunu
// gerçekten kanıtlayıp kanıtlamadığına, systemd'nin adı neye çözdüğüne bakarak
// karar verir. exec çağrısından ayrıdır ki KURAL systemd olmadan test
// edilebilsin.
func unitProvesInstalled(unit, canonical string, systemNames []string) bool {
	if canonical == "" || canonical == unit {
		return true
	}
	return containsString(systemNames, canonical)
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
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
	Removed         bool   `json:"removed"`
	Detail          string `json:"detail,omitempty"`
	Error           string `json:"error,omitempty"`
	PartialSuccess  bool   `json:"partial_success,omitempty"`
	MutationApplied bool   `json:"mutation_applied,omitempty"`
}

// serviceUninstallOps keeps privileged host operations behind one narrow
// boundary. Production supplies fixed argv commands; tests supply recorders so
// partial-mutation and package-selection contracts are exercised without
// touching the test host.
// serviceUninstallOps, yetkili makine işlemlerini tek ve dar bir sınırın
// arkasında tutar. Üretim sabit argv komutları, testler ise test makinesine
// dokunmadan kısmi değişiklik ve paket seçimi sözleşmelerini sınayan
// kaydediciler sağlar.
type serviceUninstallOps struct {
	detectPackageFamily   func() string
	packageInstalled      func(string) bool
	unitExists            func(string) bool
	unitsMatching         func(string) []string
	disableUnit           func(string) error
	removePackages        func(string, []string) (string, error)
	installedRepoPackages func(*core.ManagedService) ([]string, error)
}

func defaultServiceUninstallOps(ctx context.Context) serviceUninstallOps {
	return serviceUninstallOps{
		detectPackageFamily: detectPkgFamily,
		packageInstalled:    packageInstalled,
		unitExists:          unitExists,
		unitsMatching:       unitsMatching,
		disableUnit: func(unit string) error {
			_, err := runServiceMutationCombinedOutput(ctx, "systemctl", "disable", "--now", unit)
			return err
		},
		removePackages: func(family string, packages []string) (string, error) {
			return removePackagesContext(ctx, family, packages)
		},
		installedRepoPackages: func(service *core.ManagedService) ([]string, error) {
			return installedRepoPackagesForServiceContext(ctx, service)
		},
	}
}

// failUninstallRemoval preserves the critical distinction between a refusal
// before mutation and a package purge that failed after units were stopped.
// failUninstallRemoval, değişiklik öncesi ret ile unit'ler durdurulduktan sonra
// başarısız olan paket kaldırmayı birbirinden ayıran kritik bilgiyi korur.
func failUninstallRemoval(resp *UninstallServiceResponse, err error, mutationApplied bool) {
	resp.Error = fmt.Sprintf("package removal failed: %v", err)
	resp.PartialSuccess = mutationApplied
	resp.MutationApplied = mutationApplied
}

// failUninstallDisable is deliberately fail-closed. `systemctl disable --now`
// may stop a unit before a later disable step fails, so every attempted
// stop/disable error means the host state may already have changed. Package
// removal must not continue from that uncertain state.
// failUninstallDisable bilerek güvenli biçimde kapalı davranır. `systemctl
// disable --now`, daha sonraki devre dışı bırakma adımı başarısız olmadan önce
// unit'i durdurmuş olabilir. Bu nedenle denenen her stop/disable hatası makine
// durumunun değişmiş olabileceği anlamına gelir; paket kaldırma sürdürülmez.
func failUninstallDisable(resp *UninstallServiceResponse, unit string, err error) {
	resp.Error = fmt.Sprintf(
		"failed to stop and disable %s; service state may have changed: %v",
		unit,
		err,
	)
	resp.PartialSuccess = true
	resp.MutationApplied = true
}

func uniquePackageNames(packages []string) []string {
	seen := make(map[string]struct{}, len(packages))
	out := make([]string, 0, len(packages))
	for _, raw := range packages {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
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
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	svc := core.GetManagedServiceByID(req.ID)
	if svc == nil {
		resp.Error = "unknown service"
		return nil
	}
	// Reject an invalid catalogue/package selection before asking for a
	// privileged lease. This is pure input validation and mirrors InstallService.
	// Geçersiz katalog/paket seçimini ayrıcalıklı lease istemeden önce reddet.
	// Bu yalnızca girdi doğrulamasıdır ve InstallService akışını yansıtır.
	if req.Package != "" {
		if _, err := validateRepoPackageSelection(svc, req.Package); err != nil {
			resp.Error = err.Error()
			return nil
		}
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer finishStep()
	return a.uninstallServiceWithOps(req, resp, defaultServiceUninstallOps(ctx))
}

func (a *Agent) uninstallServiceWithOps(
	req *InstallServiceRequest,
	resp *UninstallServiceResponse,
	ops serviceUninstallOps,
) error {
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	svc := core.GetManagedServiceByID(req.ID)
	if svc == nil {
		resp.Error = "unknown service"
		return nil
	}

	// Apply the same catalogue boundary as install before any package-manager
	// detection or installed-package query.
	// Paket-yöneticisi algılama veya kurulu-paket sorgusundan önce kurulumla aynı
	// katalog sınırını uygula.
	var selectedPackageMatch []string
	if req.Package != "" {
		var err error
		selectedPackageMatch, err = validateRepoPackageSelection(svc, req.Package)
		if err != nil {
			resp.Error = err.Error()
			return nil
		}
	}

	family := ops.detectPackageFamily()
	mutationApplied := false

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
		if family != "apt" {
			resp.Error = "per-version removal is only supported on apt-based systems"
			return nil
		}
		if !ops.packageInstalled(req.Package) {
			resp.Removed = true
			resp.Detail = fmt.Sprintf("%s is not installed", req.Package)
			return nil
		}
		versionPrefix := ""
		if len(selectedPackageMatch) > 1 {
			versionPrefix = selectedPackageMatch[1]
		}
		pkgs := []string{req.Package}
		for _, tpl := range svc.Repo.VersionCompanions {
			name := strings.ReplaceAll(tpl, "{v}", versionPrefix)
			if ops.packageInstalled(name) {
				pkgs = append(pkgs, name)
			}
		}
		// Resolve the exact daemon target independently from the package name;
		// PostgreSQL's postgresql-17 package owns postgresql@17-main.
		// Tam daemon hedefini paket adından bağımsız çöz; PostgreSQL'in
		// postgresql-17 paketi postgresql@17-main unit'ine sahiptir.
		if exactUnit, exact := exactInstallUnit(req.ID, family, req.Package); exact {
			if exactUnit == "" {
				resp.Error = "selected package has no valid exact service unit"
				return nil
			}
			if ops.unitExists(exactUnit) {
				if err := ops.disableUnit(exactUnit); err != nil {
					failUninstallDisable(resp, exactUnit, err)
					return nil
				}
				mutationApplied = true
			}
		}
		pkgs = uniquePackageNames(pkgs)
		if _, err := ops.removePackages(family, pkgs); err != nil {
			failUninstallRemoval(resp, err, mutationApplied)
			return nil
		}
		resp.Removed = true
		resp.Detail = fmt.Sprintf("removed %s", strings.Join(pkgs, ", "))
		return nil
	}

	pkgs := append([]string(nil), svc.Packages[family]...)
	if len(pkgs) == 0 {
		resp.Error = fmt.Sprintf("%s cannot be removed automatically on this system yet", svc.Name)
		return nil
	}

	// Discover catalogue-matching apt packages before stopping anything. The
	// query is package-manager truth, not a systemd-name guess: in particular,
	// postgresql@17-main must add postgresql-17, never the unit string itself.
	// Herhangi bir şeyi durdurmadan önce katalogla eşleşen apt paketlerini bul.
	// Sorgu systemd adı tahmini değil, paket yöneticisi gerçeğidir; özellikle
	// postgresql@17-main, unit dizgesini değil postgresql-17 paketini eklemelidir.
	if family == "apt" && svc.Repo != nil && svc.Repo.PackagePattern != "" {
		repoPackages, err := ops.installedRepoPackages(svc)
		if err != nil {
			resp.Error = fmt.Sprintf("installed package discovery failed: %v", err)
			return nil
		}
		pkgs = append(pkgs, repoPackages...)
		re, err := regexp.Compile(svc.Repo.PackagePattern)
		if err != nil {
			resp.Error = "invalid catalogue package pattern"
			return nil
		}
		for _, pkg := range repoPackages {
			if match := re.FindStringSubmatch(pkg); len(match) > 1 {
				for _, tpl := range svc.Repo.VersionCompanions {
					companion := strings.ReplaceAll(tpl, "{v}", match[1])
					if ops.packageInstalled(companion) {
						pkgs = append(pkgs, companion)
					}
				}
			}
		}
	}
	pkgs = uniquePackageNames(pkgs)

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
	for _, unit := range append(append([]string{}, svc.SystemNames...), svc.HelperUnits...) {
		// Catalogue entries include distribution-specific alternatives (for
		// example redis-server/redis). Only an existing unit is an attempted
		// mutation; once attempted, any error is state-uncertain and aborts.
		// Katalog girdileri dağıtıma özgü alternatifler içerir (örneğin
		// redis-server/redis). Yalnız var olan bir unit için değişiklik denenir;
		// denendikten sonra her hata durumu belirsiz sayılır ve işlem kesilir.
		if !ops.unitExists(unit) {
			continue
		}
		if err := ops.disableUnit(unit); err != nil {
			failUninstallDisable(resp, unit, err)
			return nil
		}
		mutationApplied = true
	}
	if svc.SystemNamePattern != "" {
		for _, unit := range ops.unitsMatching(svc.SystemNamePattern) {
			if err := ops.disableUnit(unit); err != nil {
				failUninstallDisable(resp, unit, err)
				return nil
			}
			mutationApplied = true
		}
	}

	if _, err := ops.removePackages(family, pkgs); err != nil {
		failUninstallRemoval(resp, err, mutationApplied)
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
			// A unit name that is an ALIAS of some other component's unit does
			// not prove this component is installed. Arch's valkey package
			// ships /usr/lib/systemd/system/redis.service as a symlink to
			// valkey.service (it declares Provides: redis), so installing
			// Valkey made the panel report "Redis: installed, dead" — a
			// component that was never installed, wearing someone else's name.
			// Ask systemd for the canonical name instead of trusting the link.
			// Başka bir bileşenin unit'ine TAKMA AD olan bir unit adı, bu
			// bileşenin kurulu olduğunu kanıtlamaz. Arch'ın valkey paketi
			// /usr/lib/systemd/system/redis.service'i valkey.service'e sembolik
			// bağ olarak koyar (Provides: redis der); bu yüzden Valkey kurmak
			// paneli "Redis: kurulu, ölü" demeye itiyordu — hiç kurulmamış bir
			// bileşen, başkasının adını taşıyarak. Bağa güvenmek yerine asıl
			// adı systemd'ye sor.
			if !unitProvesInstalled(unit, unitCanonicalName(unit), svc.SystemNames) {
				continue
			}
			return unit
		}
	}
	if units := unitsMatching(svc.SystemNamePattern); len(units) > 0 {
		return units[0] // newest version present
	}
	return ""
}
