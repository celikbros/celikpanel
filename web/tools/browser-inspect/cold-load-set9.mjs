// node cold-load-set9.mjs <handover-dir> <out-dir> <cell[,cell...]>
// set9 (2026-10-10): the cold-load fix of e508af230 (RecoveryAccess draws a neutral surface during the first wait,
// explains the wait after the 1.5 s quiet time, offers the reload after 30 s, and replaces the screen at once for a
// known negative) measured in a real headless Chrome against a REAL CelikPanel on a disposable lab guest, reached
// through the lab driver's loopback SSH forward (deploy/e2e/release-recovery/set9_trial.py). set7's cell 5 method
// (web/tools/browser-inspect/live-restart.mjs, cellFlash): a new tab per load in the signed-in browser, HTTP cache
// disabled, the same three routes, the same two network profiles, Chrome's own screencast, and the kind of screen
// at every animation frame. Nothing but the loopback address of the forward is opened.
//
//   cells: flash      cold loads of each route, plain and throttled (2 Mbit/s both ways, 300 ms latency), signed in
//          slow       cold loads while every session read (GET /api/v1/auth/me) is held LIVE_SLOW_HOLD_MS in the
//                     browser (CDP Fetch.requestPaused, then continued unchanged); watched LIVE_SLOW_WATCH_MS
//          negative   known negatives on a cold load: (a) no session (a new browser context: the Panel answers
//                     401), (b) every session read fails as a closed connection (CDP Fetch.failRequest
//                     ConnectionClosed: what this browser saw through the forward while the Panel was down in set7)
//          stopped    the lab driver stops the Panel's service (restart-request-stopN), cold loads while it is
//                     stopped, then the driver starts it again (restart-request-startN) and a cold load must open
//          updatefocus  set7's update cell with one owner-like step: another tab for 1.5 s once the Panel stops
//                     answering (see cellUpdateFocus)
//          done       write done.json: the driver's browser window ends
//
//   BROWSER_INSPECT_MODULES  directory whose node_modules holds puppeteer-core (required; outside the repository)
//   CHROME_PATH              the installed Chrome (default: the usual Windows/Linux places)
//   LIVE_LANG                en (default) | tr: the interface language stored before each load
//   LIVE_FLASH_ROUTES        default /setup,/,/settings?section=updates;  LIVE_FLASH_REPEATS default 3
//   LIVE_FLASH_PROFILES      default plain,throttled
//   LIVE_SLOW_HOLD_MS        default 2500;  LIVE_SLOW_WATCH_MS default 6000;  LIVE_SLOW_ROUTES default /settings?section=updates
//   LIVE_SLOW_REPEATS        default 1;  LIVE_TAG  a word added to the cell's directory name (e.g. "tr", "35s")
//   LIVE_SLOW_PATTERN        CDP Fetch URL pattern of what is held (default */api/v1/auth/me*)
//
// Pictures: Chrome's screencast (a frame is sent when the picture changes). Kept: every frame up to 400 ms after the
// navigation call, then every 5th frame, plus the first frame after each change of the painted kind, the frames
// nearest to 1000 ms and 2000 ms, and the last; the counts received and kept are written per load.
// The owner's password is read from the root-only file the driver names and is typed into the sign-in form; every
// text this script writes is passed through the redaction list of that file first. Cookies are never read.
import { createHash } from 'node:crypto';
import { existsSync, readFileSync } from 'node:fs';
import { appendFile, mkdir, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { join, resolve } from 'node:path';
import { request as httpsRequest } from 'node:https';
import { connect as tlsConnect } from 'node:tls';
import { pathToFileURL } from 'node:url';

if (!process.env.BROWSER_INSPECT_MODULES) { console.error('Set BROWSER_INSPECT_MODULES (see README.md).'); process.exit(2); }
const requireFrom = createRequire(pathToFileURL(join(resolve(process.env.BROWSER_INSPECT_MODULES), 'package.json')));
const loaded = await import(pathToFileURL(requireFrom.resolve('puppeteer-core')).href);
const puppeteer = [loaded.default?.default, loaded.default, loaded].find(item => typeof item?.launch === 'function');
const chrome = process.env.CHROME_PATH || ['C:/Program Files/Google/Chrome/Application/chrome.exe', '/usr/bin/google-chrome', '/usr/bin/chromium'].find(candidate => existsSync(candidate));
if (!chrome) { console.error('No installed Chrome found. Set CHROME_PATH.'); process.exit(2); }

const [handDir, outRoot, cellsArg = 'flash'] = process.argv.slice(2);
if (!handDir || !outRoot) { console.error('usage: node cold-load-set9.mjs <handover-dir> <out-dir> <cells>'); process.exit(2); }
const cells = cellsArg.split(',');
const hand = JSON.parse(readFileSync(join(handDir, 'ready.json'), 'utf8'));
const port = Number(hand.local_port);
if (!(port > 1024 && port < 65536)) throw new Error('bad port in the handover file');
const base = `https://127.0.0.1:${port}`;
const wslShare = process.env.LIVE_WSL_SHARE || '//wsl.localhost/archlinux';
const secretPath = process.platform === 'win32' ? wslShare + hand.password_file : hand.password_file;
const lang = process.env.LIVE_LANG === 'tr' ? 'tr' : 'en';
const tag = (process.env.LIVE_TAG || '').replace(/[^a-z0-9-]/gi, '');
const pause = ms => new Promise(done => setTimeout(done, ms));
const iso = (ms = Date.now()) => new Date(ms).toISOString();

let secret = null;
const redactions = () => [secret?.password, ...(secret?.redact || [])].filter(value => typeof value === 'string' && value.length >= 6);
const redact = text => { let out = String(text ?? ''); for (const value of redactions()) out = out.split(value).join('[REDACTED]'); return out; };
const jsonl = async (file, value) => appendFile(file, redact(JSON.stringify(value)) + '\n');

function leafOf() {
    return new Promise((done, fail) => {
        const socket = tlsConnect({ host: '127.0.0.1', port, rejectUnauthorized: false, servername: 'localhost' }, () => {
            const cert = socket.getPeerX509Certificate();
            socket.end();
            if (!cert) return fail(new Error('no peer certificate'));
            const spki = cert.publicKey.export({ type: 'spki', format: 'der' });
            done({ leaf_sha256: createHash('sha256').update(cert.raw).digest('hex'), spki_sha256_base64: createHash('sha256').update(spki).digest('base64'),
                subject: cert.subject, valid_to: cert.validTo });
        });
        socket.setTimeout(8000, () => { socket.destroy(); fail(new Error('timeout')); });
        socket.on('error', fail);
    });
}
// The public availability route through the same forward, every second (as set7).
function hostProbe(file) {
    let stop = false;
    const seen = [];
    (async () => {
        while (!stop) {
            const started = Date.now();
            const status = await new Promise(done => {
                const req = httpsRequest({ host: '127.0.0.1', port, path: '/api/v1/panel/availability', method: 'GET', rejectUnauthorized: false,
                    headers: { Accept: 'application/json' }, timeout: 2000 }, res => { res.resume(); res.on('end', () => done(String(res.statusCode))); });
                req.on('timeout', () => { req.destroy(new Error('timeout')); });
                req.on('error', error => done('noanswer:' + (error.code || error.message)));
                req.end();
            });
            seen.push({ ms: started, status });
            await jsonl(file, { at: iso(started), ms: started, status, took_ms: Date.now() - started });
            await pause(Math.max(0, 1000 - (Date.now() - started)));
        }
    })();
    return { stop: () => { stop = true; }, last: () => seen[seen.length - 1] };
}

const leaf = await leafOf();
await mkdir(outRoot, { recursive: true });
await writeFile(join(outRoot, `leaf-${Date.now()}.json`), JSON.stringify({ at: iso(), ...leaf, handover_leaf_sha256: hand.tls_leaf_sha256 }, null, 2));
if (leaf.leaf_sha256 !== hand.tls_leaf_sha256) console.error('note: the leaf differs from the one the driver pinned');
secret = JSON.parse(readFileSync(secretPath, 'utf8'));
const profile = join(outRoot, `.chrome-profile-${Date.now()}`);
const browser = await puppeteer.launch({
    executablePath: chrome, headless: true, userDataDir: profile,
    args: ['--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--disable-component-update',
        '--disable-sync', '--disable-domain-reliability', '--force-color-profile=srgb', `--ignore-certificate-errors-spki-list=${leaf.spki_sha256_base64}`],
});
const viewport = { width: 1440, height: 900, deviceScaleFactor: 1 };

// The page's last access decision deadline (valid_until, epoch s), for the timed update cell.
let lastAccess = null;
async function instrument(page, dir, label) {
    const net = join(dir, `network-${label}.jsonl`);
    const ids = new Map();
    let n = 0;
    page.on('request', req => { const id = ++n; ids.set(req, id); void jsonl(net, { at: iso(), ev: 'request', id, method: req.method(), url: req.url().replace(base, ''), type: req.resourceType() }); });
    page.on('response', res => {
        void jsonl(net, { at: iso(), ev: 'response', id: ids.get(res.request()), method: res.request().method(), url: res.url().replace(base, ''), status: res.status(), sw: res.fromServiceWorker() });
        // The access decision the page received (no key, binding or identity is in this answer: cmd/panel/license.go).
        if (res.url().includes('/api/v1/license/access') && res.status() === 200) void res.json().then(body => { if (Number.isSafeInteger(body.valid_until)) lastAccess = { at: Date.now(), until: body.valid_until }; return jsonl(join(dir, `access-${label}.jsonl`), { at: iso(), can_use_panel: body.can_use_panel, valid_until: body.valid_until, valid_until_iso: Number.isSafeInteger(body.valid_until) ? iso(body.valid_until * 1000) : null, state: body.state, observation: body.observation }); }).catch(() => {});
    });
    page.on('requestfailed', req => void jsonl(net, { at: iso(), ev: 'failed', id: ids.get(req), method: req.method(), url: req.url().replace(base, ''), error: req.failure()?.errorText }));
    page.on('framenavigated', frame => { if (frame === page.mainFrame()) void jsonl(net, { at: iso(), ev: 'navigated', url: frame.url().replace(base, '') }); });
    page.on('console', message => { if (['error', 'warn', 'warning'].includes(message.type())) void jsonl(join(dir, `console-${label}.jsonl`), { at: iso(), type: message.type(), text: message.text().slice(0, 400) }); });
    page.on('pageerror', error => void jsonl(join(dir, `console-${label}.jsonl`), { at: iso(), type: 'pageerror', text: String(error.message).slice(0, 400) }));
}
async function newPage(dir, label, context = null) {
    const page = await (context || browser).newPage();
    await page.setViewport(viewport);
    await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'light' }]);
    await page.evaluateOnNewDocument(value => { try { localStorage.setItem('celikpanel.lang', value); localStorage.setItem('celikpanel.theme', 'light'); } catch { /* none */ } }, lang);
    await instrument(page, dir, label);
    return page;
}

