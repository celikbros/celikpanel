# Uzak DNS geliştirme kabul kaydı

*11 Eylül 2026 · [English](README.md)*

Bunlar kullanıcının [açık uygulama onayından](../../REMOTE-DNS-AUTHORIZATION.md)
sonraki çalışma ağacı geliştirme sonuçlarıdır. Sürüm yayımlama, üretim eşleştirmesi
veya kurulu panel güncellemesi kanıtı değildir.

## Arayüz sonuçları

| Kontrol | Sonuç ve tam kapsam |
|---|---|
| Tam test paketi | `npm test`: 352 geçti, 0 başarısız. Bu son çalıştırma, kayıtlı liste yerine güncel kanıttaki ad sunucularını gösterme düzeltmesini de içerir. [Tam günlük](web-full-tests.log). |
| Odaklı çalışma zamanı testleri | `node --test tests/remote-dns-ui-runtime.test.mjs tests/server-setup-runtime.test.mjs tests/external-dns-ui-runtime.test.mjs`: 29 geçti, 0 başarısız. Bu son odaklı çalıştırma, güncel ad sunucusu gösterimini ve eski bekleyen kimlik korunurken ayrı onaylı yeni eşleştirmeyi de içerir. [Odaklı günlük](web-focused-tests.log). |
| Üretim derlemesi | `npm run build`: ad sunucusu düzeltmesinden sonra TypeScript, üretim derlemesi ve paket boyutu kontrolleri geçti. [Derleme günlüğü](web-build.log). |
| Tarayıcı | Üretim derlemesi üzerinde dört kontrollü Chrome senaryosu geçti: 1440/390 pikselde Türkçe/İngilizce. Her biri açık eşleştirme ve kurulum onayı istedi, bir eşleştirme ve bir kurulum başlatma isteği gönderdi; ilk girişte yazma, çalışma zamanı hatası veya yatay taşma olmadı. Güncel ad sunucusu düzeltmesinden sonraki son tekrar başarıyla çıktı. Daha sonra eklenen, eski bekleyen bağlantı varken ayrı onaylı yeni eşleştirmeye izin verme davranışı çalışma zamanı regresyonunda doğrulandı; bu dört görüntülenen akışı değiştirmez. [Sonuçlar](browser-results.json), [düzenek](browser-fixture.cjs). |

Çalışma zamanı testleri; kiracı yalıtımı, açılışta yazma olmaması, onay, tek istek
kabulü, sırların işlenmesi, tam bekleyen kimliğin uzlaştırılması, güncel otorite
kanıtı, incelenen bağlantı kimliği, açık yetki iptali, korunan kayıt çakışma
mesajları ve pano kullanılamadığında kurtarmayı kapsar.

[Arayüz kaynak özetleri](ui-source-sha256.json), son tam test ve derleme için
geçerli kaynakları ve testleri tanımlar. Bu liste tam backend kaynak görüntüsü
veya imzalı sürüm bildirimi değildir. Tarayıcı günlüğünün kapsamı korunur; daha sonraki çalıştırma gibi gösterilmez. `browser.log` üretildiği
haliyle saklanır; başarı JSON'a yazıldığı için boş olabilir.

## Sonraki DNS kurulum sırası yönlendirmesi

Gerçek iki sunuculu DNS düzeneği, sıradaki belirsizliği ortaya çıkardı: birincilin
son doğrulaması ikincili beklerken, ikincil önce birincilin DNS kurulumuna ihtiyaç
duyar. Yerel DNS formu artık önce birincili başlatmayı, birincilin DNS kurulum
adımından sonra ikincili başlatmayı ve birincilin son doğrulamasının ikincili
beklediğini açıklar. Her iki rol seçimi de bu yönlendirmeyi gösterir; harici DNS
göstermez.

Sonraki kurulum çalışma zamanı testinde 16/16 geçti
([günlük](web-dns-pair-guidance-tests.log)); üretim derlemesi ve paket bütçesi geçti
([günlük](web-dns-pair-guidance-build.log)).
[Değişen arayüz kaynak özetleri](ui-dns-pair-guidance-sha256.json) sonraki farkı
tanımlar. Önceki 352 testlik tam paket ve dört uzak tarayıcı sonucu özgün kapsamını
korur; bu metin değişikliğinden sonraki çalıştırmalar gibi gösterilmez. Tam DNS
profilinin VM kabulü aşağıda bağlanan gerçek profil kaydında ayrıca saklanır.

## Backend sonuçları ve kaynak sınırı

