# Server setup source and browser checks

*September 11, 2026 · [Türkçe](README.tr.md)*

These are development acceptance results, not a release or installed-panel update.
The tested source was a clean snapshot of tracked files plus the setup work's new
sources, excluding local historical/plugin artifacts. Snapshot SHA-256:
`fdbb0de27a0c4f4fd56e525df23c8ecfccaa10f0075fbe2fdb13d5fd1bd61459`.

- `make test vet`: all Go packages passed and vet exited successfully, using Go
  1.26.5 on Linux with the repository's sanitized toolchain environment.
- Race detector: `cmd/panel`, `cmd/agent`, and `internal/db` passed the focused
  `TestServerSetup|TestSetup|TestMailHostCertificate|TestSQLite` selection. This is
  not a claim that the entire repository was run with the race detector.
- `npm test`: 339 tests passed. `npm run build`: TypeScript, production build and
  bundle budget checks passed.
- Actual headless Chrome against the production web build passed ten controlled
  API-fixture scenarios: new setup in Turkish/English at 1440/390 px; legacy,
  ready, waiting, unavailable, customer and license-locked states. Initial reads
  performed no writes, customers and license-locked users did not read setup,
  and confirmed new setup issued one start request. No runtime errors or
  horizontal overflow occurred. Screenshots received a bounded visual review.

The corresponding logs and browser result JSON are in this directory. Real
firewall/reboot proof is recorded [separately](../server-setup-firewall-20260911/README.md).
Controlled browser data and unit tests do not prove public ACME issuance,
every purpose profile end to end, or the unimplemented remote DNS connector.
See [current implementation status](../../SERVER-SETUP-STATUS.md).

## Peer IPv6 message follow-up

After the snapshot above, setup gained a dedicated Turkish/English explanation
for `dns_peer_ipv6_unverified`. It reports a verification limitation without
claiming that the peer's AAAA record is wrong or changing DNS records.

The following completed successfully in the development session:

- `node --test tests/server-setup-runtime.test.mjs tests/external-dns-ui-runtime.test.mjs`: 17 passed, 0 failed, exit status 0.
- `npm run build`: TypeScript, Vite and bundle budget checks passed, exit status 0.

These two follow-up results were observed in command-tool output; separate full
transcript files were not captured. The `web-tests.log` and `web-build.log` files
in this directory belong to the earlier full run above. This note does not
relabel those files as evidence for the later message change.
