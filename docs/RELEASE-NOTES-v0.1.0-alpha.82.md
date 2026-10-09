# v0.1.0-alpha.82 (candidate)

[Türkçe](RELEASE-NOTES-v0.1.0-alpha.82.tr.md)

This is the draft for the release that follows v0.1.0-alpha.81. It is a
candidate, not a published release, and the number is the owner's decision. It
changes what the interface shows when it does not know the server's state, how
settings and service actions are written and answered, and what happens when
the same change reaches the server more than once. It states what was measured,
how, and what was not.

What a screen shows rests on component tests and on a browser against a mock of
the API, not on a real Panel (see "Screens" below). That v0.1.0-alpha.81 was
published is reported by the owner; the repository holds its tag but no record
of the publication steps.

The checks were made on the code as it stood at different moments of the work.
A short table under "What was measured" says which check rests on which state
of the code, and the limits say what was never run on the final code.

## What changes for the server owner

**After the update, reload every CelikPanel page that was open.** A page loaded
before the update still reads, but the server refuses some of the changes it
sends until the page is reloaded: creating or restoring a backup, applying a
cPanel import, requesting a Let's Encrypt certificate, creating a database,
setting up CelikPanel's own account on a database server, adding a VPN device,
and saving the mail policy, a backup schedule, a scheduled task, a catch-all
address or a configuration file. The refusal says to reload. A refused request
changes nothing on the server.

**The interface no longer states a negative it does not know.** On the screens
this release touches, a read has three states: checking; could not be checked,
with a retry; or the known answer. A slow or failed read is no longer shown as
"no DNS server", "no database server installed", an empty list or "does not
point here", and a form is no longer filled with defaults the server never
sent. What you typed stays while the page checks again. Not every screen is
converted yet; the limits name what is left.

**Leaving the tab or a failed license check no longer rebuilds the page.** When
the Panel cannot confirm access for a moment, the page you were on stays, with
its open dialog, its typed input and its selected tab. A layer over it says
what happened and that it checks again by itself; nothing can be changed until
access is known. The license rules are the same: a missing, expired or invalid
license still closes the Panel.

**Server setup says in advance that the Panel restarts once.** At the step
"Secure panel access" the Panel gets its certificate and restarts to use it.
The wizard now says so before and during that step, shows the short loss of
connection as planned, and reconnects by itself. A setup stopped because the
server is busy shows the reason, who acts and the next action in one place.

**A save can no longer overwrite what is on the server with what a page
happened to show.** The mail policy, a domain's backup schedule, scheduled
tasks, the catch-all address and configuration files (the PostgreSQL and
MariaDB ones also get the checks in the next paragraph) are read with a
version, and a save must carry that version back. If the server
changed since the page loaded, or the page never read the current state, the
save is refused and nothing is written. A state that could not be read is shown
as "could not be read", not as an empty form. Your own Postfix recipient
restrictions stay in place and in order; the Panel changes only the DNSBL
entries it manages. In a crontab only the task's own line changes; your own
lines and comments stay.

**A database configuration file is checked before it replaces the current
one.** The server's own program reads a copy of the new file first. The
previous file is kept as a dated copy beside it. PostgreSQL is reloaded, and if
the reload fails the previous file is put back and the answer says what was
verified. A change that would remove the local administrator access to
PostgreSQL is refused. MariaDB cannot re-read its files without a restart: the
change is saved, the answer says it waits for a restart, and the Panel does not
restart MariaDB. A program named `mysqld` that is not MariaDB is not accepted
as the check of a MariaDB file; the file is then not replaced (component tests
only).

**A reload and a service action are judged by what the service shows
afterwards.** On Ubuntu 24.04 the unit named `postfix`, and on Debian and
Ubuntu the unit named `postgresql`, only wrap the real service, and the service
manager reports success for them whatever the service did. v0.1.0-alpha.81
did not check the reload after a mail policy save at all (read from its
source), so it answered "saved" also when Postfix had refused to reload. The
Panel now asks the service itself, and a failed action names the unit, the
service's own line and the command that shows it. An action whose result could
not be established is shown as unknown, not as done and not as failed. A reload
of a service that is not running is refused and sends nothing to it. A Stop of
Postfix is answered once Postfix has stopped; if systemd then shows the unit as
failed, the answer says so in a note and leaves the mark as it is.

**The same change is carried out once.** A browser can send a change again by
itself when a connection is reset. On the eight changes where a repeat did harm
(listed under API changes), every request now carries one identity. A repeat is
answered with the first result and never runs again; where the first result
held a one-time secret, the repeat is told the change was already made and the
secret is not shown again. Once accepted, such a change runs to its end even if
the page is closed.

**cPanel import says what it did.** The preview no longer sends mailbox
password hashes to the browser. An import that could not bring one of its parts
is reported as partial. Each part is listed as imported, not imported, or left
out (not chosen, left to your external DNS provider, nothing of it in the
archive, or none of it imported). An archive that holds its site folder as a directory entry, as `tar`
writes it, is imported instead of losing its files. An entry the archive names
with an absolute path is left out and named.

**PHP sites can be created on Arch.** The PHP hand-off is now written into
each site's web server configuration instead of naming a file that only
Debian's and Ubuntu's nginx packages ship. The update rewrites the web
server configuration of hosted sites when the new Panel starts. If you
edited the package's `/etc/nginx/snippets/fastcgi-php.conf`, your sites stop
reading that file at the update; the file is left in place and nothing detects
your edit.

**A site the web server refuses is answered as that.** When a site is created
or imported, instead of "internal server error" the answer says that nginx refused the configuration, what was
removed again and the command that shows it.

**A failed certificate request says why.** A Let's Encrypt request that certbot
could not fulfil is answered with one of five reasons (authority unreachable,
validation, rate limit, time-out, tool error), what stays in place and who
acts, instead of "internal server error".

