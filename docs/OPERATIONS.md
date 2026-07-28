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

Stopping the agent can also stop the dependent panel. The reviewed product
scripts own service ordering and recovery; do not replace their sequence with
ad-hoc SSH commands.

## 3. Release gates

Freeze one clean, pushed release commit for both servers. Before any deployment
the exact commit must pass:

```bash
go test ./...
go vet ./...
cd web && npm run build
```

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

### 4.3 Snapshot contract

The pre-ledger release requires **snapshot contract v4** so rollback preserves
the private agent state and whether the legacy ledger was absent.
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

## 7. Development checks

- **Builds:** `go test ./...`, `go vet ./...`, and `cd web && npm run build`.
- **Visual verification:** `tools/dev-preview/preview-server.py` serves
  `web/dist` behind a schema-faithful stub. Use `FRESH=1` and
  `FIREWALL=on/off` only for development previews.
- **i18n:** `web/src/i18n/en.ts` is the key source; `tr.ts` must have the exact
  same key set.
- **Design loop:** `.design-sync/NOTES.md` records the design-system workflow
  and the required CSS-entry hash refresh.

## 8. Secrets policy

Secrets never belong in the repository, runbook, release evidence, or command
output shared in chat. The operator retains panel credentials and SSH keys.
Service credentials generated by CelikPanel remain in the product's protected
storage. New engineer access is granted by the operator adding a dedicated SSH
public key; passwords are not shared.
