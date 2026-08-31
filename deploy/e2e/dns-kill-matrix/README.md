# DNS kill matrix manifest

This directory defines the S-1 cell inventory and its disposable execution
harness. The manifest, fixture provisioner, and cell controller remain separate
so inventory generation never boots a VM or delivers a signal.

The raw matrix is:

```text
5 drivers * (1 pre-intent window + 8 phases * 2 journal-write edges)
          * 3 roles * 2 peer states = 510 cells
```

Generate deterministic JSON:

```sh
python3 deploy/e2e/dns-kill-matrix/generate_manifest.py \
  --output deploy/e2e/dns-kill-matrix/manifest.json
```

Check the inventory without writing a file:

```sh
python3 deploy/e2e/dns-kill-matrix/generate_manifest.py --check
python3 deploy/e2e/dns-kill-matrix/test_manifest.py
```

The audited current-code inventory is 268 runnable and 242 explicit N/A cells.
Every N/A cell contains one or more reasons with `file:line` evidence. A future
uncertain case must stay in the runnable denominator and use
`applicability: unverified`; uncertainty is not grounds for N/A.

Important inventory choices:

- `pre-intent` is one window. It is not doubled into fictional before/after
  journal edges.
- PowerDNS adoption retains rollback coverage. Its runnable writes are
  `intent`, `target-verified`, `committed`, `rolling-back`, and `rolled-back`;
  only `target-staged`, `source-stopped`, and `target-started` are phase N/A.
  Adoption can use paired topology, but production deliberately rejects a
  directional `pair_role`; therefore only the standalone matrix role is
  runnable and both paired-primary and paired-secondary are explicit N/A.
- `signed-update-finalize` names the signed-update recovery walker for this
  inventory. Its committed branch reads and removes the journal but does not
  write `committed`; its sole writable matrix phase is the recovery write to
  `rolled-back`.
- Both peer labels remain for standalone cells as invariance controls. The
  standalone manifest has no DNS peer, but silently dropping one label would
  make the raw cross product irreconstructible.
- Paired cells apply an `unreachable` peer state only after exit 137 has proved
  the requested kill. This lets peer-dependent preflight reach the hook while
  still testing recovery with the peer unavailable.
- OS is placement metadata, not another matrix dimension. Certified PowerDNS
  and signed-update rollback cells are placed on Debian 13. BIND cells are
  deterministically spread across Debian 13 and Arch.

The runtime result for a runnable cell must separately record pass, fail, or
unverified. In particular, a cell without a proven exit-137 kill is unverified,
never passed. The execution report's D-021 denominator is the runnable cell
count, not the 510-cell raw inventory.

## QEMU fixture provisioning

`fixture.py` provisions one Debian 13 guest and one Arch guest on a **Linux
QEMU host**. Lifecycle commands fail closed on every other host, including in
dry-run mode: their commands intentionally use KVM or TCG, Unix QMP sockets,
and QEMU `-daemonize`. The Windows/WHXP QEMU build is not a certified host for
this fixture.

Install `qemu-system-x86_64`, `qemu-img`, `curl`, OpenSSH, and either
`genisoimage` or `xorriso` on the Linux host. KVM is the default and is
recommended; `--accel tcg` is available for Linux hosts without KVM. Use a
short absolute work root because Linux limits Unix socket path lengths.

The default [`images.lock.json`](images.lock.json) pins immutable official
artifacts, their exact advertised byte sizes, and the checksum algorithm the
publisher provides:

- Debian 13 build `20260826-2582`, 340262912 bytes, official SHA-512
  `184761b0dad0f9ace02f9298050ca96ce3caa39a461a47706d47ff9698b59933918b91b40177fbd4d392f6446af8b4d18ecb94caca988169b19641606bf34003`.
- Arch build `20260815.573966`, 556609024 bytes, official SHA-256
  `5d8be8d28cfd290f051b0f67df0a6874596ad23de3f3f18b90c91aeb758eb878`.

The provisioner rejects missing, writable, wrong-size, or wrong-digest base
images. Bases are opened only as read-only backing files; every cell gets new
24 GiB qcow2 overlays. No Python cloud-init package is required: the script
writes NoCloud data and invokes the selected ISO builder.

