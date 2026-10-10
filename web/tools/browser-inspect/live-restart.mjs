// node live-restart.mjs <handover-dir> <out-dir> <cell[,cell...]>
// set7 (2026-10-10): drives a real headless Chrome against a REAL CelikPanel on a disposable lab guest, reached
// through the lab driver's loopback SSH forward (deploy/e2e/release-recovery/set7_trial.py), and records what the
// interface shows while the Panel restarts. Unlike run.mjs there is no mock: the address is the loopback port the
// handover file names, and nothing else is opened.
//
//   cells: probe          sign in, open the pages cells 3 and 4 use, list their fields (no change)
//          update         Settings -> updates: check, start the update from the card, then every 2 s a screenshot
//                         and the DOM state until the Panel is back and the page is steady
//          hidden         a page with an open form and typed text; another tab is brought to the front for
//                         HIDDEN_MS (default 10.5 min) while the Panel runs; the tab is brought back
//          hiddenrestart  the same, and while the tab is hidden the lab driver restarts the Panel's service
//                         (restart-request-N in the handover directory; the driver answers restart-done-N.json)
//          done           write done.json: the driver's browser window ends
//
//   BROWSER_INSPECT_MODULES  directory whose node_modules holds puppeteer-core (required; outside the repository)
//   CHROME_PATH              the installed Chrome (default: the usual Windows/Linux places)
//   LIVE_TYPED_TARGET        cells 3/4: which page holds the typed text (see TARGETS below; default "settings-panel")
//   LIVE_HIDDEN_MS           cells 3/4: how long the tab stays in the background
//
// The owner's password is read from the root-only file the driver names and is typed into the sign-in form; every
// text this script writes is passed through the redaction list of that file first. Cookies are never read.
import { createHash, randomBytes, X509Certificate } from 'node:crypto';
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

const [handDir, outRoot, cellsArg = 'probe'] = process.argv.slice(2);
if (!handDir || !outRoot) { console.error('usage: node live-restart.mjs <handover-dir> <out-dir> <cells>'); process.exit(2); }
const cells = cellsArg.split(',');
const hand = JSON.parse(readFileSync(join(handDir, 'ready.json'), 'utf8'));
const port = Number(hand.local_port);
if (!(port > 1024 && port < 65536)) throw new Error('bad port in the handover file');
const base = `https://127.0.0.1:${port}`;
// The handover names a Linux path; on Windows it is read through the WSL share of the lab host.
const wslShare = process.env.LIVE_WSL_SHARE || '//wsl.localhost/archlinux';
const secretPath = process.platform === 'win32' ? wslShare + hand.password_file : hand.password_file;
const pause = ms => new Promise(done => setTimeout(done, ms));
const iso = (ms = Date.now()) => new Date(ms).toISOString();

let secret = null;
const redactions = () => [secret?.password, ...(secret?.redact || [])].filter(value => typeof value === 'string' && value.length >= 6);
const redact = text => { let out = String(text ?? ''); for (const value of redactions()) out = out.split(value).join('[REDACTED]'); return out; };
const jsonl = async (file, value) => appendFile(file, redact(JSON.stringify(value)) + '\n');

// The leaf the guest Panel serves, read over the same forward; its SPKI makes this one certificate valid for this
// one browser profile (--ignore-certificate-errors-spki-list), so the page is a secure context as on a real server.
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
// A host-side probe outside the browser: the public availability route through the same forward, every second.
function hostProbe(file) {
    let stop = false;
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
            await jsonl(file, { at: iso(started), ms: started, status, took_ms: Date.now() - started });
            await pause(Math.max(0, 1000 - (Date.now() - started)));
        }
    })();
    return () => { stop = true; };
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

