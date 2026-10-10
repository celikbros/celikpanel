# upd13 native run, 2026-10-02/03: the whole owner-started update matrix once more on the release candidate (`f6cdd5a0`)

Thirteenth owner-started update run (`owner_update_trial.py`). upd11 ran the whole matrix on `48e54657`; since then the
Panel gained the deferred retry of its startup mail steps (`6b6f8a0c`, `cmd/panel/startup_deferred_mail.go`, measured
natively on three Debian cells only in upd12) plus harness and documentation commits. This run builds **everything
from commit `f6cdd5a0`** (run copy `git archive f6cdd5a0`; both build clones at `f6cdd5a0`; repository HEAD stayed
`f6cdd5a0` for the whole run) and runs every cell of the matrix once, so the candidate has one complete run on its
exact code.

Disposable QEMU/KVM guests on the local `archlinux` WSL host only. No installed server (Boston, Frankfurt, any) was
touched or contacted. Nothing was committed, pushed, published or signed with a production key. `celikpanel.net`
resolved only to the guest-loopback fixture origin; no licence service was contacted. Every `result.json` carries
`native_evidence: false`; the owner judges the P0 rows. **It closes no P0 row.**

**Result in one sentence:** on `f6cdd5a0` every cell of the matrix reached its expected product end - good update
verified (Debian 13, Arch, Ubuntu 24.04; from alpha.80 on Debian and Ubuntu), automatic rollback with the platform's
second fault (migration defect and start check on three platforms; migration defect from alpha.80 on Debian), forward
completion paused at its limit with the typed first cause (Debian real start), the owner's one printed retry completing
the paused update (three platforms and from alpha.80 on Debian), and the workloads served with management off across a
reboot (three platforms) - and in every Panel that started inside an operation on a mail stack and kept running past
+30 s (11) the deferred mail retry completed both steps in its first attempt (+38.2-40.3 s), once, after the operation
had already recorded its terminal state (in the two management-off cells the owner stopped the Panel first; the work
ran at its next start). One non-product stop: a harness race on Arch (H22), fixed and re-run once.

## What was built from what

Two builds from the run copy (`go1.26.5 linux/amd64`), every archive `license_mode: acceptance-fixture`
(`build/cur/`, `build/a80/`: artifact documents, dist JSONs, fixture commits and patches, build logs `build.*.txt`).
`/root/.cache/go-build` had been emptied before the run; the first build took 4 min 27 s.

| Build | Role | Label / seq | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `run-upd1.sh build f6cdd5a0` (20:30:47-20:35:14Z) | Baseline B | v0.1.0-alpha.81 / 81 | 4a379faed756b87070a7f621787e9cbe0eef7d04 | d9dffd8de34e04fcaf6466c944403693efc3dd70 | 056c58dcbc5a3f0fbbb8291543bfd0bce7c3f2d6b7ed872cd67c9e29c5690e31 |
| | Good G (parent B) | v0.1.0-alpha.82 / 82 | 47c0b4c2d68ae75258a36dad469c2a41758e1e1c | ec151f24bf371ed4edec40997c35bc07c67d90a4 | 9238b386ed0ff6eee055efe4a32c698327bbf7fe5eaa6bce452e0087ee0e4c90 |
| | Defective D (parent G) | v0.1.0-alpha.82 / 82 | 6acb22f6d5ce471d57131ed5ecff24c85b9d8844 | 218733c25b6836e832e36bbd965310feb81effb6 | 42021ef5402c9666b128344fc58f0c2989bfd06ad8af8cea32d7128508cf54b9 |
| | Start-check S (parent G) | v0.1.0-alpha.82 / 82 | 993149f75c883c1c62bdfef536c2b5d42c85d059 | 224813c0e7e37711ae18a5a92c5a4ff54bc2bda7 | 1dbd333d6e9e0c34b973bf849acf39d888ee6699252fe635bfe188d03ad4439b |
| | Real-start R (parent G) | v0.1.0-alpha.82 / 82 | b139dbf3946e5c1042ed61e6f26290b8a944325f | d671cb31e5a06241a9e9083ae970bbcc45213ee6 | f8ac1437cf30ef574a579cdd099cc592c1e39898a04e81b5482e948f6759c886 |
| `run-upd1.sh build --baseline-ref v0.1.0-alpha.80 f6cdd5a0` (20:35:14-20:37:22Z) | Baseline B80 = tag `v0.1.0-alpha.80` (bd14d97e) + D-027 licence seam | v0.1.0-alpha.80 / 80 | 82958d4a730a50b07fc0983a3947e1fa7b9528e4 | ee6d75f688fac425179ee646a647a0ceb68283bc | 57219d269527c7491cc9192cd20bc3e5e5402e515e875849389f4fc0c08f39e9 |
| | Good G81 (parent B80) | v0.1.0-alpha.81 / 81 | 56bc29094bc37d3c74e3cee66677958ce38d4d27 | eea2d39d811497db86c034f2770e8e2b53298aa6 | 1866a9b3f18c61e7f8080c531e9581bc467a9fc8a9a654e6e7ad91fd8b9d8970 |
| | Defective D81 (parent G81) | v0.1.0-alpha.81 / 81 | 681efd709b56ad114d752967feac2d66bb99894c | fcc151de4b3a4dd1fa67bd68043f560de53b6072 | bbadbe3c47fe8e0316163275166efcd9f80d5c4382efc8db2b52809e29d0d48d |

