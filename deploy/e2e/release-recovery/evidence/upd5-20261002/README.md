# upd5 native run, 2026-10-01 (folder dated for the batch 2026-10-02)

Roadmap item 3: the owner-started update acceptance driver (`owner_update_trial.py`), fifth attempt,
six cells planned (good, real-start and owner-continuation on Debian 13 and Arch). Disposable QEMU/KVM
guests on the local `archlinux` WSL host only. Product and harness commit `6cda60b8`.

**Result in one sentence: five of six cells completed; the Arch owner-continuation cell did not (run-a
inconclusive at the baseline install, run-b aborted by the host disk failure before any step).**

- Good candidate: `update-verified` on Debian 13 (59 s) and Arch (50 s).
- Real-start candidate (Debian and Arch): the start check passed, `completion.pending` was seen, the real
  start failed with `panel_start_unverified`, forward completion was attempted three times with
  `retry_scheduled` texts between attempts, the `pause_pending` window showed no "must act", and the
  update paused with `first_failure_code=panel_start_unverified`. The renewal sentence matched the
  pre-update timers (Debian on, Arch "already off"). The printed retry command was read, not run. Site,
  SMTP and cron were never interrupted. Kind measured as-expected (16 rules).
- Debian owner continuation: the port was held, three forward attempts, `pause_pending`, pause 391 s after
  the hold, the panel log named "bind: address already in use", the owner released the port and ran the
  printed retry once, the same request ended `succeeded/update_verified`; timers and firewall equal; no
  "Recovery failed" label after success. Kind as-expected (17 rules).
- Arch owner continuation: **not measured** (see "Arch owner continuation" below).

Not measured in this run: the live-probe re-read, the typed preflight path, the snapshot cause, and the
server line on a failed card.

Nothing here passes a P0 row. Every `result.json` carries `native_evidence: false`. The owner judges the P0
rows.

No installed server (Boston, Frankfurt, any) was touched. Nothing was pushed, published or signed with a
production key. The candidates were served only by the guest-loopback fixture origin (see "Real origin").

## How this folder was made (host disk incident)

The Windows `C:` drive that holds the WSL disk filled at about 04:26Z, during the start of the Arch
owner-continuation run-b. The WSL root filesystem went read-only with I/O errors and the distro would not
start; the run agent could not package the evidence. The pre-run disk check had measured the WSL-internal
disk (`host/host-df-before.txt`), not `C:`.

After space was freed on `C:` the distro started on 2026-10-01 with a clean journal recovery
(`EXT4-fs (sdd): recovery complete`, mounted read-write, no ext4 or I/O errors after boot). This folder was
then built by a separate agent, read-only on the labs and the run copy (`tools/rescue5.sh`):

- Each cell's driver evidence directory was copied **from the stopped lab**, not from the run agent's stage
  `/var/tmp/cp-upd5-run/stage/upd5-20261002`: in that stage the Arch owner-continuation copy had a 0-byte
  `result.json` and `SHA256SUMS` (written as the disk filled). The other five staged copies verified.
- Every per-cell `SHA256SUMS` was verified after copying into the repository (all six OK; 204, 273, 239,
  152, 223 and 14 entries). No lab file outside `images/` was empty or unreadable.
- `side/extract.txt` was regenerated from each lab with the run agent's `tools/ext5.py`; all six are
  byte-identical to the extracts the run agent had pulled during the run.
- `build/`, `harness-run-copy/` and `host/` follow the run agent's `tools/stage-common.sh`, written
  directly into this folder.

## Build and proofs

`run-upd1.sh build 6cda60b8` (from the run copy) ran 03:04:34Z to 03:07:59Z and exited 0 (`build/build.*`).
Clone `/var/tmp/cp-upd1-build/20261001t030434z/repo`, `go1.26.5 linux/amd64` (`build/go-version.txt`).

