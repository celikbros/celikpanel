# Otopsi Raporu ve Borç Defteri

*İlk denetim: 11 Temmuz 2026 · [English](AUTOPSY.md) · Durum kutucukları canlıdır — kapatan, commit'iyle işaretlesin.*

Bu belge, kod tabanının acımasız-dürüst denetiminin kalıcı halidir: ne kırık,
ne çift, ne ölü, felsefe nerede ihlal ediliyor — ve ödeme planı. Amaç suçlamak
değil; **devralan mühendisin ilk gününde tüm cesetlerin yerini bilmesi.**
Yöntem: varsayım yok, her bulgu dosya:satır kanıtlı (11 Tem 2026 itibarıyla).

## Karar özeti

**Refaktör, rewrite DEĞİL.** Panel/Agent/RPC omurgası sağlam; golden path
uçtan uca kanıtlı (tek gerçek varlığımız). Sorunlar mimari değil: dikişsizlik,
çift bilgi, test yokluğu. Rewrite kanıtlanmış davranışı çöpe atar (Netscape hatası).

## A. Kırıklar (bulunduğunda canlıydı)

| # | Bulgu | Kanıt | Durum |
|---|---|---|---|
| A1 | v2 DB handler'ları `TypeID==23/24` bekliyordu; tohum 1=postgresql, 2=mariadb — iki dal da ölü, sürücüye boş tip gidiyordu | `database_v2_handlers.go` (eski 280/428), tohum `001_full_schema.sql:205` | ✅ Kapandı (11 Tem): `GetByID` artık kanonik tip adını JOIN'ler, handler `dbDriverTypeFor()` kullanır |
| A2 | Müşteri/bayi Databases sayfası görüyor (nav ALL) ama `/api/v2/` tümüyle admin kilidinde → admin olmayana her zaman kırık sayfa | `nav.ts:32` ↔ `middleware.go:141` | ✅ KALICI kapandı (17 Tem): battaniye kilit kalktı, uçlar kiracı-kapsamlı (14 denetim), sunucu-kaydı admin'de, nav admin/bayi/müşteri; taze müşteri boş liste görür (404 değil) — canlıda müşteri rolüyle doğrulandı |
| A3 | v2 uçları kiracı-güvenli değil: `subscriptionID := 1 // TODO: Get from auth` | `database_v2_handlers.go:52,106,235,507` | ✅ Kapandı (11 Tem): 6 hardcode kalktı → `callerSubscriptionID`; 6 sunucu-kapsamlı işlem (liste/oluştur/sil × db+kullanıcı) `canAccessDBServer` ile sahiplik doğruluyor (`database_v2_authz.go`). **Admin kilidi HÂLÂ duruyor** — kalkması için `handleCreateDatabaseV2Server`'ın rol ayrımı gerekli (keyfi host/port/root-parola kaydı müşteri işi değil); o v0.3 kiracı işine ait |
| A4 | DB sunucu root parolası düz metin: `// TODO: Encrypt` | `database_v2_handlers.go:138` | ✅ Kapandı (16 Tem): `internal/secrets` — AES-256-GCM, anahtar `dataDir()/secret.key` (0600, ilk açılışta üretilir). Kayıtta mühürle, sürücüde tek yardımcıdan (`dbDriverFor`) çöz; açılışta eski düz-metin satırlar idempotent migrasyonla mühürlenir. Biçim `enc:v1:` önekli — kendini tanımlar |
| A5 | `capabilities.mail_server` BOOL, `dns_server` string — tip tutarsızlığı üründe gerçek hata üretti (pano "posta kuruldu" dedi) | `capabilities_handler.go:30` | ⬜ AÇIK (ön yüz düzeltildi; API tutarlılığı B1'de) |
| A8 | **Statik site PHP kaynağını indirtiyordu**: statik vhost'ta PHP işleyicisi yok — doğru; ama `.php` için ret kuralı da yoktu. nginx dosyayı bulur, uzantıyı tanımaz (`mime.types` içinde yok), `default_type application/octet-stream`e düşer ve tarayıcıya **kaynak kodu indirtir** — içinde veritabanı parolası varsa parola sunucudan çıkar. "Burada PHP çalışmaz", "burada PHP kaynağı yayınlanır" demek olmamalı | `internal/services/templates/nginx/vhost.conf.tmpl` (statik dal) | ✅ Kapandı (21 Tem): `\.(php\|phtml\|phps\|php[0-9])$` reddi; 404 değil 403 bilinçli (dosyanın var olduğunu ama çalıştırılmadığını söyler — asıl teşhis budur). Canlı nginx'te uçtan uca ölçüldü: `secret.php` 403 ve gövde nginx hata sayfası, `.well-known/acme-challenge` 200 (SSL yenileme sağlam), `.env` 403, olmayan dosya 404. 2 regresyon testi. A6 ile aynı aile — web kökündeki dosyanın kaynak olarak dağıtılması |
| A7 | **Tarama önbelleği katalog alanlarını saklıyordu**: `service_scan_cache` tüm API yanıtını (ad, açıklama, ikon, kategori, paket adları) tutuyordu → kodda değişen her katalog alanı, biri "Tara"ya basana dek ekranda eski hâliyle kalıyordu; katalogda YENİ eklenen servis hiç görünmüyordu. Kendi kodundaki gerçeği önbellekten okuyan bir panel | `managed_service_handlers.go` (eski 62-79, 232-243) | ✅ Kapandı (21 Tem): önbellek yalnız `serviceObservation` saklar (kurulu mu / unit ayakta mı / sürümler / config'ler); katalog her okumada `catalogView` ile birleşir — yanıtın kurulduğu TEK yer orası, önbellekli okuma ile taze tarama farklı yanıt veremez. Eski biçim okunmaya devam eder (tarama beklemeden doğru durum). 3 bekçi test. Bu, üründe `kind:""` olarak patladı — aşağıdaki E notu |
| A6 | **vhost nokta-dosya kuralı iki dalda da yanlıştı**: PHP dalı yalnız `/\.ht` engelliyordu → `https://site/.env` düz metin (Laravel/Symfony DB parolası, APP_KEY, posta bilgileri) ve `.git/` açıktı; statik dal ise TÜM nokta dosyalarını engelliyordu → `.well-known/acme-challenge` kapalı, yani statik sitenin ilk sertifikası ve her yenilemesi 403 | `internal/services/templates/nginx/vhost.conf.tmpl` | ✅ Kapandı (20 Tem): iki dal da `location ~ /\.(?!well-known).*`; regex canlı nginx'te doğrulandı (commit `0088c67`). Golden path PHP sitesiyle kanıtlandığı için ACME kırığı görünmez kalmıştı |

## B. Yapısal borç (reçete — sırayla ödenir)

| # | İş | Neden | Tahmin | Durum |
|---|---|---|---|---|
| B0 | **Kanamayı durdur**: A1+A2 düzeltmeleri, ölü kod gömme | İlk müşteri kırık sayfa görmesin | 1-2 gün | ✅ 11 Tem |
| B1 | **Tek API**: v2'yi v1'e katla; kiracı kapsamı auth'tan; OpenAPI üret; ön yüz tipleri üretilen istemciden | A3+A5 sınıfı hatalar derlemede ölür; 74 ham `fetch(` tek katmana iner; "API-first" sözü gerçek olur | 3-5 gün | 🔶 Kısmi (18 Tem): A3 kiracı kapsamı + A4 parola şifreleme + rol ayrımı/admin kilidi + v2→v1 birleştirme + **hata sözleşmesi** KAPANDI. Sözleşme {error,code?,action?}: 11 kodlu ret, tek writeCodedError yazıcı; ön yüzde readApiError tek okuma yolu + ErrorBanner (action→panel-içi düğme), i18n err.* TR+EN, 13 dağınık res.text() tekleşti. Canlı: WEB_SERVER_REQUIRED/ADMIN_ONLY/AUTH_REQUIRED iki sunucuda doğru zarf+action. Kalan alt dilim: OpenAPI + üretilen istemci |
| B2 | **Route+authz tablosu**: `{yol, handler, roller}` tek veri yapısı | `main.go`'daki 72 `HandleFunc` + `middleware.go:117-141` elle listesi = unutulan satır sessiz authz deliği | 1 gün | 🔶 Fail-closed rol öne çekilip kapandı (17 Tem: okunamayan kullanıcı = geçersiz oturum, boş rolle ilerleme yok). Tablo + rol×uç matris testi açık |
| B3 | **Bilgi tek yerde**: servis kataloğu config/port/paketin TEK sahibi; scanner kataloğu okur | `managed_services.go` ↔ `service_scanner.go:93` çifti kanıtlı ıskaladı (pdns config, 10 Tem). İkinci kanıt (16 Tem, Arch): Hostinger Arch imajı devre dışı bir `named.service` ile geliyor → capabilities `dns_server: "bind"` diyor, kurulum yolculuğu "DNS kuruldu: Done" gösteriyor, ama Services sayfası 0/0 — iki tespit yolu (InstalledServiceIDs ↔ GetServices) aynı soruya farklı cevap veriyor; ayrıca "kurulu ama koşmuyor" bir DNS, zone sunamadığı hâlde Done sayılıyor | 1 gün | 🔶 Kısmi (21 Tem): **tür ayrımı** kapandı — her katalog kalemi `Kind` bildirir (15 service / 3 tool / 1 runtime), `Daemonless = len(SystemNames)==0` sezgisi silindi, satır çizimi türe dallanır; 2 bekçi test. **Önbellek/katalog ayrımı** kapandı (A7): katalog tek sahiptir, önbellek yalnız gözlem saklar. Kalan: scanner'ın kataloğu okuması (iki tespit yolunun tekleşmesi), sürüm çekmecesi, Sury deposu, Node.js kataloğa |
| B4 | **UI disiplini**: tek Button (CtrlButton/ActionIcon ölür), paylaşılan `fmtBytes` (5+ kopya), `confirm()` → temalı modal (8+ yer), tek Service tipi (`api.ts:13` / `ServiceList.tsx:8` / `Dashboard.tsx:28` üçüzü) | Tutarsızlık kullanıcıya sızıyor; kopyalar ayrı ayrı çürüyor | 2 gün | ⬜ |
| B5 | **Dürüstlük borcu**: golden-path smoke CI (build + stub ekran + kritik uçlar) | 29k satıra 9 test dosyası, CI YOK; anayasanın kendi kuralı ihlalde | başlangıç 1 gün, sonra sürekli | 🔶 Taban kondu (11 Tem): `.github/workflows/ci.yml` — her push/PR go build+vet+test + web tsc+build. İlk seed test `database_v2_driver_test.go` (A1 regresyon muhafızı). Kalan: stub render + kritik-uç smoke + <100ms ölçüm |

**Sıralama zorunlu kısıtı:** v0.3 (ilk gerçek kiracılar) B1 bitmeden BAŞLAYAMAZ.

## C. Gömülenler ve kalan kokular

- ✅ Gömüldü (11 Tem): `internal/repositories/database_repository.go` (sıfır referanslı ölü), kökteki `KONUSMA-GECMISI.md` (sohbet dökümü; tarih git'te), diskteki `ServiceList.tsx.backup`.
- ⬜ Koku: **şablon değişimi mevcut vhost'lara yansımıyor** (config drift) — A6 düzeltmesi yalnız YENİ üretilen vhost'ları kapsıyor; mevcut siteler eski kuralla kalıyor. "Tüm vhost'ları yeniden üret" yolu yok. Güvenlik düzeltmesi taşıyan her şablon değişikliğinde bu bir sessiz açık kalır.
- ⬜ Koku: `cmd/panel` 66 dosyalık tek düz paket (13.658 satır) — B1/B2 sırasında doğal bölünür; ayrı bir "büyük yeniden paketleme" YAPMAYIN (churn riski kazancından büyük).
- ⬜ Koku: `docs/CelikPanel Pano.html` 813KB blob — tasarım referansı; bilinçli tutuluyor, büyürse LFS/link.
- ⬜ Koku: `en.ts` 900+ anahtar tek dosya + **dizgide kesme işareti derlemeyi bozar** — katkı tuzağı; B4 sırasında en azından belgeli lint.
- ⬜ Koku: `cmd/debug_mariadb/main.go` — hiçbir yerden referanssız debug binary'si (`go build ./...` yine de derliyor). Ölü; gömülecek adaylardan (11 Tem taramasında bulundu).
- ⬜ Koku: agent'ta 128 `exec.Command`, 0 interface — ROADMAP'teki BSD notunun "kod zaten böyle yazılıyor" cümlesi iyimser; taşınabilirlik iddiası RPC yüzeyiyle sınırlı. Yeni RPC yazan: exec'leri fonksiyon sonuna toplayın, "ne/nasıl" ayrımını gerçekten uygulayın.

## D. Felsefe ihlalleri (anayasa vs. kod)

1. **"Dürüstlük kuralı"** (test+güvenlik+doküman yoksa bitmedi) — en çok ihlal edilen madde bizzat bu: 9 test dosyası / 789 satır, CI yok; fazlar yine de kapandı. → B5.
2. **"Tek bariz yol"** — iki API sürümü, üç düğme sistemi, iki config yönetimi. → B1+B4.
3. **"API-first"** — OpenAPI yok, tipler elle üç kez. → B1.
4. **"Hız <100ms"** — tek ölçüm yok. → B5 smoke'una bir ölçüm satırı.
5. **D-009 gerilimi** (panel=DNS otoritesi): tutarlı ve savunulur, ama dış-DNS'li müşteri kitlesini kapıda çevirir — v0.3 pazara açılmadan bilinçli yeniden tartılmalı (karar değişmese bile kayıt güncellensin).

## E. Öz eleştiri (kayda geçsin)

Bu borcun bir kısmı taze ve denetimi yazan masadan çıktı (10 Tem tasarım turu):
üç düğme sisteminin ikisi, Dashboard'daki 7 ham fetch ve bir hatayı maskeleyen
sadakatsiz test stub'ı. Ders `tools/dev-preview/preview-server.py` başlığına
gömüldü: *stub gerçek şemayı tipleriyle taklit eder.*

**21 Tem — A7'yi ben ürettim ve neredeyse kapalı gözle geçiyordum.** B3'ün ilk
diliminde katalogdaki 19 kaleme `Kind` ekledim; yerel testler geçti, derleme
temizdi, iki sunucuya dağıttım. Canlıda API `kind:""` döndürdü — hepsinde.
Yerelde doğru, üründe boş. Sebep eklediğim alan değil, alanın yanından geçtiği
önbellekti.

İki ders kayda geçsin:

1. **"Testler geçti" ≠ "üründe çalışıyor".** Testlerim kataloğu doğrudan
   okuyordu; ürün yolu ise DB'den geçiyordu. Aradaki katmanı hiçbir test
   geçmiyordu. Canlı doğrulama olmasaydı bu, sessizce yanlış çizilen bir
   arayüz olarak kalırdı — operatörün "1 ay sonra yok bunu unutmuşuz"
   dediği tam bu sınıf.
2. **Kolay cevap yanlış cevaptı.** "Bir kez Tara'ya basın, düzelir" doğru ve
   yeterliydi — o an için. Ama hata `kind`'e özgü değildi: her katalog
   düzeltmesinde geri gelecekti ve her seferinde aynı cümleyle geçiştirilecekti.
   Kalıcı düzeltme, önbelleğin ne saklamaya HAKKI olduğunu sormaktı: yalnız
   gözlem. Kodda yaşayan bir gerçeği önbelleğe almak, kod ile ekranın
   birbirinden ayrı düşme biçimidir.

**Tekrar eden yöntem hatası: tek örnekle doğrulama.** Bu projede aynı şekil
şimdiye dek üç kez çıktı ve üçünde de "çalışıyor" dedikten sonra kırıldı:

| Ne tek örnekle doğrulandı | Ne görünmez kaldı |
|---|---|
| Golden path **PHP sitesiyle** kanıtlandı | Statik sitenin ACME doğrulaması hiç çalışmıyordu (A6) — ve statik dalın `.php` kaynağını sızdırdığı (A8) |
| Vendor deposu **yalnız PGDG ile** kanıtlandı | Zırhlı-anahtar varsayımı, sondaki-sayı sıralaması, "zaten kurulu" duvarı, companion ihtiyacı — dördü de PHP'de kırıldı (D-007 düzeltmesi) |
| Katalog alanları **hep var olan alanlarla** okundu | Yeni bir alan (`Kind`) eklenince önbellek onu boş yayınladı (A7) |

Ders, "daha çok test yazın" değil — daha keskin: **bir mekanizmayı tek örnekle
doğrulamak, o örneğe özgü olanı genel sanmaktır.** İkinci örnek gelene kadar
hangi varsayımın taşıyıcı olduğu bilinemez. Pratik kural: *bir soyutlama
(şablon dalı, vendor deposu, katalog alanı) ikinci somut örneğini almadan
"kanıtlandı" sayılmaz*; ve ikinci örnek eklenirken ilkine göre yazılmış her
cümle yeniden okunmalıdır — D-007'nin "zırhlı anahtar doğrudan kullanılır"
cümlesi tam olarak böyle yanlışa döndü.
