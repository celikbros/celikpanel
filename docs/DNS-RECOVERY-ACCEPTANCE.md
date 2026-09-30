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
| 7 | PowerDNS → BIND, standalone, PowerDNS active / BIND inactive | **V2** | Agent writes the `rolling-back` decision and then refuses; owner CLI `recover-dns-bind-switch` executes the inverse from rolling-back/rolled-back. After start: forward only once the target is verified. | Main-config edit refused, evidence kept | [Protected owner CLI](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md): rolling-back/after-write after target start, owner edit refused, CLI interrupted, reboot | **PASSED** for that decided-rollback cell (Agent inactive, target had started). **PASSED** for the pre-start cuts (intent, target-staged) with the Agent restarted and running: first run failed on `7ad24282` ([evidence](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-20260929/README.md)), fixed in `411398d9`, [re-run passed](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-rerun-20260929/README.md). **PASSED** for the post-stop cuts (`source-stopped`, `target-started` after-write) with the Agent restarted and running ([evidence](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-critical-20260929/README.md), source `411398d9`, harness `7c5dfe17`): owner command exit 0, journal retired, ledger and state receipt byte-identical, PowerDNS serving again as the only port-53 authority, SOA serial unchanged, BIND stopped, 31/31; measured PowerDNS outage upper bounds 21.6 s and 9.4 s, DNS not continuous by construction. **GAP**: before-write edges and the `rolled-back` cell under V2; reboot; re-run exit status 3; after rollback the staged BIND generation tree, `rndc.key`, the install-ownership receipt and the upgraded `bind9` libraries remain, and the BIND unit is left unmasked/disabled when it had started but masked when it had not. The six 2026-09-25 Agent-mediated BIND reports used V1 and are historical for this row. |
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

## Item 1 status: closed on 2026-09-29 with named limits

Owner decision (2026-09-29): run the post-stop PowerDNS → BIND cells first;
if they pass, item 1 closes and the remaining rows move to item 2 by name.
They passed. The standalone install and switch paths — fresh BIND, fresh
PowerDNS, PowerDNS → BIND, external PowerDNS adoption (rows 1, 4, 7, 13) —
now have native same-operation recovery evidence for a cut before and a cut
after the target starts, with owner changes preserved and the behaviour on
each side stated; the paths without a recovery contract are refused before
any mutation (rows 6, 9, 10, 15, 16). Three standalone paths are reachable
and not yet at that level (rows 11, 12, 14); they are carried below by name,
not counted as passed. Two product defects were found by the
native runs and fixed on the way (`1c336f6d`, `411398d9`), and one recovery
dead end was removed (`7ad24282`); component tests had not exposed any of
the three.

Carried to item 2, open, not reclassified:

- rows 2, 3, 5, 8 and 17: every paired topology, including the missing
  panel-free native primary peer and the zone-sync interruption cell;
- row 11 (running-BIND adoption) and row 13 (PowerDNS adoption) with a
  running Agent: the released-job admission has component tests only;
- row 12 (stopped unmanaged BIND takeover) and row 14 (BIND reinstall): no
  native interruption trial;
- row 7: before-write edges and the `rolled-back` cell under the V2 producer;
  the standalone critical cells still expect V1 when run without the
  owner-inverse flag;
- reboot or power loss during recovery for every row that has none;
- guidance, fixed in source on 2026-09-29 (component tests; native re-run
  pending): completed re-runs exit 0; status before the rollback decision
  names the true next step. Still open: no durable record of which actor
  retired a released journal; `recover-dns-pdns-fresh-prestart` and the V4
  command still exit 3 on a terminal re-run;
- residue and end state after a PowerDNS → BIND rollback, fixed in source the
  same day (rollback standby: BIND always ends guard-masked; the exact staged
  generation is removed; owner-changed trees are kept). Still open: files
  BIND writes in its working directory and the upgraded `bind9` libraries
  remain;
- listener proofs now classify loopback and link-local sockets (component
  tests; native re-run pending);
- everything is Debian 13 (one Arch BIND cell) with unsigned local builds;
  signed-release and installed-server acceptance belong to items 3 and 4.

Closing item 1 does not close P0.4.

## Item 2 progress log

Entries are dated results against the carried list above. A row's state cell
in the register changes only when its native evidence exists.

**2026-09-29, batch 4** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch4-adoption-reboot-20260929/README.md),
source `8f86bdad`, product binaries identical to `411398d9`; seven standalone
Debian 13 cells, each run once):