async function instrument(page, dir, label) {
    const net = join(dir, `network-${label}.jsonl`);
    const ids = new Map();
    let n = 0;
    page.on('request', req => { const id = ++n; ids.set(req, id); void jsonl(net, { at: iso(), ev: 'request', id, method: req.method(), url: req.url().replace(base, ''), type: req.resourceType() }); });
    page.on('response', res => void jsonl(net, { at: iso(), ev: 'response', id: ids.get(res.request()), method: res.request().method(), url: res.url().replace(base, ''), status: res.status(), sw: res.fromServiceWorker() }));
    page.on('requestfailed', req => void jsonl(net, { at: iso(), ev: 'failed', id: ids.get(req), method: req.method(), url: req.url().replace(base, ''), error: req.failure()?.errorText }));
    page.on('framenavigated', frame => { if (frame === page.mainFrame()) void jsonl(net, { at: iso(), ev: 'navigated', url: frame.url().replace(base, '') }); });
    page.on('console', message => { if (['error', 'warn', 'warning'].includes(message.type())) void jsonl(join(dir, `console-${label}.jsonl`), { at: iso(), type: message.type(), text: message.text().slice(0, 400) }); });
    page.on('pageerror', error => void jsonl(join(dir, `console-${label}.jsonl`), { at: iso(), type: 'pageerror', text: String(error.message).slice(0, 400) }));
}
async function newPage(dir, label) {
    const page = await browser.newPage();
    await page.setViewport(viewport);
    await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'light' }]);
    await page.evaluateOnNewDocument(() => { try { localStorage.setItem('celikpanel.lang', 'en'); localStorage.setItem('celikpanel.theme', 'light'); } catch { /* none */ } });
    await instrument(page, dir, label);
    return page;
}
// The document marker: set once per measured page; a sample without it is a new document (a reload or a move).
const setMarker = page => page.evaluate(value => { window.__set7Document = value; return value; }, randomBytes(6).toString('hex'));

async function observe(page) {
    return page.evaluate(() => {
        const text = node => (node?.innerText || '').replace(/[ \t]+\n/g, '\n').replace(/\n{3,}/g, '\n\n').trim();
        const hold = document.querySelector('[data-top-layer="hold"]');
        const update = document.querySelector('[aria-labelledby="system-update-operation-title"]');
        const reload = document.querySelector('[data-top-layer="reload"]');
        const startButton = document.getElementById('panel-update-start-button');
        const updatesPanel = document.getElementById('settings-updates-panel');
        const selectedTab = document.querySelector('[role="tab"][aria-selected="true"]');
        const headings = Array.from(document.querySelectorAll('h1,h2')).filter(node => node.getClientRects().length > 0).map(node => node.innerText.trim()).filter(Boolean).slice(0, 12);
        const typed = Array.from(document.querySelectorAll('input[data-set7-typed]')).map(node => ({ value: node.value, inert: !!node.closest('[inert]'), focused: document.activeElement === node }));
        const w = innerWidth, h = innerHeight;
        const top = (x, y) => { const node = document.elementFromPoint(x, y); return node ? { tag: node.tagName.toLowerCase(), inHold: !!hold?.contains(node), inUpdate: !!update?.contains(node), inert: !!node.closest('[inert]') } : null; };
        return {
            document: window.__set7Document ?? null, path: location.pathname + location.search, visibility: document.visibilityState, title: document.title,
            secure_context: window.isSecureContext, service_worker_controlled: !!navigator.serviceWorker?.controller,
            hold: hold ? text(hold).slice(0, 1500) : null,
            held_subtree: !!document.querySelector('[data-access-hold="blocked"]'),
            update_window: update ? text(update).slice(0, 1500) : null,
            reload_notice: reload ? text(reload).slice(0, 600) : null,
            dialogs: Array.from(document.querySelectorAll('[role="dialog"],[role="alertdialog"]')).map(node => node.getAttribute('aria-labelledby') || node.id || node.tagName),
            headings,
            settings_updates_panel: updatesPanel ? { mounted: true, hidden: updatesPanel.hidden } : { mounted: false },
            selected_tab: selectedTab ? selectedTab.id || selectedTab.innerText.trim() : null,
            update_card: !!document.getElementById('panel-update-title'),
            start_button: startButton ? { text: startButton.innerText.trim(), disabled: startButton.disabled, inert: !!startButton.closest('[inert]') } : null,
            typed,
            centre: top(w / 2, h / 2), corner: top(30, 30),
            active: document.activeElement ? `${document.activeElement.tagName.toLowerCase()}${document.activeElement.id ? '#' + document.activeElement.id : ''}` : null,
            body: text(document.body).slice(0, 2400),
        };
    });
}

