# Independent mail runtime after reboot

P0.5 bounded native acceptance, 2026-09-22. Source `75ba5f544113997afcdf4e3a38780dedb1b1ffd6`.
[Machine evidence](MAIL-RUNTIME-BE.json) binds the exact helper/test binaries and
original transcripts. The verifier checks consistency, not remote host attestation.
Private originals and VM disks are retained. Neither installed owner server was changed.

BD follows the [earlier runtime refusal](MAIL-EXECUTOR-BD.md). After that same
reboot, the new helper creates only the absent volatile runtime, validates the
prior completed operation and acknowledges its same-leaf queue. The existing
ledger, native configuration and selected generation are unchanged; real SMTP
and IMAP still serve the expected trusted leaf. A separate fixture-owner change
to directory mode 0755 causes refusal with queue, inode, owner content, ledger
and native configuration preserved. Only explicit fixture-owner restoration of
mode 0750 allows acknowledgement, preserving the same inode and owner file.

BE is a fresh guarded QEMU Debian13 fixture with real Postfix 3.10.13 and Dovecot
2.4.1. Initial issuance prepares a second CA-signed source without renewing it.
After an orderly reboot, `/run/celikpanel` and both installed management binaries
are absent. A no-work helper invocation creates nothing. Separate native oneshot
units then queue and deploy the new source: runtime is root:celikpanel 0750,
configuration bytes/mtimes are unchanged, the exact selected receipt matches the
succeeded ledger job, and the queue is absent. Trusted SMTP and IMAP handshakes
present the new source leaf.

This is actual postboot new-leaf publication, not merely same-leaf acknowledgement.
It is manually invoked through guarded fixture oneshots. Production enrollment,
automatic scheduling, public ACME issuance, power-loss durability and independent
recovery of interrupted renewal are not established by these trials. Existing
full fault/workload/absence acceptance remains open.
