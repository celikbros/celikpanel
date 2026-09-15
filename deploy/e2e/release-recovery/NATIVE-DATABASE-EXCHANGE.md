# Native database exchange interruption

*September 15, 2026 · [Türkçe](NATIVE-DATABASE-EXCHANGE.tr.md) · D-025 / P0.3*

This fixture observes the unchanged product publishing a migrated database in a
registered disposable Linux guest. It holds the actual atomic exchange at syscall
exit, before the publication receipt, then permits one independently verified
fault. **Arch native acceptance passed for the recorded scope; Debian remains inconclusive.**
A successful cut is not a recovery result.

The affected [resilience invariants](../../../docs/RESILIENCE-CONTRACT.md) are 2–5:
meaningful evidence, stopping at the actual unsafe boundary, a recovery contract
for every mutation, and recovery independent of a failed candidate. This adds
fixture code only; product SQL, migration identities, `recovery-material/v3` and
database admission/seal/publication/restoration schemas remain unchanged.

## Fixed inputs and authority

The baseline uses genuine Alpha64 binaries and verified schema38, with populated
private SQL fixtures. The candidate is the same unpublished Go 1.26.5 local
artifact as [isolated database migration](ISOLATED-DATABASE.md) and
[native WAL interruption](NATIVE-WAL.md); its archive label is not a release.

| Input | SHA256 or source identity |
|---|---|
| Alpha64 source | `3ee8dac009c7e3db1d940f9b8be078e186693a80` |
| Candidate source | `cb3165456bb4ba4654dc19d51a5eafc13721a5fb` |
| Candidate archive SHA256 | `52dd34b435ba6d1fd1429aaadf875bf0cacae9f0cbeeb249ea9795e74a08698d` |
| Selected recovery kit manifest SHA256 | `ed7c3eebe46d7ee5a1ad4b566ef4794daf295cb695151be40a4577b8fdc5a770` |

The fixed exchange profile pins all 19 uploaded helpers and uses its own immutable
attempt records. VM registration, archive identity, boot, worker PID/start time,
systemd invocation, cgroup and startup gate are verified before tracing. Helpers
are recorded per attempt; later changes cannot relabel earlier evidence. Private
registration values, process identities, captured databases and raw logs stay private.

## Actual publication boundary

`waltrace/exchange_trace.py` exposes
`trace_database_publication(registration, callbacks, timeout=900)` and reuses the
WAL tracer's process lifecycle. It follows kernel fork/clone/exec transitions;
only the selected root `bin/panel-checker --publish-update-database=<snapshot>`
family enters syscall tracing. The immutable expectation binds operation, token,
worker start, snapshot, exact checker path and digest. Native lock FD9 must belong
to that publisher under the same operation; candidate migration is a separate step.

On Linux amd64, the observer matches syscall316 `renameat2` with exactly flags2
(`RENAME_EXCHANGE`). At entry it stops the whole admitted cgroup and reads only
the two bounded `celikpanel.db\0` names from process memory. It never writes
memory, registers, arguments, results, product SQL or markers. The independent
`guest_exchange_checkpoint.inspect_entry(...)` verifies the selected kit, lock,
snapshot, material, admission, seal, publication intent, directory FDs and exact
canonical Before/staged After pair; dependent receipts must be absent.

Only the publishing TID resumes. A matched successful exit must return integer
zero while every other task stays stopped. Independent `inspect(...)` then proves
the same authority and task inventory, canonical After and retained Before in the
same build directory, and absence of `published.json` and restoration receipts.
Only rename-related file ctime is excluded from pair equality; bytes, inode,
ownership, modes and other recorded metadata remain bound. This point precedes
directory fsyncs and the receipt: atomic visibility is not power-loss durability.

The controller copies both exact files through bounded descriptors, rechecks the
pair and independently verifies the whole checkpoint again before exact-unit
SIGKILL. No captured original is opened with SQLite. The tracer has no arbitrary
PID interface or EXITKILL option. Unsupported states, changed identities, signals
interrupting this boundary, failed exchanges and incomplete cleanup remain
`inconclusive`; cleanup detaches only
its own still-matching traced tasks. `cut-sent` records the fault, not final success.

## Independent data and recovery proof

`database_exchange_rows.verify_pair(...)` inspects new private copies of the frozen
Before/After files. It verifies schema38→42 against pinned migration identities,
all old rowids and typed columns across all 55 old tables, without exclusions.
Only ledger additions39–42 are allowed in those projections. It also checks all
10 new tables: the exact legacy setup singleton, its valid dynamic timestamp,
empty remaining new tables, and `local`/empty-remote-ID defaults on every old domain.
The 9 SQL regressions include post-seed rows, rowid changes, BLOB/text confusion,
unexpected additions, altered defaults and unsafe inputs. These are component tests.

Native acceptance separately requires the exact operation/snapshot to reach its
supported terminal state through systemd OnFailure, without a manual recovery or
second update. Final artifacts, markers, database receipts, rows and service
observations must agree. A separate post-terminal coordinator freeze/copy/thaw,
if used for stable evidence, is measurement after recovery and must be reported;
it is not automatic recovery or proof of uninterrupted service.

## Running the registered fixture

Only use an already registered disposable baseline with verified populated seed
and archive. These are host commands, not installed-panel recovery instructions:

```bash
python3 deploy/e2e/release-recovery/native_database_exchange_trial.py --work-root "$work_root" --node "$node" --mode prepare --archive "$archive" --archive-sha256 "$archive_sha256" --execute
python3 deploy/e2e/release-recovery/native_database_exchange_trial.py --work-root "$work_root" --node "$node" --mode arm --execute
python3 deploy/e2e/release-recovery/native_database_exchange_trial.py --work-root "$work_root" --node "$node" --mode start --execute
python3 deploy/e2e/release-recovery/native_database_exchange_trial.py --work-root "$work_root" --node "$node" --mode collect
```

`node` is `arch` or `debian13`; the other variables identify the existing registered
work root and verified artifact. Prepare/arm/start require explicit `--execute`;
collection is read-only. Existing attempt records prevent a silent retry.

## Evidence and acceptance

| Evidence | Current result |
|---|---|
| Root offline suite | 495 tests: 494 PASS; 1 explicit missing-actual-binary-input skip |
| Nested tracer suite, root and unprivileged `nobody` | 45/45 PASS each, no skips; includes 27 unchanged WAL regressions |
| Real newly forked child syscall tests | Successful exchange swaps inodes before child return; real ENOENT refuses a positive proof |
| U / Debian 13 | Entry/exit, exact-unit cut and native recovery observed; final acceptance inconclusive: DNS observer unavailable, then registered VM unavailable |
| U / Arch | Exact exchange cut, native OnFailure rollback, schema38 restored; all 132 cut-time rows retained in 55 tables; four later metrics samples added |

All failed or inconclusive attempts must remain in the record. Local tests and
older WAL/isolated-DB results do not establish this new native boundary. Wider
P0.3 remains open: remaining checkpoint/recovery failures, reboot or power loss,
signed Agent/UI admission, and native hosted-workload/renewal behavior are outside
this acceptance. Populated SQL fixtures do not provision real hosted domains.
Installed panels still require their owner to initiate updates through the UI.


### Preserved U evidence (September 15)

Work root: `/var/tmp/cp-release-drill-20260915-u`. Arch operation
`19152b3b45032a69c9b02320896b6236` used snapshot
`20260915T135605Z-from-unknown-to-cb3165456bb4ba4654dc19d51a5eafc13721a5fb-4903c3e01a78b58dd2393e1f1ea037bd`.
All 19 helper hashes in the immutable intent match the reviewed source files.

The exit proof binds the real successful exchange, its entry proof and the exact
SIGKILL receipt. The same boot/operation journal and native terminal checkpoint
bind systemd OnFailure to automatic rollback. Original admission, seal and
publication records remain unchanged; `published.json` is absent. The restoration
intent requires an inverse exchange and the restoration receipt hashes that exact
intent. The original Before inode is canonical again; the migrated After file
remains retained with its bytes and metadata unchanged except rename ctime.
The selected recovery kit and running/disk Alpha64 binaries agree, release
transaction markers are cleared, and retained work files remain intact.

Separate host analysis opens only fresh private copies. The exchanged pair proves
schema38→42 with all 132 old rows, all 55 old tables, the four ledger additions and
ten prescribed new tables/defaults. The terminal database is schema38 and retains
all 132 cut-time rows without changed or missing rowids/typed values. Four later
metrics samples increase its row count to 136; global equality is **DIFFERENT**.
Reconstructing a private backup from the terminal raw DB/WAL independently agrees
with the saved verified backup. No original capture is opened with SQLite.

Four authoritative loopback A/SOA queries over UDP/TCP return the fixture values;
served and installed TLS fingerprints match and HTTPS returns 200. The separate
post-terminal measurement freezes/copies/thaws only the two panel coordinators;
both thaws were verified with unchanged process identities. These point checks
and measurement pauses do not prove uninterrupted service or native hosting.

| Preserved proof | SHA256 |
|---|---|
| Arch terminal outcome | `6cfb4d26684507e3a2173242bda0ebae17bf57e06e769941c2ac11d7ca09ab23` |
| Arch fresh host row review, `analysis/arch-exchange-rows-lmv6za_6/review.json` | `5264c9b3156bf01770bdad44105c6b98fbbfa9d9499a37df5d68e114024ac73a` |
| Host seal, `analysis/host-seal-1789495982920555018.json` (52 input bindings) | `4d658dff9afe5ede2a4fc450a789fea058b7424e36a0af2f350414a9dffdb66c` |

Debian operation `553062756ff312b11fdceb7c5f5df099` has verified native recovery
observations, but its first final collector stopped before any measurement freeze:
`/usr/bin/dig` was absent. The resulting DNS status is **unknown**, not evidence of
DNS failure. A later read-only diagnostic observed named active and confirmed the
missing client. A separate collector attempt refused at the registered-QEMU guard
before guest access because the VM was no longer running. No second update,
recovery or capture was started. Old observations, disks and evidence are retained;
no controlled shutdown or successful Debian final acceptance is claimed. A fresh
separately registered Debian trial must establish the observer prerequisites before
its fault and complete the final data/service proof. Both registered U QEMU PIDs
were absent at the final host seal; this is not a controlled reboot/power-loss test.

The final local recheck ran 495 top-level tests (494 PASS, one explicit missing
actual-binary-input skip). The two relevant nested tracer modules ran 45/45 under
both root and `nobody`, with no skips. A broader nested discovery additionally ran
60 tests (56 PASS, four existing controlled-child binary-input skips); it does not
replace the focused real exchange-child checks. These are local results, not CI.
