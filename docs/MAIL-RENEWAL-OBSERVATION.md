# Mail certificate publication: current configuration barrier

P0.4/P0.5, constitution invariants 1 (owner authority), 2 (evidence meaning),
and 4 (mutation preconditions). This closes a pre-publication observation gap;
it does not establish independent renewal or complete owner-safe recovery.

The committed mail TLS v1 plan is historical accepted intent. Previously the
host certificate preflight compared only its hostname, so a later native owner
edit could be overwritten by configuration convergence during renewal. Initial
issuance and queued renewal now compare current Postfix TLS fields and the
managed Dovecot/SNI source fragments with that plan before source staging.
Publication repeats the observation while holding the publication lock and the
existing durable operation's host lease. An unknown Dovecot version or unreadable
configuration cannot become agreement. Unknown results retain their typed cause.
The public refusal names owner review and retry without exposing native command
output or owner configuration values.

The same observation is used by post-publication verification. It runs only
Dovecot version observation, Postfix field reads and protected configuration file
reads. Native `postfix check`, map compilation and service commands are excluded
from this preflight; they remain in post-publication verification as before.
No schema, receipt, job identity, automatic retry or rollback protocol changes.
A refused publication creates no certificate stage and changes no selection;
the existing renewal path records failure and retains the pending source.

Limits remain explicit: this is a point-in-time proof, not exclusion of a root
operator editing after observation. It does not observe every Dovecot override,
compiled Postfix map entry or general service health. Existing post-publication
convergence and interrupted-operation recovery still reapply the accepted plan;
this patch does not certify those paths against later owner edits. Independent
renewal enrollment, native helper/runtime ownership, reload-only publication,
durable recovery and old-Agent compatibility remain open acceptance items.
Neither installed panels nor native owner workloads are modified by source tests.

Source validation: targeted drift/unknown/read-failure tests, full Agent race
suite (194.454 s), shared configuration race tests and vet pass. Native owner-edit
publication refusal is the next acceptance check; no native success is claimed here.

[Subsequent native AY acceptance](../deploy/e2e/release-recovery/MAIL-OBSERVATION-AY.md)
passed on the exact source: deliberate owner edit blocks actual publication before
staging, native listeners and pending source are preserved, and explicit fixture
owner resolution passes readback. This does not close the limits listed above.

[Fresh native AZ lifecycle acceptance](../deploy/e2e/release-recovery/MAIL-CONTRACT-AZ.md)
also passed initial publication, real renewal, orderly boot without installed
management and owner-selected-certificate replay protection on the same source.
Renewal still executes Agent code; independent helper and interruption gaps remain.

## Independent executor boundary found during implementation

The existing `agentServiceMutationManager` is not a mail-only library entrypoint.
`newServiceMutationManagerWithWriteFault` loads state and immediately calls
`reconcilePersistedActive`. Under the shared host/publication locks that path
cleans other journal stages and can recover firewall, DNS, panel certificates
and VPN work. Even an idle active pointer can trigger released DNS publication
recovery. `begin` calls `tryResolvePersistedOrphan`, which has the same broad
recovery responsibilities. A mail helper must not gain those actions merely by
reusing this constructor or renaming the Agent executable.

A future scoped executor must establish its scope under the host/publication
lease before any cleanup, deferred recovery or mutation admission, and recheck
fresh disk state at every later admission boundary. Unrelated pending/active or
unknown evidence is retained with an actionable refusal. Checking only before
acquiring the lease is insufficient. The existing supervisor also reexecutes
its own executable with a verified inherited host-lock descriptor; that behavior
must survive the separate entrypoint without opening general RPC/CLI modes.

This is a source-grounded boundary audit for the already open P0.5 executor item,
not an implemented helper or a new authorization. Enrollment, retained identity,
managed-hook migration, prior-version rollback compatibility and native fault
acceptance remain prerequisites to shipping it. No general manager behavior was
changed by this audit.

## Scoped unattended admission (2026-09-22)

P0.5 now routes actual queued renewal through a separate manager with an immutable
copy of the exact request, owner, mail hostname and payload qualifier. It does
not instantiate the general Agent singleton. Constructor, status reconciliation
and begin observe fresh canonical disk state under the same host/publication
locks before any admission. They never dispatch general recovery, clean journal
stages, start deferred host recovery or rewrite an interrupted operation. Begin
rechecks after reacquiring the lease; a prior idle observation is insufficient.

Active work (including the same interrupted renewal), pending propagation,
retained writer/journal stages, unreadable typed journals or a retained DNS switch
journal block this narrow entry. Historical records with another owner cannot be
taken over. The ordinary Agent retains its existing supported recovery paths.
Unknown evidence is preserved and the refusal names owner recovery before retry;
this is deliberately not a new independent interrupted-operation recovery claim.

Renewal retains the existing durable v1 writer, execution tracker, supervisor,
lease watchdog, publication receipt and failure semantics. It neither trims
other jobs' terminal history nor cleans abandoned shared writer stages. A
poisoned manager or still-active worker remains reachable across polling so a
new iteration cannot discard its held lease. Ordinary Agent admission shares the
same exclusion and reloads canonical disk state; this is not a second ledger.
No schema/version transition, installed migration, hook or native unit change.

