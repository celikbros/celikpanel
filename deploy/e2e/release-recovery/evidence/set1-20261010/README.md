# set1 native run, 2026-10-08 (UTC): the Panel's writes of the owner's native configuration (`c4cf7fd9`)

First native run of the new harness cell kind `settings-writes` (`settings_writes_trial.py`). This week the Panel and
Agent gained behaviour that writes the server owner's own configuration (the three settings forms of `dcc16e84`, the
configuration editors, catch-all, mail queue and cron fixes of `545b2633`), verified by component tests and by two
validators run against unpacked binaries only. This run measures it with the real packaged services on disposable
QEMU/KVM guests of the local `archlinux` WSL host: Debian 13, Ubuntu 24.04 and Arch.

Everything was built from commit `c4cf7fd9` (run copies are `git archive c4cf7fd9` plus the harness files listed
below; the working tree was never used). No installed server (Boston, Frankfurt, any) was touched or contacted.
Nothing was committed, pushed, published or signed with a production key. `celikpanel.net` resolved only to each
guest's own loopback fixture; no licence service was contacted (acceptance-fixture licence). No update was started.
Every `result.json` carries `native_evidence: false`; the owner judges it. **It closes no P0 row.**

**Result in one sentence:** on Debian 13, Ubuntu 24.04 and Arch the new protections held with the real services
wherever a write was refused before anything changed (unknown is not empty, stale and version-less writes, duplicate
task, refused values, lockout, empty content), every accepted write changed only its own line and kept owner, group
and mode, and the owner's hand edits were detected and preserved; **two candidate product defects** were measured
(Ubuntu 24.04: a mail policy save answers "saved" although Postfix could not reload; all three platforms: after a
reload that fails twice the answer says the previous `postgresql.conf` could not be put back although it was), plus
the observations and limits listed under Findings.

## What was built from what

One build from an overlay run copy (`go1.26.5 linux/amd64`), every archive `license_mode: acceptance-fixture`
(`build/cur/`: artifact document, dist JSONs, fixture commits and patches, build logs).

| Build | Role | Label / seq | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `run-upd1.sh build c4cf7fd9` (21:00:46-21:05:11Z) | Baseline B = **the installed candidate** | v0.1.0-alpha.81 / 81 | 645b3046f1dec21f5a610ebca287a3139c41434f | 90122a620bcd86d4250e993980a074a11b2186ea | 6ea60ca5da594aa0507ee9d258a18b4af6ee82195e3017ff6534e724c518b043 |
| | Good G (sealed into the fixture origin, never installed) | v0.1.0-alpha.82 / 82 | c3d4dd37696ca6145a8a624279f5c3b7f7f2a8a2 | 82debbe4a93982087e73ebf663268230b1aee9ab | a8717f8b58fb10a5dd8a04541817a0c50280c00bc55812b6067b24a108183e39 |
| | D, S, R (built by the script, not used) | v0.1.0-alpha.82 / 82 | see `build/cur/upd1-artifacts.json` | | |

- B's tree `90122a62…` **is the tree of `c4cf7fd9`** (`build/cur/baseline-tree-equals-source.txt`): the source already
  carries the `v0.1.0-alpha.81 / 81` release policy, so the baseline fixture commit is an empty one (harness defect H23
  below). The guests ran exactly the current source, with the acceptance licence seam switched on at build time.
- `prove` exited 0 (`build/cur-prove.json`); three dry runs exited 0 with `native_evidence: false` and created no lab
  (`build/set1-dry-*.json`).
- Offline suites, all OK (`build/offline-*`): pristine `c4cf7fd9` copy (`p`) and the three overlay copies (`set1`,
  `set1b`, `set1c`): `test_owner_update_trial` 189, `test_recovery_candidate_archive` 11, `test_lab` 15,
  `test_current_worker_baseline` 7, `test_worker_fixture_origin` 15, `test_bound_worker_reboot` 11,
  `test_guest_bound_worker` 8, `test_guest_probe` 13; the new `test_settings_writes_trial` 21 / 23 / 23.

## The cell kind, the run copies and the harness changes

`settings-writes` (new files only, apart from one additive line in the build script):