All mutating commands are dry-runs until `--execute` is added. A complete
fixture lifecycle is:

```sh
FIXTURE=deploy/e2e/dns-kill-matrix/fixture.py
ROOT=/var/tmp/cp-dns-kill
CELL=bind__intent__before-write__standalone__peer-reachable

python3 "$FIXTURE" init-root --work-root "$ROOT"
python3 "$FIXTURE" init-root --work-root "$ROOT" --execute
python3 "$FIXTURE" fetch --work-root "$ROOT"
python3 "$FIXTURE" fetch --work-root "$ROOT" --execute
python3 "$FIXTURE" verify-images --work-root "$ROOT"
python3 "$FIXTURE" prepare --work-root "$ROOT" --cell-id "$CELL" \
  --ssh-public-key "$HOME/.ssh/id_ed25519.pub"
python3 "$FIXTURE" prepare --work-root "$ROOT" --cell-id "$CELL" \
  --ssh-public-key "$HOME/.ssh/id_ed25519.pub" --execute
python3 "$FIXTURE" start --work-root "$ROOT" --cell-id "$CELL"
python3 "$FIXTURE" start --work-root "$ROOT" --cell-id "$CELL" --execute
python3 "$FIXTURE" wait-ssh --work-root "$ROOT" --cell-id "$CELL" \
  --identity-file "$HOME/.ssh/id_ed25519" --execute

# Only after run_cell.py has atomically published proof of exit 137:
python3 "$FIXTURE" peer-link --work-root "$ROOT" --cell-id "$CELL" \
  --state down --kill-proof /absolute/path/to/kill-proof.json
python3 "$FIXTURE" peer-link --work-root "$ROOT" --cell-id "$CELL" \
  --state down --kill-proof /absolute/path/to/kill-proof.json --execute

python3 "$FIXTURE" stop --work-root "$ROOT" --cell-id "$CELL"
python3 "$FIXTURE" stop --work-root "$ROOT" --cell-id "$CELL" --execute
python3 "$FIXTURE" teardown --work-root "$ROOT" --cell-id "$CELL"
python3 "$FIXTURE" teardown --work-root "$ROOT" --cell-id "$CELL" --execute
```

Each guest has a NAT management NIC with loopback-only SSH forwarding and a
second NIC on an isolated, static `192.0.2.0/24` peer link. The peer device is
controlled through per-VM QMP sockets. `peer-link` refuses both dry-run and
execution unless its proof is a matching, positive-PID, `kill_proven: true`,
exit-137 record, which prevents an unreachable peer from blocking preflight
before the intended kill.

The work root is valid only when it contains the exact harness marker plus real
non-symlink `images` and `cells` directories. Cell directory names are hashes of
full manifest IDs to keep QMP socket paths bounded; the plan still records and
validates the full ID. Teardown is dry-run by default, stops guests via QMP with
no signal fallback, and recursively deletes only the resolved cell beneath the
validated work root.

Run the offline generation checks with:

```sh
python3 deploy/e2e/dns-kill-matrix/test_fixture.py
```

## Guest bootstrap and source provenance

`guest_bootstrap.py` consumes the fixture plan's exact SSH destination,
identity, and known-hosts file. It installs already-built artifacts; it never
builds inside a cell and never runs the full installer. Build the current tree
on Linux first:

```sh
ARTIFACTS=/root/celikpanel-s1-artifacts
mkdir -p "$ARTIFACTS"
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$ARTIFACTS/agent" ./cmd/agent
CGO_ENABLED=0 go build -trimpath -buildvcs=false -tags dns_kill_matrix \
  -o "$ARTIFACTS/agent.kill" ./cmd/agent
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$ARTIFACTS/panel" ./cmd/panel
CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -o "$ARTIFACTS/dns-kill-trigger" ./cmd/dns-kill-matrix-trigger
```

For the first Arch standalone BIND cell, install and prepare the honest empty
source with:

