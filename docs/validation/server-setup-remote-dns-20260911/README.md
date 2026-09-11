# Remote DNS development acceptance record

*11 September 2026 · [Türkçe](README.tr.md)*

These are working-tree development results following the user's
[explicit implementation authorization](../../REMOTE-DNS-AUTHORIZATION.md).
They do not establish a release, production pairing or an installed-panel update.

## Frontend results

| Check | Result and exact scope |
|---|---|
| Full suite | `npm test`: 352 passed, 0 failed. This final run includes the correction that displays nameservers from fresh proof instead of the saved list. See [full log](web-full-tests.log). |
| Focused runtime suite | `node --test tests/remote-dns-ui-runtime.test.mjs tests/server-setup-runtime.test.mjs tests/external-dns-ui-runtime.test.mjs`: 29 passed, 0 failed. This final focused run includes fresh nameserver display and separately confirmed enrollment while an old pending identity is preserved. See [focused log](web-focused-tests.log). |
| Production build | `npm run build`: TypeScript, production build and bundle-budget checks passed after the nameserver correction. See [build log](web-build.log). |
| Browser | Four controlled production-build Chrome scenarios passed: Turkish/English at 1440/390 px. Each required explicit pairing and setup confirmation, sent one pairing request and one setup start, made no writes on initial entry, and had no runtime errors or horizontal overflow. The final replay after the fresh-nameserver correction exited successfully. The later admission of a separately confirmed enrollment alongside an old pending connection is covered by runtime regression; it does not change these four rendered paths. See [results](browser-results.json) and [fixture](browser-fixture.cjs). |

The runtime tests cover tenant isolation, no writes on mount, confirmation,
single-request admission, secret handling, exact pending-identity reconciliation,
fresh authority proof, reviewed connection identity, explicit revocation,
preserved-record conflict messages and unavailable clipboard recovery.

[UI source hashes](ui-source-sha256.json) identify current source and tests for
the final full frontend run and build. This manifest is not a complete backend
snapshot or a signed release manifest. The browser log retains its stated scope and is not relabelled as a later execution. `browser.log`
is retained as emitted and may be empty because success is recorded in JSON.

## Later DNS ordering guidance

A real two-host DNS fixture exposed an unclear sequence: primary final
verification needs the secondary, while the secondary first needs the primary's
DNS installation. The local DNS form now explains that the primary starts first,
the secondary starts after the primary's DNS installation step, and primary
final verification waits for the secondary. Both role selections show this
instruction; external DNS does not.

The subsequent setup runtime run passed 16/16 tests ([log](web-dns-pair-guidance-tests.log));
the production build and bundle budget passed ([log](web-dns-pair-guidance-build.log)).
[Changed UI source hashes](ui-dns-pair-guidance-sha256.json) identify this later
delta. The earlier 352-test full suite and four remote browser results retain
their original scope; they are not relabelled as executions after this copy
change. Completed DNS profile VM acceptance is recorded separately in the native profile record linked below.

## Backend results and source boundary

| Check | Result and exact scope |
|---|---|
| Initial full Go run | The clean-source `make test vet` attempt passed every Go package except the database migration line-ending invariant. Migration 042 contained CRLF; this single failure stopped make before vet. The failure is preserved in the [initial log](go-initial-test-failure.log) and [result](go-initial-result.json). It is not reported as a successful run. |
| Final affected packages and all vet | After LF normalization, the Certbot identity/metadata correction and bounded DNS fixture changes, a fresh checkout passed all tests in `cmd/panel` (138.353 s), `internal/db` (7.246 s), `cmd/agent` (12.850 s) and `internal/hostcmd` (1.040 s). `make vet` checked all packages and exited 0. See [final log](go-followup-test-vet.log) and [result](go-followup-result.json). Unchanged packages retain their evidence from the initial run; this was not a second all-package test run. |
| Remote regression and race checks | `TestRemoteDNS\|TestSetupDNS` passed in the [focused run](go-focused.log), then under `-race` (87.499 s, [log](go-focused-race.log)). The final local HTTPS protocol and invalid-record HTTP 400 regressions passed under `-race` (9.128 s, [log](go-final-race.log)). |
| Certbot process and protected metadata | The selected Certbot, panel/mail certificate, supervisor and durable call-graph regressions passed under `-race` in `cmd/agent` (9.947 s, [saved tool transcript](go-certbot-race.log)). The expression only compiled `internal/hostcmd`; its guards were exercised by the final whole-package run above. |
| DNS fixture seam | The fixed-resolver seam and opt-in DNS profile fixture compiled and passed their focused race selection ([log](go-dns-fixture-race.log)). The seam retains the normal production resolver defaults; this does not establish a complete DNS profile VM run. |

