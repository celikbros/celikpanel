# upd10 native run, 2026-10-01: does the corrected package-activity rule (c855a757) admit an idle Ubuntu `packagekitd`?

Roadmap item 3, owner-started update acceptance (`owner_update_trial.py`), tenth run. [upd9](../upd9-20261001/README.md)
measured that the efcba145 rule never matched on Ubuntu 24.04 (the APT backend is mapped as `libpk_backend_apt.so`,
not `libpk_backend_aptcc.so`). Commit `c855a757` corrected it: the daemon can be idle only when every mapped file under
a directory named `packagekit-backend` is `libpk_backend_apt.so` or `libpk_backend_aptcc.so` (at least one), it has
no child and it holds or waits for no apt/dpkg lock; a refused update start for package activity now returns
`HOST_MUTATION_BUSY` with reason `package_manager_active`. `c67d1861` aligned the browser sentence. This run measures
the corrected rule on stock Ubuntu 24.04 with upd9's one-node lab, image, cells and method, and probes Debian 13.

Disposable QEMU/KVM guests on the local `archlinux` WSL host only. No installed server (Boston, Frankfurt, any) was
touched or contacted. Nothing was pushed, committed, published or signed with a production key. `celikpanel.net`
resolved only to the guest-loopback fixture origin; no licence service was contacted. Every `result.json` carries
`native_evidence: false`; the owner judges the P0 rows. **It closes no P0 row.** Repository HEAD stayed `c67d1861`
for the whole run; the run copy and the build are from `c67d1861`.

**Result in one sentence:** on stock Ubuntu 24.04 the corrected rule holds - every one of 176 readings of a running
`packagekitd` (three daemons) mapped exactly `/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so`, with
no child and no lock line, and the Agent answered `ready` in all 41 observations where only that idle daemon ran; the
candidate's fresh install + `web_mail` setup reached the isolated host's `access_dns` wait in **one** attempt with no
refusal (cell A; upd9: refused after 8 s); during the owner's own package task the update start was refused with HTTP
409 `HOST_MUTATION_BUSY` / `package_manager_active` and the corrected sentence, and right after the task, 4 s after
`packagekitd` started, the start was admitted and the update verified (cell B); stock Debian 13 has no PackageKit at
all (cell C).

## What was built from what

One build by `run-upd1.sh build c67d1861` from the run copy (`build/cur/build.*`), clone under
`/var/tmp/cp-upd1-build/20261001t210302z/repo`, `go1.26.5 linux/amd64`, `license_mode: acceptance-fixture` for every
archive. `prove` exited 0; both dry runs exited 0 and created no lab (`build/cur-*`).

| Build `20261001t210302z` (21:03:02-21:07:08Z) | Role | Label / sequence | Commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- | --- |
| Baseline B = c67d1861 + policy (installed fresh in A and B) | B | v0.1.0-alpha.81 / 81 | 2e583a9031ec50fb27d10305ef02b909f10b7674 | 2d2a9ff1ce400cb3c1d6c84099865d1f98335bfa | a52c3ee03e92144e3803f57b0bd722a717a22331a6423425c6cd8e93bc6734cf |
| Good G = B + policy (update target in B) | G | v0.1.0-alpha.82 / 82 | 962e6f242b2dbcb0d42edae9e741d3822a92c8cc | 7deed24f4bd0d200fbf922974a0d6900c6811e63 | e2de75c2013ed6095676129765bf8249832d996c87e83d7f623b223141e96eff |
| D, S, R (built, not used) | | v0.1.0-alpha.82 / 82 | bc1b3ab3…, 4ec029ef…, 021c77f2… | | `build/cur/upd1-artifacts.json` |

Image: the upd8/upd9 image, unchanged (`/var/tmp/cp-v3n28/images/ubuntu-24.04-server-cloudimg-amd64-20260826.img`);
Debian 13 `debian-13-genericcloud-amd64-20260826-2582.qcow2` and the Arch image of the existing lab kind
(`host/hostcheck-before.txt`). Nothing was downloaded by the host.

