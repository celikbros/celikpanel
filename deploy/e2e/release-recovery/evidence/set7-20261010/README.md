# set7, 2026-10-10 (UTC): the CelikPanel interface in a real Chrome while the Panel restarts, and on a full page load, on disposable Debian 13 guests

A measurement record. Disposable QEMU/KVM guests of the local `archlinux` WSL host; a real Google Chrome
(154.0.8037.98, headless, on the Windows host) driven by `puppeteer-core` 24.10.0 against the real Panel of the
guest. Dates and times are UTC from the host clock (`host/progress.txt`). Nothing in this folder is a statement about
an installed server. The harness names no installed server and no address of one; it does hold the names of the
release/licence host (`celikpanel.net`) and of four certificate-authority hosts, only to pin them to the guest's
loopback. Whether any guest process contacted an outside address was not measured: the guests' traffic was not
captured (see "Network").

## Verdicts

The rule measured (from `web/src/components/AccessHold.tsx`): only a KNOWN negative decision may replace the screen;
an UNKNOWN access state holds the mounted page; the page returns where it was. For cell 5 the coordinator's rule: a
check that has not answered yet is not shown as a page; nothing is drawn until the quiet time (1.5 s,
`ACCESS_HOLD_QUIET_MS`) has passed or a read answered without confirming access.

