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
| 2 | Boş BIND, çiftin birincili | V1 | Agent, aynı istek | 1 ile aynı | [Pair target-staged/after-write + management-disabled reboot](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md); [V3 deletion terminal](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) | Bu iki başlangıç öncesi hücre için **GEÇTİ**. **EKSİK**: source-stopped, target-started, target-verified kesintileri |
| 3 | Boş BIND, çiftin ikincili | V1 | Agent, aynı istek | 1 ile aynı | yok — şimdiye kadarki denemelerde her ikincil panelsizdi | **EKSİK**: kesinti denemesi yok |
| 4 | Boş PowerDNS, tek sunucu (yalnızca APT sunucularında) | V1 | Agent `rollbackPDNSSwitch`, başlangıçtan önce ve sonra. Sahip CLI'ı yok (D-026 karar 2). | `verifyOwnerAwarePreimage` | [Fresh-install cells 2026-09-29](../deploy/e2e/dns-kill-matrix/evidence/fresh-install-20260929/README.md): `pdns-switch__target-started__after-write` geçti (başlangıçta geri alma, ardından aynı istek yeniden denemede ileri yönde tekrar çalıştı; ~3 sn DNS boşluğu). `pdns-switch__target-staged__after-write` **başarısız**: PowerDNS hiç başlamamıştı ve birimi kurulumun kendi kalıcı maskesiydi, ama V1 geri almanın durmuş hedef kanıtı `LoadState=loaded` gerektiriyordu; kurtarma `dns_native_recovery_unknown_after_restart` ile sonuçlandı, yeniden deneme reddedildi, DNS sunulmadı. Önceki durum (DNS yok) zarar görmedi. | Başlangıç sonrası kesinti için **GEÇTİ**. **EKSİK (doğrulanmış kusur)**: başlangıç öncesi kesinti — boş kaynak günlükleri için durmuş hedef kanıtı üzerinde düzeltme sürüyor; hücre düzeltilmiş kaynak üzerinde yeniden çalıştırılmalıdır. |
| 5 | Boş veya yeniden yapılandırılmış PowerDNS, çiftin ikincili | V1 | Agent, aynı istek | 4 ile aynı | yok | **EKSİK**: kesinti denemesi yok |
| 6 | Boş çift PowerDNS birincili, V3 (boş kaynak) | V3 (yalnızca testler) | Başlangıçtan önce: sahip CLI'ı `recover-dns-pdns-fresh-prestart`. Başlangıçtan sonra: Agent yalnızca ileri yönde; başlangıç sonrası ters işlem yok. | SQL/daemon sapma denetimi reddeder | [V3 native after-start forward + SIGKILL](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-native-20260928/README.md), [V3 prestart inverse](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md), [V3 zone lifecycle](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md). Genel RPC üzerinden erişilemez. | **DESTEKLENMİYOR, reddediliyor** (`pdns_primary_switch_paused`). Bunu açmak 2. maddeye aittir ve bir sahip-düzenleme kesintisi, bir başlangıç sonrası ters işlem ya da açık bir yalnızca-ileri politikası ile genel RPC üzerinden kabul gerektirir. |
| 7 | PowerDNS → BIND, tek sunucu, PowerDNS etkin / BIND devre dışı | **V2** | Agent `rolling-back` kararını yazar ve sonra reddeder; sahip CLI'ı `recover-dns-bind-switch`, rolling-back/rolled-back durumundan ters işlemi çalıştırır. Başlangıçtan sonra: hedef doğrulandıktan sonra yalnızca ileri yönde. | Ana yapılandırma düzenlemesi reddedilir, kanıt saklanır | [Protected owner CLI](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md): hedef başladıktan sonra rolling-back/after-write, sahip düzenlemesi reddedildi, CLI kesintiye uğradı, yeniden başlatma | Karar verilmiş geri alma hücresi için **GEÇTİ**. **EKSİK**: V2 üreticisi altında intent, target-staged, source-stopped ve target-started kesintileri. 25 Eylül 2026 tarihli altı Agent-aracılı BIND raporu V1 kullandı ve bu satır için tarihseldir. |
| 8 | PowerDNS → BIND, çift | V1 | Agent, aynı istek | sahibi gözeten | çift için yok | **EKSİK** |
| 9 | **BIND → PowerDNS**, tek sunucu ve çiftin ikincili (`switch` kartı, veya PowerDNS yokken `install`) | — | — | — | yok | D-026 karar 1 ile **DESTEKLENMİYOR, reddediliyor** (`bind_source_pdns_switch_unsupported`). D-026'dan önce bu satır *desteklenmiyor, reddedilmiyor* idi: V1 günlüğü, yalnızca Agent'ın ters işlemi, çağıranı olmayan V4 üreticisi. |
| 10 | BIND → PowerDNS, çiftin birincili | — | — | — | — | **DESTEKLENMİYOR, reddediliyor** (`pdns_primary_switch_paused`) |
| 11 | Çalışan BIND devralması (`adopt_unmanaged`, tek sunucu, Debian; Arch reddedilir) | V2 `SourceBIND` | Agent reddeder ve CLI'ı adlandırır; sahip CLI'ı `recover-dns-bind-adoption`, rolling-back/rolled-back durumundan | Aynı seri numaralı bölge düzenlemesi reddedilir | [Adoption owner CLI](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-ADOPTION-OWNER-CLI-20260927.md): tek hücre, yeniden başlatma yok, denetleyici devri kasıtlı olarak doğrulanmadı | Tek hücre için **GEÇTİ**. **EKSİK**: erken ve başlangıç sonrası kesintiler, yeniden başlatma |
| 12 | Durdurulmuş, yönetilmeyen BIND devralması | V1 (boş kurulum işlemi) | 1 ile aynı | 1 ile aynı | yok | **EKSİK** (1 ile aynı sınıf) |
| 13 | Harici PowerDNS devralması (`adopt`, tek sunucu, APT) | V1 | Agent ters işlemi, artı rolling-back/rolled-back durumundan sahip CLI'ı `recover-dns-pdns-adoption` | Statik yapılandırma düzenlemesi reddedilir | [Adoption cells](../deploy/e2e/dns-kill-matrix/README.md) (`NATIVE-PDNS-ADOPTION-*`), [owner edit](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-OWNER-EDIT-20260925.md), [protected owner CLI + reboot](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-PROTECTED-OWNER-CLI-20260926.md), [management-absent boot](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-MANAGEMENT-ABSENT-BOOT-20260925.md) | **GEÇTİ** (Debian 13, imzasız yerel derleme) |
| 14 | `reinstall_active` BIND, tek sunucu | V1 | Agent, aynı istek | 1 ile aynı | yok | **EKSİK** |
| 15 | `reinstall_active` PowerDNS | — | — | — | — | **DESTEKLENMİYOR, reddediliyor** (Panel ve Agent) |
| 16 | Genel motor kurulum/durdurma/kaldırma RPC'si | — | — | — | — | **DESTEKLENMİYOR, reddediliyor** (`genericDNSEngineMutationRefusal`) |
| 17 | Çift üzerinde bölge ekleme/düzenleme/silme (switch günlüğü değil, zone-sync V3 ledger'ı) | zone-sync v3 | `RecoverDNSZoneV3` üzerinden aynı istek, Agent aracılığıyla. Ebeveynsiz silme, sahip kaydı yokluğu kanıtlayana kadar bekleyen durumda kalır. | Türlenmiş sahip-düzenleme çakışması | [V3 deletion terminal](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md), [owner edit](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-OWNER-EDIT-20260927.md) ve [düzeltmesi](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-OWNER-EDIT-CORRECTION-20260927.md), [V3 zone lifecycle](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md), [owner-proof deletion](../deploy/e2e/dns-kill-matrix/evidence/pdns-bind-owner-proof-20260928/README.md) | **EKSİK**: bir zone-sync işlemi içinde gerçek sistem SIGKILL'i yok (şimdiye kadarki her kesinti motor switch'i üzerindeydi). 2. maddeye aittir. |

Henüz denetlenmedi: `ConfigureDNSClusterV2` eşleme değişiklikleri.

## Devir maddesi 1'i ne kapatır?

Devrin 1. maddesinin kapanma koşulu şudur: desteklenen kesintilerde aynı işlem
devam eder veya geri alınır; sahip değişiklikleri korunur; başlangıç öncesi ve
başlangıç sonrası davranış açıktır. D-026 ile birlikte, bunu hâlâ engelleyen
satırlar kod değil kanıttır:

1. Satır 4 — boş tek sunucu PowerDNS: başlangıç sonrası hücre 29 Eylül 2026'da
   geçti; başlangıç öncesi hücre doğrulanmış bir kusuru ortaya çıkardı
   (durmuş hedef kanıtı, boş kaynak günlüğü için kurulumun kendi maskesini /
   henüz kurulmamış bir birimi reddediyor). Düzeltme, bileşen testleri ve
   düzeltilmiş kaynak üzerinde `target-staged` ve `intent` hücrelerinin
   yeniden çalıştırılması gerekiyor.
2. Satır 1 — boş tek sunucu BIND: başlangıç sonrası hücreler 29 Eylül 2026'da
   Debian ve Arch'ta geçti (tamamlandı). Düzenek artık boş bir kaynakla
   `bind__target-verified__{before,after}-write__standalone__*` hazırlıyor;
   before-write ucu, günlük hâlâ `target-started` durumundayken BIND
   başladıktan sonra kesiyor. Boş BIND'in `target-started`, `source-stopped`
   ve `rolled-back` durumlarındaki hâli tasarım gereği kapalı kalır: bu
   hücreler yönetilen-PowerDNS-kaynak hücreleri olarak tanımlıdır ve boş bir
   çalıştırma oradaki bir geçişin anlamını değiştirir.
3. Satır 7 — V2 üreticisi altında PowerDNS → BIND: hedef hiç başlamadan
   Agent-karar-verir / sahip-yürütür ayrımının gösterilmesi için bir erken
   kesinti (intent veya target-staged). Düzenek bugün yönetilen bir PowerDNS
   kaynağını yalnızca source-stopped, target-started ve rolled-back
   durumlarında kabul ediyor.
4. Satır 3 ve 5 — boş çiftin ikincili, BIND ve PowerDNS — hazırlanamaz:
   hiçbir eş betik, ürün kataloğunu ve üye bölgelerini AXFR/NOTIFY ile
   konuğa sunan panelsiz gerçek bir *birincili* canlandırmıyor; ayrıca
   tetikleyici, eski yeniden yapılandırma manifestiyle aynı şekle sahip
   olduğu için boş PowerDNS çiftin-ikincili manifestini reddediyor. Bu,
   2. maddenin iki topolojili kabul işidir; satırlar orada açık kalır ve
   N/A olarak yeniden sınıflandırılmaz.

İşlem yönlendirmesi (D-024), satırları aşan bir kod eksiğidir: Panel ve web
hiçbir zaman bir sahip komutu adlandırmaz, `dns-switch-status` yalnızca
`recover-dns-bind-adoption` ve `recover-dns-pdns-target-staged` komutlarını
adlandırır; satır 6, 7 ve 13, sahibi sırasıyla `recover-dns-pdns-fresh-prestart`,
`recover-dns-bind-switch` ve `recover-dns-pdns-adoption` komutlarına
yönlendirmelidir.

Satır 2, 6 (açma), 8, 11 (ek kesintiler), 12, 14 ve 17 açık kalır ve 2. maddeye
veya sonrasına taşınır. Üretici değişikliği olmadan geçen bir hücreyi
tekrarlamak bu kayda hiçbir şey katmaz.
