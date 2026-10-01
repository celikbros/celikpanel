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
"uygulanıyor" gösterir.
