# Batch 11: the two zero-zone lifecycle cells on fresh fixtures, with the owner-prepared rndc key on the panel-free BIND secondary, 2026-10-01

Scope: P0.4; D-025 invariants 1, 2, 4 and 5; D-022, D-024, D-026; acceptance register row 6 (exploratory). One native run of each zero-zone lifecycle cell that has a parentless deletion (`z04`, `z05`), on fresh fixtures. The procedure is the section "Pending parentless delete: resume after the owner's enrollment" of `deploy/e2e/dns-kill-matrix/README.md`. New in this batch: `native_pdns_bind_peer.py prepare` now creates the owner's `/etc/rndc.key` on the panel-free Arch secondary before `named` first starts (`bdd0b58c`).

Build: harness, trigger, owner tools and product were built from `git archive 542ccc8e` on the local **acceptance branch** `accept/pdns-primary-gate-open-8`. Its parent is `bdd0b58c`; the gate-open commit sets `freshPairedPDNSPrimaryAdmitted` / `freshPairedPDNSPrimaryOffered` to true.

Test bed: disposable QEMU guests on the local WSL `archlinux` host only. The Debian 13 kill guest is the CelikPanel PowerDNS primary; the Arch guest is the panel-free native BIND secondary. No installed panel, remote host, release or signed bundle was touched, and nothing was pushed or published. **Nothing here passes an acceptance-register row; these are observations.**

**Result: in both cells the resumed deletion stayed pending, now with `dns_peer_owner_edit_unknown`.** The rndc prerequisite held throughout: the key was created before `named` started, and `rndc_status_ok` was `true` before any deletion. The Agent resumed the exact pending job each time (phase `recovering`, lease, attempt 2), then kept it pending with a code for which the harness has no owner step.
- `z04`: the resume ran about 57 s after the delete. The Agent made **no** inspector exchange. The PowerDNS daemon re-stamped the catalog inside the resumed attempt.
- `z05`: the resume ran 6 min 19 s after the delete, after that re-stamp. The authenticated inspector exchange **ran and returned**, and the Agent still ended at `dns_peer_owner_edit_unknown` about 3.2 s later.

Consequences:
- No recovered delete reached `verified_published`.
- The harness ran no `observe-child --step delete` verdict and no re-add.
- `z05` had no reboot and no `peer-verdict-after-reboot.json`.

| Dir | Cell (`pdns-switch__…__paired-primary__peer-reachable`) | Commands | Exit | Outcome |
| --- | --- | --- | --- | --- |
| `z04-zero-committed-zl` | `committed__after-write`, fresh fixture `r1` | `run-prepared … --zero-zones --zone-lifecycle`, then `zone-lifecycle … --zero-zones --recover-delete --execute` | **2** (91.6 s), then **2** (5.04 s) | guest `passed`/`passed`, pair `passed`, add and edit `verified_published`, delete pending `dns_peer_enrollment_required`; after enrollment the resume ended pending with `dns_peer_owner_edit_unknown` (`zone-lifecycle-recover.json`) |
| `z05-zero-started-zl-rb` | `target-started__after-write`, fresh fixture `r2` | `run-prepared … --zero-zones --zone-lifecycle --reboot-after-recovery --disable-management-before-reboot`, then the same flags with `--resume-held-zone-lifecycle` | **2** (102.5 s, held), then **2** (17.28 s, still held) | guest `passed`/`passed` (from checkpoint 1), pair `passed`, add and edit passed, delete pending `dns_peer_enrollment_required` → hold; after enrollment the resume ended pending with `dns_peer_owner_edit_unknown`; run still held, no reboot |

## The rndc prerequisite (both cells)

- **Peer prepare receipt:** `"owner_prepared_rndc_key": "created"` (`peer-prepare.log`, last line).
- **Key file state** (`secondary-rndc-prerequisite-after-prepare.txt`, read-only; content never read):
  - `/etc/rndc.key` is `root:named 0640`, 100 bytes, owned by no package; there is no `/etc/rndc.conf` and no `controls` statement.
  - `named` logged "configuring command channel from '/etc/rndc.key'" and "command channel listening on 127.0.0.1#953" at its first start.
  - The same facts held before the enrollment and before the resume (`secondary-rndc-prerequisite-*.txt`).
