# Native mail shared publisher acceptance: AY

D-025 invariants 1/2/3, P0.4 producer/reader agreement and P0.5 continuity.
A fresh guarded Debian 13 QEMU fixture reran the AX procedure with source
`94faf059055190a75b7fe8fa11b2d91904dd15b1`, including the shared accepted mail TLS
plan and actual shared certificate publisher. Test binary SHA256:
`ad5a3b3740b06644b5268a88281584462ad6935c3133a1f4e3cc0d833de9c1a5`.

[Bounded native record](MAIL-CONTRACT-AY.json) uses the same consistency verifier
as AX. `test_verify_mail_contract.py` accepts both genuine records and rejects
changed scope, failed jobs, mismatched operations/boot/leafs and missing owner
preservation. No private key or nonce is published; original private evidence and
disks remain retained in the stopped AY lab.

Actual Postfix 3.10.13 and Dovecot 2.4.1 accepted the first generation and the real
Agent renewal path deployed the second generation with system-trusted SMTP/IMAP
handshakes. A separate process verified the exact terminal receipt and queue.
After orderly reboot the enabled native daemons served the same new certificate
before any management test ran. Both installed management binaries were absent.
The test-only volatile lock directory was then recreated explicitly, and receipt
and trusted-handshake checks repeated. Finally, an explicit owner selection of
the earlier valid certificate survived renewal replay, with the pending newer
source and historical succeeded job preserved.

This proves the new producer/reader path on Debian, not independent renewal.
The fixture CA and synthetic native-owned Certbot layout are unchanged from AX;
no external ACME or installed deploy hook was exercised. Root component tests
separately cover changed staged material, changed selection before publication,
unsafe parent metadata and published-but-fsync-failed state. Those are not native
power-loss or interrupted-publication trials. The Postfix spool resolver ownership
warning remains recorded; general mail delivery/authentication is not certified.

Remaining: independent owner-approved renewal enrollment and durable transaction,
shared outer operation exclusion, native service checks/reload, hook/retained-helper
migration, cross-version recovery and the full absence/fault/workload matrix.
No installed owner server was changed. This trial adds evidence to the existing
P0 plan; it does not close P0.4 or P0.5.
