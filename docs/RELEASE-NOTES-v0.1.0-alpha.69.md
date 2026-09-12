# CelikPanel v0.1.0-alpha.69

*Simpler access and DNS setup · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.69.tr.md)*

The setup wizard groups local DNS settings into **This server** and **Other DNS server**. Each group contains the role, nameserver and IP address; the other server's role is automatic. Changing roles preserves the name and IP assigned to each physical server. The peer nameserver is derived from its group instead of entered twice. Conflicting saved mappings stay visible and must be corrected before review.

DNS method choices and HTTPS guidance are shorter. Only relevant settings appear, with detailed pairing instructions available on demand. A usable server-reported public IPv4 address prefills an empty field; saved addresses and edits are preserved. Desktop groups stack on mobile.

Existing DNS, HTTPS renewal, firewall, licensing, remote-connection verification and reviewed-plan safeguards remain in force. Opening the wizard does not install services.

Validation: 386 frontend tests, production build and bundle budget, six TR/EN desktop/mobile browser scenarios using local mock APIs. No installed panel was updated during validation.

## Updating

Open **Settings → CelikPanel updates → Check for updates** and install **v0.1.0-alpha.69** yourself. Reopen the wizard through **Settings → Server setup** to see the revised Access and DNS step.
