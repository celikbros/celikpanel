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

## 2. Deploy and rollback

The only normative production update path is the reviewed [update.sh](../update.sh), and the
only normative rollback path is [rollback.sh](../rollback.sh). Do not reproduce either script's
snapshot, trust-chain, checksum, systemd-state, or restore internals in an SSH one-liner or a
manual runbook. `update.sh` produces snapshot contract v3; `rollback.sh` accepts only that
verified contract.

Running these version-controlled product scripts over SSH is allowed deployment work. It does
not authorize changing live panel settings, DNS, SSL, mail, firewall, or service configuration
over SSH; the operator performs those changes only through the panel.

Before deployment, merge and push a clean commit and prove it in development with
`go test ./...`, `go vet ./...`, and `cd web && npm run build`. Freeze that release commit for
the two-server rollout. Update Boston first and verify it completely; update Frankfurt only
after Boston passes. From each server's existing root-trusted CelikPanel checkout:

```bash
test -z "$(git status --porcelain)"
sudo ./update.sh
```

`update.sh` owns the root trust-chain checks, mutation idle proofs and shared flock, paired
panel/agent/web/database/ledger/unit snapshot, service enabled/active-state ledger, checksums,
retention, fast-forward Git update, rebuild, install, and post-install service checks. It prints
the absolute path of the verified rollback snapshot. Any refusal is a release blocker; do not
repair snapshot or coordinator state by hand.

After each server, require the panel and agent to be active, require the authenticated
`/api/v1/panel/version` response to report the same expected full commit for panel and agent and
the expected schema, load the served UI asset, and inspect both service journals for the release
window. Do not continue to the second server while any check differs.

Firewall boot persistence follows the same explicit-user contract. `install.sh` installs the
restore unit, then `enable-firewall-restore-if-saved.sh` removes persistent and runtime enable
links when no safe saved snapshot exists; an existing non-empty regular snapshot is re-enabled
without starting or applying it. Only explicit **Save for reboot** may create the first snapshot,
and it enables the unit only after the durable write succeeds. Background synchronization may
refresh an existing snapshot but never enables the unit. Explicit **Turn off** removes the
snapshot and disables the unit. GET, rescan, and background status work never enable it.

If verification fails, use the exact verified snapshot path printed by `update.sh`:

```bash
sudo ./rollback.sh "$VERIFIED_SNAPSHOT"
```

`rollback.sh` validates the v3 snapshot and every checksum before stopping or overwriting
anything. It restores the paired artifacts and restores each owned unit's saved enabled and
active state exactly; firewall-unit presence alone never authorizes enablement. Roll back the
already-updated server before attempting the other server, then repeat all read-only checks.

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
