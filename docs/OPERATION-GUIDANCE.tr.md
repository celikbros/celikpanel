# İşlemlerde uygulanabilir yönlendirme

*13 Eylül 2026 tarihinde kaydedilen ürün gereksinimi · [English](OPERATION-GUIDANCE.md)*

**Durum: ürünün tamamında zorunlu; uygulama ve inceleme henüz tamamlanmadı.**
Mevcut kaynak değişikliği, kabul edilen plandaki DNS rolünü kullanan kurulum
ilerlemesini, mevcut panel lisans durumlarını ve genel hizmet işlem hatalarını
kapsar. Her işlemin doğrulandığını veya üçüncü taraf ürün lisansı adaptörlerinin
uygulandığını göstermez.

## Gereksinim

CelikPanel'in yönettiği her program, hizmet, çalışma ortamı ve entegrasyon,
yürüttüğü işlemi, eksik önkoşulu veya hatayı ve kullanıcının sonraki eylemini
açıklamalıdır. Bu kural sihirbazda ve yönetim sayfalarında; kurulum, yapılandırma,
güncelleme, doğrulama, kurtarma ve desteklenen diğer yaşam döngüsü işlemleri için
geçerlidir. Kullanıcının yapabileceği veya yapması gereken bir iş varken yalnız
dönen simge, genel hata veya uzun işlem listesinin altına saklanmış mesaj yetmez.

Ana mesaj şunları belirtmelidir:

1. Ne yapıldığı veya ilerlemeyi neyin engellediği; biliniyorsa ilgili bileşen,
   sunucu veya entegrasyonun adı.
2. Kimin, nerede işlem yapacağı: bu sunucunun yöneticisi, diğer sunucunun sahibi,
   DNS sağlayıcısı, yazılım üreticisi veya kimlik bilgisi/lisans yöneticisi.
3. Varsa incelenmiş ad ve adresleri veya doğrulanmış gözlemleri kullanan somut
   sonraki eylem.
4. Sonrasında ne olacağı: otomatik kontrol, açık doğrulama, mevcut kurtarma eylemi
   veya hata giderildikten sonra yeni plan incelemesi. Yalnız işlem gerçekten
   destekliyorsa otomatik devam sözü verilir.

Bu mesaj ve eylem ayrıntılı adım listesinden önce gösterilir. Teknik ayrıntılar
erişilebilir kalır; kullanıcı iç hata kodlarını yorumlamak zorunda bırakılmaz.
Türkçe ve İngilizce mesajlar, mobil düzen ve klavye erişimi zorunludur.

## Doğruluk ve işlem kimliği

Gözlenmiş hata, eksik önkoşul ve bilinmeyen sonuç birbirinden ayrılır.
Doğrulama hizmetine ulaşılamaması lisansın geçersiz olduğunu kanıtlamaz.
DNS çiftinin doğrulanmamış olması diğer sunucuda hizmetin kurulmadığını kanıtlamaz.
Varsa son doğrulanmış gözlem ve zamanı gösterilir; teşhis, ilerleme yüzdesi veya
tamamlanma süresi uydurulmaz.

Yürütme yönlendirmesi kabul edilen işlemin değişmez planını ve tam alt işlem
kimliklerini kullanır. Sonradan düzenlenen taslak veya başka bir işlem, kabul
edilen işin açıklamasını değiştiremez. Durum sorgusu ve sayfa yenileme salt
okumadır; bileşen kuramaz, değişiklik işlemini tekrarlayamaz veya ikinci iş başlatamaz.

Bilinen hata, sonucu veya kurtarması doğrulanırken görünür kalır. Doğrulama ayrıca
açıklanır. Hata yalnız aynı işleme ait daha yeni kanıtla kaldırılır veya güncellenir;
yeni sorgu başladı diye veya dönen simge göründü diye kaybolmaz. Yanıt kaybı ve
belirsiz sonuçta, tekrar başlatma ya da yeniden deneme sunulmadan önce ilk işlemin
sonucu uzlaştırılmalıdır.

## Lisans, kimlik bilgileri ve dış bağımlılıklar

Kural yalnız CelikPanel'i değil, diğer üreticilerin programlarını da kapsar.
Bir adaptör bir bağımlılığı destekliyorsa anlamlı durumlarını ayırmalıdır:
eksik, reddedilmiş, süresi dolmuş veya iptal edilmiş lisans; doğrulama hizmetine
ulaşılamaması; eksik veya reddedilmiş kimlik bilgileri; yetersiz yetki;
bağlantı hatası; desteklenmeyen veya bilinmeyen sonuç. Desteklenen her durumun
sabit makine kodu, özel çevrilmiş açıklaması ve sonraki eylemi olmalıdır.
Sınıflandırılamayan sonuç için genel hata dürüst bir geri dönüş olabilir;
bu entegrasyonların tamamının uygulandığına kanıt değildir.

Gizli bilgiler uygun korumalı akışta istenir. Lisans anahtarı, parola, erişim
belirteci, özel anahtar veya kimlik bilgisi içeren URL; ilerleme mesajlarına,
loglara, denetim ayrıntılarına veya tanılara yazılmaz. Üretici yanıtları çevrilip
gizli bilgilerden arındırılır; ham yanıt gizli bilgi içerebilir. Lisans edinme veya
yenileme eylemi kendiliğinden satın alma yapamaz veya kimlik bilgilerini paylaşamaz.

Lisans ve kimlik bilgisi kontrolleri ilgisiz barındırılan işleri sessizce
durduramaz veya kaldıramaz. CelikPanel lisansı kaybolduğunda panel kullanımı
kısıtlanır, mevcut hizmetler çalışmayı sürdürür. Üçüncü taraf üreticinin davranışı
doğru anlatılmalıdır; CelikPanel üreticinin bağımsız lisans uygulamasını kontrol
ettiğini iddia edemez.

## DNS örnekleri ve kurulum sırası

- İkincil sunucuyla çalışacak birincil, beklenen ikincil rolünü, nameserver adını ve
  IP adresini incelenmiş plandan göstermelidir. Eş henüz hazırlanmadıysa bu
  sunucunun bütün kurulumunun bitmesi beklenmeden diğer sunucunun hazırlanması
  istenir. İlk DNS doğrulaması bile diğer sunucuya bağlı olabilir.
- İkincil, birincil sunucuyu ve gereken aktarım ilişkisini belirtmelidir.
  Birincile ulaşılamıyorsa eksik önkoşul açıklanır. Alt işlem zaten başarısız olduysa
  birincili başlatmanın her hatayı otomatik düzelteceği söylenmez; gerçek kurtarma
  durumu gösterilir.
- İki sunucu da hazırsa ilgili genel DNS kayıtları, adresler, DNS erişimi ve
  aktarım izinleri kontrol ettirilir. Tamamlanmış kurulum tekrar tekrar önerilmez.
- Harici DNS, gereken kayıtları ve sağlayıcı tarafındaki işi gösterir. Standart
  yerel DNS aktarımı başka bir CelikPanel veya HTTPS API'sini gerektiremez.
  İsteğe bağlı uzaktan kayıt yönetimi yetkisi ayrı bir adımdır; yönlendirmesi de
  ayrı olmalıdır.

## Kabul ve kalan kapsam

Her desteklenen akış için normal ilerleme, kullanıcı eylemi bekleme, hata,
belirsiz kanıt, kurtarma ve tamamlanma doğrulanır. Ters sunucu kurulum sırası,
yenileme/yeniden bağlanma, yanlış/eksik kimlik bilgileri, ilgili lisans durumları,
ulaşılamayan sağlayıcılar ve tam işlem kimliğinin korunması kapsanır.
Adaptöre özgü durumlar doğrulanmadan o adaptörün bunları desteklediği söylenemez.

Mevcut kurulum değişikliği, doğrulanmış kabul edilen plandan salt-okur bağlam,
DNS rolüne göre yönlendirme, panel lisansı yönlendirmesi ve genel hizmet hatası
gösterimi ekler. Ayrıntılı üretici lisansı entegrasyonu ve bütün yaşam döngüsü
sayfalarının eksiksiz incelenmesi gelecekteki iştir. Bu belge gereksinimi kalıcı
kılar; tamamlanmış uygulama iddiası değildir.

[D-024](DECISIONS.tr.md), [D-021 ve kurulum planı](SERVER-SETUP-PLAN.tr.md) ve
[D-022 sunucu sahibinin yönetebildiği altyapı](OWNER-INDEPENDENCE.tr.md) birlikte
geçerlidir. Bu yönlendirme gereksinimi asistana canlı sistemi değiştirme veya
panel güncelleme yetkisi vermez. Kurulu panel güncellemelerini kullanıcı,
[AGENTS.md](../AGENTS.md) gereği CelikPanel içinden kendisi başlatmaya devam eder.

### Zamanlanmış görevler ve yerel cron durumları (1 Ekim 2026)

Kaynak durumudur; bileşen testleri var, gerçek sistem denemesi bekliyor. Bu not
upd1 gerçek sistem denemesinden doğdu: yeni kurulmuş bir Debian 13 `web_mail`
sunucusunda zamanlanmış görev oluşturmak `500 INTERNAL` döndürdü. Agent cron'un
kurulu olmadığını doğru bildirmişti; Panel bu yanıtı gizledi.

- **Cron yok (doğrulanmış).** Her cron RPC'si önce `crontab` komutunu denetler.
  Komut yoksa Agent sabit metnini döndürür, Panel de `409 CRON_NOT_INSTALLED`
  yanıtını verir. `read` gerekçesi liste içindir ("gösterilemiyor; cron kurulana
  kadar hiçbir görev çalışmaz"). `write` gerekçesi oluşturma, değiştirme ve silme
  içindir ("hiçbir şey kaydedilmedi").
  - Kim yapar: sunucu sahibi.
  - Sonraki adım: Bileşenler sayfasından "Scheduled tasks (cron)" bileşenini
    kurun ya da `sudo apt-get install cron` (Debian/Ubuntu) veya
    `sudo pacman -S cronie` ve `sudo systemctl enable --now cronie` (Arch)
    komutlarını çalıştırın.
  - Devam: görevi yeniden oluşturun ya da değiştirin. Hiçbir şey kendiliğinden
    yeniden denenmez.
  - Agent satırı yalnız Panel günlüğüne yazılır. Zamanlanmış görevler ekranı bu
    açıklamayı "görev yok" boş durumunun yerine ekranda tutar.
  - Önceden eksik cron "görev yok" olarak listeleniyor, değişiklik ise "cron job
    not found" bildiriyordu. İkisi de gerçek nedeni gizliyordu.
- **Diğer cron hataları** (kiracı kanıtı, crontab yazımı) sınıflandırılmamış
  `INTERNAL` yanıtı olarak kalır. Bu bir teşhis değil, dürüst bir geri dönüştür.
- **Kurulum.** Site barındıran profiller `cron` için bir `service` adımı planlar.
  Bu adım diğer bileşenlerle aynı kurulum ve doğrulama yolunu, işlem kaydını ve
  hata kodlarını kullanır.
  - Herhangi bir cron uygulaması zaten varsa Agent hiçbir şeyi değiştirmez ve
    `preserved_existing` döndürür. Adım, birimin çalışması istenmeden korunmuş
    olarak başarılı olur.
  - Kurulu bir katalog birimi varsa inceleme bileşeni adımsız, korunan olarak
    gösterir.
  - Son kurulum hazırlık denetimi cron'u yeniden doğrulamaz. Eski bir sürümde
    kabul edilmiş planda cron adımı yoktur ve bu plan bekletilmez.
- **Kaldırma.** Genel kaldırma işlemi hem Panel'de hem Agent'ta, hiçbir değişiklik
  yapılmadan `409 NATIVE_CRON_REMOVAL_REFUSED` ile reddedilir.

Alan adı işleyicilerinin aynı incelemesi, iki sabit Agent "meşgul" yanıtını
`INTERNAL` yerine mevcut `409 HOST_MUTATION_BUSY` yanıtına bağladı
(`agent_mutation_active`: diğer CelikPanel değişikliğinin bitmesini bekleyin,
sonra yeniden deneyin):

- posta yapılandırması kilitliyken posta kutusu parolası değişikliği;
- başka bir site sertifikası işlemi sürerken Let's Encrypt sertifikası alma.

İncelemenin bulduğu diğer gizlenen Agent durumları sonraki iş olarak listelidir.
Bu not onları kapsamaz.

### Kurulumda bileşen kurulum hataları (upd1 bulgusu P2, 2026-09-30)

Bileşen testli kaynak durumu; gerçek sistemde yeniden koşu bekliyor (upd2). upd1,
temiz bir Arch `web_mail` kurulumunu `05-mail_profile` adımında yalnız
`service_install_failed` ("Servis kurulamadı ve doğrulanamadı.") ile durdurdu.
Tek bir geçici Arch konuğunda salt-okur yeniden üretim, nedeni yalnız Panel
günlüğünde buldu: `failed in profile/webmail/dovecot/configuring: … dovecot:
dovecot is not installed`. Dovecot kuruluydu (`dovecot 2.4.4-1`); Arch paketi tek
bir `/etc/dovecot/dovecot.conf` getirir ve Agent'ın gerektirdiği `conf.d` yoktur.

- **Baştan reddedilen eksik önkoşul.** Otomatik Dovecot kurulumu `pacman`
  ailesinde belirli bir katalog gerekçesiyle kapatıldı. Kurulum incelemesi Arch'ta
  e-posta içeren her plan için değişiklikten önce `server_setup_service_unsupported:dovecot`
  engelini döndürür. Sihirbaz `setup.blocker.mailUnsupported` metnini gösterir:
  sunucu yöneticisi Web barındırma'yı seçer ya da e-postayı işletim sisteminin
  araçlarıyla kurar. Arch'ta web barındırma değişmedi; var olan Dovecot gözlenmeye
  devam eder, kaldırma etkilenmez.
- **Adlandırılan doğrulanmış hata.** Kurulum hatası artık bileşeni, adımı
  (`preflight`, `package_install`, `configure`, `unit_start`, `verify`) ve makinenin
  tek satırını taşır: paket işleminde paket yöneticisinin `error:`/`E:` satırı,
  aksi hâlde nedenin ilk satırı; 180 karakterle sınırlı, URL kimlik bilgisi/yol/sorgu,
  hash biçimli ve `anahtar=değer` gizli bilgiler çıkarılmış. Başarısız satırın
  mevcut `result_json` alanında `failure` altında saklanır (şema geçişi yok) ve
  servis işlemi ile kurulum hatasında isteğe bağlı `component`/`step`/`detail`
  olarak döner.
- **Sihirbaz.** Bileşen ve adım, "Sunucunun bildirdiği: …", sunucu yöneticisinin
  yapacağı iş, ardından tamamlanan adımlar korunarak "Düzeltilmiş planı incele" ve
  e-posta için Web barındırma seçeneği gösterilir. Yerleşim değişmedi.
- **Agent metni.** `conf.d` olmayan Dovecot artık yanlış "kurulu değil" yerine bu
  yerleşimi bildirir ve `dovecot.conf` dosyasına dokunmaz.

Yapılmayan: Arch'ta e-posta desteğinin kendisi. Paketli ana dosya include'dan
sonra posta, PAM ve TLS ayarlarını yaptığı için `conf.d` oluşturmak yetmez; bu,
sahibin yapılandırmasını devralma kararı (D-022) ve Arch'ta posta TLS, gönderim ve
Roundcube PHP uzantılarının gerçek sistem denetimi gerektirir.

### Güncelleme sırasında aday panelin başlangıç hataları (2026-09-30)

Kaynak durumu bileşen ve sözleşme testleriyle; gerçek sistem denemesi bekliyor.
İki tipli güncelleme nedeni root CLI (`recovery status`), sahip SSH görünümü ve
kurtarma ekranında EN ve TR kendi yönlendirmesini taşır.

- **`candidate_panel_startup_check_failed`** (doğrulanmış hata, `active` aşaması).
  Yeni sürümün paneli, hiçbir şey devreye alınmadan önce salt-okur başlangıç
  denetiminden geçemedi. Sunucu otomatik olarak önceki sürüme döndürülür ve önceki
  sürüm çalışmaya devam eder.
  - Kim: sunucuda kimsenin işlem yapması gerekmez.
  - Sonraki eylem: panelin güncelleme sayfasında bu güncelleme için gösterilen
    neden satırını bildirin.
  - Devam: kurtarma sürerken aynı işlem yeniden denetlenir; doğrulanmış geri
    almadan sonra sürdürülecek bir şey yoktur.
- **`panel_start_unverified`** (doğrulanmış hata, `completion` aşaması). Güncelleme
  uygulandı, ancak yeni sürümün paneli sınırlı bekleme içinde çalışır kalıp kendi
  adresinde yanıt vermedi.
  - Kim: sunucu sahibi.
  - Sonraki eylem: sunucuda `sudo journalctl -u celikpanel-panel -n 50` okuyun.
  - Devam: tamamlama sınırına kadar otomatik yeniden denenir; sonra kurtarma
    günlüğü (`sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50`)
    aynı işlem için tek seferlik yeniden deneme komutunu gösterir. Bu noktadan
    önceki sürüme desteklenen bir dönüş yoktur.

Neden yalnız son kayıtlı hata güncellemenin kendi hatası olduğunda gösterilir;
sonraki kurtarma hatası, bekleme veya duraklatılmış kurtarma kendi metnini korur.
Bilinmeyen nedenler ve eski tarayıcı/CLI genel `update_failed` metnini gösterir.
Panel adresindeki çevrim dışı sayfa artık sahip SSH görünümü komutunu da gösterir
(`recovery view --request-id …`, `127.0.0.1:2084` yönlendirmesiyle). Panel
dururken canlı durum gösteremez.

Açık kalanlar: denetimden geçip açılan ve sonra çöken panel ileri tamamlanır;
`completion.pending` sonrasında geri alma yoktur; panel dururken panel adresinde
canlı tarayıcı durumu yoktur.

### Geri alınan bir güncellemeden sonra (P0.2, 2026-10-01)

Kaynak durumu, bileşen ve sözleşme testleriyle; gerçek sistem denemesi bekliyor.
Kusurlu bir adayın `active` aşamasında başarısız olduğu ve sunucunun kendiliğinden
eski sürüme döndüğü upd2 Debian 13 denemesinden (bulgular O1-O4).

- **Başarısız güncellemeden sonraki bildirim.** Birincil metin artık işçinin ham
  özetinden değil, aynı isteğin kurtarma gözleminden gelir
  (`GET /api/v1/recovery/status`; hiçbir şey başlatmayan ya da yeniden denemeyen
  bir okuma).
  - Geri alma doğrulandı (`recovered`): hedef sürüme güncelleme tamamlanmadı;
    sunucu otomatik olarak önceki sürüme döndürüldü ve şu an onu çalıştırıyor.
    Ardından neden: tipli kod (`candidate_panel_startup_check_failed`,
    `panel_start_unverified`) ya da "tamamlanmadan başarısız oldu; daha belirli
    neden kaydedilmedi". Kim: sunucuda kimse. Sonraki eylem: düzeltilmiş sürüm
    yayımlanana kadar aynı sürümü yeniden başlatmayın; sunucunun iletisi bu
    sunucudaki bir sorunu belirtiyorsa önce onu giderin. Devam: hiçbir şey
    kendiliğinden sürmez; daha yeni sürüm "Güncellemeyi kontrol et" ile görünür.
  - Kurtarma sürüyor, bekliyor, duraklatıldı ya da işlem gerekiyor: kurtarma
    ekranının o durumdaki metinleri; bildirim aynı kaydı okumayı sürdürür.
  - Henüz kayıt yok: önce "okunuyor", sonra "hangi sürümün çalıştığı
    bilinmiyor"; incelenmiş değişiklik öncesi metinler (paket yöneticisi meşgul,
    kabul edilmedi) önde kalır.
  - Sunucunun ham özeti yalnız ikincil "Sunucunun bildirdiği: …" satırıdır;
    hiçbir zaman tek ya da birincil metin değildir.
- **Burada daha önce başarısız olmuş sürüm.** `GET /api/v1/panel/update/check`,
  sunulan hedef commit için yerel kurtarma gözlemlerinden okunan isteğe bağlı
  `previous_attempt` alanını (`request_id`, `phase`, `failure_code`,
  `finished_at`) taşır. Yalnız o commit'e yapılan son kayıtlı deneme `failed` ya
  da `recovered` ile bittiyse vardır. Güncelleme kartı, Başlat düğmesinden önce
  bu sürümün ne zaman denendiğini ve sunucunun önceki sürüme döndürüldüğünü (ya
  da güncellemenin tamamlanmadığını), neden giderilmediyse yeniden başlatmanın
  aynı güncellemeyi tekrarlayacağını ve tipliyse kaydedilen nedeni söyler.
  Başlat aynı onayla kullanılabilir kalır.
- **Root CLI.** Her durum sade sahip yönlendirmesiyle başlar (ne oldu, sunucu ne
  çalıştırıyor, kim, sonraki eylem). "Üretici … kaydetmiş", "Nihai kanıt: none",
  "Önceki hata: update_failed" ve "Kaydedilen neden" satırları kalktı; iç
  belirteçler tek son satırdadır: "Kayıtlı durum (destek için): phase=…
  reason=… proof=…". `--json` bayt bayt aynıdır; yalnız ek dosyası tipli bir neden bildiren duraklatılmış kurtarma son bir anahtar kazanır (aşağıya bakın).
- **Geri alma günlüğü.** "Ürün kaynak commit'i: unknown" yerine, geri yüklenen
  Agent'ın yapı kaydı (manifestle doğrulanmış snapshot'taki
  `bin/agent-native-contract.json`) kurulu Agent baytlarına tam bağlıysa geri
  yüklenen sürümün kaynak commit'i yazılır; değilse commit'in snapshot'ta kayıtlı
  olmadığı söylenir. Snapshot adı (`…-from-unknown-to-…`) ve `commit` dosyası
  değişmedi: `rollback.sh` ikisini de kanonik olarak doğrular.
- **Tipli ilk nedenle duraklatılmış kurtarma.** Otomatik kurtarma deneme
  sınırında duraklatıldığında ve güncellemenin ilk neden ek dosyası tipli bir
  neden bildirdiğinde, duraklatma yönlendirmesi (kurtarma günlüğü ve aynı işlem
  için tek seferlik yeniden deneme komutu) kalır ve öncesine neyin yanlış
  olduğu ile nereye bakılacağı eklenir. `panel_start_unverified` için: yeni
  sürümün paneli açılmadı; `sudo journalctl -u celikpanel-panel -n 50` okunur;
  neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar; önceki sürüme
  desteklenen dönüş yoktur. `candidate_panel_startup_check_failed` için: önceki
  sürüme otomatik dönüş tamamlanmadı; kurtarma günlüğü neyin durdurduğunu
  gösterir. Root CLI ve kurtarma ekranı, EN ve TR. Ek dosya yalnız okunur;
  kurtarma durumu yalnız duraklatmada bulunan isteğe bağlı `first_failure_code`
  alanını kazanır.
- **Başlatma koruması günlüğü.** Bir sürüm işaretçisi Panel ve Agent'ı tutarken
  başlatma yetkisi yoksa (örneğin yeniden açılıştan sonra) günlük "held: a
  CelikPanel update or recovery is in progress; the unit starts when it
  finishes" der. Yanlış türdeki yol yine "unsafe directory" yazar. Çıkış kodu
  aynıdır.

Yapılmayan: teklifin kendisi değişmedi (taban, tasarım gereği adayın sırasında
kalır); panel dururken panel adresinde canlı durum yoktur.

### Barındırma köküne geçiş (upd2 bulgusu P3, 2026-10-01)

