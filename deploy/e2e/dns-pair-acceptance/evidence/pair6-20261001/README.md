# DNS pair product-flow acceptance, sixth native run (pair6), 2026-09-30

Exploratory. Sixth native run of the product-flow DNS pair acceptance driver with
`--license-mode acceptance-fixture` on two disposable QEMU guests (fixture `debian13`
192.0.2.10, `arch` 192.0.2.11) on the local WSL `archlinux` host. No installed server,
remote host, release or signed bundle was touched; nothing contacted celikpanel.net or any
license service; nothing was pushed, published or signed. Nothing here passes an
acceptance-register row; every `result.json` keeps `native_evidence: false`. The results below
are observations.

- **Product:** commit `e50fe50a07746ee6f9cd7dea80b95ccd8999256c` (tree
  `378895497061e0ebadc2712ef27f5b9d8fbef7f8`), branch `accept/pdns-primary-gate-open-9` = main line
  `37864789` ("BIND primary propagation plan carries its source state; internal proof failures get
  their own code") plus "Open the fresh paired PowerDNS primary gate (acceptance candidate)". Built
  only from `git archive e50fe50a`. First native measurement of `37864789`.
- **Driver:** `git archive 378647894a060e687aff6e0b2dfbfd280ec03924 deploy/e2e/dns-pair-acceptance
  deploy/e2e/dns-kill-matrix` (tree `96876051764d5864135103f2045358352d0c36e2`; its only driver change
  since pair5 is `guidance.py`/`test_guidance.py`: `dns_peer_proof_internal` reviewed), extracted to
  `/root/cp-pair6/driver`. **No driver file was patched; no pass or redaction rule was changed.**
  There is no `harness-workarounds.diff`.
- **Repository:** branch `feat/dns-artifact-separation`, `HEAD` `67ac84fc` at start and `dbb4105d`
  at the end ("Register: record batch 11 ..."), with uncommitted `cmd/agent/*` modifications in the
  working tree that appeared during the run. Neither was made or used by this run (product and
  driver came from `git archive`); the only write to the repository is this folder.

## Result: all three topologies stopped in zone delete on one product defect

| Dir | Topology (primary / secondary) | `overall` | Last step reached | Stopped by (classification) |
|---|---|---|---|---|
| `t1-bind-bind/p6t1-20260930t055310z` | BIND debian13 / BIND arch | **failed** | `zone-delete` (failed, 332.3 s) | P6-1 (PRODUCT): after owner enrollment the retry stays pending `dns_peer_journal_unknown`; the inspection and the post-inspection DNS checks completed |
| `t2-bind-pdns/p6t2-20260930t060837z` | BIND arch / PowerDNS debian13 | **failed** | `zone-delete` (failed, 328.7 s) | P6-1 (PRODUCT), same code after `dns-peer-enroll --engine pdns` |
| `t3-pdns-bind/p6t3-20260930t062051z` | PowerDNS debian13 / BIND arch | **failed** | `zone-delete` (failed, 329.3 s) | P6-1 (PRODUCT), same code; **regression against pair5 t3**, which completed |

`overall_cause` is `unclassified` everywhere (`zone-delete` carries no `cause`; the driver sets
`cause: product` only for its open-ended-unknown rule). The classification is this run's. Per the
run rules no topology was re-run and nothing on a guest was changed to get past a stop; each cell
was stopped with its state (overlays kept). Run order t1, t2, t3.

**P5-1 is gone.** On t1/t2 the Agent now passes the post-inspection probe selection: after the SSH
inspection it runs the pair-readiness catalog AXFRs and the member no-transfer probe (named/pdns
journal lines, see below), which pair5 never reached. No journal line "DNS peer proof could not
run", no `dns_peer_proof_internal` and no `dns_peer_owner_edit_unknown` on any Agent (all boots,
both nodes, all three topologies; ledger files likewise: `t*/obs/*-agent-proof-code-check.txt`).