async function signIn(page, dir) {
    await page.goto(base + '/', { waitUntil: 'networkidle2', timeout: 60000 });
    // A browser that signed in for an earlier cell still holds that session: no sign-in form is shown then.
    const shown = await page.waitForFunction(() => document.getElementById('username') ? 'form' : document.querySelector('a[href="/settings"]') ? 'signed-in' : null, { timeout: 60000 }).then(handle => handle.jsonValue());
    await jsonl(join(dir, 'events.jsonl'), { at: iso(), ev: 'sign-in-page', shown });
    if (shown === 'signed-in') { await page.screenshot({ path: join(dir, '00-already-signed-in.png') }); return; }
    await page.screenshot({ path: join(dir, '00-sign-in.png') });
    await page.type('#username', secret.username);
    await page.type('#password', secret.password);
    await Promise.all([page.keyboard.press('Enter'), pause(500)]);
    await page.waitForFunction(() => !document.getElementById('password'), { timeout: 60000 });
    await pause(2500);
}

async function sampler(page, dir, label, { every = 2000, until, maxMs, png = () => false }) {
    const file = join(dir, `samples-${label}.jsonl`);
    await mkdir(join(dir, 'shots'), { recursive: true });
    const started = Date.now();
    let index = 0, last = null;
    while (Date.now() - started < maxMs) {
        const at = Date.now();
        let state;
        try { state = await observe(page); } catch (error) { state = { error: String(error.message).slice(0, 300) }; }
        const name = `${label}-${String(index).padStart(3, '0')}-${iso(at).slice(11, 19).replaceAll(':', '')}`;
        const asPng = png(state, index);
        try { await page.screenshot({ path: join(dir, 'shots', name + (asPng ? '.png' : '.jpg')), ...(asPng ? {} : { type: 'jpeg', quality: 70 }) }); } catch (error) { state.screenshot_error = String(error.message).slice(0, 200); }
        await jsonl(file, { at: iso(at), t_s: Math.round((at - started) / 100) / 10, shot: `shots/${name}${asPng ? '.png' : '.jpg'}`, ...state });
        last = state;
        index++;
        if (until && until(state, index, Date.now() - started)) break;
        await pause(Math.max(0, every - (Date.now() - at)));
    }
    return last;
}

// -- cells ------------------------------------------------------------------------------------------------------

const TARGETS = {
    // A page every setup state allows (isSetupRecoveryPath): Settings, DNS section; text typed into its first field.
    'settings-dns': { path: '/settings?section=dns', field: 'input[type="text"],input:not([type]),textarea' },
    'settings-panel': { path: '/settings?section=panel', field: '#panel-certificate-domain' },
    'domain': { path: `/domains/${hand.domain}`, field: 'input[type="text"],input:not([type]),textarea' },
};

async function cellProbe(dir) {
    const page = await newPage(dir, 'probe');
    await signIn(page, dir);
    const found = {};
    for (const [name, target] of Object.entries(TARGETS)) {
        await page.goto(base + target.path, { waitUntil: 'networkidle2', timeout: 60000 });
        await pause(2500);
        await page.screenshot({ path: join(dir, `probe-${name}.png`), fullPage: true });
        found[name] = await page.evaluate(selector => ({
            path: location.pathname + location.search,
            fields: Array.from(document.querySelectorAll(selector)).map(node => ({ id: node.id, name: node.name, placeholder: node.placeholder, visible: node.getClientRects().length > 0, disabled: node.disabled })),
            buttons: Array.from(document.querySelectorAll('main button')).map(node => node.innerText.trim()).filter(Boolean).slice(0, 40),
            tabs: Array.from(document.querySelectorAll('[role="tab"]')).map(node => `${node.id}:${node.innerText.trim()}`),
        }), target.field);
    }
    await writeFile(join(dir, 'probe.json'), redact(JSON.stringify(found, null, 2)));
    await page.close();
}

