# upd7 native run, 2026-10-01: the first update from the published v0.1.0-alpha.80

Roadmap item 3, owner-started update acceptance (`owner_update_trial.py`), seventh run. Every earlier run (upd1-upd6)
installed a build of the current source **labelled** as the baseline. This run installs the **published tag
`v0.1.0-alpha.80`** (commit `bd14d97e`) as the baseline and lets its owner update it to a candidate built from the
source `48d21d58` - the path an installed server really takes, and the path `48d21d58` changed (the updater now writes
the initial observation record itself when the old worker wrote none).

Disposable QEMU/KVM guests on the local `archlinux` WSL host only. No installed server (Boston, Frankfurt, any) was
touched or contacted. Nothing was pushed, committed, published or signed with a production key. `celikpanel.net`
resolved only to the guest-loopback fixture origin; no licence service was contacted. Every `result.json` carries
`native_evidence: false`; the owner judges the P0 rows. **It closes no P0 row.**

**Result in one sentence:** on Debian 13 all three cells ended `complete-for-review` - the good update was verified,
the owner continuation paused on its typed first cause and completed after the owner's one printed retry, and the
defective candidate (with a QMP reset during recovery) returned automatically to the alpha.80 binaries, which started
and served; on Arch the published alpha.80 could not establish the pre-update workload (its `web_mail` setup fails, and
with `web` only the seeded site answers HTTP 404), so no Arch update was measured.

## What was built from what

`run-upd1.sh build --baseline-ref v0.1.0-alpha.80 48d21d58` ran from the run copy, 11:14:38Z-11:16:49Z, exit 0
(`build/build.*`). Clone `/var/tmp/cp-upd1-build/20261001t111438z/repo`, `go1.26.5 linux/amd64`.

