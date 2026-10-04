# Recovery runtime promotion native acceptance

*September 14–15, 2026 · [Türkçe](RUNTIME-PROMOTION.tr.md) · D-025 / P0.3*

The [promotion contract](../../../docs/RECOVERY-RUNTIME-PROMOTION.md) addresses
replacement of an already selected recovery kit. These trials use registered
disposable QEMU guests only. Host paths, guest identities, operation IDs and raw
journals remain private evidence. No customer panel is updated by the fixture.

## Source and predecessor

The application baseline is genuine signed Alpha75. The fixture seeds native
BIND through that installed Agent before enrolling the preceding recovery kit.
It invokes the predecessor's actual `enroll-runtime` command under the native
release lock; it does not write a synthetic selector or promotion receipt.
This is predecessor enrollment on the old application, **not a complete successful
previous application update**.

Predecessor source: `bc0ebc051de0fc542c5f90e7c0bbb519a5c0f111`, tree
`76492d58a7b9c1b86e2282f0e6d090127b71bd5a`; archive SHA-256
`4d5bde282e7d2342a4b42930f7749104c658b933cf8908da71db3279cf7542c1`.

Initial promotion candidate (K/L) source: `b34ab808f99d1d611d90d5665c10e88c724fa494`, tree
`f4a993ae42ff13f6a1b7bec79e7e930acb5fd56b`; archive SHA-256
`7863d227739d3849fca7b85dbb9a71e2b2764031501d26f4c485fd42f6e1a49d`.
The initial promotion fixture source is `58c5cda278ac7d2b143650d96c8a06343e982afe`.
The corrected candidate for fresh M/N acceptance is
`ad782d803b1ba9531b1a3e92627ebdd748fe7c89`, tree
`2314b935e2f498184bb9a19e3d7c023d72934770`; archive SHA-256
`bf7da9a4ca9a779b3e79a9f569bf085cff5b37d7a050dd6465ee2be84005489f`.
These archives are **unsigned local builds**, not signed Agent admission.

## Distinct acceptance boundaries

1. At new-launcher/old-selector publication, prove the actual native worker,
   exclusive lock, empty transaction markers, complete old/new kits and signed
   application baseline before killing the exact worker. A missed window is
   inconclusive; the fixture cannot fabricate the durable record or retry the
   same admission.
2. Invoke a read-only material-capability command through the real fixed launcher.
   The old selected executable must pass its own executable proof, and selection
   must remain unchanged. This is executable dispatch, not snapshot restoration.
3. Invoke the explicit owner `recover` command to finish the same retained
   promotion and verify the unchanged old application. This enrollment fixture
   does not establish automatic completion through the older application foundation.
4. In a separate fresh guest and admission, let promotion finish, then fail the
   application update and prove actual automatic snapshot restoration with native
   services. The earlier preflight-only interruption cannot substitute for this.

## Result record

Trial K observed the new-launcher/old-selector window on both Arch and Debian 13.
The watcher froze the exact updater but then tried to inspect the former staging
path. The real prebuilt bootstrap had already moved that tree into its retained
release directory. The fixture recorded `FileNotFoundError`, sent **no SIGKILL**,
and thawed the exact worker. Collection also exposed a fixture observation/summary
filename collision; raw observation and journal evidence was preserved.

Both updaters then completed forward normally. The candidate binaries on disk and
in running processes matched; both services were active. The new kit was selected
and the complete predecessor retained. Four DNS probes and bootstrap TLS matched
the baseline. Whole-database comparison remained **DIFFERENT**, only in metrics.
These are normal promotion/forward completion results. They do not establish the
intended interruption or automatic rollback.

Fixture commit `7dcf5c0242ec37fc2fd5b494dd9a9af555b8cf43` verifies exactly one retained
release under the fixed root with the expected name and full manifest/inventory;
there is no staging fallback. Observation and summary files now have distinct
names. All 255 Python fixture tests passed. The production candidate is unchanged.
The later interruption and post-promotion rollback trials are recorded below.

Independent host-only review rechecked all 43 Arch and 44 Debian sealed files.
No guest action was taken by that review. K guests were stopped through their
registered guards; disks and raw evidence remain retained. The metrics change
was 8→18 rows on Arch and 9→19 on Debian; old-row retention was not separately
proved here, so whole-database preservation is not inferred.

