<div align="center">

# CelikPanel

**A next-generation web hosting control panel. One binary. Zero dependencies. 60-second install.**

[Türkçe](README.tr.md) · [Roadmap](ROADMAP.md) · [User Roles](docs/ROLES.md)

</div>

---

CelikPanel is a modern alternative to cPanel and Plesk: a single statically-compiled Go binary with an embedded React interface and SQLite storage. It needs nothing else to run — no external database, no web server, no interpreter.

## Why another panel?

cPanel and Plesk are both owned by the same company today, prices climb every year, and the products carry twenty years of legacy: hours-long installs, forced dependencies, outdated defaults, cluttered interfaces.

Our answer is Google's answer to AltaVista: **radical simplicity and speed.** AltaVista tried to become a portal and lost; Google won with one search box. cPanel/Plesk are today's portals. CelikPanel is the search box.

| The old way | The CelikPanel way |
|---|---|
| Hours-long installation | One command, ~60 seconds *(target)* |
| Panel drags MySQL, PHP, Perl along | Single Go binary + SQLite — zero dependencies |
| Old service versions imposed | Always the latest from OS repos; the customer picks the version |
| Everything installed up front | Modular: install only the services you need, from the UI |
| Cluttered portal interface | Fast SPA; services you didn't install are invisible |

## Principles

Every feature, commit and design decision passes four filters — in this order:

1. **Security by default** — least privilege, secure defaults, nothing ships without authentication.
2. **Simplicity** — one obvious way to do each thing. Saying *no* is a feature.
3. **Speed** — API responses under 100 ms, instant UI, 60-second install.
4. **Flexibility** — API-first, modular services, your data is never held hostage.

## Architecture

```
Browser — React SPA
   │  HTTPS
   ▼
Panel — Go HTTP server (port 1983), unprivileged user, SQLite
   │  local RPC (moving to Unix socket + token, Phase 0)
   ▼
Agent — root daemon; the only component allowed to touch the OS
   ▼
Managed services: Nginx · PHP-FPM 8.x · MariaDB · PostgreSQL ·
Postfix · Dovecot · PowerDNS · Fail2ban · vsftpd · Redis · …
```

The privilege split is deliberate: the web-facing Panel never runs as root. Only the Agent — reachable exclusively from the local machine — holds root, which structurally blocks the classic "web layer to root" panel exploit.

## Status — v0.1.0 alpha

> ⚠️ **Not production ready.** The Phase 0 security sprint (authentication, agent lockdown, injection fixes) is in progress. Do not expose this panel to the internet yet.

**Working today** (functional, being hardened): domain & site management · PHP version selection and FPM pools · SSL (Let's Encrypt + custom certificates) · DNS (PowerDNS) · e-mail accounts and forwarding · database management with multi-server support (MariaDB/PostgreSQL) · file manager · backup/restore · cron jobs · log viewer · service control for 14 services.

**What's next:** see the [Roadmap](ROADMAP.md) — Phase 0 security sprint → Phase 1 golden path hardening → Phase 2 60-second installer → Phase 3 WordPress toolkit + cPanel importer.

## Building from source

Requirements: Go ≥ 1.24, Node ≥ 20.

```bash
# Backend (panel + agent)
go build -o bin/panel ./cmd/panel
go build -o bin/agent ./cmd/agent

# Frontend
cd web && npm install && npm run build   # output: web/dist, served by the panel binary
```

## Documentation

- [Roadmap](ROADMAP.md) — where we are, where we're going, and what we deliberately won't do
- [User Roles & Permissions](docs/ROLES.md) — Administrator / Reseller / Customer / Additional User model

## License

Not decided yet; the repository is private while the licensing model (open source / open core / commercial) is being evaluated.
