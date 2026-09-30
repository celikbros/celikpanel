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
### Independent frozen-source ownership observation

P0.2/P0.4, constitutional invariants 1/2/3: the exact frozen-source
ownership comparison is now shared by Agent and the independent status reader.
The root-only reader obtains the source engine's per-engine receipt from the
fixed private root with the established owner, no-symlink 0600 reader, then
accepts the canonical v1 or separated v2 ownership document for its semantic
projection. It reports no-source, missing, exact and different receipts
separately; malformed, wrong-engine or symlinked evidence is unknown and
retained. The Agent's existing inverse admission continues to require an exact
source match, with no wire schema or producer transition. Historical Alpha81
source, mixed v1/v2 ownership, changed owner and unsafe file tests pass.

This observation is not proof of native BIND/PowerDNS configuration, zone
answers, owner edits or worker exclusion. It cannot authorize an inverse;
independent execution and installed fault acceptance remain open.

### Quiesced switch evidence stability (2026-09-24)

P0.2/P0.4, constitutional invariants 2/3: the independent root-only DNS
observer now fingerprints the exact installed journal, ledger, current state
and source-ownership bytes, including file presence, after their secured reads.
With `--quiesced`, it reads the evidence and native systemd unit properties
twice while holding the existing release and host locks. A changed byte,
classification or unit property, or an unavailable second read, yields an
unknown result and actionable owner guidance before an earlier snapshot is
printed as stable. The unlocked status command remains an instantaneous
diagnostic. Existing journal/ledger/state/ownership wire schemas and producers
do not change; no migration or native mutation is performed.

The locks only coordinate CelikPanel actors. Matching samples cannot exclude
an intervening owner edit that returns to the same bytes, changes to unobserved
native DNS configuration or zones, later changes, or a surviving worker.
This observation therefore does not admit an inverse or certify service health.
Focused Linux tests cover state changes, each fingerprinted document/presence,
classification changes and native unit drift; race tests and vet pass. An
independent executor, worker exclusion under the mutation barrier, native
configuration/answer proofs and interrupted-switch acceptance remain open.

### Shared native BIND pairing target check (2026-09-24)

P0.4, constitutional invariants 1/2/3: the Agent's target check for a
verified BIND generation now calls a shared recovery predicate. The predicate
requires the BIND target, supported standalone or paired topology, matching
directional local/peer addresses and names, and the frozen primary catalog
serial (or the fixed secondary initial serial). A changed native peer,
journal peer or serial is not the accepted target. The Agent's surrounding
generation, native configuration, zone and operation proofs remain in place.
Historical journal/receipt wire schemas and recovery phases do not change.

This extraction allows an eventual independent executor to apply the same
pairing comparison, but does not itself read the native tree or admit an
inverse. Component boundary tests and the full Agent suite pass. Secure
independent BIND-root verification, PowerDNS native proof, worker exclusion,
native execution and interrupted-operation acceptance remain open.

### Independent managed BIND root provenance observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: the Agent's no-symlink `openat2`,
root-owner, exact-mode and ACL directory checks now call one shared
`internal/bindroot` implementation. The root-only DNS status reader invokes
that verifier for a switch involving BIND. It selects the installed APT or
pacman layout through the supported host profile, reads the service GID from
a bounded local `/etc/group`, proves exact package ownership (and APT's durable
statoverride) with fixed trusted executables, and walks the native BIND root
twice. An unsafe or unavailable proof returns **unknown** with owner action;
it cannot imply that BIND is absent or authorize a new switch.

No persisted schema or producer version changes; recovery remains read-only.
The proof establishes directory-chain and package provenance at observation
time. It does **not** establish the selected immutable generation, BIND's
loaded configuration, zone answers, owner edits, PowerDNS native identity,
worker exclusion or inverse eligibility. Native execution and complete
interrupted-switch acceptance therefore remain open. Focused root-drift,
symlink, package-refusal and local-group tests and Agent/recovery suites cover
this boundary; no installed panel was updated.

### Read-only selected BIND generation comparison (2026-09-24)

P0.4, constitutional invariants 1/2/3: when the current state receipt
exactly matches a frozen BIND switch target, independent DNS status now also
checks the real root-owned `generations` catalog without symlink traversal,
loads the complete selected generation through the existing receipt/hash
verifier twice, and compares generation ID and engine epoch with the frozen
journal. Root, package and catalog proofs surround both reads; a changed or
unreadable tree is **unknown**, with an owner action and no inverse. This adds
no persisted schema or producer transition and never selects a generation or
reloads BIND.

A verified selected tree still does not prove which configuration `named`
loaded, DNS answers, transfer health, absence of owner edits or worker
exclusion. PowerDNS native identity, complete recovery admission/execution
and interrupted-switch fault acceptance remain open. Focused catalog-symlink
and target-identity tests, recovery tests and vet pass; no installed panel was
updated.

### Shared BIND package ownership classification (2026-09-24)

P0.4, constitutional invariants 1/2/3: Agent BIND preparation, its
read-only update preflight and the root-only recovery observer now use the
same exact APT and pacman package-owner parser. APT statoverride results
distinguish an exact durable override, an absent override (exit 1 with no
output), and conflicting or failed output. The Agent alone may create an
absent override during its authorized preparation path; observation and
preflight never do. Pacman accepts exactly one canonical bind ownership
line. This removes a divergent interpretation of the native generation
root without changing the journal, state receipts, phase transitions or
recovery admission.

Malformed, redirected and failed package proofs stop with an error; an
independent observer reports unknown and an owner action. Shared-parser
adversarial tests and Agent/recovery suites pass. Loaded named configuration,
DNS answers, owner-edit detection, worker exclusion and native inverse
execution remain open; this is not P0.4 acceptance.

### Shared secure native-file observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: the Agent's exact vendor-file
reader for BIND and PowerDNS now calls the shared read-only bindroot
descriptor walk. It still requires a trusted root descriptor, canonical
absolute path, root-owned non-writable ancestors, no symlinks or ACLs, an
exact root:root 0644 single-link regular file, bounded bytes, and matching
before/after descriptor metadata. The Agent retains its exact package and byte
comparisons. No journal or state schema changes, native writes, service
reloads, recovery admission or inverse behavior were added. Existing
Agent vendor-file adversarial tests and recovery tests pass.

The independent DNS status reader does not yet compare native BIND
configuration with the selected generation or prove that named loaded it.
Owner edits, DNS answers, worker exclusion and complete interrupted-switch
acceptance remain open.

### Independent BIND vendor-unit observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: the Agent and independent DNS
status reader now use one certified APT/pacman BIND vendor-unit byte and
host-profile contract, plus the same exact package-owner output parsers.
When a retained switch involves BIND, the root-only status reader checks
package ownership and reads the native named unit twice through the shared
no-symlink, root-owned descriptor walk. On APT it also checks the exact
/etc/default/named startup options. A failed or changing proof is unknown
with an owner action; the reader starts no switch or inverse.

The historical switch journal, state receipts, lease and phase schemas do
not change. Existing Agent vendor-file fixtures, shared adversarial profile,
package-output and file-drift tests, Agent/recovery suites and vet pass.
This is a disk and package observation: systemd's loaded unit, the live
named process, managed BIND configuration, DNS answers, owner edits and
worker exclusion remain unproved. Native inverse execution and the
interrupted-switch fault matrix remain open.

### Shared systemd BIND unit identity observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: the Agent's exact systemd
identity parser and APT/pacman named-unit predicates now live in one
shared package. The independent root-only DNS status reader uses a fixed,
bounded systemctl show query for named.service; it verifies the canonical
unit ID, aliases, vendor fragment path, absent drop-ins/source override,
non-transient status and exact ExecStart path and arguments. An enabled
APT bind9 alias must resolve to the same identity. Two identity reads
must agree, surrounded by separate certified vendor-file and package
proofs. Any unknown, changed or foreign identity returns owner guidance
and no inverse. The historical journal and state schemas do not change.

Agent identity tests, independent alias/drift/foreign-executable tests,
Agent/recovery suites and vet pass. This observation does not prove the
running process, NeedDaemonReload state, loaded named configuration,
zone answers, absence of owner edits or worker exclusion. It does not
admit automatic recovery or close the native interrupted-switch matrix.

### Independent selected BIND runtime observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: for a retained switch whose target
receipt and selected immutable BIND generation match the frozen target, the
root-only status reader now uses the Agent's shared exact systemd DNS process
parser. It requires named.service to report a nonzero MainPID, zero ControlPID,
running SubState and no pending daemon reload. On APT, bind9.service must
report the same process state. Two runtime observations must agree. The Linux
process start token and /proc/PID/exe inode must match the current native
named executable path twice; certified vendor files and loaded systemd unit
identity surround those reads. Unknown, changed or foreign state stops with
an owner action and no inverse. No journal/state schema, producer transition,
native service configuration or installed panel changes were made.

Focused alias, reload, PID-drift, malformed-property and foreign-inode tests,
Agent/recovery suites and vet cover this point-in-time boundary. The installed
binary's package bytes, named's loaded configuration, authoritative DNS
answers, owner edits, worker exclusion, native inverse execution and the
interrupted-switch fault matrix remain unproved. This is not P0.4 acceptance.
### Shared native BIND include and disk observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: Agent production and the independent
status reader now use one exact managed zone-include and active-comment
predicate. The observer does not create the missing include: it accepts only
the producer's already-present canonical block pointing at the selected
layout's current/zones.conf path. A shared no-follow descriptor reader
accepts only the fixed APT or pacman native config paths and their certified
root-owned group/mode combinations, checks ACLs and hard links, and binds
bytes to inode, owner and digest across two reads. APT options and anchor
files must share one owner group. The selected immutable generation is
rechecked after native config observation. Unknown, changed or unsafe
configuration returns an owner action and no inverse.

This is an on-disk anchor proof, not a claim that the APT main config includes
that anchor, that named loaded these bytes, that it serves the selected
generation or that DNS answers are correct. Existing owner edits outside the
managed block and package binary integrity remain open. Journal/state
schemas, native files and installed panels were not changed. Root-owned
mode/group/symlink and missing/inert include tests, Agent/recovery suites
and vet passed. Independent native inverse execution and the complete
interrupted-switch acceptance matrix remain open.
### APT BIND main-config include reachability (2026-09-24)

P0.4, constitutional invariants 1/2/3: the independent selected-BIND
observer now reads the fixed /etc/bind/named.conf through the same no-follow,
root-owned native-config reader. A shared lexical predicate requires exactly
one active, top-level include of each APT options and local anchor file.
Comments, quoted strings, nested blocks, malformed statements, absent and
duplicate includes cannot stand in for those active statements. The secure
main, options and anchor file identities must remain unchanged across the
observer's two reads, and the selected generation is rechecked afterward.
Pacman continues to use its one-file config layout. No files are rewritten,
services reloaded, installed panels updated or recovery admission expanded.

This is a bounded lexical on-disk path check, not a complete BIND grammar
parse or proof that named loaded that file. Owner changes elsewhere, package
binary integrity, live authoritative answers, independent inverse execution
and interrupted-switch fault acceptance remain open. Parser adversarial
tests, root-owned fixture tests, Agent/recovery suites and vet passed; no
persisted schema or producer transition changed.
### Bounded local primary catalog answer observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: for a selected BIND target
with a verified primary pairing receipt, the independent status reader
now asks the literal local primary IPv4 address for the receipt's exact
catalog SOA over DNS/TCP without recursion. It first confirms that the
address appears on a local interface, derives the expected catalog name
from that address, and compares the authoritative SOA serial with the
frozen receipt twice. The selected immutable generation is rechecked
afterward. The shared DNS wire parser rejects non-authoritative,
truncated, wrong-question, unrelated and malformed responses. Unknown
address ownership, network failure, wrong serial or a changed target
returns owner guidance and no inverse. Standalone and secondary BIND
receipts have no local primary catalog; the reader reports this as a
non-applicable check, not healthy DNS. No external provider, panel API,
service mutation or installed-panel update is used.

This proves one bounded local authoritative answer at two instants, not
which PID owns the listener, a complete loaded config, member-zone
content, AXFR, secondary convergence, owner changes or recovery
authority. Shared parser and catalog-probe adversarial tests,
Agent/recovery suites and vet pass. Journal/state schemas and recovery
admission are unchanged; native inverse and interrupted-switch fault
acceptance remain open.

### Independent BIND listener inventory observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: the Agent and the independent
status reader now use one strict parser for the native port-53 socket
inventory. For an exact selected BIND target, the reader checks that
all public TCP and UDP listeners belong to the already verified
named.service MainPID. A primary receipt additionally requires both
transports on its literal local IPv4 address or the IPv4 wildcard.
Two bounded read-only ss observations must agree; the systemd process,
start token, native executable inode and unit identity are checked
again afterward and after the primary catalog answer. A foreign process, changed listener set, missing
transport, wrong local address, malformed output or unknown command
result is reported as unknown with owner action. No second DNS
mutation or installed-panel update is started.

The exact 64 KiB output bound, fixed ss arguments, parser rejection
cases and Agent/recovery suites are covered by local tests. The
socket inventory and authoritative SOA answer are complementary
point-in-time observations; they do not establish causal origin of
the answer, loaded named configuration, member-zone content, AXFR,
secondary convergence, complete owner-change exclusion, independent
inverse execution or interrupted native fault-matrix acceptance.
Journal/state schemas, producer transition and recovery admission
are unchanged; P0.4 remains open.

### Independent selected PowerDNS runtime observation (2026-09-24)

P0.4, constitutional invariants 1/2/3: an exact selected PowerDNS
target now has a separate native process and listener observation in
the root-only status reader. On a verified APT/systemd host, the
reader twice requires pdns.service loaded, active and enabled, with
named.service and bind9.service exactly inactive/disabled, absent or
masked. It also requires both BIND MainPIDs to be zero/dead, one
stable running PowerDNS MainPID, no pending daemon reload, a stable
Linux process start token and the same inode as the fixed installed
/usr/sbin/pdns_server file. The shared strict ss parser then requires
both public port-53 transports to belong to that MainPID; process
identity is rechecked afterward. An unknown or changed result stops
the observation with owner guidance, without an inverse or another
switch.

The reader now also shares the Agent's exact PowerDNS vendor unit identity
predicate. It rejects a drop-in, transient unit, alternate ExecStart or
different argv and compares the loaded systemd identity around the
runtime observation. This is still not the Agent's full vendor/config/database
proof. The independent reader now confirms the fixed pdns-server package
ownership and exact Debian 13 or Ubuntu 24.04 vendor unit bytes through
two secure no-follow reads, with package ownership observed before and
after. The Agent and reader share the accepted bytes and owner predicate.
The live SQLite database and zone records, loaded configuration,
replication and owner edits remain unproved. It does not admit or run
native recovery. Historical journal v1, state receipts and current
recovery decisions remain unchanged. Root-owned vendor fixture, topology, shared identity/parser and
Agent/recovery tests plus vet are the scoped evidence; installed
native interruption and inverse acceptance remain open.
### Shared native DNS inverse classification (2026-09-24)

P0.4, constitutional invariants 1/2/3: a validated historical switch
journal now reconstructs its immutable manifest through one shared reader.
Both the Agent rollback and the independent root-only status reader classify
the frozen native inverse as BIND switch, running BIND adoption, PowerDNS
switch or PowerDNS adoption. A running owner BIND is routed to the
restore-and-reload adoption path, not the first-install stop path. Contradictory
loaded BIND aliases and a manifest mismatch fail closed before that dispatch.

The status reader reports the classification as an observation only. No
journal, receipt, ledger or phase schema changes; no new admission, native
writes or worker-exclusion mechanism. Existing owner edits and service-state
proofs still govern Agent rollback. Historical Alpha81 fixture, contradictory
unit-preimage and Agent/recovery tests cover this decision boundary.
Independent native inverse execution and interrupted-switch fault acceptance
remain open.
### PowerDNS inverse stops at the first failed native step (2026-09-24)

P0.4, constitutional invariants 1/2/3: the PowerDNS switch inverse now uses
one shared ordered recovery sequence. It requires the target stop to succeed
before touching the live SQLite database, then restores the frozen database,
configuration, state receipt, target unit and source unit in order. A failed or
interrupted step withholds all later effects and returns its exact cause; the
existing rolling-back journal stays for the same operation's recovery. The
Agent still supplies native effects, owner-aware preimage checks and final
source verification.

