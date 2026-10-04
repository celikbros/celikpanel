# Native PowerDNS adoption intent after-write trial — 2026-09-25

P0.4 / constitutional invariants 1, 2 and 4. Commit `91e314c` was built into ordinary and `dns_kill_matrix`-tagged Agent binaries and run in fresh disposable Debian 13 and Arch QEMU guests. The measured Debian guest used an external, deliberately unreceipted PowerDNS service with package-owned schema, native `pdns.service`, a real SQLite zone and authoritative UDP/TCP answers. The Arch guest was an isolated fixture peer, not a configured secondary. No installed panel was changed.

The new runnable cell was `pdns-adopt__intent__after-write__standalone__peer-reachable`. The first production adoption request was the tagged measured operation; no setup adoption RPC preseeded panel ownership. The tagged Agent wrote the `intent` journal checkpoint and was then killed. The result proves SIGKILL, exit 137, process reaping and the exact boundary marker. The ordinary Agent restarted and two same-request retries plus two read-only recovery probes agreed on `target_converged`: PowerDNS was adopted and served the frozen zone. Result and safety status were `passed`; general, verification, diagnostic and safety failures were empty. Agent, Panel and authoritative UDP/TCP DNS were healthy in all 31 samples over 30 seconds.

The full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-pdnsadopt-20260925/evidence.tar.gz`; the sealed source proof, external PowerDNS preimage and package preinstall proof are in `/var/tmp/cp-dns-pdnsadopt-20260925/source-evidence.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Result archive | `8dc4f1e70be901918942cf0c8c855ec52c8ecec68f5706864fe963a4993edc43` |
| Source archive | `46d7d576d542c972caf2f56a172097f2f9d5f1d8054d79c06508db7c41ecd92a` |
| Result | `b249badd8a647f457b8ec2ae2473354819ab602d30c72c9a01b67207834fdcc4` |
| Kill proof | `5f8e85f0a91c54c1c98a7ac2f49ff3267f75c6137bb4ba2e53c5166184624d14` |
| Boundary marker | `d85d132a3c564ac454e131556c95f54322af39bcc899e1829c462a57548481fa` |
| Transcript | `8818bb035adf08be4a4b8fe9502960aeaab1bc529e57fad3e43ed45298c4388e` |
| Source proof | `de641fae1009e813941d5d5f015c40c331ff9d58864b337da021a4168bbb1e8d` |
| External preimage | `5f6bbca17bbbe24112aa5312bfa7cbb4a627d684e991a58e7fb20174b2119240` |
| Package preinstall proof | `002a0279c59255f925e00a3985c3bb607b9b16927dc6f8b7958d24c55e019e36` |

This adds one new runnable matrix cell and the first measured PowerDNS adoption interruption in this series. It proves same-request Agent-mediated forward convergence from a real external authority after this journal boundary; it does not prove continuous authority during the cut, rollback to an unreceipted source at later phases, owner-edit races, paired DNS, reboot/power loss or an Agent-independent inverse. No persisted v1 schema changed. P0.4 remains open. VM overlays, temporary binaries and extra image hardlinks were removed after capture; pinned source images and the two archives were retained.