| Kontrol | Sonuç ve tam kapsam |
|---|---|
| İlk tam Go çalıştırması | Temiz kaynaktaki `make test vet` girişiminde, veritabanı migration satır sonu değişmezi dışında bütün Go paketleri geçti. Migration 042 CRLF içeriyordu; tek hata make işlemini vet çalışmadan durdurdu. [İlk günlük](go-initial-test-failure.log) ve [sonuç](go-initial-result.json) korunuyor. Bu girişim başarılı gösterilmez. |
| Son etkilenen paketler ve tam vet | LF düzeltmesi, Certbot kimlik/dizin düzeltmesi ve sınırlı DNS test düzeneği değişikliklerinden sonra yeni temiz kopyada `cmd/panel` (138.353 sn), `internal/db` (7.246 sn), `cmd/agent` (12.850 sn) ve `internal/hostcmd` (1.040 sn) testlerinin tamamı geçti. `make vet` bütün paketleri denetledi ve 0 ile çıktı. [Son günlük](go-followup-test-vet.log), [sonuç](go-followup-result.json). Değişmeyen paketlerin kanıtı ilk çalıştırmadan korunur; bu ikinci bir bütün paketler test çalıştırması değildir. |
| Uzak regresyon ve yarış kontrolleri | `TestRemoteDNS\|TestSetupDNS`, [odaklı çalıştırmada](go-focused.log) ve ardından `-race` ile geçti (87.499 sn, [günlük](go-focused-race.log)). Son yerel HTTPS protokolü ve geçersiz kayıt HTTP 400 regresyonları `-race` ile geçti (9.128 sn, [günlük](go-final-race.log)). |
| Certbot süreci ve korunan dizin sahipliği | Seçilen Certbot, panel/posta sertifikası, gözetmen ve kalıcı çağrı zinciri regresyonları `cmd/agent` için `-race` ile geçti (9.947 sn, [kaydedilen araç çıktısı](go-certbot-race.log)). Seçim `internal/hostcmd` paketini yalnız derledi; bu paketin koruma testleri yukarıdaki son tam paket çalıştırmasında yürütüldü. |
| DNS test düzeneği bağlantısı | Sabit çözümleyici bağlantısı ve isteğe bağlı DNS profil düzeneği derlendi; odaklı yarış seçimi geçti ([günlük](go-dns-fixture-race.log)). Normal üretim çözümleyici varsayılanları korunur; bu sonuç tam DNS profilinin VM üzerinde çalıştığını kanıtlamaz. |

İlk kaynak arşivi 1.507 dosya içeriyordu; SHA-256 değeri
`324b7b518e8b6427c2690e1c350192e58d905eee0c08f1657428abc29c675e67`.
Son arşiv 1.515 dosya içeriyordu; SHA-256 değeri
`81727ee46dda96c7b5051dc40a4dfb08fc3f2cabdfc07abef17fc9b7fba30ce7`.
[İlk üstveri](source-snapshot-initial.json),
[ilk dosya bildirimi](source-manifest-initial.json),
[son üstveri](source-snapshot.json), [son dosya bildirimi](source-manifest.json)
ve [değişen 15 kaynak yolu](source-changes.json) tam sınırı korur. Kontroller ayrı
temiz Linux kopyalarında, Go 1.26.5, UID 0 ve temizlenmiş Go ortam ayarlarıyla
çalıştı. Kaynak arşivi yerel çıktı olarak tutulur; bu özetler geliştirme kanıtıdır,
imzalı sürüm bildirimleri değildir.

Bağımsız inceleme regresyonları artık ilgisiz TXT kayıtlarının korunmasını,
süresi dolmuş ve yanıtı kaybolmuş bekleyen iptalleri, simetrik yerel/uzak ad alanı
sahipliğini, aynı bağlantıyla silip yeniden oluşturmanın nesil geçmişini, eski
silmenin reddini ve yeni işler engelliyken tam bekleyen V3 yayınının kurtarılmasını
kapsıyor. İptal için kimliği doğrulanmış tam makbuz gerekir: genel HTTP 401 hiçbir
yetki verilmediğini kanıtlamaz. Çözülemeyen girişim kurtarma için korunurken ayrıca
onaylanmış yeni eşleştirme sürdürülebilir.

`TestRemoteDNSHTTPSProtocolEnrollPublishAndRevoke`, gerçek yerel TLS dinleyicisi,
makine kimlik doğrulaması ve SQL yayın/iptalini; yalnız teste özgü güven kökleri,
adres yönlendirmesi ve otorite/yayın düzenekleriyle kullanır. Tarayıcı API
düzeneğinden daha kapsamlıdır; genel DNS hizmetini veya gerçek sunucu
eşleştirmesini kanıtlamaz.

