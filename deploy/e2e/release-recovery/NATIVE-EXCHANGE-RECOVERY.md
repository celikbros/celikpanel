# Recovery reboot after a native database exchange cut

*September 15, 2026 · [Türkçe](NATIVE-EXCHANGE-RECOVERY.tr.md) · D-025 / P0.3*

This closed disposable-VM fixture combines two faults in one update operation:
a successful native database exchange is interrupted before its publication
receipt, then its automatic rollback is interrupted at `payload_restored` by one
host-controlled QEMU reset. Reset submission alone is not successful recovery.

The affected [resilience invariants](../../../docs/RESILIENCE-CONTRACT.md) are
2–5: meaningful evidence, precise unsafe boundaries, explicit recovery, and
recovery independent of a failed candidate. This is fixture and acceptance work.
Product code, schema38-to-42 migrations, `recovery-material/v3`, and database
admission/seal/publication/restoration schemas are unchanged.

## Fixed authority and second-fault admission

`native_exchange_recovery_trial.py` selects only the
`database-exchange-recovery-reboot` profile. It has separate intent, gate, tracer,
result and capture paths and pins all 20 uploaded helpers. Existing WAL and
exchange-only profiles retain their own authority and cannot borrow this profile's
reboot permission. Mutating modes require `--execute` and an exact registered
lab guest; there is no arbitrary PID, command, server or checkpoint argument.

The first fault uses the existing [native exchange observer](NATIVE-DATABASE-EXCHANGE.md):
the exact selected checker performs `renameat2(RENAME_EXCHANGE)`, returns zero,
and is held with the admitted updater family before `published.json` exists.
The checkpoint independently verifies the canonical After/retained Before pair,
immutable snapshot, recovery kit, material, locks, task identities and receipts.
Bounded copies preserve the actual pair without opening originals with SQLite.

While that exact checkpoint remains held, the fixture arms the existing recovery
observer for the fixed action `reboot` at `payload_restored`. Its sealed intent
must bind the same operation, snapshot, manifest, transaction token and selected
recovery runtime. The checkpoint is revalidated during and after admission.
Only then is SIGKILL sent to the exact updater unit. Failed admission cannot
authorize that kill; uncertain kill outcomes retain the request without a success
receipt and cancel only the admitted helper, recording unknown cleanup if needed.

Native systemd OnFailure starts the product's normal recovery. The observer waits
for a real durable `payload_restored` checkpoint, verifies its native recovery
worker and freezes that exact cgroup. This checkpoint follows database restoration;
it is **not** an interruption inside the inverse exchange syscall.

The host requires the complete first-cut event chain and matching handoff before
opening QMP. It preserves the native cut and pre-reset journal, rechecks a fresh
frozen recovery proof, then persists a once-only reset intent. QEMU PID/start,
socket ownership/inode, peer credentials and VM UUID must agree on the same
connection. One `system_reset` is allowed within the proof deadline. The guest
observer cannot reboot the guest; missed checkpoints and uncertain submissions
are never retried under the same attempt.

## Terminal acceptance

`exchange_recovery_evidence.py` checks captured evidence offline. A successful
claim needs different boot and recovery invocation IDs, the same snapshot/token
and selected kit, native OnFailure/rollback records before reset, native rollback
completion after reset, the final `schedulers_restored` checkpoint and cleared
transaction markers. An old successful record, reset acknowledgement or running
process alone cannot satisfy it.

Independent terminal inspection must additionally establish the inverse-exchange
receipt chain, original canonical Before inode, retained migrated After bytes,
unchanged sealed work, old running/disk binaries, and complete typed-row retention
across all 55 old tables. Host SQL runs on fresh private copies only. DNS A/SOA
UDP/TCP, served/installed TLS fingerprint and HTTPS status are separate point
checks. A post-terminal measurement freeze/copy/thaw is explicitly distinguished
from recovery.

## Limits

