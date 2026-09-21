# Native recovery dispatch budget acceptance

*2026-09-22 (trial: September 21 UTC) · P0.2/P0.3 · D-025 invariants 2–5*

## Result and scope

Debian AL passes a genuine worker interruption, one QEMU reset during the first
native recovery dispatch, two further recovery SIGKILLs, repeated automatic
budget exhaustion, and one explicit owner retry completing the same rollback.
The [offline-verified summary](DISPATCH-BUDGET-AL.json) retains artifact identities
and evidence hashes. This is scoped acceptance, not closure of P0.2/P0.3.

The baseline is the existing unpublished Alpha81 fixture at
`45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00`. The candidate is unpublished Alpha82
`20c72254de4211a0c56fa7e16b9aec6b0e68b888`, whose only change from implementation
`4bc2e18d1446fd9e48ae6918d8fa7cc41625c451` is its fixture release-sequence policy.
No production tag, distribution or installed owner panel was changed. Fixture
trust is enrolled only during initial installation in a registered disposable VM.
The selected kit's entrypoint, CLI and observation helper match the candidate
archive, and its complete runtime manifest is verified.

## Observed sequence

1. The actual Agent accepts request `5c03d502560c27e1a5357b47c3fe5c1f`. The existing
   guarded helper proves the worker, installed candidate, full snapshot and
   immutable request binding, then kills that update unit at `21:01:26Z`.
2. The native runner reserves automatic slot 1. At rollback `payload_restored`,
   the existing helper freezes and verifies its exact cgroup, process and durable
   checkpoint. The host submits one QMP reset to that registered QEMU only.
3. On the new boot, a bounded fixture boot job allows observation of genuine
   `starting` deferral. No reservation is consumed by this deferral. The timer
   then reserves slot 2; the independently armed fixture cuts that exact recovery
   at `payload_restored` at `21:02:34Z`. Slot 3 is likewise interrupted.
4. Two subsequent native timer invocations report that all three admissions are
   spent and start no recovery child. The exact active rollback remains, the
   native lock is free, and the real CLI reports `recovery_required`, no terminal
   proof, and `recovery_incomplete`. The journal provides the owner command.
5. The fixture fault helper has finished. An explicitly invoked
   supported owner CLI retries that same snapshot once. It records one owner
   receipt and completes rollback at `21:05:01Z`. All three automatic receipt
   hashes remain unchanged. No second update or receipt reset is performed.
6. Installed and running Agent/Panel hashes match Alpha81. The exact transaction
   is cleared. The selected recovery kit, monotonic floor and foundation remain
   at the candidate generation/sequence 82. CLI and authenticated HTTP agree on
   `recovered / rollback_verified`; anonymous HTTP returns 401. The actual update
   failure remains visible in terminal `previous_failure=update_failed`.

The three interrupted dispatches have unknown completion, not three proved
permanent child errors. Prior failure is `none` during the boot wait;
`recovery_incomplete` is recorded at exhaustion and `update_failed` by terminal
reconciliation. This does not prove preservation of an earlier known failure
through native exhaustion. The UI was not tested while the budget was exhausted.

## Reproduction and evidence

Start a fresh registered lab using `lab.py`, the [bound-worker procedure](BOUND-WORKER.md),
and the exact baseline and committed candidate archives. Create fixture keys and
stage **the public key before** `current_worker_baseline.py start`. AL's first
pre-admission attempt omitted that upload and refused before the started marker
or any product installation. That failure remains in private evidence. After
verifying fresh state, the same sealed driver was started with the public key.

After staging the bound worker intent, stage `guest_boot_wait.py`,
`guest_dispatch_budget.py` and its named dependencies. Arm each with the exact
operation ID before starting the actual worker. Neither fixture unit is started
manually. The normal worker fault and `bound_worker_reboot.py` submit the reset.
The next-boot budget helper targets only attempts 2 and 3 with existing complete
checkpoint/cgroup/snapshot proofs. It never edits product records or starts a
recovery child. Its fixture unit is bounded to 650 seconds.

Use `guest_dispatch_budget_result.py exhausted --operation-id <id>` to capture
native status and retained receipts. After verifying exhaustion and the completed
fault helper, its `owner-retry` command admits exactly one supported root CLI call
for the bound snapshot. Admission is recorded once before invocation; a repeat
refuses rather than starting a second command. This helper is for registered
disposable labs, not installed owner servers. Capture final service/hash evidence,
the normal authenticated reader and the native journal before stopping the VMs.

The offline verifier needs the retained private evidence directory, not a server:

```sh
python3 deploy/e2e/release-recovery/verify_dispatch_budget.py \
  --evidence-dir /var/tmp/cp-release-drill-20260921-al/evidence/debian13 \
  --operation-id 5c03d502560c27e1a5357b47c3fe5c1f
```

It links actual accepted worker and reboot proofs to both later native cuts,
receipt hashes, repeated pause journals, one owner command result, restored
executables and matching authenticated readers. It distinguishes owner completion
from an automatic timer completion. Negative tests reject wrong requests/boots,
reset receipts, a fourth automatic slot, duplicate owner admissions, remaining
markers, held locks and unknown outcomes. Both AL VMs are stopped; disks and
private evidence remain retained.

## Still open

Reservation staging/publication-edge power loss, repeated deterministic child
errors, native owner-retry interruption/failure, Arch budget acceptance,
HTTP/browser access and budget-specific guidance while exhausted, production
signing/admission, workload continuity and the wider checkpoint matrix remain
open. This run interrupts **after** durable receipt publication and does not
claim every fsync or storage-loss boundary. Database, observation and runtime
protocol versions are unchanged; only test tools and evidence are added here.
