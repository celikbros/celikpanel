# Owner inverse after Agent restart: re-run on the fixed source, 2026-09-29

Scope: P0.4, D-025 invariants 1, 2, 4 and 5, D-024. This repeats the two cells of the [first run](../owner-inverse-after-restart-20260929/README.md) (commit `7ad24282`, both failed at the owner command) on commit `411398d9`. That commit contains the change described in the last section of `docs/DNS-ENGINE-ARTIFACT.md` ("V2 BIND switch inverse accepts a never-started, guard-sealed target"). The flow, fixture, image, toolchain, build flags and evidence layout are the same; only paths, the commit and the additions listed below differ. The run used fresh disposable QEMU guests on the local WSL `archlinux` host. It did not touch an installed panel, a remote host, a release or a signed bundle. **Result: 2 passed** (`result.json` `status: passed`, `safety_status: passed`, `recovery_outcome.classification: rolled_back_source_serving`, in both cells).

In both cells the Agent's decision (step 2), the read-only status naming (step 3), the owner command (step 4), the post-command judgement (step 5), the post-command status, the re-run and the 30 s stability window all held. The owner command exited 0:

```
The accepted BIND switch rollback reached its terminal verdict for request <id>. Native PowerDNS was verified during recovery. Check current authoritative DNS health before another switch.
```

It wrote the `rolled-back` checkpoint and retired the journal. The ledger stayed byte-identical to the Agent's release. PowerDNS kept serving with the same MainPID, and no owner file changed. `named.service` and `bind9.service` stayed under the package guard's persistent mask and never started.

## Build and fixture

- Tested source: commit `411398d9aecb70cbe3bf50990ba5814bdee17c07` (`feat/dns-artifact-separation`), extracted with `git archive 411398d9` into guest ext4 (`/root/cp-oi2-src`). The repository HEAD at run time was `b05f61df0a34745d8f8dcbaa6dec01181e12f8e0`. That commit only adds previously ignored evidence `*.log` files; no product or harness code differs from `411398d9`. The harness (`fixture.py`, `guest_bootstrap.py`, `run_cell.py`, `guest_recovery_probe.py`) ran from the `411398d9` tree.
- Toolchain: `/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go`, `go1.26.5 linux/amd64`, `GOTOOLCHAIN=local CGO_ENABLED=0 -trimpath -buildvcs=false`. See [build/build.log](build/build.log) and [build/artifacts.sha256](build/artifacts.sha256).

| Binary | Build | SHA-256 | vs `7ad24282` run |
| --- | --- | --- | --- |
| `agent` | `./cmd/agent` | `4f8c628646b7efa2c143102f845b9e13b02d7c36a85e9a93d1e9f049e1bfa5b7` | changed |
| `agent.kill` | `./cmd/agent`, `-tags dns_kill_matrix` | `f1074078381976313e0833dcce7dd8b751e5a4b5e039f1388260792d1ee5c8ba` | changed |
| `panel` | `./cmd/panel` | `793fb81b95fa96f438a216831a146e0869934ce31318881857d3352ffb92e472` | identical |
| `dns-kill-trigger` | `./cmd/dns-kill-matrix-trigger` | `97aa0ce32546e9ba6ae56831e9f300b1f97133893449422811937e365bdfc274` | identical |
| `recovery` | `./cmd/recovery` | `5268ed0058dc8d7619df2cbeb16661ef71eab8cf75a488746194c4104e92dde6` | changed |
| `agent-checker` | `deploy/recovery/agent-checker.sources` | `82f2772fe0257dde9139d0f85a34be58e89c98d6a9d1775090f2ecf4cc3b8975` | identical |
| `panel-checker` | `deploy/recovery/panel-checker.sources` | `82f23b48908e152e55ad76598ec8be0f780805f463fe7e48ed402d87bc5984e0` | identical |
| `schema17-bridge` | `./deploy/schema17bridge` | `fe277c46ea8b50f00a80e71976198f28db89667789a737e0a78c0f4a83e9cb87` | identical |
| recovery runtime `runtime.manifest` | `go run ./deploy/recovery/bundle` | `a682b35afd664df01436b6dfb25040afda352dff7e4557586665e90a93245646` | changed (was `d87d93e8…`) |

