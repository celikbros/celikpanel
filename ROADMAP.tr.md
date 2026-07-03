# CelikPanel Yol Haritası

*Son güncelleme: 3 Temmuz 2026 · [English](ROADMAP.md)*

---

## Anayasa — Her Kararın Süzgeci

Her özellik, her commit, her tasarım kararı şu dört süzgeçten geçer.
Birine takılan iş yapılmaz, ertelenir ya da basitleştirilir.

### 1. Güvenlik varsayılandır
- Hiçbir özellik kimlik doğrulamasız yayınlanmaz.
- Varsayılan yapılandırma her zaman en güvenli olandır (localhost bind, token, en az yetki).
- Parola/token üretimi yalnızca `crypto/rand`. SQL yalnızca parametrize.
- Root yetkili Agent'a yalnızca Panel erişebilir — başka hiçbir şey.

### 2. Sadelik (Google ilkesi)
AltaVista portal olmaya çalışırken Google tek arama kutusuyla kazandı.
cPanel/Plesk bugünün AltaVista'sı: kalabalık, yavaş, korkutucu.
- Her işin **tek bariz yolu** olur. İki yol varsa biri silinir.
- Yeni özellik eklerken önce sorulur: *"Bunu eklemezsek ne kaybederiz?"*
  Cevap net değilse eklenmez.
- Kurulu olmayan servis arayüzde **görünmez**. Boş ekran, pasif menü olmaz.
- Akıllı varsayılanlar: kullanıcıya soru sormadan doğru olanı yap;
  ayar isteyen %5 için "gelişmiş" bölümü yeterli.

### 3. Hız
- Panel API yanıtı hedefi: < 100 ms. Arayüz etkileşimi: anlık.
- Kurulum hedefi: **60 saniye** (aşağıda Faz 2).
- Tek statik binary; harici bağımlılık eklemek yasak (bu bir özellik, koruyacağız).

### 4. Esneklik
- Her şey önce API'dir; arayüz yalnızca API'nin bir tüketicisidir.
- Servisler modülerdir: müşteri istediğini kurar, istediği sürümü seçer.
- Veri rehin alınmaz: yedekler standart formatta (tar.gz, SQL dump), dışa aktarım her zaman mümkün.

### Dürüstlük kuralı
Önceki dönemin hatası tekrarlanmaz: **"çalışıyor" ≠ "tamamlandı"**.
Bir iş ancak şu üçü varsa tamamlanmıştır: test + güvenlik gözden geçirmesi + belge.
Her fazın sonunda ölçülebilir çıkış kriteri vardır; kriter sağlanmadan sonraki faza geçilmez.

---

## Faz 0 — Güvenlik Sprinti 🔴 *(şimdi buradayız)*

> Temel çürükse üstüne kat çıkılmaz. Yeni hiçbir özellik, bu faz bitmeden yazılmaz.

| # | İş | Detay |
|---|----|-------|
| 0.1 | Agent'ı kilitle | TCP `:1977` → Unix socket (0700 izin) + paylaşımlı token doğrulama |
| 0.2 | Panel'e kimlik doğrulama | Session tabanlı login, argon2id parola özeti, güvenli çerez (HttpOnly, SameSite) |
| 0.3 | SQL injection temizliği | `postgresql_driver.go` ve `database_rpc.go`: parametrize sorgu + kimlik doğrulamalı identifier quoting |
| 0.4 | Zayıf rastgelelik | `site_orchestrator.go`: `math/rand` → `crypto/rand` (FTP parolaları) |
| 0.5 | Hata sızıntısı | İç hata mesajları kullanıcıya gitmez; log'a tam, kullanıcıya genel mesaj |
| 0.6 | HTTP sertleştirme | Güvenlik başlıkları, CSRF koruması, API rate limiting |
| 0.7 | Bilinen hatalar | `files_rpc.go` uid/gid dönüşüm hatası; `go vet` sıfır uyarı |
| 0.8 | Sızıntı parolası | `celikpanel_secure_2025` PostgreSQL parolasının değiştirilmesi — **ertelendi** (sunucu henüz dışa açık değil); dışa açılmadan önce zorunlu, bkz. Faz 2 çıkış kriteri |

**Çıkış kriteri:** Oturum açmadan hiçbir API endpoint'i yanıt vermez · Agent'a Panel dışından erişilemez · `gosec` taraması kritik bulgu vermez · `go vet` temiz.

---

## Faz 1 — Altın Yol: Çekirdeği Üretim Kalitesine Getir

