# set3 native run, 2026-10-09 (UTC): the second round of corrections on real services, and the owner-started update from the published v0.1.0-alpha.81 to the candidate (`cfa329676`)

Two parts on disposable QEMU/KVM guests of the local `archlinux` WSL host.

- **Part 1** re-measures, with the packaged services, the corrections the candidate `cfa329676` made after the second
  native measurement (set2): S1, P3, P3b, P4, P5, O9, O10, O11, O14. Debian 13 and Ubuntu 24.04 for all of them, Arch
  for the PHP site, the import and what does not need mail. Everything that passed in set2 runs again as a
  regression check.
- **Part 2** is the path an installed server takes: the **published tag `v0.1.0-alpha.81`** (commit `a0beb7263`,
  schema 42) is installed by its own installer, and its owner starts the update to a candidate built from
  `cfa329676` and labelled as the next release (schema migration 43, `request_identities`).

Everything was built from commit `cfa329676` (run copies are `git archive cfa329676` plus the harness files listed
below; the working tree was never used for a build or a cell) and, for the Part 2 baseline, from the tag's own
commit. No installed server (Boston, Frankfurt, any) was touched or contacted. Nothing was committed, pushed,
published or signed with a production key. `celikpanel.net` resolved only to each guest's own loopback fixture; no
licence service was contacted (acceptance-fixture licence); the two Let's Encrypt directory names resolved to each
guest's own loopback, confirmed on every platform before any request was sent to the certificate route. Every
`result.json` carries `native_evidence: false`; the owner judges it. **It closes no P0 row.**

**Result in one sentence:** on Debian 13 and Ubuntu 24.04 every correction of the second round is measured as made (S1, P3, P3b, P4, P5, O9, O10, O11, O14; every set2 section still passes; the only checks that did not pass in the first runs are the harness defect H40, and the Debian re-run after its fix passes completely); on Arch S1, O9, O10 and O14 pass and **one candidate product defect remains: a PHP site still cannot be created there** (P5b: the nginx vhost template includes a snippet file that only Debian's and Ubuntu's nginx packages ship), so no import starts, and a second Arch reading with that one file placed by the owner passes every section; and in Part 2 **every cell of the update from the published v0.1.0-alpha.81 reached its expected end** - verified on Debian 13, Ubuntu 24.04 and Arch with the ledger at the released 43, the guard and the versioned writes working and refusing what a page opened before the update sends, the deferred mail work done and the CLI and the card in agreement; automatic return to alpha.81 with the ledger at the released 42 and the data intact after a migration defect with a second fault (three platforms) and after a failed start check (Debian), with alpha.81's own recovery reader, outcome card and root CLI telling the owner what happened; the owner's one printed retry completing a paused update (Debian, Ubuntu); and the workloads served with management off across a reboot (Debian).

## What was built from what

Three builds (`go1.26.5 linux/amd64`), every archive `license_mode: acceptance-fixture` (`build/<name>/`: artifact
document, dist JSONs, fixture commits and patches, trees, build logs; the Part 2 folders also hold
`baseline-ref-proof.txt` and `agent-deps.txt`).

| Build (folder under `build/`) | Role | Label / seq | Commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `cur` (06:15:55-06:47:08Z) | baseline | v0.1.0-alpha.81 / 81 | 0473af3e56364e4e0f78b4b6b971f3ccfefb59fe | 04bb0e7aabfc965dcc344bc4e67b4808c87743fd | a94e84bfefdf842747a5d76d636271ec7fa547339f79012e3223148c230e5a57 |
|  | good | v0.1.0-alpha.82 / 82 | 9f9064bd0143e6e9978f472e3027ac18aa357ec1 | 9a6a6047e8b5c36546fc6bf0d276d4952b161dfe | c8044e600a0210b6e9b93b46b2337e9be6549acde2bcd670f5e1adb5380b94f6 |
|  | defective | v0.1.0-alpha.82 / 82 | 105d32bbe88fd5afb2d3832ff1b5c5ae41bf451b | d7889114db45787bc1d0c2b890b12f29db28404d | 06c3e7a30b0cf45ade9ffe98694a40707128abbfc47de254faeeb004e0e6c265 |
|  | startcheck | v0.1.0-alpha.82 / 82 | 9e8c2029b846afa958986aedcda4de7af40eb0a3 | 1bd465b29256d92ee3bea0a6e2dc6b134928ba21 | a9a5cc3c5e5dba5f9035f935e134ff606a53aa73365d7c9c6dbd103cb7cd9dad |
|  | realstart | v0.1.0-alpha.82 / 82 | 1831a16184dbe62325ed76f19844428df3c8dbcc | 43c56940d41e80a124669e465ce1f0ee20f0d258 | 0ef92ef0a16899ade56c5d7a0e8aaa1bdb1e8e6167a4c20a6e6d8dadfcdde12d |
| `a81-first` (06:48:30-06:51:08Z) | baseline | v0.1.0-alpha.81 / 81 | a0beb7263d1f4ca72258f6b306f9111ba4e2a334 | b1dffa78bcaca9e3514e5b2bc42f0e2cf47dca2f | 3350ff44dad2bb699ab5ee0a112b58b7bfb7fa47aa0ebdd3dd3c2da73d080109 |
|  | good | v0.1.0-alpha.82 / 82 | 30ac1488280a7f3c5cd1bb514a7447d1dc9bf7a3 | ed6e45459b10fdec3da92c04037a8c8f5cf67157 | 6fb0b8d5f0be38830294c364137da46f20c3834b3b005331fb9196f855465953 |
|  | defective | v0.1.0-alpha.82 / 82 | cc93780e6392dfeba8dfcd70c19028e9fbb79974 | 84adc63cb6e2a6c294c59b9bc3bb59fbf1287cab | 85e4b7d434ee712ce8efb0880b244e0f011c8175349a648bfc3a5007449b3933 |
| `a81` (07:04:18-07:07:47Z) | baseline | v0.1.0-alpha.81 / 81 | a0beb7263d1f4ca72258f6b306f9111ba4e2a334 | b1dffa78bcaca9e3514e5b2bc42f0e2cf47dca2f | 3350ff44dad2bb699ab5ee0a112b58b7bfb7fa47aa0ebdd3dd3c2da73d080109 |
|  | good | v0.1.0-alpha.82 / 82 | c309f79e64d144f374d9b98fe5c67a34f9bc885c | ed6e45459b10fdec3da92c04037a8c8f5cf67157 | d2063f1e7af932ed41bae6defbcb8abe42b0ac6f2cd659c16c4be4aa0f60c989 |
|  | defective | v0.1.0-alpha.82 / 82 | b11032be780c6b4f3c2edf392af7af754d124083 | 84adc63cb6e2a6c294c59b9bc3bb59fbf1287cab | e17ddb7fae3c2227986b2b36f57a631b99ff060401fc0820af236bedae663420 |
|  | startcheck | v0.1.0-alpha.82 / 82 | f18a9d416fbc785236891e6645cd53063d8f91ec | 4a681fbbcd4267ed6f82ba7032c7bb77a92752b0 | af948dc49a5b53dcb8b91212867087195bbd2062d80c32e095c23b7f75d3e53b |

- **`cur` (Part 1).** `run-upd1.sh build cfa329676` from the pristine run copy. The baseline's tree `04bb0e7a...` **is
  the tree of `cfa329676`** (`build/cur/trees.txt`): the guests of Part 1 ran exactly the candidate source with the
  acceptance licence seam switched on at build time. G, D, S and R were built by the script and not used in Part 1.
- **`a81` (Part 2).** `run-upd1.sh build --baseline-ref v0.1.0-alpha.81 cfa329676`. The baseline is the **tag's own
  commit `a0beb7263d1f4ca72258f6b306f9111ba4e2a334`, unchanged**: no fixture commit and no patched file. G is
  `cfa329676` plus the one-file release policy `v0.1.0-alpha.82 / 82, previous 81 / v0.1.0-alpha.81 /
  a0beb7263...` (exactly what a real alpha.82 would name as its predecessor), D is G plus the migrate-only defect, S
  is G plus the start-check defect (`build/a81/fixture-patches.diff`). B's `web/` was built from the tag, the
  candidates' from the source.
- **`a81-first`.** The first published-baseline build (06:48Z) had B, G and D only; the published-baseline mode did not
  build the start-check candidate. The mode was extended and the build repeated (`a81`, 07:04Z). The baseline archive
  of `a81` **is the one `a81-first` built** (`dist/a0beb7263...-acceptance-license`, SHA-256 `3350ff44...`; the dist
  builder does not build the same commit twice); G and D of `a81-first` were never installed.

### What the published baseline needs, and the proof that it is the tag

Unlike v0.1.0-alpha.80 (upd7: six licence files had to be patched in), **the tag v0.1.0-alpha.81 already carries the
D-027 acceptance-licence seam and its guard** (`internal/licensing/acceptance_fixture.go`, `acceptance_off.go`,
`acceptance_owner_*.go`, `deploy/release-acceptance-license-guard.sh`; the build refuses a tag without them). So the
baseline build needs **nothing but the tag**: the dist builder compiles the archive's `bin/panel` from the tag's
source with the build tag `acceptance_license`; every other file of the archive is the tag's.

- `build/a81/baseline-ref-proof.txt`: `git diff --name-status tag..baseline` is **empty** (the baseline is the tag
  commit); the trees `cmd/agent`, `deploy`, `download-portal`, `web` and `internal/transport` are identical; 35 entries
  (30 files and 5 trees) are listed with blob or tree id (files also with SHA-256) at the tag and at the baseline, each `identical` (0 `DIFFERENT`); the files are: `update.sh`,
  `rollback.sh`, `install.sh`, `rebuild.sh`, `bootstrap-update.sh`, `bootstrap-prebuilt-update.sh`,
  `download-portal/get.sh`, every `deploy/release-*`, `deploy/finalize-*`, `deploy/enroll-signed-release-trust.sh` and
  `deploy/systemd/*`; the Agent's package closure (`go list -deps ./cmd/agent`, `build/a81/agent-deps.txt`) holds no
  licensing package.
- `build/a81-prove.json` (`prove` exited 0, on run copy `c`): archive inventory, release policy and committed-source
  proof of B, G, D and S against their own commits' blobs.
- The tag's release policy is `v0.1.0-alpha.81 / 81, previous 80 / v0.1.0-alpha.80 / bd14d97e...`, unchanged, and the
  installed build identity is `version=v0.1.0-alpha.81 commit=a0beb7263...`: what a published installation reports.
- What it is not: the signed release archive. The installed bytes are the tag's source built here.

Offline suites, all OK on every run copy (`build/offline-<copy>-*.txt`): `test_owner_update_trial` 189 (191 from
copy `b`), `test_settings_writes_trial` 30, `test_request_identity_trial` 17, `test_recovery_candidate_archive` 11,
`test_lab` 15, `test_current_worker_baseline` 7, `test_worker_fixture_origin` 15, `test_bound_worker_reboot` 11,
`test_guest_bound_worker` 8, `test_guest_probe` 13. `prove` exited 0 for `cur` and `a81`; the dry runs of the five
Part 1 cells and of the ten Part 2 cells exited 0 with `native_evidence: false` and created no lab
(`build/set3-dry-*.json`, `build/a81-dry-*.json`, `build/prove*.out.txt`).

## Harness changes (working tree, `deploy/e2e/release-recovery/` only; no product file was changed)

| File | What |
| --- | --- |
| `owner_update_trial.py` (changed) | **Collection-time shape rules** for every driver (`shape_redactor`): hash-shaped credential values (crypt `$1$`-`$y$`, argon2, scrypt, pbkdf2, Dovecot `{SCHEME}` values, SCRAM verifiers, MariaDB native hashes) and WireGuard private and preshared keys are removed from every text and JSON string before it is written. `--baseline-ref v0.1.0-alpha.81`: the published tag as an **unpatched** baseline (`BASELINE_REFS`, `baseline_ref_patched`). New steps with a published baseline: `post-update-facts` (good cells) and `post-return-facts` (rollback cells); `ledger_verdict` against the digests `populated_database.py` pins for schema 42 and 43. |
| `build-upd1-artifacts.sh` (changed) | `--baseline-ref v0.1.0-alpha.81`: B is the tag commit itself (refused unless the tag carries the seam and the guard); candidates labelled `v0.1.0-alpha.82 / 82`; the start-check candidate is built in this mode too. The alpha.80 mode is unchanged. |
| `guest_schema_ledger.py` (new) | Read-only guest helper: the Panel database's migration ledger and schema as the digests `populated_database.py` pins, the table list, `request_identities`, SQLite's integrity checks. |
| `settings_writes_trial.py`, `guest_settings_native.py` (changed) | Cells `set3-*`. S8: a Reload of a stopped Postfix or Dovecot expects `409 not_running` and that nothing was started; Stop of Postfix with a refused `main.cf` expects success and no master; the PostgreSQL hook expects `reload_reread`; a second owner hook that fails before it signals the server expects `reload_not_reread`; the section counts `server_setup_busy` refusals (expects 0). |
| `request_identity_trial.py`, `guest_request_identity_native.py` (changed) | Cells `rid3-*`. The fixture mailbox carries the crypt hash of a password the lab knows; every archive holds the directory member; hostile members (`..`, absolute, symbolic link). New section C6b (PHP site) and C7b (mailbox login, partial import, hostile archives). C0/C3: the engines' own versions beside what the Databases page shows. C3: a sent password is not echoed and its answer is replayed. C5: the typed certificate failure. C9: hash-shaped values in the rows, in every guarded answer and in the journal. The driver counts hash-shaped values on the **raw** bytes and stores only counts and `has_password`. Guest readers: `read-mail-login` (IMAP LOGIN on loopback and `doveadm auth test`, with a wrong password beside the right one), `read-engine-versions`, `read-php`, `read-http` (a plain socket to `127.0.0.1:80`), `owner-php-probe`; `owner-nginx-php-snippet` (used only by the second Arch reading). |
| `run-set2.sh` (changed) | Accepts the `set3-*` and `rid3-*` cells. |
| `test_owner_update_trial.py`, `test_settings_writes_trial.py`, `test_request_identity_trial.py` (changed) | Offline tests of the new pure rules; they pin the new answers, stages, keys, the header, the migration and the template line against the product source. |