| Row | Cell | Result |
|---|---|---|
| 11 | running-BIND adoption, owner command with the Agent running | **passed**: owner's `named` kept its PID, owner files identical. Reboot not exercised (flag not admitted for this cell). |
| 13 | PowerDNS adoption, `intent`, restarted Agent rolls back by itself, reboot after recovery | **passed**: retry converged, second window 31/31. |
| 13 | PowerDNS adoption, `rolled-back`, owner command | **failed (harness)**: product side looked correct; the recovery probe cannot classify an external-PowerDNS rollback that ends without a state receipt. Since `3cc2de22` the Agent finishes this cut itself, so the cell's pass definition changes. |
| 7 | V2 PowerDNS → BIND `target-started`, reboot between the Agent's decision and the owner command, reboot after recovery | **passed**: journal and ledger unchanged across the first reboot; PowerDNS serving after the second; outage upper bound 93.8 s including the reboot. |
| 7 | V2 PowerDNS → BIND `rolled-back` after-write | **passed**. |
| 4 | fresh PowerDNS `target-started`, reboot after recovery | **passed**: second window 31/31. |
| 1 | fresh BIND `target-verified` after-write, reboot after recovery | **failed (safety)**: the `current` pointer was removed between the kill marker and the SIGKILL; the restarted Agent could neither verify nor roll back and released the job as unknown; `named` served from memory until the reboot, then failed to start; DNS refused 31/31. |

Findings from that batch, all open until re-run natively:

- the tagged kill hook can let the calling goroutine run into the product's
  error path before the process stops, so a cut may land past its named
  boundary. Retained cells whose hook is followed by a mutating error path
  must be re-run once the hook is fixed; until then the row 1 and row 4
  after-write passes carry that caveat;
- independent of the hook, the product cannot repair a verified BIND target
  whose pointer is missing, and the publisher's error path removes the pointer
  while BIND may still be enabled. A reboot in that state takes DNS down;
- the controller rebooted after a flow whose retries and probes had failed;
- `recovery dns-switch-status` was not available in cells without an enrolled
  recovery runtime.

Source changes the same day, component tests only, native re-run pending:
rollback standby and staged-generation removal (`f7a844f7`); either peer
catalog producer accepted by a secondary, V1 adoption finished by the Agent at
`rolled-back`, takeover retry (`3cc2de22`); harness for fresh paired-secondary
cells against a panel-free native primary of either engine, takeover and
reinstall fixtures (`883102c1`, `7ce3827e`).

**2026-09-29, batch 5, exploratory** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch5-paired-first-20260929/README.md),
source `65b86621`, built before the kill-hook fix `c04d8a2b`; first native run
of the panel-free primary peer and the paired-secondary flow; each cell once):

| Row | Cell | Result |
|---|---|---|
| 3 | fresh BIND secondary against a BIND primary, `target-verified` after-write | **failed, kill-hook race signature**: pointer removed between marker and SIGKILL, as in batch 4 c6. Peer verdict passed. To be re-run with the fixed hook. |
| 3 | same cell against a PowerDNS 5.1.4 primary serving the catalog in the BIND catalog format, reboot with management disabled | **passed**, peer verdict passed, second window 31/31; carries the kill-hook caveat. |
| 5 | fresh PowerDNS secondary against a BIND primary and against a PowerDNS primary, `target-started` after-write | **unverified, product defect before the boundary**: PowerDNS 4.9.17 consumed the catalog, transferred the member and answered authoritatively, then wrote `{"consumer": {"unique": …}}` into the member row's `options`; the Agent requires that field empty, retried until its limit and failed; the rollback refused the live database; the mutation manager went fail-closed; no DNS served. |
| 12 | takeover of a stopped unmanaged BIND, `target-staged` after-write | **passed**; carries the kill-hook caveat. |
| 14 | BIND reinstall | **not run**: the controller read the state receipt with v1 keys and stopped before any mutation. |

Native answers: PowerDNS does write a non-empty `options` value on consumed
member rows; a BIND secondary does load the member from a PowerDNS primary
serving a BIND-format catalog.

**2026-09-29, batch 6a, fixed boundary hook** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch6a-fixed-hook-20260929/README.md),
source `94cd124b`; twelve cells, each run once, no harness workaround, no
re-run): **all twelve passed.** No native mutation fell between the kill
marker and the SIGKILL in any cell.

