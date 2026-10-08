// Loopback-only mock routes for the fourth batch of "no negative UI unless
// known" (9 Oct 2026): the panels of one domain. DNS records and signing,
// hosting type and its live application, PHP, the general settings, the
// applications, mail authentication, the logs and the backups. Loaded by
// mock.mjs; like it, this answers constants and contacts nothing.
//
// Everything a scenario can switch lives in `state.b4`; a scenario sends the
// whole object through /__ctl. A read is made slow or failing with the
// overrides of mock.mjs, which are per address. A CHANGE whose answer is lost
// is set here, because it must not touch the read of the same address:
// `lose` names one "METHOD path"; when it arrives the mock makes the change
// (or not: `loseApplied`), closes the connection without answering, and
// counts it in `sent`. `loseAs: 'gateway'` answers a gateway's 502 page in
// its place.
const authRecord = (status, name) => ({ name, recommended: name.startsWith('_dmarc') ? 'v=DMARC1; p=none; rua=mailto:postmaster@example.com' : 'v=spf1 mx a:mail.example.com -all', zone_value: '', dns_value: '', resolved: true, status });

export const b4Defaults = () => ({
    zone: { type: 'NATIVE', management: 'local' }, // null: the server answers 404, this domain has no zone
    records: [
        { id: 1, name: 'example.com', type: 'SOA', content: 'ns1.example.net. hostmaster.example.com. 2026100901 10800 3600 604800 3600', ttl: 3600, disabled: false },
        { id: 2, name: 'example.com', type: 'NS', content: 'ns1.example.net', ttl: 3600, disabled: false },
        { id: 3, name: 'example.com', type: 'A', content: '192.0.2.4', ttl: 3600, disabled: false },
        { id: 4, name: 'www.example.com', type: 'CNAME', content: 'example.com', ttl: 3600, disabled: false },
        { id: 5, name: 'example.com', type: 'MX', content: 'mail.example.com', ttl: 3600, prio: 10, disabled: false },
        { id: 6, name: 'selector1._domainkey.example.com', type: 'TXT', content: 'v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6', ttl: 3600, disabled: false },
    ],
    dnssec: { secured: false, ds: null },
    hosting: { project_type: 'php', php_version: '8.3', document_root: '/home/site1/public_html' },
    nodeRuntimes: ['22.3.0', '20.11.1'],
    app: { exists: true, active: 'active', pid: 4242, memory_mb: 64 },
    appLogs: ['2026-10-09T09:00:01Z listening on 3001', '2026-10-09T09:00:04Z GET / 200 12ms'],
    php: { domain_id: 1, domain_name: 'example.com', php_version: '8.3', available_versions: ['8.3', '8.2'], pool_name: 'example_com', pool_config: { name: 'example_com', pm: 'ondemand', pm_max_children: 9, pm_start_servers: 3, pm_min_spare_servers: 2, pm_max_spare_servers: 4, user: 'site1', group: 'site1' } },
    general: { domain_id: 1, domain_name: 'example.com', document_root: '/home/site1/domains/example.com/public_html', web_server: 'nginx', redirect_www: true, redirect_www_available: true, redirect_https: true, aliases: ['shop.example.net', 'a-rather-long-alias-of-this-domain.example.org'] },
    apps: [
        { id: 'wordpress', name: 'WordPress', description: 'The publishing platform, with its own database.', icon: '', requires_db: true, requires_php: true },
        { id: 'nextcloud', name: 'Nextcloud', description: 'Files, calendars and contacts on your own server.', icon: '', requires_db: true, requires_php: true },
    ],
    mailAuth: { domain: 'example.com', zone_exists: true, dns_management_mode: 'local', spf: authRecord('ok', 'example.com'), dkim: { ...authRecord('pending', 'default._domainkey.example.com'), recommended: 'v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA1b2c3d4e5f6a7b8c9d0e1f' }, dmarc: authRecord('missing', '_dmarc.example.com'), dkim_selector: 'default', signing_installed: true },
    logs: [
        '198.51.100.23 - - [09/Oct/2026:09:00:01 +0000] "GET / HTTP/2.0" 200 5123 "-" "Mozilla/5.0"',
        '198.51.100.23 - - [09/Oct/2026:09:00:02 +0000] "GET /assets/app.css HTTP/2.0" 200 18211 "https://example.com/" "Mozilla/5.0"',
        '2001:db8:85a3::8a2e:370:7334 - - [09/Oct/2026:09:00:09 +0000] "POST /wp-login.php HTTP/2.0" 403 153 "-" "curl/8.5.0"',
    ],
    backups: [
        { name: 'example.com-files-20261008-020000.tar.gz', size: 48234496, type: 'files', origin: 'scheduled', legacy: false, restorable: true, created_at: '2026-10-08T02:00:00Z' },
        { name: 'example.com-database-shop-20261007-101500.sql.gz', size: 1048576, type: 'database', origin: 'manual', database_id: 3, legacy: false, restorable: true, created_at: '2026-10-07T10:15:00Z' },
    ],
    databases: { databases: [{ id: 3, name: 'shop', type: 'mariadb', user: 'shop', created_at: '2026-09-01T10:00:00Z' }], available_types: ['mysql'] },
    lose: '',           // 'METHOD path' of the change whose answer does not arrive
    loseAs: 'drop',     // drop | gateway
    loseApplied: true,  // whether the server made the change it did not answer
    sent: {},           // how many times each change arrived, by 'METHOD path'
});

