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

Six clean-bundle native BIND trials are recorded at the
[rollback after-write](NATIVE-BIND-ROLLBACK-20260925.md),
[rollback before-write](NATIVE-BIND-ROLLBACK-BEFORE-WRITE-20260925.md),
[source-stopped after-write](NATIVE-BIND-SOURCE-STOPPED-20260925.md),
[source-stopped before-write](NATIVE-BIND-SOURCE-BEFORE-WRITE-20260925.md) and
[target-started after-write](NATIVE-BIND-TARGET-STARTED-20260925.md) and
[target-started before-write](NATIVE-BIND-TARGET-STARTED-BEFORE-WRITE-20260925.md)
boundaries. They cover six runnable cells; the rest of the matrix is still open.
The [BIND inverse guard regression](NATIVE-BIND-STOP-GUARD-20260925.md)
repeats one of those cells with the required native stop proof; it adds no
new matrix coverage.
The [shared stop guard regression](NATIVE-SHARED-STOP-GUARD-20260925.md)
repeats that same cell after extraction into `dnsenginerecovery`. It also adds
no new matrix coverage.
The [cgroup stop guard regression](NATIVE-DNS-CGROUP-GUARD-20260925.md)
repeats it with cgroup-v2 population checks. It adds no matrix coverage.
The [shared phase checkpoint regression](NATIVE-DNS-PHASE-CHECKPOINT-20260925.md)
also repeats the rolled-back/before-write cell and adds no matrix coverage.
The [Arch target-staged/before-write trial](NATIVE-BIND-TARGET-STAGED-ARCH-20260925.md)
adds one new early-phase runnable cell with a proved uninitialized source. Seven
runnable BIND cells have now passed; the remaining matrix stays open.
The [PowerDNS external-adoption intent/after-write trial](NATIVE-PDNS-ADOPTION-INTENT-20260925.md)
adds the first runnable `pdns-adopt` cell with a sealed real external authority.
It proves same-request forward convergence after a SIGKILL, not the later
rollback or management-absent recovery boundaries.
The [PowerDNS adoption rolled-back/before-write trial](NATIVE-PDNS-ADOPTION-ROLLBACK-20260925.md)
adds a second `pdns-adopt` cell. Its terminal classification was forward
`target_converged`, so it does not prove source rollback or journal retirement.
The [PowerDNS adoption target-verified/before-write trial](NATIVE-PDNS-ADOPTION-TARGET-VERIFIED-BEFORE-WRITE-20260925.md)
adds a third `pdns-adopt` cell with proven exit 137, same-request forward
convergence, and 31/31 healthy Agent/Panel/UDP+TCP DNS samples. Independent
inverse, reboot, owner-edit and uninterrupted-DNS acceptance remain open.
The [PowerDNS adoption target-verified/after-write trial](NATIVE-PDNS-ADOPTION-TARGET-VERIFIED-AFTER-WRITE-20260925.md)
adds a fourth `pdns-adopt` cell with the retained journal at `target-verified`,
exit 137, same-request forward convergence and 31/31 healthy post-recovery
samples. It does not prove independent inverse or the remaining fault matrix.

The [real deleted-child PowerDNS trial](NATIVE-PDNS-DELETED-CHILD-20260925.md)
repeats the adoption intent/after-write cell with an active parent and one
frozen deleted child. It exercises strict negative SOA parsing against real
PowerDNS over UDP and TCP before and after SIGKILL, then observes same-request
Agent-mediated forward convergence. The optional preparation switch is
`--include-deleted-child` on `prepare-pdns-adopt`; the default source fixture
remains the existing single active zone. This does not add matrix phase
coverage or prove the independent post-kill inverse.

The [deleted-child quiesced observer trial](NATIVE-PDNS-DELETED-OBSERVER-20260925.md)
repeats that same cell with the independent read-only status at the proven
post-kill boundary. It verified one active parent and the absent child over
UDP and TCP before Agent restart. It does not execute an inverse or add
phase coverage.

The [corrected Agent deleted-zone parity trial](NATIVE-PDNS-AGENT-DELETION-PARITY-20260925.md)
repeats the same cell with Agent completion using the shared strict negative-SOA
validator. It passed the proven kill, independent 1/1 deleted-child proof,
same-request convergence and 31/31 healthy samples. It adds no phase coverage
or independent inverse.

The [shared PowerDNS rollback regression](NATIVE-PDNS-ADOPTION-SHARED-ROLLBACK-20260925.md)
repeats that cell after the Agent adopted the shared fail-stop rollback sequence.
It adds no matrix coverage or independent recovery proof.
The [shared database reader native regression](NATIVE-PDNS-SHARED-READER-20260925.md)
repeats the same adoption rollback cell with the consolidated Agent reader.
It does not exercise the independent quiesced observer.
The [native quiesced PowerDNS observer trial](NATIVE-PDNS-QUIESCED-OBSERVER-20260925.md)
repeats that cell and records the independent read-only status immediately after
the proven kill, before Agent restart. It matches the frozen database bytes and
native process/listeners at that point but does not execute an inverse or add a cell.

