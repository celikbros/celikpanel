# Owner inverse after Agent restart: critical variant, 2026-09-29

Scope: P0.4, D-025 invariants 1, 2, 4 and 5, D-024. This is the first native run of the two critical cells that commit `7c5dfe17` added to the `--owner-inverse-after-restart` controller flow (see "Critical variant: source stopped before the cut" in the [harness README](../../README.md)). It repeats the procedure of the [pre-start re-run](../owner-inverse-after-restart-rerun-20260929/README.md) (commit `411398d9`, both pre-start cells passed) with different cells. The run used fresh disposable QEMU guests on the local WSL `archlinux` host. It did not touch an installed panel, a remote host, a release or a signed bundle. **Result: 2 passed** (`result.json` `status: passed`, `safety_status: passed`, `recovery_outcome.classification: rolled_back_source_serving`, `owner_inverse_after_restart.variant: critical`, no failures and no ambiguities, in both cells).

In both cells the owner command exited 0 with the same text as in the pre-start run:

```
The accepted BIND switch rollback reached its terminal verdict for request <id>. Native PowerDNS was verified during recovery. Check current authoritative DNS health before another switch.
```

It wrote the `rolled-back` checkpoint, retired the journal and started PowerDNS again. The ledger stayed byte-identical to the Agent's release, the state receipt stayed byte-identical to the pre-cut source, and no owner PowerDNS file changed. **DNS was not continuous in either cell; this is by construction of these cuts** (see "DNS outage" per cell).

## Build and fixture

- Tested source: commit `7c5dfe17540d7e06b03095cbad8700bc2c454792` (`feat/dns-artifact-separation`), extracted with `git archive 7c5dfe17` into guest ext4 (`/root/cp-oi3-src`). The repository HEAD at run time was the same commit. Between `411398d9` and `7c5dfe17` no Go code changed: only `deploy/e2e/dns-kill-matrix/{README.md,guest_bootstrap.py,run_cell.py,test_*.py}`, docs and evidence differ. The harness (`fixture.py`, `guest_bootstrap.py`, `run_cell.py`, `guest_recovery_probe.py`) ran from the `7c5dfe17` tree.
- Toolchain: `/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go`, `go1.26.5 linux/amd64`, `GOTOOLCHAIN=local CGO_ENABLED=0 -trimpath -buildvcs=false`. See [build/build.log](build/build.log) and [build/artifacts.sha256](build/artifacts.sha256).
- **Every product binary and the recovery runtime bundle are byte-identical to the `411398d9` build.** `build.log` ends with a `cmp` of each file against `/root/cp-oi2-artifacts` and a diff of the full runtime tree; all report `IDENTICAL`.

| Binary | Build | SHA-256 | vs `411398d9` run |
| --- | --- | --- | --- |
| `agent` | `./cmd/agent` | `4f8c628646b7efa2c143102f845b9e13b02d7c36a85e9a93d1e9f049e1bfa5b7` | identical |
| `agent.kill` | `./cmd/agent`, `-tags dns_kill_matrix` | `f1074078381976313e0833dcce7dd8b751e5a4b5e039f1388260792d1ee5c8ba` | identical |
| `panel` | `./cmd/panel` | `793fb81b95fa96f438a216831a146e0869934ce31318881857d3352ffb92e472` | identical |
| `dns-kill-trigger` | `./cmd/dns-kill-matrix-trigger` | `97aa0ce32546e9ba6ae56831e9f300b1f97133893449422811937e365bdfc274` | identical |
| `recovery` | `./cmd/recovery` | `5268ed0058dc8d7619df2cbeb16661ef71eab8cf75a488746194c4104e92dde6` | identical |
| `agent-checker` | `deploy/recovery/agent-checker.sources` | `82f2772fe0257dde9139d0f85a34be58e89c98d6a9d1775090f2ecf4cc3b8975` | identical |
| `panel-checker` | `deploy/recovery/panel-checker.sources` | `82f23b48908e152e55ad76598ec8be0f780805f463fe7e48ed402d87bc5984e0` | identical |
| `schema17-bridge` | `./deploy/schema17bridge` | `fe277c46ea8b50f00a80e71976198f28db89667789a737e0a78c0f4a83e9cb87` | identical |
| recovery runtime `runtime.manifest` | `go run ./deploy/recovery/bundle` | `a682b35afd664df01436b6dfb25040afda352dff7e4557586665e90a93245646` | identical |