async function signIn(page, dir) {
    await page.goto(base + '/', { waitUntil: 'networkidle2', timeout: 60000 });
    const shown = await page.waitForFunction(() => document.getElementById('username') ? 'form' : document.querySelector('a[href="/settings"]') ? 'signed-in' : null, { timeout: 60000 }).then(handle => handle.jsonValue());
    await jsonl(join(dir, 'events.jsonl'), { at: iso(), ev: 'sign-in-page', shown });
    if (shown === 'signed-in') { await page.screenshot({ path: join(dir, '00-already-signed-in.png') }); return; }
    // Taken before anything is typed: the form's fields are empty in this picture.
    await page.screenshot({ path: join(dir, '00-sign-in.png') });
    await page.type('#username', secret.username);
    await page.type('#password', secret.password);
    await Promise.all([page.keyboard.press('Enter'), pause(500)]);
    await page.waitForFunction(() => !document.getElementById('password'), { timeout: 60000 });
    await pause(2500);
}

// The kind of screen at every animation frame (a frame callback runs just before that frame is painted), folded
// into runs of one kind. Kinds, decided in this order:
//   offline-recovery  the recovery service worker's static page (web/recovery-offline.html: [data-copy="title"])
//   access-hold       AccessHold.tsx: [data-top-layer="hold"]
//   login             the sign-in form (#username)
//   app / app-shell-page-loading   the application layout (nav or aside), with its own heading / with only a spinner
//   recovery-access   RecoveryAccess.tsx: a full page under #root with a <header> and a <main> holding an <h1>, no nav
//   quiet             e508af230's neutral surface: [data-access-quiet] and no visible text anywhere
//   empty             #root has no child (before the bundle has run); body text is recorded all the same
//   spinner           a spinner (.animate-spin) and no visible text (the language loader, App's PageLoading)
//   background        something under #root, no text, no button
//   other             anything else with text or a button
// Each run records whether visible text or a visible button was on screen: the rule measured is "no sentence and no
// button before the quiet time (1.5 s) has passed or a read has answered".
const SCREEN_PROBE = watchMs => {
    const t0 = performance.now();
    const frames = [];
    window.__set9 = { t0, timeOrigin: performance.timeOrigin, frames };
    const visible = node => !!node && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== 'hidden';
    const kind = () => {
        const body = document.body;
        const root = document.getElementById('root');
        const text = (body?.innerText || '').replace(/\s+/g, ' ').trim();
        const buttons = Array.from(document.querySelectorAll('button,[role="button"]')).filter(visible).map(node => (node.innerText || node.getAttribute('aria-label') || '').trim() || '(no text)');
        const h1 = Array.from(document.querySelectorAll('h1')).filter(visible).map(node => node.innerText.trim()).filter(Boolean)[0] || null;
        const spinner = !!document.querySelector('.animate-spin');
        const quiet = !!document.querySelector('[data-access-quiet]');
        let k;
        if (document.querySelector('[data-copy="title"]')) k = 'offline-recovery';
        else if (!body) k = 'no-body';
        else if (document.querySelector('[data-top-layer="hold"]')) k = 'access-hold';
        else if (document.getElementById('username')) k = 'login';
        else if (document.querySelector('nav, aside')) k = spinner && !h1 ? 'app-shell-page-loading' : 'app';
        else if (root && root.querySelector('header') && root.querySelector('main h1')) k = 'recovery-access';
        else if (quiet && !text) k = 'quiet';
        else if (!root || root.childElementCount === 0) k = text || buttons.length ? 'other' : 'empty';
        else if (spinner && !text && !buttons.length) k = 'spinner';
        else if (!text && !buttons.length) k = 'background';
        else k = 'other';
        return { kind: k, h1, text: text.slice(0, 240), sentence: text.length > 0, buttons: buttons.slice(0, 4), quiet, spinner, held: !!document.querySelector('[data-access-hold="blocked"]'), path: location.pathname + location.search };
    };
    const frame = () => {
        const value = kind();
        const key = `${value.kind}|${value.h1 || ''}|${value.sentence}|${value.buttons.join('/')}|${value.quiet}|${value.text.slice(0, 80)}`;
        const ms = Math.round(performance.now());
        const lastFrame = frames[frames.length - 1];
        if (lastFrame && lastFrame.key === key) { lastFrame.until = ms; lastFrame.frames++; } else frames.push({ key, ...value, from: ms, until: ms, frames: 1 });
        if (performance.now() - t0 < watchMs) requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
};

// One cold load: a new tab, cache disabled, the probe installed before the document's own scripts, the screencast
// started, the navigation started and not awaited. `prepare(page, cdp, record)` may set CDP rules first.
async function coldLoad(dir, { label, route, context = null, throttle = false, watchMs = 6000, prepare = null, note = null }) {
    const page = await newPage(dir, `load-${label}`, context);
    await page.evaluateOnNewDocument(SCREEN_PROBE, watchMs + 2000);
    await page.setCacheEnabled(false);
    const cdp = await page.createCDPSession();
    if (throttle) await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 300, downloadThroughput: 2e6 / 8, uploadThroughput: 2e6 / 8 });
    const interventions = [];
    if (prepare) await prepare(page, cdp, interventions);
    await mkdir(join(dir, 'shots', label), { recursive: true });
    const pictures = [];
    cdp.on('Page.screencastFrame', ({ data, metadata, sessionId }) => { pictures.push({ at: metadata.timestamp * 1000, data }); cdp.send('Page.screencastFrameAck', { sessionId }).catch(() => {}); });
    await cdp.send('Page.startScreencast', { format: 'jpeg', quality: 60, everyNthFrame: 1 });
    const started = Date.now();
    await jsonl(join(dir, 'events.jsonl'), { at: iso(started), ev: 'load', label, route, lang, cache: 'disabled', context: context ? 'new browser context (no session)' : 'the signed-in browser',
        throttle: throttle ? '2 Mbit/s both ways, 300 ms latency (CDP Network.emulateNetworkConditions)' : null, watch_ms: watchMs, note });
    const navigation = page.goto(base + route, { waitUntil: 'domcontentloaded', timeout: Math.max(20000, watchMs) }).then(() => 'resolved', error => 'not resolved: ' + String(error.message).slice(0, 160));
    await pause(watchMs);
    await cdp.send('Page.stopScreencast').catch(() => {});
    const navigationOutcome = await Promise.race([navigation, pause(15000).then(() => 'still pending after 15 s')]);
    await pause(throttle ? 3000 : 1000);
    const probe = await page.evaluate(() => window.__set9 ? { t0: window.__set9.t0, timeOrigin: window.__set9.timeOrigin, frames: window.__set9.frames.map(({ key, ...rest }) => rest) } : null).catch(error => ({ error: String(error.message).slice(0, 200) }));
    const timing = await page.evaluate(() => { const n = performance.getEntriesByType('navigation')[0]; return n ? { responseStart: Math.round(n.responseStart), domContentLoaded: Math.round(n.domContentLoadedEventEnd), load: Math.round(n.loadEventEnd) } : null; }).catch(() => null);
    const api = await page.evaluate(() => performance.getEntriesByType('resource').filter(e => e.name.includes('/api/')).map(e => ({ path: new URL(e.name).pathname, start: Math.round(e.startTime), end: Math.round(e.responseEnd), status: e.responseStatus }))).catch(() => null);
    const where = await page.evaluate(() => ({ href: location.href.replace(location.origin, ''), protocol: location.protocol, sw_controlled: !!navigator.serviceWorker?.controller, title: document.title })).catch(error => ({ error: String(error.message).slice(0, 160) }));
    await page.screenshot({ path: join(dir, 'shots', label, 'final.png') }).catch(() => {});
    // Pictures: ms from the navigation call; the painted kind at that moment from the frame log.
    const origin = probe?.timeOrigin ?? started;
    const runs = (probe?.frames || []).map(f => ({ ...f, from_goto_ms: Math.round(origin + f.from - started), until_goto_ms: Math.round(origin + f.until - started) }));
    const kindAt = ms => { let found = null; for (const r of runs) { if (r.from_goto_ms <= ms) found = r; else break; } return found ? `${found.kind}${found.h1 ? '(' + found.h1 + ')' : ''}` : 'before the first painted frame'; };
    const received = pictures.map((p, index) => ({ index, ms: Math.round(p.at - started), data: p.data })).filter(p => p.ms <= watchMs);
    const keep = new Set();
    received.forEach((p, i) => { if (p.ms <= 400 || i % 5 === 0 || i === received.length - 1) keep.add(i); });
    for (const r of runs) { const i = received.findIndex(p => p.ms >= r.from_goto_ms); if (i >= 0) keep.add(i); }
    for (const mark of [1000, 2000]) { let best = -1; received.forEach((p, i) => { if (best < 0 || Math.abs(p.ms - mark) < Math.abs(received[best].ms - mark)) best = i; }); if (best >= 0) keep.add(best); }
    const shots = [];
    for (const [i, p] of received.entries()) {
        if (!keep.has(i)) continue;
        const name = `${String(i).padStart(3, '0')}-${String(Math.max(p.ms, 0)).padStart(5, '0')}ms.jpg`;
        await writeFile(join(dir, 'shots', label, name), Buffer.from(p.data, 'base64'));
        shots.push({ file: `shots/${label}/${name}`, ms: p.ms, painted_kind: kindAt(p.ms) });
    }
    const apiFromGoto = (api || []).map(a => ({ ...a, start_goto_ms: Math.round(origin + a.start - started), end_goto_ms: Math.round(origin + a.end - started) }));
    const answered = apiFromGoto.filter(a => a.status > 0).map(a => a.end_goto_ms);
    const firstAnswer = answered.length ? Math.min(...answered) : null;
    const firstMount = runs.find(r => !['empty', 'no-body'].includes(r.kind));
    const worded = runs.filter(r => r.sentence || r.buttons.length);
    const firstWorded = worded[0] || null;
    const record = { label, route, lang, throttle, watch_ms: watchMs, started_at: iso(started), navigation: navigationOutcome, where, navigation_timing: timing,
        time_origin_after_goto_ms: Math.round(origin - started), probe_t0_ms: probe?.t0 != null ? Math.round(probe.t0) : null,
        painted: runs, api_requests: apiFromGoto, first_read_answered_goto_ms: firstAnswer,
        first_mount_goto_ms: firstMount ? firstMount.from_goto_ms : null,
        first_sentence_or_button: firstWorded ? { kind: firstWorded.kind, h1: firstWorded.h1, text: firstWorded.text, buttons: firstWorded.buttons, from_goto_ms: firstWorded.from_goto_ms } : null,
        // The rule: a frame with a sentence or a button painted before a read answered AND before 1.5 s since the
        // first thing was mounted under #root (the quiet time is counted from the waiting component's mount).
        rule_breaking_runs: worded.filter(r => (firstAnswer == null || r.from_goto_ms < firstAnswer) && firstMount && r.from_goto_ms - firstMount.from_goto_ms < 1500)
            .map(r => ({ kind: r.kind, h1: r.h1, text: r.text.slice(0, 120), buttons: r.buttons, from_goto_ms: r.from_goto_ms, until_goto_ms: r.until_goto_ms, frames: r.frames })),
        interventions, pictures_received: received.length, pictures_kept: shots.length, shots };
    await jsonl(join(dir, 'loads.jsonl'), record);
    await Promise.race([cdp.detach().catch(() => {}), pause(3000)]);
    await Promise.race([page.close({ runBeforeUnload: false }).catch(() => {}), pause(5000)]);
    const line = `${label}: first mount ${record.first_mount_goto_ms} ms, first read answered ${firstAnswer} ms, first sentence/button ${firstWorded ? firstWorded.from_goto_ms + ' ms ' + firstWorded.kind + (firstWorded.h1 ? '(' + firstWorded.h1 + ')' : '') : 'none'}; rule-breaking runs ${record.rule_breaking_runs.length}; ` +
        runs.map(r => `${r.from_goto_ms}-${r.until_goto_ms}:${r.kind}${r.h1 ? '(' + r.h1 + ')' : ''}${r.sentence ? '+text' : ''}${r.buttons.length ? '+btn' : ''}`).join(' > ');
    await appendFile(join(dir, 'summary.txt'), redact(line) + '\n');
    return record;
}

