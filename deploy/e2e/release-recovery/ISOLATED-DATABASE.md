# Native isolated database migration

*September 15, 2026 · [Türkçe](ISOLATED-DATABASE.tr.md) · D-025 / P0.3*

This record accompanies the [source contract](../../../docs/RECOVERY-ISOLATED-DATABASE.md).
It separates the first Q candidate from the subsequent parent-layout admission
correction. The two do not share an unqualified native acceptance claim.
No customer server was changed by these experiments.

## Genuine old state and measured boundaries

Each fresh Arch and Debian 13 guest uses the fixed, published Alpha64 source
`3ee8dac009c7e3db1d940f9b8be078e186693a80`. The installed and running Agent/Panel
hashes and all 38 migration identities are checked before admission. The native
Agent prepares BIND records; the fixture records authoritative A/SOA over UDP/TCP,
bootstrap TLS, loopback HTTPS and all 55 SQLite tables including `sqlite_sequence`.
Native coordinator-unit beforeimages include the fixture owner's recorded edits.

The unsigned local candidate enters through the registered disposable prebuilt
fixture, not signed public Agent/UI admission. VM UUID/nonce/DMI, host process,
worker invocation/PID/start time/cgroup, executable and native lock guards bind
one admitted update to each guest. The test does not start recovery or run an
owner rollback command. Actual native OnFailure recovery must complete the same
operation.

Arch targets `candidate-installed`. Its supplementary frozen DB observation must
state the actual database state. Exact initial images do not prove that SQLite
never began a transaction. Debian targets `completion-database-verified`; only
after independent database-ready proof does it quarantine the fixed three
retained files (`rollback.sh`, `bin/agent`, `web/dist/index.html`) and kill the
exact updater. Originals remain in the fixture quarantine. A temporary loopback
port hold widens that observation window; it is not recovery evidence.

## Q: initial source

Product source `0610b239a7bb976874a2c347099bf88e779237d1`, tree
`5732227c911fc3469f6e9ae0d8068f156a2a41c1`; fixture source
`adf7a1216a33d5ad12dc7f800b95ee88cee67b1f`. Candidate archive SHA-256:
`05342cf1274b09184b4bef6535fb91849fbfecb4bb1b8e2555ec0ed6d311e45f`.
Recovery kit manifest SHA-256:
`2f8981fa0db5688811757f5363cb860e17317f5c7b95b7a5c8563a8ee17f7def`.

Both measured fault chains reached their intended boundaries. Arch's frozen
observation found canonical Before and work Initial unchanged, schema38 with
all 38 identities, only `admission.json`, and no later records, build directories
or sidecars. It is explicitly `PASS-state-only`, not a mid-migration WAL cut.
Debian reached completion.pending with material v3 and the selected independent
whole-schema/history checker passing before the three-file quarantine and kill.

The first terminal reads found automatic old-Alpha64 rollback on Arch and
forward completion of the candidate on Debian. Both showed active matching
running/disk binaries, cleared transaction markers and HTTPS 200. Arch retained
schema38/55 tables; Debian reached schema42/65 tables. Each complete snapshot
contains 124 files and material data contains 16 files.

Final collection and independent review passed 25 Arch / 29 Debian scoped
checks, then rehashed every indexed file (87 / 70). Separate database/material
checks passed 27 / 43. Fault records bind the frozen and killed worker to native
OnFailure and the same terminal operation; Debian's earlier two-event collection
is a prefix of its final eight events, not a second fault.

| Q evidence | Arch | Debian 13 |
|---|---|---|
| Index SHA-256 | `ba146e922e374918aa909d6b0480b93c02c582532af871d7e5a36f65b5b7a6db` | `9f7cca4dd723f31140f9302bc3d811e3db6da8007144920cb43da76c98acbbdf` |
| Outcome SHA-256 | `c6148c5d5059d7fb2d2d0abd0ad97810ceb047410580a2fe82a07d3ffb420ee0` | `f51fa4ab51c3efc6ad21e03644331bb50706e92d3550d01d86d98ca6558740b6` |

Arch retained the original canonical inode, exact initial work image and admission
record. Debian's raw admission → seal → publication → published hash chain,
retained original Before inode/hash/mtime and canonical After inode were checked.
No active-only verifier was invoked after terminal completion by fabricating a
lock or transaction marker. The actual preterminal checks, durable records and
selected read-only whole-schema/history/queue checker provide distinct evidence.

All 55 old tables were compared using old-column, rowid and typed-value
projections, with `excluded_tables=[]`: no old row was missing or changed. The
global database result is **DIFFERENT** on both systems. Arch metrics grew from
99 to 118 rows, Debian from 101 to 120; all 19 additional samples on each system
are later than the snapshot. Debian also has ten expected new tables, two new
domain columns and exact migrations 39–42; identities and timestamps 1–38 remain
unchanged. Domains were empty, so this does not prove populated-domain conversion.
The native collector exports bounded aggregates, not raw user rows for host replay.

Arch restored the recorded owner unit beforeimages. Debian uses the new candidate
units and retains the old edited units in the snapshot; live unit equality is not
claimed. Terminal native DNS, TLS and HTTPS checks pass, not continuous service
availability. A post-kill thaw refusal and an unavailable status observation are
retained rather than interpreted as successful observations.

