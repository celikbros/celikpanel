# Native release recovery observations — 2026-09-14

*[Türkçe](RESULTS.tr.md) · [Fixture instructions](README.md)*

Four disposable QEMU trials exercised genuine released binaries and the installed
Agent's signed update worker. Debian 13 reproduced an update failure followed by
an automatic recovery failure. Arch recovered operation by finishing the pending
Alpha80 update. Fresh Arch and Debian kill trials failed before restoration.
**No trial executed a complete restoration to the old
release. P0.1 remains open.**

The first two observations concern fixture cell `release-recovery__35a6cb18dede09f7`,
retained locally under `/var/tmp/cp-release-drill-20260914-b`. They are not changes
to Boston, Frankfurt, or another installed customer panel. All times below are
UTC. This report contains selected facts and hashes; private journals, credentials,
tokens and private certificate contents are not included.

## What actually ran

All trial guests started with the genuine signed `v0.1.0-alpha.75` installation from
commit `5aa03fd5b6775b21834ff7b1ce0695d92f50ae93`. The unchanged released bootstrap
has SHA-256 `82b2674c103e347df471ec7e3f2f091d50006c957c54ac946b7c021ed19e041d`.
The real Agent acquired standalone BIND, created `recovery-fixture.test`, then
modified it. Those native producers advanced the published generation while
retaining the acquisition ownership receipt. No DNS receipts or production state
were manually constructed.

The controller retained an exact request ID before starting one update per node.
The installed Agent independently fetched and verified the requested signed
release. Its real `celikpanel-self-update-<request>.service` worker and native
`OnFailure=celikpanel-release-recovery.service` remained in the execution path.
No replacement rollback body, recovery stub, manual restoration or second update
start was used.

| Trial | Request ID | Observed result |
| --- | --- | --- |
| Debian 13, Alpha75 → Alpha79 | `5a25a60dadd257dd6023eca4a988a54c` | **FAIL:** update and automatic recovery failed; candidate files remained installed and panel/Agent were stopped. |
| Arch, Alpha75 → Alpha80, temporary TCP2083 conflict | `4db8210bd09431d0e7b187bfedc4e858` | Runtime recovery **observed** through forward finalization; full rollback objective **INCONCLUSIVE**. |
| Fresh Arch, Alpha75 → Alpha80, SIGKILL at active checkpoint | `8e79de5b2290688276d2ae8df056ba1c` | **FAIL:** automatic recovery rejected an unconsumed systemd unit transition before restoration. |
| Fresh Debian 13, Alpha75 → Alpha80, SIGKILL at active checkpoint | `8ffc77c4e7a59bdfe175b87489de5f4a` | **FAIL:** the same guard precondition blocked automatic restoration; both coordinators remained inactive. |

## Debian 13: the failure reproduced

The update started at `07:58:14`. By `07:58:33`, Alpha79 files were installed and
the worker failed with `BIND state and ownership receipts disagree` while
preparing the managed BIND generation root. The real `OnFailure` executor ran.
At `07:58:34`, its retained child rejected the inherited transaction descriptor:
`recovery transaction descriptor owns an unexpected lock`.

Recovery exited with status 1 before the restoration body. At the saved
`07:58:41` observation, Agent was inactive, panel was failed, and the active
transaction marker remained. Installed executable hashes matched the genuine
Alpha79 archive; neither executable had a running `MainPID`.

Snapshot identity:

```text
20260914T075823Z-from-unknown-to-f3390addc85bbe92a0cc865448d9bb366e6b980a-4599eadccc696b6f9f35c939372596d5
```

The `from-unknown` component is the recorded snapshot name, not evidence that the
baseline release was unknown to the fixture. Snapshot completeness was not
independently classified in this observation.

The installed TLS certificate fingerprint was unchanged, but a loopback TLS
connection was refused. BIND remained active as PID 6292 with UDP/TCP53 listeners.
The DNS answer collector could not run because `/usr/bin/dig` was absent in the
guest; both before and after answers are **unknown**, not verified failures or
proof of preservation. No diagnostic package was installed after the fault to
retroactively turn this into a successful baseline.

