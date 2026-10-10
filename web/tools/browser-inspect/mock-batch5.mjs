// Loopback-only mock routes for the request identity (D-029), the service
// actions and the VPN page (10 Oct 2026). Loaded by mock.mjs; like it, this
// answers constants and contacts nothing.
//
// The eight routes that carry an identity are answered through `guard`, which
// keeps the contract of cmd/panel/request_identity.go:
//
//   - no header: 428 REQUEST_ID_REQUIRED, before anything runs;
//   - first arrival of an identity: a row `running`, the change is made once,
//     its answer is stored (status only for an answer that carries a one-time
//     secret) and then sent;
//   - the same identity with the same request again: the stored answer, with
//     the header X-CelikPanel-Request-Replayed; while the first arrival still
//     runs it waits `wait` ms (the Panel: 20 s) and then answers 409
//     REQUEST_IN_PROGRESS; a row left by a "restart" answers 409
//     REQUEST_OUTCOME_UNKNOWN; an answer that was not kept answers 409
//     REQUEST_COMPLETED_RESULT_NOT_RETAINED (reason `failed` after an error);
//   - the same identity with another request: 409 REQUEST_ID_REUSED.
//
// It counts, per "METHOD path", how often a request ARRIVED, how often the
// change was MADE, how often an arrival was answered from the row, and which
// identity each arrival carried: one click must make one change.
//
// What a scenario switches is `state.b5.plan["METHOD path"]`:
//   loseFor: ms   every arrival within that time of the first loses its answer
//                 (the connection is reset). 1000 covers the requests Chrome
//                 repeats by itself on a reset connection and lets the page's
//                 own second asking, 1.5 s later, be answered. -1: every one.
//   loseAs: 'gateway'   a gateway's 502 page instead of the reset
//   restart: true  the "Panel restarts" while the change runs: the row stays
//                 `interrupted`, the connection is reset (`applied`: whether
//                 the change was made before it stopped)
//   slow: ms      the change takes this long
//   fail: { status, body }   the change ends with the Panel's own refusal
//   noHeader: true   the header is ignored, as if a page that predates the
//                 update had sent the request
import { createHash } from 'node:crypto';

const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
// The body as it was sent: the guard compares requests by their bytes.
const readRaw = (req) => new Promise((resolve) => { let data = ''; req.on('data', (chunk) => { data += chunk; }); req.on('end', () => resolve(data)); });
const D = '/api/v1/domains/1';

export const GUARDED = {
    backup: `POST ${D}/backups`,
    restore: `POST ${D}/backups/restore`,
    certificate: `POST ${D}/ssl/letsencrypt`,
    domainDatabase: `POST ${D}/databases`,
    serverDatabase: 'POST /api/v1/database-servers/1/databases',
    account: 'POST /api/v1/database-servers/1/admin-account',
    peer: 'POST /api/v1/vpn/peers',
    importApply: 'POST /api/v1/import/cpanel/apply',
};
// The two routes whose answers are never stored.
const SECRET = new Set([GUARDED.account, GUARDED.peer]);

// The Panel's own sentences (cmd/panel/request_identity.go).
const SENTENCES = {
    REQUEST_ID_REQUIRED: 'This page was opened before CelikPanel was updated, so the server did not accept the change and nothing was changed. Reload the page, then make the change again. (A client that is not the CelikPanel page sends the header X-CelikPanel-Request-Id: 32 lowercase hexadecimal characters, a new value for each action.)',
    REQUEST_ID_REUSED: 'This change was sent with an identifier the server already used for a different change, so it was not carried out. Reload the page, then make the change again.',
    REQUEST_IN_PROGRESS: 'This change is still running on the server. It was not started a second time. Wait a little, then reload the page to see the result; do not send it again.',
    REQUEST_OUTCOME_UNKNOWN: 'CelikPanel restarted or failed while this change was running, so it is not known whether the change was completed. It will not be run again by itself. Reload the page and check the current state; make the change again only if it is missing.',
    REQUEST_COMPLETED_RESULT_NOT_RETAINED: 'This change was already made; it was not made a second time. Its result was shown only once and is not kept. Reload the page to see the current state; if you still need what was shown once (a password or a configuration file), create a new one.',
    'REQUEST_COMPLETED_RESULT_NOT_RETAINED.failed': 'This change already ended with an error, and that answer is not kept; it was not tried a second time. Reload the page and check the current state; make the change again only if it is missing.',
};

