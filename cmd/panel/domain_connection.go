package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// "Is this domain actually pointed at this server?" — the question the panel
// never answered, and the one every other step silently depends on.
//
// The operator hit it head-on (25 Jul): they added biovision.health, could not
// find where SSL is configured, and asked what to do about nameservers. All
// three are the same question. A certificate cannot be issued until the domain
// resolves here; mail cannot be delivered until the MX points here; and the
// panel knew all of that and said none of it. The registrar screen showed
// nameservers still parked at the registrar and an A record pointing at a
// completely different server — facts the panel could have read in one lookup
// and explained in one sentence.
//
// So this endpoint answers, live and without guessing:
//   - what this server's address is,
//   - what the domain's nameservers currently are, in the real world,
//   - what the domain currently resolves to, in the real world,
//   - and therefore what the operator still has to do at their registrar.
//
// Nothing here is advice in the abstract: every value it prints is a value the
// operator can copy into the registrar's form.
//
// "Bu alan adı gerçekten bu sunucuyu mu gösteriyor?" — panelin hiç
// cevaplamadığı ve diğer her adımın sessizce bağlı olduğu soru.
//
// Operatör bununla doğrudan karşılaştı (25 Tem): biovision.health'i ekledi,
// SSL'in nereden ayarlandığını bulamadı ve nameserver'ları ne yapacağını sordu.
// Üçü de aynı sorudur. Alan adı buraya çözülmeden sertifika alınamaz; MX buraya
// bakmadan posta teslim edilemez; panel bunların hepsini biliyordu ve hiçbirini
// söylemiyordu. Kayıtçı ekranı, hâlâ kayıtçıda park etmiş nameserver'ları ve
// bambaşka bir sunucuyu gösteren bir A kaydını gösteriyordu — panelin tek
// sorguda okuyup tek cümlede açıklayabileceği olgular.
//
// Bu yüzden bu uç, canlı olarak ve tahmin etmeden şunları söyler:
//   - bu sunucunun adresi nedir,
//   - alan adının nameserver'ları gerçek dünyada şu an nedir,
//   - alan adı gerçek dünyada şu an neye çözülüyor,
//   - ve dolayısıyla operatörün kayıtçıda daha ne yapması gerekiyor.
//
// Buradaki hiçbir şey soyut tavsiye değildir: bastığı her değer, operatörün
// kayıtçının formuna kopyalayabileceği bir değerdir.

type connectionCheck struct {
	Domain   string `json:"domain"`
	ServerIP string `json:"server_ip"`
	ServerV6 string `json:"server_ipv6,omitempty"`

	// What the panel would have you delegate to, if you choose that route.
	// Bu yolu seçerseniz devredeceğiniz adlar.
	Nameservers []string `json:"nameservers"`

	// The real world, right now.
	// Gerçek dünya, şu an.
	LiveNameservers []string `json:"live_nameservers"`
	LiveIPs         []string `json:"live_ips"`

	// Verdict: delegated | a_record | elsewhere | unresolved
	// Karar: devredilmiş | a_kaydı | başka yerde | çözülmüyor
	Status string `json:"status"`
	// SSLReady is the honest precondition: Let's Encrypt cannot issue a
	// certificate for a name that does not resolve to this machine.
	// SSLReady dürüst ön koşuldur: Let's Encrypt, bu makineye çözülmeyen bir
	// ad için sertifika veremez.
	SSLReady bool `json:"ssl_ready"`
	// GlueNeeded is true only when the nameserver names live INSIDE this
	// domain, which is the one case where the operator must register glue for
	// this particular domain. With the server's shared pair
	// (ns1.celikhost.com) the glue was registered once, on the panel's own
	// domain, and a hosted domain only has to point at those names. Telling
	// someone to register glue for ns1.celikhost.com under biovision.health
	// would be an instruction they cannot carry out.
	// GlueNeeded yalnız ad sunucusu adları BU alan adının İÇİNDE yaşıyorsa
	// doğrudur; operatörün tam da bu alan adı için glue kaydettirmesi gereken
	// tek durum odur. Sunucunun ortak çiftinde (ns1.celikhost.com) glue bir kez,
	// panelin kendi alan adında kaydedilmiştir ve barındırılan alan adının tek
	// yapması gereken o adları göstermektir. Birine biovision.health altında
	// ns1.celikhost.com için glue kaydettirmesini söylemek, yerine
	// getiremeyeceği bir talimat olurdu.
	GlueNeeded bool `json:"glue_needed"`
	// NameserversUsable is false when the public names do not match the saved
	// operating mode: both local for standalone, or one local and one at the
	// configured peer for paired mode. Route A must not be offered until then.
	// NameserversUsable, kamusal adlar kayıtlı çalışma moduyla eşleşmiyorsa
	// yanlıştır: tek başına modda ikisi yerel; eşli modda biri yerel, biri
	// yapılandırılmış eşte olmalıdır. O zamana dek A yolu sunulmamalıdır.
	NameserversUsable bool             `json:"nameservers_usable"`
	NameserverFacts   []nameserverFact `json:"nameserver_facts,omitempty"`
	CheckedAt         string           `json:"checked_at"`
}

