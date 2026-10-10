# v0.1.0-alpha.82 (10 Ekim 2026'da yayımlandı)

[English](RELEASE-NOTES-v0.1.0-alpha.82.md)

Bu, v0.1.0-alpha.81'i izleyen sürümdür. 10 Ekim 2026'da sahip tarafından
yayımlandı (sonda "Yayımlama kaydı"na bakın). Sahip
10 Ekim 2026'da numarasının v0.1.0-alpha.82 olmasına karar verdi. Arayüzün
sunucunun durumunu
bilmediğinde ne gösterdiğini, ayarların ve hizmet işlemlerinin nasıl yazılıp
yanıtlandığını ve aynı değişiklik sunucuya birden çok kez ulaştığında ne
olduğunu değiştirir. Neyin, nasıl ölçüldüğünü ve neyin ölçülmediğini de söyler.

Bir ekranın ne gösterdiği, bileşen testlerine ve API'nin bir taklidine karşı
çalışan bir tarayıcıya dayanır; yalnız güncelleme ve ilk sayfa yüklemesi için,
yedinci kayıtta gerçek bir Panel'e karşı gerçek bir Chrome'a da (aşağıda
"Ekranlar").
v0.1.0-alpha.81'in yayımlandığını sahip bildirmiştir; depoda etiketi var ama
yayımlama adımlarının kaydı yok.

Denetimler, kodun çalışmanın farklı anlarındaki hâllerinde yapıldı. "Ne
ölçüldü" bölümündeki kısa bir tablo hangi denetimin kodun hangi hâline
dayandığını, sınırlar da son kodda hiç çalıştırılmayanı söyler.

## Sunucu sahibi için ne değişiyor

**Güncellemeden sonra açık olan her CelikPanel sayfasını yeniden yükleyin.**
Güncellemeden önce açılmış bir sayfa okumayı sürdürür; ama gönderdiği bazı
değişiklikleri sunucu, sayfa yeniden yüklenene kadar reddeder: yedek alma ya da
geri yükleme, cPanel içe aktarımını uygulama, Let's Encrypt sertifikası isteme,
veritabanı oluşturma, bir veritabanı sunucusunda CelikPanel'in kendi hesabını
kurma, VPN cihazı ekleme; ayrıca posta politikasını, yedek zamanlamasını,
zamanlanmış görevi, catch-all adresini ya da bir yapılandırma dosyasını
kaydetme. Ret iletisi sayfayı yeniden yüklemenizi söyler. Reddedilen istek
sunucuda hiçbir şeyi değiştirmez.

**Arayüz artık bilmediği olumsuz bir durumu söylemiyor.** Bu sürümün dokunduğu
ekranlarda bir okumanın üç durumu var: denetleniyor; denetlenemedi (yeniden
deneme ile); ya da bilinen yanıt. Yavaş ya da başarısız bir okuma artık "DNS
sunucusu yok", "veritabanı sunucusu kurulu değil", boş liste ya da "buraya
yönlenmiyor" diye gösterilmez; form da sunucunun hiç göndermediği varsayılan
değerlerle doldurulmaz. Sayfa yeniden denetlerken yazdıklarınız yerinde kalır.
Her ekran henüz dönüştürülmedi; kalanları sınırlar bölümü adıyla sayar.

**Sekmeden ayrılmak ya da başarısız bir lisans denetimi sayfayı artık baştan
kurmuyor.** Panel erişimi kısa bir süre doğrulayamadığında bulunduğunuz sayfa,
açık penceresi, yazdıklarınız ve seçili sekmesiyle yerinde kalır. Üzerine gelen
bir katman ne olduğunu ve kendiliğinden yeniden denetlediğini söyler; erişim
bilinene kadar hiçbir şey değiştirilemez. Lisans kuralları aynıdır: eksik,
süresi dolmuş ya da geçersiz lisans Paneli yine kapatır.

**Sunucu kurulumu, Panel'in bir kez yeniden başlayacağını önceden söylüyor.**
"Panel erişimini güvenceye al" adımında Panel sertifikasını alır ve onu
kullanmak için yeniden başlar. Sihirbaz bunu artık o adımdan önce ve adım
sırasında söyler, kısa bağlantı kesintisini planlı olarak gösterir ve
kendiliğinden yeniden bağlanır. Sunucu meşgul olduğu için duran kurulum; nedeni,
kimin işlem yapacağını ve sonraki adımı tek yerde gösterir.

**Bir kayıt artık sunucudakini, sayfanın o an gösterdiğiyle ezemez.** Posta
politikası, alan adının yedek zamanlaması, zamanlanmış görevler, catch-all
adresi ile yapılandırma dosyaları (PostgreSQL ve MariaDB olanlar bir sonraki
paragraftaki denetimleri de alır) bir sürümle okunur; kayıt o sürümü geri
taşımak zorundadır. Sayfa yüklendikten sonra sunucudaki
durum değiştiyse ya da sayfa güncel durumu hiç okumadıysa kayıt reddedilir ve
hiçbir şey yazılmaz. Okunamayan durum boş form olarak değil, "okunamadı" olarak
gösterilir. Kendi yazdığınız Postfix alıcı kısıtlamaları yerinde ve sırasında
kalır; Panel yalnız kendi yönettiği DNSBL girdilerini değiştirir. Crontab'da
yalnız görevin kendi satırı değişir; kendi satırlarınız ve açıklamalarınız
kalır.

**Veritabanı yapılandırma dosyası, mevcut dosyanın yerine geçmeden önce
denetlenir.** Önce sunucunun kendi programı yeni dosyanın bir kopyasını okur.
Önceki dosya, tarihli bir kopya olarak yanında saklanır. PostgreSQL yeniden
yüklenir; yeniden yükleme başarısız olursa önceki dosya geri konur ve yanıt
neyin doğrulandığını söyler. PostgreSQL'e yerel yönetici erişimini kaldıracak
bir değişiklik reddedilir. MariaDB dosyalarını yeniden başlatılmadan yeniden
okuyamaz: değişiklik kaydedilir, yanıt yeniden başlatmayı beklediğini söyler ve
Panel MariaDB'yi yeniden başlatmaz. MariaDB olmayan, `mysqld` adlı bir program
MariaDB dosyasının denetimi olarak kabul edilmez; dosya o durumda değiştirilmez
(yalnız bileşen testleri).

**Yeniden yükleme ve hizmet işlemi, hizmetin sonradan gösterdiğine göre
değerlendirilir.** Ubuntu 24.04'te `postfix` adlı birim, Debian ve Ubuntu'da da
`postgresql` adlı birim gerçek hizmeti yalnızca sarar; hizmet yöneticisi,
hizmet ne yapmış olursa olsun onlar için başarı bildirir. v0.1.0-alpha.81 posta
politikası kaydından sonraki yeniden yüklemeyi hiç denetlemiyordu (kaynağından
okundu); bu yüzden Postfix yeniden yüklemeyi reddettiğinde de "kaydedildi"
diyordu. Panel artık hizmetin kendisine sorar; başarısız bir işlem birimi,
hizmetin kendi satırını ve bunu gösteren komutu adıyla verir. Sonucu
belirlenemeyen işlem yapılmış ya da başarısız diye değil, bilinmiyor diye
gösterilir. Çalışmayan bir hizmetin yeniden yüklenmesi reddedilir ve hizmete
hiçbir şey gönderilmez. Postfix'i durdurma, Postfix durduktan sonra yanıtlanır;
systemd ardından birimi "failed" gösteriyorsa yanıt bunu bir notla söyler ve
işareti olduğu gibi bırakır.

**Aynı değişiklik bir kez uygulanır.** Bağlantı sıfırlandığında tarayıcı bir
değişikliği kendiliğinden yeniden gönderebilir. Yinelemenin zarar verdiği sekiz
değişiklikte (API değişiklikleri bölümünde sayılır) her istek artık tek bir
kimlik taşır. Yinelenen istek ilk sonuçla yanıtlanır ve bir daha çalışmaz; ilk
sonuç tek seferlik bir gizli bilgi taşıyorsa yinelemeye değişikliğin zaten
yapıldığı söylenir ve gizli bilgi bir daha gösterilmez. Kabul edildikten sonra
böyle bir değişiklik, sayfa kapatılsa da sonuna kadar sürer.

**cPanel içe aktarımı ne yaptığını söylüyor.** Önizleme artık posta kutusu
parola özetlerini tarayıcıya göndermez. Bir parçasını getiremeyen içe aktarım
"kısmi" olarak bildirilir. Her parça ya aktarıldı, ya aktarılmadı, ya da
dışarıda bırakıldı diye listelenir (seçilmedi, dış DNS sağlayıcınıza bırakıldı,
arşivde ondan bir şey yok ya da hiçbiri aktarılmadı). Site klasörünü `tar`'ın yazdığı biçimde bir
dizin girdisi olarak taşıyan arşiv, dosyalarını yitirmeden içe aktarılır.
Arşivin mutlak yolla adlandırdığı girdi dışarıda bırakılır ve adıyla bildirilir.

**Arch'ta PHP sitesi oluşturulabiliyor.** PHP'ye devir artık, yalnız Debian ve
Ubuntu'nun nginx paketlerinin getirdiği bir dosyayı anmak yerine her sitenin web
sunucusu yapılandırmasına yazılır. Güncelleme, yeni Panel başladığında
barındırılan sitelerin web sunucusu yapılandırmasını yeniden yazar. Paketin
`/etc/nginx/snippets/fastcgi-php.conf` dosyasını düzenlediyseniz siteleriniz
güncellemeyle birlikte bu dosyayı okumayı bırakır; dosya yerinde kalır ve
düzenlemenizi hiçbir şey algılamaz.

**Web sunucusunun reddettiği site, olduğu gibi yanıtlanır.** Site oluşturulurken
ya da içe aktarılırken yanıt "internal server error" yerine nginx'in yapılandırmayı reddettiğini, neyin yeniden
kaldırıldığını ve bunu gösteren komutu söyler.

**Başarısız sertifika isteği nedenini söylüyor.** certbot'un yerine
getiremediği bir Let's Encrypt isteği "internal server error" yerine beş
nedenden biriyle (sertifika otoritesine ulaşılamadı, doğrulama, sınır aşımı,
zaman aşımı, araç hatası), yerinde kalanla ve kimin işlem yapacağıyla
yanıtlanır.

**Posta sertifikası yenileme, gerçekten sunulan sertifikayı denetliyor.**
Yenileme, Postfix ve Dovecot'u yeniden yükledikten sonra onların yerel
dinleyicilerine bağlanır ve sundukları sertifikayı seçtiği sertifikayla
karşılaştırır. Farklıysa ya da hiçbir dinleyici yanıt vermiyorsa yenileme açık
kalır ve başarısız bir yeniden yüklemeden sonra olduğu gibi yeniden denenir:
mevcut üç çalıştırma sınırı içinde, ardından sizi bekler; biri açıkken sonraki
bir yenileme kabul edilmez. Bunun yalnız bileşen testleri var. Bağımsız yenilemesi daha eski bir sürümce
kurulmuş sunucu, elindeki yardımcıyı korur; sınırlar bölümüne bakın.

**Postfix yapılandırması uyarı yazdıran sunucuda posta sertifikası
değişiklikleri.** Sunucunuzda `postconf -n >/dev/null` bir uyarı yazdırıyorsa
(örneğin bir değerden sonra gelen açıklama ya da kullanılmayan bir parametre
için) bu sizi ilgilendirir. v0.1.0-alpha.81'de Agent böyle bir uyarıyı her
posta TLS ayarının değerinin parçası olarak okuyordu. Gerçek `postconf` ile,
özel bir yapılandırma dizininde ölçüldü: o kodun bir değişiklikten önce
sakladığı metin, geri almasının kullandığı komuta verildiğinde, ayarlanmış bir
ayar için reddedildi (o ayar geri yüklenmeyecekti), ayarlanmamış bir ayar için
de uyarı satırının kendisi değer olarak yazıldı. (Eski kodun saklayacağı metin,
komutun iki çıktı akışı kırpılarak yeniden üretildi; eski kodun kendisi
çalıştırılmadı.) Kaynaktan okundu,
çalıştırılmadı: böyle bir sunucuda her posta TLS değişikliği "değişti,
doğrulanmadı" olarak bitiyor, posta sertifikasının yayımı da (yenileme dahil)
duraklatılıyordu. Artık uyarı satırı hiçbir zaman değerin parçası değildir; tek
bir değer olarak okunamayan ayar, değişikliği başlamadan durdurur ve ayarı ile
onu gösteren komutları adıyla verir. Bunun, ölçülen çıktıyı kullanan bileşen
testleri var. Böyle bir dosyayla posta TLS değişikliği, geri alınması ve
sertifika yayımı çalıştırılmadı.