Certbot düzeltmesi, root agent'ın çağırdığı yalnız gerçek Certbot alt sürecine
UID/GID 0 atar; süreç gözetimi ve üst süreç ölümü davranışı korunur. Açıkça
başlatılan yönetilen yeniden sertifika alımı, yalnız doğrulanan sabit dizinleri
ve seçilmiş sertifika ailesinin dizinlerini bilinen eski gruptan düzeltebilir.
Sembolik bağlantı izlemez, özyinelemeli işlem yapmaz; eski sertifika/anahtar
dosyalarını veya ilgisiz sertifika ailelerini benimsemez. Korumalı sertifika
okuyucu değişmedi. Süreç ve dizin testleri, genel ACME alımını veya zamanlayıcıyla
yenilemeyi kanıtlamaz.

## Sonraki DNS/posta kurtarma doğrulaması

Sonraki ortak kaynak arşivi 1.519 dosya içerir; SHA-256 değeri
`31b1185c9699c131f51b026eeaef4ff81d52943f64e53b008f3563f59df9fa18`.
Yukarıdaki 1.515 dosyalı Certbot sonrası görüntüden
[15 yolda](source-changes-recovery.json) farklıdır: yedi Go dosyası, dört arayüz/test
dosyası ve dört belge. [Görüntü üstverisi](source-snapshot-recovery.json) ve
[dosya bildirimi](source-manifest-recovery.json) tam sınırı korur.

| Kontrol | Sonuç ve tam kapsam |
|---|---|
| Etkilenen paketler ve tam vet | Başka bir temiz Linux kopyasında bütün `cmd/panel` testleri (122.726 sn), bütün `cmd/agent` testleri (11.809 sn) ve bütün paketler için vet geçti; çıkış 0. Veritabanı ve host-command paketleri önceki başarılı çalıştırmaya göre değişmedi. [Günlük](go-recovery-test-vet.log), [sonuç](go-recovery-result.json). |
| DNS alt işlem kurtarması | `TestSetupDNS\|TestServerSetupDNSUnproven`, `-race` ile geçti (28.819 sn, [günlük](go-dns-recovery-race.log)). Ayrı dış işlem kabul kilidi testi `-race` ile geçti (3.468 sn, [günlük](go-dns-outer-race.log)). |
| Genel posta DNS kimliği | `TestMailDNS\|TestCertbot\|TestMailHost\|TestProtectedBuildCommit`, `-race` ile geçti (4.506 sn, [kaydedilen araç çıktısı](go-mail-dns-race.log)). |
| Son arayüz farkı | Kurulum çalışma zamanı testlerinin 17'si de geçti ([günlük](web-reconciliation-tests.log)); üretim derlemesi ve paket bütçesi geçti ([günlük](web-reconciliation-build.log)). [Arayüz özetleri](ui-reconciliation-sha256.json), yerel DNS sıralaması ve nötr işlem sonucu doğrulaması değişikliklerini tanımlar. Önceki tam test/tarayıcı çalıştırmaları özgün kapsamını korur. |

DNS kurtarma testleri, başarısız planın yeniden kabulünden önce tam son alt işlem
makbuzunu ve kanıtlanmış sunucu geri alımını gerektirir. Sonucu bilinmeyen veya
uygulanmış alt işlem, dış işlemi çalışır ve taslağı kilitli tutar. DNS sonucu
kaydedilmiş ancak panel durumu yazılamamışsa motor yeniden kurulmadan devam
edilir. Böylece kayıp yanıt, çakışan yeni plana izin vermez.

Posta sorgusu, sabit genel çözümleyicilere sınırlı TCP sorgularıyla PTR ve A/AAAA
kayıtlarını doğrudan okur; yerel FQDN'in `/etc/hosts` kaydı genel DNS gerçeklerinin
yerine geçemez. İşlem kimliği, soru ve son kayıt sahibi denetlenir; sınırlı CNAME
zinciri sınıfsız ters DNS delegasyonunu desteklerken döngüleri ve çakışan takma
adları reddeder. Testler gerçek yerel DNS dinleyicisini ve gerçek işletim sistemi
localhost kaydını kullanır; farklı harf büyüklükleri, IPv6, delege ters adlar,
ilgisiz kayıtlar ve yanlış adresler kapsanır. Bu, sorgu işleyişini kanıtlar;
İnternet üzerinden posta teslimini kanıtlamaz.

`server_setup_reconciling` sürerken arayüz nötr ilerleme bildirir: önceki sonuç
doğrulanıyor ve kurulum devam edecek. İşlem kimliği korunur; çakışan başlatma/plan
düzenleme işlemi sunulmaz ve bu durumu göstermek için değişiklik isteği
gönderilmez. Sonlandırılmış hata hâlâ uyarı olarak gösterilir.

