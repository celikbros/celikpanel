# Recovery across an interrupted unit publication

*[Türkçe](UNIT-TRANSITION.tr.md) · P0.1 / P0.3 scoped implementation · 2026-09-14*

**The bounded native rollback scenario passed on Arch and Debian 13.** The
unpublished candidate was interrupted after unit publication and before reload;
both guests automatically restored the old running release. This slice has not
updated any production panel or published a release. The earlier
[native failures](RESULTS.md) remain recorded. P0.1 and P0.3 stay open under the
[resilience contract](../../../docs/RESILIENCE-CONTRACT.md).

## Problem and bounded change

The updater could republish unchanged service unit bytes and stop before
`daemon-reload`. The recovery entry point then required `NeedDaemonReload=no`
before reaching the restoration body. A legitimate interrupted publication
therefore prevented recovery.

The [guard library](../../release-transaction-guard.sh) separates three proofs:

| Proof | What it permits |
| --- | --- |
| Guard files | Read-only validation of the exact helper, file metadata and complete immediate drop-in directory contents. Only the guard and the canonical optional Agent runtime-preservation drop-in are accepted. It never queries or changes systemd. |
| Restore admission | The same file proof plus exact loaded drop-in paths, unit fragment and start condition. A pending reload is accepted only when **both** coordinators are inactive or failed, with `MainPID=0` and `ControlPID=0`. Unknown observations refuse admission. This grants no service start or automatic reload. |
| Strict loaded guard | The normal requirement for a fully consumed manager definition remains. Rollback repeats strict guard and recovery-foundation verification after its existing reload checkpoint and before enabling or starting services. |

An unconfirmed recovery-foundation `.intent` still prevents rollback admission;
this change does not infer that an incomplete foundation is safe.

## Authority and file publication

[Rollback](../../../rollback.sh) verifies the complete v6 snapshot manifest,
payload requirements, target commit/tree and applicable transaction identity
before admitting the unit transition. The apply-only
[installer](../../../install.sh) independently rechecks the complete snapshot
envelope and its exact active-update binding; database and TLS semantic admission
remain part of the updater/rollback checks.

The [unit-transition library](../../release-unit-transition.sh) handles only the
fixed Agent, panel and firewall-restore unit files. Each installed file must match
the verified snapshot or target release. A mixture of those two states is a
recognized interrupted publication. Unknown bytes, unsafe metadata or unexpected
coordinator absence are refused, preserving owner changes.

Changed files are staged beside their destinations, checked and synced, then
atomically renamed. Admission is repeated after staging and before replacement.
Already matching files are left untouched, preserving their inode and timestamps.
Restoration uses the same rules; an absent firewall unit is restored only when
that absence is recorded in the snapshot. Unrelated owner unit files are not
replaced. The library neither reloads systemd nor changes service enablement.

## Evidence and remaining work

The [guard fixture](../../test-release-transaction-guard.sh) checks read-only
behavior, pending-reload separation, owner drop-ins and adversarial loaded
definitions. The [unit fixture](../../test-release-unit-transition.sh) exercises
real filesystem publication, partial failure, retry and intervening owner edits.
CI runs both with root metadata. These checks do not establish full native
rollback or workload recovery.

The following remain outside this slice:

- The separately versioned minimal recovery executor and full durable checkpoint
  contract required by P0.3; recovery still uses the existing retained-release path.
- Other candidate checkpoints, interruption during restoration, and reboot at
  supported checkpoints, including the recovery process itself.
- Reconstruction of missing or partially overwritten historical coordinator units
  when exact old/candidate provenance cannot be established.
- Full database, TLS renewal, DNS peer, mail and other independent workload
  acceptance across the supported matrix. Their earlier evidence limits remain.

Passing this slice must not be reported as universal self-repair or closure of
P0.1/P0.3. Exact native observations and their limits must be recorded separately.

## Native acceptance on September 14

