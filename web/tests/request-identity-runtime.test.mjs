import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import {
  REASK_DELAY_MS, REQUEST_ID_HEADER, answerWasLost, identifyRequest, isReaskRoute, newRequestId, sendIdentified,
} from '../src/lib/requestIdentity.ts';
import { lostAnswerURL } from './fixtures/shared-layer.mjs';
import { apiErrorText } from '../src/lib/apiError.ts';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';

// D-029: every state-changing request carries one identity, made once per user
// action; on the eight routes of batch 1 a lost answer is asked for again once
// with the same identity.
// D-029: durum değiştiren her istek, kullanıcı eylemi başına bir kez üretilen
// tek bir kimlik taşır.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const idOf = (init) => new Headers(init?.headers).get(REQUEST_ID_HEADER);
const noWait = async () => {};

const eightRoutes = [
  '/api/v1/domains/12/backups/restore',
  '/api/v1/domains/12/backups',
  '/api/v1/domains/12/ssl/letsencrypt',
  '/api/v1/domains/12/databases',
  '/api/v1/database-servers/3/databases',
  '/api/v1/database-servers/3/admin-account',
  '/api/v1/vpn/peers',
  '/api/v1/import/cpanel/apply',
];

test('an identity is 32 lowercase hexadecimal characters and never repeats', () => {
  const ids = new Set(Array.from({ length: 200 }, newRequestId));
  assert.equal(ids.size, 200);
  for (const id of ids) assert.match(id, /^[0-9a-f]{32}$/);
});

test('every state-changing /api/ call gets the header once; reads and other addresses get none', () => {
  for (const method of ['POST', 'PUT', 'PATCH', 'DELETE']) {
    const identified = identifyRequest('/api/v1/domains/create', { method, headers: { 'Content-Type': 'application/json' }, body: '{}' });
    assert.match(identified.id, /^[0-9a-f]{32}$/, method);
    assert.equal(idOf(identified.init), identified.id);
    assert.equal(new Headers(identified.init.headers).get('Content-Type'), 'application/json', 'the caller\'s own headers were dropped');
    assert.equal(identified.init.body, '{}');
  }
  for (const [url, init] of [
    ['/api/v1/domains', undefined],
    ['/api/v1/domains', { method: 'GET' }],
    ['/api/v1/domains', { method: 'HEAD' }],
    ['/assets/app.js', { method: 'POST' }],
    ['https://updates.example/api/v1/x', { method: 'GET' }],
    // The header is never sent to another origin.
    ['https://another.example/api/v1/vpn/peers', { method: 'POST', body: '{}' }],
  ]) {
    const identified = identifyRequest(url, init);
    assert.equal(identified.id, null, url);
    assert.equal(identified.init, init, 'a request that needs no identity was rewritten');
  }
  // A new action is a new identity.
  const first = identifyRequest('/api/v1/vpn/peers', { method: 'POST', body: '{}' });
  const second = identifyRequest('/api/v1/vpn/peers', { method: 'POST', body: '{}' });
  assert.notEqual(first.id, second.id);
});

test('a request that already names itself keeps its own identity', () => {
  // The operation-row subsystems carry request_id in the body.
  const body = JSON.stringify({ service_id: 'nginx', request_id: 'a'.repeat(32) });
  const operation = identifyRequest('/api/v1/service/install', { method: 'POST', body });
  assert.equal(operation.id, null);
  assert.equal(idOf(operation.init), null, 'a second identity was added beside the body\'s own');
  // An empty or nested request_id is not an identity.
  for (const other of ['{"request_id":""}', '{"note":"\\"request_id\\""}', '{"inner":{"request_id":"x"}}', 'request_id=1']) {
    assert.match(identifyRequest('/api/v1/service/install', { method: 'POST', body: other }).id, /^[0-9a-f]{32}$/, other);
  }
  // A page that set the header itself keeps its value.
  const own = 'b'.repeat(32);
  const page = identifyRequest('/api/v1/import/cpanel/apply', { method: 'POST', headers: { [REQUEST_ID_HEADER]: own }, body: '{}' });
  assert.equal(page.id, own);
  assert.equal(idOf(page.init), own);
  assert.equal(page.reask, true);
});

