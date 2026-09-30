# DNS pair product-flow acceptance, seventh native run (pair7), 2026-09-30

Exploratory. Seventh native run of the product-flow DNS pair acceptance driver with
`--license-mode acceptance-fixture` on two disposable QEMU guests (fixture `debian13`
192.0.2.10, `arch` 192.0.2.11) on the local WSL `archlinux` host. No installed server,
remote host, release or signed bundle was touched; nothing contacted celikpanel.net or any
license service; nothing was pushed, published or signed. Nothing here passes an
acceptance-register row; every `result.json` keeps `native_evidence: false`. The results below
are observations.

- **Product:** commit `2efc4de20038885294cef414178dfaf5f8235f27` (tree
  `406050cdd49af0de4b66c995aed97a1face28222`), branch `accept/pdns-primary-gate-open-10` = main line
  `d3d65353` ("Native peer proof: per-step budget; an accepted answer always reaches consume-once")
  plus "Open the fresh paired PowerDNS primary gate (acceptance candidate)". Built only from
  `git archive 2efc4de2`. First native measurement of `21a84211` (PowerDNS daemon re-stamp admitted
  at any point; composite owner-edit code) and `d3d65353` (per-step proof budget, fresh consume
  context, `dns_peer_proof_timeout`, new Agent log lines).
- **Driver:** `git archive d3d65353042c97d59764041d1893693e3ae7eb86 deploy/e2e/dns-pair-acceptance
  deploy/e2e/dns-kill-matrix` (tree `272b61fa1144b110583ee7281799cf9b0feefa9d`), extracted to
  `/root/cp-pair7/driver`. **No driver file was patched; no pass or redaction rule was changed.**
  There is no `harness-workarounds.diff`.
- **Repository:** branch `feat/dns-artifact-separation`, `HEAD` `d3d65353` at start and at the end.
  The only write to the repository is this folder.

## Result: all three topologies completed every step

| Dir | Topology (primary / secondary) | `overall` | Driver run (UTC) |
|---|---|---|---|
| `t1-bind-bind/p7t1-20260930t081145z` | BIND debian13 / BIND arch | **passed** | 08:11:44–08:18:26 |
| `t2-bind-pdns/p7t2-20260930t082106z` | BIND arch / PowerDNS debian13 | **passed** | 08:21:05–08:27:28 |
| `t3-pdns-bind/p7t3-20260930t082934z` | PowerDNS debian13 / BIND arch | **passed** | 08:29:33–08:35:20 |

`overall_cause` null and `failure_causes` empty everywhere. One run per topology, order t1, t2, t3;
no re-run. Each cell was stopped after its read-only collection (overlays kept).

**P6-1 does not recur.** In every topology the retry after owner enrollment returned **200
`deleted`**; the ledger job ended `succeeded` at attempt 3; the challenge journal file
(`dns-peer-challenge-v1.json` / `pdns-peer-challenge-v1.json`) is absent afterwards and no
hidden stage/retired file is left in `/var/lib/celikpanel-agent-private`; the Agent logged
"outcome verified". No `dns_peer_journal_unknown`, `dns_peer_proof_timeout`,
`dns_peer_owner_edit_unknown[:*]`, `dns_peer_native_unknown`, `dns_peer_proof_internal` or "DNS peer
proof could not run" in any Agent journal (all boots, both nodes) or ledger file
(`t*/obs/*-agent-proof-code-check.txt`).

**The native proof now runs past the old 15 s bound.** Two of three totals exceed 15 s by several
seconds (t1 20.9 s, t2 23.1 s) and still verified; consume took 7.7–9.5 s by itself.

## The deletion path end to end

Every topology (same shape; t1 detail, times from `steps/15-zone-delete/api/*` Date headers,
`obs/*-agent-journal-zone-delete.txt`, `obs/*-inspector-journal-zone-delete.txt`,
`obs/zone-delete-api-summary.txt`):

1. `DELETE /api/v1/domains/1` → **202** `dns_peer_enrollment_required`, stage `dns_cleanup`; saved
   status the same. The Agent logged "bind|pdns zone publication remains pending … (check=
   peer_native_zone) …" + `dns_peer_enrollment_required`, then about 2 s later "DNS zone V3 recovery
   remains pending …" + `dns_peer_enrollment_required` (the Panel's deletion path recovery; Panel:
   "DNS zone V3 paired propagation recovery is deferred").
