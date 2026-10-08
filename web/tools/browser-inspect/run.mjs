// node run.mjs <desktop|phone> <en|tr> <light|dark> <port> [scenario,scenario...]
// Drives a real headless Chrome against the loopback mock (mock.mjs) and saves
// screenshots plus a text/observation record per state. Reads and screenshots
// only; it talks to 127.0.0.1 and to nothing else. See README.md.
//   BROWSER_INSPECT_MODULES  directory whose node_modules holds puppeteer-core (required)
//   BROWSER_INSPECT_OUT      where the screenshots go (default: the system temp directory)
//   BROWSER_INSPECT_DIST     the built SPA to serve (default: web/dist)
//   CHROME_PATH              the installed Chrome or Chromium to drive
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdir, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { join, resolve, sep } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

// puppeteer-core is deliberately not a dependency of web/package.json: the
// release build installs exactly what that file names, and a browser driver has
// no place in it. It is installed once, outside the repository.
if (!process.env.BROWSER_INSPECT_MODULES) { console.error('Set BROWSER_INSPECT_MODULES to a directory outside the repository where "npm install puppeteer-core" was run. See README.md.'); process.exit(2); }
const requireFrom = createRequire(pathToFileURL(join(resolve(process.env.BROWSER_INSPECT_MODULES), 'package.json')));
const loaded = await import(pathToFileURL(requireFrom.resolve('puppeteer-core')).href);
const puppeteer = [loaded.default?.default, loaded.default, loaded].find(item => typeof item?.launch === 'function');
const chrome = process.env.CHROME_PATH || ['C:/Program Files/Google/Chrome/Application/chrome.exe', '/usr/bin/google-chrome', '/usr/bin/chromium', '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'].find(candidate => existsSync(candidate));
if (!chrome) { console.error('No installed Chrome found. Set CHROME_PATH.'); process.exit(2); }

const [vp = 'desktop', locale = 'en', theme = 'light', portArg = '4801', only = ''] = process.argv.slice(2);
const port = Number(portArg);
const wanted = only ? only.split(',') : null;
const base = `http://127.0.0.1:${port}`;
const out = join(resolve(process.env.BROWSER_INSPECT_OUT || join(tmpdir(), 'celikpanel-browser-inspect')), `${vp}-${theme}-${locale}`) + sep;
await mkdir(out, { recursive: true });
const HOST = 'panel.example.com';
const PLAN_ID = 'a'.repeat(32);
const REQ_ID = 'd'.repeat(32);
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const mock = spawn(process.execPath, [fileURLToPath(new URL('./mock.mjs', import.meta.url)), String(port)], { stdio: 'ignore' });
await pause(700);
const ctl = body => fetch(`${base}/__ctl`, { method: 'POST', body: JSON.stringify(body) });
const drainLog = async () => (await fetch(`${base}/__log`)).json();
const reset = () => ctl({ mode: 'ok', session: true, setupStatus: 'ready', execution: null, license: 'active', served: '', validity: 60, componentOperation: null, update: null, host: '', clear: ['/api/v1/domains', '/api/v1/auth/me', '/api/v1/panel/availability', '/api/v1/license/access', '/api/v1/hosting/capabilities'] }).then(() => { /* execution null handled below */ });

const browser = await puppeteer.launch({
    executablePath: chrome, headless: true,
    args: ['--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--force-color-profile=srgb', '--hide-scrollbars=false'],
});
const report = { config: { vp, locale, theme }, states: {}, errors: [] };
const viewport = vp === 'phone' ? { width: 390, height: 844, deviceScaleFactor: 2, isMobile: true, hasTouch: true } : { width: 1440, height: 900, deviceScaleFactor: 1 };

async function newPage(storage = {}) {
    const context = await browser.createBrowserContext();
    const page = await context.newPage();
    await page.setViewport(viewport);
    await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: theme }]);
    page.on('pageerror', error => report.errors.push(`pageerror: ${error.message}`.slice(0, 300)));
    await page.evaluateOnNewDocument((lang, mode, extra) => {
        localStorage.setItem('celikpanel.lang', lang); localStorage.setItem('celikpanel.theme', mode);
        if (!sessionStorage.getItem('__seeded')) { for (const [key, value] of Object.entries(extra)) localStorage.setItem(key, value); sessionStorage.setItem('__seeded', '1'); }
    }, locale, theme, storage);
    page.__context = context;
    return page;
}
const closePage = async page => { try { await page.__context.close(); } catch { /* already closed */ } };

// What the owner reads, plus mechanical layout facts for this state.
async function observe(page) {
    return page.evaluate(() => {
        const text = node => (node?.innerText || '').replace(/\s+\n/g, '\n').trim();
        const hold = document.querySelector('[aria-labelledby="access-hold-title"]');
        const rect = node => { if (!node) return null; const r = node.getBoundingClientRect(); return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }; };
        const clipped = [];
        for (const node of document.querySelectorAll('h1,h2,h3,h4,p,button,a,span,li,label,dd,dt')) {
            if (node.closest('[inert]') && hold) continue;
            const r = node.getBoundingClientRect();
            if (r.width === 0 || r.height === 0) continue;
            const style = getComputedStyle(node);
            if (style.visibility === 'hidden' || node.closest('.sr-only')) continue;
            if (r.right > window.innerWidth + 1 || r.left < -1) clipped.push(`offscreen-x: <${node.tagName.toLowerCase()}> "${(node.innerText || '').slice(0, 50)}" left=${Math.round(r.left)} right=${Math.round(r.right)}`);
            else if (node.scrollWidth > node.clientWidth + 1 && ['hidden', 'clip'].includes(style.overflowX) && node.children.length === 0) clipped.push(`truncated: <${node.tagName.toLowerCase()}> "${(node.innerText || '').slice(0, 50)}"`);
        }
        const active = document.activeElement;
        return {
            path: location.pathname + location.search,
            visibility: document.visibilityState,
            main: text(document.querySelector('main') || document.body).slice(0, 2600),
            hold: hold ? text(hold) : null,
            holdRect: rect(hold),
            dialogs: Array.from(document.querySelectorAll('[role="dialog"]')).map(node => node.getAttribute('aria-labelledby')),
            inert: !!document.querySelector('[data-access-hold="blocked"]'),
            horizontalOverflow: document.documentElement.scrollWidth - window.innerWidth,
            clipped: clipped.slice(0, 12),
            active: active ? `${active.tagName.toLowerCase()}${active.id ? '#' + active.id : ''} "${(active.innerText || active.value || '').slice(0, 40)}" inHold=${!!hold?.contains(active)}` : null,
            viewport: { w: window.innerWidth, h: window.innerHeight },
        };
    });
}
// Colours are compared as computed values: a reason drawn in the failure colour
// is a finding however calm its words are.
const leadFacts = page => page.evaluate(() => {
    const rect = node => { if (!node) return null; const r = node.getBoundingClientRect(); return { y: Math.round(r.y), h: Math.round(r.height) }; };
    const probe = document.createElement('p'); probe.className = 'text-danger'; document.body.appendChild(probe);
    const danger = getComputedStyle(probe).color; probe.remove();
    const section = document.querySelector('section[aria-labelledby="setup-progress-title"]');
    const list = section?.querySelector('ol');
    const before = node => !!list && !!(node.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING);
    const lead = Array.from(section?.querySelectorAll('h2,h3,p,button,summary') || []).filter(before);
    return {
        headings: Array.from(section?.querySelectorAll('h2,h3') || []).filter(before).map(node => `${node.tagName.toLowerCase()}: ${node.innerText}`),
        leadText: lead.filter(node => node.tagName === 'P').map(node => node.innerText),
        dangerText: lead.filter(node => getComputedStyle(node).color === danger).map(node => node.innerText.slice(0, 80)),
        actionsBeforeList: lead.filter(node => node.tagName === 'BUTTON').map(node => node.innerText),
        actionRect: rect(lead.find(node => node.tagName === 'BUTTON')),
        listRect: rect(list),
        fold: window.innerHeight,
    };
});
// Which layer is really on top at the centre and the corners of the viewport, and what dims the page.
const stacking = page => page.evaluate(() => {
    const hold = document.querySelector('[data-top-layer="hold"]');
    const at = (x, y) => { const node = document.elementFromPoint(x, y); return node ? { inHold: !!hold?.contains(node), tag: node.tagName.toLowerCase(), dialog: node.closest('[role="dialog"]')?.getAttribute('aria-labelledby') || null } : null; };
    const w = window.innerWidth, h = window.innerHeight;
    const scrims = Array.from(document.querySelectorAll('[class*="bg-scrim"]')).filter(node => node.getClientRects().length > 0)
        .map(node => ({ inHold: !!hold?.contains(node), inReload: !!node.closest('[data-top-layer="reload"]'), background: getComputedStyle(node).backgroundColor }));
    return {
        centre: at(w / 2, h / 2), topLeft: at(8, 8), bottomRight: at(w - 8, h - 8), scrims,
        darkening: scrims.filter(item => !/, 0\)$|^transparent$/.test(item.background)).length,
        dialogs: Array.from(document.querySelectorAll('[role="dialog"]')).map(node => node.getAttribute('aria-labelledby')),
        active: document.activeElement ? `${document.activeElement.tagName.toLowerCase()} inHold=${!!hold?.contains(document.activeElement)}` : null,
    };
});
async function shot(page, name, note = {}) {
    await pause(250);
    await page.screenshot({ path: `${out}${name}.png` });
    report.states[name] = { ...(await observe(page)), ...note };
}
async function into(page, selector) {
    await page.evaluate(sel => document.querySelector(sel)?.scrollIntoView({ block: 'center' }), selector);
    await pause(200);
}
async function clickByText(page, texts, scope = 'button') {
    const found = await page.evaluateHandle((list, sel) => {
        const nodes = Array.from(document.querySelectorAll(sel)).filter(node => { const r = node.getBoundingClientRect(); return r.width > 0 && r.height > 0 && list.some(item => (node.innerText || '').trim().includes(item)); });
        return nodes[nodes.length - 1] || null;
    }, texts, scope);
    const element = found.asElement();
    if (!element) throw new Error(`no ${scope} with text ${texts.join(' | ')}`);
    await element.click();
}
const marker = (handover = true) => ({ [`celikpanel.setup.start.admin`]: JSON.stringify({ request_id: REQ_ID, plan_id: PLAN_ID, panel_domain: HOST, ...(handover ? { handover: true } : {}) }) });
const waitFor = (page, fn, timeout = 15000, ...args) => page.waitForFunction(fn, { timeout, polling: 100 }, ...args);

