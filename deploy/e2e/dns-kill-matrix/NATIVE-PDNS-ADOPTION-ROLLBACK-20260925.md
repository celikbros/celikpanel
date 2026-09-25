# Native PowerDNS adoption rolled-back before-write trial — 2026-09-25

P0.4 / constitutional invariants 1, 2 and 4. Commit `9e7b060` was built into ordinary and `dns_kill_matrix`-tagged Agent binaries and run in fresh disposable Debian 13 and Arch QEMU guests. The measured Debian guest started with an external, unreceipted, authoritative PowerDNS/SQLite service and sealed source preimage. The Arch guest was an isolated fixture peer, not a configured secondary. No installed panel was changed.

The new runnable cell was `pdns-adopt__rolled-back__before-write__standalone__peer-reachable`. An injected precursor first entered the adoption rollback sequence; the tagged Agent was killed immediately before writing the terminal `rolled-back` checkpoint. The retained journal phase was `rolling-back`. The result proves SIGKILL, exit 137, process reaping and the exact boundary marker. The ordinary Agent restarted and the **same request converged forward to adopted PowerDNS**. The final classification was `target_converged`, not `rolled_back_source_serving`. Result and safety status were `passed`; general, verification, diagnostic and safety failures were empty. Agent, Panel and authoritative UDP/TCP DNS were healthy in all 31 samples over 30 seconds after recovery.

The full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-pdnsadopt-20260925/rollback-evidence.tar.gz`; the sealed source proof, external preimage and package preinstall proof are in `/var/tmp/cp-dns-pdnsadopt-20260925/rollback-source-evidence.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Result archive | `5fc8d8674444cc55123f85d2bdfe54d53621f08e67fca8115d99fb26c7eaefa4` |
| Source archive | `462cb2d94c077a7837a77c1f20a924b4dd3b61aec4a6789cc6d083293ecabd76` |
| Result | `d6025ccd9caefb929459c1a1ff71298a84a47d98a8846d4b506d227456b4286d` |
| Kill proof | `5c193c88d1bf89644f5d8c715a55998c18b1b334f209101f6aef1a2c30a26aba` |
| Boundary marker | `074a3d6bf5c80b490af69ab87e47ec0ed41646f63939e0dff8090222e477943c` |
| Transcript | `dd81941a0ce216dca6f900de7c414c66851b9a52056bd9b1842f48dc2b409b6e` |
| Source proof | `6f0315350b34bd8a4fa45787334ef30c8951b892042dd8a5d24b1cd93ae5d5f5` |
| External preimage | `5c46e76bcba8edee2c2273af065cafd5881917485d94285e6034c2395245b862` |
| Package preinstall proof | `d5324a5888b6fc5fcc8868821101d3858a3e618a93c22b991c3fe52fc849cf40` |

This adds one runnable `pdns-adopt` cell. Its success establishes same-request recovery from a retained `rolling-back` journal to an authoritative adopted PowerDNS target. It does **not** establish completion of the inverse, terminal journal retirement, continuous DNS during the cut, owner-edit exclusion, paired behavior, reboot/power loss or an Agent-independent executor. No persisted v1 schema changed. P0.4 remains open. VM overlays, temporary binaries and extra image hardlinks were removed after capture; pinned images and both proof archives were retained.
