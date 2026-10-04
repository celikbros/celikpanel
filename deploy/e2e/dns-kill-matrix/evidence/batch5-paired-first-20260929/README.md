# First native paired-secondary, takeover and reinstall cells, 2026-09-29

Scope: P0.4, D-025 invariants 1, 2, 4 and 5, D-022, D-024, D-026; acceptance register rows 3, 5, 12 and 14 (item 2). This is the **first native run** of the panel-free native primary peer (`native_primary_peer.py`), the paired-secondary controller flow and the takeover/reinstall fixture variants, which until now had offline tests only. It ran on product source that contains today's fixes with component tests only. Six Debian 13 cells, fresh disposable QEMU guests on the local WSL `archlinux` host; the paired cells used both guests (Debian 13 secondary under test, Arch native primary peer, as `manifest.json` places them: `kill_host` `debian-13`, `dns_peer_host` `arch`). It did not touch an installed panel, a remote host, a release or a signed bundle. It is exploratory: nothing here passes an acceptance-register row.

**Kill-hook caveat (applies to every result here).** The tagged kill hook has a suspected stop race: the calling goroutine can continue into the product's error path before the process stops (see [batch 4](../batch4-adoption-reboot-20260929/README.md), cell `c6`). A fix is in progress and was not in this build. Every cell with a proven kill (`c1`, `c2`, `c5`) carries this caveat; `c1` shows the same signature as batch 4 `c6` (below). `c3`, `c4` and `c6` never reached a kill.

