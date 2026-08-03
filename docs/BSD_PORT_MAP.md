# BSD Port Feasibility Map

*Feasibility study · July 8, 2026 · [Türkçe](BSD_PORT_MAP.tr.md)*

A concrete answer to "how much work is a FreeBSD port, really?" — every
OS-touching point in the agent, its FreeBSD equivalent, and an honest
estimate. No code was changed to produce this; it measures risk before any
investment. See the decision this supports in [DECISIONS.md](DECISIONS.md#d-004).

## Proven today

The whole codebase cross-compiles to FreeBSD **right now**, on current
source:

```
make freebsd-cross   → ✓
```

The target first rejects every compiler except exactly Go 1.26.5, disables
automatic toolchain downloads, then proves panel and agent on amd64 and panel
on arm64.

The **panel** (HTTP, SQLite, UI, business logic, the RPC contract) is
OS-neutral Go and needs **zero** changes. All BSD work lives in the agent —
and only in the ~13 of its 36 files that shell out to the OS.

## What is already portable (no work)

These tools exist on FreeBSD (from ports/pkg) with the **same command-line
interface** — only their config paths differ (handled once, see below):

- **Mail stack**: `postconf`, `postmap`, `postsuper`, `postqueue` (Postfix),
  `doveadm`, `doveconf` (Dovecot), `opendkim` — identical invocations.
- **DNS**: `pdnsutil`, `pdns_control` (PowerDNS) — identical.
- **TLS**: `certbot` — identical.
- **VPN**: `wg`, `wg-quick` — WireGuard is native on FreeBSD, identical.
- **POSIX**: `chown`, `chgrp`, `cp`, `tar`, `du`, `which`, `sudo` — identical.
- **Runtimes**: Node.js is a distro-independent tarball already — OS-neutral.

The agent guards every one of these with `exec.LookPath` already, so on a
system without a given tool it returns an honest error instead of crashing —
the same discipline that makes a BSD port safe to add incrementally.

## What needs an OS abstraction (the actual port)

| Area | Linux (today) | FreeBSD | Effort | Notes |
|------|---------------|---------|--------|-------|
| **Service control** | `systemctl` (21 call sites) | `service(8)` + `sysrc` | **Medium** | Collapse to one `serviceMgr` helper; every call site already goes through a few functions |
| **Service logs** | `journalctl` (1) | syslog / logfiles | Low | One read path |
| **Unit generation** | systemd units (`celikapp-*`, drop-ins) | `rc.d` scripts | Medium | Node app supervision + mail/DNS drop-ins → rc.d templates |
| **Packages** | `apt-get`, `apt-cache` (2) | `pkg` | Low | `detectPkgFamily` is already multi-family by design; add a `pkg` arm |
| **Users/groups** | `useradd`/`usermod`/`groupadd` (3) | `pw` | Low | One `userMgr` helper |
| **Firewall/NAT** | `nftables` (1, VPN NAT) | `pf` | Medium | Single site (VPN masquerade); pf.conf anchor |
| **Routing/IP** | `ip(8)` (3) | `route`/`ifconfig` | Low | Default-route + address lookup |
| **sysctl** | `sysctl` (1, ip_forward) | `sysctl` (same) | None | Identical |
| **Filesystem paths** | `/etc/*`, `/var/www`, `/var/mail` | `/usr/local/etc/*`, `/usr/local/www` | Medium | ~30 hardcoded paths → one `ospaths` table keyed by GOOS |

## The shape of the work

The port is **not** a rewrite — it is introducing a thin OS-abstraction seam
the agent mostly already has, then filling in a FreeBSD implementation behind it:

1. **`serviceMgr` interface** — `Start/Stop/Restart/Enable/Status/WriteUnit`.
   Linux backend = systemd (exists, just extract); FreeBSD backend = service + sysrc + rc.d.
2. **`pkgMgr`** — extend the existing `detectPkgFamily` with a `pkg` family.
3. **`userMgr`** — wrap useradd/pw.
4. **`firewall`** — nftables today, pf on BSD (one method: masquerade rule).
5. **`ospaths`** — a struct of config/data locations chosen by GOOS.

Panel, UI, database, RPC surface, install flow logic: **unchanged**. The
`install.sh` bootstrap would gain a FreeBSD branch (pkg instead of apt, rc.d
service enable) — a sibling of the existing apt path, not a rewrite.

## Honest estimate

- **One focused engineer: ~2–4 weeks** for a working FreeBSD agent covering
  the core (services, packages, users, web, mail, DNS, TLS, VPN), including
  real on-box testing. Most of the stack (Postfix/Dovecot/PowerDNS/certbot/
  WireGuard) behaves identically; the work is the five small seams above plus
  path mapping and rc.d templates.
- **Risk: low and bounded.** The surface is measured (13 files), the tools
  exist on BSD, the panel is already proven to compile. There is no unknown
  architectural blocker — the panel↔agent split was the hard part and it was
  done for security on day one.

## Recommendation

Keep the option, don't spend it yet. The seams above are worth extracting
**opportunistically** as the agent evolves (each new OS-touching feature
should go through a helper, not a raw `exec` — already the house style), so
that if a real BSD demand appears, the port is a fill-in-the-backend job of
weeks, never a fork. Full execution waits for real demand, per
[DECISIONS.md D-004](DECISIONS.md#d-004).
