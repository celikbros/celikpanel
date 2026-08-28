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

const canonical = await importTypeScript('../src/lib/systemUpdateLease.ts');
const authSignal = await importTypeScript('../src/lib/systemUpdateAuthSignal.ts');

class MemoryStorage {
    constructor(records = new Map()) {
        this.records = records;
    }

    get length() {
        return this.records.size;
    }

    getItem(key) {
        return this.records.get(key) ?? null;
    }

    key(index) {
        return [...this.records.keys()][index] ?? null;
    }

    removeItem(key) {
        this.records.delete(key);
    }

    setItem(key, value) {
        this.records.set(key, value);
    }
}

class ExclusiveLockManager {
    constructor() {
        this.tails = new Map();
    }

    request(name, callback) {
        const previous = this.tails.get(name) ?? Promise.resolve();
        let release;
        const tail = new Promise((resolve) => { release = resolve; });
        this.tails.set(name, tail);
        return previous.then(callback).finally(() => {
            release();
            if (this.tails.get(name) === tail) this.tails.delete(name);
        });
    }
}

class FakeTransaction {
    constructor(factory, mode, gate) {
        this.factory = factory;
        this.mode = mode;
        this.gate = gate;
        this.getRequest = null;
        this.getKey = null;
        this.pendingPut = null;
        this.aborted = false;
        this.onabort = null;
        this.oncomplete = null;
        this.onerror = null;
    }

    objectStore() {
        return {
            get: (key) => {
                const request = { result: undefined, error: null, onsuccess: null, onerror: null };
                this.getRequest = request;
                this.getKey = key;
                return request;
            },
            put: (value, key) => {
                if (this.mode !== 'readwrite') throw new Error('readonly transaction');
                this.pendingPut = { key, value };
                return { result: key };
            },
        };
    }

    abort() {
        if (this.aborted) return;
        this.aborted = true;
        queueMicrotask(() => {
            this.onabort?.();
            this.factory.finish(this);
        });
    }

    async start() {
        this.factory.active = this;
        const request = this.getRequest;
        if (!request) {
            this.abort();
            return;
        }
        request.result = this.factory.records.has(this.getKey)
            ? this.factory.records.get(this.getKey)
            : undefined;
        try {
            request.onsuccess?.();
        } catch (error) {
            request.error = error;
            request.onerror?.();
        }
        if (this.aborted) return;
        if (this.gate) {
            this.gate.enter();
            await this.gate.wait;
        }
        if (this.aborted) return;
        if (this.pendingPut) {
            this.factory.records.set(this.pendingPut.key, this.pendingPut.value);
        }
        this.oncomplete?.();
        this.factory.finish(this);
    }
}

class FakeIndexedDB {
    constructor() {
        this.records = new Map();
        this.created = false;
        this.queue = [];
        this.active = null;
        this.nextGate = null;
    }

    holdNextCommit() {
        let release;
        let entered;
        const enteredPromise = new Promise((resolve) => {
            entered = resolve;
        });
        const wait = new Promise((resolve) => {
            release = resolve;
        });
        this.nextGate = { wait, enter: entered };
        return { entered: enteredPromise, release };
    }

    abortActive() {
        this.active?.abort();
    }

    open() {
        const request = {
            result: null,
            error: null,
            onblocked: null,
            onerror: null,
            onupgradeneeded: null,
            onsuccess: null,
        };
        queueMicrotask(() => {
            const database = {
                objectStoreNames: {
                    contains: () => this.created,
                },
                createObjectStore: () => {
                    this.created = true;
                    return {};
                },
                transaction: (_storeName, mode) => {
                    const gate = this.nextGate;
                    this.nextGate = null;
                    const transaction = new FakeTransaction(this, mode, gate);
                    this.queue.push(transaction);
                    queueMicrotask(() => this.pump());
                    return transaction;
                },
                close: () => {},
            };
            request.result = database;
            if (!this.created) request.onupgradeneeded?.();
            request.onsuccess?.();
        });
        return request;
    }

    pump() {
        if (this.active || this.queue.length === 0) return;
        void this.queue[0].start();
    }

    finish(transaction) {
        if (this.active !== transaction) return;
        this.active = null;
        if (this.queue[0] === transaction) this.queue.shift();
        else this.queue = this.queue.filter((item) => item !== transaction);
        queueMicrotask(() => this.pump());
    }
}

