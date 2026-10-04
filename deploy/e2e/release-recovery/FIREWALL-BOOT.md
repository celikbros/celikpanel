# Independent firewall boot consumer - scoped native proof

D-025 invariants 1, 3 and 6; P0.5 remains open. This adds a standalone consumer,
not an installed-panel migration or removal command. The production boot unit
still invokes Agent. No Frankfurt/Boston service or installed update was touched.

## Contract

`cmd/firewall-restore --check | --restore` consumes only the fixed
`/etc/celikpanel/firewall.nft`. It shares the exact legacy/v2 policy decoder and
SSH parsers with Agent. The bytes/schema are unchanged; there is no automatic
normalization or new file format. Missing policy is a distinct no-op.

The Linux reader walks root-owned, non-group/world-writable directories without
following symlinks. It accepts the existing `root:celikpanel 0750` directory and
`root:celikpanel 0600` file produced by the root Agent's service group. The saved
file must be regular, single-link, bounded and 0600. A fresh read checks inode,
metadata and bytes before application. Root-owned native executables, SSH daemon
configuration and effective socket listeners are checked; unknown is refused.

A fixed nonblocking local lock excludes duplicate independent consumers. Native
nft preflight precedes one atomic batch touching only `inet celikpanel_fw`.
Current verified SSH ports and saved transition ports are retained. Errors give
the owner a preservation/inspection/retry action; they do not rewrite policy,
flush the whole host ruleset or invoke management/licensing services.

This source stage does not yet share its lock with Agent or prove concurrent
owner nft edits. It must not replace the installed boot path until that boundary,
packaging, upgrade/rollback retention and native failed-boot recovery are proved.
The final pre-apply check is not a general kernel compare-and-swap guarantee.

## Debian 13 AR, September 22, 2026

After AR's independently recorded recovery acceptance, the same disposable VM
was explicitly repurposed. Its previous recovery evidence remains intact. The
registered QEMU process, loopback SSH destination/key, nonce marker and DMI UUID
were checked before every command by the existing lab controller.

- Stop/disable Panel and Agent and move both standard binary paths to a private
  fixture evidence directory. Disable the old Agent-backed firewall unit.
- Seed canonical v2 TCP 2083 / UDP 53 / saved SSH 22 policy with actual Agent-group
  ownership. This is a fixture seed, not an Agent RPC producer test.
- Install only the locally built independent helper at the fixture path
  `/usr/local/libexec/celikpanel-firewall-boot-fixture`.
- The separate fixture unit runs `--check` then `--restore`, requires local files
  and a fixture-owned unrelated nft table, and runs before `network-pre.target`.
  It creates the standard sshd runtime directory. The unrelated table has a
  marker counter and is loaded independently on every boot.
- Collect a fresh observation, perform an orderly VM reboot, then reconnect over
  a fresh guarded SSH connection and collect a second observation.

[Machine result](FIREWALL-BOOT-AR.json) binds the helper digest, relevant source
file digests, toolchain, unit digest, policy digest, VM and two different boot IDs.
The final helper is `570bda6a4b980f8b0e0e791462778e42db5b7183c458a0809f8c369f75d7312e`.
The actual native consumer completed after reboot with both management binaries
absent and services disabled. SSH remained accessible. The loaded table retained
default drop, TCP 22/2083, UDP 53 and established connections. The independent
marker table remained unchanged.

`guest_firewall_boot.py` is a read-only collector. `verify_firewall_boot.py`
refuses same-boot, foreign-guest, active-management, altered binary/policy/unit,
failed consumer or missing-rule evidence. Negative verifier tests accompany it.
Private observations remain under the AR evidence directory; no SSH keys or
credentials are exported.

## Failed preliminary trials and refusal checks

The first unrelated-table fixture contained invalid nft syntax, so the native
consumer did not run. After correcting only that fixture, the initial reader
refused the supported `root:celikpanel` directory. Its former root-group equality
was stricter than the producer contract. The reader now requires root ownership
and no other writer, and tests retain rejection of group-write/non-root paths.
These failures are retained rather than counted as successful boots.

Before the final source build, separate native refusal checks used an unsupported
version and a 0644 policy. Both returned nonzero, preserved the submitted bytes
and left both live nft tables unchanged. Exact valid fixture bytes were restored.
These preliminary refusal checks are not a production emergency-target test or
proof for every later binary; final-source unit tests cover the same boundaries.

## Remaining acceptance

The source consumer is not packaged or selected by installed units. Shared Agent
exclusion, producer-to-new-reader migration, retained old/new boot consumers during
update/rollback, unsupported-policy boot recovery, Arch/RHEL and power-loss tests
remain open. No management-removal command or complete workload independence is
claimed. Mail/website certificate renewal and the rest of P0.5 still require their
own independent lifecycle and native evidence.


## Shared process exclusion (next source stage)

`internal/firewalllock` now supplies the same fixed native lock to the independent
consumer and the Agent's apply, committed recovery, legacy check/restore and
status/audit entry points. Lock order is the existing process mutex followed by
nonblocking native exclusion. Exclusion is a required runner method; production
adapters cannot silently fall back to an unlocked path. The helper never acquires the management mutation
ledger, so it introduces no reverse lock order. Agent's prior authorization and
durable commitment checks are unchanged.

Busy or unsafe lock state is unverified, not disabled, ready or a clean terminal
failure. Committed recovery reports an ambiguous host outcome when it cannot
acquire the lock, retaining the unresolved operation. No probe, publication or
second mutation starts under contention. Root-owned 0600 lock files support both
root:root and the Agent's root:celikpanel group without metadata normalization.
The ephemeral lock is released by the kernel after process death.

[AR native contention evidence](FIREWALL-EXCLUSION-AR.json) records both final
candidate Agent boot modes and both independent consumer modes refusing the same
held OS lock. Kernel rules and saved policy stayed identical. After release, both
preflight commands succeeded. Ordinary management binaries/services remained
absent/disabled; candidate CLIs ran only from the private disposable fixture path.
The final Agent digest is `eea3d17d904da68933876459f9c723c844ae438d5ed09f14bac0672d26dcb690`
and consumer digest is `a9637ab4ff236b9199585e88dd58b537613414fee503f192937003ff92e978af`.
This is native lock contention, not simultaneous full Agent RPC acceptance.

Race-enabled tests additionally prove independent-process exclusion and automatic
lock release after SIGKILL; untrusted modes/owners/symlink/hardlink rejection;
read-only group compatibility; no host probe/publication under Agent contention;
and preservation of an ambiguous recovery outcome. Existing firewall/SSH/security
audit regression tests pass. The older reboot evidence above remains bound to
its original binary; it is not relabeled as a reboot of this later source.

Packaging, installed-unit activation, recovery across old/new consumer versions
and remaining owner-concurrency/workload acceptance are still open.
