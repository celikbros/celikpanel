# WAL interruption prerequisites and populated SQL fixture

This fixture-only slice supports D-025 / P0.3 and invariants 2–5. It changes no
product migration, release protocol, recovery material, installed configuration
or license policy. `recovery-material/v3` and the supported schema 38→42 transition
remain unchanged. Native mid-migration WAL interruption and populated hosting
acceptance remain **open**.

## Evidence boundary

A checksum-valid WAL noncommit suffix can remain after SQLite rolls back. It is
not sufficient evidence of a currently open transaction. `wal_frames.py` checks
bounded header/frame bytes, both checksum byte orders, salts, exact prior prefix
and file-descriptor stability without opening the original files through SQLite.
Truncated, rewritten, reset or stale tails produce `unknown`; page contents are
hashed, never decoded into the result. Its fixture schema is
`celikpanel/lab-sqlite-wal-evidence/v1`.

`wal_migration_identity.py` only matches two supplied observations to an already
verified admission: exact operation cgroup, executable, command, restricted
environment, nonroot credentials, work directory and WAL inode. Its schema is
`celikpanel/lab-migration-writer-match/v1`. It authenticates no admission, reads no
process, attaches to nothing and authorizes no signal. A future native controller
must establish the kernel observations, stopped task inventory and causal write.

## Controlled writer experiment

[`waltrace/`](waltrace/) builds a disposable Go child using the repository's
unchanged modernc SQLite dependency. A small cache and explicit large transaction
force a spill in this artificial workload; production SQL is unchanged. The tracer
can seize only the gated child it just forked and kernel-reported descendants.
There is no arbitrary-PID mode. It observes the successful `pwrite64` exit,
preserves a committed baseline prefix, stops all admitted tasks and requires an
additional complete checksum-valid noncommit frame on the same inode before
selecting the interruption. Explicit child markers establish that this fixture
began a transaction and has not entered Commit; these markers are not present in
the real migrator and cannot be substituted for native proof.

The controlled process family is killed and reaped; EXITKILL covers tracer death.
A separate SQLite copy must see only the committed row. Original DB/WAL/SHM bytes
and metadata remain preserved. A completed small transaction, timeout, unsupported
kernel state or missing proof is `inconclusive`, never successful interruption.
The evidence schema is `celikpanel/lab-waltrace-feasibility/v1`.

The local experiment observed a successful 4096-byte write at offset 12416: a
12392-byte  / 3-frame committed prefix grew to 16512 bytes  / 4 frames, with one
noncommit frame and no appended commit marker. These are controlled workload
results, not native update/automatic rollback acceptance. CI runs this experiment
separately from the transport-mocked fixture contracts; it does not boot a VM.

## Populated private database

`populated_database.py` reads only a standalone private source through a bounded
descriptor. It creates a new owner-only directory and copy; it never seeds an
existing target or a product path. Source SHA, genuine Alpha64 commit, all 38
migration identities and exact schema are pinned before writing the new copy.
The fixture inserts one non-login user, one subscription, two related domains,
one alias and the six hostname reservations created by the original triggers.
Names are under `.example.test`. It provisions no DNS, website, mailbox, service,
engine receipt or license.

Seeding records the four expected AUTOINCREMENT counter adjustments separately.
After seeding, comparison includes **all 55 old tables**, every old column, rowid
and typed value, including `sqlite_sequence`; no table is excluded. Missing or
modified old rows refuse verification. New rows are reported as differences,
not whole-database equality. The schema42 check additionally requires 65 tables,
all 42 exact migration identities and `local` / empty connection defaults on the
populated domains. Evidence schemas are
`celikpanel/lab-populated-database-admission/v1` and
`celikpanel/lab-populated-database-manifest/v1`.

A separate local run used the actual released Alpha64 panel to initialize a
fresh private schema38 database, then the actual candidate panel to migrate the
populated copy to 42, under a nonroot account. All55 tables / 91 old rows and the
fixture relationships were retained. The old source bytes were unchanged.
This proves the selected binaries' SQL conversion on private copies, not
installed updater admission, snapshot publication or native workloads.

| Pinned input | Identity |
|---|---|
| Old source | `3ee8dac009c7e3db1d940f9b8be078e186693a80` |
| Old release archive SHA256 | `c669e52a6686865e9fc0515e2839f4cfea7501730877e48654a35def63557979` |
| Old panel SHA256 | `c6e91e9e478d261c104397518fdae0145733832be7eeb3e53a123ef30c5d7460` |
| Candidate source | `cb3165456bb4ba4654dc19d51a5eafc13721a5fb` |
| Candidate panel SHA256 | `74e5b00e674d720bc40caea0e2fe46c04d8d8a9d9d5085496983a7c26cc121ad` |

## Reproduce the bounded checks

The normal offline suite discovers the parser, writer matcher and populated-copy
checks. The actual-binary test explicitly skips when its two inputs are absent:

```bash
python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_*.py' -v
bash deploy/e2e/release-recovery/waltrace/run-local.sh
```

The tracer runner requires Go1.26.5 linux/amd64 and already available modules;
it installs no toolchain or packages. `CELIKPANEL_WALTRACE_GO` may select the
exact local toolchain. It archives the current Git source, records each working
fixture overlay and rechecks its hashes after execution. Run it as an ordinary
Linux user where parent/child ptrace is supported.

For the separate actual-binary component check, a nonroot operator supplies
`CELIKPANEL_POPULATED_OLD_PANEL` and `CELIKPANEL_POPULATED_CANDIDATE_PANEL` pointing
to local files matching the two fixed panel hashes above. The test refuses
installed product paths, copies verified executables to a new private directory
and runs only `--migrate-only` with a private data path. Run
`python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_populated_database_binaries.py' -v`.
Both successful and failed attempts are retained. None of these commands installs
or updates a panel.

## Validation record

The final offline suite ran 389 tests: 388 passed and the actual-binary test was
explicitly skipped because its binary inputs were absent. The controlled writer
passed 15 tests each under root and UID65534. Go 1.26.5 build/vet and the privileged
launch inventory passed. A separate final UID65534 run with the fixed actual
binaries passed two tests: FIFO replacement refusal and populated private-copy
38→42 conversion. Separate source-before/source-after records had identical SHA256:
`817d3e5279d80e1db33a08320f8ffd0c3dd4bf51741ac91909d52577bd0b8cb0`.
Final actual-binary result SHA256:
`08b49883a45b0c65c0cb66393c281a533d89d114f9718092be59304409b46c69`.
Private evidence paths and fixture row contents are not published.

## Remaining acceptance

The next native adapter must bind a registered disposable guest and exact updater
operation, admission and WAL write without modifying production SQL or injecting
syscall results. It must handle the real sudo/nonleader-exec lifecycle or refuse
it, prove a complete stopped process inventory, preserve failed DB/WAL/SHM and
observe same-operation automatic recovery. DDL may finish without a qualifying
write; that outcome stays inconclusive. Populated SQL records are not native
hosting evidence. Neither this slice nor the earlier Q/R initial-copy checkpoint
closes those acceptance items or the wider P0.3 fault matrix.

A subsequent [native WAL experiment](NATIVE-WAL.md) implements that separate adapter and records its bounded physical-write/automatic-rollback observations. It does not use the controlled child's Begin/Commit markers or extend this earlier feasibility result.
