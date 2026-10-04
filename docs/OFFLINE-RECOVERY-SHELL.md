# Browser recovery during a panel connection outage

D-025 invariants 2, 3, 5, 6 / P0.2; additive browser cache format v1.
No server observation schema, session authority, license policy, recovery runtime
or native service ownership changes. No installed owner server was updated.

After a successful trusted visit, the browser prepares a small standalone recovery
page. A network failure, a navigation deadline or a server 5xx on a recognized
panel route opens that page at the same address. It retains the existing browser
operation ID **as an unverified reference only**. It does not infer an update
outcome from the browser marker or display private API responses without current
authentication. It explains who can act, offers two read-only owner SSH commands
and checks the existing availability endpoint every 15 seconds while visible.
A response offers an explicit return to the panel; it does not restart work,
change update locks, auto-reload in a loop or authorize management.

This is automatic **offline guidance**, not authenticated live server status while
the Panel process is stopped. The existing independent owner CLI/SSH view supplies
that status. A new browser, cleared/evicted storage, forced bypass reload, an
unsupported browser or an untrusted TLS connection may have no saved shell.
[Service-worker registration requires a secure context](https://developer.mozilla.org/en-US/docs/Web/API/ServiceWorkerContainer/register).
This does not resolve IP certificate mismatch or make unavailable authentication
material usable. The wider P0.2 automatic live recovery surface remains open.

## Cache and compatibility boundaries

The production build derives the exact public resource graph from the Vite
manifest, caps it at 64 KiB and embeds content hashes, sizes and content types in
the worker. Every resource is fetched without credentials and checked before the
new shell can activate. A partial/mixed build cannot replace the selected shell.
Only that shell's namespaced caches are retired; unrelated caches are preserved.
The normal application HTML, APIs, sessions, license data and operation responses
are never cached. Unknown routes, hosted proxies, non-navigation application
fetches and every non-GET request bypass the navigation fallback. Deliberate
401/403/404/429 responses and redirects retain their original meaning.

Registration is optional and bounded; unsupported/denied registration does not
change update admission. Reviewed update dispatch briefly waits for the same
preparation without relaxing its existing authorization and cross-tab guards.
An existing different root worker is not replaced. Missing reserved files return
404 rather than the SPA, including on supported rollback paths. A previously
installed worker always attempts the network first, so it cannot hold a restored
older application behind a cached version. The offline shell has no mutation
control or persisted credential. Its CSP disallows forms and foreign resources.

## Evidence and remaining acceptance

- Twelve executed worker/registration tests cover immutable resource admission,
  corrupt/mixed responses, cache namespace retention, offline/5xx navigation,
  deliberate refusal, API/proxy/mutation bypass, missing cache and bounded denial.
- 480 web tests pass. Production build passes the unchanged main bundle budgets;
  the separate static shell is approximately 12 KiB.
- Race-enabled Go frontend tests verify JavaScript MIME/revalidation and missing
  reserved-file refusal.
- `web/scripts/test-recovery-browser.mjs` launches an isolated real headless Chrome
  and a loopback-only fixture. It stops the actual HTTP listener, reloads the same
  settings URL, checks EN desktop/TR mobile, exact ID retention, forged-result
  rejection, unchanged storage, API exclusion and no overflow. A real subsequent
  401 response permits returning to the ordinary UI. No mutation or page exception
  occurred. The API auth response is a fixture, not a native installed update.
- The first browser harness attempt stopped before its fault because a CDP
  expression lacked an async wrapper; it is not counted as acceptance. The
  corrected run passed. Screenshots were inspected together.

Normal-address independent authenticated **live** observations, fresh-browser
recovery, the native update/interrupted-restore browser matrix and complete P0.2
acceptance remain open. DNS native evidence and this browser evidence cover
separate boundaries and must not be combined into a full resilience claim.
