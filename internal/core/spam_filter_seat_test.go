package core

import "testing"

// Rspamd and SpamAssassin can technically be installed side by side — separate
// daemons, separate ports, they never collide on the machine. But they fill the
// SAME role, and Postfix hands each message to one filter chain: running both
// means every message is scored twice, by two rule sets that disagree, at twice
// the CPU. So the panel seats them together, exactly like Postfix/Exim: one
// spam filter at a time, and choosing the other one is a swap, not an addition.
//
// Rspamd ve SpamAssassin teknik olarak yan yana kurulabilir — ayrı daemon'lar,
// ayrı portlar, makinede hiç çakışmazlar. Ama AYNI rolü doldururlar ve Postfix
// her iletiyi tek bir filtre zincirine verir: ikisini birden koşturmak, her
// iletinin birbiriyle anlaşamayan iki kural kümesince, iki kat CPU ile iki kez
// puanlanması demektir. Bu yüzden panel onları aynı koltuğa oturtur, tıpkı
// Postfix/Exim gibi: aynı anda tek spam filtresi ve diğerini seçmek bir ekleme
// değil, bir değiştirmedir.
func TestSpamFiltersShareOneSeat(t *testing.T) {
	for _, id := range []string{"rspamd", "spamassassin"} {
		svc := GetManagedServiceByID(id)
		if svc == nil {
			t.Fatalf("%s missing from the catalogue", id)
		}
		if svc.ConflictGroup != "spam-filter" {
			t.Errorf("%s: ConflictGroup = %q, want spam-filter — two spam filters must never install together", id, svc.ConflictGroup)
		}
	}
}

// A spam filter that cannot be wired into the mail server filters nothing, and
// "installed" would be a lie. SpamAssassin needs spamass-milter to receive
// Postfix's mail at all: it must be in the packages we install AND in the units
// we start. Arch has no spamass-milter outside the AUR, so the row honestly
// offers no pacman packages there.
//
// Posta sunucusuna bağlanamayan bir spam filtresi hiçbir şeyi süzmez ve
// "kurulu" bir yalan olurdu. SpamAssassin'in Postfix'in postasını alabilmesi
// için spamass-milter gerekir: hem kurduğumuz paketlerde hem de başlattığımız
// unit'lerde bulunmalıdır. Arch'ta spamass-milter AUR dışında yok; bu yüzden
// satır orada dürüstçe hiç pacman paketi sunmaz.
func TestSpamAssassinShipsItsBridgeToPostfix(t *testing.T) {
	svc := GetManagedServiceByID("spamassassin")
	if svc == nil {
		t.Fatal("spamassassin missing from the catalogue")
	}
	if !contains(svc.Packages["apt"], "spamass-milter") {
		t.Error("apt packages must include spamass-milter — without it spamd never sees Postfix's mail")
	}
	if !contains(svc.HelperUnits, "spamass-milter") {
		t.Error("spamass-milter must be a HelperUnit — installing it without starting it filters nothing")
	}
	if len(svc.Packages["pacman"]) != 0 {
		t.Error("Arch has spamass-milter only in the AUR; offering an install that cannot filter breaks \"installed means working\"")
	}
}

