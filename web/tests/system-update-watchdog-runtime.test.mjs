import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

async function importTypeScript(relativePath) {
    const source = readFileSync(new URL(relativePath, import.meta.url), 'utf8');
    const compiled = ts.transpileModule(source, {
        compilerOptions: {
            module: ts.ModuleKind.ES2022,
            target: ts.ScriptTarget.ES2020,
        },
    }).outputText;
    return import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64'));
}

const watchdog = await importTypeScript('../src/lib/systemUpdateWatchdog.ts');
const lease = await importTypeScript('../src/lib/systemUpdateLease.ts');
const component = readFileSync(
    new URL('../src/components/SystemUpdateOperation.tsx', import.meta.url),
    'utf8',
);
const en = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('global navigation lock has an exact two-minute deadline', () => {
    const started = 1_000_000;
    const deadline = watchdog.SYSTEM_UPDATE_BLOCKING_DEADLINE_MS;
    assert.equal(deadline, 120_000);
    assert.equal(watchdog.systemUpdateNavigationBlocked(started, started + deadline - 1), true);
    assert.equal(watchdog.systemUpdateNavigationBlocked(started, started + deadline), false);
});

test('reload derives the same bounded state from persisted marker time', () => {
    const persistedCreatedAt = 1_000_000;
    assert.equal(watchdog.systemUpdateNavigationBlocked(persistedCreatedAt, 1_120_000), false);
    assert.equal(watchdog.systemUpdateNavigationBlocked(persistedCreatedAt, 9_000_000), false);
});

test('canonical uncertainty cannot renew an expired global navigation lease', () => {
    assert.equal(watchdog.systemUpdateGlobalNavigationBlocked(true, true), true);
    assert.equal(watchdog.systemUpdateGlobalNavigationBlocked(true, false), false);
    assert.equal(watchdog.systemUpdateGlobalNavigationBlocked(false, true), false);
    assert.equal(watchdog.systemUpdateMutationLocked(false, false), true);
    assert.equal(watchdog.systemUpdateMutationLocked(true, false), false);
});

test('navigation release keeps exact GET tracking alive while mutation authority remains closed', () => {
    assert.equal(watchdog.systemUpdateExactTrackingAllowed(true, false, true), true);
    assert.equal(watchdog.systemUpdateExactTrackingAllowed(true, false, false), false);
    assert.equal(watchdog.systemUpdateExactTrackingAllowed(false, true, true), false);
    assert.equal(
        watchdog.systemUpdateExactStatusPath('exact id/with?reserved'),
        '/api/v1/panel/update/status?request_id=exact%20id%2Fwith%3Freserved',
    );
    assert.equal(watchdog.systemUpdateMutationLocked(false, true), true);
});

test('a fresh document absence observation cannot renew the expired navigation lease', () => {
    const createdAt = 1_000_000;
    const deadline = watchdog.SYSTEM_UPDATE_BLOCKING_DEADLINE_MS;
    const observation = lease.systemUpdateNotFoundAction(null, 'exact-a', 0, deadline);
    assert.equal(observation.action, 'wait', 'a remount starts a fresh monotonic absence grace');
    assert.equal(watchdog.systemUpdateNavigationBlocked(createdAt, createdAt + deadline), false,
        'the independent page-navigation lease still opens at two minutes');
    assert.equal(watchdog.systemUpdateMutationLocked(true, true), true,
        'the exact canonical mutation gate remains closed during the fresh observation');
});

test('invalid or future clocks cannot create an unbounded page lock', () => {
    assert.equal(watchdog.systemUpdateNavigationBlocked(Number.NaN, 2_000_000), false);
    assert.equal(watchdog.systemUpdateNavigationBlocked(0, 2_000_000), false);
    assert.equal(watchdog.systemUpdateNavigationBlocked(3_000_000, 2_000_000), false);
});

