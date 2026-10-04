# Native PowerDNS SQL observer trial — 2026-09-25

P0.4, constitutional invariants 1, 2 and 3. A fresh disposable Debian 13/Arch
QEMU pair repeated the already-counted
`pdns-adopt__rolled-back__before-write__standalone__peer-reachable` cell.
Debian served an external authoritative PowerDNS/SQLite source before the
product Agent ran. Arch was an isolated fixture peer, not a configured
secondary. No installed server was touched. The root-owned recovery binary
built from this source had SHA-256
`362827a1976028b63ae29ef81d274760a9fd148012848ac544df918ec3c5e197`.
The fixture alone lacked the root-owned release lock directory; the trial
created that empty 0700 directory and 0600 lock before invoking the observer.

The tagged Agent reached the rollback journal's before-write boundary and was
killed by SIGKILL. The kill proof records exit 137 and reaping. Before Agent
restart, the independent command held release and host locks for exact request
`ae0ad45f99e986124bce4c6c992e917f`. It reconstructed the frozen adoption
manifest and returned zero after two secure database-byte reads bracketing a
read-only SQLite transaction. The transaction matched the frozen zones and peer
rows and passed `quick_check`. The native `pdns.service` process owned local
TCP/UDP port 53 between reads. The observer classified the retained journal
as accepted-active, rolling-back, with no recorded worker at that instant; it
did not authorize an inverse. The same request then converged forward through
ordinary Agent retry. Result and safety status passed, all failure arrays were
empty, and Agent, Panel and authoritative UDP/TCP DNS were healthy in the
30-second stability sample.

The first fresh trial was retained separately. Its observer exited unavailable
because the fixture lacked `/var/lib/celikpanel-release-transaction/transaction.lock`;
it did not reach the new SQL proof. The first archive is
`/var/tmp/cp-dns-native-sql-20260925/first-trial-evidence.tar.gz` (SHA-256
`ac74dd8e73fb85d87ce6e11b9c868dbf75da0585621e439ae65c554cdbd23450`).
The passing archive is
`/var/tmp/cp-dns-native-sql-20260925/second-trial-evidence.tar.gz` (SHA-256
`9f7d728339404bd40602419e983855ec2cf2a8935cc5c3d471a7cb2d82c21dd0`).
Both are private local evidence. The passing result records the exact observer
argv, return code and output digest
`ce17042a38fc24140dfcd4419df470bacdfe179fe51601b803db51eec7f4d655`.
The guests were stopped and torn down after archiving.

This repeats an existing matrix cell and proves only the read-only SQL observer
at this interruption boundary. It does not prove authoritative zone answers at
the observation instant, loaded configuration, owner-edit race exclusion, safe
Agent-independent inverse execution, paired transfer, reboot/power loss or
uninterrupted DNS. Persisted journal, state and ledger formats remain v1; P0.4
remains open.