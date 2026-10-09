# CelikPanel Yol Haritası

*Son güncelleme: 9 Ekim 2026 · [English](ROADMAP.md)*

---

## Anayasa — Her Kararın Süzgeci

Her özellik, commit ve tasarım kararı bu gereksinimleri karşılamalıdır.
Bunlar mevcut uygulamanın karşıladığı iddiası değil, bağlayıcı yükümlülüklerdir.
[Dayanıklı çalışma sözleşmesi ve kaynak incelemesi](docs/RESILIENCE-CONTRACT.tr.md),
açık P0 işlerini, uygulama sırasını ve kapatılmaları için gereken kanıtı kaydeder (D-025).

### 1. Güvenlik ve sunucu sahibinin yetkisi
- Yönetim erişiminde kimlik doğrula; her eylemi ilgili kaynaklar için yetkilendir. En az yetki, yerel kimlik doğrulamalı IPC, parametreli SQL ve gizli değerler için `crypto/rand` kullan.
- Root yetkisi olmayan Panel ile ayrıcalıklı Agent ayrı kalır. Normal ayrıcalıklı otomasyon yetkili Agent API'sini kullanır; desteklenen kullanıcı kurtarmasının ayrı ve dar bir sözleşmesi vardır. Yapay zekâ planlayıcısı veya kurtarma ekranı sınırsız root çalıştırma yetkisi kazanmaz.
- Sunucu sahibi hizmetleri kendi araçlarıyla yönetebilir. Sahibinin değişikliklerini algıla ve açıkça uzlaştır; önbellekteki hedefe uydurmak için sessizce üzerlerine yazma (D-022).
- Kanıt belirsizse ilgili güvensiz değişikliği engelle. Doğrulanmamış yetki vermeden, kimlik doğrulamalı tanı ve desteklenen kurtarma erişimini koru.

### 2. Süreklilik ve kurtarılabilirlik
- Panel kesintisi, lisans kaybı veya panelin kaldırılması; barındırılan işleri ve hizmetlerin kendi yenileme, zamanlama ve açılış mekanizmalarını durdurmamalıdır. Bağımsızlık iddiasından önce kalan bağımlılıklar giderilip sınanmalıdır.
- Her değişiklik; etkilenen kaynakları, salt-okur ön kontrolü, kalıcı kontrol noktalarını, sınırlı tekrar/kurtarmayı ve sonuç kanıtını tanımlar. İzin veya sahiplik normalleştirmesi de değişikliktir.
- Aday sürüm, normal Agent, uygulama şema geçişi veya lisans doğrulayıcı çalışmadığında kurtarma kullanılabilir kalmalıdır. Karışık ya da doğrulanmamış uygulama durumu normal yönetimi engelleyebilir; bağımsız kurtarma yolunu ortadan kaldıramaz.
- Aday sürüm ve kurtarma uyumluluğu doğrulanana kadar son doğrulanmış kullanılabilir durum ile kurtarma malzemesi korunur. Temizlik bu kanıttan sonra gelir.
- Otomatik onarım, kabul edilen işlemin kuralları belli ve tekrarlandığında ek etki yaratmayan devamı veya telafisidir. Eksik kanıt uydurmaz, sahibinin sonraki değişikliklerini geri almaz, doğrulamayı atlamaz veya sonucu belirsiz ikinci bir değişiklik başlatmaz.

### 3. Gerçeği yansıtan durum ve ortak sözleşmeler
- Sahibin niyeti, yetki, yayımlanmış yapılandırma, gözlem, yürütme, doğrulama ve kurtarma ayrı tutulur. Bilinmeyen; yok, başarısız, süresi dolmuş veya tamamlanmış değildir.
- Her kalıcı çıktının sürümlü tek bir üretim/okuma/geri yükleme sözleşmesi ve açık desteklenen geçişleri vardır. Kanıt rolüne göre karşılaştırılır; geçerli yayımlama kaydın yalnız bir bölümünü ilerletiyorsa bütün kayıt eşitliği aranmaz.
- Tamamlanmış kurulum adımı geçmiş yürütmenin kanıtıdır; güncel sağlık kanıtı değildir. Mevcut neden, sorumlu kişi, sonraki eylem ve aynı işlemin nasıl süreceği açıklanır (D-024).
- Tarayıcı yetkili işlem durumunu gözlemler. Yenileme, yeniden bağlanma ve zaman aşımı ikinci işi yetkilendirmez veya tamamlanma anlamına gelmez.

### 4. Sadelik
- Her olağan işin kullanıcı için tek ve açık yolu olur. Tarayıcı ile desteklenen kullanıcı kurtarması aynı işlem sözleşmesini paylaşır; tek ekran, kurtarmanın tek arıza noktası olamaz.
- Özellik somut kullanıcı ihtiyacı için eklenir. Kabul edilen kapsam içinde güvenli varsayılanlar kullanılır; gelişmiş seçimler gerektiğinde gösterilir.
- Kullanılmayan hizmetlerin özel menüleri arayüzü doldurmaz. Kullanıcının işi tamamlamasına yardımcı olan kurulum keşfi, gerçek çakışmalar ve kurtarma eylemleri görünür kalır.
- Olağan işler panelden yapılabilmelidir. Sahibin kendi araçlarıyla yönetimi ve kurtarması desteklenir; elle kurtarma, kurtarmayı yasaklama gerekçesi değil otomasyon açığının kanıtıdır.

### 5. Kanıtla ölçülen hız
- API yanıtında 100 ms altı, hızlı arayüz tepkisi ve asgari kurulumda 60 saniye hedefleri korunur. Bir hedefin karşılandığı söylenmeden ölçülen platform ve kapsam kaydedilir.
- Çalışma ortamı küçük tutulur. Mevcut Panel/Agent yetki ayrımı, hizmetlerin kendi yaşam döngüsü ve bağımsız kurtarma mekanizması tek binary söyleminden önce gelir.
- Hız; doğrulamayı, kurtarılabilir kontrol noktalarını veya arıza testlerini atlamayı haklı çıkarmaz.

### 6. Esneklik ve bağımsızlık
- Türleri belirli API'ler, modüler hizmetler ve standart protokoller kullanılır; isteğe bağlı otomasyon, hizmetin çalışmasından ayrıdır.
- Yedekler ve dışa aktarımlar standart biçimdedir. Yönetim yazılımı sahibinin verisinin sahibi olamaz veya veriyi rehin tutamaz.
- Standart DNS çoğaltması karşı tarafta panel veya lisans gerektirmez. Ayrı yetkilendirilen uzaktan kayıt yönetimi isteğe bağlıdır.
- Kurulu panel güncellemelerini yalnız kullanıcı CelikPanel'in güncelleme ekranından başlatır. Yayınlama, tanı ve desteklenen geri alma asistana kurulum izni vermez.

### Dürüstlük ve sürüm kabul kuralı

Test, güvenlik incelemesi ve dokümantasyon gereklidir. Yaşam döngüsü desteği ayrıca
geçici ve gerçek sistem hizmetleri kullanan ortamlarda eksiksiz arıza geçiş kanıtı
ister: gerçek önceki sürüm durumu, başarısız güncelleme, fiilen otomatik geri yükleme,
kurtarmanın kesilmesi/yeniden başlatma, sahibin değişikliklerinin korunması,
yönetim kurtarması ve hizmet kontrolleri. Bileşen testleri, taklit servis yöneticisi
ve tek başına başarılı kurulum bu sözleşmeyi kanıtlamaz.

Her yaşam döngüsü değişikliği; etkilenen ilkeyi, şema/sürüm geçişini, kurtarma
davranışını ve kabul kanıtını adlandırır. Ölçülmemiş veya sonucu belirsiz işler
açık kalır. Dar kapsamlı bir olay düzeltmesi sınırları belirtilerek yayımlanabilir;
bu, temel P0 işlerini kapatmaz veya ilgisiz özellik genişlemesini haklı çıkarmaz.
Dayanıklı çalışma iddiasından önce D-025'in kabul matrisi uygulanıp geçmelidir.
Sistemin mümkün olan her arızayı kendiliğinden gidereceği vaat edilmez.


---

## Neredeyiz — 26 Eylül 2026

**Güncel öncelik: ürünü genişletmeden önce D-025 mimari dayanıklılık işini tamamlamak.**
Bu inceleme, yerel kaynağın `f253d318` commit'ine kadarki durumunu ve saklanan kabul
raporlarını kapsar. Frankfurt/Boston'un yeniden incelendiği, yeni sürüm yayımlandığı
veya değişikliklerin kurulu olduğu anlamına gelmez. P0.1–P0.5'in tamamı **kısmi**;
hiçbiri kapanmış değil. Anayasa gereksinimleri ve mevcut iş kimlikleri değişmedi.

### Kanıtlanan kapsam ve kalan işler

| Mevcut iş | Belirli kapsamda uygulanan veya doğrulanan | Kapatmak için gereken |
|---|---|---|
| P0.1 — Gerçek güncelleme/geri alma | Arch/Debian üzerinde gerçek eski sürüme dönüş; sonraki şema geçişi ve kurtarma deneylerinde gerçek Alpha64 schema38 verisi. [Kanıt](deploy/e2e/release-recovery/ISOLATED-DATABASE.md). | Desteklenen güncelleme/arıza/hizmet matrisi ve üretim imzalı aday kabulü tamamlanmalı; seçili aşamanın geçmesi bütün güncellemenin kabulü değildir. |
| P0.2 — Erişim ve doğru durum | Bağımsız, kimlik doğrulamalı kurtarma/durum girişi; seçili CLI, HTTP ve tarayıcı sonuçları eşleşiyor. Gerçek açılış beklemesinde tekrarlar aynı işlemi koruyor. [Kanıt](deploy/e2e/release-recovery/BOUND-WORKER.md). | Gerçek bekleme/hata/yeniden bağlanma durumları, bilinen hatanın korunması, beklerken tarayıcı erişimi ve üretim güven zinciriyle kullanıcının başlattığı güncelleme yolu. |
| P0.3 — Bağımsız kurtarma | Ayrı korunan kurtarma kodu/verisi, ayrı kopyada DB dönüşümü ve atomik yayın; geri alma sırasında seçili kesintiler ve yeniden açılış sonrasında kesinti anındaki satırlar korunarak otomatik kurtarma. [İki arızalı deney](deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md). | Kalan kontrol noktaları, eksik yedek yakalama, dosya izin/sahiplik geçişleri, eski sürüm uyumu ve güvenli temizlik. Sonunda kurtulma, kesintisiz hizmet veya güç kaybı dayanıklılığı değildir. |
| P0.4 — Ortak DNS/TLS sözleşmeleri | DNS sahiplenme/yayın rolleri ayrıldı; ortak TLS ve DNS okuyucuları, bağımsız DNS gözlemi ve seçili Agent aracılı arıza kurtarması mevcut. [Sözleşme](docs/DNS-ENGINE-ARTIFACT.md). | Desteklenen **Agent'tan bağımsız DNS geri alma yürütmesi**, gerçek kesinti/sahip değişikliği kabulü, bütün üretici/geri yükleme geçişleri ve yetkili üst bölge bulunmayan ikincilde silme kanıtı. Henüz kullanıma açılmayan ters işlem kodu ve salt-okur gözlem bu açığı kapatmaz. |
| P0.5 — Hizmet bağımsızlığı | Sınırlı güvenlik duvarı/posta yenileme ve devreye alma kurtarması; raporda belirtilen yönetimsiz veya yönetim devre dışı açılışlarda tek PowerDNS ve BIND/BIND çiftinin hizmet vermesi. [Posta kanıtı](deploy/e2e/release-recovery/MAIL-ENROLLMENT-MANAGEMENT-ABSENT-BE.json); [DNS kanıtı](deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md). | Farklı DNS motorlarının çiftleri, tam kurulum/devreye alma ve eski uygulama uyumu; desteklendiği söylenen her yönetimsiz/kaldırılmış durumda web/DB/posta/cron/yenileme/güvenlik duvarı kontrolleri. Yönetimi devre dışı bırakmak, tamamen kaldırmak değildir. |

Ayrıntılı [kabul kaydı](docs/RESILIENCE-CONTRACT.tr.md), başarısız denemeleri ve
kesin sınırları korur. Yeni kanıt mevcut P0 maddesini günceller;
yeni bir mimari plan başlatmaz.

### Sürüm durumu — 9 Ekim 2026

Bu alt bölüm sürümlerin nerede durduğunu kaydeder. Hiçbir P0 durumunu
değiştirmez: P0.1–P0.5'in tamamı hâlâ **kısmi**; yukarıdaki ve aşağıdaki her açık
kabul işi açık kalır.

- **v0.1.0-alpha.81 yayımlandı.** `v0.1.0-alpha.81` etiketi `a0beb726`
  üzerindedir (4 Ekim 2026); bu, yayımlanmış `main` dalının ucudur. Sunucu
  sahibinin bildirdiği ve burada bir dosyada kayıtlı olmayanlar: yayımlama
  adımları (#204 numaralı çekme isteğinin birleştirilmesi, etiket, portal
  yayını), altı imzalı dosyanın doğrulanması ve sahibin kurulu iki paneli (biri
  Ubuntu 24.04, biri Debian 13) 8 Ekim 2026'da panelin kendi güncelleme
  ekranından güncellemesi. Depoda bu kurulu sunuculara ait bir kanıt dosyası
  yoktur; bu yüzden bu bir ölçüm değil, sahibin bildirimidir ve buradaki hiçbir
  iş için kabul kanıtı olarak kullanılmaz.
