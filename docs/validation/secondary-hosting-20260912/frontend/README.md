# Frontend acceptance

The web suite passed all 391 tests. The production build passed TypeScript,
Vite and the bundle budget. Raw results are in [web-tests.log](web-tests.log)
and [build.log](build.log); the final reviewed web source hashes are in
[source-sha256.json](source-sha256.json).

Focused runtime coverage includes secondary hosting endpoint validation,
review without enrollment or installation, scoped connection selection,
explicit execution binding and reconciliation after a lost confirmation reply.

Local Chrome exercised the production bundle with mocked APIs in Turkish and
English at 1440 px and 390 px widths. All four cases passed. Each reviewed the
secondary hosting choice, checked the fixed primary endpoint, authorized a test
connection and continued the same execution. No setup-start request or external
network request was allowed. See [browser-results.json](browser-results.json).
This is frontend acceptance, not remote server proof. The native VM acceptance
is recorded separately in the parent directory.

Desktop and mobile screenshots were visually inspected. The first browser
attempt used an incorrect translated button label in its test driver; correcting
that driver and simplifying the scoped connection form produced four passing
cases. Screenshots are retained locally under `.tmp-secondary-browser/`.
After those cases, one backend renewal-error code was mapped to the existing
renewal message; the final web suite and production build also passed.

Commands from `web/`: `npm test` and `npm run build`. The browser driver used the
locally installed Chrome and Puppeteer because the in-app browser tools were
unavailable in this session. No installed CelikPanel instance was changed.
