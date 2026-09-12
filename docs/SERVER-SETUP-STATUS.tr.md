# Sunucu kurulumu uygulama durumu

*Çalışma ağacı kaydı: 11 Eylül 2026 · [English](SERVER-SETUP-STATUS.md)*

**Onaylanan kaynak uygulaması ve yerel doğrulama tamamlandı. Bu belge sürüm yayımlama kanıtı değildir.**
[Onaylanan kurulum planı](SERVER-SETUP-PLAN.tr.md) kabul sözleşmesi olarak geçerlidir.
Burada anlatılan değişiklikler çalışma ağacındadır; bu kayıt bunların yayımlandığını
veya Boston, Frankfurt ya da başka bir sunucuya kurulduğunu söylemez.
Eski dağıtım talimatlarından bağımsız olarak [AGENTS.md](../AGENTS.md) gereğince
kurulu panel güncellemelerini kullanıcı CelikPanel'in kendi arayüzünden başlatır.

## 12 Eylül 2026 eklemesi: ikincil DNS ile barındırma

[İkincil DNS sunucusunda barındırma](SECONDARY-DNS-HOSTING.tr.md) kaydı,
Frankfurt'un birincil, Boston'un hem ikincil DNS hem web/uygulama/e-posta
sunucusu olduğu çalışma ağacı uygulamasını açıklar. Boston'un barındırma
kayıtları, tek sihirbaz içinde açıkça bağlanan yetkili Frankfurt bağlantısı
üzerinden yayımlanır. Bölgeye göre karşılıklı birincil/ikincil roller eklenmez.
Bu ek kayıt da bir sürüm yayımlama veya kurulu sunucu güncelleme kanıtı değildir;
kurulu panelleri kullanıcı kendi panel arayüzünden günceller.

Aşağıdaki 11 Eylül uygulama ve doğrulama kayıtları tarihsel kapsamını korur.

## Çalışma ağacında uygulananlar

| Alan | Geçerli davranış ve sınırı |
|---|---|
| Giriş ve geçmiş | Yeni olduğu doğrulanmış kurulumda yönetici `/setup` sayfasına gider; yarım kurulum sürdürülür. Eski kurulumların Dashboard erişimi korunur ve isteğe bağlı kurulum girişi sunulur. Tamamlanan kurulum, aktivasyon veya girişle yeniden başlamaz. Kiracı rolleri kurulum API'lerini çağırmaz. |
| İnceleme ve yürütme | Tarayıcıdan art arda kurulum çağrıları yerine revizyon kontrollü taslaklar, değişmez incelenmiş planlar, açık onay ve kalıcı sunucu işlemleri kullanılır. İstek ve alt işlem kimlikleri kayıp yanıtların uzlaştırılmasını ve yeniden başlatmayı destekler. Bekleyen plan ancak değişiklik yapan adımları bittikten sonra düzenlenebilir. |
| Amaç profilleri | Web, web ve posta, uygulama ve DNS planları desteklenen mevcut işlemleri kullanır. Web Nginx, PHP-FPM ve MariaDB kullanır; posta mevcut webmail/koruma profillerini ve posta TLS'ini ekler; uygulama resmi Node.js LTS sürümü ve isteğe bağlı veritabanı seçtirir. Web, uygulama ve web+posta ayrı geçici Debian 13 VM’lerinde; DNS profili ayrıca iki VM’li yetkili DNS çiftinde tamamlandı. |
| Yerel DNS | İlk kurulum, yönetilen motor/topoloji işlemlerini kullanır ve mevcut sahipliği korur. Primary hazırlığı ile secondary aktarım hazırlığı ayrı kanıtlarla değerlendirilir; secondary yayıncı yetkisi kazanmaz. Bağımsız authoritative yedekliliği zorunlu kalır. |
| Harici DNS | Kalıcı yönetim modu ve alan adı sahipliği, yerel authoritative motor olmadan web barındırmayı sağlar. Alan adı ve posta ekranları sağlayıcıya girilecek kayıtları ve canlı kontrolleri gösterir; haricen yönetilen bölgelerde yerel DNS değiştirme kontrolleri kullanılamaz. Mevcut alan adlarının sahipliği korunur. |
| Genel erişim | Tamamlanma için panel FQDN'i, güvenilir dinleyici TLS'i, yenileme kanıtı, sağlıklı bağımlılıklar ve güvenlik duvarı hazırlığı gerekir. Posta; sunucu kimliği, TLS ve teslimat kontrolleri ekler. Girişte kullanılan kendinden imzalı sertifika tamamlanma kanıtı değildir. Üç barındırma profili, yalıtılmış test ACME otoriteleriyle gerçek HTTP-01 sertifika alımı ve zamanlayıcı yenilemesini geçti. Genel CA üretimi henüz kanıtlanmadı. |
| Mevcut işlerin korunması | Kurulumun servis ve panel sertifikası alt işlemleri, yeniden başlatmadan sonra da incelenmiş erişim portlarını korur. İncelenmemiş yeni hizmet portları yeniden inceleme gerektirir. Lisans kaybı yeni kurulum adımlarının kabulünü duraklatır; kaydedilmiş alt işlemler uzlaştırılır ve mevcut işler çalışmayı sürdürür. |