export const b5Defaults = () => ({
    plan: {},
    wait: 1200,
    rows: {},        // identity -> { key, hash, status, code, body, retained, startedAt }
    arrivals: {},    // per "METHOD path"
    effects: {},
    replays: {},
    ids: {},         // the identity each arrival carried, in order
    first: {},       // when the first arrival of a key came
    // The VPN page.
    vpn: { installed: true, configured: true, running: true, server_public_key: 'k3Jq1v0m8yXcT2uP9wZr5sLd7aBfHn4eGi6oQxYtVUM=', port: 51820, endpoint: 'vpn.example.com:51820', peer_count: 1, sync: { in_sync: true, pending: 0, errors: 0 },
        policy: { interface: 'wg0', network: '10.8.0.0/24', server_address: '10.8.0.1', listen_protocol: 'udp', listen_port: 51820, client_dns: '10.8.0.1', allowed_ips: '0.0.0.0/0, ::/0', full_tunnel: true, nat_required: true, forward_required: true, firewall_required: true } },
    peers: [{ id: 1, subscription_id: 3, subscription: 'Example hosting', name: 'office-desktop', ip: '10.8.0.2', created_at: '2026-10-01T09:00:00Z', last_handshake: 0, rx_bytes: 0, tx_bytes: 0, desired_state: 'active', sync_state: 'applied' }],
    // What POST /api/v1/service/action answers: null is "done".
    serviceAction: null,
    actions: 0,
});

// Requests still running in this process, by identity. Not part of the state a
// scenario sends: a promise cannot travel through /__ctl.
const running = new Map();

const count = (bag, key) => { bag[key] = (bag[key] || 0) + 1; return bag[key]; };

