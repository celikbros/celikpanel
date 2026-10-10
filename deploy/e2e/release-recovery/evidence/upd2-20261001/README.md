# upd2 native run, 2026-09-30 (folder dated for the item 3 batch 2026-10-01)

Roadmap item 3: the owner-started update acceptance driver (`owner_update_trial.py`), second
attempt, four cells. Disposable QEMU/KVM guests on the local `archlinux` WSL host only. The
product build is `a81da45d`, and this is the first native measurement of that commit.

**Result in one sentence: both Debian 13 cells reached the owner's update and finished for
review; neither Arch cell reached the update.** On Debian 13 the defective candidate failed in
phase `active` and the server returned to the old release by itself (two automatic attempts, the
first interrupted by a reset at `payload_restored`). The good candidate was verified forward.
Both Arch cells stopped before the update: the owner's static site is served as 404 and the
owner's cron job cannot enter its home directory, because the web server and the site user
cannot traverse the hosting path (product finding P3).

Nothing here passes a P0 row. Every `result.json` carries `native_evidence: false`, and the
owner judges P0.1, P0.2, P0.3 and P0.5.

No installed server (Boston, Frankfurt, any) was touched. Nothing was pushed, published or
signed with a production key. The candidates were served only by the guest-loopback fixture
origin (see "Real origin").

## Build and proofs (step 1)

`bash deploy/e2e/release-recovery/run-upd1.sh build a81da45d` ran from 15:27:31Z to
15:29:44Z and exited 0 (`build/build.*`). Web assets were built fresh in the disposable clone
`/var/tmp/cp-upd1-build/20260930t152731z/repo`. The Go toolchain was `go1.26.5 linux/amd64`.

| Role | Label / sequence | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B | v0.1.0-alpha.81 / 81 | e3a569d527ec88c01e14c1df015fa3010ea7f803 | 23e9ee4bfd5503da911a5979229276bc2c5cb472 | 94e8fcfc619fb2271f788c9c69893afd7917ceb4460e2d225a183341ccf9a140 |
| Good G | v0.1.0-alpha.82 / 82 (parent B) | 74fe2fe6ed0dd06ba50a53ee92cbca4648ceb352 | 5424f2a64c99dcb825ea40dc28cddf5d1bcee2c7 | f9db4686a020dde085760df4c64016483ffd5a08ada3169f3a2db77aa800ba0e |
| Defective D | v0.1.0-alpha.82 / 82 (parent G) | ae713f912af8d8e9153a2053cc2ad6f7abe66851 | 35cea077672ca71f60f749565b549a15e656b504 | b12c88ff2de700a0f187cb3ef6a33ab9a3417dcab53dec0a3ff8e8db38d5e1b2 |

- **Source and fixture commits.** The source HEAD was a81da45dfae54e1f3cc15451ca3011ada33787ed.
  Relative to a81da45d, the fixture commits change only `cmd/panel/main.go` and
  `deploy/release-sequence-policy` (`build/fixture-commits.diff`).
- **Licence mode and guard.** All three archives are `license_mode: acceptance-fixture`. The
  acceptance-licence guard refused each of them for three reasons (`build/guard-refusal-*.txt`).
- **`prove`.** `run-upd1.sh prove` exited 0 (`build/prove.json`). It proved the inventory, the
  release policy, the committed source (479 static files) and `dns-owner-tools/` (4 files) for
  every archive.
- **Dry run.** `dry-run upd1-debian13-defective` passed with `native_evidence: false`, and no
  lab was created (`build/dry-run-upd1-debian13-defective.json`).
- **Offline suites at an exact export of a81da45d.** `test_owner_update_trial.py` passed 55/55
  and `test_recovery_candidate_archive.py` passed 11/11 (`build/offline-tests-*-a81da45d.txt`).
  - A first run of the owner suite inside the build clone had 1 error
    (`build/offline-tests-owner-in-clone-at-defective-HEAD-misplaced.txt`).
  - That run is misplaced, not a harness defect: the clone's HEAD is the defective fixture
    commit, whose `main.go` already carries the defect, so `apply_defect` correctly refuses.

