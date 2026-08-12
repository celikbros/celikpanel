package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Package managers own one machine-wide database lock. Serialize every panel
// package mutation so browser tabs and API clients cannot race pacman or apt.
var packageOperationMu sync.Mutex

var errDNFMutationCertificationPending = fmt.Errorf("DNF package mutation is pending per-service and per-distribution certification; no package changes were made")

// Package-manager abstraction so service installation is not hard-wired to
// one distro. The central host-platform detector selects a distribution family
// from os-release and then verifies that family's required tools; each manager
// knows how to install a set of packages non-interactively. apt is the first-class
// tested family; pacman (Arch) is supported as the dev-test target the
// operator keeps a second server on (D-004 amendment) — for services whose
// catalog entry carries pacman package names. The dnf transaction core is a
// bounded preview: the catalogue remains the allowlist for which services may
// use it, so recognising a RHEL-family host does not claim broad support.
//
// Paket-yöneticisi soyutlaması; servis kurulumu tek dağıtıma gömülü değildir.
// Merkezi platform dedektörü aileyi os-release içindeki tam kimliklerden seçer;
// ardından o aile için gereken araçların doğrulanmış mutlak yollarını ve canlı
// systemd yöneticisini denetler. Her aile bir paket kümesini etkileşimsiz kurmayı bilir. apt
// (Ubuntu/Debian) birinci sınıf test edilmiş ailedir; pacman (Arch),
// operatörün ikinci sunucuyu üzerinde tuttuğu geliştirme-test hedefi olarak
// desteklenir (D-004 eki) — katalog girdisi pacman paket adı taşıyan
// servisler için. dnf işlem çekirdeği sınırlı bir önizlemedir; hangi servisin
// bunu kullanabileceğine katalog izin listesi karar verir. Bir RHEL ailesini
// tanımak geniş özellik desteği iddiası değildir.

var detectHostPlatform = hostplatform.Detect

func verifiedHostProfileForAnyFamily() (hostplatform.Profile, error) {
	profile, err := detectHostPlatform()
	if err != nil {
		return hostplatform.Profile{}, fmt.Errorf("detect host platform: %w", err)
	}
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT, hostplatform.PackageManagerDNF, hostplatform.PackageManagerPacman:
		return profile, nil
	default:
		return hostplatform.Profile{}, fmt.Errorf("unsupported package manager %q", profile.PackageManager)
	}
}

func verifiedHostProfile(family string) (hostplatform.Profile, error) {
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return hostplatform.Profile{}, err
	}
	if string(profile.PackageManager) != family {
		return hostplatform.Profile{}, fmt.Errorf("host package manager changed from %s to %s", family, profile.PackageManager)
	}
	return profile, nil
}

func executableForProfile(profile hostplatform.Profile, family, name string) (string, error) {
	if string(profile.PackageManager) != family {
		return "", fmt.Errorf("host package manager is %s, want %s", profile.PackageManager, family)
	}
	path := profile.Executables[name]
	if path == "" {
		return "", fmt.Errorf("host executable %s has not been verified", name)
	}
	return path, nil
}

// detectPkgFamily is the compatibility boundary for existing package code. It
// returns a manager only after release, binaries, and systemd are verified.
// Detection failures fail closed.
func detectPkgFamily() string {
	profile, err := detectHostPlatform()
	if err != nil {
		return ""
	}
	return string(profile.PackageManager)
}

// aptListsDir is where apt keeps downloaded package lists; their newest mtime
// is when `apt-get update` last succeeded. Variable for tests.
// aptListsDir, apt'ın indirilmiş paket listelerini tuttuğu yerdir; en yeni
// mtime'ları `apt-get update`in en son ne zaman başarıldığıdır. Test için değişken.
var aptListsDir = "/var/lib/apt/lists"

