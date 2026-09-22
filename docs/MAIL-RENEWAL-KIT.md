# Independent mail renewal kit

P0.5 owner independence; P0.3/P0.4 versioned code and evidence compatibility.
This offline artifact is separate from installation authority. `make
mail-renewal-runtime` uses the reviewed compiler, clean build environment, helper
build tag and explicit build identity. Default build/dist and source/prebuilt
release paths now carry this artifact and prepare it before service downtime.
Installed Certbot hooks and native units remain unchanged until the separate
enrollment/migration operation is verified.

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

## Release payload and pre-downtime preparation

P0.3/P0.5, no evidence schema transition. Source builds, offline dist and prebuilt
staging preserve the exact helper/hook executable modes; neither helper is added
to the application bin resource. The complete outer release inventory covers all
five artifact files. Native candidate preparation verifies them under inherited
fd 9 before coordinator stop or application transaction creation. Historical
archives without this additive artifact remain readable. Preparation refusal
stops admission with its specific unconfirmed state and leaves existing native
renewal selection and workload configuration unchanged.

Shell behavior tests prove lock inheritance, ordering and refusal propagation.
Clean-toolchain tests cover the independent build tag, and local candidate
archive admission reconstructs every generated manifest/unit/hook from committed
templates using a real Go-produced vector. These checks do not establish signing,
installed native enrollment, legacy hook migration or whole-update rollback.

The full local `make dist` archive from `defa2acfb2443c6e7c4f72233b6d3f91026777bf`
was built with the reviewed Go compiler and frontend build, then read back through
complete archive inventory and committed-source validation (365 static files).
All five native mail files matched the v1 generated contract and exact archive
modes, and no helper was added to the application bin tree. See
[local artifact evidence](../deploy/e2e/release-recovery/MAIL-RELEASE-PAYLOAD.json).
It is explicitly unpublished and unsigned, with no installed update performed.

## Certificate writer compatibility with independent enrollment

P0.4/P0.5, unchanged v1 kit/ledger/receipt. The legacy hook producer bytes now
have one shared embedded contract. A pinned root-owned reader distinguishes
verified absence, exact legacy hook and complete independent hook/unit/runtime
material. Unsafe, unknown, altered or missing supporting files are errors, never
absence. The reader retains descriptors for a final revalidation after probes.

Certificate issuance preserves a recognized existing legacy or independent hook
without replacing its inode or normalizing owner metadata. Unknown owner content
stops that certificate action with guidance. Initial legacy publication remains
a compatibility path only and now uses no-replace rename, so a concurrent owner
file cannot be overwritten. This does not itself enroll independent renewal.

Independent renewal readiness additionally observes the actually loaded service
and timer: exact fragments, no loaded drop-ins or pending daemon reload, enabled
active timer and a usable oneshot service state. Disk evidence is revalidated
after these observations. The check never enables a timer, starts a service,
changes a hook or grants certificate/operation authority.

Race tests cover source/owner drift and concurrent absent-hook publication;
loaded-unit tests cover missing, overridden, disabled and failed states. Native
writer-preservation acceptance is recorded separately. Legacy enrollment,
initial managed-unit publication, rollback and owner removal remain open.

Native observation found an existing producer layout with root-owned mode-0700
Certbot hook parents carrying the `celikpanel` group. The hook observer accepts
protected root-owned directory parents without imposing a new group, pins their
original group/mode for revalidation and never normalizes them. Immutable runtime
kits and hook/unit files retain their exact root:root contract. Group-writable,
non-root-owned parents and later group changes are refused. This compatibility
rule applies only to the native file observer, not recovery kit enrollment.

[BE native hook preservation](../deploy/e2e/release-recovery/MAIL-HOOK-BE.md)
verifies these boundaries with the actual writer and loaded native schedule,
including the retained first refusal and metadata-compatible correction.

## Native enrollment before-image contract (2026-09-22)

P0.3/P0.5, invariants 1, 2 and 4. The new
`celikpanel-mail-renewal-transition/v1` describes exactly three native files,
the verified previous/target immutable kits and the observed timer preference.
It grants no execution permission. Initial enrollment accepts only verified
absence or the byte-exact historical Agent hook, with no pre-existing renewal
units. An upgrade requires the complete previous kit. A disabled or stopped
native timer remains disabled or stopped; unknown states cannot imply consent.

The private capture primitive writes
`celikpanel-mail-renewal-before-image/v1` under the inherited exclusive release
lock before any native mutation. It retains old file contents, inode and metadata
identities and protected parent identities. Read-only capture neither normalizes
metadata nor starts/stops services. The journal parent must already be admitted;
the primitive does not create it. Root-owned protected legacy hook directories
may retain their original group; files and immutable kits keep exact ownership.
Extra file attributes are refused rather than silently dropped during recovery.

