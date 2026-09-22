# AY: native mail version observation and configuration readback

On 2026-09-22, the retained disposable Debian AY cell ran the actual Agent test
binary from `b3b89ce19d18116737c3f06720945d0eeeabd52e`, SHA-256
`d3485fee86c6baeb9ffcae47e5324aaa04e4850d5fab06822f741b303409eb52`.
[Bounded record](MAIL-DIALECT-AY.json) is linked by digest to the prior
[AY mail acceptance](MAIL-CONTRACT-AY.md). This is evidence consistency, not host
attestation. QEMU/SSH/nonce/DMI guards preceded execution; the stopped lab disks
and private intent/build/start/result records remain retained.

The actual shared host mutation lock was held. With real Postfix/Dovecot serving
trusted SMTP 587 STARTTLS and IMAPS 993, a private fixture executable replaced
only the version-command lookup and exited 75. Actual TLS reconciliation refused
unknown dialect before any later command or configuration work. Restoring the
real executable yielded Dovecot 2.4; actual retained-plan verification passed
against native commands and the shared configuration contract. Configuration,
mutation ledger, pending renewal, current symlink and the owner-selected served
leaf remained unchanged. The test invoked neither certificate publication nor
service reload.

The fixture recreated only the absent volatile `/run/celikpanel` and empty host
lock with established identities. No installed Agent/Panel binary was present.
This test does not establish independent renewal runtime initialization. The
existing Postfix warning about `/var/spool/postfix/etc/resolv.conf` ownership is
retained; it was not repaired or treated as mail delivery certification.

This advances P0.4/P0.5 observation and owner preservation. No persisted schema
or configuration byte transition occurred. It is a real executable observation
failure, not a native daemon failure, crash recovery, complete effective Dovecot
include or indexed SNI verification, mail delivery/authentication certification,
or independent renewal acceptance. Those remain open.

Run:

```sh
python3 deploy/e2e/release-recovery/verify_mail_contract.py deploy/e2e/release-recovery/MAIL-DIALECT-AY.json --base deploy/e2e/release-recovery/MAIL-CONTRACT-AY.json
python3 -m unittest discover -s deploy/e2e/release-recovery -p test_verify_mail_contract.py
```