// aptListsAge returns how long ago the package lists were refreshed; ok=false
// when that cannot be determined (no lists yet → treat as stale).
// aptListsAge, paket listelerinin en son ne kadar önce tazelendiğini döndürür;
// belirlenemiyorsa ok=false (hiç liste yok → bayat say).
func aptListsAge() (time.Duration, bool) {
	entries, err := os.ReadDir(aptListsDir)
	if err != nil {
		return 0, false
	}
	var newest time.Time
	for _, e := range entries {
		if e.IsDir() { // partial/, auxfiles/
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		return 0, false
	}
	return time.Since(newest), true
}

// refreshAptListsIfStale runs `apt-get update` when the lists are older than
// maxAge (operator suggestion). Stale lists are the classic install killer:
// mirrors delete a package file the moment a newer version replaces it, so an
// old list requests a file that is gone (404) — and the "version to install"
// we showed the operator may no longer be the truth. Fresh-enough lists skip
// the cost, so a burst of installs refreshes only once. Best-effort: a broken
// third-party repo must not veto an install the base archives can satisfy.
// refreshAptListsIfStale, listeler maxAge'den eskiyse `apt-get update` koşar
// (operatör önerisi). Bayat liste kurulumların klasik katilidir: aynalar,
// paketin yenisi gelince eski dosyayı siler; eski liste artık var olmayan
// dosyayı ister (404) — ve operatöre gösterdiğimiz "kurulacak sürüm" de artık
// gerçek olmayabilir. Yeterince taze listeler maliyeti atlar; art arda
// kurulumlar yalnız bir kez tazeler. En-iyi-çaba: bozuk bir üçüncü parti depo,
// ana arşivlerin karşılayabileceği bir kurulumu veto etmemeli.
func refreshAptListsIfStale(maxAge time.Duration) {
	refreshAptListsIfStaleContext(context.Background(), maxAge)
}

func refreshAptListsIfStaleContext(ctx context.Context, maxAge time.Duration) {
	profile, err := verifiedHostProfile("apt")
	if err != nil {
		fmt.Printf("apt-get update before install skipped: %s\n", err)
		return
	}
	aptGet, err := executableForProfile(profile, "apt", "apt-get")
	if err != nil {
		fmt.Printf("apt-get update before install skipped: %s\n", err)
		return
	}
	refreshAptListsIfStaleWithExecutable(ctx, maxAge, aptGet)
}

func refreshAptListsIfStaleWithExecutable(ctx context.Context, maxAge time.Duration, aptGet string) {
	if age, ok := aptListsAge(); ok && age <= maxAge {
		return
	}
	env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if out, err := runServiceMutationCombinedOutputEnv(ctx, env, aptGet, "update"); err != nil {
		fmt.Printf("apt-get update before install failed (continuing): %s\n", firstLine(string(out)))
	}
}

// installPackages installs the given packages with the host's package
// manager, non-interactively. Unknown or not-yet-supported families return a
// clear error rather than a blind command.
// installPackages, verilen paketleri makinenin paket yöneticisiyle
// etkileşimsiz kurar. Bilinmeyen ya da henüz desteklenmeyen aileler kör bir
// komut yerine açık bir hata döndürür.
func installPackages(family string, packages []string) (string, error) {
	return installPackagesContext(context.Background(), family, packages)
}

func installPackagesContext(ctx context.Context, family string, packages []string) (string, error) {
	return installPackagesWithCandidateContext(ctx, family, packages, "")
}

func installPackagesWithCandidateContext(ctx context.Context, family string, packages []string, requiredCandidate string) (string, error) {
	packageOperationMu.Lock()
	defer packageOperationMu.Unlock()

	if len(packages) == 0 {
		return "", fmt.Errorf("no packages to install")
	}
	// Guard every name: only a real package token reaches the command line —
	// never a flag or shell metacharacter.
	// Her adı koru: komut satırına yalnız gerçek bir paket dizgesi ulaşır —
	// asla bayrak ya da kabuk metakarakteri.
	for _, p := range packages {
		if !validPackageName(p) {
			return "", fmt.Errorf("invalid package name: %q", p)
		}
	}
	profile, err := verifiedHostProfile(family)
	if err != nil {
		return "", err
	}

	switch family {
	case "apt":
		aptGet, err := executableForProfile(profile, family, "apt-get")
		if err != nil {
			return "", err
		}
		aptCache, err := executableForProfile(profile, family, "apt-cache")
		if err != nil {
			return "", err
		}
		refreshAptListsIfStaleWithExecutable(ctx, time.Hour, aptGet)
		if requiredCandidate = strings.TrimSpace(requiredCandidate); requiredCandidate != "" {
			candidate, err := aptInstallCandidateWithExecutable(ctx, aptCache, requiredCandidate)
			if err != nil {
				return "", fmt.Errorf("check APT installation candidate for %s: %w", requiredCandidate, err)
			}
			if candidate == "" {
				return "", fmt.Errorf("selected package %s has no APT installation candidate after refreshing package lists; enable or repair its managed repository and rescan", requiredCandidate)
			}
		}
		run := func() (string, error) {
			args := append([]string{"install", "-y", "--no-install-recommends"}, packages...)
			env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
			out, err := runServiceMutationCombinedOutputEnv(ctx, env, aptGet, args...)
			return string(out), err
		}
		out, err := run()
		// Mirror rotated inside our freshness window: the lists said the file
		// exists, the mirror says 404. Refresh unconditionally and retry once.
		// Ayna, tazelik penceremizin içinde döndü: listeler dosya var dedi,
		// ayna 404 diyor. Koşulsuz tazele ve bir kez yeniden dene.
		if err != nil && strings.Contains(out, "404") {
			refreshAptListsIfStaleWithExecutable(ctx, 0, aptGet)
			out, err = run()
		}
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
		}
		return out, nil
	case "pacman":
		pacman, err := executableForProfile(profile, family, "pacman")
		if err != nil {
			return "", err
		}
		// Arch supports only full-system upgrades. Refreshing databases with
		// -Sy and then installing with -S creates an unsupported partial
		// upgrade window. Keep refresh, upgrade and requested package install
		// in one pacman transaction instead.
		// Arch yalnızca tam sistem yükseltmelerini destekler. -Sy ile veritabanı
		// tazeleyip sonra -S ile kurmak desteklenmeyen bir kısmi yükseltme
		// penceresi açar. Tazeleme, yükseltme ve istenen paket kurulumunu tek
		// pacman işleminde tut.
		args := pacmanInstallArgs(packages)
		out, err := runServiceMutationCombinedOutput(ctx, pacman, args...)
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	case "dnf":
		return "", errDNFMutationCertificationPending
	default:
		return "", fmt.Errorf("unrecognised distribution; install %s manually", strings.Join(packages, ", "))
	}
}

