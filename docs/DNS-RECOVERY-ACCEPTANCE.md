# DNS engine recovery acceptance register

*Engineering record · [Türkçe](DNS-RECOVERY-ACCEPTANCE.tr.md) · first
compiled 2026-09-29 from a read-only audit of source `e9d1019d`*

This is the single list the [September 28 handoff](HANDOFF-2026-09-28.tr.md)
asked for. It binds every DNS engine mutation the product can actually start
to exactly one state. It is the acceptance ledger for roadmap item 1 (the DNS
recovery contract); the [resilience contract](RESILIENCE-CONTRACT.md) and the
[DNS artifact contract](DNS-ENGINE-ARTIFACT.md) keep the invariants and the
byte-level formats. Decisions taken on this register are recorded as
[D-026](DECISIONS.md#d-026--dns-engine-recovery-refuse-the-unrecoverable-switch-accept-same-operation-recovery-for-first-installs).

## States

| State | Meaning |
|---|---|
| **PASSED** | The named cut was run on a real disposable system with the current producer, the same operation continued or rolled back, owner changes were preserved, and the report with its limits is retained. A pass is scoped to the cells it names. |
| **GAP** | The path is supported and reachable; the named cut has code and component tests but no native trial, or has a known missing piece. Named gaps stay open until evidence exists. |
| **UNSUPPORTED, refused** | The product refuses the operation before any mutation, in Panel and Agent, with actionable guidance. Not a bug; a scoped release limit. |
| **UNSUPPORTED, not refused** | Reachable but without a recovery contract. Not an acceptable state. Must move to one of the other three before release. |

Common facts (source references are for `e9d1019d` plus the D-026 gate):

- Panel entry: `POST /api/v1/dns/engine/switch/preview` then `/switch`
  (`cmd/panel/dns_engine.go`, handler near line 3660; blockers re-run at
  commit). Server setup uses the same blockers
  (`cmd/panel/setup_dns_operations.go`). Action selection:
  `dnsEngineAction`.
- Agent entry: RPC `SwitchDNSEngineV1` (`cmd/agent/dns_engine_rpc.go`) →
  `hostDNSEngineBackend.Switch` (`cmd/agent/dns_engine_host.go`).
- Agent restart recovery: `RecoverSwitch` → shared `Reconcile`
  (`internal/dnsenginerecovery/reconcile.go`). The target is checked first;
  on mismatch the exact frozen source must be proved before a `rolling-back`
  decision is written. The Agent never executes V2/V3/V4 inverses itself; it
  names the owner command.
- Owner commands: `cmd/recovery/entry.go`. Read-only status:
  `recovery dns-switch-status [--quiesced] [--request-id]`.
- Gates: `pdns_primary_switch_paused` (paired primary → PowerDNS, any source)
  and, from D-026, `bind_source_pdns_switch_unsupported` (serving BIND →
  PowerDNS, any topology).

## Register

| # | Path (how reached) | Journal | Recovery before target start / after target start | Owner change at recovery | Native evidence (limit) | State |
|---|---|---|---|---|---|---|
| 1 | Fresh BIND, standalone (setup or card `install`) | V1 | Agent, same request, both sides. No owner CLI (accepted by D-026 decision 2). | Owner-aware config preimage in rollback | [Arch target-staged/before-write](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-TARGET-STAGED-ARCH-20260925.md): one early cell, forward only. [Fresh-install cells 2026-09-29](../deploy/e2e/dns-kill-matrix/evidence/fresh-install-20260929/README.md): `bind__target-verified__before-write` (Arch, cut after `named` started, journal at `target-started`) and `bind__target-verified__after-write` (Debian) both converged forward at Agent startup; same `named` PID, 31/31 health, authoritative UDP/TCP. | **PASSED** for the after-start cut on Debian and Arch. **GAP**: Debian pre-start cell, reboot; no continuity through the cut is claimed. |
| 2 | Fresh BIND, paired primary | V1 | Agent, same request | as 1 | [Pair target-staged/after-write + management-disabled reboot](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md); [V3 deletion terminal](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) | **PASSED** for those two before-start cells. **GAP**: source-stopped, target-started, target-verified cuts |
| 3 | Fresh BIND, paired secondary | V1 | Agent, same request | as 1 | none — every secondary in the trials so far was panel-free | **GAP**: no interruption trial |
| 4 | Fresh PowerDNS, standalone (APT hosts only) | V1 | Agent `rollbackPDNSSwitch`, before and after start. No owner CLI (D-026 decision 2). | `verifyOwnerAwarePreimage` | [Fresh-install cells 2026-09-29](../deploy/e2e/dns-kill-matrix/evidence/fresh-install-20260929/README.md): `pdns-switch__target-started__after-write` passed (startup rollback, then the same request re-ran forward on retry; ~3 s DNS gap). `pdns-switch__target-staged__after-write` **failed**: PowerDNS had never started and its unit was the install's own persistent mask, but the V1 rollback's stopped-target proof required `LoadState=loaded`; recovery ended `dns_native_recovery_unknown_after_restart`, the retry was refused, no DNS served. Prior state (no DNS) was not damaged. Fix `VerifyStoppedFreshSourceTarget` (commit `1c336f6d`); [re-run on the fixed source](../deploy/e2e/dns-kill-matrix/evidence/fresh-install-rerun-20260929/README.md): `target-staged__after-write` (same request bytes) and `intent__after-write` both rolled back at Agent startup and converged forward on the same-request retry; 31/31 health, authoritative UDP/TCP. | **PASSED** for the pre-start (intent, target-staged; masked never-started unit) and after-start cuts on Debian 13. **GAP**: the `not-found`/`loaded` accepted states and the runtime-mask refusal have component tests only; no reboot; rolled-back verdict code inferred from journal/ledger, not logged. |
| 5 | Fresh or reconfigured PowerDNS, paired secondary | V1 | Agent, same request | as 4 | none | **GAP**: no interruption trial |
| 6 | Fresh paired PowerDNS primary, V3 (empty source) | V3 (tests only) | Before start: owner CLI `recover-dns-pdns-fresh-prestart`. After start: Agent forward only; no after-start inverse. | SQL/daemon drift check refuses | [V3 native after-start forward + SIGKILL](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-native-20260928/README.md), [V3 prestart inverse](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md), [V3 zone lifecycle](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md). Not reached through the public RPC. | **UNSUPPORTED, refused** (`pdns_primary_switch_paused`). Opening it belongs to item 2 and needs an owner-edit cut, an after-start inverse or an explicit forward-only policy, and acceptance through the public RPC. |
| 7 | PowerDNS → BIND, standalone, PowerDNS active / BIND inactive | **V2** | Agent writes the `rolling-back` decision and then refuses; owner CLI `recover-dns-bind-switch` executes the inverse from rolling-back/rolled-back. After start: forward only once the target is verified. | Main-config edit refused, evidence kept | [Protected owner CLI](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md): rolling-back/after-write after target start, owner edit refused, CLI interrupted, reboot | **PASSED** for that decided-rollback cell. **GAP**: intent, target-staged, source-stopped and target-started cuts under the V2 producer. The six 2026-09-25 Agent-mediated BIND reports used V1 and are historical for this row. |
| 8 | PowerDNS → BIND, paired | V1 | Agent, same request | owner-aware | none for paired | **GAP** |
| 9 | **BIND → PowerDNS**, standalone and paired secondary (card `switch`, or `install` when PowerDNS is absent) | — | — | — | none | **UNSUPPORTED, refused** by D-026 decision 1 (`bind_source_pdns_switch_unsupported`). Before D-026 this row was *unsupported, not refused*: V1 journal, Agent-only inverse, V4 producer with no caller. |
| 10 | BIND → PowerDNS, paired primary | — | — | — | — | **UNSUPPORTED, refused** (`pdns_primary_switch_paused`) |
| 11 | Running BIND adoption (`adopt_unmanaged`, standalone, Debian; Arch refused) | V2 `SourceBIND` | Agent refuses and names the CLI; owner CLI `recover-dns-bind-adoption` from rolling-back/rolled-back | Same-serial zone edit refused | [Adoption owner CLI](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-ADOPTION-OWNER-CLI-20260927.md): one cell, no reboot, controller handoff deliberately unverified | **PASSED** for one cell. **GAP**: early and after-start cuts, reboot |
| 12 | Stopped unmanaged BIND takeover | V1 (fresh-install transaction) | as 1 | as 1 | none | **GAP** (same class as 1) |
| 13 | External PowerDNS adoption (`adopt`, standalone, APT) | V1 | Agent inverse, plus owner CLI `recover-dns-pdns-adoption` from rolling-back/rolled-back | Static config edit refused | [Adoption cells](../deploy/e2e/dns-kill-matrix/README.md) (`NATIVE-PDNS-ADOPTION-*`), [owner edit](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-OWNER-EDIT-20260925.md), [protected owner CLI + reboot](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-PROTECTED-OWNER-CLI-20260926.md), [management-absent boot](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-MANAGEMENT-ABSENT-BOOT-20260925.md) | **PASSED** (Debian 13, unsigned local build) |
| 14 | `reinstall_active` BIND, standalone | V1 | Agent, same request | as 1 | none | **GAP** |
| 15 | `reinstall_active` PowerDNS | — | — | — | — | **UNSUPPORTED, refused** (Panel and Agent) |
| 16 | Generic engine install/stop/uninstall RPC | — | — | — | — | **UNSUPPORTED, refused** (`genericDNSEngineMutationRefusal`) |
| 17 | Zone add/edit/delete on a pair (zone-sync V3 ledger, not the switch journal) | zone-sync v3 | Same request through `RecoverDNSZoneV3`, Agent-mediated. Parentless deletion stays pending until owner enrollment proves absence. | Typed owner-edit conflict | [V3 deletion terminal](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md), [owner edit](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-OWNER-EDIT-20260927.md) with its [correction](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-OWNER-EDIT-CORRECTION-20260927.md), [V3 zone lifecycle](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md), [owner-proof deletion](../deploy/e2e/dns-kill-matrix/evidence/pdns-bind-owner-proof-20260928/README.md) | **GAP**: no native SIGKILL inside a zone-sync operation (every cut so far was on the engine switch). Belongs to item 2. |

Not yet audited: `ConfigureDNSClusterV2` pairing changes.

## What closes item 1

Item 1's exit condition is: for supported interruptions the same operation
continues or rolls back; owner changes are preserved; pre-start and post-start
behaviour is explicit. With D-026, the rows that still block it are evidence,
not code:

1. Row 4 — fresh standalone PowerDNS: the after-start cell passed on
   2026-09-29; the before-start cell exposed a verified defect (stopped-target
   proof rejected the install's own mask for an empty-source journal). Fixed
   in `1c336f6d`; the `target-staged` and `intent` cells passed on the fixed
   source the same day (done).
2. Row 1 — fresh standalone BIND: the after-start cells passed on Debian and
   Arch on 2026-09-29 (done). The fixture now
   prepares `bind__target-verified__{before,after}-write__standalone__*` with
   an empty source; the before-write edge cuts after BIND has started with the
   journal still at `target-started`. Fresh BIND at `target-started`,
   `source-stopped` and `rolled-back` stays closed by design: those cells are
   defined as managed-PowerDNS-source cells and a fresh run would change what a
   pass there means.
3. Row 7 — PowerDNS → BIND under the V2 producer: one early cut (intent or
   target-staged) **with the Agent restarted before the owner command**, so
   the Agent-decides / owner-executes split is shown on the normal path. This
   is blocked by the released-undecided admission gap below and by the
   controller; both are open.
4. Rows 3 and 5 — fresh paired secondary, BIND and PowerDNS — cannot be
   prepared: no peer script plays a panel-free native *primary* that serves
   the product catalog and member zones with AXFR/NOTIFY to the guest, and the
   trigger rejects the fresh PowerDNS paired-secondary manifest because it has
   the same shape as the legacy reconfiguration manifest. This is the
   two-topology acceptance work of item 2; the rows stay open there and are
   not reclassified as N/A.

Operation guidance (D-024), closed in source on 2026-09-29:
`recovery dns-switch-status` and the Agent refusals now name
`recover-dns-pdns-fresh-prestart`, `recover-dns-bind-switch` and
`recover-dns-pdns-adoption` (rows 6, 7, 13) with the exact `--request-id`,
using the same admission predicates the commands themselves run, and print an
explicit "no owner recovery command applies" text otherwise. The Panel shows
the ledger message verbatim; it points to the status command.

**Open code gap found by that work (rows 7, 11, 13) — owner command refused
after an Agent restart.** When a restarted Agent cannot complete recovery it
retains the journal and releases its ledger lease
(`dns_native_recovery_unknown_after_restart`), which the evidence reader
reports as `released-undecided`. `recover-dns-bind-switch`,
`recover-dns-bind-adoption` and `recover-dns-pdns-adoption` admit only an
active-lease status or a terminal rolled-back job
(`activeDNSInverseStatus`), so with a running Agent a journal left at
`rolling-back` has no admitted owner command. The retained native passes for
rows 7 and 11 were taken with the Agent inactive and do not cover this. The
V2 producer makes this the normal path for row 7: the Agent writes the
rollback decision and never executes the V2 inverse itself. Closing it needs
an admission rule for the Agent's own deliberate release, component tests, a
controller step that restarts the Agent before the owner command, and one
native cell. The fixture already prepares
`bind__{intent,target-staged}__after-write__standalone__peer-reachable` with a
managed PowerDNS source; `run_cell.py` cannot run them yet (it expects a V1
journal for every cell but the handoff cell and has no owner-command step).

Rows 2, 6 (opening), 8, 11 (further cuts), 12, 14 and 17 remain open and are
carried by item 2 or later. Repeating a passing cell without a producer change
adds nothing to this register.
