// Scenarios of the third batch of "no negative UI unless known" (9 Oct 2026):
// the Fail2ban, Nginx, PHP, Dovecot and PowerDNS pages, the dashboard's
// attention list, the DNS settings, the DNS engine card's stalled change and
// the height of the configuration editor's card. Registered by run.mjs.
//
// For each read these scenarios make it slow, failing and known. The record
// says which negative sentences were on screen, which checking lines and
// notices were, which controls were enabled, and what changed place when an
// answer arrived. Looking at the screenshots is the inspection.
import { b3Defaults } from './mock-batch3.mjs';

const SERVICES = '/api/v1/managed-services';
const FIREWALL = '/api/v1/firewall';
const service = (id, name, category, extra = {}) => ({ id, name, description: `${name} on this server`, icon: '', category, kind: 'service', unit: id, versions: [], status: 'active (running)', is_installed: true, config_files: [], ...extra });
const profile = (id, name, services) => ({ id, name, description: `${name} for this server`, status: 'available', available: true, verified: false, latest_attempt_status: 'none', services });
const scan = (scannedAt = new Date().toISOString()) => ({
    scanned_at: scannedAt,
    dns_identity_ready: true,
    mail_hostname: { current: 'server1.example.com', hostname: '', source: '', current_usable: true, will_set_hostname: false },
    profiles: [profile('core-mail', 'Mail', ['postfix', 'dovecot']), profile('webmail', 'Webmail', ['roundcube']), profile('protected-mail', 'Protected mail', ['rspamd'])],
    services: [
        service('nginx', 'Nginx', 'web'),
        service('php-fpm', 'PHP-FPM', 'web', { versions: ['8.3', '8.2'] }),
        service('fail2ban', 'Fail2ban', 'security'),
        service('dovecot', 'Dovecot', 'mail'),
        service('postfix', 'Postfix', 'mail'),
        service('pdns', 'PowerDNS', 'dns', { config_files: [{ path: '/etc/powerdns/pdns.conf' }] }),
        service('postgresql', 'PostgreSQL', 'database', { config_files: [{ path: '/etc/postgresql/17/main/postgresql.conf' }, { path: '/etc/postgresql/17/main/pg_hba.conf' }] }),
        service('clamav', 'ClamAV', 'security'),
        { ...service('roundcube', 'Roundcube', 'mail'), kind: 'tool', is_installed: false, status: '' },
        { ...service('rspamd', 'Rspamd', 'mail'), is_installed: false, status: '' },
    ],
});
const DOMAINS = [{ id: 1, domain_name: 'example.com', status: 'active', project_type: 'php', php_version: '8.3', ssl_enabled: true, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 0 }];
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3', '8.2'], mail_server: true, database_servers: [], db_tools: [] };

// Sentences that claim something about the server. Each may be on screen only
// when the server has said so.
const NEGATIVE = [
    'No jails active', 'No banned IPs', 'No rate-limit zones defined', 'No extensions found', 'The firewall is off', 'Firewall is off', 'Nothing CelikPanel read needs action',
    'Aktif hapishane yok', 'Banlı IP yok', 'Tanımlı hız-limit bölgesi yok', 'Eklenti bulunamadı', 'Güvenlik duvarı kapalı', 'işlem gerektiren bir şey yok',
];
const RETRY = ['Retry', 'Tekrar dene'];