| K report | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Sealed index | `7dcd8caef5e01ca000673764584d76d55d725eaee9123cf1cb94b2e2adf56d19` | `afd53a624c074d9841847463d88f716c4725ba469186b7c74ae2b1a3b3ce972e` |
| Native outcome | `26bddf8585fe1f1640d0472cbb7e585781a2cf9da6c5bbf16a264f9b422c91f2` | `4bd063eb1e988ad65512e0761ba2b1be1a7451f2bc3b081e87912b2827f88005` |

## Trial L: real interruption exposed an owner-entry defect

With the corrected fixture, both systems reached and proved the exact mixed pair,
then received one updater SIGKILL. The read-only command through the new fixed
launcher successfully executed the old selected reader without changing the pair.
The subsequent explicit owner `recover` command failed on both systems with exit 3
and `runtime_unsafe_metadata`. The selector stayed on the predecessor; current
status remained `known/launcher_published`, no commit receipt appeared, and the
transaction root contained only its canonical lock. Both kits and the original
application processes remained intact. No second update or recovery was started.

This is a **failed owner-resume acceptance**, not a successful recovery. The source
CI run [34895863261](https://github.com/celikbros/celikpanel/actions/runs/34895863261)
passed all 21 jobs on `7dcf5c0242ec37fc2fd5b494dd9a9af555b8cf43`; that result did not
cover the actual owner process's descriptor layout. A local Go 1.26.5 process
reproduced FD 9 being retained by the Go event poller after read-only path proof.
The same read-only diagnostic in both native guests confirmed FD 9 was
`anon_inode:[eventpoll]` after entry proof. Neither that diagnostic nor collection
changed the pair or retried recovery.

Commit `ad782d8` corrects the owner path to carry its actually acquired canonical
lock descriptor; unrelated FD 9 is preserved. The admitted updater's inherited
FD 9 contract remains separate. Three regressions first failed on the old code;
after the correction all 34 root scenarios, vet and both recovery packages' race
checks passed. Fresh native acceptance for this changed candidate is recorded in M and N below.

L preserves the failed acceptance. Both guests were stopped through their guards;
48 Arch and 49 Debian evidence files were sealed and independently rehashed. The
fault watcher retained `cleanup=thaw-unconfirmed`; subsequent native observation
proved the exact killed unit had `Result=signal`, exit signal 9 and `MainPID=0`.
This is not K's successful thaw. Post-cut DNS and TLS probes matched; this is not
continuous availability measurement. Whole-database comparison differed only in
metrics (Arch 8→11, Debian 7→12 rows), without a separate old-row retention claim.

| L report | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Sealed index | `f47d5bf4527eb47036667c25b50aa55c02d41084eda3b51677940e175c7f8215` | `9cd6c8aa851f275c2aacff25ca370979acd06dcb546248292e658a38ce9524e2` |
| Native outcome | `440016113762f9b40428320ecfa4d78cbf0f2726f49101ac3f46c01f17ea7d53` | `b4f7a48895f6b91842072db5d28b11446843cdcc82bff1c7722c323c5018f219` |
| Owner-entry FD observation | `927759aebdb011b4a3867be5af0da7e8138ed740e26d4739e721146d41d6d922` | `4df9a416bfe38742b5b9e4c8fb1b25f4e99c5ba7b5ecf85b75831765a473f6b6` |

## Trial M: corrected owner recovery after a real promotion interruption

Fresh Arch and Debian 13 guests used the corrected `ad782d8` candidate. Both
proved the new-launcher/old-selector boundary and killed the exact updater once.
The real fixed launcher then dispatched the old selected material-capability
reader with exit 0 and an unchanged pair. One explicit owner `recover` completed
with exit 0 on each system.

Separate native observations proved `runtime-status=known/committed`, a canonical
commit receipt bound to the original intent, the new launcher/selector identities,
and the complete old and new 13-file kits. The selected target was
`68e02c865f597ad030b95318fe58ca1782e7b607edbaae6f3ae7c0c1e5b9e4a3`.
All 292 retained candidate files were verified. The original Alpha75 process IDs,
disk/running hashes and owner unit files remained unchanged. Post-recovery DNS
queries, served TLS and panel web asset tree observations matched. Whole-database comparison
remained **DIFFERENT** only in metrics (Arch 10→17, Debian 8→20 rows), without a separate baseline
row-retention claim or table exclusion.
The watcher retained `cleanup=thaw-unconfirmed`; the killed worker was observed
dead. It was not restarted by the test.

This establishes scoped **explicit owner completion** after launcher publication,
not automatic completion through the old application's foundation. Independent
host-only review rehashed all 56 Arch and 57 Debian sealed files and verified the operation chain. Both guests were stopped through their registered
guards and their evidence retained. The separate post-promotion application
rollback result is recorded below.

| M report | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Sealed index | `5bac0d4b7266ec2ad5aec5044322846a41f8cdf5d88eecc3d20accb451790641` | `7abb5b6fb499873390a661bf1a8b66a3f0462e722e47edb770086783fe2cd4a5` |
| Native outcome | `ddd99b67ecd4225a115490152fa4b24be30d48110d29f706c7183f314f9cca99` | `744ee242aaa6b77e6a3c8d13c314cbd39d901a870fac899a9aee2688ec8b6963` |
| Completed promotion proof | `f15627c6d5674a6e9b8d5a84af39090156ca8cc9f94adf5493db28e9705c828f` | `af667300868d79352082c3ff63980e307a7974e153d7efb3a4144610587978f7` |

## Trial N: automatic application rollback after kit promotion

Separate fresh Arch and Debian 13 guests use the same predecessor and corrected
candidate. The standard `candidate-installed` watcher verifies the real snapshot
and installed candidate before sending SIGKILL only to the exact updater group.
The fixture does not invoke `recover`, `rollback.sh` or `systemctl start` for the
recovery unit. Its local worker has the same
`OnFailure=celikpanel-release-recovery.service` relationship used by the product's
Agent launch path; this still does not test signed Agent admission. No additional
recovery or data-loss fault is selected.

Both systems reached the actual candidate-installed checkpoint, then the exact
worker exited on signal 9. Native journals tie its `OnFailure` trigger to the
recovery invocation that verified the same snapshot and reported `Rollback
complete`. Earlier timer/lock-busy observations are separate and do not count as
that terminal recovery. No owner recovery or manual rollback was invoked.

Final read-only proof found the original Alpha75 disk/running hashes and active
services restored, with no active transaction. The new kit remained selected and
committed while the predecessor was retained. The complete 292-file candidate,
129-file snapshot and 15-file independent material set passed verification.
Owner unit files, authoritative DNS probes and served TLS matched. Loopback panel HTTPS
returned 200; the panel web asset tree (`/opt/celikpanel/web`) was checked
separately. Hosted website document roots and HTTP responses were not tested.

All 65 database tables were included. Whole-database comparison remained
**DIFFERENT** only in `metrics_samples`, with no table exclusion. The additional
read-only row comparison on both guests found all 11 snapshot rows retained with
identical rowid and typed values, zero missing or changed rows, and eight new
rows after capture (19 live rows). The independent review verifies the pinned
comparison program and its sealed aggregate; it cannot reconstruct individual
rows from that aggregate because raw database rows were not exported.

This establishes scoped automatic snapshot rollback after promotion through the
native failure path. It does not establish reboot recovery or an additional
recovery interruption/candidate-data-loss combination in this trial.

Independent host-only review rehashed all 42 Arch and 43 Debian sealed files
and passed 24 scope checks on each system, including the exact recovery
invocation and snapshot/kit/material links. Both guests were stopped through
their registered guards; disks and evidence remain retained.

| N report | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Sealed index | `a1962bb9ab9f463d0b2471ffa045c0bab27dfd4c3834da01fa3c1e5db745d686` | `df8e361079e6162853ef4cf5fa5b407a85e47b5c208d3a5bdcea134d775bb4c5` |
| Native outcome | `ff7f060c2fad91340e3fbc4b8df15cfa5065082cd17092b8609e33120c85fbd1` | `1f32e03bb01ab1290e8dfdde0247d13ebb85796e2aaea88962d2adb84a079738` |
| Metrics row-retention observation | `485d8f1a35e2dd11cdc22114771d84696c76a722f257dc519087872bf8325e43` | `2fa6db27f038f6832fe004ef76e03d2085a368cb6fa5bc6505d7a66fce5d5a2b` |

The corrected source `ad782d803b1ba9531b1a3e92627ebdd748fe7c89` passed the complete
[CI run 34898469236](https://github.com/celikbros/celikpanel/actions/runs/34898469236).
This accompanies the native evidence; it does not replace it.

## Remaining scope

P0.3 remains partial. Full checkpoint combinations, signed Agent admission,
new-token historical rollback, metadata transitions, evidence cleanup and the
independent workload renewal/boot matrix remain open. Unit tests which remove the
entire candidate source or retired predecessor are distinct evidence; a native
trial does not inherit that coverage unless it performs the same fault.
