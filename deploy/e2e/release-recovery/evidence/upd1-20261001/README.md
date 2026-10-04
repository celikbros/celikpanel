# upd1 native run, 2026-09-30 (folder dated for the item 3 batch 2026-10-01)

Roadmap item 3, owner-started update acceptance driver (`owner_update_trial.py`),
four cells, disposable QEMU/KVM guests on the local `archlinux` WSL host only.

**Result in one sentence: no cell reached the owner's update.** Every run stopped
before `arm`/`owner-start`, so no update was started, no candidate failed, no
rollback or second fault happened, and no outage window, three-view agreement or
recovery text exists from this run. Nothing here passes a P0 row; every
`result.json` carries `native_evidence: false`.

No installed server (Boston, Frankfurt, any) was touched. Nothing was pushed,
published or signed with a production key. The candidate was served only by the
guest-loopback fixture origin; see "Real origin" below.

## Build (step 1)

`bash deploy/e2e/release-recovery/run-upd1.sh build 43a7f554` (2026-09-30
11:11:23Z to 11:13:45Z, exit 0). Web assets were built fresh in the disposable
clone (`npm ci`, `npm run build`); Go `go1.26.5 linux/amd64`.

| Role | Label / sequence | Fixture commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B | v0.1.0-alpha.81 / 81 | 864ae9a92111a604dd1f3ea2ff8f65173a39e6cc | 15f300b2e8051b0f1c93ce52e5c3cf18ff39073a | 7a891e232b4dffee8d5c96f8b60f2a6ef950a0bdd34c9d3991665ce07f6e0d3a |
| Good G | v0.1.0-alpha.82 / 82 (parent B) | 69688983d89cfbf94a6d003db6ebc22c85b4341e | d3d4c3fa30a375d1736715ae46d98eff9168fcaa | 94f85797fffca033a4751e910c2bd2bb7cc863c1ecad614b9f273edf78cc75cb |
| Defective D | v0.1.0-alpha.82 / 82 (parent G) | 9532109705775d1c827f6a65a9c90f5e543d04f2 | 90e723e2be341f4067d6b19b048ed809c607b9a2 | 8ef24da3a5cbc083772624b04e9fa6bff08791595c678a640a733fb394a726ef |

Source HEAD 43a7f5547a1b3f0e90c727f81b5267a310a42f78. All three archives are
`license_mode: acceptance-fixture`. The acceptance-license guard refused each
archive with three reasons: tag in the embedded build settings, fixture licence
present in `bin/panel`, and tag in `go version -m` (`build/guard-refusal-*.txt`).
Relative to G, the defective commit changes only `cmd/panel/main.go` (+4/-2).
B and G change only `deploy/release-sequence-policy` (`build/fixture-commits.diff`
is 43a7f554..D).
The offline suite `test_owner_update_trial.py` passed 29/29 at 43a7f554
(`build/offline-tests-43a7f554.txt`).

## Dry run (step 2)

`dry-run upd1-debian13-defective` failed first. It was a mechanical wrapper
defect (H1 below), not a plan problem. With the run copy it passed: 15 steps,
`native_evidence: false`, and no lab created
(`build/dry-run-upd1-debian13-defective.json`).

## Harness corrections used (run copy only; the repository was not edited)

The run copy is a clone of 43a7f554 at `/var/tmp/cp-upd1-run/harness`. It is
run from the repository root through `/var/tmp/cp-upd1-run/run-upd1.v3.sh`.
Diffs are in `harness-run-copy/`.

