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
The publication integration below activates self-contained v2 documents. Frozen
v1 switch journals and snapshots remain readable and byte-preserving. Independent
DNS recovery and full native old/new update/restore acceptance remain open. No installed server is
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

### Operation-bound document publication

The two existing atomically replaced file paths now support role-specific
self-contained documents: `celikpanel-dns-engine-state/v2` carries current
publication; `celikpanel-dns-engine-ownership/v2` carries the frozen ownership
publication checkpoint. Each embeds the independently versioned acquisition and
publication records. No untracked content object or second migration journal is
introduced. Legacy v1 remains accepted in either historical role; v2 is
role-specific. All four old/new ownership/current combinations are readable.

Normal accepted DNS writers publish v2 through the existing metadata-checked
snapshot/CAS atomic writer. Ownership now uses that same checked writer. Existing
operation admission, native-tree verification, common exclusion and journal
checkpoints continue to control publication. Reading does not migrate a file.
Ordinary zone publication never refreshes the acquisition file to match it.

Signed-update cleanup of an already committed historical journal is a distinct
case: its ownership writer preserves the exact current state's wire format.
It cannot introduce v2 merely while preparing an application to start, which
would unnecessarily remove the historical rollback target's compatibility.
Frozen journals and snapshots retain their original schema, metadata and bytes;
DNS transaction compensation restores those bytes without re-encoding.

### Application compatibility admission

Current-source `agent-native-contract.json` optionally declares
`dns_evidence_policy: separated-acquisition-publication-v2`, bound to exact Agent
bytes. Historical declarations remain parseable without acquiring this claim.
The producer must never be used to certify an old executable retroactively.

The independent `verify-dns-application` reader inspects the live private state
and both per-engine ownership files. It pins root-owned directories/files and
rechecks contents, inode, mode, UID/GID and absence. Private root:service-group
0600 files are retained without permission normalization. Wrong role/path,
unknown schema, unsafe metadata, substituted files, changed target bytes and
late evidence appearance refuse admission. Any v2 document requires a matching
binary-bound capability; canonical v1-only/absent evidence adds no v2 requirement.

Update checks installed and candidate compatibility before coordinator downtime
and again under exclusion before publication. Rollback checks the target at both
boundaries. **Application rollback checks current native DNS evidence**, not an
old snapshot's DNS copy: application restoration retains native DNS and must not
roll back later owner-managed zones. A historical Agent lacking v2 support is
explicitly refused before stop; this is not automatic downgrade support.

Independent DNS transaction recovery and the complete signed-update/automatic
rollback matrix remain open. This uses the current Agent transaction executor;
separate schemas and an independent read-only admission command do not establish
an Agent-independent DNS mutation executor.

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

### September 23 integration evidence

New tests cover actual process SIGKILL before atomic rename and after publication,
exact inverse to both legacy and v2 bytes, repeated restore, role-specific strict
readers, mixed pairs and signed-update legacy-format preservation. The independent
reader tests include target/source replacement, new previously absent evidence,
owner metadata, unknown format and no mutation on refusal. Go canonical producer
and Python lab observer share four golden documents; original Alpha81 fixtures
are unchanged. Shell admission tests execute refusal before subsequent steps.

The first full recovery-runtime race run reached the default ten-minute package
timeout while executing its existing mail interruption matrix; it did not complete.
The full rerun with a 30-minute limit passed in 787.122 seconds. The DNS Agent
race suite passed in 20.431 seconds; shared document, capability, CLI, release
producer and native-lab driver suites also passed. The Python DNS matrix suite
and shell admission checks passed.

[Debian and Arch native evidence](../deploy/e2e/release-recovery/DNS-DOCUMENTS-BE.json)
uses the unmodified Alpha81 Agent to create standalone BIND and publish its zone.
A fixture handoff to the current Agent performs two authenticated normal zone
publications, producing state/v2 while retaining the original ownership/v1 bytes.
TCP/UDP queries remain authoritative. After stopping/removing the Agent binary
(Panel was absent) and QEMU reset, BIND still serves the same answers; both DNS
receipts and the entire Agent ledger retain identical bytes. The independent
reader refuses the unchanged historical Agent against live v2 evidence with
exit 3 and no DNS rewrite. This is a native publication/continuity result, not
signed application update, automatic rollback or independent DNS recovery proof.


The same marked Debian/Arch guests also accept the exact committed `fddd595`
Agent with its matching native contract through the independent compatibility
reader. The DNS receipts and mutation ledger remain byte-identical, BIND remains
active, and neither management executable is installed or started. This is the
positive counterpart of the historical-target refusal in
[DNS-DOCUMENTS-BE.json](../deploy/e2e/release-recovery/DNS-DOCUMENTS-BE.json);
it proves read-only target admission, not a signed update or rollback.

## Shared switch journal contract — 2026-09-23

P0.4, invariants 1/2/4: `internal/dnsengineartifact/switch_journal.go` now owns
`celikpanel-dns-engine-switch-journal/v1`, file/unit before-images, canonical
encoding and decoding, source acquisition comparison, catalog serial rules and
BIND/PowerDNS switch/adoption layout validation. Agent aliases and adapters use
this implementation; persistence, locks, process observation and native inverse
execution remain in the Agent. The package performs no filesystem access.

The wire schema and field order do not change. Embedded v1 or v2 DNS source
bytes remain frozen through every journal phase, including rollback; decoding
never rewrites them. A trusted host adapter supplies state ownership and fixed
PowerDNS paths. Journal input cannot select its own permitted restore paths.
A valid journal is **not** mutation authority: exact accepted ledger identity,
worker exclusion, host ownership, unchanged evidence and native verification
are still required before any action.

Evidence: three [historical Alpha81 producer fixtures](../internal/dnsengineartifact/testdata/switch-journal/README.md)
round-trip byte-for-byte; current shared tests reject ambiguous JSON, wrong
owners/paths, source acquisition drift and conflicting adoption evidence.
Agent DNS/BIND/PowerDNS/primary catalog race tests pass (6.720s), shared artifact
race tests pass (1.403s). A 12-second bounded fuzz run passed but executed only
eight cases and is not broad fuzz coverage. No installed server was changed.
The standalone native DNS recovery executor and native interrupted operation
acceptance remain open; sharing the journal does not establish either.