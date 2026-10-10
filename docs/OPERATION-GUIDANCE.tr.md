# İşlemlerde uygulanabilir yönlendirme

*13 Eylül 2026 tarihinde kaydedilen ürün gereksinimi · [English](OPERATION-GUIDANCE.md)*

**Durum: ürünün tamamında zorunlu; uygulama ve inceleme henüz tamamlanmadı.**
Mevcut kaynak değişikliği, kabul edilen plandaki DNS rolünü kullanan kurulum
ilerlemesini, mevcut panel lisans durumlarını ve genel hizmet işlem hatalarını
kapsar. Her işlemin doğrulandığını veya üçüncü taraf ürün lisansı adaptörlerinin
uygulandığını göstermez.

> **Tarihler üzerine düzeltme notu (9 Ekim 2026'da kaydedildi).** Aşağıdaki kayıtlar ve bazı
> kod yorumları 2026-10-10, 2026-10-11 ve 2026-10-12 etiketlerini (ayrıca "10 Ekim 2026"
> gibi yazımları) taşır. Bunlar takvim tarihi değildir. Metin 2026-10-08 ve 2026-10-09
> günlerinde (yerel saat, UTC+3) işlendi; etiket saatten okunmadı, her iş turunda bir
> artırıldı. Kayıtlar birbirine bu etiketlerle atıf yaptığı için ("2026-10-10 kaydı")
> etiketler olduğu gibi kalır. Her birini bir turun adı olarak okuyun:
>
> - 2026-10-10: ayar yazılarının ilk gerçek sistem ölçümü ve sonrasındaki iş (posta
>   yenilemesi, hizmet eylemleri, istek kimliği). 2026-10-09, 00:45-05:57 arasında
>   işlendi: `6746142ae`, `15818740a`, `874d12e43`, `faa5ef085`.
> - 2026-10-11: ikinci gerçek sistem ölçümü ve düzeltmeleri. 2026-10-09, 08:05-09:12
>   arasında işlendi: `76bef04b8`, `c523bbfd2`, `cfa329676`.
> - 2026-10-12: son gerçek sistem turu. 2026-10-09, 09:12-13:06 arasında işlendi:
>   `cfa329676`, `47a28dad0`, `dd1710256`, `557b554eb`.
> - 2026-10-09 etiketi daha önce aynı biçimde kullanıldı: bu etiketli bölümler ("Dördüncü
>   parti" hariç) 2026-10-08'de işlendi (`020a98ca1`, `51c969c61`, `545b26337`, `c4cf7fd9d`).
>
> `set1-20261010`, `set2-20261011` ve `set3-20261012` kanıt dizinlerinin adları aynı
> etiketleri taşır. Gerçekte 2026-10-08 21:18-22:15 UTC, 2026-10-09 03:29-04:49 UTC ve
> 2026-10-09 07:00-08:46 UTC aralıklarında çalıştılar. Sağlama toplamı listeleri mühürlü
> olduğu için adları değişmez.
>
> Bu nottan sonra yazılan kayıtlar saat tarihini kullanır. 10, 11 veya 12 Ekim 2026
> tarihli olup burada listelenmeyen bir bölüm gerçektir. Bu belgede tur etiketi taşıyan
> veya anan bölümler: "İkinci parti", "Dördüncü parti", "İlk gerçek sistem ölçümünden
> sonra ayar yazıları", "Hizmet eylemleri ve posta sertifikası yenilemesi", "Bir değişiklik
> bir kez gönderilir ve bir kez yanıtlanır", "İkinci gerçek sistem ölçümünden sonra",
> "Son gerçek sistem turundan sonra".

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
  not start a server operation."; **2026-10-10'dan beri yanıt vermemiş ilk okuma
  bunun yerine sessiz zemini, sonra `recovery.waitingHelp` cümlesini gösterir;
  bu cümle yalnız sayfa doğrulanmış oturum olmadan yeniden okurken görünür,
  aşağıdaki o tarihli girdilere bakın**). `recovery.availabilityTitle` ("Panelin hazır
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
  yinelenmezlik anahtarının var olma nedenidir. 2026-10-10'dan beri uygulama bir
  istek kimliği taşır (D-029; aşağıdaki o tarihli kayıt).
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

#### Üçüncü parti: hizmet sayfaları, panodaki ilgi listesi, DNS ayarları ve DNS motorunun takılan değişikliği (2026-10-09)

Bileşen testleri ve yerel bir sahte sunucuya karşı tarayıcı incelemesiyle kaynak
durumu; gerçek sistemde çalıştırılmadı, kurulu sunucuya dokunulmadı. Aynı kural,
planlanandan küçük bir kümeye uygulandı: bu parti, değiştirdiği şey doğrulanabilsin
diye daraltıldı ve izin listesinin kalanına dokunulmadı ("Yapılmayanlar"a bakın).
Arayüzde okumaların nasıl gösterildiğini ve DNS motoru kartının bir isteğini kimin
gönderdiğini değiştirir; API, kayıtlı kayıt, erişim kapısı ya da yaşam döngüsü
değişmez ve hiçbir kabul işi kapanmaz.

**Bu ekranlar önceden ne gösteriyordu.**

- *Fail2ban.* Başarısız ya da henüz yanıtlanmamış okuma boş listeydi: ikisi de
  olan bir sunucuda "Aktif hapishane yok" ve "Banlı IP yok", iki sekmenin yanında 0;
  ayarlar sekmesinde ise hiçbir şey.
- *Nginx.* Üç okuma sürerken ve başarısız olduktan sonra her değer "—", hız
  sınırları "Tanımlı hız-limit bölgesi yok" idi.
- *PHP.* Aynı iki durumda "Eklenti bulunamadı"; anahtar, sunucu yanıt vermeden
  ekranda değişiyor, sunucu reddederse geri alınıyordu. php.ini düzenleyicisi
  başarısız okumayı tarayıcı uyarısıyla bildiriyor, sonra hiçbir şey çizmiyordu.
- *Dovecot.* İki değer de tek söz etmeden "—" idi.
- *PowerDNS.* Sayfa bileşen kayıtlarını kendi başına okuyordu; okuma başarısız
  olunca dosya listesini göstermiyor ve bir şey söylemiyordu.
- *DNS ayarları.* Başarısız ilk okuma, yeniden okuma yolu olmayan kırmızı bir
  banttı.
- *Pano.* `GET /api/v1/firewall` ne yanıt verirse güvenlik duvarının durumu
  olarak saklanıyordu. `enabled` taşımayan bir yanıt — Agent'ın 200 ile
  gönderdiği kendi hatası — "Güvenlik duvarı kapalı — tüm portlar açık" satırını
  "Aç" düğmesiyle çiziyordu. "İlgi istiyor" bölümü en yavaş okuma yanıt verince
  beliriyor ve sayfayı 118 px (telefonda 158 px) aşağı itiyordu.

**Metinler.** Aksi belirtilmedikçe anahtarlar `web/src/i18n/screens/server`
içindedir.

- *Fail2ban, hapishaneler* — kontrol ediliyor (`f2b.jails.checking`): EN "Reading
  Fail2ban’s jails…" · TR "Fail2ban’in hapishaneleri okunuyor…". Okunamadı
  (`f2b.jails.unknown`):
  - EN: "Fail2ban’s jails could not be read from the server, so they are not
    listed. This does not mean there are none. Nothing was changed. Try again."
  - TR: "Fail2ban’in hapishaneleri sunucudan okunamadı; bu yüzden listelenmiyor. Bu,
    hapishane olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Fail2ban, yasaklı adresler* — kontrol ediliyor (`f2b.banned.checking`): EN
  "Reading the addresses Fail2ban has banned…" · TR "Fail2ban’in yasakladığı
  adresler okunuyor…". Okunamadı (`f2b.banned.unknown`):
  - EN: "The addresses Fail2ban has banned could not be read from the server, so
    they are not listed. This does not mean none is banned. Nothing was changed.
    Try again."
  - TR: "Fail2ban’in yasakladığı adresler sunucudan okunamadı; bu yüzden
    listelenmiyor. Bu, yasaklı adres olmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
  - "Yasağı kaldır" yalnız sunucunun listelediği satır için vardır. Ardından,
    yanıt ne olursa olsun iki liste yeniden okunur; bu okuma başarısız olursa
    önceki liste ortak bildirimin (`common.staleNotice`) altında kalır ve "Yasağı
    kaldır" kapalıdır.
- *Fail2ban, ayarlar* — kontrol ediliyor (`f2b.config.checking`): EN "Reading
  Fail2ban’s settings…" · TR "Fail2ban’in ayarları okunuyor…". Okunamadı
  (`f2b.config.unknown`): EN "Fail2ban’s settings could not be read from the
  server, so they are not shown. Nothing was changed. Try again." · TR
  "Fail2ban’in ayarları sunucudan okunamadı; bu yüzden gösterilmiyor. Hiçbir şey
  değiştirilmedi. Tekrar deneyin."
- Her Fail2ban sekmesinin yanındaki sayı, liste bilinince sayıdır; okunurken "…",
  okunamayınca "–".
- *Nginx* — kontrol ediliyor (`nginx.global.checking`, `nginx.ssl.checking`,
  `nginx.rate.checking`): EN "Reading Nginx’s global settings…", "Reading Nginx’s
  TLS settings…", "Reading Nginx’s rate-limit zones…" · TR "Nginx’in genel
  ayarları okunuyor…", "Nginx’in TLS ayarları okunuyor…", "Nginx’in hız sınırı
  bölgeleri okunuyor…". Okunamadı (`nginx.global.unknown`, `nginx.ssl.unknown`,
  `nginx.rate.unknown`):
  - EN: "Nginx’s global settings could not be read from the server, so they are
    not shown. This does not mean they are not set. Nothing was changed. Try
    again." / "Nginx’s TLS settings could not be read from the server, so they
    are not shown. This does not mean they are not set. Nothing was changed. Try
    again." / "Nginx’s rate-limit zones could not be read from the server, so
    they are not listed. This does not mean none is defined. Nothing was
    changed. Try again."
  - TR: "Nginx’in genel ayarları sunucudan okunamadı; bu yüzden gösterilmiyor.
    Bu, ayarlanmadıkları anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
    deneyin." / "Nginx’in TLS ayarları sunucudan okunamadı; bu yüzden
    gösterilmiyor. Bu, ayarlanmadıkları anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin." / "Nginx’in hız sınırı bölgeleri sunucudan
    okunamadı; bu yüzden listelenmiyor. Bu, tanımlı bölge olmadığı anlamına
    gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - "—" yalnız bilinen bir yanıtın boş bıraktığı değer için çizilir. Hız sınırı
    tablosunun ilk sütununda çevrilmemiş "Name" başlığı vardı; artık
    `nginx.rl.name` (EN "Name" · TR "Ad").
- *PHP, bir sürümün eklentileri* — kontrol ediliyor (`php.extensions.checking`):
  EN "Reading the extensions of PHP {version}…" · TR "PHP {version} eklentileri
  okunuyor…". Okunamadı (`php.extensions.unknown`):
  - EN: "The extensions of PHP {version} could not be read from the server, so
    they are not listed and cannot be switched here yet. This does not mean there
    are none. Nothing was changed. Try again."
  - TR: "PHP {version} eklentileri sunucudan okunamadı; bu yüzden listelenmiyor
    ve şimdilik buradan açılıp kapatılamıyor. Bu, eklenti olmadığı anlamına
    gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - Anahtar yalnız sunucunun listelediği eklenti için vardır. Birine basmak
    değişikliği bir kez gönderir; ardından liste yeniden okunur ve anahtar, kabul
    edilse de reddedilse de sunucunun söylediğini gösterir. Bu sırada ve liste
    önceki yanıtken anahtarlar kapalıdır.
- *PHP, bir sürümün php.ini dosyası* — kontrol ediliyor (`php.ini.checking`): EN
  "Reading php.ini of PHP {version}…" · TR "PHP {version} için php.ini
  okunuyor…". Okunamadı (`php.ini.unknown`): EN "php.ini of PHP {version} could
  not be read from the server, so its settings are not shown and cannot be
  changed here yet. Nothing was changed. Try again." · TR "PHP {version} için
  php.ini sunucudan okunamadı; bu yüzden ayarları gösterilmiyor ve şimdilik
  buradan değiştirilemiyor. Hiçbir şey değiştirilmedi. Tekrar deneyin." Form
  yalnız bir yanıttan kurulur; başka bir sürüm için okunan hiçbir şey formda
  kalmaz.
- *Dovecot, çalışma süresi ve bağlantılar*: okunurken "…", okunamayınca "–" ve
  kartların altında (`dovecot.statsUnknown`):
  - EN: "Dovecot’s uptime and connection count could not be read from the server,
    so they are not shown. This does not mean Dovecot is stopped or has no
    connections. Nothing was changed. Try again."
  - TR: "Dovecot’un çalışma süresi ve bağlantı sayısı sunucudan okunamadı; bu
    yüzden gösterilmiyor. Bu, Dovecot’un durduğu ya da bağlantı olmadığı anlamına
    gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *PowerDNS, yapılandırma dosyaları*: PostgreSQL ve MariaDB sayfalarıyla aynı
  paylaşılan okuma ve aynı iki cümle (`dbconf.files.checking`,
  `dbconf.files.unknown`, "PowerDNS" ile).
- *DNS ayarları (Ayarlar → DNS)* — kontrol ediliyor (`dnssrv.checking`): EN
  "Reading this server’s DNS settings…" · TR "Bu sunucunun DNS ayarları
  okunuyor…". Okunamadı (`dnssrv.unknown`), Tekrar dene ile; sunucunun açıkladığı
  ret altında gösterilir:
  - EN: "The DNS settings of this server could not be read, so they are not shown
    and cannot be changed here yet. This does not mean DNS is not set up. Nothing
    was changed. Try again."
  - TR: "Bu sunucunun DNS ayarları okunamadı; bu yüzden gösterilmiyor ve şimdilik
    buradan değiştirilemiyor. Bu, DNS’in kurulmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
- *Pano, "İlgi istiyor"* (anahtarlar `web/src/i18n/screens` içinde). Bölüm ilk
  çizimden beri sayfadadır. Kalem yalnız sunucunun verdiği yanıttan listelenir.
  Liste dört okumadan kurulur (süresi dolan sertifikalar, alan adı listesi,
  güvenlik duvarı, bileşen kayıtları):
  - biri sürerken (`dashboard.attentionChecking`): EN "Checking what needs
    attention on this server…" · TR "Bu sunucuda ilgi isteyen bir şey olup
    olmadığı kontrol ediliyor…";
  - biri okunamadığında (`dashboard.attentionUnread`), yalnız başarısız olanları
    okuyan Tekrar dene ile: EN "Part of this server’s state could not be read, so
    this list may be incomplete. This does not mean something is wrong. Nothing
    was changed. Try again." · TR "Bu sunucunun durumunun bir bölümü okunamadı;
    bu yüzden bu liste eksik olabilir. Bu, bir şeyin yanlış olduğu anlamına
    gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin.";
  - dördü de yanıt verdiğinde ve listede bir şey yokken
    (`dashboard.attentionNone`): EN "Nothing CelikPanel read needs action:
    certificates, the firewall and the installed components." · TR "CelikPanel’in
    okuduklarında işlem gerektiren bir şey yok: sertifikalar, güvenlik duvarı ve
    kurulu bileşenler.";
  - aynı durumda, bileşen kayıtları güncel değilken
    (`dashboard.attentionNoneUnchecked`): EN "No certificate or firewall setting
    needs action. The installed components were not checked recently." · TR
    "İşlem gerektiren bir sertifika ya da güvenlik duvarı ayarı yok. Kurulu
    bileşenlere yakın zamanda bakılmadı." Ne yapılacağı, listenin üstündeki
    "Sistem servisleri" kartındadır; kart o durumda bileşen durumunun eski
    olduğunu söyler ve Bileşenler sayfasını gösterir.
  - Bu, daha önce kaldırılan "her şey yolunda" satırı değildir: neyin okunduğunu
    adlandırır ve yakın zamanda kimsenin bakmadığı bileşenler adına konuşmaz.
  - "Kontrol ediliyor" satırı yalnız listede hiçbir şey yokken çizilir. Bir kalem
    listelenmişken başka bir okuma hâlâ sürüyorsa bu, sayının yanında söylenir
    (etiketi kontrol cümlesi olan küçük bir dönen işaret); böylece listede
    belirip sonra kaybolan bir satır olmaz.
  - "Güvenlik duvarı kapalı" ve "Aç" yalnız `enabled: false` taşıyan yanıt için
    vardır. Boolean `enabled` taşımayan ya da Agent'ın `error` alanını taşıyan
    yanıt "okunamadı"dır.

**DNS motoru kartı: yoklama yalnız okur (karar).** Bir DNS motoru değişikliği
izlenirken kart `GET /api/v1/dns/engine` adresini 3 saniyede bir okur (değişiklik
iki dakikadır yeni bir şey kaydetmediyse 15 saniyede bir). Bu kayda kadar aynı
zamanlayıcı, tam işlemin `updated_at` değeri iki dakikalık olduğunda, en çok üç
kez ve bir dakika arayla `POST /api/v1/dns/engine/reconcile` de gönderiyordu. Bu
istek hâlâ gereklidir — çalışanı kaybolmuş, kabul edilmiş bir değişikliğin kayıtlı
kaydını kapatan odur — ama okuma değildir: sunucu değişiklik kilidini alır,
kayıtlı kaydı yeniden yazabilir ve `reconciled_operation` kaydını oturumdaki
yönetici adına denetim günlüğüne yazar. Zamanlayıcı onu, belki kimsenin bakmadığı
bir sekmede, ekran "Salt okunur kontroller 15 saniyede bir sürer" derken
gönderiyordu.

İki yol değerlendirildi: tek bir otomatik denemeyi korumak ve onu burada
"Yoklama ve sayfa yenileme okumadır" kuralının (yukarıda) istisnası olarak
kaydetmek, ya da isteği ekrandaki kişiye vermek. İkincisi seçildi: istek denetim
günlüğünde bir kişiye yazıldığı için o kişinin eylemi olmalıdır; yoklama kuralına
bir istisnayı bu belgenin sonraki her okuyucusu hatırlamak zorunda kalırdı; ve
hiçbir şey kaybedilmez: kilit zaten sahibine sayfada kalmasını söylüyordu,
değişikliğin kendisi de iki yolda da yeniden başlatılmaz.

- Zamanlayıcı isteği sıfır kez gönderir. İstek yalnız bir düğmeden çıkabilir:
  karttaki `Durumu yenile` (önceki gibi, bir değişiklik izlenirken kapalıdır) ve
  "Şimdi kontrol et"; bu, değişiklik takılıyken kilitte, yoklama güvenlik
  sınırında durduktan sonra ise karttadır.
- "Şimdi kontrol et" kilitte yalnız tam işlem (aynı istek ve hedef) iki dakikadır
  yeni bir şey kaydetmediğinde sunulur; aynı anda tek istek; yanıtı yalnız aynı
  istek ve hedefe, yoklamanın kullandığı aynı "değişiklik bitti" denetimiyle
  uygulanır.
- *Güvenlik sınırından sonra.* Yoklama, kesin bir sonuç olmadan 31 dakika ya da
  180 okumadan sonra durur ve kilit bırakılır (ikisi de önceki gibi). Bu kayda
  kadar zamanlayıcı o ana dek kendi isteklerini göndermiş olurdu; hiçbiri
  gönderilmediğinde ve değişiklik çözülmemişken `Durumu yenile` kapalıyken,
  arayüzde kayıtlı kaydı kapatabilecek hiçbir şey kalmazdı. Bu yüzden kart, tam
  bir işlem için o durumda aynı "Şimdi kontrol et"i, işlemin ilerlemesinin
  altındaki bir bildirimde sunmayı sürdürür. Bildirim, orada doğru olmayan
  "Otomatik takip 15 saniyede bir devam ediyor" cümlesinin yerini alır. Sayfayı
  yenilemek durumu bir kez okur ve bu bildirime döner.
- Metinler (`web/src/i18n/dnsEngine.ts`). İlk cümle (`dnsEngine.guard.reconcileDue`):
  EN "CelikPanel has not confirmed this change for {minutes} minutes: the server
  has recorded no new step in that time." · TR "CelikPanel bu değişikliği
  {minutes} dakikadır doğrulayamadı: sunucu bu sürede yeni bir adım kaydetmedi."
  Ardından (`dnsEngine.guard.reconcileOffer`):
  - EN: "CelikPanel keeps reading the state every 15 seconds and changes nothing
    by itself. As this server’s administrator you can choose Check now:
    CelikPanel then compares its saved record of this change with what the DNS
    service did, and closes the record if the change has ended. That is logged
    under your account and does not start the change again."
  - TR: "CelikPanel durumu 15 saniyede bir okumayı sürdürüyor ve kendiliğinden
    hiçbir şeyi değiştirmiyor. Bu sunucunun yöneticisi olarak Şimdi kontrol et’i
    seçebilirsiniz: CelikPanel o zaman bu değişikliğin kayıtlı kaydını DNS
    hizmetinin gerçekten yaptığıyla karşılaştırır ve değişiklik bittiyse kaydı
    kapatır. Bu işlem hesabınız adına denetim günlüğüne yazılır ve değişikliği
    yeniden başlatmaz."
  - Düğme (`dnsEngine.guard.checkNow`): EN "Check now" · TR "Şimdi kontrol et".
  - Değişikliği bitirmeyen bir kontrolden sonra ilk cümlenin yerini şunlardan biri
    alır: (`dnsEngine.guard.reconcileChecked`) EN "Checked at {time}: the change
    has not finished yet." · TR "Saat {time} itibarıyla kontrol edildi: değişiklik
    henüz bitmedi."; sunucu reddettiyse sunucunun kendi reddi; ya da
    (`dnsEngine.guard.reconcileUnread`) EN "Checked at {time}, but the DNS state
    could not be read afterwards." · TR "Saat {time} itibarıyla kontrol edildi,
    ancak ardından DNS durumu okunamadı."
  - Güvenlik sınırından sonra, kartta: ilk cümle
    (`dnsEngine.guard.deadlineDue`) EN "CelikPanel has stopped checking this
    change by itself: it reached its safety limit without a final result." · TR
    "CelikPanel bu değişikliği kendiliğinden kontrol etmeyi bıraktı: kesin bir
    sonuç olmadan güvenlik sınırına ulaştı." Ardından
    (`dnsEngine.guard.deadlineOffer`), aynı düğmeyle:
    - EN: "Nothing is changed by itself. As this server’s administrator you can
      choose Check now: CelikPanel then compares its saved record of this change
      with what the DNS service did, and closes the record if the change has
      ended. That is logged under your account and does not start the change
      again."
    - TR: "Kendiliğinden hiçbir şey değiştirilmez. Bu sunucunun yöneticisi olarak
      Şimdi kontrol et’i seçebilirsiniz: CelikPanel o zaman bu değişikliğin
      kayıtlı kaydını DNS hizmetinin gerçekten yaptığıyla karşılaştırır ve
      değişiklik bittiyse kaydı kapatır. Bu işlem hesabınız adına denetim
      günlüğüne yazılır ve değişikliği yeniden başlatmaz."
    - Oradaki bir kontrolden sonra ilk cümlenin yerini kilitteki gibi şunlardan
      biri alır: `reconcileChecked`, sunucunun reddi ya da `reconcileUnread`.
  - `dnsEngine.guard.stalled` ("İki dakikadır yeni kalıcı sunucu aşaması
    kaydedilmedi…") kaldırıldı; tam işlemin bilinmediği durum
    (`dnsEngine.guard.awaitingStalled`) değişmedi ve kilitte de güvenlik
    sınırından sonra da bir şey sunmaz.

**Ortak parçalarda dört düzeltme.**

- *Biten bir bileşen işlemi, gösterileni yeniler.* Kayıtlı bileşen kayıtlarını
  paylaşılan okumayla gösteren ekranlar bir yanıtı yarım dakika tutar; bu yüzden
  kurulumdan hemen sonra açılan PostgreSQL ya da MariaDB sayfası 30 saniyeye kadar
  önceki taramayı listeleyebiliyordu. İzleyici artık o okumaya, işlem başına bir
  kez, bitişi taze taramayla doğruladığı noktada (başarı ya da hata) yeniden
  okumasını söyler; bunu hiçbir zaman bir yoklamadan yapmaz. Böyle bir ekran açık
  değilse istek gönderilmez (`web/src/lib/remote.ts` içinde `refreshRemote`).
- *Panodaki ilgi listesi yerini korur* (yukarıda): kalem yokken ya da bir kalem
  varken altındaki hiçbir şey yer değiştirmez; sonraki her kalem kendi satırını
  ekler. Kutu, tek bir yanıtın içine koyabileceği en uzun şeyin yüksekliğini
  ayırır: geniş ekranda bir satır, `lg` genişliğinin altında üç satır; orada
  sakin satır üç, tek kalem iki satıra sarar. İçindeki, kutuda ortalanır. İki
  sakin satır bu yüzden aynı uzunluktadır.
- *Kapalı düğme artık bir eylem çağrısına benzemez.* Çukur bir dolgusu vardı; koyu
  temada bu, açık renkli etkin düğmenin yanında açık etiketli lacivert bir bloktu.
  Kullanılamayan denetimin her iki temada da dolgusu yoktur ve çerçevesi kendi
  etiketinin renginde kesik çizgilidir; çalışan düğme çukur dolguyu ve dönen
  işaretini korur.
- *Yapılandırma düzenleyicisinin kartı yüksekliğini korur.* Dosya okunurken tek
  satır, sonra düzenleyicinin tamamıydı; ham dosyaları, genel bakışı ve günlüğü
  aşağı itiyordu. Kart her durumda pencerenin kalanını alır; altındaki her şey
  önce de sonra da görünür alanın altında başlar. Ham dosya düzenleyicisi metin
  alanının yüksekliğini ayırır; MariaDB sayfasında dosya seçicinin yeri dosyalar
  okunurken ayrılır.

Ayrıca: izlenen bir işlemin kilidi telefonun penceresinden uzun olabilir (üç
ayrıntı, bir ileti ve bir eylem). Hiçbir yere kaymıyor, üstten ve alttan
kesiliyordu; artık pencerenin içinde kayar.

**Geri gelmesi nasıl önleniyor.** `web/tests/remote-state-mounted-batch3.test.mjs`
bu ekranların her birini, her okuması dört biçimde alıkonmuş hâlde bağlar ve
olumsuz metin çizilmemesini, değiştiren hiçbir denetimin açık olmamasını ve Tekrar
dene'nin yalnız okuma göndermesini şart koşar; paylaşılan okumanın tek yenilemesini
de tutar. Aynı dosya panoyu, ilgi listesinin dört okumasının her biri aynı dört
biçimde alıkonmuş hâlde ve güvenlik duvarı Agent'ın hatasını 200 ile yanıtlarken
bağlar: "Güvenlik duvarı kapalı" yok, "Aç" yok, sakin satır yok, okuma dışında
istek yok ve Tekrar dene yalnız okunamayanı okur; listelenmiş bir kalem varken
süren başka bir okumanın "kontrol ediliyor" satırı çizmediğini ve güvenlik
duvarı çözücüsünün "kapalı"yı yalnız Agent'ın gönderdiği bir boolean olarak kabul
ettiğini tutar. Gerçek DNS motoru kartını da, dört dakikadır hiçbir şey
kaydetmemiş bir değişikliğin üzerinde bağlar: yoklama okuma dışında istek
göndermez (kartın bu kayıttan önceki hâli burada, aralarında uzlaştırma isteğiyle
başarısız olur), iki kez basılan "Şimdi kontrol et" tek istek gönderir, ret
kilitte söylenir ve güvenlik sınırından sonra kilit bırakılır, döngü artık okumaz
ve karttaki bildirim o tek isteği gönderir. `component-operation-setup-scan-runtime`, "doğrulanmış bitişte bir
kez, doğrulanmamış bitişte hiç" kuralını tutar. `external-operation-lock-contract`
otomatik uzlaştırma isteğinin sınırlarını sabitliyordu; artık yoklama döngüsünde
böyle bir istek ve `fetch` bulunmadığını, isteğin tam iki çağrı yeri olduğunu
(`Durumu yenile` ve sahibin kontrolü), sahibin kontrolüne yalnız iki düğmeden
ulaşıldığını (kilit ve güvenlik sınırından sonra kart), tam bir işlem için
güvenlik sınırında sunulmaya devam ettiğini ve tek uçuşlu, tam işleme bağlı ve
değişiklik isteğine ulaşamaz olduğunu sabitler. `dashboard-truth-contract`, sakin
satırın koşullarını ve hiçbir yanıtın çözülmeden güvenlik duvarı durumu olarak
saklanmadığını tutar. `button-disabled-contrast-contract`, kapalı etiketi bir
düğmenin durduğu her yüzeyde, her palette ölçer (önceden: tek yüzeyde); etkin
birincil dolguyu da aynı yüzeylere karşı ölçer: ürünün açık ve koyu temalarında
en az 3:1 (ölçülen en düşük 7,8:1), taklit görünümlerde bugünkü 2,87:1'den düşük
değil; orada farkı kesik çizgili çerçeve taşır. Mandal: bu partiden önce 47
dosyada 20 / 36 / 10 / 71 / 23, sonra 40 dosyada 8 / 28 / 10 / 57 / 19.

**Yapılmayanlar.**

- 40 dosya hâlâ eski biçimde okuyor. Bu partide ulaşılmadı ve
  dokunulmadı: bileşenler listesi ve işlem izleyicinin kendi okumaları
  (`ServiceList`, `ComponentOperation`), `ServiceShell`, panonun kalanı (sistem
  değerleri, denetim izi, bileşen özetinin kendi okuması, hazırlık yoklaması),
  listedeki her alan adı bölümü (`HostingTypePanel`, `DomainDNSManager`, yedekler,
  uygulamalar, günlükler, PHP, genel, sertifika özeti, posta kimlik doğrulaması),
  iki veritabanı penceresi, sunucu kurulum sihirbazı, mağaza ve eklenti sayfaları,
  VPN, denetim günlüğü, ekip sayfası, güvenlik denetimi kartı ve panel veritabanı
  sayfası.
- Bilerek bırakılanlar ve onları sabitleyen sözleşme: `Layout`
  (`layout-server-identity-contract` içindeki tek sınırlı
  `fetch('/api/v1/panel/version'` ve yalnız-yönetici sıfırlaması;
  `component-inventory-contract` içindeki
  `api.getServices().then(publishComponentCensus)`), `SystemUpdateOperation`
  (`system-update-outcome` ve `panel-update-ui-contract` içindeki tam durum ve
  kurtarma okumaları; bilerek güvenli tarafta kalır), `PanelUpdateCard`
  (`panel-update-card-mounted`), `DNSEngineCard`'ın kendi durum okuması
  (`dns-engine-ui-contract`), panonun hazırlık okuması
  (`dashboard-firewall-confirmation-contract`). Erişim kapıları
  (`usePanelSession`, `LicenseOnboarding`, `RecoveryAccess`, `App`) bilinmeyeni
  olumsuzdan zaten ayırır; yalnız ham okumaları sayılır ve bunlar taşınmadı.
- Pano listede kalır (1 / 3 / 0 / 4): denetim izi, sistem değerleri, hazırlık
  yoklaması ve bileşen kayıtlarını kendi okuması paylaşılan katmanda değildir.
  Bileşenler sayfasının güvenlik duvarı paneli (`ServiceList`) hâlâ
  `GET /api/v1/firewall` adresini kendi okur.
- DNS motoru kartının `dnsEngine.guard.deadline` cümlesi, güvenlik sınırında,
  kilidin bırakıldığı anda kilide yazılır; bu yüzden onu kimse görmez. Bu kayıttan
  önce de böyleydi ve değişmedi. Sahibin orada artık gördüğü, yalnız tam bir
  işlem için, karttaki "Şimdi kontrol et" bildirimidir (yukarıda).
- DNS motoru kartının bağlanan testi, kartın ilk yoklamasını (durum okunduktan
  yarım saniye sonra; eski zamanlayıcı isteğini orada gönderiyordu) ve sahibin
  kontrolünü kapsar; sonraki, daha yavaş yoklamalar kaynak sözleşmesiyle tutulur
  ve tarayıcı çalıştırmasında sayıldı.
- php.ini düzenleyicisi hâlâ yalnız İngilizcedir ve kaydı hâlâ tarayıcı
  pencereleriyle onaylayıp bildirir; yalnız okuması değiştirildi.
- İlgi listesi şu durumlarda hâlâ yer değiştirir: ikinci bir kalem geldiğinde
  (sonraki her kalem kendi satırını ekler), bir okuma başarısız olduğunda
  (bildirim bir satırdan uzundur) ve 390 px'ten dar bir ekranda sakin satır
  dördüncü bir satıra ihtiyaç duyarsa.
- Kontrol edilemedi bildirimi okumanın neden başarısız olduğunu hâlâ söylemez.
- Gerçek sunucuda doğrulanmadı; sahte sunucuya karşı tek bir Chrome.

**Bu grubun tarayıcı incelemesi (2026-10-09).** Kurulu, gerçek bir Chrome'da,
yerel sahte sunucuya karşı (`web/tools/browser-inspect`, `servicepages`,
`attention`, `dnssettings`, `dnsreconcile`, `editorheight` senaryoları; önceki
grupların her senaryosu aynı derlemede yeniden çalıştırıldı): masaüstü 1440×900
ve telefon 390×844, Türkçe ve İngilizce, açık ve koyu.

- *Sekiz yapılandırmanın hepsinde ölçüldü.* Bu ekranların hiçbir "kontrol
  ediliyor" ya da "kontrol edilemedi" durumunda ekranda olumsuz bir cümle yoktu.
  Panodaki ilgi listesinin altında, "kontrol ediliyor" durumu ile yerleşmiş
  sayfa arasında hiçbir şey yer değiştirmedi (0 px): bileşenler güncelken
  listelenecek bir şey yokken, güncel değilken listelenecek bir şey yokken, bir
  kalem varken ve bir kalem listelenmişken başka bir okuma sürerken. DNS
  değişikliği takılıyken, sayfaya kimse dokunmadan geçen 34 saniyede (iki yavaş
  yoklama) hiçbir uzlaştırma isteği gelmedi; her "Şimdi kontrol et" tam bir istek
  gönderdi. Güvenlik sınırından sonra kilit çizilmedi, 20 saniyede hiçbir türden
  istek gelmedi, kart takibin sürdüğünü söylemedi ve karttaki her "Şimdi kontrol
  et" tam bir istek gönderdi. Yapılandırma düzenleyicisinin kartının altındaki
  ham dosyalar, dosya okunurken de sonra da görünür alanın altında başladı.
  Dovecot değerlerinin altındakiler, yanıt geldiğinde yer değiştirmedi. Fail2ban,
  Nginx ve PHP sayfalarında sekme içeriğinin altında bir şey durmaz; bu yüzden
  yeri ölçülebilecek bir şey yoktu.
- *Bakarak ya da ölçerek bulundu ve düzeltildi.*
  - Telefonda ilgi listesi geldiğinde sayfayı aşağı itiyordu: listelenecek bir
    şey yokken 24 px, bir kalem varken 12 px; çünkü bir satır ayrılmıştı, oysa
    sakin satır orada üç, bir kalem iki satır tutar. Kutu artık `lg`
    genişliğinin altında üç satır ayırır; güncel olmayan bileşenler için olan ve
    yaklaşık iki kat uzun olan sakin satır da ötekinin uzunluğuna kısaltıldı.
  - Panoda, DNS ayarlarında ve php.ini sekmesinde "kontrol edilemedi" bildirimi
    bölümün kendi kutusunun içindeydi, kutu içinde kutu; üçünde de tek başına
    duruyor.
  - DNS değişikliğinin kilidi, bileşen izleyicinin "bağlantı kesildi" simgesini
    ve "Sayfayı yenile"yi çiziyordu. Bu, sahte sunucudandı: izleyicinin etkin
    bileşen işlemi okumasını yalın bir `null` ile yanıtlıyordu; oysa Panel her
    zaman `{"operation": null}` yanıtlar (`cmd/panel/service_operations.go`).
    Sahte sunucu artık zarfı yanıtlıyor; kilit dikkat işaretini gösteriyor ve
    yenileme düğmesi çizmiyor.
  - Bir kontrolden sonra kilit "…değişiklik henüz bitmedi. Durumu 15 saniyede bir
    okumayı sürdürüyor…" diyordu; ikinci cümle artık CelikPanel'i adlandırıyor.
  - Güvenlik sınırından sonra kart, durduğunu söyleyen bildirimin yanında
    "Otomatik güncelleniyor" gösteriyordu; rozet orada çizilmiyor.
  - DNS değişikliğinin kilidi telefon penceresinden uzun olabiliyor ve üstten,
    alttan kesiliyordu; artık pencerenin içinde kayıyor.
  - Yeni Türkçe cümleler, sayfanın "hapishane" dediği yerde "hapis" diyordu.
  - Senaryoların iki değeri ölçüm değildi: Fail2ban, Nginx ve PHP sayfalarındaki
    "0 px" sayfanın kendi yeriydi, Dovecot ölçümü de İngilizcede hiçbir öğe
    bulamıyordu. Senaryolar artık yalnız bulunan bir öğe için değer kaydeder,
    bulunamazsa başarısız olur.
- *Görüldü ve değiştirilmedi.* Telefonda Fail2ban tabloları yana kayar; "Yasağı
  kaldır" tablo kaydırılana dek ekran dışındadır (Alan Adları ve Veritabanları
  tablolarındaki gibi). Kilidin iletisi uzundur ve tamamı dikkat renginde
  yazılır; bu, kaplamanın mevcut biçimidir. Panodaki "Son etkinlik", boş bir iz
  için "–" gösterir (taşınmadı). Bir yasak kaldırıldıktan sonra liste yeniden
  okunamazsa, bildirim balonu "IP yasağı kaldırıldı" derken önceki listenin
  üstündeki ortak bildirim "Hiçbir şey değiştirilmedi" der: o cümle, Veritabanları
  sayfasında bir silmeden sonra olduğu gibi, okumadan söz eder.
- *Kapsanmadı.* Gerçek sunucu; Safari, Firefox, ekran okuyucu, dokunmatik cihaz;
  taklit görünümler; yönetici dışındaki roller. PowerDNS sayfasının dosya
  listesinin okunurken ya da okunamadığındaki hâli ve php.ini düzenleyicisinin
  kaydı yalnız bağlanan testle kapsanır. Bu kaydın "önce" değerleri (118 px,
  telefonda 158 px) yeniden ölçülmedi.

#### Dördüncü parti: bir alan adının panelleri, yanıtı gelmeyen değişiklik, telefonda satır eylemleri (2026-10-09)

Bileşen testleri ve yerel bir sahte sunucuya karşı tarayıcı incelemesiyle kaynak
durumu; gerçek sistem çalıştırması ve kurulu sunucu yok. Aynı kural, bir alan
adının sayfasındaki panellere uygulandı. Okumaların ve yiten yanıtların
arayüzde nasıl gösterildiğini değiştirir; API, saklanan kayıt, erişim kapısı ya
da yaşam döngüsü değişmez ve hiçbir kabul işi kapanmaz. Sunucu gerektirenler
aşağıda "Sunucu gerektirir" başlığındadır.

**Bu paneller önce ne gösteriyordu.**

- *DNS kayıtları.* Durumu okunamayan bölge, "DNS bölgesi durumu denetlenemedi"
  başlıklı bir boş durumdu. Kayıtların başarısız okuması bir bildirim balonu ve
  kırmızı bir satır çıkarıyor, altında "Henüz kayıt yok" ve "Toplam 0 öğe"
  kalıyordu. DNSSEC kartı okuması yanıtlanana dek yoktu, sonra kayıtları aşağı
  itiyordu; `secured` taşımayan yanıt "imzasız" diye okunuyor ve "Bölgeyi
  imzala" sunuluyordu.
- *Barındırma tipi.* Başarısız okuma, sonu gelmeyen bir dönen simge bırakıyordu.
  Node.js sürümleri, okuması başarısız olunca boş listeydi. Canlı uygulama
  paneli kayıtlı tip için değil formda seçilen tip için çiziliyordu; yalnız
  seçilmiş bir tip, var olmayan bir uygulamayı yokluyordu (beş saniyede bir
  409). İki okuması yutuluyordu: yanıt yokken "Durdu" ve "Henüz günlük satırı
  yok", Başlat da sunuluyordu.
- *PHP.* Kırmızıyla "PHP ayarları yüklenemedi"; yeniden okuma yolu yoktu. Havuz
  formu eksik her değeri bir varsayılanla (`dynamic`, 5, 2, 1, 3, `www-data`)
  dolduruyor ve kaydediyordu.
- *Genel ayarlar.* Kırmızıyla "Ayarlar yüklenemedi"; Tekrar dene yoktu.
- *Uygulamalar.* Başarısız okuma için "Kullanılabilir uygulama yok".
- *Genel bakıştaki sertifika kartı.* "Durum alınamadı … Ayrıntılar için SSL/TLS
  bölümünü açın"; Tekrar dene yoktu, bunun "sertifika yok" demek olmadığı da
  söylenmiyordu.
- *Posta kimlik doğrulaması.* Başarısız okumadan sonra sonu gelmeyen dönen
  simge. Ekranın bilmediği bir kayıt durumu "Eksik" diye çiziliyordu.
- *Loglar.* Otomatik yenileme açıkken reddedilen her yoklama, beş saniyede bir
  hata balonu çıkarıyor; her yoklama satırların yerine sayfa boyu bir dönen
  simge koyuyordu. Başarısız ilk okuma için "Günlük satırı yok".
- *Yedekler.* Başarısız okuma ya da gövdesi boş yanıt için "Henüz yedek yok".
  Veritabanları okunamadığında "Bağlı veritabanı yok" ve "Web sitesi dosyaları
  + 0 veritabanı".
- *Zamanlanmış görevler.* Listeyi taşımayan yanıt "Zamanlanmış görev yok"tu.
- *Bu panellerdeki her değişiklik.* Bağlantı koptuğunda genel bir hata balonu;
  denetim de hemen yeniden açılıyordu.

**Üç durumun ötesinde şimdi ne yapıyorlar.**

- *Yanıtı gelmeyen değişiklik* (`web/src/lib/lostAnswer.ts`,
  `web/src/components/ui.tsx` içinde `ResultUnknown`). Panelin bu ekranlardaki
  değişiklikleri, sunucunun sakladığı bir kimlik taşımaz; ikinci istek ikinci
  değişikliktir. Yanıt gelmediğinde (bağlantı biter, bir geçit Panelin JSON'u
  olmayan bir sayfayla 408, 502, 503 ya da 504 yanıtlar, ya da kabul edilmiş
  yanıt okunamaz) ekran:
  1. hiçbir şeyi ikinci kez göndermez;
  2. değişikliğin etkilediği şeyi yeniden okur, yalnızca okur;
  3. değiştiren ya da kaldıran her denetimi **o okuma yanıtlanana dek kapalı**
     tutar (`holding`);
  4. değişikliğin istendiği yerde, görünür alana kaydırılmış, dikkat renginde
     tek bir bildirim gösterir (başarısız olduğu bilinen bir şey yoktur). Okuma
     yoldayken bunu söyler; okuma yanıtlanınca durumun saat kaçta yeniden
     okunduğunu söyler ve **kişi kapatana** ya da sonraki bir değişiklik
     yanıtlanana **dek durur**. "Tekrar kontrol et" yalnız okur. Okuma da
     başarısız olursa bildirim bunu söyler ve denetimler kapalı kalır.
  Panelin kendi gönderdiği ret, durum kodu ne olursa olsun, sunucunun
  gerekçesidir ve eskisi gibi gösterilir. Bu bir tekrar güvenliği değildir:
  değişikliğin yapılıp yapılmadığına, yeniden okunan duruma bakan kişi karar
  verir. O durum sonucu gösteremiyorsa (uygulama kurulumu), bildirim nereye
  bakılacağını söyler.
- *Durum yeniden okunduktan sonra formun yaptığı* (2026-10-10;
  `web/src/lib/lostAnswer.ts` içinde `Question`). Yanıtı yiten form, yeniden
  okunan duruma tek bir soru sorar ve yalnızca bakar: değişikliği gösteriyor mu?
  - Gösteriyor: değişikliği gönderen form kapatılır ya da boşaltılır; böylece
    aynı kayıt tek tıkla ikinci kez kaydedilemez. Bildirim değişikliğin
    kaydedildiğini söyler; dikkat yüzeyinde değil, onay işaretli sade yüzeyde
    durur, yalnız "Kapat" sunar ve kişi kapatana dek kalır.
  - Göstermiyor: yazılan yerinde kalır (daha yeni yanıtın formu yeniden kurduğu
    yerde gönderilen değerler onun üzerine geri konur), değiştiren denetimler
    geri gelir ve bildirim, durumun değişikliği göstermediğini, bu yüzden
    kaydedildiğinin bilinmediğini ve bağlantı koptuğunda hâlâ çalışan bir
    sunucunun işi sonradan bitirebileceğini söyler. "Tekrar kontrol et" okur ve
    soruyu yeniden sorar; değişikliği gösteren sonraki okuma formu kapatır.
  - Söyleyemiyor (listeye bu kayıt olduğu açık olmayan bir satır eklenmiş ya da
    ayarlar ne gönderilenler ne öncekiler): yukarıdaki dört adımın bildirimi
    gösterilir ve kişi bakar.
  - Durum yeniden okunamadı: yukarıdaki gibi denetimler kapalı kalır.
  Soru soran formlar: eklenen DNS kaydı (listede yeni olan, türü ve sahip adı
  gönderilenle aynı, değeri sunucunun yeniden yazdıkları dışında gönderilen
  değer olan satır: tam sahip adı, tırnaklar, sondaki nokta, büyük-küçük harf);
  eklenen takma ad (önceden olmadığı listede takma ad); genel ayarların
  yönlendirme anahtarı; barındırma tipinin Uygula'sı (tip ve kendi alanları
  gönderildiği gibi: kaydedildi; öncekiyle aynı ayarlar: görünmüyor); PHP
  sürümü; PHP havuzu. Yazılan bir formu olmadığı ya da durum sonucunu
  gösteremediği için soru sormayan değişiklikler: kayıt silme, bölge yayımlama,
  imzalama, başlat, durdur ve yeniden başlat, uygulama kurma, posta kaydı
  yayımlama, DKIM anahtarı, günlük temizleme ve yedek oluşturma, geri yükleme
  ya da silme. Bunlarda kişi, eskisi gibi, yeniden okunan duruma bakar.
