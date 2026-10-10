# set2 native run, 2026-10-09 (UTC): the set1 corrections, service actions and request identity on real services (`faa5ef085`)

Second native run of the cell kind `settings-writes` and first native run of the new cell kind `request-identity`.
Three groups of behaviour of the candidate `faa5ef085` had been verified by component tests and by a mocked browser
only: (A) the corrections made after the first native measurement of settings writes (set1), (B) the Services page's
actions, whose answer is now what the daemon shows, and (C) the request identity of D-029 on its eight routes. This
run measures them with the packaged services on disposable QEMU/KVM guests of the local `archlinux` WSL host:
Debian 13 and Ubuntu 24.04 for all three groups, Arch for group C.

Everything was built from commit `faa5ef085` (run copies are `git archive faa5ef085` plus the harness files listed
below; the working tree was never used). No installed server (Boston, Frankfurt, any) was touched or contacted.
Nothing was committed, pushed, published or signed with a production key. `celikpanel.net` resolved only to each
guest's own loopback fixture; no licence service was contacted (acceptance-fixture licence). No update was started.
No certificate authority was contacted or imitated: the two Let's Encrypt directory names resolved to each guest's
own loopback, and the one cell on which that was not confirmed was stopped before its Let's Encrypt section.
Every `result.json` carries `native_evidence: false`; the owner judges it. **It closes no P0 row.**

**Result in one sentence:** on Debian 13 and Ubuntu 24.04 every correction of set1 is measured as made (A1-A4 pass,
every set1 section still passes), the answer of 40 of 41 service actions per platform matches what the service
shows and the 41st is answered as unknown, and on all eight guarded routes one identity produced one native effect
whether it arrived three times in a row, three times at once, without its header or with another body, after a
dropped connection and after the Panel was killed (the same on Arch for the routes that work there); **three
candidate product defects** were measured (a failed Reload is answered "keeps running with the settings it had" when
PostgreSQL had re-read its files and when Postfix or Dovecot is not running at all; the cPanel import refuses the
site files of an archive that holds the directory member `homedir/public_html/` the way tar writes it; on Arch a PHP
site cannot be created, so every cPanel import answers `500`), plus the observations listed under Findings, the
first of which is that `systemctl restart celikpanel-panel` does **not** interrupt a running restore.

## What was built from what

One build from the pristine run copy (`go1.26.5 linux/amd64`), every archive `license_mode: acceptance-fixture`
(`build/cur/`: artifact document, dist JSONs, fixture commits and patches, build logs).

| Build | Role | Label / seq | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `run-upd1.sh build faa5ef085` (03:04:55-03:09:14Z) | Baseline B = **the installed candidate** | v0.1.0-alpha.81 / 81 | c00645036feb4210a04b357ffffc15cc215d010a | 1b464cd7e4bc51f7b791938e150dc4ec05f2bf47 | 795ac99bde17490b5ca13b1685e197fc7314410839292b0e92d3e611a0d9f8d0 |
| | Good G (sealed into the fixture origin, never installed) | v0.1.0-alpha.82 / 82 | 13d83c01d13e0725fe6b53afbcc878d51f5c68f8 | 4099185513f46ced9a29e585ce9f4c5c5a0e1632 | 7a6193462a1a2c41f3e691fa4f599671170913f557cdd0755ca7705ad31876d6 |
| | D, S, R (built by the script, not used) | v0.1.0-alpha.82 / 82 | see `build/cur/upd1-artifacts.json` | | |

- B's tree `1b464cd7...` **is the tree of `faa5ef085`** (`build/cur/baseline-tree-equals-source.txt`): the guests ran
  exactly the current source, with the acceptance licence seam switched on at build time.
- `prove` exited 0 (`build/cur-prove.json`); the dry runs of the four Debian and Ubuntu cells exited 0 on every run
  copy with `native_evidence: false` and created no lab (`build/set2-dry-*.json`); the Arch cell's plan was validated
  by the wrapper before its lab was created.
- Offline suites, all OK on the pristine copy (`p`) and on each overlay copy (`a`, `b`, `c`, `d`, `e`;
  `build/offline-*`): `test_owner_update_trial` 189, `test_recovery_candidate_archive` 11, `test_lab` 15,
  `test_current_worker_baseline` 7, `test_worker_fixture_origin` 15, `test_bound_worker_reboot` 11,
  `test_guest_bound_worker` 8, `test_guest_probe` 13; `test_settings_writes_trial` 26 (27 on `d` and `e`), the new
  `test_request_identity_trial` 13 (14 on `d` and `e`).

## The cell kinds, the run copies and the harness changes

| File (`deploy/e2e/release-recovery/`) | What |
| --- | --- |
| `settings_writes_trial.py` (changed) | Cells `set2-debian13`, `set2-ubuntu`, `set2-arch` beside the set1 names. S1 f applies every cron fault and judges the cause the answer carries; S2 c checks `applied`, f expects `check` with the written policy, new g (save without a change) and h (stopped Postfix); S4 g expects `restored_unit_reload_failed`, no copy, and runs the Agent's own statement batch; S5 c expects `plenty` refused; S6 expects `postfix_config`; S7 checks that each file is listed once; **new S8, service actions** (group B). |
| `guest_settings_native.py` (changed) | New read-only mode `read-service` (the `systemctl show` properties the Agent reads, `systemctl cat`, `postfix status`, the master's and the postmaster's PID); `owner-pg-reread` (the Agent's statement batch of `db_config.go`, run the same way); `postfix status` and the master's liveness in `read-postfix`; `read-mariadb-check` can read a text that is not on disk; `dovecot` and `nginx` among the units. |
| `request_identity_trial.py` (new) | The driver of the cell kind `request-identity` (group C): cells `rid-debian13`, `rid-ubuntu`, `rid-arch`. It reuses the settings cell's steps up to `site`, then C0-C9. It sends the guarded requests itself so that the identity, the moment and the connection are under its control; everything else goes through the Panel client. |
| `guest_request_identity_native.py` (new) | Guest helper: `read-*` modes (backup archives with their manifests, document root digest, engines, a login with a given password, WireGuard peers by public key, the Panel's rows without secret columns, `request_identities` without its stored bodies); `owner-*` modes (site files, rows, a cPanel archive in the import directory, `systemctl restart celikpanel-panel`); `lab-*` modes (ACME isolation, the Panel's process killed). |
| `run-set2.sh` (new) | Wrapper: `dry-run` and `cell` for both cell kinds (new lab, one cell, lab stopped). |
| `test_settings_writes_trial.py` (changed), `test_request_identity_trial.py` (new) | Offline tests of the pure rules; they pin the header, the refusal codes, the eight route patterns, the paths, the Agent's statement batch and the `systemctl show` properties against the product source. |

Setup profile: Debian/Ubuntu purpose `web_mail` with `nginx, php-fpm, mariadb, postfix, dovecot, roundcube, rspamd,
postgresql`; Arch purpose `web` with `nginx, php-fpm, mariadb, postgresql`. DNS mode external. The site of every cell
is `set1-owner.test` (static), with one mailbox where mail exists.

Harness defects found and corrected during the run (product code was never changed):

| Id | Defect | Handling | Runs |
| --- | --- | --- | --- |
| H27 | S2 f and S8 took "Postfix is running" from `postfix status`, which itself exits 1 with Postfix's fatal line while `main.cf` is refused. | The master's PID file and `/proc`; `postfix status` is recorded beside it. | set2 run-a (both) affected: its one failed S2 check is this; fixed from copy `b` |
| H28 | The Databases page's `type_name` is the display name (`MariaDB`); the driver looked for `mariadb`. | Compared in lower case. | rid-ubuntu run-a: C2, C3 not reached; fixed from `c` |
| H29 | A domain's MariaDB and PostgreSQL databases carry the same name; the driver compared names only. | Rows are compared per server. | rid-ubuntu run-a: C1 PostgreSQL i, ii "FAIL" are this; fixed from `c` |
| H30 | Signs of a reload were also looked for after a restart. | Only after a Reload. | cosmetic in set2 run-a; fixed from `b` |
| H31 | A service action refused `409 server_setup_busy` before anything was sent was taken as the action's answer (it left Postfix stopped at the end of S8). | The owner waits and presses again; refusals are recorded per action. | set2-ubuntu run-a; fixed from `b` (see O11) |
| H32 | A helper argument named `label` collided with the driver's own parameter. | Renamed. | rid-ubuntu run-a: C8 never ran; fixed from `c` |
| H33 | Evidence keys containing `secret` or `password` were blanked by the evidence redactor (counts and booleans lost). | Keys renamed; no value changed. | rid-ubuntu run-a; fixed from `c` |
| H34 | The fixture archive held the directory member `homedir/public_html/`; the import's files step refuses it (finding P4). | The arrivals use archives without that one member; one archive with it is imported once and judged. | rid-ubuntu run-a: import i, ii, v "FAIL" are P4 through this; from `c` |
| H35 | The database-server route does not keep its answer when the owner sends the new user's password either (O10). | PostgreSQL arrivals use an existing database user (kept answer); MariaDB a minted password (status only); one request with a sent password is recorded. | from `c` |
| H36 | `systemctl restart` did not cut the restore, and the driver took `503 PANEL_STARTING` for the replay's answer. | Arrival vi is measured twice (restart; process killed), each followed by a read-only wait until the Panel serves management requests. | from `c` |
| H37 | The settings driver's list of screen keys was older than the screens for four answers. | `texts-en-tr.md` was generated with the screens' keys; the driver is corrected in copy `d`. | texts only; no cell |
| H38 | On Arch the resolver still answered the public address right after the lab wrote its ACME isolation. | rid-arch run-a was stopped by hand before its Let's Encrypt section (it had passed C0-C3; no request reached that route); the helper waits until the names resolve to loopback only (2.1 s in run-b), and the section sends nothing unless that is confirmed. | rid-arch run-a stopped; fixed in `d` |
| H39 | The Let's Encrypt section also compared the number of certbot's log files with what one entry left; the first certbot run on a guest creates two, each later run one. | The files are recorded, not compared; the Panel's log line and the nginx reloads are. | rid-arch run-b: its Let's Encrypt i, ii "FAIL" are this (one log line and four reloads per identity, as for one entry); fixed in `e`, no cell |

Run copies (`harness-run-copy/`: `runcopy-faa5ef085-files.sha256`, 49,201 files; per overlay `files.sha256`,
`harness.diff`, `differs-from-archive.txt`; job records `jobs/`): `p` = pristine archive (build, prove, offline `p`);
`a` (set2 and rid-ubuntu run-a); `b` = `a` + H27, H30, H31 (set2 run-b on both platforms); `c` = `b` + H28, H29,
H32-H36 (rid-debian13 run-a, rid-ubuntu run-b, rid-arch run-a); `d` = `c` + H37, H38 (rid-arch run-b); `e` = `d` + H39
(offline suites and dry runs only) = **the working tree's files** (SHA-256 equal,
`harness-run-copy/overlay-e/files.sha256`). `PYTHONDONTWRITEBYTECODE=1`.

