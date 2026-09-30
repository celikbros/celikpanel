# Kesintilere dayanıklı işletim: mimari sözleşme ve uygulama denetimi

*14 Eylül 2026 · [English](RESILIENCE-CONTRACT.md) · D-025*

**Durum: uyulması gereken yön ve incelenmiş uygulama planı; tamamlanmış bir yetenek değildir.**
Sunucu sahibi, Frankfurt'ta art arda yaşanan sorunların ardından anayasanın ve
mimarinin incelenmesini istedi. Alpha80, gözlenen BIND ve geri alma hatalarını
düzeltir; bu sözleşmeyi bütünüyle hayata geçirmez. Bu belge hiçbir kurulu sunucuyu
değiştirmez. Kurulu panel güncellemelerini kullanıcı CelikPanel içinden başlatmaya
devam eder.

## Bulgu

Root yetkisi olmayan panel ile root yetkili agent arasındaki yetki ayrımı yararlılığını koruyor.
Eksik olan sınır; **bir kaynağı değiştirme yetkisi**, **kaynağın durumuna ilişkin
bilgi** ve **yönetim ile kurtarmanın erişilebilirliği** arasındadır. Güvenli olmayan
bir değişikliğin reddedilmesi, bugün bazı akışlarda yönetimin de kullanılamamasına
yol açıyor. Farklı bileşenler aynı kanıtı birbirinden bağımsız yorumluyor. Her hataya
yeni bir istisna eklemek, güvenilir bir işletici ortaya çıkarmayacak.

Eski anayasa güvenlik, sadelik, hız ve esneklik istiyordu. Çalışmayı sürdürme ve
kurtarma yükümlülüklerini açıkça tanımlamıyordu. Mutlak ifadeleri daha sonraki
kararlarla da çelişiyordu: tek bir arayüz yolu, tek bir kurtarma mekanizması demek
değildir; "yalnız panel üzerinden" kuralı, sunucu sahibinin yerel araçlarla yönetim
yapmasını veya panel kapalıyken kurtarma uygulamasını engelleyemez; küçük bir çalışma
ortamı hedefi, bağımsız bir kurtarma bileşenini yasaklamaz. D-022 ve D-024 istenen yönü
anlatıyordu, ancak bunu uygulayacak sınırları ve yaşam döngüsünün tamamını kapsayan
kabul koşullarını eksiksiz tanımlamıyordu.

Aynı hata sınıfı [26 Ağustos olayının](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.md)
5. ve 6. bölümlerinde de kaydedilmişti. Bu aynı zamanda bir süreç hatasıdır:
çıkarılan dersi belgelemek ve tek bir yeniden üretim senaryosunu düzeltmek,
mimari yükümlülüğü sürüm yayımlama ölçütüne dönüştürmedi.

## Kaynak koda dayalı hata haritası

Temel alınan kanıtlar: Alpha79 olay kayıtları ve Alpha80 kaynak kod commit'i
`bd14d97efc5cfd19acd70ddf0edb9c6343317e2b`. Bağlantılar kodu veya test kapsamını
anlatır; Frankfurt ya da Boston'da gözlenmemiş güncel bir arıza bulunduğunu
ileri sürmez.

| Sınır | Gözlenen mekanizma | Gereken değişiklik |
|---|---|---|
| DNS durumu ve sahipliği | `dnsEngineStateReceipt`; DNS motorunu yönetme yetkisinin edinimini, yayın neslini/kataloğu, topolojiyi ve değişiklik işleminin kimliğini aynı yapıda tutuyor. Olağan yayın yalnız bazı alanları ilerletiyor; yapının tamamını eşit saymayı şart koşmak bunu reddetti. [Yazıcı](../cmd/agent/dns_engine_host.go), [sahiplik](../cmd/agent/dns_engine_ownership.go), [Alpha80 anlam denetimi](../cmd/agent/dns_engine_ownership_publication.go). | Değişmez yetki edinim kimliğini güncel yayın kanıtından ayır. Karşılaştırmaları kaydın rolüne göre tanımla. Bayt düzeyinde tam eşitliği, farklı amaçlı kayıtlar arasında değil, dondurulmuş bir işlem günlüğü veya snapshot için koru. |
| TLS yazıcısı ve snapshot okuyucusu | Go ile yazılan sertifika düzenleyicisi bir işlem kanıtı kaydını korudu; kabuk betiğindeki snapshot doğrulayıcısı bunu bilinmeyen dördüncü dosya olarak reddetti. [Düzenleyici](../cmd/agent/panel_cert_issue_files_linux.go), [snapshot](../deploy/panel-tls-snapshot.sh), [olay](UPDATE-TLS-RECOVERY-INCIDENT.md). | Sertifika düzenleme, yenileme, snapshot alma ve geri yükleme aynı sürümlü dosya şemasını kullansın; sürümler arası testler gerçek yazıcı çıktılarıyla çalışsın. |
| Güncelleme kontrol noktaları | Kalıcı `active` aşaması hem değişiklik öncesi yakalamayı hem de daha sonraki kurulu sistem değişikliğini kapsıyor. Bazı uyumluluk kontrolleri koordinatörler durdurulduktan sonra yapılıyor. [Güncelleyici](../update.sh). | Kurulu dosyalardaki değişikliği, snapshot'ın tamamlanmasını, çalışma durumunun doğrulanmasını ve temizliği ayrı kontrol noktalarıyla tanımla. Kurtarma kararlarını tam kontrol noktasına ve etkilenen kaynaklara bağla. |
| Yakalama ve normalleştirme | TLS normalleştirmesi, yakalamadan ve kurulu sürüm dosyalarına ait `mutation_started` bayrağından önce canlı dosyaların izinlerini/sahipliğini değiştirebiliyor. [Güncelleyici](../update.sh), [TLS yardımcısı](../deploy/panel-tls-snapshot.sh). | Gözlem ve yakalama salt okunur olmalı. Normalleştirme; değişiklik öncesi durumun kalıcı kopyası ve kurtarma kontrol noktası bulunan açık bir geçiş işlemi olmalı. İkili dosyalar değiştirilmedi diye sunucuda yapılan bu değişiklikler "değişmedi" olarak adlandırılamaz. |
| Kurtarma uygulaması | Çalıştırıcı, başarısız hedef sürümü seçip onun geri alma betiğini ve denetleyicilerini çağırıyor. Bu sürümün kilit ayrıştırıcısındaki hata kurtarmayı da devre dışı bıraktı. [Çalıştırıcı](../deploy/release-recovery-runner.sh), [geri alma](../rollback.sh), D-001. | Aday sürümün keyfi yaşam döngüsü kodunu çalıştırmayı gerektirmeyen, ayrı sürümlenen asgari bir kurtarma yürütücüsü ve manifest sözleşmesi oluştur. Yeni yürütücü kurtarma tatbikatlarını geçene kadar uyumluluğu kanıtlanmış önceki yürütücüyü koru. |
| Yönetime erişim | İlk Agent bağlantısı başarısız olduğunda panel, HTTPS hizmetini başlatmadan kapanıyor. Güncelleme durumu da uyumlu bir Agent gerektiriyor. [Başlangıç](../cmd/panel/main.go), [durum](../cmd/panel/system_update_handlers.go). | Normal Agent çalışmadan veya uygulama geçişi tamamlanmadan da kimlik doğrulamalı, dar kapsamlı kurtarma/durum erişimi kullanılabilir kalmalı. Birbiriyle uyumsuz şema ve dosyalarla sınırsız yönetimi başlatma. |
| Bilinmeyen durum | Lisansın bulunmaması, süresinin dolması ve doğrulamanın kullanılamaması aynı `can_use_panel=false` sonucuna indirgeniyor; ilk oturum sorgusu da bağlantı hatasını oturum yokluğu sayıyor. [Lisans](../cmd/panel/license.go), [arayüzde kimlik doğrulama](../web/src/App.tsx), [ilk kullanım](../web/src/components/LicenseOnboarding.tsx). | Nedenleri ve yeteneklere erişim kararlarını ayrı türlerle tanımla; belirsizlik başka bir tanıya dönüşemez. Kanıtın bulunmaması, kimlik doğrulamasını veya lisansa bağlı değişiklik haklarını sağlamaz. |
| Hizmet yaşam döngüsü | Panel TLS yakalaması ortak Certbot zamanlayıcılarını askıya alıyor. Posta sertifikası dağıtımı ve açılışta güvenlik duvarını geri yükleme hâlâ Agent'ı çağırıyor. [TLS yardımcısı](../deploy/panel-tls-snapshot.sh), [bağımsızlık denetimi](OWNER-INDEPENDENCE.md). | Panel sertifikasının yayımlanmasını, web sitesi/posta sertifikalarının yerel yenilenmesinden ve güvenlik duvarının açılışta çalışmasından ayır. Yönetim ikilileri yalnız boşta değil, sistemde yokken test et. |
| Kabul | Bileşen testleri, devir test düzenekleri ve kaynak kod sırası denetimleri, yerel hizmetlerle başarısız bir kurulu sistem güncellemesini ve otomatik geri yüklemeyi baştan sona gerçekleştirmiyor. [Kurtarma testleri](../deploy/test-release-recovery-contract.sh), [devir testi](../deploy/test-release-recovery-rollback-handoff.sh). | Gerçek önceki sürüm durumu; gerçek systemd, SQLite, TLS ve DNS ile çalışan hizmet kontrolleri kullanılarak tek kullanımlık sanal makinelerde yükseltme ve hata tatbikatları yapılmalı. |

## Uyulması gereken değişmezler

### 1. Sunucu sahibi yetkisini ve çalışan hizmetlerini korur

CelikPanel, sahibinin kabul ettiği işlemin sınırları içinde hareket eder. Yerel
hizmetler panelin erişilebilirliğinden ve lisansından bağımsız çalışmaya devam eder.
Agent, sahibin yaptığı değişiklikleri tespit etmelidir; bunları önbellekteki tercih
edilen bir yapılandırmayla sessizce değiştiremez. Başarısız bir panel güncellemesi
yerel DNS, web, posta, veritabanı veya cron işleyişini durdurmamalı; bunların
bağımsız yenileme veya açılış mekanizmasını askıda bırakmamalıdır.

Desteklenen hata modeli; sunucunun güvenilir bir kurtarma ortamını
çalıştırabildiğini, doğrulanmış kurtarma malzemesini okuyabildiğini ve sahibinin
kimliğini doğrulayabildiğini varsayar. Sunucunun yok olması, depolamanın okunamaması
veya kimlik doğrulama malzemesinin kaybı karşısında koşulsuz HTTPS erişimi garanti
edilemez. Bu bağımlılıkları ve geçerli haricî geri yükleme yolunu kaydet; bunların
başarısızlığını gizlemek için kimlik doğrulamayı asla zayıflatma.

Kimlik doğrulama ve en az yetki ilkesi zorunlu olmaya devam eder. Kurtarma erişimi,
yeni bir genel root API'si veya lisansı aşma yolu değildir. Gösterdiği durum
verisinin kapsamı sınırlı olmalı, hassas içerik gizlenmeli ve veri doğru sunucu
ile işleme bağlanmalıdır.

### 2. Kanıtın bir anlamı ve yaşam döngüsü vardır

Şemalarda ve API'lerde şu kavramları ayrı tut:

- sunucu sahibinin niyeti ve kabul edilmiş değişmez plan;
- yetki/edinim kimliği ve onun dönemi;
- yayımlanmış güncel yapılandırma ve revizyonu;
- bir gözlem, kaynağı/zamanı ve hâlâ kullanılabilir olup olmadığı;
- aynı işlemin yürütme, doğrulama ve kurtarma sonuçları.

Bilinmiyor, kullanılamıyor, yok, reddedildi ve başarısız birbirinden farklı
durumlardır. Zaman aşımı; başarısızlığın, başarının, lisansın dolmasının veya yeniden
başlatma izninin kanıtı değildir. Bir kurulum adımının tamamlanması, hizmetin şu anda
sağlıklı olduğunu veya dışarıdan karşılanması gereken bir önkoşulun sağlandığını
göstermez.