const markerKey = 'celikpanel.system-update-operation.v1';
const recordKey = 'system-update-record';
const databaseName = 'canonical-test';
const storeName = 'records';
const ownerA = '1'.repeat(32);
const ownerB = '2'.repeat(32);

const targetA = {
    version: 'v0.1.0-alpha.49',
    commit: 'a'.repeat(40),
    sequence: '49',
    os: 'linux',
    arch: 'amd64',
    archive_sha256: 'a'.repeat(64),
    archive_size: '100',
};
const targetB = {
    ...targetA,
    version: 'v0.1.0-alpha.50',
    commit: 'b'.repeat(40),
    sequence: '50',
    archive_sha256: 'b'.repeat(64),
};
const markerA = {
    marker_version: 1,
    request_id: 'a'.repeat(32),
    current_version: 'v0.1.0-alpha.48',
    current_commit: 'c'.repeat(40),
    target: targetA,
    created_at: 1000,
};
const markerB = {
    marker_version: 1,
    request_id: 'b'.repeat(32),
    current_version: 'v0.1.0-alpha.49',
    current_commit: 'd'.repeat(40),
    target: targetB,
    created_at: 2000,
};
const activeA = { state_version: 1, phase: 'active', marker: markerA };
const activeB = { state_version: 1, phase: 'active', marker: markerB };
const terminalA = {
    state_version: 1,
    phase: 'terminal',
    marker: markerA,
    outcome: 'failed',
    message: 'failed safely',
    reload_scheduled: false,
    completed_at: 3000,
};
const succeededA = {
    ...terminalA,
    outcome: 'succeeded',
    message: 'reloading',
    reload_scheduled: true,
    required_reload: markerA,
};

function sameTarget(left, right) {
    return left.version === right.version
        && left.commit === right.commit
        && left.sequence === right.sequence
        && left.os === right.os
        && left.arch === right.arch
        && left.archive_sha256 === right.archive_sha256
        && left.archive_size === right.archive_size;
}

function sameMarker(left, right) {
    return left?.request_id === right.request_id
        && left.current_version === right.current_version
        && left.current_commit === right.current_commit
        && left.created_at === right.created_at
        && sameTarget(left.target, right.target);
}

function sameRecord(left, right) {
    if (!left || left.phase !== right.phase || !sameMarker(left.marker, right.marker)) return false;
    if (left.phase === 'reload') return true;
    const sameRequired = left.required_reload
        ? Boolean(right.required_reload && sameMarker(left.required_reload, right.required_reload))
        : !right.required_reload;
    if (left.phase === 'active') return left.dispatch_owner === right.dispatch_owner
        && left.dispatch_state === right.dispatch_state
        && left.dispatch_attempted_at === right.dispatch_attempted_at
        && sameRequired;
    return left.outcome === right.outcome
        && left.message === right.message
        && left.reload_scheduled === right.reload_scheduled
        && left.completed_at === right.completed_at
        && sameRequired;
}

const codec = {
    decode(raw) {
        try {
            const value = JSON.parse(raw);
            if ((value?.phase !== 'active' && value?.phase !== 'terminal' && value?.phase !== 'reload')
                || typeof value?.marker?.request_id !== 'string') return null;
            return value;
        } catch {
            return null;
        }
    },
    encode: JSON.stringify,
    matches: sameRecord,
};

function options(indexedDB, storage, ownerID = ownerA, allowLegacyBootstrap = true) {
    return {
        indexedDB,
        databaseName,
        storeName,
        recordKey,
        ownerID,
        codec,
        legacyStorage: storage,
        legacyKey: markerKey,
        mirrorStorage: storage,
        mirrorKey: markerKey,
        allowLegacyBootstrap,
        timeoutMS: 1000,
    };
}

function transact(indexedDB, storage, ownerID, mutate, allowLegacyBootstrap = true) {
    return canonical.transactCanonicalRecord(
        options(indexedDB, storage, ownerID, allowLegacyBootstrap),
        mutate,
    );
}

