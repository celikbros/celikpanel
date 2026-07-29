# Sistem SQLite Yönetimi

Durum: ilk sürüm kapsamı uygulanmıştır.

## Amaç

CelikPanel, panelin ve seçili yönetilen servislerin kullandığı SQLite
veritabanları için yöneticilere bir bakım görünümü sunar. Bu sayfa, sabit bir
envanter için hazırlanmıştır; genel amaçlı bir SQLite dosya yöneticisi değildir.

Mevcut uygulama şunları sağlar:

- yalnızca yöneticilere açık envanter
- özetlenmiş `PRAGMA quick_check` kontrolü
- güvenli anlık görüntü oluşturma ve indirme
- desteklenen, değiştirilebilir veritabanları için açıkça başlatılan
  `PRAGMA optimize` işlemi

Yol seçici, SQL konsolu, tablo veya satır düzenleyici, şema düzenleyici,
veritabanı yükleme, geri yükleme, değiştirme ya da silme sağlamaz.

## Yöneticiler nereden kullanır?

Yönetici, **Veritabanları** sayfasını açıp **Sistem SQLite** sekmesini seçer.
Sayfa bilinen sistem veritabanlarını; erişilebilirlik, boyut, journal modu,
user version, sağlık durumu ve desteklenen işlemlerle birlikte listeler.

Her karttaki **Kullanılabilir işlemler** değeri, agent'ın döndürdüğü kesin
işlem listesinden türetilir. Genel bir değiştirilebilir/salt okunur etiketi,
işlem yeteneğinin yerine kullanılmaz.

Bu sayfa kiracı veritabanı aracı olarak sunulmaz. Hem panel middleware'i hem de
SQLite yönetim handler'ları `admin` rolünü zorunlu tutar.

## Sabit envanter

HTTP ve agent RPC istekleri dosya sistemi yolu değil, sabit bir veritabanı
kimliği taşır. Bu kimliği sunucudaki dosyaya eşleyen ayrıcalıklı agent'tır.
Bilinmeyen kimlikler reddedilir.

| Kimlik | Veritabanı | Varsayılan sunucu konumu | Desteklenen bakım |
| --- | --- | --- | --- |
| `panel` | CelikPanel durumu | `/var/lib/celikpanel/celikpanel.db` | envanter ve kontrol; yakın zamanda yeniden doğrulama gelene kadar anlık görüntü ve optimize kapalıdır |
| `powerdns` | PowerDNS yetkili verisi | `/var/lib/powerdns/pdns.sqlite3` | envanter, kontrol, anlık görüntü indirme, optimize |
| `roundcube` | Roundcube uygulama durumu | `/var/lib/celikpanel-webmail/db/roundcube.sqlite3` | envanter, kontrol, anlık görüntü indirme, optimize |
| `component-catalog` | Component, add-on ve uygulama kataloğu | `/usr/share/celikpanel/manifests/components-v2.db` | envanter, kontrol, anlık görüntü indirme |

Bu konumlar agent politikasının parçasıdır ve teşhis için bir ipucu olarak
gösterilebilir. İsteklerde düzenlenebilir alan değildir. Ortama özel yol
değişiklikleri yönetim sayfasından değil, sunucu dağıtımı sırasında yapılır.

## İşlemler

### Envanter

`GET /api/v1/system-databases` sabit envanteri döndürür. Şunları gösterebilir:

- sabit kimlik, ad, amaç ve veritabanı türü
- erişilebilirlik ve özetlenmiş durum
- bayt cinsinden boyut ve değişiklik zamanı
- journal modu ve SQLite `user_version` değeri
- desteklenen işlemler

Envanter tablo satırlarını, kimlik bilgilerini, sırları veya SQL çıktısını
döndürmez.

### Kontrol

`POST /api/v1/system-databases/{id}/check`, agent'tan seçilen veritabanını sabit
politikası üzerinden açmasını ve `PRAGMA quick_check` çalıştırmasını ister.
Yanıt, özetlenmiş bütünlük sonucudur; veritabanı satırlarını açığa çıkarmaz.
Başarısız bir kontrol veritabanını onarmaz veya değiştirmez.

### Anlık görüntü indirme

