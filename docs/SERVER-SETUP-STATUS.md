# Server setup implementation status

*Working-tree snapshot: 11 September 2026 · [Türkçe](SERVER-SETUP-STATUS.tr.md)*

**The approved source implementation and local validation are complete. This is not release evidence.**
The [approved setup plan](SERVER-SETUP-PLAN.md) remains the acceptance contract.
The changes described here are present in the working tree; this record does not
claim that they are published or installed on Boston, Frankfurt, or another host.
Installed-panel updates must be initiated by the user in CelikPanel, as required
by [AGENTS.md](../AGENTS.md), regardless of older deployment instructions.

A later working-tree extension supports [secondary DNS with hosting](SECONDARY-DNS-HOSTING.md)
through a scoped connection to the publishing primary. Its separate
[acceptance record](validation/secondary-hosting-20260912/README.md) records the tested
source and limitations; the historical evidence below remains dated 11 September.

## Operation guidance extension — 13 September 2026

The alpha.73 source candidate adds reviewed-plan progress context, role-specific
DNS next actions, and distinct guidance for waiting, failure and uncertain
results before the setup step list. Mandatory local DNS readiness checks finish
before bounded peer probes; missing peer proof never grants pair readiness.
Known service failures remain visible during state verification. Progress GETs
remain read-only and do not launch duplicate operations.

Validation for this extension: 421 frontend tests, production build/bundle checks,
focused panel/agent Go and race checks, and 28 Turkish/English desktop/mobile
browser scenarios. See the [alpha.73 notes](RELEASE-NOTES-v0.1.0-alpha.73.md) and
[product-wide guidance requirement](OPERATION-GUIDANCE.md). Arbitrary third-party
license adapters and a complete lifecycle-adapter audit are not included. This
addendum is source-validation evidence, not publication or installed-host proof;
the dated 11 September evidence below retains its original scope.

After a version change, CelikPanel rechecks recorded operations. Remaining steps in a plan bound to the previous build may require a fresh review; the update does not automatically repeat recorded installations.

## Implemented in the working tree

| Area | Current behavior and boundary |
|---|---|
| Entry and history | Confirmed fresh administrators enter `/setup`; incomplete setup resumes. Legacy installations preserve Dashboard access and receive an optional setup entry. Completed setup does not restart on activation or login. Tenant roles do not fetch setup APIs. |
| Review and execution | Revision-checked drafts, immutable reviewed plans, explicit confirmation, and durable server-side execution replace browser-driven sequential installs. Exact request and child identities support lost-response reconciliation and restart. A waiting plan can be edited only after its modifying steps finish. |
| Purpose profiles | Web, web-and-mail, application and DNS plans resolve existing supported operations. Web uses Nginx, PHP-FPM and MariaDB; mail adds existing webmail/protection profiles and mail TLS; application selects an official Node.js LTS release and an optional database. Web, application and web-and-mail completed on separate disposable Debian 13 VMs; the DNS profile completed on a separate two-VM authoritative pair. |
| Local DNS | First installation reuses managed engine/topology operations and preserves existing ownership. Primary readiness and secondary transfer readiness use separate evidence; a secondary does not acquire publishing permission. Independent authoritative redundancy remains required. |
| External DNS | A persisted management mode and per-domain ownership allow web hosting without a local authoritative engine. Domain and mail screens show provider records and live checks; local DNS mutation controls are unavailable for externally managed zones. Existing domains retain their ownership. |
| Public access | Completion requires a panel FQDN, trusted listener TLS, renewal evidence, healthy dependencies and firewall readiness. Mail adds host identity, TLS and delivery checks. A self-signed entry certificate is not completion evidence. The three hosting profiles passed real HTTP-01 issuance and timer renewal against isolated test ACME authorities. Public CA issuance remains unproven. |
| Workload preservation | Setup service and panel-certificate firewall children preserve the reviewed access ports, including after restart. New unreviewed workload ports require review. License loss pauses admission of new setup steps; committed children reconcile and existing workloads continue. |

The UI provides Turkish/English steps, review, progress, actionable failures and
a purpose-specific next action. Reading setup state does not install software or
change configuration. API readiness checks remain independent of the UI.

The reviewed paired-DNS identity currently records the peer IPv4 address only.
Initial setup cannot yet verify an IPv6 address published for the peer nameserver
and reports `dns_peer_ipv6_unverified`. This is a verification limitation; it
does not establish that the peer's AAAA record is wrong. The supported setup
choices are IPv4-only nameserver publication or external DNS. Setup does not
automatically remove existing AAAA records.

The root agent now starts the actual Certbot child with UID/GID 0 while
preserving supervision. Explicit managed reissuance repairs only validated fixed
directories and the selected lineage's directories from the known legacy group;
it does not recurse, follow symlinks or adopt old certificate/key files. The strict
protected reader remains unchanged. Process-level regression verifies this
correction; full real-profile and public ACME results remain separate evidence.

## Existing remote DNS: implemented and locally verified

The user explicitly authorized the connector and local tests on 11 September
2026; see the [authorization record](REMOTE-DNS-AUTHORIZATION.md). The earlier
automatic-review rejections are historical, not a pending permission request.

The working tree now includes administrative enrollment, saved connections,
revocation, authenticated receiver status/publication, immutable domain-to-
connection associations, and durable publication generations. The setup choice
and DNS settings controls are active in source. A saved connection marked ready
means it was paired; the wizard requires fresh authenticated authority proof,
then reviews the selected endpoint and nameservers before installation. Viewing
these screens does not pair hosts, create credentials, or publish records.
Codes remain in component memory and request bodies, never URLs or browser
persistent storage. Mail actions preview required delivery records and report
conflicts instead of silently replacing existing mail routing.

The [remote DNS security scope](REMOTE-DNS-SECURITY.md) remains the acceptance
contract. Independent review identified semantic TXT preservation, cancellation
of expired pending enrollment, and symmetric local/remote DNS namespace guards
as required regression cases. Those fixes, durable generation recovery and
authenticated local TLS publication/revocation now pass focused regression and
race checks. An unresolved cancellation keeps its saved identity; a generic
HTTP 401 is not proof that no grant exists. This implementation status does not
establish production pairing, a released capability, or a real-host profile using
remote DNS. The hosting profile results below use controlled external DNS.

## Verification evidence and scope limits

| Check | Evidence at this snapshot |
|---|---|
| Frontend suite and build | The latest full frontend run passed 352/352 tests after remote DNS UI activation and the fresh-nameserver display correction. The production build and bundle-budget check passed. |
| Focused UI checks | The focused setup, external DNS and remote DNS run passed 29/29 runtime tests. Cases include explicit pairing/revocation, tenant isolation, lost-response reconciliation with the same pending identity, credential handling, fresh proof, immutable reviewed connection identity, preserved-record conflict messages and clipboard failure. Fresh nameserver display and separately confirmed enrollment while an old pending identity remains are included in both final runs. |
| Later local DNS guidance | The role-specific form explains primary DNS installation → secondary setup → final paired verification. After this bounded copy correction, all 16 setup runtime tests and the production build/budget passed. The earlier 352/29 runs retain their original snapshot scope. |
| Browser routing | Before remote DNS UI activation, the final base-setup Chrome run passed all 10 scenarios: fresh TR/EN at desktop/mobile widths, legacy, ready, waiting, unavailable, customer and license-locked entry. No runtime console errors or horizontal overflow; initial entry made no writes and customers/unlicensed users made no setup GET. These controlled fixtures do not prove a real-host profile run. |
| Remote DNS browser | The final Chrome replay passed all four Turkish/English, 1440/390 px pairing-to-setup scenarios after the fresh-nameserver correction. Each sent one explicitly authorized pairing and one setup start, with no writes on mount, runtime errors or horizontal overflow. These are controlled API fixtures. |
| Go tests and vet | The remote-integrated full run passed every package except migration 042's CRLF invariant; its failure is retained. The post-Certbot `81727ee4` snapshot passed all panel, database, agent and host-command tests plus all-package vet. The later DNS/mail recovery `31b1185c` snapshot passed all changed panel/agent package tests and all-package vet. Unchanged packages retain their preceding evidence. The [remote acceptance record](validation/server-setup-remote-dns-20260911/README.md) records the exact snapshots, results and diffs. |
| Focused race checks | Before remote connector integration, `-race` passed for panel, agent and database with `TestServerSetup\|TestSetup\|TestMailHostCertificate\|TestSQLite`, and independently for `TestSetupDNS`. Final mail lifecycle/contract race tests and the unchanged before/after failed-intent snapshot regression passed. These are focused race checks, not an all-tests race run. |
| Later DNS/mail recovery | Exact failed-child/rollback proof precedes reopening a DNS plan; uncertain children keep the draft locked. Mail DNS proof bypasses local hosts entries and supports bounded reverse CNAME chains. DNS recovery race (28.819 s and 3.468 s), mail DNS race (4.506 s), final 17 setup runtime tests and the build/budget passed. Running reconciliation uses neutral progress; no conflicting plan action is exposed. |
| Remote and Certbot race checks | Remote/setup regression race passed (87.499 s); the final real local HTTPS/invalid-record tests passed (9.128 s). Certbot process identity, protected metadata and related certificate/supervisor regressions passed (9.947 s). These are selected race checks, not a full race run. |
| Final BIND/public DNS source | Snapshot `a08aa344` passed all panel, agent, BIND and shared-DNS package tests plus all-package vet. Corrected secondary generation identities preserve historical receipts for exact rollback; owned catalog policy is verified from the receipt. Nameserver proof now bypasses hosts/NSS and requires complete A+AAAA responses. Selected shared-DNS and full BIND race checks also passed. |
| Real VM | Passed on isolated Debian 13, Ubuntu 24.04 and Arch test VMs: actual reboot with persistence enabled, SSH reconnection, exact saved nftables snapshot restoration, disabling removes the snapshot and disables the restore unit, and repeated request reconciliation creates no duplicate child. |
| Real mail TLS with a test CA | On the isolated Debian 13 VM, production export and queued renewal served the exact renewed trusted certificate through real Postfix STARTTLS and Dovecot IMAPS. Fallback bytes were unchanged; the same hook replay made no further change. The terminal mutation receipt matched the protected current generation. No panel or license manager participated. |
| Real hosting profiles and timer renewal | Separate Debian 13 VMs completed web, application and web-and-mail setup with fresh checks. All three renewed through the installed Certbot timer and production hooks against isolated test ACME authorities. Exact execution/child identities and setup completion state remained unchanged. Mail resumed its original waiting execution after the direct-DNS fix without repeating installation or issuance. |
| License loss and existing work | The web VM used a signed expired-license receipt: management returned `403 license_required` before and after renewal while Nginx, PHP and MariaDB stayed active. Certificate renewal continued under the license restriction. |
| Native paired DNS profile | On the same final `a08aa344` candidate, both DNS wizard executions succeeded with all five fresh readiness checks and exact start-replay identity. The existing member domain returned identical authoritative SOA and A answers from both servers. An initial post-create client assertion used the wrong JSON key; that failure is preserved, and read-only verification of the same domain then passed without recreation or a source change. |
| Public infrastructure limits | Public CA issuance, Internet mail delivery, public nameserver delegation and production remote pairing remain unproven. The completed native fixtures use controlled DNS/SMTP and isolated ACME trust roots; local acceptance does not imply production deployment. |
| Release and installed hosts | No release or installed-host update is established by this record. No installed panel was updated by the assistant. |

