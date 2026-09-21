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

## Arch AN acceptance

The [Arch AN summary](DISPATCH-BUDGET-AN.json) passes the same scoped policy with
exactly the same baseline/candidate archives as AL. Request
`4c0ec761ef57aa38aea5b2b7f83964fd` was admitted by the genuine Agent. One QMP reset
interrupted the first native rollback at `payload_restored`; the next-boot helper
proved and killed attempts 2 and 3 at that checkpoint. Three later timer runs
reported exhaustion without a child. The lock was free and the exact rollback
remained pending. One explicit supported owner retry completed it at
`2026-09-21T21:50:01Z`, retaining all three automatic receipt hashes and adding
exactly one owner receipt. Running and installed baseline binaries match; CLI
and authenticated HTTP agree, and anonymous HTTP is rejected.

The post-reset guest runs systemd 261 (261.3-1-arch), kernel 7.2.6-arch2-1. The
baseline installer upgraded kernel packages; this case does not validate firewall
or VPN readiness. Two genuine boot waits occurred before the second admission.
The retained wait sidecar therefore differs from the first observed publication.
A separate metadata-checked read proves its exact retained bytes, same request,
and staleness against the terminal status. The verifier requires distinct native
wait invocations and confirms that neither terminal reader exposes that stale
wait. It does not claim the first wait remained unchanged. AL's earlier evidence
still passes the verifier.

The earlier AM attempt is **inconclusive for budget exhaustion**. Its fault
observer exited on an unavailable process probe during the short boot deferral;
it did not perform the intended second/third cuts. The native timer subsequently
completed rollback. The private failure journal, cut log and terminal status are
retained under `/var/tmp/cp-release-drill-20260922-am/evidence/arch`. AN uses a fresh
VM and a bounded observer that treats an unavailable read-only process probe as
unknown, never as permission to cut. Full identity/checkpoint/snapshot checks
remain required before freezing or killing. No AM records were rewritten.

The disposable origin now uses the sealed platform's native CA store. Arch's
root-owned 0750 `/root` is accepted; the fixture payload remains root-only 0700.
These are lab-only changes, not production trust or ownership changes. Earlier
AM preparation refusals occurred before trust provisioning or update admission.
Both AM and AN VM pairs are stopped with private evidence retained.

```sh
python3 deploy/e2e/release-recovery/verify_dispatch_budget.py \
  --evidence-dir /var/tmp/cp-release-drill-20260922-an/evidence/arch \
  --operation-id 4c0ec761ef57aa38aea5b2b7f83964fd
```

## Still open

Reservation staging/publication-edge power loss, repeated deterministic child
errors, native owner-retry interruption/failure,
HTTP/browser access and budget-specific guidance while exhausted, production
signing/admission, workload continuity and the wider checkpoint matrix remain
open. This run interrupts **after** durable receipt publication and does not
claim every fsync or storage-loss boundary. Database, observation and runtime
protocol versions are unchanged; only test tools and evidence are added here.

## Debian AO native pause guidance

[AO's machine-readable result](BUDGET-GUIDANCE-AO.json) verifies the new pause
sidecar from a genuine native runner through the selected recovery CLI, in both
English and Turkish. Candidate `3218e50bc26cbe1b1544e312e4636b8c794d7809` is an
unpublished fixture commit whose parent is PR177's
`b2d739b2ecf6d31f9ee9f84282487941b5303a80`; the only fixture-source difference is
release-sequence policy. The archived SHA256 is
`3c048febf58a940328d6573d93179e6682157103ff4d176368ca105cb153b170`.
The baseline is the same Alpha81 archive as AL/AN. Trust remains isolated fixture
trust, not production signing or ordinary UI update admission.

Request `af0de8c801a3d13c780e9b4089d60c98` passed real Agent admission, worker
termination, first-recovery QMP reset and two further native recovery cuts at
`payload_restored`. Three subsequent timer invocations reported exhaustion. The
read-only guidance capture proves the root/panel metadata checks, exact status
bytes and nanosecond file identity, bound automatic hint, native JSON and EN/TR
owner instructions. The transaction lock was free, three automatic receipts
remained, and the known recovery failure stayed visible.

One supported explicit owner retry completed rollback at
`2026-09-21T22:24:36Z`. All automatic receipt hashes remained unchanged, one owner
receipt was added, and installed/running Agent and Panel matched the baseline.
The retained pause sidecar was stale; the selected CLI ignored it in favour of
verified terminal proof. Normal authenticated HTTP returned the same rollback
result and anonymous HTTP returned 401. This HTTP check is **after restoration**;
no browser or HTTP accessibility while Panel is stopped is claimed.

`guest_budget_guidance.py` only reads product state and writes root-private lab
evidence after the sealed identity and both prior native cuts are verified.
Capture once before owner continuation and once after it. The offline verifier
first requires the complete dispatch-budget proof, then checks the new native
hint, CLI text and terminal precedence. Negative cases reject stale identities,
foreign requests, changed status bytes, unknown CLI, lost failures, missing
instructions and terminal results that still show a pause.

```sh
python3 deploy/e2e/release-recovery/verify_budget_guidance.py \
  --evidence-dir /var/tmp/cp-release-drill-20260922-ao/evidence/debian13 \
  --operation-id af0de8c801a3d13c780e9b4089d60c98
```

Both AO VMs are stopped; private disks and evidence remain retained. This closes
new-hint Debian native CLI acceptance only. Arch new-hint acceptance, actual
browser pause access, interrupted owner continuation, publication-edge faults,
production trust and workload independence remain open. Existing AL/AN evidence
is unchanged. D-025 invariants 2, 4, 5 / P0.2–P0.3 remain partial. This evidence
change adds no product schema, migration, installed-panel update or release.