func aptInstallCandidateContext(ctx context.Context, packageName string) (string, error) {
	if !validPackageName(packageName) {
		return "", fmt.Errorf("invalid package name: %q", packageName)
	}
	profile, err := verifiedHostProfile("apt")
	if err != nil {
		return "", err
	}
	aptCache, err := executableForProfile(profile, "apt", "apt-cache")
	if err != nil {
		return "", err
	}
	return aptInstallCandidateWithExecutable(ctx, aptCache, packageName)
}

func aptInstallCandidateWithExecutable(ctx context.Context, aptCache, packageName string) (string, error) {
	if !validPackageName(packageName) {
		return "", fmt.Errorf("invalid package name: %q", packageName)
	}
	env := append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := runServiceMutationCombinedOutputEnv(
		ctx,
		env,
		aptCache,
		"policy",
		packageName,
	)
	if err != nil {
		return "", fmt.Errorf("apt-cache policy failed: %v: %s", err, firstLine(string(out)))
	}
	return parseAptInstallCandidate(out), nil
}

func parseAptInstallCandidate(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Candidate:") {
			continue
		}
		candidate := strings.TrimSpace(strings.TrimPrefix(line, "Candidate:"))
		if candidate == "" || candidate == "(none)" {
			return ""
		}
		return candidate
	}
	return ""
}

func pacmanInstallArgs(packages []string) []string {
	return append([]string{"-Syu", "--noconfirm", "--needed"}, packages...)
}

func dnfExecutableForProfile(profile hostplatform.Profile) (string, error) {
	if profile.PackageManager != hostplatform.PackageManagerDNF {
		return "", fmt.Errorf("host package manager is %s, want dnf", profile.PackageManager)
	}
	// A future detector may pin dnf5 explicitly. Never discover it from PATH
	// here: only a path already verified in the trusted profile is accepted.
	for _, name := range []string{"dnf5", "dnf"} {
		if path := profile.Executables[name]; path != "" {
			return path, nil
		}
	}
	return "", fmt.Errorf("host DNF executable has not been verified")
}

func dnfInstallArgs(packages []string) []string {
	return append([]string{
		"-y",
		"--setopt=install_weak_deps=False",
		"install",
	}, packages...)
}

func dnfRemoveArgs(packages []string) []string {
	return append([]string{
		"-y",
		"--setopt=clean_requirements_on_remove=False",
		"remove",
	}, packages...)
}

func dnfCandidateQueryArgs(packageName string) []string {
	return []string{
		"-C",
		"-q",
		"repoquery",
		"--available",
		"--latest-limit=1",
		`--queryformat=CELIKPANEL_EVR:%{evr}\n`,
		packageName,
	}
}

func dnfMetadataRefreshArgs() []string {
	return []string{"-q", "--refresh", "makecache"}
}

type dnfPreviewCommandRunner func(context.Context, []string, string, ...string) ([]byte, error)

var runDNFPreviewCommand dnfPreviewCommandRunner = runServiceMutationCombinedOutputEnv

func refreshDNFMetadataWithExecutable(ctx context.Context, dnf string) error {
	env := append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := runDNFPreviewCommand(ctx, env, dnf, dnfMetadataRefreshArgs()...)
	if err != nil {
		return fmt.Errorf("DNF metadata refresh failed: %v: %s", err, firstLine(string(out)))
	}
	return nil
}