- Source HEAD `f6cdd5a0c27ca865a2dc50239b106046e13739b7` in both artifact documents (`build/source-commit.txt`). B80's
  tree `ee6d75f6…` is the same tree as upd7/upd8/upd11's B80 (same tag, same seam; `build/a80/baseline-ref-proof.txt`).
  B, G, G81 carry the `f6cdd5a0` Panel unchanged apart from the release-policy label (`build/*/fixture-patches.diff`),
  so every Panel that runs after a forward end or an owner retry, and the restored B after a current-source rollback,
  contains the deferred retry; the restored B80 (alpha.80) does not.
- `prove` exited 0 for both documents (`build/cur-prove.json`, `build/a80-prove.json`); 16 + 4 dry runs exited 0 with
  `native_evidence: false` and created no lab (`build/cur-dry-run-*`, `build/a80-dry-run-*`; two more on the H22 copy,
  `build/h22-dry-run-*`; `build/prove-all.out.txt`).
- Offline suites (`build/offline-*`): pristine `f6cdd5a0` copy (`p`) and the H22 copy (`h22`) all OK -
  `test_owner_update_trial` 182 / 185, `test_recovery_candidate_archive` 11, `test_lab` 15,
  `test_current_worker_baseline` 7, `test_worker_fixture_origin` 15, `test_bound_worker_reboot` 11,
  `test_guest_bound_worker` 8.

## Run copies and the one harness change

Every harness change up to upd12 (PackageKit probe, H19-H21, the deferred-mail watch D1) is committed in `f6cdd5a0`,
so the first copy `harness` is `git archive f6cdd5a0` with **no overlay** (`harness-run-copy/runcopy-f6cdd5a0-files.sha256`,
42,236 files; `harness-run-copy/overlay/README.txt`). `PYTHONDONTWRITEBYTECODE=1`. No product file changed.

| Id | Defect | Kind | Handling | Cells |
| --- | --- | --- | --- | --- |
| H22 | `baseline_install` polls the installer status with `sudo` over SSH every 15 s and lets any failed read end the step. The Arch installer runs a full `pacman -Syu` (`deploy/test-bootstrap-update-contract.sh:412` pins that line), which replaces PAM; at 22:41:33 (18 s after the installer started) one status read got "sudo: unable to load /usr/lib/sudo/sudoers.so: libpam.so.0: cannot open shared object file" and the step became inconclusive. Three seconds later `collect` read all journals through `sudo` without an unavailable entry, and the same installer passed in the other 5 Arch runs and in upd11. | race in the harness's polling (measurement) | Second copy `harness-h22` = `git archive f6cdd5a0` + the two working-tree harness files (`harness-run-copy/overlay-h22/`: files with SHA-256 and `harness.diff` `3ad73def…`): a failed status read is recorded (`h22_failed_status_reads`: time, exit code, last stderr lines) and read again at the next poll; the 8th failed read still ends the step (`BASELINE_STATUS_READ_FAILURES_MAX`). Tests `H22BaselineStatusReadTests` (3). | arch-owner-continuation run-a kept (failed); **run-b** re-run once on `harness-h22` (passed; no failed read was recorded, so the tolerance itself was not exercised). Cells 12-20 also ran on `harness-h22` (job record `host/harness.txt` per run); none recorded a failed read. |