**Mail certificate renewal checks the certificate that is actually served.**
After it reloads Postfix and Dovecot, the renewal connects to their local
listeners and compares the certificate they present with the one it selected.
If they differ or no listener answers, the renewal stays open and is retried,
as after a failed reload: within the existing limit of three executions, then
it waits for you, and a later renewal is not admitted while one is open. This
has component tests only. A server on which the
independent renewal was set up by an earlier release keeps the helper it has;
see the limits.

**Mail certificate changes on a server whose Postfix configuration prints a
warning.** This concerns you if `postconf -n >/dev/null` prints a warning on
your server (for example for a comment after a value, or an unused parameter).
In v0.1.0-alpha.81 the Agent read such a warning as part of the value of each
mail TLS setting. Measured with the real `postconf` on a private configuration
directory: the text that code kept before a change, handed to the command its
rollback uses, was refused for a setting that was set, so that setting would
not have been restored, and was written as the warning line itself for a
setting that was not set. (The kept text was reproduced by trimming both output
streams of the command; the old code itself was not run.) Read from the source and not run: on such a server
every mail TLS change ended as "changed, not verified", and the publication of
a mail certificate, renewal included, was paused. Now a warning line is never
part of a value, and a setting that cannot be read as one value stops the
change before it starts and names the setting and the commands that show it.
This has component tests that use the measured output. A mail TLS change, its
rollback and a certificate publication with such a file were not run.

**The update card says what already happened.** When the offered version was
already tried on this server and rolled back, the card says so, when the
attempt ended, which version runs now and the recorded cause. The version is
still offered and Start is not disabled. This text belongs to this release's
interface: after a return to v0.1.0-alpha.81 the server shows alpha.81's card.

### What you may need to do

- Reload open CelikPanel pages after the update.
- If scripts of yours call the API, read "API changes" before updating.
- On a server with mail, run `postconf -n >/dev/null`. If it prints a warning,
  correct the line of `main.cf` it names; that removes the cause described
  above on any release.
- On a server with mail, `sudo postfix check` shows whether Postfix accepts
  its configuration now. If it prints an error, a mail policy saved with an
  earlier release may not be in effect; correct the line it names, then run
  `sudo postfix reload`.
- After you change a MariaDB setting in the Panel, restart MariaDB yourself
  when it suits you; until then the old value is in effect.

## API changes

Panel and Agent are installed together by one release, and the writes below
need both sides: a Panel and an Agent of different releases cannot make them.

### Writes that must carry a version

Each read returns `version`. The write sends it back, in the JSON body or,
for a `DELETE`, as the query parameter `version`.

| Write | `reason` in a refusal |
| --- | --- |
| `PUT /api/v1/mail/policy` | `mail_policy` |
| `PUT`, `DELETE /api/v1/domains/{id}/backups/schedule` | `backup_schedule` |
| `POST`, `PUT`, `DELETE /api/v1/domains/{id}/cron` | `scheduled_tasks` |
| `PUT`, `DELETE /api/v1/domains/{id}/mail/catch-all` | `mail_catch_all` |
| `POST /api/v1/config` (`path`, `content`, `version`) | `config_file` |

- Without a version: `409 SETTINGS_VERSION_REQUIRED`. Nothing is written.
- With a version that is no longer the server's: `409 SETTINGS_CHANGED`.
  Nothing is written; read again and repeat.