export default function register(scenarios, tools) {
    const { base, ctl, reset, drainLog, newPage, closePage, shot, into, clickByText, waitFor, pause, quiet } = tools;

    const fresh = async (b3 = {}, extra = {}) => {
        await reset();
        await ctl({ clearAll: true });
        await ctl({ domains: DOMAINS, capabilities: CAPABILITIES, managedScan: scan(), b3: { ...b3Defaults(), ...b3 }, ...extra });
        await drainLog();
    };
    const facts = (page) => page.evaluate((phrases) => {
        const scope = document.querySelector('main') || document.body;
        const text = scope.innerText || '';
        const label = (node) => (node.innerText || node.getAttribute('aria-label') || node.title || '').trim().replace(/\s+/g, ' ');
        const visible = (node) => node.getClientRects().length > 0;
        return {
            negativeText: phrases.filter((phrase) => text.includes(phrase)),
            checkingLines: Array.from(scope.querySelectorAll('[role="status"][aria-label]')).filter(visible).map((node) => node.getAttribute('aria-label')),
            notices: Array.from(scope.querySelectorAll('[role="alert"]')).filter(visible).map((node) => node.innerText.trim()),
            enabledButtons: Array.from(scope.querySelectorAll('button:not(:disabled)')).filter(visible).map(label).filter(Boolean),
            disabledButtons: Array.from(scope.querySelectorAll('button:disabled')).filter(visible).map(label).filter(Boolean),
            enabledSwitches: Array.from(scope.querySelectorAll('input[type="checkbox"]:not(:disabled)')).filter(visible).length,
            disabledSwitches: Array.from(scope.querySelectorAll('input[type="checkbox"]:disabled')).filter(visible).length,
        };
    }, NEGATIVE);
    // Where the first element matching `selector` whose text contains one of
    // `texts` stands on the page (not in the window), so two states can be
    // compared whatever was scrolled: its place in the window plus what every
    // ancestor, up to the root, has scrolled. (Until 9 Oct 2026 this added the
    // scroll of one element chosen by its class name, which on a phone is not
    // the one that scrolls: a measurement taken after a scroll was off by it.)
    const placeOf = (page, selector, texts = null) => page.evaluate((sel, list) => {
        const node = Array.from(document.querySelectorAll(sel)).find((item) => item.getClientRects().length > 0 && (!list || list.some((text) => (item.innerText || '').includes(text))));
        if (!node) return null;
        const r = node.getBoundingClientRect();
        // The root element's own scroll is the window's.
        let scrolled = 0;
        for (let at = node.parentElement; at; at = at.parentElement) scrolled += at.scrollTop;
        return { y: Math.round(r.y + scrolled), h: Math.round(r.height), window: window.innerHeight };
    }, selector, texts);
    const moved = (before, after) => (before && after ? after.y - before.y : null);
    const open = async (path, settled = true) => {
        const page = await newPage();
        page.on('dialog', (dialog) => { void dialog.accept(); });
        await page.goto(`${base}${path}`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('main h1, main h2, main h3');
        if (settled) { await quiet(page); await pause(300); }
        return page;
    };
    const record = async (page, name, more = {}) => shot(page, name, { ...(await facts(page)), ...more });
    // A tab of a page, by its label without the count beside it. A service
    // page draws its tabs once its header has read the component's record.
    const tab = async (page, texts) => {
        const find = () => page.evaluateHandle((list) => Array.from(document.querySelectorAll('main button, [role="tab"]'))
            .find((node) => node.getClientRects().length > 0 && list.includes((node.innerText || '').trim().replace(/\s*[\d…–]+$/, ''))) || null, texts);
        let element = (await find()).asElement();
        for (let waited = 0; !element && waited < 10000; waited += 250) { await pause(250); element = (await find()).asElement(); }
        if (!element) throw new Error(`no tab labelled ${texts.join(' | ')}`);
        await element.click();
    };
    // A checking line, or (the Dovecot figures) "…" where a figure will stand.
    const checkingShown = (page) => waitFor(page, () => Boolean(document.querySelector('main [role="status"][aria-label]')) || (document.querySelector('main')?.innerText || '').includes('…'), 15000).catch(() => {});
    const settle = async (page) => { await quiet(page); await pause(400); };
    // One read slow, then known. Where something stands under the changing
    // part (`under`), it must not move; a page with nothing under it passes
    // null and records no measurement, so that "0" is never a figure nobody took.
    const slowThenKnown = async (path, readPath, names, under, tabTexts = null) => {
        await ctl({ override: { [readPath]: { delay: 4500 } } });
        const page = await open(path, false);
        if (tabTexts) await tab(page, tabTexts);
        await checkingShown(page);
        await pause(500);
        const before = under ? await placeOf(page, under.selector, under.texts) : null;
        await record(page, names[0], under ? { under: before } : {});
        await ctl({ clear: [readPath] });
        await waitFor(page, () => !document.querySelector('main [role="status"][aria-label]') && !(document.querySelector('main')?.innerText || '').includes('…'), 15000);
        await settle(page);
        const after = under ? await placeOf(page, under.selector, under.texts) : null;
        if (under && (!before || !after)) throw new Error(`${names[1]}: nothing matched ${under.selector} ${JSON.stringify(under.texts)}, so nothing was measured`);
        await record(page, names[1], under ? { under: after, moved: moved(before, after) } : {});
        return page;
    };
    const failingThenRetry = async (path, readPath, names, tabTexts = null) => {
        await ctl({ override: { [readPath]: { status: 502, body: { error: 'agent unavailable' } } } });
        const page = await open(path, false);
        if (tabTexts) await tab(page, tabTexts);
        await page.waitForSelector('main [role="alert"]', { timeout: 15000 }).catch(() => {});
        await pause(400);
        await record(page, names[0]);
        await ctl({ clear: [readPath] });
        await clickByText(page, RETRY);
        await waitFor(page, () => !document.querySelector('main [role="alert"]'), 15000).catch(() => {});
        await settle(page);
        await record(page, names[1]);
        return page;
    };
    // The first panel under the Dovecot figures (its heading is an h2).
    const OVERVIEW = { selector: 'main section h2', texts: ['Overview', 'Genel bakış'] };

    // --- 70-74: the service pages -------------------------------------------------
    scenarios.servicepages = async () => {
        // Fail2ban: the jails slow then known, the banned addresses failing
        // then read again, a known empty list, and a ban lifted whose re-read
        // fails. Nothing stands under the tab content of the Fail2ban, Nginx
        // and PHP pages, so there is nothing whose place could be measured.
        await fresh();
        let page = await slowThenKnown('/services/fail2ban', '/api/v1/fail2ban/jails', ['70a-fail2ban-jails-checking', '70b-fail2ban-jails-known'], null);
        await closePage(page);
        await fresh();
        page = await failingThenRetry('/services/fail2ban', '/api/v1/fail2ban/banned', ['70c-fail2ban-banned-could-not-read', '70d-fail2ban-banned-after-retry'], ['Banned IPs', 'Banlı IP’ler', "Banlı IP'ler", 'Banned', 'Banlılar']);
        // The POST is the first request to this address and passes; the read
        // that follows it fails.
        await ctl({ override: { '/api/v1/fail2ban/banned': { status: 502, body: { error: 'agent unavailable' }, after: 1 } } });
        await clickByText(page, ['Unban', 'Banı kaldır', 'Yasağı kaldır']);
        await page.waitForSelector('main [role="alert"]', { timeout: 15000 }).catch(() => {});
        await settle(page);
        await record(page, '70e-fail2ban-unbanned-then-could-not-read-again');
        await closePage(page);
        await fresh({ banned: [], jails: [] });
        page = await open('/services/fail2ban');
        await record(page, '70f-fail2ban-jails-known-none');
        await tab(page, ['Banned IPs', 'Banlı IP’ler', "Banlı IP'ler", 'Banned', 'Banlılar']);
        await settle(page);
        await record(page, '70g-fail2ban-banned-known-none');
        await closePage(page);

        // Nginx.
        await fresh();
        page = await slowThenKnown('/services/nginx', '/api/v1/nginx/global', ['71a-nginx-global-checking', '71b-nginx-global-known'], null);
        await closePage(page);
        await fresh();
        page = await failingThenRetry('/services/nginx', '/api/v1/nginx/ssl', ['71c-nginx-tls-could-not-read', '71d-nginx-tls-after-retry'], ['SSL / TLS', 'SSL/TLS', 'TLS', 'SSL']);
        await closePage(page);
        await fresh({ rateLimits: [] });
        page = await open('/services/nginx');
        await tab(page, ['Rate limits', 'Rate Limits', 'Hız limitleri', 'Hız Limitleri', 'Hız sınırları']);
        await settle(page);
        await record(page, '71e-nginx-rate-limits-known-none');
        await closePage(page);

        // PHP: the extensions and php.ini.
        await fresh();
        page = await slowThenKnown('/services/php-fpm', '/api/v1/php/extensions', ['72a-php-extensions-checking', '72b-php-extensions-known'], null);
        await closePage(page);
        await fresh();
        page = await failingThenRetry('/services/php-fpm', '/api/v1/php/extensions', ['72c-php-extensions-could-not-read', '72d-php-extensions-after-retry']);
        // A switch refused by the server shows what the server says again.
        await ctl({ b3: { ...b3Defaults(), toggle: 'refused' } });
        await page.evaluate(() => document.querySelector('main input[type="checkbox"]')?.click());
        await settle(page);
        await record(page, '72e-php-extension-switch-refused');
        await closePage(page);
        await fresh();
        page = await failingThenRetry('/services/php-fpm', '/api/v1/php/extended-config', ['72f-php-ini-could-not-read', '72g-php-ini-after-retry'], ['Configuration', 'Yapılandırma', 'php.ini', 'Config']);
        await closePage(page);

        // Dovecot: two figures.
        await fresh();
        page = await slowThenKnown('/services/dovecot', '/api/v1/dovecot/stats', ['73a-dovecot-figures-checking', '73b-dovecot-figures-known'], OVERVIEW);
        await closePage(page);
        await fresh();
        page = await failingThenRetry('/services/dovecot', '/api/v1/dovecot/stats', ['73c-dovecot-figures-could-not-read', '73d-dovecot-figures-after-retry']);
        await closePage(page);

        // PowerDNS: the file list comes from the shared component records.
        await fresh();
        page = await open('/services/pdns');
        await record(page, '74a-powerdns-files-known');
        await closePage(page);
    };

    // --- 80-82: the dashboard's attention list ---------------------------------------
    scenarios.attention = async () => {
        const HOSTING = { selector: 'main h2, main h3', texts: ['Hosting', 'Barındırma'] };
        // The section is below the fold on a phone: it is brought into the
        // window before each screenshot. Places are measured on the page, not
        // in the window, so this does not change them.
        const seen = async (page, name, more = {}) => { await into(page, 'main section[aria-busy]'); return record(page, name, more); };
        // The slowest read on its way: the section is on the page with its
        // checking line; when it answers nothing under it moves.
        await fresh();
        await ctl({ override: { [FIREWALL]: { delay: 4500 } } });
        let page = await open('/', false);
        await page.waitForSelector('main section[aria-busy="true"]', { timeout: 15000 }).catch(() => {});
        await pause(900);
        let before = await placeOf(page, HOSTING.selector, HOSTING.texts);
        await seen(page, '80a-attention-checking', { under: before });
        await ctl({ clear: [FIREWALL] });
        await waitFor(page, () => !document.querySelector('main section[aria-busy="true"]'), 15000);
        await settle(page);
        let after = await placeOf(page, HOSTING.selector, HOSTING.texts);
        await seen(page, '80b-attention-known-nothing', { under: after, moved: moved(before, after) });
        await closePage(page);

        // One item arrives late: the section does not grow.
        await fresh({ firewall: { ...b3Defaults().firewall, enabled: false, persistence_state: 'disabled' } });
        await ctl({ override: { [FIREWALL]: { delay: 4500 } } });
        page = await open('/', false);
        await page.waitForSelector('main section[aria-busy="true"]', { timeout: 15000 }).catch(() => {});
        await pause(900);
        before = await placeOf(page, HOSTING.selector, HOSTING.texts);
        await ctl({ clear: [FIREWALL] });
        await waitFor(page, () => !document.querySelector('main section[aria-busy="true"]'), 15000);
        await settle(page);
        after = await placeOf(page, HOSTING.selector, HOSTING.texts);
        await seen(page, '81a-attention-firewall-known-off', { under: after, moved: moved(before, after) });
        await clickByText(page, ['Turn on', 'Turn on the firewall', 'Aç', 'Güvenlik duvarını aç']);
        await pause(900);
        await shot(page, '81b-firewall-confirmation');
        await closePage(page);

        // An item is already listed while another read is still on its way:
        // that is said beside the count, and no row comes and goes in the list.
        await fresh({ firewall: { ...b3Defaults().firewall, enabled: false, persistence_state: 'disabled' } });
        await ctl({ override: { '/api/v1/dashboard': { delay: 4500 } } });
        page = await open('/', false);
        await waitFor(page, () => /Firewall is off|Güvenlik duvarı kapalı/.test(document.querySelector('main')?.innerText || ''), 15000);
        await pause(600);
        before = await placeOf(page, HOSTING.selector, HOSTING.texts);
        await seen(page, '81c-attention-item-listed-another-read-on-its-way', { under: before, sectionBusy: await page.evaluate(() => Boolean(document.querySelector('main section[aria-busy="true"]'))) });
        await ctl({ clear: ['/api/v1/dashboard'] });
        await waitFor(page, () => !document.querySelector('main section[aria-busy="true"]'), 15000);
        await settle(page);
        after = await placeOf(page, HOSTING.selector, HOSTING.texts);
        await seen(page, '81d-attention-item-listed-all-read', { under: after, moved: moved(before, after) });
        await closePage(page);

        // The Agent's own error sent with a 200: not "off", and nothing to turn on.
        await fresh();
        await ctl({ override: { [FIREWALL]: { status: 200, body: { error: 'agent unavailable' } } } });
        page = await open('/');
        await seen(page, '82a-attention-firewall-answer-without-a-state');
        await closePage(page);
        await fresh();
        await ctl({ override: { [FIREWALL]: { status: 502, body: { error: 'agent unavailable' } } } });
        page = await open('/');
        await seen(page, '82b-attention-firewall-could-not-read');
        await ctl({ clear: [FIREWALL] });
        await clickByText(page, RETRY);
        await waitFor(page, () => !document.querySelector('main [role="alert"]'), 15000).catch(() => {});
        await settle(page);
        await seen(page, '82c-attention-after-retry');
        await closePage(page);

        // The component records could not be read.
        await fresh();
        await ctl({ override: { [SERVICES]: { status: 502, body: { error: 'agent unavailable' } } } });
        page = await open('/');
        await seen(page, '82d-attention-components-could-not-read');
        await closePage(page);

        // Components last checked a day ago: the calm line does not speak for
        // them, and it takes the place the checking line had.
        await fresh({}, { managedScan: scan(new Date(Date.now() - 26 * 3600_000).toISOString()) });
        await ctl({ override: { [FIREWALL]: { delay: 4500 } } });
        page = await open('/', false);
        await page.waitForSelector('main section[aria-busy="true"]', { timeout: 15000 }).catch(() => {});
        await pause(900);
        before = await placeOf(page, HOSTING.selector, HOSTING.texts);
        await ctl({ clear: [FIREWALL] });
        await waitFor(page, () => !document.querySelector('main section[aria-busy="true"]'), 15000);
        await settle(page);
        after = await placeOf(page, HOSTING.selector, HOSTING.texts);
        await seen(page, '82e-attention-known-nothing-components-not-current', { under: after, moved: moved(before, after) });
        await closePage(page);
    };

    // --- 83: the DNS settings ---------------------------------------------------------
    const dnsSection = async (settled = true) => {
        const page = await open('/settings', false);
        await page.waitForSelector('#settings-dns-tab', { timeout: 15000 });
        await page.click('#settings-dns-tab');
        if (settled) { await settle(page); }
        return page;
    };
    scenarios.dnssettings = async () => {
        await fresh();
        await ctl({ override: { '/api/v1/settings/dns-cluster': { delay: 4500 } } });
        let page = await dnsSection(false);
        await pause(1200);
        await record(page, '83a-dns-settings-checking');
        await ctl({ clear: ['/api/v1/settings/dns-cluster'] });
        await settle(page);
        await pause(1500);
        await record(page, '83b-dns-settings-known');
        await closePage(page);

        await fresh();
        await ctl({ override: { '/api/v1/settings/nameservers': { status: 502, body: { error: 'agent unavailable' } } } });
        page = await dnsSection();
        await record(page, '83c-dns-settings-could-not-read');
        await ctl({ clear: ['/api/v1/settings/nameservers'] });
        await page.evaluate((texts) => Array.from(document.querySelectorAll('#settings-dns-panel [role="alert"] button')).find((node) => texts.includes((node.innerText || '').trim()))?.click(), RETRY);
        await settle(page);
        await pause(1500);
        await record(page, '83d-dns-settings-after-retry');
        await closePage(page);
    };

    // --- 84: a DNS engine change that has recorded nothing for four minutes -----------
    // Polling only reads. The reconcile request is offered in the lock and is
    // sent by the person at the screen; the mock counts every one that arrives.
    scenarios.dnsreconcile = async () => {
        const lock = (page) => page.evaluate(() => {
            const node = document.querySelector('[aria-labelledby="component-operation-title"]');
            return node ? { text: node.innerText.trim(), buttons: Array.from(node.querySelectorAll('button')).map((button) => `${button.innerText.trim()}${button.disabled ? ' (disabled)' : ''}`) } : null;
        });
        const reconciles = async () => (await drainLog()).filter((line) => line.includes('POST /api/v1/dns/engine/reconcile')).length;
        await fresh({ dnsChange: 'stalled', reconcile: 'unchanged' });
        let page = await dnsSection(false);
        await waitFor(page, () => (document.querySelector('[aria-labelledby="component-operation-title"]')?.innerText || '').match(/Check now|Şimdi kontrol et/), 30000);
        // Two more slow polls (15 s each) with nobody touching the page.
        await pause(34000);
        const unattended = await reconciles();
        await shot(page, '84a-dns-change-stalled-check-offered', { lock: await lock(page), reconcileRequestsWhileNobodyActed: unattended });
        await clickByText(page, ['Check now', 'Şimdi kontrol et']);
        await pause(2500);
        await shot(page, '84b-dns-change-checked-not-finished', { lock: await lock(page), reconcileRequestsAfterOneClick: await reconciles() });
        await ctl({ b3: { ...b3Defaults(), dnsChange: 'stalled', reconcile: 'refused' } });
        await clickByText(page, ['Check now', 'Şimdi kontrol et']);
        await pause(2500);
        await shot(page, '84c-dns-change-check-refused', { lock: await lock(page), reconcileRequestsAfterOneClick: await reconciles() });
        await ctl({ b3: { ...b3Defaults(), dnsChange: 'stalled', reconcile: 'finishes' } });
        await clickByText(page, ['Check now', 'Şimdi kontrol et']);
        await waitFor(page, () => !document.querySelector('[aria-labelledby="component-operation-title"]'), 20000).catch(() => {});
        await pause(600);
        await shot(page, '84d-dns-change-checked-finished', { lock: await lock(page), reconcileRequestsAfterOneClick: await reconciles() });
        await closePage(page);

        // Past the safety limit (a change accepted forty minutes ago): the
        // loop has stopped and the lock is released. Nothing reads or sends by
        // itself any more, and the card still offers the owner's check.
        const NOTICE = '[data-testid="dns-engine-deadline-check"]';
        const notice = (page) => page.evaluate((selector) => {
            const node = document.querySelector(selector);
            return {
                lockShown: Boolean(document.querySelector('[aria-labelledby="component-operation-title"]')),
                notice: node ? { text: node.innerText.trim(), buttons: Array.from(node.querySelectorAll('button')).map((button) => `${button.innerText.trim()}${button.disabled ? ' (disabled)' : ''}`) } : null,
                saysTrackingContinues: /continues every 15 seconds|15 saniyede bir devam/.test(document.querySelector('main')?.innerText || ''),
            };
        }, NOTICE);
        await fresh({ dnsChange: 'overdue', reconcile: 'unchanged' });
        page = await dnsSection(false);
        await page.waitForSelector(NOTICE, { timeout: 30000 });
        await drainLog();
        // More than one slow poll with nobody touching the page.
        await pause(20000);
        const quietLog = await drainLog();
        await into(page, NOTICE);
        await shot(page, '84e-dns-change-past-the-safety-limit-check-offered', {
            ...(await notice(page)),
            reconcileRequestsWhileNobodyActed: quietLog.filter((line) => line.includes('POST /api/v1/dns/engine/reconcile')).length,
            stateReadsWhileNobodyActed: quietLog.filter((line) => line.includes('GET /api/v1/dns/engine')).length,
        });
        await page.click(`${NOTICE} button`);
        await pause(2500);
        await into(page, NOTICE);
        await shot(page, '84f-dns-change-past-the-safety-limit-checked-not-finished', { ...(await notice(page)), reconcileRequestsAfterOneClick: await reconciles() });
        await ctl({ b3: { ...b3Defaults(), dnsChange: 'overdue', reconcile: 'finishes' } });
        await page.click(`${NOTICE} button`);
        await waitFor(page, (selector) => !document.querySelector(selector), 20000, NOTICE).catch(() => {});
        await pause(600);
        await shot(page, '84g-dns-change-past-the-safety-limit-checked-finished', { ...(await notice(page)), reconcileRequestsAfterOneClick: await reconciles() });
        await closePage(page);
    };

    // --- 85: the card of a configuration editor keeps its place -----------------------
    scenarios.editorheight = async () => {
        const RAW = { selector: 'main h4', texts: ['raw files', 'RAW FILES', 'Raw files', 'ham dosya', 'Ham dosya', 'HAM DOSYA', 'Gelişmiş', 'Advanced'] };
        await fresh();
        await ctl({ override: { '/api/v1/config': { delay: 4500 } } });
        const page = await open('/services/postgresql', false);
        await checkingShown(page);
        await pause(600);
        const before = await placeOf(page, RAW.selector, RAW.texts);
        await record(page, '85a-editor-card-while-the-file-is-read', { under: before, underIsBelowTheFold: before ? before.y >= before.window : null });
        await ctl({ clear: ['/api/v1/config'] });
        await waitFor(page, () => !document.querySelector('main [role="status"][aria-label]'), 15000);
        await settle(page);
        const after = await placeOf(page, RAW.selector, RAW.texts);
        await record(page, '85b-editor-card-with-the-file', { under: after, underIsBelowTheFold: after ? after.y >= after.window : null });
        await closePage(page);
    };
}