This changes no journal, receipt, ledger or phase schema and adds no
independent mutation authority. An Agent-independent host adapter, native
crash/reboot trials and full interrupted-switch fault matrix remain open.
Injected failures at every boundary, cancellation after stop and Agent
tests are the scoped evidence. They do not prove that a real failed
database or unit restore will automatically finish after reboot.
### Managed BIND inverse stops at a failed predecessor (2026-09-24)

The managed BIND activation inverse now uses a separate shared ordered
sequence: restore frozen target-unit state, owner-aware configuration,
state receipt and source-unit state. A failed or interrupted predecessor
withholds later effects and retains the same rolling-back journal. This
does not apply to takeover of a running unmanaged owner BIND; that path
keeps its existing restore-and-reload sequence. The target-unit preimage
can itself be active in a managed reconfiguration, so this change proves
ordering and failure isolation, not uninterrupted DNS service.

No persisted schema, producer transition or installed host changes.
Injected failure at every step and the Agent suite cover this component
boundary. An independent native adapter, owner-edit and reboot fault
matrix, and complete P0.4 acceptance remain open.
### PowerDNS stop readback before database inverse (2026-09-24)

The ordered PowerDNS rollback now has an explicit read-only stop proof between
the native systemctl stop result and the SQLite restore. The Agent reads
pdns.service unit state and process properties twice, requiring inactive
state, zero MainPID/ControlPID, dead SubState and identical unit/process
observations. Failure or drift retains the accepted rollback journal and
withholds the database and all later effects. This avoids interpreting a
successful stop command alone as proof the database is no longer served.

The proof is point-in-time. It does not exclude a foreign database writer,
a later owner restart, or establish independent recovery. No persisted
schema or installed host was changed. Failed-stop, failed-readback, live
PID, unit drift and ordered inverse tests are component evidence only;
native interrupted-switch acceptance remains open.

### Exact worker exclusion and orphaned switch authority (2026-09-25)

A registered DNS switch worker now has three outcomes during Agent recovery:
the exact process still lives, the kernel procfs proves that PID is absent or
reused, or its identity is unknown. Unknown retains the operation's host lock
and frozen journal and runs no native inverse. The independent read-only
status command reports the same distinction without granting recovery
authority. The procfs check is repeated if the process entry disappears
during observation.

Agent and independent evidence classification now share the exact accepted
switch identity, including the historical waiting_for_orphaned_process
record that Agent writes for its still-live worker. Only the matching request,
owner, target, qualifier, worker identity, phase and reason are accepted.
Agent checks that authority before native reconciliation and before publishing
a recovered committed switch; an inconsistent cancellation is rejected before
native effects. No persisted schema, installer or live server changed.

The secured evidence reader now carries a value copy of that exact accepted
ledger job alongside the frozen journal. Agent boot recovery and the locked
independent status path call one worker-exclusion predicate for running,
expired, cancelling and orphaned jobs. It refuses mismatched job shapes and
unknown process identity before reporting absence; a still-live worker is
reported separately. The status path remains read-only, and this point-in-time
predicate cannot grant an inverse or exclude a later owner restart.

Tests cover unreadable procfs/process state, live and exited workers, changed
procfs, exact orphan completion and foreign cancellation evidence. These are
component and local startup tests. They do not establish owner-edit exclusion,
an Agent-independent native inverse or the interrupted-switch fault matrix.

### Shared PowerDNS adoption inverse sequence (2026-09-25)

The Agent now calls the same bounded fail-stop PowerDNS adoption rollback
sequence that an independent executor can use after establishing its own
accepted operation, lock and native owner proofs. The sequence proves the
existing authoritative configuration, restores only the frozen engine-state
snapshot, and verifies the restored PowerDNS authority. Cancellation is
checked before and after every step. Failure withholds successors and retains the
same rolling-back journal for exact replay. This extraction neither grants
an independent CLI authority nor changes native service lifecycle, v1
journal/ledger/state bytes or the installed host.

### Shared durable journal checkpoint protocol (2026-09-25)

P0.4, constitutional invariants 2/4: the Agent and a future independent
executor use the same bounded v1 switch-journal checkpoint protocol.
It validates and encodes the frozen journal, brackets the existing fault
hooks, and requires exact secure readback. If publication reports an error
after a rename, exact readback accepts that single checkpoint; absent or
different bytes retain the error and the operation stays unresolved.
The Agent still supplies the secure filesystem adapter and host lock.
This extraction adds no independent native effects, lock acquisition,
migration or new authority. Existing fault ordering and full Agent tests
passed; native interrupted-switch acceptance remains open.

The shared rollback sequence now retains its exact rolled-back journal;
it does not unlink it before the outer mutation ledger publishes a failed
terminal verdict. The direct BIND and PowerDNS switch/adoption producers retain the same checkpoint.
The Agent's exact cleanup re-reads that checkpoint and
requires the same request, owner, target and qualifier in the failed ledger
job. An uncertain unlink is accepted only after absence readback. On boot,
the retained checkpoint drives native inverse reproof before cleanup. This
closes the early-discard interval at the component boundary, but a native
kill/reboot matrix, owner-edit race trial, filesystem-level conditional
removal and Agent-independent inverse remain open.


### BIND first-activation inverse stop guard (2026-09-25)

P0.4, constitutional invariants 1/2/3: restoring a BIND target unit to its
inactive preimage no longer proceeds directly to configuration restoration.
The Agent now makes two native observations of named.service's inactive state,
dead SubState and zero MainPID/ControlPID after the unit restore. A failed or
changing observation leaves the rolling-back journal in place and withholds
configuration and source-state writes. The running-BIND preimage path is not
subject to this stopped-target guard; it retains the existing owner-aware
restoration and final native proof. The v1 journal, state and ledger schemas
and their publication order are unchanged. The same accepted operation can
retry its inverse after the native issue is resolved.

Focused and wider DNS/BIND/PowerDNS package tests passed. The scoped
[Debian native regression](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-STOP-GUARD-20260925.md)
repeated a real SIGKILL before the rolled-back journal write and passed
same-request target convergence and 30-second DNS/management health. It is
one repeated matrix cell, not new coverage. This guard does not
exclude an independent owner restart after the read, detect a stray process
outside systemd's MainPID/ControlPID, provide an Agent-independent executor or
prove the native owner-edit/reboot matrix. P0.4 remains open.


### Shared native DNS stopped-target observation (2026-09-25)

P0.4, constitutional invariants 1/2/3: BIND first-activation rollback and
PowerDNS switch rollback now use one read-only
`dnsenginerecovery.VerifyStoppedUnit` predicate. Their fixed-name systemd
adapters supply two observations of the target's unit properties and
MainPID/ControlPID/SubState. Only matching inactive/dead/zero-PID observations
let their existing inverse sequences proceed to configuration or database
restoration. A changed, unknown or active unit leaves the original rolling-back
journal for the same operation. There is no journal, ledger, DNS receipt or
installer schema transition.

The shared-package and complete Agent tests plus vet passed. The earlier
[real Debian BIND interruption](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-STOP-GUARD-20260925.md)
tested the preceding identical Agent guard, before this extraction; it is not
native acceptance of the new shared wiring. The predicate does not inspect
cgroup descendants, exclude later owner starts, prove DNS answers or supply
an Agent-independent host-effects executor. P0.4 remains open.

### Quiesced retained-rollback stop diagnosis (2026-09-25)

P0.4 and constitutional invariants 1/2/3: the independent `recovery dns-switch-status --quiesced` observer now selects only an exact terminal rolled-back BIND or PowerDNS switch whose frozen target unit was originally inactive. Under the existing release and host locks, it reads that fixed native unit and its process properties twice. A loaded, matching inactive/dead unit with zero systemd main/control PIDs yields a point-in-time diagnostic; a missing unit, pending daemon reload, active process, changing observation or changing journal/receipt fails closed and retains the journal. The ordinary status command is unchanged. This adds no host effect, inverse authority, journal/ledger/receipt schema transition or automatic retry. Package tests and vet are scoped evidence; cgroup descendants, later owner edits, native DNS health and an Agent-independent recovery executor remain open.

The [post-extraction native regression](../deploy/e2e/dns-kill-matrix/NATIVE-SHARED-STOP-GUARD-20260925.md) subsequently repeated the already passed BIND rolled-back/before-write standalone cell using commit `d4b8c0e`: exit 137, same-request BIND convergence and 31/31 healthy Agent/Panel/authoritative UDP+TCP samples. It does not add a matrix cell or prove the independent observer, stop-guard rejection, owner-edit races or independent inverse execution.

The shared stopped-unit predicate now also rechecks context cancellation after each native observation. A cancellation arriving while an observer is running withholds the stop proof, so Agent inverse and independent status both fail closed. This changes no persisted schema or phase. The affected package tests passed; the earlier native tests did not inject mid-observation cancellation and do not prove owner-start exclusion.

### Recursive cgroup stop observation (2026-09-25)

P0.4 and constitutional invariants 1/2/3: both Agent DNS inverses and the independent quiesced rollback-status reader now require a fixed `named.service` or `pdns.service` systemd identity in `system.slice` and a cgroup-v2 `populated 0` event, or a missing service cgroup paired with an empty systemd `ControlGroup`. This is checked in each of the two loaded/inactive/dead/zero-PID observations before a stopped target's config/database restoration. A present populated group, foreign slice or cgroup, missing v2 controllers, malformed events, disappearing reported group, or changing observation fails closed and retains the same journal. No new phase, ledger, journal or DNS receipt schema was introduced. The stop observation does not exclude a later owner restart or itself authorize any independent host effect.

Affected package tests and vet passed. The [native cgroup regression](../deploy/e2e/dns-kill-matrix/NATIVE-DNS-CGROUP-GUARD-20260925.md) repeated one existing Debian 13 BIND interruption with exit 137, same-request convergence and 31/31 healthy post-recovery samples. Real active/stopped systemd/cgroup-v2 field shapes were inspected after that result. Deliberate native cgroup population during inverse, owner-edit race, independent inverse execution and the remaining matrix are still open.

### Independent rollback checkpoint publication boundary (2026-09-25)

P0.4 and constitutional invariants 1/2/4: the shared recovery package now
provides `ReplaceRollbackJournalPhase` for an already established v1 DNS switch
journal. It permits only an exact frozen-journal transition into `rolling-back`
or from `rolling-back` to `rolled-back`. The Linux evidence adapter requires
the established 0700 directory and 0600 single-link file owner, compares the
complete preimage, publishes a synced temporary file by atomic rename, syncs
the directory and securely reads the exact result. Missing, foreign, unsafe or
changed evidence is retained; it is never created or normalized. An uncertain
post-publication error is reconciled by the existing exact journal readback.
The caller still has to hold the release and host locks and prove the accepted
ledger job and native inverse. No installed journal, ledger or DNS receipt
schema, phase name or Agent producer path changed.

Focused adversarial tests cover absent/changed evidence, symlink and hardlink
paths, wrong owner, untrusted directory and prepublication content/inode/
directory replacement. Full affected Agent/recovery tests and vet passed. This
is a publication primitive, not a callable independent inverse. It cannot
serialize uncooperative administrator root edits outside the host locks; native
owner-edit/reboot trials, the inverse executor and complete matrix remain open.

The shared `Reconcile`/`Rollback` phase writer now receives both the observed
preimage and requested next journal. This makes the exact replacement adapter
usable without reconstructing the old phase from mutable caller state. A
filesystem-backed interruption test retains `rolling-back` after an injected
inverse error, then resumes the same request and persists `rolled-back`.
The Agent's existing secure writer is still its production adapter; this API
change does not claim native inverse execution in the independent CLI. The
v1 format and phase names are unchanged.

The shared rollback sequence now changes its in-memory journal phase only after
the checkpoint writer returns a verified success. An uncertain first write
cannot cause the same call to start the inverse; an uncertain terminal write
leaves the caller at `rolling-back` and requires re-reading persisted evidence.
Focused failure-injection tests cover both boundaries. This does not change
v1 data or replace the remaining native interruption trials.

### Exact terminal journal retirement boundary (2026-09-25)

P0.4 and constitutional invariants 1/2/4: the shared recovery package now
has an Agent-independent adapter for retiring an exact `rolled-back` v1 DNS
switch journal. It uses the same private-directory and single-link 0600 owner
contract as checkpoint publication, compares the entire preimage, unlinks by
its trusted directory descriptor, syncs that directory and reads back absence.
Missing, modified, hard-linked, symlinked, foreign-owner or nonterminal
journals remain for review. The existing uncertain-unlink protocol verifies
absence, but the caller must separately prove the exact failed ledger verdict,
host locks and restored native DNS before cleanup. No new persisted format or
phase is introduced.

Adversarial filesystem and terminal-phase tests, affected Agent/recovery tests
and vet pass. This adapter is not yet called by an independent CLI and does not
complete a native inverse or its reboot/owner-edit matrix. P0.4 remains open.

### Quiesced PowerDNS adoption preimage observation (2026-09-25)

P0.4 and constitutional invariants 1/2/3: `recovery dns-switch-status --quiesced` now compares the fixed installed PowerDNS database with the accepted adoption journal's frozen byte count and SHA-256. A Linux `openat2` read rejects symlinked ancestors/final paths; the reader requires a regular single-link file, bounded context, unchanged file/path identity and metadata, and exact bytes. Under the existing release and host locks, the observer reads the database twice around a stable certified `pdns.service` process and local TCP/UDP port-53 listener proof, then rereads the same private evidence. A mismatch is unknown and retains the operation; no database, DNS service or journal is changed.

This is an observation of file bytes and native process/socket ownership at one point in time. It does not prove SQLite transaction state, authoritative zone answers, loaded configuration, later owner edits, safe source restoration or an independent inverse. The Agent now uses the same no-follow, single-link database byte reader for its PowerDNS adoption and switch checks; its v1 journal/state/ledger producer formats are unchanged. Linux package tests and vet cover this read-only path; a [native quiesced-adoption observer trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-QUIESCED-OBSERVER-20260925.md) has now passed at one interrupted rollback boundary before Agent restart. That observation is read-only and does not prove SQLite/zone state, owner-edit safety or an Agent-independent inverse. P0.4 is not complete.

### Bounded independent DNS observation (2026-09-25)

P0.2/P0.4, constitutional invariants 2/3: the independent `recovery dns-switch-status` native probes now share one 30-second context deadline, including both quiesced unit reads, stopped-target checks, BIND and PowerDNS process/listener checks, and the shorter adoption database preimage check. A deadline reached during evidence reading or after the final probe returns an unknown/unavailable observation with owner guidance; it never becomes a successful DNS operation. File readers remain individually size/metadata bounded, and the lock is nonblocking. This changes no journal, ledger or receipt format and grants no inverse authority. The deadline does not bound output-writer stalls or owner edits outside the locks; native fault/reboot and independent inverse acceptance remain open.

### Exact DNS request status after journal retirement (2026-09-25)

P0.2/P0.4, constitutional invariants 2/3/6: the root/sudo
`recovery dns-switch-status --quiesced --request-id <32-hex-id>` observer can
report the recorded status of one exact DNS switch job when its private switch
journal is absent. It takes the existing release and host locks, verifies
journal absence around two byte-identical secure reads of the canonical
service-mutation ledger v1, validates the DNS switch operation identity and
rejects a different active mutation. A present journal must match the selected
request. Missing, changing, malformed or unrelated evidence is unavailable,
not a DNS result. The output omits unbounded phase and error text and says that
ledger status does not prove native DNS health, completed recovery or inverse
authority. Without a request ID, the existing missing-journal message remains
unchanged. No persisted format or producer changes; there is no host mutation.
Linux package tests cover identity, unrelated active work, malformed scope and
journal presence. A fresh [native exact-request trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-EXACT-REQUEST-20260925.md)
observed the retained journal before Agent restart and the same failed ledger
job after ordinary Agent startup retired that journal. Agent-independent inverse,
owner-edit/reboot fault acceptance and complete P0.4 remain open.

### Journal-free historical DNS switch receipt after reboot (2026-09-28)

