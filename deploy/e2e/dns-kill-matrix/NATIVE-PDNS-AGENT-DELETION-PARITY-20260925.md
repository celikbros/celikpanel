# Native Agent/independent deleted-zone parity trial — 2026-09-25

P0.4; constitutional invariants 1, 2 and 3. A fresh disposable
Debian 13/Arch QEMU pair repeated the already-counted
pdns-adopt__intent__after-write__standalone__peer-reachable cell with an
unreceipted external PowerDNS 4.9.17 SQLite authority, an active parent
and one frozen deleted child. This run used the corrected Agent that no
longer treats REFUSED or non-authoritative NXDOMAIN as deletion proof.
The ordinary Agent binary SHA-256 was
48896be2fd1de217001023d9d56aaa02800a4d0e66e725f0163a7a8024c3fe19;
the independent recovery binary SHA-256 was
81b2b157c55e39b7b6ab31ba01cc278f1dc565e9e9ccfab7e1fc50d8b898336a.
No installed panel was touched.

The tagged Agent was killed at the intent/after-write boundary and the
controller proved SIGKILL exit 137. Before the ordinary Agent restart,
read-only recovery dns-switch-status --quiesced observed the accepted
request under release and host mutation locks. Its untruncated output
SHA-256 was
fab677caa294aad70a57c504866c1ba8db28925acfde684e582bef24afcf6f82.
It verified the active parent and strict negative SOA answers for 1/1
deleted child over both UDP and TCP at the native PowerDNS endpoint.
The same request then converged forward through the corrected ordinary
Agent. Result and safety statuses passed, all failure arrays were empty,
and 31/31 Agent/Panel/authoritative DNS samples were healthy.

Evidence archive on the local fixture host:
/var/tmp/cp-pdns-parity25/evidence.tar.gz
SHA-256 f2b67ee041b7d44ca0a228972825dbe0f218e78c738dc05b8f8d1e94c7352a5c.
It contains the exact scenario/controller argv, source and preinstall
proofs, external PowerDNS preimage, boundary marker, exit-137 proof,
full result and transcript. Both guests were stopped through QMP and
the validated cell directory was torn down.

This run supports parity for one real parent/absent-child answer under
one fault boundary. It does not prove response behavior for REFUSED on
a real configured owner server, an Agent-independent inverse,
uninterrupted service at the cut, later owner edits, reboot/power loss,
paired transfer or complete P0.4. No persisted DNS receipt, journal or
ledger schema changed and no recovery write authority was added.