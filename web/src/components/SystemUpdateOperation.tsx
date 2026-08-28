import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useLayoutEffect,
    useRef,
    useState,
    type ReactNode,
} from 'react';
import { flushSync } from 'react-dom';
import { CheckCircle2, Loader2, WifiOff, XCircle } from 'lucide-react';
import { Button } from './ui';
import { useI18n } from '../i18n';
import { replaceCurrentRouterURL, useNavigationBlocker } from '../router';
import { apiErrorText, readApiError } from '../lib/apiError';
import {
    systemUpdateDispatchAllowed,
    systemUpdateCanonicalReloadAuthorized,
    focusSystemUpdateDialog,
    systemUpdateNotFoundAction,
    runWithSystemUpdateLock,
    transactCanonicalRecord,
    type CanonicalRecordSnapshot,
    type MarkerCodec,
    type SystemUpdateLockManager,
} from '../lib/systemUpdateLease';
import { subscribeSystemUpdateAuthentication } from '../lib/systemUpdateAuthSignal';
import {
    acquireSystemUpdatePageLock,
    systemUpdateExactStatusPath,
    systemUpdateExactTrackingAllowed,
    systemUpdateGlobalNavigationBlocked,
    systemUpdateMutationLocked,
} from '../lib/systemUpdateWatchdog';
import { useSystemUpdateNavigationLease } from '../lib/useSystemUpdateNavigationLease';

type Translate = ReturnType<typeof useI18n>['t'];

export const SYSTEM_UPDATE_MARKER_KEY = 'celikpanel.system-update-operation.v1';
const POST_UPDATE_RELOAD_PARAM = '_cp_update';
const TAB_RELOADED_MARKER_KEY = 'celikpanel.system-update-reloaded.v1';
const POST_UPDATE_RELOAD_MS = 1500;
const POLL_MIN_MS = 1500;
const POLL_MAX_MS = 15000;
const POLL_REQUEST_TIMEOUT_MS = 10000;
const START_REQUEST_TIMEOUT_MS = 15000;
const NOT_FOUND_GRACE_MS = 120000;
const SYSTEM_UPDATE_STATE_DB = 'celikpanel-system-update-state-v1';
const SYSTEM_UPDATE_STATE_STORE = 'records';
const CANONICAL_RECORD_KEY = 'system-update-record';
const CANONICAL_TRANSACTION_TIMEOUT_MS = 3000;
const SYSTEM_UPDATE_DISPATCH_LOCK = 'celikpanel-system-update-dispatch-v1';

export type UpdateTarget = {
    version: string;
    commit: string;
    sequence: string;
    os: 'linux';
    arch: 'amd64' | 'arm64';
    archive_sha256: string;
    archive_size: string;
    published_at?: string;
};

export type UpdateMarker = {
    marker_version: 1;
    request_id: string;
    current_version: string;
    current_commit: string;
    target: UpdateTarget;
    created_at: number;
};

type UpdateStatus = {
    found: boolean;
    request_id: string;
    status?: 'queued' | 'running' | 'succeeded' | 'failed';
    target?: UpdateTarget;
    summary?: string;
};

type TrackingView = {
    operation: UpdateStatus | null;
    message: string;
    disconnected: boolean;
    lastAttemptAt: number | null;
};

type TerminalResult = {
    kind: 'succeeded' | 'failed';
    marker: UpdateMarker;
    message: string;
    reloadScheduled: boolean;
};

type ActiveUpdateRecord = {
    state_version: 1;
    phase: 'active';
    marker: UpdateMarker;
    dispatch_owner?: string;
    dispatch_state?: 'claimed' | 'authorized';
    dispatch_attempted_at?: number;
    required_reload?: UpdateMarker;
};

type TerminalUpdateRecord = {
    state_version: 1;
    phase: 'terminal';
    marker: UpdateMarker;
    outcome: 'succeeded' | 'failed';
    message: string;
    reload_scheduled: boolean;
    completed_at: number;
    required_reload?: UpdateMarker;
};

type ReloadUpdateRecord = {
    state_version: 1;
    phase: 'reload';
    marker: UpdateMarker;
};

type StoredUpdateRecord = ActiveUpdateRecord | TerminalUpdateRecord | ReloadUpdateRecord;

type PollOutcome =
    | { kind: 'retry'; message: string; disconnected: boolean; operation?: UpdateStatus }
    | { kind: 'auth'; message: string }
    | { kind: 'not-found'; message: string }
    | { kind: 'succeeded'; operation: UpdateStatus }
    | { kind: 'failed'; message: string; operation?: UpdateStatus };

type SystemUpdateStartResult =
    | { kind: 'accepted' | 'adopted' }
    | { kind: 'failed'; message: string };

type DispatchFence = {
    owner: string;
    attemptedAt: number;
};

type NotFoundRecoveryResult =
    | { kind: 'wait' | 'stale' }
    | { kind: 'terminal'; record: TerminalUpdateRecord };

type SystemUpdateOperationContextValue = {
    active: boolean;
    start: (marker: UpdateMarker) => Promise<SystemUpdateStartResult>;
};

type PendingRenderCommit = {
    marker: UpdateMarker;
    promise: Promise<boolean>;
    resolve: (committed: boolean) => void;
    timeout: number | null;
};

const requestIDPattern = /^[a-f0-9]{32}$/;
const commitPattern = /^[a-f0-9]{40}$/;
const digestPattern = /^[a-f0-9]{64}$/;
const decimalPattern = /^[1-9][0-9]*$/;

const SystemUpdateOperationContext = createContext<SystemUpdateOperationContextValue | null>(null);

export function validUpdateVersion(version: unknown): version is string {
    if (typeof version !== 'string' || version.length > 80) return false;
    const match = /^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(version);
    if (!match) return false;
    return !match[1]?.split('.').some((identifier) => /^0[0-9]+$/.test(identifier));
}

function validDecimal(value: unknown, maximum: bigint): value is string {
    if (typeof value !== 'string' || !decimalPattern.test(value) || value.length > 20) return false;
    try {
        const parsed = BigInt(value);
        return parsed > 0n && parsed <= maximum && parsed.toString(10) === value;
    } catch {
        return false;
    }
}

export function validUpdateTarget(target: unknown): target is UpdateTarget {
    if (!target || typeof target !== 'object') return false;
    const value = target as Record<string, unknown>;
    return validUpdateVersion(value.version)
        && typeof value.commit === 'string' && commitPattern.test(value.commit)
        && validDecimal(value.sequence, 9223372036854775807n)
        && value.os === 'linux' && (value.arch === 'amd64' || value.arch === 'arm64')
        && typeof value.archive_sha256 === 'string' && digestPattern.test(value.archive_sha256)
        && validDecimal(value.archive_size, 2147483648n)
        && (value.published_at === undefined || (typeof value.published_at === 'string'
            && value.published_at.length <= 40
            && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value.published_at)));
}

function validSummary(summary: unknown): summary is string {
    return typeof summary === 'string' && summary.length <= 240
        && !/[\x00-\x1f\x7f/\\]/.test(summary) && !summary.includes('://');
}

function decodeUpdateStatus(payload: unknown): UpdateStatus | null {
    if (!payload || typeof payload !== 'object') return null;
    const value = payload as Record<string, unknown>;
    if (typeof value.found !== 'boolean' || typeof value.request_id !== 'string'
        || !requestIDPattern.test(value.request_id)) return null;
    if (!value.found) return { found: false, request_id: value.request_id };
    if (value.status !== 'queued' && value.status !== 'running'
        && value.status !== 'succeeded' && value.status !== 'failed') return null;
    if (!validUpdateTarget(value.target)) return null;
    if (value.summary !== undefined && !validSummary(value.summary)) return null;
    return {
        found: true,
        request_id: value.request_id,
        status: value.status,
        target: value.target,
        ...(value.summary ? { summary: value.summary } : {}),
    };
}

export function sameUpdateTarget(left: UpdateTarget, right: UpdateTarget): boolean {
    return left.version === right.version
        && left.commit === right.commit
        && left.sequence === right.sequence
        && left.os === right.os
        && left.arch === right.arch
        && left.archive_sha256 === right.archive_sha256
        && left.archive_size === right.archive_size;
}

function decodeMarker(raw: string): UpdateMarker | null {
    if (!raw || raw.length > 4096) return null;
    try {
        const value = JSON.parse(raw) as Record<string, unknown>;
        if (value.marker_version !== 1
            || typeof value.request_id !== 'string' || !requestIDPattern.test(value.request_id)
            || !validUpdateVersion(value.current_version)
            || typeof value.current_commit !== 'string' || !commitPattern.test(value.current_commit)
            || typeof value.created_at !== 'number' || !Number.isFinite(value.created_at) || value.created_at <= 0
            || !validUpdateTarget(value.target)) return null;
        return value as UpdateMarker;
    } catch {
        return null;
    }
}

function activeRecord(
    marker: UpdateMarker,
    dispatchOwner?: string,
    requiredReload?: UpdateMarker,
    dispatchState: ActiveUpdateRecord['dispatch_state'] = 'claimed',
    dispatchAttemptedAt?: number,
): ActiveUpdateRecord {
    return {
        state_version: 1,
        phase: 'active',
        marker,
        ...(dispatchOwner ? { dispatch_owner: dispatchOwner, dispatch_state: dispatchState } : {}),
        ...(dispatchAttemptedAt !== undefined ? { dispatch_attempted_at: dispatchAttemptedAt } : {}),
        ...(requiredReload ? { required_reload: requiredReload } : {}),
    };
}

