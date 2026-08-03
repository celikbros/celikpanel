package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// The server's nameserver pair — one pair for the whole machine, not one pair
// per hosted domain.
//
// This exists because the panel had it backwards. The zone template wrote
// `ns1.<domain>` and `ns2.<domain>` into every zone it created, which quietly
// declared each customer domain to be its own nameserver. The operator caught
// it immediately on the screen that showed them (25 Jul):
//
//	"the server is boston.celikhost.com — how can the nameservers be
//	 ns1.biovision.health and ns2.biovision.health? there is a logic error
//	 somewhere."
//
// There was. A hosting server has ONE nameserver pair. You register glue for
// it ONCE at the registrar that holds the panel's own domain, and after that
// every hosted domain is connected by simply pointing at those names — no glue
// per domain, no child nameservers per customer. Naming each zone after itself
// is the "vanity nameserver" feature some hosts sell as an extra; making it
// the default meant every single domain needed registrar work that should
// never have been asked for.
//
// Sunucunun ad sunucusu çifti — makinenin tamamı için tek çift, barındırılan
// alan adı başına bir çift değil.
//
// Bu, panelin işi tersinden yapması yüzünden var. Zone şablonu oluşturduğu her
// zone'a `ns1.<alanadı>` ve `ns2.<alanadı>` yazıyor, böylece her müşteri alan
// adını sessizce kendi ad sunucusu ilan ediyordu. Operatör bunu, kendisine
// gösterildiği ekranda anında yakaladı (25 Tem):
//
//	"sunucu boston.celikhost.com — ad sunucuları nasıl ns1.biovision.health ve
//	 ns2.biovision.health olabilir? bir yerlerde bir mantık hatası var."
//
// Vardı. Bir barındırma sunucusunun TEK ad sunucusu çifti olur. Glue kaydını,
// panelin kendi alan adını tutan kayıtçıda BİR KEZ yaptırırsınız; ondan sonra
// barındırılan her alan adı yalnızca o adları göstererek bağlanır — alan adı
// başına glue yok, müşteri başına alt ad sunucusu yok. Her zone'u kendi adıyla
// adlandırmak, bazı barındırıcıların ek olarak sattığı "vanity nameserver"
// özelliğidir; onu varsayılan yapmak, hiç istenmemesi gereken bir kayıtçı
// işini her alan adına yüklemek demekti.

const (
	settingNS1 = "nameserver1"
	settingNS2 = "nameserver2"
)

var validHostname = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

func validDNSHostname(value string) bool {
	value = canonicalDNSName(value)
	if value == "" || len(value) > 253 || !validHostname.MatchString(value) {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
	}
	return true
}

type hostResolver interface {
	LookupHost(context.Context, string) ([]string, error)
}

func resolverAt(address string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		},
	}
}

// Public resolvers come first because a provider cache can retain an obsolete
// answer after the authoritative servers and parent glue are already correct.
// The machine resolver remains the fallback for private names and restricted
// networks.
var nameserverResolvers = []hostResolver{
	resolverAt(`1.1.1.1:53`),
	resolverAt(`8.8.8.8:53`),
	net.DefaultResolver,
}

