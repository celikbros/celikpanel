# Recovery across an interrupted unit publication

*[Türkçe](UNIT-TRANSITION.tr.md) · P0.1 / P0.3 scoped implementation · 2026-09-14*

**Native candidate acceptance is pending.** No production panel has been updated
and no public release has been published for this slice. The earlier failures in
the [native result record](RESULTS.md) establish the defect, not acceptance of this
change. P0.1 and P0.3 remain open under the
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
- Native acceptance of the candidate, interruption during restoration, and reboot
  at supported checkpoints, including the recovery process itself.
- Reconstruction of missing or partially overwritten historical coordinator units
  when exact old/candidate provenance cannot be established.
- Full database, TLS renewal, DNS peer, mail and other independent workload
  acceptance across the supported matrix. Their earlier evidence limits remain.

Passing this slice must not be reported as universal self-repair or closure of
P0.1/P0.3. Exact native observations and their limits must be recorded separately.
