# DNS pair product-flow acceptance, first native run (pair1), 2026-09-29/30

Exploratory. First native run of the product-flow DNS pair acceptance driver
(`deploy/e2e/dns-pair-acceptance`) against commit
`aa6b93808e57c5586e513601953b4db07ef6328f` (tree `9fba2e53bc58572daa6a68b1e8122b4921550a92`),
with `--license-mode acceptance-fixture` on two disposable QEMU guests (fixture
`debian13` 192.0.2.10, `arch` 192.0.2.11) on the local WSL `archlinux` host. No installed
server, remote host, release or signed bundle was touched; nothing contacted celikpanel.net
or any license service. Nothing here passes an acceptance-register row, and `result.json`
keeps `native_evidence: false`.

**Result: the driver never reached the zone lifecycle in any topology.**

| Dir | Topology (primary / secondary) | `result.json` `overall` | Stopped at |
|---|---|---|---|
| `t1-bind-bind/r1-20260929t171353z` | BIND debian13 / BIND arch | **failed** | `setup-review-secondary`: plan HTTP 500 (Arch kernel not restarted after install; harness omission, see H3) |
| `t1-bind-bind/r2-20260929t171907z` | BIND debian13 / BIND arch | **failed** | `pair-ready`: driver pass rule contradicts the product contract (harness defect D1) |
| `t2-bind-pdns/r1-20260929t181010z` | BIND arch / PowerDNS debian13 | **failed** | `pair-ready`: same harness defect D1 |
| `t3-pdns-bind/r1-20260929t183604z` | PowerDNS debian13 / BIND arch | **refused-by-product-gate** | `setup-review-primary`: `pdns_primary_switch_paused`, as expected; refused plan left nothing |

Every failure in this run is a **harness** failure. Zone add, record add/edit, zone delete
(and therefore owner enrollment), re-add, the management-disabled reboot and management
return were **never executed** in any topology; they prove nothing either way.

## Harness workarounds (read this)

All three are in the run copy only (`/root/cp-pair1/src-patched` on the host, a `git archive`
of `aa6b9380` plus the patch). No repository file was edited. No pass rule and no redaction
rule was changed. Exact diff: [build/harness-workarounds.diff](build/harness-workarounds.diff)
(used by t1-r2, t2, t3); t1-r1 ran with an earlier version without H3:
[build/harness-workarounds-v1-used-by-t1-r1.diff](build/harness-workarounds-v1-used-by-t1-r1.diff).
Offline tests: 76/76 pass on the unpatched tree and on the patched tree
([build/offline-tests-unpatched.log](build/offline-tests-unpatched.log),
[build/offline-tests-patched.log](build/offline-tests-patched.log)).

- **H1 run id.** `pair_acceptance.py` builds `run_id` with `strftime('%Y%m%dT%H%M%SZ')`,
  but `evidence.RUN_ID_RE` (`evidence.py:36`) admits only lowercase, so the first
  `--execute` died in `EvidenceWriter.__init__` (`evidence.py:98-99`) before any guest
  was touched ([build/runner-logs/t1-run.log](build/runner-logs/t1-run.log)). Patched to
  `'%Y%m%dt%H%M%Sz'`. The offline tests never exercise `execute_run`.
- **H2 PowerDNS owner enrollment.** At `aa6b9380` the driver still raised
  `blocked-product` E1 for a PowerDNS secondary (`pair_acceptance.py:1073-1098`), although
  `dns-peer-enroll` accepts `--engine pdns` since `a47b4a40` and the archive ships
  `dns-owner-tools/pdns-peer-inspect`. Patched to run the same owner sequence with
  `--engine pdns`, the packaged `pdns-peer-inspect`, and `--catalog-account` read
  read-only from the secondary's PowerDNS `domains.account` of the catalog CONSUMER row
  (probe extended to report `account`). The E1 offline test was replaced by a PowerDNS
  enrollment test. **Never exercised natively**: zone-delete did not run.
- **H3 owner restart after install.** On Arch, `install.sh` upgrades the kernel and prints
  "RESTART THIS SERVER NOW … Until this server is restarted it cannot load nftables". The
  driver went straight to setup. Patched: when `install.sh` prints that banner, the driver
  reboots that guest through `fixture.reboot_guest` (identity re-checked, new boot ID),
  waits for the services and the TLS leaf, and records `owner-restart-after-install.json`.
  Used in t1-r2 (arch), t2 (arch), t3 (arch).

