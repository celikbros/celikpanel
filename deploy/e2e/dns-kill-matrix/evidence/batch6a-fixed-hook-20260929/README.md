# Fixed boundary hook: fresh, V2, adoption, takeover and paired-secondary cells, 2026-09-29

Scope: P0.4, D-025 invariants 1, 2, 4 and 5, D-022, D-024, D-026; acceptance register rows 3, 5, 11, 12 and 13 (exploratory). This is the first native run of source that contains the thread-directed boundary stop of the tagged kill hook (`c04d8a2b`, "Stop exactly at the kill boundary; repair a missing BIND pointer; order pointer after unit") and the harness changes of `86c3fa20` (native PowerDNS catalog peer, rollback-standby judgement, complete pre-reboot verdict). Twelve cells in fresh disposable QEMU guests on the local WSL `archlinux` host: eleven on Debian 13, one on Arch (`c02`), the two paired cells with the Arch guest as panel-free native primary peer. It did not touch an installed panel, a remote host, a release or a signed bundle. It is exploratory: nothing here passes an acceptance-register row.

**Result: 12 of 12 passed** (classification is `result.json` `status`; the peer verdict is `paired-secondary-peer/peer-verdict.json` `status`; "combined" is `run-prepared`'s exit, which for the paired cells is `peer-verdict.json` `combined_exit`):

| Dir | Cell, fixture, run flags | Guest `status` / `safety_status`, classification, `complete_verdict` | Reboot(s) | Peer verdict | Combined |
| --- | --- | --- | --- | --- | --- |
| `c01-fresh-bind-reboot` | `bind__target-verified__after-write__standalone__peer-reachable`, `uninitialized`, `--reboot-after-recovery` | **passed** / passed, `target_converged`, passed | after recovery: passed | n/a | **0** |
| `c02-fresh-bind-arch` | `bind__target-verified__before-write__standalone__peer-reachable` (Arch), `uninitialized`, `--reboot-after-recovery` | **passed** / passed, `target_converged`, passed | after recovery: passed | n/a | **0** |
| `c03-fresh-pdns-pre` | `pdns-switch__target-staged__after-write__standalone__peer-reachable`, `uninitialized`, `--reboot-after-recovery` | **passed** / passed, `target_converged`, passed | after recovery: passed | n/a | **0** |
| `c04-fresh-pdns-post` | `pdns-switch__target-started__after-write__standalone__peer-reachable`, `uninitialized`, `--reboot-after-recovery` | **passed** / passed, `target_converged`, passed | after recovery: passed | n/a | **0** |
| `c05-v2-prestart` | `bind__target-staged__after-write__standalone__peer-reachable`, `managed-pdns`, `--owner-inverse-after-restart --reboot-after-recovery --retry-switch-after-rollback` | **passed** / passed, `rolled_back_source_serving`, passed; retry switch passed | after recovery: passed | n/a | **0** |
| `c06-v2-poststop` | `bind__target-started__after-write__standalone__peer-reachable`, `managed-pdns`, `--owner-inverse-after-restart --reboot-before-owner-command --reboot-after-recovery --retry-switch-after-rollback` | **passed** / passed, `rolled_back_source_serving`, passed; retry switch passed | before owner command and after recovery: passed | n/a | **0** |
| `c07-bind-adopt` | `bind__rolling-back__after-write__standalone__peer-reachable`, `owner-bind`, `--bind-rollback-after-target-started --owner-inverse-after-restart --reboot-before-owner-command --reboot-after-recovery` | **passed** / passed, `rolled_back_source_serving`, passed | before owner command and after recovery: passed | n/a | **0** |
| `c08-pdns-adopt-rb` | `pdns-adopt__rolled-back__after-write__standalone__peer-reachable`, `external-pdns-adoption`, `--expect-agent-startup-rollback --reboot-after-recovery` | **passed** / passed, `target_converged`, passed | after recovery: passed | n/a | **0** |
| `c09-pdns-adopt-intent` | `pdns-adopt__intent__after-write__standalone__peer-reachable`, `external-pdns-adoption`, `--expect-agent-startup-rollback` | **passed** / passed, `target_converged`, passed | none requested | n/a | **0** |
| `c10-takeover` | `bind__target-staged__after-write__standalone__peer-reachable`, `unmanaged-bind-stopped`, `--reboot-after-recovery` | **passed** / passed, `target_converged`, passed | after recovery: passed | n/a | **0** |
| `c11-bindsec-bindpri` | `bind__target-verified__after-write__paired-secondary__peer-reachable`, `uninitialized`, `--peer-engine bind --peer-catalog-format bind --reboot-after-recovery --disable-management-before-reboot` | **passed** / passed, `target_converged`, passed | after recovery, management disabled: passed | **passed** | **0** |
| `c12-bindsec-pdnsnative` | same cell, `--peer-engine pdns --peer-catalog-format pdns-native`, same reboot flags | **passed** / passed, `target_converged`, passed | after recovery, management disabled: passed | **passed** | **0** |

Every flag combination requested was admitted by the bootstrap and the controller (an early dry run of `run-prepared` before the guests started, `run-prepared-dryrun-early.log`, and the usual dry run after preparation). No failure reason exists in any `result.json`: `failures`, `safety_failures`, `verification_failures`, `diagnostic_failures`, `owner_inverse_failures`, `recovery_status_read_failures` and every reboot/retry `failures` list are empty or absent.

**Boundary.** In all twelve cells the tagged Agent was in state `T` when SIGKILL was delivered (exit 137, `/proc` entry absent after reap), so `wait_for_stopped_process` and the kill proof worked with the thread-directed stop. The window from the boundary marker's `recorded_at` to SIGKILL was 7.7–17.4 ms in ten cells and 47 ms (`c05`), 49 ms (`c06`) and 98 ms (`c07`) in the three owner-inverse cells. No unit-journal entry of any DNS or management unit and no file mtime in the retained tree listings falls inside that window (`boundary-window.txt` per cell, produced by `build/fixture-tools/boundcheck.py`), with one exception that is not a CelikPanel mutation: in `c07` the owner's `named` (PID 2042) logged two `managed-keys-zone: Key … for zone . is now trusted (acceptance timer complete)` lines 53 ms after the marker, its own DNSSEC trust-anchor timer. No Agent log line in any cell mentions the pointer repair or a resumed hook.

**The batch 4/5 failure signature did not recur.** In `c01` (the cell that took DNS down in batch 4) the managed BIND root and its `current` pointer have mtime 14:40:49.531023 (pointer to `generations/a6e189e7…`), 5.47 s **before** the marker (14:40:54.996724) and SIGKILL (14:40:55.005066); the sampler's lstat of the pointer (every ~0.3 s) never changed after 14:40:49.56, the Agent converged forward, and after the reboot `named` started and answered. `c11` (batch 5 `c1`'s cell) likewise converged. One run each: this shows the cut landed at the named boundary in these runs; it does not prove the race is impossible.