- `web/dist` is untracked. It was copied from the local Windows checkout; its 103 file hashes ([build/web-dist.sha256](build/web-dist.sha256)) are identical to the previous run's list.
- Work root `/var/tmp/cp-oi3-0929`. The locked base images were hard-linked from `/var/tmp/cp-v3n28/images` and passed `fixture.py verify-images` ([build/work-root-setup.log](build/work-root-setup.log)). The guest manifest was byte-identical to the tracked `manifest.json` (`dcf6e09c…d1229`) and is omitted from the cell directories.
- Each cell ran `prepare` → `start` → `wait-ssh` → `guest_bootstrap.py install` → `enroll-recovery-runtime --execute` → `prepare-bind --source-fixture managed-pdns` → `run-prepared --owner-inverse-after-restart --execute` → collect → `fixture.py stop` and `teardown`. Both enrollments installed launcher `5268ed00…` with runtime manifest `a682b35a…`; the kit tarball hashes differ per enrollment (`426c8b05…`, `ca40d73e…`) because the tar is rebuilt each time. The controller's preflight `recovery check-bind-source-inverse-v1` returned 0 with `celikpanel-bind-source-inverse/v1`.
- Both cells used scenario SHA-256 `3bc1460074a57105ad1bb1ecdc56cec8ddbd6e37531443da0a7c18e2ce11dc26`, the same as the previous runs.
- The managed PowerDNS source was established as before: fixture preinstall of `pdns-server`/`pdns-backend-sqlite3` `4.9.17-0+deb13u1`, then the product's `pdns-adopt` RPC and `rpc-normalize-pdns`. Before the measured operation, `pdns.service` was `loaded`/`active`/`enabled`, `named.service` and `bind9.service` were `not-found`, no `bind9` package was installed, and UDP and TCP queries for `www.s1-kill.test A` to `10.0.2.15` and `192.0.2.10` returned `aa`, `NOERROR`, `192.0.2.10` (`dns-pre-run.txt`).

### Evidence layout

The layout is the previous run's (`raw/results/` flattened; `owner-post-state.txt`; `service-mutation-status-post-collect.json` from the same read-only reader, binary `ff9abfa1…7890`, byte-identical), with these changes:

- **Short cell directories:** `target-started/` and `source-stopped/`.
- **`soa-pre-run.txt` and `soa-post-collect.txt` are new.** Independent read-only SOA queries for `s1-kill.test` over UDP and TCP to `10.0.2.15`, `192.0.2.10` and `127.0.0.1` (`build/fixture-tools/soaq.py`). The pre-run query was made before the watcher and the controller started, while PowerDNS served alone; the post query after the controller finished.
- **`native-and-outage.json` is new.** A derived, verbatim copy of `result.json`'s `owner_inverse_native_at_boundary`, `dns_outage`, the step-2 and step-5 `native` records and the PowerDNS PID record (`build/fixture-tools/extract.py`). `result.json` stays authoritative.
- **`owner-post-state.txt` additionally covers** `systemctl show`/`is-enabled`/`is-active` for `pdns.service` (as well as `named.service` and `bind9.service`), `UnitFilePreset`, `pdns_server` processes, `dpkg -l 'pdns*'` and the `/var/lib/bind` tree. It still contains the mask-symlink state, `dpkg -l 'bind9*'`, every file under `/var/cache/bind` (the managed BIND root is `/var/cache/bind/celikpanel`) and `/etc/bind` with hashes and package ownership, port-53 listeners, and one further `recovery dns-switch-status --quiesced --request-id <id>` as root with a clean environment.
- The unit journals of `pdns.service`, `named.service` and `bind9.service` are in `raw/journald/`.

## Cell 1: `bind__target-started__after-write__standalone__peer-reachable` — PASSED

