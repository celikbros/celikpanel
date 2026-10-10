// Batch 8 (2026-10-10): the cold full page load, after the seventh native
// record's cell 5 (`deploy/e2e/release-recovery/evidence/set7-20261010/`).
//
//   coldload   a cold full load of `/setup`, `/` and `/settings?section=updates`,
//              signed in, the Panel healthy, in a fresh browser context with the
//              HTTP cache off, the network throttled as in set7 (2 Mbit/s both
//              ways, 300 ms latency) and the session, readiness and license reads
//              slowed in the mock. The rule: a read that has not answered is not
//              shown as a page; nothing but the page background is drawn until
//              the quiet time (1.5 s) has passed or a read answered without
//              confirming access. Recorded per load: what is on screen at every
//              animation frame (from before the document's own scripts), and
//              Chrome's own screencast frames named by their time since
//              navigation start, so the pictures at 0-200 ms, 1 s and 2 s can be
//              looked at.
//   coldslow   the same, with the session read held for 2.5 s: the waiting
//              state after the quiet time must be the explained wait, never a
//              failure, and the page must open by itself once the read answers.
//   coldslow35 the ninth native record's cell 2 (2026-10-10): `/settings?section=updates`
//              with every session read held 35 s, watched for 40 s. One sequence: the
//              explained wait at 1.5 s, "Check now" at 15 s (still unknown, never "could
//              not be checked"), the automatic re-read changing nothing, and the
//              half-minute sentence with the reload at 30 s. Screenshots and the text
//              on screen at 1.6 s, 15.2 s, 25.2 s and 31 s of the page's own clock.
//              The network is not throttled here (cell 2c held only the session read).
//
// Like the batches before: the mock on 127.0.0.1 answers everything; nothing
// else is contacted.
//
// Sekizinci grup: soguk tam sayfa yuklemesi. Yanit vermemis okuma sayfa olarak
// gosterilmez; sessiz sure gecene ya da okuma yanit verene kadar yalnizca sayfa
// zemini cizilir.
import { mkdir, writeFile } from 'node:fs/promises';

const ROUTES = [['setup', '/setup'], ['root', '/'], ['settings-updates', '/settings?section=updates']];
const READS = ['/api/v1/auth/me', '/api/v1/panel/availability', '/api/v1/license/access'];