## Build and fixture

- Tested source: commit `94cd124be572f72d88b04bc5c85a9c308c791eac` (`feat/dns-artifact-separation`), extracted with `git archive 94cd124b` into guest-host ext4 (`/root/cp-b6a-src`). The harness ran from that tree (hashes in [build/harness-files.sha256](build/harness-files.sha256), including `cmd/agent/dns_engine_kill_matrix_linux.go`). At the end `/root/cp-b6a-src` was compared with a fresh `git archive 94cd124b`: the only difference is a Python `__pycache__` directory created by the host helpers importing `fixture.py` ([build/source-tree-vs-archive.diff](build/source-tree-vs-archive.diff)). The working tree held uncommitted Go and harness edits by other agents; nothing was built or run from it.
- Repository `HEAD`: `94cd124b` at the start (14:32Z) and when the build script read it (14:35:41Z, [build/build.log](build/build.log)); `66db850c` when the last cell was torn down (15:41Z) and `93b9b1f0` when this README was written (15:50Z). Other agents committed `2ac6dcbe` ("Harness: read v2 state receipt in reinstall setup, accept PowerDNS consumer options, record versions"), `66db850c` ("Durable rollback decision, PowerDNS secondary consumer state, DNS-only hold") and `93b9b1f0` ("Add product-flow DNS pair acceptance driver") during the run; none of them was built or tested here.
- Toolchain: `/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go`, `go1.26.5 linux/amd64`, `GOTOOLCHAIN=local CGO_ENABLED=0 -trimpath -buildvcs=false`, as in batches 4 and 5. Hashes: [build/artifacts.sha256](build/artifacts.sha256) (build directory), `artifacts-root1.sha256` and `artifacts-r2.sha256` (the copies in both work roots, identical).

| Binary | Build | SHA-256 | vs batch 5 (`65b86621`) |
| --- | --- | --- | --- |
| `agent` | `./cmd/agent` | `3421f44518c9e75374e69c725ba3e7ed4d50f1282990b45fc2cec5ec91c253fc` | different |
| `agent.kill` | `./cmd/agent`, `-tags dns_kill_matrix` | `43b000fce1e8e65d602f54562a5c10566b4b24df93bcb96ab5a60cb7b955e793` | different |
| `panel` | `./cmd/panel` | `5b5289ab3d3b5522830b5a7871cdfb1a6fffc1b2bacb9e31c0d7869a5b527033` | different |
| `dns-kill-trigger` | `./cmd/dns-kill-matrix-trigger` | `5476eb9996a467322aefffb8c0665bbdb0143fbc04cdd92ee424ee061660a693` | different |
| `recovery` | `./cmd/recovery` | `c494c3025c051ceea9b382e26de7da29a66c5ba6a3332de86294b57eb06cb324` | different |
| `agent-checker` | `deploy/recovery/agent-checker.sources` | `82f2772fe0257dde9139d0f85a34be58e89c98d6a9d1775090f2ecf4cc3b8975` | identical |
| `panel-checker` | `deploy/recovery/panel-checker.sources` | `82f23b48908e152e55ad76598ec8be0f780805f463fe7e48ed402d87bc5984e0` | identical |
| `schema17-bridge` | `./deploy/schema17bridge` | `fe277c46ea8b50f00a80e71976198f28db89667789a737e0a78c0f4a83e9cb87` | identical |
| recovery runtime `runtime.manifest` | `go run ./deploy/recovery/bundle` | `3b6955c553c54295e2c27f51ecc2394366c209dd8862aa4d26208b4f8d44ce0e` | different (only `bin/recovery` changed) |

