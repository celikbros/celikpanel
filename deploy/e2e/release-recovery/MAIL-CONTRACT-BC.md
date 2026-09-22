# BC: completion evidence before renewal acknowledgement

Source `24ebecf353b2fd77fdddecd14202199e51953e14`; native test SHA-256
`6116b8ef1bae4dac2b540aa173156a05908f19e3866c9b773628e66af64178f6`.
[Lifecycle record](MAIL-CONTRACT-BC.json) and
[dependent acknowledgement record](MAIL-ACK-BC.json) bind the same guarded
Debian 13 QEMU cell, source, test binary, exact operation and boot.

Initial publication, actual queued renewal, independent SMTP/IMAP handshakes
after orderly reboot, durable receipt and owner-selected certificate preservation
passed. Four separate native processes then exercised controlled post-publication
failure, fresh renewal polling, generic recovery refusing an owner edit, and
explicit fixture-owner resolution followed by same-request recovery.

The fresh polling process observed the already-selected queued leaf but preserved
the pending file and unresolved canonical ledger byte-for-byte. It returned the
specific completion-unverified result and created no execution manager. Only
after the exact selected receipt's operation succeeded did actual renewal polling
acknowledge the queue. Both native listeners then served the new trusted leaf.
The request remained `1bf47bd647da500d893f203ee9277cbf` through fault and recovery.

Full Agent race suite passed in 199.355 s; final native-test assertions were
compiled and vetted after strengthening their exact error/unchanged-ledger proof.
Installed management binaries were absent. The test still executes shared Agent
code; independent executor enrollment, public ACME, power loss, initial native
configuration-transition recovery and the complete platform/workload matrix are
not established. All P0 items retain open acceptance work. Original native logs
retain the Postfix spool resolv.conf ownership warning.

No installed owner panel changed. Private fixture state, original logs and
intents are retained. The evidence verifier checks consistency, not attestation.
