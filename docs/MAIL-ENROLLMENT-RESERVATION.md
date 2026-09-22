# Durable native mail enrollment reservation

P0.2/P0.3/P0.5; constitution: owner continuity, exact operation ownership,
unknown is not absence, and verified terminal results. This is the private
reservation boundary for the existing composite native enrollment executor.
It does not enable production enrollment or complete the resilience plan.

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
outer dispatcher on every call. No RPC, renewal hook or production CLI invokes
this boundary yet. A digest or callback alone is not serialized owner authority.

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

The composite executor has its separately recorded Arch native proof. Joining
this reservation to authenticated production owner intent, dispatch/boot recovery,
initial private identity provisioning and the full native update/rollback and
workload matrix remains open. Component success does not close those P0 items.