P0.2/P0.4, D-025 invariants 2/3: root can now request
`recovery dns-switch-status --request-id <32-hex-id>` without `--quiesced` to
read **only a terminal historical ledger result when no switch journal exists**.
It performs the same bounded, ownership-checked double reads of the canonical
ledger v1 and journal, validates the exact switch identity, rejects any active
ledger request and refuses nonterminal, changing or malformed evidence. It
creates no volatile lock and never probes or claims current native DNS health.
The fixed rollback code and exact message distinguish the original failed
switch from its separately recorded successful owner rollback; unknown text is
not echoed. A present journal still requires the existing `--quiesced` path.
No persisted schema, producer, native service or recovery decision changed.
Focused Linux tests and vet passed. The preceding [native inverse
trial](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md)
found the missing-lock problem. A separate [post-reboot status trial](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-recorded-status-20260928/README.md) used that exact canonical terminal ledger in a fresh disposable Debian VM; the read returned the same historical verdict after reboot without creating the missing lock. This was a materialized ledger, not a repeated producer/inverse or installed-kit test. It grants no inverse authority; P0.4 remains open.

### Shared PowerDNS adoption row proof (2026-09-25)

P0.4, constitutional invariants 1/2/3: the Agent's existing read-only
PowerDNS adoption transaction check now calls a shared SQL verifier. The
independent root-only `recovery dns-switch-status --quiesced` observer
reconstructs the frozen journal manifest and runs the same zone, peer-row and
SQLite `quick_check` comparison inside a read-only transaction, bracketed by
secure exact database-byte reads. A changed row, extra authority, malformed
database or unsafe database path returns unavailable and retains the original
operation. The installed state, journal, ledger and DNS receipt schemas are
unchanged; this observation writes no DNS record and grants no inverse authority.

Affected Agent, recovery and shared-package tests plus vet pass. Real SQLite
tests accept an exact zone and refuse a later record edit even when its new
database digest is supplied; they also reject an unowned extra zone, wrong
frozen digest and symlinked path. A [disposable native SQL observer
trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-SQL-OBSERVER-20260925.md)
passed at one interrupted adoption rollback boundary. Live authoritative
answer/loaded-config proof at the observation instant, owner-edit race, and
Agent-independent inverse remain open. P0.4 is not complete.

### Read-only PowerDNS adoption answer proof (2026-09-25)

P0.4 and constitutional invariants 1/2/3: the quiesced independent observer now selects a concrete local IPv4 address covered by the already-verified native PowerDNS TCP/UDP port-53 listener inventory. It reconstructs the accepted adoption manifest, extracts each active zone's single enabled apex SOA serial, and requires an exact nonrecursive authoritative answer over both UDP and TCP at that literal endpoint. Missing, truncated, ambiguous, wrong-serial or timed-out answers return unavailable and leave the original operation and native DNS untouched. Deleted-zone absence is explicitly counted as unproved. Process identity, listeners, exact database bytes and private journal are rechecked after the queries under the existing release and host locks. This narrows observation only; no independent inverse is admitted.

The existing state, ownership, journal and ledger schemas remain v1, with no new producer or migration. Linux unit tests cover endpoint selection, transport-specific serial mismatch, invalid frozen SOA, truncation and deadlines. A [disposable native SOA observer trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-SOA-OBSERVER-20260925.md) passed at one SIGKILL boundary before Agent restart. Other record values, deleted-zone absence, loaded config, answer/socket causality, later owner edits and an Agent-independent inverse remain open. P0.4 is not complete.

### Independent PowerDNS adoption config observation (2026-09-25)

P0.4 and constitutional invariants 1/2/3: the root-only quiesced DNS observer now compares the accepted adoption journal's fixed PowerDNS config snapshot set with the installed files using no-symlink descriptor walks. It requires exact root-owned non-ACL parent directories, the local `pdns` service group, regular single-link files, frozen bytes, mode and ownership, and stable path/inode metadata over two secure passes. It repeats the group and config proof around the native UDP/TCP SOA observation. A changed, missing, symlinked or unexpectedly present config returns unavailable with owner guidance; the accepted operation and native DNS remain untouched. This proves on-disk configuration only, not the daemon's loaded configuration or independent inverse authority.

The journal, state, ownership and ledger remain v1; no producer, migration, installed host or recovery write transition changed. Linux owner/symlink/parent-edit tests and [one disposable native SIGKILL trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-CONFIG-OBSERVER-20260925.md) pass. Loaded-config proof, later owner-edit exclusion, secure Agent-independent inverse and remaining native fault coverage remain open. P0.4 is not complete.

### Final quiesced DNS observation after native probes (2026-09-25)

P0.4 and constitutional invariants 2/3: the independent root-only status command
now repeats the accepted journal/ledger/receipt read and native systemd unit
inventory after its longer read-only DNS probes. It also rechecks the exact
recorded worker before reporting a stable quiesced observation. A changed
receipt, unit or live/unknown worker returns unavailable and preserves the
operation; no host effect occurs. The existing v1 formats and recovery behavior
remain unchanged. Linux package tests and vet pass. This is a point-in-time
observer guard, not owner-edit exclusion or independent inverse admission;
those and the remaining native fault matrix keep P0.4 open.
### Exact PowerDNS adoption state receipt inverse primitive (2026-09-25)

P0.4 and constitutional invariants 1/2/3: the shared recovery package now
has a narrow Linux removal primitive for a rolling-back or rolled-back
PowerDNS adoption whose frozen source state was absent. It validates the
accepted v1 journal against its caller-supplied trusted path policy, reads the
current private receipt securely, and removes it only if it semantically names
the exact target request, owner, epoch and manifest. The removal itself uses
the existing byte-exact, no-symlink, single-link CAS unlink with directory
sync and absence readback. A changed or foreign receipt, unsafe path or
nonrollback phase is retained; an already absent receipt is an idempotent
no-op, never proof that native DNS was restored.

This helper is not wired to the recovery CLI. Its caller must still hold the
release and host locks, establish ledger authority and worker exclusion,
reprove native PowerDNS before and after the write, durably publish the
rolled-back journal and terminal ledger result, and retire the exact journal.
Journal, ledger, ownership and state formats remain v1. Linux tests cover
exact removal, retry, foreign owner/request/epoch, symlink and wrong phase.
Agent-independent inverse execution, owner-edit/reboot native trials and the
remaining P0.4 matrix remain open.
### Exact independent DNS rollback ledger verdict primitive (2026-09-25)

P0.4 and constitutional invariants 1/2/3: the shared Linux recovery package
can now publish one terminal failed verdict for an exact active DNS switch
whose durable journal is already `rolled-back`. It checks canonical journal
bytes before and after publication, decodes the exact active v1 ledger job,
rechecks the recorded worker against kernel procfs, and uses byte-exact CAS
replacement with directory sync/readback. Only the matching job is closed;
the frozen journal remains for separate native reproof and exact retirement.
A live worker, different active job, changed journal, unsafe evidence or a
second publication is refused. A journal change after the ledger CAS produces
an unknown result with the terminal ledger retained for reconciliation.

This is a low-level publication primitive, not an owner recovery command.
The caller must still hold both locks and prove the native inverse before
publication. No schema changes or installed-host actions occurred. Linux tests
cover exact closure, retained journal, foreign journal/job, duplicate verdict
and a genuinely live recorded worker. Native reboot/owner-edit trials and the
Agent-independent executor remain open; P0.4 is not complete.
### PowerDNS adoption inverse transaction sequence (2026-09-25)

P0.4 and constitutional invariants 1/2/3: the shared recovery package now
has a fail-stop coordinator for an already durable PowerDNS adoption rollback.
It requires a stable exact evidence reread and worker exclusion around native
source proof, removes only the exact target state receipt, verifies source
absence and native PowerDNS again, checkpoints `rolled-back`, publishes the
same job's failed ledger verdict, then re-proves native service before retiring
the exact journal. A failure leaves the last durable checkpoint for a repeat
of that request. An unexpected target receipt, journal phase, engine/epoch,
ledger status or owner change stops before the next effect.

The coordinator receives privileged callbacks; it does not acquire locks,
choose rollback from an uncertain target, or expose a recovery CLI. The
existing v1 state/journal/ledger formats are unchanged. In-memory interruption
tests cover the state, phase, ledger and cleanup boundaries with same-request
continuation; the exact Linux state and ledger CAS adapters have separate
filesystem tests. Wiring fixed installed paths, native proof, release/host
locks and worker exclusion into a supported owner command, then passing
disposable native interruption/reboot/owner-edit trials, remain open. P0.4 is
not complete.
### Shared read-only PowerDNS adoption source proof (2026-09-25)

P0.4, constitutional invariants 1/2/3: the quiesced DNS observer now calls one
bounded native-source proof for an exact PowerDNS adoption journal. It requires
`named.service` and `bind9.service` inactive, `pdns.service` loaded and active,
then checks the frozen native config and service group, database bytes and SQL
rows, certified PowerDNS process, sole public TCP/UDP port-53 listener, and
active-zone authoritative SOA answers over both transports. It brackets the
answer with repeated process, listener, config, database and systemd reads; the
caller rereads the accepted journal, ledger and receipt under the existing
release and host locks. A mismatch reports unavailable and keeps the operation.

This refactor shares the proof with a future narrowly admitted owner recovery
executor; it does not add a CLI effect or permit an inverse. The state, journal
and ledger remain v1 with no producer or migration change. Focused Linux tests
and vet pass. One [disposable native SIGKILL trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-SHARED-NATIVE-PROOF-20260925.md)
repeated an existing adoption rollback cell and proved the read-only shared
source observation before Agent restart; it did not call the dormant inverse.
Loaded config, deleted-zone absence, later owner edits, independent inverse
execution and the full interruption/reboot matrix remain open. P0.4 is open.
### Installed-path PowerDNS adoption inverse binding (2026-09-25)

P0.4, constitutional invariants 1/2/3/6: the recovery binary now has a dormant
adapter that binds the accepted PowerDNS adoption inverse transaction to the
fixed installed journal, state and ledger paths. It requires an exact request
ID, local root-owned CelikPanel group, release-then-host lock acquisition,
secured evidence reads, recorded-worker exclusion and the shared native
PowerDNS source proof before the exact state, phase, ledger and journal effects.
The sequence also rereads exact terminal evidence and excludes the worker after
its last native proof and immediately before journal retirement; a late owner
edit retains the journal. Each effect remains a bounded exact CAS operation.

There is deliberately no CLI dispatch for this adapter yet. This code has not
been exercised against a disposable native adoption rollback with injected
interruptions and reboot, and it must not be used on installed servers. The
dormant inverse now refuses a frozen deletion until native absence is proved;
SQL row absence alone cannot show that a running daemon stopped answering.
The read-only observer still reports the unproved deletion count. The state,
journal and ledger remain v1; no schema, migration or Agent behavior
changed. Linux tests cover accepted/foreign/unknown-worker and terminal shapes,
invalid request admission, and a final owner-edit race. Package tests and vet
pass. Native reboot, owner-edit and full interruption acceptance remain open;
P0.4 is not complete.

Agent/independent deleted-zone wire parity (P0.4, invariants 1/2/3): normal Agent DNS verification now marks a deleted-zone SOA probe valid only when the same shared strict packet validator used by independent recovery accepts it. A non-authoritative NXDOMAIN or REFUSED, including REFUSED with a parent SOA, is unknown rather than proof of deletion. The normal Agent and peer SOA paths report absence as unverified when this proof fails. This changes no persisted v1 DNS state, ownership, journal or ledger schema; an accepted operation is retained for exact-operation recovery, with no new retry or inverse authority. Agent and dnswire tests pass. The earlier native deleted-child trials used the preceding Agent build, so native parity on this build and the wider reboot/owner-edit matrix remain open.

A [fresh native Agent/independent deletion-parity trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-AGENT-DELETION-PARITY-20260925.md) exercised the corrected Agent against a real parent/absent-child PowerDNS source after SIGKILL. The independent observer proved 1/1 deleted children before Agent restart; the ordinary same-request retry converged forward and 31/31 health samples passed. This closes the specific native parity gap for that reply and boundary, not the REFUSED owner-policy matrix, independent inverse, reboot or owner-edit acceptance. Persisted v1 formats remain unchanged.

A [fresh disposable PowerDNS owner-edit trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-OWNER-EDIT-20260925.md) now exercises a static post-SIGKILL native config edit. The independent fixed quiesced observer and ordinary Agent retries refuse the changed bytes, retaining the owner comment and accepted journal while native DNS stays active. The test-only controller injection is recorded; production binaries and persisted v1 formats did not change. This proves one fail-closed boundary, not a concurrent effect-point race or independent inverse authority.

### Owner recovery for an interrupted external PowerDNS adoption (2026-09-26)

For an already accepted **external PowerDNS adoption** that was interrupted and
has an exact request ID, the server owner may first inspect the operation with:

```sh
sudo /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id <32-lowercase-hex-request-id>
```

The status command is read-only. Use its result when available; after reboot it
may report that `/run` lock state is missing. In that case, the owner recovery
command below recreates only its required runtime lock path while holding the
release lock. Run it only for this exact accepted, interrupted adoption:

```sh
sudo /usr/libexec/celikpanel/recovery recover-dns-pdns-adoption --request-id <32-lowercase-hex-request-id>
```

Use the same request ID shown by the accepted operation. Do not start a new DNS
switch or update to retry it. Unknown, conflicting, changed or owner-edited
evidence fails closed and is retained for diagnosis; do not work around that
refusal or replace the request ID. A response that reports only a historical
retry/verdict does not prove present DNS health. Check the native PowerDNS
service, configuration and authoritative DNS answers separately after recovery.

The [bounded Debian 13 native acceptance](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-PROTECTED-OWNER-CLI-20260926.md) has now passed for this
selected external PowerDNS adoption path on the same protected candidate: after
an actual CLI interruption and reboot, the exact request completed with a
terminal ledger verdict and journal retirement; a historical retry explicitly
reported current health as unknown. A separate same-candidate owner-edit trial
failed closed while preserving the edit and evidence, and native PowerDNS
continued authoritative UDP/TCP answers. The tested worktree was dirty and this
is not release-grade signed-candidate provenance. Other DNS inverse kinds, the
full fault/workload matrix, other platforms and overall P0.4 remain open. No
installed server was changed. The retained report SHA-256 is `8147b644ea9024630a4a1a5b90bb423193d49a9119ed35e47ceeb499754272e7`.

### Owner recovery for an interrupted PowerDNS-to-BIND switch (2026-09-27)

P0.4, constitutional invariants 1/2/3/6: the protected owner command now supports
one additional inverse: an accepted standalone Debian/apt switch from managed
PowerDNS to initially inactive BIND, already durably in `rolling-back` or
`rolled-back`. It requires a V2 journal with exact config before/after images,
the unchanged fixed Debian main/include envelope, and frozen PowerDNS config
and logical database proof. Legacy V1 and older incomplete V2 evidence remain
readable; this command does not manufacture their missing recovery material.

```sh
sudo /usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id <32-lowercase-hex-request-id> --lang en
```

Use only the request ID of the accepted interrupted switch. The selected
protected runtime must advertise `celikpanel-bind-source-inverse/v1` before
the producer publishes this V2 intent. Its lease prevents incompatible runtime
replacement during the operation; retained source-proof V2 journals require
the `bind-source-inverse-plan-v2` application contract.

The inverse excludes the recorded worker under release/host locks, checks
native ownership and owner changes, stops only the operation's BIND target,
restores the exact configuration/pointer/source receipt, and re-enables the
frozen PowerDNS unit. It never restores or rewrites the PowerDNS database.
Terminal publication requires fresh native PowerDNS and authoritative UDP/TCP
proof. Evidence is retained on refusal; do not delete it or start a new switch
to bypass a conflict. A retry after journal retirement reports the historical
verdict with current health unknown and makes no new DNS change.

The [Debian 13 native trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md)
proved a real late producer SIGKILL while BIND served and PowerDNS was stopped,
preserved an unreloaded owner config edit, interrupted the protected inverse
after its durable restored checkpoint, and completed the same request after
an orderly reboot with Panel and Agent disabled. This is bounded local
worktree evidence, not signed-release, power-loss, continuous-service or
all-platform acceptance. Running-BIND adoption, PowerDNS switch/reinstall,
Arch, paired and empty-source variants remain outside this command's scope.

### Running-BIND adoption owner inverse scope (2026-09-27)

