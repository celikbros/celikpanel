# Resilient operation: architecture contract and implementation audit

*September 14, 2026 · [Türkçe](RESILIENCE-CONTRACT.tr.md) · D-025*

**Status: required direction and reviewed implementation plan, not completed capability.**
The owner requested an examination of the constitution and architecture after
repeated Frankfurt failures. Alpha80 corrects the observed BIND and rollback
faults; it does not complete this contract. No installed server is changed by
this document. Installed-panel updates remain user-initiated in CelikPanel.

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

The P0 identifiers above are the tracked work items. Evidence is updated through September 21:

| Item | Recorded status | Completion evidence |
|---|---|---|
| P0.1 | Partial — native old-release restoration passed at one checkpoint on Arch and Debian | [Unit-transition acceptance](../deploy/e2e/release-recovery/UNIT-TRANSITION.md) records real restoration and running old binaries after SIGKILL. The broader fault/workload matrix, consistent database semantics and signed candidate admission remain open. |
| P0.2 | Partial — typed access, Agent-independent startup observation and native recovery entrypoint implemented | [Access/observation acceptance](RECOVERY-ACCESS.md) and [independent runtime](RECOVERY-RUNTIME.md). Root/sudo status and recovery do not require Panel/Agent startup or licensing. [Native AJ](../deploy/e2e/release-recovery/BOUND-WORKER.md) proves a real worker kill, reboot during automatic recovery, verified rollback and matching CLI/authenticated HTTP/browser terminal results in a disposable Debian schema42-to42 fixture. [Native AK](../deploy/e2e/release-recovery/BOOT-WAIT.md) further proves real `starting` guidance in the root CLI and same-request timer retry to rollback. Other waits, prior-known-failure preservation through a native wait, HTTP/browser wait access, UI update-start admission, production signing and the full fault/access matrix remain open. |
| P0.3 | Partial — independent code/data, atomic publication and selected native recovery SIGKILL/reboot boundaries passed | [Independent runtime](../deploy/e2e/release-recovery/INDEPENDENT-RUNTIME.md) and [material acceptance](../deploy/e2e/release-recovery/RECOVERY-MATERIAL.md): three retained candidate files were unavailable; Arch payload_restored SIGKILL and Debian runtime_verified reboot automatically completed the same rollback. Selected-kit promotion now has a [separate source contract](RECOVERY-RUNTIME-PROMOTION.md) and [native acceptance record](../deploy/e2e/release-recovery/RUNTIME-PROMOTION.md). Separate Arch/Debian trials prove owner completion after an interrupted launcher transition and automatic application rollback after promotion. Failed earlier trials remain recorded; these bounded results do not close P0.3. [Forward completion material v2](RECOVERY-FORWARD-COMPLETION.md) has [scoped Arch/Debian native acceptance](../deploy/e2e/release-recovery/FORWARD-COMPLETION.md) after the database-ready checkpoint with three retained candidate files absent. [Isolated database migration material v3](RECOVERY-ISOLATED-DATABASE.md) now keeps normal updates active while the candidate migrates a separate copy, independently verifies it and atomically publishes it. Its [scoped native Q/R acceptance](../deploy/e2e/release-recovery/ISOLATED-DATABASE.md) includes a genuine Alpha64/schema38 baseline: Arch automatically rolls back with initial DB work preserved; Debian completes the real 38→42 migration and survives loss of three retained candidate files. Separate final-source R evidence verifies all old rows in 55 tables and the publication records. Separate [native WAL interruption evidence](../deploy/e2e/release-recovery/NATIVE-WAL.md) records one physical noncommit write boundary on populated schema38 in Debian and Arch, followed by same-operation automatic rollback. WAL evidence alone does not establish a successful populated 38→42 domain conversion. Subsequent [native exchange acceptance](../deploy/e2e/release-recovery/NATIVE-DATABASE-EXCHANGE.md) on Arch U and Debian W proves that conversion in the exchanged pair and automatic inverse-exchange rollback before the publication receipt, retaining all 55 old tables and every cut-time row. Earlier inconclusive Debian U/V attempts remain preserved. The separate [Debian X two-fault acceptance](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md) then resets the VM at native rollback payload_restored after an actual exchange cut. The first boot invocation fails while systemd is starting; the existing watchdog retries and completes the same rollback, retaining all 99 cut-time rows. The failed invocation is preserved. This is eventual recovery, not uninterrupted service or power-loss durability. The subsequent [Arch Z acceptance](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md#z-arch-native-acceptance) passes the same two-fault boundary: two early boot failures are preserved, the existing timer completes the same rollback, and all 100 old rows remain unchanged. Z also records the baseline kernel package upgrade and different running kernel after reset; firewall/VPN readiness is not claimed. The earlier Y preparation-only failure is retained and is not counted as a native fault trial. This does not close the remaining fault/workload matrix. [WAL and populated SQL prerequisites](../deploy/e2e/release-recovery/WAL-FIXTURE.md) separately record controlled-writer and private-copy tests; they are not native acceptance. The earlier v2 trials do not establish this new boundary. The full checkpoint matrix, signed admission, incomplete-capture data independence, metadata transitions and cleanup remain open. |
| P0.4 | Partial ? shared mail TLS artifact, source reader, plan, publisher and recovery cleanup | [Contract](MAIL-CERTIFICATE-ARTIFACT.md): actual Alpha81 producer compatibility and [AY native renewal/boot](../deploy/e2e/release-recovery/MAIL-CONTRACT-AY.md). Shared cleanup adds component owner-edit checks; native interrupted cleanup, full DNS schema separation and all producer/restore transitions remain open. |
| P0.5 | Partial ? independent firewall native update/rollback on Debian and Arch; native mail continuity | [Firewall update/rollback](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md), [Arch forward/boot](../deploy/e2e/release-recovery/PLATFORM-UPDATE-AV.md), [mail renewal/boot](../deploy/e2e/release-recovery/MAIL-CONTRACT-AY.md). Mail renewal still invokes Agent code. Independent renewal, production UI/trust, failed-helper boot/console and the full workload/absence matrix remain open. |

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
that changed-runner Debian boundary, not P0.1–P0.3 in full. Browser-specific
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
