import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import TestRenderer, { act } from 'react-test-renderer';
import ts from 'typescript';

const require = createRequire(import.meta.url);

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
    return 'data:text/javascript;base64,' + Buffer.from(compiled).toString('base64');
}

const watchdogURL = compileURL('../src/lib/systemUpdateWatchdog.ts');
const reactURL = pathToFileURL(require.resolve('react')).href;
const jsxRuntimeURL = pathToFileURL(require.resolve('react/jsx-runtime')).href;
const routerCoreURL = compileURL('../src/router-core.ts');
const routerHistoryURL = compileURL('../src/router-history.ts');
const routerURL = compileURL('../src/router.tsx', [
    [/from ['"]react['"]/, `from '${reactURL}'`],
    [/from ['"]react\/jsx-runtime['"]/, `from '${jsxRuntimeURL}'`],
    [/from ['"]\.\/router-core['"]/g, `from '${routerCoreURL}'`],
    [/from ['"]\.\/router-history['"]/g, `from '${routerHistoryURL}'`],
]);
const hookURL = compileURL('../src/lib/useSystemUpdateNavigationLease.ts', [
    [/from ['"]react['"]/, `from '${reactURL}'`],
    [/from ['"]\.\/systemUpdateWatchdog['"]/, `from '${watchdogURL}'`],
]);
const watchdog = await import(watchdogURL);
const { useSystemUpdateNavigationLease } = await import(hookURL);
const { isNavigationBlocked, useNavigationBlocker } = await import(routerURL);

class FakeLeaseRuntime {
    constructor(now) {
        this.time = now;
        this.monotonicTime = 0;
        this.nextTimer = 1;
        this.tasks = new Map();
        this.values = new Map();
        this.listeners = new Set();
        this.wakeListeners = new Set();
        this.storage = {
            getItem: (key) => this.values.get(key) ?? null,
            setItem: (key, value) => {
                this.values.set(key, value);
                if (key === watchdog.SYSTEM_UPDATE_NAVIGATION_RELEASE_KEY) {
                    for (const listener of [...this.listeners]) listener(value);
                }
            },
        };
        this.options = {
            now: () => this.time,
            monotonicNow: () => this.monotonicTime,
            schedule: (callback, delay) => {
                const id = this.nextTimer++;
                this.tasks.set(id, { at: this.time + delay, callback });
                return id;
            },
            cancel: (id) => this.tasks.delete(id),
            storage: this.storage,
            subscribe: (listener) => {
                this.listeners.add(listener);
                return () => this.listeners.delete(listener);
            },
            subscribeWake: (listener) => {
                this.wakeListeners.add(listener);
                return () => this.wakeListeners.delete(listener);
            },
        };
    }

    advance(milliseconds) {
        this.time += milliseconds;
        this.monotonicTime += milliseconds;
        while (true) {
            const due = [...this.tasks.entries()]
                .filter(([, task]) => task.at <= this.time)
                .sort((left, right) => left[1].at - right[1].at)[0];
            if (!due) return;
            this.tasks.delete(due[0]);
            due[1].callback();
        }
    }

    emit(identity) {
        for (const listener of [...this.listeners]) listener(identity);
    }

    elapseWhileThrottled(milliseconds, wallMilliseconds = milliseconds) {
        this.time += wallMilliseconds;
        this.monotonicTime += milliseconds;
    }

    wake() {
        for (const listener of [...this.wakeListeners]) listener();
    }
}

function Probe({ identity, createdAt, runtime, phase = 'active', requested = true }) {
    const lease = useSystemUpdateNavigationLease(identity, createdAt, requested, runtime.options);
    return React.createElement('output', {
        'data-blocked': lease.blocked,
        'data-released': lease.released,
        'data-phase': phase,
    });
}

function blocked(renderer) {
    return renderer.root.findByType('output').props['data-blocked'];
}

function GuardProbe({ identity, createdAt, runtime, requested, page }) {
    const lease = useSystemUpdateNavigationLease(identity, createdAt, requested, runtime.options);
    const navigationBlocked = watchdog.systemUpdateGlobalNavigationBlocked(requested, lease.blocked);
    const blocker = React.useRef(navigationBlocked);
    blocker.current = navigationBlocked;
    useNavigationBlocker(blocker);
    React.useLayoutEffect(() => navigationBlocked
        ? watchdog.acquireSystemUpdatePageLock(
            page.application,
            () => false,
            page.window,
            page.document,
        )
        : undefined, [navigationBlocked, page]);
    return React.createElement('output', { 'data-blocked': navigationBlocked });
}

function fakePage() {
    const attributes = new Map();
    const beforeUnload = new Set();
    return {
        attributes,
        beforeUnload,
        application: {
            inert: false,
            setAttribute: (key, value) => attributes.set(key, value),
            removeAttribute: (key) => attributes.delete(key),
        },
        document: { body: { style: { overflow: 'scroll' } } },
        window: {
            addEventListener: (type, listener) => {
                if (type === 'beforeunload') beforeUnload.add(listener);
            },
            removeEventListener: (type, listener) => {
                if (type === 'beforeunload') beforeUnload.delete(listener);
            },
        },
        dispatchBeforeUnload() {
            const event = {
                prevented: false,
                returnValue: undefined,
                preventDefault() { this.prevented = true; },
            };
            for (const listener of [...beforeUnload]) listener(event);
            return event;
        },
    };
}

test('mounted lease releases at exactly 120 seconds and never re-locks after clock rollback', () => {
    const runtime = new FakeLeaseRuntime(1_000_000);
    let renderer;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'operation-a', createdAt: 1_000_000, runtime,
        }));
    });
    assert.equal(blocked(renderer), true);
    act(() => runtime.advance(119_999));
    assert.equal(blocked(renderer), true);
    act(() => runtime.advance(1));
    assert.equal(blocked(renderer), false);

    runtime.time = 900_000;
    act(() => renderer.update(React.createElement(Probe, {
        identity: 'operation-a', createdAt: 1_000_000, runtime, phase: 'terminal',
    })));
    assert.equal(blocked(renderer), false);
    assert.equal(runtime.tasks.size, 0);
    act(() => renderer.unmount());
});

test('terminal, reload, and remount reuse the released exact-operation lease', () => {
    const runtime = new FakeLeaseRuntime(2_000_000);
    let renderer;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'operation-b', createdAt: 1_880_000, runtime, phase: 'terminal',
        }));
    });
    assert.equal(blocked(renderer), false);
    act(() => renderer.update(React.createElement(Probe, {
        identity: 'operation-b', createdAt: 1_880_000, runtime, phase: 'reload',
    })));
    assert.equal(blocked(renderer), false);
    act(() => renderer.unmount());

    runtime.time = 1_900_000;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'operation-b', createdAt: 1_880_000, runtime, phase: 'active',
        }));
    });
    assert.equal(blocked(renderer), false);
    act(() => renderer.unmount());
});