- **Before any deletion**, `peer-verdict.json` → `observation.rndc_status_ok: true` in both cells. It was still `true` in the extra read-only `observe` after the pending resume (`extra-observe-after-pending.txt`).
- **During the `z05` inspection** the secondary logged the inspector's control-channel commands: `received control channel command 'zonestatus catalog-c000020a.celikpanel.invalid'` and `… 'zonestatus s2.s1-kill.test'`, twice each (two snapshots). Each came with a loopback catalog AXFR `127.0.0.1#… AXFR started (serial 1790745970)` / `ended`. The batch 10 blocker (no key, so `dns_peer_inspection_unknown`) did not recur.

## Cell 1: z04 (committed cut, lifecycle without reboot)

Work root `/var/tmp/cp-b11-1001/r1`; cell directory `7392457812ac089a866de6ec`. `GATEPROBE=0`.

**Run** (05:12:23.80Z → 05:13:55.44Z, exit **2**; `run-prepared.log`):
- Kill: marker 05:12:51.088Z, SIGKILL 05:12:51.113Z (25.3 ms), exit 137, `kill_proven`. No native entry fell inside the window (`boundary-window.txt`).
- Gate probe `open`; host idle after 2 polls (1.03 s).
- Guest complete verdict: `passed` / `passed`, `target_converged`, no failures (`native-and-outage.json`).
- `catalog_restamp`: staged 1, `native_serial_at_cut` 1790745168, state 1790745168, served 1790745168 on both servers over UDP and TCP (`zero_zone_restamp.restamped: true`). Serial rule `pre-publication`.
- Pair verdict `passed`: members `[]`, 1790745168, `rndc_status_ok: true`.

**Lifecycle:**
- add `verified_published` (job 4.44 s; child SOA 2026092801, catalog 1790745169 on both);
- edit `verified_published` (3.96 s; 2026092802);
- delete: job `7fb6a0df9a0df4c78fa75b362d0a2988` (the same deterministic request ID as batch 9) started 05:13:48.84Z. It went **pending** at 05:13:54.92Z with `dns_peer_enrollment_required` at the child's propagation phase, with `deadline_at` 05:58:48.84Z.
- Printed `next_step`: "the Agent kept the deletion of s2.s1-kill.test pending (dns_peer_enrollment_required): the server owner enrolls the native BIND secondary for inspection (dns-peer-enroll --engine bind on both guests, as the Agent's error code names), then runs guest_bootstrap.py zone-lifecycle <same arguments> --zero-zones --recover-delete --execute; …".
- Agent journal: "pdns zone publication remains pending for s2.s1-kill.test at epoch 1: paired DNS deletion is unverified (check=peer_native_zone); the peer administrator must check native zone state and DNS access, then retry verification of the same operation", followed by `dns_peer_enrollment_required`.

**Owner enrollment** (`enroll.log`, 05:14:28.17Z → 05:14:35.17Z). This was batch 9/10's sequence with this build's packaged tools (`dns-peer-enroll 75e0e4ee…`, `bind-peer-inspect ac28c467…`):
- both status commands `disabled`;
- `primary-prepare` (credential `338da460…`);
- public key saved root-owned 0600 on the secondary;
- `secondary-install` configured;
- `secondary-host-key` `546535dd…`, matching the independent digest (same SSH channel);
- `primary-activate` (enrollment `d2cb547c…`);
- both status commands `configured`.

Boot IDs were unchanged. There was no reboot and no lifecycle command in between.