Her kalıcı kaydın tek bir şema sorumlusu, açık sürüm/geçiş kuralları ve ortak
doğrulayıcıları vardır. Üreticiler, okuyucular ve geri yükleyiciler; alanlar ve izin
verilen değişimler üzerinde aynı sözleşmeye uyar. Hiçbir "onarım", sırf eşitlik
kontrolünü geçirmek için çelişen bir kanıt kaydını diğerinin üzerine kopyalayamaz.
Geçiş, kayıtlar arasındaki ilişkiyi kanıtlar ve sonucu doğrulanana kadar özgün
kanıtı korur.

### 3. Güvenli olmayan eylemi kendi sınırında durdur

İşleme izin verme ve başarısızlık kararları, etkilenen kaynağı/yeteneği belirtir.
DNS yayın çakışması o yayını engeller; belirsiz kullanım hakkı, güncel hak
doğrulaması gerektiren yetenekleri engeller; Agent kesintisi onun ayrıcalıklı
eylemlerini engeller. Bunların hiçbiri tek başına yanlış bir aktivasyon hatası
tanısı koymayı veya kimliği doğrulanmış durum ve kurtarma erişimini kaldırmayı
haklı çıkarmaz.

Veritabanı geçişi veya ikili dosyaların birbiriyle uyumsuz olması, normal yönetimin
durmasını gerektirebilir. Bu durumda bağımsız kurtarma/durum erişimi kullanılabilir
kalmalıdır. Bu erişimi açık tutmak; değişmekte olan veritabanını güvenli olmayan
bir ikili dosyayla açma, değişiklik kilidini bırakma veya çakışan başka bir işleme
izin verme yetkisi değildir.

### 4. Her değişikliğin bir kurtarma sözleşmesi vardır

Bir kaynağı değiştirmeden önce şunları tanımla: tam kimlik ve yetkilendirme,
bağımlılıklar, etkilenen dosyalar/hizmetler, önkoşullar, gözlenebilir kontrol
noktaları, uyumlu sürümler, kurtarma işlemi ve nihai sonucun kanıtı. Keşif ve önizleme
salt okunurdur. Bilinen uyumsuzluklar, önlenebilir kesintiden önce denetlenir ve son
değişiklik bariyeri altında yeniden kontrol edilir. İzin veya sahiplik değişiklikleri
dahil üstveri normalleştirmesi bir değişikliktir. Uygulamadan önce değişiklik öncesi
durumu kalıcı olarak kaydet ve kurtarılabilir bir geçiş aşaması oluştur. Snapshot
yakalama, sözde salt okunur bir doğrulama adımının içine canlı geçiş gizleyemez.

Kurtarma en az şu durumları ayırt eder: değişiklikten önce reddedildi; yakalama
tamamlandı ve durum değişmedi; kısmen uygulandı; aday doğrulandı; çalışma durumu
doğrulandı; temizlik bekliyor. Desteklenen her kontrol noktasında yürütücü ya aynı
kabul edilmiş işleme devam etmeli, ya önceki durumu geri yükleyip doğrulamalı ya da
kanıtı koruyarak bağımsız kurtarma erişimi üzerinden sunucu sahibine somut bir
eylem göstermelidir. Doğrulanamayan geri yükleme başarı olarak bildirilemez.

Tarayıcı gözlemcidir; işi yürütme veya terk etme kararının yetkili kaynağı değildir.
Yeniden deneme politikası sınırlı, yinelendiğinde ek etki üretmeyen ve tek bir
işleme bağlıdır. Geçici olduğu bilinen bir okuma hatasını yeniden denemek ile
sonucu bilinmeyen bir değişikliği yeniden çalıştırmak farklı şeylerdir. Otomatik
geri alma yalnız incelenmiş işlemin etkileriyle sınırlıdır; sahibin daha sonra
yaptığı değişiklikleri veya ilgisiz başarılı işlemleri geri alamaz. Yeniden deneme
hakkı tükendiğinde; aynı işlemi, son doğrulanmış hatayı ve sahibin sonraki eylemini
koruyan, uygulanabilir yönlendirme içeren kalıcı bir durum oluşturulur. Aynı
koşullarda tekrarlanan kesin hata, otomatik değişiklik denemelerini sona erdirir;
desteklenen sunucu sahibi kurtarma yolu kullanılabilir kalır.

### 5. Kurtarma, adayın başarısızlığında da çalışmalıdır

Normal uygulama başlangıcından ve adayın güncelleme/geri alma kodundan ayrı,
kararlı bir kurtarma protokolü ve asgari bir yürütücü oluştur. Bu yürütücü;
güvenilir manifestleri, tam snapshot'ları, şema uyumluluğunu, kilitleri ve kaynak
kimliklerini lisans hizmetine veya çalışan bir aday Agent'a bağımlı olmadan
doğrulamalıdır. Arayüz ve sunucu sahibinin komut satırı aracı aynı sözleşmeyi
kullanır.

Bu, önerilen bir geçiştir; bugün başka bir sürümün geri alma betiğini çalıştırma
izni değildir. Uygulanana kadar desteklenen, tam olarak ilgili saklanmış sürüme
bağlı geri alma kuralları geçerlidir. Yeni yürütücü ancak mevcut snapshot'larla
uyumluluğu kanıtlandıktan ve çalışan bir önceki yürütücü korunduktan sonra
etkinleştirilebilir. Bilinmeyen manifest sürümlerinde kanıt ve desteklenen erişim
korunur; bunlar iyimser varsayımlarla yorumlanmaz.

Adayın çalışma durumu ve kurtarma uyumluluğu ortaya konana kadar kullanılabilirliği
doğrulanmış son sürümü ve onun kurtarma malzemesini koru. Gereksiz dosyaların
temizlenmesi, bu kanıtlardan sonra yapılan ayrı ve yeniden denenebilir bir işlemdir.

### 6. Hazır olma, ilerleme ve erişim birbirinden bağımsız kalır

Yürütme, doğrulama, kurtarma, bağlantı ve yeteneklere erişim kararlarını ayrı
modelle. Arayüz, nedeni ve sonraki eylemi yetkili durum kaynağından gösterir;
bunları sayfa yolundan, dönen simgeden, yalnız HTTP başarısından veya bir boolean
izin değerinden çıkarmaz. Başarıyla kurtarılan bir alt sistem, aynı işleme ait
yeni kanıt kullanarak yalnız kendi kayıtlı durumunu temizler.

Sayfa düzeninin veya üst katmanın yüklenememesi, yerleşik kurtarma görünümünü
erişilebilir bırakmalıdır. Sayfalar arasında gezinme imkânı ile çakışan değişiklik
işlemlerine izin verilmesi ayrı kontrollerdir. Yenileme/yeniden bağlantı sonrasında
açık talimatlar ve işlem kimliği erişilebilir kalır.

Mevcut bir dakikalık lisans doğrulama politikası bu sözleşmeyle değişmez. Önce
türlerle ayrılmış kararları ve kurtarma yeteneklerini fiilen uygulanan politikayla
uyumlu hale getir; erişim hatalarını gizlemek için lisansın geçerli sayıldığı
süreyi sessizce uzatma. Eski lisans belgesindeki günlük/yedi günlük süre ve bakım
iddialarının geçmişteki ve güncel politikayla ilişkisi açıkça uzlaştırılmalıdır.

## Uygulama sırası ve tamamlanma kanıtı

Bunlar yapılacak işlerdir, tamamlanmış kutular değildir. Uçtan uca doğrulanabilir
dar kapsamlı adımları tercih et; ürünü tek seferde baştan yazma.

| Öncelik | Çıktı | Gerekli kabul kanıtı |
|---|---|---|
| P0.1 | Gerçek önceki sürüm çıktısıyla yeniden üretilebilir yerel yükseltme/otomatik geri alma tatbikatı; işlem kanıtının bağımsız yakalanması. | Önce yayımlanmış dosyalarla tek kullanımlık sanal makinelerde bilinen bir başarısız yaşam döngüsünü yeniden üret. Test, geri yükleme gövdesini gerçekten çalıştırmalı, hizmetleri yeniden başlatmalı ve sonucu incelemelidir. Taklit systemctl veya yalnız başarı döndüren bir alt süreç yeterli değildir. |
| P0.2 | Türlerle ayrılmış erişim/gözlem ve Agent ile adayın başlamasından bağımsız, kimlik doğrulamalı kurtarma/durum yolu. | İlk başlangıçta ve işlem sırasında Agent'ın kullanılamaması; lisans doğrulayıcısının mevcut süreden uzun kullanılamaması; sayfa yenileme ve üst katman yükleme hatası. Aynı işlem görünür kalır; ek değişikliğe veya yeni yetkiye izin verilmez. |
| P0.3 | Kararlı kurtarma manifesti/yürütücüsü ve açık güncelleme aşamaları. | Her aşamada ve geri alma sırasında hata uygula; kurtarma sürecini öldür veya sistemi yeniden başlat. Geliştiriciye özel script olmadan desteklenen son durum ve kurtarılabilir erişimi doğrula. Etkinleştirmeden önce eski snapshot uyumluluğunu kanıtla. |
| P0.4 | Ortak DNS/TLS dosya sözleşmeleri ve desteklenen geçişler. | Gerçek eski/yeni sertifika düzenleme, yenileme, kayıt ekleme/düzenleme/silme, snapshot ve geri yükleme üreticileri birlikte çalışır. Olağan revizyon değişiklikleri geçer; değişen sahip yetkisi, bozuk kanıt ve sahibin düzenlemeleri doğru sınırda reddedilir. |
| P0.5 | Bağımsız yenileme ve hizmetlerin açılışta çalışması. | Yönetim durdurulmuşken veya sistemde yokken DNS primary/secondary aktarımı, web isteği, veritabanı işlemi, posta teslimi/kimlik doğrulaması, cron, sertifika yenileme ve yeniden başlatma sonrası güvenlik duvarı; desteklendiği söylenen her birleşimde çalışır. |

### Kabul takibi ve değişiklik koşulları

Yukarıdaki P0 kimlikleri, takip edilen iş kalemleridir. Özet 26 Eylül'de gözden geçirildi; aşağıdaki tarihli deney notları kendi kapsamlarını korur. [Güncel yol haritası](../ROADMAP.tr.md#neredeyiz--26-eylül-2026), kalan işleri ve bağımlılık sırasını bir araya getirir:

| İş | Kayıtlı durum | Tamamlanma kanıtı |
|---|---|---|
| P0.1 | Kısmi — Arch ve Debian’da bir kontrol noktasında gerçek eski sürüme dönüş geçti | [Birim geçişi kabulü](../deploy/e2e/release-recovery/UNIT-TRANSITION.tr.md), SIGKILL sonrası gerçek geri yüklemeyi ve çalışan eski dosyaları kaydeder. Geniş hata/hizmet matrisi, tutarlı veritabanı içeriği ve imzalı aday kabulü hâlâ açıktır. |
| P0.2 | Kısmi — türlenmiş erişim, Agent bağımsız başlangıç gözlemi ve yerel kurtarma girişi uygulandı | [Erişim/gözlem kabulü](RECOVERY-ACCESS.tr.md) ve [bağımsız kurtarma ortamı](RECOVERY-RUNTIME.tr.md). Root/sudo durum ve kurtarma yolu Panel/Agent başlangıcına veya lisansa bağlı değildir. [Yerel AJ](../deploy/e2e/release-recovery/BOUND-WORKER.tr.md), tek kullanımlık Debian şema42→42 deneyinde gerçek worker sonlandırmasını, otomatik kurtarma sırasında yeniden başlatmayı, doğrulanmış geri almayı ve CLI/kimlik doğrulamalı HTTP/tarayıcı nihai sonuçlarının eşleşmesini kanıtlar. [Yerel AK](../deploy/e2e/release-recovery/BOOT-WAIT.tr.md), gerçek `starting` yönlendirmesini root CLI üzerinde ve aynı isteğin zamanlayıcıyla geri almaya ulaşmasını da kanıtlar. Diğer beklemeler, önceden bilinen hatanın gerçek beklemede korunması, beklerken HTTP/tarayıcı erişimi, arayüzden güncelleme başlatma, üretim imzası ve tam kesinti/erişim matrisi açıktır. |
| P0.3 | Kısmi — bağımsız kod/veri, atomik yayın ve seçili gerçek kurtarma SIGKILL/reboot sınırları geçti | [Bağımsız çalışma ortamı](../deploy/e2e/release-recovery/INDEPENDENT-RUNTIME.tr.md) ve [veri kabulü](../deploy/e2e/release-recovery/RECOVERY-MATERIAL.tr.md): saklanan adayın üç dosyası yokken Arch payload_restored SIGKILL ve Debian runtime_verified reboot aynı geri almayı otomatik tamamladı. Seçili kit geçişinin ayrı [kaynak sözleşmesi](RECOVERY-RUNTIME-PROMOTION.tr.md) ve [gerçek sistem kabul kaydı](../deploy/e2e/release-recovery/RUNTIME-PROMOTION.tr.md) vardır; Ayrı Arch/Debian deneyleri, başlatıcı geçişindeki kesintiden sonra sahip devamını ve kit geçişinden sonra otomatik uygulama geri almasını kanıtlar. Önceki başarısız deneyler kayıtlı kalır; bu sınırlı sonuçlar P0.3’ü kapatmaz. [İleri tamamlama verisi v2](RECOVERY-FORWARD-COMPLETION.tr.md), veritabanı hazır kontrol noktasından sonra üç saklanan aday dosyası yokken [sınırlı Arch/Debian gerçek sistem kabulüne](../deploy/e2e/release-recovery/FORWARD-COMPLETION.tr.md) sahiptir. [Ayrı kopyada veritabanı dönüşümü v3](RECOVERY-ISOLATED-DATABASE.tr.md), aday ayrı kopyayı dönüştürürken normal güncellemeyi active tutar; bağımsız doğrulamadan sonra atomik yayımlar. [Sınırlı gerçek Q/R kabulü](../deploy/e2e/release-recovery/ISOLATED-DATABASE.tr.md), gerçek Alpha64/schema38 başlangıcını kapsar: Arch ilk DB çalışma kopyasını koruyarak otomatik geri alır; Debian gerçek 38→42 dönüşümünü ve saklanan üç aday dosyasının kaybını otomatik tamamlar. Son kaynağa ait ayrı R kanıtı, 55 tablonun eski satırlarını ve yayın kayıtlarını doğrular. Ayrı [gerçek WAL kesintisi kanıtı](../deploy/e2e/release-recovery/NATIVE-WAL.tr.md), Debian ve Arch üzerinde dolu schema38 verisiyle tek bir fiziksel, commit edilmemiş yazma sınırını ve ardından aynı işlemin otomatik geri alınmasını kaydeder. WAL kanıtı tek başına dolu domain verisinin başarılı 38→42 dönüşümünü kanıtlamaz. Sonraki [gerçek exchange kabulü](../deploy/e2e/release-recovery/NATIVE-DATABASE-EXCHANGE.tr.md), Arch U ve Debian W üzerinde değiştirilen çiftte bu dönüşümü ve yayın makbuzundan önce ters exchange ile otomatik geri almayı doğrular; 55 eski tablo ve kesinti anındaki bütün satırlar korunur. Önceki belirsiz Debian U/V denemeleri kayıtlı kalır. Ayrı [Debian X iki kesintili kabulü](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.tr.md), gerçek exchange kesintisinden sonra yerel geri almayı payload_restored noktasında VM yeniden başlatmasıyla keser. İlk açılış denemesi systemd starting durumundayken başarısız olur; mevcut zamanlayıcı tekrar çalışıp aynı geri almayı tamamlar ve kesinti anındaki 99 satır korunur. Başarısız deneme saklanır. Bu sonunda otomatik kurtarma kanıtıdır; kesintisiz hizmet veya güç kaybı dayanıklılığı değildir. Sonraki [Arch Z kabulü](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.tr.md#z-arch-gerçek-sistem-kabulü), aynı iki kesinti sınırını geçer: erken açılıştaki iki hata korunur, mevcut zamanlayıcı aynı geri almayı tamamlar ve 100 eski satır değişmeden kalır. Z, başlangıçtaki çekirdek paketi yükseltmesini ve yeniden başlatma sonrasındaki farklı çalışan çekirdeği de kaydeder; güvenlik duvarı/VPN hazır oluşu iddia edilmez. Önceki Y hazırlık hatası korunur ve gerçek kesinti deneyi sayılmaz. Kalan arıza/gerçek hizmet matrisi kapanmaz. [WAL ve dolu SQL deney önkoşulları](../deploy/e2e/release-recovery/WAL-FIXTURE.tr.md), kontrollü yazıcı ve özel kopya testlerini ayrı tutar; gerçek sistem kabulü değildir. Önceki v2 deneyleri bu yeni sınırı kanıtlamaz. Tam aşama matrisi, imzalı kabul, eksik yedek veri bağımsızlığı, metadata geçişleri ve temizleme açıktır. |
| P0.4 | Kısmi - ortak DNS/TLS sözleşmeleri ve bağımsız DNS gözlemi; seçili Agent aracılı kurtarma ve sınırlı korumalı sahip CLI ters işlem kanıtları | [Posta sözleşmesi](MAIL-CERTIFICATE-ARTIFACT.md), [DNS sözleşmesi](DNS-ENGINE-ARTIFACT.md) ve [DNS deney dizini](../deploy/e2e/dns-kill-matrix/README.md) üretici, sahip değişikliği ve arıza kanıtlarının kesin kapsamını korur. Yetkili üst bölge varken [BIND V3 silme](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) geçti. Debian sahip CLI kanıtı harici PowerDNS devralmasını, yönetilen PowerDNS-BIND geçişi geri almasını ve statik bir çalışan-BIND devralma geri almasını kapsar. Diğer ters işlem türleri ve platform/topoloji varyantları, bu dilimlerin dışındaki kesintili yerel temizlik, bütün üretici/geri yükleme geçişleri, üst bölgesiz ikincilde silme kanıtı ve tam DNS arıza matrisi açıktır. P0.4 açık kalır. |
| P0.5 | Kısmi — sınırlı güvenlik duvarı/posta bağımsızlığı ve yeniden açılışta yerel DNS hizmeti | [Güvenlik duvarı](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md), [yönetimsiz posta devreye alma](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-MANAGEMENT-ABSENT-BE.json), [tek PowerDNS açılışı](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-MANAGEMENT-ABSENT-BOOT-20260925.md) ve [BIND çifti kesinti/açılışı](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md) sınırlı kanıt sağlar. Tam gerçek sihirbaz/devreye alma kabulü, eski uygulamaya geri alma uyumu, farklı DNS motoru birleşimleri, Arch posta ve tam hizmet/yönetimsiz çalışma matrisi açık. Yönetimin devre dışı olması, çalıştırılabilir dosyaların yokluğu ve tam kaldırma ayrı koşullardır; genel kaldırma desteği iddia edilmez. |

P0.4 gerçek DNS kesinti kanıtına, gerçek yönetilen PowerDNS kaynağıyla beş tek sunuculu BIND hücresi eklendi: source-stopped/before-write, source-stopped/after-write, target-started/after-write, rolled-back/before-write ve rolled-back/after-write. Temiz Debian 13 deneylerinin her biri 137 ile öldürmeyi, aynı isteğin Agent aracılığıyla BIND durumuna toparlanmasını, yetkili UDP/TCP DNS yanıtlarını ve kurtarma sonrasında 30 saniye Agent/Panel/DNS sağlığını doğruladı. [DNS kesinti matrisi raporları](../deploy/e2e/dns-kill-matrix/README.md) kapsamı ve dosya özetlerini verir. Kalıcı DNS geçiş günlüğü v1 ve kurtarma gözlemi v1 değişmedi. Bu beş hücre, geçiş sırasında kesintisiz DNS, çift sunucu, yeniden başlatma/güç kaybı, sunucu sahibi değişikliği veya Agent bağımsız ters işlem kanıtı değildir; P0.4 ve kalan çalıştırılabilir matris açıktır.

Yaşam döngüsünü, kalıcı kanıtı, erişim koşullarını, kurtarmayı veya yerel hizmet
sahipliğini değiştiren her PR; etkilenen P0 işlerini/değişmezleri, önceki/sonraki
davranışı, uyumluluk ve geri alma etkilerini, tam kabul kanıtını belirtmelidir.
Burada listelenen kanıt olmadan bir değişiklik P0 işini tamamlandı sayamaz. Yerel
ortamdaki hata kapsamının eksik olması, yaşam döngüsünün hatalara dayanıklılığının
desteklendiği iddiasını engeller. Acil düzeltmeler; olayı, dar kapsamda etkilenen
sözleşmeyi, testleri ve kalan riskleri PR'da ve sürüm notlarında kaydetmelidir;
çözülmemiş takip kalemleri açık kalır. Bu elle yapılan inceleme koşulu şu andan
itibaren zorunludur; yaşam döngüsünün tamamını kapsayan otomatik CI kapısı henüz
oluşturulmamıştır.

Yeni özellik geliştirme, bu çözülmemiş temel işleri atlamak için kullanılamaz.
Acil bir olay düzeltmesi dar kapsamlı kanıtları ve sınırlarıyla yine
yayımlanabilir, ancak temelin tamamlandığı anlamına gelemez. Alpha80 böyle sınırlı
bir düzeltmedir; sözleşmenin tamamının kanıtı değildir.

[Systemd geçişi düzeltmesi](RECOVERY-RUNTIME.tr.md#işletim-sistemi-geçişinde-kurtarmayı-erteleme),
süre sınırı olan başlatma ertelemesiyle aynı bekleyen işlemi ve önceki hata kanıtını
korur. Salt-okur son doğrulamanın bekleyen işaretçiler için kurtarma başlatmasını da
engeller. Yerel sözleşmeler geçti. Yeni
[AB Arch deneyi](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.tr.md), iki
açılış ertelemesinden sonra aynı işlemin otomatik geri alındığını, açılış sonrası
sıfır kurtarma hatasını ve 55 tablodaki 102 eski satırın korunduğunu doğrular.
Ayrı [AD Debian deneyi](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.tr.md),
bir açılış ertelemesini, aynı işlemin otomatik geri alınmasını, açılış sonrası
sıfır kurtarma hatasını ve 55 tablodaki 100 eski satırın korunmasını doğrular.
Değişen yürütücünün bu Debian kabul sınırı kapanır; P0.1–P0.3 bütünüyle kapanmaz.
Tarayıcıdaki bekleme açıklaması [uyumlu ek gözlemle](RECOVERY-ACCESS.tr.md) uygulandı;
yeni gerçek worker→arayüz bağı kabulü ve geniş matris açıktır. Önceki X/Z hataları,
belirsiz AA sonucu ve yalnız hazırlıkta duran AC korunur.

[Kalıcı deneme sınırı](RECOVERY-DISPATCH-BUDGET.tr.md), snapshot başına
üç otomatik hak ve açık sahip devamıyla sınırsız alt işlem tekrarı açığını giderir.
Shell/Go sözleşmeleri geçti. Sınırlı AL kabulü aşağıda kaydedilmiştir; yayın
sınırındaki kesintiler ve tarayıcıya özel yönlendirme P0.2/P0.3 altında açıktır.
Önceki AJ/AK kanıtı yeni politikayı doğrulamaz.

## Hata matrisi

Sanal makine test düzeneği, gerçek durum içeren yayımlanmış bir kurulumdan
başlamalıdır: kayıtlarla dolu DNS kataloğu ve yerel secondary; geçmişi korunmuş,
düzenlenmiş ve yenilenmiş sertifikalar; gerçek bir panel veritabanı; çalışan örnek
hizmetler; yerel yenileme zamanlamaları. Her kalıcı kontrol noktasından önce/sonra
hata, süreç sonlandırma, yeniden başlatma, disk yazma hatası, paket kilidi
çekişmesi, Agent/API kaybı, lisans/DNS sağlayıcısının kullanılamaması, sahibin
düzenlemeleri ve güncellemeyle eşzamanlı TLS yenilemesi durumlarında güncelleme ve
geri almayı çalıştır. Hem olağan arayüz akışını hem de desteklenen sunucu sahibi
kurtarmasını kapsa.

Her durumda tam sürümleri, aşama/işlem kimliklerini, oluşturulan hatayı, kurulu
dosyaları, veritabanı ve hizmet kanıtlarını, yönetim/kurtarma erişilebilirliğini,
zamanlayıcıları/birimleri, sahibin değişikliklerini ve nihai sonucu kaydet.
Denetimler; yinelenen değişiklik, kaybolan kanıt veya sahibin sonraki değişikliğinin
kaybı, yanlış tamamlanma bildirimi ve ilgisiz hizmetlerin yaşam döngüsünde değişiklik
olmadığını kontrol etmelidir. Gerekli kanıtı elde edemeyen test geçmiş sayılmaz;
sonucu belirsizdir. Kurtarma süresi sınırları, desteklenen her işlem için ölçülüp
belirlenmelidir; evrensel bir kesintisizlik garantisi uydurulamaz.

## Bu inceleme şimdi neyi değiştiriyor?

Anayasa, D-025 ve ürün ilkeleri bu gereksinimleri açıkça tanımlıyor ve çelişen
eski ifadeleri gideriyor. İlk kaynak denetimi ve tamamlanma matrisi yapılacak işi
belirledi; yukarıdaki kabul takibi sonraki uygulamaları ve kanıtlarını kaydeder.
Bağımsız kurtarma yürütücüsü ve erişim yolu artık sınırlı kapsamda uygulanmıştır.
Bunların tam kabulü, dosya şeması geçişi ve yerel yenileme geçişi açık P0 işleridir.
Sunucu sahibinin uyguladığı Frankfurt geri alması ve Alpha80 olay düzeltmeleri,
olay ve sürüm notlarında ayrı kaydedilir.

[Debian AL deneme sınırı kabulü](../deploy/e2e/release-recovery/DISPATCH-BUDGET.tr.md),
yeni kitin yeniden başlatmaya rağmen üç kesilmiş girişimi koruduğunu, otomatik
kurtarmayı durdurduğunu ve tek açık sahip tekrarıyla aynı geri almayı tamamladığını
kanıtlar. Yayın sınırındaki güç kaybı, tarayıcı yönlendirmesi ve kalan P0.2/P0.3
matrisi açıktır.

[Arch AN kabulü](../deploy/e2e/release-recovery/DISPATCH-BUDGET.tr.md#arch-an-kabulü),
AL ile aynı adayda üç otomatik deneme sınırı ve açık kullanıcı komutuyla devam
sınırını doğrular. Önceki AM gözlemci hatası sonuçsuz olarak korunur. Birden çok
açılış bekleme yayını, tek kaydın değişmeden korunmasından ayrılır. Yalnız bu
Arch vakası kapanır; yayın anındaki güç kaybı, tarayıcı yönlendirmesi ve bütün
P0.2/P0.3 kabul kapsamı açık kalır.

[Otomatik kurtarma durma yönlendirmesi](RECOVERY-DISPATCH-BUDGET.tr.md#uyumlu-durma-yönlendirmesi-2026-09-22)
için uyumlu üretici/okuyucu sözleşmesi ve çift dilli UI/CLI davranışı eklendi.
Yerel katmanlar arası ve tarayıcı testleri geçti. Bu, P0.2'yi kapatmaz: yeni
bilginin gerçek yerel yürütücüden tarayıcıya kabulü ve Panel durmuşken doğrulanmış
erişim hâlâ kanıtlanmalıdır. AL/AN sonuçları bu yeni özelliğin kanıtı sayılmaz.

[Debian AO yerel kabulü](../deploy/e2e/release-recovery/DISPATCH-BUDGET.tr.md#debian-ao-yerel-durma-yönlendirmesi)
yeni durma kaydının üç gerçek kesintiden sonra seçili CLI'a TR/EN ulaştığını ve
tek kullanıcı devamıyla doğrulanan geri almanın eski durma kaydından üstün olduğunu
kanıtlar. Eski uygulamalar geri yüklenir; sonrasında yetkili HTTP aynı sonucu verir.
Debian'da yeni kayıttan CLI'a kabul kapanır; Panel durmuşken tarayıcı erişimi ve
tüm P0.2/P0.3 matrisi açık kalır.


[Debian AP bağımsız tarayıcı kabulü](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#debian-ap-independent-owner-browser-acceptance)
Panel ve Agent kapalıyken kurulu kurtarma okuyucusunu doğruladı. Kullanıcının
SSH tüneli, gerçek işlemin duraklama bilgisini geçici kodla EN/TR masaüstü ve
mobil tarayıcıya sundu; otomatik deneme kayıtları değişmedi. Ardından desteklenen
kullanıcı devamı eski servisleri geri getirdi; CLI ve yetkili HTTP sonucu eşleşti.
Yalnız bu Debian yedek erişim sınırı kapandı. Normal adresten otomatik erişim,
Arch/eski başlatıcı uyumu ve P0.2/P0.3'ün kalan hata matrisi açık. Önceki yalnız
yerel test sınırı bu AP vakası için aşıldı; tüm erişim kabulü tamamlanmış sayılmaz.


[Debian AR kullanıcı kurtarması kesinti kabulü](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#debian-ar-interrupted-owner-continuation)
kabul kaydından sonra kesilen kullanıcı kurtarmasının otomatik sayacı yenilemeden
durakladığını ve ikinci açık kullanıcı komutuyla aynı geri almanın tamamlandığını
doğruladı. İki kullanıcı ve üç otomatik deneme kaydı korundu; eski sürümün
çalışan süreçleri ile yetkili HTTP/CLI sonucu eşleşti. Yalnız bu Debian kabul
sınırı kapandı. Başarısız devam, Arch, diğer checkpoint'ler ve P0.3'ün kalan
matrisi açık.

Arch AU, gerçek güncellemede yeni firewall unit’i yayımlandıktan sonraki kesintiyi, otomatik geri almayı ve yeni açılışı doğruladı. [Kanıt ve sınırlar](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md#arch-automatic-rollback-and-boot-au). Arch başarılı ileri güncelleme deneyi ve P0.5 bütünü açıktır. Eski Agent’ın geçici platform tespit hatasını kalıcı güncelleme reddi olarak saklaması ayrı bir P0.2 düzeltmesi gerektirir; test Agent’ını yeniden başlatmak bu kabul maddesini kapatmaz.

[Güncel platform gözlemi](UPDATE-PLATFORM-OBSERVATION.md), AU deneyinde bulunan süreç boyu ret önbelleğini düzeltir. Aynı süreçte bekleme/hazır geçişi ve sonradan geçersiz olan önkoşulun Start işlemini engellemesi race testlerinden geçti. Kalıcı şema değişikliği veya otomatik güncelleme tekrarı yoktur. Yeni adayla gerçek açılış/kabul deneyi ve P0.2 geneli açıktır.

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


[Kabul edilmiş mail TLS planı](MAIL-CERTIFICATE-ARTIFACT.md#shared-accepted-mail-tls-plan)
Agent üreticisi ve kurtarma okuyucusunun kullandığı tek v1 sözleşmeye taşındı.
Gerçek Alpha81 üreticisinin boş/SNI kayıtları aynen korunuyor. Kabul edilmiş
planı okumak güncel sağlığı kanıtlamaz ve yeni yardımcıya değişiklik yetkisi
vermez; bağımsız yenileme işlemi ve gerçek sistem kabulü açıktır.


[Ortak mail sertifikası yayını](MAIL-CERTIFICATE-ARTIFACT.md#shared-immutable-publication)
Agent üreticisi tarafından kullanılıyor; hazırlanan dosyalarda ve sertifika
seçiminde gözlenen sahip değişikliklerini koruyor.
[AY gerçek sistem deneyi](../deploy/e2e/release-recovery/MAIL-CONTRACT-AY.md), bu
üretici ve ortak kabul edilmiş plan okuyucusuyla yenilemeyi, yeniden başlatmayı
ve sahip değişikliği sonrası tekrarı doğruladı. Şema geçişi veya bağımsız
yenileme yardımcısı iddiası yok; Agent işlem/kurtarma ve servis uyarlaması
hâlâ gerekli.


[Mail kurtarma temizliği](MAIL-CERTIFICATE-ARTIFACT.md#recovery-cleanup-shares-the-artifact-contract)
artık yalnız makbuz eşleşmesine değil, ortak tam nesil doğrulamasına dayanıyor.
Gözlenen sahip değişiklikleri, seçili nesiller ve ek dosyalar korunur. Bileşen
ve yarış testleri P0.4'ü ilerletir; yeni gerçek sistem kesintili kurtarma veya
bağımsız yenileme kabulü iddiası yoktur.


[Korunan AY sahibin incelemesiyle temizlik deneyi](../deploy/e2e/release-recovery/MAIL-CLEANUP-AY.md)
ortak temizliği gerçek dosya ve mail servisleriyle doğruladı: sahip dosyaları
korunur; sahibi çatışmayı açıkça giderince aynı işlemin hazırlığı temizlenir;
mail ve diğer bekleyen yenileme korunur. Kontrollü tamamlanmamış hazırlık deneyi,
çökme/açılış kurtarmasını veya P0.4/P0.5'in tamamını kanıtlamaz.


[Ortak sunucu kilidi](HOST-MUTATION-EXCLUSION.tr.md), gerçek Agent ve ayrı
kurtarma denetleyicisinde kullanılır. FIFO'da beklemeden veya dosyayı düzeltmeden
sahip değişikliklerini reddeder; mevcut dış flock kimliğini korur. Yerel süreç
ve kurtarma denetleyicisi testleri geçti. Bağımsız yenileme yetkisi/geçici
dizin kurulumu ve P0.3-P0.5'in tam kabul matrisi açıktır.


[Yerel mail yapılandırma sözleşmesi](MAIL-CERTIFICATE-ARTIFACT.md#shared-native-mail-configuration-contract),
gerçek Agent üreticileri ve kalıcı plan karşılaştırmasında paylaşılır. Alpha81
çıktı uyumluluğu, sahip değişiklikleri ve eksik gözlem testleri geçti. Bu
P0.4 ilerlemesidir; yapılandırma eşleşmesi yenileme yetkisi veya hizmet sağlığı
değildir. Agent'tan bağımsız yenileme ve P0.5'in tam kabul matrisi açıktır.


Dovecot lehçesi artık başarılı sürüm gözlemiyle doğrulanır; boş, bozuk
ve başarısız gözlem 2.4 sayılmaz. Desteklenmeyen sürüm ayrı reddedilir.
TLS önkoşulu dosya/snapshot değişikliğinden önce durur; sanal posta yazıcısı
ve kalıcı plan doğrulaması da doğrulanmış lehçe gerektirir. Mail race testleri
ve vet geçti; bu kaynak aşamasında native kabul ve bağımsız yenileme açıktır.


[Sonraki AY yerel sürüm gözlemi kabulü](../deploy/e2e/release-recovery/MAIL-DIALECT-AY.md)
paylaşılan ana makine kilidini ve yerel plan geri okumasını tam kaynak
sürümüyle doğrular. Gerçek hata veren sürüm komutu yapılandırmayı
değiştirmeden durur; yapılandırma, işlem kaydı, bekleyen yenileme ve
güvenilir SMTP/IMAP sertifikası korunur. Bağımsız yenileme, tüm etkin
yerel yapılandırma doğrulaması ve çökme kurtarması açık kalır.


[Ortak hizmet işlem defteri](SERVICE-MUTATION-LEDGER.md), Agent üreticisini
ve bağımsız kurtarma okuyucusunu aynı v1 sözleşmesine bağlar; yayın
makbuzları ve aktif işaretçi kuralları birlikte korunur. Eski üretici
baytları uyumluluk örneğidir; okunamayacak büyüklükte kayıt dosyaya
hazırlanmadan reddedilir. P0.3/P0.4 ilerler; bu tek başına sunucunun boşta
olduğunu veya bağımsız yenileme ve kesinti kabulünün bittiğini kanıtlamaz.

### Posta yenilemesinde yayın öncesi gözlem (2026-09-22)

[Mevcut yapılandırma kontrolü](MAIL-RENEWAL-OBSERVATION.md), ortak salt-okur
gözlemi ilk sertifika yayınına ve kuyruktaki yenilemeye bağlar. Sahibin
ayarlarıyla uyuşmazlık veya belirsiz gözlem sertifika hazırlanmadan durur;
bekleyen kaynak ve mevcut ayarlar korunur. Kalıcı şema değişmez. Yayından
sonraki sahip değişikliklerini koruyan kurtarma ve bağımsız yenileme açıktır;
bu sınırlı değişiklik P0.4/P0.5'i tamamlamaz.

### Posta yenilemesinin işlem yetkisi (2026-09-22)

Kuyruktaki yenileme artık genel kurtarma yöneticisi yerine
[dar kapsamlı yöneticiyi](MAIL-RENEWAL-OBSERVATION.md#scoped-unattended-admission-2026-09-22)
kullanır. Başka işi kurtaramaz, kayıtlarını temizleyemez, kesilen işi
başarısız varsayamaz veya başka sahibin eski işlemini devralamaz. Aynı
host/yayın kilitleri ve v1 işlem defteri korunur. Bu, P0.5 yetki ayrımını
ilerletir; bağımsız yenileme programını veya kesintili yerel kurtarma
kabulünü tamamlamaz.

### Posta sertifikasını devreye alma sınırı (2026-09-22)

Dar kapsamlı yenileme ve seçilmiş sürümün tam işlem kimliğiyle kurtarılması
[yalnız yeniden yükleme yolunu](MAIL-RENEWAL-OBSERVATION.md#reload-only-renewal-and-selected-version-recovery-2026-09-22)
kullanır. Güncel ayarlar ve çalışan servisler doğrulanır; kullanıcının
ayarları yeniden yazılmaz ve durdurduğu servis başlatılmaz. V1 kayıtları değişmez.
İlk kurulum geçişinin yeterli önceki durum kanıtı olmadan kesilmesi, açık bir
kullanıcı kurtarma gereksinimidir. Bağımsız yenileme ve P0.4/P0.5 kabulü tamamlanmadı.


BB yerel posta deneyi; yalnız yeniden yükleme ile yenilemeyi, yeniden
başlatma sonrası SMTP/IMAP hizmetini ve kontrollü yayımlama hatasından
sonra kullanıcı uyuşmazlığı giderdiğinde aynı işlemin kurtarılmasını
doğruladı. Kullanıcı ayarları ve belirsiz etkinleştirme kanıtı korundu.
[Sınırlı kanıt](../deploy/e2e/release-recovery/MAIL-CONTRACT-BB.md),
P0.3/P0.4/P0.5 tamamlandı veya bağımsız yenileme/güç kesintisi
kurtarması kanıtlandı anlamına gelmez.


Bekleyen posta yenilemesi ancak ortak işlem/yayımlama kilitleri altında,
seçili sertifikanın tam işlemine ait başarılı kalıcı yayımlama kaydıyla
silinir. Sertifika eşleşmesi tek başına yeterli değildir; yarım işlem,
belirsiz kanıt ve daha yeni kuyruk korunur. Şema değişikliği veya örtülü
genel kurtarma yoktur. P0.3/P0.5 ve ayrı izlenen yerel kabul işleri açıktır.


[Bağımsız posta yenileme girişi](MAIL-RENEWAL-EXECUTOR.md), ortak uygulamayı
kullanarak yalnız sınırlı kuyruk/işleme ve posta servislerini okuma/yeniden
yükleme eylemlerini sunar. Kurulu hizmet, hook veya unit değişikliği yapılmaz.
Kayıt, kalıcı sahiplik/runtime, yerel kabul ve yarım yenilemenin bağımsız
kurtarılması P0.3/P0.5 kapsamında ayrı ve açıktır.

### Yerel posta yenilemesi için önceki durum kaydı (2026-09-22)

P0.3/P0.5 için [geçiş ve önceki durum sözleşmesi](MAIL-RENEWAL-KIT.md#native-enrollment-before-image-contract-2026-09-22)
eklendi. Gerçek kalıtılmış kilit ve SIGKILL kullanan bileşen testleri yerel
dosyaları ve sahip değişikliği kanıtlarını korur. Bu yalnız hazırlık bileşenidir;
üretimden çağrılma, zamanlayıcıyı devreye alma ve geri alma henüz açılmadı.
Tam yerel güncelleme ve güç kesintisi kabulü açık kalır.

### Yerel posta dosyalarının geri alınması (2026-09-22)

[Özel dosya geçişi sözleşmesi](MAIL-RENEWAL-KIT.md#native-file-transition-and-inverse-exchange-2026-09-22),
P0.3/P0.5 için kalıcı dosya kimliklerini, saklanan eski dosyaları ve yönü
sabitlenmiş geri alma niyetini ekler. Süreç kesintisi testleri yayımı, geri almayı
ve geri almanın yeniden kesilmesini kapsar. Üretimde zamanlayıcıyı devreye alma,
kurtarma bağlantısı, kaldırma ve tam yerel güncelleme/iş yükü matrisi açıktır.

[Debian BE yüklü zamanlayıcı kabulü](../deploy/e2e/release-recovery/MAIL-LOADED-BE.md),
gerçek systemd yeniden yüklemesini iki yönde, iki süreç kesintisini ve aynı işlemin
geri alınmasını doğrular. Gerçek ExecStart nesilleri, zamanlayıcı tercihi ve
posta hizmetleri kontrol edildi. İlk kurulum, üretimden çağırma, eski uygulama
sürümüne dönüş uyumu ve güç kesintisi kabulü açık kalır.

### Başarısız posta yenilemesinin tekrar kabulü (2026-09-22)

P0.3/P0.5: Sonucu başarısız kaydedilmiş yenilemenin otomatik tekrarı, güncel defter
kilit altında okunarak v1 Attempt sayacı üzerinden sınırlandırılır. Üç otomatik
denemeden sonra hata ve kuyruk korunur. Sunucu sahibinin tam işlem kimliğiyle
istediği tek ek deneme sayacı sıfırlamaz; yeni işlem veya seçilmiş sertifika
kurtarması başlatmaz. [Sözleşme](MAIL-RENEWAL-KIT.md#failed-operation-retry-admission-2026-09-22).
Sertifika seçiminden önce kesinti kurtarması, sürümler arası işlem devralma,
üretimde ilk devreye alma ve tam gerçek sistem kabul matrisi açık kalır.

[Gerçek sistem başarısız deneme kanıtı](../deploy/e2e/release-recovery/MAIL-FAILED-BUDGET-BE.md)
üç ayrı yardımcı süreçte ayar çakışmasını koruyarak reddi, otomatik sınırda durmayı,
yanlış işlem kimliğinin reddini ve aynı işlemin açık kullanıcı devamıyla doğrulanmış
posta erişimine ulaşmasını kanıtlar. Otomatik zamanlayıcı çalışması, seçim öncesi
kesinti kurtarması ve üretimde ilk devreye alma bu deneyle kanıtlanmış değildir.

### Ortak DNS motoru kanıt rolleri (2026-09-22)

P0.4 artık tek bir [v1 üretici/okuyucu ve rol sözleşmesi](DNS-ENGINE-ARTIFACT.md)
kullanır. Gerçek Alpha81 edinim ve ekleme/düzenleme/silme üreticilerinin baytları
yeni ortak okuyucuyla aynen korunur. Motor dönemi, sahip ve çift yönü; yayın nesli
ve katalog ilerlemesinden ayrılır. Etkin ağaç kanıtı yine gereklidir. Kalıcı
şemaların ayrılması, kurulu kanıt geçişi ve tam gerçek üretici/geri yükleme matrisi
açıktır; bu kaynak değişikliği kurulu sunucuda geçiş yapmaz.

### İlk posta birimi yükleme sınırı (22 Eylül)

P0.3/P0.5 kapsamında ilk birim yükleme, yenilemeyi etkinleştirmekten ayrıldı.
Ayrı ve işleme bağlı kayıt yalnız devre dışı ve çalışmayan birimlerin yüklendiğini
kanıtlar; dosyalar geri alındıktan sonraki ters yükleme ise birimlerin yokluğunu
doğrular. Başarısız komut tek başına yokluk sayılmaz; ortak okuyucu eksiksiz yerel
kanıt ister. Süreç kesme ve sahip değişikliği testleri [posta yenileme
sözleşmesinde](MAIL-RENEWAL-KIT.md) kayıtlıdır. Yerel kurulum, zamanlayıcıyı
etkinleştirme ve eski uygulamaya geri dönüş uyumluluğu açık kalır.

### Yerel posta etkinleştirme kimliği (22 Eylül)

P0.3/P0.5 kapsamında ilk yerel etkinleştirme için dosya kimliğine bağlı ayrı plan
ve ters işlem eklendi. Yayın, sunucu sahibinin bağlantısını ezemez; etkinleştirme
geri alınmadan birim dosyalarının geri alınması engellenir. Her iki yön de
çalışmayan zamanlayıcıyı ve doğrulanmış yerel yeniden yüklemeyi gerektirir. Bu
özel bir bileşendir; eksik üst dizinin yayını, zamanlayıcının çalıştırılması ve
üretim akışına kabul henüz tamamlanmamıştır. [Sözleşme](MAIL-RENEWAL-KIT.md).

### İlk zamanlayıcı etkinliği ve sınırlı geri alma (2026-09-22)

P0.3/P0.5 özel activity/v1 sözleşmesi, sabit start/stop işlemlerini kesin dosya,
yükleme ve etkinleştirme zincirine bağlar; her yön en fazla üç kalıcı deneme alır.
Sahip değişiklikleri ve bilinmeyen sonuçlar korunur. Etkinlik geri alma, bağlantı
ve dosya geri almadan önce tamamlanır. [BE yerel kanıtı](../deploy/e2e/release-recovery/MAIL-ACTIVITY-BE.md)
gerçek start/stop ve iki süreç kesintisini doğrular. Eksik özel dizinden kaynaklanan
ilk fixture hatası korunmuştur; otomatik yenileme başarısı, üretim kaydı, tarihsel
geri alma uyumu ve bütün yerel hata matrisi açık kalır.

### Korunan ortak zamanlayıcı dizini (2026-09-22)

P0.3/P0.5, eksik ortak systemd wants dizinini özel parent/v1 kaydıyla kalıcı olarak
hazırlar ve yayımlar. Mevcut sahip içeriği korunur; ortak dizin geri almada silinmez.
[BE yerel kanıtı](../deploy/e2e/release-recovery/MAIL-PARENT-BE.md), eksik dizin yayımı,
yönetim yazılımları olmadan otomatik boş-kuyruk çağrısı ve iki süreç kesintisi
sonrası geri almayı doğrular. Özel günlük/grup hazırlığı, gerçek yenileme, üretim
kabulü, tarihsel geri alma ve tam hata matrisi açık kalır.


### Posta yenilemesi öncesi durum kaydı — 22 Eylül

P0.3/P0.5 kapsamında yeni yenilemeler, aktif işlem kaydından önce değişmez
`celikpanel-mail-renewal-before/v1` kanıtını yazar. Ortak salt-okur seçim kimliği,
önceki gerçek sertifika dosyalarını bağlar; aynı baytlarla yapılan sahip değişimini
de algılar. Mevcut v1 istek kimliği, önceki gerçek yerel denemenin üretici çıktısıyla
karşılaştırıldı. Sahip değişimi, belirsiz kanıt, üç gerçek SIGKILL noktası ve durum
kaydının işlem defterinden önce kalıcı olması sınandı. Tarihsel aktif işe eksik
kanıt sonradan üretilmez. Seçim öncesi yerel kurtarma ve tam ürün devreye alma
kabulü hâlâ açıktır; bu dilim P0 işlerini kapatmaz.


### Seçim öncesinde kesilen posta yenilemesi — 22 Eylül

[Yerel BE denemesi](../deploy/e2e/release-recovery/MAIL-UNSELECTED-BE.md), seçimden
önce üç kesinti sınırını ve kurtarma sonucunu yazarken ek bir SIGKILL durumunu
kanıtladı. Değişmez önceki seçim kaydı, aynı kaynak/derleme, değişmemiş hizmet
ayarları ve bitmiş işçi kanıtı; yalnız kesilen denemeyi başarısız kaydetmeye izin
verir. Ardından aynı istek kalan deneme bütçesiyle sürer. Kurtarma hazırlanmış
sertifikayı seçmez veya silmez; bilinen hatayı yok saymaz. İleri yayınlama da
başlangıçtan sonraki sahip değişimini yeniden kontrol eder. Yönetim ikilileri
yokken dört gerçek kesinti ve doğrulanmış SMTP/IMAP sonuçları kaydedildi. İki
başarısız hazırlık ayrı tutuldu. Ürün devreye alma, yeniden başlatma/güç kaybı,
tarihsel eksik kanıt ve derlemeler arası devam hâlâ açıktır; P0.3/P0.5 tamamlanmış
sayılmıyor.

### Yeniden açılışta otomatik posta yenilemesi — 22 Eylül

P0.3/P0.5: [BE gerçek açılış kanıtı](../deploy/e2e/release-recovery/MAIL-UNSELECTED-BOOT-BE.md),
sertifika seçilmeden kesilen aynı işi normal VM yeniden açılışından sonra yerel
zamanlayıcının otomatik tamamladığını gösterir. Panel ve Agent dosyaları yoktu;
açılıştan sonra elle devam komutu çalıştırılmadı. Sahibin ayarları, eski sertifika,
seçilmemiş geçici nesil ve önceki iş kayıtları korundu. SMTP/IMAP yeni sertifikayla
doğrulandı. Bu sınırlı açılış kabulü tamamlandı; güç kaybı, farklı derlemenin işi
devralması, üretim kaydı ve eski uygulama sürümüne dönüş uyumluluğu açık kalır.


## Agent uyumluluğunun işlem öncesinde denetlenmesi (22 Eylül 2026)

P0.3/P0.4/P0.5 için [uygulama uyumluluk sözleşmesi](AGENT-NATIVE-CONTRACT.md)
eklenmiştir. Yeni sürüm bildirimi tam Agent dosyasına bağlanır; güncelleme ve geri
alma, bağımsız posta yenilemesiyle uyumluluğu doğrulanmamış eski yazıcıyı
koordinatörler durmadan reddeder ve son değişiklik kilidi altında yeniden denetler.
Bildirim, Agent ile aynı atomik dizin değişiminde yayımlanır ve aynı geri alma
kanıtıyla korunur. Bu, eski ikililere sonradan uyumluluk verme, üretimde ilk
kaydı etkinleştirme veya tüm gerçek sistem geri alma kabulünü kapatma değildir.


[BE gerçek sistem uyumluluk kanıtı](../deploy/e2e/release-recovery/AGENT-COMPATIBILITY-BE.md),
çalışan bağımsız Debian postasının yanında salt-okur kabulü, Arch'ta kayıt yokluğu
ayrımını ve iki çekirdekte özel test dizinlerindeki atomik yayın/kesilme/tersine
çevirme adımlarını doğrular. Üretimde ilk kayıt ile tüm uygulamanın otomatik geri
alınması bu kanıtın kapsamına girmez.


### Birleşik yerel posta yenileme kurulumu (2026-09-22)

P0.3/P0.5 için dosya, yükleme, etkinleştirme, zamanlayıcı ve tek yönlü geri alma
adımları aynı kabul edilmiş kapsama bağlandı. Gerçek iki kilit ve her çağrıda dış
işlem yetkisi gerekir; geçmiş başarı güncel dosya/hizmet doğrulamasının yerine
geçmez. Aşama başına üç kalıcı daemon-reload denemesi vardır. Bilinen kilit beklemesi
kuyruğu veya işlem sonucunu tamamlamadan sonraki zamanlayıcı çağrısına bırakılır.
[Uygulama ve süreç testlerinin kapsamı](MAIL-RENEWAL-KIT.md#composite-enrollment-execution-2026-09-22)
üretim kabulü, süreçler arası kalıcı engel ve gerçek sistem birleşik kabulünü açık
tutar; P0 tamamlandı denmez.


[BE birleşik native kabulü](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-BE.md)
gerçek Arch systemd üzerinde iki süreç kesintisi ve aynı işlemden kesin geri almayı
kanıtlar. Önceki hazırlık ve envanter kontrolü hataları ayrı tutulmuştur. Üretim
kabulü/kalıcı engel, gerçek posta yükü ve yeniden başlatma bu kanıtın kapsamı dışındadır.

### Kalıcı posta yenileme kurulum sahipliği (2026-09-22)

P0.2/P0.3/P0.5: ortak işlem kaydı, genel başlangıç kurtarmasının, süresi geçen
kiralamanın veya RPC çağrılarının serbest bırakamadığı kesin kurulum sahipliğini
tanır. Özel yazıcı aynı yayın protokolünü release/host/publication kilitleri altında
kullanır; ileri işlem veya geri alma tamamlanmadan güncel sonuç kanıtı ister.
Güncelleyici, etkin veya bilinmeyen işlemi koordinatörleri durdurmadan reddeder.
Geçmiş v1 kayıt baytları korunur; destek yeni isteğe bağlı Agent yetenek beyanında
açıkça belirtilir. [Kapsam ve 12 süreç kesintisi](MAIL-ENROLLMENT-RESERVATION.md)
üretimde doğrulanmış kabul/çalıştırma ile tam gerçek sistem işlem kabulünün yerine
geçmez. Bu bileşen testleri hiçbir P0 işini kapatmaz.


### DNS belge yayımı ve uygulama uyumluluğu (23 Eylül 2026)

P0.4 kapsamında DNS sahipliği ile değişen yayım bilgisi, state/v2 ve ownership/v2
belgelerinde ayrı sürümlü kayıtlar olarak saklanır. Eski v1 günlük/snapshot
baytları değiştirilmez; mevcut atomik yazıcı ve kabul edilmiş DNS işleminin
kilit/kanıt sınırları korunur. Güncelleme hazırlığında eski bir işlemi tamamlama,
DNS biçimini kendiliğinden ilerletmez. Bağımsız salt-okur uyumluluk denetimi,
servisleri durdurmadan ve yayım öncesinde hedef Agent’ın canlı DNS kanıtını
okuyabildiğini doğrular. Uygulama geri alma, sonraki DNS kayıtlarını eski snapshot
ile ezmez; uyumsuz hedefi reddeder.

[DNS geçiş sözleşmesi](DNS-ENGINE-ARTIFACT.md) süreç öldürme, birebir geri alma,
eski/yeni karışık kayıt ve sahip değişikliği testlerini açıklar. Bağımsız DNS işlem
kurtarıcısı ve bütün imzalı güncelleme/otomatik geri alma matrisi hâlâ açıktır.
Bu değişiklik bütün mimari planın tamamlandığı anlamına gelmez.

## Normal panel adresinde çevrimdışı yönlendirme (2026-09-23)

[Statik tarayıcı kurtarma kabuğu](OFFLINE-RECOVERY-SHELL.md), daha önce hazırlanmış
bir tarayıcı panel bağlantısını kaybettiğinde normal sayfa yenilemesinde açılır.
Mevcut işlem kimliği yalnız doğrulanmamış bir referans olarak korunur. Salt-okur
SSH kontrolleri gösterilir; panel gerçekten yanıt verince geri dönme seçeneği
sunulur. Yalnız genel statik dosyalar saklanır; oturum, lisans, API yanıtları veya
değişiklik yetkisi önbelleğe alınmaz. Gerçek Chrome'da bağlantıyı kapatma, İngilizce
masaüstü ve Türkçe mobil kontrolleri geçti. Bu çevrimdışı yönlendirmedir; panel
kapalıyken bağımsız ve kimlik doğrulamalı güncel durum erişimi değildir. Bu sınır
ve P0.2'nin kalan kabul işleri açıktır. Yeni tarayıcı, silinmiş önbellek veya
güvenilmeyen TLS bağlantısında bu kabuğun bulunacağı sözü verilmez.
### Ortak DNS geçiş günlüğü sözleşmesi — 23 Eylül 2026

P0.4 kapsamında tarihsel geçiş günlüğünün kodlayıcısı, okuyucusu, dondurulmuş
dosya/birim görüntüleri, kaynak edinim kimliği ve sabit sunucu yerleşimi kuralları
ortak pakete taşındı. [Kanıt ve sınırlar](DNS-ENGINE-ARTIFACT.md#shared-switch-journal-contract--2026-09-23):
Alpha81 üreticisinin gerçek çıktıları baytları değişmeden okunuyor; Agent ve ortak
paketin yarış denetimli testleri geçti. Günlük v1 ve içindeki kaynak v1/v2 baytları
korunuyor. Bağımsız değişiklik komutu eklenmedi; kabul edilmiş işlem yetkisi,
işçi/sunucu kilitleri ve yerel geri alma yürütücüsünün ayrılması ve kanıtlanması
hâlâ gerekiyor. Bu çalışma P0.4'ü kapatmıyor; kurulu sunucular değiştirilmedi.
Aynı P0.4 sınırında kabul edilmiş işlem ve tamamlanmış defter karşılaştırmaları
`dnsengineartifact.SwitchIdentity` içinde ortaklaştırıldı. Beklenen kimlik de
kanonik olmalı; kayıtlı işçinin biçimi kurtarma yetkisi vermiyor. Tarihsel evre ve
süre aşımı baytları korunuyor. Ortak paket ve Agent yarış denetimleri geçti;
bağımsız sunucu kilitleri, canlılık kontrolü ve yerel geri alma hâlâ açık.
P0.4 için [DNS geçişi kurtarma karar sırası](DNS-ENGINE-ARTIFACT.md#shared-switch-recovery-decision-sequence)
ortak pakete taşındı; Agent da bu sırayı kullanıyor. Günlük v1 ve evre baytları
değişmedi. Hata testleri doğrulama, kayıt ve geri alma sırasını ve belirsizlikte
kanıtların korunmasını denetliyor. Sunucu kilitleri, bağımsız yerel geri alma
ve tam gerçek sürüm kabulü hâlâ açık; kurulu sunuculara dokunulmadı.

P0.4 DNS geçişinde doğrulanamayan hedef için geri alma artık yalnızca hata
dönmesine dayanmaz: yeni otomatik ters işlem, günlüğün dondurduğu kaynak durumunun
kesin kanıtını gerektirir. Tarihsel günlük v1 baytları değişmedi. Belirsiz
denetimler kanıtları korur ve kullanıcı kurtarması gerektirebilir; bağımsız yerel
yürütücü ile tam hata matrisi kabulü hâlâ açıktır.

P0.4, DNS geçişinin v1 günlüğündeki kalıcı geri alma kararı yeniden başlatmada
ileri alma olarak sınıflandırılamaz; rolled-back evresi rolling-back evresine
dönmez. Ortak yarış testleri bu sırayı denetliyor. Bağımsız yerel kurtarma
kabulü hâlâ açık.

Dondurulmuş kaynak durumunun kesin karşılaştırması dnsengineartifact içinde
ortaklaştırıldı ve üç Alpha81 günlük örneğiyle denetlendi. Sunucu gözlemi ve
ters işlem hâlâ Agent'ta; bu bağımsız DNS kurtarması değildir.

Bileşen testi v1 geçiş günlüğünün v2 kaynak belgesiyle uyumunu da doğruluyor;
sonraki yayımı dondurulmuş kaynak saymıyor. Kurulu sistem geçişi ve gerçek
geri alma kabulü hâlâ açık.

### DNS geri alma günlüğü ile nihai işlem kaydı sırası — 25 Eylül 2026

P0.4/P0.3 ve anayasal 1/2/4 ilkeleri kapsamında Agent, geri alınmış DNS
geçiş günlüğünü işlem defterinin aynı isteğe ait kalıcı başarısızlık sonucu
yazılana kadar saklar. Doğrudan BIND geçişi/devralması ile PowerDNS
geçişi/devralması da günlüğü korur. Sonuçtan sonra yalnız birebir eşleşen
günlük silinir.
Aradaki yeniden başlatmada Agent yerel ters işlemi yeniden doğrular; yeni
bir DNS işlemi önceki günlüğü yalnız okur ve kendi yetkisiyle kurtarmaya
çalışmadan durur. Bağımsız salt-okur durum aracı bu kalmış kanıtı tanır
fakat değişiklik yetkisi vermez.
v1 günlük ve defter şemaları değişmedi; mevcut evrelerin kalıcılık sırası
değişti. Bileşen ve Agent testleri bu sınırı denetler. Gerçek süreç öldürme
ve yeniden başlatma matrisi, sahip değişikliği yarışları ve Agent'tan bağımsız
yerel ters işlem hâlâ açıktır; P0.3/P0.4 tamamlanmış sayılmaz.

P0.4 ve anayasal 3. ilke: boşta açılış kurtarmasında DNS günlüğü okunamazsa
veya yerel doğrulama belirsizse günlük korunur, fakat genel işlem yöneticisi
kilitlenmez. Yeni DNS işlemi kendi ön kontrolünde bu günlüğü reddeder; ilgisiz
sunucu işlemleri kendi kilit yolunu kullanabilir. Yerel açılış testi bu sınırı
denetler; gerçek hizmet sürekliliği kabulü değildir.


P0.4, anayasal 1/2/3 ilkeleri: DNS geri alma isleminin kalici basarisizlik
kaydi yazildiktan sonra gunlugun temizlenememesi artik butun Agent'i
kilitlemez ve ortak sunucu islem kilidini tutmaz. Gunluk ve hata DNS'e ozel
inceleme icin korunur; yeni DNS islemi bu belirsiz kaniti yine reddeder.
Etkin islem ve acilista kalan islem ayni siniri kullanir. v1 gunluk ve
defter semalari degismedi. Bilesen testi farkli sahipli gunlugun korundugunu,
kalici sonucu ve sunucu kilidinin serbest kaldigini dogrular. Gercek yeniden
baslatma, sahip degisikligi ve Agent'tan bagimsiz ters islem kabulu aciktir.

P0.4, anayasal 1/2/3 ilkeleri: Agent acilisinda yarim kalan DNS gecisinin
yerel sonucu dogrulanamazsa, yalniz bitmis iscinin kabul edilmis defter
kirasi birakilabilir. Bunun icin donmus gunluk istek, sahip, hedef ve
yeterlilik bilgileriyle tam eslesmeli ve ortak sunucu kilidi tutulmalidir.
`dns_native_recovery_unknown_after_restart` kalici neden kodu eski
belirsiz-birakilmis okuyucusunca taninir; salt-okur
`recovery dns-switch-status` komutu kullaniciya sonraki adimi aciklar. Gunluk
korunur, yeni DNS islemi
durur, sonraki acilis ayni kurtarmayi deneyebilir; ilgisiz sunucu islemleri
devam eder. Eksik, okunamayan veya uyusmayan gunlukte sunucu kilidi
korunur. v1 gunluk/defter semalari degismedi; yalniz ek bir nihai neden
tanindi. Yerel tam/eksik kanit ve ortak yetki testleri gecti. Gercek
yeniden baslatma, sahip degisikligi ve Agent'tan bagimsiz ters islem
kabulu halen aciktir.

P0.5, anayasanın 3/6 ilkeleri — Tarihsel DNS çifti kanıtı düzeltmesi (25 Eylül 2026): 12 Eylül BIND birincil/PowerDNS ikincil testinde katalog üyesi çıkarıldıktan sonra status: REFUSED, sıfır yanıt ve yetkili bayrağının yokluğu kaydedilmişken sonuç başarılı işaretlenmişti. Bu yanıt, ikincil bölgenin kaldırıldığını kanıtlamaz. Ham kanıt korunmuştur; eski test artık belirsizlikte başarısız olur ve İngilizce/Türkçe doğrulama, sahip bağımsızlığı ve Alpha72 sürüm notları kaldırmayı doğrulanmamış sayar. Yerel ekleme/değiştirme/aktarım/DNS hizmeti yeniden başlatma gözlemleri sınırlı kanıt olarak kalır. Bu P0.5 parçasını kapatmak için yeni bir yönetimsiz çift testinde katalogdan çıkarma sonrası ikincilin yerel bölge durumu ve yeniden açılış kanıtlanmalıdır. Kalıcı şema, üretim kurtarma yetkisi ve kurulu sunucu değişmedi.

P0.5, anayasanın 3/6 ilkeleri: [Eski yerel DNS çifti yeniden açılış denemesi](../deploy/e2e/dns-kill-matrix/NATIVE-DNS-PAIR-ARCHIVED-BOOT-RECHECK-20260925.md) başarısız bir kabul denemesidir. 12 Eylül BIND/PowerDNS çiftinin yeni alt disk katmanları, eski imajdaki celikpanel-firewall-restore.service hata verip network-pre.target bağımlılıktan başarısız olunca acil moda düştü. SSH ve çift DNS doğrulanamadı. Bu sonuç güncel imajdaki güvenlik duvarında kusur veya yönetimsiz çift sürekliliği kanıtı değildir. Eski ana diskler değişmedi, alt katmanlar silindi; güncel imajla çiftin açılış/aktarım/kaldırma denemesi açık kalır.

### 27 Eylül: kapsamı belirli DNS kurtarma kanıtı

[Tek sunuculu Debian PowerDNS→BIND geçişini geri alma deneyi](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md),
korumalı bağımsız kullanıcı komutunu gerçek işlem ve kurtarma kesintileri,
sunucu sahibinin değişikliğini koruyarak ret ve düzenli yeniden başlatma
sonrasında aynı isteğin tamamlanmasıyla doğruladı. Panel ve Agent devre dışı
kaldı. Kaynak PowerDNS veritabanı yalnız doğrulanır; değiştirilmez veya geri
yüklenmez. P0.4 içindeki bu yerel kurtarma yolu doğrulandı; çalışan BIND'i
devralma, PowerDNS geçişi/yeniden kurulum geri almaları ve belirtilen diğer
platform/topoloji çeşitleri açıktır. P0.4 tamamlanmış değildir; imzalı sürüm,
açılışta otomatik kurtarma, kesintisiz hizmet veya güç kaybına dayanıklılık
kanıtı sayılmaz.

### Çalışan BIND devralmasının geri alınması — gerçek sistem kanıtı (2026-09-27)

P0.4 ve D-025: [Bağımsız sahip kurtarma denemesi](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-ADOPTION-OWNER-CLI-20260927.md), Debian 13 üzerinde statik bir kullanıcı bölgesini sunan BIND'in kesintiye uğrayan devralmasının geri alınmasını doğruladı. V2 işlem günlüğü `SourceBIND` kanıtı taşır; `SourcePDNS` içermez. Kullanıcı dosyalarının içeriği ve sahipliği, yerel süreç kimliği ve UDP/TCP üzerinden yetkili DNS yanıtları doğrulanır.

Agent, `rolling-back/after-write` aşamasında SIGKILL ile sonlandırıldı. Aynı SOA seri numarasıyla yapılan kullanıcı değişikliği üzerine kurtarma; dosyayı, günlüğü, işlem defterini ve geçici paket sahiplik kaydını değiştirmeden durdu. Deneme düzeneği yalnızca kendi değişikliğini geri aldı. Bağımsız kurtarma aracı da kalıcı `rolled-back` kontrol noktasında sonlandırıldı; aynı istekle tekrar çalıştırılınca tamamlandı. Bu kontrol noktasından sonra ve son tekrarın öncesinde/sonrasında motor durumu ve geçici sahiplik kayıtları yoktu. BIND aynı süreçle çalışmaya devam etti. Son UDP/TCP sorguları yetkili `192.0.2.10` yanıtını verdi. Tamamlanmış isteğin yeniden çağrılması yeni değişiklik yapmadı; güncel sağlık durumunu bilinmiyor olarak bildirdi.

İlk deneme, geçici paket sahiplik kaydı kaldığı için kabul edilmedi ve kanıtı ayrı saklandı. Düzeltilmiş kodla yapılan yeni denemenin arşiv SHA-256 değeri `aa296895caa0dd73bdaea881e64c7dbd6ab2ab52a3e676cfe0397014e2511707`. Denetleyicinin bağımsız kurtarmaya devir sonucu bilerek `unverified` kaldı; son salt-okur gözlem `rolled_back_source_active` ve `converged=false` bildirdi. Bu değerler ileri geçişin başarıyla tamamlandığı anlamına gelmez. Kabul, bağımsız kurtarma ve ayrı yerel DNS kontrollerine dayanır.

Bu denemede yeniden başlatma yapılmadı. Tam kayıt kümesi, kesintisiz hizmet garantisi, diğer platform/topolojiler ve imzalı sürüm kabulü açık kalır. PowerDNS geçiş/yeniden kurulum kurtarmaları ile genel 1. madde/P0.4 tamamlanmış değildir.

### Aday panel başlangıç denetimi ve başlatma sonrası kanıt (2026-09-30)

P0.1/P0.2/P0.3, D-025 ilkeleri 2, 3, 4 ve 5. Paneli açılamayan bir aday geç ya da
hiç fark edilmiyordu: `completion.pending` sonrasında tek `systemctl is-active`,
işçi ve çalıştırıcı son kanıtında HTTP yok ve kurtarma malzemesi varken
`rollback.sh`'nin reddettiği bir geri alma komutu yazdırılıyordu.

- **Değişen.** Güncelleyici, ayrı veritabanı yayımlandıktan sonra ve
  `completion.pending` öncesinde panel hesabıyla salt-okur
  `panel --check-startup-readiness` çalıştırır; hata `active` aşamasında tipli bir
  hatadır (`candidate_panel_startup_check_failed`) ve mevcut kurtarma önceki
  sürüme döner. Gerçek başlatmadan sonra tek `is-active` yerine sınırlı kararlılık
  beklemesi gelir (en az 5 sn aynı ana PID ve sayaç, `RestartSec=3` üzerinde,
  ayrıca paneldan loopback `401 AUTH_REQUIRED`; olağan güncellemede denetlenen
  açık anahtarlara sabitli); hatası `completion` aşamasında kalır
  (`panel_start_unverified`). Tamamlanma özeti geri alma komutunu yalnız
  `rollback.sh` kabul edecekse yazar. Root CLI, sahip görünümü ve kurtarma ekranı
  iki nedeni EN/TR açıklar.
- **Şema veya sürüm geçişi yok.** Snapshot v6, malzeme v3, işaretçi dilbilgisi,
  kit protokolü 1 ve sekiz alanlı v1 gözlem kaydı değişmedi. Tipli neden isteğe
  bağlı, ek bir `<id>.failure` dosyasıdır (`celikpanel-recovery-failure/v1`, ilk
  neden geçerli); eski okuyucular açmaz. Eski tarayıcılar ek JSON alanını yok sayar.
- **Kurtarma davranışı.** Değişmedi: `update:active` geri alır,
  `update:completion` ileri tamamlar.
- **Kanıt.** Yalnız bileşen ve sözleşme testleri; gerçek sistem denemesi bekliyor.

Açık: denetimden geçip sonra çöken panel ileri tamamlanır ve desteklenen dönüş
yoktur; `completion.pending` sonrasında geri alma yoktur; panel dururken panel
adresinde canlı tarayıcı durumu yoktur (yalnız sahip SSH görünümü).

### Doğrulanmış geri almadan sonraki yönlendirme (P0.2, 2026-10-01)

P0.2 (doğru ve uygulanabilir durum), D-025 ilke 3 (tipli kanıt), D-024. upd2
Debian 13 kusurlu aday denemesinden (O1-O4).

- **Değişen.** Başarısız güncelleme bildirimi, aynı isteğin kurtarma gözlemini
  sahip yönlendirmesine eşler (geri alma doğrulandı: iki sürüm, tipli ya da genel
  neden, kim, sonraki eylem, devam; aksi hâlde kurtarma ekranının durum
  metinleri) ve işçi özetini ikincil "sunucunun bildirdiği" satırında tutar.
  Güncelleme denetimi, sunulan hedef commit için yerel gözlemlerden
  `previous_attempt` ekler (salt-okur, Agent RPC yok, sınırlı dizin taraması,
  bozuk ya da yabancı kayıtlar yok sayılır). Root CLI önce sade konuşur,
  belirteçleri son "Kayıtlı durum (destek için)" satırına taşır. Geri alma
  günlüğü, manifestin kapsadığı Agent yapı kaydı kurulu Agent baytlarına bağlıysa
  geri yüklenen commit'i yazar. Başlatma koruması bekletmeyi adlandırır. Deneme
  sınırında duraklatılmış kurtarma, duraklatma yönlendirmesinden önce
  güncellemenin tipli ilk nedenini de (mevcut hata ek dosyasından okunur) söyler.
- **Şema veya sürüm geçişi yok.** Gözlem v1, hata ek dosyası v1, snapshot v6,
  malzeme v3, işaretçi dilbilgisi, kit protokolü 1, tarayıcının sakladığı
  güncelleme kaydı ve CLI `--json` baytları değişmedi. Tek iletişim eki,
  `GET /api/v1/panel/update/check` yanıtındaki isteğe bağlı `previous_attempt`
  alanı ile kurtarma durumunun isteğe bağlı `first_failure_code` alanıdır (en
  sona eklenir; yalnız otomatik kurtarma duraklatılmışken ve ek dosya tipli bir
  neden bildirirken bulunur; CLI `--json` çıktısında da yalnız o durumda); eski
  Panel'ler göndermez, eski tarayıcılar yok sayar. Başlatma
  korumasının baytları değiştiği için, bu kaynaktan üretilen sürümlerde temel
  manifestindeki `start-guard-sha256` alanının biçimi değil değeri değişir.
- **Kurtarma davranışı.** Değişmedi: `update:active` geri alır,
  `update:completion` ileri tamamlar; koruma aynı çıkış koduyla aynı biçimde kabul
  ve ret eder; Başlat sahibin kararı olarak kalır.
- **Kanıt.** Yalnız bileşen testleri; gerçek sistem denemesi bekliyor.

### Ön denetim nedeni, denemeler arası yeniden deneme ve duraklamada yenileme (P0.1/P0.2/P0.5, 2026-10-01)

D-025 ilkeleri 1 (bağımsız yenileme askıda kalmamalı), 2 (tipli, korunan kanıt),
3 ve 4 (bir okumanın sınırlı yeniden denenmesi; deneme hakkı bitince kalıcı ve
uygulanabilir durum); D-022, D-024. upd3 gerçek sistem denemesinden (F1, F2, F3, O6).

- **Değişen.**
  - F1: `recovery verify-compatibility` `step=<adım>: <denetleyicinin ilk satırı>`
    döndürür (sınırlı, yazdırılabilir ASCII; iç çalışma ortamı, kilit ve üst veri
    hataları gizli kalır, yalnız adımları adlandırılır). `update.sh` seçili çalışma
    ortamının dört ön denetim adımını yakalar, EXIT tuzağı henüz yokken
    `code=recovery_runtime_preflight_failed state=unchanged` bildirir ve hata ek
    kaydını bu kodla yazar (anlık görüntü adı yokken commit doğrulanmış hedeften).
    Bu kodu taşıyan başarısız kayıt, isteği için sondur.
  - F1 yarışı: kod okumasıyla kanıtlandı, sunucuda gözlenmedi. WAL uyumlu
    denetleyici canlı veritabanını ve WAL'ı açmadan kopyalar; sabitlemeden sonra
    veritabanı, `-wal` veya `-shm` üst verisi değiştiyse kapalı kalarak reddeder
    (`verifyPath` boyut, mtime ve ctime karşılaştırır). Bu aralıktaki her Panel
    commit'i, checkpoint'i ya da paylaşılan bellek yazımı denetimi reddettirir ve
    ön denetim Panel çalışırken yapılır. Kaynağı açmadan ya da Panel'i durdurmadan
    tutarlı bir anlık görüntü mümkün değildir, ön denetim bunları yapamaz; bu
    yüzden reddeden denetleyici 2 sn'lik sınırlı bir beklemeden sonra bir kez daha
    okunur (zaman aşımından sonra asla). Aynı tek yeniden okuma yükseltme uyumluluk
    denetimine de uygulanır. Güncellemenin sonraki donmuş ve kilitli denetimleri
    değişmedi.
  - F3: deneme hakkı kalan başarısız bir otomatik deneme isteğe bağlı
    `celikpanel-recovery-automatic/v1` değeri `retry_scheduled`'ı yazar (yalnız
    `recovery_required/recovery_failed` kaydında). Okuyucular güncellemenin ilk
    tipli nedenini yeniden denerken (`retry_scheduled` ve kurtarma hatasından
    sonra `recovering`) ve duraklamada gösterir.
  - F2: bir ileri tamamlamanın (`update:completion`, `completion-scheduler`,
    `scheduler`) son kabul edilen denemesi (üçüncü otomatik deneme ya da sahip
    yeniden denemesi) başarısız olunca çalıştırıcı Certbot zamanlayıcısını mevcut
    geri yükleme işleviyle, bir kez ve sürüm kilidi altında kayıtlı güncelleme
    öncesi durumuna döndürür. Zaten eşleşiyorsa hiçbir şey değişmez; reddedilen
    bir geri yükleme (örneğin sahibin sonradan etkinleştirmeyi değiştirmesi)
    bildirilir, zorlanmaz. İleri yeniden deneme anlık görüntüyü doğruladıktan hemen
    sonra ve herhangi bir koordinatörü durdurmadan önce zamanlayıcıyı yeniden
    duraklatır; mevcut duraklatma kanıtı değişmiş etkinleştirmeyi reddeder. Geri
    alma yönleri (`update:active`, `rollback:*`) yenilemeyi duraklatılmış tutar ve
    nedenini söyler: geri alma panel TLS ağacını, bekleyen etkinleştirmeyi ve
    dağıtım kancasını anlık görüntüden geri yükler; yeniden denemeden önceki bir
    yenileme geri alınırdı. Kod kanıtı: ileri yeniden deneme
    (`validate_pending_update_snapshot`, `verify_installed_release_artifacts`,
    `VerifyInstalledCompletion`) yalnız anlık görüntü kopyasını doğrular, canlı
    sertifika dosyalarını karşılaştırmaz ve geri yüklemez; `panel_tls_restore_snapshot`
    ise geri alma yolunda canlı TLS ağacını değiştirir.
  - O6: bildirimin ikincil sunucu satırı iç belirteçleri içermez.
- **Şema veya sürüm geçişi: yok.** Gözlem v1, hata ek kaydı v1 dilbilgisi,
  otomatik ipucu v1 dilbilgisi, anlık görüntü v6, malzeme v3, işaretçi
  dilbilgisi, dağıtım makbuzları v1 ve kit protokolü 1 değişmedi. Yeni kapalı
  değerler: `recovery_runtime_preflight_failed` hata kodu ve `retry_scheduled`
  otomatik değeri; eski okuyucular ikisini de yok sayar ve genel metni gösterir.
  Durum JSON'u her anahtarı ve konumunu korur; `first_failure_code` artık yeniden
  denerken de görünebilir. Çalıştırıcı, güncelleyici ve `panel-tls-snapshot.sh`
  baytları değiştiği için temel ve kit manifestlerinin biçimi değil değerleri
  değişir.
- **Kurtarma davranışı.** Dağıtım, deneme hakkı (üç otomatik kabul ve açık sahip
  yeniden denemeleri) ve son kanıtlar değişmedi. Yeni değişiklikler: başarısız son
  ileri denemeden sonra zamanlayıcının geri yüklenmesi ve yeniden denemenin
  başında yeniden duraklatılması; ikisi de mevcut, doğrulanmış işlevlerle.
- **Kanıt.** Yalnız bileşen ve sözleşme testleri; gerçek sistem denemesi bekliyor.

Açık: duraklatılan bir geri alma, yeniden denemesi bitene kadar yenilemeyi
durdurulmuş bırakır; bağımsız `deploy/finalize-pending-update.sh` başlangıcında
yenilemeyi yeniden duraklatmaz; ön denetim duruşu bildirimi bu değişikliği içeren
kurulu bir Panel gerektirir; bu dört adım dışında EXIT tuzağından önceki durma
satırları hâlâ tipli özet taşımaz (örneğin `*_runtime_preparation_unconfirmed`).

#### Sahibin devamında başlatma sınırı (aynı tarih)

`celikpanel-panel.service` hata durumunda 3600 sn'de `StartLimitBurst=30` ile
yeniden başlar ve hiçbir yaşam döngüsü betiği birimin sayacını sıfırlamıyordu.
Çöken bir adayla üç ileri denemeden sonra sahibin yazdırılan yeniden denemesi
"start request repeated too quickly" ile reddedilebilirdi. Her betiğin mevcut
`systemctl` sarmalayıcısı artık yalnız `start celikpanel-panel.service` ve
`start celikpanel-agent.service` çağrılarını `release_unit_controlled_start`
üzerinden geçirir; bu yalnız o birim için `reset-failed` çalıştırır, sonra onu
başlatır. `Result=start-limit-hit` ile yine reddedilen başlatma, birimi ve
`sudo systemctl reset-failed <birim>` komutunu ardından yeniden denemeyle birlikte
`code=unit_start_limit_hit` olarak EN ve TR yazar; güncelleyici kodu özetinde de
taşır. Kapsanan başlatma yerleri: `update.sh` (normal denetimli başlatmalar,
sahip yeniden denemesi dahil bekleyen/ileri tamamlama, değişiklik öncesi yeniden
başlatma), `rollback.sh` (geri yüklenen başlatmalar, önceki hizmetlerin yeniden
başlatılması), `deploy/finalize-pending-update.sh`,
`deploy/finalize-pending-rollback.sh` ve `deploy/abort-pre-mutation-active-update.sh`.
Kurtarma çalıştırıcısı ve Go kurtarma ortamı bu birimlerden hiçbirini başlatmaz
(bu betikleri çalıştırır). `install.sh` yalnız ilk kurulumda yeniden başlatır
(yalnız-uygulama kipi daha önce çıkar) ve değişmedi. Şema geçişi yok; sözleşme
testi `deploy/test-unit-start-limit-contract.sh`. Kod bir gözlem değeri değildir;
sahip onu kurtarma günlüğünde okur.