```sh
BOOTSTRAP=deploy/e2e/dns-kill-matrix/guest_bootstrap.py
CELL=bind__intent__before-write__standalone__peer-reachable

python3 "$BOOTSTRAP" install --work-root "$ROOT" --cell-id "$CELL" \
  --node arch --identity-file "$HOME/.ssh/id_ed25519" \
  --source-fixture uninitialized --agent "$ARTIFACTS/agent" \
  --tagged-agent "$ARTIFACTS/agent.kill" --panel "$ARTIFACTS/panel" \
  --trigger "$ARTIFACTS/dns-kill-trigger" --web-dir "$PWD/web/dist" --execute
python3 "$BOOTSTRAP" prepare-bind --work-root "$ROOT" --cell-id "$CELL" \
  --node arch --identity-file "$HOME/.ssh/id_ed25519" \
  --source-fixture uninitialized --execute
```

This emits the exact initial trigger, retry, and recovery-probe argv arrays,
plus the absolute `source-proof.json` path required by `--source-proof`.
The bundle installs the exact controller and selected manifest, and preparation
writes a complete ready-to-run array to
`/var/lib/celikpanel-dns-kill-matrix/controller-argv.json`. The generated
scenario, source proof, manifest, and controller array are root-owned mode
0600. The scenario carries
`source_fixture: uninitialized`, empty source engine, source epoch/revision
`0/0`, and target epoch `1`. Preparation proves that the engine receipt,
switch journal, both engines' ownership/install receipts, and all three DNS
units are absent/inactive. It also records that global UDP and TCP port 53 are
bindable and that no authoritative answer was observed; the controller does
not query an unrelated local resolver for this empty source. It does not
pretend that this shape covers stopped-source recovery.

The standalone Debian adoption cells use a distinct measured path. For
`pdns-adopt__intent__after-write__standalone__peer-reachable`, for example:

```sh
CELL=pdns-adopt__intent__after-write__standalone__peer-reachable

python3 "$BOOTSTRAP" install --work-root "$ROOT" --cell-id "$CELL" --node debian13 --identity-file "$HOME/.ssh/id_ed25519" --source-fixture external-pdns-adoption --agent "$ARTIFACTS/agent" --tagged-agent "$ARTIFACTS/agent.kill" --panel "$ARTIFACTS/panel" --trigger "$ARTIFACTS/dns-kill-trigger" --web-dir "$PWD/web/dist" --execute
python3 "$BOOTSTRAP" prepare-pdns-adopt --work-root "$ROOT" --cell-id "$CELL" --node debian13 --identity-file "$HOME/.ssh/id_ed25519" --source-fixture external-pdns-adoption --execute
```

`prepare-pdns-adopt` installs the two PowerDNS source prerequisites under the
same masked-service package guard, constructs and starts the exact unreceipted
external authority, and then seals
`source-external-pdns-preimage.json`. It deliberately does not invoke a setup
adoption RPC: the first production `pdns-adopt` call is the tagged measured
operation. It leaves `pdns.service` active and authoritative while stopping
only the ordinary agent and panel coordinators. Production engine state,
switch journal, and both engines' active/install ownership receipts must all
remain absent at tagged launch.

The sealed external-adoption preimage binds the package evidence, scenario,
configs, official schema, database bytes, unit state, and authoritative
UDP/TCP preflight. Its live SQLite contract is the same bounded no-follow
shape used by source-adoption v2: the rollback journal is absent; the WAL is
regular `pdns:pdns` 0640, single-link, on the database device, exact
device/inode, and empty; the SHM has the same safe metadata with exact
device/inode and size 32768, but no content hash
(`content_policy: volatile-unhashed`). The guest rechecks all identities
after its immutable SQLite query, and the controller independently revalidates
them without reading or hashing SHM before tagged launch.

PowerDNS adoption requires that package, config, database, and live service
preimage to exist, so package installation is intentionally fixture work, not
part of the measured adoption operation. The measured call performs
certification and journaled adoption without package installation; it therefore
does not enter the BIND package-install heartbeat window.

Current production code rejects PowerDNS target/adoption work outside the
certified Debian+APT path (`cmd/agent/dns_engine_pdns_unit.go:63-71`). Therefore
every critical BIND `source-stopped` and `target-started` cell is placed on
Debian 13 and declares `source_fixture_policy: managed-pdns-required`. Prepare
one of those cells with `--node debian13 --source-fixture managed-pdns`.
Bootstrap first proves the BIND target and both PowerDNS source packages absent.
It refreshes APT, masks `pdns.service`, and installs only `pdns-server` plus
`pdns-backend-sqlite3` outside the service-mutation ledger. Before the
external source starts it proves the package hook could not start PowerDNS,
removes and proves absence of that temporary mask, proves BIND remains absent, and
writes canonical root-only `source-preinstall-pdns.json`. This source-only
fixture step avoids the known long APT-worker/heartbeat overlap without erasing
the measured BIND package-install window.

