# DNS pair product-flow acceptance, fifth native run (pair5), 2026-09-30

Exploratory. Fifth native run of the product-flow DNS pair acceptance driver with
`--license-mode acceptance-fixture` on two disposable QEMU guests (fixture `debian13`
192.0.2.10, `arch` 192.0.2.11) on the local WSL `archlinux` host. No installed server,
remote host, release or signed bundle was touched; nothing contacted celikpanel.net or any
license service; nothing was pushed, published or signed. Nothing here passes an
acceptance-register row; every `result.json` keeps `native_evidence: false`. The results below
are observations.

- **Product:** commit `b1e322756736f1a074bfca3f25cf6d7c3c28619b` (tree
  `8d7d078b652dbe1e0da54cf972de1fd3db5c18b8`), local branch `accept/pdns-primary-gate-open-7`
  = main line `4941b605` ("PowerDNS peer inspector: accept the CelikPanel-managed secondary;
  config_unreviewed reason", on top of `0988bc9a` "Domain deletion: loopback catalog transfer on
  the managed BIND secondary, inspector reasons, truthful mail stage") plus "Open the fresh paired
  PowerDNS primary gate (acceptance candidate)". Built only from `git archive b1e32275` (the
  `gate7` worktree was not used). This run is the first native measurement of `0988bc9a` and
  `4941b605`.
- **Driver:** `git archive 4941b60536f3d1d23f4af6cf71330c49df888e24 deploy/e2e/dns-pair-acceptance
  deploy/e2e/dns-kill-matrix` (tree `88229be7264084a04031ca343e5b3707d1bad678`; its only driver
  change since pair4's `92fc5eee` is `guidance.py`: `dns_peer_catalog_transfer_refused` reviewed,
  mail-stage failure reviewed), extracted to `/root/cp-pair5/driver`. **No driver file was
  patched; no pass or redaction rule was changed.** There is no `harness-workarounds.diff`.
- **Repository `HEAD`** (branch `feat/dns-artifact-separation`): `4941b605` at start and end. The
  uncommitted `deploy/e2e/dns-kill-matrix/*` modifications in the working tree were there before
  the run and were not used (the driver came from the archive).

## Result: t3 completed every step; t1 and t2 stopped in zone delete on one product defect

| Dir | Topology (primary / secondary) | `overall` | Last step reached | Stopped by (classification) |
|---|---|---|---|---|
| `t1-bind-bind/p5t1-20260930t041418z` | BIND debian13 / BIND arch | **failed** | `zone-delete` (failed, 319.9 s) | P5-1 (PRODUCT): after owner enrollment the retry stays pending `dns_peer_owner_edit_unknown`; the inspection itself completed |
| `t2-bind-pdns/p5t2-20260930t043751z` | BIND arch / PowerDNS debian13 | **failed** | `zone-delete` (failed, 322.8 s) | P5-1 (PRODUCT), same code after `dns-peer-enroll --engine pdns` |
| `t3-pdns-bind/p5t3-20260930t042946z` | PowerDNS debian13 / BIND arch | **passed** | `collect` | every step 00-19 passed |

`overall_cause` is `unclassified` for t1/t2 (`zone-delete` carries no `cause`; the driver sets
`cause: product` only for its open-ended-unknown rule). The classification above is this run's.
Per the run rules no topology was re-run and nothing on a guest was changed to get past a stop;
the t1/t2 cells were stopped with their state (overlays kept). The t3 cell was stopped after the
run (overlays kept).

**Pair4's two stops are gone in this build.** P4-1: the Arch BIND secondary now renders the
catalog zone with `allow-transfer { 192.0.2.10/32; 127.0.0.1/32; ::1/128; }` and named served
the inspector's loopback AXFRs (no "denied" line in any topology). P4-2: every deletion logged
"mail runtime cleanup skipped: no mail runtime for this domain on this server" and reached
`dns_cleanup`, including on the Arch primary (t2) whose `/var/mail -> spool/mail` is unchanged.

## P5-1 (t1, t2): a completed inspection on a BIND primary is refused as `dns_peer_owner_edit_unknown`

Classification: **PRODUCT**. Only the BIND-primary topologies stop; the PowerDNS primary (t3)
with the same Arch BIND secondary and inspector completes.

What happened (t1; t2 is the same sequence with `--engine pdns`):

1. `DELETE /api/v1/domains/1` → **202** `{"reason": "dns_peer_enrollment_required", "stage":
   "dns_cleanup", "status": "deletion_pending"}` (04:18:16Z, `t1-bind-bind/p5t1-*/steps/15-zone-delete/api/0056-…json`);
   saved status the same (`0057`). No `detail` field (the reason has none).