const routeLabel = route => route.replace(/[^a-z0-9]+/gi, '_').replace(/^_|_$/g, '') || 'root';

async function signedIn(dir) {
    const signer = await newPage(dir, 'sign-in');
    await signIn(signer, dir);
    await signer.close();
}

async function cellFlash(dir) {
    const routes = (process.env.LIVE_FLASH_ROUTES || '/setup,/,/settings?section=updates').split(',');
    const repeats = Number(process.env.LIVE_FLASH_REPEATS || 3);
    const profiles = (process.env.LIVE_FLASH_PROFILES || 'plain,throttled').split(',');
    await signedIn(dir);
    for (const profile of profiles) for (const route of routes) for (let n = 1; n <= repeats; n++)
        await coldLoad(dir, { label: `${profile}-${routeLabel(route)}-${n}`, route, throttle: profile === 'throttled', watchMs: 6000 });
}

// Every request of the load matching `pattern` (default: GET /api/v1/auth/me, the session read) is held in the
// browser for holdMs, then continued unchanged. LIVE_SLOW_PATTERN="*/assets/SystemUpdateOperation-*" holds instead the
// interface chunk App.tsx's Suspense waits for (its fallback is StandaloneRecovery with cause 'loading').
const SLOW_PATTERN = process.env.LIVE_SLOW_PATTERN || '*/api/v1/auth/me*';
const holdSessionReads = holdMs => async (page, cdp, interventions) => {
    await cdp.send('Fetch.enable', { patterns: [{ urlPattern: SLOW_PATTERN, requestStage: 'Request' }] });
    cdp.on('Fetch.requestPaused', async ({ requestId, request }) => {
        const pausedAt = Date.now();
        await pause(holdMs);
        const outcome = await cdp.send('Fetch.continueRequest', { requestId }).then(() => 'continued', error => 'not continued: ' + String(error.message).slice(0, 120));
        interventions.push({ what: `held ${holdMs} ms, then continued unchanged`, url: request.url.replace(base, ''), paused_at: iso(pausedAt), released_at: iso(), outcome });
    });
};
async function cellSlow(dir) {
    const holdMs = Number(process.env.LIVE_SLOW_HOLD_MS || 2500);
    const watchMs = Number(process.env.LIVE_SLOW_WATCH_MS || 6000);
    const routes = (process.env.LIVE_SLOW_ROUTES || '/settings?section=updates').split(',');
    const repeats = Number(process.env.LIVE_SLOW_REPEATS || 1);
    await signedIn(dir);
    for (const route of routes) for (let n = 1; n <= repeats; n++)
        await coldLoad(dir, { label: `slow${holdMs}-${lang}-${routeLabel(route)}-${n}`, route, watchMs, prepare: holdSessionReads(holdMs),
            note: `every request matching ${SLOW_PATTERN} held ${holdMs} ms in the browser (CDP Fetch), then continued unchanged` });
}

