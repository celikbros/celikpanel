import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import TestRenderer, { act } from 'react-test-renderer';
import ts from 'typescript';

const require = createRequire(import.meta.url);

function moduleURL(source) {
    return 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');
}

function compileURL(relativePath, replacements = []) {
    const source = readFileSync(new URL(relativePath, import.meta.url), 'utf8');
    let compiled = ts.transpileModule(source, {
        compilerOptions: {
            module: ts.ModuleKind.ES2022,
            target: ts.ScriptTarget.ES2020,
            jsx: ts.JsxEmit.ReactJSX,
        },
    }).outputText;
    for (const [pattern, replacement] of replacements) compiled = compiled.replace(pattern, replacement);
    return moduleURL(compiled);
}

const reactURL = pathToFileURL(require.resolve('react')).href;
const jsxRuntimeURL = pathToFileURL(require.resolve('react/jsx-runtime')).href;
const iconsURL = moduleURL(`
    import React from '${reactURL}';
    const Icon = (props) => React.createElement('i', props);
    export const AlertTriangle = Icon;
    export const DownloadCloud = Icon;
    export const Loader2 = Icon;
    export const RefreshCw = Icon;
`);
const uiURL = moduleURL(`
    import React from '${reactURL}';
    export function Button({ children, ...props }) {
        return React.createElement('button', props, children);
    }
`);
const i18nURL = moduleURL(`
    export function useI18n() {
        return {
            t: (key, params) => params?.version && Object.keys(params).length === 1
                ? key + ':' + params.version
                : params ? key + ' ' + JSON.stringify(params) : key,
            locale: 'en',
        };
    }
`);
const apiErrorURL = moduleURL(`
    export async function readApiError() { return {}; }
    export function apiErrorText() { return 'api-error'; }
`);
const operationURL = moduleURL(`
    export function createSystemUpdateRequestID() { return 'mounted-request-id'; }
    export function systemUpdateResponseHint() { return 'response-error'; }
    export function useSystemUpdateOperation() { return globalThis.__panelUpdateOperation; }
    export function validUpdateTarget() { return true; }
    export function validUpdateVersion() { return true; }
`);
const admissionURL = moduleURL(`
    export function unverifiedHostMutationReadiness() {
        return { ready: false, code: 'HOST_MUTATION_UNAVAILABLE', reason: 'state_unverified' };
    }
    export function fetchHostMutationReadiness(_runtime, signal) {
        return globalThis.__nextPanelReadiness(signal);
    }
    export async function runHostMutationAdmission(readReadiness, onReady) {
        const readiness = await readReadiness();
        if (readiness.ready) await onReady();
        return readiness;
    }
`);
const recoveryObservationURL = compileURL('../src/lib/recoveryObservation.ts');
const systemUpdateFailureURL = compileURL('../src/lib/systemUpdateFailure.ts');
const outcomeURL = compileURL('../src/lib/systemUpdateOutcome.ts', [
    [/from ['"]\.\/recoveryObservation['"]/g, `from '${recoveryObservationURL}'`],
    [/from ['"]\.\/systemUpdateFailure['"]/g, `from '${systemUpdateFailureURL}'`],
]);
const panelURL = compileURL('../src/components/PanelUpdateCard.tsx', [
    [/from ['"]\.\.\/lib\/systemUpdateOutcome['"]/g, `from '${outcomeURL}'`],
    [/from ['"]react['"]/g, `from '${reactURL}'`],
    [/from ['"]react\/jsx-runtime['"]/g, `from '${jsxRuntimeURL}'`],
    [/from ['"]lucide-react['"]/g, `from '${iconsURL}'`],
    [/from ['"]\.\/ui['"]/g, `from '${uiURL}'`],
    [/from ['"]\.\.\/i18n['"]/g, `from '${i18nURL}'`],
    [/from ['"]\.\.\/lib\/apiError['"]/g, `from '${apiErrorURL}'`],
    [/from ['"]\.\.\/lib\/panelUpdateAdmission['"]/g, `from '${admissionURL}'`],
    [/from ['"]\.\/SystemUpdateOperation['"]/g, `from '${operationURL}'`],
]);
const { PANEL_UPDATE_CHECK_TIMEOUT_MS, PanelUpdateCard, fetchPanelUpdateCheck } = await import(panelURL);

const updateCheck = {
    supported: true,
    available: true,
    current_version: 'v0.1.0-alpha.51',
    current_commit: 'a'.repeat(40),
    target: {
        version: 'v0.1.0-alpha.52',
        commit: 'b'.repeat(40),
        sequence: '52',
        os: 'linux',
        arch: 'amd64',
        archive_sha256: 'c'.repeat(64),
        archive_size: '22500000',
    },
};

async function flushMicrotasks() {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
}

async function mountCheckedCard(nextReadiness, start, check = updateCheck) {
    globalThis.__nextPanelReadiness = nextReadiness;
    globalThis.__panelUpdateOperation = { active: false, start };
    globalThis.fetch = async (input) => {
        const path = String(input);
        if (path === '/api/v1/panel/version') {
            return { ok: true, json: async () => ({ version: updateCheck.current_version, commit: updateCheck.current_commit }) };
        }
        if (path === '/api/v1/panel/update/check') {
            return { ok: true, json: async () => check };
        }
        throw new Error('unexpected fetch: ' + path);
    };
    let renderer;
    await act(async () => {
        renderer = TestRenderer.create(React.createElement(PanelUpdateCard));
        await flushMicrotasks();
    });
    const checkButton = renderer.root.findAllByType('button')[0];
    await act(async () => {
        checkButton.props.onClick();
        await flushMicrotasks();
    });
    return renderer;
}

test('the complete update-check fetch and decode have one hard eight-second deadline', async () => {
    let fireDeadline;
    let cleared = false;
    const pending = fetchPanelUpdateCheck((key) => key, {
        fetch: () => new Promise(() => undefined),
        setTimeout: (callback, delay) => {
            assert.equal(delay, PANEL_UPDATE_CHECK_TIMEOUT_MS);
            fireDeadline = callback;
            return 41;
        },
        clearTimeout: (timer) => {
            assert.equal(timer, 41);
            cleared = true;
        },
    });
    assert.equal(typeof fireDeadline, 'function');
    fireDeadline();
    await assert.rejects(pending, /panelUpdate\.checkFailed/);
    assert.equal(cleared, true);
});

test('unmount aborts an unresolved update check and suppresses its stale continuation', async () => {
    const originalFetch = globalThis.fetch;
    let resolveCheck;
    let readinessCalls = 0;
    let starts = 0;
    let renderer;
    try {
        globalThis.__nextPanelReadiness = async () => {
            readinessCalls += 1;
            return { ready: true };
        };
        globalThis.__panelUpdateOperation = {
            active: false,
            start: async () => { starts += 1; return { kind: 'accepted' }; },
        };
        globalThis.fetch = async (input) => {
            const path = String(input);
            if (path === '/api/v1/panel/version') {
                return { ok: true, json: async () => ({ version: updateCheck.current_version, commit: updateCheck.current_commit }) };
            }
            if (path === '/api/v1/panel/update/check') {
                return new Promise((resolve) => { resolveCheck = resolve; });
            }
            throw new Error('unexpected fetch: ' + path);
        };
        await act(async () => {
            renderer = TestRenderer.create(React.createElement(PanelUpdateCard));
            await flushMicrotasks();
        });
        const checkButton = renderer.root.findAllByType('button')[0];
        act(() => checkButton.props.onClick());
        await act(flushMicrotasks);
        assert.equal(typeof resolveCheck, 'function');
        act(() => renderer.unmount());
        renderer = null;
        await act(async () => {
            resolveCheck({ ok: true, json: async () => updateCheck });
            await flushMicrotasks();
        });
        assert.equal(readinessCalls, 0);
        assert.equal(starts, 0);
    } finally {
        if (renderer) act(() => renderer.unmount());
        globalThis.fetch = originalFetch;
        delete globalThis.__nextPanelReadiness;
        delete globalThis.__panelUpdateOperation;
    }
});

test('unmount before a late ready result never creates a marker or starts the update', async () => {
    const originalFetch = globalThis.fetch;
    let readinessCalls = 0;
    let resolveLate;
    let starts = 0;
    let renderer;
    try {
        renderer = await mountCheckedCard(
            () => {
                readinessCalls += 1;
                if (readinessCalls === 1) return Promise.resolve({ ready: true });
                return new Promise((resolve) => { resolveLate = resolve; });
            },
            async () => {
                starts += 1;
                return { kind: 'accepted' };
            },
        );
        const startButton = renderer.root.findByProps({ id: 'panel-update-start-button' });
        act(() => startButton.props.onClick());
        await act(flushMicrotasks);
        assert.equal(readinessCalls, 2);
        assert.equal(typeof resolveLate, 'function');

        act(() => renderer.unmount());
        renderer = null;
        await act(async () => {
            resolveLate({ ready: true });
            await flushMicrotasks();
        });
        assert.equal(starts, 0);
    } finally {
        if (renderer) act(() => renderer.unmount());
        globalThis.fetch = originalFetch;
        delete globalThis.__nextPanelReadiness;
        delete globalThis.__panelUpdateOperation;
    }
});

test('a fresh mounted busy preflight remains actionable and never starts the update', async () => {
    const originalFetch = globalThis.fetch;
    let readinessCalls = 0;
    let starts = 0;
    let renderer;
    try {
        renderer = await mountCheckedCard(
            async () => {
                readinessCalls += 1;
                return readinessCalls === 1
                    ? { ready: true }
                    : { ready: false, code: 'HOST_MUTATION_BUSY', reason: 'host_lock_busy' };
            },
            async () => {
                starts += 1;
                return { kind: 'accepted' };
            },
        );
        const startButton = renderer.root.findByProps({ id: 'panel-update-start-button' });
        await act(async () => {
            startButton.props.onClick();
            await flushMicrotasks();
        });
        assert.equal(readinessCalls, 2);
        assert.equal(starts, 0);
        assert.equal(renderer.root.findByProps({ id: 'panel-update-start-button' }).props.disabled, true);
        assert.ok(renderer.root.findAllByType('button').length >= 3, 'busy readiness exposes the retry action');
    } finally {
        if (renderer) act(() => renderer.unmount());
        globalThis.fetch = originalFetch;
        delete globalThis.__nextPanelReadiness;
        delete globalThis.__panelUpdateOperation;
    }
});

function textOf(node) {
    if (typeof node === 'string') return node;
    return (node.children ?? []).map(textOf).join('');
}

test('a previously failed target is named before Start, and Start stays with the owner', async () => {
    const originalFetch = globalThis.fetch;
    let starts = 0;
    let renderer;
    const attempt = { request_id: 'e'.repeat(32), phase: 'recovered', failure_code: 'candidate_panel_startup_check_failed', finished_at: '2026-09-30T15:53:24Z' };
    try {
        renderer = await mountCheckedCard(
            async () => ({ ready: true }),
            async () => { starts += 1; return { kind: 'accepted' }; },
            { ...updateCheck, previous_attempt: attempt },
        );
        const notes = renderer.root.findAll((node) => node.type === 'div' && node.props.role === 'note');
        // 12 Oct 2026: a version that was tried here and rolled back says so in
        // its heading, names what the server runs now, the recorded cause, and
        // what starting it again does.
        const notice = notes.find((node) => textOf(node).includes('panelUpdate.previousAttempt.rolledBackTitle'));
        assert.ok(notice, 'previous-attempt notice is shown');
        const text = textOf(notice);
        assert.doesNotMatch(text, /panelUpdate\.previousAttempt\.title/);
        assert.match(text, /panelUpdate\.previousAttempt\.recovered/);
        assert.match(text, /v0\.1\.0-alpha\.52/);
        assert.match(text, /"current":"v0\.1\.0-alpha\.51"/);
        assert.match(text, /panelUpdate\.previousAttempt\.cause .*recovery\.reason\.candidate_panel_startup_check_failed/);
        assert.doesNotMatch(text, /previousAttempt\.noCause/);
        assert.match(text, /panelUpdate\.previousAttempt\.again:v0\.1\.0-alpha\.52/);
        const order = ['rolledBackTitle', 'recovered', 'cause', 'again'].map((part) => text.indexOf(`panelUpdate.previousAttempt.${part}`));
        assert.deepEqual(order, [...order].sort((a, b) => a - b), 'what happened, the cause, then what starting again does');
        const all = renderer.root.findAll(() => true);
        const noticeIndex = all.indexOf(notice);
        const startIndex = all.indexOf(renderer.root.findByProps({ id: 'panel-update-start-button' }));
        assert.ok(noticeIndex >= 0 && noticeIndex < startIndex, 'the notice precedes the Start button');
        const startButton = renderer.root.findByProps({ id: 'panel-update-start-button' });
        assert.equal(startButton.props.disabled, false);
        await act(async () => { startButton.props.onClick(); await flushMicrotasks(); });
        assert.equal(starts, 1);
    } finally {
        if (renderer) act(() => renderer.unmount());
        globalThis.fetch = originalFetch;
        delete globalThis.__nextPanelReadiness;
        delete globalThis.__panelUpdateOperation;
    }
    // A return for which the server recorded no typed cause (the measured
    // case: the candidate's migration failed) says that, instead of nothing.
    try {
        renderer = await mountCheckedCard(async () => ({ ready: true }), async () => ({ kind: 'accepted' }),
            { ...updateCheck, previous_attempt: { request_id: 'e'.repeat(32), phase: 'recovered', finished_at: '2026-10-09T07:52:52Z' } });
        const text = textOf(renderer.root);
        assert.match(text, /panelUpdate\.previousAttempt\.rolledBackTitle/);
        assert.match(text, /panelUpdate\.previousAttempt\.noCause/);
        assert.match(text, /panelUpdate\.previousAttempt\.again/);
        assert.doesNotMatch(text, /previousAttempt\.cause/);
        assert.equal(renderer.root.findByProps({ id: 'panel-update-start-button' }).props.disabled, false);
    } finally {
        if (renderer) act(() => renderer.unmount());
        renderer = null;
        globalThis.fetch = originalFetch;
        delete globalThis.__nextPanelReadiness;
        delete globalThis.__panelUpdateOperation;
    }
    for (const [previous, expected] of [
        [undefined, null],
        [{ request_id: 'e'.repeat(32), phase: 'failed', failure_code: 'private_detail', finished_at: '2026-09-30T15:53:24Z' }, 'failed'],
        [{ request_id: 'e'.repeat(32), phase: 'recovering', finished_at: '2026-09-30T15:53:24Z' }, null],
        [{ request_id: '../x', phase: 'recovered', finished_at: '2026-09-30T15:53:24Z' }, null],
    ]) {
        try {
            renderer = await mountCheckedCard(async () => ({ ready: true }), async () => ({ kind: 'accepted' }),
                previous ? { ...updateCheck, previous_attempt: previous } : updateCheck);
            const text = textOf(renderer.root);
            if (expected === null) {
                assert.doesNotMatch(text, /previousAttempt/);
            } else {
                assert.match(text, /panelUpdate\.previousAttempt\.failed/);
                assert.match(text, /"current":"v0\.1\.0-alpha\.51"/);
                assert.doesNotMatch(text, /previousAttempt\.cause|private_detail/);
                // No rollback was recorded for it, so none is claimed.
                assert.match(text, /panelUpdate\.previousAttempt\.title/);
                assert.match(text, /panelUpdate\.previousAttempt\.noCause/);
                assert.doesNotMatch(text, /previousAttempt\.rolledBackTitle|previousAttempt\.again/);
            }
            assert.equal(renderer.root.findByProps({ id: 'panel-update-start-button' }).props.disabled, false);
        } finally {
            if (renderer) act(() => renderer.unmount());
            renderer = null;
            globalThis.fetch = originalFetch;
            delete globalThis.__nextPanelReadiness;
            delete globalThis.__panelUpdateOperation;
        }
    }
});

// upd3 F1: an earlier attempt that stopped in its preflight changed nothing;
// the notice says so, repeats no cause line and Start stays available.
// upd4 F4: a refused update check reads the same.
for (const code of ['recovery_runtime_preflight_failed', 'update_preflight_refused'])
test(`an earlier preflight stop (${code}) is named as unchanged before Start`, async () => {
    const originalFetch = globalThis.fetch;
    let renderer;
    const attempt = { request_id: 'e'.repeat(32), phase: 'failed', failure_code: code, finished_at: '2026-09-30T17:54:30Z' };
    try {
        renderer = await mountCheckedCard(async () => ({ ready: true }), async () => ({ kind: 'accepted' }),
            { ...updateCheck, previous_attempt: attempt });
        const notes = renderer.root.findAll((node) => node.type === 'div' && node.props.role === 'note');
        const notice = notes.find((node) => textOf(node).includes('panelUpdate.previousAttempt.stoppedTitle'));
        assert.ok(notice, 'preflight notice is shown');
        const text = textOf(notice);
        assert.match(text, /panelUpdate\.previousAttempt\.stopped/);
        assert.match(text, /"current":"v0\.1\.0-alpha\.51"/);
        assert.doesNotMatch(text, /previousAttempt\.title|previousAttempt\.failed|previousAttempt\.recovered|previousAttempt\.cause/);
        assert.equal(renderer.root.findByProps({ id: 'panel-update-start-button' }).props.disabled, false);
    } finally {
        if (renderer) act(() => renderer.unmount());
        globalThis.fetch = originalFetch;
        delete globalThis.__nextPanelReadiness;
        delete globalThis.__panelUpdateOperation;
    }
});
