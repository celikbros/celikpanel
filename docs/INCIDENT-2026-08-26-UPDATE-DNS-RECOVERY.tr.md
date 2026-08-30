# Olay Raporu — 26 Ağustos 2026'da Başlayan Update ve DNS Kurtarma Hataları

*Hazırlanma: 30 Ağustos 2026 · [English](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.md)*

## 1. Belge durumu

| Alan | Değer |
|---|---|
| Olay durumu | Alpha52 düzeltmesi ve iki node canlı kabulü doğrulandı; child-zone/açık-otorite kanıtı ve harici sahiplik için olay AÇIK |
| Başlangıç | 2026-08-26 06:15 UTC, korunmuş ilk başarısız-update kanıtı |
| Bitiş | İlan edilmedi; kapanış bölüm 10'daki kalan ölçütleri gerektirir |
| Önem | Onaylı severity modelinde REPO DIŞI / ATA |
| Olay komutanı | REPO DIŞI / ATA |
| Etkilenen yüzeyler | İmzalı self-update, update rollback/kurtarma, yetkili DNS işlemleri, işlem-ilerleme arayüzü |
| Birleşik düzeltmeyi içeren açık sürüm | [v0.1.0-alpha.52](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.52) |

Bu rapor sır içermez. Kanıt sınıflarını ayırır:

- **DOĞRULANMIŞ:** Korunmuş ürün çıktısı, servis durumu, değişmez release
  metadata'sı, incelenmiş kod veya regresyon testi bilgiyi gösterir.
- **KULLANICI GÖZLEMİ:** Operatör ekran görüntüsü veya doğrudan gözlem verdi;
  bu, sunucu tarafı nedeni tek başına kanıtlamayabilir.
- **ÇIKARIM:** Kanıtla uyumlu fakat bağımsız kanıtlanmamış sonuçtur; gerçek gibi
  sunulamaz.
- **BİLİNMİYOR:** Yeterli kanıt yoktur.

## 2. Yönetici özeti

Birden fazla hata tek ve tekrarlayan bir sorun gibi yaşandı: işlem paneli
durdurabiliyor veya yeniden başlatabiliyor, kurtarma transaction'ını beklemede
bırakabiliyor ya da tarayıcı yalnız uzun süreli engelleyici katman gösterirken
sunucu tarafında sürebiliyordu. Ürün, host mutasyonu bittiği sınırla yetkili
terminal sonucun yayımlandığı sınırı her zaman aynı anda tamamlamıyordu. DNS ile
self-update tam host-artı-terminal-yayın exclusion sınırını da paylaşmıyordu.

Bu durum, fail-closed davranış ikinci mutasyonu önlese bile güvensiz bir işletim
deneyimi oluşturdu: kullanıcı aktif çalışma, bağlantı kaybı, doğrulanmış rollback
ve bayat istemci durumunu güvenilir biçimde ayıramadı. Kurtarma, sıradan müşteriden
beklenemeyecek ürün-özel teşhis gerektirdi.

Alpha52 terminal yayın, sistemler arası admission, sınırlı istemci kurtarması ve
exact abandon/tombstone davranışına ait kaynak düzeltmeleri ile regresyon
testlerini birleştirir. İmzalı GitHub sürümü ve portal yayını doğrulanmıştır. İki
node daha sonra Alpha52 canlı kabulünü geçti. `celikhost.com` child zone yokluğu,
başarısız açık-otorite matrisi ve harici sahiplik/kalan işlemler kapanmadığı için
olay açık kalır.

## 3. Etki

### Doğrulanmış etki

- Frankfurt panel ve agent, başarısız Alpha45 update'i ile yinelenen rollback
  denemeleri sonrasında durmuş kaldı.
- Update receipt'i hata bildirip transaction marker'ları active kalırken kurulu
  Alpha45 binary'leri ve release floor mevcuttu.
- Daha sonraki rollback Alpha44 binary'lerini ve canonical veritabanını geri
  getirdi; fakat paket/mutasyon kilidi agent başlangıcını engelledi ve
  `completion.pending` kaldı.
- Alpha46 kurtarma yolu daha sonra doğrulanmış çift provenance ile bekleyen
  rollback'i tamamladı.

### Kullanıcı tarafından gözlenen etki

- DNS değişiklikleri uygulanabilir faz ilerlemesi olmadan uzun süren engelleyici
  katmanlar gösterdi.
- Bir Frankfurt BIND denemesi sonunda yaklaşık 1 saat 16 dakikalık süreden sonra
  doğrulanmış rollback gösterdi.