Arayüz Türkçe/İngilizce adımlar, plan inceleme, ilerleme, uygulanabilir hatalar ve
amaca uygun sonraki işlemi sunar. Kurulum durumunu okumak yazılım kurmaz veya
yapılandırmayı değiştirmez. API hazırlık kontrolleri arayüzden bağımsızdır.

İncelenmiş eşli DNS kimliği şu anda yalnız eşin IPv4 adresini kaydeder. İlk
kurulum, eş ad sunucusu için yayımlanan IPv6 adresini henüz doğrulayamaz ve
`dns_peer_ipv6_unverified` bildirir. Bu bir doğrulama sınırıdır; eşin AAAA
kaydının yanlış olduğunu göstermez. Desteklenen kurulum seçenekleri yalnız IPv4
ile ad sunucusu yayını veya harici DNS'tir. Kurulum mevcut AAAA kayıtlarını
otomatik kaldırmaz.

Root agent artık gerçek Certbot alt sürecini UID/GID 0 ile başlatır; gözetim
korunur. Açıkça başlatılan yönetilen yeniden sertifika alımı, yalnız doğrulanan
sabit dizinleri ve seçili sertifika ailesinin dizinlerini bilinen eski gruptan
düzeltir; özyinelemeli işlem yapmaz, sembolik bağlantı izlemez veya eski
sertifika/anahtar dosyalarını benimsemez. Sıkı korumalı okuyucu değişmedi. Süreç
düzeyindeki regresyon bu düzeltmeyi doğrular; tam gerçek profil ve genel ACME
sonuçları ayrı kanıtlardır.

## Mevcut uzak DNS: uygulandı ve yerelde doğrulandı

Kullanıcı 11 Eylül 2026'da bağlantı bileşenini ve yerel testleri açıkça
yetkilendirdi; [onay kaydına](REMOTE-DNS-AUTHORIZATION.md) bakın. Önceki otomatik
inceleme retleri geçmiş kayıtlardır; bekleyen izin isteği değildir.

Çalışma ağacında yönetici eşleştirmesi, kayıtlı bağlantılar, yetki iptali,
kimliği doğrulanmış alıcı durumu/yayını, değişmez alan adı–bağlantı ilişkileri ve
kalıcı yayın nesilleri bulunuyor. Kurulum seçeneği ve DNS ayarlarındaki kontroller
kaynakta etkindir. Kayıtlı bağlantının hazır görünmesi eşleştirildiği anlamına
gelir; sihirbaz güncel, kimliği doğrulanmış otorite kanıtı ister ve kurulumdan önce
seçilen uç noktayı ve ad sunucularını inceletir. Sayfaları okumak sunucu
eşleştirmez, kimlik bilgisi oluşturmaz veya kayıt yayımlamaz. Kodlar bileşen
belleğinde ve istek gövdesinde tutulur; URL'ye ya da tarayıcının kalıcı depolamasına
yazılmaz. Posta işlemleri gerekli teslimat kayıtlarını gösterir ve mevcut posta
yönlendirmesini sessizce değiştirmek yerine çakışmayı bildirir.

[Uzak DNS güvenlik kapsamı](REMOTE-DNS-SECURITY.tr.md) kabul sözleşmesi olarak
geçerlidir. Bağımsız inceleme; ilgisiz TXT kayıtlarının korunmasını, süresi dolmuş
bekleyen eşleştirmenin iptalini ve yerel/uzak DNS ad alanlarının simetrik
korunmasını zorunlu regresyon senaryoları olarak belirledi. Bu düzeltmeler,
kalıcı nesil kurtarması ve kimliği doğrulanmış yerel TLS yayın/iptali odaklı
regresyon ve yarış kontrollerinden geçti. Çözülemeyen iptal, kaydedilmiş kimliği
korur; genel HTTP 401 hiçbir yetki verilmediğini kanıtlamaz. Bu durum kaydı üretim
eşleştirmesi, yayımlanmış özellik veya uzak DNS kullanan gerçek sunucu profili
kanıtı değildir. Aşağıdaki barındırma profilleri kontrollü harici DNS kullanır.

