# CelikPanel v0.1.0-alpha.76

*Panel access recovery and truthful setup verification · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.76.tr.md)*

Managed panel HTTPS keeps the usable original IP certificate for connections
without a hostname, while the configured hostname uses its managed certificate.
A validated hostname link is available on sign-in and recovery screens. IP access
can still require the browser's certificate warning acknowledgement. Explicit
owner TLS settings are preserved; missing bootstrap certificates are not created.

Temporary access-check failures no longer masquerade as license rejection or
send an unconfirmed session to activation. Previously verified access is retained
only until its original deadline. Otherwise, a connection recovery screen offers
retry and reload. Confirmed license restrictions and backend authorization still
apply. Interrupted operations retain their exact identity when reloading.

Setup's final verification stays pending until readiness checks and guarded
completion succeed. Older premature success receipts are corrected without
reinstalling completed services. Check requirements again now reports unchanged
requirements, unknown results, failures or passed checks beside the button.
A successful installation step is no longer confused with a ready server.

## Updating and continuing

Install this version yourself through **Settings → CelikPanel updates**.
Publishing the release does not update installed servers. An update does not
restart a failed installation or change its reviewed plan. Existing completed
steps remain in place; unresolved DNS, PTR, package-manager or other requirements
still need their own recovery. For mail identity, the chosen mail hostname must
match its public forward and reverse DNS records.

## Validation

Local frontend tests (442), production build and bundle budgets passed. Linux
Go tests cover TLS selection, access boundaries, setup verification and revision
guards. Local Chrome fixtures cover EN/TR desktop/mobile connection recovery and
all four verification outcomes. Tagged CI validates the exact release commit and
reproducible signed archives before publication.

See [panel recovery](PANEL-ACCESS-RECOVERY.md) and
[verification feedback](SETUP-VERIFICATION-FEEDBACK.md) for limits and evidence.
