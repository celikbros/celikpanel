# Güncellemeyi bağımsız tamamlama

*15 Eylül 2026 · [English](RECOVERY-FORWARD-COMPLETION.md) · D-025 / P0.3*

Bu dilim, **tam v6 yedeği olan ve tamamlanmayı bekleyen güncellemenin** desteklenen
yolunda saklanan aday verisine bağımlılığı kaldırır. Bağımsız geri alma ve kurtarma
ortamı geçişini genişletir; bütün dayanıklılık matrisini tamamlamaz.

## Sürüm geçişi

Yeni hazırlık `celikpanel/recovery-material/v2` üretir. Dizin düzeni
`recovery-material/v1/<yedek-adının-sha256-özeti>` olarak kalır; dizin düzeni ve
kayıt şeması ayrı sürümlerdir. Yedek v6, dosya değişimi yayınlama niyeti v2 ve kurtarma protokolü
1 değişmez. Eklenen tek karşılaştırma dosyası `libexec/get.sh` olur. Adayın
Panel/Agent/web ürünleri ile install/rollback girişleri veri kitine kopyalanmaz;
tamamlama yordamı bunları çalıştırmaz.

Koordinatörler durmadan önce seçili ortam tam
`verify-material-support --layout snapshot-name-sha256-v1 --schema celikpanel/recovery-material/v2`
yeteneğini desteklemelidir. Mevcut v1 kayıtlar geri alma için okunmaya devam eder.
Kayıtlar yeniden yazılmaz, eksik v2 kanıtı sonradan üretilmez.

Bin veya web ağacı değişmiyorsa yayınlama, ayrı
`celikpanel/recovery-resource-noop-intent/v1` kaydı ve yayın makbuzu oluşturur.
Kurulu ağaç değiştirilmeden tam önceki/sonraki kimlik bağlanır. Bu kayıt yalnız
material-v2 ileri güncellemede, değişim hazırlığı olmadan ve önceki/sonraki ağaçlar
tam aynıyken geçerlidir. Mevcut değişim niyeti v2 biçimi değişmez. Eski v1'in
atlama davranışı kendi doğrulanmış aday yolunda kalır. Bu tarihsel durumda çıkış
6, veriyi ve mevcut durumun eski aday yoluna uygunluğunu doğrular; eski üreticinin
hiç kaydetmediği tam yayın kimliğini kanıtlayamaz. V2 tamamlama, içerik eşitliğine
bakıp eksik kayıt üretmez.

## Desteklenen devam yolu

Yürütücü yalnız aynı kabul edilmiş güncelleme, tam yedek ve `completion.pending`,
onunla eşleşen zamanlayıcı işareti veya yalnız zamanlayıcı işareti ile devam eder.
`completion-material-root --snapshot NAME` doğrulanmış v2 veri dizinini döndürür.
Çıkış 3 doğrulanmış yokluk, çıkış 6 tamamen doğrulanmış eski v1 kayıt demektir.
Yalnız bu iki durumda sağlam saklanan adayın eski yolu kullanılabilir. Bozuk,
yabancı, okunamayan veya yayınlama kanıtıyla ilişkili eksik veri eski yola
indirgenemez. Saklanan güncelleyiciyi doğrudan çalıştırmak bu kuralı aşamaz.
Yeni güncelleyici eski doğrudan tamamlama için de uyumlu seçili okuyucu ister.
Eksik veya eski başlatıcı yokluk kanıtı değildir. Değişmemiş tarihsel saklanan
güncelleyiciler kendi eski davranışlarını korur.

`verify-installed-completion --snapshot NAME` salt okunurdur. V2 veri için bin/web ürünlerini
tam yayınlama niyeti ve yayın sonrası durumuyla; dosya sistemi kimliği, metadata
ve içerik üzerinden doğrular. Makul görünen hedef veya aynı dosya içeriği tek
başına yetmez. Sahibin değişen kaynakları korunur; kurtarma devam etmez. Tam yedek,
veritabanı, TLS, servis unit'leri, kurtarma altyapısı, yerel servis durumu ve zamanlayıcı
kontrolleri zorunlu kalır. Yeni ürün yayımlanmaz, aday migration'ı çalıştırılmaz.
Kontrollü servis başlangıcından önce bağımsız okuyucu
`--check-completed-update-database-wal-aware` ile tam gömülü migration geçmişini,
bütün şemayı ve boş işlem kuyruğunu aynı özel WAL kopyasında doğrulamalıdır.
Yalnız kuyruğun boş olması veritabanının hazır olduğunu kanıtlamaz.

