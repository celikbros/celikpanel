# Strategic Decisions

*Project record · [Türkçe](DECISIONS.tr.md)*

Durable record of the **why** behind big directional choices — the reasoning
we do not want to re-derive from scratch each time the question resurfaces.
Code decisions live in git; this file is for strategy. Newest first.

---

## D-019 · The panel does not scare: every management page carries its own help; empty pages are forbidden

*25 July 2026*

**Decision.** The operator asked for it directly: user-friendly, reassuring
pages with a help button that explains the page itself. Three rules are
permanent:

1. **Every management page has a Help button.** It comes from the single
   shell (ServiceShell), so nobody writing a new page has to remember help
   separately. Content has three sections: *What is this?* (2-3 plain
   sentences), *Tips* (practical panel-first tasks), *Something looks
   wrong?* (symptom → fix). The language is deliberately non-technical:
   "background process", "databases have no trash bin", "reading is always
   safe".
2. **Help content is CONTENT, not chrome.** It is not smeared over hundreds
   of `help.x.tip3` i18n keys; both locales sit side by side in one entry
   per component (`web/src/help/serviceHelp.ts`), so updating one language
   and silently forgetting the other is structurally impossible. A component
   without a specific entry gets its kind's generic one — a page without
   help cannot exist.
3. **Empty management pages are forbidden.** The panel already knows a
   component's status, unit, versions, packages, ports, config files and
   journal; a "coming soon" page is a refusal to show what is known. The
   derived panels (ComponentPanels) are every page's floor; specialised
   pages build ON TOP of it, never instead of it.

**Why.** A page that scares goes unused, and an unused panel is untrusted.
The help button must be there at exactly the moment a page frightens its
reader — so it shows on not-installed, failed and half-configured services
too. Hand-written page lists (the nine-page switch) rotted the same way the
scanner's hand-written unit list did; the derived floor closes that class.

---

## D-018 · The install source is fixed by kind: system service = distro package, app/runtime = official release, Docker is not the base

*July 24, 2026*

**Decision.** The operator asked: "wouldn't it be better to always pull from
git and build for our system, or use Docker?" The answer is not "one method
for everything" — chaos comes from having no rule, not from this one. The
source is chosen by the item's KIND, and each kind's rule is fixed:

1. **System service** (nginx, PostgreSQL, MariaDB, Postfix, Dovecot, Redis…)
   → the **distro's package repo** (apt/pacman/dnf). Why: the distro's
   security team owns the patching (`apt upgrade` fixes it); systemd
   integration and config paths come ready. Building from source puts CVE
   tracking on us; Dockerizing breaks start/stop/status-read over systemd.
2. **Runtime + portable app** (Node.js, Roundcube, later Python/Ruby, webmail
   alternatives) → the **vendor's official release** (download + verify a
   pinned SHA-256 + unpack). Why: the distro freezes one version, we want
   many + current; and the distro package is distro-specific (live proof in
   Roundcube: apt `roundcube` / pacman `roundcubemail` differ in layout →
   worked on Debian, not Arch). The official release is one path on every
   Linux (D-004). This is NOT "pull from git + build" but "signed/frozen
   release" — none of the composer/build-step fragility.
3. **Docker is NOT the base.** The shared-hosting model runs in-system
   directly (nginx vhost + per-site FPM pool); 100 sites = 100 containers of
   waste + port/network/volume chaos + loss of systemd integration. Docker
   may later be a KIND (a project type for a customer's own container, or a
   store option for an isolated/heavy stack) but never the substrate — in
   Plesk too it is an add-on, not the model.
4. **Building from source**: only when genuinely required (not in the distro
   AND no official binary — rare); then build tools are installed
   temporarily and removed after (clean-server).