async function cellNegative(dir) {
    const routes = (process.env.LIVE_FLASH_ROUTES || '/setup,/,/settings?section=updates').split(',');
    // (a) No session: a new browser context holds no cookie; the Panel answers the session read with 401.
    const context = await browser.createBrowserContext();
    for (const route of routes) await coldLoad(dir, { label: `nosession-${routeLabel(route)}`, route, context, watchMs: 4000, note: 'a new browser context: no session cookie' });
    await context.close();
    // (b) Signed in; every session read fails as a closed connection.
    await signedIn(dir);
    const failSessionReads = async (page, cdp, interventions) => {
        await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*/api/v1/auth/me*', requestStage: 'Request' }] });
        cdp.on('Fetch.requestPaused', async ({ requestId, request }) => {
            const outcome = await cdp.send('Fetch.failRequest', { requestId, errorReason: 'ConnectionClosed' }).then(() => 'failed as ConnectionClosed', error => 'not failed: ' + String(error.message).slice(0, 120));
            interventions.push({ what: 'failed in the browser as a closed connection (CDP Fetch.failRequest ConnectionClosed)', url: request.url.replace(base, ''), at: iso(), outcome });
        });
    };
    for (const route of routes) await coldLoad(dir, { label: `sessionreadfails-${routeLabel(route)}`, route, watchMs: 4000, prepare: failSessionReads,
        note: 'every session read failed in the browser as a closed connection; the document and the interface came from the Panel' });
}

