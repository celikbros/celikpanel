# Native DNS independence validation

*September 12, 2026 · [Türkçe](README.tr.md)*

Source validation, not an installed-panel update or release claim.

- Web: all 399 tests passed; production TypeScript/Vite build and bundle budgets passed.
- Backend: full `cmd/panel` and `internal/binddns` suites passed with Go 1.26.5.
  Focused setup/DNS tests and race checks also passed. Manual secondary tests
  cover both engines and web, web/mail, application and custom purposes.
- Browser: production bundle served only on localhost with intercepted API
  fixtures; Turkish/English at 1440px and 390px. Manual secondary needs no
  endpoint; switching to automatic exposes it; switching back clears unused
  invalid endpoint input on save. Tab switch, reload and license remount retain
  editor state; review is re-fetched and confirmation is cleared. No host setup
  or remote enrollment was started. See `browser-results.json`.
- Native DNS: two disposable Debian 13 QEMU clones from the prior Alpha71 test
  fixture. BIND primary `192.0.2.10`, PowerDNS secondary `192.0.2.20`. Both panel
  and agent services stopped; standard management executable paths absent.
  Primary changed to owner-written `named.conf.local` and native zone/catalog
  files. Addition, A record change, DNS daemon restarts and catalog member
  removal propagated without either management service. See `native-proof.json`.

`native-probe.py` preserves the fixture-specific probe. It targets only the
named local QEMU fixture root, loopback SSH forwards and existing pinned host
keys; it is not a production configuration script. It assumes the recorded
clones and prior management-stop preparation. Both disposable guests were
stopped through QMP after the test; backing images were retained unchanged.
See `shutdown-native.json`.

Limits: the native test exercises DNS, not complete hosting removal or whole
host reboot. Firewall boot restore and mail certificate deployment retain
agent dependencies recorded in [the audit](../../OWNER-INDEPENDENCE.md).