| File (`deploy/e2e/release-recovery/`) | What |
| --- | --- |
| `settings_writes_trial.py` (new) | The driver: reuses the upd1 steps preflight, origin, baseline-install, owner-login, licence, setup; then site and S1-S7. It logs in as the owner and calls only the Panel's HTTP API. Cells `set1-debian13`, `set1-ubuntu`, `set1-arch`. |
| `guest_settings_native.py` (new) | Guest helper behind the lab's identity guard: `read-*` modes are read-only inspections; `owner-*` modes are what an owner does by hand on the server (edit a file in place, `crontab -u`, `systemctl reload/stop/start`, `/etc/cron.allow`, a relocated cron spool, a unit drop-in), limited to fixed path and unit lists. |
| `run-set1.sh` (new) | Wrapper: `dry-run` and `cell` (new lab, one cell, lab stopped). |
| `test_settings_writes_trial.py` (new) | 23 offline tests of the pure rules. |
| `build-upd1-artifacts.sh` (1 line + comment) | H23: `git commit --allow-empty` for the fixture commits. |

Setup profile: Debian/Ubuntu purpose `web_mail` with the customization `nginx, php-fpm, mariadb, postfix, dovecot,
roundcube, rspamd, postgresql`; Arch purpose `web` with `nginx, php-fpm, mariadb, postgresql` (mail is not supported
on Arch). DNS mode external. As in every upd1 run, setup settles at the isolated host's `access_dns` wait: all service,
mail-profile and firewall steps succeeded; the certificate, mail-enrollment and verify steps did not run.

| Id | Defect | Kind | Handling | Runs |
| --- | --- | --- | --- | --- |
| H23 | `build-upd1-artifacts.sh` committed the baseline fixture with `git commit -am`; since the v0.1.0-alpha.81 release the source's own release policy equals the baseline label, the edit changes nothing and the commit failed (`build/cur/build-first-attempt.*`, 20:56:28-20:56:45Z). | harness (build) | `--allow-empty`; copy `harness-h23`, overlay `harness-run-copy/overlay-h23/`. | the build |
| H24 | The second cron fault of the first copy (spool moved aside) is not stable on cronie: its `crontab` recreates a missing spool directory itself. Found by reading cronie's behaviour before the Arch cell; never ran. | harness (fault choice) | Second fault is a relocated spool behind a dangling symlink, and a fault is judged only when the native read fails before and after the Panel's calls. | from `set1b` |
| H25 | (1) The "unusable value" sent to MariaDB was `plenty`, which MariaDB reads as 0 with the size suffix P and adjusts, so it is not unusable; the two following "unchanged" comparisons used the wrong pre-image. (2) The catalogue lookup attached every sentence of a screen to every answer. | harness (test value, measurement) | Unusable value `unlimited`; every comparison uses the file as it was just before that request; catalogue keys chosen per answer. | Debian run-a affected (its S5 c/d "failed" are this defect); fixed from `set1b` |
| H26 | Run-a could not say what MariaDB itself makes of `plenty`. | harness (missing observation) | New sub-step: the Panel is sent `plenty`, the installed `mariadbd` reads the file as written (read-only, private data directory), then the page corrects it. | from `set1c` (Arch, Debian run-b); not on Ubuntu |