## Arch: forward completion recovered service operation

The update started at `08:12:22`. The fixture held `127.0.0.1:2083` after the
original panel stopped. At `08:12:47.821587`, it verified the installed candidate
files while the conflict was present. At `08:12:50`, the update worker exited
with `saved active-like service is not active: celikpanel-panel.service`.
The fixture released its port at `08:12:50.857236` when that exact update unit
exited; it did not restore or start CelikPanel services.

The real `OnFailure` recovery executor then completed successfully. Its native
`08:12:55` journal explicitly records
`Previous pending update finalized from verified snapshot`. At `08:13:35`, the
Agent and panel were active with installed **and running Alpha80** hashes. The
active transaction marker was absent. This is evidence of forward finalization,
not restoration of Alpha75.

Snapshot identity:

```text
20260914T081230Z-from-unknown-to-bd14d97efc5cfd19acd70ddf0edb9c6343317e2b-85e1fd2f9c6dc7b0f040fc2aeb6adc07
```

Before and after loopback A and SOA queries were authoritative `NOERROR` over
both UDP and TCP. Answers were preserved: A `192.0.2.11`, SOA serial `2` for
`recovery-fixture.test`. The served TLS fingerprint was also preserved:
`a52eaeb13389a65ac6f6e2b6829f173fa531f11e01d8a63e80123e529e4c1797`.
The earlier update-worker failure remains a recorded failure even though the
recovery operation subsequently restored service availability.

## Fresh Arch: active-phase kill blocked recovery before restoration

This third trial used fresh cell `release-recovery__a62839366acb9420` under
`/var/tmp/cp-release-drill-20260914-c`. Request
`8e79de5b2290688276d2ae8df056ba1c` started at `08:27:22`. At `08:27:56.681569`,
the fixture had frozen the exact original worker (PID 10557), verified 129
snapshot files and matched the installed Alpha80 binaries while the transaction
phase remained `active`. SIGKILL reached only that update unit's cgroup at
`08:27:56.733642`; native systemd events confirm its exit. The helper did not
rewrite service definitions or recovery state.

Real `OnFailure` recovery started immediately. At `08:27:57`, it reported
`celikpanel-agent.service has unconsumed guard changes`, then
`release transaction service guards differ from the monotonic foundation`.
The retained child exited with status 1 before restoration. Native timer attempts
repeated the refusal at `08:28:33`, `08:29:07` and `08:29:39`, under the same
transaction. At `08:29:55`, Alpha80 files remained installed, neither coordinator
was running and the active marker remained. DNS A/SOA answers over UDP/TCP matched
the baseline. The TLS file fingerprint was unchanged, but served TLS was refused.
The old-release restoration objective is **FAIL for this interruption**.

```text
20260914T082735Z-from-unknown-to-bd14d97efc5cfd19acd70ddf0edb9c6343317e2b-beb02f79fc26e90507dbccecb65e4018
```

Read-only unit inspection confirmed `NeedDaemonReload=yes` on Agent and panel,
with expected loaded `FragmentPath` and guard `DropInPaths`. Each disk vendor unit
had **identical bytes to both its old snapshot and retained Alpha80 candidate**:
Agent `d13caa039eb0515b5399ecece1df2620c289356fb0eaff66d7c524812a62a22b`, panel
`ee1fa28cf8532faee65ae6fae0437634e61aa5616326017eecb9558bb82f6fa3`.
Installed mtimes were approximately `08:27:56.364` / `08:27:56.367`; snapshot copy
mtimes were `08:27:51.331` / `08:27:51.351`. Both guard drop-ins retained hash
`82e0fe894b3fa676c1b64e65ac964b74b27dcfe5a83ddf91761f44ad5971359f`.
Manager properties and disk hashes were observed separately; no historical
in-memory unit-text hash is claimed.

