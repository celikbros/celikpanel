package services

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// phpVersionPattern matches a PHP version like "8.3". Anything else is
// rejected before it can be interpolated into a filesystem path.
// phpVersionPattern, "8.3" gibi bir PHP sürümüyle eşleşir. Başka her şey,
// bir dosya sistemi yoluna gömülmeden önce reddedilir.
var phpVersionPattern = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]{1,2}$`)

// ValidatePHPVersion guards the version component of paths such as
// /etc/php/<version>/fpm/php.ini. Because the agent writes these as root,
// an unvalidated version (e.g. "../../../etc/cron.d/x") would be an
// arbitrary-file-write as root. Callers must validate before building the
// path.
//
// ValidatePHPVersion, /etc/php/<version>/fpm/php.ini gibi yolların sürüm
// bileşenini korur. Agent bunları root olarak yazdığından, doğrulanmamış
// bir sürüm (örn. "../../../etc/cron.d/x") root olarak keyfi-dosya-yazma
// olurdu. Çağıranlar yolu oluşturmadan önce doğrulamalıdır.
func ValidatePHPVersion(version string) error {
	if !phpVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid PHP version %q", version)
	}
	return nil
}

// DetectInstalledPHPVersion returns the newest PHP version with an FPM tree
// actually present on this host (/etc/php/<ver>/fpm), or "" when none. Each
// distro release ships a different PHP (Ubuntu 24.04 → 8.3, Debian 13 → 8.4),
// so a hard-coded default silently points pool files at a directory that does
// not exist — caught live on Debian 13. The host, not a constant, is the
// source of truth.
// DetectInstalledPHPVersion, bu makinede FPM ağacı gerçekten var olan en yeni
// PHP sürümünü döndürür (/etc/php/<ver>/fpm), yoksa "". Her dağıtım sürümü
// farklı bir PHP taşır (Ubuntu 24.04 → 8.3, Debian 13 → 8.4); sabit bir
// varsayılan, havuz dosyalarını sessizce var olmayan bir dizine yöneltir —
// Debian 13'te canlı yakalandı. Gerçeğin kaynağı sabit değil, makinedir.
func DetectInstalledPHPVersion() string {
	if all := DetectInstalledPHPVersions(); len(all) > 0 {
		return all[0]
	}
	return ""
}

// DetectInstalledPHPVersions lists every PHP version with an FPM tree on this
// host, newest first — the honest source for a version picker.
// DetectInstalledPHPVersions, bu makinede FPM ağacı olan her PHP sürümünü, en
// yenisi önde listeler — bir sürüm seçicinin dürüst kaynağı.
func DetectInstalledPHPVersions() []string {
	entries, err := os.ReadDir("/etc/php")
	if err != nil {
		return nil
	}
	var found []string
	for _, e := range entries {
		v := e.Name()
		if !phpVersionPattern.MatchString(v) {
			continue
		}
		if _, err := os.Stat("/etc/php/" + v + "/fpm"); err != nil {
			continue
		}
		found = append(found, v)
	}
	sort.Slice(found, func(i, j int) bool { return phpVersionLess(found[j], found[i]) })
	return found
}

// phpVersionLess compares two validated "major.minor" versions numerically
// (so "8.10" > "8.9", which a string compare would get wrong).
// phpVersionLess, doğrulanmış iki "major.minor" sürümünü sayısal karşılaştırır
// ("8.10" > "8.9"; dizgi karşılaştırması bunu yanlış yapardı).
func phpVersionLess(a, b string) bool {
	pa, pb := strings.SplitN(a, ".", 2), strings.SplitN(b, ".", 2)
	amaj, _ := strconv.Atoi(pa[0])
	bmaj, _ := strconv.Atoi(pb[0])
	if amaj != bmaj {
		return amaj < bmaj
	}
	amin, _ := strconv.Atoi(pa[1])
	bmin, _ := strconv.Atoi(pb[1])
	return amin < bmin
}
