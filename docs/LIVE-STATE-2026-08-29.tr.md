# Canlı Durum — 29 Ağustos 2026

*[English](LIVE-STATE-2026-08-29.md) · Devir snapshot'ı; izleme akışı değildir*

Bu belge doğrulanmış gözlemleri, beyan edilmiş topolojiyi ve bilinmeyen canlı
bilgileri birbirinden ayırır. Kimlik bilgisi, token, özel anahtar, müşteri
verisi veya ham log içermez.

## Durum sözlüğü

| Durum | Anlam |
|---|---|
| DOĞRULANMIŞ | Belirtilen tarihte devir hazırlığında kanıt mevcuttu |
| BEYAN EDİLMİŞ | Repo sözleşmesi veya runbook değeri söyler; canlı durum bağımsız kanıtlanmamıştır |
| BİLİNMİYOR / YENİDEN DOĞRULA | Yeterli güncel kanıt yoktur; güvenmeden önce salt okunur toplayın |
| REPO DIŞI | Kanıt veya sahiplik kaydı onaylı harici devir sisteminde kalmalıdır |

Kaynak kimliği ile canlı kurulu kimlik farklı bilgilerdir. Panel ve agent tam
olarak aynı kimliği bağımsız bildirip kanıtlamadan kaynak etiketini veya
commit'ini sunucu satırına kopyalamayın.

## 1. Kaynak kontrol referansı

| Bilgi | Değer | Durum |
|---|---|---|
| Canonical kaynak etiketi ve sürümü | [v0.1.0-alpha.51](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51) | DOĞRULANMIŞ |
| Canonical kaynak commit'i | 45d01ffb29013b9457180072c3b25ab24d5ff7bd | DOĞRULANMIŞ |
| Kaynağa yazılmış release sequence | 51 | KAYNAKTA DOĞRULANMIŞ |
| Güncel yayımlanmış binary sürüm | v0.1.0-alpha.51 | DOĞRULANMIŞ |
| Alpha51 etiketli sürüm CI'ı | Yayımlanmış sürüm için geçti | DOĞRULANMIŞ |
| GitHub sürüm ürünü sayısı | Tam 6 değişmez ürün | DOĞRULANMIŞ |
| Resmi sürüm otoritesi | Ed25519 imzalı manifest v2 ve ayrık imza doğrulandı | DOĞRULANMIŞ |
| Manifest'in yetkilendirdiği arşiv | SHA256 `57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365`; 22.644.115 bayt | DOĞRULANMIŞ |
| Git etiketi imzası | İmzasız | DOĞRULANMIŞ; etiket imzası update otoritesi değildir |
| Devir dalı | `docs/alpha51-engineering-handoff` | Yalnız belge; Alpha51 binary referansının parçası değildir |
| GitHub açık pull request'leri | 5 taslak: [#69](https://github.com/celikbros/celikpanel/pull/69), [#70](https://github.com/celikbros/celikpanel/pull/70), [#71](https://github.com/celikbros/celikpanel/pull/71), [#72](https://github.com/celikbros/celikpanel/pull/72), [#73](https://github.com/celikbros/celikpanel/pull/73) | 29.08.2026 itibarıyla DOĞRULANMIŞ; hiçbiri Alpha51'in parçası değildir |
| İndirme portalı tam imzalı Alpha51 ürün kümesini sunuyor | Burada kanıtlanmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Devir temizliği sonucu | Kopya worktree/dallar ve listelenen kalıntılar kaldırıldı; yalnız primary kayıtlı worktree kaldı | DOĞRULANMIŞ devir gözlemi |
| Yeni ekibin temiz `main` checkout'u | Yeni ekip tarafından henüz doğrulanmadı | BİLİNMİYOR / YENİDEN DOĞRULA |

