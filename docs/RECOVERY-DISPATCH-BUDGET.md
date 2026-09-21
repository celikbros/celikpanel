# Bounded recovery dispatch

*2026-09-21 · D-025 invariants 2–5 · P0.2/P0.3 partial implementation*

## Problem and scope

The native recovery timer runs 30 seconds after an inactive invocation. Previously
it had no persisted dispatch budget: the same deterministic child failure could
re-enter the mutation body indefinitely. A timeout or systemd start-rate limit
would not provide an exact-operation, reboot-persistent bound.

The runner now admits at most **three automatic child dispatches per snapshot**.
This is a conservative total-attempt limit, not a classifier claiming that every
failure is permanent. Each admission consumes a slot before the child starts;
an interrupted or uncertain attempt remains consumed. Transient operating-system
waits, lock contention, absent transactions and final-proof reads consume none.
The timer remains enabled and can observe the same transaction after exhaustion,
but cannot dispatch another recovery child for it.

## Durable contract

`/var/lib/celikpanel-release-state/recovery-dispatch/v1/<snapshot>/` is separate
from strict transaction markers, application data and nonauthorizing observations.
Root-only 0700 directories contain immutable root:root 0600 receipts `1`, `2`, `3`.
Each receipt has six canonical newline-terminated fields:

```
schema=celikpanel-recovery-dispatch/v1
snapshot=<exact native snapshot>
attempt=<1|2|3|owner>
token_sha256=<digest of the current native token>
operation=<update|rollback>
phase=<native transaction phase>
```

The snapshot identity spans update-to-rollback takeover; the token hash records
that attempt's provenance and is never a caller-supplied mutation capability.
Receipts must form a contiguous sequence. Unsafe modes, links, FIFOs, wrong
snapshot/schema, truncation or a missing middle receipt refuse dispatch. Owner
retry does not normalize or bypass these checks. The existing native lock covers
inspection, reservation and child dispatch. Staging is fsynced, published without
overwriting an existing destination, then its directory is synced before dispatch.
An unpublished `.pending.*` stage is retained but does not authorize a child.
A published receipt is conservatively spent even if execution was interrupted
before the child actually started. These receipts are retained after completion;
this change implements no evidence cleanup.

No marker, snapshot, native service or permission proof is relaxed. Existing
transactions without receipts begin with three available slots; historical
attempts cannot be reconstructed. Old selected kits do not implement this policy.
The new source is enrolled through the existing verified kit transition; database,
observation v1, snapshot format and kit protocol do not change. Older CLIs reject
the new explicit retry arguments instead of silently treating them as permission.

## Owner action and observations

After exhaustion, the exact observation becomes `recovery_required`,
`terminal_proof=none`, `reason=recovery_incomplete`. A previously verified failure
is preserved by both shell and Go publishers. Terminal proof still dominates.
The service journal explains the limit and prints the exact owner command:

```
sudo /usr/libexec/celikpanel/recovery recover --retry --snapshot <exact pending snapshot>
```

The owner first inspects `journalctl -u celikpanel-release-recovery.service` and
resolves the reported cause. This command authorizes **one** dispatch for that
pending snapshot. It cannot start an update, name another transaction, bypass
readiness/locks/evidence checks or replenish automatic slots. A private
`owner.<random>` receipt records the explicit dispatch. If a lock is held, no
transaction exists or the snapshot differs, the command refuses without saving
future retry authority. If the OS is still transitioning, it only defers; the
explicit permission is not queued for a later invocation.

The HTTP/CLI v1 status retains the existing recovery-required state. The native
runner now additionally publishes exact-status-bound `automatic_recovery:
paused_retry_limit` guidance; see the contract below. Older readers keep generic
guidance. Polling never authorizes retries.

## Evidence and open acceptance