| Id | Defect | Kind | Correction | First run using it |
| --- | --- | --- | --- | --- |
| H1 | `run-upd1.sh` keeps `python3 $HERE/...` in unquoted strings; the repository path contains a space, so the dry run fails with `can't find '__main__' module in '/mnt/c/CELIKBROS'` | mechanical | Bash arrays (`run-upd1.sh.diff`) | dry run |
| H2 | `candidate_archive.verify_committed_source` has no rule for the `dns-owner-tools/` directory that `make dist` packages since 3197ff84/a47b4a40 (2026-09-28/29); preflight stops with `candidate contains uncommitted source: dns-owner-tools/README.md` | mechanical | Exact 4-file inventory. The three tools are skipped like `bin/`; README.md is proved against `cmd/dns-peer-enroll/README.md` (`candidate_archive.py.diff`). All three archives were proved offline before reuse | d13-def run-b |
| H3 | The setup draft sends `peer_ip: ""`, `peer_ns: ""`. Local DNS requires a paired identity (`cmd/panel/setup_dns_operations.go:50-59`); the product correctly returns `server_setup_dns_identity_required` | mechanical (owner choice) | Existing `--setup-draft-json` passed through: debian13 peer 192.0.2.11 / arch peer 192.0.2.10, `peer_ns=ns2.upd1-infra.test` | d13-good run-a |
| H4 | The driver requires setup `succeeded`. On an isolated host the product's fixed public-resolver check (`cmd/panel/server_setup_access_dns.go:16-26`) waits indefinitely at `access_dns`; the pair driver already treats this as settled (`pair_acceptance.py:86,1248`) | design gap | A stable 120 s wait at `access_dns`/`panel_certificate`/`verification` settles the step as `observed`, with a finding. Debian mail is not required when the wizard never reached its mail steps (`owner_update_trial.py.v4.diff`) | d13-good run-b |
| H5 | A local-DNS primary publishes only after its peer secondary serves the catalogue (`cmd/panel/dns_engine.go:749-752`). A single-node lab cannot create a domain (`DNS_SERVER_REQUIRED`) | design gap | **Scope reduction**: the owner chooses `dns_mode: external` (`draft-external.json`). The DNS workload is recorded `not-provided-external-dns` and never passed (`owner_update_trial.py.v4-to-v5.diff`) | d13-def run-c, both Arch cells |

H4 and H5 are not mechanical. They reduce what a cell can show: no DNS and no
mail continuity. They were used only so that the owner-update core could be
reached; it still was not reached (see below).

## Cells (steps 3-4)

Port bases as in the README (2361/2371/2381/2391). One lab per run; every lab
was stopped by the wrapper. Disks, overlays and host logs are retained under
`/var/tmp/cp-release-drill-upd1-*`.

| Cell / run | Harness | Lab | Steps reached | Stop | Cause class |
| --- | --- | --- | --- | --- | --- |
| upd1-debian13-defective / run-a | H1 | upd1-d13-def-a | preflight inconclusive (11:15:46Z) | dns-owner-tools source proof | harness H2 |
| upd1-debian13-defective / run-b | H1-H2 | upd1-d13-def-b | preflight, origin, baseline-install (80 s), login, licence passed; setup failed 11:20:58Z | `server_setup_dns_identity_required` | harness H3 |
| upd1-debian13-good / run-a | H1-H3 | upd1-d13-good-a | through licence passed; setup failed after 3600 s (11:28:09Z to 12:28:12Z) | waiting at `access_dns` for the whole hour | harness H4 |
| upd1-debian13-good / run-b | H1-H4 | upd1-d13-good-b | setup observed (wait at `05-access_dns`, 120 s stable); seed failed 12:33:29Z | domain create 409 `DNS_SERVER_REQUIRED` | harness H5 |
| upd1-debian13-defective / run-c | H1-H5 | upd1-d13-def-c | setup observed (steps 01-08 succeeded incl. both mail profiles; wait at `09-access_dns`); domain 1 created, index page written, mailbox `owner@upd1-owner.test` created (200); cron failed 12:45:05Z | `POST /api/v1/domains/1/cron` → 500 `INTERNAL` | **product** P1 (+ environment: no cron on the guest) |
| upd1-arch-defective / run-a | H1-H5 | upd1-arch-def-a | baseline + owner restart the installer demanded (41.7 s); setup failed 12:52:48Z | `05-mail_profile` (webmail) `service_install_failed` | **product** P2 |
| upd1-arch-good / run-a | H1-H5 | upd1-arch-good-a | same as above (restart 41.8 s); setup failed 12:58:41Z | same, reproduced | **product** P2 |

`upd1-debian13-good` was not run a third time. It would stop deterministically at the
same cron step as d13-def run-c (same image, same seed order). The defective cell
had already used three runs, each with a distinct recorded cause.