test('readwrite transactions serialize a suspended writer and prevent stale mirror overwrite', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    const gate = indexedDB.holdNextCommit();
    const first = transact(indexedDB, storage, ownerA, () => ({
        kind: 'write',
        record: activeA,
        result: 'A',
    }));
    await gate.entered;
    assert.deepEqual(codec.decode(storage.getItem(markerKey)), activeA);

    let secondCallbackRan = false;
    const second = transact(indexedDB, storage, ownerB, () => {
        secondCallbackRan = true;
        return { kind: 'write', record: activeB, result: 'B' };
    });
    await new Promise((resolve) => setTimeout(resolve, 5));
    assert.equal(secondCallbackRan, false);

    gate.release();
    assert.equal((await first).kind, 'committed');
    assert.equal((await second).kind, 'committed');
    assert.equal(secondCallbackRan, true);
    assert.deepEqual(codec.decode(storage.getItem(markerKey)), activeB);
});

test('an aborted writer repairs its optimistic mirror from canonical state', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write',
        record: activeB,
        result: true,
    }));

    const gate = indexedDB.holdNextCommit();
    const aborted = transact(indexedDB, storage, ownerA, () => ({
        kind: 'write',
        record: activeA,
        result: true,
    }));
    await gate.entered;
    assert.deepEqual(codec.decode(storage.getItem(markerKey)), activeA);
    indexedDB.abortActive();
    gate.release();
    assert.equal((await aborted).kind, 'failed');
    assert.deepEqual(codec.decode(storage.getItem(markerKey)), activeB);
});

test('valid legacy state migrates once and never overrides an existing canonical record', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    storage.setItem(markerKey, codec.encode(activeA));
    const migrated = await transact(indexedDB, storage, ownerA, (current) => ({
        kind: 'keep',
        result: current,
    }));
    assert.equal(migrated.kind, 'committed');
    assert.deepEqual(migrated.snapshot.record, activeA);

    storage.setItem(markerKey, codec.encode(activeB));
    const authoritative = await transact(indexedDB, storage, ownerB, (current) => ({
        kind: 'keep',
        result: current,
    }));
    assert.deepEqual(authoritative.snapshot.record, activeA);
    assert.deepEqual(codec.decode(storage.getItem(markerKey)), activeA);
});

test('invalid legacy state becomes a durable tombstone and cannot deny a fresh claim', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    storage.setItem(markerKey, '{not-json');
    const reconciled = await transact(indexedDB, storage, ownerA, (current) => ({
        kind: 'keep',
        result: current,
    }));
    assert.equal(reconciled.kind, 'committed');
    assert.equal(reconciled.snapshot.record, null);
    assert.equal(storage.getItem(markerKey), null);

    const claim = await transact(indexedDB, storage, ownerB, (current) => current
        ? { kind: 'keep', result: false }
        : { kind: 'write', record: activeB, result: true }, false);
    assert.equal(claim.kind, 'committed');
    assert.equal(claim.result, true);
    assert.deepEqual(claim.snapshot.record, activeB);
});

test('terminal removal followed by a successor converges stale tabs to the authoritative successor', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write',
        record: activeA,
        result: true,
    }));
    await transact(indexedDB, storage, ownerA, (current) => sameRecord(current, activeA)
        ? { kind: 'write', record: terminalA, result: true }
        : { kind: 'keep', result: false }, false);
    await transact(indexedDB, storage, ownerA, (current) => sameRecord(current, terminalA)
        ? { kind: 'write', record: null, result: true }
        : { kind: 'keep', result: false }, false);
    await transact(indexedDB, storage, ownerB, (current) => current
        ? { kind: 'keep', result: false }
        : { kind: 'write', record: activeB, result: true }, false);

    const staleWake = await transact(indexedDB, storage, ownerA, (current) => ({
        kind: 'keep',
        result: current,
    }), false);
    assert.deepEqual(staleWake.snapshot.record, activeB);
    assert.deepEqual(codec.decode(storage.getItem(markerKey)), activeB);

    let stalePostDispatched = 0;
    await transact(indexedDB, storage, ownerA, (current) => ({
        kind: 'keep',
        result: sameRecord(current, activeA) ? ++stalePostDispatched : 0,
    }), false);
    assert.equal(stalePostDispatched, 0);
});

test('exact ownership includes provenance even when request id and target match', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write',
        record: activeA,
        result: true,
    }));
    const forged = {
        ...activeA,
        marker: {
            ...markerA,
            current_commit: 'e'.repeat(40),
            created_at: markerA.created_at + 1,
        },
    };
    let dispatched = 0;
    await transact(indexedDB, storage, ownerB, (current) => ({
        kind: 'keep',
        result: sameRecord(current, forged) ? ++dispatched : 0,
    }), false);
    assert.equal(dispatched, 0);
});

