# upd4 native run, 2026-09-30/10-01 (folder dated for the item 3 batch 2026-10-01)

Roadmap item 3: the owner-started update acceptance driver (`owner_update_trial.py`), fourth attempt,
twelve cells. Disposable QEMU/KVM guests on the local `archlinux` WSL host only. Product and harness
commit `a6dd5b1e`. This is the first native measurement of `a6dd5b1e` (preflight cause kept, compatibility
re-read, Certbot restored at a forward pause, `retry_scheduled` texts, Turkish card, `reset-failed` before
every controlled start) and of the upd4 harness (`4a581ea7`: H8, H9, H10, sidecar v2, the owner-continuation
and management-off cells).

**Result in one sentence: ten of twelve cells reached their expected end; both real-start cells did not,
because their updates failed before the candidate ran, each on a database-consistency step that is not
part of the fixture (F4 Debian, F5 Arch).**

- Good candidate G: verified forward on Debian 13 (62 s) and Arch (49 s). The upd3 Debian preflight stop
  (F1) did not recur in this cell.
- Owner continuation (new): on both platforms the held port made the candidate Panel fail its stability
  wait, forward completion was retried 3 times and paused, the panel log named the port conflict, the owner
  released the port and ran the printed one-time retry once, and the same request ended
  `succeeded/update_verified` (`recovered-after-owner-continuation`). Site, SMTP (Debian) and cron were never
  interrupted. Debian `certbot.timer` was back to active/enabled at the pause and after (F2 corrected).
- Management off and one orderly reboot (new): with the Panel and Agent disabled and stopped, the site,
  SMTP (Debian), cron, the owner's MariaDB row, the renewal timer and the firewall ruleset were all served or
  kept for 180-185 s; management returned to the same owner state. This needed harness corrections H12/H13
  (Debian, one re-run) and H14 (Arch, one re-run); both first runs are kept.
- Start-check candidate S and migrate-only candidate D: automatic rollback on both platforms, 2 attempts
  each, with the second fault (Debian QMP reset at `payload_restored`, Arch SIGKILL at `runtime_verified`).
  The arch-startcheck H9 rule now passes.
- Real-start candidate R: **not measured.** Debian stopped in the update's preliminary WAL-aware idle probe
  (`-shm changed after pinning`, F4); Arch failed its online database snapshot after the freeze and rolled
  back (F5). Neither cell was re-run.

Nothing here passes a P0 row. Every `result.json` carries `native_evidence: false` (14 runs, 10 kind
judgements). The owner judges P0.1, P0.2, P0.3 and P0.5.

No installed server (Boston, Frankfurt, any) was touched. Nothing was pushed, published or signed with a
production key. The candidates were served only by the guest-loopback fixture origin; the real
`celikpanel.net` and license services were not contacted (see "Real origin").

## Build and proofs

`bash deploy/e2e/release-recovery/run-upd1.sh build a6dd5b1e` (from the run copy) ran 22:19:19Z to
22:22:50Z and exited 0 (`build/build.*`). Clone `/var/tmp/cp-upd1-build/20260930t221920z/repo`, web built
fresh, `go1.26.5 linux/amd64` (`build/go-version.txt`).

| Role | Label / seq | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B | v0.1.0-alpha.81 / 81 | 0e515e908fc7b3de5c4e74dc7535638cbe56d401 | 28158bce8cc55465912aa07e44cb1018ec1041ff | af263bd487cd3f4dc75cd7e28a7104fee76c9416c2887e3293a20754c37b03d6 |
| Good G | v0.1.0-alpha.82 / 82 (parent B) | e1fdca3898d950a6408bb5161f5b25ac32440d28 | 5cccb268a59d14692bc4cf82c8af248b97870d47 | 7adcd0dfb16eb437606c9597aa925fcb282e5576cbe330a5fe0d05e14e814892 |
| Defective D | v0.1.0-alpha.82 / 82 (parent G) | bd0e626ee8efae8bb11f3e4f8fa3cf0e7ff39df9 | 80f6cf0b82971e321d4022fd93fdcc5a6235e738 | cdd55db8b3ab6b0011811303b6ed355dcb414edcded8bec51b70be59052e6899 |
| Start-check S | v0.1.0-alpha.82 / 82 (parent G) | 96de9cdb6b48a8e5fa2cd02bd6cd9845579255f5 | 223f0147f0a2a71c3a5371e784a97c85fceb433f | e510c85ef966cf4357a7a285824939f40830a4187d3c4a2854d87fa6ed5d105c |
| Real-start R | v0.1.0-alpha.82 / 82 (parent G) | dd4adede7c24bd1ad92118612fd379beb7874b0b | 19ef5f80f6d0fe3244df0bdd403f5e2d857d11ce | fc34970a51de54501bfc9baeea9909c7bec94029e52e4c8bcf440a3059a639fd |

- Source HEAD a6dd5b1ec66c142d5b7be1b8181fe61fb23cfc70 (`build/source-commit.txt`); fixture commits and
  patches in `build/fixture-commits*.txt` and `build/fixture-kind-patches.diff`.
- All five archives `license_mode: acceptance-fixture` (`build/dist-*.json`).
- `prove` exited 0 for all five roles: 618 files each, 485 committed static files proved,
  `dns-owner-tools/` 4 files (`build/prove.json`).
- `dry-run` exited 0 for all twelve cells with `native_evidence: false`; no lab was created. The
  owner-continuation dry run shows the port-hold bound 1080 s / `RuntimeMaxSec` 1140 s
  (`build/dry-run-*.json`).
- Offline suites on the a6dd5b1e run copy: `test_owner_update_trial.py` 124/124,
  `test_recovery_candidate_archive.py` 11/11 (`build/offline-tests-*-a6dd5b1e.txt`).

## Run copies and harness corrections

The harness ran from `git archive a6dd5b1e` at `/var/tmp/cp-upd4-run/harness` (file hashes:
`harness-run-copy/runcopy-a6dd5b1e-files.sha256`). The repository was not edited; `PYTHONDONTWRITEBYTECODE=1`.