## Harness changes (working tree, `deploy/e2e/release-recovery/`; offline suites green)

The run copy is `git archive c67d1861` (`harness-run-copy/runcopy-c67d1861-files.sha256`) plus two files
(`harness-run-copy/overlay/`, `harness.diff` SHA-256 `8e910765…`; `owner_update_trial.py` `038e3e9b…`,
`test_owner_update_trial.py` `e563046f…`). Offline: `test_owner_update_trial` 175 (1 new), `test_lab` 15,
`test_worker_fixture_origin` 15, `test_recovery_candidate_archive` 11, `test_bound_worker_reboot` 11,
`test_guest_bound_worker` 8, `test_current_worker_baseline` 7 - all OK (`build/offline-o1-*`, run as job `offline-t1`;
`offline-o1-misstart.*` is a first start whose argument was passed as part of the file name and ran nothing). The
unmodified `c67d1861` suite fails one test (`build/offline-pristine-test_owner_update_trial.txt`, F4).

- `PK_BACKEND_RULE` (new, `owner_update_trial.py:194`): the c855a757 backend reading in Python - the pathname column of
  every maps line whose directory is named `packagekit-backend`, and the all-APT-modules answer. `PK_PROBE` (`:221`)
  now records per daemon `backend_pathnames` (full pathname column), `apt_backend` (the corrected reading) and
  `maps_read`; the efcba145 reading (`aptcc_backend`, `grep_c_aptcc`) is kept beside it; `rule_idle`/`rule` use the
  corrected reading. New `context_processes`: package-related processes outside the Agent's list
  (`unattended-upgr`, `needrestart`, debconf `frontend`, apt fetch methods `http`/`https`/`gpgv`, `apt-check`, ...)
  and every child of `packagekitd`, each with a 200-character cmdline, so a refusal for real activity can be told
  apart from the idle daemon. `pk_summary` (`:307`) adds `backend_pathnames_seen`, `apt_backend`,
  `context_processes_seen`.