test('dispatch begins only after the exact unique owner fence commits', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    const claimed = {
        ...activeA,
        dispatch_owner: ownerA,
        dispatch_state: 'claimed',
    };
    const attemptedAt = 5000;
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write', record: claimed, result: true,
    }));
    const authorize = () => transact(indexedDB, storage, ownerA, (current) => {
        if (current?.phase !== 'active' || !sameMarker(current.marker, markerA)
            || current.dispatch_owner !== ownerA || current.dispatch_state !== 'claimed') {
            return { kind: 'keep', result: false };
        }
        return {
            kind: 'write',
            record: { ...current, dispatch_state: 'authorized', dispatch_attempted_at: attemptedAt },
            result: true,
        };
    }, false);
    let posts = 0;
    const gate = indexedDB.holdNextCommit();
    const aborted = authorize();
    await gate.entered;
    assert.equal(posts, 0, 'the network side effect cannot run inside the transaction callback');
    indexedDB.abortActive();
    gate.release();
    const abortedResult = await aborted;
    if (abortedResult.kind === 'committed' && abortedResult.result) posts += 1;
    assert.equal(posts, 0, 'an aborted canonical authorization sends no POST');

    const committed = await authorize();
    if (committed.kind === 'committed' && committed.result) posts += 1;
    assert.equal(posts, 1, 'the committed unique owner sends exactly one POST');
    const replay = await authorize();
    if (replay.kind === 'committed' && replay.result) posts += 1;
    assert.equal(posts, 1, 'same-marker reentry cannot dispatch again');
});

test('not-found recovery policy never repeats a POST', () => {
    const action = canonical.systemUpdateNotFoundAction;
    assert.equal(action('claimed', 1000, undefined, 1099, 100), 'wait');
    assert.equal(action('claimed', 1000, undefined, 1100, 100), 'fail-dispatch');
    assert.equal(action('authorized', 1000, 1050, 1149, 100), 'wait');
    assert.equal(action('authorized', 1000, 1050, 1150, 100), 'fail-dispatch');
    assert.equal(action('legacy', 1000, undefined, 1100, 100), 'fail-dispatch');
});

test('dispatch authorization is bound to one authenticated guarded generation', () => {
    const allowed = canonical.systemUpdateDispatchAllowed;
    assert.equal(allowed(7, 7, true, true), true);
    assert.equal(allowed(7, 8, true, true), false, 'a new login cannot inherit old approval');
    assert.equal(allowed(7, 7, false, true), false);
    assert.equal(allowed(7, 7, true, false), false, 'POST cannot start without the inert guard');
    assert.equal(allowed(-1, -1, true, true), false);
});

test('auth changes while the prefetch fence is suspended produce no POST', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    const authorized = {
        ...activeA,
        dispatch_owner: ownerA,
        dispatch_state: 'authorized',
        dispatch_attempted_at: 5000,
    };
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write', record: authorized, result: true,
    }));

    const approvalGeneration = 7;
    let currentGeneration = 7;
    let authenticated = true;
    let guardReady = true;
    let posts = 0;
    const gate = indexedDB.holdNextCommit();
    const prefetch = transact(indexedDB, storage, ownerA, (current) => ({
        kind: 'keep',
        result: Boolean(current?.phase === 'active'
            && current.dispatch_owner === ownerA
            && current.dispatch_state === 'authorized'
            && current.dispatch_attempted_at === 5000),
    }), false);
    await gate.entered;
    currentGeneration = 8;
    authenticated = false;
    guardReady = false;
    gate.release();
    const fenced = await prefetch;
    if (fenced.kind === 'committed' && fenced.result
        && canonical.systemUpdateDispatchAllowed(
            approvalGeneration,
            currentGeneration,
            authenticated,
            guardReady,
        )) posts += 1;
    assert.equal(posts, 0, 'logout during the fence cannot dispatch');

    currentGeneration = 9;
    authenticated = true;
    guardReady = true;
    if (fenced.kind === 'committed' && fenced.result
        && canonical.systemUpdateDispatchAllowed(
            approvalGeneration,
            currentGeneration,
            authenticated,
            guardReady,
        )) posts += 1;
    assert.equal(posts, 0, 'a new login cannot inherit the old click');
});