The first host timestamp invocation and the first host seal script failed before
completing their observations. Corrected host-only collectors preserve both
failures; they did not start another guest update, fault or recovery. Q guests
were stopped through their registered guards after sealing; disks and evidence
remain in the retained private Q evidence store.

## Parent-layout admission correction

Source review found that the new normal probe and Before capture accepted
root-owned or mode0700 parents, while the installer and native service lifecycle
expect panel:panel0750. The existing snapshot producer can also normalize earlier
quarantine layouts. Accepting a later owner edit as a new Before could therefore
permit normalization followed by a recovery identity mismatch.

Commit `cb3165456bb4ba4654dc19d51a5eafc13721a5fb` rejects unsupported parent
layout in new preflight/capture, preserves metadata and supplies typed owner
guidance. Historical material readers and quarantine recovery keep their existing
contract. Eight reproductions failed before the correction. Afterwards, 398 tests
passed both normally and with race detection; two existing filesystem-dependent
xattr cases were skipped in each run. Vet passed. The cases include historical
material reads and concurrent owner changes.

Q's ordinary panel:panel0750 outcome is evidence for its recorded source; it does
not prove the corrected candidate. R below records the corrected source separately.

## R: final source on fresh guests

Candidate source is `cb3165456bb4ba4654dc19d51a5eafc13721a5fb`, tree
`336778673eb5bdfb19515a626e2e7c4bf08e3b5b`; fixture is
`1919140c4350085b3b81bd8023dfc0e034fa2ad5`. The fixed Go 1.26.5 build includes
six binaries and the actual frontend build; all 303 packaged files and 192 static
Git files were verified. Archive SHA-256:
`52dd34b435ba6d1fd1429aaadf875bf0cacae9f0cbeeb249ea9795e74a08698d`.
Selected recovery kit manifest:
`ed7c3eebe46d7ee5a1ad4b566ef4794daf295cb695151be40a4577b8fdc5a770`.
The Alpha81 archive label is an unpublished local test artifact, not a release.

| R identity | Arch | Debian 13 |
|---|---|---|
| Sealed files | 62 | 63 |
| Index SHA-256 | `976ad91c5d96f6c6e0103fa3717056075bd45a94bebed9da44fcd7f80ba460c6` | `89283f97a8cd8883917e12d95e6b5d297c2a119c9ab6a02f10582fb2edbe13e0` |
| Outcome SHA-256 | `c6a4bd65b9e833ee10ebbd54eeb1ea8c8fdbd3c27c71e2b0214edb0b9b4d31ba` | `be74c999f8ecac73a2254fc9d2f66151cbbb739ba5c7e07042cfd06ec29cd0e4` |

Fresh registered guests each passed 13 genuine Alpha64/schema38 baseline checks.
One update and one actual worker SIGKILL were admitted per guest. Arch's six-event
chain reaches automatic rollback; Debian's eight-event chain reaches exact-snapshot
forward completion. Each terminal recovery invocation is bound in the sealed
private evidence. Later timer no-op
invocations are separate. No manual recovery or second update was invoked.

All 25 Arch / 29 Debian scoped outcome checks passed. Arch retained old running
and disk binaries, the original canonical inode, exact initial work image and
schema38/55 tables, with only admission evidence. The frozen database observation
again proves initial state only. Debian independently proved schema38→42 before
losing the fixed three retained files and updater. It retained the complete raw
publication receipt chain, the original Before counterpart and the new canonical
inode, schema42/65 tables, and 300 of 303 retained candidate files. Each complete
snapshot has 124 files; each material data directory has 16.

All 55 old-table projections retain every old row without changes or exclusions.
Global comparison remains **DIFFERENT**: Arch metrics 15→19 and Debian 16→20,
with all four additions after snapshot capture. Debian's ten new tables, two
domain columns and migrations 39–42 are the intended additional differences.
The empty-domain and aggregate-only evidence limits remain unchanged.

Final DNS, served TLS, HTTPS 200, selected read-only checker and marker-absence
checks passed. Arch restores owner unit beforeimages; Debian keeps candidate
units and the original owner unit bytes in the snapshot. Neither proves
continuous availability or arbitrary owner metadata preservation. Both killed
worker records retain `thaw_exit=1`; the unavailable status message is also
retained. R needed no failed collector or repeated observation to reach its
sealed result.

Two independent host reviews rehashed every one of the 62/63 sealed files and
verified the fault/OnFailure chains. The separate DB/material/receipt review
passed 28/44 additional checks. Registered guards stopped both R guests after
verification; disks and private evidence remain in the retained R evidence store.
This is scoped acceptance for the supported normal parent layout and these two
fault boundaries, not closure of P0.3.

## Limits

The all-table comparison must keep `excluded_tables=[]` and report every actual
difference. New schema objects, added domain columns, four new migration records
and live metrics must not be renamed whole-database equality. Populated-domain
migration, real interrupted migration WAL retention, post-exchange/pre-receipt
native death, active renewal scheduling, public TLS trust, peer DNS transfer,
hosted web/mail/database workloads and continuous availability need separate
observations.

The full kill/reboot matrix, signed public admission, incomplete snapshot capture,
owner metadata migrations, unrelated later owner/data changes, evidence cleanup
and older schema17/pre-ledger transitions remain open. P0.3 is partial.
