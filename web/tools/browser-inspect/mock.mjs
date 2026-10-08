// Loopback-only mock of the CelikPanel API for browser inspection.
// Serves a built SPA (web/dist, or the directory in BROWSER_INSPECT_DIST) and
// answers the routes the inspected screens read. It listens on 127.0.0.1 only
// and contacts no other host: no installed server, no license service.
// node mock.mjs [port]
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { join, extname } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = process.env.BROWSER_INSPECT_DIST || fileURLToPath(new URL('../../dist/', import.meta.url));
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2', '.woff': 'font/woff', '.svg': 'image/svg+xml', '.json': 'application/json', '.png': 'image/png', '.ico': 'image/x-icon' };

const HOST = 'panel.example.com';
const PLAN_ID = 'a'.repeat(32);
const EXEC_ID = 'b'.repeat(32);
const user = { username: 'admin', role: 'admin', effective_role: 'admin', account_type: 'account', email: 'owner@example.com', impersonating: false, features: { team_members: false } };
const draft = { purpose: 'web', panel_domain: HOST, mail_hostname: '', dns_mode: 'external', remote_dns_connection_id: '', dns_engine: 'bind', dns_role: 'primary', ns1: '', ns2: '', local_ip: '', peer_ip: '', peer_ns: '', node_version: '', database: 'mariadb' };
const planSteps = [
    { id: 'service-nginx', kind: 'service', target: 'nginx' },
    { id: 'service-php-fpm', kind: 'service', target: 'php-fpm' },
    { id: 'service-mariadb', kind: 'service', target: 'mariadb' },
    { id: 'service-certbot', kind: 'service', target: 'certbot' },
    { id: 'firewall', kind: 'firewall', target: 'nftables' },
    { id: 'panel-certificate', kind: 'panel_certificate', target: HOST },
    { id: 'verify', kind: 'verify', target: '' },
];
const catalog = {
    version: 1, inventory_state: 'ready',
    presets: { web: ['nginx', 'php-fpm', 'mariadb', 'certbot'], web_mail: ['nginx', 'php-fpm', 'mariadb', 'certbot'], application: ['nginx', 'certbot'], dns: [], custom: [] },
    required_components: ['certbot'],
    components: ['nginx', 'php-fpm', 'mariadb', 'certbot'].map(id => ({ id, name: { nginx: 'Nginx', 'php-fpm': 'PHP-FPM', mariadb: 'MariaDB', certbot: 'Certbot' }[id], category: id === 'mariadb' ? 'database' : id === 'certbot' ? 'security' : 'web', dependencies: [], conflicts: [], supported: true, installed: false })),
};