test('a cloned tab cannot steal a frozen authorized dispatch and exactly one POST survives', async () => {
    const indexedDB = new FakeIndexedDB();
    // Duplicated tabs may begin with the same cloned storage contents. Their
    // live document owners are nevertheless distinct and never come from it.
    const clonedStorageRecords = new Map([['copied-session-owner', ownerA]]);
    const storage = new MemoryStorage(clonedStorageRecords);
    const firstAttemptAt = 5000;
    const authorizedA = {
        ...activeA,
        dispatch_owner: ownerA,
        dispatch_state: 'authorized',
        dispatch_attempted_at: firstAttemptAt,
    };
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write', record: authorizedA, result: true,
    }));

    const posts = [];
    const recover = async (owner, now) => {
        const result = await transact(indexedDB, storage, owner, (current) => {
            if (current?.phase !== 'active' || !sameMarker(current.marker, markerA)) {
                return { kind: 'keep', result: { kind: 'stale' } };
            }
            if (current.dispatch_state === 'authorized' && current.dispatch_owner !== owner) {
                return { kind: 'keep', result: { kind: 'wait' } };
            }
            const action = canonical.systemUpdateNotFoundAction(
                current.dispatch_state ?? 'legacy',
                current.marker.created_at,
                current.dispatch_attempted_at,
                now,
                100,
            );
            if (action === 'wait') return { kind: 'keep', result: { kind: 'wait' } };
            return { kind: 'write', record: terminalA, result: { kind: 'terminal' } };
        }, false);
        return result;
    };

    const takeover = await recover(ownerB, firstAttemptAt + 1000);
    assert.equal(takeover.result.kind, 'wait');
    assert.ok(sameRecord(takeover.snapshot.record, authorizedA));
    assert.deepEqual(posts, [], 'another tab never repeats or terminates an authorized POST');

    const successor = await transact(indexedDB, storage, ownerA, (current) => current
        ? { kind: 'keep', result: false }
        : { kind: 'write', record: activeB, result: true }, false);
    assert.equal(successor.result, false, 'a distinct update cannot replace the exact active marker');
    assert.ok(sameMarker(successor.snapshot.record.marker, markerA));

    const fencedPost = async () => {
        const fence = await transact(indexedDB, storage, ownerA, (current) => ({
            kind: 'keep',
            result: Boolean(current?.phase === 'active'
            && sameMarker(current.marker, markerA)
            && current.dispatch_owner === ownerA
            && current.dispatch_state === 'authorized'
            && current.dispatch_attempted_at === firstAttemptAt),
        }), false);
        if (fence.kind === 'committed' && fence.result) posts.push(markerA.request_id);
        return fence.result;
    };
    assert.equal(await fencedPost(), true);
    assert.deepEqual(posts, [markerA.request_id], 'the original owner emits one POST after resuming');

    const terminalized = await recover(ownerA, firstAttemptAt + 2000);
    assert.equal(terminalized.result.kind, 'terminal');
    assert.equal(await fencedPost(), false, 'a terminal canonical record fences any stale continuation');
    assert.equal(posts.length, 1, 'all interleavings emit at most one POST');
});