Bootstrap then constructs an external, deliberately unreceipted PowerDNS
authority. It leaves Debian's package-owned main config and certified vendor
unit in place, writes create-new standalone managed config, initializes a
create-new SQLite database from the package-owned official
`schema.sqlite3.sql`, inserts the exact `s1-kill.test` snapshot, and enables
the vendor `pdns.service`. Before production sees it, the harness proves that
all engine state, switch-journal, active-ownership, and install-ownership
receipts are absent, BIND remains uninstalled, and the source answers
authoritatively over both UDP and TCP.

The untagged trigger then invokes unchanged production `pdns-adopt` with an
unresolved `0/0` source. Production independently certifies the package,
config, database, service identity, sole port-53 authority, and live zone before
it writes engine state. Normal production finalization publishes active
ownership and removes the committed journal; adoption creates no install
ownership. Bootstrap requires active ownership bytes to equal engine-state
bytes, requires both BIND receipts and the PowerDNS install receipt to remain
absent, proves BIND is still uninstalled, and proves every external config and
database identity/hash stayed unchanged across adoption.

`source-adoption-pdns.json` records those exact package, setup, config,
official-schema, database, unit, production-receipt, and measured-target
claims. Its v2 database contract describes the live SQLite/WAL shape rather
than claiming every sidecar is absent: `pdns.sqlite3-journal` must be absent;
`pdns.sqlite3-wal` must be a regular, single-link, `pdns:pdns` 0640 file on the
database device with the exact recorded device/inode and zero-byte size; and
`pdns.sqlite3-shm` must have the same safe file shape and exact recorded
device/inode with a 32768-byte size. The SHM proof deliberately records no
content digest: SQLite mutates that shared-memory content while the source is
serving, so its contract is exact metadata plus `volatile-unhashed`, not
immutable bytes. Both bootstrap and the controller use no-follow opens and
compare `lstat`/`fstat` metadata before and after inspection; a symlink,
replacement, link-count drift, owner/mode/device/inode/size drift, nonempty
WAL, or present rollback journal fails closed before tagged launch.

`source-proof.json` binds both that file and
`source-preinstall-pdns.json` by absolute path and SHA-256 (or explicit
`absent` sentinels for an uninitialized source). The controller securely
re-reads both artifacts, rejects path/hash/schema or safety-claim drift,
re-hashes the live source artifacts, proves production state equals active
ownership, proves all transitional/target receipts are absent, and repeats its
own authoritative UDP+TCP query before launching the tagged agent. No source
engine-state or ownership receipt is hand-written.

The current guest source-proof producer is complete for three exact shapes:
`uninitialized` BIND, `managed-pdns` BIND at the two critical stopped-source
phases, and standalone Debian `external-pdns-adoption`. The controller requires
`absent-by-proof` provenance for the first; production setup-adoption hashes
plus source-preinstall/source-adoption hashes for the second; and
`harness-external-pdns-preimage` plus source-preinstall/external-preimage
hashes for the third. It fails closed for `managed-bind` and
`legacy-pdns-secondary` until equally strict fixture producers are
implemented. Those remaining cells are harness-blocked/unverified; they must
not be counted as passed or failed.

Installed runtime separation is deliberate:

- `/opt/celikpanel/bin/agent` is the only binary referenced by the production
  `celikpanel-agent.service` unit.
- `/opt/celikpanel/bin/agent.kill` is launched directly only by `run_cell.py`.
- `/opt/celikpanel/libexec/dns-kill-run-cell.py` and
  `/var/lib/celikpanel-dns-kill-matrix/manifest.json` are the bundled
  controller and the exact selected manifest.
- `/opt/celikpanel/bin/dns-kill-trigger` publishes the measured trigger
  identity receipt create-new. Bootstrap creates its root-owned 0700 parent but
  must leave the receipt itself absent before the initial RPC.
