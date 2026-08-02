package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Package managers own one machine-wide database lock. Serialize every panel
// package mutation so browser tabs and API clients cannot race pacman or apt.
var packageOperationMu sync.Mutex

// Package-manager abstraction so service installation is not hard-wired to
// one distro. We detect the family from the package-manager binary present
// (with an /etc/os-release fallback); each family knows how to install a set
// of packages non-interactively. apt (Ubuntu/Debian) is the first-class
// tested family; pacman (Arch) is supported as the dev-test target the
// operator keeps a second server on (D-004 amendment) — for services whose
// catalog entry carries pacman package names. dnf is recognised and returns
// an honest "not supported yet". We never claim a distro we haven't run on.
//
// Paket-yöneticisi soyutlaması; servis kurulumu tek dağıtıma gömülü değildir.
// Aileyi var olan paket-yöneticisi ikilisinden tespit ederiz (os-release
// yedeğiyle); her aile bir paket kümesini etkileşimsiz kurmayı bilir. apt
// (Ubuntu/Debian) birinci sınıf test edilmiş ailedir; pacman (Arch),
// operatörün ikinci sunucuyu üzerinde tuttuğu geliştirme-test hedefi olarak
// desteklenir (D-004 eki) — katalog girdisi pacman paket adı taşıyan
// servisler için. dnf tanınır ve dürüst "henüz desteklenmiyor" döndürür.
// Üzerinde çalışmadığımız bir dağıtımı asla iddia etmeyiz.

// detectPkgFamily reports which package-manager family this host uses
// ("apt", "dnf", "pacman", or "" when unrecognised).
// detectPkgFamily, bu makinenin hangi paket-yöneticisi ailesini kullandığını
// bildirir.
func detectPkgFamily() string {
	for _, m := range []struct{ bin, family string }{
		{"apt-get", "apt"},
		{"dnf", "dnf"},
		{"yum", "dnf"},
		{"pacman", "pacman"},
	} {
		if _, err := exec.LookPath(m.bin); err == nil {
			return m.family
		}
	}
	// Fall back to os-release when no known binary is on PATH.
	// PATH'te bilinen ikili yoksa os-release'e düş.
	data, _ := os.ReadFile("/etc/os-release")
	osr := strings.ToLower(string(data))
	switch {
	case strings.Contains(osr, "debian"), strings.Contains(osr, "ubuntu"):
		return "apt"
	case strings.Contains(osr, "rhel"), strings.Contains(osr, "fedora"), strings.Contains(osr, "centos"):
		return "dnf"
	case strings.Contains(osr, "arch"):
		return "pacman"
	}
	return ""
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
	if age, ok := aptListsAge(); ok && age <= maxAge {
		return
	}
	env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if out, err := runServiceMutationCombinedOutputEnv(ctx, env, "apt-get", "update"); err != nil {
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

	switch family {
	case "apt":
		refreshAptListsIfStaleContext(ctx, time.Hour)
		run := func() (string, error) {
			args := append([]string{"install", "-y", "--no-install-recommends"}, packages...)
			env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
			out, err := runServiceMutationCombinedOutputEnv(ctx, env, "apt-get", args...)
			return string(out), err
		}
		out, err := run()
		// Mirror rotated inside our freshness window: the lists said the file
		// exists, the mirror says 404. Refresh unconditionally and retry once.
		// Ayna, tazelik penceremizin içinde döndü: listeler dosya var dedi,
		// ayna 404 diyor. Koşulsuz tazele ve bir kez yeniden dene.
		if err != nil && strings.Contains(out, "404") {
			refreshAptListsIfStaleContext(ctx, 0)
			out, err = run()
		}
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
		}
		return out, nil
	case "pacman":
		// Arch supports only full-system upgrades. Refreshing databases with
		// -Sy and then installing with -S creates an unsupported partial
		// upgrade window. Keep refresh, upgrade and requested package install
		// in one pacman transaction instead.
		// Arch yalnızca tam sistem yükseltmelerini destekler. -Sy ile veritabanı
		// tazeleyip sonra -S ile kurmak desteklenmeyen bir kısmi yükseltme
		// penceresi açar. Tazeleme, yükseltme ve istenen paket kurulumunu tek
		// pacman işleminde tut.
		args := pacmanInstallArgs(packages)
		out, err := runServiceMutationCombinedOutput(ctx, "pacman", args...)
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	case "dnf":
		return "", fmt.Errorf("automatic install on this distro (%s) is not supported yet; install %s manually", family, strings.Join(packages, ", "))
	default:
		return "", fmt.Errorf("unrecognised distribution; install %s manually", strings.Join(packages, ", "))
	}
}

func pacmanInstallArgs(packages []string) []string {
	return append([]string{"-Syu", "--noconfirm", "--needed"}, packages...)
}

// removePackages purges the given packages with the host's package manager,
// non-interactively — the mirror of installPackages, for shrinking the attack
// surface back down. "purge" (not "remove") also drops config, so an
// uninstalled service leaves nothing behind. autoremove clears now-orphaned
// dependencies for the same reason.
// removePackages, verilen paketleri makinenin paket yöneticisiyle etkileşimsiz
// kaldırır (purge) — installPackages'in aynası, saldırı yüzeyini geri
// küçültmek için. "purge" config'i de siler; autoremove artık öksüz kalan
// bağımlılıkları temizler.
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
	switch family {
	case "apt":
		args := append([]string{"purge", "-y", "--auto-remove"}, packages...)
		env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		out, err := runServiceMutationCombinedOutputEnv(ctx, env, "apt-get", args...)
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	case "pacman":
		// -Rns = purge: the package, its now-unneeded dependencies (-s) and
		// its config files (-n) — mirrors apt's purge --auto-remove.
		// -Rns = purge: paket, artık gereksiz bağımlılıkları (-s) ve config
		// dosyaları (-n) — apt'ın purge --auto-remove'unun aynası.
		args := append([]string{"-Rns", "--noconfirm"}, packages...)
		out, err := runServiceMutationCombinedOutput(ctx, "pacman", args...)
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	case "dnf":
		return "", fmt.Errorf("automatic removal on this distro (%s) is not supported yet; remove %s manually", family, strings.Join(packages, ", "))
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
	return packageInstalledForFamily(detectPkgFamily(), pkg)
}

func packageInstalledForFamily(family, pkg string) bool {
	if !validPackageName(pkg) {
		return false
	}
	switch family {
	case "apt":
		out, err := exec.Command("dpkg-query", "-W", "-f", "${Status}", pkg).Output()
		return err == nil && strings.Contains(string(out), "install ok installed")
	case "pacman":
		return exec.Command("pacman", "-Q", pkg).Run() == nil
	case "dnf":
		return exec.Command("rpm", "-q", "--", pkg).Run() == nil
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
	*reply = detectPkgFamily()
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
