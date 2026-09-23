# Durable native mail enrollment reservation

P0.2/P0.3/P0.5; constitution: owner continuity, exact operation ownership,
unknown is not absence, and verified terminal results. This is the private
reservation boundary for the existing composite native enrollment executor.
The explicit root-owner worker, authenticated setup admission and automatic
boot consumer share that boundary. The current implementation and acceptance
limits are stated next; this does not complete the resilience plan.

## Current implementation boundary — September 23

New mail setup reviews now read the released helper and native before-state.
Verified absent/recognized legacy renewal produces an explicit `mail_enrollment`
step after the mail certificate and before final verification, bound to the
reviewed kit generation. Existing independent schedules are preserved, including
owner-disabled timers and their current kit. Older accepted plans are unchanged.
This is source enablement, not a published release or full native wizard/update
acceptance. The detailed historical slices below retain their original scope.

## One common durable owner

Enrollment uses the existing canonical `service-mutations.json` v1 ledger and
writer. There is no second update marker that can unnecessarily block Panel or
Agent startup. A new reserved kind, `mail_renewal_enrollment`, binds the operation,
owner and SHA-256 of the canonical enrollment scope (capture, file plan, target
kit). Target is fixed to `mail-renewal`. The phase protocol is
`commit/mail-renewal-enrollment/v1/<state>/<request>/<scope-sha256>` and the
payload qualifier is `mail-renewal-enrollment/v1:sha256:<scope-sha256>`.

Active states `forward` and `rollback` use the existing `orphaned` status and
active pointer. They are reservations, not RPC workers; worker identity is empty,
attempt remains one, and lease/deadline equal the initial admission timestamp.
An elapsed timestamp never grants new authority. Generic Agent startup, orphan
handling, RPC begin/resume, heartbeat, cancellation, finish and status polling
cannot clear this kind. Startup remains healthy and read-only status is available.
The normal mutation idle probe refuses while the active pointer exists.

Only exact `forward -> published`, `forward -> rollback -> restored` transitions
release the reservation. Published is succeeded; restored is failed with the
specific `mail_enrollment_restored` outcome, describing verified compensation,
not a missing native resource or a guessed failed worker. Terminal records cannot
be reopened, reused or implicitly rolled back. A later independent inverse needs
a separately accepted operation and current ownership evidence.

## Publication and authority

The private writer requires existing release fd9 then host fd8, then acquires
the existing ledger publication lock. It reads an established private ledger;
it does not initialize owner/group identity or start the general Agent recovery
constructor. Exact owner intent and authenticated source must be supplied by the
outer dispatcher on every call. The root-owner CLI and reviewed setup RPC can
invoke this boundary. Renewal-hook and polling admission remain disabled. A
digest or callback alone is not serialized owner authority.

A pinned current Agent must explicitly declare
`mail_enrollment_policy: retain-enrollment-ledger-v1`. This optional field extends
the native Agent contract without changing historical canonical bytes when it is
absent. Absent means no enrollment retention capability. The current-source build
producer emits the field. Earlier strict readers may reject the extension; they
must not be assumed compatible or given fabricated declarations. This claim
covers ledger retention only, not a complete enrollment/recovery dispatcher.

Admission durably publishes the reservation before native work. Every write
revalidates source, accepted intent, both caller locks, publication lock, parent
identity and the exact old ledger inode/content. Forward and inverse terminal
publication additionally require fresh native result proof. Unknown results
retain the pointer. Existing complete canonical temporary stages are handled by
the common writer cleanup; ambiguous stages remain refusal. Unrelated history
is not pruned to make room. A visible reservation after uncertain directory sync
is synced again on explicit same-operation retry before returning success.

The updater's normal path now performs the existing read-only mutation idle
probe before snapshot staging or quiesce marker publication, after the native
lock directory preflight. An active reservation or unknown ledger leaves the
coordinators available. Existing final idle checks under exclusion remain, as do
the separately strict pre-ledger bootstrap paths. No installed update is started
by this change; users still initiate updates through the panel.

## Evidence and remaining acceptance

Shared contract tests cover canonical historical v1 bytes, owner/scope/status
conflicts, early pointer release, invalid worker adoption, monotonic inverse,
terminal identity reuse, and old Agent capability absence. Agent tests reopen
three managers against an expired reservation and exercise generic RPC attempts
and updater idle checks without changing a byte of the reserved ledger.

Twelve real subprocess SIGKILL cases cover before rename, after rename and after
directory sync for admission, inverse intent, forward completion and restored
completion. Fresh-process retries preserve operation identity. Further cases
refuse missing inherited locks, missing/changed intent, historical Agent support,
late source/ledger/publication-lock replacement, an occupied publication lock,
and unknown native result. Native result callbacks in these writer tests are
fixtures, not evidence of a complete native enrollment transaction.