2. Owner enrollment exactly as the text says (`owner-enrollment-transcript.json`; t2 with `--engine
   pdns` on every command and `--catalog-account` read read-only from the secondary's CONSUMER row).
   Host-key digest agreed three ways (CLI, `/etc/ssh/ssh_host_ed25519_key.pub`, fixture
   known-hosts), e.g. t1 `00164976…9827`.
3. The same `DELETE` once more → **200** `{"domain": "pair-accept.test", "status": "deleted"}`.
   During it: SSH inspection by `celikpeer` on the secondary (sudo `celikpanel-bind-peer-inspect` /
   `celikpanel-pdns-peer-inspect`), on BIND with `zonestatus` control-channel commands and two
   loopback catalog AXFRs; then the Agent's inspector-answer and step-times lines; the Panel lists no
   domain; both servers answer `pair-accept.test` SOA with REFUSED (aa=False) over UDP/TCP from both
   guests; native absence on both; the primary's catalog has no member.

| | t1 BIND deb / BIND arch | t2 BIND arch / PowerDNS deb | t3 PowerDNS deb / BIND arch |
|---|---|---|---|
| first 202 (`dns_peer_enrollment_required`), request time | 08:16:14 (7.20 s) | 08:25:15 (7.11 s) | 08:33:12 (7.06 s) |
| retry answered 200 `deleted` (request time) | 08:16:42 (**24.07 s**) | 08:25:44 (**25.47 s**) | 08:33:36 (**19.90 s**) |
| SSH inspection on the secondary | 08:16:30 | 08:25:30 | 08:33:24–25 |
| Agent "inspector answer … accepted" | 08:16:41 | 08:25:42 | 08:33:34 |
| Agent "step times … outcome verified" | 08:16:42 | 08:25:44 | 08:33:35 |
| ledger job (attempt, status, finished) | 3, `succeeded`, 08:16:42.80 | 3, `succeeded`, 08:25:44.58 | 3, `succeeded`, 08:33:35.92 |
| challenge journal after | absent | absent | absent |
| `zone-delete` step | passed 39.8 s | passed 40.7 s | passed 36.0 s |

The driver retries within seconds of the first 202, so no PowerDNS re-stamp could fall between
pending and retry. t3's catalog re-stamps (`obs/catalog-watch.jsonl`, primary database rows): new
`CATALOG-HASH` at 08:32:03 (zero members) and 08:33:03 (`{pair-accept.test}`, after the add); none
between the first DELETE and the retry.

## New Agent log lines (verbatim, primary Agent)

- t1: `2026/09/30 08:16:41 DNS peer inspector answer for pair-accept.test (BIND secondary): catalog_state=transferred member_state=absent native_state=unloaded; accepted`
- t1: `2026/09/30 08:16:42 DNS peer proof for pair-accept.test (BIND secondary) step times: prepare=1.64s challenge_write=6.654s exchange=572ms post_inspection=3.767s consume=8.283s; total 20.916s; outcome verified`
- t2: `2026/09/30 08:25:42 DNS peer inspector answer for pair-accept.test (PowerDNS secondary): catalog_state=transferred member_state=absent native_state=unloaded; accepted`
- t2: `2026/09/30 08:25:44 DNS peer proof for pair-accept.test (PowerDNS secondary) step times: prepare=1.581s challenge_write=7.415s exchange=414ms post_inspection=4.217s consume=9.458s; total 23.084s; outcome verified`
- t3: `2026/09/30 08:33:34 DNS peer inspector answer for pair-accept.test (BIND secondary): catalog_state=transferred member_state=absent native_state=unloaded; accepted`
- t3: `2026/09/30 08:33:35 DNS peer proof for pair-accept.test (BIND secondary) step times: prepare=1.265s challenge_write=4.967s exchange=538ms post_inspection=2.911s consume=7.692s; total 17.373s; outcome verified`

Counts over all boots: 1 "step times" and 1 "inspector answer" on each primary, 0 on each
secondary; **0 "stopped at step"**, **0 "admitted the PowerDNS daemon's re-stamp"**, **0 "observed
different evidence"** anywhere. t2's line is the first native record of the `pdns-peer-inspect`
outcome fields (pair6 had only inferred them).

Measured against the bounds in `docs/DNS-ENGINE-ARTIFACT.md` ("Native peer proof per-step budget"):
`challenge_write` 4.97–7.42 s (bound 12 s; pair6 inference 3.5–4.1 s), `exchange` 0.41–0.57 s
(10 s), `post_inspection` 2.91–4.22 s (10 s), `consume` 7.69–9.46 s (20 s; the doc's estimate was
3–4 s), `prepare` 1.27–1.64 s (8 s), totals 17.4–23.1 s (60 s).

## Per-topology step verdicts (from `result.json`; seconds from `started_at`/`finished_at`)

| # | Step | t1 BIND deb / BIND arch | t2 BIND arch / PowerDNS deb | t3 PowerDNS deb / BIND arch |
|---|---|---|---|---|
| 00 | preflight | passed 2.7 | passed 2.4 | passed 2.4 |
| 01 | install-primary | passed 60.1 | passed 140.5 (incl. owner restart, arch) | passed 28.2 |
| 02 | install-secondary | passed 109.6 (incl. owner restart, arch) | passed 24.4 | passed 103.8 (incl. owner restart, arch) |
| 03–04 | login-* | passed 0.7 / 0.4 | passed 0.5 / 0.5 | passed 0.5 / 0.4 |
| 05–06 | license-* | passed 0.4 / 0.4 | passed 0.4 / 0.4 | passed 0.4 / 0.3 |
| 07 | setup-review-primary | passed 1.1 | passed 1.2 | passed 1.0 (no gate blocker) |
| 08 | setup-start-primary | passed 28.0 | passed 16.1 | passed 18.8 |
| 09 | setup-review-secondary | passed 1.2 | passed 1.0 | passed 1.2 |
| 10 | setup-start-secondary | passed 19.0 | passed 19.0 | passed 16.0 |
| 11 | pair-ready | passed 8.4 | passed 11.1 | passed 8.5 |
| 12 | zone-add | passed 10.8 | passed 9.1 | passed 10.4 |
| 13 | record-add | passed 6.7 | passed 5.1 | passed 6.6 |
| 14 | record-edit (`ui-replace`) | passed 11.8 | passed 9.8 | passed 11.6 |
| 15 | zone-delete | passed 39.8 | passed 40.7 | passed 36.0 |
| 16 | zone-readd | passed 10.2 | passed 9.7 | passed 9.9 |
| 17 | independence-reboot | passed 77.6 | passed 78.5 | passed 79.5 |
| 18 | management-return | passed 8.4 | passed 7.5 | passed 7.6 |
| 19 | collect | passed 3.7 | passed 4.9 | passed 3.4 |

Step checks behind the later rows (per `result.json`): `zone-delete` `delete_first_outcome`
pending `dns_peer_enrollment_required`, `delete_final_outcome` 200 `deleted`, `dns_absent` passed at
the first attempt (all eight observations REFUSED aa=False), `native_delete_*`, `catalog_delete`,
`host_key_review`, `owner_enrollment` (t2 also `pdns_catalog_consumer_account`); `zone-readd`
`domain_listed`, `dns_readd`, `native_readd_*`, `catalog_readd`; `independence-reboot` `boot_ids`
(new boot ID on both guests), `dns_management-absent`, `native_management-absent_*`,
`catalog_management-absent`; `management-return` `pair_readiness_return_primary/secondary`. Owner
steps (each topology): the install.sh "RESTART THIS SERVER NOW" reboot of the arch guest (kernel
`7.1.8-arch1-3`) and the enrollment. Findings (all topologies): `ui-record-edit-is-delete-add` and
`setup-not-complete-offline-primary/secondary` (setup waits at `access_dns` on the isolated link,
expected offline). No `license_required` observation, no open-ended unknown, no
contradictory-peer-start finding.

## t3 extra experiment (outside the driver's verdicts)

Done only because t3 completed every step; on the still-running t3 guests after the driver finished
and after t3's read-only collection; files in `t3-pdns-bind/extra-experiment/`
(`extra-runner.log`, `api.jsonl`, journals, native/catalog probes, ledger). Not a driver step and
not a verdict.

- **Owner step to authenticate.** The driver's administrator password exists only in the driver's
  memory, so a separate administrator `p7extra` was created on the primary with the product's own
  CLI, as `install.sh` does: `setpriv --reuid=999 --regid=989 --clear-groups -- /bin/sh -c 'umask
  077; exec "$@"' celikpanel-install /opt/celikpanel/bin/panel --create-admin
  --admin-credentials-file=-` (rc 0, "Created administrator / Yönetici oluşturuldu."). The random
  password was generated on the guest, kept root-only, never printed or copied off the guest, and
  the file was removed at the end (the account stays on the stopped guest).
- **Create** (`POST /api/v1/domains/create {"domain": "extra-accept.test", "project_type": "dnsonly",
  "ssl_type": "none"}`, 08:36:20–25): 200, `DomainID` 3; the secondary served the catalog with
  `extra-accept.test.` and `pair-accept.test.` at serial 1790757186 at 08:36:26.
- **Delete** (`DELETE /api/v1/domains/3`, 08:36:27): **200 `deleted` directly, 23.86 s**; the
  deletion status then 404 "domain not found". The enrollment from the driver's zone-delete step was
  already configured, so the Agent ran the native proof in the first attempt:
  `2026/09/30 08:36:49 DNS peer inspector answer for extra-accept.test (BIND secondary): catalog_state=transferred member_state=absent native_state=unloaded; accepted` and
  `2026/09/30 08:36:50 DNS peer proof for extra-accept.test (BIND secondary) step times: prepare=1.275s challenge_write=6.156s exchange=545ms post_inspection=3.41s consume=7.485s; total 18.871s; outcome verified`.
- **Therefore the planned delayed retry did not happen:** there was no pending state, no 90 s wait,
  no retry, and no "admitted the PowerDNS daemon's re-stamp" line. No `CATALOG-HASH` line was
  logged in the experiment window (08:36:18–08:36:52), and none after it up to 08:37:20
  (`restamp-after-extra.txt`: the only lines of the whole run are 08:32:03 and 08:33:03; add and
  delete returned the member set to `{pair-accept.test}`, equal to the stored hash). The re-stamp
  path of `21a84211` is **not** exercised by this experiment.

## Guidance (D-024), verbatim

Setup states, same as pair6 in every topology (full texts in each `result.json` and
`obs/guidance-summary.txt`): keys `setup.guide.primary` + `setup.guide.startSecondary`,
`setup.guide.component`, `setup.guide.secondary` + `setup.guide.startPrimary`,
`server_setup_access_dns_required` on both roles, `setup.guide.firewall` on the secondary in t2
(debian13) and t3 (arch). Observation (not a driver finding): in **t1** a pair-ready setup read on the
arch secondary showed `setup.guide.unknown` once, "No more specific requirement has been confirmed
for this step. Check the current status and any reported error below; waiting alone may not resolve a
missing requirement." | "Bu adım için daha belirli bir gereksinim henüz doğrulanmadı. Aşağıdaki
durumu ve bildirilen hatayı kontrol edin; yalnızca beklemek eksik bir gereksinimi gidermeyebilir."
(the driver rated the read actionable, class none; it was followed by `server_setup_access_dns_required`).

Deletion (EN | TR as the web shows them), all three topologies, first DELETE and saved status,
`dns_peer_enrollment_required` (actor "primary administrator together with the secondary owner"):
"The DNS change is saved on this server, but the secondary has not been shown to have removed the
zone: no inspection access is set up between the two servers. The administrators of both servers set
it up once with the dns-peer-enroll owner tool (add --engine pdns to each command when the secondary
runs PowerDNS): primary-prepare here, secondary-install and secondary-host-key on the secondary, then
primary-activate here. After that, use “Retry this deletion” to retry the same publication; it
continues from where it stopped. Nothing retries by itself." | "DNS değişikliği bu sunucuda
kaydedildi, ancak ikincil sunucunun bölgeyi kaldırdığı doğrulanamadı: iki sunucu arasında inceleme
erişimi kurulmamış. İki sunucunun yöneticileri bunu dns-peer-enroll sahip aracıyla bir kez kurar
(ikincil sunucu PowerDNS çalıştırıyorsa her komuta --engine pdns ekleyin): burada primary-prepare,
ikincil sunucuda secondary-install ve secondary-host-key, sonra burada primary-activate. Ardından “Bu
silme işlemini yeniden dene” ile aynı yayını yeniden deneyin; işlem kaldığı yerden sürer.
Kendiliğinden yeniden denenmez." Server `message` (English fallback): "… After that, retry the same
publication; it continues from where it stopped. Nothing retries by itself." No other pending code
occurred, so the new texts for `dns_peer_proof_timeout`, `dns_peer_journal_unknown` (revised) and
`domains.peerOwnerEditDetail.<check>` were not shown natively; no `detail` field appeared.

## Configuration and receipt observations (read-only, after each run)

- **rndc key** (metadata only; content never read into any file): arch `/etc/rndc.key` `root:named
  0640`, companion `provenance: product_created` (t1 born 08:15:17, t2 08:24:02, t3 08:32:18);
  t1 debian13 `/etc/bind/rndc.key` `bind:bind 0640`, `owner_or_package_provided (key_present)`. Files
  `t*/obs/<node>-rndc-key.txt`.
- **Catalog stanzas, effective options, generation receipts, transfer policy:** `obs/*-bind-catalog-stanzas.txt`,
  `*-named-effective-options.txt`, `*-bind-transfer-policy.txt`; the PowerDNS daemon identity and
  configuration in `obs/debian13-pdns-daemon-group.txt` (t2, t3).
- **PowerDNS notify (t3 primary)** (`t3-pdns-bind/obs/notify-counts.txt`): 13 "Notification request to
  host 192.0.2.11:53 … received from operator", 0 spurious, 0 `:0`, **1** "Notification for
  catalog-c000020a.celikpanel.invalid to 192.0.2.11:53 failed after retries" at 08:32:37 (during the
  secondary's setup; pair6 had one at the same point). Arch secondary: 16 "received notify" (9
  catalog, 7 member).
