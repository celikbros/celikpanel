# Strategic Decisions

*Project record · [Türkçe](DECISIONS.tr.md)*

Durable record of the **why** behind big directional choices — the reasoning
we do not want to re-derive from scratch each time the question resurfaces.
Code decisions live in git; this file is for strategy. Newest first.

---

## D-012 · Licensed products and the resale chain: an entitlement is a pool; compliance is between the operator and the vendor

*July 20, 2026*

**Decision.** The operator's model ("everything is an item; some free, some paid;
the admin buys and sells to resellers or directly to customers") is met by the
following — and **third-party commercial products are in scope from the start**
(operator's decision):

1. **"The thing" and "the right to use it" are separated.** The product itself is
   installed once on the server (multiplication test = 1 → catalog item, D-011).
   The **entitlement** is per subscription and flows down the tree. These are
   independent axes: installation is a binary state, an entitlement is a countable
   resource.
2. **An entitlement is a pool, like disk/domains.** No new mechanism: v0.3's
   `reseller_pools` pattern extends to products. The admin's quota → allocated to a
   reseller → allocated to a customer; on overflow, a coded 409 + remaining pool.
   The admin may also sell **directly to a customer** (the subscription ownership
   tree already supports this).
3. **The license model enters the product definition:** `license_model ∈ {server,
   seat}`. *server* → one price, unlimited users; the admin's pool is infinite and
   resale is pure margin. *seat* → the admin pays the vendor per unit, so
   **over-allocation is real money and a licence breach** — the pool is enforced
   hard (a refusal, not a warning).
4. **`seat_unit` is mandatory** (`mailbox | site | subscription | server`). Vendors
   count different things; without the unit we count the wrong one and the panel's
   number will not match the vendor's invoice — the subtlest trap here.
5. **The licence key is stored as a secret** — A4's `enc:v1` mechanism (sealed at
   rest, opened at use). Most commercial products need the key at install time; an
   install without it is half an install.
6. **A price is not one number.** The chain has three: vendor→admin (cost),
   admin→reseller, reseller→customer. Today's single `MonthlyPriceCents` is not
   enough; the pattern matches v0.3's reseller-owned plans (`service_plans.owner_id`).
7. **Visibility follows entitlement.** If a reseller has not bought the product,
   their customers **never see it** ("what isn't installed is invisible", applied to
   entitlements). Subtlety: a reseller may switch on a "buy" prompt — visibility is
   the reseller's call, hidden by default.
8. **A product that is not installed cannot be sold.** Selling an entitlement for a
   product absent from the server sells the customer nothing → coded refusal.
9. **Revocation follows the SAME rule as subscription suspension.** When a reseller
   stops paying: new allocations in their tree are 403, existing usage lives until
   the end of grace, no data is deleted. There are not two different "cut off"
   behaviours.

**Why this burden was accepted.** Opening the chain to third parties later would
mean rewriting the allocation and pricing model; adding `license_model`/`seat_unit`
up front costs two fields. The operator deliberately chose the harder path.

**THE HONESTY LIMIT — what the panel does NOT guarantee.** The panel enforces only
**its own allocation records**; it does **not** guarantee compliance with a
vendor's licence terms:
- The vendor may count differently (e.g. domains instead of mailboxes); the panel
  shows a reconciliation view ("vendor: 50 · allocated: 47 · free: 3"), but **the
  invoice is the vendor's truth**, not the panel's.
- **The right to sublicense a third-party product (to a reseller or customer) is
  between the operator and the vendor**; most vendors bind it to a partner
  agreement. The panel cannot verify that right and does not claim to. This is
  written plainly in the UI and the docs — otherwise we would ship a feature that
  encourages licence violations.

**Rejected alternatives.** Silent over-allocation (warn and continue — under the
*seat* model it runs up a debt for the operator) · putting per-vendor API
integrations in the core (each vendor is a separate integration, against the
no-dependency rule and simplicity; we start with a **manual key + manual quota**,
and automate only if real demand appears for one specific vendor) · burying
entitlements inside plans (a plan change would silently drop rights; an entitlement
is its own record, a plan only supplies defaults) · deferring third parties (the
operator refused: opening the chain later means rewriting the allocation model).

