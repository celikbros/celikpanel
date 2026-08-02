package core

import "testing"

// Every catalogue service that a scan should ever detect must be reachable by
// BOTH derived halves: the fetch glob must select its unit names, and the
// ownership test must claim them back. The old hand-written list broke this
// three times (spamd, rspamd, netdata) — a stem present for the fetch but
// missing for the match, or vice versa. Deriving both from the catalogue makes
// that impossible, and this test is the guard that keeps it impossible.
//
// Taramanın algılaması gereken her katalog servisi, türetilen İKİ yarıdan da
// ulaşılabilir olmalı: çekme glob'u unit adlarını seçmeli, sahiplik testi de
// onları geri sahiplenmeli. Eski elle liste bunu üç kez kırdı (spamd, rspamd,
// netdata). İkisini de katalogdan türetmek bunu imkânsız kılar; bu test de o
// imkânsızlığı koruyan bekçidir.
func TestScanGlobsCoverEveryUnitName(t *testing.T) {
	globs := ServiceScanGlobs()

	// A glob "nginx*" covers a name iff the name starts with "nginx".
	covers := func(name string) bool {
		for _, g := range globs {
			stem := g[:len(g)-1] // strip trailing '*'
			if len(name) >= len(stem) && name[:len(stem)] == stem {
				return true
			}
		}
		return false
	}

	for i := range ManagedServices {
		s := &ManagedServices[i]
		for _, n := range s.SystemNames {
			if !covers(n) {
				t.Errorf("%s: SystemName %q is not covered by any scan glob %v", s.ID, n, globs)
			}
			// The name it declares must also belong back to it.
			if ServiceForUnit(n) == nil {
				t.Errorf("%s: SystemName %q belongs to no service via UnitBelongsTo", s.ID, n)
			}
		}
	}
}

// The pattern half is what makes versioned units (php8.4-fpm, postgresql@16-main)
// visible even though SystemNames lists only the stem. Pin the two shapes we
// rely on so a pattern edit that breaks them fails here, not silently in prod.
// Desen yarısı, sürümlü unit'leri (php8.4-fpm, postgresql@16-main) SystemNames
// yalnız kökü listelese de görünür kılan şeydir. Güvendiğimiz iki biçimi
// sabitle ki onları bozan bir desen düzenlemesi üretimde sessizce değil burada
// başarısız olsun.
func TestVersionedUnitsAreScannable(t *testing.T) {
	cases := []struct {
		unit    string
		service string
	}{
		{"php8.4-fpm", "php-fpm"},
		{"php8.3-fpm", "php-fpm"},
		{"postgresql@16-main", "postgresql"},
		{"postgresql", "postgresql"}, // Debian wrapper unit
		{"nginx", "nginx"},
		{"mariadb", "mariadb"},
		{"wg-quick@wg0", "wireguard"},
		{"rspamd", "rspamd"},
		{"netdata", "netdata"},
		{"spamd", "spamassassin"},
	}

	globs := ServiceScanGlobs()
	coversGlob := func(name string) bool {
		for _, g := range globs {
			stem := g[:len(g)-1]
			if len(name) >= len(stem) && name[:len(stem)] == stem {
				return true
			}
		}
		return false
	}

	for _, c := range cases {
		if !coversGlob(c.unit) {
			t.Errorf("%s: no scan glob fetches it", c.unit)
		}
		owner := ServiceForUnit(c.unit)
		if owner == nil {
			t.Errorf("%s: belongs to no service", c.unit)
			continue
		}
		if owner.ID != c.service {
			t.Errorf("%s: owned by %q, want %q", c.unit, owner.ID, c.service)
		}
	}
}

// A unit that belongs to nothing must be claimed by no one — the fold skips
// it, so a stray systemd unit the glob over-collects never becomes a phantom
// catalogue row.
// Hiçbir şeye ait olmayan bir unit, kimse tarafından sahiplenilmemeli — birleştirme
// onu atlar; böylece glob'un fazladan topladığı başıboş bir systemd unit'i
// asla hayalet katalog satırı olmaz.
func TestForeignUnitsBelongToNothing(t *testing.T) {
	for _, u := range []string{"ssh", "systemd-journald", "cron", "phpsessionclean", "dbus"} {
		if owner := ServiceForUnit(u); owner != nil {
			t.Errorf("%q wrongly claimed by %s", u, owner.ID)
		}
	}
}

func TestPatternPrefix(t *testing.T) {
	cases := map[string]string{
		`^php[0-9]+\.[0-9]+-fpm$`:   "php",
		`^postgresql@`:              "postgresql",
		`^(php[0-9]+\.[0-9]+)-fpm$`: "", // a group opens immediately — no literal head
		``:                          "",
	}
	for pat, want := range cases {
		if got := patternPrefix(pat); got != want {
			t.Errorf("patternPrefix(%q) = %q, want %q", pat, got, want)
		}
	}
}
