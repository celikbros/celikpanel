# Component Manifest V2

Status: architecture decision; implementation is intentionally incremental.

## Decision

CelikPanel will move operating-system-specific component recipes into a signed,
read-only SQLite catalog. SQLite is only a data store. It never executes a
command. The agent remains the sole trusted executor and is responsible for
authorization, validation, locking, process execution, readiness checks,
rollback, and audit logging.

The immutable catalog and mutable runtime state must remain separate:

- `/usr/share/celikpanel/manifests/components-v2.db`: signed recipe catalog
- panel database: operations, audit events, step results, and rollback journal

## Trust boundary

The catalog may describe:

- OS, distribution, version, architecture, package manager, and service manager selectors
- support state and unsupported reason per component operation
- package and service names
- ordered typed operation steps
- configuration templates and approved target paths
- readiness probes and rollback counterparts

The catalog may not grant authority or contain a generic executable step.
`exec`, `exec_probe`, `program`, `args`, and `environment` are not part
of V2. Package, file, service, firewall, and probe operations use typed agent
adapters whose executable choices remain compiled into trusted Go code. This
leaves no interpreter path such as Python `-c`, `env sh`, BusyBox, `cmd`,
or PowerShell for catalog data to select.

The following always remain in Go:

- authentication, authorization, entitlement, and audit policy
- trusted host-profile detection
- recipe validation and deterministic recipe selection
- process, package, file, service, firewall, permission, and probe adapters
- path traversal, typed-variable, timeout, secret, and adapter-input checks
- package-manager and operation locking

## Support states

Support is operation-specific. For example, status may be supported while
automatic installation is not.

- `supported`: may be planned and executed automatically
- `unsupported`: deliberately unavailable on this platform, with a reason
- `manual_only`: an existing installation may be detected or managed
- `unavailable`: no tested matching recipe exists
- `blocked`: a recipe exists but a dependency, conflict, repository, or policy blocks it

Missing data must never silently fall back to a generic Linux command.

## Host selection

The agent creates a trusted host profile, for example:

```text
os_family=linux
distro_family=debian
distro_id=ubuntu
distro_like=debian
version=24.04
architecture=amd64
package_manager=apt
service_manager=systemd
```

Recipes are selected from most to least specific:

1. exact distro, version range, and architecture
2. exact distro and version range
3. exact distro
4. distro family and version range
5. distro family
6. `ID_LIKE` and version range
7. `ID_LIKE`
8. package manager and service manager
9. explicit OS-family default

Two matches at the same specificity are an error. Untested new OS versions do
not match unless the selector explicitly permits an open-ended version range.
Every recipe must name an explicit audited `os_family`; a distro or package
manager without that boundary is rejected. The managed-server schema accepts
only `linux` recipe data. Every non-Linux family is rejected because it has no
supported mutation or activation contract.

## Minimum schema domains

The catalog uses a small relational envelope with versioned JSON payloads. It
does not split every possible step field into another table, and it does not
hide the entire catalog inside one unqueryable JSON blob.

Relational columns own identity and lookup:

- release metadata: schema version, catalog version, monotonic catalog sequence,
  minimum agent schema, and key ID
- item ID, item kind (`component`, `addon`, `application`), revision, and enabled state
- recipe ID, item ID, platform key, operation, revision, and support state

Strict JSON objects own the parts that vary by platform and operation:

- item presentation and product metadata
- platform selectors
- typed operation variables and validation rules
- ordered package, file, service, firewall, and probe steps
- templates, readiness probes, and rollback relationships

All signed JSON rejects duplicate keys and uses exact, case-sensitive field
names; Go's case-insensitive struct matching is not accepted. Selector and
recipe JSON are decoded with unknown fields rejected. Each step
type has an exact semantic field allowlist, and rollback references are valid
only on entries in the main `steps` list. Item `metadata` is intentionally an
extension bag for presentation and product data: unknown keys are accepted
there, but metadata is never interpreted by an executor and cannot change
authorization, selection, or operation behavior. The agent validates every
recipe before it can participate in deterministic selection.

## Signing and updates

The catalog is root-operation policy and therefore part of the software supply
chain.

- Sign the database digest with Ed25519 and embed the trusted public key in the agent.
  `BuildCatalog` returns the digest of its still-open private inode, and signing
  must receive that digest. Signing fails if reopening the published path does
  not produce exactly those bytes. Build, signing, and opening all enforce the
  same 64 MiB database limit.
- Copy the candidate, with that 64 MiB size limit, into a private `0700`
  temporary directory as a `0600` snapshot. Hash and verify that snapshot,
  then open the exact same snapshot read-only and immutable with
  `trusted_schema=OFF`; never hash one pathname and reopen mutable source
  bytes.
- Validate exact `sqlite_master.sql`, `table_xinfo`, tables, columns, CHECK
  constraints, foreign keys, indexes, absence of extra schema objects, one and
  only one metadata row, row-domain invariants, catalog version, and minimum
  agent schema before use.
- Require a positive `AgentSchema`. A bootstrap policy uses sequence zero and
  no digest. After bootstrap, require
  `OpenPolicy{MinimumCatalogSequence, MinimumCatalogDigest}` with a positive
  sequence and its 64-character lowercase SHA-256 digest. Reject a lower
  sequence and reject a different digest at the same sequence, preventing both
  replay and same-sequence equivocation.
