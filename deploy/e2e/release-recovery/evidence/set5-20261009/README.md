# set5 native run, 2026-10-09 (UTC): the owner-started update from the published v0.1.0-alpha.81 to the release candidate `67b62cc0f`, the ten cells of set3's Part 2 again, with set4's item 9 and one Postfix Stop

A measurement record. Disposable QEMU/KVM guests of the local `archlinux` WSL host; this folder is named with the
calendar date of the first cell (`date -u` on the host: `host/hostcheck-before.txt`, `host/progress.txt`).

**What ran.** The ten cells of set3's Part 2 (`evidence/set3-20261012/`, there measured on commit `cfa329676`), with
the same driver, the same steps and the same pass criteria, on a candidate built from commit
`67b62cc0fa4a6b8edd33e6a1bf4a55a982ceaaed` ("A value read from postconf is a value, or it is unknown"), the last
commit of the branch that changes product code. In each cell the published tag `v0.1.0-alpha.81` (unpatched source,
test-licence build) is installed by its own installer, its owner sets the server up, seeds a site, a mailbox (Debian,
Ubuntu), a cron job, and starts the update to the candidate, labelled `v0.1.0-alpha.82 / 82`, from the Panel's own
update route. In the two good-update cells on Debian 13 and Ubuntu 24.04 two things were added: set4's item 9 (a PHP
site created by alpha.81, ten requests compared before the update, after it and after the vhost was rendered again)
and, after the cell's own verdicts, one Stop of Postfix through the Panel with a `main.cf` that `postfix check`
refuses (set4's M10 section).

**What this is not.** No installed server was addressed by the harness: it holds no name or address of one, and none appears
in the raw files (`secret-scan.txt` does not search for that; the search is recorded in "Removals, leftovers, secrets").
That no traffic of the host or the guests reached an installed server was not measured: the guests have outbound NAT
("Network" below) and no traffic was captured. Nothing was committed, pushed, published or signed with a production key. Product
code (`cmd/`, `internal/`, `web/`), `docs/` and `ROADMAP*` were not edited by this run. Every `result.json` carries
`native_evidence: false`. **It closes no P0 row.** Nothing was rendered in a browser. The guests' disks were kept on
RAM-backed mounts of the WSL host (see "Disk"), so no duration of this run is a statement about a disk.

**Result in one sentence.** All ten cells of the update from the published v0.1.0-alpha.81 to a candidate built from
`67b62cc0f` reached the end set3 measured on `cfa329676`, with the same verdict for every step, the same outcome and
final state, and the same value for every compared fact (`compare-with-set3.md`: no difference): verified on Debian
13, Ubuntu 24.04 and Arch; automatic return to alpha.81 with the ledger at the released 42 after a migration defect
with a second fault (three platforms) and after a failed start check (Debian); the owner's one printed retry
completing a paused update (Debian, Ubuntu); the workloads served with management off across a reboot (Debian). A PHP
site created by alpha.81 answers ten requests with the same status and body before the update, after it and after
its vhost was rendered again (Debian, Ubuntu). One Postfix Stop through the Panel with a refused `main.cf` answers
`200` with the note `unit_marked_failed_config`, leaves the unit `failed`, issues no `reset-failed`, and Start works
after `main.cf` is restored (Debian: `postfix.service`; Ubuntu: `postfix@-.service`). No step and no check failed;
nothing is NOT-MEASURED among the ten cells. Each cell ran once, with RAM-backed guest disks.

## What was built from what, and how this folder shows which code was measured

One build (`go1.26.5 linux/amd64`, `build/go-version.txt`; 17:00:58-17:05:10Z, `build/build-job.*.txt`), every archive
`license_mode: acceptance-fixture` (`build/a81/`: the artifact document, the dist JSONs and their build logs, the
fixture commits and patches, the trees, the builder's logs).

| Role | Label / seq | Commit | Tree | Archive SHA-256 | Built |
| --- | --- | --- | --- | --- | --- |
| baseline | v0.1.0-alpha.81 / 81 | a0beb7263d1f4ca72258f6b306f9111ba4e2a334 | b1dffa78bcaca9e3514e5b2bc42f0e2cf47dca2f | 3350ff44dad2bb699ab5ee0a112b58b7bfb7fa47aa0ebdd3dd3c2da73d080109 | by set3; reused unchanged |
| good | v0.1.0-alpha.82 / 82 | 8c2250b05132fa91291a33bd23924d0aefd72617 | 7e850e716b8d57bb6557205ee0fbbcc9afa6e71f | a814f52a98b088edccb256d926786eba9320fc25115945b83731a65ef782218a | this run |
| defective | v0.1.0-alpha.82 / 82 | ecccd8d7b3f3e8106b9d7d21bfb8d0ef1e1ad8ef | eab4182c22d0b970ce4cc6b46ce84b65e753d882 | 3f35d8c653ca2f8177434b5a1ec2f171c309bd89419db2152096cee82bc58c59 | this run |
| startcheck | v0.1.0-alpha.82 / 82 | 49143bd1c108ecc7d84b7c861358312db6f15063 | 7b7ef65c7e40bd2b5370384c5dd19ce60f2ee591 | 8b3b75d275aceafbbd63272da64f3c025b0642659b0d2adccab44b3329458cce | this run |

- **The candidate.** `run-upd1.sh build --baseline-ref v0.1.0-alpha.81 67b62cc0f`. The good candidate is commit
  `67b62cc0f` (tree `a3167582da96fe38caa44b58dbc46cecf42a4600`) plus one file, the release policy
  `deploy/release-sequence-policy` (`v0.1.0-alpha.82 / 82`, previous `81 / v0.1.0-alpha.81 / a0beb7263...`: what a real
  alpha.82 names as its predecessor); the defective candidate is the good one plus the migrate-only defect in
  `cmd/panel/main.go`; the start-check candidate is the good one plus the start-check defect in
  `cmd/panel/server_lifecycle.go`. The three diffs are in `build/a81/fixture-patches.diff`, the commits in
  `build/a81/fixture-commits.txt`. These fixture commits exist only in the builder's disposable clone
  (`/var/tmp/cp-upd1-build/20261009t170058z/repo`); no commit object was made in the working repository.
- **The baseline: the published tag's unpatched source, test-licence build.** The tag's own commit, no fixture commit
  and no patched file (`patched_files: []` in `build/a81/upd1-artifacts.json`; `build/a81/baseline-ref-proof.txt`:
  `git diff --name-status tag..baseline` is empty, 35 listed entries each `identical`, 0 `DIFFERENT`, no licensing
  package in the Agent's package closure). `bin/panel` is compiled from that source with the build tag
  `acceptance_license` (`build/a81/dist-baseline.json`, `panel_build_tags`). **Its archive was not built by this
  run**: it is the one set3 built (`/var/tmp/cp-pair-accept/dist/a0beb7263...-acceptance-license`), used read-only; its
  SHA-256 read on the host before the build (`host/hostcheck-before.txt`) is `3350ff44...`, the value in set3's README
  and in set4's; the builder says so (`build/a81/build.err.txt`, `BUILD-UPD1-REUSED`) and the artifact document marks
  it (`reused_dist`). It is not the signed release archive.
- **`67b62cc0f` against the branch head.** While this run went on the branch head was `2ab7bf20c`. `git diff --stat
  67b62cc0f 2ab7bf20c -- cmd internal web` is empty; the 321 files that differ are all under
  `deploy/e2e/release-recovery/evidence/set4c-20261009/` (among them nine `tools/*.sh`, which is what the pathspec
  `deploy/*.sh` matches; no script of the product differs). `build/a81/trees.txt` records the counts as read by the
  staging script, and the product commits between set3's candidate and this one (`557b554eb`, `e2be8af30`,
  `1f182a483`, `67b62cc0f`).
- **How a cell shows what it ran.** `build/a81-prove.json` (`run-upd1.sh prove`, exit 0, `build/afterbuild-job.out.txt`):
  the inventory, the release policy and the committed-source proof of each of the four archives against its own
  commit's blobs, the reused one included. Each cell's `result.json` names the archives by commit and SHA-256
  (`artifacts`); its `steps/NN-preflight/preflight.json` repeats the proof for the two archives it uses; its
  `steps/NN-terminal/step.json` (`builds`) holds the identity files of the installed Agent and Panel as read on the
  guest at the end, `version=v0.1.0-alpha.82 commit=8c2250b0...` after a forward end and `version=v0.1.0-alpha.81
  commit=a0beb726...` after a return (`timeline.md`).

Offline suites on each run copy (`build/offline-<copy>-*.txt`), all OK on copies `b` to `e`:
`test_owner_update_trial` 191 (3 skipped), `test_settings_writes_trial` 30, `test_request_identity_trial` 17,
`test_recovery_candidate_archive` 11, `test_lab` 15, `test_guest_probe` 13, `test_set4b_trial` 8, and the new
`test_set5_trial` 10. On copy `a` `test_owner_update_trial` ends with 4 errors and the others are OK: four of its
tests read fixtures from `evidence/upd1-20261001/`, which copy `a` did not hold (see the run copies below). The dry
run of each of the ten cells exited 0 and created no lab (`build/dry-<copy>-*.json`: copies `c` and `d` all ten,
copy `e` the eight cells that ran from it).

## Harness changes (working tree, `deploy/e2e/release-recovery/` only)

| File | What |
| --- | --- |
| `set5_trial.py` (new) | The driver of this run. `Set5UpdateTrial` is set4's `Set4UpdateTrial` (itself `owner_update_trial.Trial`); no step of either is changed (`test_set5_trial.py` pins that `execute`, `collect`, `verdicts`, `terminal`, `post_update_facts`, `seed`, `setup`, `preflight`, `origin` are the base driver's own functions). Added steps: `set5-name-pinning` before `preflight`; set4's two item-9 steps, only in `upd1-debian13-good` and `upd1-ubuntu-good`; `M10-postfix-stop` after `verdicts`, only in those two cells; `set5-name-pinning-at-the-end` as the last measuring step (before `kind-expectation` where a cell has one). M10 is `Set4Trial.m10_postfix_stop` with the recording and service-action methods of `SettingsTrial`, borrowed as plain functions. set4's update-check step of the rollback cells (item 12) is not run. |
| `set5_redact.py` (new) | Collection-time rules for digests of tokens: a hexadecimal value of 32 to 128 characters under a key whose name contains `token`; the directory name under `.release-db-migrations/`; and every such value wherever it appears again. Put in front of the driver's redactor; before the result is written every file of the run is passed through once more (`set5-redaction-sweep.json`, counts only). The same module is run over the staged copy with the values of the lab's raw records known (`host/token-digest-sweep-at-staging.json`). |
| `run-set5.sh` (new) | Wrapper, as `run-set4.sh`: one cell per new lab. Two optional programs: `SET5_AFTER_PREPARE` (after the lab is prepared) and `SET5_DISK_GATE` (immediately before the guests start). |
| `test_set5_trial.py` (new) | Offline tests of the redaction rules, the pinning verdict, the cell list and the borrowed methods. |
| `lab.py` (changed, +9 -2) | Opt-in `CELIKPANEL_LAB_LINK_BASE_IMAGES=1`: the lab's base image is a hard link to the cached image instead of a copy. The image is verified against its pin as before. Without the variable nothing changes. |

Run copies (`harness-run-copy/`: per overlay `files.sha256`, `harness.diff`, `differs-from-archive.txt`,
`runcopy-against-commit.txt`, `pristine-files.sha256`; job files `jobs/`; `queue.log`). A run copy is `git archive
67b62cc0f` **without the retained evidence of earlier runs** under `deploy/e2e/{release-recovery,dns-kill-matrix,
dns-pair-acceptance}/evidence/` (about 490 MB of records, no harness code), plus the listed harness files of the
working tree. `runcopy-against-commit.txt` of each copy: every file of the pristine copy has the blob id the commit
has for its path (0 files not in the commit), and every file of the commit that is missing lies under one of those
three folders (0 outside them). `a` = without all three folders (offline suites: the 4 fixture errors named above);
`b` = `a` + `evidence/upd1-20261001` kept (the build); `c` = `b` with the gate called through `bash` (proof, first dry
runs); `d` = `c` + the `lab.py` change and the after-prepare program (cells `upd1-debian13-good`, `upd1-ubuntu-good`);
`e` = `d` + the package reading in the last step and H46 (the eight other cells) = the working tree's five files (SHA-256 equal, `host/working-tree-against-copy-e.txt`).
`PYTHONDONTWRITEBYTECODE=1`.

Harness defects found during the run (product code was never changed):

| Id | Defect | Handling | Runs |
| --- | --- | --- | --- |
| H46 | The first class of the redaction report was named `token_named_field`; the pair redactor blanks the value of any JSON key whose name contains `token`, so that count reads `"[REDACTED]"` in `set5-redaction-sweep.json`. | The class is `named_field` from copy `e`. No value of a run is affected, but that one count of the two runs cannot be read from their reports. The staging pass over the same files afterwards (`host/token-digest-sweep-at-staging.json`, not written through the redactor) found nothing left to remove in either run (0 places). | `upd1-debian13-good/run-a`, `upd1-ubuntu-good/run-a` (cosmetic) |
| H47 | Not a cell: a checking script of the session (`tools/wtcheck.sh` as first written) ran `git status` and `git diff --stat HEAD` over the Windows working tree from inside WSL at about 19:21Z, while `upd1-ubuntu-owner-continuation` ran. Over drvfs that takes minutes; it was stopped after about 14 minutes. | The two calls were removed from the script; the working tree's state is read with the Windows git instead (`host/working-tree-status.txt`). No file of the tree was changed by it (the same `git status --short` before and after); `git status` may have rewritten `.git/index` with refreshed file times. The cell that ran meanwhile shows no gap (`sampler-gaps.txt`, `host-clock-gaps.txt`). | none (no cell reads the working tree) |

## Guests, images, packages

Base images (the image cache `/var/tmp/cp-v3n28/images` of earlier runs, read-only; digests from each cell's
`host/fixture-plan.json`): `Arch-Linux-x86_64-cloudimg-20260815.573966.qcow2` (sha256 `5d8be8d2...`),
`debian-13-genericcloud-amd64-20260826-2582.qcow2` (sha512 `184761b0...`),
`ubuntu-24.04-server-cloudimg-amd64-20260826.img` (sha256 `d0fe84bb...`): the images of set3 and set4. One new lab
per cell, stopped by the wrapper; the cells ran one after the other. A Debian or Arch cell starts both guests of the
pair lab and works on one; an Ubuntu cell starts one guest. Each guest: 2 CPUs, 3072 MB, KVM, a 24 GB qcow2 overlay
over the base image, QEMU 11.1.1 with the planned command line (`cache=none`).

Packages are the distributions' own, installed by the product's setup during the cell from the distributions'
repositories. Read on the guest (`timeline.md`, section "Packages", per run): in the two good cells by set4's
platform reading before the update, in the other eight by the last step of the cell.

| | Arch | Debian 13 | Ubuntu 24.04 |
| --- | --- | --- | --- |
| OS | Arch Linux | Debian GNU/Linux 13 (trixie) | Ubuntu 24.04.4 LTS |
| kernel (read in the cells of copy `e`) | 7.2.9-arch1-1 | 6.12.105+deb13-cloud-amd64 | 6.8.0-138-generic |
| certbot | 5.8.0-1 | 4.0.0-2+deb13u1 | 2.9.0-1 |
| cron | 1.7.2-2 | 3.0pl1-197 | 3.0pl1-184ubuntu2 |
| cronie | 1.7.2-2 | not read | not read |
| dovecot-core | not read | 1:2.4.1+dfsg1-6+deb13u7 | 1:2.3.21+dfsg1-2ubuntu6.5 |
| mariadb | 13.0.2-2 | not read | not read |
| mariadb-server | 13.0.2-2 | 1:11.8.6-0+deb13u1 | 1:10.11.14-0ubuntu0.24.04.1 |
| nginx | 1.30.5-1 | 1.26.3-3+deb13u9 | 1.24.0-2ubuntu7.18 |
| nginx-common | not read | 1.26.3-3+deb13u9 | 1.24.0-2ubuntu7.18 |
| openssl | 3.6.5-1 | 3.5.7-1~deb13u2 | 3.0.13-0ubuntu3.15 |
| php | 8.5.11-1 | not read | not read |
| php-fpm | 8.5.11-1 | 2:8.4+96 | 2:8.3+93ubuntu2 |
| php8.3-fpm | not read | not read | 8.3.6-0ubuntu0.24.04.11 |
| php8.4-fpm | not read | 8.4.26-1~deb13u1 | not read |
| postfix | not read | 3.10.13-0+deb13u1 | 3.8.6-1ubuntu0.1 |
| rspamd | not read | 3.12.1-1 | 3.8.1-1ubuntu3 |
| systemd | 262-1 | 257.13-1~deb13u1 | 255.4-1ubuntu8.17 |

A cell with one value per platform means every run of that platform that read the package read the same version; where runs
differ, each value is followed by its cells. `not read`: the package is not installed there, or no run of the platform read it
(the two good cells of Debian and Ubuntu read set4's shorter package list before the update; the other cells read the list above at their end).

Setup profile as in set3: Debian and Ubuntu purpose `web_mail`, Arch purpose `web`; DNS mode external. The owner's
setup runs to the wait at `access_dns` (`server_setup_access_dns_required`, recorded as a finding in every
`result.json`); the certificate steps of the wizard are not reached.

## Network: QEMU user networking with outbound NAT, and name pinning (this is not network isolation)

The guests run on QEMU user networking with outbound NAT (`host/fixture-plan.json` of each cell: `-netdev
user,id=mgmt,hostfwd=tcp:127.0.0.1:<port>-:22`, no `restrict=on`), and the distributions' packages are fetched from
their repositories during a cell. No file records the guests' traffic, so nothing here shows that a guest could not
reach the outside. What is established, per cell (`pinning.md`, generated from the step files):

1. **The certificate authorities' names, at guest preparation.** The first step of every cell,
   `steps/01-set5-name-pinning/` (`name-pinning.json`, `step.json`), appends to the guest's hosts file the two Let's
   Encrypt directory names (`acme-v02.api.letsencrypt.org`, `acme-staging-v02.api.letsencrypt.org`) and the two other
   directories the product names (`acme.zerossl.com`, `dv.acme-v02.api.pki.goog`; `internal/core/acme_providers.go`),
   each as `127.0.0.1` and `::1`, and reads them back with `getent -s files ahosts NAME` and `getent -s files hosts
   NAME` (the hosts file only; no DNS question is sent). The step passes only if every address read is the guest's
   own loopback and none of `/opt/celikpanel`, `/etc/celikpanel`, `/var/lib/celikpanel`, `/usr/libexec/celikpanel`
   exists yet; `preflight` and everything after it need this step. It ran 18 to 58 s after the guest's boot (18.1 to 57.9 s) and
   before the driver's first own step. This is lab preparation, not an owner action.
   The read-back asks the hosts file database only. On Debian and Ubuntu the guest's `nsswitch.conf` lists `files` first;
   on Arch it reads `mymachines resolve [!UNAVAIL=return] files myhostname dns` (`nsswitch_hosts` in `name-pinning.json`),
   so a program there asks `resolve` before `files`. What an Arch program's own lookup of these names returned was not asked.
2. **`celikpanel.net`, before the baseline is installed.** As in set3, the `origin` step maps the name to `127.0.0.1`,
   where the lab's own fixture origin answers, and reads it back (`steps/03-origin/step.json`, `origin_check`:
   `loopback_only: true`, HTTP 200) before `baseline-install` starts. It is not written earlier because the
   fixture's own provisioning refuses a hosts file that already maps the name.
3. **At the end.** The last measuring step, `steps/NN-set5-name-pinning-at-the-end/` (in the four cells that have a
   `kind-expectation` step, the Debian start-check cell, both owner-continuation cells and the Debian management-off cell,
   that judging step comes after it), reads the five names again the same way,
   with the boot id (a cell with a VM reset or an owner's reboot ends in another boot than it began; so does every
   Arch cell), and lists `/var/log/letsencrypt` and `/etc/letsencrypt/{accounts,live,renewal}`. In every cell certbot
   is installed by then (`/usr/bin/certbot`) and has no log file, no account directory, no lineage and no renewal
   configuration: the three `/etc/letsencrypt` directories do not exist, and `/var/log/letsencrypt` does not exist
   (Debian, Ubuntu) or is empty (Arch; `[]` in the last column below).
4. **The licence step** was answered, by the Panel's own account, without a licence service: the Panel's own answer in
   `steps/06-license/step.json` is `license_service: "not contacted: acceptance test build"`.
5. **No certificate route was called**: no API exchange of any cell names a certificate, SSL or ACME route
   (`api-routes.txt`: every route each cell called, from its recorded exchanges; 0 routes whose path names ssl, certificate, acme, letsencrypt or certbot, in every cell; the licence routes are `GET` and `POST /api/v1/panel/license` and `GET /api/v1/license/access`).

The `access_dns` finding line in the raw `result.json` files says the wait happens "on an isolated host". That is the harness's
wording for a guest without a public name; it is not a statement that the guests were cut off from the network.

| Run | Pinned at (guest) | Product paths present then | All four CA names loopback-only | origin step: celikpanel.net loopback-only, HTTP | Install starts (host) | At the end: all five loopback-only | Boot id changed | certbot log directory |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| update-alpha81/upd1-debian13-good/run-a | 2026-10-09T17:14:54Z, 56.1 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T17:15:06Z | True (passed) | False | absent |
| update-alpha81/upd1-ubuntu-good/run-a | 2026-10-09T17:27:22Z, 18.1 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T17:27:36Z | True (passed) | False | absent |
| update-alpha81/upd1-arch-good/run-a | 2026-10-09T18:03:32Z, 53.13 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T18:03:44Z | True (passed) | True | [] |
| update-alpha81/upd1-debian13-defective/run-a | 2026-10-09T17:49:03Z, 56.08 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T17:49:18Z | True (passed) | True | absent |
| update-alpha81/upd1-ubuntu-defective/run-a | 2026-10-09T18:12:57Z, 18.17 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T18:13:12Z | True (passed) | True | absent |
| update-alpha81/upd1-arch-defective/run-a | 2026-10-09T18:34:23Z, 52.92 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T18:34:34Z | True (passed) | True | [] |
| update-alpha81/upd1-debian13-startcheck/run-a | 2026-10-09T18:45:15Z, 54.12 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T18:45:26Z | True (passed) | True | absent |
| update-alpha81/upd1-debian13-owner-continuation/run-a | 2026-10-09T18:59:29Z, 55.51 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T18:59:40Z | True (passed) | False | absent |
| update-alpha81/upd1-ubuntu-owner-continuation/run-a | 2026-10-09T19:18:58Z, 25.31 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T19:19:20Z | True (passed) | False | absent |
| update-alpha81/upd1-debian13-mgmt-off-reboot/run-a | 2026-10-09T19:47:50Z, 57.91 s after boot (passed) | [] | True | True, 200 (passed) | 2026-10-09T19:48:02Z | True (passed) | True | absent |

## Disk, RAM-backed guest disks, host power

**The rule.** Before every guest start the newest Windows `C:` reading (PowerShell `Get-PSDrive C`, written every
30 s by `tools/cwatch.ps1`; WSL cannot run PowerShell on this host) had to be younger than 120 s and at least
40 GiB (42 949 672 960 bytes; the brief says "40 GB", read here as PowerShell's `40GB`, the larger of the two
readings). `tools/gate.sh` was asked twice per cell: by the queue before the lab was prepared and by the wrapper
immediately before the guests started (`host/c-drive-cells.txt`; per cell `host/c-drive-gate.txt`). All 20 decisions of the ten cells were `allowed=yes` (readings between 41.94 and 41.07 GiB, none older than 30 s); the watcher never wrote its below-40 flag.

**Readings (GiB).** 45.85 before any work (16:42Z); 45.56 when the build started; 41.89 when it ended: the builder's
clone (1.1 GB) and three dist directories (0.97 GB each) were written to parts of the WSL virtual disk that had not
been used before. 41.93 after 2.5 GB of this run's own build intermediates were removed (no change); 41.92 before the first cell
(17:13Z). At the gate, before the ten cells in their order: 41.92, 41.94, 41.91, 41.90, 41.78, 41.59, 41.58, 41.56,
41.46, 41.07 (41.09 at its guest start). 41.00 after the last cell was staged (20:02Z); lowest of the 368 readings
taken every 30 s: 41.02 GiB (20:02:49Z, the last one); 40.96 at 20:15Z, after this folder had been written once (`host/c-drive.txt`).
Never under 40 GiB before a guest start or at any reading.

Across the ten cells `C:` fell by 0.92 GiB. What this run wrote per cell is the lab directory without disks and
images (66 MB on the WSL disk) and the staged evidence (24 MB on `C:` for the whole folder). The reading did not
fall evenly: about 0.2 GiB at 18:13Z and about 0.46 GiB between 19:14Z and 19:26Z, with flat stretches between
(`host/c-drive-watch.txt`). Another session was working in the same repository during the run
(`host/working-tree-status.txt`), and this run's own stopped check H47 falls in the second window. What part of the
0.92 GiB is this run's was not established.

**Why the guests' disks were in RAM.** The WSL virtual disk grows when new blocks are written and does not give
space back to Windows when files are deleted (no sparse-VHD setting on this host: after 2.5 GB of this run's own build
intermediates were removed at 17:06Z, the `C:` reading did not move: 41.93 before, 41.93 two minutes later). With
1.9 GiB above the rule's floor and about 1.9 GB of overlay written per cell, the rule would have stopped the run
after the first cells. Two changes kept the cells from writing large files to the WSL disk at all:

- the lab's base images are hard links to the cached images, not copies (`lab.py`, opt-in; 0.86 GB per pair lab);
- after a lab is prepared and before its guests start, each guest's node directory (overlay disk, seed image, QEMU's
  pid file, socket and serial log) is moved onto a tmpfs mount of the WSL host, in place (`tools/ramnodes.sh`; per
  cell `host/ram-node-directories.txt`; `host/ram-nodes.txt`). The paths and QEMU's command line are the planned
  ones; `cache=none` on tmpfs was probed first (`tools/ramtest.sh`, `host/ramtest-output.txt`: kernel `6.18.33.2-microsoft-standard-WSL2`, a
  direct write and read and `qemu-io -t none` succeed). The lab's evidence directory stays on the disk.

