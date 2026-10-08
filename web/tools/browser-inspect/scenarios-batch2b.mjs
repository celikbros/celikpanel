// Scenarios of the second batch of "no negative UI unless known" (9 Oct 2026):
// the database configuration editors, a domain's mail screens, the mail queue
// and policy, and a domain's scheduled tasks. Registered by run.mjs.
//
// For each read these scenarios make it slow, failing and known, and for each
// save they make it accepted, refused because the file changed, refused by the
// service's own program, and failed at the reload. The record says which
// negative sentences were on screen, which checking lines and notices were,
// which fields and saving controls existed and were enabled, and what moved
// when an answer arrived. Looking at the screenshots is the inspection.
import { b2Defaults } from './mock-batch2b.mjs';

const SERVICES = '/api/v1/managed-services';
const CONFIG = '/api/v1/config';
const SETUP = '/api/v1/domains/1/mail/setup';
const CATCH_ALL = '/api/v1/domains/1/mail/catch-all';
const ACCOUNTS = '/api/v1/domains/1/mail/accounts';
const FORWARDINGS = '/api/v1/domains/1/mail/forwardings';
const QUOTA = '/api/v1/domains/1/mail/quota';
const QUEUE = '/api/v1/postfix/queue';
const CRON = '/api/v1/domains/1/cron';
const PATHS = [SERVICES, CONFIG, SETUP, CATCH_ALL, ACCOUNTS, FORWARDINGS, QUOTA, QUEUE, CRON, '/api/v1/mail/policy', '/api/v1/domains', '/api/v1/hosting/capabilities'];

// A record as the scan writes it for a running service: its unit is named, so
// the page's header offers start, stop and restart as on a real server.
const service = (id, name, files) => ({ id, name, description: '', icon: '', category: id === 'postfix' ? 'mail' : 'database', kind: 'service', unit: id, versions: [], status: 'active (running)', is_installed: true, config_files: files.map((path) => ({ path })) });
// The whole contract of the cached scan: the component pages refuse an answer
// with a part missing (it is "not checked yet" to them, not "not installed").
const profile = (id, name) => ({ id, name, description: `${name} for this server`, status: 'complete', available: true, verified: true, latest_attempt_status: 'succeeded', services: ['postfix'] });
const SCAN = {
    scanned_at: '2026-10-09T09:00:00Z',
    dns_identity_ready: true,
    mail_hostname: { current: 'server1.example.com', hostname: 'mail.example.com', source: 'saved', current_usable: true, will_set_hostname: false },
    profiles: [profile('core-mail', 'Mail'), profile('webmail', 'Webmail'), profile('protected-mail', 'Protected mail')],
    services: [
        service('postgresql', 'PostgreSQL', ['/etc/postgresql/17/main/postgresql.conf', '/etc/postgresql/17/main/pg_hba.conf']),
        service('mariadb', 'MariaDB', ['/etc/mysql/mariadb.cnf', '/etc/mysql/mariadb.conf.d/50-server.cnf']),
        service('postfix', 'Postfix', ['/etc/postfix/main.cf']),
    ],
};
const DOMAINS = [{ id: 1, domain_name: 'example.com', status: 'active', project_type: 'php', php_version: '8.3', ssl_enabled: true, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 0 }];
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3'], mail_server: true, database_servers: ['mariadb'], db_tools: [] };

// Sentences that claim something about the server. Each may be on screen only
// when the server has said so.
const NEGATIVE = [
    'not found.', 'holds no access rule', 'found no setting', 'is not available on this server', 'No email accounts yet', 'No forwarders yet',
    'The mail queue is empty', 'No catch-all address is set', 'No scheduled tasks', 'Nothing changed yet', '0 items total',
    'bulunamadı.', 'hiç erişim kuralı yok', 'bir ayar bulamadı', 'bu sunucuda kullanılamıyor', 'Henüz e-posta hesabı yok', 'Henüz yönlendirici yok',
    'Mail kuyruğu boş', 'catch-all adresi ayarlı değil', 'Zamanlanmış görev yok', 'Henüz bir şey değişmedi', 'Toplam 0',
];
const SAVE = ['Save changes', 'Değişiklikleri kaydet'];
const RETRY = ['Retry', 'Tekrar dene'];

