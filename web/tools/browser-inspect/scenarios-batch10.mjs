// Batch 10 (2026-10-10): a site configuration file the owner changed (D-031).
//
//   siteconfig   one domain's Configuration file page in each state the Agent's
//                classifier reports: edited by the owner (the notice above the
//                domain, the page, "merge by hand", the confirmation of "take
//                CelikPanel's" and its result), replaced by the owner, not
//                recognised (unknown origin), missing, unreadable (locked),
//                CelikPanel's text adopted from an earlier release, a read
//                that failed; and the Domains list with the first state after
//                an update (adopted / left alone, the row badge).
//                Step 1b (same day): a new certificate the kept file does not
//                use yet, a certificate the kept file does not let validate,
//                the notice of a file kept by choice, an unreadable link, and
//                the list once every left-alone file has a decision.
//
// Like the batches before: the mock on 127.0.0.1 answers everything; nothing
// else is contacted, nothing is started. Every answer here is an override of
// the mock's routes.
//
// Onuncu grup: sahibin değiştirdiği site yapılandırma dosyası.

const D = '/api/v1/domains/1';
const SC = `${D}/site-config`;
const PATH = '/etc/nginx/sites-available/example.com.conf';
const INCLUDE = '/etc/nginx/celikpanel-sites.d/example.com';
const MANAGED = '/etc/nginx/celikpanel-managed.d/example.com';
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3'], mail_server: true, database_servers: ['mariadb'], db_tools: [] };
const CONNECTION = { domain: 'example.com', server_ip: '192.0.2.4', nameservers: ['ns1.example.net', 'ns2.example.net'], live_nameservers: ['ns1.example.net', 'ns2.example.net'], live_ips: ['192.0.2.4'], status: 'delegated', ssl_ready: true, glue_needed: false, nameservers_usable: true, propagation_pending: false, checked_at: '2026-10-10T09:00:00Z', dns_management_mode: 'local' };
const row = (id, name, siteConfig) => ({ id, domain_name: name, status: 'active', project_type: 'php', php_version: '8.3', ssl_enabled: false, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 0, ...(siteConfig ? { site_config: siteConfig } : {}) });
const DIFF = [
    `--- ${PATH} (on this server)`,
    '+++ CelikPanel\'s text',
    '@@ -18,9 +18,9 @@',
    '     error_log /var/log/nginx/example.com-error.log;',
    '     root /var/www/celikpanel/subscriptions/1/sites/1/public_html;',
    '-    index owner.html index.php index.html index.htm;',
    '+    index index.php index.html index.htm;',
    ' ',
    '     location / {',
    '         try_files $uri $uri/ /index.php?$query_string;',
    '     }',
    '-',
    '-    location /owner-extra/ {',
    '-        return 200 "owner";',
    '-    }',
    '@@ -44,3 +40,6 @@',
    '     location ~ /\\.(?!well-known).* {',
    '         deny all;',
    '     }',
    '+',
    '+    # Your additions for this site (server context) / Bu site için eklemeleriniz',
    `+    include ${INCLUDE}/*.conf;`,
    ' }',
    '',
].join('\n');
const view = (state, more = {}) => ({
    domain_id: 1, domain: 'example.com', kind: 'nginx_vhost', path: PATH, state, include_dir: INCLUDE,
    managed_dir: MANAGED, managed_include: `include ${MANAGED}/*.conf;`,
    file_sha256: 'c'.repeat(64), render_sha256: 'b'.repeat(64),
    actions: ['owner_edited', 'foreign', 'unknown_origin'].includes(state) ? ['keep', 'take', 'merge'] : state === 'absent' ? ['recreate'] : [],
    ...more,
});
const EDITED = view('owner_edited', { pending_path: `${PATH}.celikpanel-pending`, diff: DIFF });
const KEPT_DECISION = { kind: 'keep_mine', decided_at: '2026-10-10T11:30:00Z', current: true };
const CERT_READY = { ...EDITED, decision: KEPT_DECISION, validation: 'ready', pending_reason: 'certificate', certificate: {
    cert_path: '/var/lib/celikpanel-agent/certificates/example.com/sha256-' + 'd'.repeat(64) + '/fullchain.pem',
    key_path: '/var/lib/celikpanel-agent/certificates/example.com/sha256-' + 'd'.repeat(64) + '/privkey.pem',
    expires_at: '2027-01-08T00:00:00Z', served_expires_at: '2026-10-22T00:00:00Z', served_days_left: 11, referenced: false,
} };
const CERT_VALIDATION = view('unknown_origin', { diff: DIFF, validation: 'include_missing', pending_reason: 'certificate_validation', certificate: {
    cert_path: '/etc/letsencrypt/live/example.com/fullchain.pem', key_path: '/etc/letsencrypt/live/example.com/privkey.pem',
    expires_at: '2026-10-19T00:00:00Z', served_expires_at: '2026-10-19T00:00:00Z', served_days_left: 8, referenced: false,
} });

