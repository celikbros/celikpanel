# Independent recovery runtime

*September 14, 2026 · [Türkçe](RECOVERY-RUNTIME.tr.md) · D-025 · P0.2/P0.3*

The owner can inspect an exact update request and resume its existing recovery
without starting the panel, Agent RPC, an application migration, or a license
check. Native root or authorized sudo authenticates this narrow entrypoint.
No separate HTTP listener or duplicate session authority is introduced.

## Boundary

`/usr/libexec/celikpanel/recovery status --request-id <id> --json` reads the same
bounded observation contract used by the panel. Exit zero means a known
observation, **not** successful installation. Unknown evidence stays unknown.
`recover` examines only the already durable transaction under the native release
lock. It cannot create a new installed-panel update or accept an arbitrary target.
The user still initiates every installed-panel update from the panel interface.

The first admitted install/update enrolls a verified recovery kit before stopping
coordinators. Code is retained under
`/usr/libexec/celikpanel/recovery-runtimes/v1/<manifest-sha256>`; the root-only
selector is `/var/lib/celikpanel-release-state/recovery-runtime.v1`.
Enrollment publishes the complete fsynced kit and launcher before the selector.
After enrollment, the candidate CLI asks the selected kit's offline readers to
check the current normal, pre-ledger or schema17 state under the release lock,
before publishing quiesce intent or stopping coordinators. An incompatible reader
therefore refuses the update while the panel is still running. This proves current
state readability, not success of a future restore.

An existing compatible selection is retained. Missing/corrupt selected bytes do
not authorize overwriting it with a new candidate or falling back to candidate
lifecycle scripts. Interrupted unpublished stages remain unselected evidence.

The canonical manifest fixes protocol 1, snapshot format 6, twelve files, modes,
SHA-256 values, and exact inventory. The reader checks root ownership, canonical
ancestors, links, bounds and path/file identity; it pins descriptors and repeats
its proof before dispatch. This protects the administrative boundary, not against
an arbitrary concurrent root writer after the final check.

## Existing operation and snapshot transitions

The executor uses its enrolled scripts and offline checkers. Failed target release
material is a separate data/provenance root. Panel/Agent offline checkers compile
from the **same** production snapshot and ledger validators, without their normal
main functions, RPC setup or HTTP listeners. The schema17 bridge is part of the kit.

| Existing state | Independent behavior |
|---|---|
| No durable operation | No mutation is admitted. |
| Updater owns the lock | Do not compete; the native recovery timer retries. |
| Quiesce capture | Abort the exact pre-active capture through its existing proof. |
| Active, incomplete capture | Complete the verified snapshot, then hand off to kit rollback before candidate installation. |
| Active, final snapshot | Restore that exact snapshot through kit rollback. |
| Completion/scheduler cleanup | Verify the existing runtime and finish only the exact recorded cleanup; do not run a candidate migration. |
| Unknown/mismatched evidence | Preserve the operation, refuse the affected action, keep independent status available. |

Only outer snapshot format 6 is accepted. Its normal, pre-ledger schema20 and
schema17 bridge transition modes retain their existing explicit guards. Snapshot
formats 4/5 are not silently reinterpreted. Normal rollback keeps the already admitted Agent ledger inode in place and repeats
its lock, exact bytes, ownership and metadata proof. It does not delete that ledger
and create an interruption gap. Missing or changed owner state remains a refusal.
The restore checks retain inherited
release/mutation lock proof, stopped-coordinator proof, exact durable identity,
snapshot integrity and owner-change checks.

## Acceptance record and limits

- Canonical manifest, root/path/FIFO/link/content/replacement tests and native
  root inherited-FD enrollment tests cover the standalone boundary.
- Offline checker tests exercise real SQLite schemas/WAL and real ledger files;
  they do not substitute a successful child exit for a native restored runtime.
- The disposable VM collector compares all tables from one supported SQLite
  read transaction, including sessions and operations. Existing WAL coordination
  metadata may change and is reported; DB/WAL payloads are not rewritten.
- Native recovery-process interruption and reboot evidence is recorded with the
  disposable VM matrix, not inferred from these component tests.

P0.3 remains open until the complete supported checkpoint matrix passes. The kit
currently shares restoration algorithms with the release scripts; enrolling them
does not by itself make every payload publication atomic, make TLS normalization
read-only, or remove every dependency on retained candidate **data** integrity.
A later recovery protocol/kit promotion needs compatibility drills and retention
of the proven predecessor. P0.4 artifact schema separation and P0.5 independent
workload renewal/boot acceptance remain separate work.

If enrollment stops after publishing the launcher but before its selector, retry
with the same kit is supported and tested with actual SIGKILL. A different kit's
launcher is not substituted automatically: the existing launcher and first kit
remain intact, and no selector is fabricated.
