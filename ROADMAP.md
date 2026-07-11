# CelikPanel Roadmap

*Last updated: July 11, 2026 · [Türkçe](ROADMAP.tr.md)*

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
- Installation target: **60 seconds** (v0.1 delivered `install.sh`; the target is held).
- One static binary; adding external dependencies is forbidden (this is a feature — we protect it).

### 4. Flexibility
- Everything is an API first; the UI is just one consumer of it.
- Services are modular: customers install what they want, at the version they want.
- Data is never held hostage: backups in standard formats (tar.gz, SQL dump), export always possible.

### The honesty rule
The previous era's mistake will not be repeated: **"it runs" ≠ "it's done."**
Work counts as done only with all three: tests + security review + documentation.
Every version has a measurable exit criterion; the next does not start until it is met.

---

## The Version Ladder

The destination: **v1.0 — a panel a stranger can install on a clean VPS in minutes,
run a real hosting business on, and trust.** Everything below is the road there.

### ✅ v0.0 — The Inheritance *(July 3, 2026)*
~23k lines taken over: solid architecture (Panel + root Agent, SQLite), but insecure
(open TCP agent, SQL injection, no auth) and a mock-data UI. Decision made: continue, don't rewrite.

### ✅ v0.1 — Secure Core + Proven Golden Path *(July 3–10, 2026 — the current release, v0.1.0)*
Eight days, four fronts, all pushed:
- **Security (Phase 0):** agent behind Unix socket + token · session auth (argon2id) + 2FA/TOTP ·
  SQL injection cleanup · CSRF/headers/rate limits · gosec highs closed · leaked credential neutralized.
- **Hosting core (Phases 1–3):** domain types (php/static/node/proxy/forwarding) + subdomains ·
  SSL with real auto-renewal · authoritative DNS (PowerDNS/SQLite sync, DNSSEC, DANE) ·
  the full mail stack (TLS+SNI, authenticated submission 587/465, DKIM signing, server policy,
  deliverability health screen) · databases v2 · one-click WordPress · cPanel importer v1 ·
  accounts (admin/reseller/customer, plans, quotas, impersonation) · entitlements + WireGuard VPN ·
  scheduled backups with retention · audit log · firewall (default-deny) · service uninstall ·
  unattended-upgrades · managed vendor repos (PGDG version choice).
- **Operations (Phase 2):** `install.sh` (one command → login) · `update.sh` with snapshots · `rollback.sh` ·
  systemd units · **golden path proven end-to-end on Ubuntu**: clean install → domain → HTTPS →
  own DNS answering the world → DKIM-signed mail in Gmail's INBOX.
- **Product & design:** Plesk-density UI, light/dark, TR+EN i18n · design system exported to
  claude.ai/design (the design loop: describe → agent drafts with real components → refine → ship) ·
  self-hosted brand fonts · new type scale across all pages · live dashboard with setup journey.
- **The alpha working model (D-008):** the operator drives the panel like a real customer;
  every wall hit becomes a product fix. ~20 real bugs found and shipped this way in two days.

**Exit criterion met:** golden path proven E2E (Ubuntu) · panel self-hosts its updates · alpha model running.

### 🔶 v0.2 — Alpha Complete: Debian Re-Proof *(← WE ARE HERE, in progress)*
The same golden path, re-proven on the production VPS (Debian 13) **entirely through panel clicks**:
- ✅ Panel-only install (zero extra packages) · ✅ PowerDNS installed from the panel ·
  ✅ manage page honest (config visibility, working repair)
- ⏳ Next clicks: auto-repair → first domain → panel Let's Encrypt cert → DS record at registrar →
  web server + site live → mail stack → **Gmail INBOX from Debian**
