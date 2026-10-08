import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';

// The real ComponentOperationProvider, driven through the end of an install:
// the operation reports success and the follow-up catalogue scan is answered
// in different ways. What these tests hold (fail-closed contract, 2026-10-08):
//
//   - the overlay is released and the stored operation cleared exactly once,
//     and only after a snapshot that confirms the operation;
//   - while a server setup owns the host the scan is refused
//     (409 server_setup_busy); the scan the operation itself stored is read
//     instead, with no new host probe, and must confirm the operation too;
//   - every other refused, failed or unreadable reply keeps the overlay and
//     the stored operation, and is retried.
//
// Component tests only: the Panel's replies are scripted here.
//
// Gercek saglayici, kurulumun sonuna kadar surulur. Katman yalnizca islemi
// dogrulayan bir snapshot'tan sonra ve tam bir kez birakilir; kurulum makineyi
// tutarken islemin kendi sakladigi tarama okunur; diger her yanit kilidi korur.
const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const dataModule = source => 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');
const read = path => readFileSync(new URL(path, import.meta.url), 'utf8');
const compile = text => ts.transpileModule(text, { compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 } }).outputText;

// The setup refusal is read by the product's own function.
const handoverURL = dataModule(compile(read('../src/lib/panelHandover.ts')));
const stub = dataModule(`import React from '${reactURL}';
 const fixture = () => globalThis.operationTest;
 export const useI18n = () => ({ t: (key, vars) => (vars ? key + ' ' + JSON.stringify(vars) : key) });
 export const readApiError = async response => ({ message: 'error ' + response.status });
 export const useNavigationBlocker = () => {};
 export const showToast = (kind, message) => { fixture().toasts.push([kind, message]); };
 export const createPortal = node => node;
 // The tracker tells the shared read of the component records to read again
 // when an operation has ended; the test records each call.
 export const refreshRemote = url => { (fixture().refreshed ??= []).push(url); };
 export default function OperationOverlay(props) { return React.createElement('operation-overlay', props); }
`);
const providerURL = dataModule(`import React from '${reactURL}';\n` + compile(read('../src/components/ComponentOperation.tsx'))
  .replace(/from ['"]([^'"]+)['"]/g, (_, specifier) => `from '${specifier === 'react' ? reactURL : specifier.endsWith('/panelHandover') ? handoverURL : stub}'`)
  .replace(/import\(['"]([^'"]+)['"]\)/g, () => `import('${stub}')`));
const { ComponentOperationProvider, useComponentOperation } = await import(providerURL);

const ID = 'a'.repeat(32), REQUEST = 'b'.repeat(32);
const STARTED = '2026-10-08T10:00:00.400Z', FINISHED = '2026-10-08T10:00:42.250Z';
const KEYS = ['celikpanel.components.operation-id', 'celikpanel.components.operation-label', 'celikpanel.components.operation-recovery'];
const SCAN = 'POST /api/v1/managed-services/scan', STORED = 'GET /api/v1/managed-services', STATUS = `GET /api/v1/service/operation?id=${ID}`;
const POLL_DELAY_MS = 1500, RETRY_DELAY_MS = 3000;

const operation = (status, extra = {}) => ({ operation: {
  id: ID, request_id: REQUEST, kind: 'service_install', service_id: 'nginx', status, phase: status === 'succeeded' ? 'succeeded' : 'installing',
  started_at: STARTED, ...(status === 'running' ? {} : { finished_at: FINISHED }), ...extra,
} });
const service = (id, installed) => ({ id, name: id, description: `${id} service`, icon: 'box', category: 'web', status: installed ? 'active' : 'inactive', is_installed: installed, versions: [] });
const snapshot = (scannedAt, installed = true) => ({
  services: [service('nginx', installed), service('postfix', false)],
  profiles: ['core-mail', 'webmail', 'protected-mail'].map(id => ({ id, name: id, description: `${id} profile`, services: ['postfix'], status: 'available', available: true, verified: false, latest_attempt_status: 'none' })),
  dns_identity_ready: true,
  mail_hostname: { current: 'host', current_usable: false, hostname: '', source: '', will_set_hostname: false },
  scanned_at: scannedAt,
});
const json = (body, status = 200) => () => Response.json(body, { status });
const setupBusy = json({ error: 'Server setup is in progress. Follow its current operation.', code: 'server_setup_busy', action: '/setup' }, 409);