Taslak PR #69 migration DDL canonicalization işini kapsar. Taslak PR #70 restart
acknowledgement UX'ini kapsar ve ürün kararı gerektirir. Taslak PR #71
`agent/ci-fast` dalındaki CI duplicate-release validation işini korur. Taslak PR
#72 `agent/ssl-hostnames-hsts` dalındaki arşivlik SSL/backup WIP işini korur;
arşivliktir ve olduğu hâliyle merge edilmemelidir. Taslak PR #73,
`archive/alpha35-portal-tooling` head'i ve
`0ef899f3cb96390c4ef3822f199eddc67bb0ee1f` commit'iyle beş benzersiz Alpha35
portal scriptini ve yayımlanmamış bir PR72-follow-up patch'ini arşivler.
Arşivliktir; olduğu hâliyle merge edilmemeli veya çalıştırılmamalıdır. Hiçbir
taslak için bütün kontrollerin yeşil olduğu iddia edilmez. Devir dalının merge
edilmesi `main` dalını yalnız belge değişiklikleriyle Alpha51 etiketinin önüne
taşıyabilir; ayrı bir sürüm üretilene kadar yayımlanmış binary Alpha51 olarak
kalır.

Benzersiz işler PR #72 ve PR #73'te korunduktan sonra repo temizliği; 109
kayıtlı kopya worktree'yi, 105 eski yerel dalı, 56 eski remote dalı, `.attic`,
`.worktrees`, `.claude/worktrees`, root `__pycache__` ve geçici devir
worktree'sini kaldırmıştır. Yalnız primary kayıtlı worktree kalmıştır. Tracked
`.design-sync` bilinçli olarak tutulmuştur. Bu yalnız repo temizliğidir: canlı
sunucuda değişiklik yapmamış, DNS veya servis durumunu kanıtlamamıştır. Yeni ekip
taze ve temiz bir `main` checkout'unu yine doğrulamalıdır.

## 2. Ortam sınıflandırması

SECURITY.md ürünü ön sürüm ve üretime hazır değil olarak tanımlar.
OPERATIONS.md Boston ve Frankfurt için production rollout dili kullanırken
ROADMAP.md bunları test sunucuları olarak da anlatır. Operasyon sınıfları ve
müşteri verisi taşıyıp taşımadıkları repo dışında açıkça kararlaştırılmalıdır.

| Ortam bilgisi | Değer | Durum |
|---|---|---|
| Ürün yaşam evresi | Alpha / ön sürüm | BEYAN EDİLMİŞ |
| Ürünün üretime hazır olması | Üretime hazır değil | BEYAN EDİLMİŞ |
| Boston sınıfı: test, staging veya production | Kesinleşmedi | BİLİNMİYOR / YENİDEN DOĞRULA |
| Frankfurt sınıfı: test, staging veya production | Kesinleşmedi | BİLİNMİYOR / YENİDEN DOĞRULA |
| İki node'dan birinde müşteri veya kişisel veri var mı | Belirlenmedi | BİLİNMİYOR / YENİDEN DOĞRULA |
| İki node'un güncel DNS ve canlı servis durumu | Aşağıdaki kısmi Boston gözlemi dışında toplanmadı | BİLİNMİYOR / YENİDEN DOĞRULA |

## 3. Beyan edilmiş iki node topolojisi

Aşağıdaki satırlar dondurulmuş operasyon topolojisini tekrarlar. Canlı host'ların
şu anda buna uyduğunu kanıtlamaz.

| Node | Adres | Beyan edilmiş rollout/DNS rolü | Beyan edilmiş işletim sistemi | Canlı kanıt |
|---|---|---|---|---|
| boston.celikhost.com | 2.25.80.4 | İlk rollout hedefi; NS2; yönlü PowerDNS secondary | Ubuntu 24.04 | Aşağıda kısmen doğrulandı |
| frankfurt.celikhost.com | 72.62.38.15 | İkinci rollout hedefi; NS1; doğrudan BIND primary | Debian 13 | BİLİNMİYOR / YENİDEN DOĞRULA |

## 4. Boston

