# Strategic Decisions

*Project record · [Türkçe](DECISIONS.tr.md)*

Durable record of the **why** behind big directional choices — the reasoning
we do not want to re-derive from scratch each time the question resurfaces.
Code decisions live in git; this file is for strategy. Newest first.

---

## D-005 · Install installs nothing but the panel; a server can be panel-only

*July 8, 2026*

**Decision.** `install.sh` installs only the panel + agent (self-contained Go
binaries + SQLite) and four tiny fetch tools (tar, xz, curl, ca-certificates)
— **nothing** for hosting. nginx, PHP, MariaDB, Postfix, PowerDNS, mail: every
one is added later from the panel, on demand. A server may stay panel-only
(e.g. someone who wants only the built-in VPN) and be managed over its IP with
no domain at all.

**Why.** The constitution: "what isn't installed is invisible." A freshly
installed server carries zero attack surface and zero cruft beyond the panel.
The operator composes exactly the server they want — a full hosting box, or
just a VPN endpoint, or just DNS. Nothing unwanted is ever present.

**How it holds up.**
- Verified: `install.sh` runs only `apt-get install tar xz-utils curl
  ca-certificates`. On the production VPS, nginx/PHP/etc. exist only because
  they were installed *from the panel* to prove the golden path — a bare
  install has none.
- Domain-less access works by design: the panel's self-signed certificate puts
  the machine's IPs in its SAN (`tmpl.IPAddresses = hostIPs()`), so IP access
  presents a matching cert (self-signed warning only, never a name mismatch).
  A domain + Let's Encrypt is an upgrade, never a requirement.

---

## D-004 · Operating system: Ubuntu LTS first, BSD option preserved (never a fork)

*July 8, 2026*

**Decision.** Ubuntu 24.04 LTS is the first-class, only-tested target. BSD is
a deliberate non-goal **for now** — but the option to support it later is
preserved by architecture, and it will **never** be a separate product.

**Why Ubuntu, not BSD.**
- The whole product sits on Linux-specific pieces: systemd (the agent's core —
  services, `celikapp-*`, `wg-quick@`), apt + package whitelists, nftables
  (VPN NAT), and the Ubuntu layout of postfix/dovecot/opendkim. Switching OS
  = rewriting the agent's "hands," months of work for zero user-visible gain.
- The stability the user admired ("versions locked, stable") **is Ubuntu LTS
  itself**: 5 years of security-only patches. BSD would not add stability we
  don't already have.
- Market reality: no competitor is on BSD (cPanel = RHEL family, Plesk =
  Linux+Windows, aaPanel = Linux). VPS images, customer habits, the
  WordPress/PHP ecosystem, Let's Encrypt tooling are all Linux-first. The
  customer asks "does my WordPress work, does my mail avoid spam?" — not
  "are you on BSD?"

**Why the option is still cheap to keep — and why NOT a fork.**
- The panel↔agent RPC split (built for security) is also OS-independence. The
  **panel** (HTTP, SQLite, UI, business logic) is portable Go; the **agent**
  is the only OS-touching layer. The panel says *what* ("create site"); only
  the agent knows *how* (systemctl/apt/nftables).
- Proven, not claimed: as of this date `GOOS=freebsd go build` compiles **both**
  panel and agent. One portability fix was needed (Statfs_t field types differ
  Linux vs BSD — explicit `uint64` casts in `cmd/panel/system_stats.go`) and is
  done. The whole codebase cross-compiles to FreeBSD today.
- Two CelikPanels would be the exact bloat the user feared: every feature
  twice, every bug twice, two mediocre products. Instead, a hypothetical BSD
  future = a **BSD agent backend behind the same RPC surface** (rc.d, pkg, pf).
  Estimate: weeks, not years — the whole stack (nginx, postfix, dovecot,
  opendkim, PowerDNS, MariaDB, WireGuard) exists in FreeBSD ports; only the
  "hands" change. Panel, UI, DB, business logic: zero change.

