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
        if (performance.now() < 9000) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
    Object.defineProperty(window, '__frames', { value: frames });
}

export default function batch8(scenarios, { base, locale, ctl, reset, newPage, closePage, pause }) {
    const out = process.env.BROWSER_INSPECT_OUT ? `${process.env.BROWSER_INSPECT_OUT}/coldload-${locale}/` : null;
    async function load(name, path, slow) {
        await reset();
        await ctl({ clearAll: true, session: true, setupStatus: 'ready', license: 'active' });
        await ctl({ override: Object.fromEntries(READS.map(read => [read, { delay: read === '/api/v1/auth/me' && slow ? 2500 : 300 }])) });
        const page = await newPage();
        await page.setCacheEnabled(false);
        await page.evaluateOnNewDocument(recorder);
        const cdp = await page.createCDPSession();
        await cdp.send('Network.enable');
        await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 300, downloadThroughput: 2e6 / 8, uploadThroughput: 2e6 / 8 });
        const shots = [];
        cdp.on('Page.screencastFrame', async frame => {
            shots.push({ at: frame.metadata.timestamp, data: frame.data });
            try { await cdp.send('Page.screencastFrameAck', { sessionId: frame.sessionId }); } catch { /* page closed */ }
        });
        await cdp.send('Page.startScreencast', { format: 'jpeg', quality: 70, everyNthFrame: 1 });
        const started = Date.now();
        await page.goto(`${base}${path}`, { waitUntil: 'domcontentloaded', timeout: 15000 }).catch(() => {});
        await pause(7000);
        const origin = await page.evaluate(() => performance.timeOrigin);
        const frames = await page.evaluate(() => window.__frames);
        await cdp.send('Page.stopScreencast').catch(() => {});
        const dir = out ? `${out}${name}/` : null;
        if (dir) await mkdir(dir, { recursive: true });
        const kept = [];
        for (const shot of shots) {
            const ms = Math.round(shot.at * 1000 - origin);
            kept.push(ms);
            if (dir) await writeFile(`${dir}${String(ms).padStart(5, '0')}ms.jpg`, Buffer.from(shot.data, 'base64'));
        }
        await closePage(page);
        const gate = frames.filter(item => item.kind.startsWith('gate:') || item.kind.startsWith('text:'));
        const record = { name, path, slow, goto: started, frames, screencast: kept, firstGateMs: gate[0]?.t ?? null, gateKinds: [...new Set(gate.map(item => item.kind))] };
        if (dir) await writeFile(`${dir}frames.json`, JSON.stringify(record, null, 2));
        console.log(`coldload ${name}: ${frames.map(item => `${item.t}:${item.kind}`).join(' > ')}`);
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
}