2. Owner enrollment exactly as the text says, all exit 0
   (`steps/15-zone-delete/owner-enrollment-transcript.json`; `<tools>` =
   `/var/backups/celikpanel/pair-accept/celikpanel-v0.0.0-pairaccept-acceptance-license.b1e322756736/dns-owner-tools`):
   ```
   t1 debian13: sudo -n <tools>/dns-peer-enroll primary-prepare                      -> prepared
   t1 arch:     (reviewed public key written root-only to /root/celikpanel-primary-inspector.pub)
   t1 arch:     sudo -n <tools>/dns-peer-enroll secondary-install --primary-ip 192.0.2.10 --peer-ip 192.0.2.11 --catalog catalog-c000020a.celikpanel.invalid --primary-public-key /root/celikpanel-primary-inspector.pub --inspector <tools>/bind-peer-inspect   -> configured
   t1 arch:     sudo -n <tools>/dns-peer-enroll secondary-host-key
   t1 debian13: sudo -n <tools>/dns-peer-enroll primary-activate --credential-id <id> --primary-ip 192.0.2.10 --peer-ip 192.0.2.11 --catalog catalog-c000020a.celikpanel.invalid --host-key-sha256 <digest>   -> configured
   t1 debian13: primary-status -> configured ; arch: secondary-status -> configured

   t2 arch:     sudo -n <tools>/dns-peer-enroll primary-prepare --engine pdns        -> prepared
   t2 debian13: (public key written root-only)
   t2 debian13: sudo -n <tools>/dns-peer-enroll secondary-install --engine pdns --primary-ip 192.0.2.11 --peer-ip 192.0.2.10 --catalog catalog-c000020b.celikpanel.invalid --catalog-account celikpanel-peer-catalog-v1 --primary-public-key /root/celikpanel-primary-inspector.pub --inspector <tools>/pdns-peer-inspect   -> configured
   t2 debian13: sudo -n <tools>/dns-peer-enroll secondary-host-key --engine pdns
   t2 arch:     sudo -n <tools>/dns-peer-enroll primary-activate --engine pdns --credential-id <id> --primary-ip 192.0.2.11 --peer-ip 192.0.2.10 --catalog catalog-c000020b.celikpanel.invalid --host-key-sha256 <digest>   -> configured
   t2 arch: primary-status --engine pdns -> configured ; debian13: secondary-status --engine pdns -> configured, catalog_account celikpanel-peer-catalog-v1
   ```
   Host-key digest agreed three ways (CLI, `/etc/ssh/ssh_host_ed25519_key.pub`, fixture
   known-hosts): t1 `b2498d40…5327`, t2 `9a4f27d4…5007`, t3 `396f98ef…fff4`. The t2
   `--catalog-account` was read read-only from the secondary's catalog CONSUMER row. This is the
   **first native `dns-peer-enroll --engine pdns`**; every command exited 0.
3. The same `DELETE` once more → **202** `{"reason": "dns_peer_owner_edit_unknown", "stage":
   "dns_cleanup", "status": "deletion_pending"}` (t1 04:18:30Z after 10.05 s, `0058`; t2 04:41:53Z
   after 12.6 s, `0057`). The saved status kept that reason for the full 300 s of read-only
   polling (t1 `0059`-`0090`, t2 `0058`-`0089`). No `detail` field. Nothing retried by itself.

Timeline of the retry, read-only (`t1-bind-bind/obs/arch-inspector-journal-zone-delete.txt`,
`debian13-named-journal-zone-delete.txt`, `debian13-agent-panel-deletion-journal.txt`,
`debian13-ledger-dns-jobs.txt`; t2 `obs/debian13-inspector-journal-zone-delete.txt`,
`arch-named-journal-zone-delete.txt`, `arch-ledger-dns-jobs.txt`):

| | t1 (BIND primary debian13) | t2 (BIND primary arch) |
|---|---|---|
| Panel: mail stage | 04:18:21 "mail runtime cleanup skipped: no mail runtime for this domain on this server" | 04:41:41 same |
| Pre-inspection proofs (catalog AXFR local + peer, member AXFR `NOTAUTH`) | 04:18:21-22 | 04:41:41-43 |
| Challenge journal published (ledger attempt 3, request attempt 1) | 04:18:26 `dns-peer-challenge-v1.json` | 04:41:48 `pdns-peer-challenge-v1.json` |
| Inspector on the secondary | 04:18:28 SSH `celikpeer`; 04:18:29 `sudo … celikpanel-bind-peer-inspect`; `rndc zonestatus` catalog + member, **two loopback catalog AXFRs ended normally** (serial 3); session closed 04:18:29 | 04:41:51 SSH `celikpeer`; `sudo … celikpanel-pdns-peer-inspect`; session opened and closed within the second |
| Agent pending code | 04:18:30 `dns_peer_owner_edit_unknown` | 04:41:53 `dns_peer_owner_edit_unknown` |
| AXFR from the primary after the inspection | **none** on either named/pdns until the watcher's 04:18:34 | **none** (next from 192.0.2.10 at 04:41:54 is the watcher) |
| Challenge journal state at the end | `outstanding` | `outstanding` |
| Ledger job | attempt 3, `pending`, `error_code dns_peer_owner_edit_unknown` | same |