let tree, timers, calls, routes, seen, storage;
const realTimeout = setTimeout;
globalThis.HTMLElement = class {};
const root = { attributes: new Map(), hasAttribute(name) { return this.attributes.has(name); }, getAttribute(name) { return this.attributes.get(name) ?? null; }, setAttribute(name, value) { this.attributes.set(name, value); }, removeAttribute(name) { this.attributes.delete(name); } };
globalThis.document = Object.assign(new EventTarget(), { visibilityState: 'visible', activeElement: null, body: {}, getElementById: id => (id === 'root' ? root : null) });
globalThis.window = Object.assign(new EventTarget(), {
  // Every product timer is fired by hand, so a retry is an observable event.
  setTimeout: (fn, ms) => { const timer = { fn, ms, live: true }; timers.push(timer); return timer; },
  clearTimeout: timer => { if (timer) timer.live = false; },
});
globalThis.sessionStorage = { getItem: key => (storage.has(key) ? storage.get(key) : null), setItem: (key, value) => { storage.set(key, String(value)); }, removeItem: key => { seen.removed.push(key); storage.delete(key); } };
const originalFetch = globalThis.fetch;

function Probe() {
  const value = useComponentOperation();
  seen.snapshots.push(value.catalogSnapshot);
  seen.value = value;
  return null;
}

// routes: { 'METHOD url': [reply, ...] }; the last reply of a route repeats.
async function open(script) {
  timers = []; calls = []; routes = script; root.attributes.clear();
  seen = { snapshots: [], removed: [], value: null };
  storage = new Map([
    [KEYS[0], ID], [KEYS[1], 'Nginx'],
    [KEYS[2], JSON.stringify({ version: 3, operation_kind: 'service_install', request_id: REQUEST, service_id: 'nginx', label: 'Nginx', created_at: Date.parse(STARTED) })],
  ]);
  globalThis.operationTest = { toasts: [] };
  globalThis.fetch = async (url, init = {}) => {
    const key = `${init.method || 'GET'} ${url}`;
    calls.push(key);
    const replies = routes[key];
    if (!replies) throw new Error(`unexpected request ${key}`);
    const reply = replies.length > 1 ? replies.shift() : replies[0];
    return reply();
  };
  await act(async () => { tree = Renderer.create(React.createElement(ComponentOperationProvider, null, React.createElement(Probe))); });
  await settle();
}
// Lets the scripted replies, the lazy overlay and the state updates run out.
const settle = async () => { for (let round = 0; round < 6; round++) await act(async () => { await new Promise(resolve => realTimeout(resolve, 0)); }); };
const pending = () => timers.filter(timer => timer.live).map(timer => timer.ms);
const fire = async () => { const due = timers.filter(timer => timer.live); for (const timer of due) timer.live = false; await act(async () => { for (const timer of due) timer.fn(); }); await settle(); return due.length; };
const overlay = () => tree.root.findAllByType('operation-overlay').find(node => !node.props.failure) ?? null;
const count = key => calls.filter(call => call === key).length;
const cleared = () => KEYS.every(key => !storage.has(key));
const clears = () => seen.removed.filter(key => key === KEYS[0]).length;
async function close() { if (tree) await act(async () => tree.unmount()); tree = undefined; globalThis.fetch = originalFetch; delete globalThis.operationTest; }

