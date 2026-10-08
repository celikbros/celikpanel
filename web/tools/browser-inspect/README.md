# Browser inspection of access and setup states

A development tool. It opens the built interface in a real, installed Chrome
against a mock of the Panel API, walks through the states that component tests
cannot show, and saves a screenshot and a text record of each one. A person (or
an assistant) then looks at the screenshots.

It was written on 2026-10-08, after two interface changes had passed their
component tests and still showed eight defects on screen: text broken inside a
word, a notice below the fold, two stacked scrims, a failure colour on a state
that was not a failure. None of those is visible to a test that renders React to
a tree.

## What it never does

- It contacts nothing but `127.0.0.1`. `mock.mjs` listens on the loopback
  address only, and `run.mjs` opens only that address.
- It does not contact an installed server, celikpanel.net or a license service,
  and it holds no credentials. Host names such as `panel.example.com` are text
  on the page; nothing resolves or opens them.
- It starts no update, setup or other change anywhere. Every "server" answer is
  a constant in `mock.mjs`.

## What it needs

- Node.js (the version the web build uses).
- A build of the interface: `npm run build` in `web/`, which writes `web/dist`.
- An installed Chrome or Chromium. Set `CHROME_PATH` if it is not in a usual place.
- `puppeteer-core`, installed **outside the repository**:

  ```
  npm install --prefix ~/.celikpanel-browser-inspect puppeteer-core
  ```

  It is deliberately not a dependency of `web/package.json`. The release build
  runs `npm ci` on exactly what that file and its lock name, and its result is
  signed and reproducible; a browser driver that no build step and no shipped
  file uses does not belong in that set. `puppeteer-core` downloads no browser.

## How to run it

From the repository root, after `npm run build` in `web/`:

```
export BROWSER_INSPECT_MODULES=~/.celikpanel-browser-inspect
export BROWSER_INSPECT_OUT=/tmp/celikpanel-shots        # default: the system temp directory
node web/tools/browser-inspect/run.mjs desktop en light 4801
node web/tools/browser-inspect/run.mjs phone tr dark 4802 setup,busy
```

Arguments: viewport (`desktop` 1440×900 or `phone` 390×844), language (`en` or
`tr`), theme (`light` or `dark`), a free loopback port, and optionally a
comma-separated list of scenarios. Each run starts its own mock on that port, so
several runs can go side by side on different ports. The whole set for one
configuration takes about eight minutes; `hold` alone is four and a half of
them, because it waits twice for a real access decision to run out in a hidden
tab.

`BROWSER_INSPECT_DIST` serves another build than `web/dist`.

