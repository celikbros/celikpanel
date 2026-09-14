# Independent recovery material

*September 14, 2026 · [Türkçe](RECOVERY-MATERIAL.tr.md) · D-025 / P0.3*

This source slice extends the [independent runtime](RECOVERY-RUNTIME.md) with a
separate data contract for rollback from a complete v6 snapshot. It does not
claim the entire resilience exit matrix or install an update on any server.

## Boundary and formats

Previously, rollback could run independent code but still required the failed
candidate archive's complete retained file tree. Missing candidate files could
therefore prevent restoration from an intact old snapshot.

Before the first schema or product apply, the selected recovery executable now
seals `celikpanel/recovery-material/v1` under the fixed private recovery-material
root, indexed directly by SHA-256 of the canonical snapshot name. Unrelated
historical records are not scanned. The record binds the exact transaction token hash, v6 snapshot name and
manifest digest, candidate provenance, old/new product tree descriptions, and a
fixed inventory of recovery data. Snapshot format 6 is unchanged.

The data contains release provenance, recovery protocol and sequence policy,
foundation comparison files and the three product service units. Candidate
Agent/Panel/web payloads and candidate install/rollback entrypoints are not copied.
Saved script bytes are comparison data; execution remains in the separately
selected recovery kit. Bin/web publication uses intent v2 bound to the material
record digest. With a compatible selected reader, an exact v1 transaction without
material keeps its legacy interpretation.

Preparation requires native root, the held release lock on FD 9, the exact active
update, stopped coordinators, verified source/snapshot trees, unchanged old
installed resources and no prior publication intent. Private files and directory
entries are flushed before an atomic no-replace publication. A retry cannot
manufacture authority for already changed resources. Interrupted private stages
are preserved; they are not adopted as a committed record.

## Recovery behavior

For active rollback and rollback completion/scheduler recovery, the runner first
verifies existing material and passes only its data directory to the selected
kit. The bin/web restorer consumes the old snapshot and recorded target evidence
without opening the retained candidate. A changed installed resource is still
refused unless the exact publication journal establishes its before/after state.
Owner changes are not overwritten by a comparison against a guessed target.

Only verified absence permits the legacy retained-candidate lookup. A corrupt,
foreign, unsafe or missing material associated with v2 publication cannot be
downgraded. The full existing snapshot database/TLS/units and runtime checks remain
mandatory. Successful file restoration alone is not recovery completion.

The selected executable must confirm the exact `snapshot-name-sha256-v1` layout
with `verify-material-support --layout snapshot-name-sha256-v1` before coordinator stop.
An older selected kit is retained and the update refuses before that downtime;
this slice does not automatically promote a replacement recovery kit.

This contract resumes the same accepted transaction. A fresh historical rollback
after that transaction has completed has a different token and is not admitted
by this slice. When its snapshot has recovery material, direct rollback refuses
before creating a new marker or stopping services; it does not reuse the old
token or fabricate replacement evidence. Genuine legacy v1 rollback remains
separate. New-operation historical rollback admission is an open compatibility
item, alongside selected-kit promotion. The new direct rollback script also
requires the selected reader to prove material absence for an existing legacy
transaction; an older incompatible kit may therefore refuse before downtime.
Unchanged historical retained scripts keep their separate legacy path.

The closed CLI commands are internal root entrypoints: `verify-material-support`,
`prepare-recovery-material` and `material-root`. They cannot start a new update,
accept caller-supplied transaction tokens, choose an output directory, or bypass
owner authentication. Owner recovery remains `sudo /usr/libexec/celikpanel/recovery recover`.

## Evidence and remaining acceptance

Linux root tests exercise candidate-tree disappearance after real publication,
replay, actual SIGKILL around material publication, corruption and owner-change
refusal, and rollback completion marker combinations. Shell/CLI tests cover the
held FD, strict absence/error distinction, source/data separation and pre-apply
ordering. These tests alone do not establish native whole-system acceptance.

The disposable VM fixture can quarantine exactly the retained candidate's
`rollback.sh`, `bin/agent` and `web/dist/index.html` after the installed candidate
checkpoint, then kill the exact updater. It preserves originals and verifies
that installed products, snapshot and runtime were not changed by the injection.
Its private records are test evidence, never product recovery authority.
[Native acceptance](../deploy/e2e/release-recovery/RECOVERY-MATERIAL.md) records
successful automatic rollback with those files absent, plus recovery SIGKILL on
Arch and reboot on Debian 13. Full database equality was not observed: old rows
were retained and later metrics were added. The acceptance record distinguishes
those results and remaining workload/fault coverage.

Incomplete snapshot capture and update completion still require retained
candidate data. The full checkpoint matrix, signed Agent admission, selected-kit
promotion, metadata migrations and evidence cleanup remain open. P0.3 is partial.
