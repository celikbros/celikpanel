# Native WAL interruption

*September 15, 2026 · [Türkçe](NATIVE-WAL.tr.md) · D-025 / P0.3*

This fixture exercises an unchanged product migrator in a registered disposable
Linux guest. It observes a successful native WAL write, preserves the stopped
state, and permits one fault only after independent verification. Same-operation
native recovery and final data retention are separate acceptance checks. The
successful Debian S and fresh Arch T cases below passed scoped native rollback
and independent cut-time row-retention checks. Wider P0.3 acceptance remains open.

The affected [resilience contract](../../../docs/RESILIENCE-CONTRACT.md) is
P0.3, invariants 2–5: durable operation identity, truthful observations, recovery
from preserved evidence, and protection of owner state. This work adds fixture
code only. Product SQL, migration history, `recovery-material/v3`, database
admission/publication schemas and the supported 38→42 transition are unchanged.
It does not authorize updates of installed customer panels.

## Fixed product inputs

Each case starts with genuine Alpha64 binaries and all 38 verified migration
identities. The candidate is the same unpublished local artifact used by the
final [isolated-database experiment](ISOLATED-DATABASE.md), built with Go 1.26.5;
the Alpha81 archive label does not identify a published release.

| Input | Identity |
|---|---|
| Alpha64 source | `3ee8dac009c7e3db1d940f9b8be078e186693a80` |
| Alpha64 archive SHA256 | `c669e52a6686865e9fc0515e2839f4cfea7501730877e48654a35def63557979` |
| Alpha64 panel SHA256 | `c6e91e9e478d261c104397518fdae0145733832be7eeb3e53a123ef30c5d7460` |
| Candidate source | `cb3165456bb4ba4654dc19d51a5eafc13721a5fb` |
| Candidate tree | `336778673eb5bdfb19515a626e2e7c4bf08e3b5b` |
| Candidate archive SHA256 | `52dd34b435ba6d1fd1429aaadf875bf0cacae9f0cbeeb249ea9795e74a08698d` |
| Candidate panel SHA256 | `74e5b00e674d720bc40caea0e2fe46c04d8d8a9d9d5085496983a7c26cc121ad` |
| Selected recovery kit manifest SHA256 | `ed7c3eebe46d7ee5a1ad4b566ef4794daf295cb695151be40a4577b8fdc5a770` |

The host records the exact archive, source provenance and uploaded helper hashes
for each attempt. Helpers can differ between attempts; a later source correction
does not relabel an earlier observation. Private registration values, process
identities, captured databases and raw logs remain in the evidence store.
The final source also requires the fixed inventory of 17 helper names, bounded
no-follow evidence reads, explicit host mutation acknowledgement and a host-side
result consistent with the complete event chain. These final guard corrections
were made after the recorded S/T cuts and tested locally; they did not alter the
frozen guest helpers or retroactively change either experiment's evidence.

## Native fixture API and authority

`native_wal_trial.py` prepares, arms, starts and collects one registered guest
operation. Its prepare/arm/start mutations require explicit `--execute`; collection
is read-only. Separate immutable attempt records prevent an uncertain transport
result from starting another update or fault. The existing prebuilt fixture
validates the archive and real baseline; signed public Agent/UI admission is
outside this experiment.

`guest_native_wal_trial.py` uses a short-lived fixture gate before executing the
unchanged bootstrap updater. The gate's executable, PID/start time, systemd
invocation, cgroup and registered VM identity are pinned. A bounded private-file
permit releases it only after the tracer has attached and stopped its exact
initial task. The gate has a deadline and preserves the systemd invocation when
it executes the updater. It adds no marker to product code or SQL.

`waltrace/native_trace.py` exposes
`trace_native(registration, callbacks, timeout=...)`. Registration specifies one
exact `celikpanel-self-update-<operation>.service`, worker PID/start time, boot
identity and gate executable identity. There is no arbitrary-PID command line.
The callback boundary supplies current writer admission, gate release, independent
checkpoint verification and the separately authorized exact-unit fault.

The Linux amd64 tracer follows kernel fork/clone/exec events, including validated
nonleader exec. Only the admitted candidate `panel --migrate-only` process family
is switched to syscall tracing; other descendants retain lifecycle tracing.
Unexpected task identities, unsupported kernel states, missing admission, changed
files, timeouts and bounds produce `inconclusive`. Cleanup detaches only tasks
still held by this tracer with the same admitted identity. EXITKILL is deliberately
absent: tracer failure is not authority to kill an externally attached updater.
An unresolved cleanup is reported rather than treated as successful detachment.

## Physical WAL boundary