A harness debugging session ran between the cells and is **not evidence**: the finished rid-ubuntu run-a guest was
started again (03:54:51-04:05Z) and the sections that run-a never reached were run on it from a mutable development
copy, to find H35 and H36 before the remaining runs. Its output stayed on the host (`rid-debug-*`).

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`, GiB; `host/c-drive.txt`)

141.7 at the first reading before any work (03:03Z); then 139.3 (start, 03:03:44Z); 134.4 (before set2-ubuntu and rid-ubuntu (run-a), 03:27:50Z); 131.5 (before set2-debian13 (run-a), 03:30:56Z); 127.3 (before set2-debian13 and set2-ubuntu (run-b), 03:51:23Z); 121 (before rid-ubuntu (run-b) and rid-debian13 (run-a), 04:05:23Z); 121 (before rid-arch (run-a), 04:14:42Z); 119.8 (before rid-arch (run-b), 04:24:31Z); 116.8 (end (all cells staged, all overlays of this run removed), 04:40:49Z). Lowest reading: **116.8 GiB**. Never under 40 GiB before a cell. A
process-level keep-awake request and one idle WSL session were held for the run (`host/keepawake.log`); both were
ended at the end.

## Cells

One new lab per cell, stopped by the wrapper (`<cell>/<run>/host/wrapper.*`); per-step times in `sections-table.md`.
Cells ran two or three at a time on the host.

| # | Cell / run | Lab, SSH port | Harness copy | Wrapper (UTC) | Driver steps (UTC) | Overall |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | set2-ubuntu/run-a | set2-ub-a, 4121 | a | 03:28:00-03:49:06 | 03:28:22-03:49:06 | `failed` (S2-mail-policy, S8-service-actions) |
| 2 | rid-ubuntu/run-a | rid-ub-a, 4141 | a | 03:28:02-03:51:16 | 03:28:24-03:51:16 | `failed` (C0-prepare, C1-domain-databases, C2-admin-account, C3-server-databases, C7-import, C8-restore, C9-identities) |
| 3 | set2-debian13/run-a | set2-d13-a, 4111 | a | 03:30:56-03:43:45 | 03:31:55-03:43:44 | `failed` (S2-mail-policy, S8-service-actions) |
| 4 | set2-debian13/run-b | set2-d13-b, 4151 | b | 03:51:29-04:05:25 | 03:52:32-04:05:24 | `failed` (S8-service-actions) |
| 5 | set2-ubuntu/run-b | set2-ub-b, 4161 | b | 03:51:31-04:12:27 | 03:51:55-04:12:26 | `failed` (S8-service-actions) |
| 6 | rid-ubuntu/run-b | rid-ub-b, 4171 | c | 04:06:32-04:32:12 | 04:06:57-04:32:12 | `failed` (C7-import) |
| 7 | rid-debian13/run-a | rid-d13-a, 4131 | c | 04:06:34-04:20:03 | 04:07:35-04:20:02 | `failed` (C7-import) |
| 8 | rid-arch/run-a | rid-arch-a, 4181 | c | 04:14:42-04:22:29 | - | stopped by hand; no result was written |
| 9 | rid-arch/run-b | rid-arch-b, 4191 | d | 04:24:48-04:35:36 | 04:25:51-04:35:36 | `failed` (C5-letsencrypt, C7-import) |

Each platform was run once and re-run at most once, for harness defects only: set2 run-a → run-b (H27, H31) on
Debian 13 and Ubuntu; rid-ubuntu run-a → run-b (H28, H29, H32-H36); rid-arch run-a (stopped, H38) → run-b;
rid-debian13 ran once. The tables below take the latest run of each cell; the earlier runs are kept as run
(rid-arch run-a has no `result.json`: its files were compared with their source one by one,
`rid-arch/run-a/host/staged-files.sha256`). Every `overall` is `failed` because at least one check did not pass in
every run; which ones, and why, is in the sections below and in `checks-not-passed.txt`.

Platform facts read on the guests: Debian 13: Postfix 3.10.13, PostgreSQL 17.11, MariaDB 11.8.6. Ubuntu 24.04:
Postfix 3.8.6, PostgreSQL 16.15, MariaDB 10.11.14. Arch: PostgreSQL 18.6.

## Group A: the set1 corrections

Generated from the cells (`group-a-settings-writes.md`; verdicts are the driver's, every check with its detail in
`<cell>/<run>/steps/NN-*/section.json`).

| Item | set2-debian13/run-a | set2-debian13/run-b | set2-ubuntu/run-a | set2-ubuntu/run-b |
| --- | --- | --- | --- | --- |
| A1 mail policy: healthy save reloads (S2 c) | pass (8 checks) | pass (8 checks) | pass (8 checks) | pass (8 checks) |
| A1 mail policy: refused main.cf -> 502 check with policy (S2 f) | **FAIL** (1 of 9 checks) | pass (9 checks) | **FAIL** (1 of 9 checks) | pass (9 checks) |
| A1 mail policy: unchanged save reloads (S2 g) | pass (3 checks) | pass (3 checks) | pass (3 checks) | pass (3 checks) |
| A1 mail policy: stopped Postfix stays stopped (S2 h) | pass (6 checks) | pass (6 checks) | pass (6 checks) | pass (6 checks) |
| A2 PostgreSQL reload hook -> restored_unit_reload_failed (S4 g) | pass (9 checks) | pass (9 checks) | pass (9 checks) | pass (9 checks) |
| A3 O1 MariaDB `plenty` refused; stock file saves (S5 b, c) | pass (10 checks) | pass (10 checks) | pass (10 checks) | pass (10 checks) |
| A3 O3 cron.allow cause; relocated spool neutral (S1 f) | pass (11 checks) | pass (11 checks) | pass (11 checks) | pass (11 checks) |
| A3 O3 queue: main.cf typo -> postfix_config (S6) | pass (2 checks) | pass (2 checks) | pass (2 checks) | pass (2 checks) |
| A3 O5 each configuration file listed once (S7) | pass (1 checks) | pass (1 checks) | pass (1 checks) | pass (1 checks) |
| A4 setup completes to the isolated DNS wait | pass: step observed, waits at `access_dns` (server_setup_access_dns_required); 10 steps succeeded; mail steps reached: True | pass: step observed, waits at `access_dns` (server_setup_access_dns_required); 10 steps succeeded; mail steps reached: True | pass: step observed, waits at `access_dns` (server_setup_access_dns_required); 9 steps succeeded; mail steps reached: True | pass: step observed, waits at `access_dns` (server_setup_access_dns_required); 9 steps succeeded; mail steps reached: True |

Regression (every section that passed in set1):

| Section | set2-debian13/run-a | set2-debian13/run-b | set2-ubuntu/run-a | set2-ubuntu/run-b |
| --- | --- | --- | --- | --- |
| S1-cron | **passed** (35 checks, 0 not passed; 03:39:54-03:39:59) | **passed** (35 checks, 0 not passed; 04:01:21-04:01:26) | **passed** (35 checks, 0 not passed; 03:44:22-03:44:29) | **passed** (35 checks, 0 not passed; 04:07:53-04:08:00) |
| S2-mail-policy | **failed** (38 checks, 1 not passed; 03:39:59-03:40:42) | **passed** (38 checks, 0 not passed; 04:01:26-04:02:14) | **failed** (38 checks, 1 not passed; 03:44:29-03:45:18) | **passed** (38 checks, 0 not passed; 04:08:00-04:08:50) |
| S3-backup-schedule | **passed** (10 checks, 0 not passed; 03:40:42-03:40:44) | **passed** (10 checks, 0 not passed; 04:02:14-04:02:15) | **passed** (10 checks, 0 not passed; 03:45:18-03:45:20) | **passed** (10 checks, 0 not passed; 04:08:50-04:08:52) |
| S4-postgresql | **passed** (37 checks, 0 not passed; 03:40:44-03:41:02) | **passed** (37 checks, 0 not passed; 04:02:15-04:02:35) | **passed** (37 checks, 0 not passed; 03:45:20-03:45:38) | **passed** (37 checks, 0 not passed; 04:08:52-04:09:11) |
| S5-mariadb | **passed** (21 checks, 0 not passed; 03:41:02-03:41:11) | **passed** (21 checks, 0 not passed; 04:02:35-04:02:44) | **passed** (21 checks, 0 not passed; 03:45:38-03:45:47) | **passed** (21 checks, 0 not passed; 04:09:11-04:09:21) |
| S6-catchall-queue | **passed** (15 checks, 0 not passed; 03:41:11-03:41:20) | **passed** (15 checks, 0 not passed; 04:02:45-04:02:54) | **passed** (15 checks, 0 not passed; 03:45:47-03:45:56) | **passed** (15 checks, 0 not passed; 04:09:21-04:09:30) |
| S7-file-metadata | **passed** (9 checks, 0 not passed; 03:41:20-03:41:21) | **passed** (9 checks, 0 not passed; 04:02:54-04:02:55) | **passed** (9 checks, 0 not passed; 03:45:56-03:45:58) | **passed** (9 checks, 0 not passed; 04:09:30-04:09:31) |
| S8-service-actions | **failed** (54 checks, 2 not passed; 03:41:21-03:43:43) | **failed** (54 checks, 2 not passed; 04:02:55-04:05:23) | **failed** (54 checks, 3 not passed; 03:45:58-03:49:04) | **failed** (54 checks, 2 not passed; 04:09:31-04:12:25) |

### A1 (P1) mail policy, on both platforms (`steps/09-s2-mail-policy/`)

Times are of run-b (Debian 13 / Ubuntu 24.04).

- **Healthy save** (S2 c, 04:01:35 / 04:08:09): `200`, `applied: reloaded`. Journal: `postfix/master[28494]: reload --
  version 3.10.13, configuration /etc/postfix` (Debian), `postfix/master[39065]: reload -- version 3.8.6, configuration
  /etc/postfix` (Ubuntu); the master PID is the same before and after.
- **`main.cf` holds a line Postfix refuses** (S2 f, 04:01:58 / 04:08:32; the owner's `default_process_limit = 200 #
  raised for the campaign`, not reloaded): **`502 MAIL_POLICY_NOT_RELOADED`, reason `check`, on both platforms**, with
  `mutation_applied: true`, `vars.detail` = `postfix: fatal: bad numerical configuration: default_process_limit = 200
  # raised for the campaign`, and the written policy in the body (`outbound_rate_limit: 46` with a new `mp1-` version;
  the next read answers that version). `main.cf` holds 46; the master is the same process and alive; the journal has no
  reload line. On Ubuntu this sequence answered `200 success` in set1 (P1).