| Id | Defect | Kind | Handling | Cells |
| --- | --- | --- | --- | --- |
| H11 | The recovery-screen model (`recovery_guidance`, owner_update_trial.py:563-617) mirrors the JSX layout of 94be6b6e: it renders `retry_scheduled` as the paused screen and omits the `recovery.automatic.renewal` line at the pause. The product's `RecoveryAccess.tsx:49-60` (and the card, `systemUpdateOutcome.ts:140-160`) show "automatic recovery will try again" with the typed cause, and add the renewal line at the pause. | harness model lag (texts only) | Recorded, not changed, no re-run: no rule reads these lines (the CLI texts and catalogue keys are judged and passed). The product-rendered texts for both states are in `h11-product-screen-texts.txt` (rendered from the build's catalogues in the source order). In the cells, the `guidance_cli` lines at the retry_scheduled samples are the harness model, not the product. | owner-continuation (both) |
| H12 | `management_return` read the owner's Panel state right after login while the Panel still answered `503 PANEL_STARTING` ("Panel management is still starting"), so every compared field read as empty. | mechanical | Run copy `harness-h12`: wait (read-only, bounded 180 s) for `panel_state=ready` in the recovery status before reading (`harness-run-copy/H12-H13-owner_update_trial.py.diff`). Offline 124/124. | d13-mgmt-off: run-a (finding) kept; **run-b** re-run once with H12/H13 |
| H13 | The verdict cut for the update-only window used `management_off_at` taken after `systemctl disable --now` had returned; samples of the owner's own stop counted as a Panel outage outside the transaction. | mechanical | Same run copy: the instant is taken before the command (completion kept as `management_off_done_at`). | as H12; arch-mgmt-off run-a used it from the start |
| H14 | A guest sample's `t` is the start of the sampler cycle; the Panel probe runs last in the cycle (`guest_upd1_workload.py` `sample()`, after the site, DNS, SMTP and cron probes), about 2 s later on an external-DNS node. The cycle that started at 01:17:13.716 probed the Panel after the owner's stop (unit stopped 01:17:15.567, guest clock) and the cut counted it as `down-outside-transaction`. | mechanical | Run copy `harness-h14` (= h12 + H14): the cut ends one sample interval (5 s) before the stop request, guest and host series alike (`H14-owner_update_trial.py.diff`, cumulative `H12-H13-H14-cumulative-owner_update_trial.py.diff`). Offline 123/124: the only failure is `test_verdicts_judge_the_update_part_only`, which pins the old cut (expects 20 samples, gets 19) - the intended change (`offline-tests-owner-h14.txt`). | arch-mgmt-off: run-a (verdicts finding) kept; **run-b** re-run once with H14 |
| H15 | The raw port-hold events file (`owner-port-hold-<request>.jsonl`) stays on the guest's private root; the driver's evidence keeps only its summary (`hold_at_pause`, `release`, `outcome.port_hold`). | evidence gap | Recorded; the raw file remains in the stopped overlays of upd4-d13-oc-a and upd4-arch-oc-a. | owner-continuation (both) |

No other cell was re-run. The two real-start cells were not re-run: their stops are product failures
(F4, F5), not harness ones.

## Cells

One new lab per run, stopped by the wrapper. Order and SSH port bases:

| # | Cell | Lab | Port | Harness | Outcome | Overall |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | upd1-debian13-good | upd4-d13-good-a | 2371 | a6dd5b1e | `update-verified` | complete-for-review |
| 2 | upd1-debian13-defective | upd4-d13-def-a | 2361 | a6dd5b1e | `recovered-automatically`, 2 attempts | complete-for-review |
| 3 | upd1-debian13-startcheck | upd4-d13-sc-a | 2401 | a6dd5b1e | `recovered-automatically`, 2 attempts; kind as-expected | complete-for-review |
| 4 | upd1-debian13-realstart | upd4-d13-rs-a | 2421 | a6dd5b1e | `not-terminal`: preliminary idle probe refused, B still installed (F4); H8 stop after 606 s | failed |
| 5 | upd1-debian13-owner-continuation | upd4-d13-oc-a | 2481 | a6dd5b1e | `recovered-after-owner-continuation`, 3 + 1 owner; kind as-expected | complete-for-review |
| 6a | upd1-debian13-mgmt-off-reboot run-a | upd4-d13-mr-a | 2501 | a6dd5b1e | `update-verified`; workloads served; return F (H12), verdicts F (H13) | failed (harness) |
| 6b | upd1-debian13-mgmt-off-reboot run-b | upd4-d13-mr-b | 2521 | h12 | `update-verified`; kind as-expected | complete-for-review |
| 7 | upd1-arch-good | upd4-arch-good-a | 2391 | a6dd5b1e | `update-verified` | complete-for-review |
| 8 | upd1-arch-defective | upd4-arch-def-a | 2381 | a6dd5b1e | `recovered-automatically`, 2 attempts | complete-for-review |
| 9 | upd1-arch-startcheck | upd4-arch-sc-a | 2411 | a6dd5b1e | `recovered-automatically`, 2 attempts; kind as-expected | complete-for-review |
| 10 | upd1-arch-realstart | upd4-arch-rs-a | 2431 | a6dd5b1e | `real-start-candidate-rolled-back`: snapshot failed in active (F5), 1 attempt | failed |
| 11 | upd1-arch-owner-continuation | upd4-arch-oc-a | 2491 | a6dd5b1e | `recovered-after-owner-continuation`, 3 + 1 owner; kind as-expected | complete-for-review |
| 12a | upd1-arch-mgmt-off-reboot run-a | upd4-arch-mr-a | 2511 | h12 | `update-verified`; kind as-expected; verdicts F (H14) | failed (harness) |
| 12b | upd1-arch-mgmt-off-reboot run-b | upd4-arch-mr-b | 2531 | h14 | `update-verified`; kind as-expected | complete-for-review |

### Step table (UTC start-end; P passed, O observed, F failed, I inconclusive, S skipped, N not run, - not part of the cell)

