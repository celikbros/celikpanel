# Stratejik Kararlar

*Proje kaydı · [English](DECISIONS.md)*

Büyük yön kararlarının **neden**inin kalıcı kaydı — soru her yeniden
gündeme geldiğinde sıfırdan türetmek istemediğimiz gerekçeler. Kod kararları
git'te yaşar; bu dosya strateji içindir. En yeni en üstte.

---

## D-019 · Panel korkutmaz: her yönetim sayfası kendi yardımını taşır; boş sayfa yasaktır

*25 Temmuz 2026*

**Karar.** Operatör istedi: "Kullanıcı dostu olması önemli. Korkutmasın,
yardımcı olsun... yönetim sayfasında bir yardım butonu olsun ve sayfa ile
ilgili yardım alınabilsin." Üç kural kalıcıdır:

1. **Her yönetim sayfasında Yardım düğmesi vardır.** Tek kabuktan gelir
   (ServiceShell); yeni sayfa yazan kimse yardımı ayrıca hatırlamak zorunda
   değildir. İçerik üç bölümdür: *Bu nedir?* (düz dille 2-3 cümle),
   *İpuçları* (panelden yapılacak pratik işler), *Bir şey mi ters
   görünüyor?* (belirti → çözüm). Dil teknik değildir: "arka plan süreci",
   "veritabanında çöp kutusu yoktur", "okumak her zaman güvenlidir".
2. **Yardım içeriği chrome değil İÇERİKtir.** i18n anahtar dosyalarına
   yüzlerce `help.x.tip3` anahtarı olarak dağıtılmaz; bileşen başına tek
   kayıtta iki dil yan yana durur (`web/src/help/serviceHelp.ts`). Tek dili
   güncelleyip öbürünü unutmak yapısal olarak imkânsızdır. Özel kaydı olmayan
   bileşen türünün genel kaydını alır — yardımsız sayfa var olamaz.
3. **Boş yönetim sayfası yasaktır.** Panel bir bileşenin durumunu, birimini,
   sürümlerini, paketlerini, portlarını, ayar dosyalarını ve günlüğünü zaten
   biliyor; "yakında" yazan bir sayfa, bilineni göstermeyi reddetmektir.
   Türetilmiş paneller (ComponentPanels) her sayfanın tabanıdır; özel
   sayfalar bunun ÜSTÜNE koyar, yerine geçmez.

**Neden.** Korkutan sayfa kullanılmaz; kullanılmayan panel güvenilmez. Yardım
düğmesinin sayfanın okuyucusunu korkuttuğu anda orada olması gerekir — kurulu
olmayan, düşmüş, yarım servislerde de görünür. Elle yazılmış sayfa listeleri
(9 sayfalık switch) tarayıcının elle unit listesi gibi çürüdü; türetilmiş
taban bu sınıfı kapatır.

---

## D-018 · Kurulum kaynağı cinse göre sabittir: sistem servisi = dağıtım paketi, uygulama/runtime = resmi sürüm, Docker temel değildir

*24 Temmuz 2026*

**Karar.** Operatör sordu: "her zaman git'ten çekip sisteme göre derlesek ya da
Docker kullansak daha iyi olmaz mı?" Cevap "her şeye tek yöntem" değildir —
kaos onu değil, kuralsızlığı doğurur. Kaynak, kalemin CİNSİNE göre seçilir ve
her cins için kural sabittir:

1. **Sistem servisi** (nginx, PostgreSQL, MariaDB, Postfix, Dovecot, Redis…)
   → **dağıtımın paket deposu** (apt/pacman/dnf). Neden: güvenlik yamasını
   dağıtımın güvenlik ekibi üstlenir (`apt upgrade` yamalıyor); systemd
   entegrasyonu ve config yolları hazır. Kaynaktan derlemek CVE takibini bize
   yükler; Docker'lamak systemd üzerinden başlat/durdur/durum okumayı kırar.
2. **Runtime + taşınabilir uygulama** (Node.js, Roundcube, ileride
   Python/Ruby, webmail alternatifleri) → **üreticinin resmi sürümü**
   (indir + sabit SHA-256 doğrula + aç). Neden: dağıtım tek sürüm dondurur,
   biz çok+güncel isteriz; ve dağıtım paketi dağıtıma özgüdür (Roundcube'da
   canlı kanıt: apt `roundcube` / pacman `roundcubemail` farklı yerleşim →
   Debian'da çalışıp Arch'ta çalışmıyordu). Resmi sürüm her Linux'ta TEK yol
   (D-004). Bu "git'ten çek + derle" DEĞİL "imzalı/donmuş sürüm" — composer/
   derleme adımının kırılganlığı yok.
3. **Docker temel DEĞİLDİR.** Paylaşımlı barındırma modeli sistemde doğrudan
   çalışır (nginx vhost + site başına FPM havuzu); 100 site = 100 konteyner
   israfı + port/ağ/volume kaosu + systemd entegrasyonu kaybı. Docker ileride
   bir CİNS olabilir (müşterinin kendi konteyner uygulaması için proje tipi,
   ya da izole/ağır bir yığın için mağazada seçenek) ama altyapının zemini
   değil — Plesk'te de eklentidir, ana model değil.
4. **Kaynaktan derleme**: yalnız gerçekten gerekince (dağıtımda yok VE resmi
   binary yok — nadir); o zaman derleme araçları geçici kurulur, sonra
   kaldırılır (temiz-sunucu).

