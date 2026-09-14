# Recovery material native acceptance

*September 14, 2026 · [Türkçe](RECOVERY-MATERIAL.tr.md) · D-025 / P0.3*

The [material contract](../../../docs/RECOVERY-MATERIAL.md) separates rollback
recovery data from the failed candidate tree. These experiments ran only in
registered disposable QEMU guests. Detailed host paths, guest identities, operation
IDs and raw journals remain in local evidence. No customer panel was updated.

## Source and baseline

Each fresh Arch and Debian 13 guest started from genuine signed Alpha75, with
native BIND acquired through the Agent and a DNS zone created and edited. Owner
comments in the coordinator units were present before snapshot capture and the
native service manager was reloaded. The baseline installed and running hashes:

- Agent: `e8e69c520cf4112a5c60380378b8347021aaa035cdaacd820c9927436beff078`
- Panel: `568a7b15f20e5666fced0dcec77f52e83e596efd6e0969d3bf05508faa485d05`

The final candidate came from clean committed source
`bc0ebc051de0fc542c5f90e7c0bbb519a5c0f111`, tree
`76492d58a7b9c1b86e2282f0e6d090127b71bd5a`. Its archive SHA-256 is
`4d5bde282e7d2342a4b42930f7749104c658b933cf8908da71db3279cf7542c1`.
This is **unsigned local candidate admission**, not the signed Agent update path.
The later `fbb9b62` commit changes only the CI test time budget.

Preliminary trial I used `bed635d` and passed the updater interruption on both
systems with three candidate files unavailable. It did not interrupt recovery
again. Final trial J below additionally uses direct snapshot-name material lookup
and explicit selected-runtime layout negotiation; I is not evidence for those
later changes. Earlier evidence remains retained.

## Combined faults and automatic recovery

At the installed-candidate checkpoint the fixture quarantined exactly these
retained candidate files: `rollback.sh`, `bin/agent`, `web/dist/index.html`.
It then killed the exact updater. The product's native recovery service and timer
handled restoration; no manual restore or second update was started.

| Guest | Second fault during automatic recovery | Observed continuation | Result |
|---|---|---|---|
| Arch | SIGKILL at `payload_restored`, with active rollback | A new recovery invocation on the same boot | Exact old runtime restored automatically. |
| Debian 13 | One guarded VM reset at `runtime_verified`, with completion pending | A new recovery invocation after a new boot | Exact old runtime restored automatically. |

The second fault was bound to the actual checkpoint, held cgroup/PID identity,
transaction, snapshot and selected runtime. Both completed the same recovery
operation while the three canonical candidate paths remained absent. Checkpoint
sequences advanced `2 → 7` on Arch and `4 → 9` on Debian. Debian's first boot
attempt refused while systemd was still `starting` (status 1); the native timer
then completed recovery. That intermediate failure is retained in the evidence.

The original three files remained in a private quarantine with matching contents,
inodes and metadata; rename changes to ctime are recorded. The retained candidate
manifest was unchanged and the other 280 entries matched the remaining inventory.
The fixture also checked the fixed installed executable/web targets and foundation
files around its injection. Its private fault records are test evidence, not
product restoration authority.

Both guests verified 129 snapshot files, 12 selected-runtime files and 15 recovery
data files. The material record's canonical digest, snapshot-name directory key,
transaction binding, data manifest and twelve source-file mappings were checked.
The recovery observations before the second fault and after continuation matched
the same snapshot and runtime. There was no separately collected pre-fault material
inventory; the report does not claim such a before/after observation.

## Preserved state

Both restored the exact baseline hashes on disk **and in running processes**.
Panel and Agent were active; HTTPS returned 200. Four authoritative A/SOA TCP/UDP
DNS probes, installed and served TLS, the web tree and owner unit bytes matched.
The native manager reported `NeedDaemonReload=no`; the transaction cleared and
the recovery timer returned to waiting.

The whole-database result is **DIFFERENT**, only in `metrics_samples`; none of its
65 tables or rows was excluded. Integrity and all other table/schema comparisons
passed. The supplementary native read-only collector reported all nine Arch and
ten Debian snapshot metric rows retained with identical rowid and typed values,
zero missing/changed rows and six later samples added on each guest. An independent
review checked the collector and its sealed aggregate result; it did not recompute
row retention from exported raw database rows. This establishes scoped data
preservation, not whole-database equality.

These are before/after workload probes. They do not measure uninterrupted
availability, mail delivery, external certificate renewal or native secondary
transfer throughout the faults.

## Evidence and acceptance limits

Independent review rechecked all 33 Arch and 38 Debian sealed report files.
Both guests were stopped through their registered guards; disks and evidence
remain retained. These fingerprints identify local reports, not recovery tokens.

| J report | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Sealed evidence index | `39ce09565852f876959fc7f8e14a3da1e1a63fb3d2b44d93f8a9f7c8301db3e4` | `e9f905d3860b76a720bc7ba98fb2da034889a778cfb0d0b331a588547e14a92e` |
| Native outcome | `7e17893c9a07e91aeddad6c2d31a518cfac866899b5efe9e37e38db6e576e542` | `00db6d02e728023a537c02fd5268d538fdf5a496147ffe1ff230abb14d016851` |

[CI 34878728810](https://github.com/celikbros/celikpanel/actions/runs/34878728810)
passed for `fbb9b62fb911ace820c5987c609e8eb54e63c3a7`: Go build/vet/full tests,
race shards, shell/recovery contracts, web checks and reproducible archive checks.
The local Python fixture suite passed 214/214 tests. An earlier run exhausted Go's
aggregate default package timeout; the explicit 20-minute budget preserves all
tests and real password hashing. No cryptographic parameter was reduced.

Subsequent source review found that a missing material record plus an orphan
`published` receipt could reach legacy lookup before later rejection. A focused
correction makes that corrupt state refuse at material lookup, before rollback
stops coordinators. Its negative-path regression tests are separate evidence;
these native trials did not inject that corruption. A second compatibility check
refuses fresh-token historical rollback against a material-backed snapshot before
creating an active marker or stopping services. That unsupported operation must
not consume the previous update's authority; it is distinct from the same-token
automatic recovery exercised here.

**The two combined interruption boundaries passed.** Whole candidate-tree loss
is exercised in Linux root tests; these native trials removed only the three
stated candidate files. Incomplete snapshot capture and forward update completion
still require retained candidate data. Selected-kit promotion, all other checkpoint
combinations, signed Agent admission, metadata migrations, cleanup and the full
native workload matrix remain open. P0.3 is partial; this is not a published release.
