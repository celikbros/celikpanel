package main

import (
	"context"
	"errors"
	"net"
	"testing"
)

// The verdict this endpoint prints is the sentence an operator acts on, so it
// must never round "half connected" up to "connected". The distinction that
// matters: an A record pointing here is enough for a WEBSITE and a
// certificate, but only nameserver delegation gives the panel the zone — which
// is what mail authentication and automatic record management need.
//
// Bu ucun bastığı karar, operatörün üzerine iş yaptığı cümledir; bu yüzden
// "yarı bağlı"yı asla "bağlı"ya yuvarlamamalıdır. Önemli ayrım: buraya bakan
// bir A kaydı bir WEB SİTESİ ve sertifika için yeterlidir, ama zone'u panele
// veren şey yalnız nameserver devridir — posta kimlik doğrulamasının ve
// otomatik kayıt yönetiminin ihtiyacı olan da odur.
func TestClassifyConnection(t *testing.T) {
	const ip = "2.25.80.4"
	base := func() connectionCheck {
		return connectionCheck{
			Domain:      "biovision.health",
			ServerIP:    ip,
			Nameservers: []string{"ns1.biovision.health", "ns2.biovision.health"},
		}
	}

	for _, c := range []struct {
		name       string
		mutate     func(*connectionCheck)
		wantStatus string
		wantSSL    bool
		why        string
	}{
		{
			name:       "hic cozulmuyor",
			mutate:     func(c *connectionCheck) {},
			wantStatus: "unresolved", wantSSL: false,
			why: "yeni alınmış ya da yayılmamış alan adı — sertifika istenemez",
		},
		{
			// The operator's actual situation: parked at the registrar, A record
			// pointing at a completely different server.
			// Operatörün gerçek durumu: kayıtçıda park etmiş, A kaydı bambaşka
			// bir sunucuyu gösteriyor.
			name: "baska sunucuyu gosteriyor",
			mutate: func(c *connectionCheck) {
				c.LiveNameservers = []string{"lunar.dns-parking.com", "solar.dns-parking.com"}
				c.LiveIPs = []string{"2.57.91.91"}
			},
			wantStatus: "elsewhere", wantSSL: false,
			why: "başka sunucuya bakan alan adı için Let's Encrypt sertifika veremez",
		},
		{
			name: "yalniz A kaydi buraya bakiyor",
			mutate: func(c *connectionCheck) {
				c.LiveNameservers = []string{"lunar.dns-parking.com"}
				c.LiveIPs = []string{ip}
			},
			wantStatus: "a_record", wantSSL: true,
			why: "web sitesi ve sertifika çalışır; zone panelin değildir",
		},
		{
			name: "nameserverlar devredilmis ve adres tutuyor",
			mutate: func(c *connectionCheck) {
				c.LiveNameservers = []string{"ns1.biovision.health", "ns2.biovision.health"}
				c.LiveIPs = []string{ip}
			},
			wantStatus: "delegated", wantSSL: true,
			why: "tam bağlantı: zone panelde, adres doğru",
		},
		{
			// Delegated to us but the address disagrees — the zone was edited or
			// DNS is still propagating. Saying "connected" here would send the
			// operator to ask Let's Encrypt for a certificate that cannot be
			// issued.
			// Bize devredilmiş ama adres uyuşmuyor — zone düzenlenmiş ya da DNS
			// hâlâ yayılıyor. Burada "bağlı" demek, operatörü alınamayacak bir
			// sertifikayı istemeye yollardı.
			name: "devredilmis ama adres tutmuyor",
			mutate: func(c *connectionCheck) {
				c.LiveNameservers = []string{"ns1.biovision.health", "ns2.biovision.health"}
				c.LiveIPs = []string{"203.0.113.9"}
			},
			wantStatus: "delegated_mismatch", wantSSL: false,
			why: "yarı doğruyu tam doğru saymak, alınamayacak sertifikayı istetir",
		},
		{
			name: "nameserverlarin yalniz yarisi devredilmis",
			mutate: func(c *connectionCheck) {
				c.LiveNameservers = []string{"ns1.biovision.health", "lunar.dns-parking.com"}
				c.LiveIPs = []string{ip}
			},
			wantStatus: "a_record", wantSSL: true,
			why: "tek eşleşen NS tam delegasyon değildir; yalnız A kaydı web için yeterlidir",
		},
		{
			// Case must not decide the answer: DNS is case-insensitive and
			// resolvers return whatever the zone happens to carry.
			// Büyük/küçük harf cevabı belirlememeli: DNS harf duyarsızdır ve
			// çözümleyiciler zone'da ne yazıyorsa onu döndürür.
			name: "buyuk harfli nameserver",
			mutate: func(c *connectionCheck) {
				c.LiveNameservers = []string{"NS2.BioVision.Health", "NS1.BioVision.Health."}
				c.LiveIPs = []string{ip}
			},
			wantStatus: "delegated", wantSSL: true,
			why: "harf büyüklüğü bir alan adını bağsız yapmaz",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			cc := base()
			c.mutate(&cc)
			status, ssl := classifyConnection(cc)
			if status != c.wantStatus {
				t.Errorf("status = %q, want %q — %s", status, c.wantStatus, c.why)
			}
			if ssl != c.wantSSL {
				t.Errorf("ssl_ready = %v, want %v — %s", ssl, c.wantSSL, c.why)
			}
		})
	}
}