- The bundle's printed digest equals the `runtime.manifest` hash. The runtime's embedded `bin/*` are the four binaries above, and its scripts are byte-identical to the previous run's.
- `web/dist` is untracked. It was copied from the local Windows checkout, and its 103 file hashes ([build/web-dist.sha256](build/web-dist.sha256)) are byte-identical to the previous run's list, for example `index.html` `9808280612b052b1346f04281a7931acdd50c980d355e30640ff147ebd28e8e5`.
- Work root `/var/tmp/cp-oi2-0929`. The locked base images were hard-linked from `/var/tmp/cp-v3n28/images` and passed `fixture.py verify-images`: Debian 13 `20260826-2582` and Arch `20260815.573966` ([build/work-root-setup.log](build/work-root-setup.log)). The guest manifest was byte-identical to the tracked `manifest.json` (`dcf6e09c…d1229`) and is omitted from the cell directories.
- Each cell ran `prepare` → `start` → `wait-ssh` → `guest_bootstrap.py install` → `enroll-recovery-runtime --execute` → `prepare-bind --source-fixture managed-pdns` → `run-prepared --owner-inverse-after-restart --execute` → collect → `fixture.py stop` and `teardown`. Both enrollments installed launcher `5268ed00…` with runtime manifest `a682b35a…` ([enroll-recovery-runtime.log](bind__target-staged__after-write__standalone__peer-reachable/enroll-recovery-runtime.log)). The kit tarball hash differs per enrollment (`289f4ffc…`, `e65ebd87…`), because the tar is rebuilt each time. The controller's preflight `recovery check-bind-source-inverse-v1` returned 0.
- Both cells used scenario SHA-256 `3bc1460074a57105ad1bb1ecdc56cec8ddbd6e37531443da0a7c18e2ce11dc26`, the same as the previous run.
- The managed PowerDNS source was established exactly as in the previous run: fixture preinstall of `pdns-server`/`pdns-backend-sqlite3` `4.9.17-0+deb13u1`, then the product's `pdns-adopt` RPC and `rpc-normalize-pdns` ([prepare-source.log](bind__target-staged__after-write__standalone__peer-reachable/prepare-source.log)). Before the measured operation, `pdns.service` was `loaded`/`active`/`enabled`, `named.service` and `bind9.service` were `not-found`, no `bind9` package was installed, and UDP and TCP queries for `www.s1-kill.test A` to `10.0.2.15` and `192.0.2.10` returned `aa`, `NOERROR`, `192.0.2.10`.

### Evidence layout

The layout follows the previous run, with these changes:

- **`raw/results/` is flattened.** The controller writes `results/<cell>/`. Here those four files sit directly in `raw/results/`, which keeps every path under about 230 characters on Windows. The previous run's deepest paths reach 260 characters.
- **`owner-post-state.txt` is new.** It is a read-only capture, made after the controller finished, that is, after the owner command, the re-run and the 30 s stability window. It contains:
  - `systemctl show`, `is-enabled` and `is-active` for `named.service` and `bind9.service`, and the persistent and runtime mask symlinks;
  - `named` processes and journal entry counts;
  - `dpkg -l 'bind9*'` and `dpkg --verify`;
  - every file under `/var/cache/bind` and `/etc/bind`, with mode, owner, size, mtime, SHA-256, package ownership and the `bind9` conffile MD5s;
  - the apt history;
  - port-53 listeners;
  - one further `recovery dns-switch-status --quiesced --request-id <id>` run (as root, `env -i PATH=… LANG=C.UTF-8`, cwd `/`) with its exit code;
  - the private evidence hashes after it.
  The script is in `build/fixture-tools/ownerpost_remote.sh`.
