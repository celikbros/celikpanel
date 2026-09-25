# Native Arch BIND target-staged before-write trial — 2026-09-25

P0.4 / constitutional invariants 1, 2 and 4. Commit `3bee0d4` was built into ordinary and `dns_kill_matrix`-tagged Agent binaries and run in fresh disposable Arch and Debian 13 QEMU guests. The measured Arch guest had a proved uninitialized DNS source; the Debian guest was an isolated fixture peer, not a configured secondary. No installed panel was changed.

The new runnable cell was `bind__target-staged__before-write__standalone__peer-reachable`. The tagged Agent was killed immediately before the `target-staged` journal write; the retained checkpoint was `intent`. The result proves SIGKILL, exit 137 and process reaping (PID 1308). The ordinary Agent restarted, retried the same request and converged to BIND. Result and safety status were `passed`, classification was `target_converged`, and general, verification, diagnostic and safety failures were empty. Agent, Panel and authoritative UDP/TCP DNS were healthy in all 31 samples across 30 seconds after recovery.

The full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-arch-20260925/evidence.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Archive | `a803a0a133091e3bb899e69db0777d668a88d94b140fb59d4f25a6bbb4b62aea` |
| Result | `f9675ba0c1121b33c5c1304b709983a090074218e9dee159106db1033d9087dc` |
| Kill proof | `ebda5ef7768d75e912f8cee2221a547b1e215d4a295088065636a2353c8b05fa` |
| Boundary marker | `1af4ce01caff69153c183e516e3ffd2efc478d0259ceff808ff98ac441be97a3` |
| Transcript | `37e58fd1aaf29739639ac6594fc7d3e562dd209b405c440473fb6cb13e8a27c9` |

This adds one new runnable matrix cell and the first measured Arch BIND interruption in this series. It proves recovery and post-recovery native health for an empty source at this early journal boundary. It does not prove DNS continuity through the cut, a pre-existing source's restoration, owner-edit exclusion, paired behavior, reboot/power loss, or an Agent-independent inverse. No persisted v1 schema changed. P0.4 remains open. VM overlays, temporary binaries and extra image hardlinks were removed after capture; pinned source images and this archive were retained.