- *Alan adının altındaki sertifika satırı* (2026-10-10, `DomainDetail`).
  Başlığın altındaki şerit sertifikayı kendisi okur; genel bakış kartının ve
  SSL/TLS sekmesinin adresinden ve çözücüsüyle, böylece üçü tek isteği paylaşır.
  Kontrol ediliyor, kontrol edilemedi (sözün yanında yalnız okuyan "Tekrar dene"
  ile) ya da sunucunun söylediği olur. Başarısız bir yenilemeden sonra önceki
  yanıt kalır. Bundan önce kartın ya da sekmenin bildirmesini bekliyor,
  başarısız okumadan sonra ve ikisini de takmayan her sekmede sonsuza dek
  "durum kontrol ediliyor" diyordu.
  SSL/TLS sekmesi için bir sonucu, 2026-10-10 tarayıcı çalıştırmasında ölçüldü:
  şerit her sekmede ekrandadır; bu yüzden alan adının sayfası açıkken yanıt hiç
  bırakılmaz ve sekme artık hiçten başlamaz. Sayfanın ilk okuması yoldayken
  açılırsa kontrol satırını gösterir ve o okumayı paylaşır. Sonra açılırsa
  sayfanın zaten tuttuğu yanıtı önceki yanıt olarak gösterir, yeniden okuduğunu
  söyler, denetimlerini kapalı tutar ve bir okuma daha gönderir; bu değişiklikten
  önce yalnız kontrol satırını gösteriyor ve tek okumayı o gönderiyordu.
  Sunucunun söylemediği hiçbir olumsuz gösterilmez ve o okumadan sonra sertifika
  isteği yine çalışır. Birkaç saniyelik bir yanıtın sekme açıldığında yeniden
  okunup okunmayacağına burada karar verilmedi.
- *Yinelenen okuma* (`web/src/lib/remote.ts` içinde `useRefreshEvery`; loglar
  kendi zamanlayıcısını korur). Bir tik yalnız okur; son okuma yoldayken okumaz.
  Başarısız okuma hiçbir şey yükseltmez: önceki yanıt, ne zaman okunduğunu
  söyleyen tek bir bildirimin altında kalır, bir şeyi değiştiren denetimler
  kapalıdır ve sonraki tik yeniden sorar; iyi bir yanıt bildirimi kaldırır.
- *DNS.* "Bölge yok" diyen tek yanıt, sunucunun bu alan adının bölgesi için
  verdiği 404'tür; diğer her ret "kontrol edilemedi"dir. Kayıtlar yalnız
  sunucunun var dediği bölge için okunur. DNSSEC kartı kontrol ederken yerinde
  ve olağan yanıtının kaplayacağı yerle durur; alttaki kayıtlar oynamaz.
- *Barındırma tipi.* Form sunucunun gönderdiğini gösterir; yazılan, üzerine
  yazıldığı yanıtın yanında tutulur ve Uygula'dan sonra form yine kayıtlı
  ayarları gösterir. Canlı uygulama paneli kayıtlı tipi izler.
- *PHP.* Havuz formu sunucunun değerlerini tutar, varsayılan tutmaz; daha yeni
  yanıt formu yeniden kurar.
- *Adres başına tek çözücü.* Bir alan adının veritabanları, Veritabanları ve
  Yedekler sekmeleri için `web/src/lib/domainDatabases.ts` içinde çözülür;
  sertifika kartı SSL/TLS sekmesinin çözücüsünü kullanır.
- *Telefonda satır eylemleri.* Veri tablosu telefondan geniş olabilir ve
  çerçevesinde yana kayar; satırın yapabildiği şey ekran dışında kalan kısım
  olamaz. Satır eylemlerini tutan hücre ve üstündeki başlık hücresi
  `row-actions` sınıfını taşır (`web/src/index.css`): `sm` genişliğinin (640 px)
  altında, diğer sütunlar altından kayarken tablonun son kenarında, opak ve ön
  kenarında ince bir çizgiyle durur. Geniş ekranlar değişmez. DNS kayıtlarına,
  Alan Adları listesine, Veritabanları sayfasına (iki tablo) ve Fail2ban'ın
  banlı adreslerine uygulandı. DNS kayıtlarında değer sütununun ayrıca bir en az
  genişliği vardır: yokken telefon uzun bir değeri satır başına tek karaktere
  sıkıştırıyor, tek satır iki ekran boyu oluyordu. O hücredeki adı olan eylem
  sözlerini tek satırda tutar (2026-10-10; "Yasağı kaldır" 390 px'te ikiye
  bölünüyordu).

**Metinler.** Anahtarlar `web/src/i18n` altındadır (`common.*` kabuk
kataloğunda, kalanı `screens` içinde).

- *Yanıtı gelmeyen değişiklik, durum yeniden okunduktan sonra*
  - `common.resultUnknownRead`
    - EN: "The connection dropped before the answer arrived, so it is not known
      whether the change was made. Nothing was sent a second time. What is shown
      here was read again at {time}: check it before repeating the action."
    - TR: "Yanıt gelmeden bağlantı koptu; bu yüzden değişikliğin yapılıp
      yapılmadığı bilinmiyor. Hiçbir şey ikinci kez gönderilmedi. Burada
      gösterilen, saat {time} itibarıyla yeniden okundu: işlemi yinelemeden
      önce ona bakın."
  - O okuma yoldayken: `common.resultUnknown` (ikinci parti).
- *Aynısı, form sorduğunda ve yeniden okunan durum değişikliği gösterdiğinde*
  (2026-10-10)
  - `common.resultUnknownMade`
    - EN: "The connection dropped before the answer arrived, and nothing was
      sent a second time. What was read again at {time} shows the change, so it
      was saved. Nothing needs to be sent again."
    - TR: "Yanıt gelmeden bağlantı koptu ve hiçbir şey ikinci kez gönderilmedi.
      Saat {time} itibarıyla yeniden okunan durum değişikliği gösteriyor; yani
      kaydedildi. Hiçbir şeyin yeniden gönderilmesi gerekmiyor."
- *Aynısı, değişikliği göstermediğinde* (2026-10-10)
  - `common.resultUnknownNotMade`
    - EN: "The connection dropped before the answer arrived, and nothing was
      sent a second time. What was read again at {time} does not show the
      change, so it is not known to have been saved. What you entered is still
      here. If the server was still working when the connection dropped, the
      change can appear later: check again before sending it a second time."
    - TR: "Yanıt gelmeden bağlantı koptu ve hiçbir şey ikinci kez gönderilmedi.
      Saat {time} itibarıyla yeniden okunan durum değişikliği göstermiyor; bu
      yüzden kaydedildiği bilinmiyor. Girdikleriniz hâlâ burada. Bağlantı
      koptuğunda sunucu hâlâ çalışıyorduysa değişiklik sonradan görünebilir:
      ikinci kez göndermeden önce tekrar kontrol edin."
- *Aynısı, durum da yeniden okunamadığında*
  - `common.resultUnknownUnread`
    - EN: "The connection dropped before the answer arrived, so it is not known
      whether the change was made. Nothing was sent a second time. The current
      state could not be read again either, so controls that change or remove
      something stay off. Check again."
    - TR: "Yanıt gelmeden bağlantı koptu; bu yüzden değişikliğin yapılıp
      yapılmadığı bilinmiyor. Hiçbir şey ikinci kez gönderilmedi. Güncel durum
      da yeniden okunamadı; bu yüzden bir şeyi değiştiren ya da kaldıran
      denetimler kapalı kalıyor. Tekrar kontrol edin."
  - `common.checkAgain`: EN "Check again" · TR "Tekrar kontrol et". Bildirimi
    kapatmak: `common.close`, EN "Close" · TR "Kapat".
- *DNS: bölge okunamadı*
  - `dns.zoneUnknown`
    - EN: "The DNS zone of {name} could not be read from the server, so its
      records are not shown as current. This does not mean the zone is missing.
      Nothing was changed. Try again."
    - TR: "{name} alan adının DNS bölgesi sunucudan okunamadı; bu yüzden
      kayıtları güncel diye gösterilmiyor. Bu, bölgenin olmadığı anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *DNS: kayıtlar*
  - `dns.records.checking`: EN "Reading this domain’s DNS records…" · TR "Bu alan
    adının DNS kayıtları okunuyor…"
  - `dns.records.unknown`
    - EN: "The DNS records of this domain could not be read from the server, so
      the list is not shown. This does not mean there are none. Nothing was
      changed. Try again."
    - TR: "Bu alan adının DNS kayıtları sunucudan okunamadı; bu yüzden liste
      gösterilmiyor. Bu, kayıt olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *DNS: bölgenin imzalı olup olmadığı*
  - `dnssec.checking`: EN "Checking whether this zone is signed…" · TR "Bu
    bölgenin imzalı olup olmadığı kontrol ediliyor…"
  - `dnssec.unknown`
    - EN: "CelikPanel could not check whether this zone is signed, so signing is
      not offered. This does not mean it is unsigned. Nothing was changed. Try
      again."
    - TR: "CelikPanel bu bölgenin imzalı olup olmadığını kontrol edemedi; bu
      yüzden imzalama sunulmuyor. Bu, imzasız olduğu anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *Barındırma tipi*
  - `hosting.checking`: EN "Reading this domain’s hosting settings…" · TR "Bu
    alan adının barındırma ayarları okunuyor…"
  - `hosting.unknown`
    - EN: "The hosting settings of this domain could not be read from the
      server, so they are not shown and cannot be changed here yet. Nothing was
      changed. Try again."
    - TR: "Bu alan adının barındırma ayarları sunucudan okunamadı; bu yüzden
      gösterilmiyor ve şimdilik buradan değiştirilemiyor. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `hosting.nodeChecking`: EN "Checking which Node.js versions are installed…"
    · TR "Kurulu Node.js sürümleri kontrol ediliyor…"
  - `hosting.nodeUnknown`
    - EN: "The installed Node.js versions could not be checked, so only the
      saved version is listed. Nothing was changed. Try again."
    - TR: "Kurulu Node.js sürümleri kontrol edilemedi; bu yüzden yalnız kayıtlı
      sürüm listeleniyor. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Barındırma tipi: canlı uygulama*
  - `hosting.app.checking`: EN "Reading the application’s state…" · TR
    "Uygulamanın durumu okunuyor…"
  - `hosting.app.logsChecking`: EN "Reading the application’s logs…" · TR
    "Uygulamanın günlükleri okunuyor…"
  - `hosting.app.unknown`
    - EN: "The state and the logs of this application could not be read from
      the server. This does not mean it has stopped. Nothing was changed; start,
      stop and restart are off until the state can be read. CelikPanel keeps
      trying."
    - TR: "Bu uygulamanın durumu ve günlükleri sunucudan okunamadı. Bu,
      uygulamanın durduğu anlamına gelmez. Hiçbir şey değiştirilmedi; başlat,
      durdur ve yeniden başlat, durum okunana dek kapalıdır. CelikPanel denemeyi
      sürdürüyor."
  - `hosting.app.stale` (önceki yanıt hâlâ gösteriliyor)
    - EN: "The application could not be read again just now, so what is shown is
      as it was at {time}. This does not mean it has stopped. Nothing was
      changed; start, stop and restart are off until the state can be read.
      CelikPanel keeps trying."
    - TR: "Uygulama az önce yeniden okunamadı; gösterilen, saat {time}
      itibarıyla olan hâlidir. Bu, uygulamanın durduğu anlamına gelmez. Hiçbir
      şey değiştirilmedi; başlat, durdur ve yeniden başlat, durum okunana dek
      kapalıdır. CelikPanel denemeyi sürdürüyor."
- *PHP*
  - `php.checking`: EN "Reading this domain’s PHP settings…" · TR "Bu alan
    adının PHP ayarları okunuyor…"
  - `php.unknown`
    - EN: "The PHP settings of this domain could not be read from the server, so
      they are not shown and cannot be changed here yet. Nothing was changed.
      Try again."
    - TR: "Bu alan adının PHP ayarları sunucudan okunamadı; bu yüzden
      gösterilmiyor ve şimdilik buradan değiştirilemiyor. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *Genel ayarlar*
  - `general.checking`: EN "Reading this domain’s settings…" · TR "Bu alan
    adının ayarları okunuyor…"
  - `general.unknown`
    - EN: "The settings of this domain could not be read from the server, so
      they are not shown and cannot be changed here yet. This does not mean it
      has no aliases. Nothing was changed. Try again."
    - TR: "Bu alan adının ayarları sunucudan okunamadı; bu yüzden gösterilmiyor
      ve şimdilik buradan değiştirilemiyor. Bu, takma adı olmadığı anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Uygulamalar*
  - `apps.checking`: EN "Reading the applications that can be installed…" · TR
    "Kurulabilecek uygulamalar okunuyor…"
  - `apps.unknown`
    - EN: "The list of applications could not be read from the server, so
      nothing is offered. This does not mean no application is available.
      Nothing was changed. Try again."
    - TR: "Uygulama listesi sunucudan okunamadı; bu yüzden hiçbir şey
      sunulmuyor. Bu, kullanılabilir uygulama olmadığı anlamına gelmez. Hiçbir
      şey değiştirilmedi. Tekrar deneyin."
  - `apps.resultUnknownWhere` (sonucu bilinmeyen bildiriminin altında)
    - EN: "This page cannot show whether the application was installed. Look at
      this domain’s Files and Databases before installing again."
    - TR: "Bu sayfa uygulamanın kurulup kurulmadığını gösteremez. Yeniden
      kurmadan önce bu alan adının Dosyalar ve Veritabanları bölümlerine bakın."
- *Genel bakıştaki sertifika kartı*
  - `domain.overview.ssl.unavailableHint` (yeniden yazıldı; kartta artık Tekrar
    dene de var)
    - EN: "The certificate state could not be read from the server. This does
      not mean there is no certificate. Nothing was changed. Try again."
    - TR: "Sertifika durumu sunucudan okunamadı. Bu, sertifika olmadığı anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `domain.overview.ssl.stale`: EN "This could not be read again just now; it
    is shown as it was at {time}." · TR "Bu, az önce yeniden okunamadı; saat
    {time} itibarıyla olan hâliyle gösteriliyor."
- *Alan adının altındaki sertifika satırı* (2026-10-10)
  - `domain.info.sslUnknown`: EN "Could not be checked" · TR "Kontrol edilemedi",
    yanında `common.retry` ile. Okunurken: `domain.overview.ssl.checking`, EN
    "Checking status" · TR "Durum kontrol ediliyor".
- *Posta kimlik doğrulaması*
  - `mailauth.checking`: EN "Checking this domain’s SPF, DKIM and DMARC records…"
    · TR "Bu alan adının SPF, DKIM ve DMARC kayıtları kontrol ediliyor…"
  - `mailauth.unknown`
    - EN: "The SPF, DKIM and DMARC records of this domain could not be checked,
      so their state is not shown and nothing can be published here yet. This
      does not mean they are missing. Nothing was changed. Try again."
    - TR: "Bu alan adının SPF, DKIM ve DMARC kayıtları kontrol edilemedi; bu
      yüzden durumları gösterilmiyor ve şimdilik buradan hiçbir şey
      yayımlanamıyor. Bu, kayıtların eksik olduğu anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *Loglar*
  - `logs.checking`: EN "Reading the log…" · TR "Günlük okunuyor…"
  - `logs.unknown`
    - EN: "This log could not be read from the server, so no lines are shown.
      This does not mean the log is empty. Nothing was changed. Try again."
    - TR: "Bu günlük sunucudan okunamadı; bu yüzden satır gösterilmiyor. Bu,
      günlüğün boş olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
  - Satırlar ekrandayken başarısız olan yoklama: `common.staleNotice` (birinci
    parti).
- *Yedekler*
  - `backup.checking`: EN "Reading this domain’s backups…" · TR "Bu alan adının
    yedekleri okunuyor…"
  - `backup.unknown`
    - EN: "The backups of this domain could not be read from the server, so the
      list is not shown. This does not mean there are none. Nothing was changed.
      Try again."
    - TR: "Bu alan adının yedekleri sunucudan okunamadı; bu yüzden liste
      gösterilmiyor. Bu, yedek olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `backup.databasesUnknown`
    - EN: "The databases linked to this domain could not be read, so a database
      or full backup cannot be made here yet. This does not mean there are none.
      Nothing was changed; a files backup is still available. Try again."
    - TR: "Bu alan adına bağlı veritabanları okunamadı; bu yüzden şimdilik
      buradan veritabanı yedeği ya da tam yedek alınamıyor. Bu, veritabanı
      olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi; dosya yedeği yine
      alınabilir. Tekrar deneyin."
  - `backup.databasesNotRead` (seçicide): EN "Databases not read" · TR
    "Veritabanları okunamadı"
- Artık hiçbir şey göstermediği için kaldırıldı: `dns.zoneStatusUnavailable`,
  `dns.recordsLoadFailed`, `php.loadFailed`, `general.loadFailed`,
  `backup.databaseLoadError`, `backup.retry`.

**Geri gelmesi nasıl önleniyor.**

- *Mandal.* Bu partiden önce 40 dosyada 8 / 28 / 10 / 57 / 19; sonra 31 dosyada
  8 / 25 / 6 / 42 / 12; yeni tavanlar olarak sabitlendi (2026-10-10'da, ayar
  yazımı düzeltmelerini de taşıyan ağaçta yeniden üretildi: aynı dosyalar ve
  toplamlar). Listeden çıkanlar:
  `DomainDNSManager`, `HostingTypePanel`, `DomainPHPSettings`,
  `DomainGeneralSettings`, `DomainAppsPanel`, `DomainSSLOverviewCard`,
  `MailAuthPanel`, `DomainBackupManager`, `DomainCronManager`.
  `DomainLogsViewer` tek bir ham okumayla, başka hiçbir şeyle listede kalır:
  `TestDomainLogsViewerExposesHonestTimeFilterControlsAndMetadata`
  (`cmd/panel/domain_logs_frontend_test.go`) o dosyada
  `parseDomainLogsResponse(await res.json())` satırını sabitler; okuması aynı üç
  durumu orada kurar.
- *Bağlanan test* (`web/tests/remote-state-mounted-batch4.test.mjs`, 113 durum):
  dört yolla esirgenen on dört okuma; bilinen olumsuzlar; yanıtı iki yolla
  yitirilen (bağlantı biter; bir geçit yanıtlar) on beş değişiklik: tek istek,
  yalnız okuyan yeniden okuma, o yanıtlanana dek kapalı denetimler ve yerinde
  kalan bildirim; Panelin bilinmeyen sonuç olmayan reddi; başarısız yoklamalar
  (tek bildirim, balon yok, yalnız okuma); okuma yoldayken sormayan yoklama;
  sunucunun değerlerini tutan formlar; sayfada çizilmeden tutulan tek yer;
  `row-actions` kuralı ve onu taşıyan tablolar. 2026-10-10'dan beri dosyada 129
  durum var: on beş değişikliğin her biri için yeniden okumadan sonra bildirimin
  söylediği; soru soran altı form, her biri değişikliği gösteren ve göstermeyen
  bir durumla, sunucunun geç bitirdiği kaydı bulan sonraki kontrol, hiçbir şeye
  karar vermeyen yeni satır, sunucunun bir kaydı yeniden yazması ve yeniden
  okunamayan liste; bir crontab'ın okunamamasının altı yolu (aşağıdaki
  2026-10-10 kaydı); ve adı olan satır eylemi için tek satır. Alan adının
  altındaki şeridin durumu `web/tests/remote-state-mounted.test.mjs`
  içindedir: kontrol ediliyor, okumanın başarısız olduğu her yol için genel
  bakışta ve DNS sekmesinde kontrol edilemedi, sertifika için tek istek, yalnız
  okuyan Tekrar dene ve sunucunun "sertifika yok" yanıtı.
- *Sabitlemeler gevşetilmedi, kodla birlikte taşındı*
  (`web/tests/additional-user-domain-ui-contract.test.mjs`): ekip üyesinin PHP
  sürümleri hâlâ bu alan adının kendi yanıtındaki kiracı-güvenli
  `available_versions`'tır ve yalnız o yanıt bilinirken vardır (önce: her
  yüklemeden önce temizlenen durum); PHP paneli yalnız ortak katman üzerinden
  gönderir ve okur; salt-okunur ya da dışarıda yönetilen bölgeye kayıtlarının
  okuması sunulur, onları değiştiren hiçbir şey sunulmaz ve üç DNS değişikliği
  de bu kontrolle başlar; bir alan adının veritabanlarının çözücüsü
  `lib/domainDatabases.ts` içinde sabitlenir, bileşende ikincisi reddedilir.
  İlk bağlanan testin sahte verisi DNSSEC okumasını `{ enabled: false }` ile
  yanıtlıyordu; işleyicinin yazdığı bu değildir, artık `{ secured, ds }`
  yanıtlar.

**Sunucu gerektirir (burada yapılmadı; Go değişmedi).**

- Bu değişikliklerin hiçbirinin 2026-10-09'da istek kimliği yoktu.
  2026-10-10'dan beri elle alınan yedek ve geri yükleme bir kimlik taşır
  (D-029; aşağıdaki o tarihli kayıt). Ötekilerde, sunucu bir kimlik saklayana
  dek, yiten yanıt sonucu yukarıdaki gibi kişiye bırakır:
  `POST /api/v1/domains/{id}/dns/records`,
  `DELETE /api/v1/domains/{id}/dns/records?id=`,
  `POST /api/v1/domains/{id}/dns/zone`, `POST /api/v1/domains/{id}/dnssec` (DNS
  sekmesi); `PUT /api/v1/domains/{id}/hosting`,
  `POST /api/v1/domains/{id}/app/{start|stop|restart}` (Barındırma tipi);
  `POST /api/v1/domains/{id}/php`, `POST /api/v1/domains/{id}/php/pool` (PHP);
  `POST /api/v1/domains/{id}/general`, `POST /api/v1/domains/{id}/aliases`,
  `DELETE /api/v1/domains/{id}/aliases/{alias}` (Genel);
  `POST /api/v1/domains/{id}/apps/install` (Uygulamalar; uygulamanın kurulu olup
  olmadığını gösteren bir okuması da yoktur);
  `POST /api/v1/domains/{id}/mail/auth/apply`,
  `POST /api/v1/domains/{id}/mail/auth/dkim` (Posta kimlik doğrulaması);
  `DELETE /api/v1/domains/{id}/logs/{type}` (Loglar);
  `POST /api/v1/domains/{id}/backups`,
  `POST /api/v1/domains/{id}/backups/restore`,
  `DELETE /api/v1/domains/{id}/backups?name=` (Yedekler). İkinci partiden, hâlâ
  yalnız balon ve yeniden okumayla: `POST`/`PUT`/`DELETE /api/v1/users…`,
  `POST /api/v1/users/{id}/impersonate`, `/api/v1/plans…` (Hesaplar) ve bir alan
  adının Dosyalar değişiklikleri. Henüz taşınmadı ve burada bakılmadı: eklentiler,
  VPN eşleri ve ekip üyeleri (VPN cihazı ekleme 2026-10-10'dan beri bir kimlik
  taşır).
- Tarayıcı bir değişikliği kendiliğinden yeniden gönderebilir. Tarayıcı
  çalıştırmasında, sahte sunucu daha önce bir istek taşımış bağlantıyı yalnızca
  sıfırladığında Chrome `POST`'u sayfa istemeden yeniden gönderdi: tek tık, üç
  varış. Sayfadaki hiçbir şey bunu önleyemez; ikinci varışı zararsız kılan
  yalnız sunucudaki istek kimliğidir.
- Hiç yanıtlanmayan ve hiç başarısız olmayan değişiklik (bağlantı açık kalır)
  denetimini meşgul bırakır; sayfanın bunun için bir süre sınırı yoktur.

**Yapılmadı.**

- 31 dosya hâlâ eski yolla okuyor. Bu partinin kapsamında olup taşınmayanlar:
  `ServiceList` (1 / 1 / 0 / 6 / 1), `ServiceShell` (0 / 2 / 2 / 2 / 2),
  `Layout` (1 / 2 / 0 / 1 / 0), `Dashboard` (1 / 3 / 0 / 4 / 0), `AddonsPage`
  (0 / 0 / 0 / 2 / 3), `StoreCatalogAdmin` (0 / 0 / 0 / 1 / 2), `AuditLogPage`
  (0 / 1 / 0 / 1 / 1), `VPNPage` (0 / 0 / 0 / 0 / 1), `TeamMembersPage`
  (0 / 0 / 0 / 0 / 1), `SystemSQLiteManager` (0 / 2 / 0 / 1 / 1),
  `SecurityAuditCard` (0 / 0 / 0 / 1 / 0), `AddDatabaseModalV2`
  (0 / 0 / 0 / 1 / 0), `DatabaseAccountStrip` (0 / 1 / 0 / 1 / 0). Hiçbiri bu
  partide açılmadı. İkisinin okuyucuları mevcut sözleşme testlerince kaynak
  metni olarak tutulur; taşıma bunları kodla birlikte taşımak zorundadır:
  `Layout`, `layout-server-identity-contract` ile (dosyada tam bir
  `fetch('/api/v1/panel/version'`, sürüm damgasında hiç); `ServiceShell`,
  `component-inventory-contract`, `service-unobserved-state-contract` ve
  `service-shell-install-confirmation-contract` ile.
- İkinci partinin hesap, plan ve dosya değişiklikleri kopan bağlantıya hâlâ
  kendiliğinden kaybolan bir balonla yanıt verir; yerinde kalan bildirime
  taşınmadılar.
- Zamanlanmış görevler liste ve sürüm için kendi durumunu korur; yalnız boş
  listenin kanıtı ve 2026-10-10'dan beri bir crontab'ın neden okunamadığı
  (aşağıdaki o tarihli kayıt) eklendi. Yazmalarına dokunulmadı.
- Geniş ekranda imzalı bölgenin DNS kartı, kontrol ederken kendisine ayrılan
  yerden uzundur (ayrılan yer imzasız kartınkidir); imzalı bölgede kayıtlar bir
  kez yer değiştirir.
- Telefonda satırın üzerine gelme tonu sabit eylem hücresinin altına uzanmaz.
- Kontrol edilemedi bildirimi okumanın neden başarısız olduğunu hâlâ söylemez;
  sunucunun bir nedeni doğruladığı yerler dışında: zamanlanmış görevler ve posta
  kuyruğu (aşağıdaki 2026-10-10 kaydı).
- Yeniden okunan duruma soru sormayan değişiklikler (yukarıda sayıldı) sonucu
  hâlâ kişiye bırakır. O tarihte bu panellerdeki hiçbir değişikliğin istek
  kimliği yoktu; yedek ve geri yüklemenin 2026-10-10'dan beri vardır.
- Gerçek sunucuda doğrulanmadı; sahte sunucuya karşı tek bir Chrome.

**Bu partinin tarayıcı incelemesi (2026-10-09).** Kurulu, gerçek bir Chrome'da,
yerel sahte sunucuya karşı (`web/tools/browser-inspect`; `domaindns`,
`domainhosting`, `domainphp`, `domaingeneral`, `domainapps`, `domainmailauth`,
`domainlogs`, `domainbackups`, `sslcard`, `rowactions` senaryoları; önceki
partilerin bütün senaryoları aynı derlemede yeniden çalıştırıldı): masaüstü
1440×900 ve telefon 390×844, Türkçe ve İngilizce, açık ve koyu. Sekiz
yapılandırmanın her birinde 357 durum, 81'i bu partinin; hiçbir senaryo hata
bildirmedi. Bu partinin bir senaryosu, kaydetmesi gereken durumda kontrol
satırı, bildirim ya da sonucu bilinmeyen bildirimi yoksa, ölçmesi gereken yer
bulunamazsa ya da sayması gereken yoklama çalışmadıysa başarısız olur.