Bileşen testleriyle kaynak durumu; yerel yeniden koşu bekliyor. Taze bir Arch
`web` sunucusunda yeni statik site 404 döndürdü ve cron görevi hiç çalışmadı.
Tek kullanımlık konukta yeniden üretim nedeni kanıtladı: Arch'ta `/var/www` yok
(`pacman -Qo /var/www`: hiçbir paket sahip değil); Agent'ın ilk site oluşturması
onu birimin `UMask=0027` ve `Group=celikpanel` ayarıyla `MkdirAll` üzerinden
`0750 root:celikpanel` olarak yaptı (doğum zamanı site ev diziniyle aynı). Ne
nginx (`http`) ne de site kullanıcısı oradan geçebildi.

- **Ürünün oluşturduğu üst dizinler.** Agent, hiçbir site değişikliğinden önce
  `/var/www/celikpanel` dahil üstündeki her eksik dizini tam olarak
  `0755 root:root` oluşturur (`mkdir`'den sonra açıkça ayarlanır) ve yalnız
  root'un okuduğu `/var/lib/celikpanel-agent-private/hosting-root-v1.json`
  makbuzuna yazar (şema `celikpanel/hosting-root-directories/v1`).
- **Sahip dizinleri asla değiştirilmez.** Barındırma kökünün üstünde var olan ve
  web sunucusu hesabının ya da site kullanıcılarının giremediği bir dizin,
  siteyi hiçbir değişiklikten önce reddeder: Agent kodu
  `hosting_root_not_traversable`, Panel `409 HOSTING_ROOT_NOT_TRAVERSABLE`
  (`vars`: dizin, kip, sahip, komut).
  - Kim işlem yapar: sunucu sahibi.
  - Sonraki eylem: geçişe izin vermek, örneğin `sudo chmod 755 /var/www`
    (engelleyen dizin adıyla gösterilir).
  - Devam: siteyi yeniden oluşturmak; hiçbir şey kendiliğinden yeniden denenmez.
- **Kurulum incelemesi.** Site barındıran profiller aynı salt-okur kanıtı
  incelemede çalıştırır. Engelleyen dizin
  `server_setup_hosting_root_not_traversable:<kip>:<sahip>:<grup>:<dizin>`
  engelidir ve aynı sahip eylemini gösterir; sahip sonra planı yeniden inceler.
  Eksik dizin engel değildir. İnceleme hatası günlüğe yazılır, doğrulanmış engel
  olarak gösterilmez.
- **Mevcut sunucular.** Eski sürümler hiçbir şey kaydetmedi; onların oluşturduğu
  `/var/www 0750` sahibin tercihinden ayırt edilemez ve komutla reddedilir.
  Makbuz asla onarım yetkisi vermez: ürünün `0755` oluşturduğu bir dizin sonradan
  engelliyorsa sonradan değiştirilmiştir.
- **Node çalışma ortamları.** Aynı umask `…/runtimes/node` dizinini `0750`, her
  sürüm dizinini (hazırlık dizininden) `0700` yapıyordu; kendi site
  kullanıcısıyla çalışan site uygulamaları oraya ulaşamazdı. İkisi de artık
  açıkça `0755` yapılır.

Yapılmayan: POSIX ACL okunmaz (geçiş için ACL'ye dayanan dizin engelleyici
bildirilir); barındırma kökünün üstündeki sembolik bağ izlenir ama hedefinin üst
dizinleri kanıtlanmaz. Yerel Arch yeniden koşusu sitenin işaretle 200
döndürdüğünü, cron damgasının ilerlediğini ve belge kökünün `namei -l` çıktısını
hâlâ göstermelidir.

### Güncelleme ön denetiminde duruş, otomatik yeniden denemeler ve duraklatılan yenileme (upd3 F1-F3, O6, 2026-10-01)

Bileşen ve sözleşme testleriyle kaynak durumu; gerçek sistem denemesi bekliyor.
upd3 denemesinden (F1, F2, F3 bulguları ve O6 gözlemi).

- **Güncelleme kurulu sürümü değiştirmeden durdu (F1).** Seçili kurtarma çalışma
  ortamının salt-okur ön denetimi (`verify-compatibility`, kurtarma verisi ve
  veritabanı desteği, veritabanı üst verisi) artık başarısız adımı ve denetleyicinin
  ilk tanı satırını `recovery_runtime_preflight_failed` koduyla ve
  `state=unchanged` olarak bildirir; isteğin hata ek kaydına bu kodu yazar. İstek
  sonlanmıştır.
  - Kök CLI, kurtarma ekranı ve güncelleme bildirimi: güncelleme, kurulu sürüm
    değiştirilmeden ve hiçbir hizmet durdurulmadan salt-okur denetimde durdu;
    sunucu önceki sürümünü eskisi gibi çalıştırıyor; neden (bildirimde çevrilmiş
    adım, ikincil sunucu satırında denetleyicinin satırı; CLI işçi günlüğünü
    gösterir: `sudo journalctl -u celikpanel-self-update-<istek>.service --no-pager -n 20`).
  - Kim yapar: neden bu sunucudaki bir durumu (hâlâ süren başka bir işlem, meşgul
    paket yöneticisi) belirtmiyorsa kimse; belirtiyorsa önce bitmesi beklenir ya
    da sorun giderilir. Paket yöneticisi reddi mevcut `package_manager_busy`
    metnini korur.
  - Sürdürme: hiçbir şey kendiliğinden sürmez; güncellemeyi yeniden başlatmak
    güvenlidir. Bildirim bu istek için sorgulamayı bırakır.
  - Böyle bir deneme için "Bu sürüm daha önce başarısız oldu" bildirimi bunun
    yerine kurulu hiçbir şeyi değiştirmeden durduğunu ve yeniden başlatmanın
    güvenli olduğunu söyler.
- **Otomatik denemeler arasında (F3).** Bir otomatik kurtarma denemesi başarısız
  olup zamanlayıcı bir deneme daha yapacaksa hata kaydı isteğe bağlı
  `automatic_recovery=retry_scheduled` ipucunu taşır. CLI, kurtarma ekranı ve
  bildirim sunucunun aynı işlemi kendiliğinden yeniden deneyeceğini (normalde
  önceki denemenin bitişinden yaklaşık 30 saniye sonra, en çok üç deneme), şimdi
  bir şey gerekmediğini ve sahibin yalnız kurtarma durursa işlem yapacağını
  söyler. Güncellemenin ilk tipli nedeni (`panel_start_unverified`: panel günlüğü
  komutu) planlanan yeniden deneme ve sonraki deneme boyunca görünür kalır.
  "Sunucu sahibinin işlem yapması gerekiyor" yalnız duraklamada veya son deneme
  başarısız olduktan sonra görünür.
- **Duraklamada sertifika yenileme (F2).** Duraklama metni otomatik sertifika
  yenilemenin (Certbot) güncelleme için durdurulduğunu ve kurtarma günlüğünün onun
  önceki hâline döndürülüp döndürülmediğini ya da işlem bitene kadar durdurulmuş
  kalacağını söylediğini ekler (hangisinin ne zaman geçerli olduğu aynı tarihli
  dayanıklılık sözleşmesi kaydındadır).
- **Doğrulanmış geri almadan sonra sunucu satırı (O6).** İkincil "Sunucunun
  bildirdiği" satırı güncelleyicinin işaretini, `code=`/`state=` belirteçlerini ve
  `reason=`/`detail=` etiketlerini içermez; okunur bir şey kalmazsa gösterilmez.
  Türkçe bildirimin birincil metni yalnız Türkçedir.

Yapılmayan: ön denetim duruşu bildirimi yalnız kurulu Panel bu değişikliği
içeriyorsa görünür (kart kurulu sürümden gelir; kök CLI, ön denetimin adayınkine
yükselttiği seçili kurtarma kitinden gelir). EXIT tuzağından önceki diğer
güncelleyici hataları hâlâ yalnız kendi durma satırını bırakır.

- **Devamda başlatma sınırı (aynı tarih).** systemd Panel ya da Agent başlatmasını başlatma sınırı yüzünden reddederse kurtarma günlüğü satırı birimi ve `sudo systemctl reset-failed <birim>` komutunu, ardından yeniden denemeyi adlandırır (kod `unit_start_limit_hit`). Her denetimli başlatma artık önce yalnız o birimin sınırını temizlediği için bu seyrek olmalıdır. Kim yapar: sunucu sahibi. Sürdürme: aynı yeniden deneme.

### Reddedilen güncelleme denetimi, biten son deneme, zaten kapalı yenileme (upd4 F4-F6, O7-O9, 2026-10-01)

Bileşen ve sözleşme testleriyle kaynak durumu; gerçek sistem denemesi bekliyor.
upd4 denemesinden (F4, F5, F6 bulguları ve O7, O8, O9 gözlemleri).

- **Salt-okur bir denetim güncellemeyi hiçbir şey değişmeden reddetti (F4).**
  Güncelleyicinin koordinatörler dondurulmadan önceki kendi denetimleri (panel
  işlem kuyruğu, Agent işlemleri, BIND ve posta/DNS uyumluluğu, ilk geçiş durumu)
  artık `state=unchanged` ile tipli `update_preflight_refused` olarak biter; adımı,
  bir neden sınıfını ve denetleyicinin satırını taşır ve isteğin hata kaydını
  yazar. Yayımlanmış bir quiesce önce geri alınır; bu geri alma başarısız olursa
  sonuç kurtarma gerektiren `update_failed` olarak kalır.
  - Eşzamanlı panel yazımı: canlı panel veritabanı ya da `-wal`/`-shm` dosyası
    denetim okurken değişti (meşgul kuyruk değil, panel kaydı veya checkpoint).
    Denetim 2 sn sonra bir kez daha okunur; ancak yine değişirse güncelleme durur.
    Meşgul işlem kuyruğu ya da başka bir ret asla yeniden okunmaz.
  - Kök CLI, kurtarma ekranı ve güncelleme bildirimi: güncelleme kurulu sürümü
    değiştirmeden ve hiçbir hizmeti durdurmadan durdu ve sunucu önceki sürümünü
    çalıştırıyor. Bildirim nedeni adlandırır (eşzamanlı yazım: "denetim panel
    veritabanını okurken panel veri kaydediyordu ... Sunucuda bir sorun yok";
    sırada ya da çalışan işlem: "güncellemeyi yeniden başlatmadan önce bitmesini
    bekleyin"; diğer adımlar denetimi adlandırır); CLI nedeni taşıyan güncelleme
    günlüğü satırını gösterir.
  - Kim yapar: neden başka bir işlemi adlandırmıyorsa kimse; adlandırıyorsa
    bitmesini bekleyin.
  - Sürdürme: hiçbir şey kendiliğinden sürmez; güncellemeyi yeniden başlatmak
    güvenlidir. Bildirim sorgulamayı bırakır.
  - Panel böyle bir özeti uzun olduğu ya da yol içerdiği için artık düşürmez:
    kapalı belirteçlerden (kod, durum, adım, neden sınıfı) kurulan sınırlı bir
    biçim gösterir. Tam satır Agent günlüğünde kalır ("System update worker
    failed: …").
- **Başarısız panel veritabanı anlık görüntüsü nedenini korur (F5).** Anlık görüntü
  aracının ilk tanı satırı (günlük zaman damgası olmadan, yazdırılabilir, en çok
  240 bayt) artık hata satırında (`detail=`), dolayısıyla Agent günlüğünde ve
  güncelleme durum kaydındadır. Otomatik geri alma değişmedi. Burada yeniden okuma
  yoktur.
- **Son deneme bitiyor (F6).** Üçüncü otomatik deneme ya da sahibin yeniden
  denemesi başarısız olunca kayıt, sonraki zamanlayıcı çalışması duraklamayı
  kaydedene kadar `automatic_recovery=pause_pending` taşır. Her okuyucu
  güncellemenin ilk tipli nedenini korur, kurtarmanın son denemesini bitirdiğini
  ve sonraki adımı ile tek seferlik yeniden deneme komutunu içeren duraklamanın
  yaklaşık bir dakika içinde kaydedileceğini, o zamana kadar bir şey gerekmediğini
  söyler. "Sunucu sahibinin işlem yapması gerekiyor" yalnız kaydedilen duraklamada
  görünür.
- **Kurtarma sürerken sunucu satırı (O7).** Bildirim ham güncelleyici satırını
  asla göstermez: iç belirteçler her durumda çıkarılır ve gözden geçirilmiş çevrili
  bir özet aynısını söylüyorsa (paket yöneticisi meşgul, çevrili neden sınıfı)
  satır gösterilmez. Türkçede "Sunucunun İngilizce günlük satırı" olarak etiketlenir.
- **Duraklamada yenileme (O8).** Güncelleyici, Certbot zamanlayıcısının
  güncellemeden önce açık (bir zamanlayıcı etkin ya da çalışıyor) mı kapalı mı
  olduğunu kaydeder. Kapalıysa duraklama, yenilemenin zaten kapalı olduğunu ve
  güncellemenin onu durdurmadığını söyler (CLI ayrıca: açık olması gerekip
  gerekmediğini kontrol edin); aksi hâlde ya da kayıt yoksa mevcut cümle kalır.
- **Önceki hatadan sonra doğrulanan güncelleme (O9).** Başarılı bir yeniden
  denemeden sonra kurtarma ekranı ve bildirim hata başlığı ya da "Kurtarma başarısız
  oldu" etiketi göstermez; SSH sahip görünümü önceki denemelerin tamamlanmadığını
  ve sonraki bir denemenin güncellemeyi tamamladığını söyler.

Yapılmayan: reddedilen denetimin adım ve neden sınıfı metinleri sunucu ekran
katalogundadır (açılış katalogunda yer yok); o gelene kadar bildirim genel nedeni
gösterir. Bu duruşun bildirimi bu değişikliği içeren kurulu bir Panel gerektirir.
upd4 anlık görüntü hatasının tetikleyicisi hâlâ bilinmiyor; sonraki gerçek sistem
denemesi onu kaydeder.

### Aday incelemesi: bilinmeyen güncelleme durumu, geri alınan quiesce, erken sahip yeniden denemesi (2026-10-01)

Bileşen ve sözleşme testleriyle kaynak durumu; gerçek sistem denemesi bekliyor.
Dayanıklılık sözleşmesindeki aynı tarihli "Aday incelemesi düzeltmeleri" girişine
bakın.

- **Bilinmeyen güncelleme durumu.** Bir güncellemenin kayıtlı durumu yoksa (durum
  kaydetmeyen kurulu bir sürüm başlattı ve işçi kimliği bulunamadı ya da işçisinin
  dışında çalıştı), root CLI yine sonucun bilinmediğini ve yeniden sorgulamayı
  söyler; artık bilinmiyor olarak kalırsa nereye bakılacağını da ekler: kurtarma
  günlüğü `sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50`
  otomatik kurtarmanın çalışıp çalışmadığını ya da durduğunu ve duraklamada tek
  seferlik yeniden deneme komutunu gösterir. Güncelleyicinin ve çalıştırıcının
  günlük satırları da aynısını söyler. v0.1.0-alpha.80'in başlattığı bir
  güncelleme artık normalde durumunu kendisi kaydeder.
- **Geri alınan yarım güncelleme (quiesce).** Kurtarma, panel dondurulmuşken yarım
  kalmış bir güncelleme bulursa panel ve Agent'ı güncellemeden önceki durumlarına
  döndürür ve güncelleme biter: durum "güncelleme başarısız oldu" olur ve günlük,
  kurulu sürüm veya verileri değişmeden durduğunu söyler. Kim işlem yapar: kimse.
  Devam: güncellemeyi panelden yeniden başlatın; hiçbir şey kendiliğinden yeniden
  denenmez. "Zamanlayıcı yeniden dener" ya da bekleyen duraklama metni gelmez.
- **Otomatik deneme hakkı kalmışken sahip yeniden denemesi.** Sahip, üç otomatik
  deneme kullanılmadan yeniden dener ve deneme başarısız olursa günlük kaç otomatik
  deneme kaldığını ve zamanlayıcının sonrakini başlatacağını söyler; henüz sahip
  işlemi gerekmez ve yenileme değiştirilmez. Duraklama ve yeniden deneme komutu
  ancak otomatik denemeler bitince gelir.
- **Başlangıç denetiminin okuyamadığı panel ortamı.** Güncelleme hata satırı genel
  kodu korur ve denetimin panel ortamını okuyamadığını (birim ortamında, ek
  dosyalarında ya da `panel.env` içinde tırnak, ters eğik çizgi, bilinmeyen anahtar
  ya da desteklenmeyen karakter) ve yeni panelin denetlenmediğini söyler;
  güncelleme yine önceki sürüme döndürülür. Kim işlem yapar: girdi onunsa sunucu
  sahibi; sonra güncellemeyi yeniden başlatır.
- **Eksik curl.** `/usr/bin/curl` bulunmayan ana makinede güncelleme herhangi bir
  değişiklikten önce "required update tool is missing: /usr/bin/curl; install it
  explicitly before retrying" ile durur.
- **Kurtarma çalışma ortamı ön denetiminde duruş.** Günlük satırı artık kurulu
  sürüm ve verilerinin değiştirilmediğini söyler (EN ve TR).

Yapılmayan: web güncelleme ekranları metinlerini korur ("kurulu olan hiçbir şey"
ve "kurulu dosyalar" değişmeden durdu anlamındaki metinler); alpha.80'in başlattığı
başarılı bir güncelleme, yeni Agent o isteği ilk kez denetleyene kadar
"uygulanıyor" gösterir. 2026-10-01'de düzeltildi (`706c1c91`): iki web metni de
artık kurulu sürümün ve verilerinin değiştirilmediğini söyler.

### Kurulumda ve güncellemeden önce meşgul paket yöneticisi (upd8 F1/F2, 2026-10-01)

Bileşen ve sözleşme testleriyle kaynak durumu; Ubuntu'da gerçek sistem denemesi
bekliyor. Aynı tarihli dayanıklılık sözleşmesi girdisine bakın.

- **Boştaki PackageKit hizmeti artık engellemez.** Ubuntu'da apt, her paket
  işleminden sonra PackageKit'i başlatır ve o yaklaşık beş dakika boşta kalır.
  Kurulum adımları, güncelleme başlatma, hizmetler sayfasının hazırlık bilgisi ve
  güncelleme/geri alma denetimleri bu boştaki hizmet için artık "paket yöneticisi
  meşgul" demez. Gerçekten bir paket işi çalışırken yine der: apt, dpkg ve
  listedeki diğer araçlar ya da alt süreci veya apt/dpkg kilidi olan PackageKit.
- **Gerçek bir paket işi çalışırken sahibin gördüğü.** Bu nedenle reddedilen bir
  kurulum adımı, posta profili ya da güvenlik duvarı adımı artık, nedeni yalnız
  panel günlüğünde kalan `mail_profile_install_failed` /
  `server_setup_firewall_failed` yerine `HOST_MUTATION_BUSY` kodunu ve var olan
  "Bu sunucunun paket yöneticisi meşgul — CelikPanel dışında bir şey paket kuruyor
  ya da güncelliyor. Bir dakika sonra yeniden deneyin." cümlesini gösterir.
  - Kim işlem yapar: paket işi dışında kimse; iş sunucu sahibinin ise ve bitmiyorsa
    sunucu sahibi.
  - Devam: kurulum kendiliğinden sürmez; iş bittikten sonra yeni bir kurulum planı
    inceleyip başlatın (tamamlanan adımlar korunur). Güncelleme
    `package_manager_busy` metnini korur: yeniden başlatın.

Yapılmayan: kurulum sihirbazında `HOST_MUTATION_BUSY` için başlık yok; genel
"Gerekli bir kontrol tamamlanmadı" metnini ve yukarıdaki cümleyi "Ayrıntılar"
altında gösterir (web eşlemesi gerekir). 2026-10-01'de düzeltildi (`fb04289b`).
Engelleyen iş adlandırılmaz. Reddedilen kurulum adımı kendiliğinden bekleyip
yeniden denemez.

2026-10-01'de düzeltildi (upd9 F1-F3; bileşen testleri, Ubuntu'da gerçek sistem
denemesi yeniden bekliyor). Ubuntu 24.04'te yukarıdaki boştaki hizmet yine de
engelliyordu: kural yanlış PackageKit arka uç dosya adını arıyordu; artık
Ubuntu'nunkini tanır.

- **Gerçek paket etkinliği nedeniyle reddedilen kurulum adımı.** Panel'in
  ("Ayrıntılar" altında gösterilen) cümlesi artık "This server's package manager
  is busy — a package task is still running on this server. Wait for it to
  finish, then try again." İş CelikPanel'in kendi önceki adımı olabilir ve süresi
  bilinmez; bu yüzden cümle artık "CelikPanel dışında" ya da "bir dakika" demez.
  Sihirbaz başlığı değişmedi.
- **Gerçek paket etkinliği nedeniyle reddedilen güncelleme başlatma.** Sahip genel
  ve çevrilmemiş "the update service did not accept this request"
  (`PANEL_UPDATE_START_REFUSED`) yerine `HOST_MUTATION_BUSY`, `package_manager_active`
  nedeni ve kendi dilinde paket yöneticisi cümlesini alır. Kim işlem yapar: paket
  işi dışında kimse. Devam: güncelleme kartı sunucuyu hazır gösterene dek bekleyin,
  sonra güncellemeyi yeniden başlatın; hiçbir şey kurulmadı ya da kaydedilmedi.
  Nedeni göndermeyen eski Agent genel metni korur.
- Yapılmayan: tarayıcının `HOST_MUTATION_BUSY` / `package_manager_active` katalog
  cümlesi (EN ve TR) hâlâ "CelikPanel dışında" ve "bir dakika" der; `web/`
  içinde aynı yeniden yazım gerekir.

### Güncelleme sunucuyu tutarken posta başlangıç işi (upd11 F2, 2026-10-02)

Bileşen testleriyle kaynak durumu; gerçek sistem denemesi bekliyor. Aynı tarihli
dayanıklılık sözleşmesi girdisine bakın. Yalnız günlük (her Panel günlük satırı
gibi İngilizce); hiçbir ekran değişmedi.

- **Mevcut neden.** Bir güncelleme ya da geri alma içinde başlayan Panel, posta
  sertifikası (SNI) kümesini yayımlayamaz ve Postfix posta filtrelerini
  besteleyemez, çünkü güncelleme sunucuyu hâlâ tutar. `certificate startup
  reconcile: certificate dependents: … still running; …` ve `milter wiring at
  startup: … still running` başlangıç satırları artık "the Panel retries this by
  itself every 30 seconds for up to 10 minutes once no other server change is
  running; nothing needs to be done now" ile biter. Posta bu arada mevcut
  yapılandırmasıyla çalışmayı sürdürür.
- **Kim işlem yapar.** Güncelleme biterken kimse.
- **Nasıl devam eder.** Kendiliğinden: tek satır `startup mail work, attempt N of
  20: mail certificate publication completed (…)` / `mail filter wiring
  completed (…)`. Doğrulanmış bir hata o satırda bir kez adlandırılır ("failed: …;
  it is not retried now and runs again at the next Panel start (sudo systemctl
  restart celikpanel-panel)") ve yinelenmez.
- **Vazgeçerse.** Sunucu 10 dakika boyunca serbest kalmazsa satır, işin hâlâ
  yapılmadığını, bu denemelerin hiçbir şeyi değiştirmediğini ve postanın
  çalışmayı sürdürdüğünü söyler; diğer iş bittikten sonra sunucu yöneticisi
  `sudo systemctl restart celikpanel-panel` çalıştırır; SSL sayfası "posta TLS
  eşitlemesi tamamlanmadı" diyen bir alan adı orada "Etkinleştirmeyi yeniden
  dene"yi de kullanabilir.

Yapılmayan: hiçbir ekran ertelemeyi göstermez; Panel başlangıcında kendi posta
adımı reddedilen bir sertifika yenilemesi hâlâ alan adının "Etkinleştirmeyi
yeniden dene" işlemini ya da bir sonraki Panel başlangıcını bekler.

### Kurulum sırasında planlı sertifika devri: bilinmeyen sonuç ekranları yerine yönlendirme (2026-10-08)

Birim, sözleşme ve bağlanmış bileşen testleriyle kaynak durumu; gerçek sistem
denemesi yapılmadı. Ekranlar 8 Ekim 2026'da gerçek bir tarayıcıda, yerel bir
taklit sunucuya karşı incelendi ve düzeltildi (sonraki kaydın sonundaki
"Tarayıcı incelemesi" bölümüne bakın). v0.1.0-alpha.81 çalıştıran kurulu bir
Ubuntu sunucusunda, `https://<sunucu-IP>:2083` adresinden oturum açmış bir sahip
tarafından bulundu. Etkilenen sözleşme maddeleri: dayanıklılık ilkeleri 2 ve 6
(P0.2 kapsamı); hiçbir kabul maddesi kapanmadı.

**Sahibin gördüğü.**

1. İlk deneme: "Bileşeni hazırla: Nginx" adımı genel başlıkla durdu: "Kurulum
   durdu, çünkü başka bir sunucu değişikliği veya paket işlemi hâlâ sürüyor…".
   Neyin meşgul olduğu söylenmedi. Dakikalar sonraki ikinci deneme çalıştı.
2. İkinci denemede "certbot kuruluyor" başlıklı bir katman "Bağlantı kesildi.
   Panel kilidi açılmadan yeniden bağlanılıyor…" ve "Sunucunun son durumu
   doğrulanamadı…" dedi; ardından tam sayfa "Panelin hazır olma durumu kontrol
   edilemedi" ekranı, ilgisiz bir "Güncelleme ve kurtarma durumu: Güncelleme
   doğrulandı" bölümünün üstünde göründü. Sayfa sonra kendiliğinden düzeldi ve
   "Sunucunuz hazır" gösterdi. Sunucuda hiçbir sorun yoktu.

**Sunucuda olan (koddan okundu, o sunucuda gözlenmedi).** "Panel erişimini
güvenceye al" adımı sertifikayı alır; Agent ardından paneli, sertifikayı sunması
için bir kez yeniden başlatır (`systemctl restart celikpanel-panel`); bu, adımın
kendi Agent işleri arasındaki bir boşlukta ya da adımın hemen ardından olur.
Panel çalışırken sertifika değiştirmez. Yeniden başlatmadan sonra alan adıyla
gelen bağlantı yeni sertifikayı alır; IP adresiyle gelen bağlantı başlangıçtaki
kendinden imzalı sertifikayı almayı sürdürür, bu yüzden IP adresinde açık sayfa
kendiliğinden yeniden bağlanır. Katman başta bir bağlantı kaybı değildi:
tarayıcı biten certbot adımını üstlenmişti ve ardından gelen katalog taraması,
kurulum sürdüğü sürece `server_setup_busy` ile reddedildi; katman bunu "bağlantı
kesildi" diye bildirdi. Tam sayfa, yeniden başlatmadan (`PANEL_STARTING`) geldi;
güncelleme bölümü tarayıcıda kayıtlı bir güncelleme kimliğini okur ve her
yöneticiye gösterilir.

**Ürünün artık söylediği.** Anahtarlar `web/src/i18n` altındadır; `{host}`
incelenen panel alan adıdır.

- *Adımdan önce ve adım sırasında, tarayıcı o alan adında değilken* (inceleme
  sayfasında ve ilerleme sayfasında adım listesinin üstünde tek blok, sakin
  yönlendirme yüzeyi, uyarı biçimi yok; tarayıcı incelemesine kadar inceleme
  sayfasında adım listesinin altında, 1440×900 ekranda görünür alanın dışında
  duruyordu):
  - `setup.handover.title` — TR "Bu kurulum sırasında panel bir kez yeniden
    başlar" · EN "The Panel restarts once during this setup"
  - `setup.handover.notice` — TR "“Panel erişimini güvenceye al: {host}” adımında
    panel sertifikasını alır ve onu kullanmaya başlamak için bir kez yeniden
    başlar. Bu sayfanın bağlantısı o sırada kısa süreliğine kesilir. Bu planlı
    bir durumdur: kurulum sunucuda devam eder ve bu sayfa kendiliğinden yeniden
    bağlanır. Sizin bir şey yapmanız gerekmez." · EN "At the step “Secure panel
    access: {host}”, the Panel gets its certificate and restarts once to start
    using it. This page then loses its connection for a short time. That is
    planned: setup continues on the server and this page reconnects by itself.
    You do not need to do anything."
  - `setup.handover.address` — TR "O adım bittikten sonra panel güvenli
    adresinden de açılabilir:" · EN "Once that step has finished, the Panel can
    also be opened at its secure address:"; aynı cümlenin devamında, oradaki
    sihirbaza giden bağlantı olarak `https://{host}:<port>`. Bu bir bilgidir,
    yapılacak bir iş değildir: bildirim az önce kimsenin bir şey yapması
    gerekmediğini söylemiştir (8 Ekim 2026 düzeltmesi; önceki metin "O adımdan
    sonra paneli güvenli adresinden açın:" / "After that step, open the Panel at
    its secure address:" idi).
  - `setup.handover.addressHelp` — TR "Orada yeniden giriş yapılır ve kurulum
    aynı ilerlemeyi gösterir. Tarayıcı o adreste sertifika uyarısı verirse adım
    henüz bitmemiştir; bu sayfa kurulumu kendiliğinden izlemeyi sürdürür." · EN
    "You sign in again there, and setup shows the same progress. If the browser
    warns about the certificate at that address, the step has not finished yet,
    and this page keeps following setup by itself." (8 Ekim 2026 düzeltmesi;
    önceki metin "…; bu sayfaya dönüp bekleyin." / "…; return to this page and
    wait." ile bitiyordu.)
  - `setup.handover.stepNote`, bildirim gösterilirken inceleme listesinde ve
    ilerleme listesinde o adımın satırında — TR "Panel burada bir kez yeniden
    başlar." · EN "The Panel restarts once here."
- *Bağlantı koptuğunda ve okunan son durum sertifika adımını en öne koyuyorsa*
  (çalışıyor; önceki bütün adımlar bitmiş ve sırada; ya da bitmiş ve sonraki
  hiçbir adım bitmemiş). "Kuruluma yeniden bağlanılıyor / Sonuç henüz
  doğrulanmadı…" metninin yerini alır:
  - `setup.handover.dropTitle` — TR "Panel bu adımda yeniden başlar" · EN "The
    Panel restarts at this step"
  - `setup.handover.drop` — TR "Kurulum {host} için panel erişimini güvenceye
    alırken bu sayfanın bağlantısı kesildi. Bu beklenen bir durumdur: panel,
    yeni sertifikasını kullanmaya başlamak için bu adımda bir kez yeniden
    başlar. Kurulum sunucuda devam eder ve bu sayfa kendiliğinden yeniden
    bağlanır. Kurulumu yeniden başlatmayın." · EN "This page lost its connection
    while setup was securing panel access for {host}. That is expected: the
    Panel restarts once at this step to start using its new certificate. Setup
    continues on the server and this page reconnects by itself. Do not start
    setup again."
  - `setup.handover.dropAddress` (yalnız başka adreste) — TR "Bu sayfa birkaç
    dakika içinde yeniden bağlanmazsa panelin güvenli adresinden devam edin:" ·
    EN "If this page has not reconnected after a few minutes, continue at the
    Panel’s secure address:"; aynı bağlantı ve yardım satırıyla. "Bu işleme
    yeniden bağlan" ikincil eylem olarak kalır; yalnızca okur.
  - Kim işlem yapar: kimse. Nasıl devam eder: kendiliğinden. Başka bir adımdaki
    kopma bilinmeyen sonuç metnini korur.
- *Panel başlarken tam sayfa*, yalnız bu tarayıcı planında bu adım bulunan bir
  kurulum başlatmışsa ve panelin kendisi o alan adı için yönetilen bir sertifika
  bildiriyorsa (`GET /api/v1/panel/access-address`):
  - `recovery.handoverTitle` — TR "Panel kurulum sırasında bir kez yeniden
    başlar" · EN "The Panel restarts once during setup"
  - `recovery.handoverHelp` — TR "Kurulum {host} için panel erişimini güvenceye
    aldı; panel yeni sertifikasını kullanmaya başlamak için bir kez yeniden
    başlar. Kurulum sunucuda devam eder. Bu sayfa kendiliğinden kontrol eder ve
    panel hazır olduğunda açılır; sizin bir şey yapmanız gerekmez." · EN "Setup
    secured panel access for {host}, and the Panel restarts once to start using
    its new certificate. Setup continues on the server. This page checks by
    itself and opens when the Panel is ready; you do not need to do anything."
  - `recovery.handoverAddress` (yalnız başka adreste) — TR "Dilerseniz panelin
    güvenli adresinden de devam edebilirsiniz:" · EN "You can also continue at
    the Panel’s secure address:"; bağlantıyla birlikte.
  - Güncelleme ve kurtarma durumu sayfada kalır, kendi başlığı altında kapalı
    durur; neden buymuş gibi gösterilmez. O sunucu bildirimi yoksa sayfa mevcut
    metnini korur.
- *Katman.* Biten bir kurulum adımı artık kurulumun geri kalanı boyunca katmanı
  "certbot kuruluyor / Bağlantı kesildi" ile tutmaz. Ardından gelen tarama
  `server_setup_busy` ile reddedildiğinde sayfa, işlemin son aşamasında kendisinin
  sakladığı taramayı okur (`GET /api/v1/managed-services`; sunucu yeniden
  yoklanmaz). Katman yalnızca bu kayıtlı tarama işlemin başladığı saniyeden eski
  değilse ve bileşeni kurulu gösteriyorsa bırakılır; taşıdığı katalog açık
  sayfalara yayımlanır ve olağan "kuruldu" iletisi gösterilir. Kayıtlı işlem tek
  bir yerde, doğrulayan bir snapshot'tan sonra silinir (8 Ekim 2026 düzeltmesi:
  ilk sürüm katmanı hiçbir snapshot olmadan, yalnızca redde dayanarak
  bırakıyordu). Kayıtlı tarama okunamaz ya da adımı doğrulamazsa katman mevcut
  metniyle kalır ve bir tarama kabul edilene dek yeniden denenir. Reddedilen ya
  da başarısız olan diğer her tarama yanıtı eskisi gibi ele alınır. Yeni katman
  metni yok.
- *Sunucu meşgul olduğu için reddedilen kurulum adımı.* Başarısız adım artık
  tipli nedeni (`error.reason`) taşır ve metin ondan seçilir; eski kayıtlar için
  Panel cümlesi yedek olarak kalır. Agent artık "kirayı başka bir Agent işi
  tutuyor" durumunu adlandırır (`agent_mutation_active`) — panelin başlangıçtan
  sonraki kısa işleri, bir sertifika etkinleştirmesi ya da bir yenileme böyle
  işler olarak çalışır — böylece sahip genel metin yerine
  `setup.blocker.changeBusy` metnini okur. 8 Ekim 2026 tarayıcı incelemesinden
  beri neden, ilerleme görünümünün tek başlığıdır; gövdesi hemen altında, sakin
  yönlendirme yüzeyinde durur (tutulan sunucuda: dikkat tonu); "Düzeltilmiş
  planı incele" eylemi o bloğun içindedir ve "Teknik ayrıntılar" altında kapalı
  durur. Genel başlık, iki genel paragraf ve hata rengi bu durumda gösterilmez.
  Başlık · gövde, önce TR sonra EN:
  - `setup.blocker.packageBusyTitle` "Kurulum durdu: bu sunucuda başka bir paket
    işlemi sürüyor" · `setup.blocker.packageBusy` "Diğer işlem otomatik bir
    güncelleme olabilir. Bu bir arıza değil. Bitmesini bekleyin, ardından
    Düzeltilmiş planı incele’yi seçip kurulumu yeniden başlatın. Tamamlanan
    adımlar korunur." — "Setup stopped: another package task is running on this
    server" · "The other task may be an automatic update. Nothing is wrong. Wait
    for it to finish, then choose Review a revised plan and start setup again.
    Steps that already finished are kept."
  - `setup.blocker.changeBusyTitle` "Kurulum durdu: bu sunucuda başka bir
    CelikPanel değişikliği sürüyor" · `setup.blocker.changeBusy` "O değişikliğin
    tamamlanmasını bekleyin, ardından Düzeltilmiş planı incele’yi seçip kurulumu
    yeniden başlatın. Tamamlanan adımlar korunur." — "Setup stopped: another
    CelikPanel change is still running on this server" · "Wait for that change
    to finish, then choose Review a revised plan and start setup again. Steps
    that already finished are kept."
  - `setup.blocker.hostHeldTitle` "Kurulum durdu: tamamlanmamış bir değişiklik
    bu sunucuyu hâlâ tutuyor" · `setup.blocker.hostHeld` "Bu durum beklemekle
    geçmez. Sunucu yöneticisi sunucuyu yeniden başlatır, ardından Düzeltilmiş
    planı incele’yi seçip kurulumu yeniden başlatır; tamamlanan adımlar korunur.
    Yeniden başlatmadan sonra yine olursa incelenmesi gerekir." — "Setup
    stopped: an unfinished change is still holding this server" · "Waiting will
    not clear it. The server administrator restarts the server, then chooses
    Review a revised plan and starts setup again; steps that already finished
    are kept. If the hold returns after the restart, it needs investigating."
  - Tipli neden yoksa: `setup.blocker.hostBusyTitle` "Kurulum durdu: bu sunucu
    başka bir değişiklikle meşgul" · `setup.blocker.hostBusy` "Başka bir sunucu
    değişikliği veya paket işlemi hâlâ sürüyor. Tamamlanmasını bekleyin,
    ardından Düzeltilmiş planı incele’yi seçip kurulumu yeniden başlatın.
    Tamamlanan adımlar korunur." — "Setup stopped: this server is busy with
    another change" · "Another server change or package task is still running.
    Wait for it to finish, then choose Review a revised plan and start setup
    again. Steps that already finished are kept."
- *İlerleme görünümünde her durum için tek başlık* (8 Ekim 2026). Bölüm başlığı
  ile yönlendirme bloğunun başlığı neredeyse aynı şeyi iki kez söylüyordu
  ("Kurulum için işlem gerekiyor" altında "Devam etmek için işlem gerekiyor").
  Duran, bekleyen ya da sonucu doğrulanan kurulum artık bir kez, yönlendirme
  başlığıyla adlandırılır (`setup.guide.failedTitle`, `setup.guide.waitTitle`,
  `setup.guide.confirmTitle`, `setup.licenseWaiting`,
  `setup.guide.buildChangedTitle`); altındaki blok ilgili adımla başlar. Süren
  kurulum "Sunucunuz hazırlanıyor" altında "Bu adımda ne yapılacak?" başlığını
  korur. Duran kurulumda "Düzeltilmiş planı incele" eylemi, bildirilen hatanın
  ardından ve adım listesinin altında değil üstünde durur. Hata rengi yalnızca
  duran kurulumda kullanılır; karşılanmamış bir önkoşul ve henüz doğrulanan bir
  sonuç olağan metin renkleriyle çizilir.

**Kayıt biçimi.** `error.reason`, kurulum yürütme kaydında ve API'de isteğe bağlı
bir alandır; eski kayıtlar ve yeni kaydı okuyan eski bir Panel etkilenmez. Alt
işlem satırı hâlâ yalnız kod ve cümle saklar; nedeni Panel'de cümleden geri
okunur. Tarayıcının kurulum başlangıç işaretine isteğe bağlı bir `handover`
alanı eklenir. Geçiş (migration) yok.

**Ele alınmayan.**

- 2026-10-08'de neyin meşgul olduğu saptanmadı; o sunucunun Agent günlüğü
  gerekir. Agent'ın sahibini belirleyemediği bir kilit ya da bir güncellemenin
  sürüm kapısı hâlâ genel başlığı verir.
- Reddedilen adım kurulumu hâlâ durdurur; kendiliğinden bekleyip devam etmez.
- Engelleyen iş yalnız türüyle adlandırılır ("başka bir CelikPanel değişikliği"),
  ne yaptığıyla değil.
- Bir kurulum adımı kurulurken sekme yeniden odak alırsa katman sihirbazı hâlâ
  örtebilir.
- Başlangıçtaki kendinden imzalı sertifika yoksa, süresi dolmuşsa ya da IP adresi
  içermiyorsa IP adresindeki sayfa yeniden başlatmadan sonra yeniden bağlanamaz;
  bildirim o durumda güvenli adres bağlantısına dayanır. Denenmedi.
- Tam sayfada, aynı kurulumda sonraki bir adım bitmeden gerçekleşen ikinci bir
  panel yeniden başlatması da bu yeniden başlatma olarak açıklanır.
- Gerçek sunucuda doğrulanmadı: yeniden başlatmanın adım listesine göre
  zamanlaması ve tam sayfanın on saniyelik yeniden denetimi. Metinlerin yerinde
  görünümü tarayıcıda yalnızca taklit sunucuya karşı görüldü (aşağıdaki
  "Tarayıcı incelemesi").

### Erişim ve hazır olma kontrolleri sayfayı korur: açıklanan bekletme, kontrol durumu, sona eren oturum (2026-10-08)

Kaynak durumu, bileşen testleriyle; gerçek sistem koşusu yok. Ekranlar 8 Ekim
2026'da gerçek bir tarayıcıda, yerel bir taklit sunucuya karşı incelendi ve
düzeltildi (aşağıdaki "Tarayıcı incelemesi"). v0.1.0-alpha.81 çalışan kurulu
bir sunucunun sahibi tarafından bulundu.
Etkilenen sözleşme maddeleri: dayanıklılık ilkeleri 2, 3 ve 6 (P0.2 kapsamı);
hiçbir kabul işi kapanmaz. Mekanizma ve değişikliğin tamamı
[dayanıklılık sözleşmesinde](RESILIENCE-CONTRACT.tr.md#açık-sayfayı-yalnızca-bilinen-olumsuz-erişim-sonucu-değiştirir-p02-2026-10-08).

**Sahibin gördüğü.** Bir sayfadan bir süre ayrıldıktan sonra: tam ekran "Lisans
durumu kontrol edilemedi" ve "Panel erişimini kontrol et"; altında, günler önce
bitmiş bir güncelleme için "Güncelleme ve kurtarma durumu: Güncelleme
doğrulandı". Dönünce panel yeniden açıldı; açık pencere, yazılanlar ve seçili
sekme yoktu. Lisans baştan sona geçerliydi.

**Ürün şimdi ne diyor.** Anahtarlar `web/src/i18n/screens` içindedir; yalnızca
açık bir sayfanın üzerinde ya da ardından gösterilir.

- *Sekme gizliyken süresi dolan karar ya da 1,5 sn içinde yanıtlanan okuma:*
  hiçbir şey gösterilmez. Okuma yanıtlanana kadar sayfa kullanılamaz.
- *Okuma 1,5 sn sonra yanıtlanmadı* (sayfanın üzerinde katman, hiçbir şey
  başarısız olmadı): başlık `recovery.checkingTitle`, TR "Panel erişimi kontrol
  ediliyor" · EN "Checking panel access"; `accessHold.waitingHelp`, TR "Panel
  henüz yanıt vermedi. Yanıt verir vermez bu sayfa kaldığı yerden devam eder." ·
  EN "The Panel has not answered yet. This page continues where it was as soon as
  it does."
- *Okuma erişimi doğrulamadan yanıtlandı* (önce neden):
  - Lisans sonucu okunamadı: `accessHold.licenseTitle`, TR "Panel erişimi az önce
    doğrulanamadı" · EN "Panel access could not be confirmed just now";
    `accessHold.licenseHelp`, TR "CelikPanel az önce bu sunucunun lisans sonucunu
    okuyamadı. Bu, lisansınızın eksik veya süresinin dolmuş olduğu anlamına
    gelmez." · EN "CelikPanel could not read the license result for this server a
    moment ago. This does not mean your license is missing or expired."
  - Hazır olma durumu okunamadı: `accessHold.availabilityTitle`, TR "Panel az
    önce yanıt vermedi" · EN "The Panel did not answer just now";
    `accessHold.availabilityHelp`, TR "CelikPanel panelin hazır olduğunu
    doğrulayamadı. Panel yeniden başlıyor olabilir." · EN "CelikPanel could not
    confirm that the Panel is ready. It may be restarting."
  - Oturum okunamadı: `accessHold.authTitle`, TR "Oturumunuz az önce
    doğrulanamadı" · EN "Your session could not be confirmed just now";
    `accessHold.authHelp`, TR "CelikPanel az önce oturumunuzu okuyamadı. Bu,
    oturumunuzun kapatıldığı anlamına gelmez." · EN "CelikPanel could not read
    your session a moment ago. This does not mean you were signed out."
  - Panel başlatıldığını bildiriyor: mevcut `recovery.startingTitle`, TR "Panel
    başlatılıyor" · EN "The panel is starting"; `recovery.startingHelp` ile,
    artık TR "Sunucu panel erişimini hazırlıyor. Bu sayfa hazır olma durumunu
    otomatik kontrol eder." · EN "The server is preparing panel access. This page
    checks readiness automatically." ("Güncellemenizin kaydedilmiş son sonucunu
    aşağıda inceleyebilirsiniz." / "You can inspect the last recorded result of
    your update below." cümlesi çıkarıldı; çünkü blok artık her zaman orada
    değil).
  - Kurulumun planlı sertifika yeniden başlatması sırasında mevcut
    `recovery.handoverTitle`, `recovery.handoverHelp` ve
    `recovery.handoverAddress` sihirbazın üzerinde kullanılır.
- *Kim işlem yapar ve iş nasıl sürer* (nedenin altında, planlı yeniden başlatma
  dışında): `accessHold.resume`, TR "Şimdilik bir şey yapmanız gerekmiyor.
  CelikPanel kendiliğinden yeniden kontrol eder. Erişim doğrulandığında bu sayfa,
  yazdıklarınızla birlikte kaldığı yerden devam eder. O zamana kadar bu sayfada
  değişiklik yapılamaz." · EN "You do not need to do anything yet. CelikPanel
  checks again by itself. When access is confirmed, this page continues where it
  was, with what you typed. Until then nothing on this page can be changed."
  (8 Ekim 2026'dan beri aralık söylenmez: metin "birkaç saniyede bir" diyordu;
  oysa lisans okuması 5 sn'de, oturum ve hazır olma okuması 10 sn'de bir
  yinelenir.) Eylem: mevcut `recovery.retry`, TR "Panel
  erişimini kontrol et" · EN "Check panel access" (sahibin istediği okuma
  sürerken `recovery.checking`, "Kontrol ediliyor…" / "Checking…"). Yalnızca
  okur; hiçbir şey başlatmaz.
- *30 sn sonra hâlâ bilinmiyor:* `accessHold.prolonged`, TR "Bu durum yarım
  dakikadan uzun sürdü. Beklemeyi sürdürebilirsiniz, otomatik kontrol devam eder;
  dilerseniz CelikPanel’i yeniden yükleyebilirsiniz. Yeniden yüklemek, bu sayfada
  yazıp kaydetmediğiniz her şeyi siler." · EN "This has taken longer than half a
  minute. You can keep waiting, and the automatic check continues, or you can
  reload CelikPanel. Reloading discards anything you typed on this page and did
  not save."; kontrolün yanında mevcut `app.reload`, TR "CelikPanel’i yeniden
  yükle" · EN "Reload CelikPanel".
- *Güncelleme ve kurtarma durumu* (`recovery.operationTitle`), katmanda ve erişim
  ile hazır olma sayfalarında yalnızca süren, başarısız olan, başarısızlıktan
  sonra geri alınan ya da okunamayan kayıtlı işlem için görünür. Doğrulanmış
  güncelleme ve kayıtlı işlemi olmayan tarayıcı orada hiçbir şey göstermez.
- *İlk yükleme ve girişten sonra:* oturum ve hazır olma okumaları sürerken sayfa
  `recovery.checkingTitle` ile `recovery.checkingHelp` gösterir (TR "Oturumunuz
  ve panelin hazır olma durumu doğrulanıyor. Bu kontrol sunucuda bir işlem
  başlatmaz." · EN "Confirming your session and panel readiness. This check does
  not start a server operation."). `recovery.availabilityTitle` ("Panelin hazır
  olma durumu kontrol edilemedi" / "Panel readiness could not be checked") için
  başarısız olmuş bir okuma gerekir. Bu sayfalar kendiliğinden yeniden okur
  (oturum ve hazır olma 10 sn'de, lisans sonucu 5 sn'de bir) ve artık sahibinden
  yalnızca yeniden kontrol etmesini istemek yerine bunu söyler (8 Ekim 2026):
  - `recovery.authHelp` — TR "CelikPanel şu anda oturumunuzu doğrulayamıyor. Bu
    sayfa kendiliğinden yeniden kontrol eder; dilerseniz şimdi de kontrol
    edebilirsiniz. Oturumunuz ve panel erişimi doğrulanana kadar yönetim kapalı
    kalır." · EN "CelikPanel cannot confirm your session right now. This page
    checks again by itself; you can also check now. Management stays closed
    until your session and panel access are verified."
  - `recovery.availabilityHelp` — TR "Oturumunuz doğrulandı ancak panelin hazır
    olup olmadığı bilinmiyor. Bu sayfa kendiliğinden yeniden kontrol eder ve
    sunucu hazır olduğunu ve erişimi doğruladığında açılır. Dilerseniz şimdi
    kontrol edebilir veya sayfayı yenileyebilirsiniz." · EN "Your session was
    verified, but panel readiness is unknown. This page checks again by itself
    and opens once the server confirms readiness and access. You can also check
    now or reload the page."
  - `recovery.licenseHelp` — TR "Lisans sonucu alınamıyor. Bu, lisansınızın
    eksik veya süresi dolmuş olduğunu göstermez. Bu sayfa kendiliğinden yeniden
    kontrol eder; sunucu erişimi doğruladığında yönetim açılır. Dilerseniz şimdi
    de kontrol edebilirsiniz." · EN "The license result is unavailable. This
    does not establish that your license is missing or expired. This page checks
    again by itself, and management opens once the server confirms access. You
    can also check now."
- *Kullanılan sayfanın altında oturum sona erdi* (doğrulanmış 401), giriş
  formunun üstünde: `accessHold.sessionEnded`, TR "Oturumunuz sona erdi.
  Bulunduğunuz sayfaya dönmek için giriş yapın. Orada yazıp kaydetmediğiniz
  bilgiler korunmadı." · EN "Your session ended. Sign in to return to the page
  you were on. Anything you had typed there and not saved was not kept." Kim
  işlem yapar: sahip. Devam: girişten sonra aynı adres. Çıkış yapıldığında neden
  gösterilmez.
- *Güncellemeden sonra arayüzün bir parçası yüklenemedi*, sayfa yeniden
  yüklenmeden önce 7 sn boyunca, her şeyin üstünde duran ortak pencere olarak
  (8 Ekim 2026 düzeltmesi: 4 sn boyunca üstte tek satırdı; telefonda açık bir
  pencerenin başlığını örtüyor ve kaydedilmemiş girdinin kaybolacağını
  söylemiyordu): `accessHold.updateReloadTitle`, TR "Bu sayfa birazdan yeniden
  yüklenecek" · EN "This page is about to reload"; `accessHold.updateReload`, TR
  "CelikPanel’in bir bölümü yüklenemedi; büyük olasılıkla bu sekme açıkken
  CelikPanel güncellendi. Güncel sürümü yüklemek için bu sayfa birazdan yeniden
  yüklenir. Bu sayfada yazıp kaydetmediğiniz her şey kaybolur." · EN "A part of
  CelikPanel could not be loaded, most likely because CelikPanel was updated
  while this tab was open. This page reloads in a moment to load the current
  version. Anything you typed on this page and did not save is lost." Eylem:
  hemen yeniden yüklemek için mevcut `app.reload`. Altındaki sayfa artık
  kullanılamaz. Neden, bilinen olarak değil olası olarak belirtilir.

**Bilinen olumsuz sonuçlar değişmedi:** eksik, süresi dolmuş ya da geçersiz
olduğu bildirilen lisans yine etkinleştirme sayfasını (yönetici) ya da yöneticiye
başvurma iletisini (diğer roller) gösterir.

**Ele alınmayan.**

- Metin parçası gelmemişse katman yalnızca "Panel erişimi kontrol ediliyor"
  başlığını, kabuğun yardım satırını ve kontrolü gösterir; sona eren oturumun
  nedeni ve yeniden yükleme satırı o durumda gösterilmez.
- Yüklenemeyen parçadan sonraki yeniden yükleme reddedilemez: duyurulur ve
  yalnızca öne alınabilir.
- Katmanın altında kendi isteği reddedilen sayfa bunu kendisi ele alır; sayfa
  sayfa incelenmedi.
- Gönderilmemiş girdi gerçek bir yeniden girişte korunmaz.
- Gerçek sunucuda doğrulanmadı. Tek bir tarayıcıda, taklit sunucuya karşı
  görüldü: metinlerin yerinde görünümü, 1,5 sn ve 30 sn adımları, odağın dönüşü
  ve gizli sekmenin zamanlayıcıları (aşağıya bakın).
- Katman ortalanır; bu yüzden kendi kutusu, altında açık duran bir pencerenin
  ortasını hâlâ örter. Çevresi loş ve okunur kalır.

**Tarayıcı incelemesi (8 Ekim 2026).** Bu tarihin iki değişikliği de gerçek,
kurulu bir Chrome'da, Panel API'sinin yerel adresteki bir taklidine karşı
incelendi (`web/tools/browser-inspect`): kurulu sunucu, lisans hizmeti, gerçek
sertifika ve gerçek yeniden başlatma yok. `8a65d4ca` üzerindeki ilk geçiş, hiçbir
bileşen testinin göstermediği sekiz kusur buldu; bunlar aynı değişiklikte
düzeltildi ve geçiş düzeltilmiş kaynakta yinelendi.

- *Bulunan ve düzeltilen.* Planlı yeniden başlatma bildirimi "Sizin bir şey
  yapmanız gerekmez" deyip ardından sahibine başka bir adresi açmasını
  söylüyordu; inceleme sayfasında görünür alanın altındaydı. Güvenli adres
  sözcüğün içinden, telefonda `https://` içinden bölünüyordu; artık tek
  parçadır ve noktadan ya da porttan önce bölünür. Sunucu meşgul olduğu için
  reddedilen adım nedenini üçüncü sırada, neredeyse aynı iki başlığın ve iki
  genel paragrafın altında, hata renginde gösteriyor, eylemi adım listesinin
  altında kalıyordu. Bekletme katmanının başlığı bir denetim gibi odak halkası
  çiziyordu. "Kontrol ediliyor" katmanında iki çizgi arasında boş bir şerit
  vardı. Açık bir pencerenin üstündeki katman ikinci bir karartma ekliyor,
  korunduğunu söylediği sayfa okunamıyordu; katman ya da yeniden yükleme
  penceresi çizilirken artık tek karartma odur; bileşen işlemi katmanının ve
  güncelleme kilidinin üstünde çizilir ve klavye odağını ikisine karşı da
  tutar. Etkinleştirme sayfası, lisans kararının yanında "Güncelleme ve kurtarma
  durumu — Bu tarayıcıda kayıtlı güncelleme işlem kimliği yok…" gösteriyordu;
  diğer kapılar gibi artık o bölümü yalnızca bitmemiş bir işlem için çizer.
- *Ayrıca düzeltilen, bugünkü sunucuyla ulaşılamayan.* Erişim yolunun kendi
  verdiği kodlu ret artık kapıdan o yolu yeniden okumasını istemez; erişim
  bilinmezken reddedilen bir istek, 5 sn'lik yeniden kontrolden daha sık okuma
  başlatamaz. Erişim yolunda yapay bir 503 ile tarayıcı o yolu 28,7 sn'de
  10.581 kez okumuştu; sunucu o yolu 200 ve tipli bir gövdeyle yanıtlar.
- *Kapsanan.* Masaüstü 1440×900 ve telefon 390×844, Türkçe ve İngilizce, açık
  ve koyu: bildirimle inceleme ve ilerleme, yeniden başlatma ve devamı; başka
  bir adımda kopan bağlantı; her tipli neden için ve nedensiz meşgul sunucu;
  başarısız kurulum, başarısız güvenlik duvarı adımı, karşılanmamış DNS
  önkoşulu, lisans beklemesi, süren kurulum ve doğrulanan sonuç; yazılmış girdisi
  olan bir pencerenin üstünde bekletme (sessiz dönüş, başarısız okuma, fare ve
  klavye, 30 sn sonra yeniden yükleme önerisi, yavaş okuma, kopan bağlantı,
  reddedilen istek), bileşen işlemi katmanının üstünde ve güncelleme kilidiyle
  birlikte bekletme; yeniden yükleme penceresi; adresi gösteren dört yerin
  hepsinde uzun bir alan adı; bilinen olumsuz kapı; yavaş ve başarısız
  okumalarla ilk yükleme; sona eren oturum.
- *Kapsanmayan.* Gerçek sunucu, sertifika ya da yeniden başlatma; Safari,
  Firefox, ekran okuyucu, dokunmatik cihaz; taklit temalar; yönetici dışındaki
  roller; katmanın altında Alan Adları dışındaki sayfalar. Bekletme başladığında
  güncelleme kilidi bırakılır (izleyici eskisi gibi duraklar); bu yüzden katman
  kilitle birlikte yalnızca kilit ayrılırken görüldü. Koyu temada tek
  karartmanın altındaki sayfa loştur: başlıklar ve denetimler okunur, küçük
  soluk metin ancak dikkatle.

### Okunamayan ya da sayfa yüklendikten sonra değişen geçerli ayarlar (2026-10-08)

Bileşen testleriyle kaynak durumu; gerçek sistem denemesi yok. Aynı tarihli
dayanıklılık sözleşmesi girdisine bakın. Üç ekran: sunucu posta politikası
(Postfix sayfası), bir alan adının otomatik yedekleri ve bir alan adının
zamanlanmış görevleri. Bundan önce başarısız bir okuma varsayılanları ya da
"Zamanlanmış görev yok" yazısını gösteriyor, tek bir Kaydet ya da eklenen tek bir
görev sahibin gerçekte sahip olduğunun yerine geçiyordu.

Bu ekranlardaki kural: sunucunun geçerli durumu bilinene kadar hiçbir şey ayar,
"kapalı" durumu ya da boş liste olarak gösterilmez ve hiçbir kayıt
varsayılanlardan kurulmaz. Aşağıdaki her ret herhangi bir değişiklikten önce
gelir.

- **Okunuyor (bekleme; kimse işlem yapmaz).** Formun ya da listenin yerinde:
  - EN: "Reading the current settings from the server…"
  - TR: "Geçerli ayarlar sunucudan okunuyor…"
- **Okunamadı (bilinmeyen sonuç).** Form yok, liste yok, Kaydet yok. Bildirim
  ekranı adlandırır, hiçbir şeyin değiştirilmediğini söyler ve yeniden okuyan,
  hiçbir şeyi değiştirmeyen **Retry** / **Tekrar dene** eylemini sunar.
  - Posta politikası, EN: "The current mail policy could not be read from the
    server, so no settings are shown and nothing can be saved here. Nothing was
    changed. Try again; if it keeps failing, check on the server that Postfix is
    running (sudo systemctl status postfix)."
  - Posta politikası, TR: "Geçerli posta politikası sunucudan okunamadı; bu
    yüzden ayarlar gösterilmiyor ve buradan kayıt yapılamıyor. Hiçbir şey
    değiştirilmedi. Tekrar deneyin; sorun sürerse sunucuda Postfix’in çalıştığını
    denetleyin (sudo systemctl status postfix)."
  - Otomatik yedekler, EN: "The automatic backup settings for this domain could
    not be loaded, so they are not shown and cannot be changed here. Nothing was
    changed; an existing schedule stays as it is. Try again."
  - Otomatik yedekler, TR: "Bu domain'in otomatik yedek ayarları yüklenemedi; bu
    yüzden gösterilmiyor ve buradan değiştirilemiyor. Hiçbir şey değiştirilmedi;
    var olan bir zamanlama olduğu gibi duruyor. Tekrar deneyin."
  - Zamanlanmış görevler, EN: "The scheduled tasks of this domain could not be
    read from the server, so the list is not shown and tasks cannot be added or
    changed here. Nothing was changed; the tasks already on the server are
    untouched. Try again."
  - Zamanlanmış görevler, TR: "Bu domain'in zamanlanmış görevleri sunucudan
    okunamadı; bu yüzden liste gösterilmiyor ve buradan görev eklenemiyor ya da
    değiştirilemiyor. Hiçbir şey değiştirilmedi; sunucudaki görevlere dokunulmadı.
    Tekrar deneyin."
  - Cron eksikse bu bildirim değil, kendi doğrulanmış yanıtı gösterilir
    (`CRON_NOT_INSTALLED`, 2026-10-01 girdisi).
- **Sayfa yüklendikten sonra sunucuda değişti (doğrulanmış ret,
  `409 SETTINGS_CHANGED`; sürümsüz bir kayıt, `409 SETTINGS_VERSION_REQUIRED`,
  aynı biçimde gösterilir).** Bildirim formun üstünde ekranda kalır, yazılan
  görünür kalır, Kaydet devre dışıdır ve tek eylem yeniden yükler: **Reload
  current settings** / **Geçerli ayarları yeniden yükle** (zamanlanmış görevlerde
  **Reload the list** / **Listeyi yeniden yükle**). Kim işlem yapar: ekranın
  başındaki kişi. Hiçbir şey kendiliğinden yeniden denenmez.
  - Posta politikası, EN: "The mail policy changed on the server after this page
    loaded, so nothing was saved. What you entered is still shown below. Reload
    the current settings, then make your change again."
  - Posta politikası, TR: "Posta politikası bu sayfa yüklendikten sonra sunucuda
    değişti; bu yüzden hiçbir şey kaydedilmedi. Girdikleriniz aşağıda duruyor.
    Geçerli ayarları yeniden yükleyin, sonra değişikliğinizi tekrar yapın."
  - Otomatik yedekler, EN: "The automatic backup settings changed on the server
    after this page loaded, so nothing was saved. What you chose is still shown
    below. Reload the current settings, then make your change again."
  - Otomatik yedekler, TR: "Otomatik yedek ayarları bu sayfa yüklendikten sonra
    sunucuda değişti; bu yüzden hiçbir şey kaydedilmedi. Seçtikleriniz aşağıda
    duruyor. Geçerli ayarları yeniden yükleyin, sonra değişikliğinizi tekrar
    yapın."
  - Zamanlanmış görevler, EN: "The scheduled tasks changed on the server after
    this list loaded, so nothing was changed. Reload the list, then try again; a
    task you were typing stays in the form."
  - Zamanlanmış görevler, TR: "Zamanlanmış görevler bu liste yüklendikten sonra
    sunucuda değişti; bu yüzden hiçbir şey değiştirilmedi. Listeyi yeniden
    yükleyin, sonra tekrar deneyin; yazmakta olduğunuz görev formda kalır."
- **Panel'in yeniden yazmayacağı DNSBL (eksik önkoşul; yüklemede gösterilir).**
  DNSBL denetimlerinin yerini gerekçe ve sahibin eylemi alır; ileti boyutu ve hız
  sınırı düzenlenebilir kalır. Kim işlem yapar: sunucu sahibi, `main.cf` içinde.
  - Başka bir ayara başvuruyor, EN: "DNSBL cannot be changed from this page: the
    recipient restrictions in Postfix refer to another setting ($name), so
    CelikPanel cannot tell which checks they contain and will not rewrite them."
    TR: "DNSBL bu sayfadan değiştirilemiyor: Postfix’teki alıcı kısıtları başka
    bir ayara ($ad) başvuruyor; CelikPanel hangi denetimleri içerdiklerini
    bilemediği için onları yeniden yazmaz."
  - Kesin olarak okunamıyor, EN: "DNSBL cannot be changed from this page:
    CelikPanel could not read the recipient restrictions in Postfix with
    certainty (an unclosed brace, or reject_rbl_client without a zone) and will
    not rewrite them." TR: "DNSBL bu sayfadan değiştirilemiyor: CelikPanel,
    Postfix’teki alıcı kısıtlarını kesin olarak okuyamadı (kapanmamış bir süslü
    ayraç ya da bölgesi olmayan bir reject_rbl_client) ve onları yeniden yazmaz."
  - İki permit girdisi olmadan elle yazılmış, EN: "DNSBL cannot be changed from
    this page: the recipient restrictions in Postfix were written by hand
    without both permit_mynetworks and permit_sasl_authenticated, so a DNSBL
    check placed by CelikPanel could reject this server’s own users." TR: "DNSBL
    bu sayfadan değiştirilemiyor: Postfix’teki alıcı kısıtları elle yazılmış ve
    permit_mynetworks ile permit_sasl_authenticated girdilerinin ikisini birden
    içermiyor; CelikPanel’in koyacağı bir DNSBL denetimi bu sunucunun kendi
    kullanıcılarını reddedebilir."
  - Son bir eylemle bitiyor, EN: "DNSBL cannot be changed from this page: the
    recipient restrictions in Postfix end with permit, reject or defer, so a
    DNSBL check added after them would never run, and where it belongs is your
    decision." TR: "DNSBL bu sayfadan değiştirilemiyor: Postfix’teki alıcı
    kısıtları permit, reject ya da defer ile bitiyor; arkasına eklenen bir DNSBL
    denetimi hiç çalışmaz ve nereye konacağı sizin kararınızdır."
  - Bu ekranın sözü olmayan bir gerekçe, EN: "DNSBL cannot be changed from this
    page: CelikPanel will not rewrite the recipient restrictions it found in
    Postfix." TR: "DNSBL bu sayfadan değiştirilemiyor: CelikPanel, Postfix’te
    bulduğu alıcı kısıtlarını yeniden yazmaz."
  - Eylem, EN: "To change it, edit the reject_rbl_client entries of
    smtpd_recipient_restrictions in /etc/postfix/main.cf, run sudo systemctl
    reload postfix, then reload this page. Message size and the rate limit can
    still be saved here."
  - Eylem, TR: "Değiştirmek için /etc/postfix/main.cf içindeki
    smtpd_recipient_restrictions değerinin reject_rbl_client girdilerini
    düzenleyin, sudo systemctl reload postfix komutunu çalıştırın, sonra bu
    sayfayı yenileyin. İleti boyutu ve hız sınırı buradan kaydedilebilir."
- **Diğer retler (doğrulanmış; hata iletisi olarak gösterilir, form kalır).**
  - Yinelenen görev (`409 CRON_JOB_DUPLICATE`), EN: "A task with the same
    schedule and command already exists, so nothing was added. Change the
    existing task instead, or enable it if it is disabled." TR: "Aynı zamanlama
    ve komutla bir görev zaten var; bu yüzden hiçbir şey eklenmedi. Var olan
    görevi değiştirin ya da devre dışıysa etkinleştirin."
  - Düz alan adı olmayan bir bölge (`400 MAIL_POLICY_INVALID`, `dnsbl_zone`),
    EN: "Nothing was saved: a DNSBL zone must be a plain host name such as
    zen.spamhaus.org, with zones separated by commas. Correct the zones and save
    again." TR: "Hiçbir şey kaydedilmedi: DNSBL bölgesi zen.spamhaus.org gibi düz
    bir alan adı olmalı ve bölgeler virgülle ayrılmalıdır. Bölgeleri düzeltip
    yeniden kaydedin."
  - Kayıt anında durum okunamadı (`502 CURRENT_SETTINGS_UNREADABLE`), EN:
    "CelikPanel could not read what is currently set on the server, so nothing
    was changed. Reload the page and try again." TR: "CelikPanel sunucuda şu an
    neyin ayarlı olduğunu okuyamadı; bu yüzden hiçbir şey değiştirilmedi. Sayfayı
    yenileyip tekrar deneyin."

API yanıtları, API'yi doğrudan okuyanlar için aynı durumların İngilizce cümlesini
taşır; Agent'ın ve `postconf`'un kendi satırları günlüklerde kalır.

Yapılmayan: kaydedilen bir politikadan sonra başarısız olan Postfix yeniden
yüklemesi ve başarısız bir `postconf` yazısı sınıflandırılmadı (ikincisi
`INTERNAL` yedeğidir); bilinmeyen bildirimi okumanın neden başarısız olduğunu
söylemez; API'den gelen DNSBL reddinin (`MAIL_POLICY_RESTRICTIONS_UNMANAGED`)
yalnız İngilizce cümlesi vardır, çünkü ekran bu retle karşılaşmak yerine denetimi
geri çeker; diğer ekranlar hâlâ eski kalıbı izler ve burada incelenmedi.

### Bilinmeden olumsuz durum yok: kontrol ediliyor, kontrol edilemedi, biliniyor (2026-10-09)

Bileşen testleriyle ve yerel bir sahte sunucuya karşı tarayıcı incelemesiyle
(bu girdinin sonuna bakın) kaynak durumu; gerçek sistem denemesi ve kurulu
sunucu yok. 2026-10-08'de kurulu bir sunucuda bir sahip bildirdi. Yukarıdaki
"Doğruluk ve işlem kimliği" kuralını (bilinmeyen sonuç, eksik önkoşul değildir)
sunucuyu okuyan her ekrana uygular. Okumaların nasıl gösterildiğini değiştirir;
yaşam döngüsü, erişim kapısı, saklanan kayıt ya da API değişmez ve hiçbir kabul
işi kapanmaz.

**Sahibin gördüğü.** DNS'i olan bir sunucuda "Alan adı ekle" açıldığında,
sunucunun yeteneklerinin okunması süren saniyeler boyunca "Alan adı eklemeden
önce panel tarafından yönetilen etkin bir DNS sunucusu gerekir… [DNS motoru seç]"
yazısı ve kapalı bir form göründü. Altındaki Alan Adları sayfası da aynı
saniyelerde "Alan adı ekle" düğmesini kapattı. Eksik bir şey yoktu; yanıt henüz
gelmemişti.

**Neden.** Sunucuyu okumanın ortak bir yolu yoktu. 68 dosya onu ham `fetch` ile
bileşen durumuna okuyor, çoğu "okuma başarısız oldu" ya da "okuma henüz yanıt
vermedi" durumunu ekranın sonra olgu diye gösterdiği bir değere (`null`, boş
liste, `false`) çeviriyordu. Aynı adres, `GET /api/v1/hosting/capabilities`,
sekiz yerde ve "henüz yanıt yok" için sekiz ayrı anlamla okunuyordu.

**Bundan sonra her ekran için kural: bilinmeden olumsuz arayüz yok.** Bir ekranın
sunucudan okuduğu şey her zaman üç durumdan birindedir ve bunlar birbirine
benzemez:

1. **Kontrol ediliyor** (henüz bilinmiyor; kimsenin bir şey yapması gerekmez).
   Olağan metin renginde, yanıt geldiğinde ekranın geri kalanını yerinden
   oynatmayan bir yerde, sakin tek satır. "Eksik", "hazır değil", "yok", "kapalı"
   ve boş liste yok.
2. **Kontrol edilemedi** (bilinmeyen sonuç; ekranın başındaki kişi işlem yapar).
   Ekranın kendi cümlesi neyin okunamadığını, bunun o şeyin eksik olduğu anlamına
   gelmediğini ve hiçbir şeyin değiştirilmediğini söyler; **Tekrar dene** /
   **Retry** yeniden okur ve hiçbir şeyi değiştirmez. Önceki bir yanıt varsa,
   bunun önceki yanıt olduğunu ve ne zaman okunduğunu söyleyen bir bildirimin
   altında ekranda kalır.
3. **Biliniyor.** Ancak o zaman "eksik", "hazır değil", "boş" ya da "kapalı", o
   durumun zaten sahip olduğu yönlendirmeyle.

Bir şey gönderen ya da kaldıran denetim, yalnız üzerinde işlem yaptığı şey
bilinirken etkindir. Hiçbir form varsayılanlardan kurulmaz ya da kaydedilmez.
Okuma hiçbir zaman değişiklik olarak yinelenmez: Tekrar dene, "Tekrar kontrol et"
ve yenileme yalnız okur.

**Metinler.** Aksi belirtilmedikçe anahtarlar `web/src/i18n/screens` içindedir.

- *Başarısız bir yenilemeden sonra önceki yanıt hâlâ gösteriliyor* (kabuk,
  `common.staleNotice`; `{time}` okunduğu saat ve dakikadır):
  - TR: "Bu, az önce sunucudan yeniden okunamadı; aşağıda gösterilen, saat {time}
    itibarıyla olan hâlidir. Hiçbir şey değiştirilmedi. Bir şeyi kaldıran ya da
    değiştiren denetimler, yeniden okunana dek kapalıdır."
  - EN: "This could not be read again from the server just now, so what is shown
    below is as it was at {time}. Nothing was changed. Controls that remove or
    change something are off until it has been read again."
- *Alan adı ekle penceresi ve bir alan adının DNS kayıtları sekmesi* — kontrol
  ediliyor (`dns.checkingServer`): TR "Bu sunucunun DNS durumu kontrol ediliyor…"
  · EN "Checking this server’s DNS…". Pencerede form gösterilir ve
  doldurulabilir, "Alan adı oluştur" kapalıdır ve hiçbir engel çizilmez.
- *Alan adı ekle penceresi* — kontrol edilemedi (`domains.add.dnsUnknown`),
  Tekrar dene ile ve "DNS motoru seç" olmadan:
  - TR: "CelikPanel bu sunucunun DNS durumunu kontrol edemedi; bu yüzden şimdilik
    alan adı eklenemiyor. Bu, DNS’in eksik olduğu anlamına gelmez. Hiçbir şey
    değiştirilmedi ve yazdıklarınız duruyor. Tekrar deneyin."
  - EN: "CelikPanel could not check DNS on this server, so a domain cannot be
    added yet. This does not mean DNS is missing. Nothing was changed and what
    you typed is kept. Try again."
- *Alan adı ekle penceresi ve Alan Adları sayfası* — bilinen olumsuz: değişmedi.
  Etkin motor yok: `err.DNS_SERVER_REQUIRED.action` ile `domains.add.needsDns`;
  kimliği olmayan motor: eylemiyle `err.DNS_SETTINGS_REQUIRED`. DNS hazırlığı
  kontrol edilirken ya da edilemediğinde Alan Adları sayfası DNS hakkında hiçbir
  şey söylemez ve "Alan adı ekle" kullanılabilir kalır; açtığı pencere yukarıdaki
  kontrol satırını ya da bildirimi gösterir.
- *Alan Adları sayfası, liste* — kontrol ediliyor (`domains.checking`): TR "Alan
  adı listesi okunuyor…" · EN "Reading the domain list…". Okunamadı
  (`domains.unknown`):
  - TR: "Alan adı listesi sunucudan okunamadı; bu yüzden gösterilmiyor. Bu, alan
    adı olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - EN: "The domain list could not be read from the server, so it is not shown.
    This does not mean there are no domains. Nothing was changed. Try again."
  - "Henüz alan adı yok" / "No domains yet" (`domains.empty`) yalnız sunucunun
    satırsız yanıtladığı liste için.
- *Alan Adları sayfası, beklemede listelenen ve kayıtlı silmesi okunamayan satır*
  (`domains.pendingUnknown`; bundan önce satır sessizce bildirimsiz kalıyordu):
  - TR: "{name} beklemede görünüyor, ancak CelikPanel onun için bekleyen bir silme
    olup olmadığını okuyamadı. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - EN: "{name} is listed as pending, but CelikPanel could not read whether a
    deletion is waiting for it. Nothing was changed. Try again."
- *Alan Adları sayfası, abonelik kullanımı* (`quota.unknown`): TR "Abonelik
  kullanımı sunucudan okunamadı; bu yüzden gösterilmiyor. Hiçbir şey
  değiştirilmedi. Tekrar deneyin." · EN "Subscription usage could not be read
  from the server, so it is not shown. Nothing was changed. Try again."
- *Veritabanları sayfası, motorlar* — kontrol ediliyor
  (`databases.checkingServers`): TR "Bu sunucudaki veritabanı motorları
  okunuyor…" · EN "Reading the database engines on this server…". Okunamadı
  (`databases.serversUnknown`):
  - TR: "Bu sunucudaki veritabanı motorları okunamadı; bu yüzden hiçbir şey
    listelenmiyor. Bu, kurulu motor olmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
  - EN: "The database engines on this server could not be read, so nothing is
    listed. This does not mean no engine is installed. Nothing was changed. Try
    again."
  - "Kurulu veritabanı motoru yok" ve "Servisler sayfasına git" yalnız bilinen
    boş yanıt için.
- *Veritabanları sayfası, bir motorun veritabanları ve kullanıcıları* — kontrol
  ediliyor (`databases.checkingDatabases`, `databases.checkingUsers`): TR "Bu
  motordaki veritabanları okunuyor…", "Bu motordaki veritabanı kullanıcıları
  okunuyor…" · EN "Reading the databases on this engine…", "Reading the database
  users on this engine…". Okunamadı (`databases.databasesUnknown`,
  `databases.usersUnknown`):
  - TR: "Bu motordaki veritabanları okunamadı; bu yüzden liste gösterilmiyor. Bu,
    veritabanı olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
    deneyin." / "Bu motordaki veritabanı kullanıcıları okunamadı; bu yüzden liste
    gösterilmiyor. Bu, kullanıcı olmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
  - EN: "The databases on this engine could not be read, so the list is not
    shown. This does not mean there are none. Nothing was changed. Try again." /
    "The database users on this engine could not be read, so the list is not
    shown. This does not mean there are none. Nothing was changed. Try again."
  - Her sekmenin yanındaki sayı, liste bilindiğinde sayıdır; okunurken "…",
    okunamadığında "–". "Veritabanı oluştur" iki listenin de bilinmesini ister,
    çünkü penceresi var olan kullanıcıları sunar.
- *Bir alan adının Veritabanları sekmesi* — kontrol ediliyor (`db.checking`): TR
  "Bu alan adının veritabanları okunuyor…" · EN "Reading this domain’s
  databases…". Okunamadı (`db.unknown`):
  - TR: "Bu alan adının veritabanları sunucudan okunamadı; bu yüzden liste
    gösterilmiyor. Bu, veritabanı olmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
  - EN: "The databases of this domain could not be read from the server, so the
    list is not shown. This does not mean there are none. Nothing was changed.
    Try again."
  - Motorlar — kontrol ediliyor (`db.checkingEngines`): TR "Kurulu veritabanı
    motorları kontrol ediliyor…" · EN "Checking which database engines are
    installed…". Kontrol edilemedi (`db.enginesUnknown`): TR "CelikPanel bu
    sunucuda hangi veritabanı motorlarının kurulu olduğunu kontrol edemedi; bu
    yüzden şimdilik burada veritabanı oluşturulamıyor. Hiçbir şey değiştirilmedi.
    Tekrar deneyin." · EN "CelikPanel could not check which database engines are
    installed on this server, so a database cannot be created here yet. Nothing
    was changed. Try again." Varsayılan motor yoktur: oluşturma formu yalnız
    sunucunun adını verdiği bir motor için vardır.
- *Bir alan adının bağlantı kartı* — kontrol ediliyor (`conn.checking`): TR "Bu
  alan adının nereyi gösterdiği kontrol ediliyor…" · EN "Checking where this
  domain points…". Kartın kendi okuması başarısız (`conn.readFailed`; bundan önce
  kart kayboluyordu):
  - TR: "Bu alan adının bağlantısı kontrol edilemedi: CelikPanel bu sunucudan
    yanıt alamadı. Bu, alan adının bağlı olmadığı anlamına gelmez. Tekrar
    deneyin."
  - EN: "The connection of this domain could not be checked: CelikPanel did not
    get an answer from this server. This does not mean the domain is not
    connected. Try again."
  - Okuma başarılı ve sunucu genel çözümleyicilere soramadığını söylüyor
    (`status: unknown`): durum satırı, olağan metin renginde, var olan
    `conn.status.unknown` ("Genel DNS kontrol edilemedi" / "Public DNS could not
    be checked"); iki "şu an" kutusu "henüz yok" yerine `conn.notChecked` der, TR
    "kontrol edilemedi" · EN "could not be checked"; "Bu alan adı henüz bu
    sunucuyu göstermiyor…" yerine de `conn.unknownHelp`: TR "Bu sunucu az önce
    genel DNS çözümleyicilerine ulaşamadı; bu yüzden CelikPanel bu alan adının
    nereyi gösterdiğini bilmiyor. Bu, alan adının bağlı olmadığı anlamına gelmez.
    Biraz sonra tekrar kontrol edin. Alan adı henüz bağlı değilse, alan adını
    aldığınız firmada girilecek değerler aşağıdadır." · EN "This server could not
    reach the public DNS resolvers just now, so CelikPanel does not know where
    this domain points. This does not mean it is not connected. Check again in a
    moment. If the domain is not connected yet, the values to enter at the
    company you bought it from are below."
  - Aynı yanıtta ad sunucusu adları da doğrulanamadıysa, bozuk denmez ve onlara
    devir sunulmaz: `conn.routeAUnknown`, TR "Bu sunucunun ad sunucusu adlarının
    yanıt verip vermediği de kontrol edilemedi; bu yüzden DNS’i onlara devretmek
    şu anda sunulmuyor. Tekrar kontrol edin." · EN "Whether this server’s
    nameserver names answer could not be checked either, so handing DNS to them
    is not offered right now. Check again."
  - Sertifika satırı: `conn.sslUnknown`, TR "Sertifika alınıp alınamayacağı, genel
    DNS kontrol edilene dek bilinmiyor." · EN "Whether a certificate can be
    issued is not known until public DNS can be checked." Üç durumda da eylem:
    var olan "Tekrar kontrol et" / "Check again"; okur.
- *Bir alan adının PHP ayarları, kurulu sürümler* — kontrol ediliyor
  (`php.checkingVersions`): TR "Kurulu PHP sürümleri kontrol ediliyor…" · EN
  "Checking which PHP versions are installed…". Kontrol edilemedi
  (`php.versionsUnknown`): TR "Kurulu PHP sürümleri kontrol edilemedi; bu yüzden
  yalnız geçerli sürüm listeleniyor ve sürüm şimdilik buradan değiştirilemiyor.
  Hiçbir şey değiştirilmedi. Tekrar deneyin." · EN "The installed PHP versions
  could not be checked, so only the current version is listed and the version
  cannot be changed here yet. Nothing was changed. Try again."
- *Bir alan adının barındırma türü* — "PHP-FPM kurulu değil" yalnız bilinen yanıt
  için; kontrol edilemedi (`hosting.phpUnknown`): TR "CelikPanel bu sunucuda
  PHP-FPM’in kurulu olup olmadığını kontrol edemedi; bu yüzden PHP türü
  kullanılabilir ya da kullanılamaz diye işaretlenmedi. Hiçbir şey
  değiştirilmedi. PHP-FPM kurulu değilse PHP türünü uygulamak reddedilir. Tekrar
  deneyin." · EN "CelikPanel could not check whether PHP-FPM is installed on this
  server, so the PHP type is not marked either way. Nothing was changed. If
  PHP-FPM is not installed, applying the PHP type is refused. Try again."
- *Bir alan adının sekmeleri*: Posta ve Veritabanları yalnız sunucuda o hizmetin
  olmadığı bilindiğinde kaldırılır; bu kontrol edilirken ya da edilemediğinde
  kalırlar ve her birinin altındaki bölüm kendi durumunu söyler.
- 2026-10-08'in üç ayar ekranı (posta politikası, otomatik yedekler, zamanlanmış
  görevler) metinlerini korur; artık aynı katmanın üzerinde dururlar.