func TestSameNameserverSetRequiresTheFullSet(t *testing.T) {
	expected := []string{"ns1.celikhost.com", "ns2.celikhost.com"}
	for _, tc := range []struct {
		name string
		live []string
		want bool
	}{
		{name: "same set reordered", live: []string{"NS2.CELIKHOST.COM.", "ns1.celikhost.com"}, want: true},
		{name: "only one", live: []string{"ns1.celikhost.com"}, want: false},
		{name: "one ours one foreign", live: []string{"ns1.celikhost.com", "lunar.dns-parking.com"}, want: false},
		{name: "extra name", live: []string{"ns1.celikhost.com", "ns2.celikhost.com", "ns3.celikhost.com"}, want: false},
		{name: "duplicate name", live: []string{"ns1.celikhost.com", "ns1.celikhost.com"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameNameserverSet(tc.live, expected); got != tc.want {
				t.Fatalf("sameNameserverSet(%v) = %v, want %v", tc.live, got, tc.want)
			}
		})
	}
}

// One server, one nameserver pair. The panel used to write ns1.<domain> into
// every zone, which made each hosted domain its own nameserver and would have
// forced glue registration per customer — the operator caught it on sight
// (25 Jul): "the server is boston.celikhost.com, how can the nameservers be
// ns1.biovision.health?" The names must come from the SERVER's domain, never
// from the domain being hosted.
//
// Tek sunucu, tek ad sunucusu çifti. Panel her zone'a ns1.<alanadı> yazıyordu;
// bu, barındırılan her alan adını kendi ad sunucusu yapıyor ve müşteri başına
// glue kaydını zorunlu kılacaktı — operatör görür görmez yakaladı (25 Tem):
// "sunucu boston.celikhost.com, ad sunucuları nasıl ns1.biovision.health
// olabilir?" Adlar SUNUCUNUN alan adından gelmeli, barındırılan alan adından
// asla.
func TestPanelBaseDomainIsTheServersDomainNotTheHostedOne(t *testing.T) {
	for host, want := range map[string]string{
		"boston.celikhost.com":    "celikhost.com",
		"frankfurt.celikhost.com": "celikhost.com",
		"srv1.eu.example.co":      "example.co",
		"celikhost.com":           "celikhost.com",
		"localhost":               "",
	} {
		if got := baseDomainOf(host); got != want {
			t.Errorf("baseDomainOf(%q) = %q, want %q", host, got, want)
		}
	}
}

type fakeConnectionDNSResolver struct {
	nameservers []*net.NS
	addresses   []string
	nsErr       error
	hostErr     error
}

func (r fakeConnectionDNSResolver) LookupNS(context.Context, string) ([]*net.NS, error) {
	return r.nameservers, r.nsErr
}

func (r fakeConnectionDNSResolver) LookupHost(context.Context, string) ([]string, error) {
	return r.addresses, r.hostErr
}

func nsRecords(names ...string) []*net.NS {
	out := make([]*net.NS, 0, len(names))
	for _, name := range names {
		out = append(out, &net.NS{Host: name})
	}
	return out
}