async function cellUpdate(dir) {
    const page = await newPage(dir, 'update');
    await signIn(page, dir);
    await page.goto(base + '/settings?section=updates', { waitUntil: 'networkidle2', timeout: 60000 });
    await page.waitForSelector('#panel-update-title', { timeout: 60000 });
    await pause(1500);
    const docMarker = await setMarker(page);
    await page.screenshot({ path: join(dir, '01-settings-updates.png') });
    await jsonl(join(dir, 'events.jsonl'), { at: iso(), ev: 'settings-updates-open', document: docMarker, state: await observe(page) });
    const check = await page.$('section[aria-labelledby="panel-update-title"] button');
    await check.click();
    await jsonl(join(dir, 'events.jsonl'), { at: iso(), ev: 'check-clicked' });
    await page.waitForFunction(() => { const b = document.getElementById('panel-update-start-button'); return b && !b.disabled; }, { timeout: 180000 });
    await pause(1000);
    await page.screenshot({ path: join(dir, '02-update-offered.png') });
    await jsonl(join(dir, 'events.jsonl'), { at: iso(), ev: 'offered', state: await observe(page) });
    const stopProbe = hostProbe(join(dir, 'host-probe.jsonl'));
    const startedAt = Date.now();
    await page.click('#panel-update-start-button');
    await jsonl(join(dir, 'events.jsonl'), { at: iso(startedAt), ev: 'start-clicked' });
    // Until: the Panel has been seen down (a failed or 5xx request) and has then been steady for 60 s with no hold
    // layer, no failed request and no change of the update window's text; at most 40 minutes.
    let sawDown = false, steadySince = null, lastText = null;
    const netFile = join(dir, 'network-update.jsonl');
    const failuresSince = since => { try { return readFileSync(netFile, 'utf8').split('\n').filter(Boolean).map(line => JSON.parse(line)).filter(e => Date.parse(e.at) >= since && (e.ev === 'failed' || (e.ev === 'response' && e.status >= 500))).length; } catch { return 0; } };
    const last = await sampler(page, dir, 'update', {
        every: 2000, maxMs: 40 * 60000,
        png: (state, index) => index % 10 === 0 || !!state.hold,
        until: (state, index, elapsed) => {
            const now = Date.now();
            if (!sawDown && failuresSince(startedAt) > 0) sawDown = true;
            const key = `${state.document}|${state.path}|${state.hold}|${state.update_window}|${state.reload_notice}`;
            if (state.hold || state.error || key !== lastText || failuresSince(now - 4000) > 0) { steadySince = now; lastText = key; return false; }
            return sawDown && now - steadySince > 60000;
        },
    });
    stopProbe();
    await page.screenshot({ path: join(dir, '03-steady-after-update.png') });
    await jsonl(join(dir, 'events.jsonl'), { at: iso(), ev: 'steady', saw_down: sawDown, state: last });
    // The window the update leaves open: its own buttons are listed; one that closes it is pressed once, so the page
    // underneath can be seen.
    const buttons = await page.$$eval('[aria-labelledby="system-update-operation-title"] button', nodes => nodes.map(node => node.innerText.trim()));
    await jsonl(join(dir, 'events.jsonl'), { at: iso(), ev: 'update-window-buttons', buttons });
    const closer = (await page.$$('[aria-labelledby="system-update-operation-title"] button'))[buttons.findIndex(text => /^(close|done|continue|ok|dismiss)/i.test(text))];
    if (closer) {
        await closer.click();
        await pause(2000);
        await page.screenshot({ path: join(dir, '04-after-closing-the-update-window.png') });
        await jsonl(join(dir, 'events.jsonl'), { at: iso(), ev: 'closed-update-window', state: await observe(page) });
    }
    await page.close();
}

async function typedPage(dir, label) {
    const name = process.env.LIVE_TYPED_TARGET || 'settings-panel';
    const target = TARGETS[name];
    const page = await newPage(dir, label);
    await signIn(page, dir);
    await page.goto(base + target.path, { waitUntil: 'networkidle2', timeout: 60000 });
    await pause(2500);
    const field = (await page.$$(target.field)).find(Boolean);
    const typed = `set7-typed-${randomBytes(3).toString('hex')}`;
    if (field) {
        await field.evaluate(node => node.setAttribute('data-set7-typed', '1'));
        await field.click({ clickCount: 3 });
        await field.type(typed);
    }
    const docMarker = await setMarker(page);
    return { page, typed: field ? typed : null, target: name, document: docMarker };
}