## Doğrulama kanıtları ve kapsam sınırları

| Kontrol | Bu kayıttaki kanıt |
|---|---|
| Tam arayüz testleri ve derleme | Uzak DNS arayüzünün etkinleştirilmesi ve güncel ad sunucusu gösteriminin düzeltilmesinden sonraki tam çalıştırmada 352/352 test geçti. Üretim derlemesi ve paket boyutu denetimi geçti. |
| Odaklı arayüz kontrolleri | Kurulum, harici DNS ve uzak DNS odaklı çalıştırmasında 29/29 çalışma zamanı testi geçti. Açık eşleştirme/iptal, kiracı yalıtımı, kayıp yanıtın aynı bekleyen kimlikle uzlaştırılması, kimlik bilgisi işleme, güncel kanıt, incelenen bağlantı kimliği, korunan kayıt çakışma mesajları ve pano hatası kapsanıyor. Güncel ad sunucusu gösterimi ve eski bekleyen kimlik korunurken ayrı onaylı yeni eşleştirme her iki son çalıştırmada da bulunuyor. |
| Sonraki yerel DNS yönlendirmesi | Rol formu birincil DNS kurulumu → ikincil kurulum → son eşli doğrulama sırasını açıklar. Bu sınırlı metin düzeltmesinden sonra 16 kurulum çalışma zamanı testinin tamamı, üretim derlemesi ve paket bütçesi geçti. Önceki 352/29 çalıştırmaları özgün kaynak kapsamını korur. |
| Tarayıcı yönlendirmesi | Uzak DNS arayüzü etkinleştirilmeden önceki son temel kurulum Chrome çalıştırmasındaki 10 senaryo geçti: yeni kurulumda TR/EN masaüstü/mobil genişlikleri, eski kurulum, hazır, bekleyen, erişilemiyor, müşteri ve lisans kilidi. Çalışma zamanı konsol hatası veya yatay taşma yoktu; ilk giriş yazma yapmadı, müşteri/lisanssız kullanıcı kurulum GET çağrısı yapmadı. Kontrollü test verileri gerçek sunucuda profil çalıştırmasını kanıtlamaz. |
| Uzak DNS tarayıcı kontrolü | Güncel ad sunucusu düzeltmesinden sonraki son Chrome tekrarında, Türkçe/İngilizce ve 1440/390 pikselde dört eşleştirmeden kuruluma geçiş senaryosu geçti. Her birinde açık onaylı bir eşleştirme ve bir kurulum başlatma isteği vardı; açılışta yazma, çalışma zamanı hatası veya yatay taşma yoktu. Bunlar kontrollü API düzenekleridir. |
| Go testleri ve vet | Uzak bağlantı bütünleşmiş tam çalıştırmada migration 042'nin CRLF değişmezi dışında bütün paketler geçti; hata kaydı korunuyor. Certbot sonrası `81727ee4` görüntüsünde panel, veritabanı, agent ve host-command testlerinin tamamı ve bütün paketlerde vet geçti. Sonraki DNS/posta kurtarma `31b1185c` görüntüsünde değişen panel/agent paketlerinin bütün testleri ve bütün paketlerde vet geçti. Değişmeyen paketlerin önceki kanıtı korunur. [Uzak kabul kaydı](validation/server-setup-remote-dns-20260911/README.tr.md) tam görüntüleri, sonuçları ve farkları saklar. |
| Odaklı yarış durumu kontrolleri | Uzak bağlantı bütünleşmesinden önce panel, agent ve veritabanında `TestServerSetup\|TestSetup\|TestMailHostCertificate\|TestSQLite` seçimiyle ve bağımsız `TestSetupDNS` çalıştırmasında `-race` geçti. Son posta yaşam döngüsü/sözleşme yarış testleri ve değiştirilmeden önce/sonra çalıştırılan başarısız niyet kaydı regresyonu geçti. Bunlar odaklı kontrollerdir; bütün testlerin race çalıştırması değildir. |
| Sonraki DNS/posta kurtarması | DNS planı yeniden açılmadan önce tam başarısız alt işlem/geri alım kanıtı gerekir; sonucu belirsiz alt işlem taslağı kilitli tutar. Posta DNS kanıtı yerel hosts kayıtlarını kullanmaz ve sınırlı ters CNAME zincirini destekler. DNS kurtarma race (28.819 sn ve 3.468 sn), posta DNS race (4.506 sn), son 17 kurulum çalışma zamanı testi ve derleme/bütçe geçti. Süren uzlaştırma nötr ilerleme gösterir; çakışan plan işlemi sunulmaz. |
| Uzak DNS ve Certbot yarış kontrolleri | Uzak/kurulum regresyon yarış kontrolü geçti (87.499 sn); son gerçek yerel HTTPS/geçersiz kayıt testleri geçti (9.128 sn). Certbot süreç kimliği, korunan dizin sahipliği ve ilgili sertifika/gözetmen regresyonları geçti (9.947 sn). Bunlar seçilmiş yarış kontrolleridir; bütün testlerin race çalıştırması değildir. |
| Son BIND/genel DNS kaynağı | `a08aa344` görüntüsünde panel, agent, BIND ve ortak DNS paketlerinin bütün testleri ve bütün paketlerde vet geçti. Düzeltilmiş secondary nesil kimlikleri, tam geri alım için tarihsel makbuzları korur; sahip olunan katalog politikası makbuzdan doğrulanır. Ad sunucusu kanıtı artık hosts/NSS kullanmaz ve eksiksiz A+AAAA yanıtları ister. Seçilmiş ortak DNS ve tam BIND yarış kontrolleri de geçti. |
| Gerçek VM | Yalıtılmış Debian 13, Ubuntu 24.04 ve Arch test VM’lerinde geçti: kalıcılık açıkken gerçek yeniden başlatma, SSH ile yeniden bağlantı, kaydedilmiş nftables görüntüsünün tam geri yüklenmesi, kapatmanın görüntüyü kaldırıp geri yükleme birimini devre dışı bırakması ve tekrar isteğin aynı alt işlemi uzlaştırıp kopya oluşturmaması. |
| Test CA ile gerçek posta TLS | Yalıtılmış Debian 13 VM’inde üretim aktarım ve kuyruklu yenileme kodu, tam yenilenmiş güvenilir sertifikayı gerçek Postfix STARTTLS ve Dovecot IMAPS üzerinden sundu. Yedek sertifika baytları değişmedi; aynı hook tekrarında ek değişiklik yapılmadı. Son işlem makbuzu korumalı geçerli nesille eşleşti. Panel veya lisans yöneticisi katılmadı. |
| Gerçek barındırma profilleri ve zamanlayıcı yenilemesi | Ayrı Debian 13 VM’lerinde web, uygulama ve web+posta kurulumu güncel kontrollerle tamamlandı. Üçü de yalıtılmış test ACME otoriteleriyle kurulu Certbot zamanlayıcısı ve üretim kancaları üzerinden yenilendi. Tam işlem/alt işlem kimlikleri ve kurulum tamamlanma durumu değişmedi. Posta, doğrudan DNS düzeltmesinden sonra aynı bekleyen işlemde devam etti; kurulum veya sertifika alımı tekrarlanmadı. |
| Lisans kaybı ve mevcut işler | Web VM’inde imzalı süresi dolmuş lisans yanıtıyla yönetim, yenilemeden önce ve sonra `403 license_required` döndürdü; Nginx, PHP ve MariaDB çalıştı. Sertifika yenilemesi lisans kısıtlaması altında sürdü. |
| Gerçek eşlenmiş DNS profili | Aynı son `a08aa344` adayıyla iki DNS sihirbazı işlemi de beş güncel hazırlık kontrolü ve tam tekrar-başlatma kimliğiyle başarılı oldu. Mevcut üye alan adı iki sunucudan aynı yetkili SOA ve A yanıtını verdi. İlk oluşturma sonrası istemci doğrulaması yanlış JSON anahtarı kullandı; hata korunuyor. Aynı alan adının salt-okur doğrulaması, yeniden oluşturma veya kaynak değişikliği olmadan geçti. |
| Genel altyapı sınırları | Genel CA üretimi, internet posta teslimatı, genel ad sunucusu delegasyonu ve üretim uzak eşleştirmesi henüz kanıtlanmadı. Tamamlanan gerçek düzenekler kontrollü DNS/SMTP ve yalıtılmış ACME güven kökleri kullanır; yerel kabul üretime dağıtım anlamına gelmez. |
| Sürüm ve kurulu sunucular | Bu kayıt sürüm yayımlandığını veya kurulu sunucunun güncellendiğini kanıtlamaz. Asistan hiçbir kurulu paneli güncellemedi. |

