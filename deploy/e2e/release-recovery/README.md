# Disposable release and recovery lab

*[Türkçe](README.tr.md) · P0.1 evidence tooling*

This directory prepares fresh Debian 13 and Arch QEMU guests, installs the real
Alpha75 release, prepares a bounded DNS fixture through the installed Agent, and
collects native observations. **It does not establish a native PASS or close
P0.1 by existing, booting guests, or passing its offline tests.** Full actual
update/automatic rollback and native workload acceptance remain required by the
[resilience contract](../../../docs/RESILIENCE-CONTRACT.md).

## Recorded native results

Later scoped records cover [independent material](RECOVERY-MATERIAL.md),
[runtime promotion](RUNTIME-PROMOTION.md) and [forward completion](FORWARD-COMPLETION.md).
They retain the earlier failures below and do not close the full matrix.

The [2026-09-14 result record](RESULTS.md) contains the exact operations,
artifacts, snapshots and private-evidence digests for genuine QEMU trials:

| Trial | Observed outcome | Full old-release rollback |
| --- | --- | --- |
| Debian 13, Alpha75 → Alpha79 | Update failed on BIND ownership compatibility; automatic recovery then failed its inherited-lock check. Candidate files remained installed; panel/Agent were stopped. | Restoration body did not execute; the recovery attempt failed. |
| Arch, Alpha75 → Alpha80 with the temporary port conflict | Native recovery completed the pending update. Both installed and running executables were Alpha80; DNS A/SOA answers and served bootstrap TLS fingerprint were preserved. | **INCONCLUSIVE:** forward completion restored availability, but did not restore Alpha75. |
| Arch, Alpha75 → Alpha80 interrupted during the active update | The candidate and completed snapshot were verified before killing the exact update worker. Native recovery rejected `NeedDaemonReload=yes` after unchanged vendor unit bytes were republished but before reload. | **FAIL:** recovery stopped before the full restoration body. |
| Debian 13, Alpha75 → Alpha80 interrupted during the active update | Real `OnFailure` recovery failed the same reload-state guard. DNS A/SOA answers over UDP/TCP were preserved, while HTTPS was refused. | **FAIL:** recovery stopped before the full restoration body. |

Forward completion is a useful observed recovery outcome. It has a different
acceptance criterion from restoring old binaries, data and workloads; it is
neither a full rollback PASS nor a new functional failure. The original update
failure remains recorded separately. **P0.1 is still open.** The active-worker
interruption reached native recovery, but its steady-state guard rejected the
interrupted transition before restoration. That is open P0.3 failure evidence,
not a fixed defect or a completed rollback acceptance.

The report also preserves the evidence limits: the first Debian trial lacked `dig`, live WAL
prevented complete database comparison, TLS issuance/renewal and native DNS peer
transfer were not tested, and other independent workloads remain unmeasured.

### Three evidence directory names carry round labels, not dates

`evidence/set1-20261010`, `evidence/set2-20261011` and `evidence/set3-20261012` are named
with labels that were raised by one per round of work instead of being read from the clock;
the days 10, 11 and 12 October 2026 had not come when they were written. The runs happened
on 2026-10-08 21:18-22:15 UTC (set1), 2026-10-09 03:29-04:49 UTC (set2) and 2026-10-09
07:00-08:46 UTC (set3), from the raw timestamps inside each directory, and were committed on
2026-10-09 (`7a64bda91`, `c523bbfd2`, `dd1710256`; local time, UTC+3). Each directory's own
`README.md` already gives the real date in its title. The names are not changed: every file
in them is listed in a sealed `SHA256SUMS`, other documents cite them, and the directories
are the identifiers of the rounds. The label mapping is in the erratum at the top of
[OPERATION-GUIDANCE.md](../../../docs/OPERATION-GUIDANCE.md). [RESULTS.md](RESULTS.md) is the
record of the four trials of 2026-09-14 and does not list these directories.

## Boundary and prerequisites

- Run on a Linux QEMU host with the dependencies and pinned base-image cache
  described in the [DNS fixture](../dns-kill-matrix/README.md). The launcher uses
  KVM, two CPUs, 3 GiB RAM and a fresh 24 GiB overlay per guest. Windows QEMU is
  not this launcher's execution environment.
- A work root must be a new `/var/tmp/cp-release-drill-NAME`. There is no argument
  for an existing server or arbitrary SSH destination. Each lab gets a new key,
  nonce, sealed fixture plan and cloud-init marker. Management SSH forwards bind
  only to `127.0.0.1`; subsequent operations require the learned host key.
- The host checks the registered QEMU command and PID. Guest execution verifies
  the root-owned marker, exact nonce/cell/node/UUID, matching DMI UUID and native
  systemd. The seed/update drivers and collector also check QEMU identity.
  These guards identify the disposable fixture; they are not protection against
  a malicious administrator controlling both the host and its evidence.
- **The lab is not air-gapped.** Its NAT management network permits public
  package and signed release downloads, including the real Agent's public
  manifest fetch. The peer NIC uses the isolated `192.0.2.0/24` fixture link.
  Do not describe loopback SSH as outbound isolation. No production credentials,
  license, configuration, database or private certificate is copied into guests.
- Installed customer panels retain the user-only update rule. These fixture
  drivers are not administration or deployment tools for Boston, Frankfurt or
  another existing installation.

## Actual CLI

Run from the repository root. Use a new lab name; the image cache must already
contain the exact pinned images. Mutating launcher commands require `--execute`.
`status` reads the running guests without that flag.

```sh
LAB_ROOT=/var/tmp/cp-release-drill-example
NODE=debian13
python3 deploy/e2e/release-recovery/lab.py prepare --work-root "$LAB_ROOT" --ssh-port 2261
python3 deploy/e2e/release-recovery/lab.py prepare --work-root "$LAB_ROOT" --ssh-port 2261 --execute
python3 deploy/e2e/release-recovery/lab.py start --work-root "$LAB_ROOT" --execute
python3 deploy/e2e/release-recovery/lab.py status --work-root "$LAB_ROOT"
```

`--image-cache` overrides the default `/var/tmp/cp-install-vm/images`. Ports
`2261`, `2262` and `2263` in this example are the two SSH forwards and peer
transport; the launcher validates their range before preparation writes.

Check collector prerequisites, including `/usr/bin/dig`, before recording the
baseline. A missing tool produces unknown evidence; adding it after a fault does
not repair the earlier baseline. Install and inspect the original release:

```sh
python3 deploy/e2e/release-recovery/install_baseline.py start --work-root "$LAB_ROOT" --node all
python3 deploy/e2e/release-recovery/install_baseline.py start --work-root "$LAB_ROOT" --node all --execute
python3 deploy/e2e/release-recovery/install_baseline.py status --work-root "$LAB_ROOT" --node all
python3 deploy/e2e/release-recovery/install_baseline.py collect --work-root "$LAB_ROOT" --node all
```

`--node` accepts `debian13`, `arch`, or `all` here. Start returns after launching
the guest's `celikpanel-lab-alpha75-install.service`; use status to inspect the
result. A partial attempt is evidence to inspect, not permission to install again.
The guest generates its own administrator credentials and retains them only in
its private fixture directory. The installer log is private; do not publish it
without reviewing it for secrets. `collect` requires a terminal installation result
and copies the private result/log to the host with digests; it does not copy the
separate credentials file.

The baseline uses unchanged `download-portal/get.sh` from commit
`5aa03fd5b6775b21834ff7b1ce0695d92f50ae93`, pinned by SHA-256
`82b2674c103e347df471ec7e3f2f091d50006c957c54ac946b7c021ed19e041d`.
That released bootstrap selects sequence 75 / `v0.1.0-alpha.75` and the public
origin `https://celikpanel.net`. Its pinned release key, signed manifest, archive
checks and genuine installer remain in the path. It creates the old schema,
units, socket and ledger through the real release. This is distinct from copying
current binaries, importing a production database or hand-writing receipt files.
The baseline's loopback `curl --insecure` check establishes HTTP reachability,
**not trusted certificate issuance**.