The scenarios from `twofactor` on never let the browser leave the loopback
address: `panelcert` watches for the one move the product makes on purpose (to
the Panel's secure address after its certificate was issued), records where it
was going and stops it before anything is contacted. `monitoring` waits a
minute for the page's own poll, and `lookup` half a minute for a capability
answer to age, so the whole second set takes about six minutes per
configuration.

Output: `<out>/<viewport>-<theme>-<language>/<state>.png` and a `report*.json`
with, for every state, the visible text, the open dialogues, what has focus,
horizontal overflow and clipped text, plus the measurements a scenario adds
(where the notice is relative to the fold, which text is drawn in the failure
colour, which layer is on top, how many scrims darken the page, where the
browser broke an address). The scenarios from `adddomain` on add, for every
state, which negative sentences were on screen (`negativeText`), which checking
lines and notices were, which buttons were disabled, and what changed place or
size when the answer arrived (`moved`).

## Scenarios

| Name | States |
| --- | --- |
| `setup` | review and progress with the planned-restart notice, the restart itself, the hold over the wizard, the readiness page, continuation, completion (01, 02) |
| `otherdrop` | a lost connection at another step keeps the unknown-result wording (03) |
| `busy` | a step refused because the server was busy, for each typed reason and for none (04) |
| `stops` | the same lead area for a verified step failure, an unmet prerequisite, a license wait, a run in progress and a result being confirmed (04b–04g) |
| `hold` | the access hold over a dialogue with unsent input: quiet return, failed read, keyboard and pointer, the reload offer, a slow read, a dropped connection, a visible refusal, then the reload dialogue (05, 10) |
| `overlays` | the hold over the component-operation overlay and over the update lock, and the reload dialogue over the hold (11–13) |
| `address` | a long secure address in the wizard, at the restart, in the hold and on the readiness page (14) |
| `negative` | a known missing or expired license still takes the whole screen (06) |
| `firstload` | first load with a slow, failed or dropped session, readiness and license read (07) |
| `session` | the session ended under a page in use (08) |
| `capabilities` | reference: a dialogue opened while its data is slow (09) |
| `adddomain` | the Domains page and the Add domain dialogue while this server's capabilities are slow (frames during the read), failing (then Retry), known negative and known positive; the dialogue over a page that already has the answer (20–23) |
| `domainslist` | the domain list slow, failing (then Retry) and known empty (25) |
| `databases` | the Databases page: engines and one engine's lists slow, failing, empty and populated; a list that could not be read again after a delete; the panel's own account after it was removed (30–33) |
| `connection` | one domain's page: the connection card slow, failing, not checked by the server (`status: unknown`, every list `null`), known negative and known positive; the domain's own database list (40–41) |
| `twofactor` | Settings, two-factor sign-in: the status slow, failing (then Retry), known off and known on (50) |
| `panelcert` | Settings, the certificate the Panel serves: its state slow, failing, known, and not readable by the Panel; a request that succeeds on the page that asked (what it says before it moves, "Stay here", and the one move when left alone), in another section and in another tab (neither is moved); a poll that gets no answer; a request reported as failed (51) |
| `accounts` | Accounts: the list slow, failing, known empty and populated; the plans failing on both tabs; a list that could not be read again after a change (52) |
| `files` | a domain's files: a folder slow, failing, populated; another folder opened while it is slow (53) |
| `domainssl` | a domain's certificate: slow, failing, known none; a successful request whose re-read is slow, and one whose re-read fails (54) |
| `importer` | import: the subscriptions slow and failing; an apply that loses its connection, then the check that only reads, with the domain present and absent (55) |
| `dashboard` | the dashboard: the four counts and the navigation badge slow, failing and known; the license notice for "could not be verified", for a known expired license and for a failed read (56) |
| `monitoring` | monitoring: slow, known, a poll that fails a minute later, failing (57) |
| `lookup` | pages opened by their address: a domain whose list could not be read or that the server does not list, a component after a reload, and a domain's tabs after a later capability read failed (58) |
| `component` | one component's page: its record known, could not be read, installed without a unit; its log failing; the help drawer and a help text that cannot be fetched (59) |
| `installdialog` | the install dialogue of the components list: whether a repository is required, slow, known required and failing (60) |
| `scrim` | the page under the access hold, with the colour of the scrim and of a muted text under it (61) |

## Limits

The mock answers what these screens read and nothing more; a route it does not
know gets a 404, as the report shows. It proves nothing about a real server, a
real certificate, a real restart or real timing. It is one Chrome on one machine:
no Safari, no Firefox, no screen reader, no touch device. Looking at the
screenshots is the inspection; the script only makes them.

## Not shipped

The customer archive is assembled by `make dist`, which copies `web/dist` and
never `web/` itself, so nothing in this directory can reach an installed server.

## Scenarios of the second batch (2026-10-09)

These live in `scenarios-batch2b.mjs`, with their mock routes in
`mock-batch2b.mjs`, so two batches of scenarios can be merged without touching
each other's lines; `run.mjs` and `mock.mjs` each load them in one marked block.
A scenario can also set the whole component scan the mock answers
(`managedScan`, cleared by every reset), because a component page treats a scan
with a part missing as one it could not read. An override can carry `times`
beside `after` (apply to that many requests, then let the rest through), and
`clearAll` removes every override, whichever batch set it.

| Name | States |
| --- | --- |
| `dbconfig` | the Components list as the scan of this batch draws it (50-0); the PostgreSQL page, opened from that list with Manage, while the one read shared by the file list and the sections under it is slow and failing (then the one Retry), and known without the file; `postgresql.conf` slow, failing, changed, saved, refused because the file changed (409), refused by PostgreSQL next to the field, reload failed and restored, answer lost; `pg_hba.conf` loaded, failing, a rule marked for removal, the lockout refusal, a new rule incomplete and refused next to the rule; a MariaDB option file failing, loaded, refused next to the field, saved and waiting for a restart; the raw file loaded and failing (50–54) |
| `mailscreens` | one domain's Mail tab: the mailbox list and the webmail card slow, failing (then one Retry), known empty and known unavailable, usage that could not be read; the catch-all address slow, failing, known none, known set, refused because it changed (60–62) |
| `mailqueue` | the Postfix page: the queue slow, failing, known empty and populated; a mail policy that was written and not reloaded (70) |
| `cron` | a domain's scheduled tasks with a disabled task, and that task enabled again (80) |

Each state's record adds which negative sentences were on screen, the checking
lines and notices, how many fields existed and were enabled, the enabled and
disabled buttons, the fields marked invalid with the message tied to them, and
targets smaller than 24 px. The per-row column names of the access rules are
hidden on a wide screen on purpose (the first row names them) and are recorded
as "truncated"; that is the record, not a defect.

## Scenarios of the third batch (2026-10-09)

These live in `scenarios-batch3.mjs`, with their mock routes in
`mock-batch3.mjs` (state under `state.b3`, sent whole through `/__ctl`; without
it the defaults answer, so the scenarios of the earlier batches see a server
whose firewall is on). `run.mjs` and `mock.mjs` each load them in one marked
block. `dnsreconcile` waits 34 s and then 20 s on purpose: slow polls with
nobody touching the page.

| Name | States |
| --- | --- |
| `servicepages` | the Fail2ban page: the jails slow then known, the banned addresses failing (then Retry), a ban lifted whose re-read fails, both lists known empty; the Nginx page: global settings slow then known, TLS settings failing (then Retry), rate limits known empty; the PHP page: the extensions slow, known, failing (then Retry), a switch the server refuses, php.ini failing (then Retry); the Dovecot page: its two figures slow, known, failing (then Retry); the PowerDNS page with its file list (70–74) |
| `attention` | the dashboard's attention list: checking and known with nothing to list (what stands under it must not move), one item that arrives late and the firewall confirmation, an item already listed while another read is on its way, a firewall answer without a state (the Agent's error sent with a 200), a refused read (then Retry), component records that could not be read, and component records that are not current, measured from the checking state (80–82). The section is below the fold on a phone and is scrolled into the window before each screenshot |
| `dnssettings` | Settings → DNS: the saved settings slow, known, failing (then Retry) (83) |
| `dnsreconcile` | a DNS engine change that has recorded nothing for four minutes: what the lock offers, how many reconcile requests arrive while nobody acts (none) and after each "Check now" (one), for a check that changes nothing, one the server refuses and one that finishes the change; then a change accepted forty minutes ago, past the card's safety limit: the lock is released, nothing reads or sends by itself, and the card's notice offers the same check (84) |
| `editorheight` | the PostgreSQL page while `postgresql.conf` is read and after: where the raw files under the editor's card stand relative to the fold (85) |

Each state's record adds which negative sentences were on screen, the checking
lines and notices, the enabled and disabled buttons and switches, and, where a
scenario measures it, where the element under the changing part stood before
and after (`under`, `moved`). A place is recorded only for an element that was
found; a scenario that should measure one and finds none fails. The Fail2ban,
Nginx and PHP pages have nothing under their tabs and record no `moved`.

`mock.mjs` answers the tracker's read of the active component operation with
the envelope the Panel sends, `{"operation": null}` when nothing is running.
Until this batch it answered a bare `null`, which the tracker reads as "could
not find out"; a lock held by another screen then drew the "connection
interrupted" treatment, which a real Panel does not cause.