Tamamlama işareti veritabanı dönüşümünden önce bulunabilir; **veritabanının hazır
olduğunu kanıtlamaz**. Bağımsız okuyucu bunu doğrulayamazsa aynı işlem ve kanıtlar
korunur. Bu dilim dönüşüm durumunu tahmin etmez. Günlükteki
`CELIKPANEL_UPDATE_CHECKPOINT database_verified_before_start` satırı gözlemdir;
kalıcı yetki veya başarı kaydı değildir.

Dönüşüm öncesi kesinti penceresi açık bir P0.3 eksiğidir. Yeni migration gereken
bir güncelleme, `completion.pending` yayımlandıktan sonra fakat dönüşüm bitmeden
kesilirse katı okuyucu ileri tamamlamayı reddeder. Sabit sahip kurtarma girişi aynı
tamamlama yolunu seçer; saklanan update/rollback girişleri bu veriye dayalı kabul
kuralını aşamaz. Bu birleşim için şu anda desteklenen otomatik telafi veya sahibin
uygulayabileceği devam yolu yoktur. Sonraki kontrol noktası geçişi, veritabanının
hazır olmasını ayrı göstermeli; adaydan bağımsız devam veya yedeğe dönüşü
kanıtlamalıdır. Okuyucu gevşetilmemeli, mevcut işaretten hazır olunduğu çıkarılmamalıdır.

Material-v2 yolu kontrollü başlangıçlardan sonra, zamanlayıcı yükümlülüğü
yayımlanmadan önce ve zamanlayıcı geri yüklendikten sonra son işaret silinmeden
hemen önce kurulu ürünleri ve kaydedilmiş servis çalışma/etkinlik durumunu yeniden
doğrular. Kontrol başarısızsa tam işlem kanıtları kalır, başarı yayımlanmaz. Geç
zamanlayıcı yolundaki hata koordinatörleri yeniden başlatmaz. Bunlar tamamlama
anındaki gözlemlerdir; kesintisiz sağlık güvencesi değildir.

Yürütücü ancak gerçek süreçler ve zamanlayıcı doğrulandıktan, tam işaretler
kaldırıldıktan sonra `succeeded / update_verified` yayımlayabilir. Durum sorgusu
yeni değişiklik başlatamaz. Sahibin kurtarma komutu
`sudo /usr/libexec/celikpanel/recovery recover` olarak kalır. Yeni güncelleme
komutu, lisans atlama veya serbest komut çalıştırma arayüzü eklenmez.

## Kabul sınırı

Gerekli testler iki şemayı, üç geç tamamlanma işareti düzenini, bozuk veri ve
makbuzları, eksik aday dosyalarını, değişmiş kurulu kaynakları ve root/kilit/CLI
sınırlarını kapsar. Gerçek sistem kabulü, veritabanı doğrulandıktan sonra tam
güncelleyiciyi aday verileri yokken öldürmeli; otomatik tamamlama ve hizmetlerin
korunduğunu gözlemlemelidir. Bileşen testleri tek başına bunu kanıtlamaz.

Eksik yedek alma, desteklenmeyen veritabanı geçişleri, yeni tarihsel geri alma
işlemi, tüm hata/reboot matrisi, imzalı Agent kabulü, metadata geçişleri ve kanıt
temizliği açıktır. [Gerçek O ve P deneyleri](../deploy/e2e/release-recovery/FORWARD-COMPLETION.tr.md),
temiz Arch ve Debian 13 konuklarında üç saklanan aday dosyası karantinaya
alındıktan ve tam güncelleyici öldürüldükten sonra sınırlı otomatik tamamlamayı
kaydeder. P, son kanıt kontrollerini içeren nihai kaynağı çalıştırır. Değişmeyen
şema, başlangıç TLS'i ve etkin olmayan zamanlayıcı sınırları açık kalır; çalışma
kurulu kullanıcı panellerinde güncelleme başlatmadı.

Sonraki kaynak dilimi material v3 ile [ayrı veritabanı dönüşümünü](RECOVERY-ISOLATED-DATABASE.tr.md) getirir. Normal güncelleme, ayrı dönüştürülen kopya doğrulanıp yayımlanana kadar active kalır. O dilimin uyumluluk ve sınırlı Q/R gerçek sistem kabul kaydı ayrıdır; burada anlatılan tarihsel v2 davranışı geriye dönük değiştirilmez.
