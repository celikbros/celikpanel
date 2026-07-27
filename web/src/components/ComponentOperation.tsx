import {
    createContext,
    useContext,
    useEffect,
    useRef,
    useState,
    type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { Loader2 as LoaderCircle, WifiOff, X } from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { readApiError, type ApiError } from '../lib/apiError';
import { showToast } from './Toast';
import { ErrorBanner } from './ui';

const OPERATION_ID_KEY = 'celikpanel.components.operation-id';
const OPERATION_LABEL_KEY = 'celikpanel.components.operation-label';
const POLL_DELAY_MS = 1500;
const RETRY_DELAY_MS = 3000;
const RECOVERY_LOOKUP_GRACE_MS = 15000;
const RECENT_OPERATION_MS = 2 * 60 * 1000;

type OperationStatus = 'queued' | 'running' | 'succeeded' | 'failed';

export interface ComponentOperation {
    id: string;
    kind: string;
    service_id: string;
    package_name?: string;
    status: OperationStatus;
    phase: string;
    started_at: string;
    finished_at?: string;
    error?: unknown;
    result?: unknown;
}

export interface ManagedServicesSnapshot {
    services: Record<string, unknown>[];
    scanned_at?: string | null;
}

export interface InstallOperationRequest {
    serviceId: string;
    name: string;
    package?: string;
    version?: string;
}

interface ComponentOperationContextValue {
    operation: ComponentOperation | null;
    locked: boolean;
    failure: ApiError | null;
    catalogSnapshot: ManagedServicesSnapshot | null;
    startInstall: (request: InstallOperationRequest) => Promise<boolean>;
    clearFailure: () => void;
}

const ComponentOperationContext = createContext<ComponentOperationContextValue | null>(null);

function readSessionValue(key: string): string {
    try {
        return sessionStorage.getItem(key) ?? '';
    } catch {
        return '';
    }
}

function storeOperation(id: string, label: string) {
    try {
        sessionStorage.setItem(OPERATION_ID_KEY, id);
        sessionStorage.setItem(OPERATION_LABEL_KEY, label);
    } catch {
        // Restricted storage does not stop the live operation. The current
        // page can still poll it; only reload reattachment is unavailable.
    }
}

function clearStoredOperation() {
    try {
        sessionStorage.removeItem(OPERATION_ID_KEY);
        sessionStorage.removeItem(OPERATION_LABEL_KEY);
    } catch {
        // Nothing else is required when storage is unavailable.
    }
}

function decodeOperation(payload: unknown): ComponentOperation | null {
    const envelope = payload && typeof payload === 'object' ? payload as Record<string, unknown> : null;
    const candidate = envelope?.operation && typeof envelope.operation === 'object'
        ? envelope.operation as Record<string, unknown>
        : envelope;
    if (!candidate) return null;

    const status = candidate.status;
    if (
        typeof candidate.id !== 'string'
        || !candidate.id
        || (status !== 'queued' && status !== 'running' && status !== 'succeeded' && status !== 'failed')
    ) {
        return null;
    }

    return {
        id: candidate.id,
        kind: typeof candidate.kind === 'string' ? candidate.kind : 'install',
        service_id: typeof candidate.service_id === 'string' ? candidate.service_id : '',
        package_name: typeof candidate.package_name === 'string' ? candidate.package_name : undefined,
        status,
        phase: typeof candidate.phase === 'string' ? candidate.phase : status,
        started_at: typeof candidate.started_at === 'string' ? candidate.started_at : '',
        finished_at: typeof candidate.finished_at === 'string' ? candidate.finished_at : undefined,
        error: candidate.error,
        result: candidate.result,
    };
}

function expectedOperationKind(request: InstallOperationRequest): string {
    return request.version ? 'runtime_install' : 'service_install';
}

function expectedOperationTarget(request: InstallOperationRequest): string {
    return (request.version || request.package || '').trim().toLowerCase();
}

function operationMatchesRequest(operation: ComponentOperation, request: InstallOperationRequest): boolean {
    if (operation.kind !== expectedOperationKind(request)) return false;
    if (operation.service_id.trim().toLowerCase() !== request.serviceId.trim().toLowerCase()) return false;
    return (operation.package_name || '').trim().toLowerCase() === expectedOperationTarget(request);
}

function operationDisplayLabel(operation: ComponentOperation): string {
    const serviceID = operation.service_id.trim();
    const target = (operation.package_name || '').trim();
    if (operation.kind === 'runtime_install' && serviceID.toLowerCase() === 'node') {
        return target ? `Node.js ${target}` : 'Node.js';
    }
    if (serviceID && target && serviceID.toLowerCase() !== target.toLowerCase()) {
        return `${serviceID} (${target})`;
    }
    return serviceID || target || 'service';
}

function responseReferenceTime(response: Response): number {
    const serverDate = Date.parse(response.headers.get('Date') || '');
    return Number.isFinite(serverDate) ? serverDate : Date.now();
}

function isRecentTerminalOperation(operation: ComponentOperation, referenceTime: number): boolean {
    if (operation.status !== 'succeeded' && operation.status !== 'failed') return false;
    const timestamp = Date.parse(operation.finished_at || operation.started_at);
    return Number.isFinite(timestamp)
        && Math.abs(referenceTime - timestamp) <= RECENT_OPERATION_MS;
}

function waitForRetry(): Promise<void> {
    return new Promise((resolve) => window.setTimeout(resolve, RETRY_DELAY_MS));
}

function operationError(value: unknown, fallback: string): ApiError {
    if (typeof value === 'string') {
        return { message: value || fallback };
    }
    if (value && typeof value === 'object') {
        const raw = value as Record<string, unknown>;
        const details = Array.isArray(raw.details)
            ? raw.details.filter((item): item is string => typeof item === 'string')
            : undefined;
        return {
            message:
                (typeof raw.message === 'string' && raw.message)
                || (typeof raw.error === 'string' && raw.error)
                || fallback,
            code: typeof raw.code === 'string' ? raw.code : undefined,
            action: typeof raw.action === 'string' ? raw.action : undefined,
            details,
        };
    }
    return { message: fallback };
}

function restoredOperation(): ComponentOperation | null {
    const id = readSessionValue(OPERATION_ID_KEY);
    if (!id) return null;
    return {
        id,
        kind: 'install',
        service_id: '',
        status: 'queued',
        phase: 'queued',
        started_at: '',
    };
}

export function ComponentOperationProvider({ children }: { children: ReactNode }) {
    const { t } = useI18n();
    const [operation, setOperation] = useState<ComponentOperation | null>(restoredOperation);
    const [label, setLabel] = useState(() => readSessionValue(OPERATION_LABEL_KEY));
    const [submitting, setSubmitting] = useState(false);
    const [refreshingCatalog, setRefreshingCatalog] = useState(false);
    const [connectionInterrupted, setConnectionInterrupted] = useState(false);
    const [failure, setFailure] = useState<ApiError | null>(null);
    const [catalogSnapshot, setCatalogSnapshot] = useState<ManagedServicesSnapshot | null>(null);
    const locked = submitting || operation !== null || refreshingCatalog;
    const lockedRef = useRef(locked);
    const recoveryGenerationRef = useRef(0);

    useEffect(() => {
        lockedRef.current = locked;
    }, [locked]);

    useEffect(() => () => {
        recoveryGenerationRef.current += 1;
    }, []);

    // The overlay is portalled outside #root. Making #root inert therefore
    // blocks pointer, focus and keyboard interaction everywhere underneath
    // while the status dialog remains accessible.
    useEffect(() => {
        if (!locked) return;
        const root = document.getElementById('root');
        if (!root) return;
        const hadInert = root.hasAttribute('inert');
        const previousBusy = root.getAttribute('aria-busy');
        root.setAttribute('inert', '');
        root.setAttribute('aria-busy', 'true');
        return () => {
            if (!hadInert) root.removeAttribute('inert');
            if (previousBusy === null) root.removeAttribute('aria-busy');
            else root.setAttribute('aria-busy', previousBusy);
        };
    }, [locked]);

    const finishFailure = (error: ApiError) => {
        clearStoredOperation();
        lockedRef.current = false;
        setFailure(error);
        setOperation(null);
        setSubmitting(false);
        setRefreshingCatalog(false);
        setConnectionInterrupted(false);
    };

    const adoptOperation = (next: ComponentOperation, nextLabel: string) => {
        lockedRef.current = true;
        storeOperation(next.id, nextLabel);
        setLabel(nextLabel);
        setFailure(null);
        setConnectionInterrupted(false);
        setOperation(next);
    };

    // A failed POST response is not proof that the POST failed. Keep the
    // panel locked while the authoritative operation endpoint is temporarily
    // unreachable, and attach to the operation once its identity is known.
    const recoverCurrentOperation = async (
        request: InstallOperationRequest,
        mode: 'busy' | 'indeterminate',
        generation: number,
    ): Promise<boolean> => {
        const graceDeadline = Date.now() + RECOVERY_LOOKUP_GRACE_MS;
        while (recoveryGenerationRef.current === generation) {
            let response: Response;
            try {
                response = await fetch('/api/v1/service/operation', { cache: 'no-store' });
            } catch {
                setConnectionInterrupted(true);
                await waitForRetry();
                continue;
            }

            if (!response.ok) {
                if (response.status === 429 || response.status >= 500) {
                    setConnectionInterrupted(true);
                    await waitForRetry();
                    continue;
                }
                const error = await readApiError(response);
                finishFailure({
                    ...error,
                    message: error.message || t('services.operation.startFailed'),
                });
                return false;
            }

            let next: ComponentOperation | null;
            try {
                next = decodeOperation(await response.json());
            } catch {
                finishFailure({ message: t('services.operation.invalidResponse') });
                return false;
            }

            if (next) {
                const matching = operationMatchesRequest(next, request);
                const active = next.status === 'queued' || next.status === 'running';
                const recentTerminal = isRecentTerminalOperation(next, responseReferenceTime(response));
                if (active || (recentTerminal && (mode === 'busy' || matching))) {
                    // A 409 means the requested POST was rejected. Even if the
                    // existing target looks similar, describe that operation
                    // from its own persisted fields rather than the clicked UI.
                    const nextLabel = mode === 'busy' || !matching
                        ? operationDisplayLabel(next)
                        : request.name;
                    adoptOperation(next, nextLabel);
                    return true;
                }
            }

            setConnectionInterrupted(false);
            if (Date.now() >= graceDeadline) {
                finishFailure({ message: t('services.operation.startFailed') });
                return false;
            }
            await waitForRetry();
        }
        return false;
    };

    const startInstall = async (request: InstallOperationRequest): Promise<boolean> => {
        if (lockedRef.current) return false;
        const recoveryGeneration = ++recoveryGenerationRef.current;
        lockedRef.current = true;
        setFailure(null);
        setLabel(request.name);
        setSubmitting(true);
        setConnectionInterrupted(false);

        const isNodeRuntime = Boolean(request.version);
        const endpoint = isNodeRuntime ? '/api/v1/runtimes/node' : '/api/v1/service/install';
        const body = isNodeRuntime
            ? { version: request.version }
            : {
                  service_id: request.serviceId,
                  ...(request.package ? { package: request.package } : {}),
              };

        try {
            const response = await fetch(endpoint, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });
            if (!response.ok) {
                if (response.status === 409) {
                    return await recoverCurrentOperation(request, 'busy', recoveryGeneration);
                }
                if (response.status === 429 || response.status >= 500) {
                    setConnectionInterrupted(true);
                    return await recoverCurrentOperation(request, 'indeterminate', recoveryGeneration);
                }
                const error = await readApiError(response);
                finishFailure({
                    ...error,
                    message: error.message || t('services.operation.startFailed'),
                });
                return false;
            }

            let payload: unknown;
            try {
                payload = await response.json();
            } catch {
                return await recoverCurrentOperation(request, 'indeterminate', recoveryGeneration);
            }
            const next = decodeOperation(payload);
            if (!next) {
                return await recoverCurrentOperation(request, 'indeterminate', recoveryGeneration);
            }
            adoptOperation(next, request.name);
            return true;
        } catch {
            // The POST may have reached the server even when its response was
            // lost. Try the authoritative active-operation endpoint before
            // allowing another click that could duplicate the request.
            setConnectionInterrupted(true);
            return await recoverCurrentOperation(request, 'indeterminate', recoveryGeneration);
        } finally {
            if (recoveryGenerationRef.current === recoveryGeneration) {
                setSubmitting(false);
            }
        }
    };

    useEffect(() => {
        if (!operation?.id) return;

        let cancelled = false;
        let timer: number | undefined;
        const schedule = (fn: () => void, delay: number) => {
            if (!cancelled) timer = window.setTimeout(fn, delay);
        };

        const poll = async () => {
            try {
                const response = await fetch(
                    `/api/v1/service/operation?id=${encodeURIComponent(operation.id)}`,
                    { cache: 'no-store' },
                );
                if (!response.ok) {
                    if (response.status >= 400 && response.status < 500 && response.status !== 429) {
                        const error = await readApiError(response);
                        if (!cancelled) {
                            finishFailure({
                                ...error,
                                message: error.message || t('services.operation.notFound'),
                            });
                        }
                        return;
                    }
                    if (!cancelled) {
                        setConnectionInterrupted(true);
                        schedule(poll, RETRY_DELAY_MS);
                    }
                    return;
                }

                let payload: unknown;
                try {
                    payload = await response.json();
                } catch {
                    if (!cancelled) finishFailure({ message: t('services.operation.invalidResponse') });
                    return;
                }
                const next = decodeOperation(payload);
                if (!next) {
                    if (!cancelled) finishFailure({ message: t('services.operation.invalidResponse') });
                    return;
                }
                if (cancelled) return;

                setConnectionInterrupted(false);
                setOperation(next);

                if (next.status === 'failed') {
                    finishFailure(operationError(next.error, t('services.operation.failed')));
                    return;
                }

                if (next.status !== 'succeeded') {
                    schedule(poll, POLL_DELAY_MS);
                    return;
                }

                // A terminal success is not a UI success yet. Keep the global
                // lock until a fresh server scan has been received and stored;
                // every Components consumer then renders the same snapshot.
                setRefreshingCatalog(true);
                let scanResponse: Response;
                try {
                    scanResponse = await fetch('/api/v1/managed-services/scan', { method: 'POST' });
                } catch {
                    setConnectionInterrupted(true);
                    schedule(poll, RETRY_DELAY_MS);
                    return;
                }
                if (!scanResponse.ok) {
                    if (scanResponse.status >= 400 && scanResponse.status < 500 && scanResponse.status !== 429) {
                        const error = await readApiError(scanResponse);
                        finishFailure({
                            ...error,
                            message: error.message || t('services.operation.refreshFailed'),
                        });
                        return;
                    }
                    setConnectionInterrupted(true);
                    schedule(poll, RETRY_DELAY_MS);
                    return;
                }

                let snapshot: unknown;
                try {
                    snapshot = await scanResponse.json();
                } catch {
                    finishFailure({ message: t('services.operation.refreshFailed') });
                    return;
                }
                const record = snapshot && typeof snapshot === 'object'
                    ? snapshot as Record<string, unknown>
                    : null;
                if (!record || !Array.isArray(record.services)) {
                    finishFailure({ message: t('services.operation.refreshFailed') });
                    return;
                }

                const freshSnapshot: ManagedServicesSnapshot = {
                    services: record.services as Record<string, unknown>[],
                    scanned_at: typeof record.scanned_at === 'string' || record.scanned_at === null
                        ? record.scanned_at
                        : undefined,
                };
                setCatalogSnapshot(freshSnapshot);
                clearStoredOperation();
                lockedRef.current = false;
                setOperation(null);
                setRefreshingCatalog(false);
                setConnectionInterrupted(false);
                setFailure(null);
                showToast('success', t('services.installed', {
                    name: label || next.service_id || t('services.install'),
                }));
            } catch {
                // Network errors and 5xx/429 responses are transient. Never
                // unlock here: the server may still be changing packages.
                if (!cancelled) {
                    setConnectionInterrupted(true);
                    schedule(poll, RETRY_DELAY_MS);
                }
            }
        };

        poll();
        return () => {
            cancelled = true;
            if (timer !== undefined) window.clearTimeout(timer);
        };
        // The operation id owns one polling loop. Phase/status updates should
        // not tear it down and open a second loop.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [operation?.id]);

    return (
        <ComponentOperationContext.Provider
            value={{
                operation,
                locked,
                failure,
                catalogSnapshot,
                startInstall,
                clearFailure: () => setFailure(null),
            }}
        >
            {children}
            {locked && createPortal(
                <OperationOverlay
                    operation={operation}
                    label={label}
                    submitting={submitting}
                    refreshing={refreshingCatalog}
                    interrupted={connectionInterrupted}
                />,
                document.body,
            )}
            {failure && !locked && createPortal(
                <div
                    role="alert"
                    className="fixed inset-x-4 top-4 z-[90] mx-auto max-w-2xl rounded-xl bg-surface shadow-2xl"
                >
                    <div className="relative">
                        <ErrorBanner error={failure} className="pr-12" />
                        <button
                            type="button"
                            onClick={() => setFailure(null)}
                            aria-label={t('services.operation.dismiss')}
                            title={t('services.operation.dismiss')}
                            className="absolute right-2 top-2 rounded-md p-1.5 text-danger transition-colors hover:bg-danger/10"
                        >
                            <X className="h-4 w-4" />
                        </button>
                    </div>
                </div>,
                document.body,
            )}
        </ComponentOperationContext.Provider>
    );
}

function OperationOverlay({
    operation,
    label,
    submitting,
    refreshing,
    interrupted,
}: {
    operation: ComponentOperation | null;
    label: string;
    submitting: boolean;
    refreshing: boolean;
    interrupted: boolean;
}) {
    const { t } = useI18n();
    const dialogRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        dialogRef.current?.focus();
    }, []);

    let statusText = t('services.operation.starting');
    if (interrupted) {
        statusText = t('services.operation.reconnecting');
    } else if (refreshing) {
        statusText = t('services.operation.refreshing');
    } else if (!submitting && operation) {
        const normalizedPhase = operation.phase.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
        const phaseKey = `services.operation.phase.${normalizedPhase}` as TranslationKey;
        const translated = t(phaseKey);
        statusText = translated === phaseKey
            ? operation.phase || t('services.operation.running')
            : translated;
    }

    return (
        <div
            ref={dialogRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="component-operation-title"
            tabIndex={-1}
            className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/75 p-4 backdrop-blur-sm outline-none"
        >
            <div className="w-full max-w-lg rounded-2xl border border-border-strong bg-surface p-6 text-center shadow-2xl">
                <span className={`mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl ${
                    interrupted ? 'bg-warning/10 text-warning' : 'bg-primary/10 text-primary'
                }`}>
                    {interrupted
                        ? <WifiOff className="h-7 w-7" />
                        : <LoaderCircle className="h-7 w-7 animate-spin" />}
                </span>
                <h2 id="component-operation-title" className="text-xl font-semibold text-fg">
                    {t('services.operation.title', { name: label || t('services.install') })}
                </h2>
                <p role="status" aria-live="polite" className="mt-2 text-sm font-medium text-fg-muted">
                    {statusText}
                </p>
                <p className="mt-4 rounded-lg border border-border bg-surface-2 px-4 py-3 text-xs leading-5 text-fg-subtle">
                    {t('services.operation.backgroundHint')}
                </p>
                {operation?.id && (
                    <p className="mt-3 font-mono text-[11px] text-fg-subtle">
                        {t('services.operation.id', { id: operation.id })}
                    </p>
                )}
            </div>
        </div>
    );
}

export function useComponentOperation(): ComponentOperationContextValue {
    const value = useContext(ComponentOperationContext);
    if (!value) {
        throw new Error('useComponentOperation must be used within ComponentOperationProvider');
    }
    return value;
}
