# Bileşen Manifestosu V2

Durum: mimari karar; uygulama bilinçli olarak aşamalı ilerleyecektir.

## Karar

CelikPanel, işletim sistemine özgü bileşen reçetelerini imzalı, salt okunur bir
SQLite kataloğuna taşıyacaktır. SQLite yalnızca bir veri deposudur. Hiçbir zaman
komut çalıştırmaz. Agent tek güvenilir yürütücü olmaya devam eder ve
yetkilendirme, doğrulama, kilitleme, süreç yürütme, hazırlık kontrolleri, geri
alma ve denetim günlüğünden sorumludur.

Değiştirilemez katalog ile değiştirilebilir çalışma zamanı durumu ayrı
tutulmalıdır:

- `/usr/share/celikpanel/manifests/components-v2.db`: imzalı reçete kataloğu
- panel veritabanı: operasyonlar, denetim olayları, adım sonuçları ve geri alma günlüğü

## Güven sınırı

Katalog şunları tanımlayabilir:

- işletim sistemi, dağıtım, sürüm, mimari, paket yöneticisi ve servis yöneticisi seçicileri
- her bileşen operasyonu için destek durumu ve desteklenmeme nedeni
- paket ve servis adları
- sıralı, türü belirlenmiş operasyon adımları
- yapılandırma şablonları ve onaylanmış hedef yollar
- hazırlık probları ve geri alma karşılıkları

Katalog yetki veremez veya genel amaçlı bir çalıştırılabilir dosya adımı
içeremez. `exec`, `exec_probe`, `program`, `args` ve `environment` V2'nin
parçası değildir. Paket, dosya, servis, güvenlik duvarı ve prob operasyonları,
çalıştırılabilir dosya seçimleri güvenilir Go koduna derlenmiş türü belirlenmiş
agent adaptörlerini kullanır. Böylece katalog verisinin seçebileceği Python
`-c`, `env sh`, BusyBox, `cmd` veya PowerShell gibi bir yorumlayıcı yolu
kalmaz.

Aşağıdakiler her zaman Go içinde kalır:

- kimlik doğrulama, yetkilendirme, hak sahipliği ve denetim politikası
- güvenilir ana makine profili algılama
- reçete doğrulama ve deterministik reçete seçimi
- süreç, paket, dosya, servis, güvenlik duvarı, izin ve prob adaptörleri
- yol geçişi, türü belirlenmiş değişken, zaman aşımı, gizli bilgi ve adaptör girdisi kontrolleri
- paket yöneticisi ve operasyon kilitleme

## Destek durumları

Destek operasyona özgüdür. Örneğin durum sorgulama desteklenirken otomatik
kurulum desteklenmeyebilir.

- `supported`: otomatik olarak planlanabilir ve yürütülebilir
- `unsupported`: bir gerekçeyle bu platformda bilinçli olarak kullanılamaz
- `manual_only`: mevcut bir kurulum algılanabilir veya yönetilebilir
- `unavailable`: test edilmiş, eşleşen bir reçete yoktur
- `blocked`: bir reçete vardır ancak bir bağımlılık, çakışma, depo veya politika bunu engeller

Eksik veriler hiçbir zaman sessizce genel bir Linux komutuna geri dönmemelidir.

## Ana makine seçimi

Agent, örneğin aşağıdaki gibi güvenilir bir ana makine profili oluşturur:

```text
os_family=linux
distro_id=ubuntu
distro_like=debian
version=24.04
architecture=amd64
package_manager=apt
service_manager=systemd
```

Reçeteler en özelden en genele doğru seçilir:

1. tam dağıtım, sürüm aralığı ve mimari
2. tam dağıtım ve sürüm aralığı
3. tam dağıtım
4. `ID_LIKE` ve sürüm aralığı
5. `ID_LIKE`
6. paket yöneticisi ve servis yöneticisi
7. açıkça tanımlanmış işletim sistemi ailesi varsayılanı

Aynı özgüllük düzeyindeki iki eşleşme hatadır. Test edilmemiş yeni işletim
sistemi sürümleri, seçici açık uçlu bir sürüm aralığına açıkça izin vermedikçe
eşleşmez.
Her reçete açıkça denetlenmiş bir `os_family` belirtmelidir; bu sınır olmadan
yalnız dağıtım veya paket yöneticisi belirtmek reddedilir. Geçerli şema
doğrulayıcısı `linux` ve `windows` reçete verilerini kabul eder; diğer tüm
aileleri yol ve adaptör sözleşmeleri denetlenene kadar reddeder.