async function driverRequest(verb, events) {
    const number = `${verb}${Date.now()}`;
    await writeFile(join(handDir, `restart-request-${number}`), iso() + '\n');
    await jsonl(events, { at: iso(), ev: `${verb}-requested`, number });
    for (let i = 0; i < 300 && !existsSync(join(handDir, `restart-done-${number}.json`)); i++) await pause(1000);
    const record = existsSync(join(handDir, `restart-done-${number}.json`)) ? JSON.parse(readFileSync(join(handDir, `restart-done-${number}.json`), 'utf8')) : { missing: true };
    await jsonl(events, { at: iso(), ev: `${verb}-done`, record });
    return record;
}
async function cellStopped(dir) {
    const routes = (process.env.LIVE_FLASH_ROUTES || '/setup,/,/settings?section=updates').split(',');
    const events = join(dir, 'events.jsonl');
    await signedIn(dir);
    // What this browser holds before the Panel stops: the recovery service worker's registration (read, not changed).
    const look = await newPage(dir, 'before-stop');
    await look.goto(base + '/', { waitUntil: 'networkidle2', timeout: 60000 });
    await pause(4000);
    const registration = await look.evaluate(async () => { const r = await navigator.serviceWorker?.getRegistration('/'); return { registered: !!r, active: r?.active ? new URL(r.active.scriptURL).pathname : null, state: r?.active?.state ?? null, controlled: !!navigator.serviceWorker?.controller }; }).catch(error => ({ error: String(error.message).slice(0, 200) }));
    await jsonl(events, { at: iso(), ev: 'service-worker-before-stop', registration });
    await look.close();
    const probe = hostProbe(join(dir, 'host-probe.jsonl'));
    await driverRequest('stop', events);
    for (let i = 0; i < 30 && !String(probe.last()?.status || '').startsWith('noanswer'); i++) await pause(1000);
    await jsonl(events, { at: iso(), ev: 'host-probe-before-loads', last: probe.last() });
    for (const route of routes) await coldLoad(dir, { label: `stopped-${routeLabel(route)}`, route, watchMs: 6000, note: 'the Panel service stopped by the harness' });
    await driverRequest('start', events);
    for (let i = 0; i < 60 && probe.last()?.status !== '401' && probe.last()?.status !== '200'; i++) await pause(1000);
    await jsonl(events, { at: iso(), ev: 'host-probe-after-start', last: probe.last() });
    await pause(5000);
    await coldLoad(dir, { label: 'after-start-root', route: '/', watchMs: 6000, note: 'the Panel started again by the harness' });
    probe.stop();
}

