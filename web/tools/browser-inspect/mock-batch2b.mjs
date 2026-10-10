// Loopback-only mock routes for the second batch of "no negative UI unless
// known" (9 Oct 2026): the database configuration editors, a domain's mail
// screens, the mail queue and policy, and a domain's scheduled tasks. Loaded by
// mock.mjs; like it, this answers constants and contacts nothing.
//
// Everything a scenario can switch lives in `state.b2`; a scenario sends the
// whole object through /__ctl.
import { createHash } from 'node:crypto';

const PG_CONF = [
    '# -----------------------------',
    '# PostgreSQL configuration file',
    '# -----------------------------',
    '#',
    "# This file is read on server startup and when the server receives a SIGHUP",
    '',
    '#------------------------------------------------------------------------------',
    '# CONNECTIONS AND AUTHENTICATION',
    '#------------------------------------------------------------------------------',
    '',
    "#listen_addresses = 'localhost'\t\t# what IP address(es) to listen on;",
    '\t\t\t\t\t# comma-separated list of addresses;',
    'port = 5432\t\t\t\t# (change requires restart)',
    'max_connections = 100\t\t\t# (change requires restart)',
    '#superuser_reserved_connections = 3\t# (change requires restart)',
    "unix_socket_directories = '/var/run/postgresql'\t# comma-separated list of directories",
    'ssl = on',
    "ssl_cert_file = '/etc/ssl/certs/ssl-cert-snakeoil.pem'",
    '',
    '#------------------------------------------------------------------------------',
    '# RESOURCE USAGE (except WAL)',
    '#------------------------------------------------------------------------------',
    '',
    '   shared_buffers=128MB # tuned by hand, 2025-03',
    '#work_mem = 4MB\t\t\t\t# min 64kB',
    '#maintenance_work_mem = 64MB\t\t# min 64kB',
    'dynamic_shared_memory_type = posix\t# the default is usually the first option',
    '',
    '#------------------------------------------------------------------------------',
    '# REPORTING AND LOGGING',
    '#------------------------------------------------------------------------------',
    '',
    "log_line_prefix = '%m [%p] %q%u@%d '\t\t# special values:",
    "log_timezone = 'Europe/Istanbul'",
    'pg_stat_statements.track = all',
    "include_dir = 'conf.d'\t\t\t# include files ending in '.conf' from",
    "# the owner's note: reviewed with the hosting team in March",
    '',
].join('\n');

const PG_HBA = [
    '# PostgreSQL Client Authentication Configuration File',
    '# ===================================================',
    '#',
    '# DO NOT DISABLE!',
    '# If you change this first entry you will need to make sure that the',
    '# database superuser can access the database using some other method.',
    'local   all             postgres                                peer',
    '',
    '# TYPE  DATABASE        USER            ADDRESS                 METHOD',
    '',
    '# "local" is for Unix domain socket connections only',
    'local   all             all                                     peer',
    '# IPv4 local connections:',
    'host    all             all             127.0.0.1/32            scram-sha-256',
    '# IPv6 local connections:',
    'host    all             all             ::1/128                 scram-sha-256',
    '# the reporting server, added by the owner',
    'host    reporting       reporting_ro    203.0.113.24/32         scram-sha-256',
    'host    all             ldapusers       10.0.0.0/8              ldap ldapserver=ldap.example.net ldapprefix="cn="',
    'include_dir hba.d',
    '',
].join('\n');

const MY_CNF = [
    '#',
    '# These groups are read by MariaDB server.',
    '# tuned by the owner, 2024',
    '',
    '[server]',
    '',
    '[mysqld]',
    'pid-file                = /run/mysqld/mysqld.pid',
    'basedir                 = /usr',
    'bind-address            = 127.0.0.1',
    '#key_buffer_size        = 128M',
    'max_connections=150   # raised for the shop',
    '#skip-name-resolve',
    'character-set-server    = utf8mb4',
    '!includedir /etc/mysql/extra.d/',
    '',
    '[mariadb-11.8]',
    'plugin-load-add = auth_socket',
    '',
].join('\n');