**Aynı ekranlarda düzeltilen iki kusur.**

- *Veritabanları sayfası.* "Hesabı kaldır"dan (ya da CelikPanel'in bir motordaki
  kendi hesabında yapılan herhangi bir değişiklikten) sonra şerit, sayfanın ilk
  yüklendiği satırı göstermeyi sürdürüyordu: kaldırılan hesap hâlâ "var"
  görünüyor, "Yeni parola" da onu yeniden oluşturuyordu. Şerit artık her zaman
  sunucunun son yanıtını gösterir; bu yanıt yeniden okunurken ya da okunamadığında
  denetimleri kapalıdır.
- *Bağlantı kartı.* `status: unknown` yanıtı `null` listeler taşır; bunları okumak
  alan adının bütün sayfasını düşürüyordu. Artık liste olarak okunurlar ve
  `unknown` artık "henüz burayı göstermiyor" diye okunmaz (yukarıda).

**Geri gelmesi nasıl engelleniyor.**

- `web/src/lib/remote.ts` okumanın tek yoludur: `useRemote(url, decode)`
  `loading | known(value, observedAt) | unknown(reason, previous?)` verir, aynı
  adresi okuyan ekrandaki her şey arasında tek isteği paylaşır ve hiçbir zaman
  varsayılan üretmez. `web/src/lib/hostingCapabilities.ts` yeteneklerin tek
  okuyucusudur. `Checking`, `CouldNotCheck`, `RemoteGate` ve `KnownEmpty`
  `web/src/components/ui.tsx` içindedir.
