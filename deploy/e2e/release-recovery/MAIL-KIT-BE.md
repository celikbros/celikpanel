# Independent native mail scheduling

P0.5 bounded Debian13 acceptance, 2026-09-22. Source `fbe05598bd8f7d4fa528546a25945e4df1bc8ccf`.
[Machine evidence](MAIL-KIT-BE.json) binds the source, compiled kit manifest,
native files and original transcripts. It is consistency evidence, not remote
attestation or production release signing. Guarded QEMU fixture BE from the
[prior runtime trial](MAIL-RUNTIME-BE.md) is reused; originals remain private.

The fixture explicitly installs the exact immutable kit under its generated
`/usr/libexec` path, retaining the previous Agent hook as evidence. Neither
installed management binary exists. `systemd-analyze verify` accepts both units.
The actual service sandbox processes a same-leaf queue from the new hook without
changing the existing successful operation, even though the helper build identity
has changed. This fixture installation is not the product enrollment implementation.

A new disposable CA source is then prepared. The actual deploy hook queues it;
enabling the native timer triggers the service without a Panel/Agent call. The
new leaf is selected, its exact receipt matches a succeeded ledger operation,
configuration bytes/mtimes remain unchanged, and trusted SMTP/IMAP present it.

A fourth source is queued while the enabled timer is deliberately stopped. After
an orderly reboot, the enabled timer starts automatically, invokes the exact
helper, recreates the absent volatile runtime and deploys that queued new leaf.
The changed boot ID, timer/service journal, exact durable completion, missing queue,
unchanged native settings and trusted SMTP/IMAP fingerprints agree. No manual
service start or helper process invocation occurs after that reboot.

Production enrollment, old/new hook rollback, signed release integration, public
ACME issuance, power-loss durability and recovery of an interrupted renewal remain
open. This proves automatic boot scheduling in the named disposable Debian fixture,
not the full P0.5 workload/absence matrix or behavior on all supported systems.