Build the fixture-only producer on Linux with the repository's supported Go
toolchain, then run it once on a selected guest:

```sh
ARTIFACT_ROOT=/var/tmp/cp-release-drill-artifacts
mkdir -p "$ARTIFACT_ROOT"
GOTOOLCHAIN=go1.26.5 go build -o "$ARTIFACT_ROOT/release-recovery-seed" ./deploy/e2e/release-recovery/driver
python3 deploy/e2e/release-recovery/exercise.py seed --work-root "$LAB_ROOT" --node "$NODE" --binary "$ARTIFACT_ROOT/release-recovery-seed"
python3 deploy/e2e/release-recovery/exercise.py seed --work-root "$LAB_ROOT" --node "$NODE" --binary "$ARTIFACT_ROOT/release-recovery-seed" --execute
```

Seeding uses the old Agent's authenticated local socket and real
Begin/heartbeat/finish/status operations: none-to-BIND acquisition, then zone
creation and modification for `recovery-fixture.test`. It verifies publication
progression while retaining acquisition ownership bytes. It does not manually
write DNS ownership, engine state, configuration or license receipts. This is
**standalone Agent-producer coverage**, not the browser wizard, panel database
or licensed user admission path, and not a primary/secondary replication test.

`seed-intent.json` is retained before the seed command. Once present, the
controller refuses another seed attempt, including after a timeout. Keep the
partial output and inspect the same attempt; do not remove the intent to retry.
An accepted final producer event still needs independent native DNS observations.

Collect observations with a unique label and a UTC start time within 24 hours:

```sh
SINCE_UTC=$(date -u +%Y-%m-%dT%H:%M:%SZ)
python3 deploy/e2e/release-recovery/exercise.py observe --work-root "$LAB_ROOT" --node "$NODE" --label before-update --since "$SINCE_UTC"
```

Use the earlier update start time for an after-update capture; optionally pass
`--operation-id` with the exact 32-character lowercase hexadecimal ID.
`observe` needs no `--execute`: it uploads the collector to the private fixture
directory and creates a host evidence file. The collector itself reads managed
state and queries only loopback DNS/TLS; it does not repair or mutate workloads.
Evidence labels are not overwritten.

`driver-update` is a separate, guest-gated native update RPC fixture. Its current
flags are `--nonce`, `--manifest`, `--signature`, optional `--request-id`, and
`--mode preview|start|status` (default `preview`). It verifies the unchanged
published manifest/signature, persists client review identity, and uses the
installed Agent's signed update path. It does not complete a fault/rollback
matrix by itself; the controller must retain the exact candidate, operation,
snapshot, fault and recovery observations. This is not a supported command for
updating an existing customer's panel.

### Prepare one signed update trial

`update_trial.py` binds a trial to the registered guest, collected Alpha75
installation result and exact seed sequence. It accepts only `--sequence 79`
or `80`. The unchanged published manifest/signature must already be in
`.tmp-release79/assets` or `.tmp-release80/assets`; the fault fixtures also
require the pinned Alpha80 archive there. These are local copies of released
assets, not newly signed test manifests.

Build the fixture driver and prepare the Alpha80 preview on the node seeded
above. Use `NODE=arch` only after installing and seeding that node instead.

```sh
GOTOOLCHAIN=go1.26.5 go build -o "$ARTIFACT_ROOT/release-recovery-update" ./deploy/e2e/release-recovery/driver-update
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node "$NODE" --sequence 80 --mode prepare --binary "$ARTIFACT_ROOT/release-recovery-update" --execute
```

`prepare` saves `update-intent.json`, captures or checks `before-update.json`,
and obtains the real Agent's preview. It does not start an update. The reviewed
request must still name Alpha75 as the current release and the exact signed
target. An existing intent is never silently replaced.

Before the start below, choose no fault or **one** of the following alternatives.
Do not arm both on the same trial. Each new target/fault experiment needs a fresh
baseline guest and evidence directory; do not clear an intent to repeat a start.

### Optional fault: temporary panel-port conflict

```sh
python3 deploy/e2e/release-recovery/arm_port_fault.py --work-root "$LAB_ROOT" --node "$NODE" --mode arm --execute
```

The controller requires the saved Alpha80 preview and no previous start attempt.
It pins the reviewed archive and both candidate executable hashes before arming.
`guest_port_fault.py` waits for the exact updater to stop the old panel, then
holds only `127.0.0.1:2083`. It releases the socket when the updater exits,
recovery begins, the transaction changes, an observation fails or its 600-second
limit expires. A separate systemd runtime limit also bounds the helper.
The candidate checkpoint records installed candidate hashes and the complete
snapshot checksum inventory while the fault is held. It is not a rollback result.

### Alternative fault: interrupt the verified active updater

```sh
python3 deploy/e2e/release-recovery/arm_update_kill.py --work-root "$LAB_ROOT" --node "$NODE" --mode arm --execute
```

This is an alternative for a fresh prepared Alpha80 trial. The guest helper
`guest_update_kill.py` requires the original Alpha75 worker executable and its
exact request, PID/start ticks, systemd invocation, cgroup and command line. It
freezes only that updater unit, verifies the active transaction, installed
candidate hashes and full snapshot checksum inventory, then rechecks the worker
before sending SIGKILL to that exact unit's cgroup. It does not signal the ordinary
panel/Agent services or start recovery itself. A missed checkpoint produces no
kill. The helper has a 600-second limit, a 30-second frozen-checkpoint budget and
both `finally` and systemd `ExecStopPost` thaw cleanup. `kill_sent` proves only the
injected interruption; native automatic recovery must still be observed.

### Start once, then inspect the same request

After an optional fault reports armed, start promptly. If no fault is selected,
use the same start command after preparation:

```sh
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node "$NODE" --sequence 80 --mode start --execute
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node "$NODE" --sequence 80 --mode status
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node "$NODE" --sequence 80 --mode observe
```

The start attempt is durably recorded before the sole native Start RPC. A
transport error or timeout keeps that identity; only `status`/`observe` may
follow. Those commands do not restart the update. Raw driver output and bounded
native journals remain in private evidence files; console summaries contain
status metadata and evidence references. An exit-zero driver call does not by
itself prove installation or recovery completed.

Collect evidence only for the fault selected above; these are alternatives:

```sh
python3 deploy/e2e/release-recovery/arm_port_fault.py --work-root "$LAB_ROOT" --node "$NODE" --mode collect
```

```sh
python3 deploy/e2e/release-recovery/arm_update_kill.py --work-root "$LAB_ROOT" --node "$NODE" --mode collect
```

Fault collection preserves exact guest/operation events and unit state. It does
not repair the guest, rearm a fault, or declare the rollback acceptance complete.

### Unpublished committed candidate: native recovery regression

`local_candidate_trial.py` tests an unpublished local build through the existing
`bootstrap-prebuilt-update.sh` entrypoint and actual retained-release recovery.
It requires a fresh registered VM with the genuine Alpha75 installation and
producer seed above. Do not combine it with a signed trial on the same guest.
Build the archive with `make dist` from a clean committed source export; supply
its exact SHA256. The controller verifies the complete archive inventory and
every packaged static source file against that Git commit/tree. This path is
explicitly **unsigned local-build evidence**, not signed Agent admission coverage;
it does not sign a release, enroll a key or change production trust policy.

The default `require-unit-reload` checkpoint requires a legitimate old-to-new
coordinator unit revision. For example, before the baseline capture, add a
harmless owner comment to the old fixture's coordinator units, reload systemd
and verify them; preserve that fixture-only change in
`evidence/<node>/owner-unit-baseline.json`. Preparation records its digest.
Do not manufacture a pending reload after preparation. Byte-identical units
may correctly be left untouched, so a missing checkpoint yields no injected
kill. Choose `--boundary candidate-installed` only for a separately described
trial that does not claim the stale-manager regression.

Set `CANDIDATE_ARCHIVE` and `CANDIDATE_SHA256` to the verified local artifact.
Run each mutation once, inspect its result, and start promptly after the exact
fault reports armed:

