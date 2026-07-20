# CelikPanel Yol Haritası

*Son güncelleme: 20 Temmuz 2026 · [English](ROADMAP.md)*

---

## Anayasa — Her Kararın Süzgeci

Her özellik, her commit, her tasarım kararı bu dört süzgeçten geçer.
Birinden geçemeyen iş bitmiş sayılmaz, ertelenir ya da sadeleştirilir.

### 1. Güvenlik varsayılandır
- Kimlik doğrulamasız hiçbir özellik yayına çıkmaz.
- Varsayılan yapılandırma her zaman en güvenli olandır (localhost bağlama, token, en az yetki).
- Parola/token yalnız `crypto/rand` üretir. SQL yalnız parametrelidir.
- Root yetkili Agent'a yalnız Panel erişebilir — başka hiçbir şey.

### 2. Sadelik (Google ilkesi)
AltaVista portal olmaya çalışırken Google tek arama kutusuyla kazandı.
cPanel/Plesk bugünün AltaVista'sıdır: kalabalık, yavaş, ürkütücü.
- Her işin **tek bariz yolu** olur. İki yol varsa biri silinir.
- Özellik eklemeden önce sor: *"Eklemezsek ne kaybederiz?"* Cevap net değilse eklenmez.
- Kurulu olmayan servis arayüzde **görünmezdir**. Boş ekran yok, pasif menü yok.
- Akıllı varsayılanlar: sormadan doğrusunu yap; düğme isteyen %5 için "gelişmiş" bölümü.

### 3. Hız
- Panel API yanıt hedefi: < 100 ms. Arayüz etkileşimleri: anında.
- Kurulum hedefi: **60 saniye** (v0.1 `install.sh`'ı teslim etti; hedef korunuyor).
- Tek statik binary; dış bağımlılık eklemek yasaktır (bu bir özelliktir — koruruz).

### 4. Esneklik
- Her şey önce API'dir; arayüz onun tüketicilerinden yalnızca biridir.
- Servisler modülerdir: müşteri istediğini, istediği sürümde kurar.
- Veri asla rehin tutulmaz: yedekler standart biçimde (tar.gz, SQL dump), dışa aktarım her zaman mümkün.

### Dürüstlük kuralı
Önceki dönemin hatası tekrarlanmayacak: **"çalışıyor" ≠ "bitti".**
İş ancak üçü birdenle biter: test + güvenlik incelemesi + dokümantasyon.
Her sürümün ölçülebilir çıkış ölçütü vardır; karşılanmadan sonrakine geçilmez.

---

## Sürüm Merdiveni

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

### ✅ v0.1 — Güvenli Çekirdek + Kanıtlı Golden Path *(3–10 Temmuz 2026 — mevcut sürüm, v0.1.0)*
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

### 🔶 v0.2 — Alfa Tamam: Debian Yeniden-Kanıtı *(← BURADAYIZ, sürüyor)*
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
- CI güvenlik kapıları: `gosec`, `govulncheck`, `npm audit --audit-level=high` her PR'da; istisnalar
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

## Neredeyiz — 20 Temmuz 2026

**Sürüm:** v0.1.0 alfa (henüz tag'siz — v0.2.5'te tek kaynağa bağlanacak), üretim VPS'inde canlı
(Debian 13, yalnız-panel kurulum). İki test sunucusu: boston (Debian 13) + frankfurt (Arch — bilerek,
D-004 Ek: paket katmanı çeşitliliği için). İki panel de gerçek Let's Encrypt sertifikası ve açık
firewall'la (varsayılan-reddet) çalışıyor; yenileme provaları iki sunucuda da kanıtlı.
**Merdivendeki yer:** v0.2 sürüyor — PowerDNS panelden kuruldu ve celikhost.com'u dünyaya sunuyor;
sıradaki tıklama otomatik onarım, sonra ilk gerçek site.
**Borç durumu (AUTOPSY):** A1–A4 kapalı (A3'ün admin kilidi B1'e bağlı duruyor), A5 açık ·
B0 tamam, B1 ve B5 kısmi, B2–B4 açık.
**Bu güncelleme:** operatörün üç yeni gereksinimi (panel-içi yardım, bayi+müşteri deneyimi, ücretsiz
katmanlı plan sistemi) merdivene basamak basamak işlendi; abonelik makinesine giriş kadar **çıkış** da
yazıldı (iptal, dönem modeli, ödeme defteri, süre bildirimleri, iade/chargeback, bayi tahsilatı,
havuz zinciri); v0.35 "Plan Dürüstlüğü" ara basamağı eklendi; v0.4–v0.9 üretim güveni ve sürüm
mühendisliği kalemleriyle genişletildi; hedef-dışılar gerekçeleriyle tahkim edildi.
**Karne (11 Tem ölçümü, yeniden ölçülmedi):** özellik kodu ≈ v1.0 kapsamının %70'i · kanıtlar ≈ %45
(Ubuntu tam, Debian kısmi) · cila/tasarım ≈ %80 · dokümantasyon ≈ %60 · dış doğrulama ≈ %0 (v0.6'da başlar).
**Bugünkü sistem:** ~29 bin satır Go (151 dosya, 72 HTTP ucu, 38 agent RPC dosyası, 15 migration,
19 servislik katalog) + ~16,5 bin satır TypeScript (55 bileşen dosyası, TR+EN).

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
