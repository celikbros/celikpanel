# upd11 native run, 2026-10-01/02: the whole owner-started update matrix on one exact candidate (`48e54657`)

Roadmap item 3, owner-started update acceptance (`owner_update_trial.py`), eleventh run. Until now every kind of update
cell was last measured on a different, earlier build (migration defect, start check and management-off at `a6dd5b1e`;
good, real-start and owner continuation at `6cda60b8`; the alpha.80 path at `48d21d58`/`e9e2d3f3`; Ubuntu setup and
the package-activity rule at `c67d1861`). Since then the update script, the recovery runner, the Agent's
package-activity rule and the Panel's start check changed. This run builds **everything from commit `48e54657`**
(the run copy is `git archive 48e54657`; HEAD stayed `48e54657` for the whole run) and runs the matrix once on it.

Disposable QEMU/KVM guests on the local `archlinux` WSL host only. No installed server (Boston, Frankfurt, any) was
touched or contacted. Nothing was committed, pushed, published or signed with a production key. `celikpanel.net`
resolved only to the guest-loopback fixture origin; no licence service was contacted. Every `result.json` carries
`native_evidence: false`; the owner judges the P0 rows. **It closes no P0 row.**

**Result in one sentence:** on `48e54657` every kind reached its expected product end on every platform where it was
run - good update verified (Debian 13, Arch, Ubuntu 24.04, and from the alpha.80 source on Debian and Ubuntu), automatic rollback
with the platform's second fault (migration defect and start check, three platforms; migration defect from alpha.80 on
Debian), forward completion paused at its limit with the typed first cause (Debian real start), the owner's one printed
retry completing the paused update (three platforms, and from alpha.80 on Debian), and the workloads served with management off across a reboot
(three platforms) - with four non-product stops on the way: one setup refusal for package activity on Debian (F1, the
known open setup limitation), two harness defects (H20, H21) and one host suspension (P1).

## What was built from what

Two builds from the run copy, both `go1.26.5 linux/amd64`, every archive `license_mode: acceptance-fixture`
(`build/cur/`, `build/a80/`: artifact documents, dist JSONs, build logs `build.*.txt`).

| Build | Role | Label / seq | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `run-upd1.sh build 48e54657` (22:12:15-22:15:27Z) | Baseline B | v0.1.0-alpha.81 / 81 | df01e72f0a04767c7ceb0c07a148228cd71837c7 | e986acb86e8caf03409a0399034725bb7f6b519a | 825b2385181104967c3b1eca877b6c61ee0b56bca36bb76e44e25b1a4faf40bc |
| | Good G | v0.1.0-alpha.82 / 82 (parent B) | 73eef2862644ccf48290073d3eeeb6bb461c1330 | 60e46d72783248a2d040f466a9d2834adc3e336c | 4a00f28d2a73c5646110a1cbc44b7e12fc379ef3b5467adeb9cca335cfa2d7fa |
| | Defective D | v0.1.0-alpha.82 / 82 (parent G) | f37e1cccf6ba16bf2a648c343e6defc45dc8cefe | 0775b432d3e8ab6d332f0f7c2f1950bd66a71303 | 3997b6bd8b73bb74a21ff211d15c8c855dfd0d14168a03f59c547254fdccd98c |
| | Start-check S | v0.1.0-alpha.82 / 82 (parent G) | f91732bae9a03c42a0e63e2f166794e3d76bd6aa | e11eaeee0364d41b0dc44a813986dcd08a8f877c | c8c449a2bc4edbe83f4a75875ec6bb1a0d58f8d9650215b5b519a397dcad77ba |
| | Real-start R | v0.1.0-alpha.82 / 82 (parent G) | 235198ff900dbb851a5fdcc966c94db7857f556d | ad7a431a4ce0a87c1782b9006f298670c8d6e63f | abd9cb3a144d25264cedbf391b6bb1a5473d62769d37b32182790f0e507a7031 |
| `run-upd1.sh build --baseline-ref v0.1.0-alpha.80 48e54657` (22:15:33-22:17:36Z) | Baseline B80 = tag `v0.1.0-alpha.80` (bd14d97e) + D-027 licence seam | v0.1.0-alpha.80 / 80 | b9fe1037c6b5580fed97fa0382ec576d5f041045 | ee6d75f688fac425179ee646a647a0ceb68283bc | e00ea2267aa9fc2231d0ce52e165855098c88b7d748d5a5df7c34bff3bb1dcb4 |
| | Good G81 | v0.1.0-alpha.81 / 81 (parent B80) | 9656dabad6d85b9a3b1a9360a7ff6e984fe4b4d3 | 9ce07b820c7fd4b358609f7fd3381d04f530e505 | 71d191cee0c91448e462d652674671d4250548623823bb0484dbd8bd9dbe0955 |
| | Defective D81 | v0.1.0-alpha.81 / 81 (parent G81) | ca9c11d17a76b98aaccb3512ee87a94c80b44b04 | 1e084fa2b949e0fdd717c93215c6df6937b0fdc1 | bed12c10d6554031b5867c1b73ce82715740038ea3643f47747cbfa09407e4ab |

- Source HEAD `48e5465712c3ef94f10fa8f91cc952289902ec2d` in both artifact documents (`build/source-commit.txt`).
  B80's tree `ee6d75f6…` is the same tree as upd7's and upd8's B (same tag, same seam); the unchanged-file proof is
  `build/a80/baseline-ref-proof.txt` (the build refuses any `DIFFERENT`), the agent package list `build/a80/agent-deps.txt`.
- `prove` exited 0 for both documents (`build/cur-prove.json`, `build/a80-prove.json`); 16 + 4 dry runs exited 0
  with `native_evidence: false` and created no lab (`build/cur-dry-run-*`, `build/a80-dry-run-*`; three more on the
  H20 copy, `build/h20-dry-run-*`).
