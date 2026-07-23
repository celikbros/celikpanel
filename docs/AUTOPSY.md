# Autopsy Report & Debt Ledger

*First audit: July 11, 2026 · [Türkçe](AUTOPSY.tr.md) · The status boxes are live — whoever closes one marks it with their commit.*

This is the durable form of a brutally honest audit of the codebase: what is
broken, duplicated, dead, and where the philosophy is violated — plus the
payment plan. The goal is not blame; it is that **an engineer taking over
knows where every body is buried on day one.** Method: no assumptions, every
finding cites file:line (as of Jul 11, 2026).

## Verdict

**Refactor, NOT rewrite.** The Panel/Agent/RPC spine is sound; the golden path
is proven end-to-end (our only real asset). The problems are not architectural:
missing seams, duplicated knowledge, absent tests. A rewrite throws away proven
behavior (the Netscape mistake).

## A. Breakages (live when found)

| # | Finding | Evidence | Status |
|---|---|---|---|
| A1 | v2 DB handlers expected `TypeID==23/24`; the seed is 1=postgresql, 2=mariadb — both branches dead, the driver received an empty type | `database_v2_handlers.go` (old 280/428), seed `001_full_schema.sql:205` | ✅ Closed (Jul 11): `GetByID` now JOINs the canonical type name; handlers use `dbDriverTypeFor()` |
| A2 | Customers/resellers saw the Databases page (nav ALL) while the whole `/api/v2/` prefix is admin-gated → permanently broken page for non-admins | `nav.ts:32` ↔ `middleware.go:141` | ✅ PERMANENTLY closed (Jul 17): blanket gate removed, endpoints tenant-scoped (14 guards), server registration stays admin, nav is admin/reseller/customer; a fresh customer sees an empty list (not 404) — verified live as the customer role |
| A3 | v2 endpoints are not tenant-safe: `subscriptionID := 1 // TODO: Get from auth` | `database_v2_handlers.go:52,106,235,507` | ✅ Closed (Jul 11): 6 hardcodes removed → `callerSubscriptionID`; 6 server-scoped ops (list/create/delete × db+user) verify ownership via `canAccessDBServer` (`database_v2_authz.go`). **Admin gate STILL stands** — lifting it needs role-splitting `handleCreateDatabaseV2Server` (registering an arbitrary host/port/root-password is not a customer action); that belongs to the v0.3 tenant work |
| A4 | DB server root password stored in plain text: `// TODO: Encrypt` | `database_v2_handlers.go:138` | ✅ Closed (Jul 16): `internal/secrets` — AES-256-GCM, key at `dataDir()/secret.key` (0600, generated on first boot). Sealed on create, opened via one helper (`dbDriverFor`) at driver time; legacy plaintext rows sealed by an idempotent startup migration. Format is `enc:v1:`-prefixed — self-describing |
| A5 | `capabilities.mail_server` is BOOL while `dns_server` is a string — the type inconsistency produced a real product bug (dashboard claimed mail was installed) | `capabilities_handler.go:30` | ⬜ OPEN (frontend fixed; API consistency lands with B1) |
| A8 | **A static site served PHP source as a download**: the static vhost has no PHP handler — correct; but it had no deny rule for `.php` either. nginx finds the file, does not recognise the extension (not in `mime.types`), falls back to `default_type application/octet-stream` and makes the browser **download the source** — database password included. "PHP does not run here" must not mean "PHP source is published here" | `internal/services/templates/nginx/vhost.conf.tmpl` (static branch) | ✅ Closed (Jul 21): deny on `\.(php\|phtml\|phps\|php[0-9])$`; 403 rather than 404 on purpose (it says the file exists but is not executed — the actual diagnosis). Measured end to end against live nginx: `secret.php` 403 with nginx's error page as the body, `.well-known/acme-challenge` 200 (renewals intact), `.env` 403, missing file 404. 2 regression tests. Same family as A6 — a file in the webroot handed out as source |
| A7 | **The scan cache stored catalog fields**: `service_scan_cache` held whole API responses (name, description, icon, category, package names) → every catalog field changed in code kept its old value on screen until someone pressed Scan, and a NEWLY added catalog service never appeared at all. A panel reading the truth of its own code out of a cache | `managed_service_handlers.go` (old 62-79, 232-243) | ✅ Closed (Jul 21): the cache stores only `serviceObservation` (installed / unit up / versions / config files); the catalog is joined on every read by `catalogView` — the ONE place a response is built, so a cached read and a fresh scan cannot answer differently. The legacy format is still read (correct state without waiting for a rescan). 3 guard tests. This shipped to production as `kind:""` — see note E |
| A6 | **The vhost dotfile rule was wrong in both branches**: the PHP branch blocked only `/\.ht` → `https://site/.env` served in plain text (a Laravel/Symfony DB password, APP_KEY, mail credentials) and `.git/` was readable; the static branch blocked ALL dotfiles → `.well-known/acme-challenge` was closed, so a static site's first certificate and every renewal 403'd | `internal/services/templates/nginx/vhost.conf.tmpl` | ✅ Closed (Jul 20): both branches now use `location ~ /\.(?!well-known).*`; the regex was validated against live nginx (commit `0088c67`). The ACME break stayed invisible because the golden path was proven with a PHP site |

