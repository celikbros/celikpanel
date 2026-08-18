<div align="center">

# CelikPanel

**A next-generation web hosting control panel. One binary. Zero dependencies. 60-second install.**

[Türkçe](README.tr.md) · [Roadmap](ROADMAP.md) · [User Roles](docs/ROLES.md)

</div>

---

CelikPanel is a modern alternative to cPanel and Plesk: a single statically-compiled Go binary with an embedded React interface and SQLite storage. It needs nothing else to run — no external database, no web server, no interpreter.

## Why another panel?

cPanel and Plesk are both owned by the same company today, prices climb every year, and the products carry twenty years of legacy: hours-long installs, forced dependencies, outdated defaults, cluttered interfaces.

Our answer is Google's answer to AltaVista: **radical simplicity and speed.** AltaVista tried to become a portal and lost; Google won with one search box. cPanel/Plesk are today's portals. CelikPanel is the search box.

| The old way | The CelikPanel way |
|---|---|
| Hours-long installation | One command, ~60 seconds *(target)* |
| Panel drags MySQL, PHP, Perl along | Single Go binary + SQLite — zero dependencies |
| Old service versions imposed | Always the latest from OS repos; the customer picks the version |
| Everything installed up front | Modular: install only the services you need, from the UI |
| Cluttered portal interface | Fast SPA; services you didn't install are invisible |

## Principles

Every feature, commit and design decision passes four filters — in this order:

1. **Security by default** — least privilege, secure defaults, nothing ships without authentication.
2. **Simplicity** — one obvious way to do each thing. Saying *no* is a feature.
3. **Speed** — API responses under 100 ms, instant UI, 60-second install.
4. **Flexibility** — API-first, modular services, your data is never held hostage.

## Architecture

```
Browser — React SPA
   │  HTTPS
   ▼
Panel — Go HTTP server (port 2083), unprivileged user, SQLite
   │  local RPC (moving to Unix socket + token, Phase 0)
   ▼
Agent — root daemon; the only component allowed to touch the OS
   ▼
Managed services: Nginx · PHP-FPM 8.x · MariaDB · PostgreSQL ·
Postfix · Dovecot · PowerDNS · BIND · Fail2ban · vsftpd · Redis · …
```

The privilege split is deliberate: the web-facing Panel never runs as root. Only the Agent — reachable exclusively from the local machine — holds root, which structurally blocks the classic "web layer to root" panel exploit.

## Status — v0.1.0 alpha

> ⚠️ **Not production ready.** The Phase 0 security sprint (authentication, agent lockdown, injection fixes) is in progress. Do not expose this panel to the internet yet.

