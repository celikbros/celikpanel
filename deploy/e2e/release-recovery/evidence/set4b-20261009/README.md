# set4b native run, 2026-10-09 (UTC): what the Agent reads around a Stop of Postfix on Ubuntu 24.04, and the same Stop after the correction

A measurement record. set4 (`../set4-20261009/`) found one product failure: on Ubuntu 24.04 a Stop of Postfix with a
`main.cf` that `postfix check` refuses leaves `postfix@-.service` marked `failed`, and the answer carries no `note`
(its item 10). This run first measured, on the product as set4 measured it, what the Agent reads around that Stop
(`diagnostic/`); the correction was written from that record; then the corrected candidate was installed fresh on
Ubuntu 24.04 and on Debian 13 and the Stop was measured again (`remeasure/`). The folder is named with the calendar
date of the run (`date -u` on the host: `host/hostcheck-before.txt`).

No installed server was touched, and no driver of this run was pointed at one (the guests have outbound network access,
see "Name pinning" below; nothing records their traffic). What can be shown of that: the drivers open only loopback ports of the
local `archlinux` WSL host (each lab's SSH forward and the tunnel to its guest's Panel; `host/fixture-plan.json` and
`host/wrapper.out.txt` of each cell), and every guest is a new disposable QEMU guest of that host. Nothing was committed to the working repository, pushed, published
or signed with a production key. Every `result.json` carries `native_evidence: false`. **It closes no P0 row.**

## What was installed

| Cells | Archive installed | Source | Archive SHA-256 |
| --- | --- | --- | --- |
| `diagnostic/` (both runs) | set4's build `cur`, baseline, labelled v0.1.0-alpha.81 (`/var/tmp/cp-upd1-build/20261009t101529z/upd1-artifacts.json`, read only; set4's `build/cur/`) | commit `7fbbeaa6b`, whose tree is the tree of `557b554eb`. The files of the Stop (`cmd/agent/service_action_verify.go`, `mail_service_verify.go`) are byte-identical in `557b554eb`, `e2be8af30` and `2e796418d` | `d2fcf5c98c194c36bec9dd0fe2442eecb6390ba3824842f879a77f9b5debebc5` |
| `remeasure/` | this run's build, baseline, labelled v0.1.0-alpha.81 (`build/upd1-artifacts.json`) | commit `9180c6f87` = an empty fixture commit over the candidate `de34e4485` (tree `f46b90a34` for both, `build/trees.txt`) | `39c06bcc259a5d6a732c6d7c9fa08b78b3b69e424b8a57576bbc800907fb4a32` |

