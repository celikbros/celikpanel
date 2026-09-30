# DNS motoru kurtarma kabul kaydı

*Mühendislik kaydı · [English](DNS-RECOVERY-ACCEPTANCE.md) · ilk derleme 29 Eylül 2026,
`e9d1019d` kaynağının salt-okur denetiminden*

Bu, [28 Eylül devir belgesinin](HANDOFF-2026-09-28.tr.md) istediği tek listedir.
Ürünün gerçekten başlatabildiği her DNS motoru mutasyonunu tam olarak bir duruma
bağlar. Devrin 1. maddesinin (DNS kurtarma sözleşmesi) kabul kaydıdır;
[dayanıklılık sözleşmesi](RESILIENCE-CONTRACT.md) ve
[DNS kalıcı çıktı sözleşmesi](DNS-ENGINE-ARTIFACT.md) değişmezleri ve bayt
düzeyindeki biçimleri korur. Bu kayıtta alınan kararlar
[D-026](DECISIONS.tr.md) olarak kaydedilir.

## Durumlar

| Durum | Anlamı |
|---|---|
| **GEÇTİ** | Adı geçen kesinti, güncel üreticiyle gerçek, tek kullanımlık bir sistemde çalıştırıldı; aynı işlem devam etti veya geri alındı; sahip değişiklikleri korundu; sınırlarıyla birlikte rapor saklandı. Bir geçiş, adını verdiği hücrelerle sınırlıdır. |
| **EKSİK** | Yol desteklenir ve erişilebilir; adı geçen kesintinin kod ve bileşen testleri var ama gerçek sistem denemesi yok, ya da bilinen bir eksik parça var. Adı geçen eksikler kanıt bulunana kadar açık kalır. |
| **DESTEKLENMİYOR, reddediliyor** | Ürün, herhangi bir mutasyondan önce işlemi hem Panel'de hem Agent'ta, uygulanabilir yönlendirmeyle reddeder. Bu bir hata değil; kapsamı belirlenmiş bir sürüm sınırıdır. |
| **DESTEKLENMİYOR, reddedilmiyor** | Erişilebilir ama bir kurtarma sözleşmesi yok. Kabul edilebilir bir durum değil. Sürümden önce diğer üç durumdan birine taşınmalıdır. |

Ortak bilgiler (kaynak referansları `e9d1019d` artı D-026 kapısı içindir):

- Panel girişi: önce `POST /api/v1/dns/engine/switch/preview`, sonra `/switch`
  (`cmd/panel/dns_engine.go`, 3660. satır civarındaki işleyici; engelleyiciler
  commit sırasında yeniden çalışır). Sunucu kurulumu aynı engelleyicileri
  kullanır (`cmd/panel/setup_dns_operations.go`). Eylem seçimi:
  `dnsEngineAction`.
- Agent girişi: RPC `SwitchDNSEngineV1` (`cmd/agent/dns_engine_rpc.go`) →
  `hostDNSEngineBackend.Switch` (`cmd/agent/dns_engine_host.go`).
- Agent yeniden başlatma kurtarması: `RecoverSwitch` → paylaşılan `Reconcile`
  (`internal/dnsenginerecovery/reconcile.go`). Önce hedef denetlenir;
  uyuşmazlık durumunda bir `rolling-back` kararı yazılmadan önce dondurulmuş
  kaynağın tam olarak kanıtlanması gerekir. Agent, V2/V3/V4 ters işlemlerini
  hiçbir zaman kendisi çalıştırmaz; sahip komutunu adlandırır.
- Sahip komutları: `cmd/recovery/entry.go`. Salt-okur durum:
  `recovery dns-switch-status [--quiesced] [--request-id]`.
- Kapılar: `pdns_primary_switch_paused` (çiftin birincili → PowerDNS, herhangi
  bir kaynaktan) ve D-026'dan itibaren `bind_source_pdns_switch_unsupported`
  (hizmet veren BIND → PowerDNS, herhangi bir topolojide).

## Kayıt