- `web/dist` is untracked and was copied from the Windows checkout; its hash list ([build/web-dist.sha256](build/web-dist.sha256)) equals batch 5's. The read-only `Agent.ServiceMutationStatus` reader `oi-smstatus` (`ff9abfa1…7890`) is byte-identical to the earlier runs.
- **Recovery runtime enrolled in every standalone cell** (`c01`–`c10`, `enroll-recovery-runtime.log`), so the rpc-retry flow recorded `recovery dns-switch-status` before and after recovery (`recovery_status_reads`). The bootstrap admits enrollment for standalone cells only, so the paired cells `c11`/`c12` were not enrolled; their status reads are recorded as `available: false`.
- **Two work roots.** A cell directory is `cells/<sha256(cell id)[:24]>`. `c10` repeats `c05`'s cell ID and `c12` repeats `c11`'s, so those two used `/var/tmp/cp-b6a-0929/r2`; all others used `/var/tmp/cp-b6a-0929`. Both were initialised with `fixture.py init-root`, the locked base images hard-linked from `/var/tmp/cp-v3n28/images` and verified ([build/work-root-setup.log](build/work-root-setup.log)); one SSH key. The guest manifest equals the tracked `manifest.json` (`dcf6e09c…d1229`).
- Per cell: `prepare` → early dry run of `run-prepared` → `start` (both guests) → `wait-ssh` → `install` → `enroll-recovery-runtime` (standalone) → `prepare-bind` / `prepare-pdns-switch` / `prepare-pdns-adopt` (paired cells: with `--peer-engine` and `--peer-catalog-format`, which run `native_primary_peer.py prepare` and a baseline `observe` on the Arch peer first) → read-only snapshots → dry run → `run-prepared … --execute` → collect → `stop` and `teardown`. The exact lines are at the top of each `driver.log`; the scripts are in [build/fixture-tools/](build/fixture-tools/) (`cell.sh` now runs `runonly.sh` for the run part).
- **Software versions** (`versions-pre-run-*.txt`, `versions-post-collect-guest.txt` / `-other.txt` in every cell): Debian guest kernel `6.12.105+deb13-cloud-amd64`, systemd `257.13-1~deb13u1`, `bind9` `1:9.20.29-1~deb13u1` (installed inside the measured operation where BIND is the target), `pdns-server`/`pdns-backend-sqlite3` `4.9.17-0+deb13u1`. Arch guest (base image kernel `7.1.8-arch1-3`, systemd `261.2-1`): `bind 9.20.29-1`, `powerdns 5.1.4-4`. On Arch every package transaction (the measured BIND install in `c02`, the peer preparation in `c11`/`c12`) is a full upgrade that also installed kernel `7.2.7-arch1-1` and systemd `262-1`; the `c02` kill guest then rebooted into kernel 7.2.7 during the after-recovery reboot, the paired peers kept running 7.1.8.
- **No harness workaround was applied** (no `build/harness-workarounds.diff`). No cell was re-run.

### Evidence layout

As in batches 4 and 5 (`raw/results/` flattened; `raw/watch/` sampler per boot; `raw/journald/` unit journals of the kill guest; `native-and-outage.json` derived from `result.json` by `build/fixture-tools/extract.py`, now also copying `complete_verdict`, `pre_reboot_verdict`, `recovery_status_reads`, `reboot_after_recovery`, `retry_switch_after_rollback`, `agent_startup_rollback`, the boundary marker, kill record and `rollback_end_state`; `owner-post-state.txt`, which now runs the enrolled `recovery dns-switch-status` after everything; `service-mutation-status-post-collect.json`; the paired-cell peer files described in batch 5), plus:

- `boundary-window.txt`: marker and SIGKILL times and every natively timestamped line found inside that window.
- `cell-digest.txt`: a read-only digest of `result.json` (request, kill, retries, probes, windows, reboots, status reads, owner-inverse steps, retry switch) made by `build/fixture-tools/digest.py`.
- `versions-*.txt`: OS, kernel, systemd and DNS package versions on both guests, before the controller and after collection.
- The sampler (`watcher.sh`) now also records an lstat signature of the managed BIND root and its `current` pointer (Debian `/var/cache/bind/celikpanel`, Arch `/var/named/celikpanel`) as `POINTER` lines in `timeline.log`, and lists both roots in every snapshot.
- `unitfacts`/`owner-post-state` capture `pacman -Q`, `/etc/named.conf` and the `/var/named` tree on Arch.

## Cell 1 `c01-fresh-bind-reboot` — PASSED

- Request `abd7cb29722819ec4117f95c8deddce5`. Controller 14:40:38.19Z–14:42:17.36Z.
- **Cut.** Journal `target-verified` (`4d6af030…`). Marker 14:40:54.996724Z, SIGKILL 14:40:55.005066Z (PID 1699, state `T`, exit 137, reaped). Managed root `/var/cache/bind/celikpanel` and pointer `current` → `generations/a6e189e7…`, both mtime 14:40:49.531023 (inode of the pointer 131594), unchanged in the at-kill-proof snapshot and in every later sample.
- **Status before recovery** (Agent down; exit 0, no evidence change): `DNS switch request abd7cb29722819ec4117f95c8deddce5: accepted-active (journal phase target-verified).` / `No owner recovery command applies to this journal's recorded shape and ledger status. …`
- **Recovery.** Agent startup converged forward (journal changed at 14:40:56.61 and was retired by 14:40:57.21); both `rpc-retry` exit 0 (0.008 s, 0.004 s, idempotent); probes 1 and 2 `target_converged` (`13b3b367…`). Health 31/31. Status after recovery: `No DNS switch journal was observed for request abd7cb29722819ec4117f95c8deddce5. Its exact ledger job records status succeeded. …`
- **Reboot after recovery** (`complete_verdict` passed): boot `4109eeb3…` → `8fa954b0…`, 18.9 s; BIND alone on port 53 with the same answer counts; second window 31/31; `reboot_after_recovery.status: passed`.
- **Compared with earlier runs:** batch 4 `c6` (`8f86bdad`) failed this cell (pointer gone between marker and SIGKILL, Agent refused, `named` could not start after the reboot, DNS refused 0/31); `fresh-install-20260929` cell 4 (`574cadf2`) passed without reboot.

## Cell 2 `c02-fresh-bind-arch` — PASSED

- Arch kill guest (manifest `kill_host: arch`). Request `8d6edfff6835819901869204d21d7bae`. Controller 14:45:36.94Z–14:48:29.70Z.
- **Cut.** On-disk journal `target-started` (`d84d3b8c…`), the predecessor, marker at the `target-verified` `before_write` hook 14:46:36.849579Z, SIGKILL 14:46:36.859590Z (PID 1548, `T`, 137). `dns-engine-state.json` mtime 14:46:36.839230 (before the marker, as this edge requires). Arch managed root `/var/named/celikpanel` mtime 14:46:33.113692, pointer `current` → `generations/80caf1bc…` mtime 14:46:33.109670 (from `owner-post-state.txt`; the tree was not changed afterwards).
- Status before recovery: `accepted-active (journal phase target-started)`, no owner command (exit 0). Agent startup converged; both retries exit 0 (idempotent); probes `target_converged` (`1eb4c171…`); 31/31. Reboot (50.2 s, into the new kernel 7.2.7 that the measured `pacman` transaction installed): passed, second window 31/31.
- **Sampler gap.** As in `fresh-install-20260929` cell 3, the measured `pacman` transaction upgraded base packages under the running sampler, which stopped at 14:46:07 (its last line is `done`), so boot 1 has no at-kill-proof snapshot and no `POINTER` samples after that. The boot-2 sampler ran normally. The controller evidence is unaffected; the pointer timing above comes from the final tree listing.
- **Compared with earlier runs:** `fresh-install-20260929` cell 3 (`574cadf2`) passed without reboot, same journal hash `d84d3b8c…`.

## Cell 3 `c03-fresh-pdns-pre` — PASSED

- Request `c60a3d86859d1980ad1d13ce8b86a793`. Controller 14:51:55.44Z–14:53:30.22Z.
- **Cut.** Journal `target-staged` (`49ddaaaf…`), marker 14:52:06.234850Z, SIGKILL 14:52:06.242568Z (PID 1702, `T`, 137). No `pdns.service` journal entry inside the window.
- Status before recovery: `accepted-active (journal phase target-staged)`, no owner command. Agent startup rolled the first install back (journal went through two versions and was retired by 14:52:07.20); retry 1 re-ran the request forward (exit 0, 4.86 s; job `succeeded`, attempt 2), retry 2 idempotent (exit 0); probes `target_converged` (`4c7dd74b…`); 31/31. Reboot 16.8 s: PowerDNS alone on port 53, second window 31/31, passed.
- **Compared with earlier runs:** `fresh-install-20260929` cell 1 (`574cadf2`) failed (recovery unknown, DNS refused 31/31); `fresh-install-rerun-20260929` cell 1 (`1c336f6d`) passed without reboot, same probe fingerprint `4c7dd74b…`.

