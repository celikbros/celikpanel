# DNS pair product-flow acceptance, third native run (pair3), 2026-09-30

Exploratory. Third native run of the product-flow DNS pair acceptance driver with
`--license-mode acceptance-fixture` on two disposable QEMU guests (fixture `debian13`
192.0.2.10, `arch` 192.0.2.11) on the local WSL `archlinux` host. No installed server,
remote host, release or signed bundle was touched; nothing contacted celikpanel.net or any
license service; nothing was pushed or published. Nothing here passes an acceptance-register
row; every `result.json` keeps `native_evidence: false`. The results below are observations.

- **Product:** commit `d1f2ad87fbebd1d5c325a9fa8f0bd8579e30434d` (tree
  `c6c1d155d1d1b0986de8fc791f270aa92af1b519`), local branch `accept/pdns-primary-gate-open-3`
  = main line `8a548090` ("Zero-zone PowerDNS primary, truthful setup states, license refresh,
  release contents") plus "Open the fresh paired PowerDNS primary gate (acceptance candidate)".
- **Driver:** `git archive 8d94c8ffe43a4de0fb3adbd85d6ee4e38fdfa22c deploy/e2e/dns-pair-acceptance
  deploy/e2e/dns-kill-matrix` (main line; tree `6c9ef2aa1a0f0781b1458d21c1baef4ebfb4e569`),
  extracted to `/root/cp-pair3/driver` on the run host. Not run from a working tree. **No driver
  file was patched; no pass or redaction rule was changed.**
- **Repository `HEAD`** (branch `feat/dns-artifact-separation`): `48be63ac` when the build
  started, `1c5ebd56` at the end (other agents committed during the run; the product and driver
  used here are the archives above, unaffected).

## Result: all three topologies passed pair readiness and stopped at `zone-add`

| Dir | Topology (primary / secondary) | `overall` | Last step reached | Stopped by |
|---|---|---|---|---|
| `t3-pdns-bind/p3t3-20260930t000726z` | PowerDNS debian13 / BIND arch | **failed** | `zone-add` (failed) | native reader on the Arch BIND **secondary**: `native state is 'unknown', want present` |
| `t2-bind-pdns/p3t2-20260930t001511z` | BIND arch / PowerDNS debian13 | **failed** | `zone-add` (failed) | native reader on the Arch BIND **primary**: same |
| `t1-bind-bind/p3t1-20260930t002048z` | BIND debian13 / BIND arch | **failed** | `zone-add` (failed) | native reader on the Arch BIND **secondary**: same |

`overall_cause` is `unclassified` in all three (`zone-add` carries no `cause`). Steps 00–11
passed in every topology, including `pair-ready` with the corrected D1 rule on both Panels:
the first native passes of D1 for a primary (BIND twice, PowerDNS once). `record-add`,
`record-edit`, `zone-delete` (and therefore deletion proof and owner enrollment), `zone-readd`,
`independence-reboot` and `management-return` were **not run** (prerequisite chain) and
prove nothing either way. `collect` passed in all three.

**t3, first native measurement of the fresh paired PowerDNS primary with no zones:** the
wizard's setup DNS step on debian13 completed in 16 s (driver view 00:10:10–00:10:26Z; engine
operation `started_at` 00:10:11Z, `committed`/`succeeded` 00:10:25Z). pair2's P-A
(`verifyPDNSProducerMembershipTx` empty-versus-nil) did not recur. Its Panel then reported
exactly the D1 primary shape (`steps/11-pair-ready/engine-ready-primary.json`):
`active_engine: pdns, state: ready, topology: paired, pair_role: primary, pair_ready: true`,
no `secondary_ready` key, `zone_count: 0`, `revision: 3`, `engine_epoch: 1`. The zone was then
added, served authoritatively by both servers (UDP and TCP, one serial `2026093001`,
first attempt), native `present` on the PowerDNS primary, and listed in the catalog; only
the Arch secondary's native read failed.

## Stop S-1 (all three): the native reader needs an authenticated `rndc`; the product's Arch BIND has none

