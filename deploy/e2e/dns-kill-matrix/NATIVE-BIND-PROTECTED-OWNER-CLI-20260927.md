# Protected BIND owner recovery: native acceptance, 2026-09-27

**Passed scope:** rollback of an accepted standalone managed PowerDNS-to-BIND
switch on disposable Debian 13. The independent protected owner CLI restored
PowerDNS, survived its own interruption and an orderly reboot, then completed
the same request. Panel and Agent remained disabled during reboot and recovery.
No installed user server was changed.

This exercises the V2 journal, frozen PowerDNS configuration and logical
database proof, exact BIND before/after configuration and unchanged include
envelope. The PowerDNS database is verified, never restored or rewritten.

## Final candidate and evidence

| Artifact | SHA-256 |
| --- | --- |
| Recovery binary | `282c55d5d748f02adf34c378831abdc2bf7e12ed325f7f8a0c1a7c4edfde2b34` |
| Selected runtime manifest | `ce42268f81410c6047bb31636e2f56b95add6b0dd9a473571202acdb2cedf50e` |
| Production Agent | `94ffda51cc1e593176fa64a2a644e06d1fbe2c6fdf542d860f97b7b8d56d6364` |
| Tagged producer | `b3aeaf197787461c785454260a1c017061590ad3ca3b86de02d7d598f1a62001` |
| Final evidence archive | `7ff083e6c4b5094c0649a173be898894d611a3a53c1eae1d2eab59e4d05dae6d` |

Archive: `/var/tmp/cp-bind-source-inverse-20260927/native-final-late-recovery.tar.gz`.
The local source worktree was dirty; these are exact artifact identities, not
production-signed release provenance.

Request: `8b14693eaf9461f13a415d5e637b6d91`.
Cell: `bind__rolling-back__after-write__standalone__peer-reachable`, explicitly
using `--stop-after-kill-for-independent-recovery --bind-rollback-after-target-started`.
The default early precursor remains unchanged.

## Observed sequence

1. At 09:11:26 UTC the real producer PID 3710 was killed at
   `rolling-back/after-write`, after the explicit `target-started` precursor.
   Exit 137 and `kill_proven: true` were retained. Before the signal, BIND was
   active, PowerDNS inactive, and BIND answered authoritatively over UDP and TCP.
   Journal SHA-256:
   `e7ea589fe9c6faa5a765cb215c647daba710e93b60e845e0a4a774449239bfa1`.

2. The harness appended an owner zone statement to BIND's main config without
   reloading it. The protected CLI exited 3 specifically because the main config
   differed from its frozen include layout. The edited bytes, journal, ledger
   and named PID 6084 remained unchanged; original DNS answers continued.
   Ledger hash during refusal:
   `40efdaab79d3831cb9e0d44d597b98e59a6b8b9f92ae077535b6ae74e5556678`.
   Edited main hash:
   `e426c8a0cc0b0c4ba2b610bade806868b199452eab939812a118320ad4127402`.
   The harness then removed only its exact edit after checking those bytes.
   This proves pending, unreloaded owner-edit preservation, not a newly loaded
   owner zone or arbitrary concurrent edits.

3. The same selected CLI stopped the operation's BIND target and restored
   PowerDNS. An external 1-ms observer sent SIGKILL only after reading durable
   `rolled-back`; the CLI exited -9 after 9,135 ms. Checkpoint hash:
   `0e973e270b9306db0f9632d4c6e49fa14bc3d1946ea4e5f0deab9ba9023c55e7`.
   The producer restores its generation pointer before writing rolling-back;
   the adapter verified the immutable target, exact after-config, native process,
   stopped source and authoritative answers before stopping the active target.

4. Agent and Panel were disabled before orderly reboot. Boot ID changed from
   `4f36ee15-74b1-45be-ad65-b984df7f1bd3` to
   `97a0be6a-5a53-4679-867e-cdb843b9b02c`.
   PowerDNS was active/enabled at PID 636; named was inactive/disabled and both
   management units remained inactive/disabled.