function reloadRecord(marker: UpdateMarker): ReloadUpdateRecord {
    return { state_version: 1, phase: 'reload', marker };
}

function decodeOptionalMarker(value: unknown): UpdateMarker | null | undefined {
    if (value === undefined) return undefined;
    try {
        return decodeMarker(JSON.stringify(value));
    } catch {
        return null;
    }
}

function decodeStoredRecord(raw: string): StoredUpdateRecord | null {
    if (!raw || raw.length > 8192) return null;
    try {
        const value = JSON.parse(raw) as Record<string, unknown>;
        if (value.marker_version === 1) {
            const marker = decodeMarker(raw);
            return marker ? activeRecord(marker) : null;
        }
        if (value.state_version !== 1
            || (value.phase !== 'active' && value.phase !== 'terminal' && value.phase !== 'reload')) return null;
        const encodedMarker = JSON.stringify(value.marker);
        const marker = decodeMarker(encodedMarker);
        if (!marker) return null;
        if (value.phase === 'reload') return reloadRecord(marker);
        const requiredReload = decodeOptionalMarker(value.required_reload);
        if (requiredReload === null) return null;
        if (value.phase === 'active') {
            const hasDispatch = value.dispatch_owner !== undefined
                || value.dispatch_state !== undefined
                || value.dispatch_attempted_at !== undefined;
            if (hasDispatch && (typeof value.dispatch_owner !== 'string'
                || !requestIDPattern.test(value.dispatch_owner)
                || (value.dispatch_state !== 'claimed' && value.dispatch_state !== 'authorized'))) return null;
            const authorized = value.dispatch_state === 'authorized';
            if (authorized !== (typeof value.dispatch_attempted_at === 'number'
                && Number.isFinite(value.dispatch_attempted_at)
                && value.dispatch_attempted_at > 0)) return null;
            if (value.dispatch_state === 'claimed' && value.dispatch_attempted_at !== undefined) return null;
            return activeRecord(
                marker,
                hasDispatch ? value.dispatch_owner as string : undefined,
                requiredReload,
                hasDispatch ? value.dispatch_state as ActiveUpdateRecord['dispatch_state'] : undefined,
                authorized ? value.dispatch_attempted_at as number : undefined,
            );
        }
        if ((value.outcome !== 'succeeded' && value.outcome !== 'failed')
            || typeof value.message !== 'string' || value.message.length > 512
            || /[\x00-\x1f\x7f]/.test(value.message)
            || typeof value.reload_scheduled !== 'boolean'
            || typeof value.completed_at !== 'number' || !Number.isFinite(value.completed_at)
            || value.completed_at <= 0) return null;
        return {
            state_version: 1,
            phase: 'terminal',
            marker,
            outcome: value.outcome,
            message: value.message,
            reload_scheduled: value.reload_scheduled,
            completed_at: value.completed_at,
            ...(requiredReload ? { required_reload: requiredReload } : {}),
        };
    } catch {
        return null;
    }
}

function readStoredRecord(): StoredUpdateRecord | null {
    try {
        const raw = localStorage.getItem(SYSTEM_UPDATE_MARKER_KEY);
        return raw ? decodeStoredRecord(raw) : null;
    } catch {
        return null;
    }
}

function mirrorMatchesCanonicalRecord(canonical: StoredUpdateRecord | null): boolean {
    try {
        const raw = localStorage.getItem(SYSTEM_UPDATE_MARKER_KEY);
        if (raw === null) return canonical === null;
        const mirrored = decodeStoredRecord(raw);
        return mirrored !== null && canonical !== null && recordMatches(mirrored, canonical);
    } catch {
        return false;
    }
}

function markerMatches(left: UpdateMarker | null | undefined, right: UpdateMarker): boolean {
    return left?.request_id === right.request_id
        && left.current_version === right.current_version
        && left.current_commit === right.current_commit
        && left.created_at === right.created_at
        && sameUpdateTarget(left.target, right.target);
}

function exactMarkerFingerprint(marker: UpdateMarker): string {
    return JSON.stringify(activeRecord(marker));
}

function tabHasReloadedMarker(marker: UpdateMarker): boolean {
    try {
        return sessionStorage.getItem(TAB_RELOADED_MARKER_KEY) === exactMarkerFingerprint(marker);
    } catch {
        return false;
    }
}

function rememberTabReloadedMarker(marker: UpdateMarker): boolean {
    try {
        const fingerprint = exactMarkerFingerprint(marker);
        if (sessionStorage.getItem(TAB_RELOADED_MARKER_KEY) === fingerprint) return false;
        sessionStorage.setItem(TAB_RELOADED_MARKER_KEY, fingerprint);
        return true;
    } catch {
        return false;
    }
}

function optionalMarkerMatches(left?: UpdateMarker, right?: UpdateMarker): boolean {
    return left ? Boolean(right && markerMatches(left, right)) : !right;
}

function requiredReloadFromRecord(record: StoredUpdateRecord | null): UpdateMarker | null {
    if (!record) return null;
    if (record.phase === 'reload' || (record.phase === 'terminal' && record.outcome === 'succeeded')) {
        return record.marker;
    }
    return record.required_reload ?? null;
}

function recordMatches(left: StoredUpdateRecord | null, right: StoredUpdateRecord): boolean {
    if (!left || left.phase !== right.phase || !markerMatches(left.marker, right.marker)) return false;
    if (left.phase === 'reload' && right.phase === 'reload') return true;
    if (left.phase === 'active' && right.phase === 'active') {
        return left.dispatch_owner === right.dispatch_owner
            && left.dispatch_state === right.dispatch_state
            && left.dispatch_attempted_at === right.dispatch_attempted_at
            && optionalMarkerMatches(left.required_reload, right.required_reload);
    }
    return left.phase === 'terminal' && right.phase === 'terminal'
        && left.outcome === right.outcome
        && left.message === right.message
        && left.reload_scheduled === right.reload_scheduled
        && left.completed_at === right.completed_at
        && optionalMarkerMatches(left.required_reload, right.required_reload);
}

const recordCodec: MarkerCodec<StoredUpdateRecord> = {
    decode: decodeStoredRecord,
    encode: JSON.stringify,
    matches: recordMatches,
};

function canonicalOptions(ownerID: string, allowLegacyBootstrap = true) {
    return {
        indexedDB,
        databaseName: SYSTEM_UPDATE_STATE_DB,
        storeName: SYSTEM_UPDATE_STATE_STORE,
        recordKey: CANONICAL_RECORD_KEY,
        ownerID,
        codec: recordCodec,
        legacyStorage: localStorage,
        legacyKey: SYSTEM_UPDATE_MARKER_KEY,
        mirrorStorage: localStorage,
        mirrorKey: SYSTEM_UPDATE_MARKER_KEY,
        allowLegacyBootstrap,
        timeoutMS: CANONICAL_TRANSACTION_TIMEOUT_MS,
    };
}

async function transactStoredRecord<Result>(
    ownerID: string,
    mutate: Parameters<typeof transactCanonicalRecord<StoredUpdateRecord, Result>>[1],
    allowLegacyBootstrap = true,
) {
    const result = await transactCanonicalRecord(
        canonicalOptions(ownerID, allowLegacyBootstrap),
        mutate,
    );
    return result;
}

export function createSystemUpdateRequestID(): string | null {
    try {
        const bytes = new Uint8Array(16);
        crypto.getRandomValues(bytes);
        return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
    } catch {
        return null;
    }
}

function browserSystemUpdateLockManager(): SystemUpdateLockManager | null {
    try {
        const locks = navigator.locks;
        if (!locks || typeof locks.request !== 'function') return null;
        return {
            request: <Result,>(name: string, callback: () => Promise<Result>) => (
                new Promise<Result>((resolve, reject) => {
                    try {
                        void locks.request(name, async () => {
                            try {
                                resolve(await callback());
                            } catch (error) {
                                reject(error);
                            }
                        }).catch(reject);
                    } catch (error) {
                        reject(error);
                    }
                })
            ),
        };
    } catch {
        return null;
    }
}

export function systemUpdateResponseHint(status: number, t: Translate): string {
    if (status === 401) return t('panelUpdate.sessionExpired');
    if (status === 403) return t('panelUpdate.adminOnly');
    if (status === 408 || status === 504) return t('panelUpdate.timeout');
    if (status === 409) return t('panelUpdate.busy');
    if (status === 429) return t('panelUpdate.rateLimited');
    if (status >= 500) return t('panelUpdate.restarting');
    return t('panelUpdate.invalidResponse');
}

