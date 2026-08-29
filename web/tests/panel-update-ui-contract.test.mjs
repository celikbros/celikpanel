import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const card = readFileSync(new URL('../src/components/PanelUpdateCard.tsx', import.meta.url), 'utf8');
const tracker = readFileSync(new URL('../src/components/SystemUpdateOperation.tsx', import.meta.url), 'utf8');
const lease = readFileSync(new URL('../src/lib/systemUpdateLease.ts', import.meta.url), 'utf8');
const watchdog = readFileSync(new URL('../src/lib/systemUpdateWatchdog.ts', import.meta.url), 'utf8');
const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8');
const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const settings = readFileSync(new URL('../src/components/Settings.tsx', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');
const en = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const updateSources = card + tracker;

test('update card is reachable only through the admin settings panel', () => {
    assert.match(settings, /role === 'admin'/);
    const adminBlock = settings.slice(settings.indexOf("{role === 'admin'"));
    assert.match(adminBlock, /\{activeID === 'updates' && <PanelUpdateCard \/>\}/);
    assert.ok(adminBlock.indexOf('<PanelUpdateCard />') < adminBlock.indexOf('id="settings-dns-panel"'));
});

test('the provider persists and revalidates exact ownership before the only update POST', () => {
    const cardStart = card.slice(card.indexOf('async function startUpdate()'));
    assert.match(cardStart, /await systemUpdate\.start\(exactMarker\)/);
    assert.doesNotMatch(card, /fetch\('\/api\/v1\/panel\/update\/start'/);
    assert.equal(updateSources.match(/fetch\('\/api\/v1\/panel\/update\/start'/g)?.length, 1, 'POST must not be auto-retried');

    const dispatch = tracker.slice(
        tracker.indexOf('const dispatchOwnedStart'),
        tracker.indexOf('const start = useCallback', tracker.indexOf('const dispatchOwnedStart')),
    );
    const post = tracker.slice(
        tracker.indexOf('const postAuthorizedStart'),
        tracker.indexOf('const recoverNotFound'),
    );
    const fenced = dispatch.indexOf('transactStoredRecord');
    const exact = dispatch.indexOf(`current?.phase !== 'active'`);
    const committed = dispatch.indexOf(`authorized.kind !== 'committed' || !authorized.result`);
    const posted = dispatch.indexOf('postAuthorizedStart(');
    assert.ok(fenced >= 0 && exact > fenced && committed > exact && posted > committed,
        'POST dispatch must occur only after the durable exact-owner authorization committed');
    assert.doesNotMatch(dispatch.slice(fenced, committed), /fetch\(/,
        'a transaction abort or mirror failure must occur before any network side effect');
    assert.match(post, /request_id: exactMarker\.request_id/);
    assert.match(post, /\.\.\.exactMarker\.target/);
    assert.match(dispatch, /transactStoredRecord<boolean>[\s\S]*dispatch_owner !== ownerID/);
    assert.match(dispatch, /dispatch_state !== 'claimed'/);
    assert.match(dispatch, /dispatch_state: 'authorized'[\s\S]*dispatch_attempted_at: attemptedAt/);
    assert.match(post, /commitTerminal\(exactMarker, 'failed', failure, dispatchFence\)/);
    assert.match(lease, /database\.transaction\(options\.storeName, 'readwrite'\)/);
    assert.match(lease, /const get = store\.get\(options\.recordKey\)/);
    assert.match(lease, /store\.put\(/);
});

test('root polling survives restart, focus and route changes while accepting only the exact identity', () => {
    assert.match(tracker, /decodeUpdateStatus\(await response\.json\(\)\)/);
    assert.match(tracker, /fetch\(systemUpdateExactStatusPath\(marker\.request_id\)/);
    assert.match(tracker, /payload\.request_id !== marker\.request_id/);
    assert.match(tracker, /!sameUpdateTarget\(payload\.target, marker\.target\)/);
    assert.match(tracker, /Math\.min\(POLL_MAX_MS, Math\.round\(delay \* 1\.6\)\)/);
    assert.match(tracker, /window\.addEventListener\('focus', reconcileOnWake\)/);
    assert.match(tracker, /window\.addEventListener\('online', reconcileOnWake\)/);
    assert.match(tracker, /window\.addEventListener\('pageshow', reconcileOnWake\)/);
    assert.match(tracker, /document\.addEventListener\('visibilitychange', onVisibility\)/);
    assert.match(tracker, /payload\.status === 'succeeded'/);
    assert.match(tracker, /payload\.status === 'failed'/);
    assert.equal(tracker.match(/fetch\(systemUpdateExactStatusPath\(marker\.request_id\)/g)?.length, 1);
    assert.equal(watchdog.match(/\/api\/v1\/panel\/update\/status\?request_id=/g)?.length, 1);
});

test('self-update owns its exact root tracker without joining the service-operation discovery channel', () => {
    assert.match(tracker, /const SYSTEM_UPDATE_MARKER_KEY = 'celikpanel\.system-update-operation\.v1'/);
    assert.match(tracker, /fetch\(systemUpdateExactStatusPath\(marker\.request_id\)/);
    assert.doesNotMatch(updateSources, /useComponentOperation|service\/operation\?active=1/);
});

test('the tracker is mounted above routes and remains a single modal interaction owner', () => {
    const root = app.slice(app.indexOf('function App()'));
    const provider = root.indexOf('<SystemUpdateOperationProvider>');
    const router = root.indexOf('<BrowserRouter>');
    const authGate = root.indexOf('<AuthGate />');
    assert.ok(provider >= 0 && router > provider && authGate > router, 'root tracker and continuously mounted router must wrap AuthGate');
    const authenticated = app.slice(app.indexOf('function AuthGate()'), app.indexOf('function App()'));
    const componentOperations = authenticated.indexOf('<ComponentOperationProvider>');
    const routes = authenticated.indexOf('<AppRoutes />', componentOperations);
    assert.ok(componentOperations >= 0 && routes > componentOperations);
    assert.ok(!authenticated.includes('<SystemUpdateOperationProvider>'));
    assert.ok(!authenticated.includes('<BrowserRouter>'));
    assert.equal(app.match(/<SystemUpdateOperationProvider>/g)?.length, 1);
    assert.match(tracker, /terminalBlocks/);
    assert.match(tracker, /pendingReload !== null/);
    assert.match(tracker, /role="dialog"/);
    assert.match(tracker, /aria-modal="true"/);
    assert.match(tracker, /className="fixed inset-0 z-\[110\]/);
    assert.match(tracker, /acquireSystemUpdatePageLock/);
    assert.match(watchdog, /application\.inert = true/);
    assert.match(watchdog, /targetDocument\.body\.style\.overflow = 'hidden'/);
    assert.match(tracker, /document\.addEventListener\('focusin', keepFocusInDialog\)/);
    assert.match(tracker, /marker \|\| pendingReload \|\| requiredReloadMarker/);
});

test('a saved marker resumes after reload and another tab can adopt it without replacing an active identity', () => {
    assert.match(tracker, /useState<StoredUpdateRecord \| null>\(\(\) => readStoredRecord\(\)\)/);
    assert.match(tracker, /window\.addEventListener\('storage', onStorage\)/);
    assert.match(tracker, /void reconcileCanonicalRecord\(\)/);
    assert.match(tracker, /legacyStorage: localStorage/);
    assert.match(tracker, /mirrorStorage: localStorage/);
    assert.match(lease, /if \(get\.result === undefined\)/);
    assert.match(lease, /options\.codec\.decode\(raw\)/);
    assert.match(lease, /record: legacyRecord/);
    assert.match(tracker, /message: t\('panelUpdate\.running'\)/);
});

test('an exact successful update reloads once with a cache-busting identity', () => {
    const success = tracker.slice(tracker.indexOf("if (outcome.kind === 'succeeded')"), tracker.indexOf("if (outcome.kind === 'failed')"));
    assert.match(success, /commitTerminal\(/);
    assert.match(success, /'succeeded'/);
    assert.match(success, /panelUpdate\.reloading/);
    assert.match(tracker, /const POST_UPDATE_RELOAD_MS = 1500/);
    assert.match(tracker, /reloadTimerRef\.current !== null/);
    assert.match(tracker, /searchParams\.get\(POST_UPDATE_RELOAD_PARAM\) === exactMarker\.request_id/);
    assert.match(tracker, /searchParams\.set\(POST_UPDATE_RELOAD_PARAM, exactMarker\.request_id\)/);
    assert.match(tracker, /searchParams\.delete\(POST_UPDATE_RELOAD_PARAM\)/);
    assert.match(tracker, /replaceCurrentRouterURL\(/);
    assert.doesNotMatch(tracker, /window\.history\.replaceState\(/);
    assert.match(tracker, /window\.location\.replace\(next\.toString\(\)\)/);
    assert.doesNotMatch(updateSources, /window\.location\.reload\(/);
    assert.match(tracker, /const TAB_RELOADED_MARKER_KEY/);
    assert.match(tracker, /tabHasReloadedMarker\(exactMarker\)/);
    assert.match(tracker, /pendingReloadRef\.current = exactMarker/);
    assert.match(tracker, /const exactMarker = pendingReload \?\? requiredReloadMarker \?\? provisional/);
    assert.match(tracker, /requiredReloadMarker \? 'succeeded'/);
    assert.match(tracker, /reload_scheduled: false/);
    assert.match(tracker, /systemUpdateCanonicalReloadAuthorized\(\s*canonicalReadyRef\.current,[\s\S]*requiredReloadFromRecord\(canonicalRecordRef\.current\)/);
    assert.match(tracker, /const canonicalRequired = requiredReloadFromRecord\(snapshot\.record\)/);
    assert.match(tracker, /cancelPostUpdateReload\(\)/);
    assert.match(tracker, /if \(!canonicalReady \|\| provisional \|\| pendingReload\) return/);
    assert.match(tracker, /if \(!canonicalReady \|\| !requiredReload\) return/);
    const consumeStart = tracker.indexOf('const consumeOrSchedule');
    const consume = tracker.slice(consumeStart, tracker.indexOf('useEffect(() => subscribeSystemUpdateAuthentication', consumeStart));
    assert.match(consume, /transactStoredRecord<TerminalUpdateRecord \| null>/);
    assert.match(consume, /setReloadRetryNonce/);
    assert.match(tracker, /if \(!canonicalReady \|\| terminal\?\.kind !== 'succeeded'\) return/);
    assert.match(tracker, /canonical\.reload_scheduled\) return/);
    assert.match(consume, /rememberTabReloadedMarker\(committed\.marker\)/);
});

test('terminal failure remains explicit until acknowledgement and never reloads', () => {
    const failure = tracker.slice(tracker.indexOf("if (outcome.kind === 'failed')"), tracker.indexOf('setView({', tracker.indexOf("if (outcome.kind === 'failed')")));
    assert.match(failure, /commitTerminal\(exactMarker, 'failed', outcome\.message\)/);
    assert.doesNotMatch(failure, /schedulePostUpdateReload/);
    assert.match(tracker, /terminal\.kind === 'failed'/);
    assert.match(tracker, /onClick=\{\(\) => void dismissTerminal\(\)\}/);
    assert.match(tracker, /t\('dnssrv\.continue'\)/);
    assert.match(tracker, /outcome\.message/);
});

test('build identity reads bypass browser caches after a completed update', () => {
    assert.match(layout, /fetch\('\/api\/v1\/panel\/version', \{ cache: 'no-store', credentials: 'same-origin' \}\)/);
});

test('definitive refusal, durable absence and identity mismatch cannot wedge the browser', () => {
    assert.match(tracker, /const NOT_FOUND_GRACE_MS = 120000/);
    assert.match(tracker, /response\.status === 400 \|\| response\.status === 401[\s\S]*response\.status === 409[\s\S]*response\.status === 429/);
    assert.match(tracker, /apiError\.code === 'PANEL_UPDATE_UNAVAILABLE'/);
    assert.match(tracker, /systemUpdateNotFoundAction\(/);
    assert.match(tracker, /current\.dispatch_owner !== ownerID/);
    assert.match(tracker, /const fenced = await transactStoredRecord<boolean>/);
    assert.doesNotMatch(tracker, /kind: 'replay'/);
    const fenceCheck = tracker.indexOf('const fenced = await transactStoredRecord<boolean>');
    const authFence = tracker.indexOf('systemUpdateDispatchAllowed(', fenceCheck);
    const updatePost = tracker.indexOf("fetch('/api/v1/panel/update/start'", fenceCheck);
    assert.ok(fenceCheck >= 0 && authFence > fenceCheck && updatePost > authFence);
    assert.match(tracker.slice(authFence, updatePost), /authSignalGenerationRef\.current/);
    assert.match(tracker.slice(authFence, updatePost), /authenticatedRef\.current/);
    assert.match(tracker.slice(authFence, updatePost), /guardReadyRef\.current/);
    const startHandler = tracker.indexOf('const start = useCallback');
    const approvalCapture = tracker.indexOf('const approvalGeneration = authSignalGenerationRef.current', startHandler);
    const provisionalGuard = tracker.indexOf('establishProvisionalGuard(exactMarker)', startHandler);
    assert.ok(startHandler >= 0 && approvalCapture > startHandler && provisionalGuard > approvalCapture);
    assert.match(tracker.slice(approvalCapture, provisionalGuard), /authenticatedRef\.current/);
    assert.match(tracker.slice(approvalCapture, provisionalGuard), /authPausedRef\.current/);
    assert.ok(tracker.includes('dispatchOwnedStart(exactMarker, approvalGeneration)'));
    assert.ok(tracker.includes("t('panelUpdate.notAccepted')"));
    assert.match(tracker, /await commitTerminal\(exactMarker, 'failed', failure, dispatchFence\)/);
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
    assert.match(card, /type="checkbox"/);
    assert.match(card, /checked=\{restartAcknowledged\}/);
    assert.match(card, /!target \|\| !restartAcknowledged/);
    assert.match(card, /disabled=\{starting \|\| !restartAcknowledged\}/);
    assert.match(card, /panelUpdate\.restartAcknowledgement/);
    assert.match(card, /createSystemUpdateRequestID\(\)/);
    assert.match(card, /t\('panelUpdate\.start', \{ version: target\.version \}\)/);
    assert.match(card, /\{t\('panelUpdate\.targetVersion'\)\}.*\{target\.version\}/s);
    assert.match(card, /\{t\('panelUpdate\.sequence'\)\}.*\{target\.sequence\}/s);
    assert.doesNotMatch(card, /type="text"|panel-update-confirmation|panelUpdate\.confirm/);
    assert.equal(card.match(/\bstartUpdate\(\)/g)?.length, 2, 'startUpdate may only be declared and invoked by the final button');
    assert.match(card, /id="panel-update-start-button"/);
    assert.match(tracker, /const START_REQUEST_TIMEOUT_MS = 15000/);
    assert.match(tracker, /signal: controller\.signal/);
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
    assert.match(updateSources, /readApiError\(response\)/);
    assert.match(updateSources, /apiErrorText\(apiError, t\)/);
    assert.match(tracker, /apiError\.code === 'PANEL_UPDATE_UNAVAILABLE'/);
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
    const postBody = tracker.slice(tracker.indexOf('body: JSON.stringify({'), tracker.indexOf('}),', tracker.indexOf('body: JSON.stringify({')));
    assert.doesNotMatch(postBody, /\b(url|path|command|args|environment)\b/i);
    for (const field of ['version', 'commit', 'sequence', 'os', 'arch', 'archive_sha256', 'archive_size']) {
        assert.ok(updateSources.includes(field), `missing exact target field ${field}`);
    }
});

test('multi-tab ownership and browser lifecycle remain fail closed', () => {
    assert.ok(tracker.includes('const [tabOwnerID] = useState(() => createSystemUpdateRequestID())'));
    assert.doesNotMatch(tracker, /sessionStorage[\s\S]{0,160}dispatch.owner|TAB_DISPATCH_OWNER_KEY|getOrCreateTabDispatchOwnerID/,
        'dispatch authority must be unique to one live document, never cloneable storage');
    assert.match(tracker, /SYSTEM_UPDATE_DISPATCH_LOCK/);
    assert.match(tracker, /runWithSystemUpdateLock/);
    assert.match(tracker, /if \(dispatchState === 'authorized' && !allowAuthorized\)/);
    assert.match(tracker, /const authorized = current\?\.phase === 'active'[\s\S]*current\.dispatch_state === 'authorized'/);
    assert.doesNotMatch(tracker, /foreignAuthorized/);
    assert.match(tracker, /const verified = await pollExact\(exactMarker, t\)/);
    assert.match(tracker, /verified\.kind !== 'not-found'/);
    assert.match(tracker, /transactCanonicalRecord/);
    assert.match(lease, /readwrite transaction owns the canonical record/);
    assert.match(lease, /transaction\.onabort/);
    assert.match(lease, /repairCanonicalMirror/);
    assert.match(lease, /mirrorCanonicalSnapshot\([\s\S]*committedSnapshot/);
    assert.doesNotMatch(lease, /electionSettleMS|commitSettleMS|expiresAt/);
    assert.doesNotMatch(tracker, /claimFallbackSystemUpdateLease/);
    assert.match(tracker, /acquireSystemUpdatePageLock/);
    assert.match(watchdog, /targetWindow\.addEventListener\('beforeunload', onBeforeUnload\)/);
    assert.ok(tracker.includes('useNavigationBlocker(navigationBlockedRef)'));
    assert.ok(tracker.includes('focusTarget?.isConnected'));
    assert.ok(tracker.includes('focusTarget !== document.body'));
    assert.ok(tracker.includes('celikpanel-main-content'));
    assert.ok(tracker.includes("document.getElementById('panel-update-start-button')"));
});

test('the provisional guard commits DOM inertness and beforeunload before any asynchronous ownership wait', () => {
    const start = tracker.slice(tracker.indexOf('const start = useCallback'), tracker.indexOf('const dismissTerminal'));
    assert.ok(start.indexOf('const guardCommit = establishProvisionalGuard(exactMarker)')
        < start.indexOf('claimCanonicalRecord(exactMarker)'));
    assert.match(start, /markerMatches\(current, exactMarker\)[\s\S]*\? \{ kind: 'adopted' \}/);

    const provisional = tracker.slice(tracker.indexOf('const establishProvisionalGuard'), tracker.indexOf('const resumeTrackingGuard'));
    assert.match(provisional, /guardReadyRef\.current/);
    assert.match(provisional, /return pending\.promise/);
    assert.match(provisional, /flushSync\(\(\) =>/);
    assert.match(provisional, /setProvisional\(exactMarker\)/);

    const layoutEffect = tracker.slice(tracker.indexOf('useLayoutEffect(() =>'), tracker.indexOf('}, [blocking, settlePendingCommit]'));
    assert.ok(layoutEffect.indexOf('acquireSystemUpdatePageLock(')
        < layoutEffect.indexOf('settlePendingCommit(pendingGuard.marker, true)'));
    assert.match(layoutEffect, /acquireSystemUpdatePageLock/);
    assert.match(watchdog, /application\.inert = true/);
    assert.match(watchdog, /targetWindow\.addEventListener\('beforeunload', onBeforeUnload\)/);

    const adopt = tracker.slice(tracker.indexOf('const adoptStoredRecord'), tracker.indexOf('const schedulePostUpdateReload'));
    assert.match(adopt, /flushSync\(\(\) =>/);
    assert.match(adopt, /setProvisional\(null\)/);
    assert.match(adopt, /setMarker\(record\.marker\)/);
});

test('authentication loss pauses only the global guard and resumes exact tracking after auth', () => {
    const poll = tracker.slice(tracker.indexOf('async function pollExact'), tracker.indexOf('function terminalResultFromRecord'));
    assert.match(poll, /response\.status === 401[\s\S]*kind: 'auth'/);
    assert.match(poll, /response\.status === 403[\s\S]*kind: 'retry'/);
    assert.match(tracker, /const operationBlocks = pendingReload !== null/);
    assert.match(tracker, /const blocking = systemUpdateGlobalNavigationBlocked\([\s\n]*operationBlocks,[\s\n]*navigationLease\.blocked/);
    assert.match(tracker, /\|\| \(!authPaused &&/);
    assert.match(tracker, /authPausedRef\.current = true/);
    assert.match(tracker, /setAuthPaused\(true\)/);
    assert.match(tracker, /resumeTrackingGuard\(exactMarker\)/);
    assert.match(tracker, /subscribeSystemUpdateAuthentication/);
    assert.match(tracker, /const \[authPaused, setAuthPaused\] = useState\(true\)/);
    assert.match(tracker, /const authenticatedRef = useRef\(false\)/);
    assert.match(tracker, /const paused = !authenticatedRef\.current/);
    assert.match(tracker, /await reconcileCanonicalRecord\(\);[\s\S]*pollWakeRef\.current\?\.\(\)/);
    assert.match(app, /useLayoutEffect\(\(\) => \{[\s\S]*publishSystemUpdateAuthentication\(!loading && user !== null\)/);
    const authBranch = tracker.slice(tracker.indexOf(`if (outcome.kind === 'auth')`), tracker.indexOf(`if (authPausedRef.current`));
    assert.doesNotMatch(authBranch, /schedule\(/);
    assert.match(tracker, /if \(cancelled \|\| authPausedRef\.current\) return/);
    assert.match(tracker, /requestAuthGeneration !== authSignalGenerationRef\.current/);
    assert.match(app, /const authGenerationRef = useRef\(0\)/);
    assert.match(app, /const requestGeneration = authGenerationRef\.current/);
    assert.match(app, /shouldApplyUnauthorizedResponse\(requestGeneration, authGenerationRef\.current\)/);
    assert.match(app, /<Login onSuccess=\{transitionAuthentication\}/);
});

test('terminal propagation and acknowledgement use the same exact serialized storage record', () => {
    assert.match(tracker, /phase: 'terminal'/);
    const commit = tracker.slice(tracker.indexOf('const commitTerminal'), tracker.indexOf('const acknowledgeTerminal'));
    assert.match(commit, /transactStoredRecord<TerminalUpdateRecord \| null>/);
    assert.match(commit, /current\?\.phase === 'active' && markerMatches\(current\.marker, exactMarker\)/);
    assert.match(commit, /kind: 'write', record, result: record/);
    const acknowledge = tracker.slice(tracker.indexOf('const acknowledgeTerminal'), tracker.indexOf('useEffect(() =>', tracker.indexOf('const acknowledgeTerminal')));
    assert.match(acknowledge, /transactStoredRecord<boolean>/);
    assert.match(acknowledge, /record\.outcome !== 'failed'/);
    assert.match(acknowledge, /recordMatches\(current, record\)/);
    assert.match(acknowledge, /record: record\.required_reload \? reloadRecord\(record\.required_reload\) : null/);
    const claim = tracker.slice(tracker.indexOf('const claimCanonicalRecord'), tracker.indexOf('const dispatchOwnedStart'));
    assert.match(claim, /completedTerminal/);
    assert.match(claim, /completedReload/);
    assert.match(claim, /tabHasReloadedMarker\(current\.marker\)/);
    assert.match(claim, /kind: 'write',[\s\S]*record: requested/);
    assert.match(claim, /activeRecord\([\s\S]*ownerID,[\s\S]*requiredReloadFromRecord\(current\)/);
    const storage = tracker.slice(tracker.indexOf('const onStorage'), tracker.indexOf("window.addEventListener('storage', onStorage)"));
    assert.match(storage, /void reconcileCanonicalRecord\(\)/);
});

test('a successful release remains a per-tab reload barrier across every successor state', () => {
    assert.match(tracker, /type ReloadUpdateRecord/);
    assert.match(tracker, /required_reload\?: UpdateMarker/);
    assert.match(tracker, /function requiredReloadFromRecord/);
    assert.match(tracker, /record\.phase === 'reload' \|\| \(record\.phase === 'terminal' && record\.outcome === 'succeeded'\)/);
    assert.match(tracker, /const reloadRequired = requiredReload !== null && !tabHasReloadedMarker\(requiredReload\)/);
    assert.match(tracker, /const operationBlocks = pendingReload !== null \|\| reloadRequired/);
    assert.match(tracker, /const exactMarker = pendingReload \?\? requiredReloadMarker/);
    assert.match(tracker, /schedulePostUpdateReload\(requiredReload\)/);
    assert.match(tracker, /kind === 'succeeded'[\s\S]*required_reload: exactMarker/);
    assert.match(tracker, /current\.required_reload[\s\S]*required_reload: current\.required_reload/);
    assert.match(tracker, /record\.required_reload \? reloadRecord\(record\.required_reload\) : null/);
    assert.match(tracker, /requiredReloadFromRecord\(current\) \?\? undefined/);
    assert.match(tracker, /terminalKind === 'failed' \? <XCircle/);
    assert.match(tracker, /markerMatches\(provisionalRef\.current, exactMarker\)[\s\S]*&& !markerRef\.current\)/);
});

test('canonical wake convergence is independent of a local marker and exact identity includes provenance', () => {
    const wakeStart = tracker.indexOf('const reconcileOnWake');
    const wake = tracker.slice(wakeStart, tracker.indexOf('}, [reconcileCanonicalRecord])', wakeStart));
    assert.match(wake, /reconcileCanonicalRecord\(\)/);
    assert.match(wake, /pollWakeRef\.current\?\.\(\)/);
    assert.match(tracker, /window\.addEventListener\('pageshow', reconcileOnWake\)/);
    const exact = tracker.slice(tracker.indexOf('function markerMatches'), tracker.indexOf('function recordMatches'));
    for (const field of ['request_id', 'current_version', 'current_commit', 'created_at']) {
        assert.ok(exact.includes(field), 'exact marker identity is missing ' + field);
    }
    assert.match(exact, /sameUpdateTarget\(left\.target, right\.target\)/);
});

test('focus restoration rejects body and html and captures focus before cross-tab adoption', () => {
    const capture = tracker.slice(tracker.indexOf('const captureMeaningfulFocus'), tracker.indexOf('const settlePendingCommit'));
    assert.match(capture, /active === document\.body/);
    assert.match(capture, /active === document\.documentElement/);
    const adopt = tracker.slice(tracker.indexOf('const adoptStoredRecord'), tracker.indexOf('const clearVisibleRecord'));
    assert.match(adopt, /captureMeaningfulFocus\(\)/);
    const pending = tracker.slice(tracker.indexOf('const markCanonicalPending'), tracker.indexOf('const reconcileCanonicalRecord'));
    assert.ok(pending.indexOf('captureMeaningfulFocus()') < pending.indexOf('setCanonicalReady(false)'));
    const cleanup = tracker.slice(tracker.indexOf('const restoreFocus'), tracker.indexOf('}, [blocking, settlePendingCommit]'));
    assert.match(cleanup, /document\.activeElement === candidate/);
    assert.match(cleanup, /startButton[\s\S]*mainContent/);
});

test('modal focus follows exact operation identity changes while its bounded lease stays active', () => {
    assert.match(tracker, /const modalIdentity = exactMarker \? exactMarkerFingerprint\(exactMarker\) : null/);
    assert.match(tracker, /useLayoutEffect\(\(\) => \{[\s\S]*focusSystemUpdateDialog\(dialogRef\.current, applicationRef\.current\)[\s\S]*\}, \[blocking, modalIdentity, settlePendingCommit\]\)/);
});

test('canonical startup and cross-tab changes keep update mutations fail closed without renewing navigation', () => {
    assert.match(tracker, /const \[canonicalReady, setCanonicalReady\] = useState\(false\)/);
    assert.match(tracker, /const occupied = systemUpdateMutationLocked\([\s\n]*canonicalReady/);
    assert.match(tracker, /if \(!canonicalReadyRef\.current\)[\s\S]*panelUpdate\.markerFailed/);
    const storage = tracker.slice(tracker.indexOf('const onStorage'), tracker.indexOf("window.addEventListener('storage', onStorage)"));
    assert.ok(storage.indexOf('markCanonicalPending()') < storage.indexOf('reconcileCanonicalRecord()'));
    const pending = tracker.slice(tracker.indexOf('const markCanonicalPending'), tracker.indexOf('const reconcileCanonicalRecord'));
    assert.ok(pending.indexOf('cancelPostUpdateReload()') < pending.indexOf('setCanonicalReady(false)'));
    assert.match(tracker, /!mirrorMatchesCanonicalRecord\(canonicalRecordRef\.current\)\) markCanonicalPending\(\)/);
    assert.match(tracker, /panelUpdate\.canonicalChecking/);
});
