# Wizard editing recovery - September 12, 2026

Source fix, locally verified; not yet released. No installed panel was updated
or configured.

## Cause and reproduction

The wizard kept its step, unsaved fields, review and confirmation in React state.
After a remount it chose Components whenever the saved draft was customized,
even if the administrator had reached Access and DNS or Review.

LicenseOnboarding checks every minute and on focus. Successful timely renewal
already preserves its children. A hard access expiry (including in a hidden
tab), failed verification or explicit license rejection unmounts management.
Once verified again, setup remounts. A full browser reload also exposes the same
missing editing recovery. We reproduced those boundaries locally; we did not
capture the user's live network requests to identify their precise trigger.

## Change

- Store the nonsecret editing draft and named step in sessionStorage, scoped by
  origin, browser tab and username, with the saved server revision.
- Restore only an editable state at the same server revision. Never overwrite a
  newer server draft from another session or a running/completed operation.
- Rebuild a recovered Review through the server plan endpoint after checking for
  an accepted operation. Do not cache a plan or confirmation. Blocked plans stay
  blocked; failed/stale review recovery returns to Access with retained fields.
- Clear editing recovery on execution/completion/manual exit. The existing
  durable operation marker and license enforcement remain authoritative.
- Unavailable browser storage does not block normal setup, but recovery then
  falls back to the server-saved draft. Closing the tab ends this editing cache.

## Verification

- `node --test web/tests/server-setup-runtime.test.mjs web/tests/license-onboarding-runtime.test.mjs`: 61 passed.
- `npm test` in web: 397 passed.
- `npm run build` in web: TypeScript, production build and bundle budgets passed.
- Local production bundle in Chrome, mocked API only: TR/EN at 1440/390 px.
  Tab switch/focus, unsaved Access reload, Access license lock/recovery, Review
  reload and Review license lock/recovery all passed. Review confirmation resets.
  Zero setup/start, publisher-binding or external network requests.
- Regression cases additionally cover changed revisions/users, malformed storage,
  unavailable storage, blocked/stale/offline review and operation precedence.
- Desktop Review and mobile Access screenshots inspected. Existing layout kept.

[Browser results](browser-results.json). Reproducible browser driver is retained
locally as `.tmp-setup-editor-browser.cjs`; screenshots and full logs are in
`.tmp-setup-editor-browser/` and `.tmp-setup-editor-*.log`.

## Deferred user discussion

Return to the DNS design discussion after this fix: Frankfurt BIND primary,
Boston PowerDNS secondary plus web/application/mail hosting. The user asked how
standard DNS transfer differs from the panel HTTPS management connection. This
navigation fix makes no DNS topology or live configuration change.