- The preliminary PowerDNS source transaction has a different identity receipt
  and its digest is recorded in `source-proof.json`; it is never reused for the
  measured BIND cell. The external adoption path has no preliminary production
  transaction at all.
- The first administrator is created through the panel's supported
  `--create-admin` CLI. Its fixture-only password is derived in memory, sent on
  stdin, never printed, and never written by the harness. Panel readiness means
  continuous `active` state plus successful TCP connects for five seconds, not
  one transient systemd sample.

The production agent runs as UID `root` with primary GID `celikpanel`. The
tagged child must inherit the same identity; a plain root shell normally has
primary GID `root` and would change new receipt metadata. Execute the prepared
array without shell re-parsing under the production identity:

```sh
sudo /usr/sbin/runuser -u root -g celikpanel -- /usr/bin/python3 -c \
  'import json,os,sys; a=json.load(open(sys.argv[1],encoding="utf-8")); os.execv(a[0],a)' \
  /var/lib/celikpanel-dns-kill-matrix/controller-argv.json
```

The cloud-image fixture user has passwordless sudo for a target user, not a
direct target-group selection. The outer `sudo` therefore becomes root first;
root's `runuser` then establishes the required `root:celikpanel` identity. A
collector or operator must not replace this with `sudo -u root -g celikpanel`.

The generated request ID and nonce are deterministic inside the disposable
cell, while all output artifacts are create-new. Explicit timeout decisions
are 60 seconds startup, 45 minutes boundary/command/recovery, 15 seconds each
for stop and kill, 60 seconds endpoint readiness, five seconds per DNS query,
and a 30-second stability window sampled once per second. An exceeded timeout
is a cell finding; bootstrap adds no retry or sleep to turn it green.

`guest_recovery_probe.py` is installed under `/opt/celikpanel/libexec`. It is
read-only and safe to run twice. It strictly binds the scenario, canonical
trigger identity, deterministic owner, engine state, and finalized idle ledger;
requires the exact target engine/epoch/revision; requires an absent switch
journal; checks target/source systemd states; and fingerprints both engines'
ownership and install-ownership residue. Target ownership must equal active
state, target install ownership must be absent, and a retained prior source
ownership receipt must match the scenario's epoch/revision/topology. Its
fingerprint excludes
timestamps and numeric PIDs but includes semantic failure shape, so an
unchanged non-convergence repeats the same fingerprint while changing recovery
state does not. It reports exact target convergence, exact prior-source
rollback activity, or indeterminate recovery separately from the independent
UDP/TCP serving assertion.

Run the offline guest checks with:

```sh
python3 deploy/e2e/dns-kill-matrix/test_guest_bootstrap.py
python3 deploy/e2e/dns-kill-matrix/test_guest_recovery_probe.py
```

The base images are immutable, but Debian/Arch package repositories are not
pinned by this bootstrap. The BIND target package remains absent until the
measured production switch, so its pre-intent package window is real; only the
different PowerDNS source packages are preinstalled for the setup switch
described above. For `pdns-adopt`, the PowerDNS target is also the live
external source being adopted, so its packages are necessarily preexisting and
the proof labels them `preexisting-required-by-adoption`; no package operation
occurs inside that measured RPC. If a moving repository no longer supplies the
exact package/unit identity certified by the current code, preparation or the
cell fails closed. The harness never preinstalls the measured BIND target.

## Cell controller

`run_cell.py` executes one already-provisioned runnable cell. It does not
prepare driver state or provision a VM, and it derives no paths from a cell ID:
the manifest, state directory, lock, sockets, journal, marker, proof, result,
and transcript are all explicit absolute paths. This is compatible with the
fixture's hashed cell-directory names.

Run the controller itself as UID root with primary GID `celikpanel`, matching
the production unit, and start from an empty environment. For example:

```sh
/usr/sbin/runuser -u root -g celikpanel -- /usr/bin/env -i \
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  LANG=C.UTF-8 /usr/bin/python3 /absolute/repo/deploy/e2e/dns-kill-matrix/run_cell.py ...
```