- Tests: the probe test now pins `"libpk_backend_apt.so"`, `"libpk_backend_aptcc.so"` and `"packagekit-backend"` in
  both the Agent source and the probe; a new test runs `PK_BACKEND_RULE` on the Agent test's cases (upd9 Ubuntu maps,
  aptcc, another backend, an extra test backend, `(deleted)`, a same-named file outside the directory, an unclean
  path, none). No product file changed; `run-upd1.sh` unchanged.

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`, GiB; `host/c-drive.txt`)

107.7 GB free before anything (floor 40, stop at 25); 98.8 before cell A (logged with the locale's decimal comma),
96.5 before cell B, 94.1 after the last cell.

## Cells

| Cell | Lab, port | Wrapper (UTC) | Overall | Outcome |
| --- | --- | --- | --- | --- |
| A `upd1-ubuntu-setuponce` run-a | upd10-ub-once-a, 3111 | 21:12:34-21:26:01 | complete-for-review | `setup-reached-access_dns-wait-in-1-attempt(s)`, no refusal |
| B `upd1-ubuntu-busystart` run-a | upd10-ub-busy-b, 3121 | 21:26:51-21:50:23 | complete-for-review | `update-verified`; start refused during the task, admitted while `packagekitd` idled |
| C Debian 13 probe (no CelikPanel) | upd10-deb-probe-c3, 3037 | 21:10:00-21:11:58 | answered | no PackageKit installed or running before/after apt |

A and B ran once each; no cell was re-run. C: runs `probe-c` and `probe-c2` stopped at their first guest call before
any output (probe-script defect, F10); `probe-c3` is the measurement.

### A. The candidate installed fresh, setup started once by an ordinary owner

| UTC | Event |
| --- | --- |
| 21:13:33-21:15:07 | the candidate's own installer (c67d1861 + policy, `v0.1.0-alpha.81`); `PackageKit[4920]: daemon start` 21:14:06, `PackageKit[5168]: daemon start` 21:14:32 |
| 21:15:09-11 | owner login, licence (`not contacted: acceptance test build`), guided setup, `web_mail` plan reviewed and started once; no wait for package checks |
| 21:15:10 | reading: `packagekitd` 5168, age 39.5 s, backend `…/packagekit-backend/libpk_backend_apt.so`, no child, no lock; readiness (Agent) **`ready`** |
| 21:15:28-21:20:47 | 02-service, 03-service, 04-service, 05-mail_profile (21:17:21), 06-mail_profile (21:19:48), 07-service run; the setup's own `apt-get`/`dpkg`/`dpkg-deb`, debconf `frontend`, `needrestart -m u`, `apt-check` seen; the Panel answers its own `panel_operation_active` while setup runs |
| 21:20:58 | setup `waiting`, phase `access_dns`, `server_setup_access_dns_required` (08-firewall done) - **one attempt, 5 min 47 s after the start** |
| 21:20:57-21:25:39 | 21 observations (setup polls, setup end, after-setup polls): the same daemon (age 387-667 s), apt backend, no child, no lock, nothing else; readiness 21/21 `ready` (with 3 earlier ones at 21:15:10, 21:15:50, 21:17:15: 24) |
| 21:25:57.47 | `PackageKit[5168]: daemon quit` (alive 685 s: the setup's own apt runs kept it alive) |
| 21:25:59-21:26:00 | no `packagekitd`; readiness `ready` (2/2) |

Refusals the owner saw: **none**. The only code shown is the isolated host's DNS wait: `server_setup_access_dns_required`
"Public DNS must resolve panel-ubuntu.upd1-infra.test to 192.0.2.12 before a certificate is requested. Check this
hostname's A/AAAA records and its zone delegation. For DNS managed here, review the infrastructure DNS records in this
wizard; otherwise correct them at the primary DNS server or provider. This check resumes automatically when DNS is
verified." (API, English only). No step was refused, so there was no refusal instant to classify as real or idle-daemon
activity.

### B. Real package activity, then the update start while PackageKit idles

Setup (H19 on, as upd9): the harness's owner model waited 278.6 s for `packagekitd` 5076 to quit (21:28:15-21:32:54;
the harness's own probe still counts the daemon, F5), then **one attempt**: 21:32:55 start, 02-service 21:33:01 …
08-firewall 21:38:09, `access_dns` wait 21:38:15; no refusal. Seed after another H19 wait of 176.1 s (daemon 10409,
21:33:02-21:43:08). Pre-state to 21:45:09.

| UTC | Owner action / reading | Answer |
| --- | --- | --- |
| 21:45:10 | the owner's task starts (`golang-1.22-src`, 19.7 MB, download only, 80 KiB/s); 0.6 s later `apt-get` 43010 holds `/var/lib/dpkg/lock` and `lock-frontend`; no `packagekitd` | |
| 21:45:11 | readiness (Agent) | `HOST_MUTATION_BUSY / package_manager_active` |
| 21:45:11 | version `v0.1.0-alpha.81` / 2e583a90, agent_matches; Agent PID 7058, Panel PID 7321 | |
| 21:45:11 | `GET /api/v1/panel/update/check` | available `v0.1.0-alpha.82` / 962e6f24 |
| 21:45:12 | `POST /api/v1/panel/update/start`, request `435669941ed19f9cd64de676173081ab` | **HTTP 409 `HOST_MUTATION_BUSY` / `package_manager_active`** (verbatim below) |
| 21:45:13 | `apt-get` 43010 still running; status of that request | `{"found": false, …}` |
| 21:45:13 | panel log | `[panel-update] agent refused start: global service mutation state is not idle: the host package manager is active` |
| after | version and unit PIDs/start times | unchanged (`v0.1.0-alpha.81`, 7058, 7321) |
| 21:45:13-21:49:08 | 15 readings every ~16 s | `apt-get` 43010 + archives lock, apt `http` method, no `packagekitd`; readiness 15/15 `package_manager_active` |
| 21:49:15.21 | the task's `apt-get update` -> `PackageKit[51688]: daemon start`; task unit `Result=success` (244.9 s) | |
| 21:49:15 | reading: `packagekitd` 51688, age 1.7 s, backend `/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so`, no child, no lock, nothing else | readiness **`ready`** |
| 21:49:16-17 | arm (no H19 wait): reading 51688 age 2.3/2.6 s, same backend, idle; readiness `ready` | |
| 21:49:19 | `POST …/update/start` at once, request `8754390060ba490c8c04ea800790ec21`, `packagekitd` alive before and after | **HTTP 202** `accepted`, `queued`; status `running` |
| 21:50:11 | update | `succeeded / update_verified` (API, root CLI and recovery API agree, 11 samples) |

Terminal (passed): G installed and running (`v0.1.0-alpha.82`, 962e6f24), running = installed, floor 82; database equal
except volatile tables (`metrics_samples`, `server_setup_executions`); timers (3) and firewall equal; login works;
domain, mailbox and cron rows present; site marker and SMTP served; cron never interrupted; Panel down 20-30 s.
`packagekitd` 51688 shows no `daemon quit` up to the journal read at 21:50:21, so it was alive for the whole update.

**Verbatim owner-visible texts for the refused update start**

- API (HTTP 409): `{"code": "HOST_MUTATION_BUSY", "error": "This server's package manager is busy — a package task is
  still running on this server. Wait for it to finish, then try again.", "reason": "package_manager_active"}`
- Catalogue of the installed build for that code and reason (what a screen can show; not rendered):
  `err.HOST_MUTATION_BUSY.package_manager_active` EN "This server's package manager is busy — a package task is still
  running on this server. Wait for it to finish, then try again." (= the API sentence) / TR "Bu sunucunun paket
  yöneticisi meşgul — bu sunucuda bir paket işlemi hâlâ sürüyor. Tamamlanmasını bekleyip yeniden deneyin."
  `err.HOST_MUTATION_BUSY` EN "Another server change or operating-system package task is still running. Wait for it
  to finish, then try again." / TR "Başka bir sunucu değişikliği veya işletim sistemi paket işlemi hâlâ sürüyor.
  Tamamlanmasını bekleyip yeniden deneyin."
- The update card's readiness texts: `services.mutationReadiness.title` EN "Server changes are temporarily
  unavailable." / TR "Sunucu değişiklikleri geçici olarak kullanılamıyor."; `services.mutationReadiness.package_manager_active`
  EN "The operating system is installing or updating packages. Wait for it to finish." / TR "İşletim sistemi paket
  kuruyor veya güncelliyor. Tamamlanmasını bekleyin." `err.PANEL_UPDATE_START_REFUSED`: no catalogue entry (no longer
  returned for this cause).
