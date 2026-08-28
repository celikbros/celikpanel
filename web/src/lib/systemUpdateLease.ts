export interface StorageLike {
    readonly length: number;
    getItem(key: string): string | null;
    key(index: number): string | null;
    removeItem(key: string): void;
    setItem(key: string, value: string): void;
}

export type MarkerCodec<Marker> = {
    decode: (raw: string) => Marker | null;
    encode: (marker: Marker) => string;
    matches: (left: Marker | null, right: Marker) => boolean;
};

export type CanonicalRecordSnapshot<Record> = {
    canonical_version: 1;
    revision: number;
    writer_id: string;
    record: Record | null;
};

export type CanonicalMutationDecision<Record, Result> =
    | { kind: 'keep'; result: Result }
    | { kind: 'write'; record: Record | null; result: Result };

export type CanonicalTransactionResult<Record, Result> =
    | { kind: 'committed'; snapshot: CanonicalRecordSnapshot<Record>; result: Result }
    | { kind: 'failed' };

export type CanonicalRecordOptions<Record> = {
    indexedDB: IDBFactory;
    databaseName: string;
    storeName: string;
    recordKey: string;
    ownerID: string;
    codec: MarkerCodec<Record>;
    legacyStorage?: StorageLike;
    legacyKey?: string;
    mirrorStorage?: StorageLike;
    mirrorKey?: string;
    allowLegacyBootstrap?: boolean;
    timeoutMS?: number;
};

export type SystemUpdateDispatchState = 'legacy' | 'claimed' | 'authorized';

export type SystemUpdateNotFoundAction = 'wait' | 'fail-dispatch';

export type SystemUpdateLockManager = {
    request: <Result>(name: string, callback: () => Promise<Result>) => Promise<Result>;
};

export type SystemUpdateLockResult<Result> =
    | { kind: 'completed'; value: Result }
    | { kind: 'unavailable' };

// Web Locks are the only optional liveness path for a document that did not
// authorize an exact POST. The callback is never retried: a rejected or
// unavailable lock fails closed so a network side effect cannot be duplicated.
export async function runWithSystemUpdateLock<Result>(
    manager: SystemUpdateLockManager | null,
    name: string,
    callback: () => Promise<Result>,
): Promise<SystemUpdateLockResult<Result>> {
    if (!manager) return { kind: 'unavailable' };
    let entered = false;
    try {
        const value = await manager.request(name, async () => {
            if (entered) throw new Error('system update lock callback repeated');
            entered = true;
            return callback();
        });
        return entered ? { kind: 'completed', value } : { kind: 'unavailable' };
    } catch {
        return { kind: 'unavailable' };
    }
}

export function systemUpdateInteractionBlocked(
    canonicalReady: boolean,
    operationBlocks: boolean,
): boolean {
    return !canonicalReady || operationBlocks;
}

export function systemUpdateCanonicalReloadAuthorized<Marker>(
    canonicalReady: boolean,
    canonicalRequired: Marker | null,
    candidate: Marker | null,
    matches: (left: Marker, right: Marker) => boolean,
): boolean {
    return canonicalReady
        && canonicalRequired !== null
        && candidate !== null
        && matches(canonicalRequired, candidate);
}

export function focusSystemUpdateDialog(
    dialog: { isConnected: boolean; focus: () => void } | null,
    application: { inert: boolean } | null,
): boolean {
    if (!dialog?.isConnected || !application?.inert) return false;
    dialog.focus();
    return true;
}

export function systemUpdateDispatchAllowed(
    approvalGeneration: number,
    currentGeneration: number,
    authenticated: boolean,
    guardReady: boolean,
): boolean {
    return Number.isSafeInteger(approvalGeneration)
        && approvalGeneration >= 0
        && approvalGeneration === currentGeneration
        && authenticated
        && guardReady;
}

export function systemUpdateNotFoundAction(
    dispatchState: SystemUpdateDispatchState,
    markerCreatedAt: number,
    dispatchAttemptedAt: number | undefined,
    now: number,
    graceMS: number,
): SystemUpdateNotFoundAction {
    if (!Number.isFinite(now) || !Number.isFinite(graceMS) || graceMS < 0) return 'wait';
    const startedAt = dispatchState === 'authorized'
        ? dispatchAttemptedAt
        : markerCreatedAt;
    return startedAt !== undefined && Number.isFinite(startedAt) && now - startedAt >= graceMS
        ? 'fail-dispatch'
        : 'wait';
}

type PersistedCanonicalRecord = {
    canonical_version: 1;
    revision: number;
    writer_id: string;
    encoded_record: string | null;
};

const ownerPattern = /^[a-f0-9]{32}$/;

