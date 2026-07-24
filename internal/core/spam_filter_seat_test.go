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
