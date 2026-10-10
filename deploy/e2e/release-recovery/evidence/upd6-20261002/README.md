# upd6 native run, 2026-10-01 (folder dated for the batch 2026-10-02)

Roadmap item 3: the owner-started update acceptance driver (`owner_update_trial.py`), sixth run. **One
cell**: `upd1-arch-owner-continuation`, the cell that upd5 did not complete. Disposable QEMU/KVM guests on
the local `archlinux` WSL host only. Product and harness commit `6cda60b8`; the repository HEAD `b2143b00`
differs from it only under `evidence/` (`git diff 6cda60b8 b2143b00` outside `evidence/` is empty).

**Result in one sentence:** the owner continuation on Arch ran from start to finish. The held port made the
good candidate's real start fail. Forward completion ran three times and paused. The owner read the panel
log, released the port and ran the printed retry once. The same request ended `succeeded/update_verified`
(`recovered-after-owner-continuation`). The cell's `overall` is `complete-for-review`, and the owner-continuation
judge returned `as-expected` on all 17 of its rules, with no findings and no unknowns.

Nothing here passes a P0 row. `result.json` carries `native_evidence: false`. The owner judges the P0 rows.

No installed server (Boston, Frankfurt, any) was touched. Nothing was pushed, published or signed with a
production key. The candidates were served only by the guest-loopback fixture origin (see "Real origin and
licence").

## Disk safety (upd5 incident)

Free space was read on the Windows `C:` drive with PowerShell `Get-PSDrive C`, not inside WSL
(`host/c-drive.txt`). The floor was 40 GB to start, and the run would have stopped below 25 GB.

| When (UTC) | C: free |
| --- | --- |
| 09:27:05 before the run | 176.2 GB |
| 09:31:38 before the cell | 172.4 GB |
| 09:38:40 during the cell | 170.0 GB |
| 09:47:10 after the cell | 166.7 GB |
| 09:48:36 before staging | 165.8 GB |

The run used about 10.4 GB of `C:`, including the build and the lab overlay.

## Build and proofs

`run-upd1.sh build 6cda60b8` ran from the run copy, 09:28:30Z to 09:31:15Z, and exited 0
(`build/build.*`). The clone is `/var/tmp/cp-upd1-build/20261001t092830z/repo`, built with `go1.26.5 linux/amd64`. The fixture commits are new
(they are commits in a new clone), so their hashes differ from upd5. The trees of B and of the source
equal upd5's.

| Role | Version | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B | v0.1.0-alpha.81 | d0676ba398836721884c6d586be9ff86c14056ac | d0042a336ae464b2cf641c28d0ea8b0cc08e87d8 | 2ee4a92be52d9101931b06866e8a2e93dd6ac4e63b9cedef23f6d9fe68cf3a32 |
| Good G | v0.1.0-alpha.82 (parent B) | 8b89ef29a9e931a6ff2b8e20f68a4fc3c4ac90be | 10f7b822a2a6f7d9aa2e53b37ce26ba6f3e5dc1b | f076c7c9f4aa0335bcfd59e41f7c87aef17f36ea1b6a6456fe15cc2856c360d0 |
| Defective D | v0.1.0-alpha.82 (parent G) | a3e59bbfd25d8f8470212ed6e68875f5f1a5420c | cb7bd7f9620817b0f460ebd41e66ce80b7a82e8c | 4ee0702d48cbd58e2b93446b96ad4737739ae5897f2bbc0e1d0f926382fa5e8a |
| Start-check S | v0.1.0-alpha.82 (parent G) | a1e3066c5eace35651ea2d01c2aaeca8eda08642 | 355c276268c25b8787539c5029f74635d960485e | 9352125e7aabea7e10456e38a9a26cffded14430993bdc37d142aa81e24e04b6 |
| Real-start R | v0.1.0-alpha.82 (parent G) | 0dd5e73c80be18e7adc3b81c84a0d434fc0b0224 | d81d829456f4fd2e3adefdfa7ced9adb45665b28 | b575833e97bf44b0bbd664fa6e0709d3a8b8066510f6f9aff8252497c706f93a |

- Source HEAD 6cda60b87e8144ae33aaa343d04a9c3923d8755e (`build/source-commit.txt`). The fixture commits and
  their patches are in `build/fixture-commits*.txt` and `build/fixture-kind-patches.diff`.
- All five archives are `license_mode: acceptance-fixture` (`build/dist-*.json`). This cell used only B and G.
- `prove` exited 0 (`build/prove.*`). The dry run of the cell exited 0 with `native_evidence: false`, and its
  port-hold bound was 1080 s with `RuntimeMaxSec` 1140 s (`build/dry-run-upd1-arch-owner-continuation.*`). The dry run
  created no lab.
- Offline suites on the run copy: `test_owner_update_trial.py` ran 161 tests OK and
  `test_recovery_candidate_archive.py` ran 11 tests OK (`build/offline-tests-*-6cda60b8.txt`).

## Commands

All commands ran as root on the `archlinux` WSL host. The job scripts are in `harness-run-copy/` and
`tools/`.

```sh
# run copy (never the working tree), PYTHONDONTWRITEBYTECODE=1 everywhere
git -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' archive 6cda60b8 | tar -x -C /var/tmp/cp-upd6-run/harness
bash /var/tmp/cp-upd6-run/harness/deploy/e2e/release-recovery/run-upd1.sh build 6cda60b8
ART=/var/tmp/cp-upd1-build/20261001t092830z/upd1-artifacts.json
bash .../run-upd1.sh prove "$ART"
bash .../run-upd1.sh dry-run upd1-arch-owner-continuation "$ART" upd6-dry
bash .../run-upd1.sh cell upd1-arch-owner-continuation "$ART" upd6-arch-oc-a 2611
```

## Harness

The harness ran from `git archive 6cda60b8` at `/var/tmp/cp-upd6-run/harness`. The file hashes are in
`harness-run-copy/runcopy-6cda60b8-files.sha256` (30015 files). The repository was not edited.

**There is no run-copy diff.** The baseline install passed unchanged, so the H16 copy was not needed and was
not made. The conditions of upd5's failed read were present again. The Arch prerequisite step upgraded the
kernel and printed the "RESTART THIS SERVER NOW" notice while the driver polled the status every 15 s, and
no status read failed. One run does not settle H16. It is consistent with the upd5 exit-127 read being a
symptom of the failing host disk, but it does not prove that.

## The cell

Lab `upd6-arch-oc-a`, SSH port 2611, request `64a87b68438919b32870d6ccc1bf3d0e`. The wrapper ran from
09:31:42Z to 09:47:02Z (15 min 20 s) and exited 0. The driver ran from 09:32:41Z to 09:47:02Z.

| # | Step | Verdict | UTC |
| --- | --- | --- | --- |
| 1 | preflight | passed | 09:32:41-09:32:45 |
| 2 | origin | passed | 09:32:45-09:32:53 |
| 3 | baseline-install | passed (owner restart demanded by the installer, 41.6 s) | 09:32:53-09:34:52 |
| 4 | owner-login | passed | 09:34:52-09:34:53 |
| 5 | license | passed (acceptance fixture; licence service "not contacted") | 09:34:53 |
| 6 | setup | observed (isolated-host wait at `access_dns`; `web_mail` refused on Arch) | 09:34:53-09:37:39 |
| 7 | seed | passed (site, cron; no mail on Arch) | 09:37:39-09:37:42 |
| 8 | pre-state | passed | 09:37:42-09:39:10 |
| 9 | arm | passed | 09:39:10-09:39:12 |
| 10 | owner-start | passed | 09:39:12 |
| 11 | track | observed (stopped at the pause) | 09:39:12-09:46:02 |
| 12 | owner-continuation (required) | observed | 09:46:02-09:46:50 |
| 13 | track-after-owner-continuation | passed | 09:46:50-09:46:52 |
| 14 | terminal | passed | 09:46:52-09:46:59 |
| 15 | collect | passed | 09:46:59-09:47:02 |
| 16 | verdicts | passed | 09:47:02 |
| 17 | kind-expectation | passed (`as-expected`, 17 rules, 0 findings, 0 unknown) | 09:47:02 |

The driver recorded three findings, all known platform or scope facts:

- `web_mail` was refused (`server_setup_service_unsupported:dovecot`), so mail is not provided on Arch.
- Setup waits at `access_dns` (`server_setup_access_dns_required`), so `09-panel_certificate` and `10-verify`
  were not run.
- While the retry budget was used up, the Panel recovery-status API was unavailable, so only the root CLI showed
  the exhausted state.

### The owner continuation, end to end

All times are UTC and come from the journals, the observer, the hold events and the dispatch receipts.

| Time | Event |
| --- | --- |
| 09:39:12.8 | The owner starts the update. The observer and the port hold are armed (hold bound 1080 s). |
| 09:39:29 | Snapshot `20261001T093929Z-from-unknown-to-8b89ef29a9…-d552c18c…` is taken. |
| 09:39:31.4 | `port_held`: after the updater stopped the old Panel, the hold took `127.0.0.1:2083`. The transaction phase is `active`. |
| 09:39:50.0 | `completion.pending` is seen by the observer and by the hold. The start check passed, and the candidate's real start is next. |
| 09:39:51 to 09:40:29 | The candidate Panel fails 5 times with `Failed to start panel listener: listen tcp :2083: bind: address already in use`. |
| 09:40:52.2 | The update's failure line: `!! CELIKPANEL_UPDATE_FAILURE code=panel_start_unverified state=recovery_required reason=the new panel did not stay running and answer on its own address after the update was applied`. |
| 09:40:53.3 | Automatic attempt 1: dispatch `update/completion` (forward). It fails at 09:42:01. |
| 09:41:49 | The CLI shows `retry_scheduled`. |
| 09:42:32.7 | Automatic attempt 2 (forward). It fails at 09:43:40. The CLI shows `retry_scheduled` again at 09:43:39. |
| 09:44:11.5 | Automatic attempt 3 (forward). It fails at 09:45:19. The runner logs that Certbot "is already in its state from before the update". |
| 09:45:11 to 09:45:28 | The `pause_pending` window. Its texts are below; none says the owner must act. |
| 09:45:50.9 | The pause, 379.4 s after the hold: `Automatic recovery paused after three admitted attempts…` and `After resolving the cause, the owner may authorize one same-snapshot retry: sudo /usr/libexec/celikpanel/recovery recover --retry --snapshot 20261001T093929Z-from-unknown-to-8b89ef29a9e931a6ff2b8e20f68a4fc3c4ac90be-d552c18c0461df125803f4f101bb59d0`. Status: `recovery_required/recovery_incomplete`, `automatic_recovery=paused_retry_limit`, `first_failure_code=panel_start_unverified`, `renewal_before_update=off`. |
| at the pause | The panel log that the product names (`sudo journalctl -u celikpanel-panel -n 50`) holds five `bind: address already in use` lines (`names_cause: true`). The hold is still held. Panel unit: `NRestarts=4`, `Result=exit-code`, inactive. The journal shows 20 failed Panel starts between 09:39:51 and 09:44:57, below the unit's `StartLimitBurst=30`. |
| 09:46:23.7 | The printed retry is read from the recovery journal and validated, not run (`validated-not-executed`, journal SHA-256 `011cd401…`). |
| 09:46:24.2 | The owner releases the port with `systemctl stop celikpanel-lab-owner-port-hold-<request>.service`, rc 0. Recorded: `owner-released`, held 412.8 s, `released_before_retry: true`. |
| 09:46:26.4 | The owner runs the printed command once (`executed-once`). Owner receipt `owner.XUuHQN0A` at 09:46:27.3, `update/completion`, forward. |
| 09:46:51.3 | The retry exits 0: `Recovery dispatch admitted: attempt=owner …`, `CELIKPANEL_UPDATE_CHECKPOINT database_verified_before_start`, `==> Previous pending update finalized from verified snapshot: …` / `==> Önceki bekleyen güncelleme doğrulanmış snapshot'tan tamamlandı: …`. |
| 09:46:51 | The same request reads `succeeded/update_verified`, with `previous_failure=recovery_failed`, in all three views. |

Terminal (09:46:52 to 09:46:59, passed):

- G is installed and running (`build_identity_ok`). The floor and foundation are at sequence 82, with commit `8b89ef29`.
  No transaction marker remains.
- The database equals the pre-update state except the listed volatile tables (`audit_logs`, `metrics_samples`,
  `server_setup_executions`, `sessions`, `sqlite_sequence`), with no unexpected difference.
- Timers: 2 compared, equal. `certbot-renew.timer` was disabled and inactive before the update and still is, and
  `celikpanel-release-recovery.timer` is active.
- The firewall ruleset is equal. The site marker is present. A fresh login works. The seeded domain and cron rows
  are present, and mail is not seeded (Arch).
- The update card is `succeeded`, with keys `panelUpdate.succeeded` and no missing key. The card judge returned `as-expected`.

Workloads (verdicts, passed):

- site HTTP: never interrupted;
- cron: never interrupted;
- SMTP: not seeded (Arch);
- DNS: `not-provided-external-dns`;
- Panel: down only during the transaction. The guest window was 421.7 to 431.7 s, and the host tunnel window
  was 418.3 to 428.3 s. SSH was never down.

Three-view agreement: `passed`. Of 28 samples, 6 agreed and 0 were start-instant lag. The other 22 were
`single-source`, because the Panel API was down and only the root CLI answered. Clock skew was 0.762 s.

The `renewal_before_update` record is `off`, which matches the pre-update timers (`as-before`). The `<id>.renewal`
sidecar is present and valid with `off`.

### The "Recovery failed" label after success

The label `Recovery failed` / `Kurtarma başarısız oldu` appears only in step 12 (the
`Previously recorded failure` line of the paused screen and CLI guidance, 1 EN and 1 TR). It appears in no
record taken after the success. Track-after, terminal and verdicts have none, and the
recovery screen after success renders only `recovery.phase.succeeded` and `recovery.next.succeeded`. The retry's output
has no `!!` failure line. After success, the CLI's support line still carries the field
`previous_failure=recovery_failed`, which is a raw state field, not a label or a failure line.

### Guidance texts (verbatim; root CLI, first time seen)

The `Request`/`Recorded at` lines and the trailing `This is a recorded observation; current service health was
not checked.` / `Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi.` line are left out
here. They are in `side/extract.txt` and the sample files.

**running (09:39:12)**
- EN: The update is being applied; the panel may be unavailable for a short time. Nothing to do now: check this same request again in a minute. Checking does not restart the update.
- TR: Güncelleme uygulanıyor; panel kısa bir süre erişilemeyebilir. Şimdi yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın. Sorgulamak güncellemeyi yeniden başlatmaz.

**recovering, failure_code=panel_start_unverified (09:40:56; the same text again at 09:42:24 with first_failure_code)**
- EN: The update was applied, but the new version's panel did not come up. The server owner should read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. The update's completion is retried automatically up to its limit; after that, sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 shows a one-time retry command for this operation. There is no supported return to the previous version from this point.
- TR: Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı. Sunucu sahibi sunucudaki panel günlüğünü okumalı: sudo journalctl -u celikpanel-panel -n 50. Güncellemenin tamamlanması sınırına kadar otomatik olarak yeniden denenir; sonrasında sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 bu işlem için tek seferlik yeniden deneme komutunu gösterir. Bu noktadan önceki sürüme desteklenen bir dönüş yok.

**retry_scheduled (09:41:49)**: the recovering text, followed by:
- EN: The last automatic recovery attempt did not finish, and automatic recovery tries this same operation again by itself: the next attempt normally starts about 30 seconds after the previous one ended, up to three attempts in total. Nothing is needed on the server now: check this same request again in a minute and do not start another update. The server owner has to act only if automatic recovery pauses.
- TR: Son otomatik kurtarma denemesi tamamlanmadı ve otomatik kurtarma aynı işlemi kendiliğinden yeniden deniyor: sonraki deneme normalde bir öncekinin bitişinden yaklaşık 30 saniye sonra başlar, toplamda en çok üç deneme yapılır. Şimdi sunucuda yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın ve başka güncelleme başlatmayın. Sunucu sahibinin ancak otomatik kurtarma durursa işlem yapması gerekir.

**pause_pending (09:45:11)**: the recovering text, followed by:
- EN: The last admitted recovery attempt did not finish, and no automatic attempt remains. Recovery is finishing this attempt: the next state, with what the server owner needs to do and the one-time retry command, is recorded within about a minute. Nothing to do yet: check this same request again in a minute and do not start another update.
- TR: İzin verilen son kurtarma denemesi tamamlanmadı ve başka otomatik deneme kalmadı. Kurtarma bu denemeyi kapatıyor: sunucu sahibinin ne yapacağını ve tek seferlik yeniden deneme komutunu içeren sonraki durum yaklaşık bir dakika içinde kaydedilir. Henüz yapılacak bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın ve başka güncelleme başlatmayın.

**paused_retry_limit (09:45:46; recorded 09:45:50)**
- EN: The update was applied, but the new version's panel did not come up, and completing the update was retried to its limit. Read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed. There is no supported return to the previous version from this point. Automatic recovery used all three attempts without finishing, so the server may be between versions. The server owner must act: read sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50, fix the cause it names, then run the one-time same-operation retry command shown there. Checking status does not retry recovery. Automatic certificate renewal (Certbot) was already off before this update, so the update did not stop it; check whether it should be on.
- TR: Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve güncellemenin tamamlanması sınırına kadar yeniden denendi. Sunucudaki panel günlüğünü okuyun: sudo journalctl -u celikpanel-panel -n 50. Bu neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu noktadan önceki sürüme desteklenen bir dönüş yok. Otomatik kurtarma üç denemenin hepsini kullandı ve tamamlanamadı; sunucu iki sürüm arasında kalmış olabilir. Sunucu sahibinin işlem yapması gerekiyor: sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 çıktısını okuyun, belirtilen nedeni giderin ve orada aynı işlem için gösterilen tek seferlik yeniden deneme komutunu çalıştırın. Durum sorgusu kurtarmayı yeniden başlatmaz. Otomatik sertifika yenileme (Certbot) bu güncellemeden önce zaten kapalıydı, bu yüzden güncelleme onu durdurmadı; açık olması gerekip gerekmediğini kontrol edin.

**succeeded/update_verified (09:46:50)**
- EN: The update completed and the new version was verified when it finished. Nothing else is needed on the server. Open the panel to check that everything works now.
- TR: Güncelleme tamamlandı ve yeni sürüm bittiği anda doğrulandı. Sunucuda başka bir işlem gerekmiyor. Her şeyin şu an çalıştığını görmek için paneli açın.

**Recovery screen modelled at the pause** (rendered from the build's `RecoveryAccess.tsx`; the Panel was down, so
no browser could show it). Keys: `recovery.automatic.pausedTitle`, `.cause.panel_start_unverified`, `.pausedHelp`,
`.renewalOff`, `.inspect`, `.resume`, `recovery.previousFailure`, `recovery.reason.recovery_failed`. No key is missing.
- EN: Last recorded state: automatic recovery paused / The update was applied, but the new version's panel did not come up, and completing the update was retried to its limit. The server owner should read the panel log on the server with sudo journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed. There is no supported return to the previous version from this point. / All three automatic recovery attempts were used without a verified result. Automatic recovery will not start another attempt. The server owner must act. / Certbot renewal was already off before this update; the update did not stop it. / On this server, open a terminal and inspect the recovery log: / sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 / Resolve the reported cause, then use the one-time retry command shown in that log for this operation. It continues the existing recovery. This page checks for its result; checking or reloading does not restart recovery. / Previously recorded failure: Recovery failed
- TR: Son kayıt: otomatik kurtarma durdu / Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve güncellemenin tamamlanması sınırına kadar yeniden denendi. Sunucu sahibi sunucudaki panel günlüğünü sudo journalctl -u celikpanel-panel -n 50 ile okumalı. Bu neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu noktadan önceki sürüme desteklenen bir dönüş yok. / Üç otomatik kurtarma denemesi de doğrulanmış sonuç alınamadan kullanıldı. Otomatik kurtarma yeni deneme başlatmayacak. Sunucu sahibinin işlem yapması gerekiyor. / Certbot yenilemesi bu güncellemeden önce zaten kapalıydı; güncelleme onu durdurmadı. / Bu sunucuda terminal açıp kurtarma günlüğünü inceleyin: / sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 / Bildirilen nedeni giderdikten sonra günlükte bu işlem için gösterilen tek seferlik yeniden deneme komutunu kullanın. Komut mevcut kurtarmayı sürdürür. Bu sayfa sonucu kontrol eder; kontrol etmek veya sayfayı yenilemek kurtarmayı yeniden başlatmaz. / Önceden kaydedilmiş hata: Kurtarma başarısız oldu

The modelled `retry_scheduled` and `pause_pending` screens, the offline page texts and the update card texts
(queued, running, succeeded) are in `side/extract.txt` and in step 12 (`offline-shell-paused.json`). After
success, the screen reads `Update verified` / `Güncelleme doğrulandı` and `The server verified this update. Reload CelikPanel to load the installed interface.` /
`Sunucu bu güncellemeyi doğruladı. Kurulu arayüzü yüklemek için CelikPanel’i yeniden yükleyin.`, and the card
reads `The update completed. The panel and agent restarted on the new version.` / `Güncelleme tamamlandı. Panel
ve agent yeni sürümle yeniden başladı.` The card shows its settled text and its fallback, which are the same line.

## Real origin and licence

- The baseline installer log names neither `celikpanel.net` nor `185.95.` (0 and 0, `secret-scan.txt`).
- The origin checks after provisioning, after the owner restart and before `arm` all resolved `celikpanel.net`
  only to `127.0.0.1` with fixture HTTP 200 (`result.json` `scope.origin`).
- The only journal lines that name the domain are the fixture origin unit's start and stop lines.
- The licence step records `license_service: not contacted: acceptance test build`. Every candidate Panel start
  logs `ACCEPTANCE FIXTURE — NOT FOR PRODUCTION: … it never contacts the license service …`.

## Secret scan

The scan used `tools/scan6.py` (upd4's `scan4.py` with the lab glob set to upd6, as in upd5) and `tools/scan.sh`.
It covered the whole folder before hashing (`secret-scan.txt`), and the result is **clean**:

- 0 PEM private-key blocks, and 0 hits for the 58 body lines of the 4 key files (the lab SSH key and the fixture
  signing, CA and TLS keys).
- 0 hits for the fixture licence literal, and 0 `CPK-` keys.
- 0 unredacted password, secret, token, session or cookie fields. There are 125 redaction markers.
- 8 tokens have the admin-password shape. One is the start of the lab's SSH public key in
  `host/fixture-plan.json` (public `authorized_keys` material). Seven are upd4 file names
  (`inspect-after-continuation-20260930T…`) in the run-copy hash list. There are 0 tokens of the
  32-character shape.
- `185.95.` appears only in the scan scripts and the scan output.
- The longest path from the repository root is 192 characters.

## Files

- `build/`: the artifacts document, dist JSONs, prove, dry run, build and offline logs, and fixture commits.
- `harness-run-copy/`: the run-copy hashes (6cda60b8), the cell job, and the scripts that made the run copy, the build and
  the job.
- `tools/`: host-only readers, wait and peek scripts, `stage6.sh` (built this folder), `ext6.py`
  (`side/extract.txt`) and the scan scripts.
- `host/`: the pre-run host check, `C:` readings, WSL-internal disk readings and host leftovers.
- `upd1-arch-owner-continuation/run-a/`: the driver's evidence directory, unchanged (its `SHA256SUMS`, 184
  entries, was verified after copying); `host/` (wrapper logs, job and harness used, lab identity, baseline
  installer result and log, worker-origin intent and manifest, fixture plan); `side/extract.txt`.
- `secret-scan.txt`, and `SHA256SUMS` over every file except itself.

## Host leftovers (archlinux WSL)

These are listed in `host/host-leftovers.txt`.

Created by this run:

- `/var/tmp/cp-upd6-run` (337 MB): the run copy, logs, the job and build records.
- `/var/tmp/cp-release-drill-upd6-arch-oc-a` (2.5 GB): the stopped lab, with its overlays and evidence.
- `/var/tmp/cp-upd1-build/20261001t092830z` (671 MB): the clone and the archives.
- `/var/tmp/cp-pair-accept/dist/{d0676ba3…,8b89ef29…,a3e59bbf…,a1e3066c…,0dd5e73c…}-acceptance-license`
  (657 MB each): the build outputs for B, G, D, S and R.

Not created by this run: `/root/cp-prune` appeared at 09:38:11Z. It belongs to a separate concurrent job
(`scratchpad/prune/runall.sh`), which was still running on the host at 09:48Z. This run neither made it nor
touched it. Host CPU and disk were shared with that job during the cell.

The guests were stopped by the wrapper (no QEMU process afterwards; `lab.py status` reports the identity
unavailable). The overlays are kept. Nothing under `/var/tmp` or `/root` was deleted. The repository's git
configuration is unchanged, and the only repository write is this folder.

## Deviations

- One cell only, as instructed. There was no H16 copy and no re-run, because the baseline install passed unchanged.
- This folder was staged directly from the stopped lab into the repository (`tools/stage6.sh`), without a
  separate host stage directory.

## What this run proves and does not prove

It shows, on a disposable Arch guest with the acceptance fixture at 6cda60b8, the owner path that upd5
measured only on Debian:

- the held port kept through the update's exit and all three forward attempts;
- `retry_scheduled` between the attempts, and a `pause_pending` window with no "must act";
- the pause on the typed first cause, with Certbot's "already off" renewal sentence matching the pre-update timers;
- the panel log the text names showing the cause;
- the owner releasing the port and running the printed retry exactly once;
- the same request completing forward to `update_verified`, with timers, firewall and database unchanged
  (apart from the listed exclusions), and site and cron never interrupted.

Together with upd5, the owner continuation is now measured on both platforms.

It does **not** show:

- a cause the owner cannot fix, or a Panel that fails because of a product defect;
- recovery after a second fault during the retry;
- browser rendering: the Panel was down at the pause, so the card and screen are modelled from the build's source, not seen;
- mail on Arch, DNS (external mode), certificate issuance or renewal execution, power loss, production signing;
- the Panel's start limit being reached: 20 starts in this run, against a limit of 30 per hour;
- whether H16 was a harness defect: one passing run is consistent with the disk explanation but does not settle it;
- the live-probe re-read, the typed preflight path, the snapshot cause, or the server line on a failed card;
- the defective and start-check kinds, or management off and reboot.

It closes no P0 row.

## Corrections after intake (2026-10-10)

After this directory was published (2026-10-10), the operator's machine name was replaced by `<operator-host>` in the SSH public-key comments (and in the text that named it) of this directory: 2 occurrences in 1 file; the key material itself, a lab public key, is unchanged.
After this directory was published (2026-10-10), 1 digest value of the one-shot update-transaction token (1 in plain text in 1 file, as `transaction_token_sha256` values and as `.release-db-migrations/<digest>` directory names; 0 inside base64 `events_base64` text in 0 files) were replaced by `[REDACTED-SHA256]`.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.