Classification: **driver pass rule versus observed product behaviour** (not changed here; the
owner decides). Not a mechanical driver defect: the reader did not misparse an answer, it
got none; and the check it feeds is a pass rule ("native state present on both").

Observed, identical in all three runs (`steps/12-zone-add/native-add-secondary.json` in t3/t1,
`native-add-primary.json` in t2):

```
"argv": ["/usr/sbin/rndc", "zonestatus", "pair-accept.test"],
"output": "rndc: neither /etc/rndc.conf nor /etc/rndc.key was found",
"returncode": 1  ...  "native_state": "unknown"
```

Driver side: `guest_probe.py:256-276` (`rndc_zone_state`) runs `rndc zonestatus` with rndc's
defaults; `:344-354` maps anything but `loaded`/exact-unloaded to `unknown`;
`dns_checks.py:139-151` (`evaluate_native`) fails `unknown` when presence is expected;
`pair_acceptance.py` `Driver.native` (`:561-581`) is called from `zone_add` (`:1315`).

Guest side, read-only (`t*/obs/arch-rndc-readonly.txt`, `t1-bind-bind/obs/debian13-rndc-readonly.txt`):
Arch `bind 9.20.29-1`, `named -f -u named` active, **no** `/etc/rndc.key`, `/etc/rndc.conf` or
any `rndc*.key` on the file system; `named` nevertheless listens on `127.0.0.1:953` and
`::1:953` ("configuring command channel from '/etc/rndc.key'", "command channel listening on
::1#953"); `/etc/named.conf` has no `controls` statement (only `include
"/var/named/celikpanel/current/zones.conf"`). On Debian 13 `/etc/bind/rndc.key` exists
(created at package configuration, not a dpkg-owned path) and `rndc status` works; the
PowerDNS and Debian-BIND native reads all returned `present`.

Product source that defines the behaviour (source reading, d1f2ad87):
- No product code creates an RNDC key: the only non-test `rndc` references are callers
  (`git grep rndc` over `*.go`/`*.sh` outside `deploy/e2e`).
- `cmd/dns-peer-enroll/README.md:3`: the tool "does not configure AXFR, create an RNDC key …".
- `cmd/bind-peer-inspect/README.md:5`: "a missing RNDC control listener … fail[s] closed. The
  Arch fixture needed an owner-prepared `/etc/rndc.key`; the enrollment CLI does not create
  or repair that native prerequisite." (`internal/bindpeerinspector/native_linux.go:84,96`
  runs `rndc -s 127.0.0.1 zonestatus`.)
- `deploy/e2e/dns-kill-matrix/evidence/pdns-bind-owner-proof-20260928/README.md:11`: "RNDC key
  preparation on this Arch guest was manual."

Consequence for the rest of the flow (source reading only, **not observed**): the Agent's
own BIND deletion proof calls `rndc zonestatus` without a key argument
(`cmd/agent/dns_engine_bind_deletion.go:20-28`, used by `verifyBINDDeletedZoneAt` via
`:99`/`:119`; called from `dns_engine_bind_catalog_deletion.go:87`). On an Arch BIND
**primary** installed by this product (t2) that proof cannot succeed as installed; on an
Arch BIND **secondary** the owner enrollment path (`bind-peer-inspect`) needs the same
owner-prepared key. Whether the product should create or require the key on Arch, or the
driver should perform the documented owner preparation as a recorded owner step, is the
owner's decision. Nothing on the guests was changed to get past it, and no topology was
re-run.

## t3 catalog with zero members and after the first zone (read-only observations)

`t3-pdns-bind/obs/catalog-watch.jsonl` (catalog AXFR from the BIND secondary to the primary,
the driver's own `guest_probe.py catalog`, every ~10 s, logged on change; on each change a
read-only SQLite open of the primary's `/var/lib/powerdns/pdns.sqlite3`, `mode=ro`) and
`t3-pdns-bind/obs/debian13-pdns-catalog-after.txt` (one more read at 00:13:36Z plus the
`pdns.service` journal lines naming catalog/notify):

| Time (UTC) | Members | Producer SOA serial | `domains.notified_serial` | `CATALOG-HASH` |
|---|---|---|---|---|
| 00:10:26–27 (after setup, zero zones) | `[]` | `1790727021` | `1` | `47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=` (= base64 SHA-256 of empty input) |
| 00:10:58–59 (zone added, product publication) | `[pair-accept.test.]` | `1790727022` (+1) | `1` | unchanged (empty-set hash) |
| 00:11:21 (PowerDNS journal) | — | — | — | "new CATALOG-HASH 'n6L34LeIr9zlcH/7CcW04T4BhE5GuvXjRFRo31n4dKY=' for zone 'catalog-c000020a.celikpanel.invalid'" |
| 00:13:36 | `[pair-accept.test.]` | `1790727081` (re-stamped by the daemon; = epoch 00:11:21Z) | `1790727022` | `n6L34LeIr9zlcH/7CcW04T4BhE5GuvXjRFRo31n4dKY=` |

The producer row is `PRODUCER`, account `celikpanel-bind-catalog-v1`; the catalog carries
only SOA `invalid invalid <serial> 60 30 3600 30` and NS `invalid.` records plus the member.
The zero-member serial `1790727021` equals the epoch of 00:10:21Z. The daemon's re-stamp after
a membership change matches what the product expects (`pdnsDaemonCatalogSerialAdvance`,
`cmd/agent/dns_engine_pdns_propagation.go:50-62`); no later product mutation ran, so the
product's handling of it was not exercised. The driver's own `catalog-add.json` recorded
serial `1790727022` with the member.

Other `pdns.service` journal lines (observation, cause not established; the Agent journal
shows no error from them): "Unable to queue notification of domain … to nameserver
'ns1.ns-accept.test' / 'ns2.ns-accept.test' / 'invalid': nameserver does not resolve!",
"Received spurious notify answer for … from 192.0.2.11:53", and at 00:11:29 "Notification
for catalog-c000020a.celikpanel.invalid to 192.0.2.11:0 failed after retries" (same for
`pair-accept.test`). The BIND secondary transferred the catalog and the zone regardless.
The Agent logged once, at 00:10:25Z before the secondary was set up, "pdns primary pair
readiness proof failed: DNS peer catalog AXFR is unavailable"; `pair_ready` was `true` by
the first `pair-ready` read at 00:10:43Z.

## Guidance (D-024)

No waiting/failed/pending state without actionable text was recorded; no D-024 finding and no
open-ended-unknown, contradictory-peer-start or license-required finding was raised
(`license_required_observations: []` in all three; pair2's P-C did not recur). `zone-add`
recorded no guidance (domain create HTTP 200). The deletion texts were not reached. States
shown, verbatim from `result.json` (EN | TR):

- **Primary setup progress** (`setup.guide.primary` + `setup.guide.startSecondary`, all three;
  t3 debian13): "This server is Primary: ns1.ns-accept.test (192.0.2.10). Its expected
  Secondary is ns2.ns-accept.test (192.0.2.11). | Start the secondary’s DNS setup, using this
  server as its primary, once this server’s DNS step shows as finished. Until then, do not
  point the secondary at this server. You do not need to wait for the rest of this server’s
  setup." | "Bu sunucu Birincil: ns1.ns-accept.test (192.0.2.10). Beklenen İkincil sunucu:
  ns2.ns-accept.test (192.0.2.11). | Bu sunucunun DNS adımı tamamlandı olarak görününce ikincil
  sunucunun DNS kurulumunu başlatın ve birincil olarak bu sunucuyu gösterin. O zamana kadar
  ikincil sunucuyu bu sunucuya yönlendirmeyin. Bu sunucudaki kurulumun geri kalanını
  beklemeniz gerekmez." Peer-start check: shown while the DNS step was `running`, phase
  `01-dns`, no error code (`peer_start_guidance_observations`), no finding.
- **Component** (`setup.guide.component`, all three): "The current component is shown below.
  If preparation stops, the reported error determines the next action. Selecting a component
  does not replace a required credential or license; follow a verified request if one is
  reported." | "İşlem yapılan bileşen aşağıda gösterilir. Hazırlık durursa sonraki adımı
  bildirilen hata belirler. Bileşeni seçmek, gerekli erişim bilgisi veya lisans yerine geçmez;
  doğrulanmış bir istek bildirilirse yönlendirmeyi izleyin."
- **Secondary setup progress** (`setup.guide.secondary` + `setup.guide.startPrimary`, all
  three; t3 arch): "This server is Secondary: ns2.ns-accept.test (192.0.2.11). Its expected
  Primary is ns1.ns-accept.test (192.0.2.10). | If the primary has not been configured, start
  its DNS setup now. It must serve the zones and allow this secondary to transfer them. If
  this operation has already stopped, resolve that requirement before reviewing a new plan." |
  "Bu sunucu İkincil: … | Birincil sunucu henüz yapılandırılmadıysa DNS kurulumunu şimdi
  başlatın. Bölgeleri sunmalı ve bu ikincil sunucunun onları aktarmasına izin vermelidir. Bu
  işlem zaten durmuşsa yeni planı incelemeden önce bu gereksinimi giderin."
- **Public hostname wait, primary** (`server_setup_access_dns_required`; actor "the domain
  owner (public DNS / registrar)"; expected offline): "Certificate issuance requires this public
  A record: panel-debian13.ns-accept.test → 192.0.2.10. | The public record has not been
  verified yet. This result alone does not establish whether the record is absent or a DNS
  server is unreachable. | Create or correct this record at the DNS provider or primary that
  manages it. You do not need to create a website in Domains. | This prerequisite is checked
  automatically. After it passes, the same setup continues. To change the reviewed names or
  addresses, use Edit setup plan below." | "Sertifika için gereken genel A kaydı: … | Genel
  kayıt henüz doğrulanmadı. Bu sonuç tek başına kaydın eksik olduğunu veya bir DNS sunucusuna
  ulaşılamadığını göstermez. | Kaydı onu yöneten DNS sağlayıcısında veya birincil sunucuda
  oluşturun ya da düzeltin. Alan Adları bölümünde web sitesi oluşturmanız gerekmez. | Bu
  gereksinim otomatik kontrol edilir. Doğrulanınca aynı kurulum devam eder. İncelenen adları
  veya adresleri değiştirmek için aşağıdaki Kurulum planını düzenle düğmesini kullanın."
- **Public hostname wait, secondary** (same code): third sentence "On the primary
  ns1.ns-accept.test (192.0.2.10), prepare the required record and allow this secondary to
  transfer the zone." | "Birincil ns1.ns-accept.test (192.0.2.10) üzerinde gereken kaydı
  hazırlayın ve bu ikincilin bölgeyi aktarmasına izin verin." (t2: primary 192.0.2.11).
- **Firewall** (`setup.guide.firewall`, t1 arch secondary during `pair-ready`): "The reviewed
  firewall rules must keep SSH and panel access available. If verification fails, check the
  reported access or firewall requirement before applying another plan." | "İncelenen
  güvenlik duvarı kuralları SSH ve panel erişimini korumalıdır. Doğrulama başarısız olursa
  başka bir plan uygulamadan önce bildirilen erişim veya güvenlik duvarı gereksinimini
  kontrol edin."

Each setup ended waiting at `access_dns` (findings `setup-not-complete-offline-*`, expected on
the isolated link). The native-reader stop (S-1) is not shown to the owner anywhere: it is
the driver's check, and the Panel reported the domain as created.

## Per-topology step verdicts (from `result.json`; seconds from `started_at`/`finished_at`)

| # | Step | t3 PowerDNS deb / BIND arch | t2 BIND arch / PowerDNS deb | t1 BIND deb / BIND arch |
|---|---|---|---|---|
| 00 | preflight | passed 2.5 | passed 2.3 | passed 2.4 |
| 01 | install-primary | passed 32.3 | passed 133.6 (incl. owner restart, arch) | passed 29.7 |
| 02 | install-secondary | passed 127.0 (incl. owner restart, arch) | passed 33.0 | passed 127.2 (incl. owner restart, arch) |
| 03–04 | login-* | passed 0.4 / 0.4 | passed 0.5 / 0.5 | passed 0.4 / 0.4 |
| 05–06 | license-* | passed 0.3 / 0.3 | passed 0.4 / 0.4 | passed 0.4 / 0.3 |
| 07 | setup-review-primary | passed 1.0 (no blocker) | passed 1.5 | passed 0.9 |
| 08 | setup-start-primary | passed 15.9 | passed 19.4 | passed 25.2 |
| 09 | setup-review-secondary | passed 1.2 | passed 1.1 | passed 1.2 |
| 10 | setup-start-secondary | passed 16.0 | passed 22.5 | passed 16.0 |
| 11 | pair-ready | passed 8.1 | passed 11.0 | passed 8.2 |
| 12 | zone-add | **failed** 8.3 | **failed** 7.1 | **failed** 7.9 |
| 13–18 | record-add … management-return | not-run | not-run | not-run |
| 19 | collect | passed 3.6 | passed 3.6 | passed 3.5 |

`zone-add` reason, verbatim: t3 and t1 `native state (add) differs: ['secondary: ["native
state is 'unknown', want present"]']`; t2 the same with `primary`. Checks that passed inside
`zone-add` in all three: `domain_listed: true`, `dns_add` (attempt 1, serial `2026093001`),
`catalog_add` (member present; serial t3 `1790727022`, t2 `2`, t1 `2`), native on the
Debian/PowerDNS side `present` (t1 debian13 BIND: `rndc zonestatus` `type: primary`,
`serial: 2026093001`; t3 debian13 and t2 debian13: PowerDNS row present, listed by
`pdns_control list-zones`).

Pair readiness as served (`steps/11-pair-ready/engine-ready-*.json`): primaries t3 `pdns`,
t2 `bind`, t1 `bind`, each `pair_role: primary, pair_ready: true`, no `secondary_ready`;
secondaries t3 `bind`, t2 `pdns`, t1 `bind`, each `pair_role: secondary, pair_ready: false,
secondary_ready: true`; all `state: ready, topology: paired, zone_count: 0`. Catalog before
the first zone: t2 serial `1`, t1 serial `1`, no members (`t*/obs/catalog-watch.jsonl`).

## Owner steps and owner enrollment

The only owner steps were the three post-install restarts of the arch guest that `install.sh`
asked for ("RESTART THIS SERVER NOW", kernel `7.1.8-arch1-3`, performed by the driver through
`fixture.reboot_guest`, new boot ID recorded in `owner_steps`). Owner enrollment
(`dns-peer-enroll`, BIND or PowerDNS) was not reached: `zone-delete` never ran.

## Builds

- `web/dist`: Windows, `git archive d1f2ad87 web` into the session scratchpad, `web/node_modules`
  junction to the repository's (lockfile SHA-256 identical to the commit's), `npm run build`
  (node v24.18.0, npm 11.16.0), 00:03:13–00:03:36Z. `tsc`, `vite build` and the recovery
  shell step completed; the bundle-budget check **exited 1** (`tr-D44QfBfj.js: 163.29 KiB raw /
  45.85 KiB gzip (limit 160.25 KiB / 45.25 KiB)`). Deviation: the produced dist (104 files) was
  used ([build/web-build.log](build/web-build.log)). The host copy is identical to the Windows
  build ([build/web-dist.sha256](build/web-dist.sha256),
  [build/web-dist-windows-build.sha256](build/web-dist-windows-build.sha256)).
- Run host: `/root/cp-pair3/repo` = `git clone --no-checkout` of the repository plus that
  `web/dist`; `build-dist.sh --acceptance-license d1f2ad87…` with `CELIKPANEL_REPO` pointing at
  it, 00:04:46–00:05:31Z ([build/build-dist.log](build/build-dist.log),
  [build/build-dist-make.log](build/build-dist-make.log), [build/dist.json](build/dist.json),
  [build/go-version.txt](build/go-version.txt): `go1.26.5 linux/amd64`). Archive
  `celikpanel-v0.0.0-pairaccept-acceptance-license.d1f2ad87fbeb-acceptance-license.tar.gz`,
  SHA-256 `afb3fc79ad08304c0421308f6ee5f6debe32d5851feb2ea09ee43d979bdeb78f`, unsigned local
  build, `release: false`.
- Packaging guard: refused the acceptance tree three ways
  ([build/guard-refusal.txt](build/guard-refusal.txt)) and, run separately on the final
  `.tar.gz`, the same three ways with exit 1
  ([build/guard-refusal-archive.txt](build/guard-refusal-archive.txt)).
- Archive observation ([build/archive-evidence-entries.json](build/archive-evidence-entries.json)):
  643 entries, **0** under any `evidence/` directory (pair2's archive had kill-matrix
  evidence; `8a548090` prunes it), 362 under `deploy/e2e/`.
- Product texts: `build-dist.sh` exported the commit's `web/src` with marker
  `PRODUCT-COMMIT` = `d1f2ad87… c6c1d155…`; `run-topology.sh` passed it as
  `--product-web-src` (`run.json` `web_src.source: --product-web-src`)
  ([build/product-web-src.sha256](build/product-web-src.sha256)).
- Driver files as extracted: [build/driver-files.sha256](build/driver-files.sha256) (kill-matrix
  `evidence/` entries omitted from the list; the archive contains them, unused).

## Offline tests

126/126 pass for the run composition (driver archive + `git archive d1f2ad87 web/src` at the
driver root, which the tests read as `<driver root>/web/src`):
[build/offline-tests-run-composition.log](build/offline-tests-run-composition.log). 126/126
also pass with the driver commit's own `web/src` (`git archive 8d94c8ff web/src`):
[build/offline-tests-driver-commit-pure.log](build/offline-tests-driver-commit-pure.log).
The run itself does not read the driver root's `web/src` (`--product-web-src` given).

## Harness changes, deviations and additions

- **No harness workaround.** There is no `harness-workarounds.diff`: no driver defect needed a
  fix, and S-1 is a pass-rule question, not changed.
- **Run paths only** ([build/run-path-changes.diff](build/run-path-changes.diff)): copies of
  `build-dist.sh`, `prepare-guests.sh`, `run-topology.sh` (and an unchanged `teardown.sh`) in
  `/root/cp-pair3/scripts-run/` with the hard-coded `/var/tmp/cp-pair-accept/dist/` and
  `/var/tmp/cp-pair-accept/id_ed25519` replaced by `/var/tmp/cp-pair3-dist/` and
  `/var/tmp/cp-pair3-key/id_ed25519`, so that this run created only `cp-pair3*` paths. Work
  root `/var/tmp/cp-pair3-work` (fixture `init-root`; the two locked images hard-linked from
  `/var/tmp/cp-v3n28/images` and checked by `fixture.py verify-images`; nothing downloaded),
  evidence root `/var/tmp/cp-pair3-evidence`, `CELIKPANEL_REPO=/root/cp-pair3/driver` for the
  run scripts. Labels `p3t3`, `p3t2`, `p3t1`. Driver flags: defaults plus
  `--license-mode acceptance-fixture`.
- **Read-only watcher outside the driver** ([build/watch-catalog.sh](build/watch-catalog.sh),
  [build/pdnsdb.py](build/pdnsdb.py)): during each run, over the fixture's SSH key and known
  hosts (`StrictHostKeyChecking=yes`), the driver's `guest_probe.py catalog` on the secondary
  every ~10 s (catalog AXFR from the primary), and in t3 up to 3 read-only SQLite opens of the
  primary's PowerDNS database. It added AXFR requests from 192.0.2.11 that the PowerDNS log
  does not distinguish from BIND's own transfers. Lines whose value was an SSH error (guest
  restarting) were wrapped as `{"raw": …}` when copied. Further read-only diagnosis after each
  run: `t*/obs/*-rndc-readonly.txt`, `t3-pdns-bind/obs/debian13-pdns-catalog-after.txt`.
