# AZ: native mail lifecycle with the configuration observation barrier

Source `32b3b548d4fca64b27638a8ac380aa63e4fcbade`; test binary SHA-256
`c3816e91d7c695f7bfbfaccefb06d5e1f7d68e972fccbeb7eaf451cd91308d5d`.
[Bounded record](MAIL-CONTRACT-AZ.json) uses the same verifier as AX/AY.

On 2026-09-22 a fresh, guarded Debian 13 QEMU cell ran the full prior AY procedure
with the strict shared evidence reader and new pre-publication configuration
observation. Native apt installed Postfix/Dovecot/Certbot. The real CLI initialized
the empty ledger; no ledger or receipt was synthesized by the controller.

Actual initial mail TLS configuration, first host certificate publication and
second-generation renewal all passed. A separate process verified the exact
successful job, publication phase and selected immutable receipt. After orderly
reboot, enabled Postfix/Dovecot served the same renewed certificate before any
management test ran; independent OpenSSL verified trusted SMTP/IMAP hostname and
leaf. The installed Panel and Agent executables were absent throughout. Only then
was the absent volatile test lock recreated and the exact receipt verified again.
Finally the deliberate fixture-owner selection of the earlier valid certificate
survived replay: pending newer source and historical success both remained intact.

Thus the new observation boundary does not block the tested initial/renewal path;
[AY's separate owner-configuration refusal](MAIL-OBSERVATION-AY.md) uses the same
source. This is fixture-CA native acceptance, not external ACME, a deployed hook,
an independent renewal helper or power-loss recovery. General delivery/authentication,
Arch mail acceptance and the full P0.4/P0.5 matrix remain open. Native logs retain
the Postfix spool resolv.conf ownership warning. No installed owner panel changed.
The lab is stopped with disks/build/intent/original evidence retained. Published
logs are consistency evidence, not host attestation.
