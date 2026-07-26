package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
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
	// ResolverObservations keeps "Check again" honest while a DNS change is
	// spreading. One recursive cache can be hours behind the parent
	// delegation; treating that single cache as the whole Internet made the
	// panel keep showing the previous provider after the domain was delegated.
	ResolverObservations []connectionResolverObservation `json:"resolver_observations,omitempty"`
	PropagationPending   bool                            `json:"propagation_pending"`

	// Verdict: delegated | a_record | elsewhere | unresolved | unknown
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

type connectionDNSResolver interface {
	LookupNS(context.Context, string) ([]*net.NS, error)
	LookupHost(context.Context, string) ([]string, error)
}

type namedConnectionResolver struct {
	Name     string
	Resolver connectionDNSResolver
}

type connectionResolverObservation struct {
	Resolver    string   `json:"resolver"`
	Nameservers []string `json:"nameservers"`
	IPs         []string `json:"ips"`
	Status      string   `json:"status"`
	SSLReady    bool     `json:"ssl_ready"`
}

// A recheck must not be another question to the same stale ISP cache. Ask
// independent public resolvers in parallel and expose disagreements as DNS
// propagation. The machine resolver remains a last fallback for restricted
// networks and private installations.
var publicConnectionResolvers = []namedConnectionResolver{
	{Name: "Cloudflare (1.1.1.1)", Resolver: resolverAt("1.1.1.1:53")},
	{Name: "Google (8.8.8.8)", Resolver: resolverAt("8.8.8.8:53")},
	{Name: "Quad9 (9.9.9.9)", Resolver: resolverAt("9.9.9.9:53")},
	{Name: "OpenDNS (208.67.222.222)", Resolver: resolverAt("208.67.222.222:53")},
}

