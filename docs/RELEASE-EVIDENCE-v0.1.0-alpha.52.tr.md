# Alpha52 Sürüm Kanıtı

*Yayımlanma: 30 Ağustos 2026 · [English](RELEASE-EVIDENCE-v0.1.0-alpha.52.md)*

Bu kayıt `v0.1.0-alpha.52` için incelenmiş kaynağı ve değişmez açık sürüm
ürünlerini tanımlar. Sürüm yayını tek başına kurulum kanıtı değildir; sonraki
node kanıtı ayrıca [LIVE-STATE-2026-08-30.tr.md](LIVE-STATE-2026-08-30.tr.md)
içinde kayıtlıdır.

Bu belgeye kimlik bilgisi, token, özel anahtar, müşteri verisi, özel kanıt yolu
veya ham üretim logu girmez.

## 1. Kanıt durumu

| Durum | Anlam |
|---|---|
| DOĞRULANMIŞ | Belirtilen repo, GitHub veya portal kanıtı kontrol edildi |
| BEYAN EDİLMİŞ | İzlenen sözleşme gereksinimi tanımlar; bu kayıt onu bağımsız kanıtlamaz |
| AÇIK / YENİDEN DOĞRULA | Zorunlu canlı veya harici kanıt henüz yok |
| REPO DIŞI | Sır içermeyen kanıt referansı ve hesap verebilir sorumlu onaylı harici sicilde kalır |

## 2. Kaynak ve inceleme kimliği

| Bilgi | Değer | Durum |
|---|---|---|
| Sürüm | [v0.1.0-alpha.52](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.52) | DOĞRULANMIŞ |
| Release sequence | `52` | DOĞRULANMIŞ |
| Release commit'i | `adb25d8ec487dcb76dd95304a551d8cb37565115` | DOĞRULANMIŞ |
| Etiket hedefi | Annotated etiket yukarıdaki exact release commit'ine çözülür | DOĞRULANMIŞ |
| Etiket imzası | Etiket nesnesi imzasızdır | DOĞRULANMIŞ; etiket ayrıcalıklı update otoritesi değildir |
| Etiketli sürüm workflow'u | [run 33283088681](https://github.com/celikbros/celikpanel/actions/runs/33283088681), başarıyla tamamlandı | DOĞRULANMIŞ |
| Workflow head'i | `v0.1.0-alpha.52` etiketi, `adb25d8ec487dcb76dd95304a551d8cb37565115` commit'i | DOĞRULANMIŞ |
| Yayın zamanı | `2026-08-30T00:30:40Z` | GitHub release metadata'sında DOĞRULANMIŞ |

Ayrıcalıklı update otoritesi Ed25519 imzalı manifest v2, ayrık imzası,
sabitlenmiş public key ve monotonik sequence değeridir. Git etiketi, HTTPS,
`latest.txt` ve checksum dosyaları bu otoritenin yerine geçmez.

## 3. İmzalı manifest kimliği

Yayımlanmış manifest şu exact alanları içerir:

```text
format=celikpanel-release-manifest-v2
sequence=52
version=v0.1.0-alpha.52
commit=adb25d8ec487dcb76dd95304a551d8cb37565115
published_at=2026-08-30T00:19:13Z
os=linux
arch=amd64
archive=celikpanel-v0.1.0-alpha.52-linux-amd64.tar.gz
archive_sha256=9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304
archive_size=22672364
```

| Manifest bilgisi | Sonuç | Durum |
|---|---|---|
| Biçim | `celikpanel-release-manifest-v2` | DOĞRULANMIŞ |
| Hedef | `linux/amd64` | DOĞRULANMIŞ |
| Yetkili arşiv SHA-256 | `9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304` | DOĞRULANMIŞ |
| Yetkili arşiv boyutu | `22.672.364` bayt | DOĞRULANMIŞ |
| Ayrık imza boyutu | 64 bayt | GitHub asset metadata'sında DOĞRULANMIŞ |
| İmzalama otoritesi | Etiketli sürüm CI'ının zorunlu tuttuğu exact tracked Ed25519 public key | Fail-closed workflow başarısıyla DOĞRULANMIŞ; private key repo dışındadır |

