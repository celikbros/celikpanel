# Strategic Decisions

*Project record · [Türkçe](DECISIONS.tr.md)*

Durable record of the **why** behind big directional choices — the reasoning
we do not want to re-derive from scratch each time the question resurfaces.
Code decisions live in git; this file is for strategy. Newest first.

---

## D-009 · No DNS server, no domains: the panel is authoritative for its domains

*July 9, 2026*

**Decision.** A domain can only be added when a DNS server (PowerDNS/BIND) is
installed, and the DNS server cannot be uninstalled while domains exist. Every
domain on a CelikPanel server is served by that server's own DNS — there is no
"or manage DNS at your external provider" branch.

**Why.** The operator's verdict after living with the either/or messaging:
"dns yoksa domain de olmamalı — kafa karıştırıyor." A record list that nothing
serves is a trap dressed as a feature; two valid-but-different mental models in
one dialog is one too many. One rule reads instantly: install DNS first, then
domains. This consciously supersedes the earlier "DNS can be external" stance
in D-008's first-day notes — external DNS can return later as an explicit
advanced mode if a real user needs it; the alpha optimises for coherence.

**How it holds up.** Preflight blocks creation of every domain type with an
actionable 409 (and the dialog disables all choices with one clear banner);
the mirror guard refuses uninstalling the dns-server group member while any
domain exists — otherwise every domain would silently go dark, the exact trap
the rule prevents. All "or manage externally" copy removed. Proven live:
creation without DNS → 409.

---

## D-008 · Alpha: the operator drives the panel; every gap becomes a product feature

*July 9, 2026*

**Decision.** From the Debian 13 reinstall onward, the operator uses CelikPanel
exactly like a real customer: every install, every setting, every domain goes
through the panel, by their hand. The developer never configures the server —
not even with permission. When the operator hits a wall, the wall is the
product's fault: the missing capability is built into the panel (or the broken
one fixed) and shipped as a product update. Diagnosis over SSH stays read-only.

**Why.** Hand-fixing a server over SSH makes the product look more finished
than it is — the gap disappears from view instead of from the product. This is
the final escalation of a line that started with D-005 (install nothing) and
sharpened through "never install unasked": now the developer installs and
configures *nothing at all*. Alpha quality comes from walking into the walls.

