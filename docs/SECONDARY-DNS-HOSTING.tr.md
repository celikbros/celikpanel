# İkincil DNS sunucusunda barındırma

Uygulama kaydı, 12 Eylül 2026 · [English](SECONDARY-DNS-HOSTING.md)

Bu belge çalışma ağacındaki uygulamayı anlatır; sürüm yayımlama veya kurulu
sunucu güncelleme kaydı değildir.

## Desteklenen yapı

Bir CelikPanel sunucusu aynı anda ikincil yetkili DNS hizmeti verebilir ve web
siteleri, uygulamalar, e-posta barındırabilir. Yerel DNS rolü ile barındırma
kayıtlarının düzenlendiği yer birbirinden ayrıdır.

İstenen iki sunuculu yapı:

| Sunucu | Yerel DNS | Barındırma kayıtlarının yayımlanması |
| --- | --- | --- |
| Frankfurt | BIND birincil, `ns1.celikhost.com`, `72.62.38.15` | Kendi bölgelerini yönetir; Boston'dan yetkisi sınırlandırılmış kayıt değişikliklerini kabul eder |
| Boston | PowerDNS ikincil, `ns2.celikhost.com`, `2.25.80.4` | Barındırdığı alan adlarının kayıt değişikliklerini yetkilendirilmiş Frankfurt paneline gönderir |

Boston'un siteleri ve e-postası Boston'da kalır; ilgili kayıtlar Boston'a
yönelir. Frankfurt birincil DNS bölgesini tutar; Boston mevcut eşleştirme ve
DNS aktarım sistemiyle ikincil kopyayı alır. İkincil DNS, site dosyalarını,
veritabanlarını veya posta kutularını çoğaltmaz.

Yayın bağlantısı yalnızca kendisine ait bölgeleri yönetebilir. Mevcut alan
adlarının kayıtlı DNS sahipliği korunur; bu seçim birincildeki mevcut bölgeleri
içe aktarmaz veya devralmaz.

Bu uygulamada çiftin tek bir yayın birincili vardır. Bölgeye göre karşılıklı
birincil/ikincil roller, birden fazla birincil veya genel DNS kümeleri eklenmez;
bunlar motorun topoloji ve sahiplik modelinde ayrı bir çalışma gerektirir.
Desteklenen yerel motorlar BIND ve PowerDNS'tir.

## Tek sihirbaz akışı

1. Barındırma amacını veya özel bileşenleri seçin. **Erişim ve DNS** adımında
   **Bu sunucu**, **İkincil**, yerel motor ve eşleşen ad sunucusu/IP bilgilerini
   girin. Birincilin HTTPS panel adresini belirtin.
2. Yayın bağlantısı dahil tüm planı inceleyin. Sihirbazı açmak veya planı
   incelemek bağlantı oluşturmaz ve hizmet başlatmaz.
3. Önce birincilde, ardından ikincilde incelenmiş kurulumu başlatın. Eşleşmeyi
   beklemeden önce yerel DNS, gerekli erişim bileşenleri, güvenlik duvarı ve
   güvenilir panel HTTPS'i hazırlanır. Nginx seçildiyse sertifikadan önce
   hazırlanır; sonradan kurulup bağımsız sertifika yenilemesini bozmaz.
4. İkincil, **Birincil DNS sunucusuna bağlan** adımında bekler. Birincilde
   **Ayarlar → DNS altyapısı** bölümünden DNS yayın bağlantısı için kayıt kodu
   oluşturun ve ikincilin sihirbazına girin. Bekleme sırasında Ayarlar'a ve
   Erişim ve kurtarma bağlantılarına ulaşılabilir.
5. Doğrulama sonrasında **Bu bağlantıyı kullan ve kuruluma devam et** seçeneğini
   kullanın. Aynı kayıtlı kurulum, barındırma ve e-posta adımlarını sürdürür;
   ikinci bir sihirbaz veya elle DNS varsayılanı değişikliği gerekmez.
6. Tamamlanma; yerel ikincil, uzaktan yayın, hizmetler, güvenlik duvarı,
   sertifikalar ve yenilemenin güncel doğrulamasına bağlıdır. Eksik dış
   gereksinimler bekleme nedeni olarak gösterilir.

Birincilin HTTPS adresi güvenilir olmalı; genel DNS kayıtları ve ad sunucuları
doğru çözülmelidir. E-posta kimliği, sertifikaları, PTR ve alan adı e-posta
kayıtları ayrıca doğrulanır. E-posta eklemek işletim sistemi adını e-posta
adresine dönüştürmez.

## Kalıcı yetkilendirme ve kurtarma

Taslak, incelenen `dns_publisher_endpoint` adresini saklar. Yerel ikincil DNS
ile barındırma DNS kayıtları yayımlayan hizmetler birlikte seçilince plana
`dns_publisher` adımı eklenir. Birincil barındırma planı da güvenli erişimi
hazırladıktan sonra `dns_readiness` adımında bekleyebilir.

`POST /api/v1/setup/publisher`, bağlantıyı belirli bir kurulum çalıştırmasına
ve plana bağlar; yönetici yetkisi ve geçerli kurulum erişimi gerektirir. Güncel
kanıt; incelenen adres, sıralı ad sunucuları ve iki sunucunun IP'leriyle
eşleşmelidir. Yerel ikincilin hazır olması bağımsız olarak denetlenir.

Değiştirilemez ve gizli bilgi içermeyen bağlama kaydı, çalıştırıcının JSON
durumundan ayrı tutulur. Aynı onayın tekrarı aynı işlemi sürdürür; başka bir
bağlantı kaydı değiştiremez. HTTP yanıtı kaybolursa mevcut işlemle durum
uzlaştırılır; yeni kurulum veya yeni yetkilendirme başlatılmaz. Plan
değiştirildiğinde eski çalıştırıcının geç gelen yanıtı DNS varsayılanlarını
değiştiremez.

Yeni barındırma alan adlarının uzaktan yayın varsayılanı ancak bu denetimden
sonra kaydedilir. Tamamlanmış yerel DNS adımı yeniden başlatmada tekrar
çalıştırılmaz. Son doğrulama bağlı yayın birincilini ve yerel ikincili denetler.
Eş kimliği bilgisi protokolde isteğe bağlıdır; eski istemcilerin yanıt yapısı
korunur.

DNS bekleme adımlarında **Planı düzenle**, önceki adımlar tamamlanmış, sonraki
adımlar başlamamış ve sonucu belirsiz hizmet/ajan/DNS alt işlemi kalmamışsa
kullanılabilir. Tamamlanan sunucu değişiklikleri korunur ve yeni planda
değerlendirilir; kurtarma bunları kendiliğinden kaldırmaz.

## Doğrulama ve yayımlama sınırı

Kaynak testleri, gerçek iki sanal makinedeki DNS sonuçları ve sınırları için
[ikincil DNS ile barındırma kabul kaydına](validation/secondary-hosting-20260912/README.md)
bakın. Kontrollü DNS/ACME ortamı, genel DNS delegasyonunu, genel sertifika
otoritesinden sertifika alınmasını veya İnternet üzerinden e-posta teslimini
kanıtlamaz.

Kurulu panelleri yalnızca kullanıcı, CelikPanel'in güncelleme arayüzünden
günceller. Kaynak uygulaması, yerel sanal makine doğrulaması veya sürüm
yayımlama izni; Frankfurt, Boston ya da başka bir kurulu örneği güncelleme
izni değildir.