function decodeCanonicalSnapshot<Record>(
    value: unknown,
    codec: MarkerCodec<Record>,
): CanonicalRecordSnapshot<Record> | null {
    if (!value || typeof value !== 'object') return null;
    const persisted = value as Partial<PersistedCanonicalRecord>;
    if (persisted.canonical_version !== 1
        || !Number.isSafeInteger(persisted.revision) || (persisted.revision ?? 0) < 1
        || typeof persisted.writer_id !== 'string' || !ownerPattern.test(persisted.writer_id)
        || (persisted.encoded_record !== null && typeof persisted.encoded_record !== 'string')) return null;
    if (persisted.encoded_record === null) {
        return {
            canonical_version: 1,
            revision: persisted.revision!,
            writer_id: persisted.writer_id,
            record: null,
        };
    }
    const record = codec.decode(persisted.encoded_record!);
    if (!record) return null;
    return {
        canonical_version: 1,
        revision: persisted.revision!,
        writer_id: persisted.writer_id,
        record,
    };
}

function encodeCanonicalSnapshot<Record>(
    snapshot: CanonicalRecordSnapshot<Record>,
    codec: MarkerCodec<Record>,
): PersistedCanonicalRecord {
    return {
        canonical_version: 1,
        revision: snapshot.revision,
        writer_id: snapshot.writer_id,
        encoded_record: snapshot.record === null ? null : codec.encode(snapshot.record),
    };
}

function restoreMirrorRaw(storage: StorageLike | undefined, key: string | undefined, raw: string | null): void {
    if (!storage || !key) return;
    try {
        if (raw === null) storage.removeItem(key);
        else storage.setItem(key, raw);
    } catch {
        // The authoritative IndexedDB record remains fail-closed.
    }
}

function repairCanonicalMirror<Record>(
    options: CanonicalRecordOptions<Record>,
    fallbackRaw: string | null,
): Promise<void> {
    return new Promise((resolve) => {
        if (!options.mirrorStorage || !options.mirrorKey) {
            resolve();
            return;
        }
        let request: IDBOpenDBRequest;
        try {
            request = options.indexedDB.open(options.databaseName, 1);
        } catch {
            restoreMirrorRaw(options.mirrorStorage, options.mirrorKey, fallbackRaw);
            resolve();
            return;
        }
        request.onerror = () => {
            restoreMirrorRaw(options.mirrorStorage, options.mirrorKey, fallbackRaw);
            resolve();
        };
        request.onblocked = request.onerror;
        request.onsuccess = () => {
            const database = request.result;
            if (!database.objectStoreNames.contains(options.storeName)) {
                database.close();
                restoreMirrorRaw(options.mirrorStorage, options.mirrorKey, fallbackRaw);
                resolve();
                return;
            }
            let transaction: IDBTransaction;
            try {
                transaction = database.transaction(options.storeName, 'readonly');
                const get = transaction.objectStore(options.storeName).get(options.recordKey);
                get.onsuccess = () => {
                    if (get.result === undefined) {
                        restoreMirrorRaw(options.mirrorStorage, options.mirrorKey, fallbackRaw);
                        return;
                    }
                    const snapshot = decodeCanonicalSnapshot(get.result, options.codec);
                    if (snapshot) {
                        mirrorCanonicalSnapshot(
                            options.mirrorStorage!,
                            options.mirrorKey!,
                            snapshot,
                            options.codec,
                        );
                    }
                };
                transaction.oncomplete = () => {
                    database.close();
                    resolve();
                };
                transaction.onabort = transaction.oncomplete;
                transaction.onerror = () => {
                    // onabort/oncomplete closes the database.
                };
            } catch {
                database.close();
                restoreMirrorRaw(options.mirrorStorage, options.mirrorKey, fallbackRaw);
                resolve();
            }
        };
    });
}

/**
 * Runs a synchronous compare-and-mutate callback while one IndexedDB
 * readwrite transaction owns the canonical record. A suspended or crashed
 * caller cannot later overwrite a successor: IndexedDB either keeps the
 * transaction queued or aborts it before another writer can commit.
 *
 * When the canonical record does not exist yet, a valid legacy localStorage
 * marker is migrated in this same transaction. Once a canonical tombstone
 * exists, localStorage is only a mirror and can never recreate old authority.
 */