## 4. Değişmez GitHub ürün envanteri

Tamamlanan sürümde tam altı açık ürün vardır:

| Ürün | Boyut | GitHub tarafından bildirilen digest |
|---|---:|---|
| `celikpanel-v0.1.0-alpha.52.tar.gz` | 22.672.364 | `sha256:9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304` |
| `celikpanel-v0.1.0-alpha.52.tar.gz.sha256` | 100 | `sha256:5b97485b851165647327b9b5a39247f6c3f40ed912c12fa00b563cded747b09d` |
| `celikpanel-v0.1.0-alpha.52-linux-amd64.tar.gz` | 22.672.364 | `sha256:9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304` |
| `celikpanel-v0.1.0-alpha.52-linux-amd64.tar.gz.sha256` | 112 | `sha256:cd41d02c6cbee742678b93e55fe245a5d4945aa5c5a9cad9f1e7607bfc746a0a` |
| `celikpanel-v0.1.0-alpha.52-linux-amd64.release-manifest-v2` | 332 | `sha256:e6597d9b598f0ab17ab7341b1dcc3591cfd6c6c19e3e13d348201d88ac2c5cdf` |
| `celikpanel-v0.1.0-alpha.52-linux-amd64.release-manifest-v2.sig` | 64 | `sha256:7aeda55121828566931dcd8cf00a3c69dcd38626a044c515d0578d721576aaae` |

Genel ve platform arşivleri aynı boyut ve SHA-256 değerine sahiptir. Sürüm
workflow'u ayrıca [release-signing.md](release-signing.md) içindeki arşiv
bootstrap, iç checksum, build kimliği ve reproducibility sözleşmelerini uygular.

## 5. İndirme portalı yayını

Alpha52 adayı izlenen production publisher ile yayımlanmış, ortaya çıkan portal
durumu imzalı sürüme karşı doğrulanmıştır.

| Kapı | Sonuç | Durum |
|---|---|---|
| İzlenen publisher kullanıldı | `deploy/publish-download-portal.ps1` | DOĞRULANMIŞ |
| Portal hedefi | Alpha52 / sequence 52 / exact imzalı linux-amd64 arşivi | DOĞRULANMIŞ |
| Aday ve imzalı sürüm kimliği | Sürüm, commit, sequence, digest ve boyut uyuşuyor | DOĞRULANMIŞ |
| Açık portal doğrulaması | Atomik değişim sonrasında tamamlandı | DOĞRULANMIŞ |
| Önceki sürümün korunması | Publisher sözleşmesinde zorunlu | BEYAN EDİLMİŞ; saklama referansı REPO DIŞI |

Portal doğrulaması iki sunucudan birinin Alpha52 kurduğunu kanıtlamaz. Kurulu
updater, release floor ve panel/agent kimlikleri her düğümde ayrıca gözlenmelidir.

## 6. Canlı rollout kabulü

Zorunlu önce-Boston-sonra-Frankfurt panel rollout'u tamamlandı. Tarihli canlı
durum kaydı kimliği doğrulanmış ürün ve sınırlı salt okunur host kanıtını taşır;
bu bölüm o kanıtın yerini almadan özetler.

Kullanıcı imzalı panel güncellemesini tamamladıktan sonra her düğüm için şunları
kaydedin:

1. update operation ID, başlangıç/bitiş UTC, süre ve doğrulanmış terminal sonuç;
2. panel ve agent sürümü ile tam commit, şema sürümü ve sunulan UI ürünü;
3. active panel/agent unit'leri ve update aralığındaki sınırlı temiz journal'lar;
4. idle servis, DNS ve self-update işlem durumu;
5. kurulu updater kimliği, release-sequence floor `52`, tam v6 snapshot kimliği
   ve eşleşen rollback yardımcısı;
