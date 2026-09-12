# Secondary DNS with hosting — local acceptance, 2026-09-12

The final fresh two-VM run completed a BIND primary and a PowerDNS secondary
that also hosted a real Nginx website. The secondary kept its DNS role while
publishing the website and mail DNS records through an explicitly authorized
connection to the primary. Both panels completed setup and passed an actual
Certbot timer renewal. No installed user panel was updated or configured.

## Final native result

| Check | Observed result | Evidence |
| --- | --- | --- |
| Primary setup | BIND primary; original reviewed execution succeeded | [Primary execution](final/dnsprimary/ready-execution.json) |
| Secondary bootstrap | PowerDNS secondary; selected Nginx installed before the panel certificate; pauses for publisher authorization | [Reviewed plan](final/dnssecondary/review.json), [waiting execution](final/dnssecondary/publisher-gate.json) |
| Explicit authorization | Real enrollment, authenticated machine connection and TLS; the same reviewed execution resumes after confirmation | [Connection](final/dnssecondary/connection.json), [confirmation](final/dnssecondary/publisher-confirmation.json), [completed execution](final/dnssecondary/ready-execution.json) |
| Final readiness | HTTPS, renewal, DNS, firewall and selected services all ready | [Primary state](final/dnsprimary/ready.json), [secondary state](final/dnssecondary/ready.json) |
| Website and records | Real static website returns HTTP 200 on secondary; A, MX, mail A, custom TXT, SPF and DMARC published on primary and transferred to secondary | [Wire, HTTP and SQL proof](final/final-before-proof.json) |
| DNS authority separation | Primary panel owns a `MASTER` zone; secondary runtime owns the matching `SLAVE`; secondary hosting row uses `existing` remote DNS, with no local authoritative zone or publication ledger | [SQL and wire proof](final/final-before-proof.json) |
| Transfer convergence | Both real DNS daemons answer authoritatively over UDP and TCP with equal SOA serials | [Wire proof](final/final-before-proof.json) |
| Exact deletion | Deleting the created domain through the secondary removes primary zone, transferred secondary zone and hosting row; DNS engine epoch and pair identity stay unchanged | [Delete result](final/final-delete-result.json), [after proof](final/final-after-proof.json) |
| Real scheduled renewal | Installed Certbot timer fires; real HTTP-01 renewal succeeds; served leaf changes through the deploy hook; setup/child identities and DNS roles remain unchanged | [Primary before](final/dnsprimary/renewal-before.json), [primary after](final/dnsprimary/renewal-after.json), [secondary before](final/dnssecondary/renewal-before.json), [secondary after](final/dnssecondary/renewal-after.json) |
| Readiness after renewal | Both panels still report every setup check ready | [Primary](final/dnsprimary/post-renewal-ready.json), [secondary](final/dnssecondary/post-renewal-ready.json) |
| Teardown | Both created V2 guests stopped; overlays and logs retained | [Shutdown](final/shutdown.json) |

The fixture addresses are `192.0.2.10` and `192.0.2.20`, with independent
disposable Debian 13 guests on an isolated QEMU peer network. The hosted domain
was `customer.secondaryfixture.test`; its A and mail A records point to the
secondary. The primary used the DNS purpose; the secondary used a custom plan
selecting Nginx. The native run did **not** install a complete mail stack.

## Mail execution and failure regressions

[secondary_hosting_mail_profile_test.go](../../../cmd/panel/secondary_hosting_mail_profile_test.go)
executes `runServerSetupStep` and the complete mail-profile child runner after
the same reviewed secondary-hosting execution has bound its publisher.
Both `webmail` and `protected-mail` profiles succeed, including their package
operations, mail stack configuration, submission, TLS synchronization and final
service checks. OS hostname mutation stays absent and the reviewed mail identity
is retained. Revoking the connection before either child executes rejects the
operation before any host mutation. The secondary DNS identity and empty local
publication tables are checked after every case.

