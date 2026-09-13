# Sunucu kurulumunda erişim DNS kayıtları

*Çalışma ağacı uygulama notu: 13 Eylül 2026 · [English](SERVER-SETUP-ACCESS-DNS.md)*

**Henüz yayımlanmadı; kurulu Frankfurt/Boston çiftinde doğrulanmadı.** Bu belge,
[onaylı kurulum akışındaki](SERVER-SETUP-PLAN.tr.md) sınırlı düzeltmeyi kaydeder.
Yayımlama kanıtı veya kurulu panel güncelleme izni değildir. Kurulu panelin her
güncellemesini kullanıcı CelikPanel'in kendi arayüzünden başlatır.

## Sorun ve yeni davranış

Yetkili DNS motorunun kurulmuş olması, panel adının kullanılabilir DNS
kayıtlarına sahip olduğunu göstermez. Önceki sıra, panel adının bölgesi henüz
yokken DNS kurulumunu bitirip sertifika istemeye geçebiliyordu. Sunucu sahibini
alan adı oluşturmak için sihirbazdan çıkarmak, onaylı kurulum sonucunu sağlamaz.

Yeni incelenen planlar üç işlemi ayırır: yetkili altyapı kayıtlarını hazırlamak,
hizmet adını herkese açık DNS üzerinden doğrulamak, ardından sertifika almak.
Posta seçilmişse aynı DNS önkoşulu posta sunucusu sertifikasından önce de geçer.
Kontroller `/etc/hosts` sonucunu DNS kanıtı saymaz; beklerken sertifika isteği
başlatmaz veya kayıt değiştirmez. AAAA kaydının bulunmaması IPv6 açma zorunluluğu
değildir; yayımlanmış adresler desteklenen sunucu kimliğiyle eşleşmelidir.

## Açık bölge seçimi ve gerekli kayıtlar

Yerel birincil sunucu, ancak sunucu sahibi sihirbazda tam bölge adını seçerse
bölgeyi hazırlayabilir. Plan, gerekli kayıtları eklenecek veya korunacak kayıt
olarak gösterir. Yalnız panel adresini girmek, onun üst bölgesinin yönetimini
otomatik devralma izni değildir.

Hazırlık yalnız şunları kapsar:

- Bölgenin SOA kaydı ve incelenen ad sunucusu çiftinin NS kayıtları.
- Bölge içindeki ad sunucularının, ilgili sunucunun incelenen IP'sine giden A kayıtları.
- Panel adı ve posta seçilmişse posta sunucusu adı için A kayıtları.
- İsteğe bağlı diğer panel adı için, diğer sunucunun incelenen IP'sine giden A kaydı.

Hazırlanan erişim adları seçilen bölge içinde olmalıdır. Bu altyapı adımı müşteri,
site, posta kutusu, uygulama kök kaydı, `www`, MX, SPF veya DMARC oluşturmaz.
İlgisiz mevcut kayıtlar, TTL ve öncelikler korunur. Mevcut bölgenin yerel sahipliği
alan adı kaydı veya önceki altyapı sahiplik kaydıyla doğrulanır. Bilinmeyen
sahiplik, harici/uzak yönetim, çelişen adresler, takma adlar, devredilmiş alt
bölgeler veya belirsiz otorite, açıklanan bir nedenle hazırlığı durdurur.

## Birincil BIND ve ikincil PowerDNS sırası

İncelenen Frankfurt birincil/Boston ikincil düzeninde:

1. Önce birincilin doğal DNS kurulumunu başlatın. Birincil kataloğunu sunabildiğinde
   ikincili başlatın; birincilin bütün hosting adımlarının veya panel sertifikasının
   tamamlanmasını beklemeyin.
2. İkincil sunucu doğal aktarım ilişkisini kurar. Önce ikincil başlatıldıysa eksik
   birincil katalog önkoşulu, gereken sunucuyu ve sahibinin yapacağı işlemi gösterir.
   Önceden başlatılmış bir işlem gerçekten başarısızsa hata korunur ve kurtarma
   yolu izlenir; işlem sessizce yeniden başlatılmaz.
3. Birincil, doğal DNS çiftinin doğrulaması yayına izin verdikten sonra incelenen
   altyapı bölgesini hazırlar. İkincil, kopyasını mevcut standart DNS bölge
   aktarımı/katalog mekanizmalarıyla alır.
4. Herkese açık ad doğrulaması, doğru yetkili kayıtları ve yönlendirmeyi bekler.
   Panelin yapma yetkisi bulunmayan kayıt kuruluşu/glue veya harici sağlayıcı
   değişikliklerini sunucu sahibi yapar.
5. Sertifika isteği DNS doğrulamasından sonra gelir. Diğer hosting adımları ve
   son hazırlık kontrolleri kendi önkoşullarını korur.

