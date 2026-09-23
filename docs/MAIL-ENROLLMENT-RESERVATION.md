# Durable native mail enrollment reservation

P0.2/P0.3/P0.5; constitution: owner continuity, exact operation ownership,
unknown is not absence, and verified terminal results. This is the private
reservation boundary for the existing composite native enrollment executor.
The explicit root-owner worker entry is described below; authenticated wizard
admission and automatic boot dispatch are still open. This does not complete
the resilience plan.

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
outer dispatcher on every call. The root-owner CLI below can invoke this
boundary; RPC, renewal-hook and polling admission remain disabled. A digest or
callback alone is not serialized owner authority.

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

**Enablement remains gated:** no new capability is advertised and the plan
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
