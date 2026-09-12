# CelikPanel v0.1.0-alpha.71

*Hosting on a secondary DNS server · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.71.tr.md)*

A server can now host websites, applications and mail while serving secondary DNS. For the Frankfurt/Boston pair, Frankfurt remains the publishing primary and Boston serves the transferred secondary copy. Boston's hosting records are published through its authorized connection to Frankfurt; its sites and mail stay on Boston. BIND and PowerDNS can be used together in this arrangement.

The wizard now offers Secondary alongside hosting components and reviews the primary's HTTPS panel address. It prepares local DNS and secure panel access, then pauses for an explicit DNS publishing connection. Enter the primary's enrollment code in that same wizard and confirm the verified connection to continue the saved setup. Existing domains retain their recorded DNS ownership. This change does not add reciprocal per-zone primary roles or replicate site files, databases or mailboxes.

The connection is bound to the reviewed plan and server pair. Repeated confirmations and lost responses reconcile the same operation. Editing a waiting plan prevents a late response from the old execution from changing DNS defaults. Restarted setup preserves its completed local DNS step and verifies both local secondary DNS and remote publication before finishing.

Selected Nginx is prepared before panel certificate issuance so subsequent installation does not break standalone renewal. DNS waiting states keep setup recovery and settings available. Mail identity remains separate from the operating-system hostname.

Validation includes automated panel and frontend checks and a local two-VM BIND-primary/PowerDNS-secondary run. Controlled DNS/ACME fixtures verify the setup and DNS transfer path; they do not prove public DNS delegation, public CA issuance or Internet mail delivery. Mail-specific routing and readiness are covered by automated tests. See the [secondary hosting acceptance record](validation/secondary-hosting-20260912/README.md) for results and limits.

## Updating

Open **Settings → CelikPanel updates → Check for updates** and install **v0.1.0-alpha.71** yourself on both servers. Return to **Settings → Server setup** and review the pair and plan before starting setup. Start the primary first, then continue the secondary as the wizard directs. Installed panels are never updated during release publication.
