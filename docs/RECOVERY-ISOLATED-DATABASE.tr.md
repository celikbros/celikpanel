# Güncellemede veritabanı dönüşümünü ayırma

*15 Eylül 2026 · [English](RECOVERY-ISOLATED-DATABASE.md) · D-025 / P0.3*

Bu dilim, tam v6 snapshot kullanan normal güncellemede veritabanı hazır olmadan
kesilen işlemi ele alır. Aday sürüm ayrı çalışma kopyasını dönüştürür; asıl
veritabanı ancak bağımsız doğrulamadan sonra atomik olarak değiştirilir.
[İleri tamamlamanın](RECOVERY-FORWARD-COMPLETION.tr.md) devamıdır. P0.3 kapanmaz;
lisans politikası ve kurulu panel güncellemesini yalnız kullanıcının başlatması
kuralı değişmez. Aşağıdaki sınırlı Q sonucu `0610b239` kaynağına aittir. Ayrı
ve temiz R konukları, düzeltilmiş `cb31654` için aynı iki sınırı doğrular;
bunlar tam arıza matrisi kabulü değildir.

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
salt okunur kontrol eder; çalışan SQLite'ın WAL dosyasına izin verir.

`cb31654` ile yeni normal kabul, `/var/lib/celikpanel` için panel hesabının tam
UID'sini, `celikpanel` grubunun tam GID'sini ve `0750` iznini gerektirir. Aynı
koşul, apply-only kurulumdan önce yeni material için asıl Before yakalanırken
yeniden kontrol edilir. Böylece kurucunun veya yerel hizmetin
`StateDirectoryMode=0750` başlangıcının daha sonra normalleştireceği bir üst dizin
düzeni kabul edilmez. Root sahipliği, `0700` izni veya farklı grup; dizin, DB ve
WAL değiştirilmeden reddedilir.

Gözlenen desteklenmeyen üst dizin `ErrUnsupportedDatabaseParent` döndürür.
Yönlendirme, sahibin bilinçli ayarlarını korumasını; desteklenen düzeni veya
uyumlu kurtarma sürümünü seçtikten sonra panelden yeniden denemesini belirtir.
Bu sonuç, desteklenmeyen dosya öznitelikleri için `ErrUnsupportedMetadata` ve
bilinmeyen/okunamayan metadata sonucundan ayrıdır. Kontrol servisleri durdurmaz,
`chown`/`chmod` onarımı yetkisi vermez. Asıl DB veya üst dizinin genişletilmiş
öznitelikleri desteklenmez; silinmez veya yalnız bir kısmı kopyalanmaz.

Tarihsel material okuyucuları daha geniş güvenli/karantina üst dizin sözleşmesini
korur. Kabul düzeltmesi eski Before kayıtlarını yeniden yazmaz, kurtarma yolunu
değiştirmez. Mevcut snapshot üreticisi önce dayanıklı kopyayı alır, sonra asıl
SQLite'ı normalleştirir; karantina çıkışında panel sahipliği ve `0750` iznini
geri yükler. Bu önceki normalleştirme, sahibin keyfi dizin düzeninin korunduğu
kanıtı değildir. Yeni normal ön kontrol desteklenmeyen düzeni bu yola girmeden
reddeder. V3 önceki-hal yayını ayrıca asıl DB'nin yan dosyalarının yokluğunu arar.

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

[Q gerçek sistem kabul kaydı](../deploy/e2e/release-recovery/ISOLATED-DATABASE.tr.md),
`0610b239a7bb976874a2c347099bf88e779237d1` kaynağını gerçek yayımlanmış
Alpha64/schema38 durumuyla başlayan yeni Arch ve Debian 13 konuklarına bağlar.
Bağımsız host incelemesinde Arch'ın 87, Debian'ın 70 indeksli kanıt dosyasının tümü
doğrulandı. Deney, kayıtlı imzasız yerel aday düzeneğini kullandı; yayımlanmış
imzalı Agent/UI kabul yolu sınanmadı.