- Daha sonraki Frankfurt BIND işlemi tamamlanırken Boston PowerDNS değişikliği
  beklenmedik ölçüde uzun süre doğrulamada kaldı.
- İmzalı self-update bağlantı kesintisi gösterdi ve tarayıcı açısından on
  dakikadan uzun süre kilitli kaldı.

### Bilinmeyen veya kanıtlanmamış

- Müşteri verisi kaybı gösterilmemiştir.
- Etkilenen her aralıkta kesintisiz açık DNS erişimi tam kanıt kümesiyle
  kaydedilmemiştir.
- Toplam etkilenen kullanıcı sayısı ve iş etkisi kaydedilmemiştir.
- Tam clean-host disaster-recovery kanıtı yoktur.

## 4. Sır içermeyen UTC zaman çizelgesi

| Zaman | Kanıt sınıfı | Olay |
|---|---|---|
| 2026-08-26 06:15:16–06:15:17 | DOĞRULANMIŞ | Alpha44→Alpha45 update'i agent ve paneli durdurdu; panel SIGSTOP ve ardından SIGKILL aldı, iki unit inactive/failed kaldı. |
| 2026-08-26 06:15:26 | DOĞRULANMIŞ | Update receipt'i `failed` kaydetti; Alpha45 panel/agent binary'leri ile floor 45 vardı, `active` ve `transaction.lock` kaldı. |
| 2026-08-26 10:33 | DOĞRULANMIŞ | Korunmuş ilk rollback denemesi “panel TLS compatibility state could not be restored” ile durdu; servisler stopped kaldı. |
| 2026-08-26 10:42 | DOĞRULANMIŞ | İkinci deneme daha fazla durum geri getirdi fakat canonical database parent metadata'sını secure-normal veya recoverable-quarantine olmadığı için reddetti. |
| 2026-08-26 11:16 | DOĞRULANMIŞ | Canonical veritabanı ile Alpha44 binary'leri geri geldi; host paket/mutasyon kilidi agent başlangıcını engelledi, `completion.pending` ve iki servis stopped kaldı. |
| 2026-08-26 13:14 | DOĞRULANMIŞ | Alpha46 incelenmiş kurtarma, çift doğrulanmış provenance ile bekleyen rollback'i tamamladı ve taze normal update istedi. |
| Daha sonraki operatör oturumu | KULLANICI GÖZLEMİ | DNS işlemi “in progress” kaldı, sonra yaklaşık 1 saat 16 dakika ile yeterli canlı faz görünümü olmadan rollback gösterdi. |
| Daha sonraki operatör oturumu | KULLANICI GÖZLEMİ | Frankfurt BIND daha sonra yaklaşık 27 saniyede commit oldu; Boston PowerDNS ardından navigation lock arkasında doğrulamada kaldı. |
| Daha sonraki operatör oturumu | KULLANICI GÖZLEMİ | İmzalı self-update bağlantı kesintisi tarayıcıda on dakikadan uzun süre çözümsüz işlem olarak kaldı. |
| 2026-08-30 00:30 | DOĞRULANMIŞ | Alpha52 etiketli workflow başarıyla tamamlandı ve birleşik düzeltme sürümünü yayımladı. |
| 2026-08-30 daha sonra | DOĞRULANMIŞ | Boston ve Frankfurt exact build `adb25d8ec487dcb76dd95304a551d8cb37565115` için terminal başarılı Alpha52 receipt'leri üretip sınırlı host/sürüm kabulünü geçti. |
| 2026-08-30 daha sonra | DOĞRULANMIŞ | Zone öncesi Frankfurt BIND-primary/Boston PowerDNS-secondary katalog serisi `1` ve source-bound AXFR iki yönde geçti. Parent delegation/glue geçti; child zone yokluğu `REFUSED`, `NOTAUTH` ve açık `SERVFAIL` üretti. |

Ham loglar, özel yollar, operatör hesapları ve kimlik bilgileri repo dışında
kalır. Kanıt referansları onaylı olay sicilinde atanmalıdır.

## 5. Teknik nedenler

Bu tek bir timeout değildi. İncelenen hata zincirinde ayrı sınır kusurları vardı.

### 5.1 Update rollback ve kurtarma coupling'i

Başarısız update, her uyumluluk ve restore önkoşulu yeniden doğrulanmadan önce
servis-stop sınırını geçti. Sonraki kurtarma sıkı metadata ve mutasyon-kilidi
kontrolleriyle karşılaştı. Bu kontroller tahmin yürütmeyi doğru biçimde reddetti;
fakat ürün sıradan kurulumu erişilemez bıraktı ve özel kurtarma bilgisi istedi.

