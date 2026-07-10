package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Package-manager abstraction so service installation is not hard-wired to
// one distro. We detect the family from the package-manager binary present
// (with an /etc/os-release fallback); each family knows how to install a set
// of packages non-interactively. Only apt (Ubuntu/Debian) is exercised and
// tested today — dnf/pacman are recognised so we return an honest "not
// supported yet" instead of guessing, and so adding real support later is one
// tested adapter, not a rewrite. We never claim a distro we haven't run on.
//
// Paket-yöneticisi soyutlaması; servis kurulumu tek dağıtıma gömülü değildir.
// Aileyi var olan paket-yöneticisi ikilisinden tespit ederiz (os-release
// yedeğiyle); her aile bir paket kümesini etkileşimsiz kurmayı bilir. Bugün
// yalnız apt (Ubuntu/Debian) çalıştırılıp test edilmiştir — dnf/pacman tanınır
// ki tahmin yerine dürüst "henüz desteklenmiyor" dönelim ve ileride gerçek
// destek yeniden yazım değil tek test edilmiş adaptör olsun. Üzerinde
// çalışmadığımız bir dağıtımı asla iddia etmeyiz.

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
	if age, ok := aptListsAge(); ok && age <= maxAge {
		return
	}
	cmd := exec.Command("apt-get", "update")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if out, err := cmd.CombinedOutput(); err != nil {
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
		refreshAptListsIfStale(time.Hour)
		run := func() (string, error) {
			args := append([]string{"install", "-y", "--no-install-recommends"}, packages...)
			cmd := exec.Command("apt-get", args...)
			cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
		out, err := run()
		// Mirror rotated inside our freshness window: the lists said the file
		// exists, the mirror says 404. Refresh unconditionally and retry once.
		// Ayna, tazelik penceremizin içinde döndü: listeler dosya var dedi,
		// ayna 404 diyor. Koşulsuz tazele ve bir kez yeniden dene.
		if err != nil && strings.Contains(out, "404") {
			refreshAptListsIfStale(0)
			out, err = run()
		}
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
		}
		return out, nil
	case "dnf", "pacman":
		return "", fmt.Errorf("automatic install on this distro (%s) is not supported yet; install %s manually", family, strings.Join(packages, ", "))
	default:
		return "", fmt.Errorf("unrecognised distribution; install %s manually", strings.Join(packages, ", "))
	}
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
		cmd := exec.Command("apt-get", args...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	case "dnf", "pacman":
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
	if !validPackageName(pkg) {
		return false
	}
	out, err := exec.Command("dpkg-query", "-W", "-f", "${Status}", pkg).Output()
	return err == nil && strings.Contains(string(out), "install ok installed")
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
