# Native BIND rollback-before-write trial — 2026-09-25

This is one additional P0.4 / constitutional invariants 1, 2 and 3 trial.
The tested source commit was b34db57, whose only change after the first trial
was documentation. A fresh disposable Debian 13 QEMU guest used a managed
PowerDNS source; its isolated peer fixture was Arch. No installed panel was
accessed or updated.

Cell: bind__rolled-back__before-write__standalone__peer-reachable.
The tagged real Agent was killed immediately before writing the rolled-back
journal checkpoint. The result proves SIGKILL, exit 137, process reaping and
the exact boundary marker. The ordinary Agent restarted, and two same-request
retries plus two read-only recovery probes agreed on target_converged with BIND
active. The result and safety status were passed with no verification,
diagnostic or safety failures. BIND answered authoritative UDP and TCP
queries, and Agent and Panel were reachable in all 31 post-recovery stability
samples spanning 30 seconds.

The full result.json, kill-proof.json and transcript.jsonl were copied outside
the deleted VM overlay to
/var/tmp/cp-dns-rollback-20260925/evidence/celikpanel-dns-before-write-evidence.tar.gz
on the local WSL test host. SHA-256 values:

| Artifact | SHA-256 |
| --- | --- |
| Archive | 1788b476d77178954fd032a0b72909e345853200dd3a5408f7f19b5206cc5ab0 |
| Result | 44df96745a99de610b44a547a6b58725469d91adf7300ea93050595f70bf037e |
| Kill proof | d3da2d61b4ff6150212f9e420694364b63357bebdb630f479b62321d2c9bce6f |
| Transcript | 602bbd14ca1b9e320f910a8fcb6c0b1859140e58ad892b17efbbbc46c220cecd |

This trial introduced no persisted production schema transition. It exercised
the existing DNS switch journal v1 and recovery probe v1. Recovery was the
same-request, Agent-mediated forward convergence to BIND. It does not prove
uninterrupted DNS during transition, reboot/power-loss recovery, an
Agent-independent inverse, owner-edit races or paired/unreachable-peer
behavior. The remaining matrix cells and P0.4 acceptance stay open.