| A9 | **PHP version switching was broken at two layers** (caught by B3d live verification, Jul 23): (1) the FPM-as-root fix's identity-forgery gate also locked migration — the 8.3→8.4 switch returned 500 (two callers with two intents in one function); (2) past the gate, the second layer appeared: the pool moved and the DB updated while the nginx vhost kept proxying to the DELETED old socket — the site answered 502 while everything reported success. The config-drift smell (section C) made flesh | `php_pool_manager.go` MigratePool, `domain_php_handlers.go` | ✅ Closed (Jul 23): migration writes the new file directly (identity from the OLD pool — written by the panel itself; one template producer, renderPool), the gate stays; the PHP switch now regenerates the vhost from the DB row via `applySiteVhost` (write→validate→rollback→reload) and a failed regen fails the request. Regression test + the live 8.3→8.4→remove-8.3 chain verified. Lesson: **whoever installs a gate must also run the legitimate paths behind it** — 45c9b2b's live verification never tried a version switch |

## B. Structural debt (the prescription — paid in order)

| # | Work | Why | Estimate | Status |
|---|---|---|---|---|
| B0 | **Stop the bleeding**: A1+A2 fixes, bury dead code | The first customer must not meet a broken page | 1-2 days | ✅ Jul 11 |
| B1 | **One API**: fold v2 into v1; tenant scope from auth; generate OpenAPI; frontend types from the generated client | Kills the A3+A5 class at compile time; collapses 74 raw `fetch(` calls into one layer; makes "API-first" true | 3-5 days | 🔶 Partial (Jul 18): A3 tenant scoping + A4 password encryption + role split/admin gate + v2→v1 merge + **the error contract** DONE. Contract {error,code?,action?}: 11 coded refusals, one writeCodedError writer; frontend readApiError as the one reader + ErrorBanner (action→in-panel button), i18n err.* TR+EN, 13 scattered res.text() unified. Live: WEB_SERVER_REQUIRED/ADMIN_ONLY/AUTH_REQUIRED with the right envelope+action on both servers. Remaining sub-slice: OpenAPI + generated client |
| B2 | **Route+authz table**: one `{path, handler, roles}` structure | 72 hand `HandleFunc` lines in `main.go` + the hand list at `middleware.go:117-141` = a forgotten line is a silent authz hole | 1 day | 🔶 Fail-closed roles pulled forward and closed (Jul 17: unreadable user = invalid session, no proceeding with an empty role). The table + role×endpoint matrix test remain open |
| B3 | **Knowledge in one place**: the service catalog owns config paths/ports/packages; the scanner reads the catalog | The `managed_services.go` ↔ `service_scanner.go:93` duplication provably missed (pdns config, Jul 10). Second proof (Jul 16, Arch): Hostinger's Arch image ships a disabled `named.service` → capabilities says `dns_server: "bind"`, the setup journey shows "Install a DNS server: Done", yet the Services page says 0/0 — two detection paths (InstalledServiceIDs ↔ GetServices) answer the same question differently; and an installed-but-stopped DNS counts as Done although it serves no zone | 1 day | 🔶 Partial (Jul 21): **kind separation** closed — every catalog entry declares its `Kind`, the `Daemonless = len(SystemNames)==0` heuristic is deleted, row rendering branches on kind; 2 guard tests. **Cache/catalog split** closed (A7): the catalog is the single owner, the cache stores observations only. **Sury** closed (B3c): side-by-side php8.x-fpm installs from the panel on Debian. **Version first-class** closed (B3b): one contract `Agent.ListServiceInstances` (php-fpm: Debian versioned units + the real version out of Arch's single unit; node: tarball trees + system PATH honestly `managed=false`), the `extractVersion` switch and the `"default"` sentinel deleted (3 backend + 3 frontend consumers), versions live in an in-row drawer (per-copy status + start/stop, size for node), **node in the catalog** (`Kind=runtime`, `Requires: web-server` — the until-now unwritten reverse-proxy rule made declarative), the drawer is the single address for installing node versions (`AdminNodeInstall` deleted), capabilities `php_versions` now from the agent (it returned empty on Arch and blocked PHP sites). **Deletion protection** closed (B3d, Jul 23): before anything is removed, one question — "who breaks if this goes?" — answered from the live DB (deliberately NO table: sites.php_version/runtime_version already hold the truth; the ledger is a query). `RUNTIME_IN_USE` (per version; blocking sites in `details`) and `SERVICE_HAS_DEPENDENTS` (per component: php→PHP sites, nginx→all non-dnsonly sites, pdns→all domains [the old uncoded guard, now coded], postfix→mailboxes+forwardings, mariadb/pg→v2+legacy tables) return 409; the envelope gained `details`, ErrorBanner renders the list, the uninstall dialog stays OPEN through a refusal. Per-version removal lives in the drawer (php: package+companions purged; node: tree removal, own endpoint) — no delete button ever shipped unguarded. The agent's whole-service uninstall now also strips pattern-matched units (uninstalling php-fpm used to leave php8.x installed and running). The node install box became the nodejs.org LTS list (3-4 named options); the "system interpreter" escape is closed (`RUNTIME_VERSION_REQUIRED`; legacy rows live until touched). Server-side rescan added after uninstall (install had it, uninstall didn't). Remaining: the scanner reading the catalog (unifying the two detection paths), journey honesty |
| B4 | **UI discipline**: one Button (CtrlButton/ActionIcon die), shared `fmtBytes` (5+ copies), `confirm()` → themed modal (8+ sites), one Service type (the `api.ts:13` / `ServiceList.tsx:8` / `Dashboard.tsx:28` triplets) | Inconsistency leaks to users; copies rot independently | 2 days | ⬜ |
| B5 | **The honesty debt**: golden-path smoke CI (build + stub screenshots + critical endpoints) | 9 test files against 29k lines, NO CI; the constitution's own rule is in violation | 1 day to start, then continuous | 🔶 Floor laid (Jul 11): `.github/workflows/ci.yml` — every push/PR runs go build+vet+test + web tsc+build. First seed test `database_v2_driver_test.go` (A1 regression guard). Remaining: stub render + critical-endpoint smoke + <100ms measurement |

