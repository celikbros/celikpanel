# CelikPanel v0.1.0-alpha.68

*Clearer server setup layout · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.68.tr.md)*

The server setup wizard uses a wider workspace with steps on the left on desktop
and a compact progress indicator on mobile. Component groups use two columns on
wide screens. Navigation stays visible in long forms and returns to normal flow
in short windows.

**Customize selection** now appears inside the selected purpose profile. It moves
with the selection and opens that profile's components. **Custom setup** starts
with an empty component selection. The bottom action area contains **Continue**;
the manual setup choice and its reopening instructions remain together below it.

This release changes presentation. Dependency checks, conflicts, DNS, renewable
panel HTTPS, firewall requirements, licensing and saved setup choices retain
their existing behavior. It does not install hosting services automatically.

Validation: 381 frontend tests, frontend production build, TR/EN responsive
browser checks, dark mode and keyboard checks, and all five profile entry/save
paths using local test data. These are local checks, not live server installations.

## Updating

Open **Settings → CelikPanel updates → Check for updates** and install
**v0.1.0-alpha.68** from the panel. Only the user initiates an installed update.
After completion, reopen **Settings → Server setup → Open setup wizard**.
The saved manual/guided preference is preserved.
