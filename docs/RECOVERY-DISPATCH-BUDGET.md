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

The current HTTP/CLI v1 status exposes the existing generic recovery-required
state, not a numeric budget field. Exact budget guidance is in the native journal.
A dedicated browser budget view remains open; polling never authorizes retries.

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
other platforms, browser budget guidance and the broader fault/workload matrix
remain open. No production release or installed owner panel changed. P0.2/P0.3
remain open.