What follows from it: a guest's writes, `fsync` included, end in the WSL host's memory, not on a disk. In set3 and
set4 they ended in a qcow2 file on the WSL virtual disk. Every duration in this folder (setup, update, Panel down)
was measured with RAM-backed guest disks and is not comparable with set3's as a duration; whether a product
behaviour depends on a slow disk was not measured. A VM reset (QMP `system_reset`) does not restart QEMU, so the
overlay survives it as it does on a disk. Memory of the WSL host: 15.7 GB. Before each cell's mounts at least 14714 MiB were available (`host/ram-nodes.txt`,
ten cells). During the cells the lowest reading of the 30 s watcher was 9422 MiB available (19:57Z, two guests and
1.6 GB of overlay in RAM), and swap in use was 0 MiB at each of its 262 readings (`host/mem-watch.txt`, 17:51Z to
20:02Z; the watcher was started while the third cell ran, so the first two cells have no such reading).

**Host power.** `tools/keepawake.ps1` held a process-level keep-awake request for the run (`host/keepawake.log`) and
one idle WSL session was held. The request was held from 16:58:50Z to 20:02:53Z.

What the host's System log says (`host/sleep-events.txt`, Kernel-Power, read at 20:03Z): the last sleep entry (id 42)
and resume (id 107) are at 15:06:57Z and 15:07:01Z, more than two hours before the first cell. After that the log
has "entering modern standby" (506) at 16:38:46Z, "leaving" (507) at 16:56:45Z, "entering" at 17:10:44Z, "leaving" at
19:15:44Z and "entering" at 19:31:37Z. By that log the first seven cells ran wholly inside a modern-standby interval,
`upd1-debian13-owner-continuation` across its end (19:15:44Z, during its owner step), `upd1-ubuntu-owner-continuation`
across the next entry (19:31:37Z, during its setup), and `upd1-debian13-mgmt-off-reboot` inside the second interval
(`host-clock-gaps.txt`, per cell).

