# Native BIND rollback-boundary trial — 2026-09-25

This is one P0.4 / constitutional invariants 1, 2 and 3 trial, not acceptance
of the DNS fault matrix or independent DNS recovery. The tested source commit
was 3315c6e. The test used the current, unmodified source bundle in a fresh
disposable Debian 13 QEMU guest with a managed PowerDNS source; its isolated
peer fixture was Arch. It did not access or update an installed panel.

Cell: bind__rolled-back__after-write__standalone__peer-reachable.
The tagged real Agent was killed at the rolled-back journal write boundary.
The result proves SIGKILL, exit 137, process reaping and an exact boundary
marker. After the ordinary Agent restarted, two same-request retries and two
read-only recovery probes agreed on target_converged with BIND active. The
controller classified the result and safety as passed, with no verification,
diagnostic or safety failures. After recovery, BIND answered authoritative
queries over UDP and TCP; the Agent and Panel were reachable in every one of
31 stability samples spanning 30 seconds. This trial does not establish
uninterrupted DNS during the transition or reboot/power-loss behavior.

The full result.json, kill-proof.json and transcript.jsonl were saved outside
the deleted VM overlay at
/var/tmp/cp-dns-rollback-20260925/evidence/celikpanel-dns-evidence.tar.gz
on the local WSL test host. SHA-256 values:

| Artifact | SHA-256 |
| --- | --- |
| Archive | 7a43fe43163587bb12994b538bf4f7bfb3b0612d6d02688ecd5b5d997e32972f |
| Result | 2f0396f61f5642736ac3e359b439c82d951b405a1b2f27df8a307e65c032c70a |
| Kill proof | 159388df0398498ce16098886e5534511285ea5796a4b7a2f0bab0d12d18c3f8 |
| Transcript | b5e50952fa596c0fc61ef566e0900f3ac44766135a890a78c92e19287edb9b1c |

This trial introduced no persisted production schema transition. It exercised
the existing DNS switch journal v1 and recovery probe v1. Recovery was the
same-request, Agent-mediated forward convergence to BIND. An Agent-independent
native inverse, owner-edit races, paired/unreachable peers and the remaining
runnable fault cells remain open. The current inventory is 268 runnable and
242 explicit N/A cells; this report accounts for exactly one runnable cell.
