# Native BIND source-stopped trial — 2026-09-25

This is one additional P0.4 / constitutional invariants 1, 2 and 3 trial
on source commit 9970067, whose change after the first two trials was
documentation only. A fresh disposable Debian 13 QEMU guest used a managed
PowerDNS source, with an isolated Arch peer fixture. Installed panels were
not accessed or updated.

Cell: bind__source-stopped__after-write__standalone__peer-reachable.
The tagged real Agent was killed just after the source-stopped journal write.
The result proves SIGKILL, exit 137, process reaping and the exact journal
boundary. The ordinary Agent restarted. Two same-request retries and two
read-only recovery probes agreed on target_converged with BIND active. Result
and safety status were passed with no verification, diagnostic or safety
failures. BIND answered authoritative UDP and TCP queries, and Agent and
Panel were reachable in all 31 post-recovery stability samples across
30 seconds.

The full result.json, kill-proof.json and transcript.jsonl are archived
outside the deleted VM overlay at
/var/tmp/cp-dns-rollback-20260925/evidence/celikpanel-dns-source-stopped-evidence.tar.gz
on the local WSL test host. SHA-256 values:

| Artifact | SHA-256 |
| --- | --- |
| Archive | 32ad9c5ed2f17b60b2fe60a9c22dd2d0677678f98419423a10b7e27e48363666 |
| Result | bb2bcdfef0d0ea8cb10a167772b83e5e4db67c75575bf3109bcf16431817a357 |
| Kill proof | 77b37c65045d9ecacb941818ab635dea7e3ea988e6a3eae54546ba58fdc2bbf2 |
| Transcript | 165d36e2130cc0baf34fdb7b707852b672965a5f48cc0d16fef9ec93580f1e34 |

No persisted production schema changed. This trial exercised the existing
DNS switch journal v1 and recovery probe v1. The recovery was
Agent-mediated forward convergence under the same request. It does not prove
uninterrupted DNS at the cut, reboot/power-loss recovery, an Agent-independent
inverse, owner-edit races or paired/unreachable-peer behavior. The remaining
matrix cells and P0.4 acceptance stay open.