- Offline suites (`build/offline-*`): pristine `48e54657` copy (`p`) and the overlay copies (`o1`, `h20`, `h21`) all OK -
  `test_owner_update_trial` 175 / 175 / 176 / 177, `test_recovery_candidate_archive` 11, `test_lab` 15,
  `test_current_worker_baseline` 7, `test_worker_fixture_origin` 15, `test_bound_worker_reboot` 11,
  `test_guest_bound_worker` 8.

## Run copies and harness changes

The harness ran from `git archive 48e54657` (`harness-run-copy/runcopy-48e54657-files.sha256`) plus overlays of two
working-tree files, `owner_update_trial.py` and `test_owner_update_trial.py` (`harness-run-copy/overlay*/`: files with
SHA-256 and the diff). No product file and no other harness file changed. `PYTHONDONTWRITEBYTECODE=1`.

| Copy | Overlay diff SHA-256 | Change | Used by |
| --- | --- | --- | --- |
| `harness` | `ae261126…` | **Ubuntu variants**: `upd1-ubuntu-startcheck` (reset at `payload_restored`) and `upd1-ubuntu-mgmt-off-reboot`, mirroring the Debian definitions; every Ubuntu cell has the PackageKit probe on (`pk_observe=True`: /proc facts and the Agent's read-only readiness at setup, arm and the owner's start; PackageKit's journal at collect); the result carries `packagekit` for every probed cell. H19 (the owner's package wait) unchanged. Tests: the cell list and the Ubuntu-mirrors-Debian check (only `pk_observe` differs, same plan steps). | cells 1-12 |
| `harness-h20` | `738c9a5b…` | + **H20** (below) | cells 11b, 13, 14, 15, 16 |
| `harness-h21` | `6699ac34…` | + **H21** (below), cumulative | cells 15b, 17-19 |