// handleDomainConnection answers GET /api/v1/domains/{id}/connection.
// handleDomainConnection, GET /api/v1/domains/{id}/connection'a cevap verir.
func (p *Panel) handleDomainConnection(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var name string
	if err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT name FROM domains WHERE id = ?`, domainID).Scan(&name); err != nil {
		writeClientError(w, http.StatusNotFound, "domain not found")
		return
	}

	out := connectionCheck{
		Domain:    name,
		ServerIP:  serverPrimaryIP(),
		ServerV6:  serverPrimaryIPv6(),
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// The SERVER's pair — the same names the zone template delegates to, so the
	// screen and the served zone cannot disagree. Not ns1.<this domain>: that
	// was the logic error the operator caught (25 Jul).
	// SUNUCUNUN çifti — zone şablonunun devrettiği adların aynısı; böylece ekran
	// ile sunulan zone birbirine ters düşemez. ns1.<bu alan adı> değil: operatörün
	// yakaladığı mantık hatası oydu (25 Tem).
	if ns1, ns2 := p.serverNameservers(r.Context()); ns1 != "" && ns2 != "" {
		out.Nameservers = []string{ns1, ns2}
	}

	// A live lookup can hang on a broken resolver; the screen must still come
	// back. Whatever is unknown stays empty and is reported as unknown.
	// Canlı sorgu bozuk bir çözümleyicide asılabilir; ekran yine de dönmeli.
	// Bilinmeyen boş kalır ve bilinmiyor diye bildirilir.
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	res := &net.Resolver{}
	if ns, err := res.LookupNS(ctx, name); err == nil {
		for _, n := range ns {
			out.LiveNameservers = append(out.LiveNameservers, strings.TrimSuffix(n.Host, "."))
		}
		sort.Strings(out.LiveNameservers)
	}
	if addrs, err := res.LookupHost(ctx, name); err == nil {
		out.LiveIPs = append(out.LiveIPs, addrs...)
		sort.Strings(out.LiveIPs)
	}

	for _, ns := range out.Nameservers {
		if strings.HasSuffix(ns, "."+name) {
			out.GlueNeeded = true
		}
	}
	// Do the nameserver names this panel advertises actually answer for this
	// machine? If not, saying "delegate here" would be advice that breaks the
	// domain. Glue names inside this very zone are exempt: they cannot resolve
	// until the delegation exists, so their absence is expected, not a fault.
	// Panelin ilan ettiği ad sunucusu adları gerçekten bu makine adına cevap
	// veriyor mu? Vermiyorsa "buraya devredin" demek, alan adını bozan bir
	// tavsiye olurdu. Tam da bu zone'un içindeki glue adları muaftır: devir
	// oluşmadan çözülemezler, yani yoklukları beklenen bir durumdur, arıza değil.
	if len(out.Nameservers) == 2 {
		if out.GlueNeeded {
			out.NameserversUsable = true
		} else {
			facts := verifyNameservers(ctx, out.Nameservers, out.ServerIP, out.ServerV6)
			out.NameserverFacts = facts
			out.NameserversUsable = nameserverPairUsable(
				p.setting(ctx, settingDNSRole),
				p.setting(ctx, settingDNSPeerIP),
				facts,
			)
		}
	}

	out.Status, out.SSLReady = classifyConnection(out)
	json.NewEncoder(w).Encode(out)
}

// classifyConnection turns the lookups into the one sentence the operator
// needs. Kept separate from the I/O so the decision is testable.
//
// The distinction that matters: pointing an A record here is enough for a
// WEBSITE and a certificate, but only delegating the nameservers gives the
// panel the zone — which is what mail authentication (SPF/DKIM/DMARC) and
// automatic record management need. Telling someone "you are connected" when
// only half of that is true is the kind of half-truth this project keeps
// deleting.
//
// classifyConnection, sorguları operatörün ihtiyacı olan tek cümleye çevirir.
// Karar test edilebilsin diye G/Ç'den ayrıdır.
//
// Önemli ayrım: buraya bir A kaydı yöneltmek bir WEB SİTESİ ve sertifika için
// yeterlidir, ama zone'u panele veren şey yalnız nameserver devridir — posta
// kimlik doğrulamasının (SPF/DKIM/DMARC) ve otomatik kayıt yönetiminin ihtiyacı
// olan da odur. Bunun yalnız yarısı doğruyken birine "bağlandınız" demek, bu
// projenin sürekli sildiği türden bir yarı-doğrudur.
func classifyConnection(c connectionCheck) (status string, sslReady bool) {
	pointsHere := false
	for _, ip := range c.LiveIPs {
		if (c.ServerIP != "" && ip == c.ServerIP) || (c.ServerV6 != "" && ip == c.ServerV6) {
			pointsHere = true
			break
		}
	}

	delegated := sameNameserverSet(c.LiveNameservers, c.Nameservers)

	switch {
	case delegated && pointsHere:
		return "delegated", true
	case delegated:
		// The nameservers are ours but the address does not match — usually a
		// zone we serve whose A record was edited, or DNS still propagating.
		// Nameserver'lar bizim ama adres tutmuyor — genelde sunduğumuz bir
		// zone'un A kaydı değiştirilmiştir ya da DNS hâlâ yayılıyordur.
		return "delegated_mismatch", false
	case pointsHere:
		// Registrar keeps the DNS, an A record points here. A website and a
		// certificate work; the panel does not own the zone.
		// DNS kayıtçıda, bir A kaydı buraya bakıyor. Web sitesi ve sertifika
		// çalışır; zone panelin değildir.
		return "a_record", true
	case len(c.LiveIPs) > 0:
		return "elsewhere", false
	default:
		return "unresolved", false
	}
}

func sameNameserverSet(live, expected []string) bool {
	if len(live) == 0 || len(live) != len(expected) {
		return false
	}
	seen := make(map[string]bool, len(live))
	for _, ns := range live {
		seen[strings.ToLower(strings.TrimSuffix(ns, "."))] = true
	}
	for _, ns := range expected {
		if !seen[strings.ToLower(strings.TrimSuffix(ns, "."))] {
			return false
		}
	}
	return true
}
