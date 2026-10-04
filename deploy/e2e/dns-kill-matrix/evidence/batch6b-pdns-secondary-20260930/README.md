# Fresh PowerDNS secondary, Arch BIND secondary, reinstall and takeover cells, 2026-09-30

Scope: P0.4, D-025 invariants 1, 2, 4 and 5, D-022, D-024, D-026; acceptance register rows 3, 5, 12 and 14 (exploratory). First native run of commit `6f2fb028` ("Fresh paired PowerDNS primary: recovery policy, producer rules, single closed gate"), which contains `66db850c` (durable rollback decision, PowerDNS secondary consumer state, DNS-only hold) and `2ac6dcbe` (reinstall setup reads the v2 state receipt, PowerDNS consumer `options`, versions). Eight cells in fresh disposable QEMU guests on the local WSL `archlinux` host: seven on a Debian 13 kill guest, one (`c5`) on an Arch kill guest; the five paired cells used the other guest as the panel-free native primary peer. It did not touch an installed panel, a remote host, a release or a signed bundle. It is exploratory: nothing here passes an acceptance-register row.

**Result: 7 of 8 passed, 1 unverified** (classification is `result.json` `status`; the peer verdict is `paired-secondary-peer/peer-verdict.json` `status`; "combined" is `run-prepared`'s exit, for paired cells `peer-verdict.json` `combined_exit`):

| Dir | Cell, fixture, run flags | Guest `status` / `safety_status`, classification, `complete_verdict` | Reboot | Peer verdict | Combined |
| --- | --- | --- | --- | --- | --- |
| `c1-pdnssec-bindpri` | `pdns-switch__target-started__after-write__paired-secondary__peer-reachable`, `uninitialized`, `--peer-engine bind --peer-catalog-format bind --reboot-after-recovery --disable-management-before-reboot` | **passed** / passed, `target_converged`, passed | after recovery, management disabled: passed | **passed** | **0** |
| `c2-pdnssec-pdnsnative` | same cell, `--peer-engine pdns --peer-catalog-format pdns-native`, same reboot flags | **passed** / passed, `target_converged`, passed | after recovery, management disabled: passed | **passed** | **0** |
| `c3-pdnssec-prestart` | `pdns-switch__target-staged__after-write__paired-secondary__peer-reachable`, `uninitialized`, `--peer-engine bind --peer-catalog-format bind` | **passed** / passed, `target_converged`, passed | none requested | **passed** | **0** |
| `c4-pdnssec-late` | `pdns-switch__target-verified__before-write__paired-secondary__peer-reachable`, `uninitialized`, `--peer-engine pdns --peer-catalog-format pdns-native` | **passed** / passed, `target_converged`, passed | none requested | **passed** | **0** |
| `c4b-pdnssec-rollingback` | `pdns-switch__rolling-back__after-write__paired-secondary__peer-reachable`, `uninitialized`, `--peer-engine pdns --peer-catalog-format pdns-native` (extra, see Cell 4) | **passed** / passed, `target_converged`, passed | none requested | **passed** | **0** |
| `c5-bindsec-arch` | `bind__target-staged__before-write__paired-secondary__peer-reachable` (Arch kill host, Debian peer), `uninitialized`, `--peer-engine pdns --peer-catalog-format pdns-native --reboot-after-recovery --disable-management-before-reboot` | **passed** / passed, `target_converged`, passed | after recovery, management disabled: passed | **passed** | **0** |
| `c6-reinstall` | `bind__target-staged__after-write__standalone__peer-reachable`, `managed-bind-absent`, `--reboot-after-recovery` | **unverified** / unverified, no classification, `complete_verdict` null; `kill_proven: false` | not run | n/a | **2** |
| `c7-takeover` | `bind__target-staged__after-write__standalone__peer-reachable`, `unmanaged-bind-stopped`, no flags | **passed** / passed, `target_converged`, passed | none requested | n/a | **0** |

Every requested flag combination was admitted (early dry run before the guests started, `run-prepared-dryrun-early.log`, and the dry run after preparation). The only failure reason in any `result.json` is `c6`'s `verification_failures`:

> `scenario trigger exited before boundary notification with exit code 1 (raw 1)`

**`c6` is a product finding, not a kill-matrix result** (details in Cell 6). The measured reinstall's own `apt-get install bind9` aborted in dpkg because the owner-style purge of `bind9` removed the `bind` group while the `dpkg-statoverride` entry `root bind 1775 /var/cache/bind`, which the managed BIND setup registers (`cmd/agent/dns_engine_bind_root_linux.go:186-191`), stayed behind. No kill happened; the reinstall recovery path and its predicted rollback verification problem were **not** reached.

**Boundary.** In all seven cells with a kill the tagged Agent was in state `T` when SIGKILL was delivered (exit 137, `/proc` entry absent after reap). The window from the marker's `recorded_at` to SIGKILL was 14.4–31.5 ms. No unit-journal entry of any DNS or management unit and no file mtime in the retained tree listings falls inside that window (`boundary-window.txt` per cell). In `c5` the boot-1 sampler had already died (see Cell 5), so there the check rests on the unit journals and the final tree listings only.

**Native answers** (PowerDNS 4.9.17 consumer on Debian 13; primaries PowerDNS 5.1.4 or BIND 9.20.29 on Arch):

- **Metadata for consumed members: none observed.** In every PowerDNS-secondary database read (`pdns-db-post-collect*.txt` after recovery in `c1`–`c4b`; the watcher copies with their WAL in `c2`–`c4b`, including `c2`'s copy taken 76 ms before the marker with the daemon's consumer writes in it), `domainmetadata`, `comments`, `cryptokeys`, `tsigkeys` and `supermasters` hold **zero rows**, for both producers. The rule of `66db850c` did not have to refuse on metadata in these runs; this is two producers, one PowerDNS version, no DNSSEC.
- **Exact `options` value** on the consumed member row (`s1-kill.test`, type `SLAVE`, `master` `192.0.2.11`, `account` `""`, `catalog` the consumer catalog), recorded by the controller in `pdns_secondary_rows.member_options` (`pdns-secondary-options.json`) and accepted:
  - BIND-format producer (`c1`, `c3`): `{"consumer": {"unique": "b076e9241974292fffe8ecc0209b7ace316d1d36063423d9ccb0849a."}}` (the SHA-224 label, as in batch 5);
  - PowerDNS native producer (`c2`, `c4`, `c4b`): `{"consumer": {"unique": "tlhnkturltuku63bhersj40lbi0vkdvt."}}` (the base32hex label the PowerDNS producer assigned).
  The CONSUMER row itself: `master` `192.0.2.11`, `account` `celikpanel-peer-catalog-v1`, `options` NULL, `catalog` NULL, `last_check` set, `notified_serial` NULL.
- **What a PowerDNS consumer re-serves.** The consumer stores the catalog as an ordinary zone of 5 rows: SOA and NS (`invalid`) enabled; the `version` TXT `"2"` and the member PTR `<label>.zones.<catalog>` → `s1-kill.test` stored with `disabled = 1`; one empty-type `zones.<catalog>` ENT row. TTLs follow the producer (60 for the BIND format, 0 for the PowerDNS producer's TXT/PTR). Over DNS the guest answered the catalog SOA authoritatively (`aa`) over UDP and TCP with the producer's serial (1 for BIND, e.g. `1790698723` for PowerDNS in `c2`); the PTR and TXT were not queried, so "not served because disabled" is a reading of the stored rows, not an observation. The member holds SOA, NS ×2 and A ×3.

## Build and fixture

- Tested source: commit `6f2fb028494a71f8ad396ba0abb57d06d962e451` (`feat/dns-artifact-separation`), extracted with `git archive 6f2fb028` into guest-host ext4 (`/root/cp-b6b-src`). The harness ran from that tree (hashes in [build/harness-files.sha256](build/harness-files.sha256)). At the end `/root/cp-b6b-src` was compared with a fresh `git archive 6f2fb028`: the only difference is a Python `__pycache__` directory ([build/source-tree-vs-archive.diff](build/source-tree-vs-archive.diff)). The working tree held uncommitted Go edits by other agents; nothing was built or run from it.
- Repository `HEAD`: `6f2fb028` at the start (16:05Z) and when the build read it (16:07:17Z, [build/build.log](build/build.log)); `aa6b9380` at the end (16:58Z). Other agents committed `a47b4a40` ("Owner enrollment for a PowerDNS secondary; package pdns-peer-inspect"), `01a450e6` and `aa6b9380` during the run; none of them was built or tested here.
- Toolchain: `/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go`, `go1.26.5 linux/amd64`, `GOTOOLCHAIN=local CGO_ENABLED=0 -trimpath -buildvcs=false`, as in batches 4–6a. Hashes: [build/artifacts.sha256](build/artifacts.sha256), `artifacts-root1.sha256` and `artifacts-r2.sha256` (identical copies in both work roots).

| Binary | Build | SHA-256 | vs batch 6a (`94cd124b`) |
| --- | --- | --- | --- |
| `agent` | `./cmd/agent` | `069faac5882ee85847364465c5b4425780da089c4aa848e9d021a77b9c03c431` | different |
| `agent.kill` | `./cmd/agent`, `-tags dns_kill_matrix` | `d9b53961dcb45fd94c0c860b5a2b176b09e41b484ea4358705f63ded93eedcfb` | different |
| `panel` | `./cmd/panel` | `fd902ecb1bba7dc9dc7493c8d7c32e4d897c5f4abe8c3cc024bb4c3132b0bbb6` | different |
| `dns-kill-trigger` | `./cmd/dns-kill-matrix-trigger` | `5476eb9996a467322aefffb8c0665bbdb0143fbc04cdd92ee424ee061660a693` | identical |
| `recovery` | `./cmd/recovery` | `dc131f4eee8361b01592e5b5b17d871b9cfd6c35799bc433d6f9be9bed25abe4` | different |
| `agent-checker` | `deploy/recovery/agent-checker.sources` | `82f2772fe0257dde9139d0f85a34be58e89c98d6a9d1775090f2ecf4cc3b8975` | identical |
| `panel-checker` | `deploy/recovery/panel-checker.sources` | `82f23b48908e152e55ad76598ec8be0f780805f463fe7e48ed402d87bc5984e0` | identical |
| `schema17-bridge` | `./deploy/schema17bridge` | `fe277c46ea8b50f00a80e71976198f28db89667789a737e0a78c0f4a83e9cb87` | identical |
| recovery runtime `runtime.manifest` | `go run ./deploy/recovery/bundle` | `3694af9c4b532126a0743a5aaca4bac1f90ebaf31ba7ffbe65c5ded5e016c22d` | different (only `bin/recovery` changed) |

- `web/dist` is untracked and was copied from the Windows checkout; its hash list equals batch 6a's. `oi-smstatus` (`ff9abfa1…7890`) is byte-identical to the earlier runs.
- **Recovery runtime enrolled in both standalone cells** (`c6`, `c7`, `enroll-recovery-runtime.log`). The bootstrap admits enrollment for standalone cells only, so the paired cells record their status reads as `available: false`.
- **Two work roots.** `c2` repeats `c1`'s cell ID and `c7` repeats `c6`'s, so those two used `/var/tmp/cp-b6b-0930/r2`; the others `/var/tmp/cp-b6b-0930`. Both initialised with `fixture.py init-root`, base images hard-linked from `/var/tmp/cp-v3n28/images` and verified ([build/work-root-setup.log](build/work-root-setup.log)); one SSH key; manifest `dcf6e09c…d1229`.
- Per cell as in batch 6a: `prepare` → early dry run → `start` → `wait-ssh` → `install` → `enroll-recovery-runtime` (standalone) → `prepare-pdns-switch` / `prepare-bind` (paired cells with `--peer-engine` and `--peer-catalog-format`) → read-only snapshots → dry run → `run-prepared … --execute` → collect → `stop`/`teardown` (failed cell: `stop` only). `run-prepared` passed `--peer-catalog-member-label` to the controller itself for the PowerDNS-secondary cells (from `peer-before-kill.json`). Scripts: [build/fixture-tools/](build/fixture-tools/), launch lines `c1.sh` … `c7.sh`, `c4b.sh`.
- **Versions** (`versions-*.txt`, `result.json` `native_versions`): Debian kill guest kernel `6.12.105+deb13-cloud-amd64`, systemd `257.13-1~deb13u1`, `pdns-server`/`pdns-backend-sqlite3` `4.9.17-0+deb13u1` and `bind9` `1:9.20.29-1~deb13u1` (installed inside the measured operation). Arch peer: `powerdns 5.1.4`, `bind 9.20.29-1`. In `c5` the Arch kill guest (base kernel `7.1.8-arch1-3`) got `bind 9.20.29-1`, kernel `7.2.7-arch1-1` and systemd `262-1` from the measured `pacman` transaction and rebooted into 7.2.7; the Debian peer ran PowerDNS 4.9.17 as primary.
- **No harness workaround was applied** (no `build/harness-workarounds.diff`), no pass rule was changed, no cell was re-run.

### Evidence layout

As in batch 6a (`raw/results/` flattened, `raw/watch/` sampler per boot, `raw/journald/`, `native-and-outage.json`, `cell-digest.txt`, `boundary-window.txt`, `owner-post-state.txt`, `service-mutation-status-post-collect.json`, `versions-*.txt`, paired-cell peer files), plus, for the PowerDNS-secondary cells:

- `pdns-db-post-collect.txt`: every table of the live `/var/lib/powerdns/pdns.sqlite3` after collection, read with `mode=ro` (`build/fixture-tools/pdnsdb.py`), including `domains` with `options`, record counts per domain and type, `domainmetadata`, `comments`, `cryptokeys`, `tsigkeys`, `supermasters` and the product's receipt tables. In `c1` this file shows `sqlite error: no such column: rowid` (the dumper ordered by `rowid`, which a `WITHOUT ROWID` receipt table lacks); the corrected dump taken on the still-running `c1` guest is `pdns-db-post-collect-redump.txt`.
- `pdns-db-timeline.txt`: the same dump of every database copy the sampler made (`raw/watch/*/copies/<HHMMSS.mmm>-<hash>.sqlite3` whenever the file's bytes changed, and `pdns-at-<snapshot>.sqlite3` at the start, kill-proof and result snapshots). From `c2` on the sampler also copies the `-wal` file and hashes main+WAL; `c1`'s copies are main file only, and because PowerDNS runs SQLite in WAL mode they show only the staged candidate's rows (one CONSUMER row, no records), **not** the daemon's committed writes. The copies were renamed after export to keep paths short (`build/renamed-db-copies.txt`).
- `pdns-secondary-options.json`: every `pdns_secondary_rows` / `member_options` object in `result.json`, verbatim.
- `catalog-format-lines-transcript.txt` (tagged Agent) and `catalog-format-lines-agent-journal.txt` (restarted Agent) with the Agent's catalog-format line.
- `agent-journal-hold-rollback-lines.txt`: Agent unit-journal lines matching hold/rollback wording (empty or near-empty: the startup rollbacks here log nothing).
- `c6` only: `reinstall-diagnostics-post-collect.txt`, `dpkg-statoverride-readonly.txt`, `extra-diagnostics-post-collect.txt` (all read-only).

## Cell 1 `c1-pdnssec-bindpri` — PASSED (peer passed)

- Request `5bd1de7092240beea2027afc2a707c7d`. Controller 16:12:01.74Z–16:13:50.58Z.
- **Cut.** Journal `target-started` (`6d94e225…`), marker 16:12:20.529741Z, SIGKILL 16:12:20.552997Z (PID 1781, `T`, 137), 23.3 ms, nothing inside.
- **The restarted Agent rolled back a database the daemon had written into.** Sampler: journal `rolling-back` 16:12:21.03, `pdns.sqlite3` absent 16:12:21.04, journal retired 16:12:21.81. PowerDNS had started and `retrievePDNSPairSecondaryZones` had returned before `target-started` was written (`cmd/agent/dns_engine_pdns_switch.go:2119-2131`), so the removed database held the consumer's transfers; the new "staged candidate plus consumer transfers" rule accepted it. The exact pre-removal contents are not retained for this cell (main-file copies only, see Evidence layout); `c2` retains them. The Agent unit journal has no line about the rollback.
- Retry 1 ran the request forward (exit 0, 7.11 s; job `succeeded`, attempt 2), retry 2 idempotent; probes `target_converged` (`aa38bf05…`); 31/31. Catalog-format lines (tagged and restarted Agent) all name BIND: `… the paired primary at 192.0.2.11 serves catalog catalog-c000020b.celikpanel.invalid in the BIND catalog format; this operation reads it in that format`.
- Accepted `options` (after recovery and after the reboot): `{"consumer": {"unique": "b076e9241974292fffe8ecc0209b7ace316d1d36063423d9ccb0849a."}}`, expected label equal.
- Reboot (management disabled and proven), 19.5 s: PowerDNS alone on port 53, same answer counts, rows hold, 31/31 DNS-only, passed.
- **Peer verdict passed**, `combined_exit` 0: BIND producer, format name `BIND`, serial 1, member `s1-kill.test`, catalog and member transferred to `192.0.2.10`.
- **Compared with batch 5** `c3` (`65b86621`): unverified, no kill; the Agent refused the consumed member's non-empty `options`, could not roll back (`PowerDNS rollback live database is not the staged target`), poisoned its ledger and left DNS down. Here the same cell converges.

## Cell 2 `c2-pdnssec-pdnsnative` — PASSED (peer passed)

- Same request ID as `c1` (work root `r2`). Controller 16:19:19.36Z–16:21:21.20Z.
- **Cut.** Journal `target-started` (`6d94e225…`), marker 16:19:47.833865Z, SIGKILL 16:19:47.856401Z (PID 1797, `T`, 137), 22.5 ms.
- **Database before the cut** (copy `161947.751-3e43715b61e2.sqlite3` + 86 552-byte WAL, taken 76 ms before the marker): CONSUMER row, member `SLAVE` with `options` `{"consumer": {"unique": "tlhnkturltuku63bhersj40lbi0vkdvt."}}`, 11 records (catalog 5, member 6), and `domainmetadata`, `comments`, `cryptokeys`, `tsigkeys`, `supermasters` all 0 rows.
- **Rollback of that database:** journal `rolling-back` 16:19:48.08, database absent 16:19:48.69, retired 16:19:49.00; retry 1 forward (exit 0, 9.74 s, attempt 2), retry 2 idempotent; `target_converged` (`aa38bf05…`); 31/31. Catalog-format lines all name PowerDNS.
- Reboot (management disabled), 18.8 s: passed, 31/31 DNS-only; rows and `options` held.
- **Peer verdict passed**, `combined_exit` 0: PowerDNS 5.1.4 `PRODUCER` catalog, producer `powerdns`, format name `PowerDNS`, serial `1790698723`, member label `tlhnkturltuku63bhersj40lbi0vkdvt`.
- **Compared with batch 5** `c4`: unverified with the same `options` refusal; that run used the BIND-format catalog on the PowerDNS peer. No earlier run with the native producer.

## Cell 3 `c3-pdnssec-prestart` — PASSED (peer passed)

- Request `10e89db02af20a520fae4c3d996b9376`. Controller 16:25:24.25Z–16:26:23.07Z.
- **Cut.** Journal `target-staged` (`c1e16157…`), marker 16:25:41.204457Z, SIGKILL 16:25:41.225802Z (PID 1785, `T`, 137), 21.3 ms. No live database at the cut (the candidate is activated after `source-stopped`; the first live copy is from the retry, 16:25:45.74).
- Startup rollback (journal `rolling-back` 16:25:41.41, retired 16:25:42.27); retry 1 forward (9.95 s, attempt 2), retry 2 idempotent; `target_converged` (`7a0a0f33…`); 31/31; BIND-format lines. `options` as `c1`. `mutation_hold` absent from `Agent.ServiceMutationStatus` after recovery (accepting mutations).
- **Peer verdict passed**, `combined_exit` 0. No earlier run of this cell.

## Cell 4 `c4-pdnssec-late` — PASSED (peer passed; went forward, no rollback)

**Choice of cell.** The README table admits `pdns-switch__rolling-back__after-write__paired-secondary__peer-reachable`, but for every `pdns-switch` rollback cell the tagged precursor is an injected error at `target-staged` after-write (`cmd/agent/dns_engine_kill_matrix_linux.go:424-431`), and PowerDNS is started only after `source-stopped` (`cmd/agent/dns_engine_pdns_switch.go:2097`, `2119`). That cell therefore can never cut after the daemon consumed the catalog. I ran the named alternative `target-verified__before-write` as `c4` and the literal cell as the extra `c4b`.

- Request `ff8a6f95e86a444b474385bd5018d3ab`. Controller 16:30:37.02Z–16:31:32.12Z.
- **Cut.** On-disk journal `target-started` (`7828412e…`), marker at the `target-verified` before-write hook 16:30:59.308571Z, SIGKILL 16:30:59.340089Z (PID 1776, `T`, 137), 31.5 ms.
- **The restarted Agent went forward**, not back: journal `committed` 16:31:00.43, retired 16:31:01.30; job `succeeded` at **attempt 1**; both retries idempotent (0.004 s, 0.008 s); `target_converged` (`4ab61ac8…`); 31/31. Its catalog line is the recovery form: `DNS engine change recovery ff8a6f95e86a444b474385bd5018d3ab: the paired primary at 192.0.2.11 serves catalog … in the PowerDNS catalog format; …`. So this cell does **not** exercise the rollback of a daemon-written database; `c1` and `c2` do.
- **Tables before the cut and after recovery** (`pdns-db-timeline.txt` copy `163055.111-00372edf1731` with its WAL, which is byte-equal to the kill-proof and result copies; `pdns-db-post-collect.txt`): identical content. `domains`: CONSUMER + member `SLAVE` with `options` `{"consumer": {"unique": "tlhnkturltuku63bhersj40lbi0vkdvt."}}`; `records`: catalog 5 (SOA, NS, TXT, PTR, empty-type ENT), `s1-kill.test` 6 (SOA, NS ×2, A ×3); `domainmetadata` 0, `comments` 0, `cryptokeys` 0, `tsigkeys` 0, `supermasters` 0; product receipt table 1 row. **PowerDNS wrote no metadata for the consumed member.**
- `mutation_hold` absent after recovery. **Peer verdict passed**, `combined_exit` 0, serial `1790699402`. No earlier run.

## Cell 4b `c4b-pdnssec-rollingback` — PASSED (peer passed; extra)

- Request `628b787748c9ba1e6968093dd2e9e358`. Controller 16:52:02.25Z–16:53:22.55Z.
- **Cut.** Journal `rolling-back` (`be8b2996…`) after the injected `target-staged` precursor, marker 16:52:42.812867Z, SIGKILL 16:52:42.827237Z (PID 1777, `T`, 137), 14.4 ms. **No live database existed at the cut** (first appearance 16:52:46.29, during the retry), as the code reading predicted.
- Startup rollback finished (retired 16:52:43.80); retry 1 forward (8.10 s, attempt 2), retry 2 idempotent; `target_converged` (`c3a7b270…`); 31/31; PowerDNS-format lines; `mutation_hold` absent after recovery. **Peer verdict passed**, `combined_exit` 0.

## Cell 5 `c5-bindsec-arch` — PASSED (peer passed)

- Arch kill guest, Debian 13 peer running PowerDNS 4.9.17 as native primary with its own `PRODUCER` catalog (`catalog-c000020a.celikpanel.invalid`, the peer at `192.0.2.10`). Request `d2f345432a323fbd9f5c5354f491a6cb`. Controller 16:34:05.34Z–16:37:32.43Z.
- **Cut.** On-disk journal `intent` (`7927dd72…`), marker at the `target-staged` before-write hook 16:34:43.639609Z, SIGKILL 16:34:43.662617Z (PID 1620, `T`, 137), 23.0 ms.
- Recovery: job `succeeded` at attempt 2 (retry 1 exit 0, 11.22 s; retry 2 idempotent); `target_converged` (`34e616a5…`); 31/31. Catalog-format lines (three) name PowerDNS: `… the paired primary at 192.0.2.10 serves catalog catalog-c000020a.celikpanel.invalid in the PowerDNS catalog format; …`.
- Reboot (management disabled), **92.2 s**, into kernel 7.2.7 installed by the measured `pacman` transaction: BIND alone, same answer counts, 31/31 DNS-only, passed.
- **Sampler gap** (as batch 6a `c02`): the boot-1 sampler stopped at 16:34:19 under the measured Arch upgrade, before the cut; no at-kill-proof snapshot or journal-phase samples for boot 1. Controller evidence and the boot-2 sampler are unaffected.
- **Peer verdict passed**, `combined_exit` 0: producer `powerdns`, serial `1790699611`, label `tlhnkturltuku63bhersj40lbi0vkdvt`. First run of a Debian PowerDNS primary with an Arch secondary; no earlier run of this cell.

## Cell 6 `c6-reinstall` — UNVERIFIED (product finding; stopped, overlays kept)

- **Preparation completed**, including the step that stopped batch 5 `c6`: the controller read the v2 state receipt (`source-proof-proven`, `engine_state_receipt_sha256` `4992553d…`). Setup: an untagged production fresh BIND switch (request `67beea28f7bb2d005105c97169966302`, job `succeeded`), then `systemctl disable --now named.service` and `apt-get purge -y bind9` (`dpkg: warning: while removing bind9, directory '/var/cache/bind' not empty so not removed`).
- **Measured request** `b66cbdc5c51b25639364f76a7113a214` (mode `reinstall`). Controller 16:41:46.17Z–16:41:49.87Z. The tagged Agent logged (transcript, verbatim, first lines):
  > `DNS engine switch to bind at epoch 1 failed: install BIND in no-start mode: exit status 100: Reading package lists...`
  > `dpkg: unrecoverable fatal error, aborting:`
  > ` unknown system group 'bind' in statoverride file; the system group got removed`
  > `before the override, which is most probably a packaging bug, to recover you`
  > `can remove the override manually with dpkg-statoverride`
  > `E: Sub-process /usr/bin/dpkg returned an error code (2)`

  The trigger: `agent rejected DNS switch: DNS engine switch did not complete; inspect the agent log`, exit 1. The controller recorded `scenario trigger exited before boundary notification with exit code 1 (raw 1)` and killed the tagged Agent as cleanup; `status: unverified`, `run-prepared` exit 2.
- **State left** (`reinstall-diagnostics-post-collect.txt`, read-only): `/var/lib/dpkg/statoverride` still holds `root bind 1775 /var/cache/bind`; `getent group bind` and `getent passwd bind` return nothing; `/var/cache/bind` is mode 1775 `root:104` (GID 104 no longer named); `bind9` not installed, `bind9-utils`/`-libs`/`-host` installed; `dpkg --audit` clean. **No switch journal** (the install runs before `intent`, `cmd/agent/dns_engine_host.go:1513-1523`). The install-ownership receipt for this request stays (`missing_before: ["bind9"]`), the state and ownership receipts of the setup switch are unchanged. Both BIND units `not-found`, nothing on port 53 except `systemd-resolved`.
- **Panel-visible job text and status.** Ledger job `failed`/`failed`, attempt 1, `error_code` `dns_kill_matrix_trigger_failed`, `error_message` `The DNS kill-matrix trigger ended before a verified target receipt.` — this text is written by the **test trigger** (`cmd/dns-kill-matrix-trigger/main.go:45-46`), not by the product, so it is not what a Panel would show in production. `recovery dns-switch-status --quiesced` (enrolled launcher, exit 0): `No DNS switch journal was observed for request b66cbdc5c51b25639364f76a7113a214. Its exact ledger job records status failed. This is a point-in-time ledger observation, not proof of DNS rollback, completion or current service health. Inspect the native DNS service and this same operation before any new switch.` It does not mention the dpkg override.
- **DNS-only hold:** not reachable here (no journal was written, and the production Agent was stopped by the fixture, so `Agent.ServiceMutationStatus` could not be read: `connect: cannot reach agent socket`). Whether unrelated mutations stay possible was **not** observed.
- **Reading (product finding; no fix made).** The managed BIND setup adds a durable `dpkg-statoverride --add root bind 1775 /var/cache/bind` (`cmd/agent/dns_engine_bind_root_linux.go:186-191`, verify-or-create at 210-259). The owner's `apt-get purge bind9` removes the `bind` user and group but not the override, which references a group only that package provides. From then on dpkg refuses to unpack, so the reinstall cannot even install its package. By dpkg's own message the same would abort any later package installation on that host until the owner runs `dpkg-statoverride --remove /var/cache/bind`; I did not test an unrelated package. This touches owner independence (D-022): leaving CelikPanel's BIND behind the way an owner would breaks the host's package manager. It is not a setup problem the rules let me work around (the setup is exactly the owner action this row describes), so the cell was stopped as found.
- **Not answered:** the README's prediction that the restarted Agent's reinstall rollback cannot verify its restored source; no kill, no recovery, no reboot happened.
- **Compared with batch 5** `c6` (`65b86621`): stopped earlier, in the controller (`KeyError: 'manifest_qualifier'`, harness defect, fixed by `2ac6dcbe`). This run passes that point and fails in the product's package step.
- Guests stopped, overlays kept: `/var/tmp/cp-b6b-0930/cells/e3224bede2eda2ee2dff1060`.

## Cell 7 `c7-takeover` — PASSED

- Request `b66cbdc5c51b25639364f76a7113a214` (derived from the cell ID; work root `r2`). Controller 16:47:51.71Z–16:48:42.20Z. Stock cell: the harness has no option to add owner `recursion`/`allow-*` directives to the stopped BIND (README "Rows 12 and 14": "This cell does not add such directives"), so the retry-after-rollback directive refusal is **not exercised**.
- **Cut.** Journal `target-staged` (`8d2ef877…`), marker 16:47:56.655980Z, SIGKILL 16:47:56.673824Z (PID 2412, `T`, 137), 17.8 ms. At the cut the install receipt names `bind9`, this request, `adopted_present: true`, `missing_before: []` (`f2d3f83b…`).
- **Status before recovery** exit **3**, no evidence change (recorded, not judged): `DNS switch request b66cbdc5c51b25639364f76a7113a214: accepted-active (journal phase target-staged).` / `No owner recovery command applies to this journal's recorded shape and ledger status. Keep the journal and ledger. If this operation does not resume through CelikPanel or an Agent restart, contact support with request id b66cbdc5c51b25639364f76a7113a214. This status check does not start recovery.` (Batch 6a `c10` exited 3 with an identity-parse message instead.)
- Recovery: retry 1 forward (12.59 s), job `succeeded` at attempt 2, retry 2 idempotent; `target_converged` (`bac6f937…`, same fingerprint as batch 5 `c5` and batch 6a `c10`); BIND alone; 31/31. Status after recovery exit 0 (`… Its exact ledger job records status succeeded. …`). Owner files `named.conf`, `named.conf.root-hints`, `rndc.key`, `/etc/default/named` byte-identical (judged); `named.conf.options`, `named.conf.local` rewritten (recorded). `mutation_hold` absent after recovery.
- **Compared with earlier runs:** batch 5 `c5` passed (no reboot); batch 6a `c10` passed with a reboot. No reboot was requested here.

## What remains on the host

- `/var/tmp/cp-b6b-0930` (2.1 GB apparent): both work roots (`r2` nested), logs, collected evidence and the **stopped overlays of `c6`** (`cells/e3224bede2eda2ee2dff1060`). All other cells were torn down. No QEMU process runs.
- `/root/cp-b6b-src`, `/root/cp-b6b-artifacts`, `/root/cp-b6b-tools`, `/var/tmp/cp-b6b-build.log`, `/var/tmp/cp-b6b-setup.log`, `/var/tmp/cp-b6b-webdist.sha256`. Nothing outside `cp-b6b*` was created or deleted.

## Deviations

- **Cell 4** is `target-verified__before-write`, not the admitted `rolling-back__after-write`, for the reason in Cell 4; the literal cell was added as `c4b`. Eight cells instead of seven.
- `c7` ran without reboot flags (none were specified for it).
- Fixture-tool changes against batch 6a (read-only, in [build/fixture-tools/](build/fixture-tools/)): the sampler copies the PowerDNS database (and from `c2` on its WAL) whenever its bytes change; new `pdnsdb.py`, `pdnsopts.py`; `collect.sh` captures the tables, options record and catalog lines; `secstate_remote.sh` lists `/var/named` on Arch; `peerfacts_remote.sh` reads `dpkg-query` on a Debian peer; `boundcheck.py` tolerates a result without a kill. The dumper bug (`rowid`) and the missing WAL copies affected `c1` only (above).
- `c6`: extra read-only diagnostics after collection (`reinstall-diagnostics-post-collect.txt`, `dpkg-statoverride-readonly.txt`, `extra-diagnostics-post-collect.txt`).
- `c7` teardown: `fixture.py stop` returned 2 (`fixture: invalid QEMU pidfile: …/r2/cells/e3224bede2eda2ee2dff1060/arch/qemu.pid`) for the idle Arch node of this standalone cell; `teardown` then returned 0 and removed the cell directory, and no QEMU process remained. Evidence had been collected before.
- `service-mutation-status-post-collect.json` could not reach the Agent after the management-disabled reboots (`c1`, `c2`, `c5`) or in `c6` (Agent stopped by the fixture); `smstatus.stderr` in each.
- Package versions are live-mirror versions; the Arch peer preparation runs a full `pacman -Syu`.
- No re-run, no harness workaround, no pass-rule change.

## What this does not prove

Seven passes and one unverified cell, one run each, on one host, in disposable guests, against one PowerDNS consumer version (4.9.17) and two producers. The rollback of a database the daemon had already written into was exercised twice (`c1`, `c2`) and accepted; that shows the new rule accepts this native shape with no metadata, not that PowerDNS never writes metadata (DNSSEC, other versions, longer runtimes, or a primary publishing catalog properties were not tried). `c4` went forward, so the late cut did not test a rollback. The consumer's handling of the disabled PTR/TXT rows is read from the database, not queried. The boundary held in seven cuts; that is evidence about these runs, not a proof for every boundary. The reinstall path (row 14) has still never reached its cut: its recovery, its predicted rollback-verification problem, its Panel text, `dns-switch-status` in that state and the DNS-only hold remain unobserved; the dpkg-override finding is from code reading plus one native run and was not reproduced with an unrelated package. The takeover's owner-directive refusal was not exercised. All reboots were orderly; no power loss, no peer-unreachable cell, no CelikPanel-to-CelikPanel pair. Commits made during the run were not tested. Nothing here changes the 268-runnable denominator, closes P0.4 or P0.5, opens the fresh paired PowerDNS primary gate, or passes any acceptance-register row.

Retained files are listed in [SHA256SUMS](SHA256SUMS), which covers every file except this README and itself.
