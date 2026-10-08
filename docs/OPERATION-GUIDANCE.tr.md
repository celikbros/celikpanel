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