**Working today** (functional, being hardened): domain & site management · PHP version selection and FPM pools · SSL (Let's Encrypt + custom certificates) · authoritative DNS (independently selected PowerDNS or BIND on each node, with previewed and recoverable standalone or paired switching) · e-mail accounts and forwarding · database management with multi-server support (MariaDB/PostgreSQL) · file manager · backup/restore · cron jobs · log viewer · service control for 14 services.

### Direct paired authoritative DNS

Paired identity and topology are staged before the first DNS engine is
installed. Either node may then activate BIND or PowerDNS directly; no
PowerDNS-first bootstrap or temporary engine is required. The frozen target
topology is Frankfurt/NS1 as a direct BIND primary and Boston/NS2 as the
directional PowerDNS secondary. Boston's existing empty, panel-managed
standalone PowerDNS is reviewed and reconfigured in place through a snapshotted,
restartable and rollback-safe operation.

The primary remains pair-pending and panel-local zone writes fail closed until
PairReady proves all four authorities: the local catalog AXFR exactly matches
the durable serial and sorted membership; a source-bound AXFR from the peer
catalog returns that same serial and membership; the peer catalog returns the
same authoritative SOA over UDP and TCP; and every member returns its durable
expected SOA serial locally and from the peer. Deletion additionally requires
the deleted zone's peer AXFR to be absent — a successful transfer is a stale
copy and rejects readiness. The secondary is always locally read-only.

Managed BIND options default to `allow-transfer { none; };`; only a paired BIND
secondary admits the exact primary `/32`. PowerDNS transfer/notify peer lists
contain only the exact configured peer. Pair identity is immutable after
activation and requires fixed dedicated peer IPv4 addresses; TSIG is not
implemented, and shared or dynamic NAT endpoints are unsupported. The catalog
serial is engine-neutral and survives a BIND↔PowerDNS primary switch. Only a
membership add, delete or re-add advances it; a record-only update does not, and
maximum-value overflow fails closed. For a released `v0.1.0-alpha.27` source
receipt without that serial, the value may be derived only from matching exact
durable and live backend evidence and is then bound into the new journal and
receipt. These are source-tree contracts, not a release or live-deployment
claim.

**What's next:** see the [Roadmap](ROADMAP.md) — Phase 0 security sprint → Phase 1 golden path hardening → Phase 2 60-second installer → Phase 3 WordPress toolkit + cPanel importer.

## Installing or updating a tagged release

The supported fresh-install input is the prebuilt release archive, not a Git
checkout and not an operator-specific bundle. It contains the matching panel,
agent and web application, so the target server needs no Go, Node or Git.
The current alpha archive targets Linux x86_64/amd64.

Published releases are distributed from the public CelikPanel download channel
at `https://celikpanel.net`. Run the bootstrap as root on a clean supported
server. The same command is also the supported normal update path for an
existing, completed installation. The bootstrap detects clean install versus
update, downloads the selected immutable archive and its checksum over HTTPS,
validates the SHA-256 digest, archive paths and packaged release manifest, and
only then enters the installer or transaction-safe updater.

```bash
# Latest published version
curl --fail --show-error --location --proto '=https' --tlsv1.2 \
  https://celikpanel.net/get.sh -o /tmp/celikpanel-get.sh
sh /tmp/celikpanel-get.sh

# Or pin an exact immutable version
sh /tmp/celikpanel-get.sh --version v0.1.0-alpha.1
```

Use `--install` or `--update` only when explicitly selecting a mode for
diagnostics. Automatic mode refuses partial or ambiguous installations instead
of guessing. A failed first-administrator attempt may be retried with
`--install`; a completed installation is marked only after both services and
the administrator path succeed. Normal updates use the prebuilt binaries and
web application and do not install Go, Node or Git on the server.

The installer interactively creates the first administrator. Do not place the
administrator password in shell history, deployment scripts or release files.
Release archives and machine-readable manifests remain available under
`https://celikpanel.net/releases/`. Each archive also carries an exact internal
`SHA256SUMS` manifest and commit/tree provenance. The HTTPS channel and checksum
detect changed bytes but do not independently prove publisher identity; see the
signing boundary below.

## Building from source

Requirements: exactly Go 1.26.5, Node ≥ 20. The Make gate verifies the exact
compiler, uses a clean `GOTOOLCHAIN=local` environment and never silently
downloads another Go toolchain.

Existing installations whose sealed build cache predates Go 1.26.5 must first
follow the reviewed-checkout proof and one-time migrator sequence in the
[operations runbook](docs/OPERATIONS.md), then use the applicable update mode.
Do not invoke the privileged migrator from an unverified checkout.

The migrator changes only the private build toolchain cache. It does not touch
CelikPanel services, databases, DNS records or panel settings.

```bash
# Backend (panel + agent)
make panel agent

# Frontend
cd web && npm ci --no-audit --no-fund && npm run build   # output: web/dist, served by the panel binary
```

## Release artifacts

`make dist VERSION=<version>` builds the panel, agent, schema bridge and web
application into one deterministic archive. It also writes an external
`SHA256SUMS`-style file next to the archive:

```bash
make dist VERSION=v0.3.0
sha256sum -c dist/celikpanel-v0.3.0.tar.gz.sha256
```

When the release operator has provisioned a protected GPG signing key, create
and verify a detached signature with:

```bash
make dist-sign VERSION=v0.3.0 SIGNING_KEY=<full-key-fingerprint>
gpg --verify dist/celikpanel-v0.3.0.tar.gz.asc dist/celikpanel-v0.3.0.tar.gz
```

The checksum proves byte integrity, not publisher identity. A public or
commercial release must not claim signed provenance until the release owner
has provisioned the CI signing identity and published its verification key.

## Documentation

- [CelikPanel AI Agent](docs/CELIKPANEL-AI-AGENT.md) — panel-only scope, confirmation, audit and subscription-gating plan
- [Component Manifest V2](docs/COMPONENT-MANIFEST-V2.md) — signed SQLite/JSON recipes and platform-adapter boundary
- [Store](docs/STORE.md) — offering catalogue, entitlement boundary and operator workflow
- [System SQLite Administration](docs/SYSTEM-SQLITE-ADMIN.md) — bounded inspection, backup and maintenance of panel-owned databases
- [Web Dependency Security](docs/WEB-DEPENDENCY-SECURITY.md) — lockfile, audit and dependency-update policy
- [Roadmap](ROADMAP.md) — where we are, where we're going, and what we deliberately won't do
- [Decision Ledger](docs/DECISIONS.md) — durable architecture and product decisions
- [Autopsy & Debt Ledger](docs/AUTOPSY.md) — verified failures, remaining smells and remediation status
- [Operations](docs/OPERATIONS.md) — installation, update, rollback and recovery procedures
- [Distribution Support](docs/DISTRO-SUPPORT.md) — generated platform support contract
- [User Roles & Permissions](docs/ROLES.md) — Administrator / Reseller / Customer / Additional User model
- [Frontend Architecture](docs/UI_ARCHITECTURE.md) — one inherited shell, capability-driven per role
- [Conventions](docs/CONVENTIONS.md) — language & naming: English identifiers, bilingual content (TR + EN)
- [Security Policy](SECURITY.md) — private reporting channel and safe research boundaries
- [Technical and Third-Party Notice](NOTICE) — dependency and distribution obligations

## License

CelikPanel is proprietary, source-available software owned by CELIKBROS.
Every use requires CELIKBROS's prior written authorization under a separate
commercial license agreement. Public availability of this repository does not
grant permission to use, copy, modify, distribute, host, or fork CelikPanel.
See [LICENSE](LICENSE).
