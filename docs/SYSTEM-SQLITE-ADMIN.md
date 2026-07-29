# System SQLite Administration

Status: implemented first-release scope.

## Purpose

CelikPanel provides administrators with a maintenance view for the SQLite
databases used by the panel and selected managed services. This is a
fixed-inventory maintenance surface, not a general-purpose SQLite file
manager.

The current implementation provides:

- administrator-only inventory
- a summarized `PRAGMA quick_check`
- creation and download of a safe snapshot
- an explicit `PRAGMA optimize` action for supported mutable databases

It does not provide a path picker, SQL console, table or row editor, schema
editor, database upload, restore, replacement, or deletion.

## Where administrators use it

An administrator opens **Databases** and selects **System SQLite**. The page
lists the known system databases and shows their availability, size, journal
mode, user version, health status, and supported actions.

Each card's **Available actions** value is derived from the exact action list
returned by the agent. A general mutable/read-only label is not used as a
substitute for operation capability.

The page is not shown as a tenant database tool. Both panel middleware and the
SQLite administration handlers require the `admin` role.

## Fixed inventory

HTTP and agent RPC requests carry a fixed database ID, never a filesystem path.
The privileged agent owns the mapping from that ID to the server-side file.
Unknown IDs are rejected.

| ID | Database | Default server location | Supported maintenance |
| --- | --- | --- | --- |
| `panel` | CelikPanel state | `/var/lib/celikpanel/celikpanel.db` | inventory and check; snapshot and optimize remain withheld until recent re-authentication exists |
| `powerdns` | PowerDNS authoritative data | `/var/lib/powerdns/pdns.sqlite3` | inventory, check, snapshot download, optimize |
| `roundcube` | Roundcube application state | `/var/lib/celikpanel-webmail/db/roundcube.sqlite3` | inventory, check, snapshot download, optimize |
| `component-catalog` | Signed component operation manifest/catalog (not Store offerings or entitlements) | `/usr/share/celikpanel/manifests/components-v2.db` | inventory, check, snapshot download |

The locations are agent policy and may be represented by a diagnostic hint.
They are not editable request fields. Environment-specific overrides remain a
server deployment concern, not an administration-page input.

## Operations

### Inventory

`GET /api/v1/system-databases` returns the fixed inventory. It may report:

- stable ID, name, purpose, and database kind
- availability and summarized status
- byte size and modification time
- journal mode and SQLite `user_version`
- supported actions

Inventory does not return table rows, credentials, secrets, or SQL output.

### Check

`POST /api/v1/system-databases/{id}/check` asks the agent to open the selected
database through its fixed policy and run `PRAGMA quick_check`. The response is
a summarized integrity result; it does not expose database rows. A failed
check does not repair or modify the database.

### Snapshot download

Snapshot download uses two administrator-only POST requests. First,
`POST /api/v1/system-databases/{id}/snapshot` asks the agent to create a
temporary snapshot. The panel reads the entire snapshot in bounded chunks and
checks its declared size and SHA-256 digest before returning a short-lived
download token as JSON. The live database file is not sent directly.

Second, the browser sends that token in the form body to
`POST /api/v1/system-databases/{id}/snapshot-download`. The token is never put
in the URL. The download endpoint validates the token and first chunk before
committing attachment headers, streams the prepared file, and releases it when
the request finishes.

Mutable databases are snapshotted through SQLite-aware backup handling so an
active WAL database can produce a standalone copy. The immutable component
catalog is copied without running maintenance that would rewrite it.

The CelikPanel control-plane database does not advertise snapshot download in
this release. It can contain password hashes and TOTP secrets, so that action
stays unavailable until the panel has a trustworthy recent-password/TOTP
re-authentication gate.

Snapshot downloads use an opaque, short-lived token, a server-selected safe
filename, `Cache-Control: no-store`, and
`X-Content-Type-Options: nosniff`. Preparation failures remain visible in the
panel. Download HTTP failures are not hidden in an iframe; the browser renders
the error in the same tab, while a successful attachment leaves the panel page
in place. A prepared token abandoned before download is removed by the agent's
short time-to-live cleanup.

### Optimize

`POST /api/v1/system-databases/{id}/optimize` exposes one typed maintenance
operation: `PRAGMA optimize`. The API does not accept a PRAGMA name or SQL
text.

