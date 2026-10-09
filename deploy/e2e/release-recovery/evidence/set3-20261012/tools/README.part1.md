## Part 1: the second round of corrections on real services

Cell kinds `settings-writes` (cells `set3-*`: every section of set2 again, S8 with the new expectations) and
`request-identity` (cells `rid3-*`: every section of set2 again, two new sections C6b and C7b, new checks in C0, C3,
C5, C7 and C9). Setup profile as in set2: Debian/Ubuntu purpose `web_mail` with `nginx, php-fpm, mariadb, postfix,
dovecot, roundcube, rspamd, postgresql`; Arch purpose `web` with `nginx, php-fpm, mariadb, postgresql`; DNS mode
external; the setup waits at `access_dns` on the isolated guest in every cell (observed). The installed build is the
baseline B of build `cur`, whose tree is the tree of `cfa329676`.

### Item x platform (generated from the cells: `part1-table.md`)

{{PART1_TABLE}}

How to read it:

- **Debian 13 and Ubuntu 24.04: every correction is measured as made.** The Debian column is the re-run after the
  harness fix H40 (`rid3-debian13/run-b`: every section passed; run-a is kept and differs only in the two H40 checks).
  The Ubuntu column is run-a, which was not re-run: its "FAIL" of O10 is H40, not the product. The check compared the
  new database's name on the engine with the name that was sent (`srvsent`), and the engine's name carries the
  subscription prefix (`s2_srvsent`). The facts that check recorded on Ubuntu are the expected ones on both engines:
  first answer `200` without a `password` field and with `password_set: true`, the sent value not in the answer's
  bytes, the replay `200` with `X-CelikPanel-Request-Replayed: 1` and the same bytes, exactly one new database. With
  the corrected check O10 passed on Debian 13 (run-b) and on Arch (4 of 4 each).
- **Arch: S1 (no hash-shaped value), O9, O10 and O14 pass; P5 does not.** A PHP site still cannot be created there, so
  no import starts (finding P5b). Every other "FAIL" in the Arch column is that one defect seen through another
  check (the import's arrivals, the archive with the directory member, the files step of the hostile archives, which
  never ran). Mail is not supported on Arch, so the mailbox login, and groups A and B, are not run there.
- The last column is a **second reading on Arch with one recorded owner action** (the owner placed by hand the one
  file whose absence stops the site creation); it is not the item's result. See "Arch, second reading".

Regression (every section of the latest run of each cell):

{{PART1_REGRESSION}}

### S1: no hash-shaped value in the preview, in any answer of a guarded route, in any stored row or in the journal; the imported mailbox keeps its password

- The fixture changed: every archive's mailbox (`homedir/etc/<domain>/shadow`) now carries the sha512-crypt hash of
  one password the lab chose per cell (made on the guest by `openssl passwd -6`; neither the password nor its hash
  is written anywhere in the evidence, and the base64 form handed to the guest helper is registered with the redactor).
- `POST /api/v1/import/cpanel/inspect`, 7 previews per cell (the four import archives and the three hostile ones): `200`;
  keys `databases, dns_zones, domains, forwarders, mail_accounts, main_domain, public_html, site_bytes, username`; a
  mailbox is exactly `{"domain": "...", "user": "info", "quota_mb": 256, "has_password": true}`. The driver searches
  the answer's **raw bytes** for hash-shaped values before anything is recorded: **0** in every preview on all three
  platforms; no `crypt_hash` key. Only the four named fields are stored (`previews` in
  `steps/NN-c7-import/section.json` and `steps/NN-c7b-import-answers/section.json`).
- Every answer of a guarded route (105 per cell) was searched the same way: 0 hash-shaped values, 0 answers that carry
  a secret their own request sent. `request_identities` at the end of each cell: 35 rows (34 `done`, 1 `interrupted`,
  the killed restore), **no row with a hash-shaped value** in its stored answer or in any other column (the guest
  helper counts; it never prints a stored body). The Panel's and the Agent's journal lines of the cell (374-392
  lines): 0 hash-shaped values.
