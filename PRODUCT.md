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

A web hosting control panel acting for the server owner: the operator installs
it on a Linux server and normally uses a browser to provision and manage domains,
sites, DNS, mail, databases, certificates, backups and system services. Native
services remain the owner's infrastructure; CelikPanel does not replace the
owner's authority to configure or operate them independently (D-022).

It exists as a modern replacement for cPanel and Plesk, which the project's own
README characterises as carrying twenty years of legacy: long installs, forced
dependencies, imposed service versions, and crowded interfaces.

Success means routine setup and administration are understandable through the
panel, with verified results and a supported recovery path when a dependency or
the panel itself fails. This is a product goal, not a claim of complete recovery
coverage. The [resilience contract](docs/RESILIENCE-CONTRACT.md) records the
remaining architecture and acceptance work.

## Positioning

The mechanism a neighbouring product could not truthfully copy without rebuilding:

- **Separate Go panel and agent executables, a React SPA, and SQLite.**
  The panel serves the built SPA from the installed web directory; it is not
  embedded in a single combined executable. The panel's own HTTP and database
  operation does not require a separate web server or external database.
- **A structural privilege split.** The web-facing panel runs unprivileged on
  port 2083. Its privileged requests go to a separate root agent over a local
  authenticated Unix socket, with authorization and resource checks. This limits
  the web process's authority; it is not proof against every exploit and does
  not restrict the owner's native administration of the OS.
- **Modular by install.** Services are installed on demand. Routine management
  shows installed services; discovery and setup can offer supported additions.
  The panel does not impose a fixed hosting stack.
- **Current versions from the OS repositories** rather than vendored older ones,
  with the version choice left to the operator.

## Operating Context

- Runs on managed Linux servers. Platform support follows proven host
  capabilities rather than a distribution allowlist; `apt` and `pacman` adapters
  are active and `dnf` is gated in preview (`docs/DECISIONS.md` D-020,
  `docs/DISTRO-SUPPORT.md`).
- Operators reach the panel over HTTPS on port 2083.
- **Routine product workflows use the panel.** Browser-first operation is the
  default and the ordinary-owner acceptance path. When the interface cannot
  resolve a failure, use the supported owner recovery path with short,
  understandable terminal commands. This does not prohibit native owner
  administration or authorize silent assistant-side repairs.
- **Installed panel updates are user-only.** The user initiates each update in
  CelikPanel's update interface. Publishing a release, preparing recovery or
  permission to continue does not authorize installing it through SSH, scripts,
  APIs or browser automation (`AGENTS.md`).
- **Existing hosts and owner changes are preserved.** Clean-install testing is
  one scenario. Upgrades and recovery must also preserve real existing service
  state and detect later owner edits instead of silently overwriting them.
- Two live servers are in use for verification, in different roles and on
  different distributions.
- The product surface is bilingual (Turkish and English) today. The market target
  is global; Turkish is one product language, not a privileged one.

## Capabilities and Constraints

**Implemented areas** (coverage and hardening vary by adapter): domain and site
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
- **Installed services lead routine management.** Setup and discovery may show
  supported additions with their prerequisites; an unavailable adapter must not
  be presented as an executable promise.
- **Long-running privileged operations are first-class UI states.** Installing a
  service, switching a DNS engine or applying a signed update are multi-minute
  operations that can be interrupted, and the interface has to represent stage,
  progress, terminal success and terminal failure honestly.
- **Recovery coverage is incomplete.** Owner-operated retained-release rollback
  and incident recovery paths exist, but an authenticated recovery surface that
  survives failure of the normal Agent or candidate startup is not yet complete.
  Independent recovery/status and shared UI/CLI contracts are required by D-025,
  not shipped guarantees.
- **Workload independence is required, not fully certified.** Native DNS
  operation has scoped evidence; mail certificate deployment, firewall boot
  restore and other retention boundaries still need work. See the
  [owner-independence audit](docs/OWNER-INDEPENDENCE.md).

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

Derived from the constitution (`ROADMAP.md`) and D-022, D-024 and D-025:

1. **Security by default** — authentication, least privilege and exact authority
   checks remain mandatory, including during recovery.
2. **Continuity and recovery** — reject the unsafe action at its resource
   boundary; retain supported authenticated recovery and independent workloads.
   Each mutation needs observable checkpoints and verified compensation.
3. **Typed evidence** — unknown, unavailable, absent, rejected and failed are
   different states. Preserve the accepted operation and its evidence; a timeout
   cannot invent success, revoke identity or authorize another mutation.
4. **Simplicity** — one obvious routine path, with a clear supported recovery
   route. Fewer user decisions must come from reliable defaults and contracts.
5. **Speed** — responsive interaction and bounded checks; measure setup and
   recovery rather than claim universal uptime or timing guarantees.
6. **Flexibility and owner authority** — API-first management, modular native
   services and portable data. Detect owner changes and preserve them.
7. **Honest completion** — tests, security review, documentation and the relevant
   lifecycle acceptance must agree. The open resilience work is not complete
   merely because one incident correction passes its focused tests.

## Accessibility & Inclusion

**WCAG 2.1 AA is the target** (confirmed 30 Aug 2026). Contrast, keyboard
operability and visible focus are treated as requirements, not preferences.

Two product facts extend this: the interface is used for long working sessions on
dense screens, and it must remain legible in both light and dark across whichever
palettes the skin decision ultimately leaves in place.