The protected `recover-dns-bind-adoption` command has one bounded native acceptance result: rollback of an interrupted adoption from an initially running owner-managed BIND service on Debian 13 with a static default-view zone. The V2 inverse plan requires `SourceBIND` and excludes `SourcePDNS`. Admission is separately gated by `check-bind-adoption-inverse-v1`, and the new source evidence uses `bind-adoption-inverse-plan-v2`; the existing V1 or BIND-target V2 PowerDNS-source plan is not authority for SourceBIND restoration. The plan binds owner configuration, source zone file bytes/metadata and authoritative SOA serials over UDP and TCP; it does not prove the full zone RRset. Recovery restores owner configuration and generation through reload without stopping or restarting `named`.

The [final native trial](../deploy/e2e/dns-kill-matrix/NATIVE-BIND-ADOPTION-OWNER-CLI-20260927.md) proves producer exit 137 at `rolling-back/after-write`, refusal of one same-serial owner zone edit while retaining the pending install receipt and other evidence, protected CLI interruption after durable `rolled-back`, same-request terminal recovery, and a historical retry with no effects/current health unknown. The final read-only probe validly observed `rolled_back_source_active` with `converged=false`; the controller handoff itself remains unverified because its matrix assertion expects forward convergence. Named stayed active at the same process identity; separate UDP/TCP A checks were authoritative. The earlier archive with SHA `6c9dcc30c2edb7c96b43c287efce461b8586170b38116a3272ca4235484d5a05` is provisional due to an adopted-present installation ownership receipt. The final archive SHA is `aa296895caa0dd73bdaea881e64c7dbd6ab2ab52a3e676cfe0397014e2511707`. No reboot, continuous-availability, other layout/topology or signed-release acceptance is claimed; PowerDNS switch/reinstall inverses and overall item 1/P0.4 remain open.

### Standalone BIND to PowerDNS V4 recovery code scope (2026-09-27)

P0.4, constitutional invariants 1/2/3/6: source now contains a V4 frozen
candidate proof and a protected owner recovery command for one narrow cut:
standalone managed BIND was initially serving, and PowerDNS was inactive,
disabled, and had no prior database. The accepted operation is at a pre-activation `intent`,
`target-staged`, `source-stopped`, or `target-enable-intent` checkpoint, or an
already durable rollback decision. The owner command proves the exact native
state and excluded worker, writes a phase-only rollback decision, and rereads
it before any inverse effect. After `source-stopped`, an exact candidate renamed to the live database name
can be returned to the private path if PowerDNS is inactive, has no process
or port-53 listener, and the candidate's bytes and metadata remain exact.
This is a pre-start *journal* cut; current evidence cannot prove that the
daemon never ran historically. A stopped but enabled target is admitted only
from a durable V4 `target-enable-intent` checkpoint; the distinct
`rolling-back-target-enable` decision first returns its unit to the frozen
disabled state. A running or owner-modified target remains unknown and is not
forced through this path. The V4 candidate uses the root-private Agent state
directory with same-filesystem staging; the journal binds its
inode, metadata and bytes, the managed BIND generation/receipt, and the
owner-aware PowerDNS configuration after-image. The owner CLI replays the exact
configuration and unit restoration, candidate removal, and terminal journal /
ledger publication under the installed recovery runtime.

The Agent's V4 forward journal writer rejects a direct `source-stopped` to
`target-started` transition: `target-enable-intent` must be durably recorded
first. Generic rollback cannot consume either V4 enable-intent phase.
The quiesced owner status command identifies the same-request protected CLI for
admitted pre-start V4 phases. Live workers, a running or changed target, and
owner changes cause refusal; status polling makes no DNS mutation.

This V4 producer helper is not connected to the normal switch producer. No
native interruption/reboot trial, post-start or committed PowerDNS recovery, signed
release trial, or production acceptance has been performed for this path. V1/V2
and V3 contracts remain distinct. This is implemented source code only; P0.4
and its PowerDNS-target inverse acceptance remain open.

### Serving-BIND to PowerDNS switch refused for every topology (2026-09-29)