| Cell | What | Interface | Verdict | Decisive raw file and picture |
| --- | --- | --- | --- | --- |
| 1 | Update started from Settings -> CelikPanel updates; Panel down 24 s (stopped to started; 25 s by the guest probe) | alpha.82 | **PASS** | `lab1-alpha82-interface/browser/update/samples-update.jsonl`; `.../shots/update-021-130934.png` (hold layer over the still-mounted Settings page), `.../shots/update-031-130954.jpg` (same address and section after the product's own post-update reload) |
| 2 | The same, from the published alpha.81 | alpha.81 | **the owner's report reproduced** (rule not met by alpha.81: the page is replaced) | `lab2-alpha81-interface/browser/update/samples-update.jsonl`; `.../shots/update-050-134808.png` ("License status could not be checked" full screen with "Update and recovery status") |
| 3 | Tab in the background 631 s, Panel running, typed text in a field | alpha.82 | **PASS** | `lab1-alpha82-interface/browser/hidden/`: `events.jsonl`, `samples-hidden-back.jsonl`, `shots/hidden-back-000-132244.png` |
| 4 | Tab in the background 632 s, Panel service restarted while hidden | alpha.82 | **PASS** (the restart's outage was shorter than both probes' interval, see below) | `lab1-alpha82-interface/browser/hiddenrestart/`: `events.jsonl`, `samples-hiddenrestart-back.jsonl`, `shots/hiddenrestart-back-000-133543.png` |
| 5 | Full page load of `/setup`, `/`, `/settings?section=updates`, signed in, Panel healthy, no update | alpha.82 | **FAIL on all three routes** (both network profiles) | `lab3-alpha82-interface-page-load/browser/flash/flash-loads.jsonl`, `flash-summary.txt`; `.../shots/throttled-setup-1/099-03002ms.jpg`, `.../shots/plain-setup-1/010-00190ms.jpg` |

Ubuntu 24.04: **NOT-MEASURED** (not run; the brief made it optional).

### Cell 1, alpha.82 interface (lab 1)

Logged in as the owner (`labadmin`), Settings -> CelikPanel updates, "Check for updates", then the start button of the
card at 13:08:52.055. Every 2 s a screenshot and the DOM state; the browser's full network log; a host-side probe of
the public availability route every 1 s through the same forward; a guest-side probe every 0.5 s on the guest's
loopback; the guest journal of every `celikpanel-*` unit. Full merged table: `lab1-alpha82-interface/timeline.md`.

- 13:08:52 to 13:09:16: the update window ("The update is being applied; the panel may be unavailable briefly.") over
  the Settings page.
- 13:09:14.07 the Panel is frozen (SIGSTOP), 13:09:14.85 stopped (journal); guest probe no answer from 13:09:14.33,
  host probe from 13:09:15.24.
- 13:09:17.7 / 13:09:18.0: the first failed requests (`update/status`, `license/access`: `net::ERR_CONNECTION_CLOSED`).
  13:09:18: the update window says "The connection was interrupted; the panel may be restarting. The same operation
  will be tracked." (`shots/update-020-130932.png`). Same document, Settings mounted.
- 13:09:33.0: the second failed `license/access` (and `recovery/status`). 13:09:34.2: a layer over the
  page: "Panel access could not be confirmed just now / CelikPanel could not read the license result for this server a
  moment ago. This does not mean your license is missing or expired. / You do not need to do anything yet ... this page
  continues where it was, with what you typed ..." and an "Update and recovery status" box with the operation ID
  ("The latest result could not be read ... do not start a second update"), buttons "Check this operation", "Check
  panel access". Under it the Settings page stays mounted with the updates section selected and is held
  (`data-access-hold="blocked"` read from the page; `elementFromPoint` at the centre and the corner is inside the
  layer; that the held subtree is `inert` and the layer cannot be dismissed is read from `AccessHold.tsx`, not tested
  in the page). The full
  update window is not drawn while the layer is up.
- 13:09:39.0 Panel started, 13:09:39.18 "startup listener active on :2083 (HTTPS; application gated)"; guest probe
  answers 13:09:39.38, host probe 13:09:39.49.
- 13:09:43.03 `license/access` 200; 13:09:44.3 the layer is gone, the same document, same section; the update shows as
  a card at the bottom right (`shots/update-026-130944.jpg`). 13:09:52 the card: "The update completed. The panel and
  agent restarted on the new version. ... Reloading this page with v0.1.0-alpha.82..." (`shots/update-030-130952.png`).
- 13:09:53.27 the product's own reload to `/settings?section=updates&_cp_update=<request>`, replaced by
  `/settings?section=updates`; 13:09:54 the new document shows Settings -> CelikPanel updates, current version
  v0.1.0-alpha.82, commit 2e9b30af (`shots/update-031-130954.jpg`).

The hold layer was on screen from the 13:09:34.2 sample to the 13:09:42.3 sample (absent at 13:09:44.3): about 9 to
11 s of a 24-25 s outage (it follows the second failed `license/access` read at 13:09:33.0 and ends after the first
answered read at 13:09:43.0; the 2 s sampling bounds it to 8-12 s). The only change of document was the post-update reload after the update was reported
succeeded, to the same route and section. PASS against the rule. Observation, not a verdict: the layer's first
sentence names the licence ("could not read the license result") while the cause was the Panel's own planned restart.

### Cell 2, alpha.81 interface (lab 2): the difference the owner saw

The same steps from the published v0.1.0-alpha.81 (`lab2-alpha81-interface/timeline.md`). The update window as in
cell 1 until the Panel went down (SIGSTOP 13:47:46.09; probes no answer 13:47:46.2 / 13:47:46.5). After the second
failed `license/access` (13:48:06.03) the Settings page is **unmounted** and the whole screen becomes "License status
could not be checked / The license result is unavailable. This does not establish that your license is missing or
expired. ..." with "Check panel access", "Reload CelikPanel" and the "Update and recovery status" box
(`shots/update-050-134808.png`), from the 13:48:06.3 sample to the 13:49:22.8 sample: about 78 s. The Panel answered
again at 13:49:15.0 (guest) / 13:49:15.4 (host); the replacement stayed until the next `license/access` read answered
at 13:49:24.09, then Settings was mounted again **anew** in the same document (`shots/update-088-134924.jpg`), and the
product reloaded the page at 13:49:30.4 (alpha.82 then served).

| | cell 1 (alpha.82 interface) | cell 2 (alpha.81 interface) |
| --- | --- | --- |
| update started (click) | 13:08:52.06 | 13:46:27.82 |
| Panel frozen / stopped (journal, host clock) | 13:09:14.07 / 13:09:14.85 | 13:47:46.09 / 13:47:48.47 |
| first failed request | 13:09:17.70 | 13:47:51.15 |
| update window says "connection was interrupted" | 13:09:18.2 | 13:47:58.3 |
| access read failed twice -> | 13:09:34.2 hold layer over the mounted page | 13:48:06.3 page replaced by "License status could not be checked" |
| Panel answering again (guest probe / host probe) | 13:09:39.38 / 13:09:39.49 | 13:49:14.99 / 13:49:15.41 |
| first successful access read | 13:09:43.03 | 13:49:24.09 |
| layer / replacement gone | 13:09:44.3 (same page, same section, nothing remounted) | 13:49:24.8 (Settings mounted anew) |
| product's post-update reload | 13:09:53.27 | 13:49:30.42 |
| unknown state on screen | about 9-11 s, as a layer | about 78 s, as a full-screen page |

The outages differ (25 s against 89 s by the guest probe: no answer from 13:09:14.33 to 13:09:39.38 and from 13:47:46.22
to 13:49:14.99; stopped to started it is 24 s against 86 s): cell 2's update goes from alpha.81 (schema 42) to alpha.82 (schema 43),
cell 1's from alpha.82 labelled alpha.81 to alpha.82 (schema 43 to 43). The two columns compare the interfaces'
behaviour, not the length of the updates. That what the owner saw on 2026-10-08 was the alpha.81 interface is the
coordinator's reading; this cell shows the alpha.81 interface producing exactly that screen in this lab.

### Cell 3, hidden tab, no restart (lab 1, after cell 1, Panel = the updated alpha.82 build)

Settings -> Panel HTTPS, text `set7-typed-2e7f93` typed into the "Panel domain" field (focused, not submitted). A
second tab of the same browser was brought to the front (`page.bringToFront()`; the measured page reported
`visibilityState: hidden` at once) for 631 s, from 13:12:14.2 to 13:22:44.7. While hidden the page sent **no request
at all** (`network-hidden.jsonl`: 0 events from 13:12:14.2 to 13:22:44.6). That log holds only the 8 s before hiding
(two `license/access` reads), so it shows no cadence; the update cell's log shows reads 29-36 s apart (13:09:48.4,
13:10:24.0, 13:10:53.3), and after the return they came at 13:22:47.0, 13:23:10.1 and 13:23:32.0 (22-23 s apart). DOM
read every 15 s while hidden (42 samples): same document, same address, no hold layer, typed text present; from
13:13:14 on, 60 s after hiding, 38 of the 42 samples carry `data-access-hold="blocked"` on the page subtree (the page
is held while the tab is hidden, without a layer). Back at 13:22:44.7: one
`license/access` and one `service/operation` read (200 at 13:22:44.74; the periodic reads went on at 13:22:47.0); the first sample (13:22:44.7) shows the page
inert for the quiet read but no layer drawn; from 13:22:46 not inert, focus back in the field, text unchanged, same
document, same section, no redirect (`shots/hidden-back-000-132244.png` ... `-025-`). PASS.

