package core

import (
	"regexp"
	"strings"
	"sync"
)

// Unit discovery, owned by the catalogue. The agent's scanner used to carry a
// hand-written list of unit-name stems (serviceBases); every catalogue
// addition had to be mirrored there by hand, and three times it was not —
// spamd, then rspamd, then netdata were installed and running while the panel
// showed "stopped" because the scanner never fetched their units (A-series
// smells, Jul 23-24). The cure is here: both the systemctl glob (what to
// FETCH) and the ownership test (what a unit BELONGS to) derive from the
// catalogue's own SystemNames + SystemNamePattern. Add a service to the
// catalogue and the scanner sees it — there is no second list to forget.
//
// Unit keşfi, kataloğun malı. Agent'ın tarayıcısı eskiden elle yazılmış bir
// unit-adı kökleri listesi (serviceBases) taşıyordu; her katalog eklemesi
// oraya elle yansıtılmalıydı ve üç kez yansıtılmadı — spamd, sonra rspamd,
// sonra netdata kurulu ve çalışırken panel "durdu" gösterdi çünkü tarayıcı
// unit'lerini hiç çekmedi (A-serisi kokular, 23-24 Tem). Çare burada: hem
// systemctl glob'u (neyi ÇEKECEĞİ) hem sahiplik testi (bir unit'in neye AİT
// olduğu) kataloğun kendi SystemNames + SystemNamePattern'inden türer. Kataloğa
// bir servis ekle, tarayıcı görür — unutulacak ikinci liste yoktur.

var (
	patternCache   = map[string]*regexp.Regexp{}
	patternCacheMu sync.Mutex
)

// compilePattern caches compiled SystemNamePatterns — the scan compiles the
// same handful every pass. A pattern that does not compile is treated as
// absent rather than crashing the scan.
// compilePattern, derlenmiş SystemNamePattern'leri önbelleğe alır — tarama her
// geçişte aynı avuç dolusunu derler. Derlenmeyen bir desen, taramayı
// çökertmek yerine yokmuş gibi ele alınır.
func compilePattern(pat string) *regexp.Regexp {
	if pat == "" {
		return nil
	}
	patternCacheMu.Lock()
	defer patternCacheMu.Unlock()
	if re, ok := patternCache[pat]; ok {
		return re
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		re = nil
	}
	patternCache[pat] = re
	return re
}

// UnitBelongsTo reports whether a systemd unit name (without the .service
// suffix) belongs to a catalogue service: an exact SystemNames match, or a
// SystemNamePattern match (php8.4-fpm for php-fpm's `^php[0-9]+\.[0-9]+-fpm$`).
// UnitBelongsTo, bir systemd unit adının (.service son eki olmadan) bir katalog
// servisine ait olup olmadığını bildirir: SystemNames'te tam eşleşme ya da
// SystemNamePattern eşleşmesi (php-fpm'in `^php[0-9]+\.[0-9]+-fpm$`'i için
// php8.4-fpm).
func UnitBelongsTo(unit string, svc *ManagedService) bool {
	for _, n := range svc.SystemNames {
		if unit == n {
			return true
		}
	}
	if re := compilePattern(svc.SystemNamePattern); re != nil {
		return re.MatchString(unit)
	}
	return false
}

// ServiceForUnit returns the catalogue service a unit belongs to, or nil. The
// first match wins; catalogue ids are distinct enough that a unit belongs to
// at most one service.
// ServiceForUnit, bir unit'in ait olduğu katalog servisini ya da nil döndürür.
// İlk eşleşme kazanır; katalog id'leri bir unit'in en çok bir servise ait
// olacağı kadar ayrıktır.
func ServiceForUnit(unit string) *ManagedService {
	for i := range ManagedServices {
		if UnitBelongsTo(unit, &ManagedServices[i]) {
			return &ManagedServices[i]
		}
	}
	return nil
}

// ServiceScanGlobs is the systemctl glob set the scanner fetches — derived
// from every service's SystemNames and SystemNamePattern, so it can never
// drift from the catalogue. A versioned runtime contributes its pattern's
// literal prefix (php-fpm's pattern → "php*") so php8.4-fpm is fetched even
// though SystemNames only lists "php-fpm". Extra globs are harmless: the
// ownership test filters what the fetch over-collects.
// ServiceScanGlobs, tarayıcının çektiği systemctl glob kümesidir — her servisin
// SystemNames ve SystemNamePattern'inden türer, böylece katalogdan asla
// sapamaz. Sürümlü bir runtime, deseninin sabit önekini katar (php-fpm'in
// deseni → "php*"); böylece SystemNames yalnız "php-fpm" listelese de
// php8.4-fpm çekilir. Fazladan glob zararsızdır: sahiplik testi, çekimin fazla
// topladığını süzer.
func ServiceScanGlobs() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(stem string) {
		if stem == "" {
			return
		}
		g := stem + "*"
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	for i := range ManagedServices {
		s := &ManagedServices[i]
		for _, n := range s.SystemNames {
			add(globStem(n))
		}
		add(patternPrefix(s.SystemNamePattern))
	}
	return out
}

// globStem takes the fixed head of a unit name: "wg-quick@wg0" → "wg-quick",
// so the glob "wg-quick*" matches the template instance.
// globStem, bir unit adının sabit başını alır: "wg-quick@wg0" → "wg-quick";
// böylece "wg-quick*" glob'u şablon örneğini yakalar.
func globStem(name string) string {
	if i := strings.IndexByte(name, '@'); i > 0 {
		return name[:i]
	}
	return name
}

// patternPrefix extracts the literal leading run of a `^…` regex: the letters
// (and - _ .) right after ^, stopping at the first metacharacter.
// `^php[0-9]+\.[0-9]+-fpm$` → "php". Empty for a pattern with no literal head.
// patternPrefix, bir `^…` regex'inin sabit baş kısmını çıkarır: ^'dan hemen
// sonraki harfler (ve - _ .), ilk metakaraktere kadar.
// `^php[0-9]+\.[0-9]+-fpm$` → "php". Sabit başı olmayan desen için boş.
func patternPrefix(pat string) string {
	pat = strings.TrimPrefix(pat, "^")
	var b strings.Builder
	for _, r := range pat {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		break
	}
	return b.String()
}
