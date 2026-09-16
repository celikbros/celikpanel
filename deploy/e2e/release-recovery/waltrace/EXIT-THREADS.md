# Exit-thread reconciliation in the native test tracer

*September 16, 2026 · [Türkçe](EXIT-THREADS.tr.md)*

This is test-harness work for D-025/P0.1 and P0.3 evidence. It changes no
installed-panel lifecycle, persisted product schema, recovery protocol, database,
license policy or fault-controller authority. The changed boot-readiness runner
still needs a fresh native trial; this record does not close that acceptance.

## Reproduction and bounded correction

The [AA attempt](../NATIVE-EXCHANGE-RECOVERY.md) stopped before either requested
fault: an exiting Go checker had a zombie leader and a traced, stopped sibling
missing from the tracer's admitted TIDs. Polling only admitted TIDs could never
consume the sibling's stop, so the leader could not finish exiting.

A private Go child reproduced the same shape locally. More directly, the old
`native_trace.py` from `ee70fce` failed the new two-goroutine child test on its
first trial, using the same binary as the corrected run. It reported
`trace-detach-incomplete-controller-watchdog-required`. Its original trace and
nonzero test result remain in `/var/tmp/celikpanel-exit-before.tDhmIWNF`.
Only the fixture's pinned child was killed/reaped by the fixture finalizer;
this cleanup is not attributed to the native tracer.

The native adapter now slices its existing wait deadline into 100 ms probes.
It can reconcile a sibling only after consuming an admitted leader's EXIT event
and verifying that leader is a zombie. Both identities are sampled again;
TGID, start time, tracer ownership and exact registered cgroup must agree. The
new record is `exit-thread-admitted`, with kernel thread-group authority; it is
not a fabricated CLONE event or parent relationship.

Such a sibling is admitted solely to consume its kernel stop/exit. It cannot
produce syscall, exec or fork evidence. A pending reconciled sibling blocks the
final all-task freeze/cut proof until a real wait exit removes it. No new process
is discovered or seized by inventory. Native code never uses `waitpid(-1)`,
changes process memory/registers, enables EXITKILL or signals a kill. Existing
controller authorization and total trace/cleanup deadlines are unchanged.

## Evidence and remaining uncertainty

On WSL2 Linux `6.18.33.2-microsoft-standard-WSL2`, pinned Go 1.26.5 built the
fixture `a60954608232edca0aaa3ae43117eb72fbca2ae6084edce996d9a5b8001f77d7`.
The isolated run `/var/tmp/celikpanel-exit-trace.BSsii90t` retained all 50 traces:
47 drained to real wait exits, including 42 reconciled threads. Three traces
returned the separate `task-id` uncertainty and detached without a cut; they
are **inconclusive**, not verified exit-race completions. The kernel event-message
case remains open. No run invoked a fault-controller cut.

| Proof | SHA256 |
|---|---|
| `evidence/summary.json` | `7fad1c2d8d8d116e46b48316a2d6b80c1646f649f7d09eb9f0a7f43908faddbc` |
| `source.sha256` (exact working-source copy) | `c0136a2045d988507ebfedc9850a0d2f29d846e4e6f09067714213f9a730df75` |
| `tests.log` | `88cdd5e8acc1d17042c6a3cac586d0b5cd8f8fdb03b3d0a212c76ecb927eae90` |

Earlier exploratory runs remain retained, including an overactive 32-goroutine
fixture that returned `task-id`, a minimal child that never exercised the race,
and a two-goroutine run with 45 completions / five inconclusive traces. These
were not relabeled as the isolated run above. The exact kernel event ordering
of AA is still not established by a local reproduction.

The Python native adapter contracts cover missing EXIT proof, live/foreign
leaders, changed PID/start time, wrong tracer/cgroup, non-stopped threads,
non-exit events, denied cut proof, unchanged deadline and cleanup without kill.
The tracer suite ran 73 tests (68 passed, five optional kernel tests skipped);
the separate recovery harness suite ran 517 tests (516 passed, one explicit
actual-binary test skipped). The opt-in kernel test above ran separately.

Run the isolated fixture with an already installed exact Go toolchain:

```sh
CELIKPANEL_WALTRACE_GO=/path/to/go1.26.5/bin/go \
  bash deploy/e2e/release-recovery/waltrace/run-exit-local.sh
```

The runner creates a fresh private directory, records source/toolchain/binary
hashes, disables toolchain/module downloads, checks source hashes afterward and
retains failed evidence. The test accepts only its own newly forked gated child;
its kernel adapter substitutes the fixture cgroup for native VM registration.
No installed panel, VM, update or network is touched. The stochastic test must
actually reconcile a thread; a run that never exercises the race cannot pass.
Full native exchange/reboot acceptance and the remaining P0 matrix stay open.