- **`service-mutation-status-post-collect.json`** holds one authenticated read-only `Agent.ServiceMutationStatus` call, made with the same reader source as before (`smstatus.go` `57823885…`). Only the overlay path changed. The reader binary SHA-256 `ff9abfa1…7890` is byte-identical to the previous run's.
- **`bind-unit-diagnostics-post-collect.txt`**, `raw/watch/copies/` and the other files are as in the previous run.

## Cell 1: `bind__target-staged__after-write__standalone__peer-reachable` — PASSED

- **Comparison with `7ad24282`: FAILED → PASSED.** The previous run's owner command exited 3 with `DNS target is not a loaded unit` and kept the journal. Here it exited 0, and the journal was retired.
- Debian 13, request `b66cbdc5c51b25639364f76a7113a214` (the same ID as before). The controller ran from 08:25:31.33Z to 08:26:27.37Z (56 s). The whole cell, from `prepare` to `teardown`, took 08:23:47Z–08:28:30Z.
- **Kill.** Tagged Agent PID 3860 (start ticks 9958) was in state `T` and received SIGKILL at 08:25:44.702550Z. Raw return −9, exit 137, and the `/proc` entry was absent after reap: `kill_proven: true`. The trigger exited 75.
- **Journal at the cut.** V2, phase `target-staged`, sha256 `035f4e5e58e8b3cd7268ee251180b02d9991b322a77953f27153a1069b2985c0`, byte-identical to the previous run's. `target_units_before` records both units as `not-found`.
  - The product had installed `bind9` between 08:25:36 and 08:25:39. That install upgraded `bind9-host` and `bind9-libs` from `1:9.20.26-1~deb13u1` to `1:9.20.29-1~deb13u1`.
  - It wrote the install-ownership receipt at 08:25:33.97 and masked both units at 08:25:34.05/.40.
  - The sampler copied the `intent` bytes (`035aeeec…`, 08:25:44.60) and then `rolling-back`. The `target-staged` write fell between two samples, so only the controller's boundary record holds its hash.
- **Agent decides.** The ordinary Agent was PID 6613 (started 08:25:44). By 08:25:44.93Z it had rewritten the journal to `rolling-back` (sha `f1692d5f…499f`, identical to the previous run) and released the job: `failed`/`interrupted`, `dns_native_recovery_unknown_after_restart`, empty active request. Ledger after release: sha256 `f9532d34fd48bd939483eadcae554da116001f29f9e7c78b19f9dac2c1ebceab`, 2938 bytes. The refusal line is the same text as in the previous run, logged at `2026/09/29 08:25:44`.
- **Status (step 3).** It exited **0** (the previous run exited 3), changed no evidence, and named the command. Compared with the previous run, the unknown-identity line is gone and is replaced by:
  > `BIND never started for this operation: named.service and bind9.service are under the package guard's persistent mask (root-owned links to /dev/null) and the installed BIND vendor files were stable across two reads. The package stays installed as rollback standby. Process state, DNS answers and recovery authority are proved by the owner recovery command, not by this observation.`
  > `Native unit bind9.service: load=masked active=inactive unit-file=masked.`
  > `Native unit named.service: load=masked active=inactive unit-file=masked.`

  The full text (2693 bytes, sha `b5d19102…`) is in `result.json` `steps.status`.
- **Owner command.** It ran once as root from 08:25:45.74Z to 08:25:56.30Z (10.55 s) and exited **0**. Verbatim:
  > `The accepted BIND switch rollback reached its terminal verdict for request b66cbdc5c51b25639364f76a7113a214. Native PowerDNS was verified during recovery. Check current authoritative DNS health before another switch.`