---

## D-011 · A framework is not a service: the multiplication test, a preset budget, and the limit of version ownership

*July 20, 2026*

**Decision.** Four linked decisions in one record:

1. **The boundary rule becomes countable — the multiplication test.** One question
   decides whether something is a catalog item or site-internal: *when N sites are
   created on the server, does the number of copies of this thing on disk stay at
   1, or become N?* 1 → catalog item (its version is managed by the distro/vendor
   repo); N → site-internal (its version is managed by the customer's lock file or
   the application's own updater). **The catalog = L1 (services) + L2 (multi-version
   runtimes), nothing else.** Laravel, Symfony, Django and Next.js are **not in the
   catalog and never will be**; Composer, Node and certbot are catalog items (a
   single copy). The class is set by **deployment shape**, not by the software:
   the very same phpMyAdmin, if copied into every customer's docroot, would become
   N and fall out of the catalog. The test is not arguable because it is numeric.

2. **The panel never owns the version of anything that exists N times.** The panel
   does not **write** an updater; it **triggers** the application's own updater as
   the site user. It does not pin versions, patch files, write `DISALLOW_FILE_MODS`
   or display a "supported version". This binds the roadmap's "WordPress Toolkit
   depth (updates, hardening)": no hardening preset may disable automatic updates
   by default.

3. **Framework support = site primitives (L3) + a preset; never a type, never an
   installer.** The only thing the panel may own about Laravel is a *set of
   defaults* (a `public` docroot suggestion, the cron line text, a queue command
   builder) — not an installer. Provisioning contains not one `if type == laravel`
   branch.

4. **`appCatalog` is not a type axis but a reversible install action.** The site
   type is PHP; WordPress is **not** an option in the domain-creation flow — it is
   an action run afterwards and **removable**.

**Why.** The operator asked: "Is Laravel a service, does it belong in the catalog —
and if we open that door, does it go on forever?" Five competing panels' real
record was examined:

- **The engine of drift is not the catalog, it is presets.** A catalog entry is
  expensive (package name, two distro families, a removal path, an init step); a
  preset costs three strings and has no argument against it. "Symfony comes for
  free" also holds for Django, Ghost and Strapi; by the eighth preset the screen is
  a framework picker. **cPAddons, too, began as "just a configuration recipe", not
  a one-click installer.**
- **The second engine of drift is version ownership.** What killed cPAddons was not
  installation: the panel took ownership of N-instance WordPress versions, patched
  `update.php`, and users were stranded on WP 3.9. The same pattern repeated at
  aaPanel (a one-click frozen on Laravel 5.4/PHP 7.4; their own staff say "delete
  the files we generated") and Plesk (the skeleton generator broke on an upstream
  change; the APS catalog was **removed entirely** in 18.0.77). The industry
  retreated from this model.

**Three mechanisms that make breaking the rule expensive** (a rule that lives only
in Markdown will not survive two years — unlike D-003/D-010, this decision needs
enforcement in code):

- **The structural purity test.** A preset is accepted only if it can be expressed
  as *pre-filled values of already existing generic fields*. If it needs one new
  field or one `if framework ==` branch, the preset is **rejected**. Presets live
  as pure data in one file; in the UI they are not a selectable "type" but a "fill
  the form" button above the form. Passing 3 presets is not a code change but a
  **strategy change**.
- **A framework-name ban + a CI gate.** A framework name may not appear in any enum
  constant, DB column, API field value or systemd unit name — only in i18n strings.
  The docroot dropdown lists **path values**, not framework names
  (`(root) | public | public_html`). `validProjectTypes`
  (php/static/node/proxy/forwarding) defines the *transport shape*, not the
  application — it is **considered locked**. In CI, grepping Go sources for
  `laravel|symfony|django|nextjs|ghost` must return zero matches (i18n excluded).