async function cellHidden(dir, label, restart) {
    const { page, typed, target, document: docMarker } = await typedPage(dir, label);
    const events = join(dir, 'events.jsonl');
    await page.screenshot({ path: join(dir, '01-before-hidden.png') });
    await jsonl(events, { at: iso(), ev: 'before-hidden', target, typed, document: docMarker, state: await observe(page) });
    const stopProbe = hostProbe(join(dir, 'host-probe.jsonl'));
    const other = await newPage(dir, `${label}-other-tab`);
    await other.goto('about:blank');
    await other.bringToFront();
    await pause(500);
    const hiddenAt = Date.now();
    await jsonl(events, { at: iso(hiddenAt), ev: 'other-tab-front', method: 'a second tab of the same browser was brought to the front (puppeteer page.bringToFront)', visibility: await page.evaluate(() => document.visibilityState) });
    const hiddenMs = Number(process.env.LIVE_HIDDEN_MS || 630000);
    let restarted = null;
    while (Date.now() - hiddenAt < hiddenMs) {
        if (restart && !restarted && Date.now() - hiddenAt > 150000) {
            const number = String(Date.now());
            await writeFile(join(handDir, `restart-request-${number}`), iso() + '\n');
            await jsonl(events, { at: iso(), ev: 'restart-requested', number });
            for (let i = 0; i < 300 && !existsSync(join(handDir, `restart-done-${number}.json`)); i++) await pause(1000);
            restarted = existsSync(join(handDir, `restart-done-${number}.json`)) ? JSON.parse(readFileSync(join(handDir, `restart-done-${number}.json`), 'utf8')) : { missing: true };
            await jsonl(events, { at: iso(), ev: 'restart-done', record: restarted });
        }
        let state;
        try { state = await page.evaluate(() => ({ visibility: document.visibilityState, document: window.__set7Document ?? null, path: location.pathname + location.search, hold: !!document.querySelector('[data-top-layer="hold"]'), held_subtree: !!document.querySelector('[data-access-hold="blocked"]'), typed: Array.from(document.querySelectorAll('input[data-set7-typed]')).map(node => node.value) })); } catch (error) { state = { error: String(error.message).slice(0, 200) }; }
        await jsonl(join(dir, `hidden-samples-${label}.jsonl`), { at: iso(), t_s: Math.round((Date.now() - hiddenAt) / 1000), ...state });
        await pause(15000);
    }
    await page.bringToFront();
    const back = Date.now();
    await jsonl(events, { at: iso(back), ev: 'tab-back', hidden_seconds: Math.round((back - hiddenAt) / 1000) });
    const last = await sampler(page, dir, `${label}-back`, { every: 2000, maxMs: 60000, png: (state, index) => index < 6 || index % 5 === 0 || !!state.hold });
    stopProbe();
    await page.screenshot({ path: join(dir, '02-back-steady.png') });
    await jsonl(events, { at: iso(), ev: 'back-steady', typed_expected: typed, state: last });
    await other.close();
    await page.close();
}