- **After the command.**
  - The sampler saw the journal at `rolled-back` (sha `38984975…`) at 08:25:54.69 and absent from 08:25:56.55.
  - `/etc/bind/named.conf.local` and `named.conf.options` were rewritten at 08:25:49.65–.66, inside the command's window. Their SHA-256 values now equal the journal's `config_before` (`63963de7…`, `f1c37414…`), and their MD5s equal the `bind9` package conffile MD5s.
  - The ledger was byte-identical to the release (`f9532d34…`, `ledger_unchanged_since_release: true`).
  - The state receipt was byte-identical to the pre-cut source (`0d59887a…`).
  - `owner_files_unchanged: true`: PowerDNS main and managed config, and the database identity with `quick_check ok` and domain count 1.
- **Status after the owner command** (08:25:56.47Z, exit 0, no evidence change; `owner-post-state.txt` repeated it at 08:27:50Z with the same bytes, sha `5d3ea34d…`):
  > `DNS switch request b66cbdc5c51b25639364f76a7113a214 was interrupted and the Agent could not recover it automatically when it restarted, so it released the lease. The switch has since been reconciled and its journal is retired, either by the owner recovery command or by a later Agent start; this evidence cannot tell which. This request no longer blocks DNS changes. The current DNS engine and its health are shown by the panel's DNS engine status, not by this record. This status check started nothing.`
- **Re-run.** It took 0.42 s, exited **3**, and changed no evidence. Verbatim:
  > `Request b66cbdc5c51b25639364f76a7113a214 is already reconciled: the Agent released this interrupted DNS switch and its journal has since been retired, by this command or by a later Agent start; the evidence cannot tell which. Nothing was changed now. This request no longer blocks DNS changes. Before another switch, check the current DNS engine and authoritative DNS answers in the panel.`
- **Probes.** The pre-owner probe was `indeterminate` (fingerprint `8e8ef55b…`, the same as before). After the command, both probes were `rolled_back_source_active` with the same fingerprint `46ef1847…`. The post-collect probe agrees. `recovery_outcome.classification: rolled_back_source_serving`. No failures of any kind, and no ambiguities.
- **PowerDNS.** MainPID 3072 (started 08:25:21) before the cut, at the boundary, after the restart, after the owner command, after the re-run, after the stability window and in the final `systemctl show`. `NRestarts=0`, `enabled`. The `pdns.service` journal has no entries after 08:25:22.
- **BIND after the owner command** (`owner-post-state.txt`, 08:27:50Z):
  - Both units: `LoadState=masked`, `UnitFileState=masked`, `is-enabled` `masked`, `inactive`/`dead`, MainPID and ControlPID 0, `NRestarts=0`, no `InvocationID`.
  - `/etc/systemd/system/named.service` and `bind9.service` are root-owned symlinks to `/dev/null` from 08:25:34. No runtime masks exist.
  - There is no `named` process, and there are zero named/bind9 journal entries.
  - `dpkg -l`: `bind9`, `bind9-utils`, `bind9-host`, `bind9-libs` are all `ii` at `1:9.20.29-1~deb13u1`. `dpkg --verify` printed only an error that `bind9-dnsutils` is not installed (exit 1). This output does not establish whether it verified the other packages.
- **Panel-visible message** (`Agent.ServiceMutationStatus` at 08:27:51Z). The stored job is unchanged (`failed`/`interrupted`, `dns_native_recovery_unknown_after_restart`). The message is the reconciled text computed at read time: "An interrupted DNS switch (request b66cbdc5c51b25639364f76a7113a214) could not be recovered automatically when the Agent restarted and was reconciled later. Its journal is retired and it no longer blocks DNS changes. Check the current DNS engine status before another switch."
- **Health.** Agent PID 6613 was unchanged after the Panel restart and at the end. 31/31 samples (Agent, Panel, UDP+TCP DNS) were healthy from 08:25:57.32Z to 08:26:27.32Z. Post-collect queries to `10.0.2.15` and `192.0.2.10` returned `aa`, `192.0.2.10` over UDP and TCP.

## Cell 2: `bind__intent__after-write__standalone__peer-reachable` — PASSED

