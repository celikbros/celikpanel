# Native DNS rollback serving before retry - 2026-09-25

P0.4, constitutional invariants 1, 2 and 4. Two fresh, isolated Debian 13 QEMU trials repeated `bind__rolled-back__before-write__standalone__peer-reachable` with a real managed PowerDNS source; an Arch guest supplied only the isolated fixture peer. Source commit `9766bbd` introduced the pre-retry read-only recovery observation. The second trial also included a pre-retry authoritative UDP/TCP query. No installed panel was changed.

Both trials proved SIGKILL/exit 137 with the exact retained `rolling-back` journal boundary. **Before the same-request RPC retry**, the first trial's recovery probe classified `rolled_back_source_active`: the previous PowerDNS state and ownership receipts matched, its unit was active, the measured ledger contained an exact failed/interrupted terminal verdict without worker or lease, target ownership was absent and the switch journal was retired. The later RPC retries converged forward to BIND. Its result and safety status passed; Agent, Panel and authoritative DNS were healthy in all 31 post-retry samples over 30 seconds. This first trial did not query DNS before retry.

The second trial repeated that exact pre-retry rollback classification and additionally obtained successful authoritative answers over **both UDP and TCP before retry**, setting `recovery.pre_retry_source_serving: true`. The two later same-request retries again converged to BIND. Its result and safety status passed, with no recorded failures and 30/30 healthy Agent/Panel/authoritative-DNS post-retry samples over 30 seconds.

The complete result, kill proof, boundary marker, transcript and sealed source receipts are retained on the local WSL test host:

| Trial | Evidence archive | SHA-256 |
| --- | --- | --- |
| Exact pre-retry rollback | `/var/tmp/cp-dns-preretry-bind-20260925/evidence.tar.gz` | `2614d98f569aabb80779d3c753acd9cbfd9685f20f20ea68a7f55317f38773bc` |
| Pre-retry rollback plus UDP/TCP serving | `/var/tmp/cp-dns-preretry-serving-20260925/evidence.tar.gz` | `8fede88f70eca4340e32126c8e41cf5e4290888b565e63f441e731a83b3ae8d3` |

Both disposable VM pairs, overlays, temporary binaries and duplicate base-image hardlinks were removed after capture. Pinned base images and these archives remain.

The observations prove Agent-mediated startup rollback and source DNS serving at this exact interruption boundary, followed by same-request forward convergence. They do **not** prove an Agent-independent inverse, continuous DNS across the process cut, owner-edit exclusion, paired secondary behavior or power-loss recovery. The DNS journal/ledger and recovery-probe v1 schemas did not change. Result v1 adds diagnostic pre-retry fields; older result artifacts remain post-retry only. P0.4 stays open.
