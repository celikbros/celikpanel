# Isolated update database migration

*September 15, 2026 · [Türkçe](RECOVERY-ISOLATED-DATABASE.tr.md) · D-025 / P0.3*

This slice addresses interruption before database readiness in a normal update
with a complete v6 snapshot. The candidate migrates a private working copy; the
canonical database is exchanged only after independent verification. It builds
on [forward completion](RECOVERY-FORWARD-COMPLETION.md). It does not close P0.3,
change license policy, or authorize assistant-initiated installed-panel updates.
Native acceptance for the new migration boundaries is still pending.

## Authority and compatibility

New material uses `celikpanel/recovery-material/v3`; its existing directory
layout, snapshot v6 and recovery protocol 1 are unchanged. A normal v3 record
includes the exact canonical database before-image: parent/file identity,
owner, permissions, timestamps and content hash. The complete snapshot and its
manifest are pinned separately. This evidence exists before creating a working
copy, so a crash before admission cannot be mistaken for permission to restore
an unrelated database.

The selected recovery runtime must advertise material-v3 and
`celikpanel/database-migration-admission/v1` support before downtime. The
read-only `probe-update-database` checks supported canonical metadata under the
inherited release lock; it allows the ordinary running database's WAL. This
slice rejects extended attributes on the canonical database or its parent; it
does not remove or silently copy a subset of owner attributes. The existing
snapshot producer first durably captures and then normalizes the canonical
SQLite database. V3 before-image publication requires absent sidecars.

V1/V2 material remains readable. `database-policy --snapshot NAME` returns
`required` for normal v3 material. Exit 6 with empty stdout permits the separately
verified historical path. Unknown/malformed material, or absent material with
transaction-bound database workspace residue, cannot fall back to legacy.
Schema17 and pre-ledger transitions retain their distinct existing path; this
slice does not extend their migration acceptance.

## Preparation and publication

The normal updater retains its `active` marker through these steps:

1. Prepare immutable v3 material and admit the working copy under the same
   token/snapshot. No new token or arbitrary database path is accepted.
2. Run the candidate's ordinary embedded migration as the unprivileged panel
   account in `/var/lib/celikpanel/.release-db-migrations/<token-sha256>/work`.
3. Independently verify stopped writers and seal the working database and its
   sidecars. Normalize only a separate private copy with SQLite.
4. Verify the complete target schema, exact embedded migration history and idle
   queue. Durably record before/after publication identity, exchange the two
   database entries atomically, fsync both parents, then record publication.
5. Recheck the installed payload and database publication. Only then create
   `completion.pending` and allow the existing controlled service-start path.

Admission, seal, publication intent and restoration intent have separate v1
schemas. Their receipts bind the exact corresponding intent. The original
canonical inode survives the exchange as the retained counterpart. Working
DB/WAL/SHM and failed preparation directories are preserved. No polling request
starts another migration, and the independent recovery checker never executes
the candidate migration or ordinary panel entrypoint.

The working copy is a separate migration target, not an OS sandbox for arbitrary
candidate code. The reviewed embedded migrator honors `CELIKPANEL_DATA_DIR`;
the panel account still owns the canonical database. Before/after checks reject
unexpected canonical edits rather than silently restoring over them.

The four internal recovery commands accept only an existing snapshot name:
`prepare-update-database`, `publish-update-database`,
`restore-update-database`, `verify-update-database`. They require the selected
kit and its inherited native FD9 flock. The ordinary owner interface remains
`sudo /usr/libexec/celikpanel/recovery recover`; these internal commands are not
a new update interface or a general filesystem repair API.

## Failure, recovery and final proof

A failed or interrupted migration leaves the canonical before-image in place.
Recovery preserves the working copy and verifies that exact before-image instead
of applying an older snapshot over unknown live bytes. After publication,
rollback verifies the recorded pair and compensates by the inverse exchange.
A differing owner file, missing required receipt, malformed authority or
unrecognized pair is preserved and reported as unverified. It is never made to
match by deleting WAL files or rewriting ownership evidence.

During an active stopped operation, verification requires the full recorded
before/after file proof and absent canonical sidecars. After services are
allowed to run, legitimate database/WAL writes may change content and timestamps.
Late verification therefore checks the published canonical inode/owner, retained
counterpart and receipts, plus a private WAL-aware whole-schema/migration/idle
read. Update completion requires the exact current target schema; rollback
completion requires the historical snapshot schema. Terminal checks repeat
before and after scheduler restoration. A completion marker is not success.

## Evidence and remaining work

`deploy/test-isolated-database-migration.sh` executes the actual extracted shell
orchestration. It covers publication before completion, failed migration,
failed publication/verification, running writers, invalid work paths, unknown
policy refusal, and isolated versus historical rollback dispatch. Go tests
exercise the real SQLite and protected publication APIs, including SIGKILL at
selected durable boundaries. These are component evidence, not native rollout
acceptance.

The disposable fixture now has an explicit, fixed published Alpha64/schema38
baseline alongside its Alpha75 default. It verifies actual running/disk binary
hashes and all 38 migration identities before admitting a local candidate test.
A real old-to-new migration, retained failed WAL, exact process interruption,
automatic same-operation recovery and preserved workloads must be observed on
Arch and Debian before claiming native acceptance for this slice.

The full kill/reboot matrix, signed candidate admission, incomplete snapshot
capture, owner metadata migrations, recovery after unrelated live schema/data
changes, evidence cleanup and older schema17/pre-ledger transition acceptance
remain open. An AI assistant must use this same bounded operation/recovery
contract; it cannot supply missing authority or turn an unknown result into
success.
