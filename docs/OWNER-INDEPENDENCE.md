# Owner-controlled service operation

*September 12, 2026 ? [Türkçe](OWNER-INDEPENDENCE.tr.md) ? D-022 implementation audit*

## Current boundary

The source change separates secondary DNS replication from optional remote
record-management automation. It does **not** certify removing CelikPanel and
all its directories from an installed hosting server. The audit below records
remaining dependencies; no installed server was changed by this work.

## DNS behavior

When a local secondary also hosts websites, applications or mail, the wizard
now offers two record-management choices:

- **I will manage records on the primary DNS server.** No remote panel URL,
  enrollment or credentials. BIND/PowerDNS retains its native secondary role.
  New hosted domains use external DNS instructions and verification; the owner
  creates their records on the primary through their preferred native tools.
- **Manage records through a CelikPanel connection.** The existing explicitly
  authorized connector publishes records on the primary. Its HTTPS endpoint
  belongs to management automation, not AXFR/IXFR/NOTIFY.

The manual choice is explicit in the reviewed draft and plan identity. Editing
it invalidates the previous review. A missing field in an already stored plan
keeps the Alpha71 connector behavior. The new editor defaults to manual when
no prior publisher endpoint exists; saved endpoints retain automatic selection.
Existing domain ownership is not rewritten. A pure DNS purpose keeps its local
DNS default. Manual secondary hosting still requires native pair and public
nameserver proof before hosting steps can continue.

The shipped replication implementation uses standard RFC 9432 version 2 catalog
zones to provision secondary members. A peer configured outside CelikPanel must
provide or consume the matching catalog and allow the peer's transfers. Merely
running BIND with unrelated zones is not sufficient. The expected catalog is
`catalog-<primary IPv4 bytes in lower-case hex>.celikpanel.invalid`; member labels
are lower-case SHA-224 of canonical zone names. See
[`internal/binddns/pairing.go`](../internal/binddns/pairing.go).
The generated transfer ACL is restricted to the reviewed peer. This change does
not add arbitrary catalog names, general zone adoption, TSIG enrollment or
reciprocal per-zone primary roles.

## Evidence

[Local validation](validation/native-dns-independence-20260912/README.md) covers
manual/automatic draft paths and a BIND-primary/PowerDNS-secondary pair. In two
disposable Debian 13 guests, both management services were stopped and their
standard binary paths removed by moving the binaries to a fixture evidence
directory. The primary was changed to a plain owner-written BIND configuration
with no CelikPanel include. Native catalog addition, record change, daemon
restarts and catalog removal propagated to PowerDNS without a panel API.
This establishes DNS operation for that tested pair, not full hosting removal
or whole-host reboot independence.

## Dependency audit and required follow-up

| Area | Observed dependency | Required resolution before removal support |
|---|---|---|
| DNS serving and transfer | Native named/pdns, native files/database and catalog; no panel process needed for tested replication | Retain DNS files/database, service accounts and daemon units. Detect owner edits before later panel writes. |
| Remote record automation | Requires the opted-in primary panel connector to create/change records | Optional; disable/remove credentials on explicit handover. Native existing DNS serving continues. |
| Mail host certificate renewal | Certbot deploy hook invokes `/opt/celikpanel/bin/agent --deploy-mail-host-certificate`; renewal is queued and deployed by the agent worker | Native Certbot deployment and service reload independent of the management agent, preserving atomic certificate publication, approved lineage and concurrent-change protections. |
| Firewall after host reboot | `celikpanel-firewall-restore.service` invokes the agent; persisted `firewall.nft` may be versioned JSON, not directly loadable nft syntax | Migrate to a native persisted ruleset and independent boot restore. Preserve other tables and reviewed SSH access; prove reboot and failed-restore behavior on supported platforms. |
| Node runtimes | Executables live below `/opt/celikpanel/runtimes` | Retain workload runtimes or migrate unit executable paths. Removing `/opt/celikpanel` recursively is not supported. |
| ACME challenge paths | `internal/hostingpath` uses `/var/lib/celikpanel-agent/acme-http-01`; website Certbot work is `/var/lib/celikpanel/certbot` | Retain or migrate challenge roots, lineage/work directories and native renewal schedules with a real renewal test. Directory names do not alone imply a running-agent dependency. |
| Panel HTTPS renewal | Its deploy hook invokes the agent for panel certificate material | Panel-only lineage/hook cleanup must be separate from shared website/mail renewal; do not disable Certbot globally. |
| Website/mail/database data and configurations | Native services exist, but not every generated path, mail map, auth source or scheduled deployment has been certified for removal | Produce an exact retention manifest and prove web requests, DB transactions, mail delivery/authentication, cron and renewal with both management binaries absent. |
| Tenant cron | `cmd/agent/cron_rpc.go` writes native user crontabs | Preserve users/homes/crontabs and inspect the job commands for owner-selected panel dependencies. Native cron must remain enabled. |

