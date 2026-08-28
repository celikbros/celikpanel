import { useCallback, useEffect, useLayoutEffect, useReducer, useRef } from 'react';
import {
    SYSTEM_UPDATE_BLOCKING_DEADLINE_MS,
    SYSTEM_UPDATE_NAVIGATION_RELEASE_KEY,
    rememberSystemUpdateNavigationRelease,
    systemUpdateNavigationBlocked,
    systemUpdateNavigationReleasedInStorage,
    type SystemUpdateNavigationReleaseStorage,
} from './systemUpdateWatchdog';

type TimerHandle = ReturnType<typeof setTimeout>;

export type SystemUpdateNavigationLeaseRuntime = {
    now?: () => number;
    monotonicNow?: () => number;
    schedule?: (callback: () => void, delay: number) => TimerHandle;
    cancel?: (timer: TimerHandle) => void;
    storage?: SystemUpdateNavigationReleaseStorage | null;
    subscribe?: (released: (identity: string) => void) => () => void;
    subscribeWake?: (wake: () => void) => () => void;
};

export type SystemUpdateNavigationLease = {
    blocked: boolean;
    released: boolean;
    release: () => void;
};

const defaultNow = () => Date.now();
const defaultMonotonicNow = () => typeof performance === 'undefined' ? Date.now() : performance.now();
const defaultSchedule = (callback: () => void, delay: number) => setTimeout(callback, delay);
const defaultCancel = (timer: TimerHandle) => clearTimeout(timer);

function defaultStorage(): SystemUpdateNavigationReleaseStorage | null {
    try {
        return typeof window === 'undefined' ? null : window.localStorage;
    } catch {
        return null;
    }
}

function defaultSubscribe(released: (identity: string) => void): () => void {
    const storage = defaultStorage();
    if (typeof window === 'undefined' || !storage) return () => {};
    const onStorage = (event: StorageEvent) => {
        if (event.storageArea !== storage
            || event.key !== SYSTEM_UPDATE_NAVIGATION_RELEASE_KEY
            || typeof event.newValue !== 'string') return;
        released(event.newValue);
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
}

function defaultSubscribeWake(wake: () => void): () => void {
    if (typeof window === 'undefined' || typeof document === 'undefined') return () => {};
    const onVisibility = () => {
        if (document.visibilityState === 'visible') wake();
    };
    window.addEventListener('focus', wake);
    window.addEventListener('pageshow', wake);
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
        window.removeEventListener('focus', wake);
        window.removeEventListener('pageshow', wake);
        document.removeEventListener('visibilitychange', onVisibility);
    };
}

function initiallyBlocked(
    identity: string | null,
    createdAt: number | null,
    now: number,
    storage: SystemUpdateNavigationReleaseStorage | null,
): boolean {
    if (!identity || createdAt === null) return false;
    if (systemUpdateNavigationReleasedInStorage(storage, identity)) return false;
    return systemUpdateNavigationBlocked(createdAt, now);
}

// The lease is intentionally monotonic for an exact operation identity. Once
// released, no phase transition, authentication event, wall-clock correction,
// reload, or sibling-tab event may reacquire the global navigation lock.
export function useSystemUpdateNavigationLease(
    identity: string | null,
    createdAt: number | null,
    requested: boolean,
    runtime: SystemUpdateNavigationLeaseRuntime = {},
): SystemUpdateNavigationLease {
    const now = runtime.now ?? defaultNow;
    const monotonicNow = runtime.monotonicNow ?? defaultMonotonicNow;
    const schedule = runtime.schedule ?? defaultSchedule;
    const cancel = runtime.cancel ?? defaultCancel;
    const storage = runtime.storage === undefined ? defaultStorage() : runtime.storage;
    const subscribe = runtime.subscribe ?? defaultSubscribe;
    const subscribeWake = runtime.subscribeWake ?? defaultSubscribeWake;
    const [, render] = useReducer((value: number) => value + 1, 0);
    const stateRef = useRef<{ identity: string | null; blocked: boolean; engaged: boolean } | null>(null);

    if (stateRef.current === null || stateRef.current.identity !== identity) {
        stateRef.current = {
            identity,
            blocked: initiallyBlocked(identity, createdAt, now(), storage),
            engaged: false,
        };
    }
    if (requested && stateRef.current.blocked) stateRef.current.engaged = true;

    const release = useCallback((releasedIdentity: string, persist: boolean) => {
        const state = stateRef.current;
        if (!state?.blocked || state.identity !== releasedIdentity) return;
        state.blocked = false;
        if (persist) rememberSystemUpdateNavigationRelease(storage, releasedIdentity);
        render();
    }, [storage]);

    const blocked = stateRef.current.blocked;

    // If a lock that was actually shown becomes inapplicable (authentication
    // pause, terminal transition, or exact-record reconciliation), release it
    // permanently before the next paint. Resuming the same identity can never
    // re-open the full-page modal.
    useLayoutEffect(() => {
        if (identity && blocked && stateRef.current?.engaged && !requested) release(identity, true);
    }, [blocked, identity, release, requested]);

    useEffect(() => {
        if (!identity || createdAt === null || !blocked) return undefined;
        const observedNow = now();
        const observedMonotonic = monotonicNow();
        const elapsed = observedNow - createdAt;
        const remaining = SYSTEM_UPDATE_BLOCKING_DEADLINE_MS - elapsed;
        if (!Number.isFinite(remaining) || remaining <= 0 || elapsed < 0) {
            release(identity, true);
            return undefined;
        }

        // setTimeout is monotonic within the live document. It therefore
        // cannot be extended by a wall-clock rollback after this point.
        const checkDeadline = () => {
            const wallElapsed = now() - createdAt;
            const monotonicElapsed = monotonicNow() - observedMonotonic;
            if (!Number.isFinite(wallElapsed) || wallElapsed < 0
                || wallElapsed >= SYSTEM_UPDATE_BLOCKING_DEADLINE_MS
                || (Number.isFinite(monotonicElapsed) && monotonicElapsed >= remaining)) {
                release(identity, true);
            }
        };
        const timer = schedule(checkDeadline, remaining);
        const unsubscribe = subscribe((releasedIdentity) => {
            if (releasedIdentity === identity) release(identity, false);
        });
        const unsubscribeWake = subscribeWake(checkDeadline);
        return () => {
            cancel(timer);
            unsubscribe();
            unsubscribeWake();
        };
    }, [blocked, cancel, createdAt, identity, monotonicNow, now, release, schedule, subscribe, subscribeWake]);

    useEffect(() => {
        if (identity && !blocked) rememberSystemUpdateNavigationRelease(storage, identity);
    }, [blocked, identity, storage]);

    const releaseLease = useCallback(() => {
        if (identity) release(identity, true);
    }, [identity, release]);

    return { blocked, released: !blocked, release: releaseLease };
}
