# Native PowerDNS deleted-child trial — 2026-09-25

P0.4; constitutional invariants 1, 2 and 3. A fresh disposable Debian 13/Arch
QEMU pair used the pinned official cloud images. Debian ran the real PowerDNS
4.9.17 SQLite authority. The external source had an active parent
s1-kill.test (SOA serial 2026083101) and no old.s1-kill.test zone. The
new --include-deleted-child preparation option placed that deleted child
in the frozen adoption scenario while retaining only the active parent in
the source database. The default one-zone fixture remains unchanged.

The Linux dnswire integration test queried the actual PowerDNS listener
10.0.2.15:53 without recursion. Both UDP and TCP returned the exact parent
SOA; QueryDeletedZoneSOA accepted the child's negative parent-SOA response
and refused to classify the active parent as deleted. The test passed before
and after the measured interruption. Its executable SHA-256 was
c4400a44c9102e8a8176b93b0499c57b11c2185f6e2b989534a5c9d5e2b9b607.
The first invocation against 192.0.2.10:53 failed with connection refused;
that was the wrong fixture interface, not a passing deletion proof.

The measured pdns-adopt__intent__after-write__standalone__peer-reachable
cell retained the intent journal for request
6db68749d7b24c88419ecbda31e11413 after the tagged Agent's SIGKILL.
The controller proved exit 137 and reaping. Ordinary Agent retry of the same
request converged forward to target_converged. Result and safety status
were passed, all failure arrays were empty, and 31/31 post-recovery
Agent/Panel/UDP+TCP DNS samples were healthy. This repeats an existing
matrix cell with a different zone vector; it adds no new phase coverage.

Evidence archive: /var/tmp/cp-pdns-del25/evidence.tar.gz on the local fixture
host, SHA-256
93bd869bdd02da5eaa6cadfbed46db0f27870defaffdcbdce2657ba175680c22.
It contains the source/preinstall proofs, exact scenario, installed binary
hashes, kill proof, retained-boundary evidence, result and transcript. The
native dnswire test output was observed directly (PASS) but was not
included in that archive. Both guests were stopped by QMP and their verified
cell directory was removed; temporary binaries, image links, SSH key and
test log were removed.

This establishes compatibility of the strict negative SOA parser with one
real parent/absent-child PowerDNS configuration and Agent-mediated forward
convergence of the two-zone manifest. It does not prove the independent
observer at the instant after SIGKILL, an Agent-independent inverse, loaded
configuration or answer/socket causality, uninterrupted DNS during the cut,
reboot/power-loss recovery, owner edits, paired transfer or complete P0.4.
The DNS switch journal and ledger schemas remain v1; no recovery command
was exposed and no installed server was touched.