**Güncelleme kartı daha önce ne olduğunu söylüyor.** Sunulan sürüm bu sunucuda
daha önce denenip geri alındıysa kart bunu, denemenin ne zaman bittiğini, şu an
hangi sürümün çalıştığını ve kayıtlı nedeni söyler. Sürüm yine sunulur ve
Başlat kapatılmaz. Bu metin bu sürümün arayüzüne aittir: v0.1.0-alpha.81'e
dönüşten sonra sunucu alpha.81'in kartını gösterir.

**Panelin "yalnız güvenli bağlantı" kuralı yalnız Panelin kendi adresini
kapsıyor.** Panel, HTTPS üzerinde tarayıcıya bir yıl boyunca hem Panelin ana
makine adında hem de onun altındaki her adda düz HTTP'yi reddetmesini
söylüyordu: başlık `max-age=31536000; includeSubDomains` idi. Başlığın
tanımına (RFC 6797) göre, Paneli `example.com` adresinde açmış bir tarayıcı
böylece Panelin sunmadığı `shop.example.com` adında da düz HTTP'yi
reddediyordu. Başlık artık yalnızca `max-age=31536000`; yani kural yalnız
Panelin kendi ana makine adını kapsıyor (süresi yine bir yıl). Aynı tanıma göre
tarayıcı yeni kuralı Panele bir sonraki girişinde alır; Paneli bir daha
açmayan tarayıcı eski kuralı, o tarayıcının ayarlarından silinmedikçe, bir yılı
dolana kadar tutar. Tarayıcıdaki bu davranışların hiçbiri bir tarayıcıda
ölçülmedi. Panelin barındırılan siteler için ürettiği web sunucusu
yapılandırması, v0.1.0-alpha.81'de ve bu sürümde geniş kuralı taşımıyor (şablonundan
ve geniş biçimi yasaklayan bir testten okundu; çalışan bir sitede ölçülmedi;
daha önceki sürümlere bakılmadı). Başlık, laboratuvar makinelerinde (Arch,
Debian 13 ve Ubuntu 24.04) çalışan Panelden okundu: bu sürümün kodu okunan her
HTTPS yanıtında dar başlığı gönderdi, v0.1.0-alpha.81 geniş olanı gönderdi;
Panelin bağlantı noktasına düz HTTP ise böyle bir başlık olmadan `400` ile
yanıtlandı.

### Yapmanız gerekebilecekler

- Güncellemeden sonra açık CelikPanel sayfalarını yeniden yükleyin.
- API'yi çağıran betikleriniz varsa güncellemeden önce "API değişiklikleri"
  bölümünü okuyun.
- Posta kurulu bir sunucuda `postconf -n >/dev/null` çalıştırın. Uyarı
  yazdırıyorsa `main.cf` içinde adını verdiği satırı düzeltin; bu, yukarıda
  anlatılan nedeni her sürümde ortadan kaldırır.
- Posta kurulu bir sunucuda `sudo postfix check`, Postfix'in yapılandırmasını
  şu an kabul edip etmediğini gösterir. Hata yazdırıyorsa daha eski bir sürümle
  kaydedilmiş posta politikası yürürlükte olmayabilir; adını verdiği satırı
  düzeltin, sonra `sudo postfix reload` çalıştırın.
- Panel'de bir MariaDB ayarını değiştirdikten sonra MariaDB'yi size uygun bir
  zamanda kendiniz yeniden başlatın; o zamana kadar eski değer yürürlüktedir.

## API değişiklikleri

Panel ve Agent tek bir sürümle birlikte kurulur ve aşağıdaki kayıtlar iki
tarafı da gerektirir: farklı sürümlerden bir Panel ile Agent bu kayıtları
yapamaz.

### Sürüm taşımak zorunda olan kayıtlar

Her okuma `version` döndürür. Kayıt onu JSON gövdesinde, `DELETE` için de
`version` sorgu parametresinde geri gönderir.

| Kayıt | Rette `reason` |
| --- | --- |
| `PUT /api/v1/mail/policy` | `mail_policy` |
| `PUT`, `DELETE /api/v1/domains/{id}/backups/schedule` | `backup_schedule` |
| `POST`, `PUT`, `DELETE /api/v1/domains/{id}/cron` | `scheduled_tasks` |
| `PUT`, `DELETE /api/v1/domains/{id}/mail/catch-all` | `mail_catch_all` |
| `POST /api/v1/config` (`path`, `content`, `version`) | `config_file` |

- Sürüm yoksa: `409 SETTINGS_VERSION_REQUIRED`. Hiçbir şey yazılmaz.
- Sürüm artık sunucudaki değilse: `409 SETTINGS_CHANGED`. Hiçbir şey yazılmaz;
  yeniden okuyup yineleyin.
- Güncel durum okunamıyorsa: posta politikası, zamanlanmış görevler ve
  yapılandırma dosyaları için okumada da kayıtta da
  `502 CURRENT_SETTINGS_UNREADABLE`. (Yedek zamanlaması ve catch-all Panel'in
  kendi veritabanında tutulur; orada başarısız bir okuma olağan bir sunucu
  hatasıdır.) Zamanlanmış görevlerde, neden doğrulandıysa `detail` `cron_allow`
  ya da `cron_deny` olur; `vars.detail` sunucunun kendi programının yazdığı
  satırı taşır.
- Zamanlanmış görevler: bir görevin `id` değeri artık görevin kendi satırından
  türetilen 16 onaltılık karakterdir; eski bir sürümden kalma `id` kullanmak
  yerine listeyi yeniden okuyun. Aynı görev ikinci kez eklenirse
  `409 CRON_JOB_DUPLICATE`; crontab'da iki satırda duran görev için
  `409 CRON_JOB_AMBIGUOUS`.
- Posta politikası: `200` yanıtı `applied` taşır (`reloaded`, `not_running`,
  `unchanged`, `unchanged_reloaded`). `reason` değeri `check`, `reload` ya da
  `verify` olan `502 MAIL_POLICY_NOT_RELOADED`, dosyanın değerleri tuttuğunu ve
  Postfix'in onları almadığını söyler; gövde yazılan `policy` ile, bu istek
  dosyayı değiştirdiyse, `mutation_applied` taşır.
  `502 MAIL_POLICY_RELOAD_UNKNOWN`, dosyanın değerleri tuttuğunu ve Postfix'in
  onları alıp almadığının bilinmediğini söyler.
  `409 MAIL_POLICY_RESTRICTIONS_UNMANAGED`, Panel'in kısıtlamalarınızda
  yapmayacağı bir DNSBL değişikliğini reddeder; `400 MAIL_POLICY_INVALID`,
  izin verilen aralığın dışındaki bir değeri ya da düz bir ana makine adı
  olmayan bir DNSBL bölgesini reddeder.
- Yapılandırma dosyaları: `200` yanıtı `version`, `unchanged`, `backup`,
  `applied`, `daemon_check` ve `restart_required` taşır. `reason` değeri
  `empty`, `shape`, `syntax`, `daemon`, `lockout` ya da `no_validator` olan
  `422 CONFIG_INVALID`, hiçbir şeyin yazılmadığını söyler.
  `502 CONFIG_RELOAD_FAILED` için `reason`: `restored`,
  `restored_unit_reload_failed`, `restored_running_unknown` ya da
  `not_restored`.
- `GET /api/v1/postfix/queue` ve `GET /api/v1/postfix/summary`: okunamayan
  kuyruk boş liste olarak değil, `502 MAIL_QUEUE_UNREADABLE` olarak yanıtlanır
  (doğrulandığında `reason` `postfix_config`).

### Kimlik taşımak zorunda olan istekler

Sekiz rota, hepsi `POST`:

- `/api/v1/domains/{id}/backups`
- `/api/v1/domains/{id}/backups/restore`
- `/api/v1/domains/{id}/databases`
- `/api/v1/domains/{id}/ssl/letsencrypt`
- `/api/v1/database-servers/{id}/databases`
- `/api/v1/database-servers/{id}/admin-account`
- `/api/v1/vpn/peers`
- `/api/v1/import/cpanel/apply`

`X-CelikPanel-Request-Id` başlığını gönderin: 32 küçük harfli onaltılık
karakter, her işlem için yeni bir değer. Geçerli bir kimlik taşıyan isteğin
yanıtı başlığı yineler (eksik ya da bozuk başlık reddi yineleyemez).

| Durum | Yanıt |
| --- | --- |
| Başlık yok | `428 REQUEST_ID_REQUIRED`; hiçbir şey çalışmadı |
| Başlık bu biçimde değil | `400 REQUEST_ID_REQUIRED`; hiçbir şey çalışmadı |
| Aynı kimlik, aynı istek, ilki bitmiş | ilk yanıtın aynısı, `X-CelikPanel-Request-Replayed: 1` ile; hiçbir şey çalışmaz |
| Aynı kimlik; başka gövde, yol, sorgu ya da kullanıcı | `409 REQUEST_ID_REUSED`; hiçbir şey çalışmadı |
| İlki 20 saniyelik beklemeden sonra hâlâ çalışıyor | `409 REQUEST_IN_PROGRESS`; yeniden başlatılmadı |
| İlki çalışırken Panel yeniden başladı ya da başarısız oldu | `409 REQUEST_OUTCOME_UNKNOWN`; o kimlikle bir daha asla çalıştırılmaz |
| İlki bitti, yanıtı saklanmıyor | `409 REQUEST_COMPLETED_RESULT_NOT_RETAINED` (ilki hatayla bittiyse `reason` `failed`) |

Tek seferlik gizli bilgi taşıyan yanıt (veritabanı hesabı ve VPN cihazı
rotalarında her zaman; bir veritabanı rotasında kendi ürettiği parolayı
döndürdüğünde) ya da 64 KiB'den büyük yanıt saklanmaz. Bir kimlik 24 saat
hatırlanır. 1 MiB'den büyük istek gövdesi `413` ile reddedilir. Bir alan adının
geri yüklemesi sürerken ikincisi `409 BACKUP_RESTORE_IN_PROGRESS` ile
reddedilir. Diğer bütün rotalar, başlık olsun olmasın, eskisi gibi davranır.

### cPanel içe aktarımı

- `POST /api/v1/import/cpanel/inspect`: `mail_accounts` içindeki her girdi
  `has_password` taşır; `crypt_hash` artık döndürülmez.
