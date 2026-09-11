# Real server setup profile validation

*11 September 2026 · [Türkçe](README.tr.md)*

All four purposes completed on disposable Debian 13 VMs: web, application,
web-and-mail, and DNS using an independent primary/secondary pair. All three
hosting profiles also passed a real Certbot timer renewal. The DNS pair passed
real authoritative member-zone transfer and matching SOA/A answers.
These are development results, not a release or an installed-panel update.

## Verified behavior

| Profile | Installation and readiness | Renewal |
|---|---|---|
| Web | Nginx, PHP and MariaDB; trusted panel HTTPS; external DNS mode; firewall; fresh final checks. No mail services installed. | New trusted panel certificate served after the installed Certbot timer and deploy hook ran. |
| Application | Node.js and Nginx; trusted panel HTTPS; external DNS mode; firewall; fresh final checks. No unselected mail, PHP or MariaDB services installed. | New trusted panel certificate served after the timer and hook ran. |
| Web and mail | Web services, Postfix, Dovecot, Roundcube and Rspamd; panel and mail TLS; DNS identity and delivery checks; firewall; eight fresh checks ready. | Both lineages renewed. SMTP submission on 587 and IMAPS on 993 served the same new mail certificate; the panel served its new certificate on 2083. |

The DNS primary and secondary both completed their durable executions with
all five fresh checks ready: HTTPS, renewal, DNS, firewall and services. Trusted
HTTPS and renewal readiness were verified on both; actual timer renewal was
independently exercised on the three hosting profiles above.

Each repeated explicit start returned the same durable execution. Renewal left
the setup revision, completion time and exact child-operation rows unchanged.
The web fixture then used a signed expired-license receipt: management returned
`403 license_required` before and after renewal, while Nginx, PHP and MariaDB
remained active. The license restriction did not prevent certificate renewal.

See [machine-readable results](results.json), each profile's
`execution-evidence.json`, `renewal-before.json`, `renewal-after.json`, and
`final-profile-proof.json` under [hosting](hosting/). The saved
[artifact hashes](artifact-sha256.json) identify the collected files.

The paired DNS results are in [final-native-results.json](dns/final-native-results.json).
The primary's [final evidence](dns/dnsprimary/final-execution-evidence.json)
contains the exact member answers and read-only convergence observations; the
secondary's [evidence](dns/dnssecondary/execution-evidence.json) records its
completed execution. The primary DNS step committed before secondary admission.

## Environment and scope

The HTTP process was an explicitly enabled Go test daemon, running as the
unprivileged panel account with real HTTPS, session/CSRF checks, production setup
handlers, SQLite persistence and restart recovery. Every host mutation used the
real privileged agent and its existing operation protocol. The installed-panel
binary was absent; hostname, fixture marker and QEMU identity guarded staging.
No Boston, Frankfurt or other user panel was updated.

Each guest had its own isolated ACME service and trust root. Certbot performed
real HTTP-01 validation and issuance. Timer-only overrides made renewal due during
the test; saved ACME endpoints, challenge plugins and deploy hooks remained in
use. The original timer configuration was restored afterward. DNS, reverse DNS
and the SMTP connectivity target were controlled inside the disposable guests.
The mail fixture used a TEST-NET address and a local resolver endpoint to model
public identity. This does not prove public CA issuance, Internet mail delivery
or public nameserver delegation.

## Failures preserved and corrected

Earlier attempts are retained in `attempt-1` and `attempt-2`; they are not
relabeled as successful runs. Fixture startup identity, resolver and package
configuration issues were corrected before the third set of fresh VMs.

Real issuance exposed a production defect: Certbot inherited the agent's shared
group. Its actual child now runs with UID/GID 0 and no supplementary groups.
Explicit reissuance may normalize only verified, fixed managed directories;
existing certificate/key files and unrelated lineages are not adopted or changed.
The strict certificate source reader remains intact.

Mail verification then exposed an NSS problem: the hostname operation's local
`/etc/hosts` entry hid correct external forward DNS. The negative result is saved
in `hosting/webmail/readiness-before-mail-dns-fix.json`. Bounded direct DNS queries
now verify PTR and forward answers, including classless reverse CNAME delegation.
After replacing only the disposable agent, the original waiting execution became
ready without repeating any installation or issuance. Its before/after child
identities and candidate provenance are retained alongside the mail evidence.

The first paired DNS attempt exposed invalid BIND secondary catalog syntax and
an uncertain failed-child recovery path. Its original states, logs, shutdown
record and real BIND parser proof remain under `dns-attempt-1/`. Catalog zone
declarations now live in the zones configuration and subscriptions in the owned
global options block. New receipts have a distinct configuration version; exact
historical generations remain recoverable without being accepted as current.
Foreign configuration is not silently replaced. Public nameserver checks now
use bounded direct A and AAAA queries, independently of local hosts entries.

The fresh final DNS pair completed on the corrected source. Its post-creation
test client initially expected `domain_id` while the existing API returns
`DomainID`. That assertion log and driver are preserved. The read-only
continuation verified the already-created domain; it did not repeat creation,
installation, or setup. See the [continuation record](dns/driver-continuation-record.json).

## Source provenance

The initial completed web/application runs use snapshot `81727ee4…` and agent
`db67d54c…`. Their exact manifests are under `hosting/`. The mail recovery uses
snapshot `31b1185c…` and agent `64ad8615…`, recorded separately under
`candidate-recovery/`; earlier artifacts were not overwritten. The shared
`64a000…` build stamp is a test-fixture identity, not a Git revision or release.

The final DNS pair uses snapshot `a08aa344…`, agent `3a924da5…`, and test daemon
`98550cbe…`; exact manifests and binary hashes are under `candidate-bind/`.
Final panel, agent, BIND and shared DNS tests and all-package vet passed on that
snapshot. The shared DNS extraction retains the earlier mail behavior and its
focused race regressions. Earlier profile runs retain their own source scope.
All five final VMs were stopped with their artifacts and overlays preserved.

[Source and frontend checks](../server-setup-remote-dns-20260911/README.md),
[three-OS firewall/reboot proof](../server-setup-firewall-20260911/README.md), and
[the earlier independent mail TLS proof](../server-setup-mail-20260911/README.md)
have separate scopes and logs. The reproducibility drivers are retained in
`fixtures/`; they require the guarded, disposable VM environment.
