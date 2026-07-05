# CelikPanel Roadmap

*Last updated: July 3, 2026 · [Türkçe](ROADMAP.tr.md)*

---

## The Constitution — Every Decision's Filter

Every feature, every commit, every design decision passes these four filters.
Work that fails one is not done, is postponed, or is simplified.

### 1. Security is the default
- No feature ships without authentication.
- The default configuration is always the most secure one (localhost bind, token, least privilege).
- Passwords/tokens come from `crypto/rand` only. SQL is parameterized only.
- Only the Panel may reach the root-privileged Agent — nothing else.

### 2. Simplicity (the Google principle)
While AltaVista tried to become a portal, Google won with a single search box.
cPanel/Plesk are today's AltaVista: crowded, slow, intimidating.
- Every task gets **one obvious way**. If there are two, one gets deleted.
- Before adding a feature, ask: *"What do we lose if we don't add it?"*
  If the answer isn't clear, it isn't added.
- Services that aren't installed are **invisible** in the UI. No empty screens, no disabled menus.
- Smart defaults: do the right thing without asking; an "advanced" section covers the 5% who want knobs.

### 3. Speed
- Panel API response target: < 100 ms. UI interactions: instant.
- Installation target: **60 seconds** (Phase 2 below).
- One static binary; adding external dependencies is forbidden (this is a feature — we protect it).

### 4. Flexibility
- Everything is an API first; the UI is just one consumer of it.
- Services are modular: customers install what they want, at the version they want.
- Data is never held hostage: backups in standard formats (tar.gz, SQL dump), export always possible.

### The honesty rule
The previous era's mistake will not be repeated: **"it runs" ≠ "it's done."**
Work counts as done only with all three: tests + security review + documentation.
Every phase has a measurable exit criterion; the next phase does not start until it is met.

---

## Phase 0 — Security Sprint 🔴 *(we are here)*

> You don't build floors on a rotten foundation. No new feature gets written before this phase ends.

| # | Task | Detail |
|---|------|--------|
| 0.1 | Lock down the Agent | TCP `:1977` → Unix socket (0700 perms) + shared-token authentication |
| 0.2 | Panel authentication | Session-based login, argon2id password hashing, secure cookies (HttpOnly, SameSite) |
| 0.3 | SQL injection cleanup | `postgresql_driver.go` and `database_rpc.go`: parameterized queries + validated identifier quoting |
| 0.4 | Weak randomness | `site_orchestrator.go`: `math/rand` → `crypto/rand` (FTP passwords) |
| 0.5 | Error leakage | Internal error messages never reach the user; full detail to logs, generic message to client |
| 0.6 | HTTP hardening | Security headers, CSRF protection, API rate limiting |
| 0.7 | Known bugs | `files_rpc.go` uid/gid conversion bug; zero `go vet` warnings |
| 0.8 | Leaked password | Rotate the `celikpanel_secure_2025` PostgreSQL password — **deferred** (server not internet-facing yet); mandatory before going public, see Phase 2 exit criteria |

**Exit criteria:** No API endpoint responds without a session · The Agent is unreachable from outside the Panel · `gosec` scan shows no critical findings · `go vet` is clean.

---

## Phase 1 — The Golden Path: Production-Grade Core

> Narrow and deep. Instead of managing 14 services superficially, make one flow flawless:
> **add domain → create site → PHP ready → SSL automatic → site live.**

- End-to-end integration tests for this flow (on clean Ubuntu 24.04)
- Idempotency: running the same operation twice never corrupts the system
- Rollback: any failed step in the flow is undone without leaving traces
- Every operation lands in the audit log (who, when, what)
- Dashboard with real data: CPU, RAM, disk, service states — at a glance, plain
- The empty Settings page either gets content or leaves the menu (simplicity rule)
- UI internationalization (i18n): Turkish + English primary, multilingual-capable — see [Conventions](docs/CONVENTIONS.md)

**Exit criteria:** On a clean server, "domain → live site" runs 100 times back-to-back without failure · Integration test suite green in CI.

---

## Phase 2 — 60 Seconds: The Install Experience (the Google Moment)

