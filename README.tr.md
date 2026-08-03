<div align="center">

# CelikPanel

**Yeni nesil web hosting kontrol paneli. Tek binary. Sıfır bağımlılık. 60 saniyede kurulum.**

[English](README.md) · [Yol Haritası](ROADMAP.tr.md) · [Kullanıcı Rolleri](docs/ROLES.tr.md)

</div>

---

CelikPanel, cPanel ve Plesk'e modern bir alternatiftir: gömülü React arayüzü ve SQLite depolamasıyla statik derlenmiş **tek bir Go binary'si**. Çalışmak için başka hiçbir şeye ihtiyaç duymaz — harici veritabanı yok, web sunucusu yok, yorumlayıcı yok.

## Neden bir panel daha?

cPanel ve Plesk bugün aynı şirkete ait, fiyatlar her yıl artıyor ve ürünler yirmi yıllık mirası sırtında taşıyor: saatler süren kurulumlar, zorunlu bağımlılıklar, eski varsayılanlar, kalabalık arayüzler.

Bizim cevabımız, Google'ın AltaVista'ya verdiği cevap: **radikal sadelik ve hız.** AltaVista portal olmaya çalıştı ve kaybetti; Google tek arama kutusuyla kazandı. cPanel/Plesk bugünün portalları. CelikPanel, arama kutusu.

| Eski yol | CelikPanel yolu |
|---|---|
| Saatler süren kurulum | Tek komut, ~60 saniye *(hedef)* |
| Panel yanında MySQL, PHP, Perl sürükler | Tek Go binary + SQLite — sıfır bağımlılık |
| Eski servis sürümleri dayatılır | Her zaman işletim sistemi deposundaki en güncel sürüm; sürümü müşteri seçer |
| Her şey baştan kurulur | Modüler: yalnızca ihtiyaç duyulan servis, arayüzden kurulur |
| Kalabalık portal arayüzü | Hızlı SPA; kurulmayan servis arayüzde görünmez |

## İlkeler

Her özellik, her commit, her tasarım kararı şu dört süzgeçten sırayla geçer:

1. **Güvenlik varsayılandır** — en az yetki, güvenli varsayılanlar, kimlik doğrulamasız hiçbir şey yayınlanmaz.
2. **Sadelik** — her işin tek bariz yolu. *Hayır* diyebilmek bir özelliktir.
3. **Hız** — 100 ms altı API yanıtı, anlık arayüz, 60 saniyede kurulum.
4. **Esneklik** — önce API, modüler servisler, veriniz asla rehin alınmaz.

## Mimari

```
Tarayıcı — React SPA
   │  HTTPS
   ▼
Panel — Go HTTP sunucusu (port 2083), düşük yetkili kullanıcı, SQLite
   │  yerel RPC (Faz 0'da Unix socket + token'a geçiyor)
   ▼
Agent — root daemon; işletim sistemine dokunabilen tek bileşen
   ▼
Yönetilen servisler: Nginx · PHP-FPM 8.x · MariaDB · PostgreSQL ·
Postfix · Dovecot · PowerDNS · Fail2ban · vsftpd · Redis · …
```

Yetki ayrımı bilinçli bir karar: internete bakan Panel asla root çalışmaz. Root yetkisi yalnızca — sadece yerel makineden erişilebilen — Agent'tadır. Bu, panellerin klasik "web katmanından root'a" istismar sınıfını mimari düzeyde engeller.

## Durum — v0.1.0 alpha

> ⚠️ **Üretime hazır değildir.** Faz 0 güvenlik sprinti (kimlik doğrulama, agent kilitleme, injection düzeltmeleri) devam ediyor. Paneli henüz internete açmayın.