| Row | Cell | Result |
|---|---|---|
| 1 | fresh BIND `target-verified` after-write (Debian) and before-write (Arch), reboot after recovery | passed; the cell that lost DNS in batch 4 kept its pointer and served after the reboot |
| 4 | fresh PowerDNS `target-staged` and `target-started` after-write, reboot after recovery | passed |
| 7 | V2 PowerDNS → BIND `target-staged` and `target-started`, Agent running, reboot before the owner command (post-stop cell) and after recovery, then the same switch again as a new request | passed; BIND ended guard-masked in both, staged generation removed, re-run exit 0, the retried switch completed forward with the mask lifted |
| 11 | running-BIND adoption, owner command with the Agent running, reboot before the command and after recovery | passed |
| 13 | PowerDNS adoption at `rolled-back` and at `intent`, restarted Agent finishes by itself | passed |
| 12 | takeover of a stopped unmanaged BIND, reboot after recovery | passed |
| 3 | fresh BIND secondary against a native BIND primary, and against a native PowerDNS 5.1.4 primary publishing its own PRODUCER catalog; reboot with management disabled | passed both; the Agent logged the catalog format it accepted (BIND, PowerDNS) and the peer verdicts passed |

Not exercised by that batch: the missing-pointer repair (the pointer stayed
intact), any PowerDNS secondary, reinstall. Observed and not judged: the
status read before a takeover exits 3 because the reader cannot classify an
owner-installed, disabled `named.service`; the status read before recovery of
the adoption `rolled-back` cell names the owner command although the Agent
then finishes by itself. One run per cell shows the boundary held in these
runs; it does not prove the race impossible.

**2026-09-30, batch 6b** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch6b-pdns-secondary-20260930/README.md),
source `6f2fb028`; eight cells, each run once, no harness workaround, no
re-run): seven passed, one unverified. The boundary held in every cell that
had a kill.

| Row | Cell | Result |
|---|---|---|
| 5 | fresh PowerDNS secondary, `target-started` after-write, against a native BIND primary and against a native PowerDNS primary publishing its own catalog; reboot with management disabled | **passed** both, peer verdicts passed. The restarted Agent rolled back a database the daemon had written into, then the retry converged. |
| 5 | fresh PowerDNS secondary, `target-staged` after-write; `target-verified` before-write (recovery went forward); `rolling-back` after-write | **passed** |
| 3 | fresh BIND secondary on Arch, `target-staged` before-write, against a PowerDNS primary with its native catalog; reboot with management disabled | **passed** |
| 12 | takeover of a stopped unmanaged BIND, stock options | **passed**; the owner-directive retry case is not exercised |
| 14 | BIND reinstall after the owner purged `bind9` | **unverified, product defect before any journal**: the managed BIND setup registers a `dpkg-statoverride` for `/var/cache/bind` by group name; the purge removed the `bind` group and left the override, so dpkg refuses to unpack any package on that host until the owner removes it |

Native answers: a PowerDNS 4.9.17 consumer writes `{"consumer": {"unique":
"<label>."}}` into a consumed member's `options`, with the member label of
whichever catalog format the primary serves, and writes no metadata,
comments or keys for consumed members; the consumer row itself keeps
`options` and `catalog` NULL.

**2026-09-30, batch 7, breadth** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch7-breadth-20260930/README.md),
source `dbd6a6b6`; fourteen cells on the Debian 13 kill guest, each run once,
no harness workaround, no re-run): **all fourteen passed**; the boundary held
in every cell.

| Row | Cells | Result |
|---|---|---|
| 7 | V2 PowerDNS → BIND before-write edges: `target-staged` (peer-unreachable placement), `source-stopped`, `target-started`, `rolled-back`; and `source-stopped` after-write with a reboot before the owner command, a reboot after recovery and the same switch retried as a new request | passed; first native run of the before-write edges; the retried switch completed forward after two reboots |
| 12 | takeover of a stopped unmanaged BIND carrying owner `recursion` / `allow-transfer` directives, reboot after recovery | passed; owner files were back to the sealed preimage before the retry and the same-request retry converged through the takeover |
| 1 | fresh BIND `intent` after-write, reboot | passed |
| 4 | fresh PowerDNS `intent`, `target-verified`, `committed` after-write, reboot | passed |
| 3 | fresh BIND secondary `intent` after-write against a PowerDNS primary with its native catalog, management-disabled reboot | passed |
| 5 | fresh PowerDNS secondary `intent`, `committed`, `rolled-back` after-write against BIND and PowerDNS primaries, management-disabled reboot | passed |

Measured PowerDNS outage upper bounds in the post-stop V2 cells: 27.2 s,
8.8 s and 25.9 s (the last includes two reboots' surrounding work but not the
reboots themselves are judged); recorded, not bounded.

