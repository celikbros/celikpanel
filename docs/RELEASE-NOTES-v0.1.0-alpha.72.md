# CelikPanel v0.1.0-alpha.72

*Native secondary DNS and wizard recovery · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.72.tr.md)*

A secondary DNS server with hosting can now use owner-managed DNS records without a remote CelikPanel URL or enrollment. Native DNS replication and optional panel record-management automation are separate choices. In manual mode, new hosted domains show external DNS instructions and require record verification; their owner publishes records on the primary using their own tools. Existing domain ownership and already reviewed plans are preserved. Automatic publishing through an authorized primary panel remains available.

The setup wizard preserves editable inputs and the current step within the browser tab across reloads and remounts. Returning to Review fetches a fresh plan and clears the start confirmation. It never starts setup automatically. Closing the tab ends the local recovery checkpoint; a changed server-side draft revision prevents restoration of stale inputs.

Validation: 399 frontend tests, production build and bundle checks, panel and BIND tests, focused race tests, and Turkish/English browser checks at desktop and mobile widths. A disposable BIND-primary/PowerDNS-secondary pair continued catalog discovery, zone transfer, owner record changes, service restarts and catalog removal with both panel and agent disabled and their binaries absent. This was a DNS service test, not a complete host reboot or panel-uninstall certification.

The [owner-independence audit](OWNER-INDEPENDENCE.md) records remaining mail certificate renewal, firewall boot restoration and data/runtime retention dependencies. This release does not claim that removing CelikPanel and its agent is safe for every workload. See [native DNS evidence](validation/native-dns-independence-20260912/README.md) and [wizard recovery evidence](validation/setup-editor-recovery-20260912/README.md).

## Updating

Install **v0.1.0-alpha.72** yourself through **Settings → CelikPanel updates → Check for updates** on each server. Return to **Settings → Server setup** to review DNS management and the saved plan. Release publication does not update installed panels or start server setup. The published package targets Linux amd64.