- Debian 13, request `341e457da7b4b5e6fb7949e666307ef2`. The controller ran from 09:27:54.01Z to 09:28:59.22Z (65 s). The whole cell, from `prepare` to `teardown`, took 09:25:32Z–09:31:09Z.
- **Before the cut.** PowerDNS MainPID 3085 (started 09:27:39), serving alone; SOA serial `2026083101` over UDP and TCP on both addresses.
- **Product sequence** (unit journals and sampler): `bind9` installed 09:27:58–09:28:01 (again upgrading `bind9-host`/`bind9-libs` from `1:9.20.26-1~deb13u1` to `1:9.20.29-1~deb13u1`); generation staged 09:28:03; journal V2 `intent`/`target-staged`/`source-stopped` from 09:28:06.3; `pdns.service` Stopping 09:28:06.662, Stopped 09:28:06.674; `named.service` Starting 09:28:10.133, `named` PID 7461 loaded `s1-kill.test` serial `2026083101`, Started 09:28:10.196.
- **Kill.** Tagged Agent PID 3888 (start ticks 13755) was in state `T` and received SIGKILL at 09:28:11.581513Z. Raw return −9, exit 137, reaped: `kill_proven: true`. Trigger exit 75.
- **Journal at the cut.** V2, phase `target-started`, sha256 `cdc37f0f198149c594f7d5785935f176363590c26728f69d8444ea86b060034b`. The sampler copied `intent`, `target-staged` and `source-stopped` bytes; the `target-started` write fell between samples, so only the controller's boundary record holds its hash.
- **Native state at the boundary** (`owner_inverse_native_at_boundary`, 09:28:11.52Z): `pdns.service` `loaded`/`inactive`/`dead`/`disabled`, MainPID 0; `named.service` and `bind9.service` `loaded`/`active`/`running`/`enabled`, MainPID 7461; `named` process 7461; port-53 listeners on `10.0.2.15` owned by 7461 only; the DNS address answered over UDP and TCP (84-byte answers, BIND's shape).
- **Agent decides (step 2).** Ordinary Agent PID 7904 (started 09:28:11). By 09:28:11.94Z it had written the journal at `rolling-back` (sha `cb6aaa63…72c0`) and released the job: `failed`/`interrupted`, `dns_native_recovery_unknown_after_restart`, empty active request. Ledger after release sha256 `191c4cc5f781824795e97337129553c42ab4bc4fc4c14542415bf47f78f17557`, 2936 bytes. The refusal line naming `recover-dns-bind-switch --request-id 341e457d…` was logged at `2026/09/29 09:28:11`.
  - **Native state at step 2** (09:28:11.71Z): identical to the boundary record (PowerDNS inactive/disabled; both BIND names active/enabled with MainPID 7461; `named` 7461 the only port-53 owner; authoritative UDP+TCP answers). `pdns.service` unit journal: stopping 09:28:06.662415Z, stopped 09:28:06.675839Z, no start yet.
  - **Judged:** `pdns_active_state: inactive`, `bind_active_states: [active, active]`. Both hold.
- **Status (step 3).** Exit 0, no evidence change, names the command (2503 bytes, sha `53199821…`). Unlike the pre-start cells it reports running BIND, not the never-started guard mask:
  > `Certified BIND vendor files and systemd unit identity matched across read-only checks. Process liveness, a pending daemon reload, loaded named configuration and DNS answers remain unproved.`
  > `Native unit bind9.service: load=loaded active=active unit-file=enabled.`
  > `Native unit named.service: load=loaded active=active unit-file=enabled.`
  > `Native unit pdns.service: load=loaded active=inactive unit-file=disabled.`
- **Owner command.** Ran once as root from 09:28:12.35Z to 09:28:28.04Z (15.70 s) and exited **0**. Verbatim:
  > `The accepted BIND switch rollback reached its terminal verdict for request 341e457da7b4b5e6fb7949e666307ef2. Native PowerDNS was verified during recovery. Check current authoritative DNS health before another switch.`
  - Inside that window: `named.service` Stopping 09:28:20.221 (`named` "no longer listening" 09:28:20.262), Stopped 09:28:20.267; `/etc/bind/named.conf.local` and `named.conf.options` rewritten 09:28:23.67; `pdns.service` Starting 09:28:24.771, Started 09:28:24.880 (PID 9415), "Done launching threads" 09:28:25.030; journal `rolled-back` (sha `6c7abb5a…`) from 09:28:26.9 and absent from 09:28:28.10.
- **After the command (step 5).** Journal retired; ledger byte-identical to the release (`ledger_unchanged_since_release: true`); state receipt byte-identical to the pre-cut source (`7e16cc4b…`); `owner_files_unchanged: true` (PowerDNS main and managed config, database identity, `quick_check ok`, domain count 1).
  - **Native state** (09:28:28.23Z): `pdns.service` `active`/`running`/`enabled`, MainPID 9415, sole port-53 owner, authoritative UDP+TCP answers; `named.service` `loaded`/`inactive`/`disabled`; `bind9.service` `not-found` (its alias link was removed with the disable); no `named` process.
  - **PowerDNS MainPID:** `{pre_cut: 3085, after_owner_command: 9415, changed: true, judged: false}`. 9415 held after the re-run, after the stability window and in the final `systemctl show` (`NRestarts=0`).
- **Status after the owner command** (09:28:28.28Z, exit 0, no evidence change; `owner-post-state.txt` repeated it at 09:29:06Z with the same bytes, sha `7ee2c36f…`):
  > `DNS switch request 341e457da7b4b5e6fb7949e666307ef2 was interrupted and the Agent could not recover it automatically when it restarted, so it released the lease. The switch has since been reconciled and its journal is retired, either by the owner recovery command or by a later Agent start; this evidence cannot tell which. This request no longer blocks DNS changes. The current DNS engine and its health are shown by the panel's DNS engine status, not by this record. This status check started nothing.`
- **Re-run.** 0.47 s, exit **3**, no evidence change. Verbatim:
  > `Request 341e457da7b4b5e6fb7949e666307ef2 is already reconciled: the Agent released this interrupted DNS switch and its journal has since been retired, by this command or by a later Agent start; the evidence cannot tell which. Nothing was changed now. This request no longer blocks DNS changes. Before another switch, check the current DNS engine and authoritative DNS answers in the panel.`
- **Probes.** Pre-owner `indeterminate` (fingerprint `6159a1a3…`). Probes 1 and 2 `rolled_back_source_active`, same fingerprint `f27374b0…`; the post-collect probe agrees. Classification `rolled_back_source_serving`.
- **DNS outage** (`dns_outage`, `judged: false`): `source_stopped_at` 09:28:06.662415Z (first systemd stopping entry in the `pdns.service` unit journal), `source_serving_again_at` 09:28:28.229107Z (first authoritative UDP+TCP answer the controller observed after the owner command returned), **`seconds: 21.566692`**. This is an upper bound on the PowerDNS source outage, not a guarantee and not a lower bound: the command restarted PowerDNS at 09:28:24.88, before it returned.
  - This figure is **not** the time without DNS answers. In this cell BIND answered in between: the controller received authoritative answers from the `named` 7461 listener at 09:28:11.58Z and 09:28:11.75Z, and the `named` journal shows it running from 09:28:10.196 to 09:28:20.262. From the unit and daemon journals alone, no DNS daemon was running from 09:28:06.674 to 09:28:10.196 (about 3.5 s) and from 09:28:20.262 to 09:28:24.880–25.030 (about 4.6–4.8 s). These two windows come from journal timestamps, not from continuous DNS probing, which this run did not do.
- **Source data.** SOA serial `2026083101` over UDP and TCP to both addresses before the cut (09:27:48Z) and at the end (09:29:14Z); `named` also loaded serial `2026083101` from the staged generation.
- **BIND after rollback** (`owner-post-state.txt`, 09:29:06Z):
  - `named.service`: `LoadState=loaded`, `UnitFileState=disabled` (`UnitFilePreset=enabled`), `inactive`/`dead`, PIDs 0, `NRestarts=0`. `bind9.service`: `not-found`. No persistent or runtime mask symlinks exist for either name. No `named` process. The `named.service` journal has 107 entries (its 10 s run); `bind9.service` has none.
  - `dpkg -l`: `bind9`, `bind9-utils`, `bind9-host`, `bind9-libs` `ii` at `1:9.20.29-1~deb13u1`; `pdns-server`, `pdns-backend-sqlite3` `ii` at `4.9.17-0+deb13u1`.
  - `/etc/bind`: `named.conf.local` and `named.conf.options` hashes equal the journal's `config_before` (`63963de7…`, `f1c37414…`) and their MD5s equal the `bind9` conffile MD5s; `named.conf` and `named.conf.root-hints` keep the package mtime. `rndc.key` is not package-owned.
  - `/var/cache/bind`: the staged generation `e9161bc0…` (receipt, `zones.conf`, one zone file); plus `managed-keys.bind.jnl` (bind:bind, 09:28:10) and a zero-length `tmp-1K50Kr2S9C` (bind:bind, 09:28:20.26, the moment `named` shut down), both written by `named` during its run.
- **Panel-visible message** (`Agent.ServiceMutationStatus`, 09:29:07Z). Stored job unchanged (`failed`/`interrupted`, `dns_native_recovery_unknown_after_restart`); message computed at read time: "An interrupted DNS switch (request 341e457da7b4b5e6fb7949e666307ef2) could not be recovered automatically when the Agent restarted and was reconciled later. Its journal is retired and it no longer blocks DNS changes. Check the current DNS engine status before another switch."
- **Health.** Agent PID 7904 unchanged through the Panel restart. 31/31 samples (Agent, Panel, UDP+TCP DNS) healthy from 09:28:29.16Z to 09:28:59.16Z. Post-collect `A` queries to both addresses returned `aa`, `192.0.2.10` over UDP and TCP.

## Cell 2: `bind__source-stopped__after-write__standalone__peer-reachable` — PASSED

- Debian 13, request `a86368918e6a7aa56a5677177c723e4a`. The controller ran from 09:33:11.69Z to 09:34:04.69Z (53 s). The whole cell took 09:31:15Z–09:35:29Z.
- **Before the cut.** PowerDNS MainPID 3074 (started 09:32:55), serving alone; SOA serial `2026083101` over UDP and TCP on both addresses (09:33:03Z).
- **Product sequence:** install-ownership receipt 09:33:13.66; both BIND names masked 09:33:13.74/14.02 (package guard); `bind9` installed 09:33:15–09:33:19 (same `bind9-host`/`bind9-libs` upgrade); generation staged 09:33:21; `pdns.service` Stopping 09:33:24.286 (unit journal JSON 09:33:24.289034Z), Stopped 09:33:24.295.
- **Kill.** Tagged Agent PID 3855 (start ticks 11265), state `T`, SIGKILL at 09:33:24.407217Z. Raw return −9, exit 137, reaped: `kill_proven: true`. Trigger exit 75.
- **Journal at the cut.** V2, phase `source-stopped`, sha256 `51480c6acefae23e47ce5937d955fdc716090889f3e6b439bba9bcdd9c863cd1`. The sampler copied the `target-staged` bytes (`f11b4d6b…`); the `source-stopped` write fell between samples, so only the controller's boundary record holds its hash.
- **Native state at the boundary** (09:33:24.35Z): `pdns.service` `loaded`/`inactive`/`dead`/`disabled`; `named.service` and `bind9.service` `masked`/`inactive`/`dead`/`masked`; no `named` process; no port-53 listener on the DNS address; the DNS query was refused (`[Errno 111] Connection refused`, 09:33:24.41Z).
- **Agent decides (step 2).** Ordinary Agent PID 6627. By 09:33:24.60Z it had written `rolling-back` (sha `7defa76e…e006`) and released the job with the same code and an empty active request. Ledger after release sha256 `87605649c3bca081b9b678b08fe711b9a76817348db7e5f7617121142d7887b9`, 2938 bytes. Refusal line for this request logged at `2026/09/29 09:33:24`.
  - **Native state at step 2** (09:33:24.54Z): identical to the boundary record (PowerDNS inactive/disabled, both BIND names masked and inactive, no listener, DNS refused at 09:33:24.59Z).
  - **Judged:** only `pdns_active_state: inactive` (BIND is recorded, not judged, for this cut). It holds.
- **Status (step 3).** Exit 0, no evidence change, names the command (2696 bytes, sha `6ca6bf0a…`). It contains the never-started guard-mask line seen in the pre-start cells:
  > `BIND never started for this operation: named.service and bind9.service are under the package guard's persistent mask (root-owned links to /dev/null) and the installed BIND vendor files were stable across two reads. The package stays installed as rollback standby. Process state, DNS answers and recovery authority are proved by the owner recovery command, not by this observation.`
  > `Native unit bind9.service: load=masked active=inactive unit-file=masked.`
  > `Native unit named.service: load=masked active=inactive unit-file=masked.`
  > `Native unit pdns.service: load=loaded active=inactive unit-file=disabled.`
- **Owner command.** Ran once from 09:33:25.13Z to 09:33:33.56Z (8.42 s) and exited **0**. Verbatim:
  > `The accepted BIND switch rollback reached its terminal verdict for request a86368918e6a7aa56a5677177c723e4a. Native PowerDNS was verified during recovery. Check current authoritative DNS health before another switch.`
  - Inside that window: `/etc/bind/named.conf.local` and `named.conf.options` rewritten 09:33:27.13–.14; `pdns.service` Starting 09:33:28.254, Started 09:33:28.354 (PID 7256), "Done launching threads" 09:33:28.510; journal `rolled-back` (sha `da747563…`) from 09:33:31.8 and absent from 09:33:33.57.
- **After the command (step 5).** Journal retired; ledger byte-identical to the release; state receipt byte-identical to the pre-cut source (`2b38b342…`); `owner_files_unchanged: true`.
  - **Native state** (09:33:33.72Z): `pdns.service` `active`/`running`/`enabled`, MainPID 7256, sole port-53 owner, authoritative UDP+TCP answers; `named.service` and `bind9.service` `masked`/`inactive`; no `named` process.
  - **PowerDNS MainPID:** `{pre_cut: 3074, after_owner_command: 7256, changed: true, judged: false}`; 7256 held through the re-run, the stability window and the final `systemctl show` (`NRestarts=0`).
- **Status after the owner command.** Exit 0, the same reconciled text as cell 1 for this request (sha `aebbe6d3…`); the repeat in `owner-post-state.txt` at 09:34:10Z has the same bytes.
- **Re-run.** 0.47 s, exit **3**, the "already reconciled … Nothing was changed now" text for this request (sha `59f153ed…`), no evidence change.
- **Probes.** Pre-owner `indeterminate` (`e93367dc…`). Probes 1 and 2 `rolled_back_source_active`, same fingerprint `6d0b3e5a…`; the post-collect probe agrees. Classification `rolled_back_source_serving`.
- **DNS outage** (`dns_outage`, `judged: false`): `source_stopped_at` 09:33:24.289034Z (first systemd stopping entry), `source_serving_again_at` 09:33:33.719654Z (first authoritative UDP+TCP answer observed after the owner command returned), **`seconds: 9.43062`**. This is an upper bound, not a guarantee: PowerDNS was started at 09:33:28.35 inside the command. No other DNS daemon ran in this cell; the unit journal shows `pdns.service` inactive from 09:33:24.295 to 09:33:28.354 (about 4.1 s), and the controller's queries at 09:33:24.41Z and 09:33:24.59Z were refused. There was no continuous DNS probing, so the actual answer gap is only bracketed, not measured.
- **Source data.** SOA serial `2026083101` over UDP and TCP to both addresses before the cut and at the end (09:34:16Z).
- **BIND after rollback** (09:34:10Z): both names `LoadState=masked`, `UnitFileState=masked`, `is-enabled` `masked`, `inactive`/`dead`, PIDs 0, `NRestarts=0`; `/etc/systemd/system/named.service` and `bind9.service` are root-owned symlinks to `/dev/null` from 09:33:14.02/13.74; no runtime masks; no `named` process; zero `named`/`bind9` journal entries. The same four `bind9*` packages are `ii` at `1:9.20.29-1~deb13u1`. `/etc/bind` equals the package defaults as in cell 1 (conffile MD5 match). `/var/cache/bind` holds only the staged generation `bd8a9194…`.
- **Panel-visible message** (09:34:10Z): the same reconciled text as cell 1 for this request, over the unchanged stored `failed`/`interrupted` job.
- **Health.** 31/31 samples from 09:33:34.62Z to 09:34:04.62Z. Agent PID 6627 unchanged. Post-collect UDP and TCP answers were `aa`, `192.0.2.10`.

## What remains on the guest after rollback (before teardown)

- **Both cells:** `bind9`, `bind9-utils` and `dns-root-data` installed; `bind9-host`/`bind9-libs` upgraded to `1:9.20.29-1~deb13u1`; the install-ownership receipt `/var/lib/celikpanel-agent-private/dns-engine-install-ownership-bind.json`; one staged generation tree under `/var/cache/bind/celikpanel/generations/`; `/etc/bind/rndc.key` (not package-owned). PowerDNS state and ownership receipts are unchanged.
- **Cell 1 (`target-started`) only:** the BIND unit is left **unmasked and disabled** (`named.service` `loaded`/`disabled`, `bind9.service` alias gone), not under the guard mask. `named`'s own `managed-keys.bind.jnl` and a zero-length `tmp-1K50Kr2S9C` remain in `/var/cache/bind`.
- **Cell 2 (`source-stopped`) only:** both persistent guard masks remain.

The guests were then stopped and torn down (`stop.json`, `teardown.json` in each cell directory).

## Deviations

- **A stray, argument-less `collect.sh` invocation.** While reading cell 2's results I ran one command that also started the collection script with no arguments, in the background, and killed it. The script runs under `set -u` and aborts on its first line (`N=$1`) before creating a directory or contacting the guest. A check afterwards found no file under the work root newer than cell 2's automatic collection (09:34:22Z), one collection in `cell2/collect.log`, and no running `collect.sh`. It had no effect on the evidence.
- **Post-owner facts come from after the controller finished.** `owner-post-state.txt`, the Panel-visible reply, `bind-unit-diagnostics-post-collect.txt` and `soa-post-collect.txt` were taken after the controller's run ended. The immediate post-command state is the controller's `steps.after_owner_command` and `steps.status_after_owner_command`.
- **`raw/results/` is flattened and the cell directories have short names.** See "Evidence layout".
- Each cell ran once. There was no re-run of any cell and no setup mistake requiring one.

## What this does not prove

Two cells passed on one path. This run does not establish:

- **Coverage.** One path, Debian 13 only; Arch is not exercised. The recovery kit is an unsigned local build enrolled as fixture work, not release provenance.
- **Untested conditions.** No reboot, no power loss, no owner-edit race. No before-write edge and no `rolled-back` cell: they have no V2 pass definition. No paired topology and no installed server.
- **DNS continuity.** DNS is not continuous in these cells by construction: the product stops PowerDNS before the cut. The `dns_outage.seconds` values (21.57 s and 9.43 s) are controller-observed upper bounds on the PowerDNS source outage for this run on this host, measured from the unit journal's stop entry to the first answer seen after the command returned. They are not a guarantee, are not a service-level bound, and do not measure the answer gap: in `target-started` BIND answered for part of that window, and no continuous DNS probe ran in either cell. The outage lasts until the owner runs the command, so it depends on when the owner acts.
- **Re-run exit 3 is a named gap.** The re-run of a successful inverse exits 3 while correctly reporting "already reconciled … Nothing was changed now". A caller that reads only the exit code sees a failure.
- **The ledger cannot distinguish rollback from release.** It is byte-identical in both cases; the controller's timeline (checkpoint and retirement inside the command's window) and the product text carry that evidence.
- **Different end states for BIND.** `target-started` ends unmasked and disabled; `source-stopped` ends masked. The flow records this, it does not judge it. Whether an unmasked, disabled BIND unit with the package installed is the intended rollback standby is not decided here.
- **State left behind.** The generation tree, the installed and upgraded BIND packages, the install-ownership receipt, and in cell 1 `named`'s own files and a zero-length temporary file remain after rollback.
- **Code-level limits** recorded in `docs/DNS-ENGINE-ARTIFACT.md` (a target that an owner started and masked again would also be admitted; listener proofs ignore loopback and link-local sockets) are not exercised. In cell 1 `named` also listened on `127.0.0.1`, `::1` and link-local IPv6 while running; those sockets are outside the controller's listener check.
- **Version drift.** Packages came from the live Debian mirror (`bind9` `1:9.20.29-1~deb13u1`, `pdns-server` `4.9.17-0+deb13u1`); a later run may resolve different versions.

Nothing here changes the 268-runnable denominator, closes P0.4, or passes any acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file except this README and itself.