**Resume** (`zone-lifecycle … --zero-zones --recover-delete --execute`, 05:14:44.461Z → 05:14:49.499Z, exit **2**; `zone-lifecycle-recover.log`, `fresh-primary-peer/zone-lifecycle-recover.json`, ledger sampler `raw/watch/cp-b11-watch-led/timeline.log`):
- **Before:** job pending, attempt 1, no lease, `dns_peer_enrollment_required`, child propagation phase (`primary-state-before-recover.txt`).
- **Lease:** at 05:14:45.70Z the job was `running` at `…/recovering/…`, attempt 2, `lease_expires_at` 05:15:05.70Z (20 s lease; 22 ledger changes while recovering).
- **Probes:** 05:14:47.42–47.46Z: the Agent's cache clear and catalog notify; 05:14:47.50Z: catalog AXFR from the secondary (serial 1790745229); 05:14:47.71Z: child AXFR `NOTAUTH`.
- **Daemon re-stamp inside the attempt.** At 05:14:48.564Z PowerDNS logged `new CATALOG-HASH '47DEQ…'`. The serial went 1790745229 → 1790745288, and the secondary transferred it at 05:14:48.65Z.
- **No inspector exchange.** The secondary journal from 05:14:43Z to 05:14:52Z has no `celikpeer` login and no `sudo … celikpanel-bind-peer-inspect` (`secondary-after-recover.txt`). The only SSH session was the harness's own `celik` session from 10.0.2.2.
- **Outcome.** Agent journal, 05:14:49.668Z: "DNS zone V3 recovery remains pending for s2.s1-kill.test: paired DNS deletion is unverified (check=peer_native_zone); the peer administrator must check native zone state and DNS access, then retry verification of the same operation", followed by **`dns_peer_owner_edit_unknown`**. The job went back to `pending` at the same propagation phase, attempt 2, no lease (`finished_at` 05:14:49.67Z; resumed attempt 4.07 s). The trigger returned `pending_exact_operation` (rc 75, 0 heartbeats) and the harness verdict was `pending`.
- **Harness `next_step`** (quoted): "the Agent kept the deletion of s2.s1-kill.test pending (dns_peer_owner_edit_unknown): the harness has no owner step for dns_peer_owner_edit_unknown". The re-add was not requested.
- The Agent logged **no inspector reason token**. There is no `celikpanel-peer-inspect-reason` line and no `dns_peer_inspection_unknown:<reason>` in any retained file, which is consistent with no inspection taking place.

**Extra read-only observations** (`extra-observe-after-pending.txt`, 05:16:30Z; not a harness verdict):
- `observe --zero-zones` rc 0: both catalogs had zero members at 1790745288 on UDP and TCP, `rndc_status_ok: true`.
- `observe-child --step delete --zero-zones` rc 0: both catalogs had zero members, and both servers answered `s2.s1-kill.test` SOA with REFUSED over UDP and TCP (`dns-after-pending-*.txt`: RCODE 5, no AA).

Locally, the deletion was published and consistent on both servers.

**DNS during the delete** (`peer-dns-sampler-changes.txt`): the primary refused the child from 05:13:50.88Z and the secondary from 05:13:55.46Z. Both catalogs had zero members at 1790745229 from 05:13:50.9Z, and at 1790745288 from 05:14:49.3Z.

**Catalog serial history** (`catalog-serial-history.txt`, pdns journal):

| Time (Z) | Event | Serial | Members | Hash |
| --- | --- | --- | --- | --- |
| 05:12:48.294 | first start re-stamp | 1790745168 | `{}` | `47DEQ…` |
| 05:13:34.8 | add | 1790745169 | `{s2}` | unchanged |
| 05:13:42.9 | edit | unchanged | `{s2}` | unchanged |
| 05:13:48.425 | **daemon re-stamp** (+60.13 s) | 1790745228 | `{s2}` | `1Rf5…` |
| 05:13:50.4 | delete | 1790745229 | `{}` | unchanged |
| 05:14:48.564 | **daemon re-stamp** (+120.27 s), inside the resumed attempt | 1790745288 | `{}` | `47DEQ…` |

There was no further re-stamp through the last sample (05:19:58Z).

**Collect** (150 s hold, samplers running) and stop. The guests were stopped with QMP quit at 05:20:17Z; the overlay was kept (`stop.json`).

## Cell 2: z05 (target-started cut, lifecycle, management-disabled reboot), held and resumed

Work root `/var/tmp/cp-b11-1001/r2`; cell directory `e05b4787635364e83414f682`. `GATEPROBE=0`.

**Run until the hold** (05:23:51.56Z → 05:25:34.06Z, exit **2**):
- Kill: marker 05:24:09.896Z, SIGKILL 05:24:09.931Z (34.5 ms), exit 137, `kill_proven`. No native entry fell inside the window.
- Gate probe `open`; host idle after 2 polls.
- Guest complete verdict (checkpoint 1 result): `passed` / `passed`, `target_converged`, no failures.
- `catalog_restamp`: staged 1, `native_serial_at_cut` null, state 1790745849, served 1790745849 on both servers over UDP and TCP. Serial rule `pre-publication`.
- Checkpoint `reboot-checkpoint-1.json` (stage `zone-lifecycle-before-reboot`) was written at 05:24:57.88Z, sha256 `4ccd439e…`.
- Pair verdict `passed`: members `[]`, 1790745849, `rndc_status_ok: true`.
- Lifecycle:
  - add `verified_published` (8.03 s, child SOA 2026092801);
  - edit `verified_published` (5.34 s, 2026092802);
  - delete job `192554344c6206934b16138c9cca14ca` started 05:25:26.56Z and went pending at 05:25:33.93Z with `dns_peer_enrollment_required` (`deadline_at` 06:10:26.56Z).