// --- Scenarios that withhold one read at a time (9 Oct 2026) -------------------
// "No negative UI unless known": for each read these scenarios make slow,
// failing, known negative and known positive, the record says which negative
// sentences were on screen, which checking lines and notices were, and what
// moved when the answer arrived.
const CAPS = '/api/v1/hosting/capabilities';
const LIST = '/api/v1/domains';
const ENGINES = '/api/v1/database-servers';
const ENGINE_DATABASES = '/api/v1/database-servers/1/databases';
const ENGINE_USERS = '/api/v1/database-servers/1/users';
const CONN = '/api/v1/domains/1/connection';
const DOMAIN_DATABASES = '/api/v1/domains/1/databases';
const DIALOG = '[aria-labelledby="add-domain-title"]';
const ADD_DOMAIN = ['Add domain', 'Alan adı ekle'];
const RETRY = ['Retry', 'Tekrar dene'];
const USERS_TAB = ['Users', 'Kullanıcılar'];
const DATABASES_TAB = ['Databases', 'Veritabanları'];
const REMOVE_ACCOUNT = ['Remove account', 'Hesabı kaldır'];
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3'], mail_server: true, database_servers: ['mariadb'], db_tools: [] };
const DOMAINS = [
    { id: 1, domain_name: 'example.com', status: 'active', project_type: 'php', php_version: '8.3', ssl_enabled: true, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 0 },
    { id: 2, domain_name: 'shop.example.org', status: 'active', project_type: 'static', ssl_enabled: false, created_at: '2026-09-14T08:30:00Z', disk_usage: 627, bandwidth: 0 },
];
const DB = {
    dbServers: [{ id: 1, type_id: 1, type_name: 'mariadb', type_icon: 'M', name: 'MariaDB', version: '11.4', host: 'localhost', port: 3306, is_default: true, status: 'active', created_at: '2026-09-01T10:00:00Z', admin_username: 'celikpanel_admin', is_local: true }],
    dbDatabases: [{ id: 5, name: 'example_com_shop', users: ['example_com_shop'], created_at: '2026-09-02T10:00:00Z' }, { id: 6, name: 'example_com_blog', users: ['example_com_blog', 'reporting'], created_at: '2026-09-03T10:00:00Z' }],
    dbUsers: [{ id: 7, username: 'example_com_shop', databases: ['example_com_shop'], created_at: '2026-09-02T10:00:00Z' }, { id: 8, username: 'reporting', databases: ['example_com_blog'], created_at: '2026-09-03T10:00:00Z' }],
};
const CONNECTION = { domain: 'example.com', server_ip: '192.0.2.4', nameservers: ['ns1.example.net', 'ns2.example.net'], live_nameservers: ['ns1.example.net', 'ns2.example.net'], live_ips: ['192.0.2.4'], status: 'delegated', ssl_ready: true, glue_needed: false, nameservers_usable: true, propagation_pending: false, checked_at: '2026-10-09T09:00:00Z', dns_management_mode: 'local' };
const remoteReset = async () => {
    await reset();
    await ctl({ domains: [], capabilities: CAPABILITIES, subscriptions: [], dbServers: [], dbDatabases: [], dbUsers: [], domainDatabases: { databases: [], available_types: ['mysql'] }, connection: null, clear: [CAPS, LIST, ENGINES, ENGINE_DATABASES, ENGINE_USERS, CONN, DOMAIN_DATABASES] });
    await drainLog();
};
const capabilityReads = async () => (await drainLog()).filter(line => line.includes(`GET ${CAPS}`)).length;
// Sentences that claim something about the server. Each may be on screen only
// when the server has said so.
const NEGATIVE = [
    'DNS server is required', 'Choose a DNS engine', 'DNS identity is not ready', 'Configure the DNS pair', 'No domains yet',
    'No database engine installed', 'Go to Services', 'No databases yet', 'No database users yet', 'does not point at this server yet',
    'cannot be issued yet', 'nothing yet', 'Does not resolve yet', 'nameserver setup is not ready',
    'DNS sunucusu gerekir', 'DNS motoru seç', 'DNS kimliği henüz hazır değil', 'DNS çiftini yapılandır', 'Henüz alan adı yok',
    'Kurulu veritabanı motoru yok', 'Servisler sayfasına git', 'Henüz veritabanı yok', 'Henüz veritabanı kullanıcısı yok', 'henüz bu sunucuyu göstermiyor',
    'Sertifika henüz alınamaz', 'henüz yok', 'Henüz çözülmüyor', 'ad sunucusu kurulumu henüz hazır değil',
];
const negatives = page => page.evaluate((phrases) => {
    const scope = document.querySelector('[role="dialog"]') || document.querySelector('main') || document.body;
    const text = scope.innerText || '';
    return {
        negativeText: phrases.filter(phrase => text.includes(phrase)),
        checkingLines: Array.from(scope.querySelectorAll('[role="status"][aria-label]')).filter(node => node.getClientRects().length > 0).map(node => node.getAttribute('aria-label')),
        notices: Array.from(scope.querySelectorAll('[role="alert"]')).map(node => node.innerText.trim()),
        disabledButtons: Array.from(scope.querySelectorAll('button:disabled')).map(node => (node.innerText || node.getAttribute('aria-label') || node.title || '').trim()).filter(Boolean),
    };
}, NEGATIVE);
const dialogFacts = page => page.evaluate((sel, phrases) => {
    const dialog = document.querySelector(sel);
    const rect = node => { if (!node) return null; const r = node.getBoundingClientRect(); return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }; };
    const text = dialog?.innerText || '';
    const submit = dialog?.querySelector('button[type="submit"]');
    const name = dialog?.querySelector('input[type="text"]');
    return {
        blocker: phrases.filter(phrase => text.includes(phrase)),
        checkingLine: dialog?.querySelector('[role="status"][aria-label]')?.getAttribute('aria-label') || null,
        notice: dialog?.querySelector('[role="alert"]')?.innerText.trim() || null,
        submitDisabled: submit?.disabled ?? null,
        nameDisabled: name?.disabled ?? null,
        radios: Array.from(dialog?.querySelectorAll('input[type="radio"]') || []).map(node => ({ checked: node.checked, disabled: node.disabled })),
        rects: { panel: rect(dialog), name: rect(name), purpose: rect(dialog?.querySelector('input[type="radio"]')?.closest('.grid')), ssl: rect(dialog?.querySelector('#ssl')), submit: rect(submit) },
    };
}, DIALOG, NEGATIVE);
const pageFacts = page => page.evaluate(() => {
    const rect = node => { const r = node.getBoundingClientRect(); return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }; };
    const rects = {};
    for (const node of document.querySelectorAll('main h1, main h3, main h4, main button, main [role="tab"]')) {
        if (node.getClientRects().length === 0) continue;
        const key = `${node.tagName.toLowerCase()}:${(node.innerText || node.getAttribute('aria-label') || node.title || '').trim().replace(/\s+/g, ' ').replace(/\s*[\d…–]+$/, '').slice(0, 40)}`;
        if (!(key in rects)) rects[key] = rect(node);
    }
    return { rects };
});
// Opens a page of the panel and waits for it to settle. A read that fails can
// leave another reader's request open (the navigation rail reads the domain
// list as well), so quiet is waited for with a limit instead of required.
const quiet = page => page.waitForNetworkIdle({ idleTime: 600, timeout: 7000 }).catch(() => {});
const go = async (page, path) => { await page.goto(`${base}${path}`, { waitUntil: 'domcontentloaded' }); await page.waitForSelector('main h1'); await quiet(page); };
// What changed place or size between two records of the same elements.
const moved = (before, after) => Object.keys(before || {}).filter(key => before[key] && after?.[key])
    .filter(key => ['x', 'y', 'w', 'h'].some(side => Math.abs(before[key][side] - after[key][side]) > 1))
    .map(key => `${key}: ${JSON.stringify(before[key])} -> ${JSON.stringify(after[key])}`);