export const b2Defaults = () => ({
    files: {
        '/etc/postgresql/17/main/postgresql.conf': PG_CONF,
        '/etc/postgresql/17/main/pg_hba.conf': PG_HBA,
        '/etc/mysql/mariadb.conf.d/50-server.cnf': MY_CNF,
        '/etc/mysql/mariadb.cnf': '# The MariaDB configuration file\n[client-server]\nsocket = /run/mysqld/mysqld.sock\n!includedir /etc/mysql/mariadb.conf.d/\n',
    },
    // ok | stale | daemon | syntax | lockout | reload | notRestored | dropped
    save: 'ok',
    catchAll: { enabled: true, destination: 'owner@example.net' },
    catchAllSave: 'ok',          // ok | stale
    webmail: true,
    accounts: [
        { id: 5, address: 'info@example.com', quota_mb: 1024 },
        { id: 6, address: 'a-very-long-mailbox-name.for-the-accounting-department@example.com', quota_mb: 2048 },
    ],
    forwardings: [{ id: 7, source: 'sales@example.com', destination: 'team@example.net' }],
    queue: [
        { id: '4F2C1A0B7D', size: '2.0 KB', sender: 'shop@example.com', arrival: '2026-10-09 10:00', status: 'deferred' },
        { id: '9D3E7C11A2', size: '512 B', sender: 'info@example.com', arrival: '2026-10-09 10:02', status: 'active' },
    ],
    queueAction: 'ok',           // ok | fail
    policy: { message_size_mb: 25, dnsbl_zones: ['zen.spamhaus.org'], outbound_rate_limit: 30 },
    // ok | notReloaded (an Agent that names no stage) | check | reload | verify
    // (written, and Postfix was verified not to have taken it, by stage) |
    // unknown (written, outcome not established). From `check` on the answer
    // carries the written policy, as the Panel does since 10 Oct 2026.
    policySave: 'ok',
    policyRunning: true,         // false: Postfix is stopped (applied: not_running / unchanged)
    cron: "# m h  dom mon dow   command\n15 2 * * * /usr/local/bin/report --quiet\n# DISABLED: 0 6 * * 1 /usr/local/bin/weekly\n*/5 * * * * /usr/bin/php /var/www/example.com/cron.php\n",
});

