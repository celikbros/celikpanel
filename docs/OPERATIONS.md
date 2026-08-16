# Operations Runbook

*Last updated: July 28, 2026 · [Türkçe](OPERATIONS.tr.md)*

This document is the operational source of truth for releasing and recovering
CelikPanel. Strategy lives in the [ROADMAP](../ROADMAP.md), architectural
decisions in [DECISIONS](DECISIONS.md), and contribution rules in
[CONVENTIONS](CONVENTIONS.md).

---

## 1. Authority boundary

The panel user owns every live panel change. DNS, nameservers, DNSSEC, SSL,
mail, firewall, services, add-ons, domains, users, databases, and other panel
settings are changed by the user through the CelikPanel UI.

Deployment tooling may install or roll back reviewed, versioned CelikPanel
artifacts, database migrations, and CelikPanel-owned systemd units. It must not
call panel setting APIs, click UI actions, install an operator-selected service,
or rewrite live DNS, SSL, mail, firewall, or service configuration as a side
effect of deployment. SSH is read-only for diagnosis except for the narrowly
scoped product update, one-time bootstrap, and rollback paths documented here.

If a production problem requires a panel change, explain the exact UI action
and wait for the user to perform it. Do not reproduce that action in the
background.

## 2. Stable rollout topology

The rollout targets and their required order are:

| Order | Target | Address | Role |
|---|---|---|---|
| 1 | `boston.celikhost.com` | `2.25.80.4` | First production update and verification |
| 2 | `frankfurt.celikhost.com` | `72.62.38.15` | Second update, only after Boston passes |

This runbook deliberately carries no live component inventory, firewall state,
domain count, certificate state, or assumed installed release. Those facts
become stale and must be collected read-only from the target immediately before
each release. Record the observed commit, schema, OS, unit states, and UTC
timestamp in the release evidence; do not edit this runbook into a live-state
cache.

The stable product layout is:

- binaries: `/opt/celikpanel/bin/{agent,panel}`
- web assets: `/opt/celikpanel/web/`
- panel database: `/var/lib/celikpanel/celikpanel.db`
- units: `celikpanel-agent` and `celikpanel-panel`

Stopping or cleanly restarting the agent no longer stops the panel. The panel
orders itself after and weakly wants the agent, then retries while the agent
returns. Reviewed product scripts still own the stricter freeze, update and
recovery sequence; do not replace that sequence with ad-hoc SSH commands.

## 3. Release gates

Freeze one clean, pushed release commit for both servers. Before any deployment
the exact commit must pass:

```bash
make test vet web
```

The Make targets reject every compiler except the reviewed Go 1.26.5 toolchain,
disable automatic Go toolchain downloads, and run tests and vet in a clean
environment.

After the reviewed commit has been pushed, prepare the server-side checkout with
this exact fast-forward proof. Replace both placeholders; the approved commit
must be its full object hash, not a branch name, tag, or abbreviated hash:

```bash
export CELIKPANEL_APPROVED_COMMIT='<full-reviewed-commit-hash>'
export CELIKPANEL_PREPARED_CHECKOUT='/path/to/root-owned/celikpanel-checkout'
cd "$CELIKPANEL_PREPARED_CHECKOUT"
[[ "$CELIKPANEL_APPROVED_COMMIT" =~ ^[0-9a-f]{40,64}$ ]]
git switch main
git fetch --prune origin main
test "$(git rev-parse origin/main^{commit})" = "$CELIKPANEL_APPROVED_COMMIT"
git merge --ff-only "$CELIKPANEL_APPROVED_COMMIT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
```

This checkout acquisition is an operator release step. The privileged
`bootstrap-update.sh`/`update.sh` path never fetches, merges, or otherwise
advances Git.

The firewall restore path is boot-critical. A release that changes installation,
systemd, firewall persistence, update, bootstrap, or rollback behavior must also
pass disposable real-VM tests on both **Ubuntu 24.04** and current **Arch
Linux**. For each OS, preserve evidence for fresh install, the applicable update
mode, rollback, and an actual reboot covering at least these states:

1. no saved firewall snapshot: the restore unit remains disabled and reboot
   does not enable or apply a policy;
2. a snapshot saved by an explicit panel action: reboot restores that exact
   policy;
3. explicit panel **Turn off**: the snapshot is absent and the restore unit is
   disabled after reboot.

Mocked unit tests are necessary but do not satisfy this boot gate. Missing SSH
authentication, missing VM access, or missing reboot/firewall output is a
deployment blocker, not a reason to infer success. No production deployment may
start until this evidence exists.