Run copies (`harness-run-copy/`: `runcopy-c4cf7fd9-files.sha256`, 47,668 files; per overlay `files.sha256`,
`harness.diff`, `differs-from-archive.txt`; job records `jobs/`): `harness` = pristine archive (first build attempt,
offline `p`); `harness-h23` (build, prove); `harness-set1` (Debian run-a); `harness-set1b` (Ubuntu); `harness-set1c`
(Arch, Debian run-b) = the working-tree files (SHA-256 equal). `PYTHONDONTWRITEBYTECODE=1`. No product file changed.

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`, GiB; `host/c-drive.txt`)

164.0 free at the start (20:56:09Z), 154.1 before Debian run-a, 150.7 before Ubuntu, 148.1 before Arch, 146.3 before
Debian run-b, **145.4 at the end and lowest** (22:09:48Z). Never under 40 GiB before a cell. A process-level keep-awake
request was held 20:56:29Z-22:10:00Z (`host/keepawake.log`).

## Cells

One new lab per cell, stopped by the wrapper (`<cell>/<run>/host/wrapper.*`); per-step times in `sections-table.md`.

| # | Cell / run | Lab, port | Harness | Wrapper (UTC) | Overall |
| --- | --- | --- | --- | --- | --- |
| 1 | set1-debian13 run-a | set1-d13-a, 4011 | set1 | 21:16:28-21:27:49 | failed (S4 g product; S5 c/d harness H25) |
| 2 | set1-ubuntu run-a | set1-ub-a, 4021 | set1b | 21:31:09-21:48:38 | failed (S2 f product; S4 g product) |
| 3 | set1-arch run-a | set1-arch-a, 4031 | set1c | 21:49:31-21:58:20 | failed (S4 g product) |
| 4 | set1-debian13 run-b (the one re-run) | set1-d13-b, 4041 | set1c | 21:58:51-22:09:28 | failed (S4 g product) |

Platform facts read on the guests: Debian 13: Postfix 3.10.13, PostgreSQL 17.11, MariaDB 11.8.6, Debian `cron`.
Ubuntu 24.04: Postfix 3.8.6, PostgreSQL 16.15, MariaDB 10.11.14, `cron`. Arch: cronie 1.7.2-2, PostgreSQL 18.6,
MariaDB 13.0.2.

## Sections per platform

Verdicts are the driver's (`<cell>/<run>/steps/NN-*/section.json`; every check with its detail; `natives` lists the
read-only inspections, `owner_actions` what the lab did as the owner). Sub-steps in brackets.

| Section | Debian 13 (run-b, 22:08:13-22:09:27Z) | Ubuntu 24.04 (21:47:08-21:48:35Z) | Arch (21:57:34-21:58:18Z) |
| --- | --- | --- | --- |
| S1 cron | **pass** (a-f; 3 s) | **pass** (a-f; 6 s) | **pass** (a-f; 8 s) |
| S2 mail policy | **pass** (a-f; 33 s) | **FAIL** in f (a-e pass; 38 s) | not run (no mail on Arch) |
| S3 backup schedule | **pass** (1 s) | **pass** (4 s) | **pass** (2 s) |
| S4 PostgreSQL | **FAIL** in g (a-f pass; 17 s) | **FAIL** in g (a-f pass; 20 s) | **FAIL** in g (a-f pass; 22 s) |
| S5 MariaDB | **pass** (a-e; 11 s) | **pass** (a-e; 8 s) | **pass** (a-e; 11 s) |
| S6 catch-all, mail queue | **pass** (8 s) | **pass** (9 s) | not run (no mail on Arch) |
| S7 file metadata | **pass** (4 files) | **pass** (4 files) | **pass** (3 files) |

Debian run-a (21:26:42-21:27:48Z): S1, S2, S3, S6, S7 pass; S4 fails in g as everywhere; S5 a, b, e pass and c/d are
H25 (kept as run, not corrected).

### S1 scheduled tasks

- **a, the cronie answer.** A site user without a crontab: `LC_ALL=C crontab -u set1_owner_test -l` answers exit 1, no
  output and exactly `no crontab for set1_owner_test` on standard error on **all three platforms, cronie 1.7.2-2 on
  Arch included** (`steps/08-s1-cron/native/*a-crontab-none.json`, `no_crontab_answer` in `native-facts.json`). The
  list answers 200 `{"jobs": [], "version": "ct1-e3b0c442…"}` (the version of an empty crontab), not an error. The open
  question of the 2026-10-08 contract entry is settled: cronie's wording is recognised.
- **b, c.** Add: the crontab holds exactly `*/15 * * * * /usr/bin/true set1-panel-job`. The identical task again:
  `409 CRON_JOB_DUPLICATE`, crontab unchanged.
- **d.** The owner installs a crontab with a comment line and an unrelated task (`crontab -u <user> <file>`). A write
  carrying the earlier version: `409 SETTINGS_CHANGED` / `scheduled_tasks`, nothing changed. After a reload both tasks
  are listed, the owner's comment with the owner's task.
- **e.** Disable, edit while disabled, enable, delete: after each request `crontab -l` is the expected text byte for
  byte: only the Panel's line differs (`# DISABLED: …`, the new schedule, enabled again, gone); the owner's comment and
  task never moved.
