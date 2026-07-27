# Operations Runbook

*Last updated: July 11, 2026 · [Türkçe](OPERATIONS.tr.md)*

This document carries EVERY piece of operational knowledge an engineer joining
from zero needs to understand production, deploy safely, and roll back what
breaks. Strategy lives in the [ROADMAP](../ROADMAP.md), architectural decisions
in [DECISIONS](DECISIONS.md); this is purely "how it is operated".

---

## 1. The production server

| Field | Value |
|---|---|
| Domain / IP | `celikpanel.cloud` → `2.25.80.4` (Hostinger KVM2, 2 vCPU / 8 GB) |
| OS | Debian 13 (trixie), hostname `boston.celikhost.com` (Jul 16: machine identities live under celikhost.com, named by location — celikpanel.cloud is suspended at the registrar) |
| Access | `ssh root@2.25.80.4` — **key only** (no password logins; authorized keys held by the operator) |
| Panel | `https://2.25.80.4:2083` (self-signed until LE; while the domain does not resolve, test with `curl --resolve`) |
| External blocker (Jul 2026) | Domain suspended at the registrar (Hostinger) — the operator is handling it |

**Server layout:** binaries at `/opt/celikpanel/bin/{agent,panel}` (⚠️ NOT `/usr/local/bin`),
static UI at `/opt/celikpanel/web/`, systemd units `celikpanel-agent` (root) + `celikpanel-panel`
(low-privilege). Data paths and first install have one source of truth: `install.sh` (read it — it documents itself).

**⚠️ The number-one gotcha:** stopping/restarting `celikpanel-agent` also drops the panel
(unit dependency). Every deploy that touches the agent ENDS with `systemctl start celikpanel-panel`.

## 1b. Second test server (Arch — portability guard)

| Field | Value |
|---|---|
| Domain / IP | `frankfurt.celikhost.com` → `72.62.38.15` (Hostinger KVM8, 8 vCPU / 32 GB / 400 GB) |
| OS | Arch Linux (deliberate — see the D-004 amendment: dev-test target) |
| Panel | `https://72.62.38.15:2083` (Jul 16: every server and the default now share one port — 2083) |
| Access | `ssh root@72.62.38.15` — key only |
| Role | Every change is tested on BOTH servers (Jul 16 operator decision). Expected difference on Arch: the service catalog says "not automatable" (apt-specific) — that is honesty, not a bug |

## 2. Deploy recipes

There are two release paths. Everything builds on the dev machine; only product artifacts
are copied (the server has NO Go/Node — deliberately; see D-008: no hand-installs on the server).
Copying and atomically installing those versioned artifacts over SSH is an allowed product
deployment. It does not authorize changing live panel settings, DNS, SSL, mail, firewall, or
service configuration over SSH; those changes remain UI-only.

**A) Frontend only** (only `web/src` changed; no Go, `internal`, or migration change — no
restart needed):
```bash
cd web && npm run build && cd ..
tar -C web/dist -czf /tmp/webdist.tar.gz .
scp /tmp/webdist.tar.gz root@2.25.80.4:/tmp/
ssh root@2.25.80.4 'mkdir -p /opt/celikpanel/web.new && tar -xzf /tmp/webdist.tar.gz -C /opt/celikpanel/web.new --no-same-owner && mv /opt/celikpanel/web /opt/celikpanel/web.old && mv /opt/celikpanel/web.new /opt/celikpanel/web && rm -rf /opt/celikpanel/web.old /tmp/webdist.tar.gz && echo DONE'
```
Backed swap: on trouble, rename `web.old` back. `index.html` is served no-cache;
a normal browser refresh suffices.

**B) Snapshot-backed paired backend release** (any `cmd/panel`, `cmd/agent`, `internal`,
migration, or other backend change):

Panel and agent are one fail-closed release pair. Never build, install, or roll back either
binary independently. Build both from the same clean, merged Git commit and embed the exact
40-character commit SHA in both binaries. A backend release also carries the web build from
that commit.

```bash
test -z "$(git status --porcelain)"
RELEASE_COMMIT="$(git rev-parse --verify HEAD)"
test "$(printf %s "$RELEASE_COMMIT" | wc -c)" -eq 40
RELEASE_VERSION="$(git describe --tags --always)"
RELEASE_DIR="/tmp/celikpanel-release-${RELEASE_COMMIT}"
mkdir -p "$RELEASE_DIR"
LDFLAGS="-s -w -X main.buildVersion=${RELEASE_VERSION} -X main.buildCommit=${RELEASE_COMMIT}"

go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$RELEASE_DIR/agent" ./cmd/agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$RELEASE_DIR/panel" ./cmd/panel
(cd web && npm run build)
tar -C web/dist -czf "$RELEASE_DIR/web.tar.gz" .
(cd "$RELEASE_DIR" && sha256sum agent panel web.tar.gz > SHA256SUMS)
```

