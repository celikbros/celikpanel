# AY: owner configuration blocks certificate publication

Exact source: `32b3b548d4fca64b27638a8ac380aa63e4fcbade`.
Test binary SHA-256: `c3816e91d7c695f7bfbfaccefb06d5e1f7d68e972fccbeb7eaf451cd91308d5d`.
[Bounded record](MAIL-OBSERVATION-AY.json), bound to retained [AY](MAIL-CONTRACT-AY.md).

On 2026-09-22 the guarded disposable Debian guest ran the actual Agent publication
preflight under its durable mutation lease with real Postfix/Dovecot observation.
The test verified the accepted configuration, added a deliberate fixture-owner
comment to the managed Dovecot fragment, and called the actual certificate source
publisher. It refused before staging: no added version or changed current link,
owner edit and pending renewal preserved, same trusted native SMTP/IMAP leaf.
The fixture owner explicitly removed only the test edit and the same preflight
passed. The test recorded its operation as failed, not a published certificate.

Independent OpenSSL connections afterward verified trusted hostname/leaf at
SMTP 587 STARTTLS and IMAPS 993. Native daemons stayed active; no installed Panel
or Agent executable existed. Final queue/configuration hashes match prior AY.
The controller verified QEMU process, SSH, nonce/DMI and systemd identity.
A controller serialization error prevented saving its initial command output;
the command was not rerun. Its single systemd execution/result and subsequent
independent witness were captured read-only. The lab is stopped, disks retained.

This proves a pre-publication owner-edit refusal and explicit fixture resolution.
It does not prove initial issuance/renewal completion on this source, protection
against root changes after observation, independent renewal or interruption after
publication. P0.4/P0.5 remain partial. These logs are consistency evidence, not
host attestation. No installed owner server changed.
