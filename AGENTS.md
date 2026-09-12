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