## Son BIND ve ortak DNS kaynak doğrulaması

Son test edilen kaynak arşivi 1.529 dosya içerir; SHA-256 değeri
`a08aa34402fc576572532ca17003939c4ca7ee6a160e906dd87d8b1cc910a115`.
[Dosya bildirimi](source-manifest-bind.json) ve [üstverisi](source-snapshot-bind.json)
önceki görüntülerden ayrı saklanır. `31b1185c…` görüntüsüne göre
[değişen 24 yol](source-changes-bind.json), 18 Go kaynak/test dosyası ve altı
belgedir; arayüz kaynağı değişmedi.

| Kontrol | Sonuç ve tam kapsam |
|---|---|
| Son temiz etkilenen paketler | `cmd/panel` (112.673 sn), `cmd/agent` (11.566 sn), `internal/binddns` (0.054 sn) ve `internal/dnswire` (0.005 sn) testlerinin tamamı geçti. Bütün paketlerde vet 0 ile çıktı. [Günlük](go-bind-test-vet.log), [sonuç](go-bind-result.json). |
| BIND secondary regresyonu | Tam `internal/binddns` yarış çalıştırması geçti (1.164 sn, [günlük](go-bind-secondary-race.log)). Sunucu options/kurtarma değişikliği, sonraki ortak DNS çıkarımından önce tam agent, BIND ve host-command testlerini ve hedefli vet'i de geçti ([kaydedilen çıktı](go-bind-host-tests.log)); önceki çalıştırmanın kapsamı korunur. |
| Ortak genel DNS regresyonu | Son seçilmiş doğrudan DNS/posta testleri `-race` ile geçti: panel 3.514 sn, agent 1.193 sn, dnswire 1.018 sn. Seçili paketlerde ve host commands için vet geçti. [Kaydedilen çıktı ve seçim](go-public-dns-wire-race.log). |

Düzeltilen BIND secondary, kataloğu açık bir secondary bölgesi olarak tanımlar;
aboneliği sunucunun sahipliği belirli options bloğuna yerleştirir. İşaretler
dışındaki operatör baytları korunur; yabancı katalog politikası, dört direktifle
sınırlı devralma akışında da reddedilir. Çalışma zamanı ve tamamlanmış hedefin
kurtarması aynı politikayı doğrulanmış makbuzdan türetir.

Makbuzdaki `SecondaryConfigVersion=1`, düzeltilmiş secondary için farklı değişmez
nesil baytları ve kimlik üretir. Eski sıfır sürümlü makbuzlar kanıt olarak
okunabilir; güncel hazırlık için yeterli değildir. Geri alım, kalıcı günlüğünden
yalnız tam güncel veya tarihsel hedef nesli yeniden oluşturur; değiştirilebilir
ayarlardan farklı politika seçemez veya düzenlenmiş baytları benimseyemez.

Ortak `dnswire` sorgusu artık hem posta kimliğini hem ilk ad sunucusu hazırlığını
doğrular. Ad sunucusu hazırlığı başarılı A ve AAAA yanıtlarını gerektirir;
geçerli AAAA NODATA kabul edilir fakat AAAA zaman aşımı veya SERVFAIL, başarılı
A sonucuyla gizlenemez. Üretimde hedefler sabit, doğrudan genel IP uçlarıdır;
testlerde gerçek yerel DNS/TCP dinleyicileri kullanılır. Hosts/NSS katılmaz.
Gerçek ayrıştırıcı ve eşlenmiş DNS VM sonuçlarının ayrı kayıtları vardır;
bu paket testlerinden çıkarılamazlar.

[Gerçek profil kaydı](../server-setup-profiles-20260911/README.tr.md), üç barındırma profilini ve son iki VM’li DNS profilini; tam adayları, başarısız girişimleri ve salt-okur devamıyla ayrı kaydeder. Yukarıdaki kaynak/test sınırlarını değiştirmez.

Bu sonuçlar genel ACME alma/yenilemeyi, İnternet üzerinden posta teslimini, bütün
amaç profillerini veya gerçek sunucu eşleştirmesini kanıtlamaz. Hiçbir kurulu panel
güncellenmedi. Gerçek güvenlik duvarı/yeniden başlatma ve test CA posta yaşam
döngüsü sonuçları [ayrı güvenlik duvarı kaydında](../server-setup-firewall-20260911/README.tr.md)
ve [posta kaydında](../server-setup-mail-20260911/README.tr.md) tutulur.
