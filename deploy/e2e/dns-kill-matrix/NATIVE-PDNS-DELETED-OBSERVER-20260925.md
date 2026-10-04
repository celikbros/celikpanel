# Native PowerDNS deleted-child quiesced observer — 2026-09-25

P0.4; constitutional invariants 1, 2 and 3. This fresh disposable
Debian 13/Arch QEMU pair repeated the already-counted
pdns-adopt__intent__after-write__standalone__peer-reachable cell with the
optional active-parent/deleted-child scenario. Debian began with an
unreceipted external PowerDNS 4.9.17 SQLite authority. Arch was an
isolated fixture peer, not a configured DNS secondary. No installed panel
was touched. The source-built root-owned independent recovery binary had
SHA-256 44984e583595468c95f45a49d127ecfe74321875a9275f4b565971d1c5c98e74.
Only the disposable fixture received an empty root-owned release lock.

The tagged Agent reached the intent journal after-write boundary and was
killed with SIGKILL; the controller proved exit 137. Before starting the
ordinary Agent, the independent read-only
recovery dns-switch-status --quiesced --request-id
6db68749d7b24c88419ecbda31e11413 command exited zero in 8.53 seconds
with untruncated output (SHA-256
fab677caa294aad70a57c504866c1ba8db28925acfde684e582bef24afcf6f82).
It reported accepted-active, journal phase intent and no recorded worker at
that point in time. With the release and host mutation locks held, it
matched frozen config/ownership/database evidence, a read-only SQLite
zone/peer/integrity transaction, a stable native PowerDNS process and its
TCP/UDP port-53 listeners. One active parent returned its exact
authoritative SOA over both transports. The one frozen deleted child
returned strict negative SOA answers over both transports (1/1). Evidence
and unit properties were reread after the network checks.

The subsequent Agent restart removed the journal before the later
read-only pre-retry observation; that observation reported no journal and
an exact ledger job marked failed. It did not establish rollback. The
ordinary same-request retry then converged forward to target_converged.
The cell's result and safety statuses were passed, all four failure
arrays were empty, and 31/31 post-recovery samples across 30 seconds were
healthy. This is another zone-vector repetition of an existing matrix
cell, not a new phase result.

Evidence archive on the local fixture host:
/var/tmp/cp-pdns-del25/observer-evidence.tar.gz
SHA-256 c3a1f9db0681584a4bbe62a9f6681a1fd62595d30ea12d0e4434dcdd7369c3e3.
It includes the exact scenario and controller command, source and preinstall
proofs, external PowerDNS preimage, boundary marker, exit-137 kill proof,
full result and transcript. Both guests were stopped by QMP and the
validated cell directory was torn down.

This proves that the independent observer's strict deleted-zone SOA
predicate can run against this real PowerDNS authority at the immediate
post-kill boundary. It does not confer inverse authority or prove an
Agent-independent restoration, loaded-config-to-answer causality beyond
the bounded observations, later owner edits, reboot/power loss, paired
transfer, or complete P0.4. No persisted DNS switch schema changed, and
no inverse command was exposed.