**PASS for this checkpoint on both systems.** Cell
`release-recovery__9512ecbd3287c9f0`, retained under
`/var/tmp/cp-release-drill-20260914-d`, used two fresh guests and the genuine
signed Alpha75 baseline. The real Agent acquired BIND and created/edited a zone.
Before capture, a harmless owner comment was added to each coordinator unit and
systemd was reloaded: both reported `NeedDaemonReload=no`. Thus candidate
publication changed a legitimate, snapshot-recorded old revision; the test did
not manufacture a stale manager flag. Debian's DNS probe dependency was installed
before its measured baseline.

The exact committed candidate was built in a clean export with Go1.26.5 and the
normal `make dist` path (local frontend build Node26.8.1/npm12.0.2). It was
**unsigned and unpublished**, admitted by the existing local prebuilt bootstrap.
This does not prove signed Agent update admission. Each operation had one durable
start intent, a native systemd worker and the real `OnFailure` recovery service.

While each exact worker was frozen in `active`, the fault controller proved the
candidate binaries, installed unit/helper hashes, 249 retained-release files and
all 129 snapshot files. Both coordinators reported `NeedDaemonReload=yes`. The
controller then killed only that worker's identified cgroup. No manual rollback
or second update start was used.

| System | Operation | Kill UTC | Rollback complete UTC | Seconds |
| --- | --- | --- | --- | --- |
| Arch | `ee810aa215a03c4ecdfdf4d59301a42c` | `11:16:00.079092` | `11:16:08.122731` | 8.043639 |
| Debian 13 | `0b1350e697c913a02325ff2a03a7d30c` | `11:18:21.623794` | `11:18:31.279986` | 9.656192 |

Each journal records exactly one native recovery invocation, entry into the real
restoration body, canonical database restoration and the exact rollback-complete
message. Independent observations then confirmed:

- Original Alpha75 Agent and panel hashes both installed **and running**, with
  both services active; the old web tree matched.
- Owner baseline unit hashes restored, both loaded guards reporting
  `NeedDaemonReload=no`, and loopback HTTPS returning HTTP200.
- Identical authoritative A/SOA answers over both UDP and TCP, and identical
  installed/served bootstrap certificate fingerprints.
- No active transaction remained; the recovery service exited successfully.

The measured 8–10 seconds is a result for these two trials, not a general recovery
SLA. Live database semantic comparison stayed **unknown** because the probes saw
nonempty WAL; restoration log text does not replace that proof. Bootstrap TLS is
not issuance or renewal coverage. Arch also reported unavailable running-kernel
modules, so firewall/VPN operation was not established. No secondary DNS transfer,
mail, application database traffic, owner edits during the native fault, recovery
process interruption or reboot was tested here. The broader matrix remains
**INCONCLUSIVE**, with `p0_complete=false` in both independent audit records.

Private records retain exact journals, before/after observations, fault proofs
and baseline customizations. Their bounded audit summaries are identified below;
credentials and private key contents are not published.

```text
candidate commit: bd77acd0cd55408718feebb6a1e605ccadfd97be
candidate tree: e7b73136a9b8af2cbe455fcfadab985bf7cefbc3
archive SHA256: 5ef7140eea054c5461b65f12f7adb8a2820bfde9ba07607d25e3a44291c7d473
Arch snapshot: 20260914T111552Z-from-unknown-to-bd77acd0cd55408718feebb6a1e605ccadfd97be-48485908c8f32206ff52be642245cae7
Debian snapshot: 20260914T111811Z-from-unknown-to-bd77acd0cd55408718feebb6a1e605ccadfd97be-c413728cb4adc05de792ac97519fe7fb
Arch native-outcome-manager-verified.json SHA256: 52731c76eeed9fd142cc4c142899ca17de375d279b364b12e7e8a44c86adde3b
Debian native-outcome-manager-verified.json SHA256: c57541474b36f6fa573d2f13f7739302f0a57d249fab80136d2b64927ee687bf
```