The [PowerDNS pre-retry startup observation](NATIVE-PDNS-PRE-RETRY-OBSERVATION-20260925.md)
repeats that adoption rollback cell with a read-only probe before the same-request
retry. The pre-retry result was indeterminate; only the retry converged forward.
It adds no new matrix cell and does not prove automatic recovery.

The [managed-source pre-retry serving trial](NATIVE-DNS-PRE-RETRY-SERVING-20260925.md)
repeats the BIND rollback cell and proves exact prior PowerDNS rollback plus
authoritative UDP/TCP answers before the same-request retry. It demonstrates
Agent-mediated startup recovery at that boundary, not an Agent-independent
inverse, and adds no new matrix cell.

The [exact-request PowerDNS observer trial](NATIVE-PDNS-EXACT-REQUEST-20260925.md)
repeats the adoption rollback cell with the same fixed read-only observer
bound to its verified request ID. It observes the retained journal before
Agent restart and the failed ledger job after Agent startup retires that
journal, before the same-request retry. It adds no matrix coverage or
independent inverse claim.

The [native PowerDNS SQL observer trial](NATIVE-PDNS-SQL-OBSERVER-20260925.md)
repeats the adoption rollback cell after a proven SIGKILL. Under the host
locks, the independent read-only observer matches frozen database bytes,
zone and peer rows, SQLite integrity and native port-53 ownership. It
does not authorize an independent inverse or add matrix coverage.

The [native PowerDNS SOA observer trial](NATIVE-PDNS-SOA-OBSERVER-20260925.md)
repeats the same interrupted adoption cell. The independent quiesced reader
matches the frozen active-zone SOA serial through authoritative UDP and TCP
answers at a verified local native listener. It adds no inverse authority or
new matrix cell.
The [native PowerDNS config observer trial](NATIVE-PDNS-CONFIG-OBSERVER-20260925.md)
repeats the same interrupted adoption cell with exact on-disk config owner,
mode, ACL, path and byte proof around the live SOA observation. It adds no
inverse authority or matrix cell.