| # | Yol (nasıl ulaşılır) | Günlük | Kurtarma: hedef başlamadan önce / hedef başladıktan sonra | Kurtarmada sahip değişikliği | Gerçek sistem kanıtı (sınır) | Durum |
|---|---|---|---|---|---|---|
| 1 | Boş BIND, tek sunucu (kurulum veya `install` kartı) | V1 | Agent, aynı istek, her iki tarafta da. Sahip CLI'ı yok (D-026 karar 2 ile kabul edildi). | Geri almada sahibi gözeten yapılandırma ön hâli | [Arch target-staged/before-write](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-TARGET-STAGED-ARCH-20260925.md): tek bir erken hücre, yalnızca ileri yönde. [Fresh-install cells 2026-09-29](../deploy/e2e/dns-kill-matrix/evidence/fresh-install-20260929/README.md): `bind__target-verified__before-write` (Arch, `named` başladıktan sonra kesildi, günlük `target-started` durumunda) ve `bind__target-verified__after-write` (Debian) Agent başlangıcında ikisi de ileri yönde yakınsadı; aynı `named` PID'si, 31/31 sağlık, yetkili UDP/TCP. | Debian ve Arch'ta başlangıç sonrası kesinti için **GEÇTİ**. **EKSİK**: Debian başlangıç öncesi hücre, yeniden başlatma; kesinti boyunca süreklilik iddia edilmiyor. |
| 2 | Boş BIND, çiftin birincili | V1 | Agent, aynı istek | 1 ile aynı | [Pair target-staged/after-write + management-disabled reboot](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md); [V3 deletion terminal](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) | Bu iki başlangıç öncesi hücre için ve ürün akışı için **GEÇTİ** ([çift 7](../deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md) t1/t2: bölge ekleme, düzenleme, sahip kaydından sonra kanıtla silme, yeniden ekleme, yönetim kapalıyken yeniden açılış). **EKSİK**: source-stopped, target-started, target-verified kesintileri |
| 3 | Boş BIND, çiftin ikincili | V1 | Agent, aynı istek | 1 ile aynı | Gruplar [5](../deploy/e2e/dns-kill-matrix/evidence/batch5-paired-first-20260929/README.md), [6a](../deploy/e2e/dns-kill-matrix/evidence/batch6a-fixed-hook-20260929/README.md), [6b](../deploy/e2e/dns-kill-matrix/evidence/batch6b-pdns-secondary-20260930/README.md), [7](../deploy/e2e/dns-kill-matrix/evidence/batch7-breadth-20260930/README.md): panelsiz BIND ve PowerDNS birincillerine karşı `intent`, `target-staged`, `target-verified` kesintileri, yönetim kapalıyken yeniden açılışlar; [çift 7](../deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md) t1/t3: bir CelikPanel birincili arkasında ürün akışı | **GEÇTİ** (1 Ekim 2026), sınırlarla: kesinti hücreleri panelsiz birincillere karşı çalıştı; CelikPanel-birincilli çalıştırmalarda ikincilde kesinti yoktu |
| 4 | Boş PowerDNS, tek sunucu (yalnızca APT sunucularında) | V1 | Agent `rollbackPDNSSwitch`, başlangıçtan önce ve sonra. Sahip CLI'ı yok (D-026 karar 2). | `verifyOwnerAwarePreimage` | [Fresh-install cells 2026-09-29](../deploy/e2e/dns-kill-matrix/evidence/fresh-install-20260929/README.md): `pdns-switch__target-started__after-write` geçti (başlangıçta geri alma, ardından aynı istek yeniden denemede ileri yönde tekrar çalıştı; ~3 sn DNS boşluğu). `pdns-switch__target-staged__after-write` **başarısız**: PowerDNS hiç başlamamıştı ve birimi kurulumun kendi kalıcı maskesiydi, ama V1 geri almanın durmuş hedef kanıtı `LoadState=loaded` gerektiriyordu; kurtarma `dns_native_recovery_unknown_after_restart` ile sonuçlandı, yeniden deneme reddedildi, DNS sunulmadı. Önceki durum (DNS yok) zarar görmedi. Düzeltme `VerifyStoppedFreshSourceTarget` (commit `1c336f6d`); [düzeltilmiş kaynak üzerinde yeniden çalıştırma](../deploy/e2e/dns-kill-matrix/evidence/fresh-install-rerun-20260929/README.md): `target-staged__after-write` (aynı istek baytları) ve `intent__after-write` ikisi de Agent başlangıcında geri alındı ve aynı istekle yeniden denemede ileri yönde yakınsadı; 31/31 sağlık, yetkili UDP/TCP. | Debian 13'te başlangıç öncesi (intent, target-staged; maskelenmiş hiç başlamamış birim) ve başlangıç sonrası kesintiler için **GEÇTİ**. **EKSİK**: `not-found`/`loaded` kabul edilen durumları ve çalışma zamanı maskesi reddi yalnızca bileşen testlerine sahip; yeniden başlatma yok; geri alınmış karar kodu günlük/ledger'dan çıkarsanmış, günlüğe yazılmamış. |
| 5 | Boş veya yeniden yapılandırılmış PowerDNS, çiftin ikincili | V1 | Agent, aynı istek | 4 ile aynı | Gruplar [6b](../deploy/e2e/dns-kill-matrix/evidence/batch6b-pdns-secondary-20260930/README.md) ve [7](../deploy/e2e/dns-kill-matrix/evidence/batch7-breadth-20260930/README.md): panelsiz BIND ve PowerDNS birincillerine karşı `intent`, `target-staged`, `target-started`, `target-verified`, `committed`, `rolling-back`, `rolled-back` kesintileri, daemon'ın yazdığı bir veritabanının geri alınması, yönetim kapalıyken yeniden açılışlar; [çift 7](../deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md) t2: `dns-peer-enroll --engine pdns` dahil bir CelikPanel BIND birincili arkasında ürün akışı | **GEÇTİ** (1 Ekim 2026) boş ikincil için, sınırlarla: kesinti hücrelerinde panelsiz birincil; yeniden yapılandırılmış ikincilin kesinti denemesi yok |
| 6 | Boş çift PowerDNS birincili, V3 (boş kaynak) | V3 (yalnızca testler) | Başlangıçtan önce: sahip CLI'ı `recover-dns-pdns-fresh-prestart`. Başlangıçtan sonra: Agent yalnızca ileri yönde; başlangıç sonrası ters işlem yok. | SQL/daemon sapma denetimi reddeder | [V3 native after-start forward + SIGKILL](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-native-20260928/README.md), [V3 prestart inverse](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md), [V3 zone lifecycle](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md). Genel RPC üzerinden erişilemez. | **GEÇTİ** (1 Ekim 2026, D-028, kapı ana dalda açık), sınırlarla. Genel RPC üzerinden: [8r. grup](../deploy/e2e/dns-kill-matrix/evidence/batch8r-pdns-primary-20261001/README.md) (Agent tarafından kendiliğinden geri alınan başlangıç öncesi kesintiler, başlangıç sonrası yalnızca ileri yönde, sahip yapılandırması ve SQL düzenlemeleri tutundu, Agent'ın serbest bıraktığı iş sahip komutuyla tamamlandı, bölge yaşam döngüsü, yönetim kapalıyken yeniden açılış), [9. grup](../deploy/e2e/dns-kill-matrix/evidence/batch9-pdns-primary-zero-zone-20261001/README.md) ve [12. grup](../deploy/e2e/dns-kill-matrix/evidence/batch12-zero-zone-complete-20261001/README.md) (sıfır bölge: aynı kesintiler, ilk bölgenin oluşturulması, sahip kaydından sonra sürdürülen ebeveynsiz silme, yeniden açılış), [çift 5](../deploy/e2e/dns-pair-acceptance/evidence/pair5-20261001/README.md) ve [çift 7](../deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md) (ürün akışı). Politika: başlangıçtan önce Agent kendisi geri alır ve sahip komutu serbest bırakılmış bir işi tamamlar; başlangıçtan sonra yalnızca ileri yönde; bir sahip değişikliği yalnızca DNS'i tutar. Sınırlar: hücre başına bir çalıştırma, hücrelerde Arch BIND ikincilli Debian 13 PowerDNS 4.9.17 birincili ve çift çalıştırmalarında CelikPanel ikincilleri; tasarım gereği başlangıç sonrası ters işlem yok; daemon yeniden damgalama kabulü ve kanıt zaman aşımı kodu gerçek sistemde gerçekleşmedi. |
| 7 | PowerDNS → BIND, tek sunucu, PowerDNS etkin / BIND devre dışı | **V2** | Agent `rolling-back` kararını yazar ve sonra reddeder; sahip CLI'ı `recover-dns-bind-switch`, rolling-back/rolled-back durumundan ters işlemi çalıştırır. Başlangıçtan sonra: hedef doğrulandıktan sonra yalnızca ileri yönde. | Ana yapılandırma düzenlemesi reddedilir, kanıt saklanır | [Protected owner CLI](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md): hedef başladıktan sonra rolling-back/after-write, sahip düzenlemesi reddedildi, CLI kesintiye uğradı, yeniden başlatma | Karar verilmiş geri alma hücresi için **GEÇTİ** (Agent etkisiz, hedef başlamıştı). Agent yeniden başlatılmış ve çalışır durumdayken başlangıç öncesi kesintiler (intent, target-staged) için **GEÇTİ**: ilk çalıştırma `7ad24282` üzerinde başarısız oldu ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-20260929/README.md)), `411398d9` içinde düzeltildi, [yeniden çalıştırma geçti](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-rerun-20260929/README.md). Agent yeniden başlatılmış ve çalışır durumdayken kaynak durdurulduktan sonraki kesintiler (`source-stopped`, `target-started` after-write) için **GEÇTİ** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-critical-20260929/README.md), kaynak `411398d9`, düzenek `7c5dfe17`): sahip komutu çıkış 0, günlük emekliye ayrıldı, ledger ve durum makbuzu bayt-özdeş, PowerDNS 53 numaralı bağlantı noktasının tek yetkilisi olarak yeniden hizmet veriyor, SOA seri numarası değişmedi, BIND durduruldu, 31/31; ölçülen PowerDNS kesinti üst sınırları 21,6 sn ve 9,4 sn, tasarım gereği DNS sürekli değil. **EKSİK**: V2 altında before-write uçları ve `rolled-back` hücresi; yeniden başlatma; yeniden çalıştırma çıkış durumu 3; geri almadan sonra diskte kalan staged BIND üretim ağacı, `rndc.key`, kurulum-sahipliği makbuzu ve yükseltilmiş `bind9` kütüphaneleri kalır; BIND birimi, başlamışsa maskesiz/devre dışı, başlamamışsa maskeli bırakılır. 25 Eylül 2026 tarihli altı Agent-aracılı BIND raporu V1 kullandı ve bu satır için tarihseldir. |
| 8 | PowerDNS → BIND, çift | V1 | Agent, aynı istek | sahibi gözeten | çift için yok | **EKSİK** |
| 9 | **BIND → PowerDNS**, tek sunucu ve çiftin ikincili (`switch` kartı, veya PowerDNS yokken `install`) | — | — | — | yok | D-026 karar 1 ile **DESTEKLENMİYOR, reddediliyor** (`bind_source_pdns_switch_unsupported`). D-026'dan önce bu satır *desteklenmiyor, reddedilmiyor* idi: V1 günlüğü, yalnızca Agent'ın ters işlemi, çağıranı olmayan V4 üreticisi. |
| 10 | BIND → PowerDNS, çiftin birincili | — | — | — | — | **DESTEKLENMİYOR, reddediliyor** (`pdns_primary_switch_paused`) |
| 11 | Çalışan BIND devralması (`adopt_unmanaged`, tek sunucu, Debian; Arch reddedilir) | V2 `SourceBIND` | Agent reddeder ve CLI'ı adlandırır; sahip CLI'ı `recover-dns-bind-adoption`, rolling-back/rolled-back durumundan | Aynı seri numaralı bölge düzenlemesi reddedilir | [Adoption owner CLI](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-ADOPTION-OWNER-CLI-20260927.md): tek hücre, yeniden başlatma yok, denetleyici devri kasıtlı olarak doğrulanmadı | Tek hücre için **GEÇTİ**. **EKSİK**: erken ve başlangıç sonrası kesintiler, yeniden başlatma |
| 12 | Durdurulmuş, yönetilmeyen BIND devralması | V1 (boş kurulum işlemi) | 1 ile aynı | 1 ile aynı | Gruplar [6a](../deploy/e2e/dns-kill-matrix/evidence/batch6a-fixed-hook-20260929/README.md), [6b](../deploy/e2e/dns-kill-matrix/evidence/batch6b-pdns-secondary-20260930/README.md), [7](../deploy/e2e/dns-kill-matrix/evidence/batch7-breadth-20260930/README.md): `target-staged` kesintisi, kurtarma sonrası yeniden başlatma, standart seçenekler, sahip `recursion`/`allow-transfer` direktifleriyle aynı-istek yeniden denemesinden önce geri yüklenen mühürlü ön hâl | **GEÇTİ** (1 Ekim 2026) |
| 13 | Harici PowerDNS devralması (`adopt`, tek sunucu, APT) | V1 | Agent ters işlemi, artı rolling-back/rolled-back durumundan sahip CLI'ı `recover-dns-pdns-adoption` | Statik yapılandırma düzenlemesi reddedilir | [Adoption cells](../deploy/e2e/dns-kill-matrix/README.md) (`NATIVE-PDNS-ADOPTION-*`), [owner edit](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-OWNER-EDIT-20260925.md), [protected owner CLI + reboot](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-PROTECTED-OWNER-CLI-20260926.md), [management-absent boot](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-MANAGEMENT-ABSENT-BOOT-20260925.md) | **GEÇTİ** (Debian 13, imzasız yerel derleme) |
| 14 | `reinstall_active` BIND, tek sunucu | V1 | Agent, aynı istek | 1 ile aynı | yok | **EKSİK** |
| 15 | `reinstall_active` PowerDNS | — | — | — | — | **DESTEKLENMİYOR, reddediliyor** (Panel ve Agent) |
| 16 | Genel motor kurulum/durdurma/kaldırma RPC'si | — | — | — | — | **DESTEKLENMİYOR, reddediliyor** (`genericDNSEngineMutationRefusal`) |
| 17 | Çift üzerinde bölge ekleme/düzenleme/silme (switch günlüğü değil, zone-sync V3 ledger'ı) | zone-sync v3 | `RecoverDNSZoneV3` üzerinden aynı istek, Agent aracılığıyla. Ebeveynsiz silme, sahip kaydı yokluğu kanıtlayana kadar bekleyen durumda kalır. | Türlenmiş sahip-düzenleme çakışması | [V3 deletion terminal](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md), [owner edit](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-OWNER-EDIT-20260927.md) ve [düzeltmesi](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-OWNER-EDIT-CORRECTION-20260927.md), [V3 zone lifecycle](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md), [owner-proof deletion](../deploy/e2e/dns-kill-matrix/evidence/pdns-bind-owner-proof-20260928/README.md) | **GEÇTİ** (1 Ekim 2026) her topolojide ürün akışı için: [çift 7](../deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md) (BIND/BIND, BIND/PowerDNS, PowerDNS/BIND: ekleme, kayıt düzenleme, `dns_peer_enrollment_required` üzerinde beklemede kalan silme, sahip kaydı, yeniden deneme ürünün kanıtıyla sildi, yeniden ekleme, yönetim kapalıyken yeniden açılış) ve [12. grup](../deploy/e2e/dns-kill-matrix/evidence/batch12-zero-zone-complete-20261001/README.md) (sahip kaydından sonra `RecoverDNSZoneV3` üzerinden sürdürülen, tek bölgenin ebeveynsiz silinmesi). **EKSİK**: bir zone-sync işlemi içinde gerçek sistem SIGKILL'i yok (şimdiye kadarki her kesinti motor switch'i üzerindeydi; düzenekte zone-sync V3 içinde etiketli bir kanca yok). |

Henüz denetlenmedi: `ConfigureDNSClusterV2` eşleme değişiklikleri.

## 1. maddenin durumu: 29 Eylül 2026'da adı belli sınırlarla kapandı

Sahip kararı (29 Eylül 2026): önce kaynak durdurulduktan sonraki PowerDNS →
BIND hücrelerini çalıştır; geçerlerse 1. madde kapanır ve kalan satırlar
adlarıyla 2. maddeye taşınır. Geçtiler. Tek sunucu kurulum ve switch yolları
— boş BIND, boş PowerDNS, PowerDNS → BIND, harici PowerDNS devralması (satır
1, 4, 7, 13) — artık hedefin başlamasından önceki ve sonraki bir kesinti için
gerçek sistem aynı-işlem kurtarma kanıtına sahip; sahip değişiklikleri
korunuyor ve her iki taraftaki davranış belirtiliyor; kurtarma sözleşmesi
olmayan yollar herhangi bir mutasyondan önce reddediliyor (satır 6, 9, 10,
15, 16). Üç tek sunucu yolu erişilebilir ama henüz bu düzeyde değil (satır
11, 12, 14); bunlar aşağıda adlarıyla taşınır, geçti sayılmazlar. Gerçek
sistem çalıştırmaları iki ürün kusurunu ortaya çıkardı ve yol boyunca
düzeltti (`1c336f6d`, `411398d9`), ve bir kurtarma çıkmazı kaldırıldı
(`7ad24282`); bileşen testleri bu üçünden hiçbirini ortaya çıkarmamıştı.

2. maddeye taşınan, açık, yeniden sınıflandırılmamış:

- satır 2, 3, 5, 8 ve 17: her çift topoloji, eksik panelsiz gerçek birincil
  eş ve zone-sync kesinti hücresi dahil;
- çalışan bir Agent ile satır 11 (çalışan-BIND devralması) ve satır 13
  (PowerDNS devralması): released-job kabulü yalnızca bileşen testlerine
  sahip;
- satır 12 (durdurulmuş, yönetilmeyen BIND devralması) ve satır 14 (BIND
  yeniden kurulumu): gerçek sistem kesinti denemesi yok;
- satır 7: V2 üreticisi altında before-write uçları ve `rolled-back`
  hücresi; tek sunucu kritik hücreleri, owner-inverse bayrağı olmadan
  çalıştırıldığında hâlâ V1 bekliyor;
- hiçbiri olmayan her satır için kurtarma sırasında yeniden başlatma veya
  güç kaybı;
- yönlendirme, 29 Eylül 2026'da kaynakta düzeltildi (bileşen testleri;
  gerçek sistemde yeniden koşu bekliyor): tamamlanan yeniden çalıştırmalar
  çıkış 0 veriyor; geri alma kararından önceki durum doğru sonraki adımı
  adlandırıyor. Hâlâ açık: serbest bırakılmış bir günlüğü hangi aktörün
  emekliye ayırdığına dair kalıcı bir kayıt yok; `recover-dns-pdns-fresh-prestart`
  ve V4 komutu terminal yeniden çalıştırmasında hâlâ çıkış 3 veriyor;
- bir PowerDNS → BIND geri almasından sonraki artıklar ve son durum, aynı
  gün kaynakta düzeltildi (geri alma yedeği: BIND her zaman paket
  korumasının maskesi altında sona erer; tam olarak hazırlanan sürüm
  kaldırılır; sahibin değiştirdiği ağaçlar korunur). Hâlâ açık: BIND'in
  çalışma dizinine yazdığı dosyalar ve yükseltilmiş `bind9` kütüphaneleri
  kalır;
- dinleyici kanıtları artık yerel ve bağlantı-yerel soketleri
  sınıflandırıyor (bileşen testleri; gerçek sistemde yeniden koşu
  bekliyor);
- her şey imzasız yerel derlemelerle Debian 13'tür (bir Arch BIND hücresi);
  imzalı sürüm ve kurulu sunucu kabulü 3. ve 4. maddelere aittir.

1. maddeyi kapatmak P0.4'ü kapatmaz.

## 2. maddenin durumu: 1 Ekim 2026'da adı belli sınırlarla kapandı

2. madde, çift yönlü çift topolojilerinin gerçek sistemde kabulünü istedi.
29 Eylül 2026 ile 1 Ekim 2026 arasında kill matrisi 4'ten 12'ye kadar
grupları çalıştırdı ve ürün-akışı çift sürücüsü iki tek kullanımlık
CelikPanel sunucusunda yedi kez çalıştı; her çalıştırma kanıtıyla birlikte
ilerleme günlüğündedir. Maddeyi kapatan:
[çift 7](../deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md)
üç topolojinin tümünde (BIND/BIND, BIND/PowerDNS, PowerDNS/BIND) bölge
ekleme, kayıt düzenleme, sahip kaydından sonra ürünün kanıtıyla bölge
silme, yeniden ekleme, panel ve Agent devre dışıyken DNS'in yanıt vermeye
devam ettiği bir yeniden açılış ve yönetimin dönüşü boyunca her adımı
geçti; ve
[12. grup](../deploy/e2e/dns-kill-matrix/evidence/batch12-zero-zone-complete-20261001/README.md)
sıfır bölgeli boş çift PowerDNS birincili hücrelerini, sahip kaydından
sonra sürdürülen bir ebeveynsiz silme ve yönetim kapalıyken bir yeniden
açılışla tamamladı. Bu kanıtla, boş çift PowerDNS birincili kapısı yalnızca
ölçülen kapsamla sınırlı olarak ana dalda açıktır (D-028).

Bugün durumu değişen satırlar: 3, 5, 6, 12 ve 17, hücrelerinde yazılan
sınırlarla **GEÇTİ**; satır 2 ürün akışını kazanıyor. Satır 8 (çiftin
PowerDNS → BIND'i) ve 14 (BIND yeniden kurulumu) **EKSİK** kalıyor; satır
9, 10, 15 ve 16 reddedilmiş kalıyor.

Yalnızca gerçek sistem çalıştırmalarıyla bulunan ve yol boyunca kaynakta
kapatılan ürün kusurları, düzeltmeyi ilk sınayan çalıştırmayla birlikte:
sıfır-bölge katalog denetimi (`8a548090`; çift 5), doğru kurulum durumları
ve arka plan lisans yenilemesi (`8a548090`; çift 3'ten itibaren), müşteri
arşivindeki kanıt (`8a548090`; paketleme iki kez doğrulandı, bayt-özdeş),
Arch'taki rndc anahtarı (`80bb4353`; çift 4), PowerDNS bildirim bağlantı
noktası (`a6d93f06`; çift 4), yönetilen ikincilin yerel döngü katalog
aktarımı ve denetleyici nedenleri (`0988bc9a`; çift 5), yalnızca DNS'li bir
alan adında posta aşaması (`0988bc9a`; çift 5), yönetilen bir PowerDNS
ikincilindeki sahibin denetleyicisi (`4941b605`; çift 6), BIND birincili
planının kaynak durumu (`37864789`; çift 6), bir sahip değişikliği için
alınan daemon yeniden damgalaması (`21a84211`; henüz gerçek sistemde
gözlemlenmedi) ve dalga sınırında atılan olumlu bir kanıt (`d3d65353`; çift
7).

Adı belli sınırlar, açık:

- hücre ve topoloji başına bir çalıştırma; bir dizüstü bilgisayar ana
  makinesinde tek kullanımlık QEMU konukları; düzenli yeniden açılışlar,
  güç kaybı yok; D-027'nin yalnızca test amaçlı lisansıyla imzasız yerel
  derlemeler; kurulu sunucu yok;
- daemon yeniden damgalama kabulü, kanıt zaman aşımı kodu ve bileşik
  sahip-düzenleme kodu gerçek sistemde gerçekleşmedi; gerçek sistem
  kanıtının adım sınırları tek örneklerdir (12 sn'ye karşı 11,1 sn'lik bir
  sınama yazması gözlemlendi);
- satır 17'de bir zone-sync işlemi içinde SIGKILL yok (zone-sync V3 içinde
  etiketli bir kanca yok);
- satır 3 ve 5: kesinti hücreleri panelsiz birincillere karşı çalıştı;
  yeniden yapılandırılmış bir PowerDNS ikincilinin kesinti denemesi yok;
- satır 8 (çiftin PowerDNS → BIND'i) ve satır 14 (`dpkg-statoverride`
  kararından sonraki BIND yeniden kurulumu) gerçek sistem denemesi
  görmedi;
- 1 Ekim 2026'dan önce kurulan bir BIND ikincili, panel o sunucunun DNS
  yapılandırmasını bir dahaki sefer yazana kadar sürüm-1 görüntülemesini
  korur ve bugün hiçbir panel eylemi bunu tetiklemiyor;
  `dns_peer_catalog_transfer_refused`, `detail` alanı,
  `mail_runtime_cleanup_failed`, `config_unreviewed`, rndc anahtarı geri
  alma ve sürüm 1'den 2'ye yükseltme denenmedi;
- müşteri arşivi hâlâ e2e düzenek kaynaklarını (betikler ve fikstürler)
  taşıyor; bu 4. maddeye ait bir karar;
- BIND → PowerDNS motor switch'i desteklenmiyor ve reddediliyor olarak
  kalır (D-026).

2. maddeyi kapatmak P0.4'ü kapatmaz ve kurulu panel güncellemesine yetki
vermez.

## 2. madde ilerleme günlüğü

Girdiler, yukarıda taşınan listeye karşı tarihli sonuçlardır. Bir satırın
kayıttaki durum hücresi yalnızca gerçek sistem kanıtı bulunduğunda değişir.

**29 Eylül 2026, 4. grup** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch4-adoption-reboot-20260929/README.md),
kaynak `8f86bdad`, ürün ikili dosyaları `411398d9` ile özdeş; tek sunucu
Debian 13'te yedi hücre, her biri bir kez çalıştırıldı):

| Satır | Hücre | Sonuç |
|---|---|---|
| 11 | çalışan-BIND devralması, Agent çalışırken sahip komutu | **geçti**: sahibin `named` süreci PID'sini korudu, sahip dosyaları özdeşti. Yeniden başlatma bu hücre için denenmedi (bayrak bu hücre için kabul edilmedi). |
| 13 | PowerDNS devralması, `intent`, yeniden başlatılan Agent kendisi geri alıyor, kurtarma sonrası yeniden başlatma | **geçti**: yeniden deneme yakınsadı, ikinci pencerede 31/31. |
| 13 | PowerDNS devralması, `rolled-back`, sahip komutu | **düştü (düzenek)**: ürün tarafı doğru görünüyordu; kurtarma probu, durum makbuzu olmadan sona eren bir harici-PowerDNS geri almasını sınıflandıramıyor. `3cc2de22`'den beri Agent bu kesintiyi kendisi tamamlıyor, bu yüzden hücrenin geçme tanımı değişiyor. |
| 7 | V2 PowerDNS → BIND `target-started`, Agent'ın kararı ile sahip komutu arasında yeniden başlatma, kurtarma sonrası yeniden başlatma | **geçti**: günlük ve ledger ilk yeniden başlatma boyunca değişmedi; PowerDNS ikincisinden sonra hizmet veriyor; kesinti üst sınırı, yeniden başlatma dahil 93,8 sn. |
| 7 | V2 PowerDNS → BIND `rolled-back` after-write | **geçti**. |
| 4 | boş PowerDNS `target-started`, kurtarma sonrası yeniden başlatma | **geçti**: ikinci pencerede 31/31. |
| 1 | boş BIND `target-verified` after-write, kurtarma sonrası yeniden başlatma | **düştü (güvenlik)**: `current` işaretçisi, kill işareti ile SIGKILL arasında kaldırıldı; yeniden başlatılan Agent ne doğrulayabildi ne de geri alabildi ve işi bilinmeyen olarak serbest bıraktı; `named` yeniden başlatmaya kadar bellekten hizmet verdi, sonra başlamayı başaramadı; DNS 31/31 reddetti. |

O gruptan bulgular, gerçek sistemde yeniden çalıştırılana kadar hepsi açık:

- etiketli kill kancası, çağıran goroutine'in süreç durmadan önce ürünün hata
  yoluna girmesine izin verebilir, bu yüzden bir kesinti adı geçen kesim
  sınırının ötesine düşebilir. Kancası bir mutasyon yapan hata yolunun
  ardından gelen saklanan hücreler, kanca düzeltildikten sonra yeniden
  çalıştırılmalıdır; o zamana kadar satır 1 ve satır 4'ün after-write
  geçişleri bu uyarıyı taşır;
- kancadan bağımsız olarak, ürün işaretçisi eksik olan doğrulanmış bir BIND
  hedefini onaramıyor ve yayımcının hata yolu, BIND hâlâ etkin olabilecekken
  işaretçiyi kaldırıyor. Bu durumda bir yeniden başlatma DNS'i düşürür;
- denetleyici, yeniden denemeleri ve probları başarısız olmuş bir akıştan
  sonra yeniden başlatıldı;
- kayıtlı bir kurtarma çalışma zamanı olmayan hücrelerde
  `recovery dns-switch-status` kullanılamadı.

Aynı gün kaynakta yapılan değişiklikler, yalnızca bileşen testleri, gerçek
sistemde yeniden koşu bekliyor: geri alma yedeği ve hazırlanan üretimin
kaldırılması (`f7a844f7`); bir ikincil tarafından kabul edilen her iki eş
katalog üreticisi, Agent tarafından `rolled-back` durumunda tamamlanan V1
devralması, devralma yeniden denemesi (`3cc2de22`); her iki motorun da
panelsiz gerçek bir birincili eşine karşı boş çiftin-ikincili hücreleri
için düzenek, devralma ve yeniden kurulum düzenekleri (`883102c1`,
`7ce3827e`).

**29 Eylül 2026, 5. grup, keşif amaçlı** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch5-paired-first-20260929/README.md),
kaynak `65b86621`, kill-kancası düzeltmesi `c04d8a2b`'den önce derlendi;
panelsiz birincil eşin ve çiftin-ikincili akışının ilk gerçek sistem
çalıştırması; her hücre bir kez):

