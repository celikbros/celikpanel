# Native exact-request DNS status trial — 2026-09-25

P0.2/P0.4, constitutional invariants 2, 3 and 6. A fresh disposable
Debian 13/Arch QEMU pair repeated the already-counted
`pdns-adopt__rolled-back__before-write__standalone__peer-reachable` cell.
Debian had an unreceipted authoritative PowerDNS/SQLite source; Arch was an
isolated fixture peer, not a configured secondary. The current-source
root-owned recovery binary (SHA-256
20b61fea88b349075b49d33a3d47116c69bf29385339bc8c7d491673a7356fc7)
ran with the fixed quiesced command and the controller's verified exact
32-hex request ID. No installed panel was touched.

The tagged Agent was stopped at the rollback journal's before-write boundary.
The kill proof records SIGKILL, exit 137 and reaping. The retained journal was
rolling-back for request ae0ad45f99e986124bce4c6c992e917f. Before
Agent restart, the independent command matched that request's retained journal
and reported accepted-active, no recorded worker at that instant, frozen
PowerDNS database bytes on two reads, and a stable `pdns.service` process
owning TCP/UDP port 53. After ordinary Agent startup but before any
same-request RPC retry, it reported **no journal and exact ledger status
failed** for the same request. It exited zero without claiming DNS rollback,
completion or current service health. The prior Python probe was
indeterminate. The same request subsequently converged forward through Agent
retry. Result and safety status passed, all failure arrays were empty, and
Agent, Panel and authoritative UDP/TCP DNS were healthy in 31/31 samples over
30 seconds.

The private local evidence archive is
`/var/tmp/cp-dns-native-observer-20260925/exact-request-evidence.tar.gz`
(SHA-256 8b19d1adc5ccd73f8ac0d030e57a921644d5ac87cd0d21633a35b58c02c7254f,
mode 0600). It contains the result, kill proof, boundary marker, transcript,
controller argv and sealed source/preinstall evidence. The observer executable
and its exact argv/output digest are recorded in the result. This is a
repeat of an existing cell, not new matrix coverage. The trial proves the
read-only visibility improvement at this one timing boundary; it does not
prove an Agent-independent inverse, safe owner-edit exclusion, SQLite/zone
state, paired transfer, reboot/power loss, or uninterrupted DNS. Persisted
DNS journal, state and ledger formats remain v1; P0.2/P0.4 are not complete.