- *Sekiz yapılandırmanın hepsinde ölçüldü.* Bu panellerin hiçbir kontrol ya da
  kontrol edilemedi durumunda ekranda olumsuz cümle yoktu; başarısız hiçbir
  okuma balon çıkarmadı. Yanıtı yitirilen altı değişikliğin her biri (eklenen
  DNS kaydı, bir geçidin yanıtladığı barındırma uygulaması, kaldırılan takma
  ad, kurulan uygulama, yayımlanan posta kaydı, oluşturulan yedek) sayfadan bir
  kez gönderildi ve sahte sunucuya bir kez vardı; bildirimi belirdiğinde
  pencerenin içindeydi, değiştiren denetimler durum yeniden okunana dek
  kapalıydı, "Tekrar kontrol et" yalnız okuma gönderdi ve bildirim yalnız
  "Kapat" ile gitti. Yiten yanıtlardan sonra yeniden okunan durum, sahte
  sunucunun yaptığını gösterdi: yeni kayıt bir kez, takma ad yok, sahte
  sunucunun almadığı yedek için de aynı iki satır. Uygulamanın yoklamaları on
  iki saniye başarısız olurken (dört istek) ve günlüğün otomatik yenilemesi
  reddedilirken (iki istek) tek bildirim vardı, balon yoktu, önceki durum ve
  satırlar kaldı, başlat, durdur, yeniden başlat ve temizle kapalıydı, her
  istek bir okumaydı. İmza durumu geldiğinde kayıt tablosu oynamadı (0 px).
  Sertifika kartının yüksekliği kontrol ile "sertifika yok" arasında değişmedi
  (0 px); kontrol edilemedi cümlesi ve Tekrar dene ile geniş ekranda aynı
  yükseklikte, telefonda 38 px (İngilizce) ya da 80 px (Türkçe) daha uzundur.
  Her satır eylemi, tablo başına kaydırılmışken ekran genişliğinin içinde ve en
  üstteydi: DNS kayıtlarının altısı, bir Alan Adları satırının üçü,
  Veritabanları sayfasının ikisi ve Fail2ban'ın iki "Yasağı kaldır"ı (390 px'te
  yana kayan tablolarda); takma ad ve yedek satırlarının eylemleri de (bunlar
  yana kaymaz). Hiçbir sayfa yana kaymadı.
- *Bakarak ya da ölçerek bulundu ve düzeltildi.*
  - Telefonda uzun değerli bir DNS satırı 1.982 px yüksekliğindeydi: değer
    sütunu satır başına tek karaktere sıkışmıştı. Artık en az genişliği var.
  - Telefonda DNSSEC kartı, sabit bir yükseklik ayrılmışken yanıtı gelince 86 px
    büyüyordu; artık olağan yanıtının yerini tutuyor (0 px).
  - Telefonda sertifika kartı kontrol ile "sertifika yok" arasında 19 px
    büyüyordu; ikinci satırı orada iki satırlık yer tutuyor.
  - Canlı uygulama paneli ekranın altında fotoğraflanıyordu; alan adı
    sekmelerinin ilk ekranı doldurduğu telefonda çoğu durum da öyle. Senaryolar
    artık durumun konusunu önce pencereye getiriyor.
  - Senaryolar Türkçe durdurma düğmesini ("Durdur") olumsuz "Durdu" sayıyordu;
    olumsuzlar tam sözcük olarak eşleştiriliyor.
  - Yalnızca sıfırlanan bağlantıda sahte sunucu tek tıkı üç kez aldı
    (yukarıda); bağlantıyı artık yanıt olmayan baytlarla bitiriyor.
- *2026-10-09'da görüldü, 2026-10-10'da düzeltildi.* Yiten yanıttan sonra
  değişikliği gönderen form yazılanla birlikte açık kalıyordu; liste okunduktan
  sonra aynı kayıt yeniden kaydedilebiliyordu: form artık yeniden okunan duruma
  soruyor (yukarıda). Telefonda "Yasağı kaldır" sabit hücresinde iki satıra
  bölünüyordu: artık tek satır, 107×34 px. Bunun bir bedeli var; iki tarihin
  ekran görüntüleri karşılaştırılarak görüldü, senaryo yakalamadı: Türkçede
  sabit hücre öncekinden yaklaşık 41 px daha geniş ve 390 px'te tam uzunluktaki
  bir IPv6 adresinin sonu (yaklaşık son beş karakteri) artık o hücrenin altında
  kalıyor; oysa önce adresin tamamı sığıyordu. Kesildiğini gösteren bir işaret
  yok; yanındaki hapishane sütunu gibi, tablo yana kaydırılarak görülür.
  İngilizce değişmedi ("Unban" hiç bölünmüyordu). Adresin iki satıra mı
  bölüneceğine, yoksa eylemin telefonda mı kısalacağına burada karar verilmedi.
  Alan adının altındaki şerit,
  sertifika okuması başarılı olmadığı sürece, başarısız olduktan sonra da "SSL:
  durum kontrol ediliyor" diyordu: artık üç durum.
- *Görüldü ve değiştirilmedi.* Posta kimlik doğrulaması yiten yanıttan sonra
  yeniden okunurken önceki "Eksik" bildirimin altında ekranda kalır. Devre dışı
  kart koyu temada soluktur. Node.js sürümü ile port notu arasında, kontrol
  satırı için tutulan bir boş satır vardır. Telefonda alan adının altındaki
  şerit üç satıra bölünür ve satır sonunda bir ayraç bırakır. Telefonda posta
  kuyruğunun tablosu bir adresi sözcüğün içinde böler (ikinci parti). Üç not
  (`DomainDNSManager` ekip üyesi için, `DomainDatabaseManager`, `DomainDetail`)
  temanın tanımlamadığı bir `info` rengini kullanır; bu yüzden yüzeysiz çizilir.
