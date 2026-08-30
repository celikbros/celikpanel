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
        return { t: (key, params) => params?.version ? key + ':' + params.version : key };
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
const panelURL = compileURL('../src/components/PanelUpdateCard.tsx', [
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

async function mountCheckedCard(nextReadiness, start) {
    globalThis.__nextPanelReadiness = nextReadiness;
    globalThis.__panelUpdateOperation = { active: false, start };
    globalThis.fetch = async (input) => {
        const path = String(input);
        if (path === '/api/v1/panel/version') {
            return { ok: true, json: async () => ({ version: updateCheck.current_version, commit: updateCheck.current_commit }) };
        }
        if (path === '/api/v1/panel/update/check') {
            return { ok: true, json: async () => updateCheck };
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