### Cell 4, hidden tab across a Panel restart (lab 1)

As cell 3 (text `set7-typed-5f966f`), hidden from 13:25:11.6 to 13:35:43.2 (632 s; `events.jsonl` `tab-back` `hidden_seconds`: 632). As in cell 3, 38 of the 42 hidden samples (from 13:26:11) carry `data-access-hold="blocked"` without a layer. At 13:27:41.8 the browser asked the
harness for a restart; the driver ran `systemctl restart celikpanel-panel.service` on the guest: journal Stopping
13:27:42.649, Started 13:27:42.719, "Starting CelikPanel Backend" 13:27:42.957. Neither probe saw the Panel unanswering
(guest probe 0.5 s interval, host probe 1 s): the outage was shorter than both intervals. No request from the page
while hidden. Back at 13:35:43: one `license/access` and `service/operation` read (200), no layer drawn, focus and
text restored, same document, same section (`shots/hiddenrestart-back-000-133543.png`). PASS, with the limit that the
restart happened and finished entirely while hidden; a hidden tab across a long outage was not measured.

### Cell 5, full page load (lab 3, added by the coordinator during the run)

A new guest with the alpha.82 interface (the cell-1 archive, installed fresh, no update; labs 1 and 2 were already
stopped and their disks removed). Signed in once; then per load a new tab of the same browser (session cookie kept),
HTTP cache disabled for the tab (`setCacheEnabled(false)`: a cold load), `goto` of the route. Two network profiles:
`plain` (the loopback forward, no shaping) and `throttled` (CDP `Network.emulateNetworkConditions`: 2 Mbit/s both
ways, 300 ms latency). Three loads per route and profile, 18 in all. Recorded per load (`flash-loads.jsonl`): every
DOM change of "what is on screen" from a MutationObserver installed before the document's own scripts; the kind on
screen at **every animation frame** for 8 s (a frame callback runs just before that frame is painted); Chrome's own
screencast frames for 6 s (`shots/<load>/NNN-<ms>ms.jpg`, ms from the `goto` call; a frame is delivered when the
picture changes, so the pictures are denser than one per 200 ms where something changes and absent where nothing
does); resource timing of the API reads; the browser's network log (`network-flash-<load>.jsonl`).