**Bugün çalışanlar** (işlevsel, sertleştirme sürüyor): domain ve site yönetimi · PHP sürüm seçimi ve FPM havuzları · SSL (Let's Encrypt + özel sertifika) · DNS (PowerDNS) · e-posta hesapları ve yönlendirme · çoklu sunucu destekli veritabanı yönetimi (MariaDB/PostgreSQL) · dosya yöneticisi · yedekleme/geri yükleme · cron · log görüntüleme · 14 servis için servis kontrolü.

**Sırada ne var:** [Yol Haritası](ROADMAP.tr.md) — Faz 0 güvenlik sprinti → Faz 1 altın yolun sertleştirilmesi → Faz 2 60 saniyelik kurulum → Faz 3 WordPress toolkit + cPanel importer.

## Etiketli sürüm kurulumu

Temiz kurulumun desteklenen girdisi Git checkout'u veya operatöre özel bundle
değil, önceden derlenmiş sürüm arşividir. Eşleşen panel, agent ve web uygulaması
arşivde bulunduğu için hedef sunucuda Go, Node veya Git gerekmez.
Mevcut alpha arşivi Linux x86_64/amd64 içindir.

Yayımlanan sürümler `https://celikpanel.net` adresindeki herkese açık CelikPanel
indirme kanalından dağıtılır. Bootstrap betiğini temiz ve desteklenen sunucuda
root olarak çalıştırın. Betik seçilen değişmez arşivi ve sağlama toplamını HTTPS
üzerinden indirir; SHA-256 özetini ve arşiv yollarını doğruladıktan sonra
paketteki kurucuyu çalıştırır.

```bash
# Yayımlanan son sürüm
curl --fail --show-error --location --proto '=https' --tlsv1.2 \
  https://celikpanel.net/get.sh -o /tmp/celikpanel-get.sh
sh /tmp/celikpanel-get.sh

# Veya tam bir değişmez sürümü sabitleyin
sh /tmp/celikpanel-get.sh --version v0.1.0-alpha.1
```

Kurucu ilk yöneticiyi etkileşimli olarak oluşturur. Yönetici parolasını shell
geçmişine, dağıtım betiklerine veya sürüm dosyalarına koymayın. Sağlama toplamı
değişen baytları saptar fakat yayıncı kimliğini kanıtlamaz. Sürüm arşivleri ve
makinece okunabilir manifestler `https://celikpanel.net/releases/` altında
kalıcıdır; aşağıdaki imzalama sınırına da bakın.

## Kaynaktan derleme

Gereksinimler: tam Go 1.26.5, Node ≥ 20. Make kapısı derleyicinin tam sürümünü
doğrular, temiz bir `GOTOOLCHAIN=local` ortamı kullanır ve başka bir Go araç
zincirini sessizce indirmez.

Mühürlü derleme önbelleği Go 1.26.5'ten eski olan mevcut kurulumlar önce
[operasyon kılavuzundaki](docs/OPERATIONS.tr.md) incelenmiş checkout kanıtını
ve tek seferlik geçiş sırasını izlemeli, ardından uygun güncelleme modunu
çalıştırmalıdır. Ayrıcalıklı geçiş betiğini doğrulanmamış bir checkout'tan
çalıştırmayın.

Geçiş betiği yalnız özel derleme araç zinciri önbelleğini değiştirir;
CelikPanel servislerine, veritabanlarına, DNS kayıtlarına veya panel ayarlarına
dokunmaz.

```bash
# Backend (panel + agent)
make panel agent

# Frontend
cd web && npm ci --no-audit --no-fund && npm run build   # çıktı: web/dist, panel binary'si tarafından sunulur
```

## Sürüm ürünleri

`make dist VERSION=<sürüm>` paneli, agent'ı, şema köprüsünü ve web
uygulamasını tek bir deterministik arşivde toplar. Arşivin yanına ayrıca
harici bir `SHA256SUMS` biçimli dosya yazar:

```bash
make dist VERSION=v0.3.0
sha256sum -c dist/celikpanel-v0.3.0.tar.gz.sha256
```

Sürüm operatörü korumalı bir GPG imzalama anahtarını hazırladıktan sonra,
ayrık imzayı şu şekilde oluşturup doğrular:

```bash
make dist-sign VERSION=v0.3.0 SIGNING_KEY=<tam-anahtar-parmak-izi>
gpg --verify dist/celikpanel-v0.3.0.tar.gz.asc dist/celikpanel-v0.3.0.tar.gz
```

Sağlama toplamı bayt bütünlüğünü kanıtlar; yayıncı kimliğini kanıtlamaz.
Sürüm sahibi CI imzalama kimliğini hazırlayıp doğrulama anahtarını
yayımlamadan açık veya ticari bir sürüm, imzalı kaynak iddiasında bulunmamalıdır.

## Belgeler

- [CelikPanel AI Agent](docs/CELIKPANEL-AI-AGENT.tr.md) — yalnız panel kapsamı, onay, denetim ve abonelik kapısı planı
- [Component Manifest V2](docs/COMPONENT-MANIFEST-V2.tr.md) — imzalı SQLite/JSON tarifleri ve platform adaptörü sınırı
- [Mağaza](docs/STORE.tr.md) — teklif kataloğu, hak sınırı ve operatör iş akışı
- [Sistem SQLite Yönetimi](docs/SYSTEM-SQLITE-ADMIN.tr.md) — panele ait veritabanlarının sınırlı inceleme, yedekleme ve bakımı
- [Web Bağımlılık Güvenliği](docs/WEB-DEPENDENCY-SECURITY.tr.md) — kilit dosyası, denetim ve bağımlılık güncelleme politikası
- [Yol Haritası](ROADMAP.tr.md) — neredeyiz, nereye gidiyoruz ve bilinçli olarak neleri yapmayacağız
- [Karar Defteri](docs/DECISIONS.tr.md) — kalıcı mimari ve ürün kararları
- [Otopsi ve Borç Defteri](docs/AUTOPSY.tr.md) — doğrulanmış arızalar, kalan kokular ve düzeltme durumu
- [Operasyonlar](docs/OPERATIONS.tr.md) — kurulum, güncelleme, geri alma ve kurtarma prosedürleri
- [Dağıtım Desteği](docs/DISTRO-SUPPORT.tr.md) — üretilen platform destek sözleşmesi
- [Kullanıcı Rolleri ve Yetkiler](docs/ROLES.tr.md) — Yönetici / Bayi / Müşteri / Ek Kullanıcı modeli
- [Frontend Mimarisi](docs/UI_ARCHITECTURE.tr.md) — tek kalıtımsal kabuk, role göre yetki-yönlendirmeli
- [Kurallar](docs/CONVENTIONS.tr.md) — dil ve isimlendirme: İngilizce isimler, iki dilli içerik (TR + EN)
- [Güvenlik Politikası](SECURITY.md) — özel bildirim kanalı ve güvenli araştırma sınırları
- [Teknik ve Üçüncü Taraf Bildirimi](NOTICE) — bağımlılık ve dağıtım yükümlülükleri

## Lisans

Henüz kararlaştırılmadı; lisans modeli (açık kaynak / open core / ticari) değerlendirilirken depo private tutuluyor.