Plain `sudo` with primary group `root` is rejected. The controller also rejects
caller-supplied `CELIKPANEL_*` variables. It constructs the same six production
paths as `celikpanel-agent.service` (socket, token, state, mutation lock, DKIM,
and runtimes), adds only the exact S-1/fault selectors needed by each child,
uses umask `0027`, and records the tagged and restarted agent UID/GID/umask from
`/proc`. Socket cells must pass `--source-proof`; every cell must pass the
explicit production `--agent-token-file`.

Build the real agent twice. The first binary contains only the tagged boundary
runtime used before the kill; the installed/restarted service must use the
ordinary untagged binary:

```sh
CGO_ENABLED=0 go build -trimpath -buildvcs=false -tags dns_kill_matrix \
  -o /absolute/artifacts/celikpanel-agent.kill ./cmd/agent
CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -o /absolute/artifacts/celikpanel-agent ./cmd/agent
CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -o /absolute/artifacts/dns-kill-matrix-trigger ./cmd/dns-kill-matrix-trigger
```

### Real scenario trigger

The four socket-triggered drivers (`bind`, `pdns-switch`, `pdns-adopt`, and
`pdns-secondary-reconfigure`) use the command above, not a Go test process. Its
controller argv is:

```json
[
  "/absolute/artifacts/dns-kill-matrix-trigger",
  "rpc-switch",
  "--scenario", "/absolute/cell/scenario.json",
  "--identity-receipt", "/absolute/cell/trigger-identity.json",
  "--timeout", "45m"
]
```

The controller supplies `CELIKPANEL_S1_DRIVER` and the cell's exact 32-byte
lowercase-hex `CELIKPANEL_S1_REQUEST_ID`, along with the ordinary agent socket
and token paths. The scenario is a real, non-symlink, bounded regular file with
no group/world write bits and this strict schema:

```json
{
  "schema": "celikpanel-dns-kill-matrix-trigger/v1",
  "driver": "pdns-secondary-reconfigure",
  "source_fixture": "legacy-pdns-secondary",
  "mode": "switch",
  "source_engine": "",
  "target_engine": "pdns",
  "source_epoch": 0,
  "target_epoch": 1,
  "source_revision": 0,
  "topology": "paired",
  "pair_role": "secondary",
  "local_ip": "192.0.2.10",
  "local_ns": "ns1.example.test",
  "peer_ip": "192.0.2.11",
  "peer_ns": "ns2.example.test",
  "zones": []
}
```

Ordinary switch fixtures use the same fields plus their complete canonical
zone snapshots. Adoption uses `mode: adopt`, an empty source, target `pdns`,
and no BIND pair identity. Its optional paired topology is non-directional and
does not make either directional paired matrix role runnable. `source_fixture`
is mandatory provenance, not an RPC
field: `uninitialized` requires the exact empty source with epochs 0 -> 1 and
revision 0; `managed-pdns` and `managed-bind` require the matching positive
source identity; adoption requires `external-pdns-adoption`; secondary
reconfiguration requires `legacy-pdns-secondary`. This lets early Arch BIND
cells declare that they do not exercise stopped-source recovery, while the
critical Debian cells must name and prove a real managed PowerDNS source.

The trigger canonicalizes the production manifest itself; the scenario cannot
select a qualifier, snapshot byte count, mutation owner, or RPC binding. It
rejects a secondary-reconfiguration-shaped manifest under `pdns-switch` and
rejects `signed-update-finalize` as an RPC driver.

The trigger calls `Agent.BeginServiceMutation` with kind
`dns_engine_switch`, target engine, and the canonical manifest qualifier. It
accepts only the exact running lease with `WorkerPID == 0` and empty worker
identity, then heartbeats every five seconds while a separate production
`Agent.SwitchDNSEngineV1` RPC is in flight. A normal completion is accepted
only with the exact finalized v2 ledger receipt. Transport loss without that
receipt exits 75 and leaves the lease for startup recovery; this is the
expected trigger-side shape after the controller kills the agent, but is never
itself proof of a kill. Only the controller's marker, stopped-process identity,
SIGKILL, reap, and normalized exit 137 establish a verified boundary.

