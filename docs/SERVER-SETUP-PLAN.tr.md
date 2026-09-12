# Yönlendirmeli sunucu kurulumu

*Onaylanan ürün yönü: 10 Eylül 2026 · [English](SERVER-SETUP-PLAN.md)*

**Durum: onaylanan kaynak uygulaması ve yerel doğrulama tamamlandı; henüz yayımlanmadı.**
Geçerli sınır için [uygulama durumu ve kanıtlara](SERVER-SETUP-STATUS.tr.md) bakın.
Kullanıcı aktivasyon, Dashboard ve Components ekranlarını değerlendirdikten sonra
bu yönü onayladı. Bu belge anlaşmayı ve uygulamanın kabul ölçütlerini kaydeder.
Asistana kurulu panel güncelleme yetkisi vermez; [AGENTS.md](../AGENTS.md) gereği
kurulu panel güncellemelerini kullanıcı CelikPanel'in kendi arayüzünden başlatır.

## 11 Eylül ek kararı: açık sihirbaz tercihi (Alpha66 sürüm kaynağı)

Alpha65 sonrasında ilk yönlendirmede **Sihirbazla kur** ve **Kendim
yapılandıracağım** seçenekleri sunulur. Tercih sunucuda saklanır. Manuel tercih
otomatik sihirbazı ve Dashboard davetini kaldırır; **Ayarlar → Sunucu kurulumu**
üzerinden geri dönülebilir. Güncellenen sunucu boş kabul edilmez; tamamlanmış
ve devam eden kurulumlar korunur. Manuel tercih kurulumu tamamlamaz, güvenlik
kontrollerini veya lisans denetimini kaldırmaz. Etkin/bekleyen işlem varken
tercih değiştirilemez. [Uygulama ve doğrulama kaydı](SERVER-SETUP-GUIDANCE.md).

## 1. Sonuç ve hedef kullanıcı

Sunucu yöneticisi bileşen kataloğunu yorumlamadan ve ilgisiz ayar sayfaları arasında
dolaşmadan kullanılabilir, güvenli bir ortama ulaşmalıdır. İlk soru sunucunun
amacıdır. Mevcut CelikPanel görsel dili, Türkçe ve İngilizce, mobil uyumlu yerleşim
ve erişilebilir kontroller korunur. Bayi, müşteri ve ek kullanıcılara sunucu
kurulum kontrolleri verilmez.

Lisans aktivasyonu ile sunucu kurulumu bağımsız durumlardır:

| Gözlenen sunucu durumu | Geçerli lisanstan sonra açılacak ekran |
|---|---|
| Yeni kurulum olduğu doğrulanmış | Kurulum rehberi |
| Kaydedilmiş yarım kurulum | Geçerli adımdan devam |
| Önceden hazırlanmış kurulum | Dashboard |
| Kurulum kaydı bulunmayan mevcut sunucu | Erişimi koru; salt-okur değerlendir ve yeni olduğunu varsaymadan kurulum öner |

Anahtar değişimi, yeniden giriş, yeniden başlatma veya güncelleme kurulumu
sıfırlamamalıdır. Alan adı listesinin boş olması yeni kurulum kanıtı değildir.
Hazır bir sunucuda hazırlık kontrolünün başarısız olması yeni kurulum dayatmamalı
ve mevcut işleri durdurmamalıdır.

## 2. Amaç profilleri

| Seçenek | Hedeflenen sonuç |
|---|---|
| Web hosting — önerilen | Web ve PHP hosting ile gerekli veritabanı altyapısı; varsayılan olarak posta kurulmaz |
| Web ve e-posta hosting | Web hosting, posta kutuları, webmail ve posta koruması |
| Uygulama sunucusu | Seçilen uygulamanın çalışma ortamı, web erişimi ve gerekli bağımlılıkları |
| DNS sunucusu | Yeni yetkili DNS altyapısı veya mevcut yapıya secondary olarak katılım |

Profiller gerçek sunucuda desteklenen, birbiriyle uyumlu bileşenlere çözülür.
Özelleştirme ikincildir; kullanıcı her paketi seçmek veya ilgisiz araçları kurmak
zorunda kalmaz. Bütün yaşam döngüsü uygulanıp doğrulanmadan bir profil çalıştırılabilir
olarak sunulmaz. Profiller müşterinin ilk sitesini veya posta kutusunu değil,
altyapıyı hazırlar. Bunlar altyapı hazır olduktan sonraki adımdır.

