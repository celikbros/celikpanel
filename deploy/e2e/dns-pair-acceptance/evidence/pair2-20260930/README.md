# DNS pair product-flow acceptance, second native run (pair2), 2026-09-30

Exploratory. Second native run of the product-flow DNS pair acceptance driver with
`--license-mode acceptance-fixture` on two disposable QEMU guests (fixture `debian13`
192.0.2.10, `arch` 192.0.2.11) on the local WSL `archlinux` host. No installed server,
remote host, release or signed bundle was touched; nothing contacted celikpanel.net or any
license service. Nothing here passes an acceptance-register row; every `result.json` keeps
`native_evidence: false`.

- **Product:** commit `916e1577933f37fb168818242befe02e01ce2122` (tree
  `4a1ad4336ffd72fadac4f6fc0c810eb434b842b5`), local branch `accept/pdns-primary-gate-open`
  ("Open the fresh paired PowerDNS primary gate (acceptance candidate)"; contains
  `secondary_ready` in the DNS engine snapshot, `49a3f4b1`).
- **Driver:** `git archive 13213343a8b637de9ebcb8faa3000c6e3c1277fe deploy/e2e/dns-pair-acceptance
  deploy/e2e/dns-kill-matrix` (main line), extracted on the run host. Not run from a working tree.
- **Repository `HEAD`** (branch `feat/dns-artifact-separation`): `cfbd70d1` at start,
  `7646f531` at end (another agent committed `cc2d430b`, `1f1a7830`, `7646f531` during the run;
  the driver and product used here are the archives above, unaffected).

## Result: no topology reached the zone lifecycle

| Dir | Topology (primary / secondary) | `overall` | Stopped at | Class |
|---|---|---|---|---|
| `t1-bind-bind/p2t1-20260929t212421z` | BIND debian13 / BIND arch | **failed** | `pair-ready` (primary) | harness pass rule (H-A) |
| `t2-bind-pdns/p2t2-20260929t213638z` | BIND arch / PowerDNS debian13 | **failed** | `pair-ready` (primary) | harness pass rule (H-A) |
| `t3-pdns-bind/p2t3-20260929t214728z` | PowerDNS debian13 / BIND arch | **failed** | `setup-start-primary` | **product** (P-A) |

Zone add, record add/edit, zone delete (and therefore deletion proof and owner enrollment),
re-add, the management-disabled reboot and management return were **never executed** in any
topology; they prove nothing either way. The secondary side of D1 passed natively in t1
(BIND) and t2 (PowerDNS). In t3 the reviewed plan carried **no** `pdns_primary_switch_paused`
blocker (`can_start: true`, `blockers: []`), as expected with the gate open; the fresh
paired PowerDNS primary then failed inside the product.

## Harness defect H-A: D1 demands a field the product deliberately omits on a primary

`pair_contract.py:49-51` (`EXPECTED["primary"] = {pair_ready: True, secondary_ready: False}`)
with the absent-field rule at `:141-146` fails a primary whose snapshot has no
`secondary_ready`. The product at `916e1577` serializes `secondary_ready` **only** for an
active paired secondary ("Decision D (2026-09-30): secondary_ready exists exactly on an
active paired secondary; the primary payload has no such key",
`cmd/panel/dns_engine_test.go:2003-2004`; producer `cmd/panel/dns_engine.go:637-649`, field
`omitempty` at `:108`), and the product's web client **refuses** a primary snapshot that
carries it (`web/src/lib/dnsEngineContract.ts:383-384`). So D1 as coded cannot pass for any
primary of this build, and the driver's `read_engine_snapshot` (`pair_contract.py:112`) does
not mirror the client's `:383-384` refusal. The failure text "this build does not serialize
it" (`pair_contract.py:53-57`) is stale for this build: the secondary payload did carry it.

Observed exactly so, both runs: primary `{active_engine: bind, state: ready, topology:
paired, pair_role: primary, pair_ready: true, secondary_ready: <absent>}`, identical for 48
(t1) / 55 (t2) reads over ~300 s, stopped by the stable window; secondary `{..., pair_role:
secondary, pair_ready: false, secondary_ready: true}` on the first read (t1 BIND arch, t2
PowerDNS debian13). Correcting it is a pass-rule change (for example: primary
`secondary_ready` absent, as Decision D states); **it was not made here** and needs the
owner's decision.

## Product finding P-A (t3): a fresh paired PowerDNS primary with no zones cannot pass its own catalog check