- Status: `{"found": false, "request_id": "435669941ed19f9cd64de676173081ab"}`; installed release unchanged.

### C. Debian 13 (stock genericcloud 20260826, kernel 6.12.105+deb13-cloud-amd64; `host/debian13-probe-c3.out.txt`)

`apt 3.0.3`, `unattended-upgrades 2.12` installed; `packagekit`, `packagekit-tools`, `libpackagekit-glib2-18` not
installed; no `/usr/lib/x86_64-linux-gnu/packagekit-backend/`, no `/etc/PackageKit`, no `20packagekit` apt hook, no
`packagekit*` unit (`LoadState=not-found`), no D-Bus activation file. `packagekitd_pids=none` before, 8 s after
`apt-get update` (21:11:05, rc 0), 8 s and 38 s after `apt-get install -y --no-install-recommends tree` (dpkg ran,
21:11:18, rc 0); `journalctl -u packagekit.service`: no entries; no apt/dpkg lock line. The only package-related process
is `unattended-upgrade-shutdown --wait-for-signal`. Debian's candidate `packagekit 1.3.1-1+deb13u1` (downloaded to the
guest's `/tmp`, not installed; `.deb` SHA-256 `15c450df…9163`) ships
`./usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so`, five `libpk_backend_test_*.so`,
`./usr/libexec/packagekitd` and `./etc/apt/apt.conf.d/20packagekit`.