## Cell 4 `c04-fresh-pdns-post` — PASSED

- Request `8fcd357e26d39c4e54c59713e3052809`. Controller 14:57:15.06Z–14:58:55.02Z.
- **Cut.** Journal `target-started` (`a74392cf…`), marker 14:57:31.630141Z, SIGKILL 14:57:31.639768Z (PID 1692, `T`, 137).
- Status before recovery: `accepted-active (journal phase target-started)`, no owner command. Startup rollback, retry 1 forward (exit 0, 4.23 s), retry 2 idempotent; probes `target_converged` (`42e89f1a…`); 31/31. Reboot 16.8 s: passed, 31/31.
- **Compared with earlier runs:** batch 4 `c7` (`8f86bdad`) passed with the same flags; `fresh-install-20260929` cell 2 passed without reboot.

## Cell 5 `c05-v2-prestart` — PASSED (with retry switch)

- Request `b66cbdc5c51b25639364f76a7113a214`. Controller 15:01:53.08Z–15:04:11.51Z. Variant `pre-start`.
- **Cut.** V2 journal `target-staged` (`035f4e5e…`), marker 15:02:03.740586Z, SIGKILL 15:02:03.787912Z (PID 3984, `T`, 137). PowerDNS MainPID 3077 before the cut.
- **Agent decides.** The restarted Agent wrote `rolling-back`, released the job `failed`/`interrupted`/`dns_native_recovery_unknown_after_restart` and logged:
  > `Interrupted DNS switch native recovery is unknown; retain exact journal and release only its ledger lease (request b66cbdc5c51b25639364f76a7113a214): v2 BIND switch journal requires its independent inverse adapter; the owner recovery command for this PowerDNS-to-BIND rollback is /usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id b66cbdc5c51b25639364f76a7113a214, which the server owner runs when recovery dns-switch-status --quiesced --request-id b66cbdc5c51b25639364f76a7113a214 names it; preserve the journal, ledger and native DNS until that exact request is reconciled`
  PowerDNS kept serving alone with MainPID 3077.
- **Status** (exit 0, no evidence change): `DNS switch request b66cbdc5c51b25639364f76a7113a214: released-undecided-with-journal (journal phase rolling-back).` and names the command.
- **Owner command** exit **0**, 8.62 s; summary lines verbatim:
  > `Restored: the PowerDNS service, its DNS state receipt, the BIND configuration files this switch changed and the managed BIND pointer.`
  > `BIND units: named.service and bind9.service are under the package guard's persistent mask, inactive and not enabled, so BIND cannot start by accident. A later switch lifts this mask itself.`
  > `Removed: the staged BIND generation ae4516bb90c5796390cc023653cd7cfff93684a3d3feb1e103f6b503b1445745 that this switch created.`
  > `Intentionally kept as rollback standby: the installed bind9 packages, the rndc key and the BIND install-ownership record. This command does not remove packages.`
- **Rollback standby** (`rollback_end_state`, no failures): the journal froze both BIND units as `not-found`; afterwards `named.service` and `bind9.service` are `masked`/`masked`, inactive, persistent `/dev/null` links root-owned, no runtime masks; the staged generation tree is absent. Ledger byte-identical to the release, state receipt bytes unchanged, owner files unchanged, PowerDNS MainPID 3077 throughout.
- **Re-run** exit **0**, no evidence change: `Request b66cbdc5c51b25639364f76a7113a214 is already reconciled: … Nothing was changed now. This request no longer blocks DNS changes. …`. Probes `rolled_back_source_active` (`46ef1847…`), classification `rolled_back_source_serving`; 31/31.
- **Reboot after recovery** (14.4 s): PowerDNS alone (new MainPID 651, recorded), owner files byte-identical, second window 31/31, passed.
- **Retry switch** (`retry_switch_after_rollback`, passed): new request `69124b85e5b2613c5b780cb426152841`, trigger exit 0 (11.6 s), job `succeeded` attempt 1 with the finalized phase, journal retired, BIND serving alone (`named` MainPID 6799), `pdns.service` inactive/disabled, both BIND names `loaded`/`enabled` (mask lifted by the product), probes `target_converged`, 31 samples. The forward path accepted the rollback standby.
- **Compared with earlier runs:** `owner-inverse-after-restart-rerun-20260929` cell 1 (`411398d9`) passed without reboot or retry; there the owner re-run exited 3.

## Cell 6 `c06-v2-poststop` — PASSED (critical variant, two reboots, retry switch)