The private composite executor now requires the same durable reservation at
every boundary, including native commands. It pins the ledger inode/metadata and
parent identity, verifies the established numeric owner, and binds the exact
operation, owner, scope and direction. Six additional real subprocess SIGKILL
cases resume forward and inverse execution under that reservation. Missing or
replaced ledgers, changed owners and prematurely closed reservations refuse.
An inverse accepted immediately after admission can finish without performing
the previously unstarted forward native work.

Terminal acknowledgement has a separate read-only observer. A terminal ledger
retry may re-observe exact files, links and loaded native state; it cannot
re-enter native mutation. Later owner changes remain refusal and are never
repaired merely because a historical success exists. The terminal observer
and native executor share final verification rather than separate definitions
of completion. Scoped recoveryruntime race tests and vet pass; production
dispatch remains disabled.

[The reserved Arch native composition](../deploy/e2e/release-recovery/MAIL-RESERVED-ENROLLMENT-BE.md)
now exercises two actual native cuts with the shared reservation consumer. Its
fixture producer and collector limitation are explicitly recorded. Joining
this reservation to authenticated production owner intent, dispatch/boot recovery,
initial private identity provisioning and the full native update/rollback and
workload matrix remains open. Component success does not close those P0 items.

## Joined writer and native execution (2026-09-23)

`executePreparedMailEnrollment` now sequences the actual common writer and
prepared native executor: admit/sync the exact direction, resume native work,
re-observe the result, then publish its terminal ledger state. It supplies the
native proof itself rather than accepting a cached success callback. Terminal
retry only verifies; opposite-direction retry and identity reuse refuse. Native
or observation errors preserve the reservation. Twelve additional subprocess
SIGKILL cases cut both directions before/after native work and at terminal ledger
publication, including directory sync. Scoped race tests and vet pass.

The shared runtime adapter opens the canonical existing v1 scope and file plan,
fixes native resource paths and rechecks owner/source authority and locks. The
native command adapter uses only trusted `/usr/bin/systemctl`, the two fixed unit
observations, daemon-reload, and start/stop of the renewal timer. It has a clean
environment, ten-second deadline, parent-death SIGKILL and bounded classified
output. It does not borrow a generic expiring RPC lease or register another job.

These functions are compiled but not exposed as new RPC/CLI admission. The
recorded-operation CLI below can resume only an existing accepted reservation.
Initial owner intent provisioning, authenticated setup dispatch and boot dispatch
remain open. The guarded native fixture now supports joining these actual components;
its result must be recorded separately from the fixture-native process tests.

### Combined native acceptance

[Joined Arch evidence](../deploy/e2e/release-recovery/MAIL-JOINED-ENROLLMENT-BE.json)
uses source `055851b8b2e9e0fbc8ac6fecabe7fca0ac814eed` with the actual common
reservation writer, prepared executor and production systemctl adapter. Native
processes are killed after timer start and after inverse timer stop. Fresh-process
inverse finishes the same operation and publishes `restored`. A final exact retry
performs zero native mutations and leaves the terminal ledger byte-for-byte
unchanged. The original shared-directory inventory is durably captured before
work; final native files/units are absent and retained inverse evidence remains.

The trial passes on its first run. Its owner intent and initial empty ledger are
explicit test admission, with current-source Agent compatibility material retained
in the protected lab directory; the normal installed management paths are absent.
This closes the combined writer/executor/native-adapter trial, not production UI
admission, boot dispatcher, old-release compatibility or real mail workload tests.

## Recorded-operation continuation (2026-09-23)

P0.2/P0.3/P0.5: `OpenRecordedMailEnrollment` reconstructs the scope exclusively
from the existing canonical capture and file plan and compares its digest with
one exact accepted common-ledger request. It never searches for the newest job,
creates a missing record, or treats a terminal receipt as current native success.
Forward/rollback direction comes from the same reservation. A removed request,
changed owner/scope/source or newer rollback decision invalidates the old opener.
The durable writer and native executor use the same authority check. Existing
v1 schemas and transitions are unchanged; no additional intent marker is added.

The separately built helper now accepts `--resume-enrollment <recorded-id>`.
It is a narrow owner/dispatcher entry requiring already-held release fd9 and
host fd8, the established numeric service group, the canonical common ledger,
root-owned journals under `/var/lib/celikpanel-mail-renewal/enrollment`, and the
verified current Agent declaration. It proves its own executing bytes match the
exact declared mail runtime. It cannot initialize any missing identity/evidence,
select another kit, change direction, start an update, or become a general Agent.
It invokes the existing writer/executor and terminal proof. Error guidance keeps
unknown results distinct and excludes raw private output. A terminal retry only
observes; it cannot restart an owner-stopped timer.

This is **not automatic boot recovery or authenticated setup admission**. Those
callers must still provision the accepted journal/identity, provide exclusion,
and dispatch this fixed consumer. Retained Agent compatibility files are currently
required, although the Agent daemon need not be running. This does not certify
recovery after deleting management files or enrollment after reboot with missing
volatile locks. The normal independent certificate-renewal path is unchanged.