function assertHeld(message) {
  const layer = overlay();
  assert.ok(layer, `${message}: the overlay is gone`);
  assert.equal(layer.props.interrupted, true, `${message}: the unverifiable reply is not marked`);
  assert.equal(layer.props.refreshing, true, message);
  assert.equal(seen.value.locked, true, message);
  assert.equal(root.hasAttribute('inert'), true, message);
  assert.equal(storage.get(KEYS[0]), ID, `${message}: the stored operation was cleared`);
  assert.ok(storage.has(KEYS[2]), `${message}: the recovery marker was cleared`);
  assert.equal(clears(), 0, message);
  assert.equal(seen.value.catalogSnapshot, null, `${message}: a snapshot was published`);
  assert.equal(seen.value.failure, null, message);
  assert.deepEqual(globalThis.operationTest.toasts, [], `${message}: a result was announced`);
  assert.deepEqual(pending(), [RETRY_DELAY_MS], `${message}: the retry is not scheduled`);
  // An end that is not verified changes what no screen shows: nothing is told to read again.
  assert.equal(globalThis.operationTest.refreshed, undefined, `${message}: the shared component records were re-read for an unverified end`);
}
function assertReleased(scannedAt, message) {
  assert.equal(overlay(), null, `${message}: the overlay is still shown`);
  assert.equal(seen.value.locked, false, message);
  assert.equal(seen.value.operation, null, message);
  assert.equal(seen.value.failure, null, message);
  assert.equal(root.hasAttribute('inert'), false, message);
  assert.ok(cleared(), `${message}: the stored operation is still there`);
  assert.equal(clears(), 1, `${message}: the stored operation must be cleared exactly once`);
  assert.equal(seen.value.catalogSnapshot?.scanned_at, scannedAt, `${message}: the confirming snapshot was not published`);
  assert.equal(seen.value.catalogSnapshot.services.find(item => item.id === 'nginx').is_installed, true, message);
  // Published once, and before the overlay went: no page shows the earlier catalogue.
  assert.equal(new Set(seen.snapshots.filter(Boolean).map(item => item.scanned_at)).size, 1, message);
  assert.deepEqual(globalThis.operationTest.toasts, [['success', 'services.installed {"name":"Nginx"}']], message);
  assert.deepEqual(pending(), [], `${message}: something is still scheduled`);
  // The verified end tells every screen that shows the stored component records
  // to read them again: once for the operation, not once per poll (open item of
  // 2026-10-09: a page opened after an install listed the earlier scan for 30 s).
  assert.deepEqual(globalThis.operationTest.refreshed, ['/api/v1/managed-services'], `${message}: the shared component records must be re-read exactly once`);
}

test('success and an accepted scan: released once, on the fresh snapshot', async () => {
  const fresh = '2026-10-08T10:00:43.000Z';
  await open({ [STATUS]: [json(operation('running')), json(operation('succeeded'))], [SCAN]: [json(snapshot(fresh))] });
  try {
    // Still running: the overlay is up, nothing is scanned or cleared.
    assert.ok(overlay());
    assert.equal(overlay().props.interrupted, false);
    assert.equal(count(SCAN), 0);
    assert.equal(storage.get(KEYS[0]), ID);
    assert.deepEqual(pending(), [POLL_DELAY_MS]);
    await fire();
    assertReleased(fresh, 'accepted scan');
    assert.deepEqual(calls, [STATUS, STATUS, SCAN]);
    assert.equal(count(STORED), 0, 'the stored scan is read only on the setup refusal');
    assert.equal(await fire(), 0);
  } finally { await close(); }
});

test('success and a scan refused because setup owns the host: the operation\'s own stored scan confirms it', async () => {
  // The Panel stores the scan to the second: taken at 10:00:41, during the operation.
  const stored = '2026-10-08T10:00:41Z';
  await open({ [STATUS]: [json(operation('succeeded'))], [SCAN]: [setupBusy], [STORED]: [json(snapshot(stored))] });
  try {
    assertReleased(stored, 'setup refusal');
    // One refused scan, one read of the stored scan: no retry, no second probe of the host.
    assert.deepEqual(calls, [STATUS, SCAN, STORED]);
    assert.equal(await fire(), 0);
    assert.deepEqual(calls, [STATUS, SCAN, STORED]);
  } finally { await close(); }
});

test('an operation that started and scanned within one second is confirmed by its stored scan', async () => {
  // started 10:00:00.400, scanned and stored as 10:00:00, finished 10:00:00.900
  const extra = { finished_at: '2026-10-08T10:00:00.900Z' };
  await open({ [STATUS]: [json(operation('succeeded', extra))], [SCAN]: [setupBusy], [STORED]: [json(snapshot('2026-10-08T10:00:00Z'))] });
  try { assertReleased('2026-10-08T10:00:00Z', 'same second'); } finally { await close(); }
});

test('the setup refusal alone releases nothing: the stored scan must be readable and confirm the operation', async () => {
  const unconfirmed = {
    'older than the operation': json(snapshot('2026-10-08T09:59:59Z')),
    'component not installed': json(snapshot('2026-10-08T10:00:41Z', false)),
    'component unknown': json({ ...snapshot('2026-10-08T10:00:41Z'), services: [service('postfix', false)] }),
    'never scanned': json({ ...snapshot('2026-10-08T10:00:41Z'), scanned_at: null }),
    'not a snapshot': json({ services: [] }),
    'unverified cache': json({ error: 'cached service state could not be verified; run a fresh scan', code: 'service_state_unverified' }, 503),
    'refused too': setupBusy,
    'not JSON': () => new Response('<html>', { status: 200 }),
    'no reply': () => { throw new TypeError('network'); },
  };
  for (const [name, reply] of Object.entries(unconfirmed)) {
    await open({ [STATUS]: [json(operation('succeeded'))], [SCAN]: [setupBusy], [STORED]: [reply] });
    try {
      assertHeld(name);
      assert.deepEqual(calls, [STATUS, SCAN, STORED], name);
      // The retry asks again in the same order and is held again.
      await fire();
      assertHeld(`${name}, retried`);
      assert.deepEqual(calls, [STATUS, SCAN, STORED, STATUS, SCAN, STORED], name);
    } finally { await close(); }
  }
});

