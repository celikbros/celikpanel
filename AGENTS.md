# Project instructions

## Owner-controlled infrastructure independent of the panel - 2026-09-12

CelikPanel acts as the server owner's operator, not the owner of the services.
The owner must be able to configure and operate infrastructure without CelikPanel.
Panel outage, license loss, or removal of the panel and its management agent must
not stop or remove hosted workloads, DNS, mail, databases, scheduled jobs or
certificate renewal. Use native service configuration, standard protocols and
independent service lifecycles. Optional panel automation is separate from service
operation. Detect owner configuration changes rather than silently overwrite them.

Do not require a remote CelikPanel or its HTTPS API merely to configure standard
primary/secondary DNS transfer. Distinguish replication from optional authorized
remote record-management automation. Audit and resolve existing dependencies
before claiming safe panel removal or independent operation is implemented.

This product requirement does not authorize assistant-side live configuration,
panel removal, or bypassing user-only installed-panel updates. See D-022 in
docs/DECISIONS.md.

## Installed panel updates — user instruction, 2026-09-10

Never update an installed CelikPanel instance on the user's behalf. This applies
to Boston, Frankfurt, and all other installed panels. The user must initiate
the update themselves from CelikPanel's own update interface.

Do not start updates through SSH, scripts, systemd units, APIs, browser
automation, or any other indirect path. Publishing a release and installing it
on an existing server are separate actions. A request to continue, prepare, or
publish a release does not authorize updating installed panels.

You may prepare code, test it, and publish releases when authorized. After the
user updates a panel, you may verify the result read-only. Any older deployment
permission in runbooks or session notes does not override this instruction.

Kurulu CelikPanel örneklerini kullanıcı adına kesinlikle güncelleme. Boston,
Frankfurt ve diğer kurulu panellerde güncellemeyi kullanıcı, panelin kendi
güncelleme arayüzünden bizzat başlatır. SSH, script, systemd, API veya tarayıcı
otomasyonu ile güncelleme başlatma. “Devam” ya da sürüm yayımlama izni, kurulu
panelleri güncelleme izni değildir. Kullanıcının yaptığı güncellemeyi salt-okur
kontrollerle doğrulayabilirsin. Eski dağıtım talimatları bu kuralı geçersiz kılamaz.

## Approved server setup direction

Before changing activation routing, first-run setup, purpose profiles, or DNS
prerequisites, read docs/SERVER-SETUP-PLAN.md and D-021 in docs/DECISIONS.md.
The user approved that direction on 2026-09-10. It supersedes D-009's blanket
local-DNS requirement as a product decision; the explicit modes and backend
safeguards must be implemented before changing current enforcement. The plan is
not a statement of shipped capability. Preserve existing installations and the
user-only panel update rule above.

## Remote DNS source implementation authorization

On 2026-09-11 the user explicitly approved the separately requested remote DNS
connector implementation and local tests: see docs/REMOTE-DNS-AUTHORIZATION.md.
Earlier automatic-review permission rejections are resolved by this user reply.
Continue that scoped work without asking for the same permission again. The
installed-panel update restriction above remains binding.

## Actionable guidance for every operation — user requirement, 2026-09-13

Every program, service, runtime and integration must show actionable progress,
waiting, failure and recovery guidance. Put the current reason, who must act,
the concrete next action and how work resumes before long step lists. Distinguish
verified failure, unmet prerequisite and unknown result; preserve known failures
while reconciling the exact operation. Include supported license, credential,
permission and external-provider states. Never expose secrets or start duplicate
mutations from polling. Read D-024 and docs/OPERATION-GUIDANCE.md before adding or
changing lifecycle flows. This is a product-wide requirement, not a claim that
all adapters are implemented or audited. Owner independence and user-only panel
updates remain unchanged.

