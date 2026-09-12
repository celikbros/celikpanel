# Backend release verification — Go 1.26.5 — 2026-09-12

These local backend checks use the exact Go 1.26.5 toolchain required for the
release. They cover secondary DNS plus hosting, including the Nginx-before-panel-
certificate ordering fix and the transactional publishing-default guard against
concurrent plan revision. Installed user panels are not accessed by these tests.
Native disposable-VM evidence is recorded separately in the parent directory.

Verified executable:

```text
/root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.5.linux-amd64/bin/go
```

The executable reported `go version go1.26.5 linux/amd64`. Its `go env GOVERSION`
reported `go1.26.5`; GOROOT, GOOS and GOARCH are captured in
[go-toolchain.txt](go-toolchain.txt). Every command below sets `GOTOOLCHAIN=local`
to prevent automatic toolchain substitution.

| Check | Exit status | Test-reported duration | Captured output |
| --- | --- | --- | --- |
| Setup and DNS regressions | 0 | 8.138s | [setup-dns-tests.log](setup-dns-tests.log) |
| Complete panel backend package | 0 | 123.375s | [panel-full-tests.log](panel-full-tests.log) |
| Selected publisher concurrency and recovery with race detection | 0 | 15.618s | [publisher-race-tests.log](publisher-race-tests.log) |

Exact commands executed from the repository root inside WSL:

```sh
GOTOOLCHAIN=local /root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.5.linux-amd64/bin/go test ./cmd/panel -run 'TestServerSetup|TestSetupDNS' -count=1
GOTOOLCHAIN=local /root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.5.linux-amd64/bin/go test ./cmd/panel -count=1
GOTOOLCHAIN=local /root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.5.linux-amd64/bin/go test -race ./cmd/panel -run 'TestServerSetupPublisher(CannotChangeDNSDefaultAfterConcurrentRevision|RecoveryRevisesOnlyCompletedBootstrap)$|TestServerSetupSecondaryHosting(WaitsBindsAndResumesSameExecution|RechecksRevocationAfterBinding)$' -count=1
```

The race-detector run selects these four tests and their subtests:

- `TestServerSetupPublisherCannotChangeDNSDefaultAfterConcurrentRevision`
- `TestServerSetupPublisherRecoveryRevisesOnlyCompletedBootstrap`
- `TestServerSetupSecondaryHostingWaitsBindsAndResumesSameExecution`
- `TestServerSetupSecondaryHostingRechecksRevocationAfterBinding`

Earlier tests used the default WSL Go 1.27 development toolchain. Those outputs
are preserved in [development-go1.27](development-go1.27/README.md) for traceability
and do not constitute release validation. Later independent code changes require
their own relevant checks; these results describe the source present during each
recorded run.