**Dürüst sınır (aynı dilimde kanıtlandı).** "Her Linux'ta tek yol" ideali
bedava değildir: taşınabilir bir tarball bile ALT-bağımlılıklarda dağıtım
gerçeklerine değer — Roundcube'un SQLite'ı PHP'nin pdo_sqlite'ına muhtaçtı
(iki dağıtımda ayrı paket, Arch'ta üstelik elle enable), FPM soket yolu
farklıydı, nginx conf yerleşimi Debian-only'ydi (A10). Doğru mimari saf değil
HİBRİTTİR: taşınabilir gövde + dağıtım-farkındalıklı alt-detaylar (paket adı,
soket yolu, uzantı enable) — ve o alt-detayların dağıtıma özgü tek gerçeği
agent'ta yaşar, panel/katalog dağıtım bilmez.

**Reddedilen alternatifler.**
- **Her şeyi kaynaktan derlemek:** çoğu şey (PHP/script) derlenmez; sistem
  servislerinde CVE-yama sorumluluğunu üstlenmek; "temiz sunucu"ya derleme
  araçları dolar.
- **Her şeyi Docker'a koymak:** shared-hosting DNA'sıyla çatışır; systemd
  temelli tüm mekanizmalar (durum okuma, başlat/durdur) kırılır.
- **Her şeyi dağıtım paketiyle kurmak:** runtime'da tek-sürüm hapsi;
  taşınabilir uygulamada dağıtıma özgü yerleşim (Roundcube tuzağı).
- **"Duruma göre karar" (yazılı kural yok):** her yeni kalemde aynı tartışma;
  ve sınıf-atlaması riski (bir uygulamayı sistem servisi gibi paketlemek).

---

## D-017 · Sunum evrenseldir: seçilebilen her şey — bileşen, sürüm, entegrasyon, ürün — tek teklif uzayında zincirden geçer

*24 Temmuz 2026*

**Karar.** Operatörün cümlesi kanun maddesi niteliğinde: *"müşteri SSL seçerken
veya bayi müşterilerine hangi SSL'leri seçme izni vereceğini düşünürken,
esasında bizim onlara sunduğumuz seçenekler içinde hareket edecekler."*

1. **Zincirin öznesi genelleşir.** D-014'ün `kurulu ⊇ sunulan ⊇ kullanılan`
   zinciri yalnız kurulu bileşenler için değil, SEÇİLEBİLEN her şey için
   geçerlidir — sunucuya hiçbir şey kurmayan entegrasyonlar (SSL CA'ları,
   ileride Cloudflare, yedek hedefleri) dahil. Müşteri deneyiminde "hangi
   CA" seçimi, "hangi PHP sürümü" seçiminden farksız bir tekliftir.
2. **Tek teklif-kimliği uzayı.** Her sunulabilir kalem kanonik bir kimlik
   taşır: `component:<id>` (bütün) · `component:<id>:<sürüm>` (D-014: birim
   sürümdür) · `integration:acme:<id>` · `product:<id>` (D-012'nin
   `subscription_entitlements.product_id`'si bu uzayın ilk sakinidir).
   Teklif türü başına ayrı tablo/model YOKTUR — "aynı bilginin iki sahibi"
   hastalığının teklif katmanındaki karşılığı olurdu.
3. **Üç katman, üç ayrı soru; etkin küme = kesişim.**
   *Mevcut* (admin): kurulu bileşenler + kayıt defterinde sunucu genelinde
   AÇIK bırakılanlar — admin bir CA'yı bütün panelde kapatabilmelidir.
   *Sunulan* (bayi): plan içeriği — D-014'ün "eksik halka"sı burada
   `plan_offerings` (plan ↔ teklif-kimliği) olarak doğar.
   *Kullanılan* (müşteri): kullanım anındaki seçim (site başına sürüm,
   sertifika başına CA).
4. **Uygulama deseni her seçici uçta AYNIDIR.** Listeleme uçları (ssl
   sağlayıcıları, php sürümleri, node sürümleri…) çağıranın etkin kümesine
   süzülür; eylem uçları sunucu tarafında yeniden doğrular ve kodlu retle
   döner (`NOT_OFFERED` ailesi). UI yalnız sunulanı çizer — D-012 #7'nin
   ("görünürlük hakkı izler") genel hâli.
5. **Varsayılan cömert, daraltma bilinçli.** Plan hiçbir kısıt bildirmiyorsa
   her şey sunulur (bugünkü davranış aynen). Mekanizma geriye dönük hiçbir
   şeyi kırmaz; bayi daraltmak İSTEDİĞİNDE daraltır — D-014'teki
   "sadeleştirme hakkı"nın uygulanması.
6. **Zamanlama dürüstlüğü.** Tam mekanizma v0.3 kiracı/plan dilimiyle gelir
   (plan UI'sı olmadan davranışsız iskelet kurmak tiyatro olur). Bugünden
   geçerli olan tek yükümlülük: yeni yazılan her seçici uç bu deseni bilerek
   yazılır ve teklif kimlikleri şimdiden kanonik adlandırılır.

**Neden.** Bugün üç ayrı seçici doğdu (PHP sürümü, Node sürümü, SSL CA'sı) ve
üçü de "listeyi kim daraltır?" sorusuna cevapsız. Operatör, cevabın üçü için
de AYNI olması gerektiğini gördü. Üç ayrı daraltma mekanizması yazmak, üç ayrı
yetki modeli bakımı demekti.

**Reddedilen alternatifler.**
- **Entegrasyonları zincir dışında tutmak** ("kurulmuyor ki"): müşterinin
  gözünde CA seçimi de bir tekliftir; dışarıda tutmak ikinci bir yetki
  modeli doğurur.
- **Teklif türü başına ayrı tablo:** tek uzay + tek sunum defteri; D-012
  dersi.
- **Şimdi tam uygulama:** plan arayüzü yokken davranış değiştirmeyen
  iskelet — bekleyen karar, yarım kod'dan iyidir.

---

## D-016 · Sayfanın adı Bileşenler'dir; sözlük sabitlenir; servis tanımı incelir

*23 Temmuz 2026 — 5 danışmanlı panel (pazar, TR-UX, bilgi mimarisi, şeytanın avukatı, küresel/yerelleştirme) + hakem sentezi; operatör onayı*

**Karar.**
1. **Sayfa ŞİMDİ yeniden adlandırılır:** EN **Components** · TR **Bileşenler**.
   Rota `/services` kalır. Beş danışmandan dördü bağımsız aynı sonuca vardı:
   ad bugün zaten yanlış (sektörde Services = daemon; sayfada PHP, phpMyAdmin,
   nftables var) ve değiştirme maliyeti kullanıcı/doküman biriktikçe büyür.
2. **Sözlük** (mağaza bu kelimelere doğar; şemsiye yeniden tartışılmaz):
   şemsiye **component/bileşen** · cinsler: **Service/Servis** ·
   **Runtime/Çalışma Ortamı** (rozetlerde kısa biçim: "Ortam") · **Tool/Araç** ·
   gelecekte **Module/Modül**, **Task/Görev**, **Integration/Entegrasyon** ·
   mağaza: EN **Store**, TR **Mağaza** (sayfa başlığı "Bileşen Mağazası").
   "Servis" varsayılan cins-filtresi olarak yaşar — destek dili ("servisi
   yeniden başlat") bozulmaz.
3. **Servis tanımı incelir** (operatörün "start/stop yapılan = servis" testi
   yön olarak doğru, harfiyen değil): *servis, kendi yerleşik süreci olan ve
   o sürecin çökmesi başlı başına bir olay sayılan bileşendir.* İki karşı
   örnek kataloğun kendisinden: nftables oneshot unit'iyle start/stop kabul
   eder ama yerleşik süreç bırakmaz (doğru sınıfı araç; oneshot'a
   "uygulandı/uygulanmadı" durumu yakışır, koşuyor noktası asla) ve php-fpm
   gerçek daemon'lar taşır ama runtime'dır çünkü birincil işi site-başına
   sürüm seçimi. "Denetlenebilir unit'i var" ise cins değil DİK bir
   yetenektir: başlat/durdur düğmeleri her satırda — sürüm çekmecesi dahil —
   o yetenekten çizilir.
4. **Kayda geçen karşı görüş** (küresel danışman, dürüst haliyle): hiçbir
   büyük panel nav'ında "Components" kullanmıyor (cPanel şemsiyesi
   "Software", aaPanel "App Store"); Components, ESL-ağır büyüme pazarları
   için en soyut aday ve Joomla'nın Components/Modules/Plugins üçlüsüyle
   çakışıyor. Alternatifi "Software/Yazılımlar"dı. Hakem reddetti çünkü
   "Software", entegrasyon cinsi (sunucuda yazılım olmayan Cloudflare
   bağlantısı) doğduğu gün planlı tarihte yalanlaşır — "Servisler"i bugün
   yanlış yapan hastalığın aynısı — ve satır/doküman için sayılabilir birim
   adı vermez. Bulunabilirlik ya da destek-dili sorunu ÇIKARSA geri dönüş
   adayı Software'dir; bu cümle o gün için yazıldı.

**Neden şimdi.** Kalem sayısı yalnız artacak (modül/görev/entegrasyon yolda,
mağaza yolda); yanlış adın üstüne alışkanlık ve doküman biriktirmek, düzeltme
gününü pahalılaştırmaktan başka işe yaramaz. Şeytanın avukatının kendi çöküş
koşulu da buydu: "servis olmayan satırlar çoğaldığında teslim olurum" — yol
haritası tam bunu vaat ediyor.

---

## D-015 · Eklenti pakettir, cins doğadır: mağaza hak verir kurmaz, çekirdek kataloğun sınırı para değil sorumluluktur, cins listesi açıktır

*23 Temmuz 2026*

**Karar.** Operatörün araba benzetmesi (standart/opsiyonel donanım, motor değişimi)
ve getirdiği danışman analizi (Plesk/cPanel eklenti dökümü) üzerine altı bağlı karar:

1. **İki eksen kesindir ve karışmaz.** *Kalem/eklenti* = edinme-dağıtım birimi
   (ücretsiz/paralı, 1./3. taraf — D-012'nin ekseni). *Cins* = kurulan şeyin
   mekanik doğası (service/runtime/tool — D-010'un ekseni). Danışmanın özeti
   modelin kendisidir: *"eklentiler çoğu zaman servisleri yönetir; kendileri
   servis değildir."* Bir eklenti 0..N parça getirebilir (Plesk Email Security
   tek eklenti → postfix+dovecot+clamav yönetir); her parça KENDİ cinsiyle
   görünür, eklenti kendisi bir cins değildir.
2. **Mağaza hak verir, kurmaz.** Satın alma / lisans anahtarı / koltuk havuzu
   mağazada; edinilen kalem Servisler kataloğunda belirir ve oradan kurulur.
   Kurulumun tek adresi katalogdur — ikinci Kur düğmesi, B3b'de silinen
   AdminNodeInstall'ın (iki-adres hatası) tekrarı olurdu.
3. **Çekirdek katalog ile mağazanın sınırı PARA değil SORUMLULUKTUR.**
   Çekirdek katalog, panelin garanti verdiği küçük küratörlü kümedir (iki
   dağıtımda paket adları, güvenlik duvarı portları, kaldırma, test —
   hepsi bizim borcumuz). Nadir kullanılan ÜCRETSİZ kalemler (örn. niş bir
   veritabanı) çekirdeğe şişkinlik ve veremeyeceğimiz garanti demektir —
   onlar da mağazadan gelir. (Bu, 22 Tem'deki "ücretsiz kalem mağazaya
   girmez" ifadesinin düzeltmesidir: ölçüt fiyat değil, garanti sınırı.)
4. **Cins listesi açıktır ama cins, İLK kalemiyle doğar.** Bugün üç cins üç
   gerçek kalem ailesini taşıyor. Danışman dökümündeki kalan biçimlerin
   haritası hazırdır ve günü gelince açık cins olarak eklenir:
   *modül* (host servisin içinde yaşar, kendi unit'i yok — ModSecurity,
   Brotli; "hangi servisin içinde" alanı gerekir) · *görev/timer* (çalışır ve
   biter — yedekleme, sertifika yenileme) · *entegrasyon* (sunucuya hiçbir
   şey kurulmaz; durumu "bağlı mı", sırrı mühürlü API anahtarı — Cloudflare,
   S3 yedek hedefi, UptimeRobot). *Agent* ayrı cins DEĞİLDİR: mekanikçe
   servistir (Imunify/Acronis agent'ı başlatılır/durdurulur); farkı ticari.
5. **Derlenen dil için kurulacak şey yoktur.** Go/Rust "desteği" katalog
   kalemi olamaz çünkü çoğalma testi (D-011) N der: runtime, müşterinin
   binary'sinin İÇİNDE gelir. Uygulama modu (ters vekil + unit) herhangi bir
   binary'yi bugün zaten çalıştırır — Go/Rust desteği fiilen vardır, satır
   gerekmez. Yorumlanan diller (Python, Ruby) Node ile aynı sınıftır: günü
   gelince runtime kalemi olurlar. (İleride "Git'ten çek + sunucuda derle"
   yapılırsa derleyici zinciri bir *tool* kalemi olur — o gün gelirse.)
6. **İki sayfa iki sorudur.** Servisler (adı ileride "Bileşenler"
   olabilir) = "sunucumda ne var, nasıl işliyor?" Mağaza = "ne edinebilirim,
   kimden, hangi hakla?" Aynı ürün ikisinde de görünebilir; çelişki değil,
   iki farklı an.

**Neden.** Operatör üç somut soruyla modeli zorladı (nadir-ücretsiz kalem
nereye; Go/Rust ne olacak; danışman dökümü 7 çalışma biçimi sayıyor) ve model
üçüne de mevcut eksenlerle cevap verdi — yalnız çekirdek/mağaza sınırının
gerekçesi yanlış ifade edilmişti (fiyat değil sorumluluk). Danışmanın
"gerçek sunucuda kontrol" bölümü (list-unit-files, ss -lntup) taramanın
bugün yaptığının aynısıdır — dış doğrulama olarak kayda geçer.

**Ek (24 Tem — operatörün sınır bekçiliği üzerine): çekirdeğin ölçüsü.**
Operatör sordu: "hayati servisler dışındaki her şey önce mağazada durup
istenince kataloğa geçmeyecek miydi? Yüzlerce program Components'e girerse
kargaşa olmaz mı?" Doğru hatırlıyor ve sınır şimdi sayıya bağlanıyor:
- **Çekirdek = rol başına varsayılan + 1-2 adlandırılmış alternatif.**
  (web: nginx+Apache · DNS: PowerDNS+BIND · SMTP: Postfix+Exim · spam:
  SpamAssassin+Rspamd · webmail: Roundcube …) Hedef ONLARCA kalem; asla
  yüzlerce. 23-24 Tem eklemeleri (rspamd, exim) bu sınıfa girer — uzun
  kuyruk değil, koltuk üyesi.
- **Uzun kuyruk mağazada yaşar** (ücretsizse tek tık "ekle" ile edinilir ve
  kataloğda belirir — 2. madde zaten böyle diyordu; sayı disiplini artık
  açık). SOGo gibi büyük grupware çekirdeğe GİRMEZ, mağazayı bekler; netdata
  sınırdadır (izleme rolünü panelin kendi sayfası karşılıyor) ve mağaza
  doğduğunda oraya taşınmaya adaydır.
- Kargaşanın bugünkü freni de kayıtta kalsın: varsayılan görünüm yalnız
  kuruluları gösterir (D-010, kurulu-önce); katalog düğmenin arkasındadır.
  Ama ölçek sigortası görünüm değil, mağaza kapısıdır.

**Reddedilen alternatifler.**
- **Danışmanın 7 biçimini bugün 7 cins yapmak:** elimizde modül, timer ve
  entegrasyon KALEMI yokken şema spekülatif olurdu; cins ilk kalemiyle
  doğar, yoksa D-010'un çözdüğü sorunun tersine döner (boş kategoriler).
- **Go/Rust için "destek satırı":** kavramsal hata — D-011'in "Go satırı"
  reddiyle aynı; kurulacak şey olmayan yerde satır yalan söyler.
- **Sınırı ücretsiz/ücretli çizmek:** nadir ücretsiz kalem çekirdekte
  garanti yükü, mağazada doğal dağıtım bulur; para yanlış eksendir.
- **Cins başına ayrı sayfa:** "kurulu olan tek listede" ilkesini yedi
  sayfaya bölerdi; cins satırın çizimini değiştirir, adresini değil.

---

## D-014 · Yetki zinciri daralan bir süzgeçtir: admin kurar, bayi sunar, müşteri kullanır — birim sürümdür

*21 Temmuz 2026*

**Karar.** Bir yeteneğin kime ulaşacağı üç ayrı kararla belirlenir ve her katman
yalnızca **daraltır**, asla genişletemez:

    kurulu (admin)  ⊇  sunulan (bayi)  ⊇  kullanılan (müşteri)

- **Admin** yalnız şuna karar verir: bu servis/eklenti **sunucuda var mı**.
  Kurmuş olması kimseye verdiği anlamına gelmez.
- **Bayi** yalnız şuna karar verir: adminin kurduklarından hangilerini
  **müşterilerime sunuyorum**.
- **Müşteri** yalnız şuna karar verir: bayinin sunduklarından hangilerini
  **kullanıyorum** — ve bu karar site başınadır, hesap başına değil.

**Zincirin birimi servis değil, sürümdür.** "PHP sunuyorum" bir şey ifade etmez;
bayi **PHP 8.3'ü** sunar ya da sunmaz, müşteri site başına sürüm seçer. Bu,
D-010 ile çelişmez, ona ikinci bir eksen ekler: katalogda **görüntü** birimi
runtime'dır (tek satır, sürümler çekmecede), **hak** birimi sürümün kendisidir.

**Ayarlar da aynı zincirden geçer**, yalnız varlık değil: admin sunucu
varsayılanını ve tavanını koyar, bayi hangi düğmeye dokunulabileceğine karar
verir, müşteri o sınırlar içinde ayarlar.

**Neden.** Operatörün somut vakası: admin yalnız PHP 8.4 kurdu; site sahibinin
8.3'e ihtiyacı olabilir ve 8.4'ü hiç kullanmayacak. Tek bir "kurulu mu" bayrağı
bu üç farklı soruyu aynı anda cevaplayamaz. Bugün kodda tek bağış katmanı var
(`hasEntitlement`: abonelik ürünü tutuyor mu); admin→bayi ile bayi→müşteri
ayrımı yok, yani bayinin "sunmuyorum" deme hakkı temsil edilemiyor.

**Katmanların kodda yeri** (üçünden ikisi hazır):
- Admin katmanı = kurulu set (`InstalledServiceIDs` + katalog). ✅ var
- **Bayi katmanı = servis planları** (`plan_handlers.go`, `004_accounts.sql`).
  Yeni bir yapı gerekmez: "sunuyorum" kararı planın içeriğidir — *"bu plan PHP
  8.2 ve 8.3 içerir, 8.4 içermez"*. Plesk de aynısını yapar (Service Plans).
  ⬜ eksik halka budur
- Müşteri katmanı = site başına seçim (`site.php_version`, `CreatePool(siteID,
  username, phpVersion)`). ✅ var

**Sonuçlar.**
- **Kaldırma zincir boyu bir olaydır.** Admin bir sürümü silmek isterse, onu
  sunan planlar ve kullanan siteler engeller. D-010'daki `RUNTIME_IN_USE` reddi
  "kullanımda" demekle yetinmemeli, *kimin* engellediğini saymalı (kaç plan, kaç
  site) — aksi hâlde admin neyi kırdığını göremeden karar verir.
- **Sunucu-geneli PHP ayarı bu modelle bağdaşmaz.** Bugünkü PHP-FPM sayfası
  uzantı anahtarlarını ve php.ini değerlerini sunucu genelinde tutuyor:
  `disable_functions` bir güvenlik denetimidir, bir müşterinin ihtiyacı herkesin
  politikası olur; `memory_limit` sunucu genelindeyse tek bir ağır site herkesin
  tavanını belirler; `ffi`yi bir müşteri isterken diğerinin istememesi mümkün
  değildir. Sayfa kendini **sunucu varsayılanı** olarak adlandırmalı ve site
  altlığı ayrıca var olmalıdır. Altyapı hazır: her site zaten kendi FPM havuzunu
  alıyor, havuz kendi `php_admin_value` yönergelerini taşıyabilir.

**Önkoşul (dürüstlük notu).** Bu model PHP için şu an **boştur**: Sury deposu
olmadığı için admin ikinci bir sürümü isteseydi bile kuramaz (bkz. D-002
düzeltmesi, D-010). Verecek tek şey varken "kimin neyi sunacağı" tartışması
teoriktir. Bu yüzden B3c (Sury + çoklu sürüm) sıranın önüne alınır — süs değil,
zinciri anlamlı kılan önkoşuldur.

**Reddedilen alternatifler.**
- **Bayi katmanı için ayrı bir "teklif" tablosu**: planlar zaten tam olarak bu
  soruyu cevaplıyor; ikinci bir yapı, aynı bilginin iki sahibi olurdu (B3'ün
  kapatmaya çalıştığı hatanın aynısı).
- **Adminin kurduğu her şeyin otomatik sunulması**: en az koddur ve yanlıştır —
  bayinin sadeleştirme hakkını yok eder (bir bayi bilerek yalnız 8.3 sunmak,
  destek yükünü azaltmak isteyebilir).
- **Hak birimini servis düzeyinde tutmak** ("PHP hakkı var/yok"): operatörün
  vakasını hiç çözmez; 8.4 kurulu diye 8.3 gerektiren müşteriye "PHP'n var"
  demek olurdu.
- **Sunucu-geneli ayarları yeterli saymak**: çok kiracılı bir panelde bir
  müşterinin `disable_functions` ihtiyacını herkese dayatmak demektir.

---

## D-013 · Alan adı oluşturmak runtime seçmek değildir: PHP bir site anahtarıdır, kurulum kararı değil

*21 Temmuz 2026*

**Durum: UYGULANDI (24 Tem).** Ekran artık yalnız amacı soruyor (Web sitesi /
Yalnız DNS); PHP sürüm seçici oluşturmadan tamamen kalktı ve web sitesi için
PHP ön koşul olmaktan çıktı — PHP'siz sunucu da site barındırabiliyor. Kabloda
proje tipi "static" gider (PHP'nin kapalı hâli); sunucu sözleşmesi değişmedi.
Canlı doğrulama: Boston'da site oluşturuldu ve anında sunuldu (HTTP 200).

**Karar.** "Alan adı ekle" ekranı **"ne çalışacak?"** sorusunu sormayı bırakır.
Sorduğu tek şey amaçtır: **web sitesi · yalnız posta · yalnız DNS**. Runtime
(PHP açık/kapalı + sürüm, Node/proxy modu, port, başlangıç dosyası) oluşturmadan
sonra **sitenin ayarı** olur — açılıp kapanabilen, geri alınabilir bir özellik.

Bunun doğrudan sonucu: **`static` ile `php` ayrı proje tipi değildir.** İkisi
aynı şeyin PHP anahtarı kapalı ve açık hâlidir; şablon farkı üç satırdır
(`index.php` dizin sayılır mı · eşleşmeyen URL `/index.php`'ye mi düşer yoksa
404 mü · `.php` fastcgi'ye gider mi). `node`/`proxy` ise gerçekten ayrı bir
moddur — doküman kökü sunulmaz, her şey ters vekile gider.

**PHP-FPM isteğe bağlı kalır** (operatör kararı, 21 Tem). Anayasanın "kurulum
anında hiçbir şey kurulmaz, kurulu olmayan görünmez" maddesi PHP için de
geçerlidir: yalnız-DNS ya da yalnız-Node sunan bir makine PHP taşımaz. PHP
kurulu değilse site ayarındaki anahtar bunu dürüstçe söyler ve yöneticiye kurma
yolunu gösterir — ama bunu **oluşturma ekranında** değil, kendi yerinde yapar.

**Neden.** Operatörün sorusu şuydu: "PHP kullanmayacak birine neden PHP
zorluyoruz, gereksiz ve kafa karıştırıcı değil mi? Herkes WordPress kurmak
zorunda değil." Haklıydı ve teşhis şuydu: ekran, kurulu olmayan bir yeteneği
turuncu bir çağrıyla reklam ediyordu — Servisler sayfasında daha yeni
uyguladığımız "kurulu olmayan görünmez" kuralının tam tersi. Statik site kuracak
birine sunucusu eksikmiş gibi hissettiriyordu; sunucu eksik değildi, o kişi PHP
istemiyordu.

Plesk'in gerçek akışı incelendi (operatörün 9 ekran görüntüsü, canlı kurulum) ve
şu çıktı: **Plesk'in oluşturma diyaloğu bir runtime seçici değildir.** İlk ekran
"içerik buraya nasıl gelecek" diye sorar (boş sayfa · dosya yükle · WordPress ·
Git'ten çek · Node.js · yalnız posta). İkinci ekranda runtime hiç sorulmaz.
Üçüncü ekranda "Configuring PHP" **koşulsuz** bir adımdır. PHP sonradan sitenin
ayar sayfasında bir onay kutusudur (sürüm listesi + site başına php.ini).
Kritik ayrıntı: **"Node.js" seçeneği oluşturmayı hiç değiştirmez** — form "Blank
website" ile birebir aynıdır ve oluşturma bitince Node **hâlâ etkin değildir**,
"Enable Node.js" düğmesine basmak gerekir. Yani Plesk'te bile o ilk ekran bir
yönlendirme menüsüdür; gerçek karar sitenin ayarında verilir.

**Plesk'ten şekli alıyoruz, paketlemeyi almıyoruz.** Plesk'te soru hiç doğmaz
çünkü PHP'yi kendisi getirir (`/opt/plesk/php/8.3`); PHP asla "kurulu değil"
olamaz. Bizde olabilir. Bu yüzden "her sitede PHP varsayılan açık" doğrudan
kopyalanamaz — kopyalansaydı D-003 ve anayasa maddesi çiğnenirdi.

**Yan bulgu (kapatıldı).** Bu tartışma gerçek bir açığı ortaya çıkardı: statik
vhost'ta PHP işleyicisi yoktu ama `.php` için ret kuralı da yoktu, bu yüzden
nginx dosyayı `application/octet-stream` olarak veriyor ve tarayıcı **kaynak
kodu indiriyordu**. A6 ailesi. Canlı nginx'te ölçülüp kapatıldı (`b1a6aac`).
Ders kayda geçsin: *"burada X çalışmaz", "burada X'in kaynağı yayınlanır"
demek olmamalı.*

**Reddedilen alternatifler.**
- **PHP-FPM temel kurulumun parçası olsun** (Plesk modeli): soruyu kökten
  bitirirdi ve pazar beklentisiyle birebir örtüşürdü, ama "kurulu olmayan
  görünmez" ilkesinden PHP için feragat demekti. Operatör isteğe bağlıyı seçti.
- **Kurulum sırasında operatöre sorulsun** ("bu sunucu PHP sunacak mı?"):
  makine başına tek karar cazipti, ama install.sh'a kalıcı bir dallanma ekler ve
  sonradan fikir değiştireni yarı-yolda bırakır.
- **Dile göre seçenek çoğaltmak** ("Node.js sitesi", "Python sitesi", "Go
  sitesi"): nginx'in ve panelin gözünde üçü de aynı şeydir —
  `127.0.0.1:PORT`'a ters vekil + bir unit. D-010'un "Go satırı kavramsal hata
  olurdu" gerekçesinin aynısı. Uygulama modu **tek** seçenektir, runtime onun
  içinde bir alandır.
- **Oluşturma ekranındaki turuncu uyarıyı yumuşatmak/kısaltmak**: semptomu
  tedavi ederdi. Sorun uyarının tonu değil, sorunun oraya ait olmamasıydı.
- **Uyuşmazlık tespitini tek başına yeterli saymak** (statik sitede `.php`
  görünce uyar): iyi bir özellik ve yapılacak, ama yanlış soruyu sormaya devam
  edip arkasından temizlik yapmak olurdu. Önce soru düzelir, tespit emniyet
  ağıdır.

**Uygulama sırası.** (1) ✅ statik `.php` sızıntısı kapandı — güvenlik, tasarım
tartışmasından bağımsız · (2) Add Domain "ne için?" sorusuna iner, turuncu
dayatma kalkar, metinler markadan davranışa çevrilir ("WordPress, Laravel…"
gider) · (3) site ayarında PHP anahtarı + sürüm; `static`/`php` tek moda birleşir
· (4) uyuşmazlık tespiti + tek tıkla düzelt · (5) Uygulama modu (Node/proxy)
oluşturma sonrası ayarda görünür hâle gelir — arka uç (`hosting_handlers.go`,
şablonun vekil dalı, `HostingTypePanel`) zaten hazırdır, eksik olan tek dil.

---

## D-012 · Lisanslı ürünler ve satış zinciri: hak bir havuzdur, uyum satıcıyla operatör arasındadır

*20 Temmuz 2026*

**Karar.** Operatörün modeli ("her şey bir kalem; bazıları bedava bazıları paralı;
admin satın alır, bayiye ya da doğrudan müşteriye satar") şu yapıyla karşılanır —
ve **üçüncü-taraf ticari ürünler baştan kapsam içindedir** (operatör kararı):

1. **"Şey" ile "kullanma hakkı" ayrılır.** Ürünün kendisi sunucuya bir kez kurulur
   (çoğalma testi = 1 → katalog kalemi, D-011). **Hak** ise abonelik başınadır ve
   ağaçta aşağı akar. Bu ikisi bağımsız eksenlerdir: kurulum ikili bir durumdur,
   hak sayılabilir bir kaynaktır.
2. **Hak, disk/domain gibi bir havuzdur.** Yeni mekanizma yok: v0.3'ün
   `reseller_pools` deseni ürünlere genişler. Admin'in kontenjanı → bayiye tahsis →
   müşteriye tahsis; aşımda kodlu 409 + kalan-havuz. Admin **doğrudan müşteriye** de
   satabilir (abonelik sahiplik ağacı bunu zaten taşır).
3. **Lisans modeli ürün tanımına girer:** `license_model ∈ {server, seat}`.
   *server* → tek fiyat, sınırsız kullanıcı; admin havuzu sonsuz, satış saf marj.
   *seat* → admin satıcıya adet başına öder; **fazla tahsis gerçek paradır ve lisans
   ihlalidir**, bu yüzden havuz sertçe uygulanır (uyarı değil, ret).
4. **`seat_unit` zorunludur** (`mailbox | site | subscription | server`). Satıcılar
   farklı şey sayar; birimi yazmazsak yanlış şeyi sayarız ve panelin sayısı
   satıcının faturasını tutmaz — bu, en sinsi tuzaktır.
5. **Lisans anahtarı sır olarak saklanır** — A4'ün `enc:v1` mekanizması (mühürle,
   kullanım anında çöz). Ticari ürünlerin çoğu kurulumda anahtar ister; anahtarsız
   kurulum yarım kurulumdur.
6. **Fiyat tek sayı değildir.** Zincirde üç fiyat vardır: satıcı→admin (maliyet),
   admin→bayi, bayi→müşteri. Bugünkü tek `MonthlyPriceCents` alanı yetmez; desen
   v0.3'teki "bayiye ait plan" (`service_plans.owner_id`) ile aynıdır.
7. **Görünürlük hakkı izler.** Bayi ürünü almadıysa müşterileri onu **hiç görmez**
   ("kurulu olmayan görünmez"in haklara uygulanmış hâli). İnce nokta: bayi isterse
   "satın al" tanıtımını açabilir — görünürlük bayinin kararıdır, varsayılan gizli.
8. **Kurulu olmayan ürün satılamaz.** Sunucuda kurulu olmayan bir ürüne hak satmak
   müşteriye hiçbir şey satmaktır → kodlu ret.
9. **Geri alma, abonelik askısıyla AYNI kuraldır.** Bayi ödemeyi kestiğinde:
   ağacında yeni tahsis 403, mevcut kullanım grace sonuna dek yaşar, veri silinmez.
   İki farklı "kesildi" davranışı olmaz.

**Neden bu yük kabul edildi.** Zinciri sonradan üçüncü-tarafa açmak, tahsis ve
fiyat modelini yeniden yazmak demekti; `license_model`/`seat_unit` alanlarını
baştan koymanın maliyeti iki alandır. Operatör bilinçli olarak zor yolu seçti.

**DÜRÜSTLÜK SINIRI — panel neyi garanti ETMEZ.** Panel yalnız **kendi tahsis
kayıtlarını** uygular; satıcının lisans şartlarına uyumu **garanti etmez**:
- Satıcı farklı sayabilir (ör. kutu yerine alan adı); panel mutabakat ekranı
  gösterir ("satıcı: 50 · tahsis: 47 · boşta: 3") ama **fatura satıcının
  gerçeğidir**, panelin değil.
- **Üçüncü-taraf ürünü alt-lisanslama (bayiye/müşteriye satma) hakkı operatörle
  satıcı arasındadır**; çoğu satıcı bunu ortaklık sözleşmesine bağlar. Panel bu
  hakkı doğrulayamaz ve doğruladığını iddia etmez. Ekranda ve dokümanda açıkça
  yazar — aksi hâlde kullanıcıyı lisans ihlaline teşvik eden bir özellik yapmış
  oluruz.

**Reddedilen alternatifler.** Sessiz fazla-tahsiz (uyarıp geçmek — *seat* modelinde
operatöre borç yazdırır) · satıcı başına API entegrasyonunu çekirdeğe koymak (her
satıcı ayrı entegrasyon, dış bağımlılık yasağına ve sadeliğe aykırı; başlangıç
**elle anahtar + elle kontenjan**, otomasyon ancak tek bir satıcıda gerçek talep
doğarsa) · hakları plana gömmek (plan değişince hak kaybı sessiz olur; hak ayrı
kayıttır, plan yalnız varsayılan verir) · üçüncü-tarafı sonraya bırakmak (operatör
reddetti: zincir sonradan açılırsa tahsis modeli yeniden yazılır).

---

## D-011 · Framework servis değildir: çoğalma testi, preset bütçesi ve sürüm sahipliğinin sınırı

*20 Temmuz 2026*

**Karar.** Dört bağlı karar, tek kayıtta:

1. **Sınır kuralı sayısallaşır — çoğalma testi.** Bir şeyin katalog kalemi mi
   site-içi mi olduğunu tek soru belirler: *sunucuda N site açıldığında bu şeyin
   diskteki örnek sayısı 1'de mi kalır, N'e mi çıkar?* 1 → katalog kalemi (sürümü
   dağıtım/vendor deposu yönetir); N → site-içi (sürümü müşterinin lock dosyası ya
   da uygulamanın kendi güncelleyicisi yönetir). **Katalog = L1 (servisler) + L2
   (çok-sürümlü runtime'lar), başka hiçbir şey.** Laravel, Symfony, Django,
   Next.js katalogda **yoktur ve olmayacaktır**; Composer, Node ve certbot ise
   katalog kalemidir (tek örnek). Sınıfı yazılım değil **konuşlandırma biçimi**
   belirler: aynı phpMyAdmin her müşterinin docroot'una kopyalansaydı N olur ve
   katalogdan düşerdi. Test tartışmaya kapalıdır çünkü sayısaldır.

2. **Panel, N örnekli hiçbir şeyin sürümünü sahiplenmez.** Panel bir güncelleyici
   **yazmaz**; uygulamanın kendi güncelleyicisini site kullanıcısı kimliğiyle
   **tetikler**. Sürüm sabitlemez, dosya yamalamaz, `DISALLOW_FILE_MODS` yazmaz,
   "desteklenen sürüm" göstermez. Bu, yol haritasındaki "WordPress Toolkit
   derinliği (güncelleme, sertleştirme)" maddesini bağlayıcı olarak sınırlar:
   sertleştirme presetlerinden otomatik güncellemeyi kapatan hiçbir madde
   varsayılan açık olamaz.

3. **Framework desteği = site ilkelleri (L3) + preset; asla tip, asla kurucu.**
   Panelin Laravel'e dair sahip olabileceği tek şey bir *varsayılanlar seti*dir
   (docroot `public` önerisi, cron satırı metni, queue komut derleyicisi) — bir
   kurucu değil. Provizyonda tek bir `if type == laravel` dalı yoktur.

4. **`appCatalog` bir tip ekseni değil, geri alınabilir kurulum eylemidir.** Site
   tipi PHP'dir; WordPress domain oluşturma akışında bir seçenek **değildir**,
   kurulduktan sonra çalıştırılan ve **kaldırılabilen** bir eylemdir.

**Neden.** Operatörün sorusu: "Laravel servis mi, katalogda mı durmalı — ve bir
kez açarsak bu böyle gider mi?" Beş rakip panelin gerçek kaydı incelendi:

- **Kaymanın motoru katalog değil, preset'tir.** Katalog girişi pahalıdır (paket
  adı, iki dağıtım ailesi, kaldırma yolu, init adımı); preset'in maliyeti üç
  dizedir ve reddetmenin argümanı yoktur. "Symfony bedava gelir" cümlesi Django,
  Ghost, Strapi için de kurulur; sekizinci preset'te ekran bir framework
  seçicisidir. **cPAddons da "tek tık kurulum" değil, "yalnızca yapılandırma
  reçetesi" olarak başladı.**
- **Kaymanın ikinci motoru sürüm sahipliğidir.** cPAddons'ı öldüren kurulum
  değildi: N örnekli WordPress'in sürümünü panel üstlendi, `update.php`'yi
  yamaladı, kullanıcılar WP 3.9'da çakılı kaldı. Aynı desen aaPanel'de (Laravel
  5.4/PHP 7.4'te donmuş one-click; kendi personeli "ürettiğimiz dosyaları silin"
  diyor) ve Plesk'te (iskelet üreteci upstream değişiminde kırıldı; APS kataloğu
  18.0.77'de **tamamen kaldırıldı**) tekrarlandı. Sektör bu modelden geri çekildi.

**Kuralı ihlal etmeyi pahalı kılan üç mekanizma** (kural yalnız Markdown'da
yaşarsa iki yıl dayanmaz — bu karar D-003/D-010'un aksine kodda zorlayıcı ister):

- **Yapısal saflık şartı (preset testi).** Bir preset ancak *zaten var olan
  jenerik alanların ön-doldurulmuş değerleri* olarak ifade edilebiliyorsa kabul
  edilir. Tek bir yeni alan ya da tek bir `if framework ==` dalı gerekiyorsa
  preset **reddedilir**. Preset'ler salt-veri tek dosyada yaşar; UI'da seçilebilir
  bir "tip" değil, formun üstündeki "formu doldur" düğmesidir. Preset sayısı 3'ü
  aşarsa bu bir kod değişikliği değil **strateji değişikliğidir**.
- **Framework adı yasağı + CI kapısı.** Framework adı hiçbir enum sabitinde, DB
  kolonunda, API alan değerinde veya systemd unit adında geçemez; yalnız i18n
  dizelerinde geçer. Docroot açılır kutusu framework adı değil **yol değeri**
  listeler (`(kök) | public | public_html`). `validProjectTypes`
  (php/static/node/proxy/forwarding) *taşıma biçimini* tanımlar, uygulamayı değil —
  **kilitli sayılır**. CI'da Go kaynaklarında `laravel|symfony|django|nextjs|ghost`
  grep'i sıfır eşleşme vermeli (i18n hariç).
- **Girişi olan her kalemin çıkışı baştan yazılır.** `appCatalog` girişleri zorunlu
  karar-kaydı ID'si + bakımcı + kaynak sağlaması taşır — "üç satırda giriş eklemek"
  imkânsızlaşır. Kabul kararına girişi kaldıran yol ve "kurulmuş siteler ne olur"
  cevabı (her zaman: **dosyalar müşterinindir, panel yalnız düğmeyi kaldırır**)
  birlikte yazılır.

**Ticari basınç da kapatılır.** `app_installer` girişi bugün "One-click installs
for WordPress **and other apps**" diyor — tek girişli bir listeyi çoğul bir plan
özelliği olarak satıyor. İki zararı var: satış tarafı doldurulacak boş bir kova
görür (kaymanın en güçlü motoru mühendislik değil **satılmış bir vaattir**), ve
liste plan özelliğine bağlandığı an giriş silmek sözleşme ihlaline döner. Ürün
gerçekliğe indirilir: "WordPress tek-tık kurulumu"; çoğul ifade kaldırılır.

**Konumlandırma (satış ve ürün aynı cümleyi kullanır).**
*"CelikPanel uygulamanızı kurmaz; uygulamanızın altındaki sunucuyu yönetir.
Kodunuzun sürümü sizindir — ve bunu bir sınır olarak değil, bir garanti olarak
veriyoruz."* Teknik alıcı için: *"Panelin sahiplendiği her şeyin sunucuda tek bir
örneği vardır; sitenizde N kopya halinde duran hiçbir şeyin sürümüne karışmayız."*
Rakibin veremeyeceği bir garantidir. **Not:** "bizde de tek tık var, adı
`composer create-project`" cümlesi ancak site-kullanıcısı kimliğiyle çalışan komut
ucu **yayınlandıktan sonra** kullanılabilir; bugün yoktur.

**Asıl iş katalogda değil.** Bu karar neyi yapmayacağımızı sabitler; boşluk
L3'tedir — bugün Laravel'i katalogsuz da barındıramıyoruz:
- docroot `site_orchestrator.go`'da `public_html`'e sabit (Laravel `public/` ister)
- ~~vhost `.env`'i düz metin sunuyor~~ → **kapatıldı** (commit `0088c67`, aynı gün;
  ayrıca statik sitelerin ACME kırığı da bu düzeltmeyle giderildi)
- `composer`/`artisan` kod tabanında sıfır kez geçiyor (site kullanıcısı kimliğiyle
  komut çalıştıran uç yok)
- queue worker üç bağımsız engelle imkânsız (`RunAsUser: "www-data"` sabiti,
  `req.Port <= 0` reddi, `project_type == "node"` kilidi)

**Reddedilen alternatifler.** "Laravel" proje tipi (enum bir kez framework adı
taşıdığında katalog örtük doğar ve DB'de kalıcılaştığı için geri alınamaz) ·
panelin ürettiği iskelet/tarball (aaPanel donmuş sürümde, Plesk upstream
kırılmasında) · Softaculous tarzı "Frameworks" kategorisi (15 giriş: Bootstrap bir
CSS framework'ü, Kohana 2016'dan beri ölü, Symfony 2.3.42 EOL 2017 — hâlâ
kurulabiliyor) · `.env`'i alan alan şemalaştırmak (çok satırlı değerlerde
müşterinin dosyasını sessizce tahrip eder; Forge da Ploi de opak blob tutar —
meşru "akıllılık" diskteki gerçekliğe bakmaktır: `.env.example` varsa kopyala) ·
scheduler'ı birinci sınıf kavram yapmak (sıradan bir crontab satırıdır) ·
preset'i otomatik uygulamak (onay kullanıcıdadır; yoksa hayalet cron/unit doğar).

---

## D-010 · Katalogda tür ayrımı (servis/runtime/araç) + "kurulu-önce" varsayılan; PHP çoklu-sürüm Sury ile gerçek olur

*20 Temmuz 2026*

**Karar.** Üç bağlı karar, tek kayıtta:

1. **Katalog tek kalır, ama `ManagedService`'e `Kind` girer:** `service` (systemd
   daemon'ı), `runtime` (sürümlü yorumlayıcı), `tool` (daemon'suz web aracı).
   PHP-FPM ve Node.js aynı türün iki üyesi olur; phpMyAdmin/phpPgAdmin "servis"
   olmaktan çıkar. Satır çizimi `Kind`'e dallanır ve bugünkü
   `Daemonless = len(SystemNames)==0` sezgisi silinir — o bayrak bugün üç ayrı
   şeyi işaretliyor. **Sürümler satır değil, satırın içindedir** (sürüm çekmecesi).
   Ayrı `/runtimes` ya da `/apps` sayfası **açılmaz**.
2. **Servisler sayfası "kurulu-önce" olur:** "kurulu olmayanı gizle" varsayılan
   AÇIK. Katalog = aynı listenin süzgeci kapalı hâli (ikinci ekran, ikinci arama
   kutusu yok). *Uygulama düzeltmesi (20 Tem, aynı gün): katlama sabit varsayılan
   değil, görünümü izler* — kurulu görünümde kategoriler AÇIK (liste zaten kısa;
   katlamak 3 servisi görmek için 3 tık demekti), katalog açılınca KATLI (liste
   uzun, yönetilmek ister). ✅ uygulandı: commit `312e378`.
3. **PHP çoklu-sürüm Sury vendor deposuyla gerçek olur** (D-007'nin PHP'ye
   uygulanması): Debian/Ubuntu'da yan yana `php8.x-fpm` paketleri panelden
   kurulabilir. Arch'ta temiz yol yoktur (yalnız AUR) — orada dürüstçe "tek
   dağıtım sürümü" denir (D-004: apt birinci sınıf, pacman geliştirme-test).

**Neden.** Operatörün sorusu şuydu: "Node.js/Go gibi seçenekler yok; buraya mı
eklenmeli, sayfa çok uzamaz mı?" Beş rakip panelin gerçek dokümantasyonu
incelendi ve iki şey çıktı:

- **Sayfa uzunluğu bir taksonomi sorunu değil, varsayılan sorunudur.** Sayfa
  uzunluğu katalog boyutuyla değil **kurulu kalem sayısıyla** ölçülmeli; süzgeç
  varsayılan açık olunca temiz sunucuda ekran 3-4 satırdır, katalog 19 da olsa
  40 da olsa. Bu, anayasanın "kurulu olmayan servis arayüzde görünmezdir"
  maddesini bir onay kutusu olmaktan çıkarıp ürün mekaniğine çevirir.
- **Liste patlamasının kaynağı sürümleri satır yapmaktır.** Plesk'in "PHP
  interpreter versions (2 of 12 selected)" ağacı bunun kanıtı. Dil başına tek
  satır + içinde sürüm yöneticisi bu patlamayı kaynağında keser.

Kaçındığımız somut hatalar: **cPanel**'in Service Manager / Application Manager
ikiliği (kullanıcı hangi ekranın hangi soruyu cevapladığını ezberler);
**Plesk**'in tutarsız runtime modeli (Node Linux'ta eklenti, Windows'ta bileşen;
Ruby CLI'da; Python onay kutusu); **HestiaCP**'nin 5050 numaralı hatası (N sürüm
satırının "Edit"i sürümsüz tek sayfaya gider — sürüm uçlarımız parametreli olur);
**aaPanel**'in PM2 çiftlemesi (tek yetenek, iki giriş noktası — bizdeki karşılığı
`AdminNodeInstall`'dır, silinir: runtime kurulumunun tek adresi Servisler'dir).

**Go/Java runtime olarak hedef-dışıdır.** Go derlenip tek binary olur; kurulacak
runtime yoktur, doğru destek zaten `proxy` proje tipi + systemd unit'tir.
Katalogda "Go" satırı kavramsal hata olurdu. Java/Tomcat ayrıca WAR dağıtım
modeliyle panelin geri kalanına uymaz. Ruby, PM2, Supervisor, Docker,
Elasticsearch, MongoDB/RabbitMQ/Varnish de hedef-dışı kalır.

**Bağlı üç alt karar** (aynı deseni 5 dilde tekrarlayacağımız için burada sabitlenir):
- **"Sistem yorumlayıcısı" kaçağı kaldırılır:** panel yalnız kendi kurduğu
  runtime'la çalışır (PHP'deki disiplinin aynısı). PATH'teki `node`u kurmadan,
  güncellemeden, kaldıramadan çalıştırmayı kabul etmek görünmez bağımlılık üretir.
- **Web sunucusuz Node projesi reddedilir** (php/static ile simetrik, B1 hata
  sözleşmesiyle kodlu ret + eylem düğmesi): bugün unit kalkıyor ama reverse proxy
  olmadığı için alan adı açılmıyor — kullanıcı yeşil durumla bozuk siteyi aynı
  anda görüyor.
- **Kullanımdaki sürüm/servis kaldırılamaz:** kodlu ret + engelleyen site listesi
  (`RUNTIME_IN_USE`, `SERVICE_HAS_DEPENDENTS`). Toplu sürüm taşıma ayrı iştir (v0.3).

**D-002 düzeltmesi.** D-002 "PHP — yan yana paketler, site başına seçilir ✅
yapıldı" diyordu; kod bunu yalnız yarısıyla karşılıyor: **tespit ve site başına
seçim yapıldı, çoklu sürüm KURULUMU yapılmadı** — `php-fpm` katalog girdisinde
`Repo` yok, kod tabanında `sury`/`ondrej` geçen tek satır yok. Yani panel çoklu-PHP
arayüzü taşıyor ama tek sürüm sunabiliyor: "seçici var, seçenek yok". Bu karar o
boşluğu kapatır.

**Uygulama düzeltmeleri (21 Tem).** Kodu yazmak kararın iki yerini
inceltti; ikisi de kayda geçer:

- **`Kind` durumu değil, DENETİMİ belirler.** "Satır çizimi Kind'e dallanır"
  ilk okunuşta "runtime'ın da çalışıyor/durdu durumu yok" demeye geliyordu.
  Yanlış: `php-fpm`'in gerçek unit'leri (`php8.3-fpm`…) vardır ve tarama onları
  toplar. Ölü bir php-fpm her PHP sitesini kırar; panodan alarmı kaldırmak
  körlük olurdu. Doğru ayrım ikilidir: **durumdan yalnız `tool` muaftır**
  (bize ait daemon'ı yoktur); **satır içi başlat/durdur ise yalnız
  `service`indir** — runtime'ın sürüm başına unit'i olduğu için tek bir
  "Durdur" düğmesi "hangi PHP?" sorusuna yalan söylerdi; onun denetimi sürüm
  çekmecesindedir. Saha kanıtı: `nftables` (tool) unit'i "inactive (dead)"
  görünürken güvenlik duvarı iki sunucuda da açık ve 12 kural yüklüydü — eski
  davranış çalışan bir firewall için "1 servis durdu" yanlış alarmı üretiyordu.
- **Katalog gerçeği önbelleğe alınmaz** (A7). Tür ayrımı ilk denemede
  `kind:""` olarak yayına gitti: `service_scan_cache` tüm API yanıtını, katalog
  alanları dahil saklıyordu. Kural artık açıktır: **önbellek yalnız taramanın
  keşfettiğini saklar** (kurulu mu / unit ayakta mı / sürümler / config'ler);
  ad, açıklama, ikon, kategori, paket adları ve `Kind` her okumada koddan
  birleştirilir. Bu, D-010'un "katalog tek kalır" cümlesinin çalışma-zamanı
  karşılığıdır — tek sahiplik, saklanan bir kopyayla birlikte var olamaz.

**Reddedilen alternatifler.** Ayrı `/runtimes` sayfası (ikinci ekran = cPanel'in
ikiliği) · ayrı `/apps` sayfası (Node satırının altındaki sayaç+liste yeterli;
30 uygulamayı aşarsa yeniden tartılır) · sunucu rolü/profil UI'ı (`Role` alanı
veri modeline şimdi girer ama filtre 25 kalemden sonra tartılır) · toplu onay
planlayıcısı (bağımlılık zinciri 2 kalemden derinleşmedikçe yazılmaz) ·
**"tür ayrımından sonra bir kez Tara'ya basılsın"** (A7'yi düzeltmek yerine
üstünü örterdi: hata `kind`'e özgü değildi, her katalog düzeltmesinde geri
gelirdi).

---

## D-009 · DNS sunucusu yoksa domain de yok: panel, domain'lerinin otoritesidir

*9 Temmuz 2026*

**Karar.** Bir domain ancak DNS sunucusu (PowerDNS/BIND) kuruluyken
eklenebilir ve domain'ler varken DNS sunucusu kaldırılamaz. CelikPanel
sunucusundaki her domain'in DNS'ini o sunucunun kendisi sunar — "ya da DNS'i
dış sağlayıcıda yönet" dalı yoktur.

**Neden.** Operatörün ya/ya-da mesajlarıyla yaşadıktan sonraki hükmü: "dns
yoksa domain de olmamalı — kafa karıştırıyor." Hiçbir şeyin sunmadığı bir
kayıt listesi, özellik kılığında bir tuzaktır; tek pencerede iki ayrı zihinsel
model, bir fazladır. Tek kural anında okunur: önce DNS kur, sonra domain. Bu,
D-008'in ilk-gün notlarındaki "DNS dışarıda da olabilir" duruşunu bilinçli
olarak geçersiz kılar — dış DNS, gerçek bir kullanıcı isterse ileride açık bir
gelişmiş mod olarak dönebilir; alfa tutarlılık için eniyilenir.

**Nasıl duruyor.** Ön-denetim her domain tipinin oluşturulmasını eyleme dönük
bir 409 ile engeller (pencere de tüm seçenekleri tek ve net bir bantla
kapatır); ayna koruması, herhangi bir domain varken dns-server grubu üyesinin
kaldırılmasını reddeder — yoksa her domain sessizce kararırdı; kuralın
önlediği tuzağın ta kendisi. Tüm "ya da dışarıda yönet" metinleri kaldırıldı.
Canlı kanıt: DNS'siz oluşturma → 409.

**Ek (26 Temmuz 2026 — iki sunuculu yetki).** DNS rolü makine çapında tek
birincil/tek ikincil değildir; PowerDNS'te sahiplik zone başınadır. İki
CelikPanel **Eşli** modda hem birincil hem ikincil yeteneğini açar: bir panelde
oluşturulan zone orada `MASTER`, diğer makinede otomatik ikincil kopyadır. Bu
sayede site hangi panelde oluşturulursa oluşturulsun iki ad sunucusu da yetkili
cevap verir. İki panel aynı ortak ad çiftini taşır; adlardan tam biri yerel IP'ye,
diğeri eş IP'ye çözülür. Glue yalnız bu adların sahibi olan üst domain'de bir kez
kaydedilir; müşteri domain'i altında child nameserver üretilmez.

---

## D-008 · Alfa: paneli operatör sürer; her boşluk bir ürün özelliğine dönüşür

*9 Temmuz 2026*

**Karar.** Debian 13 yeniden kurulumundan itibaren operatör, CelikPanel'i
gerçek bir müşteri gibi kullanır: her kurulum, her ayar, her domain panelden
ve kendi eliyle geçer. Geliştirici sunucuyu asla yapılandırmaz — izinle bile.
Operatör bir duvara çarptığında suç ürünündür: eksik yetenek panele eklenir
(ya da bozuk olan düzeltilir) ve ürün güncellemesi olarak yayınlanır. SSH ile
teşhis salt-okunur kalır.

**Neden.** Sunucuyu SSH ile elle onarmak, ürünü olduğundan bitmiş gösterir —
boşluk üründen değil görüş alanından kaybolur. Bu, D-005 ile (hiçbir şey
kurma) başlayıp "sormadan asla kurma" ile keskinleşen çizginin son
basamağıdır: geliştirici artık *hiçbir şey* kurmaz ve ayarlamaz. Alfa
kalitesi duvarlara bilerek yürümekten gelir.

**İlk günde nasıl işledi.** Bir öğleden sonralık gerçek operatör kullanımı
sırayla şunları çıkardı ve düzelttirdi: domain'in yanlışlıkla PHP zorunlu
sayması (oluşturma yolu tip sisteminden eskiydi) → php/statik/**yalnız-DNS**
tipli, canlı önkoşul denetimli rol-farkında Alan Adı Ekle (Plesk'in "no web
hosting" karşılığı); kurulu olmayan servislerin hayalet ayar sayfaları → her
sekme, motor listesi ve sürüm açılır kutusu artık sunucunun gerçek yetenek
kümesini okur (tek uç nokta: `/api/v1/hosting/capabilities`); ölü
phpMyAdmin/pgAdmin bağlantıları → genel bir `Requires` katalog kavramı
(çakışma gruplarının tersi): bağımlı araçlar üst servisleri kurulana dek
kilitlidir, kurulunca yalnız-loopback'te, admin-kapılı panel vekilinin
arkasında sunulur; panele gerçek sertifika almanın yolu yoktu → Ayarlar'da
tek tık Let's Encrypt + otomatik yenileme; ve en sessiz, en büyüğü — panel
`index.html`'i önbellek başlıksız veriyordu; tarayıcılar her güncellemeden
sonra ESKİ arayüzü göstermeye devam etti. Operatör, çoktan düzeltilmiş
hayaletleri gördü; artık giriş noktası `no-cache`, parmak izli asset'ler
immutable.

---

## D-007 · Sürüm seçimi, dağıtımı fork'lamadan yönetilen vendor depolarıyla

*9 Temmuz 2026*

**Karar.** Bir servis resmi bir yukarı-akış deposu bildirebilir (PostgreSQL →
PGDG). Onu açmak, kurulumda sürüm seçimini açan birinci-sınıf bir panel
eylemidir: dağıtımın dondurduğu tek major yerine, yönetici vendor'ın sunduğu
tüm güncel major'lardan seçer. Opt-in, seçilmiş, imzalı ve geri alınabilirdir —
asla varsayılan açık değil.

**Neden.** Bir dağıtım sürümü her veritabanı/çalışma zamanının tek major'unu
sabitler (Ubuntu noble yalnız PostgreSQL 16; Debian bookworm yalnız 15).
Müşteriler belirli bir sürüme inip orada kalmak ister — bu, operatörün açık
kaygısıydı ("PG'nin bir sürü sürümü var; install hangisini kurar, bizi tek
sürüme kilitler mi?"). İki kötü yanıt: *dağıtımı fork'la* (tüm paket ağacına
sonsuza dek sahip ol) ya da *OS karar versin* (hiç seçim yok). Sektör-standardı
orta yol, tüm güncel major'ları yan yana taşıyan vendor'ın kendi imzalı apt
deposudur.

**Minimal kurulumla (D-005/D-006) neden tutarlı.** Üçüncü-parti depo güven
sınırını genişletir; bu yüzden servis kurmakla aynı disipline tabidir: yalnız
katalogda tanımlı depolar açılabilir (UI asla URL geçirmez); imza anahtarı depo
başına apt `signed-by=` ile sabitlenir (küresel `apt-key` güveni yok); kapatmak
kaynağı + anahtarı temizce kaldırır. Zırhlı anahtar doğrudan kullanılır, böylece
**`gpg` çekilmez** — minimal ayak izi korunur. Sürüm seçimi deponun paket
desenine bağlıdır; asla keyfi paket kurulumuna dönüşemez.

**Nasıl doğrulandı.**
- Katalog: `ManagedRepo` (id, ad, anahtar URL'si, `{codename}` yer tutuculu
  kaynak şablonu, paket deseni); `postgresql` PGDG taşır. Agent `EnableRepo`/
  `DisableRepo`/`RepoStatus`/`RepoPackages`; hangi sürümlerin var olduğunun
  kaynağı kodumuz değil depodur (`apt-cache` ile keşfedilir, en yeni major önce).
- Panel `GET/POST /api/v1/repo` (yalnız admin, audit'li); izin listesi burada
  (depoyu katalogdan servis id'siyle çöz). Kurulum, bir sürüm paketini yalnız o
  servisin depo desenine uyuyorsa kabul eder.
- Üretim VPS'te kanıtlandı: baseline **yalnız `postgresql-16`** sunuyordu; PGDG
  açılınca **9 major (10–18)** açıldı ve `postgresql-17` seçimi PGDG'den
  `17.10-1.pgdg24.04+1`'e çözüldü; kapatmak baseline'a döndürdü.
- Testte yakalanan hata: agent `UMask=0027` ile koşar; `0644` keyring
  `0640 root:celikpanel` olarak indi — apt'ın yetkisiz `_apt` doğrulayıcısı
  okuyamadı, "imzasız" hatası verdi. Yazdıktan sonra `chmod 0644` ile düzeltildi.

**Düzeltme (21 Tem) — ikinci vendor, tek-vendor varsayımlarını ortaya çıkardı.**
Bu karar tek bir depoyla (PGDG) yazıldı ve uygulandı; PHP için Sury eklenince
üç varsayımın PGDG'ye özgü olduğu görüldü. Genel kural: *bir mekanizma tek
örnekle doğrulandığında, örneğe özgü olanı genel sanır.*

1. **"KeyURL zırhlı `.asc`'dir" yanlıştı.** Yukarıdaki metin bunu bir gerçek
   gibi yazıyor ("Zırhlı anahtar doğrudan kullanılır"). PGDG zırhlı yayınlar,
   **Sury ikili keyring yayınlar** (`0x99` = OpenPGP açık anahtar paketi;
   sunucuda ölçüldü) ve `.asc` varyantı yok (418). `fetchArmoredKey` onu
   reddederdi — yani karar, kendi kapsamındaki ikinci vendor'ı dışarıda
   bırakıyordu. Artık iki biçim de kabul edilir ve dosya **içeriğine göre**
   adlandırılır (`.asc`/`.gpg`); apt ikisini de doğrudan okur, gpg bağımlılığı
   yine çekilmez. Kabul, paket tag'i (tag 6) denetlenerek sınırlıdır — bir HTML
   hata sayfası güvenilir keyring olamaz.
2. **Sürüm sıralaması sondaki tam sayıyı okuyordu.** `postgresql-17` için 17
   verir; `php8.3-fpm` için 0 ("fpm" sayı değil). Tek vendor'da doğru görünen
   kod, PHP'de tüm sürümleri berabere bırakıyordu — çekmece sırası apt-cache ne
   bastıysa o oluyordu. Artık `(major, minor)` okunur.
3. **"Zaten kurulu" duvarı.** Kurulum, sürüm seçimine bakmadan servisin kurulu
   olup olmadığını soruyordu. PostgreSQL'de zararsızdı (tek küme); PHP'de
   ölümcül: 8.4 varken 8.3 isteği "PHP-FPM zaten kurulu" diye reddediliyordu.
   Düz kurulumda soru "bu servis burada mı", sürüm seçiminde "**bu sürüm**
   burada mı" olmalıdır (D-014'ün zincirini fiilen engelleyen şey buydu).

Ayrıca **`VersionCompanions`** eklendi: bir sürüm seçimi çıplak kurulur
(`php8.3-fpm`in tek başına veritabanı sürücüsü, mbstring'i, curl'ü yoktur) ve
panel, site sunamayacak bir runtime için başarı bildirirdi. PGDG'de bu ihtiyaç
yoktu çünkü `postgresql-17` tek başına çalışır — tek-vendor körlüğünün dördüncü
yüzü. Companion'lar en-iyi-çabadır ve atlanan **raporlanır** (Sury
`php8.5-opcache` yayınlamıyor; katı kurulum tek uzantı yüzünden 8.5'i tümüyle
reddederdi).

---

## D-006 · Saldırı yüzeyi yönetimi: her kurulum geri alınabilir

*8 Temmuz 2026*

**Karar.** Panel neyi kurabiliyorsa, onu kaldırabilir de. Servis kaldırma
(durdur + devre dışı + `apt purge --auto-remove`) kurulumun yanında birinci
sınıf bir eylemdir, elle SSH işi değil. Bu, üç parçalı "saldırı yüzeyi"
hattının ilkidir: **geri alınabilir kurulumlar**, otomatik güvenlik yaması ve
yalnız kullanılan portları açan bir güvenlik duvarı.

**Neden.** Operatörün ilkesi, açıkça: *kurulu her servis ya da paket bir
güvenlik riskidir — bekleyen bir CVE, açık bir port.* Minimalizm derli
topluluk değil, daha küçük saldırı yüzeyidir. Ama minimalizm ancak yüzey hem
büyüyüp hem küçülebiliyorsa gerçektir: nginx ekleyip asla kaldıramıyorsan,
yanlış ya da artık gereksiz bir kurulum kalıcı risktir. O yüzden kurulumun bir
aynası var.

**Nasıl korunuyor.**
- `Agent.UninstallService`: her unit'i durdurur + devre dışı bırakır, sonra
  `removePackages` purge eder (config gider, `--auto-remove` öksüz bağımlılığı
  düşürür). `InstallService`'in aynası; aynı whitelist, aynı dürüst "bu
  dağıtımda henüz yok".
- Panel `POST /service/uninstall` (yalnız admin, audit'li); UI tam paket
  listesini ve bağımlı site/postanın duracağı uyarısını içeren yıkıcı-onay
  modalı gösterir.
- Üretim VPS'inde kanıtlı: SpamAssassin kuruldu (dpkg var) sonra kaldırıldı
  (dpkg yok, unit gitti) — yüzey ölçülebilir şekilde küçüldü.

**Bu hattın ilerlemesi (tamam):** geri alınabilir kurulumlar ✅ · otomatik
güvenlik güncellemeleri ✅ · güvenlik duvarı ✅. Duvar varsayılan-reddet
gelen; panel tam olarak panel portu + kurulu her servisin bildirdiği portları
açar, agent ise SSH'ı (canlı dinleyicilerden tespit) + loopback + kurulu
bağlantıları daima açık tutar; bir kural operatörü asla kilitleyemez.
Kurulum/kaldırma yeniden senkronlar; açık-port kümesi daima koşan-servis
kümesine eşittir. VPS'te kanıtlı: açınca SSH, panel, web ve DNS erişilebilir
kaldı, politika gerisini düşürdü.

---

## D-005 · Kurulum panelden başka hiçbir şey kurmaz; sunucu yalnız-panel olabilir

*8 Temmuz 2026*

**Karar.** `install.sh` yalnız panel + agent'ı (kendine yeten Go binary'leri +
SQLite) ve dört minik indirme aracını (tar, xz, curl, ca-certificates) kurar —
barındırma için **hiçbir şey**. nginx, PHP, MariaDB, Postfix, PowerDNS, posta:
hepsi sonradan, panelden, talep üzerine eklenir. Bir sunucu yalnız-panel
kalabilir (örn. sadece yerleşik VPN isteyen biri) ve hiç domain olmadan IP
üzerinden yönetilebilir.

**Neden.** Anayasa: "kurulu olmayan görünmez." Taze kurulmuş bir sunucu, panel
dışında sıfır saldırı yüzeyi ve sıfır fazlalık taşır. Operatör tam istediği
sunucuyu kurar — tam bir barındırma kutusu, ya da sadece VPN ucu, ya da sadece
DNS. İstenmeyen hiçbir şey asla bulunmaz.

**Nasıl korunuyor.**
- Doğrulandı: `install.sh` yalnız `apt-get install tar xz-utils curl
  ca-certificates` koşar. Üretim VPS'inde nginx/PHP/vb. yalnız golden path'i
  kanıtlamak için *panelden* kurulduğu için var — çıplak kurulumda hiçbiri yok.
- Domainsiz erişim tasarım gereği çalışır: panelin self-signed sertifikası
  makinenin IP'lerini SAN'a koyar (`tmpl.IPAddresses = hostIPs()`), böylece IP
  erişimi eşleşen bir sertifika sunar (yalnız self-signed uyarısı, asla isim
  uyuşmazlığı). Domain + Let's Encrypt bir yükseltmedir, asla zorunluluk değil.

---

## D-004 · İşletim sistemi: önce Ubuntu LTS, BSD seçeneği korunur (asla fork)

*8 Temmuz 2026*

**Karar.** Ubuntu 24.04 LTS birinci sınıf, yalnızca test edilen hedeftir. BSD
**şimdilik** bilinçli bir hedef-dışıdır — ama ileride desteklemek seçeneği
mimariyle korunur ve **asla** ayrı bir ürün olmaz.

**Neden Ubuntu, BSD değil.**
- Ürünün tamamı Linux'a özgü parçaların üstünde: systemd (agent'ın kalbi —
  servisler, `celikapp-*`, `wg-quick@`), apt + paket whitelist'leri, nftables
  (VPN NAT) ve postfix/dovecot/opendkim'in Ubuntu yerleşimi. OS değiştirmek =
  agent'ın "ellerini" yeniden yazmak, kullanıcıya sıfır fark için aylarca iş.
- Kullanıcının beğendiği kararlılık ("versiyonlar kilitli, kararlı") zaten
  **Ubuntu LTS'nin kendisi**: 5 yıl yalnız güvenlik yaması. BSD, elimizde
  olmayan bir kararlılık katmaz.
- Pazar gerçeği: hiçbir rakip BSD'de değil (cPanel = RHEL ailesi, Plesk =
  Linux+Windows, aaPanel = Linux). VPS imajları, müşteri alışkanlıkları,
  WordPress/PHP ekosistemi, Let's Encrypt araçları hep Linux-öncelikli.
  Müşteri "WordPress'im çalışıyor mu, mailim spam'e düşüyor mu?" diye sorar —
  "BSD'de misiniz?" diye değil.

**Seçeneği ucuz tutan şey — ve neden fork DEĞİL.**
- Panel↔agent RPC ayrımı (güvenlik için kuruldu) aynı zamanda OS
  bağımsızlığıdır. **Panel** (HTTP, SQLite, UI, iş mantığı) taşınabilir Go;
  OS'e dokunan tek katman **agent**. Panel *ne* yapılacağını der ("site kur");
  *nasıl*ını yalnız agent bilir (systemctl/apt/nftables).
- İddia değil kanıt: bu tarih itibarıyla `make freebsd-cross` **hem** paneli
  hem agent'ı derliyor. Tek bir taşınabilirlik düzeltmesi gerekti (Statfs_t
  alan tipleri Linux ile BSD'de farklı — `cmd/panel/system_stats_disk_unix.go`'da açık
  `uint64` dönüşümleri) ve yapıldı. Kod tabanının tamamı bugün FreeBSD'ye
  çapraz derleniyor.
- İki CelikPanel, kullanıcının korktuğu hantallığın ta kendisi olurdu: her
  özellik iki kez, her hata iki kez, iki vasat ürün. Bunun yerine olası bir
  BSD geleceği = aynı RPC yüzeyinin arkasına bir **BSD agent arka ucu** (rc.d,
  pkg, pf). Tahmin: yıllar değil haftalar — tüm yığın (nginx, postfix,
  dovecot, opendkim, PowerDNS, MariaDB, WireGuard) FreeBSD ports'ta var;
  yalnız "eller" değişir. Panel, UI, DB, iş mantığı: sıfır değişiklik.

**Bugün ne yapıyoruz.** Neredeyse hiçbir şey — ve asıl nokta bu. Tek bedel
disiplin: yeni agent özellikleri *ne*yi (RPC yüzeyi) *nasıl*dan (exec
çağrıları) ayrı tutar, ki kod zaten böyle yazılıyor. Spekülatif soyutlama
katmanı yok, "belki lazım olur" kodu yok. Karar, gerçek talep doğduğunda
verilir (örn. Linux'ta bir güven/güvenlik krizi barındırıcıları BSD'ye
iterse).

**Diğer dağıtımlar.** `detectPkgFamily` apt / dnf / pacman tanır. Bugün yalnız
apt (Ubuntu/Debian) tam destekli ve test edilmiş; dnf/pacman tanınır ama
kurulum tahmin etmek yerine dürüstçe "bu dağıtımda henüz otomatik kurulamıyor"
der. Not: dnf module stream'leri (`postgresql:16` vs `:18`) orada gerçek bir
sürüm seçiciyi anlamlı kılar — dnf desteği eklersek UI buna hazır.

*Ek (16 Tem, operatör kararı):* Arch artık bir **geliştirme-test hedefi** —
taşınabilirlik sözünü dürüst tutmak için ikinci test sunucusu bilerek Arch'ta.
`install.sh` pacman'i destekler (ön gereksinimler, araç zinciri mimarisi
`uname -m` ile, dürüst "güvenlik-yalnız kanal yok" notu).

*Ek 2 (16 Tem, aynı gün genişletildi):* Operatör iki sunucuda aktif test
yaparken "apt'ye özgü katalog" duvarına üç kez çarptı (certbot kurulumu, bind
kaldırma, paket adı `bind9` gösteren onay penceresi). Karar: **agent'ın paket
katmanı pacman'i de sürer** (kur `-S --needed`, kaldır `-Rns`, varlık `-Q`;
asla `-Syu` — sürpriz sistem yükseltmesi yok). Katalogda pacman paket adları
yalnız eşlemenin kesin olduğu VE dağıtıma özgü init adımı istemeyen servisler
için dolu; MariaDB/PostgreSQL (initdb ister), phpPgAdmin (AUR-only) ve Redis
(Arch'ta Valkey çatalı) bilerek boş — dürüst "henüz desteklenmiyor" derler.
UI, paket adlarını agent'ın bildirdiği aileden gösterir (`Agent.PkgFamily`).

*Ek 3 (3 Ağu 2026):* Debian 13, Ubuntu 24.04'ün yanında açık bir apt kabul
hedefidir. Bu, çalıştırılmamış bir testi geçmiş saymaz: sürüm kabul edilmeden
önce etiketli, önceden derlenmiş ürün tam belgelenmiş operatör akışıyla temiz
Debian 13 kurulumundan geçmelidir. Kaynak checkout'ları, geliştiriciye özel
bundle'lar ve kurulum sonrası SSH düzeltmeleri kanıt sayılmaz.

---

## D-003 · Servis kataloğu: her şeyi göster, çakışmayla engelle — "yönetilmiyor" ile değil

*8 Temmuz 2026*

**Karar.** Servisler sayfası (yalnız admin) tüm kataloğu listeler, kurulu olsun
olmasın, tek tıkla Kur ile. Derinlemesine yönetmediğimiz servisleri
**gizlemeyiz** ve "yönetilmiyor" etiketleriyle **karmaşıklaştırmayız**. Bir
kurulumu engelleyen tek şey **gerçek bir çakışma**dır.

**Neden.** Kullanıcının sezgisi: "yönetiyor muyuz?" üzerinden kapılamak
gereksiz karmaşıklık. Asıl kısıt, iki servisin yan yana çalışıp
çalışamayacağıdır.
- **Çakışma grupları** (katalogda `ConflictGroup`): aynı rol/portu tutanlar
  karşılıklı dışlar — :80'de web sunucusu (Nginx ↔ Apache), :53'te DNS (BIND ↔
  PowerDNS). Biri kuruluysa diğerinin Kur düğmesi "X ile çakışır" olur.
- **Grup yoksa = yan yana**: MariaDB + PostgreSQL birlikte koşar (farklı
  portlar), ikisi de kurulabilir. Redis + Memcached de öyle.

**Sonuç.** Her kurulum admin'in açık, tek-tıklık onayıdır ("yalnız izinle kur"
ilkesine uygun). Katalog kategoriye göre akordiyonla gruplu (tümünü aç/kapat);
düzinelerce servise doğru büyüdükçe taranabilir kalır.

---

## D-002 · Servis sürümleri: dürüst tek sürüm, gerçek çoklu-sürüm yalnız var olan yerde

*8 Temmuz 2026*

**Karar.** Kurulum modalı apt'ın kuracağı **gerçek** sürümü gösterir (canlı
`apt-cache policy` ile okunur), sürüm *seçici* değil. Seçici yalnız birden
fazla sürümün gerçekten var olduğu yerde sunulur.

**Neden.** Bir Debian/Ubuntu sürümü her paketin tam olarak tek sürümünü verir
ve dondurur (yalnız güvenlik yaması) — kararlılık modeli bu, bilerek. Tek
seçenekli bir açılır liste, kullanıcıyı yanıltan bir yalandır. Dolayısıyla:
- **Tek-sürümlü servisler** (PostgreSQL, MariaDB, nginx…): ne ineceğini göster
  ("Kurulacak sürüm: 16"), seçici yok.
- **Gerçekten çoklu-sürüm, hosting-kritik** → dağıtımdan bağımsız, gerçek
  seçiciyle:
  - **PHP** — yan yana paketler (`php8.1-fpm`…`php8.4-fpm`), site başına
    seçilir. ⚠️ *Düzeltme (20 Tem, bkz. D-010): yalnız YARISI yapılmış.* Tespit
    (`DetectInstalledPHPVersions`) ve site başına seçim çalışıyor; ama katalogda
    `php-fpm` için `Repo` tanımlı olmadığından panel çoklu sürüm **kuramıyor** —
    dağıtımın tek sürümüyle sınırlı. Sury vendor deposu D-010 ile eklenir.
  - **Node.js** — dağıtımdan bağımsız resmi tarball'lar, proje başına seçilir.
    ✅ yapıldı.
- **Gelecek seçeneği**: Ubuntu 24.04'te örn. PostgreSQL 18 isteyen müşteri,
  üreticinin kendi apt deposunu (PGDG) bilinçli bir özellik olarak eklemek
  demektir — gerçek depo yönetimi, asla sahte seçici. Backlog'da, yapılmadı.

Desen: **dağıtımın tek sürümü = güvenli taban; gerçek çoklu-sürüm (PHP, Node)
kendi mekanizmamızla, dağıtımdan bağımsız.** cPanel/Plesk gibi (PHP'yi
kendileri derler, gerisini dağıtıma bırakır).

---

## D-001 · Güncelleme ve geri alma: sunucuyu asla sıfırdan kurma

*8 Temmuz 2026 · 28 Temmuz 2026'da değiştirildi*

**Karar.** Üretim güncellemeleri yalnız
`sudo /bin/bash ./bootstrap-update.sh --normal` ile veya bir kerelik
`sudo /bin/bash ./bootstrap-update.sh --bootstrap-pre-ledger` geçişiyle başlar.
Bootstrap temiz ve incelenmiş commit'i dışa aktarır;
`/var/backups/celikpanel/releases/<commit>-<nonce>/` altında değişmez, root
sahipli bir release yayımlar. Güncellemeyi yalnız o release'in açık modla
çağrılan `update.sh` dosyası yapabilir. Ayrıcalıklı güncelleme `git pull`
çalıştırmaz ve değişebilir checkout içindeki updater veya installer'ı
çalıştırmaz.

Her güncelleme önce bir geri-alma snapshot'ı alır ve doğrular. Kurtarma yalnız
update çıktısındaki kesin komutu ve `VERIFIED_SNAPSHOT` değerini kullanır:
`sudo /bin/bash /var/backups/celikpanel/releases/<commit>-<nonce>/rollback.sh "$VERIFIED_SNAPSHOT"`.
Checkout'taki `rollback.sh` veya başka bir release'in
rollback scripti kurtarma yolu değildir. Servis mutasyon ledger'ı asla elle
oluşturulmaz, değiştirilmez, boşaltılmaz veya migrate edilmez; yalnız ürünün
kontrollü tek seferlik initializer'ı pre-ledger kurulum akışında onu
oluşturabilir.

**Neden.** Kutuda müşteri olması, verinin her güncellemeden sağ çıkması
gerektiği anlamına gelir. Müşteri verisi (SQLite DB, site dosyaları, posta,
DNS, sertifikalar, DKIM anahtarları) değişen yolların (`bin/`, `web/`) dışında
yaşar. Anlık görüntüden sonra güncelleme, şema geçişlerini iki koordinatör de
dururken staged panelin `--migrate-only` modu üzerinden çevrimdışı çalıştırır;
bu adım tamamlanmadan hiçbir unit yeniden başlatılamaz. Anlık görüntü (panel DB +
binary'ler + unit dosyaları + kaynak commit'i) "bir güncelleme işleri bozdu"yu
felaket değil, kurtarılabilir bir olay yapar. Düzeltme akışı: dev kutusunda
(VPS'in aynası) yeniden üret → kanıtla → push → VPS'te
[`bootstrap-update.sh`](../bootstrap-update.sh) ile değişmez release'i stage
edip doğrula → gerekirse yalnız çıktıda verilen root tarafından güvenilen
rollback komutunu kullan. İç release ve snapshot sözleşmeleri için
[`update.sh`](../update.sh) ve [`rollback.sh`](../rollback.sh) dosyalarına bakın.