| Step | d13-good | d13-def | d13-sc | d13-rs | d13-oc | d13-mr a | d13-mr b |
| --- | --- | --- | --- | --- | --- | --- | --- |
| preflight + origin | P 22:24:45-24:57 | P 22:36:56-37:09 | P 22:48:37-48:52 | P 23:01:55-02:08 | P 23:24:13-24:24 | P 23:41:25-41:37 | P 00:06:18-06:30 |
| baseline-install | P 49 s | P 49 s | P 49 s | P 64 s | P 34 s | P 34 s | P 33 s |
| login, license | P ("not contacted") | P | P | P | P | P | P |
| setup | O 22:25:47-32:14 | O 22:37:59-43:56 | O 22:49:42-56:09 | O 23:03:13-09:35 | O 23:24:59-30:51 | O 23:42:12-48:03 | O 00:07:04-13:01 |
| seed | P | P | P | P | P | P (+ owner DB) | P (+ owner DB) |
| pre-state, arm, start | P -22:34:16 | P -22:45:07 | P -22:58:12 | P -23:11:07 | P -23:32:12 | P -23:50:16 | P -00:15:14 |
| track | P -22:35:19 (15) | P -22:47:20 (27) | P -23:00:34 (29) | I -23:21:41 (50, H8) | O -23:39:18 (28) | P -23:51:14 (14) | P -00:16:17 (15) |
| owner-continuation | S | S | S | N | O 23:39:18-40:03 | S | S |
| track after owner | - | - | - | - | P -23:40:05 | - | - |
| terminal | P | P | P | P | P | P | P |
| management-off, reboot | - | - | - | - | - | P, P (SSH 8.3 s) | P, P (SSH 3.7 s) |
| management-off-measure | - | - | - | - | - | P 185.0 s | P 180.0 s |
| management-return | - | - | - | - | - | F (H12) | P |
| collect, verdicts | P, P | P, P | P, P | P, F (panel never down) | P, P | P, F (H13) | P, P |
| kind-expectation | - | - | P | F (not measured, F4) | P | F (H12) | P |

| Step | arch-good | arch-def | arch-sc | arch-rs | arch-oc | arch-mr a | arch-mr b |
| --- | --- | --- | --- | --- | --- | --- | --- |
| preflight + origin | P 23:56:26-56:38 | P 00:21:34-21:47 | P 00:31:18-31:30 | P 00:41:13-41:25 | P 00:51:20-51:34 | P 01:08-01:08:33 | P 01:24-01:25:00 |
| baseline-install | P 157 s | P 117 s | P 118 s | P 174 s | P 188 s | P 172 s | P 135 s |
| login, license | P | P | P | P | P | P | P |
| setup | O 23:59:16-02:03 | O 00:23:45-26:32 | O 00:33:29-36:21 | O 00:44:20-47:17 | O 00:54:43-57:40 | O 01:11:26-14:24 | O 01:27:16-30:13 |
| seed | P (no mail) | P | P | P | P | P (+ owner DB) | P (+ owner DB) |
| pre-state, arm, start | P -00:04:07 | P -00:28:15 | P -00:38:14 | P -00:49:11 | P -00:59:14 | P -01:16:08 | P -01:32:08 |
| track | P -00:05:00 (14) | P -00:29:53 (22) | P -00:39:55 (21) | P -00:49:56 (11) | O -01:06:01 (27) | P -01:17:02 (14) | P -01:33:02 (14) |
| owner-continuation | S | S | S | S | O 01:06:01-06:46 | S | S |
| track after owner | - | - | - | - | P -01:06:47 | - | - |
| terminal | P | P | P | P | P | P | P |
| management-off, reboot | - | - | - | - | - | P, P (SSH 80.7 s) | P, P (SSH 41.5 s) |
| management-off-measure | - | - | - | - | - | P 180.0 s | P 185.0 s |
| management-return | - | - | - | - | - | P | P |
| collect, verdicts | P, P | P, P | P, P | P, F (panel came back) | P, P | P, F (H14) | P, P |
| kind-expectation | - | - | P | F (not measured, F5) | P | P | P |

Every Arch baseline install includes the owner restart that the installer requires. Setup waited at
`access_dns` in every cell (H4 rule). Arch `web_mail` was refused at review
(`server_setup_service_unsupported:dovecot`) and fell back to `web`, as in upd2/upd3. The full table with
exact times is `step-table.md`; per-cell summaries are `summary-per-cell.txt`.

## What each kind showed

### Good candidate G

| | Debian 13 | Arch |
| --- | --- | --- |
| Start / verified | 22:34:16 / 22:35:18 (62 s) | 00:04:07 / 00:04:56 (49 s) |
| completion.pending | 22:35:06.5 (observer) | seen |
| Installed / running | G, identity and running executables match | same |
| Database | `equal-except-volatile`, `unexpected: []` | same |
| Timers, firewall | equal (`certbot.timer`, recovery timer) | equal (`certbot-renew.timer` disabled before and after) |
| Card after | "The update completed. The panel and agent restarted on the new version." / "Güncelleme tamamlandı. Panel ve agent yeni sürümle yeniden başladı." | same |

The same G preflight also passed in d13-oc, d13-mr (a, b), arch-oc and arch-mr (a, b): eight G updates, no
preflight stop. Whether the re-read added by a6dd5b1e ran is not visible (it logs nothing when the first
read passes).

### Start-check S and migrate-only D (automatic rollback)

| | d13-sc | arch-sc | d13-def | arch-def |
| --- | --- | --- | --- | --- |
| Failure line | 22:59:07.6 `candidate_panel_startup_check_failed ... tls_pair_invalid` | 00:38:53.9 same | 22:45:55 `update_failed ... offline panel database migration failed` | 00:28:49.9 same |
| Attempt 1 | 22:59:09 `update/active` | 00:38:54.9 `update/active` | 22:45:56 `update/active` | 00:28:50.9 `update/active` |
| Second fault | QMP reset at `payload_restored`, SSH back 8.3 s | SIGKILL at `runtime_verified` 00:39:10.04 | QMP reset 22:46:13, SSH back 8.3 s | SIGKILL at `runtime_verified` 00:29:06.81 |
| Attempt 2 | 23:00:11.6 `rollback/active` | 00:39:41.2 `rollback/completion` | 22:46:58 `rollback/active` | 00:29:37.9 `rollback/completion` |
| Result | `recovered/rollback_verified`, `failure_code` kept, 23:00:31 | 00:39:53 same | `recovered/rollback_verified` 22:47:18 | 00:29:50 |
| Kind judge | as-expected (12 rules) | as-expected (H9 rule now passes) | - | - |
| Terminal | B running, database `equal-except-volatile`, timers and firewall equal, marker, mailbox/SMTP (Debian), cron rows, fresh login | same (no mail) | same | same |

### Real-start R: not measured (F4, F5)

- **Debian (F4):** the update failed at 23:11:32, 25 s after the start, before any release change: the
  status went to `failed/none/update_failed` and stayed there (H8 stop after 606.3 s over 44 reads). B
  stayed installed and running; the Panel was never down; site, SMTP and cron never interrupted; the
  database equal. Panel API and root CLI agreed in 50/50 samples. The candidate R never ran.
