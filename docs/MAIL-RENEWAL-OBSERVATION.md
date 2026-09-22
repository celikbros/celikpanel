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
