# Sahibinin denetiminde hizmet işletimi

*12 Eylül 2026 · [English](OWNER-INDEPENDENCE.md) · D-022 uygulama incelemesi*

## Mevcut kapsam

Kaynak değişikliği, ikincil DNS aktarımını isteğe bağlı uzaktan kayıt yönetiminden
ayırır. Kurulu bir hosting sunucusunda CelikPanel'i ve bütün dizinlerini kaldırmanın
güvenli olduğunu **henüz kanıtlamaz**. Aşağıdaki inceleme kalan bağımlılıkları kaydeder.
Bu çalışma sırasında kurulu sunucularda değişiklik yapılmadı.

## DNS davranışı

Yerel ikincil DNS aynı zamanda site, uygulama veya e-posta barındırıyorsa sihirbaz
iki kayıt yönetimi seçeneği sunar:

- **Kayıtları birincil DNS sunucusunda kendim yöneteceğim.** Uzak panel adresi,
  eşleştirme veya kimlik bilgisi istenmez. BIND/PowerDNS yerel ikincil rolünü korur.
  Yeni alan adlarında harici DNS kayıt talimatları ve doğrulama kullanılır; sahibi
  kayıtları birincilde istediği yerel araçla oluşturur.
- **Kayıtları CelikPanel bağlantısıyla yönet.** Mevcut, açıkça yetkilendirilmiş
  bağlantı kayıtları birincilde yayımlar. HTTPS adresi kayıt yönetimi içindir;
  AXFR/IXFR/NOTIFY aktarımının parçası değildir.

Manuel tercih taslakta ve incelenen planın kimliğinde açıkça tutulur. Tercihi
 değiştirmek önceki incelemeyi geçersiz kılar. Önceden kaydedilmiş planda alan yoksa
Alpha71 bağlantı davranışı korunur. Düzenleyici, eski bir yayıncı adresi yoksa
manuel seçeneği gösterir; kayıtlı adres varsa otomatik seçimi korur. Mevcut alan
adlarının DNS sahipliği değiştirilmez. Yalnızca DNS amacı yerel DNS varsayılanını
korur. Manuel ikincil hosting kurulumunda da hosting adımlarından önce gerçek DNS
çifti ve genel ad sunucusu kayıtları doğrulanır.

Mevcut aktarım uygulaması, ikincil bölgeleri oluşturmak için standart RFC 9432
sürüm 2 katalog bölgelerini kullanır. CelikPanel dışından yapılandırılan eş sunucu,
uyumlu kataloğu sunmalı veya tüketmeli ve eşin aktarımına izin vermelidir. İlgisiz
bölgelerle çalışan herhangi bir BIND kurulumu tek başına yeterli değildir. Beklenen
katalog adı `catalog-<birincil IPv4 baytlarının küçük harfli onaltılık değeri>.celikpanel.invalid`,
üye etiketi ise kanonik bölge adının küçük harfli SHA-224 değeridir.
Bkz. [`internal/binddns/pairing.go`](../internal/binddns/pairing.go).
Oluşturulan aktarım izni incelenen eş IP ile sınırlıdır. Bu değişiklik serbest
katalog adı, genel bölge devralma, TSIG eşleştirmesi veya bölge başına karşılıklı
birincil roller eklemez.

## Kanıt

[Yerel doğrulama](validation/native-dns-independence-20260912/README.tr.md),
manuel/otomatik taslak yollarını ve BIND birincil–PowerDNS ikincil çiftini kapsar.
İki geçici Debian 13 makinesinde yönetim hizmetleri durduruldu; standart konumdaki
çalıştırılabilir dosyalar testin kanıt dizinine taşındı. Birincil, CelikPanel include
satırı içermeyen normal bir BIND yapılandırmasına geçirildi. Katalogdan bölge ekleme,
kayıt değiştirme, DNS hizmetlerini yeniden başlatma ve katalogdan bölge kaldırma,
panel API'si olmadan PowerDNS'e yansıdı. Bu, test edilen çiftin DNS işletimini
kanıtlar; bütün hosting hizmetlerinin kaldırma veya makineyi yeniden başlatma
bağımsızlığını kanıtlamaz.

