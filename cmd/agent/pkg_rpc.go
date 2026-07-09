package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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
		args := append([]string{"install", "-y", "--no-install-recommends"}, packages...)
		cmd := exec.Command("apt-get", args...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
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