- **Save without a change after the owner corrected the line** (S2 g, 04:02:06 / 04:08:41; the owner did not reload):
  `200`, `applied: unchanged_reloaded`; `main.cf` byte-identical; one `postfix/master ... reload` line from the same
  master.
- **Postfix stopped** (S2 h): `200`, `applied: not_running`; 47 is in `main.cf`; `postfix status` still says `the
  Postfix mail system is not running` and the unit is inactive. A second save without a change: `200`, `applied:
  unchanged`, still stopped. The owner then starts Postfix; mail is accepted on 25 and 587 answers.
- Read on the way (H27): while `main.cf` is refused, `postfix status` itself exits 1 with the same fatal line and does
  not say whether the master runs.

### A2 (P2) PostgreSQL, the owner's reload hook that fails after it signalled the server (`steps/11-s4-postgresql/`)

The S4 g sequence of set1: `502 CONFIG_RELOAD_FAILED`, reason **`restored_unit_reload_failed`**, `vars` = `{"detail":
"Failed to reload set1-owner-pooler.service: Unit set1-owner-pooler.service not found.", "unit": "postgresql@17-main"}`
(`postgresql@16-main` on Ubuntu); **no copy is named** (`vars.name` absent) and none was left (as many kept copies as
before the save, no validation copy); the file is byte-identical to the previous one (same SHA-256, owner, group,
mode); `SHOW work_mem` = `8MB`; same postmaster PID (15729 on Debian, 21432 on Ubuntu, run-b).

PostgreSQL 17.11 (Debian 17.11-0+deb13u1) and 16.15 (Ubuntu 16.15-0ubuntu0.24.04.1). The statement batch the Agent
uses for this answer (`cmd/agent/db_config.go` `dbConfigPostgreSQLRereadVerified`), run the same way by the lab right
after the answer, with its raw output in `native-text/postgresql-agent-queries-g.txt`. Debian run-b:
`file=/etc/postgresql/17/main/postgresql.conf`, `before=1791518552.863723`, `signal=true`, `waited=1`,
`loaded=1791518552.865815`, no `error=` line; Ubuntu run-b: `file=/etc/postgresql/16/main/postgresql.conf`,
`before=1791518948.383809`, `signal=true`, `waited=1`, `loaded=1791518948.386346`, no `error=` line. So
`pg_conf_load_time()` did move on an error-free re-read on both versions (about 2 ms after the signal).

### A3 (O1, O3, O5)

- **O1.** `max_connections = plenty`: `422 CONFIG_INVALID` / `daemon`, `vars` = `{"detail": "option
  'max_connections': unsigned value 0 adjusted to 10", "name": "max_connections"}`; the file is unchanged. The installed
  `mariadbd` read on that same text: exit 0, `[Warning] ... option 'max_connections': unsigned value 0 adjusted to 10`.
  The stock file with one changed option still saves (S5 b: `200`, `restart_required`, `daemon_check: accepted`).
- **O3, cron.allow** (the owner restricts cron to root): `502 CURRENT_SETTINGS_UNREADABLE` / `scheduled_tasks` with
  `detail: cron_allow` and `vars.detail` = `The user set1_owner_test cannot use this program (crontab)`, for the list
  and for Add. **O3, relocated spool** (now measured on Debian and Ubuntu too): native `crontabs: No such file or
  directory`; the answer names **no cause** and carries that line in `vars.detail`. **O3, queue:** with the owner's
  line in `main.cf`, `502 MAIL_QUEUE_UNREADABLE` / `postfix_config` with `vars.detail` = `bad numerical configuration:
  default_process_limit = 200 # raised for the campaign`.
