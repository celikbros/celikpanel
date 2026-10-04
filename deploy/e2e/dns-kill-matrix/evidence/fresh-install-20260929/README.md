# Fresh-install native kill cells (D-026), 2026-09-29

Scope: D-026 decision 2 (same-operation Agent recovery for a first engine install), D-025 invariants 1, 2 and 4, P0.4. This run covers four standalone `uninitialized` (`absent-by-proof`) cells in fresh disposable QEMU guests on the local WSL `archlinux` host. It did not touch an installed panel, a remote host, a release or a signed bundle. **Result: 3 passed, 1 verified safety failure.** The failed cell is a real finding and must not be counted as passing.

## Build and fixture

- Commit `574cadf2a040965bc41c3a8bd083f455e3892759` (`feat/dns-artifact-separation`) was extracted with `git archive` into guest ext4 (`/root/cp-d026-src`). The harness (`fixture.py`, `guest_bootstrap.py`, `run_cell.py`, `guest_recovery_probe.py`) ran from that tree.
- Toolchain: `/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go`, `go version go1.26.5 linux/amd64` (`go.mod` says `go 1.26.5`), `GOTOOLCHAIN=local CGO_ENABLED=0 -trimpath -buildvcs=false`. See [build/build.log](build/build.log).
- Binaries ([build/artifacts.sha256](build/artifacts.sha256)):

| Binary | Build | SHA-256 |
| --- | --- | --- |
| `agent` | `./cmd/agent` | `166eda4bbf380a23f57a2398b284f796029d34f9c6b3d84ba06cf7af42942a60` |
| `agent.kill` | `./cmd/agent`, `-tags dns_kill_matrix` | `d6ef322d2ea57cf81df9f4b969d41bf7f722ab18df28c9d7ad8c1d536050d5db` |
| `panel` | `./cmd/panel` | `793fb81b95fa96f438a216831a146e0869934ce31318881857d3352ffb92e472` |
| `dns-kill-trigger` | `./cmd/dns-kill-matrix-trigger` | `97aa0ce32546e9ba6ae56831e9f300b1f97133893449422811937e365bdfc274` |

- `web/dist` is untracked. It was copied from the local Windows checkout, built 2026-09-27. Its file hashes are listed in [build/web-dist.sha256](build/web-dist.sha256), for example `index.html` `9808280612b052b1346f04281a7931acdd50c980d355e30640ff147ebd28e8e5`. The panel needs it only to start.
- Work root `/var/tmp/cp-d026-0929`. The locked base images were hard-linked from `/var/tmp/cp-v3n28/images` and passed `fixture.py verify-images`: Debian 13 `20260826-2582` and Arch `20260815.573966`, pinned in `images.lock.json`. Nothing was downloaded. See [build/work-root-setup.log](build/work-root-setup.log).
- The guest manifest was byte-identical to the tracked `manifest.json` (`dcf6e09c026864ee15566a03b3c4aa30d6e1805333d548753b58d070e7fd1229`). That copy is omitted from each cell directory.
- Each cell used a new overlay pair and ran `prepare` → `start` → `wait-ssh` → `guest_bootstrap.py install` → `prepare-pdns-switch` or `prepare-bind --source-fixture uninitialized` → `run-prepared`. `run-prepared` runs the controller as `root:celikpanel` under `env -i` with `--agent-token-file`. The controller performs the kill, restarts the ordinary Agent, runs two same-request `rpc-retry` attempts and two read-only probes, restarts Panel, and takes 31 health samples over 30 s. After capture, the overlays were stopped through QMP and torn down.
- Placement was taken from `manifest.json` and matched the brief: cells 1, 2 and 4 on Debian 13 and cell 3 on Arch.

Each cell directory holds the controller artifacts under `raw/results/<cell>/`: `result.json`, `kill-proof.json`, `boundary-marker.json` and `transcript.jsonl`. `raw/fixture/` holds the scenario, source proof, controller argv and trigger identity. `raw/state/` holds the post-run ledger, state and ownership receipts plus any retained journal. `raw/journald/` holds the unit journals. `guest-recovery-probe-post-collect.json` is a third read-only probe taken after the controller finished. `dns-post-collect.txt` holds independent stdlib UDP/TCP queries for `www.s1-kill.test A`. `raw/watch/` holds a read-only 0.2–0.5 s systemd/journal-phase sampler that ran beside the controller. The sampler is auxiliary and not controller evidence.