[Depodaki kaynak ve tarayıcı kabul kaydı](validation/server-setup-checks-20260911/README.tr.md)
tam Go, yarış durumu, arayüz ve Chrome sonuçlarını test edilen kaynak
görüntüsünün özetiyle saklar.

[Depodaki güvenlik duvarı kabul kaydı](validation/server-setup-firewall-20260911/README.tr.md)
üç işletim sisteminin kanıtlarını içerir. Özgün aşama kayıtları ve açılış
kimlikleri test makinesinde
`/var/tmp/cp-server-setup-20260911/{os}/firewall-acceptance.json` yolunda tutuluyor.
[Depodaki posta sertifikası kaydı](validation/server-setup-mail-20260911/README.tr.md)
Debian canlı dinleyici/makbuz kanıtını, son kaynak özetlerini ve test günlüklerini
içerir. İlk başarısız kuyruk sonucunu ve sonraki başarılı devamı korur; düzenek
baştan boş VM üzerinde tekrar çalıştırılmadı. Korunan son başarılı yapılandırma
görüntüsü, bağımsız başarısız niyet kaydı regresyonundan da geçti. Test CA sonucu
genel sertifika alımını, gerçek Certbot zamanlayıcısıyla yenilemeyi, İnternet
üzerinden posta teslimini veya tam bir amaç profilini kanıtlamaz.