A positive observation requires a matched successful `pwrite64` entry/exit on
the admitted work database's exact WAL descriptor and inode. An earlier successful
write must already have supplied an actual complete header or committed-prefix
image. The new bytes must preserve that image and add a complete checksum-valid
noncommit frame, without a new commit marker; the successful write ends at the
observed appended boundary. The parser never invents an earlier prefix by slicing
a later WAL image.

The writer remains stopped at that exit while every admitted updater task is
stopped and checked against the exact cgroup inventory. The independent
`guest_wal_checkpoint.inspect(...)` reader rechecks the registered guest, worker,
native exclusive lock, snapshot, material, database admission, canonical Before,
candidate executable, credentials, environment, work files and WAL bytes. It
returns `celikpanel/lab-native-wal-checkpoint/v1` evidence; it neither attaches
nor sends signals.

Raw work DB/WAL/SHM files and the prior WAL image are copied through bounded
descriptors while the family remains stopped. No original file is opened through
SQLite. The controller repeats the full checkpoint after capture, and the tracer
rechecks task, admission and WAL identity before permitting the exact-unit SIGKILL.
`cut-sent` records the fault only; it does not mean recovery succeeded.

This establishes a **physical noncommit WAL write held at syscall exit**. SQLite's
Commit entry is unknown: the real migrator has no fixture transaction marker.
No syscall argument, return value, process memory, product SQL or migration is
modified. Checksum-valid noncommit bytes alone can survive rollback, as the
[parser tests](WAL-FIXTURE.md) demonstrate. Neither these bytes nor a successful
write establish fsync or power-loss durability.

## Populated data and terminal verification

The SQL fixture contains linked non-login user, subscription, domain, alias and
reservation records under `.example.test`. Its preparation verifies genuine
schema38, creates a private copy and installs it only in the separately registered
disposable baseline, with stopped coordinators and the native release lock.
An exchange retains the original inode and bytes; final named-path and descriptor
checks refuse an owner change instead of undoing it. This provisions no website,
mailbox or externally served domain.

Following a fault, the fixture observes systemd's native OnFailure recovery.
It does not invoke recovery, rollback or another update manually. Acceptance must
bind the same operation, snapshot and recovery invocation to the final binaries,
cleared markers, durable database records and service observations.

A separate post-terminal measurement may briefly freeze the exact already-active
coordinators to obtain a stable raw DB/WAL/SHM copy. PID/start time, invocation and
cgroup are checked around freeze/thaw; cleanup always attempts an authorized thaw
and reports any unconfirmed result. This measurement occurs after native recovery
and cannot count as part of automatic recovery. SQLite inspection runs only on
private copies after thaw. All 55 old tables, old columns, rowids and typed values
must be compared without excluded tables; new metrics rows are reported separately
from missing or changed old rows.

## Retained attempts and scoped acceptance

| Case | Actual fault boundary | Recovery observation | Final acceptance |
|---|---|---|---|
| S / Arch | `inconclusive`: lock-device check rejected; no SIGKILL | Tracer detached; update completed normally | No WAL-interruption acceptance |
| S / Debian 13 | Independently verified physical WAL write; exact-unit SIGKILL; cleanup confirmed | Same-snapshot native OnFailure rollback; schema38 restored | PASS within the recorded boundary; all cut-time rows retained |
| T / fresh Arch | Corrected observer; independently verified physical WAL write; exact-unit SIGKILL | Same-snapshot native OnFailure rollback; schema38 restored | PASS within the recorded boundary; all cut-time rows retained |

Both successful cuts extended a previously observed 32-byte WAL header to 4152 bytes:
one complete 4096-byte page frame with no commit frame. The successful `pwrite64`
returned 4096 bytes at offset 56, ending exactly at the observed EOF.

The Debian terminal collector reports `scoped-pass`: schema38 and all 55 old
tables remain, with zero missing or changed old rows and no excluded tables.
All 126 baseline rows, including the linked fixture records, are retained. Metrics
increased from 33 to 80 rows; those 47 additions mean global database equality is
not claimed. The admitted work DB/WAL/SHM evidence is unchanged. The terminal
outcome SHA256 is
`82d3b2bc1d7041dce8785ebb872070c493767db867bfa5dfd9e46a43ec37adb6`.

Independent inspection of private copies using the prior WAL and the complete
cut WAL found equal visible views: 55 tables, 153 rows, migrations 1–38 and two
domains. Original captured files remained unchanged. These views reflect the
later cut-time state, while the 126-row retention comparison uses the populated
baseline. Independent review report SHA256:
`b455fed20f82d775a3b1afdf66d848b425007858525e2e6da54728077abaa117`.