| Satır | Hücre | Sonuç |
|---|---|---|
| 3 | boş BIND ikincili, bir BIND birincili karşısında, `target-verified` after-write | **düştü, kill-kancası yarış imzası**: işaretçi, işaret ile SIGKILL arasında kaldırıldı, 4. gruptaki c6'da olduğu gibi. Eş kararı geçti. Düzeltilmiş kancayla yeniden çalıştırılacak. |
| 3 | aynı hücre, kataloğu BIND katalog biçiminde sunan bir PowerDNS 5.1.4 birincili karşısında, yönetim kapalıyken yeniden açılış | **geçti**, eş kararı geçti, ikinci pencerede 31/31; kill-kancası uyarısını taşır. |
| 5 | boş PowerDNS ikincili, bir BIND birincili karşısında ve bir PowerDNS birincili karşısında, `target-started` after-write | **doğrulanmadı, kesim sınırından önce ürün kusuru**: PowerDNS 4.9.17 kataloğu tüketti, üyeyi aktardı ve yetkili olarak yanıt verdi, ardından üye satırının `options` alanına `{"consumer": {"unique": …}}` yazdı; Agent bu alanın boş olmasını gerektiriyor, sınırına kadar yeniden denedi ve başarısız oldu; geri alma, canlı veritabanını reddetti; mutasyon yöneticisi güvenli-kapalıya geçti; DNS sunulmadı. |
| 12 | durdurulmuş, yönetilmeyen bir BIND'in devralınması, `target-staged` after-write | **geçti**; kill-kancası uyarısını taşır. |
| 14 | BIND yeniden kurulumu | **çalıştırılmadı**: denetleyici, durum makbuzunu v1 anahtarlarıyla okudu ve herhangi bir mutasyondan önce durdu. |