Validation: shared-ledger selector, recorded preparation/refusal and full
forward/inverse composition tests pass with the race detector; Agent enrollment
and CLI scope/guidance tests and vet pass. Real management/helper builds pass;
the actual independent helper refuses missing inherited locks, malformed IDs,
direction overrides and update-worker arguments. These are component/process and
build checks, not a new native reboot or automatic-dispatch acceptance result.


## Explicit owner handoff and initial admission (2026-09-23)

P0.2/P0.3/P0.5, same v1 schemas and terminal transitions. The independently
built helper now implements root-owner initial admission and detached execution:

- `--start-enrollment <request-id> <owner-id> <reviewed-kit-digest>` accepts an
  exact root-authorized tuple. Request and owner are canonical 32-hex IDs; the
  64-hex kit must match the current verified Agent declaration and helper bytes.
- `--continue-enrollment <recorded-id>` cannot admit a missing request, choose a
  direction, change owner, or reprepare its capture. The existing ledger decides
  forward/inverse execution. A terminal retry only re-observes native proof.
- Both dispatch the fixed installed helper into
  `celikpanel-mail-enrollment-<request-id>.service`. The deterministic unit name
  prevents concurrent instances of that request. A returned handoff is only
  systemd acceptance, not admission, successful enrollment or native readiness.
  A lost handoff response stays unconfirmed. No polling path starts this worker.
- The worker takes the existing release lock, then the existing host lock. It
  re-executes its own pinned image with the correct inherited descriptors and a
  clean environment. No general Agent is started. systemd owns the process group
  independently of the invoking process; runtime and shutdown bounds apply.
  There is no automatic restart loop or application-update recovery dependency.

Initial admission requires the established numeric service group, canonical
common ledger, publication lock, compatible source and prepared installed kit.
It creates only the fixed private enrollment journal directory, refusing existing
unsafe metadata rather than normalizing it. Preparation records the native
before-image and file plan. The existing common writer durably reserves the
scope before any native publication or timer start. An interrupted preparation
can be retried with the same reviewed tuple; once a record exists, even a repeated
start selects the recorded opener. Reused, malformed or differently owned
requests refuse. Unknown native results retain the active reservation.

An owner inspects `journalctl -u celikpanel-mail-enrollment-<request-id>.service`
and the common request result. A failed worker can be explicitly continued after
the prerequisite is resolved; if it never admitted the request, only the original
reviewed start tuple can admit it. Existing evidence is not cleared. A busy lock
is reported as a wait, and a timeout remains an unknown result. These commands
are an owner entry for this source implementation, not instructions to update
Frankfurt/Boston or to execute a helper flag on a historical management Agent.

Validation: race tests cover exact initial/recorded admission, owner/target
conflicts, terminal/inverse preservation, competing work, real subprocess fd8/fd9
handoff, lock contention, deadline termination and missing runtime refusal.
Journal tests preserve inode/content and reject owner mode/symlink changes.
CLI scope/guidance, privileged-command guard and vet pass. Actual management,
helper and recovery builds pass; six actual helper entry/refusal probes pass.
A guarded native read was refused before SSH because the registered lab QEMU was
not running. **No new native systemd dispatch or reboot acceptance is claimed.**

Authenticated wizard admission/status, accepted-plan binding in Panel/Agent RPC,
boot dispatch, volatile lock/identity restoration, management-file absence and
full native lifecycle/workload acceptance remain open. No existing accepted setup
plan is retroactively changed; no new setup capability is advertised. This source
change was not installed on an owner's panel.


## Accepted setup binding and read-only observation (2026-09-23)

P0.2/P0.3/P0.5, constitution invariants 1, 2 and 3. The authenticated local
Agent IPC now separates `StartMailEnrollmentV1` from `MailEnrollmentStatusV1`.
Start accepts only the reviewed request/owner/kit tuple and the paired build.
It verifies the running Agent bytes against the protected release declaration,
the complete installed helper bundle against that declaration, and any existing
request against the immutable scope before handing off to the fixed independent
systemd worker. It cannot accept an executable path, shell command, direction or
arbitrary unit. The existing root CLI and IPC share the same bounded launcher.
A returned handoff means acceptance by systemd, not completed enrollment. A lost
reply stays unknown; private command/source diagnostics are not returned to IPC.

The setup execution consumer derives its tuple from the immutable reviewed plan
and deterministic child IDs, including the exact kit in the existing step
qualifier. Before dispatch it checks licensing, paired build and platform
capability, then persists `enrollment_dispatch_attempted` in the existing
execution JSON. That optional field is omitted for old steps; the plan, common
ledger and immutable enrollment records keep their existing v1 schemas. Polling
or reloading that execution cannot clear the attempt fence or dispatch again,
even if the Agent has not yet recorded admission. A database failure prevents
handoff. A historical published result can complete the execution step; present
service health still requires separate final verification. A verified restored
result remains a known failure even when unrelated generic Agent status fails.