**Batch-11 re-stamp not involved.** t3's retry came 4 s after the first 202; the catalog serial
the challenge bound (1790749547, observed from 06:26:00) did not change until the daemon re-stamp
at 06:26:46 (1790749606), 27 s after the pending result (`t3-pdns-bind/obs/catalog-watch.jsonl`,
primary PowerDNS rows logged with each change). The code is not `dns_peer_owner_edit_unknown`.

## P6-1 (t1, t2, t3): a positive authenticated proof is refused at consume-once as `dns_peer_journal_unknown`

Classification: **PRODUCT**. Every topology, both primary engines, both secondary engines.

Sequence (t1; t2 and t3 identical in shape, times in the table):

1. `DELETE /api/v1/domains/1` → **202** `dns_peer_enrollment_required`, stage `dns_cleanup`
   (`steps/15-zone-delete/api/0062`), saved status the same.
2. Owner enrollment exactly as the text says, every command exit 0
   (`steps/15-zone-delete/owner-enrollment-transcript.json`; same commands as pair5, t2 with
   `--engine pdns` on every command and `--catalog-account celikpanel-peer-catalog-v1` read
   read-only from the secondary's CONSUMER row). Host-key digest agreed three ways (CLI,
   `/etc/ssh/ssh_host_ed25519_key.pub`, fixture known-hosts): t1 `6c57f611…b940`, t2 `a009a036…3c82`,
   t3 `ad9fb24d…e08b`.
3. The same `DELETE` once more → **202** `{"reason": "dns_peer_journal_unknown", "stage":
   "dns_cleanup", "status": "deletion_pending"}` (`0064`); the saved status kept that reason for the
   full 300 s of read-only polling (31 identical reads, `0065`-`0096`). Nothing retried by itself.

| | t1 BIND deb / BIND arch | t2 BIND arch / PowerDNS deb | t3 PowerDNS deb / BIND arch | pair5 t3 (passed) |
|---|---|---|---|---|
| first 202 (`dns_peer_enrollment_required`) | 05:57:14 | 06:12:47 | 06:25:58 | 04:33:34 |
| retry sent (Panel "mail runtime cleanup skipped") | 05:57:19-20 | 06:12:50-51 | 06:26:02-03 | 04:33:38 |
| pre-inspection catalog AXFRs | 05:57:20-22 | 06:12:51-53 | 06:26:03-04 | |
| challenge `issued_at` / journal file written | 05:57:24 / 05:57:27.99 | 06:12:55 / 06:12:59.11 | 06:26:06 / 06:26:09.55 | |
| SSH inspection on the secondary (`celikpeer`, sudo inspector) | 05:57:31-32 | 06:13:02-03 | 06:26:12 | 04:33:45 |
| post-inspection catalog AXFR + member NOTAUTH from the primary | 05:57:33-34 | 06:13:04 | 06:26:14 | |
| Agent pending code, ledger write | 05:57:37.2 | 06:13:08.78 | 06:26:19.7 | (200 `deleted`) |
| retry request `elapsed_seconds` | **18.075** | **17.529** | **17.055** | 13.832 |
| challenge journal at the end | `dns-peer-challenge-v1.json` `outstanding`, attempt 1, ledger attempt 3, mtime = publish time | `pdns-peer-challenge-v1.json` same | `dns-peer-challenge-v1.json` same | retired |
| ledger job | attempt 3 `pending`, `error_code dns_peer_journal_unknown` | same | same | `succeeded` |
| stage/retired files in `/var/lib/celikpanel-agent-private` | none | none | none | |

Sources: `t*/obs/*-ledger-dns-jobs.txt`, `*-agent-journal-zone-delete.txt`,
`*-panel-journal-zone-delete.txt`, `*-inspector-journal-zone-delete.txt`,
`*-named-journal-zone-delete.txt` / `debian13-pdns-journal-zone-delete.txt`,
`*-agent-private-dir.txt` (names and metadata only).

