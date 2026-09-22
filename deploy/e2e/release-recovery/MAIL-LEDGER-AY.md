# AY: independent checker reads the retained native ledger

On 2026-09-22, a standalone executable built from the production
`deploy/recovery/agent-checker.sources` at
`11a088fd621993235b02a144d3e4b0797480c4e1` ran on retained disposable Debian AY.
Binary SHA-256: `e9c89faa86a7d1a9beb740b42628218f6550b6b1c9123261911ce81553de76a5`.
The [bounded record](MAIL-LEDGER-AY.json) binds to [prior AY evidence](MAIL-CONTRACT-AY.md).
Logs are consistency evidence, not host attestation.

The controller verified QEMU process, SSH, nonce/DMI and systemd identity before
upload/execution. The guest had no installed Panel or Agent executable. Only the
absent volatile host-lock fixture was created with established numeric owner
identity; existing mismatched paths/metadata would be refused. The standalone
checker was copied solely into the private lab directory, not installed.

The real retained ledger passed `--check-service-mutation-idle`. Holding the
actual host flock made that ordinary check fail with the expected busy reason.
Passing the same held descriptor via `CELIKPANEL_MUTATION_LOCK_FD=9` to
`--check-service-mutation-idle-under-external-lock` passed. This used the new
shared full ledger/phase validation and the native package-manager checks.

Byte hashes of the ledger, pending renewal, Postfix main configuration and
Dovecot TLS fragment, plus the selected symlink, were identical before/after.
Both native services remained active and independent OpenSSL connections to
SMTP 587 STARTTLS / IMAPS 993 verified the hostname and trusted owner-selected
certificate. This is readback/exclusion acceptance, not certificate renewal,
crash recovery, full daemon configuration validation or general mail delivery.
The lab was stopped with disks and private build/intent/result records retained.

P0.3/P0.4/P0.5 remain partial. There is no on-disk schema migration and no installed
owner-server change. Native independent renewal enrollment, safe hook migration,
durable transaction/reload and full interruption acceptance remain open.

```sh
python3 deploy/e2e/release-recovery/verify_mail_contract.py deploy/e2e/release-recovery/MAIL-LEDGER-AY.json --base deploy/e2e/release-recovery/MAIL-CONTRACT-AY.json
```