## Asgari şema alanları

Katalog, sürümlü JSON yükleriyle küçük bir ilişkisel zarf kullanır. Olası her
adım alanını ayrı bir tabloya bölmez ve tüm kataloğu sorgulanamayan tek bir JSON
kutusuna da saklamaz.

Kimlik ve sorgulama ilişkisel kolonların sahibidir:

- sürüm üst verileri: şema sürümü, katalog sürümü, monoton katalog sıra
  numarası, asgari agent şeması ve anahtar kimliği
- öğe kimliği, öğe türü (`component`, `addon`, `application`), revizyon ve etkinlik durumu
- reçete kimliği, öğe kimliği, platform anahtarı, operasyon, revizyon ve destek durumu

Platforma ve operasyona göre değişen parçaların sahibi katı JSON nesneleridir:

- öğe sunumu ve ürün üst verileri
- platform seçicileri
- türü belirlenmiş operasyon değişkenleri ve doğrulama kuralları
- sıralı paket, dosya, servis, güvenlik duvarı ve prob adımları
- şablonlar, hazırlık probları ve geri alma ilişkileri

Tüm imzalı JSON, yinelenen anahtarları reddeder ve büyük/küçük harfe duyarlı,
tam alan adlarını zorunlu tutar; Go'nun büyük/küçük harfe duyarsız yapı alanı
eşleştirmesi kabul edilmez. Seçici ve reçete JSON'u bilinmeyen alanlar
reddedilerek çözülür. Her adım
türünün tam bir anlamsal alan izin listesi vardır ve geri alma başvuruları
yalnız ana `steps` listesindeki girdilerde geçerlidir. Öğe `metadata` alanı
sunum ve ürün verileri için bilinçli olarak bir genişletme torbasıdır: burada
bilinmeyen anahtarlar kabul edilir; ancak bu üst veri yürütücü tarafından hiçbir
zaman yorumlanmaz ve yetkilendirmeyi, seçimi veya operasyon davranışını
değiştiremez. Agent, her reçeteyi belirlenimci seçime katılmadan önce doğrular.

## İmzalama ve güncellemeler

Katalog, root operasyon politikasıdır ve bu nedenle yazılım tedarik zincirinin
bir parçasıdır.

- Veritabanı özetini Ed25519 ile imzalayın ve güvenilir açık anahtarı agent içine gömün.
  `BuildCatalog`, hâlâ açık olan özel inode'un özetini döndürür ve imzalama bu
  özeti girdi olarak almak zorundadır. Yayımlanan yol yeniden açıldığında tam
  olarak aynı baytlar elde edilmezse imzalama başarısız olur. Oluşturma,
  imzalama ve açma aynı 64 MiB veritabanı sınırını uygular.
- Adayı bu 64 MiB boyut sınırıyla özel bir `0700` geçici dizine `0600` snapshot
  olarak kopyalayın. Bu snapshot'ı karmalayarak doğrulayın, ardından tam olarak
  aynı snapshot'ı `trusted_schema=OFF` ile salt okunur ve değiştirilemez açın;
  bir yolu karmalayıp değiştirilebilir kaynak baytlarını yeniden açmayın.
- Kullanmadan önce tam `sqlite_master.sql`, `table_xinfo`, tablo, kolon, CHECK
  kısıtı, yabancı anahtar ve indeks yapısını, ek şema nesnesi bulunmadığını,
  yalnızca tek bir üst veri satırı olduğunu, satır alanı değişmezlerini, katalog
  sürümünü ve asgari agent şemasını doğrulayın.
- Pozitif bir `AgentSchema` zorunludur. İlk kurulum politikası sıfır sıra ve boş
  özet kullanır. İlk kurulumdan sonra pozitif sıra ile onun 64 karakterli küçük
  harf SHA-256 özetini taşıyan
  `OpenPolicy{MinimumCatalogSequence, MinimumCatalogDigest}` zorunludur. Daha
  düşük sırayı ve aynı sıradaki farklı özeti reddederek hem yeniden oynatmayı
  hem de aynı sıra numarasındaki çatallanmayı önleyin.