- `POST /api/v1/import/cpanel/apply`; ya `status: "active"` ile `200`, ya da
  `status: "partial"`, `code: "IMPORT_PARTIAL"` ve `message` ile `200`
  yanıtlar. Artık `status: "pending"` ile `202` yanıtlamaz. İkisi de
  `domain_status`, `steps` ve üç liste taşır; her parça bunlardan tam birinde
  yer alır:
  - `imported`;
  - `not_imported`: adım başarısız oldu (`ok: false`); bir içe aktarımı yalnız
    bu kısmi yapar;
  - `left_out`: adım hatasız bitti ve hiçbir şey aktarmadı. Böyle bir adımda
    `ok: true` ve bir `state` bulunur: `left_to_owner` (DNS'i dış sağlayıcıda
    olan sunucunun DNS'i), `not_chosen`, `none_in_archive` ya da
    `none_imported`.
- Mutlak yolla adlandırılmış arşiv girdisi, `ok: false` taşıyan bir
  `member:<ad>` adımıdır; böyle 20 girdiden fazlası için tek bir `members:<n>`
  adımı kalanları sayar. Yalnız böyle girdiler eksikse içe aktarım
  `domain_status: "active"` ile `partial` olur; başka durumda kısmi içe aktarım
  `domain_status: "pending"` bırakır.
- Site oluşturulamadıysa arşivden hiçbir şey içe aktarılmamıştır: nginx sitenin
  yapılandırmasını reddettiyse `502 SITE_WEB_SERVER_REFUSED` (`reason`:
  `import_removed` ya da `import_cleanup_unconfirmed`), başka durumda
  `502 IMPORT_SITE_NOT_CREATED`.

### Siteler

- `POST /api/v1/domains/create`: nginx yeni sitenin yapılandırmasını
  reddettiğinde `500` yerine `502 SITE_WEB_SERVER_REFUSED`; `reason` değeri
  `removed` (site için oluşturulanlar yeniden kaldırıldı ve bu doğrulandı) ya
  da `cleanup_unconfirmed`; `vars.domain`, `vars.command` ve yönetici için
  nginx'in satırını taşıyan `details`.
- `POST /api/v1/domains/create`: sunucunun çalıştırmadığı, adıyla istenen bir
  PHP sürümü, hiçbir şey oluşturulmadan `409 PHP_VERSION_NOT_INSTALLED` olur.
- `GET /api/v1/domains`: `php_version`, site için kaydedilmiş PHP sürümüdür;
  sitesi olmayan alan adında boştur (orada eskiden sabit `8.3` yanıtlanırdı).

### Hizmet işlemleri

`POST /api/v1/service/action`. Postfix, Dovecot ve bir hizmeti yalnızca saran
birimlerden (Ubuntu'da `postfix`; Debian ve Ubuntu'da `postgresql`) gelen
başarılı yanıta `outcome` ve `applied` eklenir; yanıtın arkasında tek bir birim
duruyorsa `unit` de eklenir. Diğer hizmetler başarıyı eskisi gibi yanıtlar.
Başarısız işlem eskiden `500` yanıtlıyordu; artık:

- `reason` değeri `check`, `reload`, `start`, `stop`, `verify`, `command`,
  `reload_reread` ya da `reload_not_reread` olan `502 SERVICE_ACTION_FAILED`
  (son ikisi yalnız PostgreSQL için);
- `reason` değeri `not_running` olan `409 SERVICE_ACTION_FAILED`: birimi durmuş
  ya da başarısız olan bir hizmetin yeniden yüklenmesi (durum hiçbir şey
  gönderilmeden önce okunur); ona hiçbir şey gönderilmedi;
- `502 SERVICE_ACTION_UNKNOWN`: gönderildi, sonuç belirlenemedi.

Hepsi `vars.unit`, `vars.action`, `vars.command`, hizmet bir şey söylediyse
`vars.detail` ve hizmeti başka bir birim çalıştırıyorsa `vars.owner_unit`
taşır.

Başarılı bir Durdur `note` taşıyabilir (`code` `SERVICE_ACTION_NOTE`):

- `reason` `unit_marked_failed` ya da `unit_marked_failed_config`: systemd,
  durdurmadan sonra birimi "failed" gösteriyor. `vars`: `unit`, `failed_unit`,
  `result`, `command`; ikinci neden için ayrıca `detail` (hizmetin kendi
  satırı).
- `reason` `unit_not_settled` ya da `unit_state_not_read`: birim hâlâ iki durum
  arasındaydı ya da okunamadı. `vars`: `unit`, `pending_unit`, `command`;
  ilki için ayrıca `state`.

Postfix'i durdurma, ana süreci bittiğinde yanıtlanır; bir birim iki durum
arasındayken Durdur yaklaşık 15 saniyeye kadar daha uzun sürebilir.

### Sertifikalar ve veritabanları

- `POST /api/v1/domains/{id}/ssl/letsencrypt`: certbot'un çalışıp yerine
  getiremediği istek `502 CERTIFICATE_ISSUE_FAILED` olur; `reason`:
  `authority_unreachable`, `validation`, `rate_limited`, `timeout` ya da
  `tool`; `vars.kept` `none` ya da `previous` olur; `vars.detail` (certbot'un
  satırı) yalnız yöneticilere gönderilir.
- `POST /api/v1/database-servers/{id}/databases` ve
  `POST /api/v1/database-servers/{id}/users`: çağıranın gönderdiği parola geri
  gönderilmez; yanıtta `password_set: true` bulunur. Sunucunun ürettiği parola
  yine bir kez, `password` olarak döndürülür. (`users` yanıtı her iki durumda da
  `password_set: true` taşır.)

Bu yanıtlardan hangilerinin gerçek bir sistemde üretildiği, hangilerinin yalnız
bileşen testi olduğu sonraki iki bölümde ve sınırlarda yazılıdır.

## Fark edeceğiniz davranış değişiklikleri

- Durmuş bir Postfix, posta politikası kaydıyla başlatılmaz. Değerler yazılır
  ve yanıt Postfix'in çalışmadığını söyler.
- Posta politikasını değişiklik yapmadan kaydetmek, çalışan Postfix'i yeniden
  yükler; böylece "kaydedildi ama yeniden yüklenmedi" durumundan sonraki kayıt
  onu uygular.
- MariaDB ayarı, sizin yapacağınız yeniden başlatmayı bekler.
- Bileşenler sayfasında Başlat ve Durdur, yalnız bileşenin kendi kaydı hizmet
  birimini adıyla veriyorsa; Kur da yalnız paket deposunun yanıtı bilindiğinde
  sunulur.
- Durmuş bir hizmetin yeniden yüklenmesi reddedilir ve hiçbir şeyi başlatmaz.
- Yapılandırması reddedilen Postfix durdurulabilir. Yanıt Postfix durduğunda
  gelir ve systemd birimi "failed" işaretlediğinde bir not taşır. Eskisinden
  geç gelir: Ubuntu 24.04'te istek düzeltmeden önce 1,9–3,3 saniye, sonra
  4,2–5,8 saniye sürdü (laboratuvarın farklı makinelerinde beş ve dört
  durdurma).
- Yalnız genel adres denetiminde olan (Panel'in adının sunucuya çözülmesini
  bekleyen) sunucu kurulumu, ilgisiz hizmet işlemlerini artık reddetmez; diğer
  DNS adımlarında bekleyen kurulum tasarım gereği hâlâ reddeder.
- Güncelleme, yeni Panel başladığında barındırılan sitelerin web sunucusu
  yapılandırmasını yeniden yazar.
- DNS motoru kartı zamanlayıcısında yalnız okur. Bir işlem bir süredir
  doğrulanamadıysa, sizin başlatacağınız "Şimdi kontrol et"i sunar.
- Panel sertifikası alındıktan sonra sayfa güvenli adrese yalnız isteği yapan
  sekmede, "Burada kal" seçeneği olan bir bildirimden sonra geçer.
- Yanıt kaybolduğunda sayfa hiçbir şeyi yeniden göndermez. Durumu okur ve o
  okuma yanıtlanana kadar denetimlerini kapalı tutar.
- Telefonda, diğer sütunlar kayarken satırın işlemleri erişilebilir kalır (DNS
  kayıtları, alan adları, veritabanları, yasaklı adresler).
- Veritabanları sayfası, sunucunun bildirdiği MariaDB sürümünü gösterir.
- Devre dışı bir zamanlanmış görev etkinleştirilebilir, düzenlenebilir ve
  silinebilir; bir görevi silmek artık üstündeki satırı kaldırmaz.

Postfix, Dovecot, MariaDB, durmuş hizmetler, yeniden yazılan site
yapılandırması, DNS'i bekleyen kurulum ve zamanlanmış görevlerle ilgili maddeler
gerçek hizmetlerde ölçüldü (sonraki bölüm). Bir ekranın ne gösterdiğiyle ilgili
maddeler yalnız bileşen testlerinde ve API'nin bir taklidine karşı tarayıcıda
denetlendi.

## Ne ölçüldü, nasıl ölçüldü

8, 9 ve 10 Ekim 2026'da (UTC; altıncı koşu yerel saatle gece yarısından sonra, 10
Ekim 2026'da yapıldı; yedincisi etiketten sonra, yayımlanmış kodla 10 Ekim
2026'da yapıldı) alınmış dokuz kayıt. Yedisi, ürünün tek bir dizüstü ana
makinedeki geçici sanal makinelerde, Debian 13, Ubuntu 24.04 ve Arch'ın paketli
hizmetleriyle koşularıdır (yedincisi yalnız Debian 13'te); biri dördüncünün yeni kurulmuş makinelerdeki ek
ölçümüdür; biri de tek bir programın okumasıdır. Bir sayı verilmedikçe her sıra
platform başına bir kez koştu; bir hücre yalnız test düzeneğinin bir hatasından
sonra yinelendi, aşağıdaki bir paragraf başka türlü söylemedikçe (üçüncü
koşuda ikinci bir Arch okuması; dördüncüde çalışan sürüm olarak adayın kaynağını
kullanan bir hücre). İlk altı kayıtta hiçbir ekran çizilmedi: sürücü, ekranların
gönderdiğini gönderdi. Gerçek bir Panel'in önüne gerçek bir tarayıcıyı yalnız
yedinci kayıt koydu. Her sonuç dosyası kendisini henüz kabul edilmemiş kanıt
olarak işaretler; değerlendirmesi sahibindir.

**Laboratuvarın saptadığı ve saptamadığı.** Sanal makineler ağdan kopuk
değildir: dışarıya doğru NAT yapan QEMU kullanıcı ağıyla çalışırlar ve
dağıtımların paketleri koşu sırasında kendi depolarından kurulur. Bir
makinenin neye erişebildiğini veya erişemediğini burada hiçbir şey göstermez.
Saptanan daha dardır. Sürüm kaynağı, makinenin
hosts dosyasında kendi loopback adresine sabitlenmiştir. Birinci koşuda hiçbir
Let's Encrypt adı sabitlenmedi ve hiçbir sertifika rotası çağrılmadı. İki
Let's Encrypt adı, sertifika rotası çağrılmadan önce aynı biçimde sabitlenir
(ikinci ve üçüncü koşu); dördüncü koşuda ve ek ölçümünde bu adlar yalnız yeni
kurulum hücrelerinde, kurulumdan ve sunucu kurulumundan sonra gelen bir adımda
sabitlendi, güncelleme hücrelerinde sabitlenmedi ve oralarda hiçbir sertifika
rotası çağrılmadı; beşinci koşuda
ürünün bildiği dört sertifika otoritesi adı her hücrenin ilk adımında
sabitlendi ve hiçbir sertifika rotası çağrılmadı. Altıncı koşuda sürüm
kaynağının adı da o ilk adımda sabitlendi; beş ad, üç platformda da hem hosts
dosyasından hem makinenin olağan ad çözümleme yolundan geri okundu (Arch'ta bu
yol hosts dosyasından önce çözümleyiciye sorar); her yanıt makinenin kendi
loopback adresiydi. Çözümleyicinin kendi DNS işlem sayacı her hücrenin sonunda
ilk adımdakinden yüksekti (örneğin Debian 13'te ilk adımda 6, sonda 146); yani
makineler o koşu sırasında DNS soruları sordu; hangi adları sordukları
kaydedilmedi. Lisans adımını,
bir lisans hizmetine başvurmadan bir test derlemesi yanıtlar. Hiçbir sürücü
kurulu bir sunucuya yöneltilmedi ve koşuların ham kayıtlarında kurulu hiçbir
sunucunun adı ya da adresi yer almaz. Makinelerin trafiği kaydedilmedi. Derlemeler üretim
anahtarını değil, bir deneme imzalama anahtarını taşır.

**Adlar ve tarihler.** Dizinler `deploy/e2e/release-recovery/evidence/`
altındadır. `set1-20261010`, `set2-20261011` ve `set3-20261012`, koşu günü
olmayan etiketler taşır: birincisi 8 Ekim 2026'da, diğer ikisi 9 Ekim 2026'da
koştu. `set4-20261009`, `set4b-20261009`, `set4c-20261009`, `set5-20261009`,
`set6-20261009` ve `set7-20261010` gerçek tarihi (UTC) taşır. `set7-20261010`
bu yazılırken henüz commit edilmemişti ve kabul denetimi bitmemişti.

**Kanıtın saklanması.** Dizinler depoda baytı baytına saklanır; önceki
dizinlerin on iki ana makine okuma dosyası, toplandıkları hâliyle yeniden
saklandı. Toplamadan sonra iki değer karartıldı. `set2-20261011` içinde, içe
aktarım önizlemesinin geri yanıtladığı 30 posta kutusu parola özeti
değiştirildi; bunları laboratuvarın içe aktarım düzeneği üretmişti ve hiçbir
parolanın özeti değildir. `set4-20261009` içinde tek kullanımlık bir güncelleme
işlemi belirtecinin özeti değiştirildi; daha önce saklanmış olan
`set3-20261012` aynı alanı karartılmamış taşır. Bu, artık var olmayan bir
makinenin laboratuvar belirtecidir. Alım sırasında `set4b-20261009` içindeki üç
ana makine okuma dosyası, sağlama toplamları kaydedilmeden önce CRLF'den LF'ye
çevrildi; `set4-20261009` ve `set4b-20261009` içindeki bazı betiklerde yerel
geçici klasör yolu bir yer tutucuyla değiştirildi. Üçüncü koşudan itibaren sürücüler özet
biçimli değerleri toplarken kaldırır; beşinci koşu belirteç özetlerini de o
anda kaldırır.

**Ayar kayıtları (birinci koşu; Debian 13, Ubuntu 24.04, Arch; posta politikası
ve catch-all yalnız Debian 13 ve Ubuntu 24.04'te).** Crontab'ı olmayan kullanıcı üçünde de bilinen boş listedir. Elle yazılmış crontab
satırları ekleme, devre dışı bırakma, düzenleme, etkinleştirme ve silme boyunca
yerinde kaldı. Eskimiş sürümle yapılan kayıt her kaynakta reddedildi ve hiçbir
şey değişmedi. Sürümsüz kayıt burada yedek zamanlaması ve catch-all için,
aşağıdaki güncelleme hücrelerinde de zamanlanmış görevler ve posta politikası
için denendi ve reddedildi; yapılandırma dosyası için yalnız bileşen testleri
var. Bir PostgreSQL ayarı tek satırı değiştirdi, tarihli bir kopya bıraktı ve
yeniden başlatma olmadan yürürlüğe girdi; geçersiz değer, boş içerik ve yerel
erişimi olmayan bir `pg_hba` reddedildi. MariaDB yeniden başlatılmadı. Yazılan
her dosyanın sahibi, grubu ve izinleri değişmedi. Debian 13 ve Ubuntu 24.04'te
sahibin Postfix kısıtlamaları öğelerini ve sırasını korudu. Koşu iki kusur
buldu: Ubuntu 24.04'te politika kaydı, Postfix yeniden yüklenmediği hâlde başarı
yanıtladı; ve üç platformda da, iki kez başarısız olan bir veritabanı
yapılandırması yeniden yüklemesinden sonra (laboratuvarda sahibin kendi birim
drop-in'iyle oluşturuldu) yanıt, önceki dosya geri konduğu hâlde konamadığını
söyledi.

**Düzeltmeler, hizmet işlemleri, istek kimliği (ikinci koşu).** Debian 13 ve
Ubuntu 24.04'te iki kusurun da düzeltildiği ölçüldü: Ubuntu'daki kayıt artık
`MAIL_POLICY_NOT_RELOADED` yanıtlıyor; başarısız yeniden yükleme de önceki
dosyanın yerinde olduğunu yanıtlıyor ve dosya öncekiyle aynı. Durmuş Postfix
kayıttan sonra durmuş kaldı. Platform başına nginx, MariaDB, Dovecot, Postfix
ve PostgreSQL üzerindeki 41 hizmet işleminden 40'ının yanıtı hizmetin
gösterdiğiyle eşleşti, biri bilinmiyor diye yanıtlandı. Debian 13, Ubuntu 24.04
ve Arch'ta korunan her değişiklik art arda üç kez ve aynı anda üç kez
gönderildiğinde sunucuda tek etki bıraktı (sertifika rotasında bu, işleyicinin
bir kez çalışıp başarısız olması demektir, çünkü laboratuvarda sertifika
otoritesi yok); başlıksız istek `428`, başka gövdeli istek
`409 REQUEST_ID_REUSED` yanıtladı ve hiçbir şey değişmedi. Arch'ta buna
içe aktarım dahil değildi; o sırada orada başarısız oluyordu (aşağıda).
Bağlantısı kesilen geri yükleme sunucuda tamamlandı ve yinelemesi saklanan
yanıtı aldı. Geri yükleme sırasında yeniden başlatılan Panel isteğin bitmesini
bekledi ve istemci yanıtını aldı. Sonlandırılan Panel isteği "yarıda kesildi"
olarak bıraktı ve yinelemesi `REQUEST_OUTCOME_UNKNOWN` yanıtladı; Agent üç
platformda da geri yüklemeyi tamamlamıştı. Koşunun gösterdiği ya da gönderdiği
21 gizli bilginin hiçbiri saklanan bir satırda bulunmadı.

**İkinci düzeltmeler (üçüncü koşu; Debian 13 ve Ubuntu 24.04).** Platform başına
42 hizmet işleminin tamamı hizmetle eşleşti, hiçbiri bilinmiyor olmadı; durmuş
Postfix ve Dovecot'un yeniden yüklenmesi, yapılandırması reddedilen Postfix'in
durdurulması ve PostgreSQL'in iki yeniden yükleme nedeni buna dahildir. Kurulum
DNS'i beklerken hiçbir ret olmadı. İçe aktarım önizlemesinde, korunan
yanıtlarda, saklanan satırlarda ve Panel ile Agent'ın günlük satırlarında özet
biçimli hiçbir değer bulunmadı; bu Arch'ta da böyleydi. İçe aktarılan posta
kutusu özgün parolasını kabul etti, yanlış parolayı reddetti. Dizin girdisi
taşıyan arşiv eksiksiz içe aktarıldı; başarısız dosya adımı, listeleri sunucuyla
eşleşen kısmi bir sonuç olarak yanıtlandı. Bir PHP sitesi oluşturuldu, PHP
çalıştırdı ve silindi. Bir sertifika isteği `CERTIFICATE_ISSUE_FAILED` /
`authority_unreachable` yanıtladı, Arch'ta da; bu hücrelerde Let's Encrypt
adları makinenin kendisini gösteriyordu. Gösterilen MariaDB sürümü üç platformda
da sunucunun sürümüydü. Veritabanı parolası denetimi Debian 13 ve Arch'ta geçti;
Ubuntu 24.04'te iki denetimi test düzeneğinin bir hatası yüzünden düştü, Ubuntu
yeniden koşulmadı, kaydedilen yanıtlar beklenen yanıtlardı. Bu koşuda Arch'ta
PHP sitesi oluşturulamadı; bu yüzden orada hiçbir cPanel içe aktarımı
başlamadı. Yayımlanmış v0.1.0-alpha.81 de orada PHP sitesi oluşturamaz
(kaynağından okundu). Sahibin, site oluşturmayı durduran tek nginx dosyasını
elle koyduğu yeni bir makinedeki ikinci bir Arch okuması her bölümü geçti; bu,
nedenin ne olduğunu gösterir, ürünün olağan bir Arch makinesinde nasıl
davrandığını değil.

**Son düzeltmeler, yeni kurulmuş sunucularda (dördüncü koşu, `set4-20261009`;
Arch, Debian 13, Ubuntu 24.04; on iki hücre).**

- Arch dahil üç platformun da olağan bir sunucusunda Panel üzerinden PHP sitesi
  oluşturuldu. Bir PHP sayfası sitenin kendi hesabıyla çalıştı, olmayan bir
  betik 404 yanıtladı ve site silindikten sonra ondan hiçbir şey hizmet
  vermeyi sürdürmedi. Kaydedilen PHP sürümü ve soketi, kurulu sürümünkiydi.
- cPanel içe aktarımı üçünde de tamamlandı ve içe aktarılan site arşivin
  sayfasını sundu.
- Durmuş nginx, MariaDB ve PostgreSQL'in yeniden yüklenmesi üçünde de `409` /
  `not_running` yanıtladı; birime hiçbir şey gönderilmedi.
- Web sunucusunun reddettiği site üçünde de, ikişer okumada,
  `502 SITE_WEB_SERVER_REFUSED` / `removed` yanıtladı (birinde sahip, üretilen
  yapılandırmanın içerdiği bir nginx dosyasını taşımıştı; ötekinde site adı 58
  karakterdi); yanıtın kaldırıldığını söylediği şeyler sunucuda yoktu.
- Mutlak adlı bir arşiv girdisi listelendi, içe aktarım `partial` oldu, geri
  kalanı aktarıldı ve alan adı hizmetteydi; üçünde de.
- Yayımlanmış v0.1.0-alpha.81'in oluşturduğu bir PHP sitesi, Debian 13 ve
  Ubuntu 24.04'te: on istek güncellemeden önce, güncellemeden sonra ve sitenin
  ayarları yeniden kaydedildikten sonra aynı durum kodunu ve aynı gövdeyi
  verdi. Güncellemenin kendisi, yeni Panel başladığında sitenin web sunucusu
  yapılandırmasını yeniden yazmıştı.
- Postfix'in reddettiği bir yapılandırmayla Postfix'i durdurma: Debian 13'te
  yanıt notu taşıdı. Ubuntu 24.04'te, birim "failed" olarak bittiği hâlde, iki
  koşuda da taşımadı. Bu, koşunun geçmeyen tek denetimiydi.
- Arch'ta otomatik dönüşten sonra (yayımlanmış v0.1.0-alpha.81'e dönen iki
  hücre, adayın kendi derlemesine dönen bir hücre) güncelleme denetimi, önceki
  denemeyi kaydederek sürümü yine sundu.

**Ubuntu'da Postfix'i durdurma: neden ve düzeltme (ek ölçüm,
`set4b-20261009`).** Ubuntu 24.04'te, düzeltmeden önce, beş durdurmanın beşi de
notsuz yanıtlandı. İzler nedenini gösterir: `postconf`, reddedilen satırla
ilgili bir uyarıyı kuyruk diziniyle birlikte yazdırdı ve Agent ikisini tek bir
yol olarak aldı. O adın altında süreç dosyasını bulamadı, Postfix'i ilk
bakışta durmuş saydı ve birimi, Postfix durmadan yaklaşık 0,9 saniye önce,
systemd onu hâlâ "durduruluyor" gösterirken okudu. Yanıtın kendisi daha sonra,
istekten 1,9–3,3 saniye sonra gönderildi. Düzeltmeden sonra, yeni kurulmuş bir
Ubuntu 24.04 ve yeni kurulmuş bir Debian 13 makinesinde dörder durdurma, başarısız
birimi adıyla veren notla birlikte `200` yanıtladı; işareti hiçbir şey silmedi;
yapılandırma geri yüklendikten sonra Başlat çalıştı. İkisinde de, DNS'i dış
sağlayıcıda kalan bir içe aktarım `dns` parçasını `left_out` altında listeledi.
Burada ölçülen derleme, düzeltmeler commit edilmeden önce yapıldı; kayıt, ürün
dosyalarının commit edilenlerle aynı olduğunu gösterir.

**`postconf` ne yazdırıyor (okuma, `set4c-20261009`).** Ürünün bir koşusu
değildir. Postfix 3.10.13'ün gerçek `postconf`'u, geçici olmayan bir geliştirme
makinesinde özel bir yapılandırma dizinine karşı çalıştırıldı; sistemin kendi
yapılandırması ne okundu ne değiştirildi. Uyarının ve değerin hangi çıktıya
yazıldığını ve eski geri almanın komutunun, eski kodun sakladığı metinle ne
yaptığını gösterdi (sonuç yukarıda, sahip bölümündedir).

**Yayımlanmış v0.1.0-alpha.81'den güncelleme (üçüncü koşu; on hücre, hepsi
beklenen sonuca ulaştı).** Başlangıç sürümü, etiketin kendi kaynağının hiç
değiştirilmeden, laboratuvarda deneme lisansıyla derlenmiş hâliydi; imzalı arşiv
değildi. Aday, bu sürümün son düzeltmelerden önceki hâliydi.

- İyi güncelleme Debian 13, Ubuntu 24.04 ve Arch'ta doğrulandı. Ardından
  veritabanı şeması yeni şemaydı; güncellemeden önce açılmış bir sayfanın
  gönderdiği biçimde gönderilen istek reddedildi (`428`,
  `409 SETTINGS_VERSION_REQUIRED`) ve hiçbir şeyi değiştirmedi; aynı istekler
  başlık ve sürümle çalıştı. Debian ve Ubuntu'da Panel'in ertelenmiş posta işi
  tamamlandı.
- Geçişi başarısız olan aday, kurtarma sırasındaki ikinci bir arızayla birlikte
  (Debian ve Ubuntu'da makine sıfırlama, Arch'ta sonlandırılan kurtarma işlemi)
  otomatik olarak v0.1.0-alpha.81'e döndü. Şema önceki şemaydı; 65 tablo,
  kendiliğinden değişen ikisi dışında, güncellemeden önceki durumuna eşitti.
  Bu hücrelerde aday veritabanının bir kopyasını geçirdi ve yayımlanmadan önce
  başarısız oldu; yani canlı veritabanı hiç geçirilmedi. alpha.81'in kurtarma
  durumu, root komutu ve güncelleme kartının metni (o sürümün kendi
  kurallarından üretildi, tarayıcıda görülmedi) önceki sürümün geri
  yüklendiğini söyledi.
- Başlangıç denetiminden geçemeyen aday aynı biçimde döndü (yalnız Debian 13).
  Burada güncelleyici, denetim başarısız olmadan önce geçirilmiş veritabanını
  yayımlamıştı ve dönüşten sonra hizmet veren önceki veritabanıydı; canlı
  veritabanının aradaki sürede yeni şemada olduğu güncelleyicinin sırasından
  çıkar, çünkü canlı defter o sırada okunmadı.
- Üç denemeden sonra duraklayan güncelleme, ürünün sahip için yazdırdığı tek
  yeniden deneme komutuyla, test sürücüsünün çalıştırmasıyla tamamlandı
  (Debian 13, Ubuntu 24.04). Duraklamaya, Panel'in portunu tutan bir
  laboratuvar süreci yol açtı; sürücü portu yeniden denemeden önce bıraktı.
- Panel ve Agent yeniden açılış boyunca kapalıyken site, veritabanı ve SMTP
  hizmet verdi, cron çalıştı ve güvenlik duvarı kuralları yerindeydi (yalnız
  Debian 13).

O koşunun ölçülen yollarında v0.1.0-alpha.81 ile hiçbir uyumsuzluk bulunmadı.
Dördüncü koşu, Debian 13 ve Ubuntu 24.04'te iyi güncellemeyi, Arch'ta da
otomatik dönüşü, dördüncü koşunun koduyla yineledi.

**Güncelleme bir kez daha (beşinci koşu, `set5-20261009`; aynı on hücre).** Bu
koşunun kodu, Panel'in güvenli bağlantı kuralı dışında son koddur; kural
sonradan değiştirildi (bir satır kod, açıklaması ve testi; `cmd/`, `internal/`
ve `web/` altında başka hiçbir şey).

- On hücrenin tamamı üçüncü koşunun ölçtüğü sonuca ulaştı; her adımın kararı,
  sonuç ve karşılaştırılan olgular aynıydı (`compare-with-set3.md`): Debian 13,
  Ubuntu 24.04 ve Arch'ta doğrulanan güncelleme; geçişi başarısız olan aday
  için, ikinci bir arızayla birlikte, üçünde de v0.1.0-alpha.81'e otomatik
  dönüş; başarısız başlangıç denetiminden sonra dönüş (Debian 13); duraklayan
  güncellemeyi tamamlayan, test sürücüsünün çalıştırdığı yazdırılmış tek yeniden
  deneme (Debian 13, Ubuntu 24.04); yeniden açılış boyunca yönetimin kapalı
  olması (Debian 13). Hiçbir adım ve hiçbir denetim düşmedi.
- v0.1.0-alpha.81'in oluşturduğu bir PHP sitesi, on isteği güncellemeden önce,
  güncellemeden sonra ve ayarları yeniden kaydedildikten sonra aynı durum
  koduyla ve aynı gövdeyle yanıtladı (Debian 13, Ubuntu 24.04).
- Platform başına bir Postfix durdurma (Debian 13, Ubuntu 24.04), Postfix'in
  reddettiği bir yapılandırmayla, yeni kurulmuş değil az önce güncellenmiş bir
  sunucuda: başarısız birimi adıyla veren notla `200` (Ubuntu'da hizmeti
  çalıştıran birim, `postfix@-.service`); işareti hiçbir şey silmedi;
  yapılandırma geri yüklendikten sonra Başlat çalıştı.
- Bu koşunun öncekilerden farkı: makinelerin diskleri bellekte tutuldu ve taban
  imajları paylaşıldı; bu yüzden bu koşunun hiçbir süresi önceki bir koşuyla
  karşılaştırılamaz ve yavaş ya da arızalı bir diske dair hiçbir şey ölçülmedi.
  Windows ana makinesi, koşunun yaklaşık on altı dakikası dışında tamamında
  "modern bekleme" kaydetti; koşunun kendi saatleri duraklama göstermiyor (iki
  örnek arasında 10,5 saniyeden uzun boşluk yok; en uzunu bir hücrenin kendi
  makine sıfırlamasında). Her hücre bir kez koştu.
- Bu koşunun sınamadıkları: posta TLS ayarlarının düzeltilmiş okuması (anlık
  görüntü, geri yükleme, geri okuma) ve yeni kurulum.

**Son kodun yeni kurulumu, Panel'in güvenli bağlantı kuralı ve bir güncelleme
daha (altıncı koşu, `set6-20261009`; Arch, Debian 13, Ubuntu 24.04).** Son kod,
beşinci koşunun kodunun değiştirilmiş kuralı içeren hâlidir. Ondan sonra dalda
`cmd/`, `internal/` veya `web/` altında hiçbir şey değiştirilmedi; eklenenler
test düzeneği, kanıtlar ve "Yayımlama kaydı" bölümünde ("Etiketten önce") anılan sürüm
hazırlığıdır. Bu koşunun arşivleri test derlemeleriydi: yeni kurulan, önceki
sürümün etiketini ve sürüm sırasını taşıyordu; güncelleme hedefi koddan yalnız
82'ye ayarlı sürüm sırası dosyasıyla ayrılıyordu.

- Üç platformda da yeni kurulduğunda, dördüncü koşunun ve ek ölçümünün
  denetimleri yeniden geçti: Panel üzerinden PHP sitesi oluşturuldu, bir PHP
  sayfası sitenin kendi hesabıyla çalıştı, olmayan bir betik 404 yanıtladı ve
  site silindikten sonra ondan hiçbir şey hizmet vermeyi sürdürmedi; kaydedilen
  PHP sürümü ve soketi kurulu sürümünkiydi; cPanel içe aktarımı tamamlandı ve
  `dns` parçasını `left_out` altında listeledi; mutlak adlı arşiv girdisi
  listelendi ve içe aktarım `partial` oldu; web sunucusunun reddettiği site
  `502 SITE_WEB_SERVER_REFUSED` yanıtladı ve yanıtın kaldırıldığını söylediği
  şeyler yoktu; durmuş nginx, MariaDB ve PostgreSQL'in yeniden yüklenmesi
  `409` / `not_running` yanıtladı. Postfix'in reddettiği bir yapılandırmayla
  Postfix'i durdurma, Debian 13 ve Ubuntu 24.04'te notla birlikte `200`
  yanıtladı; posta desteklenmediği için Arch'ta ölçülmedi.
  Arch'ta bu denetimler yinelenen hücrede geçti; ilk hücre aşağıdaki tek
  denetimi geçemedi.
- Panel'in kuralı, makinede çalışan Panel'den okundu: her birinde on bir HTTPS
  yanıtı olan 14 okuma (durum kodları 200, 204, 401, 403 ve 404). Yeni
  kurulumlardaki sekiz okumada (her platformda iki, ve iki kez koşan ilk Arch
  hücresinde iki tane daha) ile güncellemeden sonraki üç okumada her yanıt tam
  olarak `max-age=31536000` taşıdı. Güncellemeden önceki üç okumada
  v0.1.0-alpha.81 her seferinde `max-age=31536000; includeSubDomains`
  yanıtladı. Panel'in bağlantı noktasına düz HTTP, 14 okumanın hepsinde `400`
  ile yanıtlandı ve böyle bir başlık taşımadı.
- Yayımlanmış v0.1.0-alpha.81'den platform başına bir güncelleme, yeni
  veritabanı şemasıyla (43), doğrulanmış olarak bitti. Güncellemeden önce
  yüklenmiş bir sayfanın gönderdiği biçimde gönderilen istek reddedildi ve
  hiçbir şeyi değiştirmedi: yedek rotasında istek kimliği olmadan `428`;
  yedek zamanlamasında ve zamanlanmış görevlerde, Debian 13 ve Ubuntu 24.04'te
  posta politikasında da, sürümsüz istek için `409 SETTINGS_VERSION_REQUIRED`.
  Debian 13 ve Ubuntu 24.04'te, v0.1.0-alpha.81'in oluşturduğu sitenin
  karşılaştırması ve bir Postfix durdurma bu hücrelerde yeniden koştu ve geçti.
  Diğer yedi güncelleme hücresi son kodda yinelenmedi; onlar beşinci koşuya
  dayanır.
- Bir denetim geçmedi, ilk Arch koşusunda: test düzeneğinin bir kuralı `dns`
  parçasını hâlâ `imported` altında bekliyordu, oysa ürün onu belgelendiği gibi
  `left_out` altında listeledi. Hücre, kural düzeltilerek yeniden koşuldu ve
  geçti; ürünün hiçbir denetimi düşmedi.
- Ölçülmeyenler: yeni sürümün adıyla etiketlenmiş bir arşivin yeni kurulumu
  (yeni kurulan arşiv, dördüncü koşudaki gibi önceki etiketi taşıyordu);
  tarayıcıda herhangi bir şey; yönetilen sertifikası olan ya da ana makine
  adıyla ulaşılan bir Panel'de kural; makinelerin trafiği. Ana makine koşunun
  tamamında "modern bekleme" kaydetti; koşunun kendi saatleri duraklama
  göstermiyor. Yinelenen Arch hücresi dışında her hücre bir kez koştu.

**Arayüz, yayımlanmış kodda gerçek bir Chrome'da (yedinci kayıt,
`set7-20261010`, 10 Ekim 2026 UTC, etiketten sonra yapıldı).** Bu yazılırken
commit edilmemişti ve kabul denetimi bitmemişti; aşağıdaki hükümler kaydın
kendi hükümleridir. Geçici Debian 13 konukları; ana makinede gerçek bir Google
Chrome 154 (başsız), her konuğun gerçek Panel'ine bir komut dosyasıyla sürülerek,
SSH yönlendirmesi üzerinden ulaşıldı. Kod, etiketteki `main`'dir (commit'in
`git archive` çıktısı); imzalı arşiv değil, kabul testi lisansıyla derlendi.
Ölçülen kural: ekranın yerini yalnızca bilinen bir olumsuz karar alabilir;
bilinmeyen erişim durumu bağlanmış sayfayı tutar ve sayfa kaldığı yerden
devam eder. Beş hücre, her biri bir kez:

- **Güncelleme karttan başlatıldı, alpha.82 arayüzü: GEÇTİ.** Ayarlar sayfası,
  24 sn'lik kesintinin yaklaşık 8–10 sn'si boyunca kapatılamayan bir katmanın
  ("Panel erişimi şu an doğrulanamadı ...") altında bağlı ve etkisiz kaldı, sonra
  aynı belgede ve aynı bölümde devam etti; başarıdan sonra ürünün kendi yeniden
  yüklemesi aynı rota ve bölüme gitti. Gözlem, hüküm değil: katmanın ilk cümlesi
  lisansı anıyor, oysa neden Panel'in kendi planlı yeniden başlatmasıydı.
- **Aynı güncelleme, yayımlanmış alpha.81 arayüzünden başlatıldı: sahibin
  bildirdiği ekran yeniden üretildi.** Ayarlar sayfasının yerini, 89 sn'lik
  kesintinin yaklaşık 78 sn'si boyunca tam ekran "Lisans durumu
  denetlenemedi" sayfası aldı (iki kesinti karşılaştırılamaz: alpha.81'den
  güncelleme veritabanı şemasını da 42'den 43'e taşıdı, diğeri taşımadı).