**2026-09-30, batch 8, fresh paired PowerDNS primary** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch8-pdns-primary-20260930/README.md),
acceptance branch `accept/pdns-primary-gate-open` commit `916e1577`, gate
open; the main line keeps the gate closed; ten cells through the public Agent
RPC, each run once, no harness workaround, no re-run). Row 6. Two passed,
seven failed on one wrong harness expectation, one unverified. The boundary
held in all nine cuts.

| Cell | Product behaviour observed | Verdict |
|---|---|---|
| pre-start cuts at `intent`, `target-staged`, `target-enable-intent` | restarted Agent rolled the install back by itself, retired the journal, unit back at its guard-masked standby, no database, candidate, receipt or listener; same-request retry converged; native BIND secondary answered exactly as the primary | **failed (harness)**: the pair check compared the primary's `www` A with the guest's management address instead of the zone's record |
| pre-journal cut at `intent` before-write | job ended with the pre-commit restart code; retry converged | **failed (harness)**, same cause |
| post-start cuts at `target-started`, `target-verified`, `committed` | forward only; the same request succeeded; PowerDNS kept its process | **failed (harness)**, same cause; the zone lifecycle and the reboots did not run because they follow a passing verdict |
| owner SQL edit between the kill and the Agent restart, post-start | refused; journal and database kept; the owner's row preserved; an unrelated mutation could begin | **passed** |
| Agent-released pre-start recovery finished by `recover-dns-pdns-fresh-prestart` | exit 0, pre-install state, ledger byte-identical to the Agent's release, re-run exit 0 | **passed** |
| owner configuration edit on a pre-start cell | the Agent answered `HOST_MUTATION_BUSY` before any mutation; no cut | **unverified**; cause not established |

Catalog re-stamp as observed: staged serial 1 with no metadata; at first
start the daemon added one `CATALOG-HASH` row and re-stamped the producer SOA
to an epoch serial; the state receipt and both servers' served serials
equalled it. Guidance gap recorded: `dns-switch-status` in the two owner-edit
cells does not name the owner change that was refused. Closed in source by
`cc2d430b` (component tests only).

**2026-09-30, pair 1, product flow, exploratory** ([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair1-20260930/README.md),
product `aa6b9380`, two CelikPanel servers installed by the real installer,
test-only license of D-027, no license service contacted). Rows 2, 3, 5, 17.
No topology reached the zone operations; every stop was a driver fault, and
the run passes no row.

| Topology (primary / secondary) | Reached | Stopped at |
|---|---|---|
| BIND / BIND | install, license, setup on both servers; the secondary took the catalog | driver waited for `pair_ready` on a secondary; the product reports `secondary_ready` there |
| BIND / PowerDNS | same | same |
| PowerDNS / BIND | plan refused with `pdns_primary_switch_paused`; nothing left behind | gate closed in that build, as expected |

Product finding: after a kernel upgrade that needs a host restart, the setup
plan answered HTTP 500 instead of naming the restart. Closed in source by
`a751e46a` (`host_restart_required` and the other typed firewall blockers);
component tests only.

**2026-09-30, pair 2, product flow, exploratory** ([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair2-20260930/README.md),
product `916e1577` with the gate open, driver `13213343`). Rows 2, 3, 5, 6,
17. No topology reached the zone operations; the run passes no row.

| Topology (primary / secondary) | Reached | Stopped at |
|---|---|---|
| BIND / BIND | setup on both servers; secondary `secondary_ready: true` | driver rule demanded `secondary_ready: false` on the primary; the product omits the field on a primary. Corrected in `184f633b`. |
| BIND / PowerDNS | same | same |
| PowerDNS / BIND | plan admitted with no blocker; PowerDNS installed | **product defect**: a fresh paired PowerDNS primary with no zones failed its own catalog check (empty list compared with an absent one). Batch 8 had only measured this path with a zone present. |

Further product findings from the same run: the wizard showed an open-ended
"result unknown" for 45 minutes while the Agent had recorded a verified
failure; background setup on a secondary read `license_required` once,
because only HTTP requests refreshed the license state; the customer archive
contained test evidence. All four are closed in source by `8a548090`
(component tests only): the catalog check accepts zero members; the wizard
names a verified failure, a recovery hold, a rollback, or an unknown result
bounded at five minutes; background setup uses the same license refresh as
the HTTP gate with no policy change; evidence and harness tests are pruned
from the archive and a guard refuses them.