- `OpenPolicy` yalnız doğrulama API'sidir. Çalışma zamanı etkinleştirmesi ve
  kalıcı yeniden oynatma durumu bu paket tarafından henüz uygulanmamıştır.
  Gelecekteki etkinleştirici; etkinleştirmeyi sıraya koymalı, bir işlem başlatmalı,
  geçerli sıra ve özet tabanını işlemin içinde yeniden okumalı, adayı
  karşılaştırmalı ve commit öncesinde CAS eşdeğeri korumalı bir güncelleme
  kullanmalıdır. Yeni tabanı yalnız başarıyla etkinleştirdiği katalog için
  commit edebilir. Sonraki her açılış kalıcılaştırılmış iki değeri de vermelidir.
  Katalog paketi bu tabanı hiçbir zaman sessizce düşürmez, sıfırlamaz veya aynı
  sıra özet sabitlemesini atmaz.
- Linux, hazırlamadan önce yayım üst dizinini
  `O_DIRECTORY|O_CLOEXEC|O_NOFOLLOW` ile açıp sabitler ve `fstat` ile doğrular.
  Üst dizin root'a veya geçerli etkin UID'ye ait olmalı; grup ya da diğer
  kullanıcılarca yazılabilir olmamalıdır. Sembolik bağlantı, sahiplik uyuşmazlığı
  veya güvensiz izinler güvenli biçimde başarısız olur. Rastgele özel hazırlama
  dizinini ve kataloğu bu sabitlenmiş üst dizin altında `mkdirat`/`openat` ile,
  `0700`/`0600` kipleri ve `O_NOFOLLOW` kullanarak oluşturun.
- SQLite hazırlanan kataloğa sabitlenmiş hazırlama-dirfd yolu üzerinden erişir;
  aynı önceden açılmış düzenli dosya inode'u oluşturma, eşzamanlama, özet alma ve
  üzerine yazmayan tek atomik sabit bağlantıyla yayım boyunca yetkili kaynak
  olarak kalır. İmzalayıcı son üst dizini bağımsız olarak yeniden doğrulayıp
  sabitler, yalnız taban adını `openat(..., O_NOFOLLOW)` ile açar; düzenli dosya
  olmayan, yanlış sahipli veya grup/diğer kullanıcılarca yazılabilir bir ürünü
  reddeder ve oluşturma özetini beklenen değer sabitlemesi olarak kullanarak tam
  olarak bu açık inode'u karmalayıp imzalar. İnceleme ve temizlik aynı dirfd
  sınırlarını kullanır. Dizin genelindeki danışma kilidi, iş birliği yapan tüm
  yayımcıları bağlantı, eşzamanlama ve olası temizlik boyunca sıraya koyar.
  Dosyayı ve üst dizini eşzamanlayın. Bağlantı sonrası dizin eşzamanlaması
  başarısız olursa türü belirlenmiş kısmi yayım hatası döndürün, yalnız hâlâ
  oluşturulan inode olan hedefi kaldırın, temizliği eşzamanlayın ve hedefin
  kalmış olabileceği durumu bildirin. Mevcut hedef hiçbir zaman değiştirilmez.
- Oluşturma ve açma Linux dışındaki her GOOS üzerinde güvenli biçimde başarısız
  olur. Bugün dosya sistemi etkinleştirmesi denetlenmiş tek hedef Linux'tur.
  Linux'taki kataloglar Windows seçicili reçete verilerini yine saklayıp
  doğrulayabilir; bu kısıtlama veri modeline değil dosya sistemi
  etkinleştirmesine ilişkindir.
- Önceki doğrulanmış sürümleri adli inceleme veya açık acil durum kurtarması için
  saklayın. Normal geri alma yalnız sıra numarası
  `highest_accepted_catalog_sequence` değerinin altında olmayan bir sürümü
  seçebilir; tabanı düşürmek ayrı, açık ve denetlenen bir kurtarma politikası
  gerektirir ve hiçbir zaman normal `OpenPolicy` yolu değildir.
- Şema değişiklikleri için yeni bir veritabanı oluşturup imzalayın; çalışma zamanında asla taşımayın.
- Panel kullanıcı arayüzünde imzasız bir yerel reçete düzenleyicisi sunmayın.