**The candidate is a commit that exists only in a disposable clone.** The builder checks out a commit, so the working
tree's corrections needed one. `tools/mk-src.sh` cloned the working repository to `/var/tmp/cp-set4b-run/src-c1`,
copied the 21 files of `tools/candidate-files.txt` in from the working tree and made ONE commit there:
`de34e44859d49039972ae2e1f9f42ba12d9e1595`, parent `2e796418d` (the working repository's HEAD at that moment),
tree `f46b90a341f524f3cbd31d11b244f2c8eecea9b0`. `build/candidate-source.diff` is that commit's whole diff,
`build/candidate-source-files.sha256` the 21 files as committed, `build/candidate-source-listed-vs-committed.txt` is
empty (the commit changes exactly the listed files). The object is not in the working repository
(`host/host-leftovers.txt`: "candidate commit present in the working repository: no") and no branch was moved by this
run. The builder made its own clone of that clone (`/var/tmp/cp-upd1-build/20261009t143202z/repo`) with the five
fixture commits of `build/fixture-commits.txt`; `good`, `defective`, `startcheck` and `realstart` were built because
the artifacts document needs them and were never installed. Build: `go1.26.5 linux/amd64`, 14:32:02Z to 14:36:59Z,
`license_mode: acceptance-fixture` for every archive (`build/build.out.txt`, `build/dist-*.json`).

**What changed in the working tree after the build.** The candidate was built before the documents were written and
before the last edits of tests and of the browser tool. `verification/product-files-after-the-build.txt` compares
every file under `cmd/`, `internal/` and `web/src` of the working tree at the end of the run with the candidate
commit: see "Verification" below for its result. While this run went on, another session committed set4's evidence
and harness files to the working repository (HEAD moved from `e2be8af30` to `2e796418d`); none of those commits
changes a file under `cmd/`, `internal/`, `web/` or `docs/`.

## Harness (working tree, `deploy/e2e/release-recovery/`, new files only)

| File | What |
| --- | --- |
| `set4b_trial.py` | The driver. `Set4bTrial` over set4's `Set4Trial`, which it does not change. Cell `set4b-diag-ubuntu`: M0, then D10 (what the Agent reads). Cells `set4b-ubuntu`, `set4b-debian13`: M0, set4's own M10 unchanged, R10 (the same Stop three more times under the kernel trace), set4's M2 with what the answer lists. |
| `guest_set4b_native.py` | Guest helper: `read-agent-view` (the Agent's exact reading commands with their raw answers, and the master as the Agent looks for it), `diag-arm` / `diag-collect` (a kernel trace instance with clock `mono` and the events `sched_process_fork`, `sched_process_exec`, `sched_process_exit`; optionally `strace` on the Agent and a sampler of the two units), `owner-stop-by-hand`. The trace stops no process; `strace` does, and its runs are marked. What `diag-collect` returns is filtered on the guest: only lines of the fixed programs and of the two files the master lookup reads. |
| `run-set4b.sh` | Wrapper: one cell per new lab, as `run-set4.sh`. |
| `test_set4b_trial.py` | Offline rules of the driver and the helper (8 tests): the timeline on one clock, the trace and `strace` readers on texts in the shapes those tools print, the sampler, the modes. |

Run copies (`harness-run-copy/`): `git archive e2be8af30` plus the files of `tools/files.txt` from the working tree
(the harness files of `e2be8af30` and of `2e796418d` are the same but for the ones set4 added, which are in the
overlay). `a`: the first diagnostic run. `b`: after H46 and H47. `c`: the same files as `b`
(`overlay-b/harness.diff` and `overlay-c/harness.diff` have the same SHA-256); the build and the two re-measurement
cells ran from `c`. Offline suites on `b` and `c`, all OK (`build/offline-*`): `test_set4b_trial` 8,
`test_settings_writes_trial` 30, `test_request_identity_trial` 17, `test_lab` 15, `test_guest_probe` 13; the dry runs
printed their plan with an empty error stream (`build/dry-*`; their exit status is not recorded, and they name set4's
archive `7fbbeaa6b`, not the one built here). No file shows a lab created by them.

Harness defects found during the run:

| Id | Defect | Handling | Runs |
| --- | --- | --- | --- |
| H46 | `snap4b(label, mode, **arguments)` took the helper's own argument `label` as its file label: `TypeError` at the first `diag-arm`. | The file label and the mode are positional-only; pinned by `test_set4b_trial`. | `diagnostic/set4b-diag-ubuntu/run-a` stopped in D10 before the owner's own stop: it holds the Agent's reading commands with their answers, with and without the owner's line in `main.cf`, and nothing else of D10. Fixed from copy `b` (`run-b`). |
| H47 | The timeline took the first `systemctl` of the Agent near the stop as the stop command. Found by the offline test before any run used it. | It is the last `systemctl` the Agent started before the unit left `active`. | none |
| H48 | `last_systemctl_started_after_the_unit_settled_ms` in each run's summary is not a reading of the Stop: the Agent's own status scan of all services runs right after every action, so its last `systemctl` is that scan's. Found while reading run-b. | Not corrected; the field is not used. The moments quoted below are taken from each run's list `agent_programs`, by the order of the commands. | every cell |

## Guests, name pinning, disk, and a host that slept

Base images (set3's image cache `/var/tmp/cp-v3n28/images`, read only): `ubuntu-24.04-server-cloudimg-amd64-20260826.img`
and `debian-13-genericcloud-amd64-20260826-2582.qcow2`, as in set4 (digests in each cell's `host/fixture-plan.json`).
One new lab per cell, stopped by the wrapper; the cells ran one after the other. Ubuntu 24.04.4, kernel
6.8.0-138-generic, Postfix 3.8.6-1ubuntu0.1, systemd 255.4-1ubuntu8.17 (each cell's
`steps/08-m0-prepare/native/*-platform.json`); the Debian guest's versions are in its own file. Setup profile as in
set4: purpose `web_mail`, DNS mode external. `strace` (`/usr/bin/strace`) is part of the Ubuntu image; nothing was
installed on a guest by this run besides what the product's own setup installs.

**Name pinning, licence step and certificate routes (this is not network isolation).** As in set4: the guests run on
QEMU user networking with outbound NAT, packages are installed from the distributions' repositories during a cell,
and no file records the guests' traffic, so nothing here shows that a guest could not reach the outside. What is
established, per cell: (1) `steps/02-origin/step.json` (before the baseline is installed): `celikpanel.net` resolves
to `127.0.0.1` only on the guest, where the lab's own fixture origin answers (`loopback_only: true`);
(2) `steps/08-m0-prepare/section.json` (`isolation`): the two Let's Encrypt directory names resolve to the guest's
loopback only, asked with `getent -s files hosts NAME`. That pin is made in M0, which is step 8: the same file's
`before_the_acme_lines` shows the two names unpinned before it, so during the install, login, licence, setup and site
steps (3 to 7) they were not pinned. In those steps no certificate was requested through the product: the last
reading of the setup operation in `steps/06-setup/api/` of every cell shows `panel_certificate` and
`mail_certificate` still `pending` (the operation `waiting` at `access_dns`). This is a state the product reports,
not a record of traffic; (3) `steps/05-license/step.json`: `license_service: "not contacted: acceptance test build"`.
The driver itself calls no certificate or ACME route: the routes this driver adds are `POST /api/v1/service/action`
and, through set4's M2, the import routes.

**Disk (Windows `C:`, PowerShell `Get-PSDrive C`, `host/c-drive.txt`).** Before each guest: 72.4, 68.9, 55.4 and 50.7
GiB free. Never under 40 GiB before a guest. The fall between the second and the third reading is this run's build
(`host/host-leftovers.txt` lists what it left).

**The host slept during the Ubuntu re-measurement cell.** The Windows host's lid was closed while that cell ran
(`host/sleep-events.txt`, Windows power events; the process-level "system required" request of
`tools/keepawake.ps1` does not prevent that). The WSL host did not execute between 14:39:04Z and 15:22:47Z: the
cell's `origin` step, which takes 10 to 19 seconds in the other cells, is recorded from 14:39:04Z to 15:22:47Z
(`remeasure/set4b-ubuntu/run-a/result.json`); the Windows record puts the wake at about 15:22:34Z. The pause lies
before the candidate was installed; every measured step of that cell (M10, R10 and M2, 15:39:35Z to 15:41:40Z) ran
after it, and after the last later spell below ended (15:39:03Z), on a guest that was installed and set up after the
pause. Whether the guest's clocks were affected was not examined; the
moments quoted from that cell are differences on the guest's own CLOCK_MONOTONIC within one Stop.

The same record shows three earlier spells of modern standby for an idle screen (13:57:28Z to 14:07:45Z, 14:17:15Z
to 14:21:38Z, 14:32:46Z to 14:37:54Z) and two later ones inside the Ubuntu re-measurement cell, after the wake
(15:23:08Z to 15:23:35Z, 15:32:13Z to 15:39:03Z; they overlap that cell's baseline-install, setup and site steps, none
of which is a measured step). The record lists no event after 15:39:03Z; the time at which it was read is not recorded
in the file. The WSL host went on executing through the earlier spells: the steps of the cells and the
build that ran in those spells carry consecutive times of their own (`diagnostic/set4b-diag-ubuntu/run-b/result.json`:
D10 from 14:17:04Z to 14:19:26Z; `build/build-job.out.txt`). For the two later spells the product journal of that
cell (`remeasure/set4b-ubuntu/run-a/steps/12-collect/journal-product.txt`) has entries up to the 15:36 minute and none
for 15:37 and 15:38, and the site step (15:36:30Z to 15:39:33Z) took about as long as the same step of the other
Ubuntu cells; whether the WSL host paused in them was not established. Whether the processor was slowed in the earlier spells was not
measured; the five Stops of D10 gave the same order of events and the same second between the stop and the master's
end as the Stops of the re-measurement.

## Cells

| Run (folder) | Lab | Copy | Wrapper (UTC) | Overall | Sections |
| --- | --- | --- | --- | --- | --- |
| diagnostic/set4b-diag-ubuntu/run-a | s4b-diag-ub | a | 13:34:32-13:56:29 | `inconclusive` | M0 passed; D10 stopped by H46 before the first Stop |
| diagnostic/set4b-diag-ubuntu/run-b | s4b-diag-ub-b | b | 13:59:57-14:19:28 | `complete-for-review` | M0, D10 passed (D10's checks say only that each instrument measured; its "S8 postfix stop ... the answer (success) matches what the service shows" checks also read PASS in this run although the unit ended `failed`: their detail records `truth_action_took_effect: true` and the units' states, and test that the master is gone, not that the answer was complete) |
| remeasure/set4b-ubuntu/run-a | s4b-ub-a | c | 14:38:27-15:41:42 (43 minutes of it asleep) | `complete-for-review` | M0, M10, R10, M2 passed |
| remeasure/set4b-debian13/run-a | s4b-d13-a | c | 15:42:04-15:55:03 | `complete-for-review` | M0, M10, R10, M2 passed |

`checks-all.txt` lists every check of every cell with its verdict and file (generated, `tools/summary.py`);
`checks-not-passed.txt` the ones that did not pass.

## 1. Before the correction: what the Agent reads (`diagnostic/set4b-diag-ubuntu/run-b`)

Paths below are under `diagnostic/set4b-diag-ubuntu/run-b/steps/09-d10-what-the-agent-reads/`. The owner's line is
set3's and set4's: `default_process_limit = 200 # raised for the campaign`; `postfix check` refuses it. Every moment
is CLOCK_MONOTONIC of the guest, given relative to the moment `postfix@-.service` left `active` (systemd's
`ActiveExitTimestampMonotonic`); the unit's end is `InactiveEnterTimestampMonotonic`; the programs the Agent started
and the master's end are `sched_process_exec` / `sched_process_exit` of a trace instance with clock `mono`.

**Result: five Stops through the Panel out of five were answered `200
{"applied":"stopped","outcome":"verified","success":true}` without a `note`, and `postfix@-.service` was `failed`,
`Result=exit-code`, after each** (`section.json`, `summary`; the exchanges are in `api/`).

**What the Agent read (the two Stops under `strace`: `native/074-p3-collected.json`, `native/088-p4-collected.json`,
field `strace.lines`; quoted from p4).**

1. Before the stop, its two readings (`systemctl show <unit> --property=LoadState --property=ActiveState
   --property=Result`) were answered `Result=success`, `LoadState=loaded`, `ActiveState=active` for
   `postfix.service` and for `postfix@-.service`. Neither unit was `failed` before the stop.
2. `systemctl stop postfix` exited 0.
3. `postconf -h queue_directory` exited 0 and wrote, in this order, to its error stream
   `/usr/sbin/postconf: warning: /etc/postfix/main.cf: #comment after other text is not allowed: # raised for the campa...`
   and to its output `/var/spool/postfix`.
4. The Agent then opened one path: the warning line, a line break and `/var/spool/postfix/pid/master.pid`. The
   kernel answered `ENOENT`. In the lines kept (the `openat` of `master.pid` and of `/proc/<pid>/comm`, the `execve`, `write` and exit lines of the fixed programs), no other look at the master follows; the many `/proc/<pid>/comm` openings in the file all precede the stop.
5. `systemctl show postfix.service ...` was answered `ActiveState=inactive`; `systemctl show postfix@-.service ...`
   was answered `Result=success`, `LoadState=loaded`, `ActiveState=deactivating`. No `postfix check` follows and the
   answer was given.

**When (the three Stops without `strace`, `section.json` `runs[].timeline.agent_programs`; p2a / p2b / p2c).**

| Moment, ms after the unit left `active` | p2a | p2b | p2c |
| --- | --- | --- | --- |
| `systemctl stop postfix` ended | +2.9 | +2.2 | +1.3 |
| the one `postconf` ended (the master was taken as gone) | +36.4 | +31.5 | +30.0 |
| the reading of `postfix@-.service` started / ended | +97.3 / +103.1 | +102.2 / +112.0 | +82.2 / +88.0 |
| the master's process ended | +1009.0 | +1057.2 | +1007.7 |
| the unit became `failed` | +1013.4 | +1060.4 | +1009.3 |

So the unit was read about 0.9 seconds before Postfix had stopped, while systemd was still stopping it, and the
answer carries the verdict of that reading. The HTTP answer itself reached the caller later than the master's end:
the five Stops took 1.9 to 3.3 seconds (`api/0111`, `0113`, `0115`, `0117`, `0119`, `response.elapsed_seconds`), which
fits the Agent's own status scan running after every action (H48; in p2a to p2c its last `systemctl` starts at +1.7 to
+1.9 s). Under `strace` the same order holds with every moment later (p3 / p4: the reading at +225 / +220 ms,
the master's end at +1584 / +1600 ms, the unit `failed` 6.9 / 2.3 ms after it); `strace` stops the Agent at every
traced call, so those two runs show what was read, not when.

**The owner's own `systemctl stop postfix` (`native/018-owner-p1-stop-by-hand.json`,
`native/019-p1-collected.json`; 964 samples taken in 10 s, one about every 10 ms, of which 55 are kept: every sample at which anything differs from the one before, the sample before it, and one in twenty).** Sent at 0; it returned at +24.2 ms.
The unit left `active` at +16.1 ms. The first sample after the stop (read +3.8 to +25.7 ms) shows `postfix.service`
`inactive (dead)` and `postfix@-.service` `deactivating (stop)`, `Result=success`, the master alive; every sample
shows the same until the unit's stop command `postmulti -i - -p stop` ended at +1028.0 ms (trace) and the master
ended at +1031.3 ms; systemd's own moment for the unit's end is +1034.1 ms, and the sample read +1030.0 to +1043.8 ms
is the first that shows `failed (failed)`, `Result=exit-code`. No sample shows another state in between.
`systemctl show` after the stop gives the stop command's own record: `ExecStop={ path=/usr/sbin/postmulti ;
argv[]=/usr/sbin/postmulti -i - -p stop ; ... code=exited ; status=1 }`. The unit files are in `section.json`
(`unit_files`): `postfix.service` is `Type=oneshot`, `ExecStart=/bin/true`; `postfix@.service` has
`PartOf=postfix.service`, `Before=postfix.service`, `Type=forking`, `GuessMainPID=no`.

**What this says about set4's reading.** set4's README inferred from the journal that the unit was read once, before
systemd marked it failed, "the mark comes 4 ms after the master ends", and said that this was an inference. The
single reading is confirmed. The cause is not that window: the unit was read about 0.9 seconds before the mark,
because the master lookup had taken postconf's two lines as one path and read "no such file" as "no master".

**Not measured here.** Debian 13 before the correction (whether the lookup misread there too; the Stop there returns
after the unit's whole stop, and set4 measured the note twice). Any other Postfix warning than this one. `run-a`
holds only the readings before the stop (`agent_view_at_start`, `agent_view_with_the_refused_main_cf` in its
`section.json`): the same two answers for the units, and the same warning from `postconf` (started there by its
bare name, so the line begins `postconf: warning:`).

## 2. After the correction (`remeasure/`)

The corrected candidate, installed fresh. Request: `POST /api/v1/service/action {"name":"postfix","action":"stop"}`
while `postfix check` refuses `main.cf` (the same owner's line), four times per platform: once in set4's own section
M10, unchanged, and three times in R10 under the kernel trace. `S/` is `steps/`.

| | Ubuntu 24.04 (`remeasure/set4b-ubuntu/run-a`) | Debian 13 (`remeasure/set4b-debian13/run-a`) |
| --- | --- | --- |
| Answer, four Stops out of four | `200`, `success: true`, `outcome: verified`, `applied: stopped`, with `note` | `200`, `success: true`, `outcome: verified`, `applied: stopped`, with `note` |
| `note` | `code: SERVICE_ACTION_NOTE`, `reason: unit_marked_failed_config`, `vars.failed_unit: postfix@-.service`, `result: exit-code`, `command: sudo systemctl reset-failed postfix@-.service`, `detail: postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign`; `error` is the documented sentence | `code: SERVICE_ACTION_NOTE`, `reason: unit_marked_failed_config`, `vars.failed_unit: postfix.service`, `result: exit-code`, `command: sudo systemctl reset-failed postfix.service`, the same `detail`; `error` is the documented sentence |
| The unit after the answer and later | `postfix@-.service` `failed`, `Result=exit-code`, after the answer and 8 seconds later (M10) and at the later reading of each R10 Stop; `postfix.service` `inactive` | `postfix.service` `failed`, `Result=exit-code`, after the answer and 8 seconds later (M10) and at the later reading of each R10 Stop; `postfix@-.service` is loaded and `inactive` throughout (it is not the unit that runs the daemon here) |
| `reset-failed` | none: the unit stayed `failed` until Start, and the product's journal of the window names no `reset-failed` (M10) | none, by the same two readings |
| Start after the owner restored `main.cf` | `200`, `applied: started`, the master runs, no unit `failed`, ports 25 and 587 answer (M10); `200` and the master runs after each R10 Stop | `200`, `applied: started`, the master runs, no unit `failed`, ports 25 and 587 answer (M10); `200` and the master runs after each R10 Stop |
| Raw files | `S/09-m10-postfix-stop/section.json` (`postfix_stop`, `postfix_start`), its `api/`, `native/`, `journal/postfix-since-the-stop.txt`; `S/10-r10-postfix-stop-repeated/section.json` (`runs`, `summary`) | the same paths |

**What the Agent did, Ubuntu 24.04 (kernel trace of the three R10 Stops, `runs[].timeline.agent_programs`; ms after
the unit left `active`; r1 / r2 / r3).** `systemctl stop postfix` ended at +2 / +2 / +2. `postconf -h
queue_directory` was run three times in each Stop, ending at +40, +926, +1491 / +59, +593, +1129 / +54, +603, +1140:
the master was looked for until it had ended (+1012.9 / +1014.5 / +1024.9), where before the correction there was
one look. The unit became `failed` at +1020.5 / +1016.3 / +1026.1. The Agent's next `systemctl` (by the order of the
source, its reading of `postfix.service`, which the reading of `postfix@-.service` follows) started at +1533 / +1161 / +1174, that is 513 /
145 / 147 ms after the unit had settled. So each verdict was taken after Postfix had stopped, from readings that (by
that order) started after the unit had settled; the notes of all three Stops name the unit as `failed`. No `strace` was
used in these cells; what each command printed was not recorded here.

**What the Agent did, Debian 13 (kernel trace of the three R10 Stops).** `systemctl stop postfix` ran for 1066 / 1083 / 1067 ms: there the unit that is stopped runs the daemon, and the command returns after the unit's whole stop, Postfix's one-second pause included. One `postconf -h queue_directory` followed (the master had ended; no second look was needed), then one `systemctl` 54 / 63 / 60 ms after the stop command had returned (by the order of the source, the reading of `postfix.service`; the trace shows no second `systemctl` after the stop, so `postfix@-.service` was not read after it in these Stops), then `postfix` for about one second (the trace records the program name only; by the source it is `postfix check`, for the note's line). `postfix@-.service` exists on this platform as a loaded, inactive instance of the template `postfix@.service` (`S/09-m10-postfix-stop/native/011` and `015`) and never left `inactive`. The timeline's own fields are empty in this cell: they are taken from `postfix@-.service`, which was never `active` here because the daemon runs under `postfix.service`; the moments above are from `runs[].timeline.agent_programs`. Debian GNU/Linux 13 (trixie), kernel 6.12.105+deb13-cloud-amd64, Postfix 3.10.13-0+deb13u1, systemd 257.13-1~deb13u1.

**Not measured.** A unit that stays between two states for the whole wait (the note `unit_not_settled`), and a unit
that cannot be read after the stop (`unit_state_not_read`): no guest produced either; component tests and a real
Chrome against the loopback mock only. A Stop of Postfix with a `main.cf` that makes `postconf` warn and that
`postfix check` accepts. Arch (no Postfix there). Dovecot, and any other unit. The Services screen: nothing of
these cells was seen in a browser.

## 3. The import's lists (`S/11-m2-import/section.json`, `lists`)

set4's M2 unchanged (the set3 fixture, `do_dns: false`, the server's DNS in external mode), with three checks added.

| | Ubuntu 24.04 | Debian 13 |
| --- | --- | --- |
| Answer | `200`, `status: active`, `imported: [domain, files, mail, forwarders, database:s4imp_app]`, `not_imported: []`, `left_out: [dns]` | the same answer: `imported: [domain, files, mail, forwarders, database:s4imp_app]`, `not_imported: []`, `left_out: [dns]`, `status: active` |
| The `dns` step | `ok: true`, `state: left_to_owner`, `detail: external DNS ownership preserved; verify provider records before publishing the site` | the same |

Every step of the answer is in exactly one of the three lists, and the import is `active`. The fixture holds one
forwarder, so `forwarders` is imported; the states `not_chosen`, `none_in_archive` and `none_imported` were not
produced on a guest (component tests only), and the import page was not seen in a browser on a guest.

## Verification on the final working tree (`verification/`)

Not native evidence; recorded here so that the numbers have their files.

- **The tree that was built against the tree at the end** (`verification/product-files-after-the-build.txt`): of the
  29 files of the working tree that differ from HEAD at the end of the run, 18 are byte-identical to the candidate
  commit: all 16 under `cmd/`, `internal/` and `web/src`, and `web/tests/set3-corrections.test.mjs` and
  `web/tools/browser-inspect/run.mjs`. Changed after the build: the four documents (written after the measurement), one test of
  the web suite and two files of the browser tool. The four harness files are not part of the candidate.
- **Go** (WSL `CelikPanel-S2-Debian`, go1.26.5; `verification/go-final-*.txt`): `gofmt -l cmd internal` lists one file, `internal/recoveryruntime/bind_source_support_linux.go`, which this run did not touch (it is as at HEAD); `go vet ./cmd/agent/... ./cmd/panel/... ./internal/...` exit 0 with no output; `go build ./cmd/... ./internal/...` exit 0 for linux/amd64 and for linux/arm64. `go test ./cmd/agent/...`: 4473 tests passed, 94 failed, 29 skipped; the failing set (95 entries with the package) is, name by name, the failing set of the same run on the tree before any edit of this run (4467 passed, 94 failed: `go-baseline-failset.txt`, `go-final-agent-failset.txt`); the six tests of `cmd/agent/set4_corrections_test.go` are the difference in passes. `go test ./cmd/panel/... ./internal/...`: 66 packages, 64 ok, 2 without tests, none failed; 6829 tests passed, 0 failed, 44 skipped (`go-final-summary.txt`).
- **Web** (Windows, `web/`): `npm run build` exit 0, no bundle budget changed
  (`verification/web-build-final.log.txt`); `npm test`: 1245 tests, 1245 passed, 0 failed (`verification/web-test-final.log.txt`).
- **Python harness suites** (WSL `archlinux`, `python3 -m unittest discover -s deploy/e2e/release-recovery -p
  'test_*.py'` on a scratch copy without a `.git`): at HEAD `2e796418d` 968 tests, 5 errors in 3 of the 75 test
  files (`verification/py-head.txt`); on the tree as it stands 976 tests, the same 5 errors in the same 3 of 76 files
  (`verification/py-worktree.txt`). The 8 new tests are `test_set4b_trial.py`.
- **A real Chrome against the loopback mock** (`web/tools/browser-inspect`, scenarios `updaterolledback`,
  `importentries`, `importleftout`, `stopnote`; `verification/browser/`): six configurations (desktop and phone,
  English and Turkish, light; desktop Turkish and phone English, dark), each `errors []`, 11 states each. The reports
  of all six and the screenshots of two (desktop light English, phone light Turkish) are kept; the screenshots were
  looked at. It shows a mock's answers, not a server's.

## Removals, leftovers, secrets

- Removed by this run, each listed in `host/removals.txt` after its cell's checksums were verified on the staged
  copy: the overlay disks of this run's four labs (five disks: the Debian lab starts an Arch guest as well, which the
  cell does not use). One stray build output of this run (`panel`, written to the working repository's root by a
  `go build` of this run at 14:05Z and ignored by git) was removed by this run (no file of this folder records that
  removal or the build). Nothing else was removed; no directory of an earlier run was touched.
- Left on the WSL host, with sizes: `host/host-leftovers.txt` (the run directory `/var/tmp/cp-set4b-run` with the
  disposable clone `src-c1`, the four lab directories `/var/tmp/cp-release-drill-s4b-*` without their overlay disks,
  the build clone `/var/tmp/cp-upd1-build/20261009t143202z`, the five dist directories under
  `/var/tmp/cp-pair-accept/dist/`). Each cell's `host/wrapper.rc.txt`/`wrapper.out.txt` records its lab as `stopped`; no file of this folder lists the host's processes after the last cell.
- `secret-scan.txt`: set3's and set4's scan over this folder (PEM private keys, the body lines of every lab key file,
  licence keys, WireGuard keys, secret-named JSON fields, hash-shaped credential values, password assignments). Its
  last line is the result. The drivers redact at collection time (set3's shape rules, unchanged). The new guest
  helper reads no secret-shaped file; what it returns of the `strace` record is, by rule, the argv and output of
  `systemctl`, `postconf`, `postfix` and `postmulti`, any other program by its path only, and the opening and the
  first reads of `master.pid` and `/proc/<pid>/comm`. This cell kind starts no update, so no update-transaction token
  exists in it. The scratch folder's path is replaced by `<scratchpad>` in `tools/`.
- `SHA256SUMS`: every file of this folder but itself; generated last and verified.

## Files

`README.md`; `checks-all.txt`, `checks-not-passed.txt` (generated); `secret-scan.txt`; `SHA256SUMS`; `build/` (the
candidate source's commit, diff and file list, the artifacts document, dist JSONs, fixture commits, trees, build
logs, offline suites, dry runs); `harness-run-copy/` (overlays, jobs); `host/` (host check, disk readings, power
events, progress, removals, leftovers); `diagnostic/` (the measurement before the correction); `remeasure/` (after
it); `verification/`; `tools/` (every script this run used, the document templates and their generator included).

## Corrections after intake (2026-10-09)

Made after the evidence was collected, before it was committed to the public repository, by a second reader who
re-checked the sums, scanned every file for secrets and outside contact, and compared each result stated above with its
raw file. No measured value was changed. Secrets found: none (see the last paragraph of `secret-scan.txt`). Identity: the
16 product files under `cmd/`, `internal/` and `web/src` recorded as `same` in `verification/product-files-after-the-build.txt`
are, byte for byte, the files of commit `1f182a483` (where this work was committed); so are 18 of the 21 files of
`build/candidate-source-files.sha256` (those 16, `web/tests/set3-corrections.test.mjs` and
`web/tools/browser-inspect/run.mjs`). The three that differ from their recorded checksums in `1f182a483` are
`web/tests/set4-corrections.test.mjs`, `web/tools/browser-inspect/README.md` and
`web/tools/browser-inspect/scenarios-batch7.mjs`, the ones this README already lists as changed after the build. The
commit changes exactly 29 files against `2e796418d`, the 29 of that list. The four harness files of
`deploy/e2e/release-recovery/` in `1f182a483` equal the ones the cells ran from (`harness-run-copy/overlay-c/files.sha256`).

README changes:

1. The opening claim "No installed server was touched or contacted" now says that no server was touched and no driver
   was pointed at one, and that the guests have outbound access that no file records.
2. "Name pinning" section: the Let's Encrypt names are pinned in M0 (step 8), not before every step that could matter
   (`before_the_acme_lines` shows them unpinned until then); "no cell calls a certificate or ACME route" is now stated
   as "the driver calls none", with the setup operation's `panel_certificate` / `mail_certificate` steps still `pending`
   in the last reading of every cell as the product's own state, not as a record of traffic.
3. Host sleep: the sentence "the WSL host's uptime at 15:23:25Z was ten minutes" is removed (no file shows it); the
   `origin` duration in the other cells is 10 to 19 seconds, not "about ten"; the two later spells of modern standby
   inside the Ubuntu cell (15:23:08Z to 15:23:35Z, 15:32:13Z to 15:39:03Z) are listed, with what the files show and do
   not show about them. The measured steps (M10, R10, M2, from 15:39:35Z) ran after the wake and after both.
4. Section 1: "Both units were counted as 'not failed before the stop'" now says only that neither unit was `failed`;
   "No other look at the master follows" is limited to the lines the guest filter keeps, and states that all
   `/proc/<pid>/comm` openings precede the stop; "the answer was given about 0.9 seconds before Postfix had stopped" is
   replaced by what the files show (the unit was read ~0.9 s before the master's end; the HTTP answer took 1.9 to
   3.3 s); the owner's own stop: 964 samples were taken but 55 are kept, by the stated rule.
5. The cells table: D10's "S8 ... matches what the service shows" checks read PASS although the unit ended `failed`;
   the sentence now says what they test and that they do not show the answer was complete.
6. Section 2, Ubuntu: "148" ms is "147" (147.4); Debian: "61" ms is "60" (60.3). Debian: `postfix@-.service` is not "not
   a unit of this platform" and was not "not read as loaded": it is loaded and `inactive` (`native/011`, `015`); the empty
   timeline fields are explained by that unit never being `active`, the daemon running under `postfix.service`.
   `postfix check` for the 1-second `postfix` program is marked as the source's reading, since the trace records program
   names only. The Debian command-count sentence is limited to what the trace shows (no second `systemctl` after the stop).
7. "the dry runs exited 0 and created no lab" is now "printed their plan with an empty error stream", with the note that
   their exit status is not recorded and that they name set4's archive.
8. "No QEMU process and no job of this run is left running" is replaced by what is recorded (each lab `stopped` by its
   wrapper); the removal of the stray `panel` build output is marked as not recorded in a file of this folder.

Other changes: `host/c-drive.txt`, `host/keepawake.log`, `host/sleep-events.txt` had CRLF line ends and were converted to
LF (this repository normalises line ends on commit, which would have broken their checksums after a clone); a paragraph
was added to `secret-scan.txt`; the root `SHA256SUMS` was regenerated last. The per-run `SHA256SUMS` files were not
changed (none of the changed files is listed in them).

Not corrected, left as they are: `harness-run-copy/runcopy-e2be8af30-files.sha256` is 12 MB (a checksum list of the
whole archive of `e2be8af30`, reproducible from git); the dry-run plans in `build/` still name set4's archive; the
capture time of `host/sleep-events.txt` is not recorded.
