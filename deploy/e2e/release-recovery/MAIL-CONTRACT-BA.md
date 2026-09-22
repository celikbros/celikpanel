# BA: native mail lifecycle with scoped renewal admission

Source `519db3486d383dff61536603a8e8cb6b0be62366`; test binary SHA-256
`dedee37ea70601d993072e9fd8fb21d6dc9dfbc0fc9d8cce2f700865a9a02b58`.
[Bounded record](MAIL-CONTRACT-BA.json) uses the existing AX/AY/AZ verifier.

A fresh guarded Debian 13 QEMU fixture on 2026-09-22 ran initial native mail
configuration, host certificate publication and the actual queued renewal using
the new exact-operation manager. Native Postfix/Dovecot presented both expected
fixture-CA trusted generations. A separate process verified the exact terminal
job and selected receipt. The ordinary Agent manager and narrow renewal manager
successfully exchanged the shared canonical ledger through the real host lock.

After orderly reboot, independent OpenSSL probes verified the renewed SMTP/IMAP
leaf and hostname before any management fixture executed or its volatile test
lock was recreated. Enabled mail services were active; installed Panel and Agent
executables were absent. The exact receipt survived reboot. Deliberate fixture
owner selection of the older valid certificate then remained unchanged by queue
replay, alongside the pending newer source and historical successful receipt.

This is lifecycle regression acceptance for admission isolation, not evidence of
an independent renewal executable, public ACME, a deployed Certbot hook, power
loss, or interrupted-operation recovery. Foreign-evidence and scope refusal are
covered by the source tests, not claimed as native crash trials here. The full
Agent race suite passed in 203.139 s and vet passed. Native logs retain the Postfix
spool resolv.conf ownership warning; delivery/authentication and Arch mail remain
outside this check. All P0 items retain their open acceptance work.

No installed owner panel changed. Lab disks, build hashes, intents and original
logs are retained. These logs prove internal consistency, not host attestation.
