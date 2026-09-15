# Güncellemede veritabanı dönüşümünü ayırma

*15 Eylül 2026 · [English](RECOVERY-ISOLATED-DATABASE.md) · D-025 / P0.3*

Bu dilim, tam v6 snapshot kullanan normal güncellemede veritabanı hazır olmadan
kesilen işlemi ele alır. Aday sürüm ayrı çalışma kopyasını dönüştürür; asıl
veritabanı ancak bağımsız doğrulamadan sonra atomik olarak değiştirilir.
[İleri tamamlamanın](RECOVERY-FORWARD-COMPLETION.tr.md) devamıdır. P0.3 kapanmaz;
lisans politikası ve kurulu panel güncellemesini yalnız kullanıcının başlatması
kuralı değişmez. Yeni migration sınırlarının gerçek sistem kabulü henüz bekliyor.

## Yetki ve uyumluluk

Yeni kayıt `celikpanel/recovery-material/v3` kullanır. Dizin düzeni, snapshot v6
ve recovery protokolü 1 değişmez. Normal v3 kaydı, asıl veritabanının önceki
halini dosya/üst dizin kimliği, sahiplik, izinler, zamanlar ve içerik özetiyle
bağlar. Tam snapshot ve manifest ayrıca sabitlenir. Bu kanıt çalışma kopyasından
önce oluşur; admission öncesi kesinti başka bir veritabanını değiştirme yetkisi
sayılmaz.

Servisler durmadan seçili kurtarma ortamının material-v3 ve
`celikpanel/database-migration-admission/v1` desteği doğrulanır.
`probe-update-database`, devralınan yerel işlem kilidi altında dosya özelliklerini
salt okunur kontrol eder; çalışan SQLite'ın WAL dosyasına izin verir. Bu dilim
asıl DB veya üst dizinin genişletilmiş özniteliklerini desteklemez; bunları
silmez veya bir kısmını sessizce atmaz. Mevcut snapshot üreticisi önce dayanıklı
kopyayı alır, sonra asıl SQLite'ı normalleştirir. V3 önceki-hal kaydı oluşurken
asıl veritabanının yan dosyaları bulunmamalıdır.

V1/V2 kayıtları okunmaya devam eder. `database-policy --snapshot NAME`, normal
v3 için `required` döndürür. Yalnız boş standart çıktıyla çıkış 6, ayrıca
doğrulanmış eski yola izin verir. Bozuk/bilinmeyen kayıt veya kayıt yokken aynı
işleme bağlı DB çalışma alanının kalmış olması eski yola düşürülemez. Schema17
ve pre-ledger geçişleri kendi mevcut yollarında kalır.

## Hazırlama ve yayınlama

Normal güncelleme şu adımlar boyunca `active` kalır:

1. Aynı token/snapshot altında v3 kanıt ve çalışma kopyası kabulü hazırlanır.
2. Adayın normal migration'ı panel kullanıcısıyla
   `/var/lib/celikpanel/.release-db-migrations/<token-sha256>/work` içinde çalışır.
3. Yazıcıların durmuş olduğu doğrulanır; çalışma DB ve yan dosyaları sabitlenir.
   SQLite normalleştirmesi yalnız ikinci, ayrı özel kopyada yapılır.
4. Hedef şemanın tamamı, migration kimlikleri ve boş işlem kuyruğu doğrulanır.
   Önceki/sonraki kimlikleri içeren niyet kaydı dayanıklı yazılır. Atomik dosya
   takası, iki üst dizinin fsync'i ve yayın makbuzu izler.
5. Kurulu dosyalar ve DB yayını yeniden doğrulanır. Ancak sonra
   `completion.pending` oluşturulup kontrollü servis başlangıcına geçilir.

Admission, çalışma mühürü, yayın niyeti ve geri alma niyeti ayrı v1 şemalarıdır.
Makbuzlar tam ilgili niyeti bağlar. Asıl DB'nin eski inode'u takasın karşı
ucunda korunur. Çalışma DB/WAL/SHM ve başarısız hazırlık dizinleri saklanır.
Durum sorgusu ikinci migration başlatmaz; bağımsız kurtarma doğrulayıcısı aday
migration'ını veya normal panel başlangıcını çalıştırmaz.

