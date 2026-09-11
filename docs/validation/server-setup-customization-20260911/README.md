# Custom server setup validation — 11 September 2026

Scope: the approved [customization flow](../../SERVER-SETUP-CUSTOMIZATION.md).
Source implementation and local checks are complete. No release was published
and no installed CelikPanel instance was updated for this work.

## Backend

The `TestServerSetup` suite passed with Go 1.26.5 under WSL, including the race
detector. After the final Node guard and completion-conflict assertions, the
focused customization race suite passed again (14.214 seconds).

Reproduction commands, with the repository's Linux Go toolchain selected:

```sh
go test ./cmd/panel -run TestServerSetup -count=1
go test -race ./cmd/panel -run TestServerSetupCustomization -count=1
```

`cmd/panel/server_setup_components_test.go` covers:

- Frozen Alpha66 draft/plan serialization, canonical selection ordering and
  unchanged identity for legacy plans.
- Administrator-only, read-only catalogue discovery; host support, installed
  service conflicts, unknown inventory and separately observed Node versions.
- Database-tool dependencies in execution order, preserved installed services
  and firewall ports, and rejection of competing Redis/Valkey selections.
- Core Mail, Webmail and Spam-Protected Mail selection through existing audited
  mail-profile operations and matching completion receipts.
- Exact selected Node runtime version, missing/stopped dependencies, and
  installed conflicts rejected by completion as well as review.
- Explicit empty selection and DNS publisher requirements for selected services.

The existing setup suite continues to exercise admission, revision checks,
license behavior, durable operation recovery and readiness independently.

## Frontend

The complete frontend suite passed: **381 tests, zero failures**. The focused
setup/helper suite passed **42 tests**, with extra completion assertions added
inside the existing cases for components that implicitly require Nginx.

```sh
npm --prefix web test
npm --prefix web run build
```

TypeScript compilation, production Vite build and the bundle-budget check
passed. Runtime coverage includes catalogue failure/retry, explicit empty
versus omitted customization, saved selections, invalidation of a previous
review, installed/dependency labels, incompatible saved DNS choices, and
completion links based on the selected components. Incompatible DNS choices
remain visible with an explanation; no silent DNS rewrite is performed.

## Local browser checks

The production frontend was served on localhost with mocked APIs and inspected
using headless Chrome. **Eight scenarios passed**: Turkish and English, 1440px
and 390px viewport widths, each through both custom setup and preset
customization. Representative desktop and mobile screenshots were visually
inspected.

Checks exercised the fifth purpose choice, preset customization, keyboard Space
selection, removable Postfix/Dovecot grouping, required dependencies, conflicting
cache choices, preservation labels, and exact draft values saved through review.
Every scenario had zero page errors, horizontal overflow, external requests or
`/api/v1/setup/start` calls. These tests did not perform host operations.

Session artifacts are in `.tmp-custom-setup-browser/` (32 screenshots and
`browser-results.json`), with the local harness
`.tmp-custom-setup-browser.cjs`. Build and test logs are
`.tmp-custom-setup-build.log`, `.tmp-custom-setup-web-tests.log` and
`.tmp-setup-components-race.log`.

## Evidence boundary

The browser fixture deliberately uses a reduced catalogue and an unavailable
row to exercise disabled-choice rendering. It is not the production catalogue;
ClamAV is excluded from the initial backend selection scope.

The new combinations were tested through local regression fixtures, not a new
disposable-VM installation matrix. They reuse the already supported service
and mail operations. This report does not claim public-CA issuance, real DNS
delegation, mail delivery, or successful installation of every new combination
on Boston or Frankfurt. Existing database web tools are verified through the
available package and service evidence, without adding a new HTTP health probe.