The source explains a legitimate interrupted transition: in Alpha80,
`install.sh:2940–2942` republishes vendor units, before `daemon-reload` at line
2969. But `rollback.sh:1407–1410` demands guard verification before restoration;
`deploy/release-transaction-guard.sh:1361–1364` requires `NeedDaemonReload=no`.
Thus a steady-state precondition rejects an updater-created transition, even
without changed unit content. Recovery needs explicit handling of published but
not yet consumed unit definitions with ownership proof; this report does not
claim that correction is implemented. No daemon reload, manual restoration or
second update was performed to fix the trial.

Evidence below is under `/var/tmp/cp-release-drill-20260914-c/evidence/arch/`.

| Evidence | SHA-256 |
| --- | --- |
| `native-outcome.json` | `6145f55fa50c8ab7814dcda2de9cfca461b5840a7a99f81ee2079d58980c1a65` |
| `update-kill-collection-1789374510707201472.jsonl` | `585367355f231199c861ffc31fc4b30d42fbbb225bd391687243abd3deaf697d` |
| `update-observe-20260914T082953498183Z.native.json` | `7fa287dd4fcb1b45103dc0d1f86e66a6629d68e3d1b7168cb782bc3204a283df` |
| `update-observe-20260914T082953498183Z.json` | `ad2b04b67a6ffe029dce74930f96e0965c051e37b0b2f996bc3670783e315a22` |
| `unit-transition-observation.json` | `ce8a70b2d34d07982a4dff56b55e37085aad2b1a66f53d5c977ad41b809dfb43` |
| Alpha80 `install.sh` source | `1550d6134638ccb7b843c8d3cc891c3d9f95799528186d5a5150a562bec53baa` |
| Alpha80 `deploy/release-transaction-guard.sh` source | `666373620d35ef5f5826c38eab4732fdba5954aa6fa2a503b00e8a49ce752595` |

The kill sequence uses a separately prepared fresh lab, not the retained port-fault
node. Baseline installation and real producer seeding precede these commands:

```sh
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$KILL_LAB_ROOT" --node arch --sequence 80 --mode prepare --execute
python3 deploy/e2e/release-recovery/arm_update_kill.py --work-root "$KILL_LAB_ROOT" --node arch --mode arm --execute
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$KILL_LAB_ROOT" --node arch --sequence 80 --mode start --execute
python3 deploy/e2e/release-recovery/arm_update_kill.py --work-root "$KILL_LAB_ROOT" --node arch --mode collect
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$KILL_LAB_ROOT" --node arch --sequence 80 --mode observe
```

## Fresh Debian 13: the active-phase recovery failure repeated

The fourth trial used the Debian node of fresh lab-c and request
`8ffc77c4e7a59bdfe175b87489de5f4a`. `bind9-dnsutils` was installed as a separate
fixture dependency before its baseline observations; this does not change the
missing-DNS-evidence result of the earlier Debian trial.

At `08:37:33.523`, the fixture verified the active-phase checkpoint, 129 snapshot
files, and both installed Alpha80 hashes while the exact original worker was
frozen (PID 10028). Its exact unit cgroup received SIGKILL at `08:37:33.586`.
The real `OnFailure` recovery failed with the same unconsumed-guard rejection at
`08:37:37`; timer attempts repeated it at `08:38:10`, `08:38:42` and `08:39:14`.
No full restoration body ran. Both coordinators were inactive with `MainPID=0`,
candidate Alpha80 files remained installed, and the active marker remained.

```text
20260914T083722Z-from-unknown-to-bd14d97efc5cfd19acd70ddf0edb9c6343317e2b-49f34c2c3e3c8ca04e884d28d98d0b78
```

All four baseline and after DNS queries were authoritative `NOERROR`, preserving
A `192.0.2.11` and SOA serial `2`. Served TLS was connection-refused; the certificate
file retained fingerprint
`d81106101d7e00f9eb4550bf0f1972a89f20ec7ebcdfd3dcbb9e56014ce95321`.
After-state database integrity was `ok` at schema 42; the nonempty baseline WAL
still prevents a semantic preservation claim. The detailed disk/manager unit
comparison described above belongs to the Arch trial; this Debian result confirms
the native guard refusal independently. No manual recovery or second update start
was performed. The kill-controller sequence above was repeated on the fresh
`debian13` node, with all operation IDs generated and retained separately.