- Cells were stopped with `fixture.py stop` (QMP quit; overlays kept, no teardown):
  `t*/obs/cell-stop.log`.

## Wall time (UTC, 2026-09-30)

Host check 00:02:02; web build 00:03:13–00:03:36; host setup 00:04:02–00:04:16; archive
00:04:46–00:05:31; offline tests to 00:05:42. t3 prepare 00:06:23, run 00:07:24–00:11:03;
t2 prepare 00:14:09, run 00:15:10–00:19:08; t1 prepare 00:19:47, run 00:20:48–00:24:32.
About 35 minutes to the last run; runner logs in [build/runner-logs/](build/runner-logs/).
Host before the run: 15697 MiB memory (14399 available), 818 GB free disk, 16 CPUs, KVM, no
QEMU process ([build/host.txt](build/host.txt)).

## License fixture

Every Panel reported `state: active`, `license_kind: acceptance_fixture`, label "ACCEPTANCE
FIXTURE — NOT FOR PRODUCTION", `license_service: "not contacted: acceptance test build"`,
`acceptance_guest: verified`, its own cell and node (`result.json` `license_status`). Journal
check: 0 lines naming `celikpanel.net` or the refusing transport on all six Panels. The
in-process refused-attempt counter is not exposed and was not read.

## What remains on the host