## 3. DNS politikası

Hosting kurulumunda DNS yönetim modeli seçilir. Yerel yetkili DNS motoru her
sunucu için zorunlu değildir:

1. **DNS'i burada yönet:** desteklenen yerel motoru, nameserver kimliğini ve gerekli
   topolojiyi yapılandır. Bu modelde üretime hazırlık bağımsız sunucularda yetkili
   DNS yedekliliğini içerir; aynı makinedeki iki ad bunu sağlamaz.
2. **Mevcut CelikPanel DNS altyapısını kullan:** yetkili yayıncı/tüketici ilişkilerini
   kur ve uzak otoriteyi doğrula. Yalnızca eş IP adresi yönetim izni vermez.
   Uzak DNS kullanan web sunucusuna DNS motoru kurmak veya onu secondary yapmak gerekmez.
3. **Harici DNS sağlayıcısını kullan:** eklenecek kayıtları tam olarak göster ve
   doğrula. Elle kayıt yönetimi tasarımın parçasıdır; sağlayıcı API bilgileri
   önkoşul değil, isteğe bağlı otomasyondur.

Altyapı/varsayılan model ilk kurulumda seçilir; her alan adının gerekli kayıtları
o alan adı yayımlanırken doğrulanır. İşletim sisteminin DNS çözümleyicisi, yetkili
DNS hazırlığına kanıt değildir. Secondary aktarılan DNS zone'larını sunar;
siteleri yedeklemez ve yeni yerel alan adlarının yayıncısı sayılmaz.

Bu onaylı yön, D-009'un her durumda yerel DNS gerektiren ürün kuralının yerini
alır. Yerel otoritede motor sahipliği, yayınlama, topoloji, DNSSEC ve kurtarma
kuralları geçerliliğini korur. Kontroller gevşetilmeden açık bir harici DNS modu
uygulanmalıdır; yalnız ön yüzdeki engeli kaldırmak bu özellik değildir.
Desteklenen açık bir taşıma işlemi yapılana kadar mevcut alan adlarının DNS
sahipliği korunur.

## 4. Güvenli erişim ve zorunlu kontroller

İlk uygulama `panel.example.com` gibi bir genel panel FQDN'si, güvenilir sertifika
ve doğrulanmış otomatik yenileme kullanır. Yeni domain satın almak gerekmez.
Başlangıçtaki kendinden imzalı sertifika giriş/kurtarma aracıdır; genel panel
kurulumunun tamamlandığına kanıt değildir. Panel adresi, işletim sistemi hostname'i,
nameserver adları ve posta kimliği ayrıdır; birini almak diğerlerini sessizce
değiştirmemelidir.

IP sertifikaları sonraki bir yol olabilir; mevcut CelikPanel yeteneği değildir.
Yalnız özel ağda çalışan kurulumlar ve güven modeli ayrıca açıkça tasarlanmalıdır;
genel hosting profilinin denetimsiz atlama yolu olmamalıdır.

| Gereklilik | Uygulanacağı sınır |
|---|---|
| Güvenilir panel HTTPS'i ve çalışan yenileme | İlk genel kurulumu tamamlandı saymadan önce |
| Panel/SSH erişimini koruyan ve gerekli servislere izin veren güvenlik duvarı | İlk kurulumu tamamlandı saymadan önce |
| Kurulu, sağlıklı ve uyumlu bağımlılıklar | İlgili özelliği açmadan önce |
| Doğru alan adı DNS'i ve site sertifikası | İlgili HTTPS sitesini yayında saymadan önce |
| Posta kimliği, TLS, gerekli DNS/doğrulama kayıtları ve gönderim önkoşulları | İlgili posta hizmetini/alan adını hazır saymadan önce |

Posta hazırlığı SPF, DKIM ve DMARC içerir; ilgili durumda ters DNS ve bağlantı
kontrolleri yapılır. Sunucu hazırlığı ile alan adı hazırlığı ayrılır. Panelin
yetkilendirilmiş entegrasyonu yoksa harici PTR veya kayıt kuruluşu değişikliği
açık bir kullanıcı görevi olarak kalır.