- **O5.** The component scan lists each file once: PostgreSQL `postgresql.conf`, `pg_hba.conf`; MariaDB
  `/etc/mysql/mariadb.cnf`, `/etc/mysql/mariadb.conf.d/50-server.cnf`.

### A4 setup

On Debian 13 and Ubuntu 24.04 the setup runs to the isolated host's DNS wait as before: `01-dns`, the services, both
mail profiles (`webmail`, `protected-mail`: filter wiring and DKIM, now verified), cron, certbot (Debian) and the
firewall step `succeeded`; `access_dns` is `running` with `server_setup_access_dns_required`; the certificate,
mail-enrollment and verify steps did not run. The same holds in the request-identity cells.

## Group B: service actions (`POST /api/v1/service/action`, as the owner)

Section S8 of the settings cells (`steps/15-s8-service-actions/`; per action the answer, the units before and after,
the daemon's own PID, the journal in `journal/`; the unit texts in `native-text/units-*.txt`). "matches": the answer
says success exactly when the action took effect natively (start: the daemon runs; stop: it does not; restart: it
runs with a new process; reload: same process, a reload is visible, and the unit reports no failed reload). The full
table with PIDs and times is `group-b-service-actions.md`.

| Service | Situation | Action | set2-debian13/run-b | set2-ubuntu/run-b |
| --- | --- | --- | --- | --- |
| nginx | already running | start | 200 success: matches | 200 success: matches |
| nginx | healthy | reload | 200 success: matches | 200 success: matches |
| nginx | healthy | restart | 200 success: matches | 200 success: matches |
| nginx | healthy | stop | 200 success: matches | 200 success: matches |
| nginx | while stopped | reload | 502 SERVICE_ACTION_FAILED/command: matches | 502 SERVICE_ACTION_FAILED/command: matches |
| nginx | healthy | start | 200 success: matches | 200 success: matches |
| mariadb | already running | start | 200 success: matches | 200 success: matches |
| mariadb | healthy | reload | 502 SERVICE_ACTION_FAILED/command: matches | 502 SERVICE_ACTION_FAILED/command: matches |
| mariadb | healthy | restart | 200 success: matches | 200 success: matches |
| mariadb | healthy | stop | 200 success: matches | 200 success: matches |
| mariadb | while stopped | reload | 502 SERVICE_ACTION_FAILED/command: matches | 502 SERVICE_ACTION_FAILED/command: matches |
| mariadb | healthy | start | 200 success: matches | 200 success: matches |
| dovecot | already running | start | 200 running: matches | 200 running: matches |
| dovecot | healthy | reload | 200 reloaded: matches | 200 reloaded: matches |
| dovecot | healthy | restart | 200 restarted: matches | 200 restarted: matches |
| dovecot | healthy | stop | 200 stopped: matches | 200 stopped: matches |
| dovecot | while stopped | reload | 502 SERVICE_ACTION_FAILED/reload: matches | 502 SERVICE_ACTION_FAILED/reload: matches |
| dovecot | healthy | start | 200 started: matches | 200 started: matches |
| postfix | already running | start | 200 running: matches | 200 running: matches |
| postfix | healthy | reload | 200 reloaded: matches | 200 reloaded: matches |
| postfix | healthy | restart | 200 restarted: matches | 200 restarted: matches |
| postfix | healthy | stop | 200 stopped: matches | 200 stopped: matches |
| postfix | while stopped | reload | 502 SERVICE_ACTION_FAILED/reload: matches | 502 SERVICE_ACTION_FAILED/reload: matches |
| postfix | healthy | start | 200 started: matches | 200 started: matches |
| postgresql | already running | start | 200 started: matches | 200 started: matches |
| postgresql | healthy | reload | 200 reloaded: matches | 200 reloaded: matches |
| postgresql | healthy | restart | 200 restarted: matches | 200 restarted: matches |
| postgresql | healthy | stop | 200 stopped: matches | 200 stopped: matches |
| postgresql | while stopped | reload | 502 SERVICE_ACTION_FAILED/command: matches | 502 SERVICE_ACTION_FAILED/command: matches |
| postgresql | healthy | start | 200 started: matches | 200 started: matches |
| postfix | main.cf holds a line Postfix refuses | reload | 502 SERVICE_ACTION_FAILED/check: matches | 502 SERVICE_ACTION_FAILED/check: matches |
| postfix | main.cf holds a line Postfix refuses | restart | 502 SERVICE_ACTION_FAILED/check: matches | 502 SERVICE_ACTION_FAILED/check: matches |
| postfix | main.cf holds a line Postfix refuses | stop | 502 SERVICE_ACTION_UNKNOWN: unknown (truth: took effect) | 502 SERVICE_ACTION_UNKNOWN: unknown (truth: took effect) |
| postfix | main.cf holds a line Postfix refuses | start | 502 SERVICE_ACTION_FAILED/check: matches | 502 SERVICE_ACTION_FAILED/check: matches |
| postfix | after the owner's correction | start | 200 started: matches | 200 started: matches |
| postgresql | the owner's reload hook fails | reload | 502 SERVICE_ACTION_FAILED/reload: matches | 502 SERVICE_ACTION_FAILED/reload: matches |
| postgresql | the owner's reload hook fails | restart | 200 restarted: matches | 200 restarted: matches |
| postgresql | after the hook was removed | reload | 200 reloaded: matches | 200 reloaded: matches |
| postgresql | postgresql.conf holds a value the server refuses | restart | 502 SERVICE_ACTION_FAILED/start: matches | 502 SERVICE_ACTION_FAILED/start: matches |
| postgresql | postgresql.conf holds a value the server refuses, after the failed restart | start | 502 SERVICE_ACTION_FAILED/start: matches | 502 SERVICE_ACTION_FAILED/start: matches |
| postgresql | after the owner's correction | start | 200 started: matches | 200 started: matches |

- set2-debian13/run-b: 41 actions, 40 answers match the service, 0 do not, 1 answered as unknown; 1 refusals `server_setup_busy` before an action was sent
- set2-ubuntu/run-b: 41 actions, 40 answers match the service, 0 do not, 1 answered as unknown; 0 refusals `server_setup_busy` before an action was sent

What the units are (read with `systemctl cat` and `systemctl show`, both platforms in `native-facts.json` under
`unit_facts`):

| Unit | Debian 13 | Ubuntu 24.04 |
| --- | --- | --- |
| `postfix.service` | **real unit**: `Type=forking`, `ExecStart=postfix debian-systemd-start`, `ExecReload=postfix reload`; `ConsistsOf=` and `PropagatesReloadTo=postfix@-.service` | **wrapper**: `Type=oneshot`, `ExecStart=/bin/true`, `ExecReload=/bin/true`; `Wants=`, `ConsistsOf=`, `PropagatesReloadTo=postfix@-.service` |
| `postfix@-.service` | loaded and **inactive** (the template exists; `PartOf=` and `ReloadPropagatedFrom=postfix.service`) | the daemon: `Type=forking`, `ExecStart=postmulti -i - -p start`, `ExecReload=postmulti -i - -p reload`; `PartOf=`, `ReloadPropagatedFrom=postfix.service` |
| `dovecot.service` | real unit: `Type=notify`, `ExecStart=/usr/sbin/dovecot -F`, `ExecReload=/usr/bin/doveadm reload` | the same |
| `postgresql.service` | wrapper: `Type=oneshot`, `ExecStart=/bin/true`, `ExecReload=/bin/true`; `Wants=`, `ConsistsOf=`, `PropagatesReloadTo=postgresql@17-main.service` | the same with `postgresql@16-main.service` |
| `postgresql@<ver>-main.service` | `Type=forking`, `ExecStart=pg_ctlcluster --skip-systemctl-redirect 17-main start`, `ExecReload=pg_ctlcluster ... reload`; `PartOf=`, `ReloadPropagatedFrom=postgresql.service`; `ReloadResult=success` after a healthy reload | the same with `16-main` |

What the table shows, on both platforms alike:

- Healthy start, reload, restart, stop and start again of nginx, MariaDB, Dovecot, Postfix and PostgreSQL: the answer
  matches the service. For the two wrapper units the answer names the instance (`unit: postgresql@17-main.service`,
  `applied: reloaded | restarted | stopped | started`); a healthy Reload of Postfix leaves `postfix/master[...]: reload`
  in the journal with the same master PID, a healthy Reload of PostgreSQL moves `pg_conf_load_time()` with the same
  postmaster.
- Reload of a stopped service is refused and starts nothing (`502 SERVICE_ACTION_FAILED`, `reload` for Postfix and
  Dovecot, `command` with systemd's own line for nginx and the PostgreSQL wrapper).
- **Postfix with a line its own check refuses**: Reload, Restart and Start answer `502 SERVICE_ACTION_FAILED` /
  `check` with `vars.detail` = Postfix's fatal line and `vars.command` = `sudo postfix check`; nothing was sent (same
  master PID, no journal line). **Stop** is sent, the master is gone (Debian: `postfix.service` `failed`,
  `Result=exit-code`; Ubuntu: `postfix@-.service` `failed`), and the answer is `502 SERVICE_ACTION_UNKNOWN`: "systemctl
  stop postfix reported success, but `postfix check` did not pass, so `postfix status` cannot say whether the master
  stopped" (O8).
- **PostgreSQL with the owner's failing reload hook**: Reload answers `502 SERVICE_ACTION_FAILED` / `reload`,
  `vars.owner_unit` = the instance, the instance's `ReloadResult=exit-code`. A failure is the right answer; its wording
  is finding P3. Restart under the same hook succeeds and is answered as success.
- **PostgreSQL with a `postgresql.conf` the server refuses at start** (not asked for, added): Restart answers `502
  SERVICE_ACTION_FAILED` / `start` with `postgresql@17-main.service is failed (failed), result protocol`; the wrapper
  is active and `systemctl` had exited 0. Start then answers the same. After the owner's correction Start brings it
  back (`200`, `started`).
- MariaDB has no reload: `502 SERVICE_ACTION_FAILED` / `command`, `Failed to reload mariadb.service: Job type reload is
  not applicable for unit mariadb.service` (O12).

## Group C: request identity (D-029)

For each guarded route: (i) the same request (same identity, same body) three times in a row, (ii) three times at
once on three connections, (iii) without the header, (iv) the same identity with a different body, and the native
state after each. (v) and (vi) for the long routes; (vii) the rows. Generated from the cells
(`group-c-request-identity.md`).

**rid-debian13/run-a** (overall `failed`; sections C0 passed, C1 passed, C2 passed, C3 passed, C4 passed, C5 passed, C6 passed, C7 failed, C8 passed, C9 passed)

| Route (POST) | i | ii | iii | iv | v | vi | vii | Times (UTC) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `domains/{id}/databases` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:16:11-04:16:15 |
| `domains/{id}/databases` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:16:15-04:16:18 |
| `database-servers/{id}/admin-account` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:16:19-04:16:22 |
| `database-servers/{id}/admin-account` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:16:22-04:16:25 |
| `database-servers/{id}/databases` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:16:25-04:16:28 |
| `database-servers/{id}/databases` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:16:29-04:16:32 |
| `domains/{id}/backups` (manual backup) | pass | pass | pass | pass | - | - | - | 04:16:32-04:16:50 |
| `domains/{id}/backups/restore` | pass | pass | pass | pass | pass | pass | - | 04:17:58-04:20:01 |
| `domains/{id}/ssl/letsencrypt` (guard only) | pass | pass | pass | pass | - | - | - | 04:16:53-04:16:59 |
| `vpn/peers` | pass | pass | pass | pass | - | - | - | 04:17:10-04:17:13 |
| `import/cpanel/apply` | pass | pass | pass | pass | pass | - | - | 04:17:17-04:17:28 |
| `request_identities` rows | - | - | - | - | - | - | pass | 04:20:01-04:20:01 |

**rid-ubuntu/run-b** (overall `failed`; sections C0 passed, C1 passed, C2 passed, C3 passed, C4 passed, C5 passed, C6 passed, C7 failed, C8 passed, C9 passed)

| Route (POST) | i | ii | iii | iv | v | vi | vii | Times (UTC) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `domains/{id}/databases` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:23:05-04:23:09 |
| `domains/{id}/databases` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:23:09-04:23:13 |
| `database-servers/{id}/admin-account` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:23:14-04:23:17 |
| `database-servers/{id}/admin-account` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:23:17-04:23:20 |
| `database-servers/{id}/databases` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:23:21-04:23:24 |
| `database-servers/{id}/databases` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:23:24-04:23:28 |
| `domains/{id}/backups` (manual backup) | pass | pass | pass | pass | - | - | - | 04:23:28-04:23:45 |
| `domains/{id}/backups/restore` | pass | pass | pass | pass | pass | pass | - | 04:30:03-04:32:10 |
| `domains/{id}/ssl/letsencrypt` (guard only) | pass | pass | pass | pass | - | - | - | 04:23:49-04:23:56 |
| `vpn/peers` | pass | pass | pass | pass | - | - | - | 04:24:13-04:24:17 |
| `import/cpanel/apply` | pass | pass | pass | pass | pass | - | - | 04:29:20-04:29:32 |
| `request_identities` rows | - | - | - | - | - | - | pass | 04:32:10-04:32:11 |

**rid-arch/run-b** (overall `failed`; sections C0 passed, C1 passed, C2 passed, C3 passed, C4 passed, C5 failed, C6 passed, C7 failed, C8 passed, C9 passed)

| Route (POST) | i | ii | iii | iv | v | vi | vii | Times (UTC) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `domains/{id}/databases` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:32:09-04:32:13 |
| `domains/{id}/databases` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:32:13-04:32:18 |
| `database-servers/{id}/admin-account` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:32:19-04:32:23 |
| `database-servers/{id}/admin-account` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:32:23-04:32:26 |
| `database-servers/{id}/databases` (MariaDB) | pass | pass | pass | pass | - | - | - | 04:32:26-04:32:30 |
| `database-servers/{id}/databases` (PostgreSQL) | pass | pass | pass | pass | - | - | - | 04:32:31-04:32:35 |
| `domains/{id}/backups` (manual backup) | pass | pass | pass | pass | - | - | - | 04:32:35-04:32:52 |
| `domains/{id}/backups/restore` | pass | pass | pass | pass | pass | pass | - | 04:33:40-04:35:34 |
| `domains/{id}/ssl/letsencrypt` (guard only) | FAIL | FAIL | pass | pass | - | - | - | 04:32:56-04:33:03 |
| `vpn/peers` | pass | pass | pass | pass | - | - | - | 04:33:15-04:33:19 |
| `import/cpanel/apply` | FAIL | FAIL | pass | pass | FAIL | - | - | 04:33:23-04:33:34 |
| `request_identities` rows | - | - | - | - | - | - | pass | 04:35:34-04:35:34 |

Reading the Arch table: its Let's Encrypt i and ii are the harness defect H39 (the guard held: one Panel log line and
four nginx reloads per identity, as for one entry); its import i, ii and v are finding P5 (the import answers `500`
on Arch before anything is imported; that `500` was replayed byte for byte and nothing was imported on any arrival);
VPN, databases, engine accounts, backup, restore and the rows pass there as on the other two.