```sh
python3 deploy/e2e/release-recovery/local_candidate_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode prepare --archive "$CANDIDATE_ARCHIVE" --archive-sha256 "$CANDIDATE_SHA256" --boundary require-unit-reload --execute
python3 deploy/e2e/release-recovery/local_candidate_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode arm --execute
python3 deploy/e2e/release-recovery/local_candidate_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode start --execute
python3 deploy/e2e/release-recovery/local_candidate_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode collect
```

Preparation durably saves the exact artifact and guest intent before staging;
arming and the sole native start each have separate durable attempt records.
An ambiguous response permits only collection of the same operation, never a
second start or rearm. Collection may be repeated with new evidence labels.
The fault binds the original Bash executable, exact bootstrap command line,
PID/start ticks, invocation and cgroup. Before SIGKILL, it freezes that updater,
proves the candidate binaries and installed units/helpers, and verifies both
the complete snapshot and the retained candidate inventory. Native systemd
`OnFailure` invokes normal recovery; the fixture neither substitutes a rollback
body nor signals ordinary panel/Agent services. Collected events and journals
remain evidence, not an inferred recovery PASS.

The 22 focused offline tests in `test_local_candidate_trial.py` passed when this
path was added. That count is not whole-CI coverage or proof of a native rollback;
the actual before/checkpoint/recovery/after workload evidence is still required.

Stop guests while preserving their evidence:

```sh
python3 deploy/e2e/release-recovery/lab.py stop --work-root "$LAB_ROOT"
python3 deploy/e2e/release-recovery/lab.py stop --work-root "$LAB_ROOT" --execute
```

Known-dead nodes are excluded from the QMP stop set. A missing identity with an
unexplained socket or a reused PID is refused. Live nodes must still match their
registered QEMU process. Stop retains overlays, logs and evidence; this wrapper
has no recursive teardown command.

## Recovery-process fault handoff (not yet native acceptance)

`recovery_fault_trial.py` opts a **new** unpublished candidate trial into a second
fault during automatic recovery. It retains the genuine Alpha75 baseline and
committed local archive boundary above; it does not test signed Agent admission.
Use one fresh node per action and never replace a consumed intent:

```sh
python3 deploy/e2e/release-recovery/recovery_fault_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode prepare --archive "$CANDIDATE_ARCHIVE" --archive-sha256 "$CANDIDATE_SHA256" --action kill --checkpoint payload_restored --execute
python3 deploy/e2e/release-recovery/recovery_fault_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode arm --execute
python3 deploy/e2e/release-recovery/recovery_fault_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode start --execute
python3 deploy/e2e/release-recovery/recovery_fault_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode collect
```

The updater fault helper first freezes and proves the exact candidate-installed
checkpoint. `guest_recovery_handoff.py` then reads the real canonical active
transaction and selected runtime, seals a root-only snapshot/token-hash intent,
and starts the exact recovery fault helper. Only after the same operation emits
`armed` may the original updater receive its single SIGKILL. Unknown launch results
enter cleanup, which must prove the exact helper command, VM identity and process
invocation before stopping it; they never cause a second launch. There is no guessed
token, wildcard operation or delay-based kill. Default updater fault trials do
not enable this handoff.

`guest_recovery_fault.py` requires the existing VM nonce/DMI identity, full
snapshot/runtime proof and explicit `celikpanel/recovery-checkpoint/v1` record.
It binds the recovery service's MainPID/start ticks, invocation, boot, cgroup and
executable before freezing and rechecking that checkpoint. Missing or changed
proof remains unavailable. `freeze_requested` is fsynced before freezing;
`freeze_observed` records the actual frozen invocation solely for cleanup. If a
restart races with freezing, the changed invocation cannot pass the old checkpoint
fault proof, but it can be thawed. If the helper dies before writing that result,
`ExecStopPost` re-proves the currently frozen fixed unit in the same boot for thaw
only; this cleanup observation cannot admit a kill or reboot.

For a separate fresh-node reboot trial, prepare with `--action reboot` and the
selected checkpoint, then arm/start as above and immediately run:

```sh
python3 deploy/e2e/release-recovery/recovery_fault_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode reboot --execute
```

The guest publishes `reboot_ready` and holds only that recovery cgroup frozen for
at most 30 seconds. It issues no guest reboot command. The host waits at most 600
seconds for this exact event, rechecks the frozen checkpoint and uses one QMP
connection with verified process identity/peer PID and VM UUID. It durably saves
an attempt before one `system_reset`; ambiguous results never trigger a retry.
A reset submission is not proof of a new boot or successful recovery. Collect
native service, boot, exact snapshot/old-file, workload and database evidence
separately. No native recovery-process kill/reboot acceptance is claimed yet.

The producer writes
`/var/lib/celikpanel-recovery-checkpoints/<token-sha256>.json`, outside the strict
release marker directory. Its API accepts only a checkpoint name and requires
held transaction fd9, verified selected runtime and real recovery-unit identity.
Publication is atomic root-only observation. It cannot authorize a mutation;
failure to publish must not interrupt or falsely complete recovery.

## Evidence and remaining acceptance

`guest_probe.py` emits `celikpanel/release-recovery-observation/v1`: installed and
running executable hashes, web tree digest, service/timer state, database
integrity and migration versions, public installed/served TLS properties,
loopback UDP/TCP DNS answers, limited transaction fields and allowlisted journal
events. It never reads private keys, passwords or token contents. One observation
error remains `unknown` without deleting other observations. The exact native
`Rollback complete` journal line is text evidence, not a recovery-success flag.

`evidence.py` consumes a separately assembled
`celikpanel/release-recovery-evidence/v1` record. See
[the schema fixture](test_evidence.py) for required baseline, candidate, update,
fault, recovery, after-state and workload sections. Run:

```sh
python3 deploy/e2e/release-recovery/evidence.py /absolute/path/to/assembled-evidence.json
```

Exit codes are 0 `PASS`, 1 `FAIL`, 2 `INCONCLUSIVE`. Optional
`--expected-regression CODE` reports reproduction separately; reproducing a known
recovery failure does not turn it into a recovery PASS. The classifier checks
consistency and completeness of supplied facts. It cannot authenticate their
origin or make manually constructed JSON into native evidence. Controller refs,
artifact signatures and real execution traces must substantiate those facts.

P0.1 remains open until the required real update/automatic retained-release
rollback and workload matrix is measured. Current limits include:

- The database collector now reads a supported SQLite read-only transaction,
  including committed WAL frames, and produces schema/all-table digests from one
  consistent view. It neither checkpoints nor normalizes source permissions.
  Existing safe WAL/SHM files with matching database ownership and modes are
  required; missing/unsafe sidecars, locks, unsupported schema or bounds produce
  `unknown`. SQLite read coordination can touch existing WAL/SHM metadata; the
  observation reports metadata changes without claiming their cause or bytewise
  metadata preservation. Row contents and raw schema SQL are never emitted.
- Optional `--snapshot-name NAME --snapshot-manifest-sha256 SHA` on the guarded
  guest probe also requires `--operation-id`. It verifies the complete final v6
  snapshot inventory, file hashes and standalone database before semantic reading.
  `evidence.compare_verified_snapshot_database` requires the pinned snapshot name
  and manifest digest and returns `EQUAL`, `DIFFERENT` or `INCONCLUSIVE`. Every
  table, including sessions and operations, participates; no volatile rows are
  silently excluded. The controller must substantiate operation-to-snapshot
  binding. Historical native trials with unknown WAL evidence stay unknown until
  independently measured again; this implementation does not rewrite their result.
- Bootstrap HTTPS and public certificate fingerprints do not prove trusted
  issuance, renewal execution, ACME/DNS validation or independent renewal after
  removing panel/Agent binaries. Timer status alone is insufficient.
- Standalone loopback DNS does not prove a real secondary, catalog transfer,
  TSIG, peer loss/recovery or external delegation.
- Native mail, hosted web traffic, database applications, cron and firewall
  boot/renewal behavior require their own live before/after workloads. They are
  not inferred from panel reachability or service labels.
