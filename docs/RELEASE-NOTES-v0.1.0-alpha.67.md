# CelikPanel v0.1.0-alpha.67

*Custom setup and clearer update failures · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.67.tr.md)*

Administrators can now choose components inside the setup wizard. Select
**Custom setup** or choose a purpose profile and **Customize selection**.
The added Components step includes dependencies, explains conflicting choices,
and identifies installed software. Clearing a choice never uninstalls existing
software or deletes data.

The reviewed plan, firewall ports, Node runtime requirements, mail profiles and
completion checks follow the selected components. DNS, trusted renewable panel
HTTPS and safe firewall requirements remain in force. Existing Alpha66 setup
plans retain their serialized identity. Components remains available for later
service administration; no bulk removal or component-update flow is added.

An updater refusal now preserves its cause across cleanup. An explicitly detected
package-manager conflict with no installed changes receives a clear retry
instruction. Other failures no longer claim that a safe rollback necessarily
occurred. These changes preserve package locks and admission checks; they do not
stop OS package operations or start updates automatically.

Local verification includes 381 frontend tests, setup backend tests and race
checks, update failure/rollback contracts, and eight TR/EN desktop/mobile custom
setup browser scenarios. The new component combinations reuse existing supported
operations; a new real-server installation matrix is not claimed. See
[customization evidence](validation/server-setup-customization-20260911/README.md)
and [update diagnostics](validation/update-failure-report-20260911/README.md).

## Updating from Alpha66

On Boston, open **Settings → CelikPanel updates → Check for updates** and select
the signed **v0.1.0-alpha.67** update. Wait for completion and verify Current
version before repeating on Frankfurt. Only the user starts installed updates.

Then open **Settings → Server setup → Open setup wizard**. Select a profile and
customize it, or choose Custom setup. Review the separate server preparation
plan before starting it. Installing this panel update does not install selected
hosting components or reset the saved manual/guided preference.
