# Shared DNS stop guard native regression ? 2026-09-25

P0.4 / constitutional invariants 1, 2 and 3. Commit `d4b8c0e` was built into ordinary and `dns_kill_matrix`-tagged Agent binaries and exercised in a fresh disposable Debian 13 QEMU VM with a real managed PowerDNS source and an isolated Arch peer VM. No installed panel was changed.

The repeated cell was `bind__rolled-back__before-write__standalone__peer-reachable`. The tagged Agent was killed at the journal boundary with exit 137 and positive kill proof (PID 3653). The ordinary Agent restarted and the **same request** converged to BIND. Result and safety status were `passed`, classification was `target_converged`, and general, verification, diagnostic and safety failures were all empty. Agent, Panel, authoritative UDP DNS and authoritative TCP DNS were healthy in all 31 samples over 30 seconds.

The full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-guard-20260925/evidence/shared-stop-current.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Archive | `296db187f70b4acc8f3b7beea8b686a94df0a0c47ee4a453cefa740c5d274ac4` |
| Result | `675f6ccef8b714c23a064709dabb31f2d7c7493ec80061e8a90f4b1576a16e56` |
| Kill proof | `901c08776fb0045516113043396a68ab2d4cc4bbf0003812d1021b848c40f4e2` |
| Transcript | `b09e3e410a7481ab915ec6ba64728f47122186feca7cf9acc3b296586c099420` |

This repeats one of five previously passed runnable cells; it adds no matrix coverage. It proves the extracted shared guard remains compatible with this native interrupted-switch recovery path. It does not prove guard rejection under a native owner restart, cgroup emptiness, uninterrupted DNS during the cut, paired behavior, reboot, or an Agent-independent inverse. The v1 journal and ledger schemas were unchanged. VM overlays, temporary binaries and extra image hardlinks were removed after evidence capture; the pinned image sources and evidence were retained.
