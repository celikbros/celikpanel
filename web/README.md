# CelikPanel Web

The CelikPanel frontend is a React and TypeScript application built with Vite.
Production assets are embedded in the CelikPanel release and served by the Go
panel on the same origin as the API.

## Requirements

- Exactly Node.js 24.18.0, the reviewed CI and release version.
- npm from that Node.js distribution.
- A checkout rooted at the matching CelikPanel backend commit.

Do not use the root of the repository as an npm project. Frontend dependencies
and the authoritative lockfile live in this directory.

## Install, test and build

From the repository root:

```bash
cd web
node --version
npm ci --no-audit --no-fund
npm test
npm run build
```

`node --version` must report `v24.18.0` for a reviewed CI/release-equivalent
build. `npm ci` consumes `web/package-lock.json`; do not replace it with an
unlocked install during review or release work.

The build runs TypeScript, Vite and the bundle-budget check. Its output is
`web/dist`. That directory is generated:

- do not edit generated files;
- do not review generated output as source authority; and
- rebuild it from the exact source and lockfile instead of copying an older
  directory between checkouts.

The repository-level equivalent is:

```bash
make web
```

## Development server

```bash
cd web
npm run dev
```

The frontend calls relative `/api/v1` paths and assumes the UI and API share
one origin. The tracked Vite configuration does not define a backend proxy, so
`npm run dev` alone serves the interface but cannot redirect API calls to a
different panel process.

For interactive work against a backend, use an authorized local same-origin
reverse proxy or an explicitly reviewed local-only Vite proxy. Do not hardcode
a server URL, credential, token or environment-specific API base into source,
and do not commit a developer's proxy target.

## Source structure

| Path | Responsibility |
|---|---|
| `src/auth` | Session identity and frontend access context |
| `src/components` | Pages, cards, dialogs and shared UI |
| `src/lib` | API helpers and product contracts used by the UI |
| `src/i18n` | English and Turkish message catalogs |
| `src/nav.ts` | Single role-aware navigation registry |
| `src/router*.ts*` | Route matching, history and provider boundaries |
| `src/theme*` | Shared theme tokens and provider |
| `tests` | Node-based UI and source contract tests |
| `scripts` | Build-time checks such as the bundle budget |

## Navigation and authorization

`src/nav.ts` is the single frontend registry for navigation visibility and
route access. Add or change a route there rather than creating a second
role-specific menu or shell.

Frontend gating is not authorization. Every backend endpoint must independently
enforce authentication, role, tenant and ownership rules. A hidden link cannot
replace a server-side denial.

## Internationalization

`src/i18n/en.ts` is the translation-key source and `tr.ts` must carry the
same complete key set. User-visible text belongs in the catalogs, not directly
in components. Update both languages in the same change.

## Tests

```bash
cd web
npm test
```

The suite under `web/tests` covers routing, tenant and role visibility, API
errors, operation locks, secret-handling UI, update recovery and other product
contracts. Add a focused failure-path test whenever one of those boundaries
changes. The complete repository gate remains the GitHub CI workflow; a local
frontend pass is not release evidence.

Dependency and audit policy is documented in
[WEB-DEPENDENCY-SECURITY.md](../docs/WEB-DEPENDENCY-SECURITY.md).
