# What is offered on which distro?

<!-- THIS FILE IS GENERATED — do not edit by hand. -->
<!-- Source: internal/core/managed_services.go · Regenerate: go run ./tools/gen-distro-matrix -->
<!-- Guard: internal/core/distro_matrix_test.go (the test fails when the catalogue changes without regenerating) -->

This list is not written by hand; it is generated from the component catalogue.
The panel's Components page reads the same catalogue — this document and the
screen cannot drift apart.

Rules:

- **An empty cell (—) is a promise, not a gap:** when "installed means working"
  cannot be guaranteed on a distro, the row is deliberately not offered there.
  A half-install is broken.
- **Seat:** components in the same seat do the same job; only one can be
  installed at a time (two web servers cannot share port 80). Choosing the
  other one is a swap, not an addition.
- **Needs** names a ROLE, not a product: a row that needs "an SMTP server" is
  satisfied by any installed member of that seat.

## Installed from distro packages

### Web

| Component | Kind | Debian/Ubuntu (apt) | Arch (pacman) | Seat | Needs |
|---|---|---|---|---|---|
| 🐘 PHP-FPM | Runtime | `php-fpm` · version choice: Sury PHP (packages.sury.org) | `php-fpm` | — | — |
| 🔄 Nginx | Service | `nginx` | `nginx` | web server | — |
| 🪶 Apache | Service | `apache2` | `apache` | web server | — |

### Database

| Component | Kind | Debian/Ubuntu (apt) | Arch (pacman) | Seat | Needs |
|---|---|---|---|---|---|
| 🐘 PostgreSQL | Service | `postgresql` · version choice: PostgreSQL Global Development Group (PGDG) | — | — | — |
| 🦭 MariaDB | Service | `mariadb-server` | — | — | — |
| 🐬 phpMyAdmin | Tool | `phpmyadmin` | `phpmyadmin` | — | MariaDB, web server, PHP-FPM |
| 🐘 phpPgAdmin | Tool | `phppgadmin` | — | — | PostgreSQL, web server, PHP-FPM |

### E-mail

| Component | Kind | Debian/Ubuntu (apt) | Arch (pacman) | Seat | Needs |
|---|---|---|---|---|---|
| 📧 Postfix | Service | `postfix` | `postfix` | SMTP server | — |
| 📮 Exim | Service | `exim4-daemon-light` | `exim` | SMTP server | — |
| 🧹 Rspamd | Service | `rspamd` | `rspamd` | spam filter | SMTP server |
| 📬 Dovecot | Service | `dovecot-imapd` `dovecot-pop3d` `dovecot-lmtpd` | `dovecot` | IMAP server | — |
| 🛡️ SpamAssassin | Service | `spamassassin` `spamd` `spamass-milter` | — | spam filter | SMTP server |

### Security

| Component | Kind | Debian/Ubuntu (apt) | Arch (pacman) | Seat | Needs |
|---|---|---|---|---|---|
| 🔐 WireGuard VPN | Service | `wireguard` | `wireguard-tools` | — | — |
| 🚫 Fail2ban | Service | `fail2ban` | `fail2ban` | — | — |
| 🧱 Firewall engine | Tool | `nftables` | `nftables` | — | — |
| 🦠 ClamAV | Service | `clamav` `clamav-daemon` | `clamav` | — | — |

### DNS

| Component | Kind | Debian/Ubuntu (apt) | Arch (pacman) | Seat | Needs |
|---|---|---|---|---|---|
| 🌐 BIND | Service | `bind9` | `bind` | DNS server | — |
| ⚡ PowerDNS | Service | `pdns-server` `pdns-backend-sqlite3` | `powerdns` | DNS server | — |

### FTP

| Component | Kind | Debian/Ubuntu (apt) | Arch (pacman) | Seat | Needs |
|---|---|---|---|---|---|
| 📂 vsftpd | Service | `vsftpd` | `vsftpd` | — | — |

### Cache

| Component | Kind | Debian/Ubuntu (apt) | Arch (pacman) | Seat | Needs |
|---|---|---|---|---|---|
| ⚡ Redis | Service | `redis-server` | — | — | — |
| 💾 Memcached | Service | `memcached` | `memcached` | — | — |

### Monitoring

| Component | Kind | Debian/Ubuntu (apt) | Arch (pacman) | Seat | Needs |
|---|---|---|---|---|---|
| 📈 Netdata | Service | — | `netdata` | — | — |

## Installed from the official release (identical on every distro)

These components never bind to a distro package: the panel downloads the
vendor's release, verifies its SHA-256 and installs it the same way on every
Linux (D-018). There is no distro column because the answer is the same in
every column: yes.

| Component | Kind | Seat | Needs |
|---|---|---|---|
| 🟩 Node.js | Runtime | — | web server |
| ✉️ Roundcube | Tool | — | IMAP server, web server, PHP-FPM |