[Gerçek barındırma profili ve zamanlayıcı kaydı](validation/server-setup-profiles-20260911/README.tr.md)
üç Debian barındırma tamamlanmasını, gerçek zamanlayıcı yenilemelerini, süresi
dolmuş lisansta mevcut işlerin korunmasını ve son iki VM’li DNS profilini içerir. Üretim işleyicileri açıkça
etkinleştirilen yetkisiz test sunucusunda; sunucu işlemleri gerçek agent üzerinden
çalıştı. Önceki başarısız girişimler, postanın bekleyen sonucu ve aynı işlemle
sonraki kurtarması korunur. DNS/SMTP ve ACME güveni kontrollü test hizmetleriydi.
Bu kanıt genel CA üretimi, genel delegasyon veya uzak DNS eşleştirmesi iddiası
taşımaz.

[Uzak DNS kaynak ve arayüz kabulü](validation/server-setup-remote-dns-20260911/README.tr.md)

Güncel uzak DNS arayüz günlükleri, depo çalışma alanındaki
`.tmp-remote-dns-ui-full-tests.log`, `.tmp-remote-dns-ui-tests.log` ve
`.tmp-remote-dns-ui-build.log` dosyalarıdır. Kalıcı kopyaları, son yerel backend
bütünleşim/yarış sonuçları, temiz kaynak görüntüleri, aralarındaki farklar ve dört
Chrome senaryosu yukarıdaki uzak kabul kaydında tutulur.

Tarayıcı sonuçları ve ekran görüntüleri yerelde
`.tmp-portal-review/server-setup/` altında (`browser-results.json`) tutuluyor.
Bunlar geçici çalışma çıktılarıdır; imzalı veya yayımlanmış sürüm kaydı değildir.

Bu görev, onaylanan kaynak uygulaması ve yerel kabul sınırında tamamlandı.
Yayımlama istenmedi; kurulu panel güncellemelerini kullanıcı başlatır. Sonraki bir
sürüm, kendi tam kaynağıyla ilgili [operasyon kontrollerini](OPERATIONS.tr.md)
karşılamalıdır. Bu geliştirme sonuçları genel altyapı kanıtının yerine geçmez ve
dağıtım yetkisi vermez.

Uygulama kaynakları: [kurulum durumu](../cmd/panel/server_setup.go),
[yürütme](../cmd/panel/server_setup_operations.go),
[hazırlık](../cmd/panel/server_setup_readiness.go),
[DNS sahipliği](../cmd/panel/setup_dns.go),
[kurulum arayüzü](../web/src/components/ServerSetup.tsx),
[uzak DNS arayüzü](../web/src/components/ServerSetupDNSConnections.tsx),
[uzak DNS çalışma zamanı testleri](../web/tests/remote-dns-ui-runtime.test.mjs),
[arayüz çalışma zamanı testleri](../web/tests/server-setup-runtime.test.mjs),
[güvenlik duvarı regresyon testleri](../cmd/panel/server_setup_firewall_test.go) ve
[VM test aracı](../deploy/test-server-setup-firewall-vm.py).