async function pollExact(marker: UpdateMarker, t: Translate): Promise<PollOutcome> {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), POLL_REQUEST_TIMEOUT_MS);
    try {
        const response = await fetch(systemUpdateExactStatusPath(marker.request_id), {
            cache: 'no-store',
            credentials: 'same-origin',
            signal: controller.signal,
        });
        if (!response.ok) {
            if (response.status === 401) {
                return { kind: 'auth', message: systemUpdateResponseHint(response.status, t) };
            }
            if (response.status === 403) {
                return {
                    kind: 'retry',
                    message: systemUpdateResponseHint(response.status, t),
                    disconnected: false,
                };
            }
            return {
                kind: 'retry',
                message: systemUpdateResponseHint(response.status, t),
                disconnected: response.status >= 500,
            };
        }
        const payload = decodeUpdateStatus(await response.json());
        if (!payload) {
            return { kind: 'retry', message: t('panelUpdate.invalidResponse'), disconnected: false };
        }
        if (!payload.found) {
            return { kind: 'not-found', message: t('panelUpdate.notFoundYet') };
        }
        if (payload.request_id !== marker.request_id || !payload.target
            || !sameUpdateTarget(payload.target, marker.target)) {
            return { kind: 'failed', message: t('panelUpdate.identityMismatchCleared'), operation: payload };
        }
        if (payload.status === 'succeeded') return { kind: 'succeeded', operation: payload };
        if (payload.status === 'failed') {
            return { kind: 'failed', message: payload.summary || t('panelUpdate.failed'), operation: payload };
        }
        return {
            kind: 'retry',
            message: payload.status === 'running' ? t('panelUpdate.running') : t('panelUpdate.queued'),
            disconnected: false,
            operation: payload,
        };
    } catch {
        return { kind: 'retry', message: t('panelUpdate.connectionLost'), disconnected: true };
    } finally {
        window.clearTimeout(timeout);
    }
}

function formatElapsed(startedAt: number, now: number): string {
    const seconds = Math.max(0, Math.floor((now - startedAt) / 1000));
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const remainder = seconds % 60;
    return hours > 0
        ? `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}:${remainder.toString().padStart(2, '0')}`
        : `${minutes.toString().padStart(2, '0')}:${remainder.toString().padStart(2, '0')}`;
}

function terminalResultFromRecord(record: TerminalUpdateRecord): TerminalResult {
    return {
        kind: record.outcome,
        marker: record.marker,
        message: record.message,
        reloadScheduled: record.reload_scheduled,
    };
}