func lookupNameserverHost(ctx context.Context, host string, resolvers []hostResolver) ([]string, error) {
	var lastErr error
	for _, resolver := range resolvers {
		lookupCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		addrs, err := resolver.LookupHost(lookupCtx, host)
		cancel()
		if err == nil && len(addrs) > 0 {
			return addrs, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

// panelBaseDomain is the panel host's own domain: boston.celikhost.com →
// celikhost.com. This is what the default nameserver names are built from,
// because it is the domain whose registrar entry the operator already
// controls.
// panelBaseDomain, panel makinesinin kendi alan adıdır: boston.celikhost.com →
// celikhost.com. Varsayılan ad sunucusu adları bundan kurulur, çünkü kayıtçı
// kaydını operatörün zaten yönettiği alan adı odur.
func panelBaseDomain() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return baseDomainOf(h)
}

// baseDomainOf strips the host label: boston.celikhost.com → celikhost.com.
// Separated so the rule is testable without a machine name.
// baseDomainOf makine etiketini atar: boston.celikhost.com → celikhost.com.
// Kural makine adı olmadan test edilebilsin diye ayrıdır.
func baseDomainOf(hostname string) string {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	parts := strings.Split(h, ".")
	if len(parts) < 2 {
		return "" // an unqualified name tells us nothing
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// serverNameservers returns the pair every hosted zone should delegate to.
// Configured value wins; otherwise it is derived from the panel's own domain;
// if even that is unknown the pair is empty and the UI says so rather than
// inventing names.
// serverNameservers, barındırılan her zone'un devredeceği çifti döndürür.
// Ayarlanmış değer kazanır; yoksa panelin kendi alan adından türetilir; o da
// bilinmiyorsa çift boştur ve arayüz ad uydurmak yerine bunu söyler.
func (p *Panel) configuredNameservers(ctx context.Context) (string, string) {
	ns1 := strings.TrimSpace(p.setting(ctx, settingNS1))
	ns2 := strings.TrimSpace(p.setting(ctx, settingNS2))
	if ns1 != "" && ns2 != "" {
		return ns1, ns2
	}
	return "", ""
}

func (p *Panel) dnsIdentityConfigured(ctx context.Context) bool {
	ns1, ns2 := p.configuredNameservers(ctx)
	role := p.setting(ctx, settingDNSRole)
	if ns1 == "" || ns2 == "" {
		return false
	}
	if role == "standalone" {
		return true
	}
	if role != "paired" || net.ParseIP(p.setting(ctx, settingDNSPeerIP)) == nil {
		return false
	}
	peerNS := p.setting(ctx, settingDNSPeerNS)
	return strings.EqualFold(peerNS, ns1) || strings.EqualFold(peerNS, ns2)
}

func (p *Panel) serverNameservers(ctx context.Context) (string, string) {
	if ns1, ns2 := p.configuredNameservers(ctx); ns1 != "" && ns2 != "" {
		return ns1, ns2
	}
	if base := panelBaseDomain(); base != "" {
		return "ns1." + base, "ns2." + base
	}
	return "", ""
}

// nameserverFact is one name and where it actually points, right now.
// nameserverFact bir addır ve şu an gerçekte nereyi gösterdiğidir.
type nameserverFact struct {
	Host       string   `json:"host"`
	IPs        []string `json:"ips"`
	PointsHere bool     `json:"points_here"`
}

// verifyNameservers resolves the pair and reports whether each name actually
// answers for THIS machine.
//
// Without this the panel confidently told an operator to delegate a domain to
// ns1.celikhost.com while that name resolved to a DIFFERENT server of theirs
// (Frankfurt, 72.62.38.15) and the domain was hosted here (Boston, 2.25.80.4).
// Following that instruction would have sent every query for the domain to a
// machine with no zone for it — the domain would simply not work, and the panel
// would have been the one that said to do it. The operator caught it by knowing
// their own infrastructure: "isn't celikhost.com on Frankfurt?"
//
// A guessed default is only safe when it is checked. This is the check.
//
// verifyNameservers çifti çözer ve her adın gerçekten BU makine adına cevap
// verip vermediğini bildirir.
//
// Bu olmadan panel, operatöre bir alan adını ns1.celikhost.com'a devretmesini
// güvenle söylüyordu; oysa o ad onların BAŞKA bir sunucusuna çözülüyordu
// (Frankfurt, 72.62.38.15) ve alan adı burada barındırılıyordu (Boston,
// 2.25.80.4). O talimatı uygulamak, alan adına gelen her sorguyu o alan adının
// zone'u olmayan bir makineye yollardı — alan adı düpedüz çalışmazdı ve bunu
// söyleyen panel olurdu. Operatör bunu kendi altyapısını bilerek yakaladı:
// "celikhost.com ana domaini Frankfurt'ta değil mi?"
//
// Tahmin edilen varsayılan, ancak kontrol edildiğinde güvenlidir. Kontrol budur.
func verifyNameservers(ctx context.Context, hosts []string, serverIP, serverV6 string) []nameserverFact {
	out := make([]nameserverFact, 0, len(hosts))
	for _, h := range hosts {
		// Keep the JSON contract stable on a fresh install or during a DNS
		// outage. A nil slice is encoded as null, while every consumer expects
		// an array it can safely inspect.
		f := nameserverFact{Host: h, IPs: make([]string, 0)}
		if h == "" {
			out = append(out, f)
			continue
		}
		addrs, err := lookupNameserverHost(ctx, h, nameserverResolvers)
		if err == nil {
			f.IPs = addrs
			for _, a := range addrs {
				if (serverIP != "" && a == serverIP) || (serverV6 != "" && a == serverV6) {
					f.PointsHere = true
				}
			}
		}
		out = append(out, f)
	}
	return out
}

// nameserverPairUsable validates the topology, not just each hostname in
// isolation. Standalone mode expects both names here. Paired mode expects one
// name here and one at the configured peer.
func nameserverPairUsable(role, peerIP string, facts []nameserverFact) bool {
	if len(facts) != 2 {
		return false
	}
	switch normalizeDNSRole(role) {
	case "standalone":
		return facts[0].PointsHere && facts[1].PointsHere
	case "paired":
		// Validated below.
	default:
		return false
	}
	here, peer := 0, 0
	for _, fact := range facts {
		atHere := fact.PointsHere
		atPeer := peerIP != "" && containsStr(fact.IPs, peerIP)
		// The two roles must be held by two different names. One multi-A name
		// pointing at both machines does not rescue a missing second server.
		if atHere == atPeer {
			return false
		}
		if atHere {
			here++
		}
		if atPeer {
			peer++
		}
	}
	return here == 1 && peer == 1
}

func (p *Panel) setting(ctx context.Context, key string) string {
	var v string
	_ = p.db.GetDB().QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key = ?`, key).Scan(&v)
	return v
}

func (p *Panel) setSetting(ctx context.Context, key, value string) error {
	_, err := p.db.GetDB().ExecContext(ctx,
		`INSERT INTO panel_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		key, value)
	return err
}

type nameserverSettings struct {
	NS1 string `json:"ns1"`
	NS2 string `json:"ns2"`
	// Derived reports whether these are the defaults built from the panel's
	// hostname rather than a deliberate choice — the UI says "we guessed this
	// from your server's name, confirm it" instead of implying it was set.
	// Derived, bunların bilinçli bir seçim değil, panelin makine adından
	// kurulmuş varsayılanlar olup olmadığını bildirir — arayüz "bunu
	// sunucunuzun adından tahmin ettik, doğrulayın" der; ayarlanmış gibi
	// göstermez.
	Derived  bool   `json:"derived"`
	ServerIP string `json:"server_ip"`
	// Facts is where each name actually points. Usable means the public mapping
	// matches the saved mode: both names here when standalone, or one here and
	// one at the configured peer when paired.
	// Facts, her adın gerçekte nereyi gösterdiğidir. Usable, kamusal eşleşmenin
	// kayıtlı moda uyduğunu belirtir: tek başına modda iki ad burada; eşli modda
	// biri burada, diğeri yapılandırılmış eşte.
	Facts  []nameserverFact `json:"facts"`
	Usable bool             `json:"usable"`
}

// handleNameserverSettings keeps the legacy pair read contract. Legacy writes
// fail closed so the complete DNS topology is published through dns-setup.
// handleNameserverSettings eski çift okuma sözleşmesini korur. Eski yazmalar,
// DNS topolojisinin tamamı dns-setup üzerinden yayınlansın diye kapalı reddedilir.
func (p *Panel) handleNameserverSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}

	switch r.Method {
	case http.MethodGet:
		ns1, ns2 := p.serverNameservers(r.Context())
		ip4, ip6 := serverPrimaryIP(), serverPrimaryIPv6()
		facts := verifyNameservers(r.Context(), []string{ns1, ns2}, ip4, ip6)
		usable := nameserverPairUsable(
			p.setting(r.Context(), settingDNSRole),
			p.setting(r.Context(), settingDNSPeerIP),
			facts,
		)
		json.NewEncoder(w).Encode(nameserverSettings{
			NS1:      ns1,
			NS2:      ns2,
			Derived:  p.setting(r.Context(), settingNS1) == "",
			ServerIP: ip4,
			Facts:    facts,
			Usable:   usable,
		})

	case http.MethodPut:
		writeDNSSetupRequired(w)

	default:
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