### 5.2 Host tamamlama ile terminal yayın ayrı pencerelerdi

DNS host mutasyonu bitip son kalıcı service-operation receipt'i yayımlanmadan
host kilidini bırakabiliyordu. Başka alt sistem bu aralıkta idle ledger
görebiliyordu. Bu, ilk işlemin terminal durumu kalıcı ve gözlenebilir olmadan
ikinci ayrıcalıklı mutasyonun girememesi değişmezini ihlal etti.

### 5.3 Sistemler arası self-update admission eksikti

Self-update admission host kilidini alıyor fakat DNS terminal-publication
kilidini de almıyordu. Bir goroutine regresyon testi yarışı yeniden üretti:
self-update, DNS host kilidini bıraktıktan sonra fakat DNS terminal receipt'ini
yayımlamadan önce lease alabiliyordu.

### 5.4 İstemci belirsizliği süresiz aktif işlem sayılıyordu

Bağlantı kesintisi, yinelenen not-found yanıtları ve yeniden başlayan panel tek,
sınırlı, sunucu-otoriteli abandonment sözleşmesine sahip değildi. Arayüz bu
nedenle işlemin active, terminal, missing veya superseded olduğunu kanıtlamadan
tam ekran navigation lock'u koruyordu.

### 5.5 Geç-start koruması eksikti

Exact kalıcı tombstone olmadan abandon edilmiş request kimliği gecikmiş start
yolunda daha sonra kabul edilebilirdi. Yalnız istemci temizliği hâlâ başlama
otoritesi taşıyan işlemi gizleyebileceği için güvensizdi.

## 6. Var olan korumalar olayı neden önlemedi

- Bileşen testleri DNS ile self-update arasındaki host-release/terminal-
  publication interleaving'ini kapsamıyordu.
- Fail-closed kurtarma durumu korudu fakat sınırlı müşteri kurtarma yolu veya
  yeterli ilerleme kanıtı sunmadı.
- Tarayıcı durumu, işlemin hâlâ yetkili olup olmadığına karar vermede gereğinden
  fazla sorumluluk taşıdı.
- Canlı iki düğüm kabulü, disposable-VM rollback/reboot kanıtı ve olay
  sorumlusu/escalation modeli eksikti.
- İşlem arayüzü başlangıçta yetkili son durum hazır olmadan başarı veya retry
  mesajı göstererek ek tıklamalara fırsat verdi.

## 7. Alpha52 düzeltmesi

| Düzeltme | Kaynak/inceleme sonucu | Canlı sonuç |
|---|---|---|
| Composite service-mutation edinimi host → terminal-publication sırasını izler | Uygulandı ve regresyon testi geçti | KISMEN DOĞRULANDI |
| Self-update DNS terminal-publication penceresinde giremez | Yeniden üreten test düzeltme öncesi kırmızı, sonrası ve race modunda yeşil | KISMEN DOĞRULANDI; çakışan canlı işlem gözlenmedi |
| DNS terminal receipt'i kalıcı son duruma kadar publication exclusion'ı tutar | Uygulandı ve test edildi | İki node terminal Alpha52 receipt'iyle DOĞRULANDI |
| DNS başlangıcı exact kurtarılabilir eski durumları yükseltir ve guarded expiry'yi yeniden dener | Uygulandı ve test edildi | AÇIK / YENİDEN DOĞRULA |
| Self-update abandonment exact tam kimlik ve sunucu otoritesi gerektirir | Uygulandı ve test edildi | AÇIK / YENİDEN DOĞRULA |
| Kalıcı tombstone abandon edilmiş kimliğin gecikmiş start'ını reddeder | Uygulandı ve test edildi | AÇIK / YENİDEN DOĞRULA |
| Belirsiz timeout, authorization veya 5xx pending kalır; yalnız exact terminal hata istemci durumunu bırakır | Uygulandı ve test edildi | AÇIK / YENİDEN DOĞRULA |
| Update ve DNS katmanları operation kimliğini gösterir ve doğrulanmış sonuca kadar navigation lock'u korur | Uygulandı ve web testleri geçti | AÇIK / YENİDEN DOĞRULA |
| Alpha52 imzalı GitHub sürümü ve portal yayını | Alpha52 release kaydında DOĞRULANMIŞ | Exact build iki canlı node'a kuruldu ve kabul edildi |