## Cell 1: `pdns-switch__target-staged__after-write__standalone__peer-reachable` — FAILED (verified safety failure)

- Debian 13, request `c60a3d86859d1980ad1d13ce8b86a793`, scenario SHA-256 `1eafee63015facb31b3fd759c6967c1c8c72440c65c7ca5209e3a906db557acf`. Cell 2 uses the same scenario bytes, and both BIND cells use `5351462df920b73d4fec98c527062afdd2e0d99445f23597169ab0346501a64e`.
- Kill: tagged Agent PID 1492 (start ticks 6808), state `T`, SIGKILL at 22:26:16.793Z, raw return −9, normalized exit 137, `/proc` entry absent after reap. `kill_proven: true`. The journal at the cut was `target-staged` (sha256 `49ddaaaf…a890`). The trigger exited 75 (transport EOF).
- **Genuine pre-start cut.** Before the run, `pdns.service` was `LoadState=not-found`. The measured operation installed `pdns-server` and `pdns-backend-sqlite3` 4.9.17-0+deb13u1, and the install-ownership receipt records both as `missing_before`. At the kill-proof snapshot and after recovery, `pdns.service` was `LoadState=masked`/`UnitFileState=masked` (`/etc/systemd/system/pdns.service -> /dev/null`), `ExecMainStartTimestamp` was empty, `ExecMainPID=0` and `NRestarts=0`. `journalctl -u pdns.service` reported `-- No entries --`. PowerDNS never started.
- Agent startup recovery (verbatim): `Interrupted DNS switch native recovery is unknown; retain exact journal and release only its ledger lease (request c60a3d86859d1980ad1d13ce8b86a793): PowerDNS switch rollback verify target stopped: DNS target is not a loaded unit`. The journal was retained at phase `rolling-back`. The ledger job ended `failed`/`interrupted` with `dns_native_recovery_unknown_after_restart`.
- Both same-request retries exited 1: `validate retry failed receipt: service mutation job is not an exact resumable DNS switch failure`.
- Both probes were `indeterminate` with the same fingerprint `89b41e11…ca30`. `result.json`: `status: failed`, `safety_status: failed`, classification `repeated_nonconvergence`. DNS on `10.0.2.15:53` was refused before the retry, after restart and in all 31 stability samples. Agent and Panel were healthy in all 31 samples.
- Code reading, not a fix: the V1 PowerDNS rollback verifies the stopped target with the non-mask verifier. `cmd/agent/dns_engine_pdns_switch.go:1427-1428` calls `verifyPDNSStoppedBeforeDatabaseRestore`, and that path goes through `internal/dnsenginerecovery/stopped_unit.go:70-71`, which requires `LoadState=loaded`. The fresh install leaves the target persistently masked at `target-staged`. The trigger's `validateRetryableFailedJob` (`cmd/dns-kill-matrix-trigger/main.go:1573-1598`) does not admit `dns_native_recovery_unknown_after_restart`. As a result, the D-026 same-operation recovery contract was **not** met at this pre-start boundary. The operator-facing message asks the owner to restart the Agent. This run did not test whether another restart changes the outcome.

## Cell 2: `pdns-switch__target-started__after-write__standalone__peer-reachable` — PASSED

- Debian 13, request `8fcd357e26d39c4e54c59713e3052809`.
- Kill: PID 1496 (ticks 6711), state `T`, SIGKILL at 22:30:37.646Z, exit 137, reaped. Journal at the cut: `target-started` (`a74392cf…6208`). `pdns_server` PID 3045 had bound UDP/TCP 53 at 22:30:37.572Z, before the cut.
- Recovery: the ordinary Agent's startup path rolled the fresh install back. The journal passed through `rolling-back` and was then retired, `pdns.service` became inactive, and the pre-retry probe was `indeterminate` with pre-retry DNS refused. Retry 1 (3.89 s) re-executed the same request forward: `intent` → … → `target-started` → `committed` → retired, with new `pdns_server` PID 3782 started at 22:30:40. Retry 2 returned idempotent success in 0.009 s. The ledger shows `succeeded`, **attempt 2**, finalized v2 phase.
- Probes: `target_converged`, active engine `pdns`, matching fingerprint `42e89f1a…db17`. Classification `target_converged`. After restart, authoritative UDP and TCP answers were returned: 1 answer, flags 0x8400. Health was 31/31 (Agent, Panel, UDP+TCP DNS) from 22:30:42.94Z to 22:31:12.94Z. The independent post-collect queries to `10.0.2.15` and `192.0.2.10` returned `aa` and `NOERROR` with `192.0.2.10` over UDP and TCP.
- DNS was **not** continuous: PowerDNS was stopped from about 22:30:37.8Z to 22:30:40.8Z while the Agent rolled back and the retry re-ran.

