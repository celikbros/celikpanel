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
### Accepted operation identity

`switch_authority.go` shares the existing exact active-job, registered-worker,
expired-cancellation and finalized-ledger comparisons. The Agent uses these same
functions. The published v1 and finalized v2 phase strings and generic lease
expiry values retain their historical bytes. Expected request/owner IDs, target
engine and manifest qualifier must also be canonical before comparison.

Registered-worker shape deliberately does not probe `/proc`, clear the worker
slot or permit independent recovery of a living worker. The strict active-job
comparison still requires an empty worker slot. Host/publication locks, liveness
and accepted-ledger revalidation remain obligations of the executor. An exact
finalized ledger records past completion, not current DNS health.

Shared race checks (artifact 1.408s, ledger 2.235s) cover different IDs, target,
manifest, phase, lease timing, worker and conflicting terminal evidence. Agent
DNS/BIND/PowerDNS/primary-catalog/service-mutation race checks pass (11.525s).
The initial expanded run rejected one old worker-shape test's literal `qualifier`
placeholder; the test now supplies a valid canonical manifest qualifier, while
new tests separately require malformed expected identities to be refused.
No new native mutation entry point or lifecycle permission is introduced.
### Shared switch recovery decision sequence

P0.4, invariants 1/2/3/4: `internal/dnsenginerecovery` now owns the bounded
decision order for the historical switch journal. It rejects another operation
or a malformed journal before any native callback. It tries to prove the target
before considering the inverse; a previously verified or committed target that
now disagrees is left for owner review. A precommit inverse records
`rolling-back`, runs the host inverse, records `rolled-back`, then removes the
journal. Any failed checkpoint or inverse leaves evidence in place. The Agent
uses this shared sequence with its existing target proof and native inverse.

The journal remains v1; the new package creates no persistent schema. Existing
accepted ledger/job identity and native host/publication locks still surround
the Agent call. The package itself is not an independent native executor: it
cannot acquire a lock, observe a worker, change a service or certify current DNS
health. A future independent host adapter must prove those conditions with the
same accepted operation and preserve owner edits. The selected recovery kit has
not yet gained that adapter or a command to run it. Complete native historical
interruption and signed update/automatic rollback acceptance remain open.

Shared race tests exercise verified target, mismatch, committed conflict,
precommit inverse, foreign identity, malformed before-images, inverse failure,
checkpoint failure and failed removal. Agent DNS/BIND/PowerDNS race checks and
`go vet` complete the scoped source validation. These checks do not constitute
native installed-release fault acceptance; no installed server was changed.

### Source-proof gate for an unverified switch target

P0.4 and constitutional invariants 1/2/3: an early switch journal no longer
starts an inverse merely because target verification returned an error. The
shared reconciler first requires a separate proof that the current state receipt
matches the journal's exact frozen source (or that both source and current state
are absent). The Agent adapter also reconstructs the committed manifest and
checks the source ownership receipt. Read failure, foreign receipt, target
receipt with an inconclusive runtime probe, cancellation, or an expired deadline
retains the journal and blocks this automatic inverse. A previously durable
rolling-back intent continues through existing owner-aware native inverse
checks; verified/committed target disagreement remains blocked.

There is no wire/schema transition: journal v1 and historical phase bytes are
unchanged. Scoped race tests cover exact source, foreign/malformed receipt,
unknown and cancelled observation, checkpoint order and durable inverse resume.
This narrows destructive recovery admission but may require owner action when
the target receipt exists and runtime proof is unavailable. It does not add an
independent executor or satisfy native historical fault acceptance.

### Monotonic recovery checkpoints

P0.4, invariants 2/3: once the v1 journal records rolling-back or
rolled-back, replay cannot reclassify that operation as committed even if a
later target probe would pass. The accepted inverse is retried with its
owner-aware native checks. Replay does not write rolling-back again when it
already exists, and rolled-back replay performs the inverse/source proof
before removal without regressing either checkpoint. Direct rollback of a
target-verified or committed journal is refused. No journal schema or phase
encoding changes. Race tests cover both crash-replay phases, a later passing
target probe, and direct verified-target refusal. Native power-cut and
independent executor acceptance remain open.