Exact release kimliği ve ürün kanıtı
[RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md)
içindedir.

## 8. Müşteri için güvenli kurtarma kuralı

1. İkinci bir DNS veya update işlemi başlatmayın.
2. Exact operation ID ile UTC zamanını görünür tutun; kimlik bilgisi açmayın.
3. Sunucu terminal olmayan yetkili işlem bildirdiği sürece otomatik status
   retry'larına izin verin.
4. Bağlantı kesilirse tarayıcıdan başarı veya hata çıkarmayın; aynı işleme
   yeniden bağlanın.
5. Ürünün exact abandon işlemini yalnız sunucu tam kimliği kabul edip
   failure/tombstone sonucunu yayımladığında kullanın.
6. Müşteriden veritabanı, transaction marker, mutation journal, release floor
   veya DNS daemon durumunu elle değiştirmesini asla istemeyin.
7. SSH, değişmez incelenmiş release kurtarma prosedürü sınırlı mutasyona açıkça
   yetki vermedikçe salt okunur teşhistir.

Destek yalnız sır içermeyen version/commit, operation ID, terminal faz, unit
durumu ve sınırlı journal kanıtı istemelidir. Parola, private key, token, ham
veritabanı ve sınırsız log destek ürünü değildir.

## 9. Düzeltici işlemler

| ID | İşlem | Durum | Kabul kanıtı | Sorumlu / hedef |
|---|---|---|---|---|
| IR-001 | Composite DNS/self-update kilidi ve terminal-receipt düzeltmelerini yayımla | ALPHA52 KAYNAK/SÜRÜMÜNDE TAMAM | Exact commit, yeşil CI ve imzalı sürüm | REPO DIŞI / ATA |
| IR-002 | Alpha52 imzalı update'ini önce Boston, sonra Frankfurt'ta doğrula | TAMAM / SNAPSHOT PROVENANCE UYARISI R-016 | Node receipt'leri, exact build, floor 52, unit, idle durum, ürün ve v6 rollback kanıtı | REPO DIŞI / ATA |
| IR-003 | İki update sonrasında BIND-primary/PowerDNS-secondary katalog ve açık DNS'i yeniden kanıtla | KISMİ / R-015 ALTINDA ENGELLİ | Zone öncesi AXFR/katalog ve delegation/glue geçti; child-zone/zone-sonrası açık kanıt kaldı | REPO DIŞI / ATA |
| IR-004 | Update kesinti/reconnect ve DNS tamamlanması için bir browser golden-path kaydı koru | AÇIK | Alpha52'ye bağlı kimliği doğrulanmış sınırlı browser kanıtı | REPO DIŞI / ATA |
| IR-005 | Gerçek disposable Debian, Ubuntu ve Arch install/update/rollback/reboot kapılarını tamamla | R-007 KAPSAMINDA AÇIK | Exact ürün digest'ine bağlı kanıt | REPO DIŞI / ATA |
| IR-006 | Clean-host kontrol düzlemi restore tatbikatını tamamla | R-003 KAPSAMINDA AÇIK/ENGELLEYİCİ | Şifreli yedek ve kabul edilmiş RPO/RTO ile doğrulanmış restore | REPO DIŞI / ATA |
| IR-007 | Severity, olay komutanı, on-call, iletişim ve action owner'larını haricen ata | R-014 KAPSAMINDA AÇIK | Onaylı harici olay sicili | REPO DIŞI / ATA |
| IR-008 | İki dilli olay şablonunu benimse ve tatbik et | REPODA KISMEN AZALTILDI | İncelenmiş olay tatbikatı ve hesap verebilir onay | REPO DIŞI / ATA |

## 10. Kapanış ölçütleri

Olay yalnız şu koşullarda kapatılabilir:

- Boston ve Frankfurt zaten geçen Alpha52 panel, agent, UI, şema, floor ve
  operation-idle kanıtını korur;
- child zone yayımlanır ve zorunlu zone-sonrası DNS pair/açık çözümleme matrisi
  geçer;
- hiçbir update veya DNS isteği belirsiz active, missing ya da yeniden
  başlatılabilir kalmaz;
- sınırlı browser reconnect/terminal davranışı kabul edilir;
- kalan her düzeltici işlem tamamlanır, hesap verebilir harici sorumlu
  tarafından açıkça risk-kabul edilir veya hedef tarihle devredilir; ve
- yeni tarihli canlı durum kaydı ile risk sicili sır içermeyen kanıta bağlanır.

Yalnız release yayımlamak olayı kapatmaz.
