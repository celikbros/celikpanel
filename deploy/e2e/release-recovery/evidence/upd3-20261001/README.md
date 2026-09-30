# upd3 native run, 2026-09-30 (folder dated for the item 3 batch 2026-10-01)

Roadmap item 3: the owner-started update acceptance driver (`owner_update_trial.py`), third
attempt, eight cells. Disposable QEMU/KVM guests on the local `archlinux` WSL host only. Product
build `94be6b6e`. This is the first native measurement of `8ffc5e06` (candidate start check and
stability wait), `32fce9c7` and `94be6b6e` (hosting root on Arch, update card, root CLI, guard and
rollback journal texts).

**Result in one sentence: seven of eight cells reached their expected end; the Debian 13 good cell
did not, because its update stopped in preflight with a cause the product did not keep.**

- Start-check candidate S: failed in phase `active` with `candidate_panel_startup_check_failed`,
  reason `tls_pair_invalid`, and the server returned to the old release automatically on both
  platforms (2 automatic attempts each, second fault included, retry budget not exhausted).
- Real-start candidate R: passed the start check, failed the stability wait with
  `panel_start_unverified`, was retried forward 3 times and paused on both platforms. Site and cron
  ran throughout (Debian mail too). The printed one-time retry was read, never run.
- Good candidate G: passed the start check and was verified forward on Arch. On Debian 13 the
  update never reached the start check (product finding F1).
- Migrate-only candidate D: rolled back automatically on both platforms (2 attempts each). Arch's
  SIGKILL at `runtime_verified` was reached for the first time.
- Arch hosting root (P3 correction): `/var/www` and `/var/www/celikpanel` were created 0755
  root:root at the first site and recorded in the receipt. The site answered 200 with the marker
  and the cron stamp advanced in all four Arch cells.

Nothing here passes a P0 row. Every `result.json` carries `native_evidence: false` (8 top-level
and 4 kind judgements). The owner judges P0.1, P0.2, P0.3 and P0.5.

No installed server (Boston, Frankfurt, any) was touched. Nothing was pushed, published or signed
with a production key. The candidates were served only by the guest-loopback fixture origin; the
real `celikpanel.net` and license services were not contacted (see "Real origin").

## Build and proofs

`bash deploy/e2e/release-recovery/run-upd1.sh build 94be6b6e` (from the run copy, see below) ran
17:40:23Z to 17:43:30Z and exited 0 (`build/build.*`). Clone:
`/var/tmp/cp-upd1-build/20260930t174023z/repo`, web built fresh, `go1.26.5 linux/amd64`.

| Role | Label / seq | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B | v0.1.0-alpha.81 / 81 | 58813f1d0b903cbbc645a4fbab724b8a84406b86 | 2144156947f4ef8f7124964d3c2a7277ea455c95 | 28f27f5d95b7cb2eb1239693020664bae57fbd780db9a46fd84bea3d5ca33546 |
| Good G | v0.1.0-alpha.82 / 82 (parent B) | 901593abf12c90c91222d2c61a8bd7c0040523c3 | a07e038d0bbb6e2ec97ef5e86fb3e47b03385dd9 | 36766be1eb37359658ebdb53ddb431660812f620c22639a2097e6f7762066542 |
| Defective D | v0.1.0-alpha.82 / 82 (parent G) | 7d11ff6535c86a865add390062311db2fb1d6e50 | ebc85a3809562ae6108c59c38e72c779699704c5 | d4f96c107f4ee98c0685a6509a815999dc797737020a897995a0c722fa082f97 |
| Start-check S | v0.1.0-alpha.82 / 82 (parent G) | 592fc7baf38536821450388d44bca18e195b06ef | 56cc63708e24b1d1c4c9936ee3ca7a8831e357fa | 4e07e47313b524466bc671f4bcccb7714603465aa92f68cce960a672840ad17b |
| Real-start R | v0.1.0-alpha.82 / 82 (parent G) | 36c5c252a5bdbd71f3cd0f9af58e004dca1d47bb | 23ffe07f177e92aa78b44a082271803dc828dc06 | 4b95724bb8c84300ce0b600a86c6690461a2668158ffef7d63814ec00cc92c40 |

- Source HEAD 94be6b6ec3c50929bd8119fadb0c8c0879ae8dbe (`build/source-commit.txt`). Fixture
  commits and their patches: `build/fixture-commits*.txt`, `build/fixture-kind-patches.diff`.