- **Sekme 631 sn arka planda, Panel çalışıyor: GEÇTİ.** Sayfa gizliyken hiç
  istek göndermedi; dönüşte tek bir sessiz okuma; yazılan metin, odak, adres ve
  bölüm değişmedi.
- **Sekme 631 sn arka planda, Panel yeniden başlarken: GEÇTİ, bir sınırla.**
  Yeniden başlatma 0,5 sn içinde bitti, iki yoklamanın aralığından kısaydı; yani
  uzun bir kesintide gizli sekme ölçülmedi.
- **`/setup`, `/` ve `/settings?section=updates` yollarının soğuk tam sayfa
  yüklemesi: üç yolda da BAŞARISIZ.** Henüz dönüştürülmemiş kapılardan biri olan
  `RecoveryAccess`'in tam sayfa "Panel erişimi denetleniyor" ekranı, 18
  yüklemenin her birinde, hiçbir oturum okuması yanıtlanmadan boyandı: hızlı
  bağlantıda yaklaşık 80–100 ms, 2 Mbit/sn ve 300 ms'ye kısılmış bağlantıda
  yaklaşık 1,7 sn. "Sessiz süre (1,5 sn) geçene ya da bir okuma erişimi
  doğrulamadan yanıtlayana kadar hiçbir şey çizilmez" kuralını bu kapı
  karşılamıyor. Hiçbir yüklemede bekletme katmanı görülmedi.