- **The imported mailbox authenticates with its original password** (Debian 13 and Ubuntu 24.04, three imported
  mailboxes each: `info@set2-import-tar.test`, `-seq`, `-drop`): a real IMAP `LOGIN` on `127.0.0.1:993` answers `OK`
  and `INBOX` is selected; `doveadm auth test -x service=imap` exits 0. Beside each, a password that is certainly
  wrong: IMAP `LOGIN` refused, `doveadm auth test` exit 77. Dovecot 2.4.1 (Debian) and 2.3.21 (Ubuntu).
  The import's own step line: "1 accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)".

### P3 and P3b: service actions (S8, `POST /api/v1/service/action`; Debian 13 and Ubuntu 24.04 alike)

42 actions per platform (set2's 41 and one new), every answer matches what the service shows, none is answered as
unknown (`steps/15-s8-service-actions/section.json`, `journal/`, `native-text/`).

| Situation | Action | Answer | Native |
| --- | --- | --- | --- |
| Postfix stopped | reload | **`409 SERVICE_ACTION_FAILED` / `not_running`**, `vars.detail` "Postfix is not running; nothing was reloaded", `command: sudo postfix status` | the master was not running before and is not running after; nothing was started |
| Dovecot stopped | reload | **`409` / `not_running`**, "Dovecot is not running; nothing was reloaded", `command: sudo systemctl status dovecot` | stopped before and after |
| PostgreSQL, the owner's hook signals the server and then fails | reload | **`502 SERVICE_ACTION_FAILED` / `reload_reread`**, `vars.detail` "the reload of postgresql@17-main.service was reported as failed (exit-code), but PostgreSQL re-read its configuration files after it: pg_conf_load_time() moved" (`@16-main` on Ubuntu) | `pg_conf_load_time()` moved, same postmaster PID, the instance's `ReloadResult=exit-code` |
| PostgreSQL, the owner's hook fails **before** it signals the server (new) | reload | **`502` / `reload_not_reread`**, "the reload of postgresql@17-main.service failed (exit-code) and PostgreSQL did not re-read its configuration files: pg_conf_load_time() did not move" | `pg_conf_load_time()` unchanged, same postmaster PID, `ReloadResult=exit-code` |
| Postfix, `main.cf` holds a line its own check refuses | stop | **`200`**, `success: true`, `outcome: verified`, `applied: stopped` (set2: `502 SERVICE_ACTION_UNKNOWN`) | the master's PID file names no running master; the unit is left `failed` as in set2 |
| the same | reload, restart, start | `502` / `check` with Postfix's fatal line, as in set2 | nothing was sent |

The second hook is realistic and was built for this run: the owner's `ExecReload` script reloads a connection pooler
first (`systemctl reload set1-owner-pooler.service || exit 1`, a unit that does not exist) and signals the server only
when that worked (`native/*owner-postgresql-reload-hook-before-signal.json` holds the script).

Unchanged and recorded: a Reload of a stopped **nginx** and of the stopped **PostgreSQL wrapper** still answers `502` /
`command` with systemd's own line ("postgresql.service is not active, cannot reload."), and MariaDB's Reload answers
`502` / `command` ("Job type reload is not applicable"). The contract's `not_running` for "a wrapper with no running
unit behind it" was therefore not produced by this sequence on either platform: systemd refuses the wrapper's reload
before the Agent's own check is asked (observation O16).

### O11: no `server_setup_busy` while the setup waits at `access_dns`

Counted over the whole service-action section: **0 refusals in 42 actions** on Debian 13 and **0 in 42** on Ubuntu
24.04 (`busy_refusals` in the S8 section; the setup of both cells was waiting at `access_dns`,
`server_setup_access_dns_required`, for the whole section). set2 had 1 on each platform.

### P4: the import's archive handling (C7 and C7b; Debian 13 and Ubuntu 24.04)

- **Every archive of this run holds the directory member `homedir/public_html/` as tar writes it** (set2 had to leave
  it out of the arrivals' archives, H34). All of them import completely: the dedicated archive (`tar`), the
  sequential and the concurrent arrivals and the dropped one answer `200`, `status: active`, `domain_status: active`,
  `imported: [domain, files, mail, forwarders, dns, database:<name>]`, `not_imported: []`; the document root's files
  are **equal to the archive's by name, size and SHA-256** (2 files; 3 with the 16 MiB file of the dropped import),
  the mailbox, the forwarder and the 300 rows are there.
- **A failed files step answers `200` with `status: partial`.** Archive with a member
  `homedir/public_html/../../set3-escape-dotdot.txt`, imported with files, mail and databases: `200`, `status: partial`,
  `code: IMPORT_PARTIAL`, `domain_status: pending`, `imported: [domain, mail, forwarders, dns, database:s3hdd_app]`,
  `not_imported: [files]`, step `files`: "unsafe cpmove member path". Natively the lists are true: the domain row is
  `pending`, the mailbox and the forwarder exist, the database holds its 300 rows, and the document root holds only
  the new site's own `index.php` (none of the archive's files). The same identity again: the stored answer byte for
  byte, nothing imported again. Never `202`, never `pending` as the import's status.
- **Hostile members are never extracted.** After each of the three imports the guest was searched for any entry named
  `set3-escape*` outside the import directory (`/var/www`, `/var/lib`, `/var/tmp`, `/var/backups`, `/etc`, `/tmp`,
  `/home`, `/root`, `/srv`, `/opt`, `/usr/local`): **none**, no link in any document root, no stage directory left in a
  site's home. `..` path: files step refused ("unsafe cpmove member path"). Symbolic link `set3-escape-link -> /etc`
  with a file named through it: files step refused ("unsupported cpmove site entry type"), answer `200` / `partial`,
  `imported: [domain, dns]`, `not_imported: [files]`. Absolute path `/etc/set3-escape-absolute.txt`: **not refused but
  left out** (it is not site payload): the import answers `200` / `active` with the archive's own two files in the
  document root and nothing at `/etc/set3-escape-absolute.txt`; the answer does not mention the member (observation
  O17).

Message of the partial answer (EN, API): "The import ended with a part of the archive not imported, and it does not
continue by itself. Imported: domain, mail, forwarders, dns, database:s3hdd_app. Not imported: files. The domain
set3-hostile-dotdot.test was created and is kept; it is left marked as not finished. The reason of each part that
was not imported is in its step below. The server owner either adds the missing parts by hand on the domain's own
pages, or removes set3-hostile-dotdot.test on the Domains page, corrects what the step names and imports the archive
again; an import into a domain that already exists is refused, so nothing is imported twice."

### P5: a PHP site (C6b)

| | Debian 13 | Ubuntu 24.04 | Arch (run-a) |
| --- | --- | --- | --- |
| `POST /api/v1/domains/create` (`project_type: php`) | `200` | `200` | **`500 INTERNAL` "internal server error"** |
| `GET /set3-probe.php` through nginx on loopback | `200`, `set3-php-executed:42:8.4.26:fpm-fcgi:set3_php_test` | `200`, `set3-php-executed:42:8.3.6:fpm-fcgi:set3_php_test` | not reached |
| PHP-FPM unit | `php8.4-fpm.service` | `php8.3-fpm.service` | `php-fpm.service` (`/usr/lib/systemd/system/php-fpm.service`, `ProtectSystem=full`), PHP 8.5.11 |
| Pool directory, the site's pool file | `/etc/php/8.4/fpm/pool.d/site2.conf` | `/etc/php/8.3/fpm/pool.d/site2.conf` | `/etc/php/php-fpm.d/` (only the stock `www.conf` after the failed creation) |
| Socket, owner, mode | `/var/run/php/php8.4-fpm-site2.sock`, `www-data:www-data`, `0660` | `/var/run/php/php8.3-fpm-site2.sock`, `www-data:www-data`, `0660` | stock socket `/run/php-fpm/php-fpm.sock`, `http:http`, `0660`; no site socket |
| `DELETE /api/v1/domains/{id}` | `200`; not listed after 1.0 s; no row, no account, no pool file, the page answers `404`, PHP-FPM active | the same (1.5 s) | not reached |

The page prints a marker, `6 * 7`, `PHP_VERSION`, `php_sapi_name()` and the account it runs as; served as text it
would show its source. It ran as the site's own account on both platforms.