Common to every run that got that far: the fixture origin was served on the
guest's loopback (`origin_check` shows 127.0.0.1 and HTTP 200). The baseline
installed with `verified: true`, labelled v0.1.0-alpha.81. The licence moved
from `missing` to `active` with `license_service: "not contacted: acceptance
test build"`, and `can_use_panel: true`.

## Product findings

- **P1 (D-024)**: on a Debian 13 guest without cron, an owner cron job returns
  `500 {"code":"INTERNAL","error":"internal server error"}`. The UI text is
  `err.INTERNAL` (EN "The server hit an internal error. Try again; if it
  persists, check the panel logs." / TR "Sunucuda bir iç hata oluştu. Yeniden
  deneyin; sürerse panel günlüklerine bakın.").
  - The agent has an honest reason: `cmd/agent/cron_rpc.go:442-443`, "cron is
    not installed on this server". The Panel masks it with `writeServerError`
    (`cmd/panel/domain_cron_handlers.go:122-124`).
  - The text gives no reason, owner action or resume path. Setup never
    installs cron either.
  - Read-only disk read of the stopped overlay: `/usr/bin/crontab`,
    `/usr/sbin/cron` and `cron.service` are absent (`upd1-debian13-defective/run-c/host/offline-disk-read/`).
    The overlay hash is unchanged. The guest journal on disk ends before the
    request, so the agent's message is not on disk.
- **P2 (D-024)**: on Arch the `web_mail` plan is admitted with `blockers: []`,
  but setup fails at `05-mail_profile` (webmail).
  - Operations: `215316f3f423179b1394a421e9b3451a` (arch-def) and
    `48942034e22b38538bf02118ca9942e5` (arch-good).
  - The text is `service_install_failed`: EN "The service could not be installed
    and verified." / TR "Servis kurulamadı ve doğrulanamadı." It names no cause,
    actor or next action.
  - The README expected an Arch `web_mail` refusal with a `web` fallback. Instead
    the product admits the plan and then fails.
  - The Arch root is btrfs, and the host has no btrfs tools, so the failed
    operation's detail could not be read offline.
- **Observation (not a defect)**: with local DNS, the wizard orders `access_dns`
  (public resolvers) before php-fpm, mariadb and the mail steps. With external
  DNS it orders services first. The product text for the wait is actionable.
  - EN server message: "Public DNS must resolve panel-debian13.upd1-infra.test
    to 192.0.2.10 before a certificate is requested. … This check resumes
    automatically when DNS is verified."
  - UI key `setup.infrastructure.accessRequired`, EN: "The required public DNS
    record has not been verified. Follow the record and DNS management
    instructions above; certificate requests wait for this check."
  - TR: "Gerekli genel DNS kaydı henüz doğrulanmadı. Yukarıdaki kayıt ve DNS
    yönetimi yönlendirmesini izleyin; sertifika isteği bu kontrolü bekler."
  - While it waits, the runner flips the row to `running` for each retry
    (`server_setup_dns_retry.go:25`), about every 25 s.
- Other D-024 texts seen, both with an actor and an action:
  - `DNS_SERVER_REQUIRED`, EN "No managed authoritative DNS engine is active.
    Choose and activate BIND or PowerDNS before publishing domains." / TR "Etkin
    ve panel tarafından yönetilen bir yetkili DNS motoru yok. Domain yayımlamadan
    önce BIND veya PowerDNS seçip etkinleştirin." Action: "Choose a DNS engine" /
    "DNS motoru seç".
  - `setup.blocker.dnsIdentity`, EN "Configure the nameserver identity and verify
    the selected DNS engine." / TR "Ad sunucusu kimliğini yapılandırın ve seçilen
    DNS motorunu doğrulayın."
  - Arch installer restart notice (EN/TR banner, verbatim in the arch
    `steps/03-baseline-install/step.json`).

## Latent harness issues (not reached)

- L1: the guest-loopback origin is a transient `systemd-run` unit started
  **before** the baseline. On Arch the installer demands a restart, and the
  harness performs it inside `baseline-install`. The origin unit does not come
  back after that reboot, so an Arch `arm` would find no update offered. This
  was not reached, because setup stopped first.
- L2: the waiting setup rewrites `server_setup_executions` about every 25 s. The
  defective cells' database comparison would list that table as `unexpected`
  unless it is treated as a background writer.
- L3: `collect` depends on `seed`, so a run that stops at seed keeps no guest
  journal.

## Real origin

The driver's own preflight runs `getent hosts celikpanel.net` on the fresh guest
**before** the fixture origin is provisioned. On the Debian and Arch guests that
resolved the real name through the guest's resolver to `185.95.0.123`. The
result is recorded in each `steps/01-preflight/preflight.json`/`step.json`.

That was a DNS lookup, not a connection. After provisioning, the name resolved
to 127.0.0.1 and the fixture answered HTTP 200. No installer log (all 7 labs)
and no line of the extracted debian13 journal (d13-def run-c) names
`185.95.0.123` or `celikpanel.net`. No journal was collected by the harness
(collect was never reached), so "no journal line" is established only for that
one extracted journal.

## Not measured, because no update started

Owner update sequence, durable start attempt, fault 1 (candidate failure in
`active`), automatic recovery attempts, fault 2 (reset at `payload_restored` /
kill at `runtime_verified`), owner continuation, three-view agreement, recovery
texts, per-workload outage windows (site, DNS, SMTP, cron, Panel, host SSH), and
terminal checks. None of these exist from this run.

## Files

- `build/`: artifacts JSON, dist JSONs, guard refusals, fixture commit list and
  diff, build logs, offline tests, dry run.
- `harness-run-copy/`: the diffs H1-H5, the draft override files, the cell
  wrapper, hashes of the run-copy files.
- `<cell>/run-<x>/`: the driver's evidence directory unchanged (its own
  `SHA256SUMS` verified when staged), plus `host/` (wrapper stdout/stderr, start,
  end, rc, lab name, run id, baseline result JSON).