test('future clock releases fail-open across remount while a new exact operation gets a new lease', () => {
    const runtime = new FakeLeaseRuntime(3_000_000);
    let renderer;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'future-operation', createdAt: 3_100_000, runtime,
        }));
    });
    assert.equal(blocked(renderer), false);
    act(() => renderer.unmount());
    runtime.time = 3_100_001;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'future-operation', createdAt: 3_100_000, runtime,
        }));
    });
    assert.equal(blocked(renderer), false);
    act(() => renderer.update(React.createElement(Probe, {
        identity: 'new-operation', createdAt: 3_100_001, runtime,
    })));
    assert.equal(blocked(renderer), true);
    assert.equal(runtime.tasks.size, 1);
    act(() => renderer.unmount());
});

test('sibling-tab release and unmount clean up listeners and fake timers', () => {
    const runtime = new FakeLeaseRuntime(4_000_000);
    let renderer;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'operation-c', createdAt: 4_000_000, runtime,
        }));
    });
    assert.equal(runtime.tasks.size, 1);
    assert.equal(runtime.listeners.size, 1);
    assert.equal(runtime.wakeListeners.size, 1);
    act(() => runtime.emit('operation-c'));
    assert.equal(blocked(renderer), false);
    assert.equal(runtime.tasks.size, 0);
    assert.equal(runtime.listeners.size, 0);
    assert.equal(runtime.wakeListeners.size, 0);
    act(() => renderer.unmount());
    assert.equal(runtime.tasks.size, 0);
    assert.equal(runtime.listeners.size, 0);
    assert.equal(runtime.wakeListeners.size, 0);
});

test('background timer throttling cannot extend the lease after focus, visibility, or pageshow wake', () => {
    const runtime = new FakeLeaseRuntime(5_000_000);
    let renderer;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'operation-hidden', createdAt: 5_000_000, runtime,
        }));
    });
    assert.equal(blocked(renderer), true);
    runtime.elapseWhileThrottled(120_000);
    assert.equal(blocked(renderer), true, 'the deliberately throttled timer has not fired');
    act(() => runtime.wake());
    assert.equal(blocked(renderer), false, 'returning to the tab rechecks the absolute deadline');
    assert.equal(runtime.tasks.size, 0);
    assert.equal(runtime.wakeListeners.size, 0);
    assert.equal(
        runtime.storage.getItem(watchdog.SYSTEM_UPDATE_NAVIGATION_RELEASE_KEY),
        'operation-hidden',
    );
    act(() => renderer.unmount());
});