Standart aktarım, diğer sunucunun CelikPanel HTTPS API'sini veya panel giriş
bilgilerini gerektirmez. Uzak kayıt yönetimi otomasyonu ayrı ve isteğe bağlıdır.
DNS yapılandırması mevcut doğal hizmet yaşam döngüsünü kullanır; bu düzeltme,
panel/agent kaldırılmasının her durumda güvenli olduğunu veya bütün iş
 yüklerinin bağımsızlığının denetlendiğini belgelememektedir.

## Kurtarma ve incelenen plan kimliği

İncelenen tam bölge değişiklikleri ve sunucu adresi plan kimliğinin parçasıdır.
Kayıt eklemeleri ile altyapı sahiplik/hazırlık kaydı tek SQLite işleminde kaydedilir.
Yayın mevcut V3 motor, dönem, nesil ve kalıcı değişiklik kimliğini kullanır.
Sayfaya dönmek veya kaybolan bir yanıtı uzlaştırmak, aynı işlemi denetler; ikinci
bölge kurulumu veya doğrulanmış tamamlanmadan sonra tekrarlanan SOA artışı üretmez.

İncelemeden sonra sahiplik ya da kayıt değişirse yeni inceleme gerekir.
Doğrulanmış yayın hatası korunur; nedeni giderildiğinde yeni incelenen işlem
ilerleyebilir. Bilinmeyen/bekleyen yayın, aynı işlem kimliğini korur. Önceden
başlatılan planlara bu yeni adımlar eklenmez. Plan yeniden incelenirken tamamlanan
sunucu değişiklikleri korunur. Kurulum durumunu okumak, ilgisiz yeni bir değişiklik
başlatma yetkisi vermez.

## Kanıt ve destek sınırları

Altyapı yardımcısının 14 ana testi ve alt senaryoları; salt-okur incelemeyi, iki
doğal birincil motor kimliğini, gerekli kayıtları, mevcut MX/TTL/öncelik korumasını,
sahiplik ve takma ad/devir çelişkilerini, çift hazır değilken yazmama durumunu,
değişmez kimliği, hazırlık işlemi sonrası kesintiden dönüşü, aynı bekleyen yayın
kimliğinin uzlaştırılmasını, bilinen hata/yeni incelemeyi ve sürüm değişimini kapsar.
Birleşik yerel komut geçti:

```text
go test ./cmd/panel -run '^(TestServerSetupInfrastructureDNS|TestDNSZoneV3)' -count=1
```

Bunlar yerel SQLite/RPC test düzenekleri ve mevcut V3 gerileme testleridir.
Gerçek DNS servisi, herkese açık sertifika otoritesi, sürüm yayımlama veya kurulu
sunucu doğrulaması kanıtı değildir.

Bu sınırlı hazırlık, imzalı bölgenin anahtarını/imzasını/politikasını değiştirmek
yerine mevcut DNSSEC kanıtı bulunan bölgelerde işlemi durdurur. Harici/uzak DNS
sahipliği korunur. Çakışan bölgeler veya alt bölge devirleri açık uzlaştırma ister.
Önceden devralınmış çeşitli doğal PowerDNS yapılandırmaları ve sunucu sahibinin
yaptığı değişikliklerin algılanması bu düzeltmede bütünüyle denetlenmedi; güvenli
otomatik devralma iddia edilmez. Panel genelindeki lisans/yönlendirme denetimi ve
bütün işlem uyarlayıcıları bu değişikliğin dışındadır.
[D-021](DECISIONS.md), [D-022](OWNER-INDEPENDENCE.md) ve
[D-024](OPERATION-GUIDANCE.tr.md) geçerliliğini korur.

Ek yerel doğrulamalar geçti: Linux panel (1.258 ana test), agent (1.341) ve
DNS protokolü (5) paketlerinin tam testleri; son kurulum odaklı testler (104);
436 arayüz testi; TypeScript/Vite üretim derlemesi ve değişmeyen paket
boyutu sınırları; masaüstü/mobil ve Türkçe/İngilizce sekiz tarayıcı
senaryosu. Kontroller yerel kaynak kopyaları ve test düzeneklerinde yapıldı;
kurulu sunucularda yapılmadı.

V3 motor geçişiyle oluşturulan PowerDNS veritabanlarında yeni yayın veya
silme öncesinde son yönetim kaydı; mevcut DNS kayıtları, bölge türü ve
bölgenin varlığı ile karşılaştırılır. Sahipliği kanıtlanmamış ad
çakışmaları, elle değiştirilmiş kayıtlar, kaybolmuş bölgeler ve
beklenmedik yeniden oluşturma; kayıt, katalog veya işlem makbuzu
değiştirilmeden reddedilir. Genel yerel metadata ve eski kurulumların
yönetime alınması ayrı denetim kapsamında kalır.