- **Arch (F5):** the update failed at 00:49:32 in phase `active` (after the freeze); one automatic rollback
  attempt (00:49:33.9, `update/active`) returned B at 00:49:52 (`recovered/rollback_verified`). The
  candidate R never ran; the Panel was down 15-25 s and came back.
- The kind judge reports these as findings (no `panel_start_unverified`, no completion marker, no pause);
  they describe the unmeasured kind, not R.

### Owner continuation (both platforms)

| | Debian 13 | Arch |
| --- | --- | --- |
| Old Panel stopped, hold bound | 23:32:35.5 | 00:59:34.4 |
| completion.pending | 23:32:58.6 | seen |
| Update failure line | 23:34:00.7 `code=panel_start_unverified state=recovery_required` | 01:00:56.3 same |
| Forward attempts (`update/completion`) | 23:34:01.99, 23:35:42.87, 23:37:22.99 | 01:00:57.58, 01:02:36.81, 01:04:16.74 |
| Each attempt | 5 Panel starts, "bind: address already in use", restart counter back to 1 at each attempt | same |
| Certbot at the last failure | 23:38:39.6 "Automatic certificate renewal (Certbot) was returned to its state from before the update while this operation waits; the retry pauses it again before it continues." / TR "Otomatik sertifika yenileme (Certbot) bu işlem beklerken güncellemeden önceki durumuna döndürüldü; yeniden deneme devam etmeden önce onu yeniden duraklatır." | 01:05:24.5 "Automatic certificate renewal (Certbot) is already in its state from before the update." / TR "Otomatik sertifika yenileme (Certbot) zaten güncellemeden önceki durumunda." |
| Pause (`paused_retry_limit`, `first_failure_code=panel_start_unverified`) | 23:39:10.9 (395 s after the old Panel stopped) | 01:05:55.8 (381 s) |
| Timers at the pause | `certbot.timer` active/enabled (pre-update state) | `certbot-renew.timer` inactive/disabled (pre-update state) |
| Panel log read (`sudo journalctl -u celikpanel-panel -n 50`) | names the cause: "Failed to start panel listener: listen tcp :2083: bind: address already in use" | same |
| Printed retry (read with `owner-retry`, not run) | `/usr/libexec/celikpanel/recovery recover --retry --snapshot 20260930T233233Z-from-unknown-to-e1fdca38...-dcded64ee4c7e6f26b3133dd6213f9d8` | `... 20261001T005932Z-...-c0330bbe5baad02c2df5efe451b8d128` |
| Panel unit before the retry | inactive, `Result=exit-code`, `NRestarts=4` (no start-limit hit) | same |
| Owner releases the port | 23:39:38.8 `owner-released`, held 423.3 s | 01:06:22.4, held 408.0 s |
| Owner retry (once) | 23:39:40.5-23:40:04.2, rc 0, owner receipt 23:39:41.6 `update/completion` | owner receipt 01:06:25.4, rc 0 |
| Panel back / verified | listener 23:39:49.8; `succeeded/update_verified` 23:40:04 | 01:06:32; 01:06:46 |
| Panel unit after | active, `NRestarts=0`, `Result=success` (consistent with `reset-failed` before the start; the command itself logs nothing) | same |
| Workloads | site 69/69, SMTP 69/69, cron 9 stamps 23:32:01-23:40:01, never interrupted | site never interrupted, cron never interrupted, no mail |
| Panel window | 430-440 s, `down-only-during-transaction`; fresh login after | 414-425 s |
| Timers, firewall after | equal to pre-update | equal |
| Kind judge | as-expected, all 17 rules | as-expected, all 17 rules |

Hold bound 1080 s; the pause came 395/381 s after the old Panel stopped (685/699 s margin).

### Management off and one orderly reboot

| | d13 run-a | d13 run-b | arch run-a | arch run-b |
| --- | --- | --- | --- | --- |
| Good update first | verified 23:51:12 | 00:16:13 | verified | verified |
| Off | `disable --now` 23:51:21-31, both units inactive/disabled | 00:16:24-33 | 01:17:09-19 | 01:33:09-19 |
| Reboot, SSH back | new boot, 8.3 s | new boot, 3.7 s | new boot, 80.7 s | new boot, 41.5 s |
| Window (5 s samples, new boot) | 185.0 s, 38 samples | 180.0 s, 37 | 180.0 s, 37 | 185.0 s, 38 |
| Units after boot | inactive/disabled | same | same | same |
| Site + marker | served (first OK 12.7 s after boot; 1 failing sample before it) | same (12.7 s, 1 before) | 37/37 from 12.3 s | 38/38 from 12.6 s |
| SMTP 587 | 38/38 from 7.7 s | 37/37 from 7.7 s | not seeded | not seeded |
| Owner DB row (`mariadb` as root, `row_matches`) | 38/38 | 37/37 | 37/37 | 38/38 |
| Cron | 3 new stamps, no stall | 3, no stall | 2, no stall | 2, no stall |
| Renewal timer | `certbot.timer` active/enabled before and after | same | `certbot-renew.timer` inactive/disabled before and after | same |
| Firewall | `table inet celikpanel_fw` present, equal | same | same | same |
| Return | login ok; every API read 503 `PANEL_STARTING` (H12) | 4 reads `starting` to `ready` in 9 s; no difference | ready at first read; no difference | 2 reads, 3 s; no difference |
| Owner state after return | - | domains `[1, upd1-owner.test]`, cron listed, mailbox listed, database `upd1_owner_test_upd1db`, v0.1.0-alpha.82 e1fdca38 schema 42 agent match, update `succeeded`, recovery `succeeded/update_verified` | same without mailbox | same |
| needed_panel | [] | [] | [] | [] |

The firewall ruleset after the reboot comes from `celikpanel-firewall-restore.service`, which
management-off does not disable (recorded in the snapshots). Arch setup did install MariaDB before its
`access_dns` wait; the owner database was created through the Panel in all four runs.

### Outage windows (guest sampler 5 s; `outage-windows.txt`)

