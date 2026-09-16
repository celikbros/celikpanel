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
installation. The 20 helper pins bind the reviewed working files. A separate source binding verifies every uploaded helper against commit `a422ec739ce64e38602ca7b506762cfa01f0b529`: five differ only by Git CRLF-to-LF normalization; the other 15 are byte-identical. Raw uploaded pins remain unchanged. The binding is `analysis/helper-source-binding-1789504804577210406.json`, SHA256 `5265b66db7f53ca94f5d1df92d9ecef6f77024c7710f99ee77fc60bcb570cd57`.

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
These local and native results are distinct from CI. At the X checkpoint this two-fault result was Debian-only; Z below adds Arch.
Other checkpoints, real workloads and the remaining P0.3 items remain open.

## Z: Arch native acceptance

The September 16 local-date run used fresh disposable root
`/var/tmp/cp-release-drill-20260916-z`; the journal timestamps below are September
15 UTC. It reused X's genuine Alpha64/schema38 baseline, unchanged candidate
archive and selected recovery kit. All 20 uploaded helper pins were independently
bound to commit `f63229497da0eff906c91a0a70f870e964415ae7`: 15 byte-identical files
and five with only Git CRLF-to-LF normalization. The raw pins were preserved.

An earlier Y preparation attempt stopped before baseline installation or native
fault admission when an extra DNS-observer package command returned nonzero.
Its first controller did not preserve that command's output; the exact package
failure reason is unconfirmed. Y was stopped with its evidence retained, not
reused or counted as a recovery test. Z prepared the persistent journal before
baseline installation and used the existing DNS seed operation to supply BIND
and its observer. Later command failures preserve stdout/stderr. The Y assessment
hash is recorded below.

Operation `54ac53a2ba3b5f5c84076d2f064f392f` used snapshot
`20260915T212447Z-from-unknown-to-cb3165456bb4ba4654dc19d51a5eafc13721a5fb-ebba15fe7f3f45aa50b11acb9a048eeb`.
The successful native exchange, held canonical After/retained Before pair,
absent publication receipt and exact updater SIGKILL were verified. Native
OnFailure began rollback; its durable `payload_restored` checkpoint was held at
21:30:42 UTC and one QEMU reset was submitted. The new boot and final recovery
invocation differ; the operation, snapshot, transaction token and selected kit
are unchanged.

The first two boot recovery invocations failed at 21:30:55 and 21:31:26 because
systemd was still `starting`. Both failures remain in the journal and causal
proof. The host only observed the existing native timer; it did not start a
second update or manually invoke recovery. The timer started the third boot
invocation at 21:31:57, and the same rollback completed at 21:32:14 with the
`schedulers_restored` checkpoint and cleared transaction markers. This proves
eventual automatic recovery, not first-attempt success or uninterrupted access.

The exchanged pair passed the populated schema38-to-42 semantic checks across
all 55 old tables. Final schema38 restored the original Before inode and all
100 cut-time rowids and typed values unchanged. One later metrics row makes 101
final rows: global equality is **DIFFERENT**, with zero missing or changed old
rows. The inverse-exchange intent/receipt, unchanged admission/seal/publication
records, retained migrated After bytes and sealed work were independently checked.
Raw DB/WAL reconstruction agreed with the saved backup. Both running/disk
baseline binaries, authoritative A/SOA UDP/TCP, served/installed TLS fingerprints
and HTTPS 200 passed. The first and only terminal measurement freeze/copy/thaw
preserved both coordinator process identities. Both registered Z guests were
then stopped; disks and private evidence remain retained.

The baseline installer had upgraded the kernel package before the candidate
trial: the running kernel was `7.1.8-arch1-3`, while installed modules belonged to
`7.2.6-arch2-1`. The test reset booted `7.2.6-arch2-1`; installed Linux/systemd
package versions were unchanged across that reset. The installer's reboot warning
and both observations are preserved. This result does not claim firewall/VPN
readiness or recovery with an identical running kernel across boots.

