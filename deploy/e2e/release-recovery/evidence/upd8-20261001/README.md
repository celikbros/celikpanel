# upd8 native run, 2026-10-01: the update from the published v0.1.0-alpha.80 on Ubuntu 24.04

Roadmap item 3, owner-started update acceptance (`owner_update_trial.py`), eighth run. upd7 measured the update from the
published tag `v0.1.0-alpha.80` on Debian 13 only; there was no update evidence on Ubuntu at all, although one of the
owner's installed servers runs Ubuntu. This run repeats upd7's method on a one-node **Ubuntu 24.04 LTS** guest: baseline
B = tag `v0.1.0-alpha.80` + the D-027 licence seam, candidates built from the source `e9e2d3f3`.

Disposable QEMU/KVM guests on the local `archlinux` WSL host only. No installed server (Boston, Frankfurt, any) was
touched or contacted. Nothing was pushed, committed, published or signed with a production key. `celikpanel.net`
resolved only to the guest-loopback fixture origin; no licence service was contacted. Every `result.json` carries
`native_evidence: false`; the owner judges the P0 rows. **It closes no P0 row.**

**Result in one sentence:** on Ubuntu 24.04 all three cells ended `complete-for-review` - the good update was verified,
the owner continuation paused on its typed first cause and completed after the owner's one printed retry, and the
defective candidate (with a QMP reset at `payload_restored`) returned automatically to the alpha.80 binaries, which
started and served - but alpha.80's own **server setup cannot finish in one owner attempt on a stock Ubuntu 24.04
cloud image** (finding F1: PackageKit), so every cell needed seven owner setup attempts (about 46 minutes) before the
workload existed.

## Feasibility (step 1)

| Question | Answer | Evidence |
| --- | --- | --- |
| Ubuntu release alpha.80 supports | 24.04. Neither the tag's nor HEAD's `install.sh` has a release allowlist: `package_hint_for_token` maps `debian\|ubuntu` to apt (tag `install.sh:1003-1009`); `internal/hostplatform/hostplatform.go:425-428` maps ubuntu to the Debian family; the tag's README names "Boston/NS2 on Ubuntu 24.04" as the tested fixture. | `host/image-provenance.txt` |
| Image | Already on the host (no download): `/var/tmp/cp-install-vm/images/ubuntu-24.04-20260826.img`, 624 829 952 bytes, SHA-256 `d0fe84bb5f80853425fa6be28e2c106f30104c3cfe8611933f2e65c9b63f0e30` = line 52 of the published `https://cloud-images.ubuntu.com/releases/noble/release-20260826/SHA256SUMS` (`ubuntu-24.04-server-cloudimg-amd64.img`). Only `SHA256SUMS` and `SHA256SUMS.gpg` were fetched; the GPG signature was **not** verified (signing key not on the host). Hard-linked into the image cache under the pinned name. | `host/image-provenance.txt`, `build/ubuntu-noble-release-20260826.*` |
| Harness for an `ubuntu` node | A one-node lab variant, three cells, five node allowlists widened (below). Ubuntu needs no package-name, unit or firewall change in the harness: `cron.service`, `certbot.timer`, `nftables`, `/usr/local/share/ca-certificates` + `update-ca-certificates` are the Debian ones. | `harness-run-copy/overlay/` |
| Does alpha.80's installer complete? | Yes, in about 50 s, every time (the installer's `needrestart` restarts `packagekit.service`). | `*/run-*/host/current-worker-baseline-ubuntu-current-worker-baseline.log` |
| Which setup profile works? | `web_mail` is accepted, but **no profile completes in one attempt** (F1). With the owner waiting about 5 min and starting a newly reviewed plan after each stop, as the product's `HOST_MUTATION_BUSY` text says, `web_mail` reaches the isolated host's `access_dns` wait on the 7th attempt. | `*/run-*/steps/06-setup/step.json` (`owner_attempts`, `owner_idle_waits`) |

## What was built from what

`run-upd1.sh build --baseline-ref v0.1.0-alpha.80 e9e2d3f3` from the run copy, 12:57:17Z-12:59:19Z, exit 0
(`build/build.*`). Clone `/var/tmp/cp-upd1-build/20261001t125717z/repo`, `go1.26.5 linux/amd64`.