- **Hold:** `fresh-primary-peer/zone-lifecycle-held.json` records `peer_verdict_exit` 0, `lifecycle_status: pending`, `lifecycle_exit` 2. Its `next_step` now carries the real code (the batch 10 fix): "the Agent kept the deletion of s2.s1-kill.test pending (dns_peer_enrollment_required): the server owner enrolls the native BIND secondary for inspection (dns-peer-enroll --engine bind on both guests, as the Agent's error code names), then runs guest_bootstrap.py run-prepared <same arguments and run flags> --zero-zones --resume-held-zone-lifecycle --execute (the guest run stays suspended on this boot until then; do not reboot or restart it); …".

**Owner enrollment, no reboot** (`enroll.log`, finished 05:26:11.2Z). Same sequence and tool hashes as in `z04`:
- credential `18a892cc…`;
- host key `b5f1e0d9…` (DIGEST MATCH);
- enrollment `fb1fe723…`;
- both status commands `configured`.

Boot IDs `1d18ca8b…` / `09b1f948…` were the same before and after.

**Pacing before the resume** (`resume-pacing-gate.txt`; see Deviations). The resume started at 05:31:45Z, 6 min 19 s after the delete job began. By then the daemon's post-delete re-stamp (05:26:10.121Z, `47DEQ…`, 1790745970) had already happened, and the primary served 1790745970.

**Resume** (`run-prepared … --resume-held-zone-lifecycle --execute`, 05:31:45.273Z → 05:32:02.555Z, exit **2** after 17.28 s; `resume.log`):
- **Lease:** the hold was accepted and `zone-lifecycle-recover.json` was written. At 05:31:45.99Z the job was `recovering` with a lease, attempt 2, `lease_expires_at` 05:32:05.99Z (79 ledger changes while recovering).
- **Probes:** at 05:31:47.62–47.70Z the Agent cleared the cache and notified. At 05:31:47.73Z the secondary served the catalog AXFR (1790745970), and at 05:31:47.79Z the child AXFR got `NOTAUTH`.
- **Authenticated inspector exchange:**
  - `Accepted publickey for celikpeer from 192.0.2.10 port 47776` at 05:31:58.993Z;
  - `sudo[6121]: celikpeer : … COMMAND=/usr/local/libexec/celikpanel-bind-peer-inspect` at 05:31:59.230Z;
  - the rndc/AXFR lines above from 05:31:59.325Z to 05:31:59.456Z;
  - `Connection closed by 192.0.2.10` at 05:31:59.506Z.
- **After the exchange:** at 05:32:01.39Z the primary transferred its own catalog, at 05:32:01.45Z it transferred the secondary's catalog (1790745970), and at 05:32:01.63Z a child AXFR from the secondary got `NOTAUTH`. PowerDNS logged no re-stamp or serial change during the attempt (`secondary-after-resume.txt`; pdns journal).
- **Outcome.** Agent, 05:32:02.729Z: the same "DNS zone V3 recovery remains pending for s2.s1-kill.test: … (check=peer_native_zone) …" line, followed by **`dns_peer_owner_edit_unknown`**. The job was pending again (attempt 2, 16.78 s, 3 heartbeats).
- **Harness output:** `next_step` "the Agent kept the deletion of s2.s1-kill.test pending (dns_peer_owner_edit_unknown): the harness has no owner step for dns_peer_owner_edit_unknown". `zone_lifecycle_before_reboot: {"outcome": "held", "resumed": true}`. The guest was not continued: no reboot, management not disabled, no `result.json`, no `peer-verdict-after-reboot.json`.
- **No inspector reason token** appears in any retained file. With a non-nil inspection error the Agent would have mapped the reason into `dns_peer_inspection_unknown[:reason]` or `dns_peer_catalog_transfer_refused` (`cmd/agent/dns_engine_peer_pending.go`, `inspectionPendingCode`), so the inspector call itself returned without an error.

