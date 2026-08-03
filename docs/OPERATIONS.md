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

**B) Paired backend release** (any `cmd/panel`, `cmd/agent`, `internal`, migration, or other
backend change):

Panel and agent are one fail-closed release pair. Never build, install, or roll back either
binary independently. Build both from the same clean Git commit with identical
`main.buildVersion` and `main.buildCommit` linker values; a backend release also carries the
web build from that commit. The commit value must be the exact SHA that will be verified after
deployment.

```bash
test -z "$(git status --porcelain)"
RELEASE_COMMIT="$(git rev-parse HEAD)"
RELEASE_VERSION="$(git describe --tags --always)"
RELEASE_FLAGS="-X main.buildVersion=${RELEASE_VERSION} -X main.buildCommit=${RELEASE_COMMIT}"
go build -trimpath -ldflags "-s -w ${RELEASE_FLAGS}" -o /tmp/celikpanel-agent ./cmd/agent
go build -trimpath -ldflags "-s -w ${RELEASE_FLAGS}" -o /tmp/celikpanel-panel ./cmd/panel
cd web && npm run build && cd ..
tar -C web/dist -czf /tmp/webdist.tar.gz .
```

Upload all three artifacts before changing the running pair. On each server, deploy in this
order: **agent first**, then **panel and web**, then start the panel. If any stage fails, stop;
do not perform panel mutations with a mixed pair.

```bash
SERVER=root@2.25.80.4
scp /tmp/celikpanel-agent /tmp/celikpanel-panel /tmp/webdist.tar.gz "$SERVER":/tmp/
ssh "$SERVER" 'install -m 0755 /tmp/celikpanel-agent /opt/celikpanel/bin/agent.next && cp -a /opt/celikpanel/bin/agent /opt/celikpanel/bin/agent.previous && mv -f /opt/celikpanel/bin/agent.next /opt/celikpanel/bin/agent && systemctl restart celikpanel-agent'
ssh "$SERVER" 'install -m 0755 /tmp/celikpanel-panel /opt/celikpanel/bin/panel.next && cp -a /opt/celikpanel/bin/panel /opt/celikpanel/bin/panel.previous && rm -rf /opt/celikpanel/web.new /opt/celikpanel/web.previous && mkdir -p /opt/celikpanel/web.new && tar -xzf /tmp/webdist.tar.gz -C /opt/celikpanel/web.new --no-same-owner && mv /opt/celikpanel/web /opt/celikpanel/web.previous && mv /opt/celikpanel/web.new /opt/celikpanel/web && mv -f /opt/celikpanel/bin/panel.next /opt/celikpanel/bin/panel && systemctl start celikpanel-panel'
```
Repeat the same release pair on both servers. Keep the previous pair and web tree
available until verification succeeds. A rollback is also paired: restore the matching agent,
panel, web, and schema snapshot together. Sessions live in SQLite, so a normal short restart
does not log users out.

**Snapshot-backed full release:** the paired artifact recipe above is normative. Before a
release that can migrate the database, take the same DB + WAL + binaries + units + commit
snapshot used by `update.sh` (last 5 are kept). `update.sh` may be used only when it installs
the prebuilt exact-commit pair in the same agent-first order; otherwise use the paired artifact
recipe. `rollback.sh` restores the DB and matching binaries together — an old binary must
never run against a newer schema.

**Post-deploy verification** (after every deploy, from the dev machine):
```bash
curl -sk https://2.25.80.4:2083/ | grep -oE 'assets/index[^"]*'   # served hash == web/dist?
curl -sk -o /dev/null -w '%{http_code}\n' https://2.25.80.4:2083/  # 200?
curl -sk https://2.25.80.4:2083/api/v1/version
# Require: commit == RELEASE_COMMIT, agent_commit == RELEASE_COMMIT, agent_matches == true.
```
Verify those three version conditions on both servers. A reachable UI is not sufficient when
the pair is mismatched; the backend fails closed for privileged mutations until the exact
commits match.

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
