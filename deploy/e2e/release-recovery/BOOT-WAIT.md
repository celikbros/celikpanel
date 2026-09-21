# Native boot-wait acceptance

*2026-09-21 · P0.2 / D-025 invariants 2, 4 and 5 · disposable fixture only*

## Result and boundary

Debian AK passed one genuine `waiting_for=starting` deferral, followed by the
existing native timer completing the same bound rollback. This extends
[AJ's terminal-reader acceptance](BOUND-WORKER.md); it does not close P0.2.
The [machine-readable summary](BOOT-WAIT-AK.json) contains source identities,
scoped results and hashes of 15 retained evidence files, without credentials,
private lab identity material or absolute evidence paths.

The test uses AJ's exact unpublished Alpha81/B and Alpha82/C archives and
separately generated fixture signing trust. A genuine Agent request
`a9d913db174173a92a92b0ede7ee03eb` starts the actual Go worker. The existing
bound-worker fault verifies its request, executable, complete snapshot and
immutable binding before SIGKILL. One guarded QMP reset then interrupts native
rollback at `payload_restored`.

A separately named, bounded fixture oneshot is enabled **without starting it**
before the reset. On the next boot it participates in `multi-user.target` and
keeps real systemd readiness at `starting` while collecting the actual native
wait. It exits after verification or after 120 seconds; systemd imposes a further
150-second timeout. It does not replace `systemctl`, write product observations,
start recovery, modify a lock/transaction, or change the recovery units. Only
registered nonce/DMI/QEMU guests and one sealed next-boot intent are accepted.

## Observed sequence

1. The guest boot ID changes after the verified recovery reset.
2. At `18:54:21Z`, the native runner records `recovering`, `terminal_proof=none`
   and `waiting_for=starting` for the exact request. Real `is-system-running`
   returns `starting` / exit 1; the current-boot journal records its deferral.
3. The root recovery CLI reads that actual record. The recovery service is
   inactive with exit 0, and a nonblocking probe confirms the transaction lock
   is free. The active descriptor, binding and snapshot manifest hashes match
   before and after the observed deferral. No recovery child is dispatched by
   the observed deferral branch.
4. The fixture boot job exits. A different native recovery invocation completes
   rollback at `18:55:09Z`. No manual recovery/start or second update is issued.
5. The actual CLI and authenticated HTTP agree on `recovered / rollback_verified`;
   HTTP adds its documented `panel_state=ready`. Anonymous HTTP returns 401.
   The old `.wait` bytes remain, but terminal readers expose no `waiting_for`.
6. Installed and running Panel/Agent hashes match baseline B. The active
   descriptor is absent. Both monotonic sequence floor and foundation remain
   82/C while the restored payload is Alpha81/B.

During the wait, persisted `previous_failure` is `none`; the CLI omits it.
`update_failed` is recorded later and remains in the terminal result. This trial
therefore does **not** prove preservation of a pre-existing recorded failure
through a native wait; the corresponding component test remains separate.

## Reproduction and verification

Use a new registered lab and the [bound-worker procedure](BOUND-WORKER.md).
After staging its exact worker intent and helpers, stage `guest_boot_wait.py`
with the existing guarded upload and run its `arm --operation-id <exact-id>`
inside that guest before the accepted update. Do not start its unit manually.
The normal bound-worker kill and guarded recovery reset start the native trial.
Collect the root-private boot-wait log/receipt, before/after boot IDs, genuine
start/cut/reset evidence, native journal and terminal reader output.

The offline verifier reads only retained files and never contacts a server:

```sh
python3 deploy/e2e/release-recovery/verify_boot_wait.py \
  --evidence-dir /var/tmp/cp-release-drill-20260921-ak/evidence/debian13 \
  --operation-id a9d913db174173a92a92b0ede7ee03eb
```

The 15 new unit tests reject stale/wrong-operation hints, wrong boot identities,
held locks, active runners, mismatched HTTP results, altered recovery material,
wrong running binaries and same-invocation/reversed-order retry claims. They are
supporting checks, not substitutes for the native evidence above.

Both AK guests are stopped and their private disks/evidence retained. No owner
panel, production release, product schema, recovery ABI or access guard changed.

## Remaining acceptance

Native `initializing` and `stopping` hints, prior-known-failure wait preservation,
HTTP/browser wait access, other platforms/checkpoints and the wider P0 matrix
remain open. Active transaction start guards can intentionally keep Panel/Agent
off while recovery has deferred; this test proves the independent root CLI,
not continuous HTTPS availability. Production signing/distribution, database
migration and hosted workload continuity are not established by AK.
