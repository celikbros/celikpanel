// Batch 7 (12 Oct 2026): the corrections from the final native round that have
// a screen.
//
//   siterefused       a site the web server refused stays on the page in plain
//                     words: what was removed again and whether that was
//                     confirmed, the command to run, nginx's own line under the
//                     sentence. In the Add Domain dialog and on the import page.
//   importentries     an import that left archive entries out lists them as not
//                     imported, in the page's own words, and says that the
//                     domain is in service when nothing else is missing.
//   stopnote          a Stop that succeeded and left the unit marked as failed
//                     says so on the attention surface, never as a failure.
//                     Since 9 Oct 2026 also a Stop whose unit was not read as
//                     settled: it says that, and claims no mark.
//   updaterolledback  the update card says, above Start, that the offered
//                     version was already tried here and rolled back, with the
//                     recorded cause or that none was recorded; Start stays
//                     enabled. Since 9 Oct 2026 the sentence says its time as
//                     the end of that attempt.
//   importleftout     (9 Oct 2026) an import whose DNS was left to the owner's
//                     provider is complete, and the step is neither marked as
//                     imported nor as failed.
//
// Like the batches before: the mock on 127.0.0.1 answers everything, nothing
// else is contacted. The answers of this batch are given through the mock's
// own override of one address, so the mock itself is unchanged.
//
// Yedinci grup: son yerel turun ekranı olan düzeltmeleri.
import { b4Defaults } from './mock-batch4.mjs';
import { b5Defaults, GUARDED } from './mock-batch5.mjs';

