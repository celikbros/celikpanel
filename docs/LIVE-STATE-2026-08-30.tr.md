# Canlı Durum — 30 Ağustos 2026

*[English](LIVE-STATE-2026-08-30.md) · Tarihli devir snapshot'ı; izleme akışı değildir*

Bu sır içermeyen kayıt, Alpha52 rollout'u sonrasında sağlanan sınırlı gerçeklerle
tarihsel [29 Ağustos kaydını](LIVE-STATE-2026-08-29.tr.md) günceller. Gözlenmeyen
gerçekleri türetmez; harici işlem receipt'lerinin veya registrar kanıtının yerini
tutmaz.

## Kanıt sözlüğü

| Sınıf | Anlam |
|---|---|
| DOĞRULANMIŞ | Doğrudan ürün veya sınırlı host kanıtı gerçeği ortaya koydu |
| ZONE ÖNCESİ DOĞRULANMIŞ | Gerçek, owner zone yayımlanmadan önce kanıtlandı; açık DNS kanıtı değildir |
| AÇIK / YENİDEN DOĞRULA | Zorunlu değişiklik sonrası veya harici kanıt yoktur |

## Alpha52 kurulumu

İmzalı Alpha52 sürümü ile portal yayını
[RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md)
belgesindedir. İki host daha sonra normal imzalı panel update'ini tamamlamış ve
terminal başarı receipt'i döndürmüştür. Receipt kimlikleri ile bunların dayandığı
sır içermeyen host kanıtı harici devir kanıtı olarak kalır ve custodian
atanmalıdır.

| Host | Kurulu sürüm | Update sonucu | Durum |
|---|---|---|---|
| Boston | `v0.1.0-alpha.52` | Sunucu-otoriteli terminal başarı receipt'i gözlendi | DOĞRULANMIŞ |
| Frankfurt | `v0.1.0-alpha.52` | Sunucu-otoriteli terminal başarı receipt'i gözlendi | DOĞRULANMIŞ |

Bu, sınırlı promotion sonucunu kanıtlar. Harici receipt paketinde açıkça yer
almayan şema, snapshot, unit, UI ürünü veya reboot alanlarının tümünü tek başına
kanıtlamaz.

Sonraki sınırlı host denetimi iki panel ve agent servisini
`adb25d8ec487dcb76dd95304a551d8cb37565115` commit'inde aktif, işlem probe'larını
idle, veritabanı şemasını `37`, release floor'u `52`, transaction marker
durumunu beklenen halde ve sunulan UI index/release/v6 checksum'larını eşleşmiş
olarak gözledi. Bir provenance çekincesi kalır: terminal receipt'leri iki hostun
update'e Alpha51'den girdiğini kanıtlasa da snapshot source identity `unknown`
olarak raporlandı.

## Owner-zone yayını öncesi authoritative DNS durumu

| Gerçek | Gözlenen durum | Durum |
|---|---|---|
| Frankfurt motoru ve rolü | BIND primary | ZONE ÖNCESİ DOĞRULANMIŞ |
| Boston motoru ve rolü | PowerDNS secondary | ZONE ÖNCESİ DOĞRULANMIŞ |
| Karma-motor çifti | Pair kimliği ve zone öncesi katalog sağlığı eşleşti | ZONE ÖNCESİ DOĞRULANMIŞ |
| Owner zone | `celikhost.com` iki panel veritabanında ve DNS motorunda da yok | AÇIK / ENGELLEYİCİ |
| Zone öncesi katalog | İki motorda authoritative, serial `1` ve üye zone yok; source-bound katalog AXFR iki yönde başarılı | ZONE ÖNCESİ DOĞRULANMIŞ |
| Owner-zone UDP/TCP ve AXFR | İki motor UDP/TCP SOA, NS ve A sorgularına `REFUSED`, source-bound owner-zone AXFR'ye `NOTAUTH` döndürüyor | Yokluğu DOĞRULANMIŞ |
| Parent delegation | `.com`, `ns1.celikhost.com` ve `ns2.celikhost.com` sunucularına delege ediyor; TTL `172800` | DOĞRULANMIŞ harici gözlem |
| In-bailiwick registrar glue | `ns1.celikhost.com A 72.62.38.15` ve `ns2.celikhost.com A 2.25.80.4`; TTL `172800` | DOĞRULANMIŞ harici gözlem |
| Parent DS | DS kaydı yayımlanmıyor; `.com` yokluk kanıtı NSEC3 ile doğrulanmış | DOĞRULANMIŞ harici gözlem |
| Açık authoritative çözümleme | Delege edilmiş child zone olmadığı için recursive açık çözümleme `SERVFAIL` döndürüyor | AÇIK / ENGELLEYİCİ |

Zone öncesi katalog sonucu, amaçlanan BIND-primary ve PowerDNS-secondary kontrol
düzlemi eşleşmesinin owner zone yayımlanmadan önce sağlıklı olduğunu kanıtlar.
Açık delegation, zone sonrası serial eşitliği, AXFR, authoritative AA/SOA
cevapları veya availability iddiasına dönüştürülemez.

## Kalan kabul sınırı

Açık DNS geçişi [R-015](RISK-REGISTER.tr.md#r-015--child-zone-ve-açık-otorite-engelleyicisi)
kapsamında engelli kalır. Bu risk kapanmadan önce:

1. registrar owner ve yedek atamasını repo dışında koruyun;
2. `celikhost.com` authoritative owner zone'unu normal panel akışıyla yayımlayın;
3. iki motorda eşleşen zone sonrası katalog üyeliği ve serial'ları, source-bound
   AXFR, AA içeren UDP/TCP SOA, PairReady ve açık çözümlemeyi kanıtlayın; ve
4. sınırlı sonuçları harici devir kanıt sistemine bağlayın.

Registrar delegation ve exact IPv4 glue zaten doğrulanmıştır; kalan cutover
adımları değildir. Owner-zone yayınından sonra regresyon kanıtı olarak yeniden
kontrol edin; registrar tarafında değişiklik olduğuna dair bağımsız kanıt yoksa
yeniden kaydetmeyin.

Bu adımlar tamamlandığında bu snapshot'ı yeniden yazmayın. Yeni tarihli kayıt
oluşturup risk sicili ile devir belgesinden bağlayın.

## İlgili kanıtlar

- [Alpha52 sürüm kanıtı](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md)
- [Update ve DNS kurtarma olayı](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.tr.md)
- [Risk sicili](RISK-REGISTER.tr.md)
- [Tarihsel 29 Ağustos canlı durumu](LIVE-STATE-2026-08-29.tr.md)
