# Batch 12: the two zero-zone lifecycle cells on fresh fixtures, first native run of the re-stamp admission and the per-step peer proof budget, 2026-10-01

Scope: P0.4; D-025 invariants 1, 2, 4 and 5; D-022, D-024, D-026; acceptance register row 6 (exploratory). One native run of each zero-zone lifecycle cell that has a parentless deletion (`z04`, `z05`), on fresh fixtures, exactly as batch 11. The procedure is the section "Pending parentless delete: resume after the owner's enrollment" of `deploy/e2e/dns-kill-matrix/README.md`. New in the product under test: `21a84211` (PowerDNS daemon catalog re-stamp admitted at any point of a V3 operation; `dns_peer_owner_edit_unknown:<check>`), `d3d65353` (native peer proof per-step budget, fresh consume-once context, `dns_peer_proof_timeout`, new Agent log lines); harness `f084b1ed` (composite codes, `job_error_detail`).

Build: harness, trigger, owner tools and product were built from `git archive 2efc4de2` on the local **acceptance branch** `accept/pdns-primary-gate-open-10`. Its parent is `d3d65353` (main line); the gate-open commit sets `freshPairedPDNSPrimaryAdmitted` / `freshPairedPDNSPrimaryOffered` to true.

Test bed: disposable QEMU guests on the local WSL `archlinux` host only. The Debian 13 kill guest is the CelikPanel PowerDNS primary; the Arch guest is the panel-free native BIND secondary with the owner-prepared `/etc/rndc.key`. No installed panel, remote host, release or signed bundle was touched, and nothing was pushed or published. The pair7 run (`deploy/e2e/dns-pair-acceptance/evidence/pair7-20261001/`) ran before this batch on the same host; its guests were stopped before `z04` started. **Nothing here passes an acceptance-register row; these are observations.**

**Result: in both cells the resumed parentless deletion completed.** After the owner's enrollment, the Agent resumed the exact pending job (phase `recovering`, lease, attempt 2), ran the authenticated inspection, logged "outcome verified" and ended the job `succeeded` at `published`. The harness then passed `observe-child --step delete` (zero members, REFUSED on both servers over UDP and TCP) and the re-add; in `z05` the held guest run continued on the same boot, disabled management, rebooted both guests, and `peer-verdict-after-reboot.json` passed with members `[s2.s1-kill.test]` under the post-publication rule.

| Dir | Cell (`pdns-switch__…__paired-primary__peer-reachable`) | Commands | Exit | Outcome |
| --- | --- | --- | --- | --- |
| `z04-zero-committed-zl` | `committed__after-write`, fresh fixture `r1` | `run-prepared … --zero-zones --zone-lifecycle`, then `zone-lifecycle … --zero-zones --recover-delete --execute` | **2** (held pending, as expected), then **0** (27.63 s) | guest `passed`/`passed`, pair `passed`, add and edit `verified_published`, delete pending `dns_peer_enrollment_required`; after enrollment the resumed delete `verified_published`, `observe-child --step delete` passed, re-add `verified_published`/passed (`zone-lifecycle-recover.json` `status: passed`) |
| `z05-zero-started-zl-rb` | `target-started__after-write`, fresh fixture `r2` | `run-prepared … --zero-zones --zone-lifecycle --reboot-after-recovery --disable-management-before-reboot`, then the same flags with `--resume-held-zone-lifecycle` | **2** (held), then **0** (141.26 s) | guest `passed`/`passed`, pair `passed`, add and edit passed, delete pending `dns_peer_enrollment_required` → hold; after enrollment the resumed delete `verified_published`, delete observation passed, re-add passed; `zone_lifecycle_before_reboot {"outcome": "passed", "resumed": true}`; management disabled; both guests rebooted; after-reboot verdict `passed` |

## New Agent log lines (verbatim, primary Agent, all boots)

- `z04`: `2026/09/30 08:42:55 DNS peer inspector answer for s2.s1-kill.test (BIND secondary): catalog_state=transferred member_state=absent native_state=unloaded; accepted`
- `z04`: `2026/09/30 08:42:56 DNS peer proof for s2.s1-kill.test (BIND secondary) step times: prepare=2.125s challenge_write=5.157s exchange=527ms post_inspection=2.994s consume=6.454s; total 17.257s; outcome verified`
- `z05`: `2026/09/30 08:53:34 DNS peer inspector answer for s2.s1-kill.test (BIND secondary): catalog_state=transferred member_state=absent native_state=unloaded; accepted`
- `z05`: `2026/09/30 08:53:35 DNS peer proof for s2.s1-kill.test (BIND secondary) step times: prepare=1.466s challenge_write=11.07s exchange=684ms post_inspection=3.891s consume=7.032s; total 24.144s; outcome verified`