Anlık görüntü indirme, yalnızca yöneticilere açık iki POST isteği kullanır.
Önce `POST /api/v1/system-databases/{id}/snapshot`, agent'tan geçici bir anlık
görüntü oluşturmasını ister. Panel, anlık görüntünün tamamını sınırlı parçalar
hâlinde okur; kısa ömürlü indirme kimliğini JSON olarak döndürmeden önce beyan
edilen boyutu ve SHA-256 özetini doğrular. Canlı veritabanı dosyası doğrudan
gönderilmez.

Ardından tarayıcı bu kimliği form gövdesinde
`POST /api/v1/system-databases/{id}/snapshot-download` adresine gönderir.
Kimlik hiçbir zaman URL'ye eklenmez. İndirme endpoint'i ek başlıklarını
kesinleştirmeden önce kimliği ve ilk parçayı doğrular, hazırlanmış dosyayı
akış hâlinde gönderir ve istek bittiğinde serbest bırakır.

Değiştirilebilir veritabanlarında SQLite'ın çalışma biçimine uygun yedekleme
kullanılır; böylece etkin bir WAL veritabanından bağımsız bir kopya
oluşturulabilir. Değişmez component kataloğu ise içeriğini yeniden yazacak bir
bakım işlemi uygulanmadan kopyalanır.

CelikPanel denetim düzlemi veritabanı bu sürümde anlık görüntü indirme işlemini
sunmaz. Parola özetleri ve TOTP sırları içerebildiği için, güvenilir bir yakın
zamanda parola/TOTP ile yeniden doğrulama katmanı gelene kadar bu işlem kapalı
kalır.

Anlık görüntü indirmelerinde kısa ömürlü ve dışarıdan anlam taşımayan bir
kimlik, sunucunun belirlediği güvenli dosya adı, `Cache-Control: no-store` ve
`X-Content-Type-Options: nosniff` kullanılır. Hazırlama hataları panelde görünür
kalır. İndirme HTTP hataları gizli bir iframe içinde kaybolmaz; tarayıcı hatayı
aynı sekmede gösterir, başarılı dosya eki ise panel sayfasını yerinde bırakır.
Hazırlanıp indirilmeden bırakılan kimlikler agent'ın kısa yaşam süresi temizliği
ile kaldırılır.

### Optimize

`POST /api/v1/system-databases/{id}/optimize` yalnızca türü önceden belirlenmiş
bir bakım işlemini dışarı açar: `PRAGMA optimize`. API bir PRAGMA adı veya SQL
metni kabul etmez.

Arayüz işlemi başlatmadan önce onay ister. Katalog değişmez ve salt okunur bir
artifact olarak ele alındığı için `component-catalog` üzerinde optimize
kullanılamaz. Yakın zamanda yeniden doğrulama uygulanana kadar CelikPanel
denetim düzlemi veritabanında da optimize işlemi kapalıdır.

## Güvenlik sınırı

- Yalnızca yukarıdaki dört sabit kimlik kabul edilir.
- İstekler yol, dosya adı, SQL cümlesi, tablo adı veya keyfî PRAGMA taşıyamaz.
- Ayrıcalıksız panel süreci veritabanı dosyası erişimini agent'a devreder.
- Linux'ta servis tarafından yazılabilen bir veritabanının her SQLite açılışı,
  UID, GID ve ek grupları doğrulanmış en az ayrıcalıklı veritabanı dosyası
  sahibine veya açıkça yapılandırılmış servis yazıcı kimliğine düşürülen dar
  kapsamlı bir alt süreçte gerçekleşir. Roundcube için kullanılan açık grup
  yazıcısı kimliğine yalnızca yapılandırılmış UID/GID ve dosya kipi
  doğrulandıktan sonra izin verilir. Bu sınırı sunamayan platformlarda
  değiştirilebilir işlemler güvenli biçimde reddedilir.
- Tarayıcıya dönen agent hataları özetlenir; sunucudaki dosya sistemi yolları
  açığa çıkarılmaz.
- Anlık görüntü hazırlama ve akışı sınırlandırılmış parçalar kullanır. Panel,
  geçici dosyayı indirme denemesinden ya da hazırlama hatasından sonra serbest
  bırakır; hiç indirilmeyen hazırlanmış dosyaları agent süre sonunda temizler.
