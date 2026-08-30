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

const admission = await importTypeScript('../src/lib/panelUpdateAdmission.ts');

test('readiness decoder accepts only the exact fail-closed wire contract', () => {
    assert.deepEqual(admission.decodeHostMutationReadiness({ ready: true }), { ready: true });
    assert.deepEqual(admission.decodeHostMutationReadiness({
        ready: false,
        code: 'HOST_MUTATION_BUSY',
        reason: 'host_lock_busy',
    }), {
        ready: false,
        code: 'HOST_MUTATION_BUSY',
        reason: 'host_lock_busy',
    });
    assert.equal(admission.decodeHostMutationReadiness({ ready: true, reason: 'host_lock_busy' }), null);
    assert.equal(admission.decodeHostMutationReadiness({
        ready: false,
        code: 'HOST_MUTATION_BUSY',
        reason: 'unexpected',
    }), null);
});

test('busy readiness never invokes the marker/start callback', async () => {
    let starts = 0;
    const result = await admission.runHostMutationAdmission(
        async () => ({
            ready: false,
            code: 'HOST_MUTATION_BUSY',
            reason: 'agent_mutation_active',
        }),
        async () => {
            starts += 1;
            return 'started';
        },
    );
    assert.equal(starts, 0);
    assert.equal(result.ready, false);
});

test('a readiness timeout fails closed without invoking the marker/start callback', async () => {
    let abortRequest;
    let clearedHandle = null;
    let starts = 0;
    const runtime = {
        // Deliberately ignore AbortSignal. The complete preflight must still
        // settle at the hard deadline instead of trusting fetch cooperation.
        fetch: () => new Promise(() => undefined),
        setTimeout: (callback, delay) => {
            assert.equal(delay, admission.HOST_MUTATION_READINESS_TIMEOUT_MS);
            abortRequest = callback;
            return 73;
        },
        clearTimeout: (handle) => {
            clearedHandle = handle;
        },
    };

    const operation = admission.runHostMutationAdmission(
        () => admission.fetchHostMutationReadiness(runtime),
        async () => {
            starts += 1;
            return 'started';
        },
    );
    await Promise.resolve();
    assert.equal(typeof abortRequest, 'function');
    abortRequest();
    const result = await operation;

    assert.equal(starts, 0);
    assert.deepEqual(result, {
        ready: false,
        code: 'HOST_MUTATION_UNAVAILABLE',
        reason: 'state_unverified',
    });
    assert.equal(clearedHandle, 73);
});

test('caller cancellation fails closed and clears the admission timer', async () => {
    let clearedHandle = null;
    const controller = new AbortController();
    const runtime = {
        fetch: () => new Promise(() => undefined),
        setTimeout: (_callback, delay) => {
            assert.equal(delay, admission.HOST_MUTATION_READINESS_TIMEOUT_MS);
            return 91;
        },
        clearTimeout: (handle) => {
            clearedHandle = handle;
        },
    };

    const operation = admission.fetchHostMutationReadiness(runtime, controller.signal);
    controller.abort();
    assert.deepEqual(await operation, {
        ready: false,
        code: 'HOST_MUTATION_UNAVAILABLE',
        reason: 'state_unverified',
    });
    assert.equal(clearedHandle, 91);
});

test('only a verified ready response invokes the admitted callback once', async () => {
    let starts = 0;
    const result = await admission.runHostMutationAdmission(
        async () => ({ ready: true }),
        async () => {
            starts += 1;
            return 'started';
        },
    );
    assert.equal(starts, 1);
    assert.deepEqual(result, { ready: true });
});