const DOMAINS = [{ id: 1, domain_name: 'example.com', status: 'active', project_type: 'php', php_version: '8.4', ssl_enabled: false, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 1024 }];
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.5'], mail_server: true, database_servers: ['mariadb'], db_tools: [] };
const SUBS = [{ id: 3, name: 'Main', owner: 'admin' }];
const PREVIEW = {
    username: 'olduser', main_domain: 'old.example', domains: ['old.example'], public_html: true, site_bytes: 48234496,
    mail_accounts: [{ domain: 'old.example', user: 'info', quota_mb: 1024, has_password: true }],
    forwarders: [], dns_zones: { 'old.example': [{}, {}, {}] }, databases: [{ name: 'olduser_shop', dump_bytes: 2048 }],
};
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
const NGINX_LINE = 'nginx: [emerg] open() "/etc/nginx/snippets/fastcgi-php.conf" failed (2: No such file or directory) in /etc/nginx/sites-enabled/refused.example.conf:27';
const refused = (reason, domain) => ({
    error: 'The server sentence, which the page replaces with its own.', code: 'SITE_WEB_SERVER_REFUSED', reason,
    vars: { domain, command: 'sudo nginx -t' }, details: [NGINX_LINE],
});
const ABSOLUTE = "not imported: the archive names this entry with an absolute path, and an import writes only below the site's own folder; nothing was written for it";
const LONG_ENTRY = '/var/lib/another-application/releases/2026-10-09/shared/storage/framework/cache/data/9f/4c/9f4c1e0d2b7a48c7b3f1a0e5d6c7b8a9f0e1d2c3.cache';
const ENTRIES = {
    domain_id: 9, site_id: 4, domain: 'old.example', status: 'partial', domain_status: 'active', code: 'IMPORT_PARTIAL',
    message: 'The import ended and every part that was chosen was imported; old.example is in service.',
    imported: ['domain', 'files', 'mail', 'database:olduser_shop'],
    not_imported: ['member:/etc/set3-escape-absolute.txt', `member:${LONG_ENTRY}`, 'members:44'],
    left_out: ['forwarders', 'dns'],
    steps: [
        { step: 'domain', ok: true, detail: 'old.example (id 9, site 4) → /var/www/celikpanel/subscriptions/3/sites/9/public_html' },
        { step: 'files', ok: true, detail: '412 files, 48234496 bytes. 1530 other entries of the archive are outside the site folder (homedir/public_html) and are not copied by this step: homedir/mail (1502), homedir/etc (11), mysql (2), cp (1), homedir (14). The databases, mailboxes, forwarders and DNS records are read from their own entries by their own steps; mailbox contents and the other folders of the home directory are not imported' },
        { step: 'member:/etc/set3-escape-absolute.txt', ok: false, detail: ABSOLUTE },
        { step: `member:${LONG_ENTRY}`, ok: false, detail: ABSOLUTE },
        { step: 'members:44', ok: false, detail: 'not imported: 44 more entries of the archive were refused by their names in the same way; 46 in all' },
        { step: 'mail', ok: true, detail: '1 accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)' },
        { step: 'forwarders', ok: true, state: 'none_in_archive', detail: '0 forwarders' },
        { step: 'dns', ok: true, state: 'not_chosen', detail: 'panel DNS template created; archive DNS import was not selected' },
        { step: 'database:olduser_shop', ok: true, detail: 'created exclusively and dump imported (db USERS are not migrated; repoint app configs)' },
    ],
};
// 9 Oct 2026: an import that ended complete on a server whose DNS is the
// owner's external provider. The `dns` step ended without an error and
// imported nothing (`state: left_to_owner`); so did `forwarders`, of which the
// archive holds none.
const EXTERNAL_DNS_DETAIL = 'external DNS ownership preserved; verify provider records before publishing the site';
const LEFT_OUT = {
    domain_id: 9, site_id: 4, domain: 'old.example', status: 'active', domain_status: 'active',
    imported: ['domain', 'files', 'mail', 'database:olduser_shop'], not_imported: [], left_out: ['forwarders', 'dns'],
    steps: [
        { step: 'domain', ok: true, detail: 'old.example (id 9, site 4) → /var/www/celikpanel/subscriptions/3/sites/9/public_html' },
        { step: 'files', ok: true, detail: '412 files, 48234496 bytes' },
        { step: 'mail', ok: true, detail: '1 accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)' },
        { step: 'forwarders', ok: true, state: 'none_in_archive', detail: '0 forwarders' },
        { step: 'dns', ok: true, state: 'left_to_owner', detail: EXTERNAL_DNS_DETAIL },
        { step: 'database:olduser_shop', ok: true, detail: 'created exclusively and dump imported (db USERS are not migrated; repoint app configs)' },
    ],
};
const POSTFIX_LINE = 'postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign';
const stopped = (reason, unit, failedUnit, result, detail) => ({
    success: true, outcome: 'verified', applied: 'stopped',
    note: {
        error: 'The service was stopped and is not running. systemd now shows its unit as failed.', code: 'SERVICE_ACTION_NOTE', reason,
        vars: { unit, failed_unit: failedUnit, result, command: `sudo systemctl reset-failed ${failedUnit}`, ...(detail ? { detail } : {}) },
    },
});
const pending = (reason, unit, pendingUnit, state) => ({
    success: true, outcome: 'verified', applied: 'stopped',
    note: {
        error: 'The service was stopped and is not running. How its unit ended was not read.', code: 'SERVICE_ACTION_NOTE', reason,
        vars: { unit, pending_unit: pendingUnit, command: `systemctl status ${pendingUnit}`, ...(state ? { state } : {}) },
    },
});
const TARGET = { version: 'v0.1.0-alpha.82', commit: 'b'.repeat(40), sequence: '82', os: 'linux', arch: 'amd64', archive_sha256: 'c'.repeat(64), archive_size: '65870672' };
const check = (attempt) => ({ supported: true, available: true, current_version: 'v0.1.0-alpha.81', current_commit: 'a'.repeat(40), target: TARGET, ...(attempt ? { previous_attempt: attempt } : {}) });