Ölçülmeyenler: Ubuntu 24.04; Arch; görünür (başlıklı) bir tarayıcı; telefon
görünümü; koyu tema; Türkçe; HTTP önbelleği açık bir yükleme; kurtarma
hizmet çalışanının denetlediği bir sayfa; kurulu herhangi bir sunucu. Konuklar
diğer koşulardaki gibi dışarıya NAT yapan QEMU kullanıcı ağında çalıştı;
trafikleri yakalanmadı.

**Hangi denetim hangi koda dayanıyor.** Sonraki her hâl önceki düzeltmeleri
içerir. "Yinelenmedi", denetimin sonraki kodda yeniden koşulmadığı demektir.

| Denetlenen | Üzerinde koştuğu kod |
| --- | --- |
| Gerçek hizmetlerde ayar kayıtları (zamanlanmış görevler, posta politikası, yedek zamanlaması, yapılandırma dosyaları, catch-all) | ilk üç koşunun kodu; son kodda yinelenmedi; tek istisna, güncellemeden sonra sürümsüz kaydın reddi (altıncı koşu) |
| Hizmet işlemleri, platform başına 41 ve sonra 42 | ikinci ve üçüncü koşunun kodu; son kodda yinelenmedi; istisna, durmuş nginx, MariaDB ve PostgreSQL'in yeniden yüklenmesi ile Postfix'i durdurma (altıncı koşu) |
| Sekiz rotada istek kimliği | ikinci ve üçüncü koşunun kodu; son kodda yinelenmedi; tek istisna, güncellemeden sonra yedek rotasında başlıksız isteğin reddi (altıncı koşu) |
| İçe aktarılan parolayla posta kutusu oturumu; sertifika hatası yanıtı; MariaDB sürümü; veritabanı parolası; korumalı bir yanıtta, saklanan bir satırda veya günlük satırında özet biçimli değer bulunmaması | üçüncü koşunun kodu; yinelenmedi |
| PHP sitesinin oluşturulması, çalışması ve silinmesi; cPanel içe aktarımı, parola özeti içermeyen önizlemesi ve üç listesi; mutlak adlı arşiv girdisi; reddedilen site | son kod, üç platformda yeni kurulum (altıncı koşu) |
| Postfix durdurma notu | son kod: Debian 13 ve Ubuntu 24.04'te birer kez yeni kurulumda ve güncellenmiş sunucuda (altıncı koşu); dörder durdurma ek ölçümün kodunda |
| Panel'in güvenli bağlantı kuralı | son kod, üç platform (altıncı koşu); tarayıcıda değil |
| v0.1.0-alpha.81'den güncelleme: iyi güncelleme; alpha.81 sitesinin güncelleme boyunca karşılaştırması | son kod (altıncı koşu; site Debian 13 ve Ubuntu 24.04'te) |
| v0.1.0-alpha.81'den güncelleme: otomatik dönüş, başarısız başlangıç denetimi, yazdırılmış yeniden deneme, yönetimin kapalı olması | beşinci koşunun kodu; son koddan yalnız Panel'in güvenli bağlantı kuralında ayrılır; yinelenmedi |
| `postconf` uyarısının ardındaki posta TLS ayarlarının okunması | bileşen testleri ve programın bir okuması; bir sunucuda çalıştırılmadı |
| MariaDB olmayan bir `mysqld` | yalnız bileşen testleri |