The shared descriptor reader now reconstructs the same immutable scope for both
the locked executor and read-only observation. Observation binds request, owner,
kit, capture and file-plan digests, validates the whole common ledger, and refuses
concurrent changes rather than combining two different observations. It does not
need a current Agent contract, installed runtime, held mutation locks, systemd
or a license. It never creates directories, edits evidence, extends leases or
starts workers. `not_recorded` means only absence from a verified existing ledger;
a missing/unreadable ledger is unknown, and neither result grants retry authority.
The IPC status method still requires the ordinary authenticated Agent connection;
this shared reader alone is not an Agent-independent HTTP recovery endpoint.

**Historical gate for this slice (superseded by the new-plan review below):** no new capability is advertised and the plan
builder does not offer the new enrollment step. Existing accepted plans are not
rewritten. Native dispatch/reboot acceptance, explicit UI continuation for an
unconfirmed handoff, boot dispatch, volatile identity/lock restoration and the
full management-absence/workload matrix remain open. This backend consumer is
not a claim that independent enrollment is now available in the public wizard.
No installed owner panel was changed and no release was installed.

Validation covers exact plan/owner/kit binding, a real database reload after a
lost handoff, repeated observation without redispatch, licensing/storage refusal,
historical terminal results without management/runtime files, changed and mixed
immutable evidence, source/bundle replacement, bounded IPC policy and the shared
build gate. This is component/process and build evidence, not new native systemd
or reboot acceptance. See the retained local build/test metadata for this source.


### Native owner handoff and transient observation (2026-09-23)

[Arch BE evidence](../deploy/e2e/release-recovery/MAIL-OWNER-HANDOFF-BE.json)
uses actual helper source `ef53be5b7c3b7c93ccf81cb8dceffc5afd9da579` and the
fixed systemd worker, without starting either management daemon. Initial root
ledger and volatile lock identities were explicitly prepared by the fixture;
this does not establish production initialization or reboot dispatch.

The first start admitted one request and installed, enabled and started the
native timer. It did **not** finish: the timer immediately invoked its oneshot,
and final observation encountered the positively busy service. The service
finished successfully 37 milliseconds after the worker reported uncertainty.
The active reservation and all attempt records remained intact. Supported owner
continuation of that exact request then published its verified result. After
an explicit owner stop, terminal continuation preserved the stopped timer and
left ledger bytes unchanged. The initial unsuccessful trial remains evidence.

The native observation adapter now waits at most five seconds, reading only the
fixed service properties every 100 milliseconds while the known loaded oneshot
is active/activating/deactivating. It neither repeats start/stop/reload nor changes
admission or attempt records. Busy exhaustion remains busy; failed, overridden,
stale or unreadable evidence is not retried as healthy. Cancellation terminates
the wait. Owner guidance names a still-running invocation. Component regression
checks cover these cases. A subsequent source-bound native trial at
`2fc9a5902ba5dcd2e5f89f9e635007b67635d2ed` completed fresh enrollment on its
first start, including a successful detached worker exit and active/enabled
timer. The fixture explicitly retained and removed its known stopped old unit
files before the trial, preserved old ledger history, and installed the exact
new helper and compatible Agent bytes; this is test preparation, not production
migration or recovery. Existing v1 ledger and enrollment receipt schemas do not change.

This advances P0.2 truthful observation and P0.5 enrollment only. Automatic boot
continuation, production wizard admission, actual Arch mail workloads, previous
application rollback compatibility and the complete acceptance matrix remain open.


### Reboot observation and recorded volatile runtime (2026-09-23)

[Arch completed-enrollment boot evidence](../deploy/e2e/release-recovery/MAIL-ENROLLED-BOOT-BE.json)
proves that source `2fc9a5902ba5dcd2e5f89f9e635007b67635d2ed` leaves its
native timer enabled across a guarded QEMU reset, with Agent, Panel and Agent
compatibility-declaration files absent. The new boot ran the no-pending-work
helper successfully without recreating `/run/celikpanel`; native files, immutable
receipts and the common ledger stayed byte-identical. The first SSH read was
unavailable during boot, followed by a successful guarded read. This is completed
schedule boot evidence, not interrupted enrollment recovery or real certificate
renewal in that Arch fixture.

The independent enrollment worker now supports one additional explicit owner
continuation boundary: if `/run/celikpanel` is absent, it may publish that volatile
directory together with both common lock files. It holds the existing durable
release lock, verifies no active release transaction, requires the established
numeric group, verifies the complete source-bound helper and exact recorded
request/owner/kit/capture/file-plan relationship, then publishes an isolated stage
with `RENAME_NOREPLACE`. Authority is checked again around publication. A competing
path is re-observed; an existing partial or changed runtime is not normalized or
filled in. Existing lock identity and contention are preserved.

This cannot initialize a missing ledger or admit new work from an unrecorded
start tuple. Changed source, lost identity and conflicting requests preserve
existing evidence. A denied or interrupted staged publication may retain its
private unselected stage; this is not a selected runtime or a successful enrollment.
The existing v1 receipt and common-ledger formats are unchanged. Native execution
still revalidates accepted authority under release and host exclusion. The generic
lock-only runner remains non-initializing. No automatic boot dispatcher is added.