P0.4, constitutional invariants 2/4 and
[D-026](DECISIONS.md#d-026--dns-engine-recovery-refuse-the-unrecoverable-switch-accept-same-operation-recovery-for-first-installs):
a switch whose recorded source engine is BIND and whose target is PowerDNS is
now refused before any preview token, snapshot or Agent mutation, for
standalone hosts and paired secondaries as well as the paired primary that
`pdns_primary_switch_paused` already covered. The Panel emits the blocker
`bind_source_pdns_switch_unsupported` from `dnsEnginePreviewBlockers` (also
reached by server setup); the Agent rejects the canonical manifest in
`SwitchDNSEngineV1` before the mutation step claim and again in the host
backend before journal reconciliation. The screen states that BIND keeps
serving, that nothing changed, and that PowerDNS can be installed on a host
without a DNS engine.

Reason: the reachable producer for that path was the V1 switch journal, whose
only recovery is the Agent's own inverse; the V4 pre-start proof and its owner
CLI above have no producer wired to them; no native interruption trial exists.
Fresh PowerDNS installs (empty source), PowerDNS-to-BIND switches, PowerDNS
adoption and secondary reconfiguration, BIND reinstall and installed servers
are unchanged. Startup recovery of an already-persisted V1 journal is not
routed through this gate. Removing the check is not the way to reopen the
path; a wired producer, pre-start and post-start Agent-independent inverses and
native evidence are. The [acceptance register](DNS-RECOVERY-ACCEPTANCE.md)
tracks the remaining rows.

### First-install rollback accepts a target that never started (2026-09-29)

P0.4, constitutional invariants 2/4, D-026 decision 2. The native cell
`pdns-switch__target-staged__after-write__standalone__peer-reachable`
([evidence](../deploy/e2e/dns-kill-matrix/evidence/fresh-install-20260929/README.md))
exposed a verified defect: a fresh PowerDNS install installs its packages
before the `intent` write and leaves `pdns.service` under the package guard's
persistent mask until activation, so the journal's `target_units_before`
records a masked, never-started unit. The V1 rollback's stopped-target proof
(`VerifyStoppedUnit`) required `LoadState=loaded`, so Agent startup recovery
ended in `dns_native_recovery_unknown_after_restart` with the journal retained
at `rolling-back`; the same-request retry was refused and no DNS was served.
The prior no-DNS state was not damaged.

Fix (source only; native re-run pending): for a journal whose source is
empty (`SourceEngine == ""`, `SourceEpoch == 0`) the proof is
`VerifyStoppedFreshSourceTarget`, which accepts exactly the three states a
never-started target passes through — absent (`not-found`, empty unit-file
state), the guard's persistent mask (`masked`/`masked`), or `loaded` — each
only inactive/dead with zero main/control PIDs, twice-observed and identical,
and each observation additionally proving no public port-53 listener with the
same inventory the package guard uses. A runtime-only mask is refused. The
PowerDNS stop step is skipped only when a first-install target reads
`not-found`. Journals with a source engine keep the `loaded` requirement;
`VerifyStoppedPDNSPersistentMask` for V3 is unchanged. The identical rule now
applies to the first-install BIND rollback, whose preimage restore had masked
the defect. Paired-secondary reconfiguration journals also carry an empty
source and therefore now require the no-listener proof before restore; their
target is loaded after the stop, so their outcome is otherwise unchanged.
Known limit, shared with the V3 inverse: the listener inventory ignores
loopback and link-local sockets. Component tests cover the accepted and
refused states; the failed cell and the `intent` cell must be re-run on the
fixed source before row 4 of the acceptance register can pass its pre-start
cut.

### Owner inverse admits the Agent's deliberate release (2026-09-29)

P0.4; constitutional invariants 1, 2, 4 and 5; D-024. No journal or ledger
schema or version changes.

A restarted Agent that cannot complete switch recovery retains the journal and
releases its ledger lease with `dns_native_recovery_unknown_after_restart`.
For V2 journals this is the designed path: the Agent writes the rollback
decision and never executes the V2 inverse. The evidence reader reports that
ledger as `released-undecided`. `recover-dns-bind-switch`,
`recover-dns-bind-adoption` and `recover-dns-pdns-adoption` previously
admitted only active-lease statuses or a terminal rolled-back job, so a
journal left at `rolling-back` beside a running Agent had no admitted owner
command.

Rule: those three commands additionally admit `released-undecided` only when
the release reason is exactly that code, the journal is the exact request's
journal at `rolling-back` or `rolled-back` and passes the command's unchanged
journal-shape predicate, and the released job passes the Agent's own
released-job predicate (terminal failed/interrupted, no worker, no lease,
exact owner, target and qualifier). Every existing lock, worker-exclusion,
native-evidence and owner-change check is unchanged; a released status
requires an empty active request, so any other active mutation refuses the
command. `recover-dns-pdns-fresh-prestart` and `recover-dns-pdns-target-staged`
keep refusing a released job.

Durable path: inverse effects, the `rolled-back` checkpoint, journal
retirement. The command does not publish a ledger verdict for a released job:
the verdict publisher requires an active job, a released job already
satisfies the terminal rolled-back job predicate beside a `rolled-back`
journal, and the Agent's own later-boot reconciliation likewise refuses to
rewrite a finished job. A command interrupted after the checkpoint leaves a
`rolled-back` journal that the same command re-admits as terminal rolled-back.

Truthful state is computed at read time, not stored: when the job is the
deliberate release and no journal of that request is retained,
`recovery dns-switch-status`, the owner command's re-run and the Agent's
`ServiceMutationStatus` reply report the switch as reconciled and no longer
blocking, and state that the evidence cannot tell whether the owner command or
a later Agent start retired the journal. A retained or unreadable journal
keeps the stored blocking text. The stored job is never modified.

Evidence at this commit is component tests only; the native
`--owner-inverse-after-restart` cells have not run.

### V2 BIND switch inverse accepts a never-started, guard-sealed target (2026-09-29)

P0.4, D-026; constitutional invariants 1, 2, 4, 5, 6; D-024. No schema or
version change.

The first native run of the owner-inverse-after-restart flow on `7ad24282`
([evidence](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-20260929/README.md))
failed in both cells with safety passed: the owner command was admitted and
then refused at native verification with `DNS target is not a loaded unit`.
At `intent` and `target-staged` the product has already installed `bind9` and
the package guard holds `named.service` and `bind9.service` under its
persistent mask; BIND has never started. The owner-side stopped-target proof
required `LoadState=loaded` and the unit-identity reader could not represent
a masked unit. Until then `recover-dns-bind-switch` had only ever completed
for a target that had started.

The V2 journal does not record the phase that preceded the rollback decision
(`phase` is overwritten with `rolling-back`), and a recorded `source-stopped`
would not prove the target never started, because activation runs between
that write and `target-started`. The never-started class is therefore proven
by journal plus native state, not phase history: the journal passes the
unchanged inactive-BIND switch predicate and froze both target units as
`not-found` (this operation created BIND), and both units are either absent or
under the guard's persistent mask with each link proven root-owned and
pointing to `/dev/null`. Activation lifts that mask before enable/start and
nothing in the operation re-creates it, so a present guard mask is the
evidence. Known limit: a target an owner started and masked again would be
admitted; every stopped and source-only proof still applies and the mask is
kept.

For that class the stopped proof accepts absent or persistently masked,
inactive/dead, zero main/control PIDs, empty cgroup, two identical reads; a
runtime mask is refused. Each read also proves the source alone serves: no
process named `named`, and either PowerDNS is active and every public port-53
listener belongs to its verified main PID, or PowerDNS is inactive and no
public listener exists. A loaded target keeps exactly the previous proof.
`dnsunitidentity.ParseTargetObservation` adds typed loaded / persistent-mask /
absent observations; the strict `Parse` and its callers are unchanged.

Inverse effects at pre-start cuts: the guard mask and the installed package
stay as rollback standby (the target unit restore is skipped only when both
units are guard-sealed); configuration this operation wrote is restored
owner-aware; the pointer is restored; a running PowerDNS whose unit reads its
exact frozen preimage receives no `systemctl` call; a stopped source is
restored and started; then the `rolled-back` checkpoint and journal
retirement. The PowerDNS state and ownership receipts are not replaced.

Not changed and recorded as open: the staged immutable BIND generation tree
remains on disk after rollback; the `bind9-host`/`bind9-libs` upgrade is not
reversed; status for a V2 journal that has not yet reached `rolling-back`
still reports a masked target as unknown; listener proofs ignore loopback and
link-local sockets; the `named` scan matches the kernel command name only;
Arch is covered by code reading only. Evidence at this commit is component
tests; the two failed cells must be re-run on the fixed source.

Native result (2026-09-29, source `411398d9`,
[evidence](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-after-restart-rerun-20260929/README.md)):
the `target-staged` and `intent` cells passed on the
owner-inverse-after-restart flow with the Agent restarted and running. The
owner command exited 0, the journal reached `rolled-back` and was retired, the
ledger stayed byte-identical to the Agent's release, PowerDNS served on the
same process throughout, both BIND units remained under the guard's
persistent mask and never started, and the read-time texts reported the
switch as reconciled. One path on Debian 13 with an unsigned local recovery
kit; no reboot, owner-edit race, source-stopped or target-started cut.

### PowerDNS-to-BIND rollback after the source was stopped, Agent running (2026-09-29)

P0.4; invariants 2, 4, 5. No source, schema or version change: product
binaries are byte-identical to the `411398d9` build; only the controller
gained the critical variant (`7c5dfe17`).

[Evidence](../deploy/e2e/dns-kill-matrix/evidence/owner-inverse-critical-20260929/README.md):
`bind__target-started__after-write` and `bind__source-stopped__after-write`,
standalone Debian 13, managed PowerDNS source, V2 journal. After the kill the
restarted Agent wrote `rolling-back`, released its lease and kept running;
PowerDNS was inactive in both cells; BIND was active and answering in the
first and guard-masked and never started in the second, where no DNS daemon
answered. The read-only status named the command and changed nothing. The
owner command exited 0 in 15.7 s and 8.4 s: BIND stopped, PowerDNS started
with a new main PID and became the only port-53 authority, the journal was
retired, the ledger and the state receipt stayed byte-identical, owner files
were unchanged, and the zone's SOA serial was the same before the cut and at
the end. Re-run changed nothing (exit 3). 31/31 health samples.

Measured PowerDNS outage, an upper bound taken from the unit journal's
stopping entry to the first answer the controller saw after the command:
21.6 s and 9.4 s. In the first cell BIND answered for about ten seconds of
that window. These cells interrupt DNS by construction; nothing here claims
continuity or bounds the outage.

Final BIND unit state differs by history and is recorded, not judged:
unmasked and disabled where BIND had started (the absent preimage is restored
by unmask/disable and the `bind9.service` alias is removed), still under the
guard mask where it had not. In both cells the `bind9` packages, the
install-ownership receipt, the staged generation tree and the non-package
`rndc.key` remain; `/etc/bind` configuration equals the journal's preimage.
While running, `named` also listened on loopback and link-local addresses,
which the listener proofs do not inspect.

### Rollback standby, local listeners, completed re-runs, pre-decision status (2026-09-29)

P0.4; constitutional invariants 1, 2, 4, 5, 6; D-024. No journal or ledger
schema or version change. Component tests only at this commit; the native
cells must be re-run on this source. This section supersedes two statements
above: a present guard mask no longer proves by itself that BIND never
started once a rollback decision exists, and the listener proofs no longer
ignore loopback and link-local sockets.

**Rollback standby.** After a PowerDNS-to-BIND rollback whose journal froze
both BIND units as absent, or both under the guard's persistent mask, the
target always ends under the package guard's persistent mask on both names,
inactive and never enabled, whether or not BIND had started. A target that
had started is first compensated from the absent preimage (stop, unmask,
disable, which removes the enabled alias) and then sealed with the guard's own
mechanism; a sealed or absent target receives no `systemctl` call. A journal
that froze another BIND preimage keeps restoring that exact preimage. The
forward path accepts the sealed state on a retry. Before this change a retry
from the guard-masked state froze a masked preimage and, if it failed after
its intent was written, neither the Agent nor the owner command could finish
its rollback. The Agent's in-process V2 rollback ends in the same unit state.

**Staged generation.** The inverse removes the staged BIND generation tree
only when the journal and receipt prove the exact generation, the tree
verifies as what the operation staged, and the pointer does not select it;
removal is a rename followed by deletion and resumes after an interruption.
An owner-changed or owner-added tree is left and recorded without failing the
rollback. In the owner command a removal I/O error retains the journal at
`rolling-back` after the source has been restored; in the Agent it is logged.
Files BIND itself writes in its working directory are outside the managed
root and are listed, never removed. Packages, the rndc key and the
install-ownership receipt stay and are named in the command's output, which
now states what was restored, removed, not removed and intentionally kept.

**Local listeners.** The never-started target proof, the fresh-source stopped
proof and the source-only proof also classify loopback and link-local port-53
sockets from the same inventory, read twice. Accepted: sockets of the
verified source process, and `systemd-resolve` on 127.0.0.53 or 127.0.0.54
whose cgroup is exactly the systemd-resolved unit; accepted stub sockets are
recorded. Refused: `named` or `pdns_server` outside the verified source, any
other process, the stub name on another address or cgroup, malformed rows.

**Completed re-runs.** Re-running `recover-dns-bind-switch`,
`recover-dns-bind-adoption` or `recover-dns-pdns-adoption` after the request
is already reconciled, or after an earlier owner run recorded the terminal
verdict, exits 0 with the text on stdout and states that current DNS health
is not checked. Refusals and unknown results keep exit 3.
`recover-dns-pdns-fresh-prestart` and `recover-dns-pdns-target-staged` still
exit 3 on a terminal re-run.

**Status before the rollback decision.** For a V2 PowerDNS-to-BIND journal
that has not reached `rolling-back`, `recovery dns-switch-status` reads a
never-started target with the typed observation and names the next step
truthfully: restart the Agent, which records the decision for the same
request, then re-run the status check. It names no owner inverse command at
that phase.

### Peer catalog producer, adoption at rolled-back, takeover retry (2026-09-29)

P0.4; constitutional invariants 1, 2, 3, 4, 6; D-022, D-024. No schema or
version change: the accepted catalog producer lives in memory only. Component
tests only at this commit.

**Peer catalog producer.** A CelikPanel secondary validated the paired
primary's catalog in the BIND format only, while a CelikPanel PowerDNS primary
publishes PowerDNS's native catalog, so a pair with a PowerDNS primary and a
CelikPanel secondary could not be installed; earlier trials used panel-free
secondaries and did not reach this check. Every read of the PEER's catalog
(install preflight, readiness, zone verification, legacy secondary,
deletion-proof inspection on a PowerDNS peer) now tries the BIND format and,
only when the refusal is one the two formats encode differently (version or
member TTL, a member label of the other producer's shape), the PowerDNS format
on a fresh transfer. Both keep their strict bounds. Network errors, refused
transfers and refusals common to both formats are not retried. If both refuse,
the error names both reasons. The accepted producer is pinned per operation
and logged once; a later read in the same operation that sees the other
format fails. Reads of the LOCAL engine's own catalog keep their explicit
producer. Open native questions: whether PowerDNS writes a non-empty
`options` value on consumed member rows, whether a PowerDNS consumer
re-serves a consumed catalog with the original TTLs and owner names. Open
code question, not changed: three call sites check a legacy PowerDNS
primary's own catalog, as the peer re-serves it, with the BIND policy.

**PowerDNS adoption at `rolled-back`.** The restarted Agent's binding check
accepted only `rolling-back`, so a V1 adoption journal already at
`rolled-back` failed on every boot. For V1 journals the Agent now re-runs the
same proofs it used to write that checkpoint (config, unit snapshots,
database bytes and content, sole PowerDNS process and listeners, SOA answers),
restores nothing, keeps or publishes the same terminal verdict and retires the
journal. V2 stays owner-only. The V1 paired-secondary PowerDNS reconfiguration
has the same `rolling-back`-only proof and is not changed.

**Status text at `rolled-back`.** `dns-switch-status` no longer promises that
an Agent restart will resolve a journal the Agent cannot finish; it words the
next step per journal class and names an owner command only when that
command's own admission accepts the journal.

**Stopped-BIND takeover retry.** A retry of the same request treated the
adopted-present install receipt written by its own first attempt as
CelikPanel authority and selected the exclusive options mode, refusing
operator directives the first attempt had adopted. An adopted-present receipt
bound to the exact request no longer counts as authority, so the retry makes
the same adoption decision. A new request after a rolled-back takeover still
sees the surviving receipt as managed standby.

**Completed re-runs** of `recover-dns-pdns-fresh-prestart` and
`recover-dns-pdns-target-staged` exit 0 only when the ledger holds this
request's own owner-recovery verdict and the journal is retired; both paths
remain behind product gates.

### Boundary stop, missing-pointer repair, pointer ordering (2026-09-29)

P0.4; constitutional invariants 1, 2, 4; D-024, D-026. No schema or version
change. Component tests only at this commit.

Native cell `c6-fresh-bind-reboot`
([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch4-adoption-reboot-20260929/README.md)):
a fresh BIND install cut at `target-verified` after-write lost its `current`
pointer between the kill marker and the SIGKILL. The restarted Agent could
neither verify the target nor roll back, released the job as unknown, `named`
served from memory, and after a reboot `named` could not start; DNS was
refused.

**Boundary stop (test build only, tag `dns_kill_matrix`).** The hook sent a
process-directed SIGSTOP and returned an error if execution continued, so the
calling goroutine could run into the product's error path before the process
stopped and a cut could land past its named boundary. The hook now stops its
own locked thread with a thread-directed signal and never returns into the
operation once the marker write has started; it parks. A switch, its
in-process rollback and its recovery run in one request goroutine, so nothing
else advances the operation. Retained cells whose hook is followed by a
mutating error path (most after-write and several before-write boundaries of
the bind, pdns-switch and adoption drivers) are to be re-run on this source.

**Missing-pointer repair.** During same-request recovery of a BIND-target
journal at `target-verified` or `committed`, if target verification fails,
the pointer is absent, the state receipt is exactly the journal's target, the
exact target generation loads and verifies with matching generation, epoch
and pairing, and the managed runtime configuration is exact, recovery
restores the pointer atomically under the same locks as a switch, logs one
sentence and re-runs the full target verification before continuing forward.
It refuses and keeps the evidence when the pointer selects another
generation or is unreadable, the tree is missing or changed, records or
configuration changed, or the restore fails; the message then states what
was found, whether BIND can start after a reboot, and the next step. The
ledger code stays `dns_native_recovery_unknown_after_restart`.

**Pointer ordering.** On a failed first switch the publisher removed the
pointer before the inverse stopped BIND, and the startup inverse restored the
pointer before it restored the unit; a crash or reboot in either window left
an enabled BIND with a missing include. For a first generation the inverse
now runs while the pointer still selects the target and the pointer is
removed only after it succeeds; if the inverse fails the pointer is kept. The
running-BIND adoption rollback restores owner configuration and reloads
before it changes the pointer, as its startup recovery already did.

Open, not changed: an in-process failure after a durable `target-verified`
write still runs the full inverse even when the `rolling-back` write fails,
which conflicts with the rule that a verified target does not enter automatic
rollback; `recovery dns-switch-status` does not yet name the missing-pointer
case; a journal before `target-verified` that holds the target state receipt
and no pointer can exist only on hosts that crashed with an older Agent and
is not repaired.

### Durable rollback decision, PowerDNS secondary consumer state, DNS-only hold (2026-09-29)

P0.4; constitutional invariants 1-6; D-024, D-025, D-026. No schema or
version change. Component tests only at this commit; native cells must be
re-run.

**No inverse without a durable decision.** For every switch and adoption
driver the in-process failure path re-reads the journal and decides from what
is on disk, never from the phase it last tried to write. At `target-verified`
or `committed` the operation goes forward through the same recovery path and
is finalized; at `rolling-back` or `rolled-back` the decision already exists;
at an earlier phase `rolling-back` is written and the inverse runs only if
that exact phase is then on disk; an absent, unreadable or foreign journal is
handed to recovery with the evidence kept. Before this change both rollbacks
ran the full inverse even when the `rolling-back` write failed, and a write
error at `target-verified` that was nevertheless durable led to a rolled-back
host under a verified journal.

**Consumed PowerDNS member options.** Native PowerDNS 4.9.17 writes
`{"consumer": {"unique": "<label>."}}` into `options` of a consumed catalog
member. The Agent required the field empty, so a fresh PowerDNS secondary
whose daemon had already transferred the member and was answering
authoritatively was refused
([evidence](../deploy/e2e/dns-kill-matrix/evidence/batch5-paired-first-20260929/README.md)).
The field must now be empty or exactly that object, one key per level, string
value, bounded, equal to the member's label in the peer catalog plus the
trailing dot. The label is kept in memory from the catalog transfer for
either producer.

**Rollback of a fresh PowerDNS secondary after the daemon wrote.** For an
empty-source paired-secondary journal, after the target is proven stopped,
the live database is removed only when it is the staged candidate plus
consumer transfers: schema equal to a freshly initialized candidate; this
operation's manifest receipt; exactly one consumer row as staged; every other
domain a secondary of the peer assigned to that consumer, named by a PTR in
the database's own catalog copy, with options as above; no orphan records;
comments, metadata, keys, TSIG keys, autoprimaries and the legacy receipts
table empty. Anything else keeps the database and names what was found. If
PowerDNS writes metadata for consumed members this rule refuses; that is an
open native question.

**DNS-only hold.** When a switch fails and its native result cannot be
verified while the exact journal is readable, the Agent now writes the same
terminal release a restarted Agent writes, keeps the journal so the DNS
preflight refuses further DNS changes, and releases the host lock so
unrelated mutations proceed. In the native run no ledger write had been
attempted or ambiguous; the whole mutation manager had gone fail-closed by
choice. A missing or unreadable journal, or a release write that may have
published, still fails closed.

**Status and older hosts.** `dns-switch-status` names a missing or foreign
BIND pointer in plain words, read-only, and says whether `named` can start
after a reboot. A V1 first-install journal before `target-verified` that holds
the target state receipt and no pointer, possible only after a crash with an
older Agent's ordering, is rolled back to no DNS engine under the existing
proofs; every shape with a source is refused with text that says so.

Open: the fail-closed sentinel text still reads "after an ambiguous ledger
write"; a failed finalize still fails closed; the in-process path with an
unreadable journal still fails closed until a restart.

### Fresh paired PowerDNS primary: recovery policy and single gate (2026-09-30)

P0.4; constitutional invariants 1, 2, 4, 5; D-024, D-026 decision 2. No
journal, ledger, state or enrollment schema or version change. Component
tests only; the gate stays CLOSED at this commit.

**Policy.** The prior state of a first install is "no DNS engine", so
same-operation recovery is the contract. Every V3 journal is routed by shape:
- cut before the target ever started (`intent`, `target-staged`, or
  `target-enable-intent` with PowerDNS stopped): the restarted Agent rolls the
  install back by itself under the first-install stopped proof (absent,
  guard-masked or loaded; inactive/dead; zero PIDs; empty cgroup; listener
  proofs including local sockets; read twice), with the candidate exactly as
  sealed, writes a durable rollback decision first, restores the unit to its
  frozen standby, restores configuration, removes the staged candidate and
  writes `rolled-back` only after the pre-install state is proved. A retry may
  run forward past the guard mask only when the install-ownership receipt
  shows CelikPanel installed exactly these packages;
- cut after the target started: forward only, by policy. CelikPanel never
  removes a PowerDNS that has started, because its database may already hold
  the daemon's own changes;
- owner change at any boundary (configuration, SQL, state or ownership
  receipt not as the install wrote it): refuse, keep journal and database,
  DNS-only hold. A capture that cannot complete stays unknown, not "changed".
`recover-dns-pdns-fresh-prestart` also admits the Agent's deliberate release,
keeps the released job and exits 0 on a completed re-run.

**Catalog the primary publishes.** Staged: one PRODUCER row and its SOA/NS
to `invalid.`; members MASTER assigned to it; no metadata. Admitted daemon
changes at first start: the producer SOA re-stamped with the epoch serial,
one `CATALOG-HASH` metadata row, `notified_serial`. `last_check` must not
change (the checker had left it unconstrained). After commit the producer's
master, last_check, options and catalog stay NULL and its metadata is none
or that single row. Zone publication follows a daemon re-stamp during a
propagation wave only when the serial is strictly higher and identity,
members and member serials are identical. *(Changed 2026-10-01: the former
bound "at most twice per wave and only before any native peer inspection" is
replaced by the admission rule of the section "PowerDNS daemon catalog
re-stamp admitted at any point of a V3 operation", which also requires a
changed `CATALOG-HASH` row.)*

**Producer by local engine.** Three call sites read a legacy primary's own
catalog, as the peer re-serves it, with the BIND policy regardless of engine;
they now select the parser by the local engine that produced the catalog.

**Gate.** One policy constant per side decides whether the fresh paired
PowerDNS primary is offered (Panel) and admitted (Agent); both are closed and
behaviour is unchanged. With the constant open only an empty source is
admitted, Debian 13 amd64 only; a BIND source still gets
`bind_source_pdns_switch_unsupported`. The setup wizard's refusal is now
server-side at plan time and before any DNS identity is staged; the web
renders the server's blocker.

**Before the gate may be opened:** native evidence through the public RPC for
pre-start cuts, post-start cuts including a daemon re-stamp during zone
publication, an owner-edit cut, an Agent-released job finished by the owner
command and a reboot during recovery; a pre-install check of the candidate
package version; a recovery path for an unsealed partial candidate at
`intent`; owner enrollment for a PowerDNS secondary and truthful Panel
guidance for parentless deletion.

### Owner enrollment for a PowerDNS secondary (2026-09-30)

P0.4, P0.5; constitutional invariants 1, 2, 4; D-022, D-024. Component tests
only; no native enrollment has run.

The Agent could report `dns_peer_enrollment_required` for a PowerDNS peer,
but only a BIND enrollment writer existed, so on a BIND primary with a
PowerDNS secondary a parentless zone deletion could stay pending with
guidance the owner could not follow. `dns-peer-enroll` now takes
`--engine bind|pdns` on every subcommand (default `bind`), and the owner
tools package builds `pdns-peer-inspect` next to `bind-peer-inspect`.

The PowerDNS path keeps the BIND contract: locked system account, forced
command, pinned host key digest handed over a trusted channel, explicit
revoke, idempotent resume from every checkpoint, refusal of owner edits to
files it created. The primary record is written in the existing
`celikpanel-pdns-peer-inspection/v1` reader format and read back before it
counts. One new persisted record exists on the secondary:
`celikpanel-pdns-peer-sshd-include/v1`, the receipt of the published
`sshd_config`, with the same structure as the BIND receipt; revoke restores
the original only when the live file is exactly the published one. One
engine per host: the primary refuses to prepare or activate while the other
engine's record exists, and the secondary refuses to install while any of
the other engine's channel files exist. BIND output stays byte-identical
except that no text promises a live exchange that no command performs.

Open: the secondary install, resume and revoke paths depend on real sshd,
systemctl, useradd and visudo and have component coverage only; the Panel's
parentless-deletion text still does not name the tool, the steps, which
owner acts or how the operation resumes.

### Package manager safety, reinstall, wizard retry, typed host blockers (2026-09-30)

P0.4, P0.5; constitutional invariants 1-6; D-022, D-024, D-026. No journal,
ledger or state schema or version change. Two optional wire fields were
added, both omitted by older Agents: `rollback_standby` on the DNS backend
runtime state and `error_code` on the firewall status response. Component
tests only at this commit.

**dpkg statoverride (D-022).** Native cell c6 of batch 6b: the managed BIND
setup had registered `dpkg-statoverride root bind 1775 /var/cache/bind`; after
the owner purged `bind9` the group was gone and the override remained, so
dpkg refused every package operation on the host. dpkg stores an override by
name even when it is registered by numeric id, and a package upgrade keeps an
existing directory's owner and mode without an override, so the product no
longer adds one; it re-asserts ownership and mode itself. On hosts that
already carry the exact legacy line it is accepted on every path and LEFT IN
PLACE, because older releases' read-only proofs require it and the update
contract treats it as hardening that rollback retains. Before any package
installation the product lists the overrides: its own legacy line is removed
only when the group it names no longer exists; any other entry naming a
missing user or group refuses the operation before any mutation and names the
command the owner can run. Named limits: an owner who removes `bind9` by hand
on a host set up by an earlier release must also run
`dpkg-statoverride --remove /var/cache/bind`; a host that gets its first BIND
under this release carries no override, so after an owner rollback to an
older release that release's root proof refuses zone publication until one of
its own BIND operations re-adds it.

**Package failures carry their reason.** When the package manager fails, the
ledger job and the Panel response carry the package manager's own first
relevant line, bounded and sanitized, instead of only an exit status.

**Reinstall.** A reinstall exists because the recorded engine is not running,
so its rollback can never prove "only BIND active". The restored state is the
pre-operation state: the state receipt equals the frozen one and no managed
DNS authority is active or listening. The stopped-target proof uses the
never-served class. An install-ownership receipt left by an earlier attempt of
the same reinstall manifest is residue, not ambiguity.

**Retry from the setup wizard.** After a rolled-back first install the
packages stay installed as standby. The Agent reports `rollback_standby` only
when no engine state or ownership receipt exists, the install-ownership
receipt shows CelikPanel installed exactly these packages, and the unit is
inactive under the guard mask or loaded and disabled; an owner-installed
engine never qualifies and stays a takeover or adoption decision. With the
signal the Panel treats the target as a new installation for both engines and
the wizard accepts a new plan; the old request keeps its terminal answer with
code `server_setup_dns_rolled_back`. The reconcile scope and rollback
evidence, previously initial BIND standalone or paired primary only, now
cover BIND as paired secondary and PowerDNS standalone or paired secondary;
the fresh paired PowerDNS primary stays behind its gate.

**Typed host blockers in setup.** The setup plan answered a generic 500 when
the Agent could not read the firewall status, for example right after the
installer upgraded the kernel. The Agent now classifies what it can prove
(`host_restart_required` from a missing running-kernel module tree with
another installed, or the OS's reboot-required marker;
`firewall_kernel_unavailable`; `firewall_engine_unavailable`;
`firewall_busy`; `firewall_status_unknown`) and the plan returns a blocker
with the reason and the next action, leaving the draft and DNS identity
unchanged.

**Fresh paired PowerDNS primary, gate still closed.** The measured package
version is checked against the package manager's candidate before any
mutation and the install pins it; the candidate database is built under a
temporary name and published by an atomic no-replace rename, and recovery
removes only the operation's own interrupted build. A crash between that
rename and the `target-staged` checkpoint still leaves a complete but
unsealed file that is refused as unknown. The tagged kill hook accepts V3
journals for this shape only.

**Status and hold texts.** Status describes an owner-installed or
guard-masked BIND before a takeover instead of reporting an identity parse
failure; names the Agent's own finish first for a V1 PowerDNS adoption;
names `recover-dns-pdns-fresh-prestart` only through its admission.
Fail-closed texts follow their cause instead of always saying "after an
ambiguous ledger write".

### Zero-zone primary, truthful setup states, license refresh, release contents (2026-10-01)

P0.2, P0.4; constitutional invariants 2, 3, 6; D-024, D-027. No journal,
ledger or state schema or version change. License policy unchanged.
Component and script tests only at this commit.

Found by the second native run of the pair acceptance driver
([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair2-20260930/README.md)),
on the acceptance branch with the gate open.

**Zero zones.** A fresh paired PowerDNS primary installed through the wizard
has no zones. Its producer membership check compared the observed member
list, an empty slice, with the manifest's, a nil slice, using a deep
comparison that treats them as different, so the install failed its own
verification. The comparison is now element-wise; order, duplicates and exact
names stay strict. About 290 list comparisons across the DNS code were
audited; this was the only affected site. The failed install had in fact
been rolled back by the same request's pre-start inverse; the host was left
at rollback standby. Not measured: whether PowerDNS writes its catalog hash
and re-stamps the producer serial at first start with zero members; the
forward (post-start) recovery of a zero-zone install depends on it and needs
a native run before the gate opens on the main line.

**Setup states.** The wizard showed "result not confirmed yet" without end
while the Agent held a verified failure, because the Panel's reconcile scope
excluded the operation and every poll mapped the error to reconciling. After
its own reconcile cannot finish, the Panel now reads the Agent's ledger for
the exact request, read-only: an active job is progress; a failed job is
`server_setup_dns_agent_failed`; a job released with a recovery code is
`server_setup_dns_recovery_held`; anything else is unknown, and after five
minutes becomes `server_setup_dns_result_unknown`, which states since when,
that it is not a verified failure and that nothing is started twice. Each
names who acts and the status command. Polling never mutates. The rollback
evidence scope includes the fresh paired PowerDNS primary only while its gate
is open. The primary's role text tells the owner to start the secondary only
after this server's DNS step shows as finished.

**License refresh in the background.** The setup runner read a license check
that only HTTP handlers refreshed, so with no browser request for a minute
it reported `license_required` for an active license. It now performs the
same synchronous refresh, one-minute rule and failure semantics as the HTTP
gate. A status that cannot be verified is shown as unverified, never as
license required.

**Release contents.** The dist recipe copied `deploy/` whole, so the customer
archive carried the e2e evidence directories. The recipe now prunes every
`evidence` directory and every `deploy/e2e/**/test_*.py`, and the manifest
writers refuse an archive or tree that contains such paths. Archive ordering,
timestamps, ownership and modes are unchanged; the recipe produced
byte-identical archives under two umasks with a make stand-in. Not executed
here: a real `make dist`, the release sequence policy test, and the bootstrap
update contract test, which stops at an earlier Makefile expectation.

### PowerDNS operator notify carries an explicit port (2026-10-01)

No journal, ledger, state schema or version change. Component tests only;
native re-run pending.

Found in the third native run of the pair acceptance driver
([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair3-20261001/README.md),
topology t3: PowerDNS 4.9.17 primary, BIND 9.20.29 secondary). After a zone
add the primary logged, for the catalog and the member zone, four "Received
spurious notify answer … from 192.0.2.11:53" lines and then "Notification
for … to 192.0.2.11:0 failed after retries". The BIND secondary had received
the first NOTIFY and transferred both zones within half a second, so
replication was not affected; the batch 8r kill-matrix journals show the
same pattern, one ":0 failed after retries" line per operator notify.

**Cause.** After a V3 zone mutation the Agent runs `pdns_control notify-host
<zone> <peer>` with the bare peer address. In PowerDNS 4.9.17 the notify queue
stores that address with port 0, sends to port 53, and then matches the
answer on address and port, so no answer matches: each NOTIFY is re-sent
until its retries run out and the queue reports a failure that did not
happen (PowerDNS issue 13576, present since 4.8.0; the upstream fix, commit
1ec11cb0, is not in 4.9.17). The configured `also-notify` path is not
affected: PowerDNS applies port 53 there itself, and its answers were
matched in the same run.

**Change.** The notify-host destination is now `<peer>:53`
(`pdnsNotifyHostTarget`, `cmd/agent/dns_engine_pdns_propagation.go`), a form
the 4.9 parser accepts and that a fixed PowerDNS treats identically. Unit
tests pin the exact argument. Unchanged, with reasons: `also-notify` in the
directional pair configuration (port 53 is the documented default, and
changing the rendered line would make existing owner-visible configuration
differ from what the Agent expects); the consumer catalog's `domains.master`
(PowerDNS applies port 53); BIND `also-notify` (BIND sends to `#53`, as its
journals show).

**Native re-run must show:** "Notification request to host 192.0.2.11:53"
for each zone mutation, no "spurious notify answer" and no ":0 failed after
retries" on the primary, one NOTIFY per zone received by the secondary, and
the member zone served by the secondary at the same delay as before.

### BIND rndc key at fresh install, rollback and typed rndc reason (2026-10-01)

P0.4; constitutional invariants 1, 2, 4, 6; D-022, D-024, D-025. Install-
ownership receipt, switch journal, ledger and state receipt: no schema or
version change. One new companion record, one additive wire field.

Found by the third native run of the pair acceptance driver
([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair3-20261001/README.md),
stop S-1): Arch `bind 9.20.29-1` creates no rndc key, so on an Arch BIND the
product installed `rndc zonestatus` answers "neither /etc/rndc.conf nor
/etc/rndc.key was found". Debian 13's `bind9` creates `/etc/bind/rndc.key`
(`bind:bind 0640`) at package configuration. The product's own deletion proof
(row 17 of the [acceptance register](DNS-RECOVERY-ACCEPTANCE.md)) and the
owner's `bind-peer-inspect` both ask named through rndc with its defaults.

**When the key is created.** In the BIND switch transaction (fresh install,
PowerDNS-to-BIND switch, `reinstall_active`, stopped-BIND takeover; every host
layout alike), after the package step and before named first starts (the
package guard still masks it), and before the intent journal. The default key
path comes from the certified layout, not from probing: pacman layout
(`/etc/named.conf`) uses `/etc/rndc.key`, APT layout (`/etc/bind/named.conf`)
uses `/etc/bind/rndc.key`; these are each build's compiled-in sysconfdir. The
product runs the native `rndc-confgen -a` only when the key file is absent,
no `rndc.conf` exists beside it and `named-checkconf -p <main config>` shows no
`controls` statement; it then sets `root:named 0640` (Arch) or `bind:bind 0640`
(Debian, matching the package) and fails the step if `rndc-confgen -a` wrote
anywhere else. A present key, `rndc.conf` or `controls` statement is never
touched.

**Record.** `dns-engine-bind-rndc-key.json` (schema
`celikpanel-dns-engine-bind-rndc-key/v1`) next to
`dns-engine-install-ownership-bind.json`, bound to the same manifest qualifier,
request and owner id: `path`, `provenance` (`product_created` with the content
`sha256`, or `owner_or_package_provided` with `basis` `key_present`,
`rndc_conf_present` or `controls_statement`). For a created key it is written
before ownership is changed. It is a separate file rather than a new receipt
field because the receipt is read byte-exactly by older Agents (unknown fields
refused), by the owner recovery kit and by the native kill-matrix probe; old
receipts are unchanged and remain the only install-ownership format. A retry
recognises a present key as its own only when the record names the install
receipt that existed before the retry (a never-committed earlier attempt) and
the content hash still matches; commit retires that receipt, so a committed
install's key is afterwards always owner or package provided.

**Rollback rule.** After a completed rollback of that transaction (Agent
in-process, Agent startup recovery of a V1 journal, and `recover-dns-bind-switch`
for the V2 PowerDNS-to-BIND journal), with no named process, the key is removed
only when the record names this exact transaction as `product_created` and the
file is still a regular file with the recorded hash. A changed key is the
owner's and is kept; a package- or owner-provided key is kept; the outcome is in
the Agent log line and in the owner command's summary. It never fails the
rollback. Packages and the install receipt stay as rollback standby as before.

**Typed reason.** Every product rndc call classifies a missing or unreadable
key, an authentication refusal and a refused control connection as
`bind_rndc_unavailable` (`internal/bindrndckey`), keeping rndc's first output
line as log detail. The deletion proof returns it at once instead of waiting
for the proof limit; `SyncDNSZoneV3Response.failure_reason` (additive, omitted
by older Agents, ignored by older Panels) carries it; the Panel answers the
pending domain deletion (202, `stage: dns_cleanup`) and the 409 publication
failure with `reason: bind_rndc_unavailable` and the D-024 text
(`err.DNS_PUBLICATION_FAILED.bind_rndc_unavailable`): the server owner runs
`sudo rndc-confgen -a` and `sudo systemctl restart named` on that server, then
"Retry this deletion". A lost RPC response loses the reason (generic text), and
the saved deletion-status read does not show it. The paired primary's optional
`rndc notify` keeps the typed reason only in its joined log error.

**Hosts affected.** Arch (pacman) BIND: key created on fresh install. Debian:
normally package-provided, recorded, untouched.

**Evidence.** Component tests only; native re-run pending (Arch BIND secondary
deletion proof, Arch BIND standalone/primary deletion, rollback with a
product-created key).

### Managed BIND secondary allows the owner inspector's loopback catalog transfer (2026-10-01)

P0.4; constitutional invariants 1, 2, 4, 6; D-022, D-024, D-025. BIND
generation receipt: `secondary_config_version` 1 → 2 (a value the field
already carries, no new field). Ledger v1, switch journal, state receipt and
peer request/response v1: no schema change. One additive API field
(`detail`), one new reviewed pending code, one versioned inspector stderr line.

Found by the fourth native run of the pair acceptance driver
([evidence](../deploy/e2e/dns-pair-acceptance/evidence/pair4-20261001/README.md),
finding P4-1, topologies t1 and t3): after the owner enrollment completed, the
retry of a parentless zone deletion stayed pending with
`dns_peer_inspection_unknown` for 300 s. The owner's `bind-peer-inspect` on the
CelikPanel-managed Arch BIND secondary asks `dig @127.0.0.1 <catalog> AXFR`;
the product rendered the catalog zone without its own transfer clause, so the
options-scope `allow-transfer { <primary>/32; }` applied and named logged
"zone transfer … denied" from 127.0.0.1. The zone itself was already gone on
both servers. Measured on Debian 13 `bind9 1:9.20.26`: a refused AXFR makes dig
print only `; Transfer failed.` and exit 0, so the inspector actually failed on
the catalog parser, not on the "AXFR unavailable" branch the README described.

**Rendered statement.** The secondary's immutable generation (`zones.conf`)
now renders the catalog zone as

```
zone "catalog-<primary>.celikpanel.invalid" {
	type secondary;
	primaries { <primary>; };
	allow-transfer { <primary>/32; 127.0.0.1; ::1; };
};
```

(before: the same stanza without the `allow-transfer` line). Only the catalog
zone: `bind-peer-inspect` transfers nothing else (member zones are read with
`rndc zonestatus`), so member zones keep the options-scope
`allow-transfer { <primary>/32; }`. The primary's rendering is unchanged.
Loopback AXFR needs no key. `named-checkconf -p` prints the clause after
`primaries`, so the inspector's catalog-binding check is unaffected.

**Rendering transition.** The generation receipt's
`secondary_config_version` is 2 for the new rendering and 1 for the previous
one; the version is part of the content-addressed generation ID, so the two
renderings never share an ID (an existing generation directory is never asked
to hold different bytes). Version 1 is recognised as this product's own earlier
output, not as an owner change: `VerifyCurrentConfig` and the publisher
reconstruct a tree under its own recorded version (1 or 2), and the Agent's
switch-journal, completed-switch and recovery checks
(`bindExpectedGenerationForTarget`, `bindSecondaryOptionsFromJournal`,
`bindSecondaryOptionsFromReceipt`) accept a version-1 target that an earlier
release recorded. Every newly derived plan (`NewTreePlan`,
`ReconfigurePairing`) renders version 2, so the upgrade happens at the next
generation this product writes on that secondary. Any other statement,
including a self-consistent receipt over an edited `zones.conf`, stays an owner
change and is refused as before; version 0 stays historical only; version 3+ is
refused. The options file (`named.conf.options`) is unchanged.

**Reason propagation.** The inspector now classifies an incomplete observation
with one reviewed token (`inspector_policy`, `named_unavailable`,
`listeners_unverified`, `catalog_unverified`, `catalog_transfer_refused`,
`catalog_transfer_failed`, `catalog_malformed`, `observation_expired`); `; Transfer
failed.` after a verified loaded catalog secondary is `catalog_transfer_refused`.
The CLI's single stderr line is either the fixed generic sentence or
`celikpanel-peer-inspect-reason/v1 <request-sha256> <token>`; raw error text is
never printed. The primary's SSH transport keeps stderr only when the pinned
host's forced command exited non-zero, and accepts exactly that one line with
this request's digest and a reviewed token. The Agent records
`dns_peer_catalog_transfer_refused` (new reviewed pending code) for the refusal
and `dns_peer_inspection_unknown:<token>` for any other reviewed token in the
ledger v1 `error_code`; an older reader treats the composite as unreviewed and
shows generic text. The Panel splits it: `reason` stays a reviewed code and the
new additive `detail` field carries the token on the 202 deletion-pending
response and the saved deletion status; the English `message` appends the
detail sentence. The web shows the reason's text followed by the translated
detail sentence (`domains.peerInspectorDetail.<token>`). The typed refusal text
names the secondary's owner, the statement, which case applies (a secondary
CelikPanel set up with this release already allows it; one set up by an earlier
release gets it when CelikPanel next writes its DNS configuration; a panel-free
secondary's owner adds 127.0.0.1 and ::1 by hand and reloads named) and
“Retry this deletion”. The PowerDNS inspector path is unchanged.

**PowerDNS secondary.** `pdns-peer-inspect` reads the catalog through a
read-only SQLite connection bound to the daemon's database inode plus the fixed
`LIST-ZONES` control command; it performs no AXFR, so the loopback gap does not
exist there. Its reviewed-configuration check, however, accepts only the exact
panel-free fixture `pdns.conf` (12 keys, `primary=no`, `allow-axfr-ips=<primary>/32,127.0.0.1/32`,
no `include-dir`), while a CelikPanel-managed PowerDNS secondary has
`gsqlite3-dnssec`, `include-dir` and a managed drop-in with `primary=yes`,
`secondary=yes` and `allow-axfr-ips=<primary>`; source reading predicts that
inspection fails there with "configuration is unreviewed" (not natively
observed; pair4 t2 stopped before enrollment). Not changed here.

**Remaining gap.** A BIND secondary whose current generation is version 1 has
no panel action today that writes a new generation (the DNS engine reinstall is
offered only for standalone engines), so until such a write it keeps the
peer-only catalog transfer and its owner cannot add the loopback allowance
without it being detected as an owner change.

**Evidence.** Component tests only; native re-run pending (pair 5).

### PowerDNS inspector recognises a CelikPanel-managed secondary (2026-10-01)

P0.4, P0.5; constitutional invariants 1, 2, 4; D-022, D-024, D-025. Closes the
gap recorded in the previous section's **PowerDNS secondary** paragraph.
Component tests only; native re-run pending (pair 5 t2).

`pdns-peer-inspect` reviewed the daemon configuration against one exact
panel-free fixture, so after owner enrollment (`dns-peer-enroll --engine pdns`)
a parentless deletion on a pair whose secondary is a CelikPanel-managed
PowerDNS would have stayed pending as "configuration is unreviewed", with no
reason reaching the primary. The inspector now accepts an explicit allow-list
of two shapes and refuses anything else:

- **Panel-free fixture** (unchanged): one `pdns.conf` of at most 4096 bytes,
  `root:root`, with exactly the 12 reviewed directives and no `include-dir`;
  the daemon must hold 127.0.0.1 and `<peer>` UDP/TCP listeners.
- **CelikPanel-managed secondary** (new): `pdns.conf` whose only active lines
  are `include-dir=/etc/powerdns/pdns.d` (required) and, optionally, the empty
  `launch=` and `security-poll-suffix=` — exactly the Debian/Ubuntu package
  file (20579 bytes, SHA-256 `8b46927e…f262a`, measured in batch 6b), or an
  owner file to which the Agent appended only the include-dir. It may be up to
  64 KiB and belong to root's group or the running daemon's group (the
  package ships it `root:pdns 0640`). The include directory must load exactly
  `celikpanel.conf` and `celikpanel-cluster.conf` (root-owned, not group/other
  writable; hidden and non-`.conf` names are ignored as PowerDNS ignores
  them; any other loaded `.conf` is refused). Each drop-in must equal,
  directive for directive, what the product renders: `celikpanel.conf` is
  re-rendered from its own `local-address` list (which must include the
  enrolled peer) with the fixed database `/var/lib/powerdns/pdns.sqlite3`
  (`launch=gsqlite3`, `gsqlite3-dnssec=yes`, `gsqlite3-database`,
  `local-address`, `zone-cache-refresh-interval=0`, `webserver=no`,
  `api=no`); `celikpanel-cluster.conf` must be the secondary-role directional
  rendering for this peer and the policy's primary (`primary=yes`,
  `secondary=yes`, `allow-axfr-ips=<primary>`), or the same rendering from
  v0.1.0-alpha.29 to alpha.38, which also carried `autosecondary=yes`. The
  undirected legacy pair rendering (`also-notify`) is not accepted: it never
  carries a catalog CONSUMER, so it cannot pass the catalog check anyway. No
  loopback listener is required, because the product renders only public
  addresses.

The expected drop-ins come from the product's renderer itself: the Agent's
`managedPowerDNSStandaloneConfigForAddresses` and
`dnsDirectionalClusterConfig` now delegate to the new shared package
`internal/pdnsmanagedconf`, which the inspector imports (bytes unchanged; a
test reproduces both drop-in SHA-256 digests measured on the batch 6b managed
secondary). The historical autosecondary rendering is pinned by a byte test.

What the inspector verifies is otherwise identical for both shapes and nothing
new is observed about the daemon: the same `pdns.service` PID, executable,
invocation and start time, the peer listener, the SQLite inode the daemon holds
open, `LIST-ZONES` over the `SO_PEERCRED`-verified control socket, the catalog
CONSUMER row and the deleted zone's absence or presence, twice. The managed
pair drop-in's `allow-axfr-ips` names only the primary; that it lacks loopback
does not matter here, because this inspector performs no AXFR (a test states
it). Files are read through no-follow descriptors under root-owned ancestors
and rechecked against their final path; the include directory's identity is
rechecked after reading. The only additional read is the daemon's
`/proc/<pid>/status` group, used solely to allow the package's group
ownership of `pdns.conf`. Any line ending in a backslash is now refused in
either shape, because PowerDNS joins it with the next line.

**Reason.** Any failure to read or recognise the configuration is classified
with the new reviewed token `config_unreviewed` and leaves `pdns-peer-inspect`
as the single `celikpanel-peer-inspect-reason/v1 <request-sha256>
config_unreviewed` stderr line (the BIND scheme had no equivalent token:
`named_unavailable` names named). All other PowerDNS inspector failures keep
the fixed generic sentence. The PowerDNS SSH transport now keeps the
authenticated forced command's bounded stderr exactly as the BIND transport
does, accepting only `config_unreviewed` on the PowerDNS channel (and the BIND
channel no longer accepts it), and the Agent records
`dns_peer_inspection_unknown:config_unreviewed` through the same
`inspectionPendingCode`. The Panel appends: "The secondary's inspector
reported that PowerDNS is not running with a configuration it recognises (the
panel's own or the documented panel-free one)." (`domains.peerInspectorDetail.config_unreviewed`,
EN/TR). No new pending reason code, so the pair acceptance driver is
unchanged.

**Evidence.** Component tests only; native re-run pending (pair 5 t2). Pair 5
t2 must show, on a pair with a CelikPanel-managed PowerDNS secondary after
`dns-peer-enroll --engine pdns`, that a parentless deletion's inspection
returns `catalog_state=transferred`, `member_state=absent`,
`native_state=unloaded` and the deletion completes; and that an owner edit to
a drop-in (for example an added `loglevel=`) leaves the deletion pending with
`detail=config_unreviewed` and the sentence above.

### BIND primary plan carries its source state; internal proof failure code (2026-10-01)

P0.4, P0.5; constitutional invariants 1, 4; D-024, D-025. Corrects pair 5
finding P5-1 (`deploy/e2e/dns-pair-acceptance/evidence/pair5-20261001/README.md`).
Component tests only; native re-run pending (pair 6).

**Defect.** On a managed BIND primary (pair 5 t1 BIND/BIND, t2 BIND/PowerDNS),
after owner enrollment the retry of a parentless deletion ran the inspector to
completion and about one second later the Agent returned
`dns_peer_owner_edit_unknown`, which it kept. `bindV3PrimaryPropagationPlan`
built the primary propagation plan without `SourceState`; the pre-inspection
recheck treated the empty engine as BIND, so the challenge was minted and the
SSH inspection ran, but after it `catalogAXFRProbesForSourceEngine("")` failed
and that failure was mapped to the owner-edit code. The PowerDNS plan set
`SourceState: plan.State` and completed (t3). Nothing had changed on either
server.

**Plan source state on BIND.** `bindV3PrimaryPropagationPlan(tree, domain,
source)` now takes the active engine state receipt and refuses anything that
is not a BIND receipt. The production callers pass the `state` they already
verified with `bindStateTreePairContract` and persisted with exact readback
(`completeManagedBINDV3PropagationForState` on the publication path and on
`RecoverZone`), which is the receipt `recheckBINDPeerLocalEvidence` rereads
with `readDNSEngineState`. That recheck now rebuilds the plan from the receipt
it read, so the `reflect.DeepEqual` comparison also covers the source state.
The stateless `completeManagedBINDV3Propagation` (no caller) was removed.

**Constructor rule.** `newDNSV3PrimaryPropagationPlan(source, evidence,
changed, legacy, operation)` is the only non-test way to build a non-empty
`dnsV3PrimaryPropagationPlan`: it refuses an empty or unknown source engine
(the same selection the proof makes) and then runs
`validateDNSV3PrimaryPropagationPlan`. Builders: the BIND plan above;
`completePDNSV3Propagation` (source `plan.State`); the PowerDNS prepare
validation `validatePDNSPrimaryPropagationPlan` (source `plan.State`, so a
PowerDNS plan without its receipt no longer validates); the test-only
`verifyPDNSV3PropagationAt`. A source test parses every non-test file in
`cmd/agent` and fails on any other non-empty composite literal of the type.

**Probe selection before the challenge.** Both native verifiers
(`verifyEnrolledBINDPeerDeletion`, `verifyEnrolledPDNSPeerDeletion`) select
the catalog AXFR probes by `plan.SourceState.Engine` right after the active
ledger attempt is confirmed and before enrollment is read or any challenge is
minted, and reuse them after the inspection, so the post-inspection
re-verification cannot fail on selection. `recheckNativePeerLocalEvidence` no
longer treats an empty engine as BIND.

**New reviewed code `dns_peer_proof_internal`.** An internal precondition
failure of the proof (probe selection impossible, an empty or unknown source
engine, a PowerDNS plan whose source receipt is not a managed primary, a
missing recheck boundary) is no longer reported as an owner change. It is
carried like the other codes: `transport.DNSPeerPendingProofInternal`, the
Agent's pending ledger `error_code`, the Panel's `reason` and English
fallback, the Domains screen (`err.DNS_PUBLICATION_FAILED.dns_peer_proof_internal`,
EN/TR, in the reviewed set), and the pair driver's `REVIEWED_DNS_PEER_REASONS`
with actor "this server's owner (read the Agent log and report it; retry after
a fixed Agent)". Text: the change is saved but the deletion is not verified;
this server could not run its own check of the secondary; no change by either
owner was found and nothing needs to be undone; the server owner reads
`sudo journalctl -u celikpanel-agent | grep peer` on this server and reports
it; retrying does not help until the Agent is updated with a fix, after which
"Retry this deletion" retries the same publication; until then the deletion
stays pending and DNS answers are unaffected. At that point the Agent now logs
the underlying error (control characters replaced, at most 512 bytes,
product-authored text only) as "DNS peer proof could not run its own check
(<stage>); no owner change was observed: <error>". Evidence that was observed
and differed after the inspection (pair readiness, peer no-transfer, the
deleted zone's REFUSED observation, a changed ledger attempt or local receipt)
keeps `dns_peer_owner_edit_unknown`; its text additionally names the concrete
action: compare the catalog zone and zone serials on both servers, then retry.

**Transition.** No schema or durable format change; the new code is additive.
A Panel or driver older than this release shows the generic pending text for
`dns_peer_proof_internal`. A deletion left pending as
`dns_peer_owner_edit_unknown` by the defect is retried unchanged after the
owner updates the panel; the plan then carries the BIND receipt.

**Evidence.** Component tests only; native re-run pending (pair 6). Pair 6
must show on a BIND primary (t1 BIND/BIND and t2 BIND/PowerDNS) that after
owner enrollment the retried parentless deletion completes (200 `deleted`,
ledger job `succeeded`, challenge journal consumed and retired), and on t2 the
`pdns-peer-inspect` outcome fields (`catalog_state=transferred`,
`member_state=absent`, `native_state=unloaded`); and that the Agent journal
has no "DNS peer proof could not run" line and no `dns_peer_proof_internal`
ledger code.

### PowerDNS daemon catalog re-stamp admitted at any point of a V3 operation; owner-edit check tokens (2026-10-01)

P0.4, P0.5; constitutional invariants 1, 2, 4, 5; D-022, D-024, D-025.
Replaces the "at most twice per wave and only before any native peer
inspection" contract of the 2026-09-30 section above. No journal, ledger,
state, enrollment or database schema change; the ledger `error_code` gains an
additive composite value. Component tests only; native re-run pending
(batch 12: `z04`/`z05`; pair 6/7 t3 with a delayed retry).

**Finding** (batch 11,
`deploy/e2e/dns-kill-matrix/evidence/batch11-zero-zone-complete-20261001/README.md`).
On a PowerDNS 4.9.17 primary, a parentless deletion pending on
`dns_peer_enrollment_required` and resumed after the owner's enrollment came
back pending as `dns_peer_owner_edit_unknown`, although nothing had changed:
in `z04` the daemon re-stamped the empty catalog inside the resumed attempt
(1790745229 → 1790745288, new `CATALOG-HASH`), before any inspector exchange;
in `z05` the attempt started after that re-stamp and failed after a complete
inspector exchange. The Agent did not log which check produced the code. Pair 5
t3 completed because its retry came 14 s after the delete, before any
re-stamp. Batches 8r–11 established that PowerDNS 4.9.17 checks its producer
catalogs about every 60 s and re-stamps the producer SOA with the epoch serial
and a new `CATALOG-HASH` whenever the member set changed since the stored
hash, including to the empty set. That is the daemon, not an owner change.

**Admission rule** (`classifyProducerCatalogEvidence`, one function, one test
table). An observed producer catalog is compared with the attempt's recorded
evidence. It is *unchanged* when identity (producer address, peer address,
catalog name), member list, member serials, serial and `CATALOG-HASH` are all
equal (member lists element-wise, so an empty and an absent list are the same
zero-zone catalog). It is an *admitted daemon re-stamp* only on a PowerDNS
source when the serial is strictly higher, the `CATALOG-HASH` row is present
and differs from the recorded one, and identity, members and member serials
are identical. Everything else stays `dns_peer_owner_edit_unknown`
(detail `producer_catalog`): a member added or removed, a member serial, an
identity change, a lower or equal serial, and — decided — a higher serial
under an unchanged or missing hash (the daemon writes both; CelikPanel's own
publication never happens inside a recorded attempt, because the attempt holds
the host mutation lock and a pending V3 job blocks every other DNS mutation).
BIND has no daemon re-stamp: a BIND producer is admitted only when unchanged.
The hash is read only on a native V3 PowerDNS producer
(`verifyPDNSProducerBaseWithHashTxMode`, the row
`verifyNativePDNSProducerDaemonStateTx` already admitted); other PowerDNS
producers carry no hash, so a re-stamp there is still refused, as before.

**Where the evidence is recorded and re-stamped.** Each attempt — the
publication of an add, edit or delete (`syncPDNSV3Zone`) and the resumed job
(`RecoverDNSZoneV3` → `recoverPDNSV3Zone`) — reads the producer once in
`prepareManagedPDNSV3Propagation`. The completion wave turns that plan into one
`dnsRecordedProducerCatalog` and shares it with the native peer proof
(`plan.catalog`). Every comparison of the attempt judges against that record.
An admitted re-stamp re-stamps the record before the proof continues (the
wave's pair check re-proves against it; the native recheck admits it only once
its whole read bracket held). The record is attempt-scoped; there is no
separate durable copy. Each attempt re-derives it from the durable producer
rows, which already hold the daemon's re-stamp, so a resume minutes or hours
later starts from the re-stamped serial (`z05`). A durable copy across
attempts would add nothing the producer rows do not hold. After a real owner
edit it would also turn every retry into the same `producer_catalog` refusal,
with no way to proceed after the owner reconciled.

**Every comparison routed through the rule** (the call sites it replaced):
- the wave's pre-native catalog pair re-check
  (`completeDNSV3PrimaryPropagationWithNativeAt`; formerly
  `pdnsDaemonCatalogSerialAdvance`, bounded to two per wave and to "before any
  inspection"). This serves add/edit publication, deletion and the resumed
  job alike;
- the PowerDNS local recheck in the native proof (`recheckPDNSPeerLocalEvidence`;
  formerly `reflect.DeepEqual(current, plan.Evidence)`), run before the
  challenge, inside journal publish and consume-once, after the SSH round trip
  and before success;
- the BIND local recheck (`recheckBINDPeerLocalEvidence`; formerly
  `reflect.DeepEqual(current, plan)`). The plan identity is now compared by
  `sameDNSV3PlanIdentity` and the evidence by the rule, which for BIND admits
  nothing;
- the post-inspection pair check and `fresh != authority`, now shared by the
  BIND- and PowerDNS-secondary verifiers (`verifyNativePeerAfterInspectionAt`).

**Timing within the native proof.**
- *Re-stamp before the challenge* (`z04`): the recheck admits it. The recorded
  serial is then past the serial the wave proved at the peer, so the proof
  mints no challenge and opens no inspection
  (`errDNSProducerCatalogRestampedBeforeChallenge`). The wave re-proves the
  catalog pair at the new serial and calls the native proof again. This does
  not count as the wave's one inspection.
- *Re-stamp after the challenge*: the post-inspection pair check judges the
  re-stamped record. It waits up to 20 × 250 ms for the secondary to transfer
  the re-stamped catalog (measured about 0.1 s), and only while the record
  moved from the challenge serial through admitted re-stamps. It accepts the
  pair identity at the new serial only in that case.
- *Remaining gap, unchanged*: if the re-stamp reaches the secondary before
  its inspector reads the catalog, the signed response carries the new serial
  and `dnspeerproof.Verify` refuses it against the challenge. The attempt stays
  pending as `dns_peer_native_unknown`, and the next retry completes.

**Not changed here: the wave's time bound.** *(Corrected 2026-10-01, section
"Native peer proof per-step budget" below: the native proof no longer shares
the wave's 15 s context; each of its steps has its own bound, and consume-once
runs under a fresh context. The following sentences describe the state before
that correction.)* The completion wave and its one
native proof share the 15 s `dnsPairProofLimit` context. Every admission in
this section runs under it: the wave's refresh, the native local rechecks
(including those inside journal publish and consume-once), and the
post-inspection wait for a re-stamped catalog, which also stops when that
context ends. In `z05` the Agent's pending line came about 15.0 s after the
wave's first probe (05:31:47.7Z → 05:32:02.729Z), which is consistent with
that bound expiring after the inspection, but this is timing only. Pair 6
(`deploy/e2e/dns-pair-acceptance/evidence/pair6-20261001/README.md`) found the
bound expiring inside consume-once after a positive answer; that is a
separate correction.

**Momentary differences in the PowerDNS local recheck.** The recheck brackets
its reads between two reads of the daemon's process and database identity. A
daemon write during the bracket moves only the database file's size and
times. A half-written re-stamp shows a higher serial with the old hash, or the
reverse. In either case the recheck reads again, up to three times, 250 ms
apart, and logs it. A process restart, another inode or a lasting difference
is decided at once.

**Loop guard.** At most `dnsProducerCatalogRestampLimit` = 3 re-stamps are
admitted per attempt. The daemon re-stamps once per membership change, and no
membership change can happen inside an attempt, so one is the expected
maximum. The fourth is refused as `producer_catalog`, and the log says the
limit was exceeded.

**Owner-edit check tokens (D-024).** `dns_peer_owner_edit_unknown` now
carries one reviewed detail naming the check that found the difference, the
same way as the inspector detail of 2026-09-30: Agent ledger
`dns_peer_owner_edit_unknown:<check>`, Panel `reason` unchanged plus `detail`,
Domains screen sentence `domains.peerOwnerEditDetail.<check>` (EN/TR, each
starting "What differed:" / "Farklı olan:"), English fallback in
`dnsPeerOwnerEditDetailEnglish`. The reason text, "Retry this deletion" and
"same publication"/"aynı yayını" are unchanged. Tokens:

| Token | Check |
| --- | --- |
| `operation_attempt` | the durable ledger attempt of this operation changed or was lost |
| `engine_state` | the DNS engine state receipt changed or was unreadable (BIND: also the tree generation or pair receipt) |
| `active_engine` | another DNS service is active, or the managed one is not |
| `native_binding` | the daemon's process or database identity (PowerDNS), or the managed runtime configuration (BIND), changed |
| `deletion_receipt` | the exact durable deletion receipt of the operation changed |
| `producer_catalog` | the producer catalog differs other than by an admitted re-stamp (or the guard was exceeded) |
| `catalog_probe` | after the inspection, the catalog served locally or by the secondary did not match the record |
| `authority` | after the inspection, the pair answered for another identity or an unadmitted serial |
| `transfer_observed` | after the inspection, the secondary did not refuse the deleted zone's transfer |
| `zone_answered` | after the inspection, the secondary's answer was not the empty REFUSED seen before |

The Agent logs, at that point, "DNS peer proof observed different evidence
(check=<token>); the deletion stays pending as dns_peer_owner_edit_unknown:
<recorded and observed values>". The text is bounded to 512 bytes and
product-authored: addresses, catalog and member names, serials, a shortened
catalog hash, receipt generations and process/database identity. It holds no
key material. An admitted re-stamp logs "DNS peer proof admitted the PowerDNS
daemon's re-stamp of catalog <name> …: serial a -> b, CATALOG-HASH … -> …;
re-stamp n of at most 3". An observation that is not typed keeps the plain
code and logs `check=unclassified`. The managed-BIND configuration owner edit
recognised at recovery (`pendingExactBINDV3OwnerEdit`) is outside the peer
proof and keeps the plain code.

**Transition.** No persisted format changes. The composite owner-edit code is
additive. A Panel older than this Agent treats it as an unreviewed pending
code, as with the 2026-09-30 inspector composite; Panel and Agent ship
together. A deletion left pending by this defect is retried unchanged after
the owner updates the panel. The kill-matrix harness is not changed here; its
printed codes will carry the new detail.

**Evidence.** Component tests only: the admission table; the record
re-stamped and read back through the shared record; the loop guard; the
PowerDNS local recheck with fake readers (each token, a re-stamp written during
the bracket, a half-written re-stamp); the post-inspection tokens; and fake
daemon runs of the wave with the production helpers. In those runs a
re-stamp between pending and resume, inside the resumed attempt before the
challenge, and after the challenge with a late secondary each completes. An
owner member change and continuous re-stamping stay `producer_catalog`. The
tests also cover add/edit publication and the Panel/web texts EN/TR. Native
re-run pending:
- batch 12 `z04`/`z05` must show that the resumed deletion completes
  (`verified_published`, job `succeeded`, challenge consumed and retired). For
  `z04` (resume within 60 s of the delete) the Agent journal must show the
  "admitted the PowerDNS daemon's re-stamp" line, with no challenge before it.
  For `z05` (resume after the re-stamp) it must show none. The harness verdict
  `observe-child --step delete`, the re-add and, in `z05`, the reboot must
  follow. If either cell stays pending, the ledger code now names the check;
  that check and its logged values decide the next step;
- pair 6/7 t3 with a delayed retry (more than 60 s after the delete) must show
  the same completion.

### Native peer proof per-step budget; an accepted answer reaches consume-once (2026-10-01)

P0.4, P0.5; constitutional invariants 1, 4; D-024, D-025. Corrects pair 6
finding P6-1 (`deploy/e2e/dns-pair-acceptance/evidence/pair6-20261001/README.md`).
Replaces the single 15 s wave bound described as unchanged in the section
above. No journal, ledger, enrollment or database schema change; one additive
pending code. Component tests only; native re-run pending (pair 7; batch 12).

**Defect.** On all three pair 6 topologies the retried parentless deletion
after owner enrollment ran the inspection, `dnspeerproof.Verify` accepted the
peer's answer, and the attempt then stayed pending as
`dns_peer_journal_unknown`. The completion wave and its one native proof ran
under one `context.WithTimeout(ctx, dnsPairProofLimit)` (15 s). Challenge
write (3.5-4.1 s), SSH exchange (2.5-4 s) and post-inspection probes used it
up, and consume-once ran its three current-evidence rechecks (`systemctl`,
`ss`, named/pdns reads through `exec.CommandContext`) under the expired
context; the consume failure was mapped to the journal code. Wave start to
pending was 17.1-18.1 s (pair 6); pair 5 t3 completed in 13.8 s. A positive,
authenticated proof was discarded for a deadline. This is inferred from source
and timing; the Agent logged only the code.

**Budget.** The wave's DNS answer probes and their retries keep
`dnsPairProofLimit` (`dnsPairProofWaveLimit` in the wave, a variable only for
tests). The native proof no longer receives the wave's context: the wave
passes it the request context, and each step has its own bound
(`dnsPeerProofSteps`, `cmd/agent/dns_engine_peer_budget.go`):

| Step | Work | Measured (pair 5/6, batch 11) | Bound |
| --- | --- | --- | --- |
| `prepare` | active attempt, probe selection, enrollment read, recheck, previous-challenge reconciliation, journal read, challenge mint | about 1-3 s | 8 s |
| `challenge_write` | journal publish (four rechecks, durable write), enrollment recheck | 3.5-4.1 s | 12 s |
| `exchange` | SSH connect, pinned host key, key auth, forced command, answer | 2.5-4 s | 10 s = `dnspeertransport.ExchangeLimit` |
| `post_inspection` | recheck, catalog AXFRs, no-transfer, SOA, recheck (re-stamp wait at most 5 s) | about 2-3 s | 10 s |
| `consume` | `Verify`, consume-once (three rechecks, durable write), final recheck | about 3-4 s | 20 s, fresh context |

Total wall bound 60 s for the native proof, after at most 15 s of wave DNS
probes. The enrollment's transport bound (`Enrollment.Timeout`, built by both
enrollment readers) rises from 5 s to the transport maximum of 10 s; the
exchange step passes it as its own bound, so the transport's deadline is the
step's. Once a challenge is minted, every later step before acceptance also
ends 5 s (`expiryReserve`) before the challenge expires (lifetime 30 s,
unchanged), so an accepted answer leaves consume-once time to reach its own
expiry check. The measured values are the pair 6 journal/timeline gaps and the
batch 11 `z05` resume; they are inferred from other services' log lines, not
from Agent step timings, which this change adds.

