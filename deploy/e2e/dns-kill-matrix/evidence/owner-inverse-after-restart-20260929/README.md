# Owner inverse after Agent restart: first native run, 2026-09-29

Scope: P0.4, D-025 invariants 1, 2, 4 and 5, D-024. The run used the `--owner-inverse-after-restart` flow from `deploy/e2e/dns-kill-matrix/README.md` ("Owner inverse after Agent restart") on commit `7ad24282`. That commit contains the Go change described in the last section of `docs/DNS-ENGINE-ARTIFACT.md` ("Owner inverse admits the Agent's deliberate release"). Two standalone Debian 13 PowerDNS-to-BIND cells ran with a managed PowerDNS source, in fresh disposable QEMU guests on the local WSL `archlinux` host. The run did not touch an installed panel, a remote host, a release or a signed bundle. **Result: 2 failed** (`result.json` `status: failed`, `safety_status: passed`, in both cells).

Both cells failed at the same step, step 5. The Agent's decision (step 2) and the read-only status naming (step 3) both held. The owner command `recover-dns-bind-switch` was admitted past the released-ledger check but refused at native assessment:

```
The accepted BIND switch rollback could not be verified. Inspect recovery dns-switch-status --quiesced --request-id <id>; resolve the reported evidence, worker, lock or native DNS condition and retry this same request. Preserve the journal and ledger. Reason: BIND switch native preimage is unknown or owner-modified
DNS target is not a loaded unit
```

It exited 3 and left the journal at `rolling-back`. At both cuts the product had already installed `bind9` under the package guard's persistent mask: `named.service` and `bind9.service` were `LoadState=masked`, `UnitFileState=masked`, with symlinks to `/dev/null` in `/etc/systemd/system`. The independent owner-side stopped-target proof demands `LoadState=loaded`, and the status command could not parse the masked unit's identity. This is the same class of defect as the PowerDNS first-install rollback fixed in `1c336f6d`, now on the V2 BIND owner inverse. That diagnosis is a reading of the code, not a verified root cause (see "Refusal and likely cause"). PowerDNS kept serving with the same MainPID throughout, and no owner file changed.

## Build and fixture

- Commit `7ad242828eb03242e52dd5d5e10c7b4627fe6a14` (`feat/dns-artifact-separation`) was extracted with `git archive` into guest ext4 (`/root/cp-oi-src`). The harness (`fixture.py`, `guest_bootstrap.py`, `run_cell.py`, `guest_recovery_probe.py`) ran from that tree.
- Toolchain: `/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go`, `go1.26.5 linux/amd64`, `GOTOOLCHAIN=local CGO_ENABLED=0 -trimpath -buildvcs=false`. See [build/build.log](build/build.log) and [build/artifacts.sha256](build/artifacts.sha256).

| Binary | Build | SHA-256 |
| --- | --- | --- |
| `agent` | `./cmd/agent` | `8dd390ce9b7b725a8cacd70da262805d9d160203a78623ce6f40fd3fe74fc15e` |
| `agent.kill` | `./cmd/agent`, `-tags dns_kill_matrix` | `e4c05c3afaaaca228bf55bdd67c2ab46946ff9fd3238961d8324dd8d647b4474` |
| `panel` | `./cmd/panel` | `793fb81b95fa96f438a216831a146e0869934ce31318881857d3352ffb92e472` |
| `dns-kill-trigger` | `./cmd/dns-kill-matrix-trigger` | `97aa0ce32546e9ba6ae56831e9f300b1f97133893449422811937e365bdfc274` |
| `recovery` | `./cmd/recovery` | `15dbd98bbe7ce54d030b4bc2e5c26fc40a6351639a57373089edbe4976409dd9` |
| `agent-checker` | `deploy/recovery/agent-checker.sources` | `82f2772fe0257dde9139d0f85a34be58e89c98d6a9d1775090f2ecf4cc3b8975` |
| `panel-checker` | `deploy/recovery/panel-checker.sources` | `82f23b48908e152e55ad76598ec8be0f780805f463fe7e48ed402d87bc5984e0` |
| `schema17-bridge` | `./deploy/schema17bridge` | `fe277c46ea8b50f00a80e71976198f28db89667789a737e0a78c0f4a83e9cb87` |
| recovery runtime `runtime.manifest` | `go run ./deploy/recovery/bundle` | `d87d93e8e6427daafc399e5d8e5c21ec1db5adb414698e872b86a46e22fd3bd6` |