**Where the code comes from (source reading of `e50fe50a`, matched to the timeline).** In both
verifiers `dns_peer_journal_unknown` is returned at five places. Four precede the SSH inspection
(`cmd/agent/dns_engine_peer_native_linux.go:80,86,90,98`; `dns_engine_peer_pdns_linux.go:166,172,176,183`)
and cannot have produced it: one wave inspects at most once (`dns_engine_pdns_propagation.go:341-361`)
and the inspection ran. The fifth is the consume-once callback
(`dns_engine_peer_native_linux.go:128-137`, `dns_engine_peer_pdns_linux.go:211-219`):
`dnspeerproof.Verify` calls `consume` only after the response passed validation, the context
match and `catalog_state == transferred && member_state == absent && native_state == unloaded`
(`internal/dnspeerproof/proof.go:292-306`; `pdnspeerproof.Verify` delegates to it). Had any of those
failed, the code would be `dns_peer_native_unknown` (line 139/221). So `ConsumeOnce` returned an
error. The journal file is unchanged since publish and no stage file is left, so it failed before
the `RENAME_EXCHANGE` (`internal/dnspeerjournal/store_linux.go:131-167, 380-457`).

**Inferred cause: the proof wave's 15 s deadline expires inside consume-once.** The whole
native-proof wave runs under `proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)`
(`cmd/agent/dns_engine_pdns_propagation.go:336`; `dnsPairProofLimit = 15 * time.Second`,
`cmd/agent/dns_engine_zone_verify.go:35`). `ConsumeOnce` calls the caller's `verifyCurrent` three
times (`store_linux.go:141, 385, 420`), and `verifyCurrent` re-runs the local evidence recheck
(`recheckBINDPeerLocalEvidence` / `recheckPDNSPeerLocalEvidence`), which runs `systemctl`, `ss` and
named/pdns inspection through `exec.CommandContext(proofCtx)` (`cmd/agent/mutation_command.go:27-39`,
`dns_engine_host.go:2758, 2901-2930`). Once the deadline has passed those commands fail at once,
`consume` returns `StateError{Unknown}` and the verifier maps it to `dns_peer_journal_unknown`. The
last `verifyCurrent` before `Verify` (line 125/208) still passed (otherwise the code would be
`dns_peer_owner_edit_unknown`), so the deadline fell between it and consume. Timing agrees in all
three topologies: the wave starts at the pre-inspection AXFRs and the pending code follows
15.8-17 s later; the publish alone took 3.5-4 s (four `verifyCurrent` calls), the SSH connect about
3 s. pair5 t3 completed the same PowerDNS-primary path in 13.8 s; `37864789` adds no measurable
work on that path (probe selection moved earlier, error types changed), so the t3 regression is
the same margin being crossed (inspection 9 s after the retry here versus 7 s in pair5), not a
logic change. **This is inference from source and timing; the Agent logs only the code**, not the
consume error. A retry by the owner was not tried (run rule); if the wave's duration is stable, it
would meet the same deadline.

Native state while pending and after the run (driver probe, `steps/15-zone-delete/native-pending-*.json`,
`obs/*-native-after-run.json`, `obs/catalog-after-run.json`): the zone was gone natively on both
servers in all three topologies (BIND "no matching zone"/no zone file; PowerDNS no zone row, only
the CONSUMER/PRODUCER catalog row); the catalogs list no member (t1/t2 serial 3, t3 1790749606).
The product's proof did not complete. The driver's authoritative-absence check, re-add,
management-disabled reboot and management return were not reached anywhere.

D-024 observation (the driver rates the reviewed code actionable; not a driver finding): after a
correct enrollment and a positive inspection, every owner is told "the private peer challenge
cannot be reconciled. This server's administrator must review the retained challenge and exact
operation record before retrying the same publication." Nothing was changed by anyone; the text
names no concrete check the owner can perform, the Agent log gives no reason, and a retry would
probably meet the same deadline.

## `pdns-peer-inspect` outcome fields (t2) and the daemon's group

