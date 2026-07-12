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
| A2 | Customers/resellers saw the Databases page (nav ALL) while the whole `/api/v2/` prefix is admin-gated → permanently broken page for non-admins | `nav.ts:32` ↔ `middleware.go:141` | ✅ Stopgap closed (Jul 11): nav is admin-only. **Permanent fix depends on B1** |
| A3 | v2 endpoints are not tenant-safe: `subscriptionID := 1 // TODO: Get from auth` | `database_v2_handlers.go:52,106,235,507` | ✅ Closed (Jul 11): 6 hardcodes removed → `callerSubscriptionID`; 6 server-scoped ops (list/create/delete × db+user) verify ownership via `canAccessDBServer` (`database_v2_authz.go`). **Admin gate STILL stands** — lifting it needs role-splitting `handleCreateDatabaseV2Server` (registering an arbitrary host/port/root-password is not a customer action); that belongs to the v0.3 tenant work |
| A4 | DB server root password stored in plain text: `// TODO: Encrypt` | `database_v2_handlers.go:138` | ⬜ OPEN — needs a key-management decision (where is the key stored?); a separate focused commit, the remaining slice of B1 |
| A5 | `capabilities.mail_server` is BOOL while `dns_server` is a string — the type inconsistency produced a real product bug (dashboard claimed mail was installed) | `capabilities_handler.go:30` | ⬜ OPEN (frontend fixed; API consistency lands with B1) |

## B. Structural debt (the prescription — paid in order)

| # | Work | Why | Estimate | Status |
|---|---|---|---|---|
| B0 | **Stop the bleeding**: A1+A2 fixes, bury dead code | The first customer must not meet a broken page | 1-2 days | ✅ Jul 11 |
| B1 | **One API**: fold v2 into v1; tenant scope from auth; generate OpenAPI; frontend types from the generated client | Kills the A3+A5 class at compile time; collapses 74 raw `fetch(` calls into one layer; makes "API-first" true | 3-5 days | 🔶 Partial (Jul 11): A3 tenant scoping DONE. Remaining sub-slices: A4 password encryption · server-registration role split + lift the admin gate · v2→v1 path merge · OpenAPI + generated client |
| B2 | **Route+authz table**: one `{path, handler, roles}` structure | 72 hand `HandleFunc` lines in `main.go` + the hand list at `middleware.go:117-141` = a forgotten line is a silent authz hole | 1 day | ⬜ |
| B3 | **Knowledge in one place**: the service catalog owns config paths/ports/packages; the scanner reads the catalog | The `managed_services.go` ↔ `service_scanner.go:93` duplication provably missed (pdns config, Jul 10) | 1 day | ⬜ |
| B4 | **UI discipline**: one Button (CtrlButton/ActionIcon die), shared `fmtBytes` (5+ copies), `confirm()` → themed modal (8+ sites), one Service type (the `api.ts:13` / `ServiceList.tsx:8` / `Dashboard.tsx:28` triplets) | Inconsistency leaks to users; copies rot independently | 2 days | ⬜ |
| B5 | **The honesty debt**: golden-path smoke CI (build + stub screenshots + critical endpoints) | 9 test files against 29k lines, NO CI; the constitution's own rule is in violation | 1 day to start, then continuous | 🔶 Floor laid (Jul 11): `.github/workflows/ci.yml` — every push/PR runs go build+vet+test + web tsc+build. First seed test `database_v2_driver_test.go` (A1 regression guard). Remaining: stub render + critical-endpoint smoke + <100ms measurement |

**Hard ordering constraint:** v0.3 (first real tenants) MUST NOT start before B1 is done.

## C. Buried, and remaining smells

- ✅ Buried (Jul 11): `internal/repositories/database_repository.go` (zero-reference dead code), root-level `KONUSMA-GECMISI.md` (chat dump; history lives in git), on-disk `ServiceList.tsx.backup`.
- ⬜ Smell: `cmd/panel` is one flat 66-file package (13,658 lines) — it splits naturally during B1/B2; do NOT run a separate "great repackaging" (churn outweighs gain).
- ⬜ Smell: `docs/CelikPanel Pano.html`, an 813KB blob — kept deliberately as design reference; move to LFS/link if it grows.
- ⬜ Smell: `en.ts` with 900+ keys in one file, and **an apostrophe in a string breaks the build** — a contributor trap; at minimum a documented lint during B4.
- ⬜ Smell: `cmd/debug_mariadb/main.go` — a debug binary referenced from nowhere (`go build ./...` still compiles it). Dead; a burial candidate (found in the Jul 11 sweep).
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
