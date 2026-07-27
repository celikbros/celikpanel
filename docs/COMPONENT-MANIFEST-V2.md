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
- executable candidates and separate argv values
- configuration templates and approved target paths
- readiness probes and rollback counterparts

The catalog may not grant authority or contain an unrestricted shell program.
The agent must reject shell text such as `sh -c`, `bash -c`, `cmd /c`, or
PowerShell script strings. Executable and every argument are stored separately
and invoked through a direct process API. File and service operations use typed
agent adapters rather than arbitrary command strings.

The following always remain in Go:

- authentication, authorization, entitlement, and audit policy
- trusted host-profile detection
- recipe validation and deterministic recipe selection
- process, package, file, service, firewall, permission, and probe adapters
- path traversal, environment, timeout, secret, and argument checks
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
4. `ID_LIKE` and version range
5. `ID_LIKE`
6. package manager and service manager
7. explicit OS-family default

Two matches at the same specificity are an error. Untested new OS versions do
not match unless the selector explicitly permits an open-ended version range.

## Minimum schema domains

The normalized catalog contains these domains:

- release metadata: schema version, catalog version, minimum agent version, key ID
- platform selectors
- component recipes and operation-specific support state
- operation variables with types and validation rules
- ordered typed steps: package, executable, file, service, firewall, and probe
- executable candidates with absolute paths
- argv and environment values as separate rows
- templates with content hashes and atomic-write policy
- readiness probes
- forward-step to rollback-step relationships

## Signing and updates

The catalog is root-operation policy and therefore part of the software supply
chain.

- Sign the database digest with Ed25519 and embed the trusted public key in the agent.
- Open the verified database read-only and immutable, without WAL or journal files.
- Validate signature, schema, catalog version, and minimum agent version before activation.
- Download to a new file and activate with an atomic rename.
- Keep the previous two verified releases for rollback.
- Build and sign a new database for schema changes; never migrate it at runtime.
- Do not provide an unsigned local recipe editor in the panel UI.

Every audit event records the manifest digest, recipe ID and revision, redacted
argv, exit code, readiness result, and rollback result.

## Completion and rollback rules

An installation is successful only after package, configuration, service, and
component-specific functional probes pass. Merely returning exit code zero from
a package manager is insufficient.

Before execution, the agent records package presence, service active/enabled
state, hashes and safe backups of owned files, and CelikPanel-owned firewall
state. Rollback reverts only changes made by that operation. It never deletes
existing user data. If a safe rollback is impossible, the operation ends as
`failed_recovery_required`.

## Platform adapters

Additional distributions that share existing adapters can normally be added by
catalog data alone. A completely new OS family still requires a one-time trusted
adapter implementation for host probing, process execution, files, services,
firewall, permissions, and readiness probes.

- Linux: package managers, systemd, nftables, Unix permissions
- FreeBSD: pkg, rc.d/sysrc, pf or ipfw
- Windows: native process API, SCM, Windows Firewall, ACLs

After the adapter exists, component and release differences belong in recipes.

## Migration sequence

1. Implement HostProfile, schema validation, signature verification, and dry-run plans.
2. Move Memcached first for Debian/Ubuntu and Arch, including lifecycle and TCP readiness.
3. Compare legacy and V2 plans in shadow mode without executing V2.
4. Move simple package-backed services.
5. Move web, DNS, mail, VPN, and database tools with typed configuration steps.
6. Move runtimes, vendor repositories, version selection, and Roundcube.
7. Add and test the FreeBSD adapter.
8. Add and test the Windows adapter.
9. Remove legacy OS branches only after verified parity.