- **Mail stage:** each deletion attempt logged "mail runtime cleanup skipped: no mail runtime for this
  domain on this server".

## Builds

- `web/dist`: Windows, `git archive 2efc4de2 web` into the session scratchpad, `web/node_modules`
  junction to the repository's (lockfile SHA-256 `8dab8b6b…6d26` identical to the commit's), `npm run
  build` (node v24.18.0, npm 11.16.0), 08:03:16–08:03:34Z, **exit 0** ([build/web-build.log](build/web-build.log)).
  106 files; the host copy equals the Windows build after normalising `sha256sum`'s binary marker and
  sort order, and equals the `web/dist` in the dist ([build/web-dist-windows-build.sha256](build/web-dist-windows-build.sha256),
  [build/web-dist.sha256](build/web-dist.sha256)).
- Run host: `/root/cp-pair7/repo` = `git clone --no-checkout` of the repository plus that `web/dist`;
  `build-dist.sh --acceptance-license 2efc4de2…` with `CELIKPANEL_REPO` pointing at it,
  08:05:20–08:06:28Z ([build/build-dist.log](build/build-dist.log), [build/build-dist-make.log](build/build-dist-make.log),
  [build/dist.json](build/dist.json), [build/go-version.txt](build/go-version.txt): `go1.26.5 linux/amd64`).
  Archive `celikpanel-v0.0.0-pairaccept-acceptance-license.2efc4de20038-acceptance-license.tar.gz`,
  SHA-256 `cad46975d87bb3c9c5ce7a1b52803eacf087ae256e868f8ef63ec54618ca6c32`, unsigned local build,
  `release: false`.