- Beginning an update, observing old hashes before replacement, a transport
  timeout or seeing `recovery-required` does not prove actual rollback. Capture
  candidate application and the matching native recovery body, then verify the
  old runtime, data, DNS and independent workloads after restoration.

Offline checks (run on Linux for native no-follow file semantics):

```sh
python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_*.py' -v
GOTOOLCHAIN=go1.26.5 go test ./deploy/e2e/release-recovery/driver ./deploy/e2e/release-recovery/driver-update -count=1
```

These checks mock transport or use disposable local files. They do not report a
native guest lifecycle result.

Selected recovery-kit replacement has its own [promotion acceptance record](RUNTIME-PROMOTION.md). It separates genuine predecessor enrollment, read-only mixed-pair dispatch, explicit owner continuation and actual post-promotion automatic rollback. A missed fault is retained as inconclusive.

## WAL and populated SQL prerequisites

[The bounded fixture record](WAL-FIXTURE.md) separates controlled-child WAL
interruption and populated private SQL copies from native update acceptance.
The separate [native WAL experiment](NATIVE-WAL.md) now records an unchanged product migrator, one verified physical noncommit WAL write and same-operation automatic rollback. Its scoped acceptance and retained inconclusive attempt do not close the full fault matrix.

## Owner-started update acceptance (upd1)

*Roadmap item 3, first combined native run. D-022 / D-024 / D-025; evidence for
P0.1, P0.2, P0.3 and P0.5. Harness only: no product code, schema or release
policy changes. `result.json` always says `native_evidence: false`; the owner
judges the P0 rows.*