Gerçek sistem yanıtları: PowerDNS, tüketilen üye satırlarına boş olmayan bir
`options` değeri yazıyor; bir BIND ikincili, BIND biçiminde bir katalog sunan
bir PowerDNS birincilinden üyeyi gerçekten yüklüyor.

**29 Eylül 2026, 6a. grup, düzeltilmiş kesim sınırı kancası** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch6a-fixed-hook-20260929/README.md),
kaynak `94cd124b`; on iki hücre, her biri bir kez çalıştırıldı, düzenek geçici
çözümü yok, yeniden çalıştırma yok): **on ikisi de geçti.** Hiçbir hücrede
gerçek sistem mutasyonu kill işareti ile SIGKILL arasına düşmedi.

| Satır | Hücre | Sonuç |
|---|---|---|
| 1 | boş BIND `target-verified` after-write (Debian) ve before-write (Arch), kurtarma sonrası yeniden başlatma | geçti; 4. grupta DNS kaybeden hücre işaretçisini korudu ve yeniden başlatmadan sonra hizmet verdi |
| 4 | boş PowerDNS `target-staged` ve `target-started` after-write, kurtarma sonrası yeniden başlatma | geçti |
| 7 | V2 PowerDNS → BIND `target-staged` ve `target-started`, Agent çalışıyor, sahip komutundan önce (kaynak durdurulduktan sonraki hücre) ve kurtarmadan sonra yeniden başlatma, ardından aynı switch yeni bir istek olarak tekrar | geçti; BIND ikisinde de koruma maskesi altında sona erdi, hazırlanan üretim kaldırıldı, yeniden çalıştırma çıkış 0, yeniden denenen switch maske kaldırılmış olarak ileri yönde tamamlandı |
| 11 | çalışan-BIND devralması, Agent çalışırken sahip komutu, komuttan önce ve kurtarmadan sonra yeniden başlatma | geçti |
| 13 | `rolled-back` ve `intent` durumunda PowerDNS devralması, yeniden başlatılan Agent kendisi tamamlıyor | geçti |
| 12 | durdurulmuş, yönetilmeyen bir BIND'in devralınması, kurtarma sonrası yeniden başlatma | geçti |
| 3 | gerçek bir BIND birincili karşısında ve kendi PRODUCER kataloğunu yayımlayan gerçek bir PowerDNS 5.1.4 birincili karşısında boş BIND ikincili; yönetim kapalıyken yeniden açılış | ikisi de geçti; Agent kabul ettiği katalog biçimini (BIND, PowerDNS) günlüğe yazdı ve eş kararları geçti |

O grupta denenmeyen: eksik-işaretçi onarımı (işaretçi bozulmadan kaldı),
herhangi bir PowerDNS ikincili, yeniden kurulum. Gözlemlendi ama
değerlendirilmedi: bir devralmadan önceki durum okuması çıkış 3 veriyor,
çünkü okuyucu, sahibin kurduğu, devre dışı bir `named.service`'i
sınıflandıramıyor; devralma `rolled-back` hücresinin kurtarmasından önceki
durum okuması, Agent sonradan kendisi tamamlasa da sahip komutunu
adlandırıyor. Hücre başına bir çalıştırma, bu çalıştırmalarda kesim
sınırının tutunduğunu gösterir; yarışın imkânsız olduğunu kanıtlamaz.

**30 Eylül 2026, 6b. grup** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch6b-pdns-secondary-20260930/README.md),
kaynak `6f2fb028`; sekiz hücre, her biri bir kez çalıştırıldı, düzenek geçici
çözümü yok, yeniden çalıştırma yok): yedisi geçti, biri doğrulanmadı. Kill
içeren her hücrede kesim sınırı tutundu.

| Satır | Hücre | Sonuç |
|---|---|---|
| 5 | boş PowerDNS ikincili, `target-started` after-write, gerçek bir BIND birincili karşısında ve kendi kataloğunu yayımlayan gerçek bir PowerDNS birincili karşısında; yönetim kapalıyken yeniden açılış | **geçti** ikisi de, eş kararları geçti. Yeniden başlatılan Agent, daemon'ın yazdığı bir veritabanını geri aldı, sonra yeniden deneme yakınsadı. |
| 5 | boş PowerDNS ikincili, `target-staged` after-write; `target-verified` before-write (kurtarma ileri yönde ilerledi); `rolling-back` after-write | **geçti** |
| 3 | Arch üzerinde boş BIND ikincili, `target-staged` before-write, kendi gerçek kataloğuna sahip bir PowerDNS birincili karşısında; yönetim kapalıyken yeniden açılış | **geçti** |
| 12 | durdurulmuş, yönetilmeyen bir BIND'in devralınması, standart seçenekler | **geçti**; sahip-direktifli yeniden deneme durumu denenmedi |
| 14 | sahip `bind9`'u kaldırdıktan sonra BIND yeniden kurulumu | **doğrulanmadı, herhangi bir günlükten önce ürün kusuru**: yönetilen BIND kurulumu, `/var/cache/bind` için grup adına göre bir `dpkg-statoverride` kaydeder; kaldırma işlemi `bind` grubunu sildi ve geçersiz kılmayı bıraktı, bu yüzden dpkg, sahip onu kaldırana kadar o sunucuda hiçbir paketi açmayı reddeder |

Gerçek sistem yanıtları: bir PowerDNS 4.9.17 tüketicisi, tükettiği bir
üyenin `options` alanına, birincilin sunduğu katalog biçimi ne olursa olsun
üyenin etiketiyle `{"consumer": {"unique": "<label>."}}` yazar ve tüketilen
üyeler için hiçbir üst veri, yorum veya anahtar yazmaz; tüketici satırının
kendisi `options` ve `catalog` alanlarını NULL olarak tutar.

**30 Eylül 2026, 7. grup, genişlik** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch7-breadth-20260930/README.md),
kaynak `dbd6a6b6`; Debian 13 kill konuğunda on dört hücre, her biri bir kez
çalıştırıldı, düzenek geçici çözümü yok, yeniden çalıştırma yok): **on
dördü de geçti**; kesim sınırı her hücrede tutundu.

| Satır | Hücreler | Sonuç |
|---|---|---|
| 7 | V2 PowerDNS → BIND before-write uçları: `target-staged` (eşe ulaşılamayan yerleşim), `source-stopped`, `target-started`, `rolled-back`; ve sahip komutundan önce bir yeniden başlatma, kurtarmadan sonra bir yeniden başlatma ve aynı switch'in yeni bir istek olarak yeniden denenmesiyle `source-stopped` after-write | geçti; before-write uçlarının ilk gerçek sistem çalıştırması; yeniden denenen switch iki yeniden başlatmadan sonra ileri yönde tamamlandı |
| 12 | sahip `recursion` / `allow-transfer` direktiflerini taşıyan, durdurulmuş yönetilmeyen bir BIND'in devralınması, kurtarma sonrası yeniden başlatma | geçti; sahip dosyaları yeniden denemeden önce mühürlü ön hâline dönmüştü ve aynı-istek yeniden denemesi devralma boyunca yakınsadı |
| 1 | boş BIND `intent` after-write, yeniden başlatma | geçti |
| 4 | boş PowerDNS `intent`, `target-verified`, `committed` after-write, yeniden başlatma | geçti |
| 3 | kendi gerçek kataloğuna sahip bir PowerDNS birincili karşısında boş BIND ikincili `intent` after-write, yönetim kapalıyken yeniden açılış | geçti |
| 5 | BIND ve PowerDNS birincilleri karşısında boş PowerDNS ikincili `intent`, `committed`, `rolled-back` after-write, yönetim kapalıyken yeniden açılış | geçti |

Kaynak durdurulduktan sonraki V2 hücrelerinde ölçülen PowerDNS kesinti üst
sınırları: 27,2 sn, 8,8 sn ve 25,9 sn (sonuncusu iki yeniden başlatmayı
çevreleyen işi kapsar, ama yeniden başlatmaların kendisi değerlendirilmez);
kaydedildi, sınırlandırılmadı.

**30 Eylül 2026, 8. grup, boş çift PowerDNS birincili** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch8-pdns-primary-20260930/README.md),
kabul dalı `accept/pdns-primary-gate-open` commit `916e1577`, kapı açık; ana
dal kapıyı kapalı tutuyor; genel Agent RPC'si üzerinden on hücre, her biri
bir kez çalıştırıldı, düzenek geçici çözümü yok, yeniden çalıştırma yok).
Satır 6. İkisi geçti, yedisi tek bir yanlış düzenek beklentisinde düştü,
biri doğrulanmadı. Kesim sınırı dokuz kesintinin tümünde tutundu.