6. güncel DNS motoru, rol ve değişmez eş kimliği;
7. katalog serial/üyelik, kaynak-bağlı AXFR, UDP/TCP SOA ve PairReady kanıtı;
8. harici HTTPS, TCP/53, UDP/53, delegation ve glue kontrolleri; ve
9. sessizce değiştirmeden firewall, reboot-required ve TLS gözlemleri.

| Düğüm | Beklenen geçiş | Bu kayıttaki güncel kanıt | Durum |
|---|---|---|---|
| Boston | Alpha51 → Alpha52, ardından host/sürüm kabulü | Receipt `b6fd0052b2c4a04b117a753637d68798`; exact build `adb25d8ec487dcb76dd95304a551d8cb37565115`; floor 52; host/sürüm kabulü geçti | DOĞRULANMIŞ |
| Frankfurt | Yalnız Boston geçtikten sonra Alpha51 → Alpha52, ardından host/sürüm kabulü | Receipt `b85dee68b54a01689333112ae8ccaa5f`; exact build `adb25d8ec487dcb76dd95304a551d8cb37565115`; floor 52; host/sürüm kabulü geçti | DOĞRULANMIŞ |
| Düğümler arası DNS | BIND primary ve PowerDNS secondary exact serving pair olarak kalır | Zone öncesi katalog serisi `1`, boş üyelik ve iki yönde source-bound AXFR geçti | ZONE ÖNCESİ DOĞRULANMIŞ |

İki node ayrıca aktif servis, idle ledger, kesintisiz şema 37, kurulu binary/UI
eşitliği, sunulan index eşitliği ve v6 snapshot/rollback kontrollerini geçti. İki
v6 snapshot kaynak kimliği, receipt'ler önceki Alpha51 commit'ini kanıtlasa da
`unknown` değerindedir; R-016 bu provenance uyarısını izler. Tarihsel
`LIVE-STATE-2026-08-29` kaydı değiştirilmez.

Parent delegation ve glue `ns1.celikhost.com → 72.62.38.15` ve
`ns2.celikhost.com → 2.25.80.4`, TTL `172800` olarak doğrulandı; DS yoktur. Child
zone oluşturulmamıştır. Doğrudan UDP/TCP sorguları `REFUSED`, AXFR `NOTAUTH`,
açık recursive çözümleme `SERVFAIL` döndürür.

## 7. Kalan sürüm kapıları ve riskler

- Disposable gerçek Debian 13, Ubuntu 24.04 ve güncel Arch Linux
  install/update/rollback/reboot kanıtı R-007 kapsamındadır. Başarılı iki düğüm
  panel update'i tek başına bu riski kapatmaz.
- Kontrol düzlemi disaster recovery R-003 kapsamındadır.
- Ortam sınıflandırması ve müşteri verisi durumu R-005 kapsamındadır.
- Alpha52 kurulu kimliği kanıtlandı; R-004, R-016 snapshot provenance uyarısı
  düzeltilip yeniden doğrulanana kadar kısmen azaltılmış kalır.
- Repoya olay şablonu eklense bile olay sahipliği ve harici escalation R-014
  kapsamında kalır.
- Açık DNS geçişi, child zone ve tam zone-sonrası otorite matrisi geçene kadar
  R-015 kapsamında engellidir; delegation ve glue zaten geçmiştir.

[RISK-REGISTER.tr.md](RISK-REGISTER.tr.md) ve
[INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.tr.md](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.tr.md)
belgelerine bakın.

## 8. Kabul kuralı

Bu sürüm kaydı incelenmiş Alpha52 kaynağını, imzalı GitHub sürümünü ve
doğrulanmış portal yayınını kanıtlar. Bağlantılı tarihli kayıt yukarıdaki sınırlı
iki node host/sürüm kabulü ile zone öncesi DNS-pair kontrollerini kanıtlar.
Owner-zone DNS kabulünü, açık child-zone otoritesini, disaster recovery'yi veya
production readiness'ı **kanıtlamaz**. Bu iddialar
yukarıdaki tarihli kanıtı ve hesap verebilir harici kabulü gerektirir.