| Cell | Site | SMTP | Cron | Panel (guest) |
| --- | --- | --- | --- | --- |
| d13-good | never | never | never | 25-35 s |
| d13-def | 0-19.7 s at the reset (`interrupted-only-by-host-reset`) | 0-14.7 s at the reset | never | 99.7-109.7 s (reset) |
| d13-sc | 0-18.8 s at the reset | 0-13.8 s at the reset | never | 103.8-113.8 s (reset) |
| d13-rs | never | never | never | never down (F4) |
| d13-oc | never | never | never | 430-440 s |
| d13-mr b | never | never | never | 20-30 s (update part) |
| arch-good | never | not seeded | never | 15-25 s |
| arch-def | never | not seeded | never | 30-40 s + 0-10 s |
| arch-sc | never | not seeded | never | 30-40 s + 5-15 s |
| arch-rs | never | not seeded | never | 15-25 s, came back (F5) |
| arch-oc | never | not seeded | never | 414-425 s |
| arch-mr b | never | not seeded | never | 15-25 s (update part) |

DNS: `not-provided-external-dns` in every cell.

### Views: which answered, and whether they agreed

Agreement passed in every run; when both known sources answered they agreed (`views-per-cell.txt`).
Panel API / root CLI answers per status sample:

| d13-good | d13-def | d13-sc | d13-rs | d13-oc | d13-mr b | arch-good | arch-def | arch-sc | arch-rs | arch-oc | arch-mr b |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 10/15, 15/15 (1 lag) | 9/27, 27/27 | 9/29, 28/29 (1 none during reset) | 50/50, 50/50 | 7/29, 29/29 | 10/15, 15/15 | 10/14, 14/14 | 15/22, 22/22 | 13/21, 21/21 | 7/11, 11/11 | 7/28, 28/28 (1 lag) | 10/14, 14/14 |

At the owner-continuation pause the Panel API, the update card and the offline page were unreachable (the
Panel was down; nothing serves the saved page). The root CLI answered and showed the exhausted state. The
saved page names `sudo /usr/libexec/celikpanel/recovery status --request-id <rid> --lang en|tr`. The
recovery screen and card at the pause are modelled (H10/H11), not seen. The SSH owner view was not
attempted.

## Guidance observed (verbatim, EN / TR)

All texts come from the product: the CLI output of each sample or the build's catalogues. Per-run lists
with first-seen times are in `<cell>/run-*/side/extract.txt`.

**Owner continuation, root CLI** (Debian and Arch identical):

- First forward failure (23:33:55 / 01:00:58), `recovering`, `failure_code=panel_start_unverified`:
  - EN: "The update was applied, but the new version's panel did not come up. The server owner should read
    the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. The update's completion is
    retried automatically up to its limit; after that, sudo journalctl -u
    celikpanel-release-recovery.service --no-pager -n 50 shows a one-time retry command for this
    operation. There is no supported return to the previous version from this point."
  - TR: "Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı. Sunucu sahibi sunucudaki panel
    günlüğünü okumalı: sudo journalctl -u celikpanel-panel -n 50. Güncellemenin tamamlanması sınırına kadar
    otomatik olarak yeniden denenir; sonrasında sudo journalctl -u celikpanel-release-recovery.service
    --no-pager -n 50 bu işlem için tek seferlik yeniden deneme komutunu gösterir. Bu noktadan önceki sürüme
    desteklenen bir dönüş yok."
- Between attempts, `recovery_required/recovery_failed`, `automatic_recovery=retry_scheduled`,
  `first_failure_code=panel_start_unverified` (Debian 23:35:10-23:35:26 and 23:36:38-23:36:54; Arch
  01:01:51-01:02:08 and 01:03:41-01:03:58): the text above, followed by
  - EN: "The last automatic recovery attempt did not finish, and automatic recovery tries this same
    operation again by itself: the next attempt normally starts about 30 seconds after the previous one
    ended, up to three attempts in total. Nothing is needed on the server now: check this same request
    again in a minute and do not start another update. The server owner has to act only if automatic
    recovery pauses."
  - TR: "Son otomatik kurtarma denemesi tamamlanmadı ve otomatik kurtarma aynı işlemi kendiliğinden
    yeniden deniyor: sonraki deneme normalde bir öncekinin bitişinden yaklaşık 30 saniye sonra başlar,
    toplamda en çok üç deneme yapılır. Şimdi sunucuda yapmanız gereken bir şey yok: bir dakika sonra aynı
    işlemi yeniden sorgulayın ve başka güncelleme başlatmayın. Sunucu sahibinin ancak otomatik kurtarma
    durursa işlem yapması gerekir."
  - Support line: "phase=recovery_required reason=recovery_failed proof=none previous_failure=recovery_failed
    automatic_recovery=retry_scheduled first_failure_code=panel_start_unverified". The F3 text of upd3
    ("the server owner must act" between attempts) did not appear between attempts.
- After the third attempt, before the pause was recorded (Debian 23:38:27-23:38:44, Arch 01:05:10-01:05:27;
  see F6):
  - EN: "Automatic recovery could not finish this update, so the server may be between versions. The server
    owner must act: read the recorded reason and next step with sudo journalctl -u
    celikpanel-release-recovery.service --no-pager -n 50. Keep the server's files as they are and do not
    start another update."
  - TR: "Otomatik kurtarma bu güncellemeyi tamamlayamadı; sunucu iki sürüm arasında kalmış olabilir. Sunucu
    sahibinin işlem yapması gerekiyor: kaydedilen nedeni ve sonraki adımı sudo journalctl -u
    celikpanel-release-recovery.service --no-pager -n 50 ile okuyun. Sunucudaki dosyalara dokunmayın ve
    başka güncelleme başlatmayın."
  - Support line: "phase=recovery_required reason=recovery_failed proof=none previous_failure=recovery_failed".
- At the pause (23:39:02 / 01:05:44):
  - EN: "The update was applied, but the new version's panel did not come up, and completing the update was
    retried to its limit. Read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50.
    Retrying repeats the same start until that cause is fixed. There is no supported return to the previous
    version from this point. Automatic recovery used all three attempts without finishing, so the server
    may be between versions. The server owner must act: read sudo journalctl -u
    celikpanel-release-recovery.service --no-pager -n 50, fix the cause it names, then run the one-time
    same-operation retry command shown there. Checking status does not retry recovery. Automatic
    certificate renewal (Certbot) was stopped for this update; the same recovery journal says whether it was
    returned to how it was before the update or stays stopped until this operation finishes."
  - TR: "Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve güncellemenin tamamlanması sınırına
    kadar yeniden denendi. Sunucudaki panel günlüğünü okuyun: sudo journalctl -u celikpanel-panel -n 50. Bu
    neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu noktadan önceki sürüme desteklenen
    bir dönüş yok. Otomatik kurtarma üç denemenin hepsini kullandı ve tamamlanamadı; sunucu iki sürüm
    arasında kalmış olabilir. Sunucu sahibinin işlem yapması gerekiyor: sudo journalctl -u
    celikpanel-release-recovery.service --no-pager -n 50 çıktısını okuyun, belirtilen nedeni giderin ve
    orada aynı işlem için gösterilen tek seferlik yeniden deneme komutunu çalıştırın. Durum sorgusu
    kurtarmayı yeniden başlatmaz. Otomatik sertifika yenileme (Certbot) bu güncelleme için durduruldu; aynı
    kurtarma günlüğü, güncellemeden önceki hâline döndürülüp döndürülmediğini ya da bu işlem bitene kadar
    durdurulmuş kalacağını söyler."
  - Support line: "phase=recovery_required reason=recovery_incomplete proof=none previous_failure=recovery_failed
    automatic_recovery=paused_retry_limit first_failure_code=panel_start_unverified".
