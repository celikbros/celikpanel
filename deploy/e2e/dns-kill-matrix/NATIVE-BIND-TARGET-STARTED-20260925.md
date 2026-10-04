# Native BIND target-started trial — 2026-09-25

This is one additional P0.4 / constitutional invariants 1, 2 and 3 trial
on source commit 3e5baaf, whose change after the first three trials was
documentation only. A fresh disposable Debian 13 QEMU guest used a managed
PowerDNS source, with an isolated Arch peer fixture. Installed panels were
not accessed or updated.

Cell: bind__target-started__after-write__standalone__peer-reachable.
The tagged real Agent was killed just after the target-started journal
write. The result proves SIGKILL, exit 137, process reaping and the exact
journal boundary. The ordinary Agent restarted. Two same-request retries
and two read-only recovery probes agreed on target_converged with BIND
active. Result and safety status were passed with no verification,
diagnostic or safety failures. BIND answered authoritative UDP and TCP
queries, and Agent and Panel were reachable in all 31 post-recovery
stability samples across 30 seconds.

The full result.json, kill-proof.json and transcript.jsonl are archived
outside the deleted VM overlay at
/var/tmp/cp-dns-rollback-20260925/evidence/celikpanel-dns-target-started-evidence.tar.gz
on the local WSL test host. SHA-256 values:

| Artifact | SHA-256 |
| --- | --- |
| Archive | dd6d1dbc113d4ffc40e6f7e0942a158caf5b51b66e20d4ae7d6685aef290cf91 |
| Result | ae80bb212fa0021a7d7402ef8403b0b4d1c8ed5973558ee90a83ad488dc296e1 |
| Kill proof | 42b450c4644fef0411a5aa589172ea83f20c37f4686d533bf29608bc36d27f5a |
| Transcript | 62668568194b2caeb7497bcb9032a945aeeeae198c13299a8f2829554acccc46 |

No persisted production schema changed. This trial exercised the existing
DNS switch journal v1 and recovery probe v1. Recovery was Agent-mediated
forward convergence under the same request. It does not prove uninterrupted
DNS at the cut, reboot/power-loss recovery, an Agent-independent inverse,
owner-edit races or paired/unreachable-peer behavior. The remaining matrix
cells and P0.4 acceptance stay open.