**How it held up on day one.** One afternoon of real operator use surfaced and
fixed, in order: a domain wrongly requiring PHP (creation predated the type
system) → role-aware Add Domain with php/static/**DNS-only** types (Plesk's
"no web hosting" equivalent) and live prerequisite checks; ghost settings
pages for services that are not installed → every tab, engine list and version
dropdown now reads the server's real capability set (one endpoint,
`/api/v1/hosting/capabilities`); dead phpMyAdmin/pgAdmin links → a general
`Requires` catalogue concept (the inverse of conflict groups): dependent tools
are locked until their parent service exists, and once installed are served
loopback-only behind an admin-gated panel proxy; no way to get the panel a
real certificate → one-click Let's Encrypt in Settings with automatic renewal;
and the quietest, biggest one — the panel served `index.html` with no cache
headers, so browsers kept showing the OLD interface after every update. The
operator saw ghosts that had already been fixed; now the entry point is
`no-cache` and fingerprinted assets are immutable.

---

## D-007 · Version choice via managed vendor repositories, not a fork of the distro

*July 9, 2026*

**Decision.** A service may declare an official upstream repository (PostgreSQL
→ PGDG). Enabling it is a first-class panel action that unlocks a version pick
at install time: instead of the single major the distro froze, the admin
chooses from every current major the vendor ships. It is opt-in, curated,
signed and reversible — never on by default.

**Why.** A distro release pins one major of each database/runtime (Ubuntu noble
ships only PostgreSQL 16; Debian bookworm only 15). Customers need to land on a
specific version and stay there — this was the operator's open worry ("PG has
many versions; which one does install pick, and does it lock us to one?"). The
two bad answers are *fork the distro* (own the whole package tree forever) or
*let the OS decide* (no choice at all). The industry-standard middle path is the
vendor's own signed apt repo, which carries all current majors side by side.

**Why it stays consistent with minimal-install (D-005/D-006).** A third-party
repo widens the trust boundary, so it gets the same discipline as installing a
service: only catalog-declared repos can be enabled (the UI never passes a URL);
the signing key is pinned per-repo with apt's `signed-by=` (no global `apt-key`
trust); and disabling removes the source + key cleanly. The armoured key is used
directly, so **no `gpg` is pulled in** — the minimal footprint holds. A version
pick is bounded by the repo's package pattern, so it can never become an
arbitrary package install.

**How it holds up.**
- Catalog: `ManagedRepo` (id, name, key URL, source template with `{codename}`,
  package pattern); `postgresql` carries PGDG. Agent `EnableRepo`/`DisableRepo`/
  `RepoStatus`/`RepoPackages`; the repo — not our code — is the source of truth
  for which versions exist (discovered via `apt-cache`, newest major first).
- Panel `GET/POST /api/v1/repo` (admin-only, audited); the allowlist lives here
  (resolve repo from catalog by service id). Install accepts a version package
  only when it matches that service's repo pattern.
- Proven on the production VPS: baseline offered **only `postgresql-16`**;
  enabling PGDG unlocked **9 majors (10–18)**, and picking `postgresql-17`
  resolved to `17.10-1.pgdg24.04+1` from PGDG; disabling returned to baseline.
- Bug caught in testing: the agent runs with `UMask=0027`, so a `0644` keyring
  landed as `0640 root:celikpanel` — unreadable by apt's unprivileged `_apt`
  verifier, failing as "not signed". Fixed by `chmod 0644` after write.

---

## D-006 · Attack-surface management: every install is reversible

*July 8, 2026*

**Decision.** Whatever the panel can install, it can uninstall. Service
removal (stop + disable + `apt purge --auto-remove`) is a first-class action
next to install, not a manual SSH chore. This is the first of a three-part
"attack-surface" line: **reversible installs**, automatic security patching,
and a firewall that only opens ports in use.

**Why.** The operator's principle, stated plainly: *every installed service or
package is a security risk — a CVE waiting, a port open.* Minimalism is not
tidiness, it is a smaller attack surface. But minimalism is only real if the
surface can shrink as well as grow: if you can add nginx but never remove it,
a mistaken or outgrown install is permanent risk. So install has a mirror.

**How it holds up.**
- `Agent.UninstallService`: stops + disables every unit, then `removePackages`
  purges (config gone, `--auto-remove` drops orphaned deps). Mirror of
  `InstallService`; same whitelist, same honest "not on this distro yet".
- Panel `POST /service/uninstall` (admin-only, audited); UI shows a
  destructive-confirm dialog listing the exact packages and warning that
  dependent sites/mail will stop.
- Proven on the production VPS: SpamAssassin installed (dpkg present) then
  uninstalled (dpkg absent, unit gone) — the surface measurably shrank.

**This line's progress (complete):** reversible installs ✅ · auto security
updates ✅ · firewall ✅. The firewall is default-deny inbound; the panel
opens exactly the panel port + each installed service's declared ports, and
the agent always keeps SSH (auto-detected from live listeners) + loopback +
established open, so a rule can never lock the operator out. Install/uninstall
re-syncs it, so the open-port set always equals the running-service set.
Proven on the VPS: enabling it left SSH, panel, web and DNS reachable while
the policy dropped everything else.

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

*Amendment (Jul 16, operator decision):* Arch is now a **dev-test target** —
we keep a second test server on Arch precisely to keep the portability promise
honest. `install.sh` supports pacman (prerequisites, toolchain arch via
`uname -m`, honest "no security-only channel" note).

*Amendment 2 (Jul 16, widened the same day):* Testing actively on both servers,
the operator hit the "apt-only catalog" wall three times (certbot install, bind
removal, a confirm dialog showing the package name `bind9`). Decision: **the
agent's package layer now drives pacman too** (install `-S --needed`, remove
`-Rns`, presence `-Q`; never `-Syu` — no surprise system upgrade). Catalog
pacman package names are filled only where the mapping is certain AND no
distro-specific init step is needed; MariaDB/PostgreSQL (need initdb),
phpPgAdmin (AUR-only) and Redis (Valkey fork on Arch) stay deliberately empty —
they keep saying an honest "not supported yet". The UI shows package names for
the family the agent reports (`Agent.PkgFamily`).

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