5. The same protected request exited 0 after 3,045 ms and retired the journal.
   The ledger recorded `failed/interrupted`,
   `dns_engine_switch_rolled_back_by_owner_recovery`, attempt 1, finished at
   `2026-09-27T09:13:24.830951223Z`. This is the terminal rollback result of
   the interrupted switch. Terminal ledger hash:
   `e783310286cc67bcfa46e298e5cdefb0292ce02828f2a5b54386510361549379`.

6. Post-retirement retry exited 3 with the historical verdict and current native
   health unknown. The ledger hash, absent journal and unit states were
   unchanged. Separate A queries before reboot and after the retries returned
   AA=true and `www.s1-kill.test A 192.0.2.10` over both UDP and TCP.

The archive was read back and its hash, actual kill proof and terminal job
checked. The disposable QEMU pair was then stopped and its validated cell
directory/temporary overlays removed. Evidence and build artifacts were retained.

## Failed attempts retained

All archives below are under `/var/tmp/cp-bind-source-inverse-20260927/`.

| Attempt / archive | Finding and correction | SHA-256 |
| --- | --- | --- |
| `native-attempt1-parent-refusal.tar.gz` | Before-intent refusal of native root:bind setgid directory. Production reader now admits the exact secure native parent metadata; the guest directory was not rewritten. | `f3dc03bb0eb12dca2ca4667670df6f9172f99bbc1fa2491ecbd2271629ba9de7` |
| `native-attempt2-layout-refusal.tar.gz` | Before-intent refusal of Debian's root-hints include. Parser now accepts the two fixed native alternatives and freezes the actual choice. | `bcc9303e4f2b1556286a1b2d5f88e9dc7d9dfcaaa16ef92dc618014981d0b116` |
| `native-attempt3-early-cut.tar.gz` | Genuine early exit 137; source still active, target masked. Recovery refused missing masked ExecStart. Product now proves exact masks, then loaded vendor identity after unmasking. | `f0540c5f2dc487a11624b20a018f355b51717b4e39390bd6a6c28c4ee21a75a8` |
| `native-attempt4-source-gate-refusal.tar.gz` | General readiness rejected its own published journal before stopping PowerDNS. Accepted-manifest proof now runs after freezing source and before intent; exact frozen proof remains immediately before source stop. No broad readiness bypass. Kill not proven. | `cf1679b8ef9d61d52c656162eb67218d138b39cddf784e732e75b9e4cab61f36` |
| `native-attempt5-harness-refusal.tar.gz` | Controller expected the wrong source-config order; fixed against a real producer fixture. Exploratory CLI also exposed root-path versus apt-label comparison, now corrected with production-enum regression. Cleanup kill and unrelated edit refusal were not counted as acceptance. | `de2dfa802660e53a0f4ff58a7cf8d561231cd15151c71278d97e71de3101ffff` |

## Checks and scope

Final Linux Agent, recovery, binddns, dnsenginerecovery, native-contract and
recovery-runtime package tests passed, as did focused artifact/config tests and
recovery/binddns/dnsenginerecovery vet. The 100-test Python controller/bootstrap suites completed successfully
with 13 platform skips. Relevant whitespace checks passed.

| Existing inverse kind | Admission / evidence |
| --- | --- |
| External standalone PowerDNS adoption | Separate bounded [Debian owner CLI evidence](NATIVE-PDNS-PROTECTED-OWNER-CLI-20260926.md). |
| PowerDNS-to-inactive-BIND switch | This standalone Debian/apt V2 rollback passed. Legacy V1 or incomplete V2 cannot invent missing source proof. |
| Running-BIND adoption | Independent restore/reload adapter remains unimplemented; stopping inverse cannot substitute. |
| PowerDNS switch/reinstall | Independent inverse remains unimplemented; database restoration is outside this change. |
| Arch, paired, empty source, initially active BIND | Outside this command's admitted scope. |

Item 1 and P0.4 remain open for the listed inverse kinds/variants. Source and peer
fixtures belong to item 2; the full fault/workload matrix to item 3; signed
release admission to item 4. This trial is owner-initiated recovery, not automatic
boot recovery. It does not prove uninterrupted service, power-loss durability or
all owner-edit races.