Validation covers both locks being published together, existing contention,
missing/malformed/owner-edited lock preservation, concurrent directory publication,
authority failure, exact recorded identity and release-before-host ordering. Native
continuation using this new runtime publication, reboot during unfinished enrollment,
production UI continuation and automatic boot dispatch remain open. This step
advances P0.2/P0.3/P0.5; it does not close them or authorize an installed update.


The first native recorded-runtime trial (`f586e74`) refused before publication
because the worker-owned release descriptor was sent to the inherited-fd9-only
preflight API. It left volatile runtime absent and the durable ledger unchanged;
that failed trial remains in the boot evidence. The owner-held descriptor API
now verifies the identical fixed transaction path, metadata, exclusive open-file
description and absent transaction markers without requiring a Go-allocated fd to
be number 9. The inherited API still requires fd9. Regression tests cover both
contracts, an unlocked/different descriptor, all transaction markers and changed
metadata. Corrected native continuation is recorded separately after verification.


Corrected native proof at `3af32e78675009b0ec76079dbef79eedd866d46a`:
Arch boot changed from `4f2dd9f5-9b75-47bd-95c7-97d7da0d672d` to the
new boot recorded in the evidence. `/run/celikpanel` was positively absent
before explicit owner continuation. The real installed helper then published the
0750 root:celikpanel directory and both empty single-link 0600 root:celikpanel
locks, verified the same terminal enrollment, and exited successfully. The
entire common ledger and immutable enrollment files remained byte-identical;
the native timer remained active. Management daemons were not started; compatible
Agent bytes remained required for this enrollment authority check.

This closes the bounded **terminal recorded continuation with absent volatile
runtime** test for this source on Arch. It does not prove an unfinished enrollment
through reboot, automatic boot dispatch, management-file-absent enrollment
continuation, production wizard enablement or a real mail certificate renewal.
The earlier failed descriptor trial is preserved alongside the passing trial.


### Recorded boot continuation (2026-09-23)

P0.1 bounded recovery and P0.5 independent native enrollment now have a boot
consumer. Before the first common reservation/workload publication, explicit
owner admission stages a per-request oneshot unit and its multi-user wants link.
A separate `celikpanel-mail-enrollment-boot/v1` plan binds the owner, enrollment
scope, exact unit/link inodes and protected parent directories. No renewal kit
schema or common ledger schema changes. This is bootstrap registration, not
permission for a boot-time process to admit a request. A cut before common
admission leaves a harmless boot probe; the same explicit owner start can finish
admission. Unreferenced private stages are preserved, not adopted or removed.

The fixed content-addressed helper's `--boot-enrollment` reads the canonical
ledger first. A missing request or a recorded terminal result does nothing;
missing/malformed ledger is unknown. Only the exact pending forward/rollback
reservation enters the existing release-lock, verified volatile runtime,
host-lock and recorded-scope consumer. Boot never arms a missing registration,
changes direction, fabricates an owner, restarts completed timers or starts an
installed-panel update. Ordinary Panel/Agent daemons are unnecessary. Compatible
retained Agent bytes/declaration are still required for unfinished enrollment.

Before automatic native work the helper verifies the recorded boot unit/link
and spends one immutable attempt receipt. The lifetime limit is three automatic
attempts per operation, including killed attempts. A systemd activation runs
once, bounded by the existing two-minute helper context and a three-minute unit
limit; it does not loop or reset the budget on reboot. Exclusion contention
before admission leaves evidence untouched and reports owner continuation.
After exhaustion, preserve evidence, inspect the exact native unit and mail
state, resolve the cause, and explicitly continue the same request. Owner
continuation retains native action budgets and does not reset the boot budget.

A completed boot registration cannot recreate an owner-removed enable link or
replace an edited unit, including a byte-identical different inode. Terminal
boot no-ops preserve later owner service preferences even without management
binaries. Registration is retained for diagnosis; automatic cleanup is outside
this slice. Older readers may reject the new optional receipt names; source/kit
binding prevents silently executing a different-generation consumer.

Component evidence: actual inherited-lock subprocess cuts at eight registration
checkpoints, exact resume, owner unit/link/mode/inode/stage changes, missing
paths, malformed/future/gapped attempts, absent arm, denied/cancelled authority,
and bounded attempts. Boot selection covers forward/inverse, terminal no-op,
missing request, malformed identity, and refusal of new-intent boot arguments.
Race tests passed for enrollment boot/worker/runtime and existing timer-enable
contracts; Agent/recovery vet and independent-helper/test-binary builds passed.
Native interrupted-boot evidence is not established by those component results.
Production wizard admission, management-file-absent unfinished enrollment,
previous-release migration and real mail renewal remain separate acceptance.


### Native automatic boot evidence (2026-09-23)