const digest = (text) => createHash('sha256').update(text).digest('hex');
const jobId = (text) => digest(text).slice(0, 16);
const cronJobs = (crontab) => {
    const jobs = [];
    let comment = '';
    for (const line of crontab.split('\n')) {
        const trimmed = line.trim();
        if (!trimmed) continue;
        const disabled = trimmed.startsWith('# DISABLED:');
        const text = disabled ? trimmed.slice('# DISABLED:'.length).trim() : trimmed;
        const fields = text.split(/\s+/);
        if ((trimmed.startsWith('#') && !disabled) || fields.length < 6) { comment = trimmed.startsWith('#') ? trimmed.replace(/^#\s?/, '') : ''; continue; }
        jobs.push({ id: jobId(text), schedule: fields.slice(0, 5).join(' '), command: fields.slice(5).join(' '), enabled: !disabled, comment });
        comment = '';
    }
    return jobs;
};
const catchAllVersion = (value) => 'ca1-' + digest(`${value.enabled}\0${value.destination}`);
const policyVersion = (value) => 'mp1-' + digest(JSON.stringify(value));

// Answers the request when it is one of this batch's routes. Returns false
// when it is not, and mock.mjs goes on to its own 404.
export async function batch2b(req, res, path, query, { state, send, coded, readBody }) {
    const b2 = state.b2 || (state.b2 = b2Defaults());
    const key = `${req.method} ${path}`;
    const refuse = (status, code, reason, vars) => { send(res, status, { error: 'the server sentence (the screen shows its own)', code, reason, ...(vars ? { vars } : {}) }); return true; };

    if (key === 'GET /api/v1/config') {
        const file = query.get('path') || '';
        if (!(file in b2.files)) { coded(res, 403, 'CONFIG_PATH_REFUSED', `not a managed configuration file: ${file}`); return true; }
        send(res, 200, { Content: b2.files[file], Parsed: '', Version: 'cf1-' + digest(b2.files[file]) });
        return true;
    }
    if (key === 'POST /api/v1/config') {
        const body = await readBody(req);
        const file = body.path || '';
        const service = file.includes('mysql') ? 'mariadb' : 'postgresql';
        if (!(file in b2.files)) { coded(res, 403, 'CONFIG_PATH_REFUSED', `not a managed configuration file: ${file}`); return true; }
        if (!body.version) return refuse(409, 'SETTINGS_VERSION_REQUIRED', 'config_file');
        if (b2.save === 'dropped') { req.socket.destroy(); return true; }
        if (b2.save === 'stale' || body.version !== 'cf1-' + digest(b2.files[file])) return refuse(409, 'SETTINGS_CHANGED', 'config_file');
        if (!String(body.content || '').trim()) return refuse(422, 'CONFIG_INVALID', 'empty');
        if (b2.save === 'daemon') {
            // What the two real programs print for a value they cannot use.
            if (service === 'mariadb') return refuse(422, 'CONFIG_INVALID', 'daemon', { detail: "Unknown suffix 'l' used for variable 'max_connections' (value 'lots'). Legal suffix characters are: K, M, G, T, P, E", name: 'max_connections' });
            if (file.endsWith('pg_hba.conf')) return refuse(422, 'CONFIG_INVALID', 'daemon', { detail: 'hostssl record cannot match because SSL is disabled', line: String(String(body.content).split('\n').length - 1) });
            return refuse(422, 'CONFIG_INVALID', 'daemon', { detail: 'invalid value for parameter "max_connections": "lots"', name: 'max_connections' });
        }
        if (b2.save === 'syntax') return refuse(422, 'CONFIG_INVALID', 'syntax', { detail: 'end-of-line before authentication method', line: String(String(body.content).split('\n').length - 1) });
        if (b2.save === 'lockout') return refuse(422, 'CONFIG_INVALID', 'lockout', { name: 'denied' });
        if (b2.save === 'reload') return refuse(502, 'CONFIG_RELOAD_FAILED', 'restored', { detail: 'Error: /usr/lib/postgresql/17/bin/pg_ctl reload: could not send reload signal (PID: 1123): No such process' });
        // The two answers of 10 Oct 2026: the previous file is back, the unit
        // could not reload with it either, and the server was (or could not
        // be) asked directly which settings it runs.
        if (b2.save === 'reloadUnit') return refuse(502, 'CONFIG_RELOAD_FAILED', 'restored_unit_reload_failed', { detail: 'Job for postgresql@17-main.service failed because the control process exited with error code.', unit: 'postgresql@17-main' });
        if (b2.save === 'reloadUnknown') return refuse(502, 'CONFIG_RELOAD_FAILED', 'restored_running_unknown', { detail: 'Job for postgresql@17-main.service failed because the control process exited with error code.', unit: 'postgresql@17-main' });
        if (b2.save === 'notRestored') return refuse(502, 'CONFIG_RELOAD_FAILED', 'not_restored', { detail: 'Job for postgresql@17-main.service failed.', name: `${file}.celikpanel-backup-20261009T120000Z` });
        const unchanged = b2.files[file] === body.content;
        b2.files[file] = body.content;
        send(res, 200, {
            success: true, version: 'cf1-' + digest(body.content), unchanged,
            backup: unchanged ? '' : `${file}.celikpanel-backup-20261009T120000Z`,
            applied: unchanged ? '' : service === 'mariadb' ? 'restart_required' : 'reloaded',
            daemon_check: unchanged ? '' : 'accepted',
            restart_required: !unchanged && file.endsWith('postgresql.conf') ? ['max_connections', 'shared_buffers'] : [],
        });
        return true;
    }

    const mail = path.match(/^\/api\/v1\/domains\/(\d+)\/mail\/([a-z-]+)$/);
    if (mail) {
        const what = mail[2];
        if (req.method === 'GET') {
            if (what === 'setup') {
                const proto = (port, security, extra = {}) => ({ host: 'mail.example.com', port, security, ...extra });
                send(res, 200, { mail_host: 'mail.example.com', imap: proto(993, 'SSL/TLS'), pop3: proto(995, 'SSL/TLS'), smtp: proto(587, 'STARTTLS', { auth_required: true }), username_is_full_email: true, webmail_available: b2.webmail, webmail_url: b2.webmail ? '/webmail/' : '' });
                return true;
            }
            if (what === 'catch-all') { send(res, 200, { enabled: b2.catchAll.enabled, destination: b2.catchAll.destination, version: catchAllVersion(b2.catchAll) }); return true; }
            if (what === 'accounts') { send(res, 200, { accounts: b2.accounts }); return true; }
            if (what === 'forwardings') { send(res, 200, { forwardings: b2.forwardings }); return true; }
            if (what === 'quota') { send(res, 200, { plugin_enabled: true, usages: b2.accounts.map((account, index) => ({ email: account.address, used_kb: 2048 * (index + 1) * 300, limit_kb: account.quota_mb * 1024, available: true })) }); return true; }
            if (what === 'health') { send(res, 200, { overall: 'warn', server_ip: '203.0.113.10', expected_ptr: 'mail.example.com', checks: [{ id: 'mx', status: 'ok' }, { id: 'ptr', status: 'warn', detail: 'no PTR' }, { id: 'spf', status: 'ok' }, { id: 'dkim', status: 'ok' }] }); return true; }
            if (what === 'rbl') { send(res, 200, { ip: '203.0.113.10', results: [{ zone: 'zen.spamhaus.org', listed: false }, { zone: 'bl.spamcop.net', listed: false }] }); return true; }
        }
        if (what === 'catch-all' && (req.method === 'PUT' || req.method === 'DELETE')) {
            const body = req.method === 'PUT' ? await readBody(req) : {};
            const version = req.method === 'PUT' ? body.version : query.get('version');
            if (!version) return refuse(409, 'SETTINGS_VERSION_REQUIRED', 'mail_catch_all');
            if (b2.catchAllSave === 'stale' || version !== catchAllVersion(b2.catchAll)) return refuse(409, 'SETTINGS_CHANGED', 'mail_catch_all');
            if (req.method === 'PUT' && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(body.destination || '')) { send(res, 400, { error: 'invalid email address' }); return true; }
            b2.catchAll = req.method === 'PUT' ? { enabled: true, destination: String(body.destination).toLowerCase() } : { enabled: false, destination: '' };
            send(res, 200, { ...b2.catchAll, version: catchAllVersion(b2.catchAll) });
            return true;
        }
    }

    if (key === 'GET /api/v1/postfix/queue') { send(res, 200, b2.queue); return true; }
    if (key === 'POST /api/v1/postfix/queue') {
        const body = await readBody(req);
        if (b2.queueAction === 'fail') { send(res, 502, { error: 'mail queue action was not confirmed by the agent' }); return true; }
        if (body.action === 'delete_all') b2.queue = [];
        if (body.action === 'delete_id') b2.queue = b2.queue.filter((item) => item.id !== body.id);
        send(res, 200, { success: true });
        return true;
    }
    if (key === 'GET /api/v1/mail/policy') { send(res, 200, { ...b2.policy, version: policyVersion(b2.policy) }); return true; }
    if (key === 'PUT /api/v1/mail/policy') {
        const body = await readBody(req);
        if (!body.version) return refuse(409, 'SETTINGS_VERSION_REQUIRED', 'mail_policy');
        if (body.version !== policyVersion(b2.policy)) return refuse(409, 'SETTINGS_CHANGED', 'mail_policy');
        const next = { message_size_mb: body.message_size_mb, dnsbl_zones: body.dnsbl_zones || [], outbound_rate_limit: body.outbound_rate_limit };
        // A save that changes nothing writes nothing; a running Postfix is
        // reloaded all the same (10 Oct 2026), so it can fail the same ways.
        const wrote = JSON.stringify(next) !== JSON.stringify(b2.policy);
        b2.policy = next;
        if (b2.policySave === 'notReloaded') {
            send(res, 502, { error: 'written, not reloaded', code: 'MAIL_POLICY_NOT_RELOADED', partial_success: true, mutation_applied: true, vars: { detail: 'exit status 1: Job for postfix.service failed because the control process exited with error code.' } });
            return true;
        }
        const said = {
            check: 'postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign',
            reload: 'postfix/postfix-script: fatal: the Postfix mail system is not running',
            verify: 'the Postfix master process stopped while it was reloading',
            unknown: 'fork/exec /usr/sbin/postfix: resource temporarily unavailable',
        }[b2.policySave];
        if (said) {
            send(res, 502, {
                error: 'the server sentence (the screen shows its own)',
                code: b2.policySave === 'unknown' ? 'MAIL_POLICY_RELOAD_UNKNOWN' : 'MAIL_POLICY_NOT_RELOADED',
                ...(b2.policySave === 'unknown' ? {} : { reason: b2.policySave }),
                ...(wrote ? { partial_success: true, mutation_applied: true } : {}),
                vars: { detail: said },
                policy: { ...b2.policy, version: policyVersion(b2.policy) },
            });
            return true;
        }
        const applied = b2.policyRunning === false ? (wrote ? 'not_running' : 'unchanged') : wrote ? 'reloaded' : 'unchanged_reloaded';
        send(res, 200, { success: true, applied, policy: { ...b2.policy, version: policyVersion(b2.policy) } });
        return true;
    }

    const cron = path.match(/^\/api\/v1\/domains\/(\d+)\/cron$/);
    if (cron) {
        const version = 'ct1-' + digest(b2.cron);
        if (req.method === 'GET') { send(res, 200, { jobs: cronJobs(b2.cron), version }); return true; }
        const body = req.method === 'DELETE' ? { id: query.get('id'), version: query.get('version') } : await readBody(req);
        if (!body.version) return refuse(409, 'SETTINGS_VERSION_REQUIRED', 'scheduled_tasks');
        if (body.version !== version) return refuse(409, 'SETTINGS_CHANGED', 'scheduled_tasks');
        // The same rule as the Agent: a job is the one line whose own text is
        // the ID, enabled or disabled, and only that line changes.
        const lines = b2.cron.split('\n');
        const textOf = (line) => { const trimmed = line.trim(); return trimmed.startsWith('# DISABLED:') ? trimmed.slice('# DISABLED:'.length).trim() : trimmed; };
        const at = lines.findIndex((line) => line.trim() && (!line.trim().startsWith('#') || line.trim().startsWith('# DISABLED:')) && jobId(textOf(line)) === body.id);
        if (req.method === 'POST') lines.splice(lines.length - 1, 0, ...(body.comment ? [`# ${body.comment}`] : []), `${body.schedule} ${body.command}`);
        else if (at < 0) { send(res, 500, { error: 'An internal error occurred.', code: 'INTERNAL' }); return true; }
        else if (req.method === 'DELETE') lines.splice(at, 1);
        else lines[at] = `${body.enabled ? '' : '# DISABLED: '}${body.schedule} ${body.command}`;
        b2.cron = lines.join('\n');
        send(res, 200, { success: true });
        return true;
    }
    return false;
}