So the SSH inspection returned without a transport error (an inspector failure would have been
`dns_peer_inspection_unknown[:<token>]` or `dns_peer_catalog_transfer_refused`), and the Agent
stopped before its post-inspection pair-readiness re-check (which starts with a catalog AXFR that
named logs) and before proof verification (the challenge was never consumed).

Source reading (`b1e32275`), consistent with every observation above:

- `cmd/agent/dns_engine_bind_propagation.go:36-77` `bindV3PrimaryPropagationPlan` builds the plan
  with `Evidence`, `Changed` and `Operation` and **never sets `SourceState`**; the only assignment is
  the PowerDNS plan (`cmd/agent/dns_engine_pdns_propagation.go:230`, `SourceState: plan.State`).
- Before the inspection, `recheckNativePeerLocalEvidence` accepts the empty engine as BIND
  (`cmd/agent/dns_engine_peer_native_linux.go:257-261`, `case "", transport.DNSEngineBIND:`), so the
  challenge is minted and the SSH inspection runs.
- Right after it, both verifiers call `catalogAXFRProbesForSourceEngine(plan.SourceState.Engine)`
  (`cmd/agent/dns_engine_peer_native_linux.go:106-109`, `cmd/agent/dns_engine_peer_pdns_linux.go:189-192`),
  which has no empty case (`cmd/agent/dns_engine_pdns_propagation.go:277-286`: "DNS catalog producer
  engine is unavailable"), and the error maps to `transport.DNSPeerPendingOwnerEditUnknown`.
  `cmd/agent/dns_catalog_axfr_test.go:361` asserts that the empty engine is refused.
- The four cited Agent files are byte-identical on the main line `4941b605`; the gate-open commit
  changes none of them.

The cause is inferred from source and timing, not logged: the Agent writes only the code. The
other post-inspection `verifyCurrent` failures (ledger attempt, local BIND evidence) map to the
same code and are not excluded by a log line; they are not suggested by anything observed. A
PowerDNS primary sets `SourceState` and passed (t3).

Native state while pending and after the run (driver probe, `steps/15-zone-delete/native-pending-*.json`,
`obs/*-native-after-run.json`, `obs/catalog-after-run.json`): t1 both `absent` ("no matching zone" /
no zone file), catalog serial 3 with no member; t2 BIND primary `absent`, PowerDNS secondary no
zone row (only the CONSUMER catalog row), catalog serial 3 with no member. The zone was gone
natively on both servers in both topologies; the product's proof of it did not complete. The
driver's authoritative-absence check, re-add, reboot and management return were not reached.

D-024 observation (not a driver finding; the driver rates the reviewed code actionable): after a
correct enrollment and a completed inspection, the owner is told "local or peer evidence changed
during verification. This server's administrator must reconcile the accepted operation with
native DNS configuration" although no one changed anything; the text names no concrete action
the owner can take, and a retry would meet the same refusal.

## t3 (PowerDNS primary / BIND secondary): the whole flow, observations

Every step passed; `result.json`: `overall: passed`, `overall_cause: null`, `failure_causes: []`,
`native_evidence: false`, `license_mode: acceptance-fixture`. The per-step verdict objects (with
`checks`) are in `t3-pdns-bind/p5t3-20260930t042946z/result.json`, `steps[15..18]`; summary:

- **zone-delete** (passed, 26.4 s): `delete_first_outcome` `{"reason": "dns_peer_enrollment_required",
  "stage": "dns_cleanup", "state": "pending"}` (04:33:34, `0055`); enrollment (BIND secondary, no
  `--engine`); retry `DELETE` → **200** `{"domain": "pair-accept.test", "status": "deleted"}`
  after 13.8 s (04:33:52, `0057`, `delete_final_outcome`). The Agent job moved from
  `dns_peer_enrollment_required` (attempts 1-2) to `succeeded` at attempt 3, 04:33:52
  (`obs/debian13-ledger-dns-jobs.txt`); the BIND challenge journal was retired (absent). The
  inspector ran at 04:33:45 with two loopback catalog AXFRs (serial 1790742761). Panel list empty
  (`0058`); `dns_absent` passed, all eight observations `REFUSED aa=False`;
  `native_delete_primary`/`secondary` `absent`; `catalog_delete` member absent, serial 1790742761.
- **zone-readd** (passed, 9.0 s): new domain, `dns_readd` serial 2026093001, native `present` on
  both, catalog member present (serial 1790742762), old record absent.
- **independence-reboot** (passed, 78.9 s): Panel and Agent `inactive/disabled` on both after an
  orderly reboot of both (`management-after-reboot-*.json`; boot IDs changed: arch
  `dfd1ca03…` → `ee26de7f…`, debian13 `ae675d90…` → `b26f99cc…`); `pdns.service` (debian13) and
  `named.service` (arch) active; both servers answered SOA/NS NOERROR over UDP and TCP with serial
  2026093001 (`dns-management-absent-*.json`); native state present on both; catalog member present;
  Agent ledger digests unchanged (arch `df9758fc…`, debian13 `66f32025…`). The PowerDNS producer
  catalog serial was re-stamped by the daemon at its start (1790742762 → 1790742863); the member
  zone serial did not change.
- **management-return** (passed, 7.3 s): both Panels back with the same TLS leaf; D1 held on both
  (`pair_ready: true`, `secondary_ready` absent on the primary; `secondary_ready: true`,
  `pair_ready: false` on the secondary), same engine/topology/role/domains as before the disable,
  no setup state change, ledger digests unchanged.

## Configuration and receipt observations (read-only, after each run)

- **Arch BIND secondary catalog stanza** (`named-checkconf -p`; t1 and t3,
  `obs/arch-bind-catalog-stanzas.txt`, `obs/arch-named-effective-options.txt`):
  ```
  zone "catalog-c000020a.celikpanel.invalid" {
  	type secondary;
  	primaries { 192.0.2.10; };
  	allow-transfer { 192.0.2.10/32; 127.0.0.1/32; ::1/128; };
  };
  ```
  Options scope: `allow-transfer { 192.0.2.10/32; };` and `catalog-zones { zone
  "catalog-c000020a.celikpanel.invalid" default-primaries { 192.0.2.10; } in-memory yes; };`.
  **Member zones have no stanza** on the secondary: they come from the catalog (`in-memory yes`),
  so `named-checkconf -p` cannot show one; they inherit the options-scope
  `allow-transfer { 192.0.2.10/32; }`. (In t1 the member was already deleted when collected; in t3
  the re-added member was present and still not in the configuration.)
- **Generation receipt** (Arch secondary, `/var/named/celikpanel/generations/68b2d37e…/receipt.json`,
  the `current` target, t1 and t3): schema `celikpanel-bind-generation-receipt/v2`,
  `pairing.role secondary`, `pairing.secondary_config_version = 2`. BIND primaries' receipts carry no
  such key (t1 debian13, t2 arch: role `primary`).
- **named journal, 127.0.0.1/::1 with denied/transfer**: t1 four lines (two loopback AXFRs
  started/ended, 04:18:29), t3 four lines (04:33:45); **no "denied" line** from any client in any
  topology.
- **Agent inspector exchange**: the primary Agent logs only the pending code (t1/t2
  `dns_peer_owner_edit_unknown`; t3 none after enrollment). No reason-token line appeared anywhere:
  no inspector reported a reviewed token.
- **Mail stage** (`obs/*-agent-panel-deletion-journal.txt`, Panel section): every deletion attempt
  in every topology logged "domain deletion for pair-accept.test: mail runtime cleanup skipped: no
  mail runtime for this domain on this server" (t1 04:18:12/04:18:21 debian13, t2 04:41:32/04:41:41
  arch, t3 04:33:30/04:33:38 debian13). t2 Arch mail root unchanged: `/var/mail -> spool/mail`
  (package `filesystem 2025.10.12-1`), `/var/mail/vhosts` absent (`obs/arch-mail-root-readonly.txt`).
