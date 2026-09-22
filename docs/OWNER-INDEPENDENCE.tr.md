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

## Ortak firewall politika sözleşmesi (kaynak aşaması)

D-025 ilkeleri 1, 3, 6 / P0.5. Kalıcı firewall üreticisi, okuyucusu ve açılış
kuralları hazırlığı artık `internal/firewallpolicy` paketini paylaşıyor. Paket
yalnız Go standart kütüphanesini kullanır; Agent, uygulama veritabanı, lisans,
sunucu komutu veya dosya erişimi bağımlılığı yoktur. Agent aynı paketten v2
yazar, eski/v2 dosyayı okur ve güncel doğrulanmış SSH portları ile kaydedilmiş
SSH geçiş korumasını birleştirir.

V2 JSON baytları, boş liste temsili, boyut sınırı ve eski nft biçimi korundu;
şema geçişi veya disk normalizasyonu yok. Bilinmeyen sürüm, keyfi nft metni,
yinelenen JSON alanı ve geçersiz SSH portu reddedilir. Yalnız `inet celikpanel_fw`
tablosu hedeflenir. Yetki, güvenli dosya okuma, SSH keşfi, nft ön kontrolü,
atomik uygulama ve geri alma mevcut çağıranda kalır; kural üretmek başarı kanıtı değildir.

Paket ve mevcut Agent firewall testleri yarış algılayıcıyla geçti. Kurulu açılış
uniti hâlâ Agent'ı çağırıyor. Bağımsız tüketicinin paketlenmesi, kalıcı
politika/açılış geçişi, hata kurtarması ve Agent yokken gerçek yeniden başlatma
kabulü açık. Kurulu sunucular değiştirilmedi.


## Bağımsız firewall okuyucusu: sınırlı yerel kanıt

Kaynak `cmd/firewall-restore`, ortak eski/v2 politikayı Agent, uygulama
veritabanı veya lisans olmadan okur. [Debian AR kanıtı](../deploy/e2e/release-recovery/FIREWALL-BOOT.md),
yönetim binary’leri yokken normal yeniden başlatma, yeni SSH bağlantısı ve
bağımsız nft tablosunun korunmasını doğrular. Deneme, mevcut root sahipliğini
ve yazma izni olmayan `celikpanel` grup düzenini kullanır. Kayıtlı şema değişmez.

Kurulu unit hâlâ Agent kullanır. Paketleme, ortak değişiklik kilidi,
güncelleme/geri alma sırasında okuyucu koruması, başarısız açılış kurtarması
ve kalan yerel iş yükü matrisi açıktır; bu sonuç P0.5’i kapatmaz veya panelin
kaldırılmasını onaylamaz.