`owner_update_trial.py` runs **one** owner-started update on **one** fresh
registered guest per cell. The owner's own admin session drives the Panel API
exactly as the web UI does (session cookie, `Origin` header, TLS leaf pinned
through a loopback SSH forward); the root recovery CLI is used only where the
product's text tells the owner to use it. It reuses `lab.py`,
`worker_fixture_origin.py`, `current_worker_baseline.py`,
`guest_bound_worker.py`, `guest_recovery_handoff.py`/`guest_recovery_fault.py`,
`recovery_fault_trial.py` (QMP), `guest_probe.py` and, by path, the DNS pair
driver's `panel_api`, `redaction`, `evidence`, `guidance` and `install_steps`
modules. New guest helpers: `guest_owner_update_observer.py` (checkpoint proof
and fault arming, **never a signal**) and `guest_upd1_workload.py` (sampler,
read-only state, the owner's printed one-time retry).

### Artifacts and provenance labels

`build-upd1-artifacts.sh` clones the repository into
`/var/tmp/cp-upd1-build/<stamp>/repo` and makes three **disposable fixture
commits** there (never in the working repository):

| Role | Commit | Label / policy | Notes |
| --- | --- | --- | --- |
| Baseline B | source HEAD + policy | `v0.1.0-alpha.81` / 81, previous Alpha80 `bd14d97e` | installed by the real installer via `current_worker_baseline.py`; fixture trust root enrolled; `not-production-release-admission` |
| Good G | B + policy | `v0.1.0-alpha.82` / 82, previous = B | success cell |
| Defective D | G + `cmd/panel/main.go` fixture patch | `v0.1.0-alpha.82` / 82, previous = B | `--migrate-only` migrates the isolated copy, then exits 1 |
| Start-check S (upd3) | G + `cmd/panel/server_lifecycle.go` fixture patch | `v0.1.0-alpha.82` / 82, previous = B | `configurePanelHTTPTLS`, shared by the read-only start check and the real start, always fails |
| Real-start R (upd3) | G + `cmd/panel/main.go` fixture patch | `v0.1.0-alpha.82` / 82, previous = B | `main()` exits just before the listener; the start check never reaches that line |

Each is built by `dns-pair-acceptance/scripts/build-dist.sh --acceptance-license`
with `CELIKPANEL_DIST_VERSION` (D-027 acceptance license; the archive carries
its `ACCEPTANCE-LICENSE-BUILD.txt` notice, the only file exempted from Git-blob
proof, and its bytes are checked). Candidates are signed at run time with the
lab's disposable fixture key and served by the guest-loopback `celikpanel.net`
origin, which is provisioned **before** the baseline is installed, so neither
the installer, the Agent nor the Panel reaches the real `celikpanel.net` or a
license service. The update path itself needs no license
(`cmd/panel/license.go` `licenseRecoveryRequest`).

**Why not the 45dfc265 Alpha81 build:** that commit predates the acceptance
license seam (`internal/licensing/acceptance_fixture.go` is absent), and its
Panel refuses every seeding API with `license_required` without a license.
Owner seeding through the Panel is therefore impossible there. upd1 uses a
build of HEAD **labelled** as the baseline version; this is recorded in
`result.json` provenance and is not the historical Alpha81 payload.

**Why this defect:** a Panel that cannot start fails only after
`completion.pending`, where native recovery selects forward completion, not
rollback (`release-recovery-runner.sh`, `update:completion`). The migrate-only
defect fails in phase `active` with a complete snapshot, which is the rollback
path. It is a genuine candidate failure after `candidate-installed`, produced by
a committed fixture change (label: *simulated defect*).

### Cells

| Cell | Node | Candidate | Second fault | What it can show | What it cannot show |
| --- | --- | --- | --- | --- | --- |
| `upd1-debian13-defective` | Debian 13 | D | QMP `system_reset` at `payload_restored` | owner start, failed candidate, automatic rollback interrupted by a reboot, rollback to B by timer/boot; the same request id in API/CLI/shell; workloads, data, timers and firewall preserved | production signing, browser rendering, power-loss durability, external DNS/mail delivery, certificate issuance or renewal execution |
| `upd1-debian13-good` | Debian 13 | G | none | owner-started forward update to G with the same observations | rollback |
| `upd1-arch-defective` | Arch | D | SIGKILL of recovery at `runtime_verified` | as Debian, with a killed recovery instead of a reboot; mail is attempted and recorded | mail continuity if the platform refuses `web_mail` |
| `upd1-arch-good` | Arch | G | none | forward update on Arch | rollback |

**DNS scope (default `--dns-mode external`).** upd1 runs on one isolated
node. A local-DNS primary publishes only after its peer secondary serves the
catalogue (`cmd/panel/dns_engine.go`), so a single node cannot even create a
domain (`DNS_SERVER_REQUIRED`). The owner therefore chooses DNS hosted
elsewhere (`dns_mode: external`). Every cell records DNS as **not provided by
this run; covered by the DNS pair runs of roadmap item 2**
(`not-provided-external-dns`). The driver never counts DNS as passed in this
mode: it is not a pre-update health condition, not a terminal check and not an
outage verdict. `--dns-mode local` (wrapper: `UPD1_DNS_MODE=local`) is kept for
a future two-node variant; it requires the paired identity (`peer_ip`,
`peer_ns`) through `--setup-draft-json` (wrapper: `UPD1_SETUP_DRAFT_JSON`),
because the product refuses a local setup without it
(`server_setup_dns_identity_required`).

If automatic recovery exhausts its three attempts
(`automatic_recovery=paused_retry_limit`), the driver does **not** fail: it
records the Panel recovery-status body, the root CLI EN/TR texts and the saved
offline page, then runs step `owner-continuation (required)`: exactly the
one-time `recovery recover --retry --snapshot <pending snapshot>` command the
product printed in `journalctl -u celikpanel-release-recovery.service -n 50`,
once, with a durable attempt record. The outcome distinguishes
`recovered-automatically` from `recovered-after-owner-continuation`; automatic
attempts are counted from the dispatch receipts and journal, with timestamps.

### Commands (Linux QEMU host, as root, repository root)

```sh
bash deploy/e2e/release-recovery/run-upd1.sh build            # prints .../upd1-artifacts.json
ART=/var/tmp/cp-upd1-build/<stamp>/upd1-artifacts.json
bash deploy/e2e/release-recovery/run-upd1.sh prove "$ART"     # read-only proof of all three archives
bash deploy/e2e/release-recovery/run-upd1.sh dry-run upd1-debian13-defective "$ART" upd1-d13-def-a
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-defective "$ART" upd1-d13-def-a 2361
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-good "$ART" upd1-d13-good-a 2371
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-defective "$ART" upd1-arch-def-a 2381
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-good "$ART" upd1-arch-good-a 2391
```

`cell` prepares and starts a **new** lab `/var/tmp/cp-release-drill-NAME`
(image cache `/var/tmp/cp-v3n28/images`, override `UPD1_IMAGE_CACHE`), runs the
cell on its node and stops the lab; disks and evidence stay. Run cells one at a
time. `UPD1_DNS_MODE` (default `external`) and `UPD1_SETUP_DRAFT_JSON` pass the
owner's DNS choice and draft fields to both `plan` and `run`. The wrapper keeps
every path as one argument, so it works from a repository path with spaces
(`/mnt/c/CELIKBROS PROJECTS/celikpanel`). `prove` repeats, without any guest,
the archive inventory, release-policy and committed-source proof that preflight
runs per cell. Evidence: `<lab>/evidence/<node>/upd1/<cell>-<utc>/` (redacted API
exchanges per step, status samples, guest/host sample series, journals,
observer and recovery-fault events, `result.json`, `SHA256SUMS`). Web assets
are built fresh in the clone; `CELIKPANEL_UPD1_WEB_DIST` may name a `web/dist`
built from the same source commit.

### Owner API sequence and probes

Login (`POST /api/v1/auth/login`, `GET /api/v1/auth/me`,
`GET /api/v1/panel/availability`); acceptance license (`GET`/`POST
/api/v1/panel/license`, `GET /api/v1/license/access`); setup wizard (`GET`/`PUT
/api/v1/setup`, `PUT /api/v1/setup/guidance`, `POST /api/v1/setup/plan`,
`POST /api/v1/setup/start`, `GET /api/v1/setup/operation`; `web_mail`, where Arch
may fall back to `web` with the refusal recorded); seeding (`POST
/api/v1/domains/create` static site, `POST .../files?path=/index.html` with a
marker, `GET .../dns/zone` and `.../dns/records`, `POST .../mail/accounts`,
`POST .../cron` writing a timestamp every minute, `GET /api/v1/firewall`);
update (`GET /api/v1/panel/update/check`, `GET /api/v1/host-mutation-readiness`,
one `POST /api/v1/panel/update/start` with the UI's body and a fresh 32-hex
request id, then `GET /api/v1/panel/update/status?request_id=` with the UI
backoff of 1.5 s x1.6 up to 15 s, `GET /api/v1/recovery/status?request_id=`,
the root CLI `status --json`, `--lang en` and `--lang tr`, and the saved
offline page's command).

The guest sampler unit `cp-lab-upd1-sampler.service` (enabled, so it resumes
after the reboot) records every 5 s: site HTTP with the `Host` header and
marker, SOA over UDP, the SMTP 587 banner (when a mailbox exists), the cron
stamp mtime and the Panel HTTPS status. A host loop records SSH reachability
and the Panel through the tunnel. Outage windows are computed per workload; a
window overlapping the recorded reset is labelled `host-reset`, any other
window `unexplained`. Terminal checks cover build identity, floor/foundation,
database digests (volatile tables listed), seeded rows, marker, mailbox, cron,
timers, firewall ruleset, a fresh login and the update card.

### Corrections from the 2026-09-30 run (H1-H5, L1-L3)

The first native run ([evidence/upd1-20261001](evidence/upd1-20261001/README.md))
used an uncommitted run copy and reached no update. These corrections are now
part of the harness; they are harness behaviour, not product changes:

| Id | Now |
| --- | --- |
| H1 | `run-upd1.sh` holds the driver and lab commands in Bash arrays; a repository path with spaces stays one argument. |
| H2 | `candidate_archive.verify_committed_source` knows the `dns-owner-tools/` directory of `make dist`: exactly `README.md`, `dns-peer-enroll`, `bind-peer-inspect`, `pdns-peer-inspect`. The three tools are build outputs like `bin/`; `README.md` is proved against the committed `cmd/dns-peer-enroll/README.md`. Any other inventory is refused. |
| H3 | `--setup-draft-json` is passed by the wrapper to `plan` and `run`; `--dns-mode local` refuses to start without `peer_ip` and `peer_ns`. |
| H4 | The setup poll stops at `succeeded`/`failed`, or when the execution has stayed at `access_dns`, `panel_certificate`, `verification` or `verify` for 120 s over at least 3 reads and the last read says `waiting` (the product flips the row to `running` on each retry; that does not restart the clock). The step is then `observed` with an `isolated_host_wait` record (phase, code, steps not run) and a finding. The mailbox is optional only when the wizard's `mail_profile` steps were still pending. |
| H5 | `--dns-mode external` is the default (see "DNS scope" above). |
| L1 | The fixture origin is the enabled lab unit `cp-lab-upd1-origin.service` (`Restart=on-failure`, `WantedBy=multi-user.target`), not a transient unit. It is named `cp-lab-*` because a `celikpanel-*` unit file would make the real installer's first-install check (`get.sh`) fail. The driver proves that `celikpanel.net` resolves only to 127.0.0.1 and the fixture answers HTTP 200 after provisioning, again after the owner restart the installer demands, and again at the start of `arm`. |
| L2 | When this run recorded the setup wait, the database comparison also excludes `server_setup_executions` (the waiting runner rewrites it about every 25 s). The verdict lists every excluded table with its reason (`volatile_reasons`). `server_setup_state` is still compared. No other periodic writer applies to an upd1 guest: backup schedules, certificate renewal and VPN writers need rows upd1 never creates. |
| L3 | `collect` runs whatever step stopped the cell, as long as preflight had prepared the guest. It keeps the sample series and three journals: `product` (Panel, Agent, recovery), `setup-services` (web, PHP-FPM, database, DNS, mail and cron units) and `lab` (origin, sampler, baseline installer). It also keeps the observation records (all records when no update was started), the observer events and the budget receipts. A part that cannot be read is listed under `unavailable`; the rest is kept. |
| origin lookup | Preflight no longer asks the resolver for `celikpanel.net` (on 2026-09-30 that resolved the real name before the fixture existed). It reads `/etc/hosts` and the `hosts:` line of `nsswitch.conf` only. The helper's `origin-check` refuses to look the name up before the origin is provisioned. |
| cron | Before seeding, the driver checks read-only whether `crontab` (the Agent's own gate) is present. If it is absent, the cell records `cron: not available on this baseline` and does not create the cron job. This is product finding P1 (Debian setup does not install cron), which is being fixed separately. Cron continuity is then `not-available-on-baseline`, never passed. Once setup installs cron, the check passes and the job is seeded as before. |

`result.json` carries a `scope` record for DNS, cron, the setup wait and each
origin check, so nothing counts as passed by omission.

### Corrections from the upd2 run (H6, H7, O5)

From [evidence/upd2-20261001](evidence/upd2-20261001/README.md), where they were
run-copy diffs; now permanent, with offline tests:

| Id | Now |
| --- | --- |
| H6 | `owner-start` accepts HTTP 200 or 202 when the body says `accepted: true` (`start_accepted`). The product answers 202 for an accepted start (`cmd/panel/system_update_handlers.go`); every other answer is still a refusal. |
| H7 | `origin_verdict` takes the address of every `getent hosts celikpanel.net` answer line. With nss-resolve (Arch) the loopback answer is printed under `localhost`. The loopback-only rule is unchanged; the raw output, status and return code are recorded. |
| O5 | In a sample within the first poll interval after the owner's start (the first sample, or at most 1.5 s after the start answer), the Panel API saying `accepted` while the root CLI says `running` is recorded as `start-instant-lag`. It is not a disagreement. Every other difference, and the same pair at any later time, is still a disagreement. `agreement_verdict` counts `lag_samples`. |

### Candidate-panel start kinds (upd3: start-check, real-start)

Product commit `8ffc5e06` added two boundaries; until now they had component
tests only:

1. `panel --check-startup-readiness` runs after the database publication and
   before `completion.pending`. A failure is a failure in phase `active` with
   code `candidate_panel_startup_check_failed`, and the automatic rollback
   returns the old release.
2. After the real start, a stability wait replaces the single `is-active`.
   A failure stays in `completion` with code `panel_start_unverified`, and
   forward completion is retried up to its limit.

Each kind has its own candidate (S, R), two cells and its own verdict rules
(`judge_start_check`, `judge_real_start`, step `kind-expectation`). The good and
migrate-only cells are unchanged, and an artifacts document built before upd3
still serves them. The texts are always loaded from the build, never copied:
the web catalogues from the product build's `web/src` (`product_web_src`), and
the root-CLI texts from `cmd/recovery/main.go` of the built commit
(`git show` in the fixture clone). The CLI output is compared verbatim in EN and
TR. Both kinds keep `native_evidence: false`.

| Cell | Candidate | Second fault | Expected observation | A finding is |
| --- | --- | --- | --- | --- |
| `upd1-debian13-startcheck` | S | QMP reset at `payload_restored` | The update fails in `active` after the database publication. The update's failure line and the `<request>.failure` sidecar (`celikpanel-recovery-failure/v1`, bound to the request and the S commit) both carry `candidate_panel_startup_check_failed`. The check's reason is the fixture's `tls_pair_invalid`. The observer never sees `completion.pending`. Every automatic dispatch receipt says `phase=active` (rollback). The final state is `recovered`/`rollback_verified` with that code. The old release runs. The database equals the pre-update digests (listed exclusions). The CLI prints the product's "returned to the previous version" text in EN and TR. The recovery-screen keys exist in the catalogue. | Any of these differs. In particular: a check reason other than `tls_pair_invalid` (a good candidate could fail the same way), a completion marker, or a forward dispatch. |
| `upd1-arch-startcheck` | S | SIGKILL of recovery at `runtime_verified` | As above. | As above. |
| `upd1-debian13-realstart` | R | none | The check passes (no check reason, no `candidate_panel_startup_check_failed`). The observer sees `completion.pending`. The update's failure line and the sidecar carry `panel_start_unverified`. Forward completion is retried (dispatch `phase=completion`; N and timestamps from the receipts and journal), then pauses at `paused_retry_limit`. There is no rollback, and R stays installed. The printed one-time retry command is read with `owner-retry` **without `--execute`**, recorded and **not run**. The CLI printed, in EN and TR, the product's `panel_start_unverified` text, which names the panel log command and says there is no supported return. The paused text follows. Site, mail and cron are never interrupted. The Panel is down from the update to the end (`down-from-update-until-end`). View reachability is recorded (the Panel API and offline page are down; the root CLI answers; the SSH owner view is not attempted). | A rollback, a check refusal, no completion marker, an owner retry run, no printed retry, an interrupted workload, a Panel that came back or was never down, or typed texts that were never shown in EN and TR. |
| `upd1-arch-realstart` | R | none | As above (Arch has no mail workload). | As above. |

**What these cells prove and do not prove.** start-check shows that the start
check really runs in the native order: after publication and before
completion. It also shows that the rollback reverses a published database, and
it shows the typed guidance in both views. It does not show that the check
catches every start failure; the fixture fails one shared function.
real-start shows the forward-only path and the pause, the owner's guidance
while the Panel cannot start, and workload continuity without the Panel. It
does not show a recovery from that state: the owner retry would retry the same
candidate, so it is not run. Neither kind covers production signing, browser
rendering or power loss.

**Possible finding, recorded rather than assumed.** The product exposes
`failure_code` only while the update's own failure is the latest recorded one
(`internal/recoveryobs`). Once a forward attempt fails, and at the pause (which
takes precedence in the CLI), the typed real-start text may no longer be shown.
The judge then reports "never printed" rather than inferring it.

Commands (Linux QEMU host, as root, repository root; each cell in a **new** lab):

```sh
bash deploy/e2e/release-recovery/run-upd1.sh build            # five archives: B G D S R
ART=/var/tmp/cp-upd1-build/<stamp>/upd1-artifacts.json
bash deploy/e2e/release-recovery/run-upd1.sh prove "$ART"     # proves every role in the document
for c in upd1-debian13-startcheck upd1-arch-startcheck upd1-debian13-realstart upd1-arch-realstart; do
    bash deploy/e2e/release-recovery/run-upd1.sh dry-run "$c" "$ART" upd3-dry
done
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-startcheck "$ART" upd3-d13-sc-a 2401
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-startcheck "$ART" upd3-arch-sc-a 2411
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-realstart "$ART" upd3-d13-rs-a 2421
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-realstart "$ART" upd3-arch-rs-a 2431
```

The build source must include `8ffc5e06` (the start check). The fixture patches
refuse any other text of `configurePanelHTTPTLS` or the listener line.

### upd4: the upd3 run-copy corrections made permanent

From [evidence/upd3-20261001](evidence/upd3-20261001/README.md), where H8 was a
run-copy diff and H9, H10 and the sidecar v1 defects were recorded; now harness
behaviour with offline tests:

| Id | Now |
| --- | --- |
| H8 | Named rule `settled-failed-before-change` (`SettledFailure`): `track` stops after **600 s over at least 3 reads** of an unchanged known status `failed`/`none` with no `automatic_recovery` and no `waiting_for` (every field of the status unchanged; any change or any recovery activity restarts the clock). The step is `inconclusive`; the reason names the rule, and `checks.settled_failure` / `outcome.track_stop` record the status, seconds and reads. Upd3 cell 1 would have ended after 10 min instead of 90. |
| H9 | Planner's rule (`dispatch_direction`): a dispatch receipt is a **rollback** when it says `operation=rollback` in any phase, or `operation=update phase=active`; `operation=update phase=completion` (or `completion-scheduler`) is **forward**; anything else is `unknown`. In a start-check cell a forward attempt is a finding; unknown ones leave the rule unknown. The arch-startcheck pair `update/active` + `rollback/completion` is a rollback. The real-start judge uses the same rule for "forward only". |
| H10 | The update card and the recovery screen follow the **product build's current code**: `web_source_eval.py` reads the build's `web/src/lib/systemUpdateOutcome.ts`, `recoveryObservation.ts` and `systemUpdateFailure.ts` and evaluates `failedUpdateGuidance`, `parseRecoveryObservation`, `recoveryFailureGuidanceKey` and `systemUpdateFailureMessage` themselves (a strict TypeScript subset; no copy of their rules). The card component's outer selection (found/succeeded/running/failed) is mirrored; the recovery screen is rendered from the build's own JSX since H11 (below). Texts come from the served build's catalogues (baseline after a rollback, the candidate otherwise). `judge_update_card` fails only on a real mismatch: a key the catalogue lacks, an unfilled placeholder, or a card state contradicting the cell's server record. A source the evaluator cannot read gives `unknown`, never a finding; the server-reported line (O6) is recorded, not judged. The old "summary verbatim" finding is gone. |
| sidecar v2 | The driver's own read-only inspections (`guest_upd1_workload.py inspect`): the hosting-root light series every 30 s while setup runs, then `after-setup`, `after-seed`, `before-check`, and after a terminal, paused or settled state (`after-track`/`at-pause`, `after-continuation`, `after-terminal`, and the management-off points). `inspection_allowed` refuses any inspection between the update check (or the owner's retry) and the next terminal, paused or settled state, so none overlaps the update preflight; the v1 `pre-update` point is gone. `getent` exit code 2 is `not-found`, never a probe failure (`getent_outcome`, also in `origin_verdict`); no command's exit status can lose an inspection. |

### upd4 cells: the owner's continuation and management off

Both kinds use the good candidate G (any artifacts document with B and G).

| Cell | Node | What is done | Expected (verdict class) | A finding is |
| --- | --- | --- | --- | --- |
| `upd1-debian13-owner-continuation` | Debian 13 | `guest_owner_port_hold.py` is armed with the observer. Once the updater has stopped the old Panel it holds `127.0.0.1:2083` and keeps it through the update's exit and every forward attempt. The candidate passes the start check, cannot bind, the stability wait fails (`panel_start_unverified`), forward completion is retried three times and pauses. At the pause the driver records every owner text (CLI EN/TR, the card and recovery screen when a Panel answers, the modelled screen with the cause line, the offline page), the panel log the product names (`sudo journalctl -u celikpanel-panel -n 50`), timers (F2 shape) and the hold's events; reads the printed retry without running it; then does what the text tells the owner: releases the port (`systemctl stop` of the hold unit, verified `owner-released`) and runs **exactly the printed retry command once**. The same request is tracked to its end. | `recovered-after-owner-continuation`: `succeeded/update_verified`; G installed and running; `certbot.timer` and every other timer in their pre-update state; site, SMTP and cron never interrupted; the Panel down only inside the operation, then a fresh login; the card `succeeded`. | Any other outcome class (`update-verified-without-owner-continuation`, `rolled-back-instead-of-forward`, a new pause), a hold that ended before the pause, a pause on another cause, fewer than three forward dispatches, a panel log that does not show the port conflict, no printed retry or more than one owner receipt, changed timers, an interrupted workload, a missing EN/TR paused text or catalogue key. |
| `upd1-arch-owner-continuation` | Arch | As above (no mail workload). | As above. | As above. |
| `upd1-debian13-mgmt-off-reboot` | Debian 13 | The good update first (same steps and verdicts as the good cell; the seed adds one owner database through `POST /api/v1/domains/{id}/databases` and one table with one row, the site marker, written through the native client as the server owner). After `terminal` passed: `sudo systemctl disable --now celikpanel-panel.service celikpanel-agent.service`, one orderly `sudo systemctl reboot`, at least **180 s** of 5 s samples in the new boot, then `sudo systemctl enable --now celikpanel-agent.service celikpanel-panel.service` and a fresh login. | Every workload served with management off: site HTTP with the marker, SMTP (Debian), new cron stamps without a stall, the owner's row through `mariadb`/`mysql` as root, the renewal timer in its pre-update state, the firewall ruleset present and unchanged; after the return, the same owner state (domains, cron, mailbox, database, version, update and recovery status). | A workload never served or interrupted after the boot, a changed renewal timer or firewall, the Panel/Agent not disabled, a window shorter than 180 s, or a different owner state after the return. `needed_panel` lists anything that needed management. |
| `upd1-arch-mgmt-off-reboot` | Arch | As above (no mail workload). | As above; Arch's `certbot-renew.timer` was disabled before the update in upd3 (setup waits before its certificate step), so "as before" is all Arch can show. | As above. |

**Port-hold bound.** The hold must outlast the three forward attempts; it is
sized at run time from the **candidate commit's own files** (`git show` in the
fixture clone): `PANEL_START_WAIT_SECONDS` in `update.sh` (60),
`OnUnitInactiveSec`/`AccuracySec` of `celikpanel-release-recovery.timer`
(30 s / 1 s) and the runner's automatic budget (`$count == 3`). With
allowances of 120 s update work before the real start, 60 s work per forward
attempt, 120 s to detect the pause and 300 s for the owner-continuation reads:

    hold = 120 + 60 + 3 x (60 + 60 + 30 + 1) + 120 + 300 = 1053 s -> 1080 s (18 min)
    RuntimeMaxSec = 1140 s

upd3 measured 391 s (Debian) and 383 s (Arch) from the old Panel's stop to the
pause with the same wait, timer and budget, so the hold has a 689 s margin
(`plan --dry-run` shows the bound from the driver checkout under `port_hold`).
The hold is released early only by the owner, a rollback marker, another
snapshot, no transaction marker for 5 s, an observation unavailable for 10 s,
or its bound; one ambiguous read during a marker rename is tolerated.

**What these cells prove and do not prove.** owner-continuation shows, on a
disposable guest, the one owner path never exercised natively: the retry budget
exhausted on a transient host cause, the product's own texts leading to that
cause, and the one-time retry completing the same operation forward, with
workloads and timers intact. It does not show a cause the owner cannot fix, a
panel that fails for a product defect, recovery after a second fault during the
retry, or browser rendering (the Panel is down at the pause, so the card and
screen are modelled, not seen). mgmt-off-reboot shows native serving of the
seeded site, mail, cron, database, renewal timer and firewall across one
orderly reboot with the Panel and Agent disabled, and the Panel returning to
the same owner state. It does not show a power loss, DNS (external mode), a
certificate renewal run, or removal of the packages or files. Neither closes a
P0 row; `native_evidence` stays `false`.

**Product behaviour that may block a cell (recorded, not assumed).**

- The Panel unit is `Restart=on-failure` with `StartLimitBurst=30` per
  3600 s, and neither `update.sh` nor the runner resets a start limit. The held
  port makes the candidate Panel fail and restart repeatedly (every forward
  attempt starts it again). If 30 starts accumulate before the owner's retry,
  the retry's `systemctl start` is refused (`start-limit-hit`) and
  `completed-forward` becomes a finding. The `after-continuation` inspection
  records the unit's `NRestarts`, `Result` and state just before the retry;
  once the port is free systemd may also start the Panel by itself, which is
  recorded, not prevented.
- F1: a good update may stop in its preflight (upd3 cell 1). Then `track`
  ends by the H8 rule and the new cells' later steps do not run (`not-run`).
- Arch setup may not install MariaDB before it waits at `access_dns`; then the
  database create answers non-200, a finding is recorded and the database
  workload is `not-seeded` (never passed by omission).
- The firewall ruleset after the reboot comes from
  `celikpanel-firewall-restore.service`, a product unit that management-off
  does not disable; its state is recorded in the snapshot.

Commands (Linux QEMU host, as root, repository root; each cell in a **new** lab):

```sh
bash deploy/e2e/release-recovery/run-upd1.sh build            # B G D S R; the upd4 cells use B and G
ART=/var/tmp/cp-upd1-build/<stamp>/upd1-artifacts.json
bash deploy/e2e/release-recovery/run-upd1.sh prove "$ART"
for c in upd1-debian13-owner-continuation upd1-arch-owner-continuation \
         upd1-debian13-mgmt-off-reboot upd1-arch-mgmt-off-reboot; do
    bash deploy/e2e/release-recovery/run-upd1.sh dry-run "$c" "$ART" upd4-dry
done
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-owner-continuation "$ART" upd4-d13-oc-a 2481
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-owner-continuation "$ART" upd4-arch-oc-a 2491
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-mgmt-off-reboot "$ART" upd4-d13-mr-a 2501
bash deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-mgmt-off-reboot "$ART" upd4-arch-mr-a 2511
```

Evidence per cell as before (`<lab>/evidence/<node>/upd1/<cell>-<utc>/`), plus
`inspect-<point>-NN.json`, the hold's raw `owner-port-hold-<request>.jsonl`
(copied from the guest's private root into the owner-continuation step when the
owner releases the hold and into `collect`, H15), `panel-log-at-pause.txt`,
`recovery-journal-at-pause.txt`, `workload-at-pause.json`, `panel-truth-*.json`
and `journal-product-before-reboot.txt`.

### Corrections from the upd4 run (H11-H15) and unmeasured start kinds

From [evidence/upd4-20261001](evidence/upd4-20261001/README.md), where H12-H14
were run-copy diffs and H11 and H15 were recorded; now harness behaviour with
offline tests:

| Id | Now | Why |
| --- | --- | --- |
| H11 | The recovery screen is rendered from the served build's own `RecoveryAccess.tsx`. `web_source_eval.py` reads JSX (elements, fragments, text with JSX whitespace rules, `{expression}` children, the TypeScript `x!`) and finds the one JSX tree `RecoveryStatus` returns and its `role="status"` region (`parse_screen_source`). `recovery_guidance` passes the status through the build's `parseRecoveryObservation` and `reconcileRecoveryObservation` as a freshly opened screen does, then evaluates the region with the build's functions (`retryingCauseKey`, `recoveryFailureGuidanceKey`) and the served catalogue: one line per `<p>`/`<pre>` in source order, `keys` every key looked up. Fixed inputs: the read has settled, and the observed-at line (browser locale time) is left out. No layout is copied, so nothing has to be kept in step with the product. A missing component or region, a component or other element inside the region, or any construct outside the evaluator's subset makes the screen `unavailable`: unknown, never a finding (`web_keys_missing` is then unknown in the kind judges). The Python mirror of 94be6b6e is gone. | The mirror showed `retry_scheduled` as the paused screen and left out the renewal line at the pause. The rendered texts now equal `h11-product-screen-texts.txt` for both states. |
| H12 | `management-return` reads the exact request's recovery status (read-only, every 3 s, at most 180 s) until `panel_state=ready`, then compares the owner state. If it is never ready, the step is `inconclusive` and the judge's `management-returned` rule is unknown. | Right after login the Panel still answered 503 `PANEL_STARTING`, and every compared field read as empty (d13 run-a). |
| H13 | `management_off_at` is taken before `systemctl disable --now` is issued; the completion is kept as `management_off_done_at`. | The instant taken after the command returned counted the owner's own stop as a Panel outage (d13 run-a). |
| H14 | The update-only verdict window ends one sample interval (5 s) before that instant, for the guest and the host series. The test that pinned the old cut now expects 19 samples instead of 20. | A sample's `t` is the start of its sampler cycle, and its Panel probe runs last in the cycle, about 2 s later on an external-DNS node. The cycle that started 1.85 s before the stop probed the Panel after it (arch run-a). |
| H15 | The raw port-hold events file is copied into the evidence (listed in `SHA256SUMS`) by the owner-continuation step when it releases the hold and again by `collect`. | Only its summary was kept; the raw file stayed in the stopped overlays. |

**Start kinds that were not reached (upd4 F4, F5).** `kind_not_reached` uses
positive evidence only. The update's failure line was read and names another
code. There is no start-check reason and no sidecar naming a start kind. The
observer saw no `completion.pending` (`false`, not unknown). The previous release
is installed. Every automatic dispatch is a rollback. The request ended in one
of two shapes: stopped before change (the H8 rule on an unchanged
`failed`/`none` status, F4) or `recovered`/`rollback_verified` without a
start-kind code (F5). When all of these hold, the start-check and real-start
judges answer `not-measured` with the reason, judge no rule and report no
finding. The `kind-expectation` step is `inconclusive`
(`kind-not-reached: ...`), and the cell's `overall` is
**`inconclusive-kind-not-reached`**, unless a step failed. `result.kind.measured`
is `false`. For a real-start cell in that case, `verdicts` uses the ordinary
Panel rule (down only during the operation) instead of "down until the end".
Any sign that the candidate ran keeps the kind judged normally: a start-kind
code, a start-check reason, `completion.pending`, the candidate installed, or a
forward dispatch. So does any unknown record. The product's own stop is still
recorded by the run (journal, track stop, outcome).

**Product states after upd4 (upd5 driver knowledge, offline only).** The
driver knows `update_preflight_refused` (`FAILURE_CODES`, the sidecar and the
CLI text, which is read from `cmd/recovery/main.go` with the request's own
journal command). A `failed`/`none` record with `previous_failure=update_failed`
and a preflight stop code (`update_preflight_refused` or
`recovery_runtime_preflight_failed`) is final for the request. `track` stops
there at once, without the 600 s H8 wait, and `terminal` expects the previous
release and an `unchanged` card. The outcome class is `stopped-before-change`
only when the updater's failure line names the same code with
`state=unchanged`. It is `-unconfirmed` while the line is unread and
`-contradicted` when the line says otherwise. The line's `step=`/`class=` and
the Panel's bounded form are parsed. In a real-start or start-check cell, that
confirmed stop is a fourth positive-evidence shape of "kind not reached". The
sidecar, when read, must name the same code. `pause_pending` is recovery in
progress: it is neither the pause nor an owner action, and its CLI text is
checked verbatim. At the pause, `renewal_before_update` is recorded and compared
with the pre-update timer snapshot (`certbot.timer`/`certbot-renew.timer`
enabled or active means `on`, the rule in `update.sh`). `collect` cross-checks
the `<id>.renewal` sidecar, read only. A mismatch is a finding, while
`not-recorded` (an older product) is not. The CLI's renewal sentence (on or
off) is checked verbatim. The card keeps each `OutcomeText.fallback` beside the
settled text, and a fallback key missing from the catalogue is a mismatch. The
evaluator subset already covered the new constructs (no extension).

