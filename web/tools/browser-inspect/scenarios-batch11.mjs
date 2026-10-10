// Batch 11 (2026-10-10, second round of D-031 step 1b and the session wait):
//
//   a83ssl       the domain's SSL/TLS tab for a certificate waiting on the site's
//                configuration file, in both causes (a new certificate the file
//                does not use yet; a request the file stopped), and an expired
//                certificate in use that also waits (the failure is shown).
//   a83dash      the dashboard's attention list with a waiting certificate whose
//                certificate in use has expired, before one with days left.
//   a83siteconfig the Configuration file page: the probe's answer named, an
//                unanswered probe (unknown), and a read whose probe found the
//                file ready again.
//   a83session   the recovery page after a read answered (the Panel starting; the
//                session read refused with 503), then every read held: the wait
//                at the re-read's own limit with "Check now", the reload 15 s
//                later, an earlier failure only as "last known".
//
// Like the batches before: the mock on 127.0.0.1 answers everything; nothing
// else is contacted, nothing is started.
// On birinci grup.
import { mkdir } from 'node:fs/promises';

const D = '/api/v1/domains/1';
const SC = `${D}/site-config`;
const PATH = '/etc/nginx/sites-available/example.com.conf';
const MANAGED = '/etc/nginx/celikpanel-managed.d/example.com';
const CAPABILITIES = { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3'], mail_server: true, database_servers: ['mariadb'], db_tools: [] };
const CONNECTION = { domain: 'example.com', server_ip: '192.0.2.4', nameservers: ['ns1.example.net', 'ns2.example.net'], live_nameservers: ['ns1.example.net', 'ns2.example.net'], live_ips: ['192.0.2.4'], status: 'delegated', ssl_ready: true, glue_needed: false, nameservers_usable: true, propagation_pending: false, checked_at: '2026-10-10T09:00:00Z', dns_management_mode: 'local' };
const row = (siteConfig) => ({ id: 1, domain_name: 'example.com', status: 'active', project_type: 'php', php_version: '8.3', ssl_enabled: true, created_at: '2026-09-01T10:00:00Z', disk_usage: 48234496, bandwidth: 0, ...(siteConfig ? { site_config: siteConfig } : {}) });
const certificate = (more) => ({
    id: 3, domain_id: 1, type: 'letsencrypt', cert_path: '/var/lib/celikpanel-agent/certificates/example.com/fullchain.pem',
    key_path: '/var/lib/celikpanel-agent/certificates/example.com/privkey.pem', issuer: 'Let’s Encrypt', subject: 'example.com',
    issued_at: '2026-10-10T08:00:00Z', expires_at: '2027-01-08T08:00:00Z', days_until_expiry: 89, auto_renew: true,
    renewal_status: 'waiting_for_owner', status: 'active', dns_names: ['example.com', 'www.example.com'], activated: true, usable: true,
    trust_status: 'trusted', activation_pending: false, dependents_pending: false, waiting_for_owner: true, ...more,
});
const ssl = (cert) => ({ domain_id: 1, domain_name: 'example.com', has_certificate: true, managed_names: ['example.com', 'www.example.com'], certificate: cert, settings: { force_https: false, hsts_enabled: false, hsts_max_age: 300 } });
const view = (more) => ({
    domain_id: 1, domain: 'example.com', kind: 'nginx_vhost', path: PATH, state: 'unknown_origin',
    include_dir: '/etc/nginx/celikpanel-sites.d/example.com', managed_dir: MANAGED, managed_include: `include ${MANAGED}/*.conf;`,
    file_sha256: 'c'.repeat(64), render_sha256: 'b'.repeat(64), actions: ['keep', 'take', 'merge'],
    diff: `--- ${PATH} (on this server)\n+++ CelikPanel's text\n@@ -1,3 +1,4 @@\n server {\n     listen 80;\n+    include ${MANAGED}/*.conf;\n`,
    ...more,
});
const WAITING = { pending_reason: 'certificate_validation', certificate: { served_expires_at: '2026-10-19T00:00:00Z', served_days_left: 8, referenced: false } };

const overflowFacts = page => page.evaluate(() => ({
    pageOverflowX: document.documentElement.scrollWidth > window.innerWidth,
    main: (document.querySelector('main')?.innerText || '').split('\n').filter(Boolean).slice(0, 40),
}));

export default function batch11(scenarios, { base, locale, ctl, reset, newPage, closePage, shot, pause, waitFor, clickByText, into }) {
    const setup = async (siteConfig, extra = {}) => {
        await reset();
        await ctl({ clearAll: true });
        await ctl({ domains: [row(siteConfig)], capabilities: CAPABILITIES, connection: CONNECTION, ssl: null, ...extra });
    };
    const openDomain = async () => {
        const page = await newPage();
        await page.goto(`${base}/domains/example.com`, { waitUntil: 'networkidle0' });
        await page.waitForSelector('main h1, main h2', { timeout: 20000 });
        await pause(400);
        return page;
    };
    const openSub = async (page, tab, sub) => {
        await clickByText(page, tab);
        await pause(300);
        await clickByText(page, sub);
        await pause(800);
    };

    scenarios.a83ssl = async () => {
        for (const [name, cert] of [
            ['110a-ssl-waiting-new-certificate', certificate({ waiting_for_owner_reason: 'certificate' })],
            ['110b-ssl-waiting-request-stopped', certificate({ waiting_for_owner_reason: 'certificate_validation', days_until_expiry: 8, expires_at: '2026-10-19T00:00:00Z' })],
            ['110c-ssl-expired-outranks-waiting', certificate({ waiting_for_owner_reason: 'certificate_validation', days_until_expiry: -3, expires_at: '2026-10-07T00:00:00Z' })],
        ]) {
            await setup({ state: 'unknown_origin', pending_reason: cert.waiting_for_owner_reason }, { ssl: ssl(cert) });
            const page = await openDomain();
            await openSub(page, ['Hosting', 'Barındırma'], ['SSL/TLS']);
            await waitFor(page, () => !!document.querySelector('[data-ssl-waiting-for-owner]') || /expired|doldu/i.test(document.querySelector('main')?.innerText || ''), 15000).catch(() => {});
            await into(page, '[data-ssl-waiting-for-owner]');
            await shot(page, name, { ...(await overflowFacts(page)), waiting: await page.evaluate(() => document.querySelector('[data-ssl-waiting-for-owner]')?.getAttribute('data-ssl-waiting-for-owner') ?? null) });
            await closePage(page);
        }
        await ctl({ clearAll: true });
    };

    scenarios.a83dash = async () => {
        await setup(null, { dashboard: { databases: 1, mail_accounts: 2, expiring_certs: [
            { domain_name: 'expired.example', days_left: 80, waiting_for_owner: true, served_days_left: -4, domain_id: 2 },
            { domain_name: 'example.com', days_left: 85, waiting_for_owner: true, served_days_left: 6, domain_id: 1 },
            { domain_name: 'shop.example.net', days_left: 12 },
        ] } });
        const page = await newPage();
        await page.goto(`${base}/`, { waitUntil: 'networkidle0' });
        await waitFor(page, () => /expired\.example/.test(document.querySelector('main')?.innerText || ''), 20000).catch(() => {});
        await pause(500);
        await page.evaluate(() => Array.from(document.querySelectorAll('main *')).find(node => node.children.length === 0 && /expired\.example/.test(node.textContent || ''))?.scrollIntoView({ block: 'center' }));
        await pause(300);
        await shot(page, '111a-dashboard-expired-first', await overflowFacts(page));
        await closePage(page);
        await ctl({ clearAll: true });
    };

    scenarios.a83siteconfig = async () => {
        for (const [name, answer] of [
            ['112a-siteconfig-probe-answer', view({ ...WAITING, validation: 'include_missing', validation_name: 'www.example.com', validation_status: 301 })],
            ['112b-siteconfig-probe-unknown', view({ ...WAITING, validation: 'unknown' })],
            ['112c-siteconfig-ready-again', view({ validation: 'ready', resolved_reason: 'certificate_validation' })],
        ]) {
            await setup({ state: 'unknown_origin', pending_reason: 'certificate_validation' });
            await ctl({ override: { [SC]: { method: 'GET', status: 200, body: answer } } });
            const page = await openDomain();
            if (name === '112a-siteconfig-probe-answer') await shot(page, '112a0-notice-validation', await overflowFacts(page));
            await openSub(page, ['Hosting', 'Barındırma'], ['Configuration file', 'Yapılandırma dosyası']);
            await waitFor(page, () => !!document.querySelector('[data-site-config-certificate]'), 15000).catch(() => {});
            await into(page, '[data-site-config-certificate]');
            await shot(page, name, await overflowFacts(page));
            await closePage(page);
        }
        await ctl({ clearAll: true });
    };

    // The page's own clock: wait until performance.now() reaches ms, then record.
    const at = async (page, ms) => {
        for (;;) {
            const now = await page.evaluate(() => performance.now());
            if (now >= ms) return;
            await pause(Math.min(ms - now, 1000));
        }
    };
    const sample = page => page.evaluate(() => ({
        t: Math.round(performance.now()),
        title: document.querySelector('h1')?.innerText ?? null,
        text: Array.from(document.querySelectorAll('main p')).map(node => node.innerText),
        buttons: Array.from(document.querySelectorAll('main button')).map(node => ({ label: node.innerText.trim(), disabled: node.disabled })),
        quiet: !!document.querySelector('[data-access-quiet]'),
        lastKnown: document.querySelector('[data-access-last-known]')?.getAttribute('data-access-last-known') ?? null,
        pageOverflowX: document.documentElement.scrollWidth > window.innerWidth,
    }));

    scenarios.a83session = async () => {
        await mkdir(`${process.env.BROWSER_INSPECT_OUT || '.'}`, { recursive: true }).catch(() => {});
        for (const [name, first, held] of [
            ['113a-session-after-starting', { '/api/v1/panel/availability': { status: 200, body: { schema: 'celikpanel-panel-availability/v1', state: 'starting' } } }, '/api/v1/panel/availability'],
            ['113b-session-after-failure', { '/api/v1/auth/me': { status: 503, body: { error: 'unavailable' } } }, '/api/v1/auth/me'],
        ]) {
            await reset();
            await ctl({ clearAll: true, session: true, setupStatus: 'ready', license: 'active' });
            await ctl({ override: first });
            const page = await newPage();
            await page.goto(`${base}/`, { waitUntil: 'domcontentloaded', timeout: 15000 }).catch(() => {});
            await at(page, 4000);
            await shot(page, `${name}-01-answered`, await sample(page));
            // Every later read of that kind is held: the re-read at about 10 s reaches its own limit at about 25 s.
            await ctl({ override: { [held]: { hang: true } } });
            await at(page, 26500);
            await shot(page, `${name}-02-limit-26s`, await sample(page));
            await at(page, 41000);
            await shot(page, `${name}-03-prolonged-41s`, await sample(page));
            await closePage(page);
            await ctl({ clearAll: true });
        }
    };
}