Sonraki [ortak kilit kaynağı ve yerel çakışma kanıtı](../deploy/e2e/release-recovery/FIREWALL-BOOT.md#shared-process-exclusion-next-source-stage),
bağımsız okuyucu ve Agent girişlerini kapsar. Meşgul kilit, doğrulanmamış
ve belirsiz sonucu korur; süreç ölünce kilidi çekirdek bırakır. Bu,
kaynak düzeyindeki ortak kilit açığını kapatır; kurulu sistemde devreye alma
ve bütün eşzamanlılık/güncelleme matrisi tamamlanmış değildir.


## Sürümlü firewall paketi (henüz etkin değil)

Derleme artık `firewall-runtime/` paketini uygulama binary dizininden ayrı hazırlar.
`celikpanel-firewall-runtime/v1`; politika okuyucusu sürüm 2’yi, binary’yi ve
sabit v1 şablonundan üretilen systemd birimini aynı generation’a bağlar.
Birim `/usr/libexec/celikpanel/firewall/<generation>/restore` yolunu kullanır.
Bilinmeyen sürüm, kanonik olmayan manifest, değiştirilmiş binary/birim reddedilir.
Mevcut eski/v2 politika dosyasının biçimi değişmez.

Çevrimdışı paketleyici tanınmayan çıktıyı ve önceki doğrulanmış derlemeyi
korur. Normal arşiv ve kaynak bootstrap aynı bağımsız paketi taşır. Paket
hazırlamak, kurulum yetkisi vermez. Kalıcı yayın, birim geçişi ve geri alma
koruması uygulanıp kanıtlanana kadar mevcut installer/açılış birimi korunur.

Sözleşme/paketleyici yarış testleri, bağlantılı girdi ve kullanıcının düzenlediği
çıktının reddi geçti. Linux dosya sisteminde Go 1.26.5 ile 022/077 umask altında
iki derleme aynı baytları ve 0755/0644 izinlerini üretti. Windows paylaşımının
0777 gösterimi reddedildi; izin denetimi gevşetilmedi. Bu, tam sürüm veya yerel
etkinleştirme kanıtı değildir.


## Kalıcı güvenlik duvarı hazırlığı (birim etkinleştirme henüz açık)

Aday kurtarma girişi, `prepare-firewall-runtime --source <mutlak incelenmiş
firewall-runtime dizini> --transaction-fd 9` komutunu destekler. Root, devralınan
tekil sürüm kilidi ve etkin işlem işaretçilerinin yokluğu gerekir. Çağıran
önce dış sürüm paketini kabul etmelidir. Bu iç komut güncelleme başlatmaz,
birim kurmaz, kural uygulamaz veya hizmet başlatmaz; eski seçili kurtarma
çalıştırıcısına yönlendirilmez.

Hazırlık, kurtarmanın açık dosya tanıtıcılarıyla kimlik ve root sahipliği
doğrulamasını paylaşır. Yalnız tamamı doğrulanan nesil sabit bağımsız
konuma yayımlanır. Dosyalar ve dizinleri diske eşitlenir; var olan hedefin
üzerine yazılmaz. Sonuç bildirilmeden üst dizin de eşitlenir. Tekrar denemede
mevcut nesil yeniden doğrulanır. Yarım dizinler, eski nesiller ve sahip
değişiklikleri korunur. Politika veya kurulu birim biçimi değişmez.

Kilit devri, etkin işlem reddi, güvensiz kaynak, sonradan kaynak/hedef değişikliği
ve hedef çakışması testleri geçti. İlk dosya, kalıcı dizin, yayım ve üst
dizin eşitleme sınırlarındaki gerçek SIGKILL sonrası tekrar hazırlık başarılı.
Bunlar yerel Linux dosya sistemi/süreç testleridir; güç kaybı veya yerel
hizmet etkinleştirme kabulü değildir. Otomatik kurucu/güncelleyici bağlantısı,
tam birim geçişi, geri almada yardımcıların korunmasının kanıtı ve kalan
P0.5 hizmet matrisi açıktır.


Sonraki ön kontrol bağlantısı, paketi taşıyan sürümlerde hazırlığı
çağırır: güncelleyicide kurtarma kiti geçişi ve hizmet duruşundan, ilk
kurucuda kalıcı temel niyeti yayımından önce. Paketi olmayan tarihsel
arşivlerin yolu korunur. Ret akışı durdurur; yarım hazırlık yanlış
"değişmedi" yerine `firewall_runtime_preparation_unconfirmed` sonucu verir.
Gerçek FD devri ve ilk kurucu fonksiyonu testleri başarılı hazırlığı ve bozuk
paket reddini kapsar. Kurulu güvenlik duvarı birimi henüz bu yardımcıya geçmez.


[Debian AR nesil kabulü](../deploy/e2e/release-recovery/FIREWALL-GENERATIONS.md),
paketlenen gerçek birimin Panel/Agent dosyaları yokken üç A/B/A açılışını
kanıtlar. İki yardımcı nesli, kayıtlı politika ve ayrı yerel tablo korunur.
Eksik tablo deneyinin ilk sonucu da saklanır. Nesiller aynı okuyucunun iki
derlemesidir; anlamsal sürüm geçişi veya normal uygulama güncelleme/geri alma
kanıtı değildir. P0.5 açık kalır.


## Paketlenen yerel birim geçişi (kaynak aşaması)

D-025 ilkeleri 1, 3, 4 / P0.3, P0.5. Çevrimdışı dağıtım ve kaynak bootstrap
sürümleri, paketteki güvenlik duvarı biriminin aynı baytlarını incelenen systemd
içeriğine koyar. Kaynak ağacındaki eski birim uyumluluk için korunur; doğrudan
kaynak ağacı kurulumu veya panel kaldırma desteği tamamlanmış sayılmaz. Yardımcı,
koordinatörler durmadan hazırlanır; aday okuyucu hedef birimi korunan nesliyle
doğrular. Yeni paket kurulumu aynı kanıtı temel kurulum niyetinden önce ister.

Atomik eski/aday birim geçişi artık gidilecek birimin yardımcısını da doğrular.
Bozuk aday, değişikliksiz tekrar dahil yayını durdurur; sağlam eski birime geri
almayı engelleyemez. Eski bağımsız birim kendi korunan yardımcısını gerektirir.
Bilinmeyen şablon, sahip düzenlemesi, okuma hatası veya eksik/değişmiş yardımcı
kanıtları korur ve ilgili geçişi durdurur. Her iki yardımcı nesli uygulama
dosyalarının değiştirilmesinden ayrı kalır. Eski/v2 politika, v1 yapıt ve v6
tam snapshot biçimleri değişmez.

Kanıt: tam runtime/CLI yarış testleri; salt-okur birim/yardımcı doğrulamasının
bozuk, bağlı ve güvensiz dosyaları reddi; gerçek kurulum/güncelleme önkontrol
fonksiyonları; hedef yardımcıya göre ret ve eski birime geri alma shell testleri.
Bunlar kaynak/bileşen kanıtıdır. Önceki yerel A/B/A deneyi tam paket birimini
kullanır fakat normal uygulama güncelleme gövdesini kanıtlamaz. Tam imzalı
güncelleme/otomatik geri alma, hatalı boot konsol kurtarması, Arch ve kalan
iş yükü/yenileme matrisi açıktır. Kurulu kullanıcı sunucusu değiştirilmez.


Sonraki [Debian AS/AT yerel kabulü](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md),
yalıtılmış test imzasıyla gerçek Agent güncelleme yürütücüsünü kapsar:
bağımsız birim yayımlanır; ikinci denemede aday birim/yardımcı yayınından
sonra yürütücü öldürülür ve yerel kurtarma eski birimi otomatik geri getirir.
İki yol da politika, ayrı tablo, yardımcı ve HTTPS erişimini yeniden açılışta korur.
AS, eski kapalı boot durumunu koruduktan sonra test için açıkça etkinleştirildi;
AT, baştan etkin olan durumu korudu. Üretim arayüzü/imzası, Arch, bozuk yardımcının
boot/konsol kurtarması ve P0.5'in diğer kabul işleri açıktır.

Arch AU, gerçek güncellemede yeni firewall unit’i yayımlandıktan sonraki kesintiyi, otomatik geri almayı ve yeni açılışı doğruladı. [Kanıt ve sınırlar](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md#arch-automatic-rollback-and-boot-au). Arch başarılı ileri güncelleme deneyi ve P0.5 bütünü açıktır. Eski Agent’ın geçici platform tespit hatasını kalıcı güncelleme reddi olarak saklaması ayrı bir P0.2/P0.4 düzeltmesi gerektirir; test Agent’ını yeniden başlatmak bu kabul maddesini kapatmaz.

[Ortak mail sertifika v1 sözleşmesi](MAIL-CERTIFICATE-ARTIFACT.md), kayıt/bekleyen yenileme/lineage ayrıştırmasını ve sertifika doğrulamasını Agent dışına taşır. Gerçek Alpha81 üreticisinin byte’ları yeni ortak ve Agent okuyucularında aynen korunur. Bu P0.4/P0.5 temel adımıdır; bağımsız yenileme tüketicisi, hook geçişi ve yönetim programları yokken gerçek yenileme kabulü açıktır.

[Ortak dosya okuyucusu](MAIL-CERTIFICATE-ARTIFACT.md#shared-descriptor-reader), Agent’ın gerçek mail sertifika okumalarında kullanılır. Eski dosya güven kuralları korunur; okuma sırasında sahibin değiştirdiği seçim geri yazılmadan reddedilir. Bağımsız yayın/yeniden yükleme ve Agent yokken yenileme kabulü açıktır; yeni kalıcı şema veya kurulu sistem geçişi yoktur.

[Arch AV ileri güncelleme ve açılış kanıtı](../deploy/e2e/release-recovery/PLATFORM-UPDATE-AV.md)
aynı aday yardımcı/birim, sahip etkinleştirme durumu, politika, ilgisiz tablo ve
HTTPS korunarak geçti. Yeni Agent, açılıştaki geçici ret sonrası aynı PID ve
süreç kimliğiyle hazır kontrol sonucu verdi. Bu sınırlı kabul tamamlandı;
P0.2/P0.3/P0.5 matrisi, üretim arayüzü/imza güveni ve bağımsız yenileme açık.

[Ortak sertifika yayınlama kilidi](MAIL-CERTIFICATE-ARTIFACT.md#shared-publication-exclusion)
eski sabit flock kimliğini korur; sınırlı beklemeyi destekler ve değiştirilmiş
kilidi izinlerini düzeltmeden reddeder. Agent ortak kodu kullanıyor; ayrı süreçle
kilitleme ve süreç kesintisi testleri geçti. Yerel yenileme, dış mutasyon kilidi,
hook geçişi ve yönetim yokken kabul P0.4/P0.5 kapsamında açık kalıyor.

[Ortak yerel Certbot okuyucusu](MAIL-CERTIFICATE-ARTIFACT.md#shared-native-certbot-source-reader)
gerçek Agent panel/mail yollarında aynı sınırlı live/archive okumasını kullanır.
Zincir, süre ve amaç denetimleri çağıranda korunur; mevcut olumsuz kaynak testleri
geçti. Yerel yenileme hook'u ve yönetim yokken kabul henüz tamamlanmadı.


[AX mail kabul deneyi](../deploy/e2e/release-recovery/MAIL-CONTRACT-AX.md), gerçek
servislerle ortak sözleşme üzerinden yenilemeyi, Debian yeniden başlatmasından
sonra TLS'nin korunmasını ve tamamlanmış yenileme tekrarında sahibin sertifika
seçimiyle bekleyen işin korunmasını doğruladı. P0.4/P0.5 kısmen kanıtlıdır:
yenileme hâlâ Agent kodunu kullanıyor; bağımsız yardımcının yetkilendirilmesi,
tekrar denemesi, hook geçişi ve panel kaldırma kabulü açıktır. Kurulu kullanıcı
panelinde değişiklik yapılmadı.