- **PowerDNS secondary, t2** (`obs/debian13-pdns-daemon-group.txt`): `pdns_server` PID 5023,
  `Uid 103`, `Gid 104`, `Groups: 104` = group `pdns`; `/etc/powerdns/pdns.conf` `root:pdns 0640`,
  20579 bytes, SHA-256 `8b46927e…f262a` (active lines `include-dir=/etc/powerdns/pdns.d`, `launch=`,
  `security-poll-suffix=`); `pdns.d` holds exactly `celikpanel.conf` (`launch=gsqlite3`,
  `gsqlite3-dnssec=yes`, `gsqlite3-database=/var/lib/powerdns/pdns.sqlite3`,
  `local-address=10.0.2.15,192.0.2.10`, `zone-cache-refresh-interval=0`, `webserver=no`, `api=no`)
  and `celikpanel-cluster.conf` (`primary=yes`, `secondary=yes`, `allow-axfr-ips=192.0.2.11`), both
  `root:root 0644`.
- **`pdns-peer-inspect` outcome fields** (`catalog_state`, `member_state`, `native_state`): **not
  observed.** The response is not logged or stored; the challenge stayed `outstanding` because the
  Agent stopped (P5-1) before `pdnspeerproof.Verify`, the only place that reads them. What the
  timing shows: the forced command ran and the Agent recorded no inspection-failure code, so the
  managed PowerDNS shape was not refused as `config_unreviewed` (inferred from the code path; no
  reason line exists to confirm it).