## 4. Update modes

### Public prebuilt path (normal users)

For a supported tagged release, normal users do not prepare a Git checkout and
do not build on the server. Download `https://celikpanel.net/get.sh` over HTTPS
and run it as root. With no mode flag it distinguishes a clean server from a
completed CelikPanel installation. Partial or ambiguous layouts still fail
closed, with one deliberately narrow exception: an update interrupted by the
`v0.1.0-alpha.4` panel TLS compatibility snapshot defect. The public no-flag
command selects a recovery update only when the transaction records the exact
alpha.4 target `8bbbac8b628fae4fca0e127e52c1c7835f56f8b8`, the expected
version, token, operation and snapshot metadata, all file type, owner and mode
checks pass, no conflicting phase marker exists, and both CelikPanel services
are stopped.
It verifies that complete fingerprint once before release storage or download
and again after archive verification, immediately before starting the updater.
Any mismatch remains ambiguous and stops. The operator uses no recovery flag,
and this recovery path does not change panel settings or rely on their values.

On update, the bootstrap verifies the external archive checksum, rejects links
and special archive objects, verifies the exact internal `SHA256SUMS` manifest
and commit/tree provenance, publishes a root-only immutable release under
`/var/backups/celikpanel/releases/`, and enters that release's existing
transactional `update.sh --normal` path. No compiler or mutable Git checkout is
used. `--install` and `--update` are diagnostic overrides, not routine choices.

The source-build modes below are retained for release engineering, audited
transitions and recovery. They are not the normal customer update procedure.

### 4.0 Exact Go build-cache prerequisite

Every source-build update requires the sealed private cache at
/opt/celikpanel/.toolchain/go to be exact Go 1.26.5. If an existing, otherwise
trusted installation still has an older Go tree, run the migrator from the
same clean reviewed checkout before choosing the applicable update mode below:

~~~bash
cd "$CELIKPANEL_PREPARED_CHECKOUT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sudo /bin/bash ./deploy/migrate-go-toolchain.sh
~~~

The migrator accepts no paths or version overrides. It verifies the pinned
official archive SHA-256 and the complete staged tree before retiring the old
tree. The old tree is retained for operator review; a publication or final
validation failure restores it. It changes no service, database, DNS record or
panel setting. Do not use this command to repair an untrusted, missing or newer
toolchain tree; investigate that state instead.

### 4.1 Normal update

Use the normal path only when the durable service-operation table and the
private agent mutation state have already been introduced by an earlier
verified release. From the prepared checkout proven in section 3:

```bash
cd "$CELIKPANEL_PREPARED_CHECKOUT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sudo /bin/bash ./bootstrap-update.sh --normal
```

`bootstrap-update.sh` must export the clean reviewed commit, build and verify it,
and publish one immutable, root-owned, mode `0700` release below
`/var/backups/celikpanel/releases/<commit>-<nonce>/`. It then invokes that
release's `update.sh` with the explicit `--normal` mode. Do not invoke an
`update.sh` from the mutable checkout. The staged updater must fail closed if
panel operations, the privileged mutation ledger, the shared lock, or the host
package manager is not idle.

### 4.2 One-time pre-ledger bootstrap

A server whose installed release predates the durable service-operation ledger
cannot use the normal update path. This is a one-time transition, not a general
recovery switch.

Only use this path when the release contains the reviewed
[`bootstrap-update.sh`](../bootstrap-update.sh) and
[`update.sh`](../update.sh) supports `--bootstrap-pre-ledger`:

```bash
cd "$CELIKPANEL_PREPARED_CHECKOUT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sudo /bin/bash ./bootstrap-update.sh --bootstrap-pre-ledger
```

As in normal mode, the bootstrap exports a clean `git archive HEAD`, builds and
validates the staged panel and agent, and publishes an immutable root-only
release. It proves the exact contiguous migration history through schema v20,
proves that the service-operation table and indexes are absent, and accepts only
the missing private ledger/runtime state allowed by that legacy release. It
then invokes that release's `update.sh` with the explicit
`--bootstrap-pre-ledger` mode. It must reject partial service-operation objects,
existing inconsistent state, active package mutations, dirty source, or an
untrusted checkout.

Never create, edit, truncate, or fabricate the mutation ledger by hand, and
never run its migrations manually. During the pre-ledger install flow, the
product's controlled one-shot initializer alone creates the ledger; normal
panel and agent startup cannot substitute for that initializer. After one
successful transition, every later release uses `--normal`.