- `SHA256SUMS`: over every file here except itself.

## Host leftovers (archlinux WSL)

- `/var/tmp/cp-upd1-build/20260930t111123z` (clone + archives' clone, 579 MB)
- `/var/tmp/cp-upd1-run` (run copy, logs, staging, two extracted debian13
  journal files, 456 MB)
- Seven labs `/var/tmp/cp-release-drill-upd1-{d13-def-a,d13-def-b,d13-def-c,d13-good-a,d13-good-b,arch-def-a,arch-good-a}`
  (1.1 to 2.4 GB each), stopped, with overlays and per-lab fixture keys under
  `worker-origin/`.
- Written by the harness's own `build-dist.sh` outside the `cp-upd1*` prefix:
  `/var/tmp/cp-pair-accept/dist/{864ae9a9…,69688983…,95321097…}-acceptance-license`.
  These are new directories; nothing existing was modified.
- The Go build cache and npm cache in root's home (`~/.cache/go-build`,
  `~/.npm`) were used by the build.

No QEMU process is running. Nothing else under `/var/tmp` or `/root` was deleted.

## Secret scan

Run over the whole folder before hashing; result: **clean**.

- 0 PEM private-key blocks.
- 0 hits for any body line of every lab's fixture signing key, fixture CA key,
  fixture TLS key and lab SSH key (7 labs, 25 key files).
- 0 hits for the D-027 fixture licence literal and 0 `CPK-` keys.
- 0 unredacted password, FTP password, TSIG, secret, token or API-key values.
- 0 tokens of the admin-password shape (43-character base64url) and 0 of the
  mailbox-password shape (32 characters; the only match is a font asset name).
- 963 `[REDACTED]` markers (cookies, passwords, FTP password).

The admin password itself exists only inside the stopped guests. It was never
on the host outside the driver's memory, so it was checked by shape, not by
value.

## What this run proves and does not prove

It shows:

- the three-archive build and guard refusals at 43a7f554;
- the real installer's baseline on Debian 13 and Arch with the fixture trust and
  the acceptance licence ("not contacted");
- the owner login and licence path through the Panel API;
- the setup wizard's behaviour on an isolated host;
- two product D-024 defects (P1, P2).

It does **not** show any update, rollback, recovery, fault handling, view
agreement, workload continuity or preservation result. It gives no evidence for
P0.1, P0.2, P0.3 or P0.5. Wall time: 11:10Z to about 13:10Z.
