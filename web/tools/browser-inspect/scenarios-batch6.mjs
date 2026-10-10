// Batch 6 (11 Oct 2026): the corrections from the second native measurement
// that have a screen.
//
//   importpreview   the archive preview says, for each mailbox, only whether
//                   the archive holds a password the import keeps; no hash is
//                   anywhere on the page or in what the page received.
//   importresult    an import that ended with a part not imported is a verified
//                   partial result (what was imported, what was not, what to
//                   do), a complete one says so, and a site that could not be
//                   created is a refusal that stays on the page.
//   reloadwording   the sentences of a reload that failed: not running,
//                   PostgreSQL re-read its files, PostgreSQL did not, and the
//                   plain one that claims neither. The Services screens send
//                   Start, Stop and Restart only; a Reload reaches the Panel
//                   through its API. The answers are therefore given here to a
//                   Restart press, to see the sentences laid out in the notice.
//   certfailure     a certificate request certbot did not fulfil stays on the
//                   page with its kind, and an administrator sees certbot's
//                   own line.
//
// Like the batches before: the mock on 127.0.0.1 answers everything, nothing
// else is contacted.
//
// Altıncı grup: ikinci yerel ölçümün ekranı olan düzeltmeleri.
import { b4Defaults } from './mock-batch4.mjs';
import { b5Defaults, GUARDED } from './mock-batch5.mjs';