| Hücre | Gözlemlenen ürün davranışı | Karar |
|---|---|---|
| `intent`, `target-staged`, `target-enable-intent` durumundaki başlangıç öncesi kesintiler | yeniden başlatılan Agent kurulumu kendisi geri aldı, günlüğü emekliye ayırdı, birim koruma-maskeli bekleme durumuna geri döndü; veritabanı, aday, makbuz veya dinleyici yok; aynı-istek yeniden denemesi yakınsadı; gerçek BIND ikincili birincil ile tam olarak aynı yanıtı verdi | **düştü (düzenek)**: eş denetimi, birincilin `www` A kaydını bölgenin kaydı yerine konuğun yönetim adresiyle karşılaştırdı |
| `intent` before-write durumunda günlük öncesi kesinti | iş, commit öncesi yeniden başlatma koduyla sona erdi; yeniden deneme yakınsadı | **düştü (düzenek)**, aynı neden |
| `target-started`, `target-verified`, `committed` durumundaki başlangıç sonrası kesintiler | yalnızca ileri yönde; aynı istek başarılı oldu; PowerDNS sürecini korudu | **düştü (düzenek)**, aynı neden; bölge yaşam döngüsü ve yeniden başlatmalar çalışmadı çünkü bunlar geçen bir kararın ardından gelir |
| kill ile Agent'ın yeniden başlaması arasında sahip SQL düzenlemesi, başlangıç sonrası | reddedildi; günlük ve veritabanı korundu; sahibin satırı korundu; ilgisiz bir mutasyon başlayabilirdi | **geçti** |
| `recover-dns-pdns-fresh-prestart` tarafından tamamlanan, Agent tarafından serbest bırakılmış başlangıç öncesi kurtarma | çıkış 0, kurulum öncesi durum, ledger Agent'ın serbest bırakmasıyla bayt-özdeş, yeniden çalıştırma çıkış 0 | **geçti** |
| başlangıç öncesi bir hücrede sahip yapılandırma düzenlemesi | Agent, herhangi bir mutasyondan önce `HOST_MUTATION_BUSY` yanıtı verdi; kesinti yok | **doğrulanmadı**; neden belirlenemedi |

Gözlemlenen katalog yeniden damgalaması: üst veri olmadan hazırlanan seri
numarası 1; ilk başlangıçta daemon bir `CATALOG-HASH` satırı ekledi ve
üretici SOA'yı bir epoch seri numarasına yeniden damgaladı; durum makbuzu ve
her iki sunucunun sunduğu seri numaraları buna eşitti. Kaydedilen
yönlendirme eksiği: iki sahip-düzenleme hücresinde `dns-switch-status`,
reddedilen sahip değişikliğini adlandırmıyor. Kaynakta `cc2d430b` ile
kapatıldı (yalnızca bileşen testleri).

**30 Eylül 2026, çift 1, ürün akışı, keşif amaçlı** ([kanıt](../deploy/e2e/dns-pair-acceptance/evidence/pair1-20260930/README.md),
ürün `aa6b9380`, gerçek kurulumcuyla kurulan iki CelikPanel sunucusu,
D-027'nin yalnızca test amaçlı lisansı, lisans hizmetine bağlanılmadı).
Satır 2, 3, 5, 17. Hiçbir topoloji bölge işlemlerine ulaşmadı; her durma bir
sürücü hatasıydı ve bu çalıştırma hiçbir satırı geçirmiyor.

| Topoloji (birincil / ikincil) | Ulaşılan | Durma noktası |
|---|---|---|
| BIND / BIND | her iki sunucuda da kurulum, lisanslama, ilk yapılandırma; ikincil kataloğu aldı | sürücü, bir ikincilde `pair_ready` bekledi; ürün orada `secondary_ready` raporluyor |
| BIND / PowerDNS | aynı | aynı |
| PowerDNS / BIND | plan `pdns_primary_switch_paused` ile reddedildi; geride hiçbir şey kalmadı | o derlemede kapı beklendiği gibi kapalı |

Ürün bulgusu: sunucu yeniden başlatması gerektiren bir çekirdek (kernel)
yükseltmesinden sonra, kurulum planı yeniden başlatmayı adlandırmak yerine
HTTP 500 yanıtı verdi. Kaynakta `a751e46a` ile kapatıldı
(`host_restart_required` ve diğer türlenmiş güvenlik duvarı engelleyicileri);
yalnızca bileşen testleri.

**30 Eylül 2026, çift 2, ürün akışı, keşif amaçlı** ([kanıt](../deploy/e2e/dns-pair-acceptance/evidence/pair2-20260930/README.md),
kapı açıkken ürün `916e1577`, sürücü `13213343`). Satır 2, 3, 5, 6, 17.
Hiçbir topoloji bölge işlemlerine ulaşmadı; bu çalıştırma hiçbir satırı
geçirmiyor.

| Topoloji (birincil / ikincil) | Ulaşılan | Durma noktası |
|---|---|---|
| BIND / BIND | her iki sunucuda da ilk yapılandırma; ikincilde `secondary_ready: true` | sürücü kuralı, birincilde `secondary_ready: false` değerini gerektiriyordu; ürün bu alanı bir birincilde atlıyor. `184f633b` içinde düzeltildi. |
| BIND / PowerDNS | aynı | aynı |
| PowerDNS / BIND | plan hiçbir engelleyici olmadan kabul edildi; PowerDNS kuruldu | **ürün kusuru**: bölgesi olmayan, boş çift PowerDNS birincili kendi katalog denetiminde başarısız oldu (boş liste, yok olanla karşılaştırıldı). 8. grup bu yolu yalnızca bir bölge varken ölçmüştü. |

Aynı çalıştırmadan ek ürün bulguları: sihirbaz, Agent doğrulanmış bir hata
kaydetmişken 45 dakika boyunca açık uçlu bir "sonuç bilinmiyor" gösterdi; bir
ikincildeki arka plan kurulumu, yalnızca HTTP istekleri lisans durumunu
yenilediği için `license_required` değerini bir kez okudu; müşteri arşivi
test kanıtı içeriyordu. Dördü de kaynakta `8a548090` ile kapatıldı (yalnızca
bileşen testleri): katalog denetimi sıfır üyeyi kabul ediyor; sihirbaz
doğrulanmış bir hatayı, bir kurtarma bekletmesini, bir geri almayı veya beş
dakikayla sınırlı bilinmeyen bir sonucu adlandırıyor; arka plan kurulumu,
herhangi bir politika değişikliği olmadan HTTP kapısıyla aynı lisans
yenilemesini kullanıyor; kanıt ve düzenek testleri arşivden budanıyor ve bir
koruma bunları reddediyor.

**1 Ekim 2026, 8r. grup, boş çift PowerDNS birincili, ikinci çalıştırma** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch8r-pdns-primary-20261001/README.md),
kabul dalı `accept/pdns-primary-gate-open-2` commit `e3591875`, kapı açık;
ana dal kapıyı kapalı tutuyor; aynı on hücre, genel Agent RPC'si üzerinden,
düzeltilmiş eş denetimiyle, her biri bir kez çalıştırıldı, düzenek geçici
çözümü yok, yeniden çalıştırma yok). Satır 6. Dokuzu geçti, biri bir düzenek
beklentisinde düştü. Kesim sınırı on kesintinin tümünde tutundu.

| Hücre | Gözlemlenen ürün davranışı | Karar |
|---|---|---|
| `intent` (yönetim kapalıyken yeniden açılışla), `target-staged`, `target-enable-intent` durumundaki başlangıç öncesi kesintiler; günlük öncesi kesinti | yeniden başlatılan Agent kurulumu kendisi geri aldı; yerel BIND ikincili birincille aynı yanıtı verdi | **geçti** |
| `target-verified`, `committed` durumundaki başlangıç sonrası kesintiler | yalnızca ileri yönde; aynı istek başarılı oldu | **geçti**; `committed` hücresinde bölge ekleme, düzenleme, silme ve yeniden ekleme geçti |
| `target-started` durumunda başlangıç sonrası kesinti, ardından bölge ekleme, düzenleme, silme, yeniden ekleme, ardından yönetim kapalıyken yeniden açılış | yeniden açılıştan önceki her karar geçti; yeniden açılıştan sonra iki sunucu da aynı katalog seri numarasını ve yeniden eklenen bölgeyi yanıtladı; yönetim kapalı kaldı ve DNS baştan sona yanıt verdi | **düştü (düzenek)**: yeniden açılıştan sonra eş denetimi, sunulan katalog seri numarasının durum makbuzundaki seri numarasına eşit olmasını şart koştu; oysa bölge işlemleri onu yükseltmişti. Ürün, makbuzdaki seri numarasını alt sınır sayar. |
| başlangıç öncesi bir hücrede sahip yapılandırma düzenlemesi | reddedildi ve tutuldu; hücre bu kez kesintisine ulaştı | **geçti** |
| başlangıç sonrası sahip SQL düzenlemesi | reddedildi; `dns-switch-status` artık ne kurulumun ne de daemon'un yazdığı veritabanı içeriğini adlandırıyor | **geçti** |
| `recover-dns-pdns-fresh-prestart` tarafından tamamlanan, Agent tarafından serbest bırakılmış başlangıç öncesi kurtarma | çıkış 0; yeniden çalıştırma çıkış 0 | **geçti** |

Gözlemlenen ve henüz açıklanmayan: ilk başlangıçtaki yeniden damgalamadan
yaklaşık 60 saniye sonra PowerDNS daemon'u kataloğu yeni bir `CATALOG-HASH`
ile bir kez daha yeniden damgaladı; bir hücrede herhangi bir bölge isteğinin
dışında, diğerinde silme isteğinin içinde. Hiçbir silme beklemede kalmadı, bu
yüzden sahip kaydı denenmedi. Senaryo bir bölge taşıyor, bu yüzden sıfır
bölgeli birincil bu çalıştırmayla ölçülmedi. `target-started` hücresi,
beklentisi düzeltilip hücre yeniden çalıştırılana kadar düşmüş kalır.

**1 Ekim 2026, çift 3, ürün akışı** ([kanıt](../deploy/e2e/dns-pair-acceptance/evidence/pair3-20261001/README.md),
kapı açıkken ürün `d1f2ad87`, D-027'nin yalnızca test amaçlı lisansı,
sürücü `8d94c8ff`, sürücü yaması yok, geçme kuralı değişikliği yok). Satır
2, 3, 5, 6, 17. Üç topoloji de her iki sunucuda kurulum, lisanslama, ilk
yapılandırma ve çift hazırlığını geçti; üçü de bölge ekleme içinde tek bir
yerel okumada durdu. Bu çalıştırma hiçbir satırı geçirmiyor.

