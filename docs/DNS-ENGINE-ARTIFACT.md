# DNS engine evidence: shared v1 contract and roles

P0.4, invariants 1, 2 and 4. The Agent now uses `internal/dnsengineartifact`
for every canonical v1 engine state/acquisition encode, decode and validation.
Historical field order, required fields, omission rules, trailing newline, size
limit and value constraints are preserved. This is not a disk schema migration.
Existing snapshots and switch journals still compare their frozen exact bytes.

The legacy combined wire record has two explicitly separate semantic types:

| Role | Fields | Meaning |
|---|---|---|
| `AcquisitionV1` | Mode, engine, epoch, pair role and endpoints, source revision, acquisition manifest, request and owner | The accepted engine tenure and directional authority. Source revision belongs to acquisition; ordinary zone publication does not advance it. |
| `PublicationV1` | BIND generation and primary catalog serial | Configuration publication inside that tenure. This does not establish current native serving state. |

`CompareV1` validates both inputs, compares acquisition identity, then classifies
an unchanged publication or a supported later BIND publication. A primary record
edit may change generation without changing catalog membership/serial. Adding or
deleting a zone may also advance the catalog serial. Same-generation serial
changes, regression, secondary publication and authority changes are refused.
The caller must still prove the actual generation tree and native configuration
before treating a later publication as current. The shared package never grants
mutation authority or substitutes for that native proof. Equal malformed records
are refused, rather than bypassing validation through whole-record equality.

The Agent's signed-update preparation/preflight and ownership/finalization paths
consume this relationship through their existing verified-tree gates. The absent-engine reinstall admission now also checks the whole acquisition:
engine/epoch equality alone previously accepted changed owner, request, manifest,
reviewed source and directional authority. Five regression cases reproduced that
acceptance before the correction and preserve conflicting evidence after refusal.
It still admits ordinary standalone publication generation changes, and its
existing no-running-authority/native-conflict checks remain mandatory. No reader
copies a publication record over acquisition to make them equal. Switch-journal
and frozen snapshot equality remains distinct from this live relationship.

## Historical producer acceptance

The actual Alpha81 producer at `45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00`
was built from its unchanged archived production source with Go 1.26.5. Its
original BIND acquisition, delta renderer, add/edit/delete state writer and
standalone/adopted state producer emitted seven retained fixtures. The new
reader accepts and re-encodes each byte-for-byte. Role comparisons recognize
all three publication changes without changing acquisition identity.

[Fixtures and provenance](../internal/dnsengineartifact/testdata/README.md)
include the export-only test driver and source/file digests. Tests also reject
every acquisition-field drift, unknown/duplicate/noncanonical wire data,
unsupported schema, catalog regression and malformed equal records. A field-role
coverage test prevents a future wire field from silently escaping comparison.
Existing Agent tests exercise actual shared writers, source snapshot/restore,
BIND tree proofs, owner edits and rollback guards.

This establishes a shared producer/reader and explicit semantic role boundary.
It does not replace the combined durable v1 record with separate immutable
acquisition/publication schemas, migrate installed evidence, create an independent
DNS recovery executor or complete native old/new update/restore acceptance.
Those P0.4 items remain open. No installed server is changed by this refactor.