| Role | Label / sequence | Commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B = tag `v0.1.0-alpha.80` + licence seam | v0.1.0-alpha.80 / 80 (the tag's own policy, unchanged) | f650e8c12d1d0c91764d201802008332f49b8d57 (parent bd14d97e) | ee6d75f688fac425179ee646a647a0ceb68283bc | 4ecd3aa17f4bda8af847f22dce6a3e2b9d97cd48ace3a1496f6e58447f263521 |
| Good G = source 48d21d58 + policy | v0.1.0-alpha.81 / 81, previous 80 / alpha.80 / B | 622ddcb40568073d1407983bf78d0b8b8e24b0a5 | 5e10aed16f8b944e7d5a55d2f21176560e3f69d9 | b82279714781d7e927be969921036e061c185eac8fce9f5da980a08cd03dd00d |
| Defective D = G + migrate-only defect | v0.1.0-alpha.81 / 81 | 205abc6463d0bbc047a1c38765e102d2fcf3c05b | 44585f688314296ef6ed1d553ec46e585b38abae | 71f487e60d778061aef08b77b46e3ca86efc147de71793177163ca261201aca2 |

- All three are `license_mode: acceptance-fixture` (`build/dist-*.json`); B's `web/` was built from the tag, G's and
  D's from the source. The start-check and real-start kinds were not built in this mode.
- `prove` exited 0 (`build/prove.*`): archive inventory, release policy and committed-source proof per role
  (B: 107 static files proved against B's blobs). Six dry runs exited 0 and created no lab (`build/dry-run-*`).
- G's policy names B as `previous_commit`, so "previous = installed commit" holds as in a real alpha.81.

### The only change to the tag's tree (D-027 licence seam)

`git diff bd14d97e f650e8c1` names exactly six files (`build/baseline-ref-proof.txt`, full patch in
`build/fixture-patches.diff`):

| File | Change |
| --- | --- |
| `internal/licensing/acceptance_off.go`, `acceptance_owner_linux.go`, `acceptance_owner_other.go` | added, byte-identical to 48d21d58 |
| `internal/licensing/acceptance_fixture.go` | added: 48d21d58's file with the `Observation` field dropped from 6 literals (the tag's `Status` has no such field) and an `errInvalidState` sentinel defined (the tag has none); `build/seam-adaptation-vs-source.diff` |
| `internal/licensing/license.go` | the seam commit 01a450e6's six hooks (embedded `acceptanceStatus`, `seam` field and interface, four `if m.seam != nil` guards) |
| `cmd/panel/license.go` | `licensing.New` -> `licensing.NewServer` (01a450e6's one line) |

Why it is needed: the tag predates the seam (`acceptance_fixture.go` is absent) and its Panel refuses every owner API
with `license_required` without a licence, and a real licence would contact the licence service. The ordinary
(non-tag) build of B compiles `acceptance_off.go`, i.e. exactly the tag's licence policy; only the archive's `bin/panel`
is rebuilt with `-tags acceptance_license`. No other fixture patch exists: the trust root is the tag's own unchanged
`deploy/enroll-signed-release-trust.sh` with the per-lab fixture key, and the origin is the existing guest-loopback
fixture.

**Unchanged-file proof** (`build/baseline-ref-proof.txt`, 31 `identical` lines, build refuses any `DIFFERENT`): blob ids
and SHA-256 at the tag and at B are equal for `update.sh`, `rollback.sh`, `install.sh`, `rebuild.sh`,
`bootstrap-update.sh`, `bootstrap-prebuilt-update.sh`, `download-portal/get.sh`, every `deploy/release-*`
(foundation, runner, protocol, sequence policy, signing key, transaction guards), `deploy/finalize-pending-update.sh`,
`deploy/finalize-pending-rollback.sh`, `deploy/enroll-signed-release-trust.sh` and `deploy/systemd/*`; the trees
`cmd/agent`, `deploy/`, `download-portal/`, `web/` and `internal/transport` are identical; and the Agent's package
closure (`go list -deps ./cmd/agent`, 270 packages, `build/agent-deps.txt`) contains no licensing package. Examples:
`update.sh` 703263a6…, `rollback.sh` e559f741…, `get.sh` e8cf0260…, `bootstrap-update.sh` 022ea8c6…,
`bootstrap-prebuilt-update.sh` 14341769…, `finalize-pending-update.sh` f86a87e0… (same at tag and B). The
Agent's update code was not touched.

## Harness changes (working tree, `deploy/e2e/`; offline suites green)

The run copy is `git archive 48d21d58` plus these files (`harness-run-copy/overlay/`: `files.sha256`, `harness.diff`).
The Debian cells ran with overlay `fa65e446…`; H18 was added after Arch run a, so Arch run b ran with `bcac83c2…`
(`*/run-*/host/harness.txt`).

- `build-upd1-artifacts.sh --baseline-ref v0.1.0-alpha.80` (and `run-upd1.sh build` usage): B from the tag + seam, the
  six-file and byte-identical proofs, per-tree web builds, G/D labelled alpha.81. Default mode unchanged.
- `dns-pair-acceptance/scripts/build-dist.sh`: `CELIKPANEL_ACCEPTANCE_GUARD` names the harness commit's guard when the
  built commit has none (the tag predates `release-acceptance-license-guard.sh`; its `deploy/` is not changed to add it).
- `owner_update_trial.py`: `fixture-source --kind baseline-ref`; `configure_labels` sets the alpha.80/81 labels in the
  driver, `current_worker_baseline` and `worker_fixture_origin` from the artifacts document; validation of the
  seam-only document; provenance names the published tag. **H17** `record-running-after-panel-success`: `track` stops
  (verdict observed, a finding) if the Panel says succeeded while the record stays known `running` for 900 s - the
  48d21d58 open point, measured instead of waited on for 90 min (never triggered here). **H18**: with the published
  baseline and mail not required (Arch), the owner chooses `web` (see Arch run a).
- `current_worker_baseline.py`: the guest driver renders version, sequence, policy and floor from the module (defaults
  unchanged). `worker_fixture_origin.py`, `guest_bound_worker.py`: a closed set of two transitions (81->82, 80->81).
- Offline: `test_owner_update_trial.py` 166 OK (6 new upd7 tests), `test_recovery_candidate_archive.py` 11,
  `test_current_worker_baseline.py` 7, `test_worker_fixture_origin.py` 15, `test_bound_worker_reboot.py` 11,
  `test_guest_bound_worker.py` 8 - all OK (`build/offline-*.txt`).

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`; `host/c-drive.txt`)

174.5 GB free before anything (floor 40 GB, stop at 25 GB); 174.4 GB before cell 1; 162.5 GB after the last cell.

## Cells

| Cell | Lab, port | Wrapper (UTC) | Overall | Outcome |
| --- | --- | --- | --- | --- |
| `upd1-debian13-good` run-a | upd7-d13-good-a, 2711 | 11:17:36-11:29:14 | complete-for-review | update-verified |
| `upd1-debian13-owner-continuation` run-a | upd7-d13-oc-a, 2721 | 11:30:26-11:49:17 | complete-for-review | recovered-after-owner-continuation; judge as-expected, 17 rules, 0 findings |
| `upd1-debian13-defective` run-a | upd7-d13-def-a, 2731 | 11:49:50-12:04:03 | complete-for-review | recovered-automatically (rollback_verified) |
| `upd1-arch-good` run-a | upd7-arch-good-a, 2741 | 12:04:48-12:09:42 | failed at setup | not reached (web_mail 05-mail_profile failed) |
| `upd1-arch-good` run-b (H18) | upd7-arch-good-b, 2751 | 12:10:58-12:20:55 | failed at pre-state | not reached (seeded site HTTP 404) |

In every cell: baseline install by the tag's real installer with the tag's unchanged trust enrollment; licence step
`license_service: not contacted: acceptance test build`; setup waits at `access_dns` on the isolated host (observed);
cron is not installed by alpha.80 on Debian (`CRON_NOT_INSTALLED`), so cron is not measured; DNS
`not-provided-external-dns`.

### 1. Good candidate (Debian)

Request `103ae9abc2944440997ce03055540e1a`, owner start 11:28:03.

| UTC | Panel API (alpha.80 until stopped) | Root CLI `/usr/libexec/celikpanel/recovery status` |
| --- | --- | --- |
| 11:28:03-11:28:10 | queued/running; `/api/v1/recovery/status` 404 (alpha.80 has none) | CLI absent (alpha.80 installs none; `status: unavailable`) |
| 11:28:14 | running | present (staged by the candidate updater): `observation=unavailable` |
| 11:28:20 | Panel stopped at 11:28:23 | `known running/none update_running`, recorded 11:28:21 - the record the **candidate updater** created for the alpha.80 worker |
| 11:29:01.7 | old worker writes succeeded; unit deactivated 11:29:01 | |
| 11:29:03 | succeeded; recovery API 200 `succeeded/update_verified` | `succeeded/update_verified`, recorded 11:29:05 |

**The 48d21d58 open point** (record stays `running` after success): not observed - the record moved to
`succeeded` about 4 s after the old worker ended; H17 never fired. One run only.

Terminal: G installed and running, floor and foundation at 81 / 622ddcb4; database equal except the listed volatile
tables; timers (2) equal; firewall equal; site and SMTP never interrupted; Panel down 20-30 s. Card `succeeded`
(`panelUpdate.succeeded`, judge as-expected); screen `Update verified` / `Güncelleme doğrulandı`.

### 2. Owner continuation (Debian) - what the owner sees at the pause

Request `b8a757e6fd0b5d00513f930a1be58819`, start 11:41:26, hold on `127.0.0.1:2083` from 11:41:45.

| UTC | Event (journal / root CLI) |
| --- | --- |
| 11:41:43 | record `running/update_running` (created by the candidate updater) |
| 11:43:10 | old worker logs `!! CELIKPANEL_UPDATE_FAILURE code=panel_start_unverified state=recovery_required` |
| 11:43:11 | attempt 1 (update/completion, forward); CLI `recovering`, `failure_code=panel_start_unverified` |
| 11:44:22 / 11:44:24 | attempt 1 did not finish; CLI `retry_scheduled`, `first_failure_code=panel_start_unverified` |
| 11:44:53, 11:46:37 | attempts 2 and 3 (forward); `retry_scheduled` again 11:45:58 |
| 11:47:49 | Certbot returned to its pre-update state while waiting; CLI `pause_pending` |
| 11:48:20 | pause: `recovery_required/recovery_incomplete`, `automatic_recovery=paused_retry_limit`, `first_failure_code=panel_start_unverified`, `renewal_before_update=on`; journal prints the retry |
| 11:48:42 | owner stops the hold (held 417 s, released before retry) |
| 11:48:44-11:49:07 | the printed command run once, exit 0: `Recovery dispatch admitted: attempt=owner`, `==> Previous pending update finalized from verified snapshot` / `==> Önceki bekleyen güncelleme doğrulanmış snapshot'tan tamamlandı` |
| 11:49:07 | `succeeded/update_verified`, `previous_failure=recovery_failed` (CLI and API) |

The panel log the text names showed five `bind: address already in use` lines (`names_cause: true`). Terminal as in
cell 1 (timers equal, Certbot back as before). Panel down 423-433 s; site and SMTP never interrupted.

**Verbatim at the pause (root CLI; the Request / Recorded-at / observation-note lines are in `side/extract.txt`):**

- EN: The update was applied, but the new version's panel did not come up, and completing the update was retried to its limit. Read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed. There is no supported return to the previous version from this point. Automatic recovery used all three attempts without finishing, so the server may be between versions. The server owner must act: read sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50, fix the cause it names, then run the one-time same-operation retry command shown there. Checking status does not retry recovery. Automatic certificate renewal (Certbot) was stopped for this update; the same recovery journal says whether it was returned to how it was before the update or stays stopped until this operation finishes.
- TR: Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve güncellemenin tamamlanması sınırına kadar yeniden denendi. Sunucudaki panel günlüğünü okuyun: sudo journalctl -u celikpanel-panel -n 50. Bu neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu noktadan önceki sürüme desteklenen bir dönüş yok. Otomatik kurtarma üç denemenin hepsini kullandı ve tamamlanamadı; sunucu iki sürüm arasında kalmış olabilir. Sunucu sahibinin işlem yapması gerekiyor: sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 çıktısını okuyun, belirtilen nedeni giderin ve orada aynı işlem için gösterilen tek seferlik yeniden deneme komutunu çalıştırın. Durum sorgusu kurtarmayı yeniden başlatmaz. Otomatik sertifika yenileme (Certbot) bu güncelleme için durduruldu; aynı kurtarma günlüğü, güncellemeden önceki hâline döndürülüp döndürülmediğini ya da bu işlem bitene kadar durdurulmuş kalacağını söyler.
- Journal (`recovery-journal-at-pause.txt`): `Automatic certificate renewal (Certbot) was returned to its state from before the update while this operation waits; the retry pauses it again before it continues.` / `Otomatik sertifika yenileme (Certbot) bu işlem beklerken güncellemeden önceki durumuna döndürüldü; yeniden deneme devam etmeden önce onu yeniden duraklatır.` and `After resolving the cause, the owner may authorize one same-snapshot retry: sudo /usr/libexec/celikpanel/recovery recover --retry --snapshot 20261001T114142Z-from-unknown-to-622ddcb40568073d1407983bf78d0b8b8e24b0a5-d92ee53a6b2f6f1438b413605326bc2f`.

The `recovering`, `retry_scheduled` and `pause_pending` texts are the same as upd6's (EN/TR in `side/extract.txt`).
During the update the Panel was down, so only the root CLI showed these states.

### 3. Defective candidate + QMP reset (Debian) - return to alpha.80

Request `6e1bec97621a3a13e0d70dbf959acf83`, start 12:01:53. Record `running` 12:02:10 (candidate updater); failure line
12:02:32 `code=update_failed state=recovery_required reason=offline panel database migration failed; its original
database and work evidence are preserved`; attempt 1 (rollback, phase active) 12:02:33; QMP `system_reset` once at
`payload_restored` 12:02:51 (new boot, SSH back in 8.3 s); attempt 2 (operation rollback) 12:03:36; **old Agent started
12:03:48, old Panel ready 12:03:49**; `==> Rollback complete / Geri alma tamamlandı` 12:03:54; CLI
`recovered/rollback_verified`, `previous_failure=update_failed`.

Terminal (passed): installed and running agent/panel are the alpha.80 builds (`version=v0.1.0-alpha.80`,
commit f650e8c1); running = installed; login works; domain and mailbox rows present; database equal except volatile
tables; timers and firewall equal. Web and SMTP were interrupted only by the host reset; Panel down 94-104 s. The old
binaries started and served with the candidate's records on disk (`release-state` floor `81 / v0.1.0-alpha.81` and the
recovery foundation of D stay, as after every earlier rollback, e.g. upd4). Its startup logged only `another server
change or package-manager task is still running` for the mail SNI/milter wiring while recovery finished.

Root CLI texts: recovering - `The update did not finish normally, and automatic recovery is running: …` / `Güncelleme
normal biçimde tamamlanmadı ve otomatik kurtarma çalışıyor: …`; recovered - `The update did not complete, and the server
was returned automatically to the version it ran before; that restoration was verified. Nothing needs to be done on
the server. Do not start the same version again until a corrected version is published; the panel's update page shows
what is known about the cause.` / `Güncelleme tamamlanmadı ve sunucu otomatik olarak güncellemeden önce çalıştırdığı
sürüme döndürüldü; bu geri dönüş doğrulandı. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana
kadar aynı sürümü yeniden başlatmayın; nedenle ilgili bilinenler panelin güncelleme sayfasında gösterilir.`

What the restored alpha.80 Panel shows (finding F1).

### 4. Arch (not measured)

- run-a: alpha.80 accepts the `web_mail` plan on Arch (the source refuses it with `server_setup_service_unsupported:dovecot`)
  and fails at `05-mail_profile`: `install profile service postfix: mail stack configuration: vmail user: open mail root
  parent: not a direct…` -> `service_install_failed`. A failed setup is not retried on the same guest.
- run-b (H18, purpose `web`): setup waits at `access_dns` as usual; the seeded static site answers **HTTP 404** before
  the update (`workload-pre-update.json`), so `pre-state` fails and no update starts. Side record: `/var/www` is `0750
  root:celikpanel`, nginx runs as `http`, no hosting-root receipt exists. The exact cause was not established.
- Not re-run again (one harness re-run per cell). The owner-continuation and defective Arch cells were not started:
  they need the same pre-state.

## Findings

| # | Class | Finding | Evidence |
| --- | --- | --- | --- |
| F1 | incompatibility with alpha.80 (owner-visible) | After the automatic return, the alpha.80 Panel has no recovery reader (`/api/v1/recovery/status` 404). Its update status is `failed` with the updater's raw line as `summary` (`…state=recovery_required reason=offline panel database migration failed…`, 221 chars), which alpha.80's card shows verbatim as the failure message (`web/src/components/SystemUpdateOperation.tsx:527-528` at the tag), while the server is in fact `recovered/rollback_verified`. Its update check offers the same defective v0.1.0-alpha.81 again (`available: true`); only the root CLI says not to start it again. The harness cannot model alpha.80's card (no `lib/systemUpdateOutcome.ts`): card judge `unknown`. | `upd1-debian13-defective/run-a/steps/13-terminal/step.json` (`update_card.status`, `.check`, `.recovery`) |
| F2 | incompatibility with alpha.80 (expected, recorded) | For about 11 s after the owner's start the root CLI does not exist (alpha.80 installs none); then about 6 s `observation=unavailable` ("the result … is unknown … check this same request again in a minute"); then the self-produced `running` record. Same in all three Debian cells. | `*/run-a/side/extract.txt`, `steps/11-track/samples/0001-0006.json` |
| F3 | product fix confirmed (candidate) | With an alpha.80 worker the candidate updater's own initial record let the runner bind and record the typed cause, `retry_scheduled`, `pause_pending`, the pause with `first_failure_code` and the printed retry (cell 2), and the rollback states (cell 3). | cell 2 judge, `result.json` |
| F4 | published-baseline limitation (Arch) | alpha.80 on Arch: web_mail setup fails at 05-mail_profile; with web only, the seeded site is HTTP 404. Not a candidate defect; the source has changed since (upd6 Arch served the site). | `upd1-arch-good/run-a`, `run-b` |
| F5 | fixture limitation | Debian alpha.80 installs no cron (`CRON_NOT_INSTALLED`): cron continuity not measured. | `steps/07-seed/seeded.json` |
| F6 | harness defect, fixed (H18) | Arch run a: the driver fell back from web_mail only on a refused plan, not on an accepted plan that fails. | run-a `steps/06-setup` |

No product defect in the candidate was found by these cells. The 48d21d58 open point did not appear (record resolved
about 4 s after the old worker ended).

## Real origin, licence, secrets

- Each baseline installer log names neither `celikpanel.net` nor `185.95.` (`secret-scan.txt`); every origin check
  resolved `celikpanel.net` only to `127.0.0.1` with fixture HTTP 200 (`result.json` `scope.origin`).
- Licence: `not contacted: acceptance test build`; every Panel start (alpha.80 and candidate) logs `ACCEPTANCE FIXTURE
  — NOT FOR PRODUCTION: … never contacts the license service …`.
- `secret-scan.txt` (tools `scan7.py`, `scan.sh`) over the whole folder before hashing: 0 PEM private-key blocks, 0 hits
  for the 286 body lines of the 20 lab key files, 0 unredacted password/secret/token/cookie fields, 528 redaction
  markers, 0 tokens of the 32-character shape; the 7 tokens of the admin-password shape are upd4 file names in the
  run-copy hash list. The one fixture-licence literal hit is `build/fixture-patches.diff` line 75: the seam patch
  carries `AcceptanceFixtureKey`, the constant committed in `internal/licensing/acceptance_fixture.go` since 01a450e6
  (public source, accepted only by the acceptance-tag build on a marked guest). `185.95.` appears only in this README,
  the scan scripts and the scan output. Longest repository-relative path: 196 characters.
- `SHA256SUMS` lists every file except itself (`sha256sum -c SHA256SUMS`).

## Files

`build/` (artifacts document, dist JSONs, baseline-ref proof, agent package list, fixture commits and patches, seam
adaptation diff, prove, dry runs, build and offline logs); `harness-run-copy/` (run-copy hashes of 48d21d58, overlay
files and diff, jobs, setup scripts); `tools/` (host readers, waiters, extract, scan, stage); `host/` (host check,
`C:` readings, leftovers); one folder per cell run with the driver's evidence unchanged (its own `SHA256SUMS` verified
after copying), `host/` (wrapper logs, job, harness overlay, lab identity, baseline installer result and log, origin
intent and manifest, fixture plan) and `side/extract.txt`.