- *Mandal* (`web/tests/remote-state-ratchet.test.mjs`) dosya başına eski kalıpları
  sayar: değere çevrilen başarısız okuma (`x.ok ? … : null`), yutulan hata (bir
  okumanın çevresinde boş `catch {}`, `.catch(() => {})`), yok sayılan hata
  (else'siz `if (res.ok) {…}`, `if (!res.ok) return;`), `src/lib` dışında ham
  okuma `fetch(` ve kanıtlayan yanıt olmadan kullanılan `<EmptyState>`. Sayılar
  `web/tests/remote-state-ratchet.json` içindedir; yalnız azalabilirler, listede
  olmayan dosyada hiçbiri bulunamaz ve toplamlar testte sabitlenmiştir. Bu
  girdiden önce: 68 dosyada 46 / 68 / 16 / 126 / 32. Sonra: 62 dosyada
  37 / 58 / 14 / 109 / 29.
- *Bağlanan test* (`web/tests/remote-state-mounted.test.mjs`) taşınan her ekranı,
  her okuması alıkonmuş hâlde çizer — hâlâ yolda, kopmuş, reddedilmiş, sözleşme
  dışı — ve olumsuz metinlerden hiçbirinin çizilmemesini, gönderen ya da kaldıran
  hiçbir denetimin etkin olmamasını ister.

**Bir ekran nasıl taşınır.** `useRemote(url, decode)` ile okuyun; çözücü,
sözleşme olmayan yanıtta varsayılan doldurmak yerine hata fırlatır.
`<RemoteGate remote checking failed onRetry>` (çocukları yalnız sunucunun
gönderdiği değeri alır) ya da `remote.state` üzerinde açık bir ayrımla çizin.
Kontrol satırına, yanıt geldiğinde yüksekliği değişmeyen bir yer verin. "Boş"u
`<KnownEmpty of={…}>` ile çizin; bir gereksinimi, yalnız bilinen yanıt için
"engelli" diyebilen `gateOn` ile hesaplayın. Durum `known` değilken her kaydı ve
silmeyi kapatın. Ekranın kendi değişikliğinden sonra `retry()` çağırın. Kontrol
ve kontrol-edilemedi cümlelerini iki dilde ekleyin; ikincisi neyin okunamadığını,
bunun eksik olduğu anlamına gelmediğini ve hiçbir şeyin değiştirilmediğini
söyler. Sonra `web/` içinde `node tests/remote-state-ratchet.mjs --tighten`
çalıştırın ve ekranı bağlanan testin tablosuna ekleyin.

**Yapılmayan.**

- 62 dosya hâlâ eski yolla okuyor; izin listesi onlardır. Bir ekran taşınana dek
  bilinmeyen bir durum için olumsuz durum gösterebilir. (Aşağıdaki ikinci
  partiden sonra 53.)
- Aynı adreslerin başka okuyucularına dokunulmadı: gezinti rayı ve tek bir alan
  adının sayfası alan adı listesini kendi başlarına okur. (İkisi de aşağıdaki
  ikinci partide ortak okumaya taşındı.)
- Alan Adları sayfası, DNS hazırlığı kontrol edilemediğinde kendisi bir şey
  göstermez; bildirim ve Tekrar dene penceredir.
- Kontrol-edilemedi bildirimi okumanın neden başarısız olduğunu söylemez.
- Gerçek bir sunucuda doğrulanmadı; sahte sunucuya karşı tek bir Chrome.

**Tarayıcı incelemesi (2026-10-09).** Gerçek, kurulu bir Chrome'da, yerel sahte
sunucuya karşı (`web/tools/browser-inspect`, `adddomain`, `domainslist`,
`databases`, `connection` senaryoları): masaüstü 1440×900 ve telefon 390×844,
Türkçe ve İngilizce, açık ve koyu. Kapsanan: yetenekler yavaşken (okuma sırasında
dört kare), başarısızken ve sonra Tekrar dene ile, bilinen olumsuzken (motor yok,
kimlik yok ve pencerenin kendi engeli) ve bilinen olumluyken Alan Adları sayfası
ile Alan adı ekle penceresi, ayrıca yanıtı zaten olan bir sayfanın üstünde açılan
pencere; yavaş, başarısız sonra Tekrar dene ve bilinen boş alan adı listesi;
motorlar ve bir motorun listeleri yavaş, başarısız, boş ve doluyken Veritabanları
sayfası, bir silmeden sonra yeniden okunamayan liste ve kaldırıldıktan sonra panel
hesabı; yavaş, başarısız, her listesi `null` olan `status: unknown`, bilinen
olumsuz ve bilinen olumlu bağlantı kartı; yavaş, başarısız, boş ve dolu alan adı
veritabanı listesi.

- *Sekiz yapılandırmanın hepsinde ölçülen.* Yetenekler yoldayken alınan dört
  karenin her birinde pencere formu, kontrol satırını ve kapalı bir "Alan adı
  oluştur" düğmesini gösterdi; ad yazılabiliyor, iki amaç da seçilebiliyordu;
  hiçbir karede engel görünmedi. Yanıt geldiğinde pencerede hiçbir şey yer ya da
  boyut değiştirmedi. Sayfa ile pencere birlikte tek istek yaptı; yanıtı olan bir
  sayfanın üstünde açılan pencere hiç istek yapmadı ve "Alan adı oluştur" etkin
  hâlde başladı. Yazılan, Tekrar dene boyunca korundu. Bir liste yeniden
  okunamadıktan sonra iki silme denetimi de kapalıydı.
- *Bakarak bulunan ve düzeltilen.* Bağlantı kartı kontrol ederken tek satırlık
  bir şeritti ve sonra Genel Bakış'ın geri kalanını 186 px (telefonda 83 px)
  aşağı itiyordu; artık her durumda tek bir en az yüksekliği korur ve hiçbir
  yapılandırmada altındaki hiçbir şey yerinden oynamadı. "kontrol edilemedi"
  harfi değerlerin yazı yüzüyle duruyordu; sözdür ve metin yazı yüzüyle dizilir.
  Bir alan adının Veritabanları sekmesindeki "Create Database", koyu temanın açık
  birincil rengi üstünde beyazdı; artık ortak birincil düğmedir.
- *Görülen ve burada değiştirilmeyen.* Telefonda Alan Adları ve Veritabanları
  tabloları yana kayar; bu yüzden silme denetimi, tablo kaydırılana dek ekran
  dışındadır. Alan adı listesinin başarısız bir okuması, gezinti rayının kendi
  isteğini açık bırakır (ray taşınmadı). Kapalı birincil düğme koyu temada etkin
  olana yakındır.
- *Kapsanmayan.* Herhangi bir gerçek sunucu; Safari, Firefox, ekran okuyucu,
  dokunmatik aygıt; taklit görünümler; yönetici dışındaki roller. DNS kayıtları
  sekmesi, PHP ayarları ve barındırma türü tarayıcı çalıştırmasında yoktu; üç
  durumlarını yalnız bağlanan test kapsar.

#### İkinci parti: Ayarlar, hesaplar, bir alan adının dosyaları, sertifikası ve sayfası, içe aktarım, izleme, bileşenler (2026-10-09)

Bileşen testleri ve yerel sahte sunucuya karşı tarayıcı incelemesiyle kaynak
durumu; gerçek sistem çalıştırması ve kurulu sunucu yok. Aynı kural, incelemenin
sıradaki ekranlarına uygulandı. Okumaların ve kaybolan yanıtların arayüzde nasıl
gösterildiğini değiştirir; API, saklanan kayıt, erişim kapısı ya da yaşam
döngüsü değişmez ve hiçbir kabul işi kapanmaz. İçindeki üç şey sunucu tarafını
gerektirir; aşağıda "Sunucu tarafını gerektiren" başlığında listelenmiştir.

**Bu ekranlar önceden ne gösteriyordu.**

- *Ayarlar, iki faktörlü giriş.* Başarısız durum okuması, kurulum formuyla
  birlikte "kapalı" diye çiziliyordu. (Sunucu ikinci bir kurulumu reddeder; yani
  bu tehlikeli değil, yanıltıcıydı.)
- *Ayarlar, Panelin sunduğu sertifika.* Başarısız okuma durum satırını kaldırıp
  yalnız "Sertifika al"ı bırakıyordu. Panelin okuyabildiği bir sertifika
  bulamadığı yanıt "güvenilir bir sertifika ()" diye çiziliyordu. Yanıt alamayan
  sorgu sonsuza dek "Sertifika alınıyor…" diyor, sunucunun on dakika sonra
  kaydını tutmadığı istek ise başarısız sayılıyordu. Sertifika alındıktan sonra,
  Ayarlar'ı hangi bölümde olursa olsun açık tutan her sekme altı saniye sonra
  yeni adrese taşınıyordu; tek bildirim bir bildirim balonuydu.
- *Hesaplar.* Başarısız okumada bildirim olmadan "Henüz hesap yok" ve boş plan
  listesi. Yanıtı kaybolan değişiklik, ele alınmamış bir hata bırakıyor ve
  ekranda hiçbir şey göstermiyordu.
- *Bir alan adının dosyaları.* Başarısız okumada "Bu klasör boş"; yeni klasör
  okunurken az önce çıkılan klasörün satırları yeni yolun altında kalıyordu.
- *Bir alan adının sertifikası.* Başarılı bir isteğin ardından gelen okuma
  başarısız olursa ekran, işaretsiz biçimde "Sertifika yok"u ve istek formunu
  göstermeyi sürdürüyordu. Bağlantısı kopan istek başarısız sayılıyordu.
- *İçe aktarım.* Bağlantısı kopan uygulama hiçbir şey göstermiyor ve "İçe
  aktarmayı başlat"ı yeniden sunuyordu.
- *Pano.* Lisans yalnızca doğrulanamadığında lisans bildirimi "aktif lisans
  gerekiyor" diyordu. Başarısız okuma "0 alan adı, 0 veritabanı, 0 kullanıcı, 0
  posta hesabı" oluyor, hiçbir şey kurulu değilken barındırma bölümünün tamamı
  sayfadan çıkıyordu. Gezinme rozeti başarısız okumada yok oluyordu.
- *İzleme.* Başarısız sorgu grafikleri "Henüz örnek yok" ile değiştiriyordu.
- *Adresiyle açılan alan adı ya da bileşen.* Başarısız arama ya da sunucunun
  listelemediği ad, tek söz etmeden listeye dönüyordu.
- *Bir bileşenin sayfası.* Okunamayan kayıt, tarama sunularak "Bu sunucuya henüz
  bakılmadı" diye çiziliyordu. Sayfa yalnızca bir işlemin sürüp sürmediğini
  sorarken Kur düğmesi "Kuruluyor…" diyordu. Başlat ve durdur, sayfanın ikinci
  kayıt kopyası yüklenene dek bileşenin kimliğini gönderiyordu (BIND'in kimliği
  `bind`, birimi `named`). Servisi olmayan bir araç "Durdu" görünüyordu.
  Bölümleri "ayar dosyası bulunamadı" diyor ve henüz kimsenin adlandırmadığı bir
  birimin günlüğünü okuyordu.
- *Bileşenler listesinin kurulum penceresi.* Bileşenin ek bir depoya ihtiyacı
  olup olmadığını söyleyen okuma başarısız olduğunda depo bölümü gizleniyor ve
  Kur etkin kalıyordu; sürüm "dağıtım varsayılanı" diye okunuyordu.

**Üç durumun ötesinde şimdi ne yapıyorlar.**

- *Sertifika isteği.* İsteğin sonucu şunlardan biridir: hâlâ soruluyor;
  **doğrulanamadı** (sorgular dokuz saniyedir yanıt almıyor; tam istek akılda
  tutulur, sayfa beş saniyede bir sormayı sürdürür, "Tekrar kontrol et" hemen
  sorar, ikinci bir istek sunulmaz ve güvenli adres bağlantı olarak verilir);
  **başarısız** (yalnız sunucu isteği başarısız bildirdiğinde; sunucunun nedeni
  gösterilir ve kartta kalır); **kaydedilmedi** (sunucu, gönderilmesinden on
  dakika sonra hâlâ böyle bir isteği olmadığını söylüyor: hiçbir şey
  başlatılmadı ve istek yeniden yapılabilir); **alındı**.
- *Güvenli adrese taşınma.* Yalnız isteği gönderen sayfa kendini taşır; yalnız
  Panel HTTPS bölümü açıkken ve sekme görünürken; ve ancak nerede, neden yeniden
  açılacağını on saniye boyunca **Burada kal** seçeneğiyle söyledikten sonra.
  Başka bir bölümü açmak kalmak sayılır. İsteği yalnızca tarayıcının deposunda
  bulan sekme onu izler ve asla taşınmaz. Her durumda kart sonrasında güvenli
  adresi bağlantı olarak verir.
- *Yanıtı gelmeyen değişiklik* (hesaplar, planlar, dosyalar, bir alan adının
  sertifikası ve ayarları, iki faktörlü giriş, başlat ve durdur, bir bileşenin
  deposu): ekran değişikliğin yapılıp yapılmadığının bilinmediğini söyler,
  hiçbir şeyi ikinci kez göndermez ve kişi yinelemeden önce bakabilsin diye
  durumu yeniden okur. Sunucunun reddi, sunucunun kendi nedeniyle gösterilir.
- *İçe aktarım.* Bağlantısını kaybeden (ya da Panelin yanıtı yerine bir geçidin
  yanıtını alan: 408, 429, 502, 503, 504) uygulama sayfada bir bildirim bırakır.
  İsteği oluşturan seçimler dondurulur ve "İçe aktarmayı başlat" kalkar.
  "{domain} alan adını kontrol et" yalnız alan adı listesini okur. Alan adı
  oradaysa içe aktarım onu oluşturmuştur; sayfa ona bağlantı verir ve içe
  aktarımı yeniden başlatmaz. Orada değilse sayfa bunu söyler, kontrolü yeniden
  sunar ve ancak o zaman "İçe aktarımı yeniden başlat"ı sunar.
- *Bir bileşenin sayfası.* Başlat, durdur ve yeniden başlat yalnız bir birim
  adlandıran kayıt için vardır ve o birimi gönderir. Birim adlandırmayan kayıtta
  böyle bir denetim yoktur; araç ya da birimi olmayan çalışma ortamı "Kurulu"
  diye okunur.
- *Bir alan adının sekmeleri.* Hangi sekmelerin var olduğu, sunucunun verdiği
  son yanıtı izler. Yeteneklerin sonraki başarısız bir okuması, sunucunun elediği
  sekmeyi geri getirmez, var olanı da almaz; kişi yalnız artık var olmayan
  sekmeden alınır.
- *Sayılar.* Panodaki dört sayı, hesap ve plan listelerinin üstündeki toplamlar
  ve gezinme rozeti yalnız bir yanıt için sayıdır: okunurken "…", okunamadığında
  "–" (gezinmede rozet yok).
- *Sertifika kartındaki sıra.* Önce sertifikanın durumu ya da isteğin sonucu
  gelir; sonra üç hazırlık adımı; sonra da sonuçlanmamış istek, gönderildiği
  formun yanında. Kişinin baktığı yerden uzakta beliren bildirim görünür alana
  kaydırılır (içe aktarım sayfasında da).

**Metinler.** Anahtarlar `web/src/i18n` altındadır (`common.*` kabuk
kataloğunda, kalanı `screens` ve `screens/server` içinde).

- *Bu ekranların herhangi birinde, yanıtı gelmeyen değişiklik (kabuk)*
  - `common.resultUnknown`
    - EN: "The connection dropped before the answer arrived, so it is not known
      whether the change was made. Nothing is sent a second time. What is shown
      is being read again; check it before repeating the action."
    - TR: "Yanıt gelmeden bağlantı koptu; bu yüzden değişikliğin yapılıp
      yapılmadığı bilinmiyor. Hiçbir şey ikinci kez gönderilmez. Gösterilen
      yeniden okunuyor; işlemi yinelemeden önce ona bakın."
- *Ayarlar, iki faktörlü giriş: kontrol ediliyor, kontrol edilemedi, yanıtı
  kaybolan değişiklik*
  - `settings.2fa.checking`
    - EN: "Checking whether two-factor authentication is on for this account…"
    - TR: "Bu hesapta iki faktörlü doğrulamanın açık olup olmadığı kontrol
      ediliyor…"
  - `settings.2fa.unknown`
    - EN: "CelikPanel could not check whether two-factor authentication is on
      for this account, so neither turning it on nor turning it off is offered.
      This does not mean it is off. Nothing was changed. Try again."
    - TR: "CelikPanel bu hesapta iki faktörlü doğrulamanın açık olup olmadığını
      kontrol edemedi; bu yüzden açma da kapatma da sunulmuyor. Bu, kapalı
      olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `settings.2fa.resultUnknown`
    - EN: "The connection dropped before the answer arrived, so it is not known
      whether the change was made. CelikPanel is reading the current state
      again; nothing is sent a second time."
    - TR: "Yanıt gelmeden bağlantı koptu; bu yüzden değişikliğin yapılıp
      yapılmadığı bilinmiyor. CelikPanel güncel durumu yeniden okuyor; hiçbir
      şey ikinci kez gönderilmez."
- *Ayarlar, Panelin sunduğu sertifika: okunuyor, okunamadı, Panel okuyabildiği
  bir sertifika bulamadı*
  - `panelCert.checking`
    - EN: "Reading the certificate the Panel serves…"
    - TR: "Panelin sunduğu sertifika okunuyor…"
  - `panelCert.unknown`
    - EN: "The certificate the Panel serves could not be read from the server,
      so its state is not shown and a new one cannot be requested yet. This does
      not mean the certificate is missing or invalid. Nothing was changed. Try
      again."
    - TR: "Panelin sunduğu sertifika sunucudan okunamadı; bu yüzden durumu
      gösterilmiyor ve şimdilik yenisi istenemiyor. Bu, sertifikanın eksik ya da
      geçersiz olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
  - `panelCert.notReadable`
    - EN: "The Panel answered, but it found no certificate it could read in its
      certificate folder, so it cannot say which one it serves. Nothing was
      changed."
    - TR: "Panel yanıt verdi, ancak sertifika klasöründe okuyabildiği bir
      sertifika bulamadı; bu yüzden hangisini sunduğunu söyleyemiyor. Hiçbir şey
      değiştirilmedi."
- *Aynı kart, sonucu doğrulanamayan istek ("Tekrar kontrol et" ve güvenli adres
  bağlantısıyla)*
  - `panelCert.unconfirmed`
    - EN: "CelikPanel could not confirm the result of the certificate request
      for {domain}: this page is not getting an answer about it. The request may
      still be running, may have finished or may have failed. Nothing else was
      started, and a second request is not offered until this one is known. If
      the Panel has restarted with the new certificate, it now answers at
      {address}."
    - TR: "CelikPanel, {domain} için sertifika isteğinin sonucunu doğrulayamadı:
      bu sayfa onunla ilgili yanıt alamıyor. İstek hâlâ sürüyor, tamamlanmış ya
      da başarısız olmuş olabilir. Başka hiçbir şey başlatılmadı; bu isteğin
      sonucu bilinene dek ikinci bir istek sunulmaz. Panel yeni sertifikayla
      yeniden başladıysa artık {address} adresinde yanıt verir."
  - `panelCert.checkAgain`
    - EN: "Check again"
    - TR: "Tekrar kontrol et"
  - `panelCert.openSecure`
    - EN: "Open the secure address"
    - TR: "Güvenli adresi aç"
- *Aynı kart, sunucunun başarısız bildirdiği istek (nedenli ve nedensiz);
  sunucunun hiç kaydetmediği istek*
  - `panelCert.failedDetail`
    - EN: "The certificate for {domain} was not issued. The server reported:
      {reason} The Panel keeps serving its current certificate. Correct the
      cause, then request the certificate again."
    - TR: "{domain} için sertifika alınamadı. Sunucunun bildirdiği: {reason}
      Panel mevcut sertifikasını sunmayı sürdürüyor. Nedeni giderin, sonra
      sertifikayı yeniden isteyin."
  - `panelCert.failedPlain`
    - EN: "The certificate for {domain} was not issued, and the server gave no
      reason. The Panel keeps serving its current certificate. Check the three
      steps above, then request the certificate again."
    - TR: "{domain} için sertifika alınamadı ve sunucu bir neden bildirmedi.
      Panel mevcut sertifikasını sunmayı sürdürüyor. Yukarıdaki üç adımı kontrol
      edin, sonra sertifikayı yeniden isteyin."
  - `panelCert.notRecorded`
    - EN: "The server has no record of the certificate request for {domain}, so
      it was not started and nothing was changed. The certificate state above
      was read again; you can request the certificate again."
    - TR: "Sunucuda {domain} için sertifika isteğinin kaydı yok; yani istek
      başlatılmadı ve hiçbir şey değiştirilmedi. Yukarıdaki sertifika durumu
      yeniden okundu; sertifikayı yeniden isteyebilirsiniz."
- *Aynı kart, sertifika alındıktan sonra: isteği gönderen sayfa, taşınmadan önce
  ({seconds} geri sayar)*
  - `panelCert.reopen.title`
    - EN: "Certificate issued for {domain}"
    - TR: "{domain} için sertifika alındı"
  - `panelCert.reopen.body`
    - EN: "The Panel is restarting to serve the new certificate. The certificate
      is valid for {domain} only, so this page will reopen at the Panel’s secure
      address, {address}, in {seconds} s. Because that is a different address,
      you may be asked to sign in again there."
    - TR: "Panel yeni sertifikayı sunmak için yeniden başlıyor. Sertifika yalnız
      {domain} için geçerli olduğundan bu sayfa {seconds} sn içinde Panelin
      güvenli adresinde, {address} adresinde yeniden açılacak. Bu farklı bir
      adres olduğu için orada yeniden oturum açmanız istenebilir."
  - `panelCert.reopen.reload`
    - EN: "The Panel is restarting to serve the new certificate. This page will
      reload in {seconds} s so that the browser uses it."
    - TR: "Panel yeni sertifikayı sunmak için yeniden başlıyor. Tarayıcının onu
      kullanması için bu sayfa {seconds} sn içinde yeniden yüklenecek."
  - `panelCert.reopen.stay`
    - EN: "Stay here"
    - TR: "Burada kal"
- *Aynı kart, "Burada kal"dan sonra, başka bir bölümde ya da başka bir sekmede*
  - `panelCert.reopen.stayed`
    - EN: "The Panel restarted to serve the new certificate, which is valid for
      {domain} only. This page was left where it is; the browser may warn about
      the certificate at this address. The Panel’s secure address is {address}."
    - TR: "Panel yeni sertifikayı sunmak için yeniden başladı; sertifika yalnız
      {domain} için geçerlidir. Bu sayfa olduğu yerde bırakıldı; tarayıcı bu
      adreste sertifika uyarısı gösterebilir. Panelin güvenli adresi: {address}"
- *Hesaplar: liste ve planlar*
  - `users.checking`
    - EN: "Reading the accounts…"
    - TR: "Hesaplar okunuyor…"
  - `users.unknown`
    - EN: "The accounts could not be read from the server, so the list is not
      shown. This does not mean there are no accounts. Nothing was changed. Try
      again."
    - TR: "Hesaplar sunucudan okunamadı; bu yüzden liste gösterilmiyor. Bu,
      hesap olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
  - `users.plansUnknown`
    - EN: "The plans could not be read from the server, so an account cannot be
      created here yet. This does not mean there are no plans. Nothing was
      changed. Try again."
    - TR: "Planlar sunucudan okunamadı; bu yüzden şimdilik burada hesap
      oluşturulamıyor. Bu, plan olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `plans.checking`
    - EN: "Reading the plans…"
    - TR: "Planlar okunuyor…"
  - `plans.unknown`
    - EN: "The plans could not be read from the server, so the list is not
      shown. This does not mean there are no plans. Nothing was changed. Try
      again."
    - TR: "Planlar sunucudan okunamadı; bu yüzden liste gösterilmiyor. Bu, plan
      olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Adresiyle açılan alan adı sayfası*
  - `domain.checking`
    - EN: "Reading this domain…"
    - TR: "Bu alan adı okunuyor…"
  - `domain.unknown`
    - EN: "This domain could not be read from the server, so its page is not
      shown. This does not mean the domain is gone. Nothing was changed. Try
      again."
    - TR: "Bu alan adı sunucudan okunamadı; bu yüzden sayfası gösterilmiyor. Bu,
      alan adının kaldırıldığı anlamına gelmez. Hiçbir şey değiştirilmedi.
      Tekrar deneyin."
  - `domain.absent`
    - EN: "This domain is not on this server"
    - TR: "Bu alan adı bu sunucuda değil"
  - `domain.absentHint`
    - EN: "The server answered, and its list has no such domain. It may have
      been removed, or the address may be mistyped. Nothing was changed."
    - TR: "Sunucu yanıt verdi ve listesinde böyle bir alan adı yok. Kaldırılmış
      ya da adres yanlış yazılmış olabilir. Hiçbir şey değiştirilmedi."
  - `domain.noAccess`
    - EN: "This account has no access to this domain"
    - TR: "Bu hesabın bu alan adına erişimi yok"
  - `domain.noAccessHint`
    - EN: "The domain is on this server, but no part of it is shared with this
      account. The account owner can grant access under Team members."
    - TR: "Alan adı bu sunucuda, ancak hiçbir bölümü bu hesapla paylaşılmamış.
      Hesap sahibi, Ekip üyeleri altından erişim verebilir."
- *Bir alan adının dosyaları*
  - `files.checking`
    - EN: "Reading this folder…"
    - TR: "Bu klasör okunuyor…"
  - `files.unknown`
    - EN: "This folder could not be read from the server, so its contents are
      not shown. This does not mean the folder is empty. Nothing was changed.
      Try again."
    - TR: "Bu klasör sunucudan okunamadı; bu yüzden içeriği gösterilmiyor. Bu,
      klasörün boş olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
  - `files.contentUnknown`
    - EN: "The contents of this file could not be read from the server, so it
      was not opened for editing. Nothing was changed. Try again."
    - TR: "Bu dosyanın içeriği sunucudan okunamadı; bu yüzden düzenlemek için
      açılmadı. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `files.uploadUnreadable`
    - EN: "The browser could not read {name} from this device, so nothing was
      uploaded. Choose the file again."
    - TR: "Tarayıcı {name} dosyasını bu cihazdan okuyamadı; bu yüzden hiçbir şey
      yüklenmedi. Dosyayı yeniden seçin."
- *Bir alan adının sertifikası*
  - `ssl.checking`
    - EN: "Reading this domain’s certificate…"
    - TR: "Bu alan adının sertifikası okunuyor…"
  - `ssl.unknown`
    - EN: "The certificate of this domain could not be read from the server, so
      its state is not shown and a certificate cannot be requested or removed
      here yet. This does not mean the domain has no certificate. Nothing was
      changed. Try again."
    - TR: "Bu alan adının sertifikası sunucudan okunamadı; bu yüzden durumu
      gösterilmiyor ve şimdilik buradan sertifika istenemiyor ya da
      kaldırılamıyor. Bu, alan adının sertifikası olmadığı anlamına gelmez.
      Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `ssl.rereading`
    - EN: "Reading the certificate again. What is shown below is the earlier
      answer; the controls are off until the server has answered…"
    - TR: "Sertifika yeniden okunuyor. Aşağıda gösterilen önceki yanıttır;
      sunucu yanıt verene dek denetimler kapalı…"
  - `ssl.checkingProviders`
    - EN: "Reading the certificate authorities this server offers…"
    - TR: "Bu sunucunun sunduğu sertifika yetkilileri okunuyor…"
  - `ssl.providersUnknown`
    - EN: "The certificate authorities this server offers could not be read, so
      a certificate cannot be requested here yet. Nothing was changed. Try
      again."
    - TR: "Bu sunucunun sunduğu sertifika yetkilileri okunamadı; bu yüzden
      şimdilik buradan sertifika istenemiyor. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
- *İçe aktarım: abonelikler ve arşiv*
  - `import.checkingSubs`
    - EN: "Reading the subscriptions…"
    - TR: "Abonelikler okunuyor…"
  - `import.subsUnknown`
    - EN: "The subscriptions could not be read from the server, so a target
      cannot be chosen and the import cannot be started yet. This does not mean
      there are no subscriptions. Nothing was changed. Try again."
    - TR: "Abonelikler sunucudan okunamadı; bu yüzden şimdilik hedef seçilemiyor
      ve içe aktarım başlatılamıyor. Bu, abonelik olmadığı anlamına gelmez.
      Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `import.noSubs`
    - EN: "There is no subscription to import into yet. Create an account with a
      plan under Accounts first."
    - TR: "İçe aktarılacak bir abonelik henüz yok. Önce Hesaplar altında planlı
      bir hesap oluşturun."
  - `import.inspectUnanswered`
    - EN: "The server did not answer, so the archive was not inspected.
      Inspecting only reads the archive; nothing was changed. Try again."
    - TR: "Sunucu yanıt vermedi; bu yüzden arşiv incelenmedi. İnceleme arşivi
      yalnız okur; hiçbir şey değiştirilmedi. Tekrar deneyin."
- *İçe aktarım: yanıtı kaybolan uygulama*
  - `import.unknown.title`
    - EN: "The result of this import is not known"
    - TR: "Bu içe aktarımın sonucu bilinmiyor"
  - `import.unknown.body`
    - EN: "The connection dropped before the server answered. The import may
      have run completely, in part, or not at all. Nothing is sent again by
      itself, and starting it again is not offered until you have checked. Check
      whether {domain} is on this server now; checking only reads."
    - TR: "Sunucu yanıt vermeden bağlantı koptu. İçe aktarım tamamen, kısmen
      çalışmış ya da hiç çalışmamış olabilir. Hiçbir şey kendiliğinden yeniden
      gönderilmez; siz kontrol edene dek yeniden başlatma sunulmaz. {domain}
      alan adının şu an bu sunucuda olup olmadığını kontrol edin; kontrol yalnız
      okur."
  - `import.unknown.check`
    - EN: "Check {domain}"
    - TR: "{domain} alan adını kontrol et"
  - `import.unknown.checkAgain`
    - EN: "Check {domain} again"
    - TR: "{domain} alan adını yeniden kontrol et"
  - `import.unknown.unreadable`
    - EN: "The domain list could not be read, so it is still not known whether
      the import ran. Nothing was changed. Check again."
    - TR: "Alan adı listesi okunamadı; bu yüzden içe aktarımın çalışıp
      çalışmadığı hâlâ bilinmiyor. Hiçbir şey değiştirilmedi. Yeniden kontrol
      edin."
  - `import.unknown.present`
    - EN: "{domain} is on this server now, so the import created it. Which of
      its files, mail, DNS records and databases were imported is not known
      here, and the import is not started again from this page. Open the domain
      and look at each of them."
    - TR: "{domain} şu an bu sunucuda; yani içe aktarım onu oluşturdu.
      Dosyalarından, postasından, DNS kayıtlarından ve veritabanlarından
      hangilerinin aktarıldığı burada bilinmiyor ve içe aktarım bu sayfadan
      yeniden başlatılmaz. Alan adını açıp her birine bakın."
  - `import.unknown.open`
    - EN: "Open {domain}"
    - TR: "{domain} alan adını aç"
  - `import.unknown.absent`
    - EN: "{domain} is not on this server at this moment, so the import has not
      created it. If the server is still working on the archive it can appear
      later: check again in a moment. If it is still absent, you can start the
      import again."
    - TR: "{domain} şu an bu sunucuda değil; yani içe aktarım onu oluşturmadı.
      Sunucu hâlâ arşiv üzerinde çalışıyorsa sonradan görünebilir: biraz sonra
      yeniden kontrol edin. Hâlâ yoksa içe aktarımı yeniden başlatabilirsiniz."
  - `import.runAgain`
    - EN: "Start import again"
    - TR: "İçe aktarımı yeniden başlat"
- *Pano: lisans doğrulanamadığında lisans bildirimi; okunamayan sayı*
  - `license.noticeUnverified`
    - EN: "CelikPanel could not verify the license just now. This does not mean
      your license is missing or expired, and nothing was changed. Existing
      sites, mail, databases and scheduled tasks keep running. You can check
      again under License."
    - TR: "CelikPanel lisansı şu an doğrulayamadı. Bu, lisansınızın eksik ya da
      süresinin dolmuş olduğu anlamına gelmez ve hiçbir şey değiştirilmedi.
      Mevcut siteler, e-posta, veritabanları ve zamanlanmış görevler çalışmaya
      devam eder. Lisans bölümünden yeniden kontrol edebilirsiniz."
  - `license.noticeOpen`
    - EN: "Open License"
    - TR: "Lisans bölümünü aç"
  - `dashboard.countUnread`
    - EN: "could not be read"
    - TR: "okunamadı"
- *İzleme*
  - `monitoring.checking`
    - EN: "Reading the recorded measurements…"
    - TR: "Kaydedilen ölçümler okunuyor…"
  - `monitoring.unknown`
    - EN: "The recorded measurements could not be read from the server, so no
      charts are shown. This does not mean nothing was recorded. Nothing was
      changed. Try again."
    - TR: "Kaydedilen ölçümler sunucudan okunamadı; bu yüzden grafik
      gösterilmiyor. Bu, hiçbir şey kaydedilmediği anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *Bir bileşenin sayfası: kaydı*
  - `svc.checkingRecord`
    - EN: "Reading what is known about {name} on this server…"
    - TR: "Bu sunucuda {name} hakkında bilinen okunuyor…"
  - `svc.recordUnread`
    - EN: "Could not be read"
    - TR: "Okunamadı"
  - `svc.recordUnknown`
    - EN: "CelikPanel could not read what is known about {name} on this server,
      so its state is not shown and nothing is offered for it. This does not
      mean {name} is missing, stopped or unchecked. Nothing was changed. Try
      again."
    - TR: "CelikPanel bu sunucuda {name} hakkında bilineni okuyamadı; bu yüzden
      durumu gösterilmiyor ve onun için hiçbir işlem sunulmuyor. Bu, {name}
      bileşeninin eksik, durmuş ya da bakılmamış olduğu anlamına gelmez. Hiçbir
      şey değiştirilmedi. Tekrar deneyin."
  - `svc.recordStale`
    - EN: "The state of {name} could not be read again just now, so what is
      shown is the earlier answer. Nothing was changed. Start, stop and restart
      are off until it has been read again."
    - TR: "{name} durumu az önce yeniden okunamadı; bu yüzden gösterilen önceki
      yanıttır. Hiçbir şey değiştirilmedi. Başlat, durdur ve yeniden başlat,
      yeniden okunana dek kapalıdır."
  - `svc.installChecking`
    - EN: "Checking for a running operation…"
    - TR: "Süren bir işlem var mı, kontrol ediliyor…"
  - `component.checking`
    - EN: "Reading this component’s record…"
    - TR: "Bu bileşenin kaydı okunuyor…"
  - `component.unknown`
    - EN: "This component’s record could not be read from the server, so its
      unit, versions, ports, configuration files and log are not shown. This
      does not mean it has none. Nothing was changed. Try again."
    - TR: "Bu bileşenin kaydı sunucudan okunamadı; bu yüzden birimi, sürümleri,
      portları, ayar dosyaları ve günlüğü gösterilmiyor. Bu, bunların olmadığı
      anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `component.absent`
    - EN: "This server’s catalogue has no component “{id}”"
    - TR: "Bu sunucunun kataloğunda “{id}” bileşeni yok"
  - `component.absentHint`
    - EN: "The server answered, and its list has no such component. The address
      may be mistyped, or the component is not offered on this server. Nothing
      was changed."
    - TR: "Sunucu yanıt verdi ve listesinde böyle bir bileşen yok. Adres yanlış
      yazılmış olabilir ya da bileşen bu sunucuda sunulmuyor. Hiçbir şey
      değiştirilmedi."
- *Bir bileşenin sayfası: günlüğü*
  - `component.logsChecking`
    - EN: "Reading the log of {unit}…"
    - TR: "{unit} günlüğü okunuyor…"
  - `component.logsUnknown`
    - EN: "The log of {unit} could not be read from the server, so no lines are
      shown. This does not mean the log is empty. Nothing was changed. Try
      again."
    - TR: "{unit} günlüğü sunucudan okunamadı; bu yüzden hiçbir satır
      gösterilmiyor. Bu, günlüğün boş olduğu anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `component.logsNoMatch`
    - EN: "No line in the log contains “{filter}”."
    - TR: "Günlükte “{filter}” içeren satır yok."
- *Bileşenler listesinin kurulum penceresi*
  - `services.repo.checking`
    - EN: "Checking whether this component needs an additional package
      repository…"
    - TR: "Bu bileşenin ek bir paket deposuna ihtiyacı var mı, kontrol
      ediliyor…"
  - `services.repo.unknown`
    - EN: "CelikPanel could not check whether {name} needs an additional package
      repository on this server, so installing is not offered yet. This does not
      mean a repository is missing. Nothing was changed. Try again."
    - TR: "CelikPanel, {name} bileşeninin bu sunucuda ek bir paket deposuna
      ihtiyacı olup olmadığını kontrol edemedi; bu yüzden şimdilik kurulum
      sunulmuyor. Bu, deponun eksik olduğu anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `services.repo.stale`
    - EN: "The package repository of {name} could not be read again just now, so
      what is shown below is the earlier answer. Nothing was changed. Installing
      and changing the repository are off until it has been read again."
    - TR: "{name} bileşeninin paket deposu az önce yeniden okunamadı; bu yüzden
      aşağıda gösterilen önceki yanıttır. Hiçbir şey değiştirilmedi. Kurulum ve
      depo değişikliği, yeniden okunana dek kapalıdır."
  - `services.versionUnknown`
    - EN: "could not be checked"
    - TR: "kontrol edilemedi"
- *Yardım çekmecesi*
  - `help.loading`
    - EN: "Fetching the help text…"
    - TR: "Yardım metni getiriliyor…"
  - `help.unknown`
    - EN: "The help text could not be fetched from the panel. Nothing was
      changed. Try again."
    - TR: "Yardım metni panelden getirilemedi. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
- *Kurulum rehberi, düğmeyi adlandıran cümle (düğmede "Edit plan" / "Planı
  düzenle" yazar)*
  - `setup.guide.accessDNSResume`
    - EN: "This prerequisite is checked automatically. After it passes, the same
      setup continues. To change the reviewed names or addresses, use Edit plan
      below."
    - TR: "Bu gereksinim otomatik kontrol edilir. Doğrulanınca aynı kurulum
      devam eder. İncelenen adları veya adresleri değiştirmek için aşağıdaki
      Planı düzenle düğmesini kullanın."

**Aynı ekranlarda düzeltilenler.**

- Kurulum rehberindeki cümle düğmeyi "Edit setup plan" / "Kurulum planını
  düzenle" diye adlandırıyordu; düğmede "Edit plan" / "Planı düzenle" yazar.
  Cümle artık düğmeyi yazıldığı gibi adlandırır.
- Koyu temada erişim bekletmesinin ve yeniden yükleme bildiriminin karartması
  yarı güçte çizilir. Sayfa zaten laciverttir; açık temanın gerektirdiği güçte
  bu katmanların altındaki küçük soluk metin yaklaşık 1,5:1'e iniyordu.
- Telefonda bir bileşenin başlat, durdur ve yeniden başlat denetimleri sağ
  kenardan taşıyordu; artık alt satıra geçer.
- Hiçbir satırla eşleşmeyen günlük süzgeci "Günlük kaydı yok" diyordu; artık
  hiçbir satırın süzgeci içermediğini söyler.

**Geri gelmesi nasıl engelleniyor.**

- *Mandal.* Bu partiden sonra: 53 dosyada 27 / 43 / 12 / 83 / 25 (ilkinden
  sonra: 62 dosyada 37 / 58 / 14 / 109 / 29). Sonraki kaydın veritabanı ve
  posta yapılandırma ekranlarıyla aynı ağaçta: 47 dosyada 20 / 36 / 10 / 71 /
  23; testin sabitlediği budur. Listeden kalıcı olarak çıkanlar:
  `Settings`, `UsersPage`, `DomainFileManager`, `DomainSSLSettings`,
  `DomainDetail`, `ImportPage`, `LicenseNotice`, `MonitoringPage`,
  `ComponentDetail` ve yeni `ServiceRecordLookup`. Azalan ama hâlâ listede
  olanlar: `App` (tek bir yanıt gözlemcisi), `ServiceList` (kurulum penceresi
  taşındı; listenin kendisi taşınmadı), `Dashboard` (üç sayı okuması taşındı),
  `Layout` (alan adı rozeti taşındı), `ServiceShell` (hiçbir sayı değişmedi:
  okuması, kontrolü ve kurulumu üç güvenlik sözleşmesiyle satır satır
  sabitlenmiştir; bu yüzden yeni bir okuyucu yerine o yapının içinde bir
  "okunamadı" durumu kazandı).
- *Adres başına tek okuyucu.* `lib/managedServices.ts` (kayıtlı bileşen
  kayıtları, işlem izleyicinin güvenli taraf çözücüsüyle),
  `lib/subscriptions.ts`, `lib/accounts.ts`; alan adı listesini Alan Adları
  sayfası, bir alan adının sayfası, ada göre araması, pano ve gezinme rayı tek
  adres ve tek çözücüyle okur, böylece tek isteği paylaşırlar. Kayıtlı bileşen
  kayıtlarının, sonraki kaydın PostgreSQL ve MariaDB sayfaları dahil her
  okuyucu için tek çözücüsü vardır (`useComponentConfigFiles`, içinden tek olgu
  alınmış `useManagedServices`tir): liste olmayan bir yapılandırma dosyası
  listesini de reddeder. Sayfanın kendi okumasından sonraki 30 saniye içinde
  açılan bölüm o yanıtı kullanır ve istek göndermez; barındırma yetenekleri
  zaten böyleydi. Böylece bir bileşen sayfası, başlığının kendi isteğinin
  yanında paylaşılan tek istek yapar. `Remote` bir
  reddin HTTP durumunu taşır; böylece "böyle bir kayıt yok", "yanıt yok"tan
  ayrılabilir; `countText` bir sayıyı yazar; `CouldNotCheck`, Tekrar dene'nin
  yanında ikinci bir bakma yolu alır.
- *Bağlanan test* yukarıdaki her ekranı kazandı: tablonun 12 yeni satırı (her
  okuma dört biçimde esirgenir, sonra olumsuz yanıtlanır), ekranların kendi
  değişikliklerinin çevresinde ne yaptığına dair 14 test: isteği gönderen
  sayfadan ve başka bir sekmeden görülen sertifika isteği; yanıtsız sorgu;
  Hesaplar'da kaybolan yanıt; klasör değişimi; yeniden okuması yanıtlanan ve
  yanıtlanmayan sertifika isteği; alan adı varken ve yokken içe aktarımın
  kaybolan uygulaması; lisans bildirimi; başarısız izleme sorgusu; adıyla açılan
  alan adı; bir bileşenin kaydı, birimi ve Kur etiketi; ve bu dosyanın
  bağlayamadığı dört ekranın kaynağını okuyan bir test.
- *Yer.* Yardım metinleri (iki dil için yaklaşık 100 KiB), Yardım düğmesini
  taşıyan her sayfanın sabit bir parçasıydı ve Ayarlar sayfasını sınırına 0,29
  KiB kalacak kadar yaklaştırmıştı. Artık bir yardım çekmecesi açıldığında
  getirilir; çekmece getirdiğini ya da getiremediğini Tekrar dene ile söyler.
  Paket denetimi o parçayı adıyla arar; geri birleştirilirse derleme düşer.
  Hiçbir sınır yükseltilmedi: Ayarlar 272,90 → 180,67 KiB (gzip 79,71 → 48,35),
  bir alan adının sayfası 236,45 → 139,31 KiB.

**Sunucu tarafını gerektiren; burada değiştirilmedi.**

- `GET /api/v1/panel/certificate`, hem sertifika dosyası olmadığında hem de
  dosya okunamadığında ya da çözümlenemediğinde `https_enabled: false,
  self_signed: false` yanıtlar. Arayüz artık Panelin okuyabildiği bir sertifika
  bulamadığını söyler; ikisinden hangisi olduğunu söyleyemez.
- İçe aktarımın uygulamasının istek kimliği ve işlem kaydı yoktur ve isteğin
  kendi bağlamında çalışır. Kaybolan yanıttan sonra sonucu yalnız alan adı
  listesinden çıkarsanabilir. Tarayıcı çalıştırmasında sayfanın tek gönderimi
  sahte sunucuya altı ya da yedi bağlantıyla ulaştı (tarayıcı, altında kapanan
  bağlantıdaki isteği yineler); bu sahte sunucuya karşı zararsızdır ve tam da
  yinelenmezlik anahtarının var olma nedenidir.
- Yanıtı kaybolan değişikliğin (hesaplar, planlar, dosyalar) de kimliği yoktur;
  ekran yalnız durumu yeniden okuyabilir.

**Yapılmayan.**

- 47 dosya hâlâ eski yolla okuyor (sonraki kaydın veritabanı ve posta
  yapılandırma ekranlarından önce 53). Bu partinin ekranlarından: kurulum penceresi
  dışındaki bileşenler listesi, sayıları dışındaki pano (sistem rakamları,
  güvenlik duvarı, denetim kaydı, bileşen özeti), gezinme rayının sürüm ve
  bileşen okumaları ve `ServiceShell`'in okuyucusu.
- Panodaki "Dikkat gerekenler" listesi alan adı listesine bağlıdır ve o
  geldiğinde belirir; liste bilerek yavaşlatıldığında altındaki her şey 118 px
  (telefonda 158 px) aşağı kaydı. Kullanımda, pano açıldığında ray listeyi
  zaten okumuştur.
- Panoda okunamayan bir sayının kendi Tekrar dene düğmesi yoktur; kartın açtığı
  sayfada vardır.
- Lisans bildirimi kendi okuması başarısız olduğunda hiçbir şey söylemez.
- Bu ekranlar için yönetici dışındaki roller denenmedi.

**Bu partinin tarayıcı incelemesi (2026-10-09).** Gerçek, kurulu bir Chrome'da,
yerel sahte sunucuya karşı (`web/tools/browser-inspect`, `twofactor`,
`panelcert`, `accounts`, `files`, `domainssl`, `importer`, `dashboard`,
`monitoring`, `lookup`, `component`, `installdialog`, `scrim` senaryoları):
masaüstü 1440×900 ve telefon 390×844, Türkçe ve İngilizce, açık ve koyu; sekiz
yapılandırmanın her birinde 69 durum.

- *Sekiz yapılandırmanın hepsinde ölçülen.* Hiçbir durum, sunucu söylemeden
  olumsuz bir cümle çizmedi; hiçbir kontrol durumu bildirim çizmedi; hiçbir
  başarısız okuma hâlâ kontrol ettiğini söylemedi. Sertifika alındıktan sonra
  isteği gönderen sayfa, kendi bölümündeki on saniyenin ardından bir kez,
  `https://panel.example.com:<port>/` adresine taşındı; "Burada kal"dan sonra,
  başka bir bölümden ve başka bir sekmede hiçbir şey taşınmadı. Bir isteğin
  sonucu doğrulanamazken "Sertifika al" kapalıydı. İçe aktarım sayfası
  uygulamayı bir kez gönderdi; kontrol yalnız `GET /api/v1/domains` istedi; "İçe
  aktarımı yeniden başlat" ancak alan adının olmadığı görüldükten sonra
  sunuldu. Pano sayıları önce "…", sonra sayı; başarısız okumalarda "–" okundu;
  gezinme rozeti önce yoktu, sonra "2" oldu. Bir dakika sonraki başarısız
  sorgudan sonra her izleme grafiği bildirimin altında hâlâ çiziliydi. Okuması
  yavaşken açılan klasör, az önce çıkılan klasörün hiçbir satırını göstermedi.
  Adresiyle açılan dört sayfa adresinde kaldı. Kaydı okunamayan bileşen
  "Okunamadı" ve yalnız Yardım'ı; birimi olmayan bileşen "Kurulu" ve yalnız
  Yardım'ı gösterdi. Kurulum penceresinde Kur; depo kontrol edilirken, kontrol
  edilemediğinde ve etkin olmayan zorunlu depo için kapalıydı. Yardım metni bir
  kez, çekmece açıldığında istendi. Bir alan adının sekmeleri, başarısız bir
  yetenek okumasından önce ve sonra aynıydı. Her kontrol durumu ile onu izleyen
  bilinen durum arasında, pano dışında (aşağıda), ölçülen hiçbir şey yer ya da
  boyut değiştirmedi.
- *Bakarak bulunan ve düzeltilen.* Telefonda sertifika kartı üç hazırlık
  adımıyla başlıyordu; bu yüzden durum ve "doğrulanamadı" görünür alanın
  altındaydı; artık önce durum gelir ve beliren bildirim görünür alana
  kaydırılır. Sertifikanın durum satırı telefonda yanıt gelince 34 px, iki
  faktör kartı 12 px büyüyordu; ikisi de genişliklerine göre bir en az
  yüksekliği korur. "Tekrar kontrol et" ile güvenli adres bağlantısı üst üste
  iki ayrı bloktu; artık aynı satırı paylaşır. Bir istekten sonra alan adının
  sertifikası yeniden okunurken "Sertifika yok", yalnızca okuduğunu söyleyen bir
  satırla başarı bildiriminin altında duruyordu; satır artık gösterilenin önceki
  yanıt olduğunu söyler. Servisi olmayan bir araç, sayfasının başında "Durdu"
  okunuyordu. Telefonda başlat, durdur ve yeniden başlat denetimleri sağ
  kenardan taşıyordu. İçe aktarım bildirimi uzun bir sütunun sonunda, telefonda
  görünür alanın dışında beliriyordu.
- *Görülen ve burada değiştirilmeyen.* Panonun dikkat listesi (yukarıda,
  "Yapılmayan"). Telefonda hesap ve dosya tabloları yana kayar; satır
  denetimleri tablo kaydırılana dek ekran dışındadır. Kapalı birincil düğme koyu
  temada etkin olana yakındır (kurulum penceresi). Başarı bildirimi birkaç
  saniyeliğine tema ve hesap denetimlerinin üstünü örter.
- *Kapsanmayan.* Herhangi bir gerçek sunucu, sertifika ya da yeniden başlatma:
  güvenli adrese taşınma kaydedildi ve durduruldu, hiç izlenmedi. Safari,
  Firefox, ekran okuyucu, dokunmatik aygıt; taklit görünümler; yönetici
  dışındaki roller. Hesap ya da plan oluşturan formlar gönderilmedi. Kurulum
  rehberi cümlesi katalogda değiştirildi, ekranda görülmedi.

### Veritabanı ve posta yapılandırma ekranları: dosya düzenleyici olmadan önce okunur, kayıt hizmetin onunla ne yaptığını söyler (2026-10-09)

Bileşen testleri, bir geliştirme konuğunda gerçekten çalıştırılan iki doğrulama
programı ve yerel bir sahte sunucuya karşı tarayıcı incelemesi (bu kaydın
sonunda) bulunan kaynak durumu; kurulu sunucu yok. Aynı tarihli dayanıklılık
sözleşmesi kaydına bakın. Yukarıdaki kaydın kuralını ("bilinmeden olumsuz durum
yok") PostgreSQL ve MariaDB sayfalarına, bir alan adının posta sekmelerine ve
Postfix sayfasına uygular ve bir yapılandırma kaydının söylemesi gerekeni ekler:
her kaydın sonucu şunlardan biridir: kaydedildi (çalışan hizmete ne olduğuyla
birlikte), herhangi bir değişiklikten önce reddedildi, bir değişiklikten sonra
başarısız oldu ve önceki dosya geri kondu, ya da bilinmiyor.

**Aşağıdaki her durumda kim işlem yapar.** Ekrandaki kişi; bir metin "sunucuda"
diyorsa sunucu sahibi, metindeki komutla. Hiçbir şey kendiliğinden yeniden
denenmez. Tekrar dene, "Dosyayı yeniden yükle" ve "Geçerli adresi yeniden yükle"
yalnız okur.

**Metinler.** Anahtarlar `web/src/i18n/screens/server` (veritabanı, kuyruk) ve
`web/src/i18n/screens` (posta) içindedir; `{file}` dosyanın kendi adıdır
(`postgresql.conf`), `{service}` hizmetin adıdır (`PostgreSQL`, `MariaDB`).

- *Bir yapılandırma dosyası: okunuyor, okunamadı (bilinmeyen sonuç; düzenleyici yok, Kaydet yok).*
  - `dbconf.files.checking` (bileşenin hangi dosyaları olduğu):
    - EN: "Reading which configuration files {service} has on this server…"
    - TR: "{service} hizmetinin bu sunucudaki yapılandırma dosyaları okunuyor…"
  - `dbconf.files.unknown` (aynı okuma başarısız):
    - EN: "The configuration files of {service} could not be read from the
      server, so none is shown. This does not mean a file is missing. Nothing
      was changed. Try again."
    - TR: "{service} hizmetinin yapılandırma dosyaları sunucudan okunamadı; bu
      yüzden hiçbiri gösterilmiyor. Bu, bir dosyanın eksik olduğu anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `dbconf.checking` (dosyanın kendisi):
    - EN: "Reading {file} from the server…"
    - TR: "{file} sunucudan okunuyor…"
  - `dbconf.unknown` (dosya okunamadı):
    - EN: "{file} could not be read from the server, so its settings are not
      shown and nothing can be saved here. This does not mean the file is empty
      or missing. Nothing was changed. Try again."
    - TR: "{file} sunucudan okunamadı; bu yüzden ayarları gösterilmiyor ve
      buradan kayıt yapılamıyor. Bu, dosyanın boş ya da eksik olduğu anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Bir yapılandırma dosyası: biliniyor. Düzenleyicinin kendisi hakkında söyledikleri.*
  - `dbconf.note`:
    - EN: "Only the lines you change are rewritten. Comments, includes and
      everything this screen does not show stay in the file exactly as they
      are."
    - TR: "Yalnız değiştirdiğiniz satırlar yeniden yazılır. Yorumlar, include
      satırları ve bu ekranın göstermediği her şey dosyada olduğu gibi kalır."
  - `dbconf.noSettings`:
    - EN: "CelikPanel found no setting it can show in {file}. The whole file can
      be read and edited under “Advanced: raw files”."
    - TR: "CelikPanel, {file} içinde gösterebileceği bir ayar bulamadı. Dosyanın
      tamamı “Gelişmiş: ham dosyalar” altında okunabilir ve düzenlenebilir."
  - `dbconf.hba.none`:
    - EN: "This file holds no access rule. PostgreSQL then refuses every
      connection."
    - TR: "Bu dosyada hiç erişim kuralı yok. PostgreSQL bu durumda her
      bağlantıyı reddeder."
  - `dbconf.hba.order`:
    - EN: "PostgreSQL uses the first rule that matches a connection, so the
      order matters. New rules are added at the end; to place a rule elsewhere,
      edit the file under “Advanced: raw files”."
    - TR: "PostgreSQL bir bağlantıyla eşleşen ilk kuralı kullanır; bu yüzden
      sıra önemlidir. Yeni kurallar sona eklenir; bir kuralı başka bir yere
      koymak için dosyayı “Gelişmiş: ham dosyalar” altında düzenleyin."
  - `dbconf.hba.asWritten`:
    - EN: "Shown as written in the file. This screen does not change or remove
      it; edit it under “Advanced: raw files”."
    - TR: "Dosyada yazıldığı gibi gösteriliyor. Bu ekran onu değiştirmez ya da
      kaldırmaz; “Gelişmiş: ham dosyalar” altında düzenleyin."
  - `dbconf.hba.incomplete`:
    - EN: "Fill in every field of the changed or new rule before saving."
    - TR: "Kaydetmeden önce değişen ya da yeni kuralın her alanını doldurun."
- *Dosya okunduktan sonra değiştiği için reddedilen kayıt (doğrulanmış ret, `409 SETTINGS_CHANGED` ya da `SETTINGS_VERSION_REQUIRED`; yazılan durur, Kaydet kapalıdır, tek eylem yeniden yükler).*
  - `dbconf.stale`:
    - EN: "{file} changed on the server after this page read it, so nothing was
      saved. What you changed is still shown below. Reload the file, then make
      your change again."
    - TR: "{file}, bu sayfa onu okuduktan sonra sunucuda değişti; bu yüzden
      hiçbir şey kaydedilmedi. Değiştirdikleriniz aşağıda duruyor. Dosyayı
      yeniden yükleyin, sonra değişikliğinizi tekrar yapın."
  - `dbconf.reload` (eylem):
    - EN: "Reload the file"
    - TR: "Dosyayı yeniden yükle"
- *Herhangi bir şey yazılmadan reddedilen kayıt (`422 CONFIG_INVALID`; doğrulanmış ret, ekrandaki kişi düzeltir ve yeniden kaydeder). Hizmetin söylediği satır, adlandırdığı alanın ya da kuralın yanında, "{service} yanıtı:" ya da "Kabul edilmedi:" sözünden sonra gösterilir.*
  - `dbconf.refused.daemon` (gerekçe `daemon`):
    - EN: "Nothing is changed: {service} read the new file and does not accept
      it. Correct what it names and save again."
    - TR: "Hiçbir şey değişmedi: {service} yeni dosyayı okudu ve kabul etmiyor.
      Adını verdiği yeri düzeltip yeniden kaydedin."
  - `dbconf.refused.syntax` (gerekçe `syntax`):
    - EN: "Nothing was saved: a rule you changed or added is not one {service}
      accepts. Correct the rule marked below and save again."
    - TR: "Hiçbir şey kaydedilmedi: değiştirdiğiniz ya da eklediğiniz bir kural
      {service} tarafından kabul edilen bir kural değil. Aşağıda işaretlenen
      kuralı düzeltip yeniden kaydedin."
  - `dbconf.refused.lockout` (gerekçe `lockout`):
    - EN: "Nothing was saved: this change would take away the local
      administrator access to PostgreSQL, the rule that lets the server’s own
      postgres account connect over the local socket. CelikPanel and your own
      console both use it. Keep a “local all postgres peer” rule above any rule
      that would refuse it, then save again."
    - TR: "Hiçbir şey kaydedilmedi: bu değişiklik PostgreSQL’e yerel yönetici
      erişimini, yani sunucunun kendi postgres hesabının yerel soket üzerinden
      bağlanmasını sağlayan kuralı kaldırırdı. Onu hem CelikPanel hem de sizin
      konsolunuz kullanır. Onu reddedecek her kuralın üstünde bir “local all
      postgres peer” kuralı bırakın, sonra yeniden kaydedin."
  - `dbconf.refused.empty` (gerekçe `empty`):
    - EN: "Nothing was saved: the new content is empty, and CelikPanel does not
      replace a configuration file with nothing. Reload the file, then make the
      change again."
    - TR: "Hiçbir şey kaydedilmedi: yeni içerik boş ve CelikPanel bir
      yapılandırma dosyasını boş içerikle değiştirmez. Dosyayı yeniden yükleyin,
      sonra değişikliği tekrar yapın."
  - `dbconf.refused.shape` (gerekçe `shape`):
    - EN: "Nothing was saved: the new content is not a configuration file
      CelikPanel writes (it is larger than 1 MB or holds a NUL byte)."
    - TR: "Hiçbir şey kaydedilmedi: yeni içerik CelikPanel’in yazdığı bir
      yapılandırma dosyası değil (1 MB’tan büyük ya da NUL baytı içeriyor)."
  - `dbconf.refused.no_validator` (gerekçe `no_validator`; sunucuda eksik bir önkoşul):
    - EN: "Nothing was saved: the program that checks this file before it
      replaces the current one ({name}) could not be run on this server, and
      CelikPanel does not install a database configuration it could not check.
      The file can still be edited on the server itself."
    - TR: "Hiçbir şey kaydedilmedi: bu dosyayı geçerli dosyanın yerine konmadan
      önce denetleyen program ({name}) bu sunucuda çalıştırılamadı ve CelikPanel
      denetleyemediği bir veritabanı yapılandırmasını kurmaz. Dosya sunucunun
      kendisinde yine düzenlenebilir."
  - `dbconf.refused.other` (bu ekranın sözü olmayan bir gerekçe):
    - EN: "Nothing is changed: the new file was refused. Correct it and save
      again."
    - TR: "Hiçbir şey değişmedi: yeni dosya reddedildi. Düzeltip yeniden
      kaydedin."
  - `dbconf.says`:
    - EN: "{service} says:"
    - TR: "{service} yanıtı:"
  - `dbconf.notAccepted`:
    - EN: "Not accepted:"
    - TR: "Kabul edilmedi:"
  - `dbconf.atLine`:
    - EN: "Line {line}:"
    - TR: "{line}. satır:"
- *Dosya kurulduktan sonra yeniden yükleme başarısız oldu (`502 CONFIG_RELOAD_FAILED`; doğrulanmış hata, hizmetin satırıyla birlikte hata yüzeyinde çizilir).*
  - `dbconf.reloadFailed.restored` (gerekçe `restored`: şu an hiçbir şey değişmiş değil):
    - EN: "The change was not kept: {service} could not reload with the new
      file, so CelikPanel put the previous file back and {service} is running
      with it. Correct the setting and save again."
    - TR: "Değişiklik tutulmadı: {service} yeni dosyayla yeniden yüklenemedi; bu
      yüzden CelikPanel önceki dosyayı geri koydu ve {service} onunla çalışıyor.
      Ayarı düzeltip yeniden kaydedin."
  - `dbconf.reloadFailed.notRestored` (gerekçe `not_restored`: sunucu sahibi, sunucuda işlem yapar):
    - EN: "{service} could not reload with the new file, and CelikPanel could
      not put the previous file back with certainty. What the server holds now
      is shown below. Check the file on the server, reload {service} there, then
      reload this page. The other version is kept on the server as:"
    - TR: "{service} yeni dosyayla yeniden yüklenemedi ve CelikPanel önceki
      dosyayı kesin olarak geri koyamadı. Sunucunun şu an tuttuğu dosya aşağıda
      gösteriliyor. Dosyayı sunucuda denetleyin, {service} hizmetini orada
      yeniden yükleyin, sonra bu sayfayı yenileyin. Diğer sürüm sunucuda şu adla
      duruyor:"
- *Bir kaydın yanıtı hiç gelmedi (bilinmeyen sonuç; her şeyden önce dosya yeniden okunur).*
  - `dbconf.saveUnknown`:
    - EN: "The answer to this save did not arrive, so CelikPanel does not know
      whether the file was changed. Reload the file to see what the server holds
      before saving again."
    - TR: "Bu kaydın yanıtı gelmedi; bu yüzden CelikPanel dosyanın değişip
      değişmediğini bilmiyor. Yeniden kaydetmeden önce sunucunun ne tuttuğunu
      görmek için dosyayı yeniden yükleyin."
- *Kaydedildi. Çalışan hizmete ne olduğu söylenir, asla varsayılmaz.*
  - `dbconf.saved.reloaded` (PostgreSQL):
    - EN: "Saved. {service} read the file again."
    - TR: "Kaydedildi. {service} dosyayı yeniden okudu."
  - `dbconf.saved.waitsForRestart` (PostgreSQL: bekleyen ayarlar):
    - EN: "These settings take effect only after {service} restarts: {names}.
      Restart it from the top of this page when it suits you."
    - TR: "Şu ayarlar ancak {service} yeniden başlatıldıktan sonra geçerli olur:
      {names}. Size uygun olduğunda bu sayfanın üstünden yeniden başlatın."
  - `dbconf.saved.notChecked` (sunucuya sorulamadı):
    - EN: "CelikPanel could not ask {service} whether it accepts every line of
      the file. The reload itself reported no error."
    - TR: "CelikPanel, {service} hizmetine dosyanın her satırını kabul edip
      etmediğini soramadı. Yeniden yüklemenin kendisi hata bildirmedi."
  - `dbconf.saved.restartRequired` (MariaDB):
    - EN: "Saved. {service} checked the file and accepts it. {service} reads
      this file only when it starts, so the change takes effect after its next
      restart. Restart it from the top of this page when it suits you."
    - TR: "Kaydedildi. {service} dosyayı denetledi ve kabul ediyor. {service} bu
      dosyayı yalnız başlarken okur; bu yüzden değişiklik bir sonraki yeniden
      başlatmadan sonra geçerli olur. Size uygun olduğunda bu sayfanın üstünden
      yeniden başlatın."
  - `dbconf.saved.notRunning` (hizmet durmuş):
    - EN: "Saved. {service} is not running, so it will read the file when it
      starts."
    - TR: "Kaydedildi. {service} çalışmıyor; dosyayı başladığında okuyacak."
  - `dbconf.saved.unchanged`:
    - EN: "Nothing to save: the file on the server already holds exactly this."
    - TR: "Kaydedilecek bir şey yok: sunucudaki dosya zaten tam olarak bunu
      tutuyor."
  - `dbconf.saved.backup`:
    - EN: "The previous file is kept on the server as"
    - TR: "Önceki dosya sunucuda şu adla duruyor:"
- *Bir alan adının postası: okunuyor, okunamadı.*
  - `mail.accounts.checking`:
    - EN: "Reading the email accounts of this domain…"
    - TR: "Bu alan adının e-posta hesapları okunuyor…"
  - `mail.accounts.unknown`:
    - EN: "The email accounts of this domain could not be read from the server,
      so the list is not shown. This does not mean there are none. Nothing was
      changed. Try again."
    - TR: "Bu alan adının e-posta hesapları sunucudan okunamadı; bu yüzden liste
      gösterilmiyor. Bu, e-posta hesabı olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `mail.forwarders.checking`:
    - EN: "Reading the forwarders of this domain…"
    - TR: "Bu alan adının yönlendiricileri okunuyor…"
  - `mail.forwarders.unknown`:
    - EN: "The forwarders of this domain could not be read from the server, so
      the list is not shown. This does not mean there are none. Nothing was
      changed. Try again."
    - TR: "Bu alan adının yönlendiricileri sunucudan okunamadı; bu yüzden liste
      gösterilmiyor. Bu, yönlendirici olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `mail.quota.unknown`:
    - EN: "How much each email account uses could not be read from the server,
      so the usage column says so. The accounts themselves are listed. Try
      again."
    - TR: "Her e-posta hesabının ne kadar yer kullandığı sunucudan okunamadı;
      kullanım sütunu bunu belirtiyor. E-posta hesaplarının kendisi
      listeleniyor. Tekrar deneyin."
  - `mail.setup.checking`:
    - EN: "Reading the connection settings…"
    - TR: "Bağlantı ayarları okunuyor…"
  - `mail.setup.unknown`:
    - EN: "The connection settings of this domain could not be read from the
      server, so they are not shown. Nothing was changed. Try again."
    - TR: "Bu alan adının bağlantı ayarları sunucudan okunamadı; bu yüzden
      gösterilmiyor. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `mail.webmail.unknown`:
    - EN: "CelikPanel could not check whether webmail is available on this
      server. This does not mean it is not. Nothing was changed. Try again."
    - TR: "CelikPanel bu sunucuda web postanın kullanılabilir olup olmadığını
      kontrol edemedi. Bu, kullanılamadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `mail.healthChecking`:
    - EN: "Checking deliverability…"
    - TR: "Teslim edilebilirlik kontrol ediliyor…"
  - `mail.healthUnknown`:
    - EN: "The deliverability checks of this domain could not be read from the
      server, so no result is shown. This does not mean a check failed. Nothing
      was changed. Try again."
    - TR: "Bu alan adının teslim edilebilirlik denetimleri sunucudan okunamadı;
      bu yüzden sonuç gösterilmiyor. Bu, bir denetimin başarısız olduğu anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `mail.rbl.unknown`:
    - EN: "The blocklist check did not complete, so no result is shown. This
      does not mean the address is listed, and it does not mean it is clean. Try
      again."
    - TR: "Kara liste kontrolü tamamlanmadı; bu yüzden sonuç gösterilmiyor. Bu,
      adresin listede olduğu anlamına da temiz olduğu anlamına da gelmez. Tekrar
      deneyin."
- *Catch-all adresi: değiştirilebilmeden önce gösterilir.*
  - `mail.catchAll.checking`:
    - EN: "Reading the catch-all address of this domain…"
    - TR: "Bu alan adının catch-all adresi okunuyor…"
  - `mail.catchAll.unknown`:
    - EN: "The catch-all address of this domain could not be read from the
      server, so it is not shown and cannot be changed here. This does not mean
      none is set. Nothing was changed. Try again."
    - TR: "Bu alan adının catch-all adresi sunucudan okunamadı; bu yüzden
      gösterilmiyor ve buradan değiştirilemiyor. Bu, bir adres ayarlı olmadığı
      anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `mail.catchAll.none` (biliniyor: ayarlı değil):
    - EN: "No catch-all address is set for this domain."
    - TR: "Bu alan adı için catch-all adresi ayarlı değil."
  - `mail.catchAll.stale` (`409 SETTINGS_CHANGED`):
    - EN: "The catch-all address changed on the server after this page read it,
      so nothing was saved. What you typed is still in the field. Reload the
      current address, then make your change again."
    - TR: "Catch-all adresi, bu sayfa onu okuduktan sonra sunucuda değişti; bu
      yüzden hiçbir şey kaydedilmedi. Yazdığınız adres alanda duruyor. Geçerli
      adresi yeniden yükleyin, sonra değişikliğinizi tekrar yapın."
  - `mail.catchAll.reload` (eylem):
    - EN: "Reload the current address"
    - TR: "Geçerli adresi yeniden yükle"
  - `mail.catchAll.notSaved`:
    - EN: "The catch-all address was not saved. Nothing was changed. Try again."
    - TR: "Catch-all adresi kaydedilmedi. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
- *Posta kuyruğu.*
  - `postfix.queue.checking`:
    - EN: "Reading the mail queue…"
    - TR: "Mail kuyruğu okunuyor…"
  - `postfix.queue.unknown`:
    - EN: "The mail queue could not be read from Postfix, so it is not shown.
      This does not mean the queue is empty. Nothing was changed. Try again; if
      it keeps failing, check on the server that Postfix is running (sudo
      systemctl status postfix)."
    - TR: "Mail kuyruğu Postfix’ten okunamadı; bu yüzden gösterilmiyor. Bu,
      kuyruğun boş olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin; sorun sürerse sunucuda Postfix’in çalıştığını denetleyin (sudo
      systemctl status postfix)."
  - `postfix.actionFailed`:
    - EN: "The queue action was not confirmed by the server. The queue is read
      again below."
    - TR: "Kuyruk işlemi sunucu tarafından doğrulanmadı. Kuyruk aşağıda yeniden
      okunuyor."
- *Her ekrandan kodla okunan iki ret* (kabuk, `err.<CODE>`):
  - `err.MAIL_POLICY_NOT_RELOADED` (`502`, `mutation_applied: true`; kaydedilen değerlerin üstünde, "Yeniden yükleme yanıtı:" sözü ve satırıyla çizilir):
    - EN: "The mail policy was saved to /etc/postfix/main.cf, but Postfix could
      not be reloaded, so Postfix is still running with the previous settings.
      Nothing was rolled back. On the server, run sudo postfix check to see what
      Postfix objects to, correct it, then run sudo systemctl reload postfix.
      The values shown below are the saved ones."
    - TR: "Posta politikası /etc/postfix/main.cf dosyasına kaydedildi ancak
      Postfix yeniden yüklenemedi; bu yüzden Postfix hâlâ önceki ayarlarla
      çalışıyor. Hiçbir şey geri alınmadı. Sunucuda sudo postfix check komutuyla
      Postfix’in neye itiraz ettiğini görün, düzeltin, sonra sudo systemctl
      reload postfix komutunu çalıştırın. Aşağıda gösterilen değerler kaydedilen
      değerlerdir."
  - `err.CRON_JOB_AMBIGUOUS` (`409`):
    - EN: "This task stands twice in the crontab, so CelikPanel cannot tell
      which line to change and changed nothing. Remove one of the two lines on
      the server (sudo crontab -u <site user> -e), then reload this list."
    - TR: "Bu görev crontab’da iki kez duruyor; bu yüzden CelikPanel hangi
      satırı değiştireceğini bilemedi ve hiçbir şeyi değiştirmedi. Sunucuda iki
      satırdan birini kaldırın (sudo crontab -u <site kullanıcısı> -e), sonra bu
      listeyi yeniden yükleyin."

API yanıtları aynı durumlar için İngilizce bir cümle taşır (Panel:
`config_rpc_error.go`, `mail_policy_handlers.go`, `email_handlers.go`,
`domain_cron_errors.go`); hizmetin söylediği satır onun yanında `vars.detail`
içinde, sınırlı olarak, doğrulama kopyasının adı dosyanınkiyle değiştirilmiş ve
parola atamasına benzeyen her şey silinmiş hâlde taşınır.

**Ekrandaki kişi için ne değişti.**

- *PostgreSQL ve MariaDB sayfaları.* Bileşenin dosya listesi okunurken: sakin
  tek satır. Okunamadığında: bildirim ve Tekrar dene; "… bulunamadı." yalnız
  okunmuş ve öyle bir dosyayı adlandırmayan bir tarama için. Düzenleyici yalnız
  okunmuş bir dosya için vardır; her ayarı etkin ya da yorum satırı olarak
  gösterir, değişeni işaretler ve Kaydet yalnız bilinen, değişmiş, eskimemiş bir
  dosya için açıktır. `pg_hba.conf` dosyasının tablonun tutamadığı kuralları
  (seçenekler, tırnaklı adlar, ağ maskesi, include'lar) yazıldığı gibi
  gösterilir.
- *Bir alan adının postası.* "Hesaplar" ve "Yönlendirme" yanındaki sayı, liste
  bilinince sayıdır, okunurken "…", okunamayınca "–". "Adres oluştur" listeyi
  gerektirir. Okunamayan kullanım, sütununda bunu söyler ve posta kutuları
  listelenmeye devam eder. Web posta kartı "bu sunucuda kullanılamıyor" sözünü
  yalnız sunucunun verdiği bir yanıt için söyler.
- *Catch-all.* Önce geçerli adres okunur ve gösterilir; o zamana dek alan ve iki
  düğme kapalıdır. Bir adres ayarlıyken "Kapat" her zaman sunulur.
- *Posta kuyruğu.* Sayılar ve "Mail kuyruğu boş" yalnız okunmuş bir kuyruk için
  vardır. Boşaltma ve silme o zamana dek kapalıdır ve yanıtları okunur: sunucunun
  doğrulamadığı bir işlem yapıldı diye duyurulmaz.
- *Zamanlanmış görevler.* Devre dışı bir görev etkinleştirilebilir,
  değiştirilebilir ve silinebilir (sunucu önceden üçünü de reddediyordu). Hiçbir
  metin değişmedi.

**Yapılmayan.** Okunamadı bildirimi okumanın neden başarısız olduğunu söylemez.
Posta yöneticisinin sekmeleri telefonda kaymak yerine alt satıra geçer.
Telefonda posta kutusu ve kuyruk tabloları yana kayar. Panel'in zamanlanmış bir
görevin üstüne yazdığı açıklama, görev silindikten sonra kalır. Taşınmayan: izin
listesinin diğer 47 dosyası (bu partinin iki parçası tek ağaçta durmadan önce
56); posta kimlik doğrulama paneli bunlardan biridir.

**Bu partinin ilk parçasıyla tek ağaçta (aynı tarih).** İki parça yan yana
yazıldı ve her biri kayıtlı bileşen kayıtlarını kendi yoluyla okuyordu. Artık
tek çözücü ve tek istek var (yukarıda "Adres başına tek okuyucu"); bu,
PostgreSQL ve MariaDB sayfalarında üç şeyi değiştirir.

1. *Çözücünün reddettiği tarama "okunamadı"dır.* Bu kaydın daha gevşek
   okuyucusu, hizmet listesi taşıyan her yanıtı tarama sayıyordu; bu yüzden hiç
   taranmamış bir sunucu ya da katalog alanlarını taşımayan bir yanıt
   "postgresql.conf bulunamadı" diye okunuyordu. İkisi de artık Tekrar dene ile
   `dbconf.files.unknown`dır; "bulunamadı", öyle bir dosyayı adlandırmayan
   eksiksiz bir tarama ister.
2. *Tek okuma bir kez duyurulur.* Düzenleyicinin altındaki genel bakış ve
   günlük, dosya listesiyle aynı okumadan çizilir. Yanıtla birlikte gelirler; o
   okuma sürerken ya da başarısız olduğunda bunu dosya kartı, tek Tekrar dene
   ile söyler. Bu düzeltilmeden önce başarısız bir okuma iki bildirim ve iki
   Tekrar dene gösteriyor, geç açılan bölümün yaptığı ikinci istek başarısız
   olup ilk yanıtın az önce gösterdiği düzenleyiciyi geri alabiliyordu.
3. *Adresiyle açılan sayfa kayıtlarla başlar.* Bir bileşen sayfasının üstündeki
   arama (ilk parça) kayıtları sayfa çizilmeden önce okur; bu yüzden orada
   yavaş ya da başarısız bir okuma, aramanın "okunuyor" ya da "okunamadı"
   durumudur. Dosya kartının kendi iki durumu, sayfa Bileşenler listesinden
   açıldığında görülür. Tarayıcı senaryosu `dbconfig` sayfaya bu yoldan ulaşır.

Bir bileşen sayfasının başlık düğmeleri artık Türkçede 390 px'lik ekranın dışına
taşmaz: ilk parçanın kabuğu onları ikinci satıra indirir.

**Tarayıcı incelemesi (2026-10-09).** Gerçek, kurulu bir Chrome'da yerel sahte
sunucuya karşı (`web/tools/browser-inspect`, `dbconfig`, `mailscreens`,
`mailqueue`, `cron` senaryoları): masaüstü 1440×900 ve telefon 390×844, Türkçe ve
İngilizce, açık ve koyu; sekiz yapılandırmanın her birinde 45 durum.

- *Sekizinde de ölçülen.* Bir okumanın yolda olduğu ya da başarısız olduğu her
  durumda olumsuz cümlelerin hiçbiri ekranda değildi ("bulunamadı.", "hiç erişim
  kuralı yok", "Henüz e-posta hesabı yok", "bu sunucuda kullanılamıyor", "Mail
  kuyruğu boş", "catch-all adresi ayarlı değil" ve İngilizce biçimleri) ve
  bilinmeyen bir yapılandırma dosyası için hiçbir düzenleyici alanı ve Kaydet
  yoktu. Başarısız her okuma, Tekrar dene ile birlikte tam olarak kendi
  bildirimini gösterdi. Hiçbir durumda yatay sayfa taşması yoktu. Reddedilen bir
  kayıttan sonra yazılan değer alanında duruyordu ve hizmetin satırı onun
  yanındaydı (`aria-describedby` ile `aria-invalid`). 409'dan sonra alanlar ve
  Kaydet kapalıydı ve tek eylem yeniden yüklemeydi. Devre dışı zamanlanmış görev
  tek bir PUT ve tek bir yeniden okumayla etkin oldu.
- *Bakılarak bulunan ve düzeltilen.* Posta yöneticisinin dört sekmesi telefon
  ekranının dışına taşıyordu; "Ayarlar" sekmesine (catch-all oradadır)
  ulaşılamıyordu ve bir alanı görünür kılmak bütün sayfayı yana kaydırıyordu:
  artık alt satıra geçiyorlar. Telefonda bir erişim kuralının kaldırma denetimi
  her kuralın altında kendi satırında duruyordu ve kayıt satırı üç satır
  tutuyordu: kuralın numarası ve kaldırma denetimi aynı satırı paylaşıyor, durum
  satırı iki kısa eylemin üstünde duruyor. Erişim kurallarının satır başına
  yinelenen sütun adları geniş ekranda tekrar ediyordu: orada sütunları ilk satır
  adlandırıyor. Hizmetin bir alanın yanındaki satırı aynı ret için ikinci bir
  uyarıydı: tek duyuru formun üstündeki bildirimdir. İki simge denetimi (kota
  düzenle, kopyala) 22 px idi: artık 26 px.
- *Görülen ve burada değiştirilmeyen.* Bileşen sayfasının geri bağlantısı (başka
  bir ekranın dosyası; başlık düğmeleri bu partinin ilk parçasından beri alt
  satıra geçer). Kapalı birincil düğme koyu
  temada etkin olana yakındır (ortak düğme). Düzenleyici kartı, dosya gelince
  tek satırdan bütün düzenleyiciye büyür; altında duranlar yer değiştirir.
- *Kapsanmayan.* Herhangi bir gerçek sunucu; Safari, Firefox, ekran okuyucu,
  dokunmatik aygıt; taklit görünümler; yönetici dışındaki roller; ham dosya
  düzenleyicisinin ret durumları (yalnız bağlanan test).