- **Every entry's exit is written first.** `appCatalog` entries carry a mandatory
  decision-record ID, maintainer and source checksum — "adding an entry in three
  lines" becomes impossible. Each admission is written together with its removal
  path and the answer to "what happens to sites already installed" (always: **the
  files belong to the customer; the panel only removes the button**).

**Commercial pressure is closed too.** The `app_installer` entry today says
"One-click installs for WordPress **and other apps**" — selling a one-entry list as
a plural plan feature. Two harms: sales sees an empty bucket to fill (**the
strongest engine of drift is a promise already sold**, not engineering), and once
the list is bound to a plan feature, deleting an entry becomes a contract breach.
The product is brought back to reality: "One-click WordPress install".

**Positioning (sales and product use the same sentence).**
*"CelikPanel does not install your application; it manages the server underneath
it. Your code's version is yours — and we give you that not as a limit but as a
guarantee."* For the technical buyer: *"Everything the panel owns exists exactly
once on the server; we never touch the version of anything that sits in your site
as N copies."* A competitor cannot make that promise honestly. **Note:** "we have
one-click too, it's called `composer create-project`" may only be used once the
run-as-site-user command endpoint **ships**; it does not exist today.

**The real work is not in the catalog.** This decision fixes what we will not do;
the gap is at L3 — today we cannot host Laravel even without a catalog entry:
- the docroot is pinned to `public_html` in `site_orchestrator.go` (Laravel needs `public/`)
- ~~the vhost served `.env` in plain text~~ → **closed** (commit `0088c67`, same day;
  the same fix also repaired ACME for static sites)
- `composer`/`artisan` appear zero times in the codebase (no endpoint runs a command as the site user)
- a queue worker is impossible behind three independent blocks (the `RunAsUser: "www-data"`
  constant, the `req.Port <= 0` rejection, the `project_type == "node"` lock)

**Rejected alternatives.** A "Laravel" project type (once an enum carries a
framework name the catalog is implicitly born, and it is irreversible because it
persists in the DB) · a panel-generated skeleton/tarball (aaPanel frozen on an old
version, Plesk broken by an upstream change) · a Softaculous-style "Frameworks"
category (15 entries: Bootstrap is a CSS framework, Kohana dead since 2016,
Symfony 2.3.42 EOL 2017 — still installable) · schematizing `.env` field by field
(it silently corrupts the customer's file on multi-line values; Forge and Ploi both
keep an opaque blob — the only legitimate "smartness" is to look at what is on
disk: copy `.env.example` if it exists) · making the scheduler a first-class
concept (it is an ordinary crontab line) · applying a preset automatically
(consent belongs to the user; otherwise ghost crons/units appear).

---

## D-010 · Catalog kinds (service/runtime/tool) + an "installed-first" default; real multi-PHP via Sury

*July 20, 2026*

**Decision.** Three linked decisions in one record:

1. **One catalog, but `ManagedService` gains a `Kind`:** `service` (a systemd
   daemon), `runtime` (a versioned interpreter), `tool` (a daemonless web tool).
   PHP-FPM and Node.js become two members of the same kind; phpMyAdmin/phpPgAdmin
   stop being "services". Row rendering branches on `Kind`, and today's
   `Daemonless = len(SystemNames)==0` heuristic is deleted — that one flag marks
   three different things today. **Versions are not rows; they live inside the
   row** (a version drawer). No separate `/runtimes` or `/apps` page.
2. **The Services page becomes "installed-first":** "hide not installed" defaults
   to ON. The catalog is the same list with the filter off (no second screen, no
   second search box). *Implementation correction (Jul 20, same day): collapsing
   is not a fixed default, it follows the view* — categories are EXPANDED in the
   installed view (the list is already short; folding it meant three clicks to
   see three services) and COLLAPSED once the catalog is shown (a long list wants
   folding). ✅ shipped: commit `312e378`.