export function transactCanonicalRecord<Record, Result>(
    options: CanonicalRecordOptions<Record>,
    mutate: (
        current: Record | null,
        snapshot: CanonicalRecordSnapshot<Record>,
    ) => CanonicalMutationDecision<Record, Result> | null,
): Promise<CanonicalTransactionResult<Record, Result>> {
    return new Promise((resolve) => {
        if (!ownerPattern.test(options.ownerID)) {
            resolve({ kind: 'failed' });
            return;
        }
        let settled = false;
        let transaction: IDBTransaction | null = null;
        let database: IDBDatabase | null = null;
        let committedSnapshot: CanonicalRecordSnapshot<Record> | null = null;
        let mutationResult: Result | undefined;
        let hasMutationResult = false;
        let mirrorBeforeMutation: string | null = null;
        try {
            mirrorBeforeMutation = options.mirrorStorage && options.mirrorKey
                ? options.mirrorStorage.getItem(options.mirrorKey)
                : null;
        } catch {
            mirrorBeforeMutation = null;
        }
        const finish = (result: CanonicalTransactionResult<Record, Result>) => {
            if (settled) return;
            settled = true;
            globalThis.clearTimeout(timeout);
            database?.close();
            resolve(result);
        };
        const timeout = globalThis.setTimeout(() => {
            try {
                if (transaction) {
                    transaction.abort();
                    return;
                }
            } catch {
                // The transaction may already be completing.
            }
            finish({ kind: 'failed' });
        }, options.timeoutMS ?? 2000);
        let request: IDBOpenDBRequest;
        try {
            request = options.indexedDB.open(options.databaseName, 1);
        } catch {
            finish({ kind: 'failed' });
            return;
        }
        request.onblocked = () => finish({ kind: 'failed' });
        request.onerror = () => finish({ kind: 'failed' });
        request.onupgradeneeded = () => {
            const opened = request.result;
            if (!opened.objectStoreNames.contains(options.storeName)) {
                opened.createObjectStore(options.storeName);
            }
        };
        request.onsuccess = () => {
            database = request.result;
            if (settled) {
                database.close();
                return;
            }
            try {
                transaction = database.transaction(options.storeName, 'readwrite');
                const store = transaction.objectStore(options.storeName);
                const get = store.get(options.recordKey);
                const abort = () => {
                    try {
                        transaction?.abort();
                    } catch {
                        finish({ kind: 'failed' });
                    }
                };
                get.onerror = abort;
                get.onsuccess = () => {
                    let snapshot: CanonicalRecordSnapshot<Record> | null = null;
                    let bootstrapped = false;
                    if (get.result === undefined) {
                        if (!options.allowLegacyBootstrap) {
                            abort();
                            return;
                        }
                        let legacyRecord: Record | null = null;
                        try {
                            const raw = options.legacyStorage && options.legacyKey
                                ? options.legacyStorage.getItem(options.legacyKey)
                                : null;
                            if (raw !== null) {
                                legacyRecord = options.codec.decode(raw);
                                // A corrupt, user-controlled legacy hint must not permanently
                                // deny access to the canonical transaction. Bootstrap a durable
                                // null tombstone; the caller may then claim a fresh record in this
                                // same transaction. Once written, the bad hint cannot resurrect.
                                if (!legacyRecord) legacyRecord = null;
                            }
                        } catch {
                            abort();
                            return;
                        }
                        snapshot = {
                            canonical_version: 1,
                            revision: 0,
                            writer_id: options.ownerID,
                            record: legacyRecord,
                        };
                        bootstrapped = true;
                    } else {
                        snapshot = decodeCanonicalSnapshot(get.result, options.codec);
                        if (!snapshot) {
                            abort();
                            return;
                        }
                    }
                    let decision: CanonicalMutationDecision<Record, Result> | null;
                    try {
                        decision = mutate(snapshot.record, snapshot);
                    } catch {
                        abort();
                        return;
                    }
                    if (!decision) {
                        abort();
                        return;
                    }
                    mutationResult = decision.result;
                    hasMutationResult = true;
                    if (decision.kind === 'write' || bootstrapped) {
                        committedSnapshot = {
                            canonical_version: 1,
                            revision: snapshot.revision + 1,
                            writer_id: options.ownerID,
                            record: decision.kind === 'write' ? decision.record : snapshot.record,
                        };
                        try {
                            store.put(
                                encodeCanonicalSnapshot(committedSnapshot, options.codec),
                                options.recordKey,
                            );
                        } catch {
                            abort();
                            return;
                        }
                    } else {
                        committedSnapshot = snapshot;
                    }
                    if (options.mirrorStorage && options.mirrorKey
                        && !mirrorCanonicalSnapshot(
                            options.mirrorStorage,
                            options.mirrorKey,
                            committedSnapshot,
                            options.codec,
                        )) abort();
                };
                transaction.onabort = () => {
                    void repairCanonicalMirror(options, mirrorBeforeMutation)
                        .finally(() => finish({ kind: 'failed' }));
                };
                transaction.onerror = () => {
                    // onabort is the authoritative failure event.
                };
                transaction.oncomplete = () => {
                    if (!committedSnapshot || !hasMutationResult) {
                        finish({ kind: 'failed' });
                        return;
                    }
                    finish({
                        kind: 'committed',
                        snapshot: committedSnapshot,
                        result: mutationResult as Result,
                    });
                };
            } catch {
                finish({ kind: 'failed' });
            }
        };
    });
}

export function mirrorCanonicalSnapshot<Record>(
    storage: StorageLike,
    markerKey: string,
    snapshot: CanonicalRecordSnapshot<Record>,
    codec: MarkerCodec<Record>,
): boolean {
    try {
        if (snapshot.record === null) {
            storage.removeItem(markerKey);
            return storage.getItem(markerKey) === null;
        }
        const encoded = codec.encode(snapshot.record);
        storage.setItem(markerKey, encoded);
        return storage.getItem(markerKey) === encoded;
    } catch {
        return false;
    }
}