- Recovery journal at the pause: "Automatic recovery paused after three admitted attempts. No recovery child
  was started. Preserve evidence and inspect the recovery service journal." / "Otomatik kurtarma, izin
  verilen üç denemeden sonra durdu. Kurtarma alt işlemi başlatılmadı. Kanıtları koruyun ve kurtarma servisi
  günlüğünü inceleyin." / "After resolving the cause, the owner may authorize one same-snapshot retry: sudo
  /usr/libexec/celikpanel/recovery recover --retry --snapshot <snapshot>".
- Owner retry output: "Recovery dispatch admitted: attempt=owner snapshot=<snapshot>",
  "CELIKPANEL_UPDATE_CHECKPOINT database_verified_before_start", "==> Previous pending update finalized from
  verified snapshot: ..." / "==> Önceki bekleyen güncelleme doğrulanmış snapshot'tan tamamlandı: ...".
- After the retry: EN "The update completed and the new version was verified when it finished. Nothing else
  is needed on the server. Open the panel to check that everything works now." / TR "Güncelleme tamamlandı
  ve yeni sürüm bittiği anda doğrulandı. Sunucuda başka bir işlem gerekmiyor. Her şeyin şu an çalıştığını
  görmek için paneli açın." The card: "The update completed. The panel and agent restarted on the new
  version." / "Güncelleme tamamlandı. Panel ve agent yeni sürümle yeniden başladı." The recovery screen:
  "Update verified || The server verified this update. Reload CelikPanel to load the installed interface. ||
  Recovery failed" / "Güncelleme doğrulandı || ... || Kurtarma başarısız oldu" (O9).
- Product screen/card texts for `retry_scheduled` and the pause (with the renewal line):
  `h11-product-screen-texts.txt`; all keys present in both catalogues.

**Update card after a verified rollback** (d13-sc; arch-sc identical):

- EN: "Update not completed; previous version restored || The update to v0.1.0-alpha.82 did not complete.
  The server was returned to v0.1.0-alpha.81 automatically and is running it now. || Cause: the new
  version's panel failed its start check before anything was switched on. || Nothing needs to be done on the
  server. Do not start the update to v0.1.0-alpha.82 again until a corrected version is published. If the
  server's message names a problem on this server, fix it first. || Nothing resumes by itself: the server
  keeps running v0.1.0-alpha.81. When a newer version is published, “Check for updates” offers it."
- TR: "Güncelleme tamamlanmadı; önceki sürüm geri yüklendi || v0.1.0-alpha.82 sürümüne güncelleme
  tamamlanmadı. Sunucu otomatik olarak v0.1.0-alpha.81 sürümüne döndürüldü ve şu an onu çalıştırıyor. ||
  Neden: yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi. || Sunucuda
  yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar v0.1.0-alpha.82 güncellemesini
  yeniden başlatmayın. Sunucunun iletisi bu sunucudaki bir sorunu belirtiyorsa önce onu giderin. || Hiçbir
  işlem kendiliğinden sürmez: sunucu v0.1.0-alpha.81 sürümünü çalıştırmaya devam eder. Daha yeni bir sürüm
  yayımlandığında “Güncellemeyi kontrol et” onu gösterir."
