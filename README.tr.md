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
Panel — Go HTTP sunucusu (port 1983), düşük yetkili kullanıcı, SQLite
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

## Kaynaktan derleme

Gereksinimler: Go ≥ 1.24, Node ≥ 20.

```bash
# Backend (panel + agent)
go build -o bin/panel ./cmd/panel
go build -o bin/agent ./cmd/agent

# Frontend
cd web && npm install && npm run build   # çıktı: web/dist, panel binary'si tarafından sunulur
```

## Belgeler

- [Yol Haritası](ROADMAP.tr.md) — neredeyiz, nereye gidiyoruz ve bilinçli olarak neleri yapmayacağız
- [Kullanıcı Rolleri ve Yetkiler](docs/ROLES.tr.md) — Yönetici / Bayi / Müşteri / Ek Kullanıcı modeli
- [Frontend Mimarisi](docs/UI_ARCHITECTURE.tr.md) — tek kalıtımsal kabuk, role göre yetki-yönlendirmeli
- [Kurallar](docs/CONVENTIONS.tr.md) — dil ve isimlendirme: İngilizce isimler, iki dilli içerik (TR + EN)

## Lisans

Henüz kararlaştırılmadı; lisans modeli (açık kaynak / open core / ticari) değerlendirilirken depo private tutuluyor.