Kısaca: ilk üç koşunun ayar kaydı, istek kimliği ve tam hizmet işlemi hücreleri
son kodda yinelenmedi; on güncelleme hücresinden yedisi de yinelenmedi. Son koda
dayanan denetimler, altıncı koşunun andıklarıdır.

**Ekranlar.** Arayüz değişikliklerinin bileşen testleri var; ayrıca kurulu bir
Chrome'da, API'nin loopback üzerindeki bir taklidine karşı incelendiler
(masaüstü ve telefon, İngilizce ve Türkçe, açık ve koyu tema). O tarayıcının
arkasında gerçek bir Panel yoktu. Yedinci kayıt ise yalnız güncelleme ve ilk
sayfa yüklemesi için gerçek bir Chrome'u gerçek bir Panel'in önüne koydu
(İngilizce, açık tema, masaüstü boyutu, başsız).

**Testler.** Test günlükleri, Postfix durdurma düzeltmesini içeren kod ve
`postconf` düzeltmesinin commit edilmeden önceki hâlini içeren kod için
saklanır (`set4b-20261009/verification/`, `set4c-20261009/verification/`).
İkisiyle birlikte, geliştirme makinesinde: Panel ve ortak paketlerde 6829 test
geçti, hiçbiri düşmedi; arayüzde 1245 testin 1245'i geçti; Agent paketinde 4481
test geçti, 94'ü düştü, 29'u atlandı. Düşen küme, bu düzeltmelerden öncekiyle
adı adına aynıdır. Değişiklik kaydı bu 94'ün nedeni olarak makinenin ortamını
gösterir; saklanan günlükler nedeni saptamaz.

## Bu sürümün sınırları

Bunlar bilinen sınırlardır. Gizli kusur değildir.

- **Son kodda yinelenmeyenler.** İlk üç koşunun ayar kaydı, istek kimliği ve
  tam hizmet işlemi hücreleri son kodda yeniden koşulmadı. On güncelleme
  hücresinin yedisi (otomatik dönüş, başarısız başlangıç denetimi, yazdırılmış
  yeniden deneme, yönetimin kapalı olması) beşinci koşunun kodunda koştu (bu kod son
  koddan yalnız Panel'in güvenli bağlantı kuralında ayrılır) ve yinelenmedi.
  Yeni kurulan arşiv önceki sürümün etiketini taşıyordu; v0.1.0-alpha.82
  etiketli arşiv yalnız güncellemeyle kuruldu; sürüm hazırlığı (sürüm sırası,
  önyükleme sabitlemeleri, sürüm satırları) ölçülen hiçbir arşivde yoktur.
- **Panel'in güvenli bağlantı kuralı.** Bir tarayıcıda ölçülmedi; yönetilen
  sertifikası olan ya da ana makine adıyla ulaşılan bir Panel'de de
  laboratuvarda ölçülmedi. Yayımlamadan sonra başlık, kurulu iki sunucuda
  dışarıdan okundu ("Yayımlama kaydı"na bakın); tarayıcıda değil.
  Başlığın tanımına göre, Paneli bir daha açmayan tarayıcı geniş kuralı bir
  yılı dolana kadar tutar. Panel'in bağlantı noktasına düz HTTP, HTTPS'e
  yönlendirme değil `400` yanıtlar; v0.1.0-alpha.81 de aynısını yanıtlar.
- **Bir PHP sitesinde olmayan durağan dosya**, güncellemeden önce de sonra da,
  sitenin ana sayfasıyla 200 yanıtlar (Debian 13 ve Ubuntu 24.04'te okundu).
- **Üretilmiş bir site yapılandırma dosyasında kendi düzenlemeleriniz.** Panel
  başladığında, güncellemeden sonra ve başka zamanlarda, barındırılan sitelerin
  web sunucusu yapılandırmasını yeniden yazar; dayanıklılık sözleşmesi önceki
  sürümlerin de aynısını yaptığını kaydeder. Böyle bir dosyada elle yaptığınız
  değişikliğe ne olduğu ölçülmedi ve bu yol denetlenmedi. Ürünün kuralı,
  sahibin değişikliğini sessizce ezmek değil algılamaktır; denetim yapılana
  kadar bu dosyalardaki el düzenlemelerinin bir Panel başlangıcından sağ
  çıkacağına güvenmeyin.
- **Ölçülen platformlar.** Ayar kaydı düzeltmeleri ve hizmet işlemlerinin tam
  kümesi: yalnız Debian 13 ve Ubuntu 24.04; Arch'ta yalnız durmuş nginx,
  MariaDB ve PostgreSQL'in yeniden yüklenmesi koşuldu. İstek kimliği (Arch'ta,
  ikinci koşuda içe aktarım hariç) ve güncelleme: Debian 13, Ubuntu 24.04 ve
  Arch. RHEL ailesi engelli önizleme olarak kalır. Arch'ta posta desteklenmez.
- **Arch'ta PHP.** PHP sitesi oluşturma, çalıştırma, silme ve içe aktarma
  ölçüldü. Listelenen, düzeltilmeyen ve ölçülmeyenler: PHP eklenti dizinleri,
  MariaDB ayar dosyasının yolu, web postası kurulumunun kullandığı PHP sürümü
  algılaması ve Arch'ta durağan bir sitenin PHP'ye çevrilmesi.
  v0.1.0-alpha.81'in oluşturduğu site Arch'ta karşılaştırılamadı, çünkü o
  sürüm orada PHP sitesi oluşturamaz.
- **Uzun site adı.** 58 karakterlik bir adla PHP sitesini üç platformun da
  olağan nginx'i reddetti ("could not build server_names_hash"). Yanıt
  `sudo nginx -t` komutunu gösterir; o komut geçer ve siteyi yeniden oluşturmak
  aynı yanıtı verir. Çalışan en uzun ad ölçülmedi; nginx'in satırında adı geçen
  ayarı yükseltmek de denenmedi.
- **PATH_INFO.** Betikten sonraki ek yol PHP'ye yalnız istek yolunun kendisi
  `.php` ile bittiğinde ulaşır; başka bir yol sitenin `index.php` dosyasına
  verilir. v0.1.0-alpha.81 de aynı davranır.
- **Yerinde kalan dizin.** Agent'ın bir alan adının sertifika doğrulaması için
  hazırladığı dizin, reddedilen bir site oluşturmadan ve bir site silindikten
  sonra hâlâ oradadır. Hiçbir yanıt onun kaldırıldığını söylemez.
- **Düzenlenmiş PHP parça dosyası** algılanmaz (sahip bölümü). Güncelleme,
  düzenlenmiş bir dosyayla ya da sertifikası olan bir siteyle ölçülmedi.
- **Barındırma türü, sertifika ya da site ayarı değişikliği sırasında nginx'in
  reddi** hâlâ eski hatasını yanıtlar; yeni yanıt yalnız site oluşturmada ve içe
  aktarımda vardır.
- **Gerçek bir sistemde üretilmeyen yanıtlar:** `cleanup_unconfirmed`,
  `import_removed` ya da `import_cleanup_unconfirmed` nedenli
  `SITE_WEB_SERVER_REFUSED`; `PHP_VERSION_NOT_INSTALLED`; sitesi olmayan alan
  adı için boş `php_version`; "failed" durumundaki bir birimin ve PHP-FPM'in
  yeniden yüklenmesi; 20'den fazla dışarıda bırakılan arşiv girdisi; içe
  aktarım durumları `not_chosen`, `none_in_archive` ve `none_imported`; Durdur
  notları `unit_not_settled` ve `unit_state_not_read`; `restored` ya da
  `restored_running_unknown` nedenli `CONFIG_RELOAD_FAILED`;
  `MAIL_POLICY_RELOAD_UNKNOWN`; `reload` ya da `verify` nedenli
  `MAIL_POLICY_NOT_RELOADED`; `REQUEST_IN_PROGRESS`; MariaDB olmayan bir
  `mysqld` için `CONFIG_INVALID` / `no_validator`; `CONFIG_INVALID` / `shape`;
  `MAIL_POLICY_INVALID`; `variable` dışında bir nedenle
  `MAIL_POLICY_RESTRICTIONS_UNMANAGED`; `CRON_JOB_AMBIGUOUS`; zamanlanmış
  görevlerin `cron_deny` nedeni; `stop` ya da `verify` nedenli
  `SERVICE_ACTION_FAILED`; bozuk bir başlık için `400 REQUEST_ID_REQUIRED`;
  aşırı büyük gövde için `413`; `reason` değeri `failed` olan
  `REQUEST_COMPLETED_RESULT_NOT_RETAINED`.
- **`postconf` uyarısının ardındaki posta ayarları.** Düzeltmenin bileşen
  testleri ve gerçek programın bir okuması var (Postfix 3.10.13, Debian 13).
  Böyle bir yapılandırmayla posta TLS değişikliği, geri alınması ve sertifika
  yayımı çalıştırılmadı; beşinci ve altıncı koşuda da. Ubuntu'nun Postfix 3.8'i ve Arch bu ayarlar için
  okunmadı. v0.1.0-alpha.81 hakkında, ölçülen iki komutun ötesindeki ifadeler
  kaynağından okundu. Sunucuda zaten kurulu bir yenileme yardımcısı panel
  güncellemesiyle değiştirilmez (değişiklik kaydı); bu yüzden bu düzeltme ona
  ulaşmaz.
- **Postfix'i durdurma.** Ölçülmeyenler: `postconf`'a uyarı verdiren ama
  `postfix check`'in kabul ettiği bir yapılandırmayla durdurma; Arch (orada posta
  desteklenmez); Dovecot; son kodda hücre başına birden fazla durdurma (platform başına
  dört durdurma ek ölçümün kodunda koştu). systemd'nin "failed" işareti bırakılır; notun adını verdiği
  `sudo systemctl reset-failed <birim>` onu siler.
