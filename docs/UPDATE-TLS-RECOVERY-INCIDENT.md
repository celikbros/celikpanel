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

## Alpha78 follow-up: retained issuance receipt

The owner's Alpha78 attempt at 2026-09-13 22:17 UTC stopped before TLS snapshot
publication. Recovery repeatedly reached `existing managed TLS tree failed
strict validation`. The owner-provided file inventory identifies a retained
certificate version with `.panel-certificate-issue-receipt.json` (root:root,
0600, one link, 325 bytes), alongside a newer three-file `current` version.

The certificate issuance writer deliberately writes and retains this fourth
file as operation evidence. The shell snapshot validator required exactly three
files in every version directory, including retained versions. A local fixture
of the supplied layout reproduces the rejection. Alpha78's earlier tests used
manually constructed three-file versions, so the release checks did not cover
this actual issuance output. Passing CI did not establish this upgrade path.

The follow-up source change accepts only that named optional receipt with its
root-only metadata and 1–1024-byte bound. Snapshot and restore preserve its exact
bytes and metadata; the agent retains responsibility for interpreting operation
identity and canonical JSON. Unknown files and unsafe receipt metadata remain
rejected. Deleting the receipt is not a recovery strategy.

This follow-up is included in Alpha79. Before publication, the owner transferred the
reviewed incident recovery tool (SHA256
`13f03ebe790a3b9af9b01207a22393ab287a9c938b87b697859873ac2b19763c`),
verified its checksum and ran it. It reported both existing Alpha75 services
active after its locked identity/data checks; no update was installed. Evidence
was preserved at `/var/backups/celikpanel/frankfurt-recovery-20260913T221708Z`.
A subsequent independent read-only HTTPS login request returned HTTP 200 with
certificate verification successful. This confirms panel access, not an audit
of hosted workloads. Previous incident evidence, rescue snapshots and TLS files
were outside the recovery tool's mutation scope.

## Alpha79 follow-up: BIND publication and automatic rollback

The owner initiated Alpha79 operation `971510e51549422a7cd8c4878dc714af` on
September 14. At 04:31:58 UTC, the updater rejected `BIND state and ownership
receipts disagree` after installing and verifying the target artifacts. The
subsequent recovery service repeatedly failed with `recovery transaction
descriptor owns an unexpected lock`. Historical failed systemd trigger
candidates in the journal do not prove concurrent updates.

The supplied current BIND state and acquisition ownership have the same engine,
epoch 1, primary role, local/peer IPs, source revision and mutation identity.
Only generation and primary catalog serial differ: current state records
`ea6ea68b1b7caa4fa9b0a4260e567842353f3667aa5ad4df98ba5b70cb1f1469`
and serial 2, while acquisition ownership records
`b19ee90a81d87342cba30751f1aede6342706643c243130f37c3f164006926c7`
and serial 1. Ordinary zone publication advances those current-state fields
without replacing engine acquisition evidence. The updater incorrectly required
complete equality and then selected the historical acquisition generation.
A regression using the actual zone renderer and state writer reproduces the
original rejection. The corrective code preserves acquisition evidence and
requires the current generation and runtime configuration to verify.

Automatic rollback independently failed because Alpha78's fdinfo field-parsing
fix covered `update.sh` but left `rollback.sh` using a single-space substring.
A real inherited Linux exclusive-flock test against Alpha79's rollback
entrypoint reproduces `exclusive status=41 expected=0`. The local fix parses
fields on both entrypoints while retaining single-record, inode identity and
independent exclusion checks. Shared, unlocked, closed, wrong-identity and
foreign-owner descriptors remain rejected.

### Confirmed owner-operated Alpha79 rollback

This was a post-apply failure, so the earlier pre-mutation abort tools were not
applicable. The owner used the standard rollback entrypoint in the retained,
verified Alpha79 release:

- Release: `/var/backups/celikpanel/releases/f3390addc85b-1f7e1f714db463724f795ebb`
- Snapshot: `/var/backups/celikpanel/update-snapshots/20260914T043142Z-from-unknown-to-f3390addc85bbe92a0cc865448d9bb366e6b980a-db8813bd340936d749913d55dbcda68d`

Normal owner invocation acquires its own transaction lock and avoids the broken
inherited-lock parser. It still verifies the retained release, complete snapshot,
matching transaction and idle ledger before restoration. The original Alpha79
entrypoint's normal acquisition and busy-lock refusal were tested locally before
the command was supplied. No retained signed file or transaction marker was
manually edited.

At 05:53:39 UTC, the owner output reported `Rollback complete`, `Panel: active`
and `Agent: active`, after database, artifacts, TLS and lifecycle restoration.
An independent read-only request to `https://frankfurt.celikhost.com:2083/login`
then returned HTTP 200 with certificate validation successful at `72.62.38.15`.
This confirms management access; it does not establish hosted workload health
or correct the remaining BIND receipt-consumer defect. No new update was installed
by this recovery. Permanent fixes remain local until separately published.