## rndc key (metadata only; content never read into any file)

| Topology / node | Key | Owner, mode | Birth | Agent log | named first start | Companion `provenance` |
|---|---|---|---|---|---|---|
| t1 arch (secondary) | `/etc/rndc.key` | `root:named 0640`, no package owns it | 04:17:30.326 | 04:17:30 "… is product_created" | 04:17:33 | `product_created`, sha256 `866bd4b0…debe` |
| t2 arch (primary) | `/etc/rndc.key` | `root:named 0640` | 04:40:40.004 | 04:40:40 "… is product_created" | 04:40:42 | `product_created`, sha256 `28cb9af7…c722` |
| t3 arch (secondary) | `/etc/rndc.key` | `root:named 0640` | 04:32:50.479 | 04:32:50 "… is product_created" | 04:32:53 (again 04:34:44 after reboot) | `product_created`, sha256 `c8b94bbd…6eda` |
| t1 debian13 (primary) | `/etc/bind/rndc.key` | `bind:bind 0640`, not a dpkg path | 04:17:12.728 (package configuration) | 04:17:15 "… is owner_or_package_provided (key_present)" | 04:17:19 | `owner_or_package_provided`, `basis: key_present` |

Arch `bind 9.20.29-1`, Debian `bind9 1:9.20.29-1~deb13u1`. No `bind_rndc_unavailable` anywhere.
Files: `t*/obs/<node>-rndc-key.txt`.

## PowerDNS notify (t3 primary)

`t3-pdns-bind/obs/notify-counts.txt`, `debian13-pdns-notify-journal.txt`: 13 "Notification request
to host 192.0.2.11:53 … received from operator", 0 without port, 0 "spurious notify answer", 0
"failed after retries". Arch secondary: 16 "received notify" (10 catalog, 6 member). pair4 t3 had
11/0/0 for six mutations; pair5 t3 also ran the re-add and the reboot.

## Guidance (D-024), verbatim

Setup states match pair4 (keys `setup.guide.primary` + `setup.guide.startSecondary`,
`setup.guide.component`, `setup.guide.secondary` + `setup.guide.startPrimary`,
`server_setup_access_dns_required` on both roles, `setup.guide.firewall` on the Arch secondaries of
t1/t3); texts in each `result.json`. Peer-start checks: only while the primary's DNS step was
`running` (no finding). No `license_required` observation, no open-ended unknown, no
contradictory-peer-start finding. Each setup ended waiting at `access_dns` (findings
`setup-not-complete-offline-*`, expected offline).

Deletion states (EN | TR as the web shows them; server `message` differs only where noted):