Arch'ın `candidate-installed` kesintisinde asıl Before ve ilk çalışma kopyası
tam eşleşiyordu; yalnız admission kanıtı vardı, sonraki DB yayın kayıtları yoktu.
Otomatik geri alma çalışma kopyasını koruyarak eski ikililere ve schema38/55
tabloya döndü. Bu ilk durum kanıtıdır; SQLite işleminin içinde gerçek kesinti veya
başarısız WAL koruma sonucu değildir. Debian gerçek schema38→42 dönüşümünü
bitirdikten sonra `completion-database-verified` sınırında adayın saklanan tam üç
dosyası ve güncelleyici kaybedildi. Otomatik ileri tamamlama yeni ikilileri ve
schema42/65 tabloyu korudu. Ham admission, mühür ve yayın makbuzları; korunan özgün
Before inode/içeriğini ve yayınlanmış asıl inode'u bağlar. Son işlem işaretçileri
yoktu, loopback HTTPS erişilebilirdi; kesintisiz erişim kanıtlanmış sayılmaz.

Yerel toplu veri doğrulayıcısı, `sqlite_sequence` dahil 55 eski tabloyu eski sütun
izdüşümleri, satır kimliği ve tür bilgili değerlerle karşılaştırdı. Eski satır
kaybı/değişimi yoktu; `applied_at` dahil eski migration satırları korundu. Her konukta
19 sonraki metrics satırı eklendi. Hiçbir tablo dışlanmadan tam DB karşılaştırması
`DIFFERENT` kaldı: Debian'da ayrıca amaçlanan 10 yeni tablo, iki yeni domain sütunu
ve 39–42 migration kayıtları bulunuyor. Domain tablosu boştu; dolu domain dönüşümü
sınanmış değildir. Ham satır değerleri dışarı aktarılmadı; host incelemesi özel
satırları yeniden hesaplamak yerine doğrulayıcıyı, mühürlü sonuçları ve kanıt
bağlarını denetler. Son inceleme silinmiş işaretçileri üretmedi veya yalnız active
aşamasına ait DB API'sini yeniden çalıştırmadı.

Sonraki `cb31654` düzeltmesinin root/Linux regresyonları önce desteklenmeyen üst
dizinin kabul edildiğini gösterdi; ardından izin/içerik değiştirmeyen reddi,
tarihsel okuma uyumluluğunu ve ayrı uygulanabilir düzen yönlendirmesini doğruladı.
Bu bileşen testleri Q'nun kaynak kapsamını genişletmez.

Yeni R konukları tam `cb3165456bb4ba4654dc19d51a5eafc13721a5fb` kaynağını,
aynı gerçek Alpha64 başlangıcını ve iki arıza sınırını kullandı. Bağımsız
incelemede Arch 62, Debian 63 mühürlü dosyanın tamamı ve 28/44 ek DB kontrolü
geçti: Arch ilk çalışma kopyasını koruyarak otomatik geri döndü; Debian üç dosya
kaybından sonra schema38→42 geçişini otomatik tamamladı. Eski 55 tablonun bütün
eski satırları korundu; her konukta dört sonraki metrics örneği eklendi
(15→19 ve 16→20), genel karşılaştırma `DIFFERENT` kaldı. Kabul kaydı kaynak,
düzenek, arşiv ve tam işlem özetlerini bağlar. Gerçek deney desteklenen normal
dizin yolunu kanıtlar; desteklenmeyen düzenin reddi bileşen test kanıtıdır.
İki laboratuvar da disk ve kanıtları korunarak durduruldu.

Tam kill/reboot matrisi, imzalı aday kabulü, eksik snapshot yakalama, sahibin
öznitelik geçişleri, ilgisiz canlı şema/veri değişiklikleri sonrası kurtarma,
kanıt temizliği ve eski schema17/pre-ledger kabulü açıktır. AI yardımcısı da aynı
sınırlı işlem/kurtarma sözleşmesini kullanmalıdır; eksik yetkiyi tamamlayamaz veya
bilinmeyen sonucu başarı sayamaz.