**Honest limit (proven in the same slice).** The "one path on every Linux"
ideal is not free: even a portable tarball touches distro reality in its
SUB-dependencies — Roundcube's SQLite needed PHP's pdo_sqlite (a separate
package on each distro, and on Arch manually enabled), the FPM socket path
differed, the nginx conf layout was Debian-only (A10). The right architecture
is not pure but HYBRID: a portable body + distro-aware sub-details (package
name, socket path, extension enable) — and that distro-specific truth lives
in the agent; the panel/catalog knows no distro.

**Rejected alternatives.**
- **Build everything from source:** most things (PHP/scripts) don't compile;
  taking on CVE-patching for system services; build tools clutter the "clean
  server".
- **Put everything in Docker:** clashes with shared-hosting DNA; every
  systemd-based mechanism (status read, start/stop) breaks.
- **Install everything via distro package:** single-version prison for
  runtimes; distro-specific layout for portable apps (the Roundcube trap).
- **"Decide case by case" (no written rule):** the same debate on every new
  item; plus a class-slip risk (packaging an app like a system service).

---

## D-017 · Offering is universal: everything choosable — component, version, integration, product — flows through the chain in one offering-ID space

*July 24, 2026*

**Decision.** The operator's sentence reads like a statute: *"when a customer
picks an SSL provider, or a reseller decides which SSL providers their
customers may pick, they in fact move within the options WE offer them."*

1. **The chain's subject generalizes.** D-014's `installed ⊇ offered ⊇ used`
   applies not only to installed components but to EVERYTHING choosable —
   including integrations that install nothing on the server (SSL CAs;
   later Cloudflare, backup targets). To a customer, "which CA" is an
   offering exactly like "which PHP version".
