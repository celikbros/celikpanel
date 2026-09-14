# Recovery runtime promotion

*September 14, 2026 · [Türkçe](RECOVERY-RUNTIME-PROMOTION.tr.md) · D-025 / P0.3*

**Source implementation and acceptance work in progress; not a published or
installed capability.** This slice addresses replacement of an already selected
independent recovery kit. It does not complete the resilience contract.

## Why this is a separate transition

Enrollment previously kept an existing selected kit. That preserved the old
executor but could not provide a newer recovery-data reader when an update
needed it. Replacing the launcher and selector with unrelated writes would
introduce a second failure window inside recovery itself.

Promotion is restricted to the already admitted update's preflight, with the
native release lock on FD 9 and no active, quiesce, completion or scheduler
transaction marker. It prepares a complete retained kit, checks the current
installation with its fixed offline readers, and preserves the predecessor.
No panel or Agent service is stopped to promote a kit. An unreadable selected
kit is not treated as a missing selection.

The runtime manifest remains protocol 1 / snapshot 6 with its existing twelve
files. Promotion has its own versioned evidence; it does not reinterpret old
snapshot or material records. Compatibility checks prove current state
readability, not the success of a future restoration.

## Interruption behavior

The intended durable states are a proved old launcher and old selection, a new
launcher with the old selection, and the complete new pair. The new launcher
can verify the pending transition and execute the selected kit's recovery
binary through a pinned descriptor. The old binary therefore still performs
its original selected-executable proof. Retaining an old directory alone would
not establish that behavior.

Before launcher publication, interruption leaves the old entry usable. That
old program does not understand promotion evidence, so automatic completion of
this early bootstrap window is not claimed. After launcher publication, the
new retained entry can resume the exact transition without executing code from
the candidate archive. It rechecks compatibility and lock/marker evidence;
a previously recorded successful check does not authorize a later mutation.

`recovery runtime-status [--json] [--lang en|tr]` reports a verified kit-transition
phase, or explicitly unavailable evidence, without changing selection. Its
English/Turkish guidance distinguishes reviewing the same release at the early
bootstrap boundary from invoking owner recovery after launcher publication.
An old unchanged launcher does not acquire this new command retroactively.
Status and version remain read-only and available independently of selected-kit
verification. Material/capability reads dispatch the currently proved selected
reader without changing selection. Only the narrow recovery/admitted-preflight
path can resume a pending promotion. The admitted updater must still inherit
the proved native lock on FD 9. Owner recovery instead uses the descriptor it
actually acquires for that same canonical lock: Go may already use FD 9 for its
event poller. An unrelated descriptor is preserved and grants no authority; a
busy native lock still prevents recovery. An inherited FD 9 is reused only when
its exact lock identity and exclusive ownership are proved. Owner edits, foreign objects, unsafe
metadata and unexplained missing evidence are preserved and refused. Extended
attributes on either replaced entry are unsupported and rejected rather than
removed. Once commit is proved, current recovery depends on the verified new
pair and target kit; retired predecessor files are retained evidence, not a new
runtime dependency. Their proof remains necessary for a later archival transition.

An unconfirmed preparation is reported as `recovery_required` in the existing
update failure envelope even if no application transaction has started. It must
not claim that all recovery files remained unchanged. A completed kit transition
does not prove that the application update or current service health succeeded.
After verified kit preparation, a later application preflight may still report
`state=unchanged`: that describes the application payload, not a reversal of the
completed kit transition. Inspect the kit through `runtime-status` separately.

Older unchanged enrollment programs still expect the fixed launcher to have
the selected binary's digest. They can refuse the intermediate dispatcher state
before normal service downtime. This is an explicit bootstrap compatibility
limit, not evidence that those older programs implement promotion.

## Required acceptance for this slice

Local tests must cover exact lock inheritance, closed arguments, old/new object
identity, actual process interruption at publication boundaries, replay,
compatibility failure and owner-change refusal. A successful child exit from a
mock checker is not a whole-server recovery test.

Native acceptance must start from a genuinely enrolled predecessor on registered
disposable Arch and Debian 13 guests. The fixture must call the predecessor's
real enrollment command rather than manufacture selector records. It must
exercise selected-executable proof through the read-only material-capability
entry during the transition and
perform an actual failed update and restoration after promotion. A predecessor
enrollment fixture is distinct from a complete successful previous update.

Record exact source commits, fault boundaries, negative or missed injections,
and retained evidence digests. Until those results exist, this document does
not claim native promotion acceptance. The complete checkpoint matrix, new-token
historical rollback, metadata transitions, independent workload renewal/boot,
and cleanup remain separate open work. Installed-panel updates remain actions
initiated by the user in CelikPanel's own interface.