export async function batch4(req, res, path, query, { state, send, coded, readBody }) {
    if (!state.b4) return false;
    const b4 = state.b4;
    const key = `${req.method} ${path}`;
    if (key === 'GET /api/v1/__b4') { send(res, 200, { sent: b4.sent }); return true; }
    const of = path.match(/^\/api\/v1\/domains\/1\/(.+)$/);
    const sub = of ? of[1] : null;
    const reads = {
        'GET /api/v1/apps': () => ({ apps: b4.apps }),
        'GET /api/v1/runtimes/node': () => ({ installed: b4.nodeRuntimes }),
    };
    if (reads[key]) { send(res, 200, reads[key]()); return true; }
    if (!sub) return false;

    if (req.method === 'GET') {
        if (sub === 'dns/zone') { if (b4.zone) send(res, 200, b4.zone); else coded(res, 404, 'not_found', 'DNS zone not found'); return true; }
        if (sub === 'app/status' || sub === 'app/logs') {
            if (b4.hosting.project_type !== 'node') { coded(res, 409, 'conflict', 'this domain is not a node project'); return true; }
            send(res, 200, sub === 'app/status' ? b4.app : { lines: b4.appLogs });
            return true;
        }
        const answers = {
            'dns/records': () => ({ records: b4.records }),
            dnssec: () => b4.dnssec,
            hosting: () => b4.hosting,
            php: () => b4.php,
            general: () => b4.general,
            'mail/auth': () => b4.mailAuth,
            backups: () => ({ backups: b4.backups }),
            databases: () => b4.databases,
            'backups/schedule': () => ({ enabled: false, version: 'bs1' }),
        };
        if (answers[sub]) { send(res, 200, answers[sub]()); return true; }
        if (/^logs\/(access|error|php)$/.test(sub)) {
            const lines = sub === 'logs/access' ? b4.logs : [];
            send(res, 200, { success: true, lines, total: lines.length, log_path: `/var/log/nginx/example.com.${sub.slice(5)}.log` });
            return true;
        }
        return false;
    }

    // A change. It is counted, made (unless it is the lost one and
    // `loseApplied` is off) and then answered, or not answered.
    const body = await readBody(req);
    b4.sent[key] = (b4.sent[key] || 0) + 1;
    const lost = b4.lose === key;
    const make = !lost || b4.loseApplied;
    const changes = {
        'POST dns/records': () => { b4.records = [...b4.records, { id: 100 + b4.records.length, name: body.name === '@' ? 'example.com' : `${body.name}.example.com`, type: body.type, content: body.content, ttl: body.ttl, disabled: false }]; },
        'DELETE dns/records': () => { b4.records = b4.records.filter((record) => String(record.id) !== query.get('id')); },
        'POST dns/zone': () => { b4.zone = b4.zone || { type: 'NATIVE', management: 'local' }; },
        'POST dnssec': () => { b4.dnssec = { secured: true, ds: ['12345 13 2 49FD46E6C4B45C55D4AC49FD46E6C4B45C55D4AC49FD46E6C4B45C55D4AC1234'] }; },
        'PUT hosting': () => { b4.hosting = { ...b4.hosting, ...body }; },
        'POST app/stop': () => { b4.app = { ...b4.app, active: 'inactive', pid: 0, memory_mb: 0 }; },
        'POST app/start': () => { b4.app = { ...b4.app, active: 'active', pid: 4300, memory_mb: 61 }; },
        'POST app/restart': () => { b4.app = { ...b4.app, active: 'active', pid: 4301, memory_mb: 60 }; },
        'POST php': () => { b4.php = { ...b4.php, php_version: body.php_version }; },
        'POST php/pool': () => { b4.php = { ...b4.php, pool_config: { ...b4.php.pool_config, ...body.pool_config } }; },
        'POST general': () => { b4.general = { ...b4.general, redirect_www: body.redirect_www === true }; },
        'POST aliases': () => { b4.general = { ...b4.general, aliases: [...b4.general.aliases, body.alias] }; },
        'POST apps/install': () => {},
        'POST mail/auth/apply': () => { b4.mailAuth = { ...b4.mailAuth, [body.record]: { ...b4.mailAuth[body.record], status: 'pending' } }; },
        'POST mail/auth/dkim': () => {},
        'DELETE logs/access': () => { b4.logs = []; },
        'POST backups': () => { b4.backups = [{ name: `example.com-${body.type}-20261009-091500.tar.gz`, size: 4096, type: body.type, origin: 'manual', legacy: false, restorable: true, created_at: '2026-10-09T09:15:00Z' }, ...b4.backups]; },
        'POST backups/restore': () => {},
        'DELETE backups': () => { b4.backups = b4.backups.filter((backup) => backup.name !== query.get('name')); },
    };
    const alias = sub.match(/^aliases\/(.+)$/);
    const change = alias && req.method === 'DELETE'
        ? () => { b4.general = { ...b4.general, aliases: b4.general.aliases.filter((item) => item !== decodeURIComponent(alias[1])) }; }
        : changes[`${req.method} ${sub}`];
    if (!change) return false;
    if (make) change();
    if (lost) {
        if (b4.loseAs === 'gateway') { res.writeHead(502, { 'Content-Type': 'text/html' }); res.end('<html><body><h1>502 Bad Gateway</h1></body></html>'); return true; }
        // The connection ends with bytes that are not an answer. A connection
        // that is only reset is not used here: when it had carried an earlier
        // request, Chrome sends the request again by itself, change or not
        // (seen 9 Oct 2026: one click, three arrivals), and that is the
        // browser's doing, not the page's. What the page does with no answer
        // is the same either way.
        req.socket.end('connection lost\r\n\r\n');
        return true;
    }
    send(res, 200, sub === 'apps/install' ? { success: true, setup_url: 'https://example.com/wp-admin/install.php' } : sub === 'dns/zone' ? { created: false } : { success: true });
    return true;
}