- `panel` and `dns-kill-trigger` are byte-identical to the `1c336f6d` run. The bundle's printed digest equals the `runtime.manifest` hash. The runtime's embedded `bin/*` are the four binaries above.
- `web/dist` is untracked. It was copied from the local Windows checkout, and its 103 file hashes ([build/web-dist.sha256](build/web-dist.sha256)) are identical to the previous two runs', for example `index.html` `9808280612b052b1346f04281a7931acdd50c980d355e30640ff147ebd28e8e5`.
- Work root `/var/tmp/cp-oi-0929`. The locked base images were hard-linked from `/var/tmp/cp-v3n28/images` and passed `fixture.py verify-images`: Debian 13 `20260826-2582` and Arch `20260815.573966` ([build/work-root-setup.log](build/work-root-setup.log)). The guest manifest was byte-identical to the tracked `manifest.json` (`dcf6e09c…d1229`) and is omitted from the cell directories.
- Each cell ran `prepare` → `start` → `wait-ssh` → `guest_bootstrap.py install` → `enroll-recovery-runtime --recovery-runtime <art>/recovery-runtime --execute` → `prepare-bind --source-fixture managed-pdns` → `run-prepared --owner-inverse-after-restart --execute` → collect → `fixture.py stop`. Both enrollments installed launcher `15dbd98b…` with runtime manifest `d87d93e8…` ([enroll-recovery-runtime.log](bind__target-staged__after-write__standalone__peer-reachable/enroll-recovery-runtime.log)). The kit tarball hash differs per enrollment, because the tar is rebuilt each time. The controller's preflight `recovery check-bind-source-inverse-v1` returned 0.
- Both cells used scenario SHA-256 `3bc1460074a57105ad1bb1ecdc56cec8ddbd6e37531443da0a7c18e2ce11dc26`.

### How the managed PowerDNS source was established

See `prepare-source.log` in each cell. `prepare-bind --source-fixture managed-pdns` works in stages:

1. **Fixture preinstall.** The fixture preinstalled `pdns-server` and `pdns-backend-sqlite3` `4.9.17-0+deb13u1` from the live `deb.debian.org` trixie mirror, under a temporary mask (`source-preinstall-pdns.json`). It then wrote an owner-style PowerDNS configuration and SQLite zone.
2. **Adoption RPC.** The production Agent adopted that source through a real `pdns-adopt` `rpc-switch` (`source-adoption-pdns.json`, `source-setup-trigger-identity.json`).
3. **Normalization.** The production Agent then normalized it with `rpc-normalize-pdns` (`source-normalization-pdns-identity.json`).

So the packages were installed by the fixture, and adoption and normalization were done by the product.

Before the measured operation, the state was:

- `pdns.service` was `loaded`/`active`/`enabled`, `NRestarts=0`.
- `named.service` and `bind9.service` were `not-found`, and no `bind9` package was installed ([pre-run-units.txt](bind__target-staged__after-write__standalone__peer-reachable/pre-run-units.txt)).
- UDP and TCP queries for `www.s1-kill.test A` to `10.0.2.15` and `192.0.2.10` returned `aa`, `NOERROR`, `192.0.2.10` ([dns-pre-run.txt](bind__target-staged__after-write__standalone__peer-reachable/dns-pre-run.txt)).
- The controller's preflight saw PowerDNS as the only port-53 owner, with authoritative UDP and TCP answers.

### Evidence layout

The layout follows the previous runs, with these additions:

- `raw/watch/copies/` holds a byte copy of the switch journal, the mutation ledger and the DNS state receipt each time the 0.2 s read-only sampler saw their SHA-256 change. The file name is the UTC time and a hash prefix.
- `service-mutation-status-post-collect.json` holds one authenticated read-only `Agent.ServiceMutationStatus` call for the request, made after the controller finished. That is the Panel-visible job.
- `bind-unit-diagnostics-post-collect.txt` holds read-only `systemctl show`/`status` output, unit-file links, `dpkg -S` and the apt history, taken after collect.
- `build/fixture-tools/` holds the sampler, the DNS query script, the unit-facts and diagnostics scripts, and the source and overlay of the status reader. The reader was built with `go build -overlay` against the same tree without modifying it; its binary SHA-256 is in `oi-smstatus.sha256`.

