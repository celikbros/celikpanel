# CelikPanel v0.1.0-alpha.74

*Server identity stays visible during setup · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.74.tr.md)*

The server setup wizard now uses the same application shell as the rest of
CelikPanel. Its navy sidebar shows setup steps in place of normal management
menus. Hostname, IPv4 address and the server-reported version remain visible;
on narrow screens, identity stays in the header and the build in a persistent
footer. Initial choice, loading, assessment failure and completion also use
this shared shell.

DNS, panel HTTPS, update and password recovery remain accessible. The generic
Components shortcut is removed from the wizard. The setup shell does not fetch
navigation counts. Setup plans, confirmation, saved drafts, licensing and
running-operation recovery keep their existing rules.

Validation: 424 frontend tests, production build and bundle budgets passed.
Twelve local browser scenes cover choice, purpose, access, review and progress
at desktop, mobile and intermediate widths, including Turkish and English.
These fixture checks do not claim live server setup or DNS recovery success.

## Updating

Install **v0.1.0-alpha.74** yourself through **Settings → CelikPanel updates →
Check for updates**. The signed release package is Linux amd64. Publication does
not update installed panels or start setup. An active-operation update block
must be resolved through the supported recovery path; this release does not
bypass it. Previously recorded setup operations are retained and remaining
steps may require review after a build change.