> The first impression is our only shot. cPanel takes hours to install;
> ours will finish before the coffee is stirred.

- ✅ `install.sh`: one command → login screen. Self-bootstrapping (downloads Go/Node if absent, builds from source), or uses a prebuilt `make dist` release tarball. *(built July 5, 2026)*
- ✅ systemd units (panel low-priv + agent root), autostart, crash recovery, reboot survival *(built)*
- ✅ First-run: creates the first administrator (`--create-admin`) *(built)*. Remaining: hostname + panel SSL (serving the panel itself over HTTPS).
- Self-update mechanism — later.

**Exit criteria (NOT yet met — needs a real clean-server run):** Clean Ubuntu 24.04 VPS, command to login screen · Everything comes back up by itself after reboot · Golden path (domain→PHP→SSL→DB→live) actually works · All secrets rotated before any public exposure (incl. 0.8).

> **Reality check (July 5, 2026):** the install scaffolding is written and validated offline (bash -n, systemd-analyze, fresh-DB bootstrap), but it has **not** been run on a real fresh machine. Faz 1's golden path is still unverified — on the dev box the agent is non-root, so domain/SSL/DB creation fails. The next real milestone is: wipe a machine → `install.sh` → create a live site end to end.

---

## Phase 3 — The Winning Features

> The difference between "a nice alternative" and "the panel people actually leave cPanel for."
> *(Reordered July 5, 2026: runtimes come first — app installers like WordPress build on top of a solid runtime foundation, not the other way around.)*

### 3A. Runtimes done right — PHP depth + Node.js projects
Classic hosting is PHP, but the market increasingly ships Node apps too. A site is not just "a PHP vhost": it has a **project type**.
- Project types on a site: `php` (FPM, per-site version), `static`, `node` (long-running app behind nginx reverse proxy), `proxy`
- Node.js runtime management: install official versions side by side, pick per project
- Node project = directory + start command + port; supervised (systemd unit), start/stop/logs in the panel
- PHP depth: per-site version switching and pool settings stay first-class (already live), composer presence honest-reported

### 3B. cPanel Importer v1
Our target customers are on cPanel today. If they can't migrate with one click, they won't come at any price.
- Import from cPanel account archives (cpmove/backup): site files + MySQL + mail accounts + DNS records

### 3C. Taking mail seriously — the start
Mail is the hardest part of the panel business and the biggest churn driver.
- ✅ Automatic generation and validation of DKIM/SPF/DMARC records *(done July 5, 2026)*
- ✅ Quota management with live usage and honest enforcement status *(done July 5, 2026)*
- Remaining: DKIM signing filter integration (opendkim/rspamd) — needs package install

### 3D. WordPress Toolkit v1 *(deliberately last in this phase)*
~40% of the market is WordPress; it lands on top of 3A's foundation (one-click install = create PHP site + DB + fetch WP, which 3A makes trivial).
- One-click WP install (latest version, correct permissions, database ready)
- Update management, basic hardening (file permissions, xmlrpc, login protection)

**Exit criteria:** A Node app deploys from the panel and survives a reboot · A real cPanel account restores from archive and works, DNS included · A freshly created mailbox reaches Gmail without landing in spam · One-click WP install in ≤ 30 seconds.

---

## Phase 4 — Expansion *(only after Phase 3)*

- Reseller panel and quota/limit enforcement
- API documentation (OpenAPI) — proof of the API-first promise
- License/business model decision (suggestion: open core) and repo visibility accordingly
- Monitoring/alerts, WebSocket notifications
- Multi-server only if real customer demand emerges

---

## Deliberate Non-Goals

Simplicity is the ability to say no. These are **intentionally** absent:

- ❌ Docker/container layer — the target market is classic hosting; native is right.
- ❌ A management screen for every conceivable service — what isn't installed is invisible.
- ❌ Theme/appearance marketplaces, portal showcases — the AltaVista mistake.
- ❌ Multi-server / clustering (for now) — no distributed-system dreams before one server is flawless.
- ❌ External dependencies for the panel itself (Redis, external DB, message queue) — one binary + SQLite it stays.