## Bağımlılık incelemesi ve kalan işler

| Alan | Gözlenen bağımlılık | Kaldırma desteğinden önce gereken |
|---|---|---|
| DNS hizmeti ve aktarımı | Yerel named/pdns, dosyalar/veritabanı ve katalog; test edilen aktarım panel süreci istemiyor | DNS verilerini, hizmet hesaplarını ve birimleri korumak; sonraki panel yazmalarında sahibinin değişikliklerini saptamak. |
| Uzaktan kayıt otomasyonu | Kayıt oluşturmak/değiştirmek için seçilen birincil panel bağlantısı gerekiyor | İsteğe bağlı kalmalı; açık devir işleminde yetkiler kaldırılmalı. Mevcut DNS hizmeti yerel olarak devam eder. |
| Posta sunucusu sertifika yenilemesi | Certbot kancası `agent --deploy-mail-host-certificate` çağırıyor; yenileme ajan çalışanıyla uygulanıyor | Yönetim ajanından bağımsız Certbot dağıtımı ve hizmet yenilemesi; atomik sertifika yayımı, onaylı kimlik ve eşzamanlı değişiklik korumaları korunmalı. |
| Yeniden başlatma sonrası güvenlik duvarı | `celikpanel-firewall-restore.service` ajanı çağırıyor; `firewall.nft` doğrudan nft sözdizimi yerine sürümlü JSON içerebiliyor | Yerel kalıcı kural kümesi ve bağımsız açılış görevi; diğer tablolar ve incelenen SSH erişimi korunarak açılış/hata testleri yapılmalı. |
| Node çalışma ortamları | Dosyalar `/opt/celikpanel/runtimes` altında | Çalışma ortamları korunmalı veya birim yolları taşınmalı. `/opt/celikpanel` dizinini topluca silmek desteklenmiyor. |
| ACME doğrulama yolları | `/var/lib/celikpanel-agent/acme-http-01` ve site Certbot çalışma dizini `/var/lib/celikpanel/certbot` | Doğrulama ve sertifika dizinleri ile yerel yenileme görevleri korunmalı/taşınmalı; gerçek yenileme testi yapılmalı. Dizin adı tek başına çalışan ajan bağımlılığı anlamına gelmez. |
| Panel HTTPS yenilemesi | Panel sertifikası için ajan çağrılıyor | Panel kancası/sertifikası, ortak site ve posta yenilemesinden ayrı temizlenmeli; Certbot genel olarak kapatılmamalı. |
| Site, posta ve veritabanı | Yerel hizmetler mevcut; bütün üretilen yollar, posta eşlemeleri, kimlik doğrulama kaynakları ve zamanlanmış dağıtımlar kaldırma için doğrulanmadı | Kesin koruma envanteri; yönetim dosyaları yokken web, veritabanı, posta teslimi/girişi, cron ve yenileme kanıtı gerekiyor. |
| Kullanıcı cron işleri | `cron_rpc.go` yerel kullanıcı crontab'larına yazıyor | Kullanıcılar, ev dizinleri ve işler korunmalı; iş komutlarındaki ek panel bağımlılıkları incelenmeli. Yerel cron çalışmalı. |

İlgili kaynaklar: `cmd/agent/mail_host_certificate_linux.go`,
`cmd/agent/mail_host_certificate_renewal.go`,
`deploy/systemd/celikpanel-firewall-restore.service`,
`cmd/agent/firewall_rpc.go`, `cmd/agent/runtime_rpc.go`,
`cmd/agent/ssl_rpc.go`, `internal/hostingpath/path.go`.

Yenileme ve açılış kalıcılığının değiştirilmesi; dayanıklılık, geri alma ve gerçek
hizmet testleri gerektiren ayrı bir geçiştir. Denetimsiz dosya kopyalayan betikler
D-022'yi karşılamaz. Bu değişiklik kaldırma komutu sunmaz veya onaylamaz. Kaldırma,
sahibinin incelediği, bütün hizmet verilerini koruyan ve yerel yönetim yolunu
belgeleyen bir işlem olarak tamamlanmalıdır.