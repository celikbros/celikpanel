# Batch 10: resuming the pending parentless delete after the owner enrollment (z04 on the kept batch 9 overlay, z05 held and resumed), 2026-10-01

Scope: P0.4; D-025 invariants 1, 2, 4 and 5; D-022, D-024, D-026; acceptance register row 6 (exploratory). One native run of the two zero-zone lifecycle cells that batch 9 left pending, using the section "Pending parentless delete: resume after the owner's enrollment" of `deploy/e2e/dns-kill-matrix/README.md`. Harness, trigger, owner tools and (for `z05`) the product were built from commit `0d4c0324db38319173f73c57bc01a68e52c13d32` on the local **acceptance branch** `accept/pdns-primary-gate-open-6` (parent `f0910d6e`, the main line with the trigger's corrected child-zone phase match and `run-prepared … --resume-held-zone-lifecycle`; the gate-open commit sets `freshPairedPDNSPrimaryAdmitted` / `freshPairedPDNSPrimaryOffered` to true). Disposable QEMU guests on the local WSL `archlinux` host only: Debian 13 kill guest as the CelikPanel PowerDNS primary, Arch guest as the panel-free native BIND secondary. No installed panel, remote host, release or signed bundle was touched; nothing was pushed or published. **Nothing here passes an acceptance-register row; these are observations.**

**Result: in both cells the resumed deletion stayed pending.** The corrected trigger matched the pending job, resumed it (phase `recovering`, lease, attempt 2) and the Agent made its first authenticated inspector exchange with the enrolled secondary. The Agent then kept the same job pending with a new code, `dns_peer_inspection_unknown`. The re-add and (in `z05`) the reboot did not run. `z05` remains suspended on its first boot.

| Dir | Cell (`pdns-switch__…__paired-primary__peer-reachable`), product build | Command | Exit | Outcome |
| --- | --- | --- | --- | --- |
| `z04-resume` | `committed__after-write`, batch 9 overlay (`3cceb29a` product; trigger replaced by `0d4c0324`) | `zone-lifecycle … --zero-zones --recover-delete --execute` | **2** (7.39 s) | `zone-lifecycle-recover-2.json`: delete `pending_exact_operation`, trigger rc 75, job pending, `dns_peer_inspection_unknown`; re-add not run |
| `z05-held-resume` | `target-started__after-write`, fresh fixture, `0d4c0324` product | `run-prepared … --zero-zones --zone-lifecycle --reboot-after-recovery --disable-management-before-reboot`, then the same flags with `--resume-held-zone-lifecycle` | **2**, then **2** (8.57 s) | first run held (`zone-lifecycle-held.json`); resume: delete pending `dns_peer_inspection_unknown`, run still held, no reboot, no result file |

## Cell 1: z04 resume on the kept batch 9 overlay

- **Boot without touching product state.** `fixture.py start` from the batch 9 cell plan (`/var/tmp/cp-b9-1001/r2`, cell `7392457812ac089a866de6ec`) at 02:55:36Z, SSH at 02:56:27Z. No reinstall and no re-prepare. The batch 9 serial logs were copied to `pre-boot/` first, because QEMU truncates them at start. The primary booted as boot `e5177a51…` and the secondary as `31296a2f…`. Agent, Panel and PowerDNS were active; the product binaries were batch 9's (`agent 5c88339c…`, `agent.kill c03d4473…`, `panel c518939a…`).
- **State equals batch 9's record.** The ledger file hash `b23400df…` equals batch 9's retained `rec/raw/state/service-mutations.json`. Job `7fb6a0df9a0df4c78fa75b362d0a2988` was `pending` with attempt 1, no lease, `dns_peer_enrollment_required` and `updated_at` 01:27:53.087Z. Its phase was `commit/dns-zone-sync/v3/propagation-pending/7fb6a0df…/s2.s1-kill.test/dns-zone-sync/v3:sha256:e90fb984…` (child phase). `deadline_at` 02:12:50Z had already passed. Both `dns-peer-enroll` status commands reported `configured` (enrollment `79e54e59…`). The Agent did not act on the job at startup: its journal has no zone lines before the resume, and the first ledger change was the resume (`primary-state-after-boot.txt`, `raw/watch/cp-b10-watch-led`).
- **Trigger.** The harness runs `/opt/celikpanel/bin/dns-kill-trigger` **on the primary guest** over SSH. The trigger talks to the Agent over its public socket; it does not run on the host. The batch 9 trigger `3c60de67…` was replaced by `419315fe…` (this build), with the same owner and mode (`root:root 0755`; `trigger-replacement.txt`). The trigger identity receipt carries no binary hash, and no other guest file was changed.
- **Resume** (`zone-lifecycle-recover.log`, `fresh-primary-peer/zone-lifecycle-recover-2.json`, ledger sampler `raw/watch/cp-b10-watch-led/timeline.log`):
  - At 02:57:00.81Z the job was `running` at `…/recovering/…`, attempt 2, `lease_expires_at` 02:57:20.81Z. The Agent rewrote the ledger about every 150 ms with a 20 s lease (44 changes). The new deadline was 03:42:00Z.
  - 02:57:02.04Z: the primary transferred the catalog from the secondary (AXFR, serial 1790731620). A child AXFR at 02:57:02.27Z got `NOTAUTH`.
  - **First authenticated inspector exchange.** At 02:57:07.323Z the secondary logged `Accepted publickey for celikpeer from 192.0.2.10 port 33586`. At 02:57:07.522Z `sudo` ran `/usr/local/libexec/celikpanel-bind-peer-inspect` as root; the sudo session closed at 07.618Z and the connection closed at 07.661Z (`secondary-after-recover.txt`). Authentication and the fixed command worked.
  - **Outcome.** Agent journal, 02:57:07.623Z: "DNS zone V3 recovery remains pending for s2.s1-kill.test: paired DNS deletion is unverified (check=peer_native_zone); the peer administrator must check native zone state and DNS access, then retry verification of the same operation", followed by `dns_peer_inspection_unknown`. The job went back to `pending` at the same propagation phase, attempt 2, no lease. `finished_at` was 02:57:07.623Z, so the resumed attempt took 6.90 s. The trigger returned `pending_exact_operation` (rc 75, 1 heartbeat) and the harness verdict was `pending`. The re-add was not requested.
  - **The inspector's reason is not recorded anywhere.** The Agent journal does not contain it, and the ledger message is the generic "The exact local DNS publication is waiting for paired propagation recovery.". In the product source at `3cceb29a`, `cmd/agent/dns_engine_peer_native_linux.go:97-99` maps any error from `dnspeertransport.Inspect` to this code without logging the error. `bind-peer-inspect` prints only "native BIND peer observation unavailable" to stderr and exits 1.
  - **Likely cause (inferred, not observed).** `internal/bindpeerinspector/native_linux.go:84,96` runs `rndc -s 127.0.0.1 zonestatus`. On the secondary, `/etc/rndc.key` and `/etc/rndc.conf` do not exist, although `named` logs "configuring command channel from '/etc/rndc.key'" and listens on 127.0.0.1:953 (`secondary-rndc-prerequisite.txt`, read-only). `cmd/bind-peer-inspect/README.md` says a panel-free Arch BIND needs an owner-prepared `/etc/rndc.key`. Batch 9's enrollment sequence, repeated exactly here, does not create one. **Not worked around:** no key was created and nothing was re-run.
- **Extra read-only observations** (`extra-observe-after-pending.txt`; not a harness verdict). `native_pdns_bind_peer.py observe --zero-zones` and `observe-child --step delete --zero-zones` both exited 0. Both catalogs had zero members at serial 1790731620 on UDP and TCP. Both servers answered `s2.s1-kill.test` SOA with REFUSED (rc 5, no AA) over UDP and TCP. This is the locally published deletion from batch 9, unchanged.
- **Catalog.** The serial stayed 1790731620 during this boot. `dbwatch` retained a single database copy, so the database was unchanged, and PowerDNS logged no `new CATALOG-HASH` this boot. There was no daemon re-stamp.
- **Guests stopped** with QMP quit at the end, overlays kept (`stop.json`). The overlay now differs from batch 9's in several ways: a new boot, the replaced trigger, and the ledger at attempt 2.

## Cell 2: z05 fresh, held and resumed

Work root `/var/tmp/cp-b10-1001/r1`. The fixture's `init-root` refuses a non-empty directory without its marker, and `/var/tmp/cp-b10-1001` already held this run's logs. Images were hard-linked from `/var/tmp/cp-v3n28/images`. The run used batch 9's `cell.sh` flow plus a read-only ledger sampler. `GATEPROBE=0`.

- **Run until the hold** (03:05:32.57Z → 03:06:57.73Z, exit **2**).
  - Kill: marker-to-SIGKILL 12.3 ms, exit 137, `kill_proven`. No native entry inside the window (`boundary-window.txt`, computed from the checkpoint's result because there is no `result.json`).
  - Guest complete verdict: `passed` / `passed`, `target_converged`. `catalog_restamp.zero_zone_restamp` restamped: staged 1 → state 1790737549, served 1790737549 on both servers (UDP and TCP), `native_serial_at_cut` null. Gate probe `open`, host idle.
  - Pair verdict passed (members `[]`, 1790737549).
  - Lifecycle: add `verified_published` (job 3.07 s, child SOA 2026092801), edit `verified_published` (2.88 s, 2026092802). Delete: job `192554344c6206934b16138c9cca14ca` pending `dns_peer_enrollment_required` at the child's propagation phase (03:06:48.91 → 03:06:56.40Z).
  - Hold record: `fresh-primary-peer/zone-lifecycle-held.json` (peer verdict 0, `lifecycle_status: pending`).
  - Guest state: checkpoint `reboot-checkpoint-1.json` with stage `zone-lifecycle-before-reboot` (sha256 `12bf5dd1…`, written 03:06:34.28Z). No `result.json`, no controller process running, boot IDs unchanged (`held-state-before-enroll.txt`).
- **Owner enrollment, no reboot** (`enroll.log`, 03:07:33 → 03:07:39Z). This was batch 9's sequence with this build's packaged tools (`dns-peer-enroll 5b089fbc…`, `bind-peer-inspect b2f297df…`):
  - both status commands `disabled`;
  - `primary-prepare` (credential `9a648750…`);
  - public key saved root-owned 0600 on the secondary;
  - `secondary-install` `configured`;
  - `secondary-host-key` `2d12e15c…`, matching an independent digest of the host key (same SSH channel);
  - `primary-activate` (enrollment `fce0329c…`);
  - both status commands `configured`.

  Boot IDs were the same before and after. `/etc/rndc.key` was absent on the secondary (`secondary-rndc-prerequisite-before-enroll.txt`).
- **Resume** (`run-prepared … --resume-held-zone-lifecycle --execute`, 03:07:49.34Z, exit **2** after 8.57 s; `resume.log`):
  - The hold record was accepted and `zone-lifecycle-recover.json` was written. The job went to `recovering` with a lease, attempt 2, at 03:07:49.94Z.
  - 03:07:51.18Z: catalog AXFR from the secondary; the child AXFR got `NOTAUTH`.
  - Inspector exchange: `Accepted publickey for celikpeer from 192.0.2.10` at 03:07:57.783Z, and `sudo … celikpanel-bind-peer-inspect` at 03:07:58.017Z.
  - Agent, 03:07:58.080Z: the same "DNS zone V3 recovery remains pending … (check=peer_native_zone)" line and `dns_peer_inspection_unknown`. The job was pending again (attempt 2, 8.14 s).
  - The harness printed `zone_lifecycle_before_reboot: {"outcome": "held", "resumed": true}` and did **not** continue the guest. No reboot ran, management was not disabled, and there is no `peer-verdict-after-reboot.json`.
- **Suspended run.** The guest controller suspended at 03:06:34Z and was still suspended at the last check (03:13:04Z, about 6.5 min). Nothing disturbed the checkpoint: it had the same sha256 `12bf5dd1…` and the same boot IDs (`2bdbdb71…` / `bd579b8e…`) before enrollment, before the resume, after the resume and after the collect, and there was never a `result.json` (`held-state-*.txt`). `zone-lifecycle-held.json` was not rewritten by the resumed hold (same bytes and mtime). **The guests were left running on purpose** so that the same boot can still be resumed. Stopping them makes the run unresumable.
- **DNS during the delete** (`peer-dns-sampler-changes.txt`). The primary answered the child with REFUSED from 03:06:51.30Z. The secondary still served the child SOA 2026092802 until 03:06:55.75Z, then REFUSED. Both catalogs had zero members at 1790737610 from 03:06:51.3Z. The last sample (03:12:57Z) showed both at 1790737669, with the child REFUSED on both servers over UDP and TCP.
- **Catalog serial history** (`catalog-serial-history.txt`, pdns journal):

  | Time (Z) | Event | Serial | Members | Hash |
  | --- | --- | --- | --- | --- |
  | 03:05:49.73 | first start | 1790737549 | `{}` | `47DEQ…` |
  | 03:06:37.7 | add | 1790737550 | `{s2}` | unchanged |
  | 03:06:43.9 | edit | unchanged | `{s2}` | unchanged |
  | 03:06:49.786 | **daemon re-stamp** | 1790737609 | `{s2}` | new `1Rf5KuuzsJMQ…` |
  | 03:06:50.36 | delete | 1790737610 | `{}` | unchanged |
  | 03:07:49.888 | **daemon re-stamp** | 1790737669 | `{}` | back to `47DEQ…` |

  The two re-stamps came +60.05 s and +120.15 s after the first-start re-stamp. No samples ever saw 1790737609 (it lasted about 0.5 s). There was no further re-stamp through 03:12:57Z. Batch 9's `z05` did not show these re-stamps. This run is consistent with a periodic check about every 60 s that re-stamps when the member set differs from the stored hash; this is still an inference. The second re-stamp fell 0.55 s after the resume began and the ledger shows no catalog write by the Agent, but the timing alone does not separate the two.

## Notify journal (primary, read-only, `pdns-notify-journal-and-also-notify.txt`)

- **`z04`**, unfixed build `3cceb29a`, this boot only (6 lines; 41 over all boots):
  - 1 × "Notification request to host 192.0.2.11 for zone 'catalog…' received from operator" (02:57:01.98Z, no port);
  - 4 × "Received spurious notify answer … from 192.0.2.11:53";
  - 1 × "Notification for catalog-c000020a.celikpanel.invalid to 192.0.2.11:0 failed after retries" (02:57:36.62Z).
- **`z05`**, fixed build `0d4c0324` (11 lines):
  - 6 × "Notification request to host 192.0.2.11:53 for zone … received from operator";
  - 1 × "Received unsuccessful notification report for 's2.s1-kill.test' from 192.0.2.11:53, error: Server Not Authoritative for zone / Not Authorized" (03:06:38.78Z, before the secondary loaded the new member);
  - 4 × "Unable to queue notification … nameserver does not resolve!" (catalog NS `invalid.` ×3, child NS `ns1.s1-kill.test` ×1);
  - **zero** "spurious", **zero** ":0" and **zero** "failed after retries" lines.
- `also-notify=192.0.2.11` in `/etc/powerdns/pdns.d/celikpanel-cluster.conf:7` in both cells.

## Harness observations (quoted, not changed)

- The pending `next_step` always tells the owner to enroll, whatever the code. After enrollment, with `dns_peer_inspection_unknown`, it still says "the server owner enrolls the native BIND secondary for inspection (dns-peer-enroll --engine bind on both guests, as the Agent's error code names)" (`zone_lifecycle_pending_next_step`, `guest_bootstrap.py`). That guidance is inaccurate for this code.
- `zone-lifecycle-held.json` `next_step` reads "the Agent kept the deletion of s2.s1-kill.test pending ():", with an empty code. `continue_or_hold_after_lifecycle` passes only `{"domain": …}`.

These are guidance defects in the harness. They are not failures against product behaviour; the harness judged the product's pending state as pending.

## Build and fixture

- Everything was built from `git archive 0d4c0324` into `/root/cp-b10-src` with the same toolchain and flags as batch 9 (`build/build.log`): agent `371edc0d…`, agent.kill `042b8b8f…`, panel `280499f7…`, dns-kill-trigger `419315fe…`, recovery `60bd04be…`, checkers, bridge (`fe277c46…`, identical to batch 9), recovery runtime (manifest `fdcc379d…`), owner tools, and `oi-smstatus`. Against batch 9 everything differs except `schema17-bridge`. The `r1` artifact copy is identical (`build/artifacts*.sha256`). At the end the source tree equalled a fresh archive except the `__pycache__` left by the fixture helpers' imports (`build/source-tree-vs-archive.diff`; `web/dist` excluded).
- **Web:** `npm ci` + `npm run build` exited **0** (`build/web-build.log`; 105 files in `build/web-dist.sha256`).
- **Offline tests from the archive** (`build/offline-test-counts.txt`):
  - Python: **466** with each file run directly, **467** with `discover` or per module; all OK. The difference is `test_guest_recovery_probe.py` (16 direct, 17 as a module), as in batch 9. `test_zone_lifecycle_recover.py`: 11.
  - Trigger: **60** top-level PASS, 0 FAIL, including the three `TestFreshPrimaryZoneRecovery*` tests.
- Fixture tools: batch 9's scripts renamed `cp-b9` → `cp-b10` (`build/fixture-tools-vs-batch9.diff`). New in this batch:
  - `ledwatch.sh` (read-only ledger sampler, 0.1 s);
  - `z04boot.sh` / `z04recover.sh` / `z04observe.sh` / `z04collect.sh` / `z04stop.sh`;
  - `enroll10.sh` (batch 9's enrollment without the sampler restart and without any lifecycle command; the secondary hash glob now runs under `sudo sh -c`);
  - `runresume.sh` (the resume with the boot monitor);
  - `z05held.sh` (read-only held-state capture);
  - `rndcfacts*.sh`;
  - `boundcheck10.py` (reads the checkpoint's result);
  - `derive10.sh`, `zlsum.py`, `secretscan.py`.
- Versions: Debian `pdns-server` 4.9.17-0+deb13u1 (installed inside the measured operation in `z05`), Arch `bind 9.20.29-1` (`versions-*`).
- Repository `HEAD` (record only): `6eee3b5b` at the start and at the export (`build/repo-head-at-*.txt`). It was not built or tested.

## Secret scan

`secretscan.py` ran twice. The first pass, over the 556 exported files, had 0 hits. The final pass, over 563 files after this README and the tool copies were added (`build/secret-scan.txt`), had 1 hit: the scanner's own pattern literal `openssh-key-v1` in `build/fixture-tools/secretscan.py`. Neither pass found a private-key block (text or OpenSSH binary marker) or any credential, token, license-key or TSIG/rndc secret assignment. The `tsigkeys` and `cryptokeys` tables have `rows=0` in all 17 retained database copies. The primary's inspection key directory was listed, not read. `enroll.log` contains only the primary inspector's **public** key, its SHA-256, the credential and enrollment IDs and host-key digests. Nothing needed redaction.

## What remains on the host (`build/remaining-on-host.txt`)

- **`z05` guests are RUNNING** (QEMU pids 278/290, `/var/tmp/cp-b10-1001/r1`, fixed ports 2201/2202/23053), held on their first boot. They occupy about 8 GB of RAM and block any other fixture that uses those ports. To discard them: `python3 deploy/e2e/dns-kill-matrix/fixture.py stop --work-root /var/tmp/cp-b10-1001/r1 --cell-id pdns-switch__target-started__after-write__paired-primary__peer-reachable --execute` (from `/root/cp-b10-src`). After that the held run cannot be resumed. The guest-side watchers stop by themselves; the watcher runs at most 4000 s.
- `z04` guests are stopped with overlays kept in `/var/tmp/cp-b9-1001/r2/cells/7392457812ac089a866de6ec`. This was the one deliberate reuse; the only host-side writes there were the QEMU runtime files, `serial.log` and `fresh-primary-peer/zone-lifecycle-recover-2.json`.
- `/var/tmp/cp-b10-1001` (2.6 GB), `/root/cp-b10-{src,artifacts,tools}`, `/var/tmp/cp-b10-{build,webbuild,setup}.log`, `/var/tmp/cp-b10-triggertest.log`, `/var/tmp/cp-b10-webdist.sha256`. Nothing else under `/var/tmp` or `/root` was created or deleted; `/var/tmp/cp-pair4-work` and the other `cp-b9` overlays were left untouched.

## Deviations

- `z05` used the work root `/var/tmp/cp-b10-1001/r1` instead of `/var/tmp/cp-b10-1001` (reason above).
- In `z04` the batch 9 trigger on the guest was replaced by this build's trigger (see Cell 1). This is test tooling, not product state.
- The z04 boot script's QEMU check used `pgrep -x` with a name longer than 15 characters, so it matched nothing. No QEMU process was running beforehand (checked separately at 02:51Z).
- One read-only `wsl.exe` command with an inline `$` failed on quoting and changed nothing. All other commands went through script files.
- At the end, `finalize.sh` ran `git status --porcelain` on this folder from WSL, to confirm the folder is untracked. `git status` may refresh the stat cache in `.git/index`. It changes no tracked content, branch, stash or working-tree file, but it is a write inside `.git` that this run should not have made.
- The first `SHA256SUMS` attempt in `finalize.sh` listed its own temporary file. It was deleted and regenerated with `sums.sh` (temporary file outside the folder), then verified.
- No harness workaround was applied, no pass rule was changed and no cell was re-run.

## What this does not prove

The key question is not answered. With the corrected trigger the product did resume the exact pending deletion: it took a lease, and the owner-enrolled inspector channel authenticated and ran natively for the first time in this matrix, twice. But in both cells the Agent kept the deletion pending with `dns_peer_inspection_unknown`, so a **completed** parentless deletion after owner enrollment is still unexercised. The same is true of the recovered delete's `verified_published`, `observe-child --step delete` as a harness verdict, the re-add, and the `z05` management-disabled reboot with `peer-verdict-after-reboot.json` under the post-publication rule.

The missing-RNDC-key cause is inferred from source and host facts; the inspector's own reason was not observed, because neither the Agent nor the inspector records it. `z04` ran the `3cceb29a` product with the `0d4c0324` trigger; only `z05` ran the `0d4c0324` product. This was one run per cell on an unsigned acceptance-branch build with a Debian 13 PowerDNS 4.9.17 primary and an Arch BIND 9.20.29 secondary; it covered no other topology and no installed server. The ~60 s re-stamp remains an inference from timing. Nothing here opens the gate on the main line, closes P0.4, or passes an acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file in this folder, including this README, except itself.