The working-tree `owner_update_trial.py` and `test_owner_update_trial.py` hold H22 uncommitted (= the overlay). The
cells ran one after another from a queue (`harness-run-copy/queue.sh`, list `list-all.txt`): C: reading before each cell
(start only with >= 40 GiB), a C: reading every 10 min during a cell (under 30 GiB would stop the queue), staging and the
overlay removal after each cell. From cell 11 the staging kept the overlays of a run that was not complete for
diagnosis (no later run needed it); the arch-owner-continuation run-a overlays had already been removed by the first
version, so its guest could not be inspected afterwards.

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`, GiB; `host/c-drive.txt`)

64.4 free at the start (20:29:56Z), **lowest 55.4** before cell 1 (20:38:24Z, after both builds), 58.6 before cell 10
(22:40:02Z), 65.6 after the cells (02:32:07Z), **65.5 at the end** (02:44:06Z, after staging). Every reading before a cell was >= 55.4; the 10-minute
readings during the cells never fell under 58.6. Overlay removals inside the WSL disk were reused/returned as the
readings show (C: went back to 64.2 within cell 1).

Host power: a process-level "system required" request (`tools/keepawake.ps1`, `SetThreadExecutionState`, no setting
changed) was held 20:29:57Z-02:32:33Z (`host/keepawake.log`). The System log has one Kernel-Power 506 ("entering modern
standby") at 21:28:18Z and no resume event (`host/sleep-events.txt`); the samplers of the cell then running
(debian13-realstart) have no gap over 5.0 s, and no sampler of any run has a gap that is not the cell's own reset,
reboot or management-off window (`sampler-gaps.txt`). The host did not sleep.

## Cells

One new lab per cell, stopped by the wrapper; times are the wrapper's (UTC; `cells-table.md` is generated from the runs).

| # | Cell (folder) | Lab, port | Harness | Wrapper (UTC) | Outcome | Overall |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | upd1-debian13-good run-a | upd13-d13-good-a, 3711 | base | 20:38:24-20:50:30 | `update-verified` | complete-for-review |
| 2 | upd1-debian13-defective run-a | upd13-d13-def-a, 3721 | base | 20:50:35-21:04:52 | `recovered-automatically`; attempts `update/active` 21:02:04, QMP reset at `payload_restored` (SSH back 8.3 s), `rollback/active` 21:03:03; `rollback_verified` 21:03:26 | complete-for-review |
| 3 | upd1-debian13-startcheck run-a | upd13-d13-sc-a, 3731 | base | 21:04:57-21:18:45 | `recovered-automatically` (`candidate_panel_startup_check_failed` kept), 21:16:01 / reset (8.3 s) / 21:17:00; verified 21:17:19; kind as-expected | complete-for-review |
| 4 | upd1-debian13-realstart run-a | upd13-d13-rs-a, 3741 | base | 21:18:51-21:35:22 | `paused-owner-action-required`: forward attempts 21:30:03, 21:31:42, 21:33:22; `pause_pending` 21:34:33; `paused_retry_limit` 21:35:13 (`renewal_before_update=on`); kind as-expected | complete-for-review |
| 5 | upd1-debian13-owner-continuation run-a | upd13-d13-oc-a, 3751 | base | 21:35:28-21:55:25 | `recovered-after-owner-continuation`: 21:48:11, 21:49:57, 21:51:38; `pause_pending` 21:52:50; pause 21:53:07; port held 426.0 s, released 21:53:44.2; owner retry 21:53:47; `update_verified` 21:54:09; kind as-expected | complete-for-review |
| 6 | upd1-debian13-mgmt-off-reboot run-a | upd13-d13-mr-a, 3761 | base | 21:55:30-22:10:16 | `update-verified` 22:06:10; management off 185 s served; return ready; kind as-expected | complete-for-review |
| 7 | upd1-arch-good run-a | upd13-arch-good-a, 3771 | base | 22:10:20-22:19:11 | `update-verified` 22:19:01 | complete-for-review |
| 8 | upd1-arch-defective run-a | upd13-arch-def-a, 3781 | base | 22:19:14-22:29:42 | `recovered-automatically`: `update/active` 22:28:34, recovery child SIGKILLed at `runtime_verified`, `rollback/completion` 22:29:20; verified 22:29:32 | complete-for-review |
| 9 | upd1-arch-startcheck run-a | upd13-arch-sc-a, 3791 | base | 22:29:46-22:39:58 | `recovered-automatically` 22:38:51 / SIGKILL / 22:39:37; verified 22:39:48; kind as-expected | complete-for-review |
| 10a | upd1-arch-owner-continuation run-a | upd13-arch-oc-a, 3801 | base | 22:40:02-22:41:37 | not reached: `baseline-install` inconclusive (H22); no update started | incomplete (harness) |
| 10b | upd1-arch-owner-continuation run-b | upd13-arch-oc-b, 3921 | h22 | 02:16:14-02:31:41 | `recovered-after-owner-continuation`: 02:25:47, 02:27:26, 02:29:04; `pause_pending` 02:29:57; pause 02:30:32 (`renewal_before_update=off`); owner retry 02:31:12; `update_verified` 02:31:31; kind as-expected | complete-for-review |
| 11 | upd1-arch-mgmt-off-reboot run-a | upd13-arch-mr-a, 3811 | base | 22:41:38-22:54:45 | `update-verified` 22:50:45; management off 185 s served; kind as-expected | complete-for-review |
| 12 | upd1-ubuntu-good run-a | upd13-ub-good-a, 3821 | h22 | 22:54:49-23:13:20 | `update-verified` 23:12:03 | complete-for-review |
| 13 | upd1-ubuntu-defective run-a | upd13-ub-def-a, 3831 | h22 | 23:13:24-23:33:32 | `recovered-automatically`: 23:30:46 / reset (9.1 s) / 23:31:47; verified 23:32:05 | complete-for-review |
| 14 | upd1-ubuntu-startcheck run-a | upd13-ub-sc-a, 3841 | h22 | 23:33:36-23:53:35 | `recovered-automatically` 23:50:52 / reset (8.5 s) / 23:51:52; verified 23:52:09; kind as-expected; agreement passed (the first clean Ubuntu start-check run; upd11 had none) | complete-for-review |
| 15 | upd1-ubuntu-owner-continuation run-a | upd13-ub-oc-a, 3851 | h22 | 23:53:40-00:18:55 | `recovered-after-owner-continuation`: 00:11:50, 00:13:29, 00:15:08; `pause_pending` 00:16:02; pause 00:16:37; held 406.7 s; owner retry 00:17:17; verified 00:17:38; kind as-expected | complete-for-review |
| 16 | upd1-ubuntu-mgmt-off-reboot run-a | upd13-ub-mr-a, 3861 | h22 | 00:19:00-00:40:05 | `update-verified` 00:35:58; management off 185 s served; kind as-expected | complete-for-review |
| 17 | part2-alpha80/upd1-debian13-good run-a | upd13-a80-d13-good-a, 3871 | h22 | 00:40:10-00:53:27 | `update-verified` 00:52:01 | complete-for-review |
| 18 | part2-alpha80/upd1-debian13-defective run-a | upd13-a80-d13-def-a, 3881 | h22 | 00:53:31-01:06:22 | `recovered-automatically` to alpha.80: 01:04:54 / reset (8.3 s) / 01:05:52; verified 01:06:12 | complete-for-review |
| 19 | part2-alpha80/upd1-debian13-owner-continuation run-a | upd13-a80-d13-oc-a, 3891 | h22 | 01:06:26-01:26:10 | `recovered-after-owner-continuation`: 01:18:57, 01:20:36, 01:22:15; `pause_pending` 01:23:21; pause 01:23:55; owner retry 01:24:34; verified 01:24:54; kind as-expected | complete-for-review |
| 20 | part2-alpha80/upd1-ubuntu-good run-a | upd13-a80-ub-good-a, 3901 | h22 | 01:26:14-02:16:08 | `update-verified` 02:14:51 (alpha.80 setup: 7 owner attempts, 45 min, as upd8/upd11 F5) | complete-for-review |

Every cell of the requested matrix ran; none was skipped. **Terminal checks in every complete run**
(`summary-per-cell.txt`): installed = running for Panel and Agent with the expected identity (G after a forward end, B
or B80 after a rollback), fresh owner login, database `equal-except-volatile` (`metrics_samples`,
`server_setup_executions`; `unexpected: []`), timers equal (2 on Debian/Arch, 3 on Ubuntu), `table inet celikpanel_fw`
equal, site marker, mailbox and SMTP on Debian/Ubuntu, cron row (not installed by alpha.80 on Debian:
`not-available-on-baseline`). Panel API and root CLI agreed in every run with both readers; the alpha.80 rollback (18)
has no recovery reader after the return (agreement `inconclusive`, as upd11). Workloads (`outage-windows.txt`): site,
SMTP and cron never interrupted except by the cell's own QMP reset (<= 23 s) or orderly reboot; Panel down 10-35 s for
a good update, 25-40 s + 0-10 s with the Arch SIGKILL, 84-105 s with a reset, 402-442 s while the port was held. Ubuntu PackageKit probe: setup reached `access_dns` in one
attempt in all five Ubuntu B runs; the Agent never answered `package_manager_active`; at arm and at the owner's start no
`packagekitd` ran in any Ubuntu cell (`steps/09-arm`, `steps/10-owner-start` `packagekit_*` checks, `result.json`
`packagekit`). Management off (6, 11, 16): site, the owner's DB row and (Debian/Ubuntu) SMTP served from the first
sample of the new boot (Debian 7.1 s, Arch 11.9 s, Ubuntu 8.7 s after boot) through the 185 s window, cron 3 in-boot
stamps each, renewal timer state kept (`certbot.timer` active/enabled; Arch `certbot-renew.timer` disabled), firewall
equal, nothing needed the Panel; return ready in 2-5 reads with no difference.

## The deferred mail retry: timeline in every cell with mail

From each run's collect journal parsed with the harness's own `deferred_mail_view`, the watch record and the track
samples (`deferred-mail-timeline.txt`, `<run>/side/deferred-mail-timeline.txt`). "+s" is from the Panel's
"Starting CelikPanel Backend..." line. "Terminal" is the first root-CLI sample with the operation's terminal state.

| Run | Panel inside the operation | Both startup lines with the retry sentence | Terminal (CLI) | Attempt line | Lines / repeat in 45 s settle |
| --- | --- | --- | --- | --- | --- |
| debian13-good | G `panel[37218]` 20:48:59.27 | +0.13 s | `update_verified` 20:49:13 | attempt 1 of 20, 20:49:39.54 (+40.28 s), both completed | 1 / none |
| debian13-defective | restored B `panel[4663]` 21:03:17.46 | +0.13-0.14 s | `rollback_verified` 21:03:26 | 21:03:56.48 (+39.02 s) | 1 / none |
| debian13-startcheck | restored B `panel[4741]` 21:17:14.22 | +0.11 s | `rollback_verified` 21:17:19 | 21:17:52.45 (+38.23 s) | 1 / none |
| debian13-owner-continuation | G `panel[56601]` 21:53:55.33 (inside the owner retry) | +0.09 s | `update_verified` 21:54:09 | 21:54:34.60 (+39.27 s) | 1 / none |
| debian13-realstart | 20 candidate Panel processes 21:29:02-21:34:xx | none: no process reached the startup mail steps | `paused_retry_limit` 21:35:13 | none | - |
| debian13-mgmt-off-reboot | G `panel[37776]` 22:05:55.74 | +0.12 s | `update_verified` 22:06:10 | **none**: the owner's management-off stopped the Panel at 22:06:24.75 (+29.0 s), before the first attempt (>= +30 s); after the reboot and management return `panel[2237]` 22:09:56.78 logged "mail SNI reconciled from 0 active secure-mail certificates…" (+4.69 s) and `milter chain: milters="inet:localhost:11332" maps=hash` (+9.70 s) at its own start | - |
| ubuntu-good | G `panel[53710]` 23:11:50.67 | +0.07 s | `update_verified` 23:12:03 | 23:12:29.42 (+38.76 s) | 1 / none |
| ubuntu-defective | restored B `panel[6018]` 23:31:59.96 | +0.08 s | `rollback_verified` 23:32:05 | 23:32:38.44 (+38.49 s) | 1 / none |
| ubuntu-startcheck | restored B `panel[6045]` 23:52:04.16 | +0.08 s | `rollback_verified` 23:52:09 | 23:52:42.62 (+38.46 s) | 1 / none |
| ubuntu-owner-continuation | G `panel[77975]` 00:17:25.09 (inside the owner retry) | +0.06 s | `update_verified` 00:17:38 | 00:18:03.58 (+38.49 s) | 1 / none |
| ubuntu-mgmt-off-reboot | G `panel[53351]` 00:35:46.45 | +0.07 s | `update_verified` 00:35:58 | **none**: Panel stopped by management-off at 00:36:13.27 (+26.8 s); after the return `panel[5135]` did both at its start (+4.57 s, +9.67 s) | - |
| a80 debian13-good | G81 `panel[36585]` 00:51:51.73 | +0.10 s | `update_verified` 00:52:01 | 00:52:30.58 (+38.85 s) | 1 / none |
| a80 debian13-defective | restored **alpha.80** `panel[4689]` 01:06:04.13 | the two refusal lines **without** the retry sentence (alpha.80 has no retry) | `rollback_verified` 01:06:12 | none (no retry exists; the work stays skipped until the next Panel start, not reached in the cell) | - |
| a80 debian13-owner-continuation | G81 `panel[56605]` 01:24:41.66 (owner retry) | +0.07 s | `update_verified` 01:24:54 | 01:25:20.37 (+38.71 s) | 1 / none |
| a80 ubuntu-good | G81 `panel[81228]` 02:14:41.37 | +0.07 s | `update_verified` 02:14:51 | 02:15:19.54 (+38.18 s) | 1 / none |

Every attempt line reads "startup mail work, attempt 1 of 20: mail certificate publication completed (mail SNI from 0
active secure-mail certificates); mail filter wiring completed (milters="inet:localhost:11332" maps=hash)"; no attempt
> 1, no failure line, no give-up line anywhere (`changed-paths-table.md`). Each watch saw the same five native files
change once (`main.cf`, `virtual.db`, `vmailbox.db`, `vmailbox_domains.db`, Dovecot `98-celikpanel-tls.conf`). The 20-21
held-port Panel processes of each owner-continuation cell and of real start never reached the mail steps. Arch has no
mail ("milter chain: nothing to wire at startup: no mail server is installed…").

**Was the operation's verification disturbed?** No, in any cell. In all 11 retries the root CLI already read the
operation's terminal state 5-14 s after the Panel start; the retry waits 30 s before its first readiness read, so it
began at least 16 s after that, and its attempt line came 25.6-33.6 s after the terminal state; no step of any
operation failed, every kind and terminal check passed, and the timings match upd11 (Debian good: owner start →
`update_verified` 63 s vs 57 s, Panel down 20-30 s vs 20-30 s; defective attempt 1 → 2 59 s vs 61 s, Panel down 91-101
s vs 93-103 s; owner continuation port held 426.0 s vs 413.3 s). The 5 s sampler saw no site or SMTP failure in any
retry window.

### Verbatim journal lines (debian13-good; every other retry identical apart from time and PID)

```
2026-10-02T20:48:59.395403+00:00 dns-debian13 panel[37218]: 2026/10/02 20:48:59 certificate startup reconcile: certificate dependents: publish full mail SNI snapshot: another server change or package-manager task is still running; preserve pending outbox: <nil>; the Panel retries this by itself every 30 seconds for up to 10 minutes once no other server change is running; nothing needs to be done now
2026-10-02T20:48:59.397465+00:00 dns-debian13 panel[37218]: 2026/10/02 20:48:59 milter wiring at startup: another server change or package-manager task is still running; the Panel retries this by itself every 30 seconds for up to 10 minutes once no other server change is running; nothing needs to be done now
2026-10-02T20:49:39.544887+00:00 dns-debian13 panel[37218]: 2026/10/02 20:49:39 startup mail work, attempt 1 of 20: mail certificate publication completed (mail SNI from 0 active secure-mail certificates); mail filter wiring completed (milters="inet:localhost:11332" maps=hash)
```

## Changed paths since upd11: were they exercised?

Read from each run's journals and root-CLI samples by `tools/cpaths.py` (`<run>/side/changed-paths.txt`; per-run counts
in `changed-paths-table.md`).

| Changed path | Exercised? | Where / evidence |
| --- | --- | --- |
| Deferred retry of the startup mail steps (`6b6f8a0c`) | **Yes, first-attempt path only**, in 11 Panels across Debian, Ubuntu and the alpha.80 source (after a good update, a rollback to B, a start-check rollback, and inside the owner's retry): one attempt line each, no repeat. **Cut short by a stop**: in both management-off cells the Panel was stopped before the first attempt and the work ran at the next Panel start, as before `6b6f8a0c`. **Not reached**: attempt > 1 (busy again), verified failure, give-up after 20 attempts, the pending-outbox domain retry. | table above; `deferred-mail-timeline.txt` |
| Startup refusal of the mail steps during an operation | **Yes**, every Panel start inside an operation with mail (upd11 F2's cause); now with the retry sentence on the candidate/B/G81 Panels and without it on the restored alpha.80 Panel | `changed-paths-table.md` |
| `pause_pending` and the renewal restore at a forward pause | **Yes**, all five forward-pause runs (Debian real start, owner continuation on three platforms and from alpha.80); Debian/Ubuntu "…(Certbot) was returned to its state from before the update…", Arch "…is already in its state from before the update." | `side/changed-paths.txt` |
| Start check (fixture `tls_pair_invalid`) | **Yes**, rollback with the typed cause on all three platforms; default listen form only (`CELIKPANEL_LISTEN=:2083`) | `upd1-*-startcheck/run-a` |
| Updater writes the initial record for an alpha.80 worker | **By inference only** (as upd11): no CLI ≈ 10 s, `observation_unavailable` ≈ 7 s, then `running` in the four alpha.80 cells; the updater's own line is not in any collected journal | `part2-alpha80/*/run-a/side/changed-paths.txt` |
| `/usr/bin/curl` preflight; no-marker end after a failed child | **Passing branch only / no** (as upd11): no refusal line and no "left no pending transaction" line in any run | `changed-paths-table.md` |
| Package activity inside an update (Agent `package_manager_active`) | **No**: never answered in any update | `changed-paths-table.md` |

## Guidance observed (verbatim, EN / TR)

All texts are product output recorded by the driver; the complete de-duplicated list with the cells and first times is
`owner-texts.txt`, per-run lists `<run>/side/extract.txt`. Root CLI (`sudo /usr/libexec/celikpanel/recovery status
--request-id <rid> --lang en|tr`), first sentences per state (each text ends with "This is a recorded observation;
current service health was not checked." / "Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi."
and the support line):

- `running/update_running`: EN "The update is being applied; the panel may be unavailable for a short time. Nothing to
  do now: check this same request again in a minute. Checking does not restart the update." / TR "Güncelleme
  uygulanıyor; panel kısa bir süre erişilemeyebilir. Şimdi yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi
  yeniden sorgulayın. Sorgulamak güncellemeyi yeniden başlatmaz."
- `recovering/recovery_running` (migration defect): EN "The update did not finish normally, and automatic recovery is
  running: the server is either being returned to the previous version or the update is being completed safely.
  Nothing to do now: check this same request again in a minute and do not start another update." / TR "Güncelleme
  normal biçimde tamamlanmadı ve otomatik kurtarma çalışıyor: sunucu ya önceki sürüme döndürülüyor ya da güncelleme
  güvenle tamamlanıyor. Şimdi yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın ve başka
  güncelleme başlatmayın."
- after the reset, `waiting_for=starting`: EN "The update did not finish normally, and recovery is waiting for the server
  to finish starting or stopping. Nothing to do now: the server's recovery timer checks this same operation again by
  itself when the system is ready. Recovery is not yet complete." / TR "Güncelleme normal biçimde tamamlanmadı ve
  kurtarma, sunucunun açılmasını ya da kapanmasını bitirmesini bekliyor. Şimdi yapmanız gereken bir şey yok: sunucunun
  kurtarma zamanlayıcısı, sistem hazır olduğunda aynı işlemi kendiliğinden yeniden kontrol eder. Kurtarma henüz
  tamamlanmadı."
- `recovered/rollback_verified`: EN "The update did not complete, and the server was returned automatically to the
  version it ran before; that restoration was verified. Nothing needs to be done on the server. Do not start the same
  version again until a corrected version is published; the panel's update page shows what is known about the cause." /
  TR "Güncelleme tamamlanmadı ve sunucu otomatik olarak güncellemeden önce çalıştırdığı sürüme döndürüldü; bu geri
  dönüş doğrulandı. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü yeniden
  başlatmayın; nedenle ilgili bilinenler panelin güncelleme sayfasında gösterilir."
- start check, recovering / verified: EN "The new version's panel failed its start check before anything was switched
  on. The server is being returned to the previous version automatically; nothing needs to be done on the server. Check
  this same request again for the verified result; do not start another update." then "…so the server was returned to
  the previous version automatically. The previous version keeps running. Nothing needs to be done on the server. Do
  not start the same version again until a corrected version is published. When you report this, include the reason
  line shown for this update on the panel's update page." / TR "Yeni sürümün paneli, hiçbir şey devreye alınmadan önce
  başlangıç denetiminden geçemedi. Sunucu otomatik olarak önceki sürüme döndürülüyor; sunucuda yapmanız gereken bir şey
  yok. Doğrulanmış sonuç için aynı işlemi yeniden sorgulayın; başka güncelleme başlatmayın." then "…bu yüzden sunucu
  otomatik olarak önceki sürüme döndürüldü. Önceki sürüm çalışmaya devam ediyor. Sunucuda yapmanız gereken bir şey yok.
  Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü yeniden başlatmayın. Bunu bildirirken panelin güncelleme
  sayfasında bu güncelleme için gösterilen neden satırını ekleyin."
- Forward completion (`panel_start_unverified`) through `retry_scheduled`, `pause_pending` and `paused_retry_limit`:
  identical to upd11's quoted texts (`owner-texts.txt` lines 25-48, 75-78), including the Debian/Ubuntu renewal
  sentence ("Automatic certificate renewal (Certbot) was stopped for this update; the same recovery journal says
  whether it was returned…" / "Otomatik sertifika yenileme (Certbot) bu güncelleme için durduruldu; …") and the Arch
  one ("…was already off before this update, so the update did not stop it; check whether it should be on." /
  "…bu güncellemeden önce zaten kapalıydı, bu yüzden güncelleme onu durdurmadı; açık olması gerekip gerekmediğini
  kontrol edin."). In debian13-owner-continuation the CLI read `failed/update_failed` with `failure_code=
  panel_start_unverified` once (21:47:54, 18 s before `recovering`), with the same first text (seen once before in
  upd11 15b).
- `succeeded/update_verified`: EN "The update completed and the new version was verified when it finished. Nothing else
  is needed on the server. Open the panel to check that everything works now." / TR "Güncelleme tamamlandı ve yeni
  sürüm bittiği anda doğrulandı. Sunucuda başka bir işlem gerekmiyor. Her şeyin şu an çalıştığını görmek için paneli
  açın."
- alpha.80 window, `observation_unavailable`: EN "The result of this update could not be read, so it is unknown.
  Nothing is known yet about which version the server runs. Check this same request again in a minute; keep the
  server as it is and do not start another update meanwhile. …" / TR "Bu güncellemenin sonucu okunamadı; sonuç
  bilinmiyor. …"

Update card and recovery screen (rendered by the driver from the installed build's catalogues and source, not a
browser) are identical to upd11: migration defect "Update not completed; previous version restored || … || Cause: the
update failed before it completed; the server recorded no more specific cause. || … || The server reported: reviewed
updater failed: exit status 1: offline panel database migration failed; its original database and work evidence are
preserved" (TR "… || Sunucunun İngilizce günlük satırı: reviewed updater failed: …"); start check "Cause: the new
version's panel failed its start check before anything was switched on." / "Neden: yeni sürümün paneli, hiçbir şey
devreye alınmadan önce başlangıç denetiminden geçemedi."; good and owner continuation "The update completed. The panel
and agent restarted on the new version." / "Güncelleme tamamlandı. Panel ve agent yeni sürümle yeniden başladı." and
"Update verified || The server verified this update. Reload CelikPanel to load the installed interface." / "Güncelleme
doğrulandı || Sunucu bu güncellemeyi doğruladı. Kurulu arayüzü yüklemek için CelikPanel’i yeniden yükleyin." The
retry sentence of the deferred mail work appears in the Panel journal only; no screen or CLI text names it. Setup
texts: the `access_dns` wait (all runs) and alpha.80's Ubuntu refusals (`HOST_MUTATION_BUSY`,
`mail_profile_install_failed`, `server_setup_firewall_failed`) are in `owner-texts.txt`.

## Findings

| # | Class | Finding | Evidence |
| --- | --- | --- | --- |
| R1 | candidate behaviour as designed (measured on the whole matrix) | The deferred mail retry ran once per Panel started inside an operation on a mail stack (11 of 11 that kept running past +30 s; 13 deferred, 2 were stopped first, O1), completed both steps in attempt 1 (+38.2-40.3 s), after the operation's terminal state, and did not repeat. | `deferred-mail-timeline.txt` |
| O1 | candidate product observation (open; D-022/D-024), behaviour as implemented | A Panel stopped before its first retry attempt (the owner's `systemctl disable --now` 26.8-29.0 s after the Panel start in both management-off cells) drops the deferred work; it ran at the next Panel start. The retry keeps nothing persistent by design (`cmd/panel/startup_deferred_mail.go:30-31`). With a restart in between nothing is lost here; an owner who stops the Panel and keeps it stopped keeps mail SNI/milter wiring as the update left it (0 secure-mail certificates in these fixtures, so nothing observable depended on it). | `upd1-debian13-mgmt-off-reboot/run-a/steps/18-collect/journal-product.txt` (22:06:24.75 stop), `upd1-ubuntu-mgmt-off-reboot/run-a/steps/18-collect/journal-product.txt` (00:36:13.27) |
| O2 | candidate product observation, existing before `6b6f8a0c` (= upd12 O1) | Every SNI publication logs `postfix/postmap … fatal: unsupported map type: lmdb`; now seen on Ubuntu 24.04 as well as Debian 13 (1-2 lines in each mail run). The attempt still completes (`maps=hash`). | `upd1-ubuntu-good/run-a/steps/15-collect/journal-product.txt` |
| F3 | incompatibility with alpha.80 (= upd7 F1 / upd11 F3, expected) | After the automatic return to alpha.80 its Panel shows the raw updater line as `summary`, answers `/api/v1/recovery/status` with 404, offers the same defective v0.1.0-alpha.81 again (`available: true`), and its startup skips the mail SNI/milter steps without any retry (upd11 F2, not fixable in the installed alpha.80); the skipped work was not done before the cell ended. | `part2-alpha80/upd1-debian13-defective/run-a/steps/13-terminal/step.json`, `…/steps/15-collect/journal-product.txt` (`panel[4689]`) |
| F4 | incompatibility with alpha.80 (= upd11 F4) | ≈ 10 s without the recovery CLI, then ≈ 7 s `observation_unavailable` after the owner's start, then `running`. | `part2-alpha80/*/run-a/side/changed-paths.txt` |
| F5 | published-baseline limitation (= upd8 F1 / upd11 F5) | alpha.80's own setup on stock Ubuntu needed 7 owner attempts (45 min); the candidate's setup needed one in all five Ubuntu runs. | `part2-alpha80/upd1-ubuntu-good/run-a/steps/06-setup/step.json` |
| H22 | harness defect, fixed | See "Run copies" (installer-status poll during Arch's `pacman -Syu`). | `upd1-arch-owner-continuation/run-a/steps/03-baseline-install/step.json` |

No candidate product defect was found in the update, rollback, start check, forward completion, pause, owner retry,
management-off or deferred mail paths of `f6cdd5a0`. upd11's F1 (setup refused for package activity on Debian) did
not recur in the 9 Debian runs.

## Real origin, licence, secrets

- Every origin check resolved `celikpanel.net` only to `127.0.0.1` with fixture HTTP 200 (`result.json` `scope.origin`);
  the baseline installer logs are counted in `secret-scan.txt`.
- Licence: `license_service: not contacted` in every licence step; every Panel logs the acceptance-fixture banner.
- The guests used Debian's, Arch's and Ubuntu's package mirrors (installer, setup); the host downloaded nothing.
- `secret-scan.txt` (`tools/scan13.py`, `tools/scan.sh`, addendum `tools/scan-add.sh`) over the whole folder before
  hashing: **clean.** 5,210 files; 0 PEM private-key blocks; 0 hits for the 1,198 body lines of the 84 key files of the
  21 labs (SSH keys, fixture signing, CA and TLS keys); 0 unredacted password/secret/token/session/cookie fields;
  3,148 redaction markers. The one fixture-licence literal (and the one `CPK-` shape) is `build/a80/fixture-patches.diff`
  (the D-027 seam's committed `AcceptanceFixtureKey`, as upd7/upd11); the 2 tokens of the mailbox-password shape are
  pieces of lab SSH **public** keys in two `host/fixture-plan.json`; the 7 of the admin-password shape are older
  evidence file names in the run-copy hash list. No installer log (parts 1 and 2) names `celikpanel.net` or the real
  origin's address, which appears only in the scan scripts and the scan output. Longest repository-relative path: 211
  characters.
- `SHA256SUMS` lists every file except itself (`sha256sum -c SHA256SUMS`).

## Removals (disk), host leftovers, host state

After each run's evidence was staged here and the staged driver `SHA256SUMS` verified, `tools/rmoverlay13.sh` removed
**only the overlay disks** (`cells/*/<node>/overlay.qcow2`) of the lab this run had created for that cell (refusing any
lab not named `upd13-*`, any lab without an upd13 job, a running QEMU, or a staged copy that did not verify): **36 files,
37.35 GB, in all 21 labs** `/var/tmp/cp-release-drill-upd13-*`, each listed in `host/removals.txt` (path, bytes, time).
The guests cannot be restarted; everything the driver collected is here. Nothing that existed before this run was
modified or deleted.

Host leftovers (`host/host-leftovers.txt`, 02:32:38Z): `/var/tmp/cp-upd13-run` (947 MB: run copies `harness`,
`harness-h22`, jobs, logs, build records); the 21 stopped labs without overlays (661-922 MB each); the build clones
`/var/tmp/cp-upd1-build/20261002t203047z` (816 MB) and `…/20261002t203514z` (821 MB); eight dist folders
`/var/tmp/cp-pair-accept/dist/<commit>-acceptance-license` (792 MB each, 133 MB for B80). No QEMU process remains; no
dry-run lab exists. Git: HEAD `f6cdd5a0` throughout, the local config key list unchanged (fingerprint `4363b888…`, as
upd8-upd12); repository writes are the two harness files (working tree, = `overlay-h22`) and this folder. No inline
`wsl.exe` command contained `$`; host steps ran from LF script files.

## Files

`build/` (`cur/`, `a80/`: artifact documents, dist JSONs, fixture commits and patches, build-log notes, the alpha.80
unchanged-file proof; prove, dry runs, offline logs), `harness-run-copy/` (run-copy hashes, `overlay/` (none) and
`overlay-h22/`, cell jobs with their harness record, queue, lists and the scripts that made the copies, builds, proofs
and jobs), `tools/` (host-only readers: extraction, changed paths, deferred-mail table, cell table, sampler gaps,
summaries, texts, staging, removal, scan, sums, C: reading, keep-awake), `host/` (host check, C: readings, removals,
leftovers, keep-awake log, power events, queue log), one folder per run with the driver's evidence unchanged (its own
`SHA256SUMS` verified when staged), `host/` and `side/` (`extract.txt`, `changed-paths.txt`,
`deferred-mail-timeline.txt`); part 2 under `part2-alpha80/`. `summary-per-cell.txt`, `step-table.md`, `cells-table.md`,
`outage-windows.txt`, `owner-texts.txt`, `deferred-mail-timeline.txt`, `changed-paths-table.md`, `sampler-gaps.txt`,
`secret-scan.txt`, `SHA256SUMS`.

## What this run does NOT prove

- That the kinds always end this way: one complete run per cell (two for the re-run cell) on a laptop host with the
  acceptance fixture, a fixture signing key and a loopback origin. Production signing, the real release origin, the
  licence service, DNS (external mode) and certificate issuance or renewal execution are not exercised.
- The deferred retry beyond its first attempt: busy again at +30 s, the verified-failure line, the give-up after 20
  attempts, the pending-outbox domain retry, and anything about secure-mail certificates (every publication had 0
  certificates; no SNI comparison between Nginx and Postfix/Dovecot). Whether it overwrites an owner's own edit to the
  native mail files.
- What an owner with active secure-mail certificates sees when the Panel is stopped before the retry (O1) or after a
  return to alpha.80 (F3).
- Real start on Arch and Ubuntu; start check, real start and management-off from alpha.80; any Arch path from alpha.80;
  the start check with a host-name or leading-zero listen address; an update started while an idle `packagekitd` runs;
  the writer of the initial record (inferred, not named); the curl refusal; the no-marker end after a child failure.
- That H22's tolerance works natively (no failed status read happened after the fix), and the guest state of
  arch-owner-continuation run-a (its overlays were removed before diagnosis).
- A cause the owner cannot remove, a second fault during the owner's retry, power loss, browser rendering (cards and
  screens are rendered from the build's catalogues and source by the driver), the SSH owner view.
- The signed alpha.80 archive itself (B80 is the tag rebuilt with the licence seam).
- Panel removal or uninstall: management-disabled across one orderly reboot is the only absence condition.

It closes no P0 row. Wall time: host check 20:30Z; builds 20:30:47-20:37:22Z; cells 20:38:24Z-02:31:41Z (2026-10-03);
staging, scan and sums after.

## Corrections after intake (2026-10-10)

After this directory was published (2026-10-10), the operator's machine name was replaced by `<operator-host>` in the SSH public-key comments (and in the text that named it) of this directory: 36 occurrences in 21 files; the key material itself, a lab public key, is unchanged.
After this directory was published (2026-10-10), 86 digest values of the one-shot update-transaction token (76 in plain text in 47 files, as `transaction_token_sha256` values and as `.release-db-migrations/<digest>` directory names; 10 inside base64 `events_base64` text in 5 files) were replaced by `[REDACTED-SHA256]`.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.
