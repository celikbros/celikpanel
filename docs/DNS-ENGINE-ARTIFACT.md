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
