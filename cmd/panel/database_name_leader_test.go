package main

import (
	"fmt"
	"testing"

	"github.com/alicelik/celikpanel/internal/services"
)

// R-051, second half. The subscription-scoped names were fixed at their own
// seam; the proof of that fix found the same defect on the two paths that name
// a database after a DOMAIN. A domain may legally begin with a digit -
// 1and1.com, 360.com, 1password.com are real, sold, ordinary domains - and
// ValidateSQLIdentifier refuses an identifier that begins with one. So the
// domain-scoped "add a database" and the one-click WordPress installer both
// composed a name their own agent would refuse, and no operator on such a
// domain could create a database at all.
//
// Each path is pinned from both sides, because the fix is only half a fix if
// it moves names that already exist:
//
//   - a digit-leading domain must now compose a name the validator ACCEPTS,
//   - an ordinary domain must compose EXACTLY the name it composes today,
//     byte for byte, since every database in every installation was named
//     that way and a changed rule here would rename live databases.
//
// The expected strings below are therefore written out in full rather than
// computed: a golden value cannot drift with the code it is guarding.
//
// R-051'in ikinci yarısı. Abonelik kapsamlı adlar kendi dikişinde düzeltildi;
// o düzeltmenin kanıtı aynı defekti bir DOMAIN'den ad üreten iki yolda buldu.
// Bir domain meşru olarak rakamla başlayabilir ve doğrulayıcı rakamla başlayan
// tanımlayıcıyı reddeder. Her yol iki taraftan da kilitlenir: rakamla başlayan
// domain artık KABUL EDİLEN bir ad üretmeli, sıradan domain ise bugün ürettiği
// adın TIPATIP aynısını üretmeli - aksi hâlde canlı veritabanları yeniden
// adlandırılmış olur. Beklenen dizgiler bu yüzden hesaplanmaz, yazılır.

// domainScopedNames composes the names exactly as handleCreateDatabase does.
// domainScopedNames, adları handleCreateDatabase'in kurduğu gibi kurar.
func domainScopedNames(domain, requested string) (string, string) {
	databaseName := fmt.Sprintf("%s_%s", sanitizeName(domain), requested)
	return databaseName, databaseName + "_user"
}

// wordpressNames composes the names exactly as handleInstallWordPress does.
// wordpressNames, adları handleInstallWordPress'in kurduğu gibi kurar.
func wordpressNames(domain string) (string, string) {
	base := sanitizeDBIdent(services.SiteUsername(domain))
	return base + "_wp", base + "_wp"
}

func TestDomainScopedDatabaseNameAcceptsDigitLeadingDomain(t *testing.T) {
	for _, domain := range []string{"1and1.com", "360.com", "1password.com", "7-zip.org"} {
		databaseName, userName := domainScopedNames(domain, "shop")
		if err := services.ValidateSQLIdentifier(databaseName); err != nil {
			t.Fatalf("database name for %q is refused by the validator: %v (name %q)",
				domain, err, databaseName)
		}
		if err := services.ValidateSQLIdentifier(userName); err != nil {
			t.Fatalf("user name for %q is refused by the validator: %v (name %q)",
				domain, err, userName)
		}
	}
}

func TestWordPressDatabaseNameAcceptsDigitLeadingSiteName(t *testing.T) {
	for _, domain := range []string{"1and1.com", "360.com", "1password.com", "7-zip.org"} {
		databaseName, userName := wordpressNames(domain)
		if err := services.ValidateSQLIdentifier(databaseName); err != nil {
			t.Fatalf("WordPress database name for %q is refused by the validator: %v (name %q)",
				domain, err, databaseName)
		}
		if err := services.ValidateSQLIdentifier(userName); err != nil {
			t.Fatalf("WordPress account name for %q is refused by the validator: %v (name %q)",
				domain, err, userName)
		}
	}
}

// The stability half. These are the names the shipped panel produces today,
// and they must survive the fix untouched.
// Kararlılık yarısı. Bunlar panelin bugün ürettiği adlardır ve düzeltmeden
// değişmeden çıkmalıdırlar.
func TestDomainScopedDatabaseNameIsUnchangedForOrdinaryDomains(t *testing.T) {
	cases := []struct {
		domain       string
		requested    string
		databaseName string
	}{
		{"example.com", "shop", "example_com_shop"},
		{"my-site.co.uk", "blog", "my_site_co_uk_blog"},
		{"a1.example.net", "data", "a1_example_net_data"},
	}
	for _, testCase := range cases {
		databaseName, userName := domainScopedNames(testCase.domain, testCase.requested)
		if databaseName != testCase.databaseName {
			t.Fatalf("database name for %q changed: got %q, want %q",
				testCase.domain, databaseName, testCase.databaseName)
		}
		if want := testCase.databaseName + "_user"; userName != want {
			t.Fatalf("user name for %q changed: got %q, want %q",
				testCase.domain, userName, want)
		}
	}
}