- Aynı anda yalnızca bir sistem anlık görüntüsü oluşturulabilir veya etkin
  kalabilir. Anlık görüntüler kesin 2 GiB sınırına ve 512 MiB boş alan rezervine
  tabidir. Değiştirilebilir veritabanı worker'ı hazırlama ve hedef dosya
  tanıtıcılarını denetler: ikisi aynı dosya sistemindeyse boş alan en az
  `2 × sınır + rezerv`; ayrı dosya sistemlerindeyse her biri bağımsız olarak en
  az `sınır + rezerv` olmalıdır. SQLite yedekleme adımlarında büyüme sürekli
  denetlenir.
- İndirilmeye hazır anlık görüntü deposu yalnızca root'a açıktır. Agent ilk
  kullanımda yalnızca bu hazır-anlık-görüntü deposunda kendi kesin kalıbına uyan
  çökme artıklarını temizler ve güvensiz eşleşmeleri reddeder.
- Değiştirilebilir yedekler, doğrulanmış ve root'a ait sticky geçici kökün
  altında ebeveyn sürecin oluşturduğu iç içe bir hazırlama çalışma alanı
  kullanır. Ebeveyn süreç kökü, dış dizini ve yazıcıya ait stage dizinini dosya
  tanıtıcısı, aygıt ve inode ile sabitler. Ebeveyn çalıştığı sürece başarı, hata,
  iptal veya worker'ın zorla sonlandırılmasından sonra bu alanı sembolik
  bağlantıları izlemeden geri alıp siler.
- Mevcut sınır: ebeveyn agent sürecinin tamamı aniden sonlandırılırsa geçici
  kökte bir `.celikpanel-sqlite-owner-*` hazırlama dizini kalabilir. Bu sürüm,
  yalnızca önek veya yaşa dayalı temizliğin rolling restart sırasında canlı bir
  çalışma alanını silebilmesi nedeniyle bu girdileri otomatik toplamaz. İleride
  eklenecek temizleyici, aynı dosya-tanıtıcısı-sabitli ve bağlantı-izlemeyen
  kuralları uygulamadan önce sahipliği doğrulanmış bir lease ve bloklamayan kilit
  kullanmalıdır.
- Veritabanı başına bakım işlemleri sırayla yürütülür.
- Sayfayı ziyaret etmek kontrol, anlık görüntü, optimize, onarım, migration,
  servis durdurma veya servis yeniden başlatma işlemi başlatmaz.
- Bu araçlar domain, DNS, mail, güvenlik duvarı, component, add-on veya diğer
  panel ayarlarını değiştirmez.

DNS kayıtları DNS akışından, mail ve webmail durumu mail akışından, panel
ayarları da kendilerine ait türü belirli handler'lar üzerinden değiştirilmelidir.
SQLite sayfası bu ürün sınırlarını aşmak için bir kestirme değildir.

## Bilinçli olarak kapsam dışında bırakılanlar

İlk sürümde şunlar yoktur:

- keyfî dosya sistemi gezintisi
- SQL konsolu veya sorgu metni girişi
- tablo, satır veya şema düzenleme
- keyfî PRAGMA girişi
- `ATTACH`, `DETACH` veya extension yükleme
- canlı veritabanı indirme
- yükleme, geri yükleme, değiştirme veya silme
- PowerDNS ya da Roundcube satırlarını doğrudan düzenleme
- component kataloğu düzenleme veya optimize işlemi
- kiracıların oluşturduğu SQLite veritabanlarını yönetme

## Güvenlik geliştirmeleri ve ürün yol haritası

Aşağıdakiler gelecek çalışmalardır; mevcut sürümde var oldukları
varsayılmamalıdır:

- denetim düzlemi anlık görüntü ve optimize işlemlerini açmadan önce yakın
  zamanda parola ya da TOTP ile yeniden doğrulama
- sabit güvenlik tavanının ve boş alan tabanının altında kalacak biçimde
  yapılandırılabilir saklama süresi ve kurulum başına daha düşük sınırlar
- `celikpanel.db` dışında yalnızca eklemeye açık bir agent denetim kaydı
- component katalog artifact'leri için arayüzde gösterilen ayrık imza ve
  yeniden oynatma önleme doğrulaması
- servisleri güvenli biçimde durgunlaştıran, atomik değiştirme, hazır olma
  kontrolleri ve geri dönüş içeren bakım modu geri yükleme akışı
- müşterilerin oluşturduğu SQLite veritabanları için ayrı, kiracı kapsamlı
  tasarım

Geri yükleme, değiştirme ve silme ayrı bir tasarım gerektirir; bu endpoint'lere
genel dosya işlemleri olarak eklenmemelidir.