- **f.** Debian and Ubuntu: the owner restricts cron with `/etc/cron.allow` containing `root` (a common hardening
  step). Debian's `crontab` then refuses even root for that user: exit 1, `The user set1_owner_test cannot use this
  program (crontab)`. The list answers `502 CURRENT_SETTINGS_UNREADABLE` / `scheduled_tasks` (not an empty list) and
  Add is refused with the same answer; after the owner removes the file the crontab and its version are what they
  were. Arch: cronie exempts root from `cron.allow` (native read still succeeds, list 200: recorded, not judged); the
  second cause, the spool relocated behind a symlink whose volume is not mounted, gives exit 1 `/var/spool/cron: No
  such file or directory` / `/var/spool/cron: mkdir: File exists`, and the Panel answers the same two 502s.

### S2 server mail policy (Debian 13, Ubuntu 24.04)

Stock state: `smtpd_recipient_restrictions` empty, `message_size_limit = 10240000` (shown as 9 MB);
`steps/09-s2-mail-policy/native-text/postconf-n-before.txt`.

- **a.** The read answers the native values with an `mp1-…` version.
- **b.** The owner writes, one restriction per line, `permit_mynetworks, permit_sasl_authenticated,
  reject_unauth_destination, reject_unknown_sender_domain, check_policy_service unix:private/policyd-spf` into
  `main.cf` and reloads Postfix. The Panel read has a new version; a Save with the old one: `409 SETTINGS_CHANGED` /
  `mail_policy`; `main.cf` unchanged (same SHA-256).
- **c.** Save DNSBL zone `dnsbl.set1-lab.test` and rate 30 with the new version: 200. `postconf -h`: the owner's six
  elements (five restrictions, one with its argument) in the same order, then `reject_rbl_client dnsbl.set1-lab.test`; `smtpd_client_message_rate_limit = 30`,
  `anvil_rate_time_unit = 60s`; `message_size_limit` untouched (still 10240000, not rounded). `postfix check` exit 0
  with the same output as after the owner's own edit; the journal has `postfix/master[…]: reload -- version …` with
  the same master PID; port 25 accepts MAIL FROM/RCPT TO for the site's mailbox (no DATA sent) and 587 answers.
  Diff: `diff/c-main.cf-panel-save.diff`.
- **d.** Zone removed: the list is the owner's six elements exactly.
- **e.** The owner rewrites the list as `${set1_owner_checks}, reject_unknown_sender_domain`. The read reports
  `dnsbl_locked: variable`; a DNSBL save: `409 MAIL_POLICY_RESTRICTIONS_UNMANAGED` / `variable`, `main.cf` unchanged; a
  save of size 30 MB and rate 45 only: 200, `message_size_limit = 31457280`, rate 45, the owner's line byte-identical.
- **f.** The owner leaves an unfinished edit in `main.cf` (`default_process_limit = 200 # raised for the campaign`;
  `postfix check`: `fatal: bad numerical configuration`). A save of rate 46:
  - Debian 13: `502 MAIL_POLICY_NOT_RELOADED`, `mutation_applied: true`, detail `Job for postfix.service failed…`;
    46 is in `main.cf`, the master process is the same; the next read shows 46. **As designed.**
  - **Ubuntu 24.04: `200 {"success": true, "policy": {… "outbound_rate_limit": 46}}`** although the reload failed:
    `systemd[1]: Reload failed for postfix@-.service` (`steps/15-collect/journal-services.txt`, 21:47:45.56Z). Finding
    P1.

### S3 automatic backup schedule

No row before; read: off with a `bs1-…` version. Create without a version: `409 SETTINGS_VERSION_REQUIRED` /
`backup_schedule`, no row. Create weekly/full/30 with the version: 200, row `weekly/full/30/1` (Panel database opened
read-only). Stale write (daily/files/7 with the first version): `409 SETTINGS_CHANGED`, row unchanged. Change to
retention 14 with the current version: 200, row `weekly/full/14/1`, read agrees. What the next run would prune: the
product's rule (`pruneBackups`) applied to `GET …/backups`: 0 scheduled copies listed, 0 to prune, retention read 14.

### S4 PostgreSQL (`postgresql@17-main`, `postgresql@16-main`, Arch `postgresql`)

- **a.** `GET /api/v1/config` returns the file's own bytes and `cf1-<sha256 of those bytes>` for both files.
- **b.** `work_mem` 8MB: 200 `applied: reloaded`, `daemon_check: accepted`. The file differs on one line
  (`-#work_mem = 4MB				# min 64kB` / `+work_mem = 8MB				# min 64kB`, `diff/b-postgresql.conf.diff`); the
  backup `postgresql.conf.celikpanel-backup-<UTC>` holds the previous bytes with the file's owner, group and mode; no
  validation copy left; `SHOW work_mem` = `8MB`; `pg_conf_load_time()` moved while `pg_postmaster_start_time()`, the
  postmaster PID and the unit's MainPID stayed (Debian run-b 16173, Ubuntu 21536, Arch 1874).