> Dar ve derin. 14 servisi yüzeysel yönetmek yerine, tek akışı kusursuz yapmak:
> **domain ekle → site oluştur → PHP hazır → SSL otomatik → site yayında.**

- Bu akışın uçtan uca entegrasyon testleri (temiz Ubuntu 24.04 üzerinde)
- Idempotency: aynı işlem iki kez çalışırsa sistem bozulmaz
- Rollback: akışın herhangi bir adımı başarısız olursa iz bırakmadan geri alınır
- Her işlem audit log'a yazılır (kim, ne zaman, ne yaptı)
- Dashboard gerçek verilerle: CPU, RAM, disk, servis durumu — tek bakışta, sade
- Boş Settings sayfası ya doldurulur ya menüden kaldırılır (sadelik kuralı)
- UI uluslararasılaştırma (i18n): Türkçe + İngilizce öncelikli, çok dilli — bkz. [Kurallar](docs/CONVENTIONS.tr.md)

**Çıkış kriteri:** Temiz sunucuda "domain → yayında site" akışı arka arkaya 100 kez hatasız · Entegrasyon test paketi CI'da yeşil.

---

## Faz 2 — 60 Saniye: Kurulum Deneyimi (Google Anı)

> İlk izlenim tek şansımız. cPanel kurulumu saatler sürüyor;
> bizimki bir kahve karıştırma süresinde bitecek.

- `install.sh`: tek komut → 60 saniyede login ekranı
  (`curl -fsSL https://get.celikpanel.com | bash`)
- systemd unit dosyaları (panel + agent), otomatik başlatma, çökme kurtarma
- İlk açılış sihirbazı: admin parolası + hostname + panel SSL — üç soru, fazlası değil
- Self-update mekanizması (tek binary olmanın meyvesi)

**Çıkış kriteri:** Temiz Ubuntu 24.04 VPS'te komuttan login ekranına ≤ 60 saniye · Yeniden başlatmada her şey kendiliğinden ayağa kalkar · Dışa açılmadan önce tüm sırlar yenilenmiş olmalı (0.8 dahil).

---

## Faz 3 — Kazandıran İki Özellik

> "İyi bir alternatif" ile "cPanel'den geçilen panel" arasındaki fark bu ikisi.

### 3A. WordPress Toolkit v1
Pazarın ~%40'ı WordPress. Plesk'in en değerli özelliği WP Toolkit.
- Tek tuş WP kurulumu (en güncel sürüm, doğru dosya izinleri, hazır DB)
- Güncelleme yönetimi, temel sağlamlaştırma (dosya izinleri, xmlrpc, login koruması)

### 3B. cPanel Importer v1
Hedef müşteri şu an cPanel'de. Tek tuşla taşınamıyorsa fiyat ne olursa olsun gelmez.
- cPanel hesap arşivinden (cpmove/backup) içe aktarım: site dosyaları + MySQL + e-posta hesapları + DNS kayıtları

### 3C. Mail'i ciddiye alma başlangıcı
Mail, panel işinin en zor ve en çok müşteri kaçıran parçası.
- DKIM/SPF/DMARC kayıtlarının otomatik üretimi ve doğrulanması
- Kota yönetimi, temel spam koruması (rspamd değerlendirmesi)

**Çıkış kriteri:** Gerçek bir cPanel hesabı arşivden taşınıp DNS dahil çalışır durumda açılır · WP kurulumu tek tıkla ≤ 30 saniye · Kurulan mail hesabı Gmail'e spam'e düşmeden mail atar.

---

## Faz 4 — Genişleme *(ancak Faz 3'ten sonra)*

- Reseller paneli ve kota/limit uygulaması
- API dokümantasyonu (OpenAPI) — API-first sözünün belgesi
- Lisans/iş modeli kararı (öneri: open core) ve buna göre repo görünürlüğü
- Monitoring/uyarılar, WebSocket bildirimleri
- Multi-server ancak gerçek müşteri talebi doğarsa

---

## Bilinçli Olarak Yapılmayacaklar

Sadelik, hayır diyebilmektir. Şunlar **bilerek** yok:

- ❌ Docker/konteyner katmanı — hedef kitle klasik hosting; native doğru.
- ❌ Her servise ayrı yönetim ekranı kalabalığı — kurulu olmayan görünmez.
- ❌ Tema/görünüm marketi, portal vitrini — AltaVista hatası.
- ❌ Multi-server / cluster (şimdilik) — tek sunucuyu kusursuz yapmadan dağıtık sistem hayali kurulmaz.
- ❌ Panelin kendisine harici bağımlılık (Redis, harici DB, message queue) — tek binary + SQLite kalır.