## PackageKit observations (`host/packagekit-crosstab.txt`, `host/packagekit-context.txt`, per-reading files under `*/steps/*/packagekit-observations.json`)

Every observation is two `/proc` readings around one readiness answer (`tools/pkcross.py`); "packagekitd only" ignores
the always-running `unattended-upgrade-shutdown --wait-for-signal` helper (outside the Agent's list).

| Cell | `/proc` state | Readiness answer | Observations |
| --- | --- | --- | --- |
| A | only `packagekitd` running | Agent `ready` | 24 |
| A | only `packagekitd` running | the Panel's own `panel_operation_active` (setup running) | 9 |
| A | `packagekitd` + other package activity (setup's apt/dpkg) | Panel `panel_operation_active` | 16 |
| A | nothing running | `ready` | 2 |
| B | only `packagekitd` running | Agent `ready` | 17 |
| B | only `packagekitd` running | Panel `panel_operation_active` | 9 |
| B | `packagekitd` + other package activity | Panel `panel_operation_active` | 13 |
| B | other package activity only (incl. the owner's task) | Agent `package_manager_active` 16, Panel 1, no read 1 | 18 |
| B | nothing running | `ready` | 2 |

| Daemon (pid) | Start - quit (journal) | Readings | Age range | Mapped under `packagekit-backend` | `apt_backend` | Children | Lock lines held/waited |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A 5168 | 21:14:32 - 21:25:57 | 98 | 39.5-667.6 s | `/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so` only | true | none | 0 |
| B 10409 | 21:33:02 - 21:43:08 | 78 (B, all daemons) | 1.7-437.2 s | the same single path | true | none | 0 |
| B 51688 | 21:49:15 - (alive at 21:50:21) | (in B's 78) | 1.7-2.6 s | the same single path | true | none | 0 |
| B 5076 | 21:27:48 - 21:32:53 | 0 (H19 wait, not read) | | | | | |
| upd9 probe-a (no CelikPanel) | | 5 | 12-70 s | the same path (upd9) | (n/a) | none | 0 |
| Debian 13 (C) | no daemon | 0 | | not installed | | | |

`grep -c aptcc` = 0 and the efcba145 reading `aptcc_backend` = false in every reading. In no observation with only
`packagekitd` running did the Agent answer busy; the Agent answered `package_manager_active` only while the owner's
`apt-get` held a lock (16/16). Real activity seen during setup (answered by the Panel, as setup ran): `apt-get`, `dpkg`,
`dpkg-deb` holding the dpkg and archives locks, debconf `frontend`, `needrestart -m u`, `apt-check`, `apt-key`, `gpgv`,
apt `http`/`https` fetch methods, an `apt-get` on `/var/lib/apt/lists/lock`; none of them a child of `packagekitd`.

## Findings

| # | Class | Finding | Evidence |
| --- | --- | --- | --- |
| F1 | candidate change confirmed natively (expected) | c855a757's rule matches stock Ubuntu 24.04 (PackageKit 1.2.8-2ubuntu1.5): all 176 daemon readings map exactly one backend path, `/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so`; with no child and no lock the Agent answers `ready` (41/41 idle-daemon observations). Setup reaches `access_dns` in one attempt without any refusal (A, B); upd9 needed 7 attempts / was refused after 8 s. | `upd1-ubuntu-setuponce/run-a/steps/06-setup/`, `…/07-packagekit-after-setup/packagekit-observations.json`, `host/packagekit-crosstab.txt` |
| F2 | candidate change confirmed natively (expected; upd9 F2 fixed) | A start refused for real package activity returns HTTP 409 `HOST_MUTATION_BUSY` / `package_manager_active` with the corrected sentence (no longer `PANEL_UPDATE_START_REFUSED`); no update record; release, Panel and Agent unchanged; the API sentence equals the web catalogue's EN entry (c67d1861). | `upd1-ubuntu-busystart/run-a/steps/09-busy-start/busy-start-refusal.json` |
| F3 | candidate behaviour confirmed natively (expected) | Right after the owner's task, with `packagekitd` 51688 alive 1.7-4 s and idle, readiness is `ready` and the start is admitted (HTTP 202), verified 52 s later. The daemon stayed alive during the whole update, so the package-activity checks that ran inside the update admitted it (which checks ran is not separately recorded). | `…/11-owner-start/idle-alive-start.json`, `…/10-arm/step.json`, `…/15-collect/journal-packagekit.txt` |
| F4 | harness defect (fixed in this run) | The unmodified c67d1861 harness fails `test_probe_reads_the_rule_inputs_and_reports_json`: it pins the efcba145 suffix `"/libpk_backend_aptcc.so"` in `cmd/agent/service_mutation_lock_linux.go`, which c855a757 removed (`test_owner_update_trial.py:153` at c67d1861). Its `PK_PROBE` also still judged the backend by the old suffix. Both corrected by the overlay. | `build/offline-pristine-test_owner_update_trial.txt`, `harness-run-copy/overlay/harness.diff` |
| F5 | harness limitation (not changed) | H19's own probe (`HOST_IDLE_PROBE`, `owner_update_trial.py:149`) still counts `packagekitd`, so B's owner model waited 278.6 s before setup and 176.1 s before seed for a daemon the product now admits. Changing H19 changes the cell model; left for the planner. | `upd1-ubuntu-busystart/run-a/steps/06-setup/step.json` (`owner_idle_waits`), `…/07-seed/step.json` |
| F6 | harness gap (minor) | `idle_alive_start` keeps the `/proc` readings around the start only as booleans (`packagekitd_alive_before/after`, `owner_update_trial.py:4266`); the backend path at that instant is taken from the arm readings 2-3 s earlier (same pid 51688). | `…/11-owner-start/idle-alive-start.json`, `…/10-arm/step.json` |
| F7 | pre-existing observation, not PackageKit (open, not investigated) | When the new Panel starts inside the update it logs `certificate startup reconcile: certificate dependents: publish full mail SNI snapshot: another server change or package-manager task is still running` (`cmd/panel/cert_startup_reconcile.go:178`) and `milter wiring at startup: another server change or package-manager task is still running` (`cmd/panel/main.go:1454`), 21:50:02. The same lines appear in upd8 and upd9 updates where no PackageKit ran then; presumably the update's own host lease. Whether these startup tasks are retried later, and whether an owner sees anything, was not measured. | `upd1-ubuntu-busystart/run-a/steps/15-collect/journal-product.txt`; upd9 `upd1-ubuntu-busystart/run-a/…/journal-product.txt` 18:57:09 |
| F8 | platform observation (C) | Stock Debian 13 has no PackageKit: the rule's daemon branch is not exercised there after apt operations. If an owner installs Debian's `packagekit 1.3.1-1+deb13u1`, it ships the backend as `libpk_backend_apt.so` (file listing only; the daemon's actual mapping on Debian was not measured). | `host/debian13-probe-c3.out.txt` |
| F9 | fixture limitation | One isolated node: DNS external, setup waits at `access_dns` (panel and mail certificates, verify not run). The update card and setup wizard were modelled from the build's catalogues, not rendered. | `result.json` `scope` |
| F10 | probe-script defect (cell C, fixed) | `probe-c` and `probe-c2` stopped at their first guest call with no output: the body's last command (the lock-file loop over a missing lock file) returned nonzero and the lab helper raises; run 2's script fix had not been applied (a string replacement that silently matched nothing). `probe-c3` ends every body with `true`. This is one more run than the "re-run at most once" rule; runs 1-2 measured nothing. | `host/debian13-probe-c{,2}.err.txt`, `tools/probe-c-run1.sh`, `tools/probe-c.sh` |

No candidate product defect was found in the measured scenarios.

## Real origin, licence, secrets

- Each baseline installer log names neither `celikpanel.net` nor `185.95.` (`secret-scan.txt`); every origin check
  resolved `celikpanel.net` only to `127.0.0.1` with fixture HTTP 200 (`result.json` `scope.origin`).
- Licence: `not contacted: acceptance test build` in every licence step.
- The guests used Ubuntu's and Debian's package mirrors (install, setup, the owner's task, the Debian probe). The host
  downloaded nothing.
- `secret-scan.txt` (tools `scan10.py`, `scan.sh`) over the whole folder before hashing: 0 PEM private-key blocks, 0 hits
  for the 127 body lines of the 11 lab key files (SSH keys of the five labs, fixture signing, CA and TLS keys of the two
  cell labs), 0 unredacted password/secret/token/cookie fields, 0 fixture-licence literals, 348 redaction markers. The 7
  tokens of the admin-password shape are upd4 file names in the run-copy hash list; the one token of the 32-character
  password shape is a substring of the disposable lab's SSH **public** key in `upd1-ubuntu-setuponce/run-a/host/fixture-plan.json`.
  `185.95.` appears only in this README, the scan scripts and the scan output. Longest repository-relative path: 167 characters.
- `SHA256SUMS` lists every file except itself (`sha256sum -c SHA256SUMS`).

## Files

`build/` (`cur/` with artifacts document, dist JSONs, fixture commits and patches, build logs; prove and dry-run
outputs `cur-*`; offline logs incl. the pristine c67d1861 run); `harness-run-copy/` (run-copy hashes of c67d1861, the
overlay with files and diff, jobs, setup scripts); `tools/` (host readers, waiters, PackageKit viewers, cross-table and
context, the Debian probe scripts, extract, scan, stage, sums); `host/` (host check, `C:` readings, Debian probe logs,
PackageKit cross-table and context, leftovers); one folder per cell with the driver's evidence unchanged (its own
`SHA256SUMS` verified after copying), `host/` (wrapper logs, job, overlay, lab identity, baseline installer result and
log, origin intent and manifest, fixture plan) and `side/extract.txt`.

## Host leftovers (archlinux WSL; nothing deleted; `host/host-leftovers.txt`)

`/var/tmp/cp-upd10-run` (1.2 GB: run copy, pristine archive copies, overlay, jobs, logs), the labs
`/var/tmp/cp-release-drill-upd10-ub-once-a` (2.0 GB), `-ub-busy-b` (2.3 GB), `-deb-probe-c` (1.1 GB), `-deb-probe-c2`
(1.1 GB), `-deb-probe-c3` (1.3 GB), guests stopped, overlays and evidence kept; the build clone
`/var/tmp/cp-upd1-build/20261001t210302z` (741 MB); the dist folders
`/var/tmp/cp-pair-accept/dist/<commit>-acceptance-license` for 2e583a90, 962e6f24, bc1b3ab3, 4ec029ef and 021c77f2
(718 MB each). No QEMU process remains, no dry-run lab was created. The local git configuration's key list is unchanged
(fingerprint `4363b888…`, equal to upd8's and upd9's). The repository writes are the two harness files above and this
folder.

## What this run does NOT prove

- Any other release or distribution with PackageKit running (Ubuntu 22.04, Debian 13 with PackageKit installed, hosting
  images with another backend); the `libpk_backend_aptcc.so` acceptance is still unmeasured. Stock Ubuntu 24.04
  20260826 cloud image only, signature not verified.
- A PackageKit transaction in progress (no client asked the daemon for one; no reading saw a child or a lock held by
  it), so the rule's busy branches for a real PackageKit transaction are covered by component tests only.
- That the corrected rule never admits a mutation during a real package change by another route: real activity was
  seen only as `apt-get`/`dpkg` processes and their locks (counted busy by the Agent's other checks).
- Which of the candidate's internal package checks ran inside the update (F3), and F7's startup refusals' effect.
- That reading readiness every ~10-20 s never perturbs the Agent's own admission (no refusal here named
  `host_lock_busy` or `agent_mutation_active`).
- Browser rendering of the setup wizard and update card (catalogue sentences only).
- Production signing, the real release origin, licence behaviour, DNS, certificate issuance, power loss; repeatability
  (one run per cell).