Other deviations: `t2` ran with `--setup-timeout 1200` (default 2700) because the t1-r2
failure had shown that `pair-ready` cannot pass for any secondary (D1); this only
shortened the wait. Runs used `run-topology.sh` from the run copy with
`CELIKPANEL_REPO` pointing at it; the build used a local no-checkout clone
(`/root/cp-pair1/repo`) so `web/dist` came from this run's own build, not the working tree.

## Harness defect D1 (blocks every topology after setup)

`pair_ready` (`pair_acceptance.py:877-880`, same in `management_return` `:1220`) requires
`pair_ready is True` from `GET /api/v1/dns/engine` on **both** Panels. The product contract
makes that impossible for a secondary:

- `cmd/panel/dns_engine.go:636-639` sets `pair_ready` from the runtime's `PairReady`,
  which is the *primary* publication proof;
- the secondary's consumption proof is the separate `SecondaryReady`
  (`internal/transport/dns_contracts.go:218-219`), used by setup (`cmd/panel/setup_dns.go:96-99`),
  and a runtime with both true is rejected (`cmd/panel/dns_engine.go:315`);
- the web client rejects any snapshot where a secondary reports `pair_ready !== false`
  (`web/src/lib/dnsEngineContract.ts:377-378`).

Observed exactly so: both secondaries showed `active_engine` bind/pdns, `state: ready`,
`topology: paired`, `pair_role: secondary`, `pair_ready: false` for the whole wait, while
read-only checks showed the secondary had transferred the catalog (t1: `named` "transferred
serial 1", SOA serial 1 at both servers; t2: `pdns_control list-zones` lists
`catalog-c000020b.celikpanel.invalid.`). The driver's fake Panel (`fakes.py:192`) returns
`pair_ready: true` for the secondary, so the offline tests could not see this. Correcting it
is a pass-rule change (for example: secondary ready = `pair_ready is False` plus the setup
`dns` step succeeded, or a secondary-readiness field if the product exposes one); it needs
an owner decision and was not made here.

## Product findings

- **P1 (D-024): setup plan hides a known, actionable reason behind a generic 500.** t1-r1,
  arch secondary, `POST /api/v1/setup/plan` → `500 {"code":"INTERNAL","error":"internal server
  error"}` (`t1-bind-bind/r1-…/steps/09-setup-review-secondary/api/0034-post-api-v1-setup-plan.json`).
  Panel journal: `[500] firewall status could not be verified`. Source:
  `cmd/panel/server_setup_operations.go:287-292` turns any `Agent.FirewallStatus` error into
  that string and a 500, although the Agent composes an operator sentence for this exact
  case (`cmd/agent/firewall_rpc.go:552`, "nft table discovery failed …", with the R-054
  comment above it) and the
  installer had already told the owner to restart. The wizard would show `err.INTERNAL`:
  "The server hit an internal error. Try again; if it persists, check the panel logs." /
  "Sunucuda bir iç hata oluştu. Yeniden deneyin; sürerse panel günlüklerine bakın." Retrying
  cannot help; the reason (restart needed) and the actor (server owner) are missing. The
  driver's guidance model rates `err.INTERNAL` as actionable ("specific translated guidance
  for a stable code"), and the plan-500 path records no guidance at all
  (`pair_acceptance.py:767-768`), so D-024 was not checked here by the driver.
- **P2 (observation, D-024 wording): the secondary's waiting text does not know the primary
  is refused.** t3, arch BIND secondary, `server_setup_primary_dns_required` (waiting at
  `primary_dns`): "This secondary is waiting for the primary ns1.ns-accept.test (192.0.2.10).
  Start or finish the primary's DNS configuration and allow this secondary to transfer its
  zones. …" The primary's owner cannot do that with PowerDNS while
  `pdns_primary_switch_paused` holds; the text is accurate from the secondary's side only.
- **P3 (observation): the pending-deletion text does not name `--engine pdns`.**
  `err.DNS_PUBLICATION_FAILED.dns_peer_enrollment_required` (`web/src/i18n/en.ts:205`) lists
  the dns-peer-enroll steps without `--engine pdns` / `--catalog-account`, which the tool's
  README requires for a PowerDNS secondary. Source reading only; not reached natively.

## Per-topology step verdicts (verbatim from `result.json`)

### t1-bind-bind/r1-20260929t171353z — overall `failed` (without H3)