export function SystemUpdateOperationProvider({ children }: { children: ReactNode }) {
    const { t } = useI18n();
    const [initialRecord] = useState<StoredUpdateRecord | null>(() => readStoredRecord());
    const [canonicalReady, setCanonicalReady] = useState(false);
    const canonicalReadyRef = useRef(false);
    const canonicalRecordRef = useRef<StoredUpdateRecord | null>(null);
    const initialMarker = initialRecord?.phase === 'active' ? initialRecord.marker : null;
    const initialTerminal = initialRecord?.phase === 'terminal'
        ? terminalResultFromRecord(initialRecord)
        : null;
    const [provisional, setProvisional] = useState<UpdateMarker | null>(null);
    const provisionalRef = useRef<UpdateMarker | null>(null);
    const [marker, setMarker] = useState<UpdateMarker | null>(initialMarker);
    const markerRef = useRef<UpdateMarker | null>(initialMarker);
    const [view, setView] = useState<TrackingView>({
        operation: null,
        message: t('panelUpdate.running'),
        disconnected: false,
        lastAttemptAt: null,
    });
    const [terminal, setTerminal] = useState<TerminalResult | null>(initialTerminal);
    const terminalRef = useRef<TerminalResult | null>(initialTerminal);
    const terminalRecordRef = useRef<TerminalUpdateRecord | null>(
        initialRecord?.phase === 'terminal' ? initialRecord : null,
    );
    const initialRequiredReload = requiredReloadFromRecord(initialRecord);
    const [requiredReload, setRequiredReload] = useState<UpdateMarker | null>(initialRequiredReload);
    const requiredReloadRef = useRef<UpdateMarker | null>(initialRequiredReload);
    const [authPaused, setAuthPaused] = useState(true);
    const authPausedRef = useRef(true);
    const authenticatedRef = useRef(false);
    const [now, setNow] = useState(Date.now());
    const [reloadRetryNonce, setReloadRetryNonce] = useState(0);
    const [tabReloadReceiptNonce, setTabReloadReceiptNonce] = useState(0);
    const reloadTimerRef = useRef<number | null>(null);
    const [pendingReload, setPendingReload] = useState<UpdateMarker | null>(null);
    const pendingReloadRef = useRef<UpdateMarker | null>(null);
    const programmaticReloadRef = useRef(false);
    const applicationRef = useRef<HTMLDivElement>(null);
    const dialogRef = useRef<HTMLDivElement>(null);
    const focusBeforeLockRef = useRef<HTMLElement | null>(null);
    const claimInFlightRef = useRef(false);
    const pendingCommitRef = useRef<PendingRenderCommit | null>(null);
    const guardReadyRef = useRef(false);
    const pollWakeRef = useRef<(() => void) | null>(null);
    const authSignalGenerationRef = useRef(0);
    // Dispatch authority belongs to this live document only. sessionStorage is
    // deliberately not used: duplicated/opener tabs can inherit its values and
    // must never be mistaken for the document that owns the only start POST.
    const [tabOwnerID] = useState(() => createSystemUpdateRequestID());
    const terminalBlocks = terminal !== null && (
        terminal.kind === 'failed'
        || terminal.reloadScheduled
        || !tabHasReloadedMarker(terminal.marker)
    );
    const reloadRequired = requiredReload !== null && !tabHasReloadedMarker(requiredReload);
    const requiredReloadMarker = reloadRequired ? requiredReload : null;
    const exactMarker = pendingReload ?? requiredReloadMarker ?? provisional ?? marker ?? terminal?.marker ?? null;
    const modalIdentity = exactMarker ? exactMarkerFingerprint(exactMarker) : null;
    const operationBlocks = pendingReload !== null || reloadRequired
        || (!authPaused && (
            provisional !== null
            || marker !== null
            || terminalBlocks
        ));
    const navigationLease = useSystemUpdateNavigationLease(
        modalIdentity,
        exactMarker?.created_at ?? null,
        operationBlocks,
    );
    const navigationLeaseReleasedRef = useRef(navigationLease.released);
    navigationLeaseReleasedRef.current = navigationLease.released;
    const navigationLeaseIdentityRef = useRef(modalIdentity);
    navigationLeaseIdentityRef.current = modalIdentity;
    const backgroundTracking = marker !== null && navigationLease.released;
    const blocking = systemUpdateGlobalNavigationBlocked(
        operationBlocks,
        navigationLease.blocked,
    );
    const occupied = systemUpdateMutationLocked(
        canonicalReady,
        provisional !== null || marker !== null || terminalBlocks
            || pendingReload !== null || reloadRequired,
    );
    const navigationBlockedRef = useRef(blocking);
    navigationBlockedRef.current = blocking;
    useNavigationBlocker(navigationBlockedRef);

    const captureMeaningfulFocus = useCallback(() => {
        const active = document.activeElement;
        if (focusBeforeLockRef.current
            || !(active instanceof HTMLElement)
            || active === document.body
            || active === document.documentElement
            || !active.isConnected) return;
        focusBeforeLockRef.current = active;
    }, []);

    const settlePendingCommit = useCallback((exactMarker: UpdateMarker, committed: boolean) => {
        const pending = pendingCommitRef.current;
        if (!pending || !markerMatches(pending.marker, exactMarker)) return;
        pendingCommitRef.current = null;
        if (pending.timeout !== null) window.clearTimeout(pending.timeout);
        pending.resolve(committed);
    }, []);

    const establishProvisionalGuard = useCallback((
        exactMarker: UpdateMarker,
    ): Promise<boolean> => {
        const visibleTerminal = terminalRef.current;
        const completedHere = visibleTerminal?.kind === 'succeeded'
            && !visibleTerminal.reloadScheduled
            && tabHasReloadedMarker(visibleTerminal.marker);
        const replaceableTerminal = completedHere
            && !markerMatches(visibleTerminal.marker, exactMarker);
        const visibleReload = requiredReloadRef.current;
        const replaceableReload = visibleReload && tabHasReloadedMarker(visibleReload)
            && !markerMatches(visibleReload, exactMarker);
        const visible = provisionalRef.current ?? markerRef.current
            ?? (replaceableTerminal ? undefined : visibleTerminal?.marker)
            ?? (replaceableReload ? undefined : visibleReload);
        if (visible && !markerMatches(visible, exactMarker)) return Promise.resolve(false);
        if (visible && guardReadyRef.current && !authPausedRef.current) return Promise.resolve(true);
        const pending = pendingCommitRef.current;
        if (pending && markerMatches(pending.marker, exactMarker)) return pending.promise;
        if (!visible) captureMeaningfulFocus();
        provisionalRef.current = exactMarker;
        authPausedRef.current = false;
        let resolveCommit = (_committed: boolean) => {};
        const promise = new Promise<boolean>((resolve) => {
            resolveCommit = resolve;
        });
        pendingCommitRef.current = {
            marker: exactMarker,
            promise,
            resolve: resolveCommit,
            timeout: null,
        };
        flushSync(() => {
            setView({
                operation: null,
                message: t('panelUpdate.sending'),
                disconnected: false,
                lastAttemptAt: null,
            });
            setAuthPaused(false);
            setProvisional(exactMarker);
        });
        const pendingAfterRender = pendingCommitRef.current;
        if (pendingAfterRender && markerMatches(pendingAfterRender.marker, exactMarker)) {
            pendingAfterRender.timeout = window.setTimeout(
                () => settlePendingCommit(exactMarker, false),
                1000,
            );
        }
        return promise;
    }, [captureMeaningfulFocus, settlePendingCommit, t]);

    const resumeTrackingGuard = useCallback((exactMarker: UpdateMarker): Promise<boolean> => {
        if (navigationLeaseIdentityRef.current === exactMarkerFingerprint(exactMarker)
            && navigationLeaseReleasedRef.current) {
            // The exact request remains mutation-locked, but its monotonic
            // navigation lease has expired. Resume read-only tracking without
            // waiting for a modal guard that must never be reacquired.
            guardReadyRef.current = false;
            authPausedRef.current = false;
            settlePendingCommit(exactMarker, true);
            flushSync(() => setAuthPaused(false));
            return Promise.resolve(true);
        }
        if (!authPausedRef.current && guardReadyRef.current) return Promise.resolve(true);
        const pending = pendingCommitRef.current;
        if (pending && markerMatches(pending.marker, exactMarker)) return pending.promise;
        captureMeaningfulFocus();
        let resolveCommit = (_committed: boolean) => {};
        const promise = new Promise<boolean>((resolve) => {
            resolveCommit = resolve;
        });
        pendingCommitRef.current = {
            marker: exactMarker,
            promise,
            resolve: resolveCommit,
            timeout: null,
        };
        authPausedRef.current = false;
        flushSync(() => setAuthPaused(false));
        const pendingAfterRender = pendingCommitRef.current;
        if (pendingAfterRender && markerMatches(pendingAfterRender.marker, exactMarker)) {
            pendingAfterRender.timeout = window.setTimeout(
                () => settlePendingCommit(exactMarker, false),
                1000,
            );
        }
        return promise;
    }, [captureMeaningfulFocus, settlePendingCommit]);

    const cancelPostUpdateReload = useCallback((): boolean => {
        const hadPendingReload = reloadTimerRef.current !== null
            || pendingReloadRef.current !== null;
        if (reloadTimerRef.current !== null) {
            window.clearTimeout(reloadTimerRef.current);
            reloadTimerRef.current = null;
        }
        pendingReloadRef.current = null;
        programmaticReloadRef.current = false;
        return hadPendingReload;
    }, []);

    const adoptStoredRecord = useCallback((record: StoredUpdateRecord, message?: string) => {
        const paused = !authenticatedRef.current;
        const nextRequiredReload = requiredReloadFromRecord(record);
        const sameRequiredReload = optionalMarkerMatches(
            requiredReloadRef.current ?? undefined,
            nextRequiredReload ?? undefined,
        );
        if (!provisionalRef.current) {
            if (record.phase === 'active' && markerMatches(markerRef.current, record.marker)
                && sameRequiredReload) return;
            if (record.phase === 'terminal' && recordMatches(terminalRecordRef.current, record)
                && sameRequiredReload) return;
            if (record.phase === 'reload' && !markerRef.current && !terminalRef.current
                && sameRequiredReload) return;
        }
        if (!provisionalRef.current && !markerRef.current && !terminalRef.current
            && !requiredReloadRef.current) {
            captureMeaningfulFocus();
        }
        provisionalRef.current = null;
        authPausedRef.current = paused;
        requiredReloadRef.current = nextRequiredReload;
        if (record.phase === 'active') {
            markerRef.current = record.marker;
            terminalRef.current = null;
            terminalRecordRef.current = null;
            flushSync(() => {
                setProvisional(null);
                setMarker(record.marker);
                setTerminal(null);
                setRequiredReload(nextRequiredReload);
                setAuthPaused(paused);
                setView({
                    operation: null,
                    message: message ?? t('panelUpdate.running'),
                    disconnected: false,
                    lastAttemptAt: null,
                });
            });
            return;
        }
        if (record.phase === 'reload') {
            markerRef.current = null;
            terminalRef.current = null;
            terminalRecordRef.current = null;
            flushSync(() => {
                setProvisional(null);
                setMarker(null);
                setTerminal(null);
                setRequiredReload(record.marker);
                setAuthPaused(paused);
            });
            return;
        }
        const result = terminalResultFromRecord(record);
        markerRef.current = null;
        terminalRef.current = result;
        terminalRecordRef.current = record;
        flushSync(() => {
            setProvisional(null);
            setMarker(null);
            setTerminal(result);
            setRequiredReload(nextRequiredReload);
            setAuthPaused(paused);
        });
    }, [captureMeaningfulFocus, t]);

    const clearVisibleRecord = useCallback(() => {
        if (!provisionalRef.current && !markerRef.current && !terminalRef.current
            && !requiredReloadRef.current) return;
        provisionalRef.current = null;
        markerRef.current = null;
        terminalRef.current = null;
        terminalRecordRef.current = null;
        requiredReloadRef.current = null;
        authPausedRef.current = false;
        flushSync(() => {
            setProvisional(null);
            setMarker(null);
            setTerminal(null);
            setRequiredReload(null);
            setAuthPaused(false);
        });
    }, []);

    const adoptCanonicalSnapshot = useCallback((snapshot: CanonicalRecordSnapshot<StoredUpdateRecord>) => {
        const pending = pendingReloadRef.current;
        const canonicalRequired = requiredReloadFromRecord(snapshot.record);
        if (pending && !systemUpdateCanonicalReloadAuthorized(
            true,
            canonicalRequired,
            pending,
            markerMatches,
        ) && cancelPostUpdateReload()) {
            setPendingReload(null);
        }
        canonicalRecordRef.current = snapshot.record;
        if (snapshot.record) adoptStoredRecord(snapshot.record);
        else clearVisibleRecord();
    }, [adoptStoredRecord, cancelPostUpdateReload, clearVisibleRecord]);

    const markCanonicalPending = useCallback(() => {
        const wasReady = canonicalReadyRef.current;
        if (wasReady && navigationBlockedRef.current) captureMeaningfulFocus();
        const cancelledReload = cancelPostUpdateReload();
        if (!wasReady && !cancelledReload) return;
        canonicalReadyRef.current = false;
        flushSync(() => {
            if (cancelledReload) setPendingReload(null);
            setCanonicalReady(false);
        });
    }, [cancelPostUpdateReload, captureMeaningfulFocus]);

    const reconcileCanonicalRecord = useCallback(async (): Promise<StoredUpdateRecord | null | undefined> => {
        const ownerID = tabOwnerID;
        if (!ownerID) return undefined;
        const result = await transactStoredRecord(
            ownerID,
            (current) => ({ kind: 'keep', result: current }),
        );
        if (result.kind !== 'committed') return undefined;
        adoptCanonicalSnapshot(result.snapshot);
        if (!canonicalReadyRef.current) {
            canonicalReadyRef.current = true;
            flushSync(() => setCanonicalReady(true));
        }
        return result.result;
    }, [adoptCanonicalSnapshot, tabOwnerID]);

    const schedulePostUpdateReload = useCallback((exactMarker: UpdateMarker): boolean => {
        if (!systemUpdateCanonicalReloadAuthorized(
            canonicalReadyRef.current,
            requiredReloadFromRecord(canonicalRecordRef.current),
            exactMarker,
            markerMatches,
        )) return false;
        if (tabHasReloadedMarker(exactMarker)) return false;
        if (reloadTimerRef.current !== null) {
            return markerMatches(pendingReloadRef.current, exactMarker);
        }
        const next = new URL(window.location.href);
        if (next.searchParams.get(POST_UPDATE_RELOAD_PARAM) === exactMarker.request_id) {
            return false;
        }
        next.searchParams.set(POST_UPDATE_RELOAD_PARAM, exactMarker.request_id);
        pendingReloadRef.current = exactMarker;
        setPendingReload(exactMarker);
        reloadTimerRef.current = window.setTimeout(() => {
            programmaticReloadRef.current = true;
            window.location.replace(next.toString());
        }, POST_UPDATE_RELOAD_MS);
        return true;
    }, []);

    const commitTerminal = useCallback(async (
        exactMarker: UpdateMarker,
        kind: 'succeeded' | 'failed',
        rawMessage: string,
        dispatchFence?: DispatchFence,
    ): Promise<boolean> => {
        const ownerID = tabOwnerID;
        if (!ownerID) return false;
        const shouldReload = kind === 'succeeded'
            && new URL(window.location.href).searchParams.get(POST_UPDATE_RELOAD_PARAM) !== exactMarker.request_id;
        const message = rawMessage.replace(/[\x00-\x1f\x7f]/g, ' ').slice(0, 512);
        const completedAt = Date.now();
        const result = await transactStoredRecord<TerminalUpdateRecord | null>(
            ownerID,
            (current) => {
                if (current?.phase === 'terminal' && markerMatches(current.marker, exactMarker)) {
                    return { kind: 'keep', result: current };
                }
                if (current?.phase === 'active' && markerMatches(current.marker, exactMarker)) {
                    if (dispatchFence && (
                        current.dispatch_owner !== dispatchFence.owner
                        || current.dispatch_state !== 'authorized'
                        || current.dispatch_attempted_at !== dispatchFence.attemptedAt
                    )) {
                        return { kind: 'keep', result: null };
                    }
                    const record: TerminalUpdateRecord = {
                        state_version: 1,
                        phase: 'terminal',
                        marker: exactMarker,
                        outcome: kind,
                        message,
                        reload_scheduled: shouldReload,
                        completed_at: completedAt,
                        ...(kind === 'succeeded'
                            ? { required_reload: exactMarker }
                            : current.required_reload
                                ? { required_reload: current.required_reload }
                                : {}),
                    };
                    return { kind: 'write', record, result: record };
                }
                return { kind: 'keep', result: null };
            },
        );
        if (result.kind !== 'committed') {
            await reconcileCanonicalRecord();
            return false;
        }
        adoptCanonicalSnapshot(result.snapshot);
        return result.result !== null;
    }, [adoptCanonicalSnapshot, reconcileCanonicalRecord, tabOwnerID]);

    const postAuthorizedStartUnlocked = useCallback(async (
        exactMarker: UpdateMarker,
        dispatchFence: DispatchFence,
        approvalGeneration: number,
    ): Promise<SystemUpdateStartResult> => {
        const fenced = await transactStoredRecord<boolean>(
            dispatchFence.owner,
            (current) => ({
                kind: 'keep',
                result: Boolean(current?.phase === 'active'
                    && markerMatches(current.marker, exactMarker)
                    && current.dispatch_owner === dispatchFence.owner
                    && current.dispatch_state === 'authorized'
                    && current.dispatch_attempted_at === dispatchFence.attemptedAt),
            }),
            false,
        );
        if (fenced.kind !== 'committed' || !fenced.result) {
            await reconcileCanonicalRecord();
            return { kind: 'accepted' };
        }
        adoptCanonicalSnapshot(fenced.snapshot);
        if (!systemUpdateDispatchAllowed(
            approvalGeneration,
            authSignalGenerationRef.current,
            authenticatedRef.current,
            guardReadyRef.current && !authPausedRef.current,
        )) {
            const failure = t('panelUpdate.sessionExpiredBeforeStart');
            await commitTerminal(exactMarker, 'failed', failure, dispatchFence);
            return { kind: 'failed', message: failure };
        }
        const controller = new AbortController();
        const timeout = window.setTimeout(() => controller.abort(), START_REQUEST_TIMEOUT_MS);
        try {
            const response = await fetch('/api/v1/panel/update/start', {
                method: 'POST',
                credentials: 'same-origin',
                headers: { 'Content-Type': 'application/json' },
                signal: controller.signal,
                body: JSON.stringify({
                    request_id: exactMarker.request_id,
                    confirmed: true,
                    current_version: exactMarker.current_version,
                    current_commit: exactMarker.current_commit,
                    ...exactMarker.target,
                }),
            });
            if (response.ok) {
                if (markerMatches(markerRef.current, exactMarker)) {
                    setView((current) => ({
                        ...current,
                        message: current.operation ? current.message : t('panelUpdate.accepted'),
                        disconnected: false,
                    }));
                }
                return { kind: 'accepted' };
            }
            const apiError = await readApiError(response);
            const responseMessage = apiError.code
                ? apiErrorText(apiError, t)
                : systemUpdateResponseHint(response.status, t);
            const immutableCode = apiError.code === 'PANEL_UPDATE_INVALID_REQUEST'
                || apiError.code === 'PANEL_UPDATE_INVALID_CONFIRMATION'
                || apiError.code === 'PANEL_UPDATE_TARGET_CHANGED'
                || apiError.code === 'PANEL_UPDATE_START_REFUSED'
                || apiError.code === 'PANEL_UPDATE_UNAVAILABLE';
            const immutableStatus = response.status === 400 || response.status === 401
                || response.status === 403 || response.status === 404 || response.status === 405
                || response.status === 409 || response.status === 413 || response.status === 415
                || response.status === 422 || response.status === 429;
            if (immutableCode || immutableStatus) {
                const failure = response.status === 401 && !apiError.code
                    ? t('panelUpdate.sessionExpiredBeforeStart')
                    : responseMessage;
                if (await commitTerminal(exactMarker, 'failed', failure, dispatchFence)) {
                    return { kind: 'failed', message: failure };
                }
            }
            if (markerMatches(markerRef.current, exactMarker)) {
                setView((current) => ({
                    ...current,
                    message: responseMessage,
                    disconnected: response.status >= 500,
                }));
            }
            return { kind: 'accepted' };
        } catch {
            if (markerMatches(markerRef.current, exactMarker)) {
                setView((current) => ({
                    ...current,
                    message: t('panelUpdate.ambiguous'),
                    disconnected: true,
                }));
            }
            return { kind: 'accepted' };
        } finally {
            window.clearTimeout(timeout);
        }
    }, [adoptCanonicalSnapshot, commitTerminal, reconcileCanonicalRecord, t]);

    const postAuthorizedStart = useCallback(async (
        exactMarker: UpdateMarker,
        dispatchFence: DispatchFence,
        approvalGeneration: number,
    ): Promise<SystemUpdateStartResult> => {
        const manager = browserSystemUpdateLockManager();
        if (!manager) {
            // Compatibility fallback remains safe because every authorized
            // 404 recovery waits indefinitely when Web Locks are unavailable.
            return postAuthorizedStartUnlocked(exactMarker, dispatchFence, approvalGeneration);
        }
        const locked = await runWithSystemUpdateLock(
            manager,
            SYSTEM_UPDATE_DISPATCH_LOCK,
            () => postAuthorizedStartUnlocked(exactMarker, dispatchFence, approvalGeneration),
        );
        if (locked.kind === 'completed') return locked.value;
        await reconcileCanonicalRecord();
        return { kind: 'accepted' };
    }, [postAuthorizedStartUnlocked, reconcileCanonicalRecord]);

    const recoverNotFoundCanonical = useCallback(async (
        exactMarker: UpdateMarker,
        allowAuthorized: boolean,
    ): Promise<NotFoundRecoveryResult> => {
        const ownerID = tabOwnerID;
        if (!ownerID) return { kind: 'wait' };
        const attemptedAt = Date.now();
        const result = await transactStoredRecord<NotFoundRecoveryResult>(
            ownerID,
            (current) => {
                if (current?.phase !== 'active' || !markerMatches(current.marker, exactMarker)) {
                    return { kind: 'keep', result: { kind: 'stale' } };
                }
                const dispatchState = current.dispatch_state ?? 'legacy';
                if (dispatchState === 'authorized' && !allowAuthorized) {
                    return { kind: 'keep', result: { kind: 'wait' } };
                }
                const action = systemUpdateNotFoundAction(
                    dispatchState,
                    current.marker.created_at,
                    current.dispatch_attempted_at,
                    attemptedAt,
                    NOT_FOUND_GRACE_MS,
                );
                if (action === 'wait') return { kind: 'keep', result: { kind: 'wait' } };
                const terminalRecord: TerminalUpdateRecord = {
                    state_version: 1,
                    phase: 'terminal',
                    marker: exactMarker,
                    outcome: 'failed',
                    message: t('panelUpdate.notAccepted'),
                    reload_scheduled: false,
                    completed_at: attemptedAt,
                    ...(current.required_reload ? { required_reload: current.required_reload } : {}),
                };
                return {
                    kind: 'write',
                    record: terminalRecord,
                    result: { kind: 'terminal', record: terminalRecord },
                };
            },
            false,
        );
        if (result.kind !== 'committed') {
            await reconcileCanonicalRecord();
            return { kind: 'wait' };
        }
        adoptCanonicalSnapshot(result.snapshot);
        return result.result;
    }, [adoptCanonicalSnapshot, reconcileCanonicalRecord, t, tabOwnerID]);

    const recoverNotFound = useCallback(async (
        exactMarker: UpdateMarker,
    ): Promise<NotFoundRecoveryResult> => {
        const current = canonicalRecordRef.current;
        const authorized = current?.phase === 'active'
            && markerMatches(current.marker, exactMarker)
            && current.dispatch_state === 'authorized';
        if (!authorized) return recoverNotFoundCanonical(exactMarker, false);

        const manager = browserSystemUpdateLockManager();
        if (!manager) return { kind: 'wait' };
        const locked = await runWithSystemUpdateLock(
            manager,
            SYSTEM_UPDATE_DISPATCH_LOCK,
            async () => {
                // The first 404 happened outside this lock. Read the server
                // again after every same- or cross-document dispatch callback;
                // only an exact second 404 permits authorized cleanup.
                const verified = await pollExact(exactMarker, t);
                if (verified.kind !== 'not-found') return { kind: 'wait' } as NotFoundRecoveryResult;
                return recoverNotFoundCanonical(exactMarker, true);
            },
        );
        return locked.kind === 'completed' ? locked.value : { kind: 'wait' };
    }, [recoverNotFoundCanonical, t]);

    const acknowledgeTerminal = useCallback(async (record: TerminalUpdateRecord): Promise<boolean> => {
        if (record.outcome !== 'failed') return false;
        const ownerID = tabOwnerID;
        if (!ownerID) return false;
        const result = await transactStoredRecord<boolean>(
            ownerID,
            (current) => recordMatches(current, record)
                ? {
                    kind: 'write',
                    record: record.required_reload ? reloadRecord(record.required_reload) : null,
                    result: true,
                }
                : { kind: 'keep', result: false },
            false,
        );
        if (result.kind !== 'committed') {
            await reconcileCanonicalRecord();
            return false;
        }
        adoptCanonicalSnapshot(result.snapshot);
        return result.result;
    }, [adoptCanonicalSnapshot, reconcileCanonicalRecord, tabOwnerID]);

    useEffect(() => {
        markerRef.current = marker;
    }, [marker]);

    useEffect(() => {
        terminalRef.current = terminal;
    }, [terminal]);

    useEffect(() => {
        authPausedRef.current = authPaused;
    }, [authPaused]);

    useEffect(() => () => {
        const pending = pendingCommitRef.current;
        pendingCommitRef.current = null;
        if (pending && pending.timeout !== null) window.clearTimeout(pending.timeout);
        pending?.resolve(false);
        cancelPostUpdateReload();
    }, [cancelPostUpdateReload]);

    useEffect(() => {
        if (canonicalReady) return undefined;
        let cancelled = false;
        let retryTimer: number | null = null;
        const reconcileUntilReady = async () => {
            const result = await reconcileCanonicalRecord();
            if (cancelled || result !== undefined) return;
            retryTimer = window.setTimeout(() => void reconcileUntilReady(), POLL_MIN_MS);
        };
        void reconcileUntilReady();
        return () => {
            cancelled = true;
            if (retryTimer !== null) window.clearTimeout(retryTimer);
        };
    }, [canonicalReady, reconcileCanonicalRecord]);

    useEffect(() => {
        if (!canonicalReady || provisional || pendingReload) return;
        try {
            const current = new URL(window.location.href);
            const reloadID = current.searchParams.get(POST_UPDATE_RELOAD_PARAM);
            if (!reloadID) return;
            if (requiredReload?.request_id === reloadID
                && !tabHasReloadedMarker(requiredReload)) return;
            if (terminal?.kind === 'succeeded'
                && reloadID === terminal.marker.request_id
                && terminal.reloadScheduled) return;
            current.searchParams.delete(POST_UPDATE_RELOAD_PARAM);
            replaceCurrentRouterURL(current.pathname + current.search + current.hash);
        } catch {
            // A stale same-origin cache buster carries no authority.
        }
    }, [canonicalReady, pendingReload, provisional, requiredReload, tabReloadReceiptNonce, terminal]);

    useEffect(() => {
        const onStorage = (event: StorageEvent) => {
            if (event.storageArea !== localStorage || event.key !== SYSTEM_UPDATE_MARKER_KEY) return;
            markCanonicalPending();
            void reconcileCanonicalRecord();
        };
        window.addEventListener('storage', onStorage);
        return () => window.removeEventListener('storage', onStorage);
    }, [markCanonicalPending, reconcileCanonicalRecord]);

    useEffect(() => {
        const reconcileOnWake = () => {
            if (!mirrorMatchesCanonicalRecord(canonicalRecordRef.current)) markCanonicalPending();
            void reconcileCanonicalRecord().then(() => pollWakeRef.current?.());
        };
        const onVisibility = () => {
            if (document.visibilityState === 'visible') reconcileOnWake();
        };
        window.addEventListener('focus', reconcileOnWake);
        window.addEventListener('online', reconcileOnWake);
        window.addEventListener('pageshow', reconcileOnWake);
        document.addEventListener('visibilitychange', onVisibility);
        return () => {
            window.removeEventListener('focus', reconcileOnWake);
            window.removeEventListener('online', reconcileOnWake);
            window.removeEventListener('pageshow', reconcileOnWake);
            document.removeEventListener('visibilitychange', onVisibility);
        };
    }, [markCanonicalPending, reconcileCanonicalRecord]);

    useEffect(() => {
        if (!canonicalReady || !requiredReload) return;
        if (!systemUpdateCanonicalReloadAuthorized(
            true,
            requiredReloadFromRecord(canonicalRecordRef.current),
            requiredReload,
            markerMatches,
        )) return;
        const returned = new URL(window.location.href).searchParams
            .get(POST_UPDATE_RELOAD_PARAM) === requiredReload.request_id;
        if (returned) {
            const canonical = canonicalRecordRef.current;
            if (canonical?.phase === 'terminal'
                && canonical.outcome === 'succeeded'
                && markerMatches(canonical.marker, requiredReload)
                && canonical.reload_scheduled) return;
            if (rememberTabReloadedMarker(requiredReload)) {
                setTabReloadReceiptNonce((current) => current + 1);
            }
            return;
        }
        schedulePostUpdateReload(requiredReload);
    }, [canonicalReady, requiredReload, schedulePostUpdateReload]);

    useEffect(() => {
        if (!canonicalReady || terminal?.kind !== 'succeeded') return;
        const exactRecord = terminalRecordRef.current;
        if (!exactRecord || exactRecord.outcome !== 'succeeded'
            || !markerMatches(exactRecord.marker, terminal.marker)
            || exactRecord.reload_scheduled !== terminal.reloadScheduled) return;
        let cancelled = false;
        let retryTimer: number | null = null;
        const returnedFromExactReload = new URL(window.location.href)
            .searchParams.get(POST_UPDATE_RELOAD_PARAM) === terminal.marker.request_id;
        if (!returnedFromExactReload) return;
        const consumeOrSchedule = async () => {
            const ownerID = tabOwnerID;
            if (!ownerID) return;
            const result = await transactStoredRecord<TerminalUpdateRecord | null>(
                ownerID,
                (current) => {
                    if (current?.phase !== 'terminal' || !recordMatches(current, exactRecord)) {
                        if (current?.phase === 'terminal'
                            && markerMatches(current.marker, exactRecord.marker)
                            && current.outcome === 'succeeded'
                            && !current.reload_scheduled) {
                            return { kind: 'keep', result: current };
                        }
                        return { kind: 'keep', result: null };
                    }
                    if (!returnedFromExactReload || !current.reload_scheduled) {
                        return { kind: 'keep', result: current };
                    }
                    const consumed: TerminalUpdateRecord = {
                        ...current,
                        message: t('panelUpdate.succeeded'),
                        reload_scheduled: false,
                    };
                    return { kind: 'write', record: consumed, result: consumed };
                },
                false,
            );
            if (cancelled) return;
            if (result.kind !== 'committed') {
                await reconcileCanonicalRecord();
                if (!cancelled && returnedFromExactReload
                    && terminalRecordRef.current?.reload_scheduled) {
                    retryTimer = window.setTimeout(
                        () => setReloadRetryNonce((current) => current + 1),
                        POLL_MIN_MS,
                    );
                }
                return;
            }
            const committed = result.snapshot.record;
            if (returnedFromExactReload
                && committed?.phase === 'terminal'
                && committed.outcome === 'succeeded'
                && markerMatches(committed.marker, exactRecord.marker)
                && !committed.reload_scheduled
                && rememberTabReloadedMarker(committed.marker)) {
                setTabReloadReceiptNonce((current) => current + 1);
            }
            adoptCanonicalSnapshot(result.snapshot);
        };
        void consumeOrSchedule();
        return () => {
            cancelled = true;
            if (retryTimer !== null) window.clearTimeout(retryTimer);
        };
    }, [adoptCanonicalSnapshot, canonicalReady, reconcileCanonicalRecord, reloadRetryNonce, schedulePostUpdateReload, t, tabOwnerID, terminal]);

    useEffect(() => subscribeSystemUpdateAuthentication((authenticated, generation) => {
        authSignalGenerationRef.current = generation;
        authenticatedRef.current = authenticated;
        const exactMarker = pendingReloadRef.current ?? requiredReloadRef.current
            ?? provisionalRef.current ?? markerRef.current ?? terminalRef.current?.marker;
        if (!authenticated) {
            if (!exactMarker) return;
            authPausedRef.current = true;
            guardReadyRef.current = false;
            flushSync(() => setAuthPaused(true));
            return;
        }
        if (!authPausedRef.current) return;
        if (!exactMarker) {
            authPausedRef.current = false;
            flushSync(() => setAuthPaused(false));
            return;
        }
        void resumeTrackingGuard(exactMarker).then(async (guarded) => {
            if (!guarded) return;
            await reconcileCanonicalRecord();
            pollWakeRef.current?.();
        });
    }), [reconcileCanonicalRecord, resumeTrackingGuard]);

    useEffect(() => {
        if (!systemUpdateExactTrackingAllowed(marker !== null, canonicalReady, backgroundTracking)) {
            return undefined;
        }
        const exactMarker = marker;
        if (!exactMarker) return undefined;
        let cancelled = false;
        let timer: number | null = null;
        let delay = POLL_MIN_MS;
        let inFlight = false;
        let pollAgain = false;

        const schedule = (wait: number) => {
            if (cancelled) return;
            if (timer !== null) window.clearTimeout(timer);
            timer = window.setTimeout(() => void run(), wait);
        };
        const run = async () => {
            if (cancelled || authPausedRef.current) return;
            if (inFlight) {
                pollAgain = true;
                return;
            }
            inFlight = true;
            const canonical = await reconcileCanonicalRecord();
            if (cancelled) {
                inFlight = false;
                return;
            }
            if (canonical === undefined && !backgroundTracking) {
                inFlight = false;
                schedule(POLL_MIN_MS);
                return;
            }
            if (canonical !== undefined
                && (canonical?.phase !== 'active' || !markerMatches(canonical.marker, exactMarker))) {
                inFlight = false;
                return;
            }
            const requestAuthGeneration = authSignalGenerationRef.current;
            const outcome = await pollExact(exactMarker, t);
            inFlight = false;
            if (cancelled || !markerMatches(markerRef.current, exactMarker)) return;
            if (requestAuthGeneration !== authSignalGenerationRef.current) {
                if (!authPausedRef.current) {
                    pollAgain = false;
                    schedule(0);
                }
                return;
            }

            const attemptAt = Date.now();
            if (outcome.kind === 'auth') {
                authPausedRef.current = true;
                guardReadyRef.current = false;
                flushSync(() => {
                    setAuthPaused(true);
                    setView({
                        operation: null,
                        message: outcome.message,
                        disconnected: false,
                        lastAttemptAt: attemptAt,
                    });
                });
                return;
            }
            if (authPausedRef.current && !await resumeTrackingGuard(exactMarker)) {
                schedule(POLL_MIN_MS);
                return;
            }
            if (outcome.kind === 'not-found') {
                setView({
                    operation: null,
                    message: outcome.message,
                    disconnected: false,
                    lastAttemptAt: attemptAt,
                });
                const recovery = await recoverNotFound(exactMarker);
                if (recovery.kind === 'terminal' || recovery.kind === 'stale') return;
                delay = POLL_MIN_MS;
                schedule(delay);
                return;
            }
            if (outcome.kind === 'succeeded') {
                const willReload = new URL(window.location.href).searchParams.get(POST_UPDATE_RELOAD_PARAM)
                    !== exactMarker.request_id;
                const committed = await commitTerminal(
                    exactMarker,
                    'succeeded',
                    willReload
                        ? t('panelUpdate.reloading', { version: exactMarker.target.version })
                        : t('panelUpdate.succeeded'),
                );
                if (!committed) schedule(POLL_MIN_MS);
                return;
            }
            if (outcome.kind === 'failed') {
                const committed = await commitTerminal(exactMarker, 'failed', outcome.message);
                if (!committed) schedule(POLL_MIN_MS);
                return;
            }
            setView({
                operation: outcome.operation ?? null,
                message: outcome.message,
                disconnected: outcome.disconnected,
                lastAttemptAt: attemptAt,
            });
            if (pollAgain) {
                pollAgain = false;
                delay = POLL_MIN_MS;
                schedule(0);
                return;
            }
            schedule(delay);
            delay = Math.min(POLL_MAX_MS, Math.round(delay * 1.6));
        };
        const wake = () => {
            if (cancelled) return;
            delay = POLL_MIN_MS;
            if (inFlight) {
                pollAgain = true;
                return;
            }
            schedule(0);
        };
        pollWakeRef.current = wake;
        schedule(0);
        return () => {
            cancelled = true;
            if (pollWakeRef.current === wake) pollWakeRef.current = null;
            if (timer !== null) window.clearTimeout(timer);
        };
    }, [backgroundTracking, canonicalReady, commitTerminal, marker, reconcileCanonicalRecord, recoverNotFound, resumeTrackingGuard, t]);

    useLayoutEffect(() => {
        if (!blocking) return undefined;
        const application = applicationRef.current;
        const releasePageLock = acquireSystemUpdatePageLock(
            application,
            () => programmaticReloadRef.current,
        );
        const pendingGuard = pendingCommitRef.current;
        guardReadyRef.current = focusSystemUpdateDialog(dialogRef.current, application);
        if (pendingGuard
            && dialogRef.current?.isConnected
            && application?.inert) {
            settlePendingCommit(pendingGuard.marker, true);
        }
        const keepFocusInDialog = (event: FocusEvent) => {
            if (dialogRef.current && event.target instanceof Node && !dialogRef.current.contains(event.target)) {
                dialogRef.current.focus();
            }
        };
        document.addEventListener('focusin', keepFocusInDialog);
        return () => {
            guardReadyRef.current = false;
            document.removeEventListener('focusin', keepFocusInDialog);
            releasePageLock();
            const focusTarget = focusBeforeLockRef.current;
            focusBeforeLockRef.current = null;
            const meaningfulTarget = focusTarget?.isConnected
                && focusTarget !== document.body
                && focusTarget !== document.documentElement
                ? focusTarget
                : null;
            const restoreFocus = (candidate: HTMLElement | null): boolean => {
                if (!candidate?.isConnected
                    || candidate === document.body
                    || candidate === document.documentElement) return false;
                candidate.focus();
                return document.activeElement === candidate;
            };
            if (!restoreFocus(meaningfulTarget)) {
                const startButton = document.getElementById('panel-update-start-button');
                const mainContent = document.getElementById('celikpanel-main-content');
                if (!restoreFocus(startButton instanceof HTMLElement ? startButton : null)) {
                    restoreFocus(mainContent instanceof HTMLElement ? mainContent : null);
                }
            }
        };
    }, [blocking, settlePendingCommit]);

    useEffect(() => {
        if (!blocking && marker === null) return undefined;
        setNow(Date.now());
        const timer = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(timer);
    }, [blocking, marker]);

    const abandonProvisional = useCallback((exactMarker: UpdateMarker) => {
        if (!markerMatches(provisionalRef.current, exactMarker)) return;
        provisionalRef.current = null;
        flushSync(() => setProvisional(null));
    }, []);

    const claimCanonicalRecord = useCallback(async (
        exactMarker: UpdateMarker,
    ): Promise<{ kind: 'owned' | 'adopted'; record: StoredUpdateRecord } | { kind: 'failed' }> => {
        const ownerID = tabOwnerID;
        if (!ownerID) return { kind: 'failed' };
        const result = await transactStoredRecord<{ owned: boolean; record: StoredUpdateRecord }>(
            ownerID,
            (current) => {
                const completedTerminal = current?.phase === 'terminal'
                    && current.outcome === 'succeeded'
                    && !current.reload_scheduled
                    && tabHasReloadedMarker(current.marker)
                    && !markerMatches(current.marker, exactMarker);
                const completedReload = current?.phase === 'reload'
                    && tabHasReloadedMarker(current.marker)
                    && !markerMatches(current.marker, exactMarker);
                if (!current || completedTerminal || completedReload) {
                    const requested = activeRecord(
                        exactMarker,
                        ownerID,
                        requiredReloadFromRecord(current) ?? undefined,
                    );
                    return {
                        kind: 'write',
                        record: requested,
                        result: { owned: true, record: requested },
                    };
                }
                return { kind: 'keep', result: { owned: false, record: current } };
            },
        );
        if (result.kind !== 'committed') {
            await reconcileCanonicalRecord();
            return { kind: 'failed' };
        }
        adoptCanonicalSnapshot(result.snapshot);
        return {
            kind: result.result.owned ? 'owned' : 'adopted',
            record: result.result.record,
        };
    }, [adoptCanonicalSnapshot, reconcileCanonicalRecord, tabOwnerID]);

    const dispatchOwnedStart = useCallback(async (
        exactMarker: UpdateMarker,
        approvalGeneration: number,
    ): Promise<SystemUpdateStartResult> => {
        const ownerID = tabOwnerID;
        if (!ownerID) return { kind: 'failed', message: t('panelUpdate.markerFailed') };
        if (!systemUpdateDispatchAllowed(
            approvalGeneration,
            authSignalGenerationRef.current,
            authenticatedRef.current,
            guardReadyRef.current && !authPausedRef.current,
        )) {
            return { kind: 'failed', message: t('panelUpdate.markerFailed') };
        }
        const attemptedAt = Date.now();
        const authorized = await transactStoredRecord<boolean>(
            ownerID,
            (current) => {
                if (current?.phase !== 'active'
                    || !markerMatches(current.marker, exactMarker)
                    || current.dispatch_owner !== ownerID
                    || current.dispatch_state !== 'claimed') {
                    return { kind: 'keep', result: false };
                }
                return {
                    kind: 'write',
                    record: {
                        ...current,
                        dispatch_state: 'authorized',
                        dispatch_attempted_at: attemptedAt,
                    },
                    result: true,
                };
            },
            false,
        );
        if (authorized.kind !== 'committed' || !authorized.result) {
            await reconcileCanonicalRecord();
            return { kind: 'failed', message: t('panelUpdate.markerFailed') };
        }
        adoptCanonicalSnapshot(authorized.snapshot);
        return postAuthorizedStart(
            exactMarker,
            { owner: ownerID, attemptedAt },
            approvalGeneration,
        );
    }, [adoptCanonicalSnapshot, postAuthorizedStart, reconcileCanonicalRecord, t, tabOwnerID]);

    const start = useCallback((exactMarker: UpdateMarker): Promise<SystemUpdateStartResult> => {
        if (!canonicalReadyRef.current) {
            return Promise.resolve({ kind: 'failed', message: t('panelUpdate.markerFailed') });
        }
        const approvalGeneration = authSignalGenerationRef.current;
        if (!authenticatedRef.current || authPausedRef.current) {
            return Promise.resolve({ kind: 'failed', message: t('panelUpdate.markerFailed') });
        }
        const visibleTerminal = terminalRef.current;
        const completedHere = visibleTerminal?.kind === 'succeeded'
            && !visibleTerminal.reloadScheduled
            && tabHasReloadedMarker(visibleTerminal.marker)
            && !markerMatches(visibleTerminal.marker, exactMarker);
        const visibleReload = requiredReloadRef.current;
        const replaceableReload = visibleReload && tabHasReloadedMarker(visibleReload)
            && !markerMatches(visibleReload, exactMarker);
        const current = provisionalRef.current ?? markerRef.current
            ?? (completedHere ? undefined : visibleTerminal?.marker)
            ?? (replaceableReload ? undefined : visibleReload);
        if (current) {
            return Promise.resolve(markerMatches(current, exactMarker)
                ? { kind: 'adopted' }
                : { kind: 'failed', message: t('panelUpdate.markerFailed') });
        }
        if (claimInFlightRef.current) {
            return Promise.resolve({ kind: 'failed', message: t('panelUpdate.markerFailed') });
        }
        claimInFlightRef.current = true;
        const guardCommit = establishProvisionalGuard(exactMarker);
        return (async () => {
            try {
                if (!await guardCommit) return { kind: 'failed', message: t('panelUpdate.markerFailed') };
                const claim = await claimCanonicalRecord(exactMarker);
                if (claim.kind === 'adopted') return { kind: 'adopted' };
                if (claim.kind !== 'owned' || claim.record.phase !== 'active'
                    || !markerMatches(claim.record.marker, exactMarker)) {
                    abandonProvisional(exactMarker);
                    return { kind: 'failed', message: t('panelUpdate.markerFailed') };
                }
                return await dispatchOwnedStart(exactMarker, approvalGeneration);
            } catch {
                abandonProvisional(exactMarker);
                return { kind: 'failed', message: t('panelUpdate.markerFailed') };
            } finally {
                if (markerMatches(provisionalRef.current, exactMarker)
                    && !markerRef.current) {
                    abandonProvisional(exactMarker);
                }
                claimInFlightRef.current = false;
            }
        })();
    }, [abandonProvisional, claimCanonicalRecord, dispatchOwnedStart, establishProvisionalGuard, t]);

    const dismissTerminal = async () => {
        const stored = terminalRecordRef.current;
        if (!stored) return;
        await acknowledgeTerminal(stored);
    };
    useLayoutEffect(() => {
        if (!blocking) return;
        const ready = focusSystemUpdateDialog(dialogRef.current, applicationRef.current);
        guardReadyRef.current = ready;
        const pendingGuard = pendingCommitRef.current;
        if (ready && pendingGuard) settlePendingCommit(pendingGuard.marker, true);
    }, [blocking, modalIdentity, settlePendingCommit]);
    const displayedTerminal = provisional || marker || pendingReload || requiredReloadMarker ? null : terminal;
    const terminalKind = pendingReload || requiredReloadMarker ? 'succeeded' : displayedTerminal?.kind;
    const disconnected = !pendingReload && !requiredReloadMarker && marker !== null && view.disconnected;
    const title = terminalKind === 'failed'
        ? t('panelUpdate.failed')
        : terminalKind === 'succeeded'
            ? t('panelUpdate.succeeded')
            : t('panelUpdate.title');
    const message = pendingReload || requiredReloadMarker
        ? t('panelUpdate.reloading', { version: (pendingReload ?? requiredReloadMarker)!.target.version })
        : displayedTerminal?.message ?? view.message;
    const backgroundVisible = navigationLease.released && !authPaused && !blocking && exactMarker !== null
        && (provisional !== null || marker !== null || pendingReload !== null
            || requiredReloadMarker !== null || terminalBlocks);

    return (
        <SystemUpdateOperationContext.Provider value={{
            active: occupied,
            start,
        }}>
            <div ref={applicationRef} className="contents" aria-hidden={blocking ? true : undefined}>
                {children}
            </div>
            {backgroundVisible && exactMarker && (
                <aside
                    aria-labelledby={'system-update-background-title'}
                    className={'fixed bottom-4 right-4 z-50 max-w-sm rounded-xl border bg-surface p-4 shadow-lg'}
                >
                    <p id={'system-update-background-title'} className={'font-semibold'}>{title}</p>
                    <p
                        role={terminalKind === 'failed' ? 'alert' : 'status'}
                        aria-live={terminalKind === 'failed' ? 'assertive' : 'polite'}
                        className={'mt-1 text-sm text-fg-muted'}
                    >
                        {message}
                    </p>
                    <p className={'mt-2 text-xs text-fg-subtle'}>{t('panelUpdate.watch')}</p>
                    <p className={'mt-3 font-mono text-xs text-fg'}>
                        {exactMarker.target.version} · T+{formatElapsed(exactMarker.created_at, now)}
                    </p>
                    <p className={'mt-1 break-all text-xs text-fg-subtle'}>ID {exactMarker.request_id}</p>
                    {displayedTerminal?.kind === 'failed' && (
                        <div className={'mt-3 flex justify-end'}>
                            <Button type={'button'} onClick={() => void dismissTerminal()}>
                                {t('dnssrv.continue')}
                            </Button>
                        </div>
                    )}
                </aside>
            )}
            {blocking && !exactMarker && (
                <div ref={dialogRef} role="dialog" aria-modal="true" tabIndex={-1}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-950/80 p-4">
                    <div className="rounded-2xl bg-surface p-6 text-center shadow-2xl">
                        <Loader2 className="mx-auto h-7 w-7 animate-spin text-primary" />
                        <p role="status" className="mt-4 font-semibold">{t('panelUpdate.canonicalChecking')}</p>
                    </div>
                </div>
            )}
            {blocking && exactMarker && (
                <div
                    ref={dialogRef}
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="system-update-operation-title"
                    aria-describedby="system-update-operation-status"
                    tabIndex={-1}
                    onKeyDown={(event) => {
                        if (event.key === 'Escape' || ((marker || pendingReload || requiredReloadMarker)
                            && event.key === 'Tab')) {
                            event.preventDefault();
                            dialogRef.current?.focus();
                        }
                    }}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-950/80 p-4 backdrop-blur-sm outline-none"
                >
                    <div className="w-full max-w-lg rounded-2xl border border-border-strong bg-surface p-6 text-center shadow-2xl">
                        <span className={`mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl ${
                            terminalKind === 'failed'
                                ? 'bg-danger/10 text-danger'
                                : terminalKind === 'succeeded'
                                    ? 'bg-success/10 text-success'
                                    : disconnected
                                        ? 'bg-warning/10 text-warning'
                                        : 'bg-primary/10 text-primary'
                        }`}>
                            {terminalKind === 'failed' ? <XCircle className="h-7 w-7" />
                                : terminalKind === 'succeeded' ? <CheckCircle2 className="h-7 w-7" />
                                    : disconnected ? <WifiOff className="h-7 w-7" />
                                        : <Loader2 className="h-7 w-7 animate-spin" />}
                        </span>
                        <h2 id="system-update-operation-title" className="text-xl font-semibold text-fg">{title}</h2>
                        <p
                            id="system-update-operation-status"
                            role="status"
                            aria-live="polite"
                            className="mt-2 text-sm font-medium text-fg-muted"
                        >
                            {message}
                        </p>
                        <p className="mt-4 rounded-lg border border-border bg-surface-2 px-4 py-3 text-xs leading-5 text-fg-subtle">
                            {t('panelUpdate.interactionLocked')}
                        </p>
                        <dl className="mt-4 grid grid-cols-2 gap-3 rounded-lg border border-border bg-surface-subtle p-3 text-left text-xs">
                            <div>
                                <dt className="text-fg-subtle">{t('panelUpdate.targetVersion')}</dt>
                                <dd className="mt-1 font-mono font-semibold text-fg">{exactMarker.target.version}</dd>
                            </div>
                            <div>
                                <dt className="text-fg-subtle">T+</dt>
                                <dd className="mt-1 font-mono font-semibold text-fg">{formatElapsed(exactMarker.created_at, now)}</dd>
                            </div>
                            <div className="col-span-2">
                                <dt className="text-fg-subtle">ID</dt>
                                <dd className="mt-1 break-all font-mono text-fg">{exactMarker.request_id}</dd>
                            </div>
                            {marker && view.lastAttemptAt && (
                                <div className="col-span-2">
                                    <dt className="text-fg-subtle">UTC</dt>
                                    <dd className="mt-1 font-mono text-fg">{new Date(view.lastAttemptAt).toLocaleTimeString()}</dd>
                                </div>
                            )}
                        </dl>
                        {displayedTerminal?.kind === 'failed' && (
                            <div className="mt-5 flex justify-center">
                                <Button type="button" onClick={() => void dismissTerminal()}>
                                    {t('dnssrv.continue')}
                                </Button>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </SystemUpdateOperationContext.Provider>
    );
}

export function useSystemUpdateOperation(): SystemUpdateOperationContextValue {
    const context = useContext(SystemUpdateOperationContext);
    if (!context) throw new Error('useSystemUpdateOperation must be used within SystemUpdateOperationProvider');
    return context;
}
