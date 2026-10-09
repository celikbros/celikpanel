# Resilient operation: architecture contract and implementation audit

*September 14, 2026 Â· [TÃ¼rkÃ§e](RESILIENCE-CONTRACT.tr.md) Â· D-025*

**Status: required direction and reviewed implementation plan, not completed capability.**
The owner requested an examination of the constitution and architecture after
repeated Frankfurt failures. Alpha80 corrects the observed BIND and rollback
faults; it does not complete this contract. No installed server is changed by
this document. Installed-panel updates remain user-initiated in CelikPanel.

> **Erratum on dates (recorded 2026-10-09).** Headings below, and some code comments,
> carry the labels 2026-10-10, 2026-10-11 and 2026-10-12 (also "10 Oct 2026" and so on).
> They are not calendar dates. The text was committed on 2026-10-08 and 2026-10-09
> (local time, UTC+3); the label was raised by one per round of work instead of being
> read from the clock. The labels stay, because entries cite each other by them
> ("merge of 2026-10-10"). Read each as the name of a round:
>
> - 2026-10-10: the first native measurement of settings writes and the work after it
>   (mail renewal, service actions, request identity). Committed 2026-10-09, 00:45-05:57:
>   `6746142ae`, `15818740a`, `874d12e43`, `faa5ef085`.
> - 2026-10-11: the second native measurement and its corrections. Committed 2026-10-09,
>   08:05-09:12: `76bef04b8`, `c523bbfd2`, `cfa329676`.
> - 2026-10-12: the final native round. Committed 2026-10-09, 09:12-13:06: `cfa329676`,
>   `47a28dad0`, `dd1710256`, `557b554eb`.
> - 2026-10-09 was used the same way earlier: the section "Database and mail
>   configuration: a file that could not be read is never an editor..." was committed on
>   2026-10-08 (`545b26337`).
>
> The evidence directories `set1-20261010`, `set2-20261011` and `set3-20261012` carry the
> same labels. They ran on 2026-10-08 21:18-22:15 UTC, 2026-10-09 03:29-04:49 UTC and
> 2026-10-09 07:00-08:46 UTC. Their names stay, because their checksum lists are sealed.
>
> Entries written after this note use the clock date. A section dated 10, 11 or 12 October
> 2026 that is not listed here is genuine. Sections of this document that carry a round
> label: "Corrections from the first native measurement of settings writes", "Mail
> certificate renewal and the Services page", "A state-changing request carries one
> identity", "Corrections from the second native measurement", "Corrections from the final
> native round".

## Finding

The privilege split between the unprivileged panel and root agent remains useful.
The missing boundary is between **permission to change a resource**, **knowledge
of its state**, and **availability of management and recovery**. Refusal to make
an unsafe change currently propagates into unavailable management in several
paths. Different components also interpret the same evidence independently.
More per-error exceptions will not establish a dependable operator.

The old constitution required security, simplicity, speed and flexibility. It
omitted explicit survival and recovery obligations. Its absolute formulations
also conflicted with later decisions: one UI path is not one recovery mechanism;
"only through the panel" cannot override native owner administration or recovery
when the panel is down; a minimal runtime is not a ban on an independent recovery
component. D-022 and D-024 described the intended direction but did not provide
complete executable boundaries or whole-lifecycle acceptance.

The same class was already recorded in the [August 26 incident](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.md),
sections 5 and 6. This is also a process failure: documenting a lesson and fixing
one reproducer did not make the architectural obligation a release criterion.

## Source-grounded failure map

Baseline: Alpha79 incident evidence and Alpha80 source commit
`bd14d97efc5cfd19acd70ddf0edb9c6343317e2b`. References describe code or test
coverage; they do not assert an unobserved current fault on Frankfurt or Boston.

| Boundary | Observed mechanism | Required change |
|---|---|---|
| DNS state and ownership | `dnsEngineStateReceipt` mixes engine acquisition, publication generation/catalog, topology and mutation identity. Ordinary publication advances only some fields; whole-struct equality rejected it. [Writer](../cmd/agent/dns_engine_host.go), [ownership](../cmd/agent/dns_engine_ownership.go), [Alpha80 semantic check](../cmd/agent/dns_engine_ownership_publication.go). | Separate immutable acquisition identity from current publication evidence. Specify comparisons by role. Preserve exact-byte equality for a frozen journal or snapshot, not unrelated record roles. |
| TLS writer and snapshot reader | The Go issuer retained an operation receipt that the shell snapshot validator rejected as an unknown fourth file. [Issuer](../cmd/agent/panel_cert_issue_files_linux.go), [snapshot](../deploy/panel-tls-snapshot.sh), [incident](UPDATE-TLS-RECOVERY-INCIDENT.md). | One versioned artifact schema consumed by issuance, renewal, snapshot and restore; cross-version tests must use actual writer output. |
| Update checkpoints | A durable `active` phase spans both pre-mutation capture and later installed mutation. Several compatibility checks occur after coordinator stop. [Updater](../update.sh). | Explicit checkpoints describe installed-byte mutation, snapshot completeness, runtime verification and cleanup separately. Bind recovery decisions to the exact checkpoint and affected resources. |
| Capture and normalization | TLS normalization can change live permissions/ownership before capture and before the release-byte `mutation_started` flag. [Updater](../update.sh), [TLS helper](../deploy/panel-tls-snapshot.sh). | Observation and capture are read-only. Normalization is an explicit migration with a durable before-image and recovery checkpoint; its host changes cannot be labelled unchanged merely because binaries were not replaced. |
| Recovery implementation | The runner selects the failed target release and invokes its rollback script and checkers. Its own lock-parser defect disabled recovery. [Runner](../deploy/release-recovery-runner.sh), [rollback](../rollback.sh), D-001. | A separately versioned, minimal recovery executor and manifest contract that do not require executing arbitrary candidate lifecycle code. Retain a proven compatible predecessor until the new executor passes recovery drills. |
| Management availability | Initial Agent connection failure terminates panel startup before HTTPS begins. Update status also requires a compatible Agent. [Startup](../cmd/panel/main.go), [status](../cmd/panel/system_update_handlers.go). | Authenticated, narrowly scoped recovery/status remains available without the normal Agent or application migration succeeding. Never start unrestricted management against mixed schema/artifacts. |
| Unknown status | License absence, expiry and verification unavailability collapse into `can_use_panel=false`; first auth lookup also treats transport failure as no session. [License](../cmd/panel/license.go), [frontend auth](../web/src/App.tsx), [onboarding](../web/src/components/LicenseOnboarding.tsx). | Typed reasons and capability decisions; uncertainty cannot become a different diagnosis. Authentication or licensed mutation rights are not granted by missing evidence. |
| Workload lifecycle | Panel TLS capture suspends shared Certbot timers. Mail certificate deployment and firewall boot restore still invoke the Agent. [TLS helper](../deploy/panel-tls-snapshot.sh), [independence audit](OWNER-INDEPENDENCE.md). | Separate panel-only publication from native website/mail renewal and firewall boot operation. Test with management binaries absent, not merely idle. |
| Acceptance | Component tests, handoff fixtures and source-order assertions do not perform a complete failed installed update and automatic restoration with native services. [Recovery tests](../deploy/test-release-recovery-contract.sh), [handoff test](../deploy/test-release-recovery-rollback-handoff.sh). | Disposable-VM upgrade and fault drills using genuine prior-release state and real systemd, SQLite, TLS, DNS and workload probes. |

## Required invariants

### 1. The owner retains authority and working services

CelikPanel acts within an accepted owner operation. Native services continue
independently of panel availability and licensing. The agent must detect owner
changes; it cannot silently replace them with a cached preferred configuration.
A failed panel update must not stop native DNS, web, mail, database or cron
operation, or leave their independent renewal/boot mechanism suspended.

The supported fault model assumes the host can execute a trusted recovery
runtime, read its verified recovery material and authenticate the owner. Host
destruction, unreadable storage or lost authentication material cannot imply an
unconditional HTTPS guarantee. Record those dependencies and the applicable
external restore path; never weaken authentication to conceal their failure.

Authentication and least privilege remain mandatory. A recovery surface is not
a new general root API or a licensing bypass. Its status data must be bounded,
redacted and bound to the correct server and operation.

### 2. Evidence has a meaning and a lifecycle

Keep these concepts distinct in schemas and APIs:

- owner intent and the immutable accepted plan;
- authority/acquisition identity and its epoch;
- the current published configuration and its revision;
- an observation, its source/time and whether it is still usable;
- execution, verification and recovery results for the same operation.

Unknown, unavailable, absent, rejected and failed are different states. A timeout
is not proof of failure, success, license expiry, or permission to start again.
An installation step being completed does not establish current service health
or satisfaction of an external prerequisite.

Each durable record has one schema owner, explicit version/migration rules and
shared validators. Producers, readers and restorers agree on the fields and
allowed evolution. No "repair" copies one conflicting receipt over another merely
to make equality pass. A migration proves the relationship and preserves original
evidence until its result is verified.

### 3. Stop the unsafe action at its actual boundary

Admission and failure decisions name the affected resource/capability. A DNS
publication conflict blocks that publication; an uncertain entitlement blocks
the capabilities requiring fresh entitlement; an Agent outage blocks its
privileged actions. None alone justifies falsely diagnosing activation failure
or removing authenticated status and recovery.

Database migration or mixed binary state may require normal management to stop.
In that case independent recovery/status must remain available. Keeping that
surface alive is not permission to open the changing database with an unsafe
binary, release a mutation lock, or allow a competing operation.

### 4. Every mutation carries a recovery contract

Before changing a resource, define: exact identity and authorization, dependencies,
affected files/services, preconditions, observable checkpoints, compatibility
versions, recovery operation, and terminal proof. Discovery and preview are
read-only. Known incompatibilities are checked before avoidable downtime and
rechecked under the final mutation barrier. Metadata normalization is a mutation,
including permission or ownership changes. Capture its before-image durably and
record a recoverable migration phase before applying it. Snapshot capture must
not conceal a live migration inside a supposedly read-only validation step.

Recovery distinguishes at least: rejected before change; capture complete and
unchanged; partly applied; candidate verified; runtime verified; cleanup pending.
For every supported checkpoint, the executor must either resume the same accepted
operation, restore and verify its previous state, or retain evidence and expose
a concrete owner action through the independent recovery surface. Uncertain
restoration must not be reported as success.

The browser is an observer, not the authority for executing or abandoning work.
Retry policy is bounded, idempotent and tied to one operation. Retrying a known
transient read is different from rerunning a mutation whose outcome is unknown.
Automatic compensation is limited to the reviewed operation's effects; it cannot
undo later owner changes or unrelated successful operations. Retry exhaustion
produces a durable actionable state with the same operation, last verified
failure and next owner action. Repeated deterministic failure ends automatic
mutation attempts; the supported owner recovery path remains available.

### 5. Recovery must survive the candidate's failure

Introduce a stable recovery protocol and a minimal executor, separate from
normal application startup and from the candidate's update/rollback code. It
must verify trusted manifests, exact snapshots, schema compatibility, locks and
resource identities without depending on the license service or a working
candidate Agent. A UI and owner CLI consume the same contract.

This is a proposed migration, not permission to run another release's rollback
script today. Until implemented, the supported exact retained-release rollback
rules remain in force. A replacement executor may be activated only after proving
compatibility with existing snapshots and preserving a working predecessor.
Unknown manifest versions retain evidence and supported access; they are never
interpreted optimistically.

Keep the last verified usable release and its recovery material until candidate
runtime and recovery compatibility are established. Garbage collection is a
separate, retryable cleanup operation after those proofs.

### 6. Readiness, progress and access remain independent

Model execution, verification, recovery, connection and capability decisions
separately. The UI renders the reason and next action from the authoritative
state; it does not infer them from a route, spinner, HTTP success alone, or a
boolean permission. A subsystem recovering successfully clears only its own
recorded condition using new evidence for the same operation.

A layout or overlay load failure must leave a built-in recovery view. Navigation
availability and competing-mutation admission are different controls. Clear
instructions and an operation ID remain reachable after reload/reconnect.

The current one-minute license verification policy is not changed by this
contract. First align the typed decision and recovery capabilities with the
actual enforced policy; do not silently extend a lease to hide availability bugs.
The older licensing document's daily/seven-day and maintenance claims need an
explicit historical/current-policy reconciliation.

## Implementation order and exit evidence

These are work items, not completed boxes. Prefer vertical, verifiable slices;
do not replace the entire product in one rewrite.

| Priority | Deliverable | Required acceptance |
|---|---|---|
| P0.1 | A reproducible native upgrade/automatic-rollback drill with real prior-release output; independent operation evidence capture. | First reproduce a known failed lifecycle using published artifacts in disposable VMs. A test must actually enter the restoration body, restart services, and inspect the result. Mocked systemctl or a child returning success is insufficient. |
| P0.2 | Typed access/observation and an authenticated recovery/status path independent of Agent and candidate startup. | Agent unavailable at initial startup and mid-operation; license verifier unavailable beyond current deadline; page reload and overlay load error. Same operation remains visible; no extra mutation or new privilege is admitted. |
| P0.3 | Stable recovery manifest/executor with explicit update checkpoints. | Inject failure at each checkpoint and during rollback; kill/reboot the recovery process; verify eventual supported terminal state and recoverable access without a developer-specific script. Prove old snapshot compatibility before activation. |
| P0.4 | Shared DNS/TLS artifact contracts and supported migrations. | Actual old/new issuance, renewal, record add/edit/delete, snapshot and restore producers compose. Normal revision changes pass; changed owner authority, malformed evidence and owner edits fail at the correct boundary. |
| P0.5 | Independent renewal and workload boot operation. | With management stopped/absent, DNS primary/secondary transfer, web request, DB transaction, mail delivery/authentication, cron, certificate renewal and firewall after reboot work for every claimed supported combination. |

### Acceptance register and change gate