- Request `341e457da7b4b5e6fb7949e666307ef2`. Controller 15:06:38.78Z–15:10:29.52Z. Variant `critical`.
- **Cut.** V2 journal `target-started` (`cdc37f0f…`), marker 15:06:54.869160Z, SIGKILL 15:06:54.918211Z (PID 3976, `T`, 137). PowerDNS (pre-cut MainPID 3074) had been stopped by the product at 15:06:50.55.
- **Agent decides.** Release `failed`/`interrupted`/`dns_native_recovery_unknown_after_restart`, journal `rolling-back`, refusal naming `recover-dns-bind-switch --request-id 341e457da7b4b5e6fb7949e666307ef2` (same wording as `c05`). Recorded: BIND active (MainPID 7690, `named.service` and `bind9.service` enabled), PowerDNS inactive, DNS answered (by BIND).
- **Reboot before the owner command** (`9d676c06…` → `8d546458…`, 16.7 s): management units up, journal still `rolling-back`, ledger byte-identical, no private-evidence change. Recorded, not judged: after the boot BIND came up by itself (enabled, MainPID 652) and answered; PowerDNS stayed inactive.
- **Status** exit 0: `released-undecided-with-journal (journal phase rolling-back)`, names the command.
- **Owner command** exit **0**, 13.07 s. Same `Restored:`, `BIND units:` and `Intentionally kept …` lines as `c05`; `Removed: the staged BIND generation e9161bc0fffb55a2f8cb77f3c17439f4e695b070e91e71230dc820c4ce32a39c that this switch created.`; plus `Left in BIND's working directory /var/cache/bind, which is outside the managed BIND root, so they were not removed: /var/cache/bind/managed-keys.bind (1411 bytes, owner 104:105), /var/cache/bind/managed-keys.bind.jnl (2590 bytes, owner 104:105).` and the accepted `systemd-resolved` stub-listener line.
- **After:** PowerDNS active/enabled, sole authority (new MainPID 9117, recorded as expected); BIND units `masked`/`masked`, inactive, no `named` process — BIND **had started** here, so this is the compensate-then-seal path of the rollback standby; staged tree absent; ledger unchanged since release; state bytes unchanged; owner files unchanged. `dns_outage` (not judged): PowerDNS stopped 15:06:50.553Z, first answer after the command 15:08:26.984Z, **96.4 s**, which includes the deliberate reboot before the owner command; BIND answered for part of that window.
- Re-run exit **0**, no evidence change. Probes `rolled_back_source_active` (`f27374b0…`); 31/31. Reboot after recovery (`8d546458…` → `86ea77fa…`, 17.1 s): PowerDNS alone (MainPID 654), owner files identical, 31/31, passed.
- **Retry switch** passed: request `cc39099cb630a87312b164178ba50918`, trigger exit 0 (12.6 s), job `succeeded`, `named` MainPID 6526 alone, BIND units `loaded`/`enabled`, PowerDNS inactive/disabled, `target_converged`, 31 samples.
- **Compared with earlier runs:** batch 4 `c4` (`8f86bdad`) passed with both reboots but ended with `named.service` `loaded`/`disabled` and no masks (before the rollback-standby change) and had no retry; `owner-inverse-critical-20260929` cell 1 passed without reboots.

## Cell 7 `c07-bind-adopt` — PASSED (two reboots)

- Request `8b14693eaf9461f13a415d5e637b6d91`. Controller 15:12:41.27Z–15:15:38.60Z.
- **Cut.** V2 journal `rolling-back` (`d71d8395…`, the precursor wrote it), marker 15:12:48.117740Z, SIGKILL 15:12:48.215622Z (PID 2408, `T`, 137); window 98 ms, containing only `named`'s own two managed-keys timer lines (above).
- **Agent decides:** release with `dns_native_recovery_unknown_after_restart`, journal kept, refusal:
  > `… (request 8b14693eaf9461f13a415d5e637b6d91): running BIND adoption needs independent no-stop recovery; the owner recovery command for this rollback is /usr/libexec/celikpanel/recovery recover-dns-bind-adoption --request-id 8b14693eaf9461f13a415d5e637b6d91, …`
  The owner's `named` kept MainPID 2042.
- Reboot before the owner command (`53efc75d…` → `92d8d7a6…`, 19.0 s): owner BIND serving alone again by itself (MainPID 657), journal and ledger unchanged. Status exit 0, `released-undecided-with-journal`, names `recover-dns-bind-adoption`.
- **Owner command** exit **0**, 10.63 s: `The accepted running BIND adoption rollback reached its terminal verdict for request 8b14693eaf9461f13a415d5e637b6d91. The original owner BIND zones were verified without stopping named. …`. `named` MainPID stayed 657 (no restart), owner files byte-identical, ledger unchanged, no state receipt. Re-run exit **0**, no change. Probes `rolled_back_source_active` (`fa671515…`); 31/31.
- Reboot after recovery (`92d8d7a6…` → `1d4af10d…`, 16.7 s): owner BIND alone (MainPID 655), owner files byte-identical (judged), 31/31, passed.
- **Compared with earlier runs:** batch 4 `c1` (`8f86bdad`) passed without reboot (reboot flags were not admitted then) and its re-run exited 3.

