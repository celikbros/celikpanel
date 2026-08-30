import { useEffect, useRef, useState } from 'react';
import { AlertTriangle, DownloadCloud, Loader2, RefreshCw } from 'lucide-react';
import { Button } from './ui';
import { useI18n } from '../i18n';
import { apiErrorText, readApiError } from '../lib/apiError';
import {
    fetchHostMutationReadiness,
    runHostMutationAdmission,
    unverifiedHostMutationReadiness,
    type HostMutationReadiness,
} from '../lib/panelUpdateAdmission';
import {
    createSystemUpdateRequestID,
    systemUpdateResponseHint,
    useSystemUpdateOperation,
    validUpdateTarget,
    validUpdateVersion,
    type UpdateMarker,
    type UpdateTarget,
} from './SystemUpdateOperation';

type Translate = ReturnType<typeof useI18n>['t'];

type UpdateCheck = {
    supported: boolean;
    available: boolean;
    current_version: string;
    current_commit: string;
    target?: UpdateTarget;
};

type PanelBuild = {
    version: string;
    commit: string;
};

export const PANEL_UPDATE_CHECK_TIMEOUT_MS = 8000;

export type PanelUpdateCheckRuntime = {
    fetch: typeof fetch;
    setTimeout: (callback: () => void, delay: number) => ReturnType<typeof globalThis.setTimeout>;
    clearTimeout: (timer: ReturnType<typeof globalThis.setTimeout>) => void;
};

const commitPattern = /^[a-f0-9]{40}$/;

