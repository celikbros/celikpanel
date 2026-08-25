import { useCallback, useEffect, useRef, useState } from 'react';
import { AlertTriangle, CheckCircle2, DownloadCloud, Loader2, RefreshCw, XCircle } from 'lucide-react';
import { Button } from './ui';
import { useI18n } from '../i18n';
import { apiErrorText, readApiError } from '../lib/apiError';

type Translate = ReturnType<typeof useI18n>['t'];

const UPDATE_MARKER_KEY = 'celikpanel.system-update-operation.v1';
const POST_UPDATE_RELOAD_PARAM = '_cp_update';
const POST_UPDATE_RELOAD_MS = 1500;
const POLL_MIN_MS = 1500;
const POLL_MAX_MS = 15000;
const NOT_FOUND_GRACE_MS = 120000;

type UpdateTarget = {
    version: string;
    commit: string;
    sequence: string;
    os: 'linux';
    arch: 'amd64' | 'arm64';
    archive_sha256: string;
    archive_size: string;
    published_at?: string;
};

type UpdateMarker = {
    marker_version: 1;
    request_id: string;
    current_version: string;
    current_commit: string;
    target: UpdateTarget;
    created_at: number;
};

type UpdateCheck = {
    supported: boolean;
    available: boolean;
    current_version: string;
    current_commit: string;
    target?: UpdateTarget;
};

type UpdateStatus = {
    found: boolean;
    request_id: string;
    status?: 'queued' | 'running' | 'succeeded' | 'failed';
    target?: UpdateTarget;
    summary?: string;
};

type PanelBuild = {
    version: string;
    commit: string;
};

const requestIDPattern = /^[a-f0-9]{32}$/;
const commitPattern = /^[a-f0-9]{40}$/;
const digestPattern = /^[a-f0-9]{64}$/;
const decimalPattern = /^[1-9][0-9]*$/;

function validVersion(version: unknown): version is string {
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

function validTarget(target: unknown): target is UpdateTarget {
    if (!target || typeof target !== 'object') return false;
    const value = target as Record<string, unknown>;
    return validVersion(value.version)
        && typeof value.commit === 'string' && commitPattern.test(value.commit)
        && validDecimal(value.sequence, 9223372036854775807n)
        && value.os === 'linux' && (value.arch === 'amd64' || value.arch === 'arm64')
        && typeof value.archive_sha256 === 'string' && digestPattern.test(value.archive_sha256)
        && validDecimal(value.archive_size, 2147483648n)
        && (value.published_at === undefined || (typeof value.published_at === 'string'
            && value.published_at.length <= 40
            && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value.published_at)));
}

function decodeUpdateCheck(payload: unknown): UpdateCheck | null {
    if (!payload || typeof payload !== 'object') return null;
    const value = payload as Record<string, unknown>;
    if (value.supported !== true || typeof value.available !== 'boolean'
        || !validVersion(value.current_version)
        || typeof value.current_commit !== 'string' || !commitPattern.test(value.current_commit)) return null;
    if (value.available) {
        if (!validTarget(value.target)) return null;
        return {
            supported: true, available: true,
            current_version: value.current_version, current_commit: value.current_commit,
            target: value.target,
        };
    }
    if (value.target !== undefined) return null;
    return {
        supported: true, available: false,
        current_version: value.current_version, current_commit: value.current_commit,
    };
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
    if (!validTarget(value.target)) return null;
    if (value.summary !== undefined && !validSummary(value.summary)) return null;
    return {
        found: true, request_id: value.request_id, status: value.status,
        target: value.target, ...(value.summary ? { summary: value.summary } : {}),
    };
}

function sameTarget(left: UpdateTarget, right: UpdateTarget): boolean {
    return left.version === right.version
        && left.commit === right.commit
        && left.sequence === right.sequence
        && left.os === right.os
        && left.arch === right.arch
        && left.archive_sha256 === right.archive_sha256
        && left.archive_size === right.archive_size;
}

function readMarker(): UpdateMarker | null {
    try {
        const raw = localStorage.getItem(UPDATE_MARKER_KEY);
        if (!raw || raw.length > 4096) return null;
        const value = JSON.parse(raw) as Record<string, unknown>;
        if (value.marker_version !== 1
            || typeof value.request_id !== 'string' || !requestIDPattern.test(value.request_id)
            || typeof value.current_version !== 'string'
            || typeof value.current_commit !== 'string' || !commitPattern.test(value.current_commit)
            || typeof value.created_at !== 'number' || !Number.isFinite(value.created_at)
            || !validTarget(value.target)) return null;
        return value as UpdateMarker;
    } catch {
        return null;
    }
}

