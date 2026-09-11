# CelikPanel v0.1.0-alpha.66

*Explicit setup preference · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.66.tr.md)*

The setup entry was easy to miss on unassessed servers upgraded to Alpha65.
This release presents two clear choices on first use: **Set up with the wizard**
or **I'll configure it myself**.

- Manual mode is remembered by the server across sign-ins and browsers. It
  suppresses automatic wizard navigation and the Dashboard setup invitation.
- **Settings → Server setup** provides a permanent way to reopen the wizard.
- Fresh installations offer the same choice. Existing operations resume and
  completed installations are not reset.
- Manual mode does not complete setup or remove license, security, DNS or service
  requirements. Active or waiting setup operations prevent preference changes.

No new database migration is required. The preference uses existing panel
settings and does not alter DNS records, running services or certificates.

357 frontend tests, Go setup tests and race checks passed. TR/EN desktop/mobile
browser fixtures verified selection, persisted manual navigation and re-entry.
See [implementation and evidence](SERVER-SETUP-GUIDANCE.md).

## Updating from the panel

Update Boston first: **Settings → CelikPanel updates → Check for updates**, then
start the signed **v0.1.0-alpha.66** update. Wait for completion and verify the
current version before repeating on Frankfurt. Only the user starts installed
panel updates.

After updating, open Dashboard to see the choice if no preference was recorded.
Choosing the wizard does not start a service installation: review and start its
plan separately. When license-locked, use **Update CelikPanel** on the activation
page.