The [shared PowerDNS native-source proof trial](NATIVE-PDNS-SHARED-NATIVE-PROOF-20260925.md)
repeats that cell after a proven SIGKILL. It verifies the combined read-only
native source observation before Agent restart and 31/31 healthy post-retry
samples. It does not execute an independent inverse or add matrix coverage.

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
pretend that this shape covers stopped-source recovery. `prepare-bind` accepts
this empty source at `pre-intent`, `intent` and `target-staged` for standalone
and Arch paired-primary cells, and additionally at standalone
`target-verified` (both edges, either placement host) as a first-install
post-start cut; see [Fresh-install cells](#fresh-install-cells-d-026).

For an Arch `paired-primary` BIND cell with an uninitialized source, prepare
the Debian guest as a standard native catalog secondary **before** starting
the primary operation. After both guests pass `wait-ssh`, run:

```sh
CELL=bind__intent__after-write__paired-primary__peer-reachable
python3 deploy/e2e/dns-kill-matrix/native_bind_peer.py \
  --work-root "$ROOT" --cell-id "$CELL" \
  --identity-file "$HOME/.ssh/id_ed25519" --execute
```

This fixture-only helper installs Debian `bind9`, writes its native
`/etc/bind/named.conf`, validates it, and enables `named.service`. It installs
no CelikPanel binary on the secondary. Debian package repositories must be
reachable. Then use the Arch `install`, `prepare-bind`, and `run-prepared`
commands above with the paired cell ID. The primary must observe the member
zone on the secondary before the switch can finish; SSH reachability alone is
insufficient. The separate [native BIND inspector channel fixture](NATIVE-BIND-INSPECTOR-CHANNEL.md)
exercises owner-enrolled, pinned SSH observation in this pair. Its
[serial-1 observation](NATIVE-BIND-INSPECTOR-CHANNEL-20260926.md) safely
refused loaded-zone absence; production Agent V3 deletion acceptance remains open.
The bounded [current-image pair result](NATIVE-BIND-PAIR-CURRENT-20260925.md)
does not certify other paired roles or engines.
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
every critical BIND `source-stopped`, `target-started`, and `rolled-back`
cell is placed on Debian 13 and declares `source_fixture_policy: managed-pdns-required`. Prepare
one of those cells with `--node debian13 --source-fixture managed-pdns`.

The same unchanged managed PowerDNS preparation is also admitted for the four
standalone BIND cells that the manifest places on Debian 13 with
`driver-specific` policy at `intent` and `target-staged`:
`bind__intent__after-write__standalone__peer-reachable`,
`bind__intent__before-write__standalone__peer-unreachable`,
`bind__target-staged__after-write__standalone__peer-reachable` and
`bind__target-staged__before-write__standalone__peer-unreachable`. An empty
source stays admitted there too. With a serving PowerDNS source on certified
Debian the measured switch writes the V2 frozen-source journal from `intent`
on, so these cells are meant to show the Agent-decides / owner-executes
rollback split before BIND ever started. The Arch-placed early cells, paired
roles, `pre-intent` and every other phase stay refused for `managed-pdns`.
`intent:before-write` cuts before any journal exists, so it cannot show a
rollback decision.

`run_cell.py` runs the two `after-write`/`peer-reachable` cells of that set
only in the explicit mode described in
[Owner inverse after Agent restart](#owner-inverse-after-agent-restart). Without
`--owner-inverse-after-restart`, and for the two `before-write` cells, it still
refuses the managed source before the tagged Agent starts: the managed source
preinstall and adoption proofs stay scoped to `source-stopped`,
`target-started`, `rolled-back` and the exact rolling-back handoff cell, and
the V1 journal schema stays expected for every other cell.
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

The previously exercised guest source-proof producer covered three exact shapes:
`uninitialized` BIND, `managed-pdns` BIND at the two critical stopped-source
phases, and standalone Debian `external-pdns-adoption`. The controller requires
`absent-by-proof` provenance for the first; production setup-adoption hashes
plus source-preinstall/source-adoption hashes for the second; and
`harness-external-pdns-preimage` plus source-preinstall/external-preimage
hashes for the third. A fourth source producer now prepares standalone
Debian managed-bind for pdns-switch through a real untagged Agent BIND switch,
then binds the measured scenario to the exact setup identity, engine state,
active ownership and native UDP/TCP answer. Select
prepare-pdns-switch --node debian13 --source-fixture managed-bind for such a
cell. The paired Debian-primary scenario derives the measured BIND-to-PowerDNS
switch and its setup BIND switch from the same zone, NS records and peer
addresses. The [first bounded BIND to PowerDNS trial](NATIVE-BIND-PDNS-PEER-STAGE2-20260927.md)
proved initial transfer and a terminal V3 edit but left deletion pending without
authenticated native absence proof. The [owner-enrolled SSH follow-up](NATIVE-BIND-PDNS-SSH-STAGE2-20260927.md)
initially recorded pending on its own delete request, then recovered that request to a
terminal receipt using authenticated PowerDNS loaded-zone absence, re-added the
member under a distinct request, and proved authoritative UDP/TCP after both
guests rebooted with primary management disabled. A [separate fresh absent-zone reboot trial](NATIVE-BIND-PDNS-ABSENT-REBOOT-STAGE2-20260927.md)
proved the terminal delete and PowerDNS's native unloaded-zone state persisted
after both guests rebooted with primary management disabled. Measured
kill-matrix phases remain open. A DNS REFUSED response alone is
insufficient removal evidence. Legacy-pdns-secondary remains harness-blocked/unverified. The SSH enrollment used here is fixture-only; the production Agent has a reader, but an ordinary owner enrollment action has not been demonstrated (see [guidance gap](../../../docs/DNS-PEER-INSPECTION-GUIDANCE-GAP.md)).

The exact Debian paired-primary `pdns-switch__intent__after-write__paired-primary__peer-reachable`
cell also accepts `prepare-pdns-switch --node debian13 --source-fixture uninitialized`.
That path proves an empty 0/0 source, stages one `MASTER` member, and leaves
the Arch BIND peer native; pass `--source-fixture uninitialized` to
`native_pdns_bind_peer.py prepare`. It is fixture preparation only.
Every standalone `pdns-switch` cell also accepts `--source-fixture
uninitialized`; see [Fresh-install cells](#fresh-install-cells-d-026).
The [fresh PowerDNS V3 prestart inverse trial](evidence/pdns-v3-prestart-20260928/README.md) independently restored one real `target-enable-intent`/`after-write` SIGKILL cut before PowerDNS start on disposable Debian. Two earlier runs safely refused and led to narrow cgroup/listener proof corrections. The clean rollback verdict and inactive/masked native target survived a management-disabled reboot. The original switch job remains failed, the new journal-free exact-request receipt path has [a separate materialized-ledger Debian reboot trial](evidence/pdns-v3-recorded-status-20260928/README.md) but no producer/inverse replay in that overlay, and P0.4/P0.5 remain open.

The fresh PowerDNS-primary V3 poststart path now has one bounded disposable
fault/reboot result; see [the retained evidence](evidence/pdns-v3-native-20260928/README.md).
The [fresh V3 zone lifecycle trial](evidence/pdns-v3-zone-20260928/README.md) adds terminal child-zone add/edit/delete/re-add and two management-disabled guest reboots. Parentless peer deletion still waits for authenticated native absence proof. The public paired-primary mutation remains blocked while ordinary owner enrollment, independent inverse, later fault cuts and owner edits are unproven.

The Stage 2 cross-engine fixture has a bounded native path for a Debian
managed-BIND primary and a panel-free Arch PowerDNS secondary; see the linked
trial above for exact receipts and open acceptance work. The
paired `prepare-pdns-switch` scenario uses the same zone, pair role, addresses,
and NS records as its production managed-BIND source setup. After `guest_bootstrap.py install` on Debian and before
`prepare-pdns-switch`, run `native_pdns_peer.py prepare` with the same work
root, cell ID, identity and `--execute` to provision the isolated Arch peer.
The first native attempt used the reverse order and production refused the
managed-BIND source because its member had not converged on the peer; that
failed attempt is not acceptance evidence. The peer bootstrap checks the guest marker and absence of management
binaries, installs Arch's packaged PowerDNS and SQLite backend, initializes a
CONSUMER row for the exact primary catalog, starts the native `pdns.service`,
and waits for production BIND's first publication and NOTIFY. The peer must
be prepared before the Debian managed-BIND source switch, which requires
native secondary convergence before it can complete. The fixture permits
catalog AXFR only from the paired primary and loopback; production V3 pair
proof requires the primary to read the peer catalog. This remains outside a
completed kill-matrix cell.

For the exact disposable Frankfurt/Boston authority shape, pass `--authority-acceptance`
to the paired `prepare-pdns-switch` step. Then run `native_bind_pdns_authority.py`
with the same work root, cell ID and identity and `--execute`. It checks
both native endpoints over UDP/TCP for exact SOA, apex NS and host A answers;
repeat it after disabling management and rebooting both guests, supplying
the earlier boot IDs. [The bounded native result](NATIVE-BIND-PDNS-AUTHORITY-20260927.md)
does not prove later mutation propagation or installed-panel recovery.

`native_pdns_peer.py observe --expect present --address 192.0.2.10` reads the
Arch SQLite catalog/member rows and member A record, compares the primary
catalog AXFR membership, and requires authoritative UDP and TCP answers from
PowerDNS. Repeat with the expected address after an edit. After an authorized
production member delete, use `--expect absent`; it requires no member in the
primary catalog AXFR, no loaded member row or record on the secondary, and no
old A answer. A negative DNS reply alone does not pass. Repeat the same
observation after an orderly guest reboot to establish native loading. These
observations must be retained with the exact production add/edit/delete
operation receipts and reboot transcript before claiming acceptance. The probe
is read-only and does not create those operations or reboot a guest. The first linked trial supplies initial loading, edit, delete-native-state
and reboot observations, but its exact deletion remained pending with
`dns_peer_enrollment_required`. The subsequent owner-enrolled SSH trial supplies
a terminal same-request delete recovery and a distinct terminal re-add, plus
native presence after management-disabled reboot. The separate absent-state reboot trial proves native absence after reboot
for a fresh deletion; neither trial completes a measured fault cell.

The [bounded PowerDNS-primary to panel-free BIND-secondary trial](NATIVE-PDNS-BIND-PEER-STAGE2-20260927.md)
adds the inverse engine combination, but is a failed acceptance attempt. Initial
BIND catalog transfer and authoritative secondary answers passed. One
BIND-to-PowerDNS switch then failed exact producer base-record validation; its
rollback could not confirm the staged target, leaving the exact ledger request
`running` and DNS services on the disposable primary inactive while the BIND
secondary continued serving its prior data. The raw evidence is retained and
neither operation was retried. Resolve the SOA/catalog mismatch and rollback
verification failure, then prove the switch reaches a safe terminal result and
primary service remains available before counting this combination. The separate owner-enrolled SSH trial now provides exact-operation recovery,
PowerDNS loaded-zone absence proof and a distinct re-add for the BIND-primary
path; Stage 2 still needs the integrated PowerDNS-primary setup admission,
remaining supported pair combinations and measured fault phases.

After a supported fixture is prepared, run its exact generated controller array
without copying or editing a guest launcher:

```sh
python3 "$BOOTSTRAP" run-prepared --work-root "$ROOT" --cell-id "$CELL" \
  --node debian13 --identity-file "$HOME/.ssh/id_ed25519" \
  --source-fixture managed-pdns
python3 "$BOOTSTRAP" run-prepared --work-root "$ROOT" --cell-id "$CELL" \
  --node debian13 --identity-file "$HOME/.ssh/id_ed25519" \
  --source-fixture managed-pdns --execute
```

The first invocation is a dry run. Execution uses only the fixture's SSH
destination and known-hosts policy. In the guest it requires the root-owned,
single-link, mode-0600 controller argv file, verifies the exact cell ID,
and runs the existing controller as root with primary group celikpanel under
the production path and language environment. It preserves the controller's
0/1/2/64 exit status. The controller still enforces create-new result
artifacts, so rerunning an already measured cell is refused rather than
starting a second mutation.

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

### Fresh-install cells (D-026)

Owner decision D-026 (`docs/DECISIONS.md`) accepts same-operation Agent
recovery for a first engine install, whose prior state is "no DNS engine". Its
remaining evidence is native interruption of fresh standalone PowerDNS, fresh
paired secondaries, and a post-start cut on fresh BIND. The harness prepares
these proved-empty (`uninitialized`, `absent-by-proof`) shapes:

- Every standalone `pdns-switch` cell: all 17 boundaries for both peer labels
  (34 cells), Debian 13, `driver-specific`. The scenario has an empty 0/0
  source, target epoch 1, no pair identity and one `NATIVE` `s1-kill.test`
  member. The guest checks that exact identity, proves the empty source and
  stops only the coordinators. Production writes V1 phases for this path. At
  `source-stopped` it stops the inactive `pdns.service` as a no-op before
  writing. Rollback phases follow the tagged `target-staged` precursor.
  PowerDNS packages are installed inside the measured operation.
- Standalone `bind` at `target-verified`, both edges and both peer labels
  (4 cells), on the manifest's Arch or Debian placement. `before-write` cuts
  after BIND started and the state receipt was persisted while the journal is
  still `target-started`; `after-write` leaves the journal at `target-verified`.

```sh
CELL=pdns-switch__target-started__after-write__standalone__peer-reachable
python3 "$BOOTSTRAP" prepare-pdns-switch --work-root "$ROOT" --cell-id "$CELL" \
  --node debian13 --identity-file "$HOME/.ssh/id_ed25519" \
  --source-fixture uninitialized --execute
```

Still not preparable with an empty source:

- BIND `source-stopped`, `target-started` and `rolled-back`. Fresh BIND
  writes these phases, but the manifest places them as
  `managed-pdns-required` and the controller refuses any other source.
  Recording a fresh run there needs a separately labelled matrix coordinate.
  That is a design decision, not a harness gap.
- Standalone BIND `committed` and `rolling-back`. The fresh path reaches them,
  but this change does not admit them.
- Every `paired-secondary` cell. It needs a panel-free native primary serving
  the product catalog `catalog-<hex(peer)>.celikpanel.invalid` and its
  members, with AXFR and NOTIFY to the guest. The four native peer helpers
  only act as secondaries/consumers of a production primary on the kill host.
  The trigger also rejects the fresh PowerDNS paired-secondary manifest under
  `pdns-switch`, because it has the legacy reconfiguration shape.
- `pdns-switch` paired-primary beyond the exact intent cell. Fresh
  paired-primary continues on the separate V3 path, and the Agent pauses
  paired-primary PowerDNS switching.

A prepared cell is not a measured result. The 268-runnable denominator is
unchanged, and admission here adds no passed or native-evidence cell.

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

For socket cells, a separate read-only `recovery.pre_retry_probe` is taken
after the ordinary Agent socket and exact request identity are proven, but
before either same-request RPC retry. Its `indeterminate` outcome is a
recorded startup observation, not evidence of successful rollback and not a
failed retry probe. A valid observation is required for a verified cell.
The controller also records an authoritative UDP/TCP query before retry;
only an exact `rolled_back_source_active` outcome plus a successful query
sets `pre_retry_source_serving`. That query is diagnostic and does not
change the final safety verdict. The two post-retry probes still determine
final convergence. This separates
Agent startup behavior from recovery caused by retry. Existing result v1
artifacts without this additive field retain their original meaning.

Both probes must be valid and are compared by fingerprint. Their diagnostic
`recovery_outcome` distinguishes exact target convergence, an exact prior source
that is active after rollback, and indeterminate state. Combining that with the
controller's final DNS query produces `target_converged`,
`rolled_back_source_serving`, `repeated_nonconvergence`, or `changed/race`.
A source rollback classification requires matching state and ownership receipts,
an exact durable failed mutation verdict with no worker or lease, no target
ownership, and a retired switch journal. A source process alone is indeterminate.
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

### Owner inverse after Agent restart

This mode defines and runs "the Agent decides, the owner executes" for exactly
four cells. The two pre-start cells are placed `driver-specific`:

- `bind__intent__after-write__standalone__peer-reachable`
- `bind__target-staged__after-write__standalone__peer-reachable`

The two critical cells are placed `managed-pdns-required`; see
[Critical variant](#critical-variant-source-stopped-before-the-cut):

- `bind__source-stopped__after-write__standalone__peer-reachable`
- `bind__target-started__after-write__standalone__peer-reachable`

Without the flag, every one of these cells keeps its earlier behaviour. The
critical cells in particular keep their stale V1 expectation.

All four require `--source-fixture managed-pdns`: a serving PowerDNS source that
production installed, on Debian 13, with BIND as the target. With that source
the certified APT producer writes the V2 frozen-source journal from `intent` on
(`prepareBINDIndependentInverseJournal` →
`requiresBINDIndependentSourceProof`). A restarted Agent checks the target,
proves the frozen source and writes `rolling-back`. It never runs a V2 inverse
itself. It releases its lease with `dns_native_recovery_unknown_after_restart`
and logs a refusal that names
`/usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id <id>`.
The server owner runs that command.

Select the mode with `--owner-inverse-after-restart`, or `run-prepared ...
--owner-inverse-after-restart` through the bootstrap. The mode requires socket
mode and a source proof. It cannot be combined with the independent-handoff
flags. Any other cell, or any source other than `managed-pdns`, is refused
before the tagged Agent starts. Before launch, the controller also requires:

- a root-owned `/usr/libexec/celikpanel/recovery` that is not group- or
  world-writable;
- output of exactly `celikpanel-bind-source-inverse/v1` from
  `recovery check-bind-source-inverse-v1`;
- PowerDNS as the only authority: `pdns.service` active, `bind9`/`named` not
  active, every TCP and UDP port-53 listener on the DNS address or a wildcard
  owned by the `pdns.service` MainPID (`ss -H -l -n -p -t -u`), and
  authoritative UDP and TCP answers.

The controller records the pre-cut PowerDNS MainPID and the semantic DNS state
receipt. If a prerequisite is missing, it refuses before any mutation.

Sequence and pass definition:

1. **Kill.** The tagged Agent is SIGKILLed at the boundary, with the usual kill
   proof: stopped state, exit 137, reap. The marker and the on-disk journal
   must be V2 at the cell phase, with a frozen `pdns-source/v1` source
   (`kind`, APT layout, plan digest, database path and logical hash,
   `/etc/powerdns/pdns.conf` among `config_before`). The PowerDNS observation
   at the boundary is recorded but not enforced.
2. **Agent decides.** The ordinary Agent is restarted and left running.
   Read-only ledger polling, bounded by `--recovery-timeout`, waits until it
   stops holding the request. Then the controller requires:
   - the same request's job is `failed`/`interrupted` with `error_code`
     `dns_native_recovery_unknown_after_restart`, same owner and qualifier, no
     worker or lease, and no active request;
   - the journal is V2 at `rolling-back`;
   - the `celikpanel-agent.service` journal since the restart names
     `recover-dns-bind-switch --request-id <id>`;
   - pre-start cells: PowerDNS still serves alone (the check above) with the
     pre-cut MainPID. Critical cells record native state instead; see below.

   A read-only probe (ordinal 0) is recorded.
3. **Status.** `recovery dns-switch-status --quiesced --request-id <id>` runs
   as root with a clean environment. Its output must name
   `recover-dns-bind-switch --request-id <id>`. The journal, ledger, state and
   both engines' ownership and install-ownership receipts must be
   byte-identical (path, hash, device, inode, size, mode) before and after.
4. **Owner executes.** Only if steps 2 and 3 held,
   `/usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id <id>`
   runs once as root. Its exit code and output are recorded and never judged:
   no error text is assumed.
5. **After the command:**
   - the journal is retired;
   - the ledger is byte-identical (sha256 and size) to the ledger captured in
     step 2 right after the Agent's release. For a job the Agent released, the
     owner inverse writes the journal's `rolled-back` checkpoint and retires
     the journal. It publishes no ledger verdict:
     `dns_engine_switch_rolled_back_by_owner_recovery` is written only from
     active-lease statuses. The same request therefore still reads
     `failed`/`interrupted` with `dns_native_recovery_unknown_after_restart`,
     with no worker, no lease and an empty active request. Any ledger change
     is a deviation;
   - the DNS state receipt is semantically equal to the pre-cut source, as the
     command restores it (byte equality is recorded);
   - PowerDNS serves alone and BIND is inactive (pre-start: with the pre-cut
     MainPID; critical: see below);
   - the owner PowerDNS files are unchanged. The source-normalization proof is
     re-run: main and managed config hashes and identities, database identity,
     `quick_check`, receipt rows and domain count.

   Probe 1 is recorded. `recovery dns-switch-status --quiesced --request-id
   <id>` then runs once more. Its output and any evidence change are recorded
   but not judged, so the evidence shows what the owner is told after the
   journal is retired.
6. **Re-run.** The identical command runs again. No private evidence file may
   change. Its exit code and output are recorded, not judged by text. Probe 2
   is recorded.
7. **Liveness.** The Panel is restarted. The Agent must keep its socket inode
   and MainPID. The usual post-restart checks and stability window follow: 31
   samples over 30 s with the prepared argv (Agent, Panel, authoritative
   UDP+TCP DNS). A final PowerDNS-alone check follows the window.

A pass needs D-021 safety `passed`, every step above, and
`recovery_outcome.classification: rolled_back_source_serving` from probes 1 and
2. That is the existing classification. No forward retry is attempted for
these cells.

Result classification:

- **`failed`:** a verified deviation, such as a wrong release reason, a journal
  not at `rolling-back`, a status output that does not name the command, a
  status mutation, the journal left in place, a ledger changed after the
  Agent's release, answers not
  from PowerDNS, BIND active, changed owner files, a re-run mutation, or a
  missing Agent refusal. It stays `failed` even if an unknown result also
  occurred.
- **`unverified`:** unknown results, such as no release within the bound, a
  command timeout, unreadable `journalctl`/`ss`/ledger, invalid or changed
  probes, or a changed PowerDNS MainPID. The controller cannot explain a PID
  change. It retains the `pdns.service` journal since the preflight so that
  the reason can be established.

When step 2 or 3 fails, the owner command is not run: an owner following the
product guidance would not run it either. All observations are kept under the
additive result keys `owner_inverse_preflight`,
`owner_inverse_source_at_boundary` (pre-start) or
`owner_inverse_native_at_boundary` (critical), `owner_inverse_after_restart`
(`variant`, `expectation`, steps, failures, ambiguities, status),
`owner_inverse_failures` and, for critical cells, `dns_outage`. The result
schema stays `celikpanel/dns-kill-result/v1`.

#### Critical variant: source stopped before the cut

`source-stopped` and `target-started` cut after the product stopped
`pdns.service`, so **DNS is not continuous in these cells by construction**.
The flow, the preconditions and steps 1, 3, 4 and 6 are unchanged. The
per-cell difference is data (`OwnerInverseExpectation` in `run_cell.py`), not
a second copy of the flow:

- **Boundary:** native DNS state is recorded, not the PowerDNS-serving check.
- **Step 2, recorded:** unit `LoadState`/`ActiveState`/`SubState`/
  `UnitFileState`/`MainPID` for `pdns.service`, `named.service` and
  `bind9.service`; `named` processes; the port-53 listener owners; whether the
  DNS address answers authoritatively over UDP and TCP; and the
  `pdns.service` unit journal since the preflight. The PowerDNS-serving check
  is not required.
- **Step 2, judged:** only two facts.
  - PowerDNS must **not** be active in both cells.
  - After `target-started`, `named.service` or `bind9.service` must be active.

  After `source-stopped` the BIND state is recorded but not judged: it may be
  masked, loaded-inactive or mid-activation. The Agent's decision (release,
  `rolling-back` V2 journal, refusal naming the command) is required exactly
  as in step 2 above.
- **Step 5, added requirements:**
  - PowerDNS must be **active and enabled**;
  - PowerDNS must be the only port-53 authority, with authoritative UDP+TCP
    answers;
  - `named.service` and `bind9.service` must be inactive;
  - no `named` process may remain.

  Their `LoadState`/`UnitFileState` are recorded, not judged. The product
  restores an absent preimage by unmask and disable, so `loaded`/`disabled`
  is expected and `masked` is also acceptable. The ledger, state-receipt and
  owner-file rules are unchanged.
- **PowerDNS MainPID:** it is expected to change, because the inverse starts the
  stopped source. Step 5 records `{pre_cut, after_owner_command, changed}` with
  `judged: false`, and the new PID becomes the reference for the re-run and
  the post-stability check. A PID change *after* the owner command is still an
  ambiguity.
- **`dns_outage`:** recorded with `judged: false`. It contains:
  - `source_stopped_at`: the first systemd stopping job entry (or stopped
    entry) in the `pdns.service` unit journal;
  - `source_serving_again_at`: the first authoritative UDP+TCP answer the
    controller observed after the owner command returned. This is an upper
    bound: the command may have restored service earlier;
  - `seconds`;
  - `measured_from`: states the source of each field, or says explicitly that
    it is unavailable. Nothing is guessed;
  - `last_source_answer_before_cut_at` (the pre-launch observation) and
    `pdns_started_after_stop_at` (unit journal), for reference.

A critical cell passes on the same terms as a pre-start cell: D-021 safety,
every step, and `rolled_back_source_serving`. It fails on these verified
deviations:

- PowerDNS active at step 2;
- BIND not active at step 2 after `target-started`;
- PowerDNS inactive, not enabled or not answering after the command;
- a BIND unit active, BIND still answering on port 53, or a remaining `named`
  process;
- any of the pre-start failures above.

The recovery runtime is fixture work, done as in the
[2026-09-27 owner CLI trial](NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md). Build
the offline kit, then enroll it after `install` and before `prepare-bind`.
`enroll-recovery-runtime` copies the kit to the guest and runs the product's
`recovery enroll-runtime --source <kit> --transaction-fd 9` under the native
release-transaction flock. It then requires the launcher to advertise
`celikpanel-bind-source-inverse/v1`. It is a dry run unless `--execute` is
given.

```sh
# On the Linux QEMU host, from the tested source tree:
GO=/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go
ART=/var/tmp/cp-owner-inverse/artifacts
export GOTOOLCHAIN=local CGO_ENABLED=0
"$GO" build -trimpath -buildvcs=false -o "$ART/agent" ./cmd/agent
"$GO" build -trimpath -buildvcs=false -tags dns_kill_matrix -o "$ART/agent.kill" ./cmd/agent
"$GO" build -trimpath -buildvcs=false -o "$ART/panel" ./cmd/panel
"$GO" build -trimpath -buildvcs=false -o "$ART/dns-kill-trigger" ./cmd/dns-kill-matrix-trigger
"$GO" build -trimpath -buildvcs=false -o "$ART/recovery" ./cmd/recovery
mapfile -t sources < deploy/recovery/agent-checker.sources
"$GO" build -trimpath -buildvcs=false -o "$ART/agent-checker" "${sources[@]}"
mapfile -t sources < deploy/recovery/panel-checker.sources
"$GO" build -trimpath -buildvcs=false -o "$ART/panel-checker" "${sources[@]}"
"$GO" build -trimpath -buildvcs=false -o "$ART/schema17-bridge" ./deploy/schema17bridge
"$GO" run ./deploy/recovery/bundle --source-root . --binary-root "$ART" --output "$ART/recovery-runtime"

CELL=bind__target-staged__after-write__standalone__peer-reachable  # or intent, source-stopped, target-started
COMMON=(--work-root "$ROOT" --cell-id "$CELL" --node debian13 \
        --identity-file "$ROOT/id_ed25519" --source-fixture managed-pdns)
python3 "$BOOTSTRAP" install "${COMMON[@]}" --agent "$ART/agent" \
  --tagged-agent "$ART/agent.kill" --panel "$ART/panel" \
  --trigger "$ART/dns-kill-trigger" --web-dir "$PWD/web/dist" --execute
python3 "$BOOTSTRAP" enroll-recovery-runtime "${COMMON[@]}" \
  --recovery-runtime "$ART/recovery-runtime" --execute
python3 "$BOOTSTRAP" prepare-bind "${COMMON[@]}" --execute
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --owner-inverse-after-restart --execute
```

Limits:

- **Owner command admission.** The pass depends on the Go change that admits
  the Agent's own deliberate release (`released-undecided`) in
  `recover-dns-bind-switch` and leaves that ledger untouched. A build without
  it refuses these journals. The honest result is then `failed` at step 5,
  with the journal and released ledger retained. The ledger alone does not
  distinguish "rolled back by the owner" from "released, undecided". The
  retired journal, native state and probes carry that evidence, and the
  recorded post-command status shows what the owner is told.
- **Owner files mean PowerDNS only.** The unchanged-file check covers the
  PowerDNS configuration and database. At `target-staged` the BIND package and
  any install-ownership receipt left by the product are recorded by the probe
  but not judged here. BIND configuration restoration is the command's own
  contract.
- **One path only.** There is no reboot, owner-edit race, power loss, paired
  topology, Arch placement or before-write edge. The `peer-reachable` label is
  an invariance control.
- **Local provenance.** The recovery kit is an unsigned local build enrolled
  as fixture work. It is not release provenance.
- **Stale critical cells without the flag.** Run without
  `--owner-inverse-after-restart`, the standalone managed-pdns
  `source-stopped` and `target-started` cells still expect the V1 journal,
  while the current producer writes V2 for that source. Such a run ends
  `unverified` at the marker, after a real mutation. Use the flag for them.
  `rolled-back` and every before-write edge have no V2 pass definition yet.
- **Critical-variant limits.**
  - DNS is interrupted from the product's source stop until the owner command
    restarts PowerDNS; the controller measures that window, it does not bound
    it.
  - `source_serving_again_at` is controller-observed, so it is an upper bound.
    `source_stopped_at` depends on the `pdns.service` unit journal. The last
    pre-cut answer is the pre-launch observation, not the moment of the stop.
  - Which BIND unit-file state is restored (`loaded`/`disabled` versus
    `masked`) is recorded, not judged.
  - Neither cell has run natively yet.
- **Not evidence yet.** Nothing here is native evidence. The 268-runnable
  denominator is unchanged.

The [management-absent PowerDNS reboot trial](NATIVE-PDNS-MANAGEMENT-ABSENT-BOOT-20260925.md) repeats the corrected-Agent deleted-child adoption path in a fresh disposable Debian/Arch pair. After same-request convergence, direct authoritative UDP/TCP tests passed before and after one orderly Debian reboot with Panel and Agent units disabled/stopped and their normal executable paths absent. Native pdns.service stayed enabled and active. This adds a bounded P0.5 DNS serving result, not another kill-matrix phase or proof of full panel removal, paired transfer, other workloads, owner edits or independent inverse.

The [PowerDNS owner-edit refusal trial](NATIVE-PDNS-OWNER-EDIT-20260925.md) repeats the intent/after-write cell with a guest-only controller insertion after proven exit 137. The fixed independent status command rejected the changed native config before and after Agent restart; both same-request retries failed closed, the owner edit and journal remained, and native PowerDNS stayed active. The controller's safety pass is not target convergence. This adds no matrix phase coverage and does not prove the independent inverse or concurrent effect-point owner edits.

The [archived native DNS pair boot recheck](NATIVE-DNS-PAIR-ARCHIVED-BOOT-RECHECK-20260925.md) failed before networking in both old September 12 fixture guests when their archived firewall-restore unit failed. It does not exercise a current-image pair or add matrix coverage. Fresh paired removal and management-absent reboot acceptance remain open.

The [managed BIND V3 deletion and reboot trial](NATIVE-BIND-V3-DELETION-PENDING-20260926.md) records a real deletion RPC on a fresh Arch primary and native Debian secondary. Both catalogs advanced to serial 2, the secondary unloaded the member, and both native BIND services survived reboot with the primary Agent/Panel disabled. The accepted mutation remains pending because a parentless remote `REFUSED` cannot prove peer zone absence. The report also records the preceding failed trial that stopped primary BIND during an unverified rollback and the scoped correction that preserves local service availability. P0.4/P0.5 remain open.

The [terminal BIND V3 deletion trial](NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) repeats the fresh pair with an explicit authoritative parent. It proves the exact primary SIGKILL recovery, a succeeded production V3 delete, native secondary zone removal, and BIND continuity after reboot with the primary Panel and Agent disabled. Parentless deletion and the remaining P0.4/P0.5 matrix stay open.

The [later BIND pair fault and reboot trial](NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md) proves exit 137 at `target-staged/after-write`, same-request Agent recovery to a serving BIND primary, native catalog/member transfer to a panel-free Debian secondary, and authoritative UDP/TCP answers after both guests reboot with primary management disabled. This is one additional P0.4/P0.5 cell; independent inverse, later phases and the full workload matrix remain open.