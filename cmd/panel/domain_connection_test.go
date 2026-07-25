package main

import "testing"

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
			name: "hic cozulmuyor",
			mutate: func(c *connectionCheck) {},
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
				c.LiveNameservers = []string{"ns1.biovision.health"}
				c.LiveIPs = []string{"203.0.113.9"}
			},
			wantStatus: "delegated_mismatch", wantSSL: false,
			why: "yarı doğruyu tam doğru saymak, alınamayacak sertifikayı istetir",
		},
		{
			// Case must not decide the answer: DNS is case-insensitive and
			// resolvers return whatever the zone happens to carry.
			// Büyük/küçük harf cevabı belirlememeli: DNS harf duyarsızdır ve
			// çözümleyiciler zone'da ne yazıyorsa onu döndürür.
			name: "buyuk harfli nameserver",
			mutate: func(c *connectionCheck) {
				c.LiveNameservers = []string{"NS1.BioVision.Health"}
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
		"boston.celikhost.com":     "celikhost.com",
		"frankfurt.celikhost.com":  "celikhost.com",
		"srv1.eu.example.co":       "example.co",
		"celikhost.com":            "celikhost.com",
		"localhost":                "",
	} {
		if got := baseDomainOf(host); got != want {
			t.Errorf("baseDomainOf(%q) = %q, want %q", host, got, want)
		}
	}
}
