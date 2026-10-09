// Scenarios of 10 Oct 2026: the request identity (D-029) on its eight routes,
// together with the lost-answer handling of the fourth batch; what a service
// action ends with on the Services screens; a page that predates the update.
// Registered by run.mjs. The mock (mock-batch5.mjs) keeps the guard's contract:
// the same identity with the same request is answered from the first arrival's
// stored answer, an arrival while the first still runs gets 409
// REQUEST_IN_PROGRESS, a request without the header gets 428.
//
// For each of the eight changes these scenarios press the control ONCE and
// then look at four things: what the page shows, how often the request arrived
// at the mock, how often the change was MADE, and which identity each arrival
// carried. A scenario FAILS (its error is in the report) when
//   - one click made the change more than once, or two arrivals of one click
//     carried different identities;
//   - a state that should show a result, a notice or a result-unknown notice
//     shows none: a record with nothing measured is not a record;
//   - a lost answer that the second asking brought back is still shown as
//     "result unknown", or a result that is not known is shown as done or as a
//     failure;
//   - a notice says "nothing was sent a second time" for a change whose answer
//     was asked for again, or does not say which of the two happened;
//   - a change that was made while its one-time result did not arrive does not
//     say what to do;
//   - an unknown result of a service action stands on the failure surface.
// Looking at the screenshots is still the inspection.
import { b4Defaults } from './mock-batch4.mjs';
import { b5Defaults, GUARDED } from './mock-batch5.mjs';

const D = '/api/v1/domains/1';
const DOMAINS = [{ id: 1, domain_name: 'example.com', status: 'active', project_type: 'php', php_version: '8.3', ssl_enabled: true, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 0 }];
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3', '8.2'], mail_server: true, database_servers: ['mariadb'], db_tools: [] };
const CONNECTION = { domain: 'example.com', server_ip: '192.0.2.4', nameservers: ['ns1.example.net', 'ns2.example.net'], live_nameservers: ['ns1.example.net', 'ns2.example.net'], live_ips: ['192.0.2.4'], status: 'delegated', ssl_ready: true };
const DB = {
    dbServers: [{ id: 1, type_id: 1, type_name: 'mariadb', type_icon: 'M', name: 'MariaDB', version: '11.4', host: 'localhost', port: 3306, is_default: true, status: 'active', created_at: '2026-09-01T10:00:00Z', admin_username: 'celikpanel_admin', is_local: true }],
    dbDatabases: [{ id: 5, name: 'example_com_shop', users: ['example_com_shop'], created_at: '2026-09-02T10:00:00Z' }],
    dbUsers: [{ id: 7, username: 'example_com_shop', databases: ['example_com_shop'], created_at: '2026-09-02T10:00:00Z' }],
};
const DOMAIN_DATABASES = { databases: [{ id: 3, name: 'shop', type: 'mariadb', user: 'shop', created_at: '2026-09-01T10:00:00Z' }], available_types: ['mysql'] };
const CERTIFICATE = {
    domain_id: 1, domain_name: 'example.com', has_certificate: true, managed_names: ['example.com', 'www.example.com'],
    settings: { force_https: true, hsts_enabled: false, hsts_max_age: 300 },
    certificate: {
        id: 4, type: 'letsencrypt', provider_id: 'letsencrypt', issuer: 'R11', subject: 'example.com', issued_at: '2026-10-10T08:00:00Z', expires_at: '2027-01-08T08:00:00Z',
        days_until_expiry: 90, auto_renew: true, renewal_status: 'ok', status: 'active', dns_names: ['example.com', 'www.example.com'],
        activated: true, usable: true, trust_status: 'trusted', activation_pending: false, dependents_pending: false,
    },
};
const PREVIEW = { username: 'olduser', main_domain: 'old.example', domains: ['old.example'], public_html: true, site_bytes: 48234496, mail_accounts: [{ domain: 'old.example', user: 'info', quota_mb: 1024 }], forwarders: [], dns_zones: { 'old.example': [{}, {}, {}] }, databases: [{ name: 'olduser_shop', dump_bytes: 2048 }] };
const SUBS = [{ id: 3, name: 'Main', owner: 'admin' }];
const component = (id, name, category, extra = {}) => ({ id, name, description: `${name} on this server`, icon: '', category, kind: 'service', unit: id, versions: [], status: 'active (running)', is_installed: true, config_files: [], packages: [id], ports: [], ...extra });
const profile = (id, name, services) => ({ id, name, description: `${name} for this server`, status: 'available', available: true, verified: false, latest_attempt_status: 'none', services });
const SCAN = {
    scanned_at: new Date().toISOString(), dns_identity_ready: true,
    mail_hostname: { current: 'server1.example.com', hostname: '', source: '', current_usable: true, will_set_hostname: false },
    profiles: [profile('core-mail', 'Mail', ['postfix', 'dovecot']), profile('webmail', 'Webmail', ['roundcube']), profile('protected-mail', 'Protected mail', ['rspamd'])],
    services: [
        component('redis', 'Redis', 'cache', { unit: 'redis-server', config_files: [{ path: '/etc/redis/redis.conf', is_managed: false }] }),
        component('postfix', 'Postfix', 'mail'), component('dovecot', 'Dovecot', 'mail'),
        { ...component('roundcube', 'Roundcube', 'mail'), kind: 'tool', is_installed: false, status: '' },
        { ...component('rspamd', 'Rspamd', 'mail'), is_installed: false, status: '' },
    ],
};

