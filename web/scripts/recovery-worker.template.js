/* Generated static recovery shell. No API, cookie, session or operation cache. */
const CACHE_PREFIX = 'celikpanel-recovery-shell-';
const CACHE = CACHE_PREFIX + '__BUILD_ID__';
const FILES = __RESOURCES__;
const HTML = '/recovery-offline.html';
const ROUTES = new Set(['/', '/login', '/activate', '/setup', '/settings', '/domains', '/databases', '/services', '/monitoring', '/users', '/import', '/audit', '/addons', '/vpn']);
const assets = new Set(FILES.map(file => file.path));
const sameOrigin = url => new URL(url).origin === self.location.origin;
const isPanelRoute = path => ROUTES.has(path) || /^\/(domains|services)\/[^/]+$/.test(path);

self.addEventListener('install', event => event.waitUntil((async () => {
    // Verify all immutable build members before making the shell available.
    const entries = await Promise.all(FILES.map(async file => {
        const response = await fetch(file.path, { cache: 'no-store', credentials: 'omit', redirect: 'error' });
        if (!response.ok || !sameOrigin(response.url) || response.headers.get('content-type')?.split(';')[0] !== file.type) throw new Error('recovery shell response unavailable');
        const bytes = await response.clone().arrayBuffer();
        const digest = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)), n => n.toString(16).padStart(2, '0')).join('');
        if (bytes.byteLength !== file.size || digest !== file.sha256) throw new Error('recovery shell build changed');
        return [file.path, response];
    }));
    const cache = await caches.open(CACHE);
    await Promise.all(entries.map(([path, response]) => cache.put(path, response)));
    await self.skipWaiting();
})()));
self.addEventListener('activate', event => event.waitUntil((async () => {
    // Other caches may belong to owner applications; never clear them.
    await Promise.all((await caches.keys()).filter(key => key.startsWith(CACHE_PREFIX) && key !== CACHE).map(key => caches.delete(key)));
    await self.clients.claim();
})()));

async function offline() {
    const response = await (await caches.open(CACHE)).match(HTML);
    if (!response) return new Response('Panel connection unavailable. Check the server connection and reload.', { status: 503, headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store' } });
    const headers = new Headers(response.headers);
    headers.set('Cache-Control', 'no-store');
    headers.set('Content-Security-Policy', "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'");
    headers.set('Referrer-Policy', 'no-referrer');
    headers.set('X-Content-Type-Options', 'nosniff');
    headers.set('X-CelikPanel-Offline', '1');
    // A synthetic URL prevents relative asset resolution against a failed route.
    return new Response(await response.arrayBuffer(), { status: 200, headers });
}
async function navigate(request) {
    const abort = new AbortController();
    const timer = setTimeout(() => abort.abort(), 5000);
    try {
        const response = await fetch(request, { signal: abort.signal, cache: 'no-store' });
        // A deliberate auth/refusal/redirect must never become an offline claim.
        if (response.status < 500) return response;
        return await offline();
    } catch { return await offline(); }
    finally { clearTimeout(timer); }
}
self.addEventListener('fetch', event => {
    const request = event.request;
    if (request.method !== 'GET' || !sameOrigin(request.url)) return;
    const url = new URL(request.url);
    if (request.mode === 'navigate' && isPanelRoute(url.pathname)) {
        event.respondWith(navigate(request));
    } else if (!url.search && assets.has(url.pathname)) {
        event.respondWith((async () => (await (await caches.open(CACHE)).match(url.pathname)) || fetch(request))());
    }
});