Kontroller yalnız düğme devre dışı bırakılarak değil, sunucuda uygulanır. Eksik
posta önkoşulları web hosting'i engellemez. Başarısız kontrolde düzeltme/yapılandırma
erişimi açık kalır. Hiçbir kontrol mevcut siteleri, postayı, veritabanlarını,
zamanlanmış işleri veya sertifika yenilemeyi kendiliğinden durdurmaz.

## 5. Kullanıcı akışı ve işlem sözleşmesi

**Amaç → Panel adresi ve DNS yöntemi → Planı incele → Kurulumu başlat → Sonucu doğrula**

- Mevcut durumu sınırlı, salt-okur kontrollerle otomatik oku. Sayfa açıldı diye
  yazılım kurma, ayar değiştirme veya yapılandırma yan etkisi olan tarama başlatma.
- Her adımda tek bir ana eylem sun. Yalnız ilgili girdileri göster. Gelişmiş ayrıntılar
  erişilebilir kalsın; ilk ekran Components kataloğuna dönüşmesin.
- Değişiklikten önce somut bileşenleri, ayar değişikliklerini, erişim kurallarını
  ve bekleyen dış görevleri göster. İncelenen planı kullanıcı başlatır.
- Tam plan kimliği ve bağımlılık sırasıyla kalıcı, sürdürülebilir işlemler kullan.
  Sayfa yenileme, yeniden bağlantı veya panel/agent yeniden başlatma işlemi
  kaybettirmemeli ya da iki kez çalıştırmamalıdır. Çakışan işlemler eşzamanlı yürümez.
  HTTP yanıtının kaybolması başarısızlık kanıtı değildir; tekrar denemeden önce
  kaydedilen işlem uzlaştırılır. Otomatik kurtarma yönetilmeyen yapılandırmaların
  üzerine yazmaz veya mevcut müşteri verisini silmez.
- Mevcut denetlenmiş servis/DNS/sertifika işlemlerini kullan. Kurulum, yetkili
  agent'a yeni bir serbest komut çalıştırma yolu kazandırmaz.
- Gerçek aşamaları, sonuçları ve uygulanabilir hataları göster. Harici DNS yayılımı
  bekleme durumudur; tahmini başarı veya uydurma yüzde sayacı değildir.
- Önemli girdi değişikliği önceki incelemeyi geçersiz kılar. Kurtarma tamamlanan,
  başlamayan ve sonucu belirsiz adımları ayırır; paket kurulumunun her zaman
  tamamen geri alınabileceği vaat edilmez.
- Yalnız güncel kanıtlarla tamamlandı de. Sonunda amaca uygun tek sonraki adımı sun:
  ilk siteyi ekle, uygulama yayımla veya DNS bağla/yönet.

Sonrasında Dashboard seçilen görevi, mevcut kaynakları ve işlem gerektiren
sorunları özetler. Kurulum tamamlandığında yeni sunucu kontrol listesini sürekli
göstermemelidir. Components sonraki yönetim işleri için erişilebilir kalır.

## 6. Uygulama sırası

1. **Kalıcı durum ve yönlendirme:** yeni kurulum kanıtı, korumacı yükseltme davranışı,
   yöneticiye özel kurulum API'leri, sürdürülebilir taslaklar ve bağımsız lisans kapısı.
2. **Hazırlık ve DNS modelleri:** kanıt şeması; yerel/mevcut/harici sahiplik;
   alan adı, yetenek ve posta yollarında D-009 kontrollerinin tutarlı yenilenmesi.
3. **Güvenli erişim akışı:** mevcut işlem altyapısıyla panel adresi, güvenilir
   TLS/yenileme ve güvenli güvenlik duvarı değişiklikleri.
4. **Amaç planları ve yürütme:** desteklenen bileşen çözümü, tam plan incelemesi,
   kalıcı iş yürütme, kurtarma ve amaca uygun doğrulama.
5. **Rehber arayüzü ve Dashboard:** iki dilde adımlar, ilerleme, dış görevler,
   devam/hata durumları ve tek faydalı tamamlanma eylemi.
