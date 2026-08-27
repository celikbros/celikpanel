import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useRef,
    useState,
    type ReactNode,
} from 'react';
import { CheckCircle2, Loader2, WifiOff, XCircle } from 'lucide-react';
import { Button } from './ui';
import { useI18n } from '../i18n';

type Translate = ReturnType<typeof useI18n>['t'];

export const SYSTEM_UPDATE_MARKER_KEY = 'celikpanel.system-update-operation.v1';
const POST_UPDATE_RELOAD_PARAM = '_cp_update';
const POST_UPDATE_RELOAD_MS = 1500;
const POLL_MIN_MS = 1500;
const POLL_MAX_MS = 15000;
const POLL_REQUEST_TIMEOUT_MS = 10000;
const NOT_FOUND_GRACE_MS = 120000;

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

type PollOutcome =
    | { kind: 'retry'; message: string; disconnected: boolean; operation?: UpdateStatus }
    | { kind: 'succeeded'; operation: UpdateStatus }
    | { kind: 'failed'; message: string; operation?: UpdateStatus };

type SystemUpdateOperationContextValue = {
    active: boolean;
    begin: (marker: UpdateMarker) => boolean;
    noteAccepted: (marker: UpdateMarker) => void;
    noteUncertain: (marker: UpdateMarker, message: string, disconnected?: boolean) => void;
    rejectStart: (marker: UpdateMarker, message: string) => void;
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

function readMarker(): UpdateMarker | null {
    try {
        const raw = localStorage.getItem(SYSTEM_UPDATE_MARKER_KEY);
        if (!raw) return null;
        const marker = decodeMarker(raw);
        if (!marker && localStorage.getItem(SYSTEM_UPDATE_MARKER_KEY) === raw) {
            localStorage.removeItem(SYSTEM_UPDATE_MARKER_KEY);
        }
        return marker;
    } catch {
        return null;
    }
}

function storeMarker(marker: UpdateMarker): boolean {
    try {
        const encoded = JSON.stringify(marker);
        localStorage.setItem(SYSTEM_UPDATE_MARKER_KEY, encoded);
        return localStorage.getItem(SYSTEM_UPDATE_MARKER_KEY) === encoded;
    } catch {
        return false;
    }
}

function markerMatches(left: UpdateMarker | null, right: UpdateMarker): boolean {
    return left?.request_id === right.request_id && sameUpdateTarget(left.target, right.target);
}

function clearExactMarker(marker: UpdateMarker) {
    try {
        const current = readMarker();
        if (markerMatches(current, marker)) {
            localStorage.removeItem(SYSTEM_UPDATE_MARKER_KEY);
        }
    } catch {
        // The exact terminal result remains visible in this tab.
    }
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
        const response = await fetch(`/api/v1/panel/update/status?request_id=${encodeURIComponent(marker.request_id)}`, {
            cache: 'no-store',
            credentials: 'same-origin',
            signal: controller.signal,
        });
        if (!response.ok) {
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
            if (Math.max(0, Date.now() - marker.created_at) >= NOT_FOUND_GRACE_MS) {
                return { kind: 'failed', message: t('panelUpdate.notAccepted') };
            }
            return { kind: 'retry', message: t('panelUpdate.notFoundYet'), disconnected: false };
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

export function SystemUpdateOperationProvider({ children }: { children: ReactNode }) {
    const { t } = useI18n();
    const [initialMarker] = useState<UpdateMarker | null>(() => readMarker());
    const [marker, setMarker] = useState<UpdateMarker | null>(initialMarker);
    const markerRef = useRef<UpdateMarker | null>(initialMarker);
    const [view, setView] = useState<TrackingView>({
        operation: null,
        message: initialMarker ? t('panelUpdate.running') : '',
        disconnected: false,
        lastAttemptAt: null,
    });
    const [terminal, setTerminal] = useState<TerminalResult | null>(null);
    const [now, setNow] = useState(Date.now());
    const reloadTimerRef = useRef<number | null>(null);
    const applicationRef = useRef<HTMLDivElement>(null);
    const dialogRef = useRef<HTMLDivElement>(null);

    const schedulePostUpdateReload = useCallback((exactMarker: UpdateMarker): boolean => {
        if (reloadTimerRef.current !== null) return true;
        const next = new URL(window.location.href);
        if (next.searchParams.get(POST_UPDATE_RELOAD_PARAM) === exactMarker.request_id) return false;
        next.searchParams.set(POST_UPDATE_RELOAD_PARAM, exactMarker.request_id);
        reloadTimerRef.current = window.setTimeout(() => {
            window.location.replace(next.toString());
        }, POST_UPDATE_RELOAD_MS);
        return true;
    }, []);

    useEffect(() => {
        markerRef.current = marker;
    }, [marker]);

    useEffect(() => {
        if (marker) return;
        try {
            const current = new URL(window.location.href);
            if (!current.searchParams.has(POST_UPDATE_RELOAD_PARAM)) return;
            current.searchParams.delete(POST_UPDATE_RELOAD_PARAM);
            window.history.replaceState(
                window.history.state,
                '',
                `${current.pathname}${current.search}${current.hash}`,
            );
        } catch {
            // A stale same-origin cache buster carries no authority.
        }
    }, [marker]);

    useEffect(() => {
        const onStorage = (event: StorageEvent) => {
            if (event.storageArea !== localStorage || event.key !== SYSTEM_UPDATE_MARKER_KEY || !event.newValue) return;
            const incoming = decodeMarker(event.newValue);
            if (!incoming || markerRef.current) return;
            markerRef.current = incoming;
            setTerminal(null);
            setView({
                operation: null,
                message: t('panelUpdate.running'),
                disconnected: false,
                lastAttemptAt: null,
            });
            setMarker(incoming);
        };
        window.addEventListener('storage', onStorage);
        return () => window.removeEventListener('storage', onStorage);
    }, [t]);

    useEffect(() => {
        if (!marker) return undefined;
        const exactMarker = marker;
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
            if (cancelled) return;
            if (inFlight) {
                pollAgain = true;
                return;
            }
            inFlight = true;
            const outcome = await pollExact(exactMarker, t);
            inFlight = false;
            if (cancelled || !markerMatches(markerRef.current, exactMarker)) return;

            const attemptAt = Date.now();
            if (outcome.kind === 'succeeded') {
                clearExactMarker(exactMarker);
                markerRef.current = null;
                setMarker(null);
                const reloadScheduled = schedulePostUpdateReload(exactMarker);
                setTerminal({
                    kind: 'succeeded',
                    marker: exactMarker,
                    reloadScheduled,
                    message: reloadScheduled
                        ? t('panelUpdate.reloading', { version: exactMarker.target.version })
                        : t('panelUpdate.succeeded'),
                });
                return;
            }
            if (outcome.kind === 'failed') {
                clearExactMarker(exactMarker);
                markerRef.current = null;
                setMarker(null);
                setTerminal({
                    kind: 'failed',
                    marker: exactMarker,
                    message: outcome.message,
                    reloadScheduled: false,
                });
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
        const onVisibility = () => {
            if (document.visibilityState === 'visible') wake();
        };

        window.addEventListener('focus', wake);
        window.addEventListener('online', wake);
        window.addEventListener('pageshow', wake);
        document.addEventListener('visibilitychange', onVisibility);
        schedule(0);
        return () => {
            cancelled = true;
            if (timer !== null) window.clearTimeout(timer);
            window.removeEventListener('focus', wake);
            window.removeEventListener('online', wake);
            window.removeEventListener('pageshow', wake);
            document.removeEventListener('visibilitychange', onVisibility);
        };
    }, [marker, schedulePostUpdateReload, t]);

    const blocking = marker !== null || terminal !== null;
    useEffect(() => {
        if (!blocking) return undefined;
        const application = applicationRef.current;
        const previousOverflow = document.body.style.overflow;
        if (application) application.inert = true;
        document.body.style.overflow = 'hidden';
        dialogRef.current?.focus();
        const keepFocusInDialog = (event: FocusEvent) => {
            if (dialogRef.current && event.target instanceof Node && !dialogRef.current.contains(event.target)) {
                dialogRef.current.focus();
            }
        };
        document.addEventListener('focusin', keepFocusInDialog);
        return () => {
            document.removeEventListener('focusin', keepFocusInDialog);
            if (application) application.inert = false;
            document.body.style.overflow = previousOverflow;
        };
    }, [blocking]);

    useEffect(() => {
        if (!blocking) return undefined;
        setNow(Date.now());
        const timer = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(timer);
    }, [blocking]);

    const begin = useCallback((exactMarker: UpdateMarker): boolean => {
        if (markerRef.current || terminal || !storeMarker(exactMarker)) return false;
        markerRef.current = exactMarker;
        setTerminal(null);
        setView({
            operation: null,
            message: t('panelUpdate.sending'),
            disconnected: false,
            lastAttemptAt: null,
        });
        setMarker(exactMarker);
        return true;
    }, [t, terminal]);

    const noteAccepted = useCallback((exactMarker: UpdateMarker) => {
        if (!markerMatches(markerRef.current, exactMarker)) return;
        setView((current) => ({
            ...current,
            message: current.operation ? current.message : t('panelUpdate.accepted'),
            disconnected: false,
        }));
    }, [t]);

    const noteUncertain = useCallback((exactMarker: UpdateMarker, message: string, disconnected = false) => {
        if (!markerMatches(markerRef.current, exactMarker)) return;
        setView((current) => ({ ...current, message, disconnected }));
    }, []);

    const rejectStart = useCallback((exactMarker: UpdateMarker, message: string) => {
        if (!markerMatches(markerRef.current, exactMarker)) return;
        clearExactMarker(exactMarker);
        markerRef.current = null;
        setMarker(null);
        setTerminal({
            kind: 'failed',
            marker: exactMarker,
            message,
            reloadScheduled: false,
        });
    }, []);

    const dismissTerminal = () => setTerminal(null);
    const exactMarker = marker ?? terminal?.marker ?? null;
    const disconnected = marker !== null && view.disconnected;
    const title = terminal?.kind === 'failed'
        ? t('panelUpdate.failed')
        : terminal?.kind === 'succeeded'
            ? t('panelUpdate.succeeded')
            : t('panelUpdate.title');
    const message = terminal?.message ?? view.message;

    return (
        <SystemUpdateOperationContext.Provider value={{
            active: marker !== null,
            begin,
            noteAccepted,
            noteUncertain,
            rejectStart,
        }}>
            <div ref={applicationRef} className="contents" aria-hidden={blocking ? true : undefined}>
                {children}
            </div>
            {blocking && exactMarker && (
                <div
                    ref={dialogRef}
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="system-update-operation-title"
                    aria-describedby="system-update-operation-status"
                    tabIndex={-1}
                    onKeyDown={(event) => {
                        if (event.key === 'Escape' || (marker && event.key === 'Tab')) {
                            event.preventDefault();
                            dialogRef.current?.focus();
                        }
                    }}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-950/80 p-4 backdrop-blur-sm outline-none"
                >
                    <div className="w-full max-w-lg rounded-2xl border border-border-strong bg-surface p-6 text-center shadow-2xl">
                        <span className={`mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl ${
                            terminal?.kind === 'failed'
                                ? 'bg-danger/10 text-danger'
                                : terminal?.kind === 'succeeded'
                                    ? 'bg-success/10 text-success'
                                    : disconnected
                                        ? 'bg-warning/10 text-warning'
                                        : 'bg-primary/10 text-primary'
                        }`}>
                            {terminal?.kind === 'failed' ? <XCircle className="h-7 w-7" />
                                : terminal?.kind === 'succeeded' ? <CheckCircle2 className="h-7 w-7" />
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
                            {disconnected ? t('panelUpdate.connectionLost') : t('panelUpdate.accepted')}
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
                        {terminal && (!terminal.reloadScheduled || terminal.kind === 'failed') && (
                            <div className="mt-5 flex justify-center">
                                <Button type="button" onClick={dismissTerminal}>
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
