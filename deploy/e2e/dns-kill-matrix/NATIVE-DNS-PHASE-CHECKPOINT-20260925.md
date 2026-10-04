# Shared DNS rollback checkpoint native regression — 2026-09-25

P0.4 / constitutional invariants 1, 2 and 4. Commit `e13ce5e` was built into ordinary and `dns_kill_matrix`-tagged Agent binaries and exercised in a fresh disposable Debian 13 QEMU VM with a real managed PowerDNS source and an isolated Arch peer fixture. Neither installed server was changed.

The repeated cell was `bind__rolled-back__before-write__standalone__peer-reachable`. The tagged Agent reached the retained `rolling-back` journal checkpoint and was killed immediately before writing `rolled-back`. The result proves SIGKILL, exit 137, process reaping and the boundary marker (PID 3646). The ordinary Agent restarted; same-request recovery converged to BIND. Result and safety status were `passed`, classification was `target_converged`, and general, verification, diagnostic and safety failures were all empty. Agent, Panel and authoritative UDP/TCP DNS were healthy in all 31 samples across 30 seconds.

The full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-phase-20260925/evidence.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Archive | `738f93a47a28ae07387690e1d5a9b5cadf409554ea926bc59f3df422de163f83` |
| Result | `20df40fc5ea061fea17c0df4983ff82d54e612fdef9d0a21a7a824c933255a7e` |
| Kill proof | `56d8cf6772d246949d27473e8aec07e97377a02bdb2593a961f80d450aa9e337` |
| Boundary marker | `a8521503274d7471987f4bdbecc55decd7eac3958c89d4ae19b1726e56a2181b` |
| Transcript | `24d4a62b7d56f44146917a994ac5681f975be399a430add9f6ff1b3ea8ef5344` |

This repeats a previously passed cell and adds no matrix coverage. It confirms that the shared rollback phase/checkpoint refactor remains compatible with this Agent-mediated native crash recovery path. The v1 journal and ledger schemas were unchanged. It does not exercise the new independent exact-preimage file adapters, prove owner-edit exclusion at effect time, an Agent-independent inverse, paired DNS, reboot/power loss or uninterrupted DNS during cutover. Those P0.4 acceptance items remain open. VM overlays, temporary binaries and extra image hardlinks were removed after evidence capture; pinned source images and this archive were retained.