- **All three, first DELETE and saved status, `dns_peer_enrollment_required`** (actor "primary
  administrator together with the secondary owner"): "The DNS change is saved on this server, but
  the secondary has not been shown to have removed the zone: no inspection access is set up
  between the two servers. The administrators of both servers set it up once with the
  dns-peer-enroll owner tool (add --engine pdns to each command when the secondary runs
  PowerDNS): primary-prepare here, secondary-install and secondary-host-key on the secondary,
  then primary-activate here. After that, use “Retry this deletion” to retry the same
  publication; it continues from where it stopped. Nothing retries by itself." | "DNS değişikliği
  bu sunucuda kaydedildi, ancak ikincil sunucunun bölgeyi kaldırdığı doğrulanamadı: iki sunucu
  arasında inceleme erişimi kurulmamış. İki sunucunun yöneticileri bunu dns-peer-enroll sahip
  aracıyla bir kez kurar (ikincil sunucu PowerDNS çalıştırıyorsa her komuta --engine pdns
  ekleyin): burada primary-prepare, ikincil sunucuda secondary-install ve secondary-host-key,
  sonra burada primary-activate. Ardından “Bu silme işlemini yeniden dene” ile aynı yayını yeniden
  deneyin; işlem kaldığı yerden sürer. Kendiliğinden yeniden denenmez." (Server message: "…After
  that, retry the same publication; …".) The t2 text names `--engine pdns`, so no
  `d024-observation-pending-deletion-pdns-engine-selector` was recorded; `--catalog-account` is
  named only by the tool's README.
- **t1, t2, DELETE after enrollment and saved status, `dns_peer_owner_edit_unknown`** (actor
  "stated in text"): "The DNS change is saved, but local or peer evidence changed during
  verification. This server's administrator must reconcile the accepted operation with native DNS
  configuration, then retry the same publication." | "DNS değişikliği kaydedildi; doğrulama
  sırasında yerel veya eş kanıt değişti. Bu sunucunun yöneticisi kabul edilen işlemi yerel DNS
  yapılandırmasıyla uzlaştırıp aynı yayını yeniden denemeli." (Server message: "… then retry this
  same publication.")
- **`detail` sentences:** none. No 202 or saved status in this run carried a `detail` field, and
  no `mail_runtime_cleanup_failed`, `dns_peer_catalog_transfer_refused` or
  `dns_peer_inspection_unknown[:token]` state occurred.

## Per-topology step verdicts (from `result.json`; seconds from `started_at`/`finished_at`)

| # | Step | t1 BIND deb / BIND arch | t2 BIND arch / PowerDNS deb | t3 PowerDNS deb / BIND arch |
|---|---|---|---|---|
| 00 | preflight | passed 2.3 | passed 2.1 | passed 2.3 |
| 01 | install-primary | passed 23.3 | passed 137.1 (incl. owner restart, arch) | passed 22.5 |
| 02 | install-secondary | passed 140.9 (incl. owner restart, arch) | passed 22.1 | passed 137.6 (incl. owner restart, arch) |
| 03–04 | login-* | passed 0.4 / 0.3 | passed 0.4 / 0.5 | passed 0.4 / 0.4 |
| 05–06 | license-* | passed 0.3 / 0.3 | passed 0.3 / 0.3 | passed 0.3 / 0.3 |
| 07 | setup-review-primary | passed 1.9 | passed 1.1 | passed 0.9 (no gate blocker) |
| 08 | setup-start-primary | passed 15.8 | passed 12.9 | passed 12.8 |
| 09 | setup-review-secondary | passed 1.1 | passed 0.9 | passed 1.1 |
| 10 | setup-start-secondary | passed 12.9 | passed 12.8 | passed 12.9 |
| 11 | pair-ready | passed 8.2 | passed 7.9 | passed 7.9 |
| 12 | zone-add | passed 9.3 | passed 8.1 | passed 8.8 |
| 13 | record-add | passed 5.6 | passed 4.8 | passed 5.3 |
| 14 | record-edit (`ui-replace`) | passed 9.7 | passed 8.6 | passed 9.0 |
| 15 | zone-delete | **failed** 319.9 | **failed** 322.8 | passed 26.4 |
| 16 | zone-readd | not-run | not-run | passed 9.0 |
| 17 | independence-reboot | not-run | not-run | passed 78.9 |
| 18 | management-return | not-run | not-run | passed 7.3 |
| 19 | collect | passed 3.3 | passed 3.5 | passed 3.2 |

`zone-delete` reason verbatim (t1, t2): "deletion stayed pending after owner enrollment and one
retry". Owner steps (`result.json` `owner_steps`): the install.sh "RESTART THIS SERVER NOW" reboot
of the arch guest in each topology (kernel `7.1.8-arch1-3`, fixture reboot, new boot ID) and the
enrollment in each topology.

## Builds

- `web/dist`: Windows, `git archive b1e32275 web` into the session scratchpad, `web/node_modules`
  junction to the repository's (lockfile SHA-256 `8dab8b6b…6d26` identical to the commit's), `npm
  run build` (node v24.18.0, npm 11.16.0), 04:10:43–04:10:59Z, **exit 0** (`tsc`, `vite build`,
  recovery worker, bundle budget; [build/web-build.log](build/web-build.log)). 106 files; the host
  copy equals the Windows build after normalising `sha256sum`'s binary marker and sort order, and
  equals the `web/dist` inside the dist ([build/web-dist-windows-build.sha256](build/web-dist-windows-build.sha256),
  [build/web-dist.sha256](build/web-dist.sha256)).
- Run host: `/root/cp-pair5/repo` = `git clone --no-checkout` of the repository plus that `web/dist`;
  `build-dist.sh --acceptance-license b1e32275…` with `CELIKPANEL_REPO` pointing at it,
  04:12:13–04:12:58Z ([build/build-dist.log](build/build-dist.log),
  [build/build-dist-make.log](build/build-dist-make.log), [build/dist.json](build/dist.json),
  [build/go-version.txt](build/go-version.txt): `go1.26.5 linux/amd64`). Archive
  `celikpanel-v0.0.0-pairaccept-acceptance-license.b1e322756736-acceptance-license.tar.gz`,
  SHA-256 `1100f0eb611ec9fef0140adfce735593c54a100d20d0d25c613df33c93005183`, unsigned local
  build, `release: false`.
- Packaging guard: refused the acceptance tree three ways ([build/guard-refusal.txt](build/guard-refusal.txt))
  and, run separately on the final `.tar.gz`, the same three ways with exit 1
  ([build/guard-refusal-archive.txt](build/guard-refusal-archive.txt)).