| Role | Version | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B | v0.1.0-alpha.81 | ad55cab84ce93627721040997870993cf91d81b2 | d0042a336ae464b2cf641c28d0ea8b0cc08e87d8 | c8f02220201aca986f2dc23c8a5a7434462adf1faba738f8eca421e17655029f |
| Good G | v0.1.0-alpha.82 (parent B) | d1b562d6fbe9749767dbbbe1f86ff7fbe2e4fc7c | 3080fd65fd9668a9a263462c09b3e568df4bc2fa | 0d37222c75367c0d4b486fa971f8700a6e9a5ed5b1a83aa644f09bcf60dcdb25 |
| Defective D | v0.1.0-alpha.82 (parent G) | 9f7bd8292b881e192512d129c9039b240d538259 | 39eb97271c8555ddc2f0b88248024a404a60a5b8 | a62d32166e513ba809451112649a3e0ccfc35633a65c45548b65766f74bbe95a |
| Start-check S | v0.1.0-alpha.82 (parent G) | cfa0cb33df89d29d02a00672bddc6c2d74e5204b | 6c1b913957d386671e85289b4eb50741cf4a35c2 | ec0321d6cf63299578e2190afe1e4e4971984ce30385f1cb41e524067f9f316b |
| Real-start R | v0.1.0-alpha.82 (parent G) | ed1f31e0ce0a2bff37ca0480f31d0442d4c82cc5 | 1160785534249e83faeb11dd7202faecf20f48a7 | 9fc535663c0f64c9a7773a22c1f2c7c1809fdca550f924c50d14253cb6b6292b |

- Source HEAD 6cda60b87e8144ae33aaa343d04a9c3923d8755e (`build/source-commit.txt`); fixture commits and
  patches in `build/fixture-commits*.txt` and `build/fixture-kind-patches.diff`.
- All five archives `license_mode: acceptance-fixture` (`build/dist-*.json`). D and S were built but no
  cell of this run used them.
- `prove` exited 0 (`build/prove.*`); the six dry runs carry `native_evidence: false` (`build/dry-run-*`).
- Offline suites on the 6cda60b8 run copy: `test_owner_update_trial.py` 161 tests OK,
  `test_recovery_candidate_archive.py` 11 tests OK (`build/offline-tests-*-6cda60b8.txt`).

## Run copies and harness corrections

The harness ran from `git archive 6cda60b8` at `/var/tmp/cp-upd5-run/harness` (file hashes:
`harness-run-copy/runcopy-6cda60b8-files.sha256`). The repository was not edited; `PYTHONDONTWRITEBYTECODE=1`.

| Id | Defect | Kind | Handling | Cells |
| --- | --- | --- | --- | --- |
| H16 | During the Arch baseline install, a `systemctl show` status read returned exit 127 while the installer's `pacman -Syu` was running; the driver's baseline poll treats one failed read as fatal. | possible harness defect, **not settled** (the host disk may already have been failing) | Run copy `harness-h16` (= 6cda60b8 + `mk-h16.py`): a failed read is recorded and read again 15 s later, five consecutive failures still stop the step (`harness-run-copy/H16-owner_update_trial.py.diff`). **Never exercised**: run-b aborted before any step. | arch-owner-continuation run-b only |

H16 records, and their state after the disk failure:

- The run agent's `H16-owner_update_trial.py.diff`, `runcopy-h16-files.sha256` and
  `offline-tests-owner-h16.txt` are 0 bytes on the host (written as the disk filled). The H16 offline
  suite result is therefore **not available**.
- The `harness-h16` run copy itself is truncated (its `cp -a` hit the full disk: files that are non-empty
  in `harness` are empty or differ in `harness-h16`); it must not be reused.
- Its `owner_update_trial.py` is complete: it parses, and it is byte-identical to `mk-h16.py` applied to the
  6cda60b8 file (whose SHA-256 94bcd1a5...f84838 equals `git show 6cda60b8:` of that path). The diff in this
  folder was regenerated from those two files on 2026-10-01 with the same `diff -u` command as `do-h16.sh`:
  one hunk, 31 lines.