**Hard ordering constraint:** v0.3 (first real tenants) MUST NOT start before B1 is done.

## C. Buried, and remaining smells

- ✅ Buried (Jul 11): `internal/repositories/database_repository.go` (zero-reference dead code), root-level `KONUSMA-GECMISI.md` (chat dump; history lives in git), on-disk `ServiceList.tsx.backup`.
- ⬜ Smell: **a template change does not reach existing vhosts** (config drift) — the A6 fix covers only NEWLY generated vhosts; existing sites keep the old rule. There is no "regenerate all vhosts" path, so every template change carrying a security fix leaves a silent hole.
- ⬜ Smell: `cmd/panel` is one flat 66-file package (13,658 lines) — it splits naturally during B1/B2; do NOT run a separate "great repackaging" (churn outweighs gain).
- ⬜ Smell: `docs/CelikPanel Pano.html`, an 813KB blob — kept deliberately as design reference; move to LFS/link if it grows.
- ⬜ Smell: `en.ts` with 900+ keys in one file, and **an apostrophe in a string breaks the build** — a contributor trap; at minimum a documented lint during B4.
- ⬜ Smell: `cmd/debug_mariadb/main.go` — a debug binary referenced from nowhere (`go build ./...` still compiles it). Dead; a burial candidate (found in the Jul 11 sweep).
- ⬜ Smell: **the Add-ons page lives on placeholder products** (operator, Jul 23: "nothing to pick and install, a nonsense page, even the design is terrible"). Its real function is granting rights (subscription_entitlements — features like VPN unlock by grant), but the products are mock-priced shells with no tie to the install/Components world. By decision (D-012/D-015) this page folds into the Store's "rights" layer when the Store is born; this criticism is design input for that work — not worth polishing before it.
- ⬜ Smell: **no DNS-software switch flow.** pdns↔bind share one seat (port 53) — the exclusion is right; but the B3d guard (rightly) blocks removing DNS while domains exist, so a server WITH domains now has no way to CHANGE DNS software at all. cPanel solves this by migrating zones (nameserver selection). A future "switch" flow: install new → sync zones → remove old. Recording it suffices until demand appears.
- ⬜ Smell: 128 `exec.Command` calls and 0 interfaces in the agent — the ROADMAP's BSD note ("the code is already written that way") is optimistic; the portability claim holds only at the RPC surface. If you write a new RPC: gather execs at the end of the function and actually practice the what/how split.