## Harness corrections (run copy only; the repository was not edited)

The run copy is `git archive a81da45d` at `/var/tmp/cp-upd2-run/harness`, plus the diffs in
`harness-run-copy/`. The wrappers and job scripts used for each run are there too.

| Id | Defect | Kind | Correction | First run using it |
| --- | --- | --- | --- | --- |
| H6 | `owner_update_trial.py:1719` treats any status other than 200 as a refusal. The product answers **202 Accepted** for a queued or running start (`cmd/panel/system_update_handlers.go:354,413`), and the web UI checks `response.ok`. d13-def run-a therefore marked `owner-start` failed while the update was running, and `track` never ran. | mechanical | Accept 200/202 with `accepted: true` (`H6-owner_update_trial.py.diff`) | d13-def run-b |
| H7 | `origin_verdict` (`owner_update_trial.py:640-644`) counts only `getent hosts` lines that name `celikpanel.net`. On Arch, nss-resolve prints a hosts-file name that shares 127.0.0.1 with `localhost` under its canonical name (`127.0.0.1 localhost`). arch-def run-a therefore stopped at `origin` with `addresses: []`. | mechanical | Take the address from every line of `getent hosts celikpanel.net`, which prints answers for that one name only. The 127.0.0.1-only rule is unchanged, and a non-loopback or empty answer still fails. The raw output is now recorded (`H7-owner_update_trial.py.diff`). | arch-def run-b |

The H7 cause was established on a separate fresh Arch guest (`cp-release-drill-upd2-diag-arch`),
using the neutral test name `upd2-diag.example.net` in `/etc/hosts`. `celikpanel.net` was never
looked up there (`diag-arch-resolver/`). Findings from that guest:

- `getent hosts` printed `127.0.0.1 localhost`, and `resolvectl` said "Data from: synthetic",
  answering 127.0.0.1 and ::1 on `lo`.
- In arch-def run-a, the `curl` probe's HTTP 200 therefore came from the loopback fixture.

## Cells (steps 2-4)

One new lab per run, stopped by the wrapper. Disks, overlays and host logs are retained under
`/var/tmp/cp-release-drill-upd2-*`. Port bases were 2361, 2371 and 2381, and 2391 for Arch good.

| Cell / run | Harness | Lab | Reached | Stop / outcome | Cause class |
| --- | --- | --- | --- | --- | --- |
| upd1-debian13-defective / run-a | a81da45d as is | upd2-d13-def-a | all steps to `arm`; start **accepted 202** at 15:40:11Z | `owner-start` marked failed; lab stopped at 15:40:14Z with the update in flight | harness H6 |
| upd1-debian13-defective / run-b | H6 | upd2-d13-def-b | every step (15:42:31Z to 15:53:31Z) | `recovered-automatically`, overall `complete-for-review` | - |
| upd1-debian13-good / run-a | H6 | upd2-d13-good-a | every step (15:55:23Z to 16:06:13Z) | `update-verified`, `complete-for-review` | - |
| upd1-arch-defective / run-a | H6 | upd2-arch-def-a | preflight | `origin` failed (`addresses: []`, http 200) | harness H7 |
| upd1-arch-defective / run-b | H6+H7 | upd2-arch-def-b | through `seed` | `pre-state` failed at 16:22:11Z: `web_ok` (site 404) | **product** P3 |
| upd1-arch-good / run-a | H6+H7 | upd2-arch-good-a | through `seed` | `pre-state` failed at 16:33:18Z: `web_ok` (site 404) | **product** P3 (reproduced) |

Each re-run was made exactly once, with a recorded cause. Every run is kept.

### Step timings (UTC)