test('monotonic elapsed time releases on wake even after the wall clock rolls backward', () => {
    const runtime = new FakeLeaseRuntime(6_000_000);
    let renderer;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'operation-clock-skew', createdAt: 6_000_000, runtime,
        }));
    });
    runtime.elapseWhileThrottled(120_000, -60_000);
    act(() => runtime.wake());
    assert.equal(blocked(renderer), false);
    act(() => renderer.unmount());
});

test('an engaged lease released by auth or terminal suspension never re-locks', () => {
    const runtime = new FakeLeaseRuntime(7_000_000);
    let renderer;
    act(() => {
        renderer = TestRenderer.create(React.createElement(Probe, {
            identity: 'operation-auth', createdAt: 7_000_000, runtime, requested: true,
        }));
    });
    assert.equal(blocked(renderer), true);
    act(() => renderer.update(React.createElement(Probe, {
        identity: 'operation-auth', createdAt: 7_000_000, runtime,
        requested: false, phase: 'auth-paused',
    })));
    assert.equal(blocked(renderer), false);
    assert.equal(
        runtime.storage.getItem(watchdog.SYSTEM_UPDATE_NAVIGATION_RELEASE_KEY),
        'operation-auth',
    );
    act(() => renderer.update(React.createElement(Probe, {
        identity: 'operation-auth', createdAt: 7_000_000, runtime,
        requested: true, phase: 'active',
    })));
    assert.equal(blocked(renderer), false);
    act(() => renderer.unmount());
});

test('mounted lease removes inert, beforeunload, body lock, and router blocker at 120 seconds', () => {
    const runtime = new FakeLeaseRuntime(8_000_000);
    const page = fakePage();
    let renderer;
    act(() => {
        renderer = TestRenderer.create(React.createElement(GuardProbe, {
            identity: 'operation-page-lock', createdAt: 8_000_000,
            runtime, requested: true, page,
        }));
    });
    assert.equal(isNavigationBlocked(), true);
    assert.equal(page.application.inert, true);
    assert.equal(page.attributes.get('aria-busy'), 'true');
    assert.equal(page.document.body.style.overflow, 'hidden');
    assert.equal(page.beforeUnload.size, 1);
    assert.equal(page.dispatchBeforeUnload().prevented, true);

    act(() => runtime.advance(120_000));
    assert.equal(isNavigationBlocked(), false);
    assert.equal(page.application.inert, false);
    assert.equal(page.attributes.has('aria-busy'), false);
    assert.equal(page.document.body.style.overflow, 'scroll');
    assert.equal(page.beforeUnload.size, 0);
    assert.equal(page.dispatchBeforeUnload().prevented, false);

    act(() => renderer.update(React.createElement(GuardProbe, {
        identity: 'operation-page-lock', createdAt: 8_000_000,
        runtime, requested: true, page,
    })));
    assert.equal(isNavigationBlocked(), false, 'the same exact identity never restores the router blocker');
    assert.equal(page.beforeUnload.size, 0);
    act(() => renderer.unmount());
    assert.equal(isNavigationBlocked(), false);
});

test('navigation lease and mutation authority are independent executable policies', () => {
    assert.equal(watchdog.systemUpdateGlobalNavigationBlocked(true, true), true);
    assert.equal(watchdog.systemUpdateGlobalNavigationBlocked(true, false), false);
    assert.equal(watchdog.systemUpdateGlobalNavigationBlocked(false, true), false);
    assert.equal(watchdog.systemUpdateMutationLocked(false, false), true);
    assert.equal(watchdog.systemUpdateMutationLocked(true, true), true);
    assert.equal(watchdog.systemUpdateMutationLocked(true, false), false);
});

test('localStorage SecurityError is navigation-safe and never installs a broken storage listener', () => {
    const previousWindow = globalThis.window;
    let storageListenerCalls = 0;
    globalThis.window = {
        get localStorage() { throw new DOMException('denied', 'SecurityError'); },
        addEventListener() { storageListenerCalls += 1; },
        removeEventListener() {},
    };
    const runtime = new FakeLeaseRuntime(9_000_000);
    const options = {
        now: runtime.options.now,
        monotonicNow: runtime.options.monotonicNow,
        schedule: runtime.options.schedule,
        cancel: runtime.options.cancel,
        subscribeWake: runtime.options.subscribeWake,
    };
    let renderer;
    try {
        act(() => {
            renderer = TestRenderer.create(React.createElement(Probe, {
                identity: 'operation-storage-denied', createdAt: 9_000_000,
                runtime: { options },
            }));
        });
        assert.equal(blocked(renderer), true);
        assert.equal(storageListenerCalls, 0);
        act(() => runtime.advance(120_000));
        assert.equal(blocked(renderer), false);
    } finally {
        if (renderer) act(() => renderer.unmount());
        if (previousWindow === undefined) delete globalThis.window;
        else globalThis.window = previousWindow;
    }
});