## D. Philosophy violations (constitution vs. code)

1. **"The honesty rule"** (no tests+security+docs = not done) — the most violated clause is itself: 9 test files / 789 lines, no CI; phases were still marked done. → B5.
2. **"One obvious way"** — two API versions, three button systems, two config-management styles. → B1+B4.
3. **"API-first"** — no OpenAPI; types hand-written three times. → B1.
4. **"Speed <100ms"** — not a single measurement exists. → add one measurement line to the B5 smoke.
5. **The D-009 tension** (panel = DNS authority): coherent and defensible, but it turns away the entire external-DNS customer base — re-weigh deliberately before v0.3 goes to market (even if the decision stands, refresh the record).

## E. Self-criticism (for the record)

Part of this debt is fresh and came from the desk that wrote this audit
(the Jul 10 design round): two of the three button systems, the 7 raw fetches
in Dashboard, and an unfaithful test stub that masked a real bug. The lesson is
baked into the header of `tools/dev-preview/preview-server.py`: *the stub
mimics the real schema, types included.*

**Jul 21 — I created A7 and nearly walked past it with my eyes shut.** In B3's
first slice I added `Kind` to all 19 catalog entries; local tests passed, the
build was clean, I deployed to both servers. Live, the API returned `kind:""` —
for every one of them. Right locally, empty in production. The cause was not
the field I added but the cache it happened to pass through.

Two lessons for the record:

1. **"Tests pass" ≠ "works in production".** My tests read the catalog
   directly; the production path went through the DB. No test crossed the
   layer in between. Without live verification this would have stayed a
   silently mis-drawn UI — exactly the class the operator means by "a month
   later, oh, we forgot this one".
2. **The easy answer was the wrong answer.** "Press Scan once and it fixes
   itself" was true and sufficient — for that moment. But the bug was not
   specific to `kind`: it would return on every catalog edit, and be waved
   away with the same sentence each time. The durable fix was to ask what the
   cache has a RIGHT to store: observations only. Caching a fact that lives in
   code is how code and screen drift apart.

**The recurring methodological error: validating against a single example.** The
same shape has now appeared three times, and each time it broke *after* we said
it worked:

| What was validated against one example | What stayed invisible |
|---|---|
| The golden path was proven with a **PHP site** | Static sites could never pass ACME validation (A6) — and the static branch leaked `.php` source (A8) |
| Vendor repos were proven with **PGDG only** | The armoured-key assumption, trailing-integer ordering, the "already installed" wall, and the need for companions — all four broke on PHP (D-007 correction) |
| Catalog fields were read with **fields that always existed** | Adding a new one (`Kind`) made the cache publish it empty (A7) |

The lesson is not "write more tests" — it is sharper: **validating a mechanism
against one example mistakes what is specific to that example for what is
general.** Until the second example arrives, you cannot know which assumption was
load-bearing. Working rule: *an abstraction (a template branch, a vendor repo, a
catalog field) is not "proven" until it has a second concrete instance*; and when
that second instance lands, every sentence written for the first must be re-read
— D-007's "the armoured key is used directly" became false in exactly that way.
