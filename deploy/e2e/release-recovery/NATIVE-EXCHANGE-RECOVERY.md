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

A later [controlled-child reproduction and tracer correction](waltrace/EXIT-THREADS.md)
addresses the missing exiting sibling. It includes an old-source negative control
and preserves separately inconclusive traces; it does not replace AA or establish
changed-runner native acceptance.

## AB: changed runner completes the native Arch recovery reboot

The September 16 fresh attempt `/var/tmp/cp-release-drill-20260916-ab` used the
same unpublished `8d62896f0fb5be329090923ad074115941c0328c` candidate as AA, archive
`1c49df9ea65ec5758470b4ee347ed51cd2d00b7cf3f3f34253f58543d4a6cba8`.
The 20 uploaded fixture helpers were separately bound to
`b8c99e733920e7a16a4fa7878dd9074225c6efa8`, including the
[exit-thread and replaced-stop corrections](waltrace/EXIT-THREADS.md).
Five helper files differed from Git blobs only by recorded CRLF normalization.
Candidate product source and test-helper source are deliberately identified
separately. The candidate was not published or installed on an owner's server.

Operation `d893716344ff28f4c624f998b6c47aae` belonged to cell
`release-recovery__e121df9f6b0161e5`, Arch UUID
`28b32bb4-a452-57c4-b348-bbeeeaf6220c`. Genuine Alpha64/schema38, populated SQL
fixture and authoritative DNS were checked before the fault. The real 38→42
migration completed in the separate work database; the controller verified the
successful `renameat2` exchange before the publication receipt, cut the updater,
and observed native `OnFailure` rollback. It then reset only that registered VM
at rollback's `payload_restored` checkpoint.

The new boot observed **two deferred invocations and zero recovery failures**.
Each deferral recorded that the operating system was still transitioning and
that the native timer would retry the same operation. The invocation yielded;
a later native invocation verified the same snapshot and completed rollback.
No owner command restarted recovery, resumed an update or repaired guest state.
The same-operation evidence spans boot IDs
`4a97287a-0f6b-45e2-b550-b9dcb1f03a4e` and
`2bd4be18-9a3b-43c6-b260-b30b411009e9`. Completion occurred at
12:13:15 UTC; the deferrals preceded it at 12:11:58 and 12:12:29 UTC.

The restored schema38 runtime, installed/running artifact pair, native units,
web inventory, selected recovery foundation and cleared transaction markers
were verified. DNS A/SOA answers over UDP and TCP matched the pre-fault values;
HTTPS returned 200 with the expected served certificate. The host independently
rechecked the reset, checkpoint and journal chain from preserved evidence.
A separate post-terminal fixture freeze/copy/thaw collected database evidence;
it was measurement only, after automatic recovery and health checks.

Every cut-time row in all **55 tables / 102 rows** was retained with the same
rowid and typed column values: zero missing, zero changed. One new metrics row
had a timestamp later than the last cut-time metric. Whole-database equality is
therefore **different**, not asserted. All original evidence files were left
unchanged; SQLite analysis opened only new private copies. Both registered
QEMU guests were stopped afterward and their disks and evidence retained.

| AB preserved proof | SHA256 |
|---|---|
| Terminal outcome | `ddf251ff3fa59af43a5cc3b946f33b7a00de48c983b3fb55eae858acaf418699` |
| Terminal live proof | `4b2f5c7ad394f0f582e586254f0eeccf9cd067671f655d1e59af6ef71a1a6d29` |
| Exchange checkpoint | `c3e3ec85832d521723340e8e1ca12715926a741b0c06f3cf1d8942f4e3d0fd0a` |
| Reset submission with checkpoint binding | `a52c20166cbc0d97a4863e8efb70e3a11bee65592f8b97d18ee6f1032a1d2984` |
| Native causal chain | `0cde6a950bb76be5be27b0d80f0de1a8e32505b08f221a41504f2e4adfb1b712` |
| Host row review, `analysis/arch-exchange-rows-ncs4v6vf/review.json` | `8d28992965917c81408f8f9b7822cfc328212ecabd0b31fb3121637504326380` |
| Helper source binding, `analysis/helper-source-binding-1789560280611337958.json` | `47fe271923ca0432ea711a7c7a7d1b4eca6e0640a91bbbd8cc33cfe0f89a222f` |
| Host seal, `analysis/host-seal-1789560857086448038.json` (92 inputs) | `dbe051a4f61dc986c9a95456a880762708f0e5bfe1d6c0f9fa9db0519a8c1695` |

This establishes the changed runner's bounded Arch boot-deferral acceptance for
D-025/P0.1–P0.3. Schemas and the recovery protocol did not change. X/Z remain
previous-source evidence; AA remains inconclusive. The baseline installer had
updated the kernel package: the running kernel changed from `7.1.8-arch1-3` to
`7.2.6-arch2-1` after reset, with packages `linux 7.2.6.arch2-1` and
`systemd 261.3-1` unchanged across the fault. Firewall/VPN readiness is not claimed.
This is eventual recovery, not uninterrupted availability or storage power-loss
durability. Changed-runner Debian acceptance, signed candidate admission, the
remaining checkpoint/metadata matrix and full native workload/renewal
independence remain open. No installed Frankfurt or Boston panel was changed.

