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

@@RESULT@@

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
`e` = `d` + the package reading in the last step and H46 (the eight other cells) @@COPIES@@.
`PYTHONDONTWRITEBYTECODE=1`.

Harness defects found during the run (product code was never changed):

| Id | Defect | Handling | Runs |
| --- | --- | --- | --- |
| H46 | The first class of the redaction report was named `token_named_field`; the pair redactor blanks the value of any JSON key whose name contains `token`, so that count reads `"[REDACTED]"` in `set5-redaction-sweep.json`. | The class is `named_field` from copy `e`. No value of a run is affected, but that one count of the two runs cannot be read from their reports. The staging pass over the same files afterwards (`host/token-digest-sweep-at-staging.json`, not written through the redactor) found nothing left to remove in either run (0 places). | `upd1-debian13-good/run-a`, `upd1-ubuntu-good/run-a` (cosmetic) |
@@HARNESS_DEFECTS@@

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

@@PACKAGES@@

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
   exists yet; `preflight` and everything after it need this step. It ran @@PINTIME@@ after the guest's boot and
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
   (@@ACMEGREP@@).

The `access_dns` finding line in the raw `result.json` files says the wait happens "on an isolated host". That is the harness's
wording for a guest without a public name; it is not a statement that the guests were cut off from the network.

@@PINNING@@

## Disk, RAM-backed guest disks, host power

**The rule.** Before every guest start the newest Windows `C:` reading (PowerShell `Get-PSDrive C`, written every
30 s by `tools/cwatch.ps1`; WSL cannot run PowerShell on this host) had to be younger than 120 s and at least
40 GiB (42 949 672 960 bytes; the brief says "40 GB", read here as PowerShell's `40GB`, the larger of the two
readings). `tools/gate.sh` was asked twice per cell: by the queue before the lab was prepared and by the wrapper
immediately before the guests started (`host/c-drive-cells.txt`; per cell `host/c-drive-gate.txt`). @@GATE@@

**Readings (GiB).** 45.85 before any work (16:42Z); 45.56 when the build started; 41.89 when it ended: the builder's
clone (1.1 GB) and three dist directories (0.97 GB each) were written to parts of the WSL virtual disk that had not
been used before. @@DISK@@

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
overlay survives it as it does on a disk. @@MEM@@

**Host power.** `tools/keepawake.ps1` held a process-level keep-awake request for the run (`host/keepawake.log`) and
one idle WSL session was held. @@POWER@@

## Cells

@@CELLS@@

## Good update (Debian 13, Ubuntu 24.04, Arch)

@@GOOD@@

## Item 9 (set4): a site created by the published alpha.81, across the update

@@ITEM9@@

## Postfix Stop through the Panel while `postfix check` refuses `main.cf` (set4's M10; Debian 13, Ubuntu 24.04)

@@M10@@

## Automatic return to alpha.81 (migration defect with a second fault; start check)

@@RETURN@@

## Owner continuation (Debian 13, Ubuntu 24.04)

@@OWNER@@

## Management off across a reboot (Debian 13)

@@MGMT@@

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

@@COMPARE@@

## Findings

@@FINDINGS@@

## Deviations from the brief

@@DEVIATIONS@@

## Not measured

@@NOTMEASURED@@

## Removals, leftovers, secrets

@@LEFTOVERS@@

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