- *2026-10-10'da yeniden çalıştırıldı;* ayar yazımı düzeltmelerini de taşıyan
  ağaçta, `lostforms` ve `sslfact` senaryoları eklenerek ve `cron`, `mailqueue`,
  `dbconfig`, `domaindns`, `domainhosting` ve `rowactions` genişletilerek: sekiz
  yapılandırmanın her birinde 396 durum, 39'u yeni (17'si `lostforms`, 8'i
  `sslfact`, 7'si `mailqueue`, 4'ü `cron`, 2'si `dbconfig`, 1'i `domainssl`);
  önceki 357 durumdan eksik yok. Tam çalıştırma, sekizinde de aynı olan tek bir
  hata bildirdi: `domainssl`, sertifika isteğinden sonra beklemeyi bıraktı.
  Neden sahte sunucu değil, bu değişiklikti: şerit sertifikayı okuduğu için
  SSL/TLS sekmesi sayfanın zaten tuttuğu bir yanıtın üzerine açılır (yukarıda)
  ve senaryo "Sertifika al" düğmesine sekmenin denetimleri kapalıyken bastı;
  hiçbir şey gönderilmedi ve durumlarından üçü artık doğru olmayan adlarla
  kaydedildi. Senaryo düzeltildi (artık sekmeyi iki yolla da açar, yalnız
  etkin düğmeye basar ve istek tam bir kez varmazsa başarısız olur) ve
  sekizinde de yeniden çalıştırıldı: hata yok; önceki altı durumu önceki
  olgularını taşıyor. `mailqueue`, bilinmeyen yeniden yükleme sonucunun
  düzeltilmesinden sonra (aşağıdaki 2026-10-10 kaydı) sekizinde de yeniden
  çalıştırıldı: hata yok. İki derlemenin içerik özetleri yok sayılarak parça
  parça karşılaştırılması, tam çalıştırmanın derlemesi ile son derleme arasında
  değişen tek kodun Postfix sayfası olduğunu gösterdi. Önceki 357 durumdan
  yedisi 2026-10-09'dakinden başka olgular taşıyor: bir adreste yazan yerel
  port; posta kuyruğunun cümlesi ve yeniden yükleme satırının üstündeki etiket
  (ikisi de ayar yazımı düzeltmelerinden); ve burada bilerek değiştirilen dört
  durum (formu kapanmış kaydedilen DNS kaydı, iki kez; kaydedildiği gösterilen
  Uygula; kartınkinin yanında şeridin Tekrar dene'si). Sekizinde de ölçüldü: yiten yanıttan sonra bildirim, yeniden okumanın
  gerektirdiği durumdaydı (form kapalı ve kayıt bir kez listelenmişken `made`,
  yazılan değer hâlâ formda ve kaydetme denetimi geri gelmişken `not-made`, soru
  sormayan değişiklikler için `read`); yiten her değişiklik sayfadan bir kez
  gönderildi ve bir kez vardı; "Tekrar kontrol et" yalnız okuma gönderdi ve
  sahte sunucunun sonradan eklediği kaydı buldu; kaydedildiği gösterilen
  değişiklik yalnız "Kapat" sundu ve dikkat yüzeyinde değildi. Şerit önce
  kontrol ettiğini, sonra sunucunun söylediğini söyledi; başarısız okumadan
  sonra genel bakışta ve DNS sekmesinde, Tekrar dene ekran genişliğinin içinde
  olmak üzere "kontrol edilemedi" dedi ve Tekrar dene yalnız okuma gönderdi.
  Hiçbir satır eyleminin etiketi birden fazla satırda değildi. Yeni bir senaryo,
  bildirim adı verilenden başka bir durumdaysa, okuması gereken alan
  bulunamazsa ya da hiçbir şey ölçülmediyse başarısız olur.
- *Kapsanmadı.* Gerçek sunucu; Safari, Firefox, ekran okuyucu, dokunmatik cihaz;
  taklit görünümler; yönetici dışındaki roller (ekip üyesinin bu panelleri
  görüşü yalnız bağlanan testle ve sözleşme testleriyle kapsanır). İmzalı
  bölge, dışarıda yönetilen bölge, zamanlanmış görevlerin `jobs` taşımayan
  listesi, geri yükleme ve DKIM anahtarı tarayıcı çalıştırmasında yoktu;
  bağlanan testle kapsanırlar.

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

### İlk gerçek sistem ölçümünden sonra ayar yazıları: yeniden yükleme doğrulanır, doğrulanmamış neden adlandırılmaz, başarısız yeniden yükleme sunucunun ne tuttuğunu söyler (2026-10-10)

Bileşen testleriyle kaynak durumu; gerçek hizmetlerde yeniden koşu bekliyor ve
kurulu bir sunucuda hiçbir şey gözlenmedi. Neyin ölçüldüğü ve neyin değiştiği
için aynı tarihli dayanıklılık sözleşmesi kaydına bakın. Bu kayıt cümleleri
tutar.

**Metinlerin izlediği kural.** Bir cümle, bir nedeni yalnız sunucu onu
doğruladığında söyler. Doğrulanmış hata, eksik önkoşul ve bilinmeyen sonuç üç
ayrı yanıttır: "Postfix'in kendi denetimi yapılandırmasını reddediyor"
(doğrulanmış), "bu sunucunun /etc/cron.allow dosyası kullanıcıyı içermiyor"
(yalnız sahibin değiştirebileceği bir önkoşul), "CelikPanel, Postfix'in
kaydedilen değerleri alıp almadığını belirleyemedi" (bilinmeyen). Sunucunun
kendi programı bir satır yazdıysa o satır cümleyle birlikte, sınırlı ve parola
atamaları silinmiş olarak gösterilir; kendisi asla bir cümle değildir.

**Kim işlem yapar.** Ekrandaki kişi; bir metin "sunucuda" ya da "sunucu sahibi"
diyorsa metindeki komutla sunucu sahibi. Hiçbir şey kendiliğinden yeniden
denenmez. Hiçbir metin, ne olursa olsun başarı bildiren bir komut istemez:
Postfix kurtarma komutu `sudo postfix reload` komutudur; Ubuntu'da bir
sarmalayıcı birimi yeniden yükleyen `sudo systemctl reload postfix` değil.

**İş nasıl sürer.** Sahibin düzeltmesinden sonra: sayfa yenilenir (posta
politikası, yapılandırma dosyası) ya da yeniden denenir (posta kuyruğu,
zamanlanmış görevler). Yazılmış bir politika yazılı kalır ve formun gösterdiği
odur; tutulmayan bir yapılandırma değişikliği formda durur ve sunucudaki dosya
önceki dosyadır. Posta politikasında sahip, hiçbir değeri değiştirmeden Kaydet'e
de basabilir: kabul edilen her kayıt doğrulanmış yeniden yüklemeyle biter; o
kayıt Postfix'e dosyayı aldırır ya da neden almadığını yeniden söyler (aşağıda).

**Platform sınırı.** `/etc/cron.allow` dosyası bir site kullanıcısını içermeyen
bir sunucuda CelikPanel o kullanıcının zamanlanmış görevlerini ne okuyabilir ne
değiştirebilir (Debian ve Ubuntu'nun `crontab -u <kullanıcı>` komutu kullanıcıyı
root için bile reddeder). CelikPanel bunu söyler, hiçbir şeyi değiştirmez ve
`cron.allow` dosyasını düzenlemez: sıkılaştırılmış bir sunucuda bir site
kullanıcısının cron kullanıp kullanamayacağı sahibin kararıdır.

**Metinler.** API cümleleri `error` alanıdır, yalnız İngilizce. Ekran cümleleri
katalog girdileridir: `err.*` `web/src/i18n` içinde; `mailpolicy.*`, `postfix.*`
ve `dbconf.*` `web/src/i18n/screens/server` içinde; `cron.*`
`web/src/i18n/screens` içinde. `{service}` hizmetin adı, `{unit}` ve `<unit>`
Agent'ın adlandırdığı systemd birimi, `{detail}` sunucunun kendi satırıdır.

**Mail policy save (`PUT /api/v1/mail/policy`).**

- `502 MAIL_POLICY_NOT_RELOADED`, reason `check`
  API: "The mail policy was saved to /etc/postfix/main.cf, but Postfix was not
  reloaded: Postfix's own check refuses its configuration as it is now. A
  running Postfix keeps the settings it had before, so the saved values are not
  in effect. What Postfix said is shown with this message; it can be about a
  line this page did not write. Nothing was rolled back. The server owner
  corrects that line, runs sudo postfix check until it prints no error, then
  runs sudo postfix reload. Reload this page afterwards; the saved values are
  the ones shown."
- `err.MAIL_POLICY_NOT_RELOADED.check`
  EN: "Saved to /etc/postfix/main.cf, but Postfix was not reloaded: its own
  check refuses the configuration. A running Postfix keeps the settings it had
  before, so the saved values are not in effect. The line it names is below and
  may be one this page did not write. Nothing was rolled back. On the server,
  correct that line, run sudo postfix check until it prints no error, then run
  sudo postfix reload. The values shown below are the saved ones."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi ancak Postfix yeniden
  yüklenmedi: Postfix’in kendi denetimi yapılandırmayı reddediyor. Çalışan bir
  Postfix önceki ayarlarını korur; bu yüzden kaydedilen değerler yürürlükte
  değil. Adını verdiği satır aşağıda; bu sayfanın yazmadığı bir satır olabilir.
  Hiçbir şey geri alınmadı. Sunucuda o satırı düzeltin, hata yazmayana kadar
  sudo postfix check komutunu çalıştırın, sonra sudo postfix reload komutunu
  çalıştırın. Aşağıda gösterilen değerler kaydedilen değerlerdir."
- `502 MAIL_POLICY_NOT_RELOADED`, reason `reload`
  API: "The mail policy was saved to /etc/postfix/main.cf and Postfix's own
  check accepts the file, but the reload failed, so Postfix has not taken the
  saved values. What the reload said is shown with this message. Nothing was
  rolled back. The server owner runs sudo postfix reload on the server and reads
  what it prints. Reload this page afterwards; the saved values are the ones
  shown."
- `err.MAIL_POLICY_NOT_RELOADED.reload`
  EN: "Saved to /etc/postfix/main.cf, and Postfix’s own check accepts the file,
  but the reload failed, so Postfix has not taken the saved values. Nothing was
  rolled back. On the server, run sudo postfix reload and read what it prints.
  The values shown below are the saved ones."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi ve Postfix’in kendi denetimi
  dosyayı kabul ediyor, ancak yeniden yükleme başarısız oldu; bu yüzden Postfix
  kaydedilen değerleri almadı. Hiçbir şey geri alınmadı. Sunucuda sudo postfix
  reload komutunu çalıştırın ve yazdığını okuyun. Aşağıda gösterilen değerler
  kaydedilen değerlerdir."
- `502 MAIL_POLICY_NOT_RELOADED`, reason `verify`
  API: "The mail policy was saved to /etc/postfix/main.cf, but Postfix was no
  longer running after the reload, so it has not taken the saved values and is
  not handling mail. Nothing was rolled back. The server owner runs sudo postfix
  check, then starts Postfix (sudo systemctl start postfix) and confirms it with
  sudo postfix status. Reload this page afterwards; the saved values are the
  ones shown."
- `err.MAIL_POLICY_NOT_RELOADED.verify`
  EN: "Saved to /etc/postfix/main.cf, but Postfix was no longer running after
  the reload, so it is not handling mail. Nothing was rolled back. On the
  server, run sudo postfix check, start Postfix (sudo systemctl start postfix)
  and confirm with sudo postfix status. The values shown below are the saved
  ones."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi ancak yeniden yüklemeden sonra
  Postfix artık çalışmıyordu; bu yüzden posta işlemiyor. Hiçbir şey geri
  alınmadı. Sunucuda sudo postfix check komutunu çalıştırın, Postfix’i başlatın
  (sudo systemctl start postfix) ve sudo postfix status ile doğrulayın. Aşağıda
  gösterilen değerler kaydedilen değerlerdir."
- `502 MAIL_POLICY_RELOAD_UNKNOWN`
  API: "The mail policy was saved to /etc/postfix/main.cf, but CelikPanel could
  not establish whether Postfix took the saved values: a command that checks or
  reloads Postfix could not be run, or did not answer in time. This is not a
  verified failure, and Postfix may already be running with them. Nothing was
  rolled back. The server owner runs sudo postfix status and then sudo postfix
  reload on the server. Reload this page afterwards; the saved values are the
  ones shown."
- `err.MAIL_POLICY_RELOAD_UNKNOWN`
  EN: "Saved to /etc/postfix/main.cf, but CelikPanel could not establish whether
  Postfix took the saved values: a command that checks or reloads Postfix could
  not be run or did not answer in time. This is not a verified failure; Postfix
  may already be running with them. Nothing was rolled back. On the server, run
  sudo postfix status, then sudo postfix reload. The values shown below are the
  saved ones."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi ancak CelikPanel, Postfix’in
  kaydedilen değerleri alıp almadığını belirleyemedi: Postfix’i denetleyen ya da
  yeniden yükleyen bir komut çalıştırılamadı ya da zamanında yanıt vermedi. Bu
  doğrulanmış bir hata değildir; Postfix bu değerlerle çalışıyor olabilir.
  Hiçbir şey geri alınmadı. Sunucuda önce sudo postfix status, sonra sudo
  postfix reload komutunu çalıştırın. Aşağıda gösterilen değerler kaydedilen
  değerlerdir."
- Ekranda iki tür yanıt birbirine benzemez (2026-10-10). Postfix'in almadığı
  doğrulanan yeniden yükleme (`MAIL_POLICY_NOT_RELOADED`) değişiklik sonrası bir
  hatadır ve hata yüzeyinde durur. Belirlenemeyen sonuç
  (`MAIL_POLICY_RELOAD_UNKNOWN`) hata değildir; kendi cümlesi de bunu söyler ve
  mürekkep rengi metinle dikkat yüzeyinde durur. Bu kaydın ilk biçiminde ikisi
  de aynı hata bandıyla çiziliyordu; "bu doğrulanmış bir hata değildir" kırmızı
  yazılıyordu. Bunu bir test değil, 2026-10-10 tarayıcı kaydına bakmak buldu;
  bağlanan test ve `mailqueue` senaryosu artık her birinin yüzeyini denetler.
- `mailpolicy.postfixSaid`
  EN: "Postfix said:"
  TR: "Postfix’in yanıtı:"
- `mailpolicy.observed`
  EN: "What CelikPanel observed:"
  TR: "CelikPanel’in gözlediği:"
- `mailpolicy.saved.notRunning`
  EN: "Saved to /etc/postfix/main.cf. Postfix is not running on this server, so
  there was nothing to reload; it reads these values when it starts."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi. Postfix bu sunucuda
  çalışmıyor; bu yüzden yeniden yüklenecek bir şey yoktu. Bu değerleri
  başladığında okur."
- `mailpolicy.saved.unchanged`
  EN: "Nothing to save: the server already holds exactly these values."
  TR: "Kaydedilecek bir şey yok: sunucu zaten tam bu değerleri tutuyor."
- Değişiklik içermeyen kayıt (2026-10-10, bu kaydın ilk biçiminden sonra). O
  güne dek böyle bir kayıt `200` / `unchanged` yanıtlıyor ve Postfix'e hiçbir
  şey sormuyordu; bu yüzden "yeniden yüklenmedi" yanıtından sonra main.cf'i
  düzeltip Kaydet'e basan sahibe, Postfix önceki değerlerle çalışmayı
  sürdürürken "kaydedilecek bir şey yok" deniyordu. Postfix'e çalışan ana
  sürecin hangi değerleri tuttuğu sorulamaz ve Agent önceki sonucun kaydını
  tutmaz; bu yüzden kabul edilen her kayıt, hiçbir şey yazmayan dahil, artık
  aynı doğrulanmış yeniden yüklemeyle biter (Postfix'in kendi denetimi,
  `postfix status`, `postfix reload`, `postfix status`). Hiçbir şey yazılmaz,
  onu hiçbir yoklama başlatmaz ve durmuş Postfix yine durmuş bırakılır.
  Yanıtlar:
  - `200`, `applied: unchanged_reloaded`: hiçbir şey yazılmadı; çalışan Postfix
    yeniden yüklendi ve hâlâ çalışıyor.
  - `200`, `applied: unchanged`: hiçbir şey yazılmadı ve Postfix durmuş; yeniden
    yüklenecek bir şey yoktu.
  - `502 MAIL_POLICY_NOT_RELOADED` (gerekçe `check`, `reload` ya da `verify`) ya
    da `502 MAIL_POLICY_RELOAD_UNKNOWN`, yukarıdaki cümlelerle ve gövdede
    politikayla: Postfix dosyayı yine almadı. Bu yanıt `mutation_applied` ya da
    `partial_success` taşımaz, çünkü bu istek main.cf'te hiçbir şeyi
    değiştirmedi; denetim günlüğüne de "yazıldı" satırı eklenmez.
- `mailpolicy.saved.unchangedReloaded`
  EN: "Nothing to save: the server already holds exactly these values. Postfix
  was reloaded with them and is running."
  TR: "Kaydedilecek bir şey yok: sunucu zaten tam bu değerleri tutuyor. Postfix
  bu değerlerle yeniden yüklendi ve çalışıyor."
- `mailpolicy.unreadable`
  EN: "The current mail policy could not be read from the server, so the
  settings are not shown and nothing can be saved here. Nothing was changed. Try
  again."
  TR: "Geçerli posta politikası sunucudan okunamadı; bu yüzden ayarlar
  gösterilmiyor ve buradan kayıt yapılamıyor. Hiçbir şey değiştirilmedi. Tekrar
  deneyin."

**Configuration save (`POST /api/v1/config`), `502 CONFIG_RELOAD_FAILED`.**

- reason `restored` (sentence unchanged)
  API: "The change was not kept: the service could not reload with the new file,
  so CelikPanel put the previous file back and the service is running with it.
  What the service said is below. Correct the setting and save again."
- reason `restored_unit_reload_failed`
  API: "The change was not kept, and the previous file is back in place. The
  service's systemd unit could not reload, with the new file and again with the
  previous one, so CelikPanel asked the server directly: it read the previous
  file again and is running with the settings it had before your change. What
  the unit's reload said is below. It failed with the previous file too, so the
  cause is not only this change. The server owner runs sudo systemctl reload
  <unit> on the server, corrects what it reports, and then saves the change here
  again."
- `dbconf.reloadFailed.restored_unit_reload_failed`
  EN: "The change was not kept, and the previous file is back in place. The
  systemd unit of {service} could not reload, with the new file and again with
  the previous one, so CelikPanel asked {service} directly: it read the previous
  file again and is running with the settings it had before your change. The
  reload failed with the previous file too, so the cause is not only this
  change. On the server, run sudo systemctl reload {unit} to see why, correct
  it, then save the change here again."
  TR: "Değişiklik tutulmadı ve önceki dosya yerine kondu. {service} hizmetinin
  systemd birimi yeni dosyayla da önceki dosyayla da yeniden yüklenemedi; bu
  yüzden CelikPanel doğrudan {service} hizmetine sordu: önceki dosyayı yeniden
  okudu ve değişikliğinizden önceki ayarlarla çalışıyor. Yeniden yükleme önceki
  dosyayla da başarısız olduğu için neden yalnız bu değişiklik değildir.
  Nedenini görmek için sunucuda sudo systemctl reload {unit} komutunu
  çalıştırın, düzeltin, sonra değişikliği buradan yeniden kaydedin."
- reason `restored_running_unknown`
  API: "The change was not kept, and the previous file is back in place. The
  service's systemd unit could not reload, with the new file and again with the
  previous one, and CelikPanel could not establish which settings the service is
  running with now: a reload that fails part-way may already have made it read
  the new file. What the unit's reload said is below. The server owner runs sudo
  systemctl reload <unit> on the server, corrects what it reports, and reloads
  this page."
- `dbconf.reloadFailed.restored_running_unknown`
  EN: "The change was not kept, and the previous file is back in place. The
  systemd unit of {service} could not reload, with the new file and again with
  the previous one, and CelikPanel could not establish which settings {service}
  is running with now: a reload that fails part-way may already have made it
  read the new file. On the server, run sudo systemctl reload {unit}, correct
  what it reports, then reload this page."
  TR: "Değişiklik tutulmadı ve önceki dosya yerine kondu. {service} hizmetinin
  systemd birimi yeni dosyayla da önceki dosyayla da yeniden yüklenemedi ve
  CelikPanel, {service} hizmetinin şu an hangi ayarlarla çalıştığını
  belirleyemedi: yarıda başarısız olan bir yeniden yükleme ona yeni dosyayı
  okutmuş olabilir. Sunucuda sudo systemctl reload {unit} komutunu çalıştırın,
  bildirdiğini düzeltin, sonra bu sayfayı yenileyin."
- reason `not_restored`
  API: "The service could not reload with the new file, and CelikPanel could not
  put the previous file back with certainty. The server owner checks the file on
  the server; the copy named below holds the previous file. Then reload the
  service (sudo systemctl reload <unit>) and reload this page."

**Scheduled tasks, `502 CURRENT_SETTINGS_UNREADABLE` / `scheduled_tasks`.**

- no `detail` token (no verified cause)
  API: "CelikPanel could not read this site user's scheduled tasks from the
  server, so the list is not shown and nothing was changed. This does not mean
  the user has no tasks. CelikPanel has not established why; what the server's
  crontab program said is shown with this message when it said anything. Reload
  the page to read the tasks again. If it keeps failing, the server owner runs
  sudo crontab -u <site user> -l on the server, which prints the same reason."
- `detail: cron_allow`
  API: "CelikPanel cannot read or change this site user's scheduled tasks: this
  server restricts crontab with /etc/cron.allow, and the user is not listed in
  it. Nothing was changed, and the tasks already on the server are untouched.
  While the server restricts crontab this way, CelikPanel cannot manage this
  user's tasks. The server owner adds the site user's name on its own line in
  /etc/cron.allow, then reloads this page."
- `cron.unknown.cron_allow`
  EN: "CelikPanel cannot read or change this domain’s scheduled tasks: this
  server restricts crontab with /etc/cron.allow, and the site’s system user is
  not listed in it. Nothing was changed; the tasks already on the server are
  untouched. While the server restricts crontab this way, CelikPanel cannot
  manage this user’s tasks. The server owner adds the user’s name on its own
  line in /etc/cron.allow, then this list can be read again."
  TR: "CelikPanel bu domain’in zamanlanmış görevlerini okuyamıyor ve
  değiştiremiyor: bu sunucu crontab kullanımını /etc/cron.allow ile kısıtlıyor
  ve sitenin sistem kullanıcısı o dosyada yok. Hiçbir şey değiştirilmedi;
  sunucudaki görevlere dokunulmadı. Sunucu crontab’ı bu şekilde kısıtladığı
  sürece CelikPanel bu kullanıcının görevlerini yönetemez. Sunucu sahibi
  kullanıcının adını /etc/cron.allow dosyasına ayrı bir satır olarak ekler;
  sonra bu liste yeniden okunabilir."
- `detail: cron_deny`
  API: "CelikPanel cannot read or change this site user's scheduled tasks:
  /etc/cron.deny on this server lists the user, so crontab refuses it. Nothing
  was changed, and the tasks already on the server are untouched. While the user
  is listed there, CelikPanel cannot manage this user's tasks. The server owner
  removes the site user's line from /etc/cron.deny, then reloads this page."
- `cron.unknown.cron_deny`
  EN: "CelikPanel cannot read or change this domain’s scheduled tasks:
  /etc/cron.deny on this server lists the site’s system user, so crontab refuses
  it. Nothing was changed; the tasks already on the server are untouched. The
  server owner removes the user’s line from /etc/cron.deny, then this list can
  be read again."
  TR: "CelikPanel bu domain’in zamanlanmış görevlerini okuyamıyor ve
  değiştiremiyor: bu sunucudaki /etc/cron.deny dosyası sitenin sistem
  kullanıcısını içeriyor; bu yüzden crontab onu reddediyor. Hiçbir şey
  değiştirilmedi; sunucudaki görevlere dokunulmadı. Sunucu sahibi kullanıcının
  satırını /etc/cron.deny dosyasından çıkarır; sonra bu liste yeniden
  okunabilir."
- `cron.unknown.said`
  EN: "The server’s crontab program said: {detail}"
  TR: "Sunucunun crontab programının yanıtı: {detail}"
- Ekranda (2026-10-10, `DomainCronManager`; yanıtın `detail` belirtecini
  `web/src/lib/apiError.ts` okur). Sunucunun doğruladığı neden (`cron_allow`,
  `cron_deny`) sunucu sahibinin kuralıdır, bir hata değildir: cümlesi yansız
  yüzeyde, sade bilgi işaretiyle, tek başına durur; nazikçe duyurulur, dikkat
  rengi taşımaz ve crontab'ın yazdığı satır altında yinelenmez. Diğer her yanıt,
  yansız `cron.unknown` cümlesini taşıyan kontrol edilemedi bildirimidir;
  crontab bir satır yazdıysa ardından `cron.unknown.said` gelir, satırın kendisi
  mono yazılır. Ekranın sözü olmayan bir belirteç ve bu yanıt olmayan bir ret
  yansız cümleyi alır. Her durumda liste gösterilmez, görev eklenemez ya da
  değiştirilemez ve "Tekrar dene" yeniden okur, yalnızca okur: sahip
  `cron.allow` ya da `cron.deny` dosyasını değiştirdikten sonra liste böyle
  geri gelir.
- `502 CURRENT_SETTINGS_UNREADABLE` for the other resources
  API: "CelikPanel could not read what is currently set on this server, so
  nothing is shown as a setting and nothing was changed. CelikPanel has not
  established why the read failed; when the server's own program printed a
  reason, it is shown with this message. Reload the page to read it again."

**Mail queue, `502 MAIL_QUEUE_UNREADABLE`.**

- no `reason` (no verified cause)
  API: "The mail queue could not be read, so it is not shown. This does not mean
  the queue is empty. Nothing was changed. CelikPanel has not established why;
  what Postfix's queue program said is shown with this message when it said
  anything. Try again. If it keeps failing, the server owner runs sudo postqueue
  -j on the server, which prints the same reason."
- `postfix.queue.unreadable`
  EN: "The mail queue could not be read, so it is not shown. This does not mean
  the queue is empty. Nothing was changed. Try again; if it keeps failing, run
  sudo postqueue -j on the server to see the reason."
  TR: "Mail kuyruğu okunamadı; bu yüzden gösterilmiyor. Bu, kuyruğun boş olduğu
  anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin; sorun sürerse
  nedeni görmek için sunucuda sudo postqueue -j komutunu çalıştırın."
- reason `postfix_config`
  API: "The mail queue could not be read because Postfix refuses its own
  configuration: a setting in /etc/postfix/main.cf or master.cf has an error,
  and Postfix's programs stop on it. What Postfix said about it is shown with
  this message. Nothing was changed, and this does not mean the queue is empty.
  The server owner corrects that setting, runs sudo postfix check until it
  prints no error, then reloads this page."
- `postfix.queue.unreadable.postfix_config`
  EN: "The mail queue could not be read because Postfix refuses its own
  configuration: a setting in /etc/postfix/main.cf or master.cf has an error,
  and Postfix’s programs stop on it. This does not mean the queue is empty.
  Nothing was changed. On the server, correct the setting Postfix names, run
  sudo postfix check until it prints no error, then try again."
  TR: "Mail kuyruğu okunamadı, çünkü Postfix kendi yapılandırmasını reddediyor:
  /etc/postfix/main.cf ya da master.cf içindeki bir ayar hatalı ve Postfix’in
  programları onda duruyor. Bu, kuyruğun boş olduğu anlamına gelmez. Hiçbir şey
  değiştirilmedi. Sunucuda Postfix’in adını verdiği ayarı düzeltin, hata
  yazmayana kadar sudo postfix check komutunu çalıştırın, sonra tekrar deneyin."
- `postfix.queue.said`
  EN: "Postfix said: {detail}"
  TR: "Postfix’in yanıtı: {detail}"

**2026-10-10'dan beri gösterilen ve gösterilmeyen.** Zamanlanmış görevler
ekranı doğrulanmış nedeni ve crontab'ın satırını gösterir (yukarıda); bu kaydın
ilk biçiminde yanıt ve katalog girdileri vardı, ekran hâlâ tek yansız cümlesini
gösteriyordu. Yazan ve Postfix'i yeniden yükleyen başarılı bir posta politikası
kaydı `mailpolicy.saved` metnini korur. Sahibe Postfix'in çalıştığını
denetlemesini söyleyen `postfix.queue.unknown` ve `mailpolicy.unknown`
girdilerini artık hiçbir ekran kullanmıyor. Gösterilmeyen: crontab okunamadığı
için reddedilen bir zamanlanmış görev yazımı hâlâ `CURRENT_SETTINGS_UNREADABLE`
genel cümlesini bir balonla yanıtlar. 2026-10-10'un iki değişikliği de gerçek
bir hizmette ölçülmedi; o tarihli tarayıcı çalıştırmasında (dördüncü parti,
yukarıda) posta politikası yanıtları, iki yapılandırma yeniden yükleme yanıtı ve
dört crontab yanıtı sahte sunucuya karşı fotoğraflandı. Orada görüldü ve
2026-10-10 birleştirmesiyle değiştirildi: çalışan ayarları bilinmeyen
yapılandırma yeniden yükleme yanıtı (`restored_running_unknown`) hata
yüzeyindeydi; o bir bilinmeyendir, doğrulanmış hata değildir ve artık dikkat
yüzeyinde durur (sunucunun önceki ayarlarını doğruladığı
`restored_unit_reload_failed` hata yüzeyinde kalır). İki yanıtın altındaki
satır, onu birimin günlüğü ya da yeniden yükleme komutu yazdığı halde hizmetin
söylediği diye ("PostgreSQL yanıtı:") sunuluyordu; artık `dbconf.reloadSaid`
ile sunulur (EN: "Reported when {unit} was reloaded:" TR:
"{unit} yeniden yüklenirken bildirilen:"). Orada görüldü ve
değiştirilmedi: aşama adı vermeyen (eski bir Agent'ın) yeniden yüklenmedi
yanıtının cümlesi, bu kaydın başındaki kuralın tersine, hâlâ `sudo systemctl
reload postfix` komutunu adlandırır.

**Henüz gösterilmeyen.** Zamanlanmış görevler ekranı hâlâ tek yansız cümlesini
gösterir (`cron.unknown`); yukarıdaki `cron.unknown.*` girdileri katalogdadır ve
yanıt nedeni ve satırı taşır, ancak ekran onları henüz kullanmaz. Postfix'i
yeniden yükleyen başarılı bir posta politikası kaydı `mailpolicy.saved` metnini
korur. Sahibe Postfix'in çalıştığını denetlemesini söyleyen
`postfix.queue.unknown` ve `mailpolicy.unknown` girdilerini artık hiçbir ekran
kullanmıyor.

### Hizmet eylemleri ve posta sertifikası yenilemesi: yanıt hizmetin gösterdiğidir, bilinmeyen sonuç bilinmeyen diye söylenir (2026-10-10)

Bileşen testleriyle kaynak durumu; gerçek hizmetlerde ölçüm bekliyor ve kurulu
bir sunucuda hiçbir şey gözlenmedi. Yollar ve neyin değiştiği için aynı tarihli
dayanıklılık sözleşmesi kaydına bakın. Bu kayıt cümleleri tutar.

**Metinlerin izlediği kural.** Hizmetler sayfasındaki Başlat, Durdur, Yeniden
başlat ve Yeniden yükle, hizmet yöneticisinin çıkış durumuyla değil, gözlenenle
yanıtlanır: Ubuntu'da `postfix`, Debian ve Ubuntu'da `postgresql` için o durum,
hizmeti çalıştıran birimi yalnız gruplayan bir birime aittir. Üç yanıt vardır.
Yapıldı: hizmet istenen durumda görüldü. Doğrulanmış hata: istenen durumda
olmadığı görüldü ve aşama nerede olduğunu söyler (`check`: kendi denetimi
yapılandırmasını reddediyor ve hiçbir şey gönderilmedi; `reload`, `start`,
`stop`; `verify`: gönderildi ve sonrasında o durumda değil; `command`: hizmet
yöneticisi reddetti). Bilinmeyen: eylem gönderildi ve sonucu belirlenemedi.
Bilinmeyen asla yapıldı diye de hata diye de gösterilmez.

**Kim işlem yapar.** Sunucu sahibi, sunucuda, yanıtın taşıdığı tek komutla
(`vars.command`). Postfix için bu her zaman Postfix'in kendi komutlarından
biridir (`sudo postfix check`, `sudo postfix reload`, `sudo postfix status`);
bunlar her platformda hizmetin kendisi adına yanıt verir. Başka bir hizmet için
komut, onu çalıştıran birimin `sudo systemctl status <unit>` komutudur.

**İş nasıl sürer.** Hiçbir şey kendiliğinden yinelenmez. Doğrulanmış bir
hatadan sonra sahip, hizmetin adını verdiği şeyi düzeltir ve eylemi sayfada
yineler. Bilinmeyen bir sonuçtan sonra sahip önce hizmetin durumuna bakar ve
eylemi yalnız hâlâ gerekiyorsa yineler. Reddedilen bir yapılandırma Başlat,
Yeniden başlat ve Yeniden yükle eylemlerini bir şey gönderilmeden durdurur;
Durdur her zaman gönderilir.

**Metinler, Hizmetler sayfası (`POST /api/v1/service/action`).** API cümleleri
`error` alanıdır, yalnız İngilizce. Aşağıdaki katalog girdileri 2026-10-10
birleştirmesinden beri `web/src/i18n` içindedir (`err.*` olanlar kabuk
kataloğunda, iki `services.action.*` girdisi `screens/server` içinde) ve
ekranların gösterdiği metinlerdir; `web/tests/service-action-outcome.test.mjs`
onları bu kayıtla karşılaştırır. `{unit}` eylemin uygulandığı
birim, `{command}` komut, `{detail}` hizmetin kendi satırı, `{owner_unit}`
hizmeti çalıştıran birim başka bir birimse o birimdir. Bilinmeyen dışındaki her
API cümlesi şununla biter: "The server owner runs the command shown to read the
service's own answer, corrects what it names, and then repeats this action
here; nothing repeats it automatically."

- `502 SERVICE_ACTION_FAILED`, reason `check`
  API: "Nothing was changed: the service's own check refuses its configuration,
  so the action was not carried out."
- `err.SERVICE_ACTION_FAILED.check`
  EN: "Nothing was changed: {unit} refuses its own configuration, so the action
  was not carried out. On the server, run {command} to see what it objects to,
  correct it, then repeat the action here."
  TR: "Hiçbir şey değiştirilmedi: {unit} kendi yapılandırmasını reddediyor; bu
  yüzden işlem yapılmadı. Sunucuda {command} komutunu çalıştırıp neye itiraz
  ettiğini görün, düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `reload`
  API: "The service was not reloaded and keeps running with the settings it
  had."
- `err.SERVICE_ACTION_FAILED.reload`
  EN: "{unit} was not reloaded and keeps running with the settings it had. On
  the server, run {command} to see why, correct it, then repeat the action
  here."
  TR: "{unit} yeniden yüklenmedi ve önceki ayarlarıyla çalışmayı sürdürüyor.
  Sunucuda {command} komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi
  burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `start`
  API: "The service did not start, or did not stay running."
- `err.SERVICE_ACTION_FAILED.start`
  EN: "{unit} did not start, or did not stay running. On the server, run
  {command} to see why, correct it, then repeat the action here."
  TR: "{unit} başlamadı ya da çalışır durumda kalmadı. Sunucuda {command}
  komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `stop`
  API: "The service did not stop: its daemon is still running."
- `err.SERVICE_ACTION_FAILED.stop`
  EN: "{unit} did not stop: it is still running. On the server, run {command}
  to see its state, then repeat the action here."
  TR: "{unit} durmadı: hâlâ çalışıyor. Sunucuda {command} komutuyla durumunu
  görün, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `verify`
  API: "The action was sent, but afterwards the service's daemon is not in the
  state that was asked for."
- `err.SERVICE_ACTION_FAILED.verify`
  EN: "The action was sent, but {unit} is not in the state that was asked for.
  On the server, run {command} to see its state, correct the cause, then repeat
  the action here."
  TR: "İşlem gönderildi ancak {unit} istenen durumda değil. Sunucuda {command}
  komutuyla durumunu görün, nedeni düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `command`
  API: "The server's service manager did not carry out the action."
- `err.SERVICE_ACTION_FAILED.command`
  EN: "The server's service manager did not carry out the action on {unit}. On
  the server, run {command} to see why, correct it, then repeat the action
  here."
  TR: "Sunucunun hizmet yöneticisi {unit} üzerindeki işlemi yapmadı. Sunucuda
  {command} komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi burada
  yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason yok (bu Panelin bilmediği bir aşama)
  API: "The action did not take effect."
- `err.SERVICE_ACTION_FAILED`
  EN: "The action on {unit} did not take effect. On the server, run {command}
  to see why, correct it, then repeat the action here."
  TR: "{unit} üzerindeki işlem etkili olmadı. Sunucuda {command} komutunu
  çalıştırıp nedenini görün, düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_UNKNOWN`
  API: "The action was sent, but what came of it could not be verified, so it
  is not reported as done. This is not a verified failure: the service may
  already be in the state that was asked for. The server owner runs the command
  shown to see the service's state, and repeats this action here only if it is
  still needed; nothing repeats it automatically."
- `err.SERVICE_ACTION_UNKNOWN`
  EN: "The action was sent, but what came of it could not be verified, so it is
  not shown as done. This is not a verified failure: {unit} may already be in
  the state you asked for. On the server, run {command} to see its state, and
  repeat the action here only if it is still needed."
  TR: "İşlem gönderildi ancak sonucu doğrulanamadı; bu yüzden yapıldı diye
  gösterilmiyor. Bu doğrulanmış bir hata değildir: {unit} istediğiniz duruma
  zaten gelmiş olabilir. Sunucuda {command} komutuyla durumunu görün; işlemi
  yalnız hâlâ gerekiyorsa burada yineleyin."
- `services.action.said` (`vars.detail` varsa iki yanıtın da altında
  gösterilir)
  EN: "The service said: {detail}"
  TR: "Hizmetin yanıtı: {detail}"
- `services.action.ownerUnit` (`vars.owner_unit` varsa)
  EN: "The service itself runs as {owner_unit}; {unit} only groups it."
  TR: "Hizmetin kendisi {owner_unit} olarak çalışır; {unit} yalnız onu
  gruplar."

**Metinler, posta sertifikası yenilemesi.** Ekran yok: bağımsız yenileme
yardımcısı cümleyi kendi günlüğüne yazar (`journalctl -u
celikpanel-mail-renewal.service`), Agent kendi kaydına yazar. Yalnız İngilizce.
`<service>` Postfix ya da Dovecot'tur. Yalnız bu iki neden için, genel "mail
certificate activation paused" cümlesinin yerini alırlar.

- Hizmetin dinleyicileri yeniden yüklemesinden sonra hâlâ başka bir sertifika
  sunuyor (doğrulanmış):
  "mail certificate activation is not complete: <service> was asked to reload,
  but its listeners on this server still present another certificate than the
  selected one; the server owner runs `postfix reload` as root, reads what it
  prints and the service's log, and the renewal then retries the same
  operation, by itself up to its recorded limit and after that with the
  continuation command it prints; the renewed certificate stays selected, and
  Postfix/Dovecot settings and certificate evidence are preserved"
  (Dovecot için komut `doveadm reload`).
- Hizmetin hiçbir dinleyicisi TLS ile yanıt vermedi (bilinmeyen):
  "mail certificate activation is not confirmed: no TLS listener of <service>
  answered on this server, so which certificate it presents is unknown and
  nothing is reported as activated; the server owner checks that <service> is
  running and listening (`postfix status`, which answers for the daemon where
  `systemctl is-active postfix` may answer for a wrapper unit), starts it if it
  is stopped, and the renewal then retries the same operation, by itself up to
  its recorded limit and after that with the continuation command it prints;
  the renewed certificate stays selected, and Postfix/Dovecot settings and
  certificate evidence are preserved" (Dovecot için denetim `systemctl status
  dovecot`).

**Bağımsız yenilemeye zaten kayıtlı bir sunucu.** Yenileme yardımcısı
kaydolduğu yardımcıdır ve bu cümleleri yazmaz; Ubuntu'da, Postfix yeniden
yüklenmediği hâlde bir yenilemeyi etkinleştirildi diye kaydedebilir. Yardımcı
daha yenisine taşınabilene kadar (tasarlandı, uygulanmadı; dayanıklılık
sözleşmesi) sahip bir yenilemeden sonra sunucuda şunu karşılaştırır:

    openssl s_client -connect localhost:465 </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256
    openssl x509 -noout -fingerprint -sha256 -in /etc/ssl/celikpanel/_mail/host/current/fullchain.pem

İlk satır Postfix'in sunduğu sertifika, ikincisi kurulu olandır. Farklıysa
`sudo postfix reload` Postfix'in onu almasını sağlar; alamıyorsa Postfix'in
neye itiraz ettiğini yazar. İki satır da yalnız okur.

**Ekranda (2026-10-10).** Her bileşenin sayfası (`ServiceShell`;
`ComponentDetail` genel sayfası dahil) ve bileşen listesi (`ServiceList`)
yanıtı, kapatılana ya da başka bir işlem yapılana dek içeriğin üstünde, sayfada
tutar: komutu ayrı gösterilen cümle, `services.action.said` altında hizmetin
satırı, hizmeti başka bir birim çalıştırıyorsa `services.action.ownerUnit` ve
Kapat. Doğrulanmış hata hata yüzeyinde, bilinmeyen sonuç dikkat yüzeyinde
durur. İşlem durumu değiştirmiş olabileceği için durum altında yeniden okunur.
Önceden ikisi de beş saniyede kaybolan balonlardı. Hiç yanıt almayan işlem de
artık kırmızı bir balon değildir.

**Henüz gösterilmeyen.** Paneldeki posta sertifikası durumu açık bir
yenilemenin nedenini göstermez; neden yardımcının günlüğünde ve Agent'ın
kaydındadır.

### Bir değişiklik bir kez gönderilir ve bir kez yanıtlanır: istek kimliği retleri (2026-10-10)

Bileşen testleriyle kaynak durumu; kurulu sunucu ve gerçek sistem denemesi yok.
Bkz. D-029 ve aynı tarihli dayanıklılık sözleşmesi kaydı. Durum değiştiren sekiz
rota artık bir isteği, kaç kez gelirse gelsin, bir kez çalıştırır; bunlar, yanıt
değişikliğin kendi sonucu olmadığında ekranın gösterdiği cümlelerdir.

**Aşağıdaki her durumda kim işlem yapar.** Ekrandaki kişi. Hiçbir şey
kendiliğinden yeniden gönderilmez ya da çalıştırılmaz; hiçbir şeyi iki kez
değiştirmeyen tek istisna: kaybolan bir yanıttan sonra sayfa aynı yanıtı aynı
kimlikle bir kez daha ister ve sunucu onu ilk çalışmadan yanıtlar.

**İş nasıl sürer.** Sayfayı yeniden yüklemek yalnızca okur. Yeniden yüklemeden
sonra yeniden yapılan değişiklik, yeni kimlikli yeni bir istektir.

**Metinler.** Anahtarlar kabuk kataloğundadır (`web/src/i18n/en.ts`,
`web/src/i18n/tr.ts`), çünkü her ekran bunları alabilir. Sunucunun her kod için
kendi İngilizce mesajı, API'yi doğrudan okuyanlar için aynı cümledir.

- *Sayfa Panel'den eski (herhangi bir değişiklikten önce reddedilir; `428`).*
  - `err.REQUEST_ID_REQUIRED`:
    - EN: "This page was opened before CelikPanel was updated, so the server
      did not accept the change and nothing was changed. Reload the page, then
      make the change again."
    - TR: "Bu sayfa CelikPanel güncellenmeden önce açılmış; bu yüzden sunucu
      değişikliği kabul etmedi ve hiçbir şey değiştirilmedi. Sayfayı yeniden
      yükleyin, sonra değişikliği yeniden yapın."
  - Sunucunun mesajı, sayfa olmayan bir istemci için şunu ekler: "(A client
    that is not the CelikPanel page sends the header X-CelikPanel-Request-Id:
    32 lowercase hexadecimal characters, a new value for each action.)"
- *Kimlik başka bir şey için zaten kullanılmış (herhangi bir değişiklikten önce
  reddedilir; `409`).*
  - `err.REQUEST_ID_REUSED`:
    - EN: "This change was sent with an identifier the server already used for
      a different change, so it was not carried out. Reload the page, then make
      the change again."
    - TR: "Bu değişiklik, sunucunun başka bir değişiklik için zaten kullandığı
      bir kimlikle gönderildi; bu yüzden uygulanmadı. Sayfayı yeniden yükleyin,
      sonra değişikliği yeniden yapın."
- *İlk geliş hâlâ sürüyor (bekleme; `409`).*
  - `err.REQUEST_IN_PROGRESS`:
    - EN: "This change is still running on the server. It was not started a
      second time. Wait a little, then reload the page to see the result; do
      not send it again."
    - TR: "Bu değişiklik sunucuda hâlâ sürüyor. İkinci kez başlatılmadı. Biraz
      bekleyin, sonra sonucu görmek için sayfayı yeniden yükleyin; değişikliği
      yeniden göndermeyin."
- *Değişiklik sürerken Panel durdu (bilinmeyen sonuç; `409`).*
  - `err.REQUEST_OUTCOME_UNKNOWN`:
    - EN: "CelikPanel restarted or failed while this change was running, so it
      is not known whether the change was completed. It will not be run again
      by itself. Reload the page and check the current state; make the change
      again only if it is missing."
    - TR: "CelikPanel bu değişiklik sürerken yeniden başladı ya da hata verdi;
      bu yüzden değişikliğin tamamlanıp tamamlanmadığı bilinmiyor.
      Kendiliğinden yeniden çalıştırılmayacak. Sayfayı yeniden yükleyip mevcut
      durumu kontrol edin; değişikliği yalnızca eksikse yeniden yapın."
- *Değişiklik yapıldı; tek seferlik sonucu saklanmıyor (bilinen sonuç; `409`).*
  - `err.REQUEST_COMPLETED_RESULT_NOT_RETAINED`:
    - EN: "This change was already made; it was not made a second time. Its
      result was shown only once and is not kept. Reload the page to see the
      current state; if you still need what was shown once (a password or a
      configuration file), create a new one."
    - TR: "Bu değişiklik zaten yapıldı; ikinci kez yapılmadı. Sonucu yalnızca
      bir kez gösterildi ve saklanmıyor. Mevcut durumu görmek için sayfayı
      yeniden yükleyin; bir kez gösterilene (parola ya da yapılandırma dosyası)
      hâlâ ihtiyacınız varsa yenisini oluşturun."
  - `err.REQUEST_COMPLETED_RESULT_NOT_RETAINED.failed` (ilk deneme hatayla bitti
    ve o yanıt saklanmıyor; ayrıntısı kalmamış doğrulanmış bir hata):
    - EN: "This change already ended with an error, and that answer is not
      kept; it was not tried a second time. Reload the page and check the
      current state; make the change again only if it is missing."
    - TR: "Bu değişiklik daha önce hatayla sonuçlandı ve o yanıt saklanmıyor;
      ikinci kez denenmedi. Sayfayı yeniden yükleyip mevcut durumu kontrol
      edin; değişikliği yalnızca eksikse yeniden yapın."
  - Bir veritabanı sunucusunun kendi hesabında ekran, bu ikisinden ilkini olduğu
    gibi, başarı olarak ele alır: o yanıt parola taşımaz (parola "Parolayı
    göster" ile okunur); yani eksik bir şey yoktur.
- *Aynı alan adının başka bir geri yüklemesi sürüyor (herhangi bir değişiklikten
  önce reddedilir; `409`).*
  - `err.BACKUP_RESTORE_IN_PROGRESS`:
    - EN: "Another restore of this domain is still running, so this one was not
      started and changed nothing. Wait for it to finish and check the site;
      restore again only if it is still needed."
    - TR: "Bu alan adının başka bir geri yüklemesi hâlâ sürüyor; bu yüzden bu
      geri yükleme başlatılmadı ve hiçbir şeyi değiştirmedi. Bitmesini bekleyip
      siteyi kontrol edin; yalnızca hâlâ gerekiyorsa yeniden geri yükleyin."
- *cPanel içe aktarımı: sonuç, ikinci sormadan sonra da bilinmiyor (bilinmeyen
  sonuç). Değişen metin; anahtar `web/src/i18n/screens` içinde.*
  - `import.unknown.body`:
    - EN: "This page did not get the result of the import: the answer from the
      server did not arrive, and asking once more for it did not bring the
      result either. The import may have run completely, in part or not at
      all, or may still be running. Asking again never starts it a second
      time, and starting it again is not offered until you have checked.
      Check whether {domain} is on this server now; checking only reads."
    - TR: "Bu sayfa içe aktarımın sonucunu alamadı: sunucunun yanıtı ulaşmadı ve
      yanıt bir kez daha istendiğinde de sonuç gelmedi. İçe aktarım tamamen,
      kısmen çalışmış ya da hiç çalışmamış olabilir; hâlâ sürüyor da olabilir.
      Yeniden sormak onu ikinci kez başlatmaz; siz kontrol edene dek yeniden
      başlatma da sunulmaz. {domain} alan adının şu an bu sunucuda olup
      olmadığını kontrol edin; kontrol yalnız okur."
  - Yalnızca kontrol alan adını bulamadığında sunulan "İçe aktarımı yeniden
    başlat", artık aynı isteği aynı kimlikle gönderir: sunucu onu, varsa ilk
    çalışmadan yanıtlar; yalnızca hiç ulaşmadıysa çalıştırır.

**Sonucu bilinmeyen değişiklik için tek davranış (dördüncü partiyle
birleştirildi, 2026-10-10).** İstek kimliği ile dördüncü partinin yiten yanıt
ele alışı (yukarıdaki 2026-10-09 kaydı) yan yana yazılmıştı. Birlikte
şöyledirler.

- *Yiten yanıtın ne olduğu* tek yerde tanımlıdır
  (`web/src/lib/requestIdentity.ts` içindeki `answerWasLost`): hiç yanıt yok,
  ya da durum kodu 408, 429, 502, 503 ya da 504 olan ve JSON olmayan bir yanıt;
  bu, Panel'in yerine konuşan bir geçittir. Panel'in kendi reddi, durum kodu
  bunlardan biri olsa da JSON'dur (ulaşılamayan Agent, cümlesi olan bir
  502'dir); o yanıtın kendisidir ve gösterilir. Birleştirmeden önce yakalayıcı
  onu da yeniden soruyordu; yanıtı hiç saklanmayan iki rotada ekran o zaman
  Panel'in cümlesi yerine "daha önce hatayla sonuçlandı ve o yanıt saklanmıyor"
  cümlesini gösteriyordu.
- *Sekiz rotada* yakalayıcı 1,5 saniye sonra aynı kimlikle bir kez daha sorar.
  O yanıtlanırsa ekran değişikliğin kendi sonucunu gösterir, başka bir şey
  göstermez.
- *O da yanıtlanmazsa*, ya da Panel `REQUEST_OUTCOME_UNKNOWN` ya da
  `REQUEST_IN_PROGRESS` yanıtlarsa sonuç bilinmiyordur ve sekiz ekranın hepsi,
  dördüncü partinin kimliksiz bir değişiklik için yaptığını yapar: dikkat
  yüzeyinde yerinde duran bir bildirim, değişikliğin etkilediği şeyin yeniden
  okunması (yalnız okuma) ve o okuma yanıtlanana dek değiştiren ya da kaldıran
  her denetimin kapalı kalması. Bunun hiçbiri balon değildir ve hiçbiri hata
  olarak çizilmez.
- *Bildirim hangisinin olduğunu söyler.* Kimliksiz bir değişiklikte hâlâ hiçbir
  şeyin ikinci kez gönderilmediğini söyler (`common.resultUnknown*`,
  değişmedi). Sekizden biri için bunu asla söylemez. İki cümlesi vardır: ne
  olduğu, sonra yeniden okunan durumun ne gösterdiği. O durum değişikliği
  gösterdiğinde tek cümle kalır ve onu gönderen form kapatılır.
- *O okumayla yapıldığı görülen, ama sonucu kimseye gösterilmemiş değişiklik*
  (bir VPN cihazı; yeni kullanıcılı bir veritabanı) yalnız "yapıldı" diye
  değil, aşağıdaki yalnız-durum yanıtının cümlesiyle söylenir.

**O bildirimin metinleri.** Kabuk kataloğu.

- *Ne oldu.*
  - `common.lostAsked`:
    - EN: "No answer arrived for this change, and asking the server once more
      for the same answer brought none either, so it is not known whether the
      change was made. Asking again never makes the change a second time."
    - TR: "Bu değişikliğin yanıtı ulaşmadı; sunucudan aynı yanıt bir kez daha
      istendiğinde de gelmedi. Bu yüzden değişikliğin yapılıp yapılmadığı
      bilinmiyor. Yeniden sormak değişikliği asla ikinci kez yapmaz."
  - `common.lostInterrupted`:
    - EN: "CelikPanel restarted or failed while this change was running, so it
      is not known whether the change was completed. It will not be run again
      by itself."
    - TR: "CelikPanel bu değişiklik sürerken yeniden başladı ya da hata verdi;
      bu yüzden değişikliğin tamamlanıp tamamlanmadığı bilinmiyor.
      Kendiliğinden yeniden çalıştırılmayacak."
  - `common.lostRunning`:
    - EN: "This change is still running on the server, so its result is not
      known yet. It was not started a second time."
    - TR: "Bu değişiklik sunucuda hâlâ sürüyor; bu yüzden sonucu henüz
      bilinmiyor. İkinci kez başlatılmadı."

- *Yeniden okunan durum ne gösteriyor.*
  - `common.lostStateReading`:
    - EN: "What is shown here is being read again. Controls that change or
      remove something stay off until it has been read."
    - TR: "Burada gösterilen yeniden okunuyor. Bir şeyi değiştiren ya da
      kaldıran denetimler, okuma bitene dek kapalı kalır."
  - `common.lostStateRead`:
    - EN: "What is shown here was read again at {time}. Check it before making
      the change again; if the change may still be running, check again in a
      little while."
    - TR: "Burada gösterilen, saat {time} itibarıyla yeniden okundu.
      Değişikliği yeniden yapmadan önce ona bakın; değişiklik hâlâ sürüyor
      olabilirse biraz sonra tekrar kontrol edin."
  - `common.lostStateUnread`:
    - EN: "The current state could not be read again, so controls that change
      or remove something stay off. Check again."
    - TR: "Güncel durum yeniden okunamadı; bu yüzden bir şeyi değiştiren ya da
      kaldıran denetimler kapalı kalıyor. Tekrar kontrol edin."
  - `common.lostStateNotMade`:
    - EN: "What was read again at {time} does not show the change, so it is not
      known to have been made. What you entered is still here. If the server is
      still working on it, the change can appear later: check again before
      sending it a second time."
    - TR: "Saat {time} itibarıyla yeniden okunan durum değişikliği göstermiyor;
      bu yüzden yapıldığı bilinmiyor. Girdikleriniz hâlâ burada. Sunucu hâlâ
      üzerinde çalışıyorsa değişiklik sonradan görünebilir: ikinci kez
      göndermeden önce tekrar kontrol edin."
  - `common.lostStateMade`:
    - EN: "The answer to this change did not reach this page, but what was read
      again at {time} shows the change, so it was made. Nothing needs to be
      sent again."
    - TR: "Bu değişikliğin yanıtı bu sayfaya ulaşmadı; ancak saat {time}
      itibarıyla yeniden okunan durum değişikliği gösteriyor, yani yapıldı.
      Hiçbir şeyin yeniden gönderilmesi gerekmiyor."

**Yapılmış, ama tek seferlik sonucu saklanmayan değişiklik (gerekçesiz `409
REQUEST_COMPLETED_RESULT_NOT_RETAINED`).** Kişi kapatana dek dikkat yüzeyinde,
yerinde söylenir; yapılan şey orada olsun diye liste yeniden okunur.

- *VPN cihazı.*
  - `vpn.configNotShown`:
    - EN: "The device {name} was added; it was not added a second time. Its
      configuration did not reach this page, and a configuration is shown only
      once and is not stored, so it cannot be shown again. If {name} is in the
      list below, remove it; then add the device again to get a new
      configuration."
    - TR: "{name} cihazı eklendi; ikinci kez eklenmedi. Yapılandırması bu
      sayfaya ulaşmadı; yapılandırma yalnızca bir kez gösterilir ve saklanmaz,
      bu yüzden yeniden gösterilemez. {name} aşağıdaki listedeyse onu kaldırın;
      sonra yeni bir yapılandırma almak için cihazı yeniden ekleyin."

- *Veritabanı sunucusunda, yeni kullanıcılı veritabanı.*
  - `databases.passwordNotShown`:
    - EN: "The database {name} was created; it was not created a second time.
      The answer that carried the password of its user {user} did not reach
      this page and is not kept, so the password cannot be shown again. It is
      the password you entered in the form. If you no longer have it, set a new
      password for {user} on the database server itself; this page has no
      control for that yet."
    - TR: "{name} veritabanı oluşturuldu; ikinci kez oluşturulmadı. {user}
      kullanıcısının parolasını taşıyan yanıt bu sayfaya ulaşmadı ve
      saklanmıyor; bu yüzden parola yeniden gösterilemez. Parola, formda
      girdiğiniz paroladır. Artık elinizde değilse {user} için veritabanı
      sunucusunun kendisinde yeni bir parola belirleyin; bu sayfada bunun için
      henüz bir denetim yok."
  - Panel'de bir veritabanı kullanıcısının parolasını belirleyen bir denetim
    yoktur; cümle bunu söyler. D-029 "üretilen veritabanı parolası yeniden
    belirlenir" sonucunu sayar; bunun denetimi yapılmadı.

- *Panel'in bir veritabanı motorundaki kendi hesabı:* yukarıdaki gibi, olduğu
  başarı.
- *`failed` gerekçesiyle:* yukarıdaki gibi
  `err.REQUEST_COMPLETED_RESULT_NOT_RETAINED.failed`.

**İçe aktarım sayfası** alan adı kontrolüyle birlikte kendi bildirimini korur.
Gövdesi hangisinin olduğunu söyler: ikinci soru da yanıtsız kaldığında
`import.unknown.body` (yukarıda), ayrıca

- `import.unknown.bodyRunning`:
  - EN: "The import is still running on the server, so this page does not have
    its result yet. It was not started a second time. Asking again never starts
    it a second time, and starting it again is not offered until you have
    checked. Wait a little, then check whether {domain} is on this server now;
    checking only reads."
  - TR: "İçe aktarım sunucuda hâlâ sürüyor; bu yüzden bu sayfa sonucunu henüz
    alamadı. İkinci kez başlatılmadı. Yeniden sormak onu ikinci kez başlatmaz;
    siz kontrol edene dek yeniden başlatma da sunulmaz. Biraz bekleyin, sonra
    {domain} alan adının şu an bu sunucuda olup olmadığını kontrol edin;
    kontrol yalnız okur."
- `import.unknown.bodyInterrupted`:
  - EN: "CelikPanel restarted or failed while the import was running, so it is
    not known whether it ran completely, in part or not at all. It will not be
    run again by itself, and starting it again is not offered until you have
    checked. Check whether {domain} is on this server now; checking only
    reads."
  - TR: "CelikPanel içe aktarım sürerken yeniden başladı ya da hata verdi; bu
    yüzden içe aktarımın tamamen mi, kısmen mi çalıştığı, yoksa hiç çalışmadığı
    bilinmiyor. Kendiliğinden yeniden çalıştırılmayacak; siz kontrol edene dek
    yeniden başlatma da sunulmaz. {domain} alan adının şu an bu sunucuda olup
    olmadığını kontrol edin; kontrol yalnız okur."

`REQUEST_OUTCOME_UNKNOWN` yanıtından sonraki başlatma yeni bir istektir; öteki
iki durumda aynı istek yeniden sorulur.

**Sekizinin her biri nerede çizilir.** Yedek ve geri yükleme: alan adının
Yedekler paneli (`DomainBackupManager`). Sertifika: SSL/TLS sekmesi
(`DomainSSLSettings`); sertifika yeniden okunur ve o sırada sekmenin
denetimleri kapalıdır. Alan adının veritabanı: listeye veritabanını adlandırıp
adlandırmadığını soran `DomainDatabaseManager`. Sunucudaki veritabanı: açıkken
pencere (`AddDatabaseModalV2`), sonrasında Veritabanları sayfası; iki listeye
de sorar. Motor hesabı: şeridi (`DatabaseAccountStrip`). VPN cihazı: cihazlara
onu adlandırıp adlandırmadıklarını soran Cihazlar sekmesi (`VPNPage`). İçe
aktarım: `ImportPage`.

**Güncellemeden önce açılmış sayfa (`428`).** Eski kodu çalıştırır: başlık
göndermez ve kod için girdisi yoktur; bu yüzden sunucunun yeniden yüklemeyle
biten İngilizce cümlesini gösterir. `err.REQUEST_ID_REQUIRED` girdisi, güncel
sayfanın bu kodu alırsa göstereceği metindir. Yanıt bir rettir, bilinmeyen
sonuç değildir; hiçbir şey değiştirilmemiştir. Sayfayı yeniden yükleyen bir
düğme yoktur; bunu cümle ister.

**Sınırlar.** Sunucudaki veritabanı penceresi ve alan adının veritabanı formu,
bu bildirimler dışında, eskisi gibi yalnız İngilizcedir. Kendi sözü olmayan bir
ret, kodunun girdisi varsa katalogdan, yoksa sunucunun cümlesiyle gösterilir.
Korumanın sözleşmesini tutan yerel bir sahte sunucuya karşı gerçek bir
Chrome'da incelendi (dayanıklılık sözleşmesi, bu tarihli kayıt); gerçek bir
Panel'de değil.

### İkinci gerçek sistem ölçümünden sonra: başarısız yeniden yükleme yalnızca doğrulananı söyler, içe aktarım neyi aktardığını söyler, sertifika hatası türünü söyler (2026-10-11)

Bileşen testleriyle kaynak durumu; gerçek hizmetlerde yeniden koşu bekliyor ve
kurulu bir sunucuda hiçbir şey gözlenmedi. Neyin ölçüldüğü ve neyin değiştiği
için aynı tarihli dayanıklılık sözleşmesi kaydına bakın. Bu kayıt cümleleri
tutar.

**Metinlerin izlediği kural.** Bir cümle, bir hizmetin hangi ayarlarla
çalıştığını yalnızca hizmete sorulduğunda söyler. Birimin başarısız diye
bildirdiği bir yeniden yükleme bu konuda tek başına hiçbir şey söylemez: komut
başarısız olmadan önce hizmete sinyal göndermiş olabilir. Çalışmayan bir hizmet
başarısız bir yeniden yükleme değil, eksik bir önkoşuldur. Her adımı bitmiş bir
içe aktarım ya tamdır ya da doğrulanmış kısmi bir sonuçtur; asla "beklemede"
değildir. Sertifika çıkarmayan bir istek, hatanın türünü yalnızca certbot'un
kendi çıktısı onu söylediğinde adlandırır.

**Yerini alan.** 2026-10-10 kaydında yazılı `err.SERVICE_ACTION_FAILED.reload`
cümlesi ve API'nin `reload` gerekçesinin cümlesi ("... önceki ayarlarıyla
çalışmayı sürdürüyor") artık gösterilmez. Aşağıdakiler gösterilir.

**Başarısız bir yeniden yükleme (`POST /api/v1/service/action`).**

- `502 SERVICE_ACTION_FAILED`, reason `reload`
  API: "The service reported that the reload failed. CelikPanel cannot read
  from this service which settings it is running with now, so it says neither
  that it kept the settings it had nor that it took the files on disk. The
  server owner runs the command shown to read the service's own answer,
  corrects what it names, and then repeats this action here; nothing repeats it
  automatically."
- `err.SERVICE_ACTION_FAILED.reload`
  EN: "{unit} reported that the reload failed. CelikPanel cannot read from
  {unit} which settings it is running with now, so this page says neither that
  it kept the settings it had nor that it took the files on disk. On the
  server, run {command} to see why, correct it, then repeat the action here."
  TR: "{unit} yeniden yüklemenin başarısız olduğunu bildirdi. CelikPanel,
  {unit} hizmetinin şu an hangi ayarlarla çalıştığını ondan okuyamıyor; bu
  yüzden bu sayfa ne önceki ayarlarını koruduğunu ne de diskteki dosyaları
  aldığını söylüyor. Sunucuda {command} komutunu çalıştırıp nedenini görün,
  düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `reload_reread` (PostgreSQL)
  API: "The unit reported the reload as failed, but PostgreSQL itself re-read
  its configuration files after it: the settings in the files on disk are in
  effect now, except those that need a restart. A step of the unit's own reload
  command failed after the server had been signalled. The server owner runs the
  command shown to see which step, and corrects it so that the next reload is
  reported as it went; the reload does not need to be repeated for these
  settings."
- `err.SERVICE_ACTION_FAILED.reload_reread`
  EN: "The reload of {unit} was reported as failed, but PostgreSQL itself
  re-read its configuration files after it: the settings in the files on disk
  are in effect now, except those that need a restart. A step of the unit’s own
  reload command failed after the server had been signalled. On the server, run
  {command} to see which step, and correct it so that the next reload is
  reported as it went. The reload does not need to be repeated for these
  settings."
  TR: "{unit} için yeniden yükleme başarısız diye bildirildi, ancak PostgreSQL
  yapılandırma dosyalarını bundan sonra kendisi yeniden okudu: diskteki
  dosyalardaki ayarlar, yeniden başlatma gerektirenler dışında, şu an
  yürürlükte. Birimin kendi yeniden yükleme komutunun bir adımı, sunucuya
  sinyal gönderildikten sonra başarısız oldu. Sunucuda {command} komutunu
  çalıştırıp hangi adım olduğunu görün ve sonraki yeniden yüklemenin olduğu
  gibi bildirilmesi için düzeltin. Bu ayarlar için yeniden yüklemeyi
  yinelemeniz gerekmez."
- `502 SERVICE_ACTION_FAILED`, reason `reload_not_reread` (PostgreSQL)
  API: "The reload failed and PostgreSQL did not re-read its configuration
  files: it is running with the settings it had before. The server owner runs
  the command shown to read the service's own answer, corrects what it names,
  and then repeats this action here; nothing repeats it automatically."
- `err.SERVICE_ACTION_FAILED.reload_not_reread`
  EN: "The reload of {unit} failed and PostgreSQL did not re-read its
  configuration files: it is running with the settings it had before. On the
  server, run {command} to see why, correct it, then repeat the action here."
  TR: "{unit} için yeniden yükleme başarısız oldu ve PostgreSQL yapılandırma
  dosyalarını yeniden okumadı: önceki ayarlarıyla çalışıyor. Sunucuda {command}
  komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi burada yineleyin."
- `409 SERVICE_ACTION_FAILED`, reason `not_running`
  API: "The service is not running, so there was nothing to reload and nothing
  was changed. If it should run, the server owner starts it with Start on this
  page; it reads its configuration files when it starts."
- `err.SERVICE_ACTION_FAILED.not_running`
  EN: "{unit} is not running, so there was nothing to reload and nothing was
  changed. If it should run, use Start here; it reads its configuration files
  when it starts. To see its state on the server, run {command}."
  TR: "{unit} çalışmıyor; bu yüzden yeniden yüklenecek bir şey yoktu ve hiçbir
  şey değiştirilmedi. Çalışması gerekiyorsa burada Başlat’ı kullanın; başlarken
  yapılandırma dosyalarını okur. Sunucudaki durumunu görmek için {command}
  komutunu çalıştırın."

**cPanel içe aktarımı (`POST /api/v1/import/cpanel/inspect`, `.../apply`).**

- `200`, `status: partial`, `code: IMPORT_PARTIAL`, field `message`
  API: "The import ended with a part of the archive not imported, and it does
  not continue by itself. Imported: {imported}. Not imported: {not imported}.
  The domain {domain} was created and is kept; it is left marked as not
  finished. The reason of each part that was not imported is in its step below.
  The server owner either adds the missing parts by hand on the domain's own
  pages, or removes {domain} on the Domains page, corrects what the step names
  and imports the archive again; an import into a domain that already exists is
  refused, so nothing is imported twice."
- `502 IMPORT_SITE_NOT_CREATED`
  API: "The import did not start: the site for this domain could not be created
  on this server, so no file, mailbox, DNS record or database of the archive
  was imported. Whether a part of the new site itself was left behind is not
  known from this answer: open Domains to see whether the domain is listed. The
  server owner reads the step that failed on the server with sudo journalctl -u
  celikpanel-agent, corrects it, and starts the import again; nothing starts it
  again automatically."
- step `mail:<address>`, field `detail`
  API: "not imported: the archive holds no password for this mailbox"
- `import.mailPasswords.all`
  EN: "Each of these mailboxes has a password in the archive, and the import
  keeps it. The password itself is never shown."
  TR: "Bu posta kutularının her birinin arşivde bir parolası var ve içe aktarım
  onu korur. Parolanın kendisi hiçbir zaman gösterilmez."
- `import.mailPasswords.some`
  EN: "{kept} of {total} mailboxes have a password in the archive, and the
  import keeps it. The password itself is never shown. The archive holds no
  password for {missing}, so the import does not create them: create them on
  the domain’s mail page afterwards, with a new password."
  TR: "{total} posta kutusundan {kept} tanesinin arşivde parolası var ve içe
  aktarım onu korur. Parolanın kendisi hiçbir zaman gösterilmez. Arşivde
  şunların parolası yok, bu yüzden içe aktarım onları oluşturmaz: {missing}.
  Bunları sonradan alan adının posta sayfasında yeni bir parolayla oluşturun."
- `import.inspectUnreadable`
  EN: "The server’s answer about the archive could not be read, so there is no
  preview. Inspecting only reads the archive; nothing was changed. Try again."
  TR: "Sunucunun arşivle ilgili yanıtı okunamadı; bu yüzden önizleme yok.
  İnceleme arşivi yalnız okur; hiçbir şey değiştirilmedi. Tekrar deneyin."
- `import.result.complete`
  EN: "Every part you chose was imported, and {domain} is in service."
  TR: "Seçtiğiniz her parça içe aktarıldı ve {domain} hizmette."
- `import.partial.title`
  EN: "{domain} was imported in part"
  TR: "{domain} kısmen içe aktarıldı"
- `import.partial.body`
  EN: "The import has ended and does not continue by itself. The parts listed
  as not imported are missing; the others are on this server. {domain} was
  created and is kept, marked as not finished."
  TR: "İçe aktarım bitti ve kendiliğinden sürmez. İçe aktarılmadı diye
  listelenen parçalar eksik; diğerleri bu sunucuda. {domain} oluşturuldu ve
  korunuyor; tamamlanmadı olarak işaretli."
- `import.partial.unfinished`
  EN: "Every part was imported, but {domain} could not be marked as finished.
  The import has ended and does not continue by itself."
  TR: "Her parça içe aktarıldı, ancak {domain} tamamlandı olarak
  işaretlenemedi. İçe aktarım bitti ve kendiliğinden sürmez."
- `import.partial.imported`
  EN: "Imported"
  TR: "İçe aktarıldı"
- `import.partial.notImported`
  EN: "Not imported"
  TR: "İçe aktarılmadı"
- `import.partial.next`
  EN: "To finish, either add the missing parts by hand on the pages of
  {domain}, or remove {domain} on the Domains page, correct what each step
  below names, and import the archive again. An import into a domain that
  already exists is refused, so nothing is imported twice."
  TR: "Tamamlamak için ya eksik parçaları {domain} alan adının sayfalarında
  elle ekleyin ya da {domain} alan adını Alan Adları sayfasında kaldırın,
  aşağıdaki her adımın adını verdiği şeyi düzeltin ve arşivi yeniden içe
  aktarın. Zaten var olan bir alan adına içe aktarım reddedilir; bu yüzden
  hiçbir şey iki kez içe aktarılmaz."
- `import.partial.domains`
  EN: "Open Domains"
  TR: "Alan adlarını aç"
- `import.stepsTitle`
  EN: "Each step"
  TR: "Adım adım"
- `import.step.done`
  EN: "Imported"
  TR: "İçe aktarıldı"
- `import.step.notDone`
  EN: "Not imported"
  TR: "İçe aktarılmadı"
- `import.part.domain`
  EN: "Domain and site"
  TR: "Alan adı ve site"
- `import.part.files`
  EN: "Website files"
  TR: "Site dosyaları"
- `import.part.mail`
  EN: "Mail accounts"
  TR: "Posta hesapları"
- `import.part.mailbox`
  EN: "Mailbox {name}"
  TR: "{name} posta kutusu"
- `import.part.forwarders`
  EN: "Forwarders"
  TR: "Yönlendirmeler"
- `import.part.forwarder`
  EN: "Forwarder {name}"
  TR: "{name} yönlendirmesi"
- `import.part.dns`
  EN: "DNS records"
  TR: "DNS kayıtları"
- `import.part.databases`
  EN: "Databases"
  TR: "Veritabanları"
- `import.part.database`
  EN: "Database {name}"
  TR: "{name} veritabanı"
- `import.part.finalize`
  EN: "Marking the domain as finished"
  TR: "Alan adını tamamlandı olarak işaretleme"
- `import.detail.noPassword`
  EN: "Not imported: the archive holds no password for this mailbox. Create it
  on the domain’s mail page with a new password."
  TR: "İçe aktarılmadı: arşivde bu posta kutusunun parolası yok. Onu alan
  adının posta sayfasında yeni bir parolayla oluşturun."
- `import.siteNotCreated`
  EN: "The import did not start: the site for this domain could not be created
  on this server, so no file, mailbox, DNS record or database of the archive
  was imported. Whether a part of the new site itself was left behind is not
  known here: open Domains to see whether the domain is listed. On the server,
  sudo journalctl -u celikpanel-agent shows the step that failed; correct it,
  then start the import again. Nothing starts it again automatically."
  TR: "İçe aktarım başlamadı: bu alan adının sitesi bu sunucuda oluşturulamadı;
  bu yüzden arşivden hiçbir dosya, posta kutusu, DNS kaydı ya da veritabanı içe
  aktarılmadı. Yeni sitenin kendisinden bir parçanın geride kalıp kalmadığı
  burada bilinmiyor: alan adının listede olup olmadığını görmek için Alan
  Adları sayfasını açın. Sunucuda sudo journalctl -u celikpanel-agent komutu
  başarısız olan adımı gösterir; onu düzeltin, sonra içe aktarımı yeniden
  başlatın. Hiçbir şey onu kendiliğinden yeniden başlatmaz."

**certbot'un yerine getirmediği sertifika isteği (`POST /api/v1/domains/{id}/ssl/letsencrypt`).**

- `502 CERTIFICATE_ISSUE_FAILED`, reason `authority_unreachable`
  API: "No certificate was issued: this server could not reach the certificate
  authority, so no request was placed with it. The server owner checks that
  this server can open HTTPS connections to the internet (DNS resolution,
  outbound port 443, the system clock), then requests the certificate here
  again."
- `502 CERTIFICATE_ISSUE_FAILED`, reason `validation`
  API: "No certificate was issued: the certificate authority could not validate
  one of the names. Each name of the site must resolve publicly to this server
  and answer on port 80 from the internet. The domain's owner corrects the DNS
  records (or the firewall in front of this server), waits until public DNS
  shows them, then requests the certificate here again."
- `502 CERTIFICATE_ISSUE_FAILED`, reason `rate_limited`
  API: "No certificate was issued: the certificate authority refused the
  request because one of its limits was reached. A new request before the limit
  resets is refused the same way and counts against it. The line from certbot
  names the limit and, when the authority says so, when it resets; request the
  certificate here again after that time."
- `502 CERTIFICATE_ISSUE_FAILED`, reason `timeout`
  API: "No certificate was issued: certbot did not finish within the time
  allowed and was stopped. Why it took that long is not known from this answer.
  The server owner reads /var/log/celikpanel/certbot/letsencrypt.log on the
  server, then requests the certificate here again."
- `502 CERTIFICATE_ISSUE_FAILED`, reason `tool`
  API: "No certificate was issued: certbot ended with an error. Which step
  failed is not classified here; the line from certbot says what it reported.
  The server owner reads /var/log/celikpanel/certbot/letsencrypt.log on the
  server, corrects what it names, then requests the certificate here again."
- ardından, sitenin önceden sertifikası varsa
  API: "The certificate this site already had is still in place and keeps
  serving."
- ya da yoksa
  API: "The site has no certificate from this request and is served as before."
- ve her zaman en sonda
  API: "Nothing asks again automatically."
- `ssl.issueFailure.authority_unreachable`
  EN: "No certificate was issued for {domain}: this server could not reach the
  certificate authority, so no request was placed with it. The site is served
  as before, with the certificate it already had if it had one. Check that this
  server can open HTTPS connections to the internet (DNS resolution, outbound
  port 443, the system clock), then request the certificate here again. Nothing
  asks again automatically."
  TR: "{domain} için sertifika çıkarılmadı: bu sunucu sertifika otoritesine
  ulaşamadı; bu yüzden otoriteye bir istek iletilmedi. Site eskisi gibi
  sunuluyor; önceden bir sertifikası varsa o yerinde duruyor. Bu sunucunun
  internete HTTPS bağlantısı açabildiğini denetleyin (DNS çözümleme, giden 443
  numaralı port, sistem saati), sonra sertifikayı buradan yeniden isteyin.
  Hiçbir şey kendiliğinden yeniden istemez."
- `ssl.issueFailure.validation`
  EN: "No certificate was issued for {domain}: the certificate authority could
  not validate one of the names. The site is served as before, with the
  certificate it already had if it had one. Each name must resolve publicly to
  this server and answer on port 80 from the internet. Correct the DNS records
  (or the firewall in front of this server), wait until public DNS shows them,
  then request the certificate here again. Nothing asks again automatically."
  TR: "{domain} için sertifika çıkarılmadı: sertifika otoritesi adlardan birini
  doğrulayamadı. Site eskisi gibi sunuluyor; önceden bir sertifikası varsa o
  yerinde duruyor. Her ad genel DNS’te bu sunucuya çözülmeli ve internetten 80
  numaralı portta yanıt vermelidir. DNS kayıtlarını (ya da bu sunucunun
  önündeki güvenlik duvarını) düzeltin, genel DNS onları gösterene dek
  bekleyin, sonra sertifikayı buradan yeniden isteyin. Hiçbir şey kendiliğinden
  yeniden istemez."
- `ssl.issueFailure.rate_limited`
  EN: "No certificate was issued for {domain}: the certificate authority
  refused the request because one of its limits was reached. The site is served
  as before, with the certificate it already had if it had one. A new request
  before the limit resets is refused the same way and counts against it.
  Request the certificate here again after the limit resets; nothing asks again
  automatically."
  TR: "{domain} için sertifika çıkarılmadı: sertifika otoritesi, sınırlarından
  birine ulaşıldığı için isteği reddetti. Site eskisi gibi sunuluyor; önceden
  bir sertifikası varsa o yerinde duruyor. Sınır sıfırlanmadan yapılan yeni bir
  istek aynı biçimde reddedilir ve sınıra sayılır. Sertifikayı sınır
  sıfırlandıktan sonra buradan yeniden isteyin; hiçbir şey kendiliğinden
  yeniden istemez."
- `ssl.issueFailure.timeout`
  EN: "No certificate was issued for {domain}: certbot did not finish within
  the time allowed and was stopped. Why it took that long is not known here.
  The site is served as before, with the certificate it already had if it had
  one. On the server, read /var/log/celikpanel/certbot/letsencrypt.log, then
  request the certificate here again. Nothing asks again automatically."
  TR: "{domain} için sertifika çıkarılmadı: certbot tanınan sürede bitmedi ve
  durduruldu. Neden o kadar sürdüğü burada bilinmiyor. Site eskisi gibi
  sunuluyor; önceden bir sertifikası varsa o yerinde duruyor. Sunucuda
  /var/log/celikpanel/certbot/letsencrypt.log dosyasını okuyun, sonra
  sertifikayı buradan yeniden isteyin. Hiçbir şey kendiliğinden yeniden
  istemez."
- `ssl.issueFailure.tool`
  EN: "No certificate was issued for {domain}: certbot ended with an error. The
  site is served as before, with the certificate it already had if it had one.
  On the server, read /var/log/celikpanel/certbot/letsencrypt.log, correct what
  it names, then request the certificate here again. Nothing asks again
  automatically."
  TR: "{domain} için sertifika çıkarılmadı: certbot hata ile sonlandı. Site
  eskisi gibi sunuluyor; önceden bir sertifikası varsa o yerinde duruyor.
  Sunucuda /var/log/celikpanel/certbot/letsencrypt.log dosyasını okuyun, adını
  verdiği şeyi düzeltin, sonra sertifikayı buradan yeniden isteyin. Hiçbir şey
  kendiliğinden yeniden istemez."
- `ssl.issueFailure.said`
  EN: "certbot reported: {detail}"
  TR: "certbot’un bildirdiği: {detail}"

**Her biri nerede çizilir.** Hizmet işlemi cümleleri: bir bileşenin sayfasının
ve bileşenler listesinin bildirimi (`ServiceActionNotice`). O ekranlar Başlat,
Durdur ve Yeniden başlat gönderir; Yeniden yükle Panel'e API üzerinden ulaşır,
bu yüzden dört yeniden yükleme cümlesi orada yalnızca bir API istemcisinin
kullanıcısına, tarayıcı kaydında ise taklit bir yanıtla gösterilir. İçe
aktarım: önizlemede posta hesaplarının altındaki satır, sonuç (`ImportPage`) ve
Panel'in bir reddi; ret artık bir bildirimle kaybolmak yerine sayfada kalır
(`ErrorBanner`). Bir adımın kendi satırı (`detail`), parolası olmayan posta
kutusununki dışında, sunucunun İngilizce metnidir. Sertifika: SSL/TLS
sekmesinde, kapatılana ya da sertifika yeniden istenene dek kalan bir bildirim
(`CertificateIssueNotice`); certbot'un satırı yalnızca yöneticiye gösterilir.

**Sınırlar.** Veritabanı ve veritabanı kullanıcısı pencereleri, sunucu parolayı
kendisi ürettiğinde onu hâlâ İngilizce bir bildirimde gösterir; çağıran
yazdıysa hiçbir şey gösterilmez, çünkü sunucu onu artık geri göndermez. Yerel
döngü taklidine karşı gerçek bir Chrome'da incelendi; gerçek bir Panel'de
değil.

### Son gerçek sistem turundan sonra: reddedilen site neyin kaldırıldığını söyler, durmuş hizmet yeniden yüklenmez, içe aktarım neyi dışarıda bıraktığını adlandırır, Durdur geride ne bıraktığını söyler, güncelleme kartı daha önce ne olduğunu söyler (2026-10-12)

Bileşen testleriyle kaynak durumu; Arch üzerindeki gerçek sistem denetimi
bekliyor ve kurulu bir sunucuda hiçbir şey gözlenmedi. Neyin ölçüldüğü ve neyin
değiştiği için aynı tarihli dayanıklılık sözleşmesi kaydına bakın. Bu kayıt
cümleleri tutar.

**Metinlerin izlediği kural.** Bir yanıt, bir şeyin kaldırıldığını yalnızca
kaldırma doğrulandığında söyler; doğrulanmadığında nereye bakılacağını söyler.
Hiçbiri sunucuda hiçbir şeyin kalmadığını söylemez. Çalışmayan bir hizmet,
hangi hizmet olursa olsun, yeniden yüklenmemiştir. Sahibin beklemeyeceği bir
yerel durum bırakan başarı bunu söyler ve o durumu hizmet yöneticisinin
kaydettiği gibi bırakır. Arşivin içe aktarılmayan bir girdisi adıyla listelenir
ve eksik kalan seçilmiş bir parçayla karıştırılmaz. Bu sunucuda daha önce
başarısız olmuş bir sürüm, yeniden başlatılmadan önce öyle adlandırılır ve yine
de başlatılabilir.

**Yerini alan.** 2026-10-01 kaydında yazılı
`panelUpdate.previousAttempt.recovered` cümlesi ("... ve sunucu önceki sürüme
döndürüldü. Neden giderilmediyse yeniden başlatmak aynı güncellemeyi
tekrarlar.") artık gösterilmez; geri döndürülmüş bir sürümün başlığı da artık
`panelUpdate.previousAttempt.title` değildir. Aşağıdakiler gösterilir. Web
sunucusunun reddettiği bir site eskiden `500 INTERNAL` "internal server error"
yanıtı alıyordu.

**Web sunucusunun reddettiği site (`POST /api/v1/domains/create`, `POST /api/v1/import/cpanel/apply`).**

`vars`: `domain`, `command` (`sudo nginx -t`). `details`: nginx'in kendi
satırı, tek satır, yalnızca yönetici için; sunucudaki yolları adlandırabilir.

- `502 SITE_WEB_SERVER_REFUSED`, reason `removed`
  API: "The site {domain} was not created: the web server (nginx) refused the
  configuration CelikPanel generated for it, so the site was never put into
  service. What had been created for it was removed again and the removal was
  confirmed: its web server configuration, its system account, its files and,
  for a PHP site, its PHP pool. nginx was reloaded with the configuration it
  had before. The server owner runs sudo nginx -t on the server. If it reports
  an error now, a file of the server's own nginx configuration is refused and
  is corrected first. If it passes, what nginx refused was in the configuration
  CelikPanel generated for this server; the line nginx printed names it and is
  shown to administrators. Then create the site again; nothing retries by
  itself."
- `domains.add.webServerRefused.removed`
  EN: "{domain} was not created: the web server (nginx) refused the
  configuration CelikPanel generated for it. What had been created for the site
  was removed again, and the removal was confirmed: its web server
  configuration, system account, files and, for a PHP site, PHP pool. nginx was
  reloaded with the configuration it had before. On the server, run {command}.
  If it reports an error, a file of the server’s own nginx configuration is
  refused; correct that first. If it passes, the refusal came from the
  configuration CelikPanel generated, and the line nginx printed names it
  (administrators see it below). Then create the site again; nothing retries by
  itself."
  TR: "{domain} oluşturulmadı: web sunucusu (nginx), CelikPanel’in bu site için
  ürettiği yapılandırmayı reddetti. Site için oluşturulanlar yeniden kaldırıldı
  ve bu kaldırma doğrulandı: web sunucusu yapılandırması, sistem hesabı,
  dosyaları ve PHP sitesiyse PHP havuzu. nginx, önceki yapılandırmasıyla
  yeniden yüklendi. Sunucuda {command} komutunu çalıştırın. Bir hata
  bildiriyorsa sunucunun kendi nginx yapılandırmasındaki bir dosya
  reddediliyordur; önce onu düzeltin. Geçiyorsa ret CelikPanel’in ürettiği
  yapılandırmadan gelmiştir ve nginx’in yazdığı satır onu adlandırır
  (yöneticiler aşağıda görür). Ardından siteyi yeniden oluşturun; hiçbir şey
  kendiliğinden yeniden denemez."
- `502 SITE_WEB_SERVER_REFUSED`, reason `cleanup_unconfirmed`
  API: "The site {domain} was not created: the web server (nginx) refused the
  configuration CelikPanel generated for it, so the site was never put into
  service. nginx was reloaded with the configuration it had before. Removing
  what had been created for the site was not confirmed, so parts of it may
  remain on the server: open Domains and, if {domain} is listed there, delete
  it, which removes its parts. The server owner runs sudo nginx -t on the
  server. If it reports an error now, a file of the server's own nginx
  configuration is refused and is corrected first. If it passes, what nginx
  refused was in the configuration CelikPanel generated for this server; the
  line nginx printed names it and is shown to administrators. Then create the
  site again; nothing retries by itself."
- `domains.add.webServerRefused.unconfirmed`
  EN: "{domain} was not created: the web server (nginx) refused the
  configuration CelikPanel generated for it. nginx was reloaded with the
  configuration it had before. Removing what had been created for the site was
  not confirmed, so parts of it may remain on the server: open Domains and, if
  {domain} is listed there, delete it, which removes its parts. On the server,
  run {command}. If it reports an error, a file of the server’s own nginx
  configuration is refused; correct that first. If it passes, the refusal came
  from the configuration CelikPanel generated, and the line nginx printed names
  it (administrators see it below). Then create the site again; nothing retries
  by itself."
  TR: "{domain} oluşturulmadı: web sunucusu (nginx), CelikPanel’in bu site için
  ürettiği yapılandırmayı reddetti. nginx, önceki yapılandırmasıyla yeniden
  yüklendi. Site için oluşturulanların kaldırıldığı doğrulanamadı; bu yüzden
  bir bölümü sunucuda kalmış olabilir: Alan Adları sayfasını açın ve {domain}
  orada listeleniyorsa silin; silme, parçalarını da kaldırır. Sunucuda
  {command} komutunu çalıştırın. Bir hata bildiriyorsa sunucunun kendi nginx
  yapılandırmasındaki bir dosya reddediliyordur; önce onu düzeltin. Geçiyorsa
  ret CelikPanel’in ürettiği yapılandırmadan gelmiştir ve nginx’in yazdığı
  satır onu adlandırır (yöneticiler aşağıda görür). Ardından siteyi yeniden
  oluşturun; hiçbir şey kendiliğinden yeniden denemez."
- `502 SITE_WEB_SERVER_REFUSED`, reason `import_removed`
  API: "The import did not start, and no file, mailbox, DNS record or database
  of the archive was imported. The site {domain} is the import's first step and
  it was not created: the web server (nginx) refused the configuration
  CelikPanel generated for it, so the site was never put into service. What had
  been created for it was removed again and the removal was confirmed: its web
  server configuration, its system account, its files and, for a PHP site, its
  PHP pool. nginx was reloaded with the configuration it had before. The server
  owner runs sudo nginx -t on the server. If it reports an error now, a file of
  the server's own nginx configuration is refused and is corrected first. If it
  passes, what nginx refused was in the configuration CelikPanel generated for
  this server; the line nginx printed names it and is shown to administrators.
  Then start the import again; nothing starts it again automatically."
- `import.webServerRefused.removed`
  EN: "The import did not start, and no file, mailbox, DNS record or database
  of the archive was imported. Its first step is the site {domain}, which was
  not created: the web server (nginx) refused the configuration CelikPanel
  generated for it. What had been created for the site was removed again, and
  the removal was confirmed: its web server configuration, system account,
  files and, for a PHP site, PHP pool. nginx was reloaded with the
  configuration it had before. On the server, run {command}. If it reports an
  error, a file of the server’s own nginx configuration is refused; correct
  that first. If it passes, the refusal came from the configuration CelikPanel
  generated, and the line nginx printed names it (administrators see it below).
  Then start the import again; nothing starts it again automatically."
  TR: "İçe aktarma başlamadı ve arşivden hiçbir dosya, posta kutusu, DNS kaydı
  ya da veritabanı içe aktarılmadı. İlk adımı {domain} sitesidir ve bu site
  oluşturulmadı: web sunucusu (nginx), CelikPanel’in bu site için ürettiği
  yapılandırmayı reddetti. Site için oluşturulanlar yeniden kaldırıldı ve bu
  kaldırma doğrulandı: web sunucusu yapılandırması, sistem hesabı, dosyaları ve
  PHP sitesiyse PHP havuzu. nginx, önceki yapılandırmasıyla yeniden yüklendi.
  Sunucuda {command} komutunu çalıştırın. Bir hata bildiriyorsa sunucunun kendi
  nginx yapılandırmasındaki bir dosya reddediliyordur; önce onu düzeltin.
  Geçiyorsa ret CelikPanel’in ürettiği yapılandırmadan gelmiştir ve nginx’in
  yazdığı satır onu adlandırır (yöneticiler aşağıda görür). Ardından içe
  aktarmayı yeniden başlatın; hiçbir şey onu kendiliğinden yeniden başlatmaz."
- `502 SITE_WEB_SERVER_REFUSED`, reason `import_cleanup_unconfirmed`
  API: "The import did not start, and no file, mailbox, DNS record or database
  of the archive was imported. The site {domain} is the import's first step and
  it was not created: the web server (nginx) refused the configuration
  CelikPanel generated for it, so the site was never put into service. nginx
  was reloaded with the configuration it had before. Removing what had been
  created for the site was not confirmed, so parts of it may remain on the
  server: open Domains and, if {domain} is listed there, delete it, which
  removes its parts. The server owner runs sudo nginx -t on the server. If it
  reports an error now, a file of the server's own nginx configuration is
  refused and is corrected first. If it passes, what nginx refused was in the
  configuration CelikPanel generated for this server; the line nginx printed
  names it and is shown to administrators. Then start the import again; nothing
  starts it again automatically."
- `import.webServerRefused.unconfirmed`
  EN: "The import did not start, and no file, mailbox, DNS record or database
  of the archive was imported. Its first step is the site {domain}, which was
  not created: the web server (nginx) refused the configuration CelikPanel
  generated for it. nginx was reloaded with the configuration it had before.
  Removing what had been created for the site was not confirmed, so parts of it
  may remain on the server: open Domains and, if {domain} is listed there,
  delete it, which removes its parts. On the server, run {command}. If it
  reports an error, a file of the server’s own nginx configuration is refused;
  correct that first. If it passes, the refusal came from the configuration
  CelikPanel generated, and the line nginx printed names it (administrators see
  it below). Then start the import again; nothing starts it again
  automatically."
  TR: "İçe aktarma başlamadı ve arşivden hiçbir dosya, posta kutusu, DNS kaydı
  ya da veritabanı içe aktarılmadı. İlk adımı {domain} sitesidir ve bu site
  oluşturulmadı: web sunucusu (nginx), CelikPanel’in bu site için ürettiği
  yapılandırmayı reddetti. nginx, önceki yapılandırmasıyla yeniden yüklendi.
  Site için oluşturulanların kaldırıldığı doğrulanamadı; bu yüzden bir bölümü
  sunucuda kalmış olabilir: Alan Adları sayfasını açın ve {domain} orada
  listeleniyorsa silin; silme, parçalarını da kaldırır. Sunucuda {command}
  komutunu çalıştırın. Bir hata bildiriyorsa sunucunun kendi nginx
  yapılandırmasındaki bir dosya reddediliyordur; önce onu düzeltin. Geçiyorsa
  ret CelikPanel’in ürettiği yapılandırmadan gelmiştir ve nginx’in yazdığı
  satır onu adlandırır (yöneticiler aşağıda görür). Ardından içe aktarmayı
  yeniden başlatın; hiçbir şey onu kendiliğinden yeniden başlatmaz."

**Sunucunun çalıştırmadığı bir PHP sürümü (`POST /api/v1/domains/create`).**

`vars`: `version`, `installed`. Hiçbir şey oluşturulmadan reddedilir. Hiçbir
ekran yeni bir siteyle birlikte PHP sürümü göndermez; bu cümle yalnızca bir API
istemcisine ulaşır ve katalogda karşılığı yoktur.

- `409 PHP_VERSION_NOT_INSTALLED`
  API: "Nothing was created: PHP {version} is not installed on this server.
  Installed: {installed}. Choose one of these versions and create the site
  again; another version can be used only after it is installed on this server
  (Services)."

**Çalışmayan bir hizmetin yeniden yüklenmesi (`POST /api/v1/service/action`).**

Yeni cümle yok: 2026-10-11 kaydında yazılı `409 SERVICE_ACTION_FAILED`, gerekçe
`not_running` ve `err.SERVICE_ACTION_FAILED.not_running` artık yalnızca Postfix
ve Dovecot için değil, yeniden yükleme istendiğinde `inactive` ya da `failed`
olan her birim için yanıttır (nginx, MariaDB, PHP-FPM, PostgreSQL ve
sarmalayıcısı). `vars.detail` okunan şeydir; örneğin "nginx.service is inactive
(dead); nothing was reloaded".

**Başarılı olan ve birimi `failed` işaretli bırakan Durdur (`POST /api/v1/service/action`).**

Yanıt her zamanki başarıdır ve bir `note` taşır: `code`, `reason`, `error` ve
`vars` (`unit`, `failed_unit`, `result`, `command`; hizmetin kendi denetimi şu
an bir satır yazıyorsa `detail`). `command`, `sudo systemctl reset-failed
<failed_unit>` komutudur; CelikPanel onu çalıştırmaz.

- `200`, `note.code` `SERVICE_ACTION_NOTE`, `note.reason` `unit_marked_failed`
  API: "The service was stopped and is not running. systemd now shows its unit
  as failed, which it was not before the stop. That mark is systemd's own
  record of how the unit's stop went (with the result exit-code: a command of
  the unit exited with an error), and CelikPanel leaves it as it is. Start can
  be used from this state. To clear the mark without starting the service, the
  server owner runs the command shown."
- `services.action.note.unit_marked_failed`
  EN: "{unit} was stopped and is not running. systemd now shows {failed_unit}
  as failed (result: {result}), which it was not before the stop. That mark is
  systemd’s own record of how the unit’s stop went (with the result exit-code,
  a command of the unit exited with an error), and CelikPanel leaves it as it
  is. Start can be used from this state. To clear the mark without starting,
  run {command} on the server."
  TR: "{unit} durduruldu ve çalışmıyor. systemd şimdi {failed_unit} birimini
  failed (sonuç: {result}) olarak gösteriyor; durdurmadan önce öyle değildi. Bu
  işaret, systemd’nin birimin durdurulmasının nasıl geçtiğine dair kendi
  kaydıdır (sonuç exit-code ise birimin bir komutu hatayla çıkmıştır) ve
  CelikPanel onu olduğu gibi bırakır. Bu durumdan Başlat kullanılabilir.
  İşareti başlatmadan temizlemek için sunucuda {command} komutunu çalıştırın."
- `200`, `note.code` `SERVICE_ACTION_NOTE`, `note.reason` `unit_marked_failed_config`
  API: "The service was stopped and is not running. systemd now shows its unit
  as failed, which it was not before the stop. That mark is systemd's own
  record of how the unit's stop went (with the result exit-code: a command of
  the unit exited with an error), and CelikPanel leaves it as it is. The
  service's own check refuses its configuration at present, and the unit's stop
  command reads the same file; the service will not start until that is
  corrected. Start can be used from this state. To clear the mark without
  starting the service, the server owner runs the command shown."
- `services.action.note.unit_marked_failed_config`
  EN: "{unit} was stopped and is not running. systemd now shows {failed_unit}
  as failed (result: {result}), which it was not before the stop. That mark is
  systemd’s own record of how the unit’s stop went, and CelikPanel leaves it as
  it is. {unit} refuses its own configuration at present (its line is below),
  and the unit’s stop command reads the same file; it will not start until that
  is corrected. To clear the mark without starting, run {command} on the
  server."
  TR: "{unit} durduruldu ve çalışmıyor. systemd şimdi {failed_unit} birimini
  failed (sonuç: {result}) olarak gösteriyor; durdurmadan önce öyle değildi. Bu
  işaret, systemd’nin birimin durdurulmasının nasıl geçtiğine dair kendi
  kaydıdır ve CelikPanel onu olduğu gibi bırakır. {unit} şu an kendi
  yapılandırmasını reddediyor (satırı aşağıda) ve birimin durdurma komutu da
  aynı dosyayı okur; bu düzeltilene dek başlamaz. İşareti başlatmadan
  temizlemek için sunucuda {command} komutunu çalıştırın."

**Arşiv girdilerini dışarıda bırakan cPanel içe aktarımı (`POST /api/v1/import/cpanel/apply`).**

Arşivin mutlak yolla adlandırdığı bir girdi kendi adımıdır, `member:<ad>`; en
çok 20 tanesi listelenir, listelenmeyen n tanesi için tek bir `members:<n>`
adımı gelir. Başka eksik yoksa alan adı bitmiş diye işaretlenir. Dosya adımının
kendi satırı, arşivin ne kadarının site klasörünün dışında olduğunu da
sunucunun İngilizcesiyle söyler: "2 files, 59 bytes. 5 other entries of the
archive are outside the site folder (homedir/public_html) and are not copied by
this step: homedir/mail (2), homedir/etc (1), mysql (1), homedir (1). The
databases, mailboxes, forwarders and DNS records are read from their own
entries by their own steps; mailbox contents and the other folders of the home
directory are not imported". Tümüyle reddedilen bir dosya adımı girdiyi
adlandırır: "unsupported cpmove site entry type:
cpmove-user/homedir/public_html/uploads is a symbolic link".

- step `member:<name>`, `detail`
  API: "not imported: the archive names this entry with an absolute path, and
  an import writes only below the site's own folder; nothing was written for
  it"
- `import.detail.absoluteMember`
  EN: "Not imported: the archive names this entry with an absolute path, and an
  import writes only inside the site’s own folder. Nothing was written for it."
  TR: "İçe aktarılmadı: arşiv bu girdiyi mutlak bir yolla adlandırıyor; içe
  aktarma yalnızca sitenin kendi klasörünün içine yazar. Bu girdi için hiçbir
  şey yazılmadı."
- `200`, `status: partial`, `domain_status: active`, `message`
  API: "The import ended and every part that was chosen was imported; {domain}
  is in service. Imported: {imported}. Not imported: {not imported}. These
  entries of the archive were refused by their names, and nothing was written
  for them; the reason of each is in its step below. If one of them is a file
  the site needs, add it with the file manager of {domain}. Importing the
  archive again refuses the same entries; nothing continues by itself."
- `import.partial.entriesBody`
  EN: "The import has ended. Everything you chose was imported, and {domain} is
  in service. The archive entries listed as not imported were refused by their
  names; nothing was written for them."
  TR: "İçe aktarma sona erdi. Seçtiğiniz her şey içe aktarıldı ve {domain}
  yayında. İçe aktarılmadı diye listelenen arşiv girdileri adları yüzünden
  reddedildi; onlar için hiçbir şey yazılmadı."
- `import.partial.entriesNext`
  EN: "Nothing more is needed for {domain}. If one of these entries is a file
  the site needs, add it with the site’s file manager. Importing the archive
  again refuses the same entries."
  TR: "{domain} için başka bir şey gerekmiyor. Bu girdilerden biri sitenin
  ihtiyaç duyduğu bir dosyaysa onu sitenin dosya yöneticisiyle ekleyin. Arşivi
  yeniden içe aktarmak aynı girdileri yine reddeder."
- `import.part.member`
  EN: "Archive entry {name}"
  TR: "Arşiv girdisi {name}"
- `import.part.moreMembers`
  EN: "{name} more archive entries"
  TR: "{name} arşiv girdisi daha"

**Sunulan sürüm burada daha önce denendiğinde güncelleme kartı (`GET /api/v1/panel/update/check`, `previous_attempt`).**

Başlat düğmesinin üstünde, şu sırayla gösterilir: başlık, ne olduğu ve
sunucunun şu an neyi çalıştırdığı, kaydedilen neden
(`panelUpdate.previousAttempt.cause`, değişmedi) ya da neden kaydedilmediği, ve
yeniden başlatmanın ne yaptığı. Geri dönüşü kaydedilmemiş başarısız bir deneme
2026-10-01 tarihli başlığını ve cümlesini korur; artık neden kaydedilmediğinde
bunu da söyler. Bu bildirim Başlat düğmesini hiçbir zaman kapatmaz ve sürüm
hiçbir zaman gizlenmez.

- `panelUpdate.previousAttempt.rolledBackTitle`
  EN: "This version was already tried on this server and rolled back"
  TR: "Bu sürüm bu sunucuda daha önce denendi ve geri alındı"
- `panelUpdate.previousAttempt.recovered`
  EN: "{version} was started here on {time}. The update did not complete, and
  the server was returned to {current}, which it runs now."
  TR: "{version} burada {time} tarihinde başlatıldı. Güncelleme tamamlanmadı ve
  sunucu {current} sürümüne döndürüldü; şu an onu çalıştırıyor."
- `panelUpdate.previousAttempt.noCause`
  EN: "The server recorded no more specific cause for that attempt."
  TR: "Sunucu o deneme için daha belirli bir neden kaydetmedi."
- `panelUpdate.previousAttempt.again`
  EN: "Starting it again runs the same update. If the cause was on this server
  and has been corrected, the result can differ; otherwise expect the same one.
  A corrected version, when it is published, is offered here as a newer
  version. The button below still starts {version}."
  TR: "Yeniden başlatmak aynı güncellemeyi çalıştırır. Neden bu sunucudaysa ve
  giderildiyse sonuç değişebilir; değilse aynı sonucu bekleyin. Düzeltilmiş bir
  sürüm yayımlandığında burada daha yeni bir sürüm olarak sunulur. Aşağıdaki
  düğme {version} sürümünü yine başlatır."

**Her birinin çizildiği yer.** Reddedilen site: Alan Adı Ekle penceresi ve içe
aktarma sayfası (`ErrorBanner`); orada kalır. nginx'in satırı cümlenin altında,
program çıktısının yazı tipiyle çizilir. Durdur notu: bir bileşenin
sayfasındaki ve bileşen listesindeki bildirim (`ServiceActionNotice`);
`role="status"` ile dikkat yüzeyinde, asla hata yüzeyinde değil. Hizmetin
satırı ve komut, diğer sonuçlardaki gibi ayrı gösterilir. İçe aktarım: sonucun
iki listesi ve adımları (`ImportPage`). Güncelleme kartı: `PanelUpdateCard`.

**Sınırlar.** Alan Adı Ekle penceresi statik bir site oluşturur ve hiçbir zaman
PHP sürümü göndermez; bu yüzden tarayıcıda reddedilen bir siteye içe aktarımla
ya da tarayıcı kaydındaki taklit bir yanıtla ulaşılır. Barındırma türü ya da
sertifika değişikliği sırasındaki aynı ret hâlâ önceki hatasını yanıtlar. Dosya
adımının site klasörü dışındaki girdilere dair satırı sunucunun İngilizcesidir.
Gerçek bir Chrome'da, yerel taklit sunucuya karşı incelendi; gerçek bir
Panel'de değil.

### set4 gerçek sistem ölçümünden sonra: Durdur, hizmet durduğunda yanıtlanır ve birimi hakkında ne okunduğunu söyler, güncelleme kartı gösterdiği zamanın ne olduğunu söyler, içe aktarım aktarmadığını aktarılmış diye listelemez (2026-10-09)

Bileşen testleri, yerel taklit sunucuya karşı gerçek bir Chrome ve ilk işin tek
kullanımlık konuklarda (Ubuntu 24.04 ve Debian 13; kanıt
`deploy/e2e/release-recovery/evidence/set4b-20261009/`) gerçek sistem ölçümüyle
kaynak durumu. Kurulu bir sunucuda hiçbir şey gözlenmedi. 2026-10-09 bu kaydın
takvim tarihidir; üstündeki başlıklarda yer alan 2026-10-10 ile 2026-10-12
arasındaki tarihler tur etiketleridir. Neyin ölçüldüğü ve neyin değiştiği için
aynı tarihli dayanıklılık sözleşmesi kaydına bakın. Bu kayıt cümleleri tutar.

**Metinlerin izlediği kural.** Durdur, hizmetin kendi sürecinin gittiği
görüldüğünde durduruldu diye yanıtlanır; daha önce değil. Yanıtın birim
hakkında söylediği, birim durulduktan sonra systemd’nin gösterdiğidir; bu
okunamadıysa yanıt okunamadığını söyler, bir işaret olduğunu da olmadığını da
ileri sürmez. Ekrandaki bir zaman, ne ise o adla anılır. İçe aktarılmayan bir
parça, ister başarısız olsun, ister seçilmemiş olsun, ister sahibin
sağlayıcısına bırakılmış olsun, ister arşivde ona ait hiçbir şey bulunmasın,
içe aktarılmış diye listelenmez.

**Yerini alan.** 2026-10-12 kaydında yazılı
`panelUpdate.previousAttempt.recovered` cümlesi ("{version} burada {time}
tarihinde başlatıldı. ...") artık gösterilmez: `{time}` denemenin bittiği andır
ve aşağıdaki cümle bunu söyler. Bir içe aktarımın `imported` listesi, adımı
hiçbir şey aktarmamış bir parçayı artık adlandırmaz. Ubuntu 24.04 üzerinde,
Postfix’in reddettiği bir main.cf ile Postfix’in durdurulması eskiden
2026-10-12 tarihli `note` olmadan yanıtlanıyordu; artık onu taşır.

**Biriminin kendi durdurulması okunamayan Durdur (`POST
/api/v1/service/action`).**

2026-10-12 tarihli notların (`unit_marked_failed`, `unit_marked_failed_config`)
sözleri değişmedi. Artık birim durulduktan sonra alınan bir okumadan
verilirler: systemd birimi iki durum arasında (`deactivating`) gösterdiği
sürece Agent onu yeniden okur; bir durdurmanın bütün birimleri için toplam en
çok 30 okuma, 500 ms arayla (yaklaşık 15 saniye); bu sırada `systemctl show`
dışında hiçbir şey göndermez. Bu bekleme birim hâlâ iki durum arasındayken
biterse ya da birim durdurmadan önce okunduğu halde sonra okunamazsa, başarı
kendi notunu taşır: `code` `SERVICE_ACTION_NOTE`, `reason` `unit_not_settled`
ya da `unit_state_not_read`, `vars`: `unit`, `pending_unit`, `command`
(`systemctl status <pending_unit>`; yalnızca birimi gösterir, CelikPanel onu
çalıştırmaz) ve `unit_not_settled` için `state` (okunan son durum, örneğin
`deactivating`). İki not da birimin failed olarak işaretlendiğini söylemez;
işaretlenmediğini de söylemez.

- `200`, `note.code` `SERVICE_ACTION_NOTE`, `note.reason` `unit_not_settled`
  API: "The service was stopped and is not running. systemd had not finished
  stopping its unit when CelikPanel stopped waiting for it: the unit was still
  between two states, so how its stop ended was not read, and it may end marked
  as failed. The server owner runs the command shown to see the unit as systemd
  shows it now. CelikPanel sends nothing more to the unit and does not look
  again by itself."
- `services.action.note.unit_not_settled`
  EN: "{unit} was stopped and is not running. systemd had not finished stopping
  {pending_unit} when CelikPanel stopped waiting for it (its state was still
  {state}), so how that stop ended was not read; the unit may end marked as
  failed. To see it as systemd shows it now, run {command} on the server.
  Nothing more is sent to the unit, and nothing looks again by itself."
  TR: "{unit} durduruldu ve çalışmıyor. CelikPanel beklemeyi bıraktığında
  systemd {pending_unit} birimini durdurmayı bitirmemişti (durumu hâlâ {state}
  idi); bu yüzden durdurmanın nasıl bittiği okunmadı ve birim failed olarak
  işaretlenmiş olabilir. systemd’nin şu an ne gösterdiğini görmek için sunucuda
  {command} komutunu çalıştırın. Birime başka bir şey gönderilmez ve hiçbir şey
  kendiliğinden yeniden bakmaz."
- `200`, `note.code` `SERVICE_ACTION_NOTE`, `note.reason` `unit_state_not_read`
  API: "The service was stopped and is not running. The state of its unit could
  not be read from systemd after the stop, so whether the unit ended marked as
  failed is not known. The server owner runs the command shown to see the unit
  as systemd shows it now. CelikPanel sends nothing more to the unit and does
  not look again by itself."
- `services.action.note.unit_state_not_read`
  EN: "{unit} was stopped and is not running. The state of {pending_unit} could
  not be read from systemd after the stop, so it is not known whether the unit
  ended marked as failed. To see it as systemd shows it now, run {command} on
  the server. Nothing more is sent to the unit, and nothing looks again by
  itself."
  TR: "{unit} durduruldu ve çalışmıyor. Durdurmadan sonra {pending_unit}
  biriminin durumu systemd’den okunamadı; bu yüzden birimin failed olarak
  işaretlenip işaretlenmediği bilinmiyor. systemd’nin şu an ne gösterdiğini
  görmek için sunucuda {command} komutunu çalıştırın. Birime başka bir şey
  gönderilmez ve hiçbir şey kendiliğinden yeniden bakmaz."

**Geri döndürülmüş bir deneme için güncelleme kartının cümlesi (`GET
/api/v1/panel/update/check`, `previous_attempt`).**

`{time}`, yanıtın taşıdığı tek zaman olan `previous_attempt.finished_at`
değeridir: denemenin bitişinin kaydedildiği an. Sunucu bir deneme için
başlangıç zamanı kaydetmez; cümle de bir başlangıç zamanı söylemez. Başlık,
neden satırı ve yeniden başlatmaya dair cümle 2026-10-12 tarihindekilerdir.

- `panelUpdate.previousAttempt.recovered`
  EN: "{version} was started here, and that attempt ended on {time}. The update
  did not complete, and the server was returned to {current}, which it runs
  now."
  TR: "{version} burada başlatıldı ve o deneme {time} tarihinde sona erdi.
  Güncelleme tamamlanmadı ve sunucu {current} sürümüne döndürüldü; şu an onu
  çalıştırıyor."

**cPanel içe aktarımı: içe aktarılmayan ve başarısız da olmayan parça (`POST
/api/v1/import/cpanel/apply`).**

Hatasız biten ve hiçbir şey içe aktarmayan adım `ok: true` kalır ve `state`
taşır: `left_to_owner` (DNS’i sahibinin dış sağlayıcısında olan bir sunucunun
DNS’i: kayıtlar orada kalır), `not_chosen` (arşivin DNS kayıtları seçilmedi;
panelin alan adı için kendi kayıtları yine de oluşturuldu ya da yayımlandı),
`none_in_archive` (parça seçildi ve arşivde bu alan adı için ona ait hiçbir şey
yok: yönlendirme yok, posta kutusu yok, bölge yok, site klasöründe dosya yok),
`none_imported` (parça seçildi, arşivde ona ait bir şeyler var ve hiçbiri
aktarılmadı; her biri kendi adımındadır). Yanıtta `imported` ve `not_imported`
yanında üçüncü bir liste vardır: `left_out`; bir parça üçünden yalnızca
birindedir. Böyle bir adım içe aktarımı hiçbir zaman `partial` yapmaz. Her
adımın `detail` satırı değişmedi ve sunucunun İngilizcesidir ("external DNS
ownership preserved; verify provider records before publishing the site", "0
forwarders", "panel DNS template created; archive DNS import was not
selected"). Dış DNS kipindeki bir sunucuda DNS istemeyen bir içe aktarım
eskiden `imported: [domain, files, dns, ...]` yanıtını veriyordu; artık
`imported: [domain, files, ...]`, `left_out: [dns]` yanıtını verir (posta
seçildiyse ve arşivde yönlendirme yoksa `forwarders` da `left_out`
listesindedir).

Sayfada adım, adımlar listesindeki yerini kendi satırıyla korur; işareti ne içe
aktarılmış bir adımın ne de başarısız bir adımın işareti olan yansız bir
işarettir; ekran okuyucuya adımın adından sonra aşağıdaki sözler verilir.
Parça, kısmi bir sonucun özetindeki iki listenin hiçbirinde yer almaz.

- `import.step.nothing`
  EN: "Nothing imported, nothing failed"
  TR: "İçe aktarılan yok, hata da yok"

**Her birinin çizildiği yer.** İki not: bir bileşenin sayfasındaki ve
bileşenler listesindeki bildirim (`ServiceActionNotice`); `role="status"` ile
dikkat yüzeyinde, hiçbir zaman hata yüzeyinde değil; komut ayrı gösterilir.
Güncelleme kartı: `PanelUpdateCard`. İçe aktarım: sonucun listeleri ve adımları
(`ImportPage`).

**Sınırlar.** Hiçbir ölçüm bir birimi beklemenin tamamı boyunca iki durum
arasında tutmadı, hiçbiri de bir birimi okunamaz kılmadı: iki yeni not gerçek
bir sunucuyla değil, bileşen testleriyle ve yerel taklit sunucuya karşı gerçek
bir Chrome ile kapsanır. Sayfada dışarıda bırakılan parçaların bir listesi
yoktur; onlar adımların arasındadır. Bir adımın satırı sunucunun
İngilizcesidir. Geri döndürülmeden başarısız olan ya da hiçbir şeyi
değiştirmeden duran bir denemenin cümleleri
(`panelUpdate.previousAttempt.failed`, `.stopped`) aynı zamanı "{time}
tarihinde denendi" diye taşır ve değişmedi. Düzeltilmiş kaynakla bir konukta
ölçülenler: Ubuntu 24.04 ve Debian 13 üzerinde Postfix’in durdurulmasının notu
(her birinde dört Durdur’un dördü) ve her birinde bir içe aktarımın listeleri
(`left_out: [dns]`); `not_chosen`, `none_in_archive` ve `none_imported`
durumları, iki yeni not ve bütün ekranlar ölçülmedi.

### postconf'tan okunan değer tek bir satırdır: Postfix'in kendi uyarısı asla değer sayılmaz, okunamayan bir ayar da değişikliği başlamadan durdurur (2026-10-09)

Bileşen testleri ve gerçek postconf'un özel bir yapılandırma dizinine karşı tek
bir gerçek sistem okumasıyla (Debian 13 geliştirme konuğunda Postfix 3.10.13;
kanıt `deploy/e2e/release-recovery/evidence/set4c-20261009/`) kaynak durumu.
Bunun için hiçbir sunucuda ya da konukta posta TLS değişikliği, sertifika ya da
geri alma çalıştırılmadı. Kurulu bir sunucuda hiçbir şey gözlenmedi. Neyin
okunduğu ve neyin değiştiği için aynı tarihli ve aynı başlıklı dayanıklılık
sözleşmesi kaydına bakın. Bu kayıt tek cümleyi tutar.

**Metnin izlediği kural.** Agent'ın değerini tek bir değer olarak okuyamadığı
ayar bilinmeyendir. Tahmin edilmez, geri yazılmaz, karşılaştırılmaz. Yanıt
ayarın ve gönderilen okumanın adını verir, sunucu sahibinin sunucuda neyi
çalıştıracağını ve hiçbir şeyin kendiliğinden yeniden denemediğini söyler.
postconf'un yazdığını yinelemez.

**Okunamayan bir Postfix ayarı (Agent'ın nedeni, sunucunun İngilizcesiyle).**

Üç yanıtın nedenidir; her biri neyi değiştirdiğini ya da değiştirmediğini
ekler: anlık görüntüsünde duran posta TLS değişikliği ("mail TLS snapshot:
nothing was changed: ..."), bir posta TLS değişikliğinden sonraki ya da bir
posta sertifikası yayımından önceki geri okuma ("read back postconf {setting}:
the configuration was not verified: ..."; sertifika yolunda sahip yine o yolun
kendi cümlesini görür: "mail certificate publication paused: ...") ve posta
kurulumunun alias veritabanı onarımı. Bunlar Panel'e, başarısız olan işlem için
Agent'ın nedeni olarak döner; bu nedeni hangi ekranın, ne kadarını gösterdiği
incelenmedi. `{command}`, `postconf -h {setting}` ya da `postconf -x -h
{setting}` komutudur.

- `postconfUnreadError`
  API: "the value of the Postfix setting {setting} is not known: `{command}`
  did not print exactly one value line besides its own messages. The server
  owner runs `postconf -n` and `postfix check` on the server; they name the
  line of /etc/postfix/main.cf that Postfix objects to. After it is corrected
  the same operation can be started again; nothing retries by itself"

**Sınırlar.** postconf'un olağan bir uyarısıyla (başka bir metinden sonra yorum
taşıyan satır, kullanılmayan parametre, eksik master.cf) bu neden hiç görünmez:
değer okunur ve işlem sürer; değişiklik de budur. Neden yalnızca, yanıt
postconf'un kendi iletileri dışında tek bir değer satırı olmadığında görünür;
hiçbir ölçüm bunu üretmedi, bileşen testleriyle kapsanır. Yol her zaman
`/etc/postfix/main.cf` olarak verilir; Postfix'i başka bir dizini okuyan
sunucuya doğru komutlar ve alışılmış yol söylenir. Türkçe cümle yoktur: bir
ekranın değil, Agent'ın nedenidir. Hiçbir ekran değiştirilmedi.

### Alpha.82 güncellemesinden sonra kurulu sunucularda sahibin gözlemleri (2026-10-10)

Gözlemlerdir, değişiklik değildir. Sahip, kurulu iki sunucuyu (biri Ubuntu
24.04, biri Debian 13) 10 Ekim 2026'da panelin kendi güncelleme ekranından
v0.1.0-alpha.82'ye güncelledikten sonra aşağıdaki dört şeyi kendi
ekranlarından bildirdi. Hiçbir asistan o sunuculara bakmadı; hiçbiri
v0.1.0-alpha.82'de düzeltilmedi ve bu girdi için hiçbir metin ya da ekran
değiştirilmedi. Her biri ilgili olduğu kuralla birlikte verilir: D-024 (mevcut
neden, kimin işlem yapacağı, sonraki eylem ve işin nasıl süreceği uzun
listelerden önce gelir; doğrulanmış hata, eksik önkoşul ve bilinmeyen sonuç
ayrı tutulur) ve bilinen-durum kuralı (ekran doğrulananı söyler; bilinmeyen
durum bağlı bir sayfanın yerini almaz ve sessiz süre geçmeden çizilmez).
Sonraki sürüm için yol haritasında ve aynı tarihli dayanıklılık sözleşmesi
girdisinde listelenmiştir.

1. **Panel yeniden başlarken lisans sayfası (sahip bildirdi, Ubuntu
   sunucusu).** Güncelleme v0.1.0-alpha.81 arayüzünden başlatıldı. Panel'in
   yeniden başlaması sırasında tarayıcı, "Update and recovery status" kutusuyla
   birlikte tam ekran "License status could not be checked" sayfasını gösterdi
   ve sonra kendiliğinden toparlandı. Aynı biçimde başlatılan Debian
   sunucusunda görünmedi (zamanlama meselesi). Bilinen-durum kuralını
   ilgilendirir: alpha.81 arayüzü bilinmeyen erişim durumunda sayfanın yerini
   alır. Yedinci gerçek sistem kaydı bu ekranı laboratuvarda alpha.81
   arayüzüyle yaklaşık 78 sn boyunca yeniden üretti, alpha.82 arayüzüyle
   görmedi; orada bir bekletme katmanı sayfayı bağlı tuttu. Bu sürümde
   değiştirilmedi.
2. **Güncelleme bildirimi ile kart (sahip bildirdi, Debian sunucusu).** Ayarlar,
   güncellemeler sayfasında, güncellemeden sonra kart v0.1.0-alpha.82'yi kurulu
   gösterirken köşedeki bir bildirim başlangıçtan yaklaşık iki dakika sonra
   (T+02:03) hâlâ "The update is being applied; the panel may be unavailable
   briefly" diyor ve güncellemelerin kilitli olduğunu söylüyordu. İkisi birbirini
   tutmuyordu. D-024'ü ve bilinen-durum kuralını ilgilendirir: uç bir sonuç ile
   sürüyor bildirimi çelişiyor; yani ekran sahibe güncellemenin bitip bitmediğini
   söylemiyor. Bu sürümde değiştirilmedi.
3. **İlk yükleme ekranı (sahip bildirdi; yedinci kayıtta ölçüldü).** `/setup`
   sayfasının soğuk tam yüklemesi, sayfa çizilmeden önce kısa bir an
   "Checking..." ve "Reload CelikPanel" ile tam sayfa "Checking panel access"
   ekranı gösterir. Bilinen-durum kuralını ilgilendirir: sessiz süre geçene ya
   da bir okuma erişimi doğrulamadan yanıtlayana kadar hiçbir şey çizilmemelidir.
   Yedinci kaydın 5. hücresi bunu yayımlanmış kodda, gerçek bir Panel'e karşı
   gerçek bir Chrome'da `/setup`, `/` ve `/settings?section=updates` için
   ölçtü: `RecoveryAccess` kapısı 18 yüklemenin her birinde, hiçbir oturum
   okuması yanıtlanmadan, hızlı bağlantıda yaklaşık 80–100 ms, 2 Mbit/sn ve
   300 ms'ye kısılmış bağlantıda yaklaşık 1,7 sn boyunca bunu boyadı.
   `RecoveryAccess` henüz dönüştürülmemiş bir kapıdır. Ölçülmeyenler: Ubuntu,
   başlıklı tarayıcı, telefon görünümü, Türkçe, HTTP önbelleği açık yükleme.
   Bu sürümde değiştirilmedi.
4. **Kurulum sayfasındaki durdu/sürüyor çelişkisi (sahip bildirdi, Debian
   sunucusu).** Kurulum sihirbazı sayfası en üstte "This operation stopped ... A
   required check needs attention" derken son adım "Verify the prepared
   server - In progress" diyor; neden, kapalı "Checks and how to continue" ve
   "Technical details" bölümlerinin altındadır. D-024'ü ilgilendirir: neden ve
   kimin işlem yapacağı adım listesinden önce gelir ve durmuş bir işlem aynı
   zamanda sürüyor gösterilmez. v0.1.0-alpha.81'de de aynıydı. Bu sürümde
   değiştirilmedi.

### Kurulum sayfasının son denetimi ve güncelleme bildirimi bilineni söyler (2026-10-10)

Bileşen testleri ve yerel geri döngü taklidine karşı gerçek bir Chrome ile
kaynak durumu (`web/tools/browser-inspect`, `finalcheck` ve `updatephase`
senaryoları). Bu girdi için kurulu bir sunucuda hiçbir şey gözlenmedi. Bileşen
testli ve taklit tarayıcılı; gerçek bir sistemde ölçülmedi. Sahibin yukarıdaki
girdisinin ikinci ve dördüncü gözlemini yanıtlar; birinci ve üçüncüsü başka bir
değişikliktir. Kurulum akışı, yönlendirmesi ve önkoşulları (D-021,
[sunucu kurulum planı](SERVER-SETUP-PLAN.tr.md)) değişmedi: yalnız sayfanın
söylediği ve okuduğu tipli durum değişti.

**Sahibin kurulum sayfasının gösterdiği, koddan okunduğu hâliyle.** Son denetimi
beklerken planı "Planı düzenle" ile yeniden açılan bir çalışma
(`POST /api/v1/setup/revise`, `cmd/panel/server_setup_revise.go`),
`server_setup_plan_revised` koduyla `failed` olarak kaydedilir; `verification`
aşaması ve son denetimleri korunur. Sayfa kendine ait bir işaret tutmadığında en
son çalışmayı okur ve bu kaydı hata olarak gösterdi: hata başlığı, genel "Gerekli bir
kontrol tamamlanmadı" cümlesi (kodun cümlesi yoktu) ve "Devam
ediyor" diyen son adım (adım listesi, aşama `verification` olup çalışma
beklemedikçe son adımı sürüyor sayıyordu). Çalışmanın beklediği denetim posta
kimliği denetimiydi (`server_setup_readiness.go`); genel yardımı "Checks and how
to continue" altında kapalıydı. Sahibin sunucusunda kimliğin hangi koşulunun
karşılanmadığı okunmadı; sahip ters DNS'i bildiriyor.

**Metinlerin izlediği kural.** Neden, kimin işlem yapacağı ve sonraki eylem adım
listesinin üstünde ve açık durur. Planı yeniden açıldığı için durmuş bir
çalışma ne hatadır ne de sürüyordur. Son denetim sahibi mi (tipli neden), başka
bir gereksinimi mi beklediğini, yoksa okunamadığını söyler. Başarısız adım
"Başarısız", durmuş bir çalışmanın hiç ulaşmadığı adım "Başlamadı" der.
Güncelleme bildirimi ve güncelleme kartı, izlenen güncellemenin son okumasının
ortaya koyduğunu söyler: uygulanıyor, kuruldu ve doğrulanıyor ya da bilinmiyor.

**Posta kimliği denetiminin tipli nedeni (`GET /api/v1/setup`,
`GET /api/v1/setup/operation`).** `action_required` durumundaki `mail_identity`
denetimi ek olarak `reason` ve `vars` taşır: denetimin okuduğu sırayla
karşılanmayan ilk koşul ve andığı değerler. `server_address_not_public`,
`mail_name_differs`, `reverse_dns_mismatch`, `forward_dns_mismatch` ve, bu
değişikliğin ikinci okumasından sonra, plandaki posta sunucusu adı tam bir
sunucu adı olmadığında `mail_name_not_canonical` (ad düz bir DNS adıysa
`name`). Hazır denetim ikisini de taşımaz, Agent'ın yanıtlayamadığı denetim de
taşımaz; onlardan önce yazılmış kayıt genel cümlesini korur. Durum ve kod,
aşağıdaki yanıtsız sorgu dışında değişmedi.

**Yanıt alamayan ters DNS sorgusu eksik değil, bilinmeyendir (bu değişikliğin
ikinci okuması, 2026-10-10).** Önceden iki genel çözümleyici de yanıt
vermediğinde Agent hatayı düşürüyordu (`cmd/agent/mail_health_rpc.go`,
`mail_health_dns.go`): PTR boş kalıyor, Panel `reverse_dns_mismatch`
bildiriyor ve sayfa PTR olmadığını söylüyordu. Agent'ın sağlık yanıtı artık ek
olarak `reverse_dns_lookup` (`looked_up` ya da `failed`) ve başarısız olduğunda
`reverse_dns_lookup_error` (`timeout`, `no_resolver`, `refused` ya da `other`;
çözümleyicinin kendi metni aktarılmaz) taşır. Çözümleyicinin "böyle bir ad yok"
yanıtı (NXDOMAIN), hem PTR hem sunucu adının adresi için bir yanıttır; böylece
doğrulanmış yokluk doğrulanmış yokluk olarak kalır. Sorgu başarısız olduğunda ve
önceki koşullar (kurallı ad, genel adres, posta adı) sağlandığında denetim
`unknown`, kodu `mail_identity_unavailable`, nedeni `reverse_dns_unknown`
(`ip`, `error`) olur; sayfa aşağıdaki cümleyi söyler ve son adım "Sizi
bekliyor" değil "Kontrol edilemedi" der. Bu değişiklikten önceki bir Agent
sorgu alanı göndermez; onun boş PTR'si hâlâ `reverse_dns_mismatch` olarak
bildirilir; bu nedenin cümlesi artık bir sonuç iddia etmez ("doğrulanamadı").
Eşleşen bir PTR'den sonra ileri sorgunun başarısız olması da aynı biçimde, ters
DNS sorgulanamadı diye bildirilir; iki sorgu birbirinden ayrılmaz. Algılanamayan
adres algılanamadı diye yazılır (`addressMissing`).

- `setup.check.mailIdentity.reverseDNS`
  TR: "Bu sunucunun {ip} adresinin ters DNS (PTR) adı {ptr}; posta sunucuları
  {hostname} bekler. Bunu CelikPanel’de değil, sunucu sağlayıcınızda
  ayarlarsınız. Sağlayıcınızın kontrol panelinden ayarlayın ya da destek
  ekibinden {ip} için {hostname} PTR kaydını ayarlamasını isteyin."
- `setup.check.mailIdentity.reverseDNSMissing` TR: "{ip} için bir ters DNS
  (PTR) adı doğrulanamadı; posta sunucuları {hostname} bekler. ..." (önceden:
  "... ters DNS (PTR) adı bulunamadı; ...").
- `setup.check.mailIdentity.reverseDNSUnknown` (yeni) TR: "{ip} adresinin ters
  DNS kaydı şu an sorgulanamadı; bu, kaydın ayarlı olup olmadığını göstermez.
  Gereksinimleri tekrar kontrol edin."
- `setup.check.mailIdentity.forwardDNS` TR: "... {hostname} adının DNS
  kayıtlarını yönettiğiniz yerde A kaydını {ip} olarak ayarlayın, sonra
  gereksinimleri tekrar kontrol edin."
- `setup.check.mailIdentity.mailName` / `.mailNameUnread` TR: "... Planı
  inceleyin, posta sunucusu adını orada düzeltin ve kurulumu yeniden
  başlatın." ("Postfix myhostname" yapılandırma anahtarı artık anılmaz).
- `setup.check.mailIdentity.address` / `.addressMissing` TR: "... Sunucu
  sağlayıcınızdan bu sunucuya ulaşan genel bir IPv4 adresi isteyin, sonra
  gereksinimleri tekrar kontrol edin."
- `setup.check.mailIdentity.notCanonical` (yeni) TR: "Plandaki posta sunucusu
  adı {name}, tam bir sunucu adı değil (mail.example.com gibi). Planı
  inceleyin, adı düzeltin ve kurulumu yeniden başlatın." (`.notCanonicalUnread`
  adsız).
- `setup.check.notRead`
  TR: "{check}: şu an kontrol edilemedi; bu yüzden karşılanıp karşılanmadığı
  bilinmiyor. Bu, gereksinimin karşılanmadığı anlamına gelmez." (önceden: "...
  denetlenemedi; ... Bu, bir şeyin eksik ya da durmuş olduğu anlamına gelmez.")

**Planı yeniden açıldığı için durmuş çalışma.**

- `setup.guide.revisedTitle` TR: "Kurulum durdu: planı yeniden açıldı"
- `setup.guide.revised` TR: "Bu kurulum çalışması, planı düzenlemek için
  yeniden açıldığından durdu. Bu bir hata değildir: hiçbir şey geri alınmadı ve
  kurulmuş bileşenler olduğu gibi kalır. Hiçbir şey kendiliğinden devam etmez."
- `setup.guide.revisedChecks` TR: "Durduğunda son kontrol hâlâ şunu
  bekliyordu:" ve ardından açık denetim başına bir satır.
- `setup.guide.revisedNext` TR: "Sonraki adım: planı inceleyip yeniden
  başlatın. Son kontrol o zaman yeniden çalışır." Yanındaki düğme mevcut
  "Düzeltilmiş planı incele" düğmesidir. "Teknik ayrıntılar" yalnız sunucunun
  satırını tutar.

**Bekleyen son denetim.** `setup.guide.verificationWaiting` TR: "Bütün kurulum
adımları bitti. Kurulum, son kontrolünde şunu bekliyor:", açık denetimler,
genel yardımları (artık kapalı değil), sonra `setup.guide.verificationResume`
TR: "Kurulum kendiliğinden yeniden kontrol eder ve bütün kontroller geçince
tamamlanır. Hemen kontrol etmek için Gereksinimleri tekrar kontrol et
düğmesini kullanın." Çalışma beklerken yürütücü denetimleri 20 saniyede bir
yeniden okur.

**Adımın durum sözcüğü.** "Sizi bekliyor", "Gereksinimler bekleniyor"
(değişmedi), "Kontrol edilemedi", "Başarısız", "Durduruldu", "Başlamadı".
Birkaç denetim açıkken, birinin sahibin işlem yapmasını isteyen tipli nedeni
varsa (`action_required`) "Sizi bekliyor" kullanılır; açık her denetim yine
adımların üstünde listelenir. İkinci okumadan beri son denetimin yeni Türkçe
metinleri, düğmesi "Gereksinimleri tekrar kontrol et" gibi "kontrol" der
(önceden "denetim/denetle/Denetlenemedi"; `setup.check.name.other` TR
"Gerekli bir kontrol", kart satırlarında "Güncelleme kontrolü").

**Güncelleme bildirimi ve kart (`GET /api/v1/panel/update/status`).** Agent'ın
kaydı, yeni Panel başladıktan sonra güncelleyici çıkıp son kanıtı geçene kadar
`running` kalır (`cmd/agent/system_update_worker_linux.go`). Bildirim her
`running` okumasında ve hiçbir okuma yapılmadan önce yer tutucu olarak
`panelUpdate.running` diyordu (`SystemUpdateOperation.tsx`); kart ise yanıt
veren Panel'in çalıştırdığı sürümü okuyordu (`/api/v1/panel/version`): bu
aralıkta ikisi çelişiyordu. Durum yanıtı artık, kayıt `running` iken yanıt veren
Panel'in sürümü ve commit'i güncellemenin hedefiyle aynıysa (arşiv özeti ve
sıra numarası karşılaştırılmaz), ek olarak `phase: "verifying"` taşır.

- `panelUpdate.tracking.verifyingTitle` TR: "Güncelleme kuruldu, doğrulanıyor"
- `panelUpdate.tracking.verifying` TR: "{version} kuruldu ve bu panel onu
  çalıştırıyor. Güncelleme doğrulanıyor ve henüz bitmedi; bu bildirim onu
  kendiliğinden izler. Bir şey yapmanız gerekmiyor." (son cümle ikinci okumada
  eklendi; `panelUpdate.card.verifying` sonuna da; `panelUpdate.card.unknown`
  sonuna "İkinci bir güncelleme başlatmayın.")
- `panelUpdate.tracking.unknown` TR: "Bu güncellemenin durumu şu an okunamadı;
  bu yüzden hâlâ sürüp sürmediği ya da bitip bitmediği bilinmiyor. Bu bildirim
  kendiliğinden yeniden okur; başka bir güncelleme başlatmayın." Başarısız
  okumanın nedeni (bağlantı, yeniden başlatma) altındaki satırdır.
- `panelUpdate.tracking.reading` TR: "Bu güncellemenin durumu sunucudan
  okunuyor…" (ilk okumadan önce; orada "uygulanıyor" yerine geçer).
- Kart: geçerli sürümün yanında "kuruldu, doğrulanıyor" ve bir durum satırı
  (`panelUpdate.card.verifying`, `.applying`, `.reading`, `.unknown`). Bitmiş
  kayıt ne bildirim ne satır bırakır (değişmedi: sayfa bir kez yeniden yüklenir).
- `panelUpdate.lastRead` TR: "Son okuma". Güncelleme penceresindeki saat,
  "UTC" etiketli tarayıcı yerel saatiydi; artık arayüz dilinde, dilimiyle
  yazılmış yerel saattir (örneğin "18:53:39 GMT+3").

**Sınırlar.** Bileşen testleri (`web/tests/setup-final-check-mounted.test.mjs`,
`web/tests/update-tracking-mounted.test.mjs`,
`cmd/panel/known_state_gates_test.go`) ve geri döngü taklidine karşı gerçek bir
Chrome; masaüstü ve telefon, İngilizce ve Türkçe. Ölçülmedi: ters DNS'i yanlış
olan gerçek bir sunucu (tipli neden gerçek bir Agent'tan okunmadı), sahibin kendi
kaydı, gerçek bir güncellemenin doğrulama aralığı, telefonda köşe bildiriminin
kartın satırını örtmesi (taklitte örttü; değiştirilmedi). Bildirimi sekmenin
yüklediği arayüz çizer: önceki sürümü çalıştıran bir sekmeden başlatılan
güncelleme `phase` alanını yok sayar ve eski cümleyi korur; yeni metin bu
değişiklikten sonra yüklenen bir sayfadan başlatılan güncellemeden itibaren
geçerlidir. Burada ilk kaydedilen ters DNS açığı ikinci okumayla kodda
kapandı (yanıt alamayan sorgu, sınıfıyla birlikte bilinmeyen olarak bildirilir):
bileşen testli (`cmd/agent/mail_health_dns_test.go`,
`cmd/panel/known_state_gates_test.go`, bağlanmış test) ve taklit tarayıcılı;
gerçek bir sistemde ölçülmedi ve çözümleyicileri yanıt vermeyen gerçek bir
Agent'tan okunmadı.

### İlk sayfa yüklemesinde yanıtlanmamış erişim sayfası yok; bekletme katmanı okuduğu nedeni söyler (2026-10-10)

Bileşen testleri ve geri döngü taklidine karşı bir tarayıcı koşusuyla kaynak
durumu; konuk yok, kurulu sunucu yok. Bileşen testli ve taklit tarayıcılı;
gerçek bir sistemde ölçülmedi. Yedinci gerçek sistem kaydının
(`deploy/e2e/release-recovery/evidence/set7-20261010/`) iki bulgusunu karşılar:
hücre 5 (yukarıdaki girdideki sahibin üçüncü gözlemi) ve hücre 1'in gözlemi
(yalnız sözcükleri; sayfayı değiştiren alpha.81 arayüzü değişmedi). Mekanizma ve
sözleşme maddeleri aynı tarih ve başlıklı
[dayanıklılık sözleşmesi](RESILIENCE-CONTRACT.tr.md) girdisindedir.

**Ekranda ne değişir.**

- *Soğuk tam sayfa yüklemesi, oturum, hazır olma ya da ilk lisans okuması
  yanıt vermemişken:* ilk 1,5 sn yalnızca boş sayfa zemini (bekletme
  katmanıyla aynı sessiz süre): başlık, cümle, düğme ya da kendine ait bir
  gösterge yok. Bu sürede yanıt veren okuma geride bir şey bırakmaz; erişimi
  doğrulamadan yanıt veren okuma (oturum yok, Panel başlıyor, okuma başarısız)
  zemini hemen, her zamanki sayfasıyla değiştirir.
- *Aynı okumalar 1,5 sn sonra hâlâ yanıtsız* (hiçbir şey başarısız olmadı):
  `recovery.checkingTitle`, TR "Panel erişimi kontrol ediliyor" · EN "Checking
  panel access"; yeni `recovery.waitingHelp`, TR "Panel henüz yanıt vermedi.
  Yanıt verir vermez CelikPanel açılır; bir şey yapmanız gerekmiyor." · EN "The
  Panel has not answered yet. CelikPanel opens as soon as it does; you do not
  need to do anything."; kontrol düğmesi (bir okuma sürerken meşgul,
  `recovery.checking`). "CelikPanel’i yeniden yükle" artık hemen sunulmaz;
  yarım dakika sonra (bu girdi önce sessiz sürenin bitiminden, beklemeyi
  gösteren sayfa tarafından saydı; **2026-10-10'da değiştirildi, aşağıdaki
  "Dokuzuncu gerçek sistem kaydından sonra" bölümüne bakın: yarım dakika sayfanın
  ilk beklemesi için sayfa yüklemesinden sayılır ve cümle yeniden yazıldı**)
  yeni `recovery.waitingProlonged`, ilk sözleriyle TR "Bu, yarım dakikadan uzun sürdü. CelikPanel
  kendiliğinden kontrol etmeyi sürdürür; dilerseniz yeniden de
  yükleyebilirsiniz." · EN "This has taken longer than half a minute.
  CelikPanel keeps checking by itself; you can also reload it." ile
  `app.reload`. Önceden sayfa ilk karesinden itibaren `recovery.checkingHelp`
  ("Oturumunuz ve panelin hazır olma durumu doğrulanıyor…"), "Kontrol
  ediliyor…" ve "CelikPanel’i yeniden yükle" gösteriyordu.
- *Oturum ve hazır olma doğrulanmış, arayüzün kendisi 1,5 sn sonra hâlâ
  yükleniyor:* yeni `recovery.loadingTitle`, TR "CelikPanel açılıyor" · EN
  "Opening CelikPanel"; `recovery.loadingHelp`, TR "Oturumunuz doğrulandı ve
  Panel hazır. Arayüz hâlâ yükleniyor ve kendiliğinden açılır." · EN "Your
  session is confirmed and the Panel is ready. The interface is still loading
  and opens by itself." Kontrol düğmesi yok (okunacak bir şey yok); yeniden
  yükleme yarım dakika sonra, ikinci okumadan beri kendi cümlesiyle:
  `recovery.waitingProlongedLoading`, TR "CelikPanel hâlâ yükleniyor;
  kendiliğinden açılır. Bu sayfa böyle kalırsa yeniden yükleyin." · EN
  "CelikPanel is still loading; it opens by itself. If this page stays like
  this, reload it." (önceden yükleme beklemesi, CelikPanel'in kontrol etmeyi
  sürdürdüğünü söyleyen `recovery.waitingProlonged` cümlesini kullanıyordu).
  Önceden bu durum "Panel erişimi kontrol ediliyor" diyordu.
- *Açıklanmış bir bekleme*, aynı yüklemenin bir sonraki kapısı devraldığında
  (kurtarma sayfasının altında arayüzün gelmesi, oturum okumasından sonra
  lisans okuması) yeniden gizlenmez.
- *Açık bir sayfanın üzerindeki bekletme katmanı, okumanın gösterdiği nedeni
  söyler.* Lisans metni (`accessHold.licenseTitle`/`licenseHelp`, değişmedi)
  yalnızca Panel yanıt verdiğinde ve okunamayan şey lisans sonucu olduğunda ya
  da bir istek lisans kararı olmadığı için reddedildiğinde kullanılır
  (gövdesi çözümlenemeyen bir başarı durumu da aynı sayılır, Panel'in yanıtı
  olarak: `LicenseOnboarding.tsx` nedeni çözümlemeden önce ayarlar).
  Panel'den yanıt yok: `accessHold.availabilityTitle`, TR "Panel az önce yanıt
  vermedi" · EN "The Panel did not answer just now" (değişmedi). Panel
  başladığını söylüyor: `recovery.startingTitle` (değişmedi). Bu tarayıcıdan son
  30 dakika içinde başlatılan bir güncelleme bitişini kaydetmemişken yanıt yok
  (yeni; ikinci okumada düzeltilen sözcüklerle): `accessHold.updateTitle`, TR
  "Panel yanıt vermiyor; bir güncelleme onu yeniden başlatıyor olabilir" · EN
  "The Panel is not answering; an update may be restarting it";
  `accessHold.updateHelp`, TR "Bu tarayıcıdan bir güncelleme başlatıldı ve
  bitişi burada henüz görülmedi. Güncelleme uygulanırken Panel yeniden başlar;
  bu yüzden kısa bir süre yanıt vermeyebilir. Lisansın geçerli olup olmadığı
  Panel yanıt verene kadar bilinmiyor; bu konuda hiçbir karar verilmedi." · EN
  "... Whether the license is valid is not known until the Panel answers;
  nothing about it has been decided." (ilk biçim "güncelleme sırasında" ve "Bu
  bir lisans sorunu değildir" diyordu; okumanın ortaya koymadığı bir nedeni ve
  bir lisans hükmünü iddia ediyordu). Devam satırı ve kontrol eylemi değişmedi.
  Tarayıcının kendi güncelleme kaydı yalnızca bu sözcükleri seçer; hiçbir
  zaman sunucu sonucu olarak gösterilmez. Güncellemeyi yalnız güncellemenin
  başlangıcından sonraki 30 dakika içinde anar (kaydın `created_at` alanı;
  `savedUpdateUnfinished`, `UPDATE_CAUSE_WINDOW_MS`); daha eski, başlangıç
  zamanı olmayan ya da başlangıcı gelecekte görünen kayıt genel "Panel az önce
  yanıt vermedi" cümlesini bırakır. İkinci okumadan önce kaydın yaş sınırı
  yoktu; bitişi hiç görmeyen bir tarayıcı güncellemeyi sonraki her kesinti için
  anabilirdi. Kaydın anahtarı (`celikpanel.system-update-operation.v1`) bir kez,
  `lib/recoveryObservation.ts` içinde tanımlanır ve güncelleme izleyicisi onu
  içe aktarır.
- *Hiçbir şeyin yerini almayan tam sayfa* (bir şey bağlanmadan ilk lisans
  okuması başarısız) aynı kararı kullanır: yanıt yoksa "Panelin hazır olma
  durumu kontrol edilemedi", başlayan Panel için "Panel başlatılıyor", "Lisans
  durumu kontrol edilemedi" yalnızca okunamayan bir lisans sonucu için.

**Ne değişmedi.** Bilinen olumsuzlar (oturum yok: giriş; bilinen lisans
kararı: etkinleştirme; Panel başlıyor; yüklenemeyen arayüz) ekranı eskisi gibi,
hemen değiştirir. Arayüz yüklenirken ya da yüklenemedikten sonra ulaşılan,
kapalı ya da başlayan Panel için kurtarma sayfası okuması yanıt verir vermez
gösterilir. Erişim kararları, geçerlilikleri, okumalar ve aralıkları
değişmedi; istek eklenmedi.

**Kanıt.** Bileşen testleri: `web/tests/recovery-access-runtime.test.mjs`
(soğuk yükleme, bekleme durumu, bilinen olumsuzlar, kapılar arası devir,
gerçek oturum okumalarıyla kurtarma yolu), `web/tests/access-hold-runtime.test.mjs`
(ilk lisans okuması; bitmemiş güncelleme kaydı olan ve olmayan dokuz okuma
sonucu için bekletme nedeni, 30 dakikadan eski ve başlangıç zamanı olmayan kayıt
dahil; pencerenin sınırları). İkinci okumanın ekranlarına aynı taklit
tarayıcıda bakıldı (`finalcheck`, `updatephase`, `waitcopy` senaryoları,
masaüstü, İngilizce ve Türkçe). Geri döngü taklidine karşı tarayıcı koşusu
(`web/tools/browser-inspect`, `coldload` ve `coldslow` senaryoları, 2 Mbit/sn
ve 300 ms'ye kısılmış Chrome, okumalar 300 ms yavaşlatılmış): yayımlanmış kod
"Panel erişimi kontrol ediliyor" kapısını `/setup`, `/` ve
`/settings?section=updates` üzerinde yaklaşık 1,4 sn çizdi (o koşunun konsol
çıktısı depoda tutulmaz; yedinci kayıt aynı kısıtlamada gerçek bir Panel'de
yaklaşık 1,7 sn ölçtü); bu değişiklikle üç
yüklemenin hiçbiri uygulamadan önce bir cümle çizmedi (zemin, açılış
göstergesi, zemin, sonra sayfa); oturum okuması 2,5 sn tutulduğunda açıklanmış
bekleme sessiz süreden sonra göründü ve sayfa kendiliğinden açıldı.
**Ölçülmedi:** konuktaki gerçek bir Panel (set7 yöntemi), kurulu bir sunucu,
HTTP önbelleği açıkken yükleme, gerçek bir güncelleme sırasında bekletme
katmanı. Dil yükleyicisinin metinsiz açılış göstergesi değişmedi.

**Dokuzuncu gerçek sistem kaydından sonra (2026-10-10).** Dokuzuncu gerçek
sistem kaydı (`deploy/e2e/release-recovery/evidence/set9-20261010/`, hücre 2;
konukta gerçek bir Panel'e karşı gerçek Chrome, arayüz `7c3a05809`'dan
derlendi) bu girdiyi ilk kez gerçek bir sistemde ölçtü. Soğuk yükleme geçer:
önce zemin, sessiz süreden sonra yukarıdaki açıklanmış bekleme; üç yolda,
İngilizce ve Türkçe (oturum okuması 2,5 sn tutuldu). Her oturum okuması 35 sn
tutulduğunda bu girdinin kaldırmadığı daha eski bir yol bulundu: oturum
okumasının kendi 15 sn sınırı (`web/src/auth/usePanelSession.ts`, 15000 ms
sonra `AbortController`) oturumu "okunamadı" yaptı; sayfa 15,18 sn'de "Oturumunuz
kontrol edilemedi" dedi, "Panel erişimini kontrol et" ve "CelikPanel’i yeniden
yükle" ile; 25,2 sn'deki kendiliğinden yeniden okuma `recovery.checkingHelp`
("Oturumunuz ve panelin hazır olma durumu doğrulanıyor…") cümlesini yeniden
yükleme ile gösterdi; `recovery.waitingProlonged` hiç görünmedi. Kaynakta
düzeltildi:

- *Yanıt vermemiş erişim okuması için tek sıra,* uygulama üzerinden ve kurtarma
  yolu üzerinden açılan sayfada: 1,5 sn'ye kadar zemin; meşgul denetimle
  açıklanmış bekleme; 15 sn'de hâlâ bilinmeyen, asla hata değil: yeni
  `recovery.waitingLong`, TR "Panel bir süredir yanıt vermiyor. CelikPanel
  kendiliğinden kontrol etmeyi sürdürür; dilerseniz şimdi de kontrol
  edebilirsiniz." · EN "The Panel has not answered for a while. CelikPanel keeps
  checking by itself; you can also check now.", etkin yeni `recovery.checkNow`
  ile, TR "Şimdi kontrol et" · EN "Check now" (bir okuma daha; kendiliğinden
  yeniden okuma sürerken basılırsa o okuma bitene kadar "Kontrol ediliyor…"
  gösterir ve ikinci bir okuma başlatmaz; kendiliğinden yeniden okumanın kendisi
  meşgul çizilmez); 30 sn'de `recovery.waitingProlonged` ve "Şimdi kontrol et"in
  yanında "CelikPanel’i yeniden yükle". `recovery.waitingProlonged` artık TR
  "Bu, yarım dakikadan uzun sürdü. Dilerseniz CelikPanel’i yeniden de
  yükleyebilirsiniz; Panel yine yanıt vermezse sunucu yöneticisi CelikPanel
  hizmetinin çalıştığını denetler." · EN "This has taken longer than half a
  minute. You can also reload CelikPanel; if the Panel still does not answer,
  the server administrator checks that the CelikPanel service is running." der
  (ikinci tur; üstündeki cümle CelikPanel'in kontrol etmeyi sürdürdüğünü zaten
  söyler).
- *15 sn ve 30 sn sayfa yüklemesinden sayılır* (gezinme başlangıcı), sayfanın
  ilk beklemesi için; sonraki bir bekleme için oturum açıldıktan sonraki ilk
  okumadan. Yeniden okumalar ve aynı yüklemenin sonraki kapısı onları asla
  yeniden başlatmaz (`web/src/lib/quietRead.ts` içinde `useAccessWaitStage`).
  Bu, yukarıdaki "sessiz sürenin bitiminden, beklemeyi gösteren sayfa
  tarafından sayılır" ifadesinin yerini alır; arayüz yükleme beklemesi o
  sayımı korur.
- *Hatayla yanıt veren okuma,* ne zaman olursa olsun, hemen bilinen olumsuz
  sonuçtur ve sayfa okunanı adlandırır: yeni `recovery.failure.network`, TR
  "Panel’e bağlantı, yanıt gelmeden kurulamadı (reddedildi, kapandı ya da
  ulaşılamadı). Bu sürerse sunucu yöneticisi CelikPanel hizmetinin çalıştığını
  denetler." · EN "The connection to the Panel failed before it answered
  (refused, closed or unreachable). If this continues, the server administrator
  checks that the CelikPanel service is running."; `recovery.failure.status`, TR
  "Sunucu HTTP {status} hatasıyla yanıt verdi. Bu sürerse sunucu yöneticisi
  CelikPanel hizmetini ve günlüğünü denetler." · EN "The server answered with
  HTTP error {status}. If this continues, the server administrator checks the
  CelikPanel service and its log." (ikisi de ikinci tur: bağlantı cümlesi artık
  yalnız iki neden saymıyor, kod sunucunundur ve Panel'in önündeki bir vekil
  sunucu olabilir); `recovery.failure.invalid`,
  TR "Panel yanıt verdi, ancak bu sayfa yanıtı okuyamadı. CelikPanel’i yeniden
  yüklemek, Panel’e uyan arayüzü yükler." · EN "The Panel answered, but this
  page could not read the answer. Reloading CelikPanel loads the interface that
  matches the Panel." Hangisi: isteğin kendisinin başarısız olması (`fetch`ten
  gelen `TypeError`: reddedildi, kapandı, ulaşılamadı ya da tarayıcının reddettiği
  bir sertifika) `network`; HTTP hata kodu `status` (`api.me`
  `ApiResponseError` fırlatır; `usePanelSession.ts:14-18`, `:55-58`); geri kalan
  her şey, beklenen JSON olmayan bir gövde dahil, `invalid`. `network`, yanıttan
  önce başarısız olan her isteği kapsar (cümle artık "reddedildi, kapandı ya da
  ulaşılamadı" der; reddedilen bir sertifika da böyle bir nedendir). "Oturumunuz
  kontrol edilemedi" ve "Panelin hazır olma durumu kontrol edilemedi" yalnız
  böyle yanıtlanmış bir hata için kalır (ikinci turdan beri önceki bir yanıttan
  sonra da, sonraki madde); `recovery.checkingHelp` yalnız o
  sayfa doğrulanmış oturum olmadan yeniden okurken (`RecoveryAccess.tsx:188`,
  `waiting`) ve bekletme katmanının sözleri henüz yüklenmemişken yedek olarak
  (`AccessHold.tsx:147`) kalır.
- *Önceki bir yanıttan sonra sınırına ulaşan okuma (D-025 ilke 2; ikinci tur,
  2026-10-10; ilk turun açık maddesi).* Şimdiye dek herhangi bir okuma yanıt
  verdikten sonra kendi 15 sn sınırına ulaşan sonraki okuma, önceki okumanın
  nedeniyle bilinen olumsuz sonuç ("Oturumunuz kontrol edilemedi" ya da "Panelin
  hazır olma durumu kontrol edilemedi") olarak çiziliyordu. Artık sınıra
  ulaşmak her zaman bilinmeyendir: `usePanelSession`, son okumanın sınırına
  ulaştığını kaydeder (`limitHit`; yalnız okumanın kendi zamanlayıcısı onu
  kuşağı güncelken kestiğinde konur, her yanıtla ve oturum açmayla temizlenir),
  önceki bir okuma yanıt vermiş olsun olmasın `unanswered` onun için doğrudur
  ve iki üst bileşen önceki gibi `checking` geçirir. Aynı bekleme sırası, o
  okumanın başladığı andan sayılarak uygulanır (`web/src/lib/quietRead.ts`
  içinde `beginAccessWaitAt`): "Şimdi kontrol et" hemen (okuma 15 sn önce
  başladı), yarım dakika cümlesi ve yeniden yükleme 15 sn sonra; yeniden
  okumalar hiçbir şeyi yeniden başlatmaz; bir yanıt gösteren sayfanın üzerinde
  boş sessiz süre olmaz (`afterAnswer`). Önceki yanıtlanmış bir hata yalnız
  beklemenin altında bilinen son durum olarak adlandırılır: yeni
  `recovery.lastKnown`, TR "Bu beklemeden önce bilinen son durum: {cause}" · EN
  "Last known, before this wait: {cause}", hatanın kendi cümlesiyle; asla
  güncel karar olarak değil. Önceki olumlu bir yanıt ("başlıyor") hiçbir şey
  adlandırmaz. Sonraki yanıt beklemeyi bitirir ve hemen çizilir. Bağlı
  sayfaların üzerindeki bekletme katmanı değişmedi.
- *Değişmeyen:* bağlı sayfaların üzerindeki bekletme katmanı, okumalar,
  sınırları ve aralıkları; istek eklenmedi.

Kanıt: `web/tests/recovery-access-runtime.test.mjs` (15 sn ve 30 sn
aşamaları, hiçbir şeyi yeniden başlatmayan ve ikinci okuma başlatmayan yeniden
okuma, nedeniyle yanıtlanmış hatalar, soğuk yükleme ve bilinen olumsuz sonuçlar
önceki gibi); taklit tarayıcı (`coldslow35`, `/settings?section=updates`, her
oturum okuması 35 sn tutuldu, kısıtlamasız, İngilizce ve Türkçe, ekran
görüntülerine bakıldı): 1,6 sn'de "Kontrol ediliyor…" ile açıklanmış bekleme;
15,2 sn ve 25,2 sn'de "Şimdi kontrol et" ile "bir süredir yanıt vermiyor";
31,0 sn'de aynısı, yarım dakika cümlesi ve "CelikPanel’i yeniden yükle" ile.
İkinci tur: aynı dosyada yanıttan sonra sınırına ulaşan okumanın testi (Panel
başlıyor; oturum okuması 503 ile yanıt verdi) ve taklit tarayıcı (`a83session`,
sonraki okumalar tutulmuş `/`, masaüstü ve telefon, İngilizce ve Türkçe, ekran
görüntülerine bakıldı): yaklaşık 4 sn'de yanıt ("Panel başlıyor" / "Oturumunuz
kontrol edilemedi" ile "Sunucu HTTP 503 hatasıyla yanıt verdi…"); 26,5 sn'de,
10 sn'deki yeniden okuma sınırına ulaştıktan sonra "Panel erişimi kontrol
ediliyor", "bir süredir yanıt vermiyor", etkin "Şimdi kontrol et" ve hata için
"Bu beklemeden önce bilinen son durum: Sunucu HTTP 503 hatasıyla yanıt
verdi…"; 41 sn'de yarım dakika cümlesi ve yeniden yükleme. **15 sn/30 sn yolu,
yanıttan önce ve sonra, bileşen testli ve taklit tarayıcılı; gerçek bir
sistemde ölçülmedi.**

*Gözlem, burada değiştirilmedi: kurtarma hizmet çalışanı (service worker).*
Panel durdurulduğunda dokuzuncu kayıt (hücre 4c) Chrome'un kendi hata sayfasını
gördü; çünkü `navigator.serviceWorker.getRegistration('/')` bir kayıt bulmadı;
yedinci kayıt da onun denetlediği bir sayfa görmedi. Arayüz
`/recovery-worker.js`'i `/` kapsamıyla yalnız güvenli bağlamda kaydeder
(`web/src/lib/recoveryShell.ts`; üretimde açılışta ve bir güncelleme
başlatılmadan önce çağrılır); Panel dosyayı web kökünden sunar
(`cmd/panel/frontend.go`, yoksa 404) ve bu kapsam için
`Service-Worker-Allowed` başlığı gerekmez. Olası nedenler, hiçbiri
kanıtlanmadı: (1) Chrome, betiği sertifika hatası olan bir bağlantı üzerinden
alınan hizmet çalışanını kaydetmeyi reddeder; laboratuvarın kendinden imzalı
sertifikası yalnız SPKI sabitlemesiyle kabul edilir ve bu hâlâ bir hata
sayılabilir; (2) kayıt ya da çalışanın kurulumu başarısız oldu ve neden atıldı
(`recoveryShell.ts` her reddi sessizce yakalar); (3) düzenek kaydı her
yüklemede yeni bir tarayıcı bağlamında, o yüklemede başlayan kayıt bitmeden
okur. Kesinleştirmek için: laboratuvar tarayıcısında tam bir yüklemeden sonra
kaydı açıkça çağırıp reddin adını ve iletisini, kayıtları
installing/waiting/active durumlarıyla ve `/recovery-worker.js`'in durum kodunu
ve türünü kaydetmek; sabitleme olmadan güvenilen bir sertifikayla (güven
deposunda bir laboratuvar yetkilisi) yinelemek.
Dokuzuncu kaydın girişinde bir olgu daha var; kayıt onu üç nedenden hiçbirine
bağlamıyor (ne açıklıyor ne dışlıyor): hücre 4a'nın oturumsuz üç yüklemesini
(yeni bir tarayıcı bağlamı) bir hizmet çalışanı *denetliyordu*
(`negative/loads.jsonl`, `where.sw_controlled: true`; `/` ve
`/settings?section=updates` için belge ve `recoveryObservation-*.js` çalışandan
geldi, `sw: true`), oturum açılmış bağlamların hiçbir sayfası ise
denetlenmiyordu. Çalışanın neden birinde kaydolup ötekinde kaydolmadığı
belirlenmedi ve hücre 4c böyle bir bağlamda tekrarlanmadı: kayıtlı çalışanla
durmuş Panel davranışı **ölçülmedi** olarak kalır.

### Sahibin değiştirdiği site yapılandırma dosyası korunur ve adlandırılır; seçimi sahip yapar (2026-10-10)

Bileşen testleri ve loopback sahte sunucuya karşı gerçek Chrome ile kaynak
durumu; konukta koşu yok, kurulu sunucu yok (D-031; D-022; D-024). Sekizinci
gerçek sistem kaydı (`deploy/e2e/release-recovery/evidence/set8-20261010/`)
yayımlanmış alpha.82'yi Debian 13, Ubuntu 24.04 ve Arch'ta ölçtü: sahibin bir
sitenin nginx sanal konağında yaptığı değişiklik, Panel'in sonraki başlangıcında,
yeniden başlatılmasında ya da Genel kaydında sessizce eziliyordu; sahibin
kaldırdığı sanal konak yeniden yazılıyordu; kilitli bir dosya (`chattr +i`) tüm
başlangıç toplu işini bozuyor ve etkin bağlantısını kaybediyordu; kaydetme
günlüğe satır yazmıyordu; her başlangıç değişmemiş dosyaları yeniden yazıp
nginx'i yeniden yüklüyordu.

**Sahibin şimdi gördüğü.** Her üretimden önce (başlangıç, ayarlar, sertifika,
barındırma türü, PHP sürümü, oluşturma, içe aktarma) Agent dosyayı
sınıflandırır. Yalnız CelikPanel'in kendi değişmemiş metni değiştirilir;
sahibin değiştirdiği, yenisiyle değiştirdiği, kaldırdığı ya da kilitlediği dosya
olduğu gibi korunur ve onu değiştirmek isteyen işlem başarı bildirmek yerine
bunu söyler. Alan adı sayfası durumu ve farkı gösterir, seçimi sahip yapar.
Kim yapar: sunucu sahibi (yönetici); hiçbir şey kendiliğinden sürmez.

**Nerede.** Alan adı sayfası → Barındırma → Yapılandırma dosyası (yalnız
yöneticiler; `GET /api/v1/domains/{id}/site-config`). Alan adının sekmelerinin
üstündeki satır (`siteConfig.notice.*`, "Yapılandırma dosyasını aç" ile) ve Alan
Adları listesindeki rozet oraya götürür. Rozet durumu adlandırır: "Yapılandırma
dosyası: seçiminiz gerekiyor" (`siteConfig.list.badge.kept`; sahip düzenlemiş,
yabancı, kökeni bilinmiyor), "Yapılandırma dosyası eksik" (`.missing`),
"Yapılandırma dosyası okunamıyor" (`.unreadable`); sahibin korumayı seçtiği
dosyada, üzerinde bekleyen bir sertifika yoksa rozet çizilmez. Sekmelerin
üstündeki satır "benimkini koru"yu dikkate alır: seçimle korunan dosyada düz bir
satırdır, "Bu sitenin nginx yapılandırma dosyası seçtiğiniz gibi korunuyor;
CelikPanel’in metni yanında bekletiliyor." (`siteConfig.notice.keptByChoice`),
aynı bağlantıyla; dosyada bekleyen bir sertifika bunun önüne geçer ("Bu site için
yeni bir sertifika hazır, ancak sitenin nginx yapılandırma dosyası hâlâ öncekini
kullanıyor." / "Bu sitenin nginx yapılandırma dosyasına ne olacağını seçene dek
sitenin sertifikası alınamaz ya da yenilenemez."). İkisi de Panel'in kaydettiği
son durumdan okunur (yöneticiye defterden okuma; Agent çağrısı yok), o andaki
dosyadan değil.

**Durumlar ve cümleleri** (anahtarlar `web/src/i18n/screens/server` içinde, TR):

- Kontrol ediliyor: `siteConfig.checking` "Bu sitenin yapılandırma dosyası
  okunuyor…". Kontrol edilemedi: `siteConfig.unknownRead` "Bu sitenin
  yapılandırma dosyasının durumu şu an okunamadı. Bu, dosyada bir sorun olduğu
  anlamına gelmez ve hiçbir şey değiştirilmedi. Tekrar deneyin." (Tekrar dene
  yalnız okur.)
- Bilinmiyor (durumu bildirmeyen bir Agent): `siteConfig.unknownState.title`
  "Bu dosyanın durumu bilinmiyor" — "Bu sunucudaki CelikPanel yardımcı hizmeti
  panelden eski olduğu için bu dosyanın değiştirilip değiştirilmediğini
  söyleyemiyor. Hiçbir şey değiştirilmedi. Güncelleme bu sunucuda bitince sayfayı
  yeniden yükleyin; dosyanın durumu görünür."
- CelikPanel'in metni: `siteConfig.managed.title` "CelikPanel’in metni,
  değişmemiş" — "Bu dosya CelikPanel’in en son yazdığının ta kendisi; bu yüzden
  siz bu sitenin ayarlarını değiştirdikçe CelikPanel onu günceller." Önceki
  sürümden devralınan, yazıldığı biçime göre (`adopted_from` ` (creation)` ile
  bitiyor mu; sonekin kendisi gösterilmez): "Bu dosya, {release} sürümünün site
  oluşturulduğundaki hâliyle yazdığı metnin ta kendisi; bu yüzden CelikPanel onu
  devraldı." / "Bu dosya, {release} sürümünün panel başlatıldıktan ya da bir ayar
  kaydedildikten sonraki hâliyle yazdığı metnin ta kendisi; bu yüzden CelikPanel
  onu devraldı."
- Düzenlenmiş / değiştirilmiş: `siteConfig.ownerEdited.title` "Yapılandırma
  CelikPanel dışında düzenlendi", `siteConfig.foreign.title` "Yapılandırma
  CelikPanel dışında değiştirildi" (durum adları `owner_edited` ve `foreign`
  olarak kalır) — `siteConfig.kept.body` "Bu dosya CelikPanel’in en
  son yazdığı metin değil; bu yüzden CelikPanel onu olduğu gibi korudu. Bu sitede
  CelikPanel’de yaptığınız değişiklikler (ayarlar, sertifikalar, barındırma
  türü, PHP sürümü), aşağıda bir seçim yapana dek ona uygulanmaz. nginx siteyi bu
  dosyayla sunmayı sürdürür."
- Kökeni bilinmiyor: `siteConfig.unknownOrigin.title` "Yapılandırma CelikPanel’inki
  olarak tanınmadı" — "Bu dosya CelikPanel’in bu sürümünden önce de oradaydı ve
  önceki bir CelikPanel sürümünün bu site için yazdığı her metinden farklı; bu
  yüzden sizin değişikliklerinizi taşıyor olabilir. CelikPanel onu olduğu gibi
  korudu. Bu sitede CelikPanel’de yaptığınız değişiklikler, aşağıda bir seçim
  yapana dek ona uygulanmaz. nginx siteyi bu dosyayla sunmayı sürdürür."
- Eksik: `siteConfig.missing.title` "Yapılandırma dosyası eksik" — "Bu sitenin
  nginx yapılandırma dosyası yerinde değil. CelikPanel onu yeniden oluşturmadı,
  çünkü kaldırılması sizin seçiminiz olabilir; o olmadan nginx bir sonraki
  yeniden başlatmada ya da yeniden yüklemede bu siteyi yükleyemez.
  Bu sitede CelikPanel’de yaptığınız değişiklikler, dosya yeniden yerinde olana
  dek uygulanmaz." Eylem: "Yeniden oluştur" — "CelikPanel bu site için kendi
  metnini yazar ve nginx’i yeniden yükler."
- Okunamıyor ya da değiştirilemiyor: `siteConfig.unreadable.title` "Yapılandırma
  dosyası okunamıyor ya da değiştirilemiyor", nedeniyle (kilitli dosyada:
  "Değiştirilemedi (örneğin chattr +i ile değişikliğe kilitli). CelikPanel
  dosyayı ve bağlantısını olduğu gibi korudu.") ve o nedenin sonraki adımı:
  bağlantıda "Sunucu sahibi bağlantıyı sıradan bir dosyayla değiştirir ya da
  başka yere taşır, sonra bu sayfayı yeniden yükler."; izinde "Sunucu sahibi
  dosyayı root için okunur yapar, sonra bu sayfayı yeniden yükler."; diğerlerinde
  "Sunucu sahibi sunucunun bildirdiği sorunu giderir, sonra bu sayfayı yeniden
  yükler." ve Agent bir satır gönderdiyse "Sunucunun bildirdiği: {detail}";
  ardından "Bu arada bu sitede CelikPanel’de yaptığınız değişiklikler ona
  uygulanmaz."

**Üç seçim** (her biri D-029 istek kimliğiyle bir POST; her biri sayfanın
gösterdiği özetlere bağlıdır):

- "CelikPanel’in metnini al" — "Dosyanız önce yanına tarihli bir kopya olarak
  bırakılır, sonra yerine CelikPanel’in metni yazılır ve nginx yeniden yüklenir.
  nginx onu reddederse dosyanız geri konur. Tarihli kopya yerinde kalır." Yerinde bir kez sorulur: "Dosyanız
  {path}.celikpanel-backup-<tarih ve saat> olarak saklanacak, sonra yerine
  CelikPanel’in metni yazılacak ve nginx yeniden yüklenecek." [Dosyayı değiştir]
  [Vazgeç]. Sonuç: "CelikPanel’in metni yerinde ve nginx yeniden yüklendi.
  Dosyanız {backup} olarak saklandı."
- "Benimkini koru" — "Dosyanız bayt bayt olduğu gibi kalır. CelikPanel
  seçiminizi kaydeder ve yalnız dosya değişirse yeniden sorar." Sonra: "Bu
  dosyayı {date} tarihinde korumayı seçtiniz. CelikPanel onu değiştirmez; bu
  sitede CelikPanel’de yaptığınız değişiklikler ona uygulanmaz. CelikPanel’in
  metnini dilediğiniz zaman alabilirsiniz." Dosyanın baytlarına, ilk satırına
  bile dokunulmaz: başlığı yeniden yazmak sahibin metnini CelikPanel'in
  değişmemiş metni gibi gösterir ve sonraki üretim onu değiştirirdi.
- "Elle birleştir" — "Dosyayı sunucuda kendiniz düzenlersiniz; burada hiçbir
  şey yapılmaz." Açılınca: "CelikPanel’in metni {pending} içinde. Sunucuda {path}
  dosyasını düzenleyip gerekenleri alın. Dosyayı CelikPanel’e geri bırakmak için
  {pending} dosyasını {path} üzerine kopyalayın. Sonra nginx’i kendiniz kontrol
  edip yeniden yükleyin ve bu sayfayı yeniden yükleyin."

**Sahibin eklemeleri için desteklenen yer:** `siteConfig.include` "Bu site için
kendi nginx yönergelerinizi {dir} içinde bir .conf dosyasına yazın. CelikPanel
oraya hiç yazmaz ve bu sitede yaptığı her değişiklikte onları korur." Dizin
`/etc/nginx/celikpanel-sites.d/<alan adı>/` (sanal konak yazılırken 0755 ile
oluşturulur; içindeki hiçbir şey yazılmaz, değiştirilmez ya da silinmez). Sanal
konak server bağlamında `include <dizin>/*.conf;` satırını, posta adlarının
yalnız doğrulama bloğu ile yalnızca HTTPS'e yönlendiren düz HTTP bloğu dışındaki
her blokta taşır. Yönlendirme sitesinin hedef adrese yönlendiren blokları da
taşır. Satır sitenin `location` bloklarından sonra durur; sahibin dosyası
onlardan birini yinelerse (örneğin `location /`) nginx'in kendi kuralıyla
`nginx -t` bunu reddeder (CelikPanel ile ölçülmedi) ve dosya yazan her üretim de
onunla birlikte reddedilir.

**CelikPanel'in kendi ekleme noktası (adım 1b, 2026-10-10).** CelikPanel
metninin her server bloğu, posta adlarının yalnız doğrulama bloğu ve düz HTTP
yönlendirme bloğu dahil, ayrıca `include
/etc/nginx/celikpanel-managed.d/<alan adı>/*.conf;` satırını taşır. O dizin
CelikPanel'indir (sanal konakla birlikte 0755 oluşturulur); ACME HTTP-01 konumu
(`location ^~ /.well-known/acme-challenge/`, root'a ait doğrulama köküyle)
oraya aynı üretim başlığıyla `acme-http-01.conf` olarak yazılır, artık sanal
konağa yazılmaz. Yaşam döngüsü: sanal konak o işlemden sonra dizini okuyorsa
(CelikPanel'in metni yazıldı ya da değişmedi, ya da ekleme satırını taşıyan
korunmuş dosya) sanal konakla aynı işlemde yazılır; bir sertifika işlemi ya da
Yapılandırma dosyası sayfası ölçtüğünde her korunmuş dosya için de (aşağıda)
yazılır; nginx yapılandırmayı reddedince ya da yeniden yükleyemeyince
sanal konakla birlikte geri konur; site silinirken kaldırılır (doğrulama dosyası
hâlâ CelikPanel'in değişmemiş metniyse o, sonra dizin boş kaldıysa dizin; dizinde
başka ne varsa kalır). Dosya, konumun eskiden olduğu gibi site durdukça kalır;
certbot'un doğrulama kökündeki kendi belirteçleri her doğrulamada eskisi gibi
gelip gider. Değiştirilmiş bir doğrulama dosyası korunur, üzerine yazılmaz
(D-022).

*Hazırlık okunmaz, ölçülür (ikinci tur, 2026-10-10).* Korunmuş bir dosyanın
doğrulamaya izin verip vermediğine artık metninde ekleme satırı bulunarak karar
verilmez. Doğrulama konumu (`location ^~ /.well-known/acme-challenge/ { root
<doğrulama kökü>; try_files $uri =404; }`) istek anında
`<doğrulama kökü>/.well-known/acme-challenge/` içinde ne varsa onu sunar; oraya
konan yeni bir dosya yeniden yükleme gerektirmez. Yoklama
(`internal/services/managed_vhost_probe.go`, `probeValidation`): Agent oraya
rastgele adlı (`celikpanel-probe-<32 onaltılık>`) ve rastgele içerikli bir
dosyayı dışlayıcı biçimde yazar (var olan ad ya da bağ reddedilir),
127.0.0.1'in 80 numaralı bağlantı noktasındaki nginx'e
`/.well-known/acme-challenge/<ad>` yolunu her doğrulama adını Host olarak
vererek sorar (önce sitenin adları, sonra `mail.<alan adı>` gibi yalnız
doğrulama adları), bir yönlendirmeyi yalnız aynı ada (80 ya da 443) izler (o
hedefin sertifikası denetlenmez; kanıt içeriktir) ve dosyayı kaldırır. Hazır:
her ad tam o içerikle 200 yanıtı verdi. Sunulmayan bir site adı
`include_missing`, sunulmayan bir yalnız doğrulama adı `names_missing`dir (site
adı önce gelir); ilk böyle ad ve nginx'in HTTP kodu onunla birlikte gelir. 80
numaralı bağlantı noktasında hiç yanıt olmaması (bağlantı reddedildi, istek
başına 5 sn sınır) ya da yoklama dosyasının yazılamaması, sınırlı bir ayrıntıyla
`unknown` olur, asla "hazır değil" değil. CelikPanel'in değiştirilmiş ya da
yazılamayan doğrulama dosyası, yoklamadan önce `challenge_kept` /
`challenge_failed` olarak kalır. Yoklama istendiğinde CelikPanel önce korunmuş
dosya için kendi doğrulama dosyasını, yoksa ya da başka girdileri taşıyorsa,
yayımlar: bir `nginx -t` ve bir yeniden yükleme; ret onu geri koyar (ekleme
dizinindeki yeni bir `.conf` dosyası yalnız yeniden yüklemede okunur; yoklama
dosyasının kendisi gerektirmez). Yoklama yalnız istendiğinde çalışır
(`ApplyVhostRequest.ProbeValidation`): hiçbir şey istenmeden önce sertifika
alımı, yenileme ve takma ad sertifikası yolu tarafından, ve dosyanın kayıtlı
nedeni `certificate_validation` iken Yapılandırma dosyası sayfasının okuması
tarafından; açılış, kaydedilen bir ayar ya da başka bir üretim hazırlık
değerlendirmez. Sayfa şunu söyler: "CelikPanel bu sitenin kendi parçalarını (sertifika
doğrulaması) {dir} içine yazar. Bu dosyanın onu içeren satırlarını koruyun."
(`siteConfig.managedDir`). Sahibin eklemeleri yine `celikpanel-sites.d` içine
gider. Başlıksız dosyanın dondurulmuş alpha.81/82 metinleriyle karşılaştırılması
değişmedi (o metinler konumu satır içinde taşır).

**Üretmek isteyen işlemlerin retleri** (kabuk kataloğu, `err.<KOD>`, TR):

- `SITE_CONFIG_OWNER_EDITED` (409, reason = durum): "Bu sitenin nginx
  yapılandırma dosyası CelikPanel’in değişmemiş metni değil; bu yüzden
  CelikPanel onu korudu ve bu değişikliği ona uygulamadı. Başka hiçbir şey
  değiştirilmedi. Alan adının Yapılandırma dosyası sayfasında dosyanızı koruyun,
  CelikPanel’in metnini alın (dosyanız tarihli bir kopya olarak saklanır) ya da
  ikisini elle birleştirin."
- `SITE_CONFIG_MISSING` (409): "Bu sitenin nginx yapılandırma dosyası eksik; bu
  yüzden bu değişiklik uygulanmadı ve dosya yeniden oluşturulmadı. Bilerek
  kaldırıldıysa bir şey yapmanız gerekmez; değilse alan adının Yapılandırma
  dosyası sayfasında Yeniden oluştur’u seçin."
- `SITE_CONFIG_UNWRITABLE` (409): "CelikPanel bu sitenin nginx yapılandırma
  dosyasını okuyamadı ya da değiştiremedi (örneğin bir bağlantı ya da
  değişikliğe kilitli); bu yüzden dosyayı olduğu gibi korudu ve bu değişikliği
  uygulamadı. Sunucu sahibi dosyayı sunucuda kontrol eder; nedeni alan adının
  Yapılandırma dosyası sayfasında."
- `SITE_CONFIG_EXISTS` (409, site oluşturma ve içe aktarma): "Bu ad için
  sunucuda CelikPanel’in yazmadığı bir yapılandırma dosyası zaten var; bu yüzden
  CelikPanel onu korudu ve siteyi oluşturmadı. Geride hiçbir şey kalmadı. O dosya
  artık kullanılmıyorsa sunucuda taşıyın ya da adını değiştirin, sonra siteyi
  yeniden oluşturun."
- `SITE_CONFIG_CHANGED` (409): "Yapılandırma dosyası ya da CelikPanel’in metni,
  sayfa onları gösterdikten sonra değişti; bu yüzden hiçbir şey yapılmadı. Sayfa
  dosyayı yeniden okur; ona bakıp yeniden seçin."
- `SITE_CONFIG_NOT_APPLICABLE` (409; sahip düzenlemiş, yabancı ya da kökeni
  bilinmiyor olmayan dosyada koru, ya da var olan ve korunmuş dosyada yeniden
  oluştur): "Bu seçim dosyanın şimdiki hâline uymuyor; bu yüzden hiçbir şey
  yapılmadı. Sayfa dosyayı yeniden okur; hâlâ istiyorsanız yeniden seçin."
- `SITE_CONFIG_NOT_READ` (502; Agent yanıt vermedi, hata ile yanıtladı ya da
  üretim girdisi hazırlanamadı): "CelikPanel bu sitenin yapılandırma dosyasının
  durumunu şu an okuyamadı. Bu, dosyada bir sorun olduğu anlamına gelmez ve hiçbir
  şey değiştirilmedi. Tekrar deneyin."
- `SITE_CONFIG_NGINX_REFUSED` (502, `reason` `nginx_refused` ya da
  `reload_failed`, her biri kendi değeriyle; `details[0]` yöneticiler için
  nginx'in kendi ilk satırı): "nginx, CelikPanel’in metnini kabul etmedi
  (yapılandırmayı reddetti ya da yeniden yükleyemedi); bu yüzden dosyanız geri
  kondu ve nginx onunla çalışmayı sürdürüyor. nginx bütün siteleri birlikte
  kontrol eder; neden başka bir dosya olabilir. nginx’in bildirdiğini düzeltin,
  sonra yeniden seçin." İki durumda da dosya geri konur; "al"ın yazmadan önce
  yaptığı tarihli kopya dosyanın yanında kalır.
- `SITE_CONFIG_OWNER_EDITED`, `reason` `certificate_validation` (409; sertifika
  alımı; `detail` `include_missing`, `names_missing`, `challenge_kept` ya da
  `challenge_failed`; `vars.include` eklenecek satır, `vars.name` ve
  `vars.status` yoklamanın sunulmayan ilk adı ve nginx'in onun için HTTP
  kodu): "Bu sitenin nginx yapılandırma dosyası CelikPanel dışında değiştirildi
  ve CelikPanel’in sertifika doğrulamasını dosyayı değiştirmeden yayımlamasına
  izin vermiyor; bu yüzden sertifika istenmedi ve hiçbir şey değiştirilmedi.
  Seçimi sunucu yöneticisi alan adının Yapılandırma dosyası sayfasında yapar:
  CelikPanel’in metnini alır ya da o sayfanın gösterdiğini dosyaya ekleyip
  nginx’i yeniden yükler; sonra sertifikayı yeniden isteyin."
  (`err.SITE_CONFIG_OWNER_EDITED.certificate_validation`; işi yapan ikinci
  turda adlandırıldı, çünkü bir barındırma müşterisi bunu SSL sekmesinde
  okuyabilir).
- `CERTIFICATE_VALIDATION_UNKNOWN` (503, `detail` `unknown`; sertifika alımı ve
  takma ad sertifikası yolu; yoklama yanıt almadı): "Bu sunucudaki nginx yanıt
  vermediği için CelikPanel, bu sitenin nginx yapılandırma dosyasının sertifika
  doğrulamasına izin verip vermediğini kontrol edemedi; bu yüzden sertifika
  istenmedi ve hiçbir şey değiştirilmedi. Sunucu yöneticisi nginx’in
  çalıştığını denetler; sonra sertifikayı yeniden isteyin." Defter nedeni
  yazılmaz; bu durumdaki bir yenileme hiçbir şey kaydetmez ve bir sonraki
  zamanlanmış çalışmada yeniden sorulur.

**Güncellemeden sonraki ilk durum.** alpha.81 ve alpha.82'nin yazdığı
dosyalarda başlık yoktur. İlk başlangıçta her biri, bu sürümlerin dondurulmuş
şablonlarının aynı veriyle ürettiği metinle, yazdıkları iki biçimde (oluşturma
metni: `server_name` yalnız alan adı; başlangıç/kaydetme metni: `www.` ve takma
adlarla) bayt bayt karşılaştırılır. Eşit dosya devralınır (başlığıyla
CelikPanel'in metni olarak yeniden yazılır); başka her dosya "kökeni bilinmiyor"
olur ve asla yazılmaz. Alan Adları listesi şunu söyler: `siteConfig.list.firstState`
"Site yapılandırma dosyaları: {adopted} tanesi CelikPanel’inki olarak tanınıp
devralındı; {left} tanesi bilinen her CelikPanel metninden farklı olduğu için
olduğu gibi bırakıldı. Dosyasını görmek için bir alan adını açın. Olduğu gibi
bırakılan bir dosyayı yeniden CelikPanel’in yönetmesini istemiyorsanız bir şey
yapmanız gerekmez." Başlangıçtaki
günlük satırı yazılan, değişmeyen, korunan (sahip düzenlemiş / yenisiyle
değiştirilmiş / kökeni bilinmiyor), okunamayan ya da değiştirilemeyen, eksik
(yeniden yazılmayan), başarısız ve devralınan dosyaları ayrı sayar. Siteye ait
sonuçları bildirmeyen bir Agent'a karşı satır şudur: `restored N hosted vhosts;
the Agent did not report what it found in each file, so their state is
unknown`. Agent'ın yanıtladığı bir sitenin her üretimi siteyi ve yapılanı
adlandıran bir satır yazar (`site configuration <alan adı> (<tetikleyici>): …`);
Agent'ın hiç yanıtlamadığı üretim yalnız hatayı yazar. Devralmayı 044. göçün SQL'i
değil (o satır yazmaz, dosyaya dokunmaz), başlıksız dosyayı gören ilk üretim,
normalde ilk başlangıç yapar.

Alan Adları listesi satırı saklanmaz, sahibin kararlarından hesaplanır: kökeni
bilinmeyen en az bir dosyanın şimdiki baytları için "benimkini koru" kararı
yokken çizilir ("al" dosyayı CelikPanel'in metni yapar, sayımdan çıkar) ve olduğu
gibi bırakılan her dosyanın kararı olunca kalkar. Hiçbir dosya olduğu gibi
bırakılmadıysa sahibi bekleyen bir şey yoktur ve satır çizilmez; başlangıç
satırı devralınan dosyaları yine sayar ve her alan adının sayfası bunu söyler.

**Şema ve sürüm (D-025).** Şema 43'ten 44'e (göç 044, `managed_site_files`,
var olan hiçbir tablo değişmez); site dosyası biçimi v2 (ilk satır olarak
`# celikpanel-render v2 sha256=<64 onaltılık>`). Derleme eşleşmesi
(`ExpectedBuildCommit`), başka sürümden bir Agent'ın Panel'in üretimlerini
yanıtlamasını normalde engeller (başlangıç toplu işi reddedilir). Bir Agent
siteye ait sonuç olmadan yanıtlarsa (bileşen testleri bu değişiklikten önceki bir
Agent'ı benzetir) Panel dosyayı "bilinmiyor" (`unknown`,
`agent_does_not_report`) diye kaydeder ve gösterir, asla "değişmedi" diye değil;
`Agent.InspectSiteFile` olmayan bir Agent sayfada aynı durumu verir. Önceki sürüm
Paneli (alpha.82, şema 43) defterinde 44. girdi olan veritabanını reddeder; bu
yüzden alpha.81 ya da alpha.82'ye dönüş, her göçte olduğu gibi güncelleme öncesi
anlık görüntünün geri yüklenmesidir; aşağı göç yoktur.

**Sınırlar.** Eski bir sürüme dönüş (otomatik geri alma dahil) bu korumayı
kaldırır. alpha.81 ve alpha.82 başlangıcı her siteyi `Agent.ApplyVhosts`'a
gönderir; o, her sanal konağı karşılaştırmadan veritabanından yazar (iki
sürümün kaynağında okundu; bu değişiklikten sonra ölçülmedi): sahibin
değişikliği ezilir, başlık satırı ve sahibin include satırı o metinde yoktur;
böylece `/etc/nginx/celikpanel-sites.d/<alan adı>/` içindeki dosyalar diskte
kalır ama güncelleme yeniden uygulanana dek nginx onları okumaz; tarihli
kopyalar, bekleyen dosyalar ve include dizinleri yerinde kalır. Bu sürüm yeniden
çalıştığında eski sürümün metnini bayt bayt tanır ve devralır; eski sürüm
çalışırken ve onun sonraki başlangıcından önce yapılan değişiklik korunur ve
gösterilir. Sürüm notları bunu söyler.

**Korunan dosyada sertifikalar (adım 1b, 2026-10-10; aynı gün ikinci tur).**
Alım ve yenileme doğrulamayı CelikPanel'in kendi ekleme dizini üzerinden
yayımlar; bu yüzden nginx'in onu sunmasına izin veren korunmuş bir dosyaya
dokunulmaz. İzin verip vermediği, hiçbir şey istenmeden önce yukarıdaki
yoklamayla ölçülür:

- *Alım, yoklama her adla sunuldu:* daha önce kaydedilmiş bir
  `certificate_validation` nedeni hemen biter (defter nedeni temizlenir, bir
  yenilemenin `waiting_for_owner` durumu bırakılır); sertifika istenir,
  CelikPanel'in sertifika deposuna kurulur ve defterde etkinleştirilir; sitenin
  TLS bloğu, dosyanın yanında bekletilen CelikPanel metnine girer; dosya
  gösterdiği sertifikayı göstermeyi sürdürür ve sahip bir şey yapana dek o
  sunulur (hiç sertifikası olmayan site TLS'siz kalır ve sayfada "kullanılan
  sertifika" satırı olmaz). Posta TLS'i, sertifika postayı koruyorsa, yeni
  sertifikayı hemen izler (kaynaktan okundu); yalnız web sitesi bekler. Yanıt
  `200 {"status":"waiting_for_owner","expires_at":…,"pending_reason":"certificate"}`;
  sertifikanın yenileme durumu `waiting_for_owner`, defter nedeni
  `certificate`. SSL sekmesi alımı başarı değil uyarı olarak gösterir: "Sertifika
  alındı, ancak bu sitenin nginx yapılandırma dosyası CelikPanel dışında
  değiştirildi ve hâlâ öncekini kullanıyor. Seçimi sunucu yöneticisi alan
  adının Yapılandırma dosyası sayfasında (Barındırma sekmesi) yapar." ve bekleme
  sürdükçe sertifika kartının durumunu, yenileme satırını
  (`ssl.renewal.waitingForOwner`, "Sitenin yapılandırma dosyası için bir seçim
  bekleniyor") ve bir paragrafı, her birini nedenine göre (SSL yanıtının
  eklemeli `waiting_for_owner_reason` alanı) gösterir: `certificate` için durum
  `ssl.status.waitingForOwner.certificate` "Yeni sertifika hazır; sitenin
  dosyası hâlâ eskisini gösteriyor" ve `ssl.waitingForOwner` "Bu sitenin nginx
  yapılandırma dosyası CelikPanel dışında değiştirildi ve bu sertifikayı henüz
  kullanmıyor; site şu an kullandığı sertifikayla sürer. Seçimi sunucu
  yöneticisi alan adının Yapılandırma dosyası sayfasında (Barındırma sekmesi)
  yapar."; `certificate_validation` için
  `ssl.status.waitingForOwner.certificate_validation` "Sertifika isteğini
  sitenin dosyası durdurdu" ve `ssl.waitingForOwner.validation` "Bu sitenin
  nginx yapılandırma dosyası CelikPanel dışında değiştirildi ve sertifika
  doğrulamasına izin vermiyor; bu yüzden son istek ya da yenileme durduruldu.
  Seçimi sunucu yöneticisi alan adının Yapılandırma dosyası sayfasında
  (Barındırma sekmesi) yapar; sonra sertifikayı yeniden isteyin."; alanı
  taşımayan bir yanıt `ssl.status.waitingForOwner` "Sitenin yapılandırma
  dosyası için bir seçim gerekiyor" ile kalır. Sekme bir barındırma müşterisi
  için de çizilebilir; Yapılandırma dosyası sayfası yalnız yöneticiler için
  kalır, bu yüzden bu cümlelerin her biri işi yapanı sunucu yöneticisi olarak
  adlandırır (pano yalnız yöneticiler içindir ve "seçiminiz" der). Sayfa: "Yeni
  sertifika hazır, henüz kullanılmıyor" — "CelikPanel bu site için yeni bir
  sertifika aldı, ancak yapılandırma dosyanız hâlâ öncekini gösteriyor ve siz
  bir şey yapana dek nginx o sertifikayı sunmayı sürdürür. Ya aşağıda
  CelikPanel’in metnini alın (CelikPanel dosyayı değiştirir ve nginx’i yeniden
  yükler) ya da dosyanızdaki iki sertifika satırını burada gösterilenlerle
  değiştirin; sonra nginx’i kendiniz kontrol edip yeniden yükleyin ve
  Benimkini koru’yu seçin.", iki satır (`ssl_certificate …;`,
  `ssl_certificate_key …;`) ve (önceki sertifikanın) "Kullanılan sertifikanın
  süresi {date} tarihinde doluyor ({days} gün kaldı)." `certificate` nedeni ve
  sertifikanın `waiting_for_owner` durumu birlikte biter: dosya yeniden
  CelikPanel'in metni olunca ya da bir üretim, bir başlangıç veya "benimkini
  koru" korunan dosyada `ssl_certificate <yeni yol>;` bulunca
  (`observeSiteFileCertificate`).
- *Alım, yoklama sunulmadı* (bir site adı: `include_missing`; `mail.<alan adı>`
  gibi yalnız doğrulama adı: `names_missing`; ya da CelikPanel'in doğrulama
  dosyası değiştirilmiş): hiçbir şey istenmeden reddedilir
  (`SITE_CONFIG_OWNER_EDITED`, `certificate_validation`, `vars.name` ve
  `vars.status` ile, yukarıda); defter nedeni `certificate_validation`. Sayfa:
  "Bu dosyayla sertifika doğrulanamıyor" — bir site adı için "Bu sunucudaki
  nginx, CelikPanel’in sertifika doğrulamasını bu sitenin adıyla sunmadı; bu
  yüzden CelikPanel dosyanızı değiştirmeden sertifikayı alamaz ya da
  yenileyemez. Ya aşağıda CelikPanel’in metnini alın (CelikPanel dosyayı
  değiştirir ve nginx’i yeniden yükler) ya da burada gösterilen satırı
  dosyanızın 80 numaralı bağlantı noktasını dinleyen server bloğuna ekleyin;
  sonra nginx’i kendiniz kontrol edip yeniden yükleyin ve Benimkini koru’yu
  seçin." satırla birlikte; yalnız doğrulama adı için "Sertifika mail.{domain}
  gibi yalnız doğrulama için kullanılan bir adı da kapsıyor ve bu sunucudaki
  nginx, CelikPanel’in doğrulama dosyasını o adla sunmadı. Ya aşağıda
  CelikPanel’in metnini alın (CelikPanel dosyayı değiştirir ve nginx’i yeniden
  yükler) ya da o adın server bloğunu farktan dosyanıza kopyalayın; sonra
  nginx’i kendiniz kontrol edip yeniden yükleyin ve Benimkini koru’yu seçin.";
  ardından nginx'in yanıtı, `siteConfig.certificate.validation.probe` "Bu
  sunucudaki nginx’in yanıtı: {name} için HTTP {status}.", ve
  `siteConfig.certificate.schedule` "Bu sitenin süresinin dolmasına 30 günden
  az kalmış bir sertifikası varsa yenileme kontrolü yaklaşık 12 saatte bir
  yeniden bakar. SSL sekmesinden yapılan bir istek yeniden denenmez; gerekeni
  yaptıktan sonra yeniden isteyin." (artık ilk isteğin yeniden denendiğini ima
  eden cümle değil). `challenge_kept` artık işi yapanı adlandırır: "… Sunucu
  sahibi o değişikliği geri alır ya da dosyayı kaldırır, sonra bu sayfayı
  yeniden yükler." Eksik ya da okunamayan dosya "korunmuş" değildir: alım o
  zaman o dosyanın kendi koduyla `409` verir (`SITE_CONFIG_MISSING`,
  `SITE_CONFIG_UNWRITABLE`); önceki sürüm tipsiz bir cümle veriyordu.
  *Bilinmiyor* (80 numaralı bağlantı noktasında yanıt yok): `503
  CERTIFICATE_VALIDATION_UNKNOWN` (yukarıda), defter nedeni yok; sayfa bunu
  ölçtüğünde "Bu dosyayla sertifikanın doğrulanıp doğrulanamayacağı
  bilinmiyor" — "Bu sunucudaki nginx yanıt vermediği için dosyanızın
  doğrulamaya izin verip vermediği şu an kontrol edilemedi. Bu, dosyada bir
  sorun olduğu anlamına gelmez ve hiçbir şey değiştirilmedi. Sunucu sahibi
  nginx’in çalıştığını denetler, sonra bu sayfayı yeniden yükler." der.
- *`certificate_validation` nedeni hazırlık yeniden sağlanınca biter* (açıktı).
  CelikPanel'in metni yerine geçince, yoklaması sunulan bir sonraki alım ya da
  yenilemede (yukarıda) ve Yapılandırma dosyası sayfasının okumasında biter:
  defter `certificate_validation` dedikçe o okuma yoklamayı ister; her ad
  sunulunca neden temizlenir, bir yenilemenin `waiting_for_owner` durumu
  bırakılır (pano kaydı gider; sertifika 30 gün içindeyse sonraki zamanlanmış
  yenileme yeniden vadesi gelir) ve yanıt `resolved_reason:
  "certificate_validation"` taşır. Sayfa o zaman bir kez "nginx artık bu
  sitenin sertifika doğrulamasını sunuyor" — "Sertifikayı SSL sekmesinden
  yeniden isteyin; süresinin dolmasına 30 günden az kalmış mevcut bir
  sertifikayı yenileme kontrolü de yeniden dener." gösterir ve alan adları
  listesini yeniden okur; böylece sekmelerin üstündeki satır ("Bu site için son
  sertifika isteğini ya da yenilemeyi sitenin nginx yapılandırma dosyası
  durdurdu. Dosyaya ne olacağını seçin; sonra sertifikayı yeniden isteyin.") ve
  Alan adları rozeti nedenle birlikte gider. O neden olmadan yapılan okuma
  yoklama istemez. Eksik ya da okunamayan dosya hâlâ neden yazmaz; bu yüzden
  dosyayı geri koymak bir yenilemenin bekleme durumunu ancak bir sonraki
  tamamlanan yenilemede bitirir.
- *Yenileme:* aynı yollar. Dosyanın durdurduğu bir yenileme (yoklama
  sunulmadı, eksik, okunamaz) hiçbir şey istenmeden `failed` değil
  `waiting_for_owner` olarak kaydedilir; yoklaması yanıt almayan bir yenileme
  hiçbir şey kaydetmez (ne bekliyor ne başarısız). Durdurulmuş bir yenilemeyi
  dosyayı yeniden ölçen zamanlamadan (12 saatte bir ve Panel başlarken;
  otomatik yenilemeli Let’s Encrypt sertifikaları, süresi 30 gün içinde
  dolacaklar) başka hiçbir şey yeniden denemez. Hazır korunmuş dosyada ilerleyen
  bir yenileme yenilenmiş sertifikayı kurar ve alımdaki gibi bekler; o
  sertifika süresinin dolmasına yakın değildir, bu yüzden başka yenileme
  gerekmez ve beklemeyi yalnız sahibin eylemi bitirir. Panonun ilgi listesi
  (yalnız yöneticiler) bekleyen her sertifikayı, gün sayısı ne olursa olsun,
  listenin başında, kullanılan sertifikaya göre sıralı taşır: önce süresi
  dolmuş olan, sonra en az günü kalan (`sortDashboardCertificates`): "{domain}:
  bir sertifika, sitenin yapılandırma dosyası için seçiminizi bekliyor;
  kullanılan sertifikanın {days} günü kaldı" ya da süresi dolmuş olan için yeni
  `dashboard.certWaitingOwnerExpired` "{domain}: bir sertifika, sitenin
  yapılandırma dosyası için seçiminizi bekliyor; kullanılan sertifikanın süresi
  {days} gün önce doldu" (asla eksi gün sayısı değil; `expiring_certs`
  `waiting_for_owner`, `served_days_left`, `domain_id` taşır; en çok altı kayıt).
- *Takma ad sertifikası yolu* (`cmd/panel/alias_certificates.go`,
  `issueAliasCertificateSnapshot`; açıktı) aynı iki yolu izler: aynı hazırlığı
  yoklamayla çağırır; yoklaması sunulan korunmuş dosyada sertifikayı dosyaya
  dokunmadan ve "geri koyma" üretimi olmadan ister; etkinleştirme üretimi sonra
  dosyayı korunmuş bulur, sertifikayı sahibi bekliyor olarak kaydeder
  (`activateAliasCertificateVhost`, alımdaki gibi), posta TLS'ini adımda tutar
  ve takma ad yanıtı `status: "waiting_for_owner"`, `pending_reason:
  "certificate"` taşır; Genel sekmesi o zaman "Takma ad eklendi" ve yukarıdaki
  alım uyarısını gösterir. Sunulmadıysa takma ad değişikliği alımın tipli `409`
  yanıtını verir; bilinmiyorsa `503 CERTIFICATE_VALIDATION_UNKNOWN`. Böyle bir
  sertifikanın yenilemesi yukarıdaki yenilemedir.
- *Sertifika kademesi* (`web/src/lib/sslTier.ts`; açıktı): süresi dolmuş,
  geçersiz ya da güvenilmeyen kullanılan sertifika bekleme durumunun önüne
  geçer ve öyle gösterilir; bekleme durumu, denetlenemeyen bir güvenin
  (doğrulanmış bir hata değil) önüne geçer.

Adım 1b'den önce korunmuş bir dosyada (alpha.81/82 düzenlemesi, kökeni
bilinmiyor) ekleme satırı yoktur: sahip CelikPanel'in metnini alana ya da
dosyanın doğrulamayı sunmasını sağlayana dek sertifikası bekler. PHP-FPM havuzu
ve uygulama birimi henüz kapsanmıyor (ikinci adım). Dar ekranda Alan adları
listesi rozetleri kesmek yerine alan adının altına sarar (`Domains.tsx`); koşum
düzeneği her durum için `pageOverflowX` kaydeder ve bu koşunun telefon
genişliğinde yapılıp yapılmadığı burada kayıtlı değildir. Farkın iki başlığı
sayfanın dilinde çizilir ("(bu sunucuda)", "CelikPanel’in metni"). İkinci tur
kanıtı: Go bileşen testleri (sahte nginx ve bir gerçek geri döngü HTTP
isteğiyle `internal/services/managed_vhost_probe_test.go`,
`cmd/panel/site_config_probe_test.go`, `cmd/agent/vhost_probe_test.go`), web
testleri (`ssl-tier-order`, `site-config-mounted`) ve taklit tarayıcı
(`a83ssl`, `a83dash`, `a83siteconfig`; masaüstü ve telefon, İngilizce ve
Türkçe; ekran görüntülerine bakıldı). Bileşen testli ve taklit tarayıcılı;
gerçek sistemde ölçülmedi (yoklama hiç gerçek bir nginx'e sormadı); ölçüm
hücreleri denetimin §9'udur.

**Entegratörler için: API (sürüm notlarına dek başvuru).** Tüm rotalar yönetici
içindir (aksi hâlde `403 {"error":"administrator access is required"}`);
barındırılan sitesi olmayan alan adı `404`. Üç POST rotası D-029'un istek
kimliğiyle korunur: `X-CelikPanel-Request-Id` başlığı (32 küçük harfli onaltılık
karakter, her eylem için yeni) zorunludur (yoksa `428 REQUEST_ID_REQUIRED`,
biçimi bozuksa `400`); aynı kimlik ve aynı gövde saklı yanıttan yanıtlanır
(`X-CelikPanel-Request-Replayed: 1`), farklı gövdede `409 REQUEST_ID_REUSED`,
çalışırken `409 REQUEST_IN_PROGRESS`, Panel yeniden başlamışsa `409
REQUEST_OUTCOME_UNKNOWN`. Hatalar `{"error": <İngilizce cümle>, "code": …,
"reason"?: …, "detail"?: <tek makine belirteci>, "vars"?: {…}, "details"?: […]}`
biçimindedir; ekranlar `code`'a göre çevirir.

- `GET /api/v1/domains/{id}/site-config` (Agent'ı okur; hiçbir şey yazmaz,
  yalnız defterin nedeni `certificate_validation` iken yoklama ister: doğrulama
  kökünde yeniden kaldırılan bir yoklama dosyası, yoksa CelikPanel'in doğrulama
  dosyası yayımlanır ve ölçülen `ready` o nedeni temizler) → 200
  ```json
  {"domain_id":12,"domain":"example.test","kind":"nginx_vhost",
   "path":"/etc/nginx/sites-available/example.test.conf",
   "state":"owner_edited","reason":"","detail":"","adopted_from":"",
   "include_dir":"/etc/nginx/celikpanel-sites.d/example.test",
   "enabled":"link","file_sha256":"<64 hex>","render_sha256":"<64 hex>",
   "pending_path":"…/example.test.conf.celikpanel-pending",
   "diff":"--- …\n+++ …\n@@ … @@\n…","diff_truncated":false,
   "actions":["keep","take","merge"],
   "decision":{"kind":"keep_mine","decided_at":"2026-10-10T12:00:00Z","current":true},
   "ledger":{"written_release":"…","written_at":"…","observed_at":"…","backup_path":"…"}}
  ```
  `state`: `absent`, `managed_unchanged`, `owner_edited`, `foreign`,
  `unreadable`, `unknown_origin` ya da `unknown` (`reason`
  `agent_does_not_report`; `path` ve özet yok). `reason` (unreadable):
  `symlink`, `not_regular`, `permission`, `too_large`, `read_failed`; `detail`
  sınırlı tek satırdır. `enabled`: `link`, `absent` ya da `other`
  (sites-enabled girdisi). `actions` (hep dizi): owner_edited, foreign ve
  unknown_origin için `["keep","take","merge"]`; absent için `["recreate"]`;
  diğerlerinde `[]`. `merge`'in rotası yoktur: sunucuda yapılır. `diff`,
  diskteki dosya ile CelikPanel'in metninin (başlık satırı hariç) birleşik
  farkıdır; taraf başına en çok 4000 satır ve 64 KiB; `diff_truncated` kesildiğini
  söyler; authorization, password, passwd, secret, token, api key, private key ya
  da cookie sözcüklerinden birini (büyük/küçük harf fark etmez) anan satır
  yönergesini korur ve `[hidden by CelikPanel: this value may be a credential]`
  gösterir (`auth_basic_user_file`, `ssl_certificate_key`, `ssl_password_file`
  yolları kalır); böyle bir sözcüklü yorum satırı bütünüyle değiştirilir. Bu
  sözcükleri kullanmayan bir kimlik bilgisi gizlenmez; alan yalnız yöneticiler
  içindir. `decision.current`, yalnız dosya kararın verildiği özetle aynı
  kaldıkça doğrudur. Adım 1b şunları ekler, her biri yalnız biliniyorsa:
  `managed_dir`, `managed_include` (sanal konağın ihtiyaç duyduğu tam satır),
  `challenge_file` (GET'te `absent`, `unchanged`, `differs`, `kept`; yazan bir
  işlemin yanıtında `written` ve `failed` da görünür), `validation` (yalnız
  korunan dosyalarda ve yalnız onu ölçen bir okumada: `ready`,
  `include_missing`, `names_missing`, `challenge_kept`, `challenge_failed`,
  `unknown`; `validation_name` ve `validation_status` ile, yoklamanın sunulmayan
  ilk adı ve nginx'in HTTP kodu), `resolved_reason` (ikinci tur: bu okumanın
  yoklaması dosyayı hazır bulup o nedeni bitirdiyse `certificate_validation`),
  `pending_reason` (defterin sertifika nedeni: `certificate`,
  `certificate_validation`) ve `certificate` (yalnız `pending_reason` varsa:
  `{cert_path, key_path, expires_at, served_expires_at, served_days_left,
  referenced}`; `cert_path` ve `key_path` CelikPanel'in deposundaki etkin
  sertifikadır, `expires_at` onun bitişi, `served_*` korunan dosyanın büyük
  olasılıkla hâlâ sunduğu sertifikadır — `certificate` için öncekisi,
  `certificate_validation` için etkin olan — ve olmayabilir; süresi dolunca
  `served_days_left` eksidir; `referenced` korunan dosyanın etkin sertifikayı
  adlandırdığını söyler). `take` ve `recreate` yanıtları `managed_dir`,
  `managed_include` ve `challenge_file` taşır. Hata: `502 SITE_CONFIG_NOT_READ`.
- `POST …/keep` gövde `{"file_sha256":"<GET'ten 64 hex>"}` (zorunlu; gelen
  `render_sha256` yok sayılır) → 200, karardan sonraki GET ile aynı nesne.
  `400` (özet eksik ya da bozuk), `409 SITE_CONFIG_NOT_APPLICABLE`, `409
  SITE_CONFIG_CHANGED` (dosyanın özeti farklı), `502 SITE_CONFIG_NOT_READ`.
  Dosyaya hiçbir şey yazılmaz.
- `POST …/take` gövde `{"file_sha256":"…","render_sha256":"…"}` (ikisi de
  zorunlu, ikisi de GET'ten: sahibe gösterilen dosya ve CelikPanel'in metni) → 200
  ```json
  {"domain_id":12,"domain":"example.test","kind":"nginx_vhost","path":"…",
   "state":"managed_unchanged","include_dir":"…","enabled":"link",
   "file_sha256":"<yeni dosya>","render_sha256":"…","actions":[],
   "outcome":"taken","backup_path":"….celikpanel-backup-20261010T120000Z",
   "decision":{…},"ledger":{…}}
  ```
  `outcome` `taken`, dosya zaten CelikPanel'in metniyse `unchanged`;
  kopya gerekmediyse `backup_path` boştur. Hatalar: `400`, `409
  SITE_CONFIG_CHANGED`, `409 SITE_CONFIG_MISSING` (dosya yok), `409
  SITE_CONFIG_UNWRITABLE` (`reason` `symlink`, `not_regular`, `permission`,
  `too_large`, `read_failed`, `write_refused`), `502 SITE_CONFIG_NGINX_REFUSED`
  (`reason` `nginx_refused` ya da `reload_failed`), `502 SITE_CONFIG_NOT_READ`.
- `POST …/recreate` gövde `{}` (ya da yok) → 200, `take` gibi, `outcome`
  `recreated`; var olan ve CelikPanel'in kendi metni olan dosyada yazar ya da
  `unchanged` bildirir. Hatalar: `409 SITE_CONFIG_NOT_APPLICABLE` (dosya var ve
  korunmuş), `409 SITE_CONFIG_UNWRITABLE`, `502 SITE_CONFIG_NGINX_REFUSED`,
  `502 SITE_CONFIG_NOT_READ`.
- Alan adları listesi (`GET /api/v1/domains`) yalnız yönetici için
  `site_config: {"state":…,"adopted_from"?:…,"kept_by_choice"?:true,"pending_reason"?:…}` taşır;
  Panel'in kaydettiği son gözlemden gelir (`missing` saklanan bir durumdur; yukarıdaki
  GET onu hiç döndürmez, `absent` döndürür).
- Sertifika alımı, `POST /api/v1/domains/{id}/ssl/letsencrypt` (adım 1b).
  CelikPanel'in dosyayı koruduğu site için eklenen yanıtlar: `200
  {"status":"waiting_for_owner","expires_at":"…","pending_reason":"certificate"}`
  (alındı ve saklandı, henüz sunulmuyor; olağan başarı
  `{"status":"success","expires_at":"…"}`), ve hiçbir şey istenmeden önce `409
  {"code":"SITE_CONFIG_OWNER_EDITED","reason":"certificate_validation",
  "detail":"include_missing"|"names_missing"|"challenge_kept"|"challenge_failed",
  "vars":{"include":"include /etc/nginx/celikpanel-managed.d/<alan adı>/*.conf;",
  "name":"www.<alan adı>","status":"301"}}` (her değişken yalnız biliniyorsa;
  `name` ve `status` yoklamadan, ikinci tur), ve yoklama yanıt almadıysa `503
  {"code":"CERTIFICATE_VALIDATION_UNKNOWN","detail":"unknown"}`. Eksik ya da
  okunamayan dosya, önceki sürümün tipsiz `409`'u yerine `409
  SITE_CONFIG_MISSING` / `SITE_CONFIG_UNWRITABLE` verir (`reason` yukarıdaki
  gibi). Doğrulamanın yayımlanıp yayımlanamayacağını söylemeyen bir Agent'a
  `include_missing` olarak yanıt verilir. Takma ad rotaları (`POST
  /api/v1/domains/{id}/aliases`, `confirm_certificate_reissue` ile `DELETE
  …/aliases/{alias}`) aynı retleri verir; hazır korunmuş dosyadaki yeniden
  alım `"success"` yerine `"status":"waiting_for_owner",
  "pending_reason":"certificate"` ile yanıtlar.
- `GET /api/v1/domains/{id}/ssl`: her sertifika nesnesi `waiting_for_owner`
  (bool) ve `renewal_status` taşır; değerler `` (yok), `expiring`, `current`,
  `renewed`, `failed`, `activation_pending`, `dependents_pending`,
  `waiting_for_owner`. Sonuncusu adım 1b ile yenidir: korunan dosya sertifikayı
  henüz kullanmıyor ya da yenilemesini durdurdu; yukarıda anlatıldığı gibi biter
  ve oradaki alım, yenileme ve defter yollarıyla konur; onu değiştiren tek
  okuma, yoklaması `certificate_validation`'ı bitiren site-config okumasıdır.
  `waiting_for_owner_reason` (ikinci tur, eklemeli, yalnız `waiting_for_owner`
  ile): `certificate` (korunan dosyanın henüz kullanmadığı yeni sertifika) ya da
  `certificate_validation` (dosyanın durdurduğu istek ya da yenileme; defter
  nedeni yazmayan eksik ya da okunamayan dosyanın durdurduğu yenileme için de).
- `GET /api/v1/dashboard`: `expiring_certs[]` kayıtları `waiting_for_owner:
  true`, `domain_id` ve `served_days_left` (kullanılan sertifikanın kalan günü;
  o zaman `days_left` etkin sertifikanınkidir) taşıyabilir; bu kayıtlar gün
  sayısı ne olursa olsun, diğerlerinden önce, `served_days_left`'e göre sıralı
  listelenir (süresi dolmuş kullanılan sertifika, eksi, önce); diğerleri
  `days_left`'e göre.
- Dosyayı üreten diğer işlemler `409` verir: `SITE_CONFIG_OWNER_EDITED` (`reason`
  = durum), `SITE_CONFIG_MISSING` ya da `SITE_CONFIG_UNWRITABLE` (`reason` yukarıdaki
  gibi); site oluşturma ve içe aktarma `409 SITE_CONFIG_EXISTS` verir.