Tests exercise retained foreign and same-operation active evidence, journal and
stage refusal, constructor-to-begin state change, exact immutable scope,
historical-owner mismatch, preservation beyond the normal history limit and
mutual exclusion/lost-write prevention with the ordinary Agent. Native lifecycle
acceptance for this source is recorded separately after execution. This manager
still lives inside the Agent executable. Separate runtime/enrollment, narrow
interrupted renewal recovery, reload-only convergence, current owner-change
protection after publication and old-version rollback compatibility remain open.

The final scoped-admission source passed the full Agent race suite (203.139 s).
Native results and remaining limits must be read separately; this unit-level
result does not certify an independent executor.

[BA native acceptance](../deploy/e2e/release-recovery/MAIL-CONTRACT-BA.md) passed
initial publication, actual scoped queued renewal, independent native boot
handshakes, exact persisted receipt and owner-selected-certificate preservation
on that source. Vet also passed. Independent recovery limits remain open.

## Reload-only renewal and selected-version recovery (2026-09-22)

P0.4/P0.5 owner authority and checkpoint recovery. Actual scoped renewal now
observes the accepted native fields/fragments, selected trusted host pair,
immutable customer SNI references, supported Dovecot dialect/parser and running
Postfix/Dovecot before staging. After publication it repeats those observations
and sends only `systemctl reload` to the two fixed units, observing again between
and after reloads. It cannot regenerate fallback material, compile a customer
SNI map, write native settings, run `postfix check`, or start a stopped service.
Public failure text excludes native command output while retaining typed causes.

The existing exact selected-receipt startup recovery uses this same reload-only
path. A later owner edit prevents convergence and remains intact with the active
intent and receipt. Once the owner explicitly resolves the disagreement,
supported recovery can reload and verify the same operation. A certificate
receipt alone is no longer permission for recovery to reapply the old mail plan.
No v1 ledger/receipt migration or new recovery identity is introduced.

Initial issuance still performs the separately accepted fallback-to-host native
configuration transition. If that initial transition is interrupted after
selection but before configuration agrees, legacy v1 evidence lacks a trusted
before-image for rewriting the current configuration. Recovery now retains that
uncertainty for explicit owner/configuration recovery rather than guessing.
A durable owner-safe initial-transition protocol remains open. This is a stricter
compatibility boundary, not a claim that every old interrupted issuance is
self-healing. An owner editing concurrently outside the native locks can still
change state between observations; no filesystem exclusion over root is claimed.

Unit tests cover stopped/unknown services, parser/version failure, owner drift
before/between/after reloads, command failure and cancellation, a read-only
preflight and redacted guidance. The native fixture additionally asserts config
bytes and modification times remain unchanged during real renewal. A separate
controlled post-publication fault/owner-resolution trial is prepared; native
results will be recorded only after it executes. Independent helper enrollment,
management-removal renewal, all effective overrides, power-loss and the complete
native platform/fault matrix remain open.


[BB native acceptance](../deploy/e2e/release-recovery/MAIL-CONTRACT-BB.md) now
passes reload-only renewal, independent native boot handshakes and the controlled
post-publication owner-edit/refusal/explicit-resolution sequence through actual
startup recovery. The same request and receipt survive; recovery changes no
resolved configuration bytes or modification time. This bounded controlled-fault
result does not establish power-loss recovery or an independent renewal runtime.


## Pending renewal acknowledgement (2026-09-22)

P0.3/P0.5 now treats deletion of pending renewal as a lifecycle transition.
A selected leaf matching the queued source is insufficient: publication may have
finished before activation or durable terminal completion. A different selected
hostname also retains the queue for owner review instead of silently discarding
it. Existing queued v1 bytes, receipt schema and canonical ledger are unchanged.

Acknowledgement holds the common host/ledger publication locks and certificate
publication lock while reading the latest selected validated pair/receipt,
canonical ledger, retained stages and operation journals. It requires that exact
receipt's successful published job, hostname, qualifier and leaf, no active or
pending foreign work, and no release transition. It never invokes the general
recovering constructor or creates a new job. Removal still compares the complete
pending identity and fsyncs its directory. A newer queue is preserved.

The selected receipt's original operation is used, not a newly derived identity
from the current build. Thus a verified older completed publication remains
acknowledgeable after a binary change. Missing/trimmed job evidence, unknown
selection, active/failed work or retained recovery evidence cannot be promoted
to completion. This narrows automatic cleanup for legacy incomplete evidence;
explicit supported owner recovery remains required. It does not establish
current daemon health, independent executor enrollment or all queue migration.

Targeted tests cover successful exact removal, retained active/failed/missing or
mismatched evidence, held host lock, unknown selected material, retained stages
and a newer queue. Native fresh-process same-leaf acknowledgement is prepared;
its results are recorded only after execution.


[BC native acceptance](../deploy/e2e/release-recovery/MAIL-CONTRACT-BC.md) now
verifies this acknowledgement boundary in a fresh polling process between actual
publication and generic startup recovery. Same-leaf unknown work and the ledger
remain byte-identical; only exact recovered completion clears the queue. Full
Agent race and vet passed. Independent renewal and power-loss acceptance remain
open.