test('only the eight routes of batch 1 are asked again', () => {
  for (const url of eightRoutes) {
    assert.equal(isReaskRoute('POST', url), true, url);
    assert.equal(isReaskRoute('POST', `${url}?x=1`), true, url);
    assert.equal(isReaskRoute('POST', `https://another.example${url}`), false, 'another origin was treated as this Panel');
    assert.equal(isReaskRoute('GET', url), false, url);
    assert.equal(isReaskRoute('DELETE', url), false, url);
  }
  for (const url of [
    '/api/v1/domains/12/backups/schedule', '/api/v1/domains/12/backups/restore/x', '/api/v1/domains/x/backups',
    '/api/v1/vpn/peers/4/ack', '/api/v1/import/cpanel/inspect', '/api/v1/service/install', '/api/v1/domains/create',
    '/api/v1/database-servers/3/users', '/api/v1/domains/12/ssl/upload',
  ]) assert.equal(isReaskRoute('POST', url), false, url);
});

test('a lost answer on the eight routes is asked for again once, with the same identity and the same body', async () => {
  for (const url of eightRoutes) {
    for (const lose of [
      () => Promise.reject(new TypeError('Failed to fetch')),
      () => Promise.resolve(new Response('', { status: 504 })),
      () => Promise.resolve(new Response('', { status: 502 })),
    ]) {
      const sent = [];
      const waits = [];
      const send = (input, init) => {
        sent.push({ input, id: idOf(init), body: init.body });
        return sent.length === 1 ? lose() : Promise.resolve(Response.json({ steps: [], first: true }));
      };
      const answer = await sendIdentified(send, url, { method: 'POST', body: '{"a":1}' }, async (ms) => { waits.push(ms); });
      assert.equal(sent.length, 2, `${url}: sent ${sent.length} times`);
      assert.match(sent[0].id, /^[0-9a-f]{32}$/);
      assert.equal(sent[1].id, sent[0].id, 'the second asking carried another identity');
      assert.equal(sent[1].body, sent[0].body);
      assert.deepEqual(waits, [REASK_DELAY_MS], 'the second asking did not wait');
      assert.equal(answer.status, 200);
      assert.deepEqual(await answer.json(), { steps: [], first: true }, 'the answer to the second asking was not used');
    }
  }
});

test('when the second asking fails too, the caller sees what it saw before; nothing is sent a third time', async () => {
  let sends = 0;
  await assert.rejects(
    sendIdentified(() => { sends++; return Promise.reject(new TypeError(`fetch failed ${sends}`)); },
      '/api/v1/import/cpanel/apply', { method: 'POST', body: '{}' }, noWait),
    /fetch failed 1/,
  );
  assert.equal(sends, 2);

  sends = 0;
  const gateway = await sendIdentified(() => { sends++; return Promise.resolve(new Response('', { status: 504 })); },
    '/api/v1/vpn/peers', { method: 'POST', body: '{}' }, noWait);
  assert.equal(gateway.status, 504);
  assert.equal(sends, 2);

  // The gateway answered, then the connection failed: the gateway's answer stands.
  sends = 0;
  const mixed = await sendIdentified(() => (++sends === 1 ? Promise.resolve(new Response('', { status: 502 })) : Promise.reject(new TypeError('down'))),
    '/api/v1/vpn/peers', { method: 'POST', body: '{}' }, noWait);
  assert.equal(mixed.status, 502);
  assert.equal(sends, 2);
});

