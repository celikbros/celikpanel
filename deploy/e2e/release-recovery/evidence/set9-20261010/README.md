# set9, 2026-10-10 (UTC): the cold-load fix of e508af230 in a real Chrome against a real Panel, on disposable Debian 13 guests

A measurement record. Disposable QEMU/KVM guests of the local `archlinux` WSL host; a real Google Chrome (headless, on the
Windows host; version in every `run.json`) driven by `puppeteer-core` 24.10.0 (set7's copy, reused; the version is read from that copy's `package.json` and has no raw record in this folder) against the real Panel of the
guest through the lab driver's loopback SSH forward. Dates and times are UTC from the host clock. Nothing in this folder
is a statement about an installed server. The harness holds the names of the release/licence host and of four
certificate-authority hosts only to pin them to the guest's loopback (first driver step, read back at the end in all three
labs). The guests have outbound NAT (QEMU user networking); their traffic was not captured (see "Network").

What was measured: `git archive 7c3a05809` (branch `fix/alpha83-known-state-gates`; `git diff --stat e508af230 7c3a05809 --
cmd internal web/src` is empty, `build/cur/trees.txt`), built as set7's "cur" fixtures (B = the source labelled
v0.1.0-alpha.81, G = B labelled v0.1.0-alpha.82; both differ from the source only in `deploy/release-sequence-policy`; no
file of `web/` differs). Set7's numbers are those of the published alpha.82 interface (its cell 5 and cell 1).

## Verdicts