test('held on an unconfirmed stored scan, then released when setup ends and the scan is accepted', async () => {
  const fresh = '2026-10-08T10:05:00.000Z';
  await open({ [STATUS]: [json(operation('succeeded'))], [SCAN]: [setupBusy, json(snapshot(fresh))], [STORED]: [json(snapshot('2026-10-08T09:59:59Z'))] });
  try {
    assertHeld('stale stored scan');
    await fire();
    assertReleased(fresh, 'after setup');
    assert.deepEqual(calls, [STATUS, SCAN, STORED, STATUS, SCAN]);
  } finally { await close(); }
});

test('success and a scan that fails any other way stays fail-closed', async () => {
  const failing = {
    '409 with another code': json({ error: 'another package operation is already in progress', code: 'service_operation_busy' }, 409),
    '409 without a body': () => new Response('', { status: 409 }),
    '409 with unreadable JSON': () => new Response('{', { status: 409 }),
    'server_setup_busy on another status': json({ code: 'server_setup_busy' }, 503),
    '500': json({ error: 'scan failed' }, 500),
    '429': json({ error: 'slow down' }, 429),
    '403': json({ error: 'forbidden', code: 'license_required' }, 403),
    'accepted but not JSON': () => new Response('<html>', { status: 200 }),
    'accepted but not a snapshot': json({ services: 'nginx' }),
    'accepted but older than the operation': json(snapshot('2026-10-08T10:00:41.000Z')),
    'accepted but the component is not installed': json(snapshot('2026-10-08T10:00:43.000Z', false)),
    'no reply': () => { throw new TypeError('network'); },
  };
  for (const [name, reply] of Object.entries(failing)) {
    await open({ [STATUS]: [json(operation('succeeded'))], [SCAN]: [reply] });
    try {
      assertHeld(name);
      assert.deepEqual(calls, [STATUS, SCAN], `${name}: only the setup refusal may read the stored scan`);
      await fire();
      assertHeld(`${name}, retried`);
      assert.equal(count(STORED), 0, name);
    } finally { await close(); }
  }
});

test('a scan that needs a new sign-in keeps the stored operation and schedules nothing', async () => {
  for (const script of [{ [SCAN]: [json({ error: 'unauthorized' }, 401)] }, { [SCAN]: [setupBusy], [STORED]: [json({ error: 'unauthorized' }, 401)] }]) {
    await open({ [STATUS]: [json(operation('succeeded'))], ...script });
    try {
      assert.ok(overlay());
      assert.equal(storage.get(KEYS[0]), ID);
      assert.ok(storage.has(KEYS[2]));
      assert.equal(clears(), 0);
      assert.equal(seen.value.catalogSnapshot, null);
      assert.deepEqual(globalThis.operationTest.toasts, []);
      assert.deepEqual(pending(), []);
    } finally { await close(); }
  }
});

test('a failed operation never takes the stored scan: the setup refusal keeps its error and the lock', async () => {
  const failed = operation('failed', { error: { code: 'service_install_failed', message: 'nginx is inactive' } });
  await open({ [STATUS]: [json(failed)], [SCAN]: [setupBusy] });
  try {
    assert.ok(overlay());
    assert.equal(overlay().props.operation.status, 'failed');
    assert.equal(overlay().props.interrupted, true);
    assert.deepEqual(calls, [STATUS, SCAN]);
    assert.equal(storage.get(KEYS[0]), ID);
    assert.equal(clears(), 0);
    assert.equal(seen.value.failure, null, 'a failure is published only with a fresh snapshot');
    assert.deepEqual(pending(), [RETRY_DELAY_MS]);
    await fire();
    assert.equal(count(STORED), 0);
    assert.equal(count(SCAN), 2);
  } finally { await close(); }
});