| Topoloji (birincil / ikincil) | Ulaşılan | Durma noktası |
|---|---|---|
| PowerDNS / BIND | sıfır bölgeli boş çift PowerDNS birincili tamamlandı (ilk gerçek sistem ölçümü: katalog seri numarası epoch'a yeniden damgalandı, boş girdinin `CATALOG-HASH`'i, sıfır üye); ilk bölge iki sunucuda da tek seri numarasıyla yayımlandı | bölge ekleme: sürücü bölge durumunu `rndc` ile kanıtlıyor; ürünün Arch üzerindeki boş BIND'inde rndc anahtarı yok |
| BIND / PowerDNS | aynı adımlar; Arch üzerindeki BIND birincili bölgeyi yayımladı | aynı okuma, birincilde |
| BIND / BIND | aynı | aynı okuma, ikincilde |

Ürün bulgusu: Arch'ın `bind` paketi rndc anahtarı oluşturmuyor, Debian'ınki
oluşturuyor, ürün ise hiç oluşturmuyor. Ürünün kendi BIND silme kanıtı
`rndc zonestatus` komutunu anahtar bağımsız değişkeni olmadan çağırıyor;
bu yüzden ürünün kurduğu bir Arch BIND'inde satır 17'nin silme kanıtı
kaynak okumasına göre şüpheli, gözlemlenmedi. Karar: boş BIND kurulumu,
paket oluşturmadıysa anahtarı yerel araçla oluşturur, var olanı asla
yeniden yazmaz, ürünün oluşturduğunu kaydeder, geri almada yalnızca
değişmemişse kaldırır ve eksik anahtar yüzünden her rndc hatası sahip
yönlendirmesi taşıyan türlenmiş bir nedene dönüşür (düzeltme sürüyor;
yalnızca bileşen testleri). Gözlem, nedeni belirlenemedi: PowerDNS
birincili ikincile bildirimlerini 0 numaralı bağlantı noktasına
gönderilmiş, yanıtları da sahte olarak kaydetti; BIND ikincili iki bölgeyi
yine de aktardı. Bu kez açık uçlu bilinmeyen durum, çelişkili yönlendirme
ve `license_required` okuması yok.

**1 Ekim 2026, 9. grup, c04 yeniden ve sıfır bölgeli boş çift PowerDNS birincili** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch9-pdns-primary-zero-zone-20261001/README.md),
kabul dalı `accept/pdns-primary-gate-open-4` commit `3cceb29a`, kapı açık;
ana dal kapıyı kapalı tutuyor; genel Agent RPC'si üzerinden yedi hücre, her
biri bir kez çalıştırıldı, düzenek geçici çözümü yok, yeniden çalıştırma
yok). Satır 6 ve ebeveynsiz silme için satır 17. Beşi geçti, ikisi bir
düzenek kusurunda durdu. Kesim sınırı yedi kesintinin tümünde tutundu.

| Hücre | Gözlemlenen ürün davranışı | Karar |
|---|---|---|
| tek üye, `target-started` kesintisi, bölge yaşam döngüsü, yönetim kapalıyken yeniden açılış (8r. grubun c04'ü yeniden) | ileri yönde tamamlanma; ekleme, düzenleme, silme, yeniden ekleme; yeniden açılıştan sonra iki sunucu da en az makbuzdaki kadar tek bir seri numarası sundu, veritabanındaki üretici SOA'ya eşit, üyeler beklendiği gibi | yayın sonrası kuralla **geçti**; 8r. grubun düşüşü tekrarlanmadı |
| sıfır bölge, `target-staged` durumunda başlangıç öncesi kesinti | Agent kurulumu kendisi kurulum öncesi duruma geri aldı | **geçti** |
| sıfır bölge, `target-started` ve `committed` durumunda başlangıç sonrası kesintiler | yalnızca ileri yönde; sıfır üyeli katalog doğrulandı | **geçti** |
| sıfır bölge, başlangıçtan sonra sahip SQL düzenlemesi | reddedildi ve tutuldu; yalnızca-DNS bekletmesi | **geçti** |
| sıfır bölge, `committed` sonra bölge yaşam döngüsü; `target-started` sonra bölge yaşam döngüsü ve yeniden açılış | ekleme, sunucunun ilk bölgesini iki sunucuda da oluşturdu; düzenleme geçti; ebeveynsiz çocuğun silinmesi `dns_peer_enrollment_required` ile beklemede kaldı, iki sunucu da onun için REFUSED yanıtı verdi, iki katalog da sıfır üyeye döndü; sahip kaydı paketlenmiş araçlarla yapıldı | **durdu (düzenek)**: tetikleyicinin devam yolu bekleyen işi ebeveynin adıyla eşleştirdi, oysa iş çocuğun adını taşıyor; devam, yeniden ekleme ve yeniden açılış çalışmadı |

Sıfır üyeyle ilk gerçek sistem ölçümleri: PowerDNS 4.9.17 ilk başlangıçta
`CATALOG-HASH` yazıyor ve üretici seri numarasını epoch'a yeniden
damgalıyor; durum makbuzu, iki sunucu ve veritabanı uyuşuyor; yerel BIND
ikincili boş kataloğu SOA, NS `invalid.`, `version` TXT `2` ve üyesiz
sunuyor; `notified_serial` 1'de kalıyor. 3,5 ile 10 dakikalık pencerelerde
üye değişikliği olmadan ikinci bir yeniden damgalama yok; gözlemlenen tek
örnek ilkinden 120 sn sonra ve üye kümesi değiştikten sonra geldi.
Kaydedilen yönlendirme eksiği: bekleyen ebeveynsiz silme için Agent'ın
ledger metni sahip aracını adlandırmıyor; Panel'in ekran metni adlandırıyor.
Düzeltilmemiş derlemenin 0 numaralı bağlantı noktası bildirim satırları
tekrarlandı; ikincil her değişikliği saniyeler içinde aktardı (kaynakta
`a6d93f06` ile kapatıldı).

**1 Ekim 2026, çift 4, ürün akışı** ([kanıt](../deploy/e2e/dns-pair-acceptance/evidence/pair4-20261001/README.md),
kapı açıkken ürün `96657d67`, D-027'nin yalnızca test amaçlı lisansı,
sürücü `92fc5eee`, sürücü yaması yok, geçme kuralı değişikliği yok). Satır
2, 3, 5, 6, 17. Üç topoloji de kurulum, lisanslama, ilk yapılandırma, çift
hazırlığı, bölge ekleme, kayıt ekleme ve kayıt düzenlemeyi geçti; üçü de
bölge silme içinde iki ürün kusurunda durdu. Bu çalıştırma hiçbir satırı
geçirmiyor.

| Topoloji (birincil / ikincil) | Ulaşılan | Durma noktası |
|---|---|---|
| BIND / BIND; PowerDNS / BIND | ebeveynsiz silme `dns_peer_enrollment_required` ile beklemede bırakıldı; paketlenmiş araçlarla sahip kaydı başarılı oldu; bölge iki sunucudan da çoktan kalkmıştı | yeniden deneme 300 sn boyunca `dns_peer_inspection_unknown` ile beklemede kaldı: ürünün yönetilen BIND ikincilindeki denetleyicisi yerel (loopback) katalog aktarımına ihtiyaç duyuyor, ürün ise aktarıma yalnızca çift birincilden izin veriyor; yönlendirme reddedilen yerel aktarımı adlandırmıyor |
| BIND (Arch) / PowerDNS | bölge ekleme, kayıt ekleme, kayıt düzenleme | alan adı silme, yalnızca DNS'li bir alan adı için posta temizliği çalıştırdı; Arch'ta `/var/mail` dağıtımın hazır sembolik bağı ve posta sunucusu kurulu değil; güvenli açma başarısız oldu, akış `mail_runtime_cleanup` aşamasında bilinmeyen durumla durdu, ekran eylemci ve eylem içermeyen genel bekleme metnini gösterdi; DNS silme hiç başlamadı |

İlk kez gerçek sistemde ölçülen: `80bb4353`'ün rndc anahtarı her Arch
BIND'inde named ilk başlamadan önce oluşturuldu ve ürünün oluşturduğu olarak
kaydedildi; Debian'ın paket anahtarı sağlanmış olarak kaydedildi ve
dokunulmadı; rndc her yerde çalıştı. `a6d93f06`'nın PowerDNS birincili her
bildirimi 53 numaralı bağlantı noktasıyla gönderdi: altı değişiklikte sıfır
sahte yanıt ve sıfır 0 numaralı bağlantı noktası hatası, ikincil her
değişiklik için bölge başına tek bildirim aldı. Bölge yeniden ekleme,
yönetim kapalıyken yeniden açılış ve yönetimin dönüşü çalışmadı. İki
kusurun da düzeltmesi sürüyor (yalnızca bileşen testleri).

**1 Ekim 2026, 10. grup, sürdürülen sıfır bölgeli silmeler** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch10-zero-zone-resume-20261001/README.md),
düzenek ve taze z05 ürünü `accept/pdns-primary-gate-open-6` commit
`0d4c0324`'ten; z04, 9. grubun tutulan kaplaması üzerinde ürün `3cceb29a`
ile sürdürüldü; düzenek geçici çözümü yok, yeniden çalıştırma yok). Satır 6
ve 17. İki hücre de geçmedi. Düzeltilmiş tetikleyici çocuk bölgenin bekleyen
silmesini eşleştirdi, Agent işi kirayla `recovering` durumuna aldı ve sahip
kaydıyla kurulan denetleyici kanalı panelsiz BIND ikinciline ilk kez gerçek
sistemde, iki kez çalıştı; iki seferde de Agent silmeyi
`dns_peer_inspection_unknown` ile beklemeye geri aldı, bu yüzden kurtarılan
silme, yeniden ekleme ve z05 yeniden açılışı çalışmadı. Çıkarım, gözlem
değil: düzeneğin panelsiz Arch BIND ikincilinde denetleyicinin ihtiyaç
duyduğu ve panelsiz sahibin hazırladığı rndc anahtarı yok (düzenek
düzeltmesi sürüyor). Denetleyicinin kendi nedeni ne günlüğe yazıldı ne
gösterildi; kaynakta `0988bc9a` ile kapatıldı (gözden geçirilmiş neden
belirteçleri; yalnızca bileşen testleri). z05 askıda tutma çalıştı: denetim
noktası ve önyükleme kimlikleri değişmeden 6,5 dakika askıda kaldı. z05'te
düzeltilmiş bildirim derlemesi: 53 numaralı bağlantı noktasıyla altı
bildirim, sıfır sahte yanıt, sıfır 0 numaralı bağlantı noktası satırı. İlk
başlangıçtan +60 sn ve +120 sn sonra iki daemon yeniden damgalaması, her
biri bir üye değişikliğinden sonra.

**1 Ekim 2026, çift 5, ürün akışı: ilk tamamlanan topoloji** ([kanıt](../deploy/e2e/dns-pair-acceptance/evidence/pair5-20261001/README.md),
kapı açıkken ürün `b1e32275`, D-027'nin yalnızca test amaçlı lisansı,
sürücü `4941b605`, sürücü yaması yok, geçme kuralı değişikliği yok). Satır
2, 3, 5, 6, 17. Bu kayıt durum hücrelerini değiştirmiyor; onları kapanış
değerlendirmesi yargılar.

