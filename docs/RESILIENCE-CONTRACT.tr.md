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

Yukarıdaki P0 kimlikleri, takip edilen iş kalemleridir. Başlangıç durumları şöyledir:

| İş | 14 Eylül'deki durum | Tamamlanma kanıtı |
|---|---|---|
| P0.1 | Kısmi — Arch ve Debian’da bir kontrol noktasında gerçek eski sürüme dönüş geçti | [Birim geçişi kabulü](../deploy/e2e/release-recovery/UNIT-TRANSITION.tr.md), SIGKILL sonrası gerçek geri yüklemeyi ve çalışan eski dosyaları kaydeder. Geniş hata/hizmet matrisi, tutarlı veritabanı içeriği ve imzalı aday kabulü hâlâ açıktır. |
| P0.2 | Kısmi — türlenmiş erişim, Agent bağımsız başlangıç gözlemi ve yerel kurtarma girişi uygulandı | [Erişim/gözlem kabulü](RECOVERY-ACCESS.tr.md) ve [bağımsız kurtarma ortamı](RECOVERY-RUNTIME.tr.md). Root/sudo durum ve kurtarma yolu Panel/Agent başlangıcına veya lisansa bağlı değildir; tam yerel kesinti/erişim matrisi açıktır. |
| P0.3 | Kısmi — bağımsız kod/veri, atomik yayın ve seçili gerçek kurtarma SIGKILL/reboot sınırları geçti | [Bağımsız çalışma ortamı](../deploy/e2e/release-recovery/INDEPENDENT-RUNTIME.tr.md) ve [veri kabulü](../deploy/e2e/release-recovery/RECOVERY-MATERIAL.tr.md): saklanan adayın üç dosyası yokken Arch payload_restored SIGKILL ve Debian runtime_verified reboot aynı geri almayı otomatik tamamladı. Tam aşama matrisi, imzalı kabul, eksik yedek/ileri tamamlama veri bağımsızlığı, seçili kurtarma ortamının yükseltilmesi, metadata geçişleri ve temizleme açıktır. |
| P0.4 | Kısmi — Alpha80'de sınırlı BIND ilerleme denetimi/ön kontrolü var; şema ayrımı tamamlanmadı | Gerekli: korunan TLS kanıtı ve sahibin değişiklikleri dahil, desteklenen tüm gerçek üreticiden okuyucuya ve geri yükleyiciye geçiş sonuçları. |
| P0.5 | Açık — belgelenmiş yenileme/güvenlik duvarı bağımlılıkları sürüyor | Gerekli: desteklendiği söylenen her yerel hizmet birleşimi için yönetim bileşenlerinin kaldırılması/yokluğu ve yeniden başlatma kontrolleri. |

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
