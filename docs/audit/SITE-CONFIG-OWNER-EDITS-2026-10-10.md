# Owner edits to site configuration files: what the Panel does today, and what detection needs

Date: 2026-10-10. Branch `fix/alpha83-known-state-gates`, HEAD `a9c95d437` (published v0.1.0-alpha.82 plus documents).
Kind: read-only audit from source. Nothing was run, measured on a native host, or changed. Every statement below is
"read in code" unless it is marked **inferred** (follows from the code and from OS behaviour, not observed) or
**not established**.

Binding rules this audit answers to: D-022 (`docs/DECISIONS.md:369`; AGENTS.md "Owner-controlled infrastructure"),
D-025 and `docs/RESILIENCE-CONTRACT.md` (entry "Corrections from the final native round", the passage corrected on
2026-10-09, ~line 4021-4030, which records this question as open). The user-only installed-panel update rule is
unchanged and nothing here authorizes a live change.

## 1. Answer in one paragraph

Today the Panel does not look at an owner's edit at all. At every Panel start it renders the nginx vhost of every
hosted site from the database and writes it over whatever is on disk, in one batch. The only thing it reads from the
existing file is a copy held **in memory** so it can put the file back if `nginx -t` or the reload fails. It does not
compare the file with the new text, with a recorded digest or with a marker, it keeps no backup on disk, and it
rewrites (and reloads nginx for) files whose bytes did not change. An owner's added `location`, include, edited
directive, replacement file or re-created link is replaced without a message. Only two things stop the write: nginx
refusing the whole configuration (then the owner's file is put back and the whole batch is abandoned, logged only),
and a file the agent cannot rename over (for example `chattr +i`).

## 2. Start-up path, traced

1. `cmd/panel/main.go:1092`: `panel.reconcileCertificateRuntimeAtStartup()` runs on every start, after the service-mutation
   barrier and the startup mutation lease (`main.go:1071-1093`). If the lease cannot be taken the subsystem is marked
   degraded and the reconcile is skipped (`main.go:1078-1087`).
2. `cmd/panel/cert_startup_reconcile.go:85-167`: after the lineage clean-up and the pending-certificate preparation it calls
   `reconcileHostedVhostsAtStartup` (`:148`).
3. Selection (`:424-430`): `SELECT d.id FROM domains d JOIN sites s ... WHERE COALESCE(s.project_type,'php') <> 'dnsonly'`.
   Every hosted site, whatever its status, up to 4096 (`:20`; more than that: nothing is touched, `:450-455`). Nothing in the
   query or later asks whether the site was edited, suspended, or has a vhost on disk at all.
4. Render input (`cmd/panel/vhost_apply.go:59-164`, `buildVhostRequest`): row from `sites`/`domains`, the managed host names,
   the certificate paths, and one DNS-name inspection of the installed certificate (`:117-128`). No file is read.
   One failing site (for example an unreadable certificate, `:118-124`) fails the **whole** batch (`cert_startup_reconcile.go:466-473`).
5. RPC `Agent.ApplyVhosts` (`:478-486`; `cmd/agent/vhost_rpc.go:72-117`): validates and renders every item first
   (`renderValidatedVhostBatch`, `:142-176`; `renderValidatedVhost` -> `NginxGenerator.Render`,
   `internal/services/nginx_generator.go:82-121`, template `internal/services/templates/nginx/vhost.conf.tmpl`),
   prepares each ACME challenge root, then calls `NginxGenerator.ApplyVhosts`.
6. `nginx_generator.go:475-526`, under the global `nginxMutationMu`: snapshot every item (`snapshotVhost`, `:285-308`:
   `os.ReadFile(sites-available/<d>.conf)` and `Lstat(sites-enabled/<d>.conf)`), write every item (`writeVhostFile`,
   `:642-663`), one `nginx -t` (`:511`), one `systemctl reload nginx` (`:518`). The snapshot is used **only** by
   `rollbackVhostMutations` (`:423-467`); no code compares `snapshot.config` with `item.Config` and skips an identical file.
7. Result: success is one journal line, `restored N hosted vhosts with one nginx validation and reload` (`:163-166`). Failure is
   one journal line (`:156-160`) and the function returns; there is **no** `markSubsystemDegraded` for a vhost-batch failure
   (it is used only for the lease, `main.go:1078-1087`), so no Panel screen shows it. **Read in code; the screens were not opened.**

### How the file is written

| Aspect | Today | Source |
|---|---|---|
| Method | Temp file in the same directory, `Chmod(0644)`, write, `fsync`, `rename` over the target, `fsync` of the directory. Atomic for nginx. | `atomicWriteRegularFile`, `nginx_generator.go:665-701` |
| Mode/owner of the old file | **Not preserved.** Mode is the constant `0o644` (`:648`, `:680`); the owner is the agent's (root). Extended attributes, ACLs and SELinux labels of the old file are not carried. | same |
| Symlink at `sites-available/<d>.conf` | `os.ReadFile` follows it for the snapshot; the `rename` replaces the **link** with a regular file; the owner's target file is left alone. | `:287`, `:693` |
| Regular file at `sites-enabled/<d>.conf` (owner used a copy, not a link) | Replaced by a symlink to `sites-available` (rename over it). | `atomicReplaceSymlink`, `:703-729` |
| Backup on disk | **None.** The previous bytes live in memory for the length of the batch. After success the owner's version is gone. | `vhostSnapshot`, `:271-275` |
| Compare before write | **None.** Identical files are rewritten (new inode, new mtime) and nginx is reloaded even if nothing changed. | `:495-525` |
| Rollback | Restores the snapshot of the touched prefix, validates, reloads. | `:423-467` |

Contrast, same repository: the PHP pool and php.ini writers (`internal/services/config_transaction.go:69-135`) **do** preserve
mode and ownership, refuse a symlink or non-regular target (`:75-83`) and refuse to create over an existing file
(`createManagedConfigLocked`, `:234-241`).

## 3. Every other path that renders or removes a vhost

All go through the same `Agent.ApplyVhost` / `NginxGenerator.applyVhostWithRuntime` (`nginx_generator.go:564-595`):
snapshot for rollback, write, validate, reload. **None reads the existing file to decide anything.**

| Trigger | Entry | Reads existing file first? |
|---|---|---|
| Site creation (and import, which creates sites) | `cmd/agent/site_rpc.go:305-508`, `ops.applyVhost` at `:481`. Preflight checks the site's home and the system user (`:332-352`), **not** an existing vhost with that name; an existing file is overwritten and, if nginx refuses, restored. | No |
| Certificate issue/renew/activation/alias | `applyVhostForDomain...` from `cert_renewal.go:168,184,336`, `domain_ssl_handlers.go:537,722,1031,1151,1196`, `alias_certificates.go:292,304,332`, `ssl_runtime_status.go:226` | No |
| Settings save (WWW redirect, HTTPS, HSTS), alias/domain handlers | `domain_general_handlers.go:503,676,737,869,922` | No |
| Hosting-type change (php/static/node/proxy/forwarding) and its compensation | `hosting_transition.go:278,479`, `hosting_handlers.go:100` | No |
| PHP version switch | `domain_php_handlers.go:170-214` migrates the pool, then `applySiteVhost` | No |
| Delete site | `site_rpc.go:524-632`: `ops.removeVhost` = `RemoveVhost` (`nginx_generator.go:599-623`) deletes link and file whatever they contain, restoring from memory only if nginx refuses | No |
| Panel's own ACME vhost | `cmd/agent/panel_cert_rpc.go:405-418` | No (`setup_readiness_linux.go:94-97` compares bytes, but only to *report readiness*) |

No path renders to a side file, an include point, or a `.d` directory.

## 4. Inventory: what CelikPanel writes per site, and what marks it

| File / object | Written by | Marker | Recorded digest | Owner extension point | Documented owner mechanism |
|---|---|---|---|---|---|
| `/etc/nginx/sites-available/<domain>.conf` + `sites-enabled` link | `nginx_generator.go:642` | Line 2 of the template: `# Generated by CelikPanel — do not edit by hand / elle düzenlemeyin`. **No version, no digest.** It is a request to the owner, not evidence for the Panel. | None (no column in `sites`, no file) | None. Template has no `include <dir>/*.conf`; the only includes are nginx's `fastcgi.conf` and `fastcgi_params` | None. The header says the opposite |
| ACME-only block for `mail.<domain>` etc. | same vhost file | none | none | none | none |
| PHP-FPM pool `.../pool.d/site<ID>.conf` | `php_manager.go:21-35`, `:67-91` (create); `php_pool_manager.go:178-196` (update, migrate) | **None** (the template begins with `[siteN]`) | None | PHP-FPM's own pool directives; but see §5(f): the Panel's rewrite drops them | None |
| Site document root, placeholder page, system user | `site_rpc.go:399-475` | placeholder file only | none | n/a (tenant data) | n/a |
| Node app unit `/etc/systemd/system/celikapp-<ID>.service` | `app_rpc.go:111-176` | `# Managed by CelikPanel — do not edit by hand.` (`:147`) | None | systemd drop-ins (`celikapp-N.service.d/`) are honoured by systemd, but the Panel neither creates nor documents them; `os.WriteFile` (`:162`) is in place, not atomic, and rewrites the unit file only when the owner uses the Panel's app screen | None |
| Per-site cron | `cmd/agent/cron_rpc.go` via `crontab -u <tenant>` | none (it is the tenant's own crontab) | n/a | Whole crontab belongs to the owner; the Panel reads the pre-image and rewrites **one line** (`cron_rpc.go:113-114`, `:192`) | Native `crontab` |
| Per-site logrotate | **Nothing is written.** The vhost logs to `/var/log/nginx/<domain>-access.log` / `-error.log` and relies on the distribution's own nginx rotation. | n/a | n/a | n/a | n/a |
| Per-site socket directory | none: the socket is `PHPFPMSocketPath` in the package's own run dir | n/a | n/a | n/a | n/a |
| `nginx.conf` | `cmd/agent/nginx_ready.go:100-219`: only inserts the two missing `include` lines after `http {`, only when absent (`strings.Contains`), validates, rolls back byte-exact | comment `# Added by CelikPanel so the panel's drop-in configs are served.` | no | n/a | This is a "detect, add only what is missing" precedent |

## 5. Owner scenarios and what happens today

"Start" means the Panel's own start (`reconcileHostedVhostsAtStartup`). The same result holds for any later render of that
site (certificate, setting, hosting change, PHP switch) because they all take the §3 path.

| # | Owner action | Result today | Panel reports | Basis |
|---|---|---|---|---|
| a | Adds a `location` block to the vhost | Silently **overwritten** at the next start or next render; nginx keeps serving the block until the reload at the end of that same batch, then it is gone | Success line `restored N hosted vhosts ...`; nothing names the site | §2 steps 5-7; `writeVhostFile` |
| b | Edits a directive the template also sets (`client_max_body_size` placement, `listen`, `ssl_protocols`, HSTS, `root`) | Silently **overwritten** by the database-derived value | same | same |
| c | Replaces the file wholesale (own vhost with the same file name) | Silently **overwritten**; no backup, no trace of the owner's bytes | same | no compare, no backup |
| d | Adds `include /path/mine.conf;` inside the server block (or a symlink as the available file) | The include line is lost with the rewrite; the included file stays on disk, unreferenced. A symlink is replaced by a regular file (link only, target kept) | same | `:287`, `:693` |
| e | Removes the vhost (file, or only the enabled link) | **Recreated** at start: the file is written and the link recreated, so a site the owner removed or disabled serves again. If the owner moved the site to their own file in `conf.d/` with the same `server_name`, nginx sees two servers for the name; `nginx -t` normally only warns, so the Panel's copy may or may not win by include order (**not established**) | same | `snapshotVhost` for a missing file returns "does not exist" (`:304-307`); `writeVhostFile` writes it |
| f | Edits the PHP-FPM pool file | **Not touched at start** (pools are not rendered at start). Touched at the next pool-config save, at a PHP-version switch (new version's pool is written, old one deleted), and at site delete. An update re-renders only 13 keys read from the file (`renderPool`, `php_pool_manager.go:178-196`); other directives and comments (`php_admin_value`, `pm.process_idle_timeout`, `slowlog`, `env[...]`) are dropped silently. A pool with a second section, a non-matching `listen`, or a symlink makes the operation **fail** with an error (`GetPoolConfig` `:67-69`, `:119-121`; `readManagedConfig` `config_transaction.go:25-33`) | Error to the screen in the refuse case; nothing in the drop case | as cited |
| g | Makes the vhost file immutable (`chattr +i`) | `CreateTemp` in the directory succeeds, `rename` over the immutable file fails (EPERM, **inferred** from Linux semantics). `writeVhostFile` returns an error, the batch goes to `rollbackVhostMutations`; `restoreVhost` first calls `deleteVhostFiles` (`:310-327`, `:819-829`), which removes the **enabled link** and then fails on the immutable file, so that item cannot be restored. With any restore failure the batch returns "batch rollback incomplete" and **does not validate or reload** (`:442-448`; test `TestApplyVhostsRollbackRestoreFailureDoesNotReload`). Consequences, **inferred**: the whole startup batch fails (no other site gets its render), and the immutable file's enabled link is left removed, so the next reload of nginx by anything (logrotate, certbot) would drop that site | One journal line `restore hosted vhost batch: ... batch rollback incomplete`; pending certificate activations are rolled back to pending (`:149-160`); the Panel keeps running; no screen | `:475-526`, `:423-467`, `cert_startup_reconcile.go:148-161` |
| h | Owner's other nginx file breaks `nginx -t` (for context) | Batch validation fails, previous set restored and reloaded (`:511-516`, `:441-467`); same log-only report. One owner mistake therefore blocks every site's reconcile | journal only | same |

Two further facts that matter for design: (1) the header line "do not edit by hand" is the only statement to the owner, and it
contradicts D-022; (2) because everything is overwritten from the database, the **database**, not the file, is the
Panel's source of truth for a vhost today. Detection therefore has to say "the file is no longer what the Panel last wrote",
not "the file differs from the database".

## 6. Precedents in this repository for "detect, do not overwrite"

| Precedent | What it does | Where |
|---|---|---|
| Mail policy | Reads the native values, returns a **version** of exactly what it saw, a write must carry it back, writes only values that differ, never rebuilds `smtpd_recipient_restrictions`, keeps every element the owner added; locks the value and refuses with a reason when the right result is uncertain | `cmd/agent/mail_policy_rpc.go:19-45`, `mail_policy_restrictions.go:10-40` |
| Database/server config files | Pre-image + version (`cf1-<sha256>`, `cmd/agent/main.go:60-63`), timestamped backup next to the file, atomic replace with the **same owner and mode**, only if the file still is the one that was read (`configChanged()` refusal, `config_errors.go:92`), reload, put back only if the file still is the one this write installed | `cmd/agent/db_config.go:20-60`, `:270-290` |
| DNS engines | Ownership and generation receipts bind the files the Panel wrote (`internal/binddns/manifest.go:151-156`, `Managed by CelikPanel. DO NOT EDIT.` plus an immutable generation id); owner-aware proof and `dns_peer_owner_edit_unknown` states refuse to repair an owner-modified span (`cmd/agent/dns_engine_host.go:1125-1151`); PowerDNS main config gets one `include-dir` line only if absent (`dns_engine_pdns_config_owner.go:247-262`) | `docs/DNS-ENGINE-ARTIFACT.md`, `docs/OWNER-INDEPENDENCE.md` |
| Native cron | Present cron of any kind is "present, keep"; tenant crontab changed one line at a time from a read pre-image | `cmd/agent/native_cron.go:1-60`, `cron_rpc.go` |
| `nginx.conf` | Adds only the missing include | `nginx_ready.go` |

The vhost writer is the one place where a Panel-owned file in the owner's service tree is replaced without any of these.

## 7. Smallest design that meets D-022 (proposal only; nothing implemented)

Goal: after the change, a vhost (and, in a second step, a pool) that is not byte-for-byte what the Panel last wrote is never
overwritten without the owner's explicit choice, and the owner always has somewhere supported to put their additions.

1. **Evidence of what the Panel wrote.** On every successful write the agent records, per file, `{path, kind, site_id,
   render_version, sha256_of_written_bytes, written_at}`. Two homes, both written in the same transaction as the file:
   (a) a one-line header in the file itself, `# celikpanel-render v2 sha256=<64 hex of the body below>`, replacing the
   "do not edit" line; (b) a ledger row for the Panel's screens and decisions. The header is the authority for "is this still
   the Panel's text" because it survives a database restore; the ledger holds the owner's decision. If they disagree the file
   wins and the state is "owner-edited".
2. **Classify before every render** (start, certificate, setting, hosting, PHP switch), in the agent, under `nginxMutationMu`,
   reading with the no-follow/regular-file reader used for pools: `absent` / `managed-unchanged` (header digest equals the
   body digest) / `owner-edited` (header present, digest differs) / `foreign` (no header: replaced wholesale) / `unreadable`
   (symlink, immutable, permission) / `unknown-origin` (see §8). Only `absent` and `managed-unchanged` are written. Identical
   bytes are not rewritten and cause no reload.
3. **No silent overwrite.** For any other class the file is kept exactly as is (mode and owner untouched); the Panel marks the
   site "owner-edited configuration" with the reason and shows the unified diff between the owner's file and what the Panel
   would write now. The change the owner asked for is held, not lost: the pending render is stored as a side file
   `sites-available/<domain>.conf.celikpanel-pending` (never included by nginx) and as the ledger's `pending_sha256`.
4. **Explicit owner choices**, one confirmation each, never from polling or start: *Keep mine* (the Panel stops rendering this
   file; settings that need the vhost are refused with the reason until the owner chooses again; certificate issuance, which
   needs the ACME location, is told exactly which lines the owner's file must contain); *Take CelikPanel's* (timestamped backup
   next to the file as in `db_config.go`, then the managed text with a fresh header); *Merge manually* (the owner edits, the
   Panel re-classifies at the next render).
5. **Owner extension point.** The managed template gains, inside each `server` block, `include <dir>/<domain>.d/*.conf;`
   (a directory created by the Panel, empty, owner-writable by root). Owner additions placed there survive every render, and
   the Panel's diff names this as the supported route. `nginx -t` is already the arbiter before any reload.
6. **Atomic write keeps the file's mode and ownership** (reuse `atomicWriteManagedConfig`'s ownership handling) and refuses a
   symlink or non-regular target as the pool writer does.
7. **Reconcile never takes the whole host down for one site.** Per-site classification and per-site failure; `nginx -t` still
   covers the combined set, but a site that is `owner-edited`/`unreadable` is left out of the batch and listed, instead of
   failing the batch (scenarios g, h). Each such site produces a visible state and a recorded known failure per D-024.
8. **Honest report.** The start line counts `written`, `unchanged`, `kept (owner-edited)`, `foreign`, `unreadable`, `recreated`
   separately. A site the owner removed (scenario e) is classified `absent-but-recorded`: not recreated at start; the Panel
   shows "vhost missing, last written <date>" and offers recreate. (A brand-new site is `absent` with no ledger row: written.)
9. **Pool step** (second change, not required for the vhost): add the same header, refuse to rewrite a pool that is not
   managed-unchanged, and preserve unknown directives on update (or move Panel-controlled keys to the file and leave the rest
   as read). Node unit: same rule, with `celikapp-N.service.d/` as the extension point.

## 8. Schema, ledger and migration implications (D-025)

- **New persisted evidence:** a table, next migration `044` (alpha.82 is schema 43, `internal/db/migrations/043_request_identities.sql`):
  `managed_site_files(id, site_id, kind, path, render_version, written_sha256, pending_sha256, state, state_changed_at,
  owner_decision, decision_at, backup_path)`, with a unique `(site_id, kind, path)`. And a **file-format transition**: vhost
  files move from header `Generated by CelikPanel` (no version) to `celikpanel-render v2 sha256=...`. Both are named
  transitions; each needs its pre-update and post-update reader (an older Panel must ignore the new header and ledger rows;
  a newer one must read an old file as `unknown-origin`).
- **Update, rollback and restored database.** The file is self-describing, so after an automatic rollback to the old Panel
  and database nothing is lost: the old Panel does not know the header, will overwrite as today (this regression must be
  stated in the release notes of the first version that has the header: **the protection does not apply while an old Panel
  is running**), and a later new Panel re-classifies from the header. A restored database that lacks ledger rows must not
  produce `owner-edited` for header-valid files (the header decides, rows are re-created).
- **First state for files written by alpha.81 / alpha.82 (no digest).** They are indistinguishable by bytes from an owner-edited
  file, except by regenerating: the migration step renders, in memory and with **no write**, the vhost that each earlier template
  would produce from the current database row (alpha.81's template with the `snippets/fastcgi-php.conf` include, alpha.82's
  with it written out; the old templates must be embedded as frozen read-only strings). If the file equals one of those
  renders byte for byte, it is `managed-unchanged` and is upgraded (header added) at the next normal render. Otherwise its state
  is `unknown-origin`: **never overwritten**, shown as "this file has not been verified as written by CelikPanel", with the
  diff against the current render and the same choices. The honest statement for the first start after such an update is
  therefore "N files verified as unedited and upgraded, M left alone because they differ from every known CelikPanel text", not
  "all sites are fine". A site whose vhost depends on a certificate state that has since changed (renewed lineage path) may
  also differ from every historical render and land in `unknown-origin`; that is a false positive on the safe side and should be
  measured, not assumed away.
- **Cost of the safe default:** a site that must be re-rendered for a feature (certificate activation after issuance) is
  blocked while its file is `owner-edited`/`unknown-origin`. The Panel must say who acts and what to do (D-024), not fail with
  a bare error, and issuance must not claim success if the ACME location is not served.

## 9. What must be measured natively before this can be called done

Each cell on Debian 13, Ubuntu 24.04 and Arch (three platforms of set4), starting from a site created by alpha.81 and one by
alpha.82, with the owner action done through ordinary shell tools:

1. Baseline today (to write down the open fact): owner adds a `location`, restarts the Panel; file bytes, mtime/inode, nginx
   reload count, journal line, HTTP answer of the added location before/after. Same for scenarios b, c, d, e, g.
2. New version, each scenario a-e and g: file bytes unchanged, state shown, diff shown, no reload, other sites rendered.
3. Immutable file: the batch continues for other sites; the enabled link is not removed; the state is `unreadable` with the reason.
4. Update from alpha.82 to the candidate with (i) an unedited site, (ii) an owner-edited site, (iii) a replaced file: counts in
   the start line; the unedited ones upgraded; the others untouched. Then the same update **rolled back** automatically and
   forward again: file bytes identical to the owner's after each step; old Panel's overwrite behaviour recorded.
5. Restore of the database from a backup older than the header: header-valid files stay `managed-unchanged`.
6. Keep mine -> issue a certificate (ACME location missing) -> honest refusal; Keep mine with the ACME location present ->
   issuance succeeds; Take CelikPanel's -> backup file exists with the owner's bytes, mode and owner preserved.
7. Owner-extension directory: a `location` placed in `<domain>.d/` survives start, certificate renewal, settings save, hosting
   change and PHP switch, and `nginx -t` passes; deleting the site removes the Panel's include but leaves the owner's files and
   reports it.
8. Site removed by the owner (e): not recreated at start; "vhost missing" shown; recreate on request.
9. Panel removed or stopped (D-022 independence): the vhost, header and extension directory keep working with nginx alone; the
   owner edits the file by hand, starts the Panel, and the edit is detected rather than replaced.
10. Pool (second step): `php_admin_value`, `slowlog` and comments survive a pool save and a PHP-version switch; a pool the Panel
    did not write is refused with a reason.
11. Large host: 4096 sites, mixed classes, one batch: time, one `nginx -t`, one reload, no per-site loss.
12. Failure of one site's input (unreadable certificate) leaves the other sites rendered and names the one.

## 10. Limits of this reading

- Nothing was executed. Behaviour of `rename` onto an immutable file, nginx's treatment of duplicate `server_name` between
  `conf.d` and `sites-enabled`, and the include order on each platform are inferred, not observed.
- I did not open the Panel's screens; "no screen shows it" is derived from the absence of a degraded-subsystem call or any
  other reporting in the code path (`cert_startup_reconcile.go:156-161`), not from the UI. A read-only search of `web/src` for
  a vhost-state display was not done.
- I did not trace the update/rollback code to confirm what happens to the SQLite file and to vhost files on automatic
  rollback; §8's rollback statements describe what the design needs, not what is verified today.
- Site import (`cpmove`) was assumed to create sites through `CreateSite` (the only vhost writer found in the agent besides
  `ApplyVhost`); I did not read every import step.
- `Agent.ApplyVhost` single-site path was read; whether any RPC caller passes pre-rendered text from the Panel was not exhaustively
  checked, though the Panel's `buildVhostRequest` is the only builder found.
- The Apache/other web-server seat, webmail and database-tools vhosts (`dbtools_rpc.go`, `webmail_rpc.go`) are separate
  Panel-written files in the same trees and were inventoried only by marker, not traced for owner-edit handling.
- The count of hosted vhosts the Panel rewrites in practice, and whether `nginx` reload per start affects connections, were
  not measured.
- Whether the Turkish twin is needed, and wording of owner-facing text, are left to the owner of the next step.