Relevant source: `cmd/agent/mail_host_certificate_linux.go`,
`cmd/agent/mail_host_certificate_renewal.go`,
`deploy/systemd/celikpanel-firewall-restore.service`,
`cmd/agent/firewall_rpc.go`, `cmd/agent/runtime_rpc.go`,
`cmd/agent/ssl_rpc.go`, `internal/hostingpath/path.go`.

Changing renewal or boot persistence is a separate migration with durability,
rollback and native acceptance requirements; replacing those paths with unchecked
shell copies would not meet D-022. No removal command is offered or certified by
this change. Panel removal must eventually be an owner-reviewed operation that
preserves all workload state and leaves a documented native administration path.


## Shared firewall policy contract (source stage)

D-025 invariants 1, 3, 6 / P0.5. The persisted firewall producer, reader and boot
ruleset preparation now share `internal/firewallpolicy`. The package imports only
the Go standard library and has no Agent, application database, license, host
command or filesystem dependency. The management Agent uses this same package
for v2 writing, exact legacy/v2 reading and restoring the reviewed TCP/UDP policy
with the current verified SSH ports and the saved SSH transition guard.

The stored v2 JSON, null/empty representation, size limit, canonical comparison
and exact legacy nft format stay unchanged. No schema migration or on-disk
normalization is performed. Unsupported versions, injected nft text, duplicate
JSON fields, altered canonical bytes and invalid configured SSH ports are refused.
The generated batch touches only `inet celikpanel_fw`, never the host ruleset.
Authority, secure file reads, SSH discovery, nft preflight, atomic application and
rollback still belong to the existing caller; a rendered plan is not kernel proof.

Race-enabled package tests and existing Agent firewall/port-reader tests pass.
This is the shared contract needed by the independent boot consumer, not its
activation. The installed boot unit still invokes Agent. Native consumer packaging,
owner-reviewed persistence/boot migration, failed-restore recovery and real
Agent-absent reboot acceptance remain open. Existing installations are unchanged.


## Independent firewall consumer: bounded native evidence

The source `cmd/firewall-restore` now reads the shared legacy/v2 policy without
Agent, application DB or licensing. [Debian AR evidence](../deploy/e2e/release-recovery/FIREWALL-BOOT.md)
proves an orderly reboot with both management binaries absent, fresh SSH access
and a retained unrelated nft table. The fixture uses the existing root-owned,
non-writable `celikpanel` group layout. There is no stored-schema transition.

The installed unit still uses Agent. Packaging, shared mutation exclusion,
upgrade/rollback retention, failed-boot recovery and the remaining native workload
matrix stay open; this result does not close P0.5 or certify panel removal.