func dnfInstallCandidateWithExecutable(ctx context.Context, dnf, packageName string) (string, error) {
	if !validPackageName(packageName) {
		return "", fmt.Errorf("invalid package name: %q", packageName)
	}
	if err := refreshDNFMetadataWithExecutable(ctx, dnf); err != nil {
		return "", err
	}
	env := append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := runDNFPreviewCommand(ctx, env, dnf, dnfCandidateQueryArgs(packageName)...)
	if err != nil {
		return "", fmt.Errorf("DNF repoquery failed: %v: %s", err, firstLine(string(out)))
	}
	return parseDNFInstallCandidate(out)
}

func parseDNFInstallCandidate(out []byte) (string, error) {
	const prefix = "CELIKPANEL_EVR:"
	var candidate string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		evr := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if !validRPMEVR(evr) {
			return "", fmt.Errorf("DNF repoquery returned an invalid candidate")
		}
		if candidate != "" && candidate != evr {
			return "", fmt.Errorf("DNF repoquery returned ambiguous candidates")
		}
		candidate = evr
	}
	return candidate, nil
}

func validRPMEVR(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && r != '.' && r != '_' && r != '+' && r != '~' && r != '^' && r != ':' && r != '-' {
			return false
		}
	}
	return true
}

// installDNFPreviewPackagesContext preserves the audited DNF transaction
// implementation without making it reachable from a production handler. A
// certified service/distro slice must add its own explicit gate before it may
// call this primitive.
func installDNFPreviewPackagesContext(ctx context.Context, packages []string, requiredCandidate string) (string, error) {
	packageOperationMu.Lock()
	defer packageOperationMu.Unlock()

	if len(packages) == 0 {
		return "", fmt.Errorf("no packages to install")
	}
	for _, pkg := range packages {
		if !validPackageName(pkg) {
			return "", fmt.Errorf("invalid package name: %q", pkg)
		}
	}
	profile, err := verifiedHostProfile("dnf")
	if err != nil {
		return "", err
	}
	dnf, err := dnfExecutableForProfile(profile)
	if err != nil {
		return "", err
	}
	if requiredCandidate = strings.TrimSpace(requiredCandidate); requiredCandidate != "" {
		candidate, err := dnfInstallCandidateWithExecutable(ctx, dnf, requiredCandidate)
		if err != nil {
			return "", fmt.Errorf("check DNF installation candidate for %s: %w", requiredCandidate, err)
		}
		if candidate == "" {
			return "", fmt.Errorf("selected package %s has no DNF installation candidate after refreshing repository metadata; enable or repair its managed repository and rescan", requiredCandidate)
		}
	} else if err := refreshDNFMetadataWithExecutable(ctx, dnf); err != nil {
		return "", err
	}
	env := append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := runDNFPreviewCommand(ctx, env, dnf, dnfInstallArgs(packages)...)
	if err != nil {
		return "", fmt.Errorf("DNF install failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// removeDNFPreviewPackagesContext is intentionally unreachable for the same
// reason as installDNFPreviewPackagesContext.
func removeDNFPreviewPackagesContext(ctx context.Context, packages []string) (string, error) {
	packageOperationMu.Lock()
	defer packageOperationMu.Unlock()

	if len(packages) == 0 {
		return "", fmt.Errorf("no packages to remove")
	}
	for _, pkg := range packages {
		if !validPackageName(pkg) {
			return "", fmt.Errorf("invalid package name: %q", pkg)
		}
	}
	profile, err := verifiedHostProfile("dnf")
	if err != nil {
		return "", err
	}
	dnf, err := dnfExecutableForProfile(profile)
	if err != nil {
		return "", err
	}
	env := append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := runDNFPreviewCommand(ctx, env, dnf, dnfRemoveArgs(packages)...)
	if err != nil {
		return "", fmt.Errorf("DNF removal failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// removePackages removes named packages non-interactively. APT and pacman use
// their established configuration/orphan cleanup policy; the bounded DNF
// preview explicitly keeps dependencies that merely became unused.
// removePackages, adlandırılmış paketleri etkileşimsiz kaldırır. APT ve pacman
// mevcut yapılandırma/öksüz bağımlılık temizleme politikasını kullanır; sınırlı
// DNF önizlemesi ise yeni boşa düşen bağımlılıkları bilerek otomatik kaldırmaz.
func removePackages(family string, packages []string) (string, error) {
	return removePackagesContext(context.Background(), family, packages)
}

func removePackagesContext(ctx context.Context, family string, packages []string) (string, error) {
	packageOperationMu.Lock()
	defer packageOperationMu.Unlock()

	if len(packages) == 0 {
		return "", fmt.Errorf("no packages to remove")
	}
	for _, p := range packages {
		if !validPackageName(p) {
			return "", fmt.Errorf("invalid package name: %q", p)
		}
	}
	profile, err := verifiedHostProfile(family)
	if err != nil {
		return "", err
	}
	switch family {
	case "apt":
		aptGet, err := executableForProfile(profile, family, "apt-get")
		if err != nil {
			return "", err
		}
		args := append([]string{"purge", "-y", "--auto-remove"}, packages...)
		env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		out, err := runServiceMutationCombinedOutputEnv(ctx, env, aptGet, args...)
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	case "pacman":
		pacman, err := executableForProfile(profile, family, "pacman")
		if err != nil {
			return "", err
		}
		// -Rns = purge: the package, its now-unneeded dependencies (-s) and
		// its config files (-n) — mirrors apt's purge --auto-remove.
		// -Rns = purge: paket, artık gereksiz bağımlılıkları (-s) ve config
		// dosyaları (-n) — apt'ın purge --auto-remove'unun aynası.
		args := append([]string{"-Rns", "--noconfirm"}, packages...)
		out, err := runServiceMutationCombinedOutput(ctx, pacman, args...)
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	case "dnf":
		return "", errDNFMutationCertificationPending
	default:
		return "", fmt.Errorf("unrecognised distribution; remove %s manually", strings.Join(packages, ", "))
	}
}

// packageInstalled reports whether a package is actually installed — the
// presence test for catalogue entries with no systemd unit (phpMyAdmin and
// friends are just files served by the web server, not daemons).
// packageInstalled, bir paketin gerçekten kurulu olup olmadığını bildirir —
// systemd unit'i olmayan katalog girdileri için varlık testi (phpMyAdmin ve
// benzerleri daemon değil, web sunucusunun sunduğu dosyalardır).
func packageInstalled(pkg string) bool {
	profile, err := detectHostPlatform()
	return err == nil && packageInstalledForProfile(profile, pkg)
}

func packageInstalledForFamily(family, pkg string) bool {
	if !validPackageName(pkg) {
		return false
	}
	profile, err := verifiedHostProfile(family)
	return err == nil && packageInstalledForProfile(profile, pkg)
}

func packageInstalledForProfile(profile hostplatform.Profile, pkg string) bool {
	if !validPackageName(pkg) {
		return false
	}
	family := string(profile.PackageManager)
	switch family {
	case "apt":
		dpkgQuery, err := executableForProfile(profile, family, "dpkg-query")
		if err != nil {
			return false
		}
		out, err := serviceMutationCommand(context.Background(), dpkgQuery, "-W", "-f", "${Status}", pkg).Output()
		return err == nil && strings.Contains(string(out), "install ok installed")
	case "pacman":
		pacman, err := executableForProfile(profile, family, "pacman")
		return err == nil && serviceMutationCommand(context.Background(), pacman, "-Q", pkg).Run() == nil
	case "dnf":
		rpm, err := executableForProfile(profile, family, "rpm")
		return err == nil && serviceMutationCommand(context.Background(), rpm, "-q", "--", pkg).Run() == nil
	default:
		return false
	}
}

// PkgFamily exposes the host's package-manager family to the panel, which
// needs it to show family-correct package names in install/uninstall dialogs
// (the catalog keys Packages by family; the panel must not assume apt).
// PkgFamily, makinenin paket-yöneticisi ailesini panele açar; panel kur/kaldır
// pencerelerinde aileye doğru paket adlarını göstermek için buna muhtaçtır
// (katalog Packages'ı aileyle anahtarlar; panel apt varsayamaz).
func (a *Agent) PkgFamily(_ *transport.Empty, reply *string) error {
	profile, err := detectHostPlatform()
	if err != nil {
		*reply = ""
		return fmt.Errorf("detect host platform: %w", err)
	}
	*reply = string(profile.PackageManager)
	return nil
}

// validPackageName allows the conservative charset real Debian/RPM/Arch
// package names use: letters, digits and . _ + - (must start alphanumeric).
// validPackageName, gerçek paket adlarının kullandığı muhafazakâr karakter
// setine izin verir: harf, rakam ve . _ + - (alfasayısal başlamalı).
func validPackageName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for i, r := range name {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if i == 0 && !alnum {
			return false
		}
		if !alnum && r != '.' && r != '_' && r != '+' && r != '-' {
			return false
		}
	}
	return true
}