- All five archives are `license_mode: acceptance-fixture` (`build/dist-*.json`).
- `prove` exited 0 for all five roles: 614 files each, 481 committed static files proved,
  `dns-owner-tools/` 4 files (`build/prove.json`).
- `dry-run` passed for all eight cells with `native_evidence: false`; no lab was created
  (`build/dry-run-*.json`).
- Offline suites on the pinned export: `test_owner_update_trial.py` 83/83,
  `test_recovery_candidate_archive.py` 11/11 (`build/offline-tests-*-94be6b6e.txt`). The job's
  rc file says 1 only because its final `tail -4` line was wrong (`build/offline-job.err.txt`);
  both suites printed `OK`.

## Run copy and harness corrections

The harness ran from `git archive 94be6b6e` at `/var/tmp/cp-upd3-run/harness` (file hashes:
`harness-run-copy/runcopy-94be6b6e-files.sha256`). The repository was not edited.

| Id | Defect | Kind | Handling | Cells |
| --- | --- | --- | --- | --- |
| H8 | `track` knows only terminal and paused states. A failure before any change leaves the recovery status at `failed/none` forever, so the loop waits its full 90 minutes and then reports the same inconclusive result. | mechanical | Run copy `harness-h8`: stop `track` after 600 s of unchanged `failed/none` with no automatic recovery and no wait (`harness-run-copy/H8-owner_update_trial.py.diff`). The verdict stays inconclusive; offline suite 83/83 on the H8 copy. H8 has no offline test of its own. | Cells 2-8 used H8, which never triggered. Cell 1 ran unmodified and waited the 90 min. It was **not** re-run: its stop was a product failure, not a harness one. |
| H9 | `judge_start_check` rule `rollback-dispatch` reads only the receipt's `phase`. After the Arch SIGKILL at `runtime_verified`, attempt 2 is `operation=rollback phase=completion`, the resumed rollback's own completion (`upd1-arch-startcheck/run-a/steps/14-collect/budget.json`). | pass-rule (owner's decision) | Recorded, not changed. It is the only failed rule in upd1-arch-startcheck; the other 10 rules were as expected. | arch-startcheck (the same receipt pair is in arch-defective, which has no kind judge) |
| H10 | `update_card_guidance` still models the pre-94be6b6e card (raw summary). `recovery_guidance` has no `recovery.automatic.cause.*` line. So the driver's finding "the update card shows the server summary verbatim" in d13-good, d13-defective and arch-defective describes the old card. | harness model lag | Recorded, not changed. The product's own card is rendered from `web/src/lib/systemUpdateOutcome.ts` and the catalogues of the build in each cell's `side/extract.txt` ("CARD[en]/[tr]", "PREVIOUS-ATTEMPT"). | all |

Observer tools (not part of the driver) are in `observer-tools/`. Two defects in the v1 sidecar
(`observer-tools/v1/`) affected cell 1 only; v2 fixed both for cells 2-8:

- The light "before first site" probe ended on `getent`'s rc 2, so all 13 probes of cell 1 were
  lost. `after-setup` (17:52:37) and `after-seed` still show the parents.
- The v1 `pre-update` inspection ran at 17:54:17Z, inside cell 1's update preflight window
  (17:54:15.9 to 17:54:30.0). It was read-only: `ss`, `systemctl show`, `curl`, `find` under
  `/var/www /home`, `journalctl`, `ls`/`cat` of the observation records, and `namei`/`stat`. It
  touched nothing the compatibility check reads. v2 inspects only at `setup`, `seed`, `track`,
  `owner-continuation` and `terminal`.

## Cells

One new lab per cell, stopped by the wrapper. Order and port bases:

| # | Cell | Lab | Port | Harness | Outcome | Overall |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | upd1-debian13-good | upd3-d13-good-a | 2441 | 94be6b6e | `not-terminal`: preflight failure, B still installed (F1) | failed |
| 2 | upd1-debian13-defective | upd3-d13-def-a | 2451 | H8 | `recovered-automatically`, 2 attempts | complete-for-review |
| 3 | upd1-debian13-startcheck | upd3-d13-sc-a | 2401 | H8 | `recovered-automatically`, 2 attempts; kind as-expected | complete-for-review |
| 4 | upd1-debian13-realstart | upd3-d13-rs-a | 2421 | H8 | `paused-owner-action-required`, 3 attempts; kind as-expected; terminal failed on certbot.timer (F2) | failed |
| 5 | upd1-arch-good | upd3-arch-good-a | 2461 | H8 | `update-verified` | complete-for-review |
| 6 | upd1-arch-defective | upd3-arch-def-a | 2471 | H8 | `recovered-automatically`, 2 attempts | complete-for-review |
| 7 | upd1-arch-startcheck | upd3-arch-sc-a | 2411 | H8 | `recovered-automatically`, 2 attempts; kind "finding" = H9 only | failed |
| 8 | upd1-arch-realstart | upd3-arch-rs-a | 2431 | H8 | `paused-owner-action-required`, 3 attempts; kind as-expected | complete-for-review |

Every cell was run exactly once. No cell was re-run.

### Step table (UTC start-end; P passed, O observed, F failed, I inconclusive, S skipped, N not run)

| Step | d13-good | d13-def | d13-sc | d13-rs | arch-good | arch-def | arch-sc | arch-rs |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| preflight + origin | P 17:44:56-45:07 | P 19:25:42-25:52 | P 19:37:16-37:27 | P 19:49:21-49:32 | P 20:05:25-05:36 | P 20:15:16-15:28 | P 20:24:57-25:10 | P 20:36:14-36:28 |
| baseline-install | P 80 s | P 34 s | P 80 s | P 34 s | P 148 s | P 149 s | P 188 s | P 208 s |
| login, license | P ("not contacted") | P | P | P | P | P | P | P |
| setup | O 17:46:28-52:35 | O 19:26:27-32:09 | O 19:38:48-44:50 | O 19:50:07-55:54 | O 20:08:05-10:58 | O 20:17:58-20:56 | O 20:28:19-31:31 | O 20:39:57-42:55 |
| seed | P | P | P | P | P (no mail) | P (no mail) | P (no mail) | P (no mail) |
| pre-state | P -17:54:14 | P -19:34:08 | P -19:46:08 | P -19:57:13 | P -20:13:09 | P -20:22:05 | P -20:33:12 | P -20:44:14 |
| arm, owner-start | P 17:54:15 | P 19:34:09 | P 19:46:09 | P 19:57:14 | P 20:13:10 | P 20:22:06 | P 20:33:13 | P 20:44:15 |
| track | I 17:54:15-19:24:28 (362 samples, 90 min deadline) | P -19:36:03 (25) | P -19:48:10 (27) | O -20:04:10 (56) | P -20:13:59 (13) | P -20:23:37 (21) | P -20:34:52 (22) | O -20:51:05 (55) |
| owner-continuation | N | S | S | O (retry read, not run) | S | S | S | O (retry read, not run) |
| terminal | F identity (B, expected after F1) | P | P | F certbot.timer (F2) | P | P | P | P |
| collect, verdicts | P, P | P, P | P, P | P, P | P, P | P, P | P, P | P, P |
| kind-expectation | - | - | P as-expected | P as-expected | - | - | F (H9) | P as-expected |

Every Arch baseline install includes the owner restart that the installer requires. Setup on
every cell waited at `access_dns` on the isolated host (H4 rule). Arch `web_mail` was
refused at review (`server_setup_service_unsupported:dovecot`) and fell back to `web`, as in upd2.

## What each kind showed

### Start check on the good candidate

- **Arch:** passed. The observer saw `completion.pending` at 20:13:48; `succeeded/update_verified`
  at 20:13:59; installed and running build G. The update log has no start-check failure line (a
  passing check prints none; update.sh:2136-2172).
- **Debian 13:** not measured in the good cell (F1). R, whose start-check path is G's, passed the
  check on Debian: no reason line, `CELIKPANEL_UPDATE_CHECKPOINT database_verified_before_start`,
  then `completion.pending` (d13-realstart observer events).

### Start-check candidate S (both platforms)

| | Debian 13 | Arch |
| --- | --- | --- |
| Failure line | 19:46:55 `code=candidate_panel_startup_check_failed ... panel startup check failed: tls_pair_invalid: the panel TLS certificate and private key cannot be loaded as a matching pair` | 20:33:52, same |
| Sidecar `<request>.failure` | valid, `candidate_panel_startup_check_failed`, bound to S | same |
| completion.pending | never seen | never seen |
| Attempt 1 | 19:46:56 `operation=update phase=active` | 20:33:53 `operation=update phase=active` |
| Second fault | QMP reset at `payload_restored` 19:47:10 (checkpoint 19:47:09.86), SSH back 8.3 s, new boot | SIGKILL at `runtime_verified` 20:34:08.37 |
| Attempt 2 | 19:47:53 `operation=rollback phase=active` | 20:34:39 `operation=rollback phase=completion` (H9) |
| Result | "Rollback complete" 19:48:09, `recovered/rollback_verified`, `failure_code` kept | 20:34:51, same |
| Budget | 2 of 3 used, not exhausted, no owner step | same |
| Terminal | B installed and running; database `equal-except-volatile` (65 tables; `unexpected: []`); timers and firewall equal; marker, mailbox/SMTP (Debian), cron rows; fresh login | same (no mail) |

### Real-start candidate R (both platforms)

| | Debian 13 | Arch |
| --- | --- | --- |
| Start check | passed (no reason line) | passed |
| completion.pending | 19:57:59 | seen (observer) |
| Failure line | 19:59:01 `code=panel_start_unverified ... the new panel did not stay running and answer on its own address after the update was applied` | 20:45:53, same |
| Stability wait in each forward attempt | `!! pending update panel did not stay running and answer on its own address` at 20:00:10, 20:01:50, 20:03:30 | 20:47:01, 20:48:40, 20:50:19 |
| Attempts (all `phase=completion`) | 19:59:02, 20:00:42, 20:02:22 | 20:45:54, 20:47:32, 20:49:11 |
| Pause | 20:04:02 `paused_retry_limit`, `first_failure_code=panel_start_unverified` | 20:50:50, same |
| Printed retry (read, not run) | `sudo /usr/libexec/celikpanel/recovery recover --retry --snapshot 20260930T195734Z-from-unknown-to-36c5c252...-14d3b140f0e9ad7b3b44f254594d61ca` | `... 20260930T204431Z-from-unknown-to-36c5c252...-45483c2cc4439aa4098f44a8977b2bef` |
| Installed | R (no rollback) | R |
| Site / mail / cron samples after start (5 s) | web 84/84 OK, SMTP 84/84 OK, cron 8 successive stamps 19:57:01-20:04:01 | web 83/83 OK, mail not provided, cron 8 stamps 20:44:00-20:51:00 |
| Panel | down from 19:57:31 to the end (>395 s, `down-from-update-until-end`) | from 20:44:27 (>395 s) |
| Terminal | site marker, mailbox, SMTP OK; firewall equal; **certbot.timer active -> inactive** (F2) | all equal (certbot-renew.timer was already disabled/inactive before the update) |

### Migrate-only candidate D

- **Debian 13:** failed 19:34:47 (phase active). Attempt 1 at 19:34:48. Reset at `payload_restored`
  19:35:03 (new boot, SSH back 8.3 s). Attempt 2 at 19:35:45 (`rollback/active`). Rollback complete
  19:36:02. Both attempts automatic, no owner action.
- **Arch:** failed 20:22:39. Attempt 1 at 20:22:40. SIGKILL at `runtime_verified` 20:22:55.15 (the
  first Arch second fault that was reached). Attempt 2 at 20:23:26 (`rollback/completion`). Rollback
  complete 20:23:37. Both attempts automatic, no owner action.

### Outage windows (guest sampler 5 s; `outage-windows.txt`)

| Cell | Site | SMTP | Cron | Panel (guest) | Host SSH |
| --- | --- | --- | --- | --- | --- |
| d13-good | never | never | never | never down | - |
| d13-def | 0-20.7 s at the reset (`interrupted-only-by-host-reset`) | 0-15.7 s at the reset | never | 85.7-95.7 s | 0-12.2 s |
| d13-sc | 0-22.1 s at the reset | 0-17.1 s at the reset | never | 87.1-97.1 s | 5.8-15.8 s |
| d13-rs | never | never | never | from update to end | - |
| arch-good | never | not seeded | never | 20-30 s | - |
| arch-def | never | not seeded | never | 25-35 s + 0-10 s | - |
| arch-sc | never | not seeded | never | 30-40 s + 0-10 s | - |
| arch-rs | never | not seeded | never | from update to end | - |

DNS: `not-provided-external-dns` in every cell.

### Views: which answered, and whether they agreed

- **Agreement:** passed in every cell. When both known sources answered, they agreed (d13-good
  361/362 with 1 start-instant lag; d13-sc also 1 lag).
- **Panel down (every update or recovery window):** the root CLI was the only source.
  - d13-sc: API 9/27, CLI 26/27. The CLI missed one sample during the reset.
  - arch-sc: API 14/22, CLI 22/22.
  - d13-rs: API 6/56, CLI 56/56.
  - arch-rs: API 5/55, CLI 55/55.
- **At the real-start pause:** the Panel API and the offline page were unreachable, because
  nothing serves the offline page while the Panel is down. Only a browser-saved copy could show it;
  it names `sudo /usr/libexec/celikpanel/recovery status --request-id <rid> --lang en|tr`. The root
  CLI answered. The SSH owner view was not attempted.

## Guidance observed (verbatim, EN / TR)

All texts come from the product itself: the CLI output of each sample, or the build's own
catalogues. Per-cell lists with first-seen times are in `<cell>/run-a/side/extract.txt`.

**Update card after a verified rollback** (d13-sc; arch-sc identical):

- EN: "Update not completed; previous version restored || The update to v0.1.0-alpha.82 did not
  complete. The server was returned to v0.1.0-alpha.81 automatically and is running it now. ||
  Cause: the new version's panel failed its start check before anything was switched on. || Nothing
  needs to be done on the server. Do not start the update to v0.1.0-alpha.82 again until a
  corrected version is published. If the server's message names a problem on this server, fix it
  first. || Nothing resumes by itself: the server keeps running v0.1.0-alpha.81. When a newer
  version is published, “Check for updates” offers it."
- TR: "Güncelleme tamamlanmadı; önceki sürüm geri yüklendi || v0.1.0-alpha.82 sürümüne güncelleme
  tamamlanmadı. Sunucu otomatik olarak v0.1.0-alpha.81 sürümüne döndürüldü ve şu an onu
  çalıştırıyor. || Neden: yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç
  denetiminden geçemedi. || Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm
  yayımlanana kadar v0.1.0-alpha.82 güncellemesini yeniden başlatmayın. Sunucunun iletisi bu
  sunucudaki bir sorunu belirtiyorsa önce onu giderin. || Hiçbir işlem kendiliğinden sürmez: sunucu
  v0.1.0-alpha.81 sürümünü çalıştırmaya devam eder. Daha yeni bir sürüm yayımlandığında
  “Güncellemeyi kontrol et” onu gösterir."
- For D (d13-def, arch-def), the cause line is the generic one: "Cause: the update failed before
  it completed; the server recorded no more specific cause." / "Neden: güncelleme tamamlanmadan
  başarısız oldu; sunucu daha belirli bir neden kaydetmedi."
  - The card adds the secondary line "The server reported: reviewed updater failed: exit status 1:
    !! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=offline panel
    database migration failed; ... detail=".
  - That line is English inside the TR card too ("Sunucunun bildirdiği: reviewed updater failed:
    ..."), and it still says `state=recovery_required` (O6).

**"This version already failed on this server"** (from the `previous_attempt` of the next check):

- d13-sc / arch-sc:
  - EN: "This version already failed on this server || v0.1.0-alpha.82 was tried on this server on
    <time>, and the server was returned to the previous version. Starting it again repeats the same
    update unless the cause has been fixed. || Recorded cause: The new version's panel failed its
    start check."
  - TR: "Bu sürüm bu sunucuda daha önce başarısız oldu || v0.1.0-alpha.82 bu sunucuda <time>
    tarihinde denendi ve sunucu önceki sürüme döndürüldü. Neden giderilmediyse yeniden başlatmak
    aynı güncellemeyi tekrarlar. || Kaydedilen neden: Yeni sürümün paneli başlangıç denetiminden
    geçemedi."
- d13-def / arch-def: the same text without the cause line.
- d13-good (`phase: failed`):
  - EN: "... v0.1.0-alpha.82 was tried on this server on 2026-09-30T17:54:30Z and did not
    complete; this server runs v0.1.0-alpha.81 now. Starting it again repeats the same update
    unless the cause has been fixed."
  - TR: "... denendi ve tamamlanmadı; bu sunucu şu an v0.1.0-alpha.81 sürümünü çalıştırıyor. ..."
- The real-start cells have no check, because the Panel is down.

**Root CLI** (all cells; each output ends with the support line). S after the verified rollback:

- EN: "The new version's panel failed its start check before anything was switched on, so the
  server was returned to the previous version automatically. The previous version keeps running.
  Nothing needs to be done on the server. Do not start the same version again until a corrected
  version is published. When you report this, include the reason line shown for this update on the
  panel's update page." / "Recorded state (for support): phase=recovered reason=rollback_verified
  proof=rollback_verified previous_failure=update_failed
  failure_code=candidate_panel_startup_check_failed"
- TR: "Yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi; bu
  yüzden sunucu otomatik olarak önceki sürüme döndürüldü. Önceki sürüm çalışmaya devam ediyor.
  Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü
  yeniden başlatmayın. Bunu bildirirken panelin güncelleme sayfasında bu güncelleme için gösterilen
  neden satırını ekleyin." / "Kayıtlı durum (destek için): phase=recovered ..."
- The judge compared these with `cmd/recovery/main.go` of the built commit: 0 mismatches in all
  four kind cells.

**Paused text with the panel-log cause** (d13-rs 20:04:10, arch-rs 20:51:04):

- EN: "The update was applied, but the new version's panel did not come up, and completing the
  update was retried to its limit. Read the panel log on the server: sudo journalctl -u
  celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed. There is no
  supported return to the previous version from this point. Automatic recovery used all three
  attempts without finishing, so the server may be between versions. The server owner must act:
  read sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50, fix the cause it
  names, then run the one-time same-operation retry command shown there. Checking status does not
  retry recovery." / "Recorded state (for support): phase=recovery_required
  reason=recovery_incomplete proof=none previous_failure=recovery_failed
  automatic_recovery=paused_retry_limit first_failure_code=panel_start_unverified"
- TR: "Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve güncellemenin tamamlanması
  sınırına kadar yeniden denendi. Sunucudaki panel günlüğünü okuyun: sudo journalctl -u
  celikpanel-panel -n 50. Bu neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu
  noktadan önceki sürüme desteklenen bir dönüş yok. Otomatik kurtarma üç denemenin hepsini kullandı
  ve tamamlanamadı; sunucu iki sürüm arasında kalmış olabilir. Sunucu sahibinin işlem yapması
  gerekiyor: sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 çıktısını
  okuyun, belirtilen nedeni giderin ve orada aynı işlem için gösterilen tek seferlik yeniden deneme
  komutunu çalıştırın. Durum sorgusu kurtarmayı yeniden başlatmaz."
- The web paused card (catalogue `recovery.automatic.cause.panel_start_unverified`) has the same
  cause text. The Panel was down, so no browser could show it.

**Other product lines:**

- **Guard:** Debian reset cells, 19:35:12 and 19:47:19: "celikpanel release start guard: held: a
  CelikPanel update or recovery is in progress; the unit starts when it finishes". "unsafe
  directory" no longer appears (O4 corrected). Arch had no boot, so there is no guard line.
- **Rollback journal:** all four rollback cells: "==> Rollback complete / Geri alma tamamlandı" and
  "Restored release source commit / Geri yüklenen sürümün kaynak commit'i:
  58813f1d0b903cbbc645a4fbab724b8a84406b86 (from the restored Agent's build record / geri yüklenen
  Agent'ın yapı kaydından)". This corrects O3's "unknown". The snapshot name keeps
  `from-unknown-to-` by design.
- **Wait text:** after the reset: "Recovery waiting for the operating system transition; no owner
  action is needed. The native recovery timer will retry this same operation. Recovery is not yet
  complete."

## Product findings

**F1 (D-024; stops upd1-debian13-good).** The owner-started update of the good candidate failed in
preflight at 17:54:30Z, 14 s after the start. The only reason was "!! selected recovery runtime
cannot verify the current installation before update".

- **Where it stops:** at update.sh:426-428 (`recovery verify-compatibility`), after the candidate
  recovery runtime had been prepared and selected.
- **The cause is not kept anywhere:**
  - `cmd/recovery/compatibility.go:70-109` collapses every step into one generic error.
  - `compatibility_linux.go:51-52` discards the checkers' output.
  - update.sh:426 is not wrapped in `run_update_idle_probe`, unlike line 420.
  - The die comes before the EXIT trap, so there is no `CELIKPANEL_UPDATE_FAILURE` line.
- **The status stays `failed/none/update_failed` for good:** 90 min observed. The owner is told:
  - CLI EN: "The update failed. If it stopped before anything was changed, the server keeps running
    the version it had; otherwise automatic recovery takes over and this status changes to recovery.
    Nothing to do on the server now: check this same request again in a few minutes and do not start
    another update. The panel's update page shows the reason when the panel is reachable."
    - The status never changes.
    - "Do not start another update" has no end.
    - The "reason" on the update page is that raw line.
  - The web recovery reader: "The update failed || The server owner must inspect the update log
    using this operation ID..."
  - The next check: "This version already failed on this server ... Starting it again repeats the
    same update unless the cause has been fixed", with no cause.
- **B kept running and everything was preserved:** database `equal-except-volatile`, timers and
  firewall equal, site, SMTP and cron uninterrupted (1083 samples).
- **Not reproduced:**
  - The other seven updates passed the same preflight.
  - 60 + 60 read-only runs of the candidate `panel-checker --check-service-operations-idle-wal-aware`
    and `agent-checker --check-service-mutation-idle`, with the exact environment, all passed at
    18:23Z (`side/query-checker-race-loop.txt`).
  - `/opt/celikpanel` metadata conformed.
- **Code-reading hypothesis (not proven):** a live Panel write raced the WAL-aware checker's
  pinning. The waiting setup runner writes about every 25 s and metrics every 30 s. The hosting-root
  receipt and the 8ffc5e06/94be6b6e changes are ruled out by code reading. Also recorded: the v1
  sidecar's read-only inspection overlapped this window.
- **Evidence:** `upd1-debian13-good/run-a/`, and `side/query-*.txt` there.

**F2 (D-022 owner independence; D-024).** In the real-start pause the certificate renewal timer is
left stopped.

- On Debian, `certbot.timer` went from active/enabled before the update to inactive/enabled at the
  pause (`upd1-debian13-realstart/run-a/steps/13-terminal/step.json`).
- update.sh quiesces the Certbot scheduler during the transaction (update.sh:3534) and restores it
  only after completion is durably removed (update.sh:3980-3986). A paused forward completion
  therefore stops renewal until the owner's retry succeeds.
- Neither the paused CLI text nor the web text says so.
- Arch could not show it: `certbot-renew.timer` was already disabled before the update, because
  setup waits before its certificate step.

**F3 (D-024 wording, both real-start cells).** Between automatic forward attempts the status
briefly reads `recovery_required / recovery_failed` (d13-rs 20:00:18, arch-rs 20:47:11). The CLI
then says: "Automatic recovery could not finish this update, so the server may be between versions.
The server owner must act: read the recorded reason ... Keep the server's files as they are and do
not start another update."

- The next automatic attempt followed about 24 s later.
- From the first forward failure until the pause, the typed `panel_start_unverified` text is no
  longer shown: `failure_code` drops with `previous_failure=recovery_failed`. This is the "possible
  finding" the README anticipated.
- At the pause, `first_failure_code` brings the cause back.

**O6 (observation).** The rolled-back card for D keeps the raw English server summary as its
secondary line in TR, and it still reads `state=recovery_required`. The primary texts are correct.

**Confirmed corrections from upd2:**

- P3: the Arch hosting root.
- O1: the card now follows the recovery record.
- O3: the CLI speaks plainly; the rollback journal names the commit.
- O4: the guard says "held".
- The card offers the "already failed" notice before Start.

## Arch hosting root

Each Arch cell has six or seven light probes before the first site, every 30 s during setup
(`side/inspect-before-site-*`). They show `/var` 755 root:root and `/var/www` absent. Setup does
not create it.

At the first site the Agent logged "hosting root: created /var/www at 0755 0:0" and "... created
/var/www/celikpanel at 0755 0:0" (20:10:59, 20:20:58, 20:31:33, 20:42:56).

`stat` after the site shows `/var/www` 755 root:root and `/var/www/celikpanel` 755 root:root.
`namei -l` of the site path:

```
drwxr-xr-x root root / var / www / celikpanel
drwxr-xr-x root celikpanel subscriptions
drwxr-x--x root celikpanel 2 / sites
drwxr-x--- upd1_owner_test http 1
drwxr-s--- upd1_owner_test http public_html
```

- **Receipt:** `/var/lib/celikpanel-agent-private/hosting-root-v1.json` (0600 root:root), schema
  `celikpanel/hosting-root-directories/v1`, lists both paths with mode 0755 and uid/gid 0.
- **Site and cron:** `Host: upd1-owner.test` answered 200 with the marker. `crond` ran the owner's
  job every minute, and the stamp advanced through every cell (`hosting-root-summary.txt`).
- **Debian:** `/var/www` already existed (755 root:root), and only `/var/www/celikpanel` was created
  and recorded.
- **Not exercised:** the 409 `HOSTING_ROOT_NOT_TRAVERSABLE` refusal and the plan-review blocker.
  No blocking parent existed.

## Real origin

- `celikpanel.net` resolved only to 127.0.0.1, and the fixture answered 200:
  - after provisioning, in every cell;
  - after the owner restart the Arch installer demands;
  - before arm (`origin-check.txt`).
- The installer logs name neither `celikpanel.net` nor `185.95.`.
- The only journal lines naming the name are the fixture unit's start and stop lines
  (`secret-scan.txt`).
- The licence step activated the D-027 fixture with `license_service: not contacted` in all eight
  cells.

## Secret scan

Run over the whole folder before hashing (`secret-scan.txt`). **Clean:**

- 0 PEM private-key blocks.
- 0 hits for the 457 body lines of 32 key files (each lab's SSH key, fixture signing key, fixture CA
  key and fixture TLS key).
- 0 hits for the fixture licence literal, and 0 `CPK-` keys.
- 0 unredacted password, secret, token, CSRF, session or cookie fields.
- 0 tokens of the mailbox-password shape.
- The 7 tokens of the admin-password shape are side-inspection file names
  (`inspect-after-continuation-<stamp>`).
- 1790 `[REDACTED]` markers.

## Files

- `build/`: artifacts document, dist JSONs, prove, dry runs, build logs, offline tests and fixture
  commits.
- `harness-run-copy/`: H8 diff, run-copy hashes (both copies), H8 offline test, job scripts.
- `observer-tools/`: sidecar v1 and v2, inspection, extraction, queries and scan.
- `<cell>/run-a/`: the driver's evidence directory unchanged (its `SHA256SUMS` verified when
  staged).
  - `host/`: wrapper and sidecar logs, harness used, lab identity, owner-start and reboot attempts,
    observer and recovery-fault collections, baseline installer result and log, fixture plan.
  - `side/`: read-only inspections, the queries of cell 1 and `extract.txt`.
- `summary-per-cell.txt`, `outage-windows.txt`, `hosting-root-summary.txt`, `origin-check.txt`,
  `secret-scan.txt`, and `SHA256SUMS` over every file except itself.

## Host leftovers (archlinux WSL)

- **`/var/tmp/cp-upd1-build/20260930t174023z`** (596 MB): the clone and archives, written by the
  harness's build script.
- **`/var/tmp/cp-pair-accept/dist/{58813f1d...,901593ab...,7d11ff65...,592fc7ba...,36c5c252...}-acceptance-license`:**
  new directories written by `build-dist.sh`. Nothing that existed there was modified.
- **`/var/tmp/cp-upd3-run`:** run copies `harness` and `harness-h8`, logs, jobs, tools, side
  inspections and the stage.
- **Stopped labs `/var/tmp/cp-release-drill-upd3-{d13-good-a,d13-def-a,d13-sc-a,d13-rs-a,arch-good-a,arch-def-a,arch-sc-a,arch-rs-a}`**
  (2.5-2.7 GB each): overlays, per-lab fixture keys and serial logs.
- **Caches:** the build used the Go build cache and npm cache in root's home.

No QEMU process is running. Nothing else under `/var/tmp` or `/root` was deleted or modified. The
repository was not written except this folder: the harness ran from the run copy, so there is no
`__pycache__` in the working tree.

**Process deviation:** three inline `wsl.exe` commands contained `$` against the host rule:
- a `sed` of a scratch script;
- a failed read-only `python3 -c`;
- a read-only listing.

None changed guest or lab state.

## What this run proves and does not prove

It shows, on disposable guests with the acceptance fixture:

- **The start check runs in the native order on both platforms.** S failed after the database
  publication and before `completion.pending`, with the fixture's `tls_pair_invalid`. The
  automatic rollback reversed the published database to the pre-update digests, through a reset
  (Debian) and a kill at `runtime_verified` (Arch).
- **The start check passes a good candidate:** G on Arch, and R, sharing G's check path, on both
  platforms.
- **The real start fails forward.** R went forward three times and paused. The retry command was
  printed, and the typed panel-log guidance appeared in the CLI in EN and TR. Site and cron kept
  running (Debian mail too) while the Panel stayed down.
- **The owner-facing texts of 94be6b6e are native:** the rolled-back card, the "already failed"
  notice, the plain CLI with its support line, the guard "held" line and the restored commit in the
  rollback journal.
- **The Arch hosting root is created and recorded.** The site and cron work on Arch.

It does **not** show:

- a good-candidate update on Debian 13 in this run (F1). Its cause is unknown and not reproduced;
- recovery from the real-start pause (the retry was not run by design);
- the 409 hosting-root refusal or the plan-review blocker;
- that the start check catches every start failure;
- mail on Arch, DNS continuity (external mode) or an nginx TLS listener next to the fixture on 443;
- production signing, browser rendering, power loss, external DNS or mail delivery, or certificate
  issuance or renewal execution.

It closes no P0 row. Wall time: 17:39Z to about 21:05Z (cell 1 alone took 100 min, 90 of them the
unchanged track deadline).