Before `BeginServiceMutation`, the initial command atomically creates and
fsyncs the required mode-0600 identity receipt. It binds schema, cell ID,
driver, source provenance, request ID, deterministic owner ID, and manifest
qualifier. The owner is the first 16 bytes of SHA-256 over
`celikpanel/dns-kill-matrix-owner/v1`, a NUL, request ID, a NUL, and cell ID.
An existing receipt makes an initial invocation fail closed.

After the ordinary agent restarts, the socket-mode recovery command must be
the exact same argv with only `rpc-switch` changed to `rpc-retry`:

```json
[
  "/absolute/artifacts/dns-kill-matrix-trigger",
  "rpc-retry",
  "--scenario", "/absolute/cell/scenario.json",
  "--identity-receipt", "/absolute/cell/trigger-identity.json",
  "--timeout", "45m"
]
```

The retry strictly reads the existing receipt, checks the exact durable job,
and sets `Begin.Resume=true` only for an exact failed/interrupted or
still-running identity. An
exact finalized v2 receipt is idempotent success without another mutating RPC;
an absent, changed, or other-status job is rejected. This exact retry is
required for the pre-intent window, where no DNS journal exists and the ledger
is the only durable link to the original request.

`signed-update-finalize` deliberately does not use this command. It has no RPC
entry point: the controller's startup mode invokes the production agent
one-shot described below under an inherited external mutation flock.

Every command argument to the controller is a JSON argv array. No command is
evaluated through a shell, and every executable path must be absolute. The
tagged agent receives exactly the eight
`CELIKPANEL_DNS_KILL_MATRIX_*` selectors plus the normal explicit state,
mutation-lock, and socket paths. A ready-pipe descriptor is inherited directly.
All selectors are stripped from the scenario, peer rendezvous, recovery, and
restart environments.

The controller requires every timeout on the command line and records each
chosen value in the result. Normal drivers use `--trigger-mode socket`: the
controller waits for the tagged agent socket before running the required
scenario command. A hook that fires earlier, fails to publish its exact
nonce/identity marker, fails to enter `/proc` state `T`, or cannot be reaped
as normalized exit 137 is `unverified`. The proof is create-new, mode 0600,
file-synced and directory-synced before a paired/unreachable peer rendezvous can
complete.

`run_cell.py` runs inside the kill guest, while QMP sockets and
`fixture.py peer-link` belong to the QEMU host. Never pass `fixture.py
peer-link` directly as `--peer-partition-command`: that would look for host QMP
sockets in the guest. The simplest guest-local command is the exact argv
`["/usr/bin/ip","link","set","dev","peer0","down"]`. A host-QMP design may
instead use a bounded guest rendezvous after the host consumes the durable kill
proof and changes the link.

Command exit zero is not peer proof. For every paired cell, the controller
derives the peer IP from the strict scenario and requires a real `SSH-` banner
on port 22 before tagged launch. After the kill/callback, a reachable cell must
return another banner; an unreachable cell must fail every connect throughout
the explicit stability duration. The observations are recorded. A callback
failure or peer-state mismatch makes the cell `unverified`, never passed or
failed. Standalone `peer-unreachable` cells are invariance controls and take no
callback. Paired/unreachable cells require one; every other cell rejects one.

The only startup-trigger exception is the manifest's broadened
`signed-update-finalize/rolled-back` recovery writer. Select
`--trigger-mode startup`, omit `--trigger-command`, and supply exactly this as
`--tagged-agent-command`:

```json
[
  "/absolute/artifacts/celikpanel-agent.kill",
  "--prepare-bind-generation-root-under-external-lock"
]
```

Supply the corresponding ordinary, untagged binary as the required
`--recovery-command`:

```json
[
  "/absolute/artifacts/celikpanel-agent",
  "--prepare-bind-generation-root-under-external-lock"
]
```

That mode first proves a root-owned 0600 `rolling-back` journal and its
matching terminal-failed, worker-idle ledger job. It opens the canonical
mutation lock with `O_NOFOLLOW`, verifies the real root-owned 0600 empty
single-link inode, takes a nonblocking exclusive flock, and inherits that
descriptor alongside the ready pipe. The one-shot creates no agent socket, so
its pre-boundary proof is the stable child PID/start ticks plus those durable
preconditions. After exit-137 proof is published, the controller runs the
ordinary untagged agent twice with the same one-shot argument and inherited
lock FD, with all kill selectors removed. Each bounded recovery must exit zero
and is followed immediately by a read-only recovery probe. Only after both
attempts does the controller close its lock descriptor and restart the
ordinary services. Repeating the one-shot under the same held flock is the
signed-update convergence observation; it is not N/A. No other driver or phase
can select startup mode.

