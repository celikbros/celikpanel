# DNS engine evidence: shared roles and separate record schemas

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
The following work adds separate wire schemas and a lossless transition contract.
Active Agent writers, switch journals and snapshot readers still use the legacy
v1 disk layout. Installing a filesystem migration, independent DNS recovery and
native old/new update/restore acceptance remains open. No installed server is
changed by this source work.


## Separate wire records and retained before-images (2026-09-23)

P0.4, invariants 1/2/4. `internal/dnsengineartifact` now owns three additional
strict canonical contracts:

| Schema | Role |
|---|---|
| `celikpanel-dns-engine-acquisition/v1` | Immutable engine tenure, owner/request/manifest, source revision, epoch and directional endpoints. No generation or catalog fields. |
| `celikpanel-dns-engine-publication/v1` | Exact acquisition SHA-256 plus generation/catalog publication. The digest covers canonical acquisition bytes including their newline. |
| `celikpanel-dns-engine-legacy-separation/v1` | A read-only proposal retaining both original canonical v1 byte sequences, one acquisition, and separate ownership-checkpoint/current publication records. |

`CompareV1`, already used by Agent update preflight, ownership/finalization and
reinstall admission, now delegates to these separate records and their shared
publication validator. Existing native-generation verification remains mandatory
where it was required. A matching acquisition digest is a content binding, not
an ownership signature, current daemon observation or mutation permission.
Acquisition and publication validators are also the validators for legacy v1;
they do not drift into independent interpretations of the same fields.

`SeparateV1` and `CombineV1` preserve the actual Alpha81 producer's exact bytes.
`PlanLegacySeparationV1` requires two present, canonical, compatible inputs. It
refuses acquisition drift, unsupported publication evolution, regression and
malformed records. The proposal keeps the original ownership publication even
when current publication has advanced. It does not invent a new operation,
revision counter, missing receipt or fresh ownership.

A proposal is NOT mutation admission. `VerifyLegacySeparationSourceV1` refuses
any subsequent source change, including otherwise valid later zone publication.
`LegacyBeforeImagesV1` returns independent copies of the old bytes only when all
three observed separated artifacts still exactly match the recorded proposal.
It refuses a later publication instead of restoring old evidence over new work.
Neither function reads/writes files or changes permissions or services.

### Remaining filesystem transition

Before activating this format, the publisher and independent recovery executor
must bind the proposal to the accepted operation and protected source identities,
prove native configuration under the common mutation barrier, retain metadata and
before-images durably, and publish/recover with explicit checkpoints. Source and
new-layout equality checks above are byte-level prerequisites, not replacements
for that filesystem/authority proof. Old application compatibility and frozen
switch/snapshot consumers must be covered before active writers change format.
No pending historical journal or snapshot may be edited to appear migrated.

### Evidence and limits

Component coverage uses all seven unchanged historical Alpha81 outputs and the
add/edit/delete sequence. It verifies stable acquisition bytes, publication
binding to every authority field, exact historical inverse, missing/mixed inputs,
unknown/duplicate/noncanonical JSON, changed sources, later publication, preserved
checkpoint evidence and defensive buffer ownership. The existing Agent suite
continues to exercise actual writers, native-tree proof gates, owner changes,
rollback and exact snapshots through the new comparison implementation.

These tests do not prove an installed filesystem migration, native interruption
recovery, independent DNS execution or a complete signed release transition.
P0.4 remains partial; those acceptance items stay open.

Recorded local validation (Go 1.26.5, Linux): `go test -race
./internal/dnsengineartifact` passed (1.241 s); `go test -race ./cmd/agent -run
'DNS|BIND|PDNS' -count=1` passed (15.456 s); `go vet` passed for both packages.
The 20-second separated/legacy round-trip fuzz run passed 593,703 executions.
These figures describe component checks, not native service interruption trials.