A QEMU reset is not physical power loss or storage-cache durability evidence.
These populated SQL fixtures do not prove native hosted workloads, uninterrupted
availability, every migration, every recovery checkpoint or signed update
admission. The full P0.3 matrix remains open. No installed user panel update,
production recovery, license-policy change or new product release is performed.

## X: Debian native acceptance

Fresh disposable root `/var/tmp/cp-release-drill-20260915-x` used genuine
Alpha64/schema38 and the same unpublished candidate as the preceding exchange
trial: source `cb3165456bb4ba4654dc19d51a5eafc13721a5fb`, archive SHA256
`52dd34b435ba6d1fd1429aaadf875bf0cacae9f0cbeeb249ea9795e74a08698d`, selected kit
`ed7c3eebe46d7ee5a1ad4b566ef4794daf295cb695151be40a4577b8fdc5a770`.
DNS observation tools and persistent journal storage were prepared before baseline
installation. The 20 helper pins match the reviewed source.

Operation `6c5e5a31ed6164cbc6888de7748ecbf0` used snapshot
`20260915T202701Z-from-unknown-to-cb3165456bb4ba4654dc19d51a5eafc13721a5fb-c8105cd7d2840cc6589e11faa4199d7c`.
Its real successful exchange, absent publication receipt and exact updater SIGKILL
were verified. Native OnFailure entered rollback; at 20:31:47 UTC its durable
`payload_restored` checkpoint was held and one registered QEMU reset was sent.
The new boot and final recovery invocation differ; the snapshot, token and kit
remain identical.

The first boot recovery invocation failed at 20:32:00 because systemd was still
`starting`. The original host controller stopped on that observed failure and
retained its failure record. No reset, update or manual recovery was repeated.
The existing native watchdog (`OnUnitInactiveSec=30s`) started recovery again at
20:32:30; the same rollback reached `schedulers_restored` and completed at 20:32:46.
A separate controller then performed the **first** terminal measurement, after
read-only checks established this later terminal result. The failed invocation is
retained in the journal and causal proof. This is eventual automatic recovery,
not success on the first boot invocation or uninterrupted availability.

Independent host-only analysis verified the actual exchanged schema38-to-42 pair,
all 55 old tables and 99 old rows, four migration additions and ten prescribed new
tables/defaults. After recovery, schema38 and the original Before inode were
restored. Every cut-time rowid and typed value was retained unchanged; three later
metrics rows make 102 final rows, so global equality is **DIFFERENT**. Raw DB/WAL
reconstruction agrees with the saved backup. Publication intent, inverse-exchange
restoration intent/receipt and retained After/work bytes were checked separately.

Both coordinators ran the original baseline binaries; the transaction markers
were cleared. Before/after authoritative A/SOA over UDP/TCP matched the fixture;
served/installed TLS fingerprints matched and HTTPS returned 200. The separate
post-terminal measurement freeze/copy/thaw occurred once and preserved both
coordinator process identities. Both registered X guests were then stopped, with
disks and evidence retained. No installed panel was accessed.

| Preserved proof | SHA256 |
|---|---|
| Debian terminal outcome | `72a28de7be253dc3958e0ee50deafda12d85f33ff80e575d7744d0769c563915` |
| Host row review, `analysis/debian13-exchange-rows-xqg53a0o/review.json` | `65518d099257b4dc536e3e46b5db8099ee51fd1801700d7bc05d55be44276d43` |
| Host seal, `analysis/host-seal-1789504648780978582.json` (92 inputs, 20 helper pins) | `554ea5d45b1837a253f154100300f513b3897df034e97d586f98ef8dc6b06774` |

The final local suite ran 517 tests: 516 passed, with one explicit missing actual
binary-input skip. The two focused tracer modules ran 49 tests under root and 49
under `nobody`, all passing without skips, including actual child exchange calls.
These local and native results are distinct from CI. The two-fault native result
is Debian-only; Arch, other checkpoints, real workloads and the remaining P0.3
acceptance items remain open.
