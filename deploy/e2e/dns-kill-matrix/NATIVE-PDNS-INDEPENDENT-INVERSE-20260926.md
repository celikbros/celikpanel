# Native PowerDNS independent inverse recovery — 2026-09-26

**Scope:** P0.4; constitutional invariants 2 (continuity and recoverability), 3 (truthful state and shared contracts), and 4 (simple owner recovery path). This report records disposable native Debian 13 recovery slices. It is evidence for Stage 1, not its closure.

## Initial inverse result

The disposable QEMU cell `pdns-adopt__rolling-back__after-write__standalone__peer-reachable` ran from `/var/tmp/cp-dns-inverse-20260926` using verified pinned official images. Its tagged Agent received a real SIGKILL and exited 137 (`kill_proven: true`). At the stopped boundary, the producer-written journal was in `rolling-back` for exact request `ec8bba862b6eea4a6052cbe721661dd7`; Agent and Panel were inactive while native PowerDNS remained active.

The first test-only inverse invocation stopped safely because this reduced fixture did not contain the release-transaction lock directory. In the disposable VM only, the fixture prerequisite was supplied as root-owned mode-0700 `/var/lib/celikpanel-release-transaction` with an empty mode-0600 `transaction.lock`. The same request was then passed to the existing test-only `completeInstalledPDNSAdoptionInverse` path. It completed successfully in 2.13 seconds with Agent and Panel still inactive.

Afterward, the terminal ledger recorded `failed`, phase `interrupted`, and error code `dns_engine_switch_rolled_back_by_owner_recovery`; the switch state and journal were absent, and PowerDNS remained active. The initial trial rebooted with Agent and Panel disabled; they remained inactive while PowerDNS stayed active/enabled. The terminal ledger persisted, the journal remained absent, and PowerDNS answered authoritatively (`AA`) for `www.s1-kill.test A 192.0.2.10` over UDP and TCP.

## Interrupted inverse continuation

A second native trial cut recovery after durable terminal evidence but before journal retirement. At the checkpoint, the SIGKILL marker and tagged process were absent, the journal was `rolled-back`, and the ledger was still `running`. A same-request retry reached a terminal result.

A separate SIGKILL cut occurred after the terminal ledger write and before journal retirement. At that boundary the marker remained, the journal was `rolled-back`, native PowerDNS was active, and Agent and Panel were inactive. After reboot, same-request retry passed. The fixture's Agent and Panel services auto-started during this first reboot; they were manually stopped/disabled before the retry. Therefore this reboot does not prove they stayed stopped throughout boot. A **second** reboot with both management services disabled left Agent and Panel inactive/disabled, while native PowerDNS remained active/enabled and answered authoritative `www.s1-kill.test A 192.0.2.10` queries over UDP and TCP.

## Recovery contract and limits

The inverse uses the exact accepted request and persists a terminal owner-recovery verdict while preserving native DNS service. Persisted DNS state, journal, and ledger schemas stayed at v1. The independent path has since been exercised through both a test harness and the protected owner recovery CLI described below. No installed server was changed.

These trials demonstrate selected PowerDNS adoption rollback and interruption-continuation points, including retry after reboot. They do **not** establish Stage 1 completion: other DNS inverse kinds, remaining interruption and owner-edit boundaries, and the complete native acceptance matrix are still unproven. The reduced fixture's missing release-transaction lock was corrected in its normal guest preparation and verified on subsequent fresh VMs.

## Retained evidence

Initial inverse archive: `/var/tmp/cp-dns-inverse-20260926/pdns-inverse-evidence.tar.gz`
SHA-256: `6acdf9296e2ca243d5607f3186a8915598378d943bb0c25f6a1fba63808a4965`

Interrupted-recovery checkpoint archive: SHA-256 `aa0f07d293d91c2fe3ccb78eeddc218ae67e0197c37d5105dbb099d919f4c205`

Before-reboot archive: SHA-256 `a22dfcafd5209da969dc6797521945112f06bf927e82f8d0eccac45f8ee9cc52`
Resumed-recovery archive: SHA-256 `194ea6ec4f692c42058642794b71767e8a6f262271c46d4e2e07e059a7c39b41`

## Journal-retired interruption cut