const DOMAINS = [{ id: 1, domain_name: 'example.com', status: 'active', project_type: 'php', php_version: '8.3', ssl_enabled: false, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 0 }];
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3'], mail_server: true, database_servers: ['mariadb', 'postgresql'], db_tools: [] };
const CONNECTION = { domain: 'example.com', server_ip: '192.0.2.4', nameservers: ['ns1.example.net', 'ns2.example.net'], live_nameservers: ['ns1.example.net', 'ns2.example.net'], live_ips: ['192.0.2.4'], status: 'delegated', ssl_ready: true };
const SUBS = [{ id: 3, name: 'Main', owner: 'admin' }];
const mailbox = (user, has_password, quota_mb = 1024) => ({ domain: 'old.example', user, quota_mb, has_password });
const PREVIEW = {
    username: 'olduser', main_domain: 'old.example', domains: ['old.example'], public_html: true, site_bytes: 48234496,
    mail_accounts: [mailbox('info', true), mailbox('sales', true, 250), mailbox('accounting-department-archive', false), mailbox('former.employee', false)],
    forwarders: [{ source: 'hello@old.example', destination: 'info@old.example' }],
    dns_zones: { 'old.example': [{}, {}, {}] }, databases: [{ name: 'olduser_shop', dump_bytes: 2048 }],
};
const PREVIEW_ALL = { ...PREVIEW, mail_accounts: [mailbox('info', true), mailbox('sales', true, 250)] };
const profile = (id, name, services) => ({ id, name, description: `${name} for this server`, status: 'available', available: true, verified: false, latest_attempt_status: 'none', services });
const component = (id, name, category, extra = {}) => ({ id, name, description: `${name} on this server`, icon: '', category, kind: 'service', unit: id, versions: [], status: 'active (running)', is_installed: true, config_files: [], packages: [id], ports: [], ...extra });
const SCAN = {
    scanned_at: new Date().toISOString(), dns_identity_ready: true,
    mail_hostname: { current: 'server1.example.com', hostname: '', source: '', current_usable: true, will_set_hostname: false },
    profiles: [profile('core-mail', 'Mail', ['postfix', 'dovecot']), profile('webmail', 'Webmail', ['roundcube']), profile('protected-mail', 'Protected mail', ['rspamd'])],
    services: [
        component('redis', 'Redis', 'cache', { unit: 'redis-server', config_files: [{ path: '/etc/redis/redis.conf', is_managed: false }] }),
        component('postgresql', 'PostgreSQL', 'database'),
        component('postfix', 'Postfix', 'mail'), component('dovecot', 'Dovecot', 'mail'),
        { ...component('roundcube', 'Roundcube', 'mail'), kind: 'tool', is_installed: false, status: '' },
        { ...component('rspamd', 'Rspamd', 'mail'), is_installed: false, status: '' },
    ],
};
const DB = {
    dbServers: [{ id: 1, type_id: 1, type_name: 'mariadb', type_icon: 'M', name: 'MariaDB', version: '11.8.6-MariaDB', host: 'localhost', port: 3306, is_default: true, status: 'active', created_at: '2026-09-01T10:00:00Z', admin_username: 'celikpanel_admin', is_local: true }],
    dbDatabases: [], dbUsers: [],
};
const PARTIAL = {
    domain_id: 9, site_id: 4, domain: 'old.example', status: 'partial', domain_status: 'pending', code: 'IMPORT_PARTIAL',
    message: 'The import ended with a part of the archive not imported, and it does not continue by itself.',
    imported: ['domain', 'mail', 'forwarders', 'dns', 'database:olduser_shop'],
    not_imported: ['files', 'mail:accounting-department-archive@old.example'],
    steps: [
        { step: 'domain', ok: true, detail: 'old.example (id 9, site 4) → /var/www/celikpanel/subscriptions/3/sites/9/public_html' },
        { step: 'files', ok: false, detail: 'write imported file: no space left on device' },
        { step: 'mail:accounting-department-archive@old.example', ok: false, detail: 'not imported: the archive holds no password for this mailbox' },
        { step: 'mail', ok: true, detail: '2 accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)' },
        { step: 'forwarders', ok: true, detail: '1 forwarders' },
        { step: 'dns', ok: true, detail: 'panel DNS template created; archive DNS import was not selected' },
        { step: 'database:olduser_shop', ok: true, detail: 'created exclusively and dump imported (db USERS are not migrated; repoint app configs)' },
    ],
};
const SITE_NOT_CREATED = { error: 'The import did not start: the site for this domain could not be created on this server, so no file, mailbox, DNS record or database of the archive was imported.', code: 'IMPORT_SITE_NOT_CREATED' };
// A crypt-shaped value, as a mailbox's hash would look. Nothing on a page may match it.
const HASH_SHAPED = /\$(?:1|2[abxy]?|5|6|y|argon2(?:id|i|d)?)\$[^\s"']{4,}/;

export default function register(scenarios, tools) {
    const { base, ctl, reset, drainLog, newPage, closePage, shot, clickByText, waitFor, pause, quiet } = tools;
    const must = (condition, message) => { if (!condition) throw new Error(message); };
    const fresh = async (plan = {}, extra = {}) => {
        await reset();
        await ctl({ clearAll: true });
        await ctl({
            domains: DOMAINS, capabilities: CAPABILITIES, connection: CONNECTION, ssl: null, sslAfterIssue: null, subscriptions: SUBS, importPreview: PREVIEW, ...DB,
            managedScan: { ...SCAN, scanned_at: new Date().toISOString() }, logs: [], b4: b4Defaults(), b5: { ...b5Defaults(), plan }, ...extra,
        });
        await drainLog();
    };
    const done = () => ctl({ b4: null, b5: null, clearAll: true });
    const open = async (path, ready) => {
        const page = await newPage();
        page.on('dialog', (dialog) => { void dialog.accept(); });
        page.answers = [];
        page.on('response', async (response) => {
            if (!response.url().includes('/api/v1/import/')) return;
            try { page.answers.push(await response.text()); } catch { /* the body is gone with the page */ }
        });
        await page.goto(`${base}${path}`, { waitUntil: 'domcontentloaded' });
        await page.waitForSelector('main');
        if (ready) await waitFor(page, ready, 15000);
        await quiet(page);
        return page;
    };
    const clickExact = async (page, texts, scope = 'main button, main [role="tab"]') => {
        const find = () => page.evaluateHandle((list, sel) => Array.from(document.querySelectorAll(sel))
            .find((node) => node.getClientRects().length > 0 && !node.disabled && list.includes((node.innerText || '').trim())) || null, texts, scope);
        let element = (await find()).asElement();
        for (let waited = 0; !element && waited < 10000; waited += 250) { await pause(250); element = (await find()).asElement(); }
        if (!element) throw new Error(`no enabled control labelled ${texts.join(' | ')}`);
        await element.evaluate((node) => node.scrollIntoView({ block: 'center' }));
        await element.click();
    };
    const facts = (page) => page.evaluate(() => {
        const visible = (node) => node && node.getClientRects().length > 0;
        const flat = (node) => (node ? node.innerText.trim().replace(/\s+/g, ' ') : null);
        const one = (selector) => Array.from(document.querySelectorAll(selector)).find(visible);
        const surface = (node) => {
            if (!node) return null;
            const style = getComputedStyle(node);
            const [r, g, b] = (style.backgroundColor.match(/[\d.]+/g) || []).slice(0, 3).map(Number);
            return { background: style.backgroundColor, redder: r > g * 1.6 && r > b * 1.6 };
        };
        const inView = (node) => { if (!node) return null; const r = node.getBoundingClientRect(); return r.top >= 0 && r.bottom <= window.innerHeight; };
        const result = one('[data-import-result]');
        const action = one('[data-service-action]');
        const certificate = one('[data-certificate-issue]');
        // A word that is wider than its box is cut or pushes the page sideways.
        const clipped = Array.from(document.querySelectorAll('main p, main li, main dt, main dd, main h3, main h4, main button'))
            .filter((node) => visible(node) && node.scrollWidth > node.clientWidth + 1 && getComputedStyle(node).overflowX !== 'visible')
            .map((node) => flat(node).slice(0, 60));
        return {
            mailPasswords: flat(one('[data-import-mail-passwords]')),
            importResult: result ? { kind: result.dataset.importResult, text: flat(result), alert: flat(result.querySelector('[role="alert"]')), alertSurface: surface(result.querySelector('[role="alert"]')) } : null,
            serviceAction: action ? { tone: action.dataset.serviceAction, text: flat(action), inView: inView(action), surface: surface(action) } : null,
            certificate: certificate ? { kind: certificate.dataset.certificateIssue, text: flat(certificate), inView: inView(certificate), surface: surface(certificate) } : null,
            notices: Array.from(document.querySelectorAll('[role="alert"]')).filter(visible).map(flat),
            banners: Array.from(document.querySelectorAll('main .border-danger\\/30')).filter(visible).map(flat),
            toasts: Array.from(document.querySelectorAll('.fixed.top-4.right-4 > *')).map((node) => node.innerText.trim()),
            enabledButtons: Array.from(document.querySelectorAll('main button:not(:disabled)')).filter(visible).map((node) => (node.innerText || '').trim()).filter(Boolean),
            mainText: (document.querySelector('main')?.innerText || '').replace(/\s+/g, ' ').slice(0, 4000),
            clipped,
            pageOverflowsSideways: document.documentElement.scrollWidth > window.innerWidth + 1,
        };
    });
    const record = async (page, name, selector, more = {}) => {
        if (selector) await page.evaluate((sel) => { const node = Array.from(document.querySelectorAll(sel)).find((item) => item.getClientRects().length > 0); if (node) node.scrollIntoView({ block: 'start', inline: 'nearest' }); window.scrollBy(0, -16); }, selector);
        await pause(150);
        const seen = await facts(page);
        await shot(page, name, { ...seen, ...more });
        must(!seen.pageOverflowsSideways, `${name}: the page scrolls sideways`);
        must(seen.clipped.length === 0, `${name}: text is cut inside its box: ${JSON.stringify(seen.clipped)}`);
        must(!/\{\w+\}/.test(seen.mainText), `${name}: a placeholder is on screen`);
        must(!HASH_SHAPED.test(seen.mainText), `${name}: a hash-shaped value is on screen`);
        return seen;
    };
    const inspect = async () => {
        const page = await open('/import');
        await page.waitForSelector('main input');
        await page.type('main input', '/var/lib/celikpanel-imports/cpmove-olduser.tar.gz');
        await clickByText(page, ['Inspect archive', 'Arşivi incele'], 'main button');
        await page.waitForSelector('main select');
        await waitFor(page, () => document.querySelectorAll('main select')[1]?.options.length > 1, 12000);
        await page.evaluate(() => { const select = document.querySelectorAll('main select')[1]; select.value = '3'; select.dispatchEvent(new Event('change', { bubbles: true })); });
        await pause(250);
        await quiet(page);
        return page;
    };
    const START = ['Start import', 'İçe aktarmayı başlat'];

    scenarios.importpreview = async () => {
        try {
            await fresh();
            let page = await inspect();
            let seen = await record(page, '130a-import-preview-two-mailboxes-without-a-password', '[data-import-mail-passwords]');
            must(seen.mailPasswords, '130a: the preview says nothing about the mailboxes’ passwords');
            must(/2 of 4|4 posta kutusundan 2/.test(seen.mailPasswords), `130a: the count of mailboxes with a password is not said: ${seen.mailPasswords}`);
            must(seen.mailPasswords.includes('accounting-department-archive@old.example') && seen.mailPasswords.includes('former.employee@old.example'), `130a: the mailboxes without a password are not named: ${seen.mailPasswords}`);
            must(/never shown|hiçbir zaman gösterilmez/.test(seen.mailPasswords), '130a: it is not said that the password itself is not shown');
            must(page.answers.length > 0 && page.answers.every((body) => !HASH_SHAPED.test(body) && !body.includes('crypt_hash')), '130a: an answer the page received holds a hash');
            await closePage(page);

            await fresh({}, { importPreview: PREVIEW_ALL });
            page = await inspect();
            seen = await record(page, '130b-import-preview-every-mailbox-has-a-password', '[data-import-mail-passwords]');
            must(/Each of these mailboxes|her birinin/.test(seen.mailPasswords || ''), `130b: ${seen.mailPasswords}`);
            await closePage(page);

            // An answer that is not a preview is not drawn as an empty archive.
            await fresh({}, { importPreview: { username: 'olduser' } });
            page = await open('/import');
            await page.waitForSelector('main input');
            await page.type('main input', '/var/lib/celikpanel-imports/cpmove-olduser.tar.gz');
            await clickByText(page, ['Inspect archive', 'Arşivi incele'], 'main button');
            await waitFor(page, () => document.querySelectorAll('.fixed.top-4.right-4 > *').length > 0, 12000);
            seen = await record(page, '130c-import-preview-answer-is-not-a-preview', null);
            must(seen.toasts.some((text) => /could not be read|okunamadı/.test(text)), `130c: ${JSON.stringify(seen.toasts)}`);
            must(!/Archive contents|Arşiv içeriği/.test(seen.mainText), '130c: an unreadable answer was drawn as a preview');
            await closePage(page);
        } finally {
            await done();
        }
    };

    scenarios.importresult = async () => {
        try {
            // A part of the archive was not imported.
            await fresh({ [GUARDED.importApply]: { fail: { status: 200, body: PARTIAL } } });
            let page = await inspect();
            await clickExact(page, START);
            await waitFor(page, () => Boolean(document.querySelector('[data-import-result]')), 20000);
            await quiet(page);
            let seen = await record(page, '131a-import-partial-result', '[data-import-result]');
            const result = seen.importResult;
            must(result && result.kind === 'partial', `131a: the result is drawn as "${result?.kind}"`);
            must(result.alert, '131a: the partial result is not announced as an alert');
            must(!result.alertSurface.redder, `131a: a partial result stands on the failure surface (${result.alertSurface.background})`);
            must(/imported in part|kısmen içe aktarıldı/.test(result.alert), `131a: the heading does not say "in part": ${result.alert.slice(0, 120)}`);
            must(/does not continue by itself|kendiliğinden sürmez/.test(result.alert), '131a: it is not said that nothing continues by itself');
            must(/Website files|Site dosyaları/.test(result.alert) && /accounting-department-archive@old\.example/.test(result.alert), '131a: what was not imported is not named in the page’s own words');
            must(/Database olduser_shop|olduser_shop veritabanı/.test(result.alert), '131a: what was imported is not named');
            must(/import the archive again|arşivi yeniden içe aktarın/.test(result.alert) && /by hand|elle/.test(result.alert), '131a: what the owner can do is not said');
            must(!/pending|beklemede|Import finished|İçe aktarma tamamlandı/i.test(result.text), `131a: the result reads as pending or as finished: ${result.text.slice(0, 200)}`);
            must(!/database:|mail:|forwarder:/.test(result.alert), `131a: a step is shown by its internal name: ${result.alert.slice(0, 300)}`);
            must(/no space left on device/.test(result.text), '131a: the failed step’s own line is not shown');
            must(/Create it on the domain|alan adının posta sayfasında/.test(result.text), '131a: the mailbox without a password has no sentence of its own');
            must(seen.toasts.length === 0, `131a: the result is also a toast: ${JSON.stringify(seen.toasts)}`);
            must(seen.enabledButtons.some((label) => /Open old\.example|old\.example alan adını aç/.test(label)), `131a: the domain cannot be opened from the result: ${JSON.stringify(seen.enabledButtons)}`);
            await record(page, '131b-import-partial-result-steps', '[data-import-result] ul.space-y-2');
            await closePage(page);

            // Every part was imported.
            await fresh();
            page = await inspect();
            await clickExact(page, START);
            await waitFor(page, () => Boolean(document.querySelector('[data-import-result]')), 20000);
            await quiet(page);
            seen = await record(page, '131c-import-complete-result', '[data-import-result]');
            must(seen.importResult.kind === 'complete' && !seen.importResult.alert, `131c: ${JSON.stringify(seen.importResult).slice(0, 200)}`);
            must(/Import finished|İçe aktarma tamamlandı/.test(seen.importResult.text) && /in service|hizmette/.test(seen.importResult.text), '131c: a complete import does not say so');
            await closePage(page);

            // The site could not be created: nothing was imported, and the
            // refusal stays on the page.
            await fresh({ [GUARDED.importApply]: { fail: { status: 502, body: SITE_NOT_CREATED } } });
            page = await inspect();
            await clickExact(page, START);
            await waitFor(page, () => /did not start|başlamadı/.test(document.querySelector('main')?.innerText || ''), 20000);
            await quiet(page);
            await pause(5600);
            seen = await record(page, '131d-import-site-not-created', 'main .border-danger\\/30');
            must(seen.banners.some((text) => /The import did not start|İçe aktarım başlamadı/.test(text)), `131d: the refusal is not on the page after the toasts are gone: ${JSON.stringify(seen.banners)}`);
            must(seen.banners.some((text) => /journalctl -u celikpanel-agent/.test(text)), '131d: the command that shows the failed step is not named');
            must(!seen.importResult, '131d: a refusal is drawn as a result');
            must(seen.enabledButtons.some((label) => START.includes(label)), '131d: the import cannot be started again after a refusal that changed nothing');
            await closePage(page);
        } finally {
            await done();
        }
    };

    // --- A reload that failed: only what was verified ---------------------------
    const RELOADS = {
        notRunning: { status: 409, code: 'SERVICE_ACTION_FAILED', reason: 'not_running', error: 'The service is not running, so there was nothing to reload and nothing was changed.', vars: { command: 'sudo postfix status', detail: 'Postfix is not running; nothing was reloaded' } },
        reread: { code: 'SERVICE_ACTION_FAILED', reason: 'reload_reread', error: 'The unit reported the reload as failed, but PostgreSQL itself re-read its configuration files after it.', vars: { command: 'sudo systemctl status postgresql@17-main.service', detail: 'the reload of postgresql@17-main.service was reported as failed (exit-code), but PostgreSQL re-read its configuration files after it: pg_conf_load_time() moved', owner_unit: 'postgresql@17-main.service' } },
        notReread: { code: 'SERVICE_ACTION_FAILED', reason: 'reload_not_reread', error: 'The reload failed and PostgreSQL did not re-read its configuration files.', vars: { command: 'sudo systemctl status postgresql@17-main.service', detail: 'the reload of postgresql@17-main.service failed (exit-code) and PostgreSQL did not re-read its configuration files: pg_conf_load_time() did not move', owner_unit: 'postgresql@17-main.service' } },
        plain: { code: 'SERVICE_ACTION_FAILED', reason: 'reload', error: 'The service reported that the reload failed.', vars: { command: 'sudo systemctl status dovecot', detail: 'Job for dovecot.service failed because the control process exited with error code.' } },
    };
    const CLAIMS_OLD_SETTINGS = /keeps running with the settings it had|önceki ayarlarıyla çalışmayı sürdürüyor/;
    const reloadCase = async (page, name, outcome, expected) => {
        await ctl({ b5: { ...b5Defaults(), serviceAction: outcome } });
        await clickExact(page, ['Restart', 'Yeniden başlat']);
        await waitFor(page, () => Boolean(document.querySelector('[data-service-action]')), 15000);
        await quiet(page);
        const seen = await record(page, name, '[data-service-action]');
        const notice = seen.serviceAction;
        must(notice, `${name}: the outcome is not on the page`);
        must(expected.test(notice.text), `${name}: the sentence is not the one for this answer: ${notice.text.slice(0, 260)}`);
        must(!notice.text.includes(outcome.error), `${name}: the server's English sentence is shown in place of the catalogue's`);
        must(!/\{\w+\}|SERVICE_ACTION/.test(notice.text), `${name}: a placeholder or an internal name is on screen`);
        must(notice.text.includes(outcome.vars.command), `${name}: the command is not shown`);
        must(notice.inView, `${name}: the notice is outside the window`);
        must(seen.toasts.length === 0, `${name}: the outcome is also a toast`);
        await clickExact(page, ['Close', 'Kapat']);
        await pause(250);
        return notice;
    };
    scenarios.reloadwording = async () => {
        try {
            await fresh();
            const ready = () => Array.from(document.querySelectorAll('main button')).some((node) => ['Restart', 'Yeniden başlat'].includes((node.innerText || '').trim()) && !node.disabled);
            let page = await open('/services/postfix', ready);
            let notice = await reloadCase(page, '132a-reload-of-a-service-that-is-not-running', RELOADS.notRunning, /is not running, so there was nothing to reload|çalışmıyor; bu yüzden yeniden yüklenecek bir şey yoktu/);
            must(!CLAIMS_OLD_SETTINGS.test(notice.text), '132a: a stopped service is said to keep running with its settings');
            must(/use Start here|Başlat’ı kullanın/.test(notice.text), '132a: Start is not offered');
            await closePage(page);

            page = await open('/services/postgresql', ready);
            notice = await reloadCase(page, '132b-postgresql-re-read-although-the-unit-reported-a-failure', RELOADS.reread, /PostgreSQL itself re-read its configuration files|PostgreSQL yapılandırma dosyalarını bundan sonra kendisi yeniden okudu/);
            must(!CLAIMS_OLD_SETTINGS.test(notice.text), '132b: a server that re-read its files is said to keep its settings');
            must(/in effect now|şu an yürürlükte/.test(notice.text), '132b: it is not said that the settings on disk are in effect');
            notice = await reloadCase(page, '132c-postgresql-did-not-re-read', RELOADS.notReread, /did not re-read its configuration files|yapılandırma dosyalarını yeniden okumadı/);
            await closePage(page);

            page = await open('/services/dovecot', ready);
            notice = await reloadCase(page, '132d-reload-failed-and-the-settings-in-effect-were-not-read', RELOADS.plain, /says neither|ne önceki ayarlarını koruduğunu ne de/);
            must(!CLAIMS_OLD_SETTINGS.test(notice.text), '132d: the plain reload sentence claims the old settings');
            await closePage(page);
        } finally {
            await done();
        }
    };

    // --- A certificate request certbot did not fulfil ---------------------------
    const CERTBOT_LINE = "An unexpected error occurred: requests.exceptions.SSLError: HTTPSConnectionPool(host='acme-v02.api.letsencrypt.org', port=443): Max retries exceeded with url: /directory";
    const certificateFailure = (reason, detail) => ({ status: 502, body: { error: 'No certificate was issued.', code: 'CERTIFICATE_ISSUE_FAILED', reason, vars: { domain: 'example.com', kept: 'none', ...(detail ? { detail } : {}) } } });
    scenarios.certfailure = async () => {
        try {
            for (const [name, reason, detail, expected] of [
                ['133a-certificate-authority-could-not-be-reached', 'authority_unreachable', CERTBOT_LINE, /could not reach the certificate authority|sertifika otoritesine ulaşamadı/],
                ['133b-certificate-validation-refused', 'validation', 'Detail: DNS problem: NXDOMAIN looking up A for www.example.com - check that a DNS record exists for this domain', /could not validate one of the names|adlardan birini doğrulayamadı/],
                ['133c-certificate-rate-limited-without-certbots-line', 'rate_limited', '', /one of its limits was reached|sınırlarından birine ulaşıldığı/],
            ]) {
                await fresh({ [GUARDED.certificate]: { fail: certificateFailure(reason, detail) } });
                const page = await open('/domains/example.com', () => Array.from(document.querySelectorAll('main button')).some((node) => ['Hosting', 'Barındırma'].includes((node.innerText || '').trim())));
                await clickExact(page, ['Hosting', 'Barındırma']);
                await pause(350);
                await clickExact(page, ['SSL/TLS']);
                await quiet(page);
                await waitFor(page, () => !document.querySelector('main [role="status"][aria-label]') && !!document.querySelector('main input[type="email"]'), 15000);
                await page.type('main input[type="email"]', 'owner@example.com');
                await clickExact(page, ['Get certificate', 'Sertifika al']);
                await waitFor(page, () => Boolean(document.querySelector('[data-certificate-issue]')), 20000);
                await quiet(page);
                await pause(5600);
                const seen = await record(page, name, '[data-certificate-issue]');
                const notice = seen.certificate;
                must(notice && notice.kind === reason, `${name}: drawn as "${notice?.kind}"`);
                must(expected.test(notice.text), `${name}: the sentence is not the one for this kind: ${notice.text.slice(0, 240)}`);
                must(/No certificate was issued for example\.com|example\.com için sertifika çıkarılmadı/.test(notice.text), `${name}: the domain is not named`);
                must(/Nothing asks again automatically|nothing asks again automatically|kendiliğinden yeniden istemez/.test(notice.text), `${name}: it is not said that nothing asks again`);
                must(!/internal server error|CERTIFICATE_ISSUE/.test(notice.text), `${name}: an internal name or "internal server error" is on screen`);
                must(detail ? notice.text.includes(detail.slice(0, 60)) : !/certbot reported|certbot’un bildirdiği/.test(notice.text), `${name}: certbot's own line is ${detail ? 'missing' : 'announced without a line'}`);
                must(notice.surface.redder, `${name}: a verified failure is not on the failure surface`);
                must(seen.toasts.length === 0, `${name}: the failure is still a toast after the notice is up: ${JSON.stringify(seen.toasts)}`);
                await closePage(page);
            }
        } finally {
            await done();
        }
    };
}