| Bilgi | Değer | Durum |
|---|---|---|
| Açık panel kurtarma yanıtı | HTTP 200 | DOĞRULANMIŞ devir gözlemi |
| Bildirilen canlı sürüm | v0.1.0-alpha.51 | DOĞRULANMIŞ devir gözlemi |
| Panel tam commit'i | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Agent sürümü ve tam commit'i | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Panel/agent build eşleşmesi | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Veritabanı şema sürümü ve kesintisiz migration kanıtı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| İşletim sistemi ve paket ekosistemi | Ubuntu 24.04 beyan edildi; burada canlı kanıt yok | BEYAN / YENİDEN DOĞRULA |
| celikpanel-panel unit'i | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| celikpanel-agent unit'i | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Sunulan UI ürünü Alpha51'e ait | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Panel TLS hostname, issuer ve bitiş tarihi | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| DNS engine | PowerDNS secondary beyan edildi; burada canlı kanıt yok | BEYAN / YENİDEN DOĞRULA |
| Secondary yerel yazma reddi | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Katalog/üye AXFR ve UDP/TCP SOA kanıtı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| PairReady | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| TCP/53 ve UDP/53 erişimi | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Firewall kayıtlı politika ve boot-restore durumu | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Release sequence floor | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| En son eksiksiz v6 update snapshot'ı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Son başarılı rollback veya restore tatbikatı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| secret.key ve kontrol düzlemi anahtarlarını içeren disaster backup | Kanıtlanmadı | BİLİNMİYOR / YENİDEN DOĞRULA |

Doğrulanmış HTTP 200 ve sürüm metni başka hiçbir satırı kanıtlamaz.

## 5. Frankfurt

| Bilgi | Değer | Durum |
|---|---|---|
| Açık panel yanıtı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Güncel canlı sürüm | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Panel tam commit'i | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Agent sürümü ve tam commit'i | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Panel/agent build eşleşmesi | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Veritabanı şema sürümü ve kesintisiz migration kanıtı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| İşletim sistemi ve paket ekosistemi | Debian 13 beyan edildi; burada canlı kanıt yok | BEYAN / YENİDEN DOĞRULA |
| celikpanel-panel unit'i | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| celikpanel-agent unit'i | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Sunulan UI ürün kimliği | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Panel TLS hostname, issuer ve bitiş tarihi | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| DNS engine | BIND primary beyan edildi; burada canlı kanıt yok | BEYAN / YENİDEN DOĞRULA |
| Katalog serial ve üyelik | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Eş kaynak-bağlı AXFR ve UDP/TCP SOA kanıtı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| PairReady ve yerel zone yazma hazırlığı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| TCP/53 ve UDP/53 erişimi | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Firewall kayıtlı politika ve boot-restore durumu | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Release sequence floor | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| En son eksiksiz v6 update snapshot'ı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| Son başarılı rollback veya restore tatbikatı | Alınmadı | BİLİNMİYOR / YENİDEN DOĞRULA |
| secret.key ve kontrol düzlemi anahtarlarını içeren disaster backup | Kanıtlanmadı | BİLİNMİYOR / YENİDEN DOĞRULA |

Frankfurt'un kurulu sürümünü kaynak referansından, Boston'dan, DNS yanıtından,
tarayıcı cache'inden veya eski release notundan türetmeyin.

## 6. Gerekli salt okunur toplama

Panel ayarlarını, unit'leri, paketleri, DNS'i, firewall'u, dosyaları veya
veritabanlarını değiştirmeden şunları toplayın:

1. UTC zaman damgası, operatör kimliği ve onaylı kanıt konumu.
2. Panel sürüm/commit, agent sürüm/commit ve şema sürümünü içeren kimliği
   doğrulanmış panel version yanıtı.
3. Aynı sürüme ait sunulan UI ürün kimliği.
4. Host işletim sistemi ve paket ekosistemi.
5. celikpanel-panel ve celikpanel-agent aktif durumu ile kanıt aralığına ait
   sınırlı journal incelemesi.
6. Salt okunur service-operation ve özel mutation-ledger idle kanıtı.
7. DNS engine, değişmez eş kimliği, rol, katalog serial/üyelik, kaynak-bağlı
   AXFR, UDP/TCP SOA ve PairReady kanıtı.