test('the cross-context lock orders terminal recovery around the final POST fence', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    const manager = new ExclusiveLockManager();
    const lockName = 'dispatch';
    const authorizedA = {
        ...activeA,
        dispatch_owner: ownerA,
        dispatch_state: 'authorized',
        dispatch_attempted_at: 5000,
    };
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write', record: authorizedA, result: true,
    }));

    let enterDispatch;
    let releaseDispatch;
    const dispatchEntered = new Promise((resolve) => { enterDispatch = resolve; });
    const dispatchGate = new Promise((resolve) => { releaseDispatch = resolve; });
    let posts = 0;
    let serverFound = false;
    let recoveryEntered = false;
    let sameOwnerRecoveryEntered = false;
    const dispatch = canonical.runWithSystemUpdateLock(manager, lockName, async () => {
        const fence = await transact(indexedDB, storage, ownerA, (current) => ({
            kind: 'keep', result: sameRecord(current, authorizedA),
        }), false);
        assert.equal(fence.result, true);
        enterDispatch();
        await dispatchGate;
        posts += 1;
        serverFound = true;
        return 'sent';
    });
    await dispatchEntered;

    const recovery = canonical.runWithSystemUpdateLock(manager, lockName, async () => {
        recoveryEntered = true;
        if (serverFound) return 'found';
        return transact(indexedDB, storage, ownerB, () => ({
            kind: 'write', record: terminalA, result: 'terminal',
        }), false);
    });
    const sameOwnerRecovery = canonical.runWithSystemUpdateLock(manager, lockName, async () => {
        sameOwnerRecoveryEntered = true;
        if (serverFound) return 'found';
        return transact(indexedDB, storage, ownerA, () => ({
            kind: 'write', record: terminalA, result: 'terminal',
        }), false);
    });
    await new Promise((resolve) => setTimeout(resolve, 5));
    assert.equal(recoveryEntered, false, 'foreign recovery cannot pass a live dispatch document');
    assert.equal(sameOwnerRecoveryEntered, false, 'same-owner polling cannot pass its own live dispatch');
    assert.equal(posts, 0);
    releaseDispatch();
    assert.deepEqual(await dispatch, { kind: 'completed', value: 'sent' });
    assert.deepEqual(await recovery, { kind: 'completed', value: 'found' });
    assert.deepEqual(await sameOwnerRecovery, { kind: 'completed', value: 'found' });
    assert.equal(posts, 1);
    const stillActive = await transact(indexedDB, storage, ownerB, (current) => ({
        kind: 'keep', result: current,
    }), false);
    assert.ok(sameRecord(stillActive.result, authorizedA));

    let unavailableCallback = 0;
    assert.deepEqual(await canonical.runWithSystemUpdateLock(null, lockName, async () => {
        unavailableCallback += 1;
    }), { kind: 'unavailable' });
    assert.equal(unavailableCallback, 0, 'unsupported locking never runs a recovery callback');
});

test('initial canonical reconciliation never exposes an unlocked frame', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write', record: activeA, result: true,
    }));
    storage.removeItem(markerKey);

    let canonicalReady = false;
    let operationBlocks = false;
    assert.equal(canonical.systemUpdateInteractionBlocked(canonicalReady, operationBlocks), true);
    const gate = indexedDB.holdNextCommit();
    const reconciliation = transact(indexedDB, storage, ownerB, (current) => ({
        kind: 'keep', result: current,
    }), false);
    await gate.entered;
    assert.equal(canonical.systemUpdateInteractionBlocked(canonicalReady, operationBlocks), true);
    gate.release();
    const result = await reconciliation;
    operationBlocks = result.snapshot.record?.phase === 'active';
    canonicalReady = true;
    assert.equal(operationBlocks, true);
    assert.equal(canonical.systemUpdateInteractionBlocked(canonicalReady, operationBlocks), true);
});

test('reload side effects wait for exact canonical authority and consume a returned receipt once', () => {
    const matches = (left, right) => left.request_id === right.request_id
        && left.current_version === right.current_version
        && left.current_commit === right.current_commit
        && left.created_at === right.created_at
        && sameTarget(left.target, right.target);
    const optimisticMirror = markerA;
    let canonicalReady = false;
    let canonicalRequired = null;
    let scheduled = 0;
    let receiptWrites = 0;
    let reloadParam = markerA.request_id;

    const authorized = () => canonical.systemUpdateCanonicalReloadAuthorized(
        canonicalReady,
        canonicalRequired,
        optimisticMirror,
        matches,
    );
    if (authorized()) scheduled += 1;
    if (canonicalReady) reloadParam = null;
    assert.equal(scheduled, 0, 'an optimistic mirror cannot schedule a reload before IDB commits');
    assert.equal(reloadParam, markerA.request_id, 'the return receipt survives canonical reconciliation');

    canonicalReady = true;
    canonicalRequired = null;
    if (authorized()) scheduled += 1;
    assert.equal(scheduled, 0, 'authoritative absence retracts the optimistic mirror');
    assert.equal(reloadParam, markerA.request_id);

    canonicalReady = false;
    canonicalRequired = markerA;
    assert.equal(authorized(), false, 'even an exact snapshot waits until reconciliation is committed');
    canonicalReady = true;
    assert.equal(authorized(), true);
    if (reloadParam === markerA.request_id) {
        receiptWrites += 1;
    } else if (authorized()) {
        scheduled += 1;
    }
    assert.equal(receiptWrites, 1);
    assert.equal(scheduled, 0, 'returning from the exact reload never schedules a second reload');
    reloadParam = null;
    assert.equal(receiptWrites, 1, 'URL cleanup cannot create another receipt');

    canonicalReady = false;
    assert.equal(authorized(), false, 'a cross-tab wake retracts timer authority synchronously');
    canonicalReady = true;
    canonicalRequired = markerB;
    assert.equal(authorized(), false, 'a successor cannot inherit an old reload timer');
});