- `OpenPolicy` is only the verification API. Runtime activation and durable
  replay-state persistence are not implemented by this package. A future
  activator must serialize activation, begin a transaction, re-read the current
  sequence-and-digest floor inside that transaction, compare the candidate, and
  use a CAS-equivalent guarded update before commit. It may commit the new floor
  only for the catalog it successfully activates. Every later open must pass
  both persisted values. The catalog package never silently lowers or resets
  this floor or discards its same-sequence digest pin.
- Before staging, Linux opens the publish parent with
  `O_DIRECTORY|O_CLOEXEC|O_NOFOLLOW`, pins it, and validates it with `fstat`.
  The parent must be owned by root or the current effective UID and must not be
  writable by group or other; a symlink, ownership mismatch, or unsafe mode
  fails closed. Create the random private staging directory and catalog with
  `mkdirat`/`openat` beneath that pinned parent, using modes `0700`/`0600` and
  `O_NOFOLLOW`.
- SQLite reaches the staged catalog through its pinned staging-dirfd path, and
  the same already-open regular-file inode remains authoritative through build,
  sync, digest, and one atomic no-overwrite hard-link publication. The signer
  independently revalidates and pins the final parent, opens only its basename
  with `openat(..., O_NOFOLLOW)`, rejects a non-regular, wrongly owned, or
  group/other-writable artifact, and hashes and signs that exact open inode with
  the build digest as an expected-value pin. The same dirfd boundaries are used
  for inspection and cleanup. A directory-wide advisory lock serializes all
  cooperating publishers through link, sync, and possible cleanup. Sync the
  file and parent directory. If post-link directory sync fails, return a typed
  partial-publication error, remove only a destination that is still the built
  inode, and sync the cleanup; report when a destination may remain. An existing
  destination is never modified.
- Building and opening fail closed on every non-Linux GOOS. Linux is the only
  managed-server and filesystem-activation target. `!linux` files exist only
  for build portability and preserve a no-mutation, fail-closed contract; they
  do not authorize non-Linux recipe data or promise product support.
- Keep prior verified releases for forensic or explicit emergency recovery.
  Normal rollback may select only a release whose sequence is not below
  `highest_accepted_catalog_sequence`; lowering the floor requires a separate,
  explicit, audited recovery policy and is never a normal `OpenPolicy` path.
- Build and sign a new database for schema changes; never migrate it at runtime.
- Do not provide an unsigned local recipe editor in the panel UI.

Every audit event records the manifest digest, catalog sequence, recipe ID and
revision, typed adapter action with redacted inputs, exit code, readiness
result, and rollback result.

## Completion and rollback rules

An installation is successful only after package, configuration, service, and
component-specific functional probes pass. Merely returning exit code zero from
a package manager is insufficient.

Before execution, the agent records package presence, service active/enabled
state, hashes and safe backups of owned files, and CelikPanel-owned firewall
state. Rollback reverts only changes made by that operation. It never deletes
existing user data. If a safe rollback is impossible, the operation ends as
`failed_recovery_required`.

The privileged agent now owns a durable service-mutation ledger and a
cross-process host lock. V2 execution nevertheless remains disabled until a
trusted activator binds each resolved recipe sequence and catalog digest to
that lease, commits activation with digest compare-and-swap, and enforces the
executor-side package, unit, path, probe, and firewall allowlists described
below.

## Platform adapters

CelikPanel's target architecture defines three first-class Linux distro-family
adapters. Distribution names are compatibility fixtures, not separate
installer engines:

- `debian`: Debian and Ubuntu; `apt`/`dpkg`, systemd, AppArmor, and nftables
- `rhel`: RHEL, AlmaLinux, Rocky Linux, CentOS Stream, Fedora, and CloudLinux;
  `dnf`/RPM, systemd, SELinux, and firewalld/nftables
- `arch`: Arch Linux; `pacman`, systemd, and nftables

The trusted host profile derives `distro_family` from `ID` and `ID_LIKE` in
`/etc/os-release`. A name alone never authorizes mutation: the adapter first
verifies the package manager, service manager, and required security
capabilities and fails closed on a mismatch. Genuine distribution differences
in packages, repositories, services, security policy, or paths use narrow
recipe overrides. `ID_LIKE` may identify a compatibility candidate but never
enables mutation until that distribution identity and the requested capability
have explicit certification evidence.

openSUSE/SLES, Alpine, and NixOS require distinct future family adapters if
proven demand justifies them. Kali Linux is explicitly refused despite its
Debian ancestry. FreeBSD and Windows are not managed-server targets.

Schema validation is not execution authorization. Before runtime activation,
the trusted adapter must bind each item id and typed step to compiled component
boundaries: allowed package names or prefixes, service units, writable path
roots, and firewall endpoint policy. Any recipe target outside those boundaries
must fail closed. That executor-side allowlist and activator are not implemented
by the current package, so V2 remains a verified dry-run foundation only.

## Migration sequence

1. Implement HostProfile, schema validation, signature verification, and dry-run plans.
2. Add the agent-owned durable job ledger, heartbeat/deadline recovery, and cross-process lock.
3. Move Memcached first through the `debian` family adapter, then certify Debian and Ubuntu fixtures, including lifecycle and TCP readiness.
4. Add the `rhel` family adapter and certify representative RHEL, AlmaLinux, Rocky Linux, CentOS Stream, Fedora, and CloudLinux fixtures.
5. Add the `arch` family adapter and certify the current Arch Linux fixture.
6. Compare legacy and V2 plans in shadow mode without executing V2.
7. Move simple package-backed services across the three families.
8. Move web, DNS, mail, VPN, and database tools with typed configuration steps.
9. Move runtimes, vendor repositories, version selection, and Roundcube.
10. Remove legacy OS branches only after verified parity.