- **v0.1.0-alpha.82 bir adaydır, sürüm değildir.** Numara sahibin kararıdır.
  Aday `fix/setup-handover-guidance` dalıdır (#205 numaralı taslak çekme
  isteği): `a0beb726`'dan sonra `f07cbcb5d`'ye kadar 32 commit, ardından yalnız
  kanıt ya da belge ekleyen başka commit'ler (ilki kapanış koşusunun kanıtıydı
  ve sayıyı 33 yaptı); ürün kodunu değiştiren son commit `67b62cc0f`
  ([sürüm notu taslağı](docs/RELEASE-NOTES-v0.1.0-alpha.82.tr.md)).
  İçeriği: bilmediği olumsuz bir durumu söylemek yerine "denetleniyor" ya da
  "denetlenemedi" gösteren ekranlar; hangi durumdan kurulduğunu sürümüyle
  taşıyan ve durum eskimişse reddedilen ayar kayıtları (posta politikası, yedek
  zamanlaması, zamanlanmış görevler, yapılandırma dosyaları, catch-all);
  hizmetin sonradan gösterdiğine göre yanıtlanan yeniden yüklemeler ve hizmet
  işlemleri; sekiz rotada durum değiştiren her isteğin tek bir kimlik taşıması,
  böylece yinelenen isteğin yeniden çalıştırılmadan yanıtlanması
  ([D-029](docs/DECISIONS.tr.md)); cPanel içe aktarımı, sertifika istekleri ve
  Arch'ta PHP siteleri için düzeltmeler; `postconf`'tan okunan değerler için
  tek kural. Bu kural, yayımlanmış v0.1.0-alpha.81'de de bulunan bir posta
  sertifikası kusurunu düzeltir.
  - *Geçici konuklarda, 8 ve 9 Ekim 2026'da ölçülenler (Debian 13,
    Ubuntu 24.04, Arch; kayıt bir sayı vermedikçe hücre başına tek sıra):*
    gerçek cron, Postfix, PostgreSQL ve MariaDB üzerinde ayar kayıtları
    ([birinci koşu](deploy/e2e/release-recovery/evidence/set1-20261010/README.md));
    o koşunun yol açtığı düzeltmeler, hizmet işlemleri ve istek kimliği
    ([ikinci koşu](deploy/e2e/release-recovery/evidence/set2-20261011/README.md));
    ikinci tur düzeltmeler ve yayımlanmış v0.1.0-alpha.81'den sahibin başlattığı
    güncelleme; on hücrenin tamamı beklenen sonuca ulaştı
    ([üçüncü koşu](deploy/e2e/release-recovery/evidence/set3-20261012/README.md));
    son düzeltmeler, `557b554eb` hâliyle yeni kurulmuş konuklarda: Arch dahil
    üç platformda PHP sitesi oluşturuldu, çalıştı ve silindi; yayımlanmış
    v0.1.0-alpha.81'in oluşturduğu site Debian 13 ve Ubuntu 24.04'te güncelleme
    boyunca on isteği aynı biçimde yanıtladı; tek bir denetim geçmedi:
    Ubuntu 24.04'te Postfix durdurulduktan sonraki not
    ([dördüncü koşu](deploy/e2e/release-recovery/evidence/set4-20261009/README.md));
    o hatanın nedeni ve düzeltmesi, yeni kurulmuş Ubuntu 24.04 ve Debian 13
    konuklarında dörder durdurma
    ([ek ölçüm](deploy/e2e/release-recovery/evidence/set4b-20261009/README.md)).
    İlk üç dizinin adı, koşuların yapıldığı günlerden daha ileri tarihler
    taşır. Konuklar ağdan yalıtılmış değildir: dışarıya erişimleri vardır.
    Saptanan şudur: sürüm kaynağının her hücrede, Let's Encrypt adlarının bazı
    hücrelerde (hangilerinde ve hücrenin hangi anında olduğunu sürüm notları
    söyler) konuğun kendi loopback adresine sabitlenmesi, lisans adımının test
    derlemesince yanıtlanması ve ham dosyalarda kurulu hiçbir sunucunun adının
    ya da adresinin bulunmaması. Konukların trafiği kaydedilmedi.
  - *Yalnız bir okuma olarak ölçülen:* `main.cf` uyarı verdirdiğinde gerçek
    `postconf`'un ne yazdırdığı ve eski kodun sakladığı metinle eski geri alma
    komutunun ne yaptığı
    ([okuma](deploy/e2e/release-recovery/evidence/set4c-20261009/README.md);
    Postfix 3.10.13, bir geliştirme konuğunda özel bir yapılandırma dizini).
    Buna dayanan düzeltmenin (`67b62cc0f`) yalnız bileşen testleri var: böyle
    bir dosyayla posta TLS değişikliği, geri alınması ve sertifika yayımı
    çalıştırılmadı.
  - *Son kodda (`67b62cc0f`) ölçülen, 9 Ekim 2026:* güncelleme matrisi bir kez
    daha, üçüncü koşunun aynı on hücresi, her biri bir kez
    ([kapanış koşusu](deploy/e2e/release-recovery/evidence/set5-20261009/README.md)).
    On hücrenin tamamı üçüncü koşunun `cfa329676` hâlinde ölçtüğü sonuca ulaştı;
    her adımın kararı, sonuç ve karşılaştırılan olgular aynıydı: Debian 13, Ubuntu 24.04
    ve Arch'ta doğrulanan güncelleme; üçünde de yayımlanmış v0.1.0-alpha.81'e
    otomatik dönüş; başarısız başlangıç denetiminden sonra dönüş (Debian 13);
    sahibin devam ettirmesi (Debian 13, Ubuntu 24.04); yeniden açılış boyunca
    yönetimin kapalı olması (Debian 13). v0.1.0-alpha.81'in oluşturduğu site
    güncelleme boyunca on isteği aynı biçimde yanıtladı (Debian 13,
    Ubuntu 24.04); güncellenmiş sunucuda platform başına bir Postfix durdurma
    notla yanıtlandı. Bu koşuda konukların diskleri bellekteydi; bu yüzden
    süreleri önceki koşularla karşılaştırılamaz. Ana makine koşunun büyük
    bölümünde "modern bekleme" kaydetti; örnekleyiciler duraklama göstermiyor.
  - *Hangi denetim hangi koda dayanıyor:* ayar kayıtları `c4cf7fd9d`;
    düzeltilmiş ayar kayıtları, hizmet işlemleri ve istek kimliği `faa5ef085`;
    ikinci düzeltmeler ve ilk güncelleme matrisi `cfa329676`; Arch'ta PHP
    dahil yeni kurulmuş sunucu denetimleri `557b554eb`; yeni kurulmuş
    sunucularda Postfix durdurma notu `1f182a483` ile aynı dosyalar; kapanış
    güncelleme matrisi `67b62cc0f`. Son kodda çalıştırılmayanlar: yeni kurulum
    ve gerçek bir sistemde düzeltilmiş posta sertifikası yolu (anlık görüntü,
    geri yükleme, geri okuma).
  - *Sonraki sürüme açık iş ve bitiş ölçütü:* Panel'in kendi başlangıcı,
    barındırılan sitelerin web sunucusu yapılandırmasını yeniden yazar; bu
    adayda da önceki sürümlerde de. Sahibin böyle bir dosyadaki el
    düzenlemesinin o başlangıçta neyle karşılaştığı ölçülmedi ve anayasanın 1.
    bölümündeki kurala (sahip değişikliklerini algıla, asla sessizce ezme)
    göre denetlenmedi. Bitiş: sahibin düzenlediği bir site yapılandırma
    dosyasının bir Panel başlangıcı ve bir güncelleme boyunca gerçek sistemde
    okunması; ürün düzenlemeyi ya algılayıp bildirir ya da ezme, açıkça
    yazılmış ve belgelenmiş bir sınırdır.
  - *Yalnız bileşen testleri olanlar:* `e2be8af30` (MariaDB olmayan bir
    `mysqld`, MariaDB dosyasının denetimi olarak kabul edilmez), durdurmadan
    sonra durumu oturmayan ya da okunamayan birim için iki yeni not ve posta
    sertifikası yenilemesinin sunulan sertifikayı denetlemesi.
  - *Açık ve sürüm notlarında adıyla yazılı:* arayüzün 31 kaynak dosyası
    sunucuyu hâlâ eski biçimde okuyor (erişim kapıları, kurulum sihirbazı,
    güncelleme ve işlem katmanları, Hizmetler sayfaları ve başkaları); sekiz
    rotanın dışındaki durum değiştiren rotalarda istek kimliği yok; bağımsız
    posta yenileme yardımcısı zaten kurulu olan sunucu kurulu yardımcısını
    korur; gerçek sistem koşularında gerçek bir Panel'e karşı hiçbir ekran
    çizilmedi; yayımlanmış sürümden güncelleme imzalı arşivden değil, etiketin
    kaynağının deneme lisansıyla derlenmesinden ölçüldü; 58 karakterlik bir
    site adını olağan nginx reddeder; sertifika doğrulama dizini, reddedilen
    bir oluşturmadan ve bir silmeden sonra yerinde kalır.
  - Bu aday hiçbir P0 işini kapatmaz ve ilerletmez. Yayımlanması kurulu hiçbir
    paneli güncellemez; güncellemeyi yalnız sahip, panelin kendi güncelleme
    ekranından başlatır.

### Son DNS sonuçları ve sınırları

