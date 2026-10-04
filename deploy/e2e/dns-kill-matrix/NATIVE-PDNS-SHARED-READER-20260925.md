# Native PowerDNS shared-reader regression — 2026-09-25

P0.4 / constitutional invariants 1, 2 and 3. The working tree later recorded as commit `df155f5` was built into ordinary and `dns_kill_matrix` Agent binaries and run in fresh disposable Debian 13 and Arch QEMU guests. The Debian guest began with an external, unreceipted, authoritative PowerDNS/SQLite source. The Arch guest was an isolated fixture peer, not a configured secondary. No installed panel was changed.

The existing `pdns-adopt__rolled-back__before-write__standalone__peer-reachable` cell was repeated after Agent database inspection and the independent recovery observer adopted one shared no-follow reader. The tagged Agent was killed at the boundary immediately before writing the terminal `rolled-back` checkpoint. Exit 137 and the retained `rolling-back` journal were proved. The ordinary Agent restarted and the same request converged **forward** to adopted PowerDNS. Result and safety status were `passed`; general, safety, verification and diagnostic failures were empty. Agent, Panel and authoritative UDP/TCP DNS were healthy in all 31 samples over 30 seconds.

The full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-shared-reader-20260925/rollback-evidence.tar.gz`; the sealed source proof, external preimage and package preinstall proof are in `/var/tmp/cp-dns-shared-reader-20260925/rollback-source-evidence.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Result archive | `6928b7713cf93b12a4c76d76a1cf33fb595659d13355be479b344d001fbc6956` |
| Source archive | `fbb4c68bb23ae0ed4c7caf33ca4c3ee4ab73e7d6e5f88ea478e1b4bb0e825ace` |
| Result | `54ed337832b75d4473791cbbf66bc096db28fc5b19584d695fd5d88eaf69d75c` |
| Kill proof | `7fac4031ac9fd842ea0353cbb51c918a9d3e3ad3b941f0687d4f2d1a605c1258` |
| Boundary marker | `1566d5f327890346025f73f3f4d7e98cd117dd3c2534dc7dab1bb9874ec1b4ac` |
| Transcript | `767130e565ce2b2d17b5b61632d25130c2dc8653f27b32fbd5150c7146fd7436` |
| Source proof | `6f0315350b34bd8a4fa45787334ef30c8951b892042dd8a5d24b1cd93ae5d5f5` |
| External preimage | `5c46e76bcba8edee2c2273af065cafd5881917485d94285e6034c2395245b862` |
| Package preinstall proof | `d5324a5888b6fc5fcc8868821101d3858a3e618a93c22b991c3fe52fc849cf40` |

This repeats one already-counted Agent-mediated cell. It does not execute the independent `recovery dns-switch-status --quiesced` command, prove native source rollback or journal retirement, exclude later owner edits, or cover paired DNS and reboot/power loss. No persisted v1 schema changed. P0.4 remains open. VM overlays, temporary binaries and extra image hardlinks were removed after capture; the two proof archives were retained.