No "stopped at step", **no "admitted the PowerDNS daemon's re-stamp"**, no "observed different evidence", no `dns_peer_owner_edit_unknown[:*]`, `dns_peer_journal_unknown`, `dns_peer_proof_timeout` or `dns_peer_proof_internal` in either cell (`*/agent-journal-all-boots.txt`). The only pending lines are the first delete's "pdns zone publication remains pending for s2.s1-kill.test at epoch 1: paired DNS deletion is unverified (check=peer_native_zone); the peer administrator must check native zone state and DNS access, then retry verification of the same operation" + `dns_peer_enrollment_required` (z04 08:42:04, z05 08:51:26).

**The re-stamp admission was not exercised in either cell** (see "Catalog serial history"). `docs/DNS-ENGINE-ARTIFACT.md` expected the admitted line in `z04` because batch 11's `z04` re-stamp fell inside the resumed attempt. Here the daemon's periodic check (every ~60 s from its first start) never found a changed member set between the delete and the end of the resumed attempt in `z04`; in `z05` the post-delete re-stamp came before the resume (as intended, like batch 11's `z05`), so no admission was needed.

**Timing margin (observation, not a failure):** `z05`'s `challenge_write` took 11.07 s against its 12 s bound (`dnsPeerProofSteps`); the doc's estimate was 3.5–4.1 s, pair7 measured 4.97–7.42 s, `z04` 5.16 s. Both kill cells also run the read-only watcher, database and ledger samplers on the primary (copies on every change), which may add I/O; that is not measured. `consume` took 6.45–7.03 s (bound 20 s, estimate 3–4 s).

## Cell 1: z04 (committed cut, lifecycle without reboot)

Work root `/var/tmp/cp-b12-1001/r1`; cell directory `7392457812ac089a866de6ec`. `GATEPROBE=0`.

**Run** (08:40:50Z → about 08:42:05Z, exit **2**; `run-prepared.log`):
- Kill: marker 08:41:08.451Z, SIGKILL 08:41:08.487Z (35.3 ms), exit 137, `kill_proven`. No native entry inside the window (`boundary-window.txt`).
- Guest complete verdict `passed` / `passed`, `target_converged` (`native-and-outage.json`).
- `catalog_restamp`: staged 1, `native_serial_at_cut` 1790757666, state 1790757666, served 1790757666 on both servers over UDP and TCP, `zero_zone_restamp.restamped: true`; serial rule `pre-publication`.
- Pair verdict `passed`: members `[]`, 1790757666, `rndc_status_ok: true`.

**Lifecycle:** add `verified_published` (job 3.86 s, child SOA 2026092801, catalog 1790757667 on both); edit `verified_published` (3.27 s, 2026092802); delete job `7fb6a0df9a0df4c78fa75b362d0a2988` (the same deterministic request ID as batches 9–11) leased 08:42:01.4Z and went **pending** at 08:42:04.35Z with `dns_peer_enrollment_required` (printed `next_step` names the enrollment and `zone-lifecycle <same arguments> --zero-zones --recover-delete --execute`).

**Owner enrollment** (`enroll.log`, 08:42:30.8Z → 08:42:35.1Z), the batch 9–11 sequence with this build's packaged tools (`dns-peer-enroll 2ce8ff82…`, `bind-peer-inspect bacb58d8…`): both status `disabled`; `primary-prepare` (credential `d440d77c…`); public key saved root-owned 0600 on the secondary; `secondary-install` configured; `secondary-host-key` with DIGEST MATCH; `primary-activate` (enrollment `78cfcbe0…`); both status `configured`. Boot IDs unchanged; no lifecycle command in between.

**Resume** (`zone-lifecycle … --zero-zones --recover-delete --execute`, 08:42:37.076Z → 08:43:04.707Z, exit **0**, 27.63 s; `zone-lifecycle-recover.log`, `fresh-primary-peer/zone-lifecycle-recover.json`, ledger sampler `ledwatch-timeline-snapshot.txt`); 35.6 s after the delete was leased:
- **Lease:** 08:42:37.87Z `running` at `…/recovering/…`, attempt 2, lease to 08:42:57.82Z, `error_code` still `dns_peer_enrollment_required` while recovering (118 ledger samples while recovering).
- **Inspection:** `Accepted publickey for celikpeer from 192.0.2.10` at 08:42:46.9Z on the secondary (secondary clock), sudo `celikpanel-bind-peer-inspect` (`secondary-after-recover.txt`).
- **Outcome:** Agent lines above (08:42:55–56); job `succeeded` at `published/…` attempt 2, 08:42:56.75Z (18.98 s from resume start in the ledger), 3 heartbeats; trigger `verified_published`.
- **`observe-child --step delete --zero-zones`** passed: both catalogs (primary AXFR and the one the secondary loaded) at 1790757668 with **zero members**; `s2.s1-kill.test` SOA **REFUSED** from 192.0.2.10 and 192.0.2.11 over UDP and TCP.
- **Re-add** (job `f689f311…`, 08:42:58.80Z → 08:43:01.66Z) `verified_published`; `observe-child --step re-add` passed: both catalogs `[s2.s1-kill.test]` at 1790757669, child SOA 2026092803 and `www` A 192.0.2.10 authoritative on both servers over UDP and TCP, `changed` absent.

**Collect** (150 s hold, samplers running) and stop. The guests were stopped with QMP quit; the overlay was kept (`stop.json`).

## Cell 2: z05 (target-started cut, lifecycle, management-disabled reboot), held and resumed

Work root `/var/tmp/cp-b12-1001/r2`; cell directory `e05b4787635364e83414f682`. `GATEPROBE=0`.

**Run until the hold** (08:50:04.98Z → 08:51:29.8Z, exit **2**):
- Kill: marker 08:50:20.625Z, SIGKILL 08:50:20.663Z (38.1 ms), exit 137, `kill_proven`; no native entry inside the window.
- Guest complete verdict (checkpoint 1): `passed` / `passed`, `target_converged`.
- `catalog_restamp`: staged 1, `native_serial_at_cut` null, state 1790758220, served 1790758220 on both over UDP and TCP; `pre-publication`.
- Checkpoint `reboot-checkpoint-1.json` written 08:51:03.46Z (sha256 `b7680bd3…`).
- Pair verdict `passed`: members `[]`, 1790758220, `rndc_status_ok: true`.
- Lifecycle: add `verified_published` (4.28 s, 2026092801), edit `verified_published` (3.84 s, 2026092802), delete job `192554344c6206934b16138c9cca14ca` leased 08:51:21.0Z (`started_at` 08:51:20.91Z), pending at 08:51:26.96Z with `dns_peer_enrollment_required`.
- **Hold:** `fresh-primary-peer/zone-lifecycle-held.json` (`lifecycle_status: pending`, `lifecycle_exit` 2, `peer_verdict_exit` 0), `next_step` naming the enrollment and `run-prepared <same arguments and run flags> --zero-zones --resume-held-zone-lifecycle --execute (the guest run stays suspended on this boot until then; do not reboot or restart it)`.

**Owner enrollment, no reboot** (`enroll.log`, 08:51:41.6Z → 08:51:46.5Z): same sequence and tool hashes as `z04`; credential `05c5cd85…`; DIGEST MATCH; enrollment `9f1ce053…`; both `configured`. Boot IDs `199b45a8…` / `a6a17d73…` unchanged (`held-state-before-enroll.txt`).

**Pacing before the resume** (`resume-pacing-gate.txt`, read-only, as batch 11): the gate opened at 08:52:51.7Z (delete `started_at` + 90 s). By then the daemon had re-stamped the empty catalog at 08:52:20.708Z (1790758340). The resume started 08:53:09.25Z, 108 s after the delete began.

**Resume** (`run-prepared … --resume-held-zone-lifecycle --execute`, 08:53:09.252Z → 08:55:30.514Z, exit **0**, 141.26 s; `resume.log`, `boot-monitor.log`):
- **Lease:** 08:53:10.09Z `recovering`, attempt 2, lease to 08:53:30.05Z (126 ledger samples while recovering).
- **Inspection** (secondary's previous boot, `secondary-inspector-window-previous-boot.txt`): `celikpeer` login from 192.0.2.10, `zonestatus` control-channel commands for the catalog and `s2.s1-kill.test`, two loopback catalog AXFRs (serial 1790758340), `Connection closed by 192.0.2.10` at 08:53:25.01Z (secondary clock).
- **Outcome:** Agent lines above (08:53:34–35); job `succeeded` at `published`, attempt 2 (ledger 08:53:09.96Z → 08:53:35.89Z, 25.93 s), 5 heartbeats; `verified_published`.
- **Delete observation** passed: both catalogs at 1790758340 with **zero members**; `s2.s1-kill.test` SOA **REFUSED** on both servers over UDP and TCP.
- **Re-add** (job `03bc01af…`, 08:53:37.82Z → 08:53:40.94Z) `verified_published`; observation passed: both catalogs `[s2.s1-kill.test]` at 1790758341, child SOA 2026092803, `www` A 192.0.2.10.
- **Continuation:** `zone_lifecycle_before_reboot: {"lifecycle_exit": 0, "outcome": "passed", "peer_verdict_exit": 0, "resumed": true}`; the guest disabled the Panel and Agent and both guests rebooted, secondary first: secondary boot 2 `84c566f4…` seen 08:54:35.7Z, primary boot 2 `11dc2ac9…` seen 08:54:51.8Z (`boot-monitor.log`; new samplers started on each).
- **After reboot** (`native-and-outage.json` `reboot_after_recovery`, `fresh-primary-peer/peer-verdict-after-reboot.json`): `status: passed`, `failures: []`; `celikpanel-agent.service` and `celikpanel-panel.service` `inactive`/`disabled`; `pdns.service` active since 08:54:49; `serial_rule.rule: post-publication` ("the zone lifecycle published zones in this cell …", `zone_lifecycle_outcome: passed`); catalog checks `a_receipt` served 1790758460 ≥ receipt 1790758220, `b_udp_tcp` one serial, `c_secondary` 1790758460 on both transports, `d_database` 1790758460, `e_members` `[s2.s1-kill.test]` in database, primary and secondary; child SOA 2026092803 authoritative; 30 s DNS-only stability window (31 samples, Agent "management disabled"); `peer-verdict-after-reboot.json` `status: passed`, `catalog_members` `[s2.s1-kill.test]` = expected, serial 1790758460 on both over UDP/TCP, `rndc_status_ok: true`.
- **Hold record:** `zone-lifecycle-held.json` stays in the cell directory after the successful resume, unchanged (sha256 `64cabc94…`, mtime 08:51:26.66Z; byte-identical to batch 11's). Harness observation, not changed.

**Collect** (150 s hold) and stop. `service-mutation-status-post-collect.json` is empty and `smstatus.stderr` reads "connect: cannot reach agent socket: dial unix /run/celikpanel/agent.sock: connect: no such file or directory": the fixture status tool needs the Agent, which the cell disabled before the reboot by design (D-022). Not a product finding. Guests stopped with QMP quit, overlays kept.

## Catalog serial history (pdns journal `new CATALOG-HASH`, peer DNS sampler)

`z04`:

| Time (Z) | Event | Serial | Members | Hash |
| --- | --- | --- | --- | --- |
| 08:41:06.438 | first start re-stamp | 1790757666 | `{}` | `47DEQ…` |
| ~08:41:47 | add | 1790757667 | `{s2}` | unchanged |
| ~08:41:57 | edit | unchanged | `{s2}` | unchanged |
| ~08:42:03 | delete | 1790757668 | `{}` | unchanged (equals stored `{}`) |
| 08:42:37–08:42:57 | resumed attempt | 1790757668 | `{}` | no re-stamp |
| ~08:43:00 | re-add | 1790757669 | `{s2}` | unchanged |
| 08:43:06.554 | **daemon re-stamp** (+120.1 s from the first) | 1790757786 | `{s2}` | `1Rf5…` |

The daemon's check at ~08:42:06 found `{}` again (add and delete both inside one ~60 s period), so nothing changed between the delete and the resumed attempt. The only other re-stamp came after the re-add had published.

`z05`:

| Time (Z) | Event | Serial | Members | Hash |
| --- | --- | --- | --- | --- |
| 08:50:20.541 | first start re-stamp | 1790758220 | `{}` | `47DEQ…` |
| ~08:51:07 | add | 1790758221 | `{s2}` | unchanged |
| ~08:51:16 | edit | unchanged | `{s2}` | unchanged |
| 08:51:20.643 | **daemon re-stamp** (+60.1 s) | 1790758280 | `{s2}` | `1Rf5…` |
| ~08:51:23 | delete | 1790758281 | `{}` | unchanged |
| 08:52:20.708 | **daemon re-stamp** (+120.2 s), before the resume | 1790758340 | `{}` | `47DEQ…` |
| 08:53:10–08:53:36 | resumed attempt | 1790758340 | `{}` | no re-stamp |
| ~08:53:39 | re-add | 1790758341 | `{s2}` | unchanged |
| 08:54:20.791 | **daemon re-stamp** (+240.2 s) | 1790758460 | `{s2}` | `1Rf5…` |
| after both reboots | served on both | 1790758460 | `{s2}` | |

The pattern again fits a ~60 s check that re-stamps when the member set differs from the stored hash (inference, as before).

## Notify journal (primary, read-only, `pdns-notify-journal-and-also-notify.txt`)

Both cells: 8 × "Notification request to host **192.0.2.11:53** … received from operator"; 2 × "Received unsuccessful notification report … Not Authoritative …" (right after an add, before the secondary loaded the member); "Unable to queue notification … nameserver does not resolve!" 4 (`z04`) / 7 (`z05`). **Zero** "spurious", "`:0`" or "failed after retries" lines (the only matches of those words are the capture's own header line). `also-notify=192.0.2.11` at `/etc/powerdns/pdns.d/celikpanel-cluster.conf:7`.

## The rndc prerequisite (both cells)

Peer prepare receipt `"owner_prepared_rndc_key": "created"`; `/etc/rndc.key` metadata and the `:953` listener in `secondary-rndc-prerequisite-*.txt` (content never read); `rndc_status_ok: true` in every pair verdict, including after the reboot in `z05`.

## Build and fixture

- **Artifacts** from `git archive 2efc4de2` into `/root/cp-b12-src` (Go 1.26.5, `-trimpath -buildvcs=false`, `CGO_ENABLED=0`; `build/build.log`): agent `9d39e1b1…`, agent.kill `7491ec16…`, panel `03c530a6…`, dns-kill-trigger `cddab921…`, recovery `77375e18…`; recovery runtime manifest `77c354dd…`; owner tools `dns-peer-enroll 2ce8ff82…`, `bind-peer-inspect bacb58d8…`, `pdns-peer-inspect 7745e778…`. All differ from batch 11 except `schema17-bridge` (`fe277c46…`). The `r2` artifact copy is identical (`build/artifacts*.sha256`).
- **Gate constants:** both `true` in the tested source. Changed code 542ccc8e..2efc4de2: 31 files under `cmd`, `internal` and the kill-matrix harness (`build/build.log`).
- **Source tree:** at the end equal to a fresh `git archive 2efc4de2` except `deploy/e2e/dns-kill-matrix/__pycache__` and the untracked `web/dist` (`build/source-tree-vs-archive.diff`).
- **Web (deviation from batch 11):** no `npm ci` on the host. `web/dist` is the pair7 Windows `npm run build` of `git archive 2efc4de2 web` (**exit 0**, `build/web-build.log`), copied in via pair7's host clone; the 106 file hashes are identical (`build/web-dist.sha256`, `build/build.log` "web/dist = the pair7 Windows npm run build …").
- **Offline tests from the archive** (`build/offline-test-counts.txt`, `build/test-counts-per-module.log`, `build/trigger-tests.log`): Python **474** with each file run directly (466 in the build log's loop plus `test_native_pdns_peer.py`'s 8, whose direct run prints a JSON line after the summary), **475** with `discover` or per module (`test_guest_recovery_probe.py` 16/17 as in batch 11); all OK. `test_zone_lifecycle_recover.py`: 16. Trigger **60** top-level PASS, 0 FAIL.
- **Fixture tools:** batch 11's scripts renamed `cp-b11` → `cp-b12` (`build/fixture-tools-vs-batch11.diff`). Changed: `build.sh` (commit, comparison with batch 11's artifacts, `smstatus.go` from batch 11's evidence, web/dist copy), `export12.sh`, `remain12.sh`, `tcount2.sh`; removed the host web build scripts; new: `z04chain.sh` (code check, enrollment, resume), `z04after.sh`, `z04look2.sh`, `z04fin.sh`, `z05enroll.sh`, `z05after.sh`, `z05look2.sh`, `z05fin2.sh` (adds the secondary's previous-boot inspector window; no extra `observe` after a completed resume), `peekbuild.sh`, `showbuild.sh`, `chk1.sh`–`chk5.sh` (read-only extraction).
- **Versions:** Debian `pdns-server` 4.9.17-0+deb13u1, Arch `bind 9.20.29-1` (`versions-*`).
- **Repository `HEAD`** (record only): `d3d65353` at the start and at the export (`build/repo-head-at-*.txt`).

## Secret scan

`secretscan.py` (batch 11's) ran over every file in this folder after the README and the tool copies were added (`build/secret-scan.txt`): private-key blocks and the OpenSSH binary marker; credential, token and license-key assignments; TSIG/rndc `algorithm …; secret "…"` and bare `secret "…"` values; `tsigkeys` / `cryptokeys` rows in every retained database copy. It reports a hit by file and offset only. `/etc/rndc.key` was only listed, never read. `enroll.log` contains only the primary inspector's **public** key, its SHA-256, credential and enrollment IDs and host-key digests. Result over every file in the folder (1104 files at the final scan, including the previous `SHA256SUMS`) and 37 database copies: `tsigkeys` / `cryptokeys` have 0 rows everywhere; 1 hit, the OpenSSH binary-marker literal in the scanner's own pattern list (`build/fixture-tools/secretscan.py`, as in batch 11); no private key, credential, token, license key or rndc/TSIG secret. Nothing needed redaction. The longest path is 169 characters from the repository root.

## What remains on the host (`build/remaining-on-host.txt`)

- No QEMU process is running.
- Guest overlays kept, stopped: `z04` `/var/tmp/cp-b12-1001/r1/cells/7392457812ac089a866de6ec`; `z05` `/var/tmp/cp-b12-1001/r2/cells/e05b4787635364e83414f682` (run finished; not resumable and not needed).
- Other host files from this batch: `/var/tmp/cp-b12-1001` (4.2 GB: work roots, image hard links, artifacts, SSH key, logs, collected evidence); `/root/cp-b12-{src,artifacts,tools}`; `/var/tmp/cp-b12-{build,setup,tcount,triggertest}.log`, `/var/tmp/cp-b12-webdist.sha256`. The temporary `/var/tmp/cp-b12-verify` tree was removed.
- Nothing else under `/var/tmp` or `/root` was created, changed or deleted by this batch; `/var/tmp/cp-b11-1001`, `/root/cp-b11-artifacts` and `/root/cp-b11-tools` were only read (artifact comparison).

## Deviations

- **Web build** from pair7's Windows build instead of a host `npm ci` + `npm run build` (same archive, same commit, exit 0).
- **No extra `observe` after the resume:** batch 11's `z04observe.sh`/`z05observe.sh` ran `observe-child --step delete` after a *pending* resume; here the harness itself ran the delete and re-add observations, and after the re-add a delete observation would describe the wrong state.
- **Dates:** the host clock read 2026-09-30 throughout; the folder is named `…20261001` as requested; all times are the guests' and host's UTC (the two guests differ by about a second).
- **No workarounds.** No harness workaround, no pass rule changed, no cell re-run. No repository write except this untracked folder.

## What this does not prove

- The admission of a PowerDNS daemon re-stamp inside a resumed attempt (the `z04` expectation in `docs/DNS-ENGINE-ARTIFACT.md`) was **not** exercised: no re-stamp fell between pending and the end of the resumed attempt in either cell; the "admitted …" line, the composite owner-edit code and `dns_peer_proof_timeout` did not occur. A resume timed to overlap the daemon's ~60 s check would be needed.
- Single samples of the step times; `z05`'s `challenge_write` of 11.07 s against the 12 s bound shows a thin margin on this host under the samplers' load, not how often the bound is exceeded.
- The reboot is orderly (no power loss). One run per cell on an unsigned acceptance-branch build, Debian 13 PowerDNS 4.9.17 primary and Arch BIND 9.20.29 secondary; no other topology and no installed server.

Nothing here opens the gate on the main line, closes P0.4, or passes an acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file in this folder, including this README, except itself.