// Every HelperUnit must be a unit we could actually start on the distro that
// ships it: it may not duplicate the service's own SystemNames (that would be
// started twice) and it must not be empty.
// Her HelperUnit, onu getiren dağıtımda gerçekten başlatabileceğimiz bir unit
// olmalıdır: servisin kendi SystemNames'ini tekrar edemez (iki kez başlatılırdı)
// ve boş olamaz.
func TestHelperUnitsAreDistinctAndNamed(t *testing.T) {
	for _, svc := range ManagedServices {
		for _, h := range svc.HelperUnits {
			if h == "" {
				t.Errorf("%s: empty helper unit", svc.ID)
			}
			if contains(svc.SystemNames, h) {
				t.Errorf("%s: %q is both a SystemName and a HelperUnit", svc.ID, h)
			}
		}
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// A repo marked Required means the package exists NOWHERE else on that family.
// Netdata is the case: Debian/Ubuntu package it nowhere, so an install without
// the repo dies inside apt with "no installation candidate" — the operator's
// failed attempt (25 Jul: "net data kurulamadı"). Optional repos (Sury, PGDG)
// must NOT be marked, or the panel would demand a third-party repo for a
// component the distro already ships.
//
// Required işaretli bir depo, paketin o ailede BAŞKA HİÇBİR yerde olmadığı
// anlamına gelir. Netdata bu durumdadır: Debian/Ubuntu onu hiçbir yerde
// paketlemez, dolayısıyla deposuz kurulum apt içinde "kurulum adayı yok" ile
// ölür — operatörün başarısız denemesi (25 Tem: "net data kurulamadı").
// İsteğe bağlı depolar (Sury, PGDG) işaretlenMEmelidir; yoksa panel, dağıtımın
// zaten getirdiği bir bileşen için üçüncü taraf deposu dayatırdı.
func TestOnlyUnpackagedComponentsRequireTheirRepo(t *testing.T) {
	for id, wantRequired := range map[string]bool{
		"netdata":    true,
		"php-fpm":    false,
		"postgresql": false,
	} {
		svc := GetManagedServiceByID(id)
		if svc == nil || svc.Repo == nil {
			t.Fatalf("%s: expected a catalogue entry with a repo", id)
		}
		if svc.Repo.Required != wantRequired {
			t.Errorf("%s: Repo.Required = %v, want %v", id, svc.Repo.Required, wantRequired)
		}
		// A required repo is the ONLY apt source, so the family must still list
		// the package — otherwise the row would be hidden as "not offered".
		// Zorunlu depo tek apt kaynağıdır; bu yüzden aile paketi yine de
		// saymalıdır — yoksa satır "sunulmuyor" diye gizlenirdi.
		if wantRequired && len(svc.Packages["apt"]) == 0 {
			t.Errorf("%s: a required repo still needs its apt package listed", id)
		}
	}
}

// The seat must be enforced where it cannot be skipped. It was UI-only: with
// Redis running on Boston, a panel API call installed valkey-server, which
// could not bind 6379 and died in a restart loop — "installed and dead", the
// exact outcome the seat exists to prevent. The row already said "conflicts
// with Redis"; the API did it anyway.
//
// Koltuk, atlatılamayacak yerde uygulanmalıdır. Yalnız arayüzdeydi: Boston'da
// Redis çalışırken bir panel API çağrısı valkey-server kurdu, 6379'a
// bağlanamadı ve yeniden başlatma döngüsünde öldü — "kurulu ve ölü", yani
// koltuğun var olma sebebi olan sonucun ta kendisi. Satır zaten "Redis ile
// çakışıyor" diyordu; API yine de yaptı.
func TestSeatIsTakenByTheInstalledMember(t *testing.T) {
	redis := GetManagedServiceByID("redis")
	valkey := GetManagedServiceByID("valkey")
	nginx := GetManagedServiceByID("nginx")
	memcached := GetManagedServiceByID("memcached")
	for name, svc := range map[string]*ManagedService{"redis": redis, "valkey": valkey, "nginx": nginx, "memcached": memcached} {
		if svc == nil {
			t.Fatalf("%s missing from the catalogue", name)
		}
	}

	// The live case: Redis installed, Valkey refused by name.
	// Canlı durum: Redis kurulu, Valkey adıyla reddediliyor.
	if got := SeatTakenBy(valkey, map[string]bool{"redis": true}); got != "Redis" {
		t.Errorf("Valkey with Redis installed → %q, want %q", got, "Redis")
	}
	// And the other way round, since Arch ships Valkey instead of Redis.
	// Ve tersi; çünkü Arch, Redis yerine Valkey getirir.
	if got := SeatTakenBy(redis, map[string]bool{"valkey": true}); got != "Valkey" {
		t.Errorf("Redis with Valkey installed → %q, want %q", got, "Valkey")
	}
	// A free seat installs.
	// Boş koltuk kurulur.
	if got := SeatTakenBy(valkey, map[string]bool{"memcached": true, "nginx": true}); got != "" {
		t.Errorf("Valkey with an empty kv-store seat → %q, want none", got)
	}
	// A component already installed does not block itself (reinstall/repair).
	// Zaten kurulu bir bileşen kendini engellemez (yeniden kurma/onarım).
	if got := SeatTakenBy(valkey, map[string]bool{"valkey": true}); got != "" {
		t.Errorf("Valkey must not block itself → %q", got)
	}
	// No seat, no conflict: caches that coexist must never be blocked.
	// Koltuğu olmayan çakışmaz: yan yana yaşayan önbellekler asla engellenmemeli.
	if got := SeatTakenBy(memcached, map[string]bool{"redis": true, "valkey": true}); got != "" {
		t.Errorf("Memcached has no seat and must coexist → %q", got)
	}
}
