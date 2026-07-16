# CelikPanel Yol Haritası

*Son güncelleme: 11 Temmuz 2026 · [English](ROADMAP.md)*

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

### 🩺 v0.2.5 — Borç Ödeme (Otopsi reçetesi)
11 Tem 2026 adli denetimi ([AUTOPSY](docs/AUTOPSY.tr.md)) canlı kırıklar ve yapısal borç çıkardı;
karar: **refaktör, rewrite değil.** B0 (kanamayı durdur: ölü TypeID sabitleri, admin-olmayana kırık
Databases sayfası, ölü kod) aynı gün kapandı. Kalanı sırayla: **B1** tek API (v2→v1, kiracı kapsamı
auth'tan, OpenAPI + üretilen istemci) · **B2** route+authz tablosu · **B3** servis bilgisinin tek
sahibi katalog · **B4** UI disiplini (tek Button/fmtBytes/modal) · **B5** golden-path smoke CI.

**Çıkış ölçütü:** AUTOPSY B1–B5 kapalı. **Sert kısıt: v0.3, B1 bitmeden başlayamaz.**

### v0.3 — Çok Kiracılı Gerçeklik
Birden fazla kiracıya utanmadan satabilmek:
- Bayi havuz kotaları · `additional_user` rolü · OS seviyesinde disk/trafik uygulaması (ROLES.md ertelenenleri)
- cPanel içe aktarıcının **gerçek** müşteri arşiviyle kanıtı (DB kullanıcıları dahil)
- WordPress Toolkit derinliği: güncelleme, sertleştirme, klon/staging
- FTP (vsftpd) uçtan uca bağlı · dosya yöneticisi cilası
- **Webmail (Roundcube)** — posta yığını var ama müşterinin tarayıcıdan postasına
  bakacağı yer yoktu; cPanel'den gelen herkesin ilk arayacağı şey (11 Tem eklendi)

**Çıkış ölçütü:** bir bayi + iki müşteri bir hafta self-servis işliyor; gerçek bir cPanel hesabı tek tıkla taşınıyor.

### v0.4 — İşletim Güveni
Operatörün gece 3'te ihtiyaç duyduğu şeyler:
- İzleme + uyarı (servis düştü, disk doldu, sertifika hatası → posta/webhook) · panelde log görüntüleyici
- Uzak yedek hedefleri (S3/FTP) + ürün özelliği olarak geri yükleme tatbikatı
- **İkincil DNS gerçeği:** bugün ns1/ns2 aynı makineyi gösteriyor (tek nokta arızası);
  ikinci ucuz VPS'e secondary PowerDNS (AXFR) ya da dürüst belgeleme (11 Tem eklendi)
- Arayüzden tek tık panel güncellemesi (update.sh'ın ön yüzü) · WebSocket canlı bildirimler
- **Temiz başlangıç:** panel, kendisinin kurmadığı katalog servislerini "yabancı" diye işaretler
  (audit log'da `service.install` kaydı olmayan her katalog servisi) ve operatör onayıyla kaldırmayı
  önerir — sahiplen ya da tahliye et, Import felsefesinin aynası. Saha kanıtı (16 Tem): Hostinger
  Arch imajı uyuyan bir bind ile geldi; kurulum yolculuğu "DNS kuruldu: Done" saydı. B3 üstüne oturur.

**Çıkış ölçütü:** öldürülen servis bir dakikada uyarı üretiyor; uzak yedekten tam geri yükleme temiz VPS'te başarılı.

### v0.5 — Güvenlik Derinliği
- WAF kararı (ModSecurity ya da dürüst alternatif) · fail2ban derin entegrasyonu · ClamAV zamanlanmış site taramaları
- 2FA secret'ının şifreli saklanması · dış güvenlik denetimi · dedicated IP tesisatı (satılabilir `extra_ip`)

**Çıkış ölçütü:** dış denetim yüksek önem bulgu vermiyor; bir müşteri panelden dedicated IP satın alıp kullanıyor.

### v0.6 – v0.9 — Beta Programı
- OpenAPI dokümantasyonu (API-first sözünün kanıtı) · yönetici ve kullanıcı kılavuzları TR+EN
- Gerçek dış beta kullanıcıları; onların duvarları düzeltme olur (alfa modeli, ölçeklenmiş)
- Performans hedefleri ölçülüp uygulanıyor (<100 ms API) · lisans/iş modeli kararı
  (öneri: open core) → repo görünürlüğü buna göre

**Çıkış ölçütü:** ≥3 dış operatör ≥1 ay gerçek site işletiyor; dokümantasyon sorularını bizden önce cevaplıyor.

### 🎯 v1.0 — Genel Çıkış
- Temiz VPS → dakikalar içinde çalışan panel, dokümante, kendi kendini güncelleyen
- "Domain → canlı site" arka arkaya 100 kez hatasız (Faz 1 sözü, artık CI'da)
- cPanel'den taşınma defalarca kanıtlı · fiyatlandırma/lisans yayında

### 1.0 Sonrası — ufuk *(talep sürer, hayal sürmez)*
Çoklu sunucu, BSD agent arka ucu, faturalama entegrasyonları (WHMCS vb.) — her biri ancak
gerçek talep varsa, aşağıdaki bilinçli hedef-dışılara uygun biçimde. Pazar yeri asla (AltaVista hatası).

---

## Neredeyiz — 11 Temmuz 2026

**Sürüm:** v0.1.0 alfa, üretim VPS'inde canlı (Debian 13, yalnız-panel kurulum).
**Merdivendeki yer:** v0.2'nin başı — Debian yeniden-kanıtı bir alfa oynanışı olarak sürüyor:
PowerDNS panelden kuruldu ve yönetiliyor; sıradaki tıklama otomatik onarım, sonra ilk domain.
**Karne:** özellik kodu ≈ v1.0 kapsamının %70'i · kanıtlar ≈ %45 (Ubuntu tam, Debian kısmi) ·
cila/tasarım ≈ %80 (tüm sayfalar yeni sistemde) · dokümantasyon ≈ %60 · dış doğrulama ≈ %0 (v0.6'da başlar).
**Bugünkü sistem:** ~29 bin satır Go (151 dosya, 72 HTTP ucu, 38 agent RPC dosyası, 15 migration,
19 servislik katalog) + ~16,5 bin satır TypeScript (54 bileşen, TR+EN).

---

## Bilinçli Hedef-Dışılar

Sadelik hayır diyebilmektir. Bunlar **bilerek** yok:

- ❌ Docker/konteyner katmanı — hedef pazar klasik hosting; doğrusu native.
- ❌ Akla gelen her servise yönetim ekranı — kurulu olmayan görünmezdir.
- ❌ Tema/görünüm pazarları, portal vitrinleri — AltaVista hatası.
- ❌ Çoklu sunucu / cluster (şimdilik) — tek sunucu kusursuz olmadan dağıtık sistem hayali yok.
- ❌ Panelin kendisi için dış bağımlılık (Redis, harici DB, mesaj kuyruğu) — tek binary + SQLite kalır.
- ❌ BSD desteği (şimdilik) — **ama seçenek bilinçle korunuyor ve asla fork olarak değil.** Panel↔agent RPC sözleşmesi tasarım gereği OS-nötr: panel (HTTP/SQLite/UI/iş mantığı) zaten FreeBSD'ye çapraz derlenen taşınabilir Go; yalnız agent'ın "elleri" (systemd/apt/nftables) Linux'a özgü. Gerçek talep doğarsa (örn. hosting'çileri BSD'ye iten bir Linux güven krizi) hamle, aynı RPC yüzeyinin arkasına BSD agent arka ucu koymaktır — haftalarla ölçülen iş, tek ürün. İki CelikPanel asla olmayacak. Bunu ucuz tutan disiplin: yeni agent özellikleri "ne"yi (RPC yüzeyi) "nasıl"dan (exec çağrıları) ayrı tutar — kod zaten böyle yazılıyor. *(Karar: 8 Temmuz 2026.)*
