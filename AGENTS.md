# Project instructions

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