- When the current state cannot be read: `502 CURRENT_SETTINGS_UNREADABLE`, on
  the read and on the write, for the mail policy, scheduled tasks and
  configuration files. (The backup schedule and the catch-all are kept in the
  Panel's own database; a failed read there is an ordinary server error.) For
  scheduled tasks `detail` is `cron_allow` or `cron_deny` when that cause was
  verified, and `vars.detail` carries the line the server's own program
  printed.
- Scheduled tasks: a task's `id` is now 16 hexadecimal characters, derived
  from the task's own line; read the list again instead of reusing an id from
  an earlier release. The same task twice is
  `409 CRON_JOB_DUPLICATE`; a task that stands on two lines of the crontab is
  `409 CRON_JOB_AMBIGUOUS`.
- Mail policy: `200` carries `applied` (`reloaded`, `not_running`, `unchanged`,
  `unchanged_reloaded`). `502 MAIL_POLICY_NOT_RELOADED` with `reason` `check`,
  `reload` or `verify` means the file holds the values and Postfix did not take
  them; the body carries the written `policy` and, when this request changed the
  file, `mutation_applied`. `502 MAIL_POLICY_RELOAD_UNKNOWN` means the file holds
  the values and whether Postfix took them is not known. `409
  MAIL_POLICY_RESTRICTIONS_UNMANAGED` refuses a DNSBL change the Panel will not
  make to your restrictions; `400 MAIL_POLICY_INVALID` refuses a value outside
  the allowed range or a DNSBL zone that is not a plain host name.
- Configuration files: `200` carries `version`, `unchanged`, `backup`,
  `applied`, `daemon_check` and `restart_required`. `422 CONFIG_INVALID` with
  `reason` `empty`, `shape`, `syntax`, `daemon`, `lockout` or `no_validator`
  means nothing was written. `502 CONFIG_RELOAD_FAILED` has `reason`
  `restored`, `restored_unit_reload_failed`, `restored_running_unknown` or
  `not_restored`.
- `GET /api/v1/postfix/queue` and `GET /api/v1/postfix/summary`: a queue that
  cannot be read is `502 MAIL_QUEUE_UNREADABLE` (`reason` `postfix_config` when
  verified), never an empty list.

### Requests that must carry an identity

Eight routes, all `POST`:

- `/api/v1/domains/{id}/backups`
- `/api/v1/domains/{id}/backups/restore`
- `/api/v1/domains/{id}/databases`
- `/api/v1/domains/{id}/ssl/letsencrypt`
- `/api/v1/database-servers/{id}/databases`
- `/api/v1/database-servers/{id}/admin-account`
- `/api/v1/vpn/peers`
- `/api/v1/import/cpanel/apply`

Send the header `X-CelikPanel-Request-Id`: 32 lowercase hexadecimal characters,
a new value for each action. An answer to a request that carried a valid
identity repeats the header (the refusals for a missing or malformed header
cannot).

| Situation | Answer |
| --- | --- |
| No header | `428 REQUEST_ID_REQUIRED`; nothing ran |
| Header not in that form | `400 REQUEST_ID_REQUIRED`; nothing ran |
| Same identity, same request, first one finished | the first answer again, with `X-CelikPanel-Request-Replayed: 1`; nothing runs |
| Same identity with another body, path, query or user | `409 REQUEST_ID_REUSED`; nothing ran |
| First one still running after a wait of 20 seconds | `409 REQUEST_IN_PROGRESS`; not started again |
| The Panel restarted or failed while the first one ran | `409 REQUEST_OUTCOME_UNKNOWN`; never run again under that identity |
| First one finished, its answer is not kept | `409 REQUEST_COMPLETED_RESULT_NOT_RETAINED` (`reason` `failed` when the first ended with an error) |

An answer is not kept when it carries a one-time secret (the database account
and VPN device routes always; a database route when it returns a password it
generated) or is larger than 64 KiB. An identity is remembered for 24 hours. A
request body over 1 MiB is refused with `413`. A second restore of a domain
while one runs is refused with `409 BACKUP_RESTORE_IN_PROGRESS`. Every other
route behaves as before, with or without the header.

### cPanel import

- `POST /api/v1/import/cpanel/inspect`: each entry of `mail_accounts` has
  `has_password`; `crypt_hash` is no longer returned.
- `POST /api/v1/import/cpanel/apply` answers `200` with `status: "active"`, or
  `200` with `status: "partial"`, `code: "IMPORT_PARTIAL"` and `message`. It no
  longer answers `202` with `status: "pending"`. Both carry `domain_status`,
  `steps` and three lists, and every part is in exactly one of them:
  - `imported`;
  - `not_imported`: the step failed (`ok: false`); only this makes an import
    partial;
  - `left_out`: the step ended without an error and imported nothing. Such a
    step has `ok: true` and a `state`: `left_to_owner` (DNS of a server whose
    DNS is at an external provider), `not_chosen`, `none_in_archive` or
    `none_imported`.
- An archive entry named with an absolute path is a step `member:<name>` with
  `ok: false`; beyond 20 such entries one step `members:<n>` counts the rest.
  When only such entries are missing, the import is `partial` with
  `domain_status: "active"`; otherwise a partial import leaves
  `domain_status: "pending"`.
- When the site could not be created, nothing of the archive was imported:
  `502 SITE_WEB_SERVER_REFUSED` (`reason` `import_removed` or
  `import_cleanup_unconfirmed`) when nginx refused the site's configuration,
  `502 IMPORT_SITE_NOT_CREATED` otherwise.

### Sites

- `POST /api/v1/domains/create`: when nginx refuses the new site's
  configuration, `502 SITE_WEB_SERVER_REFUSED` in place of `500`, with `reason`
  `removed` (what was created for the site is gone again, confirmed) or
  `cleanup_unconfirmed`, `vars.domain`, `vars.command` and, for an
  administrator, `details` with nginx's line.
- `POST /api/v1/domains/create`: a named PHP version the server does not run
  is `409 PHP_VERSION_NOT_INSTALLED`, before anything is created.
- `GET /api/v1/domains`: `php_version` is the PHP version recorded for the
  site, and empty for a domain without a site (it used to answer the literal
  `8.3` there).

### Service actions

`POST /api/v1/service/action`. A success from Postfix, Dovecot and the units
that only wrap a service (Ubuntu's `postfix`; `postgresql` on Debian and
Ubuntu) gains `outcome` and
`applied`, and `unit` when one unit stands behind the answer; other services
answer success as before. A failed action used to answer `500`; now:

- `502 SERVICE_ACTION_FAILED` with `reason` `check`, `reload`, `start`, `stop`,
  `verify`, `command`, `reload_reread` or `reload_not_reread` (the last two for
  PostgreSQL only);
- `409 SERVICE_ACTION_FAILED` with `reason` `not_running`: a reload of a
  service whose unit is stopped or failed, read before anything is sent;
  nothing was sent to it;
- `502 SERVICE_ACTION_UNKNOWN`: sent, result not established.

All carry `vars.unit`, `vars.action`, `vars.command`, `vars.detail` when the
service said anything, and `vars.owner_unit` when another unit runs the
service.

A successful Stop may carry `note` (`code` `SERVICE_ACTION_NOTE`):

- `reason` `unit_marked_failed` or `unit_marked_failed_config`: systemd shows
  the unit as failed after the stop. `vars`: `unit`, `failed_unit`, `result`,
  `command` and, for the second reason, `detail` (the service's own line).
- `reason` `unit_not_settled` or `unit_state_not_read`: the unit was still
  between two states, or could not be read. `vars`: `unit`, `pending_unit`,
  `command`, and `state` for the first.

A Stop of Postfix is answered when its main process has ended, and a Stop can
take up to about 15 seconds longer while a unit is between two states.

### Certificates and databases

- `POST /api/v1/domains/{id}/ssl/letsencrypt`: a request certbot ran and did
  not fulfil is `502 CERTIFICATE_ISSUE_FAILED` with `reason`
  `authority_unreachable`, `validation`, `rate_limited`, `timeout` or `tool`;
  `vars.kept` is `none` or `previous`; `vars.detail` (certbot's line) is sent
  to administrators only.
- `POST /api/v1/database-servers/{id}/databases` and
  `POST /api/v1/database-servers/{id}/users`: a password the caller sent is not
  sent back; the answer has `password_set: true`. A password the server
  generated is still returned once, as `password`. (The `users` answer carries
  `password_set: true` in both cases.)

Which of these answers were produced on a real system and which have component
tests only is said in the next two sections and in the limits.

## Behaviour changes you will notice

- A stopped Postfix is not started by a mail policy save. The values are
  written and the answer says Postfix is not running.
- Saving the mail policy without a change reloads a running Postfix again, so
  a save after "saved but not reloaded" applies it.
- A MariaDB setting waits for a restart you make.
- On the Components page, Start and Stop are offered only when the component's
  own record names its service unit, and Install only once the package
  repository answer is known.
- Reload of a stopped service is refused and starts nothing.
- Stopping Postfix works when its configuration is refused. The answer comes
  when Postfix has stopped and carries a note when systemd marks the unit as
  failed. It is later than before: on Ubuntu 24.04 the request took 1.9 to
  3.3 seconds before the correction and 4.2 to 5.8 seconds after it (five
  Stops and four, on different machines of the lab).
- A server setup that is only at its public-address check (waiting until the
  panel's name resolves to the server) no longer refuses unrelated service
  actions; a setup waiting at its other DNS steps still does, by design.
- The update rewrites hosted sites' web server configuration when the new
  Panel starts.
- The DNS engine card only reads on its timer. When an operation has not been
  confirmed for a while it offers "Check now", which you start.
- After a panel certificate is issued, the page moves to the secure address
  only in the tab that asked, after a notice with "Stay here".
- After a lost answer the page sends nothing again. It reads the state and
  keeps its controls off until that read answers.
- On a phone, a row's actions stay reachable while the other columns scroll
  (DNS records, domains, databases, banned addresses).
- The Databases page shows the MariaDB version the server reports.
- A scheduled task that is disabled can be enabled, edited and deleted;
  deleting a task no longer removes the line above it.

The items about Postfix, Dovecot, MariaDB, stopped services, the rewritten
site configuration, the setup that waits for DNS and scheduled tasks were
measured on real services (next section). The items about what a screen shows
were checked in component tests and in a browser against a mock of the API
only.

## What was measured, and how

Seven records, made on 2026-10-08 and 2026-10-09. Five are runs of the product
on disposable virtual machines on one laptop host, with the packaged services
of Debian 13, Ubuntu 24.04 and Arch; one is a follow-up of the fourth on fresh
machines; one is a reading of a single program. Each sequence ran once per
platform unless a number is given; a cell was repeated only after a fault of
the test harness, except where a paragraph below says otherwise (a second
Arch reading in the third run; a cell with the candidate source as the running
version in the fourth). No screen was rendered in these runs: the driver sent
what the screens send. Every result file marks itself as not yet accepted
evidence; the owner judges it.

**What the lab does and does not establish.** The virtual machines are not cut
off from the network: they have outbound access, and the distributions'
packages are installed from their repositories during a run. What is
established is narrower. The release origin is pinned to the machine's own
loopback in its hosts file. In the first run no Let's Encrypt name was pinned
and no certificate route was called. The two Let's Encrypt names are pinned
the same way before the certificate route is called (second and third run); in
the fourth run and its follow-up they were pinned only at a step after install
and setup in the fresh-install cells and not in the update cells, and no
certificate route was called there; in the closing run the four certificate authority names the
product knows were pinned in the first step of every cell, and no certificate
route was called. The license step is answered by a test build without contacting
a license service. No driver was pointed at an installed server, and no
installed server's name or address appears in the raw files. The machines'
traffic was not recorded. The builds carry a test signing key, not the
production one.

**Names and dates.** The directories are under
`deploy/e2e/release-recovery/evidence/`. `set1-20261010`, `set2-20261011` and
`set3-20261012` carry labels that are not the days of the runs: the first ran
on 2026-10-08, the other two on 2026-10-09. `set4-20261009`, `set4b-20261009`,
`set4c-20261009` and `set5-20261009` carry the real date.

**Evidence handling.** The directories are stored in the repository byte for
byte, and twelve host-reading files of earlier directories were stored again
as they were collected. Two values were redacted after collection. In
`set2-20261011`, 30 mailbox password hashes that the import preview answered
back were replaced; the lab's import fixture had made them, and they are the
hash of no password. In `set4-20261009` the digest of a one-shot
update-transaction token was replaced; `set3-20261012`, which was already
retained, carries the same field unredacted. It is a lab token of a machine that
no longer exists. At intake, three host-reading files of `set4b-20261009` were
converted from CRLF to LF before their checksums were recorded, and the local
scratch-folder path in some scripts of `set4-20261009` and `set4b-20261009`
was replaced by a placeholder. From the third run on, the drivers remove
hash-shaped values when they collect, and the closing run also removes token
digests at that point.

**Settings writes (first run; Debian 13, Ubuntu 24.04, Arch; the mail policy
and the catch-all on Debian 13 and Ubuntu 24.04 only).** A user without a
crontab is a known empty list on all three. Hand-written crontab lines stayed
in place through add, disable, edit, enable and delete. A save with a stale
version was refused for every resource, with nothing changed. A save without a
version was tried and refused for the backup schedule and the catch-all here,
and for scheduled tasks and the mail policy in the update cells below; for a
configuration file it has component tests only. A PostgreSQL setting changed
one line, left a dated copy and took effect without a restart; an invalid
value, empty content and a `pg_hba` without local access were refused. MariaDB
was not restarted. Owner, group and mode of every written file were unchanged.
On Debian 13 and Ubuntu 24.04 the owner's Postfix restrictions kept their
elements and order. The run found two defects: on Ubuntu 24.04 a policy save
answered success although Postfix had not reloaded, and, on all three, after a
database configuration reload that failed twice (caused in the lab by an
owner's own unit drop-in) the answer said the previous file could not be put
back although it had been.

**Corrections, service actions, request identity (second run).** On Debian 13
and Ubuntu 24.04 both defects were measured as corrected: the Ubuntu save now
answers `MAIL_POLICY_NOT_RELOADED`, and the failed reload answers that the
previous file is back, with the file identical to the previous one. A stopped
Postfix stayed stopped after a save. Of 41 service actions per platform on
nginx, MariaDB, Dovecot, Postfix and PostgreSQL, 40 answers matched what the
service showed and one was answered as unknown. On Debian 13, Ubuntu 24.04 and
Arch, each guarded change sent three times in a row and three times at once
had one effect on the server (for the certificate route that means the handler
ran once and failed, because the lab has no certificate authority); without the
header it answered `428`, and with another body `409 REQUEST_ID_REUSED`, with
nothing changed. On Arch this did not include the import, which failed there
at that time (below). A restore
whose connection was dropped finished on the server and its repeat got the
stored answer. A Panel restarted during a restore let the request finish and
the client received its answer. A Panel that was killed left the request
marked as interrupted, and its repeat answered `REQUEST_OUTCOME_UNKNOWN`; the
Agent had finished the restore on all three platforms. None of the 21 secrets
the run had shown or sent was found in a stored row.

**Second corrections (third run; Debian 13 and Ubuntu 24.04).** All 42 service
actions per platform matched the service and none was unknown, including the
reload of a stopped Postfix and Dovecot, Postfix Stop with a refused
configuration and both PostgreSQL reload reasons. No refusal occurred while
setup waited for DNS. No hash-shaped value was found in an import preview, a
guarded answer, a stored row or the Panel's and Agent's log lines; this also
held on Arch. An imported mailbox accepted its original password and refused a
wrong one. An archive with a directory entry imported completely, and a failed
files step was answered as partial with lists that matched the server. A PHP
site was created, executed PHP and was deleted. A certificate request answered
`CERTIFICATE_ISSUE_FAILED` / `authority_unreachable`, also on Arch; in these
cells the Let's Encrypt names pointed at the machine itself. The MariaDB version shown
was the server's on all three platforms. The database password check passed on
Debian 13 and Arch; on Ubuntu 24.04 two of its checks failed for a fault of the
test harness, Ubuntu was not run again, and the recorded answers were the
expected ones. On Arch a PHP site could not be created in this run, so no
cPanel import started there; the published v0.1.0-alpha.81 cannot create one
either (read from its source). A second Arch reading, on a new machine where
the owner placed by hand the one nginx file whose absence stopped the site
creation, passed every section; it shows what the cause was, not how the
product behaves on a stock Arch machine.

**The last corrections on fresh servers (fourth run, `set4-20261009`; Arch,
Debian 13, Ubuntu 24.04; twelve cells).**

- A PHP site was created through the Panel on a stock server of all three
  platforms, Arch included. A PHP page ran as the site's own account, a missing
  script answered 404, and after the site was deleted nothing of it kept
  serving. The recorded PHP version and socket were the installed version's.
- A cPanel import completed on all three and the imported site served the
  archive's page.
- Reload of a stopped nginx, MariaDB and PostgreSQL answered `409` /
  `not_running` on all three, and nothing was sent to the unit.
- A site the web server refused answered `502 SITE_WEB_SERVER_REFUSED` /
  `removed` on all three, in two readings each (in one the owner had moved an
  nginx file the generated configuration includes; in the other the site name
  had 58 characters), and what the answer says was removed was gone on the
  server.
- An archive entry with an absolute name was listed, the import was `partial`,
  the rest was imported and the domain was in service, on all three.
- A PHP site created by the published v0.1.0-alpha.81, on Debian 13 and
  Ubuntu 24.04: ten requests gave the same status and the same body before the
  update, after it and after the site's settings were saved again. The update
  itself had rewritten the site's web server configuration when the new Panel
  started.
- Postfix Stop with a configuration Postfix refuses: on Debian 13 the answer
  carried the note. On Ubuntu 24.04 it did not, in two runs, although the unit
  ended as failed. This was the one check of the run that did not pass.
- After an automatic return on Arch (two cells that returned to the published
  v0.1.0-alpha.81, one that returned to a build of the candidate itself) the
  update check still offered the version, with the earlier attempt recorded.

**Postfix Stop on Ubuntu, cause and correction (follow-up, `set4b-20261009`).**
On Ubuntu 24.04, before the correction, five Stops out of five were answered
without the note. The traces show why: `postconf` printed a warning about the
refused line together with the queue directory, and the Agent took both as one
path. It found no process file under that name, took Postfix as stopped at
its first look, and read the unit about 0.9 seconds before Postfix had
stopped, while systemd still showed it as stopping. The answer itself was sent
later, 1.9 to 3.3 seconds after the request. After the correction, on a fresh
Ubuntu 24.04 and a fresh Debian 13 machine, four Stops each answered `200`
with the note naming the failed unit; nothing cleared the mark; Start worked
once the configuration was restored. On both, an import whose DNS stays at an
external provider listed `dns` under `left_out`. The build measured here was
made from the corrections before they were committed; the record shows its
product files are identical to the committed ones.

**What `postconf` prints (reading, `set4c-20261009`).** Not a run of the
product. The real `postconf` of Postfix 3.10.13 was run against a private
configuration directory on a development machine, not a disposable one; the
system's own configuration was neither read nor changed. It showed on which
output the warning and the value are written, and what the command of the old
rollback does with the text the old code kept (the result is in the owner
section above).

**Update from the published v0.1.0-alpha.81 (third run; ten cells, all reached
their expected end).** The baseline was the tag's own source, unchanged, built
in the lab with the test license; it was not the signed archive. The candidate
was this release as it stood before the last corrections.

- A good update was verified on Debian 13, Ubuntu 24.04 and Arch. Afterwards
  the database schema was the new one; a request sent as a page opened before
  the update sends it was refused (`428`, `409 SETTINGS_VERSION_REQUIRED`) and
  changed nothing; the same requests with the header and the version worked.
  On Debian and Ubuntu the Panel's deferred mail work completed.
- A candidate whose migration fails, with a second fault during recovery (a
  machine reset on Debian and Ubuntu, a killed recovery process on Arch),
  returned to v0.1.0-alpha.81 automatically. The schema was the previous one
  and 65 tables equalled their state before the update, apart from two that
  change by themselves. In these cells the candidate migrated a copy of the
  database and failed before it was published, so the live database was never
  migrated. The alpha.81 recovery status, its root command and the text of its
  update card (generated from that release's own rules, not seen in a browser)
  all said the previous version was restored.
- A candidate that fails its start check returned the same way (Debian 13
  only). Here the updater had published the migrated database before the check
  failed, and it was the previous database that served after the return; that
  the live database stood at the new schema in between follows from the
  updater's order, because the live ledger was not read then.
- An update paused after three attempts was completed by the one retry command
  the product prints for the owner, run by the test driver (Debian 13,
  Ubuntu 24.04). The pause was caused by a lab process holding the Panel's
  port, which the driver released before the retry.
- With Panel and Agent disabled across a reboot, the site, the database and
  SMTP were served, cron ran and the firewall rules were present (Debian 13
  only).

No incompatibility with v0.1.0-alpha.81 was found in the measured paths of
that run. The fourth run repeated the good update on Debian 13 and
Ubuntu 24.04 and the automatic return on Arch, on the code of the fourth run.

**The update again, on the final code (closing run, `set5-20261009`; the same
ten cells).** The candidate was built from the last change to the product's
code; what was added to the branch afterwards is evidence and documents.

- All ten cells reached the end the third run had measured, with the same
  verdict for every step, the same outcome and the same compared facts
  (`compare-with-set3.md`): update verified on Debian 13, Ubuntu 24.04 and
  Arch; automatic return to v0.1.0-alpha.81 on all three for a candidate whose
  migration fails, with a second fault; return after a failed start check
  (Debian 13); the one printed retry, run by the test driver, completing a
  paused update (Debian 13, Ubuntu 24.04); management off across a reboot
  (Debian 13). No step and no check failed.
- A PHP site created by v0.1.0-alpha.81 answered ten requests with the same
  status and the same body before the update, after it and after its settings
  were saved again (Debian 13, Ubuntu 24.04).
- One Postfix Stop per platform (Debian 13, Ubuntu 24.04), with a
  configuration Postfix refuses, on a server that had just been updated, not
  on a fresh install: `200` with the note naming the failed unit (on Ubuntu
  the unit that runs the service, `postfix@-.service`); nothing cleared the
  mark; Start worked once the configuration was restored.
- How this run differs from the earlier ones: the machines' disks were kept in
  memory and their base images were shared, so no duration of this run can be
  compared with an earlier one and nothing about a slow or failing disk was
  measured. The Windows host logged modern standby for all but about sixteen
  minutes of the run; the run's own clocks show no pause (no gap between two
  samples longer than 10.5 seconds, the longest at a cell's own machine
  reset). Each cell ran once.
- Not exercised by this run: the corrected reading of mail TLS settings
  (snapshot, restore, read-back), and a fresh install of the final code.

**Which check rests on which code.** Each later state contains the earlier
corrections. A check was not repeated on later code unless a later row says
so.

| What was checked | The code it ran on |
| --- | --- |
| Settings writes on real services | the code of the first run |
| Corrected settings writes; 41 service actions; request identity | the code of the second run |
| 42 service actions; import preview and partial import; certificate failure answer; first update matrix | the code of the third run |
| PHP sites on fresh servers, Arch included; reload of stopped services; refused site; absolute archive entry | the code of the fourth run |
| Postfix Stop note on fresh servers, four Stops each; the import's `left_out` list | the code of the fourth run with the Postfix Stop correction (follow-up) |
| Update matrix, ten cells; the alpha.81 site across the update; one Postfix Stop on an updated server | the final code (closing run) |
| Reading of mail TLS settings behind a `postconf` warning | the final code: component tests and one reading of the program; not run on a server |
| A `mysqld` that is not MariaDB | the final code: component tests only |

A fresh install was last measured on the code of the follow-up. The final
code, which adds the `postconf` rule to it, was installed only by an update
from v0.1.0-alpha.81.

**Screens.** The interface changes have component tests and were inspected in
an installed Chrome against a loopback mock of the API (desktop and phone,
English and Turkish, light and dark). No real Panel was behind that browser.

**Tests.** Test logs are retained for the code with the Postfix Stop
correction and with the `postconf` correction as it stood before it was
committed (`set4b-20261009/verification/`, `set4c-20261009/verification/`).
With both, on the development machine: Panel and shared packages 6829 passed,
none failed; interface 1245 of 1245 passed; Agent package 4481 passed, 94
failed, 29 skipped. The failing set is the same, name by name, as before these
corrections. The change record gives the machine's environment as the cause of
those 94; the retained logs do not establish the cause.

## Limits of this release

These are known. They are not hidden defects.

- **The final code** was measured only through the update from
  v0.1.0-alpha.81, once per cell, on machines whose disks were in memory. It
  was not installed fresh, and the checks of the earlier runs were not
  repeated on it except as the table above says.
- **Your own edits of a generated site configuration file.** When the Panel
  starts, after an update and otherwise, it writes the web server
  configuration of hosted sites again; the resilience contract records that
  earlier releases do the same. What happens to a change you made by hand in
  such a file was not measured and this path has not been audited. The
  product's rule is to detect an owner's change, not to overwrite it
  silently; until the audit is done, do not rely on hand edits in those files
  surviving a Panel start.
- **Measured platforms.** Settings-write corrections and the full set of
  service actions: Debian 13 and Ubuntu 24.04 only; on Arch only the reload of
  a stopped nginx, MariaDB and PostgreSQL was run. Request identity (on Arch,
  not the import in the second run) and the update: Debian 13, Ubuntu 24.04 and
  Arch. The RHEL family remains a blocked preview. Mail on Arch is not
  supported.
- **PHP on Arch.** Creating, running, deleting and importing a PHP site were
  measured. Listed and not fixed, and not measured: PHP extension directories,
  the MariaDB settings file path, the PHP version detection used by webmail
  setup, and a change of a static site to PHP on Arch. A site created by
  v0.1.0-alpha.81 could not be compared on Arch, because that release cannot
  create a PHP site there.
- **A long site name.** A PHP site named with 58 characters was refused by the
  stock nginx of all three platforms ("could not build server_names_hash").
  The answer points to `sudo nginx -t`, which passes, and creating the site
  again gives the same answer. The longest name that works was not measured,
  and raising the nginx setting its line names was not tried.
- **PATH_INFO.** Extra path after a script reaches PHP only when the request
  path itself ends in `.php`; another path is handed to the site's
  `index.php`. v0.1.0-alpha.81 behaves the same.
- **A directory that stays.** The directory the Agent prepares for certificate
  validation of a domain is still there after a refused site creation and
  after a site is deleted. No answer claims it was removed.
- **An edited PHP snippet file** is not detected (owner section). The update
  was not measured with an edited file, nor with a site that has a certificate.
- **A refusal by nginx during a change of hosting type, certificate or site
  settings** still answers its earlier error; only site creation and the import
  have the new answer.
- **Answers not produced on a real system:** `SITE_WEB_SERVER_REFUSED` with
  `cleanup_unconfirmed`, `import_removed` or `import_cleanup_unconfirmed`;
  `PHP_VERSION_NOT_INSTALLED`; an empty `php_version` for a domain without a
  site; a reload of a unit in the failed state and of PHP-FPM; more than 20
  left-out archive entries; the import states `not_chosen`, `none_in_archive`
  and `none_imported`; the Stop notes `unit_not_settled` and
  `unit_state_not_read`; `CONFIG_RELOAD_FAILED` with `restored` or
  `restored_running_unknown`; `MAIL_POLICY_RELOAD_UNKNOWN`;
  `MAIL_POLICY_NOT_RELOADED` with `reload` or `verify`; `REQUEST_IN_PROGRESS`;
  `CONFIG_INVALID` / `no_validator` for a `mysqld` that is not MariaDB;
  `CONFIG_INVALID` / `shape`; `MAIL_POLICY_INVALID`;
  `MAIL_POLICY_RESTRICTIONS_UNMANAGED` with a reason other than `variable`;
  `CRON_JOB_AMBIGUOUS`; the scheduled-tasks cause `cron_deny`;
  `SERVICE_ACTION_FAILED` with `stop` or `verify`; `400 REQUEST_ID_REQUIRED`
  for a malformed header; `413` for an oversized body;
  `REQUEST_COMPLETED_RESULT_NOT_RETAINED` with `reason` `failed`.
- **Mail settings behind a `postconf` warning.** The correction has component
  tests and one reading of the real program (Postfix 3.10.13, Debian 13). A
  mail TLS change, its rollback and a certificate publication with such a
  configuration were not run, in the closing run either. Ubuntu's Postfix 3.8 and Arch were not read for
  these settings. The statements about v0.1.0-alpha.81 beyond the two measured
  commands are read from its source. A renewal helper already installed on a
  server is not replaced by a panel update (change record), so this correction
  does not reach it.
- **Postfix Stop.** Not measured: a Stop with a configuration that makes
  `postconf` warn while `postfix check` accepts it; Arch (no Postfix);
  Dovecot; a Stop on a fresh install of the final code (the four Stops per
  platform ran on the code of the follow-up, the one on the final code on an
  updated server). systemd's failed mark is left; `sudo systemctl reset-failed <unit>`,
  which the note names, clears it.
- **The update card.** Its new text was not seen in a browser against a real
  Panel. After a return to v0.1.0-alpha.81 the interface that is served is
  alpha.81's, so the new text cannot appear there. The update check still
  offers the same version after an automatic return.
- **Screens not yet converted.** 31 source files of the interface still read
  the server the old way in at least one place: the access gates and the sign-in
  page, the setup wizard, the update and component operation overlays, the
  Services list and service pages, the sidebar, parts of the dashboard, add-ons,
  the audit log, VPN, team members and a few smaller parts. On these a failed
  read can still look like an empty or negative state.
- **Routes without a request identity.** Only the eight routes above are
  protected. The inventory behind them classed service and application
  restart, plans and enrollment codes as harmful when repeated, about 38 other
  routes as leaving the state right but reporting a repeat wrongly, and seven
  as not classified; none of these is protected yet. The same holds for DNS
  records, hosting type, PHP settings, aliases, application install, mail
  authentication, accounts and files. After a lost answer on these the page
  reads again and leaves the result to you.
- **Request identity.** The 24-hour expiry did not occur in a test. After a
  killed Panel the answer is "outcome unknown" even when the Agent finished
  the work; nothing reconciles the two. A one-time result (a generated
  password, a VPN device file) that did not arrive cannot be shown again. The
  Panel has no control that sets a database user's password; that is done on
  the database server.
- **Installed renewal helpers are not upgraded by a panel update.** A server
  set up for independent mail renewal by an earlier release gets the new
  certificate check only when the updated Agent handles the renewal, and not
  while the Agent is absent. Until that is built, the owner of such an Ubuntu
  server can compare after a renewal:
  `openssl s_client -connect localhost:465 </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256`
  with
  `openssl x509 -noout -fingerprint -sha256 -in /etc/ssl/celikpanel/_mail/host/current/fullchain.pem`,
  and run `sudo postfix reload` when they differ. This comparison was not run
  in a test.
- **The new certificate check of mail renewal was not measured on a real
  system.** It has component tests only.
- **Postfix restriction layout.** A list you wrote one restriction per line
  becomes one line after a DNSBL save; elements and order are kept. A list the
  Panel cannot split into its part and yours is not rewritten: the DNSBL change
  is refused, size and rate still save.
- **Scheduled tasks under `/etc/cron.allow`.** On Debian and Ubuntu, when that
  file exists without the site user, the Panel cannot read or change that
  user's tasks and says so.
- **Settings.** No scheduled backup ran, so pruning by retention was not
  exercised. It was not checked that a saved MariaDB value takes effect at the
  next restart.
- **Service actions.** The two PostgreSQL reload reasons were produced only
  with two prepared reload hooks. Dovecot or nginx with a refused
  configuration, a second PostgreSQL cluster and WireGuard units were not run.
  Reload is offered for MariaDB, which has none, and answers a failure.
- **cPanel import.** No real cPanel backup was imported; the archives were
  built by the lab. Mailbox contents are not migrated. Only one password hash
  scheme was tried, and a mailbox without a password was not built. An entry
  with `..`, a link or a device below the site folder still refuses the whole
  files step. The import page has no list of the parts that were left out, and
  step detail lines are in English.
- **Certificates.** No certificate was issued in any run: where the
  certificate route was called, the Let's Encrypt names pointed at the machine
  itself, and of the five failure reasons only `authority_unreachable` was
  produced.
- **Mailbox login check.** The imported mailbox logged in over IMAP on both
  platforms. Opening the inbox afterwards was recorded as successful on
  Ubuntu and as not done on Debian, without an error; it was not a checked
  condition.
- **The update.** From the tag's source, not the signed archive. One run per
  cell in each of the two matrices. Start check and management-off on Debian only;
  owner continuation not on Arch. The Arch cells ran without mail. No power
  loss. A page open during the update was imitated by sending what it sends. A direct update from v0.1.0-alpha.80
  to this release was not measured.
- **Evidence scope.** One laptop host, a test license, a loopback release
  origin, a test signing key; machines with outbound network access whose
  traffic was not recorded. The host entered standby once during the third
  run, before any cell had started, and logged two more standby events while
  cells ran and kept executing through them (`sampler-gaps.txt` shows no gap
  beyond each cell's own reset or reboot). It slept for 43 minutes during the
  Ubuntu cell of the follow-up, before the corrected build was installed; the
  measured steps ran after it woke. In the fourth run power events were not
  collected; the 30-second disk watcher shows no gap. In the closing run the
  host logged modern standby for most of the run and the samplers show no
  pause; what that state changes for a running workload other than pausing it
  was not measured.
- **Checks that did not pass** are listed in each directory's
  `checks-not-passed.txt`. First run: the two defects above and one harness
  fault. Second run: harness faults (the earlier attempts, and two checks of
  the certificate route on Arch), the unknown answer for Postfix Stop, the
  reload wording, the directory entry of the import and the PHP pool path on
  Arch; the product faults among these were corrected afterwards. Third run:
  two harness-fault checks each on Debian 13 (passed when run again) and
  Ubuntu 24.04 (not run again), and 14 checks on Arch that all trace to the
  PHP site defect corrected since. Fourth run: the three checks of the Postfix
  Stop note on Ubuntu 24.04, in both runs, corrected and measured again in the
  follow-up; and one harness rule in the first Debian update cell, which
  passed when run again. Follow-up: its first diagnostic attempt stopped at a
  fault of the driver before the first Stop. Closing run: none.
- **Dates in the documents.** Several dated entries in the contract and
  guidance documents and in source comments carry 2026-10-10, 2026-10-11 or
  2026-10-12. They are labels of work rounds, not dates; the work was done on
  2026-10-08 and 2026-10-09. Each document says so at its top.
- **Earlier limits.** The limits of
  [v0.1.0-alpha.81](RELEASE-NOTES-v0.1.0-alpha.81.md) that this release does
  not address still apply. One does not apply to an update from alpha.81: the
  recovery command and the Panel's recovery status existed from the first
  second of the measured updates.
- **Open acceptance items.** All five foundation items of the
  [resilience contract](RESILIENCE-CONTRACT.md) remain partial; this release
  closes none. Decisions: D-022, D-024, D-025, D-029 in
  [DECISIONS](DECISIONS.md).

## Before publication (owner decisions and remaining checks)

1. Version: v0.1.0-alpha.82 is the working number (owner decision).
2. Decide whether a fresh install of the final code and the mail certificate
   path behind a `postconf` warning are measured before publication, or go out
   as the limits named above.
3. The packaging contract tests run in CI on the pull request, the root ones
   under `sudo` (`.github/workflows/ci.yml`). On draft pull request #205 the
   run that started at 16:40 UTC and ended at 17:01 UTC on 2026-10-09 passed
   for the branch head of that time, and so did the run that started at
   20:20 UTC and ended at 20:40 UTC for the head of that time (21 checks
   passed in each; the publication step is skipped on a pull request). Both
   heads hold every correction named here. A commit that adds the closing run's
   evidence followed, and a run for it had started but not finished when this
   was written. Each later head needs its own run, and a passing run is not
   the owner's test of item 5.
4. Production signing happens in CI on the release tag, as the
   [signed release contract](release-signing.md) describes. The owner then
   verifies the published assets as that document says.
5. Owner test on a disposable server before any installed panel is updated.

Install this release only through CelikPanel's update interface. Publishing it
does not update installed servers; only the owner of a server starts its update.