test('deadline releases navigation but preserves exact operation occupancy and polling', () => {
    assert.match(component, /useSystemUpdateNavigationLease\([\s\S]{0,160}operationBlocks/);
    assert.match(component, /systemUpdateGlobalNavigationBlocked\([\s\n]*operationBlocks,[\s\n]*navigationLease\.blocked/);
    assert.match(component, /systemUpdateMutationLocked\([\s\n]*canonicalReady,[\s\S]{0,180}marker !== null/);
    assert.match(component, /systemUpdateExactTrackingAllowed\(marker !== null, canonicalReady, backgroundTracking\)/);
    assert.match(component, /canonical === undefined && !backgroundTracking[\s\S]*pollExact\(exactMarker, t\)/);
    assert.match(component, /backgroundVisible && exactMarker/);
    assert.match(component, /useLayoutEffect\(\(\) => \{\s*if \(!blocking\) return undefined;/);
    assert.match(component, /useEffect\(\(\) => \{\s*if \(!blocking && marker === null\) return undefined;[\s\S]*setInterval/);
});

test('background authentication resume wakes exact GET tracking without reacquiring a modal guard', () => {
    const resumeBody = component.slice(
        component.indexOf('const resumeTrackingGuard'),
        component.indexOf('const cancelPostUpdateReload'),
    );
    assert.match(resumeBody, /navigationLeaseIdentityRef\.current === exactMarkerFingerprint\(exactMarker\)/);
    assert.match(resumeBody, /navigationLeaseReleasedRef\.current/);
    assert.match(resumeBody, /setAuthPaused\(false\)/);
    assert.match(resumeBody, /return Promise\.resolve\(true\)/);
    assert.doesNotMatch(resumeBody, /method:\s*'POST'/);
    assert.match(component, /resumeTrackingGuard\(exactMarker\)\.then\(async \(guarded\)[\s\S]{0,220}pollWakeRef\.current\?\.\(\)/);
});

test('restart disconnect retries GET only and exact terminal states remain authoritative', () => {
    assert.match(component, /fetch\(systemUpdateExactStatusPath\(marker\.request_id\)/);
    const pollBody = component.slice(
        component.indexOf('async function pollExact'),
        component.indexOf('async function abandonExact'),
    );
    assert.doesNotMatch(pollBody, /method:\s*'POST'/);
    assert.match(component, /kind: 'retry',[\s\S]{0,180}disconnected: true/);
    assert.match(component, /payload\.status === 'succeeded'/);
    assert.match(component, /payload\.status === 'failed'/);
    assert.match(component, /outcome\.kind === 'succeeded'/);
    assert.match(component, /outcome\.kind === 'failed'/);
    assert.match(component, /commitTerminal\([\s\n]*exactMarker,[\s\n]*'succeeded'/);
    assert.match(component, /commitTerminal\([\s\n]*exactMarker,[\s\n]*'failed'/);
    assert.match(component, /systemUpdateCanonicalReloadAuthorized/);
    assert.equal((component.match(/\/api\/v1\/panel\/update\/start/g) ?? []).length, 1);
    assert.equal((component.match(/\/api\/v1\/panel\/update\/abandon/g) ?? []).length, 1);
    assert.equal((component.match(/method:\s*'POST'/g) ?? []).length, 2);
});

test('background watchdog visibly reports target, elapsed time, and exact request id', () => {
    assert.match(component, /panelUpdate\.title/);
    assert.match(component, /panelUpdate\.watch/);
    assert.match(component, /exactMarker\.target\.version/);
    assert.match(component, /formatElapsed\(exactMarker\.created_at, now\)/);
    assert.match(component, /exactMarker\.request_id/);
    assert.match(component, /role=\{terminalKind === 'failed' \? 'alert' : 'status'\}/);
    assert.match(en, /'panelUpdate\.accepted': 'The request was accepted/);
    assert.match(en, /'panelUpdate\.watch': 'Navigate;/);
    assert.match(tr, /'panelUpdate\.accepted': 'İstek kabul edildi/);
    assert.match(tr, /'panelUpdate\.watch': 'Gezinin;/);
});