func normalizeNameservers(records []*net.NS) []string {
	seen := make(map[string]bool, len(records))
	out := make([]string, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(record.Host), "."))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func normalizeIPs(addrs []string) []string {
	seen := make(map[string]bool, len(addrs))
	out := make([]string, 0, len(addrs))
	for _, raw := range addrs {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil {
			continue
		}
		value := ip.String()
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func observeConnectionResolver(
	ctx context.Context,
	name string,
	source namedConnectionResolver,
) (connectionResolverObservation, bool) {
	var (
		nsRecords []*net.NS
		addresses []string
		nsErr     error
		hostErr   error
		wg        sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		queryCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		defer cancel()
		nsRecords, nsErr = source.Resolver.LookupNS(queryCtx, name)
	}()
	go func() {
		defer wg.Done()
		queryCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		defer cancel()
		addresses, hostErr = source.Resolver.LookupHost(queryCtx, name)
	}()
	wg.Wait()

	observation := connectionResolverObservation{
		Resolver:    source.Name,
		Nameservers: normalizeNameservers(nsRecords),
		IPs:         normalizeIPs(addresses),
	}
	// A partial answer is still useful: for example one address family may be
	// absent. Drop the source only when both questions failed without facts.
	if len(observation.Nameservers) == 0 && len(observation.IPs) == 0 && nsErr != nil && hostErr != nil {
		return connectionResolverObservation{}, false
	}
	return observation, true
}

func observeConnectionResolvers(
	ctx context.Context,
	name string,
	sources []namedConnectionResolver,
) []connectionResolverObservation {
	type result struct {
		index       int
		observation connectionResolverObservation
		ok          bool
	}
	results := make(chan result, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Add(1)
		go func(index int, source namedConnectionResolver) {
			defer wg.Done()
			observation, ok := observeConnectionResolver(ctx, name, source)
			results <- result{index: index, observation: observation, ok: ok}
		}(i, source)
	}
	wg.Wait()
	close(results)

	ordered := make([]*connectionResolverObservation, len(sources))
	for result := range results {
		if result.ok {
			value := result.observation
			ordered[result.index] = &value
		}
	}
	out := make([]connectionResolverObservation, 0, len(sources))
	for _, observation := range ordered {
		if observation != nil {
			out = append(out, *observation)
		}
	}
	return out
}

func connectionObservationSignature(observation connectionResolverObservation) string {
	return strings.Join(observation.Nameservers, ",") + "|" + strings.Join(observation.IPs, ",")
}

func connectionStatusRank(status string) int {
	switch status {
	case "delegated":
		return 5
	case "a_record":
		return 4
	case "delegated_mismatch":
		return 3
	case "elsewhere":
		return 2
	case "unresolved":
		return 1
	default:
		return 0
	}
}

// summarizeConnectionObservations picks the largest matching public view.
// Equal-sized views prefer the one that has progressed toward the configured
// server, while PropagationPending remains true and the UI shows every answer.
// This makes a 2-new/2-old transition useful without hiding the split.
func summarizeConnectionObservations(
	base connectionCheck,
	observations []connectionResolverObservation,
) (connectionResolverObservation, []connectionResolverObservation, bool, bool) {
	type group struct {
		count int
		first int
		rank  int
	}
	groups := make(map[string]group)
	enriched := append([]connectionResolverObservation(nil), observations...)
	for i := range enriched {
		candidate := base
		candidate.LiveNameservers = enriched[i].Nameservers
		candidate.LiveIPs = enriched[i].IPs
		enriched[i].Status, enriched[i].SSLReady = classifyConnection(candidate)
		signature := connectionObservationSignature(enriched[i])
		g, exists := groups[signature]
		if !exists {
			g = group{first: i, rank: connectionStatusRank(enriched[i].Status)}
		}
		g.count++
		groups[signature] = g
	}
	if len(enriched) == 0 {
		return connectionResolverObservation{}, enriched, false, false
	}

	best := group{first: -1}
	bestSignature := ""
	for signature, candidate := range groups {
		if best.first == -1 ||
			candidate.count > best.count ||
			(candidate.count == best.count && candidate.rank > best.rank) ||
			(candidate.count == best.count && candidate.rank == best.rank && signature < bestSignature) {
			best, bestSignature = candidate, signature
		}
	}
	return enriched[best.first], enriched, len(groups) > 1, true
}

// handleDomainConnection answers GET /api/v1/domains/{id}/connection.
// handleDomainConnection, GET /api/v1/domains/{id}/connection'a cevap verir.
func (p *Panel) handleDomainConnection(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
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

	// Ask independent public caches. A single machine/ISP cache is not "the
	// real world", especially immediately after a registrar change.
	lookupCtx, cancelLookup := context.WithTimeout(r.Context(), 4*time.Second)
	observations := observeConnectionResolvers(lookupCtx, name, publicConnectionResolvers)
	cancelLookup()
	if len(observations) == 0 {
		fallbackCtx, cancelFallback := context.WithTimeout(r.Context(), 3*time.Second)
		if fallback, ok := observeConnectionResolver(fallbackCtx, name, namedConnectionResolver{
			Name: "System resolver (fallback)", Resolver: net.DefaultResolver,
		}); ok {
			observations = append(observations, fallback)
		}
		cancelFallback()
	}
	selected, enriched, propagating, known := summarizeConnectionObservations(out, observations)
	out.ResolverObservations = enriched
	out.PropagationPending = propagating
	if known {
		out.LiveNameservers = selected.Nameservers
		out.LiveIPs = selected.IPs
		out.Status = selected.Status
		out.SSLReady = selected.SSLReady
	} else {
		out.Status = "unknown"
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
			verifyCtx, cancelVerify := context.WithTimeout(r.Context(), 4*time.Second)
			facts := verifyNameservers(verifyCtx, out.Nameservers, out.ServerIP, out.ServerV6)
			cancelVerify()
			out.NameserverFacts = facts
			out.NameserversUsable = nameserverPairUsable(
				p.setting(r.Context(), settingDNSRole),
				p.setting(r.Context(), settingDNSPeerIP),
				facts,
			)
		}
	}

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