function refuse(res, status, code, id, reason) {
    const headers = { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' };
    if (id) headers['X-CelikPanel-Request-Id'] = id;
    res.writeHead(status, headers);
    res.end(JSON.stringify({ error: SENTENCES[reason ? `${code}.${reason}` : code], code, ...(reason ? { reason } : {}), ...(id ? { vars: { request_id: id } } : {}) }));
}

// guard answers one arrival of a guarded change. `make(body)` makes the change
// and returns { status, body, secret? }.
async function guard(req, res, key, rawQuery, raw, b5, make) {
    const plan = b5.plan[key] || {};
    const nth = count(b5.arrivals, key);
    const now = Date.now();
    if (nth === 1) b5.first[key] = now;
    const id = plan.noHeader ? '' : String(req.headers['x-celikpanel-request-id'] || '');
    (b5.ids[key] = b5.ids[key] || []).push(id);
    if (!id) { refuse(res, 428, 'REQUEST_ID_REQUIRED', ''); return; }
    if (!/^[0-9a-f]{32}$/.test(id)) { refuse(res, 400, 'REQUEST_ID_REQUIRED', ''); return; }
    const hash = createHash('sha256').update(`${key}\0${rawQuery}\0`).update(raw).digest('hex');

    // Whether this arrival's answer is lost on the way back.
    const lost = plan.loseFor === -1 || (plan.loseFor > 0 && now - b5.first[key] <= plan.loseFor);
    const answer = (status, body, replayed) => {
        if (lost) {
            if (plan.loseAs === 'gateway') { res.writeHead(502, { 'Content-Type': 'text/html' }); res.end('<html><body><h1>502 Bad Gateway</h1></body></html>'); return; }
            // A reset, as a real one: what Chrome then repeats by itself
            // carries the same identity and is answered from the row.
            req.socket.destroy();
            return;
        }
        const headers = { 'Content-Type': 'application/json', 'Cache-Control': 'no-store', 'X-CelikPanel-Request-Id': id };
        if (replayed) headers['X-CelikPanel-Request-Replayed'] = '1';
        res.writeHead(status, headers);
        res.end(JSON.stringify(body));
    };
    const stored = (row) => {
        if (row.status === 'interrupted') { lost ? req.socket.destroy() : refuse(res, 409, 'REQUEST_OUTCOME_UNKNOWN', id); return; }
        if (!row.retained) { lost ? req.socket.destroy() : refuse(res, 409, 'REQUEST_COMPLETED_RESULT_NOT_RETAINED', id, row.code >= 200 && row.code < 300 ? undefined : 'failed'); return; }
        answer(row.code, row.body, true);
    };

    let row = b5.rows[id];
    if (row) {
        count(b5.replays, key);
        if (row.key !== key || row.hash !== hash) { refuse(res, 409, 'REQUEST_ID_REUSED', id); return; }
        if (row.status === 'running') {
            await Promise.race([running.get(id) ?? Promise.resolve(), pause(b5.wait)]);
            row = b5.rows[id] || row;
            if (row.status === 'running') { lost ? req.socket.destroy() : refuse(res, 409, 'REQUEST_IN_PROGRESS', id); return; }
        }
        stored(row);
        return;
    }

    row = b5.rows[id] = { key, hash, status: 'running', code: 0, body: null, retained: false };
    if (plan.restart) {
        // The Panel stops between the change and the record of it.
        if (plan.applied) { count(b5.effects, key); make(JSON.parse(raw || '{}')); }
        row.status = 'interrupted';
        req.socket.destroy();
        return;
    }
    const work = (async () => {
        if (plan.slow) await pause(plan.slow);
        let out;
        if (plan.fail) out = plan.fail;
        else { count(b5.effects, key); out = make(JSON.parse(raw || '{}')); }
        Object.assign(row, { status: 'done', code: out.status, body: out.body, retained: !SECRET.has(key) && !out.secret });
    })();
    running.set(id, work);
    void work.then(() => running.delete(id));
    // A change that takes long and whose connection is reset: the reset comes
    // now, while the change goes on running on the server.
    if (lost && plan.slow) { req.socket.destroy(); return; }
    await work;
    answer(row.code, row.body, false);
}

export async function batch5(req, res, path, query, { state, send }) {
    if (!state.b5) return false;
    const b5 = state.b5;
    const key = `${req.method} ${path}`;
    if (key === 'GET /api/v1/__b5') {
        send(res, 200, { arrivals: b5.arrivals, effects: b5.effects, replays: b5.replays, ids: b5.ids, actions: b5.actions, rows: Object.values(b5.rows).map((row) => ({ key: row.key, status: row.status, retained: row.retained })) });
        return true;
    }

    // --- The VPN page ---
    if (key === 'GET /api/v1/vpn/status') { send(res, 200, { ...b5.vpn, peer_count: b5.peers.length }); return true; }
    if (key === 'GET /api/v1/vpn/peers') { send(res, 200, { peers: b5.peers }); return true; }
    if (/^\/api\/v1\/subscriptions\/\d+\/entitlements$/.test(path) && req.method === 'GET') { send(res, 200, { entitlements: [{ product_id: 'vpn', status: 'active' }] }); return true; }
    if (/^\/api\/v1\/vpn\/peers\/\d+\/ack$/.test(path) && req.method === 'POST') { send(res, 200, { success: true }); return true; }
    const peer = path.match(/^\/api\/v1\/vpn\/peers\/(\d+)$/);
    if (peer && req.method === 'DELETE') { b5.peers = b5.peers.filter((item) => String(item.id) !== peer[1]); send(res, 200, { success: true }); return true; }

    // --- Start, Stop, Restart on the Services pages ---
    if (key === 'POST /api/v1/service/action') {
        const body = JSON.parse((await readRaw(req)) || '{}');
        b5.actions += 1;
        const outcome = b5.serviceAction;
        if (!outcome) { send(res, 200, { success: true }); return true; }
        if (outcome.drop) { req.socket.end('connection lost\r\n\r\n'); return true; }
        // 502 unless the outcome names its own status (a reload of a stopped
        // service is answered 409).
        res.writeHead(outcome.status || 502, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: outcome.error, code: outcome.code, ...(outcome.reason ? { reason: outcome.reason } : {}), vars: { unit: body.name, action: body.action, ...outcome.vars } }));
        return true;
    }

    // --- The eight routes that carry an identity ---
    const changes = {
        [GUARDED.backup]: (body) => {
            if (state.b4) state.b4.backups = [{ name: `example.com-${body.type}-20261010-091500.tar.gz`, size: 4096, type: body.type, origin: 'manual', legacy: false, restorable: true, created_at: '2026-10-10T09:15:00Z' }, ...state.b4.backups];
            return { status: 200, body: { success: true } };
        },
        [GUARDED.restore]: () => ({ status: 200, body: { success: true, safety_backup: { name: 'example.com-files-20261010-091400-before-restore.tar.gz' } } }),
        [GUARDED.certificate]: () => { if (state.sslAfterIssue) state.ssl = state.sslAfterIssue; return { status: 200, body: { success: true } }; },
        [GUARDED.domainDatabase]: (body) => {
            state.domainDatabases = { ...state.domainDatabases, databases: [...state.domainDatabases.databases, { id: 40 + state.domainDatabases.databases.length, name: body.name, type: body.type === 'postgresql' ? 'postgresql' : 'mariadb', user: body.name, created_at: '2026-10-10T09:15:00Z' }] };
            return { status: 200, body: { success: true, name: body.name } };
        },
        [GUARDED.serverDatabase]: (body) => {
            const user = body.new_username || 'example_com_shop';
            state.dbDatabases = [...state.dbDatabases, { id: 60 + state.dbDatabases.length, name: body.database_name, users: [user], created_at: '2026-10-10T09:15:00Z' }];
            if (body.new_username) state.dbUsers = [...state.dbUsers, { id: 70 + state.dbUsers.length, username: user, databases: [body.database_name], created_at: '2026-10-10T09:15:00Z' }];
            // An answer that carries a password minted by this request is not stored.
            return { status: 200, body: { success: true, name: body.database_name, user, ...(body.new_username ? { password: body.new_password } : {}) }, secret: Boolean(body.new_username) };
        },
        [GUARDED.account]: () => {
            state.dbServers = state.dbServers.map((item) => (item.id === 1 ? { ...item, admin_username: 'celikpanel_admin' } : item));
            return { status: 200, body: { success: true } };
        },
        [GUARDED.peer]: (body) => {
            const id = 10 + b5.peers.length;
            b5.peers = [...b5.peers, { id, subscription_id: body.subscription_id, subscription: 'Example hosting', name: body.name, ip: `10.8.0.${id}`, created_at: '2026-10-10T09:15:00Z', last_handshake: 0, rx_bytes: 0, tx_bytes: 0, desired_state: 'active', sync_state: 'applied' }];
            return { status: 200, body: { id, client_config: `[Interface]\nPrivateKey = (shown once)\nAddress = 10.8.0.${id}/32\nDNS = 10.8.0.1\n\n[Peer]\nPublicKey = ${b5.vpn.server_public_key}\nEndpoint = vpn.example.com:51820\nAllowedIPs = 0.0.0.0/0, ::/0\n`, delivery_token: 'f'.repeat(32) } };
        },
        [GUARDED.importApply]: (body) => {
            state.domains = [...state.domains, { id: 9, domain_name: body.domain, status: 'active', project_type: 'php', created_at: '2026-10-10T09:15:00Z' }];
            return { status: 200, body: { steps: [{ step: 'domain', ok: true, detail: 'created' }, { step: 'files', ok: true, detail: '412 files' }] } };
        },
    };
    if (changes[key]) {
        await guard(req, res, key, query.toString(), await readRaw(req), b5, changes[key]);
        return true;
    }
    return false;
}
