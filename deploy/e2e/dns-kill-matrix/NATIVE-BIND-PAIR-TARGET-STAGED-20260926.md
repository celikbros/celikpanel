# Native BIND pair at target-staged/after-write — 2026-09-26

This is a bounded P0.4/P0.5 acceptance result for constitutional invariants
1, 3 and 6. It does not close the DNS fault matrix or the native workload
matrix. Only fresh disposable Arch and Debian QEMU guests were changed; no
installed owner panel was touched.

The current checkout at `c817dc5d` was built for an Arch BIND primary. A fresh
Debian 13 guest ran a standard BIND catalog secondary with no CelikPanel
Agent or Panel binary. Official image bytes were verified against the pinned
fixture checksums. The selected cell was
`bind__target-staged__after-write__paired-primary__peer-reachable` with an
honestly uninitialized source.

The fault controller observed the exact `target-staged` journal boundary and
SIGKILLed the tagged Agent after the journal write. The sealed result records
exit 137, `kill_proven=true`, the same request ID
`001b5d46a9e838697dbe18d9c308b0c8`, and two same-request recovery calls
with exit 0. The result records `recovery_outcome.classification=target_converged`,
`status=passed`,
`safety_status=passed`, and no diagnostic or verification failures. All 31
post-recovery stability samples found Agent, Panel, and primary authoritative
UDP/TCP DNS healthy. The native secondary loaded `s1-kill.test` as a secondary
at serial `2026083101`; direct nonrecursive UDP and TCP queries returned
`NOERROR`, `aa`, and `www.s1-kill.test A 192.0.2.11`.

The primary Agent and Panel were then disabled and stopped in the disposable
guest. Both guests rebooted. The primary boot ID changed from
`60d2eefb-4591-4d5f-86af-7fe93d62fb2e` to
`e0db7dda-81ee-4b69-abdb-1068a7ac288a`; the secondary changed from
`179eae9a-32db-4aca-8512-898f8650ce51` to
`fe86526b-763a-479b-930a-55435366dd15`. After reboot,
`named.service` was enabled and active on both, while the primary Agent and
Panel remained disabled and inactive. The panel-free secondary again reported
the member zone at serial `2026083101` and gave authoritative UDP and TCP A
answers. This proves native serving and transfer-state continuity at this
specific fault and reboot boundary.

The raw fault evidence is retained at
`/var/tmp/cp-bind-late26-evidence.tar.gz`, SHA-256
`02278a192a2be66ddec8322c27bf80f4e350f4af17e6c6602a309a0583a361af`.
It contains `result.json`, `kill-proof.json`, the exact boundary marker and
`transcript.jsonl`. The reboot observations above were separate read-only SSH,
systemd, `rndc` and `dig` checks and are not sealed in this archive. A separate
attempted `target-verified/after-write` cell was rejected by fixture preflight because its driver-specific source policy does not
permit an uninitialized source; no mutation was started for that cell. Its
disposable overlays were removed before this trial.

The DNS state, ownership, switch journal and ledger schemas did not change.
Recovery was Agent-mediated and resumed the same request. This trial does not
prove an Agent-independent inverse, uninterrupted DNS during the cut, PowerDNS
interoperability, owner-edit races, catalog-member deletion, renewal, other
hosted workloads, or full P0.4/P0.5 acceptance. The later target-verified
boundary still needs an appropriate source fixture.
