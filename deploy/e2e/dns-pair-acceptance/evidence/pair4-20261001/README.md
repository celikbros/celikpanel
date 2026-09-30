# DNS pair product-flow acceptance, fourth native run (pair4), 2026-09-30

Exploratory. Fourth native run of the product-flow DNS pair acceptance driver with
`--license-mode acceptance-fixture` on two disposable QEMU guests (fixture `debian13`
192.0.2.10, `arch` 192.0.2.11) on the local WSL `archlinux` host. No installed server,
remote host, release or signed bundle was touched; nothing contacted celikpanel.net or any
license service; nothing was pushed, published or signed. Nothing here passes an
acceptance-register row; every `result.json` keeps `native_evidence: false`. The results below
are observations.

- **Product:** commit `96657d675efe05ed5083a1930382e551756544cb` (tree
  `b92ae1e15c4f29e315e13ad31c392f5eb5e498dd`), local branch `accept/pdns-primary-gate-open-5`
  = main line `80bb4353` ("BIND: create the rndc key at install when the package did not;
  typed reason when rndc is unusable", on top of `a6d93f06` "PowerDNS operator notify carries
  an explicit port") plus "Open the fresh paired PowerDNS primary gate (acceptance candidate)".
  Built only from `git archive 96657d67` (the `gate5` worktree was not used).
- **Driver:** `git archive 92fc5eeee89fc21087489e528a0521142aad56ef deploy/e2e/dns-pair-acceptance
  deploy/e2e/dns-kill-matrix` (main line; tree `8bfc0fc300b1ca369f08a1b71e79313c852c9a7b`;
  its only non-evidence change since pair3's `8d94c8ff` adds `bind_rndc_unavailable` to the
  reviewed DNS peer reasons), extracted to `/root/cp-pair4/driver`. **No driver file was
  patched; no pass or redaction rule was changed.** There is no `harness-workarounds.diff`.
- **Repository `HEAD`** (branch `feat/dns-artifact-separation`): `92fc5eee` when the run started,
  `f0910d6e` at the end (another agent committed during the run; the product and driver used
  here are the archives above, unaffected).

## Result: all three topologies reached `zone-delete` and stopped there, on product behaviour

| Dir | Topology (primary / secondary) | `overall` | Last step reached | Stopped by (classification) |
|---|---|---|---|---|
| `t1-bind-bind/p4t1-20260930t021102z` | BIND debian13 / BIND arch | **failed** | `zone-delete` (failed, 319.9 s) | P4-1 (PRODUCT): after owner enrollment the retry stays pending `dns_peer_inspection_unknown`; the Arch secondary's named denies the inspector's loopback catalog AXFR |
| `t2-bind-pdns/p4t2-20260930t022404z` | BIND arch / PowerDNS debian13 | **failed** | `zone-delete` (failed, 0.9 s) | P4-2 (PRODUCT): deletion pending at `mail_runtime_cleanup`, "too many levels of symbolic links" on Arch's stock `/var/mail -> spool/mail`; 202 carries no reason (D-024 finding) |
| `t3-pdns-bind/p4t3-20260930t023034z` | PowerDNS debian13 / BIND arch | **failed** | `zone-delete` (failed, 320.0 s) | P4-1 (PRODUCT), identical to t1 |

`overall_cause` is `unclassified` in all three (`zone-delete` carries no `cause`; the driver
sets `cause: product` only for its open-ended-unknown rule). The classification above is this
run's, from the evidence cited below. Steps 00–14 passed in every topology: **pair3's stop S-1
is gone** (the native reader's `rndc zonestatus` now works on every product-installed Arch
BIND), and `zone-add`, `record-add` and `record-edit` passed natively for the first time in all
three topologies, including a fresh zero-zone paired **PowerDNS primary** (t3). `zone-readd`,
`independence-reboot` and `management-return` were **not run** (prerequisite chain) and prove
nothing either way; in particular no management-disabled reboot happened. `collect` passed in
all three. Per the run rules no topology was re-run and nothing on the guests was changed to get
past a stop; the cells were stopped with their state (overlays kept).

## P4-1 (t1, t3): owner enrollment completes, but the product's own BIND secondary refuses the inspector's local catalog AXFR

Classification: **PRODUCT** (the product-installed secondary's configuration contradicts the
requirement of the product's own inspector; no workaround applied).

What happened (t1; t3 is the same sequence 19 minutes later):

1. `DELETE /api/v1/domains/1` → **202**, `reason: dns_peer_enrollment_required`,
   `stage: dns_cleanup` (`t1-bind-bind/p4t1-*/steps/15-zone-delete/api/0057-…json`). The saved
   `GET …/deletion-status` returned the same reason (`0058`).
2. The driver performed the owner enrollment exactly as the text and `cmd/dns-peer-enroll/README.md`
   document (BIND secondary, so no `--engine`), all exit 0
   (`steps/15-zone-delete/owner-enrollment-transcript.json`; `<tools>` =
   `/var/backups/celikpanel/pair-accept/celikpanel-v0.0.0-pairaccept-acceptance-license.96657d675efe/dns-owner-tools`):
   ```
   debian13: sudo -n <tools>/dns-peer-enroll primary-prepare
   arch:     (reviewed public key written root-only to /root/celikpanel-primary-inspector.pub)
   arch:     sudo -n <tools>/dns-peer-enroll secondary-install --primary-ip 192.0.2.10 --peer-ip 192.0.2.11 --catalog catalog-c000020a.celikpanel.invalid --primary-public-key /root/celikpanel-primary-inspector.pub --inspector <tools>/bind-peer-inspect
   arch:     sudo -n <tools>/dns-peer-enroll secondary-host-key
   debian13: sudo -n <tools>/dns-peer-enroll primary-activate --credential-id <id> --primary-ip 192.0.2.10 --peer-ip 192.0.2.11 --catalog catalog-c000020a.celikpanel.invalid --host-key-sha256 <digest>
   debian13: sudo -n <tools>/dns-peer-enroll primary-status      -> state configured
   arch:     sudo -n <tools>/dns-peer-enroll secondary-status    -> configured, "live authentication is unproven"
   ```
   Host-key digest agreed three ways (CLI, `/etc/ssh/ssh_host_ed25519_key.pub`, fixture
   known-hosts): t1 `b549dcd3…15e0`, t3 `1279cfe0…dd0e`. Recorded as an owner step.
3. The **same** `DELETE` once more → **202**, `reason: dns_peer_inspection_unknown` (`0059`); the
   saved status kept that reason for the full 300 s of read-only polling (`0060`–`0091`, last read
   02:20:14Z). Nothing retried by itself.

Why, read-only (`t1-bind-bind/obs/arch-bind-transfer-policy.txt`,
`arch-named-effective-options.txt`; t3 the same files):

```
02:15:14 dns-arch sudo[3316]: celikpeer : ... USER=root ; COMMAND=/usr/local/libexec/celikpanel-bind-peer-inspect
02:15:14 dns-arch named[2068]: client @0x7f630b84a800 127.0.0.1#54631 (catalog-c000020a.celikpanel.invalid): zone transfer 'catalog-c000020a.celikpanel.invalid/AXFR/IN' denied
```
(t3: `02:34:50 … 127.0.0.1#54810 … denied`.) The effective secondary options
(`named-checkconf -p`) are `allow-transfer { 192.0.2.10/32; };` and the catalog stanza has no
transfer clause of its own. Source (96657d67): the inspector asks `rndc -s 127.0.0.1 zonestatus
<catalog>` and then `dig @127.0.0.1 <catalog> AXFR`, returning "local catalog AXFR is
unavailable; enroll read-only inspector transfer access" when the AXFR fails
(`internal/bindpeerinspector/native_linux.go:84-90`); the product writes the secondary's
`allow-transfer` from the paired primary only (`cmd/agent/dns_engine_bind_adopt_options.go:114-129`,
`managedBINDOptionAssignments`) and the catalog stanza without one
(`internal/binddns/pairing.go:274-286`, `appendSecondaryCatalogConfig`); `dns-peer-enroll` "does not
configure AXFR" (`cmd/dns-peer-enroll/README.md:3`) while `cmd/bind-peer-inspect/README.md:5`
requires the catalog "available through loopback AXFR". The rndc step before the AXFR works now
(named reached the AXFR). The primary Agent logged only `dns_peer_inspection_unknown`
(`obs/debian13-agent-journal-zone-delete.txt`); the inspector's own reason is not shown to either
owner.

D-024 observation (not a driver finding; the driver rates the reviewed code actionable): the text
after enrollment sends both owners to "check the pinned SSH channel and inspector access", while
the verified cause was a denied local transfer on the secondary, which neither the pending text,
the enrollment tool nor its README tells the owner to allow.

Native state while pending (driver probe as root, `steps/15-zone-delete/native-pending-*.json`):
t1 primary and secondary both `absent` (`rndc zonestatus pair-accept.test` → "no matching zone
'pair-accept.test' in any view", no zone file), catalog serial 3 with no member on both; t3
PowerDNS primary: only the PRODUCER catalog row, secondary `absent` ("no matching zone …"),
catalog serial `1790735617`, no member. The zone was therefore gone natively on both servers;
the product's peer proof of it could not complete. The driver's `dns-delete` (authoritative
absence) check was not reached.

## P4-2 (t2): DNS-only domain deletion on an Arch primary stops in mail cleanup

Classification: **PRODUCT**, plus a D-024 finding recorded by the driver.

`DELETE /api/v1/domains/1` on the Arch BIND primary → **202** `{"message": "Deletion is incomplete
but retryable. Retry this deletion.", "stage": "mail_runtime_cleanup", "status":
"deletion_pending"}` with **no `reason`** (`t2-bind-pdns/p4t2-*/steps/15-zone-delete/api/0059-…json`);
the saved status read `{"message": "Deletion is pending. The server administrator should inspect
the saved operation before retrying this same deletion.", "stage": "unknown", "status":
"unknown"}` (`0060`). Panel journal (`obs/arch-panel-journal-zone-delete.txt`):

```
02:27:43 dns-arch panel[429]: domain deletion pending for pair-accept.test at mail_runtime_cleanup: remove mail domain runtime: publish mailbox directory: open mail root for domain quarantine: too many levels of symbolic links
```

Guest (`obs/arch-mail-root-readonly.txt`): `/var/mail -> spool/mail` (owned by Arch's `filesystem`
package), `/var/mail/vhosts` absent, no mail server installed. Source (96657d67): every deletion,
DNS-only included, calls `removeDomainMailRuntimeLocked` (`cmd/panel/domain_handlers.go:752-761`;
only the later site teardown skips `dnsonly`) → Agent `quarantineMailDomainDirectory` →
`openManagedMailRoot("/var/mail/vhosts")` (`cmd/agent/mail_rpc.go:34`,
`cmd/agent/mail_domain_directory_linux.go:128-141,188-195`), which opens with `openat2`
`O_NOFOLLOW` and `secureMaildirResolve` (`mail_domain_directory_linux.go:39-59`,
`mail_directory_linux.go:13`); only `ENOENT` means "nothing to clean", so the stock symlink's
`ELOOP` fails the step. Inferred, not observed: any domain deletion on a product-installed Arch
host without a mail root stops here. The DNS deletion never started: native state stayed `present`
on both servers and the catalog still listed the member (`obs/*-native-while-pending.json`,
`obs/catalog-while-pending.json`).

D-024 (driver finding `d024-no-actionable-guidance-zone-delete`, kind `product`, both reads): the
UI shows `domains.deletionPending` for a pending state without a reason — EN "Deletion is still
pending, but its reason could not be verified. Check the status again. If it remains unknown, ask
the server administrator to inspect the saved operation before retrying." / TR "Silme işlemi hâlâ
bekliyor, ancak nedeni doğrulanamadı. Durumu yeniden kontrol edin. Belirsizlik sürerse yeniden
denemeden önce sunucu yöneticisinden kayıtlı işlemi incelemesini isteyin." No actor, no concrete
action, and the saved status degrades to `unknown`. Because this is not
`dns_peer_enrollment_required`, the driver did not enroll (PowerDNS `--engine pdns` enrollment
remains natively unexercised).

## rndc key (80bb4353): first native observation

From `t*/obs/<node>-rndc-key.txt` (metadata only; content never read into any file) and the
companion records copied from `/var/lib/celikpanel-agent-private/dns-engine-bind-rndc-key.json`
(`root:celikpanel 0600`; they hold path, provenance, hash and ids):

| Topology / node | Key | Owner, mode | Key created (birth) | Agent log | named first start | Companion `provenance` |
|---|---|---|---|---|---|---|
| t1 arch (secondary) | `/etc/rndc.key` | `root:named 0640`, no package owns it | 02:14:13.359 | 02:14:13 "BIND rndc key /etc/rndc.key is product_created" | 02:14:16 | `product_created`, sha256 `4cd95dcc…b28b` |
| t2 arch (primary) | `/etc/rndc.key` | `root:named 0640` | 02:26:31.365 | 02:26:31 "… is product_created" | 02:26:35 | `product_created`, sha256 `84841019…a73c` |
| t3 arch (secondary) | `/etc/rndc.key` | `root:named 0640` | 02:33:48.114 | 02:33:48 "… is product_created" | 02:33:51 | `product_created`, sha256 `f4a197a1…1e9c` |
| t1 debian13 (primary) | `/etc/bind/rndc.key` | `bind:bind 0640`, not a dpkg path | 02:13:52.594 (package configuration) | 02:13:55 "BIND rndc key /etc/bind/rndc.key is owner_or_package_provided (key_present)" | 02:13:59 | `owner_or_package_provided`, `basis: key_present`, no hash |

On every Arch BIND the key was created before named's first start, recorded as product-created
with a hash, and named configured its command channel from it; `rndc status` worked. Debian was
recorded as package-provided and left untouched. No `bind_rndc_unavailable` reason appeared
anywhere. The rollback rule (removal of a product-created key) was not exercised.

## PowerDNS notify (a6d93f06): t3 primary journal

`t3-pdns-bind/obs/debian13-pdns-notify-journal.txt` (all `pdns.service` lines with
notif/spurious/"failed after retries", timestamps kept) and `obs/notify-counts.txt`:

| | pair3 t3 (before, one mutation; `build/pair3-notify-baseline.txt`) | pair4 t3 (after, six mutations) |
|---|---|---|
| "Notification request to host 192.0.2.11**:53** … received from operator" | 0 (2 without port) | **11** (all with `:53`) |
| "Received spurious notify answer" | 8 | **0** |
| "… to 192.0.2.11:0 failed after retries" | 2 | **0** |
| any "failed after retries" | 2 | 0 |

BIND secondary (`obs/arch-bind-notify-journal.txt`): one "received notify" per zone per mutation
— zone add 02:34:08 (catalog; member `NOTAUTH`, the member was not loaded yet and arrived through
the catalog), record add 02:34:18, record edit delete 02:34:25 and add 02:34:29 (catalog and member
each), zone delete 02:34:35, 02:34:38 and 02:34:45 (catalog only; deletion, recovery and retry),
plus 02:33:52 at setup; 12 in all, no duplicates. The primary also logged one "Received
unsuccessful notification report for 'pair-accept.test' … Not Authoritative" (02:34:09, the same
`NOTAUTH`). t1/t2 counts (BIND primaries; PowerDNS secondary logs no incoming NOTIFY at its log
level) are in `t1-bind-bind/obs/notify-counts.txt`, `t2-bind-pdns/obs/notify-counts.txt` and
`t2-bind-pdns/obs/debian13-pdns-journal-summary.txt`.

## Guidance (D-024), verbatim

Setup states are identical to pair3 (keys `setup.guide.primary` + `setup.guide.startSecondary`,
`setup.guide.component`, `setup.guide.secondary` + `setup.guide.startPrimary`,
`server_setup_access_dns_required` for both roles, `setup.guide.firewall` on t1 arch); texts in
each `result.json` and quoted in `../pair3-20261001/README.md`. Peer-start checks: shown only
while the primary's DNS step was `running` (no finding). No `license_required` observation, no
open-ended unknown, no contradictory-peer-start finding. Each setup ended waiting at
`access_dns` (findings `setup-not-complete-offline-*`, expected offline).

Deletion states (EN | TR as the web shows them; server `message` differs only where noted):

- **t1, t3, first DELETE and saved status, `dns_peer_enrollment_required`** (actor "primary
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
  that, retry the same publication; …".)
- **t1, t3, DELETE after enrollment and saved status, `dns_peer_inspection_unknown`** (actor
  "primary administrator and secondary owner"): "The DNS change is saved, but authenticated
  secondary inspection could not complete. This server's administrator and the secondary owner
  must check the pinned SSH channel and inspector access, then retry the same publication." |
  "DNS değişikliği kaydedildi; kimliği doğrulanmış ikincil sunucu incelemesi tamamlanamadı. Bu
  sunucunun yöneticisi ve ikincil sunucunun sahibi sabitlenmiş SSH erişimini ve inceleme
  yetkisini denetleyip aynı yayını yeniden denemeli." (Server message: "… then retry this same
  publication.")
- **t2, DELETE and saved status, `mail_runtime_cleanup` / `unknown`, no reason:** see P4-2 (not
  actionable; driver finding).

## Per-topology step verdicts (from `result.json`; seconds from `started_at`/`finished_at`)

| # | Step | t1 BIND deb / BIND arch | t2 BIND arch / PowerDNS deb | t3 PowerDNS deb / BIND arch |
|---|---|---|---|---|
| 00 | preflight | passed 2.2 | passed 2.2 | passed 2.3 |
| 01 | install-primary | passed 22.1 | passed 110.4 (incl. owner restart, arch) | passed 23.6 |
| 02 | install-secondary | passed 139.4 (incl. owner restart, arch) | passed 26.1 | passed 139.6 (incl. owner restart, arch) |
| 03–04 | login-* | passed 0.4 / 0.4 | passed 0.5 / 0.5 | passed 0.4 / 0.4 |
| 05–06 | license-* | passed 0.3 / 0.3 | passed 0.3 / 0.3 | passed 0.4 / 0.3 |
| 07 | setup-review-primary | passed 0.9 | passed 1.2 | passed 0.9 (no gate blocker) |
| 08 | setup-start-primary | passed 18.8 | passed 16.0 | passed 18.9 |
| 09 | setup-review-secondary | passed 1.2 | passed 1.5 | passed 1.2 |
| 10 | setup-start-secondary | passed 12.9 | passed 19.0 | passed 15.9 |
| 11 | pair-ready | passed 8.1 | passed 11.5 | passed 7.0 |
| 12 | zone-add | passed 9.3 | passed 11.5 | passed 9.7 |
| 13 | record-add | passed 6.1 | passed 6.6 | passed 6.1 |
| 14 | record-edit (`ui-replace`) | passed 10.1 | passed 10.4 | passed 10.4 |
| 15 | zone-delete | **failed** 319.9 | **failed** 0.9 | **failed** 320.0 |
| 16–18 | zone-readd, independence-reboot, management-return | not-run | not-run | not-run |
| 19 | collect | passed 3.3 | passed 3.8 | passed 3.4 |

`zone-delete` reasons verbatim: t1 and t3 "deletion stayed pending after owner enrollment and one
retry"; t2 "D-024: a blocked/pending state carried no actionable guidance: [{'context': 'domain
delete', 'code': 'mail_runtime_cleanup', …}, {'context': 'domain delete: saved deletion status',
'code': 'unknown', …}]". Inside `zone-add`, native state was `present` on both servers in every
topology, including each Arch BIND (`rndc zonestatus` type primary/secondary, serial
`2026093001`) and the PowerDNS side (row present; t2 secondary rows CONSUMER catalog + SLAVE zone).
Owner steps (`result.json` `owner_steps`): the install.sh "RESTART THIS SERVER NOW" reboot of the
arch guest in each topology (kernel `7.1.8-arch1-3`, fixture reboot, new boot ID), and in t1/t3 the
enrollment above.

## Builds

- `web/dist`: Windows, `git archive 96657d67 web` into the session scratchpad, `web/node_modules`
  junction to the repository's (lockfile SHA-256 `8DAB8B6B…6D26` identical to the commit's), `npm
  run build` (node v24.18.0, npm 11.16.0), 02:05:20–02:05:35Z, **exit 0** (`tsc`, `vite build`,
  recovery worker, bundle budget all passed; [build/web-build.log](build/web-build.log)). 106
  files; the host copy equals the Windows build after normalising `sha256sum`'s binary marker and
  sort order ([build/web-dist-windows-build.sha256](build/web-dist-windows-build.sha256),
  [build/web-dist.sha256](build/web-dist.sha256)).
- Run host: `/root/cp-pair4/repo` = `git clone --no-checkout` of the repository plus that
  `web/dist`; `build-dist.sh --acceptance-license 96657d67…` with `CELIKPANEL_REPO` pointing at it,
  02:06:47–02:07:31Z ([build/build-dist.log](build/build-dist.log),
  [build/build-dist-make.log](build/build-dist-make.log), [build/dist.json](build/dist.json),
  [build/go-version.txt](build/go-version.txt): `go1.26.5 linux/amd64`). Archive
  `celikpanel-v0.0.0-pairaccept-acceptance-license.96657d675efe-acceptance-license.tar.gz`,
  SHA-256 `479b08cd5a2fa2b07aa8bb152d6affd016294a2bc935670bd14106b063255de6`, unsigned local
  build, `release: false`.
- Packaging guard: refused the acceptance tree three ways ([build/guard-refusal.txt](build/guard-refusal.txt))
  and, run separately on the final `.tar.gz`, the same three ways with exit 1
  ([build/guard-refusal-archive.txt](build/guard-refusal-archive.txt)).
- Archive observation ([build/archive-evidence-entries.json](build/archive-evidence-entries.json)):
  645 entries, 0 under any `evidence/` directory, 362 under `deploy/e2e/`.
- Product texts: `build-dist.sh` exported the commit's `web/src` with marker `PRODUCT-COMMIT` =
  `96657d67…`; `run-topology.sh` passed it as `--product-web-src`
  ([build/product-web-src.sha256](build/product-web-src.sha256)).
- Driver files as extracted: [build/driver-files.sha256](build/driver-files.sha256) (evidence
  entries omitted from the list).

## Offline tests

126/126 pass for the run composition (driver archive + `git archive 96657d67 web/src` at the
driver root, read by the tests as `<driver root>/web/src`, the same `web/src` the run passes as
`--product-web-src`): [build/offline-tests-run-composition.log](build/offline-tests-run-composition.log).
126/126 also pass with the driver commit's own `web/src` (`git archive 92fc5eee web/src`):
[build/offline-tests-driver-commit-pure.log](build/offline-tests-driver-commit-pure.log).

## Exact commands, harness changes, deviations and additions

All host commands were LF script files in the session scratchpad run as
`MSYS_NO_PATHCONV=1 wsl.exe -d archlinux -u root -- bash /mnt/c/…/scratchpad/pair4/<script>.sh`;
copies are in [build/scripts/](build/scripts/) (`.txt`). Order: `hostcheck.sh`, Windows web build,
`setup-host.sh` (clone, driver extraction, run-path copies, key, fixture `init-root`, image hard
links, `verify-images`), `build.sh` (dist + offline tests), then per topology `launch.sh
<topology> <primary> <label>` (nohup `topo.sh` = `prepare-guests.sh` + `run-topology.sh … --license-mode
acceptance-fixture`), `launch-watch.sh` (catalog watcher), read-only collection, `stopcell.sh`.
Topologies and labels: `bind/bind debian13 p4t1`, `bind-primary/pdns-secondary auto p4t2`,
`pdns-primary/bind-secondary auto p4t3`. Driver flags: defaults plus
`--license-mode acceptance-fixture`.

- **No harness workaround**; no driver defect was found. No pass rule was changed.
- **Run paths only** ([build/run-path-changes.diff](build/run-path-changes.diff)): copies of
  `build-dist.sh`, `prepare-guests.sh`, `run-topology.sh` (and an unchanged `teardown.sh`) with
  `/var/tmp/cp-pair-accept/dist/` and `/var/tmp/cp-pair-accept/id_ed25519` replaced by
  `/var/tmp/cp-pair4-dist/` and `/var/tmp/cp-pair4-key/id_ed25519`. Work root
  `/var/tmp/cp-pair4-work` (images hard-linked from `/var/tmp/cp-v3n28/images`, verified; nothing
  downloaded), evidence root `/var/tmp/cp-pair4-evidence`, `CELIKPANEL_REPO=/root/cp-pair4/driver`.
- **Read-only catalog watcher** outside the driver, as in pair3 (`build/scripts/watch-catalog.sh.txt`,
  `pdnsdb.py.txt`): catalog AXFR from the secondary to the primary every ~10 s, logged on change
  (`t*/obs/catalog-watch.jsonl`); in t3 read-only SQLite opens of the primary's PowerDNS database.
  It adds AXFR requests from the secondary's address that the servers' logs do not distinguish
  from native transfers.
- **Read-only collection after each run, guests still up** (`ro-collect.sh`, `diag-*.sh`,
  `t*final.sh`, `t3notify.sh`): rndc key metadata, companion record, Agent/named/pdns journal
  lines, effective `named-checkconf -p` options and zone stanzas (lines containing
  `secret`/`algorithm`/`key` dropped), inspector sudo/named lines, Panel/Agent journal around the
  deletion, mail-root path metadata (t2), native/catalog probe while pending (t2), PowerDNS
  database read (t3). For the rndc content check below, the collection also computed on each BIND
  guest the SHA-256 of the key's secret string and kept only that digest in a host-only directory
  outside the evidence (`/root/cp-pair4/secret-hashes/`); the secret itself never left the guest.
- Cosmetic: Arch lacks `hostname`, so the Arch collection headers read "on  (Arch Linux)".
- Cells were stopped with `fixture.py stop` (QMP quit; overlays kept, no teardown):
  `t*/obs/cell-stop.log`.

## Wall time (UTC, 2026-09-30)

Host check 02:04:54; web build 02:05:20–02:05:35; host setup 02:06:10–02:06:19; archive
02:06:47–02:07:31; offline tests to 02:07:40. t1 prepare 02:10:01, run 02:11:01–02:20:17; t2
prepare 02:23:01, run 02:24:03–02:27:47; t3 prepare 02:29:35, run 02:30:33–02:39:54; cells stopped
02:22:48, 02:29:32, 02:40:55; pack 02:43. About 40 minutes. Runner logs:
[build/runner-logs/](build/runner-logs/). Host before the run: 15697 MiB memory (15017
available), 810 GB free disk, 16 CPUs, KVM, no QEMU process ([build/host.txt](build/host.txt)).

## License fixture

Every Panel reported the acceptance fixture license (`result.json` `license_status`). Journal
check: 0 lines naming `celikpanel.net` or the refusing transport on all six Panels. The in-process
refused-attempt counter is not exposed and was not read.

## What remains on the host

Stopped cells (overlays kept, no QEMU process running) under `/var/tmp/cp-pair4-work/cells/`
(`5394ab30…`, `cdf4eea7…`, `e42f5c78…` for `pair-accept__bind-debian13__bind-arch__p4t1`,
`pair-accept__bind-arch__pdns-debian13__p4t2`, `pair-accept__pdns-debian13__bind-arch__p4t3`),
each with its product state at the stop (deletion pending). Also `/var/tmp/cp-pair4-work/images`
(hard links), `/var/tmp/cp-pair4-dist/`, `/var/tmp/cp-pair4-evidence/`, `/var/tmp/cp-pair4-key/`
and `/root/cp-pair4/` (clone, driver, run scripts, logs, obs, test copies, evidence pack, scan
self-test, host-only secret digests). Nothing outside `cp-pair4*` was created, changed or
deleted on the host (`/var/tmp/cp-b9-1001` and earlier runs' paths untouched).

## Integrity and secrets

Each run directory carries the driver's `SHA256SUMS`, re-verified with `evidence.verify_sums` on
the host (no mismatch). [SHA256SUMS](SHA256SUMS) covers every file in this folder except itself,
this README included. The independent scanner from pair3 (only its docstring changed;
[build/secret-scan-evidence.txt](build/secret-scan-evidence.txt)) first flagged all nine
planted sample kinds (15 findings in 10 files; a redacted control file not flagged) and then found
nothing in the evidence: **0 findings over 530 files** (private keys, OpenSSH key blobs, cookies,
bearer/basic credentials, admin-password shape, license-key shape, TSIG/BIND `secret "…"`,
JSON secret-named keys). **rndc key content:** [build/rndc-key-content-scan.txt](build/rndc-key-content-scan.txt)
hashed 3568 base64-alphabet tokens in 531 files against the four guest-side secret digests
(t1 arch, t1 debian13, t2 arch, t3 arch): **0 matches**; its self-test found a planted secret only
in the planted file ([build/secret-scan-selftest-summary.txt](build/secret-scan-selftest-summary.txt)).
The companion records' `sha256` is the product's hash of the key file, not key content.

## What this does not prove

License behaviour is not evidenced (acceptance-fixture build). The archive is an unsigned local
build from the acceptance branch, not a release; the guests are disposable QEMU guests on an
isolated link, not an installed server; no Frankfurt/Boston behaviour is implied. One run per
topology. Because every topology stopped at `zone-delete`: no completed deletion, no deletion
proof accepted by the product, no authoritative-absence check, no zone re-add, **no
management-disabled reboot** (DNS answering with management off was not observed) and no
management return in any topology. The PowerDNS-secondary owner enrollment (`--engine pdns`) was
not reached (t2 stopped before it). The rndc-key rollback rule and `bind_rndc_unavailable` were
not exercised. P4-2's reach beyond this Arch guest (every Arch host, other deletions) is a source
reading, not an observation. The notify comparison is one topology and one run; the PowerDNS
secondary logs no incoming NOTIFY, so t2 shows transfers, not notify receipt.
