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
Mevcut uyumlu seçim korunur. Seçilmiş kodun kayıp/bozuk olması yeni adayla üzerine
yazma veya aday yaşam döngüsü koduna geri dönme yetkisi değildir. Yarım kalmış
paketler seçilmeden kanıt olarak saklanır.

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

Bütün desteklenen aşamalar sınanmadan P0.3 kapanmaz. Paketin mevcut geri yükleme
algoritmalarını paylaşması; bütün dosya yayımını atomik, TLS normalizasyonunu
salt-okur veya adayın veri bütünlüğünden tamamen bağımsız yapmaz. Yeni kurtarma
paketi/protokolü seçimi, uyumluluk deneyi ve kanıtlanmış önceki paketin korunmasını
gerektirir. P0.4 ortak veri sözleşmeleri ve P0.5 bağımsız yenileme/açılış kanıtı
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