test('an answer the Panel gave is never asked for again, and no other route is', async () => {
  for (const status of [200, 400, 409, 428, 500]) {
    let sends = 0;
    const answer = await sendIdentified(() => { sends++; return Promise.resolve(new Response('{}', { status })); },
      '/api/v1/domains/12/backups', { method: 'POST', body: '{}' }, noWait);
    assert.equal(answer.status, status);
    assert.equal(sends, 1, `status ${status} was asked for again`);
  }
  // Another state-changing route: identified, sent once, its failure passed on.
  let seen = null;
  await assert.rejects(sendIdentified((_input, init) => { seen = idOf(init); return Promise.reject(new TypeError('down')); },
    '/api/v1/domains/create', { method: 'POST', body: '{}' }, () => assert.fail('waited to ask again')), /down/);
  assert.match(seen, /^[0-9a-f]{32}$/);
  // A request the caller cancelled is not sent again.
  const controller = new AbortController();
  let cancelled = 0;
  await assert.rejects(sendIdentified(() => { cancelled++; controller.abort(); return Promise.reject(new DOMException('aborted', 'AbortError')); },
    '/api/v1/vpn/peers', { method: 'POST', body: '{}', signal: controller.signal }, noWait), /aborted/);
  assert.equal(cancelled, 1);
  // A read is passed through untouched.
  const init = { headers: { Accept: 'application/json' } };
  await sendIdentified((input, passed) => { assert.equal(passed, init); assert.equal(input, '/api/v1/domains'); return Promise.resolve(new Response('[]')); },
    '/api/v1/domains', init, noWait);
});

test('the Panel’s own refusal with a gateway status is its answer: shown, never asked for again', async () => {
  // An Agent that could not be reached is a 502 with a sentence and a code. On
  // the two routes whose answers are never stored, asking again would replace
  // that sentence with "already ended with an error, and that answer is not
  // kept".
  for (const status of [408, 429, 502, 503, 504]) {
    assert.equal(answerWasLost(new Response('<html>bad gateway</html>', { status, headers: { 'Content-Type': 'text/html' } })), true, `a gateway's ${status}`);
    assert.equal(answerWasLost(new Response('', { status })), true, `an empty ${status}`);
    assert.equal(answerWasLost(Response.json({ error: 'the Agent did not answer', code: 'AGENT_UNAVAILABLE' }, { status })), false, `the Panel's ${status}`);
    for (const url of ['/api/v1/vpn/peers', '/api/v1/database-servers/3/admin-account', '/api/v1/domains/12/ssl/letsencrypt']) {
      let sends = 0;
      const answer = await sendIdentified(() => { sends++; return Promise.resolve(Response.json({ error: 'the Agent did not answer', code: 'AGENT_UNAVAILABLE' }, { status })); },
        url, { method: 'POST', body: '{}' }, () => assert.fail('waited to ask again'));
      assert.equal(sends, 1, `${url}: the Panel's ${status} was asked for again`);
      assert.equal((await answer.json()).code, 'AGENT_UNAVAILABLE');
    }
  }
  for (const status of [200, 400, 409, 428, 500]) assert.equal(answerWasLost(new Response('', { status })), false, `${status}`);
});

test('one definition of a result that is not known, for every screen', async () => {
  const { unansweredCause } = await import(lostAnswerURL);
  const gateway = () => new Response('<html></html>', { status: 504, headers: { 'Content-Type': 'text/html' } });
  const refusal = (code, status = 409) => Response.json({ error: 'x', code }, { status });
  for (const url of eightRoutes) {
    // Identified: the interceptor asked once more before the screen hears of it.
    assert.equal(await unansweredCause('POST', url, null), 'asked', url);
    assert.equal(await unansweredCause('POST', url, gateway()), 'asked', url);
    assert.equal(await unansweredCause('POST', url, refusal('REQUEST_OUTCOME_UNKNOWN')), 'interrupted', url);
    assert.equal(await unansweredCause('POST', url, refusal('REQUEST_IN_PROGRESS')), 'running', url);
    // Every other answer is a result: a success, a refusal, a status-only answer.
    for (const answer of [Response.json({ ok: true }), refusal('REQUEST_ID_REUSED'), refusal('REQUEST_COMPLETED_RESULT_NOT_RETAINED'),
      refusal('BACKUP_RESTORE_IN_PROGRESS'), refusal('REQUEST_ID_REQUIRED', 428), refusal('AGENT_UNAVAILABLE', 502), new Response('conflict', { status: 409 })]) {
      assert.equal(await unansweredCause('POST', url, answer), null, url);
    }
  }
  // Not identified: nothing was sent again, and the Panel's 409 is never read as "unknown".
  for (const [method, url] of [['POST', '/api/v1/domains/12/aliases'], ['DELETE', '/api/v1/domains/12/backups'], ['PUT', '/api/v1/mail/policy']]) {
    assert.equal(await unansweredCause(method, url, null), 'dropped', url);
    assert.equal(await unansweredCause(method, url, gateway()), 'dropped', url);
    assert.equal(await unansweredCause(method, url, refusal('REQUEST_OUTCOME_UNKNOWN')), null, url);
  }
  // The answer is still the caller's to read.
  const kept = refusal('REQUEST_OUTCOME_UNKNOWN');
  await unansweredCause('POST', eightRoutes[0], kept);
  assert.equal((await kept.json()).code, 'REQUEST_OUTCOME_UNKNOWN');
});