- **Comparison with `7ad24282`: FAILED → PASSED.** The previous run's owner command exited 3 with `DNS target is not a loaded unit` and kept the journal. Here it exited 0, and the journal was retired.
- Debian 13, request `8f027a47437e196b05cf295aff339d93` (the same ID as before). The controller ran from 08:30:23.51Z to 08:31:13.96Z (50 s). The whole cell took 08:28:36Z–08:31:54Z.
- **Kill.** Tagged Agent PID 3864 (start ticks 10295) was in state `T` and received SIGKILL at 08:30:35.587476Z. Raw return −9, exit 137, reaped: `kill_proven: true`. The trigger exited 75.
- **Journal at the cut.** V2, phase `intent`, sha256 `08b8e650323e39140e40a9a74ae6c4a4425f479246a376bb10124fe2848b7f0f`. The previous run's was `58111a61…`, so the intent bytes are not identical across runs. `target_units_before` records both units as `not-found`.
  - The product had already installed `bind9` (08:30:27–08:30:31, with the same `bind9-host`/`bind9-libs` upgrade).
  - It had written the install-ownership receipt at 08:30:25.61 and masked both units at 08:30:25.69/.99.
  - It had staged the BIND generation tree at 08:30:33.
  - The journal first appeared at 08:30:35.59.
- **Agent decides.** The ordinary Agent was PID 6513. By 08:30:35.99Z it had written the journal at `rolling-back` (sha `fa365c79…`) and released the job, with the same code and an empty active request. Ledger after release: sha256 `fdf2a4475a4018ea6a78c4143f99db633e2706a48a033d82ba2fdb2fd0fb936b`, 2940 bytes. The refusal line is the same text for this request, logged at `2026/09/29 08:30:35`.
- **Status (step 3).** It exited 0, changed no evidence, and named the command. The text matches cell 1 for this request, including the never-started guard-mask line (sha `a53d9d6b…`).
- **Owner command.** It ran from 08:30:36.26Z to 08:30:42.86Z (6.61 s) and exited **0** with the same text as cell 1 for this request. The journal reached `rolled-back` (sha `2ad033c7…`) at 08:30:40.79 and was absent from 08:30:42.83.
- **After the command.**
  - `/etc/bind/named.conf.local` and `named.conf.options` were never touched at this cut: they keep the package mtime `2026-09-16T13:42:29`, their hashes equal `config_before`, and their MD5s equal the conffile MD5s.
  - The ledger was byte-identical to the release (`fdf2a447…`), and the state receipt was byte-identical to the source (`c9b20509…`).
  - `owner_files_unchanged: true`.
- **Status after the owner command.** It exited 0 with the same reconciled text as cell 1 for this request (sha `9c5f9a6a…`). The repeat in `owner-post-state.txt` at 08:31:19Z has the same bytes.
- **Re-run.** It took 0.47 s, exited **3** with the "already reconciled … Nothing was changed now" text for this request (sha `63f36e51…`), and changed no evidence.
- **Probes.** The pre-owner probe was `indeterminate` (fingerprint `1272180d…`, the same as before). Both post-command probes were `rolled_back_source_active` with fingerprint `89f52cba…`. Classification `rolled_back_source_serving`. No failures, no ambiguities.
- **PowerDNS.** MainPID 3076 (started 08:30:13) throughout, `NRestarts=0`, `enabled`.
- **BIND after the owner command** (08:31:19Z). The facts match cell 1: both units masked, `is-enabled` `masked`, `inactive`/`dead`, PIDs 0, `NRestarts=0`; the `/dev/null` symlinks date from 08:30:25; no runtime mask, no `named` process, no journal entries. The same four `bind9*` packages are `ii` at `1:9.20.29-1~deb13u1`.
- **Panel-visible message** (08:31:19Z). It is the same reconciled text as cell 1 for this request, over the unchanged stored `failed`/`interrupted` job.
- **Health.** 31/31 from 08:30:43.91Z to 08:31:13.91Z. Agent PID 6513 was unchanged. Post-collect UDP and TCP answers were `aa`, `192.0.2.10`.

