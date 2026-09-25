# Native BIND source-stopped before-write trial — 2026-09-25

This is one additional P0.4 / constitutional invariants 1, 2 and 3 trial.
The Go binaries were built from 57db78a. The test harness included the
run-prepared action committed with this report; it was used for this trial
instead of a manually copied launcher. A fresh disposable Debian 13 QEMU
guest had a managed PowerDNS source and an isolated Arch peer fixture.
Installed panels were not accessed or updated.

Cell: bind__source-stopped__before-write__standalone__peer-reachable.
The tagged real Agent was killed immediately before the source-stopped
journal write. The result proves SIGKILL, exit 137, process reaping and the
exact boundary marker. The ordinary Agent restarted. Two same-request
retries and two read-only recovery probes agreed on target_converged with
BIND active. Result and safety status were passed with no verification,
diagnostic or safety failures. BIND answered authoritative UDP and TCP
queries, and Agent and Panel were reachable in all 31 post-recovery
stability samples across 30 seconds.

The full result.json, kill-proof.json and transcript.jsonl are archived
outside the deleted VM overlay at
/var/tmp/cp-dns-rollback-20260925/evidence/celikpanel-dns-source-before-evidence.tar.gz
on the local WSL test host. SHA-256 values:

| Artifact | SHA-256 |
| --- | --- |
| Archive | c7d8ae5f7036bf38f24b5b66e0e2347a239807af05837517b0f857a4a9abe91e |
| Result | b00ac6853a65f32e4d6eb8d75b2d18a210fc05b9e71dd4f9079ace71c278c37b |
| Kill proof | f959aae764f2d29803a946d715f20bd76faf1039c44345b3ba28fa3e50abea08 |
| Transcript | 61c7db23c2e6ddcf378ba20f02c1723500d700e71d0425090d718ed06f79aa18 |

No persisted production schema changed. This trial exercised the existing
DNS switch journal v1 and recovery probe v1. Recovery was Agent-mediated
forward convergence under the same request. It does not prove uninterrupted
DNS at the cut, reboot/power-loss recovery, an Agent-independent inverse,
owner-edit races or paired/unreachable-peer behavior. The remaining matrix
cells and P0.4 acceptance stay open.