- **c.** `work_mem = eight-megabytes`: `422 CONFIG_INVALID` / `daemon`, detail `invalid value for parameter "work_mem":
  "eight-megabytes"`, name `work_mem`; file, backups and running value unchanged.
- **d.** Empty content: `422 CONFIG_INVALID` / `empty`; unchanged.
- **e.** `pg_hba.conf`: a `host all all 10.99.0.0/24 scram-sha-256` rule: 200 reloaded, one added line, backup kept,
  and `pg_hba_file_rules` lists it with no error anywhere in the file. A rule without a method: `422` / `syntax`, detail
  `end-of-line before authentication method` with the line number; unchanged. The file without its `local` rules
  (Debian/Ubuntu `local all postgres peer`, Arch `local all all trust`): `422` / `lockout`; unchanged.
- **f.** The owner appends a comment by hand; a save with the earlier version: `409 SETTINGS_CHANGED` / `config_file`;
  the file is the owner's.
- **g.** No failure of the packaged unit's reload was found that an owner causes, so the lab used an owner's own unit
  drop-in: `ExecReload` signals the server as the packaged unit does and then reloads a pooler unit that does not
  exist. A save of `work_mem = 16MB`: `502 CONFIG_RELOAD_FAILED` / **`not_restored`**. Native state: the previous file
  **is** back in place (text equal, owner/group/mode equal), the server runs with 8MB and was not restarted, and the
  copy the answer names holds that same previous file. Finding P2. (`reload_failed` in `native-facts.json`,
  `native-text/postgresql-journal-g.txt`.)

### S5 MariaDB (Debian/Ubuntu `/etc/mysql/mariadb.conf.d/50-server.cnf`, Arch `/etc/my.cnf`)

- **b.** `max_connections` 173: 200 `applied: restart_required`, `daemon_check: accepted`. One line differs on
  Debian/Ubuntu (`-#max_connections        = 100` / `+max_connections        = 173`); on Arch, whose `/etc/my.cnf` has
  no server group, the driver's edit adds `[mysqld]` and the option at the end. Backup kept with owner/group/mode.
  **MariaDB was not restarted**: same MainPID, same start time, `NRestarts` 0, `@@global.max_connections` still 151.
- **c.** An unknown variable: `422 CONFIG_INVALID` / `daemon`, `unknown variable 'set1_unknown_option=1'`. An unusable
  value (`unlimited`): `422`, `Unknown suffix 'u' used for variable 'max_connections' (value 'unlimited'). Legal suffix
  characters are: K, M, G, T, P, E`. Both unchanged.
- **c, observation (Debian run-a and run-b, Arch).** `max_connections = plenty` is **accepted and written** (200,
  `daemon_check: accepted`). The installed `mariadbd`, run read-only on the written file (run-b and Arch), says the
  same: exit 0, `[Warning] … option 'max_connections': unsigned value 0 adjusted to 10`, resulting
  `max-connections 10`. The page then saved the correct value again (200). Observation O1.
- **d, e.** Empty content `422` / `empty`; a stale write after the owner's hand edit `409 SETTINGS_CHANGED`.

### S6 catch-all and mail queue (Debian 13, Ubuntu 24.04)

- Catch-all: read off with a `ca1-…` version; set without a version `409 SETTINGS_VERSION_REQUIRED` /
  `mail_catch_all`; set: 200 and `postmap -q @set1-owner.test` over `virtual_alias_maps` returns the mailbox; stale set
  and stale disable `409 SETTINGS_CHANGED`, nothing changed; disable: 200 and Postfix has no catch-all again.
- Queue: idle: native `postqueue -j` exit 0 with no output, Panel `200 []`. **Postfix stopped**: root's `postqueue -j`
  still reads the queue (exit 0, `warning: Mail system is down -- accessing queue directly`), Panel `200 []`: the queue
  is known, correctly. **Owner's unfinished edit in main.cf**: `postqueue: fatal: bad numerical configuration`, exit
  69; Panel `502 MAIL_QUEUE_UNREADABLE`. After the correction: known and empty again.

