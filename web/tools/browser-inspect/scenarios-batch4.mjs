// Scenarios of the fourth batch of "no negative UI unless known" (9 Oct 2026):
// the panels of one domain (DNS records and signing, hosting type and its
// live application, PHP, the general settings, the applications, mail
// authentication, the logs, the backups, the certificate card of the
// overview) and the row actions of a table on a phone. Registered by run.mjs.
//
// For each read these scenarios make it slow, failing and known; for a change
// they lose its answer. A scenario FAILS (its error is in the report) when:
//   - a state that should show a checking line, a notice or a result-unknown
//     notice shows none: a record with nothing measured is not a record;
//   - a negative sentence is on screen while its read is slow or failing;
//   - a lost change arrived at the mock more than once;
//   - a row action cannot be reached at the width of the screen without
//     scrolling the table sideways, or its label breaks into two lines;
//   - after a lost answer the notice is not in the state the re-read calls for
//     (10 Oct 2026: `made` when the state read again shows the change and the
//     form that sent it is closed, `not-made` when it does not and what was
//     typed is still there), or the strip under a domain's name still says it
//     is checking the certificate after that read failed.
// Looking at the screenshots is still the inspection.
import { b4Defaults } from './mock-batch4.mjs';
import { b3Defaults } from './mock-batch3.mjs';

const D = '/api/v1/domains/1';
const DOMAINS = [{ id: 1, domain_name: 'example.com', status: 'active', project_type: 'php', php_version: '8.3', ssl_enabled: true, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 0 }];
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3', '8.2'], mail_server: true, database_servers: ['mariadb'], db_tools: [] };
const CONNECTION = { domain: 'example.com', server_ip: '192.0.2.4', nameservers: ['ns1.example.net', 'ns2.example.net'], live_nameservers: ['ns1.example.net', 'ns2.example.net'], live_ips: ['192.0.2.4'], status: 'delegated', ssl_ready: true, glue_needed: false, nameservers_usable: true, propagation_pending: false, checked_at: '2026-10-09T09:00:00Z', dns_management_mode: 'local' };
const DB = {
    dbServers: [{ id: 1, type_id: 1, type_name: 'mariadb', type_icon: 'M', name: 'MariaDB', version: '11.4', host: 'localhost', port: 3306, is_default: true, status: 'active', created_at: '2026-09-01T10:00:00Z', admin_username: 'celikpanel_admin', is_local: true }],
    dbDatabases: [{ id: 5, name: 'example_com_shop', users: ['example_com_shop'], created_at: '2026-09-02T10:00:00Z' }, { id: 6, name: 'example_com_blog', users: ['example_com_blog', 'reporting'], created_at: '2026-09-03T10:00:00Z' }],
    dbUsers: [{ id: 7, username: 'example_com_shop', databases: ['example_com_shop'], created_at: '2026-09-02T10:00:00Z' }],
};
const component = (id, name, category, extra = {}) => ({ id, name, description: `${name} on this server`, icon: '', category, kind: 'service', unit: id, versions: [], status: 'active (running)', is_installed: true, config_files: [], ...extra });
const profile = (id, name, services) => ({ id, name, description: `${name} for this server`, status: 'available', available: true, verified: false, latest_attempt_status: 'none', services });
const F2B_SCAN = {
    scanned_at: new Date().toISOString(), dns_identity_ready: true,
    mail_hostname: { current: 'server1.example.com', hostname: '', source: '', current_usable: true, will_set_hostname: false },
    profiles: [profile('core-mail', 'Mail', ['postfix', 'dovecot']), profile('webmail', 'Webmail', ['roundcube']), profile('protected-mail', 'Protected mail', ['rspamd'])],
    services: [
        component('fail2ban', 'Fail2ban', 'security'), component('postfix', 'Postfix', 'mail'), component('dovecot', 'Dovecot', 'mail'),
        { ...component('roundcube', 'Roundcube', 'mail'), kind: 'tool', is_installed: false, status: '' },
        { ...component('rspamd', 'Rspamd', 'mail'), is_installed: false, status: '' },
    ],
};

// Sentences that claim something about the server. Each may be on screen only
// when the server has said so.
const NEGATIVE = [
    'DNS zone is not active', 'No records yet', 'No aliases yet', 'No applications available', 'No backups yet', 'No linked databases', 'No log lines', 'Stopped', 'Missing', 'HTTPS is off', 'Pool configuration is not available', 'PHP-FPM is not installed',
    'DNS bölgesi aktif değil', 'Henüz kayıt yok', 'Henüz takma ad yok', 'Kullanılabilir uygulama yok', 'Henüz yedek yok', 'Bağlı veritabanı yok', 'Günlük satırı yok', 'Durdu', 'Eksik', 'HTTPS kapalı', 'Havuz yapılandırması mevcut değil',
];
const RETRY = ['Retry', 'Tekrar dene'];
const CHECK_AGAIN = ['Check again', 'Tekrar kontrol et'];
const CLOSE = ['Close', 'Kapat'];
const T = {
    hosting: ['Hosting', 'Barındırma'], dns: ['DNS'], mail: ['Mail', 'E-posta'], apps: ['Applications', 'Uygulamalar'], advanced: ['Advanced', 'Gelişmiş'],
    general: ['General', 'Genel'], type: ['Hosting type', 'Barındırma tipi'], php: ['PHP'], backups: ['Backups', 'Yedekler'], logs: ['Logs', 'Loglar'], auth: ['Authentication', 'Kimlik doğrulama'],
};

