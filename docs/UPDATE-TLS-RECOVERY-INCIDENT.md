# Managed TLS update and recovery lock failure

September 13, 2026 · [Türkçe](UPDATE-TLS-RECOVERY-INCIDENT.tr.md)

Status: source fixes tested locally. The owner recovered Frankfurt on its existing
Alpha75 binaries; process identities and HTTPS access were subsequently verified.
This record alone does not certify publication of a new release.

The reported Alpha77 attempt stopped both coordinators and then failed with
`legacy self-signed panel TLS ownership normalization failed`. The supplied TLS
listing contains a `current` link, its `.panel-cert-*` directory and the original
bootstrap certificate pair. The old normalizer unconditionally required exactly
two bootstrap files, so it rejected this supported managed layout. A local
fixture reproduces that rejection. No private key contents are needed to identify
this layout error.

The automatic recovery child independently failed with
`recovery transaction descriptor owns an unexpected lock`. Its early lock check
required the literal substring ` FLOCK ADVISORY WRITE `. Linux fdinfo emits
aligned columns, including `FLOCK  ADVISORY  WRITE`; the same valid inherited
flock therefore passed the runner's field parser but failed the updater's literal
match. A real Linux descriptor reproduces this mismatch.

The source fixes preserve a strictly validated atomic TLS tree, including a tree
with its bootstrap pair, without changing certificate bytes, ownership, modes,
links or modification times. Only the original two-file legacy layout is
normalized. Unexpected entries, untrusted atomic ownership and escaping links
remain rejected. The early updater parses the fdinfo fields while retaining
single-record, inode identity and independent exclusion checks.

Validation: the TLS snapshot/rollback contract covers atomic-only and mixed
layouts, repeated normalization and rejection without mutation. The recovery
lock test uses a real inherited exclusive flock and rejects shared, unlocked,
closed, mismatched-identity and separately owned descriptors. The bootstrap
update and recovery foundation/final-proof contracts also pass locally.

Live recovery must first establish the exact durable phase, staged payload and
unchanged installed artifacts. The old incident-specific pre-ledger abort helper
is not automatically applicable to this normal-ledger failure. Do not remove
transaction markers, edit retained signed release files, or start another update
as an improvised repair. Installed updates remain user-initiated from the panel.

## Confirmed owner-operated recovery

The owner ran the incident-specific recovery script after exact stopped-state,
release bytes, database and staged-ledger checks. The original script reported
an executable-readiness failure after removing the pre-mutation marker and
preserving the incomplete snapshot as evidence. It must not be rerun.

Subsequent owner output showed both units active, with agent PID 1156220 and panel
PID 1156319. Their running executable hashes matched the installed files and the
published Alpha75 archive. A separate read-only HTTPS request returned HTTP 200
with certificate verification successful. Hosted workload health was not audited.

A real fork/exec test reproduces a transient executable identity during startup.
The tool now waits for the exact executable, stable PID and agent socket, and
still rejects a persistent wrong executable. Frankfurt's intermediate executable
was not captured, so this test demonstrates a possible cause of the premature
failure rather than proving that exact live transition. Eighteen tests cover
filesystem/lock/marker handling and startup; systemctl responses are simulated,
while the fork/exec and lock behavior use real local Linux processes.