- Remaining alpha items as they surface + `autodiscover` (mail client autoconfig)
- External unblarker: the registrar-side domain suspension (operator's task)

**Exit criterion:** a visitor can open `https://celikpanel.cloud`, and mail from it lands in Gmail's INBOX —
with every configuring click made in the panel, none on a shell.

### 🩺 v0.2.5 — Debt Payment (the Autopsy prescription)
The Jul 11, 2026 forensic audit ([AUTOPSY](docs/AUTOPSY.md)) surfaced live breakages and structural
debt; verdict: **refactor, not rewrite.** B0 (stop the bleeding: dead TypeID constants, the
broken-for-non-admins Databases page, dead code) closed the same day. The rest, in order:
**B1** one API (v2→v1, tenant scope from auth, OpenAPI + generated client) · **B2** route+authz
table · **B3** the catalog as sole owner of service knowledge · **B4** UI discipline (one
Button/fmtBytes/modal) · **B5** golden-path smoke CI.

**Exit criterion:** AUTOPSY B1–B5 closed. **Hard constraint: v0.3 must not start before B1 is done.**

### v0.3 — Multi-Tenant Reality
Selling to more than one tenant without embarrassment:
- Reseller pooled quotas · `additional_user` role · OS-level disk/traffic enforcement (ROLES.md deferrals)
- cPanel importer proven with a **real** customer archive (incl. DB users)
- WordPress Toolkit depth: updates, hardening, clone/staging
- FTP (vsftpd) wired end-to-end · file manager polish

**Exit criterion:** one reseller + two customers operate self-service for a week; a real cPanel account migrates in one click.

### v0.4 — Operational Trust
What an operator needs at 3 a.m.:
- Monitoring + alerting (service down, disk full, cert failure → mail/webhook) · log viewer in panel
- Remote backup targets (S3/FTP) + restore drills as a product feature
- One-click panel self-update in the UI (update.sh's front end) · WebSocket live notifications

**Exit criterion:** a killed service alerts within a minute; a full restore from remote backup succeeds on a fresh VPS.

### v0.5 — Security Depth
- WAF decision (ModSecurity or honest alternative) · fail2ban deep integration · ClamAV scheduled site scans
- 2FA secret encryption at rest · external security audit · dedicated IP plumbing (sellable `extra_ip`)

**Exit criterion:** an external audit finds no high-severity issue; a customer buys and uses a dedicated IP from the panel.

### v0.6 – v0.9 — Beta Program
- OpenAPI documentation (proof of the API-first promise) · admin & user guides TR+EN
- Real outside beta users; their walls become fixes (the alpha model, scaled)
- Performance targets measured and enforced (<100 ms API) · license/business model decision
  (suggestion: open core) → repo visibility accordingly

**Exit criterion:** ≥3 external operators run real sites for ≥1 month; documentation answers their questions before we do.

### 🎯 v1.0 — Public Release
- Clean VPS → running panel in minutes, documented, self-updating
- "Domain → live site" 100 times back-to-back without failure (the Phase 1 promise, now in CI)
- Migration from cPanel proven repeatedly · pricing/licensing live

### Beyond 1.0 — the horizon *(demand-driven, never speculative)*
Multi-server, BSD agent backend, billing integrations (WHMCS et al.) — each only when real
demand exists, per the non-goals below. Marketplaces never (the AltaVista mistake).

---

## Where We Are — July 11, 2026

**Release:** v0.1.0 alpha, live on the production VPS (Debian 13, panel-only install).
**Position on the ladder:** early v0.2 — the Debian re-proof is running as an alpha play-through:
PowerDNS is installed and managed from the panel; the next click is its auto-repair, then the first domain.
**Scorecard:** feature code ≈ v1.0-scope 70% · proofs ≈ 45% (Ubuntu full, Debian partial) ·
polish/design ≈ 80% (all pages on the new system) · docs ≈ 60% · external validation ≈ 0% (starts at v0.6).
**The system today:** ~29k lines Go (151 files, 72 HTTP endpoints, 38 agent RPC files, 15 migrations,
19-service catalog) + ~16.5k lines TypeScript (54 components, TR+EN).

---

## Deliberate Non-Goals

Simplicity is the ability to say no. These are **intentionally** absent:

- ❌ Docker/container layer — the target market is classic hosting; native is right.
- ❌ A management screen for every conceivable service — what isn't installed is invisible.
- ❌ Theme/appearance marketplaces, portal showcases — the AltaVista mistake.
- ❌ Multi-server / clustering (for now) — no distributed-system dreams before one server is flawless.
- ❌ External dependencies for the panel itself (Redis, external DB, message queue) — one binary + SQLite it stays.
- ❌ BSD support (for now) — **but the option is deliberately preserved, and never as a fork.** The panel↔agent RPC contract is OS-neutral by design: the panel (HTTP/SQLite/UI/business logic) is portable Go that already cross-compiles to FreeBSD; only the agent's "hands" (systemd/apt/nftables) are Linux-specific. If real demand ever appears (e.g. a Linux trust crisis pushing hosts to BSD), the move is a BSD agent backend behind the same RPC surface — weeks of work, one product. Two CelikPanels will never exist. Discipline that keeps this cheap: new agent features keep "what" (RPC surface) separate from "how" (exec calls) — which is how the code is written anyway. *(Decision: July 8, 2026.)*