test('dialog focus follows a generic-to-exact modal replacement without dropping the guard', () => {
    const application = { inert: true };
    let genericFocuses = 0;
    let exactFocuses = 0;
    const generic = { isConnected: true, focus: () => { genericFocuses += 1; } };
    const exact = { isConnected: true, focus: () => { exactFocuses += 1; } };
    assert.equal(canonical.focusSystemUpdateDialog(generic, application), true);
    generic.isConnected = false;
    assert.equal(canonical.focusSystemUpdateDialog(exact, application), true);
    assert.equal(genericFocuses, 1);
    assert.equal(exactFocuses, 1);
    application.inert = false;
    assert.equal(canonical.focusSystemUpdateDialog(exact, application), false);
    assert.equal(exactFocuses, 1);
});

test('a cross-tab mirror notification re-establishes the barrier before canonical I/O', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    await transact(indexedDB, storage, ownerB, () => ({
        kind: 'write', record: null, result: true,
    }));
    let canonicalReady = true;
    let operationBlocks = false;
    assert.equal(canonical.systemUpdateInteractionBlocked(canonicalReady, operationBlocks), false);

    const gate = indexedDB.holdNextCommit();
    const writer = transact(indexedDB, storage, ownerA, () => ({
        kind: 'write', record: activeA, result: true,
    }), false);
    await gate.entered;
    assert.deepEqual(codec.decode(storage.getItem(markerKey)), activeA);

    canonicalReady = false;
    assert.equal(canonical.systemUpdateInteractionBlocked(canonicalReady, operationBlocks), true);
    const reader = transact(indexedDB, storage, ownerB, (current) => ({
        kind: 'keep', result: current,
    }), false);
    await new Promise((resolve) => setTimeout(resolve, 5));
    assert.equal(canonical.systemUpdateInteractionBlocked(canonicalReady, operationBlocks), true);
    gate.release();
    await writer;
    const result = await reader;
    operationBlocks = result.snapshot.record?.phase === 'active';
    canonicalReady = true;
    assert.equal(canonical.systemUpdateInteractionBlocked(canonicalReady, operationBlocks), true);
});