func TestWordPressDatabaseNameIsUnchangedForOrdinaryDomains(t *testing.T) {
	cases := []struct {
		domain       string
		databaseName string
	}{
		{"example.com", "example_com_wp"},
		{"my-site.co.uk", "my_site_co_uk_wp"},
		{"a1.example.net", "a1_example_net_wp"},
		{"Example.COM", "example_com_wp"},
	}
	for _, testCase := range cases {
		databaseName, userName := wordpressNames(testCase.domain)
		if databaseName != testCase.databaseName {
			t.Fatalf("WordPress database name for %q changed: got %q, want %q",
				testCase.domain, databaseName, testCase.databaseName)
		}
		if userName != testCase.databaseName {
			t.Fatalf("WordPress account name for %q changed: got %q, want %q",
				testCase.domain, userName, testCase.databaseName)
		}
	}
}

// The repaired names themselves are golden too: a later edit that changed the
// repair would silently rename every database created on a digit-leading
// domain from the day this ships.
// Onarılmış adlar da altındır: onarımı değiştiren sonraki bir düzenleme, bu
// sürümden itibaren rakamla başlayan bir domain'de oluşturulmuş her
// veritabanını sessizce yeniden adlandırırdı.
func TestRepairedNamesAreStable(t *testing.T) {
	databaseName, userName := domainScopedNames("360.com", "shop")
	if databaseName != "_360_com_shop" || userName != "_360_com_shop_user" {
		t.Fatalf("domain-scoped repair moved: got %q / %q", databaseName, userName)
	}
	wordpressDatabase, wordpressUser := wordpressNames("1and1.com")
	if wordpressDatabase != "_1and1_com_wp" || wordpressUser != "_1and1_com_wp" {
		t.Fatalf("WordPress repair moved: got %q / %q", wordpressDatabase, wordpressUser)
	}
}

// The WordPress fragment is bounded at 48 characters so the finished name
// fits the validator's 63. Repairing the leading character must not push a
// long digit-leading domain past that bound.
// WordPress parçası 48 karakterle sınırlıdır ki tamamlanmış ad doğrulayıcının
// 63'üne sığsın. Baş karakteri onarmak uzun bir domain'i bu sınırın dışına
// itmemelidir.
func TestWordPressFragmentStaysBoundedAfterRepair(t *testing.T) {
	// Straight through the call site: SiteUsername already caps at 32.
	// Çağrı yerinin kendisinden: SiteUsername zaten 32'de sınırlar.
	longDigitLeading := "1234567890123456789012345678901234567890123456789012345678.example.com"
	databaseName, _ := wordpressNames(longDigitLeading)
	if err := services.ValidateSQLIdentifier(databaseName); err != nil {
		t.Fatalf("long digit-leading WordPress name is refused: %v (name %q)", err, databaseName)
	}

	// And directly, on an input long enough to reach the sanitizer's own 48:
	// the repair must run before the truncation, not after it.
	// Ve doğrudan, sanitizer'ın kendi 48'ine ulaşacak kadar uzun bir girdiyle:
	// onarım kısaltmadan önce koşmalıdır, sonra değil.
	base := sanitizeDBIdent("1" + "0123456789012345678901234567890123456789012345678901234567890")
	if len(base) != 48 {
		t.Fatalf("repaired fragment is not bounded at 48: %d (%q)", len(base), base)
	}
	if err := services.ValidateSQLIdentifier(base); err != nil {
		t.Fatalf("repaired long fragment is refused: %v (fragment %q)", err, base)
	}
}

// A fragment with nothing usable in it keeps the word each call site already
// substitutes: the WordPress installer's "app", and, on the domain-scoped
// path, nothing at all - "_shop" is a name the validator has always accepted
// and may therefore already exist.
// İçinde kullanılabilir hiçbir şey olmayan bir parça, her çağrı yerinin zaten
// koyduğu sözcüğü korur.
func TestEmptyFragmentFallbacksAreUnchanged(t *testing.T) {
	if got := sanitizeDBIdent("..."); got != "app" {
		t.Fatalf("WordPress empty-fragment fallback changed: got %q, want %q", got, "app")
	}
	if databaseName, _ := domainScopedNames("!!!", "shop"); databaseName != "_shop" {
		t.Fatalf("domain-scoped empty-fragment name changed: got %q, want %q", databaseName, "_shop")
	}
}
