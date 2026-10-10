# set10, 2026-10-10 (UTC): D-031 as implemented in commit cd46ca595 (a site's nginx vhost the owner changed is kept and named), measured on disposable Debian 13, Ubuntu 24.04 and Arch guests, installed fresh and reached by the owner's update from the published v0.1.0-alpha.81

A measurement record. It measures the cells of `docs/audit/SITE-CONFIG-OWNER-EDITS-2026-10-10.md` §9 that D-031
(`docs/DECISIONS.md`) names, against the owner-facing states, codes and sentences of the D-031 entries of
`docs/OPERATION-GUIDANCE.md` and `docs/RESILIENCE-CONTRACT.md` (2026-10-10). Nothing of the product was changed; nothing
was committed or pushed. The harness addressed no installed server; the guests' names for celikpanel.net and the
certificate authorities were pinned to loopback (see "Network"). The guests had outbound NAT that was not captured, so
contact by anything else in a guest (package managers, the licence service) is not measured, and this record is not a
statement of network isolation. Every `result.json` carries `native_evidence: false`. It closes no P0 row.

## Result in one paragraph

On all three platforms every owner edit of a site's vhost (added location, changed template directive, replaced file,
own include) was kept byte for byte (same SHA-256, same inode) at a Panel start, a restart and a General save; the
Panel's text was held in `<file>.celikpanel-pending`; the journal named the site and "kept (owner-edited)" or "kept
(foreign)"; `GET /site-config` answered the state with a diff and the three choices; the Domains list row carried the
state; the save answered 409 `SITE_CONFIG_OWNER_EDITED`; nginx was not reloaded; the owner's content stayed served.
Keep, take (with its dated backup, mode/owner kept, replay without a second run), the stale-digest refusal, the nginx
refusal with the owner's file put back, missing-not-recreated with recreate, the symlink refusal, deletion with a
kept copy, the unchanged start (no write, no reload) and creation = start all held. The update from the published
alpha.81 applied migration 044, adopted the untouched files and left the owner-edited one alone ("4 adopted, 1 left
alone" Debian/Ubuntu, "3 adopted, 1 left alone" Arch); the three new routes answered 428 without a request identity;
a database put back from before the migration was migrated again and the start re-classified without writing; the
automatic return to alpha.81 brought the ledger back to 42 and alpha.81's start rewrote every vhost (an owner edit
included). **Two expectations were measured and did not hold, on every platform:** the per-site include directory is
created `root:celikpanel 0750`, not 0755 (S0); and for a locked (`chattr +i`) site, `GET /site-config` answers
`managed_unchanged` with no reason and no action while the ledger and the Domains list say `unreadable`/`write_refused`
(g). A PHP version switch, a certificate site, a forward update after a return and 4096 sites were not measured.

## Cell x platform (verdict against the D-031 text; raw = that cell's `section.json`, whose `cells.<name>.expectations` hold every expectation with what was measured)