| Step | d13-def run-b | d13-good run-a | arch-def run-b | arch-good run-a |
| --- | --- | --- | --- | --- |
| preflight | 15:42:31-15:42:34 passed | 15:55:23-15:55:26 passed | passed | 16:24:25-16:24:30 passed |
| origin | 15:42:34-15:42:42 passed | 15:55:26-15:55:34 passed | passed | 16:24:30-16:24:38 passed |
| baseline-install | 49 s passed | 64 s passed | passed (+ restart the installer demanded) | 159 s passed (restart 80.8 s) |
| owner-login, license | passed ("not contacted") | passed | passed | passed |
| setup | 15:43:32-15:49:45 observed (access_dns wait) | 15:56:39-16:03:02 observed | observed | 16:27:18-16:30:06 observed |
| seed | passed (site, mailbox, cron) | passed | passed (site, cron; no mail) | passed |
| pre-state | 15:49:46-15:51:13 passed | 16:03:03-16:05:11 passed | **failed** web_ok | **failed** web_ok |
| arm (origin before arm, check) | 15:51:13-15:51:14 passed | 16:05:11-16:05:12 passed | not-run | not-run |
| owner-start | 15:51:14 passed (202 queued; readiness ready at once) | 16:05:12 passed (202) | not-run | not-run |
| track | 15:51:14-15:53:24 passed (28 samples) | 16:05:12-16:06:06 passed (12 samples) | not-run | not-run |
| owner-continuation | skipped (not required) | skipped | not-run | not-run |
| terminal | passed | passed | not-run | not-run |
| collect | passed | passed | passed | passed |
| verdicts | passed | passed | not-run | not-run |

## What each cell observed

### Setup, cron and mail

**Debian 13.** The `web_mail` plan was admitted with `blockers: []`. Setup ran as follows:

- Steps `01-09` succeeded, including `07-service` target `cron`.
- The run then waited at `10-access_dns` (`server_setup_access_dns_required`, isolated host;
  H4 rule, stable 120 s).
- `cron.service` was active and enabled, from package `cron 3.0pl1-197`.
- The seeded job sits in `/var/spool/cron/crontabs/upd1_owner_test` as
  `# upd1 owner cron` / `* * * * * /bin/date -u -Iseconds > "$HOME/upd1-cron-stamp.txt"`.
- The panel wrote nothing into `/etc/cron.d`.
- The cron stamp advanced before the update (`side/inspect-*.txt`, `seed`/`pre-state`).

**Arch.** The `web_mail` plan was refused at review (HTTP 200, `can_start: false`, blockers
`["server_setup_service_unsupported:dovecot"]`,
`upd1-arch-good/run-a/steps/06-setup/api/0012-post-api-v1-setup-plan.json`). The driver fell
back to `web`, which was admitted with `blockers: []`. What the wizard shows for this blocker
(`setup.blocker.mailUnsupported`, `web/src/components/ServerSetup.tsx:200`):

- EN: "Mail cannot be set up automatically on this server's Linux distribution yet: CelikPanel
  cannot install and configure its mail server here. The server administrator can choose Web
  hosting to continue without mail, or install and configure mail with the operating system's
  own tools."
- TR: "Bu sunucunun Linux dağıtımında e-posta henüz otomatik kurulamıyor: CelikPanel posta
  sunucusunu burada kurup yapılandıramıyor. Sunucu yöneticisi e-posta olmadan devam etmek için
  Web barındırma'yı seçebilir ya da e-postayı işletim sisteminin kendi araçlarıyla kurup
  yapılandırabilir."

Arch setup then ran as follows:

- Steps `01-07` succeeded, including `05-service` target `cron`, and the run waited at
  `08-access_dns`.
- `cronie.service` (package `cronie 1.7.2-2`) was active and enabled.
- The job is in `/var/spool/cron/upd1_owner_test` (owner upd1_owner_test, mode 0600), and no
  panel file was added to `/etc/cron.d`.