[Arch BE automatic boot](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-AUTOMATIC-BOOT-BE.json)
uses production helper source `d3aab13bc22b085433c94b77fd47a2d56f182d3e`.
A test-only producer uses the real common writer, scope, native executor and boot
registration, then SIGKILLs itself after the actual timer start returns and
before its receipt. The fixed installed production helper, not the test binary,
automatically resumes after guarded QEMU reset. Request
`1a60a47a049c80ea77d852625ce49873` reaches verified `published` with one boot
attempt; prior jobs and pre-cut immutable records remain unchanged. No manual
continuation or management daemon runs. Compatible Agent files remain present.

A separate reset after durable native owner-disable and removal of Agent/Panel
management files proves terminal boot no-op: timer stays disabled/inactive,
ledger/receipts stay byte-identical and `/run/celikpanel` remains absent.
The first fault fixture did not reach its unnecessary daemon-reload cut and the
first owner-preference fixture omitted a durability flush before immediate
power reset. Both are preserved as unproven trials, not silently counted as
acceptance. The corrected timer-start cut and flushed owner-preference trial
provide the results above. Initial SSH unavailability caused only another read.

This closes automatic forward continuation at the lost native-start-reply/boot
boundary and terminal owner-preference no-op on Arch. Inverse/early-admission
boot cuts, Debian enrollment, pending continuation with management files absent,
production setup admission, old-release migration and the wider P0 matrix remain
open. No owner-installed panel was changed.


### Owner continuation from the accepted setup (2026-09-23)

P0.2/P0.3/P0.5 now have a same-operation owner action for the dormant enrollment
step. POST `/api/v1/setup/mail-enrollment/continue` accepts only the current
execution/step IDs. Administrator identity, current revision, immutable reviewed
plan, persisted initial handoff fence, license, paired build and mail capability
are checked before handoff. Request/owner/kit generation come from the saved
plan; the browser cannot supply them. This endpoint never rewrites execution
JSON or clears the initial fence while the existing runner is reconciling.

`Agent.ContinueMailEnrollmentV1` requires matching recorded forward/inverse
identity and the verified installed helper. The worker receives only the saved
request ID, not new-intent owner/target arguments. Missing/conflicting evidence
cannot become new admission if it changes after the Panel's observation. A
terminal race performs no native work; the existing observer reports published
versus restored. Unknown/lost handoff remains unknown; polling and remounting
only read. Explicit continuation does not reset automatic or native budgets.
No ledger, kit, boot-receipt or setup-plan schema transition is introduced.
The added RPC is paired-build gated and Linux-only.

TR/EN guidance distinguishes recorded preparation, inverse continuation,
unverified outcome and known failure. The explicit continuation action appears
only on the current recorded forward/inverse wait. The current reason/action
precedes the step list. A recorded wait no longer falls through to a generic DNS
failure. Native unit details remain available. Lack of a recorded initial
admission does not expose this action or silently retry the original handoff.

Evidence: race-enabled Agent/Panel continuation, exact-plan, build-gate and RPC
policy tests pass, including real HTTP handlers, stale identities, client owner
injection, missing fence, revision/license/build denial, terminal no-op and lost
reply. Agent/Panel vet passes. All 80 setup runtime/guidance tests and the web
production build/bundle budget pass. A local production-bundle browser fixture
passes English/Turkish at 1440/390px: no overflow or page errors; lost POST reply
retains the operation; reload issues no second mutation. Browser API responses
are fixtures, not installed-server acceptance.

This closes the supported UI continuation gap for an already admitted plan. It
does not enable plan-builder admission, prove old-release migration or close the
remaining native inverse/early-admission boot and Debian matrix. No installed
panel was updated.


### Native automatic inverse boot evidence (2026-09-23)

[Arch BE inverse boot](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-INVERSE-BOOT-BE.json)
uses the unchanged installed production helper from `d3aab13` and a separate
fault-producing test executable. The fixture explicitly records rollback for
`e4e5f42aa03057c658dbf512357b01f0`, then SIGKILLs after real systemd timer stop
returns and before its receipt. After a guarded QEMU reset, the native boot unit
finishes this same inverse with one automatic attempt. No postboot manual
continuation or management daemon is used. The result remains a known failed
enrollment with `mail_enrollment_restored`, not a claimed successful install.

The original absence of the timer/service/hook/enable link is verified both on
disk and in systemd. Other common jobs and every precut immutable receipt remain
unchanged. The initial fixture attempt stopped at its assertion because the
native adapter normalized an intentionally injected command error; that attempt
is retained and not counted as inverse acceptance. The corrected fixture opened
the same accepted scope rather than creating another request.

This closes the selected lost-stop-reply inverse/boot boundary on Arch under
P0.1/P0.5. It does not prove automatic selection of rollback after an arbitrary
failure, early admission cuts, Debian enrollment boot, production plan admission,
old-release migration or the full P0 matrix. Compatible Agent files remain
required for unfinished enrollment; no owner-installed panel changed.