| Topoloji (birincil / ikincil) | Ulaşılan | Durma noktası |
|---|---|---|
| PowerDNS / BIND | **her adım**: kurulum, lisanslama, ilk yapılandırma, çift hazırlığı, bölge ekleme, kayıt ekleme ve düzenleme, bölge silme (`dns_peer_enrollment_required` ile beklemede; paketlenmiş araçlarla sahip kaydı; yeniden deneme ürünün kanıtıyla sildi: iki sunucuda REFUSED, ikisinde de yerel olarak yok, katalog üyesi yok), bölge yeniden ekleme, iki sunucunun da yanıt verdiği ve ledger'ların değişmediği yönetim kapalıyken yeniden açılış, hazırlık kuralının tuttuğu yönetim dönüşü | — |
| BIND / BIND; BIND (Arch) / PowerDNS | bölge ekleme, kayıt ekleme ve düzenleme; ilk silme beklemede; kayıt (`--engine pdns` ilk kez uçtan uca); denetleyici alışverişi tamamlandı | yeniden deneme, hiçbir şey değişmediği hâlde `dns_peer_owner_edit_unknown` ile beklemede kaldı: BIND birincilinin yayılım planı kaynak durumu taşımıyor, denetim sonrası sonda seçimi boş motoru reddediyor ve bu iç ret sahip-düzenleme koduna eşleniyor (zamanlamayla örtüşen kaynak okuması; düzeltme sürüyor) |

İlk kez gerçek sistemde ölçülen: yönetilen BIND ikincilinin katalog
bildirimi yerel döngü aktarımlarına izin veriyor ve makbuz sürüm 2
taşıyor, hiçbir aktarım reddedilmedi; yalnızca DNS'li alan adlarında posta
aşaması Arch dahil her sunucuda atlandı; yönetilen PowerDNS ikincili sahip
denetleyicisi tarafından reddedilmedi; rndc anahtarları Arch'ta ürün
tarafından oluşturuldu, Debian'da paket tarafından sağlandı; 53 numaralı
bağlantı noktasıyla on üç PowerDNS bildirimi ve sıfır sahte yanıt.
Denenmeyenler: `dns_peer_catalog_transfer_refused`, `detail` alanı,
`mail_runtime_cleanup_failed`, `config_unreviewed`, mevcut bir ikincilde
sürüm 1'den 2'ye yükseltme, rndc anahtarı geri alma.

**1 Ekim 2026, 11. grup, taze fikstürlerde sürdürülen sıfır bölgeli silmeler** ([kanıt](../deploy/e2e/dns-kill-matrix/evidence/batch11-zero-zone-complete-20261001/README.md),
kabul dalı `accept/pdns-primary-gate-open-8` commit `542ccc8e`, kapı açık;
taze fikstürler; düzenek geçici çözümü yok, yeniden çalıştırma yok). Satır
6 ve 17. İki hücre de geçmedi. Panelsiz BIND ikincili artık sahibin
hazırladığı rndc anahtarını taşıyor (makbuz `created`, rndc durumu önce ve
sonra iyi) ve 10. grubun `dns_peer_inspection_unknown` kodu tekrarlanmadı.
Sürdürülen iki ebeveynsiz silme de kirayla `recovering` durumuna geçti ve
hiçbir üye, bölge ya da kimlik değişmediği hâlde `dns_peer_owner_edit_unknown`
ile beklemeye geri döndü: z04 herhangi bir denetleyici alışverişinden önce,
PowerDNS daemon'u denemenin içinde kataloğu yeniden damgalarken; z05 tam bir
denetleyici alışverişinden sonra, silmeden altı dakika sonra, daemon'un boş
kataloğu yeniden damgalamasının ardından. Ürün bulgusu: daemon'un dönemsel
katalog yeniden damgalaması (yaklaşık her 60 sn'de bir, üye kümesi
değiştiğinde) bir silme ile kanıtı arasına düştüğünde sahip değişikliği
sayılıyor; 5. çiftin tamamlanan topolojisi 14 sn içinde, herhangi bir
yeniden damgalamadan önce yeniden denemişti. Kodu hangi denetimin ürettiği
günlüğe yazılmıyor. Düzeltme sürüyor: kimlik, üyeler ve üye seri numaraları
aynıysa yeniden damgalama silmenin ömrünün her noktasında kabul edilir,
kayıtlı kanıt yeniden damgalanır ve kod, farklı olan denetimi adlandıran bir
ayrıntı taşır. Kurtarılan silme, yeniden ekleme ve z05 yeniden açılışı
çalışmadı.

**1 Ekim 2026, çift 6, ürün akışı** ([kanıt](../deploy/e2e/dns-pair-acceptance/evidence/pair6-20261001/README.md),
kapı açıkken ürün `e50fe50a`, D-027'nin yalnızca test amaçlı lisansı,
sürücü `37864789`, sürücü yaması yok, geçme kuralı değişikliği yok). Satır
2, 3, 5, 6, 17. Üç topoloji de kayıt düzenlemeye kadar her adımı geçti; üçü
de bölge silme içinde tek bir yeni ürün kusurunda durdu. Bu çalıştırma
hiçbir satırı geçirmiyor.

| Topoloji (birincil / ikincil) | Ulaşılan | Durma noktası |
|---|---|---|
| BIND / BIND; BIND (Arch) / PowerDNS; PowerDNS / BIND | ilk silme `dns_peer_enrollment_required` ile beklemede; sahip kaydı (PowerDNS ikincilinde `--engine pdns`); denetim çalıştı ve yanıtı kabul edildi; BIND birincillerinde denetim sonrası katalog aktarımları ve aktarımsızlık sondası ilk kez çalıştı, iç-kanıt ya da sahip-düzenleme kodu görülmedi (5. çiftin kusuru, çalıştırmanın ulaştığı yere kadar kapandı) | yeniden deneme üçünde de 300 sn boyunca `dns_peer_journal_unknown` ile beklemede kaldı; bölge iki sunucudan da yerel olarak kalkmıştı. Kaynak ve zamanlamadan çıkarım, günlüğe yazılmadı: kanıt dalgasının 15 sn sınırı, yanıt kabul edildikten sonra tek-kullanımlık tüketme adımının sunucu yeniden doğrulaması içinde doldu ve dolan süre "günlük bilinmiyor" olarak raporlanıyor; 5. çiftte 13,8 sn'de tamamlanan PowerDNS topolojisi burada 17,1 sn sürdü. Yönlendirme somut bir denetim adlandırmıyor. |

Düzeltme sürüyor: olumlu bir yanıt asla süre yüzünden atılmaz; kanıtın
bütçesi adım başına sınırlarla tüm alışverişi kapsar; yanıt kabul
edilmeden önce dolan süre kendi kodunu ve metnini alır; altta yatan hata ve
denetleyicinin sonuç alanları sınırlı biçimde günlüğe yazılır.

**1 Ekim 2026, çift 7 ve 12. grup: her topoloji ve iki sıfır-bölge hücresi
de tamamlandı** ([çift 7](../deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/README.md), [12. grup](../deploy/e2e/dns-kill-matrix/evidence/batch12-zero-zone-complete-20261001/README.md);
kapı açıkken ürün `2efc4de2` = ana dal `d3d65353`, D-027'nin yalnızca test
amaçlı lisansı, sürücü ve düzenek aynı commit'lerden, yama yok, geçme
kuralı değişikliği yok, yeniden çalıştırma yok). Satır 2, 3, 5, 6, 17.

| Çalıştırma | Sonuç |
|---|---|
| çift 7, BIND / BIND; BIND (Arch) / PowerDNS; PowerDNS / BIND | üçünde de **her adım geçti**: kurulum, lisanslama, ilk yapılandırma, çift hazırlığı, bölge ekleme, kayıt ekleme ve düzenleme, bölge silme (`dns_peer_enrollment_required` ile beklemede, sahip kaydı, yeniden deneme 19,9 ile 25,5 sn arasında ürünün kanıtıyla sildi: iki sunucuda REFUSED, yerel olarak yok, katalog üyesi yok, ledger işi başarılı oldu, sınama tüketildi ve emekliye ayrıldı), bölge yeniden ekleme, iki sunucuda da DNS'in yanıt verdiği yönetim kapalıyken yeniden açılış, yönetimin dönüşü. Agent'ın adım-süresi satırları, 6. çiftin tek 15 sn sınırının başarısız olduğu yerde 17,4, 20,9 ve 23,1 sn'de tamamlanan kanıtları gösteriyor; PowerDNS denetleyicisinin yanıtı (`transferred`, `absent`, `unloaded`) ilk kez gerçek sistemde kaydedildi. Sahip kaydından sonra PowerDNS birincilinde ek bir silme tek denemede tamamlandı. |
| 12. grup, z04 (`committed` kesintisi, bölge yaşam döngüsü, sıfır bölge) | **geçti**: ilk bölge oluşturuldu ve düzenlendi, silme beklemede, sahip kaydı, sürdürülen silme doğrulandı ve yayımlandı (iş başarılı oldu), iki sunucuda da sıfır üye ve REFUSED, yeniden ekleme. |
| 12. grup, z05 (`target-started` kesintisi, bölge yaşam döngüsü, yönetim kapalıyken yeniden açılış, sıfır bölge) | **geçti**: sürdürmeye kadar aynısı, ardından devam, yönetim kapalı, iki yeniden açılış da, iki sunucuda ve veritabanında yeniden eklenen çocuğun tek üye olduğu yayın-sonrası kuralı altında yeniden açılış sonrası kontrol. |

Bu çalıştırmalarda denenmeyen: daemon yeniden damgalama kabulü (sürdürülen
bir deneme içine hiçbir yeniden damgalama düşmedi), kanıt zaman aşımı kodu,
bileşik sahip-düzenleme kodu. Not edilen: sınama yazması z05'te 12 sn'lik
sınırına karşı 11,1 sn sürdü ve tüketme adımı 6,5 ile 9,5 sn arasındaydı;
yüklü bir dizüstü bilgisayar ana makinesinde tek örnekler.

4. ve 5. gruplardan sonra listelenen kaynak değişikliklerinin gerçek
sistemdeki kapsamı:

| Değişiklik | İlk karşılayan gerçek sistem çalıştırması |
|---|---|
| kesim sınırı durdurma (`c04d8a2b`) | 6a. grup, her hücre |
| tüketilen PowerDNS üye seçenekleri; daemon yazdıktan sonra boş bir PowerDNS ikincilinin geri alınması (`66db850c`) | 6b. grup |
| güvenli-kapalı bir mutasyon yöneticisi yerine yalnızca-DNS bekletmesi (`66db850c`) | 8. grup, sahip SQL düzenlemesi hücresi |
| eksik-işaretçi onarımı ve işaretçi sıralaması (`c04d8a2b`) | yok: düzeltilmiş kancayla işaretçi artık kaybolmuyor, bu yüzden onarımın kendisi yalnızca bileşen testlerine sahip |
| `target-verified` yazması hata döndürdüğünde ama kalıcı olduğunda ileri yönde tamamlanma; eksik bir işaretçiyi adlandıran durum (`66db850c`) | yok; yalnızca bileşen testleri |