The initial source archive had 1,507 files and SHA-256
`324b7b518e8b6427c2690e1c350192e58d905eee0c08f1657428abc29c675e67`.
The final archive had 1,515 files and SHA-256
`81727ee46dda96c7b5051dc40a4dfb08fc3f2cabdfc07abef17fc9b7fba30ce7`.
The [initial metadata](source-snapshot-initial.json),
[initial manifest](source-manifest-initial.json),
[final metadata](source-snapshot.json), [final manifest](source-manifest.json)
and [15 changed source paths](source-changes.json) preserve the exact boundary.
Checks ran in separate clean Linux checkouts with Go 1.26.5, UID 0 and sanitized
Go environment settings. The source archive remains a local artifact; these
hashes are development evidence, not signed release manifests.

Independent review regressions now cover preservation of unrelated TXT records,
expired and lost-response pending cancellation, symmetric local/remote namespace
ownership, same-connection delete/recreate generation history, rejection of stale
deletion, and exact pending V3 publication recovery while new work remains gated.
Cancellation requires an exact authenticated receipt: a generic HTTP 401 does
not prove that no grant exists. An unresolved attempt remains recoverable while
a separately confirmed new enrollment may proceed.

`TestRemoteDNSHTTPSProtocolEnrollPublishAndRevoke` uses a real local TLS listener,
machine authentication and SQL publication/revocation, with test-only trust roots,
address routing and authority/publication fixtures. It is stronger than a browser
API fixture, but does not establish public DNS service or real-host pairing.

The Certbot fix sets UID/GID 0 only for the actual Certbot subprocess invoked by
the root agent, retaining process supervision and parent-death behavior. Explicit
managed reissuance can repair only validated fixed directories and the selected
lineage's directories from the known legacy group. It follows no symlinks, does
not recurse, and does not adopt old certificate/key files or unrelated lineages.
The protected certificate reader is unchanged. The process and metadata tests
do not establish public ACME issuance or timer-driven renewal.

## Later DNS/mail recovery validation

The subsequent common source archive contains 1,519 files, SHA-256
`31b1185c9699c131f51b026eeaef4ff81d52943f64e53b008f3563f59df9fa18`.
It differs from the 1,515-file post-Certbot snapshot above in
[15 paths](source-changes-recovery.json): seven Go files, four UI/test files and
four documentation files. The [snapshot metadata](source-snapshot-recovery.json)
and [manifest](source-manifest-recovery.json) retain that exact boundary.

| Check | Result and exact scope |
|---|---|
| Affected packages and all vet | In another clean Linux checkout, all `cmd/panel` tests passed (122.726 s), all `cmd/agent` tests passed (11.809 s), and vet for all packages exited 0. The database and host-command packages were unchanged from the preceding successful run. See [log](go-recovery-test-vet.log) and [result](go-recovery-result.json). |
| DNS child recovery | `TestSetupDNS\|TestServerSetupDNSUnproven` passed under `-race` (28.819 s, [log](go-dns-recovery-race.log)). The separate outer-execution admission-lock test passed under `-race` (3.468 s, [log](go-dns-outer-race.log)). |
| Public mail DNS identity | `TestMailDNS\|TestCertbot\|TestMailHost\|TestProtectedBuildCommit` passed under `-race` (4.506 s, [saved tool transcript](go-mail-dns-race.log)). |
| Final UI delta | All 17 setup runtime tests passed ([log](web-reconciliation-tests.log)); the production build and bundle budget passed ([log](web-reconciliation-build.log)). [UI hashes](ui-reconciliation-sha256.json) identify the local-DNS ordering and neutral receipt-reconciliation changes. Earlier full-suite/browser runs retain their original scope. |