// updatefocus: set7's update cell (live-restart.mjs cellUpdate: Settings -> updates, check, start from the card, every
// 2 s a screenshot and the DOM state until the Panel has been down and the page is steady for 60 s), with ONE owner-like
// step added: 2 s after the host probe first sees the Panel not answering, another tab of the same browser is brought
// to the front for 1.5 s and the measured tab back (the owner looks at another tab and returns). Returning focuses
// the window, and LicenseOnboarding reads license/access on focus; in set9's first update attempt the only access read
// that fell inside the restart was 18 s into it, the next one answered, and no hold layer was drawn.
async function observe(page) {
    return page.evaluate(() => {
        const text = node => (node?.innerText || '').replace(/[ \t]+\n/g, '\n').replace(/\n{3,}/g, '\n\n').trim();
        const hold = document.querySelector('[data-top-layer="hold"]');
        const update = document.querySelector('[aria-labelledby="system-update-operation-title"]');
        const selectedTab = document.querySelector('[role="tab"][aria-selected="true"]');
        const w = innerWidth, h = innerHeight;
        const top = (x, y) => { const node = document.elementFromPoint(x, y); return node ? { tag: node.tagName.toLowerCase(), inHold: !!hold?.contains(node), inert: !!node.closest('[inert]') } : null; };
        const holdTitle = hold ? (hold.querySelector('h1,h2,h3,[id$="-title"]')?.innerText || '').trim() : null;
        return {
            document: window.__set9Document ?? null, path: location.pathname + location.search, visibility: document.visibilityState,
            hold: hold ? text(hold).slice(0, 1500) : null, hold_title: holdTitle,
            held_subtree: !!document.querySelector('[data-access-hold="blocked"]'),
            held_inert: !!document.querySelector('[data-access-hold="blocked"][inert], [data-access-hold="blocked"] [inert]'),
            update_window: update ? text(update).slice(0, 1500) : null,
            settings_updates_panel: !!document.getElementById('settings-updates-panel'),
            selected_tab: selectedTab ? selectedTab.id || selectedTab.innerText.trim() : null,
            centre: top(w / 2, h / 2), corner: top(30, 30),
            update_marker_phase: (() => { try { return JSON.parse(localStorage.getItem('celikpanel.system-update-operation.v1') || 'null')?.phase ?? null; } catch { return 'unreadable'; } })(),
            body: text(document.body).slice(0, 1200),
        };
    });
}
async function cellUpdateFocus(dir) {
    const events = join(dir, 'events.jsonl');
    const page = await newPage(dir, 'update');
    const signer = page;
    await signIn(signer, dir);
    await page.goto(base + '/settings?section=updates', { waitUntil: 'networkidle2', timeout: 60000 });
    await page.waitForSelector('#panel-update-title', { timeout: 60000 });
    await pause(1500);
    const docMarker = await page.evaluate(value => { window.__set9Document = value; return value; }, String(Date.now()));
    await page.screenshot({ path: join(dir, '01-settings-updates.png') });
    await jsonl(events, { at: iso(), ev: 'settings-updates-open', document: docMarker, state: await observe(page) });
    await (await page.$('section[aria-labelledby="panel-update-title"] button')).click();
    await jsonl(events, { at: iso(), ev: 'check-clicked' });
    await page.waitForFunction(() => { const b = document.getElementById('panel-update-start-button'); return b && !b.disabled; }, { timeout: 180000 });
    await pause(1000);
    await page.screenshot({ path: join(dir, '02-update-offered.png') });
    await jsonl(events, { at: iso(), ev: 'offered', state: await observe(page) });
    // LIVE_UPDATE_TIMED=1 (set9 lab 3): the owner's click is placed so that the update's restart is expected to
    // cover the page's access deadline: AccessHold draws its layer only when the decision's valid_until passes while
    // the Panel does not answer (the refresh read 15 s before it fails, LicenseOnboarding.tsx). The deadlines lie on
    // the server's 45 s grid (lab 2's access-accesswatch.jsonl); the click is LIVE_UPDATE_LEAD_MS (default 41500)
    // before the next deadline that leaves at least 3 s: in set9's two runs the Panel stopped answering 19-25 s after
    // the click and answered again 23-28 s later.
    const timed = process.env.LIVE_UPDATE_TIMED === '1';
    if (timed) {
        const lead = Number(process.env.LIVE_UPDATE_LEAD_MS || 41500);
        if (!lastAccess) throw new Error('no access answer recorded before the timed click');
        let deadline = lastAccess.until * 1000;
        while (deadline - lead < Date.now() + 3000) deadline += 45000;
        await jsonl(events, { at: iso(), ev: 'timed-click-plan', last_access_until: iso(lastAccess.until * 1000), target_deadline: iso(deadline), click_at: iso(deadline - lead), lead_ms: lead });
        await pause(Math.max(0, deadline - lead - Date.now()));
    }
    const probe = hostProbe(join(dir, 'host-probe.jsonl'));
    const startedAt = Date.now();
    await page.click('#panel-update-start-button');
    await jsonl(events, { at: iso(startedAt), ev: 'start-clicked' });
    await mkdir(join(dir, 'shots'), { recursive: true });
    const netFile = join(dir, 'network-update.jsonl');
    const failuresSince = since => { try { return readFileSync(netFile, 'utf8').split('\n').filter(Boolean).map(line => JSON.parse(line)).filter(e => Date.parse(e.at) >= since && (e.ev === 'failed' || (e.ev === 'response' && e.status >= 500))).length; } catch { return 0; } };
    let sawDown = false, steadySince = Date.now(), lastKey = null, nudged = false, downSeenAt = null, index = 0;
    while (Date.now() - startedAt < 40 * 60000) {
        const at = Date.now();
        if (!downSeenAt && String(probe.last()?.status || '').startsWith('noanswer')) downSeenAt = probe.last().ms;
        if (downSeenAt && !nudged && !timed && at - downSeenAt >= 2000) {
            nudged = true;
            const other = await newPage(dir, 'update-other-tab');
            await other.goto('about:blank');
            await other.bringToFront();
            await jsonl(events, { at: iso(), ev: 'other-tab-front', method: 'a second tab of the same browser brought to the front (page.bringToFront)', host_probe_down_since: iso(downSeenAt) });
            await pause(1500);
            await page.bringToFront();
            await jsonl(events, { at: iso(), ev: 'measured-tab-back', visibility: await page.evaluate(() => document.visibilityState).catch(() => null) });
            await other.close();
        }
        let state;
        try { state = await observe(page); } catch (error) { state = { error: String(error.message).slice(0, 300) }; }
        const name = `update-${String(index).padStart(3, '0')}-${iso(at).slice(11, 19).replaceAll(':', '')}`;
        const asPng = index % 10 === 0 || !!state.hold;
        try { await page.screenshot({ path: join(dir, 'shots', name + (asPng ? '.png' : '.jpg')), ...(asPng ? {} : { type: 'jpeg', quality: 70 }) }); } catch (error) { state.screenshot_error = String(error.message).slice(0, 200); }
        await jsonl(join(dir, 'samples-update.jsonl'), { at: iso(at), t_s: Math.round((at - startedAt) / 100) / 10, shot: `shots/${name}${asPng ? '.png' : '.jpg'}`, ...state });
        index++;
        if (!sawDown && failuresSince(startedAt) > 0) sawDown = true;
        const key = `${state.document}|${state.path}|${state.hold}|${state.update_window}`;
        if (state.hold || state.error || key !== lastKey || failuresSince(Date.now() - 4000) > 0) { steadySince = Date.now(); lastKey = key; }
        else if (sawDown && Date.now() - steadySince > 60000) break;
        await pause(Math.max(0, 2000 - (Date.now() - at)));
    }
    probe.stop();
    await page.screenshot({ path: join(dir, '03-steady-after-update.png') });
    await jsonl(events, { at: iso(), ev: 'steady', saw_down: sawDown, nudged, state: await observe(page).catch(() => null) });
    await page.close();
}