The UI asks for confirmation before starting the action. Optimize is not
available for `component-catalog`, because the catalog is treated as an
immutable, read-only artifact. It is also withheld for the CelikPanel
control-plane database until recent re-authentication is implemented.

## Safety boundary

- Only the four fixed IDs above are accepted.
- Requests cannot provide a path, filename, SQL statement, table name, or
  arbitrary PRAGMA.
- The unprivileged panel process delegates database file access to the agent.
- On Linux, every SQLite open of a service-writable database runs in a narrow
  child process after its UID, GID, and supplementary groups are changed to
  either the verified least-privileged database-file owner or an explicitly
  configured service writer identity. The explicit group-writer identity used
  by Roundcube is allowed only after the configured UID/GID and file mode are
  verified. Platforms without that boundary fail closed for mutable operations.
- Agent errors returned to the browser are summarized and do not expose
  server filesystem paths.
- Snapshot preparation and streaming use bounded chunks. The panel releases a
  temporary artifact after a download attempt or a preparation failure; the
  agent expires prepared artifacts that are never downloaded.
- At most one system snapshot may be creating or active. Snapshots have a hard
  2 GiB ceiling and a 512 MiB free-space reserve. The mutable worker probes the
  staging and destination file descriptors: when they are on the same
  filesystem, free space must be at least `2 × ceiling + reserve`; when they
  are on separate filesystems, each must independently have at least
  `ceiling + reserve`. Growth is checked throughout SQLite backup steps.
- Prepared downloadable snapshot storage is root-private. On first use the
  agent removes only exact managed crash leftovers from that prepared-snapshot
  store and rejects unsafe matching entries.
- Mutable backups use a parent-created nested staging workspace under a
  verified root-owned sticky temporary root. The parent pins the root, outer
  directory, and writer stage by descriptor, device, and inode; while the
  parent remains alive, it reclaims and removes them after success, failure,
  cancellation, or forced worker termination without following symlinks.
- Current limitation: an abrupt termination of the entire parent agent can
  leave a `.celikpanel-sqlite-owner-*` staging directory in the temporary root.
  This release does not automatically reap such entries because a prefix- or
  age-only cleanup could delete a live workspace during a rolling restart. A
  future reaper must use an ownership-checked lease and non-blocking lock before
  applying the same descriptor-pinned, no-follow cleanup rules.
- Per-database maintenance operations are serialized.
- Visiting the page does not start a check, snapshot, optimize, repair,
  migration, service stop, or service restart.
- These tools do not change domain, DNS, mail, firewall, component, add-on, or
  other panel settings.

DNS records must still be changed through the DNS workflow, mail and webmail
state through the mail workflow, and panel settings through their typed panel
handlers. The SQLite page is not a shortcut around those product boundaries.
Store offerings, their component bindings, and subscription entitlements live
in the `panel` database and are managed through **Add-ons → Catalog
management** and the typed Store/entitlement APIs. They are not rows in
`component-catalog`; the System SQLite page deliberately does not edit them.

## Deliberately excluded

The first release has no:

- arbitrary filesystem browsing
- SQL console or query text input
- table, row, or schema editing
- arbitrary PRAGMA input
- `ATTACH`, `DETACH`, or extension loading
- live database download
- upload, restore, replacement, or deletion
- direct PowerDNS or Roundcube row editing
- component catalog editing or optimize action
- tenant-created SQLite database management

## Hardening and product backlog

The following are future work and must not be assumed to be present in the
current release:

- recent-password or TOTP re-authentication before enabling control-plane
  snapshot and optimize actions
- configurable retention and lower per-installation limits beneath the fixed
  safety ceiling and free-space floor
- an append-only agent audit ledger outside `celikpanel.db`
- detached-signature and anti-replay verification status surfaced in the
  System SQLite maintenance view for component catalog artifacts (Manifest V2
  verification exists, but production runtime activation remains pending)
- a maintenance-mode restore workflow with service quiescence, atomic
  replacement, readiness checks, and rollback
- a separate tenant-scoped design for customer-created SQLite databases

Restore, replacement, and deletion require a separate design and are not to be
added to these endpoints as generic file operations.