6. **Doğrulama ve yayın:** aşağıdaki kabul durumlarını karşıla, desteklenen yolları
   belgele ve incelenmiş sürümü yayımla. Panellerini kullanıcılar günceller.

Bunlar uygulama bağımlılıklarıdır; desteklenmeyen profilleri vaat eden veya eksik
kontrolleri hazır sayan bir sihirbaz yayımlama izni değildir.

## 7. Kabul durumları

- Yeni, devam eden, hazırlanmış ve belirsiz eski kurulumlar doğru yönlenir;
  yeniden aktivasyon kurulum tekrarı veya mevcut kurulum sıfırlaması yapmaz.
- Yönetici dışındaki roller sunucu kurulum API'lerinden reddedilir; lisans bağımsız
  uygulanır ve lisans kaybı mevcut çalışan işleri durdurmaz.
- Sunulan her amaç desteklenen sunucu yetenekleriyle tamamlanır; gerekli bağımlılıklar
  ve aynı anda bulunamayan servisler doğru ele alınır.
- Yerel DNS, doğrulanmış mevcut DNS ve elle yönetilen harici DNS için tam çalışan
  yollar vardır. Harici DNS seçimi yerel DNS kurulumu başlatmaz.
- Primary/secondary görevleri, yetki, yedeklilik ve yayın doğrulanır;
  yapılandırılmış fakat ulaşılamayan veya güncel olmayan eş hazır sayılmaz.
- Geçersiz/uyumsuz/süresi dolmuş panel sertifikası, başarısız yenileme, eski kanıt,
  yanlış DNS ve eksik posta önkoşulları sahte tamamlanma üretemez.
- Güvenlik duvarı uygulaması mevcut yönetim erişimini korur. Uygulanabilir
  [OPERATIONS.tr.md](OPERATIONS.tr.md) gerçek VM yeniden başlatma/kurtarma kapıları geçer.
- Yenileme, tekrar tıklama, çıkış, eşzamanlı oturumlar, yeniden başlatma ve kesinti
  kurulumu tekrarlamaz veya kaydedilmiş işlemi kaybettirmez.
- Mevcut işler/yapılandırma hazırlık değerlendirmesinden ve ilgisiz hatalardan
  etkilenmez. Profil değişikliği canlı servisleri sessizce kaldırmaz veya sıfırlamaz.
- Türkçe/İngilizce, masaüstü/mobil, klavye erişimi ve uygulanabilir hatalar gerçek
  arayüzde doğrulanır. Başarı gizli bir ek düğmeye basmayı gerektirmez.

- Her kurulum işlemi [D-024 uygulanabilir yönlendirme](OPERATION-GUIDANCE.tr.md)
  kuralına uyar: mevcut neden, eylemi yapacak kişi, sonraki eylem ve devam davranışı
  uzun ilerleme listesinden önce gösterilir. Önkoşul, gözlenmiş hata ve bilinmeyen
  sonuç ayrılır. İki DNS başlatma sırası, seçilen DNS modu, ilgili lisans/kimlik
  bilgisi/sağlayıcı hataları, uzlaştırmada hatanın korunması, gizli bilgi içermeyen
  tanılar ve yenilemenin işlemi tekrarlamaması kapsanır.
## 8. Onay sırasındaki başlangıç durumu

Onay sırasında Dashboard içinde `StartGuide` vardı; kalıcı genel amaç sihirbazı
yoktu. Posta profilleri mevcut plan inceleme/yürütme temelini sağlar.
Alan adı ekleme D-009 altında yerel DNS gerektirir; harici nameserver tespiti,
harici sağlayıcı yönetim modu değildir. Panel sertifikası girdisi `CanonicalFQDN`
kullanır ve IP adreslerini reddeder.

Bu tarihsel başlangıç kaydı geçerli çalışma ağacını anlatmaz;
[uygulama durumuna](SERVER-SETUP-STATUS.tr.md) bakın.

İlgili kaynaklar: `web/src/components/StartGuide.tsx`,
`web/src/components/AddDomainModal.tsx`, `cmd/panel/mail_profiles.go`,
`cmd/panel/dns_engine.go`, `cmd/panel/domain_connection.go`,
`cmd/panel/panel_cert_handler.go`, `internal/hostname/hostname.go`.