// Everything a scenario can switch.
const state = {
    mode: 'ok',            // ok | down | starting
    session: true,
    setupStatus: 'draft',  // snapshot status
    setupReady: false,
    revision: 3,
    execution: null,       // set by /setup/start or by ctl
    license: 'active',     // active | missing | expired | invalid | unavailable | error | drop | hang
    served: '',            // hostname reported by /panel/access-address
    overrides: {},         // path -> { status, body, delay, drop, hang, after, times }
    domains: [],
    host: '',              // another panel host name for the certificate step (default panel.example.com)
    componentOperation: null, // a running component operation, as /service/operation reports it
    update: null,          // a running panel update, as /panel/update/status reports it
    // The whole contract of GET /hosting/capabilities: the interface treats an
    // answer with a field missing as unknown, not as "nothing installed".
    capabilities: { dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, web_server: 'nginx', php_versions: ['8.3'], mail_server: true, database_servers: ['mariadb'], db_tools: [] },
    subscriptions: [],
    dbServers: [],         // GET /database-servers
    dbDatabases: [],       // GET /database-servers/:id/databases
    dbUsers: [],           // GET /database-servers/:id/users
    domainDatabases: { databases: [], available_types: ['mysql'] }, // GET /domains/:id/databases
    connection: null,      // GET /domains/:id/connection (null: the mock has none, 404)
    // --- The second batch (2026-10-09) ---
    twoFactor: false,      // GET /auth/2fa/status
    panelCertificate: { https_enabled: true, self_signed: true, subject: 'server1', issuer: 'server1', expires_at: '2027-10-01T00:00:00Z' },
    certificateOperation: null, // the request POST /panel/certificate recorded, as /service/operation?request_id= reports it
    certificateStatus: 'running', // what that request is reported as: queued | running | succeeded | failed
    certificateError: null,
    users: [],             // GET /users
    plans: [],             // GET /plans
    files: { '/': [] },    // GET /domains/:id/files?path=
    ssl: null,             // GET /domains/:id/ssl (null: no certificate)
    sslAfterIssue: null,   // what /ssl answers after POST /ssl/letsencrypt
    noticeLicense: null,   // GET /panel/license, when it should differ from the access decision
    samples: [],           // GET /metrics/history
    services: [],          // GET /managed-services
    managedScan: null,     // GET /managed-services, whole, when a scenario of batch 2b sets it
    logs: [],              // GET /service/logs
    repo: { available: false, enabled: false }, // GET /repo
    candidate: '7.2.4',    // GET /service/candidate
    dashboard: { databases: 0, mail_accounts: 0, expiring_certs: [] }, // GET /dashboard
    importPreview: null,   // POST /import/cpanel/inspect
    assetDown: '',         // an asset whose name contains this is not served (a chunk that cannot be fetched)
};
// The component records always carry the three mail stacks and the services
// they are made of: the interface refuses a payload without them.
const component = (id, name, category, extra = {}) => ({ id, name, description: `${name} on this server`, icon: '■', category, status: '', is_installed: false, versions: [], kind: 'service', ...extra });
const MAIL_SERVICES = [component('postfix', 'Postfix', 'mail'), component('dovecot', 'Dovecot', 'mail'), component('roundcube', 'Roundcube', 'mail', { kind: 'tool' }), component('rspamd', 'Rspamd', 'mail')];
const MAIL_PROFILES = [['core-mail', 'Core mail', ['postfix', 'dovecot']], ['webmail', 'Webmail', ['roundcube']], ['protected-mail', 'Protected mail', ['rspamd']]]
    .map(([id, name, services]) => ({ id, name, description: `${name} stack`, status: 'available', available: true, verified: false, latest_attempt_status: 'none', services }));
const NO_CERTIFICATE = { domain_id: 1, domain_name: 'example.com', has_certificate: false, managed_names: ['example.com'], settings: { force_https: false, hsts_enabled: false, hsts_max_age: 300 } };
const log = [];
const snapshot = () => ({ version: 1, revision: state.revision, origin: 'fresh', status: state.setupStatus, required: state.setupStatus !== 'ready', guidance: 'guided', draft, checks: [], server_ip: '203.0.113.10' });
const execution = (requestId, statuses, extra = {}) => ({ id: EXEC_ID, request_id: requestId, plan_id: PLAN_ID, status: 'running', phase: 'service', steps: planSteps.map((step, index) => ({ ...step, ...(step.kind === 'panel_certificate' ? { target: state.host || HOST } : {}), status: statuses[index] || 'pending' })), context: { dns_mode: 'external', dns_role: '', dns_engine: 'bind', local_nameserver: '', local_ip: '', peer_nameserver: '', peer_ip: '', panel_domain: state.host || HOST, mail_hostname: '', dns_hosting_management: '' }, ...extra });