- `crond` runs the job every minute, but it logs `ERROR chdir failed
  (/var/www/celikpanel/subscriptions/2/sites/1): Permission denied` (P3).

### Fixture origin and port 443

`celikpanel.net` resolved only to 127.0.0.1, and the fixture answered HTTP 200:

- after provisioning, in every run that got that far (arch-def run-a failed only in the parser);
- after the owner restart the Arch installer demanded (arch runs b and a);
- before `arm` (both Debian runs).

The unit `cp-lab-upd1-origin.service` stayed active and enabled with `NRestarts=0`, and it came
back by itself after the Arch restart and after the Debian QMP reset.

The fixture listens on `127.0.0.1:443`. nginx listened only on `:80` in every run, and the Panel
on `:2083`, so no conflict was observed. The panel and mail certificate steps never ran (setup
waits at `access_dns`), so no nginx TLS listener existed. A conflict with a later `0.0.0.0:443`
nginx listener is therefore **not tested**.

### Owner update sequence (d13-def run-b; d13-good run-a is analogous)

`arm`:

- Origin proof at 15:51:13Z.
- `GET /api/v1/panel/update/check` offered the sealed candidate alpha.82 (commit and archive
  SHA equal to the fixture).
- The observer was armed at 15:51:14.83Z.

`owner-start`:

- `host-mutation-readiness` was `ready: true` on the first read, with no wait.
- The durable attempt record `host/upd1-owner-start-<rid>.json` was written at 15:51:14Z.
- One `POST /api/v1/panel/update/start` returned 202 with `status: queued`.

`track`: polled at the UI backoff (1.5 s x1.6, up to 15 s), 28 samples, 15:51:14Z to 15:53:24Z
(`host/status-samples-extract.txt`).

### Faults and recovery (d13-def run-b)

**Fault 1.** The worker failed at 15:51:59Z with `transaction_phase: active` (observer timeline).
The server's line: "offline panel database migration failed; its original database and work
evidence are preserved". The candidate-installed checkpoint was proven at 15:51:53.79Z, with no
signal sent.

**Automatic recovery attempt 1** was admitted at 15:52:00.6Z.

**Fault 2.**

- The `payload_restored` checkpoint was verified at 15:52:17.53Z, followed by `reboot_ready`.
- One QMP `system_reset` was sent at 15:52:18.1Z.
- The boot id changed, and SSH was back after 8.3 s.

**After the boot:**

- The start guard held the Panel and Agent.
- At 15:52:30 recovery logged, in EN and TR: "Recovery waiting for the operating system
  transition; no owner action is needed. The native recovery timer will retry this same
  operation. Recovery is not yet complete." / "Kurtarma işletim sistemi geçişini bekliyor;
  kullanıcı işlemi gerekmiyor. Yerel kurtarma zamanlayıcısı aynı işlemi yeniden deneyecek.
  Kurtarma henüz tamamlanmadı."
- **Automatic attempt 2** was admitted at 15:53:02.2Z by the timer.
- "Rollback complete" was logged at 15:53:23Z, and `rollback_verified` was observed at
  15:53:24Z.

**Result.** The outcome was `recovered-automatically`: 2 automatic attempts, 0 owner attempts.
The retry budget was not exhausted, so no owner continuation was needed and none was run.

**Arch fault 2** (kill at `runtime_verified`) was not reached.

### Views and agreement

Samples were taken with the web UI's backoff. In both Debian runs the reachable views agreed
(agreement verdict `passed`: d13-def 8 of 28 two-source samples agreed, d13-good 6 of 12), with
one exception:

- The first sample at the start instant disagreed on phase: the Panel recovery API said
  `accepted/operation_accepted` while the root CLI already said `running`.
- It was a single sample, so the driver tolerates it; it is recorded as an observation.

Which views were reachable when:

| Window (d13-def run-b) | Panel update status | Panel recovery status | Root CLI EN/TR | Offline page |
| --- | --- | --- | --- | --- |
| 15:51:14-15:51:31 | yes | yes | yes (agree) | served by the Panel |
| 15:51:42-15:53:13 (Panel down; reset at 15:52:18) | no (ConnectionReset) | no | yes, the only source; no sample fell in the ~11 s SSH gap | not served; only a browser-saved copy could show |
| 15:53:19-15:53:24 | yes (`failed`) | yes (`recovering`, then `recovered/rollback_verified`) | yes (agree) | served |

In d13-good the Panel was down from 16:05:39 to 16:05:55, and there too the CLI was the only
source. The offline page was fetched before the update: HTTP 200 (2119 bytes). Its module
carries the exact read-only command `sudo /usr/libexec/celikpanel/recovery status --request-id
<rid> --lang en|tr`.

Every guidance text seen, verbatim in EN and TR, is in `host/guidance-texts-extract.txt` of both
Debian runs. The recovery reader texts were actionable in every sample
(`no_actor_or_action: false`, no missing keys). The observations are listed below.

### Outage windows (5 s guest sampler, guest clock; skew +0.39 s)

| Workload | d13-def run-b | d13-good run-a |
| --- | --- | --- |
| Site (Host header + marker) | 15:52:12-15:52:33: up to 20.8 s, failed plus unobserved, overlaps the reset -> `interrupted-only-by-host-reset` | never interrupted |
| SMTP 587 banner | 15:52:12-15:52:28: up to 15.8 s, unobserved (guest rebooting) -> `interrupted-only-by-host-reset` | never interrupted |
| Cron stamp | never interrupted | never interrupted |
| Panel HTTPS | 15:51:32-15:53:18: 95.8-105.8 s -> `down-only-during-transaction` (labelled host-reset because it overlaps the reset) | 16:05:29-16:05:59: 20.0-30.0 s -> `down-only-during-transaction` (label `unexplained`: no reset in that cell) |
| Host view of the Panel through the tunnel | 96.9-106.9 s | 20.0-30.0 s |
| Host SSH | 15:52:17-15:52:29: 0-11.9 s | none |
| DNS | not provided (`not-provided-external-dns`) | not provided |

### Terminal checks

Both Debian runs passed every terminal check:

- **Build identity.** Expected B (alpha.81, `e3a569d5`) after the rollback and G (alpha.82,
  `74fe2fe6`) after the update. Running executables matched the installed ones for the Panel and
  the Agent.
- **Database.** `equal-except-volatile`, with 65 tables and an equal schema. Excluded tables
  (with reasons): audit_logs, metrics_samples, sessions, sqlite_sequence and
  server_setup_executions (setup wait). Differing: metrics_samples and server_setup_executions.
  `unexpected: []`.
- **Rows and services.** The seeded rows (domain, cron job, mailbox) were present. The site
  marker was served, the mailbox existed and SMTP answered.
- **Timers and firewall.** The timers were equal (release-recovery, certbot). The firewall
  ruleset was byte-equal.
- **Login and license.** A fresh login worked, and the license was `active`.
- **Floor and foundation.** Both say sequence 82 / alpha.82 in both cells.
- **Transaction record.** Absent.
- **Update card after the rollback.** `status: failed` with the raw server summary (see O1),
  and `check` offers the same alpha.82 again.

## Product findings

**P3 (D-024; stops both Arch cells).** The site and cron of a newly created static site do not
work on Arch.

- **Symptoms.**
  - nginx (worker user `http`) logs `stat() ".../subscriptions/2/sites/1/public_html/" failed
    (13: Permission denied)` and answers 404 for `Host: upd1-owner.test`.
  - `crond` logs `chdir failed (/var/www/celikpanel/subscriptions/2/sites/1): Permission denied`
    for the site user, so the owner's cron job never produces output.
  - Both reproduced on two fresh guests. Evidence:
    `upd1-arch-good/run-a/side/inspect-after-seed-*.txt` and the `journal-setup-services.txt`
    of both Arch runs.