Private proof root: `/var/tmp/cp-release-drill-20260914-c/evidence/debian13/`.

| Evidence | SHA-256 |
| --- | --- |
| `native-outcome.json` | `a11cc973e65c2d2b070c462d48a92d993f6635e50322f8b5529c7b85c70a8b09` |
| `update-kill-collection-1789375055854888567.jsonl` | `9a4d2686a472e489e2a3950b5dd4a21045d4422865d27a476189d3128b048ed1` |
| `update-observe-20260914T083933577712Z.native.json` | `f09446faeb2df9646ac75edbd4c194ed7b41a8dcb5eee038d80f8ea08a13a9c2` |
| `update-observe-20260914T083933577712Z.json` | `a8dff3f08f7031b4f67f35d50b49f912efe1578176262c310f62aa785235ac7c` |

## Artifact and source identities

All digests are SHA-256. Agent and panel candidate hashes below match the signed
archive members and the observed installed files; the Arch port-conflict trial also matched the running
executables.

| Artifact | Debian Alpha79 | Arch Alpha80 |
| --- | --- | --- |
| Commit | `f3390addc85bbe92a0cc865448d9bb366e6b980a` | `bd14d97efc5cfd19acd70ddf0edb9c6343317e2b` |
| Signed release-manifest-v2 bytes | `a58933f2e9df413967099e3e180ad0b761c12487866a5121286ba560abdb170a` | `04b20c2c0e3af195979cae0691a8b0266fe6df1245191e5cb1cd74c0f60f9fbe` |
| Archive | `cfc4ec5dd4f8b8d178d4b3bc328f62f0e541aadfe524abcedd605bc915b4ba5f` | `a29bd22d72dfd70d19811994999fda9b5d7d8d85873e2a65dc190d8034c70f10` |
| Agent | `ae3b563d65e24e4176b875a440c8e6a6b3bdea14213ac106b579701587a5782d` | `7f84ca2f03b82b51748a7a2c4f7bda8938f43a8e8bcac546171368d320f4c5ac` |
| Panel | `8abd262df639364ddfa274b303b5112f0eae25758f3118aa433e034154cad698` | `88d607bcdf160ab1d296803a5632d19e72cc7351365761de5d6847191537ce67` |
| Released `update.sh` | `2d787c590b7d0dfe0cfc59548aba957862faa13cb86d6a3d67d6cf5b0020658c` | `703263a66853503e960dd01ae13e01ced53278bc9f99f4282a7b111e12f218b1` |
| Released `rollback.sh` | `7063d2a5951fb6b7d0bc5b7eac320ec966a05a0bc58f818de7cfdbe1902ecb64` | `e559f741043c60615d67d829c0fb7769934334ef682bff3bdb7930125b76b1c7` |

Historical source can be read with `git show <commit>:<file>`. In the specified
Alpha79 revision, `update.sh:3220` reports failed BIND preparation and
`rollback.sh:707` rejects the inherited lock. In Alpha80,
`update.sh:2154` reports the observed pending-update finalization. These are
historical line numbers, not promises about current working-tree positions.

Private proof roots are `evidence/debian13/` and `evidence/arch/` beneath the
retained lab root. The summaries contain exact references and digests for the
baseline, seed, persisted intent, reviewed tuple, start attempt and native
observations.