**Which check produced the code** (source reading, not observed). In `cmd/agent/dns_engine_peer_native_linux.go` at `542ccc8e`, `dns_peer_owner_edit_unknown` has two possible sources:
- the local recheck inside `verifyCurrent` (`recheckPDNSPeerLocalEvidence`: engine state, only-PowerDNS-active, the exact V3 deletion receipt, the producer catalog evidence), which runs before minting, after the round trip and before success;
- after `dnspeertransport.Inspect`, the catalog AXFR probes, `verifyDNSPrimaryPairReadyAuthorityAt` (`fresh != authority`), `verifyPeerZoneNoTransferAt`, or `observeDeletedDNSZoneAt` not returning `dnsDeletedZoneEmptyRefused`.

The Agent logs none of these distinctions. So the `z05` failure is in a post-inspection or recheck step, and `z04` failed before minting (no SSH at all). The daemon's re-stamp inside the `z04` attempt changes the producer catalog evidence, which is consistent with, but does not prove, the pre-mint recheck in `z04`. **Not worked around; nothing re-run.**

**Suspended run.**
- **Duration:** the guest controller suspended at 05:24:57.88Z (checkpoint 1). It was still suspended at the last check (05:37:52.5Z, about 12 min 55 s) and when the guests were stopped at 05:38:05.97Z (about 13 min 8 s in total).
- **Checkpoint undisturbed:** the same sha256 `4ccd439e…` and the same boot IDs before the enrollment, before the resume, after the resume and after the collect, and never a `result.json` (`held-state-{before-enroll,before-resume,after-resume,after-collect}.txt`).
- **Hold record:** `zone-lifecycle-held.json` was **not** rewritten by the resumed hold. It has the same sha256 `64cabc94…` and mtime 05:25:33.72Z in all four held-state captures, as in batch 10. It still names `dns_peer_enrollment_required`. The new code appears only in the resume's printed `held` object and in `zone-lifecycle-recover.json`.
- **Guests stopped at the end** as requested (QMP quit, overlays kept). **This run can no longer be resumed.**

**Extra read-only observations** after the pending resume (05:33:13Z; not a harness verdict): both catalogs had zero members at 1790745970, `rndc_status_ok: true`, and the child was REFUSED on both servers over UDP and TCP.

**DNS during the delete:** the primary refused the child from 05:25:28.93Z and the secondary from 05:25:31.23Z. Both catalogs had zero members at 1790745910 from 05:25:31.2Z, then 1790745970 from 05:26:10.3Z, unchanged to the last sample (05:37:45.7Z).

**Catalog serial history:**

| Time (Z) | Event | Serial | Members | Hash |
| --- | --- | --- | --- | --- |
| 05:24:09.800 | first start re-stamp | 1790745849 | `{}` | `47DEQ…` |
| 05:25:05.4 | add | 1790745850 | `{s2}` | unchanged |
| 05:25:09.936 | **daemon re-stamp** (+60.14 s) | 1790745909 | `{s2}` | `1Rf5…` |
| 05:25:17.7 | edit | unchanged | `{s2}` | unchanged |
| 05:25:29.3 | delete | 1790745910 | `{}` | unchanged |
| 05:26:10.121 | **daemon re-stamp** (+120.32 s) | 1790745970 | `{}` | `47DEQ…` |

There was no re-stamp during the resume and none through 05:37:45Z. As in batch 10, the pattern is consistent with a periodic check about every 60 s that re-stamps when the member set differs from the stored hash; this is still an inference.

## Notify journal (primary, read-only, `pdns-notify-journal-and-also-notify.txt`, fixed build)

Both cells logged 11 matching lines each:
- 6 × "Notification request to host **192.0.2.11:53** for zone … received from operator";
- 1 × "Received unsuccessful notification report for 's2.s1-kill.test' from 192.0.2.11:53, error: Server Not Authoritative for zone / Not Authorized" (right after the add, before the secondary loaded the member);
- 4 × "Unable to queue notification … nameserver does not resolve!" (catalog NS `invalid.` ×3, child NS `ns1.s1-kill.test` ×1).

There were **zero** "spurious" lines, **zero** `:0` lines and **zero** "failed after retries" lines. A plain `grep -c` in this batch's summary tooling also counted the capture's own header line ("lines matching notify|spurious|failed after retries…"); the counts above are from the actual journal lines. `also-notify=192.0.2.11` is at `/etc/powerdns/pdns.d/celikpanel-cluster.conf:7` in both cells.