| Role | Label / sequence | Commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- |
| Baseline B = tag `v0.1.0-alpha.80` + licence seam | v0.1.0-alpha.80 / 80 | 57f4372c0485cc2e568b45d5b9c05785d3633320 (parent bd14d97e) | ee6d75f688fac425179ee646a647a0ceb68283bc | 7d95ab7a92639081b888bd8f3bfee08f9454ef380186a9a233f4b717add6abf7 |
| Good G = source e9e2d3f3 + policy | v0.1.0-alpha.81 / 81, previous 80 = B | 8c71415e56a2b9a13232377d501cf4bb8a23a9c8 | 1485ffad8d3a0238a4e93cf0358dc351ecb7ae38 | 8ccd0f25b8ccad2641b63ccfb9dad0ed54584f327947839f1bc9fde2eab86821 |
| Defective D = G + migrate-only defect | v0.1.0-alpha.81 / 81 | 80d7a43fdd326d7155da9d671836ba1e054b07b3 | 67dcf9af537a3ab55b466c5f4751ba5dffbb10b9 | 5b7bea0946796a82c7e4bb88d4cdfa862c48316067fde58068274687a51bc5d4 |

- B's tree `ee6d75f6…` is the same tree as upd7's B (same tag, same seam); its commit and archive differ only by commit
  metadata and build time. All three are `license_mode: acceptance-fixture` (`build/dist-*.json`).
- `prove` exited 0 (`build/prove.*`); three dry runs exited 0 and created no lab (`build/dry-run-*`).
- **Unchanged-file proof** (`build/baseline-ref-proof.txt`, 31 `identical` lines; the build refuses any `DIFFERENT`):
  `git diff` tag..B names exactly the six seam files; the trees `cmd/agent`, `deploy/`, `download-portal/`, `web/`,
  `internal/transport` are identical; blob and SHA-256 equal at tag and B for e.g. `update.sh` 703263a6…,
  `rollback.sh` e559f741…, `download-portal/get.sh` e8cf0260…, `bootstrap-update.sh` 022ea8c6…,
  `bootstrap-prebuilt-update.sh` 14341769…, `deploy/finalize-pending-update.sh` f86a87e0…,
  `deploy/finalize-pending-rollback.sh` 275af544…; the Agent's package closure (270 packages, `build/agent-deps.txt`)
  contains no licensing package. Full seam patch: `build/fixture-patches.diff`.

## Harness changes (working tree, `deploy/e2e/release-recovery/`; offline suites green)

The run copy is `git archive e9e2d3f3` (`harness-run-copy/runcopy-e9e2d3f3-files.sha256`) plus the files in
`harness-run-copy/overlay/` (`files.sha256`, `harness.diff` `df357c48…`, identical to the working tree at the end).

- `lab.py --platform ubuntu`, `images-ubuntu.lock.json`: a one-node lab from the pinned image (only a
  `cloud-images.ubuntu.com/releases/noble/release-*/…img` URL with its published SHA-256 is accepted); the node uses the
  fixture's own layout (`ubuntu`, sudo group, `ssh.service`, MACs `…:12`, peer address 192.0.2.12/24 on a listening,
  unconnected peer link); `load_plan` validates a one-node plan; the Debian+Arch default is unchanged.
  `run-upd1.sh cell upd1-ubuntu-*` passes `--platform ubuntu`.
- `owner_update_trial.py`: cells `upd1-ubuntu-good`, `-owner-continuation`, `-defective` (as Debian's: mail required,
  reset at `payload_restored`). **H19** (the PackageKit wait, Ubuntu only): before each setup start, the seed and the
  arm, the owner waits (read-only, ≤ 900 s) until none of the Agent's own package-manager process names
  (`linuxPackageProcessBusyAt`) runs and no apt/dpkg lock is held; after a stopped setup attempt the owner reads the
  cause - the setup error, else the failed step's component operation
  (`GET /api/v1/service/operation?id=…`), else the panel log line that names that operation or step - and only when
  it is `another server change or package-manager task is still running` waits and starts a newly reviewed plan
  (≤ 12 attempts). Any other failure stops the cell as before.