const scenarios = {
    // 1 + 2: review notice, progress notice, planned restart, continuation.
    async setup() {
        await reset(); await ctl({ setupStatus: 'draft' });
        const page = await newPage();
        await page.goto(`${base}/setup`, { waitUntil: 'networkidle0' });
        await page.waitForSelector('#setup-panel_domain');
        await shot(page, '01-0-access-step');
        await page.click('form button[type="submit"]');
        await page.waitForSelector('#setup-plan-title');
        const noticeFacts = () => page.evaluate(() => {
            const notice = document.querySelector('[aria-labelledby="setup-handover-title"]');
            const list = Array.from(document.querySelectorAll('ol')).find(node => node.innerText.includes('panel.example.com'));
            const r = notice.getBoundingClientRect(); const anchor = notice.querySelector('a');
            const row = Array.from(list?.querySelectorAll('li') || []).find(node => node.innerText.includes('panel.example.com'));
            return { noticeTop: Math.round(r.top), noticeBottom: Math.round(r.bottom), noticeHeight: Math.round(r.height), fold: window.innerHeight, aboveStepList: !!list && !!(notice.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING), linkLines: Math.round(anchor.getBoundingClientRect().height / parseFloat(getComputedStyle(anchor).lineHeight)), stepRow: row?.innerText || null };
        });
        await shot(page, '01a-review-top', await noticeFacts());
        await into(page, '#setup-handover-title');
        await shot(page, '01a-review-notice', { notice: await page.$eval('[aria-labelledby="setup-handover-title"]', node => node.innerText), link: await page.$eval('[aria-labelledby="setup-handover-title"] a', node => node.href) });
        await page.click('section[aria-labelledby="setup-plan-title"] input[type="checkbox"]');
        await into(page, '.setup-actions');
        await shot(page, '01a-review-bottom');
        await page.click('.setup-actions .ml-auto button');
        await page.waitForSelector('#setup-progress-title');
        await pause(600);
        await shot(page, '01b-progress-top', await noticeFacts());
        await into(page, '#setup-handover-title');
        await shot(page, '01b-progress-notice', { notice: await page.$eval('[aria-labelledby="setup-handover-title"]', node => node.innerText) });
        // The certificate step is now the running one.
        await ctl({ execution: { request_id: await page.evaluate(() => JSON.parse(localStorage.getItem('celikpanel.setup.start.admin')).request_id), statuses: ['succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded', 'running'], extra: { phase: 'panel-certificate' } } });
        await pause(3800);
        await page.evaluate(() => document.querySelector('main')?.scrollTo?.(0, 0)); await into(page, '#setup-progress-title');
        await shot(page, '01c-progress-cert-running');
        // 2: the Panel goes away while that step runs.
        await ctl({ mode: 'down' });
        await waitFor(page, () => !/Installing|kuruluyor|Kurulum sürüyor/.test(document.querySelector('#setup-progress-title')?.innerText || '') , 12000).catch(() => {});
        await pause(3500);
        await into(page, '#setup-progress-title');
        await shot(page, '02a-planned-drop', { heading: await page.$eval('#setup-progress-title', node => node.innerText) });
        // The Panel process is back but still starting; it serves the new certificate.
        await ctl({ mode: 'starting', served: HOST });
        await page.waitForSelector('[aria-labelledby="access-hold-title"]', { timeout: 15000 }).catch(() => {});
        await pause(1200);
        await shot(page, '02b-starting-hold-over-wizard');
        // Ready again, certificate step finished.
        await ctl({ mode: 'ok', execution: { request_id: await page.evaluate(() => JSON.parse(localStorage.getItem('celikpanel.setup.start.admin')).request_id), statuses: ['succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded'], extra: { phase: 'verification', panel_url: `https://${HOST}:${port}` } } });
        const started = Date.now();
        await waitFor(page, () => !document.querySelector('[aria-labelledby="access-hold-title"]'), 20000).catch(() => {});
        const cleared = Date.now() - started;
        await pause(3500);
        await shot(page, '02c-continues-after-restart', { holdClearedAfterMs: cleared });
        // The full readiness page: a reload while the Panel is starting.
        await ctl({ mode: 'starting' });
        await page.reload({ waitUntil: 'networkidle0' });
        await pause(1500);
        await shot(page, '02d-readiness-page-handover');
        await ctl({ mode: 'ok' });
        const reopened = Date.now();
        await page.waitForSelector('#setup-progress-title', { timeout: 20000 }).catch(() => {});
        await pause(500);
        await shot(page, '02e-readiness-page-opens', { openedAfterMs: Date.now() - reopened });
        // Setup finishes.
        await ctl({ setupStatus: 'ready', execution: { request_id: await page.evaluate(() => JSON.parse(localStorage.getItem('celikpanel.setup.start.admin') || '{}').request_id || 'adopt'), statuses: ['succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded'], extra: { status: 'succeeded', phase: 'done' } } });
        await pause(4500);
        await shot(page, '02f-setup-ready');
        await closePage(page);
    },
    // 3: a drop at another step keeps the unknown-result wording.
    async otherdrop() {
        await reset(); await ctl({ setupStatus: 'running', execution: { request_id: REQ_ID, statuses: ['succeeded', 'running'], extra: { phase: 'service-php-fpm' } } });
        const page = await newPage(marker());
        await page.goto(`${base}/setup`, { waitUntil: 'networkidle0' });
        await page.waitForSelector('#setup-progress-title');
        await pause(500);
        await shot(page, '03-0-progress-php-running');
        await ctl({ mode: 'down' });
        await pause(7000);
        await into(page, '#setup-progress-title');
        await shot(page, '03a-unknown-drop', { heading: await page.$eval('#setup-progress-title', node => node.innerText) });
        await ctl({ mode: 'starting', served: '' });
        await page.waitForSelector('[aria-labelledby="access-hold-title"]', { timeout: 15000 }).catch(() => {});
        await pause(1200);
        await shot(page, '03b-starting-hold-other-step');
        await ctl({ mode: 'ok' });
        await waitFor(page, () => !document.querySelector('[aria-labelledby="access-hold-title"]'), 20000).catch(() => {});
        await pause(3500);
        await shot(page, '03c-reconnected');
        await closePage(page);
    },
    // 4: a step refused because the server is busy, by reason.
    async busy() {
        const sentences = {
            package_manager_active: "This server's package manager is busy — a package task is still running on this server. Wait for it to finish, then try again.",
            agent_mutation_active: 'Another CelikPanel change is still running on this server. Wait for it to finish, then try again.',
            panel_operation_active: 'Another CelikPanel operation is still running. Wait for it to finish, then try again.',
            host_lock_busy: 'A change that did not finish is still holding this server, and it will not clear by waiting. Restarting the server releases the hold; if it comes back, this needs looking at.',
            none: 'another server change or package-manager task is still running; wait and try again',
        };
        for (const [reason, message] of Object.entries(sentences)) {
            await reset();
            await ctl({ setupStatus: 'failed', execution: { request_id: REQ_ID, statuses: ['failed'], extra: { status: 'failed', phase: 'service-nginx', error: { code: 'HOST_MUTATION_BUSY', message, ...(reason === 'none' ? {} : { reason }) } } } });
            const page = await newPage(marker(false));
            await page.goto(`${base}/setup`, { waitUntil: 'networkidle0' });
            await page.waitForSelector('#setup-progress-title');
            await pause(500);
            await shot(page, `04-busy-${reason}`, await leadFacts(page));
            await closePage(page);
        }
    },
    // 4b: the same lead area for a verified step failure, an unmet prerequisite, a license wait and a run in progress.
    async stops() {
        const context = { dns_mode: 'external', dns_role: '', dns_engine: 'bind', local_nameserver: '', local_ip: '', peer_nameserver: '', peer_ip: '', panel_domain: HOST, mail_hostname: '', dns_hosting_management: '', access_dns_ip: '203.0.113.10' };
        const cases = {
            '04b-failed-install': { setupStatus: 'failed', statuses: ['failed'], extra: { status: 'failed', phase: 'service-nginx', error: { code: 'service_install_failed', message: 'apt-get install nginx: exit status 100', component: 'nginx', step: 'package_install', detail: 'E: Unable to fetch some archives, maybe run apt-get update or try with --fix-missing?' } } },
            '04c-failed-firewall': { setupStatus: 'failed', statuses: ['succeeded', 'succeeded', 'succeeded', 'succeeded', 'failed'], extra: { status: 'failed', phase: 'firewall', error: { code: 'server_setup_firewall_failed', message: 'nft -f: Operation not supported' } } },
            '04d-waiting-prerequisite': { setupStatus: 'waiting', statuses: ['succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded'], extra: { status: 'waiting', phase: 'access_dns', context, error: { code: 'server_setup_access_dns_required', message: 'panel.example.com does not resolve to 203.0.113.10 yet' } } },
            '04e-waiting-license': { setupStatus: 'waiting', statuses: ['succeeded'], extra: { status: 'waiting', phase: 'license' } },
            '04f-running': { setupStatus: 'running', statuses: ['succeeded', 'running'], extra: { phase: 'service-php-fpm' } },
            '04g-confirming': { setupStatus: 'running', statuses: ['succeeded', 'running'], extra: { phase: 'service-php-fpm', error: { code: 'server_setup_reconciling', message: 'pending receipt' } } },
        };
        for (const [name, item] of Object.entries(cases)) {
            await reset();
            await ctl({ setupStatus: item.setupStatus, execution: { request_id: REQ_ID, statuses: item.statuses, extra: item.extra } });
            const page = await newPage(marker(false));
            await page.goto(`${base}/setup`, { waitUntil: 'networkidle0' });
            await page.waitForSelector('#setup-progress-title');
            await pause(500);
            await shot(page, name, await leadFacts(page));
            await closePage(page);
        }
    },
    // 5b: the hold over the two operation overlays that are drawn outside the held pages, and under the reload dialogue.
    async overlays() {
        const refuse = async page => {
            await ctl({ override: { '/api/v1/domains': { status: 503, body: { error: 'license status unavailable', code: 'LICENSE_STATUS_UNAVAILABLE' } } }, license: 'unavailable' });
            await page.evaluate(() => fetch('/api/v1/domains'));
            await page.waitForSelector('[aria-labelledby="access-hold-title"]', { timeout: 15000 });
            await pause(700);
        };
        const tabs = async page => { const seen = []; for (let i = 0; i < 4; i++) { await page.keyboard.press('Tab'); seen.push(await page.evaluate(() => { const a = document.activeElement; return `${a?.tagName.toLowerCase()} inHold=${!!document.querySelector('[data-top-layer="hold"]')?.contains(a)}`; })); } return seen; };
        // A component operation is running: its overlay is portalled to <body>, outside the inert subtree.
        await reset();
        await ctl({ componentOperation: { id: 'c'.repeat(32), request_id: 'e'.repeat(32), kind: 'service_install', service_id: 'nginx', package_name: 'nginx', status: 'running', phase: 'package_install', started_at: new Date().toISOString() } });
        let page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        const overlay = await page.waitForSelector('[aria-labelledby="component-operation-title"]', { timeout: 20000 }).then(() => true, () => false);
        await pause(500);
        await shot(page, '11a-component-overlay', { overlayShown: overlay, ...(await stacking(page)) });
        await refuse(page).catch(error => report.errors.push(`11b: ${error.message}`));
        // Try to reach the overlay underneath: a pointer on it, then the keyboard.
        await page.mouse.click(viewport.width / 2, 40);
        await shot(page, '11b-hold-over-component-overlay', { ...(await stacking(page)), tabs: await tabs(page), overlayStill: await page.evaluate(() => !!document.querySelector('[aria-labelledby="component-operation-title"]')) });
        await ctl({ clear: ['/api/v1/domains'], license: 'active' });
        await waitFor(page, () => !document.querySelector('[aria-labelledby="access-hold-title"]'), 20000).catch(() => {});
        await pause(400);
        await shot(page, '11c-component-overlay-after-hold', await stacking(page));
        await closePage(page);

        // A panel update is tracked by this browser: the update lock, then a refused request.
        await reset();
        const target = { version: 'v0.1.0-alpha.82', commit: '1'.repeat(40), sequence: '82', os: 'linux', arch: 'amd64', archive_sha256: '2'.repeat(64), archive_size: '1048576' };
        const updateMarker = { marker_version: 1, request_id: 'f'.repeat(32), current_version: 'v0.1.0-alpha.81', current_commit: '3'.repeat(40), created_at: Date.now(), target };
        await ctl({ update: { request_id: updateMarker.request_id, status: 'running', target } });
        page = await newPage({ 'celikpanel.system-update-operation.v1': JSON.stringify({ state_version: 1, phase: 'active', marker: updateMarker }) });
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        const lock = await page.waitForSelector('[aria-labelledby="system-update-operation-title"]', { timeout: 20000 }).then(() => true, () => false);
        await pause(500);
        await shot(page, '12a-update-overlay', { updateLockShown: lock, ...(await stacking(page)) });
        await refuse(page).catch(error => report.errors.push(`12b: ${error.message}`));
        await shot(page, '12b-hold-over-update-overlay', { ...(await stacking(page)), tabs: await tabs(page), updateLockStill: await page.evaluate(() => !!document.querySelector('[aria-labelledby="system-update-operation-title"]')) });
        await ctl({ clear: ['/api/v1/domains'], license: 'active' });
        await waitFor(page, () => !document.querySelector('[aria-labelledby="access-hold-title"]'), 20000).catch(() => {});
        await pause(600);
        await shot(page, '12c-update-after-hold', { ...(await stacking(page)), updateLockBack: await page.evaluate(() => !!document.querySelector('[aria-labelledby="system-update-operation-title"]')) });
        await closePage(page);

        // The reload dialogue above the hold above an open dialogue: one scrim, the reload dialogue on top.
        await reset();
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'networkidle0' });
        await clickByText(page, ['Add domain', 'Alan adı ekle', 'Alan Adı Ekle']);
        await page.waitForSelector('[aria-labelledby="add-domain-title"] input[type="text"]');
        await page.type('[aria-labelledby="add-domain-title"] input[type="text"]', 'typed-by-owner.example');
        await refuse(page);
        await shot(page, '13a-hold-over-dialog', await stacking(page));
        await page.evaluate(() => window.dispatchEvent(new Event('vite:preloadError', { cancelable: true })));
        await pause(700);
        await shot(page, '13b-reload-over-hold', { ...(await stacking(page)), reloadText: await page.evaluate(() => document.querySelector('[aria-labelledby="update-reload-title"]')?.innerText || null) });
        await closePage(page);
    },
    // 2b: a long secure address in the wizard, at the drop, in the hold and on the readiness page.
    async address() {
        const long = 'panel.customers-of-a-rather-long-company-name.hosting.example.co.uk';
        // The text of the link, line by line as the browser broke it.
        const facts = page => page.evaluate(host => {
            const link = Array.from(document.querySelectorAll('a')).filter(node => node.innerText.replace(/\s/g, '').includes(host)).pop();
            if (!link) return { link: null };
            const lines = []; let last = null;
            const walker = document.createTreeWalker(link, NodeFilter.SHOW_TEXT);
            for (let node = walker.nextNode(); node; node = walker.nextNode()) for (let i = 0; i < node.length; i++) {
                const range = document.createRange(); range.setStart(node, i); range.setEnd(node, i + 1);
                const top = Math.round(range.getBoundingClientRect().top);
                if (top !== last) { lines.push(''); last = top; }
                lines[lines.length - 1] += node.data[i];
            }
            const box = link.getBoundingClientRect();
            return { lines, right: Math.round(box.right), viewport: window.innerWidth, overflow: document.documentElement.scrollWidth - window.innerWidth };
        }, long);
        await reset(); await ctl({ host: long, setupStatus: 'running', execution: { request_id: REQ_ID, statuses: ['succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded', 'running'], extra: { phase: 'panel-certificate' } } });
        const page = await newPage({ 'celikpanel.setup.start.admin': JSON.stringify({ request_id: REQ_ID, plan_id: PLAN_ID, panel_domain: long, handover: true }) });
        await page.goto(`${base}/setup`, { waitUntil: 'networkidle0' });
        await page.waitForSelector('#setup-progress-title');
        await pause(500);
        await into(page, '#setup-handover-title');
        await shot(page, '14a-long-address-wizard', await facts(page));
        await ctl({ mode: 'down' });
        await pause(7000);
        await into(page, '#setup-progress-title');
        await shot(page, '14b-long-address-drop', await facts(page));
        await ctl({ mode: 'starting', served: long });
        await page.waitForSelector('[aria-labelledby="access-hold-title"]', { timeout: 20000 }).catch(() => {});
        await pause(1500);
        await shot(page, '14c-long-address-hold', await facts(page));
        await page.reload({ waitUntil: 'networkidle0' });
        await pause(1500);
        await shot(page, '14d-long-address-readiness-page', await facts(page));
        await closePage(page);
    },
    // 5: the access hold over an ordinary page with unsent input.
    async hold() {
        await reset();
        const page = await newPage();
        const other = await page.__context.newPage();
        await page.bringToFront();
        await page.goto(`${base}/domains`, { waitUntil: 'networkidle0' });
        await clickByText(page, ['Add domain', 'Alan adı ekle', 'Alan Adı Ekle']);
        await page.waitForSelector('[aria-labelledby="add-domain-title"] input[type="text"]');
        await page.type('[aria-labelledby="add-domain-title"] input[type="text"]', 'typed-by-owner.example');
        const field = await page.$eval('[aria-labelledby="add-domain-title"] input[type="text"]', node => { const r = node.getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 }; });
        await shot(page, '05a-dialog-typed');
        const value = () => page.$eval('[aria-labelledby="add-domain-title"] input[type="text"]', node => node.value).catch(() => null);
        // Watches every frame for the hold layer so a flash is not missed.
        await page.evaluate(() => { window.__holdSeen = []; new MutationObserver(() => { const node = document.querySelector('[aria-labelledby="access-hold-title"]'); if (node) window.__holdSeen.push([Date.now(), node.querySelector('h3')?.innerText]); }).observe(document.body, { childList: true, subtree: true }); });

        // 5.1 the decision lapses in a hidden tab; the read answers on return.
        await drainLog();
        await other.bringToFront();
        await pause(66000);
        const hiddenReads = (await drainLog()).filter(line => line.includes('/license/access'));
        const hiddenState = await page.evaluate(() => ({ visibility: document.visibilityState, inert: !!document.querySelector('[data-access-hold="blocked"]'), layer: !!document.querySelector('[aria-labelledby="access-hold-title"]') }));
        await page.bringToFront();
        await pause(120);
        await page.screenshot({ path: `${out}05b-return-ok-first-frame.png` });
        await pause(2500);
        await shot(page, '05b-return-ok', { hiddenReads, hiddenState, holdSeenOnReturn: await page.evaluate(() => window.__holdSeen.slice()), fieldValue: await value() });

        // 5.2 the same, but the read fails on return.
        await page.evaluate(() => { window.__holdSeen = []; });
        await other.bringToFront();
        // What the real access route answers when the status is unknown: 200 with a typed body
        // (cmd/panel/license.go handleLicenseAccess). It never answers a coded 503 itself.
        await ctl({ license: 'unavailable' });
        await pause(66000);
        await page.bringToFront();
        const returned = Date.now();
        await page.waitForSelector('[aria-labelledby="access-hold-title"]', { timeout: 20000 });
        const appeared = Date.now() - returned;
        await pause(400);
        await shot(page, '05c-hold-license', { appearedAfterMs: appeared, fieldValue: await value() });
        // Try to use the page underneath.
        await page.mouse.click(field.x, field.y);
        await page.keyboard.type('XYZ');
        const afterTyping = await value();
        const pathBefore = await page.evaluate(() => location.pathname);
        await page.mouse.click(vp === 'phone' ? 30 : 80, vp === 'phone' ? 30 : 122);
        await page.keyboard.press('Escape');
        await pause(300);
        const stillThere = await page.evaluate(() => ({ layer: !!document.querySelector('[aria-labelledby="access-hold-title"]'), dialog: !!document.querySelector('[aria-labelledby="add-domain-title"]'), path: location.pathname }));
        const tabs = [];
        for (let i = 0; i < 8; i++) { await page.keyboard.press('Tab'); tabs.push(await page.evaluate(() => { const a = document.activeElement; const hold = document.querySelector('[aria-labelledby="access-hold-title"]'); return `${a?.tagName.toLowerCase()} "${(a?.innerText || '').slice(0, 30)}" inHold=${!!hold?.contains(a)}`; })); }
        const back = [];
        for (let i = 0; i < 4; i++) { await page.keyboard.down('Shift'); await page.keyboard.press('Tab'); await page.keyboard.up('Shift'); back.push(await page.evaluate(() => { const a = document.activeElement; const hold = document.querySelector('[aria-labelledby="access-hold-title"]'); return `${a?.tagName.toLowerCase()} "${(a?.innerText || '').slice(0, 30)}" inHold=${!!hold?.contains(a)}`; })); }
        await shot(page, '05d-hold-keyboard-focus', { afterTyping, pathBefore, stillThere, tabs, back });
        // Pointer at the element the browser would hit at the field's position.
        const hit = await page.evaluate(point => { const node = document.elementFromPoint(point.x, point.y); return node ? `${node.tagName.toLowerCase()}.${String(node.className).slice(0, 60)} inert=${!!node.closest('[inert]')}` : null; }, field);
        // The reload option after half a minute.
        await drainLog();
        const countFrom = Date.now();
        await waitFor(page, () => document.querySelectorAll('[aria-labelledby="access-hold-title"] button').length >= 2, 45000).catch(() => {});
        const prolongedAfter = Date.now() - returned;
        // How often the automatic re-read really runs while the layer is up.
        const readsWhileHeld = (await drainLog()).filter(line => line.includes('/license/access')).map(line => line.slice(0, 12));
        const cycle = [];
        for (let i = 0; i < 5; i++) { await page.keyboard.press('Tab'); cycle.push(await page.evaluate(() => { const a = document.activeElement; const hold = document.querySelector('[aria-labelledby="access-hold-title"]'); return `${a?.tagName.toLowerCase()} "${(a?.innerText || '').slice(0, 30)}" inHold=${!!hold?.contains(a)}`; })); }
        await shot(page, '05e-hold-prolonged', { prolongedAfterMs: prolongedAfter, hit, readsWhileHeld, readsWindowMs: Date.now() - countFrom, tabCycleWithReload: cycle });
        // The read succeeds.
        await ctl({ license: 'active' });
        const fixed = Date.now();
        await waitFor(page, () => !document.querySelector('[aria-labelledby="access-hold-title"]'), 20000).catch(() => {});
        const gone = Date.now() - fixed;
        await pause(500);
        await shot(page, '05f-after-hold', { goneAfterMs: gone, fieldValue: await value(), dialogStill: await page.evaluate(() => !!document.querySelector('[aria-labelledby="add-domain-title"]')) });
        await page.keyboard.type('!');
        report.states['05f-after-hold'].typedAfter = await value();

        // 5.3 slow read on return: the neutral "checking" layer.
        await ctl({ validity: 12 });
        await page.evaluate(() => window.dispatchEvent(new Event('focus')));
        await pause(1500);
        await other.bringToFront();
        await ctl({ license: 'hang' });
        await pause(14000);
        await page.bringToFront();
        await pause(2300);
        await shot(page, '05g-hold-waiting');
        await pause(15000);
        await shot(page, '05h-hold-after-timeout');
        await ctl({ license: 'active', validity: 60 });
        await waitFor(page, () => !document.querySelector('[aria-labelledby="access-hold-title"]'), 20000).catch(() => {});
        await pause(400);
        await shot(page, '05i-after-waiting', { fieldValue: await value() });

        // 5.2b the read fails as a dropped connection (the Panel is not reachable).
        await ctl({ validity: 12 });
        await page.evaluate(() => window.dispatchEvent(new Event('focus')));
        await pause(1500);
        await other.bringToFront();
        await ctl({ license: 'drop' });
        await pause(14000);
        await drainLog();
        await page.bringToFront();
        const dropReturned = Date.now();
        await page.waitForSelector('[aria-labelledby="access-hold-title"]', { timeout: 20000 }).catch(() => {});
        const dropAppeared = Date.now() - dropReturned;
        await pause(16000);
        const dropReads = (await drainLog()).filter(line => line.includes('/license/access')).map(line => line.slice(0, 12));
        await shot(page, '05k-hold-network-failure', { appearedAfterMs: dropAppeared, readsWhileHeld: dropReads, readsWindowMs: Date.now() - dropReturned });
        await ctl({ license: 'active', validity: 60 });
        const dropFixed = Date.now();
        await waitFor(page, () => !document.querySelector('[aria-labelledby="access-hold-title"]'), 20000).catch(() => {});
        report.states['05k-hold-network-failure'].goneAfterMs = Date.now() - dropFixed;
        report.states['05k-hold-network-failure'].fieldValueAfter = await value();

        // 5.4 a management request is refused while visible (no tab switch): the real
        // server refuses such a route with a coded 503 while the access route answers "unknown".
        await ctl({ override: { '/api/v1/domains': { status: 503, body: { error: 'license status unavailable', code: 'LICENSE_STATUS_UNAVAILABLE' } } }, license: 'unavailable' });
        await drainLog();
        await page.evaluate(() => fetch('/api/v1/domains'));
        await page.waitForSelector('[aria-labelledby="access-hold-title"]', { timeout: 10000 }).catch(() => {});
        await pause(6000);
        await shot(page, '05j-hold-visible-refusal', { readsAfterOneRefusal: (await drainLog()).filter(line => line.includes('/license/access')).length });
        await ctl({ clear: ['/api/v1/domains'], license: 'active' });
        await waitFor(page, () => !document.querySelector('[aria-labelledby="access-hold-title"]'), 20000).catch(() => {});
        // 5.5 the chunk-reload notice above an open dialogue.
        await page.evaluate(() => window.dispatchEvent(new Event('vite:preloadError', { cancelable: true })));
        await pause(700);
        await shot(page, '10-update-reload-notice');
        await closePage(page);
    },
    // 6: a known negative still takes the whole screen.
    async negative() {
        for (const kind of ['missing', 'expired']) {
            await reset(); await ctl({ license: kind });
            const page = await newPage();
            await page.goto(`${base}/domains`, { waitUntil: 'networkidle0' });
            await pause(800);
            await shot(page, `06a-${kind}-first-load`);
            await closePage(page);
        }
        // Becomes known-negative under a mounted page with a dialogue open.
        await reset(); await ctl({ validity: 22 });
        const page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'networkidle0' });
        await clickByText(page, ['Add domain', 'Alan adı ekle', 'Alan Adı Ekle']);
        await page.waitForSelector('[aria-labelledby="add-domain-title"] input[type="text"]');
        await page.type('[aria-labelledby="add-domain-title"] input[type="text"]', 'typed.example');
        await ctl({ license: 'expired' });
        await waitFor(page, () => location.pathname === '/activate', 30000).catch(() => {});
        await pause(800);
        await shot(page, '06b-expired-under-mounted-page');
        await closePage(page);
    },
    // 7: first load.
    async firstload() {
        await reset(); await ctl({ override: { '/api/v1/auth/me': { delay: 5000 } } });
        let page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await pause(1800);
        await shot(page, '07a-checking-slow-session');
        await pause(5000);
        await shot(page, '07a-then-opens');
        await closePage(page);

        await reset(); await ctl({ override: { '/api/v1/panel/availability': { delay: 5000 } } });
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await pause(1800);
        await shot(page, '07b-checking-slow-readiness');
        await closePage(page);

        await reset(); await ctl({ override: { '/api/v1/panel/availability': { status: 500, body: { error: 'x' } } } });
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await pause(2500);
        await shot(page, '07c-readiness-could-not-be-checked');
        await ctl({ clear: ['/api/v1/panel/availability'] });
        const t0 = Date.now();
        await waitFor(page, () => !!document.querySelector('nav, aside'), 20000).catch(() => {});
        report.states['07c-readiness-could-not-be-checked'].recoveredAfterMs = Date.now() - t0;
        await closePage(page);

        await reset(); await ctl({ override: { '/api/v1/auth/me': { status: 500, body: { error: 'x' } } } });
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await pause(2500);
        await shot(page, '07d-session-could-not-be-checked');
        await closePage(page);

        await reset(); await ctl({ license: 'unavailable' });
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await pause(2500);
        await drainLog();
        await pause(11000);
        await shot(page, '07e-license-could-not-be-checked', { readsIn11s: (await drainLog()).filter(line => line.includes('/license/access')).length });
        await closePage(page);

        await reset(); await ctl({ license: 'drop' });
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await pause(2500);
        await shot(page, '07g-license-read-dropped-first-load');
        await closePage(page);

        await reset(); await ctl({ license: 'hang' });
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await pause(2500);
        await shot(page, '07f-license-read-slow-first-load');
        await closePage(page);
    },
    // 8: the session ended under a page in use.
    async session() {
        await reset();
        const page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'networkidle0' });
        await clickByText(page, ['Add domain', 'Alan adı ekle', 'Alan Adı Ekle']);
        await page.waitForSelector('[aria-labelledby="add-domain-title"] input[type="text"]');
        await page.type('[aria-labelledby="add-domain-title"] input[type="text"]', 'typed.example');
        await ctl({ session: false });
        await page.evaluate(() => window.dispatchEvent(new Event('focus')));
        await page.waitForSelector('input[type="password"]', { timeout: 10000 });
        await pause(500);
        await shot(page, '08a-session-ended');
        const inputs = await page.$$('form input');
        await inputs[0].type('admin'); await page.type('input[type="password"]', 'correct');
        await page.keyboard.press('Enter');
        await waitFor(page, () => !document.querySelector('input[type="password"]'), 10000).catch(() => {});
        await page.waitForNetworkIdle({ idleTime: 500, timeout: 8000 }).catch(() => {});
        await shot(page, '08b-after-sign-in');
        // An ordinary sign-out shows no reason.
        await page.evaluate(() => fetch('/api/v1/auth/logout', { method: 'POST' }).then(() => window.dispatchEvent(new Event('focus'))));
        await closePage(page);
    },
    // 9: known-bad reference: capabilities slow while the dialogue opens.
    async capabilities() {
        await reset(); await ctl({ override: { '/api/v1/hosting/capabilities': { delay: 9000 } } });
        const page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('h1');
        await pause(1200);
        await shot(page, '09-0-domains-while-capabilities-slow');
        await clickByText(page, ['Add domain', 'Alan adı ekle', 'Alan Adı Ekle']).catch(error => report.errors.push(`09: ${error.message}`));
        await pause(600);
        await shot(page, '09a-add-domain-capabilities-slow');
        await pause(10000);
        await shot(page, '09a-then-capabilities-arrived');
        await closePage(page);
        // The page's own read is quick; the dialogue's read is the slow one.
        await reset(); await ctl({ override: { '/api/v1/hosting/capabilities': { delay: 9000, after: 1 } } });
        const second = await newPage();
        await second.goto(`${base}/domains`, { waitUntil: 'networkidle0' });
        await clickByText(second, ['Add domain', 'Alan adı ekle', 'Alan Adı Ekle']);
        await second.waitForSelector('[aria-labelledby="add-domain-title"]');
        await pause(700);
        await shot(second, '09b-dialog-capabilities-slow', { dialog: await second.$eval('[aria-labelledby="add-domain-title"]', node => node.innerText) });
        await pause(10000);
        await shot(second, '09c-dialog-capabilities-arrived', { dialog: await second.$eval('[aria-labelledby="add-domain-title"]', node => node.innerText) });
        await closePage(second);
    },
    // 20-24: Add domain and the Domains page while this server's capabilities
    // are slow, failing, known negative and known positive (9 Oct 2026). The
    // report an owner made: the dialogue showed "choose a DNS engine" and a
    // disabled form for the seconds the read took, on a server that had DNS.
    async adddomain() {
        // Slow: frames of the page and of the dialogue while the read is on its way.
        await remoteReset(); await ctl({ domains: DOMAINS, override: { [CAPS]: { delay: 9000 } } });
        let page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('h1');
        await pause(900);
        await shot(page, '20a-domains-capabilities-checking', await negatives(page));
        await clickByText(page, ADD_DOMAIN);
        await page.waitForSelector(`${DIALOG} input[type="text"]`);
        await page.type(`${DIALOG} input[type="text"]`, 'typed.example');
        const frames = [];
        for (const at of [1, 2, 3, 4]) {
            await shot(page, `20b-dialog-checking-frame${at}`);
            frames.push({ at: Date.now(), ...(await dialogFacts(page)) });
            await pause(700);
        }
        await waitFor(page, sel => !document.querySelector(sel)?.querySelector('[role="status"]'), 12000, DIALOG);
        await pause(300);
        const arrived = await dialogFacts(page);
        await shot(page, '20c-dialog-known-positive', {
            frames, arrived,
            blockerSeenWhileChecking: frames.some(frame => frame.blocker.length > 0),
            framesStillChecking: frames.filter(frame => frame.checkingLine).length,
            // What moved between the last checking frame and the known answer.
            moved: moved(frames[frames.length - 1].rects, arrived.rects),
            typedKept: await page.$eval(`${DIALOG} input[type="text"]`, node => node.value),
            capabilityReads: await capabilityReads(),
        });
        await closePage(page);

        // Failing: the page stays quiet, the dialogue says it could not check, Retry reads again.
        await remoteReset(); await ctl({ domains: DOMAINS, override: { [CAPS]: { status: 502, body: { error: 'agent unavailable', code: 'AGENT_UNAVAILABLE' } } } });
        page = await newPage();
        await go(page, '/domains');
        await shot(page, '21a-domains-capabilities-failed', await negatives(page));
        await clickByText(page, ADD_DOMAIN);
        await page.waitForSelector(`${DIALOG} input[type="text"]`);
        await page.type(`${DIALOG} input[type="text"]`, 'typed.example');
        await shot(page, '21b-dialog-could-not-check', await dialogFacts(page));
        await ctl({ clear: [CAPS] });
        await clickByText(page, RETRY, `${DIALOG} button`);
        await waitFor(page, sel => !document.querySelector(sel)?.querySelector('[role="alert"]'), 8000, DIALOG);
        await pause(300);
        await shot(page, '21c-dialog-after-retry', { ...(await dialogFacts(page)), typedKept: await page.$eval(`${DIALOG} input[type="text"]`, node => node.value) });
        await closePage(page);

        // Known negative: no engine (empty list, then with domains), then an engine without its identity.
        await remoteReset(); await ctl({ capabilities: { ...CAPABILITIES, dns_server: '', dns_identity_ready: false, dns_management_ready: false } });
        page = await newPage();
        await go(page, '/domains');
        await shot(page, '22a-domains-known-no-engine-empty', await negatives(page));
        await ctl({ domains: DOMAINS });
        await page.reload({ waitUntil: 'domcontentloaded' }); await quiet(page);
        await shot(page, '22b-domains-known-no-engine-with-domains', await negatives(page));
        await ctl({ capabilities: { ...CAPABILITIES, dns_identity_ready: false, dns_management_ready: false } });
        await page.reload({ waitUntil: 'domcontentloaded' }); await quiet(page);
        await shot(page, '22c-domains-known-no-identity', await negatives(page));
        await closePage(page);
        // The dialogue's own blocker: external DNS serves websites, there is no
        // web server, and a DNS-only domain needs a local engine that is known
        // to be missing.
        await remoteReset(); await ctl({ capabilities: { ...CAPABILITIES, web_server: '', dns_server: '', dns_identity_ready: false, dns_management_mode: 'external', dns_management_ready: true } });
        page = await newPage();
        await go(page, '/domains');
        await clickByText(page, ADD_DOMAIN);
        await page.waitForSelector(`${DIALOG} input[type="text"]`);
        await pause(300);
        await shot(page, '22d-dialog-known-negative', await dialogFacts(page));
        await closePage(page);

        // Known positive, and the dialogue opened over a page that already has the answer.
        await remoteReset(); await ctl({ domains: DOMAINS });
        page = await newPage();
        await go(page, '/domains');
        await shot(page, '23a-domains-known-positive', await negatives(page));
        await drainLog();
        await clickByText(page, ADD_DOMAIN);
        await page.waitForSelector(`${DIALOG} input[type="text"]`);
        const first = await dialogFacts(page);
        await pause(600);
        await shot(page, '23b-dialog-over-a-page-that-knows', { first, ...(await dialogFacts(page)), capabilityReadsAfterOpening: await capabilityReads() });
        await closePage(page);
    },
    // 25: the domain list itself: slow, failing (then Retry), empty, populated.
    async domainslist() {
        await remoteReset(); await ctl({ domains: DOMAINS, override: { [LIST]: { delay: 5000 } } });
        let page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('h1');
        await pause(900);
        const checking = await pageFacts(page);
        await shot(page, '25a-domains-list-checking', { ...(await negatives(page)), ...checking });
        await waitFor(page, () => !!document.querySelector('main table'), 12000);
        await pause(300);
        const known = await pageFacts(page);
        await shot(page, '25b-domains-list-arrived', { ...known, moved: moved(checking.rects, known.rects) });
        await closePage(page);

        await remoteReset(); await ctl({ domains: DOMAINS, override: { [LIST]: { status: 500, body: { error: 'database is locked', code: 'INTERNAL' } } } });
        page = await newPage();
        // The page of one domain needs the list too; the list page is what is inspected here.
        await go(page, '/domains');
        await shot(page, '25c-domains-list-could-not-read', await negatives(page));
        await ctl({ clear: [LIST] });
        await clickByText(page, RETRY, 'main button');
        await waitFor(page, () => !!document.querySelector('main table'), 8000);
        await shot(page, '25d-domains-list-after-retry', await negatives(page));
        await closePage(page);

        await remoteReset();
        page = await newPage();
        await go(page, '/domains');
        await shot(page, '25e-domains-list-known-empty', await negatives(page));
        await closePage(page);
    },
    // 30-33: the Databases page: engines and their lists slow, failing, empty,
    // populated; a list that could not be read again; the panel's own account
    // after it was removed.
    async databases() {
        await remoteReset(); await ctl({ ...DB, override: { [ENGINES]: { delay: 5000 } } });
        let page = await newPage();
        await page.goto(`${base}/databases`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('h1');
        await pause(900);
        const checking = await pageFacts(page);
        await shot(page, '30a-engines-checking', { ...(await negatives(page)), ...checking });
        await waitFor(page, () => !!document.querySelector('main table'), 12000);
        await pause(300);
        await shot(page, '30b-engines-arrived-populated', await pageFacts(page));
        await closePage(page);

        await remoteReset(); await ctl({ ...DB, override: { [ENGINES]: { status: 502, body: { error: 'agent unavailable', code: 'AGENT_UNAVAILABLE' } } } });
        page = await newPage();
        await go(page, '/databases');
        await shot(page, '30c-engines-could-not-read', await negatives(page));
        await ctl({ clear: [ENGINES] });
        await clickByText(page, RETRY, 'main button');
        await waitFor(page, () => !!document.querySelector('main table'), 8000);
        await shot(page, '30d-engines-after-retry', await negatives(page));
        await closePage(page);

        await remoteReset();
        page = await newPage();
        await go(page, '/databases');
        await shot(page, '30e-engines-known-none', await negatives(page));
        await closePage(page);

        // The lists of one engine.
        await remoteReset(); await ctl({ ...DB, override: { [ENGINE_DATABASES]: { delay: 5000 }, [ENGINE_USERS]: { delay: 5000 } } });
        page = await newPage();
        await page.goto(`${base}/databases`, { waitUntil: 'domcontentloaded' });
        await waitFor(page, () => /MariaDB/.test(document.querySelector('main')?.innerText || ''), 8000);
        await pause(600);
        const listChecking = await pageFacts(page);
        await shot(page, '31a-engine-lists-checking', { ...(await negatives(page)), ...listChecking });
        await waitFor(page, () => !!document.querySelector('main table'), 12000);
        await pause(300);
        const listKnown = await pageFacts(page);
        await shot(page, '31b-engine-lists-arrived', { ...listKnown, moved: moved(listChecking.rects, listKnown.rects) });
        await closePage(page);

        await remoteReset(); await ctl({ ...DB, override: { [ENGINE_DATABASES]: { drop: true }, [ENGINE_USERS]: { status: 500, body: { error: 'connection refused' } } } });
        page = await newPage();
        await go(page, '/databases');
        await shot(page, '31c-engine-databases-could-not-read', await negatives(page));
        await clickByText(page, USERS_TAB, 'main button');
        await pause(300);
        await shot(page, '31d-engine-users-could-not-read', await negatives(page));
        await closePage(page);

        await remoteReset(); await ctl({ ...DB, dbDatabases: [], dbUsers: [] });
        page = await newPage();
        await go(page, '/databases');
        await shot(page, '31e-engine-databases-known-empty', await negatives(page));
        await closePage(page);

        // A list that was known and could not be read again after a delete.
        await remoteReset(); await ctl(DB);
        page = await newPage();
        page.on('dialog', dialog => dialog.accept());
        await go(page, '/databases');
        await ctl({ override: { [ENGINE_DATABASES]: { status: 502, body: { error: 'agent unavailable' } } } });
        await page.click('main table tbody tr:first-child button');
        await waitFor(page, () => !!document.querySelector('main [role="alert"]'), 8000);
        await pause(300);
        await shot(page, '32-engine-list-could-not-be-read-again', {
            ...(await negatives(page)),
            deleteDisabled: await page.$$eval('main table tbody button', nodes => nodes.map(node => node.disabled)),
        });
        await closePage(page);

        // The panel's own account: removed, then what the strip shows.
        await remoteReset(); await ctl(DB);
        page = await newPage();
        page.on('dialog', dialog => dialog.accept());
        await go(page, '/databases');
        await shot(page, '33a-account-present', await negatives(page));
        await clickByText(page, REMOVE_ACCOUNT, 'main button');
        await page.waitForNetworkIdle({ idleTime: 500, timeout: 8000 }).catch(() => {});
        await pause(300);
        await shot(page, '33b-account-after-removal', {
            ...(await negatives(page)),
            buttons: await page.$$eval('main button', nodes => nodes.map(node => node.innerText.trim()).filter(Boolean)),
        });
        await closePage(page);
    },
    // 40-41: one domain's page: the connection card slow, failing, not checked
    // by the server (status unknown, every list null), known negative and
    // known positive; and the domain's own database list.
    async connection() {
        const open = async (settled = true) => {
            const page = await newPage();
            await page.goto(`${base}/domains/example.com`, { waitUntil: 'domcontentloaded' });
            await page.waitForSelector('main h1, main h2');
            if (settled) await quiet(page);
            return page;
        };
        await remoteReset(); await ctl({ domains: DOMAINS, connection: CONNECTION, override: { [CONN]: { delay: 5000 } } });
        let page = await open(false);
        await pause(1500);
        const checking = await pageFacts(page);
        await shot(page, '40a-connection-checking', { ...(await negatives(page)), ...checking });
        await waitFor(page, () => /192\.0\.2\.4/.test(document.querySelector('main')?.innerText || ''), 12000);
        await pause(300);
        const known = await pageFacts(page);
        await shot(page, '40b-connection-known-positive', { ...known, moved: moved(checking.rects, known.rects) });
        await closePage(page);

        await remoteReset(); await ctl({ domains: DOMAINS, connection: CONNECTION, override: { [CONN]: { status: 502, body: { error: 'agent unavailable' } } } });
        page = await open();
        await shot(page, '40c-connection-could-not-read', await negatives(page));
        await closePage(page);

        // What the server sends when it could not ask the public resolvers.
        await remoteReset(); await ctl({ domains: DOMAINS, connection: { ...CONNECTION, status: 'unknown', ssl_ready: false, nameservers_usable: false, live_nameservers: null, live_ips: null, resolver_observations: null, nameserver_facts: null } });
        page = await open();
        await shot(page, '40d-connection-status-unknown-top', await negatives(page));
        await page.evaluate(() => document.querySelector('main section')?.scrollIntoView({ block: 'end' }));
        await pause(200);
        await shot(page, '40d-connection-status-unknown-bottom', await negatives(page));
        await closePage(page);

        await remoteReset(); await ctl({ domains: DOMAINS, connection: { ...CONNECTION, status: 'unresolved', ssl_ready: false, live_nameservers: [], live_ips: [] } });
        page = await open();
        await shot(page, '40e-connection-known-negative', await negatives(page));
        await closePage(page);

        // The domain's databases tab.
        const tab = async page => { await clickByText(page, DATABASES_TAB, 'main button'); await pause(400); };
        await remoteReset(); await ctl({ domains: DOMAINS, connection: CONNECTION, override: { [DOMAIN_DATABASES]: { delay: 5000 } } });
        page = await open();
        await tab(page);
        const dbChecking = await pageFacts(page);
        await shot(page, '41a-domain-databases-checking', { ...(await negatives(page)), ...dbChecking });
        await closePage(page);

        await remoteReset(); await ctl({ domains: DOMAINS, connection: CONNECTION, override: { [DOMAIN_DATABASES]: { status: 500, body: { error: 'Failed to load databases' } }, [CAPS]: { status: 502, body: { error: 'agent unavailable' }, after: 1 } } });
        page = await open();
        await tab(page);
        await shot(page, '41b-domain-databases-could-not-read', await negatives(page));
        await closePage(page);

        await remoteReset(); await ctl({ domains: DOMAINS, connection: CONNECTION });
        page = await open();
        await tab(page);
        await shot(page, '41c-domain-databases-known-empty', await negatives(page));
        await closePage(page);

        await remoteReset(); await ctl({ domains: DOMAINS, connection: CONNECTION, domainDatabases: { databases: [{ id: 3, name: 'example_com_shop', type: 'mysql', user: 'example_com_shop', created_at: '2026-10-01T09:00:00Z' }], available_types: ['mysql'] } });
        page = await open();
        await tab(page);
        await shot(page, '41d-domain-databases-populated', await negatives(page));
        await closePage(page);
    },
};

for (const [name, run] of Object.entries(scenarios)) {
    if (wanted && !wanted.includes(name)) continue;
    const started = Date.now();
    try { await run(); } catch (error) { report.errors.push(`${name}: ${error.message}`.slice(0, 400)); }
    console.log(`${vp}-${theme}-${locale} ${name} ${Math.round((Date.now() - started) / 1000)}s`);
}
await writeFile(`${out}report${wanted ? '-' + wanted.join('-') : ''}.json`, JSON.stringify(report, null, 2));
// The record is written. Closing the browser can wait for a page that still
// has a request open (a read that was made to hang or fail on purpose), so it
// gets a few seconds and is then stopped; nothing is lost with it.
await Promise.race([browser.close().catch(() => {}), pause(5000)]);
try { browser.process()?.kill(); } catch { /* already gone */ }
mock.kill();
console.log('errors', JSON.stringify(report.errors));
process.exit(0);