### S7 owner, group and mode

Recorded before the sections and again at the end (`steps/14-s7-file-metadata/section.json`): unchanged for every
file the Panel wrote, and every kept backup has the file's owner, group and mode. Debian 13: `main.cf` root:celikpanel
0644, `postgresql.conf` postgres:postgres 0644, `pg_hba.conf` postgres:postgres 0640, `50-server.cnf` root:root 0644
(Ubuntu: the same four; Arch: `postgresql.conf` and `pg_hba.conf` postgres:postgres 0600, `/etc/my.cnf` root:root
0644; all unchanged; `native-facts.json`). The inode changes with each write
(atomic replacement).

## Verbatim texts (EN / TR)

`texts-en-tr.md` holds every distinct answer with its status, code, reason, the API's sentence, the values it carried
and the catalogue sentences for the screen. The API answers in English only; the Turkish sentences are the installed
build's catalogue entries looked up by key (the driver does not render a screen). The ones the findings refer to:

- `409 SETTINGS_CHANGED` (all five resources), API: "The settings changed on the server since this page loaded, so
  nothing was changed. Reload the page to see the current settings, then make the change again." Screen, scheduled
  tasks, EN: "The scheduled tasks changed on the server after this list loaded, so nothing was changed. Reload the
  list, then try again; a task you were typing stays in the form." TR: "Zamanlanmış görevler bu liste yüklendikten
  sonra sunucuda değişti; bu yüzden hiçbir şey değiştirilmedi. Listeyi yeniden yükleyin, sonra tekrar deneyin;
  yazmakta olduğunuz görev formda kalır."
- `502 CURRENT_SETTINGS_UNREADABLE` / `scheduled_tasks`, API: "CelikPanel could not read what is currently set on this
  server, so nothing was changed. Reload the page to read it again. If it keeps failing, the server owner checks that
  the service behind this page is running, then reloads." Screen EN: "The scheduled tasks of this domain could not be
  read from the server, so the list is not shown and tasks cannot be added or changed here. Nothing was changed; the
  tasks already on the server are untouched. Try again." TR: "Bu domain'in zamanlanmış görevleri sunucudan okunamadı;
  bu yüzden liste gösterilmiyor ve buradan görev eklenemiyor ya da değiştirilemiyor. Hiçbir şey değiştirilmedi;
  sunucudaki görevlere dokunulmadı. Tekrar deneyin."
- `502 MAIL_POLICY_NOT_RELOADED` (Debian), API: "The mail policy was saved to /etc/postfix/main.cf, but Postfix could
  not be reloaded, so Postfix is still running with the previous settings. Nothing was rolled back. The server owner
  runs sudo postfix check to see what Postfix objects to, corrects it, and then runs sudo systemctl reload postfix.
  Reload this page afterwards; the saved values are the ones shown." TR (`err.MAIL_POLICY_NOT_RELOADED`): "Posta
  politikası /etc/postfix/main.cf dosyasına kaydedildi ancak Postfix yeniden yüklenemedi; bu yüzden Postfix hâlâ önceki
  ayarlarla çalışıyor. Hiçbir şey geri alınmadı. Sunucuda sudo postfix check komutuyla Postfix’in neye itiraz ettiğini
  görün, düzeltin, sonra sudo systemctl reload postfix komutunu çalıştırın. Aşağıda gösterilen değerler kaydedilen
  değerlerdir."
- `502 CONFIG_RELOAD_FAILED` / `not_restored`, API: "The service could not reload with the new file, and CelikPanel
  could not put the previous file back with certainty. The server owner checks the file on the server; the copy named
  below holds the other version. Then reload the service (sudo systemctl reload <service>) and reload this page."
  Screen EN: "PostgreSQL could not reload with the new file, and CelikPanel could not put the previous file back with
  certainty. What the server holds now is shown below. Check the file on the server, reload PostgreSQL there, then
  reload this page. The other version is kept on the server as:" TR: "PostgreSQL yeni dosyayla yeniden yüklenemedi ve
  CelikPanel önceki dosyayı kesin olarak geri koyamadı. Sunucunun şu an tuttuğu dosya aşağıda gösteriliyor. Dosyayı
  sunucuda denetleyin, PostgreSQL hizmetini orada yeniden yükleyin, sonra bu sayfayı yenileyin. Diğer sürüm sunucuda şu
  adla duruyor:"