Her denetim olayı manifest özetini, katalog sıra numarasını, reçete kimliğini ve
revizyonunu, hassas girdileri gizlenmiş türü belirlenmiş adaptör eylemini, çıkış
kodunu, hazırlık sonucunu ve geri alma sonucunu kaydeder.

## Tamamlanma ve geri alma kuralları

Bir kurulum ancak paket, yapılandırma, servis ve bileşene özgü işlevsel problar
başarılı olduktan sonra tamamlanmış sayılır. Bir paket yöneticisinin yalnızca
sıfır çıkış kodu döndürmesi yeterli değildir.

Yürütmeden önce agent; paket varlığını, servisin etkin/çalışır durumunu,
CelikPanel'in sahip olduğu dosyaların karmalarını ve güvenli yedeklerini ve
CelikPanel'in sahip olduğu güvenlik duvarı durumunu kaydeder. Geri alma yalnızca
ilgili operasyon tarafından yapılan değişiklikleri geri çevirir. Mevcut
kullanıcı verilerini hiçbir zaman silmez. Güvenli bir geri alma mümkün değilse
operasyon `failed_recovery_required` durumuyla sona erer.

Ayrıcalıklı agent artık kalıcı servis mutation defterine ve süreçler arası ana
makine kilidine sahiptir. Buna karşın V2 yürütmesi; güvenilir bir etkinleştirici
çözümlenen her reçete dizisini ve katalog özetini bu lease'e bağlayana, etkinliği
özet karşılaştır-değiştir yöntemiyle kaydedene ve aşağıda tanımlanan executor
tarafı paket, unit, yol, prob ve güvenlik duvarı izin listelerini uygulayana kadar
kapalı kalır.

## Platform adaptörleri

Mevcut adaptörleri paylaşan ek dağıtımlar normalde yalnızca katalog verileriyle
eklenebilir. Tamamen yeni bir işletim sistemi ailesi için ana makineyi yoklama,
süreç yürütme, dosyalar, servisler, güvenlik duvarı, izinler ve hazırlık probları
amacıyla tek seferlik güvenilir bir adaptör uygulaması yine gereklidir.

- Linux: paket yöneticileri, systemd, nftables, Unix izinleri
- FreeBSD: pkg, rc.d/sysrc, pf veya ipfw
- Windows: yerel süreç API'si, SCM, Windows Güvenlik Duvarı, ACL'ler

Adaptör mevcut olduktan sonra bileşen ve sürüm farklılıkları reçetelerde yer
alır.

Şema doğrulaması yürütme yetkisi değildir. Çalışma zamanı etkinleştirmesinden
önce güvenilir adaptör, her öğe kimliği ile türü belirlenmiş adımı derlenmiş
bileşen sınırlarına bağlamalıdır: izin verilen paket adları veya önekleri, servis
unit'leri, yazılabilir yol kökleri ve güvenlik duvarı uç noktası politikası. Bu
sınırların dışındaki her reçete hedefi güvenli biçimde reddedilmelidir. Bu
yürütücü tarafı izin listesi ve etkinleştirici geçerli paket tarafından henüz
uygulanmadığı için V2 yalnız doğrulanmış kuru çalışma temeli olarak kalır.

## Geçiş sırası

1. HostProfile, şema doğrulama, imza doğrulama ve kuru çalıştırma planlarını uygulayın.
2. Agent'a ait kalıcı iş defterini, heartbeat/son süre kurtarmasını ve süreçler arası kilidi ekleyin.
3. Yaşam döngüsü ve TCP hazırlığı dâhil olmak üzere önce Debian/Ubuntu ve Arch için Memcached'i taşıyın.
4. V2'yi yürütmeden gölge modunda eski ve V2 planlarını karşılaştırın.
5. Basit, paket tabanlı servisleri taşıyın.
6. Web, DNS, posta, VPN ve veritabanı araçlarını türü belirlenmiş yapılandırma adımlarıyla taşıyın.
7. Çalışma zamanlarını, üretici depolarını, sürüm seçimini ve Roundcube'u taşıyın.
8. FreeBSD adaptörünü ekleyin ve test edin.
9. Windows adaptörünü ekleyin ve test edin.
10. Eski işletim sistemi dallarını yalnızca doğrulanmış eşdeğerlik sağlandıktan sonra kaldırın.