Run copies (`harness-run-copy/`: `runcopy-cfa329676-files.sha256`; per overlay `files.sha256`, `harness.diff`,
`differs-from-archive.txt`; job files `jobs/`; `queue.log`): `p` = pristine archive (build `cur`); `u` = `p` + the
published-alpha.81 mode (build `a81-first`); `a` = + the Part 1 drivers (set3-ubuntu, rid3-ubuntu, rid3-debian13
run-a; `prove`, first dry runs); `b` = `a` + the Part 2 steps, the start-check candidate in the published mode and the
ledger helper (build `a81`); `c` = `b` + two native reads judged on the API's own read-back (every Part 2 cell;
`prove` of `a81` and its dry runs); `d` = `c` + H40 (set3-debian13, rid3-arch run-a); `e` = `d` + the optional owner
action (rid3-arch run-b, rid3-debian13 run-b) = **the working tree's files** (SHA-256 equal,
`harness-run-copy/overlay-e/files.sha256`). `PYTHONDONTWRITEBYTECODE=1`.

Harness defects found during the run (product code was never changed):

| Id | Defect | Handling | Runs |
| --- | --- | --- | --- |
| H40 | The O10 check compared the new database's name on the engine with the name sent (`srvsent`); the engine's name carries the subscription prefix (`s2_srvsent`). | Compared with the name the answer carries. | rid3-debian13 run-a and rid3-ubuntu run-a: their two failed C3 checks are this (the recorded facts are the expected ones); fixed from copy `d` (rid3-arch run-a, rid3-debian13 run-b) |
| H41 | Evidence keys that contain `session` or `private` are blanked by the pair redactor (`phpsessionclean.service`, `PrivateTmp`): two unit facts of C6b read `[REDACTED]`. | Left as is (no value of the run is lost: both are unit names or unit properties recorded elsewhere). | every rid3 cell; cosmetic |
| H42 | `build-upd1-artifacts.sh`: a dist build that fails inside `$(build ...)` does not stop the builder (bash does not carry `errexit` into the substitution). In the rebuild `a81` the dist builder refused the tag commit ("refusing to reuse .../dist/a0beb7263...-acceptance-license", the directory `a81-first` had made 16 minutes earlier) and the builder went on with that existing archive. | Not changed in this run. The result is the intended one here (the same commit built by the same dist script; `prove` verified the archive against the tag's blobs, `build/a81-prove.json`), but the builder should say so or stop. | build `a81`; no cell affected |

The cells ran from a queue with three workers (`harness-run-copy/queue.sh`, `queue.log`): before each cell the newest
Windows `C:` reading (PowerShell `Get-PSDrive C`, written every 30 s by `tools/cwatch.ps1`; WSL cannot run PowerShell
on this host) had to be younger than 3 minutes and at least 40 GiB; after each cell its evidence was staged, the
driver's `SHA256SUMS` verified on the staged copy, and only then the lab's overlay disks removed.

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`, GiB; `host/c-drive.txt`, `host/c-drive-cells.txt`, `host/c-drive-watch.txt`)

114.2 (before any work, 06:15Z); 114.2 (stock-take after the stall, 06:46Z); 110.9 (before set3-ubuntu, rid3-ubuntu, rid3-debian13 (run-a), 06:59Z); 94.4 (before set3-debian13 run-a, 07:13Z); 93.7 (before rid3-arch run-a, 07:22Z); 92.2 (before upd1-debian13-good (part2-alpha81) run-a, 07:27Z); 92.2 (before upd1-ubuntu-good (part2-alpha81) run-a, 07:28Z); 90.5 (before upd1-arch-good (part2-alpha81) run-a, 07:33Z); 90.5 (before upd1-debian13-defective (part2-alpha81) run-a, 07:40Z); 90.5 (before upd1-arch-defective (part2-alpha81) run-a, 07:42Z); 89.9 (before upd1-ubuntu-defective (part2-alpha81) run-a, 07:48Z); 89.5 (before upd1-debian13-mgmt-off-reboot (part2-alpha81) run-a, 07:53Z); 89.5 (before upd1-debian13-startcheck (part2-alpha81) run-a, 07:53Z); 86.6 (before upd1-debian13-owner-continuation (part2-alpha81) run-a, 08:08Z); 86.6 (before rid3-arch run-b, 08:08Z); 86.6 (before upd1-ubuntu-owner-continuation (part2-alpha81) run-a, 08:10Z); 81.1 (before rid3-debian13 run-b, 08:19Z); 80.8 (end (all cells staged, all overlays of this run removed), 08:40Z). Lowest of the 188 readings taken every 30 s by the watcher: **80.8 GiB**; last: 80.8 GiB.

Never under 40 GiB before a cell and never under 30 GiB at any reading.

**Host power.** A process-level keep-awake request and one idle WSL session were held for the run
(`host/keepawake.log`). The host nevertheless entered modern standby from 06:18:27Z to 06:45:26Z
(`host/sleep-events.txt`: Kernel-Power 506/507; it ended with a power-source change), **before any cell started**:
it stretched the first build (`build/cur/build.*.txt`: 06:15:55-06:47:08Z, of which about 4 minutes are work) and
nothing else: execution really stopped in that window. The log has two more "entering modern standby" events inside
the cells' window (07:31:03Z, left 07:56:03Z; 08:11:55Z), on mains power: **during those the host kept executing**.
The Windows-side watcher wrote its `C:` reading every 30 s without a gap from 07:06Z to the end (largest distance 31
s), and the host-side sampler of every update cell (one request to the Panel every 5 s) has no gap larger than the
cell's own VM reset or reboot (at most 10.6 s; `sampler-gaps.txt`). New WSL sessions timed out several times under
load (`Wsl/Service/0x8007274c`); the jobs inside WSL were detached and not affected.

## Cells

One new lab per cell, stopped by the wrapper. Cells ran three at a time.

| Run (folder) | Lab | Harness copy | Wrapper (UTC) | Overall | Steps or sections not passed |
| --- | --- | --- | --- | --- | --- |
| set3-ubuntu/run-a | set3-ub-a | a | 06:59:17-07:22:02 | `complete-for-review` | - |
| rid3-ubuntu/run-a | rid3-ub-a | a | 06:59:19-07:28:08 | `failed` | C3-server-databases: failed |
| rid3-debian13/run-a | rid3-d13-a | a | 06:59:21-07:13:43 | `failed` | C3-server-databases: failed |
| set3-debian13/run-a | set3-d13-a | d | 07:13:53-07:27:30 | `complete-for-review` | - |
| rid3-arch/run-a | rid3-arch-a | d | 07:22:04-07:33:19 | `failed` | C6b-php-site: failed; C7-import: failed; C7b-import-answers: failed |
| part2-alpha81/upd1-debian13-good/run-a | u14-d13-good-a | c | 07:27:38-07:40:40 | `complete-for-review` | - |
| part2-alpha81/upd1-ubuntu-good/run-a | u14-ub-good-a | c | 07:28:10-07:48:51 | `complete-for-review` | - |
| part2-alpha81/upd1-arch-good/run-a | u14-arch-good-a | c | 07:33:24-07:42:25 | `complete-for-review` | - |
| part2-alpha81/upd1-debian13-defective/run-a | u14-d13-def-a | c | 07:40:43-07:53:54 | `complete-for-review` | - |
| part2-alpha81/upd1-arch-defective/run-a | u14-arch-def-a | c | 07:42:27-07:53:04 | `complete-for-review` | - |
| part2-alpha81/upd1-ubuntu-defective/run-a | u14-ub-def-a | c | 07:48:54-08:10:00 | `complete-for-review` | - |
| part2-alpha81/upd1-debian13-mgmt-off-reboot/run-a | u14-d13-mr-a | c | 07:53:07-08:08:20 | `complete-for-review` | - |
| part2-alpha81/upd1-debian13-startcheck/run-a | u14-d13-sc-a | c | 07:53:57-08:08:01 | `complete-for-review` | - |
| part2-alpha81/upd1-debian13-owner-continuation/run-a | u14-d13-oc-a | c | 08:08:04-08:28:28 | `complete-for-review` | - |
| rid3-arch/run-b | rid3-arch-b | e | 08:08:28-08:19:56 | `complete-for-review` | - |
| part2-alpha81/upd1-ubuntu-owner-continuation/run-a | u14-ub-oc-a | c | 08:10:04-08:37:45 | `complete-for-review` | - |
| rid3-debian13/run-b | rid3-d13-b | e | 08:19:57-08:34:39 | `complete-for-review` | - |

## Part 1: the second round of corrections on real services

Cell kinds `settings-writes` (cells `set3-*`: every section of set2 again, S8 with the new expectations) and
`request-identity` (cells `rid3-*`: every section of set2 again, two new sections C6b and C7b, new checks in C0, C3,
C5, C7 and C9). Setup profile as in set2: Debian/Ubuntu purpose `web_mail` with `nginx, php-fpm, mariadb, postfix,
dovecot, roundcube, rspamd, postgresql`; Arch purpose `web` with `nginx, php-fpm, mariadb, postgresql`; DNS mode
external; the setup waits at `access_dns` on the isolated guest in every cell (observed). The installed build is the
baseline B of build `cur`, whose tree is the tree of `cfa329676`.

### Item x platform (generated from the cells: `part1-table.md`)

The Ubuntu O10 cell shows FAIL because of harness defect H40 (the check compared against an unprefixed name, `srvsent`, while the engine's name is `s2_srvsent`); the facts recorded in the raw section file (`rid3-ubuntu/run-a/steps/11-c3-server-databases/section.json`) are the expected ones: `same_bytes: true`, the replay `replayed: "1"`, `sent_value_occurs_in_the_raw_answer: 0` and `replay_sent_value_occurs: 0` (the sent value found 0 times in the answer), `new_on_engine: ["s2_srvsent"]`; Ubuntu was not re-run after the harness fix.

| Item | Debian 13 | Ubuntu 24.04 | Arch | Arch, second reading with the owner's nginx snippet |
| --- | --- | --- | --- | --- |
| S1 preview and stored rows hold no hash-shaped value | pass (10 of 10 checks) | pass (10 of 10 checks) | pass (10 of 10 checks) | pass (10 of 10 checks) |
| S1 imported mailbox authenticates with its original password | pass (6 of 6 checks) | pass (6 of 6 checks) | not run | not run |
| P3 PostgreSQL failing hook -> reload_reread | pass (2 of 2 checks) | pass (2 of 2 checks) | not run | not run |
| P3 hook failing before the signal -> reload_not_reread | pass (3 of 3 checks) | pass (3 of 3 checks) | not run | not run |
| P3 reload of a stopped Postfix and Dovecot -> 409 not_running | pass (4 of 4 checks) | pass (4 of 4 checks) | not run | not run |
| P3b Postfix Stop with a refused main.cf -> success, master gone | pass (1 of 1 checks) | pass (1 of 1 checks) | not run | not run |
| P4 archive with the directory member imports completely | pass (1 of 1 checks) | pass (1 of 1 checks) | FAIL (0 of 1 checks) | pass (1 of 1 checks) |
| P4 failed files step -> 200 partial, IMPORT_PARTIAL, truthful lists | pass (5 of 5 checks) | pass (5 of 5 checks) | FAIL (0 of 5 checks) | pass (5 of 5 checks) |
| P4 hostile members refused, nothing written outside | pass (6 of 6 checks) | pass (6 of 6 checks) | FAIL (3 of 6 checks) | pass (6 of 6 checks) |
| P5 PHP site created, PHP executed, site deleted | pass (5 of 5 checks) | pass (5 of 5 checks) | FAIL (0 of 1 checks) | pass (5 of 5 checks) |
| P5 the import completes | pass (7 of 7 checks) | pass (7 of 7 checks) | FAIL (3 of 7 checks) | pass (7 of 7 checks) |
| O9 CA unreachable -> 502 CERTIFICATE_ISSUE_FAILED authority_unreachable | pass (1 of 1 checks) | pass (1 of 1 checks) | pass (1 of 1 checks) | pass (1 of 1 checks) |
| O11 no server_setup_busy refusal in the service-action section | pass (1 of 1 checks) | pass (1 of 1 checks) | not run | not run |
| O10 sent database password not echoed; replay byte-identical | pass (4 of 4 checks) | FAIL (2 of 4 checks) | pass (4 of 4 checks) | pass (4 of 4 checks) |
| O14 MariaDB version shown is the server's | pass (2 of 2 checks) | pass (2 of 2 checks) | pass (2 of 2 checks) | pass (2 of 2 checks) |

How to read it:

- **Debian 13 and Ubuntu 24.04: every correction is measured as made.** The Debian column is the re-run after the
  harness fix H40 (`rid3-debian13/run-b`: every section passed; run-a is kept and differs only in the two H40 checks).
  The Ubuntu column is run-a, which was not re-run: its "FAIL" of O10 is H40, not the product. The check compared the
  new database's name on the engine with the name that was sent (`srvsent`), and the engine's name carries the
  subscription prefix (`s2_srvsent`). The facts that check recorded on Ubuntu are the expected ones on both engines:
  first answer `200` without a `password` field and with `password_set: true`, the sent value not in the answer's
  bytes, the replay `200` with `X-CelikPanel-Request-Replayed: 1` and the same bytes, exactly one new database. With
  the corrected check O10 passed on Debian 13 (run-b) and on Arch (4 of 4 each).
- **Arch: S1 (no hash-shaped value), O9, O10 and O14 pass; P5 does not.** A PHP site still cannot be created there, so
  no import starts (finding P5b). Every other "FAIL" in the Arch column is that one defect seen through another
  check (the import's arrivals, the archive with the directory member, the files step of the hostile archives, which
  never ran). Mail is not supported on Arch, so the mailbox login, and groups A and B, are not run there.
- The last column is a **second reading on Arch with one recorded owner action** (the owner placed by hand the one
  file whose absence stops the site creation); it is not the item's result. See "Arch, second reading".

Regression (every section of the latest run of each cell):

| Section | Debian 13 | Ubuntu 24.04 | Arch | Arch, second reading with the owner's nginx snippet |
| --- | --- | --- | --- | --- |
| C0-prepare | passed (7 checks, 0 not passed) | passed (7 checks, 0 not passed) | passed (7 checks, 0 not passed) | passed (7 checks, 0 not passed) |
| C1-domain-databases | passed (18 checks, 0 not passed) | passed (18 checks, 0 not passed) | passed (18 checks, 0 not passed) | passed (18 checks, 0 not passed) |
| C2-admin-account | passed (18 checks, 0 not passed) | passed (18 checks, 0 not passed) | passed (18 checks, 0 not passed) | passed (18 checks, 0 not passed) |
| C3-server-databases | passed (22 checks, 0 not passed) | failed (22 checks, 2 not passed) | passed (22 checks, 0 not passed) | passed (22 checks, 0 not passed) |
| C4-backup | passed (9 checks, 0 not passed) | passed (9 checks, 0 not passed) | passed (9 checks, 0 not passed) | passed (9 checks, 0 not passed) |
| C5-letsencrypt | passed (11 checks, 0 not passed) | passed (11 checks, 0 not passed) | passed (11 checks, 0 not passed) | passed (11 checks, 0 not passed) |
| C6-vpn-peer | passed (8 checks, 0 not passed) | passed (8 checks, 0 not passed) | passed (8 checks, 0 not passed) | passed (8 checks, 0 not passed) |
| C6b-php-site | passed (5 checks, 0 not passed) | passed (5 checks, 0 not passed) | failed (1 checks, 1 not passed) | passed (5 checks, 0 not passed) |
| C7-import | passed (20 checks, 0 not passed) | passed (20 checks, 0 not passed) | failed (20 checks, 5 not passed) | passed (20 checks, 0 not passed) |
| C7b-import-answers | passed (23 checks, 0 not passed) | passed (23 checks, 0 not passed) | failed (17 checks, 8 not passed) | passed (17 checks, 0 not passed) |
| C8-restore | passed (24 checks, 0 not passed) | passed (24 checks, 0 not passed) | passed (24 checks, 0 not passed) | passed (24 checks, 0 not passed) |
| C9-identities | passed (9 checks, 0 not passed) | passed (9 checks, 0 not passed) | passed (9 checks, 0 not passed) | passed (9 checks, 0 not passed) |
| S1-cron | passed (35 checks, 0 not passed) | passed (35 checks, 0 not passed) | not run | not run |
| S2-mail-policy | passed (38 checks, 0 not passed) | passed (38 checks, 0 not passed) | not run | not run |
| S3-backup-schedule | passed (10 checks, 0 not passed) | passed (10 checks, 0 not passed) | not run | not run |
| S4-postgresql | passed (37 checks, 0 not passed) | passed (37 checks, 0 not passed) | not run | not run |
| S5-mariadb | passed (21 checks, 0 not passed) | passed (21 checks, 0 not passed) | not run | not run |
| S6-catchall-queue | passed (15 checks, 0 not passed) | passed (15 checks, 0 not passed) | not run | not run |
| S7-file-metadata | passed (9 checks, 0 not passed) | passed (9 checks, 0 not passed) | not run | not run |
| S8-service-actions | passed (65 checks, 0 not passed) | passed (65 checks, 0 not passed) | not run | not run |

### S1: no hash-shaped value in the preview, in any answer of a guarded route, in any stored row or in the journal; the imported mailbox keeps its password

- The fixture changed: every archive's mailbox (`homedir/etc/<domain>/shadow`) now carries the sha512-crypt hash of
  one password the lab chose per cell (made on the guest by `openssl passwd -6`; neither the password nor its hash
  is written anywhere in the evidence, and the base64 form handed to the guest helper is registered with the redactor).
- `POST /api/v1/import/cpanel/inspect`, 7 previews per cell (the four import archives and the three hostile ones): `200`;
  keys `databases, dns_zones, domains, forwarders, mail_accounts, main_domain, public_html, site_bytes, username`; a
  mailbox is exactly `{"domain": "...", "user": "info", "quota_mb": 256, "has_password": true}`. The driver searches
  the answer's **raw bytes** for hash-shaped values before anything is recorded: **0** in every preview on all three
  platforms; no `crypt_hash` key. Only the four named fields are stored (`previews` in
  `steps/NN-c7-import/section.json` and `steps/NN-c7b-import-answers/section.json`).
- Every answer of a guarded route (105 per cell) was searched the same way: 0 hash-shaped values, 0 answers that carry
  a secret their own request sent. `request_identities` at the end of each cell: 35 rows (34 `done`, 1 `interrupted`,
  the killed restore), **no row with a hash-shaped value** in its stored answer or in any other column (the guest
  helper counts; it never prints a stored body). The Panel's and the Agent's journal lines of the cell (374-392
  lines): 0 hash-shaped values.
- **The imported mailbox authenticates with its original password** (Debian 13 and Ubuntu 24.04, three imported
  mailboxes each: `info@set2-import-tar.test`, `-seq`, `-drop`): a real IMAP `LOGIN` on `127.0.0.1:993` answers `OK`
  (`logged_in: true` on both platforms); the `INBOX` selection after the login was recorded as successful on Ubuntu
  (`inbox_selected: true`) and as not selected on Debian (`inbox_selected: false`, no `select_error` field), and it
  was not a checked condition; `doveadm auth test -x service=imap` exits 0. Beside each, a password that is certainly
  wrong: IMAP `LOGIN` refused, `doveadm auth test` exit 77. Dovecot 2.4.1 (Debian) and 2.3.21 (Ubuntu).
  The import's own step line: "1 accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)".

### P3 and P3b: service actions (S8, `POST /api/v1/service/action`; Debian 13 and Ubuntu 24.04 alike)

42 actions per platform (set2's 41 and one new), every answer matches what the service shows, none is answered as
unknown (`steps/15-s8-service-actions/section.json`, `journal/`, `native-text/`).

| Situation | Action | Answer | Native |
| --- | --- | --- | --- |
| Postfix stopped | reload | **`409 SERVICE_ACTION_FAILED` / `not_running`**, `vars.detail` "Postfix is not running; nothing was reloaded", `command: sudo postfix status` | the master was not running before and is not running after; nothing was started |
| Dovecot stopped | reload | **`409` / `not_running`**, "Dovecot is not running; nothing was reloaded", `command: sudo systemctl status dovecot` | stopped before and after |
| PostgreSQL, the owner's hook signals the server and then fails | reload | **`502 SERVICE_ACTION_FAILED` / `reload_reread`**, `vars.detail` "the reload of postgresql@17-main.service was reported as failed (exit-code), but PostgreSQL re-read its configuration files after it: pg_conf_load_time() moved" (`@16-main` on Ubuntu) | `pg_conf_load_time()` moved, same postmaster PID, the instance's `ReloadResult=exit-code` |
| PostgreSQL, the owner's hook fails **before** it signals the server (new) | reload | **`502` / `reload_not_reread`**, "the reload of postgresql@17-main.service failed (exit-code) and PostgreSQL did not re-read its configuration files: pg_conf_load_time() did not move" | `pg_conf_load_time()` unchanged, same postmaster PID, `ReloadResult=exit-code` |
| Postfix, `main.cf` holds a line its own check refuses | stop | **`200`**, `success: true`, `outcome: verified`, `applied: stopped` (set2: `502 SERVICE_ACTION_UNKNOWN`) | the master's PID file names no running master; the unit is left `failed` as in set2 |
| the same | reload, restart, start | `502` / `check` with Postfix's fatal line, as in set2 | nothing was sent |

The second hook is realistic and was built for this run: the owner's `ExecReload` script reloads a connection pooler
first (`systemctl reload set1-owner-pooler.service || exit 1`, a unit that does not exist) and signals the server only
when that worked (`native/*owner-postgresql-reload-hook-before-signal.json` holds the script).

Unchanged and recorded: a Reload of a stopped **nginx** and of the stopped **PostgreSQL wrapper** still answers `502` /
`command` with systemd's own line ("postgresql.service is not active, cannot reload."), and MariaDB's Reload answers
`502` / `command` ("Job type reload is not applicable"). The contract's `not_running` for "a wrapper with no running
unit behind it" was therefore not produced by this sequence on either platform: systemd refuses the wrapper's reload
before the Agent's own check is asked (observation O16).

### O11: no `server_setup_busy` while the setup waits at `access_dns`

Counted over the whole service-action section: **0 refusals in 42 actions** on Debian 13 and **0 in 42** on Ubuntu
24.04 (`busy_refusals` in the S8 section; the setup of both cells was waiting at `access_dns`,
`server_setup_access_dns_required`, for the whole section). set2 had 1 on each platform.

### P4: the import's archive handling (C7 and C7b; Debian 13 and Ubuntu 24.04)

- **Every archive of this run holds the directory member `homedir/public_html/` as tar writes it** (set2 had to leave
  it out of the arrivals' archives, H34). All of them import completely: the dedicated archive (`tar`), the
  sequential and the concurrent arrivals and the dropped one answer `200`, `status: active`, `domain_status: active`,
  `imported: [domain, files, mail, forwarders, dns, database:<name>]`, `not_imported: []`; the document root's files
  are **equal to the archive's by name, size and SHA-256** (2 files; 3 with the 16 MiB file of the dropped import),
  the mailbox, the forwarder and the 300 rows are there.
- **A failed files step answers `200` with `status: partial`.** Archive with a member
  `homedir/public_html/../../set3-escape-dotdot.txt`, imported with files, mail and databases: `200`, `status: partial`,
  `code: IMPORT_PARTIAL`, `domain_status: pending`, `imported: [domain, mail, forwarders, dns, database:s3hdd_app]`,
  `not_imported: [files]`, step `files`: "unsafe cpmove member path". Natively the lists are true: the domain row is
  `pending`, the mailbox and the forwarder exist, the database holds its 300 rows, and the document root holds only
  the new site's own `index.php` (none of the archive's files). The same identity again: the stored answer byte for
  byte, nothing imported again. Never `202`, never `pending` as the import's status.
- **Hostile members are never extracted.** After each of the three imports the guest was searched for any entry named
  `set3-escape*` outside the import directory (`/var/www`, `/var/lib`, `/var/tmp`, `/var/backups`, `/etc`, `/tmp`,
  `/home`, `/root`, `/srv`, `/opt`, `/usr/local`): **none**, no link in any document root, no stage directory left in a
  site's home. `..` path: files step refused ("unsafe cpmove member path"). Symbolic link `set3-escape-link -> /etc`
  with a file named through it: files step refused ("unsupported cpmove site entry type"), answer `200` / `partial`,
  `imported: [domain, dns]`, `not_imported: [files]`. Absolute path `/etc/set3-escape-absolute.txt`: **not refused but
  left out** (it is not site payload): the import answers `200` / `active` with the archive's own two files in the
  document root and nothing at `/etc/set3-escape-absolute.txt`; the answer does not mention the member (observation
  O17).

Message of the partial answer (EN, API): "The import ended with a part of the archive not imported, and it does not
continue by itself. Imported: domain, mail, forwarders, dns, database:s3hdd_app. Not imported: files. The domain
set3-hostile-dotdot.test was created and is kept; it is left marked as not finished. The reason of each part that
was not imported is in its step below. The server owner either adds the missing parts by hand on the domain's own
pages, or removes set3-hostile-dotdot.test on the Domains page, corrects what the step names and imports the archive
again; an import into a domain that already exists is refused, so nothing is imported twice."

### P5: a PHP site (C6b)

| | Debian 13 | Ubuntu 24.04 | Arch (run-a) |
| --- | --- | --- | --- |
| `POST /api/v1/domains/create` (`project_type: php`) | `200` | `200` | **`500 INTERNAL` "internal server error"** |
| `GET /set3-probe.php` through nginx on loopback | `200`, `set3-php-executed:42:8.4.26:fpm-fcgi:set3_php_test` | `200`, `set3-php-executed:42:8.3.6:fpm-fcgi:set3_php_test` | not reached |
| PHP-FPM unit | `php8.4-fpm.service` | `php8.3-fpm.service` | `php-fpm.service` (`/usr/lib/systemd/system/php-fpm.service`, `ProtectSystem=full`), PHP 8.5.11 |
| Pool directory, the site's pool file | `/etc/php/8.4/fpm/pool.d/site2.conf` | `/etc/php/8.3/fpm/pool.d/site2.conf` | `/etc/php/php-fpm.d/` (only the stock `www.conf` after the failed creation) |
| Socket, owner, mode | `/var/run/php/php8.4-fpm-site2.sock`, `www-data:www-data`, `0660` | `/var/run/php/php8.3-fpm-site2.sock`, `www-data:www-data`, `0660` | stock socket `/run/php-fpm/php-fpm.sock`, `http:http`, `0660`; no site socket |
| `DELETE /api/v1/domains/{id}` | `200`; not listed after 1.0 s; no row, no account, no pool file, the page answers `404`, PHP-FPM active | the same (1.5 s) | not reached |

The page prints a marker, `6 * 7`, `PHP_VERSION`, `php_sapi_name()` and the account it runs as; served as text it
would show its source. It ran as the site's own account on both platforms.


On **Arch** the corrected part works and the next one does not (finding P5b): the pool file is now written under
`/etc/php/php-fpm.d/` and `php-fpm`'s own configuration test passes twice ("configuration file /etc/php/php-fpm.conf
test is successful"), then the Agent's nginx activation fails: `open() "/etc/nginx/snippets/fastcgi-php.conf" failed
(2: No such file or directory) in /etc/nginx/sites-enabled/set3-php.test.conf:27`, the previous vhost is restored and
reloaded, the site account is removed again (`userdel` in the journal), and the Panel answers `500 INTERNAL`
"internal server error" (`[500] site creation failed: site provisioning failed during nginx vhost activation`). The
import, which creates a PHP site first, answers the new typed `502 IMPORT_SITE_NOT_CREATED` on every arrival and
imports nothing (no domain row, no account, no database), which is what that answer says.

### O9, O10, O14 (all three platforms)

- **O9.** `POST /api/v1/domains/{id}/ssl/letsencrypt` with the two directory names resolving to the guest's own
  loopback: **`502 CERTIFICATE_ISSUE_FAILED`, reason `authority_unreachable`** on Debian 13, Ubuntu 24.04 and Arch
  (set2: `500 INTERNAL`). `vars` = `{"domain": "set1-owner.test", "kept": "none", "detail": "An unexpected error
  occurred: requests.exceptions.SSLError: HTTPSConnectionPool(host='acme-v02.api.letsencrypt.org', port=443): Max
  retries exceeded with url: /directory (Caused by SSLError(SSLCertVerificationError(... Hostname mismatch ..."}`
  (certbot met this guest's own listener). The answer was replayed byte for byte for one identity (the guard's four
  arrivals pass on all three platforms, Arch included: set2's H39 is gone); no certificate row was written. API
  sentence: "No certificate was issued: this server could not reach the certificate authority, so no request was
  placed with it. The server owner checks that this server can open HTTPS connections to the internet (DNS
  resolution, outbound port 443, the system clock), then requests the certificate here again. The site has no
  certificate from this request and is served as before. Nothing asks again automatically."
- **O10.** `POST /api/v1/database-servers/{id}/databases` with `new_username` and a `new_password` the owner sent, on
  MariaDB and on PostgreSQL: `200`, keys `created_at, id, name, password_set, user`; **no `password` field,
  `password_set: true`, the sent value does not occur in the answer's bytes**; the same identity again: `200`,
  `X-CelikPanel-Request-Replayed: 1`, **the same bytes** (SHA-256 of the raw answers equal), and one new database on
  the engine (`s2_srvsent`). A request without a password still gets a minted one once and the status-only refusal on
  replay, as in set2. No guarded answer of any cell contained a secret its own request had sent (105 answers each).
- **O14.** `GET /api/v1/database-servers` beside the engines' own answers (`SELECT VERSION()`, `mariadbd --version`,
  the client's `--version`, `SHOW server_version`), read when the engines were registered (C0) and again after the
  Panel's account was provisioned (C3); the page's value was the same both times:

  | | Databases page (MariaDB) | `SELECT VERSION()` | client program | Databases page (PostgreSQL) | `SHOW server_version` |
  | --- | --- | --- | --- | --- | --- |
  | Debian 13 | `11.8.6-MariaDB-0+deb13u1 from Debian` | `11.8.6-MariaDB-0+deb13u1 from Debian` | client 15.2 | `17.11 (Debian 17.11-0+deb13u1)` | `17.11 (Debian 17.11-0+deb13u1)` |
  | Ubuntu 24.04 | `10.11.14-MariaDB-0ubuntu0.24.04.1` (set2: `15.1`) | `10.11.14-MariaDB-0ubuntu0.24.04.1` | `Ver 15.1 Distrib 10.11.14-MariaDB` | `16.15 (Ubuntu 16.15-0ubuntu0.24.04.1)` | `16.15 (Ubuntu 16.15-0ubuntu0.24.04.1)` |
  | Arch | `13.0.2-MariaDB` (set2: the literal `VERSION()`) | `13.0.2-MariaDB` | client 15.2 | `18.6` | `18.6` |

### Arch, second reading: with the one file the owner placed by hand (`rid3-arch/run-b`)

Not the item's result, and not a re-run for a harness defect: the same cell on a new guest, with **one recorded owner action** before C6b (`SET3_OWNER_NGINX_PHP_SNIPPET=1`, helper mode `owner-nginx-php-snippet`): the owner creates `/etc/nginx/snippets/` and places `fastcgi-php.conf` with the text Debian's nginx package ships (SHA-256 `a9dd98bf...`; Arch's own `/etc/nginx/fastcgi.conf`, which it includes, exists). It was run to learn what lies behind P5b, which the contract left open ("whether a PHP page executes on Arch under the packaged unit's hardening, and whether `/run/php-fpm` is where a site's socket may live there").

With that one file in place **every section of the cell passed** (`complete-for-review`):

- `POST /api/v1/domains/create` (PHP): `200`. `GET /set3-probe.php` through nginx: `200`, `set3-php-executed:42:8.5.11:fpm-fcgi:set3_php_test` (`X-Powered-By: PHP/8.5.11`): PHP executed under the packaged `php-fpm.service` (`/usr/lib/systemd/system/php-fpm.service`, `ProtectSystem=full`, `ProtectHome=no`), as the site's own account.
- Native: pool file `/etc/php/php-fpm.d/site2.conf` (`root:celikpanel 0644`; `user = set3_php_test`, `listen.owner = http`, `listen.group = http`, `listen.mode = 0660`); socket **`/run/php-fpm/php8.3-fpm-site2.sock`**, `http:http 0660`; `/run/php-fpm` is `root:root 0755`. The socket's name says `8.3` on a host whose only PHP is 8.5.11 (observation O21).
- `DELETE /api/v1/domains/{id}`: `200`; not listed after 1.1 s; no domain row, no account, no pool file, no socket; the page answers `403`; `php-fpm.service` active.
- The import completes: the archive with the directory member `200` / `active` with the document root's files equal to the archive's; the sequential, concurrent and dropped arrivals pass (i, ii, iii, iv, v); the `..` and the symbolic-link archives answer `200` / `partial` / `IMPORT_PARTIAL` with `not_imported: [files]`, nothing outside; the absolute member is left out. O9, O10, O14 and the 35 rows as in run-a.

So on this Arch guest the one missing file is the whole of P5b; nothing else stopped a PHP site or an import.

## Part 2: the owner-started update from the published v0.1.0-alpha.81 to the candidate

`owner_update_trial.py`, fourteenth run of the kind, the first from the published **alpha.81**. In every cell the
tag's own installer installs the tag build (`version=v0.1.0-alpha.81 commit=a0beb7263...`, ledger 42), the owner logs
in, the acceptance licence is accepted without any licence service, the owner's setup runs to the isolated host's
`access_dns` wait (observed; no certificate step runs, so no certificate authority is asked), a site, a mailbox
(Debian, Ubuntu), a cron job and an owner database are seeded, and the owner starts the update to
`v0.1.0-alpha.82 / 82` from the Panel's own update route. The fixture origin on the guest's loopback serves the
candidate signed with the per-lab fixture key.

| Run | Wrapper (UTC) | Overall | Outcome | Final state | Steps not passed |
| --- | --- | --- | --- | --- | --- |
| part2-alpha81/upd1-arch-defective/run-a | 07:42:27-07:53:04 | complete-for-review | recovered-automatically | recovered/rollback_verified | - |
| part2-alpha81/upd1-arch-good/run-a | 07:33:24-07:42:25 | complete-for-review | update-verified | succeeded/update_verified | - |
| part2-alpha81/upd1-debian13-defective/run-a | 07:40:43-07:53:54 | complete-for-review | recovered-automatically | recovered/rollback_verified | - |
| part2-alpha81/upd1-debian13-good/run-a | 07:27:38-07:40:40 | complete-for-review | update-verified | succeeded/update_verified | - |
| part2-alpha81/upd1-debian13-mgmt-off-reboot/run-a | 07:53:07-08:08:20 | complete-for-review | update-verified | succeeded/update_verified | - |
| part2-alpha81/upd1-debian13-owner-continuation/run-a | 08:08:04-08:28:28 | complete-for-review | recovered-after-owner-continuation | succeeded/update_verified | - |
| part2-alpha81/upd1-debian13-startcheck/run-a | 07:53:57-08:08:01 | complete-for-review | recovered-automatically | recovered/rollback_verified | - |
| part2-alpha81/upd1-ubuntu-defective/run-a | 07:48:54-08:10:00 | complete-for-review | recovered-automatically | recovered/rollback_verified | - |
| part2-alpha81/upd1-ubuntu-good/run-a | 07:28:10-07:48:51 | complete-for-review | update-verified | succeeded/update_verified | - |
| part2-alpha81/upd1-ubuntu-owner-continuation/run-a | 08:10:04-08:37:45 | complete-for-review | recovered-after-owner-continuation | succeeded/update_verified | - |

Terminal checks of every complete run (`summary-per-cell.txt`, `steps/NN-terminal/step.json`): installed = running for
Panel and Agent with the expected identity (the candidate after a forward end, `v0.1.0-alpha.81 / a0beb7263...` after a
return), fresh owner login, timers and `table inet celikpanel_fw` equal, site marker, mailbox and SMTP on Debian and
Ubuntu, the seeded cron row. The site, SMTP and cron were never interrupted except by a cell's own VM reset or
orderly reboot (`steps/NN-verdicts/step.json`).

### Good update (Debian 13, Ubuntu 24.04, Arch): verified, and what the owner meets afterwards

The update is verified about a minute after the owner's start (Debian: start 07:38:08Z, `succeeded/update_verified`
07:39:07Z; Arch 07:41:10Z / 07:42:08Z; Ubuntu 07:46:13Z / 07:47:13Z). Panel down 20-35 s. **With an alpha.81 baseline the root
CLI and the Panel's recovery reader exist from the first second**: the first sample after the start already reads
`known running/update_running` on the CLI and `accepted` on the reader (the alpha.80 gaps of upd7/upd13 - no CLI for
about 10 s, then `observation_unavailable` - do not occur); while the Panel is down the CLI alone answers
(`steps/11-track/samples/`).

The facts measured after the verified update (step `post-update-facts`; generated from the cells, `part2-table.md`):

#### part2-alpha81/upd1-arch-good/run-a: after the verified update (passed)
- (a) ledger 43 (43 rows), verdict as-expected; facts {"integrity": true, "ledger_is_the_released_one": true, "request_identities": true, "schema_is_the_released_one": true, "version": true}; `request_identities` {"columns": ["id", "actor_user_id", "method", "route", "request_sha256", "status", "response_status", "response_retained", "response_content_type", "response_body", "created_at", "finished_at", "expir
- (b) without the header: {"code": "REQUEST_ID_REQUIRED", "error": "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing was changed. Reload the page, then make the change again. (A client that is not the CelikPanel page sends the header X-CelikPanel-Request-Id: 32 lowercase hexadecimal characters, a new value for each action.)", "http": 428, "screen_text": {"en": "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing was changed. Reload the page, then make the change again.", "tr": "Bu sayfa CelikPanel güncellenmeden önce açılmış; bu yüzden sunucu değişikliği kabul etmedi ve hiçbir şey değiştirilmedi. Sayfayı yeniden yükleyin, sonra değişikliği yeniden yapın."}}; archives before/after/after-with-header [0, 0, 1]; with the header {"backup": "full-20261009T074221.085066988Z-4f67c7d7ca92279e.cpbak", "code": null, "error": null, "http": 200}; row {"response_retained": 1, "response_status": 200, "route": "/api/v1/domains/{id}/backups", "status": "done"}; refused and nothing changed: True; works with the header: True
- (c) backup_schedule: ok=True measured=True version bs1-a451686b962008db0e385f50feb14a1922948fda32d1f34fccc0688d7752a682 -> bs1-4f75b6db4a759e7a59f7dd36b346af84183b80ba887d440bf303357b5b217f89; without a version: {"code": "SETTINGS_VERSION_REQUIRED", "error": "This request did not say which settings it was built from, so nothing was changed. Reload the page so it reads the current settings, then make the change again.", "http": 409, "reason": "backup_schedule"}; with: http 200; 
- (c) cron: ok=True measured=True version ct1-6c9e025a95e461ac5a888e3ff347c496a910cf2756ea42f5207c7f724504e7c9 -> ct1-af0653e215e758984720703a7c0f1cbcb64f7839303f71c087aa4f1ed964e28d; without a version: {"code": "SETTINGS_VERSION_REQUIRED", "error": "This request did not say which settings it was built from, so nothing was changed. Reload the page so it reads the current settings, then make the change again.", "http": 409, "reason": "scheduled_tasks"}; with: http 200; native_crontab_lines_with_the_command=1
- (c) mail_policy: ok=None measured=False version None -> None; without a version: null; with: http None; reason=mail is not part of this cell's platform
- (d) deferred mail: {"measured": false, "reason": "no mail stack on this platform"}
- (e) root CLI ['succeeded', 'update_verified'], recovery reader ['succeeded', 'update_verified'], card judged {"findings": [], "server_message_line": null, "verdict": "as-expected"}

#### part2-alpha81/upd1-debian13-good/run-a: after the verified update (passed)
- (a) ledger 43 (43 rows), verdict as-expected; facts {"integrity": true, "ledger_is_the_released_one": true, "request_identities": true, "schema_is_the_released_one": true, "version": true}; `request_identities` {"columns": ["id", "actor_user_id", "method", "route", "request_sha256", "status", "response_status", "response_retained", "response_content_type", "response_body", "created_at", "finished_at", "expir
- (b) without the header: {"code": "REQUEST_ID_REQUIRED", "error": "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing was changed. Reload the page, then make the change again. (A client that is not the CelikPanel page sends the header X-CelikPanel-Request-Id: 32 lowercase hexadecimal characters, a new value for each action.)", "http": 428, "screen_text": {"en": "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing was changed. Reload the page, then make the change again.", "tr": "Bu sayfa CelikPanel güncellenmeden önce açılmış; bu yüzden sunucu değişikliği kabul etmedi ve hiçbir şey değiştirilmedi. Sayfayı yeniden yükleyin, sonra değişikliği yeniden yapın."}}; archives before/after/after-with-header [0, 0, 1]; with the header {"backup": "full-20261009T074033.735385197Z-5908ba585eab391d.cpbak", "code": null, "error": null, "http": 200}; row {"response_retained": 1, "response_status": 200, "route": "/api/v1/domains/{id}/backups", "status": "done"}; refused and nothing changed: True; works with the header: True
- (c) backup_schedule: ok=True measured=True version bs1-a451686b962008db0e385f50feb14a1922948fda32d1f34fccc0688d7752a682 -> bs1-4f75b6db4a759e7a59f7dd36b346af84183b80ba887d440bf303357b5b217f89; without a version: {"code": "SETTINGS_VERSION_REQUIRED", "error": "This request did not say which settings it was built from, so nothing was changed. Reload the page so it reads the current settings, then make the change again.", "http": 409, "reason": "backup_schedule"}; with: http 200; 
- (c) cron: ok=True measured=True version ct1-6c9e025a95e461ac5a888e3ff347c496a910cf2756ea42f5207c7f724504e7c9 -> ct1-af0653e215e758984720703a7c0f1cbcb64f7839303f71c087aa4f1ed964e28d; without a version: {"code": "SETTINGS_VERSION_REQUIRED", "error": "This request did not say which settings it was built from, so nothing was changed. Reload the page so it reads the current settings, then make the change again.", "http": 409, "reason": "scheduled_tasks"}; with: http 200; native_crontab_lines_with_the_command=1
- (c) mail_policy: ok=True measured=True version mp1-d66482b5c93bee71398427c4cd13255aa7a00375dcd7942d90e3a015bd30f0cc -> mp1-5b786b06cee5ee2104d7c2048495b719821b157c70faefdaf46602a04d673aa9; without a version: {"code": "SETTINGS_VERSION_REQUIRED", "error": "This request did not say which settings it was built from, so nothing was changed. Reload the page so it reads the current settings, then make the change again.", "http": 409, "reason": "mail_policy"}; with: http 200; native_postconf=37
- (d) deferred mail: {"completed": true, "last": {"deferred": ["mail certificate publication", "mail filter wiring"], "finished": true, "gave_up": false, "pid": "37522", "ready_at": "2026-10-09T07:38:56.742156+00:00", "repeated": [], "resolved": {"mail certificate publication": [{"at": "2026-10-09T07:39:38.303989+00:00", "attempt": 1, "outcome": "completed"}], "mail filter wiring": [{"at": "2026-10-09T07:39:38.303989+00:00", "attempt": 1, "outcome": "completed"}]}, "started_at": "2026-10-09T07:38:56.604980+00:00"}, "measured": true, "source": "the deferred-mail-watch step of this cell"}
- (e) root CLI ['succeeded', 'update_verified'], recovery reader ['succeeded', 'update_verified'], card judged {"findings": [], "server_message_line": null, "verdict": "as-expected"}

#### part2-alpha81/upd1-ubuntu-good/run-a: after the verified update (passed)
- (a) ledger 43 (43 rows), verdict as-expected; facts {"integrity": true, "ledger_is_the_released_one": true, "request_identities": true, "schema_is_the_released_one": true, "version": true}; `request_identities` {"columns": ["id", "actor_user_id", "method", "route", "request_sha256", "status", "response_status", "response_retained", "response_content_type", "response_body", "created_at", "finished_at", "expir
- (b) without the header: {"code": "REQUEST_ID_REQUIRED", "error": "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing was changed. Reload the page, then make the change again. (A client that is not the CelikPanel page sends the header X-CelikPanel-Request-Id: 32 lowercase hexadecimal characters, a new value for each action.)", "http": 428, "screen_text": {"en": "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing was changed. Reload the page, then make the change again.", "tr": "Bu sayfa CelikPanel güncellenmeden önce açılmış; bu yüzden sunucu değişikliği kabul etmedi ve hiçbir şey değiştirilmedi. Sayfayı yeniden yükleyin, sonra değişikliği yeniden yapın."}}; archives before/after/after-with-header [0, 0, 1]; with the header {"backup": "full-20261009T074843.735109135Z-68544bfbf638bc7f.cpbak", "code": null, "error": null, "http": 200}; row {"response_retained": 1, "response_status": 200, "route": "/api/v1/domains/{id}/backups", "status": "done"}; refused and nothing changed: True; works with the header: True
- (c) backup_schedule: ok=True measured=True version bs1-a451686b962008db0e385f50feb14a1922948fda32d1f34fccc0688d7752a682 -> bs1-4f75b6db4a759e7a59f7dd36b346af84183b80ba887d440bf303357b5b217f89; without a version: {"code": "SETTINGS_VERSION_REQUIRED", "error": "This request did not say which settings it was built from, so nothing was changed. Reload the page so it reads the current settings, then make the change again.", "http": 409, "reason": "backup_schedule"}; with: http 200; 
- (c) cron: ok=True measured=True version ct1-6c9e025a95e461ac5a888e3ff347c496a910cf2756ea42f5207c7f724504e7c9 -> ct1-af0653e215e758984720703a7c0f1cbcb64f7839303f71c087aa4f1ed964e28d; without a version: {"code": "SETTINGS_VERSION_REQUIRED", "error": "This request did not say which settings it was built from, so nothing was changed. Reload the page so it reads the current settings, then make the change again.", "http": 409, "reason": "scheduled_tasks"}; with: http 200; native_crontab_lines_with_the_command=1
- (c) mail_policy: ok=True measured=True version mp1-d66482b5c93bee71398427c4cd13255aa7a00375dcd7942d90e3a015bd30f0cc -> mp1-5b786b06cee5ee2104d7c2048495b719821b157c70faefdaf46602a04d673aa9; without a version: {"code": "SETTINGS_VERSION_REQUIRED", "error": "This request did not say which settings it was built from, so nothing was changed. Reload the page so it reads the current settings, then make the change again.", "http": 409, "reason": "mail_policy"}; with: http 200; native_postconf=37
- (d) deferred mail: {"completed": true, "last": {"deferred": ["mail certificate publication", "mail filter wiring"], "finished": true, "gave_up": false, "pid": "55198", "ready_at": "2026-10-09T07:47:03.030461+00:00", "repeated": [], "resolved": {"mail certificate publication": [{"at": "2026-10-09T07:47:46.384262+00:00", "attempt": 1, "outcome": "completed"}], "mail filter wiring": [{"at": "2026-10-09T07:47:46.384262+00:00", "attempt": 1, "outcome": "completed"}]}, "started_at": "2026-10-09T07:47:02.916535+00:00"}, "measured": true, "source": "the deferred-mail-watch step of this cell"}
- (e) root CLI ['succeeded', 'update_verified'], recovery reader ['succeeded', 'update_verified'], card judged {"findings": [], "server_message_line": null, "verdict": "as-expected"}

In words, the same on all three platforms:

- **(a)** The ledger is the released 43: 43 contiguous rows whose canonical digest equals the one
  `populated_database.py` pins for schema 43 (`a0b5c424...`), the schema SQL equals the pinned schema 43 (`48cbd3b4...`),
  `request_identities` exists with its 13 columns, `integrity_check` is `ok`.
- **(b)** `POST /api/v1/domains/{id}/backups` sent as a page opened before the update sends it (the session's own
  headers, **no** `X-CelikPanel-Request-Id`): **`428 REQUEST_ID_REQUIRED`** with the reload sentence; no archive was
  written and no `request_identities` row. The same request with the header: `200`, exactly one new archive whose name
  the answer carries, one row `done`.
  EN (API): "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing
  was changed. Reload the page, then make the change again. (A client that is not the CelikPanel page sends the header
  X-CelikPanel-Request-Id: 32 lowercase hexadecimal characters, a new value for each action.)" Screen
  (`err.REQUEST_ID_REQUIRED`), EN: "This page was opened before CelikPanel was updated, so the server did not accept
  the change and nothing was changed. Reload the page, then make the change again." TR: "Bu sayfa CelikPanel
  güncellenmeden önce açılmış; bu yüzden sunucu değişikliği kabul etmedi ve hiçbir şey değiştirilmedi. Sayfayı
  yeniden yükleyin, sonra değişikliği yeniden yapın."
- **(c)** The writes that carry a version work, so Panel and Agent are one release: a scheduled task added (`ct1-`
  version read, sent, new version read back; the line is in the site account's crontab once), the server mail policy
  saved on Debian and Ubuntu (`mp1-`; `applied: reloaded`; `postconf -h smtpd_client_message_rate_limit` = `37`), the
  automatic backup schedule created (`bs1-`; read back enabled, weekly, full, 30). **The same three writes without a
  version - what a page opened before the update sends - answer `409 SETTINGS_VERSION_REQUIRED`** (`scheduled_tasks`,
  `mail_policy`, `backup_schedule`) and change nothing: "This request did not say which settings it was built from, so
  nothing was changed. Reload the page so it reads the current settings, then make the change again."
- **(d)** The Panel's deferred startup mail work completed in its first attempt, once (Debian `panel[37522]` started
  07:38:56.60, attempt line 07:39:38.30; Ubuntu `panel[55198]` 07:47:03.03 / 07:47:46.38), after the operation's
  terminal state, as in upd12/upd13. Arch has no mail stack.
- **(e)** The root CLI and the Panel's recovery reader both read `succeeded/update_verified`; the update card rendered
  from the candidate's own rules shows the verified update (judged `as-expected`). CLI, EN: "The update completed and
  the new version was verified when it finished. Nothing else is needed on the server. Open the panel to check that
  everything works now."

### Migration defect with a second fault: automatic return to alpha.81 (Debian 13, Ubuntu 24.04: VM reset at `payload_restored`; Arch: SIGKILL at `runtime_verified`)

Debian: owner start 07:50:11Z; the candidate's offline migration fails (`CELIKPANEL_UPDATE_FAILURE code=update_failed
state=recovery_required reason=offline panel database migration failed; its original database and work evidence are
preserved`); attempt 1 (`update/active`, rollback) 07:50:59; QMP `system_reset` once at `payload_restored` (new boot,
SSH back after 8.8 s); attempt 2 (`rollback/active`) 07:52:04; `recovered/rollback_verified`, read 07:52:24. Arch:
attempt 1 07:51:49, the recovery child killed at `runtime_verified`, attempt 2 (`rollback/completion`) 07:52:38,
verified 07:52:52. Ubuntu: start 08:06:12, attempt 1 08:07:05, reset (SSH back after 10.6 s), attempt 2 08:08:12,
verified 08:08:36.

After the return (step `post-return-facts`, every defective cell alike):

#### part2-alpha81/upd1-arch-defective/run-a: after the automatic return (passed)
- ledger 42 (42 rows), verdict as-expected; facts {"integrity": true, "ledger_is_the_released_one": true, "request_identities": true, "schema_is_the_released_one": true, "version": true}
- database against the pre-update digest: equal-except-volatile
- root CLI ['recovered', 'rollback_verified'] (previous failure update_failed); recovery reader http 200 ['recovered', 'rollback_verified']
- update status: {"body": {"created_at": "2026-10-09T07:51:10.909875799Z", "found": true, "request_id": "6d5b98387271fe823bfcb132752eaf94", "status": "failed", "summary": "reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=offline panel database migration failed; its original database and work evidence are preserved detail=", "target": {"arch": "amd64", "archive_sha256": "e17ddb7fae3c2227986b2b36f57a631b99ff060401fc0820af236bedae663420", "archive_size": "65870672", "commit": "b11032be780c6b4f3c2edf392af7af754d124083", "os": "linux", "sequence": "82", "version": "v0.1.0-alpha.82"}, "updated_at": "2026-10-09T07:51:48.228398754Z"}, "http": 200}
- update card (rendered from the returned build's own rules): {"keys": ["panelUpdate.outcome.rolledBackTitle", "panelUpdate.outcome.rolledBack", "panelUpdate.outcome.cause.generic", "panelUpdate.outcome.rolledBackNext", "panelUpdate.outcome.rolledBackResume", "panelUpdate.outcome.serverMessage"], "missing_keys": [], "observation_error": null, "role": "baseline", "server_message": "reviewed updater failed: exit status 1: offline panel database migration failed; its original database and work evidence are preserved", "source": {"observation": {"path": "lib/recoveryObservation.ts", "sha256": "3b1c955fd04bdcfe754ca80da5f0a10d85a16caebc435d3e719d2e4e3fd57629"}, "outcome": {"path": "lib/systemUpdateOutcome.ts", "sha256": "7329a488a436f12637f3c46a60521bbe22027de032044c4963f5686b0a83f67c"}, "screen": {"path": "components/RecoveryAccess.tsx", "sha256": "4cc5136c5e24f522cd44c1a9e8cb698c0d7450a90670ffab19bbc03ee74311c0"}, "typed": {"path": "lib/systemUpdateFailure.ts", "sha256": "b12d9a0d2989bd70670e02481159128ba716bb21f15db5084b9631475a610af4"}}, "state": "rolled_back", "texts": {"en": ["Update not completed; previous version restored", "The update to v0.1.0-alpha.82 did not complete. The server was returned to v0.1.0-alpha.81 automatically and is runn
- offered again: {"body": {"available": true, "current_commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "current_version": "v0.1.0-alpha.81", "previous_attempt": {"finished_at": "2026-10-09T07:52:52Z", "phase": "recovered", "request_id": "6d5b98387271fe823bfcb132752eaf94"}, "supported": true, "target": {"arch": "amd64", "archive_sha256": "e17ddb7fae3c2227986b2b36f57a631b99ff060401fc0820af236bedae663420", "archive_size": "65870672", "commit": "b11032be780c6b4f3c2edf392af7af754d124083", "os": "linux", "publish
- version: {"body": {"agent_commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "agent_matches": true, "commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "hostname": "dns-arch", "ipv4": "192.0.2.11", "schema_version": 42, "version": "v0.1.0-alpha.81"}, "http": 200}
- CLI text EN: The update did not complete, and the server was returned automatically to the version it ran before; that restoration was verified. Nothing needs to be done on the server. Do not start the same version again until a corrected version is published; the panel's update page shows what is known about the cause.
Request: 6d5b98387271fe823bfcb132752eaf94
Recorded at: 2026-10-09T07:52:52Z
This is a recorded observation; current service health was not checked.
Recorded state (for support): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed

- CLI text TR: Güncelleme tamamlanmadı ve sunucu otomatik olarak güncellemeden önce çalıştırdığı sürüme döndürüldü; bu geri dönüş doğrulandı. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü yeniden başlatmayın; nedenle ilgili bilinenler panelin güncelleme sayfasında gösterilir.
İşlem: 6d5b98387271fe823bfcb132752eaf94
Kayıt zamanı: 2026-10-09T07:52:52Z
Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi.
Kayıtlı durum (destek için): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed

#### part2-alpha81/upd1-debian13-defective/run-a: after the automatic return (passed)
- ledger 42 (42 rows), verdict as-expected; facts {"integrity": true, "ledger_is_the_released_one": true, "request_identities": true, "schema_is_the_released_one": true, "version": true}
- database against the pre-update digest: equal-except-volatile
- root CLI ['recovered', 'rollback_verified'] (previous failure update_failed); recovery reader http 200 ['recovered', 'rollback_verified']
- update status: {"body": {"created_at": "2026-10-09T07:50:11.879018382Z", "found": true, "request_id": "b6d67cc409404a6f762cedab2e080fd7", "status": "failed", "summary": "reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=offline panel database migration failed; its original database and work evidence are preserved detail=", "target": {"arch": "amd64", "archive_sha256": "e17ddb7fae3c2227986b2b36f57a631b99ff060401fc0820af236bedae663420", "archive_size": "65870672", "commit": "b11032be780c6b4f3c2edf392af7af754d124083", "os": "linux", "sequence": "82", "version": "v0.1.0-alpha.82"}, "updated_at": "2026-10-09T07:50:57.85423299Z"}, "http": 200}
- update card (rendered from the returned build's own rules): {"keys": ["panelUpdate.outcome.rolledBackTitle", "panelUpdate.outcome.rolledBack", "panelUpdate.outcome.cause.generic", "panelUpdate.outcome.rolledBackNext", "panelUpdate.outcome.rolledBackResume", "panelUpdate.outcome.serverMessage"], "missing_keys": [], "observation_error": null, "role": "baseline", "server_message": "reviewed updater failed: exit status 1: offline panel database migration failed; its original database and work evidence are preserved", "source": {"observation": {"path": "lib/recoveryObservation.ts", "sha256": "3b1c955fd04bdcfe754ca80da5f0a10d85a16caebc435d3e719d2e4e3fd57629"}, "outcome": {"path": "lib/systemUpdateOutcome.ts", "sha256": "7329a488a436f12637f3c46a60521bbe22027de032044c4963f5686b0a83f67c"}, "screen": {"path": "components/RecoveryAccess.tsx", "sha256": "4cc5136c5e24f522cd44c1a9e8cb698c0d7450a90670ffab19bbc03ee74311c0"}, "typed": {"path": "lib/systemUpdateFailure.ts", "sha256": "b12d9a0d2989bd70670e02481159128ba716bb21f15db5084b9631475a610af4"}}, "state": "rolled_back", "texts": {"en": ["Update not completed; previous version restored", "The update to v0.1.0-alpha.82 did not complete. The server was returned to v0.1.0-alpha.81 automatically and is runn
- offered again: {"body": {"available": true, "current_commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "current_version": "v0.1.0-alpha.81", "previous_attempt": {"finished_at": "2026-10-09T07:52:24Z", "phase": "recovered", "request_id": "b6d67cc409404a6f762cedab2e080fd7"}, "supported": true, "target": {"arch": "amd64", "archive_sha256": "e17ddb7fae3c2227986b2b36f57a631b99ff060401fc0820af236bedae663420", "archive_size": "65870672", "commit": "b11032be780c6b4f3c2edf392af7af754d124083", "os": "linux", "publish
- version: {"body": {"agent_commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "agent_matches": true, "commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "hostname": "dns-debian13", "ipv4": "192.0.2.10", "schema_version": 42, "version": "v0.1.0-alpha.81"}, "http": 200}
- CLI text EN: The update did not complete, and the server was returned automatically to the version it ran before; that restoration was verified. Nothing needs to be done on the server. Do not start the same version again until a corrected version is published; the panel's update page shows what is known about the cause.
Request: b6d67cc409404a6f762cedab2e080fd7
Recorded at: 2026-10-09T07:52:24Z
This is a recorded observation; current service health was not checked.
Recorded state (for support): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed

- CLI text TR: Güncelleme tamamlanmadı ve sunucu otomatik olarak güncellemeden önce çalıştırdığı sürüme döndürüldü; bu geri dönüş doğrulandı. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü yeniden başlatmayın; nedenle ilgili bilinenler panelin güncelleme sayfasında gösterilir.
İşlem: b6d67cc409404a6f762cedab2e080fd7
Kayıt zamanı: 2026-10-09T07:52:24Z
Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi.
Kayıtlı durum (destek için): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed

#### part2-alpha81/upd1-debian13-startcheck/run-a: after the automatic return (passed)
- ledger 42 (42 rows), verdict as-expected; facts {"integrity": true, "ledger_is_the_released_one": true, "request_identities": true, "schema_is_the_released_one": true, "version": true}
- database against the pre-update digest: equal-except-volatile
- root CLI ['recovered', 'rollback_verified'] (previous failure update_failed); recovery reader http 200 ['recovered', 'rollback_verified']
- update status: {"body": {"created_at": "2026-10-09T08:04:14.987470916Z", "found": true, "request_id": "ea084962228e54fabc3c714f62668496", "status": "failed", "summary": "!! CELIKPANEL_UPDATE_FAILURE code=candidate_panel_startup_check_failed state=recovery_required reason= detail=", "target": {"arch": "amd64", "archive_sha256": "af948dc49a5b53dcb8b91212867087195bbd2062d80c32e095c23b7f75d3e53b", "archive_size": "65878529", "commit": "f18a9d416fbc785236891e6645cd53063d8f91ec", "os": "linux", "sequence": "82", "version": "v0.1.0-alpha.82"}, "updated_at": "2026-10-09T08:05:06.878930609Z"}, "http": 200}
- update card (rendered from the returned build's own rules): {"keys": ["panelUpdate.outcome.rolledBackTitle", "panelUpdate.outcome.rolledBack", "panelUpdate.outcome.cause.candidate_panel_startup_check_failed", "panelUpdate.outcome.rolledBackNext", "panelUpdate.outcome.rolledBackResume"], "missing_keys": [], "observation_error": null, "role": "baseline", "server_message": null, "source": {"observation": {"path": "lib/recoveryObservation.ts", "sha256": "3b1c955fd04bdcfe754ca80da5f0a10d85a16caebc435d3e719d2e4e3fd57629"}, "outcome": {"path": "lib/systemUpdateOutcome.ts", "sha256": "7329a488a436f12637f3c46a60521bbe22027de032044c4963f5686b0a83f67c"}, "screen": {"path": "components/RecoveryAccess.tsx", "sha256": "4cc5136c5e24f522cd44c1a9e8cb698c0d7450a90670ffab19bbc03ee74311c0"}, "typed": {"path": "lib/systemUpdateFailure.ts", "sha256": "b12d9a0d2989bd70670e02481159128ba716bb21f15db5084b9631475a610af4"}}, "state": "rolled_back", "texts": {"en": ["Update not completed; previous version restored", "The update to v0.1.0-alpha.82 did not complete. The server was returned to v0.1.0-alpha.81 automatically and is running it now.", "Cause: the new version's panel failed its start check before anything was switched on.", "Nothing needs to be done on the ser
- offered again: {"body": {"available": true, "current_commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "current_version": "v0.1.0-alpha.81", "previous_attempt": {"failure_code": "candidate_panel_startup_check_failed", "finished_at": "2026-10-09T08:06:32Z", "phase": "recovered", "request_id": "ea084962228e54fabc3c714f62668496"}, "supported": true, "target": {"arch": "amd64", "archive_sha256": "af948dc49a5b53dcb8b91212867087195bbd2062d80c32e095c23b7f75d3e53b", "archive_size": "65878529", "commit": "f18a9d416f
- version: {"body": {"agent_commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "agent_matches": true, "commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "hostname": "dns-debian13", "ipv4": "192.0.2.10", "schema_version": 42, "version": "v0.1.0-alpha.81"}, "http": 200}
- CLI text EN: The new version's panel failed its start check before anything was switched on, so the server was returned to the previous version automatically. The previous version keeps running. Nothing needs to be done on the server. Do not start the same version again until a corrected version is published. When you report this, include the reason line shown for this update on the panel's update page.
Request: ea084962228e54fabc3c714f62668496
Recorded at: 2026-10-09T08:06:32Z
This is a recorded observation; current service health was not checked.
Recorded state (for support): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed failure_code=candidate_panel_startup_check_failed

- CLI text TR: Yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi; bu yüzden sunucu otomatik olarak önceki sürüme döndürüldü. Önceki sürüm çalışmaya devam ediyor. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü yeniden başlatmayın. Bunu bildirirken panelin güncelleme sayfasında bu güncelleme için gösterilen neden satırını ekleyin.
İşlem: ea084962228e54fabc3c714f62668496
Kayıt zamanı: 2026-10-09T08:06:32Z
Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi.
Kayıtlı durum (destek için): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed failure_code=candidate_panel_startup_check_failed

#### part2-alpha81/upd1-ubuntu-defective/run-a: after the automatic return (passed)
- ledger 42 (42 rows), verdict as-expected; facts {"integrity": true, "ledger_is_the_released_one": true, "request_identities": true, "schema_is_the_released_one": true, "version": true}
- database against the pre-update digest: equal-except-volatile
- root CLI ['recovered', 'rollback_verified'] (previous failure update_failed); recovery reader http 200 ['recovered', 'rollback_verified']
- update status: {"body": {"created_at": "2026-10-09T08:06:12.752169894Z", "found": true, "request_id": "0318dd07312907bf6a0a04f9df144815", "status": "failed", "summary": "reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=offline panel database migration failed; its original database and work evidence are preserved detail=", "target": {"arch": "amd64", "archive_sha256": "e17ddb7fae3c2227986b2b36f57a631b99ff060401fc0820af236bedae663420", "archive_size": "65870672", "commit": "b11032be780c6b4f3c2edf392af7af754d124083", "os": "linux", "sequence": "82", "version": "v0.1.0-alpha.82"}, "updated_at": "2026-10-09T08:07:03.947220285Z"}, "http": 200}
- update card (rendered from the returned build's own rules): {"keys": ["panelUpdate.outcome.rolledBackTitle", "panelUpdate.outcome.rolledBack", "panelUpdate.outcome.cause.generic", "panelUpdate.outcome.rolledBackNext", "panelUpdate.outcome.rolledBackResume", "panelUpdate.outcome.serverMessage"], "missing_keys": [], "observation_error": null, "role": "baseline", "server_message": "reviewed updater failed: exit status 1: offline panel database migration failed; its original database and work evidence are preserved", "source": {"observation": {"path": "lib/recoveryObservation.ts", "sha256": "3b1c955fd04bdcfe754ca80da5f0a10d85a16caebc435d3e719d2e4e3fd57629"}, "outcome": {"path": "lib/systemUpdateOutcome.ts", "sha256": "7329a488a436f12637f3c46a60521bbe22027de032044c4963f5686b0a83f67c"}, "screen": {"path": "components/RecoveryAccess.tsx", "sha256": "4cc5136c5e24f522cd44c1a9e8cb698c0d7450a90670ffab19bbc03ee74311c0"}, "typed": {"path": "lib/systemUpdateFailure.ts", "sha256": "b12d9a0d2989bd70670e02481159128ba716bb21f15db5084b9631475a610af4"}}, "state": "rolled_back", "texts": {"en": ["Update not completed; previous version restored", "The update to v0.1.0-alpha.82 did not complete. The server was returned to v0.1.0-alpha.81 automatically and is runn
- offered again: {"body": {"available": true, "current_commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "current_version": "v0.1.0-alpha.81", "previous_attempt": {"finished_at": "2026-10-09T08:08:36Z", "phase": "recovered", "request_id": "0318dd07312907bf6a0a04f9df144815"}, "supported": true, "target": {"arch": "amd64", "archive_sha256": "e17ddb7fae3c2227986b2b36f57a631b99ff060401fc0820af236bedae663420", "archive_size": "65870672", "commit": "b11032be780c6b4f3c2edf392af7af754d124083", "os": "linux", "publish
- version: {"body": {"agent_commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "agent_matches": true, "commit": "a0beb7263d1f4ca72258f6b306f9111ba4e2a334", "hostname": "dns-ubuntu", "ipv4": "192.0.2.12", "schema_version": 42, "version": "v0.1.0-alpha.81"}, "http": 200}
- CLI text EN: The update did not complete, and the server was returned automatically to the version it ran before; that restoration was verified. Nothing needs to be done on the server. Do not start the same version again until a corrected version is published; the panel's update page shows what is known about the cause.
Request: 0318dd07312907bf6a0a04f9df144815
Recorded at: 2026-10-09T08:08:36Z
This is a recorded observation; current service health was not checked.
Recorded state (for support): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed

- CLI text TR: Güncelleme tamamlanmadı ve sunucu otomatik olarak güncellemeden önce çalıştırdığı sürüme döndürüldü; bu geri dönüş doğrulandı. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü yeniden başlatmayın; nedenle ilgili bilinenler panelin güncelleme sayfasında gösterilir.
İşlem: 0318dd07312907bf6a0a04f9df144815
Kayıt zamanı: 2026-10-09T08:08:36Z
Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi.
Kayıtlı durum (destek için): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed

- **alpha.81's Panel and Agent serve**: installed = running = `version=v0.1.0-alpha.81 commit=a0beb7263...`;
  `GET /api/v1/panel/version` answers `schema_version: 42`.
- **The ledger is the released 42** (42 contiguous rows, digest `40756295...` = the pinned one; schema SQL = the pinned
  schema 42 `2754d89b...`), **`request_identities` does not exist**, `integrity_check` `ok`. The owner's data is
  intact: all 65 tables compared with the pre-update digests, `equal-except-volatile` (`metrics_samples`,
  `server_setup_executions`), schema digest equal; the seeded domain, mailbox and cron rows are listed.
  In this kind the candidate migrates an **isolated copy** and fails before the database is published, so the live
  database is never at 43; the return of a database that **was** published at 43 is the start-check cell below.
- **alpha.81 has the recovery reader and the outcome card; this is no longer the alpha.80 limitation** (upd7 F1/upd13
  F3). `GET /api/v1/recovery/status` on the returned alpha.81 Panel answers `200` `recovered/rollback_verified`,
  `previous_failure: update_failed`; the root CLI reads the same; the three views agree.
  Update card, rendered from the alpha.81 build's own rules and catalogues (EN): "Update not completed; previous
  version restored || The update to v0.1.0-alpha.82 did not complete. The server was returned to v0.1.0-alpha.81
  automatically and is running it now. || Cause: the update failed before it completed; the server recorded no more
  specific cause. || Nothing needs to be done on the server. Do not start the update to v0.1.0-alpha.82 again until a
  corrected version is published. If the server's message names a problem on this server, fix it first. || Nothing
  resumes by itself: the server keeps running v0.1.0-alpha.81. When a newer version is published, "Check for updates"
  offers it. || The server reported: reviewed updater failed: exit status 1: offline panel database migration failed;
  its original database and work evidence are preserved". The Turkish card is in each cell's
  `steps/NN-post-return-facts/step.json` (`views.update_card.texts.tr`).
  Recovery screen (EN): "Rollback verified || The server verified restoration of the previous release. Reload
  CelikPanel to check panel access. || Previously recorded failure: Update failed".
  Root CLI (EN): "The update did not complete, and the server was returned automatically to the version it ran
  before; that restoration was verified. Nothing needs to be done on the server. Do not start the same version again
  until a corrected version is published; the panel's update page shows what is known about the cause." (TR:
  "Güncelleme tamamlanmadı ve sunucu otomatik olarak güncellemeden önce çalıştırdığı sürüme döndürüldü; bu geri dönüş
  doğrulandı. ..." in `views.cli_text.tr`.)
- Recorded beside it (observation O18): the raw status of the update itself is `failed` with the updater's line as
  `summary`; `GET /api/v1/panel/update/check` still answers `available: true` for the same defective
  `v0.1.0-alpha.82` (with `previous_attempt: {phase: recovered}`), and the card's own sentence tells the owner not to
  start it again; the release floor file stays at `sequence=82 / v0.1.0-alpha.82` after the return, as after every
  earlier rollback.
- The restored alpha.81 Panel's deferred mail work completed in its first attempt (Debian `panel[4733]` 07:52:18.72 /
  07:52:57.50; Ubuntu `panel[6243]` 08:08:29.26 / 08:09:09.13): alpha.81 has the retry.

### Start check (Debian 13): the return of a database that had been published at 43

Candidate S: the shared TLS preparation of the Panel fails, so the read-only start check fails **after** the
candidate's updater has migrated an isolated copy and published it as the live database (`update.sh:2296-2299`:
`panel --migrate-only`, then `publish-update-database`; the start check and the code
`candidate_panel_startup_check_failed` come after it, `update.sh:2380-2384`). Owner start 08:04:14Z; failure line
`code=candidate_panel_startup_check_failed state=recovery_required reason=new panel start check failed before
completion: panel startup check failed: tls_pair_invalid ...` 08:05:06; attempt 1 (`update/active`, rollback) 08:05:08;
VM reset at `payload_restored` (SSH back after 8.6 s); attempt 2 (`rollback/active`) 08:06:12;
`recovered/rollback_verified` 08:06:32, `failure_code` kept. The kind's 12 rules are all as expected (`result.json`
`kind.judged`, among them `rollback-dispatch`, `no-completion-marker`, `database-equal`).

After the return: **the ledger is the released 42, `request_identities` does not exist, and all 65 tables equal the
pre-update digests** except the two volatile ones (`equal-except-volatile`, schema digest equal): the pre-update
snapshot is what serves, and the owner's rows (domain, mailbox, cron) are listed. alpha.81's card names the typed
cause, EN: "Update not completed; previous version restored || The update to v0.1.0-alpha.82 did not complete. The
server was returned to v0.1.0-alpha.81 automatically and is running it now. || Cause: the new version's panel failed
its start check before anything was switched on. || Nothing needs to be done on the server. Do not start the update to
v0.1.0-alpha.82 again until a corrected version is published. ..."; recovery screen: "Rollback verified || The new
version's panel failed its start check before anything was switched on, so the server was returned to the previous
version automatically. The previous version keeps running and nothing needs to be done on the server. When you
report this, include the reason line shown for this update on the update page. || Previously recorded failure: The
new version's panel failed its start check"; root CLI: "The new version's panel failed its start check before
anything was switched on, so the server was returned to the previous version automatically. The previous version
keeps running. Nothing needs to be done on the server. Do not start the same version again until a corrected version
is published. When you report this, include the reason line shown for this update on the panel's update page."

Limit: the live ledger was not read between the publication and the return (the candidate updater's own lines are not
in the collected journals); that the database was at 43 in between follows from the updater's order and the recorded
failure code, not from a reading.

### Owner continuation (Debian 13, Ubuntu 24.04)

Good candidate G; an owner-fixable host cause: a lab process holds `127.0.0.1:2083` from the moment the updater stops
the old Panel, so the new Panel cannot bind, the update pauses after its three forward attempts, and the harness then
does what the product's text tells the owner (free the port, run the one printed retry once).

| | Debian 13 | Ubuntu 24.04 |
| --- | --- | --- |
| Owner start | 08:19:12Z | 08:28:09Z |
| Forward attempts 1, 2, 3 (`update/completion`) | 08:21:12, 08:22:53, 08:24:35 | 08:30:08, 08:31:54, 08:33:36 |
| Pause read on the root CLI (`recovery_required/recovery_incomplete`, `automatic_recovery=paused_retry_limit`, `first_failure_code=panel_start_unverified`, `renewal_before_update=on`) | 08:26:17 | 08:35:19 |
| Port held, released by the owner before the retry | 422.9 s, 08:26:43 | 439.5 s, 08:35:58 |
| The printed command run once, exit 0 (`Recovery dispatch admitted: attempt=owner`, "Previous pending update finalized from verified snapshot") | finished 08:27:10 | finished 08:36:26 |
| End | `succeeded/update_verified`, `previous_failure=recovery_failed` | the same |
| Deferred mail work of the Panel started inside the retry | `panel[57191]` 08:26:55.76, attempt 1 completed 08:27:38.55 | `panel[79975]` 08:36:11.36, completed 08:36:54.63 |

The kind's 17 rules are as expected on both (`result.json` `kind.judged`): the panel log the text names shows `Failed
to start panel listener: listen tcp :2083: bind: address already in use`; the recovery journal prints the one-time
command `/usr/libexec/celikpanel/recovery recover --retry --snapshot <snapshot>`; Certbot's timer is back as before.
Root CLI at the pause, EN: "The update was applied, but the new version's panel did not come up, and completing the
update was retried to its limit. Read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. Retrying
repeats the same start until that cause is fixed. There is no supported return to the previous version from this
point. Automatic recovery used all three attempts without finishing, so the server may be between versions. The
server owner must act: read sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50, fix the cause it
names, then run the one-time same-operation retry command shown there. Checking status does not retry recovery.
Automatic certificate renewal (Certbot) was stopped for this update; the same recovery journal says whether it was
returned to how it was before the update or stays stopped until this operation finishes." The Turkish text is in each
cell's `steps/12-owner-continuation-required/step.json` (`views.cli.texts.tr`). These texts are the candidate's: the
forward completion is run by the candidate's recovery runner. The post-update facts (a)-(e) were not repeated in
these two cells.

### Management off across a reboot (Debian 13)

After the verified update (08:04:13Z) the owner runs `sudo systemctl disable --now celikpanel-panel.service
celikpanel-agent.service` and `sudo systemctl reboot` (new boot, SSH back after 3.8 s). For the 185 s window with both
units `disabled` and `inactive`: the site, the owner's database row and SMTP are served from the first sample of the
new boot (8.0 s after boot), cron wrote 3 stamps in the boot, `certbot.timer` stayed `enabled`/`active`,
`table inet celikpanel_fw` is present and equal, nothing needed the Panel. `systemctl enable --now` brings management
back: the Panel answers `starting` at 08:08:00Z and `ready` at 08:08:12Z (5 reads), login works, no difference. The
kind's 11 rules are as expected. The post-update facts (a)-(e) were not repeated in this cell.

## Findings

### Candidate product defects

- **P5b. Arch: a PHP site still cannot be created, so every cPanel import still fails there; the pool is no longer
  the cause, the nginx vhost is.** `internal/services/templates/nginx/vhost.conf.tmpl:16` includes
  `snippets/fastcgi-php.conf`, a file that Debian's and Ubuntu's nginx packages ship (`nginx-common`) and Arch's nginx
  package does not (it has no `/etc/nginx/snippets`).
  Sequence (rid3-arch run-a): setup with purpose `web` (nginx, php-fpm, mariadb, postgresql: every step succeeded);
  `POST /api/v1/domains/create` `{"domain": "set3-php.test", "project_type": "php", "ssl_type": "none"}`. The Agent
  creates the site account and the pool (now under `/etc/php/php-fpm.d/`; `php-fpm`'s configuration test passes),
  then logs `CreateSite set3-php.test: nginx vhost activation: nginx validation failed: ... [emerg] open()
  "/etc/nginx/snippets/fastcgi-php.conf" failed (2: No such file or directory) in
  /etc/nginx/sites-enabled/set3-php.test.conf:27 ... rollback restored and reloaded the previous vhost`; the account is
  removed again. Answers: the Domains page's create **`500 INTERNAL` "internal server error"**
  (`cmd/panel/domain_handlers.go:487-498`: the orchestrator's error goes to `writeServerError`;
  `internal/services/site_orchestrator.go:246`), with no step and no next action; `POST /api/v1/import/cpanel/apply`
  the new **`502 IMPORT_SITE_NOT_CREATED`** on every arrival, nothing imported. Native after it: no domain row, no
  site account, no pool file of the site, PHP-FPM and nginx still active.
  Evidence: `rid3-arch/run-a/steps/15-c6b-php-site/section.json`, `steps/16-c7-import/section.json`,
  `steps/20-collect/journal-product.txt` (07:30:32Z, 07:30:36Z).
  Second reading (rid3-arch run-b, with `/etc/nginx/snippets/fastcgi-php.conf` placed by the owner by hand, recorded as an owner action): the same create answers `200`, the PHP page executes under the packaged `php-fpm.service`, the site is deleted cleanly and every import completes. The missing file is the whole defect on this guest. Evidence: `rid3-arch/run-b/steps/15-c6b-php-site/section.json`.

### Observations (not judged as defects)

- **O16. A Reload of the stopped PostgreSQL wrapper (and of a stopped nginx) is still answered `502` / `command`**
  with systemd's line ("postgresql.service is not active, cannot reload."), not `409 not_running`: systemd refuses the
  wrapper's reload before the Agent's own "no running unit behind it" reading applies
  (`cmd/agent/service_action_verify.go`, the `command` stage at about line 416 comes before `verifyWrapperAction`).
  The answer is truthful; it is only not the one the contract's sentence names for a wrapper. Debian 13 and Ubuntu
  24.04, `set3-*/run-a/steps/15-s8-service-actions/section.json` (`actions`).
- **O17. An archive member with an absolute path is left out silently.** It is not site payload, so the files step
  skips it, imports the rest and the answer is `200` / `active` without a word about the member. Nothing was written
  at the path. `rid3-debian13`, `rid3-ubuntu` `steps/NN-c7b-import-answers/section.json` (`hostile.absolute`).
- **O18. After an automatic return the update check still offers the same version** (`available: true`, with
  `previous_attempt: {phase: recovered}`); only the card's and the CLI's sentence say not to start it again. The
  release floor file stays at the candidate's sequence after the return. Every defective cell,
  `steps/NN-post-return-facts/step.json`.
- **O19. Postfix is left `failed` after a Stop with a refused `main.cf`** (Debian: `postfix.service` `failed`,
  `Result=exit-code`; Ubuntu: `postfix@-.service`), although the answer is now a truthful success (the master is gone).
  Unchanged from set2's O8 apart from the answer.
- **O20. On Arch the published alpha.81's `web_mail` plan is not attempted by the harness** (as for alpha.80, H18: the
  tag accepts the plan and fails at `05-mail_profile`); the Arch update cells ran with purpose `web`.
- The preview's `site_bytes`, `dns_zones` and `forwarders` were not judged. O2 of set1 and O6, O7, O12, O13 of set2 are
  unchanged, as the contract says.
- **O21. On Arch the site's PHP version is the fallback literal `8.3`** although the host's only PHP is 8.5.11: the pool's socket is `/run/php-fpm/php8.3-fpm-site2.sock` (rid3-arch run-b). Consistent with `cmd/panel/domain_handlers.go:415-418` (when `services.DetectInstalledPHPVersion()` answers nothing the handler takes `"8.3"`); where the value came from was not traced further. The site works; the name is not the host's version. Seen only in the second reading (run-a never got that far).

### Incompatibility with v0.1.0-alpha.81

None was found in the measured paths. A page opened before the update is refused on the eight guarded routes (`428
REQUEST_ID_REQUIRED`) and on every versioned write (`409 SETTINGS_VERSION_REQUIRED`) until it is reloaded: that is the
designed behaviour, measured in every good cell, and it changes nothing on the server. After a return to alpha.81 the
owner has the recovery reader, the outcome card and the root CLI; the alpha.80 limitations (upd7 F1, F2; upd13 F3, F4)
do not apply.


### Platform limitations

- The isolated lab has no certificate authority: the Let's Encrypt route was measured for its guard and for its typed
  failure only. Setup stops at `access_dns`: no panel or mail certificate, no mail enrollment.
- Mail is not supported on Arch: no mailbox login, no groups A and B there, no deferred mail work.
- This host can enter modern standby despite a process-level keep-awake request (it did once, before the cells), and
  WSL cannot start Windows programs here, so the `C:` readings come from a Windows-side watcher.

### Harness

H40 and H41 above and H42. The checks that did not pass are listed, with their detail, in
`checks-not-passed.txt`: on Debian 13 and Ubuntu 24.04 (run-a) the two H40 checks only; on Arch run-a the checks that
see P5b.

## What this run does NOT prove

- Nothing about an installed server, a production licence, production signing, the real release origin or the signed
  alpha.81 archive: the baseline is the tag's source built here with the acceptance licence seam switched on.
- The browser: the driver sends what the screens send; no screen was rendered. Cards and screens are rendered by the
  driver from the build's own rules and catalogues; Turkish sentences are catalogue lookups.
- One run per cell and per sequence. Repeatability is not shown.
- S1: a real cPanel archive (the fixture is the lab's minimal one; mailbox contents are not migrated by the product);
  other hash schemes than sha512-crypt; a mailbox without a password in the archive (`has_password: false`) was not
  built. The scan for hash-shaped values is a scan for the shapes listed in `owner_update_trial.HASH_SHAPED`.
- P3: `reload_reread` and `reload_not_reread` on anything but PostgreSQL's packaged instance unit with the two hooks
  described; `not_running` for a wrapper (O16); Dovecot or nginx with a refused configuration.
- P4: a files step that fails for another reason than a refused member (a full disk, an I/O error); a hard link or a
  device member; the import's own time limit.
- P5: nothing on Arch beyond the failed creation in run-a; what run-b shows is a reading with an owner's file in
  place, not the product's behaviour on a stock Arch host. A PHP version switch, a second PHP version, SSL on the PHP
  site were not run on any platform.
- O9: the other kinds (`authority_refused`, a rate limit, `timeout`, `tool`) and an issuance that succeeds. O11: the
  other DNS waits (`primary_dns`, `infrastructure_dns`), which still refuse by design. O14: a server upgraded in place
  (a version already recorded stays until the Panel's account is provisioned again).
- Part 2: real start, power loss, a second fault during the owner's retry, a cause the owner cannot remove, an update
  started with a page that is still open (the refusals were measured by sending what such a page sends, after the
  update), an update while guarded requests are running, rows of `request_identities` that exist before a return
  (the published alpha.81 cannot write any), the start check and management-off on Ubuntu and Arch, owner
  continuation on Arch.
- That the candidate's `request_identities` table is harmless to lose at a restore is shown only in the sense that the
  returned alpha.81 serves with its ledger at 42; no guarded request was in flight at the return.

## Removals, leftovers, secrets

- Removed, after each cell's evidence was staged and its checksums verified on the staged copy (`host/removals.txt`):
  the overlay disks of this run's labs. Nothing else, and nothing that existed before the run.
- Left on the WSL host (`host/host-leftovers.txt`): `/var/tmp/cp-set3-run` (run copies `harness-p`, `-u`, `-a` to `-e`,
  a mutable development copy `harness-dev` that no cell used, logs, queue), this run's labs without their overlays
  (`/var/tmp/cp-release-drill-set3-*`, `-rid3-*`, `-u14-*`: base image copies, keys, evidence), the three build clones
  `/var/tmp/cp-upd1-build/20261009t061555z`, `...t064831z`, `...t070418z`, and the dist directories of this run's
  fixture commits and of the tag commit under `/var/tmp/cp-pair-accept/dist/`. No QEMU and no job process was running
  at the end; HEAD and the local git configuration are unchanged; nothing was committed.
- `secret-scan.txt` (`tools/secretscan.py`, over every file before hashing): **no unexplained hit** over every file of the folder. 0 PEM private-key blocks; 0 hits for the 1,501 body lines of the 119 key files of the run's 17 labs (SSH keys, fixture signing, CA and TLS keys); 0 fixture-licence literals and 0 `CPK-` shapes; **0 hash-shaped credential values** outside the harness's own source (crypt `$N$`, Dovecot scheme values, SCRAM verifiers, MariaDB native hashes: the shapes the drivers remove at collection time), 0 values removed by that rule (the product never emitted one into anything this run collected) and no `crypt_hash` field; **WireGuard: 15 keys of the 32-byte base64 shape, all 15 public keys that the evidence itself names as such** (peers are identified by them), no private key, no preshared key, no client configuration (those three words occur only in the harness's own source); every secret-named JSON field holds `[REDACTED]` (831 fields: owner, mailbox, site-account, database and engine passwords, delivery tokens, the session cookie); no engine password handed over in an environment assignment (the two client variables), no SQL password literal, no base64 password handed to a guest helper. The mailbox password of the import fixtures and its hash are in no file. The harness source in `harness-run-copy/` and `tools/` writes the patterns and fabricated test strings it searches for; that is source, not a value of a run.
- Root `SHA256SUMS` covers every file; the longest repository-relative path is under 240 characters.

## Files

`build/` (three builds, proofs, dry runs, offline logs), `harness-run-copy/` (run-copy hashes, overlays with their
diffs, jobs, queue), `tools/` (the scripts of this run; no secret), `host/` (host check, `C:` readings, removals,
leftovers, keep-awake log, power events), one folder per run with the driver's evidence unchanged (its own
`SHA256SUMS` verified when staged) and `host/`; Part 2 under `part2-alpha81/`. Generated: `summary-per-cell.txt`,
`part1-table.md`, `part2-table.md`, `checks-not-passed.txt`, `texts-en-tr.md`, `native-facts.json`,
`sampler-gaps.txt`, `secret-scan.txt`, `SHA256SUMS`.

## Post-collection corrections to this README (2026-10-12)

An intake check found the evidence sound but this README stated two facts more strongly than the raw data does, and
lacked one explanation. Three edits, made after the collection; no evidence file other than `README.md` and
`part1-table.md` was changed (every other file is byte-identical to the collection; `SHA256SUMS` lists only the two
new digests). The generator `tools/summary.py` is unchanged, so a regeneration of `part1-table.md` would not carry
the note below.

1. **IMAP login of the imported mailboxes (section "S1 ... imported mailbox authenticates").**
   Written: "a real IMAP `LOGIN` ... answers `OK` and `INBOX` is selected" (Debian 13 and Ubuntu 24.04).
   Raw data (`steps/17-c7b-import-answers/native/166-mail-login-tar.json`, `167-mail-login-i.json`,
   `168-mail-login-v.json`, key `readings.the_original_password.imap`): `answer: "OK"`, `logged_in: true` on both
   platforms; `inbox_selected: true` for the three mailboxes of `rid3-ubuntu/run-a`, `inbox_selected: false` for the
   three mailboxes of `rid3-debian13/run-a` and `run-b`, with no `select_error` field in any of the nine files (the
   helper `tools/patch_helper.py` writes that field only when the select call raised an exception); no check asserts
   the selection. Now says so: the login succeeded on both platforms; the selection was recorded as successful on
   Ubuntu and as not selected on Debian without an error, and was not a checked condition.
2. **Baseline-identity proof.** Written: "35 files are listed ... each `identical`". Raw data
   (`build/a81/baseline-ref-proof.txt`): 35 lines end `identical`, of which 5 are tree lines (`cmd/agent`, `deploy`,
   `download-portal`, `web`, `internal/transport`) and 30 are file lines; 0 `DIFFERENT`. Now says "35 entries (30
   files and 5 trees)".
3. **Ubuntu O10 cell of the Part 1 table.** Written: the table showed `FAIL (2 of 4 checks)` with the explanation
   only under the table. Now one sentence at the table's introduction (and as a note in `part1-table.md`) says the
   FAIL is the harness defect H40, quotes the recorded fields (`same_bytes: true`, `replayed: "1"`,
   `sent_value_occurs_in_the_raw_answer: 0`, `replay_sent_value_occurs: 0`, `new_on_engine: ["s2_srvsent"]` in
   `rid3-ubuntu/run-a/steps/11-c3-server-databases/section.json`) and that Ubuntu was not re-run after the fix.

## Corrections after intake (2026-10-10)

After this directory was published (2026-10-10), the operator's machine name was replaced by `<operator-host>` in the SSH public-key comments (and in the text that named it) of this directory: 29 occurrences in 17 files; the key material itself, a lab public key, is unchanged.
After this directory was published (2026-10-10), 42 digest values of the one-shot update-transaction token (36 in plain text in 22 files, as `transaction_token_sha256` values and as `.release-db-migrations/<digest>` directory names; 6 inside base64 `events_base64` text in 3 files) were replaced by `[REDACTED-SHA256]`.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.

Checksum repair (2026-10-10): `sha256sum -c SHA256SUMS` failed for `tools/readme_values.json`, and no form of the file matches the recorded hash (not as it is, nor CRLF-to-LF, LF-to-CRLF, with a UTF-8 byte order mark or as UTF-16LE, nor the version at any commit that touched it: it has had the same bytes since its first commit). The file on disk differs from the bytes the list was written from; which bytes those were cannot be recovered from the repository. The file was kept as it is and only its line in `SHA256SUMS` was regenerated: old recorded hash `7f400399bc6ab0937d9e518e9303a70c6bab56572955bfc8534a223e9d8271e3`, new hash `7a1b9233758653e6b262208e68e49e5cc306e49ed4a7f520491219c91d33e178`. The line was wrong from the first commit of this directory (not introduced by the redaction above); the README values this file fed should be read with that in mind. The `README.md` line of the list was regenerated because this paragraph was added to the README.