The record is created through a pinned parent descriptor, synced, published with
no-replace rename and parent-synced. Repeated capture requires exact agreement
with the existing record, including inode identity. Owner changes, missing or
altered kit support, changed parents, conflicting journals and pending release
transactions preserve evidence and refuse capture. A process killed before
publication leaves its staging evidence; a new invocation can capture unchanged
native state. After publication, it reuses the identical durable record. This
re-entry is for capture only, not recovery after native file publication.

Component tests use a real inherited flock and actual SIGKILL at staged-file,
publication and parent-sync boundaries for absent, legacy and independent
fixtures. They also exercise same-byte owner inode replacement, directory/group
drift, native file/source changes, conflicting journals, extra attributes and
strict decoding. They do not prove power-loss durability or native enrollment.

No production dispatcher invokes this component yet. Accepted owner authority,
actual loaded-unit/schedule observation, after-image publication, interrupted
native transition/rollback and owner removal remain required before enrollment
can be enabled. Kit v1 and existing certificate/operation schemas are unchanged.

[Debian BE capture evidence](../deploy/e2e/release-recovery/MAIL-CAPTURE-BE.md)
records these process tests under the native kernel with unchanged running mail
workloads. It does not establish native enrollment or power-loss recovery.

## Native file transition and inverse exchange (2026-09-22)

P0.3/P0.5, invariants 1, 2 and 4. The private
`celikpanel-mail-renewal-files/v1` plan binds the before-image digest to exact new
inodes in two random staging directories under the native hook/unit parents.
The hook stage is a directory, so an incomplete executable cannot become an
additional top-level Certbot deploy hook. Files and both staging directories are
synced before the inode plan is published and parent-synced. A killed preparation
leaves orphan stages intact; the next attempt never adopts an unrecorded inode.

Only changed native files participate. Existing files are atomically exchanged
with the exact staged inode; verified absence uses no-replace rename. The old
inode remains available for inverse exchange. Each step rechecks the accepted
capture, immutable kits, both sides' content/metadata, fixed parent identities,
stage inventory and inherited release lock. Foreign contents, attributes, hard
links, same-byte replacements and missing supporting evidence stop this operation.
No existing parent metadata is normalized and no retained evidence is deleted.

An interrupted forward transition can continue the same exact plan or be
compensated. Rollback first persists a bound intent; once visible, forward action
is refused. Inverse exchange/rename restores original inodes or original absence.
Only rename-induced ctime differences on the recorded inode pairs are tolerated;
content, owner, mode, inode, size and mtime remain exact. Both directories are
synced again even when a killed predecessor already performed the move. Immutable
forward/rollback receipts record file outcomes only and are verified against
current file state; a historical receipt cannot establish current readiness.

The component preserves unchanged timer files and does not reload systemd,
operate a service/timer, enroll production renewal or complete an application
update. Production dispatch still needs accepted owner authority, host/renewal
exclusion, actual schedule observation/transition, rollback integration and
native acceptance. No new installed-panel update path is exposed. Existing kit,
certificate and ledger schemas remain unchanged; the two transition records are
additive and private.

Tests include forward/inverse inode preservation, explicit owner drift/refusal,
all 76 process-kill boundaries across absent/legacy/independent layouts, and a
second process kill during rollback of an interrupted forward operation. These
component results do not establish reboot, power loss, complete native enrollment
or whole-update rollback. Native execution evidence is recorded separately.

[Debian BE native file compensation](../deploy/e2e/release-recovery/MAIL-FILES-BE.md)
now verifies actual hook/service exchange, a second kill during inverse exchange,
original inode restoration and unchanged trusted mail workloads. The initial
test-driver refusal is retained. Loaded-unit activation and whole-update acceptance
remain open.

## Shared native schedule observation (2026-09-22)

The immutable-kit module now owns the six-property native systemd observation
parser and readiness policy used by the Agent. A complete disk kit is not proof
of a loaded, enabled schedule. Missing/duplicate/unknown properties, changed
fragment paths, overrides and pending reload remain unverified. This refactor
does not change the existing public readiness policy or start a native action.

The separate transition observation requires the current oneshot invocation to
be idle and preserves all four supported enabled/disabled and active/inactive
timer combinations. A running service is an explicit wait, not absence. An
unverified timer is not hidden by that wait. Bootstrap absence requires both
native units positively not found with empty fragment/override/enablement data;
a failed query, failed service, masked unit or substituted fragment is not absent.

These observations are not ownership, accepted intent, durable evidence or a
mutation barrier. Callers must source them from the actual local service manager,
retain native file proofs, establish host/renewal exclusion and re-observe at the
mutation boundary. Production schedule activation and rollback remain open.
