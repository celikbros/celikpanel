# Bağımsız kurtarma çalışma ortamı

*14 Eylül 2026 · [English](RECOVERY-RUNTIME.md) · D-025 · P0.2/P0.3*

Sunucu sahibi, panel açılışı, Agent RPC bağlantısı, uygulama şema geçişi veya lisans
kontrolü çalışmadan tam güncelleme işlemini sorgulayabilir ve mevcut kurtarmayı
sürdürebilir. Bu dar girişte kimlik doğrulama işletim sisteminin root/yetkili sudo
oturumudur. Ayrı HTTP dinleyicisi veya ikinci bir oturum yetkisi oluşturulmaz.

## Sınır

`/usr/libexec/celikpanel/recovery status --request-id <id> --json`, panelin de
kullandığı sınırlı gözlem sözleşmesini okur. Sıfır çıkış kodu bilinen gözlem demektir;
güncellemenin başarılı olduğunu söylemez. Bilinmeyen sonuç bilinmeyen kalır.
`recover` yalnız kalıcı olarak başlamış işlemi yerel sürüm kilidi altında inceler.
Yeni kurulu-panel güncellemesi başlatamaz veya keyfi hedef alamaz. Her kurulu-panel
güncellemesini kullanıcı panelin kendi arayüzünden başlatmaya devam eder.

İlk kabul edilmiş kurulum/güncelleme, koordinatörleri durdurmadan doğrulanmış
kurtarma paketini kaydeder. Kod
`/usr/libexec/celikpanel/recovery-runtimes/v1/<manifest-sha256>` altında;
root erişimli seçim `/var/lib/celikpanel-release-state/recovery-runtime.v1`
dosyasındadır. Tam paket ve başlatıcı diske kalıcı yazılmadan seçim yayımlanmaz.
İlk kayıt mevcut uyumlu seçimi korur. Ayrı kayda bağlı
[kurtarma ortamı geçişi](RECOVERY-RUNTIME-PROMOTION.tr.md), kabul edilmiş
güncellemenin ön kontrolünde yeni kiti hazırlar; önceki kiti korur ve doğrular.
Seçilmiş kodun kayıp/bozuk olması yeni adayla üzerine yazma veya aday yaşam
döngüsü koduna geri dönme yetkisi değildir. Yarım kalmış paketler seçilmeden
kanıt olarak saklanır.

Kanonik manifest protokol1, snapshot6, on iki dosya, izinler, SHA-256 ve tam
envanteri sabitler. Okuyucu root sahipliğini, yol zincirini, bağlantıları, boyut
sınırlarını ve dosya kimliklerini denetler; açık dosyaları tutup çalıştırmadan
önce kanıtı tekrarlar. Bu, son kontrolden sonra keyfi eşzamanlı root müdahalesine
karşı sınırsız bir garanti değildir.

## Mevcut işlem ve snapshot geçişleri

Çalıştırıcı kayıtlı paketin scriptlerini ve çevrimdışı denetleyicilerini kullanır.
Başarısız hedef sürüm ayrı veri/köken kaynağıdır. Panel/Agent denetleyicileri normal
main/RPC/HTTP başlangıcı olmadan üretimdeki aynı snapshot ve işlem kaydı kodundan
derlenir; ikinci bir ayrıştırıcı yazılmaz. Şema17 köprüsü de paketin içindedir.

| Kalıcı durum | Bağımsız davranış |
|---|---|
| İşlem yok | Değişiklik başlatılmaz. |
| Kilit canlı güncelleyicide | Yarışılmaz; yerel kurtarma zamanlayıcısı tekrar dener. |
| Quiesce yakalama | Tam active-öncesi işlem mevcut kanıtıyla iptal edilir. |
| Active, eksik snapshot | Doğrulanmış snapshot tamamlanır; aday kurulmadan paketin geri alma yoluna geçilir. |
| Active, tam snapshot | Tam olarak o snapshot geri yüklenir. |
| Tamamlama/zamanlayıcı temizliği | Mevcut çalışma durumu doğrulanır ve kayıtlı temizlik tamamlanır; aday şema geçişi çalıştırılmaz. |
| Bilinmeyen/uyuşmayan kanıt | İşlem korunur, ilgili eylem reddedilir, bağımsız durum sorgusu kullanılabilir kalır. |

Yalnız dış snapshot biçimi6 kabul edilir. İçindeki normal, işlem kaydı öncesi
şema20 ve şema17 köprü geçişlerinin açık kuralları korunur. Biçim4/5 sessizce
başka anlamda yorumlanmaz. Kilit tanıtıcısı, durmuş koordinatörler, tam kalıcı
işlem kimliği, snapshot bütünlüğü ve sahip değişiklikleri kontrol edilmeye devam eder.

## Kanıt ve kapsam

- Manifest, yol/sahiplik/FIFO/bağlantı/içerik/değiştirme testleri ve gerçek root
  miras FD kayıt testleri bağımsız giriş sınırını denetler.