- `502 MAIL_QUEUE_UNREADABLE`, API: "The mail queue could not be read from Postfix, so it is not shown. This does not
  mean the queue is empty. Nothing was changed. Try again; if it keeps failing, the server owner checks that Postfix is
  running (sudo systemctl status postfix)." TR (`postfix.queue.unknown`): "Mail kuyruğu Postfix’ten okunamadı; bu
  yüzden gösterilmiyor. Bu, kuyruğun boş olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin; sorun
  sürerse sunucuda Postfix’in çalıştığını denetleyin (sudo systemctl status postfix)."
- MariaDB saved, EN (`dbconf.saved.restartRequired`): "Saved. MariaDB checked the file and accepts it. MariaDB reads
  this file only when it starts, so the change takes effect after its next restart. Restart it from the top of this
  page when it suits you." TR: "Kaydedildi. MariaDB dosyayı denetledi ve kabul ediyor. MariaDB bu dosyayı yalnız
  başlarken okur; bu yüzden değişiklik bir sonraki yeniden başlatmadan sonra geçerli olur. Size uygun olduğunda bu
  sayfanın üstünden yeniden başlatın."

## Findings

### Candidate product defects

- **P1. Ubuntu 24.04: a mail policy save is answered "saved" although Postfix could not reload.**
  `cmd/agent/mail_policy_rpc.go:69` runs `systemctl reload-or-restart postfix`. On Ubuntu 24.04 (Postfix 3.8.6)
  `postfix.service` is a oneshot wrapper with `ExecReload=/bin/true`; the daemon is `postfix@-.service`
  (`ReloadPropagatedFrom=postfix.service`). The instance's reload failed (`Reload failed for postfix@-.service`), the
  wrapper's did not, `systemctl` exited 0, and the Panel answered `200 success` with the new rate. Sequence: owner
  leaves `default_process_limit = 200 # raised for the campaign` in `main.cf` without reloading; `PUT
  /api/v1/mail/policy` with rate 46 and the current version. `main.cf` holds 46, Postfix runs with 45, nobody is told.
  Debian 13 (native `postfix.service`) answers `502 MAIL_POLICY_NOT_RELOADED` for the same sequence. Evidence:
  `set1-ubuntu/run-a/steps/09-s2-mail-policy/section.json` (calls `S2f …`, checks `f: …`),
  `native-text/postfix-unit.txt`, `steps/15-collect/journal-services.txt` lines 240-245. The same command is used at
  `cmd/agent/mail_stack_rpc.go:139` and `:426` (not measured here).
- **P2. All three platforms: after a reload that fails twice the answer says the previous file could not be put back,
  although it was, and names a copy of that same file as "the other version".** `cmd/agent/db_config.go:302-314`: the
  first reload fails, `putBack()` succeeds, the reload with the restored file fails too, and the answer is
  `ConfigReloadNotRestored` with `name: backup` and the message "…the refused file is kept as <backup>". `backup`
  holds the previous file, the refused content is kept nowhere, and the Panel's sentence
  (`cmd/panel/config_rpc_error.go:48-50`, catalogue `dbconf.reloadFailed.notRestored`) tells the owner the previous
  file is not in place. Sequence: S4 g above. Measured: file text equal to the previous one, named copy's SHA-256
  equal to it, `SHOW work_mem` 8MB, postmaster PID unchanged. Evidence: `*/steps/11-s4-postgresql/section.json`
  (`reload_failed`, check `g: an answer that says…`). The cause used is an owner's unit drop-in (no stock reload
  failure was found); the single-failure answer `restored` was therefore **not** reached natively.

### Observations (not judged as defects)

- **O1. A MariaDB value that MariaDB itself adjusts is saved with "MariaDB checked the file and accepts it".**
  `max_connections = plenty` would be 10 at the next start (mariadbd's own resulting value; MariaDB was not
  restarted); `mariadbd --help --verbose` exits 0 with a `[Warning]` line, and `cmd/agent/db_config.go:428-431` reads only the exit status. The 2026-10-09 contract entry names this limit
  ("exits 0 for … an out-of-range value it adjusts"); the warning is not shown to the owner.