The fresh Arch terminal outcome also reports schema38 and all 55 old tables,
with all 97 baseline rows retained and zero old rows missing or changed. Metrics
grew from 4 to 33 rows. Its outcome SHA256 is
`4bd6d1b07315c8c648791cc167ca8a587382471c00bc62a6d0df5aaddde1117d`.
The independent Arch prior-WAL/full-cut-WAL replay also found equal visible
views, with 55 tables, 107 rows, migrations 1–38 and two domains; its report SHA256
is `004589c2c9d8ea0e101bed2c1570b19de17a93d700b7ec984621c2503334d70b`.

Baseline retention alone cannot prove retention of rows added before the later
fault. Separate host-only comparisons therefore matched every cut-time row to
the terminal database, including rowid and typed values in all 55 tables:

| Final-versus-cut evidence | S / Debian 13 | T / Arch |
|---|---|---|
| Rows at cut → terminal | 153 → 173 | 107 → 126 |
| Missing / changed cut-time rows | 0 / 0 | 0 / 0 |
| Metrics at cut → terminal | 60 → 80 (+20) | 14 → 33 (+19) |
| Retention report SHA256 | `2d65179c722bf7fda1fdbef3108b52255e77d06d8d08dcdc10edf93297d73a8e` | `a27ff5807ab3e465436c4356bdc2e0c6e9d499778aea6ad67a31f4161912c0c6` |

Every added row is a metrics sample after the last cut-time sample. Global
semantic equality is **DIFFERENT**, with no missing or changed old rows and no
table exclusions. Private backups independently rederived from terminal raw
DB/WAL agree with the verified terminal backups. Preserved original inputs did
not change.

The independent terminal audit passed 32 referenced artifact-hash checks and
21 crossbindings per system. Both recoveries restored matching running/disk
Alpha64 binaries, schema38, the original canonical Before inode and the complete
private work DB/WAL/SHM evidence; transaction markers cleared. The material's
16 data files, selected kit's 12 payload files plus manifest, 303 retained
candidate files and 94 panel assets were verified. The recorded owner unit
beforeimages were restored. Four authoritative DNS checks, preserved TLS and
HTTPS 200 are point-in-time checks. They do not prove uninterrupted availability.
Both coordinator thaws after the separate measurement completed successfully.
Registered guests were stopped through their guards; disks and evidence remain
preserved.

The first Debian terminal collection refused before its freeze/capture step:
two proof readers represented the same timestamps differently, as `{Sec,Nsec}`
and integer nanoseconds. A strict, lossless representation conversion allowed
the second collection to compare the same fields. The original refusal and both
collector attempts are preserved. This did not repeat the update, fault or
recovery; the later temporary freeze was solely the terminal measurement above.

The first Arch observer incorrectly equated the lock record's device with
`fstat().st_dev`. Btrfs reports a subvolume device through getattr, while the
kernel lock record reports the superblock device. The correction resolves the
exact FD's `mnt_id` through that PID's mountinfo and retains the separate canonical
path/FD device-and-inode check. It rereads the namespace, selected mount record,
fdinfo and FD metadata. It does not replace exact-FD proof with a global inode
search. This follows the kernel's [Btrfs getattr](https://raw.githubusercontent.com/torvalds/linux/v6.16/fs/btrfs/inode.c),
[lock reporting](https://raw.githubusercontent.com/torvalds/linux/v6.16/fs/locks.c),
[fdinfo](https://raw.githubusercontent.com/torvalds/linux/v6.16/fs/proc/fd.c) and
[mountinfo](https://raw.githubusercontent.com/torvalds/linux/v6.16/fs/proc_namespace.c)
implementations. The rejected S attempt is retained; T starts from a fresh baseline.

The final top-level offline root run under umask077 ran 444 tests: 443 passed and
one actual-binary-input test was explicitly skipped because its required inputs
were absent. The 27 nested tracer tests passed separately under UID65534. Targeted
root runs also passed 22 populated-baseline, 21 checkpoint and five bounded
evidence-reader tests. Tracer tests mock the kernel boundary;
checkpoint tests include a real local flock plus the distinct Btrfs device case;
baseline tests exercise private filesystem exchange and refusal/cleanup paths.
These local counts are not a remote CI result or native acceptance. The earlier
controlled-child experiment remains separate from this unchanged product path.
CI invokes `test_native_trace.py` explicitly from the nested `waltrace` directory;
the top-level fixture discovery does not substitute for these mock-kernel tests.
Three targeted root invocations cover populated-baseline, checkpoint-filesystem
and bounded-reader tests on disposable local files; they do not boot a VM.

This record does not close the wider P0.3 fault matrix, power-loss recovery,
unobserved filesystem/architecture cases, continuous service availability or
populated native hosting acceptance. The completed scope is one physical WAL-write
interruption and automatic rollback with complete cut-time SQL-row retention on
each recorded system. It does not establish all possible migration cut points.