- Çevrimdışı denetleyiciler gerçek SQLite şemaları/WAL ve işlem kayıtlarıyla
  sınanır; başarılı alt süreç çıkışı gerçek geri yüklenmiş sistem kanıtı sayılmaz.
- Geçici VM toplayıcısı bütün tabloları tek tutarlı SQLite okuma işleminde
  karşılaştırır. Oturumlar ve işlemler dışlanmaz. SQLite WAL koordinasyon
  metadata farkları raporlanır; DB/WAL içerikleri yeniden yazılmaz.
- Kurtarma sürecinin öldürülmesi ve yeniden başlatılması ayrıca gerçek VM
  matrisiyle kanıtlanır; bileşen testlerinden çıkarılmaz.

Bütün desteklenen aşamalar sınanmadan P0.3 kapanmaz. Aşağıda tanımlanan bin/web
kaynaklarının kabul edilen program yayını atomiktir. Diğer geri yükleme adımları
sürüm scriptleriyle ortak sözleşmeleri kullanır; bu dilim TLS normalizasyonunu
salt-okur veya adayın veri bütünlüğünden tamamen bağımsız yapmaz. [Seçili kit geçişi](RECOVERY-RUNTIME-PROMOTION.tr.md), uyumluluk
kontrollerini, önceki kitin korunmasını ve ayrı gerçek sistem kabulünü kaydeder.
Yeni kurtarma protokolü yine açık bir uyumluluk geçişi gerektirir. P0.4 ortak veri sözleşmeleri ve P0.5 bağımsız yenileme/açılış kanıtı
ayrı işlerdir.

## Servisler durdurulmadan önce uyumluluk

Kayıttan sonra aday CLI, seçili kitin çevrimdışı okuyucularıyla mevcut normal,
pre-ledger veya schema17 durumunu release kilidi altında denetler. Bu kontrol
quiesce niyeti yazılmadan ve koordinatörler durdurulmadan önce yapılır. Okuyucu
uyumsuzsa panel hâlâ çalışırken güncelleme reddedilir; bu sonuç gelecekteki geri
almanın başarılı olduğunu kanıtlamaz.

Normal geri alma, zaten kabul edilmiş Agent işlem kaydının inode'unu yerinde
korur; kilit, tam içerik, sahiplik ve izin kanıtını tekrarlar. Kaydı silip yeniden
yazarak kesinti aralığı oluşturmaz. Eksik veya sahibi tarafından değiştirilmiş
kayıt reddedilmeye devam eder.

Kayıt işlemi başlatıcıyı yayımladıktan, seçiciyi yazmadan önce kesilirse aynı kit
ile tekrar desteklenir ve gerçek SIGKILL ile sınanmıştır. Farklı bir kitin
başlatıcısına otomatik geçilmez: ilk kit ve başlatıcı korunur; seçici uydurulmaz.

## Atomik program yayını

Seçili kurtarma programı hem güncellemede hem geri almada
`/opt/celikpanel/bin` ve `/opt/celikpanel/web` dizinlerini yayımlar. Kapalı iç
komutlar yalnız kaynak, snapshot ve korunan aday manifest kimliğini kabul eder;
işlem token'ını parametreden almaz, mevcut yerel işlemden okur. Çalışan programın
hash'i kaynak değişikliğinden önce seçili kitle aynı olmalıdır.

Tam özel dizin doğrulanıp fsync edildikten sonra root erişimli değişmez kaynak
niyeti yazılır. Aynı dosya sistemindeki atomik dizin takası bütün dizini yayımlar
ve çıkarılan dizini korur. Tekrar giriş, önceki/sonraki inode, içerik ve metadata
çiftini doğrular. Bilinmeyen ekler, bağlantılar, sahip değişiklikleri ve kanıtsız
eksik dizinler korunup reddedilir. Yalnız panel/Agent değişirken kabul edilmiş ek
bin dosyaları korunur. İlk kurulum kendi kabul yolunu kullanır; güncelleme snapshot'ı
uydurmaz.

`celikpanel/recovery-resource-intent/v1` şeması yerel token özeti, snapshot v6
manifesti ve aday manifestine bağlıdır. Kesilen staging ve çıkarılmış dizinler
`.recovery-publications` altında kanıt olarak kalır; bu dilimde otomatik temizleme
yoktur. Eski, kayıtsız kurucuların kısmen yazdığı dosyalar geriye dönük olarak
sahiplenilmez.


Yayın, sınırlandırılmış `user.*` genişletilmiş özelliklerini korur ve tekrar girişte
doğrular. Protokol 1 ACL, dosya yetenekleri ve SELinux etiketlerini kabul etmez;
salt-okur kaynak taraması bunları koordinatörler durmadan önce bildirir. SELinux
hedef politika geçişinin kalıcı niyette tanımlanmasını gerektirir; yedek yolunun
etiketini kopyalayıp sonra `restorecon` çalıştırmak geçerli yayın kanıtı değildir.
Güncellemeyi geçirebilmek için sahip metadata'sı sessizce kaldırılmaz.