## Cell 8 `c08-pdns-adopt-rb` — PASSED

- Request `18400bad79662787153c9583ba87bc0e`. Controller 15:18:41.42Z–15:20:14.15Z.
- **Cut.** V1 adoption journal `rolled-back` (`2af7b7b6…`), marker 15:18:45.844658Z, SIGKILL 15:18:45.856216Z (PID 2180, `T`, 137).
- **Status before recovery** (Agent down; exit 0, no change): `DNS switch request 18400bad79662787153c9583ba87bc0e: accepted-active (journal phase rolled-back).` / `This PowerDNS adoption retains a rollback decision. The server owner can continue that exact inverse with: /usr/libexec/celikpanel/recovery recover-dns-pdns-adoption --request-id 18400bad79662787153c9583ba87bc0e. …` (recorded, not judged; the Agent then finished it without that command).
- **Agent startup rollback** (`agent_startup_rollback`, no failures, no unknowns): job `failed`/`interrupted`/`dns_engine_switch_rolled_back_after_restart`, journal retired, PowerDNS kept MainPID 1769, the Agent journal did not name `recover-dns-pdns-adoption`.
- Retry 1 re-ran the adoption forward (exit 0, 5.15 s; job `succeeded`, attempt 2), retry 2 idempotent; probes `target_converged` (`9b95ed86…`); 31/31. Reboot 19.3 s: PowerDNS alone, 31/31, passed.
- **Compared with earlier runs:** batch 4 `c2` (`8f86bdad`) ran this cut on the old behaviour (Agent refused at `rolled-back`, owner command finished it) and failed on the harness probe.

## Cell 9 `c09-pdns-adopt-intent` — PASSED

- Request `6db68749d7b24c88419ecbda31e11413`. Controller 15:23:35.80Z–15:24:16.81Z. No reboot flag was requested.
- Cut at `intent` (`1bb120a6…`), marker 15:23:38.679143Z, SIGKILL 15:23:38.689786Z (PID 2156, `T`, 137). Status before recovery: `accepted-active (journal phase intent)`, no owner command. Agent startup rollback as in `c08` (`dns_engine_switch_rolled_back_after_restart`, journal retired, no owner command named). Retry 1 forward (5.05 s), retry 2 idempotent; `target_converged` (`c75d51e5…`, the same fingerprint as batch 4 `c3`); 31/31.
- **Compared with earlier runs:** batch 4 `c3` passed (with a reboot after recovery).

## Cell 10 `c10-takeover` — PASSED

