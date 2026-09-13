# Setup guidance preference / Kurulum sihirbazı tercihi

September 11, 2026. Source change following the operator's Alpha65 feedback;
Included in the Alpha66 release source. Installed panels remain user-updated.

## User-visible behavior

- Fresh or unassessed legacy administrators see a brief guided/manual choice.
  An upgraded host is not inferred to be empty, and nothing is installed on view.
- Manual mode persists on the server, returns to Dashboard and hides its setup
  invitation. Settings → Server setup always provides a return path.
- Already started setup resumes; completed setup is not reset. Manual mode is
  available before execution; active or waiting operations cannot be hidden.
- Choosing manual does not mark setup complete, remove security/service checks,
  unlock an invalid license, or change existing DNS, certificates or workloads.

## Contract

The administrator-only `PUT /api/v1/setup/guidance` stores `guided` or `manual`
under `panel_settings.server_setup_guidance`. Absence means `undecided` for
`new`/`legacy`, and `guided` for existing draft/progress/completed states. No schema
migration or mutation of the setup status/draft/completion timestamp is needed.

The preference write and revision increment share a transaction. Revision CAS
rejects stale choices and invalidates old reviewed plans. Active setup or service
operations reject changes. Invalid persisted values fail assessment rather than
silently choosing manual. Readiness and the existing `required` field remain
independent; routing uses guidance plus active-operation state.

## Validation

- Go setup tests cover persisted manual/guided selection, stale revision
  rejection, unchanged drafts/status, non-admin rejection and readiness checks.
- Frontend tests cover initial legacy choice, manual and completed navigation,
  duplicate clicks, failed preference writes and re-entry from manual mode.
- TR/EN desktop (1440 px) and mobile (390 px) browser fixtures exercise choice,
  manual Dashboard after reload, Settings re-entry and the four-purpose wizard.
  These use mocked APIs, not production host operations. The fixture deliberately
  returns 503 for server statistics; no JavaScript errors or horizontal overflow.
- Recorded results: [validation artifacts](validation/server-setup-guidance-20260911/README.md). No installed panel was updated by the assistant.

Türkçe: İlk karşılaşmada sihirbaz veya manuel yapılandırma açıkça seçilir.
Tercih sunucuda hatırlanır; manuel seçim kurulumu tamamlanmış saymaz ve güvenlik
gereksinimlerini kaldırmaz. Sihirbaza Ayarlar → Sunucu kurulumu ile dönülür.

## Shared setup shell — September 13, 2026 (Alpha74 release source)

Setup now uses the panel's shared Layout in a focused mode. The navy sidebar
shows setup steps instead of management destinations. Hostname, IPv4 and the
server-reported build remain in its footer; narrow screens retain identity in
the header and the build in a persistent footer. Initial choice, loading,
assessment failure and completed setup use the same shell. DNS, panel HTTPS,
updates and password recovery remain available; the generic Components shortcut
is removed from setup. The focused shell does not fetch navigation census data.
Drafts, operation admission, licensing and installed-panel updates are unchanged.

Validation: 424 frontend tests, production build and bundle budgets pass. Twelve
local browser scenes cover choice, purpose, access, review and progress at
390/768/1024/1440 widths, including Turkish and English. These are fixture-based
UI checks, not evidence of a live server installation or recovery.

Kurulum, panelin ortak çerçevesini kullanır. Mavi yan alan genel menüler yerine
kurulum adımlarını gösterir; sunucu adı, IPv4 ve sunucudan okunan sürüm alt bölümde
kalır. Dar ekranda kimlik üstte, sürüm sabit alt bölümde görünür. Başlangıç tercihi,
yükleme, durum okuma hatası ve tamamlanma ekranları da aynı çerçeveyi kullanır.
DNS, panel HTTPS, güncelleme ve parola kurtarma erişimleri korunur; genel
Bileşenler kısayolu sihirbazdan kaldırılır. Taslak, işlem kabulü, lisans ve
kullanıcının panelden güncelleme kuralları değişmez. Bu değişiklik Alpha74 yayın kaynağına dahildir; 424 ön yüz testi, üretim derlemesi ve 12 yerel tarayıcı görünümü
ile doğrulanmıştır. Yerel örnekler canlı sunucu kurulumu veya kurtarma kanıtı değildir.
