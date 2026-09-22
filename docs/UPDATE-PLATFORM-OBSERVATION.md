# Update platform observation must remain fresh

D-025 invariants 2/3, P0.2; D-024 actionable unknown-state guidance.

The Arch AU firewall trial exposed an admission failure before update start:
Alpha81 reported that host package-manager policy could not be verified, while a
later direct read-only host-profile probe identified pacman/systemd successfully.
Restarting only the fixture Agent made ordinary preview pass. The exact initial
probe error was not retained by Alpha81, so this evidence does not establish its
precise boot-time cause. Source inspection established the permanent-cache bug:
`newPlatformSystemUpdateService` evaluated the profile once and stored a refusal
on the process-wide cached service object.

The cached service now stores a read-only verifier, not its result. Check, Start
and Abandon each evaluate it once at their existing admission boundary. A later
request can observe readiness without restarting Agent; a previously successful
check cannot authorize Start after the host policy becomes invalid. There is no
automatic mutation, alternate launch path or retry loop. Existing exact target,
signing floor, operation identity and transaction checks remain in place.

Still-starting errors retain their classification and explain that the owner can
check again after startup. Other profile errors retain the cause and direct the
owner to resolve it before checking again. DNF remains unsupported for updates.
Status observation is unchanged and does not require this admission check.

No persisted schema, release sequence, ownership or license policy changes. No
migration or receipt rewriting is needed: process-local probe results were never
durable recovery evidence. An already denied request starts nothing; the owner
reviews and explicitly starts an update after a successful new check. Existing
worker/recovery operations remain governed by their original durable identity.

Validation: same service instance goes from tagged starting error to ready;
a subsequent invalid executable observation blocks Start without fetching or
queueing; a new explicit ready Start succeeds. All SystemUpdate tests pass under
the race detector and Agent vet passes. This closes the scoped source cache defect,
not P0.2 or the native acceptance matrix. The corrected candidate still needs a
native same-process startup/admission trial; AU ran the historical Alpha81 baseline.