test('a retained success reload barrier survives an active and failed successor for frozen tabs', async () => {
    const indexedDB = new FakeIndexedDB();
    const storage = new MemoryStorage();
    const receipts = new Set([ownerA + ':' + markerA.request_id]);
    const hasReceipt = (owner, marker) => receipts.has(owner + ':' + marker.request_id);
    const durableSuccess = { ...succeededA, reload_scheduled: false };
    await transact(indexedDB, storage, ownerA, () => ({
        kind: 'write', record: durableSuccess, result: true,
    }));

    let posts = 0;
    const start = async (requested) => {
        const claim = await transact(indexedDB, storage, ownerA, (current) => {
            const completed = current?.phase === 'terminal'
                && current.outcome === 'succeeded'
                && !current.reload_scheduled
                && hasReceipt(ownerA, current.marker)
                && !sameMarker(current.marker, requested);
            const reloadOnly = current?.phase === 'reload'
                && hasReceipt(ownerA, current.marker)
                && !sameMarker(current.marker, requested);
            if (!current || completed || reloadOnly) {
                const barrier = current?.phase === 'reload' ? current.marker
                    : current?.phase === 'terminal' && current.outcome === 'succeeded'
                        ? current.marker : current?.required_reload;
                const record = {
                    state_version: 1,
                    phase: 'active',
                    marker: requested,
                    dispatch_owner: ownerA,
                    dispatch_state: 'claimed',
                    ...(barrier ? { required_reload: barrier } : {}),
                };
                return { kind: 'write', record, result: { owned: true, record } };
            }
            return { kind: 'keep', result: { owned: false, record: current } };
        }, false);
        if (!claim.result.owned) return 'adopted';
        const authorization = await transact(indexedDB, storage, ownerA, (current) => {
            if (current?.phase !== 'active' || !sameMarker(current.marker, requested)
                || current.dispatch_owner !== ownerA || current.dispatch_state !== 'claimed') {
                return { kind: 'keep', result: false };
            }
            return {
                kind: 'write', record: {
                    ...current,
                    dispatch_state: 'authorized',
                    dispatch_attempted_at: Date.now(),
                }, result: true,
            };
        }, false);
        if (authorization.kind === 'committed' && authorization.result) posts += 1;
        return authorization.result ? 'accepted' : 'failed';
    };

    assert.equal(await start(markerA), 'adopted', 'the completed exact marker cannot replay');
    assert.equal(posts, 0);
    assert.equal(await start(markerB), 'accepted');
    assert.equal(posts, 1);

    const frozenDuringActive = await transact(indexedDB, storage, ownerB, (current) => ({
        kind: 'keep', result: current?.required_reload,
    }), false);
    assert.ok(sameMarker(frozenDuringActive.result, markerA));
    assert.equal(hasReceipt(ownerB, frozenDuringActive.result), false);

    const failedB = {
        state_version: 1,
        phase: 'terminal',
        marker: markerB,
        outcome: 'failed',
        message: 'successor failed safely',
        reload_scheduled: false,
        completed_at: 4000,
        required_reload: markerA,
    };
    await transact(indexedDB, storage, ownerA, (current) => current?.phase === 'active'
        && sameMarker(current.marker, markerB)
        ? { kind: 'write', record: failedB, result: true }
        : { kind: 'keep', result: false }, false);
    const acknowledged = await transact(indexedDB, storage, ownerA, (current) => sameRecord(current, failedB)
        ? { kind: 'write', record: { state_version: 1, phase: 'reload', marker: markerA }, result: true }
        : { kind: 'keep', result: false }, false);
    assert.equal(acknowledged.result, true);
    assert.deepEqual(acknowledged.snapshot.record, { state_version: 1, phase: 'reload', marker: markerA });

    const frozenAfterFailure = await transact(indexedDB, storage, ownerB, (current) => ({
        kind: 'keep', result: current?.phase === 'reload' ? current.marker : null,
    }), false);
    assert.ok(sameMarker(frozenAfterFailure.result, markerA));
    assert.equal(hasReceipt(ownerB, markerA), false);
});

test('authentication publication resumes a paused guard synchronously without poll backoff', () => {
    let resumed = 0;
    let woken = 0;
    const unsubscribe = authSignal.subscribeSystemUpdateAuthentication((authenticated) => {
        if (!authenticated) return;
        resumed += 1;
        woken += 1;
    });
    authSignal.publishSystemUpdateAuthentication(true);
    assert.equal(resumed, 1);
    assert.equal(woken, 1);
    unsubscribe();
    authSignal.publishSystemUpdateAuthentication(false);
});

test('a 401 pause suppresses every wake poll until authentication resumes exactly once', () => {
    let authPaused = true;
    let polls = 0;
    const wake = () => {
        if (!authPaused) polls += 1;
    };
    wake();
    wake();
    assert.equal(polls, 0);
    const unsubscribe = authSignal.subscribeSystemUpdateAuthentication((authenticated) => {
        if (!authenticated || !authPaused) return;
        authPaused = false;
        wake();
    });
    authSignal.publishSystemUpdateAuthentication(true);
    assert.equal(polls, 1);
    unsubscribe();
    authSignal.publishSystemUpdateAuthentication(false);
});

test('authentication generations reject delayed 401 responses from the previous session', () => {
    let authGeneration = 0;
    let user = 'old-session';
    const requestA = authGeneration;
    const requestB = authGeneration;

    if (authSignal.shouldApplyUnauthorizedResponse(requestA, authGeneration)) {
        authGeneration += 1;
        user = null;
    }
    authGeneration += 1;
    user = 'new-session';
    if (authSignal.shouldApplyUnauthorizedResponse(requestB, authGeneration)) user = null;
    assert.equal(user, 'new-session');

    let publishedGeneration = 0;
    const unsubscribe = authSignal.subscribeSystemUpdateAuthentication((_authenticated, generation) => {
        publishedGeneration = generation;
    });
    authSignal.publishSystemUpdateAuthentication(true);
    const pollGeneration = publishedGeneration;
    authSignal.publishSystemUpdateAuthentication(false);
    authSignal.publishSystemUpdateAuthentication(true);
    assert.notEqual(pollGeneration, publishedGeneration);
    unsubscribe();
});