## Host leftovers (archlinux WSL; nothing deleted; `host/host-leftovers.txt`)

`/var/tmp/cp-upd7-run` (360 MB), `/var/tmp/cp-release-drill-upd7-{d13-good-a,d13-oc-a,d13-def-a,arch-good-a,arch-good-b}`
(2.2-2.5 GB each, guests stopped, overlays and evidence kept), `/var/tmp/cp-upd1-build/20261001t111438z` (700 MB),
`/var/tmp/cp-pair-accept/dist/{f650e8c1…,622ddcb4…,205abc64…}-acceptance-license` (133 MB, 676 MB, 676 MB). No QEMU
process remains. Repository git configuration unchanged; the repository writes are the harness files above and this
folder.

## What this run does NOT prove

- The published alpha.80 **bytes**: B is the tag's source rebuilt here, with the licence seam and a fixture-tagged
  `bin/panel`; the Agent and scripts are the tag's source but not the signed release archive (`a29bd22d…`), and the
  installed build identity is the fixture commit f650e8c1, not bd14d97e.
- Production signing, the real release origin, licence behaviour, browser rendering (cards and screens are modelled;
  alpha.80's card could not be modelled at all), DNS, certificate issuance or renewal execution, power loss.
- Any Arch path from alpha.80; the start-check and real-start kinds; management-off; the Arch kill fault.
- That the record never stays `running` after success: one good update, resolved in about 4 s.
- Repeatability: one run per Debian cell.