- Request `b66cbdc5c51b25639364f76a7113a214` (derived from the cell ID, equal to `c05`'s; different guests, work root `r2`). Controller 15:27:57.44Z–15:29:32.01Z.
- **Cut.** V1 journal `target-staged` (`8d2ef877…`), marker 15:28:01.251917Z, SIGKILL 15:28:01.260738Z (PID 2423, `T`, 137). At the cut `dns-engine-install-ownership-bind.json` names `bind9`, this request, `adopted_present: true`, `missing_before: []` (`f2d3f83b…`).
- **Status before recovery** exited **3** (recorded; only mutation is judged, and there was none). Its last line:
  > `Native BIND vendor files or loaded systemd unit identity are unknown. The server owner should inspect the named service unit, its package ownership and startup options before the same operation resumes; no inverse was started. parse named.service identity: systemctl returned incomplete DNS unit identity`
  I read this as the status reader not classifying an owner-installed, disabled `named.service` before the takeover completed; not established, no fix made.
- Recovery: the restarted Agent rolled the cut back; retry 1 (exit 0, 10.3 s) re-ran the same request forward, and the job ended `succeeded` at **attempt 2** (`raw/state/service-mutations.json`); retry 2 idempotent; `target_converged` (`bac6f937…`); BIND alone; 31/31. Owner files: `named.conf`, `named.conf.root-hints`, `rndc.key`, `/etc/default/named` byte-identical (judged); `named.conf.options` and `named.conf.local` rewritten by the takeover (recorded). Reboot 16.7 s: passed, 31/31.
- **Compared with earlier runs:** batch 5 `c5` (`65b86621`) passed without reboot, same fingerprint `bac6f937…`.

## Cell 11 `c11-bindsec-bindpri` — PASSED (peer passed)

- Request `2228e0de67c2e95ae9675187f214d1b5`. Controller 15:32:41.00Z–15:34:54.59Z.
- **Cut.** Journal `target-verified` (`d30ac941…`), marker 15:33:34.256279Z, SIGKILL 15:33:34.273720Z (PID 1742, `T`, 137). The `current` pointer (→ `generations/74c26315…`, mtime 15:33:28.044487) was untouched: the sampler's `POINTER` signature did not change after 15:33:28.16 (`raw/watch/cp-b6a-watch/timeline.log`).
- Agent startup converged; both retries exit 0 (idempotent); probes `target_converged` (`8d7a0cd6…`); 31/31. Catalog-format log lines (tagged and restarted Agent, three, all BIND):
  > `DNS engine change recovery 2228e0de67c2e95ae9675187f214d1b5: the paired primary at 192.0.2.11 serves catalog catalog-c000020b.celikpanel.invalid in the BIND catalog format; this operation reads it in that format`
- Management disabled and proven, reboot 15.7 s, `named` alone, same answer counts, second window 31/31 DNS-only, passed.
- **Peer verdict passed**, `combined_exit` 0: producer `bind`, Agent format name `BIND`, catalog serial 1, member `s1-kill.test`, catalog and member transferred to `192.0.2.10`.
- **Compared with earlier runs:** batch 5 `c1` (`65b86621`) failed this cell (`repeated_nonconvergence`, pointer removed 8 ms after the marker, the kill-hook race signature).

## Cell 12 `c12-bindsec-pdnsnative` — PASSED (peer passed)

- Same request ID as `c11` (work root `r2`). Controller 15:38:39.02Z–15:40:26.75Z.
- **Cut.** Journal `target-verified` (`d30ac941…`), marker 15:39:03.519529Z, SIGKILL 15:39:03.530509Z (PID 1761, `T`, 137).
- Recovery as `c11`: `target_converged` (`8d7a0cd6…`), 31/31, management-disabled reboot 19.1 s, 31/31 DNS-only, passed. All three catalog-format lines name **PowerDNS**:
  > `DNS engine change recovery 2228e0de67c2e95ae9675187f214d1b5: the paired primary at 192.0.2.11 serves catalog catalog-c000020b.celikpanel.invalid in the PowerDNS catalog format; this operation reads it in that format`
- **Peer verdict passed**, `combined_exit` 0: PowerDNS 5.1.4 serving its native `PRODUCER` catalog (`domains`: catalog `PRODUCER`, member `MASTER` in that catalog), producer `powerdns`, catalog serial **1790696284** (PowerDNS-maintained), `catalog_metadata_kinds` `["CATALOG-HASH"]` (PowerDNS wrote its own producer metadata; the fixture writes none), catalog and member transferred to `192.0.2.10`.
- **Compared with earlier runs:** none with the native catalog. Batch 5 `c2` ran this cell against a PowerDNS peer serving the BIND catalog format and passed.

## What remains on the host

- `/var/tmp/cp-b6a-0929` (1.3 GB apparent): both work roots (`r2` nested) with locked image hard links, artifacts, logs and collected evidence. Every cell was torn down; `cells/` is empty in both roots. No QEMU process runs.
- `/root/cp-b6a-src`, `/root/cp-b6a-artifacts`, `/root/cp-b6a-tools`, `/var/tmp/cp-b6a-build.log`, `/var/tmp/cp-b6a-setup.log`, `/var/tmp/cp-b6a-webdist.sha256`. Nothing outside `cp-b6a*` was created or deleted.

## Deviations

- Two work roots (see Build).
- `c02`: the first-boot sampler died under the measured Arch upgrade (above); the Arch kill guest rebooted into a kernel installed by the measured operation.
- `c11`/`c12`: no recovery runtime (enrollment is standalone-only), so no status reads; `service-mutation-status-post-collect.json` could not reach the Agent after the management-disabled reboot (`smstatus.stderr`), as in batch 5.
- Package versions are live-mirror versions; the Arch peer preparation runs a full `pacman -Syu`.
- No re-run, no harness workaround, no pass-rule change. PowerDNS-secondary and reinstall cells were deliberately not run.

## What this does not prove

Twelve passes, one run each, on one host, in disposable guests; eleven on Debian 13, one on Arch. The boundary held in these twelve cuts (no native mutation between marker and SIGKILL, apart from `named`'s own trust-anchor timer in `c07`); that is evidence about these runs of the fixed test hook, not a proof that the race is gone for every boundary, and the earlier failed or caveated cells at other boundaries still need their own re-runs. The missing-pointer repair was **not exercised** (the pointer was intact in every cell), so it has component tests only. All reboots were orderly `systemctl reboot`; no power loss, no owner edit during recovery, no peer-unreachable cell, no PowerDNS secondary, no reinstall, no CelikPanel-to-CelikPanel pair, no continuous probe on the kill guest itself. `dns_outage` in `c06` is an upper bound that includes a deliberate reboot. The status-text observations in `c08` and `c10` are recorded, not judged. Commits `2ac6dcbe`, `66db850c` and `93b9b1f0`, made during the run, were not tested. Nothing here changes the 268-runnable denominator, closes P0.4 or P0.5, or passes any acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file except this README and itself.