| Preserved proof | SHA256 |
|---|---|
| Arch terminal outcome | `40af76b80cf2ecfc0a5246df748c2696dc2e4211a6ce6f749d8cea7f5d22e0dc` |
| Host row review, `analysis/arch-exchange-rows-bqne81lj/review.json` | `5bf0d718b5e0fc3f339ff83a6af20024756e087d83735d19be8aba872d7c50d4` |
| Host seal, `analysis/host-seal-1789507983843020713.json` (96 inputs, 20 helper pins) | `58583442a8d8563f0bb1a91f5046fd85835707083b408a81c418e7c110884f7b` |
| Helper source binding, `analysis/helper-source-binding-1789507986010838196.json` | `a430639bad29f33d33bcd16ddcf1a6f35f98c758c100f85719eaa2f16130fc8b` |
| Y preparation assessment, `evidence/arch/y-preparation-assessment.json` under the Y root | `99b160905ce9ee0471c0209199c3e529f695a48793f5a5c8442a4955e54fdfc2` |

The unchanged local suite again ran 517 tests: 516 passed and one explicitly
skipped its missing actual-binary input. X and Z now cover this two-fault boundary
on Debian and Arch. Product code and persisted schemas are unchanged. Other
checkpoints, real workload/renewal independence, signed admission, physical
power-loss durability and the remaining P0 acceptance items stay open.

## AA: changed-runner trial was inconclusive before the requested faults

A fresh Arch attempt on September 16 used the runner correction from commit
`8d62896f0fb5be329090923ad074115941c0328c` (tree
`fb57ef517982dec3b5d08673efcc658a23ace21b`). Its unpublished Alpha81 fixture archive
was `1c49df9ea65ec5758470b4ee347ed51cd2d00b7cf3f3f34253f58543d4a6cba8`.
The genuine Alpha64/schema38 baseline, populated SQL fixture and authoritative
DNS precheck completed in `/var/tmp/cp-release-drill-20260916-aa`. Operation
`d8b241a2e3665e04cc81f261628cb926` belonged to cell
`release-recovery__90f1cb34a68e7020`, Arch UUID
`a0eaacb0-3f8f-521f-8620-390d6c62649e`.

The attempt did **not** establish either requested fault boundary. While the
candidate's `recovery verify-compatibility --mode --normal` waited, a read-only
process capture found panel-checker leader 14295 in zombie state and thread 14299
in a ptrace stop owned by tracer 10882. That thread was absent from the preserved
`task-admitted` records. The exact kernel event ordering behind this discrepancy
has not been established. The tracer reached its bound and reported
`trace-detach-incomplete-controller-watchdog-required`, with
`controller_cut_called=false`, `cleanup.complete=false` and leader 14295 still
listed in its cleanup result. The host reboot controller timed out without
submitting a reset. Recorded controller events were `armed`, `gate_released`,
`trace_finished`; no exchange-cut or recovery-reboot proof was produced.

This is an **inconclusive measurement**, not evidence that the changed boot
readiness path passed or failed. No mutation was retried. Current state, journal,
raw trace and the process diagnostic were preserved; the registered Arch and
Debian QEMU guests were then stopped with their disks retained. A corrected,
verified tracer and a fresh registered attempt are required before claiming
changed-runner native acceptance. X/Z remain evidence for their earlier source.

The local candidate used Go 1.26.5 and Node 26.8.1. Two earlier build-preparation
failures were retained: the wrong default Go version and Windows archive CRLF
conversion. The successful archive used Git's LF export; runner/checker-source
list bytes were compared with their exact Git blobs. The baseline installer again
reported a kernel/modules mismatch; there was no trial reboot to observe the new
kernel. No firewall/VPN or workload-independence claim is made.

| Preserved proof | SHA256 |
|---|---|
| AA inconclusive assessment, `evidence/arch/aa-inconclusive-assessment.json` | `f923722f2c51c2a524e18e3e03c92c673435029cf4098a85c38d1332ac767c7a` |
| Raw final trace, `evidence/arch/aa-final-trace.jsonl` | `4e2018a59e2bd734c0a34542cb87329c8ca6acae0d07138ccf0cf96ac1971765` |
| Candidate build evidence (including both failed preparations) | `54c62e5c7a2637935efa509b5a9794deb962f32fc54af8c918765342bcf6e1aa` |
| Helper source binding, `analysis/helper-source-binding-1789531049641104441.json` (20 helpers; five CRLF-only differences) | `284e54c521105e7627bed733a4f6c49de9cc9afa78299b1a57a2e46616a17a63` |