- **Fields not observed directly** (the response is neither logged nor stored). By the code path
  above, the Agent reached `consume` only because `pdnspeerproof.Verify` accepted
  `catalog_state = transferred`, `member_state = absent`, `native_state = unloaded`
  (`proof.go:300-303`). That is an inference, not a record of the fields. The inspector ran at
  06:13:02-03 (`sudo … COMMAND=/usr/local/libexec/celikpanel-pdns-peer-inspect`,
  `t2-bind-pdns/obs/debian13-inspector-journal-zone-delete.txt`).
- **Daemon identity** (`t2-bind-pdns/obs/debian13-pdns-daemon-group.txt`): `pdns_server` PID 5076,
  `Uid 103`, `Gid 104`, `Groups: 104` = group `pdns`; `/etc/powerdns/pdns.conf` `root:pdns 0640`,
  SHA-256 `8b46927e…f262a` (same as pair5); `pdns.d` holds `celikpanel.conf`
  (`launch=gsqlite3`, `gsqlite3-dnssec=yes`, `gsqlite3-database=/var/lib/powerdns/pdns.sqlite3`,
  `local-address=10.0.2.15,192.0.2.10`, `zone-cache-refresh-interval=0`, `webserver=no`, `api=no`)
  and `celikpanel-cluster.conf` (`primary=yes`, `secondary=yes`, `allow-axfr-ips=192.0.2.11`), both
  `root:root 0644`.

## Configuration and receipt observations (read-only, after each run)

- **Arch BIND secondary catalog stanza** (t1 and t3, `obs/arch-bind-catalog-stanzas.txt`,
  `obs/arch-named-effective-options.txt`), identical to pair5: `zone
  "catalog-c000020a.celikpanel.invalid" { type secondary; primaries { 192.0.2.10; }; allow-transfer
  { 192.0.2.10/32; 127.0.0.1/32; ::1/128; }; };`; options scope `allow-transfer { 192.0.2.10/32; }`
  and `catalog-zones { zone "catalog-c000020a.celikpanel.invalid" default-primaries { 192.0.2.10; }
  in-memory yes; };`. Members have no stanza (catalog, in-memory).
