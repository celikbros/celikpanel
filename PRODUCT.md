# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Four roles share one application shell; the UI is rendered from the signed-in
user's role and capabilities rather than from per-role layouts
(`web/src/nav.ts`, `docs/UI_ARCHITECTURE.md`, `docs/ROLES.md`).

- **Administrator** — owns the server. Installs and configures services, manages
  DNS engines, certificates, mail, databases, firewall, and every tenant on the
  box. Technically fluent; works in dense, high-control screens.
- **Reseller** — operates a hosting business on the server. Manages their own
  customers, plans and resource pool; does not touch host services.
- **Customer** — manages their own domains, databases, mail, files and backups.
  Not necessarily technical.
- **Additional user** — a customer's delegate, holding a subset of that
  customer's capabilities.

**Confirmed design weighting (30 Aug 2026):** no role leads. All four are first-class.
The shell, density and default decisions must work for every role rather than
being tuned for one and degraded for the others.

## Product Purpose

A web hosting control panel: the operator installs it on a clean Linux server
and, from a browser, provisions and runs domains, sites, DNS, mail, databases,
certificates, backups and the system services beneath them.

It exists as a modern replacement for cPanel and Plesk, which the project's own
README characterises as carrying twenty years of legacy: long installs, forced
dependencies, imposed service versions, and crowded interfaces.

Success is that an operator can go from a bare server to a working, secured
hosting environment through the panel alone, quickly, and can keep it running
without specialist knowledge.

## Positioning

The mechanism a neighbouring product could not truthfully copy without rebuilding:

- **One statically-compiled Go binary plus an embedded React SPA and SQLite.**
  No external database, no separate web server, no interpreter required to run
  the panel itself.
- **A structural privilege split.** The web-facing panel runs unprivileged on
  port 2083 and holds no root. A separate root agent, reachable only over a local
  authenticated Unix socket, is the only component permitted to touch the OS.
  This is intended to structurally block the classic "web layer to root" panel
  exploit rather than defend against it by policy.
- **Modular by install.** Services are installed on demand from the UI; a service
  that is not installed is not shown. The panel does not drag a fixed stack along.
- **Current versions from the OS repositories** rather than vendored older ones,
  with the version choice left to the operator.

## Operating Context

- Runs on managed Linux servers. Platform support follows proven host
  capabilities rather than a distribution allowlist; `apt` and `pacman` adapters
  are active and `dnf` is gated in preview (`docs/DECISIONS.md` D-020,
  `docs/DISTRO-SUPPORT.md`).
- Operators reach the panel over HTTPS on port 2083.
- **Install, uninstall and configuration always go through the panel API.** SSH
  is used only for read-only diagnosis. This is a standing operating rule, not a
  preference: it is what guarantees that an install the panel reports as
  successful is actually functional (`docs/AUTOPSY.md` A10, A14).
- **Servers start clean.** Manual intervention on a host is performed only by the
  operator and is documented afterwards, so that the panel's behaviour on a fresh
  machine stays the thing being tested.
- Two live servers are in use for verification, in different roles and on
  different distributions.
- The product surface is bilingual (Turkish and English) today. The market target
  is global; Turkish is one product language, not a privileged one.

## Capabilities and Constraints

**Working today** (functional; hardening in progress): domain and site
management · PHP version selection and FPM pools · SSL via Let's Encrypt and
custom certificates · DNS · e-mail accounts and forwarding · databases with
multi-server support (MariaDB, PostgreSQL) · file manager · backup and restore ·
cron jobs · log viewer · firewall · VPN · service control for the managed
service catalogue · signed self-update.

**Constraints that shape design:**

- **Not production-ready by the project's own declaration.** The repository says
  so explicitly, and the current handover state carries open blockers.
- **Exclusive service slots.** Some services compete for a single listening port
  and only one may hold it: DNS (BIND / PowerDNS on :53), SMTP (Postfix / Exim on
  :25), web (nginx / Apache on :80,:443). Database engines are not exclusive and
  coexist. The UI must be able to express "this cannot be installed because a
  competing engine holds the slot".
- **A service that is not installed is invisible** in the UI. No empty screens,
  no disabled menus for absent services.
- **Long-running privileged operations are first-class UI states.** Installing a
  service, switching a DNS engine or applying a signed update are multi-minute
  operations that can be interrupted, and the interface has to represent stage,
  progress, terminal success and terminal failure honestly.
- **The panel is the only interface.** There is no companion CLI or rescue UI for
  operators, so any state the product can enter must be explainable and
  recoverable from the browser.

## Brand Commitments

- **Name:** CelikPanel.
- **Bilingual product surface:** Turkish and English, maintained in parallel.
- **Global market:** decisions are weighed for a worldwide audience.

**Open decision — competitor skins.** The product currently ships four selectable
skins (`celik`, `plesk`, `aapanel`, `cpanel`), the latter three imitating
competitors' colour schemes, composed with a light/dark axis. Whether these are a
durable product commitment is **undecided as of 30 Aug 2026**. Until it is
decided, they are out of scope for the design system: DESIGN.md documents the
project's own palette only. Do not treat the competitor skins as binding
identity, and do not remove them either.

## Evidence on Hand

- **Real:** two live verification servers; a signed release chain with a pinned
  public key and reproducible manifests; an unusually detailed engineering record
  in `docs/AUTOPSY.md` (numbered breakages with file:line evidence),
  `docs/DECISIONS.md` (strategic decision log) and `ROADMAP.md`.
- **Absent — must not be fabricated:** there are no customers, testimonials, case
  studies, press coverage, benchmark results, uptime figures, pricing, or
  third-party security audits. The "60-second install" figure in the README is
  labelled a target in the README itself and must not be presented as a measured
  result.

## Product Principles

Derived from the project's own constitution (`ROADMAP.md`), which states these
are applied in order:

1. **Security by default** — least privilege, secure defaults, nothing ships
   without authentication.
2. **Simplicity** — one obvious way to do each thing; saying no is a feature.
   Applies to the user's path, not to the set of supported backends.
3. **Speed** — fast API responses, an instant-feeling UI, a fast install.
4. **Flexibility** — API-first, modular services, the operator's data is never
   held hostage.
5. **Everything through the panel** — if the product can do it, the panel can do
   it; an operation that requires SSH is an unfinished feature.

## Accessibility & Inclusion

**WCAG 2.1 AA is the target** (confirmed 30 Aug 2026). Contrast, keyboard
operability and visible focus are treated as requirements, not preferences.

Two product facts extend this: the interface is used for long working sessions on
dense screens, and it must remain legible in both light and dark across whichever
palettes the skin decision ultimately leaves in place.
