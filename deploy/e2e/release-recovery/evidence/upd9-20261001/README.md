# upd9 native run, 2026-10-01: does an idle Ubuntu `packagekitd` pass the new package-activity rule (efcba145)?

Roadmap item 3, owner-started update acceptance (`owner_update_trial.py`), ninth run. Commit `efcba145` ("Package
activity: an idle PackageKit daemon is not a package task") changed the Agent's rule so that `packagekitd` counts as
busy unless `/proc` shows that it runs the APT backend, has no child and holds or waits for no apt/dpkg lock. It was
verified by component tests only; that an idle Ubuntu daemon matches the rule was inferred
([RESILIENCE-CONTRACT](../../../../../docs/RESILIENCE-CONTRACT.md), "An idle PackageKit daemon is not package-manager
activity", Open). This run measures it on stock Ubuntu 24.04 with [upd8](../upd8-20261001/README.md)'s one-node lab,
image and method.

Disposable QEMU/KVM guests on the local `archlinux` WSL host only. No installed server (Boston, Frankfurt, any) was
touched or contacted. Nothing was pushed, committed, published or signed with a production key. `celikpanel.net`
resolved only to the guest-loopback fixture origin; no licence service was contacted. Every `result.json` carries
`native_evidence: false`; the owner judges the P0 rows. **It closes no P0 row.** The repository HEAD moved to
`fb04289b` (a web-only commit of another session) during the run; the run copy and every build are from `efcba145`.

**Result in one sentence:** the inference is **false** on stock Ubuntu 24.04 - its PackageKit 1.2.8 loads the APT
backend as `libpk_backend_apt.so`, not the `libpk_backend_aptcc.so` the rule looks for (`grep -c aptcc` = 0 in all 88
readings of a running daemon), so an idle `packagekitd` still counts as package activity for its whole ~305 s life:
the candidate's own fresh install refuses its first setup service step 8 s after the owner starts setup (cell A), setup
needs the same seven owner attempts as alpha.80 (cell B), and an update start right after an apt run is refused while
readiness stays busy until the daemon exits ~305 s later (cell B); the refusal during real package activity holds
(cell B) and the update from the alpha.80 baseline to the candidate is verified (cell C).

## What was built from what

Both builds by `run-upd1.sh build` from the run copy (`build/{cur,a80}/build.*`), clones under
`/var/tmp/cp-upd1-build/<stamp>/repo`, `go1.26.5 linux/amd64`, `license_mode: acceptance-fixture` for every archive.

| Build | Role | Label / sequence | Commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- | --- |
| cur `20261001t174240z` (17:42:40-17:45:57Z, cells A, B) | Baseline B = efcba145 + policy | v0.1.0-alpha.81 / 81 | 57cec4f6a51af35ab9ebf1a8c96ba29c60c8f185 | 97917f7e622f6f6668aa427d2903bd86ab1e2bd1 | 78a3659337028b9a76671e3fc3fc8e9137883c25af954f8ea4d09bee4e78a4bf |
| | Good G = B + policy | v0.1.0-alpha.82 / 82 | 00030d16b1f912d104fce582a6b15c24b45f159d | 18b949c912fe36d11b07dc7d09fd43491517adc8 | 1f8025050698f158b8658f12539751ca55e6d9e5a9df00cf5b48c5c0800c9eb2 |
| | (D, S, R built, not used) | v0.1.0-alpha.82 / 82 | ae3e1582…, f4452094…, eb89b681… | | `build/cur/upd1-artifacts.json` |
| a80 `20261001t174610z` (17:46:10-17:47:54Z, cell C) | Baseline B = tag v0.1.0-alpha.80 + licence seam | v0.1.0-alpha.80 / 80 | 0bf21ae6b59117ac8071f5612672a7731957e871 (parent bd14d97e) | ee6d75f688fac425179ee646a647a0ceb68283bc | 007782b6cac14a416ba45482db6716a3efdbfca499c20de997eb5476264e6dd1 |
| | Good G = efcba145 + policy | v0.1.0-alpha.81 / 81 | 5a1d49a4d897edd2eac8b6f7e63b92cf16a2a190 | 39d2e5bf75831a24ae8cc5576fc03cb1b7ae2671 | 9568a4b92156804df487627e8695f497a7e1eaf0dcfff2f1f73ac6a8a042430a |
| | (D built, not used) | v0.1.0-alpha.81 / 81 | 515edf778589cf5d9567d389ab9410fc6864067a | | |

- The a80 baseline tree `ee6d75f6…` is the same tree as upd7's and upd8's B (`build/a80/baseline-ref-proof.txt`, the
  build refuses any `DIFFERENT`). `prove` exited 0 for both builds; the dry runs of the three cells exited 0 and created
  no lab (`build/{cur,cur2,a80}-*`).
- Image: the upd8 image, unchanged: `/var/tmp/cp-v3n28/images/ubuntu-24.04-server-cloudimg-amd64-20260826.img`,
  SHA-256 `d0fe84bb…0e30` (= Ubuntu's published noble release-20260826 digest; provenance and the unverified signature as
  in upd8, `host/image-check.txt`). Nothing was downloaded by the host.

## Harness changes (working tree, `deploy/e2e/release-recovery/`; offline suites green)

The run copy is `git archive efcba145` (`harness-run-copy/runcopy-efcba145-files.sha256`) plus two files
(`harness-run-copy/overlay*/`): cell A ran with overlay `f1cd0cd5…`, cells B and C with `d681e116…` (= the working tree
at the end). Both offline runs: `test_owner_update_trial` 174 (5 new), `test_lab` 15, `test_worker_fixture_origin` 15,
`test_recovery_candidate_archive` 11, `test_bound_worker_reboot` 11, `test_guest_bound_worker` 8,
`test_current_worker_baseline` 7 - all OK (`build/offline-o2-*`, `build/offline-o3-*`).

- `Cell` gains `h19` (default on), `pk_observe` and `scenario`; two new cells, `upd1-ubuntu-setuponce` (h19 off,
  ends after setup) and `upd1-ubuntu-busystart` (h19 on for setup and seed). Existing cells are unchanged (a test pins
  that every cell without a scenario keeps H19 on and observes nothing new).
- `PK_PROBE` (read-only, `/proc` only, no lock taken, PackageKit never contacted): per `packagekitd` its age,
  `grep -c aptcc` of its maps, whether a maps line ends in `/libpk_backend_aptcc.so`, its children and the
  `/proc/locks` lines it holds or waits for; all `/proc/locks` lines on the four apt/dpkg lock files; every other
  process name of the Agent's list; and the probe's own reading of the rule. `pk_observation` brackets one read-only
  `GET /api/v1/host-mutation-readiness` (the Agent's readiness answer the harness already reads before an update
  start; the Panel answers `panel_operation_active` itself while its own operation runs) between two probe readings.
- setuponce: the owner starts the reviewed plan once (no H19 wait, no retry); a reading every ~10 s while setup runs,
  then every 20 s until `packagekitd` exits (≤ 420 s).
- busystart (step `busy-start` between pre-state and arm): the owner's task `cp-lab-upd9-owner-apt.service` =
  `apt-get install --download-only -y --no-install-recommends -o Acquire::http(s)::Dl-Limit=<n>` of one package that is
  not installed (selected read-only; here `golang-1.22-src`, 19.7 MB, 80 KiB/s: nothing is unpacked or configured), then
  `apt-get update` (Ubuntu's apt hook then starts PackageKit). While it holds the apt lock: readiness, update check,
  one `POST /api/v1/panel/update/start`, the status of that request, the panel log, version and unit PIDs before and
  after. Then the task ends, arm runs without the H19 wait and owner-start first POSTs the armed request at once while
  `packagekitd` idles; if that is refused the owner waits for readiness as the card does (≤ 600 s), arms a new request
  and starts it once.
- No product file changed. `run-upd1.sh` unchanged (the `upd1-ubuntu-*` names select the Ubuntu lab).

## Disk (Windows `C:`, PowerShell `Get-PSDrive C`; `host/c-drive.txt`)

134.9 GB free before anything (floor 40 GB, stop at 25 GB); 125.8 GB before cell A, 122.9 GB before cell B, 119.6 GB
before cell C, 116.1 GB after the last cell.

## Cells

| Cell | Lab, port | Wrapper (UTC) | Overlay | Overall | Outcome |
| --- | --- | --- | --- | --- | --- |
| A `upd1-ubuntu-setuponce` run-a | upd9-ub-once-a, 2911 | 17:48:39-17:54:42 | f1cd0cd5 | failed | `setup-stopped`: refused at 02-service, `HOST_MUTATION_BUSY` (F1) |
| B `upd1-ubuntu-busystart` run-a | upd9-ub-busy-a, 2921 | 17:58:47-18:57:32 | d681e116 | complete-for-review | `update-verified`; start refused during the task and while PackageKit idled (F1) |
| C `upd1-ubuntu-good` run-a (alpha.80 -> efcba145) | upd9-ub-a80-a, 2931 | 18:57:47-20:19:27 | d681e116 | complete-for-review | `update-verified`; setup 7 owner attempts (H19, alpha.80) |

Each cell ran once; no cell was re-run. Diagnostic probe `upd9-probe-a` (port 2997, 17:56:05-17:57:45Z): a fresh guest
without CelikPanel, to name what an idle daemon maps (`host/packagekit-probe-a.*.txt`, `tools/probe-a.sh`).

### A. The candidate installed fresh, setup started once by an ordinary owner

| UTC | Event |
| --- | --- |
| 17:49:11-17:50:01 | the candidate's own installer (efcba145 + policy, `v0.1.0-alpha.81`); its `needrestart` restarts `packagekit.service`: `PackageKit[5072]: daemon start` 17:49:34.36 |
| 17:50:02 | owner login, licence (`not contacted: acceptance test build`), guided setup, `web_mail` plan reviewed and started once |
| 17:50:03 | reading: `packagekitd` 5072, age 29.5 s, `grep -c aptcc` 0, no child, no lock line; readiness (Agent) `HOST_MUTATION_BUSY / package_manager_active` |
| 17:50:05 | 01-dns succeeded, 02-service running |
| 17:50:06 | panel log: `service operation 12ab53a1… could not acquire the agent lease: another server change or package-manager task is still running` |
| 17:50:08-10 | 02-service failed; setup `failed`, phase `02-service`, code `HOST_MUTATION_BUSY` (03-service … 14-verify pending) |
| 17:50:13-17:54:20 | 13 readings every 20 s: the same daemon (age 39-286 s), `grep -c aptcc` 0, no child, no lock; readiness 13/13 `package_manager_active` |
| 17:54:39.74 | `PackageKit[5072]: daemon quit` (alive 305.4 s) |
| 17:54:40-41 | no `packagekitd`; readiness `ready` (2/2) |

**Setup did not reach the isolated host's `access_dns` wait in one attempt**; it stopped at its first package step,
8 s after the start, because of the daemon the candidate's own installer had left. The owner sees (API
`GET /api/v1/setup/operation`, verbatim; the API gives only English and no `reason` field):

- code `HOST_MUTATION_BUSY`, message: This server's package manager is busy — something outside CelikPanel is installing or updating packages. Try again in a minute.
- The candidate's catalogue sentences for this code (what a screen can show; the wizard was not rendered):
  `err.HOST_MUTATION_BUSY` EN "Another server change or operating-system package task is still running. Wait for it to
  finish, then try again." / TR "Başka bir sunucu değişikliği veya işletim sistemi paket işlemi hâlâ sürüyor.
  Tamamlanmasını bekleyip yeniden deneyin."; `err.HOST_MUTATION_BUSY.package_manager_active` EN = the message above /
  TR "Bu sunucunun paket yöneticisi meşgul — CelikPanel dışında bir şey paket kuruyor ya da güncelliyor. Bir dakika
  sonra yeniden deneyin."

### Diagnostic probe: what the idle daemon maps

Fresh Ubuntu 24.04 guest, no CelikPanel: `packagekit 1.2.8-2ubuntu1.5`, `apt 2.8.3`. The backend directory
`/usr/lib/x86_64-linux-gnu/packagekit-backend/` holds `libpk_backend_apt.so` and five `libpk_backend_test_*.so`; there
is no `aptcc` file. `/etc/PackageKit/PackageKit.conf` sets no `DefaultBackend`. `apt-get update` (17:56:26-17:56:38) starts
the daemon through `/etc/apt/apt.conf.d/20packagekit` (17:56:33 `daemon start`, pid 1719). Five readings at age 12, 33,
36, 40 and 70 s: `/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so` and `libapt-pkg.so.6.0.0` are
mapped in every one (loaded at start, before any transaction), `grep -c aptcc` 0, no child, no lock line. Over D-Bus
(read-only `Properties.GetAll`) the daemon names `BackendName 'apt'`, `BackendDescription 'APT'`, version 1.2.8. A
read-only `pkcon resolve bash` transaction (238 ms) changed nothing in these readings.

### B. Real package activity, then the update start while PackageKit idles

Setup (H19 on, as upd8): seven owner attempts, 18:00:00-18:45:27Z, each after a ~287-308 s wait in which only
`packagekitd` ran - the alpha.80 pattern of upd8 F1, now with the candidate:

| Attempt | Stops at | Shown code |
| --- | --- | --- |
| 1, 2 | 03-service | `HOST_MUTATION_BUSY` |
| 3 | 03-mail_profile | `HOST_MUTATION_BUSY` |
| 4 | 02-mail_profile | `HOST_MUTATION_BUSY` (alpha.80 in upd8: `mail_profile_install_failed`) |
| 5 | 03-mail_profile | `HOST_MUTATION_BUSY` (alpha.80: `mail_profile_install_failed`) |
| 6 | 05-firewall | `HOST_MUTATION_BUSY` (alpha.80: `server_setup_firewall_failed`) |
| 7 | `access_dns` wait (isolated host) | `server_setup_access_dns_required` |

Every attempt's message was the package-manager sentence above. Seed 18:45:27-18:45:30 (domain, mailbox, cron),
pre-state to 18:47:08.

| UTC | Owner action / reading | Answer |
| --- | --- | --- |
| 18:47:09 | before the task: no package process, no lock | readiness `ready` |
| 18:47:10.6 | the owner's task starts (`golang-1.22-src`, download only, 80 KiB/s); 0.4 s later `apt-get` 71567 holds `/var/lib/dpkg/lock-frontend` and `/var/lib/dpkg/lock`, then `/var/cache/apt/archives/lock`; no `packagekitd` | |
| 18:47:10 | version: `v0.1.0-alpha.81` / 57cec4f6, agent_matches; Panel and Agent main PIDs recorded | |
| 18:47:10 | readiness (Agent) | `HOST_MUTATION_BUSY / package_manager_active` |
| 18:47:10 | `GET /api/v1/panel/update/check` | available `v0.1.0-alpha.82` / 00030d16 |
| 18:47:10 | `POST /api/v1/panel/update/start`, request `b991f0a4…` | **HTTP 409** `PANEL_UPDATE_START_REFUSED` |
| 18:47:11 | `apt-get` still running; status of `b991f0a4…` | `found: false` (no record) |
| 18:47:11 | panel log | `[panel-update] agent refused start: global service mutation state is not idle: the host package manager is active` |
| after | version and unit PIDs/start times | unchanged |
| 18:47:12-18:51:07 | 15 readings every ~16 s | `apt-get` + archives lock; readiness 15/15 `package_manager_active` |
| 18:51:13.2 | download complete (`Fetched 19.7 MB in 4min 2s`), rc 0 | |
| 18:51:14.40 | the task's `apt-get update` -> `PackageKit[80508]: daemon start` | |
| 18:51:15.4 | task unit ended, `Result=success` (246.4 s) | |
| 18:51:17 | `packagekitd` 80508, age 2.9 s, `grep -c aptcc` 0, no child, no lock, nothing else | readiness `package_manager_active` |
| 18:51:19 | arm (no H19 wait), then `POST …/update/start` at once, request `8f21a9dd…`, `packagekitd` alive before and after | **HTTP 409** `PANEL_UPDATE_START_REFUSED`; status `found: false`; panel log same line (18:51:20); version unchanged |
| 18:51:19-18:56:19 | the owner waits as the card says: 20 readiness reads every 15 s | 20/20 `package_manager_active` |
| 18:56:19.41 | `PackageKit[80508]: daemon quit` (alive 305.0 s) | |
| 18:56:22 | readiness `ready`; new check and arm, request `680cc9d4…`; no `packagekitd`, nothing else | `POST` **HTTP 202** `accepted`, `queued` |
| 18:57:21 | update | `succeeded / update_verified` (API, root CLI and recovery API agree, 13 samples) |

Terminal (passed): G installed and running (`v0.1.0-alpha.82`, 00030d16), running = installed, floor 82; database
equal except volatile tables; timers (3) and firewall equal; login works; domain, mailbox and cron rows present; site
marker and SMTP served; cron never interrupted; Panel down 20-30 s inside the transaction. The update itself ran no
apt (PackageKit's journal shows no start after 18:56:19).

**Verbatim owner-visible texts for the refused update start** (both refusals identical):

- API: HTTP 409, `{"code": "PANEL_UPDATE_START_REFUSED", "error": "the update service did not accept this request"}`.
  The candidate's catalogues have no `err.PANEL_UPDATE_START_REFUSED` entry, so `apiErrorText` shows that English
  message in both languages; it does not name the package manager.
- The update card reads readiness before it offers the start (`PanelUpdateCard.tsx`); for this answer its catalogue
  says `services.mutationReadiness.title` EN "Server changes are temporarily unavailable." / TR "Sunucu değişiklikleri
  geçici olarak kullanılamıyor." and `services.mutationReadiness.package_manager_active` EN "The operating system is
  installing or updating packages. Wait for it to finish." / TR "İşletim sistemi paket kuruyor veya güncelliyor.
  Tamamlanmasını bekleyin." (not rendered in a browser).
- The cause is named only in the panel log: `[panel-update] agent refused start: global service mutation state is not
  idle: the host package manager is active`.
- Update status for each refused request: `{"found": false, "request_id": …}`; installed release unchanged.

### C. Update from the alpha.80 baseline to the efcba145 candidate (good)

Request `81ead0a4283a21182fc0ce4453598bc7`. Baseline installed by the tag's own installer 18:58:24-18:59:15. Setup
with H19 (alpha.80's Agent counts `packagekitd`): 18:59:16-20:16:14, seven owner attempts, upd8's pattern exactly -
`HOST_MUTATION_BUSY` ("another server change or package-manager task is still running; wait and try again") at
03-service twice and 03-mail_profile, `mail_profile_install_failed` at 02- and 03-mail_profile,
`server_setup_firewall_failed` at 05-firewall, then the `access_dns` wait; each after a 288-309 s wait in which only
`packagekitd` ran. The owner's idle waits before seed and arm were 0.3 s (nothing ran); readiness `ready`; owner start
20:18:11.

| UTC | Panel API | Root CLI `/usr/libexec/celikpanel/recovery status` |
| --- | --- | --- |
| 20:18:11 | `queued`; `/api/v1/recovery/status` 404 (alpha.80 has none) | absent (`status: unavailable`) |
| 20:18:13-20:18:18 | `running`; 404 | absent |
| 20:18:22-20:18:29 | `running`; 404 | `observation=unavailable` |
| 20:18:33 | old Agent stopped (journal) | |
| 20:18:39-20:18:55 | Panel unreachable | `known running/none update_running` (created by the candidate updater) |
| 20:19:05-20:19:10 | candidate Panel answers: `running`; recovery API 200 `running` | running |
| 20:19:13.36 | self-update worker unit ended | |
| 20:19:14 | `succeeded`; recovery API 200 `succeeded/update_verified` | `succeeded/update_verified`, recorded 20:19:16 |

Terminal (passed): G installed and running (`v0.1.0-alpha.81`, 5a1d49a4), running = installed, floor 81; database equal
except volatile tables; timers (3) and firewall equal; login works; domain, mailbox and cron rows present; site marker
and SMTP served; cron never interrupted; Panel down 25-35 s; three-source agreement passed (15 samples). The
candidate's own idle probes ran inside this update and admitted it, but **no `packagekitd` is known to have run then**
(none ran at 20:18:08; this existing cell records no PackageKit readings, and in cell B the update itself started no
PackageKit), so the new rule's `packagekitd` branch was not exercised inside an update. The `unavailable`, `running`
and `succeeded` CLI texts (EN/TR) are in `upd1-ubuntu-good/run-a/side/extract.txt`.

## PackageKit observations (all cells; `host/packagekit-crosstab.txt`, per-reading files under `*/steps/*/packagekit-observations.json`)

Every observation is two `/proc` readings around one readiness answer; the state columns use both readings
(`tools/pkcross.py`).

| Cell | `/proc` state | Agent readiness answer | Observations |
| --- | --- | --- | --- |
| A | only `packagekitd` running | `HOST_MUTATION_BUSY / package_manager_active` | 16 |
| A | nothing running (after the daemon quit) | `ready` | 2 |
| B | only `packagekitd` running | `HOST_MUTATION_BUSY / package_manager_active` | 11 |
| B | only `packagekitd` running | the Panel's own `panel_operation_active` (setup running) | 5 |
| B | `packagekitd` + other package activity | `package_manager_active` 2, `panel_operation_active` 8 | 10 |
| B | other package activity only (incl. the owner's task) | `package_manager_active` 16, `panel_operation_active` 4, no read 1 | 21 |
| B | nothing running | `ready` 22, `panel_operation_active` 23, no read 2 | 47 |

| Readings of a running `packagekitd` | Count | Its age | `grep -c aptcc` | `libpk_backend_apt.so` mapped | Children | Lock lines it holds or waits for |
| --- | --- | --- | --- | --- | --- | --- |
| A | 32 | 29.5-286.1 s | 0 | (not recorded by `PK_PROBE`) | none | 0 |
| B | 51 | 0.2-93.2 s | 0 | (not recorded) | none | 0 |
| probe-a (no CelikPanel) | 5 | 12-70 s | 0 | yes, 5/5 | none | 0 |
| C | not observed (existing cell, `pk_observe` off) | | | | | |

Other package activity seen in B's setup readings (counted busy, correctly): `apt-get`, `dpkg`, `dpkg-deb`, and
`unattended-upgr` holding `/var/lib/dpkg/lock` and `lock-frontend`. In no reading did `packagekitd` have a child or a
lock line; the probe guest shows that the backend it maps is the APT one under the name `libpk_backend_apt.so`. In no
observation with `packagekitd` running did the Agent answer `ready`.

## Findings

| # | Class | Finding | Evidence |
| --- | --- | --- | --- |
| F1 | **candidate product defect** (owner-visible; P0.1/P0.2 package-activity row) | The rule matches the APT backend only by the suffix `/libpk_backend_aptcc.so` (`cmd/agent/service_mutation_lock_linux.go:455`, used at `:556`); stock Ubuntu 24.04's PackageKit 1.2.8-2ubuntu1.5 ships and loads `/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so` (`BackendName 'apt'`), loaded at daemon start. `packageKitDaemonUsesAPTBackend` is therefore false for every Ubuntu 24.04 daemon and an idle `packagekitd` stays busy for its ~305 s life - the efcba145 change has no effect on this platform. Scenarios: the candidate's fresh install + setup started within ~5 min (A: refused at 02-service after 8 s); setup itself (B: 7 attempts, 45 min, as alpha.80); an update start within ~305 s of any apt run (B: refused 18:51:19, admitted only 3 s after the daemon quit). The unit test fixture uses the same wrong name (`cmd/agent/service_mutation_packagekit_linux_test.go:27`), so the component tests pass; `docs/RESILIENCE-CONTRACT.md` ("libpk_backend_aptcc.so absent from its maps") repeats it. The other two conditions (no child, no lock) held in every reading here. Which name Debian 13 or other releases ship was not measured. | `upd1-ubuntu-setuponce/run-a/steps/06-setup/`, `…/07-packagekit-after-setup/packagekit-observations.json`, `host/packagekit-probe-a.out.txt`, `upd1-ubuntu-busystart/run-a/steps/11-owner-start/step.json` |
| F2 | owner-visible guidance gap (pre-existing, not from efcba145) | A refused update start returns HTTP 409 `PANEL_UPDATE_START_REFUSED` "the update service did not accept this request" (`cmd/panel/system_update_handlers.go:501-505`) for a known, typed Agent cause (`the host package manager is active`); there is no catalogue entry, so EN and TR owners see that English sentence without who acts or when to retry (D-024). The card's readiness read normally shows the package-manager sentence first; the generic text appears when the start is pressed in the readiness window or a task begins between the read and the start. Measured twice (B). | `upd1-ubuntu-busystart/run-a/steps/09-busy-start/busy-start-refusal.json`, `…/11-owner-start/idle-alive-start.json` |
| F3 | owner-visible text accuracy (consequence of F1) | The setup refusal says "something outside CelikPanel is installing or updating packages. Try again in a minute." Here nothing outside CelikPanel ran: the daemon was left by CelikPanel's own installer (A) or setup steps (B), and it lasts ~5 minutes, not one. | A and B setup step records |
| F4 | candidate change confirmed natively | A setup step refused for package activity keeps `HOST_MUTATION_BUSY` and the package-manager sentence: attempts 4-5 (mail profile) and 6 (05-firewall) in B, where alpha.80 showed `mail_profile_install_failed` / `server_setup_firewall_failed` (upd8). The Agent's begin refusal carries `package_manager_active` (the Panel's message is that reason's sentence). | `upd1-ubuntu-busystart/run-a/steps/06-setup/setup-execution-attempt-0{4,5,6}.json` |
| F5 | expected, measured (upd8 F2, first direction) | During real package activity (apt-get holding the dpkg and archives locks, no PackageKit) readiness answered `package_manager_active`, the update start was refused, no update record was created and the installed release, Panel and Agent stayed unchanged. | `…/09-busy-start/busy-start-refusal.json` |
| F6 | candidate update path confirmed (expected) | alpha.80 -> efcba145 (good) on Ubuntu 24.04: the old Agent admitted the start, the candidate updater and its idle probes passed, `succeeded/update_verified` 63 s after the start, terminal checks passed, as upd8 run-d with e9e2d3f3. The new `packagekitd` branch was not exercised inside the update (no daemon known to run). | `upd1-ubuntu-good/run-a/result.json`, `side/extract.txt` |
| F7 | fixture limitation | One isolated node: DNS external, setup waits at `access_dns` (panel and mail certificates, verify not run). The update card and the setup wizard were modelled from the build's catalogues, not rendered. | `result.json` `scope` |

No harness defect needed a re-run. Harness note: H19's own probe (`HOST_IDLE_PROBE`) still lists `packagekitd`; once F1
is fixed, B/C-style cells would wait ~5 min more than the product needs before each setup attempt.

## Real origin, licence, secrets

- Each baseline installer log names neither `celikpanel.net` nor `185.95.` (`secret-scan.txt`); every origin check
  resolved `celikpanel.net` only to `127.0.0.1` with fixture HTTP 200 (`result.json` `scope.origin`).
- Licence: `not contacted: acceptance test build` in every licence step.
- The guests used Ubuntu's package mirrors during install, setup and the owner's task (as upd8). The host downloaded
  nothing.
- `secret-scan.txt` (tools `scan9.py`, `scan.sh`) over the whole folder before hashing: 0 PEM private-key blocks, 0 hits
  for the 176 body lines of the 13 lab key files (SSH keys of the three cell labs and the probe lab, fixture signing,
  CA and TLS keys), 0 unredacted password/secret/token/cookie fields, 0 tokens of the 32-character password shape,
  650 redaction markers. The one fixture-licence literal is `build/a80/fixture-patches.diff` line 75 (the committed
  `AcceptanceFixtureKey` of the seam, as in upd7 and upd8); the 7 tokens of the admin-password shape are upd4 file
  names in the run-copy hash list. `185.95.` appears only in this README, the scan scripts and the scan output.
  Longest repository-relative path: 166 characters.
- `SHA256SUMS` lists every file except itself (`sha256sum -c SHA256SUMS`).

## Files

`build/` (two build folders `cur/`, `a80/` with artifacts document, dist JSONs, fixture commits and patches, the
alpha.80 baseline-ref proof and agent package list, build logs; prove and dry-run outputs `cur-*`, `cur2-*`, `a80-*`;
offline test logs); `harness-run-copy/` (run-copy hashes of efcba145, both overlays with files and diff, jobs, setup
scripts); `tools/` (host readers, waiters, PackageKit viewers and cross-table, the probe script, extract, scan, stage,
sums); `host/` (host check, `C:` readings, image check, probe logs, PackageKit cross-table, leftovers); one folder per
cell with the driver's evidence unchanged (its own `SHA256SUMS` verified after copying), `host/` (wrapper logs, job,
overlay, lab identity, baseline installer result and log, origin intent and manifest, fixture plan) and
`side/extract.txt`.

## Host leftovers (archlinux WSL; nothing deleted; `host/host-leftovers.txt`)

`/var/tmp/cp-upd9-run` (389 MB: run copy, overlays, jobs, logs), the labs `/var/tmp/cp-release-drill-upd9-ub-once-a`
(1.5 GB), `-ub-busy-a` (2.4 GB), `-ub-a80-a` (2.1 GB) and `-probe-a` (0.9 GB), guests stopped, overlays and evidence
kept; the build clones `/var/tmp/cp-upd1-build/20261001t174240z` (725 MB) and `20261001t174610z` (730 MB); the dist
folders `/var/tmp/cp-pair-accept/dist/<commit>-acceptance-license` for 57cec4f6, 00030d16, ae3e1582, f4452094, eb89b681,
0bf21ae6, 5a1d49a4 and 515edf77 (133-705 MB each). No QEMU process remains, no dry-run lab was created. The local git
configuration's key list is unchanged (fingerprint `4363b888…`, equal to upd8's). The repository writes are the two
harness files above and this folder.

## What this run does NOT prove

- That a corrected backend name makes setup finish in one attempt or admits an update start while PackageKit idles:
  the other two conditions held in every reading here, but the corrected rule did not run.
- Any other release or distribution (Debian 13's PackageKit backend name, Ubuntu 22.04, hosting-provider images
  without PackageKit or with another backend); this is the stock 20260826 cloud image only, signature not verified.
- A PackageKit transaction between its phases (the probe saw no transaction except one 238 ms `resolve`).
- That reading readiness every ~10-20 s never perturbs the Agent's own admission (the readiness read briefly probes
  the same locks; no refusal here named `host_lock_busy` or `agent_mutation_active`).
- The browser rendering of the setup wizard and update card (catalogue sentences only); the Panel's setup-wizard
  headline commit `fb04289b` is not in the candidate.
- Production signing, the real release origin, licence behaviour, DNS, certificate issuance, power loss; repeatability
  (one run per cell).
