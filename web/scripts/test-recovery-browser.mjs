// Opt-in local Chrome test. Starts only a loopback fixture and isolated profile.
// node web/scripts/test-recovery-browser.mjs <chrome executable> <evidence directory>
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { readFile, writeFile, mkdir, mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve, join, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

const [chromePath, outputPath] = process.argv.slice(2);
if (!chromePath || !outputPath) throw Error('Chrome path and fresh evidence directory required');
const output = resolve(outputPath); await mkdir(output, { recursive: false });
const profile = await mkdtemp(join(tmpdir(), 'cp-recovery-browser-'));
const dist = fileURLToPath(new URL('../dist/', import.meta.url));
const methods = [], errors = [];
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2', '.svg': 'image/svg+xml' };
const server = createServer(async (req, res) => {
    methods.push(req.method + ' ' + req.url);
    const path = new URL(req.url, 'http://localhost').pathname;
    if (path.startsWith('/api/')) {
        res.writeHead(401, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
        res.end(JSON.stringify({ error: 'authentication required', code: 'AUTH_REQUIRED' })); return;
    }
    try {
        const name = path.startsWith('/assets/') || ['/recovery-worker.js', '/recovery-offline.html'].includes(path) ? path.slice(1) : 'index.html';
        if (!/^(index\.html|recovery-offline\.html|recovery-worker\.js|assets\/[A-Za-z0-9_.-]+)$/.test(name)) throw Error('unknown asset');
        const bytes = await readFile(join(dist, name));
        res.writeHead(200, { 'Content-Type': mime[extname(name)] || 'application/octet-stream', 'Cache-Control': 'no-cache' }); res.end(bytes);
    } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const port = server.address().port, base = `http://127.0.0.1:${port}`;
const child = spawn(chromePath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--remote-debugging-address=127.0.0.1', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide: true, stdio: 'ignore' });
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
let socket;
async function until(fn, message) {
    const deadline = Date.now() + 20000;
    while (Date.now() < deadline) { try { const value = await fn(); if (value) return value; } catch {} await pause(100); }
    throw Error(message);
}
try {
    const debug = await until(async () => Number((await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0]), 'Chrome did not start');
    const tabs = await (await fetch(`http://127.0.0.1:${debug}/json/list`)).json();
    socket = new WebSocket(tabs.find(tab => tab.type === 'page').webSocketDebuggerUrl);
    await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
    let next = 0; const pending = new Map();
    socket.onmessage = ({data}) => {
        const value = JSON.parse(data);
        if (value.id) { const p = pending.get(value.id); pending.delete(value.id); if (value.error) p.reject(Error(JSON.stringify(value.error))); else p.resolve(value.result); }
        if (value.method === 'Runtime.exceptionThrown') errors.push(value.params.exceptionDetails.text);
    };
    function call(method, params = {}) {
        return new Promise((resolve, reject) => { const id = ++next; pending.set(id, {resolve, reject}); socket.send(JSON.stringify({id, method, params})); });
    }
    async function evaluate(expression) {
        if (expression.includes('await ')) expression = `(async () => (${expression}))()`;
        const r = await call('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
        if (r.exceptionDetails) throw Error(JSON.stringify(r.exceptionDetails)); return r.result.value;
    }
    await call('Page.enable'); await call('Runtime.enable');
    await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await call('Page.navigate', { url: base + '/settings?section=updates' });
    await until(() => evaluate('!!navigator.serviceWorker.controller'), 'Recovery worker did not control page');
    const id = 'a'.repeat(32);
    await evaluate(`localStorage.setItem('celikpanel.system-update-operation.v1', JSON.stringify({marker_version:1,request_id:'${id}',outcome:'succeeded',message:'FORGED RESULT MUST NOT APPEAR'})); localStorage.setItem('celikpanel.lang','en'); true`);
    const baseline = await evaluate(`({storage: JSON.stringify({...localStorage}), caches: await caches.keys()})`);
    // Stop the actual HTTP listener, not just a mocked fetch response.
    await new Promise(resolve => { server.close(resolve); server.closeAllConnections(); });
    const checks = [];
    for (const [lang, width] of [['en',1440], ['tr',390]]) {
        await call('Emulation.setDeviceMetricsOverride', { width, height: width === 390 ? 844 : 1000, deviceScaleFactor: 1, mobile: width === 390 });
        await call('Page.reload');
        await until(() => evaluate('!!document.getElementById("check")'), 'Offline page did not survive navigation');
        await evaluate(`document.getElementById('lang-${lang}').click()`);
        await until(() => evaluate('!document.getElementById("check").disabled'), 'Offline read did not settle');
        const result = await evaluate(`({lang:document.documentElement.lang, reference:document.getElementById('operation-id').textContent, overflow:document.documentElement.scrollWidth>innerWidth, forged:document.body.textContent.includes('FORGED RESULT'), openHidden:document.getElementById('open').hidden, storage:JSON.stringify({...localStorage}), view:document.getElementById('view-command').textContent, viewHelp:document.querySelector('[data-copy=viewHelp]').textContent, text:document.querySelector('h1').textContent, scripts:[...document.scripts].map(s=>s.src), cacheEntries:await Promise.all((await caches.keys()).map(async name=>(await (await caches.open(name)).keys()).map(r=>r.url)))})`);
        assert.equal(result.lang, lang); assert.equal(result.reference, id); assert.equal(result.overflow, false); assert.equal(result.forged, false); assert.equal(result.openHidden, true);
        assert.equal(result.storage, baseline.storage);
        // Owner SSH view: exact operation, local forward, no live status claimed at this address.
        assert.equal(result.view, `ssh -t -o ExitOnForwardFailure=yes -L 127.0.0.1:2084:127.0.0.1:2084 USER@127.0.0.1 sudo /usr/libexec/celikpanel/recovery view --request-id ${id} --lang ${lang}`);
        assert.ok(result.viewHelp.includes('http://127.0.0.1:2084/'));
        assert.ok(result.cacheEntries.flat().every(url => !url.includes('/api/') && !url.endsWith('/index.html')));
        const png = await call('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
        await writeFile(join(output, `${lang}-${width}.png`), Buffer.from(png.data,'base64'));
        checks.push(result);
    }
    await new Promise(resolve => server.listen(port, '127.0.0.1', resolve));
    await evaluate('document.getElementById("check").click()');
    await until(() => evaluate('!document.getElementById("open").hidden'), 'Real 401 did not offer authenticated panel return');
    await evaluate('document.getElementById("open").click()');
    await until(() => evaluate('!!document.getElementById("root")'), 'Panel did not return');
    assert.equal(await evaluate('location.pathname'), '/settings');
    assert.deepEqual(errors, []);
    assert.ok(methods.every(method => method.startsWith('GET ')), 'a mutation was sent');
    await writeFile(join(output, 'result.json'), JSON.stringify({scope:'local real Chrome; actual listener interruption; fixture API auth responses; not native update acceptance',checks,networkMethods:methods,errors,profile},null,2));
    console.log(JSON.stringify({result:'passed',output,checks:checks.length,mutations:0,pageErrors:errors.length}));
    await call('Browser.close');
} finally {
    socket?.close(); server.close(); server.closeAllConnections();
    if (child.exitCode === null) child.kill();
}