- [Güncel kaynakla BIND/PowerDNS gerçek sunucu denemesi](deploy/e2e/dns-kill-matrix/NATIVE-BIND-PDNS-CURRENT-SOURCE-SMOKE-20260927.md), Debian üzerinde yönetilen BIND'den Arch üzerinde panelsiz PowerDNS'e ilk aktarımı doğruladı. SOA, NS ve seçili A yanıtları UDP/TCP üzerinden iki konuk yeniden açılmadan önce ve sonra eşleşti; gerçek Panel ve Agent birimleri kapalı kaldı. Paket yerel ve imzasızdı. Sonraki kayıt değişiklikleri, ters topoloji, kurulu sunucular ve güncelleme kurtarması bu denemede doğrulanmadı; P0.4/P0.5 açık.
- [Geçici PowerDNS birincil/BIND ikincil gerçek DNS denemesinde](deploy/e2e/dns-kill-matrix/evidence/pdns-master-bind-20260928/contract.json), Debian PowerDNS 4.9.17 üzerinde `MASTER` üye ve katalog, panelsiz Arch BIND'e aktarıldı; iki taraf UDP/TCP'de aynı SOA/A yanıtlarını verdi. Ayrı [yerel geçiş ölçümü](deploy/e2e/dns-kill-matrix/evidence/pdns-native-transform-20260927/contract.json), PowerDNS'in SOA/seri, `CATALOG-HASH`, WAL/SHM ve farklı katalog PTR adını ürettiğini gösterdi. Üreticiye göre AXFR ayrıştırıcısının odak testleri var; ürün geçişi, yeniden açılış ve bağımsız geri alma denenmedi. Eşlenik birincil kurulum kapısı kapalı.
- DNS çiftinde PowerDNS birincil yolu, Agent değişikliği başlamadan kurulumda ve motor ön izlemesinde engelleniyor. [Başarısız yerel denemede](deploy/e2e/dns-kill-matrix/NATIVE-PDNS-BIND-PEER-STAGE2-20260927.md) PowerDNS üretici kataloğu değişmiş bulundu; [PowerDNS belgesi](https://doc.powerdns.com/authoritative/catalog.html) otomatik seri artışını açıklıyor. V1 bu canlı hedefi veya güvenli geri almayı kanıtlayamıyor. V3 durum/sahiplik ve işlem günlüğü kodu ile başlangıç öncesi korumalı geri alma eklendi. Kullanıcının onayladığı tamamlanmış günlük arşivleme kaynak kodda uygulandı; arşivin kalıcı yazılması ve etkin kaydın kaldırılması için kesinti testleri var. Aynı işleme bağlı Agent aracılı başlangıç sonrası ileri kurtarmanın kontrol noktası ve sahip değişikliği odaklı paket testleri var; ürünün gerçek kesinti ve yeniden açılış kabulü açık. V3 kanıtı için açık bir Agent/kurtarma uyumluluk sözleşmesi bulunana kadar uygulama değişimi de reddediliyor; eski kanıt politikası işareti yeterli sayılmıyor. Yeni şemalar yayımlanmadı veya kurulu sunuculara uygulanmadı; güvenlik kapısı kapalı ve P0.4/P0.5 açık.
- [Yeni PowerDNS birincil V3 gerçek sistem kesinti denemesinde](deploy/e2e/dns-kill-matrix/evidence/pdns-v3-native-20260928/README.md) başlangıçta yapılandırılmamış Debian birincil ve panelsiz Arch BIND ikincil kullanıldı. `target-enable-intent` kesintisinden sonra ayrı sürecin aynı isteği ilk kurtarma girişiminde tamamladığı işlem defterinde `succeeded` olarak kaydedildi; V3 günlük arşivlendi ve yönetim devre dışıyken birincil yeniden açıldıktan sonra iki uç UDP/TCP üzerinden yetkili yanıt vermeyi sürdürdü. Önceki deneme, Debian PowerDNS SOA adlarını düzeltirken katalog NS içeriğini değiştirmediği için güvenle bilinmiyor durumunda kaldı; sonraki makine uzlaştırması geçmiş hata kaydını korudu. SSH ile başlatılan iki kesinti `255` bildirdi; üçüncü temiz denemede systemd bağımsız olarak `Result=signal`, `ExecMainStatus=9` (SIGKILL) kaydetti ve aynı istek yine ilk kurtarma girişiminde tamamlandı. Kabuk çıkışı `137` iddia edilmiyor. Var olan BIND birincilden PowerDNS geçişi, sahip düzenlemeleri, bağımsız geri alma, diğer kesinti noktaları ve çiftin tüm yaşam döngüsü açık; genel kurulum kapısı ve P0.4/P0.5 kapanmadı.
- [Yeni PowerDNS V3 başlangıç öncesi bağımsız geri alma](deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md), geçici Debian sunucuda gerçek SIGKILL sonrasında ayrı ve doğrulanmış sahip CLI ile bir kez tamamlandı. Önceki iki temiz deneme, etkiden önce güvenli ret vererek kapalı/maskeli birimin cgroup alanı ve yalnız yerel DNS dinleyicisi hakkındaki aşırı katı denetimleri ortaya çıkardı; odak testleriyle düzeltildi. Başarılı deneme PowerDNS'i önceki kapalı durumuna döndürdü, aday veritabanı ile etkin günlüğü kaldırdı ve geri alma kararını yönetim kapalıyken yeniden açılış boyunca korudu. Eski kurulum isteğinin kaydı doğru biçimde başarısız kaldı. Gerçek sistem denemesinde yeniden açılış sonrası tam istek sorgusu geçici kilit yokluğunda çalışmadı; günlük yokken yalnız kalıcı sonuç kaydını okuyan yeni yol, odak testlerini ve önceki gerçek geri almanın kanonik kaydıyla ayrı bir geçici Debian yeniden açılış denemesini geçti. Bu, üretici/ters işlemin veya kurulu kurtarma paketinin yeniden denenmesi değildir. Sonraki kesintiler, motor göçü ve genel kullanım izni açık. P0.4/P0.5 kısmi kalır.
- İlerideki ayrı motor geçişi açığı: mevcut eşli yönetilen BIND birincilini PowerDNS birincile taşımak, yeni V3 veya tek sunuculu V4 yoluyla kanıtlanmış değildir. Bu geçiş için ayrı eşli kaynak/hedef kanıtı ve kurtarma denemeleri gerekir. Yeni PowerDNS birincil/BIND ikincil kurulumunun kabulünden ayrıdır.
- İki topolojinin doğrudan kabul yolu: geçici BIND birincil/PowerDNS ikincil çiftinde ilk yetki, tamamlanmış düzenleme, sahibi tarafından kaydedilmiş SSH ile tamamlanmış silme/yeniden ekleme ve yönetim kapalıyken yeniden açılış kanıtı var; sıradan sahip kaydı ve kesinti hücreleri açık. [Yeni PowerDNS birincil/BIND ikincil V3 denemesinde](deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md) gerçek target-enable-intent SIGKILL sonrasında aynı isteğe bağlı ayrı süreç kurtarması, üst bölgesi yetkili bir alt alan için tamamlanmış ekleme/düzenleme/silme/yeniden ekleme ve yönetim kapalıyken iki yerel DNS hizmetinin yeniden açılış sonrası yanıtı kanıtlandı. Bu denemede üst bölgesiz silme, ikincinin yerel yokluk kanıtı için yetkili kayıt olmadan doğru biçimde beklemede kaldı. Genel kurulum/RPC izni, sıradan sahip kaydı, sonraki kesinti hücreleri, sahip düzenlemeleri ve bağımsız ters işlem açık; P0.4/P0.5 ile eşli birincil kapısı tamamlanmadı.
- [Sahibin çalıştırdığı BIND inceleyici kaydı denemesi](deploy/e2e/dns-kill-matrix/evidence/owner-bind-enrollment-20260928/README.md), geçici Debian/Arch sunucularda yerel birincil/ikincil kaydı, sabit komutlu SSH kimlik doğrulaması, yeniden açılışta kalıcılık, açık yetki kaldırma ve yerel BIND hizmetinin devamını doğruladı. Son ikincil durumu yapılandırılmıştır; geçerli silme kanıtı değildir. Aktarılmış katalogla geçerli sorgu, ürünün sunucu anahtarı sabitlemesi, yarım kurulum temizliği/anahtar yenileme, Debian ikincil ve paketleme açık; P0.4/P0.5 kısmi kalır.
- Daha sonraki [sahip kayıtlı PowerDNS birincil/BIND ikincil üst bölgesiz silme denemesi](deploy/e2e/dns-kill-matrix/evidence/pdns-bind-owner-proof-20260928/README.md), sabitlenmiş ürün SSH aktarımını ve aktarılan katalog/yerel BIND bölge gözlemini kullandı. Gerçek V3 Agent silmesi kalıcı tamamlanmış işlem kaydına ulaştı; ikincil yeniden açıldıktan sonra yönetim kapalıyken katalog boş ve bölge yüklenmemişti. Yalnız bu eski deneme açığı kapandı. Ürün paketleme/izin, diğer kesintiler ve bağımsız ters işlem açık; P0.4/P0.5 kısmi.
- [Açık sahip kaydı devam yolu](deploy/e2e/dns-kill-matrix/evidence/owner-bind-resume-20260928/README.md), birebir hazırlanmış BIND inceleyici dosyalarını ve kısıtlı hesabı yeniden kullanıyor. Geçici Arch denemesinde hazırlanmış SSH kesinti durumları tamamlandı, bitmiş kayıt anahtarı değiştirmeden tekrarlandı, açık iptal sonrası yeniden yetki verildi ve sahip SSH düzenlemesi korunarak devam reddedildi. Yerel BIND çalıştı. Bu bir süreç sonlandırma/güç kaybı denemesi değildir; Debian, diğer kesintiler, temizlik/anahtar döndürme ve ürün paketleme açıktır.
- [Debian sahip yetkilendirmesini sürdürme ve arşiv bütünleştirmesi](deploy/e2e/dns-kill-matrix/evidence/owner-bind-debian-20260928/README.md), güncel sıkı hesap denetimleriyle gerçek Debian BIND/OpenSSH üzerinde geçti. Normal CLI, sahibin SSH değişikliğini korudu; anahtar kapalı, BIND aktif kaldı. Kaynak paketleme artık yetkilendirme başlatmadan isteğe bağlı sahip araçlarını taşıyor. Gerçek dist tarifi, gerçek araçlar ve ilgisiz bileşenlerin etkisiz test dosyalarıyla izin, checksum/bozulma ve umask tekrarlanabilirlik kontrollerini geçti. Bu sonuç Debian devam noktalarını ve kaynak arşiv bütünleştirmesini kapatır; tam yetkilendirme, genel kullanım kapısı, imzalı sürüm ve bağımsız geri alma açık kalır.
- Bağımsız yeni-PowerDNS V3 yürütücü denetimi, Linux üzerinde `syscall.Stat_t` döndüren `os.Lstat`/`File.Stat` çağrılarından yanlışlıkla `unix.Stat_t` bekliyordu. Gerçek root sahipli dosya testi, doğru dosyanın reddedildiğini yeniden üretti. Düzeltmeden sonra kurtarma paketinin bütün testleri geçti; değişmiş içerik, yanlış sahip/izin, özel izin bitleri ve sembolik/sabit bağlantılar reddediliyor. Yalnız geçici dosyalar kullanan root testi CI sistemine bağlandı. Şema ve kurtarma kararı değişmedi; bu dosya kanıtı düzeltmesidir, gerçek bağımsız kurtarmanın kabulü değildir.
- [Yönetilen BIND V3 silme işlemi](deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md), yetkili üst bölge sunan, panelsiz ikincille doğrulanmış sonuca ulaştı. Yerel bölge kaldırılması yeniden açılışta korundu. Üst bölge kanıtı yokken silme beklemede kalıyor; yalnız `REFUSED` yokluk kanıtı sayılmıyor.
- [Çift sunucuda target-staged/after-write](deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md), gerçek SIGKILL, aynı isteğin Agent aracılığıyla kurtarılması, ikincile aktarım ve yönetim devre dışıyken yeniden açılışı geçti. Yeniden açılış gözlemleri, özet değeri alınan arıza arşivinden ayrı belgelenmiştir.
- [PowerDNS sahip değişikliği reddi](deploy/e2e/dns-kill-matrix/NATIVE-PDNS-OWNER-EDIT-20260925.md), sonlandırma sonrası sahibin düzenlemesini, günlüğü ve çalışan DNS hizmetini korudu. Bu sınırlı ret kanıtıdır; her değişiklik anındaki yarış güvenliği veya bağımsız geri alma değildir.
- Tek sunucuda yönetilen BIND üzerinden başlangıçta kapalı ve devre dışı PowerDNS hedefine geçiş için V4 kanıtı ve korumalı sahip CLI geri alması kaynak kodda var. Tam aday dosya canlı veritabanı adına taşınmış fakat PowerDNS henüz başlamamış ve servisi kayıtlı devre dışı durumda kalmışsa, kaynak durduruldu aşamasında aynı dosya özel dizine geri alınabiliyor. Kaynak kod, servis etkin fakat henüz çalışmıyorsa bunu yalnız kalıcı V4 etkinleştirme niyetiyle kabul ediyor ve önce servisi kayıtlı devre dışı duruma getiriyor. Üretici henüz bağlı değil; gerçek sistem kesinti/yeniden açılış denemesi ve PowerDNS başladıktan sonraki geri alma kabulü yok. [Kesin kapsam](docs/DNS-ENGINE-ARTIFACT.md#standalone-bind-to-powerdns-v4-recovery-code-scope-2026-09-27). P0.4 açık.
- [DNS envanterinde](deploy/e2e/dns-kill-matrix/README.md) 510 ham birleşim var: 268 uygulanabilir/çalıştırılabilir, 242 gerekçeli kapsam dışı. **268, geçen test veya hazır deney ortamı sayısı değildir.** Yönetilen BIND ve eski ikincil kaynak hazırlayıcıları, çift sunucunun sonraki aşamaları ve çalıştırılmamış hücreler açık. Aynı hücrenin tekrarı aşama kapsamını artırmaz.
- 12 Eylül farklı motorlu çiftin silme iddiası düzeltildi: olumsuz DNS yanıtı yeterli kanıt değildi. Yeni BIND/BIND başarısı, geçmiş BIND/PowerDNS kabulünü kapatmıyor.

- 29 Eylül 2026: `e9d1019d` kaynağının salt-okur denetimi [DNS kurtarma kabul kütüğünü](docs/DNS-RECOVERY-ACCEPTANCE.tr.md) üretti. Kurtarma sözleşmesi olmayan tek erişilebilir yol bulundu: tek sunucuda veya çiftin ikincilinde çalışan BIND'i PowerDNS'e çevirmek eski V1 günlüğünü yazıyor, yalnız Agent'ın kendi ters işlemine dayanıyor ve gerçek sistemde hiç denenmemişti. [D-026](docs/DECISIONS.tr.md) bunu bu sürümde desteklenmiyor ilan etti; Panel, kurulum ve Agent artık değişiklik başlamadan reddediyor (`bind_source_pdns_switch_unsupported`), ekranda neden ve sonraki adım yazıyor. Var olan günlüğün açılış kurtarması bu kapıdan geçmez. İlk kurulumlarda Agent'ın aynı işlemle kurtarması kabul edildi. Kill-matrix düzeneği boş tek sunucu PowerDNS ve boş tek sunucu BIND target-verified hücrelerini hazırlayacak biçimde genişletildi; bu hazırlıktır, kanıt değildir; 268 çalıştırılabilir sayısı değişmedi. `cmd/panel` ve yeni `cmd/agent` testleri yerelde geçti; `cmd/agent` paketinin tamamı yerel WSL konuğunda hiçbir kullanıcı/grup düzeninde temiz koşmuyor ve `e9d1019d`'de de aynı biçimde düşüyor; tam doğrulama itilmemiş dal için CI işi olarak açık.
- 29 Eylül–1 Ekim 2026: kesinti matrisinin 4–12. grupları ve iki geçici CelikPanel sunucusunda yedi ürün akışı çift koşusu 2. maddeyi adı belli sınırlarla kapattı ([kütük](docs/DNS-RECOVERY-ACCEPTANCE.tr.md)). [7. çift](deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md) BIND/BIND, BIND/PowerDNS ve PowerDNS/BIND düzenlerinde her adımı geçti: sahibin `dns-peer-enroll` kaydından sonra ikincilde kanıtlanan bölge silme, yeniden ekleme, panel ve Agent kapalıyken DNS yanıtlamaya devam ederken yeniden açılış ve yönetimin dönüşü dahil; [12. grup](deploy/e2e/dns-kill-matrix/evidence/batch12-zero-zone-complete-20261001/README.md) sıfır bölgeli boş PowerDNS birincil hücrelerini, kayıttan sonra sürdürülen ebeveynsiz silmeyle tamamladı. Yalnızca bu gerçek sistem koşularının bulduğu on bir ürün kusuru yol boyunca kaynakta kapatıldı; her biri kanıtıyla kütükte. Boş çift PowerDNS birincili kapısı ölçülen kapsam içinde ana hatta açık ([D-028](docs/DECISIONS.tr.md)). P0.4 ve P0.5 kısmi kalıyor: bölge eşitleme içinde SIGKILL yok, dizüstü ana makinede yalnızca test amaçlı lisansla hücre başına tek çalıştırma, damga kabulü ve kanıt süre aşımı gerçek sistemde görülmedi, kurulu sunucuya dokunulmadı.

### Sıradaki işler ve bağımlılık sırası

| Sıra | Mevcut plandaki iş | Bitiş kanıtı |
|---|---|---|
| 1 | P0.4: [DNS kurtarma kabul kütüğü](docs/DNS-RECOVERY-ACCEPTANCE.tr.md) (29 Eylül 2026), ürünün başlatabildiği her DNS motoru değişikliğini geçti / adı belli eksik / desteklenmiyor durumlarından birine bağlar. [D-026](docs/DECISIONS.tr.md) ile çalışan BIND→PowerDNS geçişi artık Panel, kurulum ve Agent'ta her topolojide reddediliyor (`bind_source_pdns_switch_unsupported`; bileşen testleri geçti); ilk kurulumlarda Agent'ın aynı işlemle kurtarması kabul edilen sözleşme. Debian'da korumalı sahip CLI kurtarması için dört sınırlı gerçek sistem kanıtı var: harici PowerDNS devralması, PowerDNS-BIND geçişi geri alması, çalışan-BIND devralması geri alması ve yeni PowerDNS V3 başlangıç öncesi ters işlemi. 29 Eylül 2026'da dört boş kurulum hücresi gerçek sistemde koştu ([kanıt](deploy/e2e/dns-kill-matrix/evidence/fresh-install-20260929/README.md)): boş PowerDNS hedef başladıktan sonra ve boş BIND hedef başladıktan sonra (Debian ve Arch) aynı istekle kurtarıldı; boş PowerDNS hedef başlamadan önce kesilince **düştü** — geri almanın durmuş hedef kanıtı hiç başlamamış, kurulumun maskelediği birimi reddediyordu. `1c336f6d` ile düzeltildi; `target-staged` ve `intent` hücrelerinin düzeltilmiş kaynakla [yeniden koşusu](deploy/e2e/dns-kill-matrix/evidence/fresh-install-rerun-20260929/README.md) açılışta geri aldı ve aynı isteğin yeniden denemesiyle tamamlandı. Sahip komutu yönlendirmesi (D-024) kaynakta kapandı: durum komutu ve Agent ret metinleri tam sahip komutunu ve istek kimliğini söylüyor ya da hiçbirinin uygulanmadığını açıkça belirtiyor. Bu iş açık bir kod eksiğini ortaya çıkardı: yeniden açılan Agent kirasını bıraktıktan sonra (`released-undecided`) `recover-dns-bind-switch`, `recover-dns-bind-adoption` ve `recover-dns-pdns-adoption` günlüğü reddediyor; yani Agent çalışırken yarıda kesilen PowerDNS→BIND geçişinin veya devralmanın kabul edilen sahip komutu yok. Önceki gerçek sistem kanıtları Agent kapalıyken alınmıştı. Kabul kuralı (yalnız Agent'ın bilerek bıraktığı iş; şema değişikliği yok, defter yeniden yazılmıyor; doğru metin okuma anında hesaplanıyor) ve denetleyicinin `--owner-inverse-after-restart` akışı kaynakta. Gerçek sistem sonucu: `7ad24282` üzerindeki ilk koşu güvenlik denetimleri geçerek düştü (sahip komutu hiç başlamamış, koruma maskeli BIND hedefini reddetti), `411398d9` ile düzeltildi ve `target-staged` ile `intent` hücrelerinin [yeniden koşusu](deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-rerun-20260929/README.md) Agent yeniden başlamış ve çalışırken geçti — PowerDNS baştan sona aynı süreçle hizmet verdi, BIND hiç başlamadı. Ardından kaynak durdurulduktan sonraki kesintiler (`source-stopped`, `target-started`) akışın kritik çeşidinde [geçti](deploy/e2e/dns-kill-matrix/evidence/owner-inverse-critical-20260929/README.md): PowerDNS tek yetkili olarak yeniden hizmet verdi, bölge seri numarası değişmedi; ölçülen PowerDNS kesintisinin üst sınırı 21,6 sn ve 9,4 sn oldu, bu hücrelerde DNS yapısı gereği kesintisiz değildir. **1. madde 29 Eylül 2026 itibarıyla adı belli sınırlarla kapandı** (aynı günkü sahip kararı): tek sunucuda boş BIND, boş PowerDNS, PowerDNS→BIND ve harici PowerDNS devralması, hedef başlamadan önce ve sonra aynı işlemle kurtarma için gerçek sistem kanıtına sahip; kurtarma sözleşmesi olmayan yollar kodda reddediliyor. [Kütükte](docs/DNS-RECOVERY-ACCEPTANCE.tr.md) adlarıyla 2. maddeye taşınanlar: bütün çiftli topolojiler, Agent çalışırken BIND ve PowerDNS devralması (yalnız bileşen testi), durmuş BIND devralma ve BIND yeniden kurulumu (gerçek sistem denemesi yok), V2 before-write ve rolled-back hücreleri, yeniden açılış hücreleri, yeniden çalıştırma çıkış kodu ve diskte kalan artıklar. 1. maddenin kapanması P0.4'ü kapatmaz. Boş çift ikincil hücreleri, düzenekte olmayan panelsiz yerel birincil eşi gerektiriyor; 2. maddeye taşındı ve açık kalıyor. 1. madde/P0.4 tamamlanmamıştır. | [Kabul kütüğü](docs/DNS-RECOVERY-ACCEPTANCE.tr.md); [Çalışan-BIND devralması](deploy/e2e/dns-kill-matrix/NATIVE-BIND-ADOPTION-OWNER-CLI-20260927.md), [PowerDNS-BIND geçişi geri alması](deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md), [PowerDNS devralması](deploy/e2e/dns-kill-matrix/NATIVE-PDNS-PROTECTED-OWNER-CLI-20260926.md) ve [yeni PowerDNS V3 başlangıç öncesi ters işlem](deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md). Deneme sınırları ilgili raporlarda yer alır. |
| 2 | P0.4/P0.5: eksik kaynak/ikincil deney ortamlarını ve üst bölge ya da uzak panel gerektirmeyen uygulanabilir silme doğrulamasını tamamla. 1. maddeden taşınanlar (29 Eylül 2026, [kütükte](docs/DNS-RECOVERY-ACCEPTANCE.tr.md) adlarıyla): boş çift ikincil hücreleri için panelsiz yerel birincil eş, Agent çalışırken devralma geri almaları, durmuş BIND devralma ve BIND yeniden kurulum hücreleri, V2 before-write ve rolled-back hücreleri, yeniden açılış hücreleri. **2. madde 1 Ekim 2026 itibarıyla adı belli sınırlarla kapandı** ([kütük bölümü](docs/DNS-RECOVERY-ACCEPTANCE.tr.md)). Kesinti matrisi 4–12. grupları koştu (panelsiz BIND ve PowerDNS birincil eşler; başlamadan önce ve sonra kesilen boş BIND ve PowerDNS ikincilleri, yönetim kapalıyken yeniden açılışlar; durmuş BIND devralma; V2 before-write ve rolled-back hücreleri; genel RPC üzerinden tek bölgeli ve sıfır bölgeli boş çift PowerDNS birincili, tutulan sahip düzenlemeleri, Agent'ın bıraktığı işin sahip komutuyla bitirilmesi, sahip kaydından sonra sürdürülen ebeveynsiz silme) ve ürün akışı çift sürücüsü iki geçici CelikPanel sunucusunda yedi kez koştu; [7. çift](deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md) BIND/BIND, BIND/PowerDNS ve PowerDNS/BIND düzenlerinde her adımı geçti: kurulum, bölge ekleme, kayıt düzenleme, sahip kaydından sonra ürünün kanıtıyla bölge silme (`dns-peer-enroll`, BIND ve PowerDNS ikincilleri), yeniden ekleme, panel ve Agent kapalıyken DNS yanıtlamaya devam ederken yeniden açılış ve yönetimin dönüşü. Yalnızca gerçek sistem koşularının bulduğu on bir ürün kusuru yol boyunca kaynakta kapatıldı (sıfır bölgeli katalog denetimi, sihirbaz durumları ve lisans yenileme, müşteri arşivindeki kanıt, Arch'ta rndc anahtarı, PowerDNS bildirim bağlantı noktası, yönetilen ikincilde yerel katalog aktarımı ve denetleyici nedenleri, yalnızca DNS'li alan adında posta aşaması, yönetilen PowerDNS ikincilinde sahip denetleyicisi, BIND birincil planının kaynak durumu, sahip değişikliği sanılan daemon damgası, dalga sınırında atılan olumlu kanıt). Bu kanıtla boş çift PowerDNS birincili kapısı ölçülen kapsam içinde ana hatta açık ([D-028](docs/DECISIONS.tr.md)). Kütük satırları 3, 5, 6, 12 ve 17 sınırlarıyla GEÇTİ; 8 ve 14 EKSİK kalıyor; adı belli sınırlar (hücre başına tek çalıştırma, dizüstü ana makine, yalnızca test amaçlı lisans, bölge eşitleme içinde SIGKILL yok, damga kabulü ve kanıt süre aşımı gerçek sistemde görülmedi, güncelleme tetikleyicisi olmayan sürüm-1 BIND ikincili, müşteri arşivindeki düzenek) kütükte. 2. maddenin kapanması P0.4 ya da P0.5'i kapatmaz ve kurulu panel güncellemesine yetki vermez. | Desteklenen gerçek birincil/ikincil birleşimleri ekleme/düzenleme/silme, yüklenen yerel bölge durumu ve yeniden açılışı kanıtlar; belirsizlikte aynı işleme yönelik uygulanabilir kurtarma yolu sunulur. Deney altyapısı açığı kapsam dışı sayılamaz. |
| 3 | P0.1–P0.5: kalan uçtan uca güncelleme, erişim, şema, TLS/devreye alma ve hizmet matrisini kapat. **3. madde 1 Ekim 2026 itibarıyla adı belli sınırlarla kapandı** ([sözleşme bölümü](docs/RESILIENCE-CONTRACT.tr.md#yol-haritası-3-madde-durumu-2026-10-01-itibarıyla-adı-belli-sınırlarla-kapandı-p01p02p03p05)). Geçici Debian 13 ve Arch konuklarında sahibin başlattığı güncelleme koşuları upd2–upd6 (upd1 hiçbir güncellemeye ulaşmadı) şunları ölçtü: oturum açmış sahip olarak Panel'in güncelleme başlatma API'siyle kurulan iyi aday; tamamlanmadan önce başarısız olan adayın, ikinci bir arızadan sonra da (Debian'da VM sıfırlama, Arch'ta SIGKILL) otomatik olarak önceki sürüme dönmesi; tamamlandıktan sonra başarısız olan adayın üç kez yeniden denenmesi, nedeni korunarak duraklaması ve sahibin yazdırılmış yeniden denemesinden sonra bitmesi; cron'un hiç kesilmemesi ve sitenin yalnız enjekte edilen VM sıfırlaması çevresinde kesilmesi (en çok yaklaşık 22 sn); Panel ve Agent kapalıyken yeniden açılış boyunca site, veritabanı, cron ve güvenlik duvarının hizmet vermeye devam etmesi (SMTP Debian'da) ve yenileme zamanlayıcısının durumunu koruması. Bu koşuların bulduğu ürün kusurları kaynakta kapatıldı. 3. maddenin kapanması hiçbir P0 işini kapatmaz: deney imzası ve loopback kaynağı, az tekrar ve tarayıcı yok, Ubuntu yok, güç kaybı yok, tamamlandıktan sonra geri alma yok, Panel durmuşken Panel'in adresinde canlı durum yok ve gerçek sistemde tetiklenmeyen yollar sözleşme bölümünde adlarıyla kalır. | Kullanıcının arayüzden güncelleme başlatması, başarısız aday, otomatik geri alma, kurtarmadaki ikinci arıza, kimlik doğrulamalı yönlendirme ve korunan hizmetler tek işlemde doğrulanır. İddia edilen her platform/sürüm birleşiminin kanıtı saklanır. |
| 4 | Kesin adayın sürüm incelemesi ve kısa kullanıcı test yolu. **Aday incelemesi tamamlandı; adayın kesin kodu (`f6cdd5a0`) 2–3 Ekim 2026'da güncelleme matrisinin bir tam koşusundan geçti ve yayımlama sahibi bekliyor** ([sürüm notları](docs/RELEASE-NOTES-v0.1.0-alpha.81.tr.md)). Müşteri arşivi artık düzeneği, test betiklerini ve kanıtı taşımıyor; imzalama adımı bunları taşıyan arşivi reddediyor. Güncelleme yolunun salt-okur incelemesi yedi kusur buldu, yedisi de kaynakta düzeltildi (bileşen testleri; kalan açıklar sözleşme bölümünde listelenir); en önemlisi, v0.1.0-alpha.80'in başlattığı işçinin durum kaydı yazmamasıydı, bu yüzden yeni yönlendirme ilk yükseltmede hiç görünmüyordu. Bu ilk yükseltme sonra, durum başına tek çalıştırmayla, test lisansıyla yeniden derlenen alpha.80 kaynağından (imzalı arşivden değil) Debian 13 ve Ubuntu 24.04'te ölçüldü (iyi aday, sahibin devam ettirmesi, alpha.80'e dönen kusurlu aday); Arch'ta alpha.80'den ölçülmedi. Ubuntu, boştaki bir paket yardımcısının kurulumu engellediğini ve güncelleme başlatmayı reddettirebildiğini ortaya çıkardı; ilk düzeltme gerçek sistemde tutmadı, düzeltilmişi ölçüldü. Sahip için açık olanlar: sürüm numarası; yalnız root ile çalışan paketleme testleri (18), geçici bir makinede veya CI'da; üretim imzalaması ve uyum denetimi; satıcıya ait yayımlama araçlarının arşivde kalıp kalmayacağı; sahip testi; kurulu panellerin her güncellemesi. İnceleme düzeltmelerinden sonra Panel'in posta başlangıç adımlarını ertelenmiş yeniden denemesi eklendi ve ölçüldü, ardından tüm matris son kodda bir kez daha koşuldu (upd13: 20 hücre, aday kusuru yok). Adı belli sınırlar sürüm notlarında; hiçbir P0 işi kapanmadı. **9 Ekim 2026 durumu:** sürüm `v0.1.0-alpha.81` olarak yayımlandı (etiket `a0beb726` üzerinde, 4 Ekim 2026). Sahibin bildirdiği ve burada bir dosyada kayıtlı olmayanlar: yayımlama adımları (#204 numaralı çekme isteğinin birleştirilmesi, etiket, portal yayını), altı imzalı dosyanın doğrulanması ve kurulu iki panelin (biri Ubuntu 24.04, biri Debian 13) 8 Ekim 2026'da panelin kendi güncelleme ekranından güncellenmesi. | Gerekli kabul işleri kapanır veya bilinçli olarak dar kapsamlı sürümün açık sınırları belirtilir. İmzalı dosyalar ve kurtarma uyumu doğrulanır; kurulu panel güncellemesini yalnız kullanıcı başlatır. |
| 5 | `v0.1.0-alpha.82` adayının sürüm incelemesi (yukarıdaki "Sürüm durumu — 9 Ekim 2026" bölümüne bakın). **Aday; yayımlanmadı; numara sahibin kararı.** 8 ve 9 Ekim 2026'daki dört gerçek sistem koşusu, bir ek ölçüm ve `postconf`'un bir okuması; ayar kayıtlarını, hizmet işlemlerini, istek kimliğini, yayımlanmış v0.1.0-alpha.81'den güncellemeyi (`cfa329676` hâliyle) ve yeni kurulmuş konuklarda son düzeltmeleri (`557b554eb` hâliyle, Arch'ta PHP siteleri dahil) ölçtü; geçmeyen tek denetim, Ubuntu 24.04'te Postfix durdurulduktan sonraki not, düzeltildi ve yeniden ölçüldü. Son koddaki `postconf` düzeltmesi (`67b62cc0f`) gerçek programın bir okumasına ve bileşen testlerine dayanır. Güncelleme matrisi 9 Ekim 2026'da bu son kodda, beşinci bir koşuda yinelendi: on hücrenin tamamı, her biri bir kez ve konuk diskleri bellekteyken, üçüncü koşunun ölçtüğü sonuca ulaştı. Son kodda çalıştırılmayanlar: yeni kurulum ve gerçek bir sistemde düzeltilmiş posta sertifikası yolu. Sahip için açık olanlar: sürüm numarası; bu ikisinin yayımlamadan önce ölçülmesi ya da adı belli sınırlar olarak çıkması kararı; üretim imzalaması ve doğrulaması; geçici bir sunucuda sahip testi; kurulu panellerin her güncellemesi. Sonraki sürüme taşınan: sahibin üretilmiş bir site yapılandırma dosyasındaki düzenlemesinin bir Panel başlangıcında neyle karşılaştığının denetimi. Hiçbir P0 işi kapanmadı ve ilerlemedi. | Kapanış güncelleme matrisi saklanır ([kanıt](deploy/e2e/release-recovery/evidence/set5-20261009/README.md)) ve [sürüm notları](docs/RELEASE-NOTES-v0.1.0-alpha.82.tr.md) hangi denetimin hangi koda dayandığını söyler. Sürüm, açık kalan sınırlarını belirtir. İmzalı dosyalar ve kurtarma uyumu doğrulanır; kurulu panel güncellemesini yalnız kullanıcı başlatır. |

Yeni test eklemeden önce hangi açık kabulün kapanacağı belirtilir.
Geçen deney, ancak ilgili değişiklik veya adı konmuş belirsizlik nedeniyle tekrarlanır.
Kanıt saklanıp kontrol edildikten sonra geçici konuklar durdurulur ve doğrulanmış
geçici diskleri temizlenir; kurtarma malzemesi ve sahibin verisi korunur.

**Bitiş tarihi:** mevcut kanıtla belirlenmiş değil. Kalan uygulama ve deney altyapısı
açıkları varken güvenilir yüzde veya kesin tarih verilemiyor. Bağımsız DNS yolu ve
eksik deney kapsamı doğrulandığında süre yeniden değerlendirilir; bitiş, küçük
commit sayısına değil mevcut kabul koşullarının karşılanmasına bağlıdır.

### Yapay zekâ asistanı aşaması — işlem/kurtarma temelinden sonra

Kullanıcının istediği yapay zekâ asistanı planda: gözlenen durumu açıklayacak,
kullanıcının yetkilendirdiği panel işlerini aynı türlenmiş ve kapsamı sınırlı
işlem API'leriyle yapmasına yardımcı olacak. Planı gösterecek, izinleri koruyacak,
işlem kimliğini kaybetmeyecek ve doğrulanmış sonucu bildirecek. Kabul deneyleri;
izin reddini, kullanılamayan hizmeti, kesilen isteği ve ikinci değişiklik üretmeyen
tekrarı kapsamalı. Sınırsız root, eksik kanıt uydurma, lisans atlama veya kurulu
panel güncellemesi başlatma yetkisi almayacak. Yapay zekâ entegrasyonu kuralları
belli kurtarmanın yerine geçmez; bu incelemede uygulanmış sayılmıyor.

---

## Sürüm Merdiveni

Aşağıdaki sürüm merdiveni önceki aşamaların tarihsel kaydıdır. Geçmiş başarılı
kurulum sonuçları ve mimari değerlendirmeler, 14 Eylül dayanıklılık sözleşmesinin
karşılandığını göstermez; bu kabul çalışması açık kalmaktadır.

Varış noktası: **v1.0 — bir yabancının temiz VPS'e dakikalar içinde kurabildiği,
üzerinde gerçek hosting işi yürütebildiği ve güvenebildiği panel.** Aşağıdaki her şey o yolun taşı.

17 Temmuz güncellemesiyle merdivene üç yeni gereksinim işlendi — muğlak "ileride" değil, basamak basamak:
1. **Panel-içi yardım/ipuçları:** temel taşları v0.2.5'te (mevcut borç kalemlerinin doğal uzantısı olarak),
   kullanıcıya dönük içerik v0.3'te, yardım merkezi + sihirbaz + palet v0.6–0.9'da.
2. **Bayi + müşteri birinci sınıf deneyim:** kapı açan dilim v0.2.5/B1'de, gövde v0.3'te
   (tahsilat yetkisi dahil), `additional_user` gerçek özelliği v0.35'te.
3. **Ücretsiz katmanlı abonelik/plan sistemi:** veri modeli + dönem/iptal/askı makinesi + ödeme defteri +
   bayi havuzu v0.3'te, "her vaat uygulanır ya da silinir" dürüstlüğü v0.35'te, ödeme sağlayıcı kararı
   (iade/chargeback dahil) v0.5'te, self-signup + kötüye kullanım freni v0.6–0.9'da.

### ✅ v0.0 — Devralma *(3 Temmuz 2026)*
~23 bin satır devralındı: mimari sağlam (Panel + root Agent, SQLite) ama güvensiz
(açık TCP agent, SQL injection, kimlik doğrulama yok) ve arayüz sahte veriyle dolu.
Karar verildi: devam, sıfırdan yazma yok.

### ✅ v0.1 — Güvenli Çekirdek + Kanıtlı Golden Path *(3–10 Temmuz 2026 — tarihsel v0.1.0 aşaması)*
Sekiz gün, dört cephe, hepsi push'lu:
- **Güvenlik (Faz 0):** agent Unix socket + token arkasında · oturum kimliği (argon2id) + 2FA/TOTP ·
  SQL injection temizliği · CSRF/başlıklar/hız sınırı · gosec yüksekleri kapandı · sızmış parola etkisiz.
- **Barındırma çekirdeği (Faz 1–3):** domain tipleri (php/statik/node/proxy/yönlendirme) + alt alanlar ·
  gerçek oto-yenilemeli SSL · otoriter DNS (PowerDNS/SQLite eşitleme, DNSSEC, DANE) ·
  tam posta yığını (TLS+SNI, kimlikli gönderim 587/465, DKIM imzalama, sunucu politikası,
  teslim edilebilirlik sağlık ekranı) · veritabanları v2 · tek tık WordPress · cPanel içe aktarıcı v1 ·
  hesaplar (yönetici/bayi/müşteri, planlar, kotalar, yerine-geçme) · haklar + WireGuard VPN ·
  saklama süreli zamanlanmış yedek · denetim günlüğü · güvenlik duvarı (varsayılan-reddet) ·
  servis kaldırma · otomatik güvenlik yaması · yönetilen vendor depoları (PGDG sürüm seçimi).
- **İşletim (Faz 2):** `install.sh` (tek komut → giriş ekranı) · anlık görüntülü `update.sh` · `rollback.sh` ·
  systemd unit'leri · **golden path Ubuntu'da uçtan uca kanıtlı**: temiz kurulum → domain → HTTPS →
  dünyaya cevap veren kendi DNS'i → Gmail INBOX'a DKIM imzalı posta.
- **Ürün ve tasarım:** Plesk yoğunluğunda arayüz, açık/koyu, TR+EN · tasarım sistemi claude.ai/design'da
  (tasarım döngüsü: tarif et → ajan gerçek bileşenlerle çizer → süz → yayınla) · self-hosted marka
  fontları · tüm sayfalarda yeni ölçek · kurulum yolculuklu canlı pano.
- **Alfa çalışma modeli (D-008):** operatör paneli gerçek müşteri gibi sürer; çarptığı her duvar ürün
  düzeltmesi olur. İki günde ~20 gerçek hata bu yolla bulunup yayınlandı.

**Çıkış ölçütü karşılandı:** golden path uçtan uca kanıtlı (Ubuntu) · panel kendi güncellemesini taşıyor · alfa modeli işliyor.

### 🔶 v0.2 — Alfa Tamam: Debian Yeniden-Kanıtı *(tarihsel aşama; güncel öncelik yukarıda)*
Aynı golden path, üretim VPS'inde (Debian 13) **tamamen panel tıklamalarıyla** yeniden kanıtlanacak:
- ✅ Yalnız-panel kurulum (sıfır ek paket) · ✅ PowerDNS panelden kuruldu ·
  ✅ yönetim sayfası dürüst (config görünürlüğü, çalışan onarım)
- ⏳ Sıradaki tıklamalar: otomatik onarım → ilk domain → panel Let's Encrypt sertifikası →
  kayıt operatörüne DS kaydı → web sunucusu + canlı site → posta yığını → **Debian'dan Gmail INBOX**
- Çıktıkça kalan alfa pürüzleri + `autodiscover` (posta istemci otomatik ayarı)
- Dış engel: kayıt operatöründeki alan adı askısı (operatörün işi)

**Çıkış ölçütü:** bir ziyaretçi `https://celikpanel.cloud`'u açabiliyor ve oradan atılan posta Gmail
INBOX'ına düşüyor — yapılandıran her tıklama panelde, hiçbiri kabukta değil.

### 🩺 v0.2.5 — Borç Ödeme (Otopsi reçetesi) + Temel Taşlar
11 Tem 2026 adli denetimi ([AUTOPSY](docs/AUTOPSY.tr.md)) canlı kırıklar ve yapısal borç çıkardı;
karar: **refaktör, rewrite değil.** B0 (kanamayı durdur: ölü TypeID sabitleri, admin-olmayana kırık
Databases sayfası, ölü kod) aynı gün kapandı. Kalanı sırayla: **B1** tek API (v2→v1, kiracı kapsamı
auth'tan, OpenAPI + üretilen istemci) · **B2** route+authz tablosu · **B3** servis bilgisinin tek
sahibi katalog · **B4** UI disiplini (tek Button/fmtBytes/modal) · **B5** golden-path smoke CI.

Üç yeni gereksinimin temel taşları **ayrı iş olarak değil, B1–B5'in doğal uzantısı olarak** bu basamağa döşenir
(sonradan eklenirse hepsi ikinci kez kırılır):
- **B1'e ek — hata sözleşmesi:** tüm hata gövdeleri `{code, message, hint?, action?}` standardına geçer.
  `code` makine-okur sabittir (örn. `DNS_SERVER_REQUIRED`), `hint` i18n anahtarıyla ön yüzde çözülür,
  `action` panel-içi link olabilir ("PowerDNS kur" → /services). Tüm bilinçli retler kodlu: D-009 409'u,
  kota 409'ları, çakışma grupları, entitlement 402'si. Ön yüzde tek `ErrorBanner` bileşeni; hata gövdesi
  OpenAPI şemasında tanımlı — üretilen istemci bir kez doğru doğar.
- **B1'e ek — Databases self-servisinin önü:** sunucu-kaydı admin'de kalır; DB/kullanıcı CRUD uçları
  kiracı kapsamıyla müşteri+bayiye açılır; `nav.ts` geçici admin kilidi kalkar; phpMyAdmin vekili
  sahiplik doğrular. (v0.3'ün gerçek ön koşulu — sert kısıtın nedeni budur.)
- **B2'ye ek — fail-closed rol:** kullanıcı kaydı okunamayan istek boş rolle asla ilerlemez (bugün
  `middleware.go`'da Role='' ile devam ediyor). Route+authz tablosunun üstüne **rol×uç matris testi**:
  `--demo` tohum hesaplarıyla her (uç × admin/bayi/müşteri/anonim) hücresi beklenen 200/403/404'e
  karşı doğrulanır; tabloya kayıtsız uç testte fail eder.
- **B3'e ek — Setup Journey dürüstlüğü:** journey adımları paket varlığından değil, katalogdan gelen
  gerçek servis durumundan (kurulu + etkin + koşuyor) okunur. Saha kanıtı zaten var: Hostinger Arch
  imajındaki uyuyan bind "DNS kuruldu: Done" saydı (16 Tem). Bu senaryo B5 smoke'una regresyon olarak girer.
- **B3'e ek — katalogda tür ayrımı + "kurulu-önce" varsayılan (20 Tem, D-010):** `ManagedService`'e
  `Kind` (service/runtime/tool) ve `Role` alanları girer; php-fpm ve yeni **node** kalemi `Kind=runtime`,
  phpMyAdmin/phpPgAdmin `Kind=tool` olur. Satır çizimi `Kind`'e dallanır ve `Daemonless = len(SystemNames)==0`
  sezgisi silinir (bugün üç ayrı şeyi işaretliyor). **Sürümler satır değil, satırın içinde** (sürüm
  çekmecesi) — liste patlaması kaynağında kesilir. Servisler sayfası "kurulu-önce": "kurulu olmayanı gizle"
  varsayılan AÇIK, kategoriler katlı, arama her zaman tüm katalogda ve ikisini de geçersiz kılar. Ayrı
  `/runtimes` ya da `/apps` sayfası açılmaz.
- **B3'e ek — sürüm birinci sınıf + Node katalogda ilan edilir:** tek agent sözleşmesi
  `Agent.ListServiceInstances(id)` her örnek için Version/Unit/Path/Managed/SizeBytes döner
  (`DetectInstalledPHPVersions` ve `ListNodeVersions` bunun ilk iki uygulaması); `extractVersion` switch'i
  ve `"default"` sentinel'i gider. **Node.js yeteneği zaten kodda var** (`runtime_rpc.go`, `app_rpc.go`)
  ama katalogda ilan edilmiyor — yeni kod değil, görünürlük işi. node kalemi web sunucusunu `Requires` ile
  şart koşar (bugün hiçbir yerde ifade edilmeyen reverse-proxy gereksinimi deklaratif olur).
- **B3'e ek — PHP çoklu-sürüm gerçek olur (Sury):** D-002 "✅ yapıldı" diyordu ama yalnız yarısı yapılmış —
  tespit ve site başına seçim çalışıyor, çoklu sürüm KURULUMU yok (`php-fpm`'de `Repo` tanımlı değil,
  kodda `sury` geçen tek satır yok). PGDG için var olan `ManagedRepo` mekanizması php-fpm'e uygulanır:
  Debian/Ubuntu'da yan yana `php8.x-fpm` panelden kurulur; Arch'ta dürüstçe "tek dağıtım sürümü" denir.
  "Seçici var, seçenek yok" hali biter.
- **B3'e ek — runtime kurulumunun tek adresi + sahiplik defteri:** `AdminNodeInstall` (HostingTypePanel)
  silinir; kur/kaldır yalnız Servisler'deki sürüm çekmecesinde (sürüm uçları parametreli — Hestia 5050'ye
  düşülmez). Serbest semver kutusu kalkar, agent LTS listesini çeker (3-5 adlandırılmış seçenek).
  "Sistem yorumlayıcısı" kaçağı kaldırılır — panel yalnız kendi kurduğuyla çalışır. `site_runtimes`
  defteri + `RuntimeUsage`/`Dependents`: kullanımdaki sürüm/servis kaldırılamaz, B1 sözleşmesiyle kodlu
  ret (`RUNTIME_IN_USE`, `SERVICE_HAS_DEPENDENTS`) + engelleyen site listesi döner (bugün 40 site
  kullanırken php-fpm tek tıkla kaldırılabiliyor).
- **B3'e ek — proje tipi tek kaynak + Node oluşturmada seçilebilir:** `CreationProjectTypes` (3 tip) ile
  `validProjectTypes` (5 tip) tek `ProjectTypes` tablosundan türetilir. Add Domain'e "Node.js Uygulaması"
  kartı gelir (alan adı + sürüm + başlangıç komutu; port otomatik). Ön-denetimler tablodan okunur; node
  için de web sunucusu şartı **kaydetmeden önce** kodlu retle döner — bugünkü "PHP'de düğmeyi kapat,
  Node'da kaydet ve agent'ta patlat" asimetrisi kod düzeyinde imkânsızlaşır. Çalışan uygulamalar Node
  runtime satırının altında sayılır ("3 uygulama · 1 hatalı") ve domain'e link verir.
- **B4'e ek — yardım katmanının atomu:** `ui.tsx`'e tek Tooltip/InfoTip bileşeni (HelpCircle + i18n'li
  açıklama, klavye erişilebilir); mevcut 6 Info callout ve ≥10 kritik "ne bu?" alanı (DNSSEC DS, DKIM,
  catch-all, SNI…) taşınır. Yeni ipucunun tek bariz yolu bu bileşendir.
- **B4'e ek — i18n disiplini:** JSX'te çıplak string yakalayan lint (App.tsx'teki "Coming soon..." gider;
  vsftpd yer tutucusu ya dürüst i18n'li EmptyState olur ya nav'dan düşer) · en.ts/tr.ts anahtar eşitliği
  kontrolü (`tools/check-i18n`) CI'da — eksik anahtar sessizce İngilizce'ye düşemez.
- **B5'e ek — framework adı CI kapısı (D-011):** Go kaynaklarında
  `laravel|symfony|django|nextjs|ghost` grep'i sıfır eşleşme vermeli (i18n dizeleri hariç). Kural
  Markdown'da kalırsa iki yıl dayanmaz; enum sabiti/DB kolonu/API değeri/systemd unit adı bir kez
  framework adı taşıdığında katalog örtük olarak doğar ve DB'de kalıcılaştığı için geri alınamaz.
- **Sürüm tekliği + CHANGELOG:** annotated git tag (ilk aday v0.2.0) · sürüm `-ldflags` ile iki binary'ye
  gömülür, `/api/v1/panel/version`'dan servis edilir, Layout.tsx'teki sabit "v0.1.0" silinir · Keep-a-Changelog
  biçiminde CHANGELOG.md + CHANGELOG.tr.md başlar; `update.sh` çıkışında "değişiklikler: CHANGELOG.md" basılır.

**Çıkış ölçütü:** AUTOPSY B1–B5 kapalı · rol×uç matrisi CI'da koşuyor ve hiçbir uç anonim/boş-rol için
200 dönmez · tüm 4xx ön-denetimleri kodlu, ErrorBanner çeviriyor · `panel --version`, UI alt bilgisi ve
git tag aynı diziyi söylüyor · web'de i18n dışı kullanıcıya görünür İngilizce string sayısı 0 (lint CI'da) ·
müşteri rolüyle DB oluştur → phpMyAdmin'e gir → başkasının DB'sine erişim 404 (üçü de B5 smoke'unda) ·
**temiz sunucuda Servisler sayfası en çok 4 satır** (kurulu olmayan hiçbir kalem çizilmez) · Node sürümü
kurmanın panelde tek yolu Servisler'dir (`AdminNodeInstall` kod tabanında yok) · Add Domain'den tek formda
çalışan Node sitesi kuruluyor ("önce statik kur sonra tipi çevir" adımı yok) · Debian'da panelden ikinci bir
PHP sürümü kurulup bir siteye atanıyor (Sury), Arch'ta aynı ekran dürüstçe "tek dağıtım sürümü" diyor ·
kullanımdaki PHP sürümünü/servisi kaldırma denemesi kodlu retle dönüyor ve engelleyen site listesini
gösteriyor · proje tipi listesi tek dosyada; her ölçüt iki test sunucusunda doğrulanmış.
**Sert kısıt: v0.3, B1 bitmeden başlayamaz.**

### 🚨 v0.2.6 — Emanet Edilebilirlik Zemini *(yeni basamak, 25 Tem 2026)*
Yol haritası hazırlanırken yapılan kod denetimi **üç canlı veri-kaybı/yalan kusuru** buldu. Bunlar
sıradaki her özelliğin önüne geçer: bir panel, müşterinin sitesini emanet edilebilir olmadan satılamaz.
Üçü de kodda doğrulandı, tahmin değil.

**Zeminin üç deliği:**
1. **"Tam yedek" veritabanını içermiyor.** `createFullBackup`, kendi yorumuyla birlikte dosya
   yedeğine düşüyor (*"For now, just backup files"*). Riskli bir değişiklik öncesi "Tam yedek" alan
   operatör, geri yüklediğinde veritabanını geri alamaz — güvenlik ağı kılığında sessiz veri kaybı.
   Aynı sebeple `full_` arşivinin geri yüklenmesi de yalnız dosyaları döndürür.
2. **Geri yükleme yanlış veritabanının üzerine yazabiliyor.** Hedef ad, dosya adından alt çizgiyle
   bölünüp ilk parça alınarak çıkarılıyor: `wp_site1` veritabanının yedeği `wp` adlı veritabanına
   dönüyor. Alt çizgi veritabanı adlarında yaygın olduğu için bu, **başka bir müşterinin verisinin
   üzerine yazmak** demektir.
3. **Kurulum, servisin başlayıp başlamadığına bakmıyor.** `systemctl enable --now` sonucu yutuluyor
   ve ardından koşulsuz "kuruldu" deniyor. Anayasanın birinci kuralı ("kurulunca çalışır") tam da
   kurulum anında ölçülmüyor.

**Zeminin dört yapısal eksiği:**
4. **Şablon düzeltmeleri eski sitelere ulaşmıyor** (config drift, AUTOPSY C bölümü). Kapatılmış iki
   güvenlik bulgusu — `.env`'in düz metin sunulması ve PHP kaynağının indirilmesi — düzeltmeden ÖNCE
   oluşturulmuş her sitede hâlâ açık. "Tüm vhost'ları yeniden üret" yolu yok.
5. **Dört site ayarı sunucuya hiç gitmiyor.** Belge kökü, www/https yönlendirmesi, HSTS/zorunlu-HTTPS
   ve alan takma adları veritabanına yazılıyor, nginx'e ulaşmıyor. Operatör ayarı değiştiriyor, ekran
   onaylıyor, sunucu eskisini sunmaya devam ediyor.
6. **SSL kaldırmak vhost'u yenilemiyor** — bir sonraki yeniden yüklemede patlamaya hazır yapılandırma
   bırakıyor.
7. **Apache koltuğu alıyor ama Apache yazıcısı yok.** Tek vhost üreteci nginx'e ait
   (`internal/services/nginx_generator.go`, şablon dizini yalnız `nginx/`). Apache kurmak 80 portunu
   karartır. Yazıcı yazılana dek satır dürüstçe reddedilmeli.

**Çıkış ölçütü:** "Tam yedek" arşivi açıldığında içinde veritabanı dökümü VAR ve her arşiv kendi
içindekiler dosyasını taşıyor · geri yükleme hedefi dosya adından tahmin edilmiyor, içindekiler
dosyasından okunuyor (alt çizgili ad iki sunucuda da doğru veritabanına dönüyor) · geri yükleme
öncesi otomatik anlık görüntü alınıyor · kurulum, başlatma başarısızsa "kuruldu" demiyor ve günlük
kuyruğunu gerekçe olarak gösteriyor · panelden değiştirilen dört site ayarı sunucuda ölçülebiliyor ·
SSL kaldırıldıktan sonra `nginx -t` geçiyor · tek vhost yazıcı kaldı ve "tüm siteleri yeniden üret"
düğmesi eski siteleri de düzeltiyor (iki eski güvenlik bulgusu eski sitelerde de kapanıyor) ·
Apache satırı yazıcı yokken kodlu retle kapalı · CI gerçek bir panel açıp bu ölçütlerin duman
betiklerini koşuyor. **Sert kısıt: v0.3 bu basamak bitmeden başlayamaz.**

### v0.3 — Çok Kiracılı Gerçeklik
Birden fazla kiracıya utanmadan satabilmek. Dört ayak: müşteri ve bayi kendi başına yaşayabiliyor;
plan/abonelik makinesi "ücretsiz katman + ücretli plan"ı **girişten çıkışa** ifade edebiliyor
(satın alma kadar iptal de birinci sınıf); cPanel'den gelenin ilk hafta aradığı asgariler yerinde;
ilk gerçek kiracıdan önce üretim güveni tamam.

**Müşteri ve bayi birinci sınıf:**
- Müşteri kendi aboneliğini görür: `GET /api/v1/my/subscription` (B1 kiracı kapsamı üstüne) — plan adı,
  kotalar, canlı kullanım (domain/DB/mail sayısı + ölçülen disk). Dashboard'a "Planım" kartı: doluluk
  çubukları, %80 üstü uyarı rengi, "Yükselt" düğmesi. 409 kota hatası ekrandaki sayılarla tutarlıdır.
- Parola kurtarma: tek kullanımlık, 15 dk ömürlü, argon2id-hash'li token; posta panelin kendi MTA'sından.
  E-posta doğrulama gelir — doğrulanmamış adrese sıfırlama gönderilmez.
- Davet akışı: kullanıcı oluşturmada parola opsiyonel; verilmezse hesap "pending" açılır, ilk-parola
  bağlantısı postayla gider. Bayi müşterisinin parolasını hiçbir kanaldan görmez/iletmez.
- Parola değişimi ve sıfırlama, hedefin diğer tüm oturumlarını düşürür ("sızmış parola etkisiz" sözünün
  tamamlanması — bugün açık oturum parola değişiminden sağ çıkıyor).
- Impersonation hesap verir: `impersonate.start/stop` audit_logs'a yazılır; bürünme altındaki eylemlerde
  `acting_as` alanı gerçek operatörü işaretler; panelin üstünde kalıcı "X olarak görüntülüyorsunuz — çık" şeridi.
- **Bayi tahsilat yetkisi:** bayi, kendi ağacındaki abonelikler için askıya alma/devam ettirme,
  "ödendi işaretle" ve plan değiştirme çağırabilir (B1 kiracı kapsamı süzer); hepsi `acting_as`'lı
  audit'e düşer. Müşterisinin abonelik + ödeme durumunu bayi de görür. Ödemeyen müşterisini kesemeyen
  bayi tahsilatı operatöre taşır — "bayi birinci sınıf" sözü tahsilatsız yarımdır.
- Rol-farkındalıklı onboarding (mevcut journey kart deseni, kütüphane yok): müşteri için "ilk domain →
  SSL → ilk posta kutusu → istemcini bağla"; bayi için "plan → ilk müşteri → abonelik". Canlı tamamlanma
  izler, bitince kaybolur.
- Sayfa başı açıklamalar: 12 rota + 8 domain sekmesi için birer-iki cümle TR+EN (`pages.<id>.desc`).
  Kısıt üreten 5 yere "Neden?" açıklaması (D-002, D-003, D-009, çakışma grubu, pkg desteği) — metinler
  DECISIONS kayıtlarıyla tutarlı; kısıt açıklamasız duvar olarak çarpmaz.

**Plan ve abonelik makinesi (ücretsiz katmanın temeli):**
- Planlara fiyat: `service_plans`'a `price_cents, currency, billing_period, is_free, is_public, sort_order,
  kdv_included`. Admin "Free — 1 domain, 0₺" ve "Pro — 10 domain, X₺/ay" planlarını panelden tanımlar;
  ürün fiyatları kod sabitinden DB'ye taşınır. Müşteri kendi planının adını ve fiyatını görür.
- **Sunum defteri (D-017 / D-014'ün eksik halkası):** `plan_offerings` (plan ↔ teklif-kimliği) tek
  kanonik uzayda — `component:<id>[:<sürüm>]`, `integration:acme:<id>`, `product:<id>`. Her seçici uç
  (php sürümleri, node sürümleri, SSL sağlayıcıları) çağıranın etkin kümesine süzülür: mevcut(admin) ∩
  plan(bayi); eylem uçları `NOT_OFFERED` kodlu retle yeniden doğrular. Kısıt bildirmeyen plan her şeyi
  sunar (geriye dönük kırılma yok); admin kayıt-defteri kalemlerini (örn. bir CA'yı) sunucu genelinde
  kapatabilir. Çıkış ölçütü: bayi planından PHP 7.4'ü ve ZeroSSL'i çıkarır → müşterisinin seçicilerinde
  görünmez, API'den denerse kodlu ret + audit.
- **Dönem modeli** ("yükselttim, ne ödüyorum?" sorusunun cevabı): abonelikte `current_period_start/end`;
  kural en basit dürüst olandır ve DECISIONS'a yazılır — yükseltme anında yeni dönem başlar (kıst yok,
  bu açıkça ilan edilir); düşürme ve iptal dönem sonunda uygulanır; `expires_at` dönem ucundan türetilir.
  v0.5 webhook'u bu alanları uzatır — tanımsızlığın üstüne sağlayıcı entegrasyonu kurulmaz.
- Abonelik askısı gerçek etki üretir (bugün `status`/`expires_at` ölü alan): suspended/expired abonelikte
  yeni kaynak 403, vhost'lar geri-alınabilir "hesap askıda" sayfasına döner, posta teslimi durur (kutular
  silinmez). `expires_at` geçmiş abonelikler günlük döngüde expired'a çekilir + grace period alanı.
  Manuel "ödendi işaretle" düğmesi aynı makineyi sürer — ödeme sağlayıcı kararı v0.5'te, makine bugünden çalışır.
- **İptal birinci sınıftır** ("veri rehin tutulmaz" sözünün abonelik hali): müşteri-tetiklemeli
  "dönem sonunda iptal" (`cancel_at_period_end`); dönem sonunda otomatik Free'ye düşüş. Kullanım Free
  kotasını aşıyorsa makine kilitlenmez: **zorunlu-düşürme modu** — kaynak silinmez, aşan kısım dondurulur
  (yeni kaynak 403 + aşan vhost'lar askı sayfası) + taşan kaynak listesi + X günlük tasfiye süresi +
  bilgilendirme postası. `subscription.cancel` audit'e düşer. (Aynı mod v0.6'daki trial bitişini de sürer —
  insansız düşürme 409'a çarpıp sonsuza dek Pro'da kalamaz.)
- Plan değiştirme (yükseltme monetizasyonun ana akışıdır): `PUT /api/v1/subscriptions/{id}/plan` — kotalar
  plandan yeniden kopyalanır; **insanlı** düşürmede mevcut kullanım > yeni kota ise 409 + taşıran kaynak
  listesi; **insansız/zorunlu** modda yukarıdaki dondurma kuralı. Audit'e `subscription.plan_change`.
  Müşterinin "Yükselt" talebi ilk aşamada operatöre bildirim üretir.
- **Ödeme defteri** (manuel modda bile ödemenin izi kalır): `payments` tablosu
  (subscription_id, amount_cents, currency, period, method=manual|provider, marked_by, created_at).
  "Ödendi işaretle" bu tabloya yazar; v0.5 webhook'u aynı tabloyu besler. Müşteri "Planım" kartının
  altında ödeme geçmişini görür + yazdırılabilir basit makbuz. "Ne ödedim, ne aldım" panelden cevaplanır.
- **Süre bildirimleri** (en ucuz tahsilat aracı): `expires_at`−7/−3/−1 gün ve grace başlangıcında
  müşteriye (bayili senaryoda bayiye de) panelin kendi MTA'sından posta; askı anında "neden + nasıl açılır"
  postası. Müşteri askıyı ziyaretçisinden değil postasından öğrenir.
- **Bayi havuzu bir plan türüdür** (ticari hayatı olan kota): `reseller_pools` (max_customers, toplam
  disk/domain/DB) bayi planına bağlanır — havuz boyutları + fiyatı bayi planında yazar; "Yükselt" akışının
  bayi sürümü havuzu büyütür. Abonelik açılırken bayinin ağacındaki toplam taahhüt havuzla karşılaştırılır,
  aşımda 409 + kalan-havuz mesajı; Users'da doluluk çubuğu. **Zincir kuralı** (DECISIONS'a): bayi askıya
  düşerse ağacında yeni kaynak 403, ama mevcut müşteri siteleri/postası grace sonuna dek yaşar — masum
  son-müşteri bayisinin borcu yüzünden anında karartılmaz.
- Bayiye ait plan: ölü `service_plans.owner_id` canlanır — bayi kendi planını kurar (kotalar havuzunu
  aşamaz), listede global + kendi planlarını görür, yalnız kendi müşterisine atar; "apply to subscribers"
  kopyalaması owner kapsamına saygılıdır.
- **Lisanslı ürünler ve satış zinciri (D-012, 20 Tem — üçüncü-taraf baştan kapsamda):** hak, disk gibi bir
  **havuz** olur; `reseller_pools` deseni ürünlere genişler (admin kontenjanı → bayi → müşteri; admin
  doğrudan müşteriye de satabilir). Ürün tanımına `license_model {server|seat}` + `seat_unit
  {mailbox|site|subscription|server}` girer — *seat* modelinde fazla tahsis gerçek para ve lisans ihlali
  olduğundan havuz sertçe uygulanır (kodlu ret, uyarı değil). Lisans anahtarı A4'ün `enc:v1` mekanizmasıyla
  mühürlenir. Fiyat tek sayı olmaktan çıkar (satıcı→admin, admin→bayi, bayi→müşteri; bayiye-ait-plan
  deseniyle aynı). Görünürlük hakkı izler: bayi almadıysa müşterisi ürünü hiç görmez (bayi isterse
  "satın al" tanıtımını açar). Kurulu olmayan ürüne hak satılamaz → kodlu ret. Geri alma, abonelik
  askısıyla AYNI kural (yeni tahsis 403, mevcut kullanım grace sonuna dek yaşar, veri silinmez).
  **Dürüstlük sınırı UI'da ve dokümanda yazar:** panel yalnız kendi tahsis kaydını uygular; satıcı farklı
  sayabilir (mutabakat ekranı gösterilir ama fatura satıcının gerçeğidir) ve **alt-lisanslama hakkı
  operatörle satıcı arasındadır** — panel bunu doğrulamaz, doğruladığını iddia etmez.
- Faturalama defteri: `plan.create/update/delete`, `subscription.plan_change`, `subscription.cancel`,
  `subscription.suspend/resume`, `quota.exceeded` audit olayları — "bu kota ne zaman, kim tarafından
  değişti" sorusu ihtilaf sorusudur, kayıtsız kalınmaz.
- **Mağaza seçki ilkesi (operatör, 24 Tem): farklılaşma, herkesin sunduğunu sunmakta değil,
  görmezden geldiği MÜKEMMEL açık kaynak alternatiflerini birinci sınıf sunmaktadır.** "Birçok panel
  hep aynı şeyleri sunuyor; her alanda güzel alternatifler var, bunları sunmamız gerekir." Mağaza
  kataloğu kurulurken her kategoride yerleşiğin yanına modern FOSS alternatifi bilinçle aranır.
  İlk adaylar (operatör kararı 24 Tem: grommunio elendi — kendi vendor deposuna + ağır dağıtıma
  özgü yığına bağlı olması "her Linux'ta tek yol" ilkesine aykırı): Stalwart (tek-binary modern posta
  sunucusu — taşınabilir) · restic/borg (yedekleme) · Uptime Kuma (izleme). Her aday, girmeden önce canlı
  paket/depo doğrulamasından geçer (Buypass dersi: dış uç da bayatlıyor) VE dağıtımdan bağımsız
  kurulabilmelidir (D-004: dağıtıma özgü çözüm kabul edilmez; webmail için Roundcube resmi
  tarball'ı — Node deseni — çekirdek yol oldu).

**Barındırma asgarileri (cPanel'den gelenin ilk haftası):**
- FTP (vsftpd) uçtan uca — ölçütlü: domain başına hesap, site kullanıcısının docroot'una chroot,
  **FTPS zorunlu** (düz FTP reddedilir — güvenlik varsayılandır). Kanıt: FileZilla ile bağlan → yükle →
  site canlıda değişir; chroot dışına çıkma denemesi başarısız.
- Webmail (Roundcube) — ölçütlü: katalogdan tek tık kurulum; `webmail.<domain>` vhost + Let's Encrypt +
  Dovecot bağlantısı otomatik. Kanıt: panelde açılan posta hesabı, kabuğa hiç dokunmadan webmail'den
  Gmail'e posta atıp cevabını okuyor. (Roundcube "panel için" değil "panelin kurduğu servis"tir — dış
  bağımlılık yasağına dokunmaz.)
- Dosya yöneticisi cilası — üç ölçülebilir kalem: (1) zip/tar.gz yükle-ve-aç + seçileni sıkıştır-indir,
  (2) metin dosyası yerinde düzenleme (sahiplik/izin korunur), (3) izin görüntüle/değiştir (777'ye uyarı).
  Kanıt: bir WordPress tema zip'i yalnız dosya yöneticisiyle kurulup sitede görünüyor.
- Gürültülü komşu freni: site/abonelik başına systemd slice (CPUWeight + MemoryMax, plan alanı olarak;
  alan ancak uygulamasıyla birlikte gelir — ölü alan doğmaz). PHP-FPM havuzları ve `celikapp-*` unit'leri
  ilgili slice'a bağlanır; CloudLinux lisansı gerekmez. Kanıt: bir kiracıda sonsuz döngü PHP koşarken
  komşu site <1 sn açılıyor.
- OS seviyesinde disk uygulaması (ROLES ertelenenleri) · cPanel içe aktarıcının **gerçek** müşteri
  arşiviyle kanıtı (DB kullanıcıları dahil) · WordPress Toolkit derinliği (güncelleme, sertleştirme,
  klon/staging).
- **Framework barındırma ilkelleri (D-011, 20 Tem):** Laravel/Symfony/Django'yu katalogsuz barındırmanın
  önündeki dört gerçek engel — hiçbiri framework'e özel değil, hepsi eksik *jenerik* yetenek:
  (1) **docroot alt dizin seçimi** — bugün `public_html`'e sabit; açılır kutu framework adı değil YOL
  değeri listeler (`(kök) | public | public_html`). (2) **Site kullanıcısı kimliğiyle komut ucu** —
  `composer install`, `artisan migrate`, `npm ci` çalıştırabilmek (bugün kod tabanında `composer` sıfır
  kez geçiyor; kullanıcı SSH'a itiliyor). Çıktı akışlı, zaman aşımlı, audit'li. (3) **Uzun süreli süreç
  (queue worker)** — bugün üç bağımsız engel var: `RunAsUser: "www-data"` sabiti, `req.Port <= 0` reddi,
  `project_type == "node"` kilidi; `celikapp-*` unit soyutlaması portsuz/site-kullanıcılı işçiyi de
  taşımalı. (4) **Site cron'u** — scheduler ayrı bir kavram değil, sıradan bir crontab satırı.
  Bunların üstüne **preset**: framework varsayılanlarını forma ÖN-DOLDURAN düğme (tip değil, kurucu
  değil); D-011'in yapısal saflık şartına tabidir — tek yeni alan ya da tek `if framework ==` dalı
  gerektiren preset reddedilir, preset sayısı 3'ü aşarsa strateji tartışması açılır.
- **DNS sağlayıcı soyutlaması (D-009 yeniden tartımı + 18 Tem operatör kararı):** DNS bir SEÇİM olmalı,
  dayatma değil. Panel bugün tam kayıt setini hesaplayıp tek yere (kendi PowerDNS'i) yazıyor; o "yazıcı"
  takılabilir kılınır — üç arka uç, operatör seçer (domain başına da olabilir):
  (1) **Kendi PowerDNS'i** (varsayılan, sıfır-bağımlılık: her şeyi tek kutuda isteyen için — panel
  otoriter, ns1/ns2 bu sunucu);
  (2) **Cloudflare-sınıfı yönetilen DNS** (operatör API token'ı verir — DNS-edit kapsamlı; panel AYNI kayıt
  setini PowerDNS SQL yerine sağlayıcının API'sine yazar; zone yoksa oluşturur). **Güvenlik için önerilen
  yol** ve operatörün 18 Tem gözlemi: :53 kutuda açık kalmaz, DDoS'u Cloudflare yutar, tek-nokta arıza
  kalkar. Plesk'in "Cloudflare DNS Integration" eklentisinin dürüst çekirdek karşılığı;
  (3) **Dış/elle** (panel hiçbir şey yazmaz; "şu kayıtları girin" listesi + mail-auth'taki canlı doğrulama).
  Downstream'in tamamı (mail-auth kayıtları, HTTP-01 sertifikası, panel hostname'i) değişmeden çalışır —
  çünkü değişen yalnız yazıcı, hesaplanan kayıt seti aynı. **Otomatikleştirilemeyen TEK adım dürüstçe
  söylenir:** registrar'daki nameserver delegasyonu (celikhost.com'un NS'ini Cloudflare'e ya da bu sunucuya
  yöneltmek) hiçbir sağlayıcı API'sinde yoktur; panel bunu gösterir + doğrular, o tek tıkı insan registrar'da
  atar. Karar (hangi arka uçlar, hangisi varsayılan, öneri metni) DECISIONS'a; abstraction seam v0.3'te +
  (1) ve (3) yolları; (2) Cloudflare arka ucu v0.4'te (bkz. yönetilen DNS backend'i).
- **Panel kimliği — rehberli hostname + sertifika (18 Tem saha boşluğu):** bugün panelin kendi adının
  (örn. `boston.celikhost.com`) çözülür olması TASARLANMADI — operatör test sunucusunda bunu operatör-dışı
  el (başka sunucunun panelinden kayıt) çözdü; bu, D-008'in yasakladığı gizli elle adımdır. Panel, kendi
  hostname'ini domain kurmakla aynı dürüst üç yolla ele almalı: (a) **bu panel adının ana zone'unu kendisi
  sunuyorsa** → tek tık A kaydını kendi zone'una yaz (tek-sunucu, zone şablonunun kendi-FQDN tohumlamasının
  genellemesi); (b) **DNS dışarıda/başka sunucuda** → "şu A kaydını girin: `<host>` → `<IP>`" göster ve
  mail-auth'taki canlı DNS doğrulamasıyla çözülene dek bekle, sonra sertifikayı sun; (c) **zaten çözülüyor**
  → doğrudan sertifikaya geç. Sertifika akışı (v0.2) bu ön-adımın üstüne oturur — "install.sh → giriş →
  gerçek sertifika" zincirinde artık elle DNS boşluğu kalmaz. Çok-sunucu oto-kaydı (kardeş sunucunun adını
  zone-otoritesi sunucuya kaydettirme) bilinçli olarak çok-sunucu özelliğine (1.0-sonrası) ait — orada
  panel-arası güven modeli gerekir; o güne dek (b) yolu N sunucuyu dürüstçe karşılar.

**Üretim güveni (ilk kiracıdan önce şart):**
- Sır şifrelemesi öne çekildi: A4'ün kanıtlı `enc:v1` mekanizması TOTP secret'larına ve panelin sakladığı
  özel anahtarlara (DKIM, WireGuard) genişler; eski satırlar açılışta idempotent mühürlenir. (v0.5'te
  yalnız dış denetim doğrulaması kalır — ilk kiracının 2FA sırrı aylarca düz metin beklemez.)
- Kiracı-başına rate limit: pahalı uçlar (sertifika alma, yedek tetikleme, import, DNS toplu yazma)
  abonelik anahtarlı limitle korunur; Let's Encrypt başarısız deneme sayacı + "LE limitine yaklaşıldı"
  dürüst uyarısı — tek kiracının döngüsü herkesin sertifikasını engelleyemez.
- Migrasyon disiplini (expand/contract): yıkıcı şema değişikliği iki sürüme bölünür (N'de ekle+çift yaz,
  N+1'de kaldır) — rollback veri kaybetmez. CI'ya iki test: temiz DB'ye tam zincir + dolu v(N−1) fixture
  üstüne güncel zincir; `rollback.sh` anlık görüntüden sonra yazılmış satır sayısını raporlayıp açık onay
  ister; "geri alma, snapshot sonrası değişiklikleri kaybeder" cümlesi belgelidir.
- CI güvenlik kapıları: `gosec` ve `govulncheck` her PR'da; ağ kullanan bağımlılık denetimleri paket adlarını ve
  sürümlerini yapılandırılmış kayıt servisine gönderdiği için açık işletmen onayı gerektirir; istisnalar
  `#nosec` + gerekçe. (v0.5 dış denetimi bu kapıların aylık yeşil geçmişiyle karşılanır.)
- Yazılı terfi ritüeli (OPERATIONS.md): (1) CI yeşil → (2) iki test sunucusunda (boston/Debian,
  frankfurt/Arch) `update.sh` + golden-path smoke → (3) üretim. Kanal netleşir: main=edge (test
  sunucuları), tag=stable (üretim yalnız tag'li commit çalıştırır).
- `release.yml`: `v*` tag push'unda CI ortamında `make dist` → SHA256SUMS → GitHub Release'e
  tarball+checksum+CHANGELOG bölümü otomatik.

**Çıkış ölçütü:** bir bayi + iki müşteri **bir hafta self-servis** işliyor — parola sıfırlama, davet,
kota görüntüleme, DB, FTP, webmail dahil; operatör dokunuşu sıfır · admin panelden Free + Pro planı
tanımlıyor, bir abonelik tek çağrıyla Pro'ya taşınıyor, audit kaydı düşüyor · iptal eden Pro müşterisi
dönem sonunda otomatik Free'ye düşüyor; kotayı aşan kaynakları dondurulmuş ve listelenmiş, verisi silinmemiş ·
süresi dolmak üzere olan abonelik −7/−3/−1 postalarını alıyor; askıya düşen "neden + nasıl açılır" postası
alıyor · her "ödendi işaretle" payments defterine düşüyor ve müşteri makbuzunu panelden görüyor · askıya
alınan abonelik 60 sn içinde askı sayfası dönüyor, devam ettirilince veri kaybı sıfır · 10 GB havuzlu bayi
6+6 GB iki aboneliği açamıyor (ikincisi 409); bayi ödemeyen müşterisini kendi başına askıya alıp
"ödendi işaretle" ile geri açabiliyor · gerçek bir cPanel hesabı tek tıkla taşınıyor · dış DNS kararı
DECISIONS'a işlenmiş · v0.3.0 tag'i insan eli değmeden indirilebilir release üretmiş.

### v0.35 — Plan Dürüstlüğü: Ölü Alan Kalmasın
Kısa basamak. Şemada/katalogda olup uygulanmayan her vaat ya uygulanır ya silinir — Dürüstlük kuralının
plan hali. Satılan planın her satırı gerçek olmadan ücretli katman "bitti" sayılmaz:
- `bandwidth_quota_mb`: ya uygula ya kaldır. Uygulama: usage ölçümünden abonelik-düzeyi aylık sayaç
  (dönem başı sıfırlanır); nginx log yalnız web trafiğini sayar — mail/FTP hariç olduğu plan metninde
  dürüstçe yazılır.
- Aşım politikası — "limit ne zaman ısırır" sorusunun tek cevabı: plan başına
  `enforcement ∈ {block_new, notify, suspend_writes}`; %80 ve %100 eşiklerinde müşteriye + operatöre
  posta; Dashboard "Needs attention"a kota satırı.
- Posta kutusu kotası: `mailbox_quota_mb` + Dovecot quota plugin'i (mevcut dosya-tabanlı desenle,
  idempotent tam-durum-itme). `business_email` bu limiti yükseltir — ürünün ilk gerçek kapısı.
- Ürün kapıları: Addons'ta listelenen her ürün ya en az bir `requireEntitlement` kapısına bağlı ya satın
  alınamaz. `extra_ip` tesisatı v0.5'e dek "yakında" işaretli; `firewall` müşteri-görünür bir özellik
  doğana dek katalogdan çıkar. Satın alınmadan da çalışan "satılık" ürün kalmaz. **`app_installer`
  gerçekliğe indirilir (D-011):** bugünkü "WordPress *ve diğer uygulamalar*" ifadesi tek girişli bir
  listeyi çoğul plan özelliği olarak satıyor — satış tarafına doldurulacak boş kova gösteriyor ve liste
  plan özelliğine bağlandığı an giriş silmek sözleşme ihlaline dönüyor. Ürün adı "WordPress tek-tık
  kurulumu" olur; çoğul ifade kaldırılır.
- `additional_user` gerçek özellik olur: müşteri hesabına bağlı (`parent_id`), `user_permissions` ile
  kaynak-bazlı izin (domain listesi + dosya/mail alt izinleri), kendi girişi. (CHECK genişletme ve
  frontend'deki ölü rol dallarının dürüstlüğü v0.2.5/B2'de yapılır.)
- Kullanıcı-detay görünümü: bir müşterinin abonelikleri + kota doluluğu, domainleri, entitlement'ları,
  son girişi (`users.last_login_at`) ve son 10 audit satırı tek ekranda — "bu müşteri neden 409 alıyor"
  sorusu 4 ekran gezdirmez. Admin tümünü, bayi kendi ağacını görür (v0.3 tahsilat yetkisinin ekranı).
- Cron güvenilirliği: her iş için son çalışma zamanı + çıkış kodu + son çıktı kuyruğu; başarısız işte
  domain sahibine posta (mevcut posta yığını, yeni bağımlılık yok).

**Çıkış ölçütü:** şema ile uygulama arasında tek ölü kota/durum alanı yok (alan alan denetim listesiyle
kanıtlı) · %80 doluluğa ulaşan test aboneliği 5 dk içinde posta alıyor, %100'de politika uygulanıyor ·
100 MB kotalı kutuya 101. MB "Quota exceeded" ile reddediliyor · Addons'taki her ürün kapılı ya da satın
alınamaz · ek kullanıcı yalnız izinli domain'in sekmelerini görüyor · bilerek exit 1 dönen cron listede
kırmızı ve sahibinin INBOX'ında.

### v0.4 — İşletim Güveni
Operatörün gece 3'te ihtiyaç duyduğu şeyler:
- İzleme + uyarı (servis düştü, disk doldu, sertifika hatası → posta/webhook) · panelde log görüntüleyici
- **Metrik tarihi (Plesk kıyasından, 17 Tem):** bugünkü kartlar anlık değer gösterir, hikâye göstermez.
  Agent'ta hafif örnekleyici — CPU/RAM/disk/trafik N saniyede bir SQLite halkalı tabloya, eski veri
  otomatik seyreltilir (dış bağımlılık YOK: Prometheus/Grafana değil, anayasa korunur). Pano kartlarına
  sparkline (kart sade kalır); karta tıklayınca 24 saat / 7 gün detay grafiği. Uyarı eşikleri aynı
  veriyi okur — grafik ve alarm tek altyapının iki yüzü. Çok-lokasyonlu dış uptime izleme bilinçli
  hedef-dışı: dürüst cevap heartbeat + "UptimeRobot/360 kullanın".
- **Uyarı kanalının kendi sağlığı:** uyarılar posta VE webhook'tan bağımsız iki kanaldan gider (posta
  kuyruğa giremezse webhook'a düşer) + dışa dönük heartbeat — panel N dakikada bir operatörün seçtiği
  dış uca ping atar, kesilince alarm DIŞARIDAN çalar. En olası arıza, uyarıyı taşıyacak kanalın ölmesidir.
- Uzak yedek hedefleri (S3/FTP) + ürün özelliği olarak geri yükleme tatbikatı — **iki sertleştirmeyle:**
  (1) **Panel state felaket yedeği:** SQLite (online backup API ile tutarlı kopya) + `secret.key` +
  DKIM/WireGuard anahtarları + panel sertifikaları tek arşivde, domain yedekleriyle aynı saklama süresiyle
  uzak hedefe. `secret.key` kaybı = tüm mühürlü sırlar geri dönüşsüz; "domainler yedekte ama panelin
  beyni yok" durumu kabul edilmez. (2) **İstemci tarafında şifreleme:** uzak hedefe çıkan her arşiv
  panelde şifrelenir; yedek anahtarı `secret.key`'den ayrı tutulur ve kurulumda bir kez gösterilir —
  "anahtarsız yedek okunamaz" dürüstlüğü UI'da yazar.
- **Müşteri self-servis geri yükleme:** yedek listesinden tam site / tek dizin-dosya / DB dump dönüşü —
  onay modalı + audit kaydıyla. "Her kullanıcının sorununu sen mi çözeceksin?" sorusunun en pahalı
  cevabı restore'dur; müşteri gece 3'te kendi bozduğunu kendi döner.
- **İkincil DNS gerçeği:** bugün ns1/ns2 aynı makineyi gösteriyor (tek nokta arızası);
  ikinci ucuz VPS'e secondary PowerDNS (AXFR) ya da dürüst belgeleme (11 Tem eklendi)
- **Yönetilen DNS backend'i (Cloudflare-sınıfı) — v0.3 soyutlamasının somut sağlayıcısı:** Settings'te
  "DNS sağlayıcısı" seçimi; Cloudflare arka ucu = scoped API token + zone oto-oluşturma + `syncZoneToDNS`
  seam'ine ikinci yazıcı (aynı kayıt seti, farklı hedef). İkincil-DNS tek-nokta sorununun en temiz
  cevabı da budur: DNS'i tümüyle Cloudflare'e vermek, ikinci VPS'e AXFR kurmaktan basit ve daha güvenli.
  Panel :53'ü hiç açmaz; "güvenlik varsayılandır" ilkesiyle bu yolu ÖNERİR ama dayatmaz (kendi PowerDNS'i
  isteyen için sıfır-bağımlılık varsayılan kalır). Registrar NS delegasyonu dürüstçe elle adım olarak
  gösterilir + doğrulanır. Bu, panelin kendisine dış bağımlılık DEĞİLDİR — operatörün seçtiği opsiyonel
  arka uç (MariaDB↔PostgreSQL seçimi gibi); token operatörün, panel internetsiz de tam çalışır.
- Arayüzden tek tık panel güncellemesi (update.sh'ın ön yüzü) · WebSocket canlı bildirimler
- **Güncelleme zinciri sertleşir:** release-binary kanalı birincil olur — `update.sh` sürüm tarball'ını
  indirir, **imza doğrular** (minisign/cosign; açık anahtar install.sh'a gömülü, rotasyon planı yazılı),
  SHA256 doğrular, derleme adımını atlar. Üretim sunucusunda Go/Node toolchain'i gerekmez (saldırı
  yüzeyi + küçük VPS'te OOM + bit düzeyinde fark, üçü birden kapanır); `--from-source` geliştirici/test
  sunucularına kalır. Doğrulama başarısızsa mevcut sürümde kalınır + "Needs attention" + audit kaydı.
- **Güncelleme sonrası self-check:** bitişte otomatik smoke — panel HTTP 200 + login render + agent
  socket ping + `PRAGMA quick_check`; kalan varsa çıktı "rollback.sh önerilir" der, tek-tık akışında
  geri alma düğmesi sunulur.
- **Arıza matrisi — "panel öldü, hosting yaşıyor" kanıtı:** panel process kill → web/DNS/posta serviste
  (testle ölçülür), panel `Restart=on-failure` ile kendine gelir; "kırılma camı" runbook'u (panel
  açılmıyorsa SSH'tan teşhis adımları) D-008'e acil durum istisnası olarak yazılır.
- **CelikPanel→CelikPanel taşıma:** hesap-düzeyi export arşivi (domainler + docroot + DB dump + maildir +
  DNS zone + DKIM anahtarı + abonelik/kota metadata'sı tek imzalı tar'da); karşı sunucuda cPanel
  içe aktarıcının inspect→onay→apply akışı bu formatı da tanır. "Veri rehin tutulmaz" sözünün tam hali;
  sunucu değişimi hosting işinin rutinidir.
- **Ziyaretçi istatistiği (minimal, bilinçli sınırlı):** trafik ölçümü zaten nginx access log'unu okuyor;
  aynı geçişten günlük hit / tekil IP / ilk 10 sayfa / ilk 10 referrer çıkarılıp domain Overview'a kart
  olur. Tam analitik (oturum, coğrafya, gerçek zamanlı) hedef-dışıdır — dürüst cevap: "siteye
  Plausible/GA koyun".
- **Audit bütünlüğü:** yazma hatası sayaçlanıp "Needs attention"a düşer (eylemi bloklamadan ama sessiz
  de kalmadan); audit için yapılandırılabilir saklama süresi + budama.
- **Aktif oturumlar:** Settings'te oturum listesi (oluşturma, son kullanım, IP) + tekil/toplu sonlandırma;
  admin Users'tan hedefin oturumlarını düşürebilir.
- **Temiz başlangıç:** panel, kendisinin kurmadığı katalog servislerini "yabancı" diye işaretler
  (audit log'da `service.install` kaydı olmayan her katalog servisi) ve operatör onayıyla kaldırmayı
  önerir — sahiplen ya da tahliye et, Import felsefesinin aynası. Saha kanıtı (16 Tem): Hostinger
  Arch imajı uyuyan bir bind ile geldi; kurulum yolculuğu "DNS kuruldu: Done" saydı. B3 üstüne oturur.
- **Pano SSL/TLS özeti** (Plesk kıyasından, 17 Tem): yakında dolacak / geçerli / sertifikasız site
  sayıları tek kartta — "90 günde sessizce ölen sertifika" sınıfı, panelin yüzünde görünür olur
- **Pano Mail Queue kartı** (Plesk kıyasından, 17 Tem): Total/Deferred/Held + tek tık kuyruk
  temizleme — operatörün gece 3'te ilk baktığı yer
- **Kendi kendine teşhis:** operatörün 17 Tem'de sorduğu soru tasarım ölçütü — "her kullanıcının
  sorununu sen mi çözeceksin?" Panel, bugün elle teşhis edilen sınıfları kendisi denetlemeli:
  DNS delegasyonu bu sunucuya bakıyor mu, **panelin kendi hostname'i BU sunucuya çözülüyor mu** (18 Tem:
  boston.celikhost.com kaydı frankfurt'un zone'unda yaşıyordu, boston'ın haberi yoktu — bu bağ görünmezdi),
  sertifika yenileme zamanlayıcısı gerçekten koşuyor mu, servis config'i motoru gerçekten başlatabiliyor mu.
  Bulgu = "Needs attention" satırı + tek tık onarım (Plesk'in Repair Kit'inin dürüst karşılığı)

- **Katalog genişlemesi — Python, Arch'ta veritabanları, göç kolu (25 Tem 2026):**
  - **Python çalışma ortamı.** Bugün panelde Python **yok** — ne katalogda bir kalem, ne kodda bir
    satır; `hosting_handlers.go`'daki yorum "go/python sonradan eklenebilsin diye" diyor ama kalem hiç
    açılmadı. Node'un kullandığı üç ilkel zaten var ve büyük ölçüde genel: site başına systemd unit
    (`ApplyAppUnit`/`ControlAppUnit`/`AppUnitStatus`/`AppUnitLogs`), önündeki ters vekil ve sürüm
    çekmecesi. Python'un eklediği üç şey: **site başına sanal ortam (venv)**, **uygulama sunucusu
    seçimi** (gunicorn/uvicorn — WSGI ve ASGI ayrımı ürün kararıdır, gizlenemez) ve Add Domain'de
    "Python Uygulaması" kartı. **Sistem python3'ü kullanmak yok:** Node'da bilerek kaldırılan
    "sistem yorumlayıcısı" kaçağı burada açılmaz — panel yalnız kendi kurduğuyla çalışır.
    Django/Flask/FastAPI barındırma, cPanel'in "Setup Python App"ine karşılık gelen ve ücretsiz
    panellerin çoğunun kötü yaptığı yerdir; yeni kavram gerektirmeyen en büyük pazar genişlemesi.
  - **Arch'ta veritabanları.** MariaDB ve PostgreSQL Arch'ta hiç sunulmuyor; sebep katalogda
    "kurulum sonrası ilk hazırlık" alanının olmaması (`initdb` / `mariadb-install-db` adımı ifade
    edilemiyor). **Tek katalog alanı** iki bileşeni açar, dağıtım matrisindeki iki boşluğu kapatır ve
    Arch'ta tek tık WordPress'i mümkün kılar. "OS bağımsızlığı" iddiasının ölçülebilir sınavı budur.
  - **DNS bölge içe/dışa aktarma (+ CAA).** Kimse içeri giden yol olmadan panel değiştirmez; zone
    aktarımı en ucuz göç koludur ve panelin kendi yetkili DNS'ine sahip olması bunu yapısal olarak
    kolaylaştırır.

  **Çıkış ölçütü:** iki dağıtımda da panelden Python sürümü kurulup Add Domain'den tek formda çalışan
  bir Django/FastAPI sitesi ayağa kalkıyor (venv site başına, unit çalışıyor, ters vekil doğru) ·
  Arch'ta panelden MariaDB VE PostgreSQL kurulup ilk veritabanı oluşturuluyor · dağıtım matrisinde
  bu üç hücre artık boş değil · bir cPanel zone dosyası panele aktarılıp alan adı sorgulara doğru
  cevap veriyor.

**Çıkış ölçütü:** öldürülen servis bir dakikada uyarı üretiyor; fişi çekilen sunucu 5 dakikada **dış**
alarm üretiyor · geri yükleme tatbikatının tanımı: temiz VPS'te `install.sh` + panel-state restore +
domain yedekleri ile tam sunucu ayağa kalkıyor ve DKIM imzalı posta **aynı anahtarlarla** INBOX'a düşüyor ·
uzak depodaki hiçbir nesne şifresiz değil · üretim VPS'inde Go/Node kurulu olmadan sürüm güncellemesi
başarılı; bozuk imzalı tarball reddediliyor ve audit'e düşüyor · müşteri sildiği `wp-config.php`'yi
panelden tek başına geri getiriyor · arıza matrisindeki her hücre en az bir kez tatbik edilmiş.

### v0.5 — Güvenlik Derinliği
- WAF kararı (ModSecurity ya da dürüst alternatif) · fail2ban derin entegrasyonu · ClamAV zamanlanmış site taramaları
- Sır şifrelemesinin dış denetimle doğrulanması (uygulama v0.3'te yapıldı: TOTP + DKIM + WG anahtarları `enc:v1`)
- Dış güvenlik denetimi · dedicated IP tesisatı (satılabilir `extra_ip` — v0.35'ten beri "yakında" duran kapı açılır)
- **API token yönetimi:** kullanıcı başına adlandırılmış token (crypto/rand, DB'de yalnız hash, bir kez
  gösterim, kapsam: salt-okunur/tam, iptal); token'lı istekler kiracı-kapsam süzgecinden geçer ve audit'e
  düşer. "Her şey önce API" sözü, curl ile kullanılamayan bir API ile tutulamaz.
- **SFTP/SSH anahtar yönetimi:** site kullanıcısı için müşteri public key ekler/siler (defter panelde,
  agent `authorized_keys`'i tam-durum-itme ile yazar); parola girişi ve shell **varsayılan kapalı**
  (internal-sftp + chroot docroot); `access.ssh_key.add` audit'te. Düz FTP'nin ötesini isteyen ajans ve
  CI/rsync/git-hook akışlarının yolu budur; panel-içi terminal bilinçli hedef-dışı.
- **2FA derinliği:** etkinleştirmede 8 tek-kullanımlık kurtarma kodu (argon2id hash'li, bir kez gösterilir);
  login'de "kurtarma kodu kullan" dalı; "admin/bayi için 2FA zorunlu" ayarı — zorunluysa 2FA'sız kullanıcı
  ilk girişte kurulum ekranına kilitlenir.
- **Ödeme entegrasyonu karar kaydı (D-0xx):** sağlayıcı sınıfı = hosted-checkout / Merchant-of-Record
  (Stripe Checkout / Paddle / iyzico sınıfı — kart verisi panele asla girmez, PCI kapsamı sıfır); panel
  yalnız **webhook tüketicisidir**: abonelik↔sağlayıcı-müşteri eşleme tablosu + imza doğrulamalı,
  idempotent tek webhook ucu (işlenen event_id defteri) + `payment.event` audit'i. Olay sözleşmesi üç
  sınıftır: "ödendi" → dönem uzar (payments defterine satır), "ödenmedi" → grace → askı (v0.3 makinesi),
  **"iade/chargeback"** → iade: ilgili dönemin uzatması geri alınır (`expires_at` kısalır) +
  `payment.refund` audit'i + operatör bildirimi; chargeback: otomatik askı + "Needs attention" satırı
  (chargeback aynı zamanda kötüye-kullanım sinyalidir). Fatura PDF'i sağlayıcıdan link. Manuel "ödendi
  işaretle" modu sağlayıcısız operatör için kalır; **ilk sürümde sağlayıcı entegrasyonu yalnız operatör
  düzlemindedir, bayi tahsilatı manuel akışla sürer (bilinçli ve yazılı).** Anayasa kısıtı: tek binary +
  SQLite — ödeme çekirdeğe gömülmez.

**Çıkış ölçütü:** dış denetim yüksek önem bulgu vermiyor · bir müşteri panelden dedicated IP satın alıp
kullanıyor · dokümandaki tek curl örneği token ile domain açıyor; iptal edilen token anında 401 ·
DB dump'ında ve dataDir'de grep ile tek düz sır bulunamıyor · sahte webhook event'i test sunucusunda
`expires_at` uzatıyor; aynı event ikinci kez etkisiz; refund event'i uzatmayı geri alıyor · kurtarma
koduyla giriş bir kez başarılı, ikincide 403; zorunluluk açıkken 2FA'sız bayi giremiyor · anahtarlı
müşteri sftp ile yalnız kendi docroot'unu görüyor, shell denemesi reddediliyor.

### v0.6 – v0.9 — Beta Programı
- OpenAPI dokümantasyonu (API-first sözünün kanıtı) · yönetici ve kullanıcı kılavuzları TR+EN
- **Yardım merkezi + derin bağlantı:** docs/ altındaki markdown'dan üretilen statik TR+EN site (üretici
  basit, panele dış bağımlılık girmez); her panel sayfasının başlığında sabit slug'la doküman sayfasına
  giden yardım ikonu (`pages.<id>` → `/help/<lang>/<slug>`); kırık slug'ları yakalayan test B5 CI'ında.
  Doküman panele bağlanmazsa ölü doğar.
- **Tarayıcıdan ilk-açılış:** DB'de hiç admin yoksa panel tek seferlik ilk-açılış moduna girer —
  `install.sh`'ın ürettiği tek-kullanımlık token'la tarayıcıdan admin oluşturulur, admin oluşunca mod
  kalıcı kapanır. "install.sh → giriş ekranı" vaadinin ortasındaki "terminale dön, CLI çalıştır" adımı
  kalkar; korumasız ilk-kurulum ucu olmasın diye aceleye değil bu sürüme kondu.
- **Self-signup + kötüye kullanım freni** (ücretsiz katmanın ölçeklenme yolu): e-posta doğrulamalı kayıt
  formu — **varsayılan KAPALI** (güvenlik varsayılandır), açan operatör free plana bağlar; disposable-domain
  listesi + IP başına kayıt hız sınırı. Fren: free planda saatlik giden posta üst sınırı (mail_policy
  deseni üstüne), yeni hesapta ilk 24 saat gönderim bekletme seçeneği, kaynak oluşturma hız sınırı.
  Deneme: plan başına `trial_days` → `expires_at` otomatik dolar, dolunca **zorunlu-düşürme moduyla**
  free'ye iner (v0.3 kuralı — insansız geçiş 409'a çarpamaz). Tek kötü müşteri, teslim edilebilirlik
  ekranıyla kazanılan IP itibarını yakabilir — signup freni olmadan açılmaz.
- **Komut paleti:** Ctrl+K — nav + domain listesi + servis sayfaları tek fuzzy arama; ilk sürümde yalnız
  gezinme (eylem çalıştırma yok — güvenlik yüzeyi büyümez), yanına "?" kısayol listesi; dış bağımlılıksız,
  rol süzgecine uyar. Bugünkü 12 rotada lüks olurdu; beta operatörünün günlük verimi için doğru an burası.
- **SUPPORT.md (TR+EN):** 1.0 öncesi yalnız en son minor destek görür, güvenlik düzeltmeleri son minor'a
  patch gelir; yükseltme yolu "her sürümden en yenisine, migration zinciri fixture testleriyle kanıtlı";
  v1.0'da N−1'e genişler. Beta davetiyle birlikte yayında — cevap o anda uydurulmaz.
- **KVKK/GDPR asgarileri:** veri ihracı = taşıma arşivinin müşteri-tetiklemeli hali; hesap silme DB
  kaskadına ek maildir + docroot + hesabın yedek arşivlerini kapsar ve "neyin silindiği" raporu üretir;
  log/audit saklama süreleri yapılandırılabilir ve belgeli.
- Gerçek dış beta kullanıcıları; onların duvarları düzeltme olur (alfa modeli, ölçeklenmiş)
- Performans hedefleri ölçülüp uygulanıyor (<100 ms API)
- **Lisans/iş modeli kararı** (öneri: open core) → repo görünürlüğü buna göre. Kararla birlikte iki
  taahhüt DECISIONS'a yazılır: (1) **iki düzlem ayrımı** — operatörün kendi müşterisinden para alması
  (panelin özelliği) ile CelikPanel'in operatörden para alması (lisans) ayrı düzlemlerdir; birincinin
  hiçbir tablosu/ucu ikinciye telemetri/lisans denetimi taşımaz, panel internetsiz ortamda tam çalışır.
  (2) **Fiyat konumu:** fiyat sunucu başınadır, barındırılan hesap/domain sayısına göre **asla**
  ölçeklenmez — cPanel'in 2019 sonrası hesap-başına modeli panel tarihinin en büyük göçünü tetikledi;
  bu vaat geç ilan edilirse beta "bu da büyüyünce cPanel'leşir" şüphesiyle gelir.
- **"Neden CelikPanel" belgesi** (docs/WHY.tr.md + WHY.md): üç sütun (CelikPanel/cPanel/Plesk) —
  kurulum ayak izi, varsayılan güvenlik, tek binary vs servis ormanı, TR-birinci-sınıf, fiyat ilkesi;
  artı bilinçli eksiklerin gerekçeli itirafı (her ret bir DECISIONS kaydına link). Karşılaştırmayı
  rakip pazarlaması yapmadan biz yaparız — dürüstçe.
- **Hafif marka özelleştirme:** global "panel adı + logo + vurgu rengi" ayarı (tek tablo satırı;
  login + sidebar + posta şablonları tek kaynaktan okur). Bayi-başına marka 1.0-sonrasıdır.

**Çıkış ölçütü:** ≥3 dış operatör ≥1 ay gerçek site işletiyor; dokümantasyon sorularını bizden önce
cevaplıyor · temiz VPS'te SSH'a dönmeden tarayıcıdan admin oluşturulup giriş yapılıyor; token'sız/ikinci
deneme 403 · doğrulamasız e-postayla hesap açılamıyor; free hesap saatte N+1'inci postada sınırlanıyor ·
her panel sayfasından çalışan doküman bağlantısı var, kırık slug testi CI'da · beta duyurusu fiyat
ilkesi ve WHY sayfasıyla çıkmış · SUPPORT.md yayında ve v0.3.0→güncel atlama testi CI'da yeşil.

### 🎯 v1.0 — Genel Çıkış
- Temiz VPS → dakikalar içinde çalışan panel, dokümante, kendi kendini güncelleyen
- "Domain → canlı site" arka arkaya 100 kez hatasız (Faz 1 sözü, artık CI'da)
- cPanel'den taşınma defalarca kanıtlı · fiyatlandırma/lisans yayında

**Çıkış ölçütü:** dokümandan başka hiçbir yardım almayan bir yabancı, temiz VPS'te kurulumdan canlı
HTTPS'li siteye ve INBOX'a düşen postaya kendi başına ulaşıyor · "domain → canlı site" 100/100 CI'da ·
en az bir gerçek cPanel göçü ve bir CelikPanel→CelikPanel taşıma üretimde kanıtlı · fiyat/lisans sayfası
ve SUPPORT politikası yayında.

### 1.0 Sonrası — ufuk *(talep sürer, hayal sürmez)*
Her biri ancak gerçek talep varsa, bilinçli hedef-dışılara uygun biçimde:
- Çoklu sunucu (panel-arası güven modeli + **kardeş sunucu DNS oto-kaydı**: yeni bir CelikPanel sunucusu,
  marka domain'inin zone-otoritesi olan diğer CelikPanel'e kendi hostname A kaydını, operatörün verdiği
  API token'ıyla kaydettirir — bugün elle yapılan boston.celikhost.com adımının ürünleşmiş hali; v0.3'ün
  (b) dış-DNS yolu bu gelene dek N sunucuyu karşılar) · BSD agent arka ucu · faturalama entegrasyonları (WHMCS vb.)
- **Plesk/DirectAdmin içe aktarıcıları** — ancak gerçek talep (≥5 somut göç isteği) doğunca; cPanel
  içe aktarıcının inspect→onay→apply deseni yeniden kullanılır. O güne kadar dürüst cevap dokümante:
  "Plesk'ten geliyorsanız şimdilik elle taşıma kılavuzu."
- **Bayi-başına white-label** (özel giriş domain'i, bayi logosu) — hafif global marka v0.6–0.9'da;
  bayi-başına ancak bayi satışları bunu gerektirirse.
- **Hosted hizmet** (CelikPanel'in kendisinin VPS+panel kiralaması) — pazar yeri gibi peşin reddedilmez
  ama şimdi tasarlanmaz; açılırsa self-hosted ürünün mimarisine tek satır telemetri/phone-home taşımaz.
- Pazar yeri **asla** (AltaVista hatası).

---

## Bilinçli Hedef-Dışılar

Sadelik hayır diyebilmektir. Bunlar **bilerek** yok — ve retlerin çoğu ürünün ta kendisidir:

- ❌ **Docker/konteyner katmanı** — hedef pazar klasik hosting; doğrusu native. Rekabet cümlesi de budur:
  Plesk kurulumu yüzlerce paket ve düzinelerce servis getirir; CelikPanel iki binary getirir. Bu fark
  bir eksik değil, üründür.
- ❌ **Akla gelen her servise yönetim ekranı** — kurulu olmayan görünmezdir.
- ❌ **Tema/görünüm pazarları, portal vitrinleri, eklenti pazarı** — AltaVista hatası; ayrıca üçüncü-taraf
  kod çalıştırmama garantisi, "root Agent'a yalnız Panel erişir" ilkesinin doğal sonucudur. Eklenti
  pazarı bu garantiyi satar.
- ❌ **Genel uygulama kataloğu yarışı (Softaculous'un 400+ girişi)** — sığ-geniş katalog bakım
  karadeliğidir ve talep ezici biçimde WordPress'tedir. Katalog ancak (a) gerçek talep kanıtı ve
  (b) WordPress kalitesinde uçtan uca kurulum (resmi tarball, doğrulama, tam yapılandırma) **birlikte**
  sağlanınca tek tük büyür. Konum: WordPress'te herkesten derin, katalogda bilerek dar.
- ❌ **Tam ziyaretçi analitiği** (oturum, coğrafya, gerçek zamanlı) — v0.4'teki minimal log kartıyla
  yetinilir; ötesini isteyene dürüst cevap: siteye Plausible/GA koyun. AWStats klonu sadelik süzgecini geçmez.
- ❌ **Panel-içi terminal** — saldırı yüzeyi ve sadelik; SFTP/SSH anahtar yönetimi (v0.5) meşru ihtiyacı karşılar.
- ❌ **Çoklu sunucu / cluster (şimdilik)** — tek sunucu kusursuz olmadan dağıtık sistem hayali yok.
- ❌ **Panelin kendisi için dış bağımlılık** (Redis, harici DB, mesaj kuyruğu) — tek binary + SQLite kalır.
  Ödeme bile çekirdeğe gömülmez; panel yalnız webhook tüketicisidir (v0.5 kararı).
- ❌ **Telemetri / phone-home** — panel dış ağa kendi iradesiyle istek atmaz (operatörün kurduğu
  heartbeat ve gelen webhook hariç). Lisans modeli ne olursa olsun bu değişmez.
- ❌ **BSD desteği (şimdilik)** — **ama seçenek bilinçle korunuyor ve asla fork olarak değil.** Panel↔agent
  RPC sözleşmesi tasarım gereği OS-nötr: panel (HTTP/SQLite/UI/iş mantığı) zaten FreeBSD'ye çapraz
  derlenen taşınabilir Go; yalnız agent'ın "elleri" (systemd/apt/nftables) Linux'a özgü. Gerçek talep
  doğarsa (örn. hosting'çileri BSD'ye iten bir Linux güven krizi) hamle, aynı RPC yüzeyinin arkasına
  BSD agent arka ucu koymaktır — haftalarla ölçülen iş, tek ürün. İki CelikPanel asla olmayacak.
  Bunu ucuz tutan disiplin: yeni agent özellikleri "ne"yi (RPC yüzeyi) "nasıl"dan (exec çağrıları)
  ayrı tutar — kod zaten böyle yazılıyor. *(Karar: 8 Temmuz 2026.)*

---

## Tarihsel Durum — 29 Ağustos 2026

**Sürüm:** artık tek kaynaklı — sürüm ve commit bağlama anında HER İKİ binary'ye gömülüyor,
`/api/v1/panel/version` sunuyor, panelin alt bilgisi oradan okuyor ve panel ile agent farklı yapıdaysa
uyarı gösteriyor. Elle yazılmış "v0.1.0" metni silindi. İki test sunucusu aynı commit'te doğrulandı.
**Güncel canlı kimlik:** Önceki iki sunuculu eşleşen-commit gözlemi tarihsel
kanıttır; iki sunucunun güncel durumunu kanıtlamaz. Canlı değerleri
[tarihli canlı durum kaydında](docs/LIVE-STATE-2026-08-29.tr.md) yeniden doğrulayın.
**Merdivendeki yer:** v0.2 sürüyor; v0.2.5'in birçok kalemi yolda kapandı; **yeni v0.2.6 basamağı
v0.3'ün önüne girdi** (yukarıdaki üç veri-kaybı kusuru sebebiyle).

**Temmuz ikinci yarısında kapanan canlı kırıklar (hepsi iki sunucuda kanıtlı):** spam filtresi artık
gerçekten süzüyor (milter zinciri tek sahipli; GTUBE testi iki dağıtımda da reddedildi) · Arch'ta
Postfix gelen HER postayı reddediyordu (harita tipi artık keşfediliyor: lmdb/hash/btree/texthash) ·
WireGuard kurulunca gerçekten çalışıyor · agent artık istekten gelen yola/URL'e güvenmiyor
(yol beyaz listesi + sembolik bağ reddi; depo anahtarı kendi katalogdan) · yapılandırma editörü
yaz→doğrula→**geri al** yapıyor (nginx/postfix/dovecot/apache doğrulayıcıları) · koltuk kuralı artık
agent'ta uygulanıyor (arayüz engelliyordu, API kabul ediyordu) · takma ad unit'i artık kurulu
saymıyor · güncelleme betiği artık bayat yapı kurmuyor.

**Yeni yetenekler:** her yönetim sayfasında **Yardım** düğmesi (21 bileşen için özel içerik + türe
göre 3 genel yedek, tamamı TR+EN; yardımsız sayfa yapısal olarak imkânsız) · boş yönetim sayfası
yasağı (D-019) · dağıtım destek matrisi **katalogdan üretiliyor** ve bayatlarsa test kırmızı yanıyor ·
"bu dağıtımda sunulmuyor" rozeti · izleme sayfası · Valkey kataloğa eklendi (yeni bileşenin gerçek
maliyeti: 2 kaynak dosya, sıfır Go kodu).

**Borç durumu:** üretilen OpenAPI istemcisi, tek bildirimsel route/authz tablosu, arayüz tekleştirmesi
ve ölçülmüş uçtan uca gecikme açık. D-017 kanonik teklif kimliklerini tanımlıyor; kalıcı Mağaza satırları
ise hâlâ düz eski kimlikleri kullanıyor ve geçiş açık bir veri/API uyumluluk migration'ı gerektiriyor.
CI artık yalnız derlemiyor: Go biçim/derleme/vet/test/race, shell ve depo sözleşmeleri, kilitli web
derlemesi ve bağımlılık kapısı ile yeniden üretilebilir sürüm ürünlerini denetliyor.

**Depo anlık görüntüsü:** Ölçüm gerektiğinde tam commit üzerinde
`bash tools/repo-metrics.sh` çalıştırılır. Çıktı, üretilen kaynak-ağaç
kanıtıdır; satır, dosya, rota, migration veya komut-yeri sayıları bu yol
haritasına elle kopyalanıp tutulmaz. Komut çıktısını tam commit'iyle birlikte
inceleme veya release kanıtına kaydedin. Bu ölçüm iki sunucunun dağıtılmış
build'ini kanıtlamaz.


---

## Bu Belge Nasıl Güncellenir

- **Her önemli karar commit ile buraya işlenir.** Gerekçe [DECISIONS](docs/DECISIONS.tr.md)'a, borç
  [AUTOPSY](docs/AUTOPSY.tr.md)'ye; bu dosyaya yalnız merdivendeki yeri ve ölçütü yazılır.
- Yeni istek buraya **"ileride" diye giremez**: ya bir sürüm basamağına ölçülebilir çıkış ölçütüyle
  yazılır ya da Bilinçli Hedef-Dışılar'a gerekçesiyle eklenir. Üçüncü yol yok.
- Çıkış ölçütü karşılanmadan sonraki basamağa geçilmez; bir ölçüt değişecekse değişiklik gerekçesiyle
  birlikte commit'lenir (sessiz sulandırma yok).
- Her işlemede "Son güncelleme" tarihi ve "Neredeyiz" bölümü tazelenir; İngilizce eş (ROADMAP.md)
  aynı commit'te güncellenir.