**2026-10-01, batch 8r, fresh paired PowerDNS primary, second run** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch8r-pdns-primary-20261001/README.md),
acceptance branch `accept/pdns-primary-gate-open-2` commit `e3591875`, gate
open; the main line keeps the gate closed; the same ten cells through the
public Agent RPC with the corrected pair check, each run once, no harness
workaround, no re-run). Row 6. Nine passed, one failed on a harness
expectation. The boundary held in all ten cuts.

| Cell | Product behaviour observed | Verdict |
|---|---|---|
| pre-start cuts at `intent` (with a management-disabled reboot), `target-staged`, `target-enable-intent`; pre-journal cut | restarted Agent rolled the install back by itself; the native BIND secondary answered as the primary | **passed** |
| post-start cuts at `target-verified`, `committed` | forward only; the same request succeeded | **passed**; at `committed` the zone add, edit, delete and re-add all passed |
| post-start cut at `target-started`, then zone add, edit, delete, re-add, then a management-disabled reboot | every judgment before the reboot passed; after the reboot both servers answered the same catalog serial and the re-added zone; management stayed off and DNS answered throughout | **failed (harness)**: after the reboot the pair check demanded that the served catalog serial equal the state receipt's serial, while the zone operations had raised it. The product treats the receipt's serial as a minimum. |
| owner configuration edit on a pre-start cell | refused and held; the cell reached its cut this time | **passed** |
| owner SQL edit, post-start | refused; `dns-switch-status` now names the database content that neither the install nor the daemon wrote | **passed** |
| Agent-released pre-start recovery finished by `recover-dns-pdns-fresh-prestart` | exit 0; re-run exit 0 | **passed** |

Observed and not yet explained: about 60 seconds after the first-start
re-stamp the PowerDNS daemon re-stamped the catalog once more with a new
`CATALOG-HASH`, outside any zone request in one cell and inside the delete
request in the other. No deletion stayed pending, so owner enrollment was not
exercised. The scenario carries a zone, so the zero-zone primary is not
measured by this run. The `target-started` cell stays failed until its
expectation is corrected and the cell runs again.

**2026-10-01, pair 3, product flow** ([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair3-20261001/README.md),
product `d1f2ad87` with the gate open, test-only license of D-027, driver
`8d94c8ff`, no driver patch, no pass-rule change). Rows 2, 3, 5, 6, 17. All
three topologies passed install, license, setup on both servers and pair
readiness; all three stopped inside zone add on one native read. The run
passes no row.

| Topology (primary / secondary) | Reached | Stopped at |
|---|---|---|
| PowerDNS / BIND | the fresh paired PowerDNS primary with zero zones completed (first native measurement: catalog serial re-stamped to the epoch, `CATALOG-HASH` of empty input, zero members); the first zone was published on both servers with one serial | zone add: the driver proves zone state with `rndc`; the product's fresh BIND on Arch has no rndc key |
| BIND / PowerDNS | same steps; the BIND primary on Arch published the zone | same read, on the primary |
| BIND / BIND | same | same read, on the secondary |

Product finding: Arch's `bind` package creates no rndc key, Debian's does,
and the product creates none. The product's own BIND deletion proof calls
`rndc zonestatus` without a key argument, so on an Arch BIND installed by
the product the deletion proof of row 17 is in question from source
reading, not observed. Decision: the fresh BIND install creates the key
with the native tool when the package did not, never rewrites an existing
one, records it as product-created, removes it on rollback only when
unchanged, and every rndc failure for a missing key becomes a typed reason
with owner guidance (fix in progress; component tests only). Observation,
cause not established: the PowerDNS primary logged its notifications to
the secondary as sent to port 0 and the answers as spurious; the BIND
secondary transferred both zones regardless. No open-ended unknown state,
no contradictory guidance, no `license_required` read this time.

**2026-10-01, batch 9, c04 again and the zero-zone fresh paired PowerDNS primary** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch9-pdns-primary-zero-zone-20261001/README.md),
acceptance branch `accept/pdns-primary-gate-open-4` commit `3cceb29a`, gate
open; the main line keeps the gate closed; seven cells through the public
Agent RPC, each run once, no harness workaround, no re-run). Row 6, and row
17 for the parentless deletion. Five passed, two stopped on a harness
defect. The boundary held in all seven cuts.