export default function register(scenarios, tools) {
    const { base, ctl, reset, drainLog, newPage, closePage, shot, clickByText, waitFor, pause, quiet, locale } = tools;
    const must = (condition, message) => { if (!condition) throw new Error(message); };
    const fresh = async (plan = {}, override = {}) => {
        await reset();
        await ctl({ clearAll: true });
        await ctl({
            domains: DOMAINS, capabilities: CAPABILITIES, connection: null, ssl: null, sslAfterIssue: null, subscriptions: SUBS, importPreview: PREVIEW,
            managedScan: { ...SCAN, scanned_at: new Date().toISOString() }, logs: [], b4: b4Defaults(), b5: { ...b5Defaults(), plan }, override,
        });
        await drainLog();
    };
    const done = () => ctl({ b4: null, b5: null, clearAll: true });
    const open = async (path, ready) => {
        const page = await newPage();
        page.on('dialog', (dialog) => { void dialog.accept(); });
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
        const box = (node) => { if (!node) return null; const r = node.getBoundingClientRect(); return { top: Math.round(r.top), bottom: Math.round(r.bottom), width: Math.round(r.width) }; };
        const result = one('[data-import-result]');
        const action = one('[data-service-action]');
        const dialog = one('[aria-labelledby="add-domain-title"]');
        const card = one('section[aria-labelledby="panel-update-title"]');
        const start = document.querySelector('#panel-update-start-button');
        const banner = (root) => {
            const node = root ? Array.from(root.querySelectorAll('.border-danger\\/30')).find(visible) : null;
            if (!node) return null;
            const line = node.querySelector('ul li');
            return {
                text: flat(node), sentence: flat(node.querySelector('span')), line: flat(line), lineFace: line ? getComputedStyle(line).fontFamily : null,
                surface: surface(node), inView: inView(node), box: box(node), measure: Math.round(node.querySelector('span')?.getBoundingClientRect().width || 0),
            };
        };
        const notes = card ? Array.from(card.querySelectorAll('[role="note"]')).filter(visible) : [];
        const previous = notes.find((node) => node !== notes[0]) || null;
        // A word that is wider than its box is cut or pushes the page sideways.
        const clipped = Array.from(document.querySelectorAll('main p, main li, main dt, main dd, main h3, main h4, main button, [aria-labelledby="add-domain-title"] span, [aria-labelledby="add-domain-title"] li'))
            .filter((node) => visible(node) && node.scrollWidth > node.clientWidth + 1 && getComputedStyle(node).overflowX !== 'visible')
            .map((node) => flat(node).slice(0, 60));
        return {
            importResult: result ? { kind: result.dataset.importResult, text: flat(result), alert: flat(result.querySelector('[role="alert"]')), alertSurface: surface(result.querySelector('[role="alert"]')) } : null,
            serviceAction: action ? { tone: action.dataset.serviceAction, role: action.getAttribute('role'), text: flat(action), inView: inView(action), surface: surface(action), code: flat(action.querySelector('code')) } : null,
            dialogOpen: Boolean(dialog),
            dialogBanner: banner(dialog),
            dialogCreateEnabled: dialog ? Array.from(dialog.querySelectorAll('button[type="submit"]')).some((node) => visible(node) && !node.disabled) : null,
            mainBanner: banner(document.querySelector('main')),
            updateCard: card ? {
                previous: previous ? { text: flat(previous), heading: flat(previous.querySelector('p.font-semibold')), lines: Array.from(previous.querySelectorAll('p')).map(flat), surface: surface(previous), box: box(previous) } : null,
                start: start ? { label: flat(start), disabled: start.disabled, box: box(start) } : null,
                target: flat(card.querySelector('dl')),
            } : null,
            toasts: Array.from(document.querySelectorAll('.fixed.top-4.right-4 > *')).map((node) => node.innerText.trim()),
            enabledButtons: Array.from(document.querySelectorAll('main button:not(:disabled)')).filter(visible).map((node) => (node.innerText || '').trim()).filter(Boolean),
            mainText: (document.querySelector('main')?.innerText || '').replace(/\s+/g, ' ').slice(0, 5000),
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
    // Each row of the result's step list: its label, the words a screen reader
    // gets after it, its own line, and which of the three marks is drawn.
    const stepRows = (page) => page.evaluate(() => Array.from(document.querySelectorAll('[data-import-result] ul.space-y-2 > li')).map((row) => {
        const icon = row.querySelector('svg');
        const tone = icon ? ['text-success', 'text-danger', 'text-fg-muted'].find((name) => icon.classList.contains(name)) || null : null;
        const said = row.querySelector('.sr-only');
        const label = said ? said.parentElement.cloneNode(true) : null;
        if (label) label.querySelector('.sr-only')?.remove();
        return {
            label: label ? label.textContent.trim() : null, said: said ? said.textContent.replace(/^:\s*/, '').trim() : null,
            detail: row.querySelector('.text-xs')?.textContent.trim() || null, tone, iconColor: icon ? getComputedStyle(icon).color : null,
            iconVisible: Boolean(icon && icon.getClientRects().length > 0),
        };
    }));
    const NOTHING = ['Nothing imported, nothing failed', 'İçe aktarılan yok, hata da yok'];
    const DNS = ['DNS records', 'DNS kayıtları'];
    const FORWARDERS = ['Forwarders', 'Yönlendirmeler'];
    const leftOutRows = (name, rows) => {
        for (const labels of [DNS, FORWARDERS]) {
            const row = rows.find((item) => labels.includes(item.label));
            must(row, `${name}: the step ${labels[0]} is not in the list: ${JSON.stringify(rows.map((item) => item.label))}`);
            must(row.tone === 'text-fg-muted' && row.iconVisible, `${name}: ${labels[0]} is drawn with the mark "${row.tone}", not the neutral one`);
            must(NOTHING.includes(row.said), `${name}: ${labels[0]} is read out as "${row.said}"`);
        }
        const imported = rows.filter((item) => item.tone === 'text-success');
        must(imported.length > 0 && imported.every((item) => ['Imported', 'İçe aktarıldı'].includes(item.said)), `${name}: an imported step is not read out as imported`);
        must(rows.filter((item) => item.tone === 'text-danger').every((item) => ['Not imported', 'İçe aktarılmadı'].includes(item.said)), `${name}: a step that was not imported is not read out as that`);
        const tones = new Set(rows.map((item) => item.tone));
        const colours = new Set(rows.map((item) => item.iconColor));
        must(!tones.has(null) && colours.size === tones.size, `${name}: two marks share a colour: ${JSON.stringify(rows.map((item) => [item.tone, item.iconColor]))}`);
    };
    const DIALOG = '[aria-labelledby="add-domain-title"]';
    const refusedBanner = (name, banner, domain, removed) => {
        must(banner, `${name}: the refusal is not on the page`);
        must(!/server sentence/.test(banner.text), `${name}: the server's English sentence is shown in place of the page's own`);
        must(/refused the configuration CelikPanel generated|ürettiği yapılandırmayı reddetti/.test(banner.sentence), `${name}: it is not said what happened: ${banner.sentence.slice(0, 200)}`);
        must(banner.sentence.includes(domain), `${name}: the site is not named`);
        must(banner.sentence.includes('sudo nginx -t'), `${name}: the command to run is not shown`);
        must(removed
            ? /the removal was confirmed|bu kaldırma doğrulandı/.test(banner.sentence) && !/may remain|kalmış olabilir/.test(banner.sentence)
            : /was not confirmed|doğrulanamadı/.test(banner.sentence) && /may remain|kalmış olabilir/.test(banner.sentence) && !/the removal was confirmed|bu kaldırma doğrulandı/.test(banner.sentence),
        `${name}: the sentence says more or less than was verified: ${banner.sentence.slice(0, 300)}`);
        must(/nothing (retries by itself|starts it again automatically)|hiçbir şey (kendiliğinden yeniden denemez|onu kendiliğinden yeniden başlatmaz)/.test(banner.sentence), `${name}: it is not said that nothing retries`);
        must(banner.line === NGINX_LINE, `${name}: nginx's own line is not under the sentence: ${banner.line}`);
        must(/mono/i.test(banner.lineFace || ''), `${name}: nginx's line is not set in the face of program output (${banner.lineFace})`);
        must(!/internal server error|SITE_WEB_SERVER|\{\w+\}/.test(banner.text), `${name}: an internal name or a placeholder is on screen`);
        must(banner.surface.redder, `${name}: a refusal is not on the failure surface`);
    };

    scenarios.siterefused = async () => {
        try {
            for (const [name, reason, removed] of [
                ['140a-add-domain-web-server-refused-removed', 'removed', true],
                ['140b-add-domain-web-server-refused-removal-not-confirmed', 'cleanup_unconfirmed', false],
            ]) {
                await fresh({}, { '/api/v1/domains/create': { method: 'POST', status: 502, body: refused(reason, 'refused.example') } });
                const page = await open('/domains', () => Boolean(document.querySelector('h1')));
                await clickByText(page, ['Add domain', 'Alan adı ekle']);
                await page.waitForSelector(`${DIALOG} input[type="text"]`);
                await waitFor(page, (sel) => !document.querySelector(sel)?.querySelector('[role="status"]'), 12000, DIALOG);
                await page.type(`${DIALOG} input[type="text"]`, 'refused.example');
                await pause(300);
                await page.evaluate((sel) => Array.from(document.querySelectorAll(`${sel} button[type="submit"]`)).find((node) => !node.disabled)?.click(), DIALOG);
                await waitFor(page, (sel) => Boolean(document.querySelector(`${sel} .border-danger\\/30`)), 15000, DIALOG);
                await quiet(page);
                // Longer than a toast lives: the sentence must still be there.
                await pause(5600);
                await page.evaluate((sel) => document.querySelector(`${sel} .border-danger\\/30`)?.scrollIntoView({ block: 'center' }), DIALOG);
                const seen = await record(page, name, null);
                refusedBanner(name, seen.dialogBanner, 'refused.example', removed);
                must(seen.dialogOpen, `${name}: the dialog closed on a refusal`);
                must(seen.dialogCreateEnabled, `${name}: the site cannot be created again from the dialog`);
                must(seen.toasts.length === 0, `${name}: the refusal is also a toast: ${JSON.stringify(seen.toasts)}`);
                must(await page.$eval(`${DIALOG} input[type="text"]`, (node) => node.value) === 'refused.example', `${name}: what was typed is gone`);
                await closePage(page);
            }

            for (const [name, reason, removed] of [
                ['140c-import-web-server-refused-removed', 'import_removed', true],
                ['140d-import-web-server-refused-removal-not-confirmed', 'import_cleanup_unconfirmed', false],
            ]) {
                await fresh({ [GUARDED.importApply]: { fail: { status: 502, body: refused(reason, 'old.example') } } });
                const page = await inspect();
                await clickExact(page, START);
                await waitFor(page, () => Array.from(document.querySelectorAll('main .border-danger\\/30')).some((node) => node.getClientRects().length > 0), 20000);
                await quiet(page);
                await pause(5600);
                const seen = await record(page, name, 'main .border-danger\\/30');
                refusedBanner(name, seen.mainBanner, 'old.example', removed);
                must(/The import did not start|İçe aktarma başlamadı/.test(seen.mainBanner.sentence), `${name}: it is not said that the import did not start`);
                must(/no file, mailbox, DNS record or database|hiçbir dosya, posta kutusu, DNS kaydı ya da veritabanı/.test(seen.mainBanner.sentence), `${name}: it is not said that nothing of the archive was imported`);
                must(!seen.importResult, `${name}: a refusal is drawn as a result`);
                must(seen.enabledButtons.some((label) => START.includes(label)), `${name}: the import cannot be started again`);
                must(seen.toasts.length === 0, `${name}: the refusal is also a toast: ${JSON.stringify(seen.toasts)}`);
                await closePage(page);
            }
        } finally {
            await done();
        }
    };

    scenarios.importentries = async () => {
        try {
            await fresh({ [GUARDED.importApply]: { fail: { status: 200, body: ENTRIES } } });
            const page = await inspect();
            await clickExact(page, START);
            await waitFor(page, () => Boolean(document.querySelector('[data-import-result]')), 20000);
            await quiet(page);
            const seen = await record(page, '141a-import-only-refused-archive-entries', '[data-import-result]');
            const result = seen.importResult;
            must(result && result.kind === 'partial', `141a: the result is drawn as "${result?.kind}"`);
            must(result.alert && !result.alertSurface.redder, '141a: the result is not announced on the attention surface');
            must(/Everything you chose was imported, and old\.example is in service|Seçtiğiniz her şey içe aktarıldı ve old\.example yayında/.test(result.alert), `141a: it is not said that the chosen parts are there: ${result.alert.slice(0, 240)}`);
            must(/refused by their names|adları yüzünden reddedildi/.test(result.alert) && /nothing was written for them|hiçbir şey yazılmadı/.test(result.alert), '141a: it is not said what happened to the entries');
            must(/Archive entry \/etc\/set3-escape-absolute\.txt|Arşiv girdisi \/etc\/set3-escape-absolute\.txt/.test(result.alert), '141a: the entry is not named in the page’s own words');
            must(/44 more archive entries|44 arşiv girdisi daha/.test(result.alert), '141a: the entries that are not listed are not counted');
            must(/file manager|dosya yöneticisi/.test(result.alert) && /refuses the same entries|aynı girdileri yine reddeder/.test(result.alert), '141a: what the owner can do is not said');
            must(!/not finished|bitmemiş|by hand|elle|pending|beklemede/i.test(result.alert), `141a: the result reads as an unfinished domain: ${result.alert.slice(0, 300)}`);
            must(!/member:|members:/.test(result.text), '141a: a step is shown by its internal name');
            must(/an absolute path, and an import writes only inside|mutlak bir yolla adlandırıyor/.test(result.text), '141a: the entry’s own line is not in the page’s words');
            must(/1530 other entries of the archive are outside the site folder/.test(result.text), '141a: the files line does not say what was outside the site folder');
            must(seen.toasts.length === 0, `141a: the result is also a toast: ${JSON.stringify(seen.toasts)}`);
            must(seen.enabledButtons.some((label) => /Open old\.example|old\.example alan adını aç/.test(label)), '141a: the domain cannot be opened from the result');
            // 9 Oct 2026: a part that imported nothing without failing (DNS that
            // was not chosen, forwarders the archive does not hold) is in
            // neither list of the summary.
            const lists = await page.evaluate(() => (document.querySelector('[data-import-result] [role="alert"] dl')?.innerText || '').replace(/\s+/g, ' ').trim());
            must(/Website files|Site dosyaları/.test(lists) && /Archive entry|Arşiv girdisi/.test(lists), `141a: the two lists were not read: ${lists.slice(0, 200)}`);
            must(!DNS.some((label) => lists.includes(label)) && !FORWARDERS.some((label) => lists.includes(label)), `141a: a part that imported nothing is in a list of the summary: ${lists.slice(0, 400)}`);
            const entryRows = await stepRows(page);
            await record(page, '141b-import-refused-archive-entries-steps', '[data-import-result] ul.space-y-2', { stepRows: entryRows });
            leftOutRows('141b', entryRows);
            await closePage(page);
        } finally {
            await done();
        }
    };

    // 9 Oct 2026 (set4): an import that asked for no DNS on a server whose DNS
    // is the owner's external provider listed `dns` as imported.
    scenarios.importleftout = async () => {
        try {
            await fresh({ [GUARDED.importApply]: { fail: { status: 200, body: LEFT_OUT } } });
            const page = await inspect();
            await clickExact(page, START);
            await waitFor(page, () => Boolean(document.querySelector('[data-import-result]')), 20000);
            await quiet(page);
            const seen = await record(page, '144a-import-complete-dns-left-to-the-owner', '[data-import-result]');
            const result = seen.importResult;
            must(result && result.kind === 'complete' && !result.alert, `144a: the result is drawn as "${result?.kind}"${result?.alert ? ' with an alert' : ''}`);
            must(/Every part you chose was imported, and old\.example is in service|Seçtiğiniz her parça içe aktarıldı ve old\.example hizmette/.test(result.text), `144a: a complete import does not say so: ${result.text.slice(0, 200)}`);
            must(result.text.includes(EXTERNAL_DNS_DETAIL), '144a: the DNS step does not say that the records stay with the provider');
            const rows = await stepRows(page);
            must(rows.length === LEFT_OUT.steps.length, `144a: ${rows.length} step rows for ${LEFT_OUT.steps.length} steps`);
            leftOutRows('144a', rows);
            must(rows.filter((item) => item.tone === 'text-danger').length === 0, '144a: a step is drawn as failed');
            must(rows.find((item) => DNS.includes(item.label)).detail === EXTERNAL_DNS_DETAIL, '144a: the DNS row does not carry its own line');
            must(seen.toasts.length === 0, `144a: the result is also a toast: ${JSON.stringify(seen.toasts)}`);
            await record(page, '144b-import-complete-dns-left-to-the-owner-steps', '[data-import-result] ul.space-y-2', { stepRows: rows });
            await closePage(page);
        } finally {
            await done();
        }
    };

    const noteCase = async (page, name, answer, press) => {
        await ctl({ override: { '/api/v1/service/action': { method: 'POST', status: 200, body: answer } } });
        await press(page);
        await waitFor(page, () => Boolean(document.querySelector('[data-service-action]')), 15000);
        await quiet(page);
        await pause(5600);
        const seen = await record(page, name, '[data-service-action]');
        const notice = seen.serviceAction;
        const vars = answer.note.vars;
        must(notice, `${name}: the note is not on the page`);
        must(notice.tone === 'note' && notice.role === 'status', `${name}: drawn as "${notice.tone}" with role "${notice.role}"`);
        must(!notice.surface.redder, `${name}: a successful Stop stands on the failure surface (${notice.surface.background})`);
        must(/was stopped and is not running|durduruldu ve çalışmıyor/.test(notice.text), `${name}: it is not said that the service stopped: ${notice.text.slice(0, 200)}`);
        must(notice.text.includes(vars.failed_unit) && notice.text.includes(vars.result), `${name}: the unit and systemd's result are not named`);
        must(/leaves it as it is|olduğu gibi bırakır/.test(notice.text), `${name}: it is not said that the mark is left`);
        must(notice.code === vars.command, `${name}: the command is not set apart: ${notice.code}`);
        must(vars.detail ? notice.text.includes(vars.detail) : !/The service said|Hizmetin yanıtı/.test(notice.text), `${name}: the service's own line is ${vars.detail ? 'missing' : 'announced without a line'}`);
        must(!notice.text.includes(answer.note.error), `${name}: the server's English sentence is shown in place of the catalogue's`);
        must(!/\{\w+\}|SERVICE_ACTION|did not stop|durmadı/.test(notice.text), `${name}: a placeholder, an internal name or a failure is on screen`);
        must(notice.inView, `${name}: the note is outside the window`);
        must(seen.toasts.length === 0, `${name}: the note is also a toast: ${JSON.stringify(seen.toasts)}`);
        await clickExact(page, ['Close', 'Kapat']);
        await pause(250);
        must(!(await facts(page)).serviceAction, `${name}: the note stayed after Close`);
    };
    // 9 Oct 2026: a Stop that succeeded while the unit's own stop was not read
    // (systemd was still stopping the unit when the wait ended, or the unit
    // could not be read). It is said, on the attention surface, and it claims
    // the unit neither failed nor clean.
    const pendingCase = async (page, name, answer, press) => {
        await ctl({ override: { '/api/v1/service/action': { method: 'POST', status: 200, body: answer } } });
        await press(page);
        await waitFor(page, () => Boolean(document.querySelector('[data-service-action]')), 15000);
        await quiet(page);
        await pause(5600);
        const seen = await record(page, name, '[data-service-action]');
        const notice = seen.serviceAction;
        const vars = answer.note.vars;
        must(notice, `${name}: the note is not on the page`);
        must(notice.tone === 'note' && notice.role === 'status', `${name}: drawn as "${notice.tone}" with role "${notice.role}"`);
        must(!notice.surface.redder, `${name}: a successful Stop stands on the failure surface (${notice.surface.background})`);
        must(/was stopped and is not running|durduruldu ve çalışmıyor/.test(notice.text), `${name}: it is not said that the service stopped: ${notice.text.slice(0, 200)}`);
        must(notice.text.includes(vars.pending_unit), `${name}: the unit is not named`);
        must(vars.state
            ? notice.text.includes(vars.state) && /had not finished stopping|durdurmayı bitirmemişti/.test(notice.text) && /may end marked as failed|failed olarak işaretlenmiş olabilir/.test(notice.text)
            : /could not be read from systemd|systemd’den okunamadı/.test(notice.text) && /it is not known whether|bilinmiyor/.test(notice.text),
        `${name}: it is not said what was not read: ${notice.text.slice(0, 300)}`);
        must(notice.code === vars.command && /^systemctl status /.test(notice.code), `${name}: the command is not set apart, or it is not one that only shows the unit: ${notice.code}`);
        must(/nothing looks again by itself|hiçbir şey kendiliğinden yeniden bakmaz/.test(notice.text), `${name}: it is not said that nothing looks again`);
        must(!/now shows .* as failed|olarak gösteriyor|reset-failed|leaves it as it is|olduğu gibi bırakır/.test(notice.text), `${name}: the note claims a mark that was not read: ${notice.text.slice(0, 300)}`);
        must(!notice.text.includes(answer.note.error), `${name}: the server's English sentence is shown in place of the catalogue's`);
        must(!/\{\w+\}|SERVICE_ACTION|unit_not_settled|unit_state_not_read|did not stop|durmadı/.test(notice.text), `${name}: a placeholder, an internal name or a failure is on screen`);
        must(notice.inView, `${name}: the note is outside the window`);
        must(seen.toasts.length === 0, `${name}: the note is also a toast: ${JSON.stringify(seen.toasts)}`);
        await clickExact(page, ['Close', 'Kapat']);
        await pause(250);
        must(!(await facts(page)).serviceAction, `${name}: the note stayed after Close`);
    };
    scenarios.stopnote = async () => {
        try {
            await fresh();
            const ready = () => Array.from(document.querySelectorAll('main button')).some((node) => ['Stop', 'Durdur'].includes((node.innerText || '').trim()) && !node.disabled);
            const stop = (at) => clickExact(at, ['Stop', 'Durdur']);
            let page = await open('/services/postfix', ready);
            await noteCase(page, '142a-postfix-stop-left-the-unit-marked-failed', stopped('unit_marked_failed_config', 'postfix', 'postfix@-.service', 'exit-code', POSTFIX_LINE), stop);
            await closePage(page);
            page = await open('/services/redis', ready);
            await noteCase(page, '142b-stop-left-the-unit-marked-failed-without-a-line', stopped('unit_marked_failed', 'redis-server', 'redis-server.service', 'timeout', ''), stop);
            await closePage(page);
            page = await open('/services/postfix', ready);
            await pendingCase(page, '142c-postfix-stop-the-unit-had-not-settled', pending('unit_not_settled', 'postfix', 'postfix@-.service', 'deactivating'), stop);
            await closePage(page);
            page = await open('/services/postfix', ready);
            await pendingCase(page, '142d-postfix-stop-the-unit-could-not-be-read', pending('unit_state_not_read', 'postfix', 'postfix@-.service', ''), stop);
            await closePage(page);
        } finally {
            await done();
        }
    };

    scenarios.updaterolledback = async () => {
        try {
            for (const [name, attempt, expected] of [
                ['143a-update-card-version-rolled-back-with-a-recorded-cause', { request_id: 'e'.repeat(32), phase: 'recovered', failure_code: 'candidate_panel_startup_check_failed', finished_at: '2026-10-09T08:06:32Z' }, 'cause'],
                ['143b-update-card-version-rolled-back-no-cause-recorded', { request_id: 'e'.repeat(32), phase: 'recovered', finished_at: '2026-10-09T07:52:52Z' }, 'none'],
                ['143c-update-card-no-earlier-attempt', null, 'absent'],
            ]) {
                await fresh({}, { '/api/v1/panel/update/check': { method: 'GET', status: 200, body: check(attempt) } });
                const page = await open('/settings?section=updates', () => Array.from(document.querySelectorAll('main button')).some((node) => ['Check for updates', 'Güncellemeyi kontrol et'].includes((node.innerText || '').trim()) && !node.disabled));
                await clickExact(page, ['Check for updates', 'Güncellemeyi kontrol et']);
                await waitFor(page, () => { const start = document.querySelector('#panel-update-start-button'); return Boolean(start) && !start.disabled; }, 20000);
                await quiet(page);
                const seen = await record(page, name, 'section[aria-labelledby="panel-update-title"] dl');
                const card = seen.updateCard;
                must(card && card.start, `${name}: the card or its Start button is not on the page`);
                must(!card.start.disabled && card.start.label.includes('v0.1.0-alpha.82'), `${name}: Start is disabled or does not name the version: ${JSON.stringify(card.start)}`);
                must(/v0\.1\.0-alpha\.82/.test(card.target || ''), `${name}: the offered version is hidden`);
                if (expected === 'absent') {
                    must(!card.previous, `${name}: a notice about an earlier attempt is shown without one: ${card.previous?.text}`);
                    await closePage(page);
                    continue;
                }
                const previous = card.previous;
                must(previous, `${name}: nothing is said about the earlier attempt`);
                must(/already tried on this server and rolled back|daha önce denendi ve geri alındı/.test(previous.heading || ''), `${name}: the heading does not say it was rolled back: ${previous.heading}`);
                must(previous.lines[1].includes('v0.1.0-alpha.82') && previous.lines[1].includes('v0.1.0-alpha.81'), `${name}: the two versions are not named: ${previous.lines[1]}`);
                must(/which it runs now|şu an onu çalıştırıyor/.test(previous.lines[1]), `${name}: it is not said what the server runs now`);
                // 9 Oct 2026 (set4): the one time the server gives is when the
                // attempt ended. It is said as that and never beside "started".
                const ended = await page.evaluate((at, language) => new Date(at).toLocaleString(language === 'tr' ? 'tr-TR' : 'en-US'), attempt.finished_at, locale);
                must(previous.lines[1].includes(`that attempt ended on ${ended}.`) || previous.lines[1].includes(`o deneme ${ended} tarihinde sona erdi.`), `${name}: the time is not said as the end of the attempt (${ended}): ${previous.lines[1]}`);
                must(!/started (here |on this server )?on \d|tarihinde başlatıldı/.test(previous.lines[1]), `${name}: the end time stands beside "started": ${previous.lines[1]}`);
                must(expected === 'cause'
                    ? /Recorded cause:|Kaydedilen neden:/.test(previous.lines[2]) && !/no more specific cause|daha belirli bir neden kaydetmedi/.test(previous.text)
                    : /recorded no more specific cause|daha belirli bir neden kaydetmedi/.test(previous.lines[2]),
                `${name}: the cause line is not the one for this attempt: ${previous.lines[2]}`);
                must(/runs the same update|aynı güncellemeyi çalıştırır/.test(previous.lines[3]) && /still starts v0\.1\.0-alpha\.82|v0\.1\.0-alpha\.82 sürümünü yine başlatır/.test(previous.lines[3]), `${name}: it is not said what starting it again does: ${previous.lines[3]}`);
                must(previous.box.bottom <= card.start.box.top, `${name}: the notice is not above the Start button`);
                must(!previous.surface.redder, `${name}: guidance stands on the failure surface`);
                must(!/\{\w+\}|recovery\.reason\.|panelUpdate\./.test(previous.text), `${name}: a placeholder or a key is on screen: ${previous.text.slice(0, 200)}`);
                await closePage(page);
            }
        } finally {
            await done();
        }
    };
}