### 4.3 One-time exact schema-17 bridge

Use `--bootstrap-schema17` only for the last known pre-ledger database shape:
an exact, contiguous migration ledger `1..17`, no object or column partially
introduced by migrations 18 through 22, and no private agent mutation ledger.
The maximum migration number alone is not sufficient proof. Before selecting
this mode, collect read-only live evidence. This query is only an initial screen;
its expected output is `17|1|17|153`:

```bash
sudo /usr/bin/sqlite3 -readonly /var/lib/celikpanel/celikpanel.db \
  'PRAGMA query_only=ON; SELECT count(*), min(version), max(version), sum(version) FROM schema_migrations;'
sudo test ! -e /var/lib/celikpanel-agent-private/service-mutations.json
```

If the query cannot be run, has any other result, or the ledger exists, stop.
Do not infer compatibility and do not run SQL manually. The immutable release's
dedicated `schema17-bridge` performs the authoritative read-only ledger, object,
column, integrity and foreign-key proof before quiesce becomes active. Unknown,
newer, gapped or partial shapes fail closed before database mutation.

From the prepared clean checkout, run:

```bash
cd "$CELIKPANEL_PREPARED_CHECKOUT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sudo /bin/bash ./bootstrap-update.sh --bootstrap-schema17
```

The updater proves exact schema 17 before quiesce, while both coordinators are
frozen, and again after they are stopped. It then creates and durably publishes
a complete v4 exact-17 snapshot. Only after that publication may the dedicated
helper apply the allowlisted migrations 18, 19 and 20. The ordinary pre-ledger
bootstrap then creates the agent ledger, installs the verified release and runs
the remaining migrations offline. After a successful bridge, every later
release uses `--normal`; neither schema bootstrap mode is a repair switch.

If any post-mutation step fails, the updater intentionally leaves the release
transaction active and both coordinators stopped. Do not edit the database,
delete transaction markers, start either service manually, or hand the lock to
another process. Run only the exact trusted rollback command printed by the
failed update. That rollback verifies the complete snapshot manifest and uses
the snapshot-carried `schema17-bridge` to atomically restore exact schema 17.

### 4.4 Snapshot contract

The pre-ledger and exact-schema17 releases require **snapshot contract v4** so
rollback preserves the private agent state, whether the legacy ledger was
absent, and the exact transition-specific checker set.
`update.sh` and `rollback.sh` must both declare support for v4 before this
release is deployable. Do not mix snapshot contract versions, copy snapshot
internals by hand, or use a rollback script from a different release.
The scripts own root trust-chain validation, checksums, unit state, paired
panel/agent/web/database state, retention, and restore ordering.

## 5. Two-server rollout and verification

Deploy **Boston first**. Do not touch Frankfurt until all Boston checks pass:

1. the update or one-time bootstrap exits successfully and prints the absolute
   verified snapshot path;
2. `celikpanel-agent` and `celikpanel-panel` are active;
3. the authenticated `/api/v1/panel/version` response reports the frozen full
   commit for both panel and agent and the expected schema;
4. the served UI asset belongs to that release;
5. panel and agent journals for the release window contain no failed preflight,
   migration, restart, or reconciliation;
6. read-only service-operation and mutation-ledger checks are idle.

Only then repeat the applicable update mode on **Frankfurt** and run the same
checks. If any check fails, stop the rollout and roll back the already-updated
server before attempting the peer.

Use only the root-trusted rollback script and `VERIFIED_SNAPSHOT` value printed
by the update. Substitute the exact printed release directory; never use a
checkout copy or a rollback script from another release:

```bash
sudo /bin/bash /var/backups/celikpanel/releases/<commit>-<nonce>/rollback.sh "$VERIFIED_SNAPSHOT"
```

That release's `rollback.sh` must validate its root trust chain, the supported
snapshot contract, provenance, and every checksum before it stops or overwrites
anything. Repeat all read-only verification after rollback.

## 6. Firewall persistence ownership

Installation may place the CelikPanel firewall restore unit, but deployment must
not create the first saved policy, turn the firewall on, or silently convert an
unsaved runtime policy into a boot policy.

`install.sh` delegates boot-link reconciliation to
`deploy/systemd/enable-firewall-restore-if-saved.sh`. When the snapshot path is
absent and is not a symlink, the helper removes both persistent and runtime
activation links without starting or stopping the unit. A present snapshot must
be a non-empty regular file that is not a symlink; the helper then refreshes the
existing install topology without starting the unit or applying the firewall.
An empty, non-regular, or symlink snapshot, or a disable/reenable failure, aborts
installation instead of claiming safe persistence.