The native effect read for each route, the same on every platform that ran it (`steps/NN-c*/section.json`, `native/`):

| Route | (i) and (ii): one native effect | Answers |
| --- | --- | --- |
| `domains/{id}/databases` | one new database on the engine (`SHOW DATABASES`, `pg_database`) and one new Panel row on that server, per identity | the same bytes three times; replays carry `X-CelikPanel-Request-Replayed: 1` |
| `database-servers/{id}/admin-account` | one more `database.admin_account.provision:<engine>` audit entry and one new stored password per identity; **after the three concurrent arrivals a login to the engine with the password the Panel stores succeeds** (`CURRENT_USER()` = `celikpanel_admin@localhost`; `current_user` = `celikpanel_admin`) | the handler's `200` once, then `409 REQUEST_COMPLETED_RESULT_NOT_RETAINED` |
| `database-servers/{id}/databases` | one new database on the engine and one new row per identity | MariaDB (new user, password minted): `200` once, then the status-only `409`; PostgreSQL (existing user): the same bytes three times |
| `domains/{id}/backups` | one new archive per identity, its manifest `origin: manual`, `type: full`, `job_key: request:<identity>`; every archive's manifest readable, no partial file | the same bytes; the concurrent arrivals all answer the one archive after about 8-10 s |
| `domains/{id}/backups/restore` | the site (document root digest, 192 MB; 20,000 rows) is the backup's again and one `pre_restore` archive was written. After the first answer the owner changed the site again: arrivals 2 and 3 answered the stored bytes and **restored nothing** | the same bytes; of two **different** identities at once one restored and the other got `409 BACKUP_RESTORE_IN_PROGRESS` from the Agent's lock within 40 ms |
| `domains/{id}/ssl/letsencrypt` | what one entry into the handler leaves (one Panel log line for the Agent's answer, four nginx reload lines for the validation virtual host) was left once per identity; no certificate row | `500 INTERNAL` three times, the same bytes (O9) |
| `vpn/peers` | one new peer in `wg show wg0 peers` and one new active row per identity; the page's acknowledgement of the one delivered configuration answered `200` | `200` once, then the status-only `409` |
| `import/cpanel/apply` | per identity one domain (`active`), its two site files, one mailbox, one forwarder, one database with 300 rows | the same bytes |

(iii) Without the header every route answered `428 REQUEST_ID_REQUIRED` in 3-4 ms and nothing changed. (iv) The same
identity with a different body answered `409 REQUEST_ID_REUSED` and nothing changed.

### (v) the connection dropped mid-flight (restore and import)

The driver sent the request, waited until the server was observably at work (the Agent writing in the backup
directory; the Panel's database naming the imported domain), checked that no answer byte had arrived and reset the
connection. Restore (dropped 0.57 s after sending on Debian 13, 0.58 s on Ubuntu, 0.62 s on Arch): the row was
`running` at the drop, `done` with `200` and a kept answer afterwards; the site is the backup's and one `pre_restore`
archive was written; the same identity again answered `200` with the stored bytes (`Replayed: 1`, SHA-256 equal to
the row's) and restored nothing. Import (Debian 13 and Ubuntu; dropped after 3.7 s and 4.1 s, during a dump that ends
with `DO SLEEP(25)`): row `done`, `200`; the whole site is there (domain `active`, three files, mailbox, 300 rows);
the replay answered the stored bytes and imported nothing again. On Arch the import had already answered `500` (P5)
when the driver dropped the connection, so (v) is not measured for the import there.

### (vi) the Panel goes away while a restore runs: **the important observation**

Measured twice per platform (`steps/NN-c8-restore/section.json` `panel_goes_away`, `native-text/journal-panel-*.txt`).

- **`systemctl restart celikpanel-panel`** (what an owner or an update does), issued while the row was `running` and
  the Agent was writing: **the restore was not interrupted.** The Panel stops gracefully (it waits for running
  requests up to 25 s, `cmd/panel/server_lifecycle.go:17`; the unit's `TimeoutStopSec=30s`): `systemctl restart` itself
  took 10.2 s (Debian 13), 9.6 s (Ubuntu), 10.1 s (Arch) while the request finished, the client received its `200`
  after 10.6 s, 10.0 s, 10.6 s, the row is `done` with the answer kept, the site is the backup's, one `pre_restore`
  archive. The replay answered the stored bytes. The row `interrupted` therefore does **not** arise from a restart as
  long as the request finishes within the stop limit.
- **The Panel's process killed** (`SIGKILL` to the main process: a crash, the kernel's out-of-memory killer; a lab
  fault, recorded as such) while the row was `running`: the client lost its connection after 0.7-0.9 s with no
  answer; systemd restarted the unit (`Restart=on-failure`, `RestartSec=3`); **the row is `interrupted`** and the same
  identity answers **`409 REQUEST_OUTCOME_UNKNOWN`**, starting nothing. **What the Agent left, on all three
  platforms: the restore finished completely.** The Agent kept its process (same MainPID), the document root's digest
  and the row count are the backup's, exactly one `pre_restore` archive was written, no partial file, no work
  directory, nothing but `public_html` in the site's home. So after a killed Panel the answer "it is not known whether
  the change was completed" stands beside a restore that is complete on the host; nothing reconciles the two, as
  D-029 says.
- After either, the Panel answered every management request **`503 PANEL_STARTING`** for 12-14 s on Debian 13 and
  Ubuntu (1 s on Arch) before the first `200` (`cmd/panel/recovery_access.go:127`); the owner's session stayed valid.

### (vii) the rows

Read-only at the end of each cell (`steps/NN-c9-identities/`; stored answers are never copied out: per row its length,
digest and JSON key names, and whether any of the run's secrets occurs in it). On each of the three platforms: 31
rows, one per identity the driver used and no other; 30 `done`, 1 `interrupted` (the killed restore); none `running`;
every row lives 86,400 s. The engine-account and VPN rows keep the status only (no stored body); the stored bodies
were searched for the 21 secrets the run was shown or sent (VPN private and preshared keys, the engine passwords read
back, the minted and the sent database passwords): **no row holds one**, and no stored body contains `PrivateKey`,
`PresharedKey`, `[Interface]`, `client_config`, `delivery_token` or a `password` field. Per route (Debian 13):
admin-account 4 rows, all status only; vpn/peers 2, status only; database-servers databases 5 (2 kept, 3 status
only); domain databases 4, backups 2, Let's Encrypt 3, import 4, all kept; restore 7 (6 kept, 1 interrupted).

## Verbatim texts (EN / TR)

`texts-en-tr.md` holds every distinct answer of all cells with its status, code, reason, the API's sentence, the
values it carried and the catalogue sentences the screens of this build use (EN and TR; the driver renders no
screen). The ones the findings refer to:

- `502 SERVICE_ACTION_FAILED` / `reload`, API: "The service was not reloaded and keeps running with the settings it
  had. The server owner runs the command shown to read the service's own answer, corrects what it names, and then
  repeats this action here; nothing repeats it automatically." Screen (`err.SERVICE_ACTION_FAILED.reload`), EN:
  "{unit} was not reloaded and keeps running with the settings it had. On the server, run {command} to see why,
  correct it, then repeat the action here." TR: "{unit} yeniden yüklenmedi ve önceki ayarlarıyla çalışmayı sürdürüyor.
  Sunucuda {command} komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi burada yineleyin." Values measured:
  `{"detail": "the reload of postgresql@17-main.service failed (exit-code); it keeps running with its previous
  settings", "command": "sudo systemctl status postgresql@17-main.service", "owner_unit": "postgresql@17-main.service"}`;
  `{"detail": "Postfix is not running; nothing was reloaded", "command": "sudo postfix reload"}`; `{"detail": "Dovecot
  is not running; nothing was reloaded", "command": "sudo systemctl status dovecot"}`.
- `502 SERVICE_ACTION_UNKNOWN`, API: "The action was sent, but what came of it could not be verified, so it is not
  reported as done. This is not a verified failure: the service may already be in the state that was asked for. The
  server owner runs the command shown to see the service's state, and repeats this action here only if it is still
  needed; nothing repeats it automatically." (`vars.command`: `sudo postfix status`.)
- `502 MAIL_POLICY_NOT_RELOADED` / `check`, screen (`err.MAIL_POLICY_NOT_RELOADED.check`), EN: "Saved to
  /etc/postfix/main.cf, but Postfix was not reloaded: its own check refuses the configuration. A running Postfix keeps
  the settings it had before, so the saved values are not in effect. The line it names is below and may be one this
  page did not write. Nothing was rolled back. On the server, correct that line, run sudo postfix check until it prints
  no error, then run sudo postfix reload. The values shown below are the saved ones." TR: "/etc/postfix/main.cf
  dosyasına kaydedildi ancak Postfix yeniden yüklenmedi: Postfix’in kendi denetimi yapılandırmayı reddediyor. Çalışan
  bir Postfix önceki ayarlarını korur; bu yüzden kaydedilen değerler yürürlükte değil. Adını verdiği satır aşağıda; bu
  sayfanın yazmadığı bir satır olabilir. Hiçbir şey geri alınmadı. Sunucuda o satırı düzeltin, hata yazmayana kadar
  sudo postfix check komutunu çalıştırın, sonra sudo postfix reload komutunu çalıştırın. Aşağıda gösterilen değerler
  kaydedilen değerlerdir."
- `502 CONFIG_RELOAD_FAILED` / `restored_unit_reload_failed`, screen, EN: "The change was not kept, and the previous
  file is back in place. The systemd unit of PostgreSQL could not reload, with the new file and again with the
  previous one, so CelikPanel asked PostgreSQL directly: it read the previous file again and is running with the
  settings it had before your change. The reload failed with the previous file too, so the cause is not only this
  change. On the server, run sudo systemctl reload postgresql@17-main to see why, correct it, then save the change here
  again." TR: "Değişiklik tutulmadı ve önceki dosya yerine kondu. PostgreSQL hizmetinin systemd birimi yeni dosyayla da
  önceki dosyayla da yeniden yüklenemedi; bu yüzden CelikPanel doğrudan PostgreSQL hizmetine sordu: önceki dosyayı
  yeniden okudu ve değişikliğinizden önceki ayarlarla çalışıyor. Yeniden yükleme önceki dosyayla da başarısız olduğu
  için neden yalnız bu değişiklik değildir. Nedenini görmek için sunucuda sudo systemctl reload postgresql@17-main
  komutunu çalıştırın, düzeltin, sonra değişikliği buradan yeniden kaydedin."
- `428 REQUEST_ID_REQUIRED`, API: "This page was opened before CelikPanel was updated, so the server did not accept
  the change and nothing was changed. Reload the page, then make the change again. (A client that is not the
  CelikPanel page sends the header X-CelikPanel-Request-Id: 32 lowercase hexadecimal characters, a new value for each
  action.)"
- `409 REQUEST_ID_REUSED`, API: "This change was sent with an identifier the server already used for a different
  change, so it was not carried out. Reload the page, then make the change again."
- `409 REQUEST_COMPLETED_RESULT_NOT_RETAINED`, API: "This change was already made; it was not made a second time. Its
  result was shown only once and is not kept. Reload the page to see the current state; if you still need what was
  shown once (a password or a configuration file), create a new one."
- `409 REQUEST_OUTCOME_UNKNOWN`, API: "CelikPanel restarted or failed while this change was running, so it is not
  known whether the change was completed. It will not be run again by itself. Reload the page and check the current
  state; make the change again only if it is missing."
- `409 BACKUP_RESTORE_IN_PROGRESS`, API: "Another restore of this domain is still running, so this one was not started
  and changed nothing. Wait for it to finish and check the site; restore again only if it is still needed."
- `503 PANEL_STARTING`, API: "Panel management is still starting. Read-only connection and recovery status remain
  available."

The Turkish catalogue sentences of the request-identity refusals and of every other answer are in `texts-en-tr.md`.

## Findings

### Candidate product defects

- **P3. Services page, Reload: a failed reload is answered "keeps running with the settings it had", which was not
  verified and is not the native state in two measured cases** (Debian 13 and Ubuntu 24.04).
  (a) *PostgreSQL re-read its files.* Sequence: the owner's drop-in for `postgresql@<ver>-main.service` whose
  `ExecReload` signals the server (`kill -HUP $MAINPID`) and then fails; `POST /api/v1/service/action` `{"name":
  "postgresql", "action": "reload"}`. Answer: `502 SERVICE_ACTION_FAILED` / `reload`, `vars.detail` = "the reload of
  postgresql@17-main.service failed (exit-code); it keeps running with its previous settings", sentence "The service
  was not reloaded and keeps running with the settings it had." Native: `pg_conf_load_time()` moved, same postmaster
  PID, the instance's `ReloadResult=exit-code`. The server did re-read its files; set1 (P2) had measured the same for
  the configuration save, which now asks the server before it says so. `cmd/agent/service_action_verify.go:414`
  (the detail), `cmd/panel/service_action_outcome.go:43` (the sentence), `web/src/i18n/en.ts:449`, `tr.ts:418`.
  (b) *The service is not running.* Reload of a stopped Postfix or Dovecot: the same sentence ("... and keeps running
  with the settings it had") with `vars.detail` = "Postfix is not running; nothing was reloaded" / "Dovecot is not
  running; nothing was reloaded"; the daemon is stopped before and after. `cmd/agent/service_action_verify.go:137-143`
  (stage `reload` for a stopped daemon), `cmd/panel/service_action_outcome.go:43`.
  Evidence: `set2-*/run-b/steps/15-s8-service-actions/section.json` (`actions`, `reload_hook`; check "postgresql
  (reload hook): the answer does not say ..."), `journal/postgresql-reload-the-owner-s-reload-hook-fails.txt`.
- **P4. cPanel import: the site files of an archive that holds the directory member `homedir/public_html/` are
  refused ("unsafe cpmove member path"), and the import ends half done** (Debian 13, Ubuntu 24.04).
  `cmd/agent/cpmove_extract_linux.go:44` compares the member's name with `homedir/public_html` exactly; a directory
  member is stored with a trailing slash, falls through to the payload rule, becomes an empty path, and
  `:53` refuses it (`:353`). Sequence: a `cpmove-<user>.tar.gz` under `/var/lib/celikpanel-imports` whose tar holds
  directory members the way tar (and Python's `tarfile`) writes an archive of a directory; `POST
  /api/v1/import/cpanel/inspect` previews it without complaint; `POST /api/v1/import/cpanel/apply` with files, mail
  and databases. Answer: `202`, `status: pending`, steps `domain` ok, **`files` not ok: `unsafe cpmove member path`**,
  `mail` ok, `forwarders` ok, `dns` ok, `database:<name>` ok. Native: the domain exists with status `pending`, the
  mailbox, the forwarder and the database with its 300 rows are imported, the document root holds one file (not the
  archive's two). The same archive without that one directory member imports completely (`200`, `active`). Limit: no
  real cPanel archive was available; the archive was built by the lab from the import parser's rules. Evidence:
  `rid-debian13/run-a` and `rid-ubuntu/run-b` `steps/15-c7-import/section.json` (`archive_as_tar_writes_it`,
  `fixtures`), and rid-ubuntu run-a, where all three archives held the member.
- **P5. Arch: a PHP site cannot be created, so every cPanel import answers `500 INTERNAL` "internal server error".**
  `internal/services/php_pool_manager.go:17-25` builds the pool file's path as `/etc/php/<version>/fpm/pool.d/`
  (the Debian layout) on every platform. On the Arch guest (PHP 8.5) the Agent logs `CreateSite
  set2-import-seq.test: PHP-FPM pool creation: create PHP pool site3: publish managed configuration: create atomic
  managed configuration /etc/php/8.5/fpm/pool.d/site3.conf: open /etc/php/8.5/fpm/pool.d/.site3.conf.celikpanel-...:
  no such file or directory`, then `previous configuration restored but rollback activation failed: exit status 5`,
  and the Panel `[500] site creation failed: site provisioning failed during PHP-FPM pool creation`. Sequence: setup
  with purpose `web` (nginx, php-fpm, mariadb, postgresql: all steps succeeded), then `POST
  /api/v1/import/cpanel/apply` (the import always creates a PHP site). Native: the site account is created and
  removed again (`useradd`, `userdel` in the journal), no domain row, no database, nothing imported; the answer names
  neither the step nor a next action. The static site of the same cell was created without error. Evidence:
  `rid-arch/run-b/steps/15-c7-import/section.json`, `steps/18-collect/journal-product.txt` (04:33:22Z, 04:33:28Z).
  Whether a PHP site created from the Domains page fails the same way was not tried (it takes the same step).

### Observations (not judged as defects)

- **O6. `systemctl restart celikpanel-panel` does not interrupt a running guarded request** that finishes within the
  Panel's stop limit (25 s); the client gets its answer and the row is `done`. See (vi). The expectation "restart →
  row `interrupted`" holds for a Panel that dies, and presumably for a request that outlasts the stop limit (not
  measured: the restore here takes about 10-14 s).
- **O7. After the Panel is killed, the Agent finishes the restore alone and completely**, while the row says
  `interrupted` and the replay says the outcome is not known. See (vi).
- **O8. Stop of Postfix while `main.cf` is refused is answered `502 SERVICE_ACTION_UNKNOWN` although the master is
  gone**, and the unit is left `failed`. `postfix status` cannot answer in that state
  (`cmd/agent/mail_service_verify.go:384-394`); the master's PID file could.
- **O9. A failed issuance answers `500 INTERNAL` "internal server error"** with no cause and no next step; the cause
  is only in the Panel's log (`[500][agent] certbot failed: exit status 1 ...`). `cmd/panel/domain_ssl_handlers.go:602`
  → `cmd/panel/httperr.go:568-577`. Here the cause is the lab's isolation (certbot met this guest's own listener and
  refused its certificate); the path is the one every certbot failure takes.
- **O10. `database-servers/{id}/databases` returns the new user's password also when the owner sent it**
  (`cmd/panel/database_v2_handlers.go:603`, `:763-769`), so that answer is not kept either and its replay is the
  status-only refusal. D-029 says "a password minted by that request".
- **O11. A Services action was refused `409 server_setup_busy` ("Server setup is in progress. Follow its current
  operation.") while the setup was only waiting for DNS**: once on Ubuntu (run-a, where it left Postfix stopped until
  H31) and once on Debian (run-b, repeated after 6 s). `cmd/panel/service_operations.go:265-270` asks whether a setup
  execution is `running` (`cmd/panel/server_setup_operations.go:98-102`), which the waiting execution is for moments
  at a time. On the isolated guest the setup never finishes; on a real server it waits like this until DNS resolves.
- **O12. Reload is offered for MariaDB, whose unit has none** (`Job type reload is not applicable`): the answer is a
  correct `502` / `command`.
- **O13. Debian 13 also carries `postfix@-.service`** (loaded, inactive) beside the real `postfix.service`.
- **O14. The Databases page's version of MariaDB is not the server's on two platforms.** `GET
  /api/v1/database-servers`: Debian 13 `11.8.6-MariaDB-0+deb13u1 from Debian`; Ubuntu 24.04 `15.1` (the server is
  10.11.14); Arch the literal `VERSION()`. PostgreSQL's is right on all three (`17.11 ...`, `16.15 ...`, `18.6`).
  Recorded by the engine auto-registration (`cmd/panel/database_autodiscover.go:25`); where the value is read was not
  traced. Evidence: `rid-*/run-*/steps/08-c0-prepare/section.json` (`database_servers`).
- **O15. The import preview answers each mailbox's password hash to the browser.** `POST
  /api/v1/import/cpanel/inspect` returns `mail_accounts[].crypt_hash` as read from the archive
  (`cmd/panel/import_handlers.go:65`, `internal/transport/cpmove_contracts.go:6`). In this run the value is the lab
  fixture's fabricated one (the hash of no password); with a real archive it is the mailbox's real hash. Found by the
  secret scan (`rid-*/run-*/steps/15-c7-import/api/*-import-cpanel-inspect.json`).
- O2 of set1 (the owner's one-restriction-per-line layout becomes one line) is unchanged, as the contract says.

### Platform limitations

- The isolated lab has no certificate authority: the Let's Encrypt route was measured for its guard only.
- Arch (systemd-resolved) answers from `/etc/hosts` only a moment after the file changes (H38).
- Debian/Ubuntu `cron` refuses `crontab -u <user>` even for root under `/etc/cron.allow` (as in set1).
- Mail is not supported on Arch.

### Harness

H27-H39 above. The checks that did not pass in the earlier runs (set2 run-a, rid-ubuntu run-a) are those defects, not
product behaviour, except where the table says so (P4 through H34; O11 through H31). In the latest runs the checks
that did not pass are: P3's check and the `unknown` answer of O8 (S8, both platforms); P4's check (C7, Debian 13 and
Ubuntu); on Arch P5's checks (C7) and H39's two (C5). `checks-not-passed.txt` lists every check that did not pass in
every run, with its detail.

## What this run does NOT prove

- Nothing about an installed server, a published release, a production licence or the update path.
- The browser: the driver sends what the screens send; no screen was rendered. The page's own second asking after a
  lost answer, and Chrome's own repeats, were not exercised: the driver repeated and dropped requests itself. Turkish
  sentences are catalogue lookups.
- One run per platform and per sequence (re-runs only for harness defects); three arrivals at once, not more; one
  actor (the same identity from another user was not sent).
- Let's Encrypt: no issuance, no reissue of an existing certificate, no duplicate-certificate allowance; only that one
  identity entered the handler once and that its failure answer was replayed.
- (vi): a restore that outlasts the Panel's 25 s stop limit, a Panel restart during an import, a backup or a database
  creation, a host reboot, and an Agent restart mid-restore were not run. The Agent finished the one restore that was
  cut; that it always does is not shown.
- (v): the connection was reset by the client behind an SSH forward; the Panel saw a closed connection, not
  necessarily a TCP reset.
- The 24-hour expiry and the hourly sweep were not waited for; `REQUEST_IN_PROGRESS` was never answered (no waiter
  waited 20 s).
- The cPanel archive is the lab's minimal one, not a real cPanel backup (mailbox contents, DNS zone import and
  database users are outside it); P4's "as tar writes it" rests on Python's `tarfile`.
- Group B: `dovecot` and `nginx` with a refused configuration, `wg-quick@`, a second PostgreSQL cluster, and the mail
  certificate renewal of the same contract entry (handshake check, enrolled helper) were not run. S8 runs after the
  settings sections on the same guest. Groups A and B were not run on Arch.
- Arch, group C: the import beyond its failure (P5) and the Let's Encrypt comparison (H39) are not established there.
- Group A: as in set1, no scheduled backup ran, MariaDB was never restarted, `CONFIG_RELOAD_FAILED` / `restored` and
  `restored_running_unknown`, `MAIL_POLICY_RELOAD_UNKNOWN` and the reasons `reload` and `verify` were not reached.
- Setup stopped at the isolated host's DNS wait: no panel or mail certificate, no mail enrollment.

## Removals, leftovers, secrets

- Removed, after each cell's evidence was staged and its checksums verified on the staged copy
  (`host/removals.txt`): the overlay disks of this run's labs. Nothing else, and nothing that existed before the run.
- Left on the WSL host (`host/host-leftovers.txt`): `/var/tmp/cp-set2-run` (run copies, a mutable development copy
  `harness-dev`, logs), this run's labs without their overlays (base image copies, keys, evidence, the two `rid-debug-*`
  folders of the debugging session), the build clone `/var/tmp/cp-upd1-build/20261009t030455z`, the dist directories of
  the five fixture commits under `/var/tmp/cp-pair-accept/dist/`. No QEMU and no job process was running at the end;
  HEAD and the local git configuration are unchanged.
- `secret-scan.txt`: no private-key block, no line of the 63 lab key files, no licence key; the twelve 32-byte base64
  values in the evidence are WireGuard **public** keys that the evidence itself names as such (peers are identified by
  them); no WireGuard private key, preshared key or client configuration (the three words occur only in this README
  and in the harness's own source, as the words that were searched for); every secret-named JSON field holds
  `[REDACTED]` (owner, mailbox, site-account, database and engine passwords, delivery tokens, the session cookie); no
  password assignment and no SQL password literal. The 30 crypt-shaped values are the lab fixture's fabricated mailbox
  hash, answered back by the import preview (O15); it is the hash of no password.
- Root `SHA256SUMS` covers every file; the longest repository-relative path is under 240 characters.

## Post-collection redaction (2026-10-11)

After collection and before retention, the intake rule "no hash-shaped credential value in retained evidence" was
applied to one field. What was redacted: the 30 values of `mail_accounts[].crypt_hash` that the cPanel import preview
(`POST /api/v1/import/cpanel/inspect`, finding O15) answered back, in 19 files under
`rid-*/run-*/steps/15-c7-import/` (`section.json` in each of the four runs, and the 15 `api/*-post-api-v1-import-cpanel-inspect.json`
exchanges). The values were fabricated by the lab's import fixture (a digest of public strings, the hash of no password);
each was replaced by the literal `[REDACTED fixture crypt_hash]`. Why: the retained evidence must not contain any
hash-shaped credential value, fabricated or not. Nothing else was changed: the same bytes, line endings and JSON layout
remain, every touched file still parses as JSON, and only the lines of those files in the four nested `SHA256SUMS` and in
the root `SHA256SUMS` were recomputed. The list of touched files with their SHA-256 before and after is
`host/post-collection-redaction.txt`. The harness diff (`harness-run-copy/overlay-*/harness.diff`) still shows how the
fixture builds the prefix; that is source code, not a value. The results recorded elsewhere in this README (that the preview
answers the hash back) are unchanged; only the retained copy of the value is gone.

## Corrections after intake (2026-10-10)

After this directory was published (2026-10-10), the operator's machine name was replaced by `<operator-host>` in the SSH public-key comments (and in the text that named it) of this directory: 14 occurrences in 9 files; the key material itself, a lab public key, is unchanged.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.