- D (d13-def, arch-def) and arch-rs: the generic cause "Cause: the update failed before it completed; the
  server recorded no more specific cause." / "Neden: güncelleme tamamlanmadan başarısız oldu; sunucu daha
  belirli bir neden kaydetmedi.", then the secondary line "The server reported: reviewed updater failed:
  exit status 1: offline panel database migration failed; its original database and work evidence are
  preserved" (arch-rs: "... exit status 1: transaction-consistent panel database snapshot failed"). The
  internal tokens of upd3 O6 (`!! CELIKPANEL_UPDATE_FAILURE`, `state=recovery_required`) are gone after the
  rollback; the line is still English inside the TR card ("Sunucunun bildirdiği: reviewed updater failed:
  exit status 1: ..."; O7).
- While recovery runs (d13-def 22:47:15), the card still shows the raw line with internal tokens in EN and
  TR: "The server reported: reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE
  code=update_failed state=recovery_required reason=offline panel database migration failed; its original
  database and work evidence are preserved detail=" (O7).

**Root CLI after the start-check rollback** (d13-sc, arch-sc): EN "The new version's panel failed its start
check before anything was switched on, so the server was returned to the previous version automatically.
The previous version keeps running. Nothing needs to be done on the server. Do not start the same version
again until a corrected version is published. When you report this, include the reason line shown for this
update on the panel's update page." / TR "Yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç
denetiminden geçemedi; bu yüzden sunucu otomatik olarak önceki sürüme döndürüldü. Önceki sürüm çalışmaya
devam ediyor. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü
yeniden başlatmayın. Bunu bildirirken panelin güncelleme sayfasında bu güncelleme için gösterilen neden
satırını ekleyin." The kind judge compared the CLI output with `cmd/recovery/main.go` of the build: 0
mismatches. While returning: "The new version's panel failed its start check before anything was switched
on. The server is being returned to the previous version automatically; nothing needs to be done on the
server. Check this same request again for the verified result; do not start another update." / "Yeni
sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi. Sunucu otomatik olarak
önceki sürüme döndürülüyor; sunucuda yapmanız gereken bir şey yok. Doğrulanmış sonuç için aynı işlemi
yeniden sorgulayın; başka güncelleme başlatmayın."

**F4 texts (d13-rs, unchanged for 10 min):**

- CLI EN: "The update failed. If it stopped before anything was changed, the server keeps running the
  version it had; otherwise automatic recovery takes over and this status changes to recovery. Nothing to do
  on the server now: check this same request again in a few minutes and do not start another update. The
  panel's update page shows the reason when the panel is reachable."
- CLI TR: "Güncelleme başarısız oldu. Hiçbir şey değişmeden durduysa sunucu önceki sürümünü çalıştırmaya
  devam eder; aksi hâlde otomatik kurtarma devreye girer ve bu durum kurtarmaya geçer. Şimdi sunucuda
  yapmanız gereken bir şey yok: birkaç dakika sonra aynı işlemi yeniden sorgulayın ve başka güncelleme
  başlatmayın. Panel erişilebilir olduğunda güncelleme sayfası nedeni gösterir."
- Card and recovery screen: "The update failed || The server owner must inspect the update log using this
  operation ID. Keep the ID and check the recovery result before retrying an update." / "Güncelleme
  başarısız oldu || Sunucu sahibi bu işlem kimliğiyle güncelleme günlüğünü incelemelidir. Kimliği saklayın ve
  güncellemeyi yeniden denemeden önce kurtarma sonucunu kontrol edin." No server line.

**Other product lines:** guard "celikpanel release start guard: held: a CelikPanel update or recovery is in
progress; the unit starts when it finishes" (Debian reset cells); wait text "Recovery waiting for the
operating system transition; no owner action is needed. The native recovery timer will retry this same
operation. Recovery is not yet complete."; rollback journal "==> Rollback complete / Geri alma tamamlandı"
and "Restored release source commit / Geri yüklenen sürümün kaynak commit'i:
0e515e908fc7b3de5c4e74dc7535638cbe56d401 (from the restored Agent's build record / geri yüklenen Agent'ın
yapı kaydından)" in all four rollback cells and arch-rs.

## Product findings

**F4 (D-024; stops upd1-debian13-realstart; the upd3 F1 class in another step).** The owner-started update
of R stopped at 23:11:32, 25 s after the start, before any release change. The agent journal keeps the
cause: "!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=unchanged reason=panel service operations are
not idle; update refused; quiesce was safely aborted, rerun the exact trusted update detail=... WAL-aware
service operation idle check failed: service operations are not idle: SQLite sidecar -shm changed after
pinning" (`upd1-debian13-realstart/run-a/steps/14-collect/journal-product.txt:400`).

- Where: the preliminary WAL-aware idle probe of the live Panel database, `update.sh:3311-3313`
  (`run_update_idle_probe "$PREFLIGHT_PANEL" --check-service-operations-idle-wal-aware`, then
  `fail_before_active` -> `die` at `update.sh:3282`); the refusal is
  `cmd/panel/service_operation_idle_check.go:823`. The probe runs while the Panel is live (not frozen); the
  repeated probe after the freeze is the guard.
- a6dd5b1e's re-read after 2 s covers the recovery-runtime compatibility checker and kit promotion
  (`internal/recoveryruntime/promotion_linux.go:837-842`), not this probe: `run_update_idle_probe`
  (`update.sh:113-126`) runs once.
- The typed preflight path (`recovery_runtime_preflight_failed`, "stopped before changing the installed
  version ... starting again is safe") covers the four selected-runtime checks only. Here the code is
  `update_failed`, the observation has no `failure_code`, and every view gives the generic texts quoted
  above for 10 min (H8 stop): "check this same request again in a few minutes and do not start another
  update" (CLI), "The server owner must inspect the update log" (card, screen). The product's own line says
  the opposite ("rerun the exact trusted update").
- The cause never reaches the owner's screens: the update status has no `summary`. By code reading, the
  agent text is longer than 240 characters and contains `/` (the checker's log timestamps), and
  `sanitizePanelUpdateSummary` drops any such text (`cmd/panel/system_update_handlers.go:239-249`).
- B kept running; nothing was interrupted; database `equal-except-volatile`; timers and firewall equal.
- Would a second attempt pass? Not run (instruction). The same probe passed in the other 13 updates of
  this run (8 G, 2 D, 2 S and the Arch R), and the product's failure line says to rerun; whether a second
  attempt passes is not proven.
  Which Panel write raced the probe is not identified (the waiting setup runner and metrics writer are live
  then; upd3's hypothesis, now with a recorded cause).

**F5 (D-024; stops upd1-arch-realstart).** The update of R failed at 00:49:32 in phase `active`, right
after the coordinators were frozen and the old Panel killed (00:49:30.2-30.7): "!! CELIKPANEL_UPDATE_FAILURE
code=update_failed state=recovery_required reason=transaction-consistent panel database snapshot failed
detail=" (`upd1-arch-realstart/run-a/steps/14-collect/journal-product.txt:228`).

- Where: `panel --create-service-operation-snapshot` (`update.sh:3536-3544`), run with `$PREFLIGHT_PANEL`
  (the installed B or recovery-runtime checker, not the R candidate; `update.sh:44`, `:964`).
- The cause is not kept: `detail=` is empty, no journal line names the snapshot error. One automatic
  rollback (`update/active`) restored B in 20 s, verified; the card gives the generic cause and the server
  line "reviewed updater failed: exit status 1: transaction-consistent panel database snapshot failed".
- Not reproduced in this run: the same step passed in the 12 other updates that reached it (6 Arch,
  6 Debian). Not re-run.
- Consequence for the record: the real-start kind (forward-only completion, pause, no return) has no native
  measurement at a6dd5b1e on either platform; upd3 measured it at 94be6b6e.

**F6 (D-024 wording; both owner-continuation cells).** After the last automatic attempt fails and before
the pause is recorded by the next timer run (~37 s: Debian 23:38:33-23:39:10, Arch 01:05:24-01:05:55), the
status is `recovery_required/recovery_failed` without `automatic_recovery` or `first_failure_code`. The CLI
says "The server owner must act: read the recorded reason and next step ... Keep the server's files as they
are and do not start another update" with no cause, while the journal does not yet contain the retry
command (printed at the pause). a6dd5b1e allows "must act" after the last attempt; the observation still
drops the typed first cause in that window (upd3 F3 reduced to this window).

**O7 (observation).** The rolled-back card's secondary server line keeps the English agent text
("reviewed updater failed: exit status 1: ...") in the TR card; while recovery runs, the card shows the raw
line with `!! CELIKPANEL_UPDATE_FAILURE ... state=recovery_required ... detail=` in EN and TR (d13-def
22:47:15). Primary texts are Turkish.

**O8 (observation).** The paused CLI text says "Automatic certificate renewal (Certbot) was stopped for
this update" on Arch too, where the journal says renewal was "already in its state from before the update"
(the timer was disabled before the update).

**O9 (observation).** After the owner's retry succeeded, the recovery screen shows "Update verified" with
the previous-failure line "Recovery failed" / "Kurtarma başarısız oldu"
(`previous_failure=recovery_failed`).

**Corrections confirmed natively (a6dd5b1e):**

- F2: the Certbot scheduler was restored to its snapshot state when the last forward attempt failed
  (journal line above); `certbot.timer` active/enabled at the pause and after the retry (Debian).
- F3: between automatic attempts the status says `retry_scheduled` with the typed first cause and "the
  server owner has to act only if automatic recovery pauses" (CLI EN/TR above).
- Start-limit: the candidate Panel restarted 4 times per attempt and was started again by each attempt and
  by the owner's retry without a start-limit refusal; after the retry `NRestarts=0`, `Result=success`.
- O6 partly: internal tokens are gone from the rolled-back card; the server line is still English in TR.
- F1 (preflight stop with no cause): the Debian good cell passed; the typed preflight path itself was not
  exercised (F4 hit a different step).

## Real origin

- `celikpanel.net` resolved only to 127.0.0.1 and the fixture answered 200 after provisioning, after the
  owner restart the Arch installer demands, and before arm, in all 14 runs (`origin-check.txt`).
- The installer logs name neither `celikpanel.net` nor `185.95.` (`secret-scan.txt`).
- The only journal lines naming the name are the fixture unit's start and stop lines.
- The licence step activated the D-027 fixture with `license_service: not contacted` in every run; every
  Panel logged "ACCEPTANCE FIXTURE — NOT FOR PRODUCTION ... it never contacts the license service".

## Secret scan

Run over the whole folder before hashing (`secret-scan.txt`). **Clean:**

- 0 PEM private-key blocks; 0 hits for the 799 body lines of 56 key files (each lab's SSH key, fixture
  signing key, fixture CA key and fixture TLS key).
- 0 hits for the fixture licence literal; 0 `CPK-` keys.
- 0 unredacted password, secret, token, CSRF, session or cookie fields; 1970 redaction markers in the
  driver evidence (the count in `secret-scan.txt` also includes mentions in this README and in `tools/`).
- The 8 tokens of the admin-password shape are a snapshot name and upd3 evidence file names in the run-copy
  hash lists; the 2 of the 32-character shape are SSH ed25519 **public** key prefixes in `fixture-plan.json`.
- `185.95.` appears only in this README, the scan scripts in `tools/` and the scan output.
- Longest path from the repository root: 196 characters.

## Files

- `build/`: artifacts document, dist JSONs, prove, dry runs, build and offline logs, fixture commits.
- `harness-run-copy/`: H12/H13 and H14 diffs, run-copy hashes (a6dd5b1e, h12, h14), offline logs of the
  patched copies, job scripts and the scripts that made them.
- `tools/`: the host-only readers used for this README (extraction, summaries, scan, H11 texts).
- `<cell>/run-a/` (and `run-b` for the two management-off re-runs): the driver's evidence directory
  unchanged (its `SHA256SUMS` verified when staged); `host/` wrapper logs, harness used, lab identity,
  owner-start and owner-continuation attempts, observer intents, baseline installer result and log, fixture
  plan; `side/extract.txt`.
- `summary-per-cell.txt`, `step-table.md`, `outage-windows.txt`, `views-per-cell.txt`, `origin-check.txt`,
  `h11-product-screen-texts.txt`, `secret-scan.txt`, and `SHA256SUMS` over every file except itself.

## Host leftovers (archlinux WSL)

- `/var/tmp/cp-upd1-build/20260930t221920z` (629 MB): the clone and archives, written by the harness build.
- `/var/tmp/cp-pair-accept/dist/{0e515e90...,e1fdca38...,bd0e626e...,96de9cdb...,dd4adede...}-acceptance-license`:
  new directories written by `build-dist.sh`; nothing that existed there was modified.
- `/var/tmp/cp-upd4-run` (931 MB): run copies `harness`, `harness-h12`, `harness-h14`, logs, jobs, build
  records and the stage.
- Stopped labs `/var/tmp/cp-release-drill-upd4-{d13-good-a,d13-def-a,d13-sc-a,d13-rs-a,d13-oc-a,d13-mr-a,d13-mr-b,arch-good-a,arch-def-a,arch-sc-a,arch-rs-a,arch-oc-a,arch-mr-a,arch-mr-b}`
  (2.5-2.7 GB each): overlays, per-lab fixture keys and serial logs.
- Caches: the build used the Go build cache and npm cache in root's home.

No QEMU process is running; all guests are stopped, overlays kept. Nothing else under `/var/tmp` or `/root`
was deleted or modified; git configuration was not changed (`-c safe.directory` per command). The
repository was not written except this folder. No inline `wsl.exe` command contained `$`: every host step
ran from an LF script file; four read-only `python3 -c` reads of staged JSON and one `pgrep -a qemu` were
inline, without `$`.

## What this run proves and does not prove

It shows, on disposable guests with the acceptance fixture:

- **The owner path:** a forward completion that exhausted its budget on an owner-fixable host cause was
  completed by the owner's one printed retry, on both platforms. The product's texts led to the cause (the
  panel log named the port conflict), `retry_scheduled` replaced the early "must act" between attempts,
  renewal was restored at the pause, and site, mail and cron kept running.
- **Owner independence across a reboot:** with the Panel and Agent disabled, site, SMTP, cron, the owner's
  database row, the renewal timer and the firewall ruleset were served or kept; the Panel came back to the
  same owner state.
- **The good update forward on both platforms**, and the automatic rollback of S and D with a second fault.

It does **not** show:

- the real-start kind at a6dd5b1e (F4, F5), nor the typed preflight path (`recovery_runtime_preflight_failed`);
- that the preliminary idle probe or the online snapshot cannot refuse again (F4 and F5 are one sample each;
  their triggers are unidentified);
- a cause the owner cannot fix, a second fault during the owner's retry, browser rendering (the card and
  screen at the pause are modelled), the SSH owner view;
- power loss, DNS (external mode), mail on Arch, certificate issuance or renewal execution, production
  signing, or removal of packages or files.

It closes no P0 row. Wall time: 22:18Z (host check) to about 01:45Z; the twelve cells and two re-runs ran
22:23:40Z to 01:37:03Z.