export default function register(scenarios, tools) {
    const { base, ctl, reset, drainLog, newPage, closePage, shot, clickByText, waitFor, pause, quiet } = tools;

    // b2 is sent whole; `withB2` starts from the defaults.
    const withB2 = (changes = {}) => ({ ...b2Defaults(), ...changes });
    const fresh = async (b2 = {}, extra = {}) => {
        await reset();
        // No override of an earlier scenario is left, whichever batch it was in.
        await ctl({ clear: PATHS, clearAll: true });
        await ctl({ domains: DOMAINS, capabilities: CAPABILITIES, b2: withB2(b2), managedScan: SCAN, ...extra });
        await drainLog();
    };
    // The page's own facts: what it claims, what it offers.
    const facts = (page) => page.evaluate((phrases) => {
        const scope = document.querySelector('main') || document.body;
        const text = scope.innerText || '';
        const label = (node) => (node.innerText || node.getAttribute('aria-label') || node.title || '').trim().replace(/\s+/g, ' ');
        const visible = (node) => node.getClientRects().length > 0;
        const fields = Array.from(scope.querySelectorAll('input:not([type="search"]), textarea, select')).filter(visible);
        return {
            negativeText: phrases.filter((phrase) => text.includes(phrase)),
            checkingLines: Array.from(scope.querySelectorAll('[role="status"][aria-label]')).filter(visible).map((node) => node.getAttribute('aria-label')),
            notices: Array.from(scope.querySelectorAll('[role="alert"]')).filter(visible).map((node) => node.innerText.trim()),
            statusNotes: Array.from(scope.querySelectorAll('div[role="status"]:not([aria-label])')).filter(visible).map((node) => node.innerText.trim()),
            fields: fields.length,
            enabledFields: fields.filter((node) => !node.disabled).length,
            enabledButtons: Array.from(scope.querySelectorAll('button:not(:disabled)')).filter(visible).map(label).filter(Boolean),
            disabledButtons: Array.from(scope.querySelectorAll('button:disabled')).filter(visible).map(label).filter(Boolean),
            invalidFields: Array.from(scope.querySelectorAll('[aria-invalid="true"]')).map((node) => `${node.getAttribute('aria-label') || node.type}: ${document.getElementById(node.getAttribute('aria-describedby'))?.innerText || ''}`),
            smallTargets: Array.from(scope.querySelectorAll('button, a, select, input[type="checkbox"]')).filter(visible)
                .map((node) => ({ node, r: (node.closest('label') || node).getBoundingClientRect() }))
                .filter(({ r }) => r.height < 24 || r.width < 24).map(({ node, r }) => `${label(node).slice(0, 30)} ${Math.round(r.width)}x${Math.round(r.height)}`).slice(0, 8),
        };
    }, NEGATIVE);
    const rectOf = (page, selector) => page.evaluate((sel) => { const node = document.querySelector(sel); if (!node) return null; const r = node.getBoundingClientRect(); const scroller = document.querySelector("main")?.closest("[class*=overflow-y]") || document.scrollingElement; return { y: Math.round(r.y + (scroller?.scrollTop || 0)), h: Math.round(r.height) }; }, selector);
    const open = async (path, settled = true) => {
        const page = await newPage();
        await page.goto(`${base}${path}`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('main h1, main h2, main h3');
        if (settled) await quiet(page);
        return page;
    };
    const record = async (page, name, more = {}) => shot(page, name, { ...(await facts(page)), ...more });
    // For the states added on 10 Oct 2026: a state that should show something
    // and shows nothing fails the scenario, and its error is in the report.
    const must = (condition, message) => { if (!condition) throw new Error(message); };
    const toasts = (page) => page.evaluate(() => Array.from(document.querySelectorAll('.fixed.top-4.right-4 > *')).map((node) => node.innerText.trim()));
    const measured = async (page, name, more = {}) => {
        const seen = { ...(await facts(page)), toasts: await toasts(page), ...more };
        await shot(page, name, seen);
        return seen;
    };
    // A component's page opened from the Components list with Manage. The page
    // then reads the component records twice: its header for its own record,
    // and one read shared by everything under the header. `shared` is what the
    // mock does with that second one. The scan is a minute old, so the list
    // starts no check of its own.
    const fromList = async (name, shared, listState = '') => {
        await fresh({}, { managedScan: { ...SCAN, scanned_at: new Date(Date.now() - 60000).toISOString() } });
        const page = await open('/services');
        await page.waitForSelector('main li');
        if (listState) await record(page, listState);
        const found = await page.evaluateHandle((component, texts) => Array.from(document.querySelectorAll('main li'))
            .filter((row) => (row.innerText || '').includes(component))
            .flatMap((row) => Array.from(row.querySelectorAll('button')))
            .find((node) => texts.includes((node.innerText || '').trim())) || null, name, ['Manage', 'Yönet']);
        const element = found.asElement();
        if (!element) throw new Error(`the Components list offers no Manage for ${name}`);
        await drainLog();
        await ctl({ override: { [SERVICES]: { ...shared, after: 1, times: 1 } } });
        await element.click();
        await page.waitForSelector('main h1');
        return page;
    };
    const setValue = async (page, ariaLabelPart, value) => {
        const found = await page.evaluateHandle((part) => Array.from(document.querySelectorAll('main input[type="text"], main input[type="email"], main input[type="number"]'))
            .find((node) => (node.getAttribute('aria-label') || '').includes(part)) || null, ariaLabelPart);
        const element = found.asElement();
        if (!element) throw new Error(`no field labelled ${ariaLabelPart}`);
        await element.evaluate((node) => node.scrollIntoView({ block: 'center' }));
        await element.evaluate((node) => { node.focus(); node.select(); });
        await element.type(String(value));
    };
    const toBottom = (page) => page.evaluate(() => { const bar = Array.from(document.querySelectorAll('main .sticky')).pop(); (bar || document.querySelector('main')).scrollIntoView({ block: 'end' }); });
    const toNotice = async (page) => { await page.evaluate(() => (document.querySelector('main [role="alert"], main div[role="status"]:not([aria-label])'))?.scrollIntoView({ block: 'center' })); await pause(200); };
    const save = async (page) => { await clickByText(page, SAVE); await quiet(page); await pause(300); };
    // A tab or sub-tab of a page, by its whole label.
    const clickExact = async (page, texts) => {
        const found = await page.evaluateHandle((list) => Array.from(document.querySelectorAll('main button, main [role="tab"]'))
            .find((node) => node.getClientRects().length > 0 && list.includes((node.innerText || '').trim().replace(/\s*[\d…–]+$/, ''))) || null, texts);
        const element = found.asElement();
        if (!element) throw new Error(`no tab labelled ${texts.join(' | ')}`);
        await element.click();
    };
    // One domain's page with a tab open. The reads of a tab start when it
    // opens, so what withholds them is set between the page and the tab.
    const domainTab = async (labels, override = null, settled = true) => {
        const page = await open('/domains/example.com');
        if (override) await ctl({ override });
        await clickExact(page, labels);
        if (settled) { await quiet(page); await pause(300); }
        return page;
    };

    // --- 50-54: PostgreSQL and MariaDB configuration ---------------------------------
    scenarios.dbconfig = async () => {
        // Which files the component has: slow, then known; nothing below moves.
        // The page is reached from the Components list, as a person reaches
        // it: opened by its address, the lookup above the page reads the
        // records first (scenario `lookup`) and the page starts with them.
        let page = await fromList('PostgreSQL', { delay: 4500 }, '50-0-components-list');
        await page.waitForSelector('main [role="status"][aria-label]', { timeout: 15000 }).catch(() => {});
        await pause(600);
        const before = await rectOf(page, 'main h3, main [role="status"][aria-label]');
        await record(page, '50a-postgresql-files-checking', { requests: (await drainLog()).filter((line) => line.includes(SERVICES)) });
        await waitFor(page, () => !!document.querySelector('main input[type="search"]'), 20000);
        await quiet(page);
        await record(page, '50b-postgresql-conf-loaded', { cardTopWhileChecking: before, cardTopAfter: await rectOf(page, 'main h3') });
        await closePage(page);

        // The list of files could not be read: no "not found", Retry.
        page = await fromList('PostgreSQL', { status: 502, body: { error: 'agent unavailable', code: 'INTERNAL' } });
        await quiet(page); await pause(300);
        await record(page, '50c-postgresql-files-could-not-read', { retryControls: await page.$$eval('main button', (nodes, texts) => nodes.filter((node) => texts.includes((node.innerText || '').trim())).length, RETRY) });
        await ctl({ clear: [SERVICES] });
        await clickByText(page, RETRY);
        await waitFor(page, () => !!document.querySelector('main input[type="search"]'), 15000);
        await record(page, '50d-postgresql-files-after-retry');
        await closePage(page);

        // The scan was read and names no postgresql.conf: the known negative.
        await fresh({}, { managedScan: { ...SCAN, services: SCAN.services.map((item) => (item.id === 'postgresql' ? { ...item, config_files: [] } : item)) } });
        page = await open('/services/postgresql');
        await record(page, '50e-postgresql-files-known-none');
        await closePage(page);

        // postgresql.conf itself: slow, then failing.
        await fresh({}, { override: { [CONFIG]: { delay: 4500 } } });
        page = await open('/services/postgresql', false);
        await page.waitForSelector('main h3', { timeout: 15000 });
        await pause(1200);
        await record(page, '51a-postgresql-conf-checking');
        await closePage(page);

        await fresh({}, { override: { [CONFIG]: { status: 502, body: { error: 'x', code: 'CURRENT_SETTINGS_UNREADABLE', reason: 'config_file' } } } });
        page = await open('/services/postgresql');
        await record(page, '51b-postgresql-conf-could-not-read');
        await closePage(page);

        // Known: change one value, see it marked, then each answer to Save.
        const edit = async (mode) => {
            await fresh({ save: mode });
            const editing = await open('/services/postgresql');
            await setValue(editing, 'max_connections', mode === 'daemon' ? 'lots' : '300');
            return editing;
        };
        page = await edit('ok');
        await record(page, '51c-postgresql-conf-changed');
        await toBottom(page); await pause(200);
        await record(page, '51d-postgresql-conf-save-bar');
        await save(page);
        await toNotice(page);
        await record(page, '51e-postgresql-conf-saved');
        await closePage(page);

        page = await edit('stale');
        await save(page);
        await toNotice(page);
        await record(page, '51f-postgresql-conf-stale-409');
        await closePage(page);

        page = await edit('daemon');
        await save(page);
        await page.evaluate(() => document.querySelector('main [aria-invalid="true"]')?.scrollIntoView({ block: 'center' }));
        await pause(200);
        await record(page, '51g-postgresql-conf-refused-next-to-field');
        await toNotice(page);
        await record(page, '51h-postgresql-conf-refused-notice');
        await closePage(page);

        page = await edit('reload');
        await save(page);
        await toNotice(page);
        await record(page, '51i-postgresql-conf-reload-failed-restored');
        await closePage(page);

        // The reload failed and the previous file is back, but the unit could
        // not reload with it either (10 Oct 2026). The server was asked
        // directly: either it runs its previous settings, or what it runs
        // could not be established. Neither names a copy: the file was put
        // back. What was typed is still in the field.
        for (const [mode, name, phrases] of [
            ['reloadUnit', '51i2-postgresql-conf-reload-failed-unit-reload-failed', ['settings it had before your change', 'değişikliğinizden önceki ayarlarla']],
            ['reloadUnknown', '51i3-postgresql-conf-reload-failed-running-unknown', ['could not establish which settings', 'hangi ayarlarla çalıştığını belirleyemedi']],
        ]) {
            page = await edit(mode);
            await save(page);
            await toNotice(page);
            const typed = await page.evaluate(() => Array.from(document.querySelectorAll('main input[type="text"], main input[type="number"]')).find((node) => (node.getAttribute('aria-label') || '').includes('max_connections'))?.value ?? null);
            const seen = await measured(page, name, { typed });
            must(seen.notices.length === 1, `${name}: ${seen.notices.length} notices after a reload that failed, expected one`);
            const notice = seen.notices[0];
            must(phrases.some((phrase) => notice.includes(phrase)), `${name}: the notice does not say what the server holds: ${notice.slice(0, 200)}`);
            must(notice.includes('sudo systemctl reload postgresql@17-main'), `${name}: the notice does not name the unit to reload: ${notice.slice(0, 300)}`);
            must(notice.includes('Job for postgresql@17-main.service failed'), `${name}: what the unit's reload said is not shown`);
            must(!notice.includes('celikpanel-backup'), `${name}: a copy is named although the previous file was put back`);
            must(!/could not put the previous file back|önceki dosyayı kesin olarak geri koyamadı/i.test(notice), `${name}: says the previous file could not be put back`);
            must(typed === '300', `${name}: the change that was not kept is no longer in the form (${typed})`);
            must(seen.toasts.length === 0, `${name}: a toast was raised: ${seen.toasts.join(' | ')}`);
            await closePage(page);
        }

        page = await edit('dropped');
        await save(page);
        await toNotice(page);
        await record(page, '51j-postgresql-conf-save-answer-lost');
        await closePage(page);

        // pg_hba.conf.
        const rules = async (b2 = {}, extra = {}) => {
            await fresh(b2, extra);
            const opened = await open('/services/postgresql');
            await clickByText(opened, ['Access rules', 'Erişim kuralları']);
            await quiet(opened); await pause(300);
            return opened;
        };
        page = await rules();
        await record(page, '52a-pg-hba-loaded');
        await closePage(page);

        page = await rules({}, { override: { [CONFIG]: { status: 502, body: { error: 'x', code: 'CURRENT_SETTINGS_UNREADABLE' } } } });
        await record(page, '52b-pg-hba-could-not-read');
        await closePage(page);

        page = await rules({ save: 'lockout' });
        await page.evaluate(() => document.querySelector('main ol button[aria-label]')?.click());
        await pause(200);
        await record(page, '52c-pg-hba-rule-marked-for-removal');
        await save(page);
        await toNotice(page);
        await record(page, '52d-pg-hba-lockout-refused');
        await closePage(page);

        page = await rules({ save: 'syntax' });
        await clickByText(page, ['Add rule', 'Kural ekle']);
        await pause(200);
        const blank = async (value) => {
            await waitFor(page, () => Array.from(document.querySelectorAll('main ol input[type="text"]')).some((node) => !node.disabled && node.value === ''), 8000);
            const handle = await page.evaluateHandle(() => Array.from(document.querySelectorAll('main ol input[type="text"]')).find((node) => !node.disabled && node.value === '') || null);
            const element = handle.asElement();
            await element.evaluate((node) => node.scrollIntoView({ block: 'center' }));
            await element.type(value);
        };
        await blank('shop'); await blank('shop_rw');
        await record(page, '52e-pg-hba-new-rule-incomplete');
        await blank('192.0.2.0/24');
        await save(page);
        await page.evaluate(() => document.querySelector('main ol [id$="-refusal"]')?.scrollIntoView({ block: 'center' }));
        await pause(200);
        await record(page, '52f-pg-hba-new-rule-refused-next-to-rule');
        await closePage(page);

        // MariaDB.
        await fresh({}, { override: { [CONFIG]: { status: 502, body: { error: 'x', code: 'CURRENT_SETTINGS_UNREADABLE' } } } });
        page = await open('/services/mariadb');
        await record(page, '53a-mariadb-could-not-read');
        await closePage(page);

        await fresh({ save: 'daemon' });
        page = await open('/services/mariadb');
        await record(page, '53b-mariadb-loaded');
        await setValue(page, 'max_connections', 'lots');
        await save(page);
        await page.evaluate(() => document.querySelector('main [aria-invalid="true"]')?.scrollIntoView({ block: 'center' }));
        await pause(200);
        await record(page, '53c-mariadb-refused-next-to-field');
        await closePage(page);

        await fresh();
        page = await open('/services/mariadb');
        await setValue(page, 'max_connections', '300');
        await save(page);
        await toNotice(page);
        await record(page, '53d-mariadb-saved-waits-for-restart');
        await closePage(page);

        // The raw file.
        await fresh();
        page = await open('/services/postgresql');
        await page.evaluate(() => Array.from(document.querySelectorAll('main button')).find((node) => node.innerText.includes('pg_hba.conf'))?.click());
        await quiet(page); await pause(300);
        await record(page, '54a-raw-file-loaded');
        await closePage(page);

        await fresh({}, { override: { [CONFIG]: { status: 502, body: { error: 'x', code: 'CURRENT_SETTINGS_UNREADABLE' }, after: 1 } } });
        page = await open('/services/postgresql');
        await page.evaluate(() => Array.from(document.querySelectorAll('main button')).find((node) => node.innerText.includes('pg_hba.conf'))?.click());
        await quiet(page); await pause(300);
        await record(page, '54b-raw-file-could-not-read');
        await closePage(page);
    };

    // --- 60-62: a domain's mail screens ------------------------------------------------
    scenarios.mailscreens = async () => {
        const mail = async (override = null, settled = true) => domainTab(['Mail', 'E-posta'], override, settled);
        const settings = async (page) => { await clickExact(page, ['Settings', 'Ayarlar']); await pause(400); };

        await fresh();
        let page = await mail({ [ACCOUNTS]: { delay: 4500 }, [SETUP]: { delay: 4500 }, [FORWARDINGS]: { delay: 4500 } }, false);
        await page.waitForSelector('main [role="status"][aria-label]', { timeout: 15000 }).catch(() => {});
        await pause(900);
        await record(page, '60a-mail-accounts-and-webmail-checking');
        await waitFor(page, () => /info@example\.com/.test(document.querySelector('main')?.innerText || ''), 15000);
        await quiet(page);
        await record(page, '60b-mail-accounts-and-webmail-known');
        await closePage(page);

        await fresh();
        page = await mail({ [ACCOUNTS]: { status: 500, body: { error: 'An internal error occurred.', code: 'INTERNAL' } }, [SETUP]: { status: 502, body: { error: 'x' } }, [FORWARDINGS]: { drop: true } });
        await record(page, '60c-mail-accounts-and-webmail-could-not-read');
        await ctl({ clear: [ACCOUNTS, SETUP, FORWARDINGS] });
        await clickByText(page, RETRY);
        await quiet(page); await pause(300);
        await record(page, '60d-mail-after-one-retry');
        await closePage(page);

        await fresh({ accounts: [], forwardings: [], webmail: false });
        page = await mail();
        await record(page, '60e-mail-accounts-known-empty-webmail-known-unavailable');
        await closePage(page);

        await fresh();
        page = await mail({ [QUOTA]: { status: 502, body: { error: 'x' } } });
        await record(page, '60f-mail-usage-could-not-read');
        await closePage(page);

        // The catch-all address.
        await fresh();
        page = await mail();
        await ctl({ override: { [CATCH_ALL]: { delay: 4500 } } });
        await settings(page);
        const reading = await rectOf(page, 'main input[type="email"]');
        await page.evaluate(() => document.querySelector('main input[type="email"]')?.scrollIntoView({ block: 'center' }));
        await pause(300);
        await record(page, '62a-catch-all-checking');
        await waitFor(page, () => document.querySelector('main input[type="email"]')?.value !== '', 15000);
        await page.evaluate(() => document.querySelector('main input[type="email"]')?.scrollIntoView({ block: 'center' }));
        await pause(300);
        await record(page, '62b-catch-all-known-active', { fieldWhileChecking: reading, fieldAfter: await rectOf(page, 'main input[type="email"]') });
        await closePage(page);

        await fresh();
        page = await mail();
        await ctl({ override: { [CATCH_ALL]: { status: 500, body: { error: 'An internal error occurred.', code: 'INTERNAL' } } } });
        await settings(page);
        await quiet(page);
        await page.evaluate(() => Array.from(document.querySelectorAll('main [role="alert"]')).pop()?.scrollIntoView({ block: 'center' }));
        await pause(300);
        await record(page, '62c-catch-all-could-not-read');
        await closePage(page);

        await fresh({ catchAll: { enabled: false, destination: '' } });
        page = await mail();
        await settings(page);
        await page.evaluate(() => document.querySelector('main input[type="email"]')?.scrollIntoView({ block: 'center' }));
        await pause(300);
        await record(page, '62d-catch-all-known-none');
        await closePage(page);

        await fresh({ catchAllSave: 'stale' });
        page = await mail();
        await settings(page);
        const field = await page.$('main input[type="email"]');
        await field.evaluate((node) => node.scrollIntoView({ block: 'center' }));
        await field.evaluate((node) => { node.focus(); node.select(); });
        await field.type('new-address@example.org');
        await clickByText(page, ['Update', 'Güncelle']);
        await quiet(page); await pause(300);
        await page.evaluate(() => document.querySelector('main input[type="email"]')?.scrollIntoView({ block: 'center' }));
        await pause(200);
        await record(page, '62e-catch-all-stale-409');
        await closePage(page);
    };

    // --- 70: the mail queue and the mail policy -----------------------------------------
    scenarios.mailqueue = async () => {
        await fresh({}, { override: { [QUEUE]: { delay: 4500 } } });
        let page = await open('/services/postfix', false);
        await page.waitForSelector('main [role="status"][aria-label]', { timeout: 15000 }).catch(() => {});
        await pause(900);
        await record(page, '70a-mail-queue-checking');
        await waitFor(page, () => /4F2C1A0B7D/.test(document.querySelector('main')?.innerText || ''), 15000);
        await quiet(page);
        await record(page, '70b-mail-queue-known');
        await closePage(page);

        await fresh({}, { override: { [QUEUE]: { status: 502, body: { error: 'x', code: 'MAIL_QUEUE_UNREADABLE' } } } });
        page = await open('/services/postfix');
        await record(page, '70c-mail-queue-could-not-read');
        await closePage(page);

        // The cause the server verified itself, with Postfix's own line (10 Oct 2026).
        const queueSaid = 'postqueue: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign';
        await fresh({}, { override: { [QUEUE]: { status: 502, body: { error: 'x', code: 'MAIL_QUEUE_UNREADABLE', reason: 'postfix_config', vars: { detail: queueSaid } } } } });
        page = await open('/services/postfix');
        let seen = await measured(page, '70c2-mail-queue-could-not-read-postfix-config');
        must(seen.notices.some((item) => item.includes('main.cf') && item.includes(queueSaid)), `70c2: the notice does not name the cause with Postfix's line: ${seen.notices.join(' | ').slice(0, 300)}`);
        must(!seen.negativeText.length, `70c2: a negative sentence is on screen: ${seen.negativeText.join(' | ')}`);
        must(!/check that Postfix is running|Postfix’in çalıştığını/i.test(seen.notices.join(' ')), '70c2: the notice names a cause the server did not verify');
        await closePage(page);

        await fresh({ queue: [] });
        page = await open('/services/postfix');
        await record(page, '70d-mail-queue-known-empty');
        await closePage(page);

        const sizeField = (at) => at.$('main input[type="number"]');
        const savePolicy = async (at, value = null) => {
            if (value !== null) {
                const size = await sizeField(at);
                await size.evaluate((node) => node.scrollIntoView({ block: 'center' }));
                await size.evaluate((node) => { node.focus(); node.select(); });
                await size.type(value);
            }
            await drainLog();
            await clickByText(at, ['Save policy', 'Politikayı kaydet']);
            await quiet(at); await pause(400);
            return (await drainLog()).filter((line) => line.includes('/api/v1/mail/policy'));
        };
        const policyNotice = async (at) => {
            await at.evaluate(() => Array.from(document.querySelectorAll('main [role="alert"]')).pop()?.scrollIntoView({ block: 'center' }));
            await pause(200);
        };

        await fresh({ policySave: 'notReloaded' });
        page = await open('/services/postfix');
        await savePolicy(page, '50');
        await policyNotice(page);
        await record(page, '70e-mail-policy-written-not-reloaded');
        await closePage(page);

        // Saved, and Postfix was verified not to have taken it (or it could not
        // be established): one sentence per stage, Postfix's own line under it,
        // and the saved values in the form from this very answer, with no
        // second read (10 Oct 2026).
        const stages = [
            ['check', '70f-mail-policy-not-reloaded-check', ['its own check refuses', 'kendi denetimi yapılandırmayı reddediyor'], 'default_process_limit'],
            ['reload', '70g-mail-policy-not-reloaded-reload', ['but the reload failed', 'yeniden yükleme başarısız oldu'], 'the Postfix mail system is not running'],
            ['verify', '70h-mail-policy-not-reloaded-verify', ['no longer running after', 'artık çalışmıyordu'], 'stopped while it was reloading'],
            ['unknown', '70i-mail-policy-reload-unknown', ['could not establish whether', 'alıp almadığını belirleyemedi'], 'resource temporarily unavailable'],
        ];
        for (const [mode, name, phrases, said] of stages) {
            await fresh({ policySave: mode });
            page = await open('/services/postfix');
            const sent = await savePolicy(page, '50');
            await policyNotice(page);
            const shown = await (await sizeField(page)).evaluate((node) => node.value);
            // The surface the sentence stands on: a reload Postfix verifiably
            // did not take is a failure; an outcome that could not be
            // established is not one and must not be drawn as one. Found by
            // looking on 10 Oct 2026: both were on the failure surface.
            const surface = await page.evaluate(() => {
                const box = document.querySelector('main [data-policy-outcome]');
                const drawn = box && Array.from(box.querySelectorAll('div')).find((node) => getComputedStyle(node).borderTopWidth !== '0px');
                return box ? { outcome: box.dataset.policyOutcome, background: drawn ? getComputedStyle(drawn).backgroundColor : null, border: drawn ? getComputedStyle(drawn).borderTopColor : null, text: drawn ? getComputedStyle(drawn.querySelector('p, span') || drawn).color : null } : null;
            });
            seen = await measured(page, name, { policyRequests: sent, sizeShown: shown, surface });
            must(surface && surface.background && surface.border, `${name}: the notice's surface was not found, so its colour was not measured`);
            const attention = /245, 179, 1/.test(`${surface.background} ${surface.border}`);
            const failure = /179, 38, 30|245, 145, 136/.test(`${surface.background} ${surface.border}`);
            if (mode === 'unknown') {
                must(surface.outcome === 'unknown' && attention && !failure, `${name}: an outcome that could not be established is not on the attention surface, or is drawn as a failure: ${JSON.stringify(surface)}`);
            } else {
                must(surface.outcome === 'not-reloaded' && failure && !attention, `${name}: a verified failure is not drawn on the failure surface: ${JSON.stringify(surface)}`);
            }
            const notice = seen.notices.join(' ');
            must(seen.notices.length === 1, `${name}: ${seen.notices.length} notices, expected the one above the saved values`);
            must(phrases.some((phrase) => notice.includes(phrase)), `${name}: the notice does not say the verified stage: ${notice.slice(0, 240)}`);
            must(notice.includes(said), `${name}: the line that goes with the sentence is not shown`);
            must(shown === '50', `${name}: the form does not show the saved value (${shown})`);
            must(sent.filter((line) => line.includes(' PUT ')).length === 1, `${name}: the save was sent ${sent.filter((line) => line.includes(' PUT ')).length} times`);
            must(!sent.some((line) => line.includes(' GET ')), `${name}: the policy was read again although the answer carried it: ${sent.join(' ; ')}`);
            must(seen.toasts.length === 0, `${name}: a toast was raised for a save Postfix did not take: ${seen.toasts.join(' | ')}`);
            if (mode !== 'check') { await closePage(page); continue; }
            // The owner corrects main.cf and presses Save without a change:
            // the save reloads Postfix, the notice leaves and the page says so.
            await ctl({ b2: withB2({ policy: { message_size_mb: 50, dnsbl_zones: ['zen.spamhaus.org'], outbound_rate_limit: 30 }, policySave: 'ok' }) });
            const again = await savePolicy(page);
            seen = await measured(page, '70j-mail-policy-unchanged-save-reloaded', { policyRequests: again });
            must(again.filter((line) => line.includes(' PUT ')).length === 1, `70j: the unchanged save was sent ${again.filter((line) => line.includes(' PUT ')).length} times: ${again.join(' ; ')}`);
            must(seen.notices.length === 0, `70j: the not-reloaded notice is still on screen after a save that reloaded: ${seen.notices.join(' | ').slice(0, 200)}`);
            must(seen.toasts.some((item) => /Postfix was reloaded|Postfix bu değerlerle yeniden yüklendi/.test(item)), `70j: the page does not say Postfix was reloaded: ${seen.toasts.join(' | ')}`);
            // The owner has not corrected it yet: the same reason again.
            await ctl({ b2: withB2({ policy: { message_size_mb: 50, dnsbl_zones: ['zen.spamhaus.org'], outbound_rate_limit: 30 }, policySave: 'check' }) });
            await pause(4500);
            const still = await savePolicy(page);
            await policyNotice(page);
            seen = await measured(page, '70k-mail-policy-unchanged-save-still-refused', { policyRequests: still });
            must(seen.notices.length === 1 && phrases.some((phrase) => seen.notices[0].includes(phrase)), `70k: an unchanged save that Postfix still refuses does not say so: ${seen.notices.join(' | ').slice(0, 200)}`);
            must(seen.toasts.every((item) => !/applied|uygulandı|Nothing to save|Kaydedilecek/.test(item)), `70k: reported as saved: ${seen.toasts.join(' | ')}`);
            await closePage(page);
        }
    };

    // --- 80: a disabled scheduled task -----------------------------------------------------
    scenarios.cron = async () => {
        const tasks = async () => {
            const page = await domainTab(['Advanced', 'Gelişmiş']);
            await clickExact(page, ['Scheduled tasks', 'Zamanlanmış görevler']);
            await quiet(page); await pause(400);
            return page;
        };
        const jobs = (page) => page.evaluate(() => Array.from(document.querySelectorAll('main code, main .font-mono')).map((node) => node.innerText.trim()).filter(Boolean));
        await fresh();
        const page = await tasks();
        await record(page, '80a-cron-with-a-disabled-task', { jobs: await jobs(page) });
        // Enable the disabled one: before this batch the server answered
        // "cron job not found" for every action on a disabled task.
        await page.evaluate(() => {
            const row = Array.from(document.querySelectorAll('main .rounded-xl')).find((node) => /weekly/.test(node.innerText) && node.querySelector('button'));
            Array.from(row.querySelectorAll('button')).find((node) => ['Enable', 'Etkinleştir'].includes(node.title || node.getAttribute('aria-label') || ''))?.click();
        });
        await quiet(page); await pause(400);
        const log = (await drainLog()).filter((line) => line.includes(CRON));
        await record(page, '80b-cron-disabled-task-enabled', { jobs: await jobs(page), requests: log });
        await closePage(page);

        // Why the list could not be read (10 Oct 2026). A cause the server
        // verified (the site user is not in /etc/cron.allow, or is in
        // /etc/cron.deny) is the server owner's rule: said in its own words,
        // without the attention colour and without an alert. Any other answer
        // keeps the sentence that names no cause, followed by the line crontab
        // printed when it printed one. Retry stays, and only reads.
        const crontabSaid = 'You (site1) are not allowed to use this program (crontab)';
        const spoolSaid = "crontab: can't open '/var/spool/cron/crontabs/site1': Permission denied";
        const cases = [
            ['80c-cron-unreadable-cron-allow', { detail: 'cron_allow', vars: { detail: crontabSaid } }, 'cron_allow', '/etc/cron.allow'],
            ['80d-cron-unreadable-cron-deny', { detail: 'cron_deny', vars: { detail: crontabSaid } }, 'cron_deny', '/etc/cron.deny'],
            ['80e-cron-unreadable-crontab-said', { vars: { detail: spoolSaid } }, '', spoolSaid],
            ['80f-cron-unreadable-no-cause', {}, '', ''],
        ];
        for (const [name, extra, cause, mustSay] of cases) {
            await fresh({}, { override: { [CRON]: { status: 502, body: { error: 'x', code: 'CURRENT_SETTINGS_UNREADABLE', reason: 'scheduled_tasks', ...extra } } } });
            const at = await tasks();
            await at.evaluate(() => (document.querySelector('main [data-cron-unreadable], main [role="alert"]'))?.scrollIntoView({ block: 'center' }));
            await pause(200);
            const drawn = await at.evaluate(() => {
                const style = (node) => (node ? { background: getComputedStyle(node).backgroundColor, border: getComputedStyle(node).borderTopColor } : null);
                const box = document.querySelector('main [data-cron-unreadable]');
                const alert = document.querySelector('main [role="alert"]');
                const said = document.querySelector('main [data-cron-said]');
                const literal = said?.querySelector('span');
                const inWidth = (node) => { if (!node) return null; const r = node.getBoundingClientRect(); return r.left >= 0 && r.right <= window.innerWidth + 0.5; };
                return {
                    cause: box ? box.dataset.cronUnreadable : null,
                    causeText: box ? box.innerText.trim() : null,
                    causeSurface: style(box),
                    causeRole: box ? box.getAttribute('role') : null,
                    alertSurface: style(alert),
                    said: said ? { text: said.innerText.trim(), font: literal ? getComputedStyle(literal).fontFamily : '', inWidth: inWidth(said) } : null,
                    inWidth: inWidth(box || alert),
                    pageOverflowsSideways: document.documentElement.scrollWidth > window.innerWidth + 1,
                };
            });
            const seen = await measured(at, name, { cron: drawn });
            must(drawn.cause !== null || seen.notices.length > 0, `${name}: neither the cause nor a could-not-read notice is on screen, so nothing was measured`);
            must(!seen.negativeText.length, `${name}: a negative sentence is on screen for a list that was not read: ${seen.negativeText.join(' | ')}`);
            must(seen.enabledButtons.some((item) => RETRY.includes(item)), `${name}: Retry is not offered`);
            must(!seen.enabledButtons.some((item) => ['Add task', 'Görev ekle'].includes(item)), `${name}: a task can be added to a crontab that was not read`);
            must(seen.toasts.length === 0, `${name}: a failed read raised a toast: ${seen.toasts.join(' | ')}`);
            must(drawn.inWidth && !drawn.pageOverflowsSideways, `${name}: the notice is wider than the screen`);
            if (cause) {
                must(drawn.cause === cause, `${name}: the cause on screen is ${drawn.cause}, expected ${cause}`);
                must(drawn.causeText.includes(mustSay), `${name}: the sentence does not name ${mustSay}: ${drawn.causeText.slice(0, 200)}`);
                must(seen.notices.length === 0 && drawn.causeRole === 'status', `${name}: a server policy is announced as an alert`);
                must(!/245, 179, 1|179, 38, 30|245, 145, 136/.test(`${drawn.causeSurface.background} ${drawn.causeSurface.border}`), `${name}: a server policy is drawn in an attention or failure colour: ${JSON.stringify(drawn.causeSurface)}`);
                must(!drawn.said && !drawn.causeText.includes(crontabSaid), `${name}: the program's line is repeated under a cause that already says it`);
            } else {
                must(drawn.cause === null && seen.notices.length === 1, `${name}: expected the one neutral notice, found cause ${drawn.cause} and ${seen.notices.length} notices`);
                must(!/cron\.allow|cron\.deny/.test(seen.notices[0]), `${name}: a cause the server did not verify is named`);
                if (mustSay) {
                    must(drawn.said && drawn.said.text.includes(mustSay), `${name}: what crontab said is not on screen`);
                    must(/mono/i.test(drawn.said.font), `${name}: the program's line is not in the mono face (${drawn.said.font})`);
                    must(drawn.said.inWidth, `${name}: the program's line runs off the screen`);
                } else {
                    must(!drawn.said, `${name}: a "crontab said" line is shown although it said nothing`);
                }
            }
            // Retry reads again, and only reads; the list comes back.
            await ctl({ clear: [CRON] });
            await drainLog();
            await clickByText(at, RETRY);
            await waitFor(at, () => /cron\.php/.test(document.querySelector('main')?.innerText || ''), 15000);
            const after = (await drainLog()).filter((line) => line.includes(CRON));
            must(after.length > 0 && after.every((line) => line.includes(' GET ')), `${name}: Retry sent something other than a read: ${after.join(' ; ')}`);
            await closePage(at);
        }
    };
}