// What the page shows, read in the browser.
const facts = page => page.evaluate(() => {
    const root = document.querySelector('[data-site-config]');
    const notice = document.querySelector('[data-site-config-notice]');
    const diff = document.querySelector('[data-site-config-diff]');
    const probe = document.createElement('p'); probe.className = 'text-danger'; document.body.appendChild(probe);
    const danger = getComputedStyle(probe).color; probe.remove();
    const lines = Array.from(root?.querySelectorAll('p,h2,h3,button') || []);
    const overflow = Array.from(document.querySelectorAll('main *')).filter(node => node.scrollWidth > node.clientWidth + 1 && !['PRE', 'CODE'].includes(node.tagName) && getComputedStyle(node).overflowX === 'visible').length;
    return {
        state: root?.querySelector('[data-site-config-state]')?.getAttribute('data-site-config-state') ?? null,
        notice: notice ? notice.innerText.replace(/\s+/g, ' ') : null,
        text: root ? root.innerText.split('\n').filter(Boolean).slice(0, 30) : null,
        enabled: Array.from(root?.querySelectorAll('button') || []).filter(node => !node.disabled).map(node => node.innerText.trim()),
        dangerText: lines.filter(node => getComputedStyle(node).color === danger).map(node => node.innerText.slice(0, 60)),
        diffScrolls: diff ? diff.scrollWidth > diff.clientWidth || diff.scrollHeight > diff.clientHeight : null,
        pageOverflowX: document.documentElement.scrollWidth > window.innerWidth,
        overflowingNodes: overflow,
    };
});