### Native Debian automatic boot evidence (2026-09-23)

[Debian 13 BE enrollment boot](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-DEBIAN-BOOT-BE.json)
uses the same installed production helper generation as the Arch trials. The
root-marker/DMI/QEMU-gated static test producer is killed after actual timer
start, before its receipt. On a new boot, the native unit automatically completes
request `1a1b8b789d273cb6f7a48ef9e1350728` with one attempt. No management daemon
or postboot owner continuation is used. Existing jobs and precut immutable
receipts remain unchanged. Postfix/Dovecot run after boot, with their captured
configuration/certificate files unchanged.

The older fixture renewal service/timer/hook were moved into a protected
before-directory by explicit lab preparation. This establishes the fresh
accepted-enrollment boundary, not legacy migration. The test does not prove
SMTP traffic, new certificate issuance or real renewal in this run. Debian
inverse boot, early admission, production plan admission, old-release migration
and the full P0 matrix remain open. No installed owner panel was changed.


The same Debian fixture additionally passes inverse boot for request
`6400548513ba4842582784d16da1adb8`: explicit test-fixture rollback is persisted,
then actual timer-stop return is interrupted. On a new boot the installed helper
restores original native absence with one automatic attempt and preserves known
`mail_enrollment_restored` failure. Old jobs/receipts and the captured mail
configuration/certificate files remain unchanged; Postfix/Dovecot are active.
This closes the selected Debian inverse boot boundary noted above, not automatic
failure-to-inverse selection or the remaining early-admission/migration matrix.


### Native cut before common admission (2026-09-23)

[Debian BE pre-admission boot](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-PREADMISSION-BOOT-BE.json)
SIGKILLs after the boot unit is armed, before the first common ledger reservation.
After reset, the installed boot helper exits successfully without admitting the
missing request, creating volatile locks, consuming an automatic attempt or
changing existing ledger/immutable receipts. Native renewal files remain absent;
Postfix and Dovecot remain active. This distinguishes prepared owner intent from
an admitted native transaction instead of inventing a job during observation.

A subsequent explicit owner `--start-enrollment` with the original reviewed
request/owner/generation reuses the existing preparation and publishes exactly
that request. All older jobs and precut receipts remain unchanged. The fixture
explicitly supplies the normal new-admission volatile lock prerequisite before
this owner action; it does not claim independent automatic provisioning of that
prerequisite. No management daemon runs. The unrecorded-handoff UI retry,
production plan admission, other early registration cuts/platforms and migration
remain open. No owner-installed panel was changed.


### Explicit retry before common admission (2026-09-23)

P0.2/P0.3/P0.5: the current accepted setup exposes a separate **Retry reviewed
handoff** action only after verified common-ledger absence. Its POST accepts
execution/step IDs and uses the same immutable request, owner and kit generation.
It shares the administrator, revision, license, paired-build and saved dispatch
fence checks with recorded continuation. It neither rewrites execution JSON nor
clears the fence. Unknown/unreadable evidence cannot dispatch; an intervening
recorded or terminal result returns to read-only reconciliation without replay.

This action uses the existing authenticated Start RPC and native locked executor.
It is explicit owner authority to retry the original reviewed handoff, not a new
setup or an automatic polling side effect. A lost response remains unknown.
Double click, remount and reconnect cannot automatically repeat it. No persistent
schema, unit template, license policy or native retry budget changes.

Evidence: both HTTP actions exercise the same denial matrix and unchanged saved
execution; retry checks the exact native tuple. Lost reply and unavailable,
foreign or untimed evidence retain the result without retry. All 82 setup runtime
and guidance tests plus the production web build/budget pass. The preceding
Debian native pre-admission trial establishes the reused Start path, not a live
browser-to-native installation. Production plan-builder admission and migration
remain open. No owner-installed panel was changed.


### Recognized legacy hook transition after reboot (2026-09-23)

[Arch BE legacy enrollment](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-LEGACY-BOOT-BE.json)
uses the exact versioned legacy hook as an explicit fixture before-state, the
existing production executor and unchanged installed `d3aab13` boot helper.
The inverse trial records compensation, kills the producer after real timer stop,
and resets the VM. Automatic boot restores the same legacy hook bytes **and
inode**, removes the originally absent independent units and preserves the known
restored failure. A new explicit fixture request then uses this restored legacy
hook without rewriting it, cuts after actual timer start, and automatically
publishes independent renewal after another boot. Both use one automatic attempt;
prior jobs and precut immutable records remain unchanged. No Panel/Agent daemon
or manual postboot continuation is involved.

This closes the selected recognized-hook forward/inverse reboot boundaries on
Arch under P0.3/P0.4/P0.5. It does not prove an actual previous-release install
migration, mail traffic, new certificate renewal, automatic choice of inverse,
management-file-absent pending recovery, production plan admission or the full
P0 matrix. The initial inverse SSH read failed during boot and is retained; the
successful later read introduced no mutation. No owner-installed server changed.