Deploy Boston completely and verify it before starting Frankfurt. Frankfurt receives the
exact same artifact bytes. If Frankfurt cannot be completed, fix it immediately or restore
Boston from its snapshot; do not leave the DNS pair on different releases.

For each server, create a unique remote release directory, upload every artifact, and verify
the manifest before stopping anything:

```bash
SERVER=root@2.25.80.4                  # then root@72.62.38.15
PANEL_HOST=boston.celikhost.com        # then frankfurt.celikhost.com
REMOTE_RELEASE="/opt/celikpanel/releases/${RELEASE_COMMIT}"
ssh "$SERVER" "install -d -m 0755 '$REMOTE_RELEASE'"
scp "$RELEASE_DIR/agent" "$RELEASE_DIR/panel" "$RELEASE_DIR/web.tar.gz" \
    "$RELEASE_DIR/SHA256SUMS" "$SERVER:$REMOTE_RELEASE/"
ssh "$SERVER" "cd '$REMOTE_RELEASE' && sha256sum -c SHA256SUMS"
```
Before a schema-changing release, save the current version JSON with an authenticated admin
session, then stop the panel and take a mandatory snapshot. `ADMIN_COOKIE_JAR` must come from
the normal panel login flow (including TOTP when enabled); keep it outside the repository and
mode `0600`. `PANEL_HOST` must be that login hostname, not its IP address, so the browser's
domain-bound session cookie is sent.

```bash
curl -fsSk -b "$ADMIN_COOKIE_JAR" \
    "https://${PANEL_HOST}:2083/api/v1/panel/version" > "$RELEASE_DIR/${PANEL_HOST}.version-before.json"

DEPLOY_ID="$(date -u +%Y%m%dT%H%M%SZ)-${RELEASE_COMMIT}"
SNAPSHOT="/var/backups/celikpanel/releases/${DEPLOY_ID}"
ssh "$SERVER" "SNAPSHOT='$SNAPSHOT' bash -se" <<'REMOTE'
set -euo pipefail
systemctl stop celikpanel-panel
install -d -m 0700 "$SNAPSHOT"
cp -a /opt/celikpanel/bin/agent /opt/celikpanel/bin/panel "$SNAPSHOT/"
cp -a /opt/celikpanel/web "$SNAPSHOT/web"
cp -a /var/lib/celikpanel/celikpanel.db "$SNAPSHOT/"
for sidecar in -wal -shm; do
    source="/var/lib/celikpanel/celikpanel.db${sidecar}"
    if [ -f "$source" ]; then cp -a "$source" "$SNAPSHOT/"; fi
done
cp -a /etc/systemd/system/celikpanel-agent.service \
      /etc/systemd/system/celikpanel-panel.service "$SNAPSHOT/"
sha256sum "$SNAPSHOT/agent" "$SNAPSHOT/panel" > "$SNAPSHOT/BINARY_SHA256SUMS"
systemctl cat celikpanel-agent celikpanel-panel > "$SNAPSHOT/units.txt"
REMOTE
scp "$RELEASE_DIR/${PANEL_HOST}.version-before.json" "$SERVER:$SNAPSHOT/version-before.json"
```

Every unguarded copy above is required: a missing database, binary, web tree, or unit file
aborts the release. The WAL/SHM files are optional only because SQLite may not have created
them. Do not use `update.sh` or `rollback.sh` for a schema-changing release until those scripts
provide the same fail-closed snapshot and restore of DB sidecars, both binaries, web, units,
and the prior commit identity.

Install from the verified release directory while the panel remains stopped: stage and
atomically replace the agent first, restart it, then stage the panel and web, and finally start
the panel. Never reuse or delete the snapshot during this sequence.

```bash
ssh "$SERVER" "REMOTE_RELEASE='$REMOTE_RELEASE' SNAPSHOT='$SNAPSHOT' RELEASE_COMMIT='$RELEASE_COMMIT' bash -se" <<'REMOTE'
set -euo pipefail
install -m 0755 "$REMOTE_RELEASE/agent" /opt/celikpanel/bin/agent.next
mv -f /opt/celikpanel/bin/agent.next /opt/celikpanel/bin/agent
systemctl restart celikpanel-agent

install -m 0755 "$REMOTE_RELEASE/panel" /opt/celikpanel/bin/panel.next
WEB_NEXT="/opt/celikpanel/web.${RELEASE_COMMIT}.next"
test ! -e "$WEB_NEXT"
install -d -m 0755 "$WEB_NEXT"
tar -xzf "$REMOTE_RELEASE/web.tar.gz" -C "$WEB_NEXT" --no-same-owner
mv /opt/celikpanel/web "$SNAPSHOT/web-before-swap"
mv "$WEB_NEXT" /opt/celikpanel/web
mv -f /opt/celikpanel/bin/panel.next /opt/celikpanel/bin/panel
systemctl start celikpanel-panel
REMOTE
```

