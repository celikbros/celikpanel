# CelikPanel v0.1.0-alpha.65

*Guided server setup · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.65.tr.md)*

New installations now guide the administrator from activation to a reviewed,
purpose-specific setup. Existing installations retain Dashboard access; upgrading
does not automatically install a hosting stack or require setup to be repeated.

## What changes

- **Four setup purposes:** web hosting, web and mail, Node.js applications, or
  authoritative DNS. Each plan shows its components, access rules and identity
  changes before the administrator explicitly starts it.
- **Three DNS management choices:** managed local DNS, an authorized connection
  to another CelikPanel DNS server, or records maintained at an external provider.
  Remote enrollment, publication and revocation are restricted to the connection's
  domains. Existing domains retain their DNS ownership.
- **Verified completion:** panel HTTPS and renewal, firewall, selected services
  and DNS readiness must pass actual checks. Mail also verifies its identity,
  TLS and delivery prerequisites. A local primary/secondary pair verifies transfer
  separately from publishing authority.
- **Recoverable execution:** reviewed plans and child-operation identities persist
  on the server. Reconnecting reconciles the same operation. Uncertain results
  keep conflicting changes blocked; proven terminal failures permit a new review.
- **License and workload separation:** an invalid license blocks management and
  admission of new setup steps. Existing services and certificate renewal continue;
  already accepted operations finish or reconcile safely.

This release also corrects BIND secondary catalog configuration, public DNS checks
that could be shadowed by local hosts entries, Certbot subprocess ownership, and
firewall preservation during setup. Remote DNS record changes preserve unrelated
verification records and reject conflicting mail routing.

## Existing installations and database changes

Migrations **039–042** add setup state, per-domain DNS ownership, durable reviewed
plans/executions, and authorized remote DNS publication state. Pre-existing
installations remain classified as existing installations. Their domains keep
local ownership; selecting a new default does not move their DNS.

The Dashboard's **Review server setup** entry is optional for an existing server.
Opening it does not install software or change configuration. Any new setup plan
still requires its own review and explicit start. Completed setup is not reset by
login, activation or a panel update.

## Updating Boston, Frankfurt, or another installed panel

After this release is available, the administrator performs these steps separately
in each server's own panel:

1. Open **Settings → CelikPanel updates**.
2. Select **Check for updates** and confirm the offered version is
   **v0.1.0-alpha.65**.
3. Review the displayed target, then select **Start signed update to
   v0.1.0-alpha.65** when the panel reports it can start.
4. Wait for the operation screen to finish and reload. Confirm **Current version**
   is **v0.1.0-alpha.65**. If the connection drops, let the operation screen
   reconnect to the existing update.

If management is license-locked, sign in as an administrator and expand
**Update CelikPanel** on the activation page. The same check/start controls are
available there. Updating does not activate or renew the license.

Publishing this release does not install it on any server. Installed-panel updates
are initiated only by the user through CelikPanel's update interface.

## Validation and limits

Source tests, selected race checks, all-package vet, the frontend production build
and controlled browser scenarios passed; exact source snapshots and follow-up
scopes are in the [source acceptance record](validation/server-setup-remote-dns-20260911/README.md).
The [native profile record](validation/server-setup-profiles-20260911/README.md)
contains four completed purposes on disposable Debian 13 VMs, real hosting
certificate timer renewals, paired authoritative DNS transfer, and a license-locked
web server whose services and renewal continued. Firewall persistence also passed
[actual reboot checks on three operating systems](validation/server-setup-firewall-20260911/README.md).

Those fixtures use controlled DNS/SMTP and isolated ACME trust roots. They do not
prove public CA issuance, Internet mail delivery, public nameserver delegation or
production remote pairing. Paired setup currently cannot verify a peer nameserver's
published IPv6 address; use IPv4-only nameserver publication or external DNS for
that setup path. Existing AAAA records are never removed automatically.