test('a notice never says "nothing was sent a second time" for a change that was asked for again', () => {
  const ui = read('../src/components/ui.tsx');
  assert.match(ui, /const identified = lost\.cause !== 'dropped';/);
  assert.match(ui, /lost\.cause === 'asked' \? 'common\.lostAsked' : lost\.cause === 'running' \? 'common\.lostRunning' : 'common\.lostInterrupted'/);
  for (const [name, catalogue, twice, again] of [
    ['English', en, /sent a second time/, /asking the server once more/],
    ['Turkish', tr, /ikinci kez gönderil/, /bir kez daha istendiğinde/],
  ]) {
    for (const key of ['common.lostAsked', 'common.lostInterrupted', 'common.lostRunning', 'common.lostStateReading',
      'common.lostStateRead', 'common.lostStateUnread', 'common.lostStateMade', 'common.lostStateNotMade']) {
      assert.equal(typeof catalogue[key], 'string', `${name} has no ${key}`);
      assert.doesNotMatch(catalogue[key], twice, `${name} ${key} says nothing was sent a second time`);
    }
    assert.match(catalogue['common.lostAsked'], again, `${name} does not say the answer was asked for again`);
    for (const key of ['common.lostStateRead', 'common.lostStateMade', 'common.lostStateNotMade']) assert.match(catalogue[key], /\{time\}/, `${name} ${key}`);
  }
  // Every screen of the eight routes sends through the hook that knows the cause, or uses its one definition.
  for (const [file, pattern] of [
    ['DomainBackupManager.tsx', /answer\.send\(`\/api\/v1\/domains\/\$\{domainId\}\/backups\/restore`/],
    ['DomainBackupManager.tsx', /answer\.send\(`\/api\/v1\/domains\/\$\{domainId\}\/backups`/],
    ['DomainSSLSettings.tsx', /issueAnswer\.send\(`\/api\/v1\/domains\/\$\{domainId\}\/ssl\/letsencrypt`/],
    ['DomainDatabaseManager.tsx', /answer\.send\(`\/api\/v1\/domains\/\$\{domainId\}\/databases`/],
    ['AddDatabaseModalV2.tsx', /answer\.send\(`\/api\/v1\/database-servers\/\$\{serverId\}\/databases`/],
    ['DatabaseAccountStrip.tsx', /answer\.send\(`\$\{API_BASE\}\/database-servers\/\$\{server\.id\}\/admin-account`, \{\s+method: 'POST'/],
    ['VPNPage.tsx', /answer\.send\('\/api\/v1\/vpn\/peers'/],
    ['ImportPage.tsx', /await unansweredCause\('POST', '\/api\/v1\/import\/cpanel\/apply', res\)/],
  ]) assert.match(read(`../src/components/${file}`), pattern, `${file} sends its identified change around the shared lost-answer handling`);
});

test('the one fetch interceptor sends every call through the identity', () => {
  const app = read('../src/App.tsx');
  assert.match(app, /import \{ sendIdentified \} from '\.\/lib\/requestIdentity';/);
  assert.match(app, /const res = await sendIdentified\(originalFetch, args\[0\], args\[1\]\);/);
  assert.equal([...app.matchAll(/originalFetch\(/g)].length, 0, 'a call still reaches the network around the identity');
  assert.equal([...app.matchAll(/window\.fetch = /g)].length, 2, 'there is more than one interceptor (one assignment installs it, one removes it)');
});

test('the refusals have their sentences in English and Turkish, in the shell catalogue', () => {
  const t = (catalogue) => (key, vars) => {
    const text = catalogue[key];
    return text === undefined ? key : text.replace(/\{(\w+)\}/g, (_, name) => String(vars?.[name] ?? `{${name}}`));
  };
  const codes = ['REQUEST_ID_REQUIRED', 'REQUEST_ID_REUSED', 'REQUEST_IN_PROGRESS', 'REQUEST_OUTCOME_UNKNOWN',
    'REQUEST_COMPLETED_RESULT_NOT_RETAINED', 'BACKUP_RESTORE_IN_PROGRESS'];
  for (const [name, catalogue, reload] of [['English', en, /Reload the page|reload the page/], ['Turkish', tr, /[Ss]ayfayı yeniden yükle/]]) {
    for (const code of codes) {
      const text = apiErrorText({ message: 'server English', code, vars: { request_id: 'c'.repeat(32) } }, t(catalogue));
      assert.notEqual(text, 'server English', `${name} has no sentence for ${code}`);
      assert.doesNotMatch(text, /[A-Z]{3,}_[A-Z_]+|X-CelikPanel|[0-9a-f]{32}/, `${name} ${code} shows an internal name`);
      if (code !== 'BACKUP_RESTORE_IN_PROGRESS') assert.match(text, reload, `${name} ${code} names no next action`);
    }
    // The failed first attempt has its own sentence: it does not say the change was made.
    const made = apiErrorText({ message: '', code: 'REQUEST_COMPLETED_RESULT_NOT_RETAINED' }, t(catalogue));
    const failed = apiErrorText({ message: '', code: 'REQUEST_COMPLETED_RESULT_NOT_RETAINED', reason: 'failed' }, t(catalogue));
    assert.notEqual(failed, made);
  }
  assert.match(en['err.REQUEST_COMPLETED_RESULT_NOT_RETAINED'], /already made/);
  assert.match(en['err.REQUEST_COMPLETED_RESULT_NOT_RETAINED.failed'], /ended with an error/);
  assert.doesNotMatch(en['err.REQUEST_COMPLETED_RESULT_NOT_RETAINED.failed'], /already made/);
  assert.match(en['err.REQUEST_IN_PROGRESS'], /not started a second time/);
  assert.match(en['err.REQUEST_OUTCOME_UNKNOWN'], /not known/);
  assert.match(tr['err.REQUEST_OUTCOME_UNKNOWN'], /bilinmiyor/);
  assert.match(tr['err.REQUEST_IN_PROGRESS'], /İkinci kez başlatılmadı/);
  // The server's English originals are the sentences the screen shows.
  const guard = read('../../cmd/panel/request_identity.go').replace(/" \+\s+"/g, '');
  for (const key of ['REQUEST_ID_REUSED', 'REQUEST_IN_PROGRESS', 'REQUEST_OUTCOME_UNKNOWN',
    'REQUEST_COMPLETED_RESULT_NOT_RETAINED', 'REQUEST_COMPLETED_RESULT_NOT_RETAINED.failed']) {
    assert.ok(guard.includes(`"${en[`err.${key}`]}"`), `the server's message for ${key} is not the English sentence`);
  }
});

test('the import page names its request, and a start after a lost answer is the same request', () => {
  const page = read('../src/components/ImportPage.tsx');
  assert.match(page, /const request = \(unknownResult && applyRequest\.current\) \|\| \{\s+id: newRequestId\(\),/);
  assert.match(page, /headers: \{ 'Content-Type': 'application\/json', \[REQUEST_ID_HEADER\]: request\.id \},\s+body: request\.body,/);
  // "Still running" and "the Panel restarted" are unknown results with their
  // own sentence; only the second makes a later start a new request.
  assert.match(page, /if \(why === 'interrupted'\) applyRequest\.current = null;/);
  assert.match(page, /running: 'import\.unknown\.bodyRunning',\s+interrupted: 'import\.unknown\.bodyInterrupted',/);
  assert.doesNotMatch(page, /\[408, 429, 502, 503, 504\]/, 'the import page has its own definition of a lost answer');
});
