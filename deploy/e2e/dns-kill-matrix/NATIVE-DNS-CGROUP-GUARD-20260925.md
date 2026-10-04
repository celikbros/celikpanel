# Native DNS cgroup stop guard regression ? 2026-09-25

P0.4 / constitutional invariants 1, 2 and 3. The uncommitted cgroup-guard source tree was built into ordinary and `dns_kill_matrix`-tagged Agent binaries and exercised in a fresh disposable Debian 13 QEMU VM with a real managed PowerDNS source and isolated Arch peer. No installed panel was changed. The source is committed alongside this report.

The repeated cell was `bind__rolled-back__before-write__standalone__peer-reachable`. The tagged Agent was killed at the rolled-back journal boundary with exit 137 and positive kill proof (PID 3682). The ordinary Agent restarted and the same request converged to BIND. Result and safety status were `passed`, recovery classification was `target_converged`, and the general, verification, diagnostic and safety failure lists were empty. Agent, Panel and authoritative UDP/TCP DNS were healthy in all 31 samples over 30 seconds.

The full result, kill proof, boundary marker and transcript are retained in `/var/tmp/cp-dns-guard-20260925/evidence/cgroup-guard-current.tar.gz` on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Archive | `4c3fd2193643811b8bf88389b2b5a751c98a665f4efad4fd307be9fce15f763f` |
| Result | `a8d28c52e27ba5bdc31b06b48fed37bc7510d0be1a01ef56b670783523822607` |
| Kill proof | `495288284e6d576894cfee1358d578c55380e3c21587d6e145c0e2b5ca20656e` |
| Transcript | `e10a2e117325063d363bb57b5adbee95b043e7ab7946cea3166db2f2ac977500` |

After result capture, a separate read-only systemd observation showed active `named.service` with `Slice=system.slice`, `ControlGroup=/system.slice/named.service`, `MainPID=7986`, and cgroup v2 `cgroup.events` containing `populated 1`. The disposable service was then stopped for data-shape inspection: systemd reported loaded/inactive/dead with zero main/control PIDs and an empty `ControlGroup`, while `/sys/fs/cgroup/system.slice/named.service` was absent. This post-result stop did not contribute to the matrix result.

This repeats an existing passed cell and adds no matrix coverage. It proves the cgroup-enabled Agent path still converges in this native interruption. Unit tests inject `populated 1`, missing/changeable cgroup identity and second-read population; no native process was deliberately held in the target cgroup during inverse execution. It does not prove exclusion of a later owner restart, an Agent-independent inverse, paired behavior, reboot, or uninterrupted DNS during the cut. Journal, ledger and DNS receipt v1 schemas are unchanged. VM overlays, temporary binaries and extra image hardlinks were removed after evidence capture; pinned images and evidence were retained.