The real shell runner contract passed in an isolated filesystem with modeled
systemd readiness and a fixture child. It covers three reservations, a SIGKILL of
the runner, subsequent fresh-process refusal, released locks, unchanged markers,
owner retry, unchanged automatic receipts, unsafe/truncated/missing receipts,
foreign snapshot rejection and final terminal verification. Existing actual
rollback-entry lock handoff cases and independent-runtime shell contracts pass.
Shell/Go observation parity and race-enabled CLI/observation tests pass.

[Debian AL native acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md)
now proves selection of the new kit, a reboot after the first reservation, two
more interrupted native attempts, repeated exhaustion and one owner retry with
unchanged automatic receipts. CLI and authenticated HTTP agree on final rollback.
Earlier AJ/AK evidence predates this policy and is not reused as proof for it.
Reservation-publication-edge power loss, native repeated deterministic errors,
browser budget guidance and the broader fault/workload matrix
remain open. No production release or installed owner panel changed. P0.2/P0.3
remain open.

[Arch AN acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#arch-an-acceptance)
now passes the same three-admission exhaustion and explicit-owner continuation
boundary with the exact AL candidate. The earlier AM observer failure remains
inconclusive and retained. Repeated boot-wait publication is distinguished from
unchanged-wait preservation. This closes the scoped Arch budget case, not the
publication-edge, browser-guidance or full P0.2/P0.3 matrix.

## Compatible pause guidance (2026-09-22)

D-025 invariants 2, 4, 5 / P0.2. Only the native three-receipt exhaustion branch
publishes this hint. `<request>.automatic` uses
`celikpanel-recovery-automatic/v1` and five canonical newline-terminated fields:
`schema`, `request_id`, `observation_identity`, `observation_sha256`, and
`automatic_recovery=paused_retry_limit`. The existing status file remains the
unchanged eight-field observation v1. There is no database or kit-protocol migration.

The optional sidecar follows the wait contract: bounded 2 KiB, regular single-link
root-owned 0640 with the panel group, beneath the verified 0750 observation root.
It is published under the existing producer lock, with exact status bytes and
GNU-stat nanosecond identity, after the status rename. Later status republication
invalidates it even for identical bytes. Malformed, unsafe, stale or unsupported
optional data is ignored while the verified v1 result remains readable. A
terminal proof wins. Optional publication failure cannot retain the native lock
or admit another child. Old readers/producers need not understand or delete it.
The private receipts remain the dispatch authority; the public hint is not.

The administrator UI and owner CLI explain the recorded three-attempt limit,
name the server owner as the actor, show the bounded recovery-journal command,
and direct the owner to resolve the reported cause and use the same-operation
one-time retry command printed there. They do not infer exhaustion from a generic
error or add a mutation endpoint. The exact request, observation time and previous
failure remain visible. Missing subsequent reads retain the last observation with
an explicit unknown-current-result explanation. Check/reload only reads.

Validation: real shell producer-to-Go reader compatibility, stale/identical-byte
republication, wrong request/hash/identity/schema, unsafe modes, links and FIFO;
terminal dominance and preserved prior failure; root CLI and administrator-only
HTTP; full native-runner shell contract with modeled systemd and bounded child;
462 web tests and production build. Local Chrome EN/TR at 1440/390 px checks
reload, failed subsequent read, GET-only requests and no overflow/page errors.
Browser responses and shell readiness in those checks are fixtures. AL/AN prove
the earlier native dispatch implementation, **not** this newly added hint.
Fresh native hint-to-browser acceptance and access while Panel is stopped remain
open. No production release or installed owner panel was changed.

[Debian AO native acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#debian-ao-native-pause-guidance)
now proves the new exact-status pause hint reaches the selected CLI in EN/TR after
three interrupted native attempts, and retained stale guidance loses to verified
rollback after one owner continuation. Old binaries are restored and authenticated
HTTP agrees afterwards. This closes native Debian hint-to-CLI acceptance; it does
not close browser access while Panel is stopped or the full P0.2/P0.3 matrix.