The subsequent [shared exclusion source and native contention proof](../deploy/e2e/release-recovery/FIREWALL-BOOT.md#shared-process-exclusion-next-source-stage)
cover both the independent consumer and Agent entry points. Busy state preserves
unverified/ambiguous outcomes; the kernel releases exclusion after process death.
This closes the source-level shared-lock gap, not production activation or the
whole native concurrency/upgrade matrix.


## Versioned firewall artifact (not yet activated)

The build now packages `firewall-runtime/` separately from the application bin
resource. `celikpanel-firewall-runtime/v1` binds policy-reader version 2, exact
binary bytes, a native systemd unit and a generation derived from the binary and
frozen v1 unit template. The unit refers to an immutable path below
`/usr/libexec/celikpanel/firewall/<generation>/restore`. The same contract builds
and verifies the payload; unknown versions, noncanonical manifests, substituted
binaries and edited units are refused. Existing legacy/v2 policy bytes do not change.

The offline builder preserves unrecognized output and retains the previous known
build during atomic replacement. Normal archives and the source bootstrap carry
the same independent payload; the outer release checksum/signature covers it.
A checksummed artifact is not installation authority. The current installer and
installed boot unit remain unchanged until durable publication, unit transition
and rollback retention are implemented and proved.

Validation: race-enabled contract/builder tests, refusal of linked input and
owner-edited output, and two real Go 1.26.5 builds on WSL's Linux filesystem under
022/077 umasks produced identical payload bytes and 0755/0644 modes. The Windows
mount's synthetic 0777 modes were refused; no permission check was weakened.
This establishes artifact reproducibility, not full release/native activation.


## Durable firewall preparation (unit activation still pending)

The candidate recovery entry now supports `prepare-firewall-runtime --source
<absolute reviewed firewall-runtime directory> --transaction-fd 9`. It requires
root and the existing exclusive release-lock descriptor with no active transaction
markers. The caller must first admit the outer release; this internal command
cannot initiate an update, install a unit, apply firewall rules or start a service.
It is not dispatched to an older selected recovery executable.

Preparation shares recovery's pinned root-owned directory/file validation and
publishes only a complete verified generation below the fixed independent root.
Each file and its directory are synced before no-replace rename; the parent is
synced before reporting success. An existing generation is reverified and synced
on retry, never overwritten. Partial stages and old generations are retained.
Owner edits, substituted paths, unexpected files and changed source evidence stop
preparation before any unit transition. No policy or installed-unit format changes.

Root child-process tests pass for inherited-lock enforcement, active transaction
refusal, unsafe source metadata, late source/destination changes, collisions and
retained predecessors. Actual SIGKILL at first-file, durable-stage, published and
parent-durable boundaries is followed by successful idempotent preparation.
These are local Linux filesystem/process tests, not power-loss or native service
activation acceptance. The full runtime/CLI race suite and vet also pass. Automatic
installer/update wiring, exact unit transition, rollback helper retention proofs
and the remaining workload matrix are still open under P0.5.


The subsequent preflight wiring invokes preparation for releases carrying the
artifact: the normal updater does so before runtime promotion/coordinator
quiescence, and the fresh installer before foundation-intent publication.
Historical archives without the artifact keep their existing path. A refused
preparation halts that flow; incomplete preparation has its own
`firewall_runtime_preparation_unconfirmed` outcome instead of false unchanged
state. Real inherited-FD shell tests and the real extracted fresh-installer
function cover successful preparation and corrupt-payload refusal. This wiring
still does not switch the installed firewall unit to the prepared helper.


[Debian AR generation acceptance](../deploy/e2e/release-recovery/FIREWALL-GENERATIONS.md)
now proves the actual bundled native unit across three A/B/A boots with Panel
and Agent binaries absent. Both helper generations, saved policy and unrelated
native table remain intact. A preliminary incomplete table fixture is retained.
The generations are two builds of the same audited reader; this is not semantic
version migration or a normal application-update/rollback proof. P0.5 stays open.


## Packaged native unit transition (source stage)

D-025 invariants 1, 3, 4 / P0.3, P0.5. Offline distributions and source-bootstrap
releases now copy the exact bundled firewall unit into their reviewed systemd
payload. The source checkout's historical unit is retained for compatibility;
this does not certify direct source-checkout installation or panel removal.
The helper is prepared before coordinator downtime, and the candidate reader
then verifies the intended unit against its retained generation. Fresh packaged
installation performs the same proof before foundation intent is published.

The existing atomic old/candidate unit transition now also verifies the helper
required by its intended destination. A damaged candidate blocks publication,
including an otherwise idempotent retry, but cannot prevent rollback to a valid
legacy unit. An old independent unit requires its own retained helper to verify.
Unknown templates, owner edits, read errors, missing or changed helpers retain
evidence and stop the affected transition. Only the intended unit changes; both
helper generations remain outside application payload replacement. Policy
legacy/v2, artifact v1 and complete snapshot v6 formats stay unchanged.

Validation: full runtime/CLI race tests; root-owned read-only unit/helper proof
with changed, linked and unsafe evidence; real extracted fresh-install and update
preflights; atomic shell transition with destination-only helper refusal and old
rollback. These are source/component checks. Previous native A/B/A evidence uses
the exact bundled unit but not the normal application update body. Full signed
update/automatic rollback, failed-boot console recovery, Arch and the remaining
workload/renewal matrix remain open. No installed owner server is changed.


The subsequent [Debian AS/AT native update acceptance](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md)
now covers the actual Agent signed worker with isolated fixture trust: a normal
update publishes the independent unit; a second worker is killed after candidate
unit/helper publication and native recovery automatically restores the old unit.
Both branches retain policy, the unrelated table, the helper and working HTTPS
across orderly boots. AS explicitly enabled boot after preserving its previously
disabled state; AT preserved its already enabled baseline. Production UI/trust,
Arch, failed-helper boot/console recovery and the rest of P0.5 remain open.

Arch AU now passes the same exact candidate-unit publication cut and automatic
rollback, followed by a distinct boot with policy, unrelated table, enablement and
HTTPS preserved. [Native evidence](../deploy/e2e/release-recovery/FIREWALL-UPDATE.md#arch-automatic-rollback-and-boot-au)
retains the preliminary baseline platform-cache refusal; it is not silently
counted as automatic recovery. Successful Arch forward update remains open.

[Shared mail certificate artifact v1](MAIL-CERTIFICATE-ARTIFACT.md) now removes
Agent coupling from receipt/pending/lineage parsing and certificate verification.
Actual Alpha81 producer bytes remain exact in the new shared and Agent readers.
This advances P0.4/P0.5 groundwork only; the native renewal consumer, hook migration
and management-absent renewal acceptance remain open.

The [shared descriptor reader](MAIL-CERTIFICATE-ARTIFACT.md#shared-descriptor-reader)
now serves actual Agent mail certificate reads without an Agent dependency. It
preserves historical file trust and rejects observed concurrent owner selection
changes without rewriting them. Native deployment/reload and renewal absence
acceptance remain open; no new artifact version or installed migration is claimed.

[Arch AV forward update and boot](../deploy/e2e/release-recovery/PLATFORM-UPDATE-AV.md)
now passes with the exact candidate helper/unit, retained owner enablement, policy,
unrelated table and HTTPS. The corrected Agent also changes from a native starting
refusal to a ready update check in the same PID/start/invocation after boot. This
supersedes the scoped open Arch-forward/same-process items above, not the remaining
P0.2/P0.3/P0.5 matrix, production UI/trust or independent renewal acceptance.

[Shared certificate publication exclusion](MAIL-CERTIFICATE-ARTIFACT.md#shared-publication-exclusion)
now uses the historical fixed flock identity outside Agent code, supports bounded
waiting and refuses replaced/owner-modified locks without normalization. Actual
Agent calls use it; root cross-process death/exclusion tests pass. This advances
P0.4/P0.5 shared-contract groundwork; native renewal publication, outer mutation
exclusion, hook migration and management-absent acceptance remain open.

[Shared native Certbot reader](MAIL-CERTIFICATE-ARTIFACT.md#shared-native-certbot-source-reader)
now supplies both actual Agent source paths from the same confined live/archive
implementation. Existing chain/lifetime/purpose checks remain in the caller, and
its adversarial source tests pass. This removes another P0.4/P0.5 duplication risk;
it does not activate a native renewal hook or close management-absent acceptance.


[AX mail acceptance](../deploy/e2e/release-recovery/MAIL-CONTRACT-AX.md) now proves
real shared-contract renewal, retained native TLS after an orderly Debian reboot,
and preservation of an owner-selected certificate plus pending work after replay
of a completed renewal. P0.4/P0.5 evidence is partial: renewal still invokes Agent
code; independent helper enrollment, retries, hook migration and removal remain
open. No installed owner panel was changed.


The [accepted mail TLS plan](MAIL-CERTIFICATE-ARTIFACT.md#shared-accepted-mail-tls-plan)
now has one v1 contract shared by actual Agent production and recovery readers.
Actual Alpha81 empty/SNI producer bytes are preserved exactly. Parsing accepted
intent does not establish current health or authorize native helper mutations;
the independent renewal transaction and its native acceptance remain open.


[Shared immutable mail publication](MAIL-CERTIFICATE-ARTIFACT.md#shared-immutable-publication)
now serves the actual Agent producer and preserves observed owner changes to
staged material/current selection. The [AY native trial](../deploy/e2e/release-recovery/MAIL-CONTRACT-AY.md)
passes real renewed TLS, orderly boot and owner-drift replay with this producer
and the shared accepted-plan reader. No schema migration or independent renewal
helper is claimed; Agent commit/recovery and service convergence remain required.


[Mail recovery cleanup](MAIL-CERTIFICATE-ARTIFACT.md#recovery-cleanup-shares-the-artifact-contract)
now uses the shared complete-generation validator rather than receipt-only
removal. Observed owner changes, selected generations and extra files remain
intact. Component/race evidence advances P0.4; no new native interrupted-recovery
or independent renewal claim is made.


[Retained AY owner-reviewed cleanup](../deploy/e2e/release-recovery/MAIL-CLEANUP-AY.md)
adds real native evidence for the shared cleanup path: refusal preserves owner
files, explicit owner resolution allows exact cleanup, and native mail/queued
renewal remain intact. This controlled uncommitted stage does not establish
crash/startup recovery or full P0.4/P0.5 completion.


[Shared host exclusion](HOST-MUTATION-EXCLUSION.md) now serves actual Agent
observation and the independently built recovery checker. It refuses replaced
or malformed lock evidence without normalization or FIFO waits, preserves the
existing outer flock identity, and proves inherited exclusion without releasing
it. Native-filesystem component/process and standalone-checker tests pass. Native
renewal enrollment/runtime initialization and the full P0.3-P0.5 matrix remain open.


[Native mail configuration sharing](MAIL-CERTIFICATE-ARTIFACT.md#shared-native-mail-configuration-contract)
now gives actual Agent producers and retained-plan comparison one tested contract,
with historical Alpha81 byte fixtures and owner-change/missing-observation tests.
This advances P0.4; configuration comparison is not renewal authority or service
health. Agent-independent renewal and the full P0.5 absence matrix remain open.


The shared Dovecot dialect observation now accepts the implemented 2.3/2.4
syntax only after a successful version command. Missing, malformed and failed
observations are unknown; an observed unimplemented version is unsupported.
Neither becomes an assumed 2.4 host. Actual TLS preflight stops before snapshot,
map preparation or configuration mutation; direct TLS and virtual-mail writers
also require verified dialects. Retained-plan readback cannot certify an unknown
dialect. Native configuration bytes and on-disk schemas are unchanged. Component
race tests cover nonzero exit with plausible output, empty/malformed/unsupported
observations, preserved untouched outcome, no later command, no raw-output leak,
and successful 2.3/2.4 parsing; existing mail tests and vet pass. Native acceptance
is still pending at this source stage. Other version probes and the full
independent renewal transaction remain open.


[Subsequent AY native dialect acceptance](../deploy/e2e/release-recovery/MAIL-DIALECT-AY.md)
now verifies the shared host lock and native retained-plan readback on the exact
current source. A real failing version executable prevents configuration work;
configuration, ledger, queued renewal and trusted SMTP/IMAP leaf stay unchanged.
This closes the bounded native observation check, not independent renewal,
complete effective native configuration verification or crash recovery.