`result.json`: `setup-start-primary` **failed**, reason `setup DNS step did not finish within
2700s`. Agent journal (`guests/debian13/journal-celikpanel-agent.service.txt`, 21:51:25Z):

> DNS engine switch to pdns at epoch 1 failed: fresh PowerDNS primary remains pending in its
> durable V3 journal; reconcile the exact operation: verify PowerDNS pair catalog: PowerDNS
> producer membership differs from the switch manifest

Cause (source reading plus a one-line Go check, `build/t3-deepequal-check/`):
`verifyPDNSProducerMembershipTx` (`cmd/agent/dns_engine_pdns_catalog.go:1420-1470` at
`916e1577`) builds `actual := make([]string, 0)` (`:1453`, non-nil) and `expected :=
append([]string(nil), expectedMembers...)` (`:1465`), which is **nil** when the manifest has
no zones, then compares with `reflect.DeepEqual` (`:1467`). `DeepEqual([]string{}, nil)` is
false, so a paired PowerDNS primary installed before any zone exists always fails
(caller `cmd/agent/dns_engine_pdns.go:832-844`). A fresh server set up through the wizard has
no zones. Batch 8 passed this path only with a zone already present (`s1-kill.test`). The same
lines are unchanged on the main line at `7646f531` (gate closed there). Not proven by a code
change or a rerun here.

State left (read-only, `build/t3-primary-readonly-diagnosis.txt`; collected
`guests/debian13/management.json`): `pdns.service` inactive and **masked**, `pdns-server`
4.9.17 installed, `/etc/powerdns/pdns.d` empty, no PowerDNS SQLite database found,
`/var/lib/celikpanel-agent-private/dns-engine-install-ownership-pdns.json` present; Agent and
Panel active. The BIND secondary (arch) was never set up (its steps did not run).

## Guidance (D-024)

Every waiting/failed state the driver recorded had a stable code and specific text, and the
driver rated all of them actionable. Two of them are nevertheless product D-024 findings by
reading; the driver's model cannot see either (it checks code + specific text, not whether
the text matches the verified cause, and it bounds nothing in time):

