# Separate mail identity from the system hostname

September 12, 2026. Source change; not a published release or an installed-panel update.

The setup review previously advertised an OS rename to the mail address, and
mail profile installation called `Agent.SetServerHostname`. The TLS snapshot
also derived the mail identity from the OS. This coupled three distinct names:
the system hostname, panel address, and mail service address.

Mail profile installation now saves the mail identity and the existing
payload-bound mail TLS workflow configures Postfix and mail certificates with
that identity. Installation does not rename the OS. The panel's unused hostname
RPC grant is removed and the agent denies that mutation for mail profiles.
Setup review no longer plans a hostname change. Legacy wire fields remain
readable, and an existing installation with no saved mail identity retains its
OS-derived TLS identity. An invalid saved identity fails validation instead of
silently switching names. Previously renamed servers are not renamed back.

The standalone Components mail installer now treats only a saved mail identity
as settled; a valid OS hostname does not prevent entering a different mail
address. Turkish and English explanations describe the actual behavior.

## Profile checks

| Profile | Services and constraints |
| --- | --- |
| Web hosting | Nginx, PHP-FPM and MariaDB; no mail profile, mail certificate or mail ports added on an empty host |
| Web and mail | Web services and audited mail profiles; mail identity, TLS and delivery checks remain mandatory |
| DNS server | Local BIND or PowerDNS, primary or secondary; no unrelated web, mail or database installation |
| Application without a database | The `none` choice no longer becomes a nonexistent required service during final verification |

All profiles retain panel HTTPS and firewall requirements. Existing service
ports are preserved. Hosting with locally managed DNS still needs a publisher;
the UI now explains the secondary restriction next to the selector and points
to the DNS-only profile or external/existing DNS methods. Changing purpose drops
the previous component customization without uninstalling existing software.

## Validation

- Regression tests cover OS preservation, explicit mail identity in TLS RPCs,
  legacy fallback, invalid saved identities, and denied OS rename permissions.
- A 24-case plan matrix covers web, web/mail and DNS profiles, local/external
  DNS, both engines and both roles. Review must be read-only and must not add
  unrelated services, mail ports or OS renames.
- Runtime UI tests cover the default web restriction, switching to DNS and
  choosing either engine as secondary, and clearing inherited mail selections.
- Frontend suite: 388 tests passed; production build and bundle budget passed.
- Local Chrome: six Turkish/English desktop/mobile scenarios passed with mocked
  APIs and zero setup-start calls. Desktop/mobile screenshots were inspected.
- Backend commands: `go test ./cmd/panel -count=1` and
  `go test ./cmd/agent -count=1`.

These checks do not prove real public DNS delegation, BIND-to-PowerDNS transfers,
public CA issuance, or mail delivery on Frankfurt/Boston. No installed server
was configured or updated during this work. The user starts installed-panel
updates through CelikPanel itself.