Çalışma kopyası ayrı bir migration hedefidir; keyfi aday kodu için işletim
sistemi sandbox'ı değildir. İncelenen gömülü migrator `CELIKPANEL_DATA_DIR`
kullanır; asıl DB'nin sahibi hâlâ panel hesabıdır. Beklenmeyen asıl DB değişikliği
önceki/sonraki kanıt kontrolünde reddedilir, sessizce üzerine yazılmaz.

Dört iç komut yalnız mevcut snapshot adını kabul eder:
`prepare-update-database`, `publish-update-database`,
`restore-update-database`, `verify-update-database`. Seçili kit ve devralınmış
FD9 yerel flock gerekir. Sunucu sahibinin giriş noktası
`sudo /usr/libexec/celikpanel/recovery recover` olarak kalır. İç komutlar yeni
bir güncelleme arayüzü veya genel dosya onarım API'si değildir.

## Hata ve kurtarma

Migration başarısızsa veya kesilirse asıl veritabanı önceki halinde kalır.
Kurtarma çalışma kanıtını korur; bilinmeyen canlı verinin üzerine eski snapshot
kopyalamak yerine tam önceki hali doğrular. Yayından sonra geri alma kayıtlı
çifti doğrulayıp ters atomik takas yapar. Kullanıcının değiştirdiği dosya,
eksik gerekli makbuz veya tanınmayan durum korunur ve doğrulanamadığı bildirilir.
WAL silmek veya sahiplik kanıtını yeniden yazmak uzlaştırma sayılmaz.

Servislerin kapalı olduğu active aşamasında tam dosya/içerik kanıtı ve yan
dosyaların yokluğu aranır. Servisler başladıktan sonraki olağan DB/WAL yazımları
içerik ve zamanları değiştirebilir. Geç doğrulama yayınlanmış inode/sahiplik,
korunan karşı dosya ve makbuzlarla birlikte özel WAL kopyasında tam şema,
migration geçmişi ve kuyruk kontrolü kullanır. Güncelleme hedef şemayı; geri
alma tarihsel snapshot şemasını gerektirir. Bilinen eski iki sütunlu migration
defteri yalnız özel okuma kopyasında standartlaştırılır, sonra snapshot
geçmişiyle karşılaştırılır; bilinmeyen defter biçimleri reddedilir. Asıl
DB/WAL/SHM ve snapshot değişmez. Zamanlayıcılar geri yüklenmeden
önce ve sonra son kanıt tekrarlanır. Completion işaretçisi başarı demek değildir.

## Kanıt ve açık işler

`deploy/test-isolated-database-migration.sh`, gerçek shell akışını çıkarıp
çalıştırır: completion öncesi yayın, migration/yayın/doğrulama hataları, çalışan
yazıcı, uygunsuz çalışma yolu, belirsiz politikayı ret ve eski/yeni geri alma
seçimi sınanır. Go testleri gerçek SQLite ve korumalı yayın API'lerini, seçili
sınırlarda SIGKILL dahil sınar. Bunlar gerçek sistem güncelleme kabulü değildir.

Geçici deney düzeneğine varsayılan Alpha75 yanında sabit yayımlanmış
Alpha64/schema38 başlangıcı eklendi. Gerçek çalışan/disk ikili özetleri ve 38
migration kimliği kontrol edilir. Arch ve Debian'da gerçek eski-yeni dönüşümü,
korunan başarısız WAL, kesin süreç kesintisi, aynı işlemin otomatik kurtarılması
ve iş yüklerinin korunması gözlenmeden bu dilime gerçek sistem kabulü verilmez.

Tam kill/reboot matrisi, imzalı aday kabulü, eksik snapshot yakalama, sahibin
öznitelik geçişleri, ilgisiz canlı şema/veri değişiklikleri sonrası kurtarma,
kanıt temizliği ve eski schema17/pre-ledger kabulü açıktır. AI yardımcısı da aynı
sınırlı işlem/kurtarma sözleşmesini kullanmalıdır; eksik yetkiyi tamamlayamaz veya
bilinmeyen sonucu başarı sayamaz.