export default function batch10(scenarios, { base, ctl, reset, newPage, closePage, shot, pause, waitFor, clickByText, into }) {
    const setup = async (siteConfig) => {
        await reset();
        await ctl({ clearAll: true });
        await ctl({ domains: [row(1, 'example.com', siteConfig)], capabilities: CAPABILITIES, connection: CONNECTION, ssl: null });
    };
    const openDomain = async () => {
        const page = await newPage();
        await page.goto(`${base}/domains/example.com`, { waitUntil: 'networkidle0' });
        await page.waitForSelector('main h1, main h2', { timeout: 20000 });
        await pause(400);
        return page;
    };
    const openConfig = async (page) => {
        await clickByText(page, ['Hosting', 'Barındırma']);
        await pause(300);
        await clickByText(page, ['Configuration file', 'Yapılandırma dosyası']);
        await waitFor(page, () => !!document.querySelector('[data-site-config] [data-site-config-state], [data-site-config] [role="alert"]'), 15000).catch(() => {});
        await pause(500);
    };

    scenarios.siteconfig = async () => {
        // a: edited by the owner.
        await setup({ state: 'owner_edited' });
        await ctl({ override: { [SC]: { method: 'GET', status: 200, body: EDITED } } });
        let page = await openDomain();
        await shot(page, '100a0-notice-above-domain', await facts(page));
        await clickByText(page, ['Open configuration file', 'Yapılandırma dosyasını aç']);
        await waitFor(page, () => !!document.querySelector('[data-site-config-state]'), 15000).catch(() => {});
        await pause(500);
        await shot(page, '100a1-owner-edited', await facts(page));
        await into(page, '[data-site-config-diff]');
        await shot(page, '100a2-owner-edited-diff', await facts(page));
        await clickByText(page, ['Merge by hand', 'Elle birleştir']);
        await into(page, '[data-site-config-merge-text]');
        await shot(page, '100a3-merge-by-hand', await facts(page));
        await clickByText(page, ['Take CelikPanel’s', 'CelikPanel’in metnini al']);
        await into(page, '[data-site-config-confirm]');
        await shot(page, '100a4-take-confirm', await facts(page));
        await ctl({ override: {
            [`${SC}/take`]: { method: 'POST', status: 200, body: { ...view('managed_unchanged'), outcome: 'taken', backup_path: `${PATH}.celikpanel-backup-20261010T120000Z` } },
            [SC]: { method: 'GET', status: 200, body: view('managed_unchanged') },
        } });
        await clickByText(page, ['Replace the file', 'Dosyayı değiştir']);
        await waitFor(page, () => !!document.querySelector('[data-site-config-done]'), 15000).catch(() => {});
        await pause(500);
        await into(page, '[data-site-config]');
        await shot(page, '100a5-taken', await facts(page));
        await closePage(page);

        const states = {
            '100b-foreign': [{ state: 'foreign' }, view('foreign', { diff: DIFF })],
            '100c-unknown-origin': [{ state: 'unknown_origin' }, view('unknown_origin', { diff: DIFF })],
            '100d-missing': [{ state: 'missing' }, view('absent', { file_sha256: undefined })],
            '100e-unreadable-locked': [{ state: 'unreadable' }, view('unreadable', { reason: 'write_refused' })],
            '100f-managed-adopted': [{ state: 'managed_unchanged', adopted_from: 'v0.1.0-alpha.82' }, view('managed_unchanged', { adopted_from: 'v0.1.0-alpha.82' })],
            '100g-kept-by-choice': [{ state: 'owner_edited', kept_by_choice: true }, { ...EDITED, decision: KEPT_DECISION }],
            '100j-certificate-ready': [{ state: 'owner_edited', kept_by_choice: true, pending_reason: 'certificate' }, CERT_READY],
            '100k-certificate-validation': [{ state: 'unknown_origin', pending_reason: 'certificate_validation' }, CERT_VALIDATION],
            '100l-unreadable-link': [{ state: 'unreadable' }, view('unreadable', { reason: 'symlink' })],
            '100m-unreadable-permission': [{ state: 'unreadable' }, view('unreadable', { reason: 'permission', detail: 'open /etc/nginx/sites-available/example.com.conf: permission denied' })],
            '100n-managed-adopted-creation': [{ state: 'managed_unchanged', adopted_from: 'v0.1.0-alpha.81 (creation)' }, view('managed_unchanged', { adopted_from: 'v0.1.0-alpha.81 (creation)' })],
        };
        // The notices above the domain for a file kept by choice and for a
        // certificate waiting on the file.
        for (const [name, summary] of Object.entries({
            '100g0-notice-kept-by-choice': { state: 'owner_edited', kept_by_choice: true },
            '100j0-notice-certificate': { state: 'owner_edited', kept_by_choice: true, pending_reason: 'certificate' },
        })) {
            await setup(summary);
            await ctl({ override: { [SC]: { method: 'GET', status: 200, body: EDITED } } });
            page = await openDomain();
            await shot(page, name, await facts(page));
            await closePage(page);
        }
        for (const [name, [summary, answer]] of Object.entries(states)) {
            await setup(summary);
            await ctl({ override: { [SC]: { method: 'GET', status: 200, body: answer } } });
            page = await openDomain();
            await openConfig(page);
            await shot(page, name, await facts(page));
            await closePage(page);
        }

        // h: the read failed: could not check, no choice.
        await setup({ state: 'owner_edited' });
        await ctl({ override: { [SC]: { method: 'GET', status: 502, body: { error: 'x', code: 'SITE_CONFIG_NOT_READ' } } } });
        page = await openDomain();
        await openConfig(page);
        await shot(page, '100h-could-not-check', await facts(page));
        await closePage(page);

        // i: the Domains list, the first state after the update.
        await reset();
        await ctl({ clearAll: true });
        await ctl({ domains: [
            row(1, 'example.com', { state: 'managed_unchanged', adopted_from: 'v0.1.0-alpha.82' }),
            row(2, 'shop.example.net', { state: 'managed_unchanged', adopted_from: 'v0.1.0-alpha.82 (creation)' }),
            row(3, 'owner-edited.example.org', { state: 'unknown_origin' }),
            row(4, 'kept.example.org', { state: 'unknown_origin', kept_by_choice: true }),
            row(5, 'removed.example.org', { state: 'missing' }),
            row(6, 'locked.example.org', { state: 'unreadable' }),
        ], capabilities: CAPABILITIES });
        const listFacts = page => page.evaluate(() => ({
            firstState: document.querySelector('[data-site-config-first-state]')?.innerText ?? null,
            badges: Array.from(document.querySelectorAll('[data-site-config-badge]')).map(node => node.innerText),
            pageOverflowX: document.documentElement.scrollWidth > window.innerWidth,
        }));
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'networkidle0' });
        await waitFor(page, () => !!document.querySelector('[data-site-config-first-state]'), 15000).catch(() => {});
        await pause(400);
        await shot(page, '100i-domains-first-state', await listFacts(page));
        await closePage(page);

        // i2: every left-alone file has a decision: the first-state line is gone.
        await ctl({ domains: [
            row(1, 'example.com', { state: 'managed_unchanged', adopted_from: 'v0.1.0-alpha.82' }),
            row(4, 'kept.example.org', { state: 'unknown_origin', kept_by_choice: true }),
        ], capabilities: CAPABILITIES });
        page = await newPage();
        await page.goto(`${base}/domains`, { waitUntil: 'networkidle0' });
        await waitFor(page, () => document.querySelectorAll('tbody tr').length > 0, 15000).catch(() => {});
        await pause(400);
        await shot(page, '100i2-domains-all-decided', await listFacts(page));
        await closePage(page);
        await ctl({ clearAll: true });
    };
}