**Fresh context rule.** The consume context is created inside Verify's
consume callback, that is only after the peer's answer passed validation,
authentication, the request binding and `transferred`/`absent`/`unloaded`:
`context.WithTimeout(context.WithoutCancel(requestCtx), 20 s)`. Consume-once's
rechecks and the final recheck run under it, never under an earlier step's or
the wave's remaining time. Re-verification keeps its semantics: a recheck
inside publish, reconciliation or consume-once that found a real difference
keeps its own reviewed code (`dns_peer_owner_edit_unknown:<check>`,
`dns_peer_enrollment_changed`, `dns_peer_proof_internal`) instead of being
flattened to the journal code. A consume failure without such a difference
(journal state, or the 20 s running out) stays `dns_peer_journal_unknown`.

**Deadline before acceptance: new reviewed code `dns_peer_proof_timeout`.** A
step before acceptance (prepare, challenge write, exchange, post-inspection)
that fails after its own bound or the acceptance deadline ran out is
`dns_peer_proof_timeout`, whatever its inner error. A check that could not run
because the time ran out is marked as a deadline (`dnsPeerProofDeadlineError`)
at the local recheck and at the post-inspection probes; it is never logged or
reported as an owner edit. Publish under an expiring context leaves the
journal consistent (no retained stage; at most an outstanding challenge of the
same operation, which the next attempt supersedes), and nothing was sent to or
changed on the secondary. The code is never used for the consume step. It is
carried like the other codes: `transport.DNSPeerPendingProofTimeout`, the
Agent's pending ledger `error_code`, the Panel's `reason` and English
fallback, the Domains screen (`err.DNS_PUBLICATION_FAILED.dns_peer_proof_timeout`,
EN/TR, in the reviewed set), and the pair driver's `REVIEWED_DNS_PEER_REASONS`
with actor "this server's owner (retry now; if it repeats, either owner checks
SSH reachability and load)". Text: the check of the secondary did not finish
within its time; nothing was changed on either server; the deletion stays
pending and DNS answers are unaffected; this server's owner can retry now with
"Retry this deletion" (the same publication, continuing from where it
stopped); if it happens again, either server's owner checks that the primary
can reach the secondary's SSH port and that neither server is overloaded. The
retry button is on the primary's panel, so the retry itself is named for this
server's owner; the reachability and load check is for either owner.

