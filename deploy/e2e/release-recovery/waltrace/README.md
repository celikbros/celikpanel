# Controlled-child WAL syscall-stop feasibility

This document covers `trace_fixture.py` and its own child. The separate
`native_trace.py` adapter and its registered-VM acceptance are documented in
[Native WAL interruption](../NATIVE-WAL.md).

This is a disposable Linux x86_64 experiment, not a native CelikPanel update
acceptance result. It never attaches to a caller-supplied PID and never touches
an installed panel, a VM or the network.

`fixture_child.go` uses the repository's unchanged `modernc.org/sqlite` module.
A fixed explicit transaction inserts enough data with a small cache to force
an uncommitted WAL spill. This deliberately exercises a large statement; it
does not prove that the short product migration statements spill in the same way.
The fixture starts Go threads and a separately executed copy of itself, so the
tracer must handle actual clone/fork/vfork and exec events.

`trace_fixture.py` seizes only its own newly forked, gated child. Descendants
are admitted only from kernel ptrace events. The allowed ptrace requests are
SEIZE, INTERRUPT, SYSCALL, GETEVENTMSG and GET_SYSCALL_INFO, with EXITKILL. No
process memory, register value, syscall argument, return value or SQL is edited.
Ordinary signals retain their original delivery. Nonleader exec, unsupported
ABI, missing thread inventory or uncertain observations refuse the result.

A verified feasibility result requires all of these observations:

- A complete, checksum-valid committed baseline WAL captured while the child
  family is stopped, before the fixed fixture transaction starts writing.
- The fixed fixture reports successful BeginTx; its Commit call has not begun.
- A positive native pwrite64 syscall exit targets the same WAL device/inode and
  ends exactly at the new file end, beyond the baseline prefix.
- The independent `../wal_frames.py` reader confirms an unchanged committed
  prefix followed by at least one complete valid noncommit frame and no new
  commit frame. Merely finding a WAL or a valid old rollback remnant is insufficient.
- All admitted threads and descendants are stopped under this tracer; `/proc`
  task enumeration matches their pinned identities. WAL bytes and transaction
  markers still match at that final boundary.
- Only the created family is killed using pinned pidfds, and every admitted task
  is reaped. The original stopped DB, WAL and SHM retain their bytes and metadata.
- SQLite opens separate private copies only, verifies their integrity and sees
  exactly the single previously committed row.

Every failure is `inconclusive`, with evidence retained. A small transaction
which writes WAL only after entering Commit is deliberately inconclusive.
Tracer-death testing separately proves EXITKILL leaves no running admitted
child; it does not claim a killed tracer can itself reap the resulting zombies.

## Run locally

From a Linux checkout with Go 1.26.5 linux/amd64 already on PATH (or set
`CELIKPANEL_WALTRACE_GO` to that exact executable):

```sh
bash deploy/e2e/release-recovery/waltrace/run-local.sh
```

The runner exports the current committed tree into a unique private `/var/tmp`
directory and copies the named working-copy fixture files plus the parser into that export.
It records the explicit overlay list and their hashes, then builds the actual Go
child with `-mod=readonly` and CGO disabled, records its module/toolchain identity,
then runs the real spill observation and 15 unit/controlled-child tests. The
Go version is checked exactly; GOENV, toolchain downloads, workspace discovery
and module network access are disabled, so dependencies must already be cached. It
rechecks source hashes afterwards. Nothing is published or installed. Initial
and failed run directories are preserved, never relabeled as a later run.

Python-only discovery without `CELIKPANEL_WALTRACE_CHILD` explicitly skips the
four real-child tests. The runner supplies the freshly built binary and a new
private evidence directory, so the complete local run has no such skips.

## Remaining acceptance limits

This primitive is not yet connected to an admitted native updater or migrator.
The fixed fixture's transaction markers and large statement are controlled test
inputs, not evidence available from an unchanged product binary. Future native
integration must prove the exact operation, candidate, migrator identity,
work-directory authority, WAL inode and transaction boundary independently.
It must preserve actual product SQL and use the normal recovery path. A live
WAL pathname, process timing, a completed log line or this fixture's success may
not replace those proofs. Reboot/power-loss durability, recovery publication,
populated hosting and complete constitutional resilience remain out of scope.

The syscall-stop semantics are documented in [ptrace(2)](https://man7.org/linux/man-pages/man2/ptrace.2.html);
WAL byte rules come from SQLite's [file format](https://www.sqlite.org/fileformat2.html#walformat).