## Build and fixture

- **Artifacts:** everything was built from `git archive 542ccc8e` into `/root/cp-b11-src` with the batch 10 toolchain and flags (Go 1.26.5, `-trimpath -buildvcs=false`, `CGO_ENABLED=0`; `build/build.log`):
  - agent `395d5927…`, agent.kill `a072d916…`, panel `39a71a28…`, dns-kill-trigger `361014c0…`, recovery `e3194422…`;
  - recovery runtime manifest `d376dbc6…`;
  - owner tools `dns-peer-enroll 75e0e4ee…`, `bind-peer-inspect ac28c467…`, `pdns-peer-inspect 0e7c0212…`.
  - Everything differs from batch 10 except `schema17-bridge` (`fe277c46…`). The `r2` artifact copy is identical (`build/artifacts*.sha256`).
- **Gate constants:** both `true` in the tested source.
- **Source tree:** at the end it equalled a fresh `git archive 542ccc8e` except `deploy/e2e/dns-kill-matrix/__pycache__` (`build/source-tree-vs-archive.diff`; see Deviations).
- **Web:** `npm ci` + `npm run build` exited **0** (`build/web-build.log`; 105 files in `build/web-dist.sha256`).
- **Offline tests from the archive** (`build/offline-test-counts.txt`, `build/test-counts-per-module.log`):
  - Python: **471** with each file run directly, **472** with `discover` or per module; all OK. The difference is `test_guest_recovery_probe.py` (16 / 17). `test_zone_lifecycle_recover.py`: 13; `test_native_pdns_bind_peer.py`: 8.
  - Trigger: **60** top-level PASS, 0 FAIL.
- **Fixture tools:** batch 10's scripts renamed `cp-b10` → `cp-b11` (`build/fixture-tools-vs-batch10.diff`). Changes and additions:
  - `cell.sh` adds one read-only `rndcfacts_remote.sh` capture right after the peer prepare;
  - `runresume.sh` also restarts the ledger sampler after a primary reboot (did not trigger: no reboot happened);
  - `secretscan.py` adds a bare `secret "…"` pattern and prints only file, label and byte offset for a hit, never the matched bytes;
  - new: `enroll11.sh` (= `enroll10.sh`), `recover11.sh`, `post11.sh` / `post_remote_sec.sh`, `held11.sh`, `collect11.sh`, `stop11.sh`, `resumegate.sh`, `z04observe.sh` / `z05observe.sh`, `facts.py`, `derive11.sh`, `export11.sh`, `srcdiff.sh`, `finalize11.sh`, `remain11.sh`.
- **Versions:** Debian `pdns-server` 4.9.17-0+deb13u1, Arch `bind 9.20.29-1` (`versions-*`).
- **Repository `HEAD`** (record only, never built or tested): `bdd0b58c` at the start and `37864789` at the export (`build/repo-head-at-*.txt`). `37864789` ("BIND primary propagation plan carries its source state; internal proof failures get their own code") was committed by someone else during the run. It touches the same code paths (`dns_engine_peer_native_linux.go`, `dns_engine_peer_pending.go`, `dns_peer_pending_codes.go`) and was **not** part of this build.

## Secret scan

`secretscan.py` ran over every file in this folder after the README and the tool copies were added (`build/secret-scan.txt`). It checks:
- private-key blocks and the OpenSSH binary marker;
- credential, token and license-key assignments;
- TSIG/rndc `algorithm …; secret "…"` and bare `secret "…"` values;
- `tsigkeys` / `cryptokeys` rows in every retained database copy.

The scanner reports a hit by file and offset only. `/etc/rndc.key` was only listed (`ls`), never read. The peer receipt says only `created`. `enroll.log` contains only the primary inspector's **public** key, its SHA-256, the credential and enrollment IDs and host-key digests. The primary's inspection key directory was listed, not read. Result over every file in the folder (including `SHA256SUMS`) and 26 database copies:
- `tsigkeys` / `cryptokeys` have 0 rows everywhere;
- 1 hit: the OpenSSH binary-marker literal in the scanner's own pattern list (`build/fixture-tools/secretscan.py`);
- no private key, credential, token, license key or rndc/TSIG secret.

Nothing needed redaction.