2. **One offering-ID space.** Every offerable item carries a canonical id:
   `component:<id>` (whole) · `component:<id>:<version>` (D-014: the unit
   is the version) · `integration:acme:<id>` · `product:<id>` (D-012's
   `subscription_entitlements.product_id` is this space's first resident).
   There is NO separate table/model per offering type — that would be the
   "two owners of one fact" disease at the offering layer.
3. **Three layers, three questions; effective set = intersection.**
   *Available* (admin): installed components + registry entries left ON
   server-wide — the admin must be able to disable a CA panel-wide.
   *Offered* (reseller): plan contents — D-014's "missing link" is born
   here as `plan_offerings` (plan ↔ offering-id).
   *Used* (customer): the choice at use time (version per site, CA per
   certificate).
4. **One enforcement pattern at every chooser.** Listing endpoints (ssl
   providers, php versions, node versions…) filter to the caller's
   effective set; acting endpoints re-verify server-side and refuse with a
   coded error (the `NOT_OFFERED` family). The UI draws only what is
   offered — the general form of D-012 #7 ("visibility follows the right").
5. **Generous by default, narrowing deliberate.** A plan that declares no
   restriction offers everything (today's behavior, unchanged). The
   mechanism breaks nothing retroactively; a reseller narrows WHEN they
   choose to — D-014's "right to simplify" made real.
6. **Timing honesty.** The full mechanism ships with the v0.3 tenant/plan
   slice (a behaviorless skeleton without a plan UI would be theater). The
   one obligation effective today: every NEW chooser endpoint is written to
   this pattern, and offering ids are named canonically from now on.

**Why.** Three choosers were born in two days (PHP version, Node version,
SSL CA) and all three had no answer to "who narrows this list?". The
operator saw the answer must be the SAME for all three. Three separate
narrowing mechanisms would mean maintaining three authorization models.

**Rejected alternatives.**
- **Keeping integrations outside the chain** ("nothing gets installed"): to
  the customer a CA pick is an offering; excluding it births a second
  authorization model.
- **A table per offering type:** one space, one offering ledger; the D-012
  lesson.
- **Full implementation now:** a skeleton that changes no behavior while
  the plan UI does not exist — a recorded decision beats half-built code.

---

## D-016 · The page is named Components; the glossary is pinned; the service definition is refined

*July 23, 2026 — 5-consultant panel (market, TR-UX, information architecture, devil's advocate, global/localization) + judge synthesis; operator approved*

**Decision.**
1. **The page is renamed NOW:** EN **Components** · TR **Bileşenler**. The
   `/services` route stays. Four of five consultants independently concluded
   the current name is already false (industry-wide, Services = daemons; the
   page holds PHP, phpMyAdmin, nftables) and the rename cost only grows as
   users and docs accumulate.
2. **Glossary** (the store is born into these words; the umbrella is never
   re-litigated): umbrella **component/bileşen** · kinds: **Service/Servis** ·
   **Runtime/Çalışma Ortamı** (short badge form "Ortam") · **Tool/Araç** ·
   future **Module/Modül**, **Task/Görev**, **Integration/Entegrasyon** ·
   store: EN **Store**, TR **Mağaza** (page title "Bileşen Mağazası").
   "Service" lives on as the default kind filter — support language ("restart
   the service") keeps working.
3. **The service definition is refined** (the operator's "start/stop-able =
   service" test is directionally right, not literal): *a service is a
   component that owns a resident process whose being down is an incident in
   itself.* Two counterexamples from our own catalog: nftables accepts
   start/stop via a oneshot unit yet leaves no resident process (correctly a
   tool; oneshots deserve an "applied/not applied" state, never a running
   dot), and php-fpm ships real daemons yet is a runtime because its primary
   job is per-site version choice. "Has a controllable unit" is an ORTHOGONAL
   capability, not a kind: start/stop buttons draw from that capability on
   any row, including inside the version drawer.
4. **Recorded dissent** (the global consultant, stated fairly): no major
   panel uses "Components" in its nav (cPanel's umbrella is "Software",
   aaPanel's is "App Store"); Components is the most abstract candidate for
   ESL-heavy growth markets and collides with Joomla's
   Components/Modules/Plugins. Its alternative was "Software". The judge
   rejected it because "Software" goes false on a known schedule — the day
   the integration kind (a Cloudflare connection, not software on the
   server) is born, the exact disease that broke "Services" — and it offers
   no countable row/doc noun. IF findability or ticket-language problems
   surface, the fallback candidate is Software; this sentence exists for
   that day.

**Why now.** The item count only grows (module/task/integration are mapped,
the store is coming); letting habit and docs pile onto a wrong name does
nothing but make the correction day more expensive. That was the devil's
advocate's own collapse condition: "I concede once non-service rows
multiply" — the roadmap promises exactly that.

---

## D-015 · An extension is a package, a kind is a nature: the store grants but never installs, the core catalog's boundary is responsibility not price, and the kind list is open

*July 23, 2026*

**Decision.** Six linked decisions, prompted by the operator's car analogy
(standard vs optional equipment, engine swap) and the consultant's breakdown
of the Plesk/cPanel extension world:

1. **The two axes are final and never mix.** An *item/extension* is the unit
   of acquisition and distribution (free/paid, first/third party — D-012's
   axis). A *kind* is the mechanical nature of what gets installed
   (service/runtime/tool — D-010's axis). The consultant's summary IS the
   model: *"extensions usually manage services; they are not services
   themselves."* One extension may bring 0..N parts (Plesk Email Security:
   one extension → manages postfix+dovecot+clamav); every part shows up
   under ITS OWN kind — the extension itself is not a kind.
2. **The store grants, it never installs.** Purchase / license key / seat
   pool live in the store; an acquired item appears in the Services
   catalogue and is installed from there. The catalogue is the single
   install address — a second Install button would repeat the two-address
   mistake B3b just deleted (AdminNodeInstall).
3. **The core-catalogue/store boundary is RESPONSIBILITY, not price.** The
   core catalogue is the small curated set the panel vouches for (package
   names on both distro families, firewall ports, uninstall, tests — all
   our debt). Rarely-used FREE items (a niche database, say) would mean
   bloat plus a warranty we cannot honor — they come through the store
   too. (This corrects the Jul 22 statement "free items never enter the
   store": the criterion is the warranty boundary, not the price tag.)
4. **The kind list is open, but a kind is born with its FIRST item.** Three
   kinds carry three real item families today. The remaining shapes in the
   consultant's breakdown map cleanly and join as explicit kinds when their
   day comes: *module* (lives inside a host service, no unit of its own —
   ModSecurity, Brotli; needs a "host service" field) · *task/timer* (runs
   and exits — backups, certificate renewal) · *integration* (nothing
   installed on the server; its state is "connected?", its secret a sealed
   API key — Cloudflare, S3 backup targets, UptimeRobot). An *agent* is NOT
   a separate kind: mechanically it is a service (you start/stop the
   Imunify/Acronis agent); the difference is commercial.
5. **A compiled language needs nothing installed.** Go/Rust "support"
   cannot be a catalogue item because the multiplication test (D-011) says
   N: the runtime ships INSIDE the customer's binary. App mode (reverse
   proxy + unit) already runs any binary today — Go/Rust support exists in
   fact, no row needed. Interpreted languages (Python, Ruby) are Node's
   class: runtime items when their day comes. (If "pull from Git + build
   on server" ever ships, the toolchain becomes a *tool* item — if.)
6. **Two pages, two questions.** Services (possibly renamed "Components"
   later) = "what is on my server and how is it doing?" Store = "what can
   I acquire, from whom, under which right?" The same product may appear
   on both; that is two moments, not a contradiction.

**Why.** The operator stress-tested the model with three concrete questions
(where do rare-free items go; what about Go/Rust; the consultant counts 7
working shapes) and the model answered all three with its existing axes —
only the core/store boundary had been justified wrongly (price instead of
responsibility). The consultant's "verify on a real server" section
(list-unit-files, ss -lntup) is exactly what the scan already does —
recorded as external validation.

**Addendum (Jul 24 — on the operator's boundary-keeping): the core's measure.**
The operator asked: "wasn't everything except vital services supposed to sit
in the store first and enter the catalog on demand? won't hundreds of
programs in Components be chaos?" They remember correctly, and the boundary
now gets a number:
- **Core = one default + 1-2 named alternatives per role.** (web:
  nginx+Apache · DNS: PowerDNS+BIND · SMTP: Postfix+Exim · spam:
  SpamAssassin+Rspamd · webmail: Roundcube …) Target: TENS of items; never
  hundreds. The Jul 23-24 additions (rspamd, exim) are of this class — seat
  members, not long tail.
- **The long tail lives in the store** (free items acquired with one click
  and then appearing in the catalog — clause 2 already said so; the size
  discipline is now explicit). Large groupware like SOGo does NOT enter the
  core and waits for the store; netdata is borderline (the monitoring role
  is covered by the panel's own page) and is a candidate to move there.
- Today's chaos brake stays on record too: the default view shows installed
  items only (D-010, installed-first); the catalog sits behind a button.
  But the scale insurance is the store gate, not the view.

**Rejected alternatives.**
- **Making the consultant's 7 shapes 7 kinds today:** with zero module,
  timer or integration ITEMS on hand the schema would be speculative; a
  kind is born with its first item, otherwise we recreate the inverse of
  the problem D-010 solved (empty categories).
- **A "support row" for Go/Rust:** conceptual error — the same "a Go row
  would be a category mistake" already rejected in D-011; where there is
  nothing to install, a row lies.
- **Drawing the boundary at free/paid:** a rare free item is a warranty
  burden in the core but finds natural distribution in the store; price is
  the wrong axis.
- **A page per kind:** would split "everything installed, one list" across
  seven pages; kind changes how a row is DRAWN, not where it lives.

---

## D-014 · The authority chain is a narrowing filter: admin installs, reseller offers, customer uses — and the unit is the version

*July 21, 2026*

**Decision.** Who gets a capability is settled by three separate decisions, and
each layer can only **narrow**, never widen:

    installed (admin)  ⊇  offered (reseller)  ⊇  used (customer)

- **Admin** decides one thing only: does this service/extension **exist on the
  server**. Having installed it does not mean having given it to anyone.
- **Reseller** decides one thing only: which of the admin's installed items do I
  **offer to my customers**.
- **Customer** decides one thing only: which of the reseller's offered items do I
  **use** — and that decision is per site, not per account.

**The unit of the chain is the version, not the service.** "I offer PHP" means
nothing; a reseller offers **PHP 8.3** or does not, and a customer picks a
version per site. This does not contradict D-010; it adds a second axis to it:
in the catalog the **display** unit is the runtime (one row, versions in a
drawer), while the **entitlement** unit is the version itself.

**Settings run through the same chain**, not just presence: the admin sets the
server default and the ceiling, the reseller decides which knobs a customer may
touch, and the customer adjusts within those bounds.

**Why.** The operator's concrete case: the admin installed only PHP 8.4; a site
owner may need 8.3 and will never touch 8.4. A single "is it installed" flag
cannot answer those three different questions at once. Today the code has one
grant layer (`hasEntitlement`: does the subscription hold the product); there is
no admin→reseller vs reseller→customer distinction, so a reseller's right to say
"I do not offer that" cannot be represented.

**Where the layers live in code** (two of the three already exist):
- Admin layer = the installed set (`InstalledServiceIDs` + catalog). ✅ exists
- **Reseller layer = service plans** (`plan_handlers.go`, `004_accounts.sql`).
  No new structure is needed: "I offer it" is the plan's content — *"this plan
  includes PHP 8.2 and 8.3, not 8.4"*. Plesk does the same (Service Plans).
  ⬜ this is the missing link
- Customer layer = the per-site choice (`site.php_version`, `CreatePool(siteID,
  username, phpVersion)`). ✅ exists

**Consequences.**
- **Removal is a chain-wide event.** If an admin wants to delete a version, the
  plans offering it and the sites using it must block that. D-010's
  `RUNTIME_IN_USE` refusal must not stop at "in use" — it must count *who*
  blocks (how many plans, how many sites), or the admin decides without seeing
  what they are about to break.
- **Server-wide PHP settings are incompatible with this model.** Today's PHP-FPM
  page keeps extension toggles and php.ini values server-wide:
  `disable_functions` is a security control, so one customer's need becomes
  everyone's policy; a server-wide `memory_limit` lets one heavy site set the
  ceiling for all; `ffi` cannot be on for one customer and off for another. The
  page should name itself the **server default**, and a per-site layer must
  exist alongside it. The plumbing is ready: every site already gets its own FPM
  pool, and a pool can carry its own `php_admin_value` directives.

**Precondition (honesty note).** For PHP this model is currently **vacuous**:
without the Sury repo the admin could not install a second version even if they
wanted to (see the D-002 correction, D-010). Debating "who offers what" while
there is only one thing to give is theory. So B3c (Sury + multi-version) moves
to the front of the queue — not as polish, but as what makes the chain mean
anything.

**Rejected alternatives.**
- **A separate "offering" table for the reseller layer**: plans already answer
  exactly this question; a second structure would give the same fact two owners
  (the very mistake B3 exists to close).
- **Auto-offering everything the admin installs**: least code and wrong — it
  erases the reseller's right to simplify (a reseller may deliberately offer
  only 8.3 to cut their support load).
- **Keeping the entitlement unit at service level** ("has PHP / has no PHP"):
  solves none of the operator's case; it would tell a customer who needs 8.3
  that they "have PHP" because 8.4 is installed.
- **Treating server-wide settings as sufficient**: in a multi-tenant panel that
  means imposing one customer's `disable_functions` need on everyone.

---

## D-013 · Creating a domain is not picking a runtime: PHP is a site toggle, not an installation decision

*July 21, 2026*

**Decision.** The "Add domain" screen stops asking **"what will run here?"**. It
asks one thing — purpose: **website · mail only · DNS only**. The runtime (PHP
on/off + version, Node/proxy mode, port, startup file) becomes a **setting on
the site** after creation: a property that can be flipped and flipped back.

A direct consequence: **`static` and `php` are not separate project types.**
They are the same thing with PHP off and on; the template difference is three
lines (does `index.php` count as an index · does an unmatched URL fall through
to `/index.php` or 404 · does `.php` go to fastcgi). `node`/`proxy` genuinely is
a separate mode — no document root is served, everything goes to the proxy.

**PHP-FPM stays optional** (operator's call, Jul 21). The constitution's "nothing
is installed at setup; what isn't installed is invisible" applies to PHP too: a
DNS-only or Node-only machine carries no PHP. When PHP is absent the toggle on
the site says so honestly and shows an admin the way to install it — but it does
that in its own place, **not on the creation screen**.

**Why.** The operator asked: "why are we forcing PHP on someone who will never
use it — isn't that unnecessary and confusing? Not everyone has to install
WordPress." They were right, and the diagnosis was: the screen advertised an
uninstalled capability with an orange call to action — the exact opposite of the
"not installed = invisible" rule we had just enforced on the Services page. It
made someone building a static site feel their server was deficient. The server
was not deficient; that person did not want PHP.

Plesk's real flow was examined (9 operator screenshots of a live install) and it
showed this: **Plesk's creation dialog is not a runtime picker.** Its first
screen asks how the content will arrive (blank page · upload files · WordPress ·
deploy from Git · Node.js · mail only). The second screen never mentions a
runtime. The third runs "Configuring PHP" as an **unconditional** step. PHP is
afterwards a checkbox on the site's settings page (version list + per-site
php.ini). The telling detail: **the "Node.js" option does not change creation at
all** — the form is identical to "Blank website", and when creation finishes Node
is **still not enabled**; you must press "Enable Node.js". So even in Plesk that
first screen is a routing menu; the real decision is made in the site's settings.

**We take Plesk's shape, not its bundling.** In Plesk the question never arises
because it ships PHP itself (`/opt/plesk/php/8.3`); PHP can never be "not
installed". For us it can. So "PHP on by default for every site" cannot be
copied directly — copying it would violate D-003 and the constitution.

**Side finding (closed).** This discussion surfaced a real hole: the static vhost
had no PHP handler but also no deny rule for `.php`, so nginx served the file as
`application/octet-stream` and the browser **downloaded the source**. Same family
as A6. Measured against live nginx and closed (`b1a6aac`). The lesson goes on
record: *"X does not run here" must not mean "X's source is published here".*

**Rejected alternatives.**
- **Make PHP-FPM part of the base install** (the Plesk model): it would end the
  question outright and match market expectation exactly, but it means giving up
  "not installed = invisible" for PHP. The operator chose optional.
- **Ask the operator at install time** ("will this server serve PHP?"): one
  decision per machine was tempting, but it adds a permanent branch to
  install.sh and strands anyone who changes their mind later.
- **One option per language** ("Node.js site", "Python site", "Go site"): to
  nginx and to the panel all three are the same thing — reverse proxy to
  `127.0.0.1:PORT` + a unit. Same reasoning as D-010's "a Go row would be a
  conceptual error". Application mode is **one** option; the runtime is a field
  inside it.
- **Soften or shorten the orange warning on the creation screen**: treats the
  symptom. The problem was not the warning's tone but that the question did not
  belong there.
- **Treat mismatch detection as sufficient on its own** (warn when a static site
  contains `.php`): a good feature and it will be built, but alone it would keep
  asking the wrong question and clean up afterwards. The question gets fixed
  first; detection is the safety net.

**Implementation order.** (1) ✅ the static `.php` leak is closed — security,
independent of the design debate · (2) Add Domain drops to "what is this for?",
the orange nag goes, copy moves from brands to behaviour ("WordPress, Laravel…"
goes) · (3) a PHP toggle + version on the site's settings; `static`/`php` merge
into one mode · (4) mismatch detection + one-click fix · (5) Application mode
(Node/proxy) becomes visible in post-creation settings — the backend
(`hosting_handlers.go`, the template's proxy branch, `HostingTypePanel`) is
already there; only the shared language is missing.

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

**Implementation corrections (Jul 21).** Writing the code sharpened two points
of this decision; both go on the record:

- **`Kind` decides CONTROL, not status.** "Row rendering branches on Kind" read,
  at first pass, as "a runtime has no running/stopped state either". Wrong:
  `php-fpm` has real units (`php8.3-fpm`…) and the scan aggregates them. A dead
  php-fpm breaks every PHP site; removing its alarm from the dashboard would be
  blindness. The right split is twofold: **only a `tool` is exempt from status**
  (it has no daemon of ours); **inline start/stop belongs to `service` alone** —
  a runtime has one unit per version, so a single "Stop" button would lie about
  "which PHP?"; its control lives in the version drawer. Field evidence:
  `nftables` (a tool) reads "inactive (dead)" while the firewall is on with 12
  rules loaded on both servers — the old behaviour raised a "1 service stopped"
  false alarm about a working firewall.
- **Catalog truth is never cached** (A7). The kind separation shipped as
  `kind:""` on the first attempt: `service_scan_cache` stored whole API
  responses, catalog fields included. The rule is now explicit: **the cache
  stores only what the scan discovered** (installed / unit up / versions /
  config files); name, description, icon, category, package names and `Kind` are
  joined from code on every read. This is the runtime counterpart of D-010's
  "the catalog stays single" — single ownership cannot coexist with a stored
  copy.

**Rejected alternatives.** A separate `/runtimes` page (a second screen = cPanel's
split) · a separate `/apps` page (the counter+list under the Node row suffices;
revisit past 30 apps) · a server-role/profile UI (the `Role` field enters the data
model now, but the filter is weighed only past 25 catalog items) · a batch-commit
planner (not written until the dependency chain runs deeper than 2 items) ·
**"just press Scan once after the kind separation"** (it would have papered over
A7 instead of fixing it: the bug was not specific to `kind` and would return on
every catalog edit).

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

**Addendum (July 26, 2026 — two-server authority).** DNS ownership is not one
machine-wide primary and one machine-wide secondary; PowerDNS assigns roles per
zone. Two CelikPanels in **Paired** mode enable both capabilities: a zone created
on either panel is `MASTER` there and an automatic secondary copy on the peer.
Thus both nameservers remain authoritative regardless of which panel created the
site. Both panels carry the same shared name pair; exactly one name resolves to
the local IP and the other to the peer IP. Glue is registered once under the
domain that owns those names; no child nameservers are created under customer
domains.

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

**Correction (Jul 21) — the second vendor exposed the single-vendor assumptions.**
This decision was written and implemented against exactly one repo (PGDG); when
Sury was added for PHP, three of its assumptions turned out to be PGDG-specific.
The general rule: *a mechanism validated against a single example mistakes what
is specific to that example for what is general.*

1. **"KeyURL is ASCII-armoured" was wrong.** The text above states it as fact
   ("The armoured key is used directly"). PGDG publishes armoured; **Sury
   publishes a binary keyring** (`0x99` = OpenPGP public-key packet, measured on
   the server) and has no `.asc` variant (418). `fetchArmoredKey` would have
   rejected it — the decision locked out the second vendor inside its own scope.
   Both forms are now accepted and the file is named for **what it contains**
   (`.asc`/`.gpg`); apt reads both directly, so no gpg dependency is pulled in.
   Acceptance is bounded by checking the packet tag (tag 6) — an HTML error page
   cannot become a trusted keyring.
2. **Version ordering read the trailing integer.** That yields 17 for
   `postgresql-17` and 0 for `php8.3-fpm` ("fpm" is not a number). Code that
   looked correct against one vendor left every PHP version tied, so the drawer's
   order was whatever apt-cache happened to print. It now reads `(major, minor)`.
3. **The "already installed" wall.** Install asked whether the SERVICE was
   present before looking at the version pick. Harmless for PostgreSQL (one
   cluster); fatal for PHP: with 8.4 present, a request for 8.3 was refused with
   "PHP-FPM is already installed". For a plain install the question is "is this
   service here"; for a version pick it is "is **this version** here" — this was
   what actually blocked D-014's chain.

**`VersionCompanions`** was also added: a version pick installs bare
(`php8.3-fpm` alone has no database driver, mbstring or curl), so the panel would
report success for a runtime that cannot serve a site. PGDG never needed this
because `postgresql-17` works alone — the fourth face of the same single-vendor
blindness. Companions are best-effort and anything skipped is **reported** (Sury
publishes no `php8.5-opcache`; a strict install would refuse PHP 8.5 entirely
over one optional extension).

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
- Proven, not claimed: as of this date `make freebsd-cross` compiles **both**
  panel and agent. One portability fix was needed (Statfs_t field types differ
  Linux vs BSD — explicit `uint64` casts in `cmd/panel/system_stats_disk_unix.go`) and is
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

*Amendment 3 (Aug 3, 2026):* Debian 13 joins Ubuntu 24.04 as an explicit apt
acceptance target. This does not declare an unrun test successful: a tagged,
prebuilt release must pass a clean Debian 13 installation through the exact
documented operator workflow before the release is accepted. Source checkouts,
developer-only bundles and post-install SSH fixes do not count as evidence.

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

*July 8, 2026 · amended July 28, 2026*

**Decision.** Production updates start only with
`sudo /bin/bash ./bootstrap-update.sh --normal`, or the one-time
`sudo /bin/bash ./bootstrap-update.sh --bootstrap-pre-ledger` transition. The
bootstrap exports the clean reviewed commit and publishes an immutable,
root-owned release below
`/var/backups/celikpanel/releases/<commit>-<nonce>/`; only that release's
`update.sh`, invoked with the explicit mode, may perform the update. A privileged
update never runs `git pull` and never executes an updater or installer from the
mutable checkout.

Every update first takes and verifies a rollback snapshot. Recovery uses only
the exact command and `VERIFIED_SNAPSHOT` printed by the update:
`sudo /bin/bash /var/backups/celikpanel/releases/<commit>-<nonce>/rollback.sh "$VERIFIED_SNAPSHOT"`.
A checkout `rollback.sh` or a rollback script from
another release is not a recovery path. The service-mutation ledger is never
created, edited, truncated, or migrated by hand; only the product's controlled
one-shot initializer may create it during the pre-ledger install flow.

**Why.** Customers on the box mean data must survive every update. Customer
data (SQLite DB, site files, mail, DNS, certs, DKIM keys) lives outside the
replaced paths (`bin/`, `web/`). After the snapshot, updates run schema
migrations offline through the staged panel's `--migrate-only` mode while both
coordinators are stopped; neither unit may restart before that step completes. The snapshot
(panel DB + binaries + unit files + source commit) makes "an update made it
worse" a recoverable event, not a disaster. Fix flow: reproduce on the dev
box (the VPS's mirror) → prove → push → stage and verify the immutable release
with [`bootstrap-update.sh`](../bootstrap-update.sh) on the VPS → use only the
printed root-trusted rollback command if needed. See
[`update.sh`](../update.sh) and [`rollback.sh`](../rollback.sh) for the internal
release and snapshot contracts.