- **What the owner is shown.** Nothing names the problem: the site was created with HTTP 200 and
  the cron job is listed.
- **Hypothesis (not proven).** An ancestor directory, most likely `/var/www`, is not traversable
  by anyone but root:
  - Arch's nginx does not create `/var/www`.
  - The Agent runs with `UMask=0027` (`deploy/systemd/celikpanel-agent.service:45`).
  - `applyHostingLayout` chmods `/var/www/celikpanel` and below but not `/var/www`
    (`cmd/agent/hosting_layout.go:50-67`).
  - Per-directory modes were not captured before the labs stopped.

**O1 (D-024 observation).** After a rollback, the update card shows the server summary verbatim
in both languages. The text is "reviewed updater failed: exit status 1: !!
CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=offline panel
database migration failed; its original database and work evidence are preserved detail=".

- It comes from `web/src/components/SystemUpdateOperation.tsx:529`, and it is English in TR.
- It says `state=recovery_required` while recovery has already verified the rollback.
- The recovery reader next to it is correct: "Rollback verified / Geri alma doğrulandı".

**O2 (observation).** After the rollback, the floor and the recovery foundation stay at 82 /
alpha.82.

- This is by design: the floor advances before any host mutation (`docs/release-signing.md:255-257`).
- `check` then offers the same failed alpha.82 again. The owner can restart the defective
  candidate, and only 82 or higher is admissible.

**O3 (wording).** The root CLI texts contain raw tokens and internal wording:

- "The producer recorded verified restoration." / "Üretici, doğrulanmış geri yükleme kaydetmiş."
- "Previous failure: update_failed" / "Önceki hata: update_failed".
- "Terminal proof: none" / "Nihai kanıt: none".
- The rollback journal says "Artifact source commit / Ürün kaynak commit'i: unknown", and the
  snapshot is named `from-unknown-to-...` although the installed commit `e3a569d5` was known.

**O4 (wording).** After the reset, `release-transaction-start-guard` logs `unsafe directory:
/run/celikpanel-release-transaction` while it intentionally holds the Panel and Agent for the
pending recovery. The owner-facing texts at the same time are correct.

**O5.** At the start instant, the Panel recovery API says `accepted` while the CLI says
`running`, for one sample.

**Confirmed from the previous run.** Setup now installs cron (Debian `cron`, Arch `cronie`), the
cron API worked on both platforms, and Arch `web_mail` is refused at review with typed guidance
and a `web` fallback. The upd1 findings P1 and P2 did not recur.

## Real origin

- The helpers never asked the resolver for `celikpanel.net` before the fixture existed
  (preflight read `/etc/hosts` and `nsswitch` only).
- After provisioning, the name resolved only to loopback on both platforms.
- `185.95.0.123` (the address the real name resolved to in upd1) appears in no file here.
- The only collected journal lines that name `celikpanel.net` are the fixture unit's start and
  stop lines.
- No baseline installer log names `celikpanel.net` or `185.95.` (`secret-scan.txt`).
- The fixture logs no requests. The arch-def run-a `curl` 200 is attributed to the loopback
  fixture by the resolver diagnosis above.

## Secret scan

The scan ran over the whole folder before hashing (`secret-scan.txt`). Result: **clean**.

- 0 PEM private-key blocks.
- 0 hits for 353 body lines of 25 key files: every lab's SSH key, fixture signing key, fixture
  CA key and fixture TLS key, across 7 labs including the diagnosis lab.
- 0 hits for the D-027 fixture licence literal, and 0 `CPK-` keys.
- 0 unredacted password, secret, token, TSIG, CSRF, session or cookie values.
- The only tokens of the admin-password shape (43-character base64url) are three unit-test
  names. There are 0 tokens of the mailbox-password shape.
- 534 `[REDACTED]` markers.

## Files