### Shared frozen-source comparison

P0.4, invariants 1/2/3: the pure source-state comparison used to admit a new
inverse is now in dnsengineartifact.ProveFrozenSwitchSourceState. It decodes
the frozen v1/v2-compatible source from the historical v1 switch journal,
validates the current observation, and returns true only for exact source
equality or verified mutual absence. Agent remains responsible for manifest
reconstruction, ownership receipt and filesystem observation. The function is
read-only and does not grant native mutation authority. All three Alpha81
switch-journal fixtures and Agent DNS race tests pass. No schema bytes change;
the independent executor and native fault matrix remain open.

The source predicate also accepts a v1 switch journal with a canonical v2
source-state before-image, then refuses a later publication generation as the
same frozen source. This is component compatibility evidence, not an installed
v1-to-v2 migration or native recovery trial.

### Independent switch evidence observation (2026-09-24)

P0.2/P0.4, constitution invariants 2 (owner control) and 3 (truthful state): the root-only `recovery dns-switch-status` command now reads the installed private switch journal and service mutation ledger without starting Panel or Agent. It uses the fixed installed path and the root-owned local `/etc/group` CelikPanel identity (no network NSS lookup), a bounded no-symlink/single-link file reader, the historical v1 journal codec and the existing ledger v1 codec. There is no schema or producer transition. The observer requires an exact accepted active job, expired active lease, registered-worker job, expired cancellation, or finalized receipt for the journal identity. Conflicting or unrecognized evidence is unknown and retains the files. A missing journal is reported only as a missing journal, never as completed DNS or healthy service.

This is diagnosis, not a DNS switch recovery executor: each file is pinned while read, but there is no cross-file host lock, worker-liveness proof, native DNS verification, owner-edit protection, inverse or journal cleanup. The same operation must be rechecked under the host and publication locks before any recovery action. Focused tests cover historical Alpha81 journal binding, wrong owner/target/active identity, recorded worker, unexpired and expired cancellation, finalized receipt, malformed journal, local group ambiguity/symlinks and root-only command admission. Native interrupted-switch acceptance and the broader P0.2/P0.4 matrix remain open.
The worker start-token parser now lives in `internal/processidentity` and is shared by the Agent and independent observer. For a recorded worker, the command reports whether the PID/start token matched at inspection time, did not match, or could not be read. The Agent's existing boolean guard retains its previous behavior. A matching instant is not a durable liveness proof or recovery admission; PID/process state and the ledger may change immediately afterwards.
For a coordinated read, `recovery dns-switch-status --quiesced` takes the existing release lock and then the host mutation lock nonblocking, in the same order as installed workers, before reading either receipt. A busy or missing lock is reported as an unmet observation requirement; no new lock is created and no mutation runs. A local two-flock test verifies that a busy host lock releases the previously acquired release lock and a busy release lock stops before host acquisition. The default command remains an unlocked status snapshot that can report while an operation is active. Neither mode grants native recovery authority, and an owner edit outside those locks can still change state.
The independent command now calls the same `dnsenginerecovery.InspectFiles` reader used by its private-directory tests. The reader binds its root to the trusted journal policy and established numeric owner, reads bounded canonical journal and ledger bytes, and distinguishes a missing journal from a present journal with missing, malformed or symlinked ledger evidence. This preserves an unknown result instead of silently interpreting corrupt or incomplete files as no operation. The local file test does not establish installed-server or native DNS health.

### Running owner BIND takeover classification

P0.4 and owner-control invariants 1/2: the historical first-install BIND takeover
is now classified by `internal/dnsengineartifact`, and the Agent delegates to
that same predicate. Its exact initial standalone manifest and frozen
`named.service`/`bind9.service` preimage distinguish an already-running owner
BIND from an ordinary switch. A running takeover must preserve the native DNS
unit during inverse recovery; contradictory or incomplete unit evidence fails
closed instead of selecting a potentially service-stopping inverse.