- The job files for run-b (`job-cell-archocb.*`) are 0 bytes on the host and were not copied.

## Cells

One new lab per run, stopped by the wrapper.

| # | Cell | Lab | Start (UTC) | Outcome | Overall |
| --- | --- | --- | --- | --- | --- |
| 1 | upd1-debian13-good | upd5-d13-good-a | 03:09:28 | `update-verified` (59 s) | complete-for-review |
| 2 | upd1-debian13-realstart | upd5-d13-rs-a | 03:20:33 | `paused-owner-action-required`, 3 forward attempts; kind as-expected | complete-for-review |
| 3 | upd1-debian13-owner-continuation | upd5-d13-oc-a | 03:38:26 | `recovered-after-owner-continuation`, 3 + 1 owner; kind as-expected | complete-for-review |
| 4 | upd1-arch-good | upd5-arch-good-a | 03:56:25 | `update-verified` (50 s) | complete-for-review |
| 5 | upd1-arch-realstart | upd5-arch-rs-a | 04:06:29 | `paused-owner-action-required`, 3 forward attempts; kind as-expected | complete-for-review |
| 6a | upd1-arch-owner-continuation run-a | upd5-arch-oc-a | 04:22:29 | `not-terminal`: baseline-install inconclusive | incomplete |
| 6b | upd1-arch-owner-continuation run-b | upd5-arch-oc-b | about 04:26 | aborted by the host disk failure before any step; no evidence | - |

Per-cell steps with exact times are in each `<cell>/run-a/side/extract.txt` and `result.json`. Setup waited
at `access_dns` in every completed cell; Arch `web_mail` was refused
(`server_setup_service_unsupported:dovecot`), so mail is not provided on Arch.

Automatic forward attempts (`update/completion`):

| Cell | Owner start | Attempt 1 | Attempt 2 | Attempt 3 |
| --- | --- | --- | --- | --- |
| d13-rs | 03:30:17 | 03:32:09.0 | 03:33:50.2 | 03:35:31.2 |
| d13-oc | 03:47:08 | 03:48:59.9 | 03:50:40.7 | 03:52:21.6 |
| arch-rs | 04:14:14 | 04:15:57.7 | 04:17:37.9 | 04:19:18.7 |

- d13-rs final status 03:37:13 and arch-rs 04:20:58: `recovery_required/recovery_incomplete`,
  `automatic_recovery=paused_retry_limit`, `first_failure_code=panel_start_unverified`,
  `renewal_before_update` `on` (Debian) / `off` (Arch).
- d13-oc: port held 03:47:32.6, `owner-released` 03:54:38.8 (426.2 s, released before the retry),
  `succeeded/update_verified` 03:55:04.

### Arch owner continuation

- run-a: preflight and origin passed; `baseline-install` was **inconclusive** at 04:23:48: the guest status
  read (`systemctl show celikpanel-lab-current-worker-baseline.service ...`) returned exit status 127 while
  the installer's `pacman -Syu` was running. Every later step was not run; `collect` passed
  (`steps/14-collect/`). The baseline installer result and log were not written in that lab, so `host/`
  has no `current-worker-baseline-*` files for this cell. Whether the cause is harness defect H16 or the
  host disk already failing is **not settled**.
- run-b (harness-h16): its lab was created at about 04:26Z, as the disk filled; its key file is 0 bytes
  and it has no evidence directory. Nothing was measured and nothing from it is in this folder.

## Real origin

- The installer logs of the five completed cells name neither `celikpanel.net` nor `185.95.`
  (`secret-scan.txt`; the arch-oc run-a lab has no installer log).
- The only journal lines naming the name are the fixture origin unit's start and stop lines (the scan
  also lists one line of its own source code).
- The licence step's "not contacted" is counted in `secret-scan.txt` (that count covers the whole folder,
  including `tools/`).

## Secret scan