Stopped cells (overlays kept, no QEMU process running) under `/var/tmp/cp-pair3-work/cells/`
for `pair-accept__pdns-debian13__bind-arch__p3t3`, `pair-accept__bind-arch__pdns-debian13__p3t2`
and `pair-accept__bind-debian13__bind-arch__p3t1`, with their product state at the S-1 stop.
Also `/var/tmp/cp-pair3-work/images` (hard links), `/var/tmp/cp-pair3-dist/`,
`/var/tmp/cp-pair3-evidence/`, `/var/tmp/cp-pair3-key/` and `/root/cp-pair3/` (clone, driver,
run scripts, logs, test copies, evidence pack, scan self-test). Nothing outside `cp-pair3*`
was created, changed or deleted.

## Integrity and secrets

Each run directory carries the driver's `SHA256SUMS`, re-verified with `evidence.verify_sums`
on the host (no mismatch). [SHA256SUMS](SHA256SUMS) covers every file in this folder except
itself, this README included. An independent scanner copied unchanged from pair2
([build/secret-scan.py.txt](build/secret-scan.py.txt)) first flagged all nine planted sample
kinds (15 findings in 10 files; a redacted control file not flagged:
[build/secret-scan-selftest-summary.txt](build/secret-scan-selftest-summary.txt)), then found
nothing in the evidence (0 findings over 376 files,
[build/secret-scan-evidence.txt](build/secret-scan-evidence.txt)).

## What this does not prove

License behaviour is not evidenced (acceptance-fixture build; no activation, renewal, expiry or
rejection path; no license service). The archive is an unsigned local build from the
acceptance branch, not a release; the guests are disposable QEMU guests on an isolated link,
not an installed server; no Frankfurt/Boston behaviour is implied. Offline public-hostname DNS
and the panel certificate stay waiting by design (`access_dns`). One run per topology.
Because of S-1, record add/edit, zone delete, deletion proof, owner enrollment (BIND or
PowerDNS), zone re-add, the management-disabled reboot and management return were not
exercised natively in any topology. The passed steps show that both Panels accepted the
wizard plan and completed their DNS steps, that the three pairs reported the corrected D1
shape, that the fresh zero-zone PowerDNS primary completed its setup, and that one added zone
was served by both servers and listed in the catalog. The consequence of the missing RNDC key
for the product's own BIND deletion proof on Arch is a source reading, not an observation.