**Post-deploy verification** (required on each server before moving to the next):

```bash
EXPECTED_SCHEMA=20                    # set to the release's migration target
curl -fsSk -b "$ADMIN_COOKIE_JAR" \
    "https://${PANEL_HOST}:2083/api/v1/panel/version" | \
    jq -e --arg commit "$RELEASE_COMMIT" --argjson schema "$EXPECTED_SCHEMA" \
      '.commit == $commit and
       .agent_commit == $commit and
       .agent_matches == true and
       .schema_version == $schema'

AGENT_PID="$(ssh "$SERVER" 'systemctl show -p MainPID --value celikpanel-agent')"
PANEL_PID="$(ssh "$SERVER" 'systemctl show -p MainPID --value celikpanel-panel')"
ssh "$SERVER" "systemctl is-active --quiet celikpanel-agent celikpanel-panel && \
    sha256sum /proc/$AGENT_PID/exe /proc/$PANEL_PID/exe"
curl -fsSk "https://${PANEL_HOST}:2083/" | grep -oE 'assets/index[^"]*'
```

The two running `/proc/.../exe` hashes must equal the uploaded `agent` and `panel` hashes,
the API's `schema_version` must equal `EXPECTED_SCHEMA`, the served asset must belong to this
web archive, and both service journals must be clean for the deployment window. A reachable
UI is not sufficient. Rollback is paired: stop the panel, move the failed DB files aside, restore the
snapshot's DB + sidecars, agent, panel, web, and units together, then restart agent first and
panel last.

## 3. Development & testing

- **Builds:** `go build ./... && go vet ./...` · `cd web && npm run build` (tsc + vite; both must pass clean).
- **Visual verification (no live backend):** `tools/dev-preview/preview-server.py` — serves
  `web/dist` behind a stub that mimics the real API; `FRESH=1` for a brand-new server,
  `FIREWALL=on/off` for banner states. Screenshot with playwright (chromium) from any install.
  **Rule: the stub stays faithful to the real schema (types included)** — an unfaithful stub
  masked a real bug once (capabilities.mail_server is a BOOL, not a string).
- **i18n:** `web/src/i18n/en.ts` is the key source (it generates the TranslationKey type);
  `tr.ts` must match the key set exactly. An apostrophe inside a string breaks the build —
  phrase sentences without them.
- **Design loop:** the design system lives on claude.ai/design; details and traps in
  `.design-sync/NOTES.md` (notably: refresh the `cssEntry` hash after every web build).
- **Dev-machine conveniences:** when the agent runs unprivileged, the `CELIKPANEL_*` env
  overrides apply (BACKUP_DIR, DKIM_DIR, MAIL_DIR, RUNTIMES_DIR, NGINX_DIR, SYSTEMD_USER) —
  grep the code; each is documented at its point of use.

## 4. The working model (D-008 — alpha)

The operator drives the panel like a real customer; the engineer **never** hand-installs or
configures services on the server. Live panel settings and all DNS, SSL, mail, firewall, and
service configuration are changed only by the operator through the UI. SSH is read-only for
diagnosis except for the narrowly scoped product deployment and rollback of versioned
CelikPanel artifacts described above. Every wall the operator hits arrives as a product
change: fix → build → verify with a stub screenshot → commit+push → deploy the product
artifacts. Commit messages are Turkish; every commit carries the `momerefe` + `celikalperen`
co-authors; details in [CONVENTIONS](CONVENTIONS.md).

## 5. Live-state snapshot (July 11, 2026)

- Panel v0.1.0 + PowerDNS installed from the panel; NO other services (the operator installs from the panel).
- Firewall: off (operator will turn it on — dashboard/Services show the amber warning).
- No domains yet (D-009 required DNS first; PowerDNS is now installed → next step is a domain).
- DNSSEC: the old key and the registrar DS record are OBSOLETE (the DS was deleted during the
  July 9 reset). Enabling DNSSEC from the panel generates a FRESH key; the new DS goes to the
  registrar then.
- Next steps: [ROADMAP](../ROADMAP.md) → the v0.2 list.

## 6. Secrets policy

There are NO secrets in the repo and never will be (leaked once, never again — see the
Phase 0.8 lesson). The panel admin password is held only by the operator; server SSH keys live
on the operator's machines; service passwords (DB, mail) are generated by the panel and kept in
its SQLite. New-engineer access = the operator adds a new SSH public key on the server;
passwords are never shared.