**`dns_peer_journal_unknown` guidance (D-024).** Meaning unchanged (the
private challenge journal could not be reconciled). The text now names the
concrete check: the Agent log lines for this deletion on this server
(`sudo journalctl -u celikpanel-agent | grep peer`), which name the step and
the reason; and that a retry is safe because each challenge is consume-once
and a retry starts a new one. EN/TR, Panel fallback.

**Agent log lines (D-024).** Per attempt of the native proof, bounded to 512
bytes of product-authored text, no key material:
- `DNS peer proof for <zone> (<BIND|PowerDNS> secondary) step times:
  prepare=<d> challenge_write=<d> exchange=<d> post_inspection=<d>
  consume=<d>; total <d>; outcome <verified | pending <code> | no challenge
  (catalog re-stamped; the wave proves the pair again)>` (only the steps
  reached are listed);
- for a pending outcome, once: `DNS peer proof for <zone> (<engine>
  secondary) stopped at step <step> as <code>: <underlying error>` (for
  example the journal state and the recheck error inside consume-once, or the
  SSH transport code);
- after the inspection, for both secondary engines: `DNS peer inspector answer
  for <zone> (<engine> secondary): catalog_state=<t> member_state=<t>
  native_state=<t>; accepted | not accepted: <reason>`; each field is
  reduced to lowercase letters, digits and `_`, at most 32 bytes.