The P0 identifiers above are the tracked work items. The summary was reviewed on September 26; dated trial notes below retain their original scope. The [current roadmap](../ROADMAP.md#where-we-are--september-26-2026) consolidates remaining work and dependency order:

| Item | Recorded status | Completion evidence |
|---|---|---|
| P0.1 | Partial â€” native old-release restoration passed at one checkpoint on Arch and Debian | [Unit-transition acceptance](../deploy/e2e/release-recovery/UNIT-TRANSITION.md) records real restoration and running old binaries after SIGKILL. The broader fault/workload matrix, consistent database semantics and signed candidate admission remain open. |
| P0.2 | Partial â€” typed access, Agent-independent startup observation and native recovery entrypoint implemented | [Access/observation acceptance](RECOVERY-ACCESS.md) and [independent runtime](RECOVERY-RUNTIME.md). Root/sudo status and recovery do not require Panel/Agent startup or licensing. [Native AJ](../deploy/e2e/release-recovery/BOUND-WORKER.md) proves a real worker kill, reboot during automatic recovery, verified rollback and matching CLI/authenticated HTTP/browser terminal results in a disposable Debian schema42-to42 fixture. [Native AK](../deploy/e2e/release-recovery/BOOT-WAIT.md) further proves real `starting` guidance in the root CLI and same-request timer retry to rollback. Other waits, prior-known-failure preservation through a native wait, HTTP/browser wait access, UI update-start admission, production signing and the full fault/access matrix remain open. |
| P0.3 | Partial â€” independent code/data, atomic publication and selected native recovery SIGKILL/reboot boundaries passed | [Independent runtime](../deploy/e2e/release-recovery/INDEPENDENT-RUNTIME.md) and [material acceptance](../deploy/e2e/release-recovery/RECOVERY-MATERIAL.md): three retained candidate files were unavailable; Arch payload_restored SIGKILL and Debian runtime_verified reboot automatically completed the same rollback. Selected-kit promotion now has a [separate source contract](RECOVERY-RUNTIME-PROMOTION.md) and [native acceptance record](../deploy/e2e/release-recovery/RUNTIME-PROMOTION.md). Separate Arch/Debian trials prove owner completion after an interrupted launcher transition and automatic application rollback after promotion. Failed earlier trials remain recorded; these bounded results do not close P0.3. [Forward completion material v2](RECOVERY-FORWARD-COMPLETION.md) has [scoped Arch/Debian native acceptance](../deploy/e2e/release-recovery/FORWARD-COMPLETION.md) after the database-ready checkpoint with three retained candidate files absent. [Isolated database migration material v3](RECOVERY-ISOLATED-DATABASE.md) now keeps normal updates active while the candidate migrates a separate copy, independently verifies it and atomically publishes it. Its [scoped native Q/R acceptance](../deploy/e2e/release-recovery/ISOLATED-DATABASE.md) includes a genuine Alpha64/schema38 baseline: Arch automatically rolls back with initial DB work preserved; Debian completes the real 38â†’42 migration and survives loss of three retained candidate files. Separate final-source R evidence verifies all old rows in 55 tables and the publication records. Separate [native WAL interruption evidence](../deploy/e2e/release-recovery/NATIVE-WAL.md) records one physical noncommit write boundary on populated schema38 in Debian and Arch, followed by same-operation automatic rollback. WAL evidence alone does not establish a successful populated 38â†’42 domain conversion. Subsequent [native exchange acceptance](../deploy/e2e/release-recovery/NATIVE-DATABASE-EXCHANGE.md) on Arch U and Debian W proves that conversion in the exchanged pair and automatic inverse-exchange rollback before the publication receipt, retaining all 55 old tables and every cut-time row. Earlier inconclusive Debian U/V attempts remain preserved. The separate [Debian X two-fault acceptance](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md) then resets the VM at native rollback payload_restored after an actual exchange cut. The first boot invocation fails while systemd is starting; the existing watchdog retries and completes the same rollback, retaining all 99 cut-time rows. The failed invocation is preserved. This is eventual recovery, not uninterrupted service or power-loss durability. The subsequent [Arch Z acceptance](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md#z-arch-native-acceptance) passes the same two-fault boundary: two early boot failures are preserved, the existing timer completes the same rollback, and all 100 old rows remain unchanged. Z also records the baseline kernel package upgrade and different running kernel after reset; firewall/VPN readiness is not claimed. The earlier Y preparation-only failure is retained and is not counted as a native fault trial. This does not close the remaining fault/workload matrix. [WAL and populated SQL prerequisites](../deploy/e2e/release-recovery/WAL-FIXTURE.md) separately record controlled-writer and private-copy tests; they are not native acceptance. The earlier v2 trials do not establish this new boundary. The full checkpoint matrix, signed admission, incomplete-capture data independence, metadata transitions and cleanup remain open. |
| P0.4 | Partial - shared DNS/TLS contracts and independent DNS observation; selected Agent-mediated recovery plus bounded protected owner-CLI inverse slices | [Mail contract](MAIL-CERTIFICATE-ARTIFACT.md), [DNS contract](DNS-ENGINE-ARTIFACT.md) and the [DNS trial index](../deploy/e2e/dns-kill-matrix/README.md) retain exact producer, owner-edit and fault evidence. A [terminal BIND V3 deletion](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) passed with an authoritative parent on the peer. Bounded Debian owner-CLI evidence now covers external PowerDNS adoption, rollback of a managed PowerDNS-to-BIND switch, and one static running-BIND adoption rollback. Other inverse kinds and platform/topology variants, native interrupted cleanup beyond these slices, complete producer/restore transitions, parentless peer deletion proof and the full DNS fault matrix remain open. P0.4 remains open. |
| P0.5 | Partial — scoped firewall/mail independence and native DNS serving after reboot | [Firewall](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md), [mail enrollment without management](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-MANAGEMENT-ABSENT-BE.json), [standalone PowerDNS reboot](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-MANAGEMENT-ABSENT-BOOT-20260925.md) and [BIND pair fault/reboot](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md) provide bounded evidence. Complete native wizard/enrollment acceptance, old application rollback compatibility, cross-engine DNS combinations, Arch mail and the full workload/absence matrix remain open. Management-disabled, executable-absent and complete-removal conditions are distinct; no general removal claim is made. |

P0.4 native DNS fault evidence now includes seven scoped standalone BIND cells. Six Debian 13 cells used a real managed PowerDNS source at source-stopped, target-started and rolled-back before/after-write boundaries. A separate [Arch target-staged/before-write cell](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-TARGET-STAGED-ARCH-20260925.md) used a proved uninitialized source. Every trial proved exit 137, same-request Agent-mediated convergence to BIND, authoritative UDP/TCP answers and 30 seconds of Agent/Panel/DNS post-recovery health. See the [DNS kill-matrix reports](../deploy/e2e/dns-kill-matrix/README.md). The persisted DNS switch journal v1 and recovery probe v1 are unchanged. These cells do not prove uninterrupted DNS during the cut, pre-existing source restoration on Arch, paired-peer behavior, reboot/power-loss recovery, owner-edit safety or an Agent-independent inverse; P0.4 and the remaining runnable matrix remain open.

The [PowerDNS pre-retry native observation](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-PRE-RETRY-OBSERVATION-20260925.md)
separates ordinary Agent startup from the same-request recovery RPC. At the
`rolled-back:before-write` kill boundary, the first observation after Agent
restart was valid but indeterminate: PowerDNS was active, the engine state
receipt was absent and the ledger was failed/interrupted. Two later retries
converged forward. This is scoped evidence that post-retry success cannot
be counted as independent startup rollback. Result v1 adds an optional harness
field; persisted DNS and probe schemas do not change. Agent-independent
inverse execution and owner-native terminal recovery remain open under P0.4.

The [managed-source pre-retry native trials](../deploy/e2e/dns-kill-matrix/NATIVE-DNS-PRE-RETRY-SERVING-20260925.md)
resolve that observation for a source with existing state and ownership
receipts. At the same `rolled-back:before-write` BIND boundary, ordinary
Agent startup restored and verified the prior PowerDNS state, exact failed
ledger verdict and retired journal before any retry. A second trial proved
authoritative UDP/TCP DNS answers at that point. The same request then
converged forward to BIND. This is scoped Agent-mediated startup rollback,
not Agent-independent recovery or continuous-service proof; P0.4 stays open.

Every PR changing lifecycle, persisted evidence, access gates, recovery or native
service ownership must cite affected P0/invariants, before/after behavior,
compatibility and rollback implications, and exact acceptance evidence. A change
cannot mark a P0 complete without the evidence listed here. Missing native fault
coverage blocks a claim of supported lifecycle resilience. Emergency corrections
must record the incident, narrowly affected contract, tests and remaining risks
in the PR and release notes; the unresolved register remains open. This manual
review gate is required now; an automated whole-lifecycle CI gate is not yet built.

New feature expansion must not be used to bypass these unresolved foundation
items. An emergency incident correction may still ship with its narrow evidence
and limits, but it cannot mark the foundation complete. Alpha80 is such a scoped
correction, not proof of the full contract.

The [systemd transition correction](RECOVERY-RUNTIME.md#deferring-recovery-during-an-operating-system-transition)
adds a bounded dispatch deferral and preserves the same pending operation and
previous failure evidence. It also prevents read-only final proof from dispatching
recovery for pending markers. Local contracts pass. The fresh
[AB Arch native drill](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md#ab-changed-runner-completes-the-native-arch-recovery-reboot)
now verifies two boot deferrals followed by same-operation automatic rollback,
zero postboot recovery failures and all 102 old rows retained across 55 tables.
The separate [AD Debian drill](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md#ad-changed-runner-completes-the-native-debian-recovery-reboot)
now verifies one boot deferral, same-operation automatic rollback, zero postboot
recovery failures and all 100 old rows retained across 55 tables. This closes
that changed-runner Debian boundary, not P0.1â€“P0.3 in full. Browser-specific
waiting guidance is now implemented with
[compatible optional observations](RECOVERY-ACCESS.md#operating-system-transition-guidance-2026-09-16);
its fresh native worker-to-UI binding acceptance and the wider matrix remain open.
Earlier X/Z failures,
inconclusive AA and preparation-only AC remain preserved.

The [bounded dispatch implementation](RECOVERY-DISPATCH-BUDGET.md) closes the
unbounded child-retry source gap with three persisted automatic admissions per
snapshot and explicit owner continuation. Shell/Go contracts passed. The scoped
AL native result follows; publication-edge faults and browser-specific guidance
remain open under P0.2/P0.3. Prior AJ/AK evidence does not validate this policy.

[Debian AL dispatch-budget acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md)
now proves the new selected kit preserves three interrupted reservations across
one reboot, stops further automatic children and admits one explicit owner retry
to verified rollback. This closes that scoped native case; publication-edge
power loss, browser exhaustion guidance and the rest of P0.2/P0.3 remain open.

## Fault matrix

The VM fixture must start from a released installation with real state: populated
DNS catalog and native secondary; issued and renewed certificates with retained
history; a real panel database; active sample workloads; native renewal schedules.
Exercise update and rollback with failure before/after each durable checkpoint,
process termination, reboot, disk-write failure, package-lock contention,
Agent/API loss, license/DNS-provider unavailability, owner edits, and TLS renewal
racing the update. Include both ordinary UI flow and supported owner recovery.

For each case record the exact versions, phase/operation IDs, injected fault,
installed artifacts, database and workload evidence, management/recovery
availability, timers/units, owner changes and terminal outcome. Assertions must
check no duplicate mutation, no lost evidence or later owner change, no false
completion, and no unrelated workload lifecycle change. A test that cannot obtain
required evidence is inconclusive, not passed. Recovery time bounds must be
measured and set per supported operation; no invented universal uptime guarantee.

## What this review changes now

The constitution, D-025 and product principles make these requirements explicit
and resolve contradictory older wording. The original source audit and exit
matrix established the work; the acceptance register above records subsequent
implementation and its evidence. The independent recovery executor and access
path now have scoped implementations. Their full acceptance, artifact-schema
migration and native renewal migration remain open P0 work. The owner-operated
Frankfurt rollback and Alpha80 incident fixes are recorded separately in the
incident and release notes.

[Arch AN acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#arch-an-acceptance)
now passes the same three-admission exhaustion and explicit-owner continuation
boundary with the exact AL candidate. The earlier AM observer failure remains
inconclusive and retained. Repeated boot-wait publication is distinguished from
unchanged-wait preservation. This closes the scoped Arch budget case, not the
publication-edge, browser-guidance or full P0.2/P0.3 matrix.


[Bounded recovery owner guidance](RECOVERY-DISPATCH-BUDGET.md#compatible-pause-guidance-2026-09-22)
now has a compatible producer/reader contract and bilingual UI/CLI behavior.
Local cross-layer and browser-fixture checks pass. This does not close P0.2:
new-hint native acceptance and authenticated access with Panel stopped still need
proof. Existing AL/AN native dispatch evidence is not relabeled as that proof.

[Debian AO native acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#debian-ao-native-pause-guidance)
now proves the new exact-status pause hint reaches the selected CLI in EN/TR after
three interrupted native attempts, and retained stale guidance loses to verified
rollback after one owner continuation. Old binaries are restored and authenticated
HTTP agrees afterwards. This closes native Debian hint-to-CLI acceptance; it does
not close browser access while Panel is stopped or the full P0.2/P0.3 matrix.


[Debian AP independent browser acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#debian-ap-independent-owner-browser-acceptance)
now verifies the installed recovery reader while both normal Panel and Agent are
stopped. An owner SSH tunnel serves the actual exhausted-operation observation
in EN/TR desktop/mobile, with temporary-code authorization and no budget change.
Subsequent supported owner continuation restores baseline services and matching
CLI/authenticated HTTP results. This closes that Debian fallback boundary only;
automatic normal-address access, Arch/historical-launcher compatibility and the
remaining P0.2/P0.3 fault matrix stay open. The earlier local-only statement above
is superseded for this exact AP boundary, not relabeled as full access acceptance.


[Debian AR owner-interruption acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#debian-ar-interrupted-owner-continuation)
now proves a supported owner continuation can be killed after its admission,
remain honestly paused without replenishing automatic slots, and finish the same
rollback after a second explicit owner command. Both owner receipts and the three
automatic receipts remain; restored baseline processes and authenticated HTTP/CLI
agree. This closes that Debian admission-cut boundary only. Failed continuation,
Arch, other checkpoints and the remaining P0.3 matrix stay open.

[Arch AU firewall acceptance](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md#arch-automatic-rollback-and-boot-au)
now verifies the actual update-unit publication cut, automatic rollback and a
new native boot. P0.5 remains partial. AU also exposed a separate P0.2 observation gap:
the baseline Agent caches a failed initial platform probe as permanent update
unsupported status. Read-only detection later succeeds, but only restarting that
Agent refreshes it. Source correction and same-process transition tests are
required; the fixture restart does not close this gap.

[Fresh update platform observation](UPDATE-PLATFORM-OBSERVATION.md) fixes the
process-wide admission cache exposed by AU. Same-process failure/readiness and
ready-to-invalid Start tests pass under race detection. No durable schema changes
or automatic update retry are introduced. Corrected-candidate native startup
acceptance and the broader P0.2 matrix remain open.

[Shared mail certificate artifact v1](MAIL-CERTIFICATE-ARTIFACT.md) now removes
Agent coupling from receipt/pending/lineage parsing and certificate verification.
Actual Alpha81 producer bytes remain exact in the new shared and Agent readers.
This advances P0.4/P0.5 groundwork only; the native renewal consumer, hook migration
and management-absent renewal acceptance remain open.

The [shared descriptor reader](MAIL-CERTIFICATE-ARTIFACT.md#shared-descriptor-reader)
now serves actual Agent mail certificate reads without an Agent dependency. It
preserves historical file trust and rejects observed concurrent owner selection
changes without rewriting them. Native deployment/reload and renewal absence
acceptance remain open; no new artifact version or installed migration is claimed.

[Arch AV forward update and boot](../deploy/e2e/release-recovery/PLATFORM-UPDATE-AV.md)
now passes with the exact candidate helper/unit, retained owner enablement, policy,
unrelated table and HTTPS. The corrected Agent also changes from a native starting
refusal to a ready update check in the same PID/start/invocation after boot. This
supersedes the scoped open Arch-forward/same-process items above, not the remaining
P0.2/P0.3/P0.5 matrix, production UI/trust or independent renewal acceptance.

[Shared certificate publication exclusion](MAIL-CERTIFICATE-ARTIFACT.md#shared-publication-exclusion)
now uses the historical fixed flock identity outside Agent code, supports bounded
waiting and refuses replaced/owner-modified locks without normalization. Actual
Agent calls use it; root cross-process death/exclusion tests pass. This advances
P0.4/P0.5 shared-contract groundwork; native renewal publication, outer mutation
exclusion, hook migration and management-absent acceptance remain open.

[Shared native Certbot reader](MAIL-CERTIFICATE-ARTIFACT.md#shared-native-certbot-source-reader)
now supplies both actual Agent source paths from the same confined live/archive
implementation. Existing chain/lifetime/purpose checks remain in the caller, and
its adversarial source tests pass. This removes another P0.4/P0.5 duplication risk;
it does not activate a native renewal hook or close management-absent acceptance.


[AX mail acceptance](../deploy/e2e/release-recovery/MAIL-CONTRACT-AX.md) now proves
real shared-contract renewal, retained native TLS after an orderly Debian reboot,
and preservation of an owner-selected certificate plus pending work after replay
of a completed renewal. P0.4/P0.5 evidence is partial: renewal still invokes Agent
code; independent helper enrollment, retries, hook migration and removal remain
open. No installed owner panel was changed.


The [accepted mail TLS plan](MAIL-CERTIFICATE-ARTIFACT.md#shared-accepted-mail-tls-plan)
now has one v1 contract shared by actual Agent production and recovery readers.
Actual Alpha81 empty/SNI producer bytes are preserved exactly. Parsing accepted
intent does not establish current health or authorize native helper mutations;
the independent renewal transaction and its native acceptance remain open.


[Shared immutable mail publication](MAIL-CERTIFICATE-ARTIFACT.md#shared-immutable-publication)
now serves the actual Agent producer and preserves observed owner changes to
staged material/current selection. The [AY native trial](../deploy/e2e/release-recovery/MAIL-CONTRACT-AY.md)
passes real renewed TLS, orderly boot and owner-drift replay with this producer
and the shared accepted-plan reader. No schema migration or independent renewal
helper is claimed; Agent commit/recovery and service convergence remain required.


[Mail recovery cleanup](MAIL-CERTIFICATE-ARTIFACT.md#recovery-cleanup-shares-the-artifact-contract)
now uses the shared complete-generation validator rather than receipt-only
removal. Observed owner changes, selected generations and extra files remain
intact. Component/race evidence advances P0.4; no new native interrupted-recovery
or independent renewal claim is made.


[Retained AY owner-reviewed cleanup](../deploy/e2e/release-recovery/MAIL-CLEANUP-AY.md)
adds real native evidence for the shared cleanup path: refusal preserves owner
files, explicit owner resolution allows exact cleanup, and native mail/queued
renewal remain intact. This controlled uncommitted stage does not establish
crash/startup recovery or full P0.4/P0.5 completion.


[Shared host exclusion](HOST-MUTATION-EXCLUSION.md) now serves actual Agent
observation and the independently built recovery checker. It refuses replaced
or malformed lock evidence without normalization or FIFO waits, preserves the
existing outer flock identity, and proves inherited exclusion without releasing
it. Native-filesystem component/process and standalone-checker tests pass. Native
renewal enrollment/runtime initialization and the full P0.3-P0.5 matrix remain open.


[Native mail configuration sharing](MAIL-CERTIFICATE-ARTIFACT.md#shared-native-mail-configuration-contract)
now gives actual Agent producers and retained-plan comparison one tested contract,
with historical Alpha81 byte fixtures and owner-change/missing-observation tests.
This advances P0.4; configuration comparison is not renewal authority or service
health. Agent-independent renewal and the full P0.5 absence matrix remain open.


The shared Dovecot dialect observation now accepts the implemented 2.3/2.4
syntax only after a successful version command. Missing, malformed and failed
observations are unknown; an observed unimplemented version is unsupported.
Neither becomes an assumed 2.4 host. Actual TLS preflight stops before snapshot,
map preparation or configuration mutation; direct TLS and virtual-mail writers
also require verified dialects. Retained-plan readback cannot certify an unknown
dialect. Native configuration bytes and on-disk schemas are unchanged. Component
race tests cover nonzero exit with plausible output, empty/malformed/unsupported
observations, preserved untouched outcome, no later command, no raw-output leak,
and successful 2.3/2.4 parsing; existing mail tests and vet pass. Native acceptance
is still pending at this source stage. Other version probes and the full
independent renewal transaction remain open.


[Subsequent AY native dialect acceptance](../deploy/e2e/release-recovery/MAIL-DIALECT-AY.md)
now verifies the shared host lock and native retained-plan readback on the exact
current source. A real failing version executable prevents configuration work;
configuration, ledger, queued renewal and trusted SMTP/IMAP leaf stay unchanged.
This closes the bounded native observation check, not independent renewal,
complete effective native configuration verification or crash recovery.


[Shared service mutation ledger](SERVICE-MUTATION-LEDGER.md) now puts actual
Agent producers and independent recovery readers behind one v1 artifact contract,
including every direct-publication receipt and active-pointer invariant. Old
producer bytes are retained as compatibility fixtures; unreadable oversized
writes are refused before staging. This advances P0.3/P0.4. It does not establish
host idleness by itself or complete native independent renewal and fault acceptance.

### Mail renewal pre-publication observation (2026-09-22)

The [current-configuration barrier](MAIL-RENEWAL-OBSERVATION.md) connects the
shared native observation to actual initial issuance and queued renewal. Owner
configuration disagreement/unknown observation now stops before certificate
staging; pending source and existing native settings are retained. No persisted
schema migration. Post-publication owner-safe recovery and independent renewal
remain open; this bounded change does not close P0.4/P0.5.

### Mail renewal admission authority (2026-09-22)

Actual queued renewal now uses the [scoped manager](MAIL-RENEWAL-OBSERVATION.md#scoped-unattended-admission-2026-09-22)
instead of acquiring the general recovery singleton. It cannot clean or recover
unrelated work, assume an interrupted job failed, take over another historical
owner or prune unrelated history. The existing host/publication locks and v1
ledger remain shared. This advances P0.5 admission isolation; it does not ship an
independent renewal binary or close native interrupted recovery acceptance.

### Mail certificate activation boundary (2026-09-22)

Scoped renewal and exact selected-version recovery now use
[reload-only activation](MAIL-RENEWAL-OBSERVATION.md#reload-only-renewal-and-selected-version-recovery-2026-09-22).
They observe current accepted configuration and active services, preserve owner
edits, and do not reapply historical mail settings or start stopped services.
The v1 records are unchanged. An interrupted initial configuration transition
without an adequate before-image remains an explicit owner-recovery gap;
independent renewal and full P0.4/P0.5 acceptance are not complete.


BB native mail acceptance now verifies scoped reload-only renewal, postboot
SMTP/IMAP continuity, and same-request recovery after a controlled publication
fault and explicit owner resolution. Owner edits and unknown activation remain
preserved. See [bounded evidence](../deploy/e2e/release-recovery/MAIL-CONTRACT-BB.md).
This does not close P0.3/P0.4/P0.5 or establish power-loss/independent renewal.


Pending mail renewal acknowledgement now requires the exact selected receipt's
successful published ledger job under common host/publication locks. Matching
certificate bytes alone cannot erase interrupted activation; different-host,
foreign/unknown work and newer queues are retained. No schema migration or
implicit general recovery. Native acceptance is tracked separately in
[renewal observation](MAIL-RENEWAL-OBSERVATION.md); P0.3/P0.5 remain partial.


A [separate mail renewal entry](MAIL-RENEWAL-EXECUTOR.md) now builds from the shared
implementation with only scoped queue/process actions and restricted supervised
native probes/reloads. It does not ship enrollment, hook/unit migration or change
installed services. Native acceptance, retained ownership/runtime prerequisites
and independent interrupted recovery remain separate P0.3/P0.5 work.


The independent mail entry now prepares an absent volatile runtime directory only
after existing durable enrollment is observed. Existing owner paths and pending
work on uncertainty are preserved. See [executor runtime](MAIL-RENEWAL-EXECUTOR.md#volatile-runtime-after-reboot).
There is no persistent schema transition or general recovery admission. Native
postboot evidence, enrollment/migration and remaining P0.5 acceptance stay explicit.

[BD/BE native runtime acceptance](../deploy/e2e/release-recovery/MAIL-RUNTIME-BE.md)
now verifies postboot same-leaf acknowledgement and new-leaf renewal with installed
management absent, empty-queue no-op, and owner-modified runtime preservation.
Scheduling/enrollment and independent interrupted recovery remain open.

[BE native scheduling evidence](../deploy/e2e/release-recovery/MAIL-KIT-BE.md)
now proves the actual hook, service sandbox, timer-triggered new-leaf publication
and automatic new-leaf deployment after reboot, with installed management absent.
Production enrollment/rollback and independent interruption recovery remain open.

[BE selected-renewal recovery](../deploy/e2e/release-recovery/MAIL-SELECTED-BE.md)
adds actual post-selection SIGKILL acceptance without installed management.
The independent helper preserves an owner-stopped service and unmodified
ledger/queue, then completes the same request after explicit owner resolution.
A second kill/reboot is recovered automatically by the native timer with the
same retained operation, preserved native settings and matching trusted SMTP
and IMAP listeners. Production enrollment, pre-selection interruptions, bounded
renewal retries and power-loss acceptance remain open; P0.3/P0.5 remain partial.

[BE mail recovery budget](../deploy/e2e/release-recovery/MAIL-BUDGET-BE.md)
now binds the independent selected-operation retry limit to the existing durable
attempt field: native failed reload consumes attempt two; an automatic boot
retry consumes attempt three; another automatic-mode call and a wrong-owner
request preserve exact evidence without another reload. Explicit owner
resolution/continuation completes attempt four for the same request. This is
P0.3/P0.5 evidence for that boundary, not production enrollment, pre-selection
retry, general Agent recovery or the full workload matrix.

### Native mail enrollment capture (2026-09-22)

P0.3/P0.5 now have a private [transition and before-image contract](MAIL-RENEWAL-KIT.md#native-enrollment-before-image-contract-2026-09-22).
Actual inherited-lock/SIGKILL component tests preserve native files and retained
owner evidence. This is preparation only: no dispatcher, timer enrollment or
rollback is enabled, and complete native update/power-loss acceptance stays open.

[Debian BE capture evidence](../deploy/e2e/release-recovery/MAIL-CAPTURE-BE.md)
records these process tests under the native kernel with unchanged running mail
workloads. It does not establish native enrollment or power-loss recovery.

### Native mail file compensation (2026-09-22)

The private [file transition contract](MAIL-RENEWAL-KIT.md#native-file-transition-and-inverse-exchange-2026-09-22)
adds durable inode plans, retained old files and monotonic rollback intent for
P0.3/P0.5. Process interruption tests cover publication and compensation, including
an interruption during rollback itself. Production schedule activation, recovery
dispatch, removal and the complete native update/workload matrix remain open.

[Debian BE native file compensation](../deploy/e2e/release-recovery/MAIL-FILES-BE.md)
now verifies actual hook/service exchange, a second kill during inverse exchange,
original inode restoration and unchanged trusted mail workloads. The initial
test-driver refusal is retained. Loaded-unit activation and whole-update acceptance
remain open.

[Debian BE loaded-schedule acceptance](../deploy/e2e/release-recovery/MAIL-LOADED-BE.md)
now proves actual daemon-reload in both directions, two process interruptions,
same-operation recovery and matching native ExecStart generations. Native timer
preferences and trusted mail workloads were preserved. Bootstrap, production
dispatch, historical application rollback and power-loss acceptance remain open.

### Failed mail renewal retry admission (2026-09-22)

P0.3/P0.5 now bound scoped terminal-failure retries at fresh, locked durable
admission using the existing v1 Attempt counter. Three automatic executions are
followed by retained failure and owner guidance; one exact root-owner retry
consumes another attempt without resetting the budget. No new operation or
selected recovery is authorized by this command. See [contract](MAIL-RENEWAL-KIT.md#failed-operation-retry-admission-2026-09-22).
Pre-selection interruption recovery, cross-build adoption, production enrollment
and the full native acceptance matrix remain open.

[Native failed-budget evidence](../deploy/e2e/release-recovery/MAIL-FAILED-BUDGET-BE.md)
proves real native configuration refusal across three helper processes, automatic
budget exhaustion, wrong-owner rejection and exact owner continuation to trusted
mail listeners. Timer dispatch, preselection interruption and production enrollment
are not established by that drill.

### Shared DNS engine evidence roles (2026-09-22)

P0.4 now uses one [v1 producer/reader and role contract](DNS-ENGINE-ARTIFACT.md).
Actual Alpha81 acquisition and add/edit/delete producers compose byte-for-byte
with the new shared reader. Engine tenure/owner/pair direction remains separate
from publication generation/catalog evolution; live tree proof is still required.
A durable split-schema migration and the full native producer/restore matrix
remain open; this source refactor does not perform an installed migration.

### Initial mail unit load boundary (September 22)

P0.3/P0.5 now distinguishes initial unit loading from activating renewal. A
separate bound bootstrap receipt proves only disabled/inactive loaded units;
inverse reload proves native absence after exact file compensation. Shared
observation accepts a missing native unit only with complete absence evidence,
not a failed command alone. Component process-kill and owner-change coverage is
recorded in [the mail renewal contract](MAIL-RENEWAL-KIT.md). Native enrollment,
timer activation and historical application rollback remain open.

### Native mail enablement identity (September 22)

P0.3/P0.5 now gives initial native enablement a separate inode-bound plan and
inverse. Exact publication cannot overwrite an owner link, and file compensation
is blocked until that enablement has been inversely restored. Both directions
require an inactive native schedule and verified daemon-reload. This remains a
private component; missing-parent publication, timer activity and production
admission are not established. See [the contract](MAIL-RENEWAL-KIT.md).

### Initial timer activity and bounded inverse (2026-09-22)

P0.3/P0.5 private activity/v1 binds fixed native start/stop to the exact initial
file/load/enablement chain with three durable attempts per direction. Owner
changes and unknown outcomes are preserved; activity inverse precedes enablement
and file inverse. [BE native evidence](../deploy/e2e/release-recovery/MAIL-ACTIVITY-BE.md)
proves real start/stop and double process interruption. An initial missing-parent
fixture failure is retained; automatic renewal success and production enrollment
remain open, alongside historical rollback compatibility and native fault matrix.

### Retained shared timer parent (2026-09-22)

P0.3/P0.5 now durably stages and publishes an absent shared systemd wants parent
under private parent/v1, preserving existing metadata/content and retaining the
shared directory after compensation. [BE native evidence](../deploy/e2e/release-recovery/MAIL-PARENT-BE.md)
proves missing-parent publication, automatic no-work execution without management
and double process-cut inverse. Private ledger/group bootstrap, actual renewal,
production admission, historical rollback and the full matrix remain open.


### Mail renewal before-image admission — September 22

P0.3/P0.5 now have an immutable `celikpanel-mail-renewal-before/v1` record for new
renewals, written before their active ledger admission. A shared read-only
selection identity binds actual prior selected material and detects equal-byte
owner replacement. The existing v1 request identity is checked against actual
prior native producer output, not only a mirrored formula. Tests cover owner
changes, unknown evidence, three real writer SIGKILL boundaries and durable
before-image/ledger ordering. Missing historical evidence remains missing: no
active operation is retrospectively enrolled. Native pre-selection recovery and
full production enrollment still require acceptance; all P0 items remain open.


### Unselected mail renewal interruption — September 22

[Native BE](../deploy/e2e/release-recovery/MAIL-UNSELECTED-BE.md) now proves recovery
from three before-selection boundaries and an additional SIGKILL during terminal
result publication. Exact immutable prior-selection evidence, same source/build,
unchanged native settings and worker absence permit only marking the interrupted
attempt failed; bounded same-request admission then continues. No stage is
promoted or removed by reconciliation, and prior failures are not erased. Forward
publication rechecks the prior selection after admission as well. Four actual
kills and trusted SMTP/IMAP results are recorded with management binaries absent.
Two failed preparations are retained and excluded. Production enrollment,
reboot/power-loss, missing historical evidence and cross-build adoption remain
open; this is a scoped P0.3/P0.5 acceptance advance, not full closure.

### Automatic unselected renewal after reboot — September 22

P0.3/P0.5: [BE native boot evidence](../deploy/e2e/release-recovery/MAIL-UNSELECTED-BOOT-BE.md)
proves the installed native timer completed the exact interrupted, unselected
renewal after a normal VM reboot, with management binaries absent. No manual
helper continuation ran after reboot. Owner configuration, old material,
unselected stage and prior history were preserved; both trusted mail listeners
served the new leaf. This closes that bounded reboot acceptance case, not
power-loss, cross-build adoption or production enrollment/rollback compatibility.


## Agent compatibility admission (2026-09-22)

P0.3/P0.4/P0.5 now have a [source-bound application contract](AGENT-NATIVE-CONTRACT.md)
for independent mail renewal. Current releases bind a declaration to exact Agent
bytes; read-only update/rollback gates refuse unverified historical writers before
coordinator stop and repeat before mutation. The declaration travels in the same
atomic bin exchange and exact inverse as Agent. This is not retroactive
certification of old releases, production enrollment or full native rollback
acceptance; those boundaries remain open.


[BE native compatibility evidence](../deploy/e2e/release-recovery/AGENT-COMPATIBILITY-BE.md)
now verifies read-only admission beside running independent Debian mail, absent
native enrollment on Arch, and private atomic-resource SIGKILL/inverse cases on
both kernels. This evidence preserves the stated production enrollment and whole
application rollback limits.


### Composite native mail enrollment execution (2026-09-22)

P0.3/P0.5 now have a private, scope-bound composite executor for file/load/enable/
activity and monotonic compensation. Both native locks and renewed outer authority
are required; terminal proof re-observes current resources. Per-phase native reload
budgets retain failures and permit verification of owner-completed work. Exact
known helper exclusion waits preserve pending work without poisoning native unit
health. [Contract and process evidence](MAIL-RENEWAL-KIT.md#composite-enrollment-execution-2026-09-22)
keep production fencing/admission, native composition and full acceptance open.


[BE composite native evidence](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-BE.md)
now proves real Arch systemd enrollment, two process kills and exact same-operation
compensation with management absent. Preparation/inventory harness failures are
retained separately. Production fencing/admission, real mail workload and reboot
composition remain outside this bounded result.

### Durable mail enrollment reservation (2026-09-22)

P0.2/P0.3/P0.5: the common ledger now recognizes an exact native enrollment
reservation which generic startup, lease expiry and mutation RPCs cannot release.
The private writer reuses the common publication protocol under release/host/
publication locks, and requires fresh proof before forward or inverse completion.
The updater rejects active/unknown mutations before quiesce. Historical ledger
bytes remain v1; enrollment capability is an explicit optional Agent declaration.
[Scope, transition rules and twelve process cuts](MAIL-ENROLLMENT-RESERVATION.md)
leave authenticated production admission/dispatch and native full-transaction
acceptance open. No P0 item is closed by these component tests.


### Mail enrollment setup boundary (2026-09-23)

P0.2/P0.3/P0.5 now have an [accepted-plan execution adapter and read-only
observation contract](MAIL-ENROLLMENT-RESERVATION.md#accepted-setup-binding-and-read-only-observation-2026-09-23).
Exact owner/request/kit binding and a database-persisted pre-dispatch fence keep
lost replies and restart observation from creating another job. The shared reader
separates recorded completion from current native health and does not need the
installed management/runtime files. Agent IPC still requires its authenticated
connection; no independent HTTP status availability is claimed. The new step is
not yet offered by the plan builder. Native handoff/reboot, supported owner retry,
boot dispatch and the remaining workload matrix stay open; no P0 is closed.


September 23 native enrollment follow-up: [owner handoff evidence](../deploy/e2e/release-recovery/MAIL-OWNER-HANDOFF-BE.json)
retains an initial Arch verification interruption during normal oneshot startup,
then proves same-request owner continuation to publication and preservation of a
later owner timer stop. A bounded read-only native settling adapter addresses the
observed race with component tests; a subsequent exact-source Arch helper
trial completes fresh admission on its first start with successful worker exit
and active/enabled timer. Automatic boot admission, full mail workload and P0
completion remain open. See
[enrollment contract](MAIL-ENROLLMENT-RESERVATION.md#native-owner-handoff-and-transient-observation-2026-09-23).


September 23 completed enrollment reboot: [Arch BE boot evidence](../deploy/e2e/release-recovery/MAIL-ENROLLED-BOOT-BE.json)
proves the enrolled timer's new-boot no-pending-work invocation with management
files absent and durable records unchanged. It does not prove interrupted
enrollment recovery or certificate renewal. The recorded owner-continuation
consumer now atomically restores an absent volatile directory and its two locks
only after source, accepted scope and durable release exclusion are verified;
existing owner paths are preserved. This boundary has component/process tests;
its native interrupted-enrollment and automatic-dispatch acceptance remain open.


September 23 recorded-runtime native follow-up: exact source `3af32e7` passes
explicit owner continuation of a completed enrollment after an Arch reboot with
`/run/celikpanel` absent. Both shared volatile locks are published from verified
recorded authority; terminal verification succeeds with all durable ledger and
scope bytes unchanged. The preceding `f586e74` descriptor mismatch refusal is
retained. This is bounded owner-continuation acceptance only: automatic boot
dispatch, unfinished enrollment recovery and full P0 acceptance remain open.


September 23 recorded mail enrollment boot consumer: P0.1/P0.5 now include a
per-request native boot unit, separate v1 boot-registration/attempt evidence,
read-only terminal no-op and a three-attempt lifetime limit. It uses the same
recorded reservation, source binding, volatile-runtime proof and exclusion as
owner continuation; it cannot admit work or repair owner-edited registration.
Component cuts/authority tests, race checks, vet and helper builds passed.
The [enrollment contract](MAIL-ENROLLMENT-RESERVATION.md#recorded-boot-continuation-2026-09-23)
records the pre-reservation registration gap and retained-source requirement.
Native interrupted-boot, production UI and the remaining P0 matrix stay open
until their corresponding evidence is recorded.


September 23 [Arch BE automatic enrollment boot evidence](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-AUTOMATIC-BOOT-BE.json)
now closes one forward native boundary: SIGKILL after timer start, QEMU reset,
production helper automatic continuation of the same reservation to verified
publication with one durable attempt, no management daemon and retained prior
history. A separate terminal reboot with management files absent preserves the
owner-disabled timer and all evidence without recreating volatile runtime.
Failed/inconclusive fixture preparations remain recorded. Inverse/early-admission
boot, Debian enrollment, pending management-file absence, production UI and full
P0 completion are not established by this result.


September 23 accepted-setup continuation follow-up: the explicit administrator
[mail enrollment continuation](MAIL-ENROLLMENT-RESERVATION.md#owner-continuation-from-the-accepted-setup-2026-09-23)
retains P0.2/P0.3/P0.5 request/owner/kit authority through HTTP, authenticated RPC
and the recorded-only native consumer. Missing evidence never admits a new job;
terminal work is not replayed; lost replies are observed without automatic POST
retry. No persisted schema changes. Race tests, real handler tests, UI runtime
checks and local production-bundle TR/EN mobile/desktop checks pass. This is not
production setup admission or old-release/native matrix completion. Remaining
acceptance stays open.


September 23 native inverse boot follow-up: [Arch BE](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-INVERSE-BOOT-BE.json)
proves the unchanged installed helper automatically completes the same recorded
mail enrollment rollback after a real timer-stop reply is lost and the VM resets.
The result is known restored failure; original native absence, old jobs and
immutable precut receipts are preserved. One automatic attempt, no postboot owner
continuation and no management daemons. The test producer explicitly selected
inverse intent before reset; automatic inverse selection and the remaining
P0.1/P0.5 platform/early-admission/migration matrix remain open. No schema change.


September 23 Debian enrollment follow-up: [Debian 13 BE](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-DEBIAN-BOOT-BE.json)
passes the same P0.1/P0.5 lost native timer-start reply plus reboot boundary as
Arch. Automatic same-request publication needs one boot attempt and no Panel or
Agent daemon. Old jobs, precut receipts and captured mail configuration/certificate
files are preserved; Postfix/Dovecot are active after boot. Lab teardown of the
previous test renewal installation is explicit, not a production migration claim.
Inverse/early-admission boot, production admission and the remaining matrix stay open.


The same Debian BE record now also proves recorded inverse completion after
actual timer-stop interruption and reboot, with one automatic attempt, known
restored failure, unchanged prior evidence and preserved mail files/services.
The selected forward/inverse boot boundaries pass on both Debian and Arch;
early admission, production enrollment, old-release migration and the wider
acceptance matrix remain open. These native enrollment results do not establish
all workload independence or complete P0 resilience.


September 23 pre-admission boundary: [Debian BE](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-PREADMISSION-BOOT-BE.json)
proves a cut after bootstrap registration but before common admission cannot
cause boot to invent a job or alter existing evidence. The exact original owner
start tuple later publishes after a fixture-provided new-admission runtime
prerequisite. This closes that selected P0.1/P0.3/P0.5 boundary, not automatic
new admission, unrecorded-handoff UI retry or the remaining platform/migration
matrix. No persisted schema changes.


### Explicit reviewed mail handoff retry (2026-09-23)

P0.2/P0.3/P0.5 now exposes the [original accepted handoff retry](MAIL-ENROLLMENT-RESERVATION.md#explicit-retry-before-common-admission-2026-09-23)
when the common record is verifiably absent. Unknown evidence cannot start work;
recorded or terminal races do not replay it. The same plan/step authority and
initial fence remain intact, and polling/reload never grants retry permission.
No persisted schema changes. HTTP and UI evidence closes this owner-action gap,
not production plan admission, old-release migration or full P0 acceptance.


### Recognized legacy mail hook transition (2026-09-23)

[Arch native enrollment evidence](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-LEGACY-BOOT-BE.json)
now covers real timer-stop/start cuts and independent boot continuation from the
exact recognized legacy hook. Inverse restores its bytes and inode; forward
publishes the independent native timer. Existing ledger/receipts are preserved.
P0.3/P0.4/P0.5 remain partial: this explicit legacy-byte fixture does not establish
an actual old-release migration or the complete workload/rollback matrix.


### New-plan independent mail enrollment review (2026-09-23)

P0.2/P0.3/P0.4/P0.5 now connects [verified native/source review](MAIL-ENROLLMENT-RESERVATION.md#new-plan-native-review-and-admission-2026-09-23)
to new mail plans. Only positively absent or recognized legacy configuration adds
the exact kit step after the certificate. Independent schedules preserve the
owner's existing kit and timer choices. Unknown/changed evidence blocks review;
a busy native renewal is a distinct wait. Missing parent observation is no-follow
and never provisions directories. Existing accepted plans and persistent schemas
are unchanged. Source/component checks plus earlier scoped native helper evidence
do not establish the complete native wizard/update/rollback chain; P0 stays open.


### Reviewed mail setup native handoff — September 23

P0.2/P0.3/P0.5 now has [Arch native RPC/boot evidence](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-SETUP-RPC-BE.json):
real source/native preview preserves the existing independent schedule; a new
reviewed legacy enrollment reaches authenticated Agent IPC and the independent
systemd worker once, with SQLite fence reload and repeated terminal reads.
Following completed enrollment, QEMU reset retains identical receipts/ledger and
an enabled/active native timer with management daemons stopped and Panel absent.
Earlier setup steps and licensing are test prerequisites, not full wizard or
production entitlement acceptance. Prior-release transitions, full workload and
fault matrices remain open. No persistent schema or installed owner host changed.


The [Debian counterpart](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-SETUP-RPC-DEBIAN-BE.json)
passes the same single-dispatch/new-plan and existing-schedule preservation
boundary. After reboot, 61 mail configuration/certificate files and all captured
enrollment/ledger bytes remain unchanged; native SMTP/IMAP TLS serves the same
system-trusted fixture certificate with management daemons stopped. This is a
P0.4/P0.5 continuity proof for that trial, not delivery/authentication, new renewal,
historical application rollback or full first-install/browser acceptance.


### September 23: recorded native mail continuation source separation

P0.3/P0.5 invariants: accepted recovery survives ordinary management failure;
new authority is never inferred from absence. Fresh enrollment retains compatible
Agent and reviewed-owner admission. Already accepted boot/owner continuation uses
its ledger-bound immutable scope and complete retained helper instead. The common
writer restricts that consumer to the recorded direction and verified terminal
transition before publication-stage cleanup. No durable schema migration or
historical receipt rewriting. Root race fixtures cover removed management files,
source edits, missing/wrong admission and forbidden direction changes. The
[native management-absence record](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-MANAGEMENT-ABSENT-BE.json)
now proves same-operation Arch forward/inverse and Debian forward completion after
real timer interruption and reboot with Agent/Panel/declaration absent. The
production helper restored volatile locks; no manual postboot repair ran. Fresh
admission remained refused and explicit terminal continuation kept evidence
unchanged. Debian preserved 61 mail files and the verified SMTP/IMAP TLS leaf.
This closes the measured ordinary-management dependency, not P0.3/P0.5 in full;
old helper/application compatibility and the wider workload matrix remain open.


### DNS acquisition/publication schema boundary (2026-09-23)

P0.4 now has separate canonical acquisition and publication schemas with exact
content binding and shared legacy validators. Agent ownership/update/reinstall
comparisons consume that boundary without changing active on-disk producers.
The lossless legacy separation proposal preserves both original before-images;
its inverse refuses later publications or changed ownership evidence. Actual
Alpha81 producer fixtures cover the transition's byte contract. See
[DNS schema and migration boundary](DNS-ENGINE-ARTIFACT.md#separate-wire-records-and-retained-before-images-2026-09-23).

The operation-bound filesystem publisher, protected metadata/checkpoint protocol,
independent DNS executor and native historical update/restore matrix remain open.
This component contract is not an installed migration or closure of P0.4.


### DNS document publication and application admission (2026-09-23)

P0.4, invariants 1/2/3/4: [operation-bound DNS documents](DNS-ENGINE-ARTIFACT.md#operation-bound-document-publication)
now separate acquisition/publication on disk in self-contained state/v2 and
ownership/v2 documents, while preserving exact legacy frozen before-images.
Existing accepted DNS operations use the same native proof and atomic publication
barrier. Historical signed-update journal cleanup retains its current wire format.
An independent read-only compatibility gate checks live DNS evidence against the
exact target Agent before downtime and publication, including rollback targets.
Application rollback preserves native DNS instead of restoring stale zones.

Real SIGKILL/file inverse, old/new mixed input, owner-change refusal and build
capability checks provide component evidence. The standalone DNS recovery
executor, full signed application downgrade/automatic rollback matrix and wider
native workload matrix remain open. No installed owner server was changed.

[The native DNS document trial](../deploy/e2e/release-recovery/DNS-DOCUMENTS-BE.json)
now verifies actual Alpha81-to-current ordinary zone publication on Debian/Arch,
unchanged acquisition bytes, independent old-target refusal, and native BIND TCP/UDP
answers after reboot without management binaries. All DNS receipt and ledger bytes
survive exactly. It does not exercise signed UI update/automatic rollback or
secondary transfer, so the remaining P0.4/P0.5 matrix stays open.


## Offline guidance at the normal panel address (2026-09-23)

The [static browser recovery shell](OFFLINE-RECOVERY-SHELL.md) now survives a
normal reload after a prepared browser loses the panel connection. It preserves
the existing operation reference, exposes read-only owner guidance and offers
return after an actual availability response. Public static files alone are
cached; no authenticated status or mutation authority is retained. Real Chrome
listener-interruption and EN desktop/TR mobile checks pass. This is offline
guidance, not independent live authenticated status while Panel is stopped;
the latter and the wider P0.2 matrix remain open.

### DNS switch journal shared contract — September 23, 2026

P0.4 now shares the historical switch journal codec, frozen file/unit snapshots,
source acquisition and fixed host-layout rules between potential consumers.
[Evidence and limits](DNS-ENGINE-ARTIFACT.md#shared-switch-journal-contract--2026-09-23)
include exact historical Alpha81 producer bytes and Agent/shared race checks.
Journal v1 is unchanged; embedded source v1/v2 bytes are preserved. No independent
mutation entry point was added: accepted-ledger authority, worker/host locks and
native inverse execution must still be separated and proven. P0.4 stays open.
The same P0.4 boundary now shares exact accepted-operation and finalized-ledger
comparisons in `dnsengineartifact.SwitchIdentity`. Canonical expected identity
is required; registered worker shape is not recovery permission. Historical
phase and lease-expiry bytes are unchanged. Shared and Agent race checks pass;
independent host locks, liveness and native inverse execution remain open.
P0.4 now also has a shared [DNS switch recovery decision sequence](DNS-ENGINE-ARTIFACT.md#shared-switch-recovery-decision-sequence).
The Agent delegates its old post-crash decision order to that package; v1
journal and phase bytes are unchanged. Its fault tests guard write/verify/inverse
ordering and retained evidence after uncertainty. Host locks, independent native
inverse execution and full native release acceptance stay open.

P0.4 now fails closed on an unverified DNS switch target: a fresh automatic
inverse requires the exact frozen source-state proof, not a generic verifier
error. Historical journal v1 bytes are unchanged. Unknown probes retain evidence
and may need owner recovery; independent native execution and full fault-matrix
acceptance remain open.

P0.4 also preserves the v1 DNS switch decision across crash replay: a durable
inverse phase cannot recommit, and a rolled-back phase cannot regress to
rolling-back. Shared race tests cover the replay ordering. This is not yet
independent native recovery acceptance.

The exact frozen-source predicate is also shared in dnsengineartifact and
tested against all three Alpha81 journal fixtures. Agent still owns the host
observation and inverse; this is a reusable proof boundary, not independent
DNS recovery.

A component fixture further verifies v1 switch-journal/v2 source-document
compatibility and rejects a later publication as the original frozen source.
Installed migration and native rollback acceptance remain open.

The P0.4 independent DNS observer now shares the Agent's exact systemd
process parser and, for a selected BIND target, compares two running
named/bind9 process observations, no-pending-reload state, the Linux process
start token and the native executable inode. Vendor file and loaded unit
identity proofs surround the process reads. See the
[bounded evidence and tests](DNS-ENGINE-ARTIFACT.md#independent-selected-bind-runtime-observation-2026-09-24).
No persisted schema or recovery admission changed. Loaded named config,
authoritative answers, owner edits, independent inverse and the interrupted
native fault matrix remain open; this does not close P0.4.
The P0.4 native BIND disk observer now shares the Agent's exact managed
zone-include predicate and a descriptor-based fixed-path reader for APT
and pacman config owner modes. It refuses absent, inert, changed or unsafe
managed includes and rereads the selected generation. See
[the bounded config proof](DNS-ENGINE-ARTIFACT.md#shared-native-bind-include-and-disk-observation-2026-09-24).
This changes no persisted schema or recovery admission. Main-config
reachability, named's loaded config, authoritative answers, owner edits
outside the managed block, independent inverse and native fault acceptance
remain open; P0.4 is not complete.
The P0.4 APT disk observation additionally verifies active top-level main
config includes of the options and managed local anchor with a shared lexical
predicate. The [main-config evidence](DNS-ENGINE-ARTIFACT.md#apt-bind-main-config-include-reachability-2026-09-24)
is read-only and schema-neutral. It is not a full BIND parse, named loaded
config proof, DNS answer proof, independent inverse or native fault-matrix
acceptance; P0.4 remains open.
The P0.4 selected-primary observer can also compare the verified BIND
catalog receipt with two authoritative DNS/TCP SOA answers from a
locally owned literal address. See the
[bounded answer evidence](DNS-ENGINE-ARTIFACT.md#bounded-local-primary-catalog-answer-observation-2026-09-24).
This is read-only, schema-neutral and not recovery admission. Listener
PID binding, member zones, AXFR, secondary convergence, loaded config
and the native interrupted-switch matrix remain open.

The P0.4 selected-BIND observer also compares the native TCP/UDP
port-53 socket inventory with the verified systemd MainPID across
two bounded reads and then rechecks the process identity. For a
primary receipt it requires listeners on the local IPv4 address
or the IPv4 wildcard. See the
[bounded listener evidence](DNS-ENGINE-ARTIFACT.md#independent-bind-listener-inventory-observation-2026-09-24).
The shared parser prevents the Agent and observer from accepting
different ss grammar. This is read-only and schema-neutral, with
no recovery admission. Loaded config, answer/socket causality,
member zones, AXFR, secondary convergence, owner changes,
independent inverse and native fault acceptance remain open.

The P0.4 independent status reader now also observes the selected
PowerDNS target's APT/systemd package-owned vendor unit bytes and
loaded identity, unit topology,
exact running PID and public DNS listeners. See the
[bounded PowerDNS evidence](DNS-ENGINE-ARTIFACT.md#independent-selected-powerdns-runtime-observation-2026-09-24).
It is read-only, schema-neutral and does not extend recovery admission.
Config/database identity, zone answers, owner changes,
independent inverse and native fault acceptance remain open.
P0.4, constitutional invariants 1/2/3: the Agent and independent DNS status
reader now share the same immutable-journal native inverse classification.
It separates running owner BIND adoption from the stop/start switch inverse,
and PowerDNS adoption from its switch inverse. Manifest mismatch and
contradictory BIND alias preimages fail closed. Historical v1 journal,
receipt, ledger and phase schemas are unchanged; the independent reader
remains read-only and does not admit or execute an inverse. Fixture and
Agent/recovery tests cover this decision boundary. Worker exclusion, owner
edit and native-state proof, an Agent-independent inverse executor and the
interrupted-switch fault matrix remain open; P0.4 is not complete.
P0.4, constitutional invariants 1/2/3: the PowerDNS switch rollback's
native effects now run through a shared fail-stop sequence. A failed stop
cannot be followed by live SQLite restoration; a failed or interrupted
predecessor withholds later effects and leaves the same rolling-back journal
for recovery. No journal/receipt/ledger schema or phase transition changed.
Injected step-failure and cancellation tests plus the Agent suite verify
this component boundary. Native crash/reboot continuation, independent
host effects, owner-edit matrix and full P0.4 acceptance remain open.
The same P0.4 fail-stop boundary now covers managed BIND activation
rollback: a failed target-unit restore withholds configuration and state
writes, and any later failure withholds its successors. Running unmanaged
owner-BIND adoption retains its non-stopping inverse. The BIND target
preimage may be active for managed reconfiguration; native continuity
is not established by the ordered component test. No persisted schema
changed. Independent native inverse and interruption trials remain open.
The PowerDNS fail-stop inverse also requires two matching inactive
pdns.service and zero-PID/dead process readbacks after systemctl stop
and before SQLite restoration. A command exit code alone is insufficient.
Unknown or changing state withholds database and later writes and retains
the journal. This point-in-time proof cannot exclude a foreign writer or
future owner start; independent recovery and native fault acceptance
remain open.

P0.4, constitutional invariants 1/2/3: Agent DNS switch startup no longer
treats an unreadable registered worker as an exited worker. The common
process-identity proof requires a kernel procfs, a canonical recorded start
token, and a missing or replaced PID; unknown state retains the host lock
and journal without running an inverse. The independent status reader uses
the same point-in-time distinction. Agent recovery now also checks the
shared exact accepted ledger identity before native reconciliation and
across committed finalization. The historical Agent orphan-worker wait is
recognized only with its exact DNS operation, worker, phase and reason;
foreign or contradictory evidence stops before native effects. No
journal/ledger/receipt schema, phase producer or installed server was changed.
Process fault tests, exact orphan-exit startup, mismatched cancellation,
Agent/recovery package tests and vet are scoped evidence. A complete
Agent-independent native executor, owner-edit race proof and reboot/fault
matrix remain open; P0.4 is not complete.

P0.4, constitutional invariants 1/2/3: the independent DNS reader now returns
the validated frozen journal paired with its exact accepted ledger and receipt
observation from one bounded set of secured reads. A malformed ledger withholds
the journal. Under the existing release and host locks, the quiesced status
path compares that pair and native unit properties across two observations.
No persisted schema or phase changed, no installed server was modified, and
this read-only API supplies no inverse authority. Package tests and vet are
scoped evidence; exact worker exclusion at execution time, owner-edit and
loaded-native-state proof, an independent host-effects executor, and the
interrupted-switch native matrix remain open.

P0.4, constitutional invariants 1/2/3: an interrupted DNS switch whose boot
recovery deliberately released an undecidable lease now has one exact shared
terminal-job predicate. Agent replay and the independent status reader use the
same identity, phase, reason, timestamp and no-worker checks. The reader reports
this retained journal as a released interruption instead of treating it as
foreign evidence; unrelated terminal failures still fail closed. No persisted
schema, write sequence, installed panel or native DNS service changed. Full
affected package tests, adversarial classification tests and vet are scoped
evidence. The independent inverse executor, worker/native/owner rechecks at
effect time and interrupted-switch native matrix remain open.

The same read-only released-interruption observation now carries only its
validated reason code. Owner guidance distinguishes an unsupported or unreadable
host from a startup window timeout and names the next native inspection.
No error message, credential or arbitrary ledger text is displayed. This is
diagnostic guidance, not authority to resume or mutate DNS.

The independent DNS status command now prints the exact operation state and
owner action before lengthy BIND/PowerDNS observations. If a later native
probe fails, the owner still sees the retained operation and its next step.
This changes output order only; the observer remains read-only.

P0.4, invariants 2/4: DNS switch journal checkpoint encoding, uncertain-write
readback and error preservation now live in the shared recovery package;
the existing Agent producer calls that contract with its secure host adapter.
No v1 schema, phase transition or installed state changed. Fault-hook
ordering, Agent/recovery package tests and vet are scoped evidence.
Independent secure file publication, native inverse execution and the
interrupted-switch fault matrix remain open.

P0.4, invariants 1/2/4: DNS rollback journal removal now binds to the exact
rolled-back checkpoint before unlink and confirms absence afterward.
The v1 schema and phase order are unchanged. Shared removal fault tests,
full Agent/recovery package tests and vet are scoped evidence. External
owner rewrites between read and unlink, Agent-independent secure filesystem
effects and native interruption trials remain open; P0.4 is not complete.

P0.4/P0.3, constitutional invariants 1/2/4: Agent rollback now persists the
rolled-back DNS switch checkpoint and retains the frozen source until the exact
mutation ledger verdict is durably failed. The direct BIND switch/adoption and PowerDNS switch/adoption rollback producers
also retain this checkpoint. The running terminal path then removes only that
exact checkpoint; boot recovery recognizes the same failed operation,
reproves the native inverse and completes removal after an interrupted terminal
publication. A different owner, target, request, phase or missing verdict
withholds cleanup. A new DNS mutation only observes a retained prior switch
journal and refuses; it cannot replay that previous operation under its own
lease. The independent reader classifies a retained terminal
rollback without granting mutation authority. The historical v1 journal and
ledger schemas are unchanged; the ordering between their existing phases changed.
Shared ordering, exact-ledger, secure cleanup, direct-producer and local boot
replay tests are component evidence. A native kill/reboot trial at each boundary, owner-edit
races, an Agent-independent inverse and complete update/rollback matrix remain
open; P0.3 and P0.4 are not accepted as complete.

P0.4, constitutional invariant 3 follow-up: idle boot reconciliation of a
retained terminal DNS rollback keeps the journal and reports an unknown native
reproof or unreadable evidence without poisoning the global mutation manager.
A new DNS operation still refuses that journal at its own preflight boundary;
unrelated host operations retain their normal lock path. A local boot replay
test checks that the host lock is released on unknown reproof. This is scoped
component evidence, not proof of native workload continuity.


P0.4, constitutional invariants 1/2/3: after an exact DNS rollback has a
persisted failed ledger verdict, inability to retire its journal no longer
poisons the whole Agent or retains the common host mutation lock. The journal
and error remain available for DNS-specific review; new DNS work still refuses
that unresolved checkpoint. Active terminal publication and boot orphan
recovery use the same boundary. The v1 journal and ledger schemas are unchanged.
A focused component test proves that a mismatched journal survives while the
failed verdict is durable and the host lock becomes available. Native reboot,
owner-edit and Agent-independent inverse acceptance remain open.

P0.4, constitutional invariants 1/2/3: if Agent startup cannot verify an
interrupted DNS switch's native result, it may release only that dead worker's
accepted ledger lease when a canonical frozen journal still matches request,
owner, target and qualifier under the host lock. The new stable failure reason
`dns_native_recovery_unknown_after_restart` is accepted by the existing
released-undecided reader and gets explicit owner guidance in the read-only
`recovery dns-switch-status` command. The journal remains, new DNS work refuses it, and
idle boot may retry the exact recovery; unrelated host work can continue.
Missing, unreadable or mismatched journal evidence still retains fail-closed
host exclusion. The historical v1 journal/ledger schemas are unchanged; only
an additional terminal reason is recognized. Local exact/missing evidence and
shared-authority tests pass. Native reboot/owner-edit trials and an
Agent-independent inverse are still required for P0.4 acceptance.


P0.4 BIND inverse stop boundary: the Agent now requires two matching
inactive/dead/zero MainPID/ControlPID observations of a newly activated
named.service after restoring its unit preimage and before rewriting native
BIND configuration. Uncertain or active state retains the accepted rollback
journal and blocks later effects. The historical v1 artifacts do not change.
The focused and DNS/BIND/PowerDNS package tests pass; owner restarts, stray
processes, independent inverse execution and native interrupted recovery
remain open.


P0.4 shared native stop proof: BIND and PowerDNS inverses now consume one
fixed-unit, two-observation inactive/dead/zero-MainPID/ControlPID predicate.
This eliminates divergent Agent decisions and gives a future independent
executor the same read-only stop rule. The v1 evidence schemas and recovery
checkpoints are unchanged. Full Agent/shared-package tests and vet pass;
native post-extraction interruption, cgroup/owner-restart exclusion and
Agent-independent host effects remain open.

P0.4, constitutional invariants 1/2/3: the root-only independent `recovery dns-switch-status --quiesced` command can now give a bounded, fixed-unit, two-read stopped-target observation for a retained terminal DNS rollback with an originally inactive target. Missing/changed systemd or persisted evidence remains unknown; the journal is retained and no native mutation is started. Existing persisted schemas and the Agent's same-operation inverse behavior are unchanged. Package tests and vet establish this read-only path only. Cgroup emptiness, owner edits after observation, an Agent-independent native inverse and the native fault/reboot matrix remain unproven.

The post-extraction native BIND regression at commit `d4b8c0e` repeated one existing rolled-back/before-write standalone cell with exit 137, same-request convergence and 31/31 healthy post-recovery Agent/Panel/DNS samples. This is scoped Agent-path evidence, not new matrix coverage or proof of the independent `recovery` observer, stopped-target rejection, owner-edit exclusion or an Agent-independent inverse. P0.4 remains open.

P0.4 native matrix progress: the `bind__target-started__before-write__standalone__peer-reachable` cell now passed on a fresh Debian 13 managed-PowerDNS source: exit 137, same-request BIND convergence, and 31/31 healthy Agent/Panel/authoritative UDP+TCP samples over 30 seconds. The [native report](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-TARGET-STARTED-BEFORE-WRITE-20260925.md) brings the passed runnable BIND count to six. It leaves paired DNS, power loss/reboot, owner edits, uninterrupted cutover and independent inverse acceptance open; P0.4 is not complete.

P0.4, constitutional invariants 1/2/3: BIND and PowerDNS Agent inverses and the independent read-only DNS rollback observer now share a fixed-unit cgroup-v2 population check in each stopped-target observation. The existing journal, ledger and DNS receipt schemas are unchanged; unknown/foreign/populated cgroup state retains the same operation without config or database restoration. Component tests and [one repeated native BIND trial](../deploy/e2e/dns-kill-matrix/NATIVE-DNS-CGROUP-GUARD-20260925.md) passed. That trial adds no new matrix cell and does not prove native rejection of an injected descendant, owner-restart exclusion, paired/reboot behavior or an Agent-independent inverse. P0.4 remains open.


P0.4, constitutional invariants 1/2/4: a Linux exact-preimage atomic writer
and the shared `ReplaceRollbackJournalPhase` adapter now supply the narrow
durable journal checkpoint boundary required by a future Agent-independent
DNS inverse. It refuses a missing or changed 0600 journal and any transition
outside `rolling-back` or `rolled-back`, and exact readback resolves uncertain
publication. V1 journal, ledger and receipt schemas are unchanged. Focused
adversarial filesystem/phase tests, Agent/recovery package tests and vet are
scoped evidence. The independent CLI remains read-only: it does not yet prove
native inverse admission, perform host effects or publish terminal ledger
results. External owner races and the native interrupted/reboot matrix remain
open; P0.4 is not complete.


P0.4 follow-up: the shared DNS recovery write callback now carries exact
before/after journals. A local filesystem-backed test proves interrupted
`rolling-back` retention and same-request `rolled-back` publication through
the independent checkpoint adapter. Agent/recovery package tests and vet pass.
This changes no installed format or native service effect. The Agent's existing
writer remains in place; a separate independently admitted inverse executor,
owner-edit/reboot trials and full matrix are still required.


P0.4 checkpoint safety: shared rollback no longer advances its caller's
in-memory phase when the durable writer reports uncertainty. The next attempt
must read the exact persisted journal; neither inverse admission nor terminal
success is inferred from memory. Focused first/terminal-write tests and the
Agent package suite pass. This is a component boundary, not native recovery
acceptance or P0.4 completion.


P0.4, constitutional invariants 1/2/4: exact terminal DNS journal retirement
now has a shared Linux private-evidence adapter with full-preimage comparison,
trusted descriptor unlink, directory sync and absence readback. It refuses
foreign or unsafe evidence and permits only a `rolled-back` checkpoint. This
changes no v1 schema or Agent producer. Focused adversarial tests, wider
Agent/recovery package tests and vet passed. The independent command still
needs a proved failed ledger verdict and native inverse under host locks before
it can call this adapter; native interruption and owner-edit acceptance remain
open.

The [shared phase checkpoint native regression](../deploy/e2e/dns-kill-matrix/NATIVE-DNS-PHASE-CHECKPOINT-20260925.md) at `e13ce5e` repeated the BIND rolled-back/before-write cell: exit 137, same-request BIND convergence and 31/31 healthy Agent/Panel/authoritative UDP+TCP samples. It adds no matrix coverage and does not exercise the independent exact-file adapters or prove an Agent-independent inverse. P0.4 remains open.

P0.4 native PowerDNS adoption progress: the [external-source intent/after-write trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-ADOPTION-INTENT-20260925.md) passed on a fresh Debian 13 guest with an unreceipted authoritative PowerDNS/SQLite source: exit 137, same-request adoption convergence and 31/31 healthy post-recovery Agent/Panel/UDP+TCP DNS samples. This is one new runnable `pdns-adopt` cell; it does not establish rollback at later phases, owner-edit/reboot behavior or an Agent-independent inverse. P0.4 remains open.

The [PowerDNS adoption rolled-back/before-write native trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-ADOPTION-ROLLBACK-20260925.md) also passed exit-137 and 31/31 post-recovery health checks from a retained `rolling-back` journal. The same request converged **forward** to PowerDNS; it did not prove native inverse completion or terminal journal cleanup. This adds a second `pdns-adopt` cell while P0.4 independent recovery and later-phase acceptance remain open.

P0.4, constitutional invariants 1/2/3: the exact accepted DNS switch worker
check is now shared by Agent boot recovery and the independent locked status
reader. The secured reader carries the matching ledger job with its frozen
journal; malformed or foreign running/cancelling/orphaned jobs and unknown
procfs state fail closed before a worker is called absent. A live worker stays
distinct from an exited one. No persisted v1 schema or native effect changed.
Linux recovery/Agent package tests are scoped evidence. The status reader is
still read-only; owner edits, an Agent-independent inverse and the remaining
native fault matrix keep P0.4 open.

P0.4, constitutional invariants 1/2/3: PowerDNS adoption rollback now uses
one shared fail-stop sequence for owner-aware config proof, frozen state
restoration and native source verification. Context cancellation is checked
before and after each effect; errors withhold later steps and retain the same journal.
The Agent supplies its existing certified host callbacks. No v1 document,
phase, installed host or native PowerDNS lifecycle changed. Focused order,
failure and cancellation tests plus the affected Linux Agent suite are scoped
evidence. An independently admitted native executor, owner-edit race drills
and the interrupted-switch matrix remain open; P0.4 is not complete.

The [shared PowerDNS rollback native regression](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-ADOPTION-SHARED-ROLLBACK-20260925.md) at commit `4044707` repeated the existing adoption rolled-back/before-write cell: proved exit 137, retained `rolling-back` journal, same-request forward convergence, and 31/31 healthy Agent/Panel/authoritative UDP+TCP DNS samples. This verifies the changed Agent path at one boundary; it adds no matrix coverage and does not establish a completed inverse, independent executor, cancellation race or P0.4 acceptance.

P0.4, constitutional invariants 1/2/3: the independent quiesced DNS status reader now gives PowerDNS adoption a fixed-path, no-follow, single-link database byte proof against the accepted journal. It brackets a stable certified native PowerDNS process and TCP/UDP listener observation with two exact database reads and a final private-evidence reread under the release and host locks. Unknown bytes, identity, service or listener state fail closed without a host effect. The Agent now uses the same secure database byte reader for adoption and switching; its persisted v1 producer formats are unchanged and the observer gains no mutation authority. Linux tests and vet pass; a native quiesced-adoption observer trial now passed at one interrupted boundary. SQLite/zone proof, owner-edit race drill and Agent-independent inverse remain open. P0.4 is not complete.

The [shared-reader PowerDNS native regression](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-SHARED-READER-20260925.md) at `df155f5` repeated the existing adoption rolled-back/before-write cell: proved exit 137, same-request forward convergence and 31/31 healthy Agent/Panel/authoritative UDP+TCP DNS samples. It adds no matrix cell and does not exercise the independent quiesced observer or prove an independent inverse. P0.4 remains open.

The [native PowerDNS quiesced-observer trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-QUIESCED-OBSERVER-20260925.md) repeated that cell with the independent root-only command immediately after proven exit 137 and before Agent restart. The retained rolling-back adoption journal, frozen database bytes, stable `pdns.service` process and TCP/UDP port-53 ownership were observed under the release and host locks. A later pre-retry observation found no journal after Agent startup; the same request converged forward through Agent retry, with 31/31 healthy post-recovery samples. This is point-in-time diagnostic evidence, not safe independent inverse execution, SQLite/zone proof or new matrix coverage. Persisted schemas are unchanged and P0.4 stays open.

P0.2/P0.4, constitutional invariants 2/3: the root-only DNS observer now passes one 30-second deadline through its context-aware native unit, process, listener, generation and database probes instead of starting each with an unbounded background context. Exhaustion returns unavailable with owner guidance, leaving the accepted operation and native DNS untouched; the inner PowerDNS adoption proof keeps its shorter ten-second limit. Persisted schemas and Agent behavior are unchanged. This bounds those probes, not every possible file/output stall, and does not establish independent recovery execution or P0.4 completion.

P0.2/P0.4 follow-up, invariants 2/3/6: the independent DNS status CLI now
accepts an exact request ID only with its quiesced lock mode. If the switch
journal has already retired, it double-reads the canonical ledger and reports
only that request's recorded status; a different active mutation or changing,
missing or invalid evidence remains unavailable. This narrows a demonstrated
post-Agent-start visibility gap without interpreting journal absence as
success or running an inverse. The journal/ledger schema stays v1. Focused
package tests and [one disposable native exact-request trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-EXACT-REQUEST-20260925.md)
pass; the independent inverse/fault matrix is still open.

P0.4, constitutional invariants 1/2/3: the read-only PowerDNS adoption
database verification is now shared between Agent and the independent
quiesced DNS observer. The observer reconstructs the journal's exact manifest,
checks zones, peer rows and SQLite integrity in a read-only transaction, and
brackets that transaction with secure frozen-byte reads. The existing v1
journal/ledger/state formats and Agent mutation path are unchanged. A
mismatch preserves the same operation and reports an unavailable result;
neither a matching transaction nor the status CLI authorizes a native inverse.
Affected package tests, vet and real SQLite changed-row/extra-zone/path tests
pass. One [disposable native SQL observer
trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-SQL-OBSERVER-20260925.md)
passes after an adoption rollback SIGKILL. Owner-edit races, live DNS answers
at that observation instant and Agent-independent inverse execution remain
open.

P0.4, constitutional invariants 1/2/3: the independent quiesced PowerDNS adoption observer now binds the frozen active-zone SOA serials to live nonrecursive authoritative UDP and TCP replies at a concrete local IPv4 endpoint covered by the verified native listener inventory. It brackets that point-in-time answer check with process, listener, database-byte and journal rereads under existing locks. Unknown answers fail closed; deleted-zone absence and other records are explicitly unproved. The installed state/journal/ledger and DNS receipt schemas remain v1, recovery behavior remains read-only, and no independent inverse authority is added. Linux tests and [one disposable SIGKILL native trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-SOA-OBSERVER-20260925.md) pass. Owner-edit race exclusion, loaded configuration, other/deleted record proof, independent inverse execution and the full interruption matrix remain open; P0.4 is not complete.

P0.4, constitutional invariants 1/2/3: the independent quiesced PowerDNS adoption observer now reads the journal-frozen native config files through fixed no-symlink descriptor paths. It verifies exact bytes, root/pdns ownership, mode, ACL absence, parent integrity and stable identities twice before and after the live SOA answer. Unknown or owner-changed configuration fails closed with the same accepted operation retained. No persisted schema/version transition or recovery write authority is added. Linux security tests and [one disposable native SIGKILL trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-CONFIG-OBSERVER-20260925.md) pass. Loaded configuration, later owner edits, independent inverse execution and the remaining DNS fault matrix are still unproved; P0.4 remains open.

P0.4, constitutional invariants 2/3: after native DNS observations, the
independent quiesced status reader now rechecks exact accepted evidence,
systemd unit properties and the recorded worker while holding release and host
locks. A mismatch or non-excluded worker fails closed and retains the same
journal. Persisted schemas stay v1 and this adds no inverse authority. Linux
package tests and vet pass; later owner edits and Agent-independent recovery
execution remain open.
P0.4, constitutional invariants 1/2/3: a shared Linux primitive can now
remove only an exact target state receipt during a frozen PowerDNS adoption
rollback. It uses the established private-path owner policy and byte-exact
CAS unlink; foreign or edited receipts are retained and absence is only an
idempotent no-op. Existing v1 schemas do not change. Linux tests cover exact,
foreign, symlinked and wrong-phase cases. The primitive has no standalone
authority or CLI route: locks, worker exclusion, native source reproof,
checkpoint/ledger publication and reboot/owner-edit matrix coverage still
separate it from an Agent-independent inverse. P0.4 remains open.
P0.4, constitutional invariants 1/2/3: the independent recovery package now
has a byte-exact v1 ledger CAS publisher for a previously proved rolled-back
DNS journal. It verifies the exact active job and excludes its recorded worker
through kernel procfs before clearing only that job's lease; the journal is
retained for separate native reproof and retirement. Changed evidence, a
foreign job or a live worker fails closed. A post-CAS journal change leaves an
unknown outcome for the same operation rather than claiming success. Linux
tests pass, including a real live-process rejection. This primitive is not
wired to a CLI and grants no independent inverse authority; lock ownership,
native inverse proof, reboot and owner-edit acceptance remain open.
P0.4, constitutional invariants 1/2/3: an already durable PowerDNS adoption
rollback now has a shared fail-stop sequence across exact state removal,
native-source reproof, rolled-back checkpoint, terminal ledger publication
and exact journal retirement. It rereads accepted evidence and checks the
recorded worker before effects, rejecting changed receipts or status. Injected
failures at each durable boundary resume the same request in unit tests. The
coordinator accepts callbacks and is not a callable independent executor:
installed-path binding, both locks, native proof and the reboot/owner-edit
matrix still need integration and field evidence. Existing v1 formats remain
unchanged and P0.4 stays open.
P0.4, constitutional invariants 1/2/3: the independent quiesced PowerDNS
adoption observer now uses a shared read-only native-source proof that rejects
other journal shapes before probing systemd. It verifies inactive BIND units,
active PowerDNS, frozen configuration/database/SQL, process and sole public
TCP/UDP DNS listeners, and live authoritative active-zone SOA over both
transports, with before/after observations under the release and host locks.
Unknown or changed evidence retains the same accepted operation. Persisted v1
formats and recovery write authority are unchanged. Affected Linux tests and
vet pass; a [disposable native SIGKILL observer trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-SHARED-NATIVE-PROOF-20260925.md)
repeated one existing adoption cell and proved the read-only shared source
observation before Agent restart, followed by 31/31 healthy Agent/Panel/DNS
samples after same-request forward convergence. It adds no matrix coverage.
Loaded configuration,
deleted-zone absence, later owner edits, independent inverse execution and the
remaining fault/reboot matrix keep P0.4 open.
P0.4, constitutional invariants 1/2/3/6: a dormant independent PowerDNS
adoption inverse adapter now binds the shared transaction sequence to fixed
installed paths, release/host locks, secured exact-request evidence, native
PowerDNS proof, worker exclusion and exact state/journal/ledger CAS effects.
The coordinator additionally rechecks terminal evidence and worker exclusion
after its final native proof, so a late owner edit retains the journal. No CLI
route, installed-host change, producer/schema transition or Agent behavior is
introduced. The dormant inverse now refuses a frozen zone deletion because
the shared proof has no native deleted-zone absence check; SQL row absence
is insufficient to admit a host effect. Focused Linux tests and vet pass,
including a final owner-edit race. Native interruption/reboot and owner-edit
acceptance must pass before
exposing a supported recovery command; P0.4 remains open.
P0.4, constitutional invariants 2/3: injected cancellation immediately after
the adoption inverse's state, phase and ledger effect callbacks leaves the exact
journal available for same-request continuation. The subsequent retry reaches
the terminal failed verdict and journal retirement in focused tests. This is
in-memory checkpoint evidence only; it does not replace native process-kill or
reboot acceptance, and no CLI action was enabled.

P0.4/P0.5, constitutional invariants 1/2/3: the shared read-only native
Certbot source reader now retries only a transient Linux openat2 EAGAIN at most
three times. A successful descriptor still passes the same path, owner, inode,
revision, trust and byte checks; persistent uncertainty and every other
resolver error fail closed without issuance, renewal, publication or cleanup.
No persisted schema/version or recovery authority changes. Focused repeated
Linux reader/Agent tests and vet pass. This does not establish independent
renewal or the remaining native fault/owner-edit matrix.

P0.4, constitutional invariants 1/3: the Agent's existing native DNS SOA
reply parser now requires the exact single question, query type/class, ordinary
opcode and bounded record count. Its deleted-zone proof rejects any answer
record, including a non-SOA answer, rather than inferring absence from an SOA
list alone. Wrong or ambiguous wire evidence fails the existing verification
without another mutation. The switch journal, ledger and DNS artifact schemas
remain v1; no independent inverse or new recovery command is enabled. Focused
raw-packet regressions, full Agent package tests and vet pass. Shared
Agent/independent negative-answer semantics and native deletion, interruption,
reboot and owner-edit acceptance remain open.

P0.4 native PowerDNS adoption matrix progress: the [target-verified/before-write trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-ADOPTION-TARGET-VERIFIED-BEFORE-WRITE-20260925.md) at `99d586f6` passed in a fresh disposable Debian 13/Arch pair with an unreceipted external PowerDNS source. The controller proved SIGKILL exit 137 at the before-write hook, retained `intent` journal, same-request Agent-mediated forward `target_converged`, and 31/31 healthy post-recovery Agent/Panel/authoritative UDP+TCP DNS samples. This adds a third `pdns-adopt` cell. Persisted v1 formats are unchanged. It does not prove uninterrupted DNS, deleted-zone absence, an Agent-independent inverse, reboot, paired DNS or owner-edit safety; P0.4 remains open.

P0.4 native PowerDNS adoption matrix progress: the [target-verified/after-write trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-ADOPTION-TARGET-VERIFIED-AFTER-WRITE-20260925.md) at `97984afc` passed in a fresh disposable Debian 13/Arch pair with an unreceipted external PowerDNS source. The controller proved SIGKILL exit 137 after the `target-verified` journal write, retained that exact phase, same-request Agent-mediated forward `target_converged`, and 31/31 healthy post-recovery Agent/Panel/authoritative UDP+TCP DNS samples. This adds a fourth `pdns-adopt` cell. Persisted v1 formats are unchanged. It does not prove uninterrupted DNS, deleted-zone absence, an Agent-independent inverse, reboot, paired DNS or owner-edit safety; P0.4 remains open.

P0.4, constitutional invariants 1/3: the Agent's active-zone SOA packet parser now rejects an exact SOA accompanied by an extra answer record. This aligns its production wire acceptance with the independent reader's single-answer rule; malformed or ambiguous authority fails before the existing verification can mark a zone ready. The persisted DNS state/journal/ledger schemas remain v1 and recovery still retains the accepted operation without launching another mutation. A raw-packet regression and the full Agent package tests pass. The native trials above used the preceding parser build, and neither this test nor those trials establish deleted-zone absence, an Agent-independent inverse, reboot or owner-edit acceptance; P0.4 remains open.

P0.4, constitutional invariants 1/2/3: the independent quiesced PowerDNS adoption native proof now checks every frozen deleted zone over UDP and TCP at its already verified local daemon endpoint. Its shared bounded SOA wire exchange accepts deletion only when an exact nonrecursive child SOA question receives no answer and one authoritative strict-parent SOA (NXDOMAIN or NODATA). REFUSED and non-authoritative NXDOMAIN are unknown because access policy or referral could produce them while a child zone remains loaded. The database row proof, native process/listener/config rereads and exact journal/ledger observation still bracket this point-in-time answer. A missing or mismatched negative proof retains the operation and starts no host effect. The dormant inverse gate requires the full verified deleted-zone count, but no supported inverse CLI is exposed. Persisted v1 schemas are unchanged. Focused adversarial DNS packet, observer and recovery tests pass; real native deleted-zone interruption, loaded-config proof, owner-edit/reboot behavior and Agent/independent negative-answer parity remain open, so P0.4 is not complete.

P0.4, constitutional invariants 1/2/3: the optional
[real PowerDNS deleted-child trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-DELETED-CHILD-20260925.md)
repeated the adoption intent/after-write SIGKILL cell with an active parent
and a frozen deleted child. Real UDP/TCP SOA probes accepted the absent child
and rejected the active parent before and after the cut. The same request
converged forward with 31/31 healthy post-recovery samples. This is native
wire compatibility and Agent-mediated recovery evidence, not independent
inverse or post-kill observer proof. No persisted schema or supported
recovery authority changed; P0.4 remains open.

P0.4, constitutional invariants 1/2/3: the [native deleted-child quiesced observer trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-DELETED-OBSERVER-20260925.md) repeated an existing PowerDNS adoption SIGKILL cell. Before Agent restart, the independent read-only observer held the release/host locks and verified one active parent plus 1/1 frozen deleted child through strict UDP/TCP SOA answers, bracketed by native process, listener, config, database, journal and receipt rereads. The later ordinary same-request retry converged forward, with 31/31 healthy samples. This establishes bounded post-kill observation for the tested source; it neither authorizes an inverse nor proves reboot, owner-edit or complete native rollback acceptance. Persisted v1 schemas are unchanged and P0.4 stays open.

P0.4, constitutional invariants 1/2/3: Agent deletion verification now consumes the same strict negative-SOA wire validator as the independent observer. The previous non-authoritative REFUSED/NXDOMAIN success branch was removed: those replies do not establish deletion. Failed or ambiguous probes now report unverified absence while retaining the exact accepted operation. No v1 state/journal/ledger transition or independent inverse authority changed. The full Agent and dnswire suites pass; the disposable native trials above used the prior Agent build, and native parity, reboot and owner-edit acceptance remain open. P0.4 is not complete.

P0.4, constitutional invariants 1/2/3: the [fresh corrected-Agent native deleted-zone trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-AGENT-DELETION-PARITY-20260925.md) repeated the intent/after-write SIGKILL cell with the shared strict negative-SOA validator in the ordinary Agent. Proven exit 137, independent 1/1 absent-child UDP/TCP proof before restart, same-request forward convergence and 31/31 healthy samples passed. The previous native trials used the preceding Agent build; this new result is the bounded parity evidence for one real answer shape. It does not establish a safe Agent-independent inverse, owner-policy REFUSED behavior, reboot, owner edits or full P0.4 acceptance. No persisted schema changed.

P0.5, constitutional invariants 1/6: the [disposable management-absent PowerDNS reboot trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-MANAGEMENT-ABSENT-BOOT-20260925.md) confirms one standalone native serving slice after a real boot-ID change. The corrected Agent first converged the same accepted adoption request after exit 137; direct authoritative parent/deleted-child SOA probes passed over UDP/TCP before and after reboot with both management units disabled/stopped and normal executable paths absent. Native pdns.service remained enabled/active. No persisted schema or recovery authority changed. Other CelikPanel files remained; paired transfer, full removal, renewal, other workloads, owner edits and the full P0.5 matrix remain open.

P0.4, constitutional invariants 1/2/3: the [native PowerDNS owner-edit refusal trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-OWNER-EDIT-20260925.md) injected one guest-only native config comment after proven exit 137. The fixed independent quiesced observer exited 3 before and after ordinary Agent restart because the installed config differed from the frozen journal. Two same-request retries remained nonconvergent; the owner edit, exact journal and active PowerDNS service survived. The controller's passed classification covers only bounded service safety. No persisted schema or recovery authority changed. Concurrent effect-point edits, independent inverse and the complete P0.4 matrix remain open.

P0.5, constitutional invariants 3/6 — Historical DNS-pair evidence correction (September 25, 2026): the September 12 BIND-primary/PowerDNS-secondary fixture recorded status: REFUSED, zero answers and no authoritative flag after catalog-member removal, yet marked the run passed. This response does not establish secondary zone removal. The retained raw evidence is unchanged; the historical probe now fails closed and the English/Turkish validation, owner-independence and Alpha72 release notes mark removal unverified. Native add/update/transfer/daemon-restart observations remain scoped evidence. A fresh management-absent paired test must prove native secondary zone state after catalog removal and survive reboot before this P0.5 slice can close. No persisted schema, production recovery authority or installed server changed.

P0.5, constitutional invariants 3/6: the [archived native DNS pair reboot recheck](../deploy/e2e/dns-kill-matrix/NATIVE-DNS-PAIR-ARCHIVED-BOOT-RECHECK-20260925.md) is a failed acceptance attempt. Fresh child overlays of the September 12 BIND/PowerDNS pair both reached emergency mode after the archived celikpanel-firewall-restore.service failed and network-pre.target failed by dependency. SSH and paired DNS could not be checked. This does not establish a current-image firewall defect or management-absent pair continuity. The old parents remained unchanged and the child overlays were removed; a current-image paired boot/transfer/removal trial remains open.
P0.4/P0.5, constitutional invariants 1/3/6: the [current-image native BIND pair trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-CURRENT-20260925.md) added an honest paired-primary fixture with a panel-free Debian BIND catalog secondary. The first run refused to complete without an actual secondary; with one installed, the intent/after-write SIGKILL cell proved same-request target convergence and native catalog/member AXFR. After disabling primary Agent/Panel and rebooting both guests, native BIND remained active and both answered the member A record authoritatively over UDP/TCP. No persisted schema or production recovery authority changed. This does not prove removal, PowerDNS interoperability, catalog-member deletion, renewal, owner edits or the remaining P0.4/P0.5 matrix; both items remain open.

P0.4/P0.5, constitutional invariants 1/2/3/6: the BIND/PowerDNS
engine-neutral paired deletion verifier now treats a source-bound peer AXFR
REFUSED, NOTAUTH or NXDOMAIN as `no transfer`, never as proof that the zone
was unloaded. Completion additionally requires an exact authoritative
strict-parent negative SOA for the deleted name from the peer over both UDP
and TCP, with the local socket IPv4 matching the catalog transfer source.
Ambiguous or access-denied answers retain the same accepted V3 zone
operation for verification; they do not launch a second mutation or claim
success. The persisted DNS zone/engine/journal schemas and native service
ownership do not change. Focused and full Agent package tests exercise the
successful composite proof and REFUSED/TCP-only/wrong-source failure paths. This is a
fail-closed source correction, not a native deletion acceptance trial. A peer
that does not serve a suitable parent zone may remain pending until an
independent owner-verifiable native zone-state path is implemented. The
current-image paired add/transfer/reboot trial above used the previous
deletion verifier and did not perform a deletion; P0.4/P0.5 remain open.

P0.4/P0.5, constitutional invariants 1/3/6: the [fresh native BIND peer deletion and reboot trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PEER-DELETION-20260926.md) used a panel-free secondary and a fresh primary fault cell. After same-request recovery, a guarded owner-native primary catalog edit removed the only member with Agent/Panel disabled. The secondary transferred catalog serial 2 and `rndc zonestatus` confirmed the member zone was **not loaded** before and after both guests rebooted; both native BIND services remained active. A secondary `REFUSED` answer alone was not counted as deletion evidence. This is not a CelikPanel-mediated V3 deletion or production proof for the new composite peer verifier. With no authoritative parent zone, that verifier would retain the operation as pending; an independent owner-verifiable native zone-state path remains needed. No persisted v1 schema, production recovery authority or service ownership changed, and P0.4/P0.5 remain open.
P0.4/P0.5, constitutional invariants 1/2/3/6: the [managed BIND V3 deletion and management-absent reboot trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-PENDING-20260926.md) exposed a primary DNS outage when rollback paired proof failed after native secondary catalog removal. The correction uses the immutable primary catalog receipt and receipt-addressed local AXFR for local deletion evidence, leaves native BIND active when a locally restored rollback is peer-unverified, and permits exact peer proof to supersede optional NOTIFY failure. A fresh real V3 delete advanced both native BIND catalogs to serial 2, removed the secondary zone, preserved one exact pending mutation, and survived reboot with Agent/Panel disabled on the disposable primary. The secondary's `rndc zonestatus` proved the zone was not loaded; `REFUSED` alone did not. No persisted schema or native service ownership changed. Exact peer proof and completion, other failure cells, owner edits, PowerDNS and complete native workload/renewal acceptance remain open; P0.4/P0.5 are not closed.

P0.4/P0.5, constitutional invariants 1/2/3/6: the [terminal native BIND V3 deletion trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) adds a parent-authoritative disposable secondary with catalog AXFR limited to the primary. Two strict SOA validators now accept a syntactically valid one-label authority parent while still requiring a canonical child FQDN, exact ancestor and complete authoritative negative wire proof. The clean Arch/Debian pair passed the real SIGKILL cell; production V3 deletion then reached `verified_published/succeeded`, native secondary removal and management-disabled reboot continuity. No persisted schema or recovery authority changed. Parentless deletion, other engines and the full fault/workload matrix remain open; P0.4/P0.5 are not complete.

P0.4, constitutional invariants 1/2 and D-024: paired V3 propagation timeout
guidance now names the last fixed proof boundary (plan, catalog pair, peer zone
transfer or peer zone SOA) while retaining the peer administrator's next action
and same-operation verification. It does not persist raw probe errors, DNS
answers or credentials. The accepted mutation remains pending; polling does
not start another mutation. No persisted schema, authority, recovery executor
or native service ownership changed. A focused regression tests each deletion
boundary and secret exclusion; the Agent package tests pass. This status-only
change has not been rerun in the native pair. Parentless deletion, other engines
and the full P0.4 matrix remain open.
P0.4/P0.5, constitutional invariants 1/3/6: the [native BIND target-staged paired fault and reboot trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md) passed one fresh SIGKILL `target-staged/after-write` cell with exact same-request convergence and 31 healthy stability samples. A standard panel-free Debian BIND secondary transferred the member and retained authoritative UDP/TCP answers after both guests rebooted with primary management disabled. The sealed raw fault proof is retained; no persisted schema, recovery authority or native service ownership changed. This does not establish Agent-independent inverse, uninterrupted serving during the cut, later phase/source-policy coverage or complete P0.4/P0.5 acceptance.

P0.4, constitutional invariants 1/2/3: the [protected PowerDNS adoption owner-CLI trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-PROTECTED-OWNER-CLI-20260926.md) passed its bounded Debian 13 native acceptance on the same protected candidate. A real producer SIGKILL was followed by an owner-CLI SIGKILL at the durable rollback checkpoint, management-disabled reboot, same-request terminal recovery and exact journal retirement. A historical retry reported current health as unknown. A separate same-candidate owner edit was refused with evidence retained while native PowerDNS continued authoritative UDP/TCP answers. The worktree was dirty; this is not release-grade signed-candidate provenance. Other inverse kinds, platforms and the broader P0.4/native fault matrix remain open; no installed server changed.

P0.4/P0.5, constitutional invariants 1/2/3/6 and D-024: the
[native parentless BIND V3 deletion and inspector trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-INSPECTOR-CHANNEL-20260926.md)
used a disposable Arch primary and a panel-free Debian 13 BIND secondary.
An exact V3 delete reached catalog serial 2 and unloaded the last member, but
the accepted job stayed pending while the pinned SSH channel and four-record
empty-catalog parser were corrected. The same request/owner/qualifier then
recovered to a durable published success; the one-time challenge was retired
and terminal replay was rejected before a new mutation. With primary Panel and
Agent disabled, both guests rebooted and native BIND retained catalog serial 2,
the unloaded member and DNS service. No installed server was changed. The
existing service mutation ledger v1 `error_code` and optional V3 pending-code
response carry only reviewed reasons; no schema, recovery authority or native
service ownership changed. Component and native evidence covers this exact
parentless BIND path, not other engines, add/edit/delete combinations,
owner-edit races or the full fault/workload matrix; P0.4/P0.5 and Stage 2
remain open.
P0.4/P0.5, constitutional invariants 1/2/3 and D-024: the
[native BIND V3 owner-edit correction trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-OWNER-EDIT-CORRECTION-20260927.md)
retains the preceding failed cell as evidence. A managed-block owner edit after
an exact durable deletion had previously poisoned recovery, leaving the job
`running/recovering` with an expired lease. A typed managed-span conflict now
returns reviewed `dns_peer_owner_edit_unknown` only when the verified current
primary BIND tree, deletion tombstone, state generation and request/owner/qualifier
all match. The fresh disposable pair proved the same job returned to durable
`pending/propagation-pending` without a lease, file rewrite or DNS outage.
After explicit owner reconciliation, one same-identity recovery reached
`verified_published/succeeded` and retired the challenge without a duplicate
job. The existing ledger v1 and DNS state v2 formats did not change; unrelated
config errors still fail closed. This tests one between-attempt owner conflict,
not concurrent edits at every effect, post-terminal replay, other engines,
independent inverse or the complete Stage 2/P0.4/P0.5 matrix.
P0.4/P0.5, D-025 invariants 1/2/3/6: the [bounded BIND-primary/PowerDNS-secondary trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PDNS-PEER-STAGE2-20260927.md) proved initial native transfer, one terminal V3 edit, native delete observation and management-disabled reboot in disposable guests. The exact V3 delete remained pending because that trial had no authenticated PowerDNS loaded-zone absence proof; the observation and empty `REFUSED` replies do not authorize success. The [inverse PowerDNS-primary/BIND-secondary trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-BIND-PEER-STAGE2-20260927.md) is a failed attempt: exact producer SOA/base-record validation stopped the switch, staged-target rollback validation also failed, and the disposable primary DNS services were inactive while the secondary retained old answers. Evidence and exact operation state were preserved; no retry was issued. No durable schema, recovery authority or installed server changed. These reports narrow the cross-engine gaps but do not close Stage 2, P0.4 or P0.5. A separate fresh SSH trial below initially returned pending for its own deletion request and then resolved that same request; the first trial's pending result remains historical evidence. A separate fresh trial below addresses absent-state reboot. Still open: remaining measured faults; the [versioned PowerDNS-primary switch and rollback design](PDNS-PRIMARY-SWITCH-V3-DESIGN.md), its separately approved implementation and safe terminal/recovery proof for the inverse switch; and full native reboot/workload acceptance across supported engine pairs.

P0.4/P0.5, D-025 invariants 1/2/3/6 and D-024: the [owner-enrolled BIND-primary/PowerDNS-secondary SSH trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PDNS-SSH-STAGE2-20260927.md) retained an initial `dns_peer_inspection_unknown` pending delete. An authenticated native PowerDNS absence observation then supported one `Resume:true`/`RecoverDNSZoneV3` of the same request; exact succeeded ledger phase and retired challenge journal established terminal deletion. A distinct V3 re-add succeeded. After both disposable guests rebooted with primary Agent/Panel disabled, native BIND and panel-free PowerDNS remained active and the re-added member answered authoritatively over UDP/TCP. The absent-zone reboot was not run; the initial inspector failure lacks a stable narrower cause, recorded in the [guidance audit](DNS-PEER-INSPECTION-GUIDANCE-GAP.md). The V3 ledger and challenge schemas stayed v1; no installed panel changed. This bounded result closes neither the inverse PowerDNS-primary switch gap nor Stage 2/P0.4/P0.5 overall.

P0.4/P0.5, D-025 invariants 1/2/3/6: the [separate fresh BIND-primary/PowerDNS-secondary deleted-zone reboot trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PDNS-ABSENT-REBOOT-STAGE2-20260927.md) used new disposable overlays and an independently pinned peer enrollment. One production V3 delete reached exact `verified_published/succeeded` on its first attempt, retired its PowerDNS challenge, and left native catalog serial 2 with no member. Before and after reboot, `pdns_control list-zones` showed only the catalog and no deleted child; native SQLite member rows remained absent. Both native DNS daemons returned active/enabled after reboot while primary Agent/Panel stayed inactive/disabled. No re-add, retry or second delete occurred in this cell; the earlier presence-reboot and pending/recovery cell remains separate evidence. V3 ledger/journal schemas did not change, and no installed server changed. This closes only the tested absent-state reboot slice, not owner enrollment, inverse PowerDNS-primary switch, other pairs, interruption phases, other workloads or Stage 2/P0.4/P0.5 overall.

### September 27 scoped DNS recovery evidence

The [standalone Debian PowerDNS-to-BIND rollback trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md)
now exercises the protected independent owner CLI with genuine producer and
recovery-process interruptions, owner-edit refusal, and same-request completion
after an orderly reboot while Panel and Agent stay disabled. The source
PowerDNS database is verified, never replaced. This closes that native adapter
gap under P0.4; running-BIND adoption, PowerDNS switch/reinstall inverses and the
listed platform/topology variants remain open. It does not close P0.4 or prove
signed-release, automatic boot recovery, uninterrupted service or power-loss
durability.

### Running-BIND adoption inverse - bounded native evidence (2026-09-27)

P0.4, constitutional invariants 1/2/3/4: the [selected owner recovery CLI trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-ADOPTION-OWNER-CLI-20260927.md) proves one interrupted rollback of initial running-BIND adoption for a static default-view owner zone on Debian 13. The V2 journal carried `SourceBIND` and excluded `SourcePDNS`; ledger v1 and journal v2 remained the persisted formats. The tagged Agent received SIGKILL at `rolling-back/after-write` with exit 137. A same-serial owner A-record edit was refused while preserving the owner file, journal, ledger, pending install receipt and named process. After the harness restored only its own edit, the selected protected CLI was killed after durable `rolled-back`; same-request recovery retired the journal and wrote the rollback verdict. After that checkpoint and immediately before/after terminal retry, all engine/install ownership receipts were absent; the pending install receipt had been present and unchanged during owner-edit refusal. Named remained active at the same process identity. A historical retry made no effects and reported current health unknown. Separate UDP/TCP checks returned authoritative A `192.0.2.10`. The controller handoff result remains unverified because its matrix assertion expects forward convergence; the independent read-only probe validly reported `rolled_back_source_active` and `converged=false`. This slice does not prove a full RRset, reboot, continuous-availability SLO, other layouts/topologies or signed provenance. The earlier archive with SHA `6c9dcc30c2edb7c96b43c287efce461b8586170b38116a3272ca4235484d5a05` is provisional due to an adopted-present installation ownership receipt and is excluded. The fresh final archive SHA-256 is `aa296895caa0dd73bdaea881e64c7dbd6ab2ab52a3e676cfe0397014e2511707`. Overall item 1/P0.4 remain open.

### Journal-free DNS request receipt after management-disabled reboot (2026-09-28)

P0.2/P0.4, constitutional invariants 2/3: a bounded [fresh PowerDNS V3 prestart
inverse trial](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md)
restored one SIGKILL-interrupted, previously inactive/masked target with the
manifest-verified independent owner CLI. Ledger v1 retains the original switch
as `failed` with an exact owner-rollback verdict; the active V3 journal/state
were retired, and native inactive/masked state persisted after reboot with
management disabled. Two earlier clean trials refused before effects and led
to narrow cgroup and local-listener proof corrections. The installed status
observer could not take its missing volatile host lock after reboot. A new
root-only `dns-switch-status --request-id` source path reads only a terminal
journal-free historical ledger receipt without creating that lock or claiming
native health. It has focused tests, vet and a [disposable Debian post-reboot trial](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-recorded-status-20260928/README.md) using the earlier real rollback's exact canonical terminal ledger. That trial did not rerun the producer/inverse or installed kit.
No persisted schema or producer transition was introduced. Other cuts, owner
edits, migration, public admission and P0.4/P0.5 acceptance remain open.

### Candidate panel start check and post-start proof (2026-09-30)

P0.1/P0.2/P0.3, D-025 invariants 2, 3, 4 and 5. A candidate whose panel could not
start was detected late or not at all: one `systemctl is-active` after
`completion.pending`, no HTTP proof in the worker or runner final proof, and a
printed rollback command that `rollback.sh` refuses once recovery material exists.

- **Changed.** The updater runs a read-only `panel --check-startup-readiness`
  as the panel account after the isolated database publication and before
  `completion.pending`; a failure is a typed failure in phase `active`
  (`candidate_panel_startup_check_failed`) and the existing `update:active`
  recovery returns the previous release. After the real start, a bounded
  stability wait (unchanged main PID and restart counter for at least 5 s,
  beyond `RestartSec=3`, plus a loopback `401 AUTH_REQUIRED` from the panel,
  pinned to the checked public keys in the normal update) replaces the single
  `is-active`; its failure stays in `completion` (`panel_start_unverified`). The
  completion summary prints a rollback command only when `rollback.sh` would
  admit it. Root CLI, owner view and recovery screen explain both causes in
  EN/TR.
- **No schema or version transition.** Snapshot v6, material v3, marker grammar,
  kit protocol 1 and the eight-field v1 observation record are unchanged. The
  typed cause is an optional, additive `<id>.failure` sidecar
  (`celikpanel-recovery-failure/v1`, first cause wins) that older readers never
  open; new readers expose `failure_code` only while the update's own failure is
  the latest recorded failure. Older browsers ignore the extra JSON field.
- **Recovery behaviour.** Unchanged dispatch: `update:active` rolls back,
  `update:completion` completes forward. No new rollback after completion, no
  new marker or checkpoint.
- **Evidence.** Component and contract tests only; native run pending.

Open: a panel that passes the check and fails later is completed forward with no
supported return; no rollback after `completion.pending`; no live browser status
at the panel's address while the panel is stopped (owner SSH view only).

### Guidance after a verified rollback (P0.2, 2026-10-01)

P0.2 (truthful, actionable state), D-025 invariant 3 (typed evidence), D-024.
From the upd2 Debian 13 defective-candidate run (O1-O4).

- **Changed.** The failed-update notice maps the exact request's recovery
  observation to owner guidance (rollback verified: both versions, typed or
  generic cause, who acts, next action, resume; otherwise the recovery screen's
  state texts) and keeps the worker summary as a secondary "server reported"
  line. The update check adds `previous_attempt` for the offered target commit
  from the native observations (read-only, no Agent RPC, bounded directory scan,
  malformed or foreign records ignored). The root CLI speaks plainly first and
  moves tokens to a final "Recorded state (for support)" line. The rollback
  journal prints the restored commit from the manifest-covered Agent build record
  when it binds the installed Agent bytes. The start guard names its hold. A
  recovery paused on its retry limit also names the update's first typed cause
  (read from the existing failure sidecar) before the pause guidance.
- **No schema or version transition.** Observation v1, the failure sidecar v1,
  snapshot v6, material v3, marker grammar, kit protocol 1, the browser's stored
  update record and the CLI `--json` bytes are unchanged. The only wire addition
  is the optional `previous_attempt` field of `GET /api/v1/panel/update/check`,
  plus the optional `first_failure_code` of the recovery status (appended last,
  present only while automatic recovery is paused and the sidecar names a typed
  cause; also in the CLI `--json` for that state only); older Panels omit them
  and older browsers ignore them. The start guard's bytes
  change, so the value (not the format) of the foundation manifest's
  `start-guard-sha256` changes for releases built from this source.
- **Recovery behaviour.** Unchanged: `update:active` rolls back,
  `update:completion` completes forward; the guard admits and refuses exactly
  as before with the same exit code; Start remains the owner's decision.
- **Evidence.** Component tests only; native run pending.

### Preflight cause, retries between attempts and renewal at the pause (P0.1/P0.2/P0.5, 2026-10-01)

D-025 invariants 1 (independent renewal must not stay suspended), 2 (typed,
preserved evidence), 3 and 4 (bounded retry of a read; retry exhaustion as a
durable actionable state); D-022, D-024. From the upd3 native run (F1, F2, F3, O6).

- **Changed.**
  - F1: `recovery verify-compatibility` returns `step=<step>: <first checker
    line>` (bounded printable ASCII; internal runtime, lock and metadata errors
    stay private and are named only by their step). `update.sh` captures the four
    selected-runtime preflight steps, reports `code=recovery_runtime_preflight_failed
    state=unchanged` although the EXIT trap does not exist yet, and writes the
    failure sidecar with that code (commit from the verified target while no
    snapshot name exists). A failed record with that code is final for its request.
  - F1 race: established by code reading, not observed on the host. The
    WAL-aware checker copies the live database and WAL without opening them, then
    fails closed when the database, `-wal` or `-shm` metadata changed after
    pinning (`verifyPath` compares size, mtime and ctime). Any Panel commit,
    checkpoint or shared-memory write in that window refuses the check, and the
    preflight runs while the Panel is live. A consistent snapshot is not possible
    here without opening the source or stopping the Panel, which the preflight
    must not do, so a refused checker is read once more after a bounded 2 s pause
    (never after a timeout). The same single re-read applies to the promotion
    compatibility check. The frozen, locked checks later in the update are
    unchanged.
  - F3: a failed automatic attempt with attempts left writes the optional
    `celikpanel-recovery-automatic/v1` value `retry_scheduled` (only on a
    `recovery_required/recovery_failed` record). Readers expose the update's first
    typed cause while retrying (`retry_scheduled`, and `recovering` after a
    recovery failure) as well as at the pause.
  - F2: when the last admitted attempt (the third automatic one or an owner
    retry) of a forward completion (`update:completion`, `completion-scheduler`,
    `scheduler`) fails, the runner returns the Certbot scheduler to its recorded
    pre-update state with the existing restore function, once, under the release
    lock. If it already matches, nothing is changed; a refused restore (for
    example a later owner change of its enablement) is reported, never forced.
    The forward retry pauses the scheduler again right after validating the
    snapshot and before stopping any coordinator; the existing quiesce proof
    refuses a changed enablement. Rollback directions (`update:active`,
    `rollback:*`) keep renewal paused and say why: the rollback restores the panel
    TLS tree, the pending activation and the deploy hook from the snapshot, so a
    renewal before the retry would be undone. Code evidence: the forward retry
    (`validate_pending_update_snapshot`, `verify_installed_release_artifacts`,
    `VerifyInstalledCompletion`) validates only the snapshot copy and never
    compares or restores live certificate files, while `panel_tls_restore_snapshot`
    replaces the live TLS tree in the rollback path.
  - O6: the notice's secondary server line drops internal tokens.
- **Schema or version transition: none.** Observation v1, the failure sidecar v1
  grammar, the automatic hint v1 grammar, snapshot v6, material v3, marker
  grammar, dispatch receipts v1 and kit protocol 1 are unchanged. New closed
  values: failure code `recovery_runtime_preflight_failed` and automatic value
  `retry_scheduled`; older readers ignore both and show generic text. The status
  JSON keeps every key and its position; `first_failure_code` may now also
  appear while retrying. The runner, updater and `panel-tls-snapshot.sh` bytes
  change, so the values (not the format) of the foundation and kit manifests
  change.
- **Recovery behaviour.** Dispatch, the budget (three automatic admissions plus
  explicit owner retries) and terminal proofs are unchanged. New mutations: the
  scheduler restore after a failed final forward attempt and the re-pause at the
  retry's start, both through existing verified functions.
- **Evidence.** Component and contract tests only; native run pending.

Open: a paused rollback leaves renewal stopped until its retry finishes; the
standalone `deploy/finalize-pending-update.sh` does not re-pause renewal at its
start; the preflight-stop notice needs an installed Panel that contains this
change; die lines before the EXIT trap other than these four steps still carry no
typed summary (for example `*_runtime_preparation_unconfirmed`).

#### Start limit on the owner's continuation (same date)

`celikpanel-panel.service` restarts on failure with `StartLimitBurst=30` per
3600 s, and no lifecycle script reset the unit's counter. After three forward
attempts with a crash-looping candidate, the owner's printed retry could be
refused with "start request repeated too quickly". Each script's existing
`systemctl` wrapper now routes exactly `start celikpanel-panel.service` and
`start celikpanel-agent.service` through `release_unit_controlled_start`, which
runs `reset-failed` for that unit only and then starts it. A start still refused
with `Result=start-limit-hit` prints `code=unit_start_limit_hit` with the unit
and `sudo systemctl reset-failed <unit>` followed by the retry, EN and TR; the
updater also carries the code in its summary. Start sites covered: `update.sh`
(normal controlled starts, pending/forward completion including owner retry,
pre-mutation restart), `rollback.sh` (restored starts, restart of the previous
services), `deploy/finalize-pending-update.sh`, `deploy/finalize-pending-rollback.sh`
and `deploy/abort-pre-mutation-active-update.sh`. The recovery runner and the Go
recovery runtime start neither unit (they dispatch these scripts). `install.sh`
restarts only on a fresh install (apply-only mode exits before them) and is
unchanged. No schema transition; contract test `deploy/test-unit-start-limit-contract.sh`.
The code is not an observation value; the owner reads it in the recovery journal.

### Refused update checks, snapshot cause, pending pause and renewal state (P0.1/P0.2/P0.5, 2026-10-01)

D-025 invariants 2 (typed, preserved evidence), 3 (stop at the affected
boundary), 4 (bounded retry of a read; retry exhaustion as a durable actionable
state) and 1 (renewal is an owner workload); D-022, D-024. From the upd4 native run
(F4, F5, F6, O7, O8, O9; one sample each).

- **Changed.**
  - F4: the WAL-aware idle check now types its one concurrent-write refusal: the
    live database (same file, written) or its `-wal`/`-shm` (written, removed on a
    last close, recreated with the same owner and mode) changed while it was read.
    Only that refusal, returned directly (not joined with another error, not from
    the private copy), exits 75; every other refusal, including a busy queue, a
    rollback journal, a replaced or re-moded file, exits 1 with an unchanged
    message. `update.sh` reads its two preliminary live panel probes once more
    after 2 s on exit 75 only (`run_update_live_panel_probe`); the frozen, stopped
    and snapshot proofs never re-read, and the compatibility checker keeps its
    a6dd5b1e rule. Every read-only check that refuses before the freeze (the
    preliminary panel and Agent idle probes, the Agent probe under the lock, BIND
    and mail/DNS compatibility, the bootstrap state) reports
    `update_preflight_refused` with `step=` (`idle_probe`, `agent_idle`,
    `bind_compatibility`, `application_compatibility`, `bootstrap_state`) and
    `class=` (`concurrent_write`, `operation_active`, `check_failed`) and the
    checker's first diagnostic line, but only when the outcome is
    `state=unchanged`; a failed quiesce abort or a resumed active phase keeps
    `update_failed`. The failure sidecar records the code; it is terminal for the
    request like `recovery_runtime_preflight_failed`. The Panel's summary
    sanitizer no longer drops a reviewed updater line that is over 240 bytes or
    contains `/`: it returns a form built only from closed tokens (allowlisted
    code, state, `[a-z_]` step and class, a free reason only when it is itself
    short and plain) and never copies the detail. It still drops unknown codes,
    paths, controls and URLs; the full line stays in the Agent journal.
  - F5: the transaction-consistent snapshot and the rescue snapshot run through
    the capturing probe; on failure the tool's first diagnostic line (timestamp
    removed, printable ASCII, 240 bytes; the start banner is skipped) is printed
    and becomes `detail=` of the failure line, which the Agent logs and stores in
    the update status. Who could write the database after the freeze is not
    established: the Panel is killed and its cgroup proven empty, the Agent is
    stopped, the snapshot quarantines the directory to root 0700 and refuses any
    process with the panel UID or an open handle, but root processes (the
    recovery timer's runner, owner tools) are not excluded by evidence, and the
    upd4 failure's own cause was not recorded. So there is no re-read there; the
    next run records the cause.
  - F6: when the third automatic attempt or an owner retry fails, the runner
    writes the optional automatic value `pause_pending` (only on a
    `recovery_required/recovery_failed` record) and says in the journal that the
    next timer run records the pause. Readers keep `first_failure_code` and say
    recovery is finishing its last attempt; "the server owner must act" appears
    only with the recorded pause.
  - O8: the updater writes the optional sidecar `<id>.renewal`
    (`celikpanel-recovery-renewal/v1`: `renewal_before_update=on|off`, first value
    wins) right after it adds the Certbot timers to its service ledger. Readers
    expose it as `renewal_before_update` only at the pause; `off` replaces the
    "renewal was stopped for this update" sentence.
  - O7/O9: texts only (see the operation guidance entry of the same date).
- **Schema or version transition: none for existing artifacts.** Observation v1,
  the failure sidecar v1 grammar, the automatic hint v1 grammar, snapshot v6,
  material v3, markers, dispatch receipts and kit protocol 1 are unchanged. New
  closed values: failure code `update_preflight_refused`, automatic value
  `pause_pending`; older readers ignore both (generic text, as before). One new
  optional sidecar, `celikpanel-recovery-renewal/v1`, that older readers never
  open. The status JSON keeps every key and position; `renewal_before_update` is
  appended last and present only at the pause. The panel checker's exit status
  changes from 1 to 75 for the concurrent-write refusal only. Script and binary
  bytes change, so manifest values (not formats) change.
- **Recovery behaviour.** Unchanged: dispatch, the three-attempt budget, the
  pause, owner retry, rollback and forward completion, and every frozen proof.
  The only new automatic action is one 2 s re-read of a read-only live probe
  before any coordinator is frozen; a pre-freeze refusal aborts the quiesce as
  before.
- **Evidence.** Go, shell contract and web tests only
  (`deploy/test-update-preflight-refusal-contract.sh`, the renewal pause contract,
  `TestConcurrentPanelWriteIsTheOnlyRetryableIdleRefusal`,
  `TestPanelUpdateSummaryKeepsABoundedReviewedForm`, the recoveryobs and CLI
  tests); native run pending.

Open: the F4 trigger (which Panel write) and the F5 cause are not identified;
the compatibility checker still re-reads any non-timeout refusal once; the
refused-check texts live in the server screen catalogue (generic reason until it
arrives); the web screens omit the O9 history line (the SSH owner view has it; the boot catalogue has no room).

### Roadmap item 3 status: closed on 2026-10-01 with named limits (P0.1/P0.2/P0.3/P0.5)

D-025 invariants 1 to 6; D-022, D-024. This is an assessment of retained evidence,
not a product change: no schema or version transition and no recovery behaviour
changes with this entry. **No P0 item is closed by it.** P0.1, P0.2, P0.3 and P0.5
stay partial; the table above is unchanged. Every run recorded
`native_evidence: false` in its own result and left the judgement to this entry.

**What was measured.** Six runs of the owner-started update driver (upd1 reached
no update and proves nothing; upd2 to upd6 measured updates)
([upd1](../deploy/e2e/release-recovery/evidence/upd1-20261001/README.md),
[upd2](../deploy/e2e/release-recovery/evidence/upd2-20261001/README.md),
[upd3](../deploy/e2e/release-recovery/evidence/upd3-20261001/README.md),
[upd4](../deploy/e2e/release-recovery/evidence/upd4-20261001/README.md),
[upd5](../deploy/e2e/release-recovery/evidence/upd5-20261002/README.md),
[upd6](../deploy/e2e/release-recovery/evidence/upd6-20261002/README.md)) on
disposable Debian 13 and Arch guests. In every cell the driver logs in as the
owner and starts the update through the Panel's update-start API, the endpoint
the update screen uses (no browser); the candidate comes from a
guest-loopback origin, signed with a fixture key, under the D-027 acceptance
license; DNS is external. One request id is followed through the Panel API, the
root CLI and the recovery records.

| Roadmap exit condition | Result | Evidence and limit |
|---|---|---|
| Owner UI update admission | Measured, Debian 13 and Arch | Good candidate installed and verified (Debian 13: upd2, upd4, upd5; Arch: upd3, upd4, upd5; the Debian cell of upd3 stopped in preflight and was fixed in source). Fixture signing key and loopback origin; production signing and the real origin are not exercised. |
| Failed candidate, automatic rollback | Measured, both platforms | A candidate that fails before `completion.pending` (migration defect; start-check refusal) returns to the previous release without owner action (migration defect: Debian upd2 to upd4, Arch upd3 and upd4; start check: both platforms, upd3 and upd4). |
| A second recovery fault | Measured, one boundary per platform | Debian: VM reset at `payload_restored` (upd2 to upd4); Arch: SIGKILL at `runtime_verified` (upd3, upd4); the same rollback completes. Other checkpoints, power loss and a fault during an owner retry are not measured. |
| A candidate that fails after `completion.pending` | Measured, both platforms | Three automatic forward attempts, `retry_scheduled`, `pause_pending`, pause at `paused_retry_limit` with the first failure code kept (real-start cells: upd5 on both platforms; the same sequence precedes the owner retry in the owner-continuation cells, Debian upd5 and Arch upd6). The product completes forward; it does not roll back after `completion.pending` (named limit). |
| Owner continuation | Measured, both platforms on the newest measured build (`6cda60b8`) | The printed `recovery recover --retry --snapshot …` run once after the owner removed the cause ends `succeeded/update_verified` (Debian upd4/upd5, Arch upd4/upd6). The cause was one the owner can remove (a held port). |
| Authenticated guidance agrees on one operation | Measured with a gap | Panel API, root CLI and records agree whenever the Panel answers. While the Panel is stopped only the root CLI over SSH shows the state; screens at the pause are rendered from the build's source, not from a browser. |
| Preserved native workloads | Measured for the seeded set | Cron was never interrupted. The site was never interrupted except in the five Debian cells with a second fault, where site and SMTP were unavailable for up to about 22 s around the VM reset. Database rows equal apart from self-changing tables; firewall preserved; renewal timer state preserved from upd4 on (lost at the pause in upd3, fixed in source; disabled before the update on Arch). SMTP seeded on Debian only. |
| Operation with management off across a reboot (P0.5) | Measured, both platforms | Panel and Agent disabled, one orderly reboot (upd4, second run of each cell; the first runs failed on harness defects and are retained): site, database row, cron and firewall rules served or present; SMTP on Debian. The renewal timer kept its pre-reboot state (active on Debian, disabled on Arch). The firewall comes from a restore unit that the management-off step does not disable. DNS for this condition is in the item 2 pair evidence. Certificate issuance or renewal itself was not executed. |

**Product defects these runs found and closed in source** (each has its own dated
entry above or in the operation guidance): cron as a setup component and the
Arch mail refusal at plan review; the candidate start check before
`completion.pending` and the post-start stability wait; the hosting root
traversal receipt; the preflight cause and unchanged terminal state; Certbot
timers restored at a forward pause; the start-limit reset before every controlled
start; the typed pre-change refusal, snapshot cause, `pause_pending` and the
renewal sidecar.

**Named limits of this closing (they stay open under their P0 item):**

- Platforms: Debian 13 and Arch only. No Ubuntu evidence; the RHEL family stays a
  blocked preview. Mail on Arch is unsupported and refused at plan review.
- Few repetitions per kind on a laptop host, and the kinds were last measured at
  different builds (migration defect, start check and management-off at
  `a6dd5b1e`; good, real-start and owner continuation at `6cda60b8`); fixture signing key, loopback origin and the
  acceptance license. Production signing, the real origin and the license
  service are not exercised.
- Not triggered natively in any run: the 2 s re-read of the live idle probe, the
  typed `update_preflight_refused` path and its typed server line on the
  update card, the snapshot cause line, `unit_start_limit_hit`.
- No power-loss cell; one second-fault boundary per platform; no second fault
  during an owner retry; no cause the owner cannot remove.
- A Panel that starts and fails later is completed forward; there is no rollback
  after `completion.pending`.
- No live status at the Panel's address while the Panel is stopped; no browser
  took part in any run, so rendering of the update, pause and failure screens is
  not captured.
- A paused rollback keeps renewal paused until the owner acts.
- No panel uninstall path and no removal claim; management-disabled is the only
  absence condition measured.
- The upgrade starts from a baseline built from the same source line, not from
  the published alpha.80 archive; older published schemas are covered only by
  the earlier scoped trials listed in the table above.
- 18 root-only packaging contract tests were not run as root on the build host
  for the 2026-10-01 archive content change.

No installed server was touched. This closing authorises no installed-panel
update and no release.

### Candidate review corrections (P0.1/P0.2/P0.5, 2026-10-01)

D-025 invariants 1 (renewal is an owner workload), 2 (typed, preserved
evidence), 3 (stop at the affected boundary) and 4 (retry exhaustion as a
durable, actionable state); D-022, D-024. From a read-only review of the
candidate (F1-F4, N1-N3); no native run.

- **Changed.**
  - F1 (upgrade from the published `v0.1.0-alpha.80`): that release's Agent
    writes no `<id>.status`, so the candidate's binding, typed cause, renewal
    state and every recovery state of the request stayed unrecorded and the root
    CLI said "unknown". Identity in that path: alpha.80's Agent runs its worker as
    the transient unit `celikpanel-self-update-<request id>.service` (an id it
    validated as 32 hex), and its `get.sh`, the bootstrap and the candidate
    `update.sh` stay in that unit's cgroup; the commit is the one the worker passed
    as `--expected-commit` and the updater verified. Right before the binding,
    `update.sh` now writes the initial `running` record through the new create-only
    `release_observation_publish_initial` (the existing publisher under its lock;
    it returns 3 and changes nothing when any record of the request exists), only
    when both are established. When the updater wrote that record, it also
    records the `failed` transition at its own failure exit, as the current
    Agent's worker does. Without a worker identity (an owner's own run, an
    unsupported cgroup layout) nothing is written; the updater's journal line,
    the runner's unbound line and the root CLI's unknown-state text now name
    `sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50` as
    the place of the attempts and of a pause's retry command.
  - F2: `preflight_staged_installer_runtime` requires the exact `/usr/bin/curl`
    (`CURL_BIN`) of the post-start proof before any change, with the neighbouring
    refusal text. `rollback.sh`, `deploy/finalize-pending-update.sh`,
    `deploy/finalize-pending-rollback.sh` and
    `deploy/abort-pre-mutation-active-update.sh` use no curl or other HTTP probe.
  - F3: a quiesce recovery aborts the frozen update and exits 1 by design; the
    runner counted that as a failed attempt (`retry_scheduled` or
    `pause_pending`, never resolved). After a failed child the runner now checks
    the markers; when none remains it publishes no hint, restores nothing and
    skips the retry handling. For `update:quiesce` it records
    `failed/update_failed` (the update ended before the release or its data
    changed) and says so in the journal; any other phase keeps the exit hook's
    plain `recovery_required/recovery_failed` (an unverified end) with owner
    guidance. The consumed dispatch receipt stays (receipts are per snapshot).
  - F4: the candidate start check accepted only an IP literal, an empty host or
    `localhost` and refused leading-zero ports, while `install.sh`
    `valid_panel_listen` and `net.Listen` accept `host.example:2083` and `02083`;
    every update of such a panel rolled back with `listen_address_invalid`. The
    check now accepts the install grammar (empty host, a name or IPv4 literal
    `[A-Za-z0-9._-]+`, a bracketed IPv6 literal; a port of 1 to 5 digits in
    1..65535). It resolves no name: the previous release already listens on that
    value, a resolver outage must not cause a rollback, and a name that does not
    resolve at the real start is caught by the post-start proof. The updater's
    loopback target accepts `_` in a name, as the install grammar does.
  - N1: an owner retry admitted while automatic attempts remain gives the normal
    retry hint (`retry_scheduled`) and leaves renewal alone; `pause_pending` and
    the renewal restore apply only when the three automatic attempts are used.
  - N2: the start check's typed code is set only after the panel environment is
    parsed; an entry the reader refuses keeps `update_failed` with a plain detail
    saying the new panel was not checked. The phase is still `active`, so the
    automatic rollback is unchanged.
  - N3: the recovery runtime preflight line says "the installed release and its
    data were not changed" and has a Turkish line; the recovery kit may already
    have been promoted at that point.
- **Schema or version transition: none.** Observation v1, the failure, renewal
  and automatic sidecars v1, snapshot v6, material v3, markers, dispatch receipts
  and kit protocol 1 are unchanged. No new closed value: the updater's own record
  uses `running/update_running` and `failed/update_failed`, byte for byte the
  Agent's encoding, and the quiesce end uses `failed/update_failed`. The CLI
  `--json` bytes are unchanged; its unknown-state text gains one sentence. Script
  and binary bytes change, so manifest values (not formats) change.
- **Recovery behaviour.** Dispatch, the three-attempt budget, rollback, forward
  completion and every proof are unchanged. An aborted quiesce no longer looks
  like a pending retry; an early owner retry no longer pauses or restores
  renewal; a host without `/usr/bin/curl` stops before any change; a host-name or
  leading-zero listen address no longer forces a rollback.
- **Evidence.** Component and contract tests only:
  `deploy/test-release-recovery-observation.sh`,
  `deploy/test-update-failure-report.sh`,
  `deploy/test-update-panel-start-readiness.sh`,
  `deploy/test-recovery-renewal-pause-contract.sh`,
  `deploy/test-recovery-runtime-shell-contract.sh`,
  `TestUpdaterInitialRecordForAHistoricalWorker`,
  `TestUnavailableStatusNamesTheRecoveryJournal`,
  `TestStartupReadinessAcceptsInstalledHostNameAndLeadingZeroPort`,
  `TestStartupListenAddressFollowsTheInstallGrammar`. Native run pending; no
  alpha.80 archive was upgraded.

Open: the upgrade from the published alpha.80 archive is not run natively; with
an alpha.80 worker a successful update's record stays `running` until the
candidate Agent first reconciles that request's status (the updater claims no
terminal proof); the CLI's scheduled-retry text says "the last automatic
attempt" also after an early owner retry; a non-quiesce child that fails without
leaving a marker ends at `recovery_required` with no pause record; the web texts
`panelUpdate.previousAttempt.stoppedTitle`/`stopped` ("stopped before changing
anything installed") and `panelUpdate.packageManagerBusy` ("stopped before
changing installed files") are not adjusted (web out of scope). Corrected on
2026-10-01 (`706c1c91`): both texts now say the installed version and its data
were not changed.

### Update from the alpha.80 source to the candidate: scoped native evidence (P0.1/P0.2/P0.3, 2026-10-01)

D-025 invariants 2, 4 and 5; D-024. Evidence only: no product change, no schema
or version transition, no recovery behaviour change. P0.1, P0.2 and P0.3 stay
partial.

[upd7](../deploy/e2e/release-recovery/evidence/upd7-20261001/README.md) is the
first run whose baseline is the published `v0.1.0-alpha.80` source rather than
the current source line. The baseline is the tag rebuilt with a six-file license
test seam; the update, rollback, bootstrap, get and release scripts, `deploy/`,
`web/` and `cmd/agent` are identical to the tag
(`build/baseline-ref-proof.txt`). It is not the signed release archive. The
candidate is `48d21d58`, which carries the candidate review corrections above.

- **Measured on Debian 13, one run each.** A good candidate was installed and
  verified. With the new Panel prevented from starting, recovery made three
  forward attempts, showed `retry_scheduled` and `pause_pending`, paused with
  `first_failure_code=panel_start_unverified`, and the printed owner retry, run
  once after the owner removed the cause, ended `succeeded/update_verified`. A
  candidate with a migration defect, with a VM reset at `payload_restored`,
  returned to alpha.80 (`rollback_verified`); the alpha.80 Panel and Agent
  started and served with the candidate's records on disk.
- **The initial record.** The root CLI read `running` while the alpha.80 worker
  was still active. That is consistent with the record the candidate updater now
  creates for a worker that writes none; no evidence file names the writer, so
  this is an inference. The record did not stay `running` after success in this
  one run.
- **Owner-visible gaps found (limits of the first upgrade, not fixable in the
  installed alpha.80).** After an automatic return, the alpha.80 Panel shows the
  raw failure line although the server is `rollback_verified`, has no recovery
  reader, and offers the same version again; only the root CLI says not to start
  it. For about 11 s after the start the recovery CLI does not exist yet, and for
  about 6 s more it reads `observation=unavailable`.
- **Not measured.** Any Arch path from alpha.80 (two failed attempts are
  retained: the mail profile on the first, a seeded site answering 404 before the
  update on the second; cause not established). The signed alpha.80 archive
  itself, cron continuity on this baseline, the start-check kind, management off,
  production signing, the license service, a browser.

### An idle PackageKit daemon is not package-manager activity (P0.1/P0.2, 2026-10-01)

D-025 invariants 1 (the owner's PackageKit service is only read, never stopped),
2 (busy, idle and unanswered stay distinct; unanswered keeps busy) and 3 (stop
at the actual boundary: the apt/dpkg lock, not a daemon's existence); D-022,
D-024. From [upd8](../deploy/e2e/release-recovery/evidence/upd8-20261001/README.md)
F1/F2 (measured on alpha.80 only). Source reading: HEAD had the same rule, so its
setup on stock Ubuntu 24.04 would hit the same sequence (the product's apt run,
then `packagekitd` idle for about 300 s, then the next step refused); not
measured.

- **Changed.**
  - One rule, in the Agent (`linuxPackageProcessBusyAt`): every call site uses
    it - mutation admission (`HOST_MUTATION_BUSY`), the readiness read, update
    start admission, orphan recovery, mail enrollment admission and the
    `--check-*-idle` probes that `update.sh`, `rollback.sh`, the finalizers and
    the recovery runtime run. No product shell script keeps its own process
    list (new `deploy/test-package-activity-rule-contract.sh`).
  - `packagekitd` counts as busy only with transaction evidence: it does not
    run the APT backend (`libpk_backend_aptcc.so` absent from its maps), has a
    child process (apt fetch methods, dpkg), or holds or waits for a lock on
    `/var/lib/dpkg/lock-frontend`, `/var/lib/dpkg/lock`,
    `/var/cache/apt/archives/lock` or `/var/lib/apt/lists/lock` (`/proc/locks`;
    an unattributed lock on those files counts). An unreadable maps, status or
    lock table, or an unparseable line, keeps busy. Only `/proc` is read;
    PackageKit is never contacted, stopped or signalled (no D-Bus call, which
    could activate the daemon or reset its idle timer). Every other listed name
    and the dpkg/apt/rpm/pacman lock probes are unchanged.
  - The Agent's admission refusal for package activity (and the mail
    enrollment's) now carries the existing reason `package_manager_active`
    (same sentinel and text), so the Panel shows the package-manager sentence.
  - A mail profile sub-step and a setup step refused with `HOST_MUTATION_BUSY`
    keep that code and the reason's sentence instead of
    `mail_profile_install_failed` / `server_setup_firewall_failed` (and the
    other step codes). Joined or other causes keep the step code.
- **Schema or version transition: none.** No persisted value, wire field,
  closed value or text key is new; `HOST_MUTATION_BUSY` and
  `package_manager_active` exist. Agent and Panel binary bytes change.
- **Recovery behaviour.** Unchanged. A real transaction, a held lock or an
  unanswered question still refuses exactly as before; nothing waits or
  retries in addition. The update, rollback and finalizer probes stop being
  refused by an idle daemon.
- **Evidence.** Component and contract tests only:
  `TestPackageKitIdleDaemonIsNotPackageActivity`,
  `TestPackageKitTransactionIsPackageActivity`,
  `TestPackageKitUnanswerableQuestionStaysBusy`,
  `TestBeginRefusalForPackageActivityNamesThePackageManager`,
  `TestSetupFailuresKeepTheHostBusyCause`,
  `deploy/test-package-activity-rule-contract.sh`. Native Ubuntu run pending.

Open: not run on Ubuntu (whether an idle Ubuntu `packagekitd` holds none of
these locks and has no child is inferred from PackageKit's APT backend, not
measured); a PackageKit transaction between its phases (resolving before it
takes a lock) is not seen, the locks remain the exclusion; non-APT PackageKit
backends stay busy while the daemon runs; a setup step refused for real package
activity still fails and needs a new reviewed plan (no bounded wait); the setup
wizard maps no headline to `HOST_MUTATION_BUSY` (it shows its generic
"needs attention" text with the sentence under details; web out of scope);
which package task blocks is not named.

**Correction, 2026-10-01 (upd9).** The entry above names the wrong backend file.
[upd9](../deploy/e2e/release-recovery/evidence/upd9-20261001/README.md) measured
`efcba145` on stock Ubuntu 24.04: its PackageKit 1.2.8-2ubuntu1.5 maps the APT
backend as `/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so`
(`BackendName 'apt'`), not `libpk_backend_aptcc.so` (`grep -c aptcc` 0 in all 88
readings of a running daemon). The rule therefore never recognised an Ubuntu 24.04
daemon and an idle `packagekitd` stayed busy for its whole ~305 s life: setup
refused 8 s after the start of a fresh install, seven owner attempts, and an
update start refused until the daemon quit (upd9 F1). The no-child and no-lock
conditions held in every reading. The component test passed because its fixture
used the same wrong name.

- **Changed.** The daemon runs the APT backend when at least one file mapped
  from a `packagekit-backend` directory exists and every such file has the exact
  base name `libpk_backend_apt.so` (measured) or `libpk_backend_aptcc.so` (the
  same apt-pkg backend under its name before upstream renamed it "apt"; kept for
  older releases, not measured). The pathname column of each maps line is
  compared, not a suffix: a lookalike name (`libpk_backend_apt-x.so`), that name
  in another directory, a non-absolute or unclean path, a replaced file
  (` (deleted)`), any other backend beside it, or no backend at all keeps busy.
  dnf, zypp, alpm and every other backend stay busy; unreadable evidence stays
  busy. The no-child and no-lock conditions are unchanged.
- **Also changed (upd9 F2, F3).** The Agent's update-start reply carries the
  typed reason (`SystemUpdateStartResponse.Reason`, additive, empty from an older
  Agent), and the Panel answers a start refused for package activity with
  `HOST_MUTATION_BUSY` / `package_manager_active` and that reason's sentence
  instead of the generic `PANEL_UPDATE_START_REFUSED`; every other refusal is
  unchanged. The Panel's sentence for that reason no longer says "something
  outside CelikPanel" or "a minute" (the task can be CelikPanel's own previous
  step; its length is not known).
- **Schema or version transition.** One additive wire field (above); no
  persisted value. Agent and Panel binary bytes change.
- **Recovery behaviour.** Unchanged; an idle Ubuntu daemon no longer refuses.
- **Evidence.** Component tests only: the fixture now uses the pathnames upd9
  read from the daemon (`TestPackageKitIdleUbuntu2404DaemonIsNotPackageActivity`
  fails on `efcba145`), `TestPackageKitIdleLegacyAptccDaemonIsNotPackageActivity`,
  new lookalike/other-backend cases in
  `TestPackageKitUnanswerableQuestionStaysBusy`,
  `TestSystemUpdateStartRefusalForPackageActivityCarriesTheReason`,
  `TestPanelUpdateStartRefusedForPackageActivityNamesThePackageManager`. The
  native Ubuntu 24.04 re-run is pending; Debian 13's and other releases' backend
  names are not measured. The setup wizard headline gap above was closed by
  `fb04289b`.

### Ubuntu 24.04: update from the alpha.80 source and the package-activity rule, scoped native evidence (P0.1/P0.2/P0.3, 2026-10-02)

D-025 invariants 1, 3 and 6; D-022, D-024. Evidence only for the changes already
recorded above (`efcba145`, corrected by `c855a757`); no further product change,
no schema or version transition. P0.1, P0.2 and P0.3 stay partial.

- **Update from the alpha.80 source
  ([upd8](../deploy/e2e/release-recovery/evidence/upd8-20261001/README.md)).**
  First update evidence on Ubuntu, one final run per cell, baseline as in upd7
  (the tag rebuilt with the license test seam, scripts and Agent identical to the
  tag). A good candidate was installed and verified; with the new Panel prevented
  from starting, three forward attempts, `retry_scheduled`, `pause_pending`, the
  pause with `first_failure_code=panel_start_unverified`, and the printed owner
  retry run once ended `succeeded/update_verified`; a candidate with a migration
  defect and a VM reset at `payload_restored` returned to alpha.80
  (`rollback_verified`) with its Panel and Agent running. Site, SMTP and cron
  were interrupted only by the injected reset. The owner-visible gaps of upd7
  (raw failure line and the same version offered again by the alpha.80 Panel
  after a return; no recovery CLI for the first seconds) repeat here.
- **What Ubuntu exposed.** On stock Ubuntu 24.04 apt starts `packagekitd` after
  every package operation and the daemon stays idle for about 300 s. alpha.80
  counts it as package-manager activity, so its setup needed seven owner attempts
  (about 45 minutes) per cell. The good cell ran four times; the first three stopped
  at setup because the harness did not yet wait as an owner would, and are
  retained.
- **The first rule did not hold
  ([upd9](../deploy/e2e/release-recovery/evidence/upd9-20261001/README.md)).**
  `efcba145` looked for `libpk_backend_aptcc.so`; Ubuntu 24.04 maps
  `packagekit-backend/libpk_backend_apt.so`. The candidate's own setup was
  refused 8 s after its start and an update start was refused while the daemon
  only idled. Component tests had passed because their fixture carried the same
  wrong name.
- **The corrected rule holds
  ([upd10](../deploy/e2e/release-recovery/evidence/upd10-20261001/README.md),
  built from `c67d1861`).** The candidate's setup (`web_mail`, ordinary owner, no
  waiting) reached the isolated host's `access_dns` wait in one attempt with no
  refusal while `packagekitd` was alive throughout. During the owner's own
  package task the update start was refused with `HOST_MUTATION_BUSY`, reason
  `package_manager_active`, and the installed version and service processes were
  unchanged; with the daemon idle the start was admitted and the update verified.
  In 176 readings of a running daemon it mapped only that backend file, had no
  child and held or awaited no lock; the Agent answered `ready` in all 41
  observations with only the idle daemon and busy in 16 of 16 during the real
  task. Stock Debian 13 has no PackageKit; its package ships the same backend
  file name (file listing only, not installed).
- **Not measured.** A PackageKit transaction in progress (the busy side of the
  rule for the daemon itself is covered by component tests only); Ubuntu 22.04;
  the `aptcc` name; the start-check kind and management off on Ubuntu; the signed
  alpha.80 archive; the owner's real server image, whose package services may
  differ; a browser. One run per cell.
- **Open.** A setup step refused for real package activity still fails instead
  of waiting and resuming; the failed step's error carries no reason field (the
  wizard picks its headline from the Panel's sentence); when the new Panel starts
  inside an update it logs a busy refusal for certificate startup reconcile and
  milter wiring, and whether those are retried later is not established.

### Startup mail work refused inside an update is retried once the host is idle (P0.1/P0.2, 2026-10-02)

D-025 invariants 2 (busy, idle and unknown stay distinct) and 3 (stop at the
actual boundary, the Agent's admission under the host lease); D-022 (native mail
configuration keeps running unchanged), D-024. From
[upd11](../deploy/e2e/release-recovery/evidence/upd11-20261002/README.md) F2:
every Panel start inside an update or rollback on Debian 13 and Ubuntu 24.04
with mail installed had two startup steps refused at admission ("another server
change or package-manager task is still running"), and they ran only at the next
Panel start.

- **What was skipped, from source.** Only the mail dependents step of the
  certificate startup reconcile (`cert_startup_reconcile.go`; lineage cleanup,
  pending activation and the vhost batch ran): the full mail SNI publication
  (`Agent.SyncMailTLSV2` under a lease), TLSA (a no-op in this release) and the
  clearing of the pending certificate outbox; and `Agent.WireMailFilters`
  (Postfix virtual/lookup-table repair, alias database, milter chain). A busy
  refusal happens before any durable job or host change.
- **What an owner lost.** In the steady state nothing: both re-assert native
  files the update does not touch. It matters when (a) a certificate outbox row
  (`activation_pending`/`dependents_pending`) exists at that start - an
  installation or renewal the update interrupted, or the renewal pass the Panel
  runs at start (its renew and vhost calls take no lease, its mail step does):
  nginx serves the new certificate while Postfix/Dovecot keep the previous
  snapshot for that domain, and nothing else consumes the outbox (the renewal
  pass only selects certificates within 30 days of expiry) until the domain's
  Retry or the next Panel start; (b) the host carries `hash:` maps on a Postfix
  without Berkeley DB, the case the wiring repairs "on upgrade" (every incoming
  message rejected with 451 until it runs); (c) a release changes what either
  writes. Source reading; not measured (the upd11 fixtures had no secure-mail
  certificate).
- **Changed.** Startup reports which of the two steps was refused for a busy
  host (`hostMutationBusyError`, not terminal-uncertain) and says in the same
  journal line that the Panel retries it. After HTTP admission one background
  loop then, every 30 s for at most 20 attempts (10 minutes), reads the existing
  advisory readiness (`Agent.ServiceMutationReadiness` plus the Panel's
  operation check, quiet) and only when it says idle repeats exactly the refused
  step: the full SNI publication once, then each startup outbox domain through
  the owner's Retry path under that domain's SSL lock, only if the row is still
  pending; and the wiring once, under `serviceMutationMu` like an HTTP
  operation. A completed step never runs again; a busy refusal is retried; a
  verified failure is logged once and left for the next start. At most one
  journal line per attempt: completion, failure, or at the cap the give-up line
  naming `sudo systemctl restart celikpanel-panel` (and "Retry activation" on
  the domain's SSL page). Nothing is retried when startup was not refused as
  busy.
- **Schema or version transition: none.** No persisted value, wire field,
  closed value or text key; Panel binary bytes change, the Agent is unchanged.
- **Recovery behaviour.** The Agent's admission under the host lease stays
  authoritative: while an updater, rollback or other change holds the host the
  step is refused again with no job, and the readiness read is advisory only. A
  Panel restart still runs the same steps at start. The outbox stays durable and
  owner-retryable throughout.
- **Evidence.** Component tests only, native run pending:
  `TestStartupDeferredRetryDecisionRetriesOnlyABusyRefusal`,
  `TestStartupDeferredRetryNoteMatchesThePolicy`,
  `TestStartupDeferredRetryStopsWithTheProcess`,
  `TestStartupDeferredRetryRunsOnceTheHostIsIdle`,
  `TestStartupDeferredRetryNothingDeferredStartsNothing`,
  `TestStartupDeferredRetryGivesUpWithTheOwnerLine`,
  `TestStartupDeferredRetryNeverRepeatsACompletedTask`,
  `TestStartupDeferredRetryDoesNotRepeatAVerifiedFailure`,
  `TestStartupCertificateDependentsRefusedAsBusyCompleteOnceIdle`,
  `TestStartupCertificateDependentsVerifiedFailureIsNotDeferred`,
  `TestStartupMilterWiringRefusedAsBusyIsWiredOnceIdle`.

Open: not run natively; how long an update or rollback keeps the host after its
Panel starts is not measured against the 10-minute window; an outbox row created
after startup (a renewal whose own mail step is refused as busy) is not retried
by this loop and keeps the domain's Retry; the renewal pass at Panel start is not
deferred while the host is busy; no screen shows the deferral (journal only); a
secure-mail certificate across an update is not measured.

### One candidate through the whole update matrix (P0.1/P0.2/P0.3/P0.5, 2026-10-02)

D-025 invariants 1 to 6; D-022, D-024. Evidence only; no product change in this
entry. P0.1, P0.2, P0.3 and P0.5 stay partial.

[upd11](../deploy/e2e/release-recovery/evidence/upd11-20261002/README.md) ran
every kind of the owner-started update matrix on one build, `48e54657` (the
alpha.80 cells use a second build of the same commit for their baseline),
instead of the successive builds the earlier runs used: 20 cells, 23 runs, three
cells run twice.

- **From the candidate's own baseline.** Debian 13: good, defective with a VM
  reset at `payload_restored`, start check, real start, owner continuation,
  management-off reboot. Arch: the same without real start, the second fault a
  SIGKILL at `runtime_verified`. Ubuntu 24.04: good, defective, owner
  continuation, start check, management-off reboot. Every good update was verified,
  every defective and start-check candidate returned automatically, every forward
  pause showed `pause_pending` and the restored renewal (Arch: "already in its
  state"), every owner retry ran once and ended `succeeded/update_verified`, and
  all three management-off reboots served. Setup on Ubuntu reached the `access_dns`
  wait in one attempt in all six runs.
- **From the alpha.80 source** (rebuilt with the license test seam): Debian 13
  good, defective and owner continuation; Ubuntu 24.04 good. All as in upd7 and
  upd8; the alpha.80 limits repeat (raw failure line, no recovery reader and the
  same version offered again after a return; no recovery CLI for about 10 s; seven
  setup attempts on Ubuntu).
- **Not clean.** Ubuntu start check: the rollback was recorded twice, but the
  first run failed on a harness race and the host slept during the second, so its
  workload checks are invalid; Arch management-off needed a second run after a
  harness cron-window defect; Debian real start needed a second run after one
  setup refusal (next bullet).
- **Recorded.** (1) The Panel starting inside an update or rollback on a server
  with mail skipped the mail SNI publication and milter wiring because the server
  was held, and they stayed undone until its next start; corrected in `6b6f8a0c`
  (next entry), not yet measured natively. (2) One Debian setup refusal
  (`HOST_MUTATION_BUSY`) about 10 s after the webmail package install, the busy
  process not named; the same step passed in nine other Debian runs. (3) The
  Agent never answered `package_manager_active` inside an update.
- **Changed paths exercised:** the initial status record for an alpha.80 worker
  (by inference, four cells), `pause_pending` (five runs), renewal restore at the
  forward pause, the curl preflight's passing branch only. **Not exercised:** the
  curl refusal, the no-marker end after a failed recovery child, host-name or
  leading-zero listen addresses, an update started while `packagekitd` idles, real
  start on Arch or Ubuntu.
- **Not proven.** Repeatability (one run per cell); the signed alpha.80 archive;
  production signing, the real origin, the license service, DNS, renewal itself,
  a browser, power loss; the effect of the skipped mail steps for an owner with
  mail certificates (the fixtures had none).

#### Measured on Debian 13 (same date)

[upd12](../deploy/e2e/release-recovery/evidence/upd12-20261002/README.md), built
from `6b6f8a0c`: good, defective with a reset at `payload_restored`, and owner
continuation, `web_mail` profile, one run each. In every cell the Panel that
started inside the operation wrote the two startup lines with the new sentence,
and 38 to 39 s after its start one attempt completed both steps with one journal
line; no second attempt and no further write 45 s later. Verification was not
disturbed: every operation had recorded its final state 17 to 25 s before the
first write, and timings match upd11. Recorded: every mail SNI publication,
before and after this change, logs `postfix/postmap … fatal: unsupported map
type: lmdb` although the step completes (an owner reading the journal sees a
"fatal" line that changes nothing; open); the retry rewrites the native mail
files with identical content (it re-asserts them, as the contract says). Not reached: a
second attempt, the give-up line, a verified failure, the pending-certificate
retry; the fixture has no secure-mail certificate; Ubuntu and Arch not run.

### The release candidate's exact code through the whole update matrix (P0.1/P0.2/P0.3/P0.5, 2026-10-03)

D-025 invariants 1 to 6; D-022, D-024. Evidence only; no product change. P0.1,
P0.2, P0.3 and P0.5 stay partial.

[upd13](../deploy/e2e/release-recovery/evidence/upd13-20261002/README.md) ran
the matrix of the entry above once more on `f6cdd5a0`, the commit that carries
every product change of this candidate (the alpha.80 cells start from the
`v0.1.0-alpha.80` source with the test-license seam and update to a second build
of the same commit): 20 cells, 21 runs, one Arch baseline install stopped by a
harness status-read race and re-run. Every complete run (20 of 21) ended as in
upd11:
every good update verified, every defective and start-check candidate returned
automatically after the platform's second fault, the real-start pause on Debian
with `pause_pending` and the restored renewal, every owner retry run once to
`succeeded/update_verified`, all three management-off reboots served, the
Ubuntu start check with its first clean run, setup on Ubuntu in one attempt in
all five runs. No candidate product defect.

- **The deferred mail steps (`6b6f8a0c`), now on three platforms' mail cells.**
  Thirteen Panels that started inside an operation logged both refusals with the
  retry sentence; the eleven that lived past 30 s completed both steps in one
  attempt between 38 and 40 s with one journal line each and no repeat; the two
  that the owner stopped before the first attempt (management off) did the work
  at their next start after the reboot. No operation's verification was
  disturbed. The retry beyond its first attempt (busy again, a verified failure,
  the give-up line, the pending-certificate retry) was not reached.
- **Changed paths not exercised** (same as upd11): the curl refusal, the
  no-marker end after a failed recovery child, host-name or leading-zero listen
  addresses, package activity reported inside an update. The initial status
  record for an alpha.80 worker is again inferred from the status sequence.
- **Repeat limits.** The alpha.80 Panel after a return shows the raw failure
  line, has no recovery reader, offers the same version again and skips its mail
  steps with no retry; about 17 s with no or unavailable status after the start;
  seven setup attempts on Ubuntu. The CLI read `failed/update_failed` once for
  18 s in one owner-continuation run before the recovery record appeared (seen
  once in upd11 too). The postmap lmdb "fatal" line is now seen on Ubuntu as
  well as Debian.
- **Not proven.** Repeatability (one run per cell); real start on Arch or
  Ubuntu; secure-mail certificates; the signed alpha.80 archive; production
  signing, the real origin, the license service, DNS, renewal itself, a
  browser, power loss; panel removal.

### A mounted page is replaced only by a known negative access result (P0.2, 2026-10-08)

D-025 invariants 2, 3 and 6; D-024. P0.2 stays partial and no acceptance item is
closed. Source state with component tests; inspected in one real browser against
a loopback mock on 2026-10-08 and corrected (see "Corrected after the browser
inspection" below); no native run, no installed panel touched, no license
service contacted.

**Reported.** An owner on an installed server running v0.1.0-alpha.81: after
leaving a page for a while the whole screen became "Lisans durumu kontrol
edilemedi" with the action "Panel erişimini kontrol et"; on return the panel came
back, but not where it had been left. An open dialogue, typed input and the
selected tab were gone. The same screen showed "Güncelleme ve kurtarma durumu:
Güncelleme doğrulandı" for an update that had finished days earlier. The license
was valid throughout.

**Mechanism (read from the source at `0f9e1067`, not observed on that server).**
The server's access decision is valid for at most 60 s
(`internal/licensing/license.go:28,197`, `cmd/panel/license.go:225`). In
`web/src/components/LicenseOnboarding.tsx` both the 60 s interval (`:55`) and the
refresh 15 s before the deadline (`:81-83`) skipped a hidden tab; the deadline
timer (`:84-88`) then set `allowed: null, failed: true`, and `:115` returned the
full recovery page in place of the application, which unmounted every page. Focus
(`:56,65`) read again and the application was built from nothing. A visible tab
whose reads were slow or failed past the deadline took the same path. Related
paths in `web/src/App.tsx`: any API reply with `PANEL_STARTING` or
`AUTH_STATUS_UNAVAILABLE` replaced a mounted application (`:492-501`,
`:509-513`); the readiness read after sign-in or on load was drawn as "Panel
readiness could not be checked" while it was only in flight (`:509-513` with
`RecoveryAccess.tsx:106-107`); a 401 showed the sign-in form with no reason
(`:486-491`, `:508`); `main.tsx:19-27` reloaded without a word when a part of the
interface failed to load. The recovery page drew the saved update record for
every administrator, whatever its result.

**Changed (browser only).** A gate replaces the screen only on a KNOWN negative:
a license reported missing, expired or invalid, a confirmed 401, or a sign-out.
An unknown or merely not yet refreshed state keeps the mounted pages.

- `AccessHold` (new) keeps the pages mounted and makes them unreachable while
  access is not confirmed: the subtree is `inert` and hidden from assistive
  technology, events aimed at it are stopped in the capture phase, and focus that
  lands in it is taken out. Nothing in it can be used until the server confirms
  access; the server refuses management requests without a current decision on
  its own, as before. On release, focus returns to the field it was in.
- A read that answers within 1.5 s draws nothing. A slower one draws a modal
  layer that says access is being checked. A read that answered without
  confirming access draws the reason, that nobody needs to act, and that the page
  continues where it was. The layer is the shared dialogue with no dismissal,
  outside the inert subtree and above the operation overlay.
- Hidden tab: at the deadline the decision stops being used; that is not recorded
  as a failed read and nothing is read. Returning (visibility or focus) applies
  the deadline by the clock, so a throttled timer cannot leave an expired decision
  in use, and starts one read.
- While unknown, the license answer is read every 5 s in a visible tab (before:
  every 60 s and on focus); session and readiness every 10 s, now also for an
  unreadable session (before: on focus only). After 30 s the layer adds "Reload
  CelikPanel" with its cost.
- `PANEL_STARTING` and `AUTH_STATUS_UNAVAILABLE` from a request of a mounted page
  use the same hold. Only the first report changes the session state and it
  starts one read; pages that keep being refused cannot restart it. The same
  applies to repeated license refusals.
- First load and sign-in: the session and readiness reads in flight are a
  "checking" state. "Could not be checked" needs a read that failed or timed out.
  The full recovery page remains for a load on which nothing is mounted yet.
- The saved update record is drawn in an access or readiness gate only for an
  operation that is still running, failed, rolled back after a failure, or whose
  result cannot be read. A verified update, or no saved operation, draws nothing
  and is not read. The page for an interface that failed to load keeps the full
  reader.
- A confirmed 401 under a mounted page shows the sign-in form with the reason.
  The address is kept, so signing in opens the same route.
- A part of the interface that fails to load after an update: the reason is
  shown, then the page reloads, once per 30 s as before (first one line for 4 s;
  since the browser inspection the shared dialogue for 7 s, see below). Before
  anything is drawn it reloads at once, as before.

**Not changed.** The server-side decision, its 60 s validity, what the server
refuses without it, and what a known negative does on screen (activation page,
redirect to `/activate`, tenant message). A decision is never used past its
deadline and never extended. The update tracker is paused while a hold is
explained exactly as it was while the recovery page was shown.

**Requests under the layer are not paused.** The server refuses each management
request while it has no current decision, so a poll under the layer cannot change
anything. Holding requests in the browser would need a list of exempt recovery
and update routes kept in step with the server, and a held request would run
later, at a time nobody chose. Read in the source for the six polling screens:
the dashboard figures and application status ignore a refused poll; the
host-change readiness on the dashboard and components page becomes "unverified"
until the next read; monitoring empties its chart until the next read; the
domain log viewer with automatic refresh raises its generic error toast on each
refused poll, behind the layer; setup shows its own reconnect guidance. None of
them discards typed input.

**Schema or version transition.** None. No server code, API field, stored record
or browser storage format changes. Eleven strings are added to the screen
catalogue (EN and TR); one sentence is removed from `recovery.startingHelp`. A
tab still running the previous interface keeps the previous behaviour until it
is reloaded.

**Recovery behaviour.** Access confirmed again: the hold ends and the same page
continues. Known negative while held: the pages are removed and the existing gate
is shown. Session ended: sign-in with the reason, same address, without what had
been typed. Still unknown: the layer stays, reads continue, and the owner can
check, or reload at the stated cost. If the wording part has not arrived, the
layer still blocks and says only that access is being checked.

**Evidence.** Component tests only: `web/tests/access-hold-runtime.test.mjs`
(new) and updated cases in `license-onboarding-runtime`, `recovery-access-runtime`
and `panel-handover`. They cover a decision that ran out in a hidden tab (no
unmount, no read while hidden, one read on return, nothing drawn), a throttled
timer, an explained hold over a page that keeps typed input, selected tab and an
open dialogue, stopped events and focus, each known negative from a hold, the
checking state on first load and after sign-in, refused background requests
through the real fetch interception, a finished update in a gate, the ended
session with an unchanged address, and the reload line. 576 web tests pass.
Production build: critical boot 302.29 KiB raw / 93.46 KiB gzip (limits 361 /
110), Settings route 272.99 / 79.77 KiB (limits 280 / 80); no limit raised. The
design detector reports no finding on the changed files.

**Corrected after the browser inspection (2026-10-08).** Invariants 2, 3 and 6;
D-024. P0.2 stays partial and no acceptance item is closed. A first pass in a
real browser against a loopback mock, on commit `8a65d4ca`, showed defects that
the component tests had not; they are corrected in the same change and the pass
was repeated. What it covered and did not is recorded in the
[operation guidance](OPERATION-GUIDANCE.md#access-and-readiness-checks-keep-the-page-explained-hold-checking-state-ended-session-2026-10-08).

- The layer is drawn above the component-operation overlay and the update lock
  (layer 120 over 100 and 110) and keeps keyboard focus against both. Before,
  the update lock (110) was above the layer (105). The update lock is still
  released when a hold begins, because the tracker pauses.
- While the layer or the reload dialogue is drawn it is the only scrim on the
  page: every scrim under it is cleared (`web/src/index.css`, `:has()`). Two
  stacked scrims had made the page that the layer says is kept unreadable. A
  browser without `:has()` draws both scrims, as before.
- The title of the layer takes programmatic focus without a focus ring; the
  "checking" layer has no empty body; the secure address wraps as an address.
- The activation page draws the saved update record only for an unfinished
  operation, like the other gates. It showed "No update operation ID is saved in
  this browser…" beside a license decision.
- The reload after a failed part is the shared dialogue above everything: the
  reason, that unsaved input is lost, and "Reload CelikPanel" to bring the reload
  forward. It reloads by itself after 7 s. The page under it is covered.
- A coded refusal answered by the access route itself no longer raises the
  refusal event, and while access stays unknown a refused request starts a read
  at most as often as the 5 s re-check. Not reachable with the current server,
  whose access route answers 200 with a typed body; found with a synthetic 503,
  which had produced 10,581 reads in 28.7 s.
- The hold text no longer states an interval ("every few seconds"); the first-load
  pages say that they read again by themselves.

Schema or version transition: none. No server code, API field, stored record or
browser storage format changes; one string is added to the screen catalogue and
five texts change (EN and TR). Recovery behaviour is as above, except that the
reload after a failed part can now be brought forward. Evidence: the updated
`access-hold-runtime`, `recovery-access-runtime`, `dialog-shape-contract` and
`boot-copy-contract` tests and the new `address-link` test; 616 web tests pass.
Production build: critical boot 303.73 KiB raw / 93.97 KiB gzip (limits 361 / 110),
Settings route 272.99 / 79.76 KiB (limits 280 / 80); no limit raised. The design
detector reports no finding on the changed files. The browser pass is one Chrome
against a mock: it is not a native run and closes nothing.

**Open.**

- Seen in one Chrome against a mock only (2026-10-08): timer throttling in a
  hidden tab, the order of visibility and focus events, `inert` on a
  `display: contents` element, the caret after focus returns, stacking against
  the operation overlay and the update lock, EN and TR at desktop and phone
  width. Not confirmed: the screen-reader announcement, any other browser, a
  real server.
- What each page does when its own request is refused under the layer is read
  for the six polling screens only, not audited page by page and not observed.
  The log viewer's repeated error toast and the emptied monitoring chart are
  left as they are. An operation whose reply is lost is reconciled by its own
  contract, as before.
- Unsent input is not kept across a real sign-in.
- Each tab holds on its own; nothing is shared between tabs.
- Cause not established on the owner's server; the mechanism is read from code.

### A failed read no longer lets three settings screens overwrite the owner's state (invariants 1-4 and 6, 2026-10-08)

D-025 invariants 1 (detect owner changes; never replace them with a preferred
configuration), 2 (unknown is not absent), 3 (stop the unsafe write at its own
boundary), 4 (a mutation reads its pre-image first) and 6 (the screen renders
the authoritative state, not a default); D-022, D-024. No P0 item is closed or
advanced. Found by a read-only source verification of `v0.1.0-alpha.81`; not
observed on an installed server.

- **Confirmed in alpha.81.** One failed read on an otherwise healthy server was
  enough in three places.
  - *Server mail policy.* A failed `GET /mail/policy` left the form on 25 MB,
    DNSBL off and no rate limit, with Save enabled. Save rewrote
    `smtpd_recipient_restrictions` to three fixed entries plus the zones, set
    `message_size_limit` and `smtpd_client_message_rate_limit` and reloaded
    Postfix. The Agent also answered a failed `postconf` read as zeros with
    success, and every save, even after a good read, dropped the restrictions
    the owner had added, because the read never returned them.
  - *Automatic backup schedule.* A failed schedule read left "off / daily /
    files / 7" editable. Turn on wrote it over the real schedule: a full backup
    became files-only and the next run pruned scheduled copies beyond 7.
  - *Scheduled tasks.* `crontab -l` failing for any reason was read as an empty
    crontab. The list said "No scheduled tasks" and one added task replaced the
    whole crontab.
- **Changed.**
  - *Unknown is an error.* The mail policy read, the crontab read and the cron
    list answer an error when the current state cannot be read; no zero, default
    or empty list stands in for it. `crontab -l` is accepted as "no crontab" only
    with exit status 1, no output and exactly `no crontab for <user>` on standard
    error (run with `LC_ALL=C`); every other failure is unknown. A `postconf`
    value is known only when the command succeeded and printed a value line.
  - *Versioned writes.* Each read returns a version of the native state: a hash
    of the four Postfix values (`message_size_limit`,
    `smtpd_recipient_restrictions`, `smtpd_client_message_rate_limit`,
    `anvil_rate_time_unit`), of the crontab bytes, or of the schedule's settings
    (run status excluded, so a background run does not make a save stale). Every
    write must carry it. No version: `409 SETTINGS_VERSION_REQUIRED`. A
    different current state: `409 SETTINGS_CHANGED`. An unreadable pre-image:
    `502 CURRENT_SETTINGS_UNREADABLE`. Nothing is written in any of the three.
    For mail and cron the Agent decides under one lock per resource (the Panel
    also refuses a missing version before calling it); the schedule write is a
    single statement conditional on the row still holding the settings read.
  - *Recipient restrictions are preserved.* The writer no longer rebuilds the
    list. Postfix splits it on commas and whitespace alike and reads an argument
    as the next element, so keeping every element in order keeps the owner's
    meaning. The Panel removes or adds only `reject_rbl_client <plain zone>`
    entries (next to the existing ones, else at the end), keeps the owner's
    separator style, and writes nothing when the wanted zones are already there.
    An empty value still receives the baseline `permit_mynetworks,
    permit_sasl_authenticated, reject_unauth_destination`. A DNSBL entry with a
    reply filter or behind `warn_if_reject` is the owner's and is left alone.
  - *Refused instead of guessed.* A DNSBL change is refused with
    `409 MAIL_POLICY_RESTRICTIONS_UNMANAGED` when the value refers to another
    setting (`variable`), has an unclosed brace or a `reject_rbl_client` without
    a zone (`malformed`), is a hand-written list without both permits
    (`no_baseline`) or ends with `permit`, `reject` or `defer` and has no DNSBL
    entry to place the new one beside (`terminal`). Message size and rate still
    save. The read reports the same reason, and the screen then offers no DNSBL
    control.
  - *Only what changed is written*, in one `postconf -e`, and Postfix is reloaded
    only then. A 10240000-byte limit shown as 9 MB is not rounded by a save that
    did not touch it. A value outside the Panel's range is refused with
    `400 MAIL_POLICY_INVALID` instead of replaced by 25 MB, and a zone that is
    not a plain host name is refused instead of silently dropped.
  - *Cron duplicates.* Adding the same schedule and command again is refused
    with `409 CRON_JOB_DUPLICATE` (a disabled copy counts; both lines would share
    one ID). A written crontab always ends with a newline.
  - *Screens.* The three screens hold `loading | known | unknown`. Loading shows
    a reading line; unknown shows "could not load" with Retry; neither shows a
    form, an "off" state or an empty list. A stale save keeps what was typed,
    disables Save and offers a reload.
- **Schema or version transition.** No persisted schema and no migration:
  `main.cf`, crontabs and `backup_schedules` keep their formats. Additive wire
  fields: `version` and `dnsbl_locked` on the mail policy, `version` on the cron
  list and on the three cron requests, `code` and `reason` on the Agent's mail
  policy answer, `version` on the schedule read and write answers. **Now
  required:** `version` in the body of `PUT /api/v1/mail/policy`,
  `PUT …/backups/schedule`, `POST` and `PUT …/cron`, and as a query value on
  `DELETE …/backups/schedule` and `DELETE …/cron`. New refusal codes:
  `CURRENT_SETTINGS_UNREADABLE`, `SETTINGS_VERSION_REQUIRED`,
  `SETTINGS_CHANGED`, `CRON_JOB_DUPLICATE`,
  `MAIL_POLICY_RESTRICTIONS_UNMANAGED`, `MAIL_POLICY_INVALID`. Panel and Agent
  of different releases cannot write these three: a new Panel refuses the
  version-less list of an older Agent, and a new Agent refuses an older Panel.
- **Recovery behaviour.** Every refusal comes before any write, so there is
  nothing to compensate. The owner reloads the page and decides again against
  the current state; no read is retried into a write. For a refused DNSBL change
  the owner edits `smtpd_recipient_restrictions` in `/etc/postfix/main.cf` and
  reloads Postfix. Postfix, cron and scheduled backups keep running without the
  Panel exactly as before.
- **Evidence.** Component tests only; no native run. Agent:
  `TestPlanRecipientRestrictionsPreservesWhatThePanelDoesNotManage`,
  `TestPlanRecipientRestrictionsRefusesWhatItCannotPlaceWithCertainty`,
  `TestGetMailPolicyReportsAFailedReadAsAnErrorNotAsZeros`,
  `TestSetMailPolicyRefusesWithoutACurrentVersion`,
  `TestSetMailPolicyWritesOnlyTheValuesThatChanged`,
  `TestSetMailPolicyKeepsOwnerAddedRestrictions`,
  `TestSetMailPolicyRefusesToRewriteRestrictionsItCannotPlace`,
  `TestSetMailPolicyRefusesInvalidValuesInsteadOfSubstitutingDefaults`,
  `TestReadCrontabTellsNoCrontabFromAFailedRead`,
  `TestListCronJobsReportsAFailedReadAsAnError`,
  `TestCronChangesNeverInstallACrontabBuiltFromAFailedRead`,
  `TestCronChangesRequireTheVersionOfTheCrontabTheyWereBuiltFrom`,
  `TestAddCronJobRefusesAnExactDuplicate`. Panel:
  `TestCronChangesWithoutAVersionAreRefusedBeforeTheAgent`,
  `TestCronHandlersAnswerCrontabProtectionRefusals`,
  `TestMailPolicyGetReportsAnUnreadablePolicyAndCarriesTheVersion`,
  `TestMailPolicyPutWithoutAVersionIsRefusedBeforeTheAgent`,
  `TestMailPolicyPutAnswersEachAgentRefusalWithItsTypedGuidance`,
  `TestBackupSchedulePutFromAFormThatNeverLoadedIsRefused`,
  `TestBackupScheduleWritesNeedTheVersionOfTheScheduleTheyReplace`. Web:
  `web/tests/current-settings-runtime.test.mjs`. Read-only observation on a
  Debian development guest (Postfix 3.10): `crontab -u <user> -l` without a
  crontab prints `no crontab for <user>` with exit status 1 and no output;
  `postconf -h` prints one line per value, an empty line for an empty value and
  a multi-line value folded to one line.

Open: nothing was run on a real server. cronie's "no crontab" answer on Arch is
taken from its source, not observed; another cron implementation that words it
differently now reads as unknown instead of empty. A failed Postfix reload
after a mail policy write is logged and not reported. An owner edit between the
Agent's version check and its write is not excluded (the Panel's own requests
are). A hand-written restriction list keeps DNSBL out of the Panel until the
owner changes it. The backup schedule read is the Panel's own database, so its
only unknown state is a failed query. Found beside this and not changed: a
disabled scheduled task cannot be enabled, edited or deleted (the writers skip
comment lines), deleting a task also removes a comment or disabled task on the
line above it, and the mail queue list shows "queue empty" after a failed read.
The app-wide `loading | known | unknown` layer is a later task; other screens
are not audited.

### Database and mail configuration: a file that could not be read is never an editor, and a save names what it replaces (invariants 1-4 and 6, 2026-10-09)

D-025 invariants 1 (the owner's native configuration is detected, not replaced),
2 (unknown is not absent or empty), 3 (the unsafe write is stopped at its own
boundary), 4 (a mutation reads its pre-image, validates, keeps what it replaces
and has a tested inverse) and 6 (the screen renders the authoritative state);
D-022, D-024. No P0 item is closed or advanced. Found by a read-only source
verification of `v0.1.0-alpha.81`; not observed on an installed server.

- **Confirmed in alpha.81.**
  - *The PostgreSQL and MariaDB configuration editors did not work, and were one
    repair from destroying the file.* `saveConfig` posted the file as
    `text/plain`; `POST /api/v1/config` reads JSON, so every Save answered 400.
    Behind that accident: after a failed read the three editors showed no
    settings (or "No access rules") with Save enabled, and the access-rule
    editor always wrote `pg_hba.conf` from scratch, without its comments. A
    repaired Save after one failed read would have written a comment-only
    `pg_hba.conf` (PostgreSQL then refuses every connection, the Panel's own
    included) or an empty `postgresql.conf` or option file. The Agent had no
    check for these paths: no validator, no refusal of empty content, no
    comparison with the file that was read, and it would have changed the file's
    owner and mode. The setting editors also rewrote every line they could
    parse, including all commented defaults, and gave two lines with the same
    name one value.
  - *"postgresql.conf not found" on an installed server* for as long as the
    component scan took, and for good when it failed.
  - *The catch-all field could be typed into before the address was read*, and
    what was typed replaced the unseen address by upsert; after a failed read
    "Disable" was hidden.
  - *A failed read was shown as a fact* in three more places: an empty mailbox
    list without a word, "Webmail is not available on this server", "The mail
    queue is empty". The Agent itself answered any failure of `postqueue -j` as
    "not installed, no items", and a queue action was announced as done whatever
    the server answered.
  - *Scheduled tasks.* A task the Panel had disabled could not be enabled,
    changed or deleted: the writers skipped every line that begins with `#`.
    Deleting a task also removed the line above it whenever that line began with
    `#` (the owner's header, or a disabled task) and every blank line of the
    crontab. The task ID was a 32-bit string sum that is the same for
    `…/Aa.sh` and `…/BB.sh`.
  - *A Postfix reload that failed after the policy was written* was logged and
    answered as a successful save.
- **Changed.**
  - *Unknown is an error.* `GET /api/v1/config` answers the file's text with the
    version of the exact bytes read, or `502 CURRENT_SETTINGS_UNREADABLE`; an
    empty file is a known answer with a version, a file that cannot be read is
    never one. The mail queue read answers `502 MAIL_QUEUE_UNREADABLE` when
    `postqueue -j` fails, prints a line that is not a queue entry or cannot be
    read to its end; "Postfix is not on this server" stays a known answer.
  - *Versioned writes.* `POST /api/v1/config` must carry `version`. Before
    anything is written the Agent reads the file with its owner and mode
    (unreadable: nothing is written), compares the version (`409
    SETTINGS_VERSION_REQUIRED`, `409 SETTINGS_CHANGED`), refuses empty content, a
    NUL byte and more than 1 MiB (`422 CONFIG_INVALID`, reasons `empty`,
    `shape`), and answers identical content without touching the file or the
    service. The replacement itself is conditional on the bytes that were read,
    so an owner edit made during the validation is not overwritten either. The
    catch-all `PUT` and `DELETE` carry `version` and are written by a statement
    conditional on the row that was read.
  - *Validated before it replaces the live file.*
    - `postgresql.conf`: the installed `postgres` reads a copy placed next to
      the file: `postgres -C config_file -D <dir> -c config_file=<copy> -c
      lc_messages=C`. It parses the file and every file it includes with the
      server's own parser and value checks, prints one setting and exits; it
      starts nothing and takes no lock. `-C` first is the form PostgreSQL allows
      root to run. On Debian and Ubuntu the cluster's own binary
      (`/usr/lib/postgresql/<major>/bin/postgres`) is used, else the one on
      `PATH`.
    - A MariaDB option file: the installed `mariadbd` reads the copy:
      `mariadbd --defaults-file=<copy> --datadir=<private empty directory>
      --help --verbose`. It refuses an unknown variable, an unusable value, a
      broken group header and an option before any group. The private data
      directory, given after the file, keeps it away from the real one.
    - `pg_hba.conf` cannot be shown to PostgreSQL before it is installed (the
      server reads only the file `hba_file` names). So the Agent itself refuses
      a changed or added line PostgreSQL's parser does not accept (the rules of
      `parse_hba_line`; lines carried over unchanged are the owner's and are not
      judged), and refuses a file that takes away the local administrator
      access: if the current file lets the operating system account `postgres`
      connect as `postgres` over the local socket by `peer` or `trust`, the new
      one must still do so, in a way that can be read from the file with
      certainty (`422 CONFIG_INVALID`, reason `lockout`). After the file is
      installed and before the reload, the running server is asked about it
      (`pg_hba_file_rules` parses the file on disk at the moment of the query);
      a file it refuses is put back before it was ever loaded.
    - Where the validating program cannot be run, nothing is installed (`422
      CONFIG_INVALID`, reason `no_validator`).
  - *Installed with its inverse.* The previous file is kept next to the file as
    `<name>.celikpanel-backup-<UTC time>` with its owner and mode (a name no
    `include_dir` or `!includedir` reads; the ten newest are kept). The new file
    replaces it atomically with the same owner and mode. PostgreSQL is reloaded,
    never restarted; if the unit is known to be stopped nothing is reloaded. If
    the reload fails, the previous file is put back (only while the file still
    is the one this write installed), reloaded, and the answer is `502
    CONFIG_RELOAD_FAILED` (`restored`, or `not_restored` with the name of the
    kept copy) carrying the first line of the unit's journal that names a
    failure, with password assignments blanked and 300 characters at most. After
    a reload the server is asked which settings it could not take and which wait
    for a restart. MariaDB re-reads its option files only when it starts and has
    no reload that does, so it is left alone and the answer says the change
    waits for the next restart, which is the owner's decision.
  - *Only the changed lines.* The three editors send the file that was read with
    only the changed lines replaced: a setting keeps its indent, spacing,
    quoting and trailing comment; a rule that was opened and left alone is not
    reformatted; rules with options, quoted names, a netmask, a continuation or
    a trailing comment, and include directives, are shown as written and are
    never rewritten or removed; new rules are added at the end.
  - *Scheduled tasks.* A task is the one line whose own text is its ID, enabled
    or disabled; a change replaces that line and a delete removes that line and
    nothing else. The ID is derived from the whole text. The same task on two
    lines is refused (`409 CRON_JOB_AMBIGUOUS`), and changing a task into a copy
    of another is refused (`409 CRON_JOB_DUPLICATE`).
  - *Mail policy.* A reload that fails after `postconf -e` succeeded is answered
    `502 MAIL_POLICY_NOT_RELOADED` with `mutation_applied: true`; nothing is
    rolled back, and the screen shows the saved values under the notice.
  - *Screens.* The PostgreSQL and MariaDB pages, the three editors, the raw file
    editor, a domain's mail tabs, the webmail card, the catch-all, the
    deliverability card and the mail queue hold `loading | known | unknown`
    through `lib/remote.ts`. Unknown shows "could not be read" with Retry and no
    editor; Save exists only for a known file; a stale save keeps what was
    typed, turns Save off and offers the reload; the service's own line is shown
    next to the field or rule it names.
- **Schema or version transition.** No persisted schema and no migration:
  `postgresql.conf`, `pg_hba.conf`, option files, crontabs and `mail_catch_all`
  keep their formats. A scheduled task's ID changes form (16 hexadecimal
  characters instead of 8); it was never stored and is read again with every
  list. **Now required:** `version` in the body of `POST /api/v1/config` and of
  `PUT …/mail/catch-all`, and as a query value on `DELETE …/mail/catch-all`.
  Additive wire fields: `Version` on the configuration read; `version`,
  `unchanged`, `backup`, `applied`, `daemon_check`, `restart_required` on the
  configuration write; `version` on the catch-all answers; `Version` and the
  result fields on the Agent's `UpdateConfig`, `Reason`, `Detail`, `Line`, `Name`
  on its typed error. New refusal codes: `CONFIG_RELOAD_FAILED`,
  `MAIL_QUEUE_UNREADABLE`, `MAIL_POLICY_NOT_RELOADED`, `CRON_JOB_AMBIGUOUS`;
  `CONFIG_INVALID` gains `reason` and `vars` (`detail`, `line`, `name`). A Panel
  and an Agent of different releases cannot write a configuration file: a new
  Panel refuses the version-less read of an older Agent, and a new Agent refuses
  the version-less write of an older Panel.
- **Recovery behaviour.** Every refusal except two comes before the live file is
  touched, so there is nothing to compensate. The two that follow a change: a
  `pg_hba.conf` the running server refuses and a failed reload both put the
  previous file back, conditional on the file still being the one this write
  installed; when that cannot be done the previous version stays in the named
  backup and the owner checks the file and reloads the service on the server.
  A mail policy that was written and not reloaded is not rolled back; the owner
  runs `sudo postfix check`, corrects what it names and runs `sudo systemctl
  reload postfix`. PostgreSQL, MariaDB, Postfix and cron keep running without
  the Panel exactly as before; the backups are ordinary files the owner can copy
  back.
- **Evidence.** Component tests, plus the validating programs run for real.
  - Agent: `TestGetConfigAnswersAnUnreadableFileAsAnErrorNotAsEmpty`,
    `TestUpdateConfigRequiresTheVersionOfTheFileItReplaces`,
    `TestUpdateConfigRefusesEmptyAndMalformedContent`,
    `TestUpdateConfigWithTheSameContentWritesNothing`,
    `TestDatabaseConfigTargets`,
    `TestPostgreSQLConfIsValidatedInstalledBackedUpAndReloaded`,
    `TestPostgreSQLConfRefusedByPostgresChangesNothing`,
    `TestDatabaseConfigIsNotInstalledWithoutItsValidator`,
    `TestFailedReloadPutsThePreviousFileBack`,
    `TestFailedReloadThatCannotBeUndoneSaysSoAndKeepsTheOtherVersion`,
    `TestStoppedServiceIsNotReloaded`,
    `TestPostgresReportingAnErrorAfterTheReloadPutsTheFileBack`,
    `TestDatabaseConfigIsNotInstalledOverAFileThatChangedMeanwhile`,
    `TestMariaDBOptionFileIsValidatedInstalledAndNotReloaded`,
    `TestMariaDBOptionFileRefusedByMariaDBChangesNothing`,
    `TestHBAIsRefusedBeforeAnythingIsWritten`,
    `TestHBAIsShownToTheRunningServerBeforeItIsLoaded`,
    `TestOnlyTheNewestBackupsOfAFileAreKept`,
    `TestHBALineVerdictsAgreeWithPostgreSQL`,
    `TestValidateHBAJudgesOnlyTheLinesTheWriteChanges`,
    `TestHBALocalAdminAccess`, `TestHBALockoutRefusal`,
    `TestADisabledCronJobCanBeEnabledChangedAndDeleted`,
    `TestDeletingACronJobRemovesOnlyItsOwnLine`,
    `TestUpdatingACronJobRewritesOnlyItsOwnLine`,
    `TestCronJobsAreIdentifiedByTheirWholeText`,
    `TestACronJobThatStandsTwiceIsNotChanged`,
    `TestChangingACronJobIntoACopyOfAnotherIsRefused`,
    `TestPostfixQueueTellsAFailedReadFromAnEmptyQueue`,
    `TestSetMailPolicyReportsAFailedReloadAsWrittenNotReloaded`.
  - Panel: `TestConfigReadCarriesTheVersionAndAnUnreadableFileIsAnError`,
    `TestConfigWriteWithoutAVersionIsRefusedBeforeTheAgent`,
    `TestConfigWriteAnswersEachAgentRefusalWithItsTypedGuidance`,
    `TestConfigWriteAnswersWhatHappenedToTheService`,
    `TestCatchAllWritesNeedTheVersionOfTheCatchAllTheyReplace`,
    `TestMailQueueThatCouldNotBeReadIsNotAnEmptyQueue`,
    `TestMailPolicyWrittenButNotReloadedIsAVerifiedFailureAfterAChange`,
    `TestCronJobThatStandsTwiceIsATypedRefusal`.
  - Web: `web/tests/remote-state-mounted-batch2b.test.mjs`,
    `web/tests/db-config-text.test.mjs`, the rewritten
    `web/tests/webmail-cta-ui-contract.test.mjs`.
  - With the real programs, on a Debian 13 development guest, from the
    distribution's packages unpacked into a private directory (not installed):
    `TestRealPostgresValidatesACandidateFile` against PostgreSQL 17.11 and
    `TestRealMariaDBValidatesACandidateFile` against MariaDB 11.8.6 (both skip
    where the program is absent). Observed by hand on the same guest, with a
    private, temporary PostgreSQL cluster and MariaDB data directory under
    `/tmp`, without a TCP listener, removed afterwards: `postgres -C` runs as
    root only with `-C` first, beside a running server and without changing it,
    and leaves no file behind; an empty `postgresql.conf` passes it (so the
    Agent's own refusal of empty content is what stops that); a `pg_hba.conf`
    the server refuses leaves `pg_ctl reload` at exit status 0 and the old rules
    in force, and `pg_hba_file_rules` names the line; the 44 lines of
    `TestHBALineVerdictsAgreeWithPostgreSQL` are what that server answered;
    after a reload `pg_file_settings` and `pending_restart` name
    `shared_buffers`; `mariadbd --help --verbose` with a private data directory
    touches no file of the real one, with the server stopped or running, writes
    nothing to the configured `log_error`, exits 7 for an unknown variable, 9
    for an unusable value and 1 for a broken group, and exits 0 for a missing
    `!include`, an out-of-range value it adjusts, and anything in a group the
    server does not read; a SIGHUP does not make MariaDB re-read the file.

Open: nothing was run on a real server, and no real `systemctl reload` of a
packaged PostgreSQL unit was exercised: the unit names (`postgresql@<major>-
<cluster>` on Debian and Ubuntu, `postgresql` elsewhere), the journal line picked
after a failed reload and `sudo -u postgres psql` reaching the edited cluster are
from source and component tests. PostgreSQL 15 and 16 and MariaDB 10.11 were not
run; `postgres -C` and `mariadbd --help --verbose` are long-standing, but a
different message wording would only cost the line and setting shown next to the
field, not the refusal. Oracle MySQL (`mysqld --validate-config`) is not handled:
`mysqld` is run the MariaDB way. Not validated by any program: what
`postgres -C` does not check (a `shared_buffers` the host cannot provide, a
certificate file that cannot be loaded) is caught only after the reload or at
the next start; option-file groups the server does not read; a missing
`!include`. The `pg_hba.conf` check without a reachable server (stopped, or
another cluster than the one `psql` reaches) is the Agent's line rules alone,
which accept every method and option name any PostgreSQL release has had. An
owner edit between the Agent's read and its conditional replacement is refused;
one made in the instant between the replacement and a restore is not overwritten
and leaves the restore undone. Deleting a scheduled task no longer removes the
comment above it, so a description the Panel wrote for a task stays in the
crontab after the task is deleted and is then listed with the task below it; the
Panel cannot tell its own descriptions from the owner's comments. A webmail
probe that times out is still answered as "not available" by the server. Not
migrated: the other 47 files of the remote-state allow-list (56 before the
first part of this batch stood in the same tree).

### Corrections from the first native measurement of settings writes (invariants 1-4 and 6, 2026-10-10)

D-025 invariants 1 (the owner's native configuration is detected, not replaced),
2 (unknown is not absent, empty or success), 3 (the unsafe write is stopped at
its own boundary), 4 (a mutation reads its pre-image, validates, keeps what it
replaces and has a tested inverse) and 6 (the screen renders the authoritative
state); D-022, D-024. No P0 item is closed or advanced. Source: the `set1` run of
2026-10-08, the first time the settings writes of the two entries above met the
packaged services (disposable QEMU/KVM guests: Debian 13, Ubuntu 24.04, Arch;
evidence `deploy/e2e/release-recovery/evidence/set1-20261010/`). Nothing here
was observed on an installed server. That run measured two candidate defects
(P1, P2) and five observations (O1-O5); this entry corrects them in source.

- **Measured.**
  - *P1, Ubuntu 24.04: a mail policy save answered `200 success` although
    Postfix did not reload.* The owner had left `default_process_limit = 200 #
    raised for the campaign` in `main.cf` without reloading. The Agent ran
    `systemctl reload-or-restart postfix`. On Ubuntu `postfix.service` is a
    oneshot wrapper (`ExecStart=/bin/true`, `ExecReload=/bin/true`) and the
    daemon belongs to `postfix@-.service`; the journal said `Reload failed for
    postfix@-.service`, `systemctl` exited 0, `main.cf` held rate 46 and Postfix
    ran with 45. Debian 13, whose `postfix.service` is the real unit, answered
    `502 MAIL_POLICY_NOT_RELOADED` for the same sequence.
  - *P2, all three platforms: after a reload that failed twice the answer said
    the previous `postgresql.conf` could not be put back, although it was.* The
    cause used was an owner's unit drop-in whose `ExecReload` signals PostgreSQL
    and then fails. The first reload failed, the previous file was put back
    (byte-identical, same owner, group and mode), the reload with it failed too,
    and the answer was `CONFIG_RELOAD_FAILED` / `not_restored`, naming a copy of
    that same file as "the other version". The same run showed why "the server
    still runs the old settings" may not be assumed either: the first, "failed"
    reload had already made the server read the NEW file.
  - *O1.* `max_connections = plenty` was saved with "MariaDB checked the file
    and accepts it": `mariadbd --help --verbose` exits 0 and prints `[Warning]
    ... option 'max_connections': unsigned value 0 adjusted to 10`.
  - *O2.* A `smtpd_recipient_restrictions` written one restriction per line came
    back as one line after a DNSBL save (elements, order and separators kept).
  - *O3.* Guidance named a cause that was not the cause. Unreadable scheduled
    tasks: "checks that the service behind this page is running" (the causes
    were `/etc/cron.allow` without the site user, and a relocated spool).
    Unreadable mail queue: "checks that Postfix is running" (the cause was the
    `main.cf` line above; with Postfix stopped `postqueue -j` reads the queue
    directly and succeeds).
  - *O4.* The `502 MAIL_POLICY_NOT_RELOADED` body carried no policy, although
    its sentence says "the saved values are the ones shown".
  - *O5.* The component scan listed `postgresql.conf` and `pg_hba.conf` twice on
    Debian and Ubuntu.
- **Changed.**
  - *P1: a Postfix or Dovecot reload, start or restart is verified, never
    inferred from `systemctl`'s exit status* (`cmd/agent/mail_service_verify.go`).
    Postfix: `postfix check` first (its refusal is a verified failure with its
    own line, and nothing is reloaded); a running master is reloaded with
    `postfix reload`, the command every packaging runs as the unit's reload
    (Debian 13 and Arch `ExecReload=postfix reload`; Ubuntu's instance
    `postmulti -i - -p reload`, which is `postfix reload` for the default
    instance), so the exit status is the instance's; afterwards `postfix status`
    must still report a running master. A start or restart still goes through
    systemd, so the unit keeps owning the daemon, and is judged by `postfix
    status` and the master's process ID (`<queue_directory>/pid/master.pid`), up
    to 15 seconds. Dovecot: `doveconf -n` first; after `systemctl restart`
    `dovecot.service` must be active and running with one main process across
    two readings, and after a restart that process must be a new one. Three
    outcomes exist: verified, verified failure (stage `check`, `reload`, `start`
    or `verify`), and unknown (a command could not be run or did not answer).
    Used by: the mail policy save; mail stack setup; mail filter wiring and DKIM
    (the reload after the milter chain was `reload-or-restart` with its result
    thrown away); mail submission setup and its Dovecot recovery; the mail TLS
    reconcile and its rollback; the raw-file editor's reload after a
    `/etc/postfix` file.
  - *P1, behaviour that changes with it.* A stopped Postfix is no longer started
    by a mail policy save, by filter wiring or by a raw-file save: it is left
    stopped and the answer says so (`applied: not_running`). Only setup (mail
    stack, TLS) starts it. Filter wiring and DKIM now fail when Postfix
    verifiably did not take the chain, instead of reporting success.
  - *P1, the answers.* `502 MAIL_POLICY_NOT_RELOADED` carries `reason` `check`,
    `reload` or `verify` with a sentence for each, and Postfix's own line in
    `vars.detail`. An outcome that could not be established is the new `502
    MAIL_POLICY_RELOAD_UNKNOWN`. The recovery command in every sentence is `sudo
    postfix reload`, which prints what Postfix objects to and is the same on
    every platform; `sudo systemctl reload postfix` on Ubuntu is the wrapper.
    `200` carries `applied`: `reloaded`, `not_running`, `unchanged` or, since
    the follow-up below, `unchanged_reloaded`.
  - *P2: the answer is classified by what is verified*
    (`cmd/agent/db_config.go`). (a) previous file back and the unit reloaded it:
    `restored`, as before. (b) previous file back and the unit's reload failed
    again: the server is told directly (`SELECT pg_reload_conf()` over the local
    socket as the `postgres` account) and asked what it did. It runs with the
    settings it had before the change only when all three hold: the answer is
    about this file (`config_file` / `hba_file`); `pg_conf_load_time()` is later
    than the moment the signal was sent (PostgreSQL sets it only when a re-read
    reached the end without a syntax or value error, and with such an error
    applies nothing); and `pg_file_settings` (or `pg_hba_file_rules`) reports no
    error in the files on disk. Then `restored_unit_reload_failed`. Anything
    less is `restored_running_unknown`. (c) the previous file could not be put
    back (the file on disk is no longer the one this write installed, or the
    write failed): `not_restored`, which alone names a copy, and that copy is
    the previous file. In (a) and (b) the copy is removed, because the file on
    disk is that file; no "other version" is named that does not exist. The
    same classification applies when PostgreSQL reports an error after a
    successful reload and the reload of the restored file then fails. The
    answer carries the unit whose reload fails (`vars.unit`).
  - *O1.* A `[Warning]` of `mariadbd --help --verbose` that says a value will
    not be used as written (`option '<name>': ... adjusted to ...`, `option
    '<name>': boolean value ... wasn't recognized`) refuses the save as
    `CONFIG_INVALID` / `daemon` with MariaDB's line and the option's name. Other
    warnings do not (the empty private data directory has no `mysql.plugin`
    table; a stock Debian or Ubuntu file sets `expire_logs_days` without a
    binary log; a removed option). A warning the file on the server already
    produces is not this change's: only then the current file is read the same
    way and what it already says is left out.
  - *O3.* No sentence names a cause that was not verified. The Agent's "could
    not be read" answers keep their fixed first line and may carry two more: a
    cause the Agent verified itself, and the first line the server's own program
    printed (bounded to 300 characters, password assignments blanked).
    `cron_allow`: `crontab` said it refuses the user AND `/etc/cron.allow`
    exists without that user. `cron_deny`: the same line, no `cron.allow`, and
    `/etc/cron.deny` lists the user. `postfix_config`: `postqueue` printed
    `fatal: bad ... configuration: ...` or a fatal naming `main.cf` or
    `master.cf` and a line. Everything else is "could not be read", with the
    line. The shared sentence of the settings screens no longer says to check
    that the service is running.
  - *O4.* The `502` body after a written policy carries `policy` with its new
    version; the screen shows it without a second read.
  - *O5.* The cause: the component has two units on Debian and Ubuntu
    (`postgresql.service`, a wrapper, and `postgresql@<version>-<cluster>`), and
    each unit's scan returns the component's files. The Panel's fold lists each
    file once, compared by the path it resolves to; of two names for one file
    the one that is the file itself is kept, because the Agent refuses to write
    through a symbolic link (`/etc/mysql/my.cnf` on Debian resolves to
    `mariadb.cnf`).
- **Not changed, and why.**
  - *O2.* Measured on the development guest with a private temporary
    configuration directory (Postfix 3.10): `postconf -e` with a value that
    spans lines exits 1, `postconf: fatal: -e, -X, or -# accepts no multi-line
    input`. Keeping the owner's layout therefore needs a second writer that
    edits `main.cf` in place (the last logical assignment, its continuation
    lines, comments between them, an atomic replacement that keeps owner and
    mode, and a check that Postfix reads the same value). That is a new write
    path into the owner's file with no native measurement behind it, for a
    difference that does not change what Postfix does. Left as it is.
- **Platform limitation, stated in the guidance.** On a server whose
  `/etc/cron.allow` does not list a site user, the Panel can neither read nor
  write that user's crontab: Debian's and Ubuntu's `crontab -u <user>` refuses
  the user even for root. The Panel says so and changes nothing; it does not
  edit `cron.allow`.
- **Schema or version transition.** No database schema and no persisted state.
  Agent RPC, additive: `MailPolicyResponse.Stage` and `.Applied`, code
  `mail_policy_reload_unknown`; `ConfigRPCError.Unit`, reasons
  `restored_unit_reload_failed` and `restored_running_unknown`; the two "could
  not be read" answers may be followed by `cause=` and `detail=` lines. HTTP,
  additive: `MAIL_POLICY_RELOAD_UNKNOWN`; `reason` and `policy` on
  `MAIL_POLICY_NOT_RELOADED`; `applied` on a successful policy save; the two
  `CONFIG_RELOAD_FAILED` reasons and `vars.unit`; `detail` (the cause token) and
  `vars.detail` on `CURRENT_SETTINGS_UNREADABLE` / `scheduled_tasks`; `reason`
  and `vars.detail` on `MAIL_QUEUE_UNREADABLE`. Panel and Agent are installed
  together by one release. A Panel older than this entry that meets a newer
  Agent's "could not be read" answer with evidence lines does not recognise it
  and answers its generic internal error, never an empty list; an older Panel
  shows `not_restored` for the two new reasons, as it did before.
- **Recovery behaviour.** Nothing retries by itself and nothing is rolled back
  that was not before. A written policy that Postfix did not take stays
  written; the owner corrects the line Postfix names and runs `sudo postfix
  reload`. A configuration change that was not kept leaves the previous file in
  place; when the unit's reload fails with it too, the owner is told which unit
  and that the cause is not only the change.
- **Evidence.** Component tests only; the native re-run is pending. Agent:
  `mail_service_verify_test.go` (the wrapper that exits 0 while the daemon did
  not reload, a reload refused by `postfix check`, success, a master that
  stops, an unknown outcome, a stopped Postfix, Dovecot exiting after a
  restart); `mail_policy_reload_test.go`; `db_config_set1_linux_test.go` (each
  P2 branch, every reading that falls short, `pg_hba.conf`, and the MariaDB
  lines recorded in the evidence folder as fixtures);
  `unreadable_evidence_test.go`. Panel: `set1_corrections_test.go`. Screens:
  four mounted cases in `web/tests/remote-state-mounted-batch2b.test.mjs`. By
  hand on the development guest, read-only or in a private temporary directory:
  the `postconf -e` refusal above, and `postfix` writing its `fatal:` line to a
  standard error that is not a terminal.
- **Open.**
  - No fix here has been measured on real services. The native re-run must
    show, per platform: P1 the Ubuntu sequence answering `502
    MAIL_POLICY_NOT_RELOADED` / `check` with `policy` in the body, and a healthy
    save answering `200` / `reloaded` with a `postfix/master ... reload` journal
    line and the same master process; P2 the drop-in sequence answering
    `restored_unit_reload_failed` with no copy named or left, `SHOW work_mem`
    at the previous value; O1 `plenty` refused with MariaDB's line and the stock
    files still accepted; O3 the three measured causes; O5 one entry per file.
  - Not changed: the certificate renewal path
    (`mail_host_certificate_reload.go`) still runs `systemctl reload
    postfix.service` and judges by `systemctl is-active`, which on Ubuntu are
    the wrapper's; its command scope is closed and it must not run `postfix
    check`. The generic service actions of the Services page still report
    `systemctl`'s exit status, also for the wrapper units `postfix` (Ubuntu) and
    `postgresql` (Debian, Ubuntu).
  - Follow-up, same date, component tests only
    (`cmd/agent/mail_policy_rpc.go`, `mail_policy_reload_test.go`,
    `cmd/panel/set1_corrections_test.go`): a save that changes nothing used to
    reload nothing, so after a "not reloaded" answer a second, unchanged save
    answered `200` / `unchanged` while Postfix still ran the earlier values.
    Every accepted save now ends with the same verified reload. Nothing is
    written, no state is recorded for it (Postfix cannot report what a running
    master holds, and the Agent does not remember the earlier outcome), a
    stopped Postfix is left stopped, and no schema, stored record or version
    changes. `200` then carries `unchanged_reloaded` (running, reloaded) or
    `unchanged` (stopped); a reload that still fails is the same `502` without
    `mutation_applied`, because that request changed nothing. Still open: this
    is not measured on a real service, and the reload is asked for on every
    such save, also when Postfix already runs the file.
  - `postfix check` also creates missing queue directories; it is what Debian
    13's unit runs before every start. Postfix has no interface that reports
    the values a running master holds, so "took the settings" is its own check,
    its own reload command and a master that is still running.
  - The PostgreSQL reading reaches the cluster `psql` reaches by default; for
    another cluster the answer is `restored_running_unknown`. That
    `pg_conf_load_time()` moves only on an error-free re-read is from
    PostgreSQL's source behaviour, not measured here. The reading waits one
    second.
  - MariaDB: only the two warning forms above are refusals; Oracle MySQL is not
    handled.
  - Follow-up, same date: the scheduled tasks screen shows the verified cause
    (`cron.unknown.cron_allow`, `cron.unknown.cron_deny`) as the server owner's
    rule, and crontab's line (`cron.unknown.said`) under the neutral sentence
    for every other answer; mounted cases in
    `web/tests/remote-state-mounted-batch4.test.mjs`. Still open: a refused
    write to the scheduled tasks shows the general sentence. The catalogue
    entries `postfix.queue.unknown` and `mailpolicy.unknown` are no longer
    used.

  - The scheduled tasks screen does not show the cause or the line yet: the
    answer and the catalogue entries (`cron.unknown.cron_allow`,
    `cron.unknown.cron_deny`, `cron.unknown.said`) exist, the screen still
    shows its one neutral sentence. The catalogue entries `postfix.queue.unknown`
    and `mailpolicy.unknown` are no longer used.

### Mail certificate renewal and the Services page: the outcome is what the daemon shows, not a wrapper unit's exit status (invariants 1, 2 and 4; P0.4/P0.5; 2026-10-10)

D-025 invariants 1 (the owner keeps authority and working services), 2 (unknown
is not success; evidence has a meaning) and 4 (a mutation carries its recovery
contract); D-022 (renewal works without the Panel and the Agent), D-024. P0.4
(the activation step of the mail TLS contract) and P0.5 (independent renewal).
No P0 item is closed or advanced. Source: the two items the entry above left
open after the `set1` measurement (P1). Nothing here was observed on an
installed server, and nothing here has been measured on real services.

- **Established: every reload or restart of Postfix or Dovecot after a
  certificate changes.**
  1. *First issue of the host certificate* (Agent RPC
     `IssueMailHostCertificateV1`): `applyMailHostCertificateSelection`
     (`cmd/agent/mail_host_certificate_rpc.go`) runs the mail TLS reconcile,
     whose `reloadMailTLSService` (`cmd/agent/mail_tls_rpc.go`) has used the
     verified helpers since the entry above (`postfix check`, `postfix status`,
     `postfix reload`, `postfix status`; Dovecot `doveconf -n` and a main
     process that stays). Correct on the wrapper unit. Customer certificates
     for mail names (SNI) take the same path, and only when the Panel asks for
     it (`resyncMailTLS`); there is no native hook for them.
  2. *Renewal of the host certificate.* Certbot's deploy hook only queues: the
     kit hook runs `<generation>/renew --queue`, the earlier Agent hook
     `agent --deploy-mail-host-certificate`
     (`internal/mailrenewalkit/deploy-hook`, `legacy-deploy-hook`). The queue is
     consumed by the Agent's worker every minute
     (`runMailHostCertificateRenewalWorker`) and, on an enrolled server, by
     `celikpanel-mail-renewal.service` (`renew --process-pending`, timer every
     five minutes), whichever takes the shared locks first. Both run the same
     code: `publishMailHostCertificateSource` ->
     `reloadMailHostCertificateSelection` -> `observeOrReloadMailHostTLS`
     (`cmd/agent/mail_host_certificate_reload.go`): `systemctl reload
     postfix.service`, then `systemctl reload dovecot.service`, each judged by
     that command's exit status and by `systemctl is-active --quiet` of the same
     two units, with the accepted configuration re-read before, between and
     after.
  3. *Recovery of an already selected certificate* (Agent start, the helper's
     automatic retry, `--retry-selected`):
     `reconcilePersistedMailHostCertificateHostAt`
     (`cmd/agent/mail_host_certificate_files_linux.go`) calls the same
     `reloadMailHostCertificateSelection`.
  4. *Rollback.* The mail TLS reconcile's rollback uses the verified helpers
     (path 1). A renewal has no service rollback: a certificate that was
     selected stays selected and the operation stays open.
  5. *The Panel certificate* reloads nginx only.
- **On Ubuntu 24.04, paths 2 and 3.** `postfix.service` is `Type=oneshot`,
  `RemainAfterExit=yes`, `ExecStart=/bin/true`, `ExecReload=/bin/true`; the
  daemon is `postfix@-.service` with `ReloadPropagatedFrom=postfix.service`
  (unit texts in `evidence/set1-20261010/set1-ubuntu/run-a/steps/09-s2-mail-policy/native-text/postfix-unit.txt`).
  `systemctl reload postfix.service` exits 0 whatever the instance's reload did
  (measured, P1), and `systemctl is-active postfix.service` answers "active"
  for the wrapper while the master is stopped (from the unit text; not
  measured). So a renewal was recorded as activated without evidence that
  Postfix had reloaded or was running. Debian 13's `postfix.service` is the
  real unit (`ExecReload=postfix reload`; same evidence folder), and so is
  Arch's; there the two commands are the daemon's. Dovecot: no unit text is in
  the evidence. From its upstream packaging `dovecot.service` is one real unit
  on the three platforms, with `ExecReload=doveadm reload`; that command only
  delivers a signal, so exit 0 does not say Dovecot read its configuration
  again. Mail setup is refused on Arch, so the renewal path runs on Debian and
  Ubuntu.
- **Why the renewal path's command scope is "closed".** It is not a field of
  the kit manifest and not a protocol version. It is a compiled list in the
  helper, `validateIndependentMailCommand` (`cmd/agent/mail_renewal_entry.go`):
  `dovecot --version`, `doveconf -n`, `postconf -h` for nine fixed settings,
  `systemctl reload` and `systemctl is-active --quiet` for the two fixed units,
  each as a canonical path in four system directories. The helper checks it
  before every launch (`runMailHostCertificateCommand`) and again in the
  re-executed supervisor (`service_mutation_supervisor_linux.go`). The guard
  test `TestIndependentMailSupervisorCommandScope` pins it and names
  `/usr/sbin/postfix check` among the refused. `MAIL-RENEWAL-EXECUTOR.md` and
  `MAIL-RENEWAL-OBSERVATION.md` state it as the helper's contract, and the
  native acceptance of the helper and its sandbox (BB, BD, BE) measured exactly
  this list. `postfix check` is excluded for a reason of its own: it creates
  missing queue directories, and a renewal may observe and reload but not
  change what the owner has. The kit manifest
  (`celikpanel-mail-renewal-runtime/v1`) binds the helper's bytes; a different
  list is a different helper and therefore a different generation, which is
  what every release build is. What would be a format change is the unit
  template (`ProtectSystem=full`, `NoNewPrivileges`, `PrivateTmp`): the reader
  recognises only the exact v1 template.
- **Changed, renewal (paths 2 and 3).** `systemctl reload` stays the only thing
  sent to a service: it is the unit's own reload with whatever its owner added
  to it, and the command list is unchanged. Its exit status and `is-active` are
  no longer the outcome. After each service's reload and re-observation, the
  helper performs a TLS handshake with that service's own listeners on this
  host and compares the certificate presented with the selected one
  (`cmd/agent/mail_served_certificate.go`): Postfix 465, then 587 and 25 with
  STARTTLS; Dovecot 993, 995, then 143 and 110 with STARTTLS/STLS; on
  `127.0.0.1` and `::1`, and on this host's other addresses only when nothing
  on loopback answers. No server name is sent, so the daemon presents its
  default certificate, which is the host certificate. No name is resolved and
  no other host is contacted. Bounded: one second to connect, three for an
  exchange, nine per service. Three answers: a listener presents the selected
  certificate (verified); listeners answer and none presents it within the
  wait (verified: not activated); no listener answers with TLS (unknown).
  Unknown and "not activated" both leave the operation open exactly as a
  failed reload did before; neither is recorded as activated. This needs no
  command, so it works in the helper's existing sandbox (the v1 unit does not
  restrict the network).
- **Changed, the Services page (`POST /api/v1/service/action`).** The Agent no
  longer answers with `systemctl`'s exit status alone
  (`cmd/agent/service_action_verify.go`).
  - *Postfix and Dovecot* use the verified helpers. Postfix: `postfix check`
    first for start, restart and reload, and a refusal is the answer; reload is
    `postfix reload` and a running master afterwards; start and restart go
    through systemd and are judged by `postfix status` and the master's process
    ID; stop is judged by `postfix status` saying the master is not running.
    Dovecot: `doveconf -n` first; `systemctl reload` (no longer able to start a
    stopped one), start, restart, each followed by a main process that stays
    across two readings; stop by the unit having no main process. "Start" on a
    running service changes nothing and says so; "Reload" on a stopped one
    fails and does not start it.
  - *Any other unit* is first asked what it is (`systemctl show`: `Type`,
    `ExecStart`, `Wants`, `ConsistsOf`, `PropagatesReloadTo`). A oneshot whose
    one start command is `/bin/true` is a wrapper: Debian's and Ubuntu's
    `postgresql.service`, with `postgresql@<version>-<cluster>.service` behind
    it. Its action is judged by the instance units systemd names: start, the
    units it wants are active; restart, those and the ones that were running
    are active with a new main process; stop, none is active; reload, each
    running one shows a reload command that ran since (`ExecReload`) with
    `ReloadResult=success` and is still active. A wrapper with nothing behind
    it for the action, or state that cannot be read, is unknown; when the state
    cannot be read before the action, nothing is sent.
  - *A unit that runs its own daemon* keeps its job result and its words (nginx,
    Arch's `postgresql.service`, an instance unit named directly, `wg-quick@`).
  - *In the catalogue only two units are wrappers:* `postfix` (Ubuntu 24.04;
    Debian before 13) and `postgresql` (Debian, Ubuntu). The other unit names
    are real units or aliases of them on the three platforms.
  - *The answers.* `502 SERVICE_ACTION_FAILED` with `reason` `check`, `reload`,
    `start`, `stop`, `verify` or `command`, and `502 SERVICE_ACTION_UNKNOWN`;
    both carry `vars.unit`, `vars.action`, `vars.command` (what the owner runs
    to read the service's own answer), `vars.detail` (one bounded line) and
    `vars.owner_unit` when another unit owns the daemon. A failed action used
    to answer `500` "internal server error". The audit entry says `unknown`
    for an unknown outcome instead of `failed`.
- **Not changed, and why.**
  - *The helper's command scope.* `postfix reload`, `postfix status`, `doveadm
    reload` and `systemctl show` stay outside it. Running Postfix's own
    commands from the helper's sandbox has never been measured, and it would
    bypass the unit's reload. The handshake answers the question the renewal
    asks.
  - *The check before publication* still uses `is-active`. A handshake that
    could refuse a publication would keep a renewed certificate from a server
    whose listeners it cannot reach; a certificate published while Postfix is
    stopped harms nothing, because Postfix reads it when it starts.
  - *The enrolled helper of an existing server.* See the transition below.
- **Schema or version transition.**
  - None in persisted state: ledger v1, accepted plan v1, receipt v1, the kit
    manifest schema and the three native templates are byte-identical, and no
    database schema changes.
  - The helper's bytes change, as with every release, so the kit generation of
    a release that contains this entry differs. An enrolled server keeps the
    generation it was enrolled with: its hook and unit name that generation's
    `renew`, a Panel update only publishes the new generation beside it
    (`prepare-mail-renewal-runtime`), and "existing independent schedules are
    preserved, including their current kit" (`MAIL-ENROLLMENT-RESERVATION.md`).
    Therefore: a server with the earlier Agent hook gets the check when its
    owner updates the Panel; a server enrolled from such a release on has it in
    its helper; an already enrolled server has it only when the updated Agent's
    worker takes the queue before the helper's timer does, and not at all when
    the Agent is absent. Moving an enrolled server to a newer generation is
    designed below and not implemented.
  - Agent RPC, additive: `ServiceActionResult.Outcome`, `.Stage`, `.Applied`,
    `.Detail`, `.Unit`. An older Panel ignores them and still sees `Error` for
    every answer that is not success. HTTP, additive: the two codes above; a
    successful answer gains `outcome`, `applied` and `unit`. Panel and Agent
    are installed together by one release.
- **Recovery behaviour.**
  - *Renewal.* The selected certificate stays selected; settings, ledger, queue
    and certificate evidence are untouched. The operation stays open and is
    retried by the same request within the existing budget (three executions,
    then the owner's explicit `--retry-selected` or `--retry-failed`), exactly
    as after a failed reload. The helper's journal line and the Agent's log
    line now say which service, whether it is "not complete" or "not
    confirmed", the command (`postfix reload`, `doveadm reload`, `postfix
    status`) and that the same operation resumes. While an operation is open, a
    later renewal is not admitted; that was already so.
  - *Services page.* Nothing retries and nothing is rolled back. A refused
    configuration stops start, restart and reload before anything is sent; a
    stop is always sent. The cached service scan is refreshed after a failed or
    unknown answer too.
- **Evidence.** Component tests only; native measurement pending.
  `cmd/agent/mail_served_certificate_test.go` (a wrapper that exits 0 while
  Postfix presents the previous certificate; Dovecot keeping it after its
  reload; a stale then a fresh process; no listener; a listener without TLS; a
  service bound to one host address; each STARTTLS; a cancelled operation; the
  sentences; the command scope not widened);
  `mail_host_certificate_reload_test.go` (the order reload, observe, ask; a
  reload without the check is refused); `service_action_verify_test.go`
  (Postfix and Dovecot actions on a wrapper that exits 0 while the daemon did
  not follow; a wrapper judged by the units behind it for each action, with the
  `ExecReload` text of the evidence folder as fixture; unknown; real units
  keeping their job result); `cmd/panel/service_action_outcome_test.go`. No
  test needs a mail server; the listeners are in-memory. No privileged-command
  guard entry was added: every command goes through the existing launchers,
  and the handshake is a connection, not a process.
- **Open.**
  - *Not measured on real services.* A native cell must show, on Ubuntu 24.04
    and Debian 13, with the Panel and Agent stopped and the enrolled helper
    doing the work: (a) a healthy renewal: the fingerprint presented on 465,
    587, 993 before and after equals the previous and then the selected
    certificate, the operation completes, the journal shows Postfix's and
    Dovecot's reload; (b) Ubuntu with the instance's reload failing (a line
    Postfix refuses in `main.cf`, as in P1): `systemctl reload postfix.service`
    exits 0 and the operation stays open or completes, with what the listeners
    present recorded each way; (c) Ubuntu with `postfix@-` stopped and the
    wrapper active: "not confirmed", nothing recorded as activated, completion
    after the owner starts Postfix; (d) Dovecot with a `local.conf` line it
    refuses: `doveadm reload` exits 0, the previous certificate stays, "not
    complete"; (e) the handshakes succeed inside the helper's sandbox; (f) what
    `systemctl is-active postfix.service` says on Ubuntu with the instance
    stopped. For the Services page: Reload and Restart of `postfix` on Ubuntu
    with a refused `main.cf`; `postgresql` on Debian and Ubuntu, each action,
    with a cluster that fails to start and with the owner's failing reload hook;
    the real `ConsistsOf`, `Wants`, `PropagatesReloadTo`, `ReloadResult` and
    `ExecReload` values; Dovecot's unit text on the three platforms.
  - *What "verified" proves for Postfix.* A Postfix server process started after
    the publication reads the selected certificate even when the master was not
    reloaded, and a process that was idle leaves within `max_idle`. The check
    proves that a listener presents the selected certificate; it does not prove
    that every process that was already running has been replaced.
  - *A server whose mail listeners cannot be reached on this host* (no TLS on
    25, 465, 587, or on 110, 143, 993, 995, on any of its own addresses), or
    whose owner gave every listener another certificate in `master.cf`: the
    renewal publishes the certificate, reloads, and then stays open as "not
    confirmed" or "not complete" until the owner continues it, and a later
    renewal waits behind it. Before this entry such a server was recorded as
    activated without evidence. The stack CelikPanel sets up opens these ports.
  - *Designed, not implemented: moving an enrolled server to a newer helper
    generation.* The file, loaded-unit and inverse primitives exist and have
    native evidence (`celikpanel-mail-renewal-transition/v1` with the complete
    previous kit, before-image v1, files v1, loaded v1; `MAIL-RENEWAL-KIT.md`).
    Missing are: a durable reservation and dispatcher for an "upgrade"
    operation beside enrollment's, admitted only by the owner's reviewed step;
    waiting for a running renewal instead of replacing its unit; the
    application rollback rule (an inverse back to the previous generation
    whenever the Agent declaration it was admitted with is restored); and the
    owner's view. The manifest schema stays v1 as long as the three templates
    do; a `v2` is needed only if a template or a manifest field changes, and
    then the reader must recognise both. The owner sees, on the mail
    certificate status, which release the renewal helper is from (the helper
    already answers `--inspect-build-identity`) and, when it predates this
    entry, that it reloads the services without confirming the certificate they
    present, with one reviewed action to move it and the same action as a
    root command of the new immutable helper for a server without the Panel.
    Until then the owner of an enrolled Ubuntu server checks after a renewal:
    `openssl s_client -connect localhost:465 </dev/null 2>/dev/null | openssl
    x509 -noout -fingerprint -sha256` against `openssl x509 -noout -fingerprint
    -sha256 -in /etc/ssl/celikpanel/_mail/host/current/fullchain.pem`, and runs
    `sudo postfix reload` when they differ.
  - *Screen text (merge of 2026-10-10).* The catalogue has the
    `err.SERVICE_ACTION_FAILED.*` and `err.SERVICE_ACTION_UNKNOWN` entries, in
    English and Turkish, and the Services screens show them in place with the
    service's line, the command and the unit that runs the service (operation
    guidance entry of this date). A verified failure stands on the failure
    surface and an unknown result on the attention surface. Inspected in a
    browser against a mock; not on a real service.
  - *A wrapper is recognised only on positive evidence.* When `systemctl show`
    cannot be read before the action, the unit keeps its own job result.
  - The `StartServiceMutation` RPC (Postfix and Dovecot, used when a mail
    service has been installed) now starts through the same verified path.

### A state-changing request carries one identity; a replay is answered, never run again (invariants 2, 4 and 6, 2026-10-10)

D-025 invariants 2 (a timeout is not proof of failure, success, or permission to
start again; unknown is its own state), 4 (the browser is an observer; retry is
bounded, idempotent and tied to one operation) and 6 (an operation's identity
stays reachable after reload or reconnect); constitution rule 3 ("refresh,
reconnect and timeout never authorize duplicate work"); D-024, D-029. No P0
item is closed or advanced. Found by a real-browser inspection and a read-only
inventory of the source at `7a64bda91`; not observed on an installed server.

- **Confirmed in the source at `7a64bda91`.**
  - *The browser repeats a request by itself.* When a connection is reset while
    a POST is being sent, Chrome sends it again: one click reached the Panel
    three times. Of about 115 state-changing routes, 12 are harmful when they
    run twice.
  - *A reset tore the first attempt.* Handlers passed the connection's context
    to the Agent call and to the database writes after it. The cancelled Panel
    returned while the Agent went on, and the replay ran against that state.
  - *The eight routes of this entry, each read in the code and reproduced by
    `TestRequestIdentityEightRoutesRepeatTheirEffectWithoutTheGuard`:*
    - restore of a domain backup: the Agent had no lock, so each arrival wrote
      a safety backup, replaced the document root and imported the databases,
      side by side;
    - cPanel import: stopped between two Agent calls with the site half
      imported (`TestImportApplyWithoutTheGuardStopsWhenTheConnectionGoesAway`);
      the replay was then refused as "domain already exists";
    - Let's Encrypt reissue: every arrival forced another issuance (three
      arrivals, three issuances);
    - manual backup: no job key, so no job lock and one archive per arrival;
    - the Panel's own account on a database engine: a new password per
      arrival, with nothing keeping two arrivals apart and the record written
      on the connection's context;
    - VPN peer: new keys and a new address per arrival (three peers);
    - database on a database server: MariaDB accepts the second `CREATE
      DATABASE IF NOT EXISTS`, the replay fails on the existing record, and its
      compensation drops the database the first request created and recorded;
    - database of a domain: created on the engine, not recorded by the Panel.
- **Changed.**
  - *The guard* (`cmd/panel/request_identity.go`), inside authentication and in
    front of the router. A request for one of the eight routes must carry
    `X-CelikPanel-Request-Id` (32 lowercase hexadecimal characters); without it
    the answer is `428 REQUEST_ID_REQUIRED` and the handler is not reached.
    The first arrival writes a `running` row and runs the handler on a context
    the connection cannot cancel, bounded by the route's time limit (restore 40
    min, import 2 h, issuance 25 min, backup 35 min, databases 15 min, engine
    account 12 min, VPN peer 10 min). The answer is stored, then sent. A
    handler never starts without its row.
  - *A replay* (same identity, actor and SHA-256 of method, path, query and
    body) is answered from the row. While the first is running it waits up to
    20 seconds, then gets `409 REQUEST_IN_PROGRESS` with the same identity. The
    same identity for anything else is `409 REQUEST_ID_REUSED`.
  - *Unknown stays unknown.* A row left `running` by a previous process, or by
    a handler that failed unexpectedly, is `interrupted`; its replay is `409
    REQUEST_OUTCOME_UNKNOWN` and the request is never executed again under
    that identity.
  - *Secrets are not retained.* The engine-account and VPN peer routes, a
    database answer that carries a password minted by that request, and any
    answer over 64 KiB keep only the status code; the replay is `409
    REQUEST_COMPLETED_RESULT_NOT_RETAINED` (reason `failed` when the first
    attempt ended with an error). The request body is never stored.
  - *At the Agent.* A second restore of a domain is refused while one is
    running, before anything is read or written (`409
    BACKUP_RESTORE_IN_PROGRESS` at the Panel). A manual backup sends its
    request identity as the job key (`request:<identity>`), so the Agent's job
    lock and its lookup of an archive this job already published apply.
  - *At the Panel.* Opening, re-keying and removing one database server's own
    account run one at a time, each reading the row again under the lock.
  - *In the browser.* The one fetch interceptor adds the header to every
    non-GET `/api/` call whose body does not carry `request_id`, one identity
    per action, never to another origin. On the eight routes a lost answer
    (connection failure, or a 408, 429, 502, 503 or 504 that is not JSON; the
    Panel's own refusal with one of these statuses is its answer and is shown)
    is asked for once more after 1.5 seconds with the same identity and that
    answer is used. When that brings no answer either, or the Panel answers
    `REQUEST_OUTCOME_UNKNOWN` or `REQUEST_IN_PROGRESS`, each of the eight
    screens shows the result as not known the way the fourth batch of the
    remote-state rule does for a change without an identity: a notice in place
    that says which happened, a read-only re-read, and the controls that
    change or remove off until that read answers. A change that was made while
    its one-time result reached nobody says what was made and what to do. The
    import page keeps the identity and the exact body of its request: starting
    again after the read-only check is the same request, which the server
    answers from the first run and cannot import twice.
  - Every other route behaves as before, with or without the header.
- **Schema or version transition.** Migration 43
  (`043_request_identities.sql`) creates `request_identities` and its expiry
  index through the ordinary ledger; no existing table changes. An older Panel
  refuses to start on a ledger that carries entry 43
  (`TestOlderReleaseRefusesALedgerWithTheRequestIdentitiesEntry`); rollback is
  the pre-update snapshot restore, as for every earlier migration. The table
  holds no owner data: after a restore that loses it a replay is a first
  arrival, which is what it was before this change. **Now required:** the
  header on the eight routes. Additive: the header on every other state-changing
  call (ignored), `X-CelikPanel-Request-Id` and `X-CelikPanel-Request-Replayed`
  on guarded answers, `vars.request_id` on the guard's refusals,
  `CreateRequest.JobKey` on manual backups (a field an older Agent already
  reads), the Agent answer `RESTORE_IN_PROGRESS`. New refusal codes:
  `REQUEST_ID_REQUIRED`, `REQUEST_ID_REUSED`, `REQUEST_IN_PROGRESS`,
  `REQUEST_OUTCOME_UNKNOWN`, `REQUEST_COMPLETED_RESULT_NOT_RETAINED`,
  `BACKUP_RESTORE_IN_PROGRESS`. A page opened before the update is refused on
  the eight routes until it is reloaded.
- **Recovery behaviour.** Nothing is retried or repaired automatically. At
  start the Panel marks every `running` row `interrupted` before it serves an
  application request; a `running` row that this process is not running is
  treated the same way when its replay arrives, so a failed start-up pass or a
  failed write of the outcome cannot turn into a wait for work nobody runs. An
  interrupted request has an unknown outcome: the owner checks the current
  state on the page and makes the change again only if it is missing. The
  Agent's work is not cancelled by the Panel's time limit or by a Panel
  restart; native services keep running without the Panel exactly as before,
  and rows expire after 24 hours.
- **Evidence.** Component tests; no native run.
  - Panel, the guard: `TestRequestIdentityFirstArrivalRunsAndReplayIsAnsweredFromTheRow`,
    `TestRequestIdentityNeverStoresTheRequestBody`,
    `TestRequestIdentityIsRequiredOnProtectedRoutesAndIgnoredElsewhere`,
    `TestRequestIdentityReusedForADifferentRequestIsRefused`,
    `TestRequestIdentityReplayWhileRunningWaitsThenSaysInProgress`,
    `TestRequestIdentityRunningRowsAreInterruptedAtStartAndNeverReExecuted`,
    `TestRequestIdentityOversizeAnswerKeepsOnlyItsStatus`,
    `TestRequestIdentitySecretAnswersAreNeverStored`,
    `TestRequestIdentityExpirySweep`,
    `TestRequestIdentityHandlerPanicLeavesATruthfulRow`,
    `TestRequestIdentityHandlerOutlivesTheConnection`,
    `TestRequestIdentityConcurrentArrivalsRunOnce`,
    `TestRequestIdentityHandlerDoesNotRunWithoutItsRow`,
    `TestKeyedLocksSerialisePerKeyAndForgetIdleKeys`.
  - Panel, the eight routes through the product's dispatcher with a fake Agent
    or database driver: `TestRequestIdentityEightRoutesSentThreeTimesInARow`,
    `TestRequestIdentityEightRoutesSentThreeTimesAtOnce` (one effect, the same
    answer; for a one-time answer exactly one arrival gets it),
    `TestRequestIdentityEightRoutesRepeatTheirEffectWithoutTheGuard`,
    `TestRequestIdentityRoutesMuxMirrorsMain`,
    `TestImportApplyIsNotCutWhenTheConnectionGoesAway`,
    `TestImportApplyWithoutTheGuardStopsWhenTheConnectionGoesAway`,
    `TestDatabaseAdminAccountChangesAreSerialisedPerServer`,
    `TestRestoreRefusedByTheAgentWhileAnotherRunsIsANamedRefusal`. The import
    case ends at the import's second Agent call; a whole import was not run.
  - Agent: `TestOnlyOneRestoreOfADomainHoldsTheLock`,
    `TestConcurrentRestoreClaimsNeverOverlap`,
    `TestRestoreBackupIsRefusedWhileAnotherRestoreOfTheDomainRuns`,
    `TestSecondRestoreOfADomainIsRefusedWhileTheFirstRuns`,
    `TestManualBackupWithTheRequestJobKeyPublishesOnce`.
  - Migration: `TestRequestIdentitiesMigrationContracts`,
    `TestRequestIdentitiesMigrationAppliesToAnExistingDatabase`,
    `TestOlderReleaseRefusesALedgerWithTheRequestIdentitiesEntry`.
  - Web: `web/tests/request-identity-runtime.test.mjs` (also: the Panel's own
    refusal is never asked for again; one definition of a result that is not
    known for the eight screens); the backup cases of
    `web/tests/remote-state-mounted-batch4.test.mjs`;
    `web/tests/service-action-outcome.test.mjs` (the status-only answers); the
    import case of `web/tests/remote-state-mounted.test.mjs`. Harness:
    `deploy/e2e/dns-pair-acceptance/test_panel_api.py` (the driver names its
    unsafe requests the way the web interface does);
    `test_populated_database.py`, `test_database_exchange_rows.py` and
    `test_guest_populated_baseline.py` (the schema 43 pin).

Open: a real, installed Chrome was run against a loopback mock that keeps this
guard's contract (`web/tools/browser-inspect`, scenarios `idbackup`,
`idrestore`, `idcertificate`, `iddomaindb`, `idserverdb`, `idaccount`,
`idpeer`, `idimport`, `idrefusal`, `serviceaction`; 53 states in each of eight
configurations: 1440x900 and 390x844, English and Turkish, light and dark).
With the connection really reset, one click reached the mock up to 10 times
(the requests Chrome repeats by itself and the page's one second asking), every
arrival under one identity, and each of the eight changes was made once. That
is the browser against a mock, not against this guard; no Panel was restarted
in the middle of one of the eight; no real restore, import, issuance, backup or
database engine was run. The route time limits and the 20-second wait are chosen, not measured. The
time limit bounds the Panel's handler, not the Agent's work, which the Agent
RPC cannot cancel. A Panel that stops while a guarded request runs leaves the
Agent's work unreconciled: the row says unknown and the owner checks. The
owner-started panel update does not yet look at `running` request identities
before it stops the Panel. The guard answers a repeat of the same request; two
separate clicks are two requests and both run, except where a lock of the
route itself refuses or orders them (restore, engine account, certificate). The
engine-account answer carries no password and is still kept as status only; the
screen treats that replay as the success it was. A manual backup now stops on
an unreadable `.cpbak` in the domain's backup directory, as a scheduled backup
already did. The update harness's populated-database proof
(`deploy/e2e/release-recovery/populated_database.py`) pins schema 43 beside 38
and 42 since the merge of 2026-10-10: the migration ledger digest
`a0b5c4247f83...` and the schema digest `48cbd3b47573...`, computed from the 43
migration files the way its offline tests build their input (the same
computation reproduces the two existing pins), 66 tables: the 65 of schema 42
and `request_identities`. `verify_copy` and `guest_populated_baseline.py verify
--expected-version` accept 43; `database_exchange_rows.verify_pair` reads from
the candidate's own ledger which pinned schema it reached, holds it to that
pin, and requires `request_identities` to be empty after a migration-only
exchange. A cell that verifies this candidate passes 43 where it passed 42. No
cell was run with it. The Panel has no control that sets a database user's
password, so "a minted database password is set again" (D-029) is done on the
engine; the screen says so. Not covered: the other harmful routes of the
inventory (service and application restart, plans, enrollments), the about 38
routes that report wrongly after a replay, the 7 unclassified routes and the
version-token routes.

### Corrections from the second native measurement (invariants 1-4 and 6, 2026-10-11)

D-025 invariants 1 (the owner's native configuration is detected, not
replaced), 2 (unknown is not absent, empty or success), 3 (the unsafe write is
stopped at its own boundary), 4 (a mutation reads its pre-image, validates and
has a tested inverse) and 6 (the screen renders the authoritative state);
D-022, D-024, D-029. No P0 item is closed or advanced. Source: the `set2` run
of 2026-10-09 (UTC) on disposable QEMU/KVM guests (Debian 13, Ubuntu 24.04,
Arch; evidence `deploy/e2e/release-recovery/evidence/set2-20261011/`), which
confirmed the corrections of the entry before this one and measured three
candidate defects (P3, P4, P5) and the observations O6-O15. Nothing here was
observed on an installed server. This entry corrects them in source; none of
the corrections has been measured on real services.

- **Measured.**
  - *O15, a secret answered to the browser.* `POST
    /api/v1/import/cpanel/inspect` answered each mailbox's password hash
    (`mail_accounts[].crypt_hash`): the handler encoded the Agent's answer as
    it came. The apply route already read the archive again on the server; the
    page sent the archive's path and the owner's choices, never the hashes.
  - *P3, a failed Reload answered "keeps running with the settings it had",
    which nobody had read.* (a) PostgreSQL with an owner's drop-in whose
    `ExecReload` signals the server and then fails: the instance's
    `ReloadResult` is `exit-code` and `pg_conf_load_time()` had moved; the
    server had re-read its files. (b) A Reload of a stopped Postfix or Dovecot
    got the same sentence.
  - *O8, Stop of Postfix with a refused `main.cf` answered `502
    SERVICE_ACTION_UNKNOWN` although the master was gone.* `postfix status`
    reads `main.cf` before it answers, so it cannot answer in that state.
  - *P4, the files step of an import refused an archive that holds the
    directory member `homedir/public_html/`* ("unsafe cpmove member path"), the
    route answered `202` with `status: pending`, and the domain, the mailbox
    and the database were imported while the site files were not. Nothing was
    pending: every step had ended.
  - *P5, Arch: a PHP site could not be created, so every import answered
    `500`.* The pool file was written to `/etc/php/8.5/fpm/pool.d/`, the Debian
    layout, and the reload named a unit `php8.5-fpm`.
  - *O9.* A certbot run that did not issue answered `500 INTERNAL` "internal
    server error".
  - *O10.* `database-servers/{id}/databases` answered the new user's password
    also when the caller had sent it, so the answer was not kept and a replay
    got the status-only refusal.
  - *O11.* A service action was refused `409 server_setup_busy` while the setup
    only waited for a public DNS record, for the moments in which its runner
    asked the resolvers again.
  - *O14.* The Databases page showed `15.1` for a MariaDB 10.11.14 (Ubuntu
    24.04: the client's version) and the literal `VERSION()` on Arch.
- **Changed.**
  - *The import preview and the hashes*
    (`internal/transport/cpmove_contracts.go`, `cmd/agent/cpmove_rpc.go`,
    `cmd/panel/import_handlers.go`). The browser is answered a type of its own,
    with every field named: a mailbox is its address, its quota and
    `has_password`. `CpmoveMailAccount.CryptHash` is never encoded as JSON
    (`json:"-"`); it travels from the Agent to the Panel over their local RPC,
    and only when the Panel asks for it
    (`CpmoveInspectRequest.IncludeMailHashes`), which only the apply of an
    import with mail does. The apply reads the archive again on the server, as
    before. A mailbox whose password field is not a crypt hash (a suspended
    one) is now listed with `has_password: false` instead of being left out,
    and an apply reports it as a step that was not imported instead of skipping
    it silently.
  - *A failed reload says only what was verified*
    (`cmd/agent/service_action_verify.go`,
    `cmd/panel/service_action_outcome.go`, `internal/transport/rpc.go`). Three
    new stages. `not_running`: a reload of a stopped Postfix or Dovecot, or of
    a wrapper with no running unit behind it; answered `409`, nothing was sent
    to the daemon. `reload_reread` and `reload_not_reread`: PostgreSQL only.
    Before a reload the Agent reads, over the local socket and without sending
    a signal, the postmaster's process ID (`postmaster.pid`),
    `pg_postmaster_start_time()` and `pg_conf_load_time()`; after a reload that
    the unit reported as failed it reads them again. Only when both readings
    are of the same server process and that process is the unit's main process
    is a later load time "it re-read its files" and an unchanged one, read
    twice, "it did not". In every other case, and for every service that cannot
    be asked, the stage is the plain `reload`, whose sentence claims neither
    state.
  - *Stop of Postfix is judged by the master's process*
    (`cmd/agent/mail_service_verify.go`). After `systemctl stop postfix` the
    Agent reads the process ID in `<queue_directory>/pid/master.pid` and
    `/proc/<pid>/comm`: the master is gone when that entry does not exist or
    belongs to another program. `postfix check` is no longer asked for a stop.
    Only when the process cannot be looked for is Postfix asked itself, as
    before, and only then can a refused configuration make the outcome unknown.
  - *Directory members of an archive* (`cmd/agent/cpmove_extract_linux.go`).
    One trailing slash is taken off the name of a member that is a directory,
    the way tar stores it. Basis: the tar format (POSIX ustar typeflag `5`; GNU
    tar, Python's `tarfile` and Go's `tar.FileInfoHeader` all write the slash)
    and the import's own inspection, which already cleaned the name; the
    repository holds no real cPanel archive, and none was available to the run.
    Every other refusal stands and is tested: a `..` component anywhere, a
    backslash, a NUL, a doubled trailing slash, a payload path that cleans to
    nothing, a file's name with a trailing slash, symbolic links, hard links,
    devices.
  - *An import answers what it came to* (`cmd/panel/import_handlers.go`). `200`
    with `status: "active"` when every chosen part was imported, or `status:
    "partial"`, `code: IMPORT_PARTIAL`, the lists `imported` and
    `not_imported`, `domain_status` and a `message` (D-024). It never answers
    `202` or `status: "pending"`. A site that could not be created is `502
    IMPORT_SITE_NOT_CREATED` instead of a bare `500`.
  - *PHP-FPM on a single-unit host* (`internal/services/php_layout.go` and the
    five files that built the Debian paths; `cmd/agent/site_rpc.go`,
    `cmd/agent/vhost_rpc.go`). Decision: PHP sites on Arch are meant to be
    supported. Basis in the source: the catalogue maps `php-fpm` for pacman and
    names Arch's single unit, the instance listing asks the program for the
    version on that layout, and the capability read was changed (B3b) so that
    Add Domain offers PHP there. The layout is read from the host, as the
    instance listing reads it: a version with its own tree under `/etc/php` is
    the versioned layout; a host without it but with `/etc/php/php-fpm.d` is
    the single-unit layout (pools in `/etc/php/php-fpm.d/`, unit `php-fpm`,
    program `php-fpm`, `/etc/php/php.ini`, sockets under `/run/php-fpm/`). The
    socket's owner is the web server account that exists (`www-data`, `nginx`,
    `http`).
  - *A certbot failure is typed* (`cmd/agent/certbot_failure.go`,
    `cmd/panel/certificate_issue_failure.go`). The Agent reads certbot's own
    output for three kinds (the authority could not be reached, it refused a
    validation, one of its limits was reached), adds `timeout` and `tool`, and
    one bounded line. The answer is `502 CERTIFICATE_ISSUE_FAILED` with the
    kind as `reason`; certbot's line goes to an administrator only.
  - *A waiting setup does not refuse service actions*
    (`cmd/panel/server_setup_operations.go`). The rule: an execution is
    mutating while its row is `running`, except when the step it is at is the
    public address check (`access_dns`), which only asks public resolvers. A
    row that cannot be read as an execution counts as mutating.
  - *A password the caller sent is not sent back*
    (`cmd/panel/database_v2_handlers.go`). The database and the database-user
    routes answer `password_set: true`, and `password` only when the request
    minted it. The first answer is therefore kept and replayed like any other.
    No other route was found that echoes a sent password.
  - *The MariaDB version is the server's*
    (`internal/services/mariadb_driver.go`, `version_detector.go`,
    `cmd/panel/database_admin_account_handlers.go`). The running engine is
    asked whenever the Panel has an account on it, with a statement that marks
    its own answer (`version=...`); the service scan asks the server program
    (`mariadbd --version`), never the client.
- **API changes (for the release notes).**
  - `POST /api/v1/import/cpanel/inspect`: `mail_accounts[].crypt_hash` is gone;
    `mail_accounts[].has_password` (boolean) is new. A mailbox without a
    password in the archive is now listed.
  - `POST /api/v1/import/cpanel/apply`: `200` in place of `202` for an import
    that ended with a part not imported; `status` is `active` or `partial` (it
    was `active` or `pending`); new fields `domain`, `domain_status`, `code`,
    `message`, `imported`, `not_imported`. New refusal `502
    IMPORT_SITE_NOT_CREATED`.
  - `POST /api/v1/service/action`: new reasons `not_running` (answered `409`),
    `reload_reread`, `reload_not_reread`; a Stop of Postfix with a refused
    `main.cf` answers `200` when the master is gone.
  - `POST /api/v1/domains/{id}/ssl/letsencrypt`: `502 CERTIFICATE_ISSUE_FAILED`
    with a `reason` in place of `500 INTERNAL` for a certbot run that did not
    issue.
  - `POST /api/v1/database-servers/{id}/databases` and `.../users`: `password`
    only when the request minted it; `password_set` is new.
- **Not changed, and why.**
  - O6 and O7 (a restarted Panel does not cut a running restore; after a killed
    Panel the Agent finished it while the row says the outcome is unknown) are
    what D-029 describes; reconciling the row with the Agent is not part of
    this entry.
  - O12: a Reload of MariaDB is answered with systemd's own line. The Services
    screens send Start, Stop and Restart only, so no screen offers that Reload;
    it is reachable through the API. O13 and O2 are unchanged.
  - The other DNS waits of a setup (`primary_dns`, `infrastructure_dns`) still
    refuse service actions while they are re-checked: their re-check can
    publish native DNS records. The wait for the final verification (which
    holds the reverse-DNS check) never sets the row to `running`, so it never
    refused.
  - On a single-unit host the extension directories (`mods-available`,
    `conf.d`) are still the Debian ones, and a PHP version switch still builds
    its socket path on the Panel; such a host has one version to switch to.
  - A version already recorded for an engine stays until the Panel's account on
    it is provisioned again.
  - The Domains page treats a domain's `pending` status as a deletion that may
    be waiting and asks for its marker; a domain left by a partial import has
    none and is drawn as an ordinary row.
- **Schema or version transition.** No database schema and no persisted state.
  The Agent's RPC gains fields that are transferred by name:
  `CpmoveMailAccount.HasPassword`, `CpmoveInspectRequest.IncludeMailHashes`,
  `IssueLetsEncryptResponse.Failure` and `FailureDetail`, and three
  `ServiceActionResult.Stage` values. The archive calls already require the
  Panel and the Agent to be the same build. A Panel that meets an Agent without
  the new fields shows a mailbox as having a password when a hash arrived,
  answers a certbot failure as before, and has no sentence to choose for a
  stage it is not sent. A `request_identities` row written before the update
  replays the answer it stored, in its old shape, until it expires.
- **Recovery behaviour.** Nothing retries by itself. A partial import leaves
  the domain and what was imported in place; the owner adds the missing parts
  by hand, or removes the domain and imports the archive again, and an import
  into an existing domain is refused. A certificate request that failed added
  nothing: what certbot left of it is removed before the answer, and a
  certificate the site had keeps serving. A reload that was reported as failed
  is not repeated by the Panel, and no signal is sent to PostgreSQL to learn
  what it did.
- **Evidence.** Component tests only; the native re-run is pending. Agent:
  `cpmove_set2_linux_test.go` (an archive as tar writes it, every refusal, the
  hashes handed over only to an apply), `service_action_verify_test.go` and
  `mail_service_verify_test.go` (each PostgreSQL reading, a stopped daemon,
  Stop judged by the master's process), `certbot_failure_test.go`. Panel:
  `set2_corrections_test.go` (no answer, stored answer or log line holds a
  hash-shaped value; the partial answer; the certificate answer; a sent
  password is not echoed and its answer is replayed; the engine is asked for
  its version), `server_setup_busy_rule_test.go`,
  `service_action_outcome_test.go`. Services: `php_layout_test.go`. Screens:
  `web/tests/set2-corrections.test.mjs`, and a real Chrome against the loopback
  mock (`web/tools/browser-inspect`, scenarios `importpreview`, `importresult`,
  `reloadwording`, `certfailure`; desktop and phone, English and Turkish, light
  and dark). No real cPanel archive, no certificate authority and no Arch host
  was used.
- **Open.**
  - No correction here has been measured on real services. The native re-run
    must show: the preview and every stored answer without a hash and the
    mailbox imported with its password; an archive with directory members
    imported completely, and a failed files step answered `200` / `partial`;
    the PostgreSQL hook sequence answered `reload_reread`, a stopped Postfix
    and Dovecot answered `409` / `not_running`, and Stop of Postfix with a
    refused `main.cf` answered `200` / `stopped`; on Arch a PHP site created,
    served and deleted, and an import completed; a certbot failure answered
    `502 CERTIFICATE_ISSUE_FAILED`; no `server_setup_busy` while the setup
    waits at `access_dns`; a sent database password not echoed and its replay
    answered from the row; the server's MariaDB version on all three platforms.
  - Whether a PHP page executes on Arch under the packaged unit's hardening,
    and whether `/run/php-fpm` is where a site's socket may live there, is
    established only by that run.
  - Real cPanel archives (the home directory as a nested `homedir.tar`,
    suspended mailboxes, large sites) have not been imported.

### Corrections from the final native round (invariants 1-4 and 6, 2026-10-12)

D-025 invariants 1 (the owner's native configuration is detected, not
replaced), 2 (unknown is not absent, empty or success), 3 (the unsafe write is
stopped at its own boundary), 4 (a mutation reads its pre-image, validates and
has a tested inverse) and 6 (the screen renders the authoritative state);
D-022, D-024, D-029. No P0 item is closed or advanced. Source: the `set3` run
of 2026-10-09 (UTC) on disposable QEMU/KVM guests (Debian 13, Ubuntu 24.04,
Arch; evidence `deploy/e2e/release-recovery/evidence/set3-20261012/`), which
passed every corrected item of the entry before this one on Debian 13 and
Ubuntu 24.04 and the whole update matrix from the published v0.1.0-alpha.81,
and measured one candidate defect on Arch (P5b) and the observations O16-O21.
Nothing here was observed on an installed server. This entry corrects them in
source; none of the corrections has been measured on real services.

- **Measured.**
  - *P5b, Arch: a PHP site still could not be created.* The pool was written
    and PHP-FPM accepted it; then nginx refused the virtual host (`open()
    "/etc/nginx/snippets/fastcgi-php.conf" failed`). The PHP vhost included
    `snippets/fastcgi-php.conf`, a file Debian's and Ubuntu's nginx packages
    ship (nginx-common) and Arch's does not; Arch ships `fastcgi.conf` and
    `fastcgi_params` in `/etc/nginx` and has no `snippets` directory. The Agent
    restored the vhost and removed the account; the Panel answered `500
    INTERNAL`, and an import `502 IMPORT_SITE_NOT_CREATED`. With that one file
    placed by hand (second reading, an owner action) the site was created,
    served PHP as its own account under the packaged unit and was deleted
    cleanly, and every import completed.
  - *O21.* On that guest the site's socket was
    `/run/php-fpm/php8.3-fpm-site2.sock` although the only PHP is 8.5.11: the
    create handler read `/etc/php/<version>/fpm` on the Panel's own disk and
    took the literal `8.3` when it found nothing.
  - *O16.* A Reload of a stopped nginx and of the stopped PostgreSQL wrapper
    answered `502` / `command` with systemd's line ("postgresql.service is not
    active, cannot reload."): systemd refused the reload before the Agent's own
    reading applied. Only Postfix and Dovecot were answered `409` /
    `not_running`.
  - *O17.* An archive member named by an absolute path was left out of the
    import and the answer was `200` / `active` without a word about it. Nothing
    had been written for it.
  - *O19.* After a Stop of Postfix with a main.cf it refuses, the answer was a
    truthful success (the master was gone) and systemd showed the unit as
    `failed`, `Result=exit-code` (`postfix.service` on Debian 13,
    `postfix@-.service` on Ubuntu 24.04). The journal shows why: the unit's own
    stop command (`postfix stop`) reads main.cf first and exits 1 ("fatal: bad
    numerical configuration"), and systemd then ends the processes itself.
  - *O18.* After an automatic return from a defective candidate the update
    check still offers the same version, with `previous_attempt: {phase:
    recovered}`; a typed cause is in it only when the recovery record holds one
    (the start-check candidate), not for a candidate whose migration failed.
- **Changed.**
  - *The PHP hand-off is written out in the vhost*
    (`internal/services/templates/nginx/vhost.conf.tmpl`). The six directives
    of Debian's `snippets/fastcgi-php.conf` are rendered in its place, in its
    order: `fastcgi_split_path_info`, the `try_files $fastcgi_script_name =404`
    guard, `set $path_info`, `fastcgi_param PATH_INFO`, `fastcgi_index
    index.php`, `include fastcgi.conf`. The lines that followed the include are
    unchanged. A vhost now includes only `fastcgi.conf` and `fastcgi_params`,
    nginx's own files, which every package installs. No file is added to the
    server and none of the owner's is touched. For a site that was generated
    with the include, a re-render is the same configuration with the include
    expanded: nginx reads an include as the included file's text in its place.
  - *A site the web server refused is answered as that*
    (`internal/services/nginx_generator.go`, `cmd/agent/site_rpc.go`,
    `internal/services/site_orchestrator.go`,
    `cmd/panel/domain_web_server_refused.go`). `nginx -t` that exits with its
    own status is typed (`NginxConfigRefusedError`), and so is a vhost change
    whose inverse ran to the end (`VhostRestoredError`: the file and link are
    what they were, nginx accepted the previous configuration and was
    reloaded). Only when both hold does the Agent answer
    `WebServerRefusedConfig` with nginx's first `[emerg]` line. The Panel
    answers `502 SITE_WEB_SERVER_REFUSED`; its `reason` says whether the site's
    deletion on the Agent (the orchestrator's compensation, which answers
    success only when the vhost, the pool, the account and the site's directory
    are gone) was confirmed. nginx's line is shown to an administrator only.
  - *The PHP version of a new site is one the server runs*
    (`cmd/panel/domain_handlers.go`, `domain_php_handlers.go`,
    `internal/services/service_scanner.go`). The create handler takes the
    newest version the Agent reports (the program's own answer on a host with
    one unversioned PHP-FPM) and refuses a named version the server does not
    run (`409 PHP_VERSION_NOT_INSTALLED`) before anything is created. A version
    switch records the socket the Agent wrote the pool with
    (`services.PHPFPMSocketPath`) instead of building Debian's path. The list
    answers no PHP version for a domain without a site (it answered `8.3`). The
    configuration files listed for PHP-FPM are those of the installed layout,
    with no assumed version.
  - *An application unit runs as the web server's account of the host*
    (`cmd/agent/app_rpc.go`). The Panel asks for `www-data`; where that account
    does not exist the unit is written with `nginx` or `http`, in the order the
    rest of the product reads them.
  - *A reload of anything that is not running is `not_running`*
    (`cmd/agent/service_action_verify.go`). Read before anything is sent: a
    wrapper when every unit its reload reaches is `inactive` or `failed`, any
    other unit when it is loaded and `inactive` or `failed` itself. A state in
    between, a unit that is not loaded and a state that could not be read keep
    the service manager's own answer.
  - *A Stop that left the unit marked as failed says so and leaves the mark*
    (`cmd/agent/ service_action_verify.go`,
    `cmd/panel/service_action_outcome.go`). Decision: the Agent does not run
    `systemctl reset-failed`. The mark is systemd's own record of what the
    unit's stop command did, and the same one the owner's own `systemctl stop
    postfix` leaves on that host, so the Panel adds no native state of its own;
    clearing it would remove from `systemctl --failed` and from monitoring the
    one native sign that the configuration is refused and that the service
    cannot be started as it is, and would reset the unit's start-limit
    counters, which a Stop was not asked to do. Nothing the Agent could send
    avoids the mark: systemd runs the unit's stop command for every way of
    stopping it. The unit is read before the stop and after a verified one; a
    unit that was not failed before and is failed after is reported in the
    success (`note`), with systemd's result and, for Postfix, the line its own
    check prints now.
  - *What the files step of an import leaves out is counted and named*
    (`cmd/agent/cpmove_left_out.go`, `cpmove_extract_linux.go`,
    `cmd/panel/import_handlers.go`). Every member ends in exactly one of:
    imported; the whole step refused, as before, now naming the member and what
    it is (a symbolic link, a hard link, a device node, a named pipe, a name
    with `..`, a backslash or a NUL, a payload over the size or count limit);
    refused by its name and left out (an absolute path), which is listed as a
    step `member:<name>`, at most 20 and then a count, and makes the import
    `partial`; or outside `homedir/public_html`, which is counted by the folder
    of the archive it is in and said on the files step's own line. An import
    whose only missing entries are refused members is `partial` with
    `domain_status: active`: every chosen part is there, and the same archive
    would be refused the same entries again.
  - *The update card says before Start what already happened*
    (`web/src/components/PanelUpdateCard.tsx`). A version that was tried here
    and returned has its own heading, says what the server runs now, the
    recorded cause or that none was recorded, and what starting it again does.
    The version is not hidden and Start is not disabled.
  - *H42* (`deploy/e2e/release-recovery/build-upd1-artifacts.sh`): a dist build
    that fails or is refused stops the builder instead of being followed by the
    archive an earlier build left under the same name.
- **Other assumptions of the Debian layout that were looked for.**
  - Corrected here: the snippet file; the literal `8.3`; the socket path of a
    version switch; `www-data` as the account of an application unit; the
    PHP-FPM file list of the configuration scan.
  - Already read from the host, and seen working in the second Arch reading:
    the vhost directories (`sites-available`, `sites-enabled`, added to Arch's
    `nginx.conf` by the web server's setup step), the web server account, the
    pool directory, unit and socket directory of PHP-FPM, `/var/log/nginx`.
  - Not corrected: on a single-unit host the PHP extension directories are
    still Debian's; MariaDB's settings file is
    `/etc/mysql/mariadb.conf.d/50-server.cnf`; `DetectInstalledPHPVersion`
    still reads versioned trees only (the webmail setup uses it for package
    names that exist on Debian only); a site that was created as static gets
    PHP through the hosting type change, which was not exercised on Arch.
- **API changes (for the release notes).**
  - `POST /api/v1/domains/create`: `502 SITE_WEB_SERVER_REFUSED` with `reason`
    `removed` or `cleanup_unconfirmed`, `vars` and, for an administrator,
    `details`, in place of `500 INTERNAL` when nginx refused the site; `409
    PHP_VERSION_NOT_INSTALLED` for a named PHP version the server does not run.
  - `POST /api/v1/import/cpanel/apply`: `502 SITE_WEB_SERVER_REFUSED`
    (`import_removed`, `import_cleanup_unconfirmed`) in place of `502
    IMPORT_SITE_NOT_CREATED` for that cause; steps `member:<name>` and
    `members:<n>`; `status: partial` with `domain_status: active` when only
    such entries are missing; the files step's `detail` is longer.
  - `POST /api/v1/service/action`: `409` / `not_running` for a reload of any
    stopped unit (it was `502` / `command`); a successful Stop may carry
    `note`.
  - `GET /api/v1/domains`: `php_version` is empty for a domain without a site.
- **Not changed, and why.**
  - An owner who had edited the package's
    `/etc/nginx/snippets/fastcgi-php.conf`: a site's vhost no longer reads that
    file after its next re-render. The file is left in place and nothing
    detects that it differs from the packaged one.
  - The same refusal by nginx during a change of hosting type, of a certificate
    or of a site's settings still answers its earlier error; only site creation
    and the import carry the new answer.
  - A `..` component, a link or a device below the site folder still refuses
    the whole files step; only an absolute name is left out by itself.
  - O18: the check still offers the version and the release floor file stays at
    the candidate's sequence after a return. The update's own line ("offline
    panel database migration failed ...") is shown by the outcome notice after
    the return; the card before Start shows the typed cause or that none was
    recorded.
  - O20 (the published alpha.81's `web_mail` plan on Arch) is a harness limit
    of the baseline, not of this source.
- **Schema or version transition.**
  - No database schema and no persisted state. The Agent's RPC gains fields
    that are transferred by name: `CreateSiteResponse.ErrorDetail` and the
    error code `web_server_refused_config`; `ServiceActionResult.Notice`,
    `NoticeUnit`, `NoticeResult`, `NoticeDetail`;
    `CpmoveExtractResponse.Refused`, `RefusedCount`, `OutsideCount`,
    `OutsideGroups`. A Panel that meets an Agent without them answers as
    before. Vhost files on disk are not rewritten by an update: a site's vhost
    takes the new text when it is next rendered (a certificate, a setting, a
    hosting change), and that render is the same configuration. Sites and
    sockets already recorded with a version keep it.
- **Recovery behaviour.**
  - Nothing retries by itself. A site nginx refused is removed again by the
    Agent's own inverse and by the Panel's compensation; when that is not
    confirmed the domain row is kept so that it can be deleted from the Domains
    page. A reload of a stopped unit sends nothing. A Stop is never followed by
    `reset-failed`. An import with refused members keeps everything it imported
    and marks the domain as finished.
- **Evidence.**
  - Component tests only; the native re-check on Arch is pending. Services:
    `nginx_php_handoff_test.go` (the rendered PHP location is, directive for
    directive, the old one with Debian's file put in for the include, whose
    text is pinned by the SHA-256 the set3 run recorded; no vhost of any
    project type names a file under `snippets/` or includes anything but
    nginx's two files), `set3_corrections_test.go`. Agent:
    `set3_corrections_test.go`, `cpmove_set3_linux_test.go`. Panel:
    `set3_corrections_test.go`. Screens: `web/tests/set3-corrections.test.mjs`,
    `panel-update-card-mounted.test.mjs`, and a real Chrome against the
    loopback mock (`web/tools/browser-inspect`, scenarios `siterefused`,
    `importentries`, `stopnote`, `updaterolledback`; desktop and phone, English
    and Turkish, light and dark).
  - A local check with Debian 13's own nginx (`nginx` and `nginx-common`
    1.26.3-3+deb13u7, fetched and unpacked into a private directory of the
    development guest, not installed; loopback only): the package's
    `snippets/fastcgi-php.conf` has the pinned SHA-256 (`a9dd98bf...411f2`);
    `nginx -t` accepts the vhost as the previous source renders it on a layout
    with the snippet and refuses it without it, with the line measured on Arch;
    `nginx -t` accepts the vhost as this source renders it on a layout that has
    no `snippets` directory; and for 17 requests (a script, a query string,
    `PATH_INFO`, a missing script, the front controller, a static file, a dot
    file, the ACME location) the status and every FastCGI parameter, in order,
    are the same under both. Not checked: Ubuntu's package, Arch's nginx,
    PHP-FPM itself.
- **Open.**
  - The native re-check must show, on Arch: a PHP site created on a stock host,
    a PHP page executed as the site's account, the site deleted, an import
    completed; the socket and the recorded version carrying the installed PHP's
    version. On Debian 13 and Ubuntu 24.04, because the vhost template changed:
    that `/etc/nginx/snippets/fastcgi-php.conf` has the pinned SHA-256; a PHP
    site created, a PHP page executed, `PATH_INFO` and a missing script
    answered as before; a site created by the published alpha.81 re-rendered by
    this source with `nginx -t` passing and the page still served.
  - Also to be measured: a refused site answered `502 SITE_WEB_SERVER_REFUSED`
    with nothing of it left; a reload of a stopped nginx, MariaDB and
    PostgreSQL wrapper answered `409`; the Stop of Postfix with a refused
    main.cf answered `200` with the note and the unit still `failed`; the
    absolute member listed and the domain `active`.
  - Real cPanel archives have not been imported; the count of entries outside
    the site folder was seen only on fixtures.