**Result: 2 passed, 1 failed, 3 unverified or not judged** (classification is `result.json` `status`; the peer verdict is `paired-secondary-peer/peer-verdict.json` `status`; "combined" is `run-prepared`'s exit, `peer-verdict.json` `combined_exit`):

| Dir | Cell, fixture, flags | Guest `status` / `safety_status` | Peer verdict | Combined |
| --- | --- | --- | --- | --- |
| `c1-bindsec-bindpri` | `bind__target-verified__after-write__paired-secondary__peer-reachable`, `uninitialized`, `--peer-engine bind --reboot-after-recovery --disable-management-before-reboot` | **failed** / passed (`repeated_nonconvergence`) | passed | **1** |
| `c2-bindsec-pdnspri` | same cell, `--peer-engine pdns`, same flags | **passed** / passed (`target_converged`, reboot passed) | passed | **0** |
| `c3-pdnssec-bindpri` | `pdns-switch__target-started__after-write__paired-secondary__peer-reachable`, `uninitialized`, `--peer-engine bind`, same flags | **unverified** / unverified (no kill) | passed | **2** |
| `c4-pdnssec-pdnspri` | same cell, `--peer-engine pdns`, same flags | **unverified** / unverified (no kill) | passed | **2** |
| `c5-takeover` | `bind__target-staged__after-write__standalone__peer-reachable`, `unmanaged-bind-stopped` | **passed** / passed (`target_converged`) | n/a | **0** |
| `c6-reinstall` | same cell, `managed-bind-absent` | no `result.json` (controller crashed, `KeyError`) | n/a | **1** |

Three different findings:

- **Product defect, PowerDNS paired secondary (`c3`, `c4`), no kill involved.** Native PowerDNS 4.9.17 on Debian 13 transferred the peer catalog and the member and served the member authoritatively, and wrote a **non-empty `options`** value on the consumed member row (`{"consumer": {"unique": "b076e924….zones label."}}`). The Agent's readiness check requires `options` to be empty, so it looped for 15 s, gave up with `paired PowerDNS catalog members were not provisioned`, could not roll back (`PowerDNS rollback live database is not the staged target`) and poisoned its ledger. The journal stays `rolling-back`, the job stays `running`/`leased`, PowerDNS is stopped (still enabled) and the guest serves no DNS. The same happened against a BIND and a PowerDNS primary. The harness pass rule has the same empty-`options` expectation.
- **Kill-hook race signature (`c1`).** Same boundary as `c2`, which passed. In `c1` the BIND `current` pointer disappeared 8 ms after the boundary marker and 14 ms before SIGKILL, and the restarted Agent refused: `verified DNS engine target no longer matches its journal: file does not exist`. This matches batch 4 `c6`. It is not classified as a product failure until the hook fix is in.
- **Harness defect (`c6`).** `run_cell.py` reads the managed BIND setup state receipt as a flat v1 object, but the product writes the v2 receipt (`acquisition`/`publication`); `validate_managed_bind_setup` raised `KeyError: 'manifest_qualifier'` before any measured mutation. The reinstall path was not exercised.

## Build and fixture

- Tested source: commit `65b866211ffee85348fb3df2bfebf5fd86fbdff0` (`feat/dns-artifact-separation`), extracted with `git archive 65b86621` into guest-host ext4 (`/root/cp-b5-src`). The harness (`fixture.py`, `guest_bootstrap.py`, `guest_bootstrap.sh`, `run_cell.py`, `guest_recovery_probe.py`, `native_primary_peer.py`, `native_primary_peer_probe.py`) ran from that tree; hashes in [build/harness-files.sha256](build/harness-files.sha256). The working tree held uncommitted Go and harness edits by other agents; nothing was built or run from it.
- Repository `HEAD`: `65b86621` when the run started (≈13:47Z); `1c001994` when the build script read it (13:50:14Z, [build/build.log](build/build.log)); `c04d8a2b` at 14:26Z and `86c3fa20` at the end (14:30Z). Other agents committed `1c001994`, `c04d8a2b` and `86c3fa20` during the run; none of them was built or tested here.
- Toolchain: `/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go`, `go1.26.5 linux/amd64`, `GOTOOLCHAIN=local CGO_ENABLED=0 -trimpath -buildvcs=false`, as in batch 4. Binaries and the recovery runtime bundle: [build/artifacts.sha256](build/artifacts.sha256) (and `artifacts-r2.sha256` for the copy in the second work root, identical).

| Binary | Build | SHA-256 | vs batch 4 (`8f86bdad`) |
| --- | --- | --- | --- |
| `agent` | `./cmd/agent` | `572678e1e7654c0b432a283a63381ac43d639f777465f0650489cf4f0d3e7a83` | different |
| `agent.kill` | `./cmd/agent`, `-tags dns_kill_matrix` | `9b8b35721e651381903de20ac2c9ba2fbf68e876e97bd18cffaab699c9480856` | different |
| `panel` | `./cmd/panel` | `793fb81b95fa96f438a216831a146e0869934ce31318881857d3352ffb92e472` | identical |
| `dns-kill-trigger` | `./cmd/dns-kill-matrix-trigger` | `05866e6a477647caee637c577b0d1c4574aaa14ef71d964d8825bc748cc9feeb` | different |
| `recovery` | `./cmd/recovery` | `6922104723c03c2ccc653f3a62d1312ecd9b6d859460bc32aa628c55c0d43480` | different |
| `agent-checker` | `deploy/recovery/agent-checker.sources` | `82f2772fe0257dde9139d0f85a34be58e89c98d6a9d1775090f2ecf4cc3b8975` | identical |
| `panel-checker` | `deploy/recovery/panel-checker.sources` | `82f23b48908e152e55ad76598ec8be0f780805f463fe7e48ed402d87bc5984e0` | identical |
| `schema17-bridge` | `./deploy/schema17bridge` | `fe277c46ea8b50f00a80e71976198f28db89667789a737e0a78c0f4a83e9cb87` | identical |
| recovery runtime `runtime.manifest` | `go run ./deploy/recovery/bundle` | `47934c4ea03c8bedb10fdcff64c2224cc0923e2db0b75140a6e9c588853a1b10` | different (only `bin/recovery` changed) |

- `web/dist` is untracked and was copied from the Windows checkout; its hash list ([build/web-dist.sha256](build/web-dist.sha256)) equals batch 4's. The read-only `Agent.ServiceMutationStatus` reader `oi-smstatus` (`ff9abfa1…7890`) is byte-identical to the earlier runs. No recovery launcher was enrolled in any cell (none is an owner-inverse cell).
- **Two work roots.** A cell directory is `cells/<sha256(cell id)[:24]>`, and failed cells keep their overlays, so each cell id could run only once per work root: `c1`, `c3`, `c5` used `/var/tmp/cp-b5-0929`; `c2`, `c4`, `c6` used `/var/tmp/cp-b5-0929/r2`. Both were initialised with `fixture.py init-root`, the locked base images were hard-linked from `/var/tmp/cp-v3n28/images` and passed `verify-images` ([build/work-root-setup.log](build/work-root-setup.log)); both share one SSH key. The guest manifest equals the tracked `manifest.json` (`dcf6e09c…d1229`).
- Per cell: `prepare` → `start` (both guests) → `wait-ssh` → `guest_bootstrap.py install` on the Debian guest → `prepare-bind` / `prepare-pdns-switch` with the cell's fixture (paired cells add `--peer-engine`, which runs `native_primary_peer.py prepare` and a baseline `observe` on the Arch peer first) → read-only snapshots → a dry run of `run-prepared` → `run-prepared … --execute` → collect → `stop` and `teardown` (passed cells) or `stop` only. The exact lines are at the top of each `driver.log`; the scripts are in [build/fixture-tools/](build/fixture-tools/).
- Packages from live mirrors. Debian guest: `bind9` `1:9.20.29-1~deb13u1` (the image carries `bind9-host`/`bind9-libs` `9.20.26`, upgraded inside the operation), `pdns-server`/`pdns-backend-sqlite3` `4.9.17-0+deb13u1`. Arch peer (`pacman -Syu`, a full system upgrade that also installed kernel 7.2.7; the peer kept running 7.1.8 and was not rebooted): `bind 9.20.29-1`, `powerdns 5.1.4-4`, `python 3.14.7-1`.
- **The PowerDNS peer serves the catalog in the BIND catalog format** (ordinary `MASTER` zones with explicit rows equal to `binddns.CatalogZoneRecords`), not a native PowerDNS `PRODUCER` catalog. "Against a PowerDNS primary" in `c2`/`c4` means exactly that; a PowerDNS primary publishing its native catalog (including a CelikPanel PowerDNS primary) is not tested. Every Agent log line naming the format said `serves catalog catalog-c000020b.celikpanel.invalid in the BIND catalog format; this operation reads it in that format`.
- **No harness workaround was applied.** Nothing in `/root/cp-b5-src` was changed, so there is no `build/harness-workarounds.diff`. No cell was re-run.

### Evidence layout

As in batch 4 (`raw/results/` flattened; `native-and-outage.json` derived by `build/fixture-tools/extract.py`; `raw/watch/` sampler per boot; `raw/journald/` unit journals of the Debian guest; `owner-post-state.txt`, `service-mutation-status-post-collect.json`), plus for the paired cells:

- `paired-secondary-peer/`: the host-side peer evidence written by `guest_bootstrap.py`: `peer-prepared.json` (prepare receipt and baseline observe), `peer-before-kill.json` (observe right before the controller), `peer-verdict.json` (observe after recovery with `--require-secondary-transfer`, the judgement, `combined_exit`).
- `prepare-source.log`: the peer's `native_primary_peer.py prepare` output (package install, `named-checkconf`/`named-checkzone` or the SQLite seed) followed by the guest's preparation.
- `peer-identity.txt`, `peer-facts-pre-run.txt`, `peer-facts-post-collect.txt` (Arch packages, units, listeners, config and zone file hashes, the peer's `domains` rows for PowerDNS), `peer-daemon-journal.txt` (the peer's `named.service` or `pdns.service` journal).
- `secondary-state-pre-run.txt`, `secondary-state-post-collect.txt` (Debian guest, read only): units, listeners; BIND `rndc status`, `rndc zonestatus`/`showzone` for the catalog and the member, `/var/cache/bind` tree with mtimes and every file's hash; PowerDNS the full `domains` table (all columns, including `options`, `catalog`, `master`, `account`, `last_check`, `notified_serial`) and the records; transfer/NOTIFY/catalog lines from both engine journals; Agent catalog lines.
- DNS answers for `s1-kill.test` SOA, `www.s1-kill.test` A and the catalog SOA over UDP and TCP to `192.0.2.10` (secondary), `192.0.2.11` (primary) and `127.0.0.1`, asked from both guests: `dns-pre-run-guest.txt`/`-peer.txt` (before the controller) and `dns-post-collect-guest.txt`/`-peer.txt` (after recovery; in `c2` after the reboot). `peer-dns-sampler.log` is a read-only loop on the peer asking both guests every 2–5 s from before the controller to the collection (1 s timeouts), which covers the cut, recovery and the reboot.
- `extra-diagnostics-post-collect.txt` (`c1` only): the three BIND configuration files, `/var/cache/bind/celikpanel` without following links, the staged `zones.conf`, the Agent's DNS lines.

## Cell 1 `c1-bindsec-bindpri`: fresh BIND secondary of a native BIND primary — FAILED

- Request `2228e0de67c2e95ae9675187f214d1b5`. Controller 13:56:56.72Z–13:57:44.09Z.
- `result.json`: `status: failed`, `safety_status: passed`, `recovery_outcome.classification: repeated_nonconvergence`. `fixture_pass_definition.failures`:
  > `the same request did not converge to the target: repeated_nonconvergence`

  `diagnostic_failures`: `post-restart-rpc-retry-1: post-restart-rpc-retry-1 exited 1`, `post-restart-rpc-retry-2: post-restart-rpc-retry-2 exited 1`, `first recovery probe is indeterminate`, `second recovery probe is indeterminate`. `reboot_after_recovery`: `{"run": false, "reason": "the flow ended 'failed', not passed; the reboot is defined only for a passing flow and was not run"}`. **The reboot was not exercised.** Probes 1 and 2 (same fingerprint `5679f5e9…`):
  > `DNS engine switch journal remains after recovery; bind engine install ownership receipt remains after successful finalization; bind engine ownership receipt is absent for the active target; measured mutation job is not finalized: {'status': {'want': 'succeeded', 'got': 'failed'}, 'phase': {'want': 'commit/dns-engine-switch/v2/finalized/…', 'got': 'interrupted'}}`
- Peer verdict **passed**: after recovery the native primary logged the catalog and member AXFR to `192.0.2.10` (13:57:10.83), its config digests, catalog serial 1/members and member SOA were unchanged. Combined exit 1.
- **Sequence.** The tagged Agent read the peer catalog before intent (`…serves catalog … in the BIND catalog format…`, 13:56:57, in `transcript.jsonl`). Journal `intent` 13:57:07.49, `source-stopped` 13:57:07.77, `named` 4351 active 13:57:10.68 (it transferred the catalog and `s1-kill.test` from `192.0.2.11` at 13:57:10.7–10.83), `target-started` 13:57:12.09. From the guest's files and `result.json`:

  | Event | Time (UTC) |
  | --- | --- |
  | `dns-engine-state.json` mtime | 13:57:13.211732 |
  | journal `target-verified` mtime (`d30ac941…`) | 13:57:13.219733 |
  | boundary marker `recorded_at` | 13:57:13.235336 |
  | `/var/cache/bind/celikpanel` mtime (`current` removed) | 13:57:13.243734 |
  | SIGKILL delivered (PID 1723, state `T`, exit 137) | 13:57:13.258078 |

- **Agent decides.** The restarted Agent (5204) logged at 13:57:13.38:
  > `Interrupted DNS switch native recovery is unknown; retain exact journal and release only its ledger lease (request 2228e0de67c2e95ae9675187f214d1b5): verified DNS engine target no longer matches its journal: file does not exist`

  and released the job `failed`/`interrupted`. Both `rpc-retry` calls exited 1. The journal stays `target-verified`.
- **What serves.** `named` 4351 kept serving from memory (catalog-zones `in-memory yes`): the secondary answered `s1-kill.test` SOA `2026083101`, `www` A `192.0.2.11` and the catalog SOA 1 with `aa` over UDP and TCP on all three addresses, from both guests; the peer sampler saw it answer from 13:57:13.07 on. On disk, `named.conf.local` includes `/var/cache/bind/celikpanel/current/zones.conf`, and `current` does not exist (only `generations/74c26315…` with an intact `zones.conf`). By batch 4 `c6`, `named` then fails to start after a reboot; that was not exercised here.
- **Reading (not established; no fix made).** The same signature as batch 4 `c6`, same boundary (`target-verified`, after-write, fresh BIND): the only product path that removes the pointer on a first install is `Publisher.restoreSwitchPointerLocked` (`internal/binddns/publisher.go` 687–701, `Remove` at 698), reached from `Publisher.Switch` when `apply` returns an error (publisher.go 630–638). At `65b86621`, `apply` writes `target-verified` at `cmd/agent/dns_engine_host.go` 1799–1801, where the tagged hook runs; the hook stops with a process-directed `unix.Kill(pid, SIGSTOP)` (`cmd/agent/dns_engine_kill_matrix_linux.go` 182, reached from `stopAtBoundary` 598–647) and returns `dnsKillMatrixResumedError` (646) if execution continues. The pointer changed 8 ms after the marker and 14 ms before SIGKILL, with no `rolling-back` written. `c2` ran the same cell and passed, with the pointer untouched (mtime before the marker). I read `c1` as the kill-hook race, not as a production path; this remains a reading until the hook fix is run.
- The guests were stopped and their overlays kept (`/var/tmp/cp-b5-0929/cells/cb432d640bbee31a569ea8f8`).

## Cell 2 `c2-bindsec-pdnspri`: fresh BIND secondary of a native PowerDNS primary — PASSED

- Request `2228e0de67c2e95ae9675187f214d1b5` (the request ID is derived from the cell ID, so it equals `c1`'s; different guests). Controller 14:01:48.45Z–14:03:22.00Z.
- `result.json`: `status: passed`, `safety_status: passed`, `target_converged`, no failures of any kind; `fixture_pass_definition` (`paired-secondary`) passed: the secondary answered `s1-kill.test` SOA `[2026083101]` and `www` A `["192.0.2.11"]` over UDP and TCP, equal to the primary's answers. Peer verdict **passed** (catalog and member AXFR-out to `192.0.2.10` logged by PowerDNS 5.1.4, e.g. `AXFR-out zone 's1-kill.test', client '192.0.2.10:40423', AXFR finished` at 14:02:02.14; digests, serial, members, member SOA and `www` A unchanged). Combined exit **0**.
- **Sequence.** Journal `intent` 14:01:58.99, `source-stopped` 14:01:59.27, `target-started` 14:02:03.42; `named` transferred the catalog and member from the PowerDNS primary at 14:02:02.0–02.15. Marker 14:02:04.606869, SIGKILL 14:02:04.616232 (PID 2077, state `T`, exit 137), journal `target-verified` (`d30ac941…`). The `current` pointer (mtime 14:01:58.87) was not touched.
- **Agent recovers by itself.** The restarted Agent (5553) logged `DNS engine change recovery …` and `DNS engine change completion …` with the BIND-catalog-format sentence (14:02:05.24, 14:02:05.94), wrote `committed` 14:02:05.46 and retired the journal by 14:02:06.28 before listening. Both `rpc-retry` exited 0; probes 1 and 2 `target_converged` (`8d7a0cd6…`). First health window: **30** samples, all OK (14:02:06.60Z–14:02:36.60Z; the harness README says 31, as batch 4 `c2` also recorded; recorded, not judged).
- **Reboot with management disabled.** `systemctl disable --now celikpanel-panel.service celikpanel-agent.service` (both inactive/disabled, proven), then `fixture.reboot_guest` 14.08 s, boot `3cb17a65-e154-4423-94f0-e4f3023e7d31` → `f403d6d0-a08d-4f16-b3f6-068e37ce7ca6`, SMBIOS UUID `4030e2f9-2fb9-5e6b-a287-21bf6b805777`. After the boot `named` (648) came up alone on port 53 with the same answer counts, the same state receipt (`eae4ab1e…`), journal absent; it re-transferred the catalog and member from the PowerDNS primary at 14:02:49.36–49.50. **Second window 31/31**, DNS only (14:02:51.23Z–14:03:21.82Z); Agent and Panel recorded as not applicable. `reboot_after_recovery.status: passed`. The peer sampler saw the secondary not answering from 14:02:41.45 to 14:02:52.60 (the reboot) and answering with `aa` otherwise; the primary always answered.
- After the reboot (`dns-post-collect-*.txt`, `secondary-state-post-collect.txt`): both guests see the secondary answer SOA `2026083101`, `www` A `192.0.2.11` and catalog SOA 1 with `aa` over UDP and TCP; `rndc zonestatus`: catalog `type: secondary`, serial 1; `s1-kill.test` `type: secondary`, serial `2026083101`, last loaded 14:02:49. The management units were left disabled on the disposable guest, as the harness defines.
- **Answers the native question:** the BIND secondary loads the member from a (BIND-format-catalog) PowerDNS primary, before and after a reboot.
- Stopped and torn down (`stop.json`, `teardown.json`).

## Cell 3 `c3-pdnssec-bindpri`: fresh PowerDNS secondary of a native BIND primary — UNVERIFIED (product defect)

- Request `5bd1de7092240beea2027afc2a707c7d`. Controller 14:06:54.57Z–14:07:24.98Z.
- `result.json`: `status: unverified`, `safety_status: unverified`, `kill_proven: false`; `verification_failures`:
  > `scenario trigger exited before boundary notification with exit code 75 (raw 75)`

  No safety, diagnostic or pass-definition entries (the controller stops before them). Peer verdict **passed** (the BIND primary logged the catalog and member AXFR to the guest; nothing changed on the peer). Combined exit **2**. **No kill happened, so this is not the kill-hook race.** The reboot was not exercised.
- **Preflight** (`paired_secondary_preflight`): peer catalog SOA serial 1 authoritative over UDP/TCP, PowerDNS database absent, expected classification `fresh install … fault driver pdns-switch`.
- **Sequence.** Journal `intent` 14:07:04.95, `target-staged` 14:07:06.39, `source-stopped` 14:07:06.67; `pdns.service` (3716) started 14:07:09.21. PowerDNS consumed the catalog and created the member at 14:07:09.26 (`AXFR-in zone: 'catalog-c000020b.celikpanel.invalid', primary: '192.0.2.11', Catalog-Zone create zone 's1-kill.test'`) and committed `s1-kill.test` serial `2026083101` at 14:07:09.39. The Agent then issued **51** `Retrieval request for zone 's1-kill.test' … received from operator` (about every 270 ms) and PowerDNS completed 51 member AXFRs until 14:07:24.14. The peer sampler saw the secondary answer the member and catalog with `aa` in every sample from 14:07:11.90; the sample at 14:07:24.91 got no answer. At 14:07:24.35 PowerDNS was stopped; the journal went to `rolling-back` (14:07:24.45).
- **Verbatim Agent output** (tagged Agent, `transcript.jsonl`):
  > `DNS engine switch failure could not prove a pre-commit abort: paired PowerDNS catalog members were not provisioned`
  > `PowerDNS switch rollback restore database: PowerDNS rollback live database is not the staged target`
  > `reprove DNS engine switch abort: PowerDNS switch rollback restore database: PowerDNS rollback live database is not the staged target`
  > `service mutation manager is fail-closed after an ambiguous ledger write`
  > `finalize active DNS engine switch: paired PowerDNS catalog members were not provisioned`

  and the trigger: `DNS engine switch RPC ended without an exact terminal receipt: agent rejection "DNS engine switch outcome could not be verified; inspect the agent log"; terminal reconciliation: agent response code="" error="service mutation manager is fail-closed after an ambiguous ledger write …"`. The controller then killed the tagged Agent (cleanup).
- **PowerDNS `domains` rows on the guest** (`secondary-state-post-collect.txt`, read with `mode=ro`):

  | name | type | master | account | options | catalog | last_check | notified_serial |
  | --- | --- | --- | --- | --- | --- | --- | --- |
  | `catalog-c000020b.celikpanel.invalid` | `CONSUMER` | `192.0.2.11` | `celikpanel-peer-catalog-v1` | NULL | NULL | 1790690829 | NULL |
  | `s1-kill.test` | `SLAVE` | `192.0.2.11` | `""` | `{"consumer": {"unique": "b076e9241974292fffe8ecc0209b7ace316d1d36063423d9ccb0849a."}}` | `catalog-c000020b.celikpanel.invalid` | 1790690844 | NULL |

  The member holds its SOA, NS ns1/ns2, `ns1`/`ns2`/`www` A; the catalog holds SOA, NS, `version` TXT `"2"`, the PTR and an empty `zones` ENT row.
- **End state.** Journal `rolling-back` (`a9246bab…`); ledger job `running`/`leased`, attempt 1, lease expiring 14:07:44; `dns-engine-install-ownership-pdns.json` present; no state receipt; `pdns.service` inactive but **enabled**; production Agent stopped (the fixture stops the coordinators before the measured operation); tagged Agent killed. The guest answered nothing on `192.0.2.10` or `127.0.0.1` afterwards (post-collect and sampler).
- **Reading (product defect; no fix made).** `retrievePDNSPairSecondaryZones` (`cmd/agent/dns_engine_pdns_catalog.go` 221–260), called before `target-started` (`cmd/agent/dns_engine_pdns_switch.go` 2020–2024), becomes ready only when `verifyLivePDNSPairSecondaryProjection` → `verifyPDNSPairSecondaryTx` accepts the live rows; the member branch refuses any non-empty `options` (`dns_engine_pdns_catalog.go` 161–168, the check at 164: `PowerDNS secondary candidate contains foreign catalog authority`). Native PowerDNS 4.9.17 writes the consumer `unique` property into `options` of every consumed member, so the loop can never succeed and ends after `dnsPairProofLimit` (15 s, `cmd/agent/dns_engine_zone_verify.go` 35) with `paired PowerDNS catalog members were not provisioned` (catalog.go 258). The rollback then refuses the live database (`dns_engine_pdns_switch.go` 1165–1169, `verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial`), which I read as the same unexpected member row, but this is not established; the ledger write that followed was ambiguous and poisoned the mutation manager (`cmd/agent/service_mutation_rpc.go` 40). **The harness has the same expectation:** `run_cell.py` `check_pdns_secondary_rows` requires `members[0][5] == ""` (line 9381), so even a product fix would fail the current pass rule on PowerDNS 4.9.17. The unit tests evidently model a member without `options`; the native consumer does not.
- **Answers the native question:** yes, PowerDNS 4.9.17 writes a non-empty `options` value on consumed member rows (above, verbatim); `notified_serial` stays NULL on both rows and `last_check` is set.
- The guests were stopped and their overlays kept (`/var/tmp/cp-b5-0929/cells/6af131693a7a795257f9a58a`).

## Cell 4 `c4-pdnssec-pdnspri`: fresh PowerDNS secondary of a native PowerDNS primary — UNVERIFIED (same product defect)

- Request `5bd1de7092240beea2027afc2a707c7d` (same derivation as `c3`). Controller 14:11:40.50Z–14:12:38.96Z.
- `result.json`: `status: unverified`, `safety_status: unverified`, `kill_proven: false`, `verification_failures` exactly `scenario trigger exited before boundary notification with exit code 75 (raw 75)`. Peer verdict **passed** (PowerDNS 5.1.4 primary logged AXFR-out of both zones to `192.0.2.10`). Combined exit **2**. No kill; not the kill-hook race.
- Journal `intent` 14:12:19.40, `source-stopped` 14:12:20.50 (`target-staged` was too short for the sampler), `pdns.service` started 14:12:23.04, catalog consumed and member created, **41** operator retrieval requests and member AXFRs, secondary answering with `aa` in every sample from 14:12:23.81 (none at 14:12:39.03), `rolling-back` 14:12:38.16. The Agent output is the same five lines as `c3`, word for word. The `domains` rows are the same as `c3` (member `options` `{"consumer": {"unique": "b076e9241974292fffe8ecc0209b7ace316d1d36063423d9ccb0849a."}}`, `notified_serial` NULL). End state as in `c3` (journal `rolling-back`, job `running`/`leased`, `pdns.service` inactive and enabled, no DNS served).
- So the defect does not depend on the primary's engine: the secondary's own PowerDNS writes the `options` value.
- The guests were stopped and their overlays kept (`/var/tmp/cp-b5-0929/r2/cells/6af131693a7a795257f9a58a`).

## Cell 5 `c5-takeover`: stopped owner BIND takeover (row 12) — PASSED

- Request `b66cbdc5c51b25639364f76a7113a214`. Controller 14:16:32.29Z–14:17:19.73Z.
- **Source.** `bind9` `1:9.20.29-1~deb13u1` installed by the fixture under the package guard, then `named.service` disabled as an owner would (`named.service` loaded/inactive/disabled, `bind9.service` not-found); no CelikPanel state, ownership, install receipt or journal (`source-preinstall-bind.json`, scope `owner-installed-stopped-bind-for-takeover`).
- **Cut.** Journal `target-staged` (`8d2ef877…`), marker 14:16:38.225864, SIGKILL 14:16:38.234867 (PID 2340, state `T`, exit 137). **At the cut** (`provenance_boundary`): `dns-engine-install-ownership-bind.json` names `bind9`, this request, `adopted_present: true`, `missing_before: []` (`f2d3f83b…`).
- **Recovery.** The restarted Agent (3290, 14:16:38.25) rolled the cut back (journal `rolling-back` 14:16:38.33, retired by 14:16:40.39); the first `rpc-retry` then ran the same request forward as attempt 2: `intent` 14:16:43.45 → `source-stopped` → `target-started` 14:16:47.86 → `committed` 14:16:48.73 → retired 14:16:49.31; job `succeeded`, attempt 2. Probes 1 and 2 `target_converged` (`bac6f937…`). BIND alone on port 53. Health **31/31** (14:16:49.70Z–14:17:19.70Z).
- **Owner files** (`fixture_pass_definition.owner_files`): `named.conf`, `named.conf.root-hints`, `rndc.key` and `/etc/default/named` byte-identical (judged); `named.conf.options` (`f1c37414…` → `18a5f47b…`) and `named.conf.local` (`63963de7…` → `379603f9…`) rewritten by the documented takeover (recorded, not judged). The inventory has no `named.conf.default-zones`, `bind.keys`, `db.*` or `zones.rfc1918` on this Debian 13 package, so those were not compared.
- The README's code-reading risk (a rolled-back takeover keeps its `adopted_present` receipt and the retry takes the exclusive-options authority) did not stop convergence with Debian's stock `named.conf.options`; the owner-directive refusal it predicts was not exercised. No reboot flag was requested for this cell.
- Kill-hook caveat applies (a kill was proven); nothing in the evidence suggests the race affected this cut.
- Stopped and torn down.

## Cell 6 `c6-reinstall`: BIND reinstall after the owner purged it (row 14) — NOT JUDGED (harness defect)

- **Preparation completed.** A real untagged production fresh BIND switch (request `67beea28f7bb2d005105c97169966302`, `rpc-switch-complete`) produced the managed BIND; the fixture then ran `systemctl disable --now named.service` and `apt-get purge bind9` (`Purging configuration files for bind9 (1:9.20.29-1~deb13u1)`; `bind9-utils`, `bind9-host`, `bind9-libs` stay installed; dpkg: `directory '/var/cache/bind' not empty so not removed`). Before the controller: `named.service` and `bind9.service` not-found, state and ownership receipts present (`prepare-source.log`, `pre-run-units.txt`).
- **Controller crash** (`run-prepared.log`, exit 1, no `result.json`; `transcript.jsonl` holds one `cell-finish` line, `status: unverified`, empty failure lists):
  > `File "/opt/celikpanel/libexec/dns-kill-run-cell.py", line 3653, in validate_socket_source_proof`
  > `    validate_managed_bind_setup(proof, cell, scenario, state)`
  > `File "/opt/celikpanel/libexec/dns-kill-run-cell.py", line 3287, in validate_managed_bind_setup`
  > `    "manifest_qualifier": state["manifest_qualifier"],`
  > `KeyError: 'manifest_qualifier'`

  It happened while validating the source proof, **before the tagged Agent or the trigger started**: no measured mutation, no kill.
- **Reading (harness defect; no fix made).** The product's state receipt is `celikpanel-dns-engine-state/v2` with `acquisition` (holding `manifest_qualifier`, `mode`, `mutation_request_id`, …) and `publication` (see `raw/state/dns-engine-state.json` and `source-proof.json` `engine_state_identity`). `run_cell.py` `validate_socket_source_proof` passes the raw `state` object to `validate_managed_bind_setup` (line 3653), which reads flat v1 keys (lines 3287 and 3294–3298), while the neighbouring ownership comparison already uses `decode_dns_document(state, state_raw)` (line 3641). My reading is that the managed-BIND setup check was not updated for the v2 receipt; passing the decoded document would likely be the fix, but I did not change or re-run anything. This is not a setup problem the rules let me work around (it is controller validation code), so the cell was stopped as found.
- **Not answered:** the code-reading prediction that the restarted Agent's rollback cannot verify its restored source (`verifyOnlyBINDActive` against a frozen source with BIND not running). Nothing about the reinstall path was observed.
- The guests were stopped and their overlays kept (`/var/tmp/cp-b5-0929/r2/cells/e3224bede2eda2ee2dff1060`).

## What remains on the host

- `/var/tmp/cp-b5-0929` (5.9 GB apparent): both work roots (`r2` nested), logs, collected evidence and the **stopped overlays of the four unpassed cells**: `cells/cb432d640bbee31a569ea8f8` (`c1`), `cells/6af131693a7a795257f9a58a` (`c3`), `r2/cells/6af131693a7a795257f9a58a` (`c4`), `r2/cells/e3224bede2eda2ee2dff1060` (`c6`). `c2` and `c5` were torn down. No QEMU process runs.
- `/root/cp-b5-src`, `/root/cp-b5-artifacts`, `/root/cp-b5-tools`, `/var/tmp/cp-b5-build.log`, `/var/tmp/cp-b5-setup.log`, `/var/tmp/cp-b5-webdist.sha256`. Nothing outside `cp-b5*` was created or deleted.

## Deviations

- Two work roots instead of one (see Build), because the same cell ID ran twice and failed cells keep their cell directories.
- The peer DNS sampler (`peer-dns-sampler.log`, `build/fixture-tools/pairq.py` via `peerloop_start_remote.sh`) is new in this run: a read-only query loop started as a transient unit on the peer after its preparation and stopped at collection. Its interval is 2 s plus up to 12 × 1 s timeouts, so its gaps are several seconds when the secondary does not answer. "DNS after recovery, before the reboot" for `c2` comes from the controller's own `secondary_serving` check and this sampler; the post-collect queries in `c2` are after the reboot.
- `pacman -Syu` on the Arch peer is what `native_primary_peer.py` runs; it upgraded the whole peer (including a new kernel that was not booted). Package versions are live-mirror versions.
- `c2`'s first health window had 30 samples (the harness README says 31); recorded, not judged.
- `owner-post-state.txt` runs `recovery dns-switch-status`, which exits 127 in every cell (no launcher enrolled). `service-mutation-status-post-collect.json` could not reach the Agent in `c3`, `c4` (stopped) and `c2` (disabled for the reboot); in `c6` there was no `result.json`, so the collection had no request ID and the reader and `owner-post-state.txt` ran without one (`smstatus.stderr`: usage). See each `smstatus.stderr`.
- No cell was re-run and no harness workaround was applied.

## What this does not prove

Two passed cells, one run each, on one host, Debian 13 kill guest only, Arch peer only. `c2` is one paired-secondary pass (BIND secondary, `target-verified` after-write, BIND-format catalog from a PowerDNS 5.1.4 primary) with one orderly reboot with management disabled; the same cell against a BIND primary (`c1`) did not pass, which I attribute to the kill-hook race but cannot prove until the hook fix runs. No PowerDNS paired-secondary cell reached its boundary, so nothing about PowerDNS secondary recovery was measured; `c3`/`c4` show a product defect in the fresh PowerDNS secondary install itself, independent of kills. No peer-unreachable cell, no native PowerDNS `PRODUCER` catalog, no CelikPanel-to-CelikPanel pair, no power loss, no owner edit, no continuous probe on the guest itself. `c5` is one takeover pass without reboot and without owner directives in `named.conf.options`. The reinstall path (`c6`) was not exercised at all. Every kill result carries the kill-hook caveat. Nothing here changes the 268-runnable denominator, closes P0.4 or P0.5, or passes any acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file except this README and itself.