3. **Real multi-PHP via the Sury vendor repo** (applying D-007 to PHP): on
   Debian/Ubuntu, side-by-side `php8.x-fpm` packages become installable from the
   panel. Arch has no clean path (AUR only) — there we honestly say "the distro's
   single version" (D-004: apt first-class, pacman dev-test).

**Why.** The operator's question was: "there is no Node.js/Go option; should it
go here, and won't the page get very long?" Five competing panels were studied
from their real documentation, and two things came out:

- **Page length is not a taxonomy problem, it is a default problem.** Length must
  be measured by **installed count**, not catalog size; with the filter on by
  default a clean server shows 3-4 rows whether the catalog holds 19 items or 40.
  This turns the constitution's "a service that isn't installed is invisible" from
  a checkbox into product mechanics.
- **List explosion comes from making versions into rows.** Plesk's "PHP
  interpreter versions (2 of 12 selected)" tree is the proof. One row per
  language with a version manager inside cuts the explosion at its source.

Concrete mistakes we avoid: **cPanel**'s Service Manager / Application Manager
split (the user memorizes which screen answers which question); **Plesk**'s
inconsistent runtime model (Node is an extension on Linux but a component on
Windows; Ruby lives in the CLI; Python is a checkbox); **HestiaCP**'s bug #5050
(the Edit of N version rows all lands on one version-less page — our version
endpoints are parameterized); **aaPanel**'s PM2 duplication (one capability, two
entry points — our equivalent is `AdminNodeInstall`, which is deleted: the single
address for runtime installation is Services).

**Go/Java are out of scope as runtimes.** Go compiles to a single binary; there is
no runtime to install, and the correct support already exists as the `proxy`
project type + a systemd unit. A "Go" row in the catalog would be a conceptual
error. Java/Tomcat additionally clashes with the rest of the panel through its WAR
deployment model. Ruby, PM2, Supervisor, Docker, Elasticsearch and
MongoDB/RabbitMQ/Varnish also stay out of scope.

**Three linked sub-decisions** (fixed here because the pattern will repeat across
five languages):
- **The "system interpreter" escape hatch is removed:** the panel runs only the
  runtime it installed itself (the same discipline as PHP). Accepting the `node`
  on PATH — which the panel neither installs, updates, nor removes — creates an
  invisible dependency.
- **A Node project without a web server is refused** (symmetric with php/static,
  as a coded refusal + action button under the B1 contract): today the unit comes
  up but the domain does not answer without a reverse proxy — the user sees a
  green status and a broken site at the same time.
- **A version/service in use cannot be removed:** a coded refusal + the list of
  blocking sites (`RUNTIME_IN_USE`, `SERVICE_HAS_DEPENDENTS`). Bulk version
  migration is separate work (v0.3).

**Correction to D-002.** D-002 claimed "PHP — side-by-side packages, chosen
per-site ✅ built"; the code delivers only half of that: **detection and per-site
selection were built, multi-version INSTALLATION was not** — the `php-fpm` catalog
entry has no `Repo`, and there is not one line mentioning `sury`/`ondrej` in the
codebase. The panel carries a multi-PHP interface while being able to offer a
single version: "a picker with nothing to pick". This decision closes that gap.

**Rejected alternatives.** A separate `/runtimes` page (a second screen = cPanel's
split) · a separate `/apps` page (the counter+list under the Node row suffices;
revisit past 30 apps) · a server-role/profile UI (the `Role` field enters the data
model now, but the filter is weighed only past 25 catalog items) · a batch-commit
planner (not written until the dependency chain runs deeper than 2 items).

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
    per-site under runtimes. ⚠️ *Correction (Jul 20, see D-010): only HALF was
    built.* Detection (`DetectInstalledPHPVersions`) and per-site selection
    work; but with no `Repo` defined for `php-fpm` in the catalog the panel
    **cannot install** multiple versions — it is limited to the distro's single
    one. The Sury vendor repo is added by D-010.
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
