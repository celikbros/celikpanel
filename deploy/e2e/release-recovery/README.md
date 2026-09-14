# Disposable release and recovery lab

*[Türkçe](README.tr.md) · P0.1 evidence tooling*

This directory prepares fresh Debian 13 and Arch QEMU guests, installs the real
Alpha75 release, prepares a bounded DNS fixture through the installed Agent, and
collects native observations. **It does not establish a native PASS or close
P0.1 by existing, booting guests, or passing its offline tests.** Full actual
update/automatic rollback and native workload acceptance remain required by the
[resilience contract](../../../docs/RESILIENCE-CONTRACT.md).

## Recorded native results

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

- A nonempty or unsafe SQLite WAL yields `unknown`; the collector does not
  ignore it, checkpoint it or create SHM. Obtain a separately proven consistent
  snapshot when needed. A live database byte hash is not a semantic data check.
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
