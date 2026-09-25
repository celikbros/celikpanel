# Native PowerDNS adoption shared-rollback regression — 2026-09-25

P0.4 / constitutional invariants 1, 2 and 3. Commit `4044707` was built into ordinary and `dns_kill_matrix` Agent binaries and run in fresh disposable Debian 13 and Arch QEMU guests. The Debian guest began with an external, unreceipted, authoritative PowerDNS/SQLite source. The Arch guest was an isolated fixture peer, not a configured secondary. No installed panel was changed.

The existing `pdns-adopt__rolled-back__before-write__standalone__peer-reachable` cell was repeated after the Agent adopted the shared fail-stop PowerDNS rollback sequence. The injected precursor entered rollback; the tagged Agent was killed immediately before the terminal `rolled-back` checkpoint. Exit 137 and the retained `rolling-back` journal were proved. The ordinary Agent restarted and the same request converged **forward** to adopted PowerDNS. Result and safety status were `passed`; general, safety, verification and diagnostic failures were empty. Agent, Panel and authoritative UDP/TCP DNS remained healthy in all 31 samples over 30 seconds.

The full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-pdnsadopt-cancel-20260925/rollback-evidence.tar.gz`; the sealed source proof, external preimage and package preinstall proof are in `/var/tmp/cp-dns-pdnsadopt-cancel-20260925/rollback-source-evidence.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Result archive | `ef469bf3dc3e17f69b6b4c6c88168ad36f49d7c894262a574aadf23ebc9a69e2` |
| Source archive | `361db0676d86ade12d59881b4eff0f6a7e09a4157bf32293938ad743e90dff82` |
| Result | `a90d9eb961eba55ff9974ce4d06691b3bb37577b154516d0a151c64e46219425` |
| Kill proof | `ba3a7c10e413f7f8e2c0ac1218f4779359160a1b7789639411c245f0b35b24c9` |
| Boundary marker | `48b6ea5cfdd34633977f6c8b4d999318b78d82c20f5646174638fa5bd2423265` |
| Transcript | `9bff81a5db7762e08f0b3d068434a4ad80cdcba6fcfdd3edbbc4fcfa2a64a16b` |
| Source proof | `6f0315350b34bd8a4fa45787334ef30c8951b892042dd8a5d24b1cd93ae5d5f5` |
| External preimage | `5c46e76bcba8edee2c2273af065cafd5881917485d94285e6034c2395245b862` |
| Package preinstall proof | `d5324a5888b6fc5fcc8868821101d3858a3e618a93c22b991c3fe52fc849cf40` |

This is a regression of one already-counted cell, not new matrix coverage. It exercises the changed Agent path at this boundary but does not prove an Agent-independent inverse, completion of source rollback, terminal journal retirement, a native cancellation race, owner-edit exclusion, paired DNS or reboot/power loss. No persisted v1 schema changed. P0.4 remains open. VM overlays and temporary binaries were removed after capture; the two proof archives were retained.
