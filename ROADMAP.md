# CelikPanel Roadmap

*Last updated: July 18, 2026 · [Türkçe](ROADMAP.tr.md)*

---

## The Constitution — Every Decision's Filter

Every feature, every commit, every design decision passes these four filters.
Work that fails one is not done, is postponed, or is simplified.

### 1. Security is the default
- No feature ships without authentication.
- The default configuration is always the most secure one (localhost bind, token, least privilege).
- Passwords/tokens come from `crypto/rand` only. SQL is parameterized only.
- Only the Panel can reach the root-privileged Agent — nothing else.

### 2. Simplicity (the Google principle)
While AltaVista tried to be a portal, Google won with a single search box.
cPanel/Plesk are today's AltaVista: crowded, slow, intimidating.
- Every job has **one obvious way**. If there are two, one gets deleted.
- Before adding a feature, ask: *"What do we lose by not adding it?"* If the answer isn't clear, it isn't added.
- A service that isn't installed is **invisible** in the UI. No empty screens, no disabled menus.
- Smart defaults: do the right thing without asking; an "advanced" section for the 5% who want knobs.

### 3. Speed
- Panel API response target: < 100 ms. UI interactions: instant.
- Install target: **60 seconds** (v0.1's `install.sh` delivered it; the target stands).
- One static binary; adding external dependencies is forbidden (this is a feature — we protect it).

### 4. Flexibility
- Everything is API-first; the UI is just one of its consumers.
- Services are modular: the customer installs what they want, at the version they want.
- Data is never held hostage: backups in standard formats (tar.gz, SQL dump), export always possible.

### The honesty rule
The previous era's mistake will not repeat: **"works" ≠ "done".**
Work is finished only with all three: tests + security review + documentation.
Every release has a measurable exit criterion; the next one doesn't start until it's met.

---

## The Version Ladder

Destination: **v1.0 — a panel a stranger can install on a clean VPS in minutes,
run a real hosting business on, and trust.** Everything below is a stone on that road.

The July 17 update worked three new requirements into the ladder — not a vague "later", but step by step:
1. **In-panel help/tips:** foundation stones in v0.2.5 (as natural extensions of the existing debt items),
   user-facing content in v0.3, help center + wizard + palette in v0.6–0.9.
2. **Reseller + customer as first-class experiences:** the door-opening slice in v0.2.5/B1, the body in
   v0.3 (including collection powers), `additional_user` as a real feature in v0.35.
3. **Free-tier subscription/plan system:** data model + period/cancel/suspend machine + payments ledger +
   reseller pools in v0.3, "every promise enforced or deleted" honesty in v0.35, the payment-provider
   decision (refunds/chargebacks included) in v0.5, self-signup + abuse brakes in v0.6–0.9.

### ✅ v0.0 — Takeover *(July 3, 2026)*
~23k lines inherited: architecture sound (Panel + root Agent, SQLite) but insecure
(open TCP agent, SQL injection, no authentication) and a UI full of fake data.
Decision made: continue, no rewrite.

### ✅ v0.1 — Secure Core + Proven Golden Path *(July 3–10, 2026 — current release, v0.1.0)*
Eight days, four fronts, all pushed:
- **Security (Phase 0):** agent behind Unix socket + token · session identity (argon2id) + 2FA/TOTP ·
  SQL injection cleanup · CSRF/headers/rate limit · gosec highs closed · leaked passwords neutralized.
- **Hosting core (Phases 1–3):** domain types (php/static/node/proxy/redirect) + subdomains ·
  real auto-renewing SSL · authoritative DNS (PowerDNS/SQLite sync, DNSSEC, DANE) ·
  full mail stack (TLS+SNI, authenticated submission 587/465, DKIM signing, server policy,
  deliverability health screen) · databases v2 · one-click WordPress · cPanel importer v1 ·
  accounts (admin/reseller/customer, plans, quotas, impersonation) · entitlements + WireGuard VPN ·
  scheduled backups with retention · audit log · firewall (default-deny) · service uninstall ·
  unattended security patches · managed vendor repos (PGDG version choice).
- **Operations (Phase 2):** `install.sh` (one command → login screen) · snapshotting `update.sh` · `rollback.sh` ·
  systemd units · **golden path proven end-to-end on Ubuntu**: clean install → domain → HTTPS →
  own DNS answering the world → DKIM-signed mail in Gmail's INBOX.
- **Product & design:** Plesk-density UI, light/dark, TR+EN · design system on claude.ai/design
  (design loop: describe → agent draws with real components → filter → ship) · self-hosted brand fonts ·
  the new scale on every page · live dashboard with a setup journey.
- **Alpha operating model (D-008):** the operator drives the panel like a real customer; every wall they
  hit becomes a product fix. ~20 real bugs found and shipped this way in two days.

**Exit criterion met:** golden path proven end-to-end (Ubuntu) · the panel carries its own updates · the alpha model works.

### 🔶 v0.2 — Alpha Complete: The Debian Re-Proof *(← WE ARE HERE, in progress)*
The same golden path, re-proven on the production VPS (Debian 13) **entirely with panel clicks**:
- ✅ Panel-only install (zero extra packages) · ✅ PowerDNS installed from the panel ·
  ✅ honest management page (config visibility, working repair)
- ⏳ Next clicks: auto-repair → first domain → panel Let's Encrypt certificate →
  DS record at the registrar → web server + live site → mail stack → **Gmail INBOX from Debian**
- Remaining alpha rough edges as they surface + `autodiscover` (mail client auto-config)
- External blocker: the domain suspension at the registrar (operator's task)

**Exit criterion:** a visitor can open `https://celikpanel.cloud` and mail sent from it lands in
Gmail's INBOX — every configuring click in the panel, none in a shell.

### 🩺 v0.2.5 — Debt Payment (the Autopsy Prescription) + Foundation Stones
The July 11, 2026 forensic audit ([AUTOPSY](docs/AUTOPSY.md)) surfaced live breaks and structural debt;
decision: **refactor, not rewrite.** B0 (stop the bleeding: dead TypeID constants, broken Databases page
for non-admins, dead code) closed the same day. The rest in order: **B1** one API (v2→v1, tenant scope
from auth, OpenAPI + generated client) · **B2** route+authz table · **B3** the catalog as sole owner of
service knowledge · **B4** UI discipline (one Button/fmtBytes/modal) · **B5** golden-path smoke CI.

The foundation stones of the three new requirements are laid on this step **not as separate work but as
natural extensions of B1–B5** (added later, they would all break a second time):
- **B1 addendum — the error contract:** all error bodies move to `{code, message, hint?, action?}`.
  `code` is a machine-readable constant (e.g. `DNS_SERVER_REQUIRED`), `hint` resolves via i18n keys in
  the frontend, `action` can be an in-panel link ("Install PowerDNS" → /services). Every deliberate
  refusal is coded: the D-009 409, quota 409s, conflict groups, the entitlement 402. One `ErrorBanner`
  component in the frontend; the error body is defined in the OpenAPI schema — the generated client is
  born right once.
- **B1 addendum — opening the way for Databases self-service:** server registration stays admin;
  DB/user CRUD endpoints open to customer+reseller under tenant scope; the temporary admin lock in
  `nav.ts` lifts; the phpMyAdmin proxy verifies ownership. (The real precondition of v0.3 — this is why
  the hard constraint exists.)
- **B2 addendum — fail-closed roles:** a request whose user record can't be read never proceeds with an
  empty role (today `middleware.go` continues with Role=''). On top of the route+authz table, a
  **role×endpoint matrix test**: with the `--demo` seed accounts, every (endpoint × admin/reseller/
  customer/anonymous) cell is verified against the expected 200/403/404; an endpoint not in the table
  fails the test.
- **B3 addendum — Setup Journey honesty:** journey steps read real service state from the catalog
  (installed + enabled + running), not package presence. Field proof already exists: the dormant bind in
  Hostinger's Arch image counted as "DNS installed: Done" (July 16). That scenario enters B5 smoke as a
  regression.
- **B4 addendum — the help layer's atom:** one Tooltip/InfoTip component in `ui.tsx` (HelpCircle +
  i18n'd explanation, keyboard accessible); the existing 6 Info callouts and ≥10 critical "what is
  this?" fields (DNSSEC DS, DKIM, catch-all, SNI…) migrate to it. The one obvious way to add a new tip
  is this component.
- **B4 addendum — i18n discipline:** a lint that catches bare strings in JSX (the "Coming soon..." in
  App.tsx goes; the vsftpd placeholder either becomes an honest i18n'd EmptyState or drops from nav) ·
  an en.ts/tr.ts key-parity check (`tools/check-i18n`) in CI — a missing key can't silently fall back
  to English.
- **Version singularity + CHANGELOG:** annotated git tag (first candidate v0.2.0) · the version is baked
  into both binaries via `-ldflags`, served from `/api/v1/panel/version`, and the hard-coded "v0.1.0" in
  Layout.tsx is deleted · CHANGELOG.md + CHANGELOG.tr.md start in Keep-a-Changelog format; `update.sh`
  prints "changes: CHANGELOG.md" on exit.

**Exit criterion:** AUTOPSY B1–B5 closed · the role×endpoint matrix runs in CI and no endpoint returns
200 for anonymous/empty-role · all deliberate 4xx checks are coded and ErrorBanner translates them ·
`panel --version`, the UI footer and the git tag say the same string · user-visible non-i18n English
strings in web: 0 (lint in CI) · as customer: create DB → open phpMyAdmin → accessing someone else's DB
is 404 (all three in B5 smoke). **Hard constraint: v0.3 cannot start before B1 is done.**

### v0.3 — Multi-Tenant Reality
Selling to more than one tenant without embarrassment. Four legs: customer and reseller can live on
their own; the plan/subscription machine can express "free tier + paid plan" **from entry to exit**
(cancellation as first-class as purchase); the essentials a cPanel emigrant looks for in week one are in
place; production trust is done before the first real tenant.

**Customer and reseller first-class:**
- The customer sees their own subscription: `GET /api/v1/my/subscription` (on B1 tenant scope) — plan
  name, quotas, live usage (domain/DB/mail counts + measured disk). A "My plan" card on the Dashboard:
  usage bars, warning color above 80%, an "Upgrade" button. A 409 quota error is consistent with the
  numbers on screen.
- Password recovery: single-use, 15-minute, argon2id-hashed token; mail from the panel's own MTA.
  E-mail verification arrives — no reset is sent to an unverified address.
- Invitation flow: password optional at user creation; without one the account opens "pending" and a
  first-password link goes by mail. A reseller never sees or relays a customer's password on any channel.
- Password change and reset drop all of the target's other sessions (completing the "leaked password
  neutralized" promise — today an open session survives a password change).
- Impersonation is accountable: `impersonate.start/stop` written to audit_logs; actions under
  impersonation carry an `acting_as` field marking the real operator; a persistent "Viewing as X — exit"
  bar at the top of the panel.
- **Reseller collection powers:** a reseller can call suspend/resume, "mark paid" and plan change for
  subscriptions in their own tree (B1 tenant scope filters); all of it lands in audit with `acting_as`.
  The reseller also sees their customer's subscription + payment state. A reseller who can't cut off a
  non-paying customer forwards collections to the operator — "reseller first-class" is half a promise
  without collections.
- Role-aware onboarding (the existing journey card pattern, no library): for the customer "first domain →
  SSL → first mailbox → connect your client"; for the reseller "plan → first customer → subscription".
  Tracks live completion, disappears when done.
- Per-page descriptions: one-two sentences TR+EN for 12 routes + 8 domain tabs (`pages.<id>.desc`).
  A "Why?" explanation at the 5 constraint-producing spots (D-002, D-003, D-009, conflict groups, pkg
  support) — texts consistent with the DECISIONS records; a constraint never hits as an unexplained wall.

**The plan and subscription machine (the free tier's foundation):**
- Prices on plans: `service_plans` gains `price_cents, currency, billing_period, is_free, is_public,
  sort_order, vat_included`. The admin defines "Free — 1 domain, 0₺" and "Pro — 10 domains, X₺/mo" from
  the panel; product prices move from code constants to the DB. The customer sees their plan's name and price.
- **The period model** (the answer to "I upgraded — what am I paying?"): subscriptions gain
  `current_period_start/end`; the rule is the simplest honest one and goes to DECISIONS — an upgrade
  starts a new period immediately (no proration, declared openly); downgrades and cancellations apply at
  period end; `expires_at` derives from the period edge. The v0.5 webhook extends these fields — no
  provider integration is built on top of an undefined term.
- Subscription suspension produces real effect (today `status`/`expires_at` are dead fields): on a
  suspended/expired subscription new resources are 403, vhosts flip to a reversible "account suspended"
  page, mail delivery stops (mailboxes are not deleted). Subscriptions past `expires_at` are flipped to
  expired by a daily loop + a grace-period field. A manual "mark paid" button drives the same machine —
  the payment-provider decision is v0.5; the machine works from day one.
- **Cancellation is first-class** (the subscription form of "data is never held hostage"):
  customer-triggered "cancel at period end" (`cancel_at_period_end`); automatic downgrade to Free at
  period end. If usage exceeds the Free quota the machine doesn't deadlock: **forced-downgrade mode** —
  nothing is deleted, the excess is frozen (new resources 403 + excess vhosts get the suspension page) +
  a list of the overflowing resources + an X-day wind-down period + a notification mail.
  `subscription.cancel` lands in audit. (The same mode drives the v0.6 trial expiry — an unattended
  downgrade can't hit a 409 and stay Pro forever.)
- Plan change (upgrades are monetization's main flow): `PUT /api/v1/subscriptions/{id}/plan` — quotas
  are re-copied from the plan; on a **human** downgrade, if current usage > new quota then 409 + the list
  of overflowing resources; in **unattended/forced** mode, the freeze rule above. `subscription.plan_change`
  in audit. A customer's "Upgrade" request initially produces a notification to the operator.
- **The payments ledger** (even manual mode leaves a trace): a `payments` table
  (subscription_id, amount_cents, currency, period, method=manual|provider, marked_by, created_at).
  "Mark paid" writes to it; the v0.5 webhook feeds the same table. The customer sees a payment history
  under the "My plan" card + a printable simple receipt. "What did I pay, what did I get" is answered
  from the panel.
- **Time-based notifications** (the cheapest collections tool): mail from the panel's own MTA to the
  customer (and the reseller, in reseller scenarios) at `expires_at`−7/−3/−1 days and at grace start; a
  "why + how to reopen" mail at suspension. The customer learns of the suspension from their inbox, not
  from their visitors.
- **The reseller pool is a plan type** (a quota with a commercial life): `reseller_pools`
  (max_customers, total disk/domain/DB) binds to the reseller's plan — pool sizes + price are written in
  the reseller plan; the reseller version of the "Upgrade" flow grows the pool. When a subscription
  opens, the reseller tree's total commitment is compared to the pool; on overflow 409 + remaining-pool
  message; a usage bar in Users. **The chain rule** (to DECISIONS): if a reseller is suspended, new
  resources in their tree are 403, but existing customer sites/mail live until the end of grace — an
  innocent end-customer is not blacked out instantly for their reseller's debt.
- Reseller-owned plans: the dead `service_plans.owner_id` comes alive — a reseller builds their own
  plans (quotas can't exceed their pool), sees global + own plans in the list, assigns only to their own
  customers; "apply to subscribers" copying respects owner scope.
- The billing ledger: `plan.create/update/delete`, `subscription.plan_change`, `subscription.cancel`,
  `subscription.suspend/resume`, `quota.exceeded` audit events — "when and by whom did this quota
  change" is a dispute question; it is never unrecorded.

**Hosting essentials (a cPanel emigrant's first week):**
- FTP (vsftpd) end-to-end — with criteria: per-domain accounts, chroot to the site user's docroot,
  **FTPS required** (plain FTP refused — security is the default). Proof: connect with FileZilla →
  upload → the live site changes; an escape attempt from chroot fails.
- Webmail (Roundcube) — with criteria: one-click install from the catalog; `webmail.<domain>` vhost +
  Let's Encrypt + Dovecot wiring automatic. Proof: a mailbox created in the panel sends mail to Gmail
  from webmail and reads the reply, without ever touching a shell. (Roundcube is not "for the panel" but
  "a service the panel installs" — the no-external-dependency rule is untouched.)
- File manager polish — three measurable items: (1) upload-and-extract zip/tar.gz + compress-and-download
  selection, (2) in-place text editing (ownership/permissions preserved), (3) view/change permissions
  (warning at 777). Proof: a WordPress theme zip installed via the file manager alone shows on the site.
- Noisy-neighbor brake: a systemd slice per site/subscription (CPUWeight + MemoryMax as plan fields;
  a field only arrives together with its enforcement — no dead fields are born). PHP-FPM pools and
  `celikapp-*` units attach to their slice; no CloudLinux license required. Proof: with one tenant
  running an infinite PHP loop, the neighbor site opens in <1 s.
- OS-level disk enforcement (the ROLES deferrals) · the cPanel importer proven with a **real** customer
  archive (DB users included) · WordPress Toolkit depth (updates, hardening, clone/staging).
- **The external-DNS decision** — D-009's promised re-weighing, before going to market: a "hosting-only"
  domain type (the panel writes no zone; a "enter these records at your external DNS" list + reuse of
  the live DNS verification infrastructure from mail-auth) is evaluated. Even if the decision is "no", a
  reasoned addendum goes to D-009 — a prospect on Cloudflare is not turned away at the door unexplained.
- **Panel identity — guided hostname + certificate (Jul 18 field gap):** making the panel's own name
  (e.g. `boston.celikhost.com`) resolve was NOT designed — on the test servers a non-operator hand (a
  record added from ANOTHER server's panel) closed it; that is the hidden manual step D-008 forbids. The
  panel must handle its own hostname with the same three honest paths as adding a domain: (a) **if this
  panel serves the parent zone itself** → one-click write the A record into its own zone (single-server,
  the generalization of the zone template seeding its own FQDN); (b) **DNS is external / on another
  server** → show "add this A record: `<host>` → `<IP>`" and wait, via the live DNS check from mail-auth,
  until it resolves, then offer the certificate; (c) **already resolves** → straight to the certificate.
  The certificate flow (v0.2) sits on top of this pre-step — the "install.sh → login → real certificate"
  chain no longer has a manual DNS gap. Cross-server auto-registration (registering a sibling's name in
  the zone-authority server) belongs deliberately to the multi-server feature (post-1.0) — it needs an
  inter-panel trust model; until then path (b) serves N servers honestly.

**Production trust (required before the first tenant):**
- Secret encryption pulled forward: A4's proven `enc:v1` mechanism extends to TOTP secrets and the
  private keys the panel stores (DKIM, WireGuard); legacy rows sealed idempotently at boot. (Only the
  external-audit verification remains in v0.5 — the first tenant's 2FA secret doesn't wait months in
  plain text.)
- Per-tenant rate limits: expensive endpoints (certificate issuance, backup trigger, import, bulk DNS
  writes) protected by subscription-keyed limits; a Let's Encrypt failed-attempt counter + an honest
  "approaching the LE limit" warning — one tenant's loop can't block everyone's certificates.
- Migration discipline (expand/contract): a destructive schema change splits across two releases (add +
  double-write in N, remove in N+1) — rollback loses no data. Two CI tests: the full chain onto a clean
  DB + the current chain onto a populated v(N−1) fixture; `rollback.sh` reports the number of rows
  written after the snapshot and asks for explicit confirmation; the sentence "rollback loses changes
  made after the snapshot" is documented.
- CI security gates: `gosec`, `govulncheck`, `npm audit --audit-level=high` on every PR; exceptions are
  `#nosec` + reason. (The v0.5 external audit is met with these gates' monthly green history.)
- A written promotion ritual (OPERATIONS.md): (1) CI green → (2) `update.sh` + golden-path smoke on both
  test servers (boston/Debian, frankfurt/Arch) → (3) production. Channels become explicit: main=edge
  (test servers), tag=stable (production runs only tagged commits).
- `release.yml`: on a `v*` tag push, `make dist` in CI → SHA256SUMS → tarball+checksum+CHANGELOG section
  automatically to the GitHub Release.

**Exit criterion:** one reseller + two customers run **one week self-service** — password reset,
invitations, quota views, DB, FTP, webmail included; operator touches: zero · the admin defines Free +
Pro plans from the panel, one subscription moves to Pro in a single call, the audit record lands · a
cancelling Pro customer automatically drops to Free at period end; resources over quota frozen and
listed, no data deleted · a subscription near expiry receives the −7/−3/−1 mails; a suspended one
receives the "why + how to reopen" mail · every "mark paid" lands in the payments ledger and the
customer sees their receipt in the panel · a suspended subscription serves the suspension page within
60 s; resuming loses zero data · a reseller with a 10 GB pool cannot open two 6+6 GB subscriptions (the
second is 409); a reseller suspends their non-paying customer and reopens them with "mark paid" on their
own · a real cPanel account migrates in one click · the external-DNS decision is recorded in DECISIONS ·
the v0.3.0 tag produced a downloadable release without a human hand.

### v0.35 — Plan Honesty: No Dead Fields
A short step. Every promise that exists in schema/catalog but is not enforced is either enforced or
deleted — the honesty rule's plan form. The paid tier isn't "done" until every line of a sold plan is real:
- `bandwidth_quota_mb`: enforce or remove. Enforcement: a subscription-level monthly counter from usage
  measurement (reset at period start); nginx logs count only web traffic — that mail/FTP are excluded is
  honestly written in the plan text.
- The overflow policy — the single answer to "when does the limit bite": per plan
  `enforcement ∈ {block_new, notify, suspend_writes}`; mail to customer + operator at the 80% and 100%
  thresholds; a quota row in the Dashboard's "Needs attention".
- Mailbox quota: `mailbox_quota_mb` + the Dovecot quota plugin (the existing file-based pattern,
  idempotent full-state push). `business_email` raises this limit — the product's first real gate.
- Product gates: every product listed in Addons is either wired to at least one `requireEntitlement`
  gate or cannot be purchased. `extra_ip` plumbing stays "coming soon" until v0.5; `firewall` leaves the
  catalog until a customer-visible feature exists. No "for sale" product that works without purchase remains.
- `additional_user` becomes a real feature: bound to a customer account (`parent_id`), resource-scoped
  permissions via `user_permissions` (domain list + file/mail sub-permissions), its own login. (The CHECK
  widening and the honesty of the dead role branches in the frontend happen in v0.2.5/B2.)
- The user-detail view: one screen with a customer's subscriptions + quota usage, domains, entitlements,
  last login (`users.last_login_at`) and last 10 audit rows — "why is this customer getting 409" doesn't
  tour four screens. Admin sees all; a reseller sees their own tree (the screen of v0.3's collection powers).
- Cron reliability: per job, last run time + exit code + a tail of the last output; on a failing job,
  mail to the domain owner (the existing mail stack, no new dependency).

**Exit criterion:** not one dead quota/state field between schema and enforcement (proven with a
field-by-field checklist) · a test subscription reaching 80% gets mail within 5 minutes; at 100% the
policy applies · the 101st MB into a 100 MB mailbox is refused with "Quota exceeded" · every Addons
product gated or unpurchasable · an additional user sees only the permitted domain's tabs · a cron
deliberately exiting 1 shows red in the list and lands in its owner's INBOX.

### v0.4 — Operational Trust
What the operator needs at 3 a.m.:
- Monitoring + alerts (service down, disk full, certificate error → mail/webhook) · log viewer in the panel
- **Metric history (from the Plesk comparison, Jul 17):** today's cards show an instant value, not a
  story. A lightweight sampler in the agent — CPU/RAM/disk/traffic every N seconds into a SQLite ring
  table, old data auto-downsampled (NO external dependency: no Prometheus/Grafana, the constitution
  holds). Sparklines on the dashboard cards (cards stay quiet); clicking a card opens a 24-hour / 7-day
  detail chart. Alert thresholds read the same data — charts and alarms are two faces of one substrate.
  Multi-location external uptime monitoring is deliberately out of scope: the honest answer is the
  heartbeat + "use UptimeRobot/360".
- **The alert channel's own health:** alerts go via two independent channels, mail AND webhook (if mail
  can't enter the queue it falls to webhook) + an outward heartbeat — the panel pings an external
  endpoint the operator chose every N minutes; when it stops, the alarm rings FROM OUTSIDE. The most
  likely failure is the death of the channel that carries the alert.
- Remote backup targets (S3/FTP) + restore drills as a product feature — **with two hardenings:**
  (1) **Panel-state disaster backup:** SQLite (consistent copy via the online backup API) + `secret.key` +
  DKIM/WireGuard keys + panel certificates in one archive, to the remote target with the same retention
  as domain backups. Losing `secret.key` = every sealed secret irrecoverable; "domains in backup but the
  panel's brain missing" is not accepted. (2) **Client-side encryption:** every archive leaving for a
  remote target is encrypted in the panel; the backup key is kept separate from `secret.key` and shown
  once at setup — the "backups unreadable without the key" honesty is written in the UI.
- **Customer self-service restore:** from the backup list, full-site / single dir-file / DB dump
  restores — with a confirmation modal + audit record. The most expensive answer to "will YOU solve
  every user's problem?" is restore; at 3 a.m. the customer reverts their own mistake themselves.
- **The secondary-DNS truth:** today ns1/ns2 point at the same machine (a single point of failure); a
  secondary PowerDNS (AXFR) on a cheap second VPS, or honest documentation (added July 11)
- One-click panel self-update in the UI (update.sh's front end) · WebSocket live notifications
- **The update chain hardens:** the release-binary channel becomes primary — `update.sh` downloads the
  release tarball, **verifies a signature** (minisign/cosign; public key baked into install.sh, rotation
  plan written), verifies SHA256, skips the build step. No Go/Node toolchain needed on production
  (attack surface + OOM on small VPSes + bit-level drift close all at once); `--from-source` remains for
  dev/test servers. On verification failure: stay on the current version + "Needs attention" + audit record.
- **Post-update self-check:** an automatic smoke at the end — panel HTTP 200 + login render + agent
  socket ping + `PRAGMA quick_check`; if anything remains the output says "rollback.sh recommended", and
  the one-click flow offers a rollback button.
- **The failure matrix — proof that "panel dead, hosting alive":** panel process killed → web/DNS/mail
  keep serving (measured by test), the panel comes back via `Restart=on-failure`; a "break glass"
  runbook (diagnosis steps over SSH when the panel won't start) is written into D-008 as the emergency
  exception.
- **CelikPanel→CelikPanel migration:** an account-level export archive (domains + docroot + DB dump +
  maildir + DNS zone + DKIM key + subscription/quota metadata in one signed tar); on the target server
  the cPanel importer's inspect→confirm→apply flow recognizes this format too. The full form of "data is
  never held hostage"; changing servers is hosting routine.
- **Visitor statistics (minimal, deliberately bounded):** traffic measurement already reads the nginx
  access log; the same pass extracts daily hits / unique IPs / top 10 pages / top 10 referrers into a
  card on the domain Overview. Full analytics (sessions, geo, real-time) is out of scope — the honest
  answer: "put Plausible/GA on your site".
- **Audit integrity:** write failures are counted and surface in "Needs attention" (without blocking the
  action, but never silent); configurable audit retention + pruning.
- **Active sessions:** a session list in Settings (created, last used, IP) + single/bulk termination;
  an admin can drop a target's sessions from Users.
- **Clean start:** the panel marks catalog services it did not install itself as "foreign" (any catalog
  service with no `service.install` audit-log entry) and offers removal with the operator's consent —
  adopt or evict, the mirror of the Import philosophy. Field proof (July 16): Hostinger's Arch image
  shipped a dormant bind; the setup journey counted "DNS installed: Done". Builds on B3.
- **Dashboard SSL/TLS summary** (from the Plesk comparison, July 17): expiring-soon / valid /
  no-certificate counts in one card — the "certificate silently dies in 90 days" class becomes visible
  on the panel's face
- **Dashboard Mail Queue card** (from the Plesk comparison, July 17): Total/Deferred/Held plus one-click
  queue clear — the first place an operator looks at 3 a.m.
- **Self-diagnosis:** the operator's July 17 question is the design bar — "will YOU solve every user's
  problem?" The panel must check by itself the classes we diagnosed by hand: does the DNS delegation
  actually point at this server, **does the panel's own hostname resolve to THIS server** (Jul 18: the
  boston.celikhost.com record lived in frankfurt's zone, boston had no idea — that coupling was
  invisible), does the certificate renewal timer actually run, can the service config actually start the
  engine. A finding = a "Needs attention" row + one-click repair (the honest counterpart of Plesk's
  Repair Kit)

**Exit criterion:** a killed service alerts within a minute; a server with its plug pulled produces an
**external** alarm within 5 minutes · the restore drill's definition: on a clean VPS, `install.sh` +
panel-state restore + domain backups bring the full server up and DKIM-signed mail lands in the INBOX
**with the same keys** · no object in the remote store is unencrypted · a version update succeeds on a
production VPS with no Go/Node installed; a tarball with a broken signature is refused and lands in
audit · a customer restores their deleted `wp-config.php` from the panel on their own · every cell of
the failure matrix drilled at least once.

### v0.5 — Security Depth
- The WAF decision (ModSecurity or an honest alternative) · deep fail2ban integration · scheduled ClamAV site scans
- External-audit verification of secret encryption (implementation done in v0.3: TOTP + DKIM + WG keys under `enc:v1`)
- External security audit · dedicated IP plumbing (the sellable `extra_ip` — the gate marked "coming soon" since v0.35 opens)
- **API token management:** named tokens per user (crypto/rand, only the hash in the DB, shown once,
  scope: read-only/full, revocation); token'd requests pass the tenant-scope filter and land in audit.
  The "API-first" promise cannot be kept with an API unusable from curl.
- **SFTP/SSH key management:** the customer adds/removes public keys for the site user (ledger in the
  panel, the agent writes `authorized_keys` with full-state push); password login and shell **off by
  default** (internal-sftp + chroot docroot); `access.ssh_key.add` in audit. This is the path for
  agencies beyond plain FTP and for CI/rsync/git-hook flows; an in-panel terminal stays deliberately
  out of scope.
- **2FA depth:** 8 single-use recovery codes at enablement (argon2id-hashed, shown once); a "use a
  recovery code" branch at login; a "2FA required for admin/reseller" setting — when required, a user
  without 2FA is locked to the setup screen at first login.
- **The payment-integration decision record (D-0xx):** provider class = hosted-checkout /
  Merchant-of-Record (the Stripe Checkout / Paddle / iyzico class — card data never enters the panel,
  PCI scope zero); the panel is only a **webhook consumer**: a subscription↔provider-customer mapping
  table + one signature-verified, idempotent webhook endpoint (a ledger of processed event_ids) +
  `payment.event` audit. The event contract has three classes: "paid" → the period extends (a row in the
  payments ledger), "unpaid" → grace → suspension (the v0.3 machine), **"refund/chargeback"** — refund:
  the period's extension is reverted (`expires_at` shortens) + `payment.refund` audit + operator
  notification; chargeback: automatic suspension + a "Needs attention" row (a chargeback is also an
  abuse signal). Invoice PDFs are links from the provider. The manual "mark paid" mode remains for the
  provider-less operator; **in the first release the provider integration is operator-plane only —
  reseller collections continue via the manual flow (deliberate and written).** Constitution constraint:
  one binary + SQLite — payments are never embedded in the core.

**Exit criterion:** the external audit yields no high-severity findings · a customer buys and uses a
dedicated IP from the panel · the single curl example in the docs opens a domain with a token; a revoked
token is 401 immediately · no plain secret is grep-able in the DB dump or dataDir · a forged webhook
event extends `expires_at` on the test server; the same event a second time is a no-op; a refund event
reverts the extension · login with a recovery code succeeds once, 403 the second time; with enforcement
on, a reseller without 2FA cannot log in · a keyed customer sees only their own docroot over sftp; a
shell attempt is refused.

### v0.6 – v0.9 — Beta Program
- OpenAPI documentation (proof of the API-first promise) · admin and user guides TR+EN
- **Help center + deep links:** a static TR+EN site generated from markdown under docs/ (simple
  generator, no external dependency enters the panel); a help icon in every panel page's header going to
  the doc page via a stable slug (`pages.<id>` → `/help/<lang>/<slug>`); a broken-slug test in B5 CI.
  Documentation not linked from the panel is born dead.
- **Browser first-boot:** when no admin exists in the DB the panel enters a one-time first-boot mode —
  the admin is created from the browser with a single-use token produced by `install.sh`; the mode closes
  permanently once an admin exists. The "return to the terminal, run a CLI" step in the middle of the
  "install.sh → login screen" promise disappears; placed in this release, not rushed, so that no
  unprotected first-setup endpoint ever exists.
- **Self-signup + abuse brakes** (the free tier's scaling path): an e-mail-verified signup form —
  **default OFF** (security is the default), the operator who enables it binds it to a free plan;
  a disposable-domain list + per-IP signup rate limit. Brakes: an hourly outbound-mail cap on the free
  plan (on the mail_policy pattern), an optional 24-hour send-hold on new accounts, resource-creation
  rate limits. Trials: `trial_days` per plan → `expires_at` fills automatically; on expiry the account
  drops to free **in forced-downgrade mode** (the v0.3 rule — an unattended transition can't hit a 409).
  One bad customer can burn the IP reputation earned with the deliverability screen — signup does not
  open without brakes.
- **Command palette:** Ctrl+K — nav + domain list + service pages in one fuzzy search; navigation only
  in the first release (no action execution — the security surface doesn't grow), a "?" shortcut list
  beside it; dependency-free, respects the role filter. It would be a luxury on today's 12 routes; the
  right moment for a beta operator's daily efficiency is here.
- **SUPPORT.md (TR+EN):** before 1.0 only the latest minor is supported, security fixes are patched onto
  the latest minor; the upgrade path is "from any release to the newest, the migration chain proven with
  fixture tests"; at v1.0 it widens to N−1. Published together with the beta invitation — the answer is
  not improvised in the moment.
- **KVKK/GDPR minimums:** data export = the customer-triggered form of the migration archive; account
  deletion covers, beyond the DB cascade, the maildir + docroot + the account's backup archives and
  produces a "what was deleted" report; log/audit retention configurable and documented.
- Real external beta users; their walls become fixes (the alpha model, scaled)
- Performance targets measured and enforced (<100 ms API)
- **The license/business-model decision** (suggestion: open core) → repo visibility accordingly. With
  the decision, two commitments go to DECISIONS: (1) **the two-plane separation** — the operator
  charging their own customers (a panel feature) and CelikPanel charging the operator (a license) are
  separate planes; no table/endpoint of the first ever carries telemetry/license checks for the second,
  and the panel works fully offline. (2) **Price positioning:** the price is per server and **never**
  scales with hosted accounts/domains — cPanel's post-2019 per-account model triggered the largest
  migration in panel history; announced late, beta arrives with the "this too will cPanel-ify" suspicion.
- **The "Why CelikPanel" document** (docs/WHY.tr.md + WHY.md): three columns (CelikPanel/cPanel/Plesk) —
  install footprint, default security, one binary vs a forest of services, TR-first, the price
  principle; plus a reasoned confession of deliberate gaps (every refusal links to a DECISIONS record).
  We do the comparison ourselves, honestly, before competitor marketing does.
- **Light brand customization:** a global "panel name + logo + accent color" setting (one table row;
  login + sidebar + mail templates read one source). Per-reseller branding is post-1.0.

**Exit criterion:** ≥3 external operators run real sites for ≥1 month; documentation answers their
questions before we do · on a clean VPS an admin is created and logged in from the browser without
returning to SSH; a token-less/second attempt is 403 · no account can be opened with an unverified
e-mail; a free account is limited at the N+1st mail in an hour · every panel page has a working doc
link, the broken-slug test is in CI · the beta announcement shipped with the price principle and the
WHY page · SUPPORT.md is live and the v0.3.0→current jump test is green in CI.

### 🎯 v1.0 — General Availability
- Clean VPS → a working panel in minutes, documented, self-updating
- "Domain → live site" 100 times in a row without error (the Phase 1 promise, now in CI)
- Migration from cPanel proven repeatedly · pricing/license published

**Exit criterion:** a stranger with no help beyond the documentation reaches, on a clean VPS, from
install to a live HTTPS site and mail landing in the INBOX, on their own · "domain → live site" 100/100
in CI · at least one real cPanel migration and one CelikPanel→CelikPanel move proven in production ·
the pricing/license page and the SUPPORT policy are published.

### Post-1.0 — horizon *(demand persists, fantasies don't)*
Each only with real demand, in keeping with the deliberate non-goals:
- Multi-server (an inter-panel trust model + **sibling-server DNS auto-registration**: a new CelikPanel
  server registers its own hostname A record with the other CelikPanel that is the zone authority for the
  brand domain, using an operator-provided API token — the productized form of today's manual
  boston.celikhost.com step; v0.3's path (b) external-DNS covers N servers until this lands) · a BSD
  agent backend · billing integrations (WHMCS etc.)
- **Plesk/DirectAdmin importers** — only once real demand exists (≥5 concrete migration requests); the
  cPanel importer's inspect→confirm→apply pattern is reused. Until then the honest answer is documented:
  "Coming from Plesk? A manual migration guide, for now."
- **Per-reseller white-label** (custom login domain, reseller logo) — light global branding is in
  v0.6–0.9; per-reseller only if reseller sales demand it.
- **A hosted service** (CelikPanel itself renting VPS+panel) — not rejected outright like the
  marketplace, but not designed now; if it opens, it carries not one line of telemetry/phone-home into
  the self-hosted product's architecture.
- A marketplace **never** (the AltaVista mistake).

---

## Deliberate Non-Goals

Simplicity is being able to say no. These are absent **on purpose** — and most of the refusals are the
product itself:

- ❌ **A Docker/container layer** — the target market is classic hosting; native is correct. It is also
  the competitive sentence: a Plesk install brings hundreds of packages and dozens of services;
  CelikPanel brings two binaries. That difference is not a gap — it is the product.
- ❌ **A management screen for every conceivable service** — what isn't installed is invisible.
- ❌ **Theme/skin markets, portal showcases, a plugin marketplace** — the AltaVista mistake; moreover the
  no-third-party-code guarantee is the natural consequence of "only the Panel reaches the root Agent."
  A plugin market sells that guarantee away.
- ❌ **The general app-catalog race (Softaculous's 400+ entries)** — a shallow-wide catalog is a
  maintenance black hole and demand is overwhelmingly WordPress. The catalog grows one entry at a time
  only when (a) proof of real demand and (b) WordPress-quality end-to-end install (official tarball,
  verification, full configuration) hold **together**. Position: deeper than anyone on WordPress,
  deliberately narrow on the catalog.
- ❌ **Full visitor analytics** (sessions, geo, real-time) — the minimal log card in v0.4 suffices; for
  more, the honest answer: put Plausible/GA on the site. An AWStats clone fails the simplicity filter.
- ❌ **An in-panel terminal** — attack surface and simplicity; SFTP/SSH key management (v0.5) serves the
  legitimate need.
- ❌ **Multi-server / cluster (for now)** — no distributed-system dreams before one server is flawless.
- ❌ **External dependencies for the panel itself** (Redis, external DB, message queue) — one binary +
  SQLite stays. Even payments are not embedded in the core; the panel is only a webhook consumer (the
  v0.5 decision).
- ❌ **Telemetry / phone-home** — the panel makes no outbound request of its own volition (the
  operator-configured heartbeat and inbound webhooks excepted). Whatever the license model, this does
  not change.
- ❌ **BSD support (for now)** — **but the option is deliberately preserved, and never as a fork.** The
  panel↔agent RPC contract is OS-neutral by design: the panel (HTTP/SQLite/UI/business logic) is
  portable Go that already cross-compiles to FreeBSD; only the agent's "hands" (systemd/apt/nftables)
  are Linux-specific. If real demand arises (e.g. a Linux trust crisis pushing hosts to BSD), the move
  is a BSD agent backend behind the same RPC surface — work measured in weeks, one product. There will
  never be two CelikPanels. The discipline that keeps it cheap: new agent features keep the "what" (RPC
  surface) separate from the "how" (exec calls) — the code is already written that way.
  *(Decision: July 8, 2026.)*

---

## Where We Are — July 18, 2026

**Version:** v0.1.0 alpha (untagged yet — bound to one source in v0.2.5), live on the production VPS
(Debian 13, panel-only install). Two test servers: boston (Debian 13) + frankfurt (Arch — deliberate,
D-004 amendment: package-layer diversity). Both panels run with real Let's Encrypt certificates and the
firewall on (default-deny); renewal dry-runs proven on both.
**Place on the ladder:** v0.2 in progress — PowerDNS installed from the panel and serving celikhost.com
to the world; the next click is auto-repair, then the first real site.
**Debt state (AUTOPSY):** A1–A4 closed (A3's admin gate hangs on B1), A5 open ·
B0 done, B1 and B5 partial, B2–B4 open.
**This update:** the operator's three new requirements (in-panel help, reseller+customer experience,
free-tier plan system) worked into the ladder step by step; the subscription machine got its **exit**
as well as its entry (cancellation, the period model, the payments ledger, time-based notifications,
refund/chargeback, reseller collections, the pool chain rule); the v0.35 "Plan Honesty" step added;
v0.4–v0.9 widened with operational-trust and release-engineering items; the non-goals fortified with
reasons.
**Report card (measured July 11, not re-measured):** feature code ≈ 70% of v1.0 scope · proofs ≈ 45%
(Ubuntu full, Debian partial) · polish/design ≈ 80% · documentation ≈ 60% · external validation ≈ 0%
(starts at v0.6).
**Today's system:** ~29k lines of Go (151 files, 72 HTTP endpoints, 38 agent RPC files, 15 migrations,
a 19-service catalog) + ~16.5k lines of TypeScript (55 component files, TR+EN).

---

## How This Document Is Updated

- **Every significant decision lands here via a commit.** The reasoning goes to
  [DECISIONS](docs/DECISIONS.md), the debt to [AUTOPSY](docs/AUTOPSY.md); this file records only the
  place on the ladder and the criterion.
- A new request **cannot enter as "later"**: it is either written onto a version step with a measurable
  exit criterion, or added to Deliberate Non-Goals with a reason. There is no third way.
- The next step does not start before the exit criterion is met; if a criterion must change, the change
  is committed together with its reason (no silent dilution).
- Every edit refreshes the "Last updated" date and the "Where We Are" section; the Turkish original
  (ROADMAP.tr.md) and this mirror update in the same commit.