The initial journal may be absent or fixture-preseeded for rollback recovery.
At the stopped boundary, `pre-intent` and `intent:before-write` require an
absent journal, an after-write requires the selected phase, and a before-write
requires its exact predecessor. Every runnable non-signed `rolling-back` or
`rolled-back` marker must also contain one exact
`celikpanel-dns-kill-matrix-rollback-precursor/v1` object. It proves that the
same tagged request and driver returned the injected error at
`target-staged:after_write` for BIND, PowerDNS switch, and PowerDNS secondary
reconfiguration, or at `intent:after_write` for PowerDNS adoption. Its nested
observed journal is bound to the canonical journal path and the same complete
journal identity. Forward and signed-update markers must omit this field.

The controller derives the stopped on-disk phase from that fixed path; there
is no caller-supplied phase override. `rolling-back:before-write` expects the
driver-specific precursor phase, `rolling-back:after-write` expects
`rolling-back`, `rolled-back:before-write` expects `rolling-back`, and
`rolled-back:after-write` expects `rolled-back`.

After the proven kill (and optional host-side partition plus guest
rendezvous), recovery precedes final liveness. In socket mode the controller
restarts the untagged agent, proves a replaced and connectable socket, validates
the durable trigger identity receipt, then runs the exact `rpc-retry` command
twice. In startup mode it runs the two one-shots under the retained flock as
described above. Each recovery attempt records its complete argv, return code,
output, timeout status, and post-attempt identity-receipt check where
applicable. A read-only recovery probe follows each attempt and must print
exactly one JSON object to stdout:

```json
{
  "schema": "celikpanel/dns-kill-recovery-probe/v1",
  "converged": true,
  "recovery_outcome": "target_converged",
  "active_dns_engine": "bind",
  "fingerprint": "64-lowercase-hex-characters",
  "detail": "converged"
}
```

Both probes must be valid and are compared by fingerprint. Their diagnostic
`recovery_outcome` distinguishes exact target convergence, an exact prior source
that is active after rollback, and indeterminate state. Combining that with the
controller's final DNS query produces `target_converged`,
`rolled_back_source_serving`, `repeated_nonconvergence`, or `changed/race`.
The probes observe recovery; they do not substitute for either retry. Only
after the second attempt and probe does the controller restart the panel,
require the agent to remain up, require a reachable panel TCP port, and require
authoritative, non-truncated, successful DNS answers over both UDP and TCP. It
repeats those final checks through the caller-selected stability window.

The D-021 safety result deliberately does not require target convergence,
journal absence, or a zero retry exit. `safety_status` is failed only when the
proven-kill cell violates one of the three requested post-restart assertions:
DNS is not serving, the panel does not start/stay reachable, or the agent does
not stay running through the stability window. A safe rollback can therefore
have `safety_status: passed` with
`recovery_outcome.classification: rolled_back_source_serving`. Retry exits,
receipt checks, and both probe shapes remain fully recorded under diagnostics.
An unproven kill, peer dimension, or execution identity is `unverified` and
must be rerun; it is not silently counted in `<failed>/<total>`.

The controller creates three per-cell artifacts without replacement:

- `kill-proof.json` uses `celikpanel/dns-kill-proof/v1` and exists only
  after exact exit-137 proof.
- `result.json` records overall `passed`, `failed`, or `unverified`, the
  independent `safety_status`, `recovery_outcome`, every assertion, both
  recovery attempts, both post-attempt probes, timeouts, and artifact hashes.
- The raw transcript interleaves timestamped controller records with uncropped
  child output (bounded to 1 MiB per synchronous command).

Exit status is 0 for a verified safety pass, 1 for a verified safety failure,
2 for an unverified boundary/peer/execution dimension, and 64 for a pre-result
controller/preflight error. Run isolated
controller checks with:

```sh
python3 deploy/e2e/dns-kill-matrix/test_run_cell.py
```