| Cell | Product behaviour observed | Verdict |
|---|---|---|
| one member, `target-started` cut, zone lifecycle, management-disabled reboot (batch 8r c04 again) | forward completion; add, edit, delete, re-add; after the reboot both servers served one serial at least the receipt's, equal to the producer SOA in the database, members as expected | **passed** with the post-publication rule; the batch 8r failure did not recur |
| zero zones, pre-start cut at `target-staged` | Agent rolled the install back by itself to the pre-install state | **passed** |
| zero zones, post-start cuts at `target-started` and `committed` | forward only; the catalog with zero members verified | **passed** |
| zero zones, owner SQL edit after start | refused and held; DNS-only hold | **passed** |
| zero zones, `committed` then zone lifecycle; `target-started` then zone lifecycle and reboot | add created the server's first zone on both servers; edit passed; the delete of the parentless child stayed pending with `dns_peer_enrollment_required`, both servers answered REFUSED for it, both catalogs returned to zero members; the owner enrollment was performed with the packaged tools | **stopped (harness)**: the trigger's resume path matched the pending job with the parent's name while the job carries the child's; the resume, the re-add and the reboot did not run |

First native measurements with zero members: PowerDNS 4.9.17 writes
`CATALOG-HASH` and re-stamps the producer serial to the epoch at first
start; the state receipt, both servers and the database agree; the native
BIND secondary serves the empty catalog as SOA, NS `invalid.`, `version`
TXT `2` and no member; `notified_serial` stays 1. No second re-stamp
without a membership change in windows of 3.5 to 10 minutes; the one
observed came 120 s after the first and after the member set had changed.
Guidance gap recorded: the Agent's ledger text for the pending parentless
deletion does not name the owner tool; the Panel's screen text does. The
port-0 notify lines of the unfixed build recurred; the secondary
transferred every change within seconds (closed in source by `a6d93f06`).

**2026-10-01, pair 4, product flow** ([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair4-20261001/README.md),
product `96657d67` with the gate open, test-only license of D-027, driver
`92fc5eee`, no driver patch, no pass-rule change). Rows 2, 3, 5, 6, 17.
All three topologies passed install, license, setup, pair readiness, zone
add, record add and record edit; all three stopped inside zone delete on
two product defects. The run passes no row.

| Topology (primary / secondary) | Reached | Stopped at |
|---|---|---|
| BIND / BIND; PowerDNS / BIND | the parentless deletion was left pending with `dns_peer_enrollment_required`; the owner enrollment with the packaged tools succeeded; the zone was already gone on both servers | the retry stayed pending as `dns_peer_inspection_unknown` for 300 s: the product's inspector on the managed BIND secondary needs a loopback catalog transfer, and the product allows transfers from the paired primary only; the guidance does not name the denied local transfer |
| BIND (Arch) / PowerDNS | zone add, record add, record edit | domain deletion ran mail cleanup for a DNS-only domain; on Arch `/var/mail` is the distribution's stock symlink and no mail server is installed, so the safe open failed, the saga stopped in `mail_runtime_cleanup` with status unknown, and the screen showed the generic pending text with no actor and no action; the DNS deletion never started |

Measured for the first time natively: the rndc key of `80bb4353` was
created on every Arch BIND before named first started and recorded as
product-created; Debian's package key was recorded as provided and left
alone; rndc worked everywhere. The PowerDNS primary of `a6d93f06` sent every
notify with port 53: zero spurious answers and zero port-0 failures over
six mutations, one notify per zone per mutation received by the secondary.
Zone re-add, the management-disabled reboot and management return did not
run. Both defects have fixes in progress (component tests only).

**2026-10-01, batch 10, resumed zero-zone deletions** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch10-zero-zone-resume-20261001/README.md),
harness and fresh z05 product from `accept/pdns-primary-gate-open-6` commit
`0d4c0324`; z04 resumed on its kept batch 9 overlay with product
`3cceb29a`; no harness workaround, no re-run). Rows 6 and 17. Neither cell
passed. The corrected trigger matched the pending deletion of the child
zone, the Agent took the job to `recovering` with a lease, and the
owner-enrolled inspector channel to the panel-free BIND secondary ran
natively for the first time, twice; both times the Agent returned the
deletion to pending as `dns_peer_inspection_unknown`, so the recovered
delete, the re-add and the z05 reboot did not run. Inferred, not observed:
the harness's panel-free Arch BIND secondary has no rndc key, which the
inspector needs and which a panel-free owner prepares (harness fix in
progress). The inspector's own reason was neither logged nor shown; closed
in source by `0988bc9a` (reviewed reason tokens; component tests only). The
z05 hold worked: 6.5 minutes suspended with the checkpoint and boot IDs
unchanged. Fixed notify build on z05: six notifies with port 53, zero
spurious, zero port-0 lines. Two daemon re-stamps at +60 s and +120 s after
first start, each after a membership change.