| Relative evidence path | SHA-256 |
| --- | --- |
| `debian13/native-outcome.json` | `430f0c2bb2ad67d2a9cf87a30e3e2c204f4fff2d5d7935539b7758391bbbeb54` |
| `debian13/update-status-20260914T075840122387Z.native.json` | `082c1af58dcaed83a0d478ddb8fd30d4f4d7ee2b51195520ad6224afea977d6f` |
| `debian13/update-status-20260914T075840122387Z.json` | `072359f74d178ade01f858617d428b36d5ba522851ba201f99fdc0b2df4d9f47` |
| `arch/native-outcome.json` | `415d06623513347e5ec9188bf40a509c163a16251fc657792b9c3e104d6dacd4` |
| `arch/update-observe-20260914T081333791576Z.native.json` | `a7914a73615c1216b1399517b18db4d26c79558221ea48d02ae196e2e49c662b` |
| `arch/update-observe-20260914T081333791576Z.json` | `4e5da4786e7107746d5c3f111239d0e1db4f47626990082432935428df36c245` |
| `arch/port-fault-collection-1789373605641389722.jsonl` | `d143d7d518c75bdf48fc3f2d5cc1483a491c76724dd51f80a699e8e73df02048` |

## Controller sequence

Use the [fixture instructions](README.md) to prepare a **new** registered lab,
install Alpha75, seed both nodes, and capture the required baseline workloads.
Check diagnostic prerequisites, including `dig`, before that baseline. Do not
rerun a start on the retained evidence cell or delete an intent to retry.
The local signed assets are in `.tmp-release79/assets/` and
`.tmp-release80/assets/`; the update driver is `.tmp-release-recovery-update`.
For a fresh reproduction, build it with the supported toolchain:

```sh
GOTOOLCHAIN=go1.26.5 go build -o .tmp-release-recovery-update ./deploy/e2e/release-recovery/driver-update
```

The selected controller sequences were:

```sh
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node debian13 --sequence 79 --mode start --execute
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node debian13 --sequence 79 --mode status

python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node arch --sequence 80 --mode prepare --execute
python3 deploy/e2e/release-recovery/arm_port_fault.py --work-root "$LAB_ROOT" --node arch --mode arm --execute
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node arch --sequence 80 --mode start --execute
python3 deploy/e2e/release-recovery/arm_port_fault.py --work-root "$LAB_ROOT" --node arch --mode collect
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node arch --sequence 80 --mode observe
```

`prepare` retains the review without starting an update. `start` is admitted once
under that exact intent. A timeout remains ambiguous; `status` reconciles the same
request and does not start another mutation. These commands only operate the
nonce/DMI/process/host-key-guarded disposable fixture.

## Architectural implications and open acceptance

An `OnFailure` unit is only an execution entry point. Alpha79 reached that entry
point but its retained recovery child failed the lock contract before restoring
anything. The updater, recovery runner and retained script must agree on inherited
descriptor identity and ownership under the actual parent/child process topology.
Tests that stop before the restoration body cannot establish this property.

The BIND regression also requires producer/consumer coverage: acquisition
ownership and the current published generation describe different moments in the
same authority tenure. Legitimate publication must be recognized without erasing
historical ownership evidence or accepting an unrelated owner change.

Forward finalization is a useful recovery result, but it has a distinct acceptance
criterion from rollback. Current candidate hashes, an absent transaction marker
and healthy services cannot stand in for evidence that old binaries, data and
workloads were restored. Preserve the original failure and report the separately
verified recovery outcome.

These trials leave the following evidence gaps:

- The active-phase kill reached automatic recovery but failed before restoration.
  Successful full old-release restoration and power-loss recovery remain unproven.
- Debian's missing `dig` left its DNS answers unknown. Arch covered standalone
  loopback A/SOA over UDP/TCP, not a real secondary, transfer, TSIG or delegation.
- A live nonempty WAL prevented consistent before-state database inspection;
  Arch's after-state inspection was also unknown. Debian's later `integrity_check`
  returning `ok` does not prove semantic data preservation.
- All guests used genuine installer bootstrap TLS. Certificate issuance,
  rotation and independent renewal were not tested.
- Mail, hosted web requests, database application traffic, cron and independent
  boot/renewal workloads require separate before/after acceptance.

The [resilience contract](../../../docs/RESILIENCE-CONTRACT.md) remains a
requirement with open acceptance work. These observations do not close P0.1 or
prove that every update, failure or owner configuration is recoverable.