### Offline checks

```sh
python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py -v
python3 -m unittest deploy/e2e/release-recovery/test_recovery_candidate_archive.py -v
```

`test_owner_update_trial.py` has 161 offline tests (21 of them for the upd5
paragraph above). They cover:

- plan validation and dry run, fixture policy/defect and the acceptance-notice
  exemption;
- UI backoff, three-source agreement, D-024 guidance through the product
  catalogues and outcome classification;
- outage and cron windows, reset attribution and database/timer comparison;
- redaction through the real Panel client and evidence writer;
- the guest DNS/retry parsers and the observer (checkpoint proof and fault
  arming without a signal);
- the 2026-09-30 corrections: the wrapper from a path with spaces (dry run; as
  root also every `cell` invocation, through a stub `python3`), the DNS scope
  and draft choices, the stable setup wait, the mail rule (checked against
  the retained setup executions), the excluded tables, the cron precondition, the
  persistent origin unit and its name outside the installer's globs, the
  no-lookup rule, and `collect` after a stop at seed, at origin and at
  preflight;
- the upd2 corrections: 202 or 200 with `accepted: true` only (H6), the Arch
  `getent` shape (H7), and the start-instant lag against every other
  difference (O5);
- upd3: plan validation of the four new cells (also through the wrapper), the
  artifact rules for the S and R roles, the unchanged good and migrate-only
  cells, both fixture patches applied to the current product source (inside
  the shared TLS function, and after every early-exit mode of `main()`), the
  sidecar parser, the update failure line and check reason, the CLI texts
  parsed from `cmd/recovery/main.go` (in both output shapes the product has
  had), the web keys from the catalogues, both judges, and the real-start
  owner continuation (the retry is printed but never run);
