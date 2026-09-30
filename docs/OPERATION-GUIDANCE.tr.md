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