8. Harici TCP/UDP port testleri ve TLS sertifika metadata'sı.
9. Kayıtlı firewall politikası ve boot-restore sahiplik durumu.
10. Anahtar malzemesini açığa çıkarmadan release sequence floor ve tam kurulu
    updater kimliği.
11. En son eksiksiz v6 snapshot kimliği ve eşleşen rollback yardımcısı.
12. Yedek kapsamı, retention, son başarılı restore tatbikatı ve secret.key,
    DKIM/WireGuard anahtarları ile panel sertifikalarının kurtarılabilirliği.

Kimliği doğrulanmış salt okunur ürün görünümlerini tercih edin. SSH'ı yalnız
yetkiyle ve yalnız ürünün gösteremediği salt okunur kanıt için kullanın. Kanıtı
kaydetmeden önce token, parola, özel anahtar, e-posta adresi, müşteri verisi ve
gereksiz IP/kullanıcı bilgisini çıkarın.

## 7. Kanıt kaydı

Özel yol içeren gerçek URL'ler, hesap adları, SSH kimlikleri, kişisel veri
içeren ekran görüntüleri ve ham loglar REPO DIŞI kalır.

| UTC toplama zamanı | Konu | Sonuç | Kanıt referansı |
|---|---|---|---|
| 2026-08-29 | Canonical kaynak etiketi ve commit | Alpha51 / 45d01ffb29013b9457180072c3b25ab24d5ff7bd | Repo devir referansı |
| 2026-08-29 | Alpha51 sürüm zinciri | Sürüm/etiket kimliği, etiketli sürüm CI'ı, 6 ürün ve resmi Ed25519 manifest/imzası doğrulandı; arşiv SHA256 `57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365`, 22.644.115 bayt; Git etiketi imzasız | [GitHub sürümü](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51) |
| 2026-08-29 | GitHub açık pull request'leri | 5 taslak: #69 migration DDL canonicalization; #70 restart acknowledgement UX/ürün kararı; #71 CI duplicate-release validation (`agent/ci-fast`); #72 arşivlik SSL/backup WIP (`agent/ssl-hostnames-hsts`); #73 beş benzersiz Alpha35 portal scripti ve yayımlanmamış PR72-follow-up patch'i (`archive/alpha35-portal-tooling`, `0ef899f3cb96390c4ef3822f199eddc67bb0ee1f`) | Hiçbiri Alpha51'de değil; bütün CI kontrollerinin yeşil olduğunu türetmeyin; #72/#73 arşivliktir; ikisini de olduğu hâliyle merge etmeyin ve #73'ü olduğu hâliyle çalıştırmayın |
| 2026-08-29 | Repo temizliği | 109 kayıtlı kopya worktree, 105 eski yerel dal, 56 eski remote dal ve listelenen kalıntılar kaldırıldı; yalnız primary kayıtlı worktree kaldı; tracked `.design-sync` bilinçli tutuldu | Yalnız repo gözlemi; yeni ekibin temiz `main` checkout'u YENİDEN DOĞRULA olarak kalır |
| 2026-08-29 | Boston açık panel kurtarma | HTTP 200; Alpha51 bildirildi | REPO DIŞI kanıt referansı atanacak |
| Toplanmadı | Boston tam runtime envanteri | BİLİNMİYOR / YENİDEN DOĞRULA | REPO DIŞI kanıt referansı atanacak |
| Toplanmadı | Frankfurt tam runtime envanteri | BİLİNMİYOR / YENİDEN DOĞRULA | REPO DIŞI kanıt referansı atanacak |

## 8. Kabul kuralı

İlgili her BİLİNMİYOR / YENİDEN DOĞRULA satırının tarihli kanıtı ve harici
custodian'ı olmadan hiçbir sunucu güncel, eşleşmiş, rollback'e hazır veya
production-ready kabul edilmez. Bu belge güncellenirken hiçbir sır değeri
eklenemez.
