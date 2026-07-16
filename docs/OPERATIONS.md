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

Three kinds of change, three recipes. Everything builds on the dev machine; only artifacts
are copied (the server has NO Go/Node — deliberately; see D-008: no hand-installs on the server).

**A) Frontend only** (web/src changed — no restart needed):
```bash
cd web && npm run build && cd ..
tar -C web/dist -czf /tmp/webdist.tar.gz .
scp /tmp/webdist.tar.gz root@2.25.80.4:/tmp/
ssh root@2.25.80.4 'mkdir -p /opt/celikpanel/web.new && tar -xzf /tmp/webdist.tar.gz -C /opt/celikpanel/web.new --no-same-owner && mv /opt/celikpanel/web /opt/celikpanel/web.old && mv /opt/celikpanel/web.new /opt/celikpanel/web && rm -rf /opt/celikpanel/web.old /tmp/webdist.tar.gz && echo DONE'
```
Backed swap: on trouble, rename `web.old` back. `index.html` is served no-cache;
a normal browser refresh suffices.

**B) Panel binary** (cmd/panel or internal changed):
```bash
go build -o /tmp/panel ./cmd/panel
scp /tmp/panel root@2.25.80.4:/tmp/
ssh root@2.25.80.4 'install -m 0755 /tmp/panel /opt/celikpanel/bin/panel && systemctl restart celikpanel-panel && rm /tmp/panel && echo DONE'
```
1–2 s of downtime; sessions live in SQLite, so logins survive.

**C) Agent binary** (cmd/agent or internal/systemd|services changed):
```bash
go build -o /tmp/agent ./cmd/agent
scp /tmp/agent root@2.25.80.4:/tmp/
ssh root@2.25.80.4 'install -m 0755 /tmp/agent /opt/celikpanel/bin/agent && systemctl restart celikpanel-agent && systemctl start celikpanel-panel && rm /tmp/agent && echo DONE'
```

**Full release upgrade:** `update.sh` on the server (snapshots every run: DB + binaries +
units + commit, last 5 kept); one-command revert: `rollback.sh` (the DB deliberately travels
with the binaries — an old binary never runs against a newer schema).

**Post-deploy verification** (after every deploy, from the dev machine):
```bash
curl -sk https://2.25.80.4:2083/ | grep -oE 'assets/index[^"]*'   # served hash == web/dist?
curl -sk -o /dev/null -w '%{http_code}\n' https://2.25.80.4:2083/  # 200?
```

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
configures services on the server (SSH is read-only, for diagnosis). Every wall the operator
hits arrives as a product change: fix → build → verify with a stub screenshot → commit+push →
hand the operator the deploy recipe. Commit messages are Turkish; every commit carries the
`momerefe` + `celikalperen` co-authors; details in [CONVENTIONS](CONVENTIONS.md).

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