The [repository source and browser acceptance record](validation/server-setup-checks-20260911/README.md)
retains the full-Go, race, frontend and Chrome results with the tested source
snapshot hash.

The [repository firewall acceptance record](validation/server-setup-firewall-20260911/README.md)
contains the three-OS evidence. Original stage records and boot IDs are retained
on the test host at `/var/tmp/cp-server-setup-20260911/{os}/firewall-acceptance.json`.
The [repository mail-certificate record](validation/server-setup-mail-20260911/README.md)
contains the Debian live-listener/receipt proof, final source hashes and test logs.
It preserves the initial failing queue result and subsequent successful resume;
the whole fixture was not rerun from an empty VM. The protected last-success
configuration snapshot also passed the independent failed-intent regression.
This test-CA result does not prove public issuance, a real Certbot timer renewal,
Internet mail delivery or a complete purpose profile.

The [real hosting profile and timer record](validation/server-setup-profiles-20260911/README.md)
contains the three Debian hosting completions, actual timer renewals,
expired-license workload preservation, and the final two-VM DNS profile proof.
Production handlers ran in an explicitly enabled
unprivileged test daemon; the real agent performed host operations. Earlier failed
attempts, the mail waiting result and its later same-execution recovery are
preserved. DNS/SMTP and ACME trust were controlled test services. This evidence
does not claim public CA issuance, public delegation, or remote DNS pairing.

[Remote DNS source and UI acceptance](validation/server-setup-remote-dns-20260911/README.md)

Current remote UI logs are `.tmp-remote-dns-ui-full-tests.log`,
`.tmp-remote-dns-ui-tests.log` and `.tmp-remote-dns-ui-build.log` in the repository
workspace. Durable copies, the final local backend integration/race results,
the clean-source snapshots, their diffs and the four Chrome scenarios are retained
in the remote acceptance record linked above.

Browser results and screenshots are retained locally under
`.tmp-portal-review/server-setup/` (`browser-results.json`). They are temporary
working artifacts, not a signed or published release record.

This task ends at the approved source implementation and local acceptance.
Publication was not requested; installed-panel updates remain user-initiated.
Any later release must satisfy its relevant [operations gates](OPERATIONS.md)
against that release's exact source. These development results do not replace
public-infrastructure evidence or authorize a deployment.

Implementation references: [setup state](../cmd/panel/server_setup.go),
[execution](../cmd/panel/server_setup_operations.go),
[readiness](../cmd/panel/server_setup_readiness.go),
[DNS ownership](../cmd/panel/setup_dns.go),
[guided UI](../web/src/components/ServerSetup.tsx),
[remote DNS UI](../web/src/components/ServerSetupDNSConnections.tsx),
[remote DNS runtime tests](../web/tests/remote-dns-ui-runtime.test.mjs),
[UI runtime tests](../web/tests/server-setup-runtime.test.mjs),
[firewall regression tests](../cmd/panel/server_setup_firewall_test.go), and
[VM harness](../deploy/test-server-setup-firewall-vm.py).
