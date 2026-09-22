# BB: native reload-only renewal and retained operation recovery

Source `308cebd384973dc46c3d6ad492459f9199887b0c`; test binary SHA-256
`704dce24575056b661be37943ef41704d05a25d4addc7f73f1927a808c9c4fd9`.
[Lifecycle record](MAIL-CONTRACT-BB.json) and dependent
[recovery record](MAIL-RELOAD-BB.json) bind original native unit journals to the
same guarded Debian 13 QEMU fixture, source, binary and boot identity.

Initial publication and actual scoped queued renewal passed against native
Postfix/Dovecot 2.4. Renewal preserved configuration bytes and modification times.
Independent OpenSSL probes after orderly reboot verified both native listeners
before the test runtime lock was recreated. Installed Panel/Agent binaries were
absent. Exact persisted receipt and deliberate owner certificate selection
preservation also passed. Full Agent race suite: 196.933 s; vet passed.

Three subsequent native units in separate processes exercised the actual durable
publication and generic startup recovery paths. The first inserted an explicit
fixture-owner comment after durable certificate publication and returned a
controlled activation error. Its exact active intent and selected receipt were
retained. The second process refused to overwrite the owner edit, retaining the
same request `1a2b299bf64c88f4b321512b7d4a5a4f`. The third performed an explicit
fixture-owner resolution, then recovered that same operation by reload only.
Configuration contents and modification time stayed unchanged through recovery;
SMTP and IMAP both presented the newly selected trusted leaf afterward.

At the controlled fault and refusal both listeners served the previous trusted
leaf. The test permits either reviewed trusted leaf at that uncertain boundary:
publication is not proof that every daemon has loaded the new selection. This is
not a SIGKILL or power-loss test. It does not establish independent renewal,
public ACME, production hook enrollment, a safe initial fallback configuration
transition, or the full platform/workload/fault matrix. The Postfix spool
resolv.conf ownership warning remains in the original lifecycle log.

No installed owner server changed. Private disks, fixture-owner before/after
configuration, intents and original logs are retained. The verifier checks
record consistency and rejects changed scope, requests, binary, boot, service
results and listener identities; it is not host attestation. P0 items remain
partial with their existing acceptance work open.