These four cases use a deterministic RPC agent, not real mail daemons. They
passed with the race detector on Go 1.26.5 in 9.491 seconds:
[log](regression/mail-profile-race.log), [exact command and exit code](regression/mail-profile-result.json).
The earlier independent
[native mail acceptance](../server-setup-mail-20260911/README.md) covers actual
mail daemons and certificate convergence in its own fixture. It is separate
evidence, not a claim of end-to-end mail delivery in this two-VM scenario.

[remote_dns_secondary_hosting_test.go](../../../cmd/panel/remote_dns_secondary_hosting_test.go)
also covers BIND and PowerDNS secondary identities with real remote machine
authentication and SQL handlers: web creation, TXT/mail DNS changes, deletion,
lost-response reconciliation and revocation preserving existing data. These
six cases passed with the race detector on Go 1.26.5:
[log](regression/secondary-hosting-race.log), [command and exit code](regression/result.json).
Additional [backend checks](backend/README.md) and
[frontend/browser checks](frontend/README.md) are recorded separately.

## Preserved initial failure

The first fresh attempt reached the publisher binding and hosting steps but
stopped at renewal verification. It had issued the panel certificate with
Certbot's standalone authenticator, then installed Nginx on port 80. An enabled
timer alone did not make this a renewable configuration. The original
[inspection](attempt-1/dnssecondary/inspection.json) and
[renewal failure evidence](attempt-1/dnssecondary/renewal-failure-evidence.json)
remain intact. Its separate probe still demonstrated real DNS transfer,
website HTTP and exact deletion, but that attempt is **not** counted as a
successful completed setup. Its guests were [stopped](attempt-1/shutdown.json).

The backend correction moves selected Nginx installation before panel
certificate issuance and the publisher gate. V2 used completely new overlays
and a new source snapshot. It completed the original reviewed execution without
editing its plan, and its saved webroot renewal configuration subsequently
passed a real timer-driven renewal while Nginx stayed active.

## Provenance and reproduction

Both final native binaries were built with the explicitly pinned
`/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go`, `GOTOOLCHAIN=local`,
`GOENV=off` and `GOWORK=off`. The binary build metadata independently reports
`go1.26.5`, Linux/amd64: [compiler provenance](final/compiler-provenance.json).
[Binary hashes](final/binary-sha256.json) identify the executed artifacts;
[source hashes](final/source-manifest.json) identify every copied source file.
The [post-run comparison](final/source-comparison.json) found no production Go
changes since that snapshot. Later differences were release metadata/contract
tests and the additional mail-profile regression file. This evidence describes
the tested snapshot, not an assertion that the release metadata was identical.

The guarded [test daemon](../../../cmd/panel/secondary_hosting_vm_test.go) runs
the existing disposable setup daemon with the production handlers and agent.
A test-only transport seam maps the one pinned primary endpoint to the
isolated peer. HTTPS still verifies the endpoint name and trusted fixture CA;
machine credentials and route authorization use production logic. Public DNS
lookups and ACME use controlled fixture resolvers and Pebble. Production IP
policy and TLS verification are not relaxed.

The [fixture scripts](fixtures) run in this order with
`CELIKPANEL_SECONDARY_HOSTING_VM_ROOT=/var/tmp/cp-secondary-hosting-20260912-v2`:
`provision.py`, `build_and_stage.py`, `run.py`, `verify_and_delete.py`, `renew.py`,
`final_evidence.py`, `stop.py`, `collect.py`. Provisioning refuses an existing
root. Guest actions verify the exact QEMU identity and overlay, an explicit
fixture marker, and absence of an installed panel binary. Reproduction requires
the pinned local base image, SSH fixture key, Pebble and toolchain described by
those scripts; the retained run cannot be silently overwritten.

The renewal test temporarily shortens only the disposable timer delay and
forces its installed Certbot service to renew immediately. It retains the
saved challenge method, endpoint and deploy hooks, then restores the installed
timer. This proves the local real renewal path, not public ACME issuance,
Internet DNS delegation, mail deliverability, PTR reputation or production
network reachability. No credentials, enrollment codes, sessions or private
keys are copied into this evidence set. Collected JSON checksums are in
[the artifact manifest](final/artifact-sha256.json).