function storeMarker(marker: UpdateMarker): boolean {
    try {
        // Safety invariant: the exact request identity is durable before POST.
        localStorage.setItem(UPDATE_MARKER_KEY, JSON.stringify(marker));
        return localStorage.getItem(UPDATE_MARKER_KEY) === JSON.stringify(marker);
    } catch {
        return false;
    }
}

function clearExactMarker(marker: UpdateMarker) {
    try {
        const current = readMarker();
        if (current?.request_id === marker.request_id && sameTarget(current.target, marker.target)) {
            localStorage.removeItem(UPDATE_MARKER_KEY);
        }
    } catch {
        // The terminal result remains visible in this tab.
    }
}

function createRequestID(): string | null {
    try {
        const bytes = new Uint8Array(16);
        crypto.getRandomValues(bytes);
        return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
    } catch {
        return null;
    }
}

function responseHint(status: number, t: Translate): string {
    if (status === 401) return t('panelUpdate.sessionExpired');
    if (status === 403) return t('panelUpdate.adminOnly');
    if (status === 408 || status === 504) return t('panelUpdate.timeout');
    if (status === 409) return t('panelUpdate.busy');
    if (status === 429) return t('panelUpdate.rateLimited');
    if (status >= 500) return t('panelUpdate.restarting');
    return t('panelUpdate.invalidResponse');
}

async function codedResponseHint(response: Response, t: Translate): Promise<{ code?: string; message: string }> {
    const apiError = await readApiError(response);
    return {
        ...(apiError.code ? { code: apiError.code } : {}),
        message: apiError.code ? apiErrorText(apiError, t) : responseHint(response.status, t),
    };
}

function definitiveStartRejection(status: number): boolean {
    return status === 400 || status === 401 || status === 403 || status === 404
        || status === 405 || status === 409 || status === 413 || status === 415
        || status === 422 || status === 429;
}

function startRejectionHint(status: number, t: Translate): string {
    if (status === 401) return t('panelUpdate.sessionExpiredBeforeStart');
    if (status === 429) return t('panelUpdate.rateLimitedBeforeStart');
    return responseHint(status, t);
}

