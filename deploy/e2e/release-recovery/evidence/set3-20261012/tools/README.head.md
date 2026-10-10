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

**Result in one sentence:** {{ONE_SENTENCE}}

## What was built from what

Three builds (`go1.26.5 linux/amd64`), every archive `license_mode: acceptance-fixture` (`build/<name>/`: artifact
document, dist JSONs, fixture commits and patches, trees, build logs; the Part 2 folders also hold
`baseline-ref-proof.txt` and `agent-deps.txt`).

{{BUILD_TABLE}}

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
  commit); the trees `cmd/agent`, `deploy`, `download-portal`, `web` and `internal/transport` are identical; 35 files
  are listed with blob id and SHA-256 at the tag and at the baseline, each `identical` (0 `DIFFERENT`): `update.sh`,
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
{{HARNESS_EXTRA}}

The cells ran from a queue with three workers (`harness-run-copy/queue.sh`, `queue.log`): before each cell the newest
Windows `C:` reading (PowerShell `Get-PSDrive C`, written every 30 s by `tools/cwatch.ps1`; WSL cannot run PowerShell
on this host) had to be younger than 3 minutes and at least 40 GiB; after each cell its evidence was staged, the
driver's `SHA256SUMS` verified on the staged copy, and only then the lab's overlay disks removed.

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`, GiB; `host/c-drive.txt`, `host/c-drive-cells.txt`, `host/c-drive-watch.txt`)

{{CDRIVE}}

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

{{CELLS_TABLE}}