Yeniden başlatmadan sonra kurtarma, eksik `/run/celikpanel` dizinini yalnız tam
kabul edilmiş geri alma ve iki koordinatörün durduğu kanıtıyla oluşturabilir.
Mevcut dizinin özelliklerini düzeltmeye kalkmaz. Tamamlama bekleyen veritabanı
kontrolü mevcut WAL'ı özel kopyada okur; canlı DB/WAL'ı değiştirmez veya yeniden
geri yüklemez. WAL ve atomik yayın kesintileri gerçek alt süreç öldürülerek sınanır.

Gerçek deneyler ve uygulanamayan hata girişimleri
[bağımsız kurtarma kabul kaydında](../deploy/e2e/release-recovery/INDEPENDENT-RUNTIME.tr.md)
ayrı sonuçlarla tutulur.

Tamamlanmış yedekten geri alma ayrıca [bağımsız kurtarma verisini](RECOVERY-MATERIAL.tr.md) ve ona bağlı v2 yayınlama kaydını kullanır. Eski v1 işlemleri mevcut okuyucuyu korur; kalan kabul matrisi tamamlanmış sayılmaz.

## İşletim sistemi geçişinde kurtarmayı erteleme

Açılışta etkin olan kurtarma oneshot servisi, `multi-user.target` hedefine ulaşmanın
bir parçasıdır. İçinde systemd açılışının bitmesini beklemek, açılışın kendisini
engelleyebilir. Runner artık doğrulanmış güncelleme/geri alma alt sürecini başlatmadan
önce tek ve süre sınırı olan salt-okur hazırlık sorgusu yapar. `running` (çıkış 0)
ve `degraded` (çıkış 0 veya 1) başlatmaya izin verir. Çıkış 1 ile `initializing`,
`starting` veya `stopping` ise başlatmayı erteler: işlem kilidi bırakılır; işlem
işaretçileri, snapshot ve istek bağlantısı değiştirilmeden çağrı sonlanır. Mevcut
yerel zamanlayıcı çağrı bittikten sonra aynı işlemi yeniden ele alır. Sorgunun
zaman aşımı beş saniye, zorla sonlandırma ek sınırı bir saniyedir. Bilinmeyen çıktı,
beklenmeyen çıkış kodu veya başarısız/zaman aşımına uğramış sorgu kurtarmayı başlatma
yetkisi vermez; günlük incelenecek hazırlık kontrolünü açıklar. Alt sürecin değişiklik
anındaki platform ve kilit kontrolleri zorunlu kalır. Salt-okur son durum doğrulaması,
kurtarma başlatmadan veya gözlem yazmadan önce bekleyen işlem işaretçilerini reddeder;
ertelemeyi tamamlanma kanıtına çeviremez.

Ertelenen çağrı, tamamlanmış kurtarma değildir. Günlük beklenen önkoşulu ve otomatik
sonraki adımı açıklar. Tam isteğe bağlı gözlem `recovering`, `terminal_proof=none`
olarak kalır; önceki hata korunur. Kalıcı şema, kit protokolü veya veritabanı sürümü
değişmez. Tarayıcı yeni bir açılış bekleme nedeni yerine mevcut, sonlanmamış kurtarma
durumunu alır. Arayüzde ayrıntılı açılış bekleme açıklaması bu değişikliğin dışındadır.

Bu çalışma D-025 ilkeleri 2–5 ve P0.2/P0.3 kapsamındadır. Gerçek runner sözleşme
testleri; ilk açılışta hata uydurulmamasını, bilinen hatadan sonra üç bekleme durumunu,
istek/işaretçi kanıtlarının korunmasını, kilidin bırakılmasını, aynı işlemin sonradan
tamamlanmasını, degraded durumunu, bozuk/bilinmeyen yanıtları ve zaman aşımını sınar.
Geri alma giriş testleri gerçek devralınmış kilit kontrollerini korur. Bu testlerde
systemctl ve geri yükleme alt süreci modellenir; **değişen runner için gerçek yeniden
başlatma kabul kanıtı değildir**. Debian X ve Arch Z eski runner ile çalışmıştır;
açılış hataları tarihsel kanıt olarak korunur. Yeni kaynak kimliğine sabitlenmiş adayla
yeniden başlatma deneyi ve kalan kontrol noktası/hizmet matrisi açık kalır.

[Değişen kaynakla AA denemesi](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.tr.md), izleyici uyumluluk kontrolünde beklediği için iki hata sınırına da ulaşamadı. Sonuçsuz ölçüm, tamamlanmamış detach ve durdurulan misafirler kaydedildi; gerçek açılış hazırlığı kabulü açık kalır.