**2026-10-01, pair 5, product flow: first complete topology** ([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair5-20261001/README.md),
product `b1e32275` with the gate open, test-only license of D-027, driver
`4941b605`, no driver patch, no pass-rule change). Rows 2, 3, 5, 6, 17.
State cells are not changed by this entry; the closing assessment judges
them.

| Topology (primary / secondary) | Reached | Stopped at |
|---|---|---|
| PowerDNS / BIND | **every step**: install, license, setup, pair readiness, zone add, record add and edit, zone delete (pending on `dns_peer_enrollment_required`; owner enrollment with the packaged tools; the retry deleted with the product's proof: REFUSED on both servers, native absent on both, catalog member absent), zone re-add, management-disabled reboot with both servers answering and the ledgers unchanged, management return with the readiness rule holding | — |
| BIND / BIND; BIND (Arch) / PowerDNS | zone add, record add and edit; first delete pending; enrollment (`--engine pdns` end to end for the first time); the inspector exchange completed | the retry stayed pending as `dns_peer_owner_edit_unknown` although nothing changed: the BIND primary's propagation plan carries no source state, the post-inspection probe selection refuses the empty engine, and that internal refusal is mapped to the owner-edit code (source reading matching the timing; fix in progress) |

Measured natively for the first time: the managed BIND secondary's catalog
stanza allows loopback transfers and the receipt carries version 2, no
transfer was denied; the mail stage was skipped for DNS-only domains on
every host including Arch; the managed PowerDNS secondary was not refused
by the owner's inspector; rndc keys product-created on Arch and
package-provided on Debian; thirteen PowerDNS notifies with port 53 and no
spurious answer. Not exercised: `dns_peer_catalog_transfer_refused`, the
`detail` field, `mail_runtime_cleanup_failed`, `config_unreviewed`, the
version 1 to 2 upgrade of an existing secondary, rndc key rollback.

**2026-10-01, batch 11, resumed zero-zone deletions on fresh fixtures** ([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch11-zero-zone-complete-20261001/README.md),
acceptance branch `accept/pdns-primary-gate-open-8` commit `542ccc8e`, gate
open; fresh fixtures; no harness workaround, no re-run). Rows 6 and 17.
Neither cell passed. The panel-free BIND secondary now carries the
owner-prepared rndc key (receipt `created`, rndc status ok before and after)
and batch 10's `dns_peer_inspection_unknown` did not recur. Both resumed
parentless deletions went to `recovering` with a lease and came back pending
as `dns_peer_owner_edit_unknown` although no member, zone or identity
changed: z04 before any inspector exchange while the PowerDNS daemon
re-stamped the catalog inside the attempt; z05 after a complete inspector
exchange, six minutes after the delete, following the daemon's re-stamp of
the empty catalog. Product finding: the daemon's periodic catalog re-stamp
(about every 60 s, whenever the member set changed) is treated as an owner
change when it falls between a deletion and its proof; pair 5's completed
topology retried within 14 s, before any re-stamp. Which check emits the
code is not logged. Fix in progress: the re-stamp is admitted at any point
of the deletion's life when identity, members and member serials are
identical, the recorded evidence is re-stamped, and the code carries a
detail naming the differing check. The recovered delete, the re-add and
the z05 reboot did not run.

Native coverage of the source changes listed after batches 4 and 5:

| Change | First native run that exercised it |
|---|---|
| boundary stop (`c04d8a2b`) | batch 6a, every cell |
| consumed PowerDNS member options; rollback of a fresh PowerDNS secondary after the daemon wrote (`66db850c`) | batch 6b |
| DNS-only hold instead of a fail-closed mutation manager (`66db850c`) | batch 8, owner SQL edit cell |
| missing-pointer repair and pointer ordering (`c04d8a2b`) | none: with the fixed hook the pointer no longer goes missing, so the repair itself has component tests only |
| forward completion when the `target-verified` write returned an error but is durable; status naming a missing pointer (`66db850c`) | none; component tests only |

Native re-run pending as of `8a548090`: fresh paired PowerDNS primary with
zero zones, before and after start; the three product flows through zone add,
edit, delete and re-add, deletion proof, owner enrollment and the
management-disabled reboot; the wizard states and the host-restart blocker on
a real host; reinstall after the `dpkg-statoverride` decision.

## What closed item 1

Item 1's exit condition is: for supported interruptions the same operation
continues or rolls back; owner changes are preserved; pre-start and post-start
behaviour is explicit. The work, in the order it happened:

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
   the Agent-decides / owner-executes split is shown on the normal path. The
   admission rule and the controller flow exist in source since 2026-09-29.
   [First native run](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-20260929/README.md)
   on `7ad24282`, `target-staged` and `intent` cells: **failed**, safety
   passed. The restarted Agent wrote the decision and released the lease, the
   read-only status named the command without mutating, and the owner command
   was admitted — then refused at native verification (`DNS target is not a
   loaded unit`): before its first start the BIND target sits under the package
   guard's persistent mask, which the owner-side stopped-target proof and the
   unit-identity reader reject. PowerDNS kept serving on the same process
   (31/31), owner files and the ledger were unchanged, the journal was kept.
   Verified defect, fixed in `411398d9` (never-started, guard-sealed target
   class for the V2 BIND switch inverse). [Re-run on the fixed source](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-rerun-20260929/README.md):
   both cells **passed** (`rolled_back_source_serving`): owner command exit 0,
   journal `rolled-back` then retired, ledger byte-identical to the Agent's
   release, PowerDNS on the same process throughout (31/31), BIND units still
   under the guard mask and never started, staged BIND configuration restored
   to its preimage, owner files unchanged, status and Panel text report the
   switch as reconciled (done for the pre-start cuts). The post-stop cuts
   (`source-stopped`, `target-started`) [passed](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-critical-20260929/README.md) the same day on the
   critical variant of the flow (done).
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

**Owner command after an Agent restart (rows 7, 11, 13) — fixed; native
evidence for row 7 pre-start cuts, component tests only for rows 11 and 13.** When a restarted Agent cannot complete recovery it
retains the journal and releases its ledger lease
(`dns_native_recovery_unknown_after_restart`), which the evidence reader
reports as `released-undecided`. Until 2026-09-29 `recover-dns-bind-switch`,
`recover-dns-bind-adoption` and `recover-dns-pdns-adoption` admitted only an
active-lease status or a terminal rolled-back job, so with a running Agent a
journal left at `rolling-back` had no admitted owner command; the retained
native passes for rows 7 and 11 were taken with the Agent inactive. The V2
producer makes this the normal path for row 7. The three commands now also
admit exactly the Agent's own deliberate release: that reason code only, the
exact request's journal at `rolling-back` or `rolled-back`, the released job
passing the Agent's own released-job predicate (no worker, no lease), and
every existing lock, worker-exclusion, native-evidence and owner-change check
unchanged. No schema or version changes. The command executes the inverse,
writes the `rolled-back` checkpoint and retires the journal; it does not
rewrite the finished ledger job. What the owner is shown afterwards is
computed at read time: a released job whose journal is no longer retained is
reported as reconciled, not as still blocking. `recover-dns-pdns-fresh-prestart`
(row 6) and the V4 command still refuse a released job; row 6 is refused by
the product anyway.

The controller has an explicit `--owner-inverse-after-restart` flow for
`bind__{intent,target-staged}__after-write__standalone__peer-reachable` with a
managed PowerDNS source: kill, Agent restarted and left running, rollback
decision and release observed, read-only status names the command without
mutating, owner command run, journal retired with the ledger unchanged,
PowerDNS still authoritative over UDP/TCP, BIND inactive, owner files
unchanged, idempotent re-run, 31 health samples.

**Named guidance gap (rows 7, 11, 13).** Re-running an owner inverse command
after it completed changes nothing and now says "already reconciled", but it
still exits with the unavailable status (3), as the existing terminal re-run
does. An owner or script can read that as failure. Changing it alters an
existing command contract and is left for a separate decision. The read-time
texts cannot say whether the owner command or a later Agent start retired the
journal, because no durable record holds that; a versioned owner-recovery
receipt would be the way to add it.

**Named harness gap (row 7, item 2).** The standalone managed-PowerDNS
critical cells (`bind` source-stopped, target-started, rolled-back) still
expect a V1 journal in the controller, while the producer writes V2 for that
source. They would be rejected at the boundary marker if run today and need
their own pass definition (the Agent cannot execute the V2 inverse, so
`rpc-retry` is not their recovery). The six 2026-09-25 reports remain
historical V1 evidence.

Rows 2, 6 (opening), 8, 11 (further cuts), 12, 14 and 17 remain open and are
carried by item 2 or later. Repeating a passing cell without a producer change
adds nothing to this register.
