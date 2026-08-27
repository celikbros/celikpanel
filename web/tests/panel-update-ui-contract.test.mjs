import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const card = readFileSync(new URL('../src/components/PanelUpdateCard.tsx', import.meta.url), 'utf8');
const tracker = readFileSync(new URL('../src/components/SystemUpdateOperation.tsx', import.meta.url), 'utf8');
const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8');
const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const settings = readFileSync(new URL('../src/components/Settings.tsx', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');
const en = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const updateSources = card + tracker;

test('update card is reachable only through the admin settings panel', () => {
    assert.match(settings, /role === 'admin'/);
    const adminBlock = settings.slice(settings.indexOf("{role === 'admin'"));
    assert.match(adminBlock, /<PanelUpdateCard\s*\/>/);
    assert.ok(adminBlock.indexOf('<PanelUpdateCard />') < adminBlock.indexOf('id="settings-dns-panel"'));
});

test('exact request marker is persisted by the root tracker before the only update POST', () => {
    const start = card.slice(card.indexOf('async function startUpdate()'));
    const delegated = start.indexOf('systemUpdate.begin(exactMarker)');
    const posted = start.indexOf("fetch('/api/v1/panel/update/start'");
    assert.ok(delegated >= 0, 'root tracker handoff missing');
    assert.ok(posted > delegated, 'POST must happen after durable root-tracker handoff');
    assert.equal(updateSources.match(/fetch\('\/api\/v1\/panel\/update\/start'/g)?.length, 1, 'POST must not be auto-retried');
    assert.match(start, /request_id: exactMarker\.request_id/);
    assert.match(start, /\.\.\.exactMarker\.target/);

    const begin = tracker.slice(tracker.indexOf('const begin = useCallback'));
    assert.ok(begin.indexOf('storeMarker(exactMarker)') >= 0, 'durable marker write missing');
    assert.ok(begin.indexOf('setMarker(exactMarker)') > begin.indexOf('storeMarker(exactMarker)'));
    assert.match(tracker, /localStorage\.setItem\(SYSTEM_UPDATE_MARKER_KEY, encoded\)/);
    assert.match(tracker, /localStorage\.getItem\(SYSTEM_UPDATE_MARKER_KEY\) === encoded/);
});

test('root polling survives restart, focus and route changes while accepting only the exact identity', () => {
    assert.match(tracker, /decodeUpdateStatus\(await response\.json\(\)\)/);
    assert.match(tracker, /status\?request_id=\$\{encodeURIComponent\(marker\.request_id\)\}/);
    assert.match(tracker, /payload\.request_id !== marker\.request_id/);
    assert.match(tracker, /!sameUpdateTarget\(payload\.target, marker\.target\)/);
    assert.match(tracker, /Math\.min\(POLL_MAX_MS, Math\.round\(delay \* 1\.6\)\)/);
    assert.match(tracker, /window\.addEventListener\('focus', wake\)/);
    assert.match(tracker, /window\.addEventListener\('online', wake\)/);
    assert.match(tracker, /window\.addEventListener\('pageshow', wake\)/);
    assert.match(tracker, /document\.addEventListener\('visibilitychange', onVisibility\)/);
    assert.match(tracker, /payload\.status === 'succeeded'/);
    assert.match(tracker, /payload\.status === 'failed'/);
    assert.equal(updateSources.match(/fetch\(\x60\/api\/v1\/panel\/update\/status/g)?.length, 1);
});

test('self-update owns its exact root tracker without joining the service-operation discovery channel', () => {
    assert.match(tracker, /const SYSTEM_UPDATE_MARKER_KEY = 'celikpanel\.system-update-operation\.v1'/);
    assert.match(tracker, /status\?request_id=\$\{encodeURIComponent\(marker\.request_id\)\}/);
    assert.doesNotMatch(updateSources, /useComponentOperation|service\/operation\?active=1/);
});

test('the tracker is mounted above routes and remains a single modal interaction owner', () => {
    const provider = app.indexOf('<SystemUpdateOperationProvider>');
    const componentOperations = app.indexOf('<ComponentOperationProvider>', provider);
    const routes = app.indexOf('<AppRoutes />', componentOperations);
    assert.ok(provider >= 0 && componentOperations > provider && routes > componentOperations);
    assert.equal(app.match(/<SystemUpdateOperationProvider>/g)?.length, 1);
    assert.match(tracker, /marker !== null \|\| terminal !== null/);
    assert.match(tracker, /role="dialog"/);
    assert.match(tracker, /aria-modal="true"/);
    assert.match(tracker, /className="fixed inset-0 z-\[110\]/);
    assert.match(tracker, /application\.inert = true/);
    assert.match(tracker, /document\.body\.style\.overflow = 'hidden'/);
    assert.match(tracker, /document\.addEventListener\('focusin', keepFocusInDialog\)/);
    assert.match(tracker, /event\.key === 'Escape' \|\| \(marker && event\.key === 'Tab'\)/);
});

test('a saved marker resumes after reload and another tab can adopt it without replacing an active identity', () => {
    assert.match(tracker, /useState<UpdateMarker \| null>\(\(\) => readMarker\(\)\)/);
    assert.match(tracker, /window\.addEventListener\('storage', onStorage\)/);
    assert.match(tracker, /event\.key !== SYSTEM_UPDATE_MARKER_KEY \|\| !event\.newValue/);
    assert.match(tracker, /if \(!incoming \|\| markerRef\.current\) return/);
    assert.match(tracker, /message: t\('panelUpdate\.running'\)/);
});

test('an exact successful update reloads once with a cache-busting identity', () => {
    const success = tracker.slice(tracker.indexOf("if (outcome.kind === 'succeeded')"), tracker.indexOf("if (outcome.kind === 'failed')"));
    assert.match(success, /clearExactMarker\(exactMarker\)/);
    assert.match(success, /schedulePostUpdateReload\(exactMarker\)/);
    assert.match(success, /panelUpdate\.reloading/);
    assert.match(tracker, /const POST_UPDATE_RELOAD_MS = 1500/);
    assert.match(tracker, /reloadTimerRef\.current !== null/);
    assert.match(tracker, /searchParams\.get\(POST_UPDATE_RELOAD_PARAM\) === exactMarker\.request_id/);
    assert.match(tracker, /searchParams\.set\(POST_UPDATE_RELOAD_PARAM, exactMarker\.request_id\)/);
    assert.match(tracker, /searchParams\.delete\(POST_UPDATE_RELOAD_PARAM\)/);
    assert.match(tracker, /window\.history\.replaceState\(/);
    assert.match(tracker, /window\.location\.replace\(next\.toString\(\)\)/);
    assert.doesNotMatch(updateSources, /window\.location\.reload\(/);
    assert.equal(tracker.match(/\bschedulePostUpdateReload\(/g)?.length, 1, 'reload may only be scheduled from exact success');
});

test('terminal failure remains explicit until acknowledgement and never reloads', () => {
    const failure = tracker.slice(tracker.indexOf("if (outcome.kind === 'failed')"), tracker.indexOf('setView({', tracker.indexOf("if (outcome.kind === 'failed')")));
    assert.match(failure, /kind: 'failed'/);
    assert.match(failure, /reloadScheduled: false/);
    assert.doesNotMatch(failure, /schedulePostUpdateReload/);
    assert.match(tracker, /terminal\.kind === 'failed'/);
    assert.match(tracker, /onClick=\{dismissTerminal\}/);
    assert.match(tracker, /t\('dnssrv\.continue'\)/);
    assert.match(tracker, /payload\.summary \|\| t\('panelUpdate\.failed'\)/);
});

test('build identity reads bypass browser caches after a completed update', () => {
    assert.match(layout, /fetch\('\/api\/v1\/panel\/version', \{ cache: 'no-store', credentials: 'same-origin' \}\)/);
});

test('definitive refusal, durable absence and identity mismatch cannot wedge the browser', () => {
    assert.match(tracker, /const NOT_FOUND_GRACE_MS = 120000/);
    assert.ok(card.includes('definitiveStartRejection(response.status)'));
    assert.ok(card.includes('status === 400 || status === 401 || status === 403'));
    assert.ok(tracker.includes('Date.now() - marker.created_at'));
    assert.ok(tracker.includes("t('panelUpdate.notAccepted')"));
    assert.match(card, /systemUpdate\.rejectStart\(exactMarker, failure\)/);
    const mismatch = tracker.slice(tracker.indexOf('payload.request_id !== marker.request_id'));
    assert.ok(mismatch.indexOf("kind: 'failed'") >= 0);
    assert.ok(mismatch.indexOf("t('panelUpdate.identityMismatchCleared')") >= 0);
});

test('server payloads, stored markers and bounded summaries fail closed at the browser boundary', () => {
    assert.match(card, /function decodeUpdateCheck\(/);
    assert.match(tracker, /function decodeUpdateStatus\(/);
    assert.match(tracker, /value\.status !== 'queued'/);
    assert.match(tracker, /summary\.length <= 240/);
    assert.match(tracker, /!summary\.includes\('\:\/\/'\)/);
    assert.match(card, /value\.target !== undefined/);
    assert.match(tracker, /function decodeMarker\(/);
    assert.match(tracker, /raw\.length > 4096/);
    assert.match(tracker, /value\.marker_version !== 1/);
});

test('the exact discovered target is visible and rapid clicks are blocked', () => {
    assert.match(card, /actionInFlight\.current \|\| systemUpdate\.active/);
    assert.match(card, /disabled=\{starting\}/);
    assert.match(card, /t\('panelUpdate\.start', \{ version: target\.version \}\)/);
    assert.match(card, /\{t\('panelUpdate\.targetVersion'\)\}.*\{target\.version\}/s);
    assert.match(card, /\{t\('panelUpdate\.sequence'\)\}.*\{target\.sequence\}/s);
    assert.doesNotMatch(card, /confirmation|panel-update-confirmation|panelUpdate\.confirm/);
    assert.equal(card.match(/\bstartUpdate\(\)/g)?.length, 2, 'startUpdate may only be declared and invoked by the final button');
    assert.match(card, /const START_REQUEST_TIMEOUT_MS = 15000/);
    assert.match(card, /signal: controller\.signal/);
});

test('card carries alpha disclosure and safe response handling while tracking stays visible', () => {
    for (const phrase of ['Alpha sürüm', 'yedek', 'yeniden başlatır', 'hiçbir zaman otomatik başlamaz']) {
        assert.ok(tr.includes(phrase), `missing disclosure: ${phrase}`);
    }
    for (const status of [401, 403, 408, 409, 429, 500]) {
        assert.ok(updateSources.includes(`status === ${status}`) || updateSources.includes('status >= 500'), `missing HTTP ${status} handling`);
    }
    assert.match(tracker, /role="status"/);
    assert.match(tracker, /aria-live="polite"/);
    assert.match(tracker, />T\+<\/dt>/);
    assert.match(tracker, />ID<\/dt>/);
});

test('coded unavailable, busy and target-changed responses remain distinct', () => {
    assert.match(card, /readApiError\(response\)/);
    assert.match(card, /apiErrorText\(apiError, t\)/);
    assert.match(card, /rejection\.code === 'PANEL_UPDATE_UNAVAILABLE'/);
    assert.ok(en.includes("'err.PANEL_UPDATE_UNAVAILABLE'"));
    assert.ok(en.includes("'err.PANEL_UPDATE_BUSY'"));
    assert.ok(en.includes("'err.PANEL_UPDATE_TARGET_CHANGED'"));
    assert.ok(tr.includes("'err.PANEL_UPDATE_UNAVAILABLE'"));
    assert.ok(tr.includes("'err.PANEL_UPDATE_BUSY'"));
    assert.ok(tr.includes("'err.PANEL_UPDATE_TARGET_CHANGED'"));
});

test('update copy is localized and update components contain no visible Turkish literals', () => {
    const keyPattern = /'panelUpdate\.([A-Za-z0-9]+)'\s*:/g;
    const trKeys = new Set([...tr.matchAll(keyPattern)].map((match) => match[1]));
    const enKeys = new Set([...en.matchAll(keyPattern)].map((match) => match[1]));
    assert.deepEqual([...trKeys].sort(), [...enKeys].sort());
    assert.ok(trKeys.size >= 40, 'complete update tracker copy catalog missing');
    assert.doesNotMatch(updateSources, /[ÇçĞğİıÖöŞşÜü]/, 'visible Turkish copy must live in the catalog');
    assert.match(card, /useI18n\(\)/);
    assert.match(tracker, /useI18n\(\)/);
});

test('browser never sends an update URL, path or command', () => {
    const postBody = card.slice(card.indexOf('body: JSON.stringify({'), card.indexOf('}),', card.indexOf('body: JSON.stringify({')));
    assert.doesNotMatch(postBody, /\b(url|path|command|args|environment)\b/i);
    for (const field of ['version', 'commit', 'sequence', 'os', 'arch', 'archive_sha256', 'archive_size']) {
        assert.ok(updateSources.includes(field), `missing exact target field ${field}`);
    }
});