This moves a recovery decision, not the recovery authority or native inverse.
The caller must still validate the v1 journal and accepted operation under the
host lock and prove current evidence before acting. Historical journal bytes and
phases do not change. Shared classification cases and existing Agent crash-point
takeover tests pass; an independent native executor and installed interruption
acceptance remain open. No installed server was changed.
The shared `dnsenginerecovery.RecoverRunningBINDAdoption` sequence now orders the
fresh runtime evidence capture, owner-aware current configuration proof,
no-stop native inverse, generation-pointer restoration, and final running
service proof. Agent supplies the existing native callbacks, so its crash-point
behavior is unchanged. Any failing callback leaves the surrounding operation
journal for recovery. This pure sequence still cannot establish host authority,
worker exclusion, current native identity or an independent DNS executor.

### Shared switch target receipt comparison

P0.4, invariants 1/2/3: `ExactSwitchTargetStateV1` now owns the Agent's
historical state-versus-journal comparison, including the verified/committed
legacy paired-tuple exception and reinstall's original tenure mode. A changed
owner, generation or modern paired endpoint is not the same target. Agent uses
this common predicate before its existing native BIND/PowerDNS proofs. There is
no journal or state schema transition. A matching receipt is not proof that the
native daemon serves it; independent target verification and full native fault
acceptance remain open.

### Independent current-receipt observation

P0.2/P0.4, invariants 2/3: the root-only DNS switch status reader now pins and
validates the current state document as well as the accepted journal and ledger.
It reports a missing receipt, an exact frozen journal target, or a different
current receipt; malformed or symlinked state yields an unknown result and
retains all evidence. `--quiesced` holds the existing release and host locks
while all three files are read. The ordinary command remains an unlocked
instantaneous observation. Even an exact target receipt does not prove native
BIND/PowerDNS service, zone contents, absence of owner edits, or permission to
run an inverse. No wire schema or installed server changes; the independent
native executor and interrupted-service acceptance remain open.
### Independent frozen-source receipt observation

P0.2/P0.4, constitutional invariants 2/3: the independent root-only DNS
switch observer now uses the same frozen-source state predicate as the Agent.
It distinguishes an exact pre-switch receipt, an absent first-install source
with an absent current receipt, and a different current receipt. In particular,
a foreign owner receipt is not reported as a successful inverse. The historical
v1 journal and current state formats do not change, and no migration or native
mutation is performed. A malformed or symlinked current receipt remains unknown.
The owner must still establish native BIND/PowerDNS configuration, service and
zone health, worker exclusion, and owner edits before resuming the exact
operation. Tests cover the historical Alpha81 BIND-to-PowerDNS journal and all
source/target receipt combinations. Independent native execution and installed
fault acceptance remain open.
### Independent native unit observation

P0.2/P0.4, constitutional invariants 2/3: the root-only DNS switch status
command now extracts the fixed BIND/PowerDNS unit names from a validated
historical journal and reads their current systemd load, active and unit-file
states without Panel or Agent. The local runner accepts only named.service,
bind9.service and pdns.service, uses a fixed systemctl executable, a bounded
three-second property query per unit and strict property/alias parsing. Any
missing, inconsistent or unavailable query makes native status unknown while
the journal and ledger observations remain visible. The two installed lock
files are held through the unit reads when `--quiesced` is requested; ordinary
status remains a point-in-time observation. No journal/ledger/state schema or
native service configuration changes. Tests exercise allowed aliases, unknown
and malformed properties, duplicate/untrusted names, and partial failure.

A systemd `active` property does not establish authoritative DNS answers,
transferred zones, selected BIND generation, PowerDNS database identity or
absence of owner edits. This is diagnosis only; an independent native recovery
executor and interrupted-operation acceptance remain open.