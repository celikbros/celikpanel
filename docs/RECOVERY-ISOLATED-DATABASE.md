# Isolated update database migration

*September 15, 2026 · [Türkçe](RECOVERY-ISOLATED-DATABASE.tr.md) · D-025 / P0.3*

This slice addresses interruption before database readiness in a normal update
with a complete v6 snapshot. The candidate migrates a private working copy; the
canonical database is exchanged only after independent verification. It builds
on [forward completion](RECOVERY-FORWARD-COMPLETION.md). It does not close P0.3,
change license policy, or authorize assistant-initiated installed-panel updates.
The scoped Q native result below applies to source `0610b239`. Separate fresh
R guests establish the same two boundaries for corrected source `cb31654`;
neither result establishes the complete fault matrix.

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
inherited release lock; it allows the ordinary running database's WAL.

From `cb31654`, new normal admission requires `/var/lib/celikpanel` to have the
panel account's exact UID, the `celikpanel` group's exact GID, and mode `0750`.
The same requirement is checked again when capturing the canonical Before for
new material, before apply-only installation. This prevents admitting a parent
layout that the installer or native `StateDirectoryMode=0750` startup would
later normalize. Root ownership, mode `0700`, or a different group is refused
without changing the directory, database or WAL.

An observed unsupported parent returns `ErrUnsupportedDatabaseParent`, with
owner guidance to preserve intentional settings and retry from the panel only
after choosing a supported layout or compatible recovery version. This is
separate from `ErrUnsupportedMetadata` for unsupported filesystem attributes,
and from an unknown/unavailable metadata result. The check does not stop
services or authorize a `chown`/`chmod` repair. Extended attributes on the
canonical database or its parent remain unsupported; they are never removed
or partially copied.

Historical material readers retain the broader secure/quarantine parent
contract. This admission correction does not rewrite old Before records or
change their recovery path. The existing snapshot producer first durably
captures and then normalizes the canonical SQLite database; its quarantine
release restores panel ownership and mode `0750`. That earlier normalization
is not evidence of preserving arbitrary owner layouts. New normal preflight
rejects unsupported layouts before that path. V3 before-image publication
also requires absent canonical sidecars.

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
completion requires the historical snapshot schema. A recognized historical
two-column migration ledger is canonicalized only on that private read copy,
then compared with the snapshot history; unknown ledger layouts are rejected.
The canonical DB/WAL/SHM and snapshot remain unchanged. Terminal checks repeat
before and after scheduler restoration. A completion marker is not success.

## Evidence and remaining work

`deploy/test-isolated-database-migration.sh` executes the actual extracted shell
orchestration. It covers publication before completion, failed migration,
failed publication/verification, running writers, invalid work paths, unknown
policy refusal, and isolated versus historical rollback dispatch. Go tests
exercise the real SQLite and protected publication APIs, including SIGKILL at
selected durable boundaries. These are component evidence, not native rollout
acceptance.

The [Q native acceptance record](../deploy/e2e/release-recovery/ISOLATED-DATABASE.md)
binds source `0610b239a7bb976874a2c347099bf88e779237d1` to fresh Arch and Debian 13
guests with genuine published Alpha64/schema38 state. Independent host review
verified all 87 Arch and 70 Debian indexed evidence files. The test used the
registered unsigned local-candidate fixture, not public signed Agent/UI admission.

Arch's `candidate-installed` cut observed the exact canonical Before and initial
work copy, only admission evidence, and no later database publication records.
Automatic rollback returned to the old binaries and schema38/55 tables while
preserving that work copy. This is initial-state evidence, not a native cut
inside a SQLite transaction or a failed-WAL retention result. Debian completed
the real schema38→42 migration, then lost exactly three retained candidate files
and the updater at `completion-database-verified`. Automatic forward completion
retained the new binaries and schema42/65 tables. Raw admission, seal and
publication receipts bind the preserved original Before inode/content and the
published canonical inode. Terminal markers were absent and loopback HTTPS was
reachable; this does not establish uninterrupted availability.

The native aggregate collector compared all 55 old tables, including
`sqlite_sequence`, using old-column projections with row identity and typed
values. No old rows were missing or changed; old migration rows including
`applied_at` were retained. Each guest added 19 later metrics rows. The complete
DB comparison remains `DIFFERENT`, with no excluded tables: Debian also has the
intended ten added tables, two added domain columns and migrations 39–42. The
domain table was empty, so populated-domain migration remains untested. Raw row
values were not exported; host review verifies the collector, sealed assertions
and evidence bindings rather than recomputing private rows. Terminal inspection
did not synthesize cleared markers or rerun the active-only database API.

For the subsequent `cb31654` correction, root/Linux regressions first reproduced
unsupported parent acceptance, then verified refusal without metadata/byte
changes, preserved historical reader compatibility, and separate actionable
layout guidance. Those component tests do not extend Q's source attribution.
Fresh R guests used exact `cb3165456bb4ba4654dc19d51a5eafc13721a5fb`, with the
same genuine Alpha64 baseline and two fault boundaries. Independent review
verified all 62 Arch / 63 Debian sealed files and 28/44 additional DB checks:
Arch automatically rolled back with its initial work preserved; Debian
schema38→42 automatically completed after the three-file loss. All old rows in
55 tables remained; each guest added four later metrics samples (15→19 and
16→20), so global comparison remains `DIFFERENT`. The acceptance record binds
source, fixture, archive and exact operation hashes. This proves the supported
normal parent path; unsupported-layout refusal is component-test evidence.
Both laboratories are stopped with disks and evidence retained.

The full kill/reboot matrix, signed candidate admission, incomplete snapshot
capture, owner metadata migrations, recovery after unrelated live schema/data
changes, evidence cleanup and older schema17/pre-ledger transition acceptance
remain open. An AI assistant must use this same bounded operation/recovery
contract; it cannot supply missing authority or turn an unknown result into
success.

The next [native exchange interruption fixture](../deploy/e2e/release-recovery/NATIVE-DATABASE-EXCHANGE.md) cuts after successful database exchange and before its receipt. Arch U and fresh Debian W have scoped automatic-rollback and complete cut-row retention evidence; earlier Debian U/V attempts remain inconclusive. This adds no product schema transition and does not close P0.3.
