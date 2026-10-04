# Native BIND target-started before-write interruption ? 2026-09-25

P0.4 / constitutional invariants 1, 2 and 3. Commit `d4b8c0e` was built into ordinary and `dns_kill_matrix`-tagged Agent binaries and exercised in a fresh disposable Debian 13 QEMU VM with a real managed PowerDNS source and an isolated Arch peer VM. No installed panel was changed.

The new runnable cell was `bind__target-started__before-write__standalone__peer-reachable`. The tagged Agent was killed immediately before the target-started journal checkpoint write with exit 137 and positive kill proof (PID 3646). The ordinary Agent restarted and the same request converged to BIND. Result and safety status were `passed`, recovery classification was `target_converged`, and general, verification, diagnostic and safety failure lists were empty. Agent, Panel and authoritative UDP/TCP DNS were healthy in all 31 samples over 30 seconds.

Full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-guard-20260925/evidence/target-started-before-write.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Archive | `5abcbd5589b5a39d0deea2c3070ca94585fb93e13a8fbfc013ce7d87c215015a` |
| Result | `1ce4bf2b2e327c8907f1b62533ef1873e5dcda2d0d1f5a25f983b08a9e1bc86a` |
| Kill proof | `991a1b24431eef408ea8dda6d879cd87d7ed3a9d4eb97e1a36b75979e5302926` |
| Transcript | `520f90f4bbecab2c5c5994dd05b63c113dceee40aca4ad48176528b37fb5606e` |

This is the sixth passed runnable BIND cell in the S-1 matrix. It does not prove uninterrupted DNS during the cut, paired-peer behavior, reboot/power-loss recovery, owner-edit safety, or an Agent-independent inverse. The v1 journal and ledger schemas were unchanged. VM overlays, temporary binaries and extra image hardlinks were removed after evidence capture; pinned source images and evidence were retained.