| Cell | What | Verdict | Decisive raw file and picture |
| --- | --- | --- | --- |
| 1 | cold load x3 of `/setup`, `/`, `/settings?section=updates`, plain and throttled (2 Mbit/s, 300 ms), signed in, Panel healthy | **PASS on all three routes, both profiles** (18 of 18 loads: no sentence and no button before the first read answered, which came 71-97 ms (plain) and 0.92-0.98 s (throttled) after the first mount, always inside the 1.5 s quiet time; the "Checking panel access" page never drawn; recomputed at intake from `loads.jsonl`) | `lab1-.../browser/flash/loads.jsonl`, `summary.txt`, `lab1-.../cell1-set7-against-set9.md`; `.../shots/plain-setup-1/007-00230ms.jpg`, `.../shots/throttled-setup-1/060-02613ms.jpg` |
| 2a | every session read held 2.5 s (CDP), EN, three routes | **PASS** (explained wait 1.58-1.62 s after the quiet surface began, new EN text, no reload) | `lab1-.../browser/slow-en25/loads.jsonl`; `.../shots/slow2500-en-setup-1/032-02000ms.jpg` |
| 2b | the same, TR | **PASS** (TR text) | `lab1-.../browser/slow-tr25/`; `.../shots/slow2500-tr-settings_section_updates-1/029-01998ms.jpg` |
| 2c | every session read held 35 s, watched 40 s | **FAIL against "reload only after 30 s"**: "Reload CelikPanel" is drawn at 15.18 s | `lab1-.../browser/slow-en35/loads.jsonl`; `.../1340-15311ms.jpg`, `.../1420-26007ms.jpg` |
| 2d | the interface chunk held 35 s instead (session answered) | **PASS** for the first-wait path: reload and the "longer than half a minute" sentence at 31.65 s, 30.0 s after the wait was explained | `lab1-.../browser/slow-chunk35/`; `.../008-31668ms.jpg` |
| 3 | update started from Settings -> CelikPanel updates; the hold layer during the restart | **PASS** on lab 3 (layer titled "The Panel is not answering during an update", page mounted and inert, continues in place); labs 1 and 2: **layer not drawn** (see below) | `lab3-.../browser/updatefocus-lab3/samples-update.jsonl`; `.../shots/update-020-183544.png`, `update-029-183602.jpg`, `03-steady-after-update.png` |
| 4a | no session (401), cold load | **PASS** (sign-in form at 196-212 ms; 401 answered at 141-147 ms) | `lab1-.../browser/negative/loads.jsonl` |
| 4b | session read fails as a closed connection (CDP), cold load | **PASS** ("Your session could not be checked" at 201-206 ms) | same |
| 4c | Panel service stopped by the harness, cold load | **NOT-MEASURED for the product's screen**: Chrome's own error page at 112-116 ms; the recovery service worker was not registered in the signed-in browser context of that cell (`stopped/events.jsonl`, `service-worker-before-stop`); see the correction on service workers below | `lab1-.../browser/stopped/`; `.../shots/stopped-root/001-00136ms.jpg` |
| 4d | Panel just started by the harness (the availability read answered 200; "starting" is the state the screen names), cold load | **PASS** ("The panel is starting" at 244 ms; no body is recorded, so the answer's `starting` state is read from the screen) | `lab1-.../browser/stopped/shots/after-start-root/final.png` |

### Cell 1: set7 (alpha.82) against set9 (7c3a05809), on set7's time base

Ms since the probe started in the new document ("doc ms", set7's base; in set9 the probe started 19-37 ms after the
navigation's time origin on plain loads, 339-347 ms on throttled ones). Three loads per row; values that differ between
loads are separated by "/". Kinds: `empty` = `#root` without child; `spinner` = a spinner without text (the language
loader, `role="status"` with an `aria-label`, no visible text; App's PageLoading); `quiet` = e508af230's
`[data-access-quiet]` surface (page background only); `recovery-access` = RecoveryAccess's full page.

| Route | Profile | Run | 0-200 doc ms | at 1 s | at 2 s | "Checking panel access" page | app from (doc ms) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `/setup` | plain | set7 | empty > spinner > **recovery-access** | app | app | yes, 119-231 | 247-282 |
| `/setup` | plain | set9 | empty > spinner > quiet (1 load: empty > spinner) | app | app | **no** | 292-478 |
| `/setup` | throttled | set7 | nothing painted | empty | spinner | yes, 2249-3953 | 4654-4692 |
| `/setup` | throttled | set9 | nothing painted | empty | spinner | **no** | 4574-4630 |
| `/` | plain | set7 | empty > spinner > **recovery-access** | app | app | yes, 114-205 | 250-254 |
| `/` | plain | set9 | empty > spinner > quiet | app | app | **no** | 299-367 |
| `/` | throttled | set7 | nothing painted | empty | spinner | yes, 2243-3941 | 4654-4681 |
| `/` | throttled | set9 | nothing painted | empty | spinner | **no** | 4551-4585 |
| `/settings?section=updates` | plain | set7 | empty > spinner > **recovery-access** | app | app | yes, 118-230 | 316-337 |
| `/settings?section=updates` | plain | set9 | empty > spinner > quiet | app | app | **no** | 465-583 |
| `/settings?section=updates` | throttled | set7 | nothing painted | empty | spinner | yes, 2250-3986 | 4813-4862 |
| `/settings?section=updates` | throttled | set9 | nothing painted | empty | spinner | **no** | 4654-4860 |

(set7's "page-loading" is written "spinner" here; the full per-load table, with set7's kind names, is
`cell1-set7-against-set9.md`.) In set9 the first painted frame with a sentence or a button is the application itself in
every load (plain 292-583 doc ms, after the first read answered at 150-319 ms from the navigation call; throttled
4551-4860 doc ms, after the first read answered at 2924-3008 ms). What is drawn instead of the old page: on plain loads the
quiet surface for 110-180 ms (from 87-212 to 205-374 doc ms), on throttled loads for about 1.8-2.0 s (from 2220-2269 to
4096-4300 doc ms), then a spinner, then the app. The screencast sent only a few pictures during the throttled quiet stretch
(the picture does not change in it): between the last kept quiet picture and the next kept spinner picture
`loads.jsonl` `shots` leaves 0-3 received pictures unkept per load. The throttled quiet stretch of 1.8-2.0 s is longer than the 1.5 s quiet time, yet no explained wait was drawn:
the surface spans several gates of the load in turn (each its own wait); which component drew each part is not
distinguishable in the DOM (the marker is the same). Observation, not a verdict: the app appeared later than in set7 on
plain loads (the set9 range minus the set7 range, loads not paired: +10 to +231 ms on `/setup`, +45 to +117 on `/`, +128 to +267 on
`/settings?section=updates`) and earlier on throttled `/setup` and `/` (-24 to -118 and -69 to -130 ms; the throttled `/settings` ranges overlap, -208 to +47 ms);
different build, guest and run. "App" on `/` and `/setup` is the server-setup page: the guests wait at the DNS step (`lab*/driver/result.json`, `findings`).

### Cell 2: slow session read

**2a/2b (2.5 s, CDP `Fetch.requestPaused` on `/api/v1/auth/me*`, continued unchanged after 2.5 s).** Each load paused two
session reads; the first was cancelled by the page within 0.4 s (status 0; "Invalid InterceptionId" at release), the second
answered at 2702-2898 ms (ms from the navigation call). EN, all three routes: quiet surface from 156-295 ms to 1703-1874 ms; then, at 1731 / 1759 / 1917 ms
(1.58-1.62 s after the quiet surface was first painted) until the read answered:

> **Checking panel access** / "The Panel has not answered yet. CelikPanel opens as soon as it does; you do not need to do
> anything." / one button "Checking…" with a spinner (no reload; the probe records button names only, so "disabled" is read
> from `RecoveryAccess.tsx`, `disabled={checking}`, not measured)

TR at 1712 / 1714 / 1756 ms (1.57-1.59 s after the quiet surface): **Panel erişimi kontrol ediliyor** / "Panel henüz yanıt vermedi. Yanıt verir vermez
CelikPanel açılır; bir şey yapmanız gerekmiyor." / "Kontrol ediliyor…". Both match `recovery.checkingTitle` and
`recovery.waitingHelp` of `web/src/i18n/{en,tr}.ts` at 7c3a05809. Then a spinner and the app (2.77-3.09 s).

**2c (35 s hold, one load of `/settings?section=updates`, 40 s).** quiet 133-1678 ms; explained wait as in 2a from
1721 ms; **15.18 s**: the page's own 15 s read timeout ends the first wait and the screen becomes "Your session could not be
checked / CelikPanel cannot confirm your session right now. This page checks again by itself; you can also check now.
Management stays closed until your session and panel access are verified." with **"Check panel access" and "Reload
CelikPanel"**; 25.19 s (automatic re-read): "Checking panel access / Confirming your session and panel readiness. This
check does not start a server operation." with "Checking…" and "Reload CelikPanel"; 40.19 s: "Your session could not be
checked" again. `recovery.waitingProlonged` never appeared. The expectation "the reload action only after 30 s" is not met
on this path: the reload is offered at 15.18 s (FAIL, cause read from `usePanelSession.ts`: the 15 s abort sets
`auth_unavailable`, whose RecoveryAccess screen carries the reload; the older `recovery.checkingHelp` sentence returns on
the re-read).

**2d (the first-wait path to 35 s).** `LIVE_SLOW_PATTERN=*/assets/SystemUpdateOperation-*` held the chunk App.tsx's
Suspense waits for; the session read answered at 217 ms. quiet 137-1632 ms; 1667 ms: "Opening CelikPanel / Your session is
confirmed and the Panel is ready. The interface is still loading and opens by itself." (no action button); **31.65 s**:
"This has taken longer than half a minute. CelikPanel keeps checking by itself; you can also reload it." and "Reload
CelikPanel". After the chunk was released (35.15 s) the next gate drew "Checking panel access / The Panel has not answered
yet…" for 55 ms (35166-35221 ms, its read answered at 35205 ms), then the app at 35.41 s. Observation: that sentence was on
screen for 55 ms while the Panel had answered 35 s earlier (the reading that this is quietRead's "continue an explained wait" rule is
from `web/src/lib/quietRead.ts`, not measured; "released at 35.15 s" is the ms of the end of the held-chunk screen, the held request itself was released 35.14 s after the navigation call, `events.jsonl`).

### Cell 3: the hold layer during an update (three labs)

**Lab 3 (decisive), set7's method with the owner's click timed (`LIVE_UPDATE_TIMED=1`, lead 40 s):** the layer was drawn when the page's access decision
(`valid_until`, on the server's 45 s grid within one Panel process, `lab2-.../browser/accesswatch-lab2/access-accesswatch.jsonl`, and
`lab3-.../browser/updatefocus-lab3/access-update.jsonl`) ran out while the Panel did not answer (the refresh read is made 15 s before the
deadline and failed at 18:35:29.02); one lab shows this, and labs 1 and 2 (no layer, see below) are the only other observations, so
"only when" is not established. The click was placed 40 s before the next deadline (`events.jsonl`, `timed-click-plan`).

| Instant (UTC; the journal rows are on the guest's clock, the guest probe rows are corrected to the host clock by -0.244 s) | lab 3 |
| --- | --- |
| click "start" | 18:35:04.02 |
| Panel SIGSTOP / stopped (guest journal, guest clock) | 18:35:24.85 / 18:35:25.96 |
| no answer: guest probe (host clock; guest clock 18:35:25.20) / host probe | 18:35:24.96 / 18:35:26.16 |
| first failed request (license/access refresh) | 18:35:29.02 |
| update window: "The state of this update could not be read just now…" | 18:35:30.1 sample |
| access deadline; failed license/access | 18:35:44.0; 18:35:44.03 |
| **layer drawn** (first sample) | **18:35:44.2** |
| Panel started (journal, guest clock) / answering (guest probe, host clock; guest clock 18:35:54.46 / host probe) | 18:35:54.12 / 18:35:54.21 / 18:35:54.37 |
| first successful license/access | 18:35:59.03 |
| layer gone (same document, same section) | between the 18:35:58.25 and 18:36:00.26 samples |
| corner notice and card "Update installed, being verified" | `update-029-183602.jpg` (18:36:02.3) |
| product's own reload, new document | by the 18:36:10.3 sample; steady: v0.1.0-alpha.82, commit 4b87e628 (`03-steady-after-update.png`) |

The layer (`update-020-183544.png`, text from the DOM):

> **The Panel is not answering during an update**
> An update was started from this browser, and its end has not been seen here yet. The Panel restarts while an update is
> applied, so it may not answer for a short while. This is not a license problem.
> You do not need to do anything yet. CelikPanel checks again by itself. When access is confirmed, this page continues
> where it was, with what you typed. Until then nothing on this page can be changed.
> Update and recovery status / Operation ID … / "The latest result could not be read. … do not start a second update." /
> "Check this operation", "Check panel access"

= `accessHold.updateTitle`, `accessHold.updateHelp`, `accessHold.resume` at 7c3a05809. While it was up (8 samples) the
Settings page stayed mounted with the updates tab selected and the same document marker; the held subtree carried
`data-access-hold="blocked"` with `inert` (read in the page); the centre and the corner point were inside the layer; the
browser's update record read `phase: active`. On screen at least 14.1 s (first to last sample with the layer, 18:35:44.20-18:35:58.26)
and at most 18.1 s (the samples before and after, 18:35:42.19-18:36:00.26; 14.3-16.3 s if drawn at the 44.0 deadline) of a 29.3 s outage (guest probe; 28.75 s from first no-answer to last). set7 cell 1, same place:
"Panel access could not be confirmed just now / CelikPanel could not read the license result…", 9-11 s of a 25 s outage.

**Lab 1 (set7's method unchanged, `live-restart.mjs` at 7c3a05809, SHA-256 c9324b1c…):** click 18:03:54.77, SIGSTOP
18:04:19.52, started 18:04:47.57 (host probe no answer 18:04:19.92-18:04:48.06); one license/access read in the outage
(18:04:38.03, failed), the next answered (18:04:51.11): **no layer** (the access decision's end time was not recorded in labs 1
and 2, so "the decision never ran out" is inferred from the reads, not read). The update window said
"The state of this update could not be read just now, so whether it is still running or has finished is not known. This
notice reads it again by itself; do not start another update." / "The connection was interrupted; …" (18:04:20.9),
"Update installed, being verified / v0.1.0-alpha.82 is installed and this panel is running it. …" (18:04:51.0, `update-030-180455.png`),
"The update completed. …" (18:05:07.1), then the product's reload; "Last read 9:04:20 PM GMT+3" (local time with its zone).
**Lab 2** (one owner-like step: another tab for 1.5 s, 3 s into the outage): outage 18:18:21.27-18:18:44.45 (host probe), the
read on return failed (18:18:25.82), the next answered (18:18:56.01): **no layer**.

Observation, not a verdict (`update-029-183602.jpg`): while the card and the corner notice said "v0.1.0-alpha.82 is
installed and this panel is running it", the card's "Current version" and the sidebar still read v0.1.0-alpha.81 until the
product's reload.

### Cell 4: known negatives

Ms from the navigation call. (a) New browser context, no cookie: session read 401 at 141-147 ms, sign-in form painted at
196-212 ms on all three routes; on `/` and `/settings` the form was painted for one frame, then the quiet surface for about
12 ms, then the form again (two gates in turn). (b) Signed in, every session read failed in the browser as a closed connection
(CDP `Fetch.failRequest ConnectionClosed`, what the browser saw through the forward with the Panel down in set7): "Your
session could not be checked" at 201-206 ms, with a 3-7 ms quiet gap between two such frames (`negative/summary.txt` counts
"rule-breaking runs 2" for each of these loads, and 1 for each load of 4c: the driver's counter treats only an answered read as an
answer, so a failed read is not one; the PASS above judges a failed read as a known state shown at once, not the counter). (c) `systemctl stop celikpanel-panel.service`
by the harness (18:01:33.73): the navigation failed (`net::ERR_CONNECTION_CLOSED`) and Chrome drew its own error page
("Bu siteye ulaşılamıyor", the host's Windows language) at 112-116 ms; the product draws nothing. `navigator.serviceWorker.getRegistration('/')`
read before the stop, in the signed-in browser context of this cell (`stopped/events.jsonl`, `service-worker-before-stop`): not
registered (why not was not determined; set7 also saw no controlled page in its signed-in loads), so the product's offline
recovery page (`recovery-offline.html`) was not reached in this cell: NOT-MEASURED. The no-session loads of 4a, in a new browser
context about a minute earlier, were controlled by a service worker (see the correction on service workers below), so a registered worker was
possible in this Chrome; 4c was not repeated in such a context. (d) 6.3 s after `systemctl start` (18:01:55.97):
"The panel is starting / The server is preparing panel access. This page checks readiness automatically." with "Check panel
access" and "Reload CelikPanel" at 244 ms (quiet surface 121-214 ms before it; the availability read answered 200 at 212-217 ms,
its body is not recorded, so the `starting` state is the screen's), still on screen at 7 s; at 18:03:14 a cold load opened Settings normally (`flash-readycheck/`).

## How it ran

Set7's lab method (`run-set9.sh` = `run-set7.sh` with `set9_trial.py`; `set9_trial.py` = `set7_trial.Set7BrowserTrial` with
the run root `/var/tmp/cp-set9-run` and two extra harness requests, `stop`/`start` of the Panel's service, used by cell 4c/d
only). Driver steps in set6's order, name pinning first; all three labs: every step passed or observed, `overall:
complete-for-review` (`lab*/driver/result.json`). Lab 1 (17:38-18:07Z) cells 1, 2, 4 and update attempt 1; lab 2
(18:08-18:25Z) attempt 2 and the read-only `accesswatch`; lab 3 (18:25-18:45Z) attempt 3. Build: one job 17:28:53-17:37:43Z,
`go1.26.5 linux/amd64`, `run-upd1.sh prove` exit 0, dry run exit 0, no dry-run lab (`build/`). Debian 13 genericcloud
20260826-2582 (the pair lab boots its Arch guest, unused). Headless Chrome, light theme, 1440x900; a new tab per cold load,
cache disabled, Chrome's screencast plus the painted kind at every animation frame (set7 cell 5's method;
`web/tools/browser-inspect/cold-load-set9.mjs`). Cell 3 sampled every 2 s as set7 cell 1.

Pictures: every screencast frame up to 400 ms after the navigation call, then every 5th, plus the first frame after each
change of the painted kind, the frames nearest 1000 and 2000 ms, and the last (`loads.jsonl` `pictures_received` /
`pictures_kept`); the 35 s load was pruned further after the run (`slow-en35/shots/PRUNED.txt`: 442 of 571 deleted).
Screencast pictures lag the DOM by up to a few tens of ms (e.g. `throttled-setup-1/104-04939ms.jpg` still shows the spinner
labelled `app`).

## Deviations

1. **Three guests, not one.** Cell 3 on lab 1 (set7's exact method) and on lab 2 drew no layer; the access decision is taken
   not to have run out inside the restart (inferred from the reads, its end time was not recorded in those labs); lab 3 timed the owner's click. The tab switch (lab 2) and the timed click (lab 3) are
   additions to set7's method, recorded in each `events.jsonl`.
2. **Simulations in the browser**: cells 2 (held reads/chunk) and 4b (failed reads) use CDP `Fetch`; the document and the
   interface always came from the real Panel. 4c/d stopped and started the real service (harness, disposable guest).
3. **`cold-load-set9.mjs` changed during the run** (only additions): `LIVE_SLOW_PATTERN` after runs 2a-2c, `updatefocus`
   after lab 1, the access-answer fields and the timed click after lab 2. Final SHA-256
   `043a7a6e89bcb5a90145fdbdf6ba62bf5ddabb12b1c817739de191befb816a48` (`harness-run-copy/working-tree-against-copy-b.txt`); it equals
   the committed `web/tools/browser-inspect/cold-load-set9.mjs` (cd46ca595).
4. Forward and certificate as set7 (deviations 1-2 there): `net::ERR_CONNECTION_CLOSED` instead of a refused connection; SPKI
   pin; `secure_context: true`; no page of the signed-in contexts was controlled by a service worker, but the three no-session
   loads of 4a (a new browser context) were (`negative/loads.jsonl`, `where.sw_controlled: true`; for two of them, `/` and `/settings?section=updates`, the document and
   `recoveryObservation-*.js` came from the worker, `network-load-nosession-*.jsonl`, `sw: true`).

## Network

QEMU user networking with outbound NAT (not isolation). Not captured. Established: the five names pinned to the guest's
loopback before any product path and read back at the end (`lab*/driver/steps/01-*`, `12-*`); every page request went to the
forward (the logger strips its address; no other address appears in `network-*.jsonl`); Chrome's own traffic not captured.

## Offline suite, files changed

Whole suite on run copy `a` (pristine archive) and `b` (+ `set9_trial.py`, `run-set9.sh`): 1011 tests, the same 5 errors
by name in both (set7's known state; `harness-run-copy/`). No test added. `deploy/e2e/release-recovery/set9_trial.py`,
`deploy/e2e/release-recovery/run-set9.sh` and `web/tools/browser-inspect/cold-load-set9.mjs` are committed (cd46ca595, byte-identical
to the copies measured: SHA-256 `c2e45ba0…`, `3d562f34…`, `043a7a6e…`); this folder was not committed when it was staged.
No product file edited. (`harness-run-copy/a/files.sha256` is empty and `b/files.sha256` lists the two harness files by repository path,
so neither verifies in place; the hashes were compared with the repository files at intake and match.)

## Secrets, disk, host power

`secret-scan.txt`: no unexplained hit (owner passwords by value: 0 occurrences; the 4 informational path hits are the
scanner's own source). Its classes do not cover the operator's host name, which stood in the SSH public-key comment of each lab's
`host/fixture-plan.json` (6 places) and is now `root@<operator-host>` (see the corrections below). Passwords were typed only after `00-sign-in.png` (empty form); the decisive pictures above were
looked at and show no key or password (operation IDs are not secrets). Windows `C:` every 30 s (`host/c-drive-watch.txt`):
132.46 GiB at the start (17:28:08Z), lowest and last 115.67 (18:45:54Z); gate before each of 3 guest starts allowed (`host/c-drive-cells.txt`); overlays
removed after staging (`host/removals.txt`). Keep-awake request 17:28:09Z-18:46:10Z
(`host/keepawake.log`). `host/sleep-events.txt`: modern standby entered 18:11:09Z, left 18:32:28Z (lab 2's install and
update, lab 3's install); the watcher wrote every 30-31 s throughout (`host/watch-gaps.txt`, largest gap 31 s): no pause
of execution seen.

## Not measured

The product's offline page with the Panel stopped (no service worker registered in the signed-in context of cell 4c; not repeated in
a context that has one); a reload offer after 30 s on the
session-read path (it ends at 15 s); Ubuntu, Arch; phone viewport, dark theme; TR for cells 1, 3, 4; HTTP cache enabled;
Firefox/Safari; a visible (headful) browser; any installed server.

## Corrections after intake (2026-10-10)

Intake before the folder is committed to the public repository (2026-10-10, 19:20-20:00 UTC). Only files inside this folder were
changed. Every change is listed; the measurements themselves were not changed.

1. **Operator host name removed.** `root@` followed by the operator's Windows host name (in the SSH public-key comment of the guests'
   cloud-init data) stood in `lab1-.../host/fixture-plan.json`, `lab2-.../host/fixture-plan.json` and
   `lab3-.../host/fixture-plan.json` (2 places each, 6 in all). It is now `root@<operator-host>`, as in the other published evidence
   (commit ac473b520). The three files still parse as JSON; the public keys themselves are unchanged. `secret-scan.txt` did not
   report it because its classes name user-profile paths, not host names; a note was added there.
2. **Service-worker statements narrowed.** README said no page was controlled by a service worker and that the worker was "not
   registered in this browser". The files show: in the signed-in contexts no page was controlled and `getRegistration('/')` before
   cell 4c's stop read "not registered" (`stopped/events.jsonl`); but the three no-session loads of cell 4a were controlled
   (`negative/loads.jsonl`, `sw_controlled: true`), and for `/` and `/settings?section=updates` the document and
   `recoveryObservation-*.js` came from the worker (`sw: true`). Why the worker registered in one context and not in the other was not
   determined, and cell 4c was not repeated in a context that has a worker: it stays NOT-MEASURED. Edited: verdict row 4c, the 4c
   paragraph, Deviation 4, "Not measured", and `checks-not-passed.txt`.
3. **"Starting" answer.** The README called the 4d answer "a real `starting` answer". The availability read answered 200 at
   212-217 ms; the body is not recorded, so `starting` is the state the screen shows. Edited in the verdict row and the 4d paragraph.
4. **Screencast during the throttled quiet stretch.** "The screencast sent no picture" is replaced by "only a few": 0-3 received
   pictures per load lie unkept between the last kept quiet picture and the next kept spinner picture (indices in `loads.jsonl`).
5. **Cell 3.** (a) "The layer appears only when the access decision runs out" is narrowed to what lab 3 shows (one lab; labs 1 and 2 drew no
   layer, but their decision end time was not recorded, so "never ran out" is an inference there); (b) first failed request
   18:35:29.01 -> 18:35:29.02 (network record 18:35:29.020); (c) the table mixes the guest's clock (journal rows) and the host clock
   (probe rows, corrected by -0.244 s): the header and rows now say which; (d) "on screen 14-16 s" is now "at least 14.1 s, at most 18.1 s by
   the 2 s samples (14.3-16.3 s if drawn at the 44.0 deadline)".
6. **Cell 1 observation** "later than in set7 by about 50-250 ms, slightly earlier on throttled" now gives the ranges (plain +10 to
   +267 ms across routes, throttled -130 to +47 ms, loads not paired) and says that on `/` and `/setup` the "app" is the server-setup page
   (the guests wait at the DNS step, `lab*/driver/result.json` `findings`). Verdict text for 18 of 18 now says the first read answered
   71-97 ms (plain) and 0.92-0.98 s (throttled) after the first mount, inside the 1.5 s quiet time.
7. **Cell 4b/4c counter.** `negative/summary.txt` and `stopped/summary.txt` count "rule-breaking runs" (2 per 4b load, 1 per 4c load)
   because the driver's counter treats only an answered read as an answer; the README now says so next to the PASS/NOT-MEASURED.
8. **Cell 2 wording.** "one disabled button" -> "one button 'Checking…' with a spinner"; "disabled" is read from `RecoveryAccess.tsx`, the
   probe records button names only. The 2d remark that the 55 ms sentence is quietRead's rule is marked as a reading of the code.
9. **Committed files.** README said `set9_trial.py`, `run-set9.sh` and `cold-load-set9.mjs` were "not committed"; they are committed
   (cd46ca595) and byte-identical to the measured copies (SHA-256 of the driver `043a7a6e…`). `harness-run-copy/{a,b}/files.sha256`
   do not verify in place (empty / repository paths); stated.
10. **puppeteer-core version** 24.10.0 added to the introduction (read from set7's module copy on the host at intake; no raw record).
11. **Checksums.** The root `SHA256SUMS` now also lists the three `lab*/driver/SHA256SUMS` files (set7 lists them), and the three
    `fixture-plan.json` lines were recomputed. It was regenerated last and verified with `sha256sum -c`.

Confirmed unchanged at intake (recomputed from the raw files): the cell 1 table (identical to `cell1-set7-against-set9.md`, produced by
`tools/table9.py`); cell 2's 1.58-1.62 s (EN: 1587, 1575, 1622 ms from the first quiet picture to the explained wait; TR 1566-1591 ms),
the quoted EN/TR texts against `web/src/i18n` at 7c3a05809, the 2c events at 15.18 s, 25.19 s and 40.19 s and the 2d events at 31.65 s
(the FAIL stands exactly as reported: the 15 s abort in `usePanelSession.ts` and the 10 s re-read interval explain 15.175 s, 25.181 s and
40.186 s); cell 3's layer text, samples, clicks and network instants; cell 4's latencies; the identity (tree 8743062c… of 7c3a05809,
empty product diff to e508af230, driver and harness SHA-256). Not reproducible at intake: the owner-password "by value" part of the scan (the
host's value files were removed after the run), so the passwords' absence rests on the scan's report and on the pictures. 
Screencast JPEGs total 28.0 MB (36.4 MB in all `shots/`, set7 37.1 MB), under the ~30 MB bound for pruning; nothing was pruned.