## What remains on the host (`build/remaining-on-host.txt`)

- No QEMU process is running.
- Guest overlays are kept, stopped:
  - `z04`: `/var/tmp/cp-b11-1001/r1/cells/7392457812ac089a866de6ec`;
  - `z05`: `/var/tmp/cp-b11-1001/r2/cells/e05b4787635364e83414f682` (no longer resumable).
- Other host files from this run:
  - `/var/tmp/cp-b11-1001` (4.3 GB: work roots, image hard links, artifacts, SSH key, logs, collected evidence);
  - `/root/cp-b11-{src,artifacts,tools}`;
  - `/var/tmp/cp-b11-{build,webbuild,setup}.log`, `/var/tmp/cp-b11-triggertest.log`, `/var/tmp/cp-b11-tcount.log`, `/var/tmp/cp-b11-webdist.sha256`.
- Temporary `/var/tmp/cp-b11-verify` trees were removed.
- Nothing else under `/var/tmp` or `/root` was created, changed or deleted. `/var/tmp/cp-pair5-work`, `/var/tmp/cp-b10-1001` and `/var/tmp/cp-b9-1001` were only listed.

## Deviations

- **Pacing of the `z05` resume.** `z04` was resumed about 57 s after its delete, following batch 10's pace, and the daemon's periodic re-stamp fell inside the resumed attempt. To learn whether the outcome depends on that collision, the `z05` resume was started only after `resumegate.sh`, a read-only wait that opens 90 s after the pending delete began and lists the primary's `CATALOG-HASH` lines.
  - In practice the gate opened immediately: the resume came 6 min 19 s after the delete, because the enrollment wrapper stalled (next item).
  - This changes only when the owner acts. No harness or product behaviour was altered.
- **Enrollment wrapper stall.** In `z05` the enrollment script itself finished at 05:26:11Z (`enroll.log`: `ENROLL-DONE`), but the host-side wait command around it did not return, and the tool call was moved to the background. It was stopped at about 05:31Z. No enrollment or guest command was repeated.
- **Build start.** The first `startbuild.sh` call ran scripts that had CRLF line endings and did nothing: no log, no directory. After LF conversion the build ran once (`build/build.log`).
- **First source-tree comparison.** The first export compared the built tree against `git archive 0d4c0324`, because a string replacement in `export11.sh` missed. `srcdiff.sh` redid the comparison against `542ccc8e` and replaced the file; the result is only `__pycache__`. The build itself was always from `542ccc8e` (`build/build.log`: "tested commit: 542ccc8ef108…").
- **Stale hold `next_step` (harness observation, quoted, not changed).** After the resumed hold, `zone-lifecycle-held.json` still tells the owner to enroll (`dns_peer_enrollment_required`), while the Agent's current code is `dns_peer_owner_edit_unknown`. Only the printed `held` object carries the new code.
- **Dates.** The host clock read 2026-09-30 throughout. The folder is named `…20261001` as requested; all times above are the guests' and host's UTC.
- **No workarounds.** No harness workaround was applied, no pass rule was changed and no cell was re-run. No `git status` or other repository write was made; the only repository write is this untracked folder.

## What this does not prove

The key question is still not answered positively. On a CelikPanel PowerDNS primary with a panel-free BIND secondary that has the owner-prepared rndc key, the product resumed the exact pending parentless deletion after the owner's enrollment, twice. In `z05` it completed an authenticated inspection with working rndc and loopback AXFR. But both times it kept the deletion pending with `dns_peer_owner_edit_unknown`, so a **completed** parentless deletion after owner enrollment is still unexercised. So are:
- the recovered delete's `verified_published`;
- `observe-child --step delete` as a harness verdict;
- the re-add;
- the `z05` management-disabled reboot and `peer-verdict-after-reboot.json` under the post-publication rule.

Other limits:
- Which of the post-inspection or recheck steps produced the code is inferred from source, not observed, because the Agent does not log it.
- The `z04` link to the daemon re-stamp is timing only.
- The repository has since moved to `37864789`, which changes this area and was not tested here.
- This was one run per cell on an unsigned acceptance-branch build, with a Debian 13 PowerDNS 4.9.17 primary and an Arch BIND 9.20.29 secondary. It covered no other topology and no installed server.

Nothing here opens the gate on the main line, closes P0.4, or passes an acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file in this folder, including this README, except itself.