Only the user's explicit **Save for reboot** panel action may create the first
durable snapshot and enable restore after the write succeeds. Background synchronization may refresh an existing
snapshot but never grants initial enablement. Explicit **Turn off** removes the
snapshot and disables restore. GET, rescan, monitoring, update, bootstrap, and
rollback must not reinterpret unit presence as user consent.

## 7. Authoritative DNS engine lifecycle

PowerDNS and BIND are selected from the panel's dedicated authoritative-DNS
card. Do not use generic component start, stop, uninstall or direct systemd
commands to change which daemon owns port 53.

The first engine action is always read-only. It obtains a server-generated
preview of the exact operation type, source and target engines, state revision,
topology, zone and DNSSEC counts, expected interruption, impacts and blockers.
It does not install, start, stop or rewrite anything. A separate **Start this
DNS change** action must present the same one-use preview authority. A blocked,
expired or stale preview cannot start an operation. A live-engine cutover also
requires the explicit interruption checkbox; no typed phrase is used. A
registration-only legacy PowerDNS adoption expects no interruption because it
only proves and records existing state.

Normal PowerDNS↔BIND switching requires **Standalone** topology. One additional
direction is supported: an exact, verified Paired PowerDNS authority may switch
to Paired BIND. The saved NS pair determines the local role: Frankfurt/NS1 is
primary and Boston/NS2 is secondary. The primary publishes catalog-zone v2;
the secondary subscribes in memory and must prove the exact catalog serial and
every catalog member's SOA over UDP and TCP before the cutover commits. The
paired identity is read-only while BIND is active, and reverse paired engine
conversion remains blocked. Any DNSSEC zone, pending zone publication,
unmanaged DNS, a TCP/UDP port-53 conflict, a degraded source or another
server/DNS operation blocks confirmation. Resolve blockers through their explicit panel workflows
and request a fresh preview. Do not edit cluster, DNSSEC or daemon state, or
clear operation rows, by hand to bypass a blocker.

During an allowed install or switch, the complete desired zone set is frozen
against the selected target engine and its next activation epoch. The target
package is installed under a no-start guard when necessary, its complete zone
state is staged and validated, and the current source is stopped only for the
final cutover. Success requires the target to be the sole managed public
authority and every zone to answer the expected SOA over both UDP and TCP. When
there is a source engine, its package remains installed but stopped as the
rollback standby; it is not silently removed.

An older installation whose durable engine identity is unresolved may offer
**Adopt existing installation** only for an existing panel-managed PowerDNS
authority. Adoption is registration-only: it byte- and mode-checks managed
configuration, verifies exact unit state and topology, reads and verifies the
SQLite database and every panel-owned zone, and proves TCP/UDP authority. It
does not install packages, rewrite configuration or DNS data, restart services,
or change DNSSEC. Exact Standalone and Paired PowerDNS installations may be
adopted; another running DNS engine, unowned or divergent data, missing paired
configuration, or any changed evidence fails closed. BIND cannot be adopted.

The panel ledger and the agent's root-owned host journal bind the same operation
identity, manifest and phase. If the response is lost, refresh the DNS engine
state; do not invent a new request identity or repeat the operation through
SSH. Startup recovery first tries to prove the exact target. If that proof
fails, it restores and proves the exact pre-operation files, database,
generation pointer and systemd state. If neither outcome can be proved, DNS
mutations remain locked for explicit recovery instead of guessing or allowing a
second authority to start.

## 8. Development checks

- **Builds:** `make test vet web` (including the exact Go 1.26.5 gate).
- **Release contracts:** `bash deploy/test-bootstrap-update-contract.sh` and
  `bash deploy/test-schema17-bridge-contract.sh`.
- **Visual verification:** `tools/dev-preview/preview-server.py` serves
  `web/dist` behind a schema-faithful stub. Use `FRESH=1` and
  `FIREWALL=on/off` only for development previews.
- **i18n:** `web/src/i18n/en.ts` is the key source; `tr.ts` must have the exact
  same key set.
- **Design loop:** `.design-sync/NOTES.md` records the design-system workflow
  and the required CSS-entry hash refresh.

## 9. Secrets policy

Secrets never belong in the repository, runbook, release evidence, or command
output shared in chat. The operator retains panel credentials and SSH keys.
Service credentials generated by CelikPanel remain in the product's protected
storage. New engineer access is granted by the operator adding a dedicated SSH
public key; passwords are not shared.