The DNS recovery tests require an exact terminal child receipt and proven host
rollback before failed-plan admission can reopen. An unknown or applied child
keeps the outer execution running and the draft locked. A committed DNS result
whose panel-state save failed resumes without reinstalling the engine. These
rules prevent a lost reply from authorizing a conflicting new plan.

The mail probe reads PTR and A/AAAA directly from bounded TCP queries to fixed
public resolvers, so the local FQDN's `/etc/hosts` entry cannot replace public DNS
facts. It checks the transaction, question and terminal owner; its bounded CNAME
chain supports classless reverse delegation while rejecting cycles and
conflicting aliases. Tests use a real local DNS listener and the real OS
localhost entry, including mixed-case names, IPv6, delegated reverse aliases,
unrelated records and wrong addresses. This proves the probe's handling, not
public Internet mail delivery.

While `server_setup_reconciling` is running, the UI announces neutral progress:
the previous result is being verified and setup will continue. It retains the
operation identity, offers no competing start/review action, and does not send a
mutation merely to display this state. Terminal failure still uses an alert.

## Final BIND and shared DNS source validation

The final tested source archive contains 1,529 files, SHA-256
`a08aa34402fc576572532ca17003939c4ca7ee6a160e906dd87d8b1cc910a115`.
Its [manifest](source-manifest-bind.json) and [metadata](source-snapshot-bind.json)
are separate from the previous snapshots. The
[24 changed paths](source-changes-bind.json) relative to `31b1185c…` comprise
18 Go source/test files and six documentation files; frontend source is unchanged.

| Check | Result and exact scope |
|---|---|
| Final clean affected packages | All tests passed in `cmd/panel` (112.673 s), `cmd/agent` (11.566 s), `internal/binddns` (0.054 s) and `internal/dnswire` (0.005 s). All-package vet exited 0. [Log](go-bind-test-vet.log), [result](go-bind-result.json). |
| BIND secondary regression | The full `internal/binddns` race run passed (1.164 s, [log](go-bind-secondary-race.log)). The host options/recovery change also passed whole agent, BIND and host-command tests plus targeted vet before the subsequent shared-DNS extraction ([saved transcript](go-bind-host-tests.log)); this earlier run retains its exact scope. |
| Shared public DNS regression | The final selected public-wire/mail tests passed under `-race`: panel 3.514 s, agent 1.193 s and dnswire 1.018 s. Vet for the selected packages and host commands passed. [Saved transcript and selection](go-public-dns-wire-race.log). |

The corrected BIND secondary declares the catalog as an explicit secondary zone
and places its subscription inside the owned host options block. Existing
operator bytes outside the markers remain intact; foreign catalog policies are
refused even during the narrower four-directive adoption flow. Runtime and
completed-target recovery derive the same policy from the verified receipt.

Receipt `SecondaryConfigVersion=1` gives the corrected secondary different
immutable generation bytes and identity. Historical version-zero receipts remain
readable evidence but cannot satisfy current readiness. Rollback reconstructs
only the exact current or historical target generation from its durable journal;
it cannot choose a different policy from mutable settings or adopt edited bytes.

The common `dnswire` probe now serves both mail identity and initial nameserver
readiness. Nameserver readiness requires successful A and AAAA responses; valid
AAAA NODATA is allowed, but an AAAA timeout or SERVFAIL cannot be hidden by a
successful A result. Resolver targets are fixed literal public endpoints in
production; tests use real local DNS/TCP listeners. Hosts/NSS do not participate.
Native parser and paired-DNS VM results have separate records and must not be
inferred from these package tests.

The [native profile record](../server-setup-profiles-20260911/README.md) separately records three hosting profiles and the final two-VM DNS profile, including their exact candidates, failed attempts and read-only continuation. It does not change the source/test boundaries above.

These results do not prove public ACME issuance/renewal, Internet mail delivery,
every complete purpose profile, or any real-server enrollment. No installed panel
was updated. Real firewall/reboot and test-CA mail lifecycle results are recorded
[separately](../server-setup-firewall-20260911/README.md),
[with mail evidence](../server-setup-mail-20260911/README.md).