const send = (res, status, body, headers = {}) => { res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store', ...headers }); res.end(JSON.stringify(body)); };
const coded = (res, status, code, error, headers) => send(res, status, { error, code }, headers);
const readBody = req => new Promise(resolve => { let data = ''; req.on('data', chunk => { data += chunk; }); req.on('end', () => { try { resolve(data ? JSON.parse(data) : {}); } catch { resolve({}); } }); });
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const startingAllowed = new Set(['GET /api/v1/panel/availability', 'GET /api/v1/recovery/status', 'GET /api/v1/license/access', 'GET /api/v1/panel/access-address', 'GET /api/v1/auth/me', 'GET /api/v1/auth/demo', 'POST /api/v1/auth/login', 'POST /api/v1/auth/logout']);

async function api(req, res, path, query) {
    const key = `${req.method} ${path}`;
    if (state.mode === 'down') { req.socket.destroy(); return; }
    const override = state.overrides[path];
    // after: let the first N requests through untouched, then apply.
    // times: apply to that many requests only, then let the rest through.
    if (override && (override.hits = (override.hits || 0) + 1) > (override.after || 0) && (!override.times || override.hits <= (override.after || 0) + override.times)) {
        if (override.delay) await pause(override.delay);
        if (override.drop) { req.socket.destroy(); return; }
        if (override.hang) return;
        if (override.status) { send(res, override.status, override.body ?? {}); return; }
    }
    if (state.mode === 'starting' && !startingAllowed.has(key)) {
        coded(res, 503, 'PANEL_STARTING', 'Panel management is still starting. Read-only connection and recovery status remain available.', { 'Retry-After': '3' }); return;
    }
    if (key === 'POST /api/v1/auth/login') { const body = await readBody(req); if (body.password !== 'correct') { coded(res, 401, 'INVALID_CREDENTIALS', 'invalid credentials'); return; } state.session = true; send(res, 200, user); return; }
    if (key === 'GET /api/v1/auth/demo') { send(res, 200, []); return; }
    if (key === 'GET /api/v1/panel/access-address') { send(res, 200, { hostname: state.served }); return; }
    if (!state.session) { coded(res, 401, 'AUTH_REQUIRED', 'authentication required'); return; }
    if (key === 'POST /api/v1/auth/logout') { state.session = false; send(res, 200, {}); return; }
    if (key === 'GET /api/v1/auth/me') { send(res, 200, user); return; }
    if (key === 'GET /api/v1/panel/availability') { send(res, 200, { schema: 'celikpanel-panel-availability/v1', state: state.mode === 'starting' ? 'starting' : 'ready' }); return; }
    if (key === 'GET /api/v1/license/access') {
        const now = Math.floor(Date.now() / 1000);
        if (state.license === 'active') { send(res, 200, { can_use_panel: true, valid_until: now + (state.validity || 60), state: 'active', observation: 'known' }); return; }
        if (['missing', 'expired', 'invalid'].includes(state.license)) { send(res, 200, { can_use_panel: false, valid_until: 0, state: state.license, observation: 'known' }); return; }
        if (state.license === 'unavailable') { send(res, 200, { can_use_panel: false, valid_until: 0, state: 'status_unavailable', observation: 'unknown' }); return; }
        if (state.license === 'drop') { req.socket.destroy(); return; }
        if (state.license === 'hang') return;
        coded(res, 503, 'LICENSE_STATUS_UNAVAILABLE', 'license status unavailable'); return;
    }
    if (key === 'GET /api/v1/panel/license' && state.noticeLicense) { send(res, 200, state.noticeLicense); return; }
    if (key === 'GET /api/v1/panel/license') { send(res, 200, { state: ['missing', 'expired', 'invalid'].includes(state.license) ? state.license : 'active', can_provision: state.license === 'active', ...(state.license === 'expired' ? { expires_at: Math.floor(Date.now() / 1000) - 86400 * 3, license_id: 'CP-EXAMPLE-0001' } : {}) }); return; }
    if (key === 'GET /api/v1/panel/version') { send(res, 200, { version: 'v0.1.0-alpha.81', commit: '0000000', agent_commit: '0000000', agent_matches: true, hostname: 'server1', ipv4: '203.0.113.10' }); return; }
    if (key === 'GET /api/v1/panel/update/status') { const id = query.get('request_id') || ''; send(res, 200, state.update && state.update.request_id === id ? { found: true, ...state.update } : { found: false, request_id: id }); return; }
    if (key === 'GET /api/v1/setup') { send(res, 200, snapshot()); return; }
    if (key === 'PUT /api/v1/setup') { const body = await readBody(req); Object.assign(draft, body.draft || {}); state.revision++; state.setupStatus = 'draft'; send(res, 200, snapshot()); return; }
    if (key === 'GET /api/v1/setup/components') { send(res, 200, catalog); return; }
    if (key === 'POST /api/v1/setup/plan') { send(res, 200, { id: PLAN_ID, version: 1, revision: state.revision, purpose: 'web', steps: planSteps, blockers: [], can_start: true, tcp_ports: [22, 80, 443, 2083], udp_ports: [], preserve_ssh: true, persist_firewall: true, contact_email: 'owner@example.com', server_ip: '203.0.113.10', components: catalog.components.map(item => ({ id: item.id, selected: true, required: item.id === 'certbot', installed: false })) }); return; }
    if (key === 'POST /api/v1/setup/start') { const body = await readBody(req); state.setupStatus = 'running'; state.execution = execution(body.request_id, ['running']); send(res, 200, state.execution); return; }
    if (key === 'GET /api/v1/setup/operation') {
        if (!state.execution) { send(res, 200, null); return; }
        // A preset execution adopts the marker the browser holds.
        const requestId = query.get('request_id');
        if (requestId && state.execution.request_id === 'adopt') state.execution.request_id = requestId;
        send(res, 200, state.execution); return;
    }
    if (key === 'GET /api/v1/domains') { send(res, 200, state.domains); return; }
    if (key === 'GET /api/v1/hosting/capabilities') { send(res, 200, state.capabilities); return; }
    if (key === 'GET /api/v1/subscriptions') { send(res, 200, { subscriptions: state.subscriptions }); return; }
    if (key === 'GET /api/v1/database-servers') { send(res, 200, state.dbServers); return; }
    const engine = path.match(/^\/api\/v1\/database-servers\/(\d+)\/(databases|users|admin-account)$/);
    if (engine && req.method === 'GET' && engine[2] !== 'admin-account') { send(res, 200, engine[2] === 'databases' ? state.dbDatabases : state.dbUsers); return; }
    // The two changes the Databases page can make to the panel's own account,
    // so the page can be seen to follow the server's next answer.
    if (engine && engine[2] === 'admin-account' && ['POST', 'DELETE'].includes(req.method)) {
        state.dbServers = state.dbServers.map(item => (String(item.id) === engine[1] ? { ...item, admin_username: req.method === 'DELETE' ? '' : 'celikpanel' } : item));
        send(res, 200, { success: true }); return;
    }
    const removed = path.match(/^\/api\/v1\/databases\/(\d+)$/);
    if (removed && req.method === 'DELETE') { state.dbDatabases = state.dbDatabases.filter(item => String(item.id) !== removed[1]); send(res, 200, { success: true }); return; }
    const ofDomain = path.match(/^\/api\/v1\/domains\/(\d+)\/(connection|databases|usage|deletion-status)$/);
    if (ofDomain && req.method === 'GET') {
        if (ofDomain[2] === 'connection') { if (state.connection) send(res, 200, state.connection); else coded(res, 404, 'not_found', 'the mock has no connection answer set'); return; }
        if (ofDomain[2] === 'databases') { send(res, 200, state.domainDatabases); return; }
        if (ofDomain[2] === 'usage') { send(res, 200, { disk_usage: 4096, bandwidth: 0 }); return; }
        res.writeHead(204); res.end(); return;
    }
    if (key === 'GET /api/v1/managed-services') {
        // The whole contract of the component records: the interface treats an
        // answer with a field missing as unknown.
        const services = [...state.services, ...MAIL_SERVICES.filter(item => !state.services.some(service => service.id === item.id))];
        // A scenario of batch 2b sets the whole scan (state.managedScan); it
        // is the whole contract too, and it is cleared by every reset.
        send(res, 200, state.managedScan || { scanned_at: new Date().toISOString(), services, profiles: MAIL_PROFILES, dns_identity_ready: true, mail_hostname: { current: 'server1', current_usable: false, hostname: '', source: '', will_set_hostname: false } }); return;
    }
    // One exact request, as the panel-certificate poll asks for it. A request
    // the mock never recorded is "no such operation", as on the Panel.
    if (key === 'GET /api/v1/service/operation' && query.get('request_id')) {
        const op = state.certificateOperation;
        if (!op || op.request_id !== query.get('request_id')) { coded(res, 404, 'not_found', 'service operation not found'); return; }
        send(res, 200, { operation: { ...op, status: state.certificateStatus, ...(state.certificateStatus === 'failed' ? { error: state.certificateError || { code: 'certificate_issue_failed', message: 'The certificate authority could not reach panel.example.com on port 80.' } } : {}) } }); return;
    }
    if (key === 'GET /api/v1/service/operation') { send(res, 200, state.componentOperation ? { operation: state.componentOperation } : null); return; }
    // --- The second batch (2026-10-09) ---
    if (key === 'GET /api/v1/auth/2fa/status') { send(res, 200, { enabled: state.twoFactor }); return; }
    if (key === 'GET /api/v1/panel/certificate') { send(res, 200, state.panelCertificate); return; }
    if (key === 'POST /api/v1/panel/certificate') {
        const body = await readBody(req);
        state.certificateOperation = { id: 'e'.repeat(32), request_id: body.request_id, kind: 'panel_certificate_issue', service_id: String(body.domain || '').toLowerCase(), status: 'queued' };
        send(res, 202, { operation: state.certificateOperation }); return;
    }
    if (key === 'GET /api/v1/users') { send(res, 200, { users: state.users }); return; }
    if (key === 'GET /api/v1/plans') { send(res, 200, { plans: state.plans }); return; }
    if (/^\/api\/v1\/(users|plans)(\/\d+)?$/.test(path) && req.method !== 'GET') { send(res, 200, { success: true }); return; }
    const files = path.match(/^\/api\/v1\/domains\/(\d+)\/files$/);
    if (files && req.method === 'GET') { send(res, 200, { files: state.files[query.get('path') || '/'] ?? [] }); return; }
    const certificate = path.match(/^\/api\/v1\/domains\/(\d+)\/ssl(\/mail|\/letsencrypt)?$/);
    if (certificate && req.method === 'GET') { send(res, 200, certificate[2] === '/mail' ? { secure_mail: false } : (state.ssl || NO_CERTIFICATE)); return; }
    if (certificate && certificate[2] === '/letsencrypt' && req.method === 'POST') { if (state.sslAfterIssue) state.ssl = state.sslAfterIssue; send(res, 200, { success: true }); return; }
    if (key === 'GET /api/v1/ssl/providers') { send(res, 200, { providers: [{ id: 'letsencrypt', name: 'Let\u2019s Encrypt', note: '', needs_eab: false }] }); return; }
    if (key === 'GET /api/v1/metrics/history') { send(res, 200, { samples: state.samples }); return; }
    if (key === 'GET /api/v1/service/logs') { send(res, 200, { lines: state.logs }); return; }
    if (key === 'GET /api/v1/service/candidate') { send(res, 200, { version: state.candidate }); return; }
    if (key === 'GET /api/v1/repo') { send(res, 200, state.repo); return; }
    if (key === 'GET /api/v1/dashboard') { send(res, 200, state.dashboard); return; }
    if (key === 'GET /api/v1/host-mutation-readiness') { send(res, 200, { ready: true }); return; }
    if (key === 'POST /api/v1/import/cpanel/inspect') { send(res, 200, state.importPreview); return; }
    if (key === 'POST /api/v1/import/cpanel/apply') { send(res, 200, { steps: [{ step: 'domain', ok: true, detail: 'created' }] }); return; }
    // --- batch 2b (9 Oct 2026): database and mail configuration screens ---
    // Routes these screens read, in their own file so two batches of scenarios
    // can be merged without touching each other's lines.
    if (await (await import('./mock-batch2b.mjs')).batch2b(req, res, path, query, { state, send, coded, readBody })) return;
    // --- end of batch 2b ---
    coded(res, 404, 'not_found', `mock has no route for ${key}`);
}

const server = createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const path = url.pathname;
    if (path === '/__ctl') {
        const body = await readBody(req);
        for (const [name, value] of Object.entries(body)) {
            if (name === 'execution' && value) state.execution = execution(value.request_id || 'adopt', value.statuses || [], value.extra || {});
            else if (name === 'override') Object.assign(state.overrides, value);
            else if (name === 'clear') for (const item of value) delete state.overrides[item];
            // Every override at once: a scenario that follows another batch's starts from none.
            else if (name === 'clearAll') for (const item of Object.keys(state.overrides)) delete state.overrides[item];
            else state[name] = value;
        }
        send(res, 200, { ok: true }); return;
    }
    if (path === '/__log') { send(res, 200, log.splice(0)); return; }
    if (path.startsWith('/api/')) {
        log.push(`${new Date().toISOString().slice(11, 23)} ${req.method} ${path}${url.search} [${state.mode}]`);
        try { await api(req, res, path, url.searchParams); } catch (error) { if (!res.headersSent) send(res, 500, { error: String(error) }); }
        return;
    }
    if (state.mode === 'down' && state.assetsDown) { req.socket.destroy(); return; }
    try {
        const name = path.startsWith('/assets/') || ['/recovery-worker.js', '/recovery-offline.html', '/vite.svg'].includes(path) ? path.slice(1) : 'index.html';
        if (!/^(index\.html|recovery-offline\.html|recovery-worker\.js|vite\.svg|assets\/[A-Za-z0-9_.-]+)$/.test(name)) throw Error('unknown asset');
        if (state.assetDown && name.includes(state.assetDown)) throw Error('this asset is withheld');
        const bytes = await readFile(join(dist, name));
        res.writeHead(200, { 'Content-Type': mime[extname(name)] || 'application/octet-stream', 'Cache-Control': 'no-cache' }); res.end(bytes);
    } catch { res.writeHead(404); res.end(); }
});
const port = Number(process.argv[2] || 4791);
server.listen(port, '127.0.0.1', () => console.log(`mock on http://127.0.0.1:${port}`));