- upd4: the H8 rule (alone and inside `track` with a simulated clock), the H9
  direction rule (including the arch-startcheck receipt pair), the H10 card
  (the upd3 rolled-back cards reproduced from the product's own functions, the
  paused card and screen with the cause line, text order following an edited
  source, an unreadable source giving unknown, the judge failing only on a
  real mismatch) and the evaluator's subset, the sidecar v2 rules (`getent`
  rc 2, no inspection inside the preflight window), the port-hold helper (held
  through the updater's exit and every forward attempt, released by the owner,
  a rollback, another snapshot, a finished marker, a lasting unreadable state or
  its bound), the bound derived from the product's files, plan validation of
  the four upd4 cells (also through the wrapper), both new judges, the owner
  continuation order (panel log, printed retry read, port released, retry run
  once), the management-off rules and steps, the update-only verdict window and
  the guest database helpers;
- the upd4 corrections: H11 (the `retry_scheduled` and paused screens equal to
  `h11-product-screen-texts.txt` while the catalogue keeps that wording, with
  the renewal line at the pause; the same observations on the card; line order
  following an edited JSX source; a removed region, a component inside it, a
  renamed component or an empty source giving unknown; the JSX subset and return
  types with object shapes), H12 (ready after a 503 and a `starting` read, then
  the owner state; never ready gives unknown), H13 (the instant before the
  command), H14 (19 samples, and the upd4 cycle that started before the stop
  left out, guest and host), H15 (the raw events file kept at the release and by
  `collect`), and the unmeasured start kinds (F4 and F5 records as
  `not-measured` for both judges, every sign of a running candidate keeping the
  judge, the step `inconclusive`, `overall` `inconclusive-kind-not-reached`
  accepted by the evidence writer, and the ordinary Panel rule in `verdicts`).

`ArtifactProofTests` builds three synthetic archives with `dns-owner-tools/`
in a temporary Git repository and proves them with `prove`; a second test
also proves the five archives B, G, D, S and R. That test and the
Git-backed tests in `test_recovery_candidate_archive.py` need `git` and a
POSIX host; otherwise they are skipped. None of these tests establishes a
native result.