### Read-only reviewed kit source (2026-09-23)

`Agent.MailEnrollmentSourceV1` supplies the generation needed by a future new
reviewed plan. It validates the known paired build, protected Agent declaration,
running executable digest, complete installed helper bundle and fixed helper
path, then revalidates the pinned evidence before returning `verified` with its
observation time. Unknown source returns no usable generation or build identity;
private filesystem diagnostics are not sent over IPC. Partial proof objects are
closed even on inspection errors. The method is a bounded read policy, not a
mutation capability, enrollment admission or current native renewal health.

Race-enabled source/RPC/policy tests cover changed source, wrong running bytes,
foreign declaration, invalid generation/path, build mismatch, missing evidence,
cancellation and partial read cleanup. Agent/Panel vet passes. No persistent
schema changes, plan-builder enablement or capability advertisement occurs in
this slice. Native enrollment preflight and its plan review binding remain the
next admission boundary; existing accepted plans are untouched.


### New-plan native review and admission (2026-09-23)

P0.2/P0.3/P0.4/P0.5: `MailEnrollmentPreviewV1` combines the verified source reader
with a pinned, read-only native before-state. Absent ancestors are positively
observed beneath protected no-follow directories, never created during review.
Their later appearance invalidates the observation. Stray units/enable links,
unknown metadata, owner edits, unsupported hooks, stale loaded state and native
overrides prevent initial enrollment. The two fixed units use the shared native
observation parser. A running renewal is an explicit wait, not a claimed failure.
A verified independent schedule retains its generation and enabled/disabled,
active/inactive owner preference; review does not repair or restart it.

The new-plan builder accepts only verified, known-build, timed source/native
results. For absent/legacy configuration it adds the exact generation in the
existing step qualifier after certificate issuance. Existing accepted plans and
their identities are never extended. The existing execution adapter persists its
handoff fence before IPC, revalidates actual source/authority at dispatch and
uses the same native reservation/boot/continuation contract. Review is no promise
that a later owner edit can be overwritten. An unavailable preview blocks the
new mail plan with TR/EN action guidance. A busy renewal asks the user to wait and
review again. Internal kit digests remain in authoritative plans, not routine UI.
No persisted schema, kit format or license policy changes. No static capability
flag substitutes for the actual source/native inspection.

Validation: race-enabled native preview/hook/executor/boot tests passed (25.642s);
Agent RPC and Panel setup/build/policy regressions passed (1.423s / 379.661s).
All 83 setup UI tests, production build/bundle budget and Agent/Panel/runtime vet
passed. Logs are retained as `mail-preview-tests`, `mail-plan-admission-tests-final`,
`mail-plan-admission-ui-tests-final`, `mail-plan-admission-ui-build-final` and
`mail-plan-admission-vet` under the local evidence directory. The first broad
run exposed a DNS test double omitting its paired build; the fixture now reports
that identity, with production build checks unchanged. Preview tests cover no-follow missing ancestors, later
creation, owner metadata, orphan native files, unknown/busy/overridden systemd,
retained disabled schedules and paired source changes. HTTP/RPC and plan tests
bind the kit, order the step, deny unsupported evidence and assert no host or
execution mutation during review. The existing native forward/inverse/legacy
boot records establish those bounded helper paths; they do not prove the entire
new browser-to-native setup chain. Production publishing, full native wizard
acceptance, previous-release compatibility and full P0 acceptance remain open.


### Reviewed setup to native worker — September 23

[Arch native RPC evidence](../deploy/e2e/release-recovery/MAIL-ENROLLMENT-SETUP-RPC-BE.json)
uses committed production Agent/helper source `56ae293`, authenticated Unix IPC,
the production plan builder and start HTTP handler, SQLite execution/fence reload,
and the existing independent systemd worker. The test fixture supplies the
license and earlier completed setup prerequisites; those steps are not native
installation or certificate issuance evidence.

The real preview first observed the existing independent kit, and the plan
performed zero enrollment dispatches. After an explicit isolated-fixture change
to recognized legacy bytes, the new plan bound the actual installed kit, accepted
one setup request, persisted its fence and dispatched exactly once. Repeated
status reads after SQLite reload and after terminal publication never replayed
that call. The native helper completed request `0d7bfde74638abdbd3dc60c015ac4bae`,
with all earlier common-ledger jobs unchanged.

After stopping the fixture Agent and resetting the guarded QEMU machine, boot
`71b7013a-0f2b-454a-a7fd-217499453c88` retained every captured receipt and the exact
common ledger bytes. The native timer was enabled/active; Panel was absent and
all management daemons remained stopped. The compatible Agent file was retained.
This proves the reviewed enrollment step and its completed boot behavior, not
full first-install/browser/ACME, historical release migration, Arch mail traffic
or the complete P0 acceptance matrix. The first review attempt was rejected by
the test executable's incorrectly linked build identity; it started no enrollment
and is retained in the record. Only its test linker flag changed for the next read.