- `build/`: artifacts JSON, dist JSONs, guard refusals, archive entries, fixture commits and
  diff, build logs, prove output, dry run, and the offline tests.
- `harness-run-copy/`: the H6/H7 diffs, the run-copy file hashes, the offline tests of the run
  copy, and the job and wrapper scripts.
- `observer-tools/`: the read-only side inspection (`cpinspect.py`, `sidecar.sh`), the resolver
  diagnosis, and the extract scripts. They are not part of the driver and changed no guest state
  except the diagnosis guest's `/etc/hosts` test line.
- `diag-arch-resolver/`: the resolver diagnosis output.
- `<cell>/run-<x>/`: the driver's evidence directory unchanged (its own `SHA256SUMS` verified
  when staged).
  - `host/`: wrapper stdout, stderr, start, end and rc; lab name, run id and harness; the
    lab-side intent, observer intent, owner-start attempt, reboot attempt and recovery-fault
    collection; the baseline result; and, for the Debian runs, the sample and guidance extracts.
  - `side/`: the read-only inspections at seed, pre-update and after track.
- `secret-scan.txt`, and `SHA256SUMS` over every file here except itself.

## Host leftovers (archlinux WSL)

- `/var/tmp/cp-upd1-build/20260930t152731z` (589 MB): the clone and archives, written by the
  harness's own build script (its fixed prefix).
- `/var/tmp/cp-pair-accept/dist/{e3a569d527ec88c01e14c1df015fa3010ea7f803,74fe2fe6ed0dd06ba50a53ee92cbca4648ceb352,ae713f912af8d8e9153a2053cc2ad6f7abe66851}-acceptance-license`:
  new directories written by `build-dist.sh`. Nothing that existed before was modified.
- `/var/tmp/cp-upd2-run`: the run copy, the a81da45d export, logs, sidecar inspections, diffs,
  the stage and the scan.
- Stopped labs `/var/tmp/cp-release-drill-upd2-{d13-def-a,d13-def-b,d13-good-a,arch-def-a,arch-def-b,arch-good-a,diag-arch}`,
  1.0 to 2.7 GB each, with overlays and per-lab fixture keys. d13-def-a was stopped with an
  update in flight.
- The build used the Go build cache and npm cache in root's home.

No QEMU process is running, and nothing else under `/var/tmp` or `/root` was deleted. In the
repository, Python refreshed the gitignored `deploy/e2e/{release-recovery,dns-pair-acceptance}/__pycache__`
when the harness ran from the working tree.

## What this run proves and does not prove

It shows, on disposable Debian 13 guests with the acceptance fixture:

- one owner-started update of a defective candidate that failed in phase `active` and was rolled
  back automatically to the old release, through a host reset at `payload_restored`, with two
  automatic attempts and no owner action;
- one owner-started forward update verified on the good candidate;
- that the owner's Panel API, the root CLI (EN/TR) and the saved offline page named the same
  operation, and that the views which answered agreed;
- workload windows limited to the reset (site, SMTP) or to the transaction (Panel); cron was
  never interrupted;
- preserved database, rows, timers and firewall, and a fresh owner login;
- that setup installs cron and that Arch `web_mail` is refused at review.

It does **not** show:

- any update, rollback or fault on Arch (the kill at `runtime_verified` was not reached);
- owner continuation after an exhausted retry budget (not triggered);
- DNS continuity (external mode) or mail on Arch;
- a port-443 coexistence with an nginx TLS listener;
- production signing, browser rendering, power-loss durability, external DNS or mail delivery,
  or certificate issuance.

It closes no P0 row. Wall time: about 15:25Z to 16:50Z.

## Corrections after intake (2026-10-10)

After this directory was published (2026-10-10), 13 digest values of the one-shot update-transaction token (11 in plain text in 6 files, as `transaction_token_sha256` values and as `.release-db-migrations/<digest>` directory names; 2 inside base64 `events_base64` text in 1 file) were replaced by `[REDACTED-SHA256]`.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.
