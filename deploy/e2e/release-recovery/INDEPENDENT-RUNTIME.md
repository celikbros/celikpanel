# Independent runtime native acceptance

*September 14, 2026 · [Türkçe](INDEPENDENT-RUNTIME.tr.md) · D-025 / P0.2–P0.3*

The [runtime contract](../../../docs/RECOVERY-RUNTIME.md) separates enrolled
recovery code from Panel/Agent startup and candidate lifecycle code. The results
below come from registered disposable QEMU guests. The detailed VM identities,
local paths, operation IDs and raw journals remain in local evidence; they are
not published here. No installed customer panel was updated by these experiments.

## Baseline and candidate

Each fresh guest started from genuine signed Alpha75, with real Agent BIND
acquisition and a created/edited DNS zone. Installed and running baseline SHA-256:

- Agent: `e8e69c520cf4112a5c60380378b8347021aaa035cdaacd820c9927436beff078`
- Panel: `568a7b15f20e5666fced0dcec77f52e83e596efd6e0969d3bf05508faa485d05`

Harmless owner comments were added to coordinator units before the measured
snapshot, and the native manager was reloaded. The local candidate was built from
clean committed source `4820c4a9172180131148554c98c4a02df3d5d040`; its archive SHA-256
is `f5493fbd8e8e594ea23f850541c2a68be05be4c524ab3ad5b7c888de24e54b49`.
This is **unsigned local candidate admission**, not signed Agent admission.
The controller accepts only its registered disposable guests and one start.

## Preserved trial outcomes

| Trial | Observed behavior | Acceptance |
|---|---|---|
| E — initial source `982a9d0` | Enrollment refused a dynamic shell FD argument before quiescence; original services remained active. Mapping the inherited lock to fixed FD 9 corrected this. | Recovery not exercised. |
| F — candidate `4820c4a` | Both native updates completed and served HTTPS with exact candidate binaries. Fixture identity checks prevented fault injection. | Normal update passed; interruption acceptance inconclusive. |
| G — controller `ca8343c` | Actual updater SIGKILL triggered independent automatic rollback on Arch and Debian 13. Old running binaries, owner units, DNS/TLS/web and transaction completion were verified. | Updater interruption passed. Second recovery fault was not applied: systemd rejected freezing an activating oneshot with a pending job. |
| H — controller `b4d9742` | Exact kernel cgroup freeze and checkpoint verification allowed real recovery SIGKILL on Arch and one VM reset on Debian 13. Automatic recovery completed without manual restoration or update retry. | The two stated recovery interruption boundaries passed. |

The PATH-dependent interpreter and lock observation defects from F were corrected
in the fixture. G's freeze restriction was reproduced on separate temporary
oneshot services. H uses held cgroup inode identity and kernel frozen-state proof.
Successful fixture tests are not substituted for native fault evidence. Earlier
failed/inconclusive trials and the corrected derived G summary remain retained;
raw observations were not rewritten.

## Recovery interrupted during restoration

| Guest | Actual second fault | Native continuation | Result |
|---|---|---|---|
| Arch | Recovery SIGKILL at `payload_restored` | New service invocation on the same boot; checkpoint sequence `2 → 7` | Exact old runtime restored automatically. |
| Debian 13 | One VM reset at `runtime_verified` | New boot and new service invocation; checkpoint sequence `4 → 9` | Exact old runtime restored automatically. |

Native evidence binds the interrupted and completing invocations to the same
snapshot/token/runtime identity. The final checkpoint matches the actual rollback
terminal. Debian's first boot recovery refused while systemd was `starting`; the
native timer subsequently completed the same operation. That intermediate failure
is retained rather than presented as uninterrupted success.

Both guests restored the exact old installed **and running** executable hashes,
owner unit bytes and native manager configuration (`NeedDaemonReload=no`). Panel
and Agent were active; HTTPS returned 200; four A/SOA TCP/UDP probes, installed and
served TLS and the web tree were preserved. No active transaction remained. Each
trial verified 129 snapshot files, 279 retained candidate files and 12 enrolled
runtime files. This is **PASS for the two stated interruption boundaries**.

The complete database comparison remains **DIFFERENT**, solely in `metrics_samples`.
No table/row was excluded. Supplementary read-only comparison includes rowid and
all columns: all five snapshot metric rows on Arch and all six on Debian remained
unchanged; missing/changed rows were zero. The 17/18 additional rows respectively
were all dated after snapshot capture. All other table and schema digests matched.
G similarly preserved all six snapshot metric rows on each guest with 24 later
rows added. These are preservation results, not whole-database equality.

## Retained evidence fingerprints

These digests identify local fixture reports, not credentials or recovery tokens.
The sealed index covers 15 Arch and 19 Debian files; every digest was independently
rechecked. Both guests were stopped through their registered guards, retaining
disks and evidence.

| H report | Arch SHA-256 | Debian SHA-256 |
|---|---|---|
| Sealed evidence index | `dee8f97cede5cbabde9fb5a39b2cc9dbf8b7afc7ea7d486b249d946880458ae4` | `9d1a124a83b61545b8d09fa09aac45eb8587390baf88744e40c98b3badaf0b0d` |
| Native outcome | `02955633ccfae3ebcb7fc948651ddac7203f8777a4cfc85e2bc5dd94b98855ba` | `4aff9fc9b38df08556c0c5097f04cec4529cc78f4fad32ff18f29488319fa60c` |
| Checkpoint replay | `07a493002bcd9d46b0d4eb149569d16d17a7a38d01a1002690e833cca22dfd91` | `b7fce9c2bae0f902e63ca2d297a088ee803a9518140fd26acbd635ae24fed375` |
| Metric row retention | `8f19ee7d1022d89e22bddbcd8d50d0321ffdec3c675c45c3f348e3a2e75ea1c2` | `8ed38895c1ce46ad9d438dac4317fba25aeefcf7e5471d4c365177b0ae02c632` |

## Source checks and open acceptance

[CI 34868947743](https://github.com/celikbros/celikpanel/actions/runs/34868947743)
passed for `b4d9742b8807878dbb643be397903fb8e0d16d04`: 21 jobs succeeded; tag
publication was skipped. Build, vet, Go/race, web, shell/recovery contracts and
reproducible archive checks passed. The Python fixture suite passed **191/191**.
Later controller commits did not change the native candidate's production behavior.

The later [material acceptance](RECOVERY-MATERIAL.md) additionally exercises three
missing retained candidate files during complete-snapshot rollback. The complete
checkpoint matrix, signed Agent candidate admission, incomplete-capture/forward-
completion data independence, supported metadata transitions, evidence cleanup,
external certificate renewal, mail and native secondary-DNS workload matrix remain
open. These results do not close P0.1–P0.5 and are not a published production release.
