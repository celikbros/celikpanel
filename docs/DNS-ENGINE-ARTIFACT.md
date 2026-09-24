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