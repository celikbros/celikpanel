# Development-only backend checks — Go 1.27

These archived logs were produced with the default WSL toolchain,
`go1.27.0-X:nodwarf5`, before the release toolchain mismatch was identified.
They are retained for traceability and are not release validation evidence.
Use the parent directory's Go 1.26.5 results for release assessment.

| Development check | Exit status | Duration | Log |
| --- | --- | --- | --- |
| Setup and DNS regressions | 0 | 7.456s | setup-dns-tests.log |
| Full panel package | 0 | 145.654s | panel-full-tests.log |
| Selected publisher race tests | 0 | 15.389s | publisher-race-tests.log |

Commands run from the repository root inside WSL:

```sh
go test ./cmd/panel -run 'TestServerSetup|TestSetupDNS' -count=1
go test ./cmd/panel -count=1
go test -race ./cmd/panel -run 'TestServerSetupPublisher(CannotChangeDNSDefaultAfterConcurrentRevision|RecoveryRevisesOnlyCompletedBootstrap)$|TestServerSetupSecondaryHosting(WaitsBindsAndResumesSameExecution|RechecksRevocationAfterBinding)$' -count=1
```