**Other `dnsPairProofLimit` sites (unchanged; each bounds the DNS answer
probes of one check, not a native proof):** `queryDNSCatalogAXFRFrom`
(`dns_catalog_axfr.go`, one catalog AXFR), `verifyBINDCatalogDeletionAt`,
`verifyBINDDeletedZoneAt`, `verifyPrimaryCatalogHandoffEvidenceAt`,
`completeDNSBackendReadiness` (`dns_engine_host.go`, readiness deadline),
`retrievePDNSPairSecondaryZones` (recovery context without cancel),
`readLegacyPDNSPeerCatalogAuthority`, `verifyLegacyPDNSConsumerMemberAuthority`,
`verifyBINDPairingAuthorityAt`, `waitForExactBINDPairZoneSet`,
`verifyPDNSPairingAuthority`, `verifyDNSPrimaryPairReadyAuthorityAt` and
`verifyDNSLegacyPrimaryPairReadyAuthorityAt` (`dns_pair_readiness.go`; inside
the native proof they now run under the post-inspection step's context, so
their effective bound is the lower of the two), and
`verifyDNSSecondaryPairReadyAt`. The completion wave
(`completeDNSV3PrimaryPropagationWithNativeAt`) uses the same 15 s for its
DNS probes through `dnsPairProofWaveLimit`.

**Transition and recovery.** No persisted format changes; the new code is
additive. A Panel or driver older than this release shows the generic pending
text for `dns_peer_proof_timeout`; Panel and Agent ship together. A deletion
left pending as `dns_peer_journal_unknown` by the defect keeps its outstanding
challenge of the same operation; after the owner updates the panel, "Retry this
deletion" mints the next challenge of that operation and continues. The
challenge lifetime (30 s) and the consume-once and replay rules are unchanged.

**Evidence.** Component tests only (`cmd/agent/dns_engine_peer_budget_test.go`,
production proportions scaled by 1/50): the answer accepted at 14.5 s of the
15 s wave with a 3 s consume-once completes, consume runs past the wave's
bound, and the native proof no longer receives the wave's deadline; a deadline
in the exchange (the transport's own bound tighter than the step) is
`dns_peer_proof_timeout`, logged with its step and cause, and consume never
runs; the acceptance deadline ends a step before its own bound; checks after
their deadline are not owner edits and log no "observed different evidence";
consume's re-verification finding a real difference keeps
`dns_peer_owner_edit_unknown:producer_catalog`, a journal failure and an
exhausted consume bound stay `dns_peer_journal_unknown`, and consume survives
cancellation of the request context; the per-step timing and bounded answer
field log lines; the production budget (60 s, exchange = transport bound);
the Panel/web texts EN/TR and the driver's reviewed code. The real verifiers
(root, journal files, SSH) are not exercised by these tests. Native re-run
pending:
- pair 7 (all three topologies) must show that the retried parentless
  deletion after owner enrollment completes (200 `deleted`, ledger job
  `succeeded`, challenge consumed and retired), with the Agent journal line
  "step times: ... consume=...; ... outcome verified" and its per-step times
  (they replace the inference in pair 6's timing table), and on t2 the
  "inspector answer" line with `catalog_state=transferred member_state=absent
  native_state=unloaded; accepted`. A pending result must now show a "stopped
  at step" line naming the step and the underlying error;
- batch 12 `z04`/`z05` as listed in the section above, now also with these
  lines.
