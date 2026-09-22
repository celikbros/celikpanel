# Failed native mail renewal budget: Debian BE

Source `bbd81cc2d108fd0d79fd7fba6b144571bc6800e3`, standalone helper
`a99cb644dd235e2dfe477768994167f1ea0f99ac4db050f95c973f06e56d2102`.
[Machine-readable evidence](MAIL-FAILED-BUDGET-BE.json).

In the guarded Debian BE QEMU fixture, Panel and Agent binaries were absent.
The fixture queued a new CA-signed mail certificate and explicitly introduced a
conflicting native Postfix hostname without reloading either mail service.
The ordinary renewal timer was stopped for deterministic, independently launched
oneshot helper processes; this is not evidence of automatic timer dispatch.

Three fresh helper processes recorded failure for the same operation
`79066f5075f0b92fe8ce964819030c60`, with durable attempts 1, 2 and 3. Each left
the selected certificate, pending queue and owner-modified native settings
unchanged. A fourth automatic-mode process stopped at the budget, reported the
exact owner continuation and did not rewrite the ledger. A wrong-owner request
also left ledger, queue and native settings unchanged. Trusted SMTP 587 and
IMAP 993 still served the previous certificate.

The fixture owner then explicitly restored the original Postfix file inode and
ran `--retry-failed` for that exact operation. Attempt 4 succeeded, acknowledged
the queue and served the new leaf on both CA/hostname-verified listeners. Foreign
operation history and original configuration/hook/unit bytes, inode, owner, mode
and mtime were preserved. The original enabled timer was returned to active.
The owner-conflict file, logs and before-image remain in the fixture evidence.

This establishes terminal failed-operation admission, fresh-process persistence,
owner-change preservation and explicit same-operation continuation. It does not
establish preselection interruption recovery, cross-build adoption, initial
production enrollment, reboot/power-loss or the complete P0.3/P0.5 matrix. No
installed user panel was accessed or updated.