A fresh disposable Debian VM repeated the producer SIGKILL and ran the test-only inverse for exact request `ec8bba862b6eea4a6052cbe721661dd7`. The inverse itself received a genuine SIGKILL after its `journal-retired` marker was fsynced (PID 2486). At inspection, that process was absent, the switch journal was absent, and the exact terminal owner-recovery ledger verdict was present (`failed`); Agent and Panel were inactive and PowerDNS was active.

A same-request retry with `EXPECT_TERMINAL_LEDGER=1` passed by recognizing the historical terminal verdict. That result alone does not prove current DNS service, so a separate native check queried `@10.0.2.15 www.s1-kill.test A` over UDP and TCP; both returned authoritative (`AA`) `192.0.2.10`. This adds evidence for the journal-retired retry boundary. Stage 1 remains open.

Journal-retired archive: `/var/tmp/cp-dns-inverse-20260926/pdns-inverse-journal-retired.tar.gz`
SHA-256: `6e7b551d3e5f5e5a426bf9d82866fb5b4223439f2ecbdac91bbde8179ee88891`

## Owner-edit refusal during independent inverse

A fresh disposable Debian cell first proved the producer SIGKILL at `rolling-back`. Before the owner edit, `/etc/powerdns/pdns.conf` had SHA-256 `8b46927e48be83b2f0afc38efabdc2e2237daa5281b1a07ea44989ffa41f262a`; the switch journal hash was `9b3c7b7bcab164bb83e8faa46a9f8cb1f548fd273bbcec23295e19d83a4a83c7`, and the ledger hash was `bc0e84a5c172b430b5fc4a4b2e931a8d20466ebeb8809b3df2410a4a2dfbc038`. PowerDNS PID 1715 was active while Agent and Panel were inactive.

The disposable VM then appended `# owner-edit-refusal-trial` to the owner-managed main PowerDNS config. A transient BOM introduced by the transfer method was normalized before the trial; the tested file was plain text, mode 0640, root:pdns, size 20607, SHA-256 `74b077d71a46876e49b6d04cfcd8844bb77d2b1dbce958a7e18ecf59fc2f4484`. The exact tagged inverse refused in 0.02 seconds with: `verify frozen PowerDNS config: PowerDNS config /etc/powerdns/pdns.conf differs from frozen owner, mode, size or file type`.

After refusal, the edited owner config hash was unchanged; journal and ledger hashes were unchanged; PowerDNS remained active with the same PID; Agent and Panel remained inactive. Authoritative UDP and TCP queries still returned `AA` and `A 192.0.2.10`. This is one bounded owner-edit refusal cell: it shows the inverse leaves the later owner change and accepted operation evidence intact while native DNS serves. It does not establish race-free detection at every write boundary or the full owner-edit matrix; Stage 1 remains open.

Owner-edit refusal archive: `/var/tmp/cp-dns-inverse-20260926/pdns-inverse-owner-edit-refusal.tar.gz`
SHA-256: `0bf4f90a7b51b9bb161289f2ba4c4a4af2e1fae46427614d95cfb5ce8e0e1693`

## Protected owner CLI smoke

A fresh disposable Debian cell repeated the producer SIGKILL at `rolling-back` for request `ec8bba862b6eea4a6052cbe721661dd7`. The offline kit was enrolled through the protected runtime (manifest SHA-256 `f569893eaa36b77096cf19ef481921398e40cb0f829afe3fea5a0f72eadf8c7c`; binary SHA-256 `0f532efbd3c267adc512b4e8a62a574b541bf7c317ac80cc4877b3ddd0eab144`). Running the protected `/usr/libexec/celikpanel/recovery recover-dns-pdns-adoption --request-id ec8bba862b6eea4a6052cbe721661dd7 --lang en` exited 0 with a terminal inverse. The same arguments through the unselected `/root/celikpanel-owner-cli-smoke/recovery-runtime/bin/recovery` were rejected as `runtime_changed`. A protected same-request retry exited 1 and reported the historical verdict with current health unknown; it made no new mutation.

After the smoke, Agent and Panel were inactive (PID 0) and native PowerDNS was active (PID 1709). `dig` was unavailable in this guest, so this CLI smoke includes no additional UDP/TCP DNS query evidence; the separate native query evidence above remains separate. The disposable guest was stopped and torn down. This was a disposable smoke build using `/usr/sbin/go 1.27.0-X:nodwarf5`, not a reviewed release toolchain or release-grade acceptance. Raw VM archive transfer was rejected by automatic approval review due potential internal service-mutation data egress, so this CLI smoke retains bounded command outputs only.