`8a548090` itibarıyla gerçek sistemde yeniden koşu bekleyenler: başlangıçtan
önce ve sonra sıfır bölgeli boş çift PowerDNS birincili; bölge ekleme,
düzenleme, silme ve yeniden ekleme, silme kanıtı, sahip kaydı ve yönetim
kapalıyken yeniden açılış boyunca üç ürün akışı; sihirbaz durumları ve
gerçek bir sunucuda sunucu yeniden başlatma engelleyicisi;
`dpkg-statoverride` kararından sonra yeniden kurulum.

## 1. maddeyi ne kapattı

Devrin 1. maddesinin kapanma koşulu şudur: desteklenen kesintilerde aynı işlem
devam eder veya geri alınır; sahip değişiklikleri korunur; başlangıç öncesi ve
başlangıç sonrası davranış açıktır. İş, gerçekleştiği sırayla:

1. Satır 4 — boş tek sunucu PowerDNS: başlangıç sonrası hücre 29 Eylül
   2026'da geçti; başlangıç öncesi hücre doğrulanmış bir kusuru ortaya
   çıkardı (durmuş hedef kanıtı, boş kaynak günlüğü için kurulumun kendi
   maskesini reddetti). `1c336f6d` içinde düzeltildi; `target-staged` ve
   `intent` hücreleri aynı gün düzeltilmiş kaynak üzerinde geçti
   (tamamlandı).
2. Satır 1 — boş tek sunucu BIND: başlangıç sonrası hücreler 29 Eylül 2026'da
   Debian ve Arch'ta geçti (tamamlandı). Düzenek artık boş bir kaynakla
   `bind__target-verified__{before,after}-write__standalone__*` hazırlıyor;
   before-write ucu, günlük hâlâ `target-started` durumundayken BIND
   başladıktan sonra kesiyor. Boş BIND'in `target-started`, `source-stopped`
   ve `rolled-back` durumlarındaki hâli tasarım gereği kapalı kalır: bu
   hücreler yönetilen-PowerDNS-kaynak hücreleri olarak tanımlıdır ve boş bir
   çalıştırma oradaki bir geçişin anlamını değiştirir.
3. Satır 7 — V2 üreticisi altında PowerDNS → BIND: Agent-karar-verir /
   sahip-yürütür ayrımının normal yolda gösterilmesi için **sahip
   komutundan önce Agent yeniden başlatılmış olarak** bir erken kesinti
   (intent veya target-staged). Kabul kuralı ve denetleyici akışı kaynakta
   29 Eylül 2026'dan beri var.
   [İlk gerçek sistem çalıştırması](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-20260929/README.md),
   `7ad24282` üzerinde, `target-staged` ve `intent` hücreleri:
   **başarısız oldu**, güvenlik geçti. Yeniden başlatılan Agent kararı yazdı
   ve kirayı serbest bıraktı, salt-okur durum komutu herhangi bir mutasyon
   yapmadan komutu adlandırdı, ve sahip komutu kabul edildi — ardından
   gerçek sistem doğrulamasında reddedildi (`DNS target is not a loaded
   unit`): BIND hedefi ilk başlamadan önce paket korumasının kalıcı
   maskesi altında oturur; sahip tarafındaki durmuş-hedef kanıtı ve birim
   kimliği okuyucusu bunu reddeder. PowerDNS aynı süreç üzerinde hizmet
   vermeye devam etti (31/31), sahip dosyaları ve ledger değişmedi, günlük
   korundu. Doğrulanmış kusur, `411398d9` içinde düzeltildi (V2 BIND
   switch ters işlemi için hiç başlamamış, koruma altında mühürlenmiş
   hedef sınıfı). [Düzeltilmiş kaynak üzerinde yeniden
   çalıştırma](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-rerun-20260929/README.md):
   iki hücre de **geçti** (`rolled_back_source_serving`): sahip komutu
   çıkış 0, günlük `rolled-back` sonra emekliye ayrıldı, ledger Agent'ın
   serbest bırakmasıyla bayt-özdeş, PowerDNS baştan sona aynı süreç
   üzerinde (31/31), BIND birimleri hâlâ koruma maskesi altında ve hiç
   başlamadı, staged BIND yapılandırması ön hâline geri yüklendi, sahip
   dosyaları değişmedi, durum ve Panel metni switch'i uzlaştırılmış olarak
   raporluyor (başlangıç öncesi kesintiler için tamamlandı). Kaynak
   durdurulduktan sonraki kesintiler (`source-stopped`, `target-started`)
   aynı gün akışın kritik varyantında
   [geçti](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-critical-20260929/README.md)
   (tamamlandı).
4. Satır 3 ve 5 — boş çiftin ikincili, BIND ve PowerDNS — hazırlanamaz:
   hiçbir eş betik, ürün kataloğunu ve üye bölgelerini AXFR/NOTIFY ile
   konuğa sunan panelsiz gerçek bir *birincili* canlandırmıyor; ayrıca
   tetikleyici, eski yeniden yapılandırma manifestiyle aynı şekle sahip
   olduğu için boş PowerDNS çiftin-ikincili manifestini reddediyor. Bu,
   2. maddenin iki topolojili kabul işidir; satırlar orada açık kalır ve
   N/A olarak yeniden sınıflandırılmaz.

İşlem yönlendirmesi (D-024), kaynakta 29 Eylül 2026'da kapatıldı:
`recovery dns-switch-status` ve Agent'ın reddetmeleri artık
`recover-dns-pdns-fresh-prestart`, `recover-dns-bind-switch` ve
`recover-dns-pdns-adoption` komutlarını (satır 6, 7, 13) tam
`--request-id` ile adlandırıyor; bunu, komutların kendisinin çalıştırdığı
aynı kabul yüklemlerini kullanarak yapıyor, aksi hâlde açıkça
"uygulanabilir sahip kurtarma komutu yok" metnini basıyor. Panel, ledger
mesajını olduğu gibi gösterir; durum komutuna yönlendirir.

**Agent yeniden başlatmasından sonra sahip komutu (satır 7, 11, 13) —
düzeltildi; satır 7'nin başlangıç öncesi kesintileri için gerçek sistem
kanıtı, satır 11 ve 13 için yalnızca bileşen testleri.** Yeniden başlatılan
bir Agent kurtarmayı tamamlayamadığında günlüğü elinde tutar ve ledger
kirasını serbest bırakır (`dns_native_recovery_unknown_after_restart`);
kanıt okuyucu bunu `released-undecided` olarak raporlar. 29 Eylül 2026'ya
kadar `recover-dns-bind-switch`, `recover-dns-bind-adoption` ve
`recover-dns-pdns-adoption` yalnızca etkin kira durumunu veya sonlanmış bir
rolled-back işini kabul ediyordu; bu yüzden Agent çalışırken `rolling-back`
durumunda bırakılmış bir günlük için kabul edilen hiçbir sahip komutu
yoktu. Satır 7 ve 11 için saklanan gerçek sistem geçişleri Agent etkisizken
alınmıştı. V2 üreticisi bunu satır 7 için normal yol hâline getirir. Üç
komut artık ayrıca tam olarak Agent'ın kendi kasıtlı serbest bırakmasını da
kabul eder: yalnızca o gerekçe kodu, tam olarak o isteğin `rolling-back`
veya `rolled-back` durumundaki günlüğü, Agent'ın kendi released-job
yüklemini geçen serbest bırakılmış iş (işçi yok, kira yok) ve mevcut kilit,
işçi-dışlama, gerçek sistem kanıtı ve sahip-değişikliği denetimlerinin
tümü değişmeden. Şema veya sürüm değişikliği yok. Komut ters işlemi
çalıştırır, `rolled-back` kontrol noktasını yazar ve günlüğü emekliye
ayırır; tamamlanmış ledger işini yeniden yazmaz. Sahibe daha sonra
gösterilen şey okuma anında hesaplanır: günlüğü artık elde tutulmayan
serbest bırakılmış bir iş, hâlâ engelliyormuş gibi değil, uzlaştırılmış
olarak raporlanır. `recover-dns-pdns-fresh-prestart` (satır 6) ve V4 komutu,
serbest bırakılmış bir işi hâlâ reddeder; satır 6 zaten ürün tarafından
reddedilir.

Denetleyicide, yönetilen bir PowerDNS kaynağıyla
`bind__{intent,target-staged}__after-write__standalone__peer-reachable`
için açık bir `--owner-inverse-after-restart` akışı vardır: kill, Agent
yeniden başlatılır ve çalışır durumda bırakılır, geri alma kararı ve
serbest bırakma gözlemlenir, salt-okur durum komutu herhangi bir mutasyon
yapmadan komutu adlandırır, sahip komutu çalıştırılır, ledger değişmeden
günlük emekliye ayrılır, PowerDNS UDP/TCP üzerinde hâlâ yetkilidir, BIND
devre dışıdır, sahip dosyaları değişmemiştir, yeniden çalıştırma
bağımsızdır (idempotent), 31 sağlık örneği alınır.

**Adı belli açık yönlendirme eksiği (satır 7, 11, 13).** Tamamlandıktan
sonra bir sahip ters işlem komutunun yeniden çalıştırılması hiçbir şeyi
değiştirmez ve artık "zaten uzlaştırıldı" der, ama yine de mevcut terminal
yeniden çalıştırmasında olduğu gibi kullanılamaz durumu (3) ile çıkar. Bir
sahip veya betik bunu başarısızlık olarak okuyabilir. Bunu değiştirmek
mevcut bir komut sözleşmesini değiştirir ve ayrı bir karara bırakılmıştır.
Okuma anındaki metinler, günlüğü sahip komutunun mu yoksa daha sonraki bir
Agent başlangıcının mı emekliye ayırdığını söyleyemez, çünkü bunu tutan
kalıcı bir kayıt yoktur; sürümlenmiş bir sahip-kurtarma makbuzu bunu
eklemenin yolu olurdu.

**Adı belli açık düzenek eksiği (satır 7, 2. madde).** Tek sunucu,
yönetilen-PowerDNS kritik hücreleri (`bind` source-stopped, target-started,
rolled-back) denetleyicide hâlâ bir V1 günlüğü bekliyor, oysa üretici o
kaynak için V2 yazıyor. Bugün çalıştırılsalar sınır işaretinde reddedilirler
ve kendi geçiş tanımlarına ihtiyaç duyarlar (Agent, V2 ters işlemini
çalıştıramaz, bu yüzden `rpc-retry` onların kurtarması değildir). 25 Eylül
2026 tarihli altı rapor tarihsel V1 kanıtı olarak kalır.

Satır 2, 6 (açma), 8, 11 (ek kesintiler), 12, 14 ve 17 açık kalır ve 2. maddeye
veya sonrasına taşınır. Üretici değişikliği olmadan geçen bir hücreyi
tekrarlamak bu kayda hiçbir şey katmaz.