| Cell | Debian 13 | Ubuntu 24.04 | Arch | Raw (`<platform>/` + path; the same path on each platform) |
| --- | --- | --- | --- | --- |
| S0 header line + body digest, ledger row, include dir exists/empty/**0755**, nginx -T include | FAIL (0750) | FAIL (0750) | FAIL (0750) | `run-c\|run-a/driver/steps/09-s0-sites/section.json` |
| h unchanged start: no write (inode, mtime), no reload, "0 written, 3 unchanged" | PASS | PASS | PASS | `…/steps/10-h-i-unchanged-start/section.json` |
| i creation = start (the start writes nothing over the creation file; `server_name X www.X;`) | PASS | PASS | PASS | same |
| a1/a2/a3 added location: start / restart / save | PASS ×3 | PASS ×3 | PASS ×3 | `…/steps/11-a-owner-edit/section.json` |
| b1/b2/b3 changed `index` | PASS ×3 | PASS ×3 | PASS ×3 | `…/steps/12-b-owner-edit/section.json` |
| c1/c2/c3 replaced file (state `foreign`, D-031's class for a headerless file the ledger knows) | PASS ×3 | PASS ×3 | PASS ×3 | `…/steps/13-c-owner-edit/section.json` |
| d1/d2/d3 own include | PASS ×3 | PASS ×3 | PASS ×3 | `…/steps/14-d-owner-edit/section.json` |
| keep (428 without identity; 200; keep_mine; bytes and header untouched; replay; later start keeps) | PASS | PASS | PASS | `…/steps/15-keep/section.json` |
| j owner `root:root 0640` kept at start | PASS | PASS | PASS | `…/steps/16-take-j/section.json` |
| take (backup bytes/mode/owner; Panel text; mode/owner kept; reload; served; ledger; replay) | PASS | PASS | PASS | same |
| take with a stale digest → 409 `SITE_CONFIG_CHANGED`, nothing done | PASS | PASS | PASS | `…/steps/17-take-stale/section.json` |
| take refused by nginx (owner's `root` in the include dir) → 502 `SITE_CONFIG_NGINX_REFUSED`, owner's file back | PASS | PASS | PASS | `…/steps/18-take-refused/section.json` |
| e removed vhost + link: not recreated; ledger state and Domains row `missing`, site-config answer `absent` with the one choice `recreate`; save 409 `SITE_CONFIG_MISSING`; recreate serves | PASS | PASS | PASS | `…/steps/19-e-removed/section.json` |
| f owner `.conf` in include dir across a start that rewrites A and a save | NOT-MEASURED (4 of 5 expectations PASS; the PHP version switch is not measured) | same | same | `…/steps/20-f-include-dir/section.json` |
| g `chattr +i` A while A, B, static need a render | FAIL (site-config) | FAIL | FAIL | `…/steps/21-g-immutable/section.json` |
| g-resume: unlocked, next start writes A | PASS | PASS | PASS | same |
| k vhost replaced by a symlink: refused, not followed, link intact, `unreadable/symlink` | PASS | PASS | PASS | `…/steps/22-k-symlink/section.json` |
| l delete owner-edited (dated copy) and unchanged (removed); include dirs untouched (both left in place) | PASS | PASS | PASS | `…/steps/23-l-delete/section.json` |
| B-before: alpha.81 sites R1 creation text, R2 start text, R3 owner-edited; ledger 42; DB backup | PASS | PASS | PASS | `update-good/driver/steps/09-set10-sites-before-the-update/section.json` |
| B-forward: ledger 44, R1/R2 adopted, R3 `unknown_origin` never written, start line, Domains rows, 428 ×3 | PASS | PASS | PASS | `update-good/driver/steps/18-set10-site-files-after-the-update/section.json` (Arch: `17-…`) |
| B-db-restore: backup at 42 put back, start migrates to 44, no write, states re-recorded | PASS | PASS | PASS | `update-good/driver/steps/27-set10-database-restore-from-before-the-migration/section.json` (Arch: `23-…`) |
| B-return (defective candidate, automatic return): ledger 42, no header, alpha.81 start line | PASS | PASS | PASS | `update-defective/driver/steps/17-set10-site-files-after-the-update/section.json` |

Counts recomputed from `cells.md` at intake: 31 cells per platform x 3 = 93; PASS 84, FAIL 6 (S0 and g, on each
platform), NOT-MEASURED 3 (f, on each platform).

Generated per-cell lists: `cells.md` (`tools/table.py`). Every not-true check and not-passed step, classed product /
not-measured / harness / lab / aborted: `checks-not-passed.txt` (`tools/notpassed.py`).

## The two FAILs (what the product did instead)

1. **Include directory mode (S0, every platform).** `docs/RESILIENCE-CONTRACT.md` (line 4835): "the directory is
   created (0755) when the vhost is written"; `docs/OPERATION-GUIDANCE.md` (line 5950): "created 0755 when the vhost is
   written" (D-031 itself in `DECISIONS.md` does not name the mode).
   Measured right after creation, for A, B and set1's static site: `/etc/nginx/celikpanel-sites.d/<domain>`, empty,
   `root:celikpanel 0750` (gid 989 Debian, 988 Ubuntu, 969 Arch); the base directory `/etc/nginx/celikpanel-sites.d`
   is `root:celikpanel 0750` as well (`include_base`). In f, `nginx -t` returned 0 with the owner's file inside the 0750
   directory and `/owner-dir/` answered 200 `owner-dir` (`…/steps/20-f-include-dir/native/*-http-f-*`); that is the
   only probe showing nginx reads the directory (l shows the owner's files untouched, not served, and the nginx
   master's user was not recorded). Raw: `debian13/run-c/driver/steps/09-s0-sites/native/*-s0-after-creation-read.json`
   (`sites.<domain>.include_dir`).
2. **Locked file, site-config answer (g, every platform).** Start with A immutable and A, B, static all needing a
   render: A kept (bytes, inode) with its enabled link; B and static written; one `Reloaded` line; journal
   `site configuration set10-a.test (startup): kept, not replaced (managed_unchanged write_refused); rename … operation
   not permitted`; start line `2 written, … 1 unreadable or unwritable`; ledger `state=unreadable,
   state_reason=write_refused`; Domains row `{"state": "unreadable"}`. But `GET /api/v1/domains/2/site-config`
   answered 200 `{"state": "managed_unchanged", "reason": null, "actions": [], …}`: the screen's own read names
   neither the lock nor EPERM. Raw: `…/steps/21-g-immutable/section.json` (`calls`, label `g after the start: GET site-config`).

## NOT-MEASURED, with the reason

- PHP version switch (f): each guest has one PHP-FPM version (Debian 8.4.26, Ubuntu 8.3.6, Arch 8.5.11).
- A site with a certificate (B): the product offers no self-signed issuance (ACME or an owner's upload only); the CA
  names are pinned to loopback. Not attempted.
- "Forward again" after the automatic return, and what alpha.81 does with a header line: in each return cell the
  defective candidate fails at its offline migration, so its start never ran and no file carried a header at the
  return; the fixture origin serves one candidate per lab, so no second update was made. What the return leaves is
  measured (B-return); the forward from alpha.81-written files is B-forward.
- 4096-site batch (C): not required. The Panel's screens in a browser (only their API answers were read).

## Update cell: first state and the return, per platform

- Start line after the update, Debian 13 and Ubuntu 24.04: `4 written, 0 unchanged, …, 1 kept (unknown origin), …; 4
  adopted from an earlier release` and `4 adopted, 1 left alone because they differ from every known CelikPanel text`
  (R1, R2, the seeded static site, set4's item-9 PHP site; R3 left alone). Arch: `3 adopted, 1 left alone` (no item-9
  site on Arch). Raw: `<platform>/update-good/driver/steps/18-…/section.json` (Arch `17-…`; `start_lines`).
- Adopted files are **not** the old bytes plus a header: each was rewritten as cd46ca595's text (new comment block,
  the include line; on Debian and Ubuntu PHP sites `include snippets/fastcgi-php.conf;` expanded into its directives,
  which Arch's alpha.81 text does not have) — as `OPERATION-GUIDANCE.md` says ("rewritten as CelikPanel's text with
  its header"). Checked at intake from before/after bytes on all three platforms: the after body differs from the
  before file by 10–20 lines for every adopted file, and R3 is byte-identical to its before file with no header. Diff:
  `debian13/update-good/driver/steps/09-…/files/pre-before-the-update-set10-r2.test.conf.txt` against
  `18-…/files/after-the-update-set10-r2.test.conf.txt`.
- `adopted_from` names alpha.82 for files alpha.81 wrote: the seeded static site everywhere, and R1 `v0.1.0-alpha.82
  (creation)` / R2 `v0.1.0-alpha.82` on Arch (Debian/Ubuntu: `v0.1.0-alpha.81 (creation)` / `v0.1.0-alpha.81`). The
  ledger rows show the label; that the two frozen templates give the same bytes for those files (newest tried first)
  is the design reading, not shown by a raw file here (the templates are not in the record).
- Return (all three): ledger 42, no `managed_site_files` table, no header on any file (the candidate never started);
  alpha.81's start (`restored 4 hosted vhosts with one nginx validation and reload`) rewrote every vhost (new inode):
  R1's creation text became the start text (`www.` added), R3's owner edit was overwritten, R2 and the seed site same
  bytes. Raw: `<platform>/update-defective/driver/steps/17-…/section.json` (`cells.B-return.detail`).
- Database restore: the backup (taken online on alpha.81 just before the update) predates set4's item-9 site, so after
  it that site's vhost stays on disk with no Domains row (`restore-0` and `restore-1` reads) and the start line counts
  four sites only: `0 written, 3 unchanged, 1 kept
  (unknown origin)`, header files `managed_unchanged` with `adopted_from` empty.

## set8's five findings, re-checked

1. Creation text differed from the start text (`www.`): fixed, yes (i PASS ×3).
2. A save wrote no journal line: fixed, yes — `site configuration set10-a.test (change): kept (owner-edited); …` and
   `[409] apply general settings vhost: …` (`…/steps/11-a-owner-edit/native/*-a3-journal.json`).
3. The Panel wrote `root:celikpanel 0644` over an owner's `root:root` file: fixed for kept and taken files, yes (j,
   take: `root:root 0640` kept; backup `root:root 0640`); a new or recreated file is still `root:celikpanel 0644` (e).
4. `chattr +i` failed the whole start batch and lost the enabled link: fixed, yes (g: only A refused, link intact,
   others written, one reload) — except the site-config read above.
5. Every start rewrote unchanged files and reloaded nginx: fixed, yes (h PASS ×3).

Other observations: a refused take leaves its dated backup of the owner's file (take-refused, `backups_after`); the
ledger's release label in the fresh cells is the test build's `v0.1.0-alpha.81 0684f2703b66`.

## Method

Built from commit `cd46ca595` (tree `38ad77ab…`) in disposable clones, acceptance-licence builds (`build/`):
`run-upd1.sh build cd46ca595` → fresh baseline `0684f2703b66…` (cd46ca595 + `deploy/release-sequence-policy` labelled
alpha.81/81; archive `320c39e3…`), installed fresh in part A; `run-upd1.sh build --baseline-ref v0.1.0-alpha.81
cd46ca595` with `UPD1_REUSE_DIST_OF_COMMIT` → baseline = the published tag `a0beb7263…`, its set3 dist reused
read-only, SHA-256 `3350ff44dad2bb699ab5ee0a112b58b7bfb7fa47aa0ebdd3dd3c2da73d080109` (read before the build,
`host/hostcheck-before.txt`); good `e0c88113f717…` (tree identical to cd46ca595's, archive `ed2bd6f9…`); defective
`d688d8ff85d7…` (migrate-only defect, archive `049ae9a1…`). Both `prove` rc 0. Run copies = `git archive cd46ca595` +
the four set10 files (`harness-run-copy/`). Part A: `set10_trial.Set10Trial` over set8's driver (set6's steps: names
pinned first, preflight, origin, install, login, licence fixture, setup, set1's static site), then the sections in the
table. Part B: `set10_trial.Set10UpdateTrial` over set6's update cells `upd1-<platform>-good|defective` (set5's
defective cell with its second fault), with three added sections. Each section's owner actions, lab preparation
(`lab-restore`, `lab-stale-managed`) and API calls are recorded in its `section.json`.
During the run other commits landed on the branch (`host/commits-after-cd46ca595-at-staging.txt`); none entered a build or cell.

Runs: Debian `run-a` measured S0 only (S0's FAIL stopped the other sections through a driver dependency; fixed),
`run-b` was stopped after a harness error (evidence file names with spaces; no driver sums, `host/driver-not-finalized.txt`),
`run-c` is the complete Debian record; `update-defective-run-a` stopped at setup (`HOST_MUTATION_BUSY`: the guest's
package manager, a lab cause), `update-defective` is the complete one. Harness-class step failures (not the product):
set3's `post-update-facts` pins ledger 43 (now 44); set4's item 9 expects a save to rewrite an unchanged vhost (D-031
writes nothing); set6's header reading after a return reads alpha.81's header.

## Harness files (working tree, `deploy/e2e/release-recovery/`, not committed)

New: `set10_trial.py`, `guest_set10_native.py`, `run-set10.sh` (`SET10_NEIGHBOUR_GATE`, `SET10_DISK_GATE`),
`test_set10_trial.py` (12 offline tests). No existing file changed. Offline suite: copy a (pristine archive) 1020
tests, 5 errors; copies b and g 1031/1032 tests, the same 5 errors by name (`harness-run-copy/suites-compared.txt`).
The five are named in `harness-run-copy/a/suite-notok.txt`; one is `git rev-parse HEAD` exiting 128 in a copy without
`.git`, the causes of the other four were not examined at intake.
Copy g equals the working tree (`harness-run-copy/working-tree-against-copy-g.txt`).

## Network, guests, disk, host power

QEMU user networking with outbound NAT (`qemu-user-nat` in each `host/fixture-plan.json`); this is not network
isolation, and no traffic was captured. The five names (celikpanel.net and four certificate-authority hosts) were
pinned to loopback before any product path in all 12 labs and read back at the end of 11 of them, by `getent -s files
ahosts` and `getent -s files hosts` (the hosts file only, no DNS question; `*/driver/steps/01-*`,
`*-name-pinning-at-the-end`); `debian13/run-b` was stopped and has no read-back. Guests: Debian 13
(nginx 1.26.3), Ubuntu 24.04 (nginx 1.24.0), Arch (nginx 1.30.5). Neighbour gate (no qemu-system, no set9 job) clear at
the first poll before all 12 guest starts (`host/neighbour-gate.txt`; set9's last cell ended 18:45:34Z). Disk: Windows
`C:` by `Get-PSDrive C` every 30 s, 436 readings 18:47:00Z–22:25:12Z, 115.89 GiB at the start, lowest 89.38 GiB
(first at 19:35:11Z; the lowest byte count is 95969046528 at 19:36:41Z, also 89.38 GiB), 110.18 GiB at the end; 12 gate decisions, all allowed (`host/c-drive-cells.txt`). Each lab's overlays were
removed after staging (`host/removals.txt`). Power: keep-awake request 18:47:00Z–22:26:05Z; the System log shows modern
standby entered/left at 18:58:30/20:10:15, 20:39:44/20:58:20 and entered 21:19:54Z (`host/sleep-events.txt`) while the
watcher's readings stayed 30–31 s apart (largest gap 31 s, `host/watch-gaps.txt`): no gap is seen in the disk
watcher; guest clocks were not compared, so a pause that left the watcher unaffected is not excluded. The 21:19:54Z
entry has no matching leave entry in the log read at 22:25:40Z, and the watcher kept reading until 22:25:12Z.

## Secrets, leftovers

`secret-scan.txt` (set8's scanner, classes 1–11): no unexplained hit; 4 informational path matches are the scanner's
own pattern text. The scratch-folder prefix in job scripts and tools became `<scratchpad>` and the operator's machine
name in key comments `<operator-host>` (`host/scrubbed-files.txt`); file digests (`sha256=` headers, ledger digests)
are kept. Left on the WSL host: `/var/tmp/cp-set10-run`, `/var/tmp/cp-release-drill-s10-*` (records, no disks), the
builds `/var/tmp/cp-upd1-build/20261010t184746z` and `…t185436z` and their new dist directories under
`/var/tmp/cp-pair-accept/dist/`. Nothing of this run is running (not re-checked at intake). The root `SHA256SUMS` was
generated last and again after the intake corrections below; it lists the 11 per-run `driver/SHA256SUMS` files too.

## Corrections after intake (2026-10-11)

An intake check before the directory goes into the public repository. Scope: this directory only; no product code,
harness source or earlier evidence changed. Raw verdicts were not changed.

Checked and found as stated: the root and the 11 per-run sums (5333 files listed at root, 11 per-run sums unlisted
there, no failure, no absent entry, no CRLF);
secrets (no PEM block, no key body of the 84 lab key files by value, no licence key, no crypt hash, no non-`[REDACTED]`
value under a secret-named JSON field or HTTP credential header, `transaction_token_sha256` always the marker, the six
`.release-db-migrations/` names marked, the two base64 texts decoded clean, all 21 SSH key comments
`root@<operator-host>`); all 3982 `.json` and 17 `.jsonl` files parse; no vhost file holds a credential, and the `sha256=` header
and ledger digests are kept and are not what `secret-scan.txt` counts as token digests; the tree `38ad77ab…` of
`cd46ca595`, the baseline archive `3350ff44…`, the package versions, the 12 gate decisions; no file over 5 MB, no
binary, no CRLF in `tools/`, one `*.log` (`host/keepawake.log`); every PASS/FAIL/NOT-MEASURED verdict of the table
against the raw files (kept bytes and inodes, pending text, journal lines, API states and codes, reloads, served
content, backups, ledger rows, start lines, 428 and 409 codes), and the two FAILs.

What was narrowed or added (old sentence → new sentence is in the text above; the list is what changed):

1. Intro: "No installed server, celikpanel.net, licence service or certificate authority was addressed by the harness
   (see "Network": outbound NAT, no capture)." → now says only what is measured: the harness addressed no installed
   server, the five names were pinned to loopback, outbound NAT stayed open and was not captured, and the record is
   not a statement of network isolation.
2. S0: "D-031/`RESILIENCE-CONTRACT.md`: "the directory is created (0755)"." → the 0755 text is cited from
   `RESILIENCE-CONTRACT.md` line 4835 and `OPERATION-GUIDANCE.md` line 5950; D-031 in `DECISIONS.md` names no mode.
   The base directory is `root:celikpanel 0750` too.
3. S0: "nginx (master as root) read it and `nginx -t` passed; owner files in it were served (f, l)." → only f shows an
   owner file in the 0750 directory served; l does not, and the nginx master's user was not recorded.
4. f: "PASS (parts) / PHP switch NOT-MEASURED" → "NOT-MEASURED (4 of 5 expectations PASS; …)", the verdict `cells.md` has.
5. e: "`missing`" → the site-config answer says `absent` (with the one choice `recreate`); the ledger state and the
   Domains row say `missing`.
6. Adopted files: "on PHP sites `include snippets/fastcgi-php.conf;` expanded" → Debian and Ubuntu PHP sites only;
   Arch's alpha.81 text has no such line. `adopted_from`: "when both frozen templates give the same bytes" is the design
   reading, not shown by a raw file; the labels per platform are listed.
7. Database restore: "stays on disk with no row and is not rendered" → "no Domains row … the start line counts four
   sites only".
8. Network: "read back at the end of every lab" → 11 of 12 (`debian13/run-b` has no read-back); "not network isolation".
9. Power: "no pause of execution is seen" → no gap in the disk watcher; guest clocks were not compared.
10. Disk: "lowest 89.38 GiB (19:35:11Z)" → first at 19:35:11Z; the lowest byte count is at 19:36:41Z (also 89.38 GiB).
11. Offline suite: the five errors now have their file named and the one cause read at intake.
12. Counts recomputed: 93 cells, PASS 84, FAIL 6, NOT-MEASURED 3 (below the table and at the end of `cells.md`).

Facts found at intake and not stated before:

- `nginx -T` dumps of Debian and Ubuntu (24 files under `*/driver/nginx-T/`) carry `# Basic [REDACTED]`: the nginx.conf
  comment "# Basic Settings" was caught by the HTTP-Basic credential rule at collection. Their SHA-256 therefore
  differs from the file name and from `nginx_dump.sha256` in the reads (the marker is 2 bytes longer than the word it
  replaced). Arch's dumps are unredacted and match. The redaction removed no secret; it is a false positive of the rule.
- `debian13/update-defective-run-a` never started an update (setup failed on `HOST_MUTATION_BUSY`), so its failing
  header step read alpha.81, not a returned alpha.81; `checks-not-passed.txt` words that row as an automatic return.
- The root `SHA256SUMS` did not list the 11 per-run `driver/SHA256SUMS` files; it does now.
- In `tools/`, the repository checkout path in 6 scripts became `<repo>` (listed in `host/scrubbed-files.txt`).
- Cells B-before exists in both update labs (`update-good` and `update-defective`); the table cites `update-good`,
  `cells.md` cites `update-defective` for Debian; both are PASS on all platforms.

Still not measured or not shown by this record: a PHP version switch, a certificate site, a forward update after a
return, 4096 sites, the Panel screens in a browser, the number of `nginx -t` runs in g, guest clock continuity across
the host's standby entries, and any outside contact other than the five pinned names.