## What remains on the guest after rollback (both cells, before teardown)

Per `owner-post-state.txt`:

- **`bind9` package set.** `bind9` stays installed (with `bind9-utils` and `dns-root-data`), as the documented rollback standby. `bind9-host` and `bind9-libs` stay upgraded to `1:9.20.29-1~deb13u1`.
- **Guard masks.** Both persistent masks stay.
- **Install-ownership receipt.** `/var/lib/celikpanel-agent-private/dns-engine-install-ownership-bind.json` stays. The PowerDNS state and ownership receipts are unchanged.
- **Staged BIND generation tree.** One tree stays under `/var/cache/bind/celikpanel/generations/<id>/`: `receipt.json`, `zones.conf` and one zone file (cell 1 `ae4516bb…`, cell 2 `091c8f46…`).
- **`/etc/bind/rndc.key`.** It was created by the package, is not owned by a package, and stays.
- **`/etc/bind` configuration.** The `/etc/bind` configuration files equal the package defaults.

The guests were then stopped and torn down (`stop.json`, `teardown.json` in each cell directory).

## Deviations

- **Collection for cell 1 was started by hand.** The collection script was edited on Windows after it was copied, which gave it CRLF line endings. Cell 1's automatic collect step then aborted before running anything on the guest; it only created three empty directories named `/var/tmp/cp-oi2-0929\r/…` on the host. After finding this, I did three things:
  - removed those empty directories with `rmdir` (this run had created them);
  - normalized the script's line endings;
  - ran the collection by hand at 08:27:48Z, 81 s after `run-prepared` ended and before teardown.

  This was a harness setup mistake in the collector only. The cell itself ran once and was not re-run. Cell 2 collected automatically.
- **Post-owner facts come from after the controller finished.** `owner-post-state.txt`, the Panel-visible reply and `bind-unit-diagnostics-post-collect.txt` were taken after the controller's run ended, not immediately after the owner command. The immediate post-command status is the controller's `steps.status_after_owner_command`.
- **`raw/results/` is flattened.** See "Evidence layout" above.

## What this does not prove

Two cells passed on one path. This run does not establish:

- **Coverage.** Only one path ran, only on Debian 13. Arch is not exercised. The recovery kit is an unsigned local build enrolled as fixture work, not release provenance.
- **Untested conditions.** There was no reboot, no power loss and no owner-edit race. There was no `source-stopped` or `target-started` cut, so there was no cut where BIND had started or PowerDNS was stopped. There was no paired topology and no installed server.
- **Re-run exit 3 is a named gap.** The re-run of a successful inverse exits 3 even though it correctly reports "already reconciled … Nothing was changed now". A caller that reads only the exit code sees this as a failure.
- **The ledger cannot distinguish rollback from release.** The ledger alone cannot tell an owner rollback from the Agent's release; it is byte-identical in both cases. Here the controller's timeline (checkpoint and retirement inside the command's window) shows the owner command retired the journal. The product text itself says the evidence cannot tell which.
- **State left behind.** The staged immutable BIND generation tree and the upgraded `bind9-host`/`bind9-libs` libraries remain on disk after rollback, as do the installed `bind9` package, the guard masks and the install-ownership receipt.
- **Code-level limits.** The limits recorded in `docs/DNS-ENGINE-ARTIFACT.md` are not exercised here:
  - a target that an owner started and masked again would also be admitted;
  - status for a V2 journal that has not yet reached `rolling-back` still reports a masked target as unknown;
  - listener proofs ignore loopback and link-local sockets.
- **Version drift.** Packages came from the live Debian mirror (`bind9` `1:9.20.29-1~deb13u1`, `pdns-server` `4.9.17-0+deb13u1`). A later run may resolve different versions.

Each cell ran once. Nothing here changes the 268-runnable denominator, closes P0.4, or passes any acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file except this README and itself.
