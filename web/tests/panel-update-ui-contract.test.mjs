import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const card = readFileSync(new URL('../src/components/PanelUpdateCard.tsx', import.meta.url), 'utf8');
const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const settings = readFileSync(new URL('../src/components/Settings.tsx', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');
const en = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');

test('update card is reachable only through the admin settings panel', () => {
    assert.match(settings, /role === 'admin'/);
    const adminBlock = settings.slice(settings.indexOf("{role === 'admin'"));
    assert.match(adminBlock, /<PanelUpdateCard\s*\/>/);
    assert.ok(adminBlock.indexOf('<PanelUpdateCard />') < adminBlock.indexOf("id=\"settings-dns-panel\""));
});

test('exact request marker is persisted before the only update POST', () => {
    const start = card.slice(card.indexOf('async function startUpdate()'));
    const stored = start.indexOf('storeMarker(exactMarker)');
    const posted = start.indexOf("fetch('/api/v1/panel/update/start'");
    assert.ok(stored >= 0, 'durable marker guard missing');
    assert.ok(posted > stored, 'POST must happen after durable marker write');
    assert.equal(card.match(/fetch\('\/api\/v1\/panel\/update\/start'/g)?.length, 1, 'POST must not be auto-retried');
    assert.match(start, /request_id: exactMarker\.request_id/);
    assert.match(start, /\.\.\.exactMarker\.target/);
});

test('polling survives restart and accepts only the exact request and target', () => {
    assert.match(card, /decodeUpdateStatus\(await response\.json\(\)\)/);
    assert.match(card, /status\?request_id=\$\{encodeURIComponent\(exactMarker\.request_id\)\}/);
    assert.match(card, /payload\.request_id !== exactMarker\.request_id/);
    assert.match(card, /!sameTarget\(payload\.target, exactMarker\.target\)/);
    assert.match(card, /Math\.min\(POLL_MAX_MS, Math\.round\(delay \* 1\.6\)\)/);
    assert.match(card, /clearExactMarker\(exactMarker\)/);
    assert.match(card, /payload\.status === 'succeeded' \|\| payload\.status === 'failed'/);
});

test('an exact successful update politely reloads once with a cache-busting identity', () => {
    const terminal = card.slice(card.indexOf(`payload.status === 'succeeded' || payload.status === 'failed'`));
    const successStart = terminal.indexOf(`if (payload.status === 'succeeded')`);
    const failureStart = terminal.indexOf('} else {', successStart);
    const terminalReturn = terminal.indexOf(`return 'terminal'`, failureStart);
    const success = terminal.slice(successStart, failureStart);
    const failure = terminal.slice(failureStart, terminalReturn);

    assert.match(success, /schedulePostUpdateReload\(exactMarker\)/);
    assert.match(success, /panelUpdate\.reloading/);
    assert.doesNotMatch(failure, /schedulePostUpdateReload/);
    assert.match(card, /const POST_UPDATE_RELOAD_MS = 1500/);
    assert.match(card, /reloadTimerRef\.current !== null/);
    assert.match(card, /searchParams\.get\(POST_UPDATE_RELOAD_PARAM\) === exactMarker\.request_id/);
    assert.match(card, /searchParams\.set\(POST_UPDATE_RELOAD_PARAM, exactMarker\.request_id\)/);
    assert.match(card, /searchParams\.delete\(POST_UPDATE_RELOAD_PARAM\)/);
    assert.match(card, /window\.history\.replaceState\(/);
    assert.match(card, /window\.location\.replace\(next\.toString\(\)\)/);
    assert.doesNotMatch(card, /window\.location\.reload\(/);
    assert.equal(card.match(/\bschedulePostUpdateReload\(/g)?.length, 1, 'reload may only be scheduled from exact success');
});

test('build identity reads bypass browser caches after a completed update', () => {
    assert.match(layout, /fetch\('\/api\/v1\/panel\/version', \{ cache: 'no-store', credentials: 'same-origin' \}\)/);
});

test('definitive refusal, durable absence and identity mismatch cannot wedge the browser', () => {
    assert.match(card, /const NOT_FOUND_GRACE_MS = 120000/);
    assert.ok(card.includes('definitiveStartRejection(response.status)'));
    assert.ok(card.includes('status === 400 || status === 401 || status === 403'));
    assert.ok(card.includes('Date.now() - exactMarker.created_at'));
    assert.ok(card.includes("t('panelUpdate.notAccepted')"));
    const mismatch = card.slice(card.indexOf("payload.request_id !== exactMarker.request_id"));
    assert.ok(mismatch.indexOf('clearExactMarker(exactMarker)') >= 0);
    assert.ok(mismatch.indexOf("return 'terminal'") > mismatch.indexOf('clearExactMarker(exactMarker)'));
});

test('server payloads and bounded summaries fail closed at the browser boundary', () => {
    assert.match(card, /function decodeUpdateCheck\(/);
    assert.match(card, /function decodeUpdateStatus\(/);
    assert.match(card, /value\.status !== 'queued'/);
    assert.match(card, /summary\.length <= 240/);
    assert.match(card, /!summary\.includes\('\:\/\/'\)/);
    assert.match(card, /value\.target !== undefined/);
});

test('the exact discovered target is visible and rapid clicks are blocked', () => {
    assert.match(card, /actionInFlight\.current \|\| marker/);
    assert.match(card, /disabled=\{starting\}/);
    assert.match(card, /t\('panelUpdate\.start', \{ version: target\.version \}\)/);
    assert.match(card, /\{t\('panelUpdate\.targetVersion'\)\}.*\{target\.version\}/s);
    assert.match(card, /\{t\('panelUpdate\.sequence'\)\}.*\{target\.sequence\}/s);
    assert.doesNotMatch(card, /confirmation|panel-update-confirmation|panelUpdate\.confirm/);
    assert.equal(card.match(/\bstartUpdate\(\)/g)?.length, 2, 'startUpdate may only be declared and invoked by the final button');
});

test('card carries alpha backup restart disclosure and safe response handling', () => {
    for (const phrase of ['Alpha sürüm', 'yedek', 'yeniden başlatır', 'hiçbir zaman otomatik başlamaz']) {
        assert.ok(tr.includes(phrase), `missing disclosure: ${phrase}`);
    }
    for (const status of [401, 403, 408, 409, 429, 500]) {
        assert.ok(card.includes(`status === ${status}`) || card.includes('status >= 500'), `missing HTTP ${status} handling`);
    }
    assert.match(card, /role="status" aria-live="polite"/);
    assert.match(card, /sm:grid-cols-2/);
    assert.match(card, /payload\.summary \|\|/);
});

test('update copy is localized and components contain no visible Turkish literals', () => {
    const keyPattern = /'panelUpdate\.([A-Za-z0-9]+)'\s*:/g;
    const trKeys = new Set([...tr.matchAll(keyPattern)].map((match) => match[1]));
    const enKeys = new Set([...en.matchAll(keyPattern)].map((match) => match[1]));
    assert.deepEqual([...trKeys].sort(), [...enKeys].sort());
    assert.ok(trKeys.size >= 30, 'complete update copy catalog missing');
    assert.doesNotMatch(card, /[ÇçĞğİıÖöŞşÜü]/, 'visible Turkish copy must live in the catalog');
    assert.match(card, /useI18n\(\)/);
});

test('browser never sends an update URL, path or command', () => {
    const postBody = card.slice(card.indexOf('body: JSON.stringify({'), card.indexOf('}),', card.indexOf('body: JSON.stringify({')));
    assert.doesNotMatch(postBody, /\b(url|path|command|args|environment)\b/i);
    for (const field of ['version', 'commit', 'sequence', 'os', 'arch', 'archive_sha256', 'archive_size']) {
        assert.ok(card.includes(field), `missing exact target field ${field}`);
    }
});
