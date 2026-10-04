# upd12 native run, 2026-10-02: the Panel's deferred startup mail work after an update, a rollback and an owner retry (`6b6f8a0c`)

Twelfth owner-started update run (`owner_update_trial.py`), the native measurement of product commit `6b6f8a0c`
("Panel start inside an update: the skipped mail steps are retried once the server is free",
`cmd/panel/startup_deferred_mail.go`; upd11 F2). That commit had component tests only. This run builds **everything
from `6b6f8a0c`** (`git archive 6b6f8a0c` run copy; the build clones the repository at `6b6f8a0c`) and runs three
Debian 13 `web_mail` cells once each on the current-source baseline: good, defective with the VM reset at
`payload_restored`, and owner continuation.

Disposable QEMU/KVM guests on the local `archlinux` WSL host only. No installed server (Boston, Frankfurt, any) was
touched or contacted. Nothing was committed, pushed, published or signed with a production key. `celikpanel.net`
resolved only to the guest-loopback fixture origin; no licence service was contacted. Every `result.json` carries
`native_evidence: false`. **It closes no P0 row.**

**Result in one sentence:** in all three cells the Panel that started inside the operation (the candidate after the
update, the restored baseline after the rollback, the candidate after the owner's retry) logged both startup lines
with the new "retries this by itself…" sentence, and 38-39 s after its start one attempt line reported both steps
completed; the operation itself had finished 6-13 s after the Panel start, before the first attempt, and its
verification, timing and the owner's workloads matched upd11's Debian cells. Only the first-attempt path was
exercised: the server was always free at the first attempt.

## What was built from what

One build from the run copy (`run-upd1.sh build 6b6f8a0c`, 14:00:55-14:05:21Z, `go1.26.5 linux/amd64`), every archive
`license_mode: acceptance-fixture` (`build/cur/`: artifact document, dist JSONs, fixture commits and patches, build log).
Source HEAD `6b6f8a0c937e599c6b3b5c5f0c67cbef25c9bc81` (`build/source-commit.txt`).

| Role | Label / seq | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B (`6b6f8a0c` + release policy only) | v0.1.0-alpha.81 / 81 | bc7799284aa10841f2155ede002368d56dac1aef | dfb2fd0b13bd8cc427801392d19102d77eb63459 | be4cb4881e63d42030181704472671aecbf265c090cb2842d31af2f9c363597a |
| Good G (parent B) | v0.1.0-alpha.82 / 82 | 6be145014be68a875eb178c39597d9c6e1beae6c | ad6b30125daed7367e77854f037af7d9650717bc | 05741c74b3eb7a5a5248f574d2a6c8975e02ba86a8db5940233044a6bdde6014 |
| Defective D (parent G) | v0.1.0-alpha.82 / 82 | 1384690a365c9926157ef799f18b7597b66c717c | 5ebff7ac52a4c330a91ed2d8e57805ed6b169d4d | ca88438ef76bf1cedeeefb3dada5c13e70c072e56837c5586205b0142e7cd919 |
| Start-check S (parent G; not run) | v0.1.0-alpha.82 / 82 | 59bc22b9fe432215c5ca6379b67bcfcea41b8048 | bb50d66aed709327268e980e5b271c13877d2c75 | e387d27437c331e0fb231ff2bf6f1dd00e34b4011a305fc6a5c34badceb67842 |
| Real-start R (parent G; not run) | v0.1.0-alpha.82 / 82 | 44b14a1bb99a717a058a059555c8c4feb7c4ad29 | 2a4162e82cfb6ca1dbce3ae08d3cf802aef6e476 | a76be942d75aeae2592d86348c16fc02567cc54227400c6f8fcf6f9efc90f328 |

- B and G carry the `6b6f8a0c` Panel code unchanged (`build/cur/fixture-patches.diff`: only
  `deploy/release-sequence-policy` between `6b6f8a0c`, B and G). So the Panel after the update (G), after the rollback
  (B) and after the owner retry (G) all contain the deferred retry. D's fixture patch touches only `--migrate-only`; its
  Panel never serves.
- `prove` exited 0 (`build/cur-prove.json`); 16 dry runs exited 0 with `native_evidence: false` and created no lab
  (`build/cur-dry-run-*`, `build/prove-cur.out.txt`).
- Offline suites (`build/offline-*`): pristine `6b6f8a0c` copy (`p`) and the overlay copy (`o1`) all OK -
  `test_owner_update_trial` 177 / 182, `test_recovery_candidate_archive` 11, `test_lab` 15,
  `test_current_worker_baseline` 7, `test_worker_fixture_origin` 15, `test_bound_worker_reboot` 11,
  `test_guest_bound_worker` 8.
- The repository HEAD moved during the run from `6b6f8a0c` to `966061a2` (a documentation-only commit made outside
  this run: `docs/RELEASE-NOTES-CANDIDATE-DRAFT*.md`, `docs/RESILIENCE-CONTRACT*.md`). It did not affect the run: the
  build and the run copy are pinned to `6b6f8a0c`.

## Run copy and the one harness change

The harness ran from `git archive 6b6f8a0c` (`harness-run-copy/runcopy-6b6f8a0c-files.sha256`, 41,370 files) plus an
overlay of two working-tree files (`harness-run-copy/overlay/`: SHA-256 of each file and the diff,
`harness.diff` `a3f6dd13…`). No product file and no other harness file changed. `PYTHONDONTWRITEBYTECODE=1`.

| Id | Why | Change | Kind |
| --- | --- | --- | --- |
| D1 | The cells end too early to see the loop: in upd11 `collect` read the journal ≈ 18 s after the Panel started inside the update (Debian good: start 22:29:57, collect 22:30:15), and the deferred retry's first attempt is at ≥ 30 s. | New read-only step `deferred-mail-watch` after `terminal` for cells whose Panel ends running on a mail stack (`deferred_mail_watched`: mail required, not real-start, not management-off, no scenario): reads the Panel journal every 10 s until the **last** Panel process's deferred work resolved (each step completed or failed, or the give-up line) or 900 s; then one more read 45 s later (nothing ran again); native mail file facts (size, mtime, SHA-256 of 13 Postfix/Dovecot files, `postconf -h` milters/SNI map) at the start and end; the sampler keeps running. Result field `deferred_mail`. Tests: `DeferredMailWatchTests` (5: the note equals the product constant in `cmd/panel/startup_deferred_mail.go`; per-process parsing; give-up, failure, repetition; which plans get the step; the facts script only reads). | measurement extension (no product effect; the cell's product path is unchanged, `collect` runs ≈ 66-77 s later) |

No harness defect stopped a cell; no cell was re-run.

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`, GiB; `host/c-drive.txt`)

67.6 free at the start (14:00Z), 67.5 before cell 1, 66.9 before cell 2, 65.9 before cell 3, **lowest 65.0** after the
cells (14:53Z); the end reading is the last line of `host/c-drive.txt`. Never under 40 before a cell. A process-level
"system required" request (`SetThreadExecutionState(ES_CONTINUOUS|ES_SYSTEM_REQUIRED)`, `tools/keepawake.ps1`) was
held from 14:00:36Z to 14:54:07Z (`host/keepawake.log`); no setting was changed. The host did not sleep.

## Cells

One new lab per cell, stopped by the wrapper. Times are the wrapper's (UTC).

| # | Cell (folder) | Lab, port | Wrapper (UTC) | Update window | Outcome | Overall |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | upd1-debian13-good run-a | upd12-d13-good-a, 3611 | 14:06:35-14:18:24 | start 14:16:09, `succeeded/update_verified` 14:17:08 | `update-verified` | complete-for-review |
| 2 | upd1-debian13-defective run-a | upd12-d13-def-a, 3621 | 14:19:07-14:32:35 | start 14:29:14; attempt 1 `update/active` 14:29:51.8; QMP reset 14:30:07.0 (SSH back 8.3 s); attempt 2 `rollback/active` 14:30:50.0; `recovered/rollback_verified` 14:31:09 | `recovered-automatically`, 2 attempts | complete-for-review |
| 3 | upd1-debian13-owner-continuation run-a | upd12-d13-oc-a, 3631 | 14:33:02-14:51:58 | start 14:43:11; forward attempts 14:44:53, 14:46:33, 14:48:12; `pause_pending` 14:49:26; `paused_retry_limit` 14:49:43; port released 14:50:19.2 (held 409.8 s); printed retry 14:50:20.9-14:50:42.6 rc 0; `succeeded/update_verified` 14:50:42 | `recovered-after-owner-continuation`, 3 + 1 owner; kind as-expected | complete-for-review |

Terminal checks in every cell (`summary-per-cell.txt`): installed = running for Panel and Agent with the expected
identity (G `6be14501` in cells 1 and 3, B `bc779928` in cell 2), fresh owner login, database
`equal-except-volatile` (`metrics_samples`, `server_setup_executions`; `unexpected: []`), timers equal (2), firewall
equal, site marker, mailbox and SMTP, cron row; Panel API and root CLI agreed (`agreement` passed). Workloads
(`outage-windows.txt`): site, SMTP and cron never interrupted in cells 1 and 3; in cell 2 site/SMTP only at the reset
(≤ 21 s), cron never. The 5 s sampler kept running through the deferred-work window: no SMTP or site failure there.

## The deferred retry: timeline

From each cell's Panel journal (`steps/*-deferred-mail-watch/journal-panel.txt`, `deferred-mail.json`), the product
journal of `collect` and the track samples; `side/deferred-mail-timeline.txt` is the full per-cell extract
(`tools/dml12.py`). "+s" is from the Panel's "Starting CelikPanel Backend..." line.

| | Cell 1 good | Cell 2 defective + reset | Cell 3 owner continuation |
| --- | --- | --- | --- |
| Panel that started inside the operation | candidate G, `panel[37151]` 14:16:56.964 | restored baseline B, `panel[4749]` 14:31:02.547 (started by the rollback after the reset) | candidate G, `panel[56887]` 14:50:29.255 (inside the owner's retry) |
| "Panel ready" | +0.14 s | +0.13 s | +0.08 s |
| Both startup lines refused as busy, with the retry sentence | +0.13 s | +0.13 s | +0.08 s |
| Operation ended | update worker unit exited 14:17:08.011 (+11.05 s) | recovery run (attempt 2) ended 14:31:08.643 (+6.10 s) | owner retry returned 14:50:42.630 (+13.38 s) |
| Terminal state first seen by the root CLI | `update_verified` 14:17:08 | `rollback_verified` 14:31:09 | `update_verified` 14:50:42 |
| First native write of the retry (Dovecot `98-celikpanel-tls.conf`) | 14:17:27.806 (+30.84 s) | 14:31:33.593 (+31.05 s) | 14:50:59.862 (+30.61 s) |
| `postfix-script` runs (journal; reload/check) | 14:17:29.59, 14:17:30.21 | 14:31:35.68, 14:31:36.38 | 14:51:01.63, 14:51:02.25 |
| Milter wiring writes (`main.cf`, `virtual.db`, `vmailbox.db`, `vmailbox_domains.db`) | 14:17:33.45-.73 (+36.5-36.8 s) | 14:31:39.35-.65 (+36.8-37.1 s) | 14:51:05.32-.49 (+36.1-36.2 s) |
| Attempt line (both completed) | attempt 1 of 20, 14:17:35.404 (**+38.44 s**) | attempt 1 of 20, 14:31:41.630 (**+39.08 s**) | attempt 1 of 20, 14:51:07.430 (**+38.18 s**) |
| Attempts / lines | 1 / 1 | 1 / 1 | 1 / 1 |
| 45 s later (settle read) | same process, still 1 attempt line, nothing repeated | same | same |
| Native files changed between watch start and end | only the five above, once each (same SHA-256 for `main.cf` and the Dovecot file; the `.db` maps rebuilt) | same five | same five |

Cell 3 also had 20 earlier Panel processes (14:43:51-14:48:59) that the held port stopped at their listener: none
reached the startup mail steps, so none started a retry. The first two Panel processes of every cell (the installer,
14:08-14:34, before mail was installed) logged "mail server hostname is not a valid FQDN" and "nothing to wire" and
deferred nothing.

### Verbatim journal lines (cell 1; cells 2 and 3 identical apart from time and PID)

```
2026-10-02T14:16:57.094642+00:00 dns-debian13 panel[37151]: 2026/10/02 14:16:57 certificate startup reconcile: certificate dependents: publish full mail SNI snapshot: another server change or package-manager task is still running; preserve pending outbox: <nil>; the Panel retries this by itself every 30 seconds for up to 10 minutes once no other server change is running; nothing needs to be done now
2026-10-02T14:16:57.098835+00:00 dns-debian13 panel[37151]: 2026/10/02 14:16:57 milter wiring at startup: another server change or package-manager task is still running; the Panel retries this by itself every 30 seconds for up to 10 minutes once no other server change is running; nothing needs to be done now
2026-10-02T14:17:35.404230+00:00 dns-debian13 panel[37151]: 2026/10/02 14:17:35 startup mail work, attempt 1 of 20: mail certificate publication completed (mail SNI from 0 active secure-mail certificates); mail filter wiring completed (milters="inet:localhost:11332" maps=hash)
```

The caller's expected separate lines "mail SNI reconciled…" and "milter chain: …" are, by the product source, not
written by the retry: its single attempt line carries both results ("mail SNI from 0 active secure-mail
certificates"; `milters="inet:localhost:11332" maps=hash`, the same detail upd11 recorded at a normal Panel start).
Lines written alongside by the publication (product journal, every cell):

```
2026-10-02T14:17:29.588890+00:00 dns-debian13 postfix/postfix-script[39529]: warning: not owned by root: /var/spool/postfix/etc/resolv.conf
2026-10-02T14:17:30.283924+00:00 dns-debian13 postfix/postmap[39762]: warning: unsupported dictionary type: lmdb. Is the postfix-lmdb package installed?
2026-10-02T14:17:30.283933+00:00 dns-debian13 postfix/postmap[39762]: fatal: unsupported map type: lmdb
```

### Was the operation's own verification disturbed?

No, in all three cells. The retry waits 30 s before its first readiness read, so nothing ran inside the update's
post-start stability wait; the operation had ended and recorded its terminal state 6-13 s after the Panel start, 17-25
s before the first write. Against upd11's Debian cells of the same kinds (`48e54657`, no retry loop):

| Measure | upd11 | upd12 |
| --- | --- | --- |
| Good: owner start → candidate Panel start | 46 s (22:29:11 → 22:29:57.09) | 47 s (14:16:09 → 14:16:56.96) |
| Good: Panel start → update worker exit | 10.8 s | 11.05 s |
| Good: owner start → `update_verified` seen | 57 s | 59 s |
| Good: Panel down (sampler bound) | 20-30 s | 25-35 s |
| Defective: attempt 1 → attempt 2 | 61 s | 58 s |
| Defective: restored Panel start → recovery run end | 7.05 s | 6.10 s |
| Defective: attempt 2 → `rollback_verified` | 21 s | 19 s |
| Defective: Panel down | 93-103 s | 86-96 s |
| Owner continuation: port held | 413.3 s | 409.8 s |
| Owner continuation: retry run | 23:35:30 → 23:35:55.6 | 14:50:20.9 → 14:50:42.6 |
| Owner continuation: Panel start → retry end | 15.6 s | 13.4 s |
| Owner continuation: Panel down | 427-437 s | 417-427 s |

The release-recovery timer (every ≈ 30 s, nothing pending) ran 14:17:27.29-27.76 in cell 1 (it ended 46 ms before
the Dovecot write) and 14:31:39.36-39.77 in cell 2 (overlapping the milter wiring writes); both runs ended
"Deactivated successfully" and the retry's steps completed. No conflict was visible.

## Secure-mail certificate across the update: skipped (no seeding path)

The cells' `seed` creates the domain with `ssl_type: "none"`; `owner_update_trial.py` has no certificate step. The
fixture-CA paths elsewhere in `deploy/e2e/release-recovery` (the MAIL-KIT / MAIL-RUNTIME / MAIL-EXECUTOR trials) belong
to a separate guarded fixture that installs leaves through the native renewal hook, not a Panel secure-mail
certificate reachable from these cells. Using the Panel's custom-certificate upload with a self-signed certificate
would be a new flow, which this run was told not to build. So the comparison of Nginx and Postfix/Dovecot SNI
(native files and `openssl s_client`) was not made. Every retry here published "0 active secure-mail certificates";
`/etc/postfix/celikpanel_sni*` was absent and `tls_server_sni_maps` empty before and after.

## Findings

| # | Class | Finding | Evidence |
| --- | --- | --- | --- |
| R1 | candidate behaviour as designed (measured) | The bounded retry ran once per Panel that started inside an update, a rollback and an owner retry, completed both refused steps in its first attempt (+38.2-39.1 s), wrote each native file once, logged exactly one line, and did not run again (45 s settle). The startup lines carry the documented sentence. | table above; `*/run-a/steps/*-deferred-mail-watch/deferred-mail.json` |
| O1 | candidate product observation, existing before `6b6f8a0c` | Every SNI publication logs `postfix/postmap … fatal: unsupported map type: lmdb` (plus the lmdb warning) on Debian 13 without `postfix-lmdb`. It is between the Dovecot write and the milter wiring writes, and the attempt still completes (`maps=hash`). The same lines appear in upd11's management-off restart on `48e54657`. An owner reading the journal sees a "fatal" line that changes nothing. Not caused by this commit. | `*/steps/*-collect/journal-product.txt`; upd11 `upd1-debian13-mgmt-off-reboot/run-a/steps/18-collect/journal-product.txt` |
| O2 | observation (as the contract says: "re-assert native files") | Each retry rewrites `98-celikpanel-tls.conf` and `main.cf` with identical content (new mtime), rebuilds three `.db` maps from unchanged sources and runs `postfix-script` twice (each logs "warning: not owned by root: /var/spool/postfix/etc/resolv.conf"). `aliases.db` and `master.cf` are untouched. Whether an owner's own edit to these files would be detected or overwritten was not tested. | `deferred-mail.json` `mail_file_facts` |
| L1 | not exercised (scope) | The host was always free at the first attempt (the operation had ended 17-25 s earlier), so the busy-again branch (attempt > 1), the give-up line, a verified failure line and the pending-outbox domain retry were not reached natively. | all cells |
| L2 | fixture limitation | No secure-mail certificate could be seeded without building a new flow (above). | `seed` step |
| D1 | harness extension (not a defect) | The read-only watch step described above. | `harness-run-copy/overlay/` |

No candidate product defect was found in the three paths measured.

## Owner-visible texts (verbatim, EN/TR)

`owner-texts.txt` lists every root-CLI status text (EN and TR) with the first time and cell it was seen, and the update
card and recovery screen the driver rendered from the installed build's catalogues. They match upd11's texts for the
same kinds (`update_running`, `recovery_running`, `waiting_for=starting`, `rollback_verified`, the forward-completion
`panel_start_unverified` sequence through `retry_scheduled`, `pause_pending` and `paused_retry_limit`, and
`update_verified`). The new deferral sentence is in the Panel journal only (above); no screen or CLI text names it.

## Real origin, licence, secrets

- Every origin check resolved `celikpanel.net` only to `127.0.0.1` with fixture HTTP 200 (`result.json`
  `scope.origin`); the baseline installer logs are counted in `secret-scan.txt`.
- Licence: `license_service: not contacted`; every Panel logs the acceptance-fixture banner.
- The guests used Debian's package mirrors (installer, setup); the host downloaded nothing.
- `secret-scan.txt` (`tools/scan12.py`, `tools/scan.sh`) over the whole folder before hashing: **clean.** 864 files;
  0 PEM private-key blocks; 0 hits for the 172 body lines of the 12 key files of the 3 labs (SSH keys, fixture signing,
  CA and TLS keys); 0 fixture-licence literals or `CPK-` shapes; 0 unredacted password/secret/token/session/cookie
  fields; 0 tokens of the mailbox/database password shape; 459 redaction markers. The 7 tokens of the admin-password
  shape are older evidence file names (`inspect-after-continuation-20260930T…Z`) in the run-copy hash list. The real
  origin's address prefix appears only in the scan scripts and the scan output; the three installer logs name neither
  it nor `celikpanel.net`. Longest repository-relative path: 197 characters.
- `SHA256SUMS` lists every file except itself (`sha256sum -c SHA256SUMS`).

## Removals (disk), host leftovers, host state

After each cell's evidence was staged here and the staged driver `SHA256SUMS` verified, `tools/rmoverlay12.sh`
removed **only the overlay disks** of the lab this run had created for that cell: 6 files, 5.87 GB
(`host/removals.txt`: `upd12-d13-good-a` 169,607,168 + 1,773,862,912 bytes; `upd12-d13-def-a` 136,380,416 +
1,852,375,040; `upd12-d13-oc-a` 159,252,480 + 1,776,943,104). The guests cannot be restarted; everything the driver
collected is here. Nothing that existed before this run was modified or deleted.

Host leftovers (`host/host-leftovers.txt`): `/var/tmp/cp-upd12-run` (463 MB: run copy, overlay, jobs, logs, build
records); the three stopped labs without overlays (921 MB each); the build clone
`/var/tmp/cp-upd1-build/20261002t140055z` (800 MB); five dist folders
`/var/tmp/cp-pair-accept/dist/<commit>-acceptance-license` (778 MB each). No QEMU process remains; no dry-run lab
exists. Git: no commit, push or config change by this run (local config key list fingerprint `4363b888…`, as
upd8-upd11). Repository writes: the two harness files in the working tree (uncommitted, = the overlay) and this
folder. No inline `wsl.exe` command contained `$`; host steps ran from LF script files.

## Files

`build/` (artifact document, dist JSONs, fixture commits and patches, build log notes, prove, 16 dry runs, offline
logs), `harness-run-copy/` (run-copy hashes, overlay files and diff, cell jobs, the scripts that made the copy, the
build and the jobs), `tools/` (host-only readers: extraction, timeline, summaries, texts, staging, removal, scan,
sums, C: reading, keep-awake), `host/` (host check, C: readings, removals, leftovers, keep-awake log), one folder per
cell run with the driver's evidence unchanged (its own `SHA256SUMS` verified when staged), `host/` and `side/`
(`extract.txt`, `changed-paths.txt`, `deferred-mail-timeline.txt`). `summary-per-cell.txt`, `step-table.md` (its
step list predates D1; the watch step is in each run's `result.json`), `outage-windows.txt`, `owner-texts.txt`,
`secret-scan.txt`, `SHA256SUMS`.

## What this run does NOT prove

- The retry against an operation that still holds the server at +30 s or later (the window against a long update or
  rollback): attempts > 1, the give-up line after 20 attempts, the verified-failure line, and the per-domain retry of
  pending certificate activations. None was reached.
- Anything about secure-mail certificates: SNI consistency between Nginx and Postfix/Dovecot after an update was not
  measured (L2). Every publication here had 0 certificates.
- The case `6b6f8a0c` was made for: an interrupted certificate activation (outbox row) or a Postfix whose hash maps
  need the wiring repair.
- Ubuntu 24.04 and Arch (no mail on Arch), the start-check and real-start kinds, management-off, and any alpha.80
  baseline: not run in this matrix.
- That this always happens this way: one run per cell on a laptop host with the acceptance fixture, a fixture signing
  key and a loopback origin. Production signing, the real release origin, the licence service, DNS (external mode)
  and certificate issuance or renewal are not exercised.
- Whether the retry would overwrite an owner's own edit to the native mail files (O2).
- A second fault while the retry runs, power loss, browser rendering, panel removal.

It closes no P0 row. Wall time: host check 13:58Z; build 14:00-14:05Z; cells 14:06-14:52Z; staging and scan after.