- Packaging guard: refused the acceptance tree three ways ([build/guard-refusal.txt](build/guard-refusal.txt):
  "bin/panel was built with -tags acceptance_license (embedded Go build settings)", "bin/panel contains
  the acceptance fixture license", "… (go version -m)") and the final `.tar.gz` the same way
  ([build/guard-refusal-archive.txt](build/guard-refusal-archive.txt)).
- Archive observation ([build/archive-evidence-entries.json](build/archive-evidence-entries.json)): 645
  entries, 0 under any `evidence/` directory, 362 under `deploy/e2e/`.
- Product texts: `build-dist.sh` exported the commit's `web/src` with marker `PRODUCT-COMMIT` =
  `2efc4de2…`; `run-topology.sh` passed it as `--product-web-src` ([build/product-web-src.sha256](build/product-web-src.sha256)).
- Driver files as extracted: [build/driver-files.sha256](build/driver-files.sha256) (evidence paths omitted).

## Offline tests

**131/131** pass for the run composition (driver archive + `git archive 2efc4de2 web/src`):
[build/offline-tests-run-composition.log](build/offline-tests-run-composition.log). 131/131 also with the
driver commit's own `web/src` (`git archive d3d65353 web/src`):
[build/offline-tests-driver-commit-pure.log](build/offline-tests-driver-commit-pure.log).

## Exact commands, harness changes, deviations

All host commands were LF script files in the session scratchpad run as
`MSYS_NO_PATHCONV=1 wsl.exe -d archlinux -u root -- bash /mnt/c/…/scratchpad/p7b12/<script>.sh`;
copies in [build/scripts/](build/scripts/) (`.txt`; `scan.sh` omitted because it plants
secret-shaped self-test samples; it is pair6's with paths changed). Order: `hostcheck.sh`, Windows
web build, `setup-host.sh`, `cmpweb.sh`, `build.sh` (dist + offline tests), then per topology
`launch.sh <topology> <primary> <label>` (nohup `topo.sh` = `prepare-guests.sh` + `run-topology.sh
… --license-mode acceptance-fixture`), `waitline.sh`, `launch-watch.sh` (catalog watcher), `waitfor.sh`/`waitend.sh`,
`res.sh`, `delinfo.sh`, read-only collection (`tcoll.sh` → `ro-collect.sh`, `coll-extra.sh`,
`diag-bind-transfer.sh`, `diag-options.sh`, `ledgerjob.sh`, `counts.sh`; `t1more.sh`/`t2more.sh`/`t3more.sh`
→ `journaldiag.sh`, `guid.sh`), `stopcell.sh`; t3 additionally `extra-launch.sh` → `extra.sh` (with
`extra_guest.py` on the primary), `extra-wait.sh`, `t3final.sh`. Topologies and labels:
`bind/bind debian13 p7t1`, `bind-primary/pdns-secondary auto p7t2`, `pdns-primary/bind-secondary auto p7t3`.
Driver flags: defaults plus `--license-mode acceptance-fixture`.

- **No harness workaround**; no driver defect found; no pass rule changed.
- **Run paths only** ([build/run-path-changes.diff](build/run-path-changes.diff)): as pair6, with
  `/var/tmp/cp-pair7-dist/` and `/var/tmp/cp-pair7-key/id_ed25519`. Work root
  `/var/tmp/cp-pair7-work` (images hard-linked from `/var/tmp/cp-v3n28/images`, verified; nothing
  downloaded), evidence root `/var/tmp/cp-pair7-evidence`, `CELIKPANEL_REPO=/root/cp-pair7/driver`.
- **Additions to pair6's collection:** `tcoll.sh` counts and lists the new Agent strings (step times,
  inspector answer, stopped at step, admitted re-stamp, observed different evidence) and the codes
  `dns_peer_journal_unknown`, `dns_peer_proof_timeout`, `dns_peer_native_unknown`;
  `obs/zone-delete-api-summary.txt` (request timing and bodies of the delete step);
  `obs/guidance-summary.txt`; `obs/collect-console.txt`.
- `obs/cell-stop.log` of t2 and t3 also contains the read-only output that ran in the same script
  before the stop (Agent-private listing; t3: re-stamp lines and ledger tail).
- The t3 extra experiment is the only mutation outside the driver (one administrator account created
  with the product CLI, one domain created and deleted through the Panel API) and is labelled as such.

## Wall time (UTC, 2026-09-30)

Host check 08:02:26; web build 08:03:16–08:03:34; host setup 08:04:52–08:05:06; archive
08:05:20–08:06:28; offline tests to 08:06:44 (batch 12's build ran 08:08:25–08:09:04 before any guest
started). t1 prepare 08:10:35, run 08:11:44–08:18:26, stopped 08:19:57; t2 prepare 08:20:01, run
08:21:05–08:27:28, stopped 08:28:19; t3 prepare 08:28:30, run 08:29:33–08:35:20, extra experiment
08:36:18–08:36:53, stopped 08:37:21. About 35 minutes from host check to the last stop. Runner logs:
[build/runner-logs/](build/runner-logs/). Host before the run: 15697 MiB memory (15059 available),
784 GB free disk, 16 CPUs, KVM, no QEMU process ([build/host.txt](build/host.txt),
[build/hostcheck-summary.txt](build/hostcheck-summary.txt)).

## License fixture

Every Panel reported the acceptance fixture license (`result.json` `license_status`). Journal check:
0 lines naming `celikpanel.net` or the refusing transport on all six Panels. The in-process
refused-attempt counter is not exposed and was not read.

## What remains on the host

Stopped cells (overlays kept, no QEMU process running) under `/var/tmp/cp-pair7-work/cells/`:
`d1e5620b44fba28a45b2a6b5` (`pair-accept__bind-debian13__bind-arch__p7t1`), `b6c70a695e33217c837e27f2`
(`pair-accept__bind-arch__pdns-debian13__p7t2`), `2bc5bf644a4c57a45b802e6a` (`pair-accept__pdns-debian13__bind-arch__p7t3`).
Also `/var/tmp/cp-pair7-work/images` (hard links), `/var/tmp/cp-pair7-dist/`, `/var/tmp/cp-pair7-evidence/`,
`/var/tmp/cp-pair7-key/` and `/root/cp-pair7/` (clone, driver, run scripts, logs, obs, test copies,
evidence pack, scan self-test, host-only secret digests). Nothing outside `cp-pair7*` (and batch 12's
`cp-b12*`) was created, changed or deleted on the host.

## Integrity and secrets

Each run directory carries the driver's `SHA256SUMS`, re-verified with `evidence.verify_sums` (no
mismatch). [SHA256SUMS](SHA256SUMS) covers every file in this folder except itself, this README
included. The independent scanner (pair6's) first flagged all planted sample kinds (15 findings in
10 files; a redacted control file not flagged) and then found **0 findings over 735 files** in the
pack ([build/secret-scan-evidence.txt](build/secret-scan-evidence.txt),
[build/secret-scan-selftest-summary.txt](build/secret-scan-selftest-summary.txt)). **rndc key
content:** [build/rndc-key-content-scan.txt](build/rndc-key-content-scan.txt) hashed 4915
base64-alphabet tokens in 736 files against the four guest-side secret digests (t1 arch, t1
debian13, t2 arch, t3 arch): **0 matches**; its self-test found a planted secret only in the planted
file. The companion records' `sha256` is the product's hash of the key file, not key content. The
enrollment transcripts contain the inspector's SSH **public** key. The extra experiment's password
never left the guest. The longest path is 187 characters from the repository root. `sums.sh` re-ran both scans over this folder with the README: see the last lines of
[build/final-scan.txt](build/final-scan.txt).

## What this does not prove

License behaviour (acceptance-fixture build). The archive is an unsigned local build from the
acceptance branch, not a release; the guests are disposable QEMU guests on an isolated link, not an
installed server; nothing about Frankfurt/Boston is implied. One run per topology. The retry came
within seconds of the first 202 in every topology, so a PowerDNS daemon re-stamp between pending
and retry (or inside the retried attempt) was **not** exercised, neither by the driver nor by the t3
extra experiment (whose delete completed in the first attempt); the "admitted the PowerDNS daemon's
re-stamp" path, `dns_peer_proof_timeout`, a "stopped at step" line, the composite owner-edit code
and the revised `dns_peer_journal_unknown` text did not occur. The step times are single samples;
consume at 7.7–9.5 s (bound 20 s) and challenge write at up to 7.4 s (bound 12 s) show the margin on
this host only. The management-disabled reboot is orderly (no power loss). Setup completion (public
hostname, certificate) cannot happen offline. Nothing here closes P0.4/P0.5 or passes a register row.
