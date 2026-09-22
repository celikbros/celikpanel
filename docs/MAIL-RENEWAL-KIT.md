# Independent mail renewal kit

P0.5 owner independence; P0.3/P0.4 versioned code and evidence compatibility.
This offline artifact is separate from installation authority. `make
mail-renewal-runtime` uses the reviewed compiler, clean build environment, helper
build tag and explicit build identity. Default release packaging and installed
Certbot hooks are unchanged until enrollment/migration acceptance is implemented.

The v1 manifest binds the exact helper, service, timer and Certbot deploy hook.
Its immutable generation is derived from the helper and original template bytes;
rendered service/hook paths point to that exact generation under
`/usr/libexec/celikpanel/mail-renewal/<generation>/renew`. The manifest declares
ledger v1, accepted plan v1 and host receipt v1. Unknown versions, substitutions,
noncanonical JSON, added files and unsupported unit templates are refused.
A digest is identity, not a signing trust root; production admission still needs
the authorized release/enrollment boundary.

The hook only queues the selected managed source. A native oneshot processes
pending work, with a 180-second invocation bound and whole-cgroup termination.
The timer checks after boot and five minutes after each invocation, adding jitter.
There is no Panel, licensing or management Agent service dependency, and the unit
does not start native mail services. Filesystem protection permits certificate
publication only inside the managed host directory under `/etc`; native Postfix
and Dovecot configuration remains read-only. Retained group and durable evidence
are still necessary. An unknown/interrupted operation remains pending instead of
being treated as a new job or silently cleared by periodic polling.

The offline builder checks real parents, bounded single-link regular files, exact
inventory and modes. It verifies a staged artifact before an atomic publication;
a changed recognized build is exchanged and its predecessor retained. Unknown
owner content is preserved. Failed stages are retained rather than recursively
removed. No installed unit or hook is edited or enabled by this build command.

Contract and builder race tests and vet pass. Native service sandbox, hook,
scheduled boot renewal and production enrollment/rollback evidence are separate
acceptance items; artifact tests alone do not establish them. Independent recovery
of interrupted renewal and the full P0.5 workload matrix remain open.

[BE native scheduling evidence](../deploy/e2e/release-recovery/MAIL-KIT-BE.md)
now proves the actual hook, service sandbox, timer-triggered new-leaf publication
and automatic new-leaf deployment after reboot, with installed management absent.
Production enrollment/rollback and independent interruption recovery remain open.

## Selected-certificate interruption recovery (local implementation)

P0.3/P0.5, constitutional invariants 1, 2 and 4. No schema is rewritten:
the helper consumes the existing v1 pending source, certificate receipt and
canonical service mutation ledger. No new job or acquisition identity is made.

After a failed completion acknowledgement, a fresh independent helper may
complete only the exact active commit-intent job whose receipt is already
selected and whose leaf matches the queued source. It retains the common host,
ledger-publication and certificate-publication exclusion. A live worker,
unreadable worker identity, release transaction, retained foreign journal or
write stage, changed selection or queued source prevents recovery. Known
terminal failure is never promoted. The same poisoned process cannot abandon
its manager and acquire a new lease.

Before recording recovery it observes the accepted mail plan, actual native
configuration and running Postfix/Dovecot services. It repeats identity checks
under exclusion before using the shared durable recovery tracker to reload the
two services. It neither starts a stopped service nor rewrites configuration,
promotes a staged certificate or cleans other generations. The exact successful
terminal receipt is required before queue acknowledgement. Another interruption
retains the same operation and selected material for later reconciliation.

Unit refusal tests include owner changes during preflight and preserve ledger,
queue and foreign evidence. Native acceptance must additionally kill the actual
process after durable selection and run a separate helper without installed
management binaries. Such evidence is recorded separately; these unit tests do
not close the native acceptance item. Pre-selection interruptions, bounded
recovery retry policy, enrollment/migration/removal and power-loss acceptance
remain open.

## Bounded selected-operation retry

The existing v1 job `attempt` is now reserved in the same durable recovery-intent
write before an independent selected-certificate recovery may reload services.
The recorded initial execution and up to two automatic recovery attempts exhaust
the limit of three. Kills, reboot and a different helper build do not reset that
same selected operation's counter. Historical attempts not recorded by older
writers are not inferred or backfilled. No ledger version migration is made.

At exhaustion the helper makes no further native change and emits the recorded
request ID, preserved pending state, and a concrete root-owner continuation:
`<installed immutable helper> --retry-selected <recorded-operation-id>`.
The owner first resolves native Postfix/Dovecot problems. Each explicit command
admits only one further attempt for that exact selected receipt and pending leaf;
it does not clear or reset the counter, start a service, replace selection, grant
a broad recovery capability or reopen a terminal failure. Overflow and another
operation identity are refused. Read-only prerequisites may be checked repeatedly
without spending a mutation attempt; their failure preserves the original job.

This budget governs the independent helper's already-selected recovery only.
Pre-selection failed-issuance retry and ordinary Agent recovery policy remain
separate open work. Production enrollment must retain the selected helper's
policy and its state through migration/rollback; an older helper is not evidence
of this new retry guarantee. Native failure/reboot/exhaustion/explicit-owner
acceptance is recorded separately from the component tests.

[Debian BE budget acceptance](../deploy/e2e/release-recovery/MAIL-BUDGET-BE.md)
now verifies actual failed reload, automatic retry after reboot, refusal at the
third recorded execution, unchanged evidence for another request, and one
explicit owner continuation to successful attempt four. This closes that narrow
native budget item; the pre-selection and enrollment limitations above remain.

## Immutable runtime preparation

P0.3/P0.5, invariants 1, 2 and 4. The candidate recovery CLI accepts
`prepare-mail-renewal-runtime --source /absolute/mail-renewal-runtime --transaction-fd 9`
only as root under the inherited exclusive native release lock, before a release
transaction or service downtime. The fixed v1 kit is validated using pinned,
root-owned files, exact inventory and modes, bounded reads and digest identity.
Outer signed release admission remains the caller's responsibility.

Mail and firewall kits share the immutable publication writer. Each generation
is staged, validated, synced and published without replacing an existing path.
Repeated preparation preserves the existing generation's inode. Changed owner
content, source drift, active transactions and path replacement are refused.
A killed process leaves its partial stage as evidence; a new process prepares a
fresh stage or repeats the durability check of the already-published generation.
Older complete generations are retained. No schema migration occurs.

This command only prepares an artifact. It does not publish native units or the
Certbot hook, enable a timer, stop a service or install a panel update. Production
enrollment, legacy hook migration, snapshot/rollback and removal remain open.
Component tests cover inherited lock rejection, owner changes, restricted umask,
retained predecessors and actual SIGKILL at five publication boundaries. Native
CLI acceptance is recorded separately; these tests alone do not close P0.5.

[Native Debian BE preparation evidence](../deploy/e2e/release-recovery/MAIL-PREPARATION-BE.md)
now verifies the actual candidate CLI and process-kill boundaries with running
mail workloads and management absent. Enrollment and power-loss remain open.