export default function register(scenarios, tools) {
    const { base, vp, ctl, reset, drainLog, newPage, shot, clickByText, waitFor, pause, quiet } = tools;

    const fresh = async (b4 = {}, extra = {}) => {
        await reset();
        await ctl({ clearAll: true });
        const defaults = b4Defaults();
        await ctl({ domains: DOMAINS, capabilities: CAPABILITIES, connection: CONNECTION, domainDatabases: defaults.databases, ssl: null, b4: { ...defaults, ...b4 }, ...extra });
        await drainLog();
    };
    // The routes of this batch answer only while a scenario of it runs.
    const done = () => ctl({ b4: null, clearAll: true });
    const facts = (page) => page.evaluate((phrases) => {
        const scope = document.querySelector('main') || document.body;
        const text = scope.innerText || '';
        const label = (node) => (node.innerText || node.getAttribute('aria-label') || node.title || '').trim().replace(/\s+/g, ' ');
        const visible = (node) => node.getClientRects().length > 0;
        const fields = Array.from(scope.querySelectorAll('input:not([type="search"]), textarea, select')).filter(visible);
        const unknown = document.querySelector('[data-result-unknown]');
        return {
            // As whole words: "Durdu" (stopped) is not the button "Durdur" (stop).
            negativeText: phrases.filter((phrase) => new RegExp(`(^|[^\\p{L}])${phrase.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}($|[^\\p{L}])`, 'u').test(text)),
            checkingLines: Array.from(scope.querySelectorAll('[role="status"][aria-label]')).filter(visible).map((node) => node.getAttribute('aria-label')),
            notices: Array.from(scope.querySelectorAll('[role="alert"]')).filter(visible).map((node) => node.innerText.trim()),
            resultUnknown: unknown ? unknown.dataset.resultUnknown : null,
            toasts: Array.from(document.querySelectorAll('.fixed.top-4.right-4 > *')).map((node) => node.innerText.trim()),
            fields: fields.length,
            enabledButtons: Array.from(scope.querySelectorAll('button:not(:disabled)')).filter(visible).map(label).filter(Boolean),
            disabledButtons: Array.from(scope.querySelectorAll('button:disabled')).filter(visible).map(label).filter(Boolean),
            pageOverflowsSideways: document.documentElement.scrollWidth > window.innerWidth + 1,
        };
    }, NEGATIVE);
    // What a state is about is brought into the window before it is
    // photographed: the result-unknown notice, else a notice, else a checking
    // line, else `show`, else the start of the tab's own content. On a phone
    // the tabs of a domain alone fill the first screen.
    const record = async (page, name, more = {}, show = null) => {
        await page.evaluate((selector) => {
            const visible = (node) => node && node.getClientRects().length > 0;
            const first = (sel) => Array.from(document.querySelectorAll(sel)).find(visible);
            const target = (selector && first(selector)) || first('[data-result-unknown]') || first('main [role="alert"]') || first('main [role="status"][aria-label]');
            if (target) { target.scrollIntoView({ block: 'center', inline: 'nearest' }); return; }
            const content = first('main .rounded-xl.border.border-border.bg-surface.p-5');
            if (content) content.scrollIntoView({ block: 'start', inline: 'nearest' });
        }, show);
        await pause(150);
        const seen = await facts(page);
        await shot(page, name, { ...seen, ...more });
        return seen;
    };
    // Where the first element matching `selector` stands on the page, whatever
    // was scrolled; null when nothing matches.
    const placeOf = (page, selector, texts = null) => page.evaluate((sel, list) => {
        const node = Array.from(document.querySelectorAll(sel)).find((item) => item.getClientRects().length > 0 && (!list || list.some((text) => (item.innerText || '').includes(text))));
        if (!node) return null;
        const r = node.getBoundingClientRect();
        let scrolled = 0;
        for (let at = node.parentElement; at; at = at.parentElement) scrolled += at.scrollTop;
        return { y: Math.round(r.y + scrolled), h: Math.round(r.height) };
    }, selector, texts);
    const clickExact = async (page, texts) => {
        const find = () => page.evaluateHandle((list) => Array.from(document.querySelectorAll('main button, main [role="tab"]'))
            .find((node) => node.getClientRects().length > 0 && list.includes((node.innerText || '').trim().replace(/\s*[\d…–]+$/, ''))) || null, texts);
        let element = (await find()).asElement();
        for (let waited = 0; !element && waited < 8000; waited += 250) { await pause(250); element = (await find()).asElement(); }
        if (!element) throw new Error(`no tab labelled ${texts.join(' | ')}`);
        await element.evaluate((node) => node.scrollIntoView({ block: 'center' }));
        await element.click();
    };
    // One domain's page with a tab (and a tab under it) open. The reads of a
    // tab start when it opens, so what withholds them is set before the tab.
    const openTab = async (tabs, override = null) => {
        const page = await newPage();
        page.on('dialog', (dialog) => { void dialog.accept(); });
        await page.goto(`${base}/domains/example.com`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('main h1, main h2, main h3');
        await quiet(page);
        // Set before the first tab: a tab opens on its first tab under it, so
        // that one's reads start with the click on its parent. Nothing waits
        // for the network between the clicks, or a slowed read would be over
        // before the state it causes was looked at.
        if (override) await ctl({ override });
        // Every request the page itself makes that is not a read, for the
        // scenarios that lose the answer to one.
        page.changes = [];
        page.on('request', (request) => { if (request.method() !== 'GET') page.changes.push(`${request.method()} ${new URL(request.url()).pathname}`); });
        for (const [index, labels] of tabs.entries()) {
            await clickExact(page, labels);
            if (index < tabs.length - 1) await pause(350);
        }
        return page;
    };
    const settle = async (page) => { await quiet(page); await pause(400); };
    const must = (condition, message) => { if (!condition) throw new Error(message); };
    const quietNegatives = (seen, name) => must(seen.negativeText.length === 0, `${name}: a negative sentence is on screen while its read is not known: ${seen.negativeText.join(' | ')}`);

    // One read slow, then known; then failing, then read again with Retry.
    // `under` names what stands below the changing part and must not move.
    const threeStates = async (tabs, readPath, name, { under = null, mayBeNegativeWhenKnown = true } = {}) => {
        let page = await openTab(tabs, { [readPath]: { delay: 4500 } });
        await waitFor(page, () => Boolean(document.querySelector('main [role="status"][aria-label]')), 8000).catch(() => {});
        await pause(400);
        const before = under ? await placeOf(page, under.selector, under.texts) : null;
        let seen = await record(page, `${name}a-checking`, under ? { under: before } : {});
        must(seen.checkingLines.length > 0, `${name}a: no checking line was on screen, so the checking state was not recorded`);
        must(seen.notices.length === 0, `${name}a: a notice is shown for a read that has not failed: ${seen.notices.join(' | ').slice(0, 120)}`);
        quietNegatives(seen, `${name}a`);
        await ctl({ clear: [readPath] });
        await waitFor(page, () => !document.querySelector('main [role="status"][aria-label]'), 15000);
        await settle(page);
        const after = under ? await placeOf(page, under.selector, under.texts) : null;
        if (under) must(before && after, `${name}b: nothing matched ${under.selector}, so nothing was measured`);
        seen = await record(page, `${name}b-known`, under ? { under: after, moved: after.y - before.y } : {});
        must(seen.checkingLines.length === 0 && seen.notices.length === 0, `${name}b: the known state still shows a checking line or a notice`);
        if (!mayBeNegativeWhenKnown) quietNegatives(seen, `${name}b`);
        await page.close();

        page = await openTab(tabs, { [readPath]: { status: 502, body: { error: 'agent unavailable' } } });
        await page.waitForSelector('main [role="alert"]', { timeout: 10000 }).catch(() => {});
        await pause(400);
        seen = await record(page, `${name}c-could-not-check`);
        must(seen.notices.length > 0, `${name}c: no could-not-check notice was on screen`);
        must(seen.enabledButtons.some((item) => RETRY.includes(item)), `${name}c: Retry is not offered`);
        must(seen.toasts.length === 0, `${name}c: a failed read raised a toast: ${seen.toasts.join(' | ')}`);
        quietNegatives(seen, `${name}c`);
        await ctl({ clear: [readPath] });
        await drainLog();
        await clickByText(page, RETRY);
        await waitFor(page, () => !document.querySelector('main [role="alert"]'), 15000);
        await settle(page);
        const log = await drainLog();
        must(log.length > 0 && log.every((line) => line.includes(' GET ')), `${name}d: Retry sent something other than a read: ${log.join(' ; ').slice(0, 200)}`);
        seen = await record(page, `${name}d-after-retry`, { retryRequests: log.length });
        return page;
    };

    // A change whose answer does not arrive. `start` does what the person
    // does; the read that follows is slowed so the held state can be seen.
    const sentCount = async (key) => ((await (await fetch(`${base}/api/v1/__b4`)).json()).sent[key] || 0);
    const sentIds = async (key) => ((await (await fetch(`${base}/api/v1/__b4`)).json()).ids[key] || []);
    const noticePlace = (page) => page.evaluate(() => {
        const node = document.querySelector('[data-result-unknown]');
        if (!node) return null;
        const r = node.getBoundingClientRect();
        return { state: node.dataset.resultUnknown, inView: r.top >= 0 && r.bottom <= window.innerHeight, top: Math.round(r.top), height: Math.round(r.height) };
    });
    // `expect` is the notice's state after the re-read: `read` for a change
    // with no form to ask, `made` or `not-made` for a form that asks the
    // re-read state whether it shows the change.
    //
    // `identified` (10 Oct 2026, D-029): the change is one of the eight that
    // carry an identity the server keeps. The page then asks once more for the
    // same answer, under the same identity, before it says the result is not
    // known, and its notice says so. This mock loses that second asking too:
    // what the page does when the second asking IS answered is in the
    // scenarios of batch 5, whose mock keeps the guard's contract.
    const lostChange = async (page, name, { key, rereadPath, start, held, loseAs = 'drop', loseApplied = true, b4 = {}, expect = 'read', afterRead = null, identified = false }) => {
        await ctl({ b4: { ...b4Defaults(), ...b4, lose: key, loseAs, loseApplied }, override: { [rereadPath]: { delay: 2500 } } });
        await start(page);
        await page.waitForSelector('[data-result-unknown]', { timeout: 15000 }).catch(() => {});
        await pause(500);
        let place = await noticePlace(page);
        const cause = await page.evaluate(() => document.querySelector('[data-result-unknown]')?.dataset.lostCause ?? null);
        let seen = await record(page, `${name}a-result-unknown-held`, { notice: place, cause, sent: await sentCount(key) });
        must(place, `${name}a: no result-unknown notice appeared after the answer was lost`);
        // The notice says which happened: nothing was sent again, or the same
        // answer was asked for once more.
        must(cause === (identified ? 'asked' : 'dropped'), `${name}a: the notice's cause is "${cause}" for a change that ${identified ? 'carries an identity' : 'carries no identity'}`);
        const noticeText = seen.notices.join(' ');
        const saysNothingSentAgain = /sent a second time|ikinci kez gönderil/.test(noticeText);
        must(saysNothingSentAgain !== identified, `${name}a: the notice ${identified ? 'says nothing was sent a second time although the answer was asked for again' : 'does not say that nothing was sent a second time'}: ${noticeText.slice(0, 160)}`);
        must(place.state === 'holding', `${name}a: the notice is not holding while nothing was read again (${place.state})`);
        must(place.inView, `${name}a: the notice is outside the window (top ${place.top})`);
        const stillOn = held.filter((labels) => seen.enabledButtons.some((item) => labels.includes(item)));
        must(stillOn.length === 0, `${name}a: enabled while the result is unknown: ${stillOn.flat().join(' | ')}`);
        must(!seen.toasts.some((item) => /added|created|saved|applied|eklendi|oluşturuldu|kaydedildi|uygulandı/i.test(item)), `${name}a: an unknown result was reported as done`);
        await ctl({ clear: [rereadPath] });
        await waitFor(page, () => ['read', 'made', 'not-made'].includes(document.querySelector('[data-result-unknown]')?.dataset.resultUnknown), 15000);
        await settle(page);
        place = await noticePlace(page);
        const sent = await sentCount(key);
        const [method, path] = key.split(' ');
        const pageSent = page.changes.filter((item) => item === `${method} ${path}`).length;
        const ids = await sentIds(key);
        seen = await record(page, `${name}b-read-again`, { notice: place, sent, pageSent, identities: new Set(ids).size, expected: expect });
        must(place && place.state === expect, `${name}b: after the state was read again the notice is "${place?.state}", expected "${expect}"`);
        if (identified) {
            // One click: the request and one second asking, both under one identity.
            must(pageSent === 2, `${name}b: the page sent the identified change ${pageSent} times, expected the request and one second asking`);
            must(sent === 2, `${name}b: the identified change arrived at the server ${sent} times`);
            must(new Set(ids).size === 1 && /^[0-9a-f]{32}$/.test(ids[0] || ''), `${name}b: the arrivals did not carry one identity: ${ids.join(', ')}`);
        } else {
            must(pageSent === 1, `${name}b: the page sent the change ${pageSent} times`);
            must(sent === 1, `${name}b: the change arrived at the server ${sent} times`);
        }
        if (afterRead) await afterRead(page, seen);
        await drainLog();
        if (expect === 'made') {
            // Nothing is left to check once the state shows the change.
            must(!seen.enabledButtons.some((item) => CHECK_AGAIN.includes(item)), `${name}: "Check again" is offered for a change that is shown as saved`);
        } else {
            await clickByText(page, CHECK_AGAIN);
            await settle(page);
            const log = await drainLog();
            must(log.length > 0 && log.every((line) => line.includes(' GET ')), `${name}: "Check again" sent something other than a read: ${log.join(' ; ').slice(0, 200)}`);
            must(await sentCount(key) === (identified ? 2 : 1), `${name}: "Check again" sent the change`);
        }
        await clickByText(page, CLOSE);
        await pause(300);
        must(!(await noticePlace(page)), `${name}: the notice stayed after Close`);
        await record(page, `${name}c-closed`, { sent: await sentCount(key) });
    };

    // Whether what a row can do is within reach at this width: every action
    // inside `selector`, with the table scrolled to its start. A cell that is
    // off the right edge, or under another element, is not reachable.
    const reach = (page, selector) => page.evaluate((sel) => {
        const nodes = Array.from(document.querySelectorAll(sel)).filter((node) => node.getClientRects().length > 0);
        return nodes.map((node) => {
            node.scrollIntoView({ block: 'center', inline: 'nearest' });
            // Bringing it into view may have scrolled the table sideways; a
            // person who has not scrolled sees the table from its start.
            for (let at = node.parentElement; at; at = at.parentElement) at.scrollLeft = 0;
            const r = node.getBoundingClientRect();
            const top = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
            const frame = node.closest('.overflow-x-auto');
            // How many lines the label is drawn on: the distinct tops of the
            // boxes of its text. An icon has none.
            const range = document.createRange();
            range.selectNodeContents(node);
            const tops = new Set(Array.from(range.getClientRects()).filter((box) => box.width > 1 && box.height > 8).map((box) => Math.round(box.top)));
            const words = (node.innerText || '').trim();
            return {
                name: (node.getAttribute('aria-label') || node.title || node.innerText || '').trim().slice(0, 40),
                lines: words ? tops.size : 0,
                inWidth: r.x >= 0 && r.right <= window.innerWidth + 0.5,
                onTop: Boolean(top) && (top === node || node.contains(top)),
                tableScrollsSideways: frame ? frame.scrollWidth > frame.clientWidth + 1 : false,
                size: `${Math.round(r.width)}x${Math.round(r.height)}`,
            };
        });
    }, selector);
    const rowActions = async (page, name, selector) => {
        const actions = await reach(page, selector);
        must(actions.length > 0, `${name}: no row action matched ${selector}, so nothing was measured`);
        const seen = await record(page, name, { rowActions: actions, viewport: vp }, selector);
        const out = actions.filter((item) => !item.inWidth || !item.onTop);
        must(out.length === 0, `${name}: not reachable without scrolling sideways: ${out.map((item) => `${item.name} (${item.inWidth ? 'covered' : 'off the edge'})`).join(', ')}`);
        const broken = actions.filter((item) => item.lines > 1);
        must(broken.length === 0, `${name}: a row action's label breaks into ${broken.map((item) => `${item.lines} lines (${item.name}, ${item.size})`).join(', ')}`);
        must(!seen.pageOverflowsSideways, `${name}: the page itself scrolls sideways`);
        return actions;
    };
    const typeInto = async (page, selector, value) => {
        const element = await page.waitForSelector(selector, { timeout: 8000 });
        await element.evaluate((node) => { node.scrollIntoView({ block: 'center' }); node.focus(); if (node.select) node.select(); });
        await element.type(String(value));
    };

    // --- 90: DNS records and signing ------------------------------------------------
    scenarios.domaindns = async () => {
        // The zone itself slow, then known; failing, then Retry.
        await fresh();
        let page = await threeStates([T.dns], `${D}/dns/zone`, '90-dns-zone');
        await page.close();
        // The records under a known zone. The table's place is measured while
        // the signing state is read: the card above it keeps its height.
        await fresh();
        page = await threeStates([T.dns], `${D}/dns/records`, '90-dns-records');
        await page.close();
        await fresh();
        page = await threeStates([T.dns], `${D}/dnssec`, '90-dns-signing', { under: { selector: 'main table' } });
        await rowActions(page, '90-dns-row-actions', 'main td.row-actions button');
        await page.close();
        // Known negatives: a zone the server says does not exist, and a zone with no records.
        await fresh({ zone: null });
        page = await openTab([T.dns]);
        await settle(page);
        let seen = await record(page, '90-dns-zone-known-missing');
        must(seen.negativeText.some((item) => ['DNS zone is not active', 'DNS bölgesi aktif değil'].includes(item)), '90: a zone the server says is missing is not called missing');
        await page.close();
        await fresh({ records: [] });
        page = await openTab([T.dns]);
        await settle(page);
        seen = await record(page, '90-dns-records-known-empty');
        must(seen.negativeText.some((item) => ['No records yet', 'Henüz kayıt yok'].includes(item)), '90: a zone with no records does not say so');
        await page.close();
        // Adding a record: the connection drops, the record was made.
        await fresh();
        page = await openTab([T.dns]);
        await settle(page);
        await lostChange(page, '90-dns-add-lost-', {
            key: `POST ${D}/dns/records`, rereadPath: `${D}/dns/records`,
            held: [['Save record', 'Kaydı kaydet'], ['Republish DNS', 'DNS’i yeniden yayımla']],
            start: async (at) => {
                await clickByText(at, ['Add record', 'Kayıt ekle']);
                await typeInto(at, 'main input[placeholder="192.168.1.1"]', '192.0.2.99');
                await clickByText(at, ['Save record', 'Kaydı kaydet']);
            },
            // The records read again show it: the form that sent it is closed,
            // so the same record is not one press from being saved twice.
            expect: 'made',
            afterRead: async (at) => {
                must(!(await at.$('main input[placeholder="192.168.1.1"]')), '90: the form that sent the record is still open after the re-read showed the record');
            },
        });
        const rows = await page.evaluate(() => Array.from(document.querySelectorAll('main tbody tr')).filter((row) => row.innerText.includes('192.0.2.99')).length);
        must(rows === 1, `90: after the lost answer the re-read list shows the new record ${rows} times`);
        await page.close();
        await done();
    };

    // --- 91: hosting type and the live application ---------------------------------
    scenarios.domainhosting = async () => {
        await fresh();
        let page = await threeStates([T.hosting, T.type], `${D}/hosting`, '91-hosting');
        await page.close();
        // A Node.js project: the application's state slow, then known.
        const node = { hosting: { project_type: 'node', start_command: 'node server.js', runtime_version: '22.3.0', app_port: 3001 } };
        await fresh(node);
        page = await openTab([T.hosting, T.type], { [`${D}/app/status`]: { delay: 4000 }, [`${D}/app/logs`]: { delay: 4000 } });
        await waitFor(page, () => Array.from(document.querySelectorAll('main [role="status"][aria-label]')).length >= 1, 8000).catch(() => {});
        await pause(600);
        let seen = await record(page, '91-app-a-checking');
        must(seen.checkingLines.length > 0, '91-app-a: no checking line for the application');
        quietNegatives(seen, '91-app-a');
        must(!seen.enabledButtons.some((item) => ['Start', 'Stop', 'Restart', 'Başlat', 'Durdur', 'Yeniden başlat'].includes(item)), '91-app-a: start or stop is offered before the state is known');
        await ctl({ clear: [`${D}/app/status`, `${D}/app/logs`] });
        await waitFor(page, () => !document.querySelector('main [role="status"][aria-label]'), 15000);
        await settle(page);
        seen = await record(page, '91-app-b-known', {}, 'main h4');
        // The page polls every five seconds. Polls that fail: one calm notice,
        // the earlier state kept, no toast, nothing but reads.
        await drainLog();
        await ctl({ override: { [`${D}/app/status`]: { status: 502, body: { error: 'agent unavailable' } }, [`${D}/app/logs`]: { status: 502, body: { error: 'agent unavailable' } } } });
        await pause(12000);
        const log = await drainLog();
        seen = await record(page, '91-app-c-polls-failing', { polls: log.length });
        must(log.filter((line) => line.includes('/app/status')).length >= 2, `91-app-c: the polls did not run (${log.length} requests), so nothing was measured`);
        must(log.every((line) => line.includes(' GET ')), '91-app-c: a poll sent something other than a read');
        must(seen.toasts.length === 0, `91-app-c: a failed poll raised a toast: ${seen.toasts.join(' | ')}`);
        must(seen.notices.length === 1, `91-app-c: ${seen.notices.length} notices for failing polls, expected one`);
        quietNegatives(seen, '91-app-c');
        must(!seen.enabledButtons.some((item) => ['Start', 'Stop', 'Restart', 'Başlat', 'Durdur', 'Yeniden başlat'].includes(item)), '91-app-c: start or stop is offered on a state that could not be read again');
        await ctl({ clear: [`${D}/app/status`, `${D}/app/logs`] });
        await waitFor(page, () => !document.querySelector('main [role="alert"]'), 15000);
        await pause(300);
        await record(page, '91-app-d-polls-answer-again', {}, 'main h4');
        // Apply, with a gateway answering in the Panel's place.
        await lostChange(page, '91-hosting-apply-lost-', {
            key: `PUT ${D}/hosting`, rereadPath: `${D}/hosting`, loseAs: 'gateway', b4: node,
            held: [['Apply', 'Uygula']],
            start: (at) => clickByText(at, ['Apply', 'Uygula']),
            // Apply sent the settings the server already had, so the settings
            // read again are the ones that were sent.
            expect: 'made',
        });
        await page.close();
        await done();
    };

    // --- 92: PHP -------------------------------------------------------------------------
    scenarios.domainphp = async () => {
        await fresh();
        const page = await threeStates([T.hosting, T.php], `${D}/php`, '92-php', { mayBeNegativeWhenKnown: false });
        const pool = await page.evaluate(() => Object.fromEntries(Array.from(document.querySelectorAll('main form [name]')).map((node) => [node.getAttribute('name'), node.value])));
        must(Object.keys(pool).length >= 7, `92: the pool form has ${Object.keys(pool).length} fields, so its values were not read`);
        must(pool.pm === 'ondemand' && pool.pm_max_children === '9' && pool.user === 'site1', `92: the pool form does not hold the server's values: ${JSON.stringify(pool)}`);
        await record(page, '92-php-pool-holds-server-values', { pool }, 'main form');
        await page.close();
        await done();
    };

    // --- 93: general settings ------------------------------------------------------------
    scenarios.domaingeneral = async () => {
        await fresh();
        const page = await threeStates([T.hosting, T.general], `${D}/general`, '93-general', { mayBeNegativeWhenKnown: false });
        await rowActions(page, '93-general-alias-actions', 'main button[aria-label*="shop.example.net"], main button[aria-label*="a-rather-long-alias"]');
        // Removing an alias: the connection drops, the alias was removed.
        await lostChange(page, '93-general-alias-lost-', {
            key: `DELETE ${D}/aliases/shop.example.net`, rereadPath: `${D}/general`,
            held: [['Save changes', 'Değişiklikleri kaydet'], ['Add', 'Ekle']],
            start: (at) => at.evaluate(() => document.querySelector('main button[aria-label*="shop.example.net"]').click()),
        });
        const left = await page.evaluate(() => (document.querySelector('main').innerText.includes('shop.example.net')));
        must(!left, '93: the re-read list still shows the alias the server removed');
        await page.close();
        await done();
    };

    // --- 94: applications ----------------------------------------------------------------
    scenarios.domainapps = async () => {
        await fresh();
        let page = await threeStates([T.apps], '/api/v1/apps', '94-apps', { mayBeNegativeWhenKnown: false });
        await lostChange(page, '94-apps-install-lost-', {
            key: `POST ${D}/apps/install`, rereadPath: '/api/v1/apps',
            held: [['Install', 'Kur']],
            start: (at) => clickByText(at, ['Install', 'Kur']),
        });
        await page.close();
        await fresh({ apps: [] });
        page = await openTab([T.apps]);
        await settle(page);
        const seen = await record(page, '94-apps-known-empty');
        must(seen.negativeText.length === 1, '94: a catalogue the server sent empty does not say so');
        await page.close();
        await done();
    };

    // --- 95: mail authentication ---------------------------------------------------------
    scenarios.domainmailauth = async () => {
        await fresh();
        const page = await threeStates([T.mail, T.auth], `${D}/mail/auth`, '95-mailauth');
        await lostChange(page, '95-mailauth-publish-lost-', {
            key: `POST ${D}/mail/auth/apply`, rereadPath: `${D}/mail/auth`,
            held: [['Add to DNS', 'DNS\'e ekle']],
            start: (at) => clickByText(at, ['Add to DNS', 'DNS\'e ekle']),
        });
        await page.close();
        await done();
    };

    // --- 96: logs ------------------------------------------------------------------------
    scenarios.domainlogs = async () => {
        await fresh();
        let page = await threeStates([T.advanced, T.logs], `${D}/logs/access`, '96-logs', { mayBeNegativeWhenKnown: false });
        // Auto-refresh on, then every poll refused for two ticks.
        await page.evaluate(() => { const box = document.querySelector('main input[type="checkbox"]'); box.scrollIntoView({ block: 'center' }); box.click(); });
        await pause(300);
        await drainLog();
        await ctl({ override: { [`${D}/logs/access`]: { status: 502, body: { error: 'agent unavailable' } } } });
        await pause(12000);
        const log = await drainLog();
        let seen = await record(page, '96-logs-e-auto-refresh-refused', { polls: log.length });
        must(log.length >= 2, `96-e: auto-refresh did not poll (${log.length} requests), so nothing was measured`);
        must(log.every((line) => line.includes(' GET ')), '96-e: a poll sent something other than a read');
        must(seen.toasts.length === 0, `96-e: a refused poll raised a toast: ${seen.toasts.join(' | ')}`);
        must(seen.notices.length === 1, `96-e: ${seen.notices.length} notices for refused polls, expected one`);
        quietNegatives(seen, '96-e');
        must(await page.evaluate(() => document.querySelector('main pre')?.innerText.includes('wp-login.php') === true), '96-e: the lines already read left the screen');
        await ctl({ clear: [`${D}/logs/access`] });
        await waitFor(page, () => !document.querySelector('main [role="alert"]'), 15000);
        await pause(300);
        await record(page, '96-logs-f-auto-refresh-answers-again');
        await page.close();
        await fresh({ logs: [] });
        page = await openTab([T.advanced, T.logs]);
        await settle(page);
        seen = await record(page, '96-logs-known-empty');
        must(seen.negativeText.length >= 1, '96: a log the server sent empty does not say so');
        await page.close();
        await done();
    };

    // --- 97: backups ---------------------------------------------------------------------
    scenarios.domainbackups = async () => {
        await fresh();
        let page = await threeStates([T.advanced, T.backups], `${D}/backups`, '97-backups', { mayBeNegativeWhenKnown: false });
        await rowActions(page, '97-backups-row-actions', 'main section[aria-busy] .space-y-2 button');
        // A files backup: the connection drops and the backup was NOT made.
        await lostChange(page, '97-backups-create-lost-', {
            key: `POST ${D}/backups`, rereadPath: `${D}/backups`, loseApplied: false, identified: true,
            held: [['Create backup', 'Yedek oluştur']],
            start: (at) => at.evaluate(() => { const card = document.querySelector('main section button'); card.scrollIntoView({ block: 'center' }); card.click(); }),
        });
        const count = await page.evaluate(() => document.querySelectorAll('main section[aria-busy] .space-y-2 > div').length);
        must(count === 2, `97: after a lost answer for a backup that was not made, the re-read list has ${count} rows`);
        await page.close();
        // The linked databases could not be read: not "No linked databases".
        await fresh();
        page = await openTab([T.advanced, T.backups], { [`${D}/databases`]: { status: 502, body: { error: 'agent unavailable' } } });
        await page.waitForSelector('main [role="alert"]', { timeout: 10000 }).catch(() => {});
        await pause(400);
        let seen = await record(page, '97-backups-databases-could-not-check');
        must(seen.notices.length === 1, `97: ${seen.notices.length} notices when the linked databases could not be read`);
        quietNegatives(seen, '97-databases');
        await page.close();
        await fresh({ backups: [] }, { domainDatabases: { databases: [], available_types: [] } });
        page = await openTab([T.advanced, T.backups]);
        await settle(page);
        seen = await record(page, '97-backups-known-empty');
        must(seen.negativeText.length >= 1, '97: a domain with no backups does not say so');
        await page.close();
        await done();
    };

    // --- 98: the certificate card of the overview -------------------------------------
    scenarios.sslcard = async () => {
        const card = '#domain-overview-ssl-title';
        const cardBox = (page) => page.evaluate(() => { const node = document.getElementById('domain-overview-ssl-title')?.closest('section'); if (!node) return null; const r = node.getBoundingClientRect(); return { h: Math.round(r.height), text: node.innerText.trim() }; });
        await fresh();
        await ctl({ override: { [`${D}/ssl`]: { delay: 4500 } } });
        let page = await newPage();
        await page.goto(`${base}/domains/example.com`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector(card, { timeout: 15000 });
        await pause(600);
        const checking = await cardBox(page);
        must(checking, '98a: the certificate card is not on the overview, so nothing was measured');
        let seen = await record(page, '98-sslcard-a-checking', { card: checking });
        quietNegatives(seen, '98a');
        await ctl({ clear: [`${D}/ssl`] });
        await waitFor(page, () => /HTTPS/.test(document.getElementById('domain-overview-ssl-title')?.closest('section')?.innerText || ''), 15000);
        await settle(page);
        const known = await cardBox(page);
        await record(page, '98-sslcard-b-known-none', { card: known, grew: known.h - checking.h });
        await page.close();
        await ctl({ override: { [`${D}/ssl`]: { status: 502, body: { error: 'agent unavailable' } } } });
        page = await newPage();
        await page.goto(`${base}/domains/example.com`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector(card, { timeout: 15000 });
        await settle(page);
        const failed = await cardBox(page);
        seen = await record(page, '98-sslcard-c-could-not-check', { card: failed, grew: failed.h - checking.h });
        quietNegatives(seen, '98c');
        must(seen.enabledButtons.some((item) => RETRY.includes(item)), '98c: the card offers no Retry');
        await ctl({ clear: [`${D}/ssl`] });
        await drainLog();
        await clickByText(page, RETRY);
        await settle(page);
        const log = await drainLog();
        must(log.length > 0 && log.every((line) => line.includes(' GET ')), '98d: Retry sent something other than a read');
        await record(page, '98-sslcard-d-after-retry', { card: await cardBox(page) });
        await page.close();
        await done();
    };

    // --- 100: what a form does once the state was read again after a lost answer -------
    // (10 Oct 2026) The re-read shows the change: the form closes or empties
    // and the notice says it was saved. It does not: what was typed stays and
    // the notice says so. "Check again" asks the same question again, so a
    // change the server finished late is found. Nothing is sent a second time.
    scenarios.lostforms = async () => {
        const stateOf = (page) => page.evaluate(() => document.querySelector('[data-result-unknown]')?.dataset.resultUnknown ?? null);
        const valueOf = (page, selector) => page.evaluate((sel) => { const node = Array.from(document.querySelectorAll(sel)).find((item) => item.getClientRects().length > 0); return node ? node.value : null; }, selector);
        const noticeLook = (page) => page.evaluate(() => {
            const node = document.querySelector('[data-result-unknown]');
            if (!node) return null;
            const style = getComputedStyle(node);
            return { state: node.dataset.resultUnknown, role: node.getAttribute('role'), background: style.backgroundColor, border: style.borderTopColor, text: node.innerText.trim() };
        });

        // A DNS record the server did NOT make. The form keeps what was typed.
        await fresh();
        let page = await openTab([T.dns]);
        await settle(page);
        const recordField = 'main input[placeholder="192.168.1.1"]';
        await lostChange(page, '100-dns-add-not-shown-', {
            key: `POST ${D}/dns/records`, rereadPath: `${D}/dns/records`, loseApplied: false, expect: 'not-made',
            held: [['Save record', 'Kaydı kaydet'], ['Republish DNS', 'DNS’i yeniden yayımla']],
            start: async (at) => {
                await clickByText(at, ['Add record', 'Kayıt ekle']);
                await typeInto(at, recordField, '192.0.2.77');
                await clickByText(at, ['Save record', 'Kaydı kaydet']);
            },
            afterRead: async (at, seen) => {
                must(await valueOf(at, recordField) === '192.0.2.77', '100: what was typed is no longer in the form although the records read again do not show it');
                must(seen.enabledButtons.some((item) => ['Save record', 'Kaydı kaydet'].includes(item)), '100: the person cannot decide to save the record again');
                const look = await noticeLook(at);
                must(look.role === 'alert' && /245, 179, 1/.test(look.border), `100: the "not shown" notice is not on the attention surface: ${JSON.stringify(look).slice(0, 200)}`);
            },
        });
        await page.close();

        // The same, and then the server finishes late: "Check again" finds the
        // record, the form closes, the notice says it was saved.
        await fresh();
        page = await openTab([T.dns]);
        await settle(page);
        await ctl({ b4: { ...b4Defaults(), lose: `POST ${D}/dns/records`, loseApplied: false } });
        await clickByText(page, ['Add record', 'Kayıt ekle']);
        await typeInto(page, recordField, '192.0.2.78');
        await clickByText(page, ['Save record', 'Kaydı kaydet']);
        await waitFor(page, () => document.querySelector('[data-result-unknown]')?.dataset.resultUnknown === 'not-made', 15000);
        await settle(page);
        let seen = await record(page, '100-dns-add-late-a-not-shown-yet', { notice: await noticeLook(page) });
        const late = b4Defaults();
        await ctl({ b4: { ...late, records: [...late.records, { id: 107, name: 'example.com', type: 'A', content: '192.0.2.78', ttl: 3600, disabled: false }], sent: { [`POST ${D}/dns/records`]: 1 } } });
        await drainLog();
        await clickByText(page, CHECK_AGAIN);
        await waitFor(page, () => document.querySelector('[data-result-unknown]')?.dataset.resultUnknown === 'made', 15000);
        await settle(page);
        let log = await drainLog();
        let look = await noticeLook(page);
        seen = await record(page, '100-dns-add-late-b-shown-after-check-again', { notice: look, requests: log.length });
        must(log.length > 0 && log.every((line) => line.includes(' GET ')), `100: "Check again" sent something other than a read: ${log.join(' ; ').slice(0, 200)}`);
        must(!(await page.$(recordField)), '100: the form is still open after a later check showed the record');
        must(page.changes.filter((item) => item === `POST ${D}/dns/records`).length === 1, '100: the page sent the record a second time');
        must(look.role === 'status' && !/245, 179, 1/.test(`${look.background} ${look.border}`), `100: a change shown as saved is still drawn on the attention surface: ${JSON.stringify(look).slice(0, 200)}`);
        const rows = await page.evaluate(() => Array.from(document.querySelectorAll('main tbody tr')).filter((row) => row.innerText.includes('192.0.2.78')).length);
        must(rows === 1, `100: the list shows the record ${rows} times`);
        await clickByText(page, CLOSE);
        await pause(300);
        must(!(await stateOf(page)), '100: the notice stayed after Close');
        await page.close();

        // An alias: made (the field is emptied), and not made (it keeps the name).
        const aliasField = 'main input[placeholder="alias.example.com"], main input[placeholder="takma.example.com"]';
        for (const [applied, expect, name] of [[true, 'made', '100-alias-add-shown-'], [false, 'not-made', '100-alias-add-not-shown-']]) {
            await fresh();
            page = await openTab([T.hosting, T.general]);
            await settle(page);
            await lostChange(page, name, {
                key: `POST ${D}/aliases`, rereadPath: `${D}/general`, loseApplied: applied, expect,
                held: [['Save changes', 'Değişiklikleri kaydet'], ['Add', 'Ekle']],
                start: async (at) => {
                    await typeInto(at, aliasField, 'blog.example.net');
                    await clickByText(at, ['Add', 'Ekle']);
                },
                afterRead: async (at) => {
                    const typed = await valueOf(at, aliasField);
                    must(typed === (applied ? '' : 'blog.example.net'), `${name}: the alias field holds "${typed}" after a re-read that ${applied ? 'shows' : 'does not show'} the alias`);
                    const listed = await at.evaluate(() => Array.from(document.querySelectorAll('main .font-mono')).filter((node) => node.innerText.trim() === 'blog.example.net').length);
                    must(listed === (applied ? 1 : 0), `${name}: the alias is listed ${listed} times`);
                },
            });
            await page.close();
        }

        // Hosting type: Apply was not taken; the command that was typed is put
        // back over the settings that were read again.
        const node = { hosting: { project_type: 'node', start_command: 'node server.js', runtime_version: '22.3.0', app_port: 3001 } };
        await fresh(node);
        page = await openTab([T.hosting, T.type]);
        await settle(page);
        const command = 'main input[placeholder="node server.js"]';
        await lostChange(page, '100-hosting-apply-not-shown-', {
            key: `PUT ${D}/hosting`, rereadPath: `${D}/hosting`, loseApplied: false, expect: 'not-made', b4: node,
            held: [['Apply', 'Uygula']],
            start: async (at) => {
                await typeInto(at, command, 'node other.js');
                await clickByText(at, ['Apply', 'Uygula']);
            },
            afterRead: async (at) => {
                must(await valueOf(at, command) === 'node other.js', '100: the command that was typed was replaced by the server’s although the settings read again do not show it');
            },
        });
        await page.close();

        // The PHP pool: not taken; the form keeps the number that was entered.
        await fresh();
        page = await openTab([T.hosting, T.php]);
        await settle(page);
        await lostChange(page, '100-php-pool-not-shown-', {
            key: `POST ${D}/php/pool`, rereadPath: `${D}/php`, loseApplied: false, expect: 'not-made',
            held: [['Save pool', 'Havuzu kaydet']],
            start: async (at) => {
                await typeInto(at, 'main form input[name="pm_max_children"]', '12');
                await clickByText(at, ['Save pool', 'Havuzu kaydet']);
            },
            afterRead: async (at) => {
                must(await valueOf(at, 'main form input[name="pm_max_children"]') === '12', '100: the pool value that was entered was replaced by the server’s although the pool read again does not hold it');
            },
        });
        await page.close();
        await done();
    };

    // --- 101: the certificate line of the strip under a domain's name --------------------
    // (10 Oct 2026) Being checked, could not be checked with the read again
    // beside it, or what the server said. It used to say "checking status"
    // for as long as the read had not succeeded, also after it failed, and on
    // every tab that mounts neither the overview card nor the SSL/TLS tab.
    scenarios.sslfact = async () => {
        const CHECKING = ['Checking status', 'Durum kontrol ediliyor'];
        const strip = (page) => page.evaluate(() => {
            const fact = document.querySelector('main [data-ssl-fact]');
            const label = Array.from(document.querySelectorAll('main .text-fg-subtle')).find((node) => node.innerText.trim() === 'SSL');
            const row = label?.parentElement;
            const bar = row?.parentElement;
            const button = fact?.querySelector('button');
            const box = (node) => { if (!node) return null; const r = node.getBoundingClientRect(); return { x: Math.round(r.x), right: Math.round(r.right), h: Math.round(r.height) }; };
            return {
                state: fact ? fact.dataset.sslFact : null,
                text: row ? row.innerText.trim().replace(/\s+/g, ' ') : null,
                retry: button ? { text: button.innerText.trim(), disabled: button.disabled, box: box(button), inWidth: button.getBoundingClientRect().right <= window.innerWidth + 0.5 } : null,
                strip: box(bar),
            };
        });
        const openAt = async (path) => {
            const page = await newPage();
            await page.goto(`${base}${path}`, { waitUntil: 'domcontentloaded' });
            await page.waitForSelector('main h1', { timeout: 15000 });
            return page;
        };
        const show = 'main [data-ssl-fact], main h1';
        for (const [where, path] of [['overview', '/domains/example.com'], ['dns-tab', '/domains/example.com?tab=dns']]) {
            // Slow, then known.
            await fresh();
            await ctl({ override: { [`${D}/ssl`]: { delay: 4500 } } });
            let page = await openAt(path);
            await page.waitForSelector('main [data-ssl-fact="checking"]', { timeout: 8000 }).catch(() => {});
            await pause(300);
            let line = await strip(page);
            let seen = await record(page, `101-sslfact-${where}-a-checking`, { sslFact: line }, show);
            must(line.state === 'checking' && CHECKING.some((item) => line.text.includes(item)), `101-${where}-a: the strip does not say it is checking the certificate: ${JSON.stringify(line)}`);
            quietNegatives(seen, `101-${where}-a`);
            await ctl({ clear: [`${D}/ssl`] });
            await waitFor(page, () => !document.querySelector('main [data-ssl-fact]'), 15000);
            await settle(page);
            const known = await strip(page);
            await record(page, `101-sslfact-${where}-b-known`, { sslFact: known, stripGrew: known.strip && line.strip ? known.strip.h - line.strip.h : null }, 'main h1');
            must(known.state === null && /Off|Kapalı/.test(known.text || ''), `101-${where}-b: the strip does not say what the server said (no certificate): ${JSON.stringify(known)}`);
            await page.close();

            // Failing: not "checking", not "off"; the read again is beside it.
            await fresh();
            await ctl({ override: { [`${D}/ssl`]: { status: 502, body: { error: 'agent unavailable' } } } });
            page = await openAt(path);
            await page.waitForSelector('main [data-ssl-fact="unknown"]', { timeout: 10000 }).catch(() => {});
            await settle(page);
            line = await strip(page);
            seen = await record(page, `101-sslfact-${where}-c-could-not-check`, { sslFact: line }, show);
            must(line.state === 'unknown', `101-${where}-c: after the read failed the strip is "${line.state}" (${line.text}), expected "could not be checked"`);
            must(!CHECKING.some((item) => (line.text || '').includes(item)), `101-${where}-c: the strip still says it is checking after the read failed: ${line.text}`);
            must(!/\b(On|Off)\b|Açık|Kapalı/.test(line.text || ''), `101-${where}-c: the strip states a certificate state that was not read: ${line.text}`);
            must(line.retry && !line.retry.disabled && line.retry.inWidth, `101-${where}-c: the read again is not offered beside the words, or is off the screen: ${JSON.stringify(line.retry)}`);
            must(!seen.pageOverflowsSideways, `101-${where}-c: the page scrolls sideways`);
            await ctl({ clear: [`${D}/ssl`] });
            await drainLog();
            await page.evaluate(() => document.querySelector('main [data-ssl-fact] button').click());
            await waitFor(page, () => !document.querySelector('main [data-ssl-fact]'), 15000);
            await settle(page);
            const log = await drainLog();
            const after = await strip(page);
            await record(page, `101-sslfact-${where}-d-after-retry`, { sslFact: after, retryRequests: log.length }, 'main h1');
            must(log.length > 0 && log.every((item) => item.includes(' GET ')), `101-${where}-d: the strip's Retry sent something other than a read: ${log.join(' ; ').slice(0, 200)}`);
            must(/Off|Kapalı/.test(after.text || ''), `101-${where}-d: after Retry the strip does not say what the server said: ${after.text}`);
            await page.close();
        }
        await done();
    };

    // --- 99: row actions at the width of the screen ------------------------------------
    scenarios.rowactions = async () => {
        // The Domains list, the Databases page and the banned addresses.
        await fresh({}, { ...DB, managedScan: F2B_SCAN, b3: b3Defaults() });
        let page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('main table tbody tr', { timeout: 15000 });
        await settle(page);
        await rowActions(page, '99-rowactions-domains', 'main td.row-actions button, main td.row-actions a');
        await page.close();
        page = await newPage();
        await page.goto(`${base}/databases`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('main table tbody tr', { timeout: 15000 });
        await settle(page);
        await rowActions(page, '99-rowactions-databases', 'main td.row-actions button');
        await page.close();
        page = await newPage();
        await page.goto(`${base}/services/fail2ban`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('main h1, main h2', { timeout: 15000 });
        await settle(page);
        await clickExact(page, ['Banned IPs', 'Banlı IP\'ler', 'Banned', 'Banlı IPler', 'Yasaklı IP\'ler']);
        await page.waitForSelector('main table tbody tr', { timeout: 15000 });
        await settle(page);
        await rowActions(page, '99-rowactions-banned', 'main td.row-actions button');
        await page.close();
        await done();
    };
}
