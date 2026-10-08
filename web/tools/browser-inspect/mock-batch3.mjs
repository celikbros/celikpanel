// Loopback-only mock routes for the third batch of "no negative UI unless
// known" (9 Oct 2026): the Fail2ban, Nginx, PHP, Dovecot and PowerDNS pages,
// the dashboard's attention list, the DNS settings and the DNS engine card's
// stalled change. Loaded by mock.mjs; like it, this answers constants and
// contacts nothing.
//
// Everything a scenario can switch lives in `state.b3`; a scenario sends the
// whole object through /__ctl. Without it the defaults below answer, so the
// scenarios of the earlier batches see a server whose firewall is on.
const OP_ID = 'a'.repeat(32);
const REQUEST_ID = 'b'.repeat(32);
const minutesAgo = (n) => new Date(Date.now() - n * 60_000).toISOString();

export const b3Defaults = () => ({
    firewall: { enabled: true, engine_available: true, tcp_ports: [22, 80, 443, 2083], udp_ports: [], ssh_ports: [22], persistence_state: 'ready' },
    dovecot: { uptime: '3 days', connections: 4, logins: 0, auth_success: 0, auth_fail: 0 },
    jails: [{ name: 'sshd', enabled: true, active: true, banned: 2 }, { name: 'postfix-sasl', enabled: true, active: false, banned: 0 }],
    banned: [{ ip: '198.51.100.23', jail: 'sshd', time: '2026-10-09 08:12:44', country: '' }, { ip: '2001:db8:85a3::8a2e:370:7334', jail: 'sshd', time: '2026-10-09 08:40:02', country: '' }],
    unban: 'ok',           // ok | refused
    f2bConfig: { ban_time: '10m', find_time: '10m', max_retry: 5, ignore_ip: ['127.0.0.1/8', '::1'] },
    nginxGlobal: { worker_processes: 'auto', worker_connections: '768', keepalive_timeout: '65', client_max_body_size: '64m', server_tokens: 'off', gzip: 'on' },
    nginxSSL: { ssl_protocols: 'TLSv1.2 TLSv1.3', ssl_ciphers: 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384', ssl_prefer_server_ciphers: 'off' },
    rateLimits: [{ name: 'login', zone: '$binary_remote_addr', size: '10m', rate: '5r/m' }],
    extensions: [{ name: 'curl', enabled: true }, { name: 'gd', enabled: true }, { name: 'intl', enabled: false }, { name: 'mbstring', enabled: true }, { name: 'opcache', enabled: true }, { name: 'pdo_mysql', enabled: true }],
    toggle: 'ok',          // ok | refused
    phpIni: { memory_limit: '256M', max_execution_time: '30', max_input_time: '60', post_max_size: '64M', upload_max_filesize: '64M', opcache_enable: 'On', disable_functions: '', include_path: '.', session_save_path: '/var/lib/php/sessions', mail_force_extra_parameters: '', open_basedir: '', error_reporting: 'E_ALL & ~E_DEPRECATED', display_errors: 'Off', log_errors: 'On', allow_url_fopen: 'On', file_uploads: 'On', short_open_tag: 'Off', additional_directives: '' },
    nameservers: { ns1: 'ns1.example.com', ns2: 'ns2.example.com', derived: false, server_ip: '203.0.113.10', facts: [], usable: true },
    cluster: { configured: true, role: 'standalone', peer_ip: '', peer_ns: '', peer_reachable: false, server_ip: '203.0.113.10', ns1: 'ns1.example.com', ns2: 'ns2.example.com', facts: [], steps: [], dns_service_known: true, dns_service_ready: true },
    // null: no DNS engine change is being tracked. 'stalled': an accepted
    // change whose last recorded step is four minutes old. 'overdue': the
    // same, started forty minutes ago, past the card's safety limit of 31.
    dnsChange: null,
    reconcile: 'finishes', // finishes | unchanged | refused
    reconciles: 0,         // how many reconcile requests arrived
});

const engines = (active) => [
    { id: 'pdns', installed: true, running: active === 'pdns', managed: true, status: active === 'pdns' ? 'active' : 'installed_standby' },
    { id: 'bind', installed: true, running: active === 'bind', managed: true, status: active === 'bind' ? 'active' : 'installed_standby' },
];
const dnsEngine = (b3) => {
    const base = { revision: 5, topology: 'standalone', dnssec_zone_count: 0, zone_count: 2, pending_zone_count: 0 };
    if (b3.dnsChange === 'stalled' || b3.dnsChange === 'overdue') {
        const [started, updated] = b3.dnsChange === 'overdue' ? [40, 39] : [5, 4];
        return { ...base, engine_epoch: 1, active_engine: 'bind', state: 'switching', engines: engines('bind'), operation_id: OP_ID, operation: { id: OP_ID, request_id: REQUEST_ID, target_engine: 'pdns', phase: 'activating', status: 'running', started_at: minutesAgo(started), updated_at: minutesAgo(updated) } };
    }
    if (b3.dnsChange === 'finished') {
        return { ...base, revision: 6, engine_epoch: 2, active_engine: 'pdns', state: 'ready', engines: engines('pdns'), operation: { id: OP_ID, request_id: REQUEST_ID, target_engine: 'pdns', phase: 'committed', status: 'succeeded', started_at: minutesAgo(5), updated_at: minutesAgo(0) } };
    }
    return { ...base, engine_epoch: 1, active_engine: 'bind', state: 'ready', engines: engines('bind') };
};

export async function batch3(req, res, path, query, { state, send, coded, readBody }) {
    const b3 = state.b3 || (state.b3 = b3Defaults());
    const key = `${req.method} ${path}`;
    const answers = {
        'GET /api/v1/firewall': () => b3.firewall,
        'GET /api/v1/dovecot/stats': () => b3.dovecot,
        'GET /api/v1/fail2ban/jails': () => b3.jails,
        'GET /api/v1/fail2ban/banned': () => b3.banned,
        'GET /api/v1/fail2ban/config': () => b3.f2bConfig,
        'GET /api/v1/nginx/global': () => b3.nginxGlobal,
        'GET /api/v1/nginx/ssl': () => b3.nginxSSL,
        'GET /api/v1/nginx/ratelimits': () => b3.rateLimits,
        'GET /api/v1/php/extensions': () => b3.extensions,
        'GET /api/v1/php/extended-config': () => b3.phpIni,
        'GET /api/v1/settings/nameservers': () => b3.nameservers,
        'GET /api/v1/settings/dns-cluster': () => b3.cluster,
        'GET /api/v1/dns/engine': () => dnsEngine(b3),
        'GET /api/v1/audit-logs': () => ({ entries: [] }),
        'GET /api/v1/system/stats': () => ({ hostname: 'server1', uptime_seconds: 86400 * 3, cpu_percent: 7, cpu_cores: 4, load_avg: [0.12, 0.2, 0.18], mem_used_bytes: 2 * 2 ** 30, mem_total_bytes: 8 * 2 ** 30, disk_used_bytes: 20 * 2 ** 30, disk_total_bytes: 80 * 2 ** 30 }),
    };
    if (answers[key]) { send(res, 200, answers[key]()); return true; }

    if (key === 'POST /api/v1/firewall') {
        const body = await readBody(req);
        if (typeof body.enabled === 'boolean') b3.firewall = { ...b3.firewall, enabled: body.enabled, persistence_state: 'ready' };
        send(res, 200, b3.firewall);
        return true;
    }
    if (key === 'POST /api/v1/fail2ban/banned') {
        const body = await readBody(req);
        if (b3.unban === 'refused') { coded(res, 502, 'INTERNAL', 'An internal error occurred.'); return true; }
        b3.banned = b3.banned.filter((row) => !(row.ip === body.ip && row.jail === body.jail));
        send(res, 200, { success: true });
        return true;
    }
    if (key === 'POST /api/v1/php/extensions') {
        const body = await readBody(req);
        if (b3.toggle === 'refused') { coded(res, 502, 'INTERNAL', 'An internal error occurred.'); return true; }
        b3.extensions = b3.extensions.map((row) => (row.name === body.extension ? { ...row, enabled: body.enabled === true } : row));
        send(res, 200, { success: true });
        return true;
    }
    // The reconcile request of the DNS engine card. The mock counts every one
    // that arrives: the scenario's whole point is who sent it.
    if (key === 'POST /api/v1/dns/engine/reconcile') {
        b3.reconciles += 1;
        if (b3.reconcile === 'refused') { coded(res, 409, 'DNS_ENGINE_STATE_UNVERIFIED', 'DNS engine state could not be verified.'); return true; }
        if (b3.reconcile === 'finishes') b3.dnsChange = 'finished';
        send(res, 200, { reconciled: b3.reconcile === 'finishes' });
        return true;
    }
    if (key === 'GET /api/v1/__b3') { send(res, 200, { reconciles: b3.reconciles }); return true; }
    return false;
}