// Installed before the document's own scripts: the kind on screen at every frame.
// 'quiet' is the neutral background of RecoveryAccess or LicenseOnboarding,
// 'gate' a full-page access screen (an <h1> with no application navigation),
// 'hold' the layer over a mounted page, 'app' the application layout.
function recorder() {
    const frames = [];
    let last = '';
    const kind = () => {
        if (!document.body) return 'empty';
        if (document.querySelector('[data-top-layer="hold"]')) return 'hold';
        if (document.querySelector('nav, aside')) return 'app';
        const h1 = document.querySelector('h1');
        if (h1) return `gate:${h1.innerText.trim().slice(0, 60)}`;
        if (document.querySelector('[data-access-quiet]')) return 'quiet';
        const text = (document.body.innerText || '').trim();
        if (text) return `text:${text.slice(0, 60)}`;
        if (document.querySelector('.animate-spin')) return 'spinner';
        return 'empty';
    };
    const tick = () => {
        const now = kind();
        if (now !== last) { frames.push({ t: Math.round(performance.now()), kind: now }); last = now; }
        if (performance.now() < (window.__watchMs || 9000)) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
    Object.defineProperty(window, '__frames', { value: frames });
}

export default function batch8(scenarios, { base, locale, ctl, reset, newPage, closePage, pause }) {
    const out = process.env.BROWSER_INSPECT_OUT ? `${process.env.BROWSER_INSPECT_OUT}/coldload-${locale}/` : null;
    // The text and actions on screen at a time of the page's own clock, with a screenshot.
    async function sample(page, ms, dir, label) {
        for (;;) {
            const now = await page.evaluate(() => performance.now());
            if (now >= ms) break;
            await pause(Math.min(ms - now, 1000));
        }
        const state = await page.evaluate(() => ({
            t: Math.round(performance.now()),
            title: document.querySelector('h1')?.innerText ?? null,
            text: Array.from(document.querySelectorAll('main p')).map(node => node.innerText),
            buttons: Array.from(document.querySelectorAll('main button')).map(node => ({ label: node.innerText.trim(), disabled: node.disabled })),
            quiet: !!document.querySelector('[data-access-quiet]'),
        }));
        if (dir) await page.screenshot({ path: `${dir}${label}.png` });
        return { label, ...state };
    }
    async function load(name, path, slow, { hold = 2500, watch = 7000, samples = [], throttle = true } = {}) {
        await reset();
        await ctl({ clearAll: true, session: true, setupStatus: 'ready', license: 'active' });
        await ctl({ override: Object.fromEntries(READS.map(read => [read, { delay: read === '/api/v1/auth/me' && slow ? hold : 300 }])) });
        const page = await newPage();
        await page.setCacheEnabled(false);
        await page.evaluateOnNewDocument(`window.__watchMs = ${watch + 2000};`);
        await page.evaluateOnNewDocument(recorder);
        const cdp = await page.createCDPSession();
        await cdp.send('Network.enable');
        if (throttle) await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 300, downloadThroughput: 2e6 / 8, uploadThroughput: 2e6 / 8 });
        const shots = [];
        cdp.on('Page.screencastFrame', async frame => {
            shots.push({ at: frame.metadata.timestamp, data: frame.data });
            try { await cdp.send('Page.screencastFrameAck', { sessionId: frame.sessionId }); } catch { /* page closed */ }
        });
        await cdp.send('Page.startScreencast', { format: 'jpeg', quality: 70, everyNthFrame: 1 });
        const started = Date.now();
        await page.goto(`${base}${path}`, { waitUntil: 'domcontentloaded', timeout: 15000 }).catch(() => {});
        const dir = out ? `${out}${name}/` : null;
        if (dir) await mkdir(dir, { recursive: true });
        const sampled = [];
        for (const [label, ms] of samples) sampled.push(await sample(page, ms, dir, label));
        const waited = await page.evaluate(() => performance.now());
        if (waited < watch) await pause(watch - waited);
        const origin = await page.evaluate(() => performance.timeOrigin);
        const frames = await page.evaluate(() => window.__frames);
        await cdp.send('Page.stopScreencast').catch(() => {});
        const kept = [];
        for (const shot of shots) {
            const ms = Math.round(shot.at * 1000 - origin);
            kept.push(ms);
            if (dir && samples.length === 0) await writeFile(`${dir}${String(ms).padStart(5, '0')}ms.jpg`, Buffer.from(shot.data, 'base64'));
        }
        await closePage(page);
        const gate = frames.filter(item => item.kind.startsWith('gate:') || item.kind.startsWith('text:'));
        const record = { name, path, slow, hold, watch, goto: started, frames, samples: sampled, screencast: kept, firstGateMs: gate[0]?.t ?? null, gateKinds: [...new Set(gate.map(item => item.kind))] };
        if (dir) await writeFile(`${dir}frames.json`, JSON.stringify(record, null, 2));
        console.log(`coldload ${name}: ${frames.map(item => `${item.t}:${item.kind}`).join(' > ')}`);
        for (const item of sampled) console.log(`  ${item.label} @${item.t}ms: ${item.title} | ${item.text.join(' / ')} | ${item.buttons.map(button => `${button.label}${button.disabled ? ' (disabled)' : ''}`).join(', ')}`);
        return record;
    }
    scenarios.coldload = async () => {
        for (const [name, path] of ROUTES) await load(name, path, false);
        await ctl({ clearAll: true });
    };
    scenarios.coldslow = async () => {
        for (const [name, path] of ROUTES.slice(0, 1)) await load(`${name}-slow-session`, path, true);
        await ctl({ clearAll: true });
    };
    scenarios.coldslow35 = async () => {
        // Screencast frames are not kept for this one: the four samples are page screenshots. Not throttled, as
        // in the record's cell 2c (only the session read was held), so the samples fall where the record's did.
        await load('settings-updates-session-35s', '/settings?section=updates', true, {
            throttle: false, hold: 35000, watch: 40000, samples: [['01-1600ms', 1600], ['02-15200ms', 15200], ['03-25200ms', 25200], ['04-31000ms', 31000], ['05-33000ms', 33000]],
        });
        await ctl({ clearAll: true });
    };
}