- Archive observation ([build/archive-evidence-entries.json](build/archive-evidence-entries.json)):
  645 entries, 0 under any `evidence/` directory, 362 under `deploy/e2e/`.
- Product texts: `build-dist.sh` exported the commit's `web/src` with marker `PRODUCT-COMMIT` =
  `b1e32275…`; `run-topology.sh` passed it as `--product-web-src`
  ([build/product-web-src.sha256](build/product-web-src.sha256)).
- Driver files as extracted: [build/driver-files.sha256](build/driver-files.sha256) (evidence
  entries omitted from the list).

## Offline tests

**129/129** pass for the run composition (driver archive + `git archive b1e32275 web/src` at the
driver root, the same `web/src` the run passes as `--product-web-src`):
[build/offline-tests-run-composition.log](build/offline-tests-run-composition.log). 129/129 also
pass with the driver commit's own `web/src` (`git archive 4941b605 web/src`):
[build/offline-tests-driver-commit-pure.log](build/offline-tests-driver-commit-pure.log).

## Exact commands, harness changes, deviations and additions

All host commands were LF script files in the session scratchpad run as
`MSYS_NO_PATHCONV=1 wsl.exe -d archlinux -u root -- bash /mnt/c/…/scratchpad/pair5/<script>.sh`;
copies are in [build/scripts/](build/scripts/) (`.txt`; `scan.sh` is omitted because it plants
secret-shaped self-test samples; it is pair4's with paths changed). Order: `hostcheck.sh`,
Windows web build, `setup-host.sh`, `cmpweb.sh`, `build.sh` (dist + offline tests), then per
topology `launch.sh <topology> <primary> <label>` (nohup `topo.sh` = `prepare-guests.sh` +
`run-topology.sh … --license-mode acceptance-fixture`), `launch-watch.sh` (catalog watcher),
read-only collection, `stopcell.sh`. Topologies and labels, in run order:
`bind/bind debian13 p5t1`, `pdns-primary/bind-secondary auto p5t3`,
`bind-primary/pdns-secondary auto p5t2`. Driver flags: defaults plus
`--license-mode acceptance-fixture`.

- **No harness workaround**; no driver defect was found. No pass rule was changed.
- **Run paths only** ([build/run-path-changes.diff](build/run-path-changes.diff)): copies of
  `build-dist.sh`, `prepare-guests.sh`, `run-topology.sh` (and an unchanged `teardown.sh`) with
  `/var/tmp/cp-pair-accept/dist/` and `/var/tmp/cp-pair-accept/id_ed25519` replaced by
  `/var/tmp/cp-pair5-dist/` and `/var/tmp/cp-pair5-key/id_ed25519`. Work root
  `/var/tmp/cp-pair5-work` (images hard-linked from `/var/tmp/cp-v3n28/images`, verified; nothing
  downloaded), evidence root `/var/tmp/cp-pair5-evidence`, `CELIKPANEL_REPO=/root/cp-pair5/driver`.
- **Read-only catalog watcher** outside the driver, as in pair3/pair4 (`watch-catalog.sh`,
  `pdnsdb.py`): catalog AXFR from the secondary to the primary every ~10 s, logged on change
  (`t*/obs/catalog-watch.jsonl`). It adds AXFR requests from the secondary's address that the
  servers' logs do not distinguish from native transfers (they are never from 127.0.0.1 and never
  from the primary's own address).
- **Read-only collection after each run, guests still up** (`ro-collect.sh`, `coll-extra.sh`,
  `diag-bind-transfer.sh`, `diag-options.sh`, `ledgerjob.sh`, `t1coll.sh`, `t1extra.sh`,
  `t1arch.sh`, `t3coll.sh`, `t2coll.sh`/`t2run.sh`, `t2insp.sh`, `t2native.sh`): rndc key metadata
  and companion record; notify journals; effective `named-checkconf -p` options and catalog
  stanzas (lines containing `secret`/`algorithm`/`key` dropped); generation receipts reduced to
  their pairing fields; named/sshd/sudo/Agent/Panel journal lines around the deletion; the
  Agent ledger's `dns_zone_sync` jobs (selected fields); the peer challenge journals reduced to
  schema/state/attempt/zone/addresses (no nonce); PowerDNS daemon identity and configuration
  lines (secret/key/password lines dropped); the driver's native/catalog probe after the run
  (t1, t2); the t2 mail root metadata. For the rndc content check, the collection computed on each
  BIND guest the SHA-256 of the key's secret string and kept only that digest in a host-only
  directory outside the evidence (`/root/cp-pair5/secret-hashes/`).
- Collection fixes during the run (collection scripts only, no driver or guest change): a CRLF
  in `coll-extra.sh` made its first t1 attempt fail (a stray `/root/cp-pair5/obs/p5t1\r`
  directory was created and removed with `fixcr.sh`); the receipt search was widened from
  `*bind*` to `*/generations/*` for Arch's `/var/named` layout; the t2 secondary journal slice was
  re-read with a time window (`t2insp.sh`) because the first read was dominated by the collection's
  own sudo lines.
- Cosmetic: Arch lacks `hostname`, so the Arch collection headers read "on  (Arch Linux)".
- Cells were stopped with `fixture.py stop` (QMP quit; overlays kept, no teardown):
  `t*/obs/cell-stop.log`.

## Wall time (UTC, 2026-09-30)

Host check 04:10:17; web build 04:10:43–04:10:59; host setup 04:11:48–04:11:59; archive
04:12:13–04:12:58; offline tests to 04:13:08. t1 prepare 04:13:15, run 04:14:17–04:23:34; t3
prepare 04:28:45, run 04:29:45–04:35:34; t2 prepare 04:36:52, run 04:37:50–04:46:57; cells
stopped 04:28:34 (t1), 04:36:48 (t3), 04:49:12 (t2); pack about 04:55. About 50 minutes. Runner
logs: [build/runner-logs/](build/runner-logs/). Host before the run: 15697 MiB memory (15002
available), 801 GB free disk, 16 CPUs, KVM, no QEMU process ([build/host.txt](build/host.txt)).

## License fixture

Every Panel reported the acceptance fixture license (`result.json` `license_status`). Journal
check: 0 lines naming `celikpanel.net` or the refusing transport on all six Panels. The in-process
refused-attempt counter is not exposed and was not read.

## What remains on the host

Stopped cells (overlays kept, no QEMU process running) under `/var/tmp/cp-pair5-work/cells/`:
`52a7b2e0d11ebb86a7eecb15` (`pair-accept__bind-debian13__bind-arch__p5t1`, deletion pending),
`cf91f89e0939f18b2a9b66fd` (`pair-accept__bind-arch__pdns-debian13__p5t2`, deletion pending),
`bea8d3640a748f117a825a70` (`pair-accept__pdns-debian13__bind-arch__p5t3`, re-added zone, after
management return). Also `/var/tmp/cp-pair5-work/images` (hard links), `/var/tmp/cp-pair5-dist/`,
`/var/tmp/cp-pair5-evidence/`, `/var/tmp/cp-pair5-key/` and `/root/cp-pair5/` (clone, driver, run
scripts, logs, obs, test copies, evidence pack, scan self-test, host-only secret digests). Nothing
outside `cp-pair5*` was created, changed or deleted on the host.

## Integrity and secrets

Each run directory carries the driver's `SHA256SUMS`, re-verified with `evidence.verify_sums` on
the host (no mismatch). [SHA256SUMS](SHA256SUMS) covers every file in this folder except itself,
this README included. The independent scanner from pair3 (only its docstring changed;
[build/secret-scan-evidence.txt](build/secret-scan-evidence.txt)) first flagged all planted sample
kinds (15 findings in 10 files; a redacted control file not flagged) and then found nothing in the
evidence: **0 findings over 627 files** (private keys, OpenSSH key blobs, cookies, bearer/basic
credentials, admin-password shape, license-key shape, TSIG/BIND `secret "…"`, JSON secret-named
keys). **rndc key content:** [build/rndc-key-content-scan.txt](build/rndc-key-content-scan.txt)
hashed 4003 base64-alphabet tokens in 628 files against the four guest-side secret digests (t1
arch, t1 debian13, t2 arch, t3 arch): **0 matches**; its self-test found a planted secret only in
the planted file ([build/secret-scan-selftest-summary.txt](build/secret-scan-selftest-summary.txt)).
The companion records' `sha256` is the product's hash of the key file, not key content. The
longest path is 187 characters from the repository root.

## What this does not prove

License behaviour is not evidenced (acceptance-fixture build). The archive is an unsigned local
build from the acceptance branch, not a release; the guests are disposable QEMU guests on an
isolated link, not an installed server; no Frankfurt/Boston behaviour is implied. One run per
topology. For t1/t2: no completed deletion, no product proof accepted, no authoritative-absence
check, no re-add, no management-disabled reboot and no management return. P5-1's cause is a source
reading matched to timing, not a logged reason. The managed-PowerDNS inspector's outcome fields
were not observed, and the owner-edit case from `docs/DNS-ENGINE-ARTIFACT.md` (a drop-in edited to
show `detail=config_unreviewed`) was not exercised: this run changes nothing on a guest outside the
product flow. `dns_peer_catalog_transfer_refused`, the `detail` field, `mail_runtime_cleanup_failed`
and the version-1 → 2 generation transition on an existing secondary were not exercised (every
secondary was fresh at version 2). The rndc-key rollback rule was not exercised. t3 passing is one
run of one topology; it shows the PowerDNS-primary path with a fresh Arch BIND secondary, not the
BIND-primary paths.