`result.json` carries every `steps.*` record: `agent_restarted`, `status`, `owner_command`, `after_owner_command`, `status_after_owner_command`, `rerun` and `after_stability`.

## Cell 1: `bind__target-staged__after-write__standalone__peer-reachable` — FAILED

- Debian 13, request `b66cbdc5c51b25639364f76a7113a214`. The run went from 07:15:51Z to 07:16:37Z.
- **Kill.** Tagged Agent PID 3860 (start ticks 9940) was in state `T` and received SIGKILL at 07:16:04.128751Z. Raw return −9, exit 137, and the `/proc` entry was absent after reap: `kill_proven: true`. The trigger exited 75.
- **Journal at the cut.** `celikpanel-dns-engine-switch-journal/v2`, phase `target-staged`, sha256 `035f4e5e58e8b3cd7268ee251180b02d9991b322a77953f27153a1069b2985c0`. The frozen source is `inverse_plan.source_pdns` `pdns-source/v1`, database `/var/lib/powerdns/pdns.sqlite3` with logical sha `2a4ac535…0b82`, and `config_before` listing `/etc/powerdns/pdns.conf`, `pdns.d/celikpanel-cluster.conf` and `pdns.d/celikpanel.conf`. `target_units_before` records `bind9.service` and `named.service` as `not-found`. The product had installed `bind9` between 07:15:55 and 07:15:59, which also upgraded the preinstalled `bind9-host` and `bind9-libs` from `1:9.20.26-1~deb13u1` to `1:9.20.29-1~deb13u1`. The install-ownership receipt `dns-engine-install-ownership-bind.json` was written at 07:15:53. At the cut, both units were masked.
- **Agent decides.** The ordinary Agent was PID 6595 and started at 07:16:04. At 07:16:04.61Z it rewrote the journal to `rolling-back` (sha `f1692d5f…499f`) and released the lease. The job was `failed`/`interrupted`, `error_code: dns_native_recovery_unknown_after_restart`, with an empty active request. Ledger after release: sha256 `e43e69a2fb4cfcdb3e53138bfa6ac4c9a70cf100515953cc0eb1167792eef62b`, 2940 bytes. Verbatim refusal line:
  > `2026/09/29 07:16:04 Interrupted DNS switch native recovery is unknown; retain exact journal and release only its ledger lease (request b66cbdc5c51b25639364f76a7113a214): v2 BIND switch journal requires its independent inverse adapter; the owner recovery command for this PowerDNS-to-BIND rollback is /usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id b66cbdc5c51b25639364f76a7113a214, which the server owner runs when recovery dns-switch-status --quiesced --request-id b66cbdc5c51b25639364f76a7113a214 names it; preserve the journal, ledger and native DNS until that exact request is reconciled`
- **Status.** The status command exited 3, changed no evidence, and named the command (`names_owner_command: true`). Verbatim:
  > `DNS switch request b66cbdc5c51b25639364f76a7113a214: released-undecided-with-journal (journal phase rolling-back).`
  > `This PowerDNS-to-BIND switch retains a rollback decision. The server owner can continue that exact inverse with: /usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id b66cbdc5c51b25639364f76a7113a214. The command rechecks locks, worker exclusion, the frozen PowerDNS source and owner changes; this status check does not start recovery.`
  > `The Agent restarted, could not complete this DNS switch rollback itself, released its lease and kept the journal. […]`
  > `Managed BIND root directory and package ownership matched on two read-only walks. […]`
  > `Native BIND vendor files or loaded systemd unit identity are unknown. The server owner should inspect the named service unit, its package ownership and startup options before the same operation resumes; no inverse was started. parse named.service identity: systemctl returned incomplete DNS unit identity`

  The status names the command and, in the same output, says the native BIND unit identity is unknown.