**What we do today.** Almost nothing — and that's the point. The only cost is
discipline: new agent features keep *what* (RPC surface) separate from *how*
(exec calls), which is how the code is written anyway. No speculative
abstraction layer, no "might need it" code. The decision is made when real
demand appears (e.g. a Linux trust/security crisis pushing hosts to BSD).

**Other distros.** `detectPkgFamily` recognises apt / dnf / pacman. Today only
apt (Ubuntu/Debian) is fully supported and tested; dnf/pacman are recognised
but installs honestly say "not automatable on this distro yet" rather than
guess. Note: dnf module streams (`postgresql:16` vs `:18`) would make a real
version picker meaningful there — the UI is ready for it if we add dnf support.

---

## D-003 · Service catalogue: show everything, block by conflict — not by "unmanaged"

*July 8, 2026*

**Decision.** The Services page (admin-only) lists the whole catalogue,
installed or not, with a one-click Install. We do **not** hide services we
don't deeply manage, and we do **not** clutter them with "unmanaged" labels.
The only thing that blocks an install is a **real conflict**.

**Why.** The user's insight: gating on "do we manage it?" is needless
complexity. The real constraint is whether two services can coexist.
- **Conflict groups** (`ConflictGroup` in the catalogue): services that hold
  the same role/port are mutually exclusive — a web server on :80 (Nginx ↔
  Apache), a DNS server on :53 (BIND ↔ PowerDNS). If one is installed, the
  other's Install button becomes "Conflicts with X".
- **No group = coexists**: MariaDB + PostgreSQL run side by side (different
  ports), so both are installable. Redis + Memcached likewise.

**Consequences.** Every install is the admin's explicit, single-click consent
(honouring the "install only with permission" principle). The catalogue is
grouped by category with an accordion (expand/collapse all) so it stays
scannable as it grows toward dozens of services.

---

## D-002 · Service versions: honest single version, real multi-version only where it exists

*July 8, 2026*

**Decision.** The install modal shows the **real** version apt would install
(read live via `apt-cache policy`), not a version *picker*. A picker is only
offered where multiple versions genuinely exist.

**Why.** A Debian/Ubuntu release ships exactly one version of each package and
freezes it (security patches only) — that is the stability model, by design.
A dropdown with one option is a lie that misleads the user. So:
- **Single-version services** (PostgreSQL, MariaDB, nginx…): show what will
  land ("Version to install: 16"), no picker.
- **Genuinely multi-version, hosting-critical** → solved outside the distro,
  with a real picker:
  - **PHP** — side-by-side packages (`php8.1-fpm`…`php8.4-fpm`), chosen
    per-site under runtimes. ✅ built.
  - **Node.js** — official tarballs independent of the distro, chosen
    per-project. ✅ built.
- **Future option**: a customer wanting e.g. PostgreSQL 18 on Ubuntu 24.04
  would mean adding the vendor's own apt repo (PGDG) as a deliberate feature —
  real repo management, never a fake picker. In backlog, not built.

Pattern: **the distro's single version = safe baseline; real multi-version
(PHP, Node) via our own mechanism, independent of the distro.** Same as
cPanel/Plesk (they compile PHP themselves, leave the rest to the distro).

---

## D-001 · Update & rollback: never re-image the server

*July 8, 2026*

**Decision.** Production updates are `sudo ./update.sh` (git pull + idempotent
`install.sh`), never a wipe-and-reinstall. Every update first takes a rollback
snapshot; `sudo ./rollback.sh` returns to the previous working state.

**Why.** Customers on the box mean data must survive every update. Customer
data (SQLite DB, site files, mail, DNS, certs, DKIM keys) lives outside the
replaced paths (`bin/`, `web/`); migrations apply on panel start. The snapshot
(panel DB + binaries + unit files + source commit) makes "an update made it
worse" a recoverable event, not a disaster. Fix flow: reproduce on the dev
box (the VPS's mirror) → prove → push → `update.sh` on the VPS → `rollback.sh`
if needed. See [update.sh](../update.sh), [rollback.sh](../rollback.sh).