const results = {};
for (const cell of cells) {
    const dir = join(outRoot, tag ? `${cell}-${tag}` : cell);
    await mkdir(dir, { recursive: true });
    const started = iso();
    try {
        if (cell === 'flash') await cellFlash(dir);
        else if (cell === 'slow') await cellSlow(dir);
        else if (cell === 'negative') await cellNegative(dir);
        else if (cell === 'stopped') await cellStopped(dir);
        else if (cell === 'updatefocus') await cellUpdateFocus(dir);
        else if (cell === 'accesswatch') {
            // Read only: Settings -> updates left open for LIVE_WATCH_MS; the page's own access reads are recorded.
            const page = await newPage(dir, 'accesswatch');
            await signIn(page, dir);
            await page.goto(base + '/settings?section=updates', { waitUntil: 'networkidle2', timeout: 60000 });
            await pause(Number(process.env.LIVE_WATCH_MS || 150000));
            await page.close();
        }
        else if (cell === 'done') await writeFile(join(handDir, 'done.json'), JSON.stringify({ at: iso(), by: 'cold-load-set9.mjs' }) + '\n');
        else throw new Error(`unknown cell ${cell}`);
        results[cell] = { started, ended: iso(), ok: true };
    } catch (error) {
        results[cell] = { started, ended: iso(), ok: false, error: redact(String(error.stack || error)).slice(0, 1500) };
    }
    await writeFile(join(dir, 'run.json'), redact(JSON.stringify({ cell, lang, tag, chrome: await browser.version(), handover: { ...hand, password_file: '(root-only file, not copied)' }, leaf, ...results[cell] }, null, 2)));
    console.log(JSON.stringify({ cell, ...results[cell] }));
}
await browser.close();