| # | Step | Verdict | Reason |
|---|---|---|---|
| 00–08 | preflight … setup-start-primary | passed | |
| 09 | setup-review-secondary | **failed** | `reviewed plan cannot start: HTTP 500 blockers=None` |
| 10–18 | setup-start-secondary … management-return | not-run | prerequisite chain |
| 19 | collect | passed | |

Guests stopped, overlays kept (`/var/tmp/cp-pair-accept/work/cells/…r1`).

### t1-bind-bind/r2-20260929t171907z — overall `failed`

| # | Step | Verdict | Reason |
|---|---|---|---|
| 00 | preflight | passed | |
| 01 | install-primary | passed | |
| 02 | install-secondary | passed | (H3 owner restart, arch) |
| 03–06 | login-*, license-* | passed | fixture label, this cell and node, `state: active` |
| 07 | setup-review-primary | passed | |
| 08 | setup-start-primary | passed | |
| 09 | setup-review-secondary | passed | |
| 10 | setup-start-secondary | passed | |
| 11 | pair-ready | **failed** | `arch Panel never showed a ready paired bind secondary: {'active_engine': 'bind', 'state': 'ready', 'topology': 'paired', 'pair_role': 'secondary', 'pair_ready': False}` |
| 12–18 | zone-add … management-return | not-run | `prerequisite step(s) pair-ready=failed` (chain) |
| 19 | collect | passed | |

### t2-bind-pdns/r1-20260929t181010z — overall `failed`

| # | Step | Verdict | Reason |
|---|---|---|---|
| 00–10 | preflight … setup-start-secondary | passed | (H3 owner restart, arch primary) |
| 11 | pair-ready | **failed** | `debian13 Panel never showed a ready paired pdns secondary: {'active_engine': 'pdns', 'state': 'ready', 'topology': 'paired', 'pair_role': 'secondary', 'pair_ready': False}` |
| 12–18 | zone-add … management-return | not-run | prerequisite chain |
| 19 | collect | passed | |

### t3-pdns-bind/r1-20260929t183604z — overall `refused-by-product-gate`

| # | Step | Verdict | Reason |
|---|---|---|---|
| 00–06 | preflight … license-secondary | passed | (H3 owner restart, arch) |
| 07 | setup-review-primary | **refused-by-product-gate** | `PowerDNS primary refused by the server plan (pdns_primary_switch_paused); shown: PowerDNS primary in a DNS pair is not ready. Keep DNS running; select BIND primary. Review a new plan when support is available.` |
| 08 | setup-secondary-before-primary | passed | |
| 09–16 | pair-ready … management-return | skipped | `prerequisite step(s) setup-review-primary=refused-by-product-gate` |
| 17 | collect | passed | |

`refused_plan_left_nothing: true`: setup stayed `draft` revision 2; the DNS engine card was
`active_engine: null`, `state: unconfigured`, revision 0 before and after; plan
`can_start: false`, blockers `["pdns_primary_switch_paused"]`; after collection no DNS unit
was active on debian13. TR text: "DNS çiftinde PowerDNS birincil geçişi hazır değil. DNS'i
çalışır bırakın; BIND birincil seçin. Destek geldiğinde planı yeniden inceleyin." DNS engine
card text (EN): "PowerDNS primary in a DNS pair is paused while native catalog and rollback
support is completed. Leave any current DNS server running; review another engine or
topology plan." The arch secondary then waited with `server_setup_primary_dns_required`
(text in P2), which is where the driver leaves it by design.

## Guidance the Panel showed