export function PanelUpdateCard() {
    const { t } = useI18n();
    const [currentBuild, setCurrentBuild] = useState<PanelBuild | null>(null);
    const [check, setCheck] = useState<UpdateCheck | null>(null);
    const [marker, setMarker] = useState<UpdateMarker | null>(() => readMarker());
    const [operation, setOperation] = useState<UpdateStatus | null>(null);
    const [checking, setChecking] = useState(false);
    const [starting, setStarting] = useState(false);
    const [message, setMessage] = useState('');
    const actionInFlight = useRef(false);
    const reloadTimerRef = useRef<number | null>(null);

    useEffect(() => {
        let cancelled = false;
        fetch('/api/v1/panel/version', { cache: 'no-store', credentials: 'same-origin' })
            .then(async (response) => (response.ok ? response.json() as Promise<unknown> : null))
            .then((payload) => {
                if (cancelled || !payload || typeof payload !== 'object') return;
                const value = payload as Record<string, unknown>;
                if (typeof value.version !== 'string' || value.version.length > 80
                    || typeof value.commit !== 'string' || value.commit.length > 80) return;
                setCurrentBuild({ version: value.version, commit: value.commit });
            })
            .catch(() => undefined);
        return () => { cancelled = true; };
    }, []);

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
            // A stale cache-buster is harmless; it is same-origin and carries
            // no authority. Never turn URL cleanup into an update failure.
        }
    }, [marker]);

    const schedulePostUpdateReload = useCallback((exactMarker: UpdateMarker): boolean => {
        if (reloadTimerRef.current !== null) return true;
        const next = new URL(window.location.href);
        if (next.searchParams.get(POST_UPDATE_RELOAD_PARAM) === exactMarker.request_id) return false;
        next.searchParams.set(POST_UPDATE_RELOAD_PARAM, exactMarker.request_id);
        // Intentionally survive a component unmount caused by SPA navigation:
        // the installed frontend must replace the old in-memory application.
        reloadTimerRef.current = window.setTimeout(() => {
            window.location.replace(next.toString());
        }, POST_UPDATE_RELOAD_MS);
        return true;
    }, []);

    const pollExact = useCallback(async (exactMarker: UpdateMarker): Promise<'terminal' | 'retry'> => {
        try {
            const response = await fetch(`/api/v1/panel/update/status?request_id=${encodeURIComponent(exactMarker.request_id)}`, {
                cache: 'no-store',
                credentials: 'same-origin',
            });
            if (!response.ok) {
                setMessage(responseHint(response.status, t));
                return 'retry';
            }
            const payload = decodeUpdateStatus(await response.json());
            if (!payload) {
                setMessage(t('panelUpdate.invalidResponse'));
                return 'retry';
            }
            if (!payload.found) {
                if (Math.max(0, Date.now() - exactMarker.created_at) >= NOT_FOUND_GRACE_MS) {
                    clearExactMarker(exactMarker);
                    setMarker(null);
                    setMessage(t('panelUpdate.notAccepted'));
                    return 'terminal';
                }
                setMessage(t('panelUpdate.notFoundYet'));
                return 'retry';
            }
            if (payload.request_id !== exactMarker.request_id || !payload.target || !validTarget(payload.target)
                || !sameTarget(payload.target, exactMarker.target)) {
                // The server-side global mutation gate remains authoritative.
                // A mismatched reply can never prove this local marker, so
                // retaining it would only wedge the administrator UI.
                clearExactMarker(exactMarker);
                setMarker(null);
                setMessage(t('panelUpdate.identityMismatchCleared'));
                return 'terminal';
            }
            setOperation(payload);
            if (payload.status === 'succeeded' || payload.status === 'failed') {
                clearExactMarker(exactMarker);
                setMarker(null);
                if (payload.status === 'succeeded') {
                    setMessage(schedulePostUpdateReload(exactMarker)
                        ? t('panelUpdate.reloading', { version: payload.target.version })
                        : t('panelUpdate.succeeded'));
                } else {
                    setMessage(payload.summary || t('panelUpdate.failed'));
                }
                return 'terminal';
            }
            setMessage(payload.status === 'running' ? t('panelUpdate.running') : t('panelUpdate.queued'));
            return 'retry';
        } catch {
            setMessage(t('panelUpdate.connectionLost'));
            return 'retry';
        }
    }, [schedulePostUpdateReload, t]);

    useEffect(() => {
        if (!marker) return undefined;
        const exactMarker = marker;
        let cancelled = false;
        let timer: ReturnType<typeof setTimeout> | undefined;
        let delay = POLL_MIN_MS;
        const poll = async () => {
            const result = await pollExact(exactMarker);
            if (!cancelled && result === 'retry') {
                timer = setTimeout(() => void poll(), delay);
                delay = Math.min(POLL_MAX_MS, Math.round(delay * 1.6));
            }
        };
        void poll();
        return () => {
            cancelled = true;
            if (timer) clearTimeout(timer);
        };
    }, [marker, pollExact]);

    async function checkForUpdate() {
        if (actionInFlight.current || marker) return;
        actionInFlight.current = true;
        setChecking(true);
        setMessage('');
        setCheck(null);
        try {
            const response = await fetch('/api/v1/panel/update/check', { cache: 'no-store', credentials: 'same-origin' });
            if (!response.ok) {
                throw new Error((await codedResponseHint(response, t)).message);
            }
            const payload = decodeUpdateCheck(await response.json());
            if (!payload) {
                throw new Error(t('panelUpdate.unsupported'));
            }
            setCheck(payload);
            setCurrentBuild({ version: payload.current_version, commit: payload.current_commit });
            setMessage(payload.available ? t('panelUpdate.available') : t('panelUpdate.none'));
        } catch (error) {
            setMessage(error instanceof Error ? error.message : t('panelUpdate.checkFailed'));
        } finally {
            setChecking(false);
            actionInFlight.current = false;
        }
    }

    async function startUpdate() {
        const target = check?.target;
        if (actionInFlight.current || marker || !check?.available || !target) return;
        const requestID = createRequestID();
        if (!requestID) {
            setMessage(t('panelUpdate.randomFailed'));
            return;
        }
        const exactMarker: UpdateMarker = {
            marker_version: 1,
            request_id: requestID,
            current_version: check.current_version,
            current_commit: check.current_commit,
            target,
            created_at: Date.now(),
        };
        if (!storeMarker(exactMarker)) {
            setMessage(t('panelUpdate.markerFailed'));
            return;
        }
        setMarker(exactMarker);
        actionInFlight.current = true;
        setStarting(true);
        setMessage(t('panelUpdate.sending'));
        try {
            const response = await fetch('/api/v1/panel/update/start', {
                method: 'POST',
                credentials: 'same-origin',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    request_id: exactMarker.request_id,
                    confirmed: true,
                    current_version: exactMarker.current_version,
                    current_commit: exactMarker.current_commit,
                    ...exactMarker.target,
                }),
            });
            if (!response.ok) {
                const rejection = await codedResponseHint(response, t);
                if (definitiveStartRejection(response.status)
                    || rejection.code === 'PANEL_UPDATE_UNAVAILABLE') {
                    clearExactMarker(exactMarker);
                    setMarker(null);
                    setMessage(rejection.code ? rejection.message : startRejectionHint(response.status, t));
                    return;
                }
                setMessage(rejection.message);
                // No POST is retried automatically. Only ambiguous server
                // failures retain the marker and drive exact status polling.
                return;
            }
            setMessage(t('panelUpdate.accepted'));
        } catch {
            setMessage(t('panelUpdate.ambiguous'));
        } finally {
            setStarting(false);
            actionInFlight.current = false;
        }
    }

    const target = check?.target;
    const active = marker !== null;
    const currentVersion = check?.current_version ?? currentBuild?.version;
    const currentCommit = check?.current_commit ?? currentBuild?.commit;
    return (
        <section className="rounded-xl border border-border bg-surface p-5 shadow-card" aria-labelledby="panel-update-title">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="flex min-w-0 gap-3">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <DownloadCloud className="h-5 w-5" aria-hidden="true" />
                    </span>
                    <div>
                        <h3 id="panel-update-title" className="font-semibold text-fg">{t('panelUpdate.title')}</h3>
                        <p className="mt-1 text-sm text-fg-muted">{t('panelUpdate.description')}</p>
                    </div>
                </div>
                <Button type="button" onClick={() => void checkForUpdate()} disabled={checking || active || starting}>
                    {checking ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                    {checking ? t('panelUpdate.checking') : t('panelUpdate.check')}
                </Button>
            </div>

            <div className="mt-4 rounded-lg border border-amber-400/40 bg-amber-400/10 p-4 text-sm text-fg" role="note">
                <div className="flex items-start gap-2"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-500" aria-hidden="true" />
                    <p>{t('panelUpdate.alphaNotice')}</p>
                </div>
            </div>

            {currentVersion && currentCommit && (
                <dl className="mt-4 grid gap-3 rounded-lg border border-border bg-surface-subtle p-4 text-sm sm:grid-cols-2">
                    <div><dt className="text-fg-muted">{t('panelUpdate.currentVersion')}</dt><dd className="font-mono text-fg">{currentVersion}</dd></div>
                    <div><dt className="text-fg-muted">{t('panelUpdate.currentCommit')}</dt><dd className="break-all font-mono text-fg">{currentCommit}</dd></div>
                    {target && <>
                        <div><dt className="text-fg-muted">{t('panelUpdate.targetVersion')}</dt><dd className="font-mono font-semibold text-fg">{target.version}</dd></div>
                        <div><dt className="text-fg-muted">{t('panelUpdate.targetPlatform')}</dt><dd className="font-mono text-fg">{target.os}/{target.arch}</dd></div>
                        <div><dt className="text-fg-muted">{t('panelUpdate.sequence')}</dt><dd className="font-mono text-fg">{target.sequence}</dd></div>
                        <div><dt className="text-fg-muted">{t('panelUpdate.archiveSize')}</dt><dd className="font-mono text-fg">{t('panelUpdate.bytes', { size: target.archive_size })}</dd></div>
                        <div className="sm:col-span-2"><dt className="text-fg-muted">{t('panelUpdate.sha256')}</dt><dd className="break-all font-mono text-xs text-fg">{target.archive_sha256}</dd></div>
                    </>}
                </dl>
            )}

            {target && check?.available && !active && (
                <div className="mt-4">
                    <Button type="button" onClick={() => void startUpdate()} disabled={starting}>
                        {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <DownloadCloud className="h-4 w-4" />}
                        {starting ? t('panelUpdate.starting') : t('panelUpdate.start', { version: target.version })}
                    </Button>
                </div>
            )}

            {(message || operation) && (
                <div className="mt-4 flex items-start gap-2 rounded-lg border border-border p-3 text-sm text-fg" role="status" aria-live="polite">
                    {operation?.status === 'succeeded' ? <CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-500" />
                        : operation?.status === 'failed' ? <XCircle className="h-5 w-5 shrink-0 text-red-500" />
                            : active ? <Loader2 className="h-5 w-5 shrink-0 animate-spin text-primary" /> : <AlertTriangle className="h-5 w-5 shrink-0 text-amber-500" />}
                    <p>{message}</p>
                </div>
            )}
        </section>
    );
}