## Cell 3: `bind__target-verified__before-write__standalone__peer-reachable` — PASSED

- Arch (manifest `kill_host: arch`), request `8d6edfff6835819901869204d21d7bae`. BIND 9.20.29-1 was installed inside the measured operation.
- Kill: PID 1354 (ticks 6080), state `T`, SIGKILL at 22:34:09.480Z, exit 137, reaped. The on-disk journal at the cut was `target-started` (`d84d3b8c…3161`), the exact predecessor. The marker was recorded at the `target-verified` `before_write` hook. `dns-engine-state.json` had been written at 22:34:09.462Z, before the cut. `named` PID 8603 had started at 22:34:08Z.
- Recovery: Agent startup alone converged the same request forward. The pre-retry probe was already `target_converged` and pre-retry UDP/TCP DNS was OK. Both retries returned idempotent success in about 0.008 s. The ledger shows `succeeded`, attempt 1. After the run, `named` still had `ExecMainPID=8603` and `NRestarts=0`. The same process served before and after the cut, but DNS answers were not sampled during the kill window.
- Probes: `target_converged`, active engine `bind`, fingerprint `1eb4c171…62a9`. Health was 31/31 with authoritative UDP+TCP answers. Post-collect queries to `10.0.2.15` and `127.0.0.1` returned `aa`, `192.0.2.10`, over UDP and TCP. On this guest, `192.0.2.10` is the peer address and was not served.
- Observation: the Arch measured BIND install ran a pacman transaction that also upgraded base packages, including glibc, systemd 261.2→262, openssl, coreutils and bash ([pacman-log-measured-window.txt](bind__target-verified__before-write__standalone__peer-reachable/pacman-log-measured-window.txt)). This broke the auxiliary sampler at 22:33:43Z (`/usr/bin/date: cannot execute`), so `raw/watch/` has no at-kill or at-result snapshot for this cell. The controller evidence is unaffected.

## Cell 4: `bind__target-verified__after-write__standalone__peer-reachable` — PASSED

- Debian 13, request `abd7cb29722819ec4117f95c8deddce5`.
- Kill: PID 1495 (ticks 6598), state `T`, SIGKILL at 22:37:17.880Z, exit 137, reaped. Journal at the cut: `target-verified` (`4d6af030…aaaa`). `named` PID 4308 had been active and running since 22:37:15.7Z.
- Recovery: Agent startup converged forward. The journal went `target-verified` → `committed` → retired. The pre-retry probe was `target_converged` and pre-retry DNS was OK. Both retries returned idempotent success in about 0.008 s. The ledger shows `succeeded`, attempt 1. `named` kept PID 4308 and `NRestarts=0`.
- Probes: `target_converged`, `bind`, fingerprint `13b3b367…ac07`. Health was 31/31 with authoritative UDP+TCP answers. Post-collect queries to `10.0.2.15`, `192.0.2.10` and `127.0.0.1` all returned `aa`, `192.0.2.10`, over UDP and TCP.

## What this does not prove

It does not show DNS continuity through any cut: no answers were sampled between the kill and recovery, and cell 2 had a measured stop gap. It does not cover owner edits, reboot or power loss (no guest was rebooted), paired topology or fresh paired secondaries, or an Agent-independent inverse. It does not cover a signed or published release, an installed server, or any other matrix cell. The standalone `peer-reachable` label is an invariance control only. Cell 1 is a failed D-026 cell and leaves fresh standalone PowerDNS pre-start recovery open. Only cells 2–4 add passed native evidence. Nothing here changes the 268-runnable denominator or closes P0.4.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file except this README and itself.