func TestObserveConnectionResolversSkipsUnavailableAndKeepsPartialAnswers(t *testing.T) {
	sources := []namedConnectionResolver{
		{
			Name: "unavailable",
			Resolver: fakeConnectionDNSResolver{
				nsErr: errors.New("blocked"), hostErr: errors.New("blocked"),
			},
		},
		{
			Name: "address-only",
			Resolver: fakeConnectionDNSResolver{
				nsErr: errors.New("NS timeout"), addresses: []string{"2.25.80.4", "2.25.80.4"},
			},
		},
	}

	got := observeConnectionResolvers(context.Background(), "biovision.health", sources)
	if len(got) != 1 {
		t.Fatalf("observations = %d, want 1: %#v", len(got), got)
	}
	if got[0].Resolver != "address-only" || len(got[0].IPs) != 1 || got[0].IPs[0] != "2.25.80.4" {
		t.Fatalf("unexpected partial observation: %#v", got[0])
	}
}

func TestSummarizeConnectionObservationsReportsSplitPropagation(t *testing.T) {
	base := connectionCheck{
		Domain:      "biovision.health",
		ServerIP:    "2.25.80.4",
		ServerV6:    "2a02:4780:75:efdb::1",
		Nameservers: []string{"ns1.celikhost.com", "ns2.celikhost.com"},
	}
	newView := func(resolver string) connectionResolverObservation {
		return connectionResolverObservation{
			Resolver:    resolver,
			Nameservers: []string{"ns1.celikhost.com", "ns2.celikhost.com"},
			IPs:         []string{"2.25.80.4", "2a02:4780:75:efdb::1"},
		}
	}
	oldNSView := func(resolver string) connectionResolverObservation {
		return connectionResolverObservation{
			Resolver:    resolver,
			Nameservers: []string{"lunar.dns-parking.com", "solar.dns-parking.com"},
			IPs:         []string{"2.25.80.4", "2a02:4780:75:efdb::1"},
		}
	}
	observations := []connectionResolverObservation{
		newView("Cloudflare"), oldNSView("Google"), newView("Quad9"), oldNSView("OpenDNS"),
	}

	selected, enriched, propagating, known := summarizeConnectionObservations(base, observations)
	if !known || !propagating {
		t.Fatalf("known=%v propagating=%v, want true/true", known, propagating)
	}
	if selected.Status != "delegated" || !selected.SSLReady {
		t.Fatalf("selected = %#v, want delegated and SSL-ready", selected)
	}
	delegated, addressOnly := 0, 0
	for _, observation := range enriched {
		switch observation.Status {
		case "delegated":
			delegated++
		case "a_record":
			addressOnly++
		}
	}
	if delegated != 2 || addressOnly != 2 {
		t.Fatalf("statuses delegated=%d a_record=%d: %#v", delegated, addressOnly, enriched)
	}

	// Resolver completion order must not change the verdict.
	reversed := []connectionResolverObservation{
		oldNSView("OpenDNS"), newView("Quad9"), oldNSView("Google"), newView("Cloudflare"),
	}
	selectedAgain, _, propagatingAgain, knownAgain := summarizeConnectionObservations(base, reversed)
	if !knownAgain || !propagatingAgain || selectedAgain.Status != selected.Status ||
		connectionObservationSignature(selectedAgain) != connectionObservationSignature(selected) {
		t.Fatalf("order changed verdict: first=%#v second=%#v", selected, selectedAgain)
	}
}

func TestSummarizeConnectionObservationsUsesMajorityView(t *testing.T) {
	base := connectionCheck{
		ServerIP:    "2.25.80.4",
		Nameservers: []string{"ns1.celikhost.com", "ns2.celikhost.com"},
	}
	newView := connectionResolverObservation{
		Resolver: "new", Nameservers: base.Nameservers, IPs: []string{base.ServerIP},
	}
	oldView := func(resolver string) connectionResolverObservation {
		return connectionResolverObservation{
			Resolver:    resolver,
			Nameservers: []string{"lunar.dns-parking.com", "solar.dns-parking.com"},
			IPs:         []string{"2.57.91.91"},
		}
	}
	selected, _, propagating, known := summarizeConnectionObservations(base, []connectionResolverObservation{
		newView, oldView("old-1"), oldView("old-2"), oldView("old-3"),
	})
	if !known || !propagating || selected.Status != "elsewhere" {
		t.Fatalf("selected=%#v known=%v propagating=%v", selected, known, propagating)
	}
}

func TestSummarizeConnectionObservationsUnknownWhenAllResolversFail(t *testing.T) {
	_, enriched, propagating, known := summarizeConnectionObservations(connectionCheck{}, nil)
	if known || propagating || len(enriched) != 0 {
		t.Fatalf("known=%v propagating=%v enriched=%#v", known, propagating, enriched)
	}
}