Component markers: `recovery-access` = no application navigation (`nav`/`aside`) and a "Reload CelikPanel" button
beside an `<h1>` (RecoveryAccess.tsx; its waiting title is `recovery.checkingTitle` "Checking panel access" and help
`recovery.checkingHelp`); `access-hold` = `[data-top-layer="hold"]`; `page-loading` = a spinner without text;
`app` = the application layout. No `access-hold` was seen in any load.

| Route | plain: RecoveryAccess "Checking panel access" painted (doc ms, frames) | throttled: painted (doc ms, frames) |
| --- | --- | --- |
| `/setup` | 135-231 (15), 124-207 (15), 119-199 (14) | 2258-3953 (281), 2263-3945 (280), 2249-3939 (281) |
| `/` | 114-198 (15), 127-205 (12), 120-203 (14) | 2253-3921 (276), 2247-3941 (281), 2243-3934 (281) |
| `/settings?section=updates` | 118-203 (15), 143-223 (13), 150-230 (13) | 2284-3986 (283), 2250-3931 (279), 2252-3953 (283) |

The full page "Checking panel access / Confirming your session and panel readiness. This check does not start a server
operation." with a "Checking..." spinner button and "Reload CelikPanel" (the owner's description) is painted on
**every** load of every route: about 80-100 ms on the loopback forward (painted frames 78-96 ms, DOM record 87-103 ms; in
Chrome's screencast pictures 55-85 ms between the first and the last such picture), about 1.7 s at 2 Mbit/300 ms
(painted frames 1.67-1.70 s, DOM record 1.68-1.71 s, screencast 1.66-1.70 s). It appears
before any session read has answered (plain, `/setup` load 1: drawn from 135 ms; the first `auth/me` answered at
169 ms per resource timing, 200 in the network log; the same order holds in all 9 plain loads, and in the 9 throttled
loads the first `auth/me` is only sent at 2.58-2.62 s, after the page was drawn at 2.24-2.28 s) and long before 1.5 s. In the throttled loads it ends when the
second `auth/me` answers (e.g. 3.94 s / 3.95 s); each load reads `auth/me` and `panel/availability` twice (2.6-3.3 s,
then 3.6-4.3 s) before `license/access`; before it a spinner without text is painted (plain: from 35-76 ms until the page, about 65-85 ms; throttled: from
1.64-1.68 s until it, about 0.58-0.60 s, after a blank page). FAIL against the rule on all three routes, both profiles. Which parent rendered RecoveryAccess
(`StandaloneRecovery` as the Suspense fallback in `App.tsx`, or `AuthGate`'s `if (!shown) return <RecoveryAccess>`) is
not distinguishable in the DOM; the two session reads per load and the screen ending with the second are consistent
with both in turn. That is an inference, not a measurement. Not measured: a load with the HTTP cache enabled (as when
the owner presses Enter on an address already visited); Turkish wording; a phone viewport.

## What ran on which code

Repository `main` at `2a0af88660773b43cbe1155c622483e2f0a2bbeb` (= tag v0.1.0-alpha.82), tree
`988ea2eab0653ed442d20e1fc351cd9bb68a4c07`; the run copies are `git archive` of that commit (`harness-run-copy/`).
Builds (`build/`, `go1.26.5 linux/amd64`, acceptance-test licence build, one job 12:50:13Z-12:58:06Z):

| Build | Role | Label / seq | Commit | Tree | Archive SHA-256 | Installed |
| --- | --- | --- | --- | --- | --- | --- |
| cur | baseline | v0.1.0-alpha.81 / 81 | dc81296d06774217c2100830c8caca1ff604c4b7 | ff0d0eefe5771b695592d747ec2eedf0f4e7c024 | cad915ae9c28281443bbd50cf544ddf7a59448a7cb07b1a52814c17c56578bfe | labs 1 and 3, fresh |
| cur | good | v0.1.0-alpha.82 / 82 | 2e9b30af347fe6eb830878a72cfba5307ce484f9 | 16778766e749384deda3296f4cad23a4315e2a4c | dacad63ac3d7c6e30bbcf310127cb586c81eecc9c87bebe72ce288d2e03cd937 | lab 1, by the update |
| a81 | baseline | v0.1.0-alpha.81 / 81 | a0beb7263d1f4ca72258f6b306f9111ba4e2a334 (the tag) | b1dffa78bcaca9e3514e5b2bc42f0e2cf47dca2f | 3350ff44dad2bb699ab5ee0a112b58b7bfb7fa47aa0ebdd3dd3c2da73d080109 (set3's dist, reused read-only) | lab 2, fresh |
| a81 | good | v0.1.0-alpha.82 / 82 | 57e9bfab0a8c03d24e459106299621d8f9b3e689 | 988ea2eab0653ed442d20e1fc351cd9bb68a4c07 (= 2a0af8866's tree) | 3177659317f55a86d47b2c478cb0c7f8cf936c8f99be05e72bcf2775dc5cb2e0 | lab 2, by the update |

Both cur archives differ from 2a0af8866 in one file, `deploy/release-sequence-policy` (`build/cur/trees.txt`); no file
of `web/` differs, so labs 1 and 3 ran a fixture build of the alpha.82 `web/` source before and after the update (the built asset bytes were
not compared with the published release's; the baseline and "good" archives are unpublished local fixtures signed with a
disposable key, `lab*/host/upd1-intent.json` `provenance`). The defective, start-check
and real-start archives the builder also makes were never installed. `run-upd1.sh prove` exit 0 for both documents
(`build/*-prove.json`); dry runs exit 0 and created no lab. Each driver result names its archives
(`lab*/driver/result.json`); the Panel's own version answer before and after is in `lab*/driver/steps/09-*/step.json`
(handover) and `10-*/step.json` (`panel_version_after`: lab 1 v0.1.0-alpha.82 2e9b30af, schema 43; lab 2
v0.1.0-alpha.82 57e9bfab, schema 43). Guests: Debian 13 genericcloud 20260826-2582 (the pair lab also boots its Arch
guest, unused), 2 CPUs, 3072 MB, QEMU 11.1.1, WSL kernel 6.18.33.2.

## How the guests were prepared (set6's method)

`set7_trial.Set7BrowserTrial` runs set6's update driver steps unchanged in set6's order: `set6-name-pinning` FIRST
(`celikpanel.net` and the four certificate-authority names to the guest's loopback, read back by `getent -s files` and
by the default lookup path, refused if any product path exists), `preflight`, `origin` (the fixture release origin on
the guest's loopback offering the good archive), `baseline-install`, `owner-login`, `license` (acceptance fixture key;
"not contacted: acceptance test build"), `setup` (to the wait at `access_dns`, DNS mode external), `seed` (one static
site). Then, new: `set7-browser-window` (the driver does NOT start the update; it starts the guest probe, writes a
handover file and the owner's password to a root-only file outside the evidence, serves restart requests, waits for
`done.json`, removes the password file), `set7-guest-timeline`, set6's `collect` and `set6-name-pinning-at-the-end`.
All three labs: every step passed or observed, `overall: complete-for-review` (`lab*/driver/result.json`).

## Deviations

1. **The browser reaches the Panel through the driver's SSH forward** (`127.0.0.1:<port>` on the WSL host ->
   guest `127.0.0.1:2083`), reached from Windows by WSL's localhost forwarding. While the Panel is down, ssh accepts
   the browser's connection and then closes it: Chrome reports `net::ERR_CONNECTION_CLOSED`, where a browser connected
   directly would see a refused connection (`ERR_CONNECTION_REFUSED`). Both are a failed fetch to the interface.
2. **Certificate**: Chrome was given the SPKI hash of the guest's self-signed leaf
   (`--ignore-certificate-errors-spki-list`), so the page was a secure context (`secure_context: true` in every
   sample). `navigator.serviceWorker.controller` was null in every sample: no page of this run was controlled by the
   recovery service worker. Whether it was registered was not read.
3. **Headless Chrome**, light theme, English, 1440x900. A hidden tab = another tab brought to the front. Whether
   headless Chrome applies the same background-timer throttling as a visible desktop window was not measured.
4. **Cells 3 and 4 used Settings -> Panel HTTPS** with typed text, not a domain's page: with setup waiting at
   `access_dns` the interface sends `/domains/<name>` to `/setup` (`lab1-alpha82-interface/browser/probe/probe.json`,
   `probe-domain.png`). No dialog was opened.
5. **Cells 3 and 4 ran on lab 1 after cell 1's update** (the alpha.82 code labelled v0.1.0-alpha.82, interface
   identical to the baseline's), not on a separate guest.
6. **Cell 4 first attempt** (`.../browser/hiddenrestart-attempt1-signin-timeout/`): the browser still held cell 3's
   session, no sign-in form appeared and the script timed out after 60 s before hiding the tab; nothing was measured.
   The script then accepted an existing session; cell 4 was run again in a new browser.
7. **Cell 5 attempts 1 and 2** (`.../browser/flash-attempt1-*`, `flash-attempt2-*`): each load took 3 min because the
   tab close waited for the protocol timeout, and `page.screenshot` during the navigation produced no picture. Both
   were stopped; their DOM/frame records agree with the kept run (RecoveryAccess painted in every load) but have no
   pictures. The kept run uses Chrome's screencast and bounded close.
8. **`live-restart.mjs` changed during the run**: cells 1-3 ran with the first version, cell 4 with the sign-in fix,
   cell 5 with the final file (SHA-256 `c9324b1c5e3f6427c068d3400a3621c456d1967f9932f191baea961980924c4f`).
   The changes added code paths; the measuring code of cells 1-4 was not changed.
9. **The update window's time label**: `SystemUpdateOperation.tsx` labels `toLocaleTimeString()` as "UTC"; the
   headless Chrome ran in the host's time zone (UTC+3), so `shots/update-020-130932.png` shows "UTC 16:09:17" (the label
   is the time of the last failed attempt, 13:09:17 UTC; the picture was taken at 13:09:32 UTC) and `update-000-130852.png`
   shows "UTC 16:08:52" at 13:08:52 UTC. A product observation, outside the verdicts.
10. **Guest probe** (0.5 s, the Panel's public availability route on the guest's loopback; answer 401 without a
    session) and **host probe** (1 s, the same route through the forward) are extra requests to the Panel during the
    windows.

## Network: QEMU user networking with outbound NAT (this is not network isolation)

The guests run on QEMU user networking with outbound NAT; packages come from the distributions' repositories during
setup. No traffic was captured. What is established: the five names were pinned to the guest's loopback before any
product path existed and read back at the end (`lab*/driver/steps/01-*`, `12-*`); the licence step reports "not
contacted: acceptance test build"; every request and navigation the pages made went to the forward at `127.0.0.1:<forward port>`
or was `about:blank` (`network-*.jsonl`: the logger strips the forward's address, so any other address would appear whole,
and none does). Chrome's own background traffic is not a page request and was not captured.

## Offline suite, files changed

Whole suite (`python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_*.py'`): copy `a` (pristine
archive) 1011 tests, 5 errors; copy `b` (+ the two set7 files) 1011 tests, the same 5 errors by name
(`harness-run-copy/`; each is a `git` call on a run copy that is an archive, as in set6). No test was added.

Added in the working tree (not committed): `deploy/e2e/release-recovery/set7_trial.py`,
`deploy/e2e/release-recovery/run-set7.sh`, `web/tools/browser-inspect/live-restart.mjs`, this folder. No file under
`cmd/`, `internal/`, `web/src`, `docs/`, `ROADMAP*` was edited. `puppeteer-core` 24.10.0 was installed in the
session's scratch folder (outside the repository); nothing was installed system-wide; Chrome is the existing Windows
installation.

## Disk, host power

Windows `C:` read by `Get-PSDrive C` every 30 s (`host/c-drive-watch.txt`): 149.22 GiB at the start, lowest 142.64,
143.16 at the end; never under 40 GiB. The gate asked before each lab prepare and guest start: 6 decisions, all
allowed (`host/c-drive-cells.txt`). Each lab's overlays (about 1.4-1.8 GB) were removed after staging
(`host/removals.txt`). Host power: a process-level keep-awake request 12:49:48Z-14:38:20Z (`host/keepawake.log`). The
System log (`host/sleep-events.txt`) has no sleep entry (42); "entering modern standby" (506) at 13:04:26Z, 13:57:43Z
and 14:37:11Z, "leaving" (507) at 13:07:46Z and 14:06:47Z: the first falls in lab 1's install/setup; the second begins
between lab 2's end (13:51:11Z) and lab 3's start (13:58:56Z) and ends in lab 3's install/setup; both are before their
browser windows (lab 1 from 13:08Z, lab 3 from about 14:29Z); the third is after lab 3's last step (14:33:06Z). The watcher wrote a reading every 30-31 s from 12:49:48Z to 14:37:35Z
(`host/watch-gaps.txt`: largest gap 31 s): no pause of execution is seen.

## Not measured

Ubuntu 24.04; Arch; a phone viewport, dark theme, Turkish; Firefox or Safari; a visible (headful) browser; a page
controlled by the recovery service worker; a hidden tab across a long Panel outage (cell 4's restart lasted less than
0.5 s); the domain page and dialogs; cell 5 with the HTTP cache enabled; any installed server.

## Files

`lab1-alpha82-interface/` (cells 1, 3, 4), `lab2-alpha81-interface/` (cell 2), `lab3-alpha82-interface-page-load/`
(cell 5): `driver/` (the driver's run directory with its own `SHA256SUMS`), `host/` (lab records, wrapper output,
digest sweep at staging), `browser/` (per cell: `samples-*.jsonl`, `network-*.jsonl`, `events.jsonl`, `console-*`,
`host-probe.jsonl`, `shots/`; the Chrome profile directories, which hold the session, were not staged and were
deleted), `timeline.md` (cells 1 and 2), `flash-summary.txt` (cell 5). `build/`, `harness-run-copy/`, `host/`,
`tools/` (every script of this run, including the Windows runner and the scanners), `secret-scan.txt`,
`checks-not-passed.txt`, `SHA256SUMS` (generated last).

Secrets: the text scan (`secret-scan.txt`, set6's classes plus class 11, the owner passwords the browser typed and the
fixture key, searched by value from root-only copies kept on the lab host until the scan and then deleted) found no
unexplained hit; two explanations were added to the scanner for this run (the base64 SHA-256 of the guest leaf's
public key, `spki_sha256_base64`, and `password_file`, which holds a path or "not copied", never the password).
Pictures are not covered by a text scan: the password was typed only into the masked sign-in field after
`00-sign-in.png` was taken, no licence page was opened, and the screenshots read for this record (listed in the
verdicts) show no key or password; not every one of the 3 958 pictures was looked at.

## Corrections after intake (2026-10-10)

Before this folder was committed to a public repository its files were read again against this record: every sum, every
text file, every picture class, and each verdict against the raw files and the decisive pictures. What that found, and
what was changed in this folder (no measurement was changed; only wording, paths and this record):

- **Secrets.** No secret was found in any text file (class and count of the intake scan: `secret-scan.txt`, last section) and none in
  any picture; no text was redacted and no picture was replaced. 95 pictures were looked at one by one (one picture of
  every distinct screen in a systematic sample of 600 - the first 10 and last 10 of every sequence, every 25th between
  them, and the single named pictures - three of the eight sign-in pictures, and one of every further distinct screen
  found below; the other five sign-in pictures are equal by pixels to those looked at); all 3 958 were then compared by pixels with the pictures looked at: 2 648 are equal to one of them,
  1 062 differ only where a spinner or the update window's clock is drawn, and the remaining 248 form 12 further screens, all looked at.
  The browser took only one picture of a sign-in form (`00-sign-in.png`, before anything was typed, empty fields); none
  was taken while the owner password was typed or until the sign-in had completed.
- **Local paths.** 19 occurrences of the author's Windows user name and session folder in 10 files (`tools/job-cell-*.sh`,
  `tools/job-suites.sh`, `tools/run-browser.sh`, `lab*/host/job.sh`, `lab1-alpha82-interface/browser/runner.log`,
  `.../hiddenrestart-attempt1-signin-timeout/run.json`) were replaced by `<scratchpad>`; the scripts are records, not runnable as they stand.
- **Sentences that said more than the files show, now corrected above:** the opening claim that no licence service or
  certificate authority was addressed and that the harness holds no such name (it holds the names, to pin them; traffic was not
  captured); "about 8 to 10 s" for the hold layer in cell 1 (9 to 11 s); "24 s against 89 s" (two different measures; 25 s against
  89 s by the same probe); "no hold" for the hidden samples of cells 3 and 4 (no layer, but the page subtree carried
  `data-access-hold="blocked"` in 38 of 42 samples from 60 s after hiding); "before hiding, `license/access` every ~30 s" in
  cell 3 (that log holds 8 s before hiding; the cadence belongs to the update cell, 29-36 s, and after the return 22-23 s);
  cell 4 "631 s ... 13:35:41" (632 s, 13:35:43.2); "is inert" and "non-dismissible" (read from `AccessHold.tsx`, not tested in the page);
  the plain and throttled durations and the spinner times of cell 5 (ranges now given per measure); the UTC-label example of deviation 9;
  "the browser opened only 127.0.0.1" (page requests only; Chrome's own traffic was not captured); the modern-standby sentence;
  "ran the alpha.82 interface" (a fixture build of the alpha.82 `web/` source, asset bytes not compared with the published release).
- **Confirmed against the raw files:** the timeline of cells 1 and 2 (every instant of the two tables), the cell 3 and 4
  request counts while hidden (0), the one quiet read on return, the 18 of 18 loads with RecoveryAccess painted before any
  session read answered, the frame counts and ranges of the cell 5 table, the five pinned names read back in all three labs at the first
  and the last step, the tree `988ea2ea...` (= `git rev-parse 2a0af8866^{tree}`), the archive `3350ff44...`, `live-restart.mjs`
  `c9324b1c...`, the disk and keep-awake figures, the watcher's largest gap (31 s), the 1011 tests with 5 errors.
- **Stated here only, with no raw record:** `puppeteer-core` 24.10.0 (Chrome 154.0.8037.98 is recorded in every `run.json`;
  "headless" only in `live-restart.mjs`); the guests' package versions (only the image name, kernel and QEMU version are recorded).
- **Kept as they are, noted for the reader:** the fixture-plan files hold three throw-away public SSH keys whose comment
  is the author's machine name (`root@ALIASUSPC`; public keys, one per lab, not secrets); four files use CRLF line ends
  (`host/watch-gaps.txt`, `lab1-alpha82-interface/timeline.md`, `lab2-alpha81-interface/timeline.md`,
  `lab3-alpha82-interface-page-load/flash-summary.txt`; not converted, the sums cover the bytes); 3 684 of the 3 958 pictures are the
  cell 5 screencast frames, which are named by pattern (`shots/<load>/NNN-<ms>ms.jpg`) in this record and counted per load in
  `flash-loads.jsonl`, not one by one; no file is larger than 5 MB.
- **Pruned pictures.** To keep the repository small, 2 626 of the 3 668 cell 5 screencast frames were deleted from
  `lab3-alpha82-interface-page-load/browser/flash/shots/*/` (1 042 kept: every frame up to 400 ms for plain loads and up to
  2 500 ms for throttled loads, every 10th frame after that, the last frame, `final.png`, and every frame named in a text file);
  `shots/PRUNED.txt` gives the rule, the counts per load and the full list of deleted file names, and no other cell's pictures
  were touched. The DOM record (`flash-loads.jsonl`, `flash-summary.txt`) carries the painted-frame timeline of every load in
  ms from navigation, but it does not list the screencast frames, so a deleted picture is known only from `PRUNED.txt`; the
  picture counts quoted above ("3 958", "3 684") describe the folder before this pruning.
- `SHA256SUMS` was regenerated last, after these changes.