| Id | Defect | Kind | Handling | Cells |
| --- | --- | --- | --- | --- |
| H20 | `management-off-measure` judges cron after exactly 180 s of new-boot samples starting with the sampler (13 s after the boot). On a slow Arch boot `crond` started at 00:52:01.37, 85 s after the boot (sshd came back at 83.8 s), missed 00:52:00 by 1.4 s, then ran the owner's job at 00:53:00 and 00:54:00; the 185 s window held only the first, so the verdict was `not-advancing` while cron ran every minute with management off (`upd1-arch-mgmt-off-reboot/run-a/steps/18-collect/journal-setup-services.txt`, `…/16-management-off-measure/step.json`). | measurement (harness) | `management_off_window_complete`: with cron seeded and fewer than two in-boot stamps at 180 s, keep measuring read-only for at most one cron period limit (130 s) more; a cron that does not advance still fails. Records `h20_window_past_180_s` and `h20_cron_at_180_s`. Test `test_h20_window_waits_for_a_late_cron_daemon`. | arch-mgmt-off run-a kept (failed); **run-b** re-run once. In run-b SSH came back at 41.5 s and two stamps fell inside 185 s, so the extension itself was not needed (`h20_window_past_180_s` 5.0). |
| H21 | The track stops on the first terminal root-CLI read. In `upd1-ubuntu-startcheck/run-a` sample 28 (02:32:06-07) the Panel API was read just before the record turned final (`recovering`) and the CLI just after (`recovered/rollback_verified`); the agreement rule treats a disagreeing last sample as a failure. The terminal step 1 s later read `recovered` from both. | race in the harness's sampling | `final_needs_confirmation`: one confirming sample 2 s later decides; a persisting disagreement still fails. Test `test_h21_one_confirming_sample_after_a_final_race`. | ubuntu-startcheck run-a kept (failed); **run-b** re-run once (invalidated by P1, below) |

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`, GiB; `host/c-drive.txt`)

94.0 free at the start (22:11Z), 86.9 before cell 1 (after both builds), lowest 55.8 at the end (13:07Z). Readings before every cell are in `host/c-drive.txt`; the reading at 11:27:40Z (after the host had slept, P1) served as the reading before cell 17, started 54 s later.
Never under 40 before a cell. The WSL disk image does not shrink when files are deleted inside it; the overlay removals
below kept it from growing further (space freed inside it was reused by later labs).

## Cells

One new lab per run, stopped by the wrapper; one run per cell, two harness re-runs (H20, H21) and one re-run after a
setup refusal (F1). Times are the wrapper's (UTC).

| # | Cell (folder) | Platform | Baseline | Lab, port | Harness | Wrapper (UTC) | Outcome | Overall |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | upd1-debian13-good run-a | Debian 13 | B (48e54657) | upd11-d13-good-a, 3211 | base | 22:18:43-22:30:17 | `update-verified` | complete-for-review |
| 2 | upd1-debian13-defective run-a | Debian 13 | B | upd11-d13-def-a, 3221 | base | 22:30:29-22:42:30 | `recovered-automatically`, 2 attempts, QMP reset at `payload_restored` | complete-for-review |
| 3 | upd1-debian13-startcheck run-a | Debian 13 | B | upd11-d13-sc-a, 3231 | base | 22:42:48-22:55:27 | `recovered-automatically`, 2 attempts, reset; kind as-expected | complete-for-review |
| 4a | upd1-debian13-realstart run-a | Debian 13 | B | upd11-d13-rs-a, 3241 | base | 22:55:56-23:00:05 | not reached: setup refused at `05-mail_profile` with `HOST_MUTATION_BUSY` (F1); no update started | failed |
| 4b | upd1-debian13-realstart run-b | Debian 13 | B | upd11-d13-rs-b, 3501 | base | 23:01:07-23:17:28 | `paused-owner-action-required` (`paused_retry_limit`, `first_failure_code=panel_start_unverified`), 3 attempts; kind as-expected | complete-for-review |
| 5 | upd1-debian13-owner-continuation run-a | Debian 13 | B | upd11-d13-oc-a, 3251 | base | 23:17:49-23:36:05 | `recovered-after-owner-continuation`, 3 + 1 owner; kind as-expected | complete-for-review |
| 6 | upd1-debian13-mgmt-off-reboot run-a | Debian 13 | B | upd11-d13-mr-a, 3261 | base | 23:36:26-23:51:12 | `update-verified`; management off 185 s served; kind as-expected | complete-for-review |
| 7 | upd1-arch-good run-a | Arch | B | upd11-arch-good-a, 3271 | base | 23:51:35-00:01:22 | `update-verified` | complete-for-review |
| 8 | upd1-arch-defective run-a | Arch | B | upd11-arch-def-a, 3281 | base | 00:01:47-00:12:48 | `recovered-automatically`, 2 attempts, SIGKILL at `runtime_verified` | complete-for-review |
| 9 | upd1-arch-startcheck run-a | Arch | B | upd11-arch-sc-a, 3291 | base | 00:13:16-00:22:58 | `recovered-automatically`, 2 attempts, SIGKILL; kind as-expected | complete-for-review |
| 10 | upd1-arch-owner-continuation run-a | Arch | B | upd11-arch-oc-a, 3301 | base | 00:23:30-00:39:49 | `recovered-after-owner-continuation`, 3 + 1 owner; kind as-expected | complete-for-review |
| 11a | upd1-arch-mgmt-off-reboot run-a | Arch | B | upd11-arch-mr-a, 3311 | base | 00:40:14-00:54:16 | `update-verified`; cron judged `not-advancing` (H20) | failed (harness) |
| 11b | upd1-arch-mgmt-off-reboot run-b | Arch | B | upd11-arch-mr-b, 3511 | h20 | 01:12:46-01:25:58 | `update-verified`; management off 185 s served; kind as-expected | complete-for-review |
| 12 | upd1-ubuntu-good run-a | Ubuntu 24.04 | B | upd11-ub-good-a, 3321 | base | 00:54:30-01:12:18 | `update-verified` | complete-for-review |
| 13 | upd1-ubuntu-defective run-a | Ubuntu 24.04 | B | upd11-ub-def-a, 3331 | h20 | 01:26:25-01:46:34 | `recovered-automatically`, 2 attempts, reset | complete-for-review |
| 14 | upd1-ubuntu-owner-continuation run-a | Ubuntu 24.04 | B | upd11-ub-oc-a, 3341 | h20 | 01:46:52-02:11:37 | `recovered-after-owner-continuation`, 3 + 1 owner; kind as-expected | complete-for-review |
| 15a | upd1-ubuntu-startcheck run-a (new cell) | Ubuntu 24.04 | B | upd11-ub-sc-a, 3351 | h20 | 02:11:59-02:32:20 | `recovered-automatically`, 2 attempts, reset; kind as-expected; track agreement F (H21) | failed (harness) |
| 15b | upd1-ubuntu-startcheck run-b | Ubuntu 24.04 | B | upd11-ub-sc-b, 3521 | h21 | 02:55:56-05:52:11 | `recovered-automatically`, 2 attempts, reset; kind as-expected; verdicts F (host suspended 03:13-05:49, P1) | failed (platform) |
| 16 | upd1-ubuntu-mgmt-off-reboot run-a (new cell) | Ubuntu 24.04 | B | upd11-ub-mr-a, 3361 | h20 | 02:32:37-02:55:37 | `update-verified`; management off 185 s served; kind as-expected | complete-for-review |
| 17 | part2-alpha80/upd1-debian13-good run-a | Debian 13 | B80 (alpha.80) | upd11-a80-d13-good-a, 3371 | h21 | 11:28:34-11:40:42 | `update-verified` | complete-for-review |
| 18 | part2-alpha80/upd1-debian13-defective run-a | Debian 13 | B80 | upd11-a80-d13-def-a, 3381 | h21 | 11:41:17-11:55:40 | `recovered-automatically` to alpha.80, 2 attempts, reset | complete-for-review |
| 19 | part2-alpha80/upd1-ubuntu-good run-a | Ubuntu 24.04 | B80 | upd11-a80-ub-good-a, 3391 | h21 | 11:56:00-12:46:12 | `update-verified` (setup: 7 owner attempts, 45 min, as upd8 F1) | complete-for-review |
| 20 | part2-alpha80/upd1-debian13-owner-continuation run-a (optional) | Debian 13 | B80 | upd11-a80-d13-oc-a, 3401 | h21 | 12:46:35-13:05:19 | `recovered-after-owner-continuation`, 3 + 1 owner; kind as-expected | complete-for-review |

Cells 1-16 ran 2026-10-01 22:18Z to 2026-10-02 02:55Z; 15b and Part 2 after the pause described in P1. Every
baseline install used the build's real installer (Arch with the owner restart it requires; B80 the tag's installer and
trust enrollment). Setup waited at the isolated host's `access_dns` in every cell that got past setup. Arch `web_mail`
was refused at review and fell back to `web` (no mail on Arch), as before. DNS `not-provided-external-dns` everywhere.
Per-cell step tables, attempts, terminal checks, agreement and sample counts: `summary-per-cell.txt`, `step-table.md`;
outage windows: `outage-windows.txt`.

### Changed paths since the earlier runs: were they exercised?

Read from each run's journals and root-CLI samples by `tools/cpaths.py` (`<cell>/run-*/side/changed-paths.txt`).

| Changed path | Exercised? | Where / evidence |
| --- | --- | --- |
| Updater writes the initial status record for an alpha.80 worker (`update.sh` `publish_update_initial_observation`) | **Yes, by inference** (as upd7/upd8): in cells 17-20 the root CLI first had no CLI, then `observation_unavailable` (≈ 7-10 s), then `running/update_running`, while the alpha.80 worker writes no record; `observation-records.json` holds the request's `.status`. The updater's own line ("…the updater recorded it for request …") is not in any collected journal (the worker's unit output is not collected), so no evidence file names the writer. | `part2-alpha80/*/run-a/side/changed-paths.txt`, `steps/*collect/observation-records.json` |
| Exact `/usr/bin/curl` preflight (`update.sh:951-952`) | **Passing branch only**: every one of the 20 updates reached the start check after it; it logs nothing when it passes. The refusal ("required update tool is missing: /usr/bin/curl") never appeared. | absence of the refusal line in every run |
| No-marker end after a failed recovery child (`release-recovery-runner.sh` `end_after_failed_child_without_marker`) | **No.** Neither "left no pending transaction" nor the quiesce-abort text appeared: every failed child (reset, SIGKILL, forward failures) left its marker and was retried. | all runs |
| `pause_pending` | **Yes**, in all four forward-pause runs (Debian real start, owner continuation on Debian, Arch, Ubuntu): the journal line "The last admitted recovery attempt did not finish. The next run of the recovery timer records the pause…" and the CLI state `recovery_required/recovery_failed`, `automatic_recovery=pause_pending`, `first_failure_code=panel_start_unverified` for ≈ 17-32 s before `paused_retry_limit`. The typed first cause stays visible in that window (upd4 F6 no longer occurs). | `upd1-debian13-realstart/run-b`, `*-owner-continuation/run-a` |
| Renewal restore at a forward pause | **Yes**: Debian and Ubuntu "Automatic certificate renewal (Certbot) was returned to its state from before the update while this operation waits; the retry pauses it again before it continues." (`certbot.timer` active/enabled at the pause and after); Arch "…is already in its state from before the update." (`certbot-renew.timer` disabled before and after). | `steps/12-owner-continuation-required/recovery-journal-at-pause.txt`, `steps/*collect/journal-product.txt` |
| Start check with the host's real listen address | **Default form only**: every start check ran with `CELIKPANEL_LISTEN=:2083` (empty host), read from the panel unit (sudo command line in the product journal). The F4 grammar change (host name, leading-zero port) is not exercised by these labs. The fixture start-check failure (`tls_pair_invalid`) rolled back as before on all three platforms. | `tools/listen.sh`; `journal-product.txt` `check-startup-readiness` lines |
| Package-activity refusals inside an update (the new Panel's startup reconcile) | **Yes, every update with a mail stack** (Debian, Ubuntu, both baselines; never on Arch, which has no mail): when the Panel starts inside the update or the rollback (candidate, restored baseline, or after the owner's retry) it logs "certificate startup reconcile: certificate dependents: publish full mail SNI snapshot: another server change or package-manager task is still running; preserve pending outbox: <nil>" and "milter wiring at startup: another server change or package-manager task is still running". **Is the skipped work done later?** Only at the next Panel start: in the two management-off cells the Panel started again after the reboot (management return) and logged "mail SNI reconciled from 0 active secure-mail certificates…" and "milter chain: milters=\"inet:localhost:11332\" maps=hash" (Debian 23:51:00/23:51:05, Ubuntu 02:55:21/02:55:26). In every other cell no later start happened before the cell ended, so the work stayed skipped at the end of the cell (F2). The Agent never answered `package_manager_active` in any update. | `side/changed-paths.txt`; `upd1-*-mgmt-off-reboot/run-a/steps/18-collect/journal-product.txt` |
| PackageKit on Ubuntu (probe on) | Setup reached `access_dns` in **one** attempt in all six Ubuntu B runs, after H19's single owner wait (278-290 s) for an idle `packagekitd`; every reading mapped only `/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so`, no child, no lock held by it; readiness `ready` 15-17 times per run, `package_manager_active` 0 times; at arm no `packagekitd` ran in any cell (so no update started with the daemon alive). B80 (alpha.80) setup: 7 attempts, 45 min (upd8 F1, the baseline's own rule). | `result.json` `packagekit`, `steps/*/packagekit-observations.json` |

### What each kind showed

**Terminal checks common to every complete run** (`summary-per-cell.txt`): installed = running for Panel and Agent, the
expected build identity (G after a forward end, B or B80 after a rollback), fresh owner login, database
`equal-except-volatile` (only `metrics_samples`, `server_setup_executions` differ; `unexpected: []`), timers equal
(2 compared on Debian/Arch, 3 on Ubuntu incl. `certbot.timer` active/enabled on Debian/Ubuntu, `certbot-renew.timer`
disabled on Arch), `table inet celikpanel_fw` equal, site marker served, mailbox and SMTP on Debian/Ubuntu, cron row
present. Panel API and root CLI agreed whenever both answered, except the H21 race (15a).

| Kind | Debian 13 | Arch | Ubuntu 24.04 |
| --- | --- | --- | --- |
| Good G | verified; Panel down 20-30 s; site, SMTP, cron never interrupted | verified; Panel down 25-35 s; site, cron never interrupted (no mail) | verified; Panel down 15-25 s; site, SMTP, cron never interrupted |
| Migration defect D + second fault | attempt 1 `update/active` 22:40:58, QMP reset at `payload_restored` (SSH back 8.5 s), attempt 2 `rollback/active` 22:41:59, `rollback_verified` 22:42:20; site/SMTP interrupted only by the reset (≤ 23 s); Panel down 93-103 s | attempt 1 00:11:37, SIGKILL at `runtime_verified`, attempt 2 `rollback/completion` 00:12:24, verified 00:12:38; site never interrupted; Panel down 25-35 s + 0-10 s | attempt 1 01:44:57, reset (SSH 9.9 s), attempt 2 01:46:01, verified 01:46:21; site/SMTP only at the reset (≤ 22 s) |
| Start check S + second fault | `candidate_panel_startup_check_failed … tls_pair_invalid`; attempts 22:54:00 / 22:55:00 (reset, SSH 8.3 s); verified 22:55:18 with `failure_code` kept; kind as-expected | attempts 00:21:49 / 00:22:36 (SIGKILL); verified 00:22:48; as-expected | 15a: attempts 02:30:25 / 02:31:42 (reset, SSH 15.9 s), verified 02:32:07, as-expected (H21 race only); 15b: attempts 05:50:35 / 05:51:41, verified 05:52:00, as-expected (verdicts invalid, P1) |
| Real start R (forward only) | 4b: start 23:10:13; three forward attempts `update/completion` 23:11:58, 23:13:40, 23:15:23 with `retry_scheduled` between; renewal restored and `pause_pending` 23:16:36/23:16:45; `paused_retry_limit` 23:17:08, `renewal_before_update=on`; no rollback; site, SMTP, cron never interrupted; Panel down from 23:10:25 to the end (the candidate never listens); kind as-expected | not in scope | not in scope |
| Owner continuation (held port) | attempts 23:29:58, 23:31:39, 23:33:19; `pause_pending` 23:34:32; pause 23:34:49; port released 23:35:26.9 (held 413.3 s); printed retry run once 23:35:30, rc 0; `succeeded/update_verified` 23:35:54; Panel down 427-437 s; site/SMTP/cron never interrupted | attempts 00:33:48, 00:35:28, 00:37:08; `pause_pending` 00:38:19; pause 00:38:36; released 00:39:14.8 (408.3 s); owner retry 00:39:18; verified 00:39:38; Panel down 416-426 s | attempts 02:05:10, 02:06:54, 02:08:38; released 02:10:50.8 (431.3 s); owner retry 02:10:55; verified 02:11:23; Panel down 444-454 s; site/SMTP/cron never interrupted |
| Management off + one orderly reboot | SSH 8.3 s; 185 s window: site served (first OK 12.4 s after boot), SMTP and the owner's MariaDB row 38/38, cron 3 in-boot stamps, `certbot.timer` active/enabled kept, firewall equal; return: ready in 4 reads, no difference | 11a: SSH 83.8 s, cron late (H20); 11b: SSH 41.5 s; site and DB row 38/38, cron 2 stamps, `certbot-renew.timer` disabled kept, firewall equal; return ready, no difference | SSH 10.1 s; site, SMTP (first OK 15.7 s), DB 38/38, cron 4 stamps, `certbot.timer` active/enabled kept, firewall equal; return ready, no difference |

The firewall after each reboot comes from `celikpanel-firewall-restore.service`, which management-off does not
disable (as upd4).

**Part 2, from the published `v0.1.0-alpha.80` source (B80 = tag + licence seam):**

- Debian good (17): verified 11:40:32; the root CLI did not exist for ≈ 10 s, then `observation_unavailable` ≈ 7 s,
  then `running` (record present for an alpha.80 worker; see the changed-paths table); cron is not installed by
  alpha.80 on Debian (`not-available-on-baseline`), site and SMTP never interrupted, Panel down 20-30 s.
- Debian defective + reset (18): attempts 11:53:52 (`update/active`) and 11:55:01 (`rollback/active`, after the reset,
  SSH 8.8 s), `rollback_verified` 11:55:25; the restored Agent and Panel are the alpha.80 builds (`v0.1.0-alpha.80`,
  b9fe1037) and served; site/SMTP interrupted only at the reset. The alpha.80 Panel then shows the raw updater line as
  its update `summary`, answers `/api/v1/recovery/status` with 404 and offers the same defective v0.1.0-alpha.81 again
  (`available: true`); card judge `unknown` (alpha.80 has no `lib/systemUpdateOutcome.ts`) - upd7 F1, unchanged and
  not fixable in the installed alpha.80.
- Ubuntu good (19): verified 12:46:01 after alpha.80's own setup needed 7 owner attempts (three `HOST_MUTATION_BUSY`,
  two `mail_profile_install_failed`, one `server_setup_firewall_failed`, then the `access_dns` wait; H19 waits of
  261-300 s for an idle `packagekitd` each) - upd8 F1, a property of the alpha.80 baseline.
- Debian owner continuation (20, optional, time allowed): record `running` 12:57:46 (alpha.80 worker); forward attempts
  12:59:11, 13:00:51, 13:02:30; renewal restored and `pause_pending` 13:03:34-40; `paused_retry_limit` 13:04:08; port
  released 13:04:44.96 (held 419.0 s); printed retry run once 13:04:47; `succeeded/update_verified` 13:05:09; Panel
  down 429-439 s; site and SMTP never interrupted; kind as-expected. The same sequence as upd7, now on `48e54657`.

## Guidance observed (verbatim, EN / TR)

All texts are product output recorded by the driver: the root CLI (`sudo /usr/libexec/celikpanel/recovery status
--request-id <rid> --lang en|tr`) per sample, and the update card and recovery screen rendered by the driver from the
installed build's own catalogues and source (not a browser). The complete de-duplicated list, with the cells and first
times each text was seen, is `owner-texts.txt`; per-run lists are `<cell>/run-*/side/extract.txt`.

The forward-completion sequence (Debian real start and owner continuation; Arch and Ubuntu identical except the
renewal sentence of the journal), root CLI, in order:

- `recovering`, `failure_code=panel_start_unverified` - EN "The update was applied, but the new version's panel did
  not come up. The server owner should read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. The
  update's completion is retried automatically up to its limit; after that, sudo journalctl -u
  celikpanel-release-recovery.service --no-pager -n 50 shows a one-time retry command for this operation. There is no
  supported return to the previous version from this point." / TR "Güncelleme uygulandı, ancak yeni sürümün paneli
  açılmadı. Sunucu sahibi sunucudaki panel günlüğünü okumalı: sudo journalctl -u celikpanel-panel -n 50. Güncellemenin
  tamamlanması sınırına kadar otomatik olarak yeniden denenir; sonrasında sudo journalctl -u
  celikpanel-release-recovery.service --no-pager -n 50 bu işlem için tek seferlik yeniden deneme komutunu gösterir. Bu
  noktadan önceki sürüme desteklenen bir dönüş yok."
- `retry_scheduled` - the text above, then EN "The last automatic recovery attempt did not finish, and automatic
  recovery tries this same operation again by itself: the next attempt normally starts about 30 seconds after the
  previous one ended, up to three attempts in total. Nothing is needed on the server now: check this same request
  again in a minute and do not start another update. The server owner has to act only if automatic recovery pauses." /
  TR "Son otomatik kurtarma denemesi tamamlanmadı ve otomatik kurtarma aynı işlemi kendiliğinden yeniden deniyor:
  sonraki deneme normalde bir öncekinin bitişinden yaklaşık 30 saniye sonra başlar, toplamda en çok üç deneme yapılır.
  Şimdi sunucuda yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın ve başka güncelleme
  başlatmayın. Sunucu sahibinin ancak otomatik kurtarma durursa işlem yapması gerekir."
- `pause_pending` - the first text, then EN "The last admitted recovery attempt did not finish, and no automatic attempt
  remains. Recovery is finishing this attempt: the next state, with what the server owner needs to do and the one-time
  retry command, is recorded within about a minute. Nothing to do yet: check this same request again in a minute and
  do not start another update." / TR "İzin verilen son kurtarma denemesi tamamlanmadı ve başka otomatik deneme
  kalmadı. Kurtarma bu denemeyi kapatıyor: sunucu sahibinin ne yapacağını ve tek seferlik yeniden deneme komutunu
  içeren sonraki durum yaklaşık bir dakika içinde kaydedilir. Henüz yapılacak bir şey yok: bir dakika sonra aynı işlemi
  yeniden sorgulayın ve başka güncelleme başlatmayın."
- `paused_retry_limit` (Debian, `renewal_before_update=on`) - EN "The update was applied, but the new version's panel
  did not come up, and completing the update was retried to its limit. Read the panel log on the server: sudo
  journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed. There is no
  supported return to the previous version from this point. Automatic recovery used all three attempts without
  finishing, so the server may be between versions. The server owner must act: read sudo journalctl -u
  celikpanel-release-recovery.service --no-pager -n 50, fix the cause it names, then run the one-time same-operation
  retry command shown there. Checking status does not retry recovery. Automatic certificate renewal (Certbot) was
  stopped for this update; the same recovery journal says whether it was returned to how it was before the update or
  stays stopped until this operation finishes." / TR "Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve
  güncellemenin tamamlanması sınırına kadar yeniden denendi. Sunucudaki panel günlüğünü okuyun: sudo journalctl -u
  celikpanel-panel -n 50. Bu neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu noktadan önceki sürüme
  desteklenen bir dönüş yok. Otomatik kurtarma üç denemenin hepsini kullandı ve tamamlanamadı; sunucu iki sürüm arasında
  kalmış olabilir. Sunucu sahibinin işlem yapması gerekiyor: sudo journalctl -u celikpanel-release-recovery.service
  --no-pager -n 50 çıktısını okuyun, belirtilen nedeni giderin ve orada aynı işlem için gösterilen tek seferlik yeniden
  deneme komutunu çalıştırın. Durum sorgusu kurtarmayı yeniden başlatmaz. Otomatik sertifika yenileme (Certbot) bu
  güncelleme için durduruldu; aynı kurtarma günlüğü, güncellemeden önceki hâline döndürülüp döndürülmediğini ya da bu
  işlem bitene kadar durdurulmuş kalacağını söyler." The Arch text differs in its renewal sentence (renewal was off
  before the update); both are in `owner-texts.txt`.
- Journal at the pause: "Automatic recovery paused after three admitted attempts. No recovery child was started.
  Preserve evidence and inspect the recovery service journal." and the printed `sudo /usr/libexec/celikpanel/recovery
  recover --retry --snapshot <snapshot>`; after the owner's retry EN "The update completed and the new version was
  verified when it finished. Nothing else is needed on the server. Open the panel to check that everything works now." /
  TR "Güncelleme tamamlandı ve yeni sürüm bittiği anda doğrulandı. Sunucuda başka bir işlem gerekmiyor. Her şeyin şu an
  çalıştığını görmek için paneli açın."

Rollback cards (all three platforms identical):

- Migration defect: EN "Update not completed; previous version restored || The update to v0.1.0-alpha.82 did not
  complete. The server was returned to v0.1.0-alpha.81 automatically and is running it now. || Cause: the update failed
  before it completed; the server recorded no more specific cause. || Nothing needs to be done on the server. Do not
  start the update to v0.1.0-alpha.82 again until a corrected version is published. If the server's message names a
  problem on this server, fix it first. || Nothing resumes by itself: the server keeps running v0.1.0-alpha.81. When a
  newer version is published, “Check for updates” offers it. || The server reported: reviewed updater failed: exit
  status 1: offline panel database migration failed; its original database and work evidence are preserved" / TR
  "Güncelleme tamamlanmadı; önceki sürüm geri yüklendi || … || Sunucunun İngilizce günlük satırı: reviewed updater
  failed: exit status 1: offline panel database migration failed; its original database and work evidence are
  preserved". The TR card now labels the English line as the server's English log line (upd4 O7 addressed).
- Start check: EN "… || Cause: the new version's panel failed its start check before anything was switched on. || …" /
  TR "… || Neden: yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi. || …";
  recovery screen EN "Rollback verified || The new version's panel failed its start check before anything was switched
  on, so the server was returned to the previous version automatically. The previous version keeps running and
  nothing needs to be done on the server. When you report this, include the reason line shown for this update on the
  update page. || Previously recorded failure: The new version's panel failed its start check".
- Good and owner-continuation: card "The update completed. The panel and agent restarted on the new version." /
  "Güncelleme tamamlandı. Panel ve agent yeni sürümle yeniden başladı."; screen "Update verified || The server verified
  this update. Reload CelikPanel to load the installed interface." / "Güncelleme doğrulandı || Sunucu bu güncellemeyi
  doğruladı. Kurulu arayüzü yüklemek için CelikPanel’i yeniden yükleyin."

Setup refusal in 4a (`05-mail_profile`): API "This server's package manager is busy — a package task is still running
on this server. Wait for it to finish, then try again."; catalogue `err.HOST_MUTATION_BUSY` EN "Another server change
or operating-system package task is still running. Wait for it to finish, then try again." / TR "Başka bir sunucu
değişikliği veya işletim sistemi paket işlemi hâlâ sürüyor. Tamamlanmasını bekleyip yeniden deneyin."

## Findings

| # | Class | Finding | Evidence |
| --- | --- | --- | --- |
| F1 | candidate product limitation, already open (D-024; RESILIENCE-CONTRACT "Open: a setup step refused for real package activity still fails instead of waiting and resuming … which package task blocks is not named") - first seen natively on Debian 13 | The webmail profile's own package install (dovecot, php8.4-*) ended at 22:59:51; ≈ 10 s later its `mail-tls` sub-step was refused: panel log "service operation … (webmail) failed in profile/webmail/mail-tls: mail TLS synchronization: another server change or package-manager task is still running", step "05-mail_profile failed: HOST_MUTATION_BUSY: This server's package manager is busy …". Debian has no PackageKit; the busy process is not named anywhere (the Debian cells had no probe). The owner gets "wait … then try again" with no automatic resume; the setup does not continue by itself. Intermittent: the same step passed in 9 other Debian runs of this build. The real-start cell was re-run once (4b) and measured. | `upd1-debian13-realstart/run-a/steps/06-setup/step.json`, `…/14-collect/journal-product.txt:235-236` |
| F2 | candidate product observation (open; D-022/D-024), cause known | Every Panel start inside an update or rollback on a host with a mail stack skips the mail SNI snapshot publication and milter wiring ("another server change or package-manager task is still running"): the update/recovery holds the host mutation lease, and both run only at Panel start (`cmd/panel/cert_startup_reconcile.go:164-181`, `cmd/panel/main.go:1414-1455`). The skipped work completed at the next Panel start (management-off cells), and not before the end of any other cell. In these fixtures there were 0 active secure-mail certificates, so nothing observable depended on it; whether an owner with mail certificates would serve stale SNI/milter configuration until the next Panel restart is not measured. No owner-visible text mentions it. | `side/changed-paths.txt` of every Debian/Ubuntu run; `upd1-debian13-mgmt-off-reboot/run-a/steps/18-collect/journal-product.txt:539-549` |
| F3 | incompatibility with alpha.80 (= upd7 F1, expected) | After the automatic return to alpha.80 its Panel shows the raw updater line, has no recovery reader (404) and offers the same defective v0.1.0-alpha.81 again; only the root CLI says not to start it. | `part2-alpha80/upd1-debian13-defective/run-a/steps/13-terminal/step.json` |
| F4 | incompatibility with alpha.80 (= upd7 F2/upd8 F4, expected) | ≈ 10 s without the recovery CLI, then ≈ 7 s `observation_unavailable` after the owner's start; then `running`. | `part2-alpha80/*/run-a/side/extract.txt` |
| F5 | published-baseline limitation (= upd8 F1) | alpha.80's own setup on stock Ubuntu 24.04 needs 7 owner attempts (45 min) because its Agent counts the idle `packagekitd` as busy. The candidate's setup (B) needed one attempt in all six Ubuntu runs. | `part2-alpha80/upd1-ubuntu-good/run-a/steps/06-setup/step.json` |
| H20 | harness defect, fixed | See "Run copies and harness changes" (cron window on a slow boot). | `upd1-arch-mgmt-off-reboot/run-a` |
| H21 | harness defect, fixed | See above (final-sample race). | `upd1-ubuntu-startcheck/run-a/steps/11-track/samples/0028.json` |
| P1 | platform limitation (host) | The Windows host entered modern standby/sleep repeatedly from ≈ 03:00Z (power-source change 03:09Z, sleep 04:09:58Z, resumes up to ≈ 05:47Z; System log, Kernel-Power 506/507/42/107). Cell 15b's seed step therefore spanned 03:08:51-05:48:16 and the guest sampler has no samples 03:13-05:49; the verdict cut then also labels the real reset at 05:50:51 "unexplained", and the cell fails `verdicts`. Its product path (rollback with `failure_code`, kind as-expected, terminal passed) is recorded but its workload verdicts are invalid; it was not re-run (one re-run already used). From 11:28Z the run held a process-level "system required" request (`SetThreadExecutionState`, no setting changed) until it ended. | `upd1-ubuntu-startcheck/run-b/steps/15-verdicts/step.json`, `result.json` steps |

No candidate product defect was found in the update, rollback, forward completion, pause, owner retry or
management-off paths of `48e54657`.

## Real origin, licence, secrets

- Every origin check resolved `celikpanel.net` only to `127.0.0.1` with fixture HTTP 200 (`result.json` `scope.origin`);
  the baseline installer logs name neither `celikpanel.net` nor `185.95.` (`secret-scan.txt`).
- Licence: `license_service: not contacted` in every licence step; every Panel logs the acceptance-fixture banner.
- The guests used Debian's, Arch's and Ubuntu's package mirrors (installer, setup); the host downloaded nothing.
- `secret-scan.txt` (`tools/scan11.py`, `tools/scan.sh`) over the whole folder before hashing: **clean.** 5,836 files; 0 PEM private-key blocks; 0
  hits for the 1,312 body lines of the 92 key files of the 23 labs (SSH keys, fixture signing, CA and TLS keys); 0
  unredacted password/secret/token/session/cookie fields; 3,592 redaction markers. The one fixture-licence literal
  (and the one `CPK-` shape) is `build/a80/fixture-patches.diff`: the D-027 seam carries `AcceptanceFixtureKey`, the
  constant committed in `internal/licensing/acceptance_fixture.go` (public source, accepted only by the
  acceptance-tag build on a marked guest; as upd7). The 8 tokens of the admin-password shape are one lab SSH **public**
  key prefix and upd3 snapshot names in a run-copy hash list; the one 32-character token is a substring of a lab's SSH
  **public** key in `part2-alpha80/upd1-debian13-good/run-a/host/fixture-plan.json`. `185.95.` appears only in this
  README, the scan scripts and the scan output. Longest repository-relative path: 211 characters.
- Correction made before hashing: the common staging first wrote `build/a80/fixture-patches.diff` as the diff from the
  alpha.80 tag to the candidate source (≈ 1.7 M lines of committed repository history, 268 MB, which also tripped the
  scan with committed test fixtures). It was regenerated by `tools/a80diff.sh` as upd7 records it: tag → B80 (the six
  seam files), source `48e54657` → G81 (the release policy line), G81 → D81 (the defect); the scan above is over the
  corrected folder.
- `SHA256SUMS` lists every file except itself (`sha256sum -c SHA256SUMS`).

## Removals (disk), host leftovers, host state

To keep the WSL disk bounded, after each run's evidence was staged into this folder and the staged copy's driver
`SHA256SUMS` verified, `tools/rmoverlay11.sh` removed **only the overlay disks** (`cells/*/<node>/overlay.qcow2`) of
the lab this run had created for that cell (refusing any lab not named `upd11-*`, any lab without an upd11 job, a
running QEMU, or a staged copy that did not verify): 39 files, 41.5 GB, in all 23 labs
`/var/tmp/cp-release-drill-upd11-*`. Each removal (path, bytes, time) is listed in `host/removals.txt`. Consequence:
the guests cannot be restarted and raw files that stayed only on a guest (e.g. the port-hold events file, upd4 H15)
are gone; everything the driver collected is here. Nothing that existed before this run was modified or deleted.

Host leftovers (`host/host-leftovers.txt`): `/var/tmp/cp-upd11-run` (1.2 GB: run copies `harness`, `harness-h20`,
`harness-h21`, overlays, jobs, logs, build records); the 23 stopped labs without overlays (661-921 MB each: base image
copies, keys, evidence, logs); the build clones `/var/tmp/cp-upd1-build/20261001t221215z` (757 MB) and
`…/20261001t221533z` (762 MB); eight dist folders `/var/tmp/cp-pair-accept/dist/<commit>-acceptance-license` (729 MB
each, 133 MB for B80) written by `build-dist.sh`. No QEMU process remains; no dry-run lab exists. Git: HEAD
`48e54657` throughout, the local config key list unchanged (fingerprint `4363b888…`, as upd8-upd10); the repository
writes are the two harness files (working tree, overlays above) and this folder. No inline `wsl.exe` command
contained `$`; host steps ran from LF script files. Windows host: one background PowerShell process held
`SetThreadExecutionState(ES_CONTINUOUS|ES_SYSTEM_REQUIRED)` from 11:28Z to the end of the run (no setting changed).

## Files

`build/` (`cur/` and `a80/`: two artifact documents with dist JSONs, fixture commits and patches, build logs; the alpha.80 unchanged-file
proof; prove, dry runs, offline logs), `harness-run-copy/` (run-copy hashes, the three overlays with files and diffs,
cell jobs, the scripts that made the copies and started the jobs), `tools/` (host-only readers: extraction,
changed-path scan, summaries, texts, diagnosis scripts, staging, removal, scan, sums, the C: reading script), `host/`
(host check, C: readings, removals log, leftovers, staging log), one folder per cell run with the driver's evidence
unchanged (its own `SHA256SUMS` verified when staged), `host/` (wrapper logs, job, harness, lab identity, baseline
installer result and log, origin intent and manifest, fixture plan) and `side/` (`extract.txt`, `changed-paths.txt`);
part 2 runs under `part2-alpha80/`. `summary-per-cell.txt`, `step-table.md`, `outage-windows.txt`, `owner-texts.txt`,
`secret-scan.txt`, and `SHA256SUMS` over every file except itself.

## What this run does NOT prove

- That the kinds always end this way: one complete run per kind and platform (two each for the cells re-run), on a
  laptop host with the acceptance fixture, a fixture signing key and a loopback origin. Production signing, the real
  release origin, the licence service, DNS (external mode) and certificate issuance or renewal execution are not
  exercised.
- The Ubuntu start-check cell has no clean run: 15a's only failure is the H21 sampling race, 15b's workload verdicts
  are invalid because the host slept (P1). Its product path (rollback, typed cause kept, kind as-expected) was recorded
  twice.
- Real start on Arch and Ubuntu (not in this matrix); owner continuation from alpha.80 beyond Debian; start check,
  real start and management-off from alpha.80; any Arch path from alpha.80.
- The changed paths not exercised: the curl refusal, the no-marker end after a child failure, the start check with a
  host-name or leading-zero listen address, an update started while an idle `packagekitd` runs, the writer of the
  initial record (inferred, not named).
- What F2's skipped startup work means for an owner with active secure-mail certificates.
- Which process made the Debian setup refusal (F1), and that it cannot happen inside an update.
- A cause the owner cannot remove, a second fault during the owner's retry, power loss, browser rendering (cards and
  screens are rendered from the build's catalogues and source by the driver), the SSH owner view.
- The signed alpha.80 archive itself (B80 is the tag rebuilt with the licence seam).
- Panel removal or uninstall: management-disabled across one orderly reboot is the only absence condition.

It closes no P0 row. Wall time: host check 22:10Z; cells 1-16 22:18Z-02:55Z; 15b 02:55Z-05:52Z (host asleep in
between); part 2 11:28Z-13:05Z.