- **P-B (t3), verified failure shown as an open-ended unknown.** For 45 minutes (896 reads)
  the primary's setup stayed `running`, phase `01-dns`, code `server_setup_reconciling`
  ("The previous DNS operation is being reconciled. Its exact receipt must be verified before
  the setup plan can change."). The wizard showed (`setup.guide.confirm` + role text):
  EN "The last operation’s result has not been confirmed yet. CelikPanel is checking the saved
  result; it cannot yet say whether that step finished. Do not start it again. | This server
  is Primary: ns1.ns-accept.test (192.0.2.10). Its expected Secondary is ns2.ns-accept.test
  (192.0.2.11). | If the secondary has not been configured, start its DNS setup now using this
  server as its primary. …" / TR "Son işlemin sonucu henüz doğrulanamadı. CelikPanel kayıtlı
  sonucu kontrol ediyor; bu adımın tamamlandığı henüz kesin değil. İşlemi yeniden
  başlatmayın. | Bu sunucu Birincil: … | İkincil sunucu henüz yapılandırılmadıysa DNS
  kurulumunu şimdi başlatın …". The Agent had already logged a verified failure at 21:51:25Z
  and PowerDNS is not serving; the screen names no reason, no actor and no next action
  (neither the Agent's "reconcile the exact operation" nor `recovery dns-switch-status`), and
  it tells the owner to start the secondary against a primary that is not serving.
- **P-C (t1, t2 secondaries), unverified license shown as "license required".** In both
  runs the secondary's setup was read once at `waiting`, phase `license`, code
  `license_required` ("Activate the license to continue the remaining setup steps. Existing
  services keep running."), then 3 s later at `access_dns`
  (`t1-bind-bind/…/steps/11-pair-ready/execution-secondary-000.json` and `-001.json`; same in
  t2). The fixture license was `state: active` throughout and no journal line mentions a
  license change. Shown: EN "Open License settings to check the CelikPanel license and follow
  the message there. Setup can continue after access is restored. Do not start a second setup
  while this operation is waiting." / TR "CelikPanel lisansını kontrol etmek için Lisans
  ayarlarını açın ve oradaki yönlendirmeyi izleyin. Erişim yeniden sağlanınca kurulum devam
  edebilir. Bu işlem beklerken ikinci bir kurulum başlatmayın." Reading: the background setup
  runner checks `p.license.Status().CanProvision`
  (`cmd/panel/server_setup_operations.go:1351-1353`, written as `license_required` at
  `:822-826`); `Status()` turns to `verification_unavailable`/`CanProvision: false` one
  `CheckInterval` (1 min) after the last verification
  (`internal/licensing/acceptance_fixture.go:279-307`; the customer manager has the same rule,
  `internal/licensing/license.go:197-208`), and verification is refreshed only by HTTP
  handlers (`cmd/panel/license.go:143`). The secondary's Panel got no request for ~5 minutes
  while the driver polled the primary; the first read refreshed it and setup resumed. An
  *unknown* verification became "activate the license" (D-024 separation, D-025 "unknown must
  not become false license expiry"). Observed with the fixture seam only; the customer path is
  source reading, not evidence.

Other states, all specific and expected: setup progress text on both roles (`setup.guide.primary`,
`setup.guide.secondary`, `setup.guide.startSecondary`/`startPrimary`, `setup.guide.component`);
`server_setup_access_dns_required` on each secondary (offline public hostname, expected):
t1 EN "Certificate issuance requires this public A record: panel-arch.ns-accept.test →
192.0.2.11. | The public record has not been verified yet. This result alone does not
establish whether the record is absent or a DNS server is unreachable. | On the primary
ns1.ns-accept.test (192.0.2.10), prepare the required record and allow this secondary to
transfer the zone. | This prerequisite is checked automatically. After it passes, the same
setup continues. To change the reviewed names or addresses, use Edit setup plan below." / TR
"Sertifika için gereken genel A kaydı: panel-arch.ns-accept.test → 192.0.2.11. | Genel kayıt
henüz doğrulanmadı. … | Birincil ns1.ns-accept.test (192.0.2.10) üzerinde gereken kaydı
hazırlayın ve bu ikincilin bölgeyi aktarmasına izin verin. | Bu gereksinim otomatik kontrol
edilir. …"; t2 the same with panel-debian13 → 192.0.2.10 and primary 192.0.2.11. The
primaries' final setup state was not read in t1/t2 (the driver skips it once D1 fails for
that role).

## Per-topology step verdicts (verbatim from `result.json`)

### t1-bind-bind/p2t1-20260929t212421z — overall `failed`

| # | Step | Verdict | Reason / note |
|---|---|---|---|
| 00 | preflight | passed | |
| 01 | install-primary | passed | debian13, 35 s |
| 02 | install-secondary | passed | arch, 2 min 20 s incl. owner restart (installer "RESTART THIS SERVER NOW", kernel 7.1.8-arch1-3) |
| 03–04 | login-* | passed | |
| 05–06 | license-* | passed | fixture label, this cell and node, `state: active` |
| 07 | setup-review-primary | passed | |
| 08 | setup-start-primary | passed | DNS step done in 22 s |
| 09 | setup-review-secondary | passed | |
| 10 | setup-start-secondary | passed | DNS step done in 19 s |
| 11 | pair-ready | **failed** | `debian13 Panel does not show a ready paired bind primary (D1: pair_role == primary, pair_ready === true, secondary_ready === false); stopped early: the snapshot was identical for 48 reads over 300.763s and does not satisfy the rule: secondary_ready absent from GET /api/v1/dns/engine: this build does not serialize it (cmd/panel/dns_engine.go dnsEngineSnapshot has pair_role and pair_ready only; SecondaryReady lives in the Agent runtime, internal/transport/dns_contracts.go); D1 requires secondary_ready === false; payload steps/11-pair-ready/engine-ready-primary.json: {'active_engine': 'bind', 'state': 'ready', 'topology': 'paired', 'pair_role': 'primary', 'pair_ready': True, 'secondary_ready': '<absent>', 'revision': 3, 'engine_epoch': 1}` |
| 12–18 | zone-add … management-return | not-run | `prerequisite step(s) pair-ready=failed` (chain) |
| 19 | collect | passed | license-service journal check: no line on either Panel |

### t2-bind-pdns/p2t2-20260929t213638z — overall `failed`

| # | Step | Verdict | Reason / note |
|---|---|---|---|
| 00 | preflight | passed | |
| 01 | install-primary | passed | arch (BIND), 3 min 02 s incl. owner restart (same banner) |
| 02 | install-secondary | passed | debian13 (PowerDNS), 28 s |
| 03–06 | login-*, license-* | passed | |
| 07–10 | setup-review/start-* | passed | DNS steps done in 13 s / 16 s |
| 11 | pair-ready | **failed** | `arch Panel does not show a ready paired bind primary (D1: …); stopped early: the snapshot was identical for 55 reads over 302.043s and does not satisfy the rule: secondary_ready absent from GET /api/v1/dns/engine: this build does not serialize it (…); D1 requires secondary_ready === false; payload steps/11-pair-ready/engine-ready-primary.json: {'active_engine': 'bind', 'state': 'ready', 'topology': 'paired', 'pair_role': 'primary', 'pair_ready': True, 'secondary_ready': '<absent>', 'revision': 3, 'engine_epoch': 1}` (secondary `pdns`, `secondary_ready: true`, passed) |
| 12–18 | zone-add … management-return | not-run | prerequisite chain |
| 19 | collect | passed | |

### t3-pdns-bind/p2t3-20260929t214728z — overall `failed`

| # | Step | Verdict | Reason / note |
|---|---|---|---|
| 00 | preflight | passed | |
| 01 | install-primary | passed | debian13, 31 s |
| 02 | install-secondary | passed | arch, 2 min 59 s incl. owner restart |
| 03–06 | login-*, license-* | passed | |
| 07 | setup-review-primary | passed | plan `can_start: true`, `blockers: []` (no `pdns_primary_switch_paused`) |
| 08 | setup-start-primary | **failed** | `setup DNS step did not finish within 2700s` (P-A; 896 reads, 891 identical, stop `timeout`: a `running` state is never stopped early) |
| 09–18 | setup-review-secondary … management-return | not-run | `prerequisite step(s) setup-start-primary=failed` (chain) |
| 19 | collect | passed | |

## Owner enrollment

Not reached in any topology (zone-delete never ran). Whether the product asks for
`dns-peer-enroll` with a BIND or PowerDNS secondary remains open. No owner command was run;
the only owner steps were the three post-install restarts of the arch guest.

## Zone step timings

None: no zone was ever added. Setup DNS steps (start → `dns` step done, driver's view): t1
primary 22 s, secondary 19 s; t2 primary 13 s, secondary 16 s; t3 primary never.

## Builds

- `web/dist`: Windows, `git archive 916e1577 web` into the scratchpad, `web/node_modules`
  junction to the repository's, `npm run build` (node v24.18.0, npm 11.16.0; lockfile identical
  to the commit). `tsc`, `vite build` and the recovery-worker step completed; the bundle-budget
  script **exited 1** (`tr-BL80j64N.js: 161.67 KiB raw / 45.49 KiB gzip (limit 160.25 KiB /
  45.25 KiB)`, same as batch 8); the produced dist (104 files) was used
  ([build/web-build.log](build/web-build.log), [build/web-dist.sha256](build/web-dist.sha256),
  [build/web-dist-windows-build.sha256](build/web-dist-windows-build.sha256); same digest set).
- Run host: `/root/cp-pair2/repo` = `git clone --no-checkout` of the repository plus that
  `web/dist`; the driver's `scripts/build-dist.sh --acceptance-license 916e1577…` with
  `CELIKPANEL_REPO` pointing at it. Archive
  `celikpanel-v0.0.0-pairaccept-acceptance-license.916e1577933f-acceptance-license.tar.gz`,
  SHA-256 `a950b1e6d4449d6ea096735f6a1b28705033ad89ecf3e7034c206247029ef248`, Go
  `go1.26.5 linux/amd64`, unsigned local build, `release: false`
  ([build/dist.json](build/dist.json), [build/build-dist-make.log](build/build-dist-make.log)).
- Packaging guard: refused the acceptance tree three ways
  ([build/guard-refusal.txt](build/guard-refusal.txt)) and, run separately on the final
  `.tar.gz`, refused it the same three ways with exit 1
  ([build/guard-refusal-archive.txt](build/guard-refusal-archive.txt)).
- Offline tests of the run composition: 105/105 pass
  ([build/offline-tests-run-composition.log](build/offline-tests-run-composition.log)). The
  pure driver commit with its **own** `web/src` fails 1/105
  (`test_sequence.py:321`, `test_pdns_secondary_enrollment_then_one_retry`: the main-line
  catalogue already names `--engine pdns`), recorded only
  ([build/offline-tests-driver-commit-pure.log](build/offline-tests-driver-commit-pure.log)).

## Harness workarounds and run composition

**No driver file was patched; no pass or redaction rule was changed.** Hashes of the driver
files as run: [build/driver-files.sha256](build/driver-files.sha256). One composition choice,
not a patch: `guidance.py` resolves the UI's texts from `<driver root>/web/src`
(`guidance.py:42-43`, `:485`), which the requested `git archive` of the two driver
directories does not contain. The run host's driver root therefore received
`git archive 916e1577 web/src`, so the guidance recorded is what **this product build**
shows ([build/driver-support-files.sha256](build/driver-support-files.sha256)). Between the
two commits only `err.DNS_PUBLICATION_FAILED.dns_peer_enrollment_required` differs (EN/TR),
which no run reached.

Other deviations: evidence root `/var/tmp/cp-pair-accept/evidence-pair2` (new); run labels
`p2t1`, `p2t2`, `p2t3`; each cell was stopped with the fixture's QMP `stop` after its run
(overlays kept, no teardown). Read-only SSH diagnosis on the t3 primary (journal, unit state,
file listing) and a standalone Go check of `reflect.DeepEqual` semantics in `/tmp` (removed)
are the only actions outside the driver.

## Wall time (UTC, 2026-09-29)

web build 21:20; host setup 21:21:15–21:21:25; archive 21:22:01–21:22:47; t1 prepare
21:23:17, run 21:24:20–21:33:18; t2 21:35:28–21:46:01; t3 21:46:23–22:36:18 (45 min of it the
setup wait). Total about 1 h 20 min. Runner logs: [build/runner-logs/](build/runner-logs/).

## License fixture

Every Panel reported `license_kind: acceptance_fixture`, label "ACCEPTANCE FIXTURE — NOT FOR
PRODUCTION", `license_service: "not contacted: acceptance test build"`, `acceptance_guest:
verified`, its cell and node, `state: active` (`result.json` `license_status`). Journal check:
0 lines naming `celikpanel.net` or the refusing transport on all six Panels. The in-process
refused-attempt counter is not exposed and was not read.

## What remains on the host

Stopped cells (overlays kept, no QEMU process running) under `/var/tmp/cp-pair-accept/work/cells/`
for `pair-accept__bind-debian13__bind-arch__p2t1`, `pair-accept__bind-arch__pdns-debian13__p2t2`
and `pair-accept__pdns-debian13__bind-arch__p2t3` (the t3 guests keep the product state of
P-A). This run also created `/var/tmp/cp-pair-accept/dist/916e1577…-acceptance-license/`,
`/var/tmp/cp-pair-accept/evidence-pair2/` and `/root/cp-pair2/` (clone, driver, logs,
`driver-pure-13213343` test copy). pair1's four cells and everything else were left untouched;
nothing was deleted.

## Integrity and secrets

Each run directory carries the driver's `SHA256SUMS`, re-verified with `evidence.verify_sums`
on the host and on the copy (no mismatch). [SHA256SUMS](SHA256SUMS) covers every file here
except this README and itself. Before copying, an independent scanner
([build/secret-scan.py.txt](build/secret-scan.py.txt)) first flagged all nine planted samples
(admin-password shape, session cookie in text, header object, header list pair and cookie
list, bearer credential, OpenSSH private key, license key, BIND TSIG secret, secret-named JSON
key; a redacted control file was not flagged:
[build/secret-scan-selftest-summary.txt](build/secret-scan-selftest-summary.txt)), then found
nothing in the evidence (0 findings, [build/secret-scan-evidence.txt](build/secret-scan-evidence.txt)).

## What this does not prove

License behaviour is not evidenced: both panels ran the acceptance-fixture build, which never
contacts the license service and has no activation, renewal, expiry or rejection path. The
archive is an unsigned local build from the acceptance branch, not a release; the guests are
disposable QEMU guests on an isolated link, not an installed server, and no Frankfurt/Boston
behaviour is implied. Offline public-hostname DNS and the panel certificate stay waiting by
design (`access_dns`). One run per topology. Because of H-A and P-A, nothing about the DNS
pair's zone lifecycle, deletion proof, owner enrollment (BIND or PowerDNS), independence
reboot or management return was exercised; the passed setup steps show only that each Panel
accepted the wizard plan and completed its own DNS step, and that each secondary reported
`secondary_ready: true`. P-A's cause and P-C's mechanism are source readings supported by one
observation each, not a fix or a rerun.