What the run's own clocks say: the host kept executing through all of it. The Windows watcher wrote its line every
30 s from 16:58:49Z to 20:02:49Z (368 lines, no gap over 31 s), the WSL watcher from 17:51:51Z to 20:02:26Z (262
lines, no gap over 31 s), no step of any cell starts more than 1 s after the step before it ended, and no cell has a
gap between two of its own host samples (one every 5 s) larger than 10.5 s: 10.1 to 10.5 s at the VM reset of the
three reset cells, 9.3 s once in `upd1-ubuntu-owner-continuation` while the Panel's port was held, 5.0 to 5.8 s
elsewhere (`sampler-gaps.txt`). So for each of the ten cells: no measured step ran after a wake from a pause, because no pause
of the host is seen inside or between the cells. As in set3, "modern standby" in the log did not stop execution on
this host. What that state changes for a running workload other than pausing it was not measured.

## Cells

One new lab per cell; the cells ran one after the other, in this order. `S/` is `steps/` of the run's folder under
`update-alpha81/`. PASS: every step of set3's method ran with the verdict it had in set3 and the outcome and final
state are the expected ones; the steps set5 added are in their own column.

| Cell | What | Run, lab, harness copy | Wrapper (UTC) | By set3's criteria | Outcome / final state | Added steps | Raw files |
| --- | --- | --- | --- | --- | --- | --- | --- |
| upd1-debian13-good | good update | run-a, `s5-d13-good-a`, d | 17:13:51-17:26:10 | **PASS** | update-verified; succeeded/update_verified; overall `complete-for-review` | M10-postfix-stop: passed, set4-php-site-after-the-update: passed, set4-php-site-before-the-update: passed, set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-debian13-good/run-a/result.json`, `S/*/step.json`, `S/*-m10-postfix-stop/section.json` |
| upd1-ubuntu-good | good update | run-a, `s5-ub-good-a`, d | 17:26:58-17:47:26 | **PASS** | update-verified; succeeded/update_verified; overall `complete-for-review` | M10-postfix-stop: passed, set4-php-site-after-the-update: passed, set4-php-site-before-the-update: passed, set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-ubuntu-good/run-a/result.json`, `S/*/step.json`, `S/*-m10-postfix-stop/section.json` |
| upd1-debian13-defective | migration defect + second fault: automatic return | run-a, `s5-d13-def-a`, e | 17:47:59-18:02:01 | **PASS** | recovered-automatically; recovered/rollback_verified; overall `complete-for-review` | set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-debian13-defective/run-a/result.json`, `S/*/step.json` |
| upd1-arch-good | good update | run-a, `s5-arch-good-a`, e | 18:02:29-18:12:17 | **PASS** | update-verified; succeeded/update_verified; overall `complete-for-review` | set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-arch-good/run-a/result.json`, `S/*/step.json` |
| upd1-ubuntu-defective | migration defect + second fault: automatic return | run-a, `s5-ub-def-a`, e | 18:12:34-18:32:53 | **PASS** | recovered-automatically; recovered/rollback_verified; overall `complete-for-review` | set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-ubuntu-defective/run-a/result.json`, `S/*/step.json` |
| upd1-arch-defective | migration defect + second fault: automatic return | run-a, `s5-arch-def-a`, e | 18:33:20-18:43:57 | **PASS** | recovered-automatically; recovered/rollback_verified; overall `complete-for-review` | set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-arch-defective/run-a/result.json`, `S/*/step.json` |
| upd1-debian13-startcheck | start-check defect + VM reset: automatic return | run-a, `s5-d13-sc-a`, e | 18:44:15-18:58:00 | **PASS** | recovered-automatically; recovered/rollback_verified; overall `complete-for-review` | set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-debian13-startcheck/run-a/result.json`, `S/*/step.json` |
| upd1-debian13-owner-continuation | port held: pause, the owner's one retry | run-a, `s5-d13-oc-a`, e | 18:58:26-19:17:59 | **PASS** | recovered-after-owner-continuation; succeeded/update_verified; overall `complete-for-review` | set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-debian13-owner-continuation/run-a/result.json`, `S/*/step.json` |
| upd1-ubuntu-owner-continuation | port held: pause, the owner's one retry | run-a, `s5-ub-oc-a`, e | 19:18:26-19:46:13 | **PASS** | recovered-after-owner-continuation; succeeded/update_verified; overall `complete-for-review` | set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-ubuntu-owner-continuation/run-a/result.json`, `S/*/step.json` |
| upd1-debian13-mgmt-off-reboot | good update, then management off across a reboot | run-a, `s5-d13-mr-a`, e | 19:46:42-20:01:14 | **PASS** | update-verified; succeeded/update_verified; overall `complete-for-review` | set5-name-pinning: passed, set5-name-pinning-at-the-end: passed | `update-alpha81/upd1-debian13-mgmt-off-reboot/run-a/result.json`, `S/*/step.json` |