Waiting/pending states the driver recorded, all with a stable code and specific text:
`server_setup_access_dns_required` on each primary (t1-r2, t2; setup waiting at
`access_dns`, expected offline: "Certificate issuance requires this public A record:
panel-<node>.ns-accept.test → <ip>. …"), `pdns_primary_switch_paused` and
`server_setup_primary_dns_required` (t3). The only blocked state **without** actionable
guidance was P1. The secondary's `pair_ready: false` is not shown to the owner as a
pending state (the card shows pair readiness only for a primary,
`web/src/components/DNSEngineCard.tsx:979-988`), so it is not a guidance gap.

## Owner enrollment

Not reached in any topology (zone-delete never ran). Whether the product asks for it with
a BIND or PowerDNS secondary, and whether H2 works natively, remain open.

## License fixture

Both panels in every run reported `license_kind: acceptance_fixture`, label
"ACCEPTANCE FIXTURE — NOT FOR PRODUCTION", `license_service: "not contacted: acceptance
test build"`, `acceptance_guest: verified`, the cell and node; `acceptance_smbios_uuid:
"not readable by the panel service user"` (see `result.json` `license_status`). The
refused-connection counter (`licensing.AcceptanceLicenseServiceDialAttempts`) is in-process
only and is not exposed through any API or log, so it could not be read natively. Proxy
evidence: in every collected panel journal the refusing transport's error text ("the
acceptance test build never contacts the license service") occurs 0 times and
`celikpanel.net` occurs 0 times; the start-up banner appears on each start.

## Builds

- Tested commit `aa6b9380`. Repository `HEAD` (branch `feat/dns-artifact-separation`) was
  `e5d9cc9d` at start and `25f84573` at end (another agent committed "Harness: fresh paired
  PowerDNS primary cells through the public Agent RPC" during the run; the working tree also
  carried that agent's uncommitted Go edits). Nothing was built or run from the working
  tree: sources came from `git archive aa6b9380`. Between `aa6b9380` and `e5d9cc9d` the
  driver, `cmd/`, `internal/` and `web/` are identical.
- `web/dist`: built on Windows from `git archive aa6b9380 web` with the repository's
  `web/node_modules` by junction, `npm run build` ([build/web-build.log](build/web-build.log)).
  Hash lists: [build/web-dist.sha256](build/web-dist.sha256) (build-dist.sh's copy, 104 files) and
  [build/web-dist-windows-build.sha256](build/web-dist-windows-build.sha256) (identical except
  Windows `*` binary markers).
- Archive: `scripts/build-dist.sh --acceptance-license aa6b9380…` with
  `CELIKPANEL_REPO=/root/cp-pair1/repo` (no-checkout clone plus that `web/dist`).
  `celikpanel-v0.0.0-pairaccept-acceptance-license.aa6b93808e57-acceptance-license.tar.gz`,
  SHA-256 `ab5f211ce42df68894ee667bd217cd7d05be08a9e2db1b7c45555bdf31621832`
  ([build/dist.json](build/dist.json), [build/build.log](build/build.log),
  [build/guard-refusal.txt](build/guard-refusal.txt): the packaging guard refused the tree
  three ways). Go `go1.26.5 linux/amd64`. Unsigned local build.
- Build host: nothing installed; make 4.4.1 and git 2.55.0 were present
  ([build/build-host.txt](build/build-host.txt)).
- Runner logs (prepare/run per attempt, UTC start/end): [build/runner-logs/](build/runner-logs/).

## Wall time (UTC, 2026-09-29)

Build 17:07:08–17:07:51. t1-r1 17:08:44–17:15:55 (plus the H1 abort at 17:11). t1-r2
17:18:06–18:08:27 (45 min of it the `pair-ready` wait). t2 18:08:58–18:34:12 (20 min wait).
t3 18:34:58–18:41:06. Total about 1 h 35 min.

## What remains on the host

- Stopped cells with overlays under `/var/tmp/cp-pair-accept/work/cells/` for all four runs
  (not torn down). No QEMU process is running; two `[qemu-system-x86] <defunct>` entries
  remain in the WSL process table.
- `/var/tmp/cp-pair-accept/` (work root with hard-linked images, SSH key, dist, evidence),
  `/root/cp-pair1/` (clone, source copies, stage, logs). Nothing else was created or deleted.

## Integrity and secrets

Each run directory carries the driver's own `SHA256SUMS`, re-verified with
`evidence.verify_sums` (no mismatch). [SHA256SUMS](SHA256SUMS) covers every file here except
this README and itself. Before copying, an independent scan over all 1056 files looked for
the admin-password shape, session cookie values, Set-Cookie headers, bearer/basic
credentials, PEM/OpenSSH private keys, license-key shapes and secret-named JSON keys with
values: no findings.

## What this does not prove

License behaviour is not evidenced: both panels ran the acceptance-fixture build, which
never contacts the license service and has no activation, renewal, expiry or rejection path.
The archive is an unsigned local build, not a release; the guests are disposable QEMU guests
on an isolated link, not an installed server, and no Frankfurt/Boston behaviour is implied.
Offline public-hostname DNS and the panel certificate stay waiting by design
(`access_dns`). Because of D1, nothing about the DNS pair's zone lifecycle, deletion proof,
owner enrollment (BIND or PowerDNS), independence reboot or management return was
exercised; the passed setup steps show only that each Panel accepted the wizard plan and
completed its own DNS step. H2 and H3 are harness changes that a reviewer must accept
before any rerun counts.