- **Güncelleme kartı.** Yeni metni yalnız yedinci kayıtta, laboratuvardaki
  bir Panel'de başsız bir Chrome'da ve sahip tarafından kurulu iki sunucuda
  (aşağıdaki gözlemlere bakın) görüldü; kurulu bir sunucuda ölçülmedi.
  v0.1.0-alpha.81'e dönüşten sonra sunulan arayüz alpha.81'inkidir;
  bu yüzden yeni metin orada görünemez. Otomatik dönüşten sonra güncelleme
  denetimi aynı sürümü yine sunar.
- **Henüz dönüştürülmeyen ekranlar.** Arayüzün 31 kaynak dosyası sunucuyu en az
  bir yerde hâlâ eski biçimde okuyor: erişim kapıları ve oturum açma sayfası,
  kurulum sihirbazı, güncelleme ve bileşen işlemi katmanları, Hizmetler listesi
  ve hizmet sayfaları, kenar çubuğu, panonun bazı bölümleri, eklentiler, denetim
  günlüğü, VPN, ekip üyeleri ve birkaç küçük bölüm. Bunlarda başarısız bir okuma
  hâlâ boş ya da olumsuz bir durum gibi görünebilir.
- **Yayımlamadan sonra kurulu sunucularda gözlenenler; bu sürümde
  düzeltilmedi.** Sahibin kendi ekranlarından, sahibin 10 Ekim 2026'da kurulu
  iki sunucuyu (biri Ubuntu 24.04, biri Debian 13) panelin güncelleme
  ekranından güncellemesinden sonra bildirdikleridir. Hiçbiri bir asistan
  tarafından o sunucularda ölçülmedi ve hiçbiri bu sürümle değişmedi:
  1. Ubuntu sunucusunda güncelleme v0.1.0-alpha.81 arayüzünden başlatıldı.
     Panel'in yeniden başlaması sırasında tarayıcı, "Update and recovery
     status" kutusuyla birlikte tam ekran "License status could not be checked"
     sayfasını gösterdi ve kendiliğinden toparlandı. Aynı biçimde başlatılan
     Debian sunucusunda görünmedi (zamanlama meselesi). Yedinci kayıt bu ekranı
     laboratuvarda alpha.81 arayüzüyle yeniden üretti, alpha.82 arayüzüyle
     görmedi.
  2. Debian sunucusunun Ayarlar, güncellemeler sayfasında, güncellemeden sonra
     kart v0.1.0-alpha.82'yi kurulu gösterirken köşedeki bir bildirim, başlangıçtan
     yaklaşık iki dakika sonra (T+02:03) hâlâ güncellemenin uygulandığını,
     panelin kısa süre kullanılamayabileceğini ve güncellemelerin kilitli
     olduğunu söylüyordu. İkisi birbirini tutmuyordu.
  3. `/setup` sayfasının soğuk tam yüklemesi, sayfa çizilmeden önce kısa bir an
     "Checking..." ve "Reload CelikPanel" düğmeleriyle tam sayfa "Checking panel
     access" ekranı gösterir. Yedinci kayıt aynısını laboratuvardaki bir
     Panel'de `/setup`, `/` ve güncellemeler sayfası için ölçtü: bu,
     bilinen-durum kuralına henüz dönüştürülmemiş `RecoveryAccess` kapısıdır.
  4. Debian sunucusundaki kurulum sihirbazı sayfası en üstte "This operation
     stopped" ve "A required check needs attention" derken son adım "Verify the
     prepared server - In progress" diyor; neden, kapalı "Checks and how to
     continue" ve "Technical details" bölümlerinin altındadır. Yani neden ve
     kimin işlem yapacağı adım listesinden önce gösterilmiyor. v0.1.0-alpha.81'de
     de aynıydı.
- **İstek kimliği olmayan rotalar.** Yalnız yukarıdaki sekiz rota korunur.
  Bunların dayandığı döküm; hizmet ve uygulama yeniden başlatmayı, planları ve
  kayıt kodlarını yinelendiğinde zararlı, yaklaşık 38 başka rotayı durumu doğru
  bırakan ama yinelemeyi yanlış bildiren, yedisini de sınıflandırılamayan olarak
  ayırdı; bunların hiçbiri henüz korunmuyor. DNS kayıtları, barındırma türü, PHP
  ayarları, takma adlar, uygulama kurulumu, posta kimlik doğrulaması, hesaplar
  ve dosyalar için de aynısı geçerlidir. Bunlarda yanıt kaybolduğunda sayfa
  yeniden okur ve sonucu size bırakır.
- **İstek kimliği.** 24 saatlik süre dolumu hiçbir testte oluşmadı.
  Sonlandırılan bir Panel'den sonra, Agent işi bitirmiş olsa
  bile yanıt "sonuç bilinmiyor"dur; ikisini hiçbir şey uzlaştırmaz. Ulaşmayan
  tek seferlik sonuç (üretilen parola, VPN cihaz dosyası) yeniden gösterilemez.
  Panel'de bir veritabanı kullanıcısının parolasını belirleyen bir denetim
  yoktur; bu, veritabanı sunucusunda yapılır.
- **Kurulu yenileme yardımcıları panel güncellemesiyle yükseltilmez.** Bağımsız
  posta yenilemesi daha eski bir sürümce kurulmuş sunucu, yeni sertifika
  denetimini yalnız güncellenmiş Agent yenilemeyi üstlendiğinde alır; Agent
  yokken almaz. Bu yapılana kadar böyle bir Ubuntu sunucusunun sahibi yenilemeden
  sonra şunu
  `openssl s_client -connect localhost:465 </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256`
  şununla karşılaştırabilir:
  `openssl x509 -noout -fingerprint -sha256 -in /etc/ssl/celikpanel/_mail/host/current/fullchain.pem`;
  farklıysa `sudo postfix reload` çalıştırır. Bu karşılaştırma bir testte
  çalıştırılmadı.
- **Posta yenilemesinin yeni sertifika denetimi gerçek bir sistemde
  ölçülmedi.** Yalnız bileşen testleri var.
- **Postfix kısıtlama düzeni.** Her satıra bir kısıtlama yazdığınız liste, bir
  DNSBL kaydından sonra tek satır olur; öğeler ve sıra korunur. Panel'in kendi
  bölümü ile sizinkine ayıramadığı liste yeniden yazılmaz: DNSBL değişikliği
  reddedilir, boyut ve hız yine kaydedilir.
- **`/etc/cron.allow` altında zamanlanmış görevler.** Debian ve Ubuntu'da bu
  dosya site kullanıcısı olmadan varsa Panel o kullanıcının görevlerini okuyamaz
  ve değiştiremez; bunu da söyler.
- **Ayarlar.** Zamanlanmış hiçbir yedek çalışmadı; bu yüzden saklama sayısına
  göre budama denenmedi. Kaydedilen bir MariaDB değerinin sonraki yeniden
  başlatmada yürürlüğe girdiği denetlenmedi.
- **Hizmet işlemleri.** PostgreSQL'in iki yeniden yükleme nedeni yalnız
  hazırlanmış iki yeniden yükleme kancasıyla üretildi. Yapılandırması reddedilen
  Dovecot ya da nginx, ikinci bir PostgreSQL kümesi ve WireGuard birimleri
  koşulmadı. Yeniden yükleme, böyle bir işlemi olmayan MariaDB için de sunulur ve
  hata yanıtlar.
- **cPanel içe aktarımı.** Gerçek bir cPanel yedeği içe aktarılmadı; arşivleri
  laboratuvar kurdu. Posta kutusu içerikleri taşınmaz. Yalnız bir parola özeti
  şeması denendi ve parolasız bir posta kutusu kurulmadı. Site klasörünün
  altındaki `..` içeren, bağlantı ya da aygıt olan bir girdi hâlâ bütün dosya
  adımını reddettirir. İçe aktarım sayfasında dışarıda bırakılan parçaların
  listesi yoktur; adım ayrıntı satırları İngilizcedir.
- **Sertifikalar.** Hiçbir koşuda sertifika alınmadı: sertifika rotasının
  çağrıldığı yerlerde Let's Encrypt adları makinenin kendisini gösteriyordu ve
  beş başarısızlık nedeninden yalnız `authority_unreachable` üretildi.
- **Posta kutusu oturum denetimi.** İçe aktarılan posta kutusu iki platformda da
  IMAP üzerinden oturum açtı. Ardından gelen kutusunun açılması Ubuntu'da
  başarılı, Debian'da hata olmadan "yapılmadı" olarak kaydedildi; denetlenen bir
  koşul değildi.
- **Güncelleme.** İmzalı arşivden değil etiketin kaynağından. İki matrisin her
  birinde hücre başına tek koşu. Başlangıç denetimi ve yönetimin
  kapalı olduğu durum yalnız Debian'da; sahibin devam ettirmesi Arch'ta yok.
  Arch hücreleri postasız koştu. Güç kaybı yok. Güncelleme sırasında açık sayfa, onun gönderdiği gönderilerek
  taklit edildi. v0.1.0-alpha.80'den bu sürüme doğrudan güncelleme ölçülmedi.
- **Kanıt kapsamı.** Tek bir dizüstü ana makine, deneme lisansı, loopback sürüm
  kaynağı, deneme imzalama anahtarı; dışarıya ağ erişimi olan ve trafiği
  kaydedilmeyen makineler. Ana makine üçüncü koşu sırasında, hiçbir hücre
  başlamadan önce bir kez bekleme durumuna geçti; hücreler koşarken iki bekleme
  olayı daha kaydetti ve bunlar boyunca çalışmayı sürdürdü (`sampler-gaps.txt`,
  her hücrenin kendi sıfırlaması ya da yeniden açılışı dışında boşluk
  göstermiyor). Ek ölçümün Ubuntu hücresi sırasında, düzeltilmiş derleme
  kurulmadan önce 43 dakika uyudu; ölçülen adımlar uyandıktan sonra koştu.
  Dördüncü koşuda güç olayları toplanmadı; 30 saniyelik disk izleyicisi boşluk
  göstermiyor. Ana makine beşinci koşunun büyük bölümünde, altıncı koşunun
  tamamında "modern bekleme" kaydetti ve örnekleyiciler duraklama göstermiyor;
  bu durumun çalışan
  bir iş yükünde duraklatmak dışında neyi değiştirdiği ölçülmedi.
