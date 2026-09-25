# Native BIND inverse guard regression ? 2026-09-25

P0.4 / constitutional invariants 1, 2 and 3. Commit 75d83ac was built into
untagged and SIGKILL-tagged Agent binaries and exercised in a fresh Debian 13
QEMU VM with a real managed PowerDNS source and an isolated Arch peer VM.
No installed panel was changed.

The exact repeated cell was
`bind__rolled-back__before-write__standalone__peer-reachable`.
The tagged Agent was killed by SIGKILL immediately before the rolled-back
journal checkpoint write: exit 137, PID 3650, and proc entry absent after
reaping. The ordinary Agent restarted and the same request converged to the
BIND target. The result and safety status were `passed`, with no verification,
diagnostic, safety or general failures. Agent, Panel and authoritative UDP/TCP
DNS were healthy in all 31 samples spanning 30 seconds.

The full result, kill proof and transcript are retained in
`/var/tmp/cp-dns-guard-20260925/evidence/dns-guard-evidence.tar.gz`
on the local WSL test host. SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| Archive | d4a5fb100d973b8b85914f3f63114686ca8fe380d0be15e7297421d6b805ef70 |
| Result | 703eadb063c39f53f64fba4a58d2a0111d43eba09a7507db1cb5b5108568071c |
| Kill proof | 7ee879117a04ea79c96335969bf8392ad20e06e52ac353774440b89a9635b2e4 |
| Transcript | 4fbea3f1e5ff4f32b5807f1f9549af989a093a69c016f1499a0b10caa411f44a |

This repeats one of the five previously passed runnable cells; it does not
increase matrix coverage. It proves the changed Agent still converges in that
native interruption path. It does not prove the guard's rejection case in a
native fault, uninterrupted DNS during the cut, owner-restart races, reboot,
paired DNS, or an Agent-independent inverse. The v1 journal and ledger schemas
are unchanged. Both VM overlays, temporary binaries and extra base-image
hardlinks were removed after evidence capture.