// Cell 5 (flash): a full load of a route while signed in and with the Panel healthy. From the first byte of the
// document a MutationObserver writes every change of "what is on screen" with performance.now(); a screenshot is taken
// about every 200 ms for the first 6 s. Which component drew a screen is told by its markers:
//   recovery-access  RecoveryAccess.tsx: the full page <div class="min-h-screen ..."> whose <main> holds an <h1> with
//                    a recovery title (en: 'Checking panel access' while waiting) and the "Reload CelikPanel" button
//   access-hold      AccessHold.tsx: [data-top-layer="hold"] (and [data-access-hold="blocked"] around the pages)
//   page-loading     App.tsx PageLoading: a spinner box (.min-h-64) with no h1, inside or outside the layout
//   app              the application layout (a link to /settings in the navigation) and the route's own heading
//   empty            #root has no child yet (before the bundle has run)
const SCREEN_PROBE = () => {
    const t0 = performance.now();
    const log = [];
    window.__set7Screens = log;
    const kind = () => {
        const root = document.getElementById('root');
        if (!root || root.childElementCount === 0) return { kind: 'empty' };
        const hold = document.querySelector('[data-top-layer="hold"]');
        const h1 = Array.from(document.querySelectorAll('h1')).map(node => node.innerText.trim()).filter(Boolean)[0] || null;
        const appShell = !!document.querySelector('nav, aside');
        const reloadButton = Array.from(document.querySelectorAll('button')).some(node => /^(Reload CelikPanel|CelikPanel'i yeniden yükle)/i.test(node.innerText.trim()));
        const recovery = !appShell && reloadButton ? document.querySelector('main h1') : null;
        const buttons = Array.from(document.querySelectorAll('button')).map(node => node.innerText.trim()).filter(Boolean).slice(0, 4);
        if (hold) return { kind: 'access-hold', h1, hold: hold.innerText.trim().slice(0, 120), blocked: !!document.querySelector('[data-access-hold="blocked"]') };
        if (recovery) return { kind: 'recovery-access', h1: recovery.innerText.trim(), status: (root.querySelector('main [role="status"]')?.innerText || '').trim().slice(0, 160), buttons };
        const app = !!document.querySelector('nav, aside');
        const spinner = !!root.querySelector('.min-h-64, .animate-spin');
        return { kind: app ? (spinner && !h1 ? 'app-shell-page-loading' : 'app') : (spinner ? 'page-loading' : 'other'), h1, path: location.pathname + location.search };
    };
    let last = '';
    const note = () => {
        const value = kind();
        const key = JSON.stringify(value);
        if (key !== last) { last = key; log.push({ ms: Math.round(performance.now() - t0), ...value }); }
    };
    // What reaches the screen: the kind present at each animation frame (a frame callback runs just before that
    // frame is painted), for the first 8 s; consecutive frames of one kind are folded into one entry.
    const frames = [];
    window.__set7Frames = frames;
    const frame = () => {
        const value = kind();
        const key = value.kind + '|' + (value.h1 || '');
        const ms = Math.round(performance.now() - t0);
        const lastFrame = frames[frames.length - 1];
        if (lastFrame && lastFrame.key === key) { lastFrame.until = ms; lastFrame.frames++; } else frames.push({ key, kind: value.kind, h1: value.h1 || null, from: ms, until: ms, frames: 1 });
        if (performance.now() - t0 < 8000) requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
    new MutationObserver(note).observe(document, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ['class', 'data-top-layer', 'data-access-hold', 'hidden'] });
    document.addEventListener('DOMContentLoaded', note);
    note();
};
async function cellFlash(dir) {
    const routes = (process.env.LIVE_FLASH_ROUTES || '/setup,/,/settings?section=updates').split(',');
    const repeats = Number(process.env.LIVE_FLASH_REPEATS || 3);
    const signer = await newPage(dir, 'flash-sign-in');
    await signIn(signer, dir);
    await signer.close();
    const summary = [];
    for (const profile of ['plain', 'throttled']) {
        for (const route of routes) {
            for (let n = 1; n <= repeats; n++) {
                const label = `${profile}-${route.replace(/[^a-z0-9]+/gi, '_').replace(/^_|_$/g, '') || 'root'}-${n}`;
                const page = await newPage(dir, `flash-${label}`);
                await page.evaluateOnNewDocument(SCREEN_PROBE);
                await page.setCacheEnabled(false);
                const cdp = await page.createCDPSession();
                if (profile === 'throttled') await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 300, downloadThroughput: 2e6 / 8, uploadThroughput: 2e6 / 8 });
                await mkdir(join(dir, 'shots', label), { recursive: true });
                const started = Date.now();
                await jsonl(join(dir, 'events.jsonl'), { at: iso(started), ev: 'load', label, route, profile, cache: 'disabled', throttle: profile === 'throttled' ? '2 Mbit/s both ways, 300 ms latency (CDP Network.emulateNetworkConditions)' : null });
                // The navigation is started and not awaited: the screenshots begin at once. Its promise is bounded below.
                // Screenshots: Chrome's own screencast of this tab (CDP Page.startScreencast, every frame Chrome
                // produces; a frame is sent when the picture changes), kept for the first 6 s. page.screenshot during a
                // navigation was tried first (attempt 1) and produced no picture.
                const pictures = [];
                cdp.on('Page.screencastFrame', ({ data, metadata, sessionId }) => { pictures.push({ at: metadata.timestamp * 1000, data }); cdp.send('Page.screencastFrameAck', { sessionId }).catch(() => {}); });
                await cdp.send('Page.startScreencast', { format: 'jpeg', quality: 60, everyNthFrame: 1 });
                const navigation = page.goto(base + route, { waitUntil: 'domcontentloaded', timeout: 20000 }).then(() => 'resolved', error => 'not resolved: ' + String(error.message).slice(0, 120));
                await pause(6000);
                await cdp.send('Page.stopScreencast').catch(() => {});
                let shot = 0;
                for (const picture of pictures) {
                    const ms = Math.round(picture.at - started);
                    if (ms > 6000) continue;
                    await writeFile(join(dir, 'shots', label, `${String(shot).padStart(3, '0')}-${String(Math.max(ms, 0)).padStart(5, '0')}ms.jpg`), Buffer.from(picture.data, 'base64'));
                    shot++;
                }
                const navigationOutcome = await Promise.race([navigation, pause(15000).then(() => 'still pending after 15 s')]);
                await pause(profile === 'throttled' ? 6000 : 1500);
                const screens = await page.evaluate(() => window.__set7Screens || []).catch(() => null);
                const painted = await page.evaluate(() => (window.__set7Frames || []).map(({ key, ...rest }) => rest)).catch(() => null);
                const timing = await page.evaluate(() => { const n = performance.getEntriesByType('navigation')[0]; return n ? { responseStart: Math.round(n.responseStart), domContentLoaded: Math.round(n.domContentLoadedEventEnd), load: Math.round(n.loadEventEnd) } : null; }).catch(() => null);
                const api = await page.evaluate(() => performance.getEntriesByType('resource').filter(e => e.name.includes('/api/')).map(e => ({ path: new URL(e.name).pathname, start: Math.round(e.startTime), end: Math.round(e.responseEnd), status: e.responseStatus }))).catch(() => null);
                await page.screenshot({ path: join(dir, 'shots', label, 'final.png') });
                const recovery = (screens || []).filter(s => s.kind === 'recovery-access');
                const record = { label, route, profile, navigation: navigationOutcome, screens, painted_frames: painted, painted_recovery_access: (painted || []).filter(f => f.kind === 'recovery-access'), navigation_timing: timing, api_requests: api, shots: shot,
                    recovery_access_shown: recovery.length > 0,
                    recovery_access_from_ms: recovery[0]?.ms ?? null,
                    recovery_access_until_ms: recovery.length ? (screens[screens.indexOf(recovery[recovery.length - 1]) + 1]?.ms ?? null) : null,
                    final: await observe(page) };
                summary.push({ label, painted_recovery_access: record.painted_recovery_access, recovery_access_shown: record.recovery_access_shown, from: record.recovery_access_from_ms, until: record.recovery_access_until_ms, kinds: (screens || []).map(s => `${s.ms}:${s.kind}${s.h1 ? '(' + s.h1 + ')' : ''}`).join(' > ') });
                await jsonl(join(dir, 'flash-loads.jsonl'), record);
                await Promise.race([cdp.detach().catch(() => {}), pause(3000)]);
                await Promise.race([page.close({ runBeforeUnload: false }).catch(() => {}), pause(5000)]);
            }
        }
    }
    await writeFile(join(dir, 'flash-summary.json'), redact(JSON.stringify(summary, null, 2)));
}

const results = {};
for (const cell of cells) {
    const dir = join(outRoot, cell);
    await mkdir(dir, { recursive: true });
    const started = iso();
    try {
        if (cell === 'probe') await cellProbe(dir);
        else if (cell === 'update') await cellUpdate(dir);
        else if (cell === 'hidden') await cellHidden(dir, 'hidden', false);
        else if (cell === 'hiddenrestart') await cellHidden(dir, 'hiddenrestart', true);
        else if (cell === 'flash') await cellFlash(dir);
        else if (cell === 'done') await writeFile(join(handDir, 'done.json'), JSON.stringify({ at: iso(), by: 'live-restart.mjs' }) + '\n');
        else throw new Error(`unknown cell ${cell}`);
        results[cell] = { started, ended: iso(), ok: true };
    } catch (error) {
        results[cell] = { started, ended: iso(), ok: false, error: redact(String(error.stack || error)).slice(0, 1500) };
    }
    await writeFile(join(dir, 'run.json'), redact(JSON.stringify({ cell, chrome: await browser.version(), handover: { ...hand, password_file: '(root-only file, not copied)' }, leaf, ...results[cell] }, null, 2)));
    console.log(JSON.stringify({ cell, ...results[cell] }));
}
await browser.close();