Run with the run agent's `tools/scan5.py` (upd4's `scan4.py` with the lab glob changed to upd5) and
`tools/scan.sh` (as upd4's), over the whole folder before hashing (`secret-scan.txt`). **Clean:**

- 0 PEM private-key blocks; 0 hits for the 343 body lines of 25 key files (each lab's SSH key and the fixture
  signing, CA and TLS keys; the run-b key file is empty).
- 0 hits for the fixture licence literal; 0 `CPK-` keys.
- 0 unredacted password, secret, token, session or cookie fields; 732 redaction markers.
- The 8 tokens of the admin-password shape are a snapshot-name prefix of d13-oc
  (`20261001T034730Z-from-unknown-to-d1b562d6fb`) and upd4 evidence file names in the run-copy hash list;
  0 tokens of the 32-character shape.
- `185.95.` appears only in this README, the scan scripts in `tools/` and the scan output.
- Longest path from the repository root: 196 characters.

## Files

- `build/`: artifacts document, dist JSONs, prove, dry runs, build and offline logs, fixture commits.
- `harness-run-copy/`: run-copy hashes (6cda60b8), the regenerated H16 diff, `mk-h16.py`, `do-h16.sh`, job
  scripts and the scripts that made them.
- `tools/`: the run agent's host-only readers and stage scripts, plus `rescue5.sh` and `scan.sh` used to
  build this folder.
- `host/`: the pre-run host check and disk reading.
- `<cell>/run-a/`: the driver's evidence directory unchanged (its `SHA256SUMS` verified after copying);
  `host/` wrapper logs, job and harness used, lab identity, owner-start and owner-continuation records,
  baseline installer result and log, worker-origin intent and manifest, fixture plan; `side/extract.txt`.
- `secret-scan.txt`, and `SHA256SUMS` over every file except itself.

## Host leftovers (archlinux WSL)

- `/var/tmp/cp-upd1-build/20261001t030434z`: the clone and archives of this build.
- `/var/tmp/cp-upd5-run`: run copies `harness` and the truncated `harness-h16`, logs, jobs, build records
  and the run agent's partial stage.
- Stopped labs `/var/tmp/cp-release-drill-upd5-{d13-good-a,d13-rs-a,d13-oc-a,arch-good-a,arch-rs-a,arch-oc-a,arch-oc-b}`.

No QEMU process was running when this folder was built. Nothing under `/var/tmp` or `/root` was deleted or
modified to build it; the repository was not written except this folder.

## What this run proves and does not prove

It shows, on disposable guests with the acceptance fixture and at 6cda60b8:

- the good update forward on both platforms;
- the real-start kind on both platforms: three forward attempts with `retry_scheduled` between them, no
  "must act" in the `pause_pending` window, the pause with its typed first cause, a renewal sentence that
  matches the pre-update timers, with site, SMTP and cron never interrupted;
- on Debian, the owner path: the owner released the port, ran the printed retry once, and the same request
  ended `update_verified`.

It does **not** show:

- the Arch owner continuation at 6cda60b8;
- whether H16 is a harness defect or a symptom of the failing host disk, or the H16 harness on any cell;
- the live-probe re-read, the typed preflight path, the snapshot cause, or the server line on a failed card;
- the defective and start-check kinds (not part of this run), management off and reboot, power loss, DNS
  (external mode), mail on Arch, certificate issuance or renewal execution, production signing.

It closes no P0 row.

## Corrections after intake (2026-10-10)

After this directory was published (2026-10-10), the operator's machine name was replaced by `<operator-host>` in the SSH public-key comments (and in the text that named it) of this directory: 12 occurrences in 6 files; the key material itself, a lab public key, is unchanged.
After this directory was published (2026-10-10), 5 digest values of the one-shot update-transaction token (5 in plain text in 5 files, as `transaction_token_sha256` values and as `.release-db-migrations/<digest>` directory names; 0 inside base64 `events_base64` text in 0 files) were replaced by `[REDACTED-SHA256]`.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.