Her program, hizmet, çalışma ortamı ve entegrasyon; ilerleme, bekleme, hata ve
kurtarmada uygulanabilir yönlendirme göstermelidir. Mevcut neden, kimin işlem
yapacağı, somut sonraki eylem ve nasıl devam edileceği uzun listelerden önce gelir.
Doğrulanmış hata, eksik önkoşul ve bilinmeyen sonuç ayrılır; tam işlem sonucu
uzlaştırılırken bilinen hatalar korunur. Desteklenen lisans, kimlik bilgisi, yetki
ve dış sağlayıcı durumları kapsanır. Gizli bilgi gösterilmez; durum sorgusu ikinci
bir değişiklik işlemi başlatamaz. Yaşam döngüsü akışlarını değiştirmeden önce
D-024 ve docs/OPERATION-GUIDANCE.tr.md okunur. Bu ürün geneli gereksinimidir;
bütün adaptörlerin uygulandığı veya incelendiği iddiası değildir. Sunucu sahibinin
bağımsızlığı ve paneli yalnız kullanıcının güncellemesi kuralları değişmez.

## User-operated recovery first — 2026-09-13

For problems on installed servers, use the same recovery path available to an
ordinary server owner. Prefer the user operating CelikPanel's own interface.
If the interface cannot resolve the issue, give the user short, understandable
terminal commands after establishing what they do and why they are appropriate.
Only when that route would be long or complex may assistant-side direct recovery
be considered as the last resort; explain the reason and concrete scope first.
Do not silently repair live state through SSH, scripts, APIs or database edits.
Read-only code investigation and preparation remain allowed. This recovery
preference does not authorize assistant-side installed-panel updates: the user
must still initiate every panel update from the panel's own update interface.

Kurulu sunuculardaki sorunlarda önce kullanıcının panelden uygulayabileceği yolu
seç. Panel yeterli değilse kullanıcıya kısa ve anlaşılır terminal komutları ver;
komutun etkisini ve neden uygun olduğunu önce doğrula. Bu yol uzun veya karmaşık
olacaksa doğrudan müdahaleyi son seçenek olarak değerlendir; gerekçeyi ve somut
kapsamı önce açıkla. Canlı durumu kullanıcıdan gizli biçimde SSH, script, API veya
veritabanı düzenlemesiyle düzeltme. Kodun salt-okur incelenmesi ve hazırlık
serbesttir. Panel güncellemesini her durumda kullanıcı panelin kendi güncelleme
arayüzünden başlatır; bu kurtarma tercihi o kuralı değiştirmez.

## Constitutional resilience audit — user direction, 2026-09-14

Before changing lifecycle, persisted evidence, access gates, recovery or native
service ownership, read the constitution in ROADMAP.md, D-025 and
docs/RESILIENCE-CONTRACT.md. Name the affected invariant/P0 item, schema or
version transition, recovery behavior and evidence in the PR. Keep unresolved
acceptance items open; component tests alone do not establish complete native
update/automatic-rollback resilience. Incident corrections must record their
scope and remaining gaps rather than claim the foundation finished.

The owner asked for structural correction rather than continuing isolated
patches. This direction does not authorize live configuration, panel removal,
license-policy changes or assistant-initiated installed-panel updates. Preserve
the owner-operated recovery and installed-update rules above.

Yaşam döngüsü, kalıcı kanıt, erişim kapıları, kurtarma veya yerel hizmet sahipliği
değiştirilmeden önce ROADMAP.tr.md anayasası, D-025 ve
docs/RESILIENCE-CONTRACT.tr.md okunur. PR'da etkilenen ilke/P0 işi, şema veya
sürüm geçişi, kurtarma davranışı ve kanıt belirtilir. Açık kabul işleri açık kalır;
bileşen testleri tam gerçek sistem güncelleme/otomatik geri alma dayanıklılığını
kanıtlamaz. Olay düzeltmesi kapsamını ve kalan açığı kaydeder; temeli bitmiş saymaz.
Bu yön canlı yapılandırma, panel kaldırma, lisans politikası değişikliği veya
asistanın kurulu paneli güncellemesi izni değildir. Yukarıdaki kullanıcı kurtarma
ve kurulu panel güncelleme kuralları korunur.
