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

Output: `<out>/<viewport>-<theme>-<language>/<state>.png` and a `report*.json`
with, for every state, the visible text, the open dialogues, what has focus,
horizontal overflow and clipped text, plus the measurements a scenario adds
(where the notice is relative to the fold, which text is drawn in the failure
colour, which layer is on top, how many scrims darken the page, where the
browser broke an address).

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

## Limits

The mock answers what these screens read and nothing more; a route it does not
know gets a 404, as the report shows. It proves nothing about a real server, a
real certificate, a real restart or real timing. It is one Chrome on one machine:
no Safari, no Firefox, no screen reader, no touch device. Looking at the
screenshots is the inspection; the script only makes them.

## Not shipped

The customer archive is assembled by `make dist`, which copies `web/dist` and
never `web/` itself, so nothing in this directory can reach an installed server.