## Good update (Debian 13, Ubuntu 24.04, Arch)

The update is verified about a minute after the owner's start on each platform (`timeline.md`): Debian start
17:23:11Z, `succeeded/update_verified` read 17:24:10Z; Ubuntu 17:44:15Z / 17:45:16Z; Arch 18:11:09Z / 18:12:00Z. The
guest sampler (one probe every 5 s) has one Panel outage window per cell, inside the operation: 20-30 s (Debian,
Arch), 25-35 s (Ubuntu). Site and cron: `never-interrupted` on all three; SMTP: `never-interrupted` on Debian and
Ubuntu, not seeded on Arch (no mail there); DNS is not provided by these cells (`S/NN-verdicts/step.json`,
`workloads`). At the end
the identity files of Agent and Panel read `version=v0.1.0-alpha.82 commit=8c2250b05132fa91291a33bd23924d0aefd72617`
on all three (`S/NN-terminal/step.json`, `builds`).

The facts after the verified update (step `post-update-facts`; generated from the cells in `part2-table.md`, where
each cell's answers are quoted in full):

- **(a)** The ledger is the released 43: 43 contiguous rows whose digest equals the one `populated_database.py` pins
  for schema 43, the schema SQL equals the pinned schema 43, `request_identities` exists, `integrity_check` is `ok`
  (verdict `as-expected`, all five facts true, three platforms).
- **(b)** `POST /api/v1/domains/{id}/backups` sent as a page opened before the update sends it (no
  `X-CelikPanel-Request-Id`): `428 REQUEST_ID_REQUIRED` and no archive written. The same request with the header:
  `200`, one new archive (archives before / after the refusal / after the accepted request: 0, 0, 1), one
  `request_identities` row `done` (three platforms).
- **(c)** The versioned writes: a scheduled task (`ct1-` version read, sent, a new version read back; the line is in
  the site account's crontab once), the automatic backup schedule (`bs1-`), and on Debian and Ubuntu the server mail
  policy (`mp1-`; `postconf -h smtpd_client_message_rate_limit` = `37`). The same writes without a version answer
  `409 SETTINGS_VERSION_REQUIRED` (`scheduled_tasks`, `backup_schedule`, `mail_policy`) and change nothing. On Arch
  the mail policy is not measured (no mail stack there).
- **(d)** The Panel's deferred startup mail work completed in its first attempt, once: Debian `panel[37625]` started
  17:23:58.43, attempt line 17:24:40.42; Ubuntu `panel[55397]` 17:45:05.01 / 17:45:47.50. Arch: not measured (no mail
  stack).
- **(e)** The root CLI and the Panel's recovery reader both read `succeeded/update_verified`; the update card rendered
  from the candidate's own rules is judged `as-expected` (three platforms).

Every one of these values is equal to set3's for the same cell (`compare-with-set3.md`), the setting versions
(`bs1-...`, `ct1-...`, `mp1-...`) included.

## Item 9 (set4): a site created by the published alpha.81, across the update

Cells `upd1-debian13-good` and `upd1-ubuntu-good`, set4's two steps unchanged
(`S/09-set4-php-site-before-the-update/`, `S/18-set4-php-site-after-the-update/`, each with `step.json`, one `native/`
file per request and moment, and `native-text/vhost-*.conf.txt`); generated view `item9.md`. Both steps `passed` on
both platforms.

- **Before the update.** The installed baseline (`GET /api/v1/panel/version`: `v0.1.0-alpha.81`, commit
  `a0beb726...`) creates the PHP site `set4-upd-php.test` through `POST /api/v1/domains/create` (200). The owner
  uploads one PHP page and one text file (owner action, recorded as such). The vhost alpha.81 generated includes
  `snippets/fastcgi-php.conf` and `fastcgi_params`; `nginx -t` exits 0; the page is executed as the site's own account
  `set4_upd_php_test` by PHP 8.4.26 (Debian) and 8.3.6 (Ubuntu).
- **After the verified update, before any owner action.** The vhost file is no longer the baseline's (SHA-256
  `551945c2...` to `83bd07de...` on Debian, `1a392835...` to `4cc697cf...` on Ubuntu): the candidate's Panel rendered
  it when it started (`S/19-collect/journal-product.txt`: "certificate startup reconcile: restored 2 hosted vhosts with
  one nginx validation and reload", Debian 17:23:58Z, Ubuntu 17:45:05Z). It no longer includes the snippet; its PHP
  location holds the six directives set4 names, then `fastcgi_pass`, `SCRIPT_FILENAME` and `include fastcgi_params;`.
  `nginx -t` exits 0.
- **After the vhost was rendered again by an owner's action.** The site's General settings are saved unchanged (`GET`
  then `POST /api/v1/domains/{id}/general`, 200 `{"status":"success"}`). The file has the same bytes, a new inode and
  new times (Debian inode 141382 to 141387, Ubuntu 262669 to 263478), and nginx's journal has one reload at that
  moment. `nginx -t` exits 0; the page is still executed as `set4_upd_php_test`.
- **The ten requests.** For each of them the HTTP status and the SHA-256 of the body are equal before the update,
  after it and after the save, on both platforms (20 rows `True | True` in `item9.md`): the PHP page, the page with a
  query, the three PATH_INFO forms and `/` answer 200, the missing script 404, the dot file 403, the text file 200.
  A missing static file (`/set4-none.txt`) answers 200 with the body of `/` at all three moments, on both
  platforms: the same before and after, so not a change of this update; whether that answer is intended was not
  looked into.

This is what set4 measured on `557b554eb` (its item 9), again with the same answers; the two later product commits
that touch other code (`e2be8af30`, `1f182a483`) and `67b62cc0f` did not change it. Not measured: a site with a
certificate, an owner-edited snippet file, Arch (alpha.81 cannot create a PHP site there).

## Postfix Stop through the Panel while `postfix check` refuses `main.cf` (set4's M10; Debian 13, Ubuntu 24.04)

Where it ran: not in a fresh-install cell. It is the last measuring step of the two good-update cells
(`S/21-m10-postfix-stop/`: `section.json`, `api/`, `native/`, `journal/`), after `collect` and `verdicts`, so the
candidate under it is the one the update installed (`version=v0.1.0-alpha.82 commit=8c2250b0...`, Agent and Panel)
and the cell's own outage windows are those of the update only. The section's code is set4's
(`Set4Trial.m10_postfix_stop`), unchanged. Generated view: `m10.md`. One Stop per platform.

The owner adds the line `default_process_limit = 200 # raised for the campaign` to `main.cf` (owner action);
`postfix check` refuses it. Then `POST /api/v1/service/action {"name":"postfix","action":"stop"}`:

| | Debian 13 (17:25Z) | Ubuntu 24.04 (17:47Z) |
| --- | --- | --- |
| Answer | `200` `{"applied":"stopped","outcome":"verified","success":true,"note":{...}}` | the same |
| `note` | `code: SERVICE_ACTION_NOTE`, `reason: unit_marked_failed_config`, `vars.failed_unit: postfix.service`, `result: exit-code`, `command: sudo systemctl reset-failed postfix.service`, `detail: postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign`; the sentence is the documented one for the reason | the same, with `failed_unit: postfix@-.service` and `command: sudo systemctl reset-failed postfix@-.service` |
| Units before the stop | `postfix.service` `active`, `postfix@-.service` `inactive` | both `active` |
| Units after the answer and 8 s later | `postfix.service` `failed` (result `exit-code`), `postfix@-.service` `inactive`, at both readings | `postfix@-.service` `failed` (result `exit-code`), `postfix.service` `inactive`, at both readings |
| Master after the answer | not running | not running |
| `reset-failed` | none: the unit is `failed` at both readings and the product's journal of the window names no `reset-failed` | none, by the same two readings |
| Start after the owner restored `main.cf` | `200` `{"applied":"started","outcome":"verified","success":true}`; the master runs; no unit `failed`; ports 25 and 587 answer | the same |
| Checks of the section | 11 of 11 passed | 11 of 11 passed |

On Ubuntu, where set4 measured the answer without the `note` (its item 10, before `1f182a483`), the journal of this
run has `postfix@-.service` "Stopping" at 17:47:04.023, `postmulti` "fatal: bad numerical configuration" at .029,
"Control process exited, code=exited, status=1/FAILURE" at 17:47:05.031, "Failed with result 'exit-code'" at .035,
and the Panel's own line `[200][service action] stop postfix: SERVICE_ACTION_NOTE unit_marked_failed_config:
postfix@-.service` at 17:47:07.799 (`journal/postfix-since-the-stop.txt`).

What this does and does not show about `67b62cc0f`: the Stop path that reads Postfix's queue directory with the new
one-value rule answers as expected on both platforms while `main.cf` is refused. What the Agent's `postconf` reading
returned (a value, or unknown) is not recorded by this section; it is seen only through the answer. The other paths
`67b62cc0f` changes (the snapshot before a mail TLS change, the restore, the read-back) were not exercised by any
cell: no mail certificate is selected in these labs.

## Automatic return to alpha.81 (migration defect with a second fault; start check)

Four cells, each ending `recovered/rollback_verified` with alpha.81's Agent and Panel installed and running
(`version=v0.1.0-alpha.81 commit=a0beb7263...`, `S/NN-terminal/step.json`). Times from each `result.json`
(`outcome.attempts`, `outcome.reboot`) and `timeline.md`:

| | Debian 13, migration defect | Ubuntu 24.04, migration defect | Arch, migration defect | Debian 13, start check |
| --- | --- | --- | --- | --- |
| Owner start | 17:58:14Z | 18:29:16Z | 18:42:07Z | 18:54:11Z |
| Failure line of the update (`journal-product.txt`) | `code=update_failed state=recovery_required reason=offline panel database migration failed; its original database and work evidence are preserved`, 17:58:59 | the same, 18:30:01 | the same, 18:42:41 | `code=candidate_panel_startup_check_failed state=recovery_required reason=new panel start check failed before completion: panel startup check failed: tls_pair_invalid ...`, 18:55:05 |
| Attempt 1 (`update/active`, rollback) | 17:59:01 | 18:30:02 | 18:42:42 | 18:55:07 |
| Second fault | QMP `system_reset` once at `payload_restored`; new boot, SSH back after 8.8 s | the same; 11.0 s | the recovery child killed (SIGKILL) at `runtime_verified` (`recovery_fault_events`: armed, freeze, checkpoint verified, kill sent, released) | QMP `system_reset` once at `payload_restored`; new boot, SSH back after 8.5 s |
| Attempt 2 | `rollback/active`, 18:00:05 | `rollback/active`, 18:31:10 | `rollback/completion`, 18:43:31 | `rollback/active`, 18:56:10 |
| `recovered/rollback_verified` read | 18:00:32Z | 18:31:30Z | 18:43:46Z | 18:56:31Z, `failure_code: candidate_panel_startup_check_failed` kept |
| Panel outage windows (sampler, s) | 105-115, cause host reset | 104-114, cause host reset | 30-40 and 5-15 | 109-119, cause host reset |
| Site, SMTP (`S/NN-verdicts`) | interrupted only by the host reset | interrupted only by the host reset | site never interrupted; no mail | interrupted only by the host reset |
| Cron | never interrupted | never interrupted | never interrupted | never interrupted |

After the return (step `post-return-facts`, every cell; quoted in full in `part2-table.md`):

- **alpha.81 serves**: `GET /api/v1/panel/version` answers `200`, `v0.1.0-alpha.81`, `schema_version: 42`, Agent and
  Panel of one commit.
- **The ledger is the released 42** (42 rows, verdict `as-expected`, the five facts true: the ledger and schema digests
  equal the pinned ones, `request_identities` does not exist, `integrity_check` `ok`), and the database equals its
  pre-update digest except the two volatile tables (`equal-except-volatile`: `metrics_samples`,
  `server_setup_executions`; 65 tables compared, schema equal). In the start-check cell this is the return of a
  database that the candidate's updater had published at 43 before its start check failed; as in set3, the live
  ledger was not read in between.
- **Three views agree**: root CLI `recovered/rollback_verified` with previous failure `update_failed`; the Panel's
  recovery reader `200`, the same pair; the update card rendered from alpha.81's own rules, state `rolled_back`:
  "Update not completed; previous version restored || The update to v0.1.0-alpha.82 did not complete. The server was
  returned to v0.1.0-alpha.81 automatically and is running it now. || Cause: the update failed before it completed;
  the server recorded no more specific cause. || ... || The server reported: reviewed updater failed: exit status 1:
  offline panel database migration failed; its original database and work evidence are preserved". In the
  start-check cell the cause line is "Cause: the new version's panel failed its start check before anything was
  switched on."
- **Recorded beside it, as in set3 (its observation O18)**: the raw update status is `failed` with the updater's line
  as `summary`; `GET /api/v1/panel/update/check` still answers `available: true` for the same `v0.1.0-alpha.82` with
  `previous_attempt.phase: recovered`; the release floor file stays at `sequence=82 / v0.1.0-alpha.82` after the
  return.
- The restored alpha.81 Panel's deferred mail work completed in its first attempt (Debian migration cell `panel[5231]`
  18:00:26 / 18:01:04; Ubuntu `panel[6235]` 18:31:24 / 18:32:03; Debian start-check cell `panel[4738]` 18:56:25 /
  18:57:03). Arch has no mail stack.
- **Start check: the kind's 12 rules are all as expected** (`result.json` `kind.judged`: `rolled-back`,
  `status-failure-code`, `update-failure-line`, `fixture-reason`, `sidecar`, `no-completion-marker`,
  `rollback-dispatch`, `update-card`, `old-release-running`, `database-equal`, `cli-returned-text`, `web-catalogue`; no
  finding, none unknown).

Every compared value of these four cells equals set3's (`compare-with-set3.md`): the order and kind of the recovery
attempts, the reset, the ledger and schema digests, the views, the card's English text, the CLI's sentences in
English and Turkish, the update check and the version after the return.

## Owner continuation (Debian 13, Ubuntu 24.04)

Good candidate; an owner-fixable host cause: a lab process holds `127.0.0.1:2083` from the moment the updater stops
the old Panel, so the new Panel cannot bind, the update pauses after its three forward attempts, and the harness then
does what the product's text tells the owner (free the port, run the one printed retry once). Times from each
`result.json` (`outcome.attempts`, `outcome.port_hold`, `outcome.final_status`) and
`S/13-owner-continuation-required/step.json`.

| | Debian 13 | Ubuntu 24.04 |
| --- | --- | --- |
| Owner start | 19:08:17Z | 19:36:09Z |
| Forward attempts 1, 2, 3 (`update/completion`) | 19:10:06, 19:11:48, 19:13:29 | 19:38:05, 19:39:47, 19:41:38 |
| Pause read on the root CLI (`automatic_recovery: paused_retry_limit`); the Panel's recovery reader at that moment | step started 19:15:29; reader unavailable (the Panel is down) | step started 19:44:01; reader unavailable |
| Panel log the text names (`sudo journalctl -u celikpanel-panel -n 50`) | `Failed to start panel listener: listen tcp :2083: bind: address already in use` | the same |
| Renewal timer against before the update (`certbot.timer`) | recorded `on`, verdict `as-before` | the same |
| Port held; released by the owner before the retry | 433.9 s, released 19:15:53 | 471.0 s, released 19:44:23 |
| The printed command, run once: `/usr/libexec/celikpanel/recovery recover --retry --snapshot <snapshot>`, exit 0 (`Recovery dispatch admitted: attempt=owner`) | owner attempt 19:15:58, finished 19:16:40 | owner attempt 19:44:27, finished 19:44:52 |
| End | `succeeded/update_verified`, `previous_failure: recovery_failed`, read 19:16:40Z | the same, read 19:44:52Z |
| Site, SMTP, cron (`S/NN-verdicts`) | never interrupted | never interrupted |
| Deferred mail work of the Panel started inside the retry | `panel[57754]` 19:16:21, attempt 1 completed 19:17:05 | `panel[78472]` 19:44:36, completed 19:45:18 |
| The kind's 17 rules (`result.json` `kind.judged`) | `as-expected`, no finding, none unknown | the same |

Root CLI at the pause, EN (the candidate's text; `views.cli.texts`, Turkish beside it): "The update was applied, but
the new version's panel did not come up, and completing the update was retried to its limit. Read the panel log on
the server: sudo journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed.
There is no supported return to the previous version from this point. Automatic recovery used all three attempts
without finishing, so the server may be between versions. The server owner must act: read sudo journalctl -u
celikpanel-release-recovery.service --no-pager -n 50, fix the cause it names, then run the one-time same-operation
retry command shown there. Checking status does not retry recovery. ..." The post-update facts (a)-(e) are not part
of these two cells, as in set3.

## Management off across a reboot (Debian 13)

Cell `upd1-debian13-mgmt-off-reboot` (`S/15-management-off/` to `S/18-management-return/`; `result.json` `kind`). After
the verified update (`succeeded/update_verified` read 19:57:03Z) the owner runs `sudo systemctl disable --now
celikpanel-panel.service celikpanel-agent.service` (exit 0; both units `disabled` and `inactive`) and `sudo systemctl
reboot` (a new boot id; SSH back after 8.3 s). For the 185 s window of the new boot with both units `disabled` and
`inactive` (38 samples, one every 5 s; `workload-management-off-after-reboot.json`):

- the owner's database row and SMTP are served from the first sample, 7.8 s after boot; the site's first sample
  fails and it is served from the second, 12.8 s after boot, with no failure afterwards (set3's cell reads the same
  way: one failing sample before the first good one, at 13.0 s);
- cron wrote 3 stamps in the boot, no stall;
- `certbot.timer` stayed `enabled` / `active` (verdict `as-before`);
- `table inet celikpanel_fw` is present and equal to before;
- nothing needed the Panel (`needed_panel: []`).

`sudo systemctl enable --now celikpanel-agent.service celikpanel-panel.service` brings management back: the Panel
answers `starting` at 20:00:51Z and `ready` at 20:01:06Z (6 reads), a fresh login works, and the owner's state
(domains, cron, mailbox, database, version, update and recovery status) shows no difference from before
(`differences: []`). The kind's 11 rules are `as-expected`, no finding, none unknown. The update part of the cell:
site, SMTP and cron never interrupted, the Panel down only during the transaction (`S/20-verdicts/step.json`, judged on
the samples up to management-off). The post-update facts (a)-(e) are not part of this cell, as in set3.

## set5 against set3's Part 2, cell by cell

set3: candidate `cfa329676`, `evidence/set3-20261012/part2-alpha81/<cell>/run-a`. set5: candidate `67b62cc0f`, the one
run of each cell. Compared by `tools/summary.py` (`compare-with-set3.md`; the values of both sides are in `facts.json`
under `cells.<cell>.compared_values`): the verdict of every step set3 ran, by name; the outcome class and the final
state; from `result.json` the recovery attempts in their order and kind, the owner's retry, the port hold, the VM
reset or the owner's reboot, the final status with its previous failure and failure code, the judged kind with the
number of its rules, the finding lines; and the facts of the steps `post-update-facts`, `post-return-facts` and
`terminal` that hold no time and no identifier (ledger and schema digests, the guard's and the versioned writes'
answers, the views, the card's English text, the CLI's sentences, the update check and version after a return,
identity, login, firewall, site, SMTP). `same` means the listed values are equal on both sides and at least one side
holds a value. Not compared: times, durations, request ids, sample series, journals.

| Cell | set3 | set5 (run) | Steps of set3: verdicts | Outcome / final state | Compared facts |
| --- | --- | --- | --- | --- | --- |
| upd1-debian13-good | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (17 steps) | same: update-verified/succeeded/update_verified | result.json: same (final_status, findings, owner_attempts, owner_continuation); post-update-facts: same (agreement, deferred_mail, guard, ledger, versioned_writes); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker, smtp) |
| upd1-ubuntu-good | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (17 steps) | same: update-verified/succeeded/update_verified | result.json: same (final_status, findings, owner_attempts, owner_continuation); post-update-facts: same (agreement, deferred_mail, guard, ledger, versioned_writes); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker, smtp) |
| upd1-arch-good | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (16 steps) | same: update-verified/succeeded/update_verified | result.json: same (final_status, findings, owner_attempts, owner_continuation); post-update-facts: same (agreement, deferred_mail, guard, ledger, versioned_writes); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker) |
| upd1-debian13-defective | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (17 steps) | same: recovered-automatically/recovered/rollback_verified | result.json: same (automatic_attempts, final_status, findings, owner_attempts, owner_continuation, vm_reset); post-return-facts: same (card, card_texts_en, cli_sentence_en, cli_sentence_tr, database_against_the_pre_update_digest, ledger, offered_again, version_after_the_return, views); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker, smtp) |
| upd1-ubuntu-defective | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (17 steps) | same: recovered-automatically/recovered/rollback_verified | result.json: same (automatic_attempts, final_status, findings, owner_attempts, owner_continuation, vm_reset); post-return-facts: same (card, card_texts_en, cli_sentence_en, cli_sentence_tr, database_against_the_pre_update_digest, ledger, offered_again, version_after_the_return, views); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker, smtp) |
| upd1-arch-defective | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (16 steps) | same: recovered-automatically/recovered/rollback_verified | result.json: same (automatic_attempts, final_status, findings, owner_attempts, owner_continuation); post-return-facts: same (card, card_texts_en, cli_sentence_en, cli_sentence_tr, database_against_the_pre_update_digest, ledger, offered_again, version_after_the_return, views); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker) |
| upd1-debian13-startcheck | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (18 steps) | same: recovered-automatically/recovered/rollback_verified | result.json: same (automatic_attempts, final_status, findings, kind_judged, kind_rules, owner_attempts, owner_continuation, vm_reset); post-return-facts: same (card, card_texts_en, cli_sentence_en, cli_sentence_tr, database_against_the_pre_update_digest, ledger, offered_again, version_after_the_return, views); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker, smtp) |
| upd1-debian13-owner-continuation | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (18 steps) | same: recovered-after-owner-continuation/succeeded/update_verified | result.json: same (automatic_attempts, final_status, findings, kind_judged, kind_rules, owner_attempts, owner_continuation, port_hold, printed_retry_command); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker, smtp) |
| upd1-ubuntu-owner-continuation | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (18 steps) | same: recovered-after-owner-continuation/succeeded/update_verified | result.json: same (automatic_attempts, final_status, findings, kind_judged, kind_rules, owner_attempts, owner_continuation, port_hold, printed_retry_command); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker, smtp) |
| upd1-debian13-mgmt-off-reboot | complete-for-review | PASS (run-a; overall with the added steps: complete-for-review) | same (20 steps) | same: update-verified/succeeded/update_verified | result.json: same (final_status, findings, kind_judged, kind_rules, owner_attempts, owner_continuation, owner_reboot); terminal: same (build_identity_ok, firewall_equal, login_ok, running_matches_installed, site_marker, smtp) |

Differences in the compared facts:

none

**What the comparison does not cover.** Read at intake (2026-10-09) by comparing the step files of both sets with times, long hexadecimal identifiers and process ids masked. For the listed facts the sets are equal in all ten cells. Not listed, and not equal: the size of the offered candidate archive (`target.archive_size`) and the licence expiry in the update card; the name of the backup archive made by the guard check, the Panel's process id in the deferred mail work, and sample counts; in the Debian and Ubuntu good cells set5's `terminal` step also lists the item-9 PHP site next to `upd1-owner.test` (set3 had one site); in the start-check and owner-continuation cells the number of CLI samples per state and the set of web catalogue keys the pollers saw (the start-check cell: `recovery.phase.failed` is in set5's list and not in set3's; Debian owner-continuation: the reverse), because the pollers sample at fixed intervals; and every time, duration and outage window. None of this changes a step verdict, an outcome or a final state. The steps set5 added have no counterpart in set3, and nothing was compared beyond what is listed.

## Findings

**Fails (product).** None: no step of set3's method and no check of the added steps failed in any cell
(`checks-not-passed.txt`).

**Observations (not judged as defects here; each is in a raw file).**

1. **After an automatic return the same version is still offered** (set3's O18, unchanged): `GET
   /api/v1/panel/update/check` answers `available: true` for the `v0.1.0-alpha.82` that was just returned from, with
   `previous_attempt.phase: recovered`, and the release floor file stays at `sequence=82`; the card's own sentence
   tells the owner not to start it again. Four cells (`S/NN-post-return-facts/step.json`, `offered_again`;
   `S/NN-terminal/step.json`, `floor`). What the web build served after a return holds for the card's newer
   sentences (set4's item 12) was not read again here.
2. **The update itself renders every hosted vhost** when the candidate's Panel starts (set4's observation 4, again):
   a site generated by alpha.81 with the `snippets/fastcgi-php.conf` include is rewritten without it by the update,
   before any owner action (item 9).
3. **A missing static file of a PHP site answers 200 with the body of `/`** (`/set4-none.txt`), before the update and
   after it, Debian and Ubuntu (`item9.md`, request `missing_static`). The same on both sides of the update.
4. **While the update is paused with the Panel's port held, the Panel's recovery reader is unavailable and only the
   root CLI shows the pause** (a finding line of the two owner-continuation `result.json`, as in set3's two cells).
5. **`cron[...]: (CRON) DEATH (can't lock /var/run/crond.pid, otherpid may be ...)`** appears in the journal of the
   Panel's and the Agent's units on Debian and Ubuntu, not on Arch (counts per cell: `host/cron-death-lines.txt`);
   in the collected journals every one of them falls inside alpha.81's setup, and set3's journals hold them too (2
   and 17 in its Debian and Ubuntu good cells). With the candidate running, one such line is in each of the two M10
   reading windows (`S/21-m10-postfix-stop/journal/postfix-since-the-stop.txt`). Which program starts `cron` there
   was not looked into.
6. **Setup stops at `access_dns`** in every cell (`server_setup_access_dns_required`, a finding line of every
   `result.json`), as in set3: the guests have no public name. On Arch alpha.81's setup is run with purpose `web`
   (finding line: `web_mail` is accepted by the plan but fails at `05-mail_profile` on the published baseline).

**Harness.** H46 (above; cosmetic). No check of this run was found to have passed without measuring: the added
steps record the readings they judge (`name-pinning.json`, `section.json` with every check's detail), and the
comparison with set3 lists the compared values of both runs (`facts.json`, `cells.<cell>.compared_values`).

## Deviations from the brief

1. **The Postfix Stop ran in the two good-update cells, not in fresh-install cells.** A fresh install of the candidate
   needs a second build (the candidate source labelled as the installed version: five more dist directories, about
   6 GB written to the WSL disk while it builds) and two more guests; the host's free-disk rule left 1.9 GiB. The
   brief allows the update cell. Consequences: the candidate under the Stop was installed by the update, not by its
   own installer, and Postfix had been configured by alpha.81's setup; set4b's fresh-install record of the same
   section on `1f182a483` is not repeated on `67b62cc0f`. The section runs after `verdicts`, so the SMTP outage of the
   Stop is not in the cell's sample series; its own journal reading is in the step.
2. **The guests' disks were RAM-backed, and the base images were hard links** ("Disk" above). set3 and set4 kept
   overlays and image copies on the WSL disk. No duration of this run is comparable with theirs as a duration.
3. **Run copies leave out the retained evidence of earlier runs** (except `evidence/upd1-20261001`, which four offline
   tests read). set3 and set4 used the whole `git archive`. Each copy's `runcopy-against-commit.txt` shows that
   nothing else is missing or different.
4. **More was removed than the overlay disks**, all of it created by this run and each removal listed
   (`host/removals.txt`, `host/removals-build.txt`): the `src` directories of the three dist directories this build
   made (after the proof; 0.83 GB each), each lab's hard links to the cached base images, each lab's tmpfs
   mounts, the prepared overlay files no guest ever opened, and one bytecode file of this session. Nothing of an earlier run was removed, and nothing was removed to get above the disk floor: the removals
   on the WSL disk do not change the `C:` reading on this host.
5. **Four certificate-authority names were pinned, not two**: the two Let's Encrypt names of set3 and set4 and the two
   other directories the product's source names. `celikpanel.net` is pinned by the `origin` step as before, which is
   before the baseline is installed but after the first step of the cell.
6. **Added to every cell**: the first step and the last measuring step (name pinning and its reading at the end); in the eight cells
   of copy `e` the last measuring step also reads the kernel and package versions. These steps are in `result.json` and count
   in `overall`; the cell table judges set3's steps separately.
7. **set4's item 12** (the update check after an automatic return) was not run again; the brief asks for item 9 only.
8. **The two good cells of Debian and Ubuntu ran from copy `d`, the other eight from copy `e`** (the difference is the
   package reading and the name of one report field, H46).
9. **Evidence of the cells is under `update-alpha81/`** (set4's name), not `part2-alpha81/` (set3's).
10. **One thing is not as the brief words it**: "40 GB" was applied as 40 GiB (the stricter reading, the floor of
    set3 and set4).

## Not measured

- Anything on a screen: no browser was used. The update card, the recovery screen and the catalogue sentences quoted
  in the step files are rendered or looked up by the driver from the installed build's own rules and catalogues.
- The candidate installed fresh (its own installer, its own first setup): every cell installs alpha.81 and updates.
- The Postfix Stop on a fresh install of `67b62cc0f`, on Arch (no Postfix there), more than once per platform, and
  with the kernel trace of set4b. What the Agent's `postconf` reading returned.
- The other paths `67b62cc0f` changes: the snapshot before a mail TLS change, the restore, the read-back, with a
  `main.cf` that `postconf` warns about. No mail certificate is selected in these labs; no certificate authority is
  asked. set4c holds the readings of the real `postconf`; nothing here adds to them.
- Item 9 on Arch; a site with a certificate; an owner-edited snippet.
- set4's other items (1 to 8, 11, 12) and set3's Part 1 under this commit: not run again.
- A guest disk that is slow, full, or lost at a VM reset: the guests' disks were in RAM.
- The live ledger between the publication at 43 and the return in the start-check cell (as in set3: it follows from
  the updater's order and the recorded failure code, not from a reading).
- The guests' traffic: not captured. That no certificate authority and no licence service was contacted rests on the
  pinned names, on the Panel's own answer at the licence step, and on the absence of certificate routes among the
  recorded requests, not on a capture.
- The signed release path: the archives are test-licence builds signed by a per-lab fixture key and served by a
  fixture origin on the guest's loopback; no production key, no real origin.
- Whether the host slept inside a cell is answered from the host's event log and the watcher's readings ("Host
  power"), not from a hardware power record.
- More than one run per cell: each cell ran once.

## Removals, leftovers, secrets

- **Removed by this run, each listed** (`host/removals.txt`, `host/removals-build.txt`): the 17 overlay disks of this
  run's ten labs after each cell's checksums were verified on the staged copy (19.0 GB in all, on the labs' tmpfs
  mounts); the 17 tmpfs mounts; the labs' 17 hard links to the cached base images (the cached files stay, with the
  link counts they had: `host/host-leftovers.txt`); the 17 prepared overlay files no guest ever opened (197 KB each,
  they lay under the mounts) and the run's 17 staging copies of them; the `src` directories of the three dist
  directories this build made (0.83 GB each). One bytecode file this session's first smoke test wrote into the
  working tree's ignored `__pycache__` (`set5_redact.cpython-313.pyc`) was removed as well. Nothing else was removed.
  No directory or file of an earlier run was removed or changed; the alpha.81 dist directory of set3 and the image
  cache were read only. No shared directory was chmod'ed.
- **Left on the WSL host, with sizes** (`host/host-leftovers.txt`): the run directory `/var/tmp/cp-set5-run` (367 MB:
  five run copies, logs, staging copies); the ten lab directories `/var/tmp/cp-release-drill-s5-*` without disks or
  images (66 MB each: the lab's key, plan, evidence and its copy of the fixture origin's files); the builder's
  clone `/var/tmp/cp-upd1-build/20261009t170058z` (1.1 GB); the three dist directories this build made under
  `/var/tmp/cp-pair-accept/dist/` (`8c2250b0...`, `ecccd8d7...`, `49143bd1...`, 67 MB each). Two `__pycache__`
  directories are in run copies `d` and `e` (written when the trial scans imported `set5_redact`). No QEMU process,
  no job, no tmpfs mount and no listening port of this run is left.
- **Left in the repository's working tree**: this folder; the four new harness files and the change to `lab.py`.
  Nothing was committed or pushed; the branch head is where it was (`host/working-tree-status.txt`: `2ab7bf20c`). That
  file also shows `docs/RESILIENCE-CONTRACT*.md` modified: not by this run (the session's first `git status` listed
  only `ROADMAP*` as modified); no build and no cell read the working tree's product or docs files.
- **No installed server.** `host/installed-server-names-search.txt`: the terms set4's README lists (`celikhost`,
  `boston`, `frankfurt`, `2.25.80.4`, `72.62.38.15`, `185.95.0.123`) searched in every file of this folder outside the
  README and its parts; `frankfurt` occurs only in repository script names inside the run copies' file listings, the
  others nowhere. An absence of names, not a capture of traffic.
- **Secrets.** Redaction is at collection time: the pair redactor, set3's shape rules, and set5's token-digest rules
  in front of them, with one more pass over every file of a run before its result is written and one over the staged
  copy with the lab's raw values known (per run `set5-redaction-sweep.json`, `host/token-digest-sweep-at-staging.json`:
  counts only). In the four return cells the digest of the update-transaction token (`transaction_token_sha256`, and
  the same value as a directory name under `.release-db-migrations/` in a sudo journal line) is replaced by
  `[REDACTED-SHA256]` when the file is written. In the three return cells with a VM reset one host-side record
  (`recovery-fault-collection-*.json`, written by the lab's reset tool, not through the driver's redactor) held it;
  its plain-text places were redacted on the staged copy, 3 in each, before any checksum of this folder was written; the
  same value also lay twice inside the record's `events_base64` text, which that pass and `secret-scan.txt` did not look
  into, and was replaced at intake ("Corrections after intake"); the lab's raw copy stays on the WSL host. `secret-scan.txt` (its last line is
  the result) lists the classes: PEM private keys; the body lines of every lab key file; licence keys; WireGuard
  keys and configuration words; secret-named JSON fields; hash-shaped credential values; password assignments; token
  digests by shape and by value (3 values are read from the ten labs' raw records on the host and searched for in
  every retained file: 0 occurrences; the Arch return cell's value is in no raw record of the host, only in files the
  driver wrote redacted, so it is covered by shape only); HTTP credential headers; and, as information, local
  user-profile paths (6 occurrences, all of them the search pattern itself in `tools/secretscan.py` and
  `tools/stagecommon.sh`). The lab nonce (the
  random identity of a destroyed lab guest, passed to every guest helper) is in the records as in every earlier
  set and is not treated as a secret.

## Files

`README.md` (its text is written by hand in `tools/README.template.md` and `tools/readme-parts/`; `tools/mkreadme.py`
adds the four tables that only repeat values of the runs: cells, packages, pinning, comparison); generated by
`tools/summary.py`: `part2-table.md` (set3's table generator), `compare-with-set3.md`, `item9.md`, `m10.md`,
`pinning.md`, `timeline.md`, `api-routes.txt`, `host-clock-gaps.txt`, `sampler-gaps.txt`, `summary-per-cell.txt`,
`checks-all.txt`, `checks-not-passed.txt`, `facts.json` (the values this README quotes); `secret-scan.txt`; `SHA256SUMS` (every file of
this folder but itself; generated last and verified); `build/` (the build, proof, offline suites, dry runs);
`harness-run-copy/` (overlays, job files, queue and staging logs); `host/` (host check, disk readings, memory and
mounts, power events, progress, removals, leftovers); `update-alpha81/<cell>/run-<x>/` (the raw run of each cell:
`result.json`, `steps/`, the driver's `SHA256SUMS`, `set5-redaction-sweep.json`, and `host/` with the wrapper's
output, the job file, the lab's plan and the host-side records); `tools/` (every script this run used; the local
scratch-folder prefix in them is replaced by `<scratchpad>` at staging, the scripts as run held the full path).
Text files have LF line ends; `build/a81/build.err.txt` is the builder's raw log and holds two bare carriage returns
inside vite's progress line, as the same file of set3 and set4 does.

## Corrections after intake (2026-10-09)

Before this folder was committed it was read once more against its own raw files (this section is the only text written
after the first `secret-scan.txt`). What the intake covered: the sums of the root file and of the ten per-run files; a scan of
every file for private keys, key-shaped values, password-, token-, cookie- and licence-named fields (all 2550 JSON and JSONL
files were parsed and every secret-named field read), crypt hashes, and 32-, 40- and 64-digit hexadecimal values by class,
and for base64 text of 120 or more characters decoded and searched again; the outside names and local paths; the ten-cell
table, the ledger, the 428 / 409 answers, `item9.md` (all 20 rows recomputed from the 60 request files, and compared with
set4's request files: 60 of 60 equal), `m10.md`, the pinning table for all ten cells, the commit and tree identities (the
three fixture trees were rebuilt from the diffs in `build/a81/fixture-patches.diff` over `67b62cc0f` and equal the recorded
tree ids), the power and disk readings, and the comparison with set3 (all ten cells, step by step).

**Changed**

1. **A secret-class value was found and redacted.** The digest of the update-transaction token (`transaction_token_sha256`)
   lay twice, base64-encoded, inside the `events_base64` text of each of the three host-side records
   `update-alpha81/{upd1-debian13-defective,upd1-debian13-startcheck,upd1-ubuntu-defective}/run-a/host/recovery-fault-collection-*.json`
   (6 values in all, 3 distinct). Neither the staging pass nor `secret-scan.txt` could see them, because both read plain text
   only. Each value is now `[REDACTED-SHA256]` inside the encoded text, which is still valid JSON lines. A field
   `events_base64_redaction` was added to each record saying so; `events_sha256` was left as it was and is the digest of the
   events text before the replacement, so it no longer matches the stored text. No per-run `SHA256SUMS` lists `host/` files,
   so none changed. A search of every file for the three values in plain text and in all three base64 alignments finds 0
   occurrences. The Arch return cell's value was never in a host record and is covered by shape only, as before.
2. **README, wording.** "No installed server was touched or contacted: the harness has no route to one" now reads "No
   installed server was addressed by the harness: it holds no name or address of one, and none appears in the raw files"
   and says that no traffic was captured, so contact with an installed server is not excluded by measurement. The pin time
   range "18 to 56 s after the guest's boot" is "18 to 58 s" (the table gives 57.9 s). "The last step" of a cell is "the last
   measuring step": in four cells a `kind-expectation` step follows it. The licence step is "answered, by the Panel's own
   account, without a licence service". The lowest `C:` reading, 41.02 GiB, was at 20:02:49Z (the last reading), not 20:01Z.
   The statement that the staged copy had the three plain places redacted now says the encoded copies were missed and fixed.
3. **README, added limits.** On Arch the guest's name-service order asks `resolve` before `files`, and the pinning read-back
   asks the hosts file only, so the lookup an Arch program makes was not asked (Debian and Ubuntu list `files` first). The raw
   finding line "on an isolated host" is the harness's wording and not a claim of network isolation. A paragraph "What the
   comparison does not cover" lists facts that differ between set3 and set5 although the compared ones do not (archive size,
   licence expiry, process ids, sample counts, the set of web catalogue keys seen by the pollers, the extra PHP site in the
   two good cells). The same paragraph is in `compare-with-set3.md`, whose "latest run" now reads "the one run".
4. **Sources of the text.** `tools/README.template.md`, `tools/readme-parts/*.md` and `tools/summary.py` got the same
   wording changes, so that regenerating the README or `compare-with-set3.md` does not undo them; this section exists only
   in `README.md`.
5. **`secret-scan.txt`** has an intake block (class and count above, 2026-10-09). The root `SHA256SUMS` was regenerated last
   (same format and line order, not listing itself) and verified.

**Left as it is** (read, judged not a secret or not wrong, recorded here): the six installed-server search terms (three names and
three public addresses) appear in this README, `host/installed-server-names-search.txt`, `tools/searchnames.sh` and
`tools/readme-parts/LEFTOVERS.md` as search terms only, and the same addresses are already in the repository's test files;
the guests' SSH public key and its comment in each `host/fixture-plan.json` (a public key; the same in set3); the lab nonce;
`qualifier` values in `setup-execution.json` (the digest of a mail-renewal runtime script, not a credential); the per-run
`SHA256SUMS` files do not list that run's `host/` files (the root file does); the 15 to 17 such files per run are covered by
the root file only.
