# Resilient operation: architecture contract and implementation audit

*September 14, 2026 Â· [TÃ¼rkÃ§e](RESILIENCE-CONTRACT.tr.md) Â· D-025*

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

The P0 identifiers above are the tracked work items. Evidence is updated through September 24:

| Item | Recorded status | Completion evidence |
|---|---|---|
| P0.1 | Partial â€” native old-release restoration passed at one checkpoint on Arch and Debian | [Unit-transition acceptance](../deploy/e2e/release-recovery/UNIT-TRANSITION.md) records real restoration and running old binaries after SIGKILL. The broader fault/workload matrix, consistent database semantics and signed candidate admission remain open. |
| P0.2 | Partial â€” typed access, Agent-independent startup observation and native recovery entrypoint implemented | [Access/observation acceptance](RECOVERY-ACCESS.md) and [independent runtime](RECOVERY-RUNTIME.md). Root/sudo status and recovery do not require Panel/Agent startup or licensing. [Native AJ](../deploy/e2e/release-recovery/BOUND-WORKER.md) proves a real worker kill, reboot during automatic recovery, verified rollback and matching CLI/authenticated HTTP/browser terminal results in a disposable Debian schema42-to42 fixture. [Native AK](../deploy/e2e/release-recovery/BOOT-WAIT.md) further proves real `starting` guidance in the root CLI and same-request timer retry to rollback. Other waits, prior-known-failure preservation through a native wait, HTTP/browser wait access, UI update-start admission, production signing and the full fault/access matrix remain open. |
| P0.3 | Partial â€” independent code/data, atomic publication and selected native recovery SIGKILL/reboot boundaries passed | [Independent runtime](../deploy/e2e/release-recovery/INDEPENDENT-RUNTIME.md) and [material acceptance](../deploy/e2e/release-recovery/RECOVERY-MATERIAL.md): three retained candidate files were unavailable; Arch payload_restored SIGKILL and Debian runtime_verified reboot automatically completed the same rollback. Selected-kit promotion now has a [separate source contract](RECOVERY-RUNTIME-PROMOTION.md) and [native acceptance record](../deploy/e2e/release-recovery/RUNTIME-PROMOTION.md). Separate Arch/Debian trials prove owner completion after an interrupted launcher transition and automatic application rollback after promotion. Failed earlier trials remain recorded; these bounded results do not close P0.3. [Forward completion material v2](RECOVERY-FORWARD-COMPLETION.md) has [scoped Arch/Debian native acceptance](../deploy/e2e/release-recovery/FORWARD-COMPLETION.md) after the database-ready checkpoint with three retained candidate files absent. [Isolated database migration material v3](RECOVERY-ISOLATED-DATABASE.md) now keeps normal updates active while the candidate migrates a separate copy, independently verifies it and atomically publishes it. Its [scoped native Q/R acceptance](../deploy/e2e/release-recovery/ISOLATED-DATABASE.md) includes a genuine Alpha64/schema38 baseline: Arch automatically rolls back with initial DB work preserved; Debian completes the real 38â†’42 migration and survives loss of three retained candidate files. Separate final-source R evidence verifies all old rows in 55 tables and the publication records. Separate [native WAL interruption evidence](../deploy/e2e/release-recovery/NATIVE-WAL.md) records one physical noncommit write boundary on populated schema38 in Debian and Arch, followed by same-operation automatic rollback. WAL evidence alone does not establish a successful populated 38â†’42 domain conversion. Subsequent [native exchange acceptance](../deploy/e2e/release-recovery/NATIVE-DATABASE-EXCHANGE.md) on Arch U and Debian W proves that conversion in the exchanged pair and automatic inverse-exchange rollback before the publication receipt, retaining all 55 old tables and every cut-time row. Earlier inconclusive Debian U/V attempts remain preserved. The separate [Debian X two-fault acceptance](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md) then resets the VM at native rollback payload_restored after an actual exchange cut. The first boot invocation fails while systemd is starting; the existing watchdog retries and completes the same rollback, retaining all 99 cut-time rows. The failed invocation is preserved. This is eventual recovery, not uninterrupted service or power-loss durability. The subsequent [Arch Z acceptance](../deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md#z-arch-native-acceptance) passes the same two-fault boundary: two early boot failures are preserved, the existing timer completes the same rollback, and all 100 old rows remain unchanged. Z also records the baseline kernel package upgrade and different running kernel after reset; firewall/VPN readiness is not claimed. The earlier Y preparation-only failure is retained and is not counted as a native fault trial. This does not close the remaining fault/workload matrix. [WAL and populated SQL prerequisites](../deploy/e2e/release-recovery/WAL-FIXTURE.md) separately record controlled-writer and private-copy tests; they are not native acceptance. The earlier v2 trials do not establish this new boundary. The full checkpoint matrix, signed admission, incomplete-capture data independence, metadata transitions and cleanup remain open. |
| P0.4 | Partial - shared mail TLS and separated DNS acquisition/publication contracts; independent DNS observation only | [Mail contract](MAIL-CERTIFICATE-ARTIFACT.md) includes Alpha81 producer compatibility and [AY native renewal/boot](../deploy/e2e/release-recovery/MAIL-CONTRACT-AY.md). [DNS contract](DNS-ENGINE-ARTIFACT.md) includes historical Alpha81 v1 compatibility, self-contained v2 documents, shared source/target checks and a root-only journal/ledger/native-unit observer. Neither observer nor component tests prove an Agent-independent DNS inverse. Native interrupted cleanup, secure BIND/PowerDNS recovery execution, complete producer/restore transitions and the DNS fault matrix remain open. |
| P0.5 | Partial — independent firewall on Debian/Arch; independent mail helper and selected native recovery on Debian | [Firewall update/rollback](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md), [Arch forward/boot](../deploy/e2e/release-recovery/PLATFORM-UPDATE-AV.md), [mail helper and interruption contracts](MAIL-RENEWAL-KIT.md), [bounded failed renewal](../deploy/e2e/release-recovery/MAIL-FAILED-BUDGET-BE.md). Management-absent mail renewal is proven in the guarded Debian fixture; [pending enrollment recovery without Agent/Panel/declaration](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-MANAGEMENT-ABSENT-BE.json) now passes Arch forward/inverse and Debian forward reboot trials. New-plan enrollment admission is implemented with verified source/native review; complete native wizard acceptance, supported old application rollback compatibility, Arch mail and the full workload/absence matrix remain open. |

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
