import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHash, webcrypto } from 'node:crypto';
import vm from 'node:vm';

const template = readFileSync(new URL('../scripts/recovery-worker.template.js', import.meta.url), 'utf8');
const origin = 'https://panel.example.test';
const bodies = { '/recovery-offline.html': ['<main>Generic recovery only</main>', 'text/html'], '/assets/recovery-test.js': ['/* no user data */', 'text/javascript'] };
const files = Object.entries(bodies).map(([path, [body, type]]) => ({ path, type, size: Buffer.byteLength(body), sha256: createHash('sha256').update(body).digest('hex') }));
function fixture() {
    const events = {}, data = new Map(), calls = [];
    let skips = 0, claims = 0, fetcher = async path => response(path);
    function response(path, { body, status = 200, type, url } = {}) {
        const r = new Response(body ?? bodies[path]?.[0] ?? 'network', { status, headers: { 'content-type': type ?? bodies[path]?.[1] ?? 'text/html' } });
        Object.defineProperty(r, 'url', { value: url ?? origin + path }); return r;
    }
    const caches = {
        async keys() { return [...data.keys()]; }, async delete(key) { return data.delete(key); },
        async open(name) {
            if (!data.has(name)) data.set(name, new Map()); const cache = data.get(name);
            return { async match(key) { return cache.get(key)?.clone(); }, async put(key, value) { cache.set(key, value.clone()); } };
        },
    };
    const context = vm.createContext({ URL, Response, Headers, AbortController, setTimeout, clearTimeout, crypto: webcrypto, caches,
        fetch: (...args) => { calls.push(args); return fetcher(...args); },
        self: { location: { origin }, addEventListener: (name, handler) => events[name] = handler,
            skipWaiting: async () => { skips++; }, clients: { claim: async () => { claims++; } } },
    });
    vm.runInContext(template.replace('__BUILD_ID__', 'test').replace('__RESOURCES__', JSON.stringify(files)), context);
    const dispatch = async name => { let p; events[name]({ waitUntil: value => p = value }); await p; };
    function request(path, options = {}) {
        let result;
        events.fetch({ request: { url: origin + path, method: 'GET', mode: 'navigate', ...options }, respondWith: value => result = value });
        return result;
    }
    return { data, calls, response, dispatch, request, setFetch: value => fetcher = value, counts: () => ({ skips, claims }) };
}

test('installation pins only exact public build bytes without credentials', async () => {
    const f = fixture(); await f.dispatch('install');
    assert.deepEqual([...f.data.get('celikpanel-recovery-shell-test').keys()], Object.keys(bodies));
    assert.equal(f.counts().skips, 1);
    for (const [, options] of f.calls) assert.deepEqual(JSON.parse(JSON.stringify(options)), { cache: 'no-store', credentials: 'omit', redirect: 'error' });
});

test('wrong bytes, MIME, external response and failed fetch never replace the old cache', async () => {
    for (const bad of [{ body: 'different' }, { type: 'text/html' }, { url: 'https://foreign.test/file' }, { status: 503 }]) {
        const f = fixture(); f.data.set('celikpanel-recovery-shell-old', new Map([['old', new Response('retained')]]));
        f.setFetch(async path => f.response(path, path.endsWith('.js') ? bad : {}));
        await assert.rejects(f.dispatch('install'));
        assert.equal(f.counts().skips, 0); assert.equal(f.data.size, 1);
        assert.equal(await f.data.get('celikpanel-recovery-shell-old').get('old').text(), 'retained');
    }
});

test('activation retires only this shell namespace', async () => {
    const f = fixture(); f.data.set('owner-workload', new Map()); f.data.set('celikpanel-recovery-shell-old', new Map());
    await f.dispatch('install'); await f.dispatch('activate');
    assert.deepEqual([...f.data.keys()].sort(), ['celikpanel-recovery-shell-test', 'owner-workload']);
    assert.equal(f.counts().claims, 1);
});

test('offline reload and 503 use generic recovery with restrictive policy, preserving URL and no API cache', async () => {
    for (const fail of [async () => { throw Error('disconnected'); }, async () => new Response('maintenance', { status: 503 })]) {
        const f = fixture(); await f.dispatch('install'); f.setFetch(fail);
        for (const path of ['/settings?section=updates', '/setup', '/domains/example.test']) {
            const result = await f.request(path);
            assert.equal(result.status, 200); assert.equal(result.headers.get('X-CelikPanel-Offline'), '1');
            assert.match(result.headers.get('Content-Security-Policy'), /form-action 'none'/);
            assert.equal(await result.text(), bodies['/recovery-offline.html'][0]);
        }
        assert.equal(f.data.get('celikpanel-recovery-shell-test').size, 2);
    }
});

test('live success, deliberate auth refusal and redirects keep the original response', async () => {
    const f = fixture(); await f.dispatch('install');
    for (const status of [200, 302, 401, 403, 404, 429]) {
        f.setFetch(async () => new Response('original response', { status }));
        const response = await f.request('/setup'); assert.equal(response.status, status); assert.equal(await response.text(), 'original response');
        assert.equal(response.headers.get('X-CelikPanel-Offline'), null);
    }
});

test('API, mutations, external URLs, proxies and non-navigation fetches are never intercepted', () => {
    const f = fixture();
    for (const [path, options] of [
        ['/api/v1/recovery/status?request_id=' + 'a'.repeat(32), {}], ['/api/v1/system/update/start', { method: 'POST' }],
        ['/setup', { method: 'POST' }], ['/settings', { mode: 'cors' }], ['/webmail', {}], ['/phpmyadmin', {}],
        ['/settings', { url: 'https://foreign.test/settings' }], ['/assets/other.js', { mode: 'cors' }],
        ['/recovery-offline.html?untrusted=1', { mode: 'cors' }],
    ]) assert.equal(f.request(path, options), undefined, path);
    assert.equal(f.calls.length, 0);
});

test('immutable offline assets still load after network loss', async () => {
    const f = fixture(); await f.dispatch('install'); f.setFetch(async () => { throw Error('offline'); });
    const response = await f.request('/assets/recovery-test.js', { mode: 'cors' });
    assert.equal(await response.text(), bodies['/assets/recovery-test.js'][0]);
});

test('lost browser cache produces honest unavailable response rather than a success claim', async () => {
    const f = fixture(); f.setFetch(async () => { throw Error('offline'); });
    const response = await f.request('/setup'); assert.equal(response.status, 503);
    assert.match(await response.text(), /connection unavailable/);
});