- **O2. The owner's layout of `smtpd_recipient_restrictions` is not kept.** A list written one restriction per line
  comes back as one line after a DNSBL save (`postconf -e`, `cmd/agent/mail_policy_restrictions.go:279-290`). Elements,
  order and separator style are kept, as the contract says.
- **O3. Guidance names a cause that was not the cause.** Cron unreadable (cron.allow, relocated spool): "the server
  owner checks that the service behind this page is running" (`cmd/panel/current_settings_errors.go:39-40`); cron was
  running. Queue unreadable: "checks that Postfix is running" (`cmd/panel/email_handlers.go:56-57`); Postfix was
  running, and a stopped Postfix does not make the queue unreadable for the Agent. The Agent's log has the real line.
- **O4. `502 MAIL_POLICY_NOT_RELOADED` carries no policy** (`cmd/panel/mail_policy_handlers.go:91-94` drops the
  Agent's `Policy`); the screen's next read showed the saved values.
- **O5. The component scan lists each PostgreSQL file twice on Debian and Ubuntu** (`result.json` `site.config_files.
  listed`; the page picks the first).

### Platform limitations

- Debian/Ubuntu `cron` refuses `crontab -u <user>` even for root when `/etc/cron.allow` exists without that user: a
  hardened server's site tasks cannot be read or changed by the Panel (answered as unreadable, nothing written).
- cronie exempts root from `cron.allow` and recreates a missing spool directory itself.
- Mail is not supported on Arch: S2 and S6 did not run there.

### Harness

H23-H26 above. Debian run-a's S5 c/d verdicts are H25, not product behaviour. The verbatim-text file takes each
platform's latest run; it merges request labels across platforms (Ubuntu did not run the `plenty` sub-step).

## What this run does NOT prove

- Nothing about an installed server, a published release, a production licence or the update path.
- The browser: the driver sends what the screens send; no screen was rendered, so "what was typed stays", disabled
  Save buttons and the `loading | known | unknown` states are unmeasured. Turkish sentences are catalogue lookups.
- One run per platform and per sequence (Debian twice); no concurrency: two Panel requests at once, or an owner edit
  between the Agent's version check and its write, were not attempted.
- S3: no scheduled backup ran, so the prune itself never executed. S1 f on Debian/Ubuntu used `cron.allow` only.
- S4: `CONFIG_RELOAD_FAILED` / `restored`, a `pg_hba.conf` the running server refuses after installation, a stopped
  PostgreSQL (`not_running`), settings that wait for a restart, and the ten-backup pruning were not reached. S4 g's
  cause is an owner drop-in, not a stock failure.
- S5: MariaDB was never restarted, so no saved value was seen to take effect; Oracle MySQL is absent.
- S2: locks `malformed`, `no_baseline`, `terminal`, `MAIL_POLICY_INVALID`, and DNSBL lookups against a real zone were
  not run; mail acceptance is an SMTP dialogue up to RCPT TO from loopback, no message was delivered.
- Setup stopped at the isolated host's DNS wait: no panel or mail certificate, no mail enrollment.

## Removals, leftovers, secrets

- Removed, after each cell's evidence was staged and its driver `SHA256SUMS` verified (`host/removals.txt`): the seven
  overlay disks of the four labs (`set1-d13-a` 2, `set1-ub-a` 1, `set1-arch-a` 2, `set1-d13-b` 2). Nothing else.
- Left on the WSL host (`host/host-leftovers.txt`): `/var/tmp/cp-set1-run` (3.1 G: five run copies, a mutable
  development copy `harness-dev`, logs), the four labs without their overlays (0.66-0.92 G each: base image copies,
  keys, evidence), the build clone `/var/tmp/cp-upd1-build/20261008t210046z` and the failed first attempt
  `…/20261008t205628z`, five dist directories under `/var/tmp/cp-pair-accept/dist/` (0.84 G each), six
  `/tmp/set1-dry-*` files. No QEMU and no job process was running at the end; HEAD and the local git configuration are
  unchanged.
- `secret-scan.txt`: no private-key block, no line of the 16 lab key files, no licence key, no unredacted password,
  secret, token or cookie field (the owner, mailbox and site-account passwords appear only as `[REDACTED]`; 734
  markers). The seven 43-character strings it lists are folder names inside the run-copy file list, not secrets.
  No database was created through the Panel in this cell, so no database password exists.
- Root `SHA256SUMS` covers every file; the longest repository-relative path is under 240 characters.