function decodeUpdateCheck(payload: unknown): UpdateCheck | null {
    if (!payload || typeof payload !== 'object') return null;
    const value = payload as Record<string, unknown>;
    if (value.supported !== true || typeof value.available !== 'boolean'
        || !validUpdateVersion(value.current_version)
        || typeof value.current_commit !== 'string' || !commitPattern.test(value.current_commit)) return null;
    if (value.available) {
        if (!validUpdateTarget(value.target)) return null;
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

async function codedResponseHint(response: Response, t: Translate): Promise<{ code?: string; message: string }> {
    const apiError = await readApiError(response);
    return {
        ...(apiError.code ? { code: apiError.code } : {}),
        message: apiError.code ? apiErrorText(apiError, t) : systemUpdateResponseHint(response.status, t),
    };
}

export async function fetchPanelUpdateCheck(
    t: Translate,
    runtime: PanelUpdateCheckRuntime = {
        fetch: (...args) => fetch(...args),
        setTimeout: (callback, delay) => globalThis.setTimeout(callback, delay),
        clearTimeout: (timer) => globalThis.clearTimeout(timer),
    },
    externalSignal?: AbortSignal,
): Promise<UpdateCheck> {
    const controller = new AbortController();
    let rejectDeadline: (reason: Error) => void = () => undefined;
    const deadline = new Promise<never>((_resolve, reject) => { rejectDeadline = reject; });
    const abortAndReject = () => {
        controller.abort();
        rejectDeadline(new Error(t('panelUpdate.checkFailed')));
    };
    const timer = runtime.setTimeout(abortAndReject, PANEL_UPDATE_CHECK_TIMEOUT_MS);
    externalSignal?.addEventListener('abort', abortAndReject, { once: true });
    if (externalSignal?.aborted) abortAndReject();
    const requestAndDecode = (async () => {
        const response = await runtime.fetch('/api/v1/panel/update/check', {
            cache: 'no-store', credentials: 'same-origin', signal: controller.signal,
        });
        if (!response.ok) throw new Error((await codedResponseHint(response, t)).message);
        const payload = decodeUpdateCheck(await response.json());
        if (!payload) throw new Error(t('panelUpdate.unsupported'));
        return payload;
    })();
    try {
        return await Promise.race([requestAndDecode, deadline]);
    } finally {
        runtime.clearTimeout(timer);
        externalSignal?.removeEventListener('abort', abortAndReject);
    }
}

export function PanelUpdateCard() {
    const { t } = useI18n();
    const systemUpdate = useSystemUpdateOperation();
    const [currentBuild, setCurrentBuild] = useState<PanelBuild | null>(null);
    const [check, setCheck] = useState<UpdateCheck | null>(null);
    const [checking, setChecking] = useState(false);
    const [readinessChecking, setReadinessChecking] = useState(false);
    const [readiness, setReadiness] = useState<HostMutationReadiness | null>(null);
    const [starting, setStarting] = useState(false);
    const [message, setMessage] = useState('');
    const actionInFlight = useRef(false);
    const lifecycleGeneration = useRef(0);
    const readinessAbort = useRef<AbortController | null>(null);
    const updateCheckAbort = useRef<AbortController | null>(null);

    useEffect(() => () => {
        lifecycleGeneration.current += 1;
        readinessAbort.current?.abort();
        readinessAbort.current = null;
        updateCheckAbort.current?.abort();
        updateCheckAbort.current = null;
    }, []);

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

    async function refreshHostMutationReadiness(): Promise<HostMutationReadiness> {
        const generation = lifecycleGeneration.current;
        readinessAbort.current?.abort();
        const controller = new AbortController();
        readinessAbort.current = controller;
        setReadinessChecking(true);
        try {
            const next = await fetchHostMutationReadiness(undefined, controller.signal);
            if (lifecycleGeneration.current === generation && !controller.signal.aborted) {
                setReadiness(next);
            }
            return next;
        } catch {
            const next = unverifiedHostMutationReadiness();
            if (lifecycleGeneration.current === generation && !controller.signal.aborted) {
                setReadiness(next);
            }
            return next;
        } finally {
            if (readinessAbort.current === controller) readinessAbort.current = null;
            if (lifecycleGeneration.current === generation) setReadinessChecking(false);
        }
    }

    async function retryHostMutationReadiness() {
        if (actionInFlight.current || systemUpdate.active) return;
        actionInFlight.current = true;
        try {
            await refreshHostMutationReadiness();
        } finally {
            actionInFlight.current = false;
        }
    }

    async function checkForUpdate() {
        if (actionInFlight.current || systemUpdate.active) return;
        const generation = lifecycleGeneration.current;
        updateCheckAbort.current?.abort();
        const controller = new AbortController();
        updateCheckAbort.current = controller;
        actionInFlight.current = true;
        setChecking(true);
        setMessage('');
        setCheck(null);
        setReadiness(null);
        try {
            const payload = await fetchPanelUpdateCheck(t, undefined, controller.signal);
            if (lifecycleGeneration.current !== generation || controller.signal.aborted) return;
            setCheck(payload);
            setCurrentBuild({ version: payload.current_version, commit: payload.current_commit });
            setMessage(payload.available ? t('panelUpdate.available') : t('panelUpdate.none'));
            if (payload.available) await refreshHostMutationReadiness();
        } catch (error) {
            if (lifecycleGeneration.current === generation && !controller.signal.aborted) {
                setMessage(error instanceof Error ? error.message : t('panelUpdate.checkFailed'));
            }
        } finally {
            if (updateCheckAbort.current === controller) updateCheckAbort.current = null;
            if (lifecycleGeneration.current === generation) setChecking(false);
            actionInFlight.current = false;
        }
    }

    async function startUpdate() {
        const target = check?.target;
        if (actionInFlight.current || systemUpdate.active || !check?.available || !target) return;
        const generation = lifecycleGeneration.current;
        actionInFlight.current = true;
        setStarting(true);
        setMessage('');
        try {
            // This snapshot is advisory and intentionally has a bounded wait.
            // The backend remains the authoritative admission boundary, so a
            // fresh check is required even when the earlier UI snapshot was ready.
            await runHostMutationAdmission(refreshHostMutationReadiness, async () => {
                // Route changes unmount this card. A readiness response from the
                // abandoned generation must never create a marker or send POST.
                if (lifecycleGeneration.current !== generation) return;
                // Do not create a durable browser marker (and therefore do not mount
                // the full-page tracker) until the bounded preflight proves idle.
                const requestID = createSystemUpdateRequestID();
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
                // The provider durably stores the exact request before this component
                // is allowed to send the only start POST. Its overlay is mounted above
                // routing, so navigation cannot unmount the tracker.
                const result = await systemUpdate.start(exactMarker);
                if (lifecycleGeneration.current === generation && result.kind === 'failed') {
                    setMessage(result.message);
                }
            });
        } finally {
            if (lifecycleGeneration.current === generation) setStarting(false);
            actionInFlight.current = false;
        }
    }

    const target = check?.target;
    const active = systemUpdate.active;
    const currentVersion = check?.current_version ?? currentBuild?.version;
    const currentCommit = check?.current_commit ?? currentBuild?.commit;
    const readinessReason = readiness?.ready === false
        ? t(`services.mutationReadiness.${readiness.reason}`)
        : '';
    const readinessTitle = readinessChecking
        ? t('services.mutationReadiness.checking')
        : readiness?.ready === true
            ? t('panelUpdate.available')
            : t('services.mutationReadiness.title');
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
                <Button type="button" onClick={() => void checkForUpdate()} disabled={checking || active || starting || readinessChecking}>
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
                <div className="mt-4 space-y-3">
                    <div
                        className={`rounded-lg border p-3 text-sm ${readiness?.ready === true
                            ? 'border-emerald-400/40 bg-emerald-400/10'
                            : 'border-amber-400/40 bg-amber-400/10'}`}
                        role="status"
                        aria-live="polite"
                    >
                        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                            <div className="flex items-start gap-2">
                                {readinessChecking
                                    ? <Loader2 className="h-4 w-4 animate-spin text-primary" aria-hidden="true" />
                                    : readiness?.ready === false
                                        ? <AlertTriangle className="h-4 w-4 text-amber-500" aria-hidden="true" />
                                        : null}
                                <div>
                                    <p className="font-semibold text-fg">{readinessTitle}</p>
                                    {!readinessChecking && readinessReason && <p className="mt-1 text-fg-muted">{readinessReason}</p>}
                                </div>
                            </div>
                            {readiness?.ready === false && !readinessChecking && (
                                <Button
                                    type="button"
                                    variant="secondary"
                                    onClick={() => void retryHostMutationReadiness()}
                                    disabled={starting || readinessChecking}
                                >
                                    <RefreshCw className="h-4 w-4" />
                                    {t('common.retry')}
                                </Button>
                            )}
                        </div>
                    </div>
                    <Button
                        id="panel-update-start-button"
                        type="button"
                        onClick={() => void startUpdate()}
                        disabled={starting || readinessChecking || readiness?.ready !== true}
                    >
                        {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <DownloadCloud className="h-4 w-4" />}
                        {starting ? t('panelUpdate.starting') : t('panelUpdate.start', { version: target.version })}
                    </Button>
                </div>
            )}

            {message && (
                <div className="mt-4 flex items-start gap-2 rounded-lg border border-border p-3 text-sm text-fg" role="status" aria-live="polite">
                    <AlertTriangle className="h-5 w-5 shrink-0 text-amber-500" />
                    <p>{message}</p>
                </div>
            )}
        </section>
    );
}