const T = { hosting: ['Hosting', 'Barındırma'], advanced: ['Advanced', 'Gelişmiş'], backups: ['Backups', 'Yedekler'], databases: ['Databases', 'Veritabanları'], devices: ['Devices', 'Cihazlar'] };
const CHECK_AGAIN = ['Check again', 'Tekrar kontrol et'];
const CLOSE = ['Close', 'Kapat'];
const RELOAD_SENTENCE = /Reload the page|Sayfayı yeniden yükleyin/;
const SENT_TWICE = /sent a second time|ikinci kez gönderil/;
const ASKED_AGAIN = /asking the server once more|bir kez daha istendiğinde/;

export default function register(scenarios, tools) {
    const { base, vp, ctl, reset, drainLog, newPage, closePage, shot, clickByText, waitFor, pause, quiet } = tools;
    const must = (condition, message) => { if (!condition) throw new Error(message); };
    const counters = async () => (await fetch(`${base}/api/v1/__b5`)).json();

    const fresh = async (plan = {}, extra = {}) => {
        await reset();
        await ctl({ clearAll: true });
        await ctl({
            domains: DOMAINS, capabilities: CAPABILITIES, connection: CONNECTION, domainDatabases: DOMAIN_DATABASES, ssl: null, sslAfterIssue: CERTIFICATE,
            // The scan is as old as this moment, not as old as the run: the
            // components list scans by itself over a scan older than its limit,
            // and then offers no action until that scan has answered.
            subscriptions: SUBS, importPreview: PREVIEW, ...DB, managedScan: { ...SCAN, scanned_at: new Date().toISOString() }, logs: [],
            b4: b4Defaults(), b5: { ...b5Defaults(), plan }, ...extra,
        });
        await drainLog();
    };
    const done = () => ctl({ b4: null, b5: null, clearAll: true });

    // What is on screen that says what became of the change.
    const facts = (page) => page.evaluate(() => {
        const visible = (node) => node && node.getClientRects().length > 0;
        const label = (node) => (node.innerText || node.getAttribute('aria-label') || node.title || '').trim().replace(/\s+/g, ' ');
        const text = (selector) => { const node = Array.from(document.querySelectorAll(selector)).find(visible); return node ? node.innerText.trim().replace(/\s+/g, ' ') : null; };
        const unknown = Array.from(document.querySelectorAll('[data-result-unknown]')).find(visible);
        const importUnknown = Array.from(document.querySelectorAll('[data-import-unknown]')).find(visible);
        const action = Array.from(document.querySelectorAll('[data-service-action]')).find(visible);
        const surface = (node) => {
            if (!node) return null;
            const style = getComputedStyle(node);
            const [r, g, b] = (style.backgroundColor.match(/[\d.]+/g) || []).slice(0, 3).map(Number);
            const icon = node.querySelector('svg');
            return { background: style.backgroundColor, redder: r > g * 1.6 && r > b * 1.6, icon: icon ? getComputedStyle(icon).color : '', classes: node.className };
        };
        const inView = (node) => { if (!node) return null; const r = node.getBoundingClientRect(); return r.top >= 0 && r.bottom <= window.innerHeight; };
        return {
            resultUnknown: unknown ? { state: unknown.dataset.resultUnknown, cause: unknown.dataset.lostCause, text: unknown.innerText.trim().replace(/\s+/g, ' '), inView: inView(unknown), surface: surface(unknown) } : null,
            importUnknown: importUnknown ? { cause: importUnknown.dataset.importUnknown, text: importUnknown.innerText.trim().replace(/\s+/g, ' '), inView: inView(importUnknown), surface: surface(importUnknown) } : null,
            onceOnly: text('[data-once-only]'),
            onceOnlySurface: surface(Array.from(document.querySelectorAll('[data-once-only]')).find(visible)),
            serviceAction: action ? { tone: action.dataset.serviceAction, text: action.innerText.trim().replace(/\s+/g, ' '), inView: inView(action), surface: surface(action) } : null,
            notices: Array.from(document.querySelectorAll('[role="alert"]')).filter(visible).map((node) => node.innerText.trim().replace(/\s+/g, ' ')),
            toasts: Array.from(document.querySelectorAll('.fixed.top-4.right-4 > *')).map((node) => ({ text: node.innerText.trim(), classes: node.className })),
            dialogOpen: Array.from(document.querySelectorAll('[role="dialog"], dialog[open]')).some(visible),
            enabledButtons: Array.from(document.querySelectorAll('main button:not(:disabled), [role="dialog"] button:not(:disabled)')).filter(visible).map(label).filter(Boolean),
            disabledButtons: Array.from(document.querySelectorAll('main button:disabled, [role="dialog"] button:disabled')).filter(visible).map(label).filter(Boolean),
            mainText: (document.querySelector('main')?.innerText || '').replace(/\s+/g, ' ').slice(0, 2500),
            pageOverflowsSideways: document.documentElement.scrollWidth > window.innerWidth + 1,
        };
    });
    // The page with every request it sends that is not a read (what the page
    // itself sends: the browser's own repeats on a reset connection are seen
    // at the mock, not here).
    const open = async (path, ready) => {
        const page = await newPage();
        page.on('dialog', (dialog) => { void dialog.accept(); });
        page.changes = [];
        page.on('request', (request) => { if (request.method() !== 'GET') page.changes.push(`${request.method()} ${new URL(request.url()).pathname}`); });
        await page.goto(`${base}${path}`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('main');
        if (ready) await waitFor(page, ready, 15000);
        await quiet(page);
        return page;
    };
    const clickExact = async (page, texts, scope = 'main button, main [role="tab"]') => {
        const find = () => page.evaluateHandle((list, sel) => Array.from(document.querySelectorAll(sel))
            .find((node) => node.getClientRects().length > 0 && !node.disabled && list.includes((node.innerText || '').trim().replace(/\s*[\d…–]+$/, ''))) || null, texts, scope);
        let element = (await find()).asElement();
        for (let waited = 0; !element && waited < 10000; waited += 250) { await pause(250); element = (await find()).asElement(); }
        if (!element) throw new Error(`no enabled control labelled ${texts.join(' | ')}`);
        await element.evaluate((node) => node.scrollIntoView({ block: 'center' }));
        await element.click();
    };
    const domainTab = async (tabs) => {
        const page = await open('/domains/example.com', () => Array.from(document.querySelectorAll('main button')).some((node) => ['Hosting', 'Barındırma'].includes((node.innerText || '').trim())));
        for (const [index, labels] of tabs.entries()) { await clickExact(page, labels); if (index < tabs.length - 1) await pause(350); }
        await quiet(page);
        await pause(300);
        return page;
    };
    const typeInto = async (page, selector, value) => { await page.waitForSelector(selector, { visible: true }); await page.click(selector, { clickCount: 3 }); await page.type(selector, value); };
    const record = async (page, name, more = {}) => {
        await page.evaluate(() => {
            const visible = (node) => node && node.getClientRects().length > 0;
            const target = ['[data-result-unknown]', '[data-import-unknown]', '[data-once-only]', '[data-service-action]'].map((selector) => Array.from(document.querySelectorAll(selector)).find(visible)).find(Boolean);
            if (target) target.scrollIntoView({ block: 'center', inline: 'nearest' });
        });
        await pause(120);
        const seen = await facts(page);
        await shot(page, name, { ...seen, ...more });
        return seen;
    };

    // --- The eight changes ---------------------------------------------------
    // open: brings the page to the control. act: presses it once. done: what
    // the page shows when the change's own result arrived. after: the state of
    // the result-unknown notice once the page was read again (`made` where the
    // form asks the list whether it shows the change). secret: what the page
    // shows when the change was made and its one-time result was not kept.
    const toast = (...phrases) => (seen) => seen.toasts.some((item) => phrases.some((phrase) => item.text.includes(phrase)));
    const flows = {
        backup: {
            number: 110, key: GUARDED.backup, after: 'read', reread: `${D}/backups`, held: ['Create backup', 'Yedek oluştur'],
            open: () => domainTab([T.advanced, T.backups]),
            act: (page) => page.evaluate(() => { const card = document.querySelector('main section button'); card.scrollIntoView({ block: 'center' }); card.click(); }),
            done: toast('Backup created', 'Yedek oluşturuldu'),
            shown: (seen) => seen.mainText.includes('example.com-files-20261010-091500.tar.gz'),
        },
        restore: {
            number: 111, key: GUARDED.restore, after: 'read', reread: `${D}/backups`, held: ['Restore', 'Geri yükle'],
            open: () => domainTab([T.advanced, T.backups]),
            // An icon in the row; its name is its title.
            act: (page) => page.evaluate(() => {
                const button = Array.from(document.querySelectorAll('main button[aria-label="Restore"], main button[aria-label="Geri yükle"]')).find((node) => !node.disabled);
                button.scrollIntoView({ block: 'center' });
                button.click();
            }),
            done: toast('Backup restored', 'Yedek geri yüklendi'),
        },
        certificate: {
            number: 112, key: GUARDED.certificate, after: 'read', reread: `${D}/ssl`, held: ['Get certificate', 'Sertifika al'],
            open: async () => {
                const page = await domainTab([T.hosting, ['SSL/TLS']]);
                await waitFor(page, () => !document.querySelector('main [role="status"][aria-label]') && !!document.querySelector('main input[type="email"]'), 15000);
                await page.type('main input[type="email"]', 'owner@example.com');
                return page;
            },
            act: (page) => clickExact(page, ['Get certificate', 'Sertifika al']),
            done: toast('Certificate issued', 'Sertifika alındı'),
            shown: (seen) => /R11|Let’s Encrypt|Let's Encrypt/.test(seen.mainText) && !/No certificate|Sertifika yok/.test(seen.mainText),
        },
        domainDatabase: {
            number: 113, key: GUARDED.domainDatabase, after: 'made', reread: `${D}/databases`, held: ['Create Database'],
            open: async () => {
                const page = await domainTab([T.databases]);
                await clickExact(page, ['Create Database']);
                await typeInto(page, 'main form input[type="text"]', 'orders');
                await typeInto(page, 'main form input[type="password"]', 'a-Long-passw0rd');
                return page;
            },
            act: (page) => page.evaluate(() => { const button = document.querySelector('main form button[type="submit"]'); button.scrollIntoView({ block: 'center' }); button.click(); }),
            done: toast('Database "orders" created'),
            shown: (seen) => /\borders\b/.test(seen.mainText),
        },
        serverDatabase: {
            // With a new user, a database found made by the lists says the
            // same as the status-only answer: its password was shown to nobody.
            number: 114, key: GUARDED.serverDatabase, after: 'once', secret: 'once', reread: '/api/v1/database-servers/1/databases', held: ['Create Database'],
            open: async () => {
                const page = await open('/databases', () => Array.from(document.querySelectorAll('main button')).some((node) => ['Create database', 'Veritabanı oluştur'].includes((node.innerText || '').trim()) && !node.disabled));
                await clickExact(page, ['Create database', 'Veritabanı oluştur']);
                await typeInto(page, '[role="dialog"] input[placeholder="myapp_db"]', 'example_com_orders');
                await typeInto(page, '[role="dialog"] input[placeholder="Username"]', 'orders_app');
                await typeInto(page, '[role="dialog"] input[placeholder="Password"]', 'a-Long-passw0rd');
                return page;
            },
            act: (page) => page.evaluate(() => document.querySelector('[role="dialog"] button[type="submit"]').click()),
            done: toast('Database created: example_com_orders'),
            shown: (seen) => seen.mainText.includes('example_com_orders'),
            onceOnly: /example_com_orders.*orders_app|orders_app.*example_com_orders/,
            whatToDo: /set a new password for orders_app|orders_app için .*yeni bir parola belirleyin/,
        },
        account: {
            number: 115, key: GUARDED.account, after: 'read', secret: 'success', reread: '/api/v1/database-servers', held: ['New password', 'Yeni parola', 'Show password', 'Parolayı göster', 'Remove account', 'Hesabı kaldır'],
            open: () => open('/databases', () => Array.from(document.querySelectorAll('main button')).some((node) => ['New password', 'Yeni parola'].includes((node.innerText || '').trim()) && !node.disabled)),
            act: (page) => clickExact(page, ['New password', 'Yeni parola']),
            done: toast('The account has a new password', 'Hesabın parolası değişti'),
        },
        peer: {
            number: 116, key: GUARDED.peer, after: 'once', secret: 'once', reread: '/api/v1/vpn/peers', held: ['Create config', 'Config oluştur'],
            open: async () => {
                const page = await open('/vpn', () => Array.from(document.querySelectorAll('main button')).some((node) => ['Devices', 'Cihazlar'].includes((node.innerText || '').trim())));
                await clickExact(page, T.devices);
                await typeInto(page, 'main input[maxlength="60"]', 'work-laptop');
                return page;
            },
            act: (page) => clickExact(page, ['Create config', 'Config oluştur']),
            done: (seen) => /Configuration for work-laptop|work-laptop için yapılandırma/.test(seen.mainText),
            shown: (seen) => seen.mainText.includes('work-laptop'),
            onceOnly: /work-laptop/,
            whatToDo: /remove it; then add the device again|kaldırın; sonra .*cihazı yeniden ekleyin/,
        },
        importApply: {
            number: 117, key: GUARDED.importApply, after: 'read', own: true,
            open: async () => {
                const page = await open('/import');
                await page.waitForSelector('main input');
                await page.type('main input', '/var/lib/celikpanel-imports/cpmove-old.tar.gz');
                await clickByText(page, ['Inspect archive', 'Arşivi incele'], 'main button');
                await page.waitForSelector('main select');
                await waitFor(page, () => document.querySelectorAll('main select')[1]?.options.length > 1, 12000);
                await page.evaluate(() => { const select = document.querySelectorAll('main select')[1]; select.value = '3'; select.dispatchEvent(new Event('change', { bubbles: true })); });
                await pause(200);
                return page;
            },
            act: (page) => clickExact(page, ['Start import', 'İçe aktarmayı başlat']),
            done: (seen) => /Import finished|İçe aktarma tamamlandı/.test(seen.mainText),
        },
    };

    // The notice of a result that is not known, wherever this flow draws it.
    const unknownOf = (flow, seen) => (flow.own
        ? (seen.importUnknown ? { cause: seen.importUnknown.cause, state: 'read', text: seen.importUnknown.text, inView: seen.importUnknown.inView, surface: seen.importUnknown.surface } : null)
        : seen.resultUnknown);
    const waitUnknown = (page, flow, timeout = 20000) => waitFor(page, (own) => Boolean(document.querySelector(own ? '[data-import-unknown]' : '[data-result-unknown]')), timeout, Boolean(flow.own));
    const oneIdentity = (name, key, counts) => {
        const ids = counts.ids[key] || [];
        must(ids.length > 0, `${name}: the change never arrived`);
        must(new Set(ids).size === 1 && /^[0-9a-f]{32}$/.test(ids[0]), `${name}: the arrivals of one click did not carry one identity: ${ids.join(', ')}`);
    };
    const notAFailure = (name, notice) => {
        must(notice.surface && !notice.surface.redder, `${name}: a result that is not known stands on the failure surface (${notice.surface?.background})`);
    };
    const measured = (flow, counts, page) => ({
        arrivals: counts.arrivals[flow.key] || 0, made: counts.effects[flow.key] || 0, answeredFromTheRow: counts.replays[flow.key] || 0,
        identities: new Set(counts.ids[flow.key] || []).size, sentByThePage: page.changes.filter((item) => item === flow.key).length,
    });

    const cases = {
        // (i) The answer arrives.
        normal: async (flow, name) => {
            await fresh();
            const page = await flow.open();
            await flow.act(page);
            await waitFor(page, () => document.querySelectorAll('.fixed.top-4.right-4 > *').length > 0 || /Configuration for|için yapılandırma|Import finished|İçe aktarma tamamlandı/.test(document.querySelector('main')?.innerText || ''), 15000);
            await pause(250);
            const counts = await counters();
            const seen = await record(page, `${name}a-answered`, measured(flow, counts, page));
            must(flow.done(seen), `${name}a: the change's own result is not on screen: toasts ${JSON.stringify(seen.toasts.map((item) => item.text))}`);
            must(!unknownOf(flow, seen) && !seen.onceOnly, `${name}a: an answered change is shown as unknown`);
            must(counts.arrivals[flow.key] === 1 && counts.effects[flow.key] === 1, `${name}a: one click, ${counts.arrivals[flow.key]} arrivals, made ${counts.effects[flow.key]} times`);
            oneIdentity(`${name}a`, flow.key, counts);
            await closePage(page);
        },
        // (ii) The connection is reset under the answer; the page asks once
        // more with the same identity and the mock answers from the row.
        replayed: async (flow, name) => {
            await fresh({ [flow.key]: { loseFor: 1000 } });
            const page = await flow.open();
            await flow.act(page);
            const outcome = flow.secret === 'once'
                ? () => Boolean(document.querySelector('[data-once-only]'))
                : () => document.querySelectorAll('.fixed.top-4.right-4 > *').length > 0 || /Configuration for|için yapılandırma|Import finished|İçe aktarma tamamlandı/.test(document.querySelector('main')?.innerText || '');
            await waitFor(page, outcome, 20000);
            await pause(300);
            const counts = await counters();
            const seen = await record(page, `${name}b-reset-then-answered-from-the-first-run`, measured(flow, counts, page));
            must(counts.effects[flow.key] === 1, `${name}b: one click made the change ${counts.effects[flow.key]} times`);
            must(counts.arrivals[flow.key] >= 2 && (counts.replays[flow.key] || 0) >= 1, `${name}b: the answer was not asked for again (${counts.arrivals[flow.key]} arrivals, ${counts.replays[flow.key] || 0} answered from the row)`);
            oneIdentity(`${name}b`, flow.key, counts);
            must(!unknownOf(flow, seen), `${name}b: the second asking was answered and the page still says the result is not known`);
            must(!seen.toasts.some((item) => /bg-danger/.test(item.classes)), `${name}b: a failure is shown: ${JSON.stringify(seen.toasts.map((item) => item.text))}`);
            if (flow.secret === 'once') {
                // (iv) The change was made; its one-time result was not kept.
                must(seen.onceOnly, `${name}b: nothing says that the one-time result cannot be shown again`);
                must(flow.onceOnly.test(seen.onceOnly), `${name}b: the notice does not name what was made: ${seen.onceOnly}`);
                must(/cannot be shown again|yeniden gösterilemez/.test(seen.onceOnly), `${name}b: the notice does not say the result cannot be shown again: ${seen.onceOnly}`);
                must(flow.whatToDo.test(seen.onceOnly), `${name}b: the notice does not say what to do: ${seen.onceOnly}`);
                must(seen.onceOnlySurface && !seen.onceOnlySurface.redder, `${name}b: a change that was made stands on the failure surface`);
                must(flow.shown(seen), `${name}b: what was made is not in the list that was read again`);
                must(!seen.dialogOpen, `${name}b: the dialogue is still open over a change that was made`);
            } else {
                must(flow.done(seen), `${name}b: the real result is not on screen: toasts ${JSON.stringify(seen.toasts.map((item) => item.text))}`);
                must(!seen.onceOnly, `${name}b: shown as a result that was not kept`);
            }
            await closePage(page);
        },
        // The second asking is lost too: the result is not known, and the
        // notice says that the answer was asked for again.
        asked: async (flow, name) => {
            await fresh({ [flow.key]: { loseFor: -1 } });
            const page = await flow.open();
            // The read that follows is withheld, so the state "not known, and
            // not read again yet" can be looked at: what it says, and that
            // nothing which changes or removes is offered meanwhile.
            if (flow.reread) await ctl({ override: { [flow.reread]: { delay: 3000, method: 'GET' } } });
            await flow.act(page);
            await waitUnknown(page, flow);
            if (flow.reread) {
                await pause(400);
                const held = await record(page, `${name}c1-second-asking-lost-too-held`, measured(flow, await counters(), page));
                const first = held.resultUnknown;
                must(first && first.state === 'holding', `${name}c1: the notice is not holding while nothing was read again (${first?.state})`);
                must(first.cause === 'asked', `${name}c1: the notice's cause is "${first.cause}"`);
                must(first.inView, `${name}c1: the notice is outside the window`);
                notAFailure(`${name}c1`, first);
                must(ASKED_AGAIN.test(first.text), `${name}c1: the notice does not say the answer was asked for again: ${first.text.slice(0, 200)}`);
                must(!SENT_TWICE.test(first.text), `${name}c1: the notice says nothing was sent a second time although the answer was asked for again: ${first.text.slice(0, 200)}`);
                const stillOn = flow.held.filter((label) => held.enabledButtons.includes(label));
                must(stillOn.length === 0, `${name}c1: enabled while the result is unknown and unread: ${stillOn.join(' | ')}`);
                must(!held.toasts.some((item) => /bg-danger|bg-success/.test(item.classes)), `${name}c1: an unknown result is shown as done or as a failure: ${JSON.stringify(held.toasts.map((item) => item.text))}`);
                await ctl({ clear: [flow.reread] });
            }
            if (flow.after === 'once') {
                // The lists that were read again show what was made; the page
                // now says that its one-time result reached nobody.
                await waitFor(page, () => Boolean(document.querySelector('[data-once-only]')), 20000);
                await quiet(page);
                await pause(300);
                const counts = await counters();
                const seen = await record(page, `${name}c2-found-made-result-not-shown`, measured(flow, counts, page));
                must(!seen.resultUnknown, `${name}c2: "result unknown" stays beside the notice that says what was made`);
                must(seen.onceOnly && flow.onceOnly.test(seen.onceOnly) && flow.whatToDo.test(seen.onceOnly), `${name}c2: the notice does not say what was made and what to do: ${seen.onceOnly}`);
                must(seen.onceOnlySurface && !seen.onceOnlySurface.redder, `${name}c2: a change that was made stands on the failure surface`);
                must(flow.shown(seen), `${name}c2: what was made is not in the list that was read again`);
                must(!seen.dialogOpen, `${name}c2: the dialogue is still open over a change that was made`);
                must(counts.effects[flow.key] === 1, `${name}c2: one click made the change ${counts.effects[flow.key]} times`);
                oneIdentity(`${name}c2`, flow.key, counts);
                await closePage(page);
                return;
            }
            await waitFor(page, (own) => own || ['read', 'made', 'not-made'].includes(document.querySelector('[data-result-unknown]')?.dataset.resultUnknown), 20000, Boolean(flow.own));
            await quiet(page);
            await pause(300);
            const counts = await counters();
            const seen = await record(page, `${name}c2-second-asking-lost-too-read-again`, measured(flow, counts, page));
            const notice = unknownOf(flow, seen);
            must(notice, `${name}c2: no notice that the result is not known`);
            must(notice.cause === 'asked', `${name}c: the notice's cause is "${notice.cause}"`);
            must(notice.inView, `${name}c: the notice is outside the window`);
            notAFailure(`${name}c`, notice);
            // Once the state that was read again shows the change, one sentence
            // is left: it was made. Until then the notice says which happened.
            must(ASKED_AGAIN.test(notice.text) || flow.own || notice.state === 'made', `${name}c: the notice does not say the answer was asked for again: ${notice.text.slice(0, 200)}`);
            if (notice.state === 'made') must(/so it was made|yani yapıldı/.test(notice.text), `${name}c: the notice does not say the change was made: ${notice.text.slice(0, 200)}`);
            must(!SENT_TWICE.test(notice.text) || notice.state === 'made', `${name}c: the notice says nothing was sent a second time although the answer was asked for again: ${notice.text.slice(0, 200)}`);
            must(notice.state === flow.after, `${name}c: after the page was read again the notice is "${notice.state}", expected "${flow.after}"`);
            must(counts.effects[flow.key] === 1, `${name}c: one click made the change ${counts.effects[flow.key]} times`);
            must(counts.arrivals[flow.key] >= 2, `${name}c: the answer was not asked for again (${counts.arrivals[flow.key]} arrival)`);
            oneIdentity(`${name}c`, flow.key, counts);
            must(!seen.toasts.some((item) => /bg-danger|bg-success/.test(item.classes)), `${name}c: an unknown result is shown as done or as a failure: ${JSON.stringify(seen.toasts.map((item) => item.text))}`);
            if (!flow.own) {
                if (flow.after === 'made') {
                    must(flow.shown(seen), `${name}c: the list that was read again does not show the change`);
                    must(!seen.enabledButtons.some((item) => CHECK_AGAIN.includes(item)), `${name}c: "Check again" is offered for a change that is shown as made`);
                } else {
                    await drainLog();
                    await clickExact(page, CHECK_AGAIN, 'main button, [role="dialog"] button');
                    await quiet(page);
                    const log = await drainLog();
                    must(log.length > 0 && log.every((line) => line.includes(' GET ')), `${name}c: "Check again" sent something other than a read: ${log.join(' ; ').slice(0, 200)}`);
                }
                await clickExact(page, CLOSE, 'main button, [role="dialog"] button');
                await pause(300);
                must(!(await facts(page)).resultUnknown, `${name}c: the notice stayed after Close`);
            }
            must((await counters()).effects[flow.key] === 1, `${name}c: looking again made the change a second time`);
            await closePage(page);
        },
        // (iii) The Panel restarted while the change ran: its own word that
        // the result is not known.
        interrupted: async (flow, name) => {
            await fresh({ [flow.key]: { restart: true, applied: true } });
            const page = await flow.open();
            await flow.act(page);
            if (flow.after === 'once') {
                await waitFor(page, () => Boolean(document.querySelector('[data-once-only]')), 20000);
                await quiet(page);
                await pause(300);
                const counts = await counters();
                const seen = await record(page, `${name}d-panel-restarted-found-made-result-not-shown`, measured(flow, counts, page));
                must(!seen.resultUnknown, `${name}d: "result unknown" stays beside the notice that says what was made`);
                must(seen.onceOnly && flow.onceOnly.test(seen.onceOnly) && flow.whatToDo.test(seen.onceOnly), `${name}d: the notice does not say what was made and what to do: ${seen.onceOnly}`);
                must(counts.effects[flow.key] === 1, `${name}d: one click made the change ${counts.effects[flow.key]} times`);
                oneIdentity(`${name}d`, flow.key, counts);
                must(!seen.toasts.some((item) => /bg-danger|bg-success/.test(item.classes)), `${name}d: also a toast: ${JSON.stringify(seen.toasts.map((item) => item.text))}`);
                await closePage(page);
                return;
            }
            await waitUnknown(page, flow);
            await waitFor(page, (own) => own || ['read', 'made', 'not-made'].includes(document.querySelector('[data-result-unknown]')?.dataset.resultUnknown), 20000, Boolean(flow.own));
            await quiet(page);
            await pause(300);
            const counts = await counters();
            const seen = await record(page, `${name}d-panel-restarted-outcome-unknown`, measured(flow, counts, page));
            const notice = unknownOf(flow, seen);
            must(notice, `${name}d: no notice that the result is not known`);
            must(notice.cause === 'interrupted', `${name}d: the notice's cause is "${notice.cause}"`);
            must(notice.inView, `${name}d: the notice is outside the window`);
            notAFailure(`${name}d`, notice);
            must(/restarted or failed while|yeniden başladı ya da hata verdi/.test(notice.text) || notice.state === 'made', `${name}d: the notice does not say what the Panel said: ${notice.text.slice(0, 200)}`);
            must(!SENT_TWICE.test(notice.text), `${name}d: the notice says "sent a second time": ${notice.text.slice(0, 200)}`);
            must(counts.effects[flow.key] === 1, `${name}d: one click made the change ${counts.effects[flow.key]} times`);
            oneIdentity(`${name}d`, flow.key, counts);
            must(!seen.toasts.some((item) => /bg-danger|bg-success/.test(item.classes)), `${name}d: an unknown result is also a toast: ${JSON.stringify(seen.toasts.map((item) => item.text))}`);
            await closePage(page);
        },
        // The first arrival is still running when the page asks again.
        running: async (flow, name) => {
            await fresh({ [flow.key]: { slow: 7000, loseFor: 1000 } });
            const page = await flow.open();
            await flow.act(page);
            await waitUnknown(page, flow);
            await pause(600);
            const counts = await counters();
            const seen = await record(page, `${name}e-still-running`, measured(flow, counts, page));
            const notice = unknownOf(flow, seen);
            must(notice && notice.cause === 'running', `${name}e: the notice's cause is "${notice?.cause}"`);
            notAFailure(`${name}e`, notice);
            must(/still running on the server|sunucuda hâlâ sürüyor/.test(notice.text), `${name}e: the notice does not say the change is still running: ${notice.text.slice(0, 200)}`);
            must((counts.effects[flow.key] || 0) <= 1, `${name}e: the change was started ${counts.effects[flow.key]} times`);
            oneIdentity(`${name}e`, flow.key, counts);
            // It finishes on the server; looking again shows it, and it ran once.
            await pause(6500);
            if (!flow.own) {
                await clickExact(page, CHECK_AGAIN, 'main button, [role="dialog"] button');
                await quiet(page);
                await pause(300);
            }
            const after = await counters();
            const later = await record(page, `${name}f-finished-meanwhile`, measured(flow, after, page));
            must(after.effects[flow.key] === 1, `${name}f: the change ran ${after.effects[flow.key]} times`);
            if (flow.shown) must(flow.shown(later), `${name}f: the state that was read again does not show the change that finished`);
            await closePage(page);
        },
        // (e of the merge check) A page that predates the update sends no
        // header: refused before anything runs, with the reload sentence.
        stale: async (flow, name) => {
            await fresh({ [flow.key]: { noHeader: true } });
            const page = await flow.open();
            await flow.act(page);
            await waitFor(page, () => document.querySelectorAll('.fixed.top-4.right-4 > *').length > 0, 15000);
            await pause(250);
            const counts = await counters();
            const seen = await record(page, `${name}g-page-older-than-the-panel`, measured(flow, counts, page));
            const said = seen.toasts.map((item) => item.text).join(' ');
            must(RELOAD_SENTENCE.test(said), `${name}g: the refusal does not tell the person to reload the page: ${said.slice(0, 200)}`);
            must(/nothing was changed|hiçbir şey değiştirilmedi/.test(said), `${name}g: the refusal does not say that nothing was changed: ${said.slice(0, 200)}`);
            must(!/X-CelikPanel|REQUEST_ID/.test(said), `${name}g: an internal name is on screen: ${said.slice(0, 200)}`);
            must((counts.effects[flow.key] || 0) === 0 && counts.arrivals[flow.key] === 1, `${name}g: a refused request arrived ${counts.arrivals[flow.key]} times and made the change ${counts.effects[flow.key] || 0} times`);
            must(!unknownOf(flow, seen), `${name}g: a refusal is shown as an unknown result`);
            await closePage(page);
        },
    };

    const runFlow = async (id, which) => {
        const flow = flows[id];
        try {
            for (const kind of which) await cases[kind](flow, `${flow.number}-${id}-`);
        } finally {
            await done();
        }
    };
    scenarios.idbackup = () => runFlow('backup', ['normal', 'replayed', 'asked', 'interrupted', 'running', 'stale']);
    scenarios.idrestore = () => runFlow('restore', ['normal', 'replayed', 'asked', 'interrupted']);
    scenarios.idcertificate = () => runFlow('certificate', ['normal', 'replayed', 'asked', 'interrupted']);
    scenarios.iddomaindb = () => runFlow('domainDatabase', ['normal', 'replayed', 'asked', 'interrupted']);
    scenarios.idserverdb = () => runFlow('serverDatabase', ['normal', 'replayed', 'asked', 'interrupted']);
    scenarios.idaccount = () => runFlow('account', ['normal', 'replayed', 'asked', 'interrupted']);
    scenarios.idpeer = () => runFlow('peer', ['normal', 'replayed', 'asked', 'interrupted', 'stale']);
    scenarios.idimport = () => runFlow('importApply', ['normal', 'replayed', 'asked', 'interrupted', 'running', 'stale']);

    // The Panel's own refusal with a gateway's status is its answer: shown as
    // it is, never asked for again. On a route whose answers are never stored
    // a second asking would replace it with "that answer is not kept".
    scenarios.idrefusal = async () => {
        try {
            const flow = flows.peer;
            await fresh({ [flow.key]: { fail: { status: 502, body: { error: 'WireGuard did not accept the new device: the Agent could not be reached.', code: 'AGENT_UNAVAILABLE' } } } });
            const page = await flow.open();
            await flow.act(page);
            await waitFor(page, () => document.querySelectorAll('.fixed.top-4.right-4 > *').length > 0, 15000);
            await pause(2200);
            const counts = await counters();
            const seen = await record(page, '118-refusal-with-a-gateway-status-is-the-answer', measured(flow, counts, page));
            must(counts.arrivals[flow.key] === 1, `118: the Panel's own 502 was asked for again (${counts.arrivals[flow.key]} arrivals)`);
            must((counts.effects[flow.key] || 0) === 0, '118: a refused change was made');
            must(seen.toasts.some((item) => item.text.includes('the Agent could not be reached')), `118: the Panel's sentence is not shown: ${JSON.stringify(seen.toasts.map((item) => item.text))}`);
            must(!seen.resultUnknown && !seen.onceOnly, '118: a refusal is shown as an unknown or an unkept result');
            await closePage(page);
        } finally {
            await done();
        }
    };

    // --- Start, Stop, Restart: what the service showed ----------------------------
    const OUTCOMES = {
        check: { code: 'SERVICE_ACTION_FAILED', reason: 'check', error: 'Nothing was changed: the service\'s own check refuses its configuration, so the action was not carried out.', vars: { command: 'sudo postfix check', detail: 'postfix: fatal: /etc/postfix/main.cf, line 41: bad numerical configuration: message_size_limit = lots' } },
        verify: { code: 'SERVICE_ACTION_FAILED', reason: 'verify', error: 'The action was sent, but afterwards the service\'s daemon is not in the state that was asked for.', vars: { command: 'sudo postfix status', detail: 'postfix/postfix-script: the Postfix mail system is not running', owner_unit: 'postfix@-.service' } },
        unknown: { code: 'SERVICE_ACTION_UNKNOWN', error: 'The action was sent, but what came of it could not be verified, so it is not reported as done.', vars: { command: 'sudo systemctl status redis-server.service', detail: 'the state of redis-server.service could not be read in time' } },
    };
    const actionCase = async (page, name, outcome, press) => {
        await ctl({ b5: { ...b5Defaults(), serviceAction: outcome } });
        await drainLog();
        await press(page);
        await waitFor(page, () => Boolean(document.querySelector('[data-service-action]')), 15000);
        await quiet(page);
        await pause(300);
        const counts = await counters();
        const seen = await record(page, name, { actionsArrived: counts.actions });
        const notice = seen.serviceAction;
        const unknown = outcome.code === 'SERVICE_ACTION_UNKNOWN';
        must(notice, `${name}: the outcome is not on the page`);
        must(notice.tone === (unknown ? 'unknown' : 'failed'), `${name}: drawn as "${notice.tone}"`);
        must(notice.surface.redder === !unknown, `${name}: ${unknown ? 'an unknown result stands on the failure surface' : 'a verified failure is not on the failure surface'} (${notice.surface.background})`);
        must(notice.inView, `${name}: the outcome is outside the window`);
        must(notice.text.includes(outcome.vars.detail), `${name}: the service's own line is not shown: ${notice.text.slice(0, 300)}`);
        must(notice.text.includes(outcome.vars.command), `${name}: the command to run is not shown: ${notice.text.slice(0, 300)}`);
        must(!notice.text.includes(outcome.error), `${name}: the server's English sentence is shown in place of the catalogue's`);
        must(!/\{\w+\}|SERVICE_ACTION/.test(notice.text), `${name}: a placeholder or an internal name is on screen: ${notice.text.slice(0, 200)}`);
        if (outcome.vars.owner_unit) must(notice.text.includes(outcome.vars.owner_unit), `${name}: the unit that runs the service is not named`);
        if (unknown) must(/not a verified failure|doğrulanmış bir hata değildir/.test(notice.text), `${name}: the unknown result does not say it is not a failure`);
        must(counts.actions === 1, `${name}: one click, ${counts.actions} arrivals`);
        must(seen.toasts.length === 0, `${name}: the outcome is also a toast: ${JSON.stringify(seen.toasts.map((item) => item.text))}`);
        return seen;
    };
    scenarios.serviceaction = async () => {
        try {
            await fresh();
            // A component's own page (the generic one, in the shell).
            let page = await open('/services/redis', () => Array.from(document.querySelectorAll('main button')).some((node) => ['Restart', 'Yeniden başlat'].includes((node.innerText || '').trim()) && !node.disabled));
            const restart = (at) => clickExact(at, ['Restart', 'Yeniden başlat']);
            await actionCase(page, '120a-component-page-action-unknown', { ...OUTCOMES.unknown }, restart);
            await clickExact(page, CLOSE);
            await pause(300);
            must(!(await facts(page)).serviceAction, '120a: the notice stayed after Close');
            await actionCase(page, '120b-component-page-action-failed-verify', { ...OUTCOMES.verify, vars: { ...OUTCOMES.verify.vars, command: 'sudo systemctl status redis-server.service', detail: 'redis-server.service: Main process exited, code=exited, status=1/FAILURE', owner_unit: undefined } }, restart);
            await closePage(page);

            // Postfix's page: the check refuses, nothing was sent; and the unit that runs it.
            page = await open('/services/postfix', () => Array.from(document.querySelectorAll('main button')).some((node) => ['Restart', 'Yeniden başlat'].includes((node.innerText || '').trim()) && !node.disabled));
            await actionCase(page, '120c-postfix-page-action-failed-check', OUTCOMES.check, restart);
            await actionCase(page, '120d-postfix-page-action-failed-verify-owner-unit', OUTCOMES.verify, restart);
            await closePage(page);

            // The components list: the row action, then the confirmation.
            page = await open('/services', () => document.querySelectorAll('main button[title="Restart"], main button[title="Yeniden başlat"]').length > 0);
            const fromRow = async (at) => {
                await at.evaluate(() => { const button = document.querySelector('main button[title="Restart"], main button[title="Yeniden başlat"]'); button.scrollIntoView({ block: 'center' }); button.click(); });
                try {
                    await at.waitForSelector('[role="dialog"] button', { visible: true, timeout: 8000 });
                } catch {
                    // What the page did instead of asking for confirmation is the record.
                    const seen = await record(at, '120-components-list-no-confirmation');
                    throw new Error(`the components list did not ask to confirm the action: toasts ${JSON.stringify(seen.toasts.map((item) => item.text))}; notices ${JSON.stringify(seen.notices).slice(0, 300)}`);
                }
                await pause(200);
                await at.evaluate(() => { const buttons = Array.from(document.querySelectorAll('[role="dialog"] button')).filter((node) => node.getClientRects().length > 0 && !node.disabled); buttons[buttons.length - 1].click(); });
            };
            await actionCase(page, '120e-components-list-action-unknown', OUTCOMES.unknown, fromRow);
            await actionCase(page, '120f-components-list-action-failed', { ...OUTCOMES.verify, vars: { ...OUTCOMES.verify.vars, command: 'sudo systemctl status redis-server.service', detail: 'redis-server.service: Main process exited, code=exited, status=1/FAILURE', owner_unit: undefined } }, fromRow);
            await closePage(page);
        } finally {
            await done();
        }
    };
}
