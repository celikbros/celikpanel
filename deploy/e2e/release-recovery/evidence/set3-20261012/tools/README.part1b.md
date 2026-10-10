
On **Arch** the corrected part works and the next one does not (finding P5b): the pool file is now written under
`/etc/php/php-fpm.d/` and `php-fpm`'s own configuration test passes twice ("configuration file /etc/php/php-fpm.conf
test is successful"), then the Agent's nginx activation fails: `open() "/etc/nginx/snippets/fastcgi-php.conf" failed
(2: No such file or directory) in /etc/nginx/sites-enabled/set3-php.test.conf:27`, the previous vhost is restored and
reloaded, the site account is removed again (`userdel` in the journal), and the Panel answers `500 INTERNAL`
"internal server error" (`[500] site creation failed: site provisioning failed during nginx vhost activation`). The
import, which creates a PHP site first, answers the new typed `502 IMPORT_SITE_NOT_CREATED` on every arrival and
imports nothing (no domain row, no account, no database), which is what that answer says.

### O9, O10, O14 (all three platforms)

- **O9.** `POST /api/v1/domains/{id}/ssl/letsencrypt` with the two directory names resolving to the guest's own
  loopback: **`502 CERTIFICATE_ISSUE_FAILED`, reason `authority_unreachable`** on Debian 13, Ubuntu 24.04 and Arch
  (set2: `500 INTERNAL`). `vars` = `{"domain": "set1-owner.test", "kept": "none", "detail": "An unexpected error
  occurred: requests.exceptions.SSLError: HTTPSConnectionPool(host='acme-v02.api.letsencrypt.org', port=443): Max
  retries exceeded with url: /directory (Caused by SSLError(SSLCertVerificationError(... Hostname mismatch ..."}`
  (certbot met this guest's own listener). The answer was replayed byte for byte for one identity (the guard's four
  arrivals pass on all three platforms, Arch included: set2's H39 is gone); no certificate row was written. API
  sentence: "No certificate was issued: this server could not reach the certificate authority, so no request was
  placed with it. The server owner checks that this server can open HTTPS connections to the internet (DNS
  resolution, outbound port 443, the system clock), then requests the certificate here again. The site has no
  certificate from this request and is served as before. Nothing asks again automatically."
- **O10.** `POST /api/v1/database-servers/{id}/databases` with `new_username` and a `new_password` the owner sent, on
  MariaDB and on PostgreSQL: `200`, keys `created_at, id, name, password_set, user`; **no `password` field,
  `password_set: true`, the sent value does not occur in the answer's bytes**; the same identity again: `200`,
  `X-CelikPanel-Request-Replayed: 1`, **the same bytes** (SHA-256 of the raw answers equal), and one new database on
  the engine (`s2_srvsent`). A request without a password still gets a minted one once and the status-only refusal on
  replay, as in set2. No guarded answer of any cell contained a secret its own request had sent (105 answers each).
- **O14.** `GET /api/v1/database-servers` beside the engines' own answers (`SELECT VERSION()`, `mariadbd --version`,
  the client's `--version`, `SHOW server_version`), read when the engines were registered (C0) and again after the
  Panel's account was provisioned (C3); the page's value was the same both times:

  | | Databases page (MariaDB) | `SELECT VERSION()` | client program | Databases page (PostgreSQL) | `SHOW server_version` |
  | --- | --- | --- | --- | --- | --- |
  | Debian 13 | `11.8.6-MariaDB-0+deb13u1 from Debian` | `11.8.6-MariaDB-0+deb13u1 from Debian` | client 15.2 | `17.11 (Debian 17.11-0+deb13u1)` | `17.11 (Debian 17.11-0+deb13u1)` |
  | Ubuntu 24.04 | `10.11.14-MariaDB-0ubuntu0.24.04.1` (set2: `15.1`) | `10.11.14-MariaDB-0ubuntu0.24.04.1` | {{UB_CLIENT}} | `16.15 (Ubuntu 16.15-0ubuntu0.24.04.1)` | `16.15 (Ubuntu 16.15-0ubuntu0.24.04.1)` |
  | Arch | `13.0.2-MariaDB` (set2: the literal `VERSION()`) | `13.0.2-MariaDB` | {{ARCH_CLIENT}} | `18.6` | `18.6` |

### Arch, second reading: with the one file the owner placed by hand (`rid3-arch/run-b`)

{{ARCH_SECOND}}