- **BIND primaries' catalog stanza** (t1 debian13, t2 arch): `type master`, `allow-transfer
  { <self>/32; <peer>/32; }`, `also-notify { <peer>; }`, `notify yes`.
- **Generation receipts:** Arch secondary (t1, t3) `celikpanel-bind-generation-receipt/v2`,
  `pairing.role secondary`, `pairing.secondary_config_version = 2` (current generation
  `68b2d37e…`). BIND primaries (t1 debian13, t2 arch): role `primary`, no such key.
- **Loopback transfers and denials:** the inspector's loopback catalog AXFRs appear on the Arch
  secondary in t1 (05:57:32) and t3 (06:26:12), two each, ended normally; **no "denied" line** in
  any named journal of any topology.
- **Mail stage:** every deletion attempt logged "mail runtime cleanup skipped: no mail runtime for
  this domain on this server" (t1 05:57:08/05:57:20, t2 06:12:42/06:12:51, t3 06:25:52/06:26:03).

## rndc key (metadata only; content never read into any file)

| Topology / node | Key | Owner, mode | Birth | Agent log | Companion `provenance` |
|---|---|---|---|---|---|
| t1 arch (secondary) | `/etc/rndc.key` | `root:named 0640`, no package owns it | 05:56:12.78 | 05:56:12 "… is product_created" | `product_created`, sha256 `c7d811f3…9e51` |
| t2 arch (primary) | `/etc/rndc.key` | `root:named 0640` | 06:11:35.79 | 06:11:35 "… is product_created" | `product_created`, sha256 `34d2159f…a5dc` |
| t3 arch (secondary) | `/etc/rndc.key` | `root:named 0640` | 06:25:01.72 | 06:25:01 "… is product_created" | `product_created`, sha256 `24b51e08…f3d6` |
| t1 debian13 (primary) | `/etc/bind/rndc.key` | `bind:bind 0640`, not a dpkg path | 05:55:43.13 (package configuration) | 05:55:46 "… is owner_or_package_provided (key_present)" | `owner_or_package_provided`, `basis: key_present` |

named first starts: t1 arch 05:56:17, t1 debian13 05:55:54, t2 arch 06:11:40, t3 arch 06:25:05.
Arch `bind 9.20.29-1`, Debian `bind9 1:9.20.29-1~deb13u1`. No `bind_rndc_unavailable`. Files
`t*/obs/<node>-rndc-key.txt`.

## PowerDNS notify (t3 primary)

`t3-pdns-bind/obs/notify-counts.txt`, `debian13-pdns-notify-journal.txt`: 11 "Notification request
to host 192.0.2.11:53 … received from operator", 0 without port, 0 "spurious notify answer";
**1 "Notification for catalog-c000020a.celikpanel.invalid to 192.0.2.11:53 failed after retries"**
at 06:25:20 (a daemon notify while the Arch secondary was still being installed and restarted; the
secondary's named started 06:25:05, pair5 had 0). Arch secondary: 14 "received notify" (9 catalog,
5 member). Fewer than pair5 (13/16) because re-add and reboot did not run.

## Guidance (D-024), verbatim

Setup states as in pair5 (keys `setup.guide.primary` + `setup.guide.startSecondary`,
`setup.guide.component`, `setup.guide.secondary` + `setup.guide.startPrimary`,
`server_setup_access_dns_required` on both roles, `setup.guide.firewall` on the secondary in every
topology, t2's being debian13); full texts in each `result.json`. Peer-start checks only while the
primary's DNS step was `running` (no finding). No `license_required` observation, no open-ended
unknown, no contradictory-peer-start finding. Each setup ended waiting at `access_dns` (findings
`setup-not-complete-offline-*`, expected offline).

Deletion states (EN | TR as the web shows them):

- **All three, first DELETE and saved status, `dns_peer_enrollment_required`** (actor "primary
  administrator together with the secondary owner"): unchanged from pair5, verbatim: "The DNS
  change is saved on this server, but the secondary has not been shown to have removed the zone: no
  inspection access is set up between the two servers. The administrators of both servers set it up
  once with the dns-peer-enroll owner tool (add --engine pdns to each command when the secondary
  runs PowerDNS): primary-prepare here, secondary-install and secondary-host-key on the secondary,
  then primary-activate here. After that, use “Retry this deletion” to retry the same publication;
  it continues from where it stopped. Nothing retries by itself." | "DNS değişikliği bu sunucuda
  kaydedildi, ancak ikincil sunucunun bölgeyi kaldırdığı doğrulanamadı: iki sunucu arasında
  inceleme erişimi kurulmamış. İki sunucunun yöneticileri bunu dns-peer-enroll sahip aracıyla bir
  kez kurar (ikincil sunucu PowerDNS çalıştırıyorsa her komuta --engine pdns ekleyin): burada
  primary-prepare, ikincil sunucuda secondary-install ve secondary-host-key, sonra burada
  primary-activate. Ardından “Bu silme işlemini yeniden dene” ile aynı yayını yeniden deneyin;
  işlem kaldığı yerden sürer. Kendiliğinden yeniden denenmez."
- **All three, DELETE after enrollment and saved status, `dns_peer_journal_unknown`** (actor "stated
  in text"): "The DNS change is saved, but the private peer challenge cannot be reconciled. This
  server's administrator must review the retained challenge and exact operation record before
  retrying the same publication." | "DNS değişikliği kaydedildi; özel eş doğrulama kaydı
  uzlaştırılamıyor. Bu sunucunun yöneticisi saklanan kaydı ve tam işlem kaydını incelemeli;
  ardından aynı yayını yeniden denemeli." Server `message`: "The DNS change is saved, but its
  private peer challenge cannot be reconciled. This server's administrator must review the retained
  challenge and exact operation record; only then retry this same publication."
- No `detail` field in any 202 or saved status; `dns_peer_proof_internal`,
  `dns_peer_owner_edit_unknown`, `dns_peer_catalog_transfer_refused` and
  `dns_peer_inspection_unknown[:token]` did not occur, so their new texts were not shown natively.

## Per-topology step verdicts (from `result.json`; seconds from `started_at`/`finished_at`)

| # | Step | t1 BIND deb / BIND arch | t2 BIND arch / PowerDNS deb | t3 PowerDNS deb / BIND arch |
|---|---|---|---|---|
| 00 | preflight | passed 2.4 | passed 2.5 | passed 2.7 |
| 01 | install-primary | passed 33.1 | passed 132.0 (incl. owner restart, arch) | passed 38.3 |
| 02 | install-secondary | passed 107.6 (incl. owner restart, arch) | passed 33.4 | passed 178.3 (incl. owner restart, arch) |
| 03–04 | login-* | passed 0.7 / 0.4 | passed 0.5 / 0.5 | passed 0.5 / 0.4 |
| 05–06 | license-* | passed 0.4 / 0.4 | passed 0.4 / 0.4 | passed 0.4 / 0.4 |
| 07 | setup-review-primary | passed 1.0 | passed 1.3 | passed 1.0 (no gate blocker) |
| 08 | setup-start-primary | passed 28.0 | passed 16.1 | passed 18.8 |
| 09 | setup-review-secondary | passed 1.3 | passed 1.0 | passed 1.2 |
| 10 | setup-start-secondary | passed 19.1 | passed 18.9 | passed 16.1 |
| 11 | pair-ready | passed 8.8 | passed 11.3 | passed 12.4 |
| 12 | zone-add | passed 13.2 | passed 9.2 | passed 10.6 |
| 13 | record-add | passed 6.9 | passed 5.6 | passed 7.2 |
| 14 | record-edit (`ui-replace`) | passed 13.0 | passed 10.0 | passed 11.0 |
| 15 | zone-delete | **failed** 332.3 | **failed** 328.7 | **failed** 329.3 |
| 16 | zone-readd | not-run | not-run | not-run |
| 17 | independence-reboot | not-run | not-run | not-run |
| 18 | management-return | not-run | not-run | not-run |
| 19 | collect | passed 3.7 | passed 4.1 | passed 7.2 |

`zone-delete` reason verbatim (all three): "deletion stayed pending after owner enrollment and one
retry". Owner steps: the install.sh "RESTART THIS SERVER NOW" reboot of the arch guest (kernel
`7.1.8-arch1-3`, fixture reboot, new boot ID) and the enrollment, in each topology. No topology
completed every step, so no step-verdict summary for register rows is given.

## Builds

- `web/dist`: Windows, `git archive e50fe50a web` into the session scratchpad, `web/node_modules`
  junction to the repository's (lockfile SHA-256 `8dab8b6b…6d26` identical to the commit's), `npm run
  build` (node v24.18.0, npm 11.16.0), 05:49:19–05:49:39Z, **exit 0** ([build/web-build.log](build/web-build.log)).
  106 files; the host copy equals the Windows build after normalising `sha256sum`'s binary marker and
  sort order and equals the `web/dist` in the dist ([build/web-dist-windows-build.sha256](build/web-dist-windows-build.sha256),
  [build/web-dist.sha256](build/web-dist.sha256)).
- Run host: `/root/cp-pair6/repo` = `git clone --no-checkout` of the repository plus that
  `web/dist`; `build-dist.sh --acceptance-license e50fe50a…` with `CELIKPANEL_REPO` pointing at it,
  05:50:31–05:51:26Z ([build/build-dist.log](build/build-dist.log), [build/build-dist-make.log](build/build-dist-make.log),
  [build/dist.json](build/dist.json), [build/go-version.txt](build/go-version.txt): `go1.26.5 linux/amd64`).
  Archive `celikpanel-v0.0.0-pairaccept-acceptance-license.e50fe50a0774-acceptance-license.tar.gz`,
  SHA-256 `5e1ec56cb3c0fd5009fa24670990a00592bf77aa5bc62f659604610f6da005d6`, unsigned local build,
  `release: false`.
- Packaging guard: refused the acceptance tree three ways ([build/guard-refusal.txt](build/guard-refusal.txt))
  and the final `.tar.gz` the same three ways with exit 1 ([build/guard-refusal-archive.txt](build/guard-refusal-archive.txt)).
- Archive observation ([build/archive-evidence-entries.json](build/archive-evidence-entries.json)):
  645 entries, 0 under any `evidence/` directory, 362 under `deploy/e2e/`.
- Product texts: `build-dist.sh` exported the commit's `web/src` with marker `PRODUCT-COMMIT` =
  `e50fe50a…`; `run-topology.sh` passed it as `--product-web-src` ([build/product-web-src.sha256](build/product-web-src.sha256)).
- Driver files as extracted: [build/driver-files.sha256](build/driver-files.sha256).

## Offline tests

**130/130** pass for the run composition (driver archive + `git archive e50fe50a web/src`, the
`web/src` the run passes as `--product-web-src`): [build/offline-tests-run-composition.log](build/offline-tests-run-composition.log).
130/130 also with the driver commit's own `web/src` (`git archive 37864789 web/src`):
[build/offline-tests-driver-commit-pure.log](build/offline-tests-driver-commit-pure.log).

## Exact commands, harness changes, deviations and additions

All host commands were LF script files in the session scratchpad run as
`MSYS_NO_PATHCONV=1 wsl.exe -d archlinux -u root -- bash /mnt/c/…/scratchpad/pair6/<script>.sh`;
copies in [build/scripts/](build/scripts/) (`.txt`; `scan.sh` omitted because it plants
secret-shaped self-test samples; it is pair5's with paths changed). Order: `hostcheck.sh`, Windows
web build, `setup-host.sh`, `cmpweb.sh`, `build.sh` (dist + offline tests), then per topology
`launch.sh <topology> <primary> <label>` (nohup `topo.sh` = `prepare-guests.sh` + `run-topology.sh
… --license-mode acceptance-fixture`), `launch-watch.sh` (catalog watcher), `waitfor.sh`, `res.sh`,
`guid.sh`, read-only collection (`tcoll.sh` → `ro-collect.sh`, `coll-extra.sh`,
`diag-bind-transfer.sh`, `diag-options.sh`, `ledgerjob.sh`, `counts.sh`; plus `journaldiag.sh`),
`stopcell.sh`. Topologies and labels in run order: `bind/bind debian13 p6t1`,
`bind-primary/pdns-secondary auto p6t2`, `pdns-primary/bind-secondary auto p6t3`. Driver flags:
defaults plus `--license-mode acceptance-fixture`.

- **No harness workaround**; no driver defect found; no pass rule changed. The driver rating
  `dns_peer_journal_unknown` actionable is a reviewed-list decision, recorded, not changed.
- **Run paths only** ([build/run-path-changes.diff](build/run-path-changes.diff)): as pair5, with
  `/var/tmp/cp-pair6-dist/` and `/var/tmp/cp-pair6-key/id_ed25519`. Work root
  `/var/tmp/cp-pair6-work` (images hard-linked from `/var/tmp/cp-v3n28/images`, verified; nothing
  downloaded), evidence root `/var/tmp/cp-pair6-evidence`, `CELIKPANEL_REPO=/root/cp-pair6/driver`.
- **Read-only catalog watcher** outside the driver, as before (`watch-catalog.sh`, `pdnsdb.py`): its
  AXFR requests come from the secondary's address and are not distinguishable in server logs from
  native transfers.
- **New read-only collection this run:** `*-agent-proof-code-check.txt` (counts of the three check
  strings and three other peer codes over all Agent journal boots, and code occurrences in the
  ledger file); `*-agent-private-dir.txt` (`ls`/`stat` of `/var/lib/celikpanel-agent-private`,
  hidden stage names, filesystem, Agent unit sandbox properties; no file content). The rndc secret
  was hashed on each BIND guest and only the digest kept, host-only (`/root/cp-pair6/secret-hashes/`).
- Cells stopped with `fixture.py stop` (QMP quit; overlays kept, no teardown): `t*/obs/cell-stop.log`.

## Wall time (UTC, 2026-09-30)

Host check 05:48:47; web build 05:49:19–05:49:39; host setup 05:49:58–05:50:22; archive
05:50:31–05:51:26; offline tests to 05:51:41. t1 prepare 05:52:31, run 05:53:09–06:02:42, stopped
06:07:20; t2 prepare 06:07:33, run 06:08:36–06:18:13, stopped 06:19:38; t3 prepare 06:19:42, run
06:20:50–06:31:27, stopped 06:32:57; pack about 06:40. About 55 minutes. Runner logs:
[build/runner-logs/](build/runner-logs/). Host before the run: 15697 MiB memory (14596 available),
790 GB free disk, 16 CPUs, KVM, no QEMU process ([build/host.txt](build/host.txt),
[build/hostcheck-summary.txt](build/hostcheck-summary.txt)).

## License fixture

Every Panel reported the acceptance fixture license (`result.json` `license_status`). Journal check:
0 lines naming `celikpanel.net` or the refusing transport on all six Panels. The in-process
refused-attempt counter is not exposed and was not read.

## What remains on the host

Stopped cells (overlays kept, no QEMU process running) under `/var/tmp/cp-pair6-work/cells/`:
`f3c3fe2a16393ce3365fef0b` (`pair-accept__bind-debian13__bind-arch__p6t1`), `6dc5f5a73f81905da610fd42`
(`pair-accept__bind-arch__pdns-debian13__p6t2`), `00c285499965de492839dd97`
(`pair-accept__pdns-debian13__bind-arch__p6t3`), each with its deletion pending. Also
`/var/tmp/cp-pair6-work/images` (hard links), `/var/tmp/cp-pair6-dist/`, `/var/tmp/cp-pair6-evidence/`,
`/var/tmp/cp-pair6-key/` and `/root/cp-pair6/` (clone, driver, run scripts, logs, obs, test copies,
evidence pack, scan self-test, host-only secret digests). Nothing outside `cp-pair6*` was created,
changed or deleted on the host.

## Integrity and secrets

Each run directory carries the driver's `SHA256SUMS`, re-verified with `evidence.verify_sums` (no
mismatch). [SHA256SUMS](SHA256SUMS) covers every file in this folder except itself, this README
included. The independent scanner (pair5's) first flagged all planted sample kinds (15 findings in
10 files; a redacted control file not flagged) and then found **0 findings over 625 files** in the
pack ([build/secret-scan-evidence.txt](build/secret-scan-evidence.txt),
[build/secret-scan-selftest-summary.txt](build/secret-scan-selftest-summary.txt)). **rndc key
content:** [build/rndc-key-content-scan.txt](build/rndc-key-content-scan.txt) hashed 3981
base64-alphabet tokens in 626 files against the four guest-side secret digests (t1 arch, t1
debian13, t2 arch, t3 arch): **0 matches**; its self-test found a planted secret only in the planted
file. The companion records' `sha256` is the product's hash of the key file, not key content. The
enrollment transcripts contain the inspector's SSH **public** key, as in pair5. The longest path is
187 characters from the repository root.

## What this does not prove

License behaviour (acceptance-fixture build). The archive is an unsigned local build from the
acceptance branch, not a release; the guests are disposable QEMU guests on an isolated link, not an
installed server; nothing about Frankfurt/Boston is implied. One run per topology. No topology
completed a deletion, the authoritative-absence check, re-add, management-disabled reboot or
management return, so pair5 t3's completion is not reproduced on this build. P6-1's cause is a
source reading matched to timing, not a logged reason; which of the three `verifyCurrent` calls in
consume failed, and the exact error, are not known. The `pdns-peer-inspect` outcome fields are
inferred from the code path, not observed. That P5-1 is fixed is shown only as far as the
post-inspection checks ran; the proof did not complete. Whether a second owner retry would pass (a
fresh 15 s wave) was not tested. `dns_peer_proof_internal`, the new `dns_peer_owner_edit_unknown`
text, the `detail` field, `mail_runtime_cleanup_failed`, the version-1 → 2 generation transition and
the rndc-key rollback rule were not exercised.