- **Geçmeyen denetimler** her dizinin `checks-not-passed.txt` dosyasında
  sayılır. Birinci koşu: yukarıdaki iki kusur ve bir düzenek hatası. İkinci
  koşu: düzenek hataları (önceki denemeler ve Arch'ta sertifika rotasının iki
  denetimi), Postfix'i durdurmanın "bilinmiyor" yanıtı, yeniden yükleme
  ifadesi, içe aktarımın dizin girdisi ve Arch'ta PHP havuz yolu; bunlardaki
  ürün hataları sonradan düzeltildi. Üçüncü koşu: Debian 13'te (yeniden
  koşulunca geçti) ve Ubuntu 24.04'te (yeniden koşulmadı) düzenek hatasından
  kaynaklanan ikişer denetim ve Arch'ta hepsi o zamandan beri düzeltilen PHP
  sitesi kusuruna dayanan 14 denetim. Dördüncü koşu: Ubuntu 24.04'te Postfix
  durdurma notunun üç denetimi, iki koşuda da; düzeltildi ve ek ölçümde yeniden
  ölçüldü. Ayrıca ilk Debian güncelleme hücresinde bir düzenek kuralı; yeniden
  koşulunca geçti. Ek ölçüm: ilk tanı denemesi, ilk durdurmadan önce sürücünün
  bir hatasında durdu. Beşinci koşu: yok. Altıncı koşu: ilk Arch hücresinde bir
  düzenek kuralı; yeniden koşulunca geçti.
- **Belgelerdeki tarihler.** Sözleşme ve yönlendirme belgelerindeki bazı tarihli
  girdiler ile kaynak açıklamaları 2026-10-10, 2026-10-11 ya da 2026-10-12
  taşır. Bunlar tarih değil, çalışma turlarının etiketleridir; o iş 8 ve 9 Ekim
  2026'da yapıldı. Her belge bunu başında söyler. "Yayımlama kaydı"
  bölümündeki sahip kararları ve yayımlama adımları ile altıncı ve yedinci koşu
  ise saate göre gerçekten 10 Ekim 2026 tarihlidir.
- **Önceki sınırlar.** [v0.1.0-alpha.81](RELEASE-NOTES-v0.1.0-alpha.81.tr.md)
  sınırlarından bu sürümün ele almadıkları geçerliliğini korur. Biri
  alpha.81'den güncellemede geçerli değildir: ölçülen güncellemelerde kurtarma
  komutu ve Panel'in kurtarma durumu ilk saniyeden itibaren vardı.
- **Açık kabul işleri.** [Dayanıklılık sözleşmesinin](RESILIENCE-CONTRACT.tr.md)
  beş temel işinin tamamı kısmi kalır; bu sürüm hiçbirini kapatmaz. Kararlar:
  [DECISIONS](DECISIONS.tr.md) içinde D-022, D-024, D-025, D-029, D-030.

## Yayımlama kaydı

Aşağıdaki her şey 10 Ekim 2026 tarihlidir (saat tarihi; saat dilimi
belirtilmedikçe UTC). Adımları sahip attı. Kaynaklar belirtilmiştir; sahibin
bildirdikleri depoda bir dosyada kayıtlı değildir.

**Ne oldu.**

1. `CELIKPANEL_RELEASE_SEQUENCE` depo değişkenini sahip, birleştirmeden önce
   82 yaptı (sahip bildirdi).
2. #205 numaralı çekme isteğini sahip, 07:15 UTC'de `main`'e squash commit
   olarak birleştirdi (depo barındırıcısından okundu: birleştirme 07:15:11,
   squash commit'in tarihi 07:15:10).
3. Sahip bu commit üzerinde `v0.1.0-alpha.82` açıklamalı etiketini oluşturdu
   (etiket 08:01 UTC, depodan okundu) ve gönderdi.
4. Etiketin CI koşusu 22 işin 22'siyle başarıyla tamamlandı (08:01'de
   başladı, 08:21 UTC'de bitti; depo barındırıcısından okundu) ve tam altı
   dosya yayımladı: genel arşiv ve sağlama toplamı, linux/amd64 arşivi ve
   sağlama toplamı, imzalı manifest (v2) ve imzası. Platform arşivi
   65.894.979 bayttır, SHA-256
   `a37671064f2ea0b08fd5e0e25c14218bb8b005067855bf9c13af2fc653ffe4bc`
   (depo barındırıcısının dosya özetlerinden okundu, tutuyor); sıra 82. Depo
   barındırıcısı sürümün yayımlanma anını 08:21:32 UTC gösterir. İndirme
   portalının `published_at` alanındaki 07:15:10 UTC commit'in zamanıdır,
   sürümün yayımlanma zamanı değildir.
5. Altı dosya, etiketin özel bir klonunda doğrulandı (sürümü hazırlayan kişi
   bildirdi, burada bir dosyada kayıtlı değil): iki sağlama toplamı dosyası
   tutuyor; genel ve platform arşivi bayt bayt aynı; arşivdeki `release.commit`
   ve `release.tree` etikete eşit; önyükleme üyeleri (`libexec/get.sh`,
   `install.sh`, açık anahtar) etiketinkilerle bayt bayt aynı; içerik
   koruyucusu ve kabul-lisansı koruyucusu geçiyor; manifest imzası izlenen açık
   anahtarla doğrulanıyor; programlarda kabul-lisansı derleme etiketi yok.
6. İndirme portalı, dört platform dosyasından özel bir klonda imza öncesi
   kipte (`deploy/build-download-portal.sh`) derlendi ve iki kez aynı
   sonuçla paketlendi (sürümü hazırlayan kişi bildirdi): paket SHA-256
   `aa19c55826e04bf7de470af44adb2bba864595e4648620bcf45793bf7fad215e`,
   132.163.894 bayt; düzeni sürüm dışında v0.1.0-alpha.81 paketininkiyle aynı.
   v0.1.0-alpha.81 portalını tutan bir deneme kökü üzerinde
   `promote-download-portal.py` ile yerelde prova edildi (işlendi, 26 genel
   istek, yedek tutuldu, v0.1.0-alpha.81 korundu).
7. Sahip portalı `deploy/publish-download-portal.ps1` ile yaklaşık 09:36
   UTC'de yayımladı: işlem `status: committed`, 26 istekli tek bir genel
   doğrulama turu (`status: ok`), önceki sitenin yedeğinin tutulduğunu ve
   başarı işaretini bildirdi (sahip bildirdi). Sonrasında genel sitenin,
   sürümü hazırlayan kişice, bir dosyada kaydedilmeden okunması:
   `releases/latest.txt` v0.1.0-alpha.82'yi söylüyor; `latest.json` sıra 82'yi
   ve commit'i taşıyor; sunulan manifest, imza ve sağlama toplamı CI
   dosyalarıyla bayt bayt aynı; `get.sh` ve açık anahtar etiketinkilerle aynı;
   imza doğrulanıyor; v0.1.0-alpha.81 arşivi hâlâ sunuluyor.
8. Sahip kurulu iki sunucuyu, biri Ubuntu 24.04 biri Debian 13, panelin kendi
   güncelleme ekranından 10 Ekim 2026'da güncelledi. İkisi de
   v0.1.0-alpha.82'yi ve sürüm commit'ini bildiriyor (sahip bildirdi). Her
   Panel'in `Strict-Transport-Security` başlığı, güncellemeden önce ve sonra
   dışarıdan okundu: önce `max-age=31536000; includeSubDomains`, sonra
   `max-age=31536000`. Bu, D-030 kuralının gerçek sunucularda gözlenmesidir;
   tarayıcıda okunmadı.
9. Sahibin bu güncellemeler sırasında ve sonrasında ekranda gördükleri
   sınırların altında ("Yayımlamadan sonra kurulu sunucularda gözlenenler")
   sıralıdır. Bu sürümde düzeltilmedi.

Sürümü yayımlamak hiçbir kurulu sunucuyu güncellemedi; iki güncellemeyi yalnız
sahip başlattı. Ne sürüm ne de yayımlanması dayanıklılık sözleşmesinin bir P0
işini kapatır.

**Etiketten önce: yayımlamadan önce kaydedilen sahip kararları ve denetimler.**

1. Sürüm: v0.1.0-alpha.82 (sahibin 10 Ekim 2026 kararı).
2. Sahip 10 Ekim 2026'da son kodun yayımlamadan önce yeni kurulup
   ölçülmesine karar verdi; bu yapıldı (yukarıdaki altıncı koşu).
   `postconf` uyarısının ardındaki posta sertifikası yolu bir sunucuda
   ölçülmedi ve adı belli bir sınır olarak kalır; bu bir sahip kararı değildir.
3. Panel'in güvenli bağlantı kuralı bu sürümde Panel'in kendi ana makine adıyla
   sınırlıdır (sahibin 10 Ekim 2026 kararı; [DECISIONS](DECISIONS.tr.md)
   içinde D-030).
4. Sürüm hazırlığı ayrı bir commit'te yapıldı ve `cmd/`, `internal/` ya da
   `web/` altında hiçbir dosyayı değiştirmez: sürüm sırası (81'den sonra 82),
   kurucunun önyükleme sabitleri, README'nin sürüm satırları ve bunları
   sabitleyen sözleşme testleri. Onunla bir sunucuda hiçbir şey ölçülmedi
   (sınırlara bakın). O commit'ten müşteri arşivi, özel bir kopyada, imzasız
   olarak ve CI'ın paketleme işinin komutlarıyla iki kez derlendi: iki derleme
   bayt bayt aynıdır (232 öğe); içinde test düzeneği, test betiği ya da kanıt
   yoktur; programları deneme lisansı derleme etiketini taşımaz; iki sürüm
   koruyucusu onu kabul eder; sıra dosyası 81'den sonra 82 der ve içindeki
   Panel kendini v0.1.0-alpha.82 olarak bildirir. Aynı yolla yeniden derlenen
   yayımlanmış v0.1.0-alpha.81 arşivine (218 öğe) göre yalnız arayüz dosyaları
   farklıdır. Arşiv, v0.1.0-alpha.81'de olduğu gibi, satıcının yayın araçlarını
   ve eski bir olaydan kalan iki tek seferlik kurtarma betiğini hâlâ taşır;
   bu sürüm onları çıkarmaz. Gerçek arşivi etikette CI
   derler ve imzalar; bu derleme o arşiv değildir. Sahibin yayımlamadaki
   adımları:
   `CELIKPANEL_RELEASE_SEQUENCE` depo değişkenini 82 yapmak (etiketteki imzalama
   işi farklı bir değeri reddeder), birleştirmek, etiketlemek ve portalı
   etiketin dosyalarından yayımlamak; hepsi yapıldı (yukarıya bakın).
5. Paketleme sözleşmesi testleri çekme isteğinde CI içinde koşar; root
   gerektirenler `sudo` altında (`.github/workflows/ci.yml`). #205 numaralı
   taslak çekme isteğinde, son kodu taşıyan commit'in koşusu geçti (21 denetim
   geçti; yalnız etikette çalışan yayım işi atlandı; koşu 9 Ekim 2026'da
   21:07 UTC'de başlayıp 21:27 UTC'de bitti). Ondan sonra eklenen
   commit'ler `cmd/`, `internal/` ya da `web/` altında hiçbir dosyayı
   değiştirmez; onların koşusu bu yazılırken başlamış ama bitmemişti.
   Birleştirilecek uç, çekme isteğinde kendi koşusunu alır; geçen bir koşu
   sahibin kendi denemesinin yerini tutmaz. (Etiketin koşusu yukarıda
   kayıtlı: 22 işin 22'si.)
6. Üretim imzalaması, [imzalı sürüm sözleşmesinde](release-signing.tr.md)
   anlatıldığı gibi, sürüm etiketinde CI içinde yapılır. Ardından sahip,
   yayımlanan dosyaları o belgede anlatıldığı gibi doğrular.
7. Sahibin kendi denemesi, kurulu iki sunucunun güncellenmesidir; sahip bunu
   panelin kendi güncelleme ekranından bizzat başlatır, bunun için geçici bir
   sunucu kullanılmaz. Kurulu paneller yalnız sahiplerince güncellenir.
   (10 Ekim 2026'da yapıldı, yukarıya bakın.)

Bu sürümü yalnız CelikPanel'in güncelleme arayüzünden kurun. Yayımlamak kurulu
sunucuları güncellemez; bir sunucunun güncellemesini yalnız o sunucunun sahibi
başlatır.