- `current_worker_baseline.py`, `worker_fixture_origin.py` (CA path as Debian), `guest_probe.py`,
  `guest_owner_port_hold.py`: `ubuntu` added to the closed node sets.
- Tests: `test_lab.py` 15 (3 new), `test_owner_update_trial.py` 169 (3 new + the cell list),
  `test_worker_fixture_origin.py` 15 (ubuntu trust path; `rhel9` and `../ubuntu` still refused),
  `test_recovery_candidate_archive.py` 11, `test_current_worker_baseline.py` 7, `test_bound_worker_reboot.py` 11,
  `test_guest_bound_worker.py` 8 - all OK (`build/offline-*.txt`, final overlay).

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`; `host/c-drive.txt`)

150.5 GB free before anything (floor 40 GB, stop at 25 GB); 148.1 GB before cell 1; 132.4 GB after the last cell.

## Cells

| Cell | Lab, port | Wrapper (UTC) | Overlay | Overall | Outcome |
| --- | --- | --- | --- | --- | --- |
| `upd1-ubuntu-good` run-a | upd8-ub-good-a, 2811 | 12:59:49-13:01:25 | f155a309 (no H19) | failed at setup | `HOST_MUTATION_BUSY` at 02-service (F1, F6) |
| `upd1-ubuntu-good` run-b | upd8-ub-good-b, 2821 | 13:19:07-13:42:49 | 059e1054 (H19: typed code) | failed at setup | attempt 4: `mail_profile_install_failed` (cause only in the component/log) |
| `upd1-ubuntu-good` run-c | upd8-ub-good-c, 2831 | 13:45:58-14:09:50 | 051f5f48 (+ component op) | failed at setup | attempt 4: the component operation shows no cause either |
| `upd1-ubuntu-good` run-d | upd8-ub-good-d, 2841 | 14:11:36-15:01:18 | df357c48 (+ panel log) | complete-for-review | update-verified |
| `upd1-ubuntu-owner-continuation` run-a | upd8-ub-oc-a, 2851 | 15:02:26-15:59:55 | df357c48 | complete-for-review | recovered-after-owner-continuation; judge as-expected, 17 rules, 0 findings |
| `upd1-ubuntu-defective` run-a | upd8-ub-def-a, 2861 | 16:00:17-16:50:43 | df357c48 | complete-for-review | recovered-automatically (rollback_verified) |

**Cell 1 was run four times, beyond the one re-run the method allows.** Runs b and c each fixed one more surface of the
same harness defect (F6: the driver's owner did not follow the product's "wait and try again" through every place
the same PackageKit cause shows up); no product or fixture changed between runs. Runs a-c are kept as evidence.

In every final cell: baseline install by the tag's real installer with the tag's unchanged trust enrollment (≈ 50 s);
licence `license_service: not contacted: acceptance test build`; seven setup attempts (below), then the isolated host's
`access_dns` wait (06-panel_certificate, 07-access_dns, 08-mail_certificate, 09-verify not run); DNS
`not-provided-external-dns`; **cron available and seeded** (`cron.service`, `/usr/bin/crontab`) - unlike Debian;
the owner's idle waits before seed and arm were 0.2 s (nothing running).

### Setup on Ubuntu (identical in run-d, owner-continuation and defective)

| Attempt | Stops at | Shown code | Where the cause is | Owner wait before next |
| --- | --- | --- | --- | --- |
| 1 | 03-service (php-fpm, after nginx) | `HOST_MUTATION_BUSY` | setup error | ≈ 287-300 s |
| 2 | 03-service (mariadb) | `HOST_MUTATION_BUSY` | setup error | ≈ 297-300 s |
| 3 | 03-mail_profile (webmail) | `HOST_MUTATION_BUSY` | setup error | ≈ 297-308 s |
| 4 | 02-mail_profile webmail, phase `profile/webmail/mail-tls` | `mail_profile_install_failed` | panel log only: `service operation … (webmail) failed in profile/webmail/mail-tls: mail TLS synchronization: another server change or package-manager task is still running` | ≈ 288-308 s |
| 5 | 03-mail_profile protected-mail, `…/mail-tls` | `mail_profile_install_failed` | panel log only (same text, protected-mail) | ≈ 287-289 s |
| 6 | 05-firewall | `server_setup_firewall_failed` | panel log only: `server setup step 05-firewall failed: another server change or package-manager task is still running` | ≈ 288-309 s |
| 7 | `access_dns` wait (isolated host) | `server_setup_access_dns_required` | - | - |

Every wait saw only `packagekitd` (`owner_idle_waits.busy_seen`). Setup took 45-46 min per cell.

### 1. Good candidate (run-d)

Request `ff796a0375e7d1cceb0e3e462f69da1a`, owner start 15:00:13.

| UTC | Panel API (alpha.80 until stopped) | Root CLI `/usr/libexec/celikpanel/recovery status` |
| --- | --- | --- |
| 15:00:13-15:00:18 | running; `/api/v1/recovery/status` 404 (alpha.80 has none) | absent (`status: unavailable`) |
| 15:00:22-15:00:29 | running; 404 | `observation=unavailable` |
| 15:00:32 | Panel and Agent stopped | `known running/none update_running`, recorded 15:00:30 (created by the candidate updater) |
| 15:00:55-15:00:57 | Agent started, Panel ready (candidate) | |
| 15:01:05 | running; recovery API 200 `running` | running |
| 15:01:06 | succeeded; recovery API 200 `succeeded/update_verified` | `succeeded/update_verified`, recorded 15:01:08 |

The old worker unit ended 15:01:07.49; the record said `succeeded` at 15:01:08 - **the record did not stay `running`
after success** (H17 never fired; one run). Terminal: G installed and running (`v0.1.0-alpha.81`, 8c71415e), running =
installed, floor 81; database equal except volatile tables; timers (3) equal; firewall equal; login works; domain,
mailbox and cron rows present; site, SMTP and cron never interrupted; Panel down 20-30 s. Card `panelUpdate.succeeded`
(judge as-expected).

### 2. Owner continuation - what the owner sees at the pause

Request `ba190b5f2c470a93945ecd4b22b24f35`, start 15:52:15, port hold on `127.0.0.1:2083` from 15:52:32.

| UTC | Event (journal / root CLI) |
| --- | --- |
| 15:52:30 | record `running/update_running` (candidate updater) |
| 15:53:55 | old worker: `!! CELIKPANEL_UPDATE_FAILURE code=panel_start_unverified state=recovery_required reason=the new panel did not stay running and answer on its own address after the update was applied` |
| 15:53:56 | attempt 1 (update/completion, forward); CLI `recovering`, `failure_code=panel_start_unverified` |
| 15:55:04 | `retry_scheduled`, `first_failure_code=panel_start_unverified` |
| 15:55:35, 15:57:15 | attempts 2 and 3 (forward); `retry_scheduled` again 15:56:36 |
| 15:58:24 | Certbot returned to its pre-update state while waiting; CLI `pause_pending` (15:58:25) |
| 15:58:55 | pause: `recovery_required/recovery_incomplete`, `automatic_recovery=paused_retry_limit`, `first_failure_code=panel_start_unverified`, `renewal_before_update=on`; journal prints the retry |
| 15:59:20 | owner stops the hold (held 408.8 s, released before retry) |
| 15:59:20-15:59:45 | printed command run once, exit 0: `Recovery dispatch admitted: attempt=owner`, `==> Previous pending update finalized from verified snapshot` |
| 15:59:45 | `succeeded/update_verified`, `previous_failure=recovery_failed` (CLI and API) |

The panel log named the cause (`listen tcp :2083: bind: address already in use`). Terminal as cell 1 (timers equal,
Certbot as before: `renewal_before_update` verdict `as-before`); site, SMTP and cron never interrupted; Panel down
416-426 s. During the update the Panel was down, so only the root CLI showed these states.

**Verbatim at the pause (root CLI, 15:58:55):**

- EN: The update was applied, but the new version's panel did not come up, and completing the update was retried to its limit. Read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed. There is no supported return to the previous version from this point. Automatic recovery used all three attempts without finishing, so the server may be between versions. The server owner must act: read sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50, fix the cause it names, then run the one-time same-operation retry command shown there. Checking status does not retry recovery. Automatic certificate renewal (Certbot) was stopped for this update; the same recovery journal says whether it was returned to how it was before the update or stays stopped until this operation finishes.
- TR: Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve güncellemenin tamamlanması sınırına kadar yeniden denendi. Sunucudaki panel günlüğünü okuyun: sudo journalctl -u celikpanel-panel -n 50. Bu neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu noktadan önceki sürüme desteklenen bir dönüş yok. Otomatik kurtarma üç denemenin hepsini kullandı ve tamamlanamadı; sunucu iki sürüm arasında kalmış olabilir. Sunucu sahibinin işlem yapması gerekiyor: sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 çıktısını okuyun, belirtilen nedeni giderin ve orada aynı işlem için gösterilen tek seferlik yeniden deneme komutunu çalıştırın. Durum sorgusu kurtarmayı yeniden başlatmaz. Otomatik sertifika yenileme (Certbot) bu güncelleme için durduruldu; aynı kurtarma günlüğü, güncellemeden önceki hâline döndürülüp döndürülmediğini ya da bu işlem bitene kadar durdurulmuş kalacağını söyler.
- Journal (`steps/12-owner-continuation-required/recovery-journal-at-pause.txt`): `Automatic certificate renewal (Certbot) was returned to its state from before the update while this operation waits; the retry pauses it again before it continues.` / `Otomatik sertifika yenileme (Certbot) bu işlem beklerken güncellemeden önceki durumuna döndürüldü; yeniden deneme devam etmeden önce onu yeniden duraklatır.` and `After resolving the cause, the owner may authorize one same-snapshot retry: sudo /usr/libexec/celikpanel/recovery recover --retry --snapshot 20261001T155229Z-from-unknown-to-8c71415e56a2b9a13232377d501cf4bb8a23a9c8-aafbd208772c412c4cf8636b654a3d6d`.

The `recovering`, `retry_scheduled` and `pause_pending` texts (EN/TR) are in `side/extract.txt`; they equal upd7's.

### 3. Defective candidate + QMP reset - return to alpha.80

Request `a4795fd7c17f70edaf8a920830de5a98`, start 16:48:12. Record `running` 16:48:31; failure line 16:49:00
`code=update_failed state=recovery_required reason=offline panel database migration failed; its original database and
work evidence are preserved`; attempt 1 (rollback, phase active) 16:49:01; QMP `system_reset` once at
`payload_restored` 16:49:22 (new boot, SSH back in 11.8 s); attempt 2 (operation rollback) 16:50:11; **old Agent
started 16:50:24, old Panel ready 16:50:26**; `==> Rollback complete / Geri alma tamamlandı` 16:50:31; CLI
`recovered/rollback_verified`, `previous_failure=update_failed` (16:50:31).

Terminal (passed): installed and running Agent and Panel are the alpha.80 builds (`version=v0.1.0-alpha.80`, commit
57f4372c); running = installed; login works; domain, mailbox and cron rows present; database equal except volatile
tables; timers and firewall equal. Web and SMTP were interrupted only by the host reset (≤ 24 s and ≤ 19 s); cron
never; Panel down 108-118 s. The candidate's floor `81 / v0.1.0-alpha.81` stays on disk, as after every earlier
rollback. Root CLI texts: the same `recovering` and `recovered` texts as upd7 (`side/extract.txt`).

What the restored alpha.80 Panel shows: update status `failed` with the updater's raw line as `summary`, recovery API
404, update check `available: true` for the same defective v0.1.0-alpha.81; card judge `unknown` (alpha.80 has no
`lib/systemUpdateOutcome.ts`) - upd7 finding F1, now also on Ubuntu (F3 below).

## Findings

| # | Class | Finding | Evidence |
| --- | --- | --- | --- |
| F1 | alpha.80 incompatibility with stock Ubuntu 24.04 (owner-visible; platform interaction) | Ubuntu's cloud image ships PackageKit; apt's `DPkg::Post-Invoke` hook (`/etc/apt/apt.conf.d/20packagekit`) starts `packagekitd` after every package operation and it exits ≈ 300 s later (probe c: started 13:09:53, gone 13:15:11). alpha.80's Agent counts `packagekitd` as a package-manager task (`cmd/agent/service_mutation_lock_linux.go:586` at the tag) and its setup runner fails the step instead of waiting (`cmd/panel/server_setup_operations.go:777-792`). Each setup package step therefore blocks the next one, and the mail profile blocks **itself**: its own package install starts PackageKit, its own `mail-tls` sub-step is then refused, and the owner sees only `mail_profile_install_failed` / `server_setup_firewall_failed` (`:1223-1226`) - the cause is only in the panel log. An owner following "wait and try again" needs 7 attempts and ≈ 46 min. The source still lists `packagekitd` (`cmd/agent/service_mutation_lock_linux.go:446`) and has a newer advisory readiness read (`cmd/panel/host_mutation_readiness.go`); **the source's setup on Ubuntu was not measured.** | `*/run-*/steps/06-setup/step.json`, `upd1-ubuntu-good/run-a/steps/06-setup/setup-execution.json`, `host/packagekit-probe3.out.txt` |
| F2 | risk, not measured (owner-visible) | The update start is refused while `packagekitd` runs: alpha.80 `cmd/agent/system_update_store_linux.go:224-229` ("the host package manager is active"); the source keeps it (`:230-235`) and `update.sh:291-301` reports it as `package_manager_busy state=unchanged`. On Ubuntu any `apt`/`dpkg` run (including `apt-get update`) keeps PackageKit ≈ 5 min. Here the owner waited for idle before the start (0.2 s, nothing running), so this refusal was never exercised. | code only |
| F3 | incompatibility with alpha.80 (= upd7 F1) | After the automatic return the alpha.80 Panel shows the raw failure line, no recovery reader and offers the same defective alpha.81 again; only the root CLI says not to start it. | `upd1-ubuntu-defective/run-a/steps/13-terminal/step.json` (`update_card`) |
| F4 | incompatibility with alpha.80 (expected, = upd7 F2) | About 10 s after the owner's start without the root CLI (alpha.80 installs none), then about 5-8 s `observation=unavailable`, then the candidate updater's `running` record (recorded 15:00:30, 15:52:30, 16:48:31); all three cells. | `*/side/extract.txt` |
| F5 | product fix confirmed (candidate) | With an alpha.80 worker on Ubuntu the candidate updater's own initial record let the runner record the typed cause, `retry_scheduled`, `pause_pending`, the pause with `first_failure_code`, the printed retry (cell 2) and the rollback states (cell 3). | cell 2 judge, `result.json` |
| F6 | harness defect, fixed (H19 in three increments, runs a-c) | The driver's owner did not wait and retry on the PackageKit cause: run a at the typed `HOST_MUTATION_BUSY`, run b at the mail profile's generic code, run c because the component operation names no cause either. | `upd1-ubuntu-good/run-a`…`run-c` |
| F7 | fixture limitation | One isolated node: DNS external, setup waits at `access_dns` (panel and mail certificates, verify not run); the peer link is unconnected. | `result.json` `scope` |

No product defect in the candidate was found by these cells. The record did not stay `running` after success
(about 0.5 s after the old worker ended).

## Real origin, licence, secrets

- Each baseline installer log names neither `celikpanel.net` nor `185.95.` (`secret-scan.txt`); every origin check
  resolved `celikpanel.net` only to `127.0.0.1` with fixture HTTP 200 (`result.json` `scope.origin`).
- Licence: `not contacted: acceptance test build`; every Panel start logs `ACCEPTANCE FIXTURE — NOT FOR PRODUCTION: …`.
- The guests used Ubuntu's package mirrors during install and setup (as the Debian guests use Debian's). The host
  fetched only the two `SHA256SUMS` files above.
- `secret-scan.txt` (tools `scan7.py`, `scan.sh`) over the whole folder before hashing: 0 PEM private-key blocks, 0 hits
  for the 355 body lines of the 27 lab key files (SSH keys of the six cell and three probe labs, fixture signing, CA
  and TLS keys), 0 unredacted password/secret/token/cookie fields, 866 redaction markers. The one fixture-licence
  literal is `build/fixture-patches.diff` line 75 (the committed `AcceptanceFixtureKey` of the seam, as in upd7); the 7
  tokens of the admin-password shape are upd4 file names in the run-copy hash list; the one token of the 32-character
  shape is part of the public PGP signature `build/ubuntu-noble-release-20260826.SHA256SUMS.gpg`. `185.95.` appears
  only in this README, the scan scripts and the scan output. Longest repository-relative path: 194 characters.
- `SHA256SUMS` lists every file except itself (`sha256sum -c SHA256SUMS`).

## Files

`build/` (artifacts document, dist JSONs, baseline-ref proof, agent package list, fixture commits and patches, seam
adaptation diff, prove, dry runs, build and offline logs, Ubuntu `SHA256SUMS` copies); `harness-run-copy/` (run-copy
hashes of e9e2d3f3, final overlay files and diff, jobs with each run's overlay hash, setup scripts); `tools/` (host
readers, waiters, extract, scan, stage, diagnosis and PackageKit probe scripts); `host/` (host check, image
provenance, `C:` readings, leftovers, PackageKit probe logs); one folder per cell run with the driver's evidence
unchanged (its own `SHA256SUMS` verified after copying), `host/` (wrapper logs, job, overlay hash, lab identity,
baseline installer result and log, origin intent and manifest, fixture plan) and `side/extract.txt`.

## Host leftovers (archlinux WSL; nothing deleted; `host/host-leftovers.txt`)

`/var/tmp/cp-upd8-run` (373 MB), `/var/tmp/cp-upd8-img` (16 KB), `/var/tmp/cp-upd8-quick` (341 MB, offline-test copy),
`/var/tmp/cp-release-drill-upd8-{ub-good-a,ub-good-b,ub-good-c,ub-good-d,ub-oc-a,ub-def-a}` (1.3-2.2 GB each) and
`…-upd8-probe-{a,b,c}` (0.6-1.0 GB; PackageKit probes on fresh guests), guests stopped, overlays and evidence kept;
`/var/tmp/cp-upd1-build/20261001t125717z` (713 MB); `/var/tmp/cp-pair-accept/dist/{57f4372c…,8c71415e…,80d7a43f…}-acceptance-license`
(133 MB, 689 MB, 689 MB); the hard link `/var/tmp/cp-v3n28/images/ubuntu-24.04-server-cloudimg-amd64-20260826.img`.
No QEMU process remains. Repository git configuration unchanged; the repository writes are the harness files above and
this folder.

## What this run does NOT prove

- The published alpha.80 **bytes** (B is the tag's source rebuilt with the licence seam; installed identity 57f4372c).
- That the **source's** server setup works on Ubuntu (only alpha.80's setup ran; F1), or that an owner's update start
  succeeds while PackageKit runs (F2, never exercised).
- The owner's real Ubuntu server: hosting-provider images may or may not ship PackageKit, `needrestart` or
  `unattended-upgrades`; this is the stock 20260826 cloud image only, its signature not verified.
- Production signing, the real release origin, licence behaviour, browser rendering (cards are modelled; alpha.80's
  card could not be), DNS, panel/mail certificate issuance or renewal execution, power loss.
- That the record never stays `running` after success: one good update, resolved in about 0.5 s.
- Repeatability: one final run per cell (the setup pattern repeated identically in all three).

## Corrections after intake (2026-10-10)

After this directory was published (2026-10-10), the operator's machine name was replaced by `<operator-host>` in the SSH public-key comments (and in the text that named it) of this directory: 6 occurrences in 6 files; the key material itself, a lab public key, is unchanged.
After this directory was published (2026-10-10), 14 digest values of the one-shot update-transaction token (12 in plain text in 7 files, as `transaction_token_sha256` values and as `.release-db-migrations/<digest>` directory names; 2 inside base64 `events_base64` text in 1 file) were replaced by `[REDACTED-SHA256]`.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.

Checksum repair (2026-10-10): `sha256sum -c SHA256SUMS` failed for the files named here because the retained bytes were no longer the bytes the list was written from. The retained files had been normalised from CRLF to LF when they were first committed (git `autocrlf`); the list records the hashes of the collected CRLF form. The CRLF form was restored byte for byte (every line ends in CR LF, including the last); the recorded hashes were not changed, and the restored files hash to them. Files: `host/c-drive.txt` (the first line carries the UTF-8 byte order mark it was collected with). The `README.md` line of the list was regenerated because this paragraph was added to the README.