## AC: Debian preparation stopped before either fault

The September 16 fresh attempt `/var/tmp/cp-release-drill-20260916-ac`
completed its Alpha64 baseline, populated seed, DNS and coordinator health
checks, but stopped before candidate preparation or either fault. The observer
queried the absent `linux-image-amd64` metadata package in a Debian cloud image;
its running kernel was `6.12.105+deb13-cloud-amd64`. This was a test preparation
error, not a product update or recovery result. Both registered guests were
stopped and the disks, failed command output and preparation outcome retained.

The next fresh attempt queries the package for the actual running kernel.
There was no retry or recovery mutation in AC. It is not counted as native fault
acceptance and its failure remains separate from the subsequent result.

## AD: changed runner completes the native Debian recovery reboot

The September 16 fresh `/var/tmp/cp-release-drill-20260916-ad` trial used the
same candidate source `8d62896f0fb5be329090923ad074115941c0328c` and archive
`1c49df9ea65ec5758470b4ee347ed51cd2d00b7cf3f3f34253f58543d4a6cba8` as AB.
All 20 uploaded helpers were bound separately to
`b8c99e733920e7a16a4fa7878dd9074225c6efa8`; five differences were CRLF only.
The candidate remains an unpublished disposable-lab fixture.

Operation `a19adb95a00c684f9e11e45e8ff130c3`, cell
`release-recovery__d08881e596c231d1`, Debian UUID
`505c33bb-c657-5999-a1bd-733b4611b130` started from genuine Alpha64/schema38
with populated SQL and verified authoritative DNS. Real 38→42 migration in a
separate database and successful native exchange preceded the exact updater
kill, before the publication receipt. Native OnFailure began rollback; the
controller reset the registered VM once at `payload_restored`.

At 13:36:15 UTC the new boot deferred dispatch while systemd was still starting,
released the lock and preserved the pending operation. The native timer began
the next invocation at 13:36:47; rollback completed at 13:37:15. There was **one
boot deferral, zero postboot recovery failures, and no manual recovery**.
The boot IDs were `fbb4ab74-afc2-41d9-a43e-4393aca9ab59` and
`7d08370d-10a1-4f9e-8e6d-98e6395a90b0`.

Terminal checks verified old installed/running binaries, schema38, native units,
web files, recovery foundation, the inverse-exchange receipt chain and cleared
transaction markers. All **55 tables / 100 cut-time rows** retained identical
rowids and typed values: zero missing, zero changed. One later metrics row is
reported separately; whole-database equality is different. The preserved
migrated After database still proves the real 38→42 conversion. Analysis used
private copies; the post-terminal fixture freeze/copy/thaw was evidence
collection after automatic recovery, not an intervention to complete it.

DNS A/SOA over UDP/TCP matched the baseline and HTTPS returned 200 with the
expected certificate. Running kernel `6.12.105+deb13-cloud-amd64`, kernel package
`6.12.105-1` and systemd `257.13-1~deb13u1` were unchanged across reset. The host
independently rechecked 92 bound inputs, including the causal chain and all 20
helper pins. Both registered guests were stopped; disks and evidence remain.

| AD preserved proof | SHA256 |
|---|---|
| Terminal outcome | `8dfa3c2d75a693bb5d744d1a6a44152c7526bf51b1dc82fe74df7532996c6955` |
| Terminal live proof | `d46192f719d25b443cfdf9d3497bd91f70f19fd16d865518659ff58aecd28aa2` |
| Exchange checkpoint | `377293f3b15e4f2116dc09538cf078dafa8b6e28ea97036fe46b11c83dfa7843` |
| Reset submission with checkpoint binding | `bb5cc7a509e92bcf9d4fb164a931646a5af5ac46b90e28dd0fac450fc94ee86c` |
| Native causal chain | `cc570ac8330db3408284584e5354719c1171a4056d3c3987d88c440e96d97525` |
| Host row review | `46de9ec4e8f9b1a0ab7bab07bfcec416afcfc1ed3a0fc3d9c579c038efa81640` |
| Helper source binding | `5141a2e581944b727d6fc88db78de2a503e233216f7c5ff3171a1fb4eaecf6fd` |
| Host seal (92 inputs) | `4ca0ebf48bcc5671ec8fdc48486cd30ef6534b0fcf20e63afbb32cab1911b170` |

This closes the changed runner's Debian acceptance at this two-fault boundary
for D-025 invariants 2–5 / P0.1–P0.3. No product, persisted schema or protocol
changed in this documentation slice. AB and AD now cover Arch and Debian for
this boundary; X/Z failures, AA's inconclusive trial and AC's preparation error
remain recorded. Browser waiting guidance, signed candidate admission, the full
checkpoint/metadata matrix and native workload/renewal independence remain open.
A QEMU reset does not establish storage power-loss durability or uninterrupted
availability. No installed Frankfurt or Boston panel was changed.
