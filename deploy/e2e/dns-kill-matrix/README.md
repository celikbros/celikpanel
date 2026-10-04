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
These numbers are inventory, not passed tests: a runnable cell is one the
current code can reach, and says nothing about whether it has run or passed.
The fresh paired-secondary admission and the row 12/14 fixture variants
(2026-09-29) did not change the manifest; the counts are unchanged.
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
post-start cut; see [Fresh-install cells](#fresh-install-cells-d-026). It also
prepares the admitted fresh paired-secondary BIND cells against a native
primary peer; see [Fresh paired-secondary cells](#fresh-paired-secondary-cells-rows-3-and-5).

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

The Debian paired-primary cells admitted in
[Fresh paired PowerDNS primary cells](#fresh-paired-powerdns-primary-cells-row-6-journal-v3)
(12 peer-reachable V3 boundaries, previously only
`pdns-switch__intent__after-write__paired-primary__peer-reachable`) accept
`prepare-pdns-switch --node debian13 --source-fixture uninitialized`.
That path proves an empty 0/0 source, stages one `MASTER` member, and leaves
the Arch BIND peer native; pass `--source-fixture uninitialized` to
`native_pdns_bind_peer.py prepare`.
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

The stability window takes exactly `duration / interval + 1` samples (31 for
30 s at 1 s), sample N scheduled at `start + (N - 1) * interval`; a late
sample runs at once and is never skipped. Before 2026-09-29 the loop sampled
until a deadline, so slow samples fitted only 30 (batch cell `c2`). The result
records `sample_count` and `elapsed_seconds`.

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

Two sources CelikPanel never owned roll back to "no DNS state receipt", and
the probe classifies that end state as `rolled_back_source_active` (the
controller still proves DNS serving): an owner BIND (`owner-bind`) and, since
2026-09-29, an external PowerDNS whose adoption was rolled back
(`external-pdns-adoption`). For the latter it requires no state, ownership or
install-ownership receipt of either engine, no journal, the job `failed` with
an error code and message and no worker or lease, `pdns.service` the only
active DNS unit, and `/etc/powerdns/pdns.conf`,
`/etc/powerdns/pdns.d/celikpanel.conf` and `/var/lib/powerdns/pdns.sqlite3`
byte-identical (SHA-256) to the sealed `source-external-pdns-preimage.json`
beside the scenario (schema and cell ID checked; the paths are fixed, never
taken from the document). Any deviation stays `indeterminate`. Before this,
two equal indeterminate probes made that end state `repeated_nonconvergence`
(batch cell `c2` on `8f86bdad`). A converged adoption keeps its state receipt
and takes the target path.

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
- The paired-secondary cells outside the admitted set of
  [Fresh paired-secondary cells](#fresh-paired-secondary-cells-rows-3-and-5):
  every `peer-unreachable` twin, BIND `committed`/`rolling-back`, and the
  BIND `managed-pdns-required` phases.
- `pdns-switch` paired-primary outside the 12 admitted V3 cells of
  [Fresh paired PowerDNS primary cells](#fresh-paired-powerdns-primary-cells-row-6-journal-v3)
  (peer-unreachable twins, `pre-intent`, `rolling-back`, `rolled-back`). The
  Agent keeps pausing paired-primary PowerDNS switching until its gate opens;
  the controller detects that before any mutation.

A prepared cell is not a measured result. The 268-runnable denominator is
unchanged, and admission here adds no passed or native-evidence cell.

### Fresh paired-secondary cells (rows 3 and 5)

*Harness integration of [PAIRED-SECONDARY-FIXTURE.md](PAIRED-SECONDARY-FIXTURE.md),
2026-09-29. Offline tests only; no guest was started and no row changes state.*

The guest under test becomes a CelikPanel-managed **secondary** of a
panel-free native **primary** on the other guest (`placement.dns_peer_host`),
prepared by `native_primary_peer.py`. Admitted, `--source-fixture
uninitialized` and `peer-reachable` only:

- **BIND, 7 cells** (either kill host): `bind__pre-intent__paired-secondary__peer-reachable`
  and `bind__{intent,target-staged,target-verified}__{before,after}-write__paired-secondary__peer-reachable`.
- **PowerDNS, 17 cells** (Debian 13 only): `pdns-switch__pre-intent__paired-secondary__peer-reachable`
  and `pdns-switch__<phase>__{before,after}-write__paired-secondary__peer-reachable`
  for `intent`, `target-staged`, `source-stopped`, `target-started`,
  `target-verified`, `committed`, `rolling-back`, `rolled-back`. The
  paired-secondary V1 path is the fresh standalone writer
  `switchToPDNSOnCertifiedProfile` and writes every phase; members are
  retrieved before `target-started` is written.

Refused before any mutation, with the reason: every `peer-unreachable` twin
(no pass definition yet; it must be derived from the paired `Reconcile`
behaviour), BIND `committed` and `rolling-back`, the 12 BIND
`managed-pdns-required` cells (they need a managed PowerDNS secondary source,
row 8), and any source other than `uninitialized`.

Every secondary cell is run against a chosen primary flavour, given as
`--peer-engine {bind,pdns}` to `prepare-bind`/`prepare-pdns-switch` and again
to `run-prepared` (it must equal the prepared engine; the host checks the
recorded `peer-prepared.json`). `--peer-catalog-format {bind,pdns-native}`
selects the catalog PRODUCER the peer serves, again on both commands and
checked against `peer-prepared.json` (evidence written before the flag existed
counts as `bind`):

- `bind` (the BIND peer's only format; selectable for a PowerDNS peer): the
  `binddns.CatalogZoneRecords` format (TTL 60, SHA-224 member labels); a
  PowerDNS peer serves it as an ordinary `MASTER` zone with explicit rows.
- `pdns-native` (default for `--peer-engine pdns`): PowerDNS's own `PRODUCER`
  catalog, seeded with the SQL steps of a CelikPanel PowerDNS primary
  (`cmd/agent/dns_engine_pdns_catalog.go` `reconcilePDNSBINDCatalogWithSeedModeTx`):
  the `MASTER` member and its records, a `PRODUCER` domain row, only its SOA
  and NS rows, then `catalog` set on the member (the seed aborts before
  `COMMIT` unless exactly that member is assigned). No `domainmetadata` row is
  written, as the product writes none. PowerDNS itself emits the version TXT
  and member PTRs (TTL 0, base32hex labels) and maintains the serial. The
  producer row carries the fixture account, not the product's
  `celikpanel-bind-catalog-v1` authority marker (the peer is panel-free; the
  account is not transferred).

Since `3cc2de22` the Agent reads a peer catalog in the BIND format and, only
on a producer-format refusal, in the PowerDNS format on a fresh transfer
(`cmd/agent/dns_peer_catalog.go`); it logs the accepted format once per
operation. The peer probe ports that selection and reports
`catalog_producer` (`bind`/`powerdns`), `agent_catalog_format_name`
(`BIND`/`PowerDNS`) and, for PowerDNS, a read-only view of the `domains` rows
and the catalog's `domainmetadata` kinds; it refuses an observation whose
producer differs from the prepared format. It also reports each member's node
label (`catalog_member_labels`) and, recorded only, the native versions on the
peer (`native_versions`: `pacman -Q bind powerdns` or `dpkg-query` rows, and
the `named -v` / `pdns_server --version` banners). The controller records the
guest's versions under `native_versions.before` (before the tagged Agent) and
`native_versions.after` (after recovery), because the measured operation
installs or upgrades packages. Batch 5 ran PowerDNS 5.1.4 and BIND 9.20.29 on
the Arch peer against PowerDNS 4.9.17 and BIND 9.20.29 on the Debian guest.
Order, all driven by the bootstrap:

1. `prepare-*` runs `native_primary_peer.py prepare --engine <peer>
   --catalog-format <format>` on the peer guest, then one baseline `observe`,
   then prepares the guest: exact
   secondary scenario (topology `paired`, `pair_role` `secondary`,
   `local_ns` `ns2.s1-kill.test`, `peer_ns` `ns1.s1-kill.test`, **zero
   zones**, empty 0/0 source, epoch 1), the empty-source proof, and a read-only
   check that the peer's catalog SOA answers authoritatively over UDP and TCP
   from the guest (`peer-primary-preflight.json`). For PowerDNS the guest also
   proves no PowerDNS database exists (with `pdns.service` inactive), so the
   Agent classifies a fresh install under `pdns-switch`, not
   `pdns-secondary-reconfigure`.
2. `run-prepared` observes the peer again right before the controller
   (`peer-before-kill.json`), runs the controller (and any reboot) with
   `--peer-catalog-format-bind` or `--peer-catalog-format-pdns-native`, then
   runs `native_primary_peer.py observe --require-secondary-transfer`, and
   writes `peer-verdict.json` (with `peer_catalog_format` and the expected
   producer). Host-side evidence is create-new under
   `<cell directory>/paired-secondary-peer/`. The controller refuses a
   paired-secondary cell without one of the two format flags, before any
   mutation.

**Pass definition** (data-driven, on the unchanged socket flow; nothing is
copied from the standalone flow):

- exit 137 proven at the named boundary with the V1 journal, fault driver
  `bind` or `pdns-switch`, `pair_role` `secondary`;
- same-request recovery through the existing path (Agent restart, two
  `rpc-retry`, two probes) ending `target_converged`;
- the guest answers `s1-kill.test` SOA and `www.s1-kill.test` A
  authoritatively over UDP **and** TCP with the **primary's** serial
  (`2026083101`) and address, equal to what the primary itself answers;
- PowerDNS secondary: exactly one `CONSUMER` row for
  `catalog-<hex(peer)>.celikpanel.invalid` with `master` = primary and account
  `celikpanel-peer-catalog-v1`, and `s1-kill.test` as a `SLAVE`/`SECONDARY`
  zone from the primary in that catalog, holding its SOA. The member row's
  `options` must be empty or exactly `{"consumer": {"unique": "<label>."}}`
  (strict JSON, no duplicate or extra keys), where `<label>` is the member's
  node label in the catalog the peer serves: native PowerDNS 4.9.17 writes
  that value on a consumed member (batch 5 cells `c3`/`c4`,
  `{"consumer": {"unique": "b076e924….849a."}}`, the SHA-224 label of
  `s1-kill.test` in the BIND-format catalog). The label comes from the peer
  probe's catalog read right before the controller (`catalog_member_labels`
  in `peer-before-kill.json`); `run-prepared` passes it to the controller as
  `--peer-catalog-member-label=<label>` (the only valued guest-program token,
  56 hex or 32 base32hex characters, PowerDNS paired secondaries only; the
  controller refuses such a cell without it before any mutation). The value
  is recorded (`pdns_secondary_rows.member_options`); anything else is a
  verified failure;
- the Agent's log line "the paired primary at `<peer>` serves catalog
  `<catalog>` in the `<BIND|PowerDNS>` catalog format" (tagged Agent output in
  the transcript, and the restarted Agent's `celikpanel-agent.service`
  journal since the cell started) names the format the peer serves
  (`bind` → `BIND`, `pdns-native` → `PowerDNS`). A line naming the other
  format, or a "peer catalog producer changed during the operation" line, is
  a verified failure; no line at all is unknown;
- D-021 safety and 31 health samples;
- host side: the after-recovery peer observation shows the catalog **and** the
  member transferred to this guest, the peer serves its catalog as the
  prepared producer, and the peer's native config digests, catalog producer,
  serial/members, member SOA and `www` A are unchanged from the
  pre-controller observation. `run-prepared` returns the combined exit: 1 if
  either side found a verified deviation, 2 if either side is unknown, 0 only
  if both passed.

Optional: `--reboot-after-recovery --disable-management-before-reboot`
reboots only the secondary after a passing flow, having first stopped and
disabled `celikpanel-agent`/`celikpanel-panel` (proven inactive and disabled,
else no reboot). After the boot the units must still be inactive and disabled,
one native engine alone must own port 53 with the same answer counts, state
receipt and journal presence, the member must still answer with the
primary's data (and the PowerDNS rows hold), and the second window of 31
samples judges only DNS (Agent and Panel recorded as not applicable). The
units are left disabled on the disposable guest.

```sh
# On the Linux QEMU host, from a tree that contains this harness change.
# Artifacts as in "Guest bootstrap and source provenance". Both guests of the
# cell must pass wait-ssh. The peer guest gets no CelikPanel install.
KEY=$HOME/.ssh/id_ed25519
CELL=bind__target-verified__after-write__paired-secondary__peer-reachable  # kill debian13, peer arch
PEER=pdns          # or bind: one cell directory per run; teardown between runs
FORMAT=pdns-native # PowerDNS peer: pdns-native (default) or bind; BIND peer: bind only
COMMON=(--work-root "$ROOT" --cell-id "$CELL" --node debian13 \
        --identity-file "$KEY" --source-fixture uninitialized)
python3 "$FIXTURE" prepare --work-root "$ROOT" --cell-id "$CELL" --ssh-public-key "$KEY.pub" --execute
python3 "$FIXTURE" start --work-root "$ROOT" --cell-id "$CELL" --execute
python3 "$FIXTURE" wait-ssh --work-root "$ROOT" --cell-id "$CELL" --identity-file "$KEY" --execute
python3 "$BOOTSTRAP" install "${COMMON[@]}" --agent "$ART/agent" \
  --tagged-agent "$ART/agent.kill" --panel "$ART/panel" \
  --trigger "$ART/dns-kill-trigger" --web-dir "$PWD/web/dist" --execute
python3 "$BOOTSTRAP" prepare-bind "${COMMON[@]}" --peer-engine "$PEER" \
  --peer-catalog-format "$FORMAT" --execute
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --peer-engine "$PEER" \
  --peer-catalog-format "$FORMAT" \
  --reboot-after-recovery --disable-management-before-reboot --execute
# PowerDNS secondary: CELL=pdns-switch__target-started__after-write__paired-secondary__peer-reachable
# and prepare-pdns-switch instead of prepare-bind (Debian 13 kill host only).
```

**PowerDNS primary with its native catalog.** Until `3cc2de22` a CelikPanel
secondary read the peer catalog in the BIND format only, so the PowerDNS peer
served the BIND format and a PowerDNS primary publishing its own `PRODUCER`
catalog was not tested. The product now accepts either producer, and
`pdns-native` is the PowerDNS peer's default: "against a PowerDNS primary"
now means PowerDNS serving its native catalog, as a real (and a CelikPanel)
PowerDNS primary does. `bind` remains selectable so both producers can be
run against a PowerDNS primary. Offline only: no native run has exercised
`pdns-native`. Open native questions carried from the product note: the
served serial of a PowerDNS producer (the harness reads it, it does not
predict it), whether PowerDNS writes its own `domainmetadata` for the
producer (recorded), and whether it serves `invalid.` SOA/NS rows unchanged.

### Fresh paired PowerDNS primary cells (row 6, journal V3)

*2026-09-30. Offline tests only (`test_fresh_primary_v3.py`, the trigger's
`pdns_fresh_primary_v3_test.go`). No guest was started, nothing ran with
`--execute`, no row changes state, and the product gate stays closed. Policy:
docs/DNS-ENGINE-ARTIFACT.md "Fresh paired PowerDNS primary: recovery policy and
single gate (2026-09-30)".*

The Debian 13 guest under test becomes a CelikPanel **PowerDNS primary** on a
host with no DNS engine (`--source-fixture uninitialized`), through the public
`Agent.BeginServiceMutation` + `Agent.SwitchDNSEngineV1` RPC and the V3
journal. Its peer is the panel-free native **BIND secondary** on Arch
(`native_pdns_bind_peer.py`, the peer of the 2026-09-28 V3 trials), which
consumes the primary's native `PRODUCER` catalog and loads the member.

Since batch 10, `native_pdns_bind_peer.py prepare` also performs the owner's
documented prerequisite for the deletion inspector before named is first
started: Arch's `bind` package creates no `/etc/rndc.key`, so `rndc -s
127.0.0.1 zonestatus` (and, through it, `dns-peer-enroll`'s inspector) cannot
work on a panel-free Arch peer until one exists (`cmd/bind-peer-inspect/README.md`;
batch 10 hit this after enrollment as `dns_peer_inspection_unknown`, not
`dns_peer_enrollment_required`). When no key, no `/etc/rndc.conf` and no
owner `controls` statement are present it runs `rndc-confgen -a` and sets
`root:named 0640`, mirroring the product's own rule
(`cmd/agent/dns_engine_bind_rndc_key.go`); an existing key is left untouched.
The outcome (`created` or `present`, never the key) is recorded as
`owner_prepared_rndc_key` in the prepare receipt, and `observe` now also
reports `rndc_status_ok` (the exit code of `rndc -s 127.0.0.1 status`,
nothing else) so a later run can see the prerequisite is already met.

**Admitted: 12 cells**, peer-reachable only,
`pdns-switch__<phase>__{before,after}-write__paired-primary__peer-reachable`.
The V3 producer writes its journal through the hooked writer at intent,
target-staged, target-enable-intent, target-started (twice: after the start,
then with the native observation), target-verified and committed
(`cmd/agent/dns_engine_pdns_switch.go` intent write,
`dns_engine_pdns_fresh_v3_linux.go` `continueFreshPDNSPrimaryV3`). Its rollback
and forward-recovery checkpoints use `ReplaceFreshPrimaryJournalV3` and carry
no hook. The manifest is unchanged (still 510 cells, 268 runnable): V3 writes
no `source-stopped` (there is no source), so that coordinate selects
`target-enable-intent`; the hook fires once, at the first matching write.

| Manifest phase | Hook selects | On disk at `before-write` | Variant (before / after) |
|---|---|---|---|
| `intent` | `intent` | no journal | pre-journal / pre-start |
| `target-staged` | `target-staged` | `intent` | pre-start / pre-start |
| `source-stopped` | `target-enable-intent` | `target-staged` | pre-start / pre-start |
| `target-started` | `target-started` (first write) | `target-enable-intent`, PowerDNS started | post-start / post-start |
| `target-verified` | `target-verified` | `target-started` | post-start / post-start |
| `committed` | `committed` | `target-verified` | post-start / post-start |

The controller sends that phase as `CELIKPANEL_DNS_KILL_MATRIX_PHASE`, expects
it in the marker, requires schema `celikpanel-dns-engine-switch-journal/v3`
on disk and records it as `result.boundary.journal_phase`.

Refused before any mutation, with the reason: every `peer-unreachable` twin,
`pre-intent`, `rolling-back`, `rolled-back`, any source other than
`uninitialized`, and the owner-inverse, handoff, startup-rollback,
retry-after-rollback and paired-secondary flags.

**Product prerequisite for native runs (not in this harness's scope).** The
tagged Agent's kill-matrix hook still accepts only the V1 journal (and V2 for
the BIND driver) at the selected boundary:
`cmd/agent/dns_engine_kill_matrix_linux.go:511`
(`dnsKillMatrixRuntime.validateObservation`). With a V3 journal it returns
an error instead of publishing the marker, so the producer fails after a real
mutation and the cell ends `unverified` at the boundary. The 2026-09-28 V3
trials used their own test hook (`dns_engine_pdns_v3_native_trial_linux_test.go`).
The build under test must accept `SwitchJournalSchemaV3` for the
`pdns-switch` driver before any of these cells can reach its cut.

**Gate probe.** The product gate constants are closed. Right after the tagged
Agent's socket is up and before the scenario trigger starts, the controller
runs `dns-kill-trigger rpc-gate-probe --scenario <scenario> --timeout 60s`: an
exact `SwitchDNSEngineV1` call **without a lease**. A closed gate answers with
the Agent's pause reason before its lease check; an open gate reaches the
lease check and is refused there. Neither creates a ledger job or touches the
host. Closed: the result is `status: gate-closed` with the message "gate
closed in this build", the controller exits **4**, `run-prepared` passes 4
through without a peer verdict, and nothing is counted as a failure. Any
other answer stops the cell before the trigger as `unverified`.

The gate probe runs **inside `run-prepared` only**. A prepared guest keeps
`celikpanel-agent.service` stopped by design (`coordinator-stop-proof.json`),
so a standalone `dns-kill-trigger rpc-gate-probe` there has no socket to ask
(batch 8, `c01-pri-intent/gate-probe-prepared-guest.txt`). The standalone
probe now says so instead of a bare connection error: gate `unknown`, detail
"the Agent is not running on this prepared guest; the controller probes the
gate itself after it starts the Agent", on stderr the same sentence, and exit
**69** (distinct from 75, an uncertain RPC failure). It never starts the
Agent. `rpc-host-readiness` (below) answers the same way.

**Idle host before the measured operation** (batch 8 `c08`: the Agent refused
`BeginServiceMutation` with `HOST_MUTATION_BUSY` 0.1 s after it started, the
gate probe had answered `open` 0.1 s earlier, and no cut happened). After an
open gate and before the scenario trigger, the controller polls
`dns-kill-trigger rpc-host-readiness` once per second: one read-only
`Agent.ServiceMutationReadiness` call, the Agent's own advisory copy of the
idle check that `BeginServiceMutation` repeats under its lease (ledger active
request, host mutation lock, package manager). It proceeds after two
consecutive idle answers. For every busy answer it records the Agent's typed
code and reason (`panel_operation_active`, `agent_mutation_active`,
`host_lock_busy`, `package_manager_active`, `state_unverified`), when it was
first and last seen, and a read-only snapshot of what was running: the
ledger's active request and job (kind, target, status, phase, times), the
package-manager processes and the holders of the package-manager locks the
Agent's probe reads (from `/proc/locks`), and the pacman lock file. Bounded by
`--host-idle-timeout` (default 120 s). If the host never becomes idle the cell
ends `unverified` before any mutation, with `result.host_idle_before_trigger`
naming what was running and for how long. It never begins, finishes or
cancels anything; ServiceMutationStatus is not used here because it may
resolve a persisted orphan. Two idle answers narrow, but do not close, the
race with a startup task that begins its own lease right after them; the
trigger now prints and records the Agent's typed `reason` (and
`mutation_hold`) with code and message (`agent response code=... reason=...
error=...`; event fields `agent_error_code`, `agent_reason`,
`mutation_hold`), so a refusal that still happens names its cause.

**Pass definitions** (on the unchanged socket flow; D-021 safety and 31
samples in every forward flow):

- *pre-journal* (`intent:before-write`): proven exit 137 with no journal; the
  restarted Agent finishes the job with
  `agent_restarted_before_dns_engine_switch_commit` and there is nothing to
  undo; then the pre-install checks below.
- *pre-start* (`intent`/`target-staged` after-write, `source-stopped` both
  edges): the ordinary Agent is restarted and rolls the install back **by
  itself**: terminal `dns_engine_switch_rolled_back_after_restart` with no
  lease, journal retired, and the pre-install state: `pdns.service` inactive
  with MainPID 0 and at its frozen standby (`target_units_before` of the cut
  journal: the package guard mask, or loaded/disabled), the database and the
  staged candidate (plus `-wal`/`-shm`/`-journal`) absent, every
  `config_before` file of the cut journal back to its recorded digest (or
  absent), no DNS state receipt, no DNS unit active and no port-53 listener at
  the DNS address; the Agent's journal must not name
  `recover-dns-pdns-fresh-prestart`. A retained journal stops the retries.
- *post-start* (`target-started` and later): forward only. The restarted
  Agent completes the same request (the finalized v2 receipt); a rollback
  verdict or a release is a failed expectation. The V3 state document
  (`celikpanel-dns-engine-state/v3`, native catalog
  `pdns-fresh-paired-primary/debian-4.9/v1`) exists for this request.
  PowerDNS MainPID history is recorded (cut, after recovery); a change is
  `unverified` and the `pdns.service` journal is retained. The daemon's
  catalog re-stamp is recorded: staged serial (1), native serial at the cut
  when the journal had it, the state's serial and the served catalog SOA; the
  served serial must equal the state's and exceed 1.
- then, for every forward variant: two same-request `rpc-retry` and two
  probes ending `target_converged` (the probe now reads the v3 state and
  ownership documents), and the **pair**: the primary answers `s1-kill.test`
  SOA `2026083101` and `www` A `192.0.2.10` over UDP and TCP, and the native
  BIND secondary (`192.0.2.11`) answers the member **and** the catalog SOA
  exactly as the primary does (polled up to the endpoint timeout while it
  transfers); the served catalog serial is judged by the serial rule of the
  check (below: before any zone publication it equals the state receipt's).
  On the
  QEMU host `run-prepared` then runs `native_pdns_bind_peer.py observe`
  (primary catalog AXFR equals the secondary's loaded catalog, the member
  answers authoritatively and identically on both over UDP/TCP) into
  `<cell directory>/fresh-primary-peer/peer-verdict.json` and returns the
  combined exit.

  *How the pair is judged (corrected after batch 8).* The expected RRsets are
  read from the very scenario document the cell publishes
  (`/var/lib/celikpanel-dns-kill-matrix/scenario.json`, zone `s1-kill.test`:
  its SOA serial and its `www.s1-kill.test` A records), never from
  `--dns-address`. `--dns-address` is only where the controller sends the
  primary's queries (the QEMU management address, `10.0.2.15` in batch 8);
  the zone's `www` A is the primary's address on the isolated peer link
  (`192.0.2.10`). Batch 8 compared the answer with `--dns-address` and failed
  seven cells whose answers were correct. Both servers, UDP and TCP, must
  return the scenario's member SOA serial and `www` A; the catalog is judged
  by the serial rule below. An unreadable scenario is `unknown`, never a
  pass. `result.fresh_primary_v3.pair`
  records `expected` (the scenario records and its sha256), `query_targets`
  (the address each server was queried at) and `observations` (per server and
  query: server, port, name, type and, per transport, the flags, RCODE and the
  full answer section with owner, type, TTL and RDATA as text), so expected
  versus observed can be read without the code. The same check runs after a
  reboot, with the rule that fits what happened in the cell.

  *Catalog serial rules (corrected after batch 8r `c04`).* The state receipt
  keeps the serial of the switch; the Agent treats it as a floor
  (`cmd/agent/dns_engine_pdns.go` refuses only `serial < receipt`), and every
  zone publication, plus the daemon's own re-stamp after a membership change,
  raises the served serial. The caller selects the rule explicitly
  (`run_cell.fresh_primary_serial_rule`), never by a tolerance, and
  `result.fresh_primary_v3.pair.serial_rule` (after a reboot
  `reboot_after_recovery.fresh_primary_pair.serial_rule`) records the rule and
  why:

  - **pre-publication** (the pass definition; an after-reboot check without a
    zone lifecycle): the served serial equals the state receipt's on both
    servers, UDP and TCP. A higher serial fails. Why equality is still sound
    here (batch 8r): PowerDNS 4.9.17 re-stamped the producer a second time
    (new `CATALOG-HASH`, serial set to the Unix time of the re-stamp, e.g.
    1790723602 = 23:13:22Z) only at its first periodic check after the
    lifecycle's add changed the member set (`c04` 23:13:22, `c06` 23:26:36,
    about 60 s after the first-start re-stamp). No second re-stamp happened
    without a membership change: `c01` served 1790722508 for 3.5 minutes
    across a reboot of both guests (93 samples), `c05` and `c09` about a
    minute each, `c04` after its reboot for over a minute.
  - **post-publication** (after a zone lifecycle: the after-reboot check of the
    lifecycle-and-reboot flow): (a) served serial >= the receipt's, a lower one
    fails; (b) one serial, identical on UDP and TCP on the primary; (c) the
    secondary serves the same serial within the endpoint wait (if the primary
    itself moved during the wait, the secondary is `unknown`, not failed);
    (d) equal to the producer's SOA serial read read-only from the primary's
    PowerDNS database right after the DNS answer (database read, then the SOA
    again; up to three samples one second apart; a serial that moved across
    every sample is `unknown`); (e) the catalogs served by both servers
    (AXFR from `192.0.2.10`, which the primary's `allow-axfr-ips` and the
    secondary's `allow-transfer` admit) and the producer's member rows in the
    database list exactly the zone set at that point (scenario zones plus
    the re-added child).

  Checks (b)-(e) run under both rules; `catalog.checks.{a_receipt,b_udp_tcp,
  c_secondary,d_database,e_members}` record expected, observed and the verdict
  of each, `database.samples` the readings with their timestamps. The
  database is opened read-only only while PowerDNS holds it in WAL mode (both
  `-wal` and `-shm` present), so no sidecar owned by root is ever created; the
  two readings are as close as one process can take them, not simultaneous.

Optional `--reboot-after-recovery [--disable-management-before-reboot]`
after a passing forward flow reboots **both guests**: the host reboots the
native secondary first, then the primary; the resumed controller repeats the
pair check, then the existing post-reboot checks (management disabled: DNS
alone, 31 samples judging only DNS).

**Zone lifecycle with the reboot** (`--zone-lifecycle --reboot-after-recovery
[--disable-management-before-reboot]`; batch 8 had to refuse the full
combination). Defined order:

1. complete pass verdict of the guest controller; it then suspends on the
   same boot (checkpoint 1, stage `zone-lifecycle-before-reboot`, **exit 5**),
   with management still running and nothing disabled;
2. on the QEMU host: the pair verdict (`peer-verdict.json`) and the zone
   lifecycle below through the Agent's zone RPCs (`zone-lifecycle.json`);
3. `run-prepared` continues the suspended run with
   `--zone-lifecycle-outcome=passed|not-passed` (same boot required). After a
   lifecycle that did not pass the controller writes its result with the
   reboot not run and management left running; the host returns the worst of
   the guest, pair and lifecycle exits;
4. after a passed lifecycle the controller reads the zone set as it now
   stands on both servers (the re-added child `s2.s1-kill.test`: SOA serial
   `2026092803`, `www` A `192.0.2.10`, equal on both, UDP and TCP), then
   records the port-53 authority, **disables management** (when requested)
   and asks for the reboot (checkpoint 2, exit 3);
5. both guests reboot (secondary first); the resumed controller runs the DNS
   checks: the pair as above under the **post-publication** serial rule
   (the lifecycle published zones) **and** the child zone on both servers with
   the serials read before the reboot, then the post-reboot window (DNS alone
   when management was disabled). On the host,
   `fresh-primary-peer/peer-verdict-after-reboot.json` requires both catalogs
   to list the member and the child and `observe-child --step re-add` to
   agree.

Without `--reboot-after-recovery`, `--zone-lifecycle` still runs after the
combined pass as before.

**Owner edit** (`run-prepared --owner-edit {config,sql}`; guest flags
`--owner-edit-config` / `--owner-edit-sql`). Between the proven kill and the
Agent restart the controller edits as the owner would:

- `config`: appends exactly one line, `# owner note: edited by the server
  owner during an interrupted install (dns-kill-matrix <request id>)`, to
  `/etc/powerdns/pdns.conf` in place (mode and owner kept). The path must be
  in the cut journal's `config_before`;
- `sql` (post-start cells only; the database is live only after the start):
  inserts one row into `records` of `/var/lib/powerdns/pdns.sqlite3`:
  `owner-note.s1-kill.test TXT "edited by the server owner (dns-kill-matrix
  <request id>)"`, TTL 300, for the member's domain id.

Pass: the Agent refuses at that boundary and releases only this request
(`dns_native_recovery_unknown_after_restart`); its ledger message is the
typed guidance (not the generic release text, names "Next step" and the
request; the wording is not judged); the journal is kept byte-identical to
the cut; the live database is kept (post-start); the owner's edit is
byte-preserved (file digest, or the exact row); **DNS-only hold**: `dns-kill-
trigger rpc-unrelated-begin` begins an unrelated lease (`service_install` /
`nginx`, derived request id) and finishes it failed with no host effect
(the product's own `manager.begin` unrelated-mutation check, through the
public RPC). No `rpc-retry` runs over the retained journal. Agent and Panel
liveness and 31 samples; DNS is judged only on post-start cells (a held
first install that never started has no DNS engine by design; the samples are
recorded). Pre-journal cells refuse an owner edit (no journal to hold).

**Agent-released owner recovery** (`--owner-release-recovery`, only with
`--owner-edit config` on a pre-start cell): after the release the owner
removes the line again (truncates to the old size, digest checked), reads
`recovery dns-switch-status --quiesced --request-id <id>` (must name
`recover-dns-pdns-fresh-prestart --request-id <id>` and change no private
evidence), runs `recover-dns-pdns-fresh-prestart --request-id <id>` (exit 0),
then: journal retired, the pre-install state above, the ledger byte-identical
to the one after the Agent's release (the released job unchanged), and an
identical re-run exits 0 without changing any private evidence. The recovery
runtime must be enrolled (`enroll-recovery-runtime` now admits these cells).

**Zone lifecycle on the accepted primary** (`run-prepared --zone-lifecycle`,
after a combined pass; or later `guest_bootstrap.py zone-lifecycle`). Four
exact requests through the Panel's zone-sync V3 RPCs
(`BeginServiceMutation dns_zone_sync` / `SyncDNSZoneV3` /
`FinishServiceMutation`), trigger `rpc-pdns-primary-zone-v3 --step
{add,edit,delete,re-add}`, for the child `s2.s1-kill.test` (MASTER, engine
pdns epoch 1, generations 1-4, SOA serials 2026092801/02/03, `changed` A only
at edit), each only after its predecessor published and never begun twice.
After each step `native_pdns_bind_peer.py observe-child` requires, with
retries while the secondary transfers: both catalogs equal and listing the
child exactly when present; identical authoritative SOA serial and `www` A
on both servers over UDP/TCP; `changed` present only after edit; after
delete, an authoritative NXDOMAIN from the served parent on both. Catalog
serials are recorded per step (the daemon re-stamp during publication). If
the Agent keeps the deletion pending (for example
`dns_peer_enrollment_required`), the lifecycle stops with the Agent's code;
the server owner enrolls the native BIND secondary for inspection
(`dns-peer-enroll --engine bind`, as the Agent's message names; not automated
here), then `zone-lifecycle --recover-delete` resumes the same request with
`rpc-pdns-primary-zone-v3-recover` (`RecoverDNSZoneV3`, never a second
delete) and re-adds. With `--reboot-after-recovery` the guest run is not
continued on a pending delete: it stays suspended on the same boot and
`run-prepared ... --resume-held-zone-lifecycle` resumes it (see the zero-zone
section below for both commands). Evidence:
`fresh-primary-peer/zone-lifecycle*.json`.

```sh
# On the Arch Linux QEMU host, from a tree with this harness change and
# binaries built from the commit that opens the gate (and accepts the V3
# journal in the kill-matrix hook). Fresh cell directory per run.
KEY=$HOME/.ssh/id_ed25519
CELL=pdns-switch__source-stopped__after-write__paired-primary__peer-reachable
COMMON=(--work-root "$ROOT" --cell-id "$CELL" --node debian13 \
        --identity-file "$KEY" --source-fixture uninitialized)
PEER=(--work-root "$ROOT" --cell-id "$CELL" --identity-file "$KEY" \
      --source-fixture uninitialized)
python3 "$FIXTURE" prepare --work-root "$ROOT" --cell-id "$CELL" --ssh-public-key "$KEY.pub" --execute
python3 "$FIXTURE" start --work-root "$ROOT" --cell-id "$CELL" --execute
python3 "$FIXTURE" wait-ssh --work-root "$ROOT" --cell-id "$CELL" --identity-file "$KEY" --execute
python3 "$BOOTSTRAP" install "${COMMON[@]}" --agent "$ART/agent" \
  --tagged-agent "$ART/agent.kill" --panel "$ART/panel" \
  --trigger "$ART/dns-kill-trigger" --web-dir "$PWD/web/dist" --execute
python3 deploy/e2e/dns-kill-matrix/native_pdns_bind_peer.py prepare "${PEER[@]}" --execute
python3 "$BOOTSTRAP" prepare-pdns-switch "${COMMON[@]}" --execute
# Forward flow (pre-start/post-start by cell), both guests rebooted after it:
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" \
  --reboot-after-recovery --disable-management-before-reboot --execute
# Instead, on a post-start cell with the zone lifecycle (Agent kept running):
#   run-prepared "${COMMON[@]}" --zone-lifecycle --execute
# Zone lifecycle first, then management disabled and both guests rebooted:
#   run-prepared "${COMMON[@]}" --zone-lifecycle \
#     --reboot-after-recovery --disable-management-before-reboot --execute
# Owner edit (post-start: config or sql; pre-start: config):
#   run-prepared "${COMMON[@]}" --owner-edit sql --execute
# Agent-released owner recovery (pre-start cells; enroll the runtime first):
#   python3 "$BOOTSTRAP" enroll-recovery-runtime "${COMMON[@]}" --recovery-runtime "$ART/recovery-runtime" --execute
#   run-prepared "${COMMON[@]}" --owner-edit config --owner-release-recovery --execute
# After a pending deletion and the owner's enrollment:
#   python3 "$BOOTSTRAP" zone-lifecycle "${COMMON[@]}" --recover-delete --execute
# ... or, when the run had --reboot-after-recovery (guest still suspended):
#   python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zone-lifecycle \
#     --reboot-after-recovery --disable-management-before-reboot \
#     --resume-held-zone-lifecycle --execute
```

Exit codes: 0 passed, 1 verified deviation, 2 unknown, 3 reboot requested
(handled by `run-prepared`), 4 gate closed in this build, 5 zone lifecycle
requested before the reboot (handled by `run-prepared`), 64 refused before
any mutation. The standalone `rpc-gate-probe` / `rpc-host-readiness` exit 69
when no Agent listens on the socket.

#### Zero-zone fresh paired PowerDNS primary (2026-10-01)

*Offline tests only (`test_fresh_primary_serial_zero_zone.py`, the trigger's
`pdns_fresh_primary_v3_test.go`). No zero-zone cell has run natively.*

A fresh server set up through the wizard has **no zones** when it becomes the
paired PowerDNS primary; every V3 primary cell so far carried `s1-kill.test`.
The product fix for the zero-member catalog check (`8a548090`,
`cmd/agent/dns_engine_pdns_catalog.go`) has component tests only. The
variant is selected by one flag, the same on every host command:
`prepare-pdns-switch --zero-zones` writes the scenario with `"zones": []`
(same identity otherwise, so its own manifest qualifier); `run-prepared` and
`zone-lifecycle` take `--zero-zones` too and first read the guest's published
scenario (read-only) and refuse when the flag does not match it. On the
guest, `prepare-pdns-switch` accepts the empty primary scenario and writes the
catalog SOA (`catalog-c000020a.celikpanel.invalid SOA`) as the measured DNS
name and type of the controller argv and the source proof, because the
catalog is the only zone the primary will serve. The controller derives the
zone set from the scenario it publishes and refuses, before any mutation, a
measured name that does not fit it.

What changes in zero-zone mode (nothing changes for the one-member scenario):

- pair check: no member query; the catalog is judged by the serial rules
  above with the expected member set `[]` (after the lifecycle:
  `["s2.s1-kill.test"]`): pre-publication for the pass definition and for a
  reboot without the lifecycle, post-publication after the lifecycle;
- forward (post-start) judgement: a state receipt with the staged serial 1 is
  recorded (`catalog_restamp.zero_zone_restamp`), not failed, because whether
  PowerDNS re-stamps a producer without members at first start is the open
  question; the served serial must still equal the state's. The pair check's
  `database` reading records the `CATALOG-HASH` metadata and serial;
- zone lifecycle: `add` creates the server's **first** zone (the child
  `s2.s1-kill.test`, no served parent), `delete` returns both catalogs to zero
  members, and both servers must then **refuse** the child (`REFUSED`), not
  answer an authoritative NXDOMAIN (no parent zone exists);
  `native_pdns_bind_peer.py observe --zero-zones` requires both catalogs to be
  exactly SOA, NS `invalid.`, `version` TXT "2" and no member PTR, and the
  catalog SOA equal on both servers over UDP and TCP;
- owner SQL edit: there is no member zone, so the row
  `owner-note.catalog-c000020a.celikpanel.invalid TXT "edited by the server
  owner (dns-kill-matrix <request id>)"` goes into the catalog producer zone,
  the only zone the install wrote;
- trigger: `rpc-gate-probe` and `rpc-pdns-primary-zone-v3[-recover]` accepted
  only the one-member scenario and refused the empty one; they now accept
  both (`freshPairedPrimaryScenario`), and the lifecycle's source check admits
  a zero-zone state whose catalog serial is 1 (unmeasured re-stamp); the
  one-member scenario keeps its `> 1` floor. `rpc-switch`/`rpc-retry`
  already accepted zero zones.

Launch (same host prerequisites as the block above; one fresh fixture per
cell; `z02`, `z05` and `z06` share a cell ID, so each needs its own work root):

```sh
KEY=$HOME/.ssh/id_ed25519
zero_cell() {  # $1 work root, $2 cell ID: fixture, both guests, zero-zone scenario
  ROOT=$1 CELL=$2
  COMMON=(--work-root "$ROOT" --cell-id "$CELL" --node debian13 \
          --identity-file "$KEY" --source-fixture uninitialized)
  PEER=(--work-root "$ROOT" --cell-id "$CELL" --identity-file "$KEY" \
        --source-fixture uninitialized)
  python3 "$FIXTURE" prepare --work-root "$ROOT" --cell-id "$CELL" --ssh-public-key "$KEY.pub" --execute
  python3 "$FIXTURE" start --work-root "$ROOT" --cell-id "$CELL" --execute
  python3 "$FIXTURE" wait-ssh --work-root "$ROOT" --cell-id "$CELL" --identity-file "$KEY" --execute
  python3 "$BOOTSTRAP" install "${COMMON[@]}" --agent "$ART/agent" \
    --tagged-agent "$ART/agent.kill" --panel "$ART/panel" \
    --trigger "$ART/dns-kill-trigger" --web-dir "$PWD/web/dist" --execute
  python3 deploy/e2e/dns-kill-matrix/native_pdns_bind_peer.py prepare "${PEER[@]}" --execute
  python3 "$BOOTSTRAP" prepare-pdns-switch "${COMMON[@]}" --zero-zones --execute
}
# z01 pre-start: target-staged after-write (PowerDNS never started)
zero_cell /var/tmp/cp-zero/r1 pdns-switch__target-staged__after-write__paired-primary__peer-reachable
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zero-zones --execute
# z02 post-start: target-started after-write
zero_cell /var/tmp/cp-zero/r1 pdns-switch__target-started__after-write__paired-primary__peer-reachable
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zero-zones --execute
# z03 post-start: committed after-write
zero_cell /var/tmp/cp-zero/r1 pdns-switch__committed__after-write__paired-primary__peer-reachable
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zero-zones --execute
# z04 committed after-write + zone lifecycle (add = the server's first zone)
zero_cell /var/tmp/cp-zero/r2 pdns-switch__committed__after-write__paired-primary__peer-reachable
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zero-zones --zone-lifecycle --execute
# z05 target-started after-write + zone lifecycle, then management disabled and both guests rebooted
zero_cell /var/tmp/cp-zero/r2 pdns-switch__target-started__after-write__paired-primary__peer-reachable
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zero-zones --zone-lifecycle \
  --reboot-after-recovery --disable-management-before-reboot --execute
# z06 owner SQL edit after the start: target-started after-write
zero_cell /var/tmp/cp-zero/r3 pdns-switch__target-started__after-write__paired-primary__peer-reachable
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zero-zones --owner-edit sql --execute
# If the Agent keeps the parentless delete pending (dns_peer_enrollment_required),
# the lifecycle stops with that code; after the owner's enrollment see
# "Pending parentless delete" below for the resume command of z04 and z05.
```

**Pending parentless delete: resume after the owner's enrollment**
(correction after batch 9 `z04`/`z05`; offline tests only:
`test_zone_lifecycle_recover.py` and the trigger's
`TestFreshPrimaryZoneRecovery*`. Not yet run natively.)

In zero-zone mode the lifecycle's delete removes the server's only zone, so
the Agent may keep it pending: job `status: pending`, `error_code:
dns_peer_enrollment_required`, phase
`commit/dns-zone-sync/v3/propagation-pending/<request>/s2.s1-kill.test/dns-zone-sync/v3:sha256:<digest>`.
The lifecycle stops at that step; its `next_step` says who acts and names the
resume command. **Who acts:** the server owner enrolls the native BIND
secondary for inspection with the packaged `dns-peer-enroll` owner tool
(`primary-prepare`, the public key onto the secondary, `secondary-install`,
`secondary-host-key`, `primary-activate`, both status commands `configured`;
batch 9 `z04-zero-committed-zl/rec/enroll.log` has the full sequence). This
harness does not automate the enrollment. **Then, on the host:**

```sh
# z04 (lifecycle without the reboot; the guest run has already finished):
python3 "$BOOTSTRAP" zone-lifecycle "${COMMON[@]}" --zero-zones --recover-delete --execute
# z05 (lifecycle before the reboot): the same run flags plus the resume flag;
# the guest run stayed suspended on this boot and is continued, not restarted:
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zero-zones --zone-lifecycle \
  --reboot-after-recovery --disable-management-before-reboot \
  --resume-held-zone-lifecycle --execute
```

**The printed and held `next_step` now depend on the Agent's own code**
(correction after batch 10, which resumed both `z04` and `z05` past
enrollment and found the same job stayed pending under a second code):
`dns_peer_enrollment_required` still names the enrollment action above;
`dns_peer_inspection_unknown` (an enrolled secondary whose inspector
exchange ran but produced no observation, as in batch 10) instead says to
check the secondary's rndc key and loopback catalog transfer, then resume
with the same command, never enrollment again; any other code is named
verbatim with "the harness has no owner step for" it, rather than repeating
either guidance for the wrong reason. `zone-lifecycle-held.json`'s
`next_step` also carries that real code instead of the empty `pending ():`
it printed before the fix.

**Composite owner-edit codes (D-024).** `dns_peer_owner_edit_unknown` (the
peer proof observed different evidence and could not admit it as an owner
change) can also arrive as the composite `dns_peer_owner_edit_unknown:<check>`
naming which comparison differed
(`operation_attempt`, `engine_state`, `active_engine`, `native_binding`,
`deletion_receipt`, `producer_catalog`, `catalog_probe`, `authority`,
`transfer_observed` or `zone_answered`; see "Owner-edit check tokens" in
`docs/DNS-ENGINE-ARTIFACT.md`). `zone_lifecycle_pending_next_step` and the
step entry it feeds accept both forms: the plain code and a composite with
any of those tokens name the check in `next_step` ("the Agent observed
different evidence in check <check>; compare the catalog and zone serials on
both servers, then resume") and resume the same way as the codes above,
never a second deletion. The step entry (and the held record's `next_step`,
which reuses it) also records the raw code as `job_error_code` and the parsed
check as `job_error_detail` (`null` for the plain code), so the token is
never dropped even when this harness cannot act on it further. A composite
whose token is not in that list is still named in `next_step` and recorded in
`job_error_detail`, but flagged with `job_error_detail_recognised: false` and
a sentence saying this harness does not yet recognise it and to report it
rather than assume it is safe; it is never treated as a pass. The
guest-only `native_bind_owner_reconcile.py` fixture helper (batch 10's pinned
owner-edit-stage2 scenario) applies the same rule before it reconciles the
config it manages: it refuses to proceed on an unrecognised check token
instead of assuming the pinned reconciliation still applies, and records
`job_error_code`/`job_error_detail` in its own printed result.

What the resumed flow verifies, in order (any step that does not pass stops
it; nothing is requested twice):

1. **Exact pending job.** `rpc-pdns-primary-zone-v3-recover --step delete`
   reads the ledger and resumes only the lifecycle's own delete: same request
   ID, owner ID, kind `dns_zone_sync`, target `s2.s1-kill.test`, package
   qualifier, `status: pending`, phase `propagation-pending/<that
   request>/s2.s1-kill.test/<that qualifier>`, attempt > 0, no lease and no
   worker. The zone in the phase is the zone the lifecycle deleted (the
   child), the same phase its delete step accepted as
   `pending_exact_operation`. The trigger at `3cceb29a` compared with the
   parent `s1-kill.test` (the older deletion trials' zone), so it could never
   match and refused with `refused_not_pending`. Any other job (another zone,
   request or qualifier, running, leased, already published) is still refused
   before any mutation; the refused job's status, phase and error code are
   now in the result. Then `BeginServiceMutation` with resume (phase
   `recovering`, lease) and `RecoverDNSZoneV3` for the same request. Never
   `SyncDNSZoneV3` and never a second delete. If the Agent answers pending
   again (`pending_exact_operation`), the same job stays pending.
2. **Delete completed.** Job `succeeded` at
   `published/<request>/s2.s1-kill.test/<qualifier>` (`verified_published`).
   Then `observe-child --step delete --zero-zones`: both catalogs (the
   primary's AXFR and the one the secondary loaded) are equal and have **zero
   members**, and **both servers answer `s2.s1-kill.test` SOA with REFUSED**
   over UDP and TCP. If the child is still listed or answered NXDOMAIN, that
   is a verified deviation and the re-add does not run.
3. **Re-add** (generation 4, begun only after the delete is `published`):
   `verified_published`, then `observe-child --step re-add --zero-zones`: both
   catalogs list exactly `[s2.s1-kill.test]`, and both servers answer child
   SOA 2026092803 and `www.s2.s1-kill.test` A 192.0.2.10 authoritatively over
   UDP and TCP (`changed` absent).
4. **z05 only: the reboot.** The host continues the suspended guest run with
   `--zone-lifecycle-outcome=passed`. The guest reads the child on both
   servers itself, disables the Panel and Agent, both guests reboot
   (secondary first), and the after-reboot checks plus
   `fresh-primary-peer/peer-verdict-after-reboot.json` require the child as
   the only catalog member under the post-publication serial rule.

**Held run (z05).** With `--reboot-after-recovery`, a pending delete no
longer continues the guest with `not-passed`. The guest run stays suspended
at its zone-lifecycle checkpoint on the same boot. The host writes
`fresh-primary-peer/zone-lifecycle-held.json` (cell, `--zero-zones`, pair
verdict 0, `lifecycle_status: pending`, `next_step`) and exits 2. Do not
reboot, restart or stop either guest before the resume: the guest refuses a
continuation on another boot. The resume uses the recorded pair verdict: it is
not taken again because the lifecycle has already moved the catalog serial.
It refuses before any RPC unless that hold exists for this cell with the same
`--zero-zones`. If the resumed delete is pending again, the run stays suspended
and the same command can be repeated after the owner acts. A failed or
unknown resume continues the guest with `not-passed` (no reboot). Each
attempt writes its own `zone-lifecycle-recover.json` (then `-2` … `-9`),
chosen before any RPC, so no attempt replaces another's evidence.

Batch 9 overlays: in `z05` the guest run was already continued with
`not-passed` and has finished, so it cannot be resumed; re-run `z05` on a
fresh fixture. In `z04`, a recover on its kept overlay would write
`zone-lifecycle-recover-2.json` beside the refused attempt.

What each proves if it passes, and what it does not prove:

| Cell | Proves | Does not prove |
| --- | --- | --- |
| `z01` | the restarted Agent rolls a zero-zone install back by itself to the pre-install state (unit at its frozen standby, no database, candidate or receipt, configuration at its preimage), then the same request converges forward and the pair check passes with an empty catalog on both servers | any start-up behaviour of PowerDNS before the cut (it never started) |
| `z02` | forward completion of a zero-zone install cut right after the first start: V3 state for this request, MainPID unchanged, served catalog serial = state's; the forward judgement records whether the daemon re-stamped and wrote `CATALOG-HASH` with zero members (`catalog_restamp.zero_zone_restamp`, `pair.database.database.metadata`); the native BIND secondary loads and serves an empty catalog (SOA, NS, `version`, no PTR) | that the re-stamp behaviour is the same on other PowerDNS builds; a cut between the start and the daemon's hash write |
| `z03` | the same at the last forward phase (`committed`) | anything about pre-start cuts |
| `z04` | the lifecycle through the public zone RPCs when the add creates the server's first zone and the delete returns to zero members; post-publication catalog on both servers after each step; if the delete was held pending, the owner enrollment and the recovered delete (REFUSED on both, zero members) followed by the re-add | the after-reboot state; a parentless delete finished by the owner enrollment unless the Agent kept it pending and the owner ran it |
| `z05` | the post-publication serial rule after a management-disabled reboot of both guests with the re-added child as the only zone; DNS served without the Panel and Agent; with a held delete, that the suspended run resumes on the same boot after the enrollment | power loss (the reboot is orderly); a delete still pending after the resume keeps the run suspended, so the reboot then does not run |
| `z06` | the Agent refuses a zero-zone install whose database holds a row the install did not write (in the catalog producer zone) and holds only DNS | how the Agent treats an owner's own separate zone (not exercised) |

None of these passes a register row; they are exploratory until the zero-zone
product fix and the gate are native-tested on the build under test.

Run the offline guest checks with:

```sh
python3 deploy/e2e/dns-kill-matrix/test_guest_bootstrap.py
python3 deploy/e2e/dns-kill-matrix/test_guest_recovery_probe.py
python3 deploy/e2e/dns-kill-matrix/test_fresh_primary_v3.py
python3 deploy/e2e/dns-kill-matrix/test_fresh_primary_serial_zero_zone.py
python3 deploy/e2e/dns-kill-matrix/test_zone_lifecycle_recover.py
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
Two BIND-only fixtures record row 12 and row 14 provenance:
`unmanaged-bind-stopped` requires the exact fresh standalone BIND install
commitment (mode `switch`, empty source, epochs 0 to 1, no pair identity),
because the takeover is selected by the Agent from host state;
`managed-bind-absent` requires mode `reinstall` with source = target = `bind`,
equal epochs >= 1 and standalone topology. No other driver accepts either.
The identity receipt binds the fixture name, so a retry cannot replay a
takeover as a fresh install.

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

Because `status` is that D-021 verdict, it can read `passed` while both
retries exited 1, both probes were indeterminate and the classification is
`repeated_nonconvergence` (batch cell `c6` on `8f86bdad`, which was then
rebooted on that `status`; its cut itself ran past the boundary through the
kill hook's stop race that `c04d8a2b` addresses in the tagged build). Every
flow therefore also records
`complete_verdict` (`passed`, `reasons`, `required_classification`): safety
passed; `status` passed; rpc-retry flow: both retries ran and exited 0, both
probes valid, not indeterminate, one fingerprint, and the classification the
cell's pass definition needs (`target_converged` for fresh installs, the
fixture pass definitions and the Agent startup rollback;
`target_converged` or `rolled_back_source_serving` otherwise); owner-inverse
flow: its own status passed and `rolled_back_source_serving`. The
after-recovery reboot is gated on this complete verdict (see
[Reboot during recovery](#reboot-during-recovery)).

Recovery status reads: on the socket rpc-retry flow the controller runs the
read-only `/usr/libexec/celikpanel/recovery dns-switch-status --quiesced
--request-id <id>` before recovery (after the kill, Agent still down) and
after recovery (after the second probe), as an owner of an installed server
would. Output and exit code are recorded under `recovery_status_reads`; the
only judgement is "made no mutation" (private evidence identical before and
after; a change fails the cell). Without an enrolled launcher the read is
recorded as unavailable, never as a failure. `guest_bootstrap.py
enroll-recovery-runtime` is admitted for every supported standalone cell,
because an installed server always has the launcher (in batch cells `c3`,
`c6` and `c7` the status command exited 127 for lack of enrollment).

The controller creates three per-cell artifacts without replacement:

- `kill-proof.json` uses `celikpanel/dns-kill-proof/v1` and exists only
  after exact exit-137 proof.
- `result.json` records overall `passed`, `failed`, or `unverified`, the
  independent `safety_status`, `recovery_outcome`, every assertion, both
  recovery attempts, both post-attempt probes, timeouts, and artifact hashes.
- The raw transcript interleaves timestamped controller records with uncropped
  child output (bounded to 1 MiB per synchronous command).

Exit status is 3 when a reboot step suspended the cell (see
[Reboot during recovery](#reboot-during-recovery)); otherwise it is 0 for a
verified safety pass, 1 for a verified safety failure,
2 for an unverified boundary/peer/execution dimension, and 64 for a pre-result
controller/preflight error. Run isolated
controller checks with:

```sh
python3 deploy/e2e/dns-kill-matrix/test_run_cell.py
```

### Owner inverse after Agent restart

This mode defines and runs "the Agent decides, the owner executes". The
admitted cells are data (`OWNER_INVERSE_ADMISSIONS` in `run_cell.py`, mirrored
by `guest_bootstrap.py`), each bound to one product inverse
(`OWNER_INVERSE_PROFILES`):

| Cell | Profile / source fixture | Journal on disk at the cut | Journal after the Agent restart | Variant |
|---|---|---|---|---|
| `bind__intent__after-write__standalone__peer-reachable` | V2 switch, `managed-pdns` | `intent` | `rolling-back` (Agent-written) | pre-start |
| `bind__target-staged__after-write__standalone__peer-reachable` | V2 switch, `managed-pdns` | `target-staged` | `rolling-back` | pre-start |
| `bind__target-staged__before-write__standalone__peer-unreachable` | V2 switch, `managed-pdns` | `intent` | `rolling-back` | pre-start |
| `bind__source-stopped__after-write__standalone__peer-reachable` | V2 switch, `managed-pdns` | `source-stopped` | `rolling-back` | critical |
| `bind__source-stopped__before-write__standalone__peer-reachable` | V2 switch, `managed-pdns` | `target-staged`; PowerDNS already `disable --now` | `rolling-back` | critical |
| `bind__target-started__after-write__standalone__peer-reachable` | V2 switch, `managed-pdns` | `target-started` | `rolling-back` | critical |
| `bind__target-started__before-write__standalone__peer-reachable` | V2 switch, `managed-pdns` | `source-stopped`; BIND already started | `rolling-back` | critical |
| `bind__rolled-back__before-write__standalone__peer-reachable` | V2 switch, `managed-pdns` | `rolling-back`; the in-process inverse already ran | `rolling-back` (kept) | pre-start |
| `bind__rolled-back__after-write__standalone__peer-reachable` | V2 switch, `managed-pdns` | `rolled-back` | `rolled-back` (kept, not retired) | pre-start |
| `bind__rolling-back__after-write__standalone__peer-reachable` | V2 running-BIND adoption, `owner-bind`, with `--bind-rollback-after-target-started` | `rolling-back` | `rolling-back` (kept) | pre-start (no-stop) |

`pdns-adopt__rolled-back__after-write__standalone__peer-reachable` left this
table with `3cc2de22`: the restarted Agent now finishes a V1 adoption journal
at `rolled-back` by itself, so the cell runs with
`--expect-agent-startup-rollback` (see
[Agent startup rollback](#powerdns-adoption-agent-startup-rollback)); both
`run_cell.py` and `guest_bootstrap.py run-prepared` refuse it under
`--owner-inverse-after-restart` with a message naming that flag. The
`pdns-adoption-v1` profile and `recover-dns-pdns-adoption` stay in the code as
the owner path for a host whose Agent re-proof fails; no cell is admitted with
them.

The owner commands are `recover-dns-bind-switch` and
`recover-dns-bind-adoption`. The V2 switch has only Debian placements
(APT), which is why the `target-staged` before-write edge is its
peer-unreachable standalone invariance control. Where each before-write edge
stands (from `cmd/agent/dns_engine_host.go` and `internal/dnsenginerecovery`):
the kill hook fires before the named phase is persisted, so the disk holds its
predecessor, and every effect written before that phase has already happened.
The restarted Agent writes `rolling-back` over any earlier phase
(`Reconcile`: target not verified, source absent proven), never rewrites
`rolling-back` or `rolled-back`, never executes a V2 inverse, releases with
`dns_native_recovery_unknown_after_restart` and names the command.

Excluded edges, with the reason:

- `bind__intent__before-write` (managed PowerDNS): no journal exists at the
  cut. The restarted Agent fails the job with
  `agent_restarted_before_dns_engine_switch_commit`; the same-request
  `rpc-retry` is the recovery path, not an owner command. The controller has no
  V2-aware marker expectation for that path yet, so the cell is refused.
- `target-verified` before-write: the state receipt already names BIND; the
  Agent converges forward. The owner command is not the path.
- `rolling-back:before-write` would also be an owner path (disk
  `target-staged`), but its Debian placement is not prepared with a managed
  PowerDNS source; it is not admitted.
- The peer-unreachable twins of the critical and `rolled-back` cells have the
  same shape; they are not admitted and are refused with guidance.

PowerDNS adoption: the Agent runs the V1 adoption inverse itself at restart
(`rollbackPDNSAdoption`). From `intent` and `rolling-back` after-write it rolls
back, retires the journal and records
`dns_engine_switch_rolled_back_after_restart`; `rpc-retry` then converges the
same request forward. Since `3cc2de22` the same holds from `rolled-back`
after-write: the Agent re-proves the restored source with the rolling-back
checks (`pdnsAdoptionEvidenceRolledBack`), restores nothing, publishes the
verdict and retires the journal. No owner command applies on a healthy host
(see [Agent startup rollback](#powerdns-adoption-agent-startup-rollback)).
Before that change the Agent's restored-source proof demanded `rolling-back`,
kept the `rolled-back` journal, released the job and named
`recover-dns-pdns-adoption`; the batch run of 2026-09-29 on `8f86bdad`
([evidence](evidence/batch4-adoption-reboot-20260929/README.md), cell `c2`)
exercised that OLD behaviour through the owner command.

**Without the flag** the managed-PowerDNS standalone BIND cells are refused
before any mutation. `run_cell.py` (`refuse_unrunnable_v2_cells`, before the
transcript or any other artifact exists; exit 64) and `guest_bootstrap.py
run-prepared` both say that the producer writes V2 and name
`--owner-inverse-after-restart` for the admitted switch cells; other
managed-PowerDNS standalone BIND cells get a refusal that says why no V2 pass
definition exists. Earlier, the critical cells failed at the boundary marker
after a real mutation. Other sources and the independent handoff are
unchanged.

The running-BIND adoption cell keeps its earlier behaviour without the flag.

The V2 switch cells require `--source-fixture managed-pdns`: a serving PowerDNS source that
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
flags, except that the running-BIND adoption cell requires
`--bind-rollback-after-target-started` (and no other cell accepts it). Any
other cell, or any source other than the cell's profile fixture, is refused
before the tagged Agent starts. Before launch, the controller also requires:

- a root-owned `/usr/libexec/celikpanel/recovery` that is not group- or
  world-writable;
- output of exactly `celikpanel-bind-source-inverse/v1` from
  `recovery check-bind-source-inverse-v1` (V2 switch), or
  `celikpanel-bind-adoption-inverse/v1` from
  `recovery check-bind-adoption-inverse-v1` (running-BIND adoption);
- PowerDNS as the only authority: `pdns.service` active, `bind9`/`named` not
  active, every TCP and UDP port-53 listener on the DNS address or a wildcard
  owned by the `pdns.service` MainPID (`ss -H -l -n -p -t -u`), and
  authoritative UDP and TCP answers.

The controller records the pre-cut PowerDNS MainPID and the semantic DNS state
receipt. If a prerequisite is missing, it refuses before any mutation.

Sequence and pass definition (written for the V2 switch; the adoption
variants are below). Wherever step 2 or later says `rolling-back`, read the
"after the Agent restart" column of the table: the two `rolled-back:after-write`
cells keep `rolled-back`. For before-write cells the boundary journal is the
predecessor phase in the same table:

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
     `quick_check`, receipt rows and domain count;
   - **rollback standby** (every V2 switch cell, pre-start and critical, since
     `f7a844f7`; `rollback_end_state`): both BIND unit names that exist
     (`named.service`, `bind9.service`) are under the package guard's
     persistent mask (`LoadState`/`UnitFileState` `masked`, the
     `/etc/systemd/system/<unit>` link root-owned and pointing to
     `/dev/null`, no `/run/systemd/system/<unit>` runtime mask), inactive and
     not enabled, whether or not BIND had started (a name that does not exist
     is skipped; none existing is a failure). The staged generation tree
     `/var/cache/bind/celikpanel/generations/<target_generation>`, read from
     the journal at step 2 before it is retired, is absent; a "Not removed:
     the BIND generation" line is a failure here, because these cells never
     edit it. The command's summary must hold a `Restored:` line, an
     `Intentionally kept as rollback standby:` line and, when the tree still
     existed at step 2, a `Removed: the staged BIND generation <id>` line
     (when an earlier in-process rollback had already removed it, as in the
     `rolled-back` cells, the missing `Removed:` line is recorded with that
     reason). Every summary line is recorded verbatim, including the
     `BIND units:` line, BIND's working-directory line and accepted
     `systemd-resolved` stub-listener lines. Files in `/var/cache/bind` and
     the `bind9*` package rows (`dpkg-query`) are recorded, not judged. A
     journal that froze an existing BIND preimage (not these cells) is
     restored exactly and only recorded.

   Probe 1 is recorded. `recovery dns-switch-status --quiesced --request-id
   <id>` then runs once more. Its output and any evidence change are recorded
   but not judged, so the evidence shows what the owner is told after the
   journal is retired.
6. **Re-run.** The identical command runs again. Since `f7a844f7` a re-run of
   an already reconciled request exits **0** with its text on stdout
   (refusals and unknown results keep 3), so any other exit is a verified
   failure; no private evidence file may change. Its combined output is
   recorded as `stdout`. Probe 2 is recorded.
7. **Liveness.** The Panel is restarted. The Agent must keep its socket inode
   and MainPID. The usual post-restart checks and stability window follow: 31
   samples over 30 s with the prepared argv (Agent, Panel, authoritative
   UDP+TCP DNS). A final PowerDNS-alone check follows the window.

A pass needs D-021 safety `passed`, every step above, and
`recovery_outcome.classification: rolled_back_source_serving` from probes 1 and
2. That is the existing classification. No forward retry is attempted for
these cells unless `--retry-switch-after-rollback` is given (below).

Result classification:

- **`failed`:** a verified deviation, such as a wrong release reason, a journal
  not at `rolling-back`, a status output that does not name the command, a
  status mutation, the journal left in place, a ledger changed after the
  Agent's release, answers not
  from PowerDNS, BIND active, changed owner files, a BIND name not sealed, the
  staged generation left, a missing summary line, a re-run exit other than 0,
  a re-run mutation, or a missing Agent refusal. It stays `failed` even if an
  unknown result also occurred.
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

**`--retry-switch-after-rollback`** (V2 switch cells only, with
`--owner-inverse-after-restart`; also `run-prepared ...
--retry-switch-after-rollback`). After a rollback whose complete verdict passed
(and, with `--reboot-after-recovery`, after a judged, passed post-reboot
window), the same switch is requested again as a **new** request through the
unchanged trigger: `rpc-switch` with the scenario of the cell, the request ID
`sha256("celikpanel/dns-kill-matrix-retry-after-rollback/v1" NUL <id>)[:32]` in
`CELIKPANEL_S1_REQUEST_ID`, and a new identity receipt
`trigger-identity-retry-after-rollback.json` beside the first; the trigger
derives the deterministic owner from cell and request. No Go change was
needed: `rpc-switch` already takes the request from the environment and
creates any new receipt path. It must complete forward (`retry_switch_after_rollback`):
trigger exit 0; the retry receipt exact; the new job `succeeded` with its
finalized phase and no active request; the rolled-back job's record
unchanged; the journal retired; the state receipt naming BIND; BIND serving
authoritatively alone (`observe_bind_source_serving`); `pdns.service`
inactive; no BIND name masked any more (the product lifted the guard mask:
no `masked` load state, no persistent or runtime `/dev/null` link); two
probes with the retry receipt `target_converged`; and 31 samples. This is
the native check of "the forward path accepts the rollback standby". A
rollback that did not pass records `run: false` with the reason.

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

  Their `LoadState`/`UnitFileState` are recorded here and judged by the
  rollback-standby rule of step 5: since `f7a844f7` a target that had started
  is compensated (stop, unmask, disable) and then sealed under the guard's
  persistent mask, so `masked`/`masked` on every existing name is required
  (the 2026-09-29 critical run on `411398d9` still saw `loaded`/`disabled`
  where BIND had started). The ledger, state-receipt and owner-file rules are
  unchanged.
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
- **Scope.** There is no owner-edit race, power loss, paired topology or Arch
  placement. Reboot is covered only by the explicit flags below. The
  `peer-reachable`/`peer-unreachable` labels of these standalone cells are
  invariance controls.
- **Local provenance.** The recovery kit is an unsigned local build enrolled
  as fixture work. It is not release provenance.
- **Managed-PowerDNS cells without the flag** are refused before any mutation
  (see above); they no longer end `unverified` at the marker after a real
  mutation. `intent:before-write`, `target-verified:before-write`,
  `rolling-back:before-write` and the peer-unreachable twins still have no V2
  pass definition.
- **Newly admitted cells** (before-write edges, `rolled-back`, the adoption
  cell) and the reboot steps: see the batch run of 2026-09-29 on `8f86bdad`
  ([evidence](evidence/batch4-adoption-reboot-20260929/README.md)) for what
  ran natively; the rollback-standby judgement, the completed re-run exit 0,
  `--retry-switch-after-rollback`, `--reboot-even-if-failed` and the
  adoption-cell reboots have offline tests only.
- **Critical-variant limits.**
  - DNS is interrupted from the product's source stop until the owner command
    restarts PowerDNS; the controller measures that window, it does not bound
    it.
  - `source_serving_again_at` is controller-observed, so it is an upper bound.
    `source_stopped_at` depends on the `pdns.service` unit journal. The last
    pre-cut answer is the pre-launch observation, not the moment of the stop.
  - The sealed unit state is judged only through `systemctl show` and the two
    mask links; the rollback-standby judgement has offline tests only.
  - The two after-write cells passed natively on 2026-09-29
    ([evidence](evidence/owner-inverse-critical-20260929/README.md)); the
    before-write edges have not run natively.
- **Not evidence yet.** Nothing here is native evidence. The 268-runnable
  denominator is unchanged.

#### Adoption variants: running BIND (and the retained PowerDNS profile)

The adoption profiles run the same seven steps; the per-profile differences
are data (`OwnerInverseProfile`), not a second flow. Only the running-BIND
adoption cell is admitted since `3cc2de22`.

**Running-BIND adoption** (`bind-adoption-v2`, row 11), cell
`bind__rolling-back__after-write__standalone__peer-reachable` with
`--source-fixture owner-bind` and `--bind-rollback-after-target-started`
(the precursor the 2026-09-27 trial used: the owner's `named` was reloaded with
the adoption configuration before the in-process rollback). Pass:

1. Kill as above; the journal is V2 at `rolling-back` with
   `inverse_plan.source_bind` of kind `bind-adoption-source/v1` and no
   `source_pdns`. Preflight requires `celikpanel-bind-adoption-inverse/v1`
   and the owner's BIND as the only authority: `named`/`bind9` active,
   PowerDNS not active, every relevant port-53 listener owned by the
   `named.service` MainPID, authoritative UDP+TCP for `www.owner.test`.
2. The restarted, running Agent released the job with
   `dns_native_recovery_unknown_after_restart` (ledger target `bind`), kept the
   V2 journal at `rolling-back`, and its journal names
   `recover-dns-bind-adoption --request-id <id>`. The owner BIND serves alone
   with the **pre-cut `named` MainPID**.
3. Read-only status names `recover-dns-bind-adoption --request-id <id>` and
   changes no private evidence.
4. The owner runs `recover-dns-bind-adoption --request-id <id>` once.
5. The journal is retired; the ledger is byte-identical to the Agent's
   release; no DNS state receipt exists (the source had none); the owner BIND
   still serves alone with the **same MainPID** (a changed PID is a verified
   failure here, because the product promises reload, never restart); the
   owner files are byte-identical to the source proof (`named.conf`,
   `named.conf.options`, `named.conf.local`, the root-hints leaf and
   `db.owner.test`, compared by SHA-256, plus the `named-checkconf -l`
   inventory; inodes are recorded only because the command restores its two
   configuration preimages by replacement). Whether the adoption-only product
   zone still answers is recorded, not judged.
6. The identical re-run exits 0 and changes no private evidence.
7. Panel restart, Agent socket and MainPID kept, 31 samples, and
   `rolled_back_source_serving` from probes 1 and 2.

Reboots: both `--reboot-before-owner-command` and `--reboot-after-recovery`
are admitted for this cell (with its `owner-bind` fixture and
`--bind-rollback-after-target-started`; the precursor only shapes the tagged
Agent and the boundary marker, both consumed before the kill, so nothing
depends on it across a reboot; the controller refused the combination before
2026-09-29, see batch cell `c1`). After a reboot before the owner command the
owner's BIND must serve `www.owner.test` alone again by itself (it is the
owner's enabled service); its new `named` MainPID is recorded
(`judged: false`) and is the reference for step 5, where the no-restart rule
applies again; the owner files are recorded (`judged: false`, the product's
adoption configuration may still be in place before the command). After a
reboot after recovery the owner's BIND serves alone, the owner files must be
byte-identical to the source proof (judged), and the `named` MainPID change
is recorded, not judged. The same owner-file rule applies to every
owner-inverse profile after the after-recovery reboot.

**PowerDNS adoption** (`pdns-adoption-v1`, row 13): no cell is admitted since
`3cc2de22` (see the next section). The profile, retained for a host whose
Agent re-proof fails, judges: V1 journal (mode `adopt`, empty source,
`state_before.exists=false`) at `rolled-back`; ledger target `pdns`; the
command `recover-dns-pdns-adoption`; the external PowerDNS serving alone with
the pre-cut MainPID; owner files (main and managed configuration and
database) by SHA-256 against the sealed adoption preimage; no DNS state
receipt afterwards; no capability probe.

#### PowerDNS adoption: Agent startup rollback

For `pdns-adopt__intent__after-write__standalone__peer-reachable`,
`pdns-adopt__rolling-back__after-write__standalone__peer-reachable` and, since
`3cc2de22`, `pdns-adopt__rolled-back__after-write__standalone__peer-reachable`,
row 13's running-Agent path needs no owner command.
`--expect-agent-startup-rollback` (also `run-prepared ...
--expect-agent-startup-rollback`) keeps the existing socket recovery path and
judges, after the Agent restart and before any same-request retry:

- the job is `failed`/`interrupted` with
  `dns_engine_switch_rolled_back_after_restart`, target `pdns`, same owner and
  qualifier, no worker, no lease, no active request;
- the switch journal is retired;
- the external PowerDNS serves alone on its pre-cut MainPID (a change is an
  unknown);
- the Agent journal since the restart does not name
  `recover-dns-pdns-adoption`.

If any of these fails, no `rpc-retry` is run: over a retained journal the
Agent refuses the retry and poisons its DNS manager. A pass then also needs
D-021 safety and `target_converged` from the two post-retry probes (the retry
re-runs the adoption forward). Without the flag the cells keep their earlier
behaviour.

The `rolled-back` after-write cut, by the product code at `3cc2de22`: the
tagged worker is killed right after it wrote `rolled-back`, before its RPC
returned, so the ledger job is still the running lease. At boot
`recoverPersistedDNSEngineSwitchLocked` calls `RecoverSwitch`; `Reconcile`
hands the `rolled-back` V1 adoption journal to `rollbackPDNSAdoption`, whose
`pdnsAdoptionRollbackStage` selects `pdnsAdoptionEvidenceRolledBack`: configs
are proved, the state receipt is **not** written, and the restored source is
re-proved with the rolling-back checks (exact V1 journal, no DNS state
receipt, sole PowerDNS process and listeners, SOA answers). The outcome is
rolled back, so `finishPersistedOrphanLocked` publishes `failed`/`interrupted`
`dns_engine_switch_rolled_back_after_restart` ("The interrupted DNS engine
switch was rolled back to the verified previous state."), clears worker and
lease and the active request, and the journal is retired. That is exactly the
expectation above (the same as for the other two cuts), with PowerDNS serving
alone on the pre-cut PID. "Keeps" applies when a terminal verdict was already
recorded for the request before the restart; this cut has none. The
pre-retry probe now classifies that state `rolled_back_source_active` (see the
recovery probe above).

The batch run of 2026-09-29 on `8f86bdad`
([evidence](evidence/batch4-adoption-reboot-20260929/README.md), cell `c2`)
ran this cell on the owner-inverse flow and exercised the OLD behaviour (the
Agent refused at `rolled-back`, the owner command finished it); it is not
evidence for the startup-rollback expectation.

#### Reboot during recovery

The controller runs inside the guest it would reboot. At a reboot point it
reads the guest's boot ID, SMBIOS UUID and cloud-init fixture marker (the
marker must name this cell, or the reboot is refused), writes a create-new
0600 checkpoint (`reboot-checkpoint-N.json` in the explicit `--reboot-dir`,
holding the result so far, the failure lists and the flow state) and exits
**3**. `guest_bootstrap.py run-prepared` then reboots exactly this guest with
`fixture.reboot_guest` and runs the same prepared argv with
`--resume-after-reboot`. The prepared guest program passes `--reboot-dir` as
the directory of the prepared `--result`.

`fixture.py reboot` (and `reboot_guest`) is what the 2026-09-26..28 trials did
by hand: guest `systemctl reboot`, then the fixture's SSH readiness. It
refuses unless the cell's QEMU pidfile is alive and the guest reports the
plan's SMBIOS UUID (uuid5 of cell and node, set by `-uuid`), the plan's fixture
marker and a boot ID. It returns the boot ID before and after. There is no QMP
`system_reset` (power loss is not covered).

The resumed controller refuses (exit 64, nothing consumed) unless the boot ID
changed on the same SMBIOS UUID, exactly one unconsumed checkpoint exists, the
settings are identical (hash of every setting except the resume switch), and
the kill proof and earlier transcripts are byte-identical. It then writes
`reboot-resumed-N.json` create-new, so a checkpoint is consumed once, and
records into `transcript-after-reboot-N.jsonl`. `result.json` is written only
at the end; it lists every reboot with both boot IDs.

`--reboot-before-owner-command` (owner-inverse cells only): after step 2 and
before step 3. After the boot the resumed controller judges: both management
units active, the Agent socket connectable, the Panel port reachable, the
journal still at the step-2 phase for the same request, the ledger
byte-identical to the Agent's release. Pre-start and running-BIND adoption
cells: the source serves alone again; its new MainPID is recorded
(`judged: false`) and becomes the reference for steps 5 to 7. Critical
cells: native state and the `pdns.service` journal are recorded only. Also
recorded: the owner files (`judged: false`), private-evidence changes across
the boot, the Agent refusal since the boot and a recovery probe (ordinal 3). A deviation stops before the owner command, as in step 2.
Steps 3 to 7 follow; the Agent socket and MainPID after the boot are the
step-7 reference.

`--reboot-after-recovery` (any standalone socket flow: the rpc-retry flow of
the fresh-install cells and the owner-inverse cells, including the
running-BIND adoption cell): only when the flow's **complete** verdict passed
(`complete_verdict` / `pre_reboot_verdict`: safety, status, retries, probes
and the classification the cell's pass definition needs; see
[Cell controller](#cell-controller)), after its final health samples. Before
2026-09-29 the gate read only `status`, which is the D-021 verdict, so batch
cell `c6` (both retries exited 1, probes indeterminate,
`repeated_nonconvergence`) was rebooted. The authority before the reboot is
recorded: which engine's unit MainPID owns the port-53 listeners, answer
counts, the semantic DNS state receipt, private evidence. After the boot:
both management units came up, Agent socket and Panel port; the same port-53
authority, the same answer counts, the same semantic state receipt and the
same journal presence; owner-inverse cells additionally have the profile's
source serving alone (its new MainPID recorded, `judged: false`), the owner
files byte-identical and, for the V2 switch, BIND not serving (no `named`
process). Then a second window of 31 samples, the Agent MainPID kept through
it, and the authority compared again. Verified deviations fail the cell,
unknown inspections leave it unverified. A flow whose complete verdict did
not pass records `reboot_after_recovery.run: false` with the reasons and is
not rebooted.

`--reboot-even-if-failed` (with `--reboot-after-recovery`; also on
`run-prepared`): reboot anyway after a flow whose complete verdict did not
pass, as a diagnostic. The state carries `diagnostic_reboot: true`; the
resumed controller runs every post-reboot observation into its own lists and
records them under `reboot_after_recovery` with `judged: false`
(`observed_status`, `observed_safety_failures`,
`observed_verification_failures`); the pre-reboot `status`, `safety_status`
and failure lists are kept unchanged. It never turns a failure into a pass
and never adds a verdict.

Both flags may be combined (two reboots). Paired cells are refused (only the
kill guest would be rebooted), except the admitted fresh paired-secondary
cells with `--reboot-after-recovery`: rebooting only the secondary while its
native primary keeps serving is exactly what they check. With
`--bind-rollback-after-target-started` the reboot flags are admitted only for
the running-BIND adoption owner-inverse cell; the independent handoff still
excludes them.

`--disable-management-before-reboot` (with `--reboot-after-recovery`, rpc-retry
flow only) stops and disables the Panel and Agent units before the reboot and
proves them inactive and disabled; if it cannot, the guest is not rebooted and
the cell is unverified. After the boot the resumed controller requires the
units still inactive and disabled, one native engine alone on port 53 with the
same answer counts, state receipt and journal presence, and runs the 31-sample
window on DNS only. The Agent's runtime directory is legitimately absent then;
only this resume skips that path check.

```sh
# On the Linux QEMU host, after install/enroll/prepare as above:
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --owner-inverse-after-restart \
  --reboot-before-owner-command --reboot-after-recovery --reboot-timeout 600 --execute
```

#### Rows 12 and 14: stopped BIND takeover and BIND reinstall

*2026-09-29. Offline tests only; nothing here is native evidence and no row
changes state.* Both run as **fixture variants of one existing Debian cell**,
`bind__target-staged__after-write__standalone__peer-reachable`; the manifest
is unchanged. A result carries its `source_fixture` in the scenario, source
proof, trigger identity receipt and `result.json`, so a takeover or reinstall
run is never confused with a fresh-install run of the same cell ID.

Why `target-staged:after-write` for both: it is the pre-start cut where the
distinctive effect has just happened and BIND has never started. The takeover
has written its adopted install receipt (before intent) and rewritten
`named.conf.options`/`named.conf.local` (before `target-staged` is written);
the reinstall has reinstalled `bind9` under the unit guard. A
`target-started` cut would add the start but, by code reading, the restarted
Agent still decides a rollback there for both paths (the state receipt is
persisted only just before `target-verified`), so it tests the same recovery
decision with less of the preimage intact.

**Takeover (row 12), `--source-fixture unmanaged-bind-stopped`.** Preparation
(`prepare-bind`): prove the empty source, install `bind9` under the package
guard mask, unmask, then `systemctl disable named.service` as the owner would
(named loaded/inactive/disabled, `bind9.service` alias not-found). Nothing of
CelikPanel's exists: no state, ownership, install receipt or journal. The
canonical `source-preinstall-bind.json` records the package version, unit
states, receipt absence, bindable port 53 and a SHA-256/size/mode/owner
inventory of every regular file in `/etc/bind` plus `/etc/default/named`.
The RPC is byte-identical to a fresh install; the Agent selects the takeover
(`stoppedBINDTakeoverSelected`, cmd/agent/dns_engine_bind_adopt.go:224-242).

Pass definition:

- proven exit 137 with the V1 journal at `target-staged`;
- at the cut, read only: `dns-engine-install-ownership-bind.json` names
  `bind9`, this request, `adopted_present: true` and an empty
  `missing_before` (written before intent by
  `assumeExistingDNSEnginePackageOwnership`). It is checked at the cut because
  FinalizeSwitch retires the receipt after convergence;
- same-request recovery (Agent restart, two `rpc-retry`) ending
  `target_converged`; BIND alone owns port 53; D-021 safety and 31 samples;
- owner files: `named.conf.options` and `named.conf.local` are rewritten by
  the documented takeover (managed options block after removing the owner's
  `recursion`/`allow-*`, and the zone include; owner-aware preimage in the
  journal). Their before/after digests are recorded, not judged. Every other
  inventoried file (`named.conf`, `named.conf.default-zones`, `rndc.key`,
  `bind.keys`, `db.*`, `zones.rfc1918`, `/etc/default/named`) must be
  byte-identical.

Owner directives (2026-09-30, `prepare-bind --owner-directives`, then
`run-prepared --owner-directives`). The retry-after-rollback directive case:
the product now keeps the takeover decision for a same-request retry whose
only receipt is this request's own `adopted_present` install receipt
(`sameRequestAdoptedPresentBINDInstall`, cmd/agent/dns_engine_bind_adopt.go);
before that the retry chose the exclusive authority and refused owner
directives. Preparation, as the owner: after the stopped BIND is in place and
before `source-preinstall-bind.json` is sealed, the guest inserts exactly two
tab-indented lines, `recursion no;` and `allow-transfer { none; };`, right
after the single `options {` line of `/etc/bind/named.conf.options`, and
`named-checkconf` must accept it (refused if the file already carries either
directive or has no single `options {` line). The preparation text in the
proof names the edit. The controller (`--expect-owner-directives`) proves the
block is in the file and that the file equals the sealed preimage before the
tagged Agent starts; at the cut it records the rewritten options (reaching the
cut is the first attempt's adoption: the exclusive authority refuses these
directives before the journal); after the Agent's restart and **before** the
retry every owner file of the preimage, `named.conf.options` and
`named.conf.local` included, must be byte-identical (the rollback restored
them); the same-request retry must then converge (`target_converged`), which
only the takeover decision allows. The converged options file is recorded,
not judged. Without the flag the cell is unchanged.

**Reinstall (row 14), `--source-fixture managed-bind-absent`.** Preparation:
a real untagged production fresh BIND switch (same zone as the measured
scenario, recorded in `source-setup-bind.json` and its identity receipt),
then the operator removes the engine: `systemctl disable --now named.service`
and `apt-get purge bind9`. `apt-get remove` is not used: it leaves `deinstall
ok config-files`, which the Agent's exact package proof refuses. The state
and ownership receipts must be byte-identical before and after the removal,
no install receipt may appear, both BIND units must be `not-found`, and port
53 bindable. The measured request is the Panel's `reinstall_active` manifest:
mode `reinstall`, source = target = `bind`, epochs 1 to 1, the same zone.
The controller binds the setup operation to the retained state receipt
through the shared strict decoder (`decode_dns_document`), which reads the
product's v2 receipt (`acquisition`/`publication`, canonical bytes) and the
canonical legacy v1 one. Batch 5 cell `c6` read the v2 receipt as a flat v1
object and stopped with `KeyError: 'manifest_qualifier'` before any measured
mutation; the reinstall path has therefore not run natively yet. The offline
test uses that cell's real receipt, setup scenario, identity and source proof.

Pass definition: proven exit 137 with the V1 journal (mode `reinstall`) at
`target-staged`; at the cut the install receipt names `bind9` in
`missing_before` (the package was purged) and is not an adoption;
same-request recovery ending `target_converged` (the probe maps the reinstall
to the managed tenure: the state receipt says mode `switch`, same epoch);
BIND alone owns port 53; D-021 safety and 31 samples.

Expected risk (from code reading, unverified): a reinstall rollback verifies
the restored source with `verifyOnlyBINDActive`, but the frozen source is
BIND not running, so the restarted Agent's rollback cannot verify, keeps the
journal and releases the job; the retry is then refused over the retained
journal. If that holds natively, this cell ends `failed` or `unverified` and
is a product finding, not a harness defect.

Batch 6b follow-up (2026-09-30): the preparation's `apt-get purge bind9` is
the owner's action and leaves a stale dpkg statoverride that blocked dpkg in
the measured reinstall; the product is being changed to repair its own
override. The preparation is deliberately unchanged (the override is not
pre-cleaned). The controller records `dpkg-statoverride --list` before the
tagged Agent (`result.dpkg_statoverride.before`) and after recovery
(`.after`), output and exit code only, never judged.

Both cells accept `--reboot-after-recovery` (and
`--disable-management-before-reboot`) after a passing flow.

```sh
CELL=bind__target-staged__after-write__standalone__peer-reachable
FIX=unmanaged-bind-stopped   # row 12; managed-bind-absent for row 14 (fresh cell directory each)
COMMON=(--work-root "$ROOT" --cell-id "$CELL" --node debian13 \
        --identity-file "$KEY" --source-fixture "$FIX")
python3 "$BOOTSTRAP" install "${COMMON[@]}" --agent "$ART/agent" \
  --tagged-agent "$ART/agent.kill" --panel "$ART/panel" \
  --trigger "$ART/dns-kill-trigger" --web-dir "$PWD/web/dist" --execute
python3 "$BOOTSTRAP" prepare-bind "${COMMON[@]}" --execute
python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --execute
# Row 12 with the owner's directives (fresh cell directory):
#   prepare-bind "${COMMON[@]}" --owner-directives --execute
#   run-prepared "${COMMON[@]}" --owner-directives --execute
```

The [management-absent PowerDNS reboot trial](NATIVE-PDNS-MANAGEMENT-ABSENT-BOOT-20260925.md) repeats the corrected-Agent deleted-child adoption path in a fresh disposable Debian/Arch pair. After same-request convergence, direct authoritative UDP/TCP tests passed before and after one orderly Debian reboot with Panel and Agent units disabled/stopped and their normal executable paths absent. Native pdns.service stayed enabled and active. This adds a bounded P0.5 DNS serving result, not another kill-matrix phase or proof of full panel removal, paired transfer, other workloads, owner edits or independent inverse.

The [PowerDNS owner-edit refusal trial](NATIVE-PDNS-OWNER-EDIT-20260925.md) repeats the intent/after-write cell with a guest-only controller insertion after proven exit 137. The fixed independent status command rejected the changed native config before and after Agent restart; both same-request retries failed closed, the owner edit and journal remained, and native PowerDNS stayed active. The controller's safety pass is not target convergence. This adds no matrix phase coverage and does not prove the independent inverse or concurrent effect-point owner edits.

The [archived native DNS pair boot recheck](NATIVE-DNS-PAIR-ARCHIVED-BOOT-RECHECK-20260925.md) failed before networking in both old September 12 fixture guests when their archived firewall-restore unit failed. It does not exercise a current-image pair or add matrix coverage. Fresh paired removal and management-absent reboot acceptance remain open.

The [managed BIND V3 deletion and reboot trial](NATIVE-BIND-V3-DELETION-PENDING-20260926.md) records a real deletion RPC on a fresh Arch primary and native Debian secondary. Both catalogs advanced to serial 2, the secondary unloaded the member, and both native BIND services survived reboot with the primary Agent/Panel disabled. The accepted mutation remains pending because a parentless remote `REFUSED` cannot prove peer zone absence. The report also records the preceding failed trial that stopped primary BIND during an unverified rollback and the scoped correction that preserves local service availability. P0.4/P0.5 remain open.

The [terminal BIND V3 deletion trial](NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) repeats the fresh pair with an explicit authoritative parent. It proves the exact primary SIGKILL recovery, a succeeded production V3 delete, native secondary zone removal, and BIND continuity after reboot with the primary Panel and Agent disabled. Parentless deletion and the remaining P0.4/P0.5 matrix stay open.

The [later BIND pair fault and reboot trial](NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md) proves exit 137 at `target-staged/after-write`, same-request Agent recovery to a serving BIND primary, native catalog/member transfer to a panel-free Debian secondary, and authoritative UDP/TCP answers after both guests reboot with primary management disabled. This is one additional P0.4/P0.5 cell; independent inverse, later phases and the full workload matrix remain open.