- **Owner command.** It ran once as root and took 0.87 s. It exited **3** with the refusal quoted at the top of this README. No inverse effect was observed.
- **After the command.**
  - The journal was **still present**: `rolling-back`, sha `f1692d5f…`, the same inode. This is the recorded failure.
  - The ledger was byte-identical to the release reference (`e43e69a2…`).
  - The state receipt was byte-identical to the pre-cut source (`0d59887a…`).
  - `owner_files_unchanged: true`: main and managed config hashes and identities, database identity, `quick_check ok`, domain count 1.
  - PowerDNS served alone.
  - The post-command status was the same text, exit 3, with no evidence change.
- **Re-run.** It exited 3 with identical output (sha `532e3d01…`) and no evidence change.
- **Probes.** All probes, including the post-collect probe, were `indeterminate` with the same fingerprint `8e8ef55b…6fd9`. `recovery_outcome.classification: repeated_nonconvergence`. The pass requires `rolled_back_source_serving`.
- **Result failures (verbatim).** `owner command left the switch journal in place`; `post-command classification is repeated_nonconvergence, want rolled_back_source_serving`. No safety, verification or diagnostic failures, and no ambiguities.
- **PowerDNS.** MainPID 3066 (started 07:15:41) before the cut, at the boundary, after the restart, after the owner command, after the re-run, after the stability window and in the final `systemctl show`. `NRestarts=0`, `enabled`. The `pdns.service` journal has no entries after 07:15:43.
- **BIND.** Before the run, `named.service` and `bind9.service` were `not-found`. At the end both were `LoadState=masked`, `UnitFileState=masked`, `inactive`/`dead`, MainPID 0, `NRestarts=0`, with no named/bind9 journal entries. `/etc/bind/named.conf.local` and `named.conf.options` were rewritten at 07:16:03.96, at 176 and 226 bytes, versus the `config_before` package defaults. That is consistent with the journal's `config_after`; the file bytes were not compared. These are the files the inverse would restore. The `bind9` package stays installed.
- **Health.** Agent PID 6595 was unchanged after the Panel restart. 31/31 samples (Agent, Panel, UDP+TCP DNS) were healthy from 07:16:07.33Z to 07:16:37.33Z. Post-collect queries to `10.0.2.15` and `192.0.2.10` returned `aa`, `192.0.2.10` over UDP and TCP.
- **Panel-visible message** (`Agent.ServiceMutationStatus` at 07:16:42Z): `failed`/`interrupted`, `dns_native_recovery_unknown_after_restart`, stored text: "The interrupted DNS switch could not be verified after the Agent restarted. Its exact journal remains for DNS recovery, and new DNS changes are blocked. The server administrator should inspect the native DNS service and run recovery dns-switch-status --quiesced; after resolving the reported cause, restart the Agent to retry this same operation. Unrelated host changes can continue." The journal is retained, so the reconciled text is correctly not shown.

## Cell 2: `bind__intent__after-write__standalone__peer-reachable` — FAILED

- Debian 13, request `8f027a47437e196b05cf295aff339d93`. The run went from 07:20:08Z to 07:20:52Z.
- **Kill.** Tagged Agent PID 3861 (start ticks 10108) was in state `T` and received SIGKILL at 07:20:19.335349Z. Raw return −9, exit 137, reaped: `kill_proven: true`. The trigger exited 75.
- **Journal at the cut.** V2, phase `intent`, sha256 `58111a61044c61a388541af089036ca4b4a818530e03ad76f3c5855921e32b8d`. It has the same frozen `pdns-source/v1` shape (database logical sha `b497cbbd…db7e`, the same three `config_before` paths). `target_units_before` records `not-found`. At `intent` the product had already installed `bind9`: the apt run was 07:20:12–07:20:15, with the same upgrade of `bind9-host` and `bind9-libs`. It had written the install-ownership receipt at 07:20:09.96, and masked both units at 07:20:10. The journal first appeared at 07:20:19.30.
- **Agent decides.** The ordinary Agent was PID 6516. At 07:20:19.73Z it wrote the journal at `rolling-back` (sha `cc74eaa0…28f5d`) and released the job: `failed`/`interrupted`, `dns_native_recovery_unknown_after_restart`, empty active request. Ledger after release: sha256 `ae118d508cd2310794bd4be1ee345efaa73153d494724e303db2481900fe0067`, 2938 bytes. The refusal line is identical to cell 1 except for the request ID, logged at `2026/09/29 07:20:19`.
- **Status.** Same text as cell 1 for this request. It exited 3, changed no evidence, named the command, and reported `parse named.service identity: systemctl returned incomplete DNS unit identity`.
- **Owner command.** It exited **3** after 0.87 s with the same refusal (`BIND switch native preimage is unknown or owner-modified` / `DNS target is not a loaded unit`).
- **After the command.** The journal was retained (`rolling-back`, `cc74eaa0…`). The ledger and state were byte-identical, owner files were unchanged, and PowerDNS served alone. The post-command status was the same, exit 3.
- **Re-run.** It exited 3 with identical output and no evidence change.
- **Probes.** All were `indeterminate`, fingerprint `1272180d…f159`. Classification `repeated_nonconvergence`. The result failures are the same two strings as in cell 1.
- **PowerDNS.** MainPID 3076 (started 07:19:58) throughout, `NRestarts=0`.
- **BIND.** `not-found` before the run, and masked/inactive with PID 0 at the end.
- **Health.** 31/31 from 07:20:22.40Z to 07:20:52.40Z. Agent PID 6516 was unchanged. Post-collect UDP and TCP answers were `aa`, `192.0.2.10`.
- **Panel-visible message** (07:20:58Z): the same stored text and code as cell 1, for this request.

## Refusal and likely cause (reading of the code, not verified)

`internal/dnsenginerecovery/bind_switch_inverse_executor_linux.go:84` wraps the `AssessNative` error as "BIND switch native preimage is unknown or owner-modified". The inner error "DNS target is not a loaded unit" comes from the `default` class in `internal/dnsenginerecovery/stopped_unit.go:131`. That branch is reached through `probeStoppedUnitWithCgroupClass` → `VerifyStoppedUnit` (`stopped_unit_linux.go`). There is no fresh-target or persistent-mask class for `named.service` on this independent path.

The status line comes from `internal/dnsunitidentity/identity.go:46`. A masked unit does not return all seven identity properties; for example, `ExecStart` is absent. See `bind-unit-diagnostics-post-collect.txt`: `LoadState=masked`, `FragmentPath=/etc/systemd/system/named.service`, `LoadError=org.freedesktop.systemd1.UnitMasked`.

In the Agent's own BIND rollback guard, `cmd/agent/dns_engine_bind_rollback_guard.go:42` uses `VerifyStoppedFreshSourceTarget` only for an empty source. A PowerDNS source is not empty, so it takes the `loaded` path too.

The package guard masks `named.service`/`bind9.service` before the `intent` write, so both owner-inverse cells, as defined, reach a masked target that the owner inverse cannot accept. Whether the owner inverse should admit the guard's persistent mask for a V2 BIND target (as `1c336f6d` did for first-install journals), and how it should treat the `bind9` package, the upgraded `bind9-host`/`bind9-libs` and the install-ownership receipt, is a product decision. It is not made here. The pass definition in the kill-matrix README is unchanged.

## What this does not prove

The controller behaved as designed. Kill proof held. The Agent decided and logged the owner command. Status named it without mutating. Evidence did not change across the status, owner and re-run steps. PowerDNS kept serving with an unchanged MainPID and unchanged owner files.

This run covers one path only, on Debian 13 only, with an unsigned local recovery kit enrolled as fixture work. There was no reboot, no owner-edit race, no power loss, no paired topology and no installed server. The owner inverse's rollback effects (BIND configuration restore, package or receipt handling, the `rolled-back` checkpoint, journal retirement) were **not exercised**. Neither was the computed "reconciled" status text: the journal was never retired.

Exit status 3 is what both the refusal and the re-run returned. Exit 3 on the re-run of a *successful* inverse is a named gap that this run could not reach. The ledger alone cannot distinguish an owner rollback from the Agent's release. Here the retained journal shows that no rollback happened.

Package installation used the live Debian mirror: `bind9` `1:9.20.29-1~deb13u1`, `pdns-server` `4.9.17-0+deb13u1`. A later run may resolve different versions.

Each cell was run once and not re-run. Nothing here changes the 268-runnable denominator, closes P0.4, or passes any acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file except this README and itself.
