# CelikPanel v0.1.0-alpha.70

*Separate mail identity and clearer setup choices · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.70.tr.md)*

Mail installation preserves the operating-system hostname. The saved mail address is used for Postfix and mail TLS synchronization, including renewal, independently of the panel address. The agent no longer authorizes an OS rename from a mail profile. The setup review no longer announces that rename. Previously renamed servers are not automatically renamed back.

The standalone mail installer allows a separate mail address even when the system already has a fully qualified hostname. Existing installations without a saved mail identity retain their previous OS-derived identity; an invalid saved identity blocks synchronization rather than silently choosing another address.

The setup wizard explains why local Secondary DNS is unavailable for a hosting profile and how to select DNS-only setup or another DNS management method. Web hosting does not add mail. DNS-only setup supports either offered engine and role without adding web, mail or database services. The application profile's “no database” choice no longer fails its final service check.

Validation: panel and agent test suites, 388 frontend tests, production build and bundle budget, a 24-case profile/DNS plan matrix, and six local TR/EN desktop/mobile browser scenarios. Local fixtures do not prove public DNS delegation, mixed-engine zone transfers, public CA issuance or live mail delivery.

## Updating

Open **Settings → CelikPanel updates → Check for updates** and install **v0.1.0-alpha.70** yourself. Return to **Settings → Server setup** and review the plan again before starting setup. No installed panel was updated during development or